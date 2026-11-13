package uiclient

// sgrstream_writer_lock_test.go — the WRITER side of the two-writer defect.
//
// The reader's half (sgrstream_integrity.go) can only REPORT a stream in two
// pieces. This file pins the half that stops one being made: OpenSgrStream used to
// open O_WRONLY|O_CREATE|O_TRUNC with no check, so a second recorder on the same
// path wiped the first one's identity (header and, at close, a trailer with ITS
// zero counts) while the first kept appending at its own fd offsets. The kernel
// filled the gap with NULs and the file ended up carrying two trailers — measured
// 2026-11-07: 38,151,147 bytes, `trailer frames=0 bytes=0` at offset 116, then
// 38,150,984 bytes of which 3,874,076 were real frame bytes, then the true
// `frames=885963 bytes=35656031 grid=117x24`.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSgrStreamRefusesASecondLiveWriter pins the refusal.
func TestSgrStreamRefusesASecondLiveWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared"+ScreenStreamSuffix)
	first, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 117, Rows: 24, Revision: "first"})
	if err != nil {
		t.Fatalf("the first OpenSgrStream was refused: %v", err)
	}
	defer func() {
		if cerr := first.Close(); cerr != nil {
			t.Errorf("closing the first stream: %v", cerr)
		}
	}()
	if werr := first.Write([]byte("first writer's frame")); werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	before := first.Bytes()

	second, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 80, Rows: 24, Revision: "second"})
	if err == nil {
		_ = second.Close() //stylegate:ignore the open should have been refused; the close only tidies the handle this assertion is about
		t.Fatalf("a second live writer opened %s over a live recording — that is how the two-trailer, NUL-holed stream was produced", path)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the refusal does not name the stream it refused: %v", err)
	}
	if !strings.Contains(err.Error(), "already being written") {
		t.Errorf("the refusal does not say another recorder holds it: %v", err)
	}
	// THE FIRST WRITER MUST BE UNHARMED: the refused open must not have destroyed
	// anything, which is the difference between a refusal and a refusal that
	// destroys the data it was protecting.
	if first.Err() != "" {
		t.Errorf("the first writer recorded an error of its own: %s", first.Err())
	}
	if werr := first.Write([]byte("more")); werr != nil {
		t.Errorf("the first writer can no longer append after the refused open: %v", werr)
	}
	if first.Bytes() <= before {
		t.Errorf("the first writer's byte count did not grow (%d then %d) — the refused open disturbed it", before, first.Bytes())
	}
}

// TestSgrStreamReopensAfterItsHolderDies is the control the refusal needs: the lock
// is the KERNEL's, so once the holder is gone (a Close here; in the real incident an
// exit — panic and SIGKILL included) the path is openable again and a new recording
// starts clean. A resume that could not reopen its own session's stream would trade
// one defect for a worse one.
func TestSgrStreamReopensAfterItsHolderDies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resumed"+ScreenStreamSuffix)
	first, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 117, Rows: 24, Revision: "first"})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	if werr := first.Write([]byte("a dead run's frame")); werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	if cerr := first.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	second, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 80, Rows: 24, Revision: "second"})
	if err != nil {
		t.Fatalf("reopening a CLOSED stream was refused: %v — a resume must still be able to start a fresh recording", err)
	}
	if cerr := second.Close(); cerr != nil {
		t.Errorf("closing the second stream: %v", cerr)
	}
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read the reopened stream: %v", rerr)
	}
	if bytes.Contains(raw, []byte("a dead run's frame")) {
		t.Errorf("the reopen did not start a fresh recording: the previous run's payload is still in the file")
	}
	if !bytes.HasPrefix(raw, []byte(ScreenStreamHeaderPrefix)) {
		t.Errorf("the reopened stream does not start with a header: %q", raw[:min(len(raw), 40)])
	}
}
