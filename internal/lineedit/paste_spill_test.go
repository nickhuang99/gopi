package lineedit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/inputsource"
)

// paste_spill_test.go — the two coupled defects of pasting into the chat line
// editor, measured at the editor's own seams.
//
// (A) THE COST. The buffer used to be grown one rune at a time, so inserting N
// runes cost O(N²) — measured on this host: 4 KiB of one-at-a-time input 61ms,
// 16 KiB 936ms, 64 KiB 23.0s — and every 4 KiB read then repainted the whole
// (now huge) buffer. The regression test below asserts the WALL TIME of the
// editor's own read loop for a paste a terminal really produces (a sequence of
// full read chunks), so it fails on the pre-fix tree by timing out its budget
// and passes after — a RED proof, not a callback count.
//
// (B) THE SPILL. A read that fills the read buffer (editReadBuf = 4096 bytes)
// cannot be typing, so its bytes are written to <session>/paste/paste-<stamp>-
// <n>.txt and replaced on the input line by a reference. The assertions are on
// the FILE'S BYTES (equal to the pasted bytes, for content no text path could
// represent: NUL, CR/LF, ESC, control bytes, invalid UTF-8) and on the RENDERED
// CELL GRID (the editing row carries the reference, not the paste).

// pasteTestStamp is the fixed instant the spill names are stamped with (the
// tests assert the NAME SHAPE, so the clock is pinned rather than mocked).
const pasteTestStamp = 1_700_000_000

// shortPasteSlack is the room a "short paste" fixture leaves under the threshold
// for its own brackets and line break: the fixture's whole READ must stay below
// PasteThreshold, or it is a long paste by definition.
const shortPasteSlack = 32

// bracketedPasteStart / bracketedPasteEnd are the paste markers as a terminal
// writes them. They are the package's OWN wire spelling (PasteStartSeq /
// PasteEndSeq — the same values the drop rule matches on, composed through
// internal/sgr), so a change to the markers cannot leave this file asserting an
// obsolete shape.
var (
	bracketedPasteStart = PasteStartSeq
	bracketedPasteEnd   = PasteEndSeq
)

// pasteTestCols is the terminal width the spill tests render at.
const pasteTestCols = 80

// pasteTestRows is the terminal height the spill tests render at.
const pasteTestRows = 24

// newPasteStoreForTest builds a store over a directory the TEST owns (under the
// suite's scratch root, which TestMain pins off `/`), with the name clock pinned.
func newPasteStoreForTest(t *testing.T) *PasteStore {
	t.Helper()
	// The HUMAN face, the same provenance the chat input path's store carries
	// (cmd/agent/lineedit_wire.go): every reader below asserts against the
	// reference the production editor would render.
	s := NewPasteStoreFor(t.TempDir(), inputsource.Human)
	s.now = func() time.Time { return time.Unix(pasteTestStamp, 0) }
	return s
}

// pasteBytes builds n bytes whose content exercises every path a text-only
// handling would lose: NUL, CR, LF, ESC and a CSI, a BEL, a DEL, and two byte
// sequences that are NOT valid UTF-8.
func pasteBytes(n int) []byte {
	raw := []byte{0x00, '\r', '\n', 0x1b, '[', 'A', 0x07, 0x7f, 0xff, 0xfe, 0xc3, 0x28}
	out := make([]byte, 0, n)
	for len(out) < n {
		out = append(out, raw...)
	}
	return out[:n]
}

// TestPasteBulkSpill_LongPasteCostsOneSplicePerReadNotOnePerRune is the RED-
// proved regression test for defect (A).
//
// It drives the REAL read loop with a paste shaped the way a terminal delivers
// one (full read chunks), and it asserts three things at once, because the defect
// was three things: the wall time (the O(N²) splice), the size of the buffer (the
// whole paste used to land in it), and the screen (one reference row, not 3000
// wrapped rows).
func TestPasteBulkSpill_LongPasteCostsOneSplicePerReadNotOnePerRune(t *testing.T) {
	store := newPasteStoreForTest(t)
	data := pasteBytes(pasteCostChunks * pasteChunkBytes)

	steps := make([]scriptStep, 0, pasteCostChunks+1)
	for i := 0; i < pasteCostChunks; i++ {
		chunk := data[i*pasteChunkBytes : (i+1)*pasteChunkBytes]
		steps = append(steps, scriptStep{data: chunk})
	}
	steps = append(steps, scriptStep{data: []byte("\r"), advance: submitGap})

	ed, _, _, out := newScriptedEditor(t, steps, pasteTestCols, nil)
	ed.SetPasteStore(store)

	start := time.Now()
	res := runScript(t, ed)
	elapsed := time.Since(start)

	if elapsed > pasteCostBudget {
		t.Fatalf("the editor spent %v inside its read loop for a %d-byte paste (budget %v) — the buffer is still being "+
			"grown per rune and/or the whole paste is being re-laid-out per read; pre-fix this same work measured 23.0s at "+
			"64 KiB of one-at-a-time input. Pasted input freezes the prompt for exactly this long.",
			elapsed, len(data), pasteCostBudget)
	}
	if len(res) != 1 {
		t.Fatalf("submitted lines = %d, want 1 (the paste is spilled, so no pasted byte submits)", len(res))
	}
	// THE BUFFER STAYS SMALL: the whole paste left the line and only a reference
	// stands for it, so the line is a few hundred bytes instead of 256 KiB.
	if got := utf8.RuneCountInString(res[0].Line); got > pasteRefRunesMax {
		t.Fatalf("the submitted line holds %d runes — the paste itself is in the buffer; it must be replaced by a "+
			"short reference (line starts %q)", got, firstLine(res[0].Line))
	}
	if strings.ContainsRune(res[0].Line, 0x00) {
		t.Fatalf("the submitted line carries a NUL from the pasted bytes: %q", firstLine(res[0].Line))
	}
	// ONE paste, ONE file: the continued reads are appended, byte for byte, and
	// the file holds the whole 256 KiB (the continuation arm is asserted on its own
	// in TestPasteBulkSpill_ContinuationReadsJoinOneFile).
	files, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatalf("the paste directory was not created: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("spilled files = %d, want 1 (one paste = one file)", len(files))
	}
	spilled, err := os.ReadFile(filepath.Join(store.Dir(), files[0].Name()))
	if err != nil {
		t.Fatalf("read the spill: %v", err)
	}
	if !bytes.Equal(spilled, data) {
		t.Fatalf("the spill holds %d bytes, the paste was %d (first difference at %d)", len(spilled), len(data),
			firstDifference(spilled, data))
	}
	// THE SCREEN: the editing line shows the references, and the pasted bytes are
	// nowhere on it. The WHOLE stream is replayed (this run ends at EOF, so there
	// is no submit write to drop).
	//
	// The line is far wider than the terminal, so the renderer scrolls it and the
	// row carrying the PROMPT may have moved off the top — the assertion looks
	// for that row instead of assuming row 0, and it is the row a user reads.
	sc := wholeStreamScreen(t, out, pasteTestCols, pasteTestRows)
	// What is on the screen is the REFERENCES (the line is 64 of them, taller than
	// this 24-row terminal, so the grid holds whichever of their rows were drawn
	// last — the marker is what identifies them).
	if !strings.Contains(sc.Contents(), pasteRefOpen) {
		t.Fatalf("the painted screen carries no reference marker %q; the editing line is not showing where the paste "+
			"went:\n%s", pasteRefOpen, sc.Contents())
	}
	if strings.Contains(sc.Contents(), "\x1b[") {
		// The paste's own ESC bytes must never reach the screen: they went to the
		// file, not through the key decoder.
		t.Fatalf("the pasted ESC bytes were drawn on the screen:\n%s", sc.Contents())
	}
}

// rowStartingWith returns the first painted row whose text begins with prefix.
func rowStartingWith(sc *Screen, prefix string) (string, bool) {
	for r := 0; r < sc.Rows; r++ {
		if line := sc.Line(r); strings.HasPrefix(line, prefix) {
			return line, true
		}
	}
	return "", false
}

// pasteRefRunesMax bounds the runes one reference may occupy in a submitted line
// (a slack on top of the rendered form, which the other tests pin exactly). It
// exists so the "the buffer stays small" assertion above is a comparison and not
// a guess about the temp path's length.
const pasteRefRunesMax = 200

// pasteRefOpen is the reference's opening marker, spelled from the ONE definition
// the renderer uses (Rule 13) so the assertion cannot drift from the rendering.
var pasteRefOpen = "[" + PasteRefKeyword + " "

// wholeStreamScreen replays EVERY write (this run ends at EOF, so there is no
// submit write to drop) and returns the cell grid — the screen a user would be
// looking at.
func wholeStreamScreen(t *testing.T, out *recordingWriter, cols, rows int) *Screen {
	t.Helper()
	var all []byte
	for _, chunk := range out.snapshot() {
		all = append(all, chunk...)
	}
	return renderScreen(t, all, cols, rows)
}

// firstLine trims an assertion message to the line's first row.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestPasteBulkSpill_FileIsByteExactForEveryByteValue is the byte-exactness
// contract: the spill file holds the pasted bytes VERBATIM. Nothing is decoded,
// re-encoded or translated on the spill path — a pasted CRLF stays CR LF, a
// pasted ESC stays 0x1b, and an invalid UTF-8 sequence stays itself.
func TestPasteBulkSpill_FileIsByteExactForEveryByteValue(t *testing.T) {
	// A table rather than one blob: each shape isolates a conversion the
	// historic path performs and the spill path must not.
	// Each shape is EXACTLY one full read: that is the paste the terminal
	// delivers (and the largest single read the editor's buffer can hold — a
	// longer block arrives as several reads, each spilled on its own).
	shapes := map[string][]byte{
		"crlf":         shapeBytes([]byte("line one\r\nline two\r\n"), PasteThreshold),
		"esc-and-csi":  shapeBytes([]byte{0x1b, '[', '3', '1', 'm'}, PasteThreshold),
		"control":      shapeBytes([]byte{0x00, 0x07, 0x08, 0x7f, 0x1b}, PasteThreshold),
		"invalid-utf8": shapeBytes([]byte{0xff, 0xfe, 0xc3, 0x28}, PasteThreshold),
	}
	for name, data := range shapes {
		if len(data) != PasteThreshold {
			t.Fatalf("%s: the fixture must be one full read, got %d bytes", name, len(data))
		}
		t.Run(name, func(t *testing.T) {
			store := newPasteStoreForTest(t)
			ed, _, _, _ := newScriptedEditor(t, []scriptStep{{data: data}, {data: []byte("\r"), advance: submitGap}}, pasteTestCols, nil)
			ed.SetPasteStore(store)
			res := runScript(t, ed)
			if len(res) != 1 {
				t.Fatalf("submitted lines = %d, want 1", len(res))
			}
			path := pastePathFromRef(t, res[0].Line)
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("the reference names a file that cannot be read: %v", err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("the spill file is not the paste: %d bytes on disk vs %d pasted (first difference at %d)",
					len(got), len(data), firstDifference(got, data))
			}
			// The reference states the content's size, so a reader knows how much
			// is behind the path without stat-ing it.
			want := PasteRefFor(PasteRef{Path: path, Bytes: len(data), Runes: utf8.RuneCount(data), Source: inputsource.Human})
			if !strings.Contains(res[0].Line, want) {
				t.Fatalf("the line does not carry the reference %q: %q", want, firstLine(res[0].Line))
			}
		})
	}
}

// firstDifference reports the index of the first byte two slices disagree on.
func firstDifference(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// pastePathFromRef extracts the absolute path a reference names. It is the
// READER of the reference's spelling: the tests must take the path out of the
// line the user sees, not out of the store's own field, or the rendering could
// be wrong and the test would not notice.
//
// It goes through the PRODUCTION reader (PasteRefPaths) rather than a local copy
// of the format. That is not a convenience: once `source` and `size` were added
// to the reference, a private parser here would have taken the whole tail —
// path, `source=` and `size=` together — as the path and then reported the
// production spelling as broken (measured, 2026-10-06: eight spill tests failed
// with `size=65536B: no such file or directory`). One format, one reader.
func pastePathFromRef(t *testing.T, line string) string {
	t.Helper()
	info, ok := PasteRefInfoFor(line)
	if !ok {
		t.Fatalf("the line carries no reference: %q", firstLine(line))
	}
	if info.Runes <= 0 {
		t.Fatalf("the reference's rune count is %d, want a positive number (%q)", info.Runes, firstLine(line))
	}
	if info.Size < 0 {
		t.Fatalf("the reference's byte count is %d, want a non-negative number (%q)", info.Size, firstLine(line))
	}
	path := info.Path
	if !filepath.IsAbs(path) {
		t.Fatalf("the reference names a relative path %q — it is the only handle on the bytes, so it must resolve from "+
			"any working directory", path)
	}
	return path
}

// TestPasteBulkSpill_ShortPasteTakesTheHistoricPath pins the other side of the
// threshold: a read SMALLER than the read buffer is ordinary input and must
// behave exactly as before (bracketed paste, CRLF = one line break, no spill).
func TestPasteBulkSpill_ShortPasteTakesTheHistoricPath(t *testing.T) {
	body := strings.Repeat("x", PasteThreshold-shortPasteSlack)
	// A bracketed paste carrying a CRLF, in ONE read below the threshold: the CR
	// must not submit (the burst suppression) and the LF must not double it.
	data := []byte(bracketedPasteStart + body + "\r\n")
	if len(data) >= PasteThreshold {
		t.Fatalf("the fixture must stay under the threshold: %d bytes", len(data))
	}
	store := newPasteStoreForTest(t)
	ed, _, clock, _ := newScriptedEditor(t, []scriptStep{{data: data}}, pasteTestCols, nil)
	ed.SetPasteStore(store)

	// The burst window's own clock is the fake one; the paste is one read, so the
	// suppression is armed by NoteKeys and the CR is content.
	res, err := ed.Run()
	_ = clock
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.EOF {
		t.Fatalf("a short paste ended the line (result %+v): a pasted CRLF must never submit", res)
	}
	// CRLF is ONE line break in the buffer: the CR becomes the newline and the
	// LF that follows it is dropped (the same rule the historic path applied).
	if got := ed.Value().Text(); got != body+"\n" {
		t.Fatalf("buffer = %q, want the pasted text with CRLF collapsed to one break", firstLine(got))
	}
	if _, err := os.ReadDir(store.Dir()); err == nil {
		t.Fatalf("a short paste was spilled: %s exists", store.Dir())
	}
}

// TestPasteBulkSpill_NoStoreKeepsTheHistoricBehaviour is the "no session
// directory" arm: an editor with no store types the paste into the buffer, byte
// for byte, exactly as it did before the spill existed.
func TestPasteBulkSpill_NoStoreKeepsTheHistoricBehaviour(t *testing.T) {
	data := []byte(strings.Repeat("y", PasteThreshold-1))
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{{data: data}, {data: []byte("\r"), advance: submitGap}}, pasteTestCols, nil)
	res := runScript(t, ed)
	if len(res) != 1 {
		t.Fatalf("submitted lines = %d, want 1", len(res))
	}
	if len(res[0].Line) < len(data) {
		t.Fatalf("the submitted line holds %d bytes, want the whole %d-byte paste (no store = no spill)",
			len(res[0].Line), len(data))
	}
}

// TestPasteBulkSpill_StoreFailureFallsBackToTyping is the Rule 15 arm: a store
// that cannot write must not lose the paste. The bytes go into the buffer, and
// the reason reaches the user on the editor's own reporting stream.
func TestPasteBulkSpill_StoreFailureFallsBackToTyping(t *testing.T) {
	// A FILE standing where the paste DIRECTORY must be: MkdirAll then fails with
	// ENOTDIR, deterministically, on every platform.
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), pasteFilePerm); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	var warned bytes.Buffer
	data := []byte(strings.Repeat("z", PasteThreshold+1))
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{{data: data}, {data: []byte("\r"), advance: submitGap}}, pasteTestCols, nil)
	ed.errOut = &warned
	ed.SetPasteStore(NewPasteStore(blocked))
	res := runScript(t, ed)
	if len(res) != 1 {
		t.Fatalf("submitted lines = %d, want 1", len(res))
	}
	if !strings.Contains(res[0].Line, "zzz") {
		t.Fatalf("the paste was LOST when the spill failed: line holds %d bytes, want the pasted bytes", len(res[0].Line))
	}
	if !strings.Contains(warned.String(), "could not be spilled") {
		t.Fatalf("the failure was silent (Rule 15): editor reported %q", warned.String())
	}
}

// TestPasteSpill_NamesAndReferenceShape pins the two spellings the epic fixes:
// the file name (paste-<stamp>-<n>.txt, under <session>/paste/) and the reference
// on the line.
func TestPasteSpill_NamesAndReferenceShape(t *testing.T) {
	store := newPasteStoreForTest(t)
	ref, err := store.Spill([]byte("hello"))
	if err != nil {
		t.Fatalf("Spill: %v", err)
	}
	wantDir := filepath.Dir(store.Dir())
	_ = wantDir
	if base := filepath.Base(store.Dir()); base != PasteDirName {
		t.Fatalf("the store's directory is %q, want a %q subdirectory of the session directory", base, PasteDirName)
	}
	name := filepath.Base(ref.Path)
	if !strings.HasPrefix(name, PasteFilePrefix) || !strings.HasSuffix(name, PasteFileSuffix) {
		t.Fatalf("spill file name %q does not match %s<stamp>-<n>%s", name, PasteFilePrefix, PasteFileSuffix)
	}
	stamp := time.Unix(pasteTestStamp, 0).UTC().Format(PasteStampLayout)
	if !strings.Contains(name, stamp) {
		t.Fatalf("spill file name %q does not carry the paste's own timestamp %q", name, stamp)
	}
	if got, err := os.ReadFile(ref.Path); err != nil || string(got) != "hello" {
		t.Fatalf("spill file content = %q (err %v), want %q", got, err, "hello")
	}
	if mode := fileMode(t, ref.Path); mode != pasteFilePerm {
		t.Fatalf("spill file mode = %o, want %o (the user's own pasted bytes)", mode, pasteFilePerm)
	}
	// THE NEW SHAPE, pinned field by field: rune count, path, provenance and the
	// byte count, in that order (the order the parser expects them in).
	if got := PasteRefFor(ref); got != fmt.Sprintf("[paste %d: %s source=%s size=%dB]",
		utf8.RuneCountInString("hello"), ref.Path, inputsource.Human, len("hello")) {
		t.Fatalf("reference rendering = %q", got)
	}
	if info, ok := PasteRefInfoFor(PasteRefFor(ref)); !ok || info.Path != ref.Path || info.Source != inputsource.Human || info.Size != len("hello") || info.Runes != utf8.RuneCountInString("hello") {
		t.Fatalf("the reference did not round-trip through the production reader: %+v (ok=%v) from %q", info, ok, PasteRefFor(ref))
	}
	// A second spill gets its own file: a name that is taken is never
	// overwritten, whatever the clock says.
	second, err := store.Spill([]byte("world"))
	if err != nil {
		t.Fatalf("second Spill: %v", err)
	}
	if second.Path == ref.Path {
		t.Fatalf("two pastes share one file %q — the first paste's bytes would be replaced", ref.Path)
	}
}

// fileMode reports a file's permission bits.
func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return st.Mode().Perm()
}

// TestPasteBulkSpill_EditingLineShowsTheReferenceNotThePaste is the rendered-
// cell-grid half of the spill contract: ONE full read spilled, and the row the
// user is typing on carries the reference — the paste's own bytes are on disk,
// not on the screen.
func TestPasteBulkSpill_EditingLineShowsTheReferenceNotThePaste(t *testing.T) {
	store := newPasteStoreForTest(t)
	data := shapeBytes([]byte("pasted-content-"), PasteThreshold)
	ed, _, _, out := newScriptedEditor(t, []scriptStep{{data: data}}, pasteTestCols, nil)
	ed.SetPasteStore(store)
	if _, err := ed.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The line is drawn while it is being edited: whatever rows the wrap put the
	// reference on, the row carrying the prompt begins the reference.
	sc := editingScreen(t, out, pasteTestCols, pasteTestRows)
	head, ok := rowStartingWith(sc, promptForTest)
	if !ok {
		t.Fatalf("no painted row carries the prompt %q; the screen is:\n%s", promptForTest, sc.Contents())
	}
	if !strings.HasPrefix(head, promptForTest+pasteRefOpen) {
		t.Fatalf("the editing row is %q, want %q followed by the reference — the paste itself must not be typed into "+
			"the line", head, promptForTest)
	}
	if strings.Contains(sc.Contents(), strings.Repeat("pasted-content-", 3)) {
		t.Fatalf("the pasted text itself is on the screen:\n%s", sc.Contents())
	}
}

// TestPasteBulkSpill_DropsTheTerminalsPasteEnvelope pins the defect a REAL pty
// exposed and the scripted fixtures had missed until now.
//
// gopi's own raw mode turns bracketed paste on, so an interactive terminal wraps
// every pasted block in CSI 200 ~ … CSI 201 ~. The DECODER drops those two
// markers (KeyPasteStart/KeyPasteEnd); the spill path writes a read to disk
// before the decoder sees it, so it must drop them itself. It did not: measured
// on 2026-09-28 against a real pty, a 42 KiB bracketed paste spilled 42012 bytes
// for 42000 bytes of content, and the file's FIRST byte was the start marker —
// the envelope was filed into the file the user is handed, corrupting it for
// every paste big enough to spill.
//
// THE SHAPE THAT MATTERS is not the tidy one. A paste is delivered as a sequence
// of reads, so the start marker is not at the front of the stream and the end
// marker is not at the back of one read: here the start marker rides the head of
// the SECOND read and the end marker rides a later read, exactly as the pty
// delivered them. A fixture that brackets a single one-read paste would pass
// against the pre-fix tree and prove nothing.
func TestPasteBulkSpill_DropsTheTerminalsPasteEnvelope(t *testing.T) {
	const contentBytes = 3 * spillChunkBytes
	content := pasteBytes(contentBytes)
	// Every read is at most the read buffer — the source copies one step into the
	// editor's own buffer, so a fixture larger than a read is not a shape a
	// terminal could deliver either.
	const tailBytes = contentBytes - 2*spillChunkBytes // 12 bytes: a short tail read

	// Read 1: bare content. Read 2: the START marker, then content. Read 3: the
	// rest of the content, then the END marker — the envelope's two halves arrive
	// in different reads, neither of them at the edge of the stream.
	read1 := content[:spillChunkBytes]
	read2 := append([]byte(bracketedPasteStart), content[spillChunkBytes:2*spillChunkBytes-len(bracketedPasteStart)]...)
	read3 := append(append([]byte{}, content[2*spillChunkBytes-len(bracketedPasteStart):contentBytes-tailBytes]...), bracketedPasteEnd...)
	read4 := content[contentBytes-tailBytes:]

	store := newPasteStoreForTest(t)
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{
		{data: read1}, {data: read2}, {data: read3}, {data: read4},
		{data: []byte("\r"), advance: submitGap},
	}, pasteTestCols, nil)
	ed.SetPasteStore(store)
	res := runScript(t, ed)
	if len(res) != 1 {
		t.Fatalf("submitted lines = %d, want 1", len(res))
	}
	line := res[0].Line
	// A paste is ONE thing: both markers arrive INSIDE the same paste, so the
	// line must still carry exactly one reference.
	if got := strings.Count(line, pasteRefOpen); got != 1 {
		t.Fatalf("the line carries %d references, want ONE (line %q)", got, firstLine(line))
	}
	path := pastePathFromRef(t, line)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the reference names an unreadable file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("the spill file is not the pasted content: %d bytes on disk vs %d pasted (first difference at %d) — "+
			"the terminal's bracketed-paste framing is not content", len(got), len(content), firstDifference(got, content))
	}
	// The size the reference states is the size of the file, so a reader is not
	// told the paste was 12 bytes longer than it was.
	want := PasteRefFor(PasteRef{Path: path, Bytes: len(content), Runes: utf8.RuneCount(content), Source: inputsource.Human})
	if strings.TrimSpace(line) != want {
		t.Fatalf("the submitted line is %q, want exactly %q", strings.TrimSpace(line), want)
	}
}

// TestPasteBulkSpill_APastedEscapeSurvivesTheEnvelopeRule is the other side of
// the marker rule: dropping the terminal's framing must not turn into dropping
// escape sequences the user really pasted. ESC, a CSI that is NOT the paste
// marker, control bytes and invalid UTF-8 all stay in the file verbatim.
func TestPasteBulkSpill_APastedEscapeSurvivesTheEnvelopeRule(t *testing.T) {
	// Content that contains escape-shaped bytes, plus a near-miss of the envelope
	// (the same bytes through "201" instead of "200"). It is sized so the whole
	// fixture is EXACTLY one full read — the envelope at both ends, which is the
	// other delivery shape a terminal produces (a paste short enough to arrive in
	// one read).
	nearMiss := []byte{0x1b, '[', '3', '1', 'm', 0x1b, '[', '2', '0', '1', '~'}
	inner := append(append([]byte{}, nearMiss...),
		pasteBytes(spillChunkBytes-2*len(bracketedPasteStart)-len(nearMiss))...)
	data := append([]byte(bracketedPasteStart), inner...)
	data = append(data, []byte(bracketedPasteEnd)...)
	if len(data) != spillChunkBytes {
		t.Fatalf("the fixture must be one full read (%d bytes), got %d", spillChunkBytes, len(data))
	}

	store := newPasteStoreForTest(t)
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{
		{data: data},
		{data: []byte("\r"), advance: submitGap},
	}, pasteTestCols, nil)
	ed.SetPasteStore(store)
	res := runScript(t, ed)
	if len(res) != 1 {
		t.Fatalf("submitted lines = %d, want 1", len(res))
	}
	got, err := os.ReadFile(pastePathFromRef(t, res[0].Line))
	if err != nil {
		t.Fatalf("the reference names an unreadable file: %v", err)
	}
	if !bytes.Equal(got, inner) {
		t.Fatalf("the file holds %d bytes, want %d — only the terminal's own framing is dropped, and every pasted "+
			"escape sequence stays (first difference at %d)", len(got), len(inner), firstDifference(got, inner))
	}
	if !bytes.Contains(got, nearMiss[5:]) {
		t.Fatal("a pasted sequence that only LOOKS like the end marker was dropped: the rule must remove the " +
			"terminal's framing, never bytes the user pasted")
	}
}

// TestPasteBulkSpill_ContinuationReadsJoinOneFile is the regression guard for the
// shape a REAL terminal delivers — and the defect it guards is measured, not
// imagined.
//
// A paste is not delivered in whole read buffers: 64 KiB written to a pty reaches
// the editor as 16 reads of 4095 bytes PLUS a 16-byte tail (measured 2026-09-28
// with a pty driver against `gopi chat`, printout in devdoc/chat-paste-spill.md).
// The first fix spilled only the full reads, so the message came out as sixteen
// references followed by the tail as escaped text —
//
//	[paste 4095: …]…[paste 4095: …]pasted content
//
// — content that has left the line, mixed with content that has not. A paste is
// ONE message, so all of its reads go to ONE file and leave ONE reference, and
// the size the reference states is the size of the file.
func TestPasteBulkSpill_ContinuationReadsJoinOneFile(t *testing.T) {
	const chunkCount = 4
	const tail = 16
	data := pasteBytes(chunkCount*spillChunkBytes + tail)

	steps := make([]scriptStep, 0, chunkCount+2)
	for i := 0; i < chunkCount; i++ {
		steps = append(steps, scriptStep{data: data[i*spillChunkBytes : (i+1)*spillChunkBytes]})
	}
	// The SHORT tail read: the same paste, delivered, not a new one.
	steps = append(steps, scriptStep{data: data[chunkCount*spillChunkBytes:]})
	steps = append(steps, scriptStep{data: []byte("\r"), advance: submitGap})

	store := newPasteStoreForTest(t)
	ed, _, _, out := newScriptedEditor(t, steps, pasteTestCols, nil)
	ed.SetPasteStore(store)
	res := runScript(t, ed)
	if len(res) != 1 {
		t.Fatalf("submitted lines = %d, want 1", len(res))
	}
	line := res[0].Line
	if got := strings.Count(line, pasteRefOpen); got != 1 {
		t.Fatalf("the line carries %d references, want ONE — a paste delivered as several reads is ONE paste "+
			"(line %q)", got, firstLine(line))
	}
	// The line IS the reference: no pasted byte escaped onto it beside the file's
	// own path (the path necessarily contains the word the reference marker uses,
	// so the comparison is the whole line, not a substring hunt).
	path := pastePathFromRef(t, line)
	want := PasteRefFor(PasteRef{Path: path, Bytes: len(data), Runes: utf8.RuneCount(data), Source: inputsource.Human})
	if strings.TrimSpace(line) != want {
		t.Fatalf("the submitted line is %q, want exactly the reference %q — pasted bytes escaped beside it", line, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the reference names an unreadable file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("the spill file holds %d bytes, the paste was %d (first difference at %d): the continuation reads "+
			"must be appended to the same file", len(got), len(data), firstDifference(got, data))
	}
	// One file, not one per read.
	files, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatalf("read the paste directory: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("spilled files = %d, want 1 (all of one paste's reads land in one file)", len(files))
	}
	// And the screen carries the reference, not the content.
	sc := wholeStreamScreen(t, out, pasteTestCols, pasteTestRows)
	if !strings.Contains(sc.Contents(), pasteRefOpen) {
		t.Fatalf("the painted screen shows no reference; it is:\n%s", sc.Contents())
	}
}

// TestPasteBulkSpill_TwoPastesStayTwo is the other side of the continuation
// window: an ordinary keystroke between two pastes ENDS the first one, so the
// second is filed on its own instead of being appended to the file the first one
// opened. That is what makes the window safe — it separates "the paste is still
// arriving" from "a new paste".
func TestPasteBulkSpill_TwoPastesStayTwo(t *testing.T) {
	first := shapeBytes([]byte("first-paste-"), spillChunkBytes)
	second := shapeBytes([]byte("second-paste-"), spillChunkBytes)
	store := newPasteStoreForTest(t)
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{
		{data: first},
		// A keystroke: the user types one character between the two pastes.
		{data: []byte("|")},
		{data: second},
		{data: []byte("\r"), advance: submitGap},
	}, pasteTestCols, nil)
	ed.SetPasteStore(store)
	res := runScript(t, ed)
	if len(res) != 1 {
		t.Fatalf("submitted lines = %d, want 1", len(res))
	}
	if got := strings.Count(res[0].Line, pasteRefOpen); got != 2 {
		t.Fatalf("the line carries %d references, want 2 (two pastes, each its own file): %q", got, firstLine(res[0].Line))
	}
	files, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatalf("read the paste directory: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("spilled files = %d, want 2 — a keystroke between two pastes ends the first", len(files))
	}
	bodies := map[string]bool{}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(store.Dir(), f.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", f.Name(), err)
		}
		if !bytes.Equal(b, first) && !bytes.Equal(b, second) {
			t.Fatalf("a spill file holds neither paste: %d bytes, first %q", len(b), firstLine(string(b)))
		}
		bodies[string(b)] = true
	}
	if !bodies[string(first)] || !bodies[string(second)] {
		t.Fatalf("the two files do not hold the two pastes (%d distinct bodies)", len(bodies))
	}
	// The typed keystroke stayed TYPED: it is on the line between the references,
	// it did not open or close a paste's content.
	if !strings.Contains(res[0].Line, "|") {
		t.Fatalf("the typed keystroke is missing from the line: %q", firstLine(res[0].Line))
	}
}
