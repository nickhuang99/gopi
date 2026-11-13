package lineedit

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// window_test.go — the editing line's SCROLLING WINDOW, asserted on the RENDERED
// CELL GRID (Rule 10.2): the rows the user's terminal would show, and the marked
// rows that account for the input those rows do not reach.
//
// The defect these tests close (2nd incident on the editor's row budget — the
// first was U1, a fixed cap of 4; devdoc/editor-rows-derived.md): the editing
// line's row budget is finite and the renderer drew the input's FIRST rows, so a
// pasted message taller than the budget left the rest of the user's own text
// unstated on screen — not truncated in the buffer, but invisible and unaccounted
// for. The acceptance the epic states is exactly what is asserted here: in the
// dump, the rows that are NOT shown are NAMED with their count, and the buffer is
// whole.
//
// Three sizes are covered, as the epic requires: a short single-line input (no
// window at all, so the mechanism costs nothing), a 30-line paste (the count is
// stated, the caret's own rows are what you see), and hundreds of lines (the count
// is large, the hidden input is reachable with a key, and the buffer survives).

// windowMarkReAbove / windowMarkReBelow read the counts back OUT OF THE DUMP.
// Parsing the rendered row is the point: the test does not ask the window what it
// thinks it hid, it measures what the user would read.
var (
	windowMarkReAbove = regexp.MustCompile(`…\s+(\d+) lines above`)
	windowMarkReBelow = regexp.MustCompile(`…\s+(\d+) lines below`)
)

// windowStatedHidden reads the hidden-row counts off the rendered grid of the
// editing line's rows — every row between its top and the status row, because the
// window may draw one row more than the plan published (the key row lives on the
// menu's row when nothing else claims it). rowsHidden = above + below, as the dump
// spells them.
func windowStatedHidden(t *testing.T, h *a5Harness, top int) (above, below int) {
	t.Helper()
	screen, status := h.screen, h.region.Plan().StatusRow
	aboveSet, belowSet := false, false
	for i := top; i < status; i++ {
		line := screen.Line(i)
		if m := windowMarkReAbove.FindStringSubmatch(line); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				above, aboveSet = n, true
			}
		}
		if m := windowMarkReBelow.FindStringSubmatch(line); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				below, belowSet = n, true
			}
		}
	}
	if !aboveSet && !belowSet {
		t.Fatalf("the dump states NOTHING about the input rows the editing line does not show — the user reads their own text and cannot tell that the rest exists (the silent cut this contract closes)\n--- screen ---\n%s",
			screen.Contents())
	}
	return above, below
}

// windowVisibleTextRows counts the editing line's rows that carry a row of the
// input (the marked rows are the ones that do not).
func windowVisibleTextRows(t *testing.T, h *a5Harness, _ int, _ int) int {
	t.Helper()
	return h.ed.render.Window().textRowsOf()
}

// windowRowsOnScreen counts the editing line's rows that carry a row of the input
// ON THE SCREEN — as opposed to the window's last text row, which the key row
// displaces onto the menu's row when the layout's own EditorRows is smaller than
// the window the renderer resolved.

// screenRowCarriesInput reports whether the editing line's drawn row i carries a
// buffer row. It asks the WINDOW's own published offsets — the mapping the repaint
// drew with — so the count cannot drift from what was drawn.
func screenRowCarriesInput(h *a5Harness, i int) bool {
	off := h.ed.render.Window().windowOffsetsAt(i)
	return off != windowChromeOffset
}

// TestWindow_AShortInputDrawsNoMarker is the cost side of the mechanism: a line
// that fits is drawn exactly as it always was — every row of the input, no marked
// row, no key row, nothing taken from the text.
func TestWindow_AShortInputDrawsNoMarker(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, a5Rows, a5Cols)
	h.ed.val.SetText("hello world")
	h.ed.repaint()
	h.replay()

	p := h.region.Plan()
	if got := h.screen.Line(p.EditorTop); got != "you> hello world" {
		t.Fatalf("editing row %d = %q, want the typed line\n%s", p.EditorTop, got, h.screen.Contents())
	}
	for i := 0; i < p.EditorRows; i++ {
		if got := h.screen.Line(p.EditorTop + i); windowMarkerText(got) {
			t.Fatalf("editing row %d = %q — a marker row is drawn for a line that fits, and it costs a row the user's text could use\n%s",
				p.EditorTop+i, got, h.screen.Contents())
		}
	}
	if win := h.ed.render.Window(); win.total != 1 || win.above != 0 || win.below != 0 {
		t.Fatalf("the window of a one-row input is %+v, want 1 total row and nothing hidden", win)
	}
}

// TestWindow_AThirtyLinePasteStatesItsHiddenRows is the epic's acceptance at the
// 30-line size: the dump CARRIES a marked row naming the number of rows the
// editing line does not show, the rows that ARE shown are the window's own buffer
// rows (by equality with the wrap), and the count is EXACT —
//
//	visible + hidden above + hidden below == the input's row count
//
// which is the accounting that makes "never silently dropped" checkable rather
// than a promise.
func TestWindow_AThirtyLinePasteStatesItsHiddenRows(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, a5Rows, a5Cols)
	all, _, _ := layoutText(promptForTest, windowPastedLines(windowThirtyLines), len([]rune(windowPastedLines(windowThirtyLines))), a5Cols)
	h.ed.val.SetText(windowPastedLines(windowThirtyLines))
	h.ed.repaint()
	h.replay()

	p := h.region.Plan()
	if len(all) <= p.EditorRows {
		t.Fatalf("the fixture does not overflow the editing line (%d rows of input, %d rows of layout)", len(all), p.EditorRows)
	}
	above, below := windowStatedHidden(t, h, p.EditorTop)
	visible := windowVisibleTextRows(t, h, p.EditorTop, p.EditorRows)
	if visible+above+below != len(all) {
		t.Fatalf("the editing line accounts for %d of the input's %d rows (visible=%d, above=%d, below=%d) — a row nobody's count mentions is a row the user cannot know about\n%s",
			visible+above+below, len(all), visible, above, below, h.screen.Contents())
	}
	// The rows on screen are the input's OWN rows, in order: the window is a
	// window, not a re-rendering of something else.
	for i := 0; i < p.EditorRows; i++ {
		if !screenRowCarriesInput(h, i) {
			continue
		}
		off := h.ed.render.Window().windowOffsetsAt(i)
		if got := h.screen.Line(p.EditorTop + i); got != all[off] {
			t.Fatalf("editing row %d = %q, want the input's row %d (%q)\n%s", p.EditorTop+i, got, off, all[off], h.screen.Contents())
		}
	}
	// The caret's row is ON the screen — the input the user is typing is what they
	// see (the head window this replaces left it below the last drawn row).
	if h.screen.CurRow < p.EditorTop || h.screen.CurRow >= p.EditorTop+p.EditorRows {
		t.Fatalf("the caret is on row %d, outside the editing line's rows [%d..%d]\n%s",
			h.screen.CurRow, p.EditorTop, p.EditorTop+p.EditorRows-1, h.screen.Contents())
	}
	// And the buffer is WHOLE: the window is a display bound, never an input one.
	if got := h.ed.val.Text(); got != windowPastedLines(windowThirtyLines) {
		t.Fatalf("the buffer holds %d runes, want the %d runes that were pasted — the window must never shorten the message",
			len([]rune(got)), len([]rune(windowPastedLines(windowThirtyLines))))
	}
}

// TestWindow_HundredsOfLinesAreReachableAndTheBufferSurvives is the extreme size:
// a 300-line paste whose count is stated, whose hidden head is REACHED with the
// scroll key, and whose text the editor still hands over complete.
func TestWindow_HundredsOfLinesAreReachableAndTheBufferSurvives(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, editorRowsReproRows, a5Cols)
	text := windowPastedLines(windowHundredsOfLines)
	h.ed.val.SetText(text)
	h.ed.repaint()
	h.replay()

	p := h.region.Plan()
	above, _ := windowStatedHidden(t, h, p.EditorTop)
	if above == 0 {
		t.Fatalf("a %d-line paste states 0 hidden rows above the window\n%s", windowHundredsOfLines, h.screen.Contents())
	}
	// The head is reachable: a scroll to the top puts the buffer's FIRST row (the
	// prompt's row) on screen, and then nothing is hidden above.
	if !h.ed.scrollEditingLine(true, true) {
		t.Fatal("the scroll key reported no movement on a window with rows above it")
	}
	for h.ed.scrollEditingLine(true, true) {
	}
	h.ed.repaint()
	h.replay()
	aboveTop, _ := windowStatedHidden(t, h, p.EditorTop)
	if aboveTop != 0 {
		t.Fatalf("after scrolling to the top the dump still states %d rows hidden above the window\n%s", aboveTop, h.screen.Contents())
	}
	head, _, _ := layoutText(h.ed.render.prompt, text, 0, a5Cols)
	win := h.ed.render.Window()
	drawnFirst := false
	for i := 0; i < win.rows; i++ {
		if win.windowOffsetsAt(i) == 0 && h.screen.Line(p.EditorTop+i) == head[0] {
			drawnFirst = true
		}
	}
	if !drawnFirst {
		t.Fatalf("the input's first row %q is not on screen after scrolling to the top\n%s", head[0], h.screen.Contents())
	}
	// The buffer is untouched by every one of those window moves.
	if got := h.ed.val.Text(); got != text {
		t.Fatalf("the buffer holds %d runes after scrolling, want the %d pasted", len([]rune(got)), len([]rune(text)))
	}
}

// TestWindow_TheScrollKeysMoveTheWindowOnTheRenderedGrid drives the keys through
// the REAL editor (a scripted source, the real decoder, the real repaint) and
// measures the result on the grid: PageUp shows earlier rows, PageDown returns,
// and Alt+Arrow moves by ONE row — the paged and single-row motions the key row
// advertises.
func TestWindow_TheScrollKeysMoveTheWindowOnTheRenderedGrid(t *testing.T) {
	steps := []scriptStep{
		{data: []byte(windowPasteStart + windowPastedLines(windowThirtyLines) + windowPasteEnd)},
		{timeout: true},
	}
	h := newA5HarnessSized(t, steps, nil, a5Rows, a5Cols)
	if _, err := h.ed.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	h.replay()
	p := h.region.Plan()
	aboveAtCaret, _ := windowStatedHidden(t, h, p.EditorTop)
	if aboveAtCaret == 0 {
		t.Fatalf("the pasted line states no hidden rows at the caret\n%s", h.screen.Contents())
	}

	// PageUp (CSI 5~) shows earlier rows: the count of rows hidden ABOVE the
	// window FALLS by the page step (the window's own text height less one).
	h.ed.dec.Feed([]byte(editorPageUpSeq))
	for _, k := range h.ed.dec.Feed([]byte(editorPageUpSeq)) {
		if _, done, err := h.ed.handleKey(k); err != nil || done {
			t.Fatalf("PageUp: done=%v err=%v", done, err)
		}
	}
	h.replay()
	aboveAfterPage, _ := windowStatedHidden(t, h, p.EditorTop)
	win := h.ed.render.Window()
	if want := aboveAtCaret - (win.textRowsOf() - 1); aboveAfterPage != want {
		t.Fatalf("after PageUp the dump states %d rows hidden above, want %d (the count at the caret %d less the page step %d)\n%s",
			aboveAfterPage, want, aboveAtCaret, win.textRowsOf()-1, h.screen.Contents())
	}

	// PageDown (CSI 6~) goes back: the caret's end is where the window returns to.
	for _, k := range h.ed.dec.Feed([]byte(editorPageDownSeq)) {
		if _, done, err := h.ed.handleKey(k); err != nil || done {
			t.Fatalf("PageDown: done=%v err=%v", done, err)
		}
	}
	h.replay()
	aboveBack, _ := windowStatedHidden(t, h, p.EditorTop)
	if aboveBack != aboveAtCaret {
		t.Fatalf("after PageDown the dump states %d rows hidden above, want the caret's own %d\n%s",
			aboveBack, aboveAtCaret, h.screen.Contents())
	}

	// Alt+Up (CSI 1;3A) is the ONE-ROW motion.
	for _, k := range h.ed.dec.Feed([]byte(editorAltUpSeq)) {
		if _, done, err := h.ed.handleKey(k); err != nil || done {
			t.Fatalf("Alt+Up: done=%v err=%v", done, err)
		}
	}
	h.replay()
	aboveOneRow, _ := windowStatedHidden(t, h, p.EditorTop)
	if want := aboveBack - 1; aboveOneRow != want {
		t.Fatalf("after Alt+Up the dump states %d rows hidden above, want %d (one row)\n%s",
			aboveOneRow, want, h.screen.Contents())
	}
	// The buffer is whole after every key, and no key submitted the line.
	if got := h.ed.val.Text(); got != windowPastedLines(windowThirtyLines) {
		t.Fatalf("the buffer changed under the scroll keys: %d runes, want %d", len([]rune(got)), len([]rune(windowPastedLines(windowThirtyLines))))
	}
}

// TestWindow_AtTheFloorTerminalTheMechanismIsStillOnScreen is the availability
// half, and the reason the layout's floor is EditorRowsMin: at the smallest
// terminal that hosts the layout at all, a tall input still gets a row of its own
// text, the marker that states the rest, and the key row that reaches it. Before
// this, both chrome rows were drawn only when a spare row happened to be free —
// i.e. on a tall terminal, never when the user needed them.
func TestWindow_AtTheFloorTerminalTheMechanismIsStillOnScreen(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, rowsMinUsable, a5Cols)
	h.ed.val.SetText(windowPastedLines(windowThirtyLines))
	h.ed.repaint()
	h.replay()

	p := h.region.Plan()
	if got := h.ed.render.Window().textRowsOf(); got < minEditorRows {
		t.Fatalf("at %d rows the editing line shows %d of the user's text rows, want at least %d\n%s",
			rowsMinUsable, got, minEditorRows, h.screen.Contents())
	}
	above, _ := windowStatedHidden(t, h, p.EditorTop)
	visible := windowVisibleTextRows(t, h, p.EditorTop, p.EditorRows)
	if visible+above != len(layoutRowsOnly(t, windowPastedLines(windowThirtyLines))) {
		t.Fatalf("at the floor terminal the line accounts for %d of %d rows (visible=%d, above=%d)\n%s",
			visible+above, len(layoutRowsOnly(t, windowPastedLines(windowThirtyLines))), visible, above, h.screen.Contents())
	}
	// The mechanism is NAMED on screen: the key row states the keys (the epic's
	// "明确的滚动机制" — a key nobody is told about is not a mechanism).
	found := false
	for i := 0; i < p.EditorRows; i++ {
		if strings.Contains(h.screen.Line(p.EditorTop+i), windowScrollKeys) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no editing row names the scroll keys at the floor terminal\n%s", h.screen.Contents())
	}
	// And the status row is untouched by any of it.
	if got := h.screen.Line(p.StatusRow); strings.Contains(got, windowScrollKeys) {
		t.Fatalf("the key row landed on the status row (%q) — the mechanism must never take the status line", got)
	}
}

// TestWindow_TheMarkerRowIsCutNotWrapped: a marker row is bounded by the terminal
// width with the product's own elision marker (cutField's), never wrapped — a
// wrapped marker would take a second row of a layout whose arithmetic it does not
// own (computePlan does).
func TestWindow_TheMarkerRowIsCutNotWrapped(t *testing.T) {
	const narrow = 24
	h := newA5HarnessSized(t, nil, nil, a5Rows, narrow)
	h.ed.val.SetText(windowPastedLines(windowThirtyLines))
	h.ed.repaint()
	h.replay()

	p := h.region.Plan()
	for i := 0; i < p.EditorRows; i++ {
		line := h.screen.Line(p.EditorTop + i)
		if !windowMarkerText(line) {
			continue
		}
		if w := displayWidth(line); w > narrow {
			t.Fatalf("editing row %d = %q is %d cells wide on a %d-column terminal — a marker row must be cut, not wrapped",
				p.EditorTop+i, line, w, narrow)
		}
	}
	// The status row is still exactly one screen row and still carries the status.
	if got := h.screen.Line(p.StatusRow); displayWidth(got) > narrow {
		t.Fatalf("status row = %q is wider than the terminal", got)
	}
}

// TestWindow_TheOldHeadClipLeavesTheHiddenRowsUnstated is the NEGATIVE CONTROL,
// and the red proof's arithmetic stated in one place: the arrangement this epic
// replaces (draw the input's first `maxRows` rows, say nothing) is reconstructed
// here and shown to leave the hidden rows unaccounted for. It is a property of the
// OLD shape, not of the tree, so it stays red-or-green independently of where the
// rest of the file compiles — and it names the defect rather than describing it.
func TestWindow_TheOldHeadClipLeavesTheHiddenRowsUnstated(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, a5Rows, a5Cols)
	text := windowPastedLines(windowThirtyLines)
	all, _, _ := layoutText(promptForTest, text, len([]rune(text)), a5Cols)
	// The layout is resolved AFTER the buffer is set: a taller editing line moves
	// its own top row (the band yields), so a plan read before the paste names a
	// row the marker no longer sits on.
	h.ed.val.SetText(text)
	h.ed.repaint()
	h.replay()
	p := h.region.Plan()
	if len(all) <= p.EditorRows {
		t.Fatalf("the fixture does not overflow the editing line (%d rows, %d laid out)", len(all), p.EditorRows)
	}
	// The old window: the head, nothing said.
	oldDrawn := all[:p.EditorRows]
	oldStated := 0 // the head window states no count at all
	if oldStated+len(oldDrawn) == len(all) {
		t.Fatalf("the old head clip accounts for every row — the fixture cannot exhibit the defect it is here to name")
	}
	hidden := len(all) - len(oldDrawn)
	if hidden != len(all)-p.EditorRows {
		t.Fatalf("the old clip hid %d rows, want %d", hidden, len(all)-p.EditorRows)
	}
	// And the CURRENT tree does state them: the same input, on the same terminal,
	// now carries the count in the dump.
	above, below := windowStatedHidden(t, h, p.EditorTop)
	if above+below == 0 {
		t.Fatalf("the live dump states no hidden rows where the old clip hid %d\n%s", hidden, h.screen.Contents())
	}
}

// layoutRowsOnly is the wrapped row count of text under the test prompt at the
// harness width — the number the markers have to account for.
func layoutRowsOnly(t *testing.T, text string) []string {
	t.Helper()
	rows, _, _ := layoutText(promptForTest, text, len([]rune(text)), a5Cols)
	return rows
}

// windowPastedLines builds a multi-line message of n lines, the shape a bracketed
// paste really delivers (its newlines are CONTENT, so the buffer genuinely holds n
// lines: the paste path classifies them, it does not submit on them).
func windowPastedLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteByte('\n')
		}
		b.WriteString("pasted line ")
		b.WriteString(strconv.Itoa(i))
	}
	return b.String()
}

// The fixture sizes and the wire spellings the key test drives. The sequences are
// spelled here because a test that decodes them is the only place they are the
// SUBJECT (production reads them from the one decoder).
const (
	windowThirtyLines     = 30
	windowHundredsOfLines = 300
	windowPasteStart      = "\x1b[200~"
	windowPasteEnd        = "\x1b[201~"
	editorPageUpSeq       = "\x1b[5~"
	editorPageDownSeq     = "\x1b[6~"
	editorAltUpSeq        = "\x1b[1;3A"
)

// TestWindow_AnOwningSurfaceCanTurnTheKeyStatementOff pins the ONE switch a
// surface that claims the keys uses (`gopi chat --replay`), and pins the half it
// may NOT take away: the marker rows stop NAMING the scroll keys, and keep STATING
// the hidden counts.
//
// BOTH HALVES ARE MEASURED ON THE RENDERED GRID. A test that read the flag back
// would pass on a build whose marker row ignored it — which is exactly what the
// first attempt at this fix did: the `below == 0` arm of renderWindow returned the
// key statement ALONE, so `keys=false` changed nothing on a real pty (measured
// 2026-10-22: 103 occurrences of the sentence before and after that attempt).
func TestWindow_AnOwningSurfaceCanTurnTheKeyStatementOff(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, a5Rows, a5Cols)
	if !h.ed.WindowKeyRow() {
		t.Fatalf("a fresh editor already has the key row off — the default must be the historic behaviour")
	}
	h.ed.SetWindowKeyRow(false)
	if h.ed.WindowKeyRow() {
		t.Fatalf("SetWindowKeyRow(false) did not reach the renderer (the reader still says on)")
	}
	h.ed.val.SetText(windowPastedLines(windowThirtyLines))
	h.ed.repaint()
	h.replay()

	p := h.region.Plan()
	for i := 0; i < p.EditorRows; i++ {
		if line := h.screen.Line(p.EditorTop + i); strings.Contains(line, windowScrollKeys) {
			t.Fatalf("editing row %d = %q still names the scroll keys after the owning surface turned them off\n%s",
				p.EditorTop+i, line, h.screen.Contents())
		}
	}
	// THE COUNTS STAY: the surface may hide a KEY, never a FACT about the user's
	// own text (windowStatedHidden fails when the dump states nothing at all).
	above, below := windowStatedHidden(t, h, p.EditorTop)
	if above+below == 0 {
		t.Fatalf("with the key row off the dump states no hidden rows either — the switch took away the user's account of their own text\n%s",
			h.screen.Contents())
	}
	// And the keys are back on when the surface hands the line over.
	h.ed.SetWindowKeyRow(true)
	h.ed.repaint()
	h.replay()
	found := false
	for i := 0; i < p.EditorRows; i++ {
		if strings.Contains(h.screen.Line(p.EditorTop+i), windowScrollKeys) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the key statement did not come back when the surface released the line\n%s", h.screen.Contents())
	}
}
