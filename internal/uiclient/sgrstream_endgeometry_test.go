package uiclient

// sgrstream_endgeometry_test.go — the TRAILER's `grid=` is READ, not merely
// written.
//
// The epic's finding: `Close` wrote `grid=` into the closing record
// unconditionally while `ReadSgrFrames` parsed `grid=` from the HEADER only, so
// the trailer's value had no reader at all — a write-only field, and worse, a
// field whose neighbours (`SetGeometry`'s doc comment, `SgrStreamMeta`'s)
// promised a capability nothing implemented: the stream knew the geometry it was
// BUILT at and nothing restated the geometry the run ENDED at.
//
// The fix is the READ shape (the writer's line already carries the value; what
// was missing was a decoder). These tests are its red proof, and ① is written
// FIRST, before the field exists, so the red is the defect itself — a corrupt
// trailer geometry accepted silently — and not a compile error naming a missing
// field:
//
//	① a trailer whose `grid=` does not decode must be refused BY NAME — the
//	   unparseable word appears in the refusal, so an operator sees what the file
//	   said (the shape 01a1a629 established for the other numeric fields);
//	② a trailer that names NO grid is refused by its own sentence, never read as
//	   the zero geometry — `[0 0]` is a REAL value (a width probe that returned
//	   nothing), so it cannot double as the "unreadable" marker;
//	③ a WELL-FORMED trailer's value reaches `meta.EndGrid`, and it differs from
//	   `meta.Grid` when the run moved — which is the whole point of the field:
//	   the pair answers "was this stream recorded across a resize?" and one
//	   number alone cannot.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sgrStreamBlobWithTrailer assembles a stream from the shared word lists in
// sgrstream_meta_test.go, so a trailer case here perturbs the SAME base record
// the field-parsing cases do.
func sgrStreamBlobWithTrailer(trailer []string, frames ...string) []byte {
	var blob bytes.Buffer
	blob.WriteString(sgrStreamRecord(ScreenStreamHeaderPrefix, sgrStreamHeaderWords()))
	if len(frames) == 0 {
		frames = []string{"abcd"}
	}
	for _, p := range frames {
		blob.WriteString(itoa(len(p)) + "\n")
		blob.WriteString(p)
	}
	if trailer != nil {
		blob.WriteString(sgrStreamRecord(ScreenStreamTrailerPrefix, trailer))
	}
	return blob.Bytes()
}

// TestSgrStreamTrailerGridMustParse is the minimum repro: a trailer whose grid=
// cannot be decoded is refused BY NAME, and an ABSENT grid is refused by its own
// sentence instead of becoming [0 0].
func TestSgrStreamTrailerGridMustParse(t *testing.T) {
	cases := []struct {
		name     string
		trailer  []string
		wantWord string
		notClaim string
	}{
		{
			name:     "trailer grid is not a geometry",
			trailer:  kvReplaced(sgrStreamTrailerWords(), "grid", "10"),
			wantWord: "10",
			// The refusal must not print a geometry the file never carried.
			notClaim: "0x0",
		},
		{
			name:     "trailer grid has no separator",
			trailer:  kvReplaced(sgrStreamTrailerWords(), "grid", "wide"),
			wantWord: "wide",
			notClaim: "0x0",
		},
		{
			name:     "trailer names no grid",
			trailer:  kvDropped(sgrStreamTrailerWords(), "grid"),
			wantWord: "grid",
			notClaim: "0x0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blob := sgrStreamBlobWithTrailer(tc.trailer)
			_, _, err := ReadSgrFrames(bytes.NewReader(blob))
			if err == nil {
				t.Fatalf("a trailer geometry the reader could not decode was accepted: the read completed with no error, so a run that moved to an unreadable geometry reads exactly like a clean one — that is the write-only field the epic names")
			}
			if !strings.Contains(err.Error(), tc.wantWord) {
				t.Errorf("refusal does not name %q, so the operator cannot see what the file said: %v", tc.wantWord, err)
			}
			if tc.notClaim != "" && strings.Contains(err.Error(), tc.notClaim) {
				t.Errorf("refusal claims %q — a value the file never carried: %v", tc.notClaim, err)
			}
		})
	}
}

// TestSgrStreamTrailerGridReachesMeta is the field's own test: a well-formed
// trailer's geometry comes back out, and it is NOT the header's — the pair is
// what says "this stream was recorded across a resize".
func TestSgrStreamTrailerGridReachesMeta(t *testing.T) {
	blob := sgrStreamBlobWithTrailer(kvReplaced(sgrStreamTrailerWords(), "grid", "97x41"))
	meta, frames, err := ReadSgrFrames(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("a stream whose trailer names a well-formed geometry was refused: %v", err)
	}
	if !meta.Trailer {
		t.Fatalf("the trailer was not recognized at all")
	}
	if len(frames) != 1 {
		t.Fatalf("read %d frames, want 1", len(frames))
	}
	if meta.Grid != [2]int{10, 2} {
		t.Errorf("header grid = %v, want [10 2] (the geometry the stream was BUILT at)", meta.Grid)
	}
	if meta.EndGrid != [2]int{97, 41} {
		t.Errorf("EndGrid = %v, want [97 41] (the geometry the run ENDED at, as the trailer states it)", meta.EndGrid)
	}
}

// TestSgrStreamEndGridStaysZeroWithoutATrailer pins the TOLERANCE boundary: a
// stream cut mid-frame has no trailer, and its EndGrid must stay at the zero
// value — copying the header's in would make "no trailer" read identically to
// "the run never moved".
func TestSgrStreamEndGridStaysZeroWithoutATrailer(t *testing.T) {
	var blob bytes.Buffer
	blob.WriteString(sgrStreamRecord(ScreenStreamHeaderPrefix, sgrStreamHeaderWords()))
	blob.WriteString("16\nabcd") // announces 16 bytes, delivers 4
	meta, frames, err := ReadSgrFrames(bytes.NewReader(blob.Bytes()))
	if err != nil {
		t.Fatalf("a truncated stream must still read: %v", err)
	}
	if meta.Trailer {
		t.Fatalf("a stream with no trailer reported one")
	}
	if len(frames) != 0 {
		t.Fatalf("read %d completed frames from a stream truncated inside its only frame", len(frames))
	}
	if meta.EndGrid != [2]int{} {
		t.Errorf("EndGrid = %v on a stream with no trailer, want the zero value — there is no ended-at geometry to state", meta.EndGrid)
	}
}

// TestSgrStreamEndGridRecordsAResize drives the PRODUCTION loop rather than a
// hand-built blob: a real sink, armed with a real stream, is moved to a new
// geometry through SetGeometry — the ONE write path after construction — and the
// value has to come back out of the artifact. This is the sentence
// SetGeometry's own doc comment makes ("the trailer names the grid the run ENDED
// at … the two together answer 'was this stream recorded across a resize?'"),
// and before the trailer's grid had a reader, nothing upstream of this test made
// that sentence true.
func TestSgrStreamEndGridRecordsAResize(t *testing.T) {
	dir := t.TempDir()
	streamPath := filepath.Join(dir, "resize"+ScreenStreamSuffix)
	s, err := OpenSgrStream(SgrStreamConfig{Path: streamPath, Cols: 20, Rows: 3})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	d, err := NewScreenDumper(DumpConfig{
		Path:     filepath.Join(dir, "chat-screen.dump"),
		Cols:     20,
		Rows:     3,
		Sgr:      s,
		Identity: DumpIdentity{Surface: SurfaceChat, PID: 4, ColsRows: [2]int{20, 3}, TerminalColsRows: [2]int{20, 3}},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper: %v", err)
	}
	if _, err := d.Write([]byte("\x1b[Halpha")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	d.SetGeometry(GeometrySourceLayout, 30, 5)
	if _, err := d.Write([]byte("\x1b[2;1Hbeta")); err != nil {
		t.Fatalf("Write after the resize: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	blob, err := os.ReadFile(streamPath)
	if err != nil {
		t.Fatalf("read the stream: %v", err)
	}
	meta, frames, err := ReadSgrFrames(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("ReadSgrFrames: %v (%d bytes on disk)", err, len(blob))
	}
	if !meta.Trailer {
		t.Fatalf("the closed stream has no trailer")
	}
	if len(frames) != 2 {
		t.Fatalf("recorded %d frames, painted 2", len(frames))
	}
	if meta.Grid != [2]int{20, 3} {
		t.Errorf("header grid = %v, want [20 3] (the geometry the sink was BUILT at)", meta.Grid)
	}
	if meta.EndGrid != [2]int{30, 5} {
		t.Errorf("EndGrid = %v after SetGeometry(layout, 30, 5), want [30 5] — the trailer's grid= was written and never read, so the stream could not say it was recorded across a resize", meta.EndGrid)
	}
}
