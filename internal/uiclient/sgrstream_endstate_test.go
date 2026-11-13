package uiclient

// sgrstream_endstate_test.go — the THREE endings, measured on files built by the
// production writer (Rule 31: the surface is executed, not described).
//
// Every fixture goes through OpenSgrStream, so a change to the format shows up here
// as a failing test rather than as a hand-rolled stream that agrees with nobody.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// endStateFixture writes a recording and optionally closes it, returning the path.
func endStateFixture(t *testing.T, frames []string, closed bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture"+ScreenStreamSuffix)
	s, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 80, Rows: 24, Revision: "test"})
	if err != nil {
		t.Fatalf("open the fixture stream: %v", err)
	}
	for _, f := range frames {
		if werr := s.Write([]byte(f)); werr != nil {
			t.Fatalf("write fixture frame %q: %v", f, werr)
		}
	}
	if closed {
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("close the fixture stream: %v", cerr)
		}
	}
	return path
}

// TestScanSgrStreamEndTellsTheThreeEndingsApart is the regression guard for bug
// 01a1b823-5070: one boolean (`meta.Trailer`) used to be the whole diagnosis, so
// "the writer never closed" and "the file is cut" and "the file is intact" were
// reported as one finding.
func TestScanSgrStreamEndTellsTheThreeEndingsApart(t *testing.T) {
	t.Run("closed", func(t *testing.T) {
		path := endStateFixture(t, []string{"\x1b[2Jone", "\x1b[2;1Htwo"}, true)
		end, size, err := ScanSgrStreamEndPath(path)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if end.State != SgrStreamEndsWithTrailer {
			t.Fatalf("state = %q, want %q", end.State, SgrStreamEndsWithTrailer)
		}
		if !end.Closed() || end.Contradicts() {
			t.Fatalf("closed=%v contradicts=%v, want true/false", end.Closed(), end.Contradicts())
		}
		if !end.BoundaryWhole() {
			t.Fatalf("cut frame at %d, want %d (a closed stream has no cut frame)", end.CutFrameAt, sgrEndNoOffset)
		}
		if end.Frames != 2 {
			t.Fatalf("frames = %d, want 2", end.Frames)
		}
		if end.TrailerOffset <= 0 || end.TrailerOffset >= size {
			t.Fatalf("trailer offset = %d, want a position inside a %d-byte file", end.TrailerOffset, size)
		}
	})

	t.Run("whole but unclosed", func(t *testing.T) {
		// THE MEASURED SHAPE: the file is not closed, and its last frame ends on its
		// own record boundary. Before this change that combination was reported as
		// `mid-frame` truncation on every one of the 14 streams a census found.
		path := endStateFixture(t, []string{"\x1b[2Jone", "\x1b[2;1Htwo"}, false)
		end, _, err := ScanSgrStreamEndPath(path)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if end.State != SgrStreamEndsUnclosed {
			t.Fatalf("state = %q, want %q", end.State, SgrStreamEndsUnclosed)
		}
		if !end.BoundaryWhole() {
			t.Fatalf("cut frame at %d, want %d: every frame here ran to its full declared length, "+
				"so calling this stream truncated is the misdiagnosis this test exists to end",
				end.CutFrameAt, sgrEndNoOffset)
		}
		if end.TrailerOffset != sgrEndNoOffset {
			t.Fatalf("trailer offset = %d, want the sentinel %d", end.TrailerOffset, sgrEndNoOffset)
		}
		if end.Frames != 2 {
			t.Fatalf("frames = %d, want 2", end.Frames)
		}
	})

	t.Run("cut inside a frame", func(t *testing.T) {
		path := endStateFixture(t, []string{"\x1b[2Jwhole-frame", "\x1b[2;1Hsecond"}, false)
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read the fixture: %v", rerr)
		}
		if werr := os.WriteFile(path, raw[:len(raw)-3], 0o600); werr != nil {
			t.Fatalf("cut the fixture: %v", werr)
		}
		end, _, err := ScanSgrStreamEndPath(path)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if end.State != SgrStreamEndsUnclosed {
			t.Fatalf("state = %q, want %q", end.State, SgrStreamEndsUnclosed)
		}
		if end.BoundaryWhole() {
			t.Fatal("boundary whole = true, want false: the last frame was announced and is not all there")
		}
		if end.CutFrameAt <= 0 {
			t.Fatalf("cut frame at %d, want the offset where that frame begins", end.CutFrameAt)
		}
	})

	t.Run("records after the trailer", func(t *testing.T) {
		path := endStateFixture(t, []string{"\x1b[2Jone"}, true)
		f, oerr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if oerr != nil {
			t.Fatalf("open for the second writer: %v", oerr)
		}
		if _, werr := f.WriteString("\x00\x00trailing"); werr != nil {
			t.Fatalf("append after the close-out record: %v", werr)
		}
		if cerr := f.Close(); cerr != nil {
			t.Fatalf("close the second writer: %v", cerr)
		}
		end, size, err := ScanSgrStreamEndPath(path)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if end.State != SgrStreamEndsContradiction {
			t.Fatalf("state = %q, want %q", end.State, SgrStreamEndsContradiction)
		}
		if len(end.Findings) == 0 {
			t.Fatal("no findings, want at least the after-trailer finding")
		}
		if end.Findings[0].Kind != SgrStreamContradictionKindAfterTrailer {
			t.Fatalf("first finding = %q, want %q", end.Findings[0].Kind, SgrStreamContradictionKindAfterTrailer)
		}
		// The refusal shape is the SAME type the locator and the copier produce, so a
		// caller holding an ending can hand it to a reader expecting a failure.
		cerr := end.ContradictionError(path, size)
		if cerr == nil {
			t.Fatal("ContradictionError = nil, want the shared refusal type")
		}
		if !strings.Contains(cerr.Error(), "contradicts itself") {
			t.Fatalf("refusal = %q, want the file's own sentence", cerr.Error())
		}
	})
}

// TestScanSgrStreamEndRefusesAnUnreadableHeader pins the ONE case that IS an error:
// a file this reader cannot parse is neither "unclosed" nor "contradictory", and
// folding it into either would be the same flattening in a new place.
func TestScanSgrStreamEndRefusesAnUnreadableHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign"+ScreenStreamSuffix)
	if err := os.WriteFile(path, []byte("this file is not what it claims to be\n"), 0o600); err != nil {
		t.Fatalf("write the foreign file: %v", err)
	}
	if _, _, err := ScanSgrStreamEndPath(path); err == nil {
		t.Fatal("scan = nil error, want a refusal naming the unreadable header")
	}
}
