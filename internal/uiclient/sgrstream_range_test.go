package uiclient

// sgrstream_range_test.go — the range reader's own gates.
//
// THE BUG THIS FILE EXISTS TO KEEP OUT (measured 2026-11-06): readSgrRecordAt
// appended the chunk prefix that its per-byte loop had ALREADY appended, so every
// record line came back doubled — a 111-byte header became 222, and each frame
// prefix "16" became "1616". The walk then read "1616" as a length, rejected it,
// and the whole recording parsed as ZERO FRAMES with no error: the screen command
// reported "nothing has been painted yet" over a stream full of painted bytes. A
// silent zero is the worst shape of this defect, which is why the first test
// asserts a NON-EMPTY parse of a file whose contents are known byte for byte.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// sgrRangeFixture writes a real recording through the real writer and returns its
// path, its payload total and its frame count.
func sgrRangeFixture(t *testing.T, n int) (string, int64, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rec.screen.stream")
	s, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 100, Rows: 30, Revision: "rev-1"})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	var total int64
	for i := 0; i < n; i++ {
		p := []byte("\x1b[3" + string(rune('0'+i%8)) + "mline-" + string(rune('a'+i%26)) + "\x1b[0m\r\n")
		if werr := s.Write(p); werr != nil {
			t.Fatalf("Write: %v", werr)
		}
		total += int64(len(p))
	}
	if cerr := s.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	return path, total, n
}

// TestSgrRangeParsesEveryRecord is the regression: the walk must recover the
// recording's own counts, and the SOURCE MUST NOT HAVE A DOUBLED LINE — asserted
// on the raw bytes, so the check does not depend on the reader it is judging.
func TestSgrRangeParsesEveryRecord(t *testing.T) {
	path, total, frames := sgrRangeFixture(t, 5)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	head := raw[:bytes.IndexByte(raw, '\n')]
	if bytes.Count(head, []byte(ScreenStreamHeaderPrefix)) != 1 {
		t.Fatalf("the header line repeats its own prefix: %q", head)
	}
	if len(head) > sgrRangeLineMax {
		t.Fatalf("the header line is %d bytes — a record line was doubled", len(head))
	}
	rng, kept, err := ReadSgrRangeFile(path, 1<<20, 1000)
	if err != nil {
		t.Fatalf("ReadSgrRangeFile: %v", err)
	}
	if rng.StreamFrames != frames {
		t.Errorf("the walk found %d frames, the writer wrote %d", rng.StreamFrames, frames)
	}
	if rng.StreamBytes != total {
		t.Errorf("the walk counted %d payload bytes, the writer wrote %d", rng.StreamBytes, total)
	}
	if !rng.SourceTrailer {
		t.Errorf("the walk did not reach the trailer of a CLOSED stream")
	}
	if len(kept) != frames {
		t.Fatalf("kept %d frames, want %d", len(kept), frames)
	}
	if rng.Meta.Grid != [2]int{100, 30} || rng.Meta.Revision != "rev-1" {
		t.Errorf("the header did not decode: grid=%v revision=%q", rng.Meta.Grid, rng.Meta.Revision)
	}
	if rng.Meta.Started == "" {
		t.Errorf("the header's start stamp was not carried — a range artifact would be stamped with the copy's own clock")
	}
}

// TestSgrRangeTakesABoundedTail pins the selection: both bounds apply, the KEPT
// frames are the NEWEST ones, and the range states what it left out.
func TestSgrRangeTakesABoundedTail(t *testing.T) {
	path, total, frames := sgrRangeFixture(t, 40)
	const maxFrames = 5
	rng, kept, err := ReadSgrRangeFile(path, 1<<20, maxFrames)
	if err != nil {
		t.Fatalf("ReadSgrRangeFile: %v", err)
	}
	if len(kept) != maxFrames {
		t.Fatalf("kept %d frames, the frame budget was %d", len(kept), maxFrames)
	}
	if !rng.Truncated || rng.StartFrame != frames-maxFrames {
		t.Errorf("the tail starts at %d of %d (truncated=%v), want the last %d", rng.StartFrame, frames, rng.Truncated, maxFrames)
	}
	if rng.StreamFrames != frames || rng.StreamBytes != total {
		t.Errorf("the range misreports the whole stream: %d frames / %d bytes", rng.StreamFrames, rng.StreamBytes)
	}
	// A BYTE budget, alone, also bounds — and never exceeds it.
	const byteBudget = int64(2 * 16)
	rng2, kept2, err := ReadSgrRangeFile(path, byteBudget, 1000)
	if err != nil {
		t.Fatalf("ReadSgrRangeFile(bytes): %v", err)
	}
	if rng2.Bytes > byteBudget {
		t.Errorf("the byte budget was crossed: kept %d of a %d-byte budget", rng2.Bytes, byteBudget)
	}
	if len(kept2) == 0 {
		t.Errorf("a byte budget that fits one frame kept nothing")
	}
}

// TestSgrRangeArtifactIsAValidStream pins the compositor: what it writes is an
// ordinary stream, readable by the ordinary reader, with the INCLUDED counts.
func TestSgrRangeArtifactIsAValidStream(t *testing.T) {
	path, _, frames := sgrRangeFixture(t, 12)
	rng, kept, err := ReadSgrRangeFile(path, 1<<20, 4)
	if err != nil {
		t.Fatalf("ReadSgrRangeFile: %v", err)
	}
	var buf bytes.Buffer
	written, n, err := WriteSgrPrefix(&buf, rng.Meta, kept)
	if err != nil {
		t.Fatalf("WriteSgrPrefix: %v", err)
	}
	if n != len(kept) || written != rng.Bytes {
		t.Errorf("the compositor wrote %d bytes / %d frames, the range held %d / %d", written, n, rng.Bytes, len(kept))
	}
	meta, back, err := ReadSgrFrames(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("a range artifact is not readable by ReadSgrFrames: %v", err)
	}
	if !meta.Trailer || meta.Frames != n || meta.Bytes != written {
		t.Errorf("the artifact's own trailer says frames=%d bytes=%d trailer=%v, want %d/%d/true",
			meta.Frames, meta.Bytes, meta.Trailer, n, written)
	}
	if len(back) != len(kept) {
		t.Fatalf("the reader recovered %d frames from an artifact of %d", len(back), len(kept))
	}
	for i := range back {
		if !bytes.Equal(back[i], kept[i]) {
			t.Errorf("frame %d differs from what the compositor was given", i)
		}
	}
	if rng.StreamFrames != frames {
		t.Errorf("the source really held %d frames, the range said %d", frames, rng.StreamFrames)
	}
}

// TestSgrRangeRefusesZeroBoundsAndForeignFiles is Rule 15's half: an unbounded
// read and a file that is not this format are REFUSED BY NAME, never silently
// answered with zero frames.
func TestSgrRangeRefusesZeroBoundsAndForeignFiles(t *testing.T) {
	path, _, _ := sgrRangeFixture(t, 2)
	if _, _, err := ReadSgrRangeFile(path, 0, 10); err == nil {
		t.Errorf("a zero byte bound was accepted")
	}
	if _, _, err := ReadSgrRangeFile(path, 10, 0); err == nil {
		t.Errorf("a zero frame bound was accepted")
	}
	foreign := filepath.Join(t.TempDir(), "not-a-stream.txt")
	if err := os.WriteFile(foreign, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadSgrRangeFile(foreign, 100, 10); err == nil {
		t.Errorf("a file with no SGR header was accepted as a stream")
	}
}
