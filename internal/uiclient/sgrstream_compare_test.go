package uiclient

// sgrstream_compare_test.go — the `--verify` measurement's own proof.
//
// Three arms, each a DIFFERENT verdict a caller acts on, and each built by the
// REAL writer (OpenSgrStream / NewScreenDumper) rather than by hand-assembled
// bytes: a comparison tested against a grammar this package invented would prove
// nothing about the files a run actually leaves behind.
//
//   1. two recordings of the SAME paints → Identical() == true, and the answer is
//      `first divergence: none` (the sentence comes from the one predicate);
//   2. one paint changed → the FIRST differing frame, with an offset that points
//      at the PAYLOAD (checked by seeking the file to it and reading the same
//      bytes back — an offset off by the digits of the length would fail here);
//   3. one recording stops earlier → that IS the divergence, named as the
//      remaining frames, never reported as "identical".

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordSgrStream writes one recording of the given paints at cols×rows and
// returns its path. It is the REAL producer: a screen dumper with the raw-stream
// leg attached, so the file carries whatever header/frame shape the writer emits.
func recordSgrStream(t *testing.T, dir, name string, cols, rows int, paints [][]byte) string {
	t.Helper()
	path := filepath.Join(dir, name+ScreenStreamSuffix)
	s, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: cols, Rows: rows})
	if err != nil {
		t.Fatalf("OpenSgrStream(%s): %v", name, err)
	}
	d, err := NewScreenDumper(DumpConfig{
		Path:     filepath.Join(dir, name+"-chat-screen.dump"),
		Cols:     cols,
		Rows:     rows,
		Sgr:      s,
		Identity: DumpIdentity{Surface: SurfaceChat, PID: 7, ColsRows: [2]int{cols, rows}, TerminalColsRows: [2]int{cols, rows}},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper(%s): %v", name, err)
	}
	d.now = fixedClock()
	for _, p := range paints {
		if _, werr := d.Write(p); werr != nil {
			t.Fatalf("Write(%s): %v", name, werr)
		}
	}
	if cerr := d.Close(); cerr != nil {
		t.Fatalf("Close(%s): %v", name, cerr)
	}
	return path
}

// TestSgrStreamCompareIdentical pins the GREEN: two recordings of the same paints
// are byte-identical and the ONE predicate says so.
func TestSgrStreamCompareIdentical(t *testing.T) {
	dir := t.TempDir()
	paints := [][]byte{
		[]byte("\x1b[Hone\x1b[1;31m two\x1b[0m"),
		[]byte("\x1b[2;1Hthree"),
	}
	a := recordSgrStream(t, dir, "a", 20, 4, paints)
	b := recordSgrStream(t, dir, "b", 20, 4, paints)

	cmp, err := CompareSgrStreams(a, b)
	if err != nil {
		t.Fatalf("CompareSgrStreams: %v", err)
	}
	if !cmp.Identical() {
		t.Fatalf("the same paints compared as different: frame %d (%s)", cmp.First.Frame, cmp.First.Reason)
	}
	if cmp.FramesA != len(paints) || cmp.FramesB != len(paints) {
		t.Fatalf("frames: A=%d B=%d, want %d each", cmp.FramesA, cmp.FramesB, len(paints))
	}
	if cmp.BytesA == 0 || cmp.BytesA != cmp.BytesB {
		t.Fatalf("payload bytes: A=%d B=%d, want equal and non-zero", cmp.BytesA, cmp.BytesB)
	}
}

// TestSgrStreamCompareNamesTheFirstDifferingFrame is the RED, and it is the arm
// that matters: a comparison that can only say "identical" would pass the green
// test above while being useless.
func TestSgrStreamCompareNamesTheFirstDifferingFrame(t *testing.T) {
	dir := t.TempDir()
	base := [][]byte{
		[]byte("\x1b[Hone"),
		[]byte("\x1b[2;1Htwo"),
		[]byte("\x1b[3;1Hthree"),
	}
	mutated := [][]byte{
		[]byte("\x1b[Hone"),
		[]byte("\x1b[2;1HTWO"), // frame 2 differs
		[]byte("\x1b[3;1Hthree"),
	}
	a := recordSgrStream(t, dir, "a", 16, 4, base)
	b := recordSgrStream(t, dir, "b", 16, 4, mutated)

	cmp, err := CompareSgrStreams(a, b)
	if err != nil {
		t.Fatalf("CompareSgrStreams: %v", err)
	}
	if cmp.Identical() {
		t.Fatal("a changed paint compared as identical — the measurement would fail open")
	}
	d := cmp.First
	if d.Frame != 2 {
		t.Fatalf("first divergence at frame %d, want 2 (%s)", d.Frame, d.Reason)
	}
	if d.OffsetA < 0 || d.OffsetB < 0 {
		t.Fatalf("offsets not reported: A=%d B=%d", d.OffsetA, d.OffsetB)
	}

	// THE OFFSET MUST POINT AT THE PAYLOAD, not at the length prefix or the newline
	// before it. Seeking the file to OffsetA and reading len(d.A) bytes has to give
	// d.A back byte for byte; an offset that named the frame's prefix line would
	// return digits here.
	f, oerr := os.Open(a)
	if oerr != nil {
		t.Fatalf("open %s: %v", a, oerr)
	}
	defer f.Close() //stylegate:ignore a read-only handle
	got := make([]byte, len(d.A))
	if _, rerr := f.ReadAt(got, d.OffsetA); rerr != nil {
		t.Fatalf("ReadAt(%d): %v", d.OffsetA, rerr)
	}
	if !bytes.Equal(got, d.A) {
		t.Fatalf("offset %d does not point at the payload: read %q, reported %q", d.OffsetA, got, d.A)
	}
	if !bytes.Contains(d.A, []byte("two")) {
		t.Fatalf("side A's payload is %q, want the frame's paint bytes", d.A)
	}
	if !bytes.Contains(d.B, []byte("TWO")) {
		t.Fatalf("side B's payload is %q, want the mutated paint bytes", d.B)
	}
	if !strings.Contains(d.Reason, "byte") {
		t.Fatalf("reason %q does not say what kind of difference it is", d.Reason)
	}
}

// TestSgrStreamCompareReportsTheShorterStream pins the third verdict: a recording
// that stops earlier is a divergence, not a prefix match. This is the shape a
// regression takes when a run dies before its last paint.
func TestSgrStreamCompareReportsTheShorterStream(t *testing.T) {
	dir := t.TempDir()
	full := [][]byte{
		[]byte("\x1b[Hone"),
		[]byte("\x1b[2;1Htwo"),
		[]byte("\x1b[3;1Hthree"),
	}
	short := full[:2]
	a := recordSgrStream(t, dir, "a", 16, 4, full)
	b := recordSgrStream(t, dir, "b", 16, 4, short)

	cmp, err := CompareSgrStreams(a, b)
	if err != nil {
		t.Fatalf("CompareSgrStreams: %v", err)
	}
	if cmp.Identical() {
		t.Fatal("a stream that stops one frame earlier compared as identical")
	}
	d := cmp.First
	if d.Frame != 3 {
		t.Fatalf("first divergence at frame %d, want 3 (%s)", d.Frame, d.Reason)
	}
	if d.OffsetB != sgrFrameNoOffset {
		t.Fatalf("side B has no frame 3 but reported offset %d", d.OffsetB)
	}
	if !strings.Contains(d.Reason, "A continues") {
		t.Fatalf("reason %q does not name which side ran on", d.Reason)
	}
}

// TestSgrStreamCompareRefusesAnUnreadableFile: "no divergence" must never be the
// answer for a file that could not be read (Rule 15 — the measurement would fail
// open, and the one question this walk exists for would be answered wrongly).
func TestSgrStreamCompareRefusesAnUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	good := recordSgrStream(t, dir, "good", 10, 3, [][]byte{[]byte("\x1b[Hon")})
	if _, err := CompareSgrStreams(good, filepath.Join(dir, "absent"+ScreenStreamSuffix)); err == nil {
		t.Fatal("comparing against a missing file returned no error — the verdict would be unfounded")
	}
	empty := filepath.Join(dir, "empty"+ScreenStreamSuffix)
	if werr := os.WriteFile(empty, nil, 0o600); werr != nil {
		t.Fatalf("write the empty fixture: %v", werr)
	}
	if _, err := CompareSgrStreams(good, empty); err == nil {
		t.Fatal("comparing against an empty file returned no error")
	}
	if _, err := CompareSgrStreams(good, good); err != nil {
		t.Fatalf("self-comparison failed: %v", err)
	}
}

// TestSgrStreamSuffixesAcceptThePreRenameName pins that the READ side still sees a
// `.sgr.stream` — the 177 files this host recorded before the rename are
// real data, and a resolver that only asked for the new suffix would hide them.
func TestSgrStreamSuffixesAcceptThePreRenameName(t *testing.T) {
	for _, suf := range ScreenStreamSuffixes {
		if _, ok := ScreenStreamSuffixOf("session-1" + suf); !ok {
			t.Fatalf("ScreenStreamSuffixOf does not accept %q", suf)
		}
	}
	if _, ok := ScreenStreamSuffixOf("session-1.txt"); ok {
		t.Fatal("ScreenStreamSuffixOf accepted a .txt path")
	}
	if _, ok := ScreenStreamSuffixOf("session-1" + LegacyScreenStreamSuffix); !ok {
		t.Fatal("the pre-rename suffix is not accepted by the ONE suffix list")
	}
}

// TestSgrStreamReaderAcceptsBothHeaderSpellings: the header check goes through
// IsSgrStreamHeaderLine, so a pre-rename file parses exactly as a new one does —
// and the reading that says WHICH generation wrote it stays available.
func TestSgrStreamReaderAcceptsBothHeaderSpellings(t *testing.T) {
	dir := t.TempDir()
	paints := [][]byte{[]byte("\x1b[Hone")}
	path := recordSgrStream(t, dir, "a", 10, 3, paints)
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	first, _, _ := strings.Cut(string(blob), "\n")
	if !IsSgrStreamHeaderLine(first) {
		t.Fatalf("the writer's own header is not accepted by the reader: %q", first)
	}
	if !strings.HasPrefix(first, ScreenStreamHeaderPrefix) {
		t.Fatalf("new recordings write %q, want the %s header", first, ScreenStreamHeaderPrefix)
	}
	legacy := strings.Replace(first, ScreenStreamHeaderPrefix, LegacyScreenStreamHeaderPrefix, 1)
	if !IsSgrStreamHeaderLine(legacy) {
		t.Fatalf("a pre-rename header is refused: %q", legacy)
	}
	if !SgrStreamHeaderIsLegacy(legacy) {
		t.Fatalf("a pre-rename header is not REPORTED as legacy: %q", legacy)
	}
	if SgrStreamHeaderIsLegacy(first) {
		t.Fatalf("a current header is reported as legacy: %q", first)
	}
	if !IsSgrStreamTrailerLine(ScreenStreamTrailerPrefix + " frames=1") {
		t.Fatal("the trailer reader refused the current spelling")
	}
	if !IsSgrStreamTrailerLine(LegacyScreenStreamTrailerPrefix + " frames=1") {
		t.Fatal("the trailer reader refused the pre-rename spelling")
	}
}
