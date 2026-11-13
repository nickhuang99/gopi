package uiclient

// sgrstream_test.go — the RAW append leg's two contracts, pinned.
//
// ① THE OFF SWITCH WRITES NOTHING. An unarmed sink must be the sink it was
// before this file existed: no path, no open, no write. The test proves it by
// the strongest reading available without a syscall tracer — the sink's own
// stream field is nil after construction, the artifact's candidate path does
// not exist after a full frame sequence, and the interpreted artifact is
// BYTE-IDENTICAL to the one a sink that never heard of this capability writes.
// (The syscall-level reading — `strace -c` over a real run — is the acceptance
// run's job, not a unit test's: a test that counted syscalls would have to
// watch the process, which is a different instrument.)
//
// ② ONE FRAME IS THE BYTES, EXACTLY. The recorded frame must be `p` byte for
// byte, including the two values that make any delimiter scheme impossible:
// 0x00 and 0x1b. The assertion is on the CONTENT, never on the length — a
// length-equal but different payload is exactly the defect the artifact exists
// to make impossible.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// sgrFrameBorderline is the payload that separates a real record from a
// length-and-hope one: a NUL, an ESC-introduced SGR sequence, a CR, a LF, a
// digit run that looks exactly like a length prefix, and a literal "#" record
// header's first byte. Every one of them would collide with a delimiter scheme.
var sgrFrameBorderline = []byte("0\n\x00#gopi-sgr-stream \x1b[1;31mred\x1b[0m\r\n12\n#gopi-sgr-stream-end")

// TestSgrStreamOffWritesNothing pins the OFF state: nil stream, no artifact, and
// an interpreted dump byte-identical to the pre-capability shape.
func TestSgrStreamOffWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate"+ScreenStreamSuffix)
	d, err := NewScreenDumper(DumpConfig{
		Path: "",
		Cols: 20,
		Rows: 3,
		Identity: DumpIdentity{
			Surface:          SurfaceChat,
			PID:              7,
			Target:           "self:chat",
			ColsRows:         [2]int{20, 3},
			TerminalColsRows: [2]int{20, 3},
		},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper: %v", err)
	}
	if d.sgr != nil {
		t.Fatalf("an unarmed sink carries a stream (%v) — the OFF state must be nil, not a writer that discards", d.sgr)
	}
	if _, err := d.Write([]byte("hello\x1b[31m world\x00")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := d.Write(sgrFrameBorderline); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("an unarmed run left an artifact at %s (stat err = %v) — the switch's default must write NOTHING", path, err)
	}
	// A nil stream is also the report's off state: it reports 0 frames and 0
	// bytes rather than a plausible-looking number.
	var off *SgrStream
	if got := off.Frames(); got != 0 {
		t.Errorf("a nil stream reported %d frames", got)
	}
	if got := off.Bytes(); got != 0 {
		t.Errorf("a nil stream reported %d bytes", got)
	}
	if got := off.Path(); got != "" {
		t.Errorf("a nil stream reported path %q", got)
	}
	if got := off.Err(); got != "" {
		t.Errorf("a nil stream reported error %q", got)
	}
	if err := off.Write([]byte("x")); err != nil {
		t.Errorf("a nil stream's Write returned %v — the off path must be a no-op", err)
	}
	if err := off.Close(); err != nil {
		t.Errorf("a nil stream's Close returned %v", err)
	}
}

// TestSgrStreamFrameIsByteIdentical pins contract ②: what comes back out of the
// artifact is the bytes that went in, for every frame, byte for byte.
func TestSgrStreamFrameIsByteIdentical(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run"+ScreenStreamSuffix)
	s, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 117, Rows: 26, Revision: "deadbeef"})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	sent := [][]byte{
		[]byte("\x1b[2J\x1b[H"),    // the first paint: pure control
		{},                         // an EMPTY write is not a frame (see Write)
		[]byte("plain text\r\n"),   //
		sgrFrameBorderline,         //
		bytes.Repeat([]byte{0}, 8), // NULs only
	}
	want := make([][]byte, 0, len(sent))
	for _, p := range sent {
		if len(p) == 0 {
			continue // Write documents that an empty frame is not recorded
		}
		if err := s.Write(p); err != nil {
			t.Fatalf("Write(%q): %v", p, err)
		}
		want = append(want, p)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the artifact: %v", err)
	}
	meta, frames, err := ReadSgrFrames(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("ReadSgrFrames: %v (%d bytes on disk)", err, len(blob))
	}
	if !meta.Trailer {
		t.Fatalf("the artifact has no trailer — a completed run must say it reached close-out")
	}
	if meta.Version != SgrStreamVersion {
		t.Errorf("header version = %d, want %d", meta.Version, SgrStreamVersion)
	}
	if meta.Grid != [2]int{117, 26} {
		t.Errorf("header grid = %v, want [117 26]", meta.Grid)
	}
	if meta.Revision != "deadbeef" {
		t.Errorf("header revision = %q, want %q", meta.Revision, "deadbeef")
	}
	if len(frames) != len(want) {
		t.Fatalf("read %d frames, wrote %d", len(frames), len(want))
	}
	for i := range want {
		// CONTENT equality, not length equality (the epic's own criterion B).
		if !bytes.Equal(frames[i], want[i]) {
			t.Errorf("frame %d = %q, want %q", i, frames[i], want[i])
		}
	}
	if meta.Frames != len(want) {
		t.Errorf("trailer frames = %d, want %d", meta.Frames, len(want))
	}
	if wantBytes := int64(len(bytes.Join(want, nil))); meta.Bytes != wantBytes {
		t.Errorf("trailer bytes = %d, want %d", meta.Bytes, wantBytes)
	}
	// The frames carry NO framing decoration: the artifact's frames are exactly
	// the payloads, so nothing was interpreted on the way in.
	total := 0
	for i, f := range frames {
		total += len(f)
		if strings.Contains(string(f), "#gopi-sgr-stream") && !bytes.Equal(f, sgrFrameBorderline) {
			t.Errorf("frame %d carries a record header — the payload must be verbatim, not re-framed", i)
		}
	}
	if total != int(meta.Bytes) {
		t.Errorf("payload total %d != trailer bytes %d", total, meta.Bytes)
	}
}

// TestSgrStreamTruncatedStreamStillReads pins the tolerance the reader promises:
// a stream cut mid-frame (a run that died) returns its completed frames and
// Trailer=false, and it is NOT an error — that is a real state of the world, and
// the artifact most worth reading.
func TestSgrStreamTruncatedStreamStillReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cut"+ScreenStreamSuffix)
	s, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 10, Rows: 2})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	if err := s.Write([]byte("first")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// The file is closed WITHOUT a trailer, which is the shape of a crash: the
	// stream's own Close is what writes it.
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	meta, frames, err := ReadSgrFrames(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("a truncated stream must read, not fail: %v", err)
	}
	if meta.Trailer {
		t.Fatalf("an unterminated stream reported a trailer")
	}
	if len(frames) != 1 || string(frames[0]) != "first" {
		t.Fatalf("frames = %q, want one frame %q", frames, "first")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// A FOREIGN file is an error, not a truncation: its bytes were never this
	// writer's, and reading them as frames would invent a screen.
	if _, _, err := ReadSgrFrames(strings.NewReader("hello world\n")); err == nil {
		t.Fatalf("a file with no SGR stream header was accepted")
	}
}

// TestSgrStreamLegIsInsideTheSink pins the wiring the acceptance run depends on:
// with a stream attached, every byte the SINK is handed reaches the artifact in
// the sink's own order, and the interpreted artifact is unchanged by the leg's
// presence (criterion F: the dump is byte-identical with the switch on and off).
func TestSgrStreamLegIsInsideTheSink(t *testing.T) {
	run := func(t *testing.T, armed bool) (dump []byte, frames [][]byte) {
		t.Helper()
		dir := t.TempDir()
		dumpPath := filepath.Join(dir, "chat-screen.dump")
		cfg := DumpConfig{
			Path: dumpPath,
			Cols: 20,
			Rows: 3,
			Identity: DumpIdentity{
				Surface:          SurfaceChat,
				PID:              9,
				Target:           "self:chat",
				ColsRows:         [2]int{20, 3},
				TerminalColsRows: [2]int{20, 3},
			},
		}
		if armed {
			s, err := OpenSgrStream(SgrStreamConfig{Path: filepath.Join(dir, "s"+ScreenStreamSuffix), Cols: 20, Rows: 3})
			if err != nil {
				t.Fatalf("OpenSgrStream: %v", err)
			}
			cfg.Sgr = s
		}
		d, err := NewScreenDumper(cfg)
		if err != nil {
			t.Fatalf("NewScreenDumper: %v", err)
		}
		base := fixedClock()
		d.now = base
		paints := [][]byte{
			[]byte("\x1b[H\alpha\x1b[0m"),
			[]byte("\x1b[2;1Hbeta"),
			[]byte("\x1b[3;1Hgamma\x00"),
		}
		for _, p := range paints {
			if _, err := d.Write(p); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
		if err := d.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		blob, err := os.ReadFile(dumpPath)
		if err != nil {
			t.Fatalf("read the dump: %v", err)
		}
		// The dump's body is what a reader compares; its record line carries the
		// wall clock, so the comparison below is on the body only (the same
		// reading flush's own change detection uses).
		body := blob[bytes.IndexByte(blob, '\n')+1:]
		if !armed {
			return body, nil
		}
		sgrBlob, err := os.ReadFile(filepath.Join(dir, "s"+ScreenStreamSuffix))
		if err != nil {
			t.Fatalf("read the stream: %v", err)
		}
		_, frames, err = ReadSgrFrames(bytes.NewReader(sgrBlob))
		if err != nil {
			t.Fatalf("ReadSgrFrames: %v", err)
		}
		return body, frames
	}

	offBody, offFrames := run(t, false)
	onBody, onFrames := run(t, true)
	if offFrames != nil {
		t.Fatalf("the unarmed run produced frames")
	}
	if !bytes.Equal(offBody, onBody) {
		t.Fatalf("the interpreted dump CHANGED with the raw leg armed:\n off=%q\n  on=%q", offBody, onBody)
	}
	want := [][]byte{
		[]byte("\x1b[H\alpha\x1b[0m"),
		[]byte("\x1b[2;1Hbeta"),
		[]byte("\x1b[3;1Hgamma\x00"),
	}
	if len(onFrames) != len(want) {
		t.Fatalf("recorded %d frames, painted %d", len(onFrames), len(want))
	}
	for i := range want {
		if !bytes.Equal(onFrames[i], want[i]) {
			t.Errorf("frame %d = %q, want %q", i, onFrames[i], want[i])
		}
	}
}

// fixedClock is the deterministic clock the existing dump tests use: it returns
// the same instant every time, so `due` is false after the first write and the
// throttle never rewrites the file mid-sequence unless forced.
func fixedClock() func() time.Time {
	at := time.Unix(1_700_000_000, 0)
	return func() time.Time { return at }
}

// TestSgrStreamReplaysThroughTheInterpreter pins the epic's criterion C shape at
// the unit level: the recorded bytes replay through lineedit.Screen — an
// interpreter that knows nothing about this file — and produce the same grid the
// sink's own interpretation produced. The reconstruction is from the BYTES, not
// from the dump, which is what makes the artifact a truth rather than a copy.
func TestSgrStreamReplaysThroughTheInterpreter(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSgrStream(SgrStreamConfig{Path: filepath.Join(dir, "replay"+ScreenStreamSuffix), Cols: 12, Rows: 4})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	d, err := NewScreenDumper(DumpConfig{
		Path:     filepath.Join(dir, "chat-screen.dump"),
		Cols:     12,
		Rows:     4,
		Sgr:      s,
		Identity: DumpIdentity{Surface: SurfaceChat, PID: 3, ColsRows: [2]int{12, 4}, TerminalColsRows: [2]int{12, 4}},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper: %v", err)
	}
	d.now = fixedClock()
	paints := [][]byte{
		[]byte("\x1b[Hone\x1b[1;31m two\x1b[0m"),
		[]byte("\x1b[2;1Hthree"),
		[]byte("\x1b[3;1Hfour"),
	}
	for _, p := range paints {
		if _, err := d.Write(p); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	blob, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read the stream: %v", err)
	}
	_, frames, err := ReadSgrFrames(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("ReadSgrFrames: %v", err)
	}
	// The INDEPENDENT interpreter: a fresh screen fed only the recorded bytes.
	replay := lineedit.NewScreen(12, 4)
	for _, f := range frames {
		replay.Feed(f)
	}
	got := replay.Dump()
	want := d.Snapshot()
	for row := range want.Grid {
		if row >= len(got.Grid) {
			t.Fatalf("replay produced %d rows, want %d", len(got.Grid), len(want.Grid))
		}
		if got.Grid[row] != want.Grid[row] {
			t.Errorf("row %d: replay %q != sink %q", row, got.Grid[row], want.Grid[row])
		}
	}
}
