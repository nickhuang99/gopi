package uiclient

// sgrstream_integrity_test.go — the gates for the two contradictions of a stream
// whose own records disagree with its own length.
//
// THE DEFECT THESE PIN (measured 2026-11-07, task 01a1b336-4160). A 38 MB
// recording's own bytes were
//
//	header … grid=0x0 …  |  trailer frames=0 bytes=0  |  ~38 MB (mostly NUL)  |
//	frames written by the FIRST writer  |  a SECOND, true trailer frames=885963
//
// — two writers on one path, the second having truncated it (OpenSgrStream opens
// O_WRONLY|O_CREATE|O_TRUNC) while the first kept appending at its old fd offsets,
// the kernel filling the gap with NULs. The reader stopped at the first trailer,
// reported zero frames, and the only sentence its caller had was "the anchor does
// not match the real renderer" — a statement about the LOCATOR produced by a file
// whose records contradict each other. These tests build the MINIMAL version of
// that structure (a few hundred bytes instead of 38 MB) and assert the refusal
// names the file and never the renderer.
//
// THE FIXTURE IS WRITTEN BY HAND, NOT BY OpenSgrStream: the writer cannot produce
// this file on purpose, and a fixture that went through it could only reproduce the
// shape by accident.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	// sgrSparseFixtureHole is the NUL run the sparse fixture carries between the
	// truncated writer's trailer and the original writer's frames. It is a few
	// kilobytes rather than the measured 38 MB: the finding is arithmetic on the
	// offsets and the size, so a large hole tests nothing a small one does not.
	sgrSparseFixtureHole = 4096
	// sgrSparseFixtureFrames is how many real frames follow the hole.
	sgrSparseFixtureFrames = 3
)

// sgrSparseFixture builds the minimal two-writer stream on disk and returns its
// path plus the offsets the assertions need.
//
// The layout, in this order, is the measured one:
//
//	0                        header (grid=0x0, as the truncated writer left it)
//	len(header)              trailer frames=0 bytes=0   ← the second writer's
//	len(header+trailer)      a NUL hole
//	…                        <n>\n<payload> frames      ← the first writer's
//	…                        a true trailer with the real counts
func sgrSparseFixture(t *testing.T) (path string, holeOffset int64, trueTrailerOffset int64) {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString(sgrHeaderLine([2]int{0, 0}, "", "2026-11-06T00:06:10Z"))
	buf.WriteString(sgrTrailerLine(0, 0, [2]int{0, 0}))
	holeOffset = int64(buf.Len())
	buf.Write(make([]byte, sgrSparseFixtureHole))
	var payload int64
	for i := 0; i < sgrSparseFixtureFrames; i++ {
		frame := []byte("\x1b[22;1Hframe-" + strconv.Itoa(i) + "\r\n")
		buf.WriteString(strconv.Itoa(len(frame)) + "\n")
		buf.Write(frame)
		payload += int64(len(frame))
	}
	trueTrailerOffset = int64(buf.Len())
	buf.WriteString(sgrTrailerLine(sgrSparseFixtureFrames, payload, [2]int{117, 24}))
	path = filepath.Join(t.TempDir(), "two-writers"+ScreenStreamSuffix)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write the two-writer fixture: %v", err)
	}
	return path, holeOffset, trueTrailerOffset
}

// TestSgrTurnLocatorNamesAContradictoryStreamAsAFileDefect is the regression: the
// minimal two-writer structure must be refused BY THE FILE, with numbers, and the
// refusal must not contain a word about the anchor or the renderer.
//
// THE ASSERTION ON THE WORDING IS THE POINT, not decoration. The old output of this
// exact shape was "no turn was located … the anchor does not match the real
// renderer", and that sentence is what sent an operator to read renderer code over
// a file two writers had interleaved.
func TestSgrTurnLocatorNamesAContradictoryStreamAsAFileDefect(t *testing.T) {
	path, holeOffset, _ := sgrSparseFixture(t)
	size := int64(0)
	if st, err := os.Stat(path); err == nil {
		size = st.Size()
	}
	_, err := LocateSgrTurns(path)
	if err == nil {
		t.Fatalf("LocateSgrTurns accepted a stream whose trailer declares frames=0 and that carries %d byte(s) more — the contradiction must be refused", size)
	}
	var contradiction *SgrStreamContradictionError
	if !errors.As(err, &contradiction) {
		t.Fatalf("LocateSgrTurns reported %v (%T); a stream contradicting its own length must be a *SgrStreamContradictionError so a caller can tell it from a parse failure", err, err)
	}
	if len(contradiction.Findings) == 0 {
		t.Fatalf("the refusal carries no finding; the caller has no offset to act on")
	}
	first := contradiction.Findings[0]
	if first.Kind != SgrStreamContradictionKindAfterTrailer {
		t.Fatalf("the first finding is %q, want %q — the trailer-then-data shape is what this fixture is", first.Kind, SgrStreamContradictionKindAfterTrailer)
	}
	if first.Offset != holeOffset {
		t.Errorf("the finding points at offset %d, want %d (the first byte after the truncated writer's trailer)", first.Offset, holeOffset)
	}
	if want := size - holeOffset; first.Size != want {
		t.Errorf("the finding covers %d byte(s), want %d (everything after the trailer)", first.Size, want)
	}
	if !strings.Contains(first.Detail, "ALL NUL") {
		t.Errorf("the finding does not say the surplus is a NUL hole: %q", first.Detail)
	}
	if contradiction.Path != path {
		t.Errorf("the refusal names %q, not the stream it read (%q)", contradiction.Path, path)
	}
	if contradiction.Size != size {
		t.Errorf("the refusal states the stream is %d byte(s); the file is %d", contradiction.Size, size)
	}
	msg := err.Error()
	for _, banned := range []string{"anchor", "renderer", "the real renderer"} {
		if strings.Contains(strings.ToLower(msg), banned) {
			t.Errorf("the refusal mentions %q — a file that contradicts itself says nothing about the anchor or the renderer: %s", banned, msg)
		}
	}
	if !strings.Contains(msg, strconv.FormatInt(holeOffset, 10)) {
		t.Errorf("the refusal carries no offset: %s", msg)
	}
	t.Logf("named refusal: %s", msg)
}

// TestSgrTurnLocatorKeepsATrailerAtTheEndLegal is the control the fixture above
// needs: the SAME frames in a stream whose trailer really IS last are healthy, and
// the walk must return them. Without this arm a predicate that refused every
// trailer would pass the regression test.
func TestSgrTurnLocatorKeepsATrailerAtTheEndLegal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed"+ScreenStreamSuffix)
	s, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 117, Rows: 24, Revision: "test"})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	for i := 0; i < sgrSparseFixtureFrames; i++ {
		if werr := s.Write([]byte("\x1b[22;1Hframe-" + strconv.Itoa(i) + "\r\n")); werr != nil {
			t.Fatalf("Write: %v", werr)
		}
	}
	if cerr := s.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	turns, err := LocateSgrTurns(path)
	if err != nil {
		t.Fatalf("a closed stream whose trailer is the last record was refused: %v", err)
	}
	if turns.StreamFrames != sgrSparseFixtureFrames {
		t.Errorf("the clean stream parsed as %d frame(s), want %d", turns.StreamFrames, sgrSparseFixtureFrames)
	}
	if !turns.Trailer {
		t.Errorf("the clean stream's trailer was not read")
	}
}

// TestSgrRangeNamesTheSameContradiction pins that the RANGE reader refuses the same
// file for the same reason. Two entry points, one rule (Rule 14): a `/screen`
// capture goes through the range walk, and a defect that only one of them reported
// would reach the operator as a working command over a broken file.
func TestSgrRangeNamesTheSameContradiction(t *testing.T) {
	path, holeOffset, _ := sgrSparseFixture(t)
	_, _, err := ReadSgrRangeFile(path, 1<<20, 1000)
	var contradiction *SgrStreamContradictionError
	if !errors.As(err, &contradiction) {
		t.Fatalf("ReadSgrRangeFile reported %v (%T); the same contradiction must be named by the range walk too", err, err)
	}
	if len(contradiction.Findings) == 0 || contradiction.Findings[0].Offset != holeOffset {
		t.Errorf("the range walk's finding is %+v, want offset %d", contradiction.Findings, holeOffset)
	}
	// THE FILE READER OWES ITS CALLER THE NAME. ReadSgrRange holds only an
	// io.ReaderAt, so a contradiction it raises cannot name a file unless the
	// path-aware wrapper puts it back — and a refusal that says "the SGR stream
	// contradicts itself" without saying WHICH stream is not actionable.
	if contradiction.Path != path {
		t.Errorf("the file reader's refusal names %q, not the stream it read (%q)", contradiction.Path, path)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the refusal text does not contain the path: %s", err.Error())
	}
}

// sgrBadRecordFixture writes a stream whose frame area carries a COMPLETE record
// line that is neither a trailer nor a decimal length: bytes a second writer left
// where the original writer's frames were expected.
func sgrBadRecordFixture(t *testing.T, record string) string {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString(sgrHeaderLine([2]int{117, 24}, "test", "2026-11-06T00:06:10Z"))
	frame := []byte("\x1b[22;1Hframe\r\n")
	buf.WriteString(strconv.Itoa(len(frame)) + "\n")
	buf.Write(frame)
	buf.WriteString(record + "\n")
	path := filepath.Join(t.TempDir(), "damaged-record"+ScreenStreamSuffix)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write the damaged-record fixture: %v", err)
	}
	return path
}

// TestSgrBadRecordIsNamedAsDamageAndNotAsAPrefixComplaint pins the second finding.
//
// The old sentence — "SGR stream frame prefix %q is not a length in 0..1073741824"
// — described the PREFIX grammar and said nothing about the file being in two
// pieces, and with a NUL-run record it quoted thousands of unreadable bytes back at
// its reader. Both shapes are asserted here.
func TestSgrBadRecordIsNamedAsDamageAndNotAsAPrefixComplaint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record string
		want   string
	}{
		{name: "nul run", record: strings.Repeat("\x00", 64), want: "sparse hole"},
		{name: "garbage text", record: "NOT-A-LENGTH", want: "damaged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := sgrBadRecordFixture(t, tc.record)
			_, err := LocateSgrTurns(path)
			var contradiction *SgrStreamContradictionError
			if !errors.As(err, &contradiction) {
				t.Fatalf("LocateSgrTurns reported %v (%T); an unreadable complete record is a file contradiction", err, err)
			}
			if contradiction.Findings[0].Kind != SgrStreamContradictionKindBadRecord {
				t.Fatalf("kind is %q, want %q", contradiction.Findings[0].Kind, SgrStreamContradictionKindBadRecord)
			}
			msg := err.Error()
			if strings.Contains(msg, "frame prefix") {
				t.Errorf("the refusal still complains about the prefix grammar instead of naming the damage: %s", msg)
			}
			if !strings.Contains(msg, tc.want) {
				t.Errorf("the refusal does not contain %q: %s", tc.want, msg)
			}
			if len(msg) > 2000 {
				t.Errorf("the refusal is %d bytes — a damaged record must not flood the message it appears in", len(msg))
			}
		})
	}
}

// TestSgrOldStreamWithoutTurnsStaysSilent is the LEGAL state the contradiction
// refusal must not swallow: a stream with frames and no turn header is old, not
// broken, and its answer is an empty span list so the CALLER can name the turn
// range that would have worked.
func TestSgrOldStreamWithoutTurnsStaysSilent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-turns"+ScreenStreamSuffix)
	s, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 80, Rows: 24, Revision: "test"})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	if werr := s.Write([]byte("\x1b[1;1Hplain text, no turn header\r\n")); werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	if cerr := s.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	turns, err := LocateSgrTurns(path)
	if err != nil {
		t.Fatalf("a stream with frames and no turn header was refused: %v — that is an old stream, not a contradictory one", err)
	}
	if turns.StreamFrames != 1 {
		t.Errorf("the old stream parsed as %d frame(s), want 1", turns.StreamFrames)
	}
	if len(turns.Spans) != 0 {
		t.Errorf("an old stream produced %d span(s); with no header the answer is an empty list", len(turns.Spans))
	}
	if !turns.Trailer {
		t.Errorf("the old stream's own trailer was not read")
	}
}

// TestSgrTrailerCountsMustMatchTheFramesRead pins the SECOND measured damage path:
// a stream whose trailer really IS last but whose own counts disagree with the
// frames before it.
//
// The measured case (2026-11-06, same artifact path): a recording went from
// 9,543,867 bytes / 60,090 frames to 46,125 bytes with every turn header gone —
// the file was rewritten in place under its readers, and what remained no longer
// matched the counts the surviving bytes account for. A reader that accepted the
// trailer on its own word reported a healthy stream over a file whose content had
// been replaced, and the missing turns then looked like a locator regression.
func TestSgrTrailerCountsMustMatchTheFramesRead(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(sgrHeaderLine([2]int{117, 24}, "test", "2026-11-06T00:06:10Z"))
	var payload int64
	for i := 0; i < sgrSparseFixtureFrames; i++ {
		frame := []byte("\x1b[22;1Hframe-" + strconv.Itoa(i) + "\r\n")
		buf.WriteString(strconv.Itoa(len(frame)) + "\n")
		buf.Write(frame)
		payload += int64(len(frame))
	}
	liar := sgrSparseFixtureFrames + 7
	buf.WriteString(sgrTrailerLine(liar, payload, [2]int{117, 24}))
	path := filepath.Join(t.TempDir(), "rewritten"+ScreenStreamSuffix)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write the rewritten-in-place fixture: %v", err)
	}
	_, err := LocateSgrTurns(path)
	var contradiction *SgrStreamContradictionError
	if !errors.As(err, &contradiction) {
		t.Fatalf("LocateSgrTurns reported %v (%T); a trailer claiming %d frames over %d readable ones is a file contradiction", err, err, liar, sgrSparseFixtureFrames)
	}
	got := contradiction.Findings[0]
	if got.Kind != SgrStreamContradictionKindCountMismatch {
		t.Fatalf("kind is %q, want %q", got.Kind, SgrStreamContradictionKindCountMismatch)
	}
	if !strings.Contains(got.Detail, strconv.Itoa(liar)) || !strings.Contains(got.Detail, strconv.Itoa(sgrSparseFixtureFrames)) {
		t.Errorf("the finding does not state both numbers (declared %d, read %d): %q", liar, sgrSparseFixtureFrames, got.Detail)
	}
	t.Logf("named refusal: %v", err)
}
