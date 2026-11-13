package lineedit

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// testmain scratch root is pinned by the suite's TestMain; this file needs no
// scratch of its own.

// window_caret_row_boundary_test.go — THE CARET'S ROW at the editing line's row
// budget boundary.
//
// THE REPORTED DEFECT (epic 01a11811). As pasted input reaches the editing line's
// row budget (editorRowsCap 12 rows on a 24-row terminal, plus the two chrome rows
// the scrolling window reserves), the cursor stops being drawn on the row of the
// user's own text: it lands ONE ROW ABOVE it — on the previous row of the buffer —
// and the row it should be on is drawn as ordinary text below it. That is the
// operator's "输入光标跳一行": the caret walked off its own row at exactly the
// input height where the scrolling window starts drawing a marked row ("… N lines
// above") — the same height at which the key row
// `… PgUp/PgDn scroll · Alt+↑/↓ one row` appears.
//
// THE MECHANISM (measured; the epic's own hypothesis — `total = max(len(rows),
// r.drawnRows)` in render.go — is FALSIFIED and recorded in the devdoc: the defect
// reproduces with drawnRows == 0, i.e. with total == len(rows) and no erase phase
// involved). It is a WINDOW COORDINATE that omits the marker row: renderWindow
// inserts the lead marker at drawn index 0 and then computes the caret's drawn row
// as `cur - top`, a TEXT-ROW index — so with a lead marker on screen the caret is
// placed one row too high, and the column it is moved to is measured off the wrong
// row as well (`cursorCell(drawn[row], ...)`).
//
// THE JUDGE IS THE GRID (Rule 10.2): the assertion reads `Screen.CurRow` after
// replaying the bytes the components really wrote, and compares the row the cursor
// sits on against the row of the input the LAYOUT (layoutText, an independent
// computation) says the caret is on. Nothing here reads `Renderer.cursorRows`,
// which is the number the renderer believes it wrote.

const (
	// caretBoundaryRows / caretBoundaryCols are the terminal of the report: 24
	// rows is where editorRowsCap (12) — not the derived budget (20) — is the
	// operative bound, so the editing line is CLIPPED and the scrolling window is
	// in force. The boundary the case tests is therefore `buffer rows > 12`.
	caretBoundaryRows = 24
	caretBoundaryCols = 40
	// caretBoundaryWait bounds a wait on the read loop (the paste case only): the
	// GRID is the assertion, the wait only turns a hang into a screen dump.
	caretBoundaryWait = 3 * time.Second
	// caretBoundaryPoll is that wait's polling interval.
	caretBoundaryPoll = 2 * time.Millisecond
	// caretBoundaryPasteLines is the pasted size the epic names (>= 14 rows: past
	// the cap of 12 plus the two chrome rows).
	caretBoundaryPasteLines = 16
	// caretBoundaryShortRows is the input a redraw SHRINKS to, in the direction the
	// epic names as `drawnRows > len(rows)`: few enough rows that the scrolling
	// window is gone entirely, so the renderer's erase phase is what has to cover
	// the rows the windowed drawing left.
	caretBoundaryShortRows = 6
)

// caretBoundaryText is the input: one numbered row per buffer row, so a row's
// identity is readable straight off the grid and a caret on the wrong row is a
// one-line diff.
func caretBoundaryText(n int) string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("L%02d", i+1)
	}
	return strings.Join(rows, "\n")
}

// caretBoundaryGridLastTextRow is the greatest grid row inside the editing line's
// drawn span that carries one of the input's OWN rows, by equality with the
// layout — the render-truth reading of "the input's last row on screen" (a marked
// row is not one of `all`, so it cannot be mistaken for text; neither is the key
// row). It returns the row index, or -1 when no drawn row carries input.
func caretBoundaryGridLastTextRow(h *a5Harness, top, drawn int, all []string) int {
	last := -1
	for r := top; r < top+drawn; r++ {
		line := h.screen.Line(r)
		for _, want := range all {
			if line == want {
				last = r
				break
			}
		}
	}
	return last
}

// assertCaretOnItsOwnRow is the invariant the report violates: the grid's cursor
// must sit on the row carrying the row of input the caret is on — and, for the
// caret at the end of the buffer, that is the LAST row of input the editing line
// draws. Both halves are read off the grid.
func assertCaretOnItsOwnRow(t *testing.T, h *a5Harness, path string) {
	t.Helper()
	h.replay()
	p := h.region.Plan()
	win := h.ed.render.Window()
	text := h.ed.Value().Text()
	all, laidRow, _ := layoutText(h.ed.render.prompt, text, h.ed.Value().Cursor(), h.ed.region.Cols())

	if win.rows == 0 || len(all) < p.EditorRows {
		t.Fatalf("%s: this case needs the input to fill the editing line's rows (buffer rows=%d, EditorRows=%d, window rows=%d) — the premise of the boundary was not reached",
			path, len(all), p.EditorRows, win.rows)
	}
	// PREMISE, second half: the boundary's own shape is on screen — at least one
	// row of the editing line is a MARKED row (so the caret's drawn index and its
	// buffer row are not the same number, which is the whole boundary condition).
	marked := 0
	for i := 0; i < win.rows; i++ {
		if win.windowOffsetsAt(i) == windowChromeOffset {
			marked++
		}
	}
	if len(all) > p.EditorRows && marked == 0 {
		t.Fatalf("%s: the input outgrows the editing line (%d buffer rows, %d editing rows) and yet the window drew NO marked row — the boundary shape is not on screen\n--- screen ---\n%s",
			path, len(all), p.EditorRows, h.screen.Contents())
	}
	cursorRow := h.screen.CurRow
	lastTextRow := caretBoundaryGridLastTextRow(h, p.EditorTop, win.rows, all)

	// (1) The grid's cursor is on a row of the INPUT — the row the layout put the
	// caret on.
	if got, want := h.screen.Line(cursorRow), all[laidRow]; got != want {
		t.Errorf("%s: the cursor is on grid row %d, which carries %q — the caret's own row is %q (buffer row %d of %d), drawn elsewhere on the editing line\n--- screen ---\n%s",
			path, cursorRow, got, want, laidRow, len(all), h.screen.Contents())
	}
	// (2) With the caret at the END of the buffer nothing of the input follows it,
	// so the caret's row is the LAST input row the editing line draws. A caret one
	// row above it is the reported symptom.
	if lastTextRow < 0 {
		t.Fatalf("%s: no drawn row of the editing line carries input\n--- screen ---\n%s", path, h.screen.Contents())
	}
	if h.ed.Value().Cursor() == len([]rune(text)) && cursorRow != lastTextRow {
		t.Errorf("%s: the cursor is on grid row %d while the editing line's last row of input is grid row %d — the caret is NOT on the input's last row (the reported one-row jump)\n--- screen ---\n%s",
			path, cursorRow, lastTextRow, h.screen.Contents())
	}
	// (3) The renderer's own published caret row must agree with the grid: the
	// layout's EditorTop plus the window's caret row is the screen row the cursor
	// ends on. A disagreement here is the coordinate defect stated in one number.
	if want := p.EditorTop + win.cursorRow; cursorRow != want {
		t.Errorf("%s: the grid's cursor row is %d, but the window published caret row %d (EditorTop %d) = %d — the renderer's own coordinate does not describe the screen\n--- screen ---\n%s",
			path, cursorRow, win.cursorRow, p.EditorTop, want, h.screen.Contents())
	}
}

// TestWindowCaretRow_AtTheRowBudgetBoundary drives the input across the boundary
// (12 rows of editing line, 13..15 rows of input) and asserts the caret's grid row
// at each height. 12 is the last height at which the whole line is drawn and the
// caret is the editing line's last row; 13 is the first height at which the window
// draws a marked row — the height the operator reported.
func TestWindowCaretRow_AtTheRowBudgetBoundary(t *testing.T) {
	for _, n := range []int{editorRowsCap, editorRowsCap + 1, editorRowsCap + 2, editorRowsCap + 3} {
		t.Run(fmt.Sprintf("%drows", n), func(t *testing.T) {
			h := newA5HarnessSized(t, nil, nil, caretBoundaryRows, caretBoundaryCols)
			h.ed.val.SetText(caretBoundaryText(n))
			h.ed.repaint()
			h.replay()
			assertCaretOnItsOwnRow(t, h, fmt.Sprintf("%d rows of input", n))
		})
	}
}

// TestWindowCaretRow_ShrinkBackAcrossTheBoundary is the OTHER direction the epic
// names: the drawn row count EXCEEDS the rows the new input needs
// (drawnRows > len(rows)), so the redraw must erase the rows the longer drawing
// left AND still leave the caret on the last row of the input that remains.
//
// It runs the epic's own direction (15 rows → 13 rows: still windowed, the window
// MOVES within the same drawn height) and then the direction that makes
// `drawnRows > len(rows)` literal (13 rows → 6 rows: the window is gone and the
// renderer must erase the six rows the previous drawing left below the new one).
func TestWindowCaretRow_ShrinkBackAcrossTheBoundary(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, caretBoundaryRows, caretBoundaryCols)
	h.ed.val.SetText(caretBoundaryText(caretBoundaryPasteLines - 1))
	h.ed.repaint()
	h.replay()

	h.ed.val.SetText(caretBoundaryText(editorRowsCap + 1))
	h.ed.repaint()
	h.replay()
	assertCaretOnItsOwnRow(t, h, "15 rows shrunk to 13")

	// The literal `drawnRows > len(rows)` step: the previous drawing was 12 rows
	// tall, the new one is 6, so the renderer's `total = max(len(rows), drawnRows)`
	// is the branch under way — every withdrawn row must be ERASED and the caret
	// must end on the new drawing's last row.
	leftover := h.ed.render.DrawnRows()
	h.ed.val.SetText(caretBoundaryText(caretBoundaryShortRows))
	h.ed.repaint()
	h.replay()
	shortDrawn := h.ed.render.DrawnRows()
	if leftover <= shortDrawn {
		t.Fatalf("this step needs drawnRows > len(rows): the previous drawing was %d rows and the new one is %d",
			leftover, shortDrawn)
	}
	p := h.region.Plan()
	if got, want := h.screen.CurRow, p.EditorTop+shortDrawn-1; got != want {
		t.Fatalf("after shrinking %d rows of input to %d (drawn %d rows, was %d) the cursor is on grid row %d, want the last row of the new drawing %d\n--- screen ---\n%s",
			editorRowsCap+1, caretBoundaryShortRows, shortDrawn, leftover, got, want, h.screen.Contents())
	}
	// The withdrawn rows are ERASED, not left holding the longer input's tail.
	for r := p.EditorTop + shortDrawn; r < p.EditorTop+leftover; r++ {
		if got := h.screen.Line(r); got != "" {
			t.Fatalf("grid row %d still carries %q after the drawing shrank from %d to %d rows — the redraw left a withdrawn row painted\n--- screen ---\n%s",
				r, got, leftover, shortDrawn, h.screen.Contents())
		}
	}
	// Nothing of the withdrawn longer drawing survives inside the editing line's
	// span: every row of it carries either input or a marked row.
	win := h.ed.render.Window()
	all, _, _ := layoutText(h.ed.render.prompt, h.ed.val.Text(), h.ed.val.Cursor(), h.ed.region.Cols())
	for i := 0; i < win.rows; i++ {
		line := h.screen.Line(p.EditorTop + i)
		if line == "" {
			t.Fatalf("editing row %d is BLANK — the shorter redraw did not fill a row it drew\n--- screen ---\n%s",
				p.EditorTop+i, h.screen.Contents())
		}
		known := windowMarkerText(line)
		for _, want := range all {
			if line == want {
				known = true
			}
		}
		if !known {
			t.Fatalf("editing row %d carries %q, which is neither input nor a marked row — residue of the longer drawing\n--- screen ---\n%s",
				p.EditorTop+i, line, h.screen.Contents())
		}
	}
}

// TestWindowCaretRow_UnderARealBracketedPaste is the same assertion driven by the
// editor's OWN read loop: a real bracketed paste of caretBoundaryPasteLines rows
// arrives as input bytes and is decoded, inserted and painted by the production
// path (NewChatEditor + EnableRegion; the scripted source reports the terminal
// height). The caret's grid row is then read exactly as the operator would see it.
func TestWindowCaretRow_UnderARealBracketedPaste(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, caretBoundaryRows, caretBoundaryCols)
	src, ok := h.ed.src.(*scriptedSource)
	if !ok {
		t.Fatalf("the harness source is %T, want *scriptedSource", h.ed.src)
	}
	src.holdAtEnd = true
	src.push(scriptStep{data: []byte(PasteStartSeq + caretBoundaryText(caretBoundaryPasteLines) + PasteEndSeq)})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		// deliberately ignored: the harness only drives input — the Result of the
		// paste is another case's subject, and the grid below is the assertion.
		_, _ = h.ed.Run()
	}()
	t.Cleanup(func() {
		src.stopSource()
		select {
		case <-exited:
		case <-time.After(caretBoundaryWait):
			t.Errorf("the editor did not return after the source was stopped\n--- screen ---\n%s", h.screen.Contents())
		}
	})

	deadline := time.Now().Add(caretBoundaryWait)
	for {
		h.replay()
		if h.ed.val.Text() == caretBoundaryText(caretBoundaryPasteLines) && h.ed.render.DrawnRows() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the paste never landed: the buffer holds %d runes, want the %d pasted\n--- screen ---\n%s",
				len([]rune(h.ed.val.Text())), len([]rune(caretBoundaryText(caretBoundaryPasteLines))), h.screen.Contents())
		}
		time.Sleep(caretBoundaryPoll) //stylegate:allow-sleep bounded poll (caretBoundaryWait) for the editor's read loop to paint the pasted input — the assertion is the grid, not the wait
	}
	assertCaretOnItsOwnRow(t, h, "a real bracketed paste")
}
