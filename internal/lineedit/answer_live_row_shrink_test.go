package lineedit

// answer_live_row_shrink_test.go — THE MINIMAL REPRO for the artifact-level
// defect: a COMMITTED answer row disappears from the settled screen dump while a
// mid-delta frame still held it.
//
// THE DEFECT (measured 2026-09-26, session 01a0dd28): a chat turn's answer is
// painted on the band's live row, and the READ LOOP then raises the editing
// line's row wish — the submitted message wraps onto a second row, so
// submit() calls SetEditorRows(2) and region.layout() re-installs the scroll
// region ONE ROW SHORTER (`[1;23r` after `[1;24r`). The band's bottom moves from
// row 23 to row 22, so:
//
//   - the row the answer was committed on (23) is no longer inside the band, and
//   - the editing line's own erase/redraw — the `you>` row, which is drawn on
//     the band's bottom row — lands exactly on it.
//
// The answer is ERASED, and the settled dump (which is read at the end, when the
// screen has settled) no longer holds it: the mid-delta frame did. That is why
// the same input produced `prefix=32` on one run and `prefix=0` on the next —
// the visible outcome depends on whether a repaint happens to fall between the
// commit and the freeze.
//
// The property this test pins is the INVARIANT, not the one triggering sequence:
// the band's live row must never move onto a row a committed answer already
// occupies without that answer being finalized first. A row that has been
// committed to the screen is a row the user reads; a layout change that
// overwrites it is data loss the reader cannot distinguish from corruption —
// exactly what the dump's own `prefix=0 (the paint lost it)` reason reports.

import (
	"strings"
	"testing"
)

// TestAnswer_ShrinkingTheBandDoesNotEraseACommittedRow is the regression: a
// committed answer row survives a band shrink, and the settled screen still
// holds it.
//
// The shrink is the production one: the editing line's wish goes from 1 to 2
// rows (the submitted message wraps), which is the transition the captured pty
// stream carries as `ESC[1;24r` → `ESC[1;23r` (session 01a0dd28, byte 5166 —
// `settled/pty-raw.bin`, with `settled/dump-after.txt` showing the answer gone).
func TestAnswer_ShrinkingTheBandDoesNotEraseACommittedRow(t *testing.T) {
	const (
		cols = 117
		rows = 26
		// The answer: CJK + latin, the shape the epic's own repro uses — it is
		// also the shape whose wrap arithmetic differs from latin-only text, so
		// a width bug cannot pass here by accident.
		text = "自观测闭环：中文与 latin 混排的一行，dump 必须逐字符还原。"
	)
	var out strings.Builder
	screen := NewScreen(cols, rows)
	reg := newTestRegion(&screenWriting{screen: screen, tee: &out}, rows, cols)
	if !reg.Enable(rows, cols) {
		t.Fatalf("Enable(%d, %d) = false, want the layout installed", rows, cols)
	}
	answer := NewAnswerRenderer(reg, nil)
	answer.SetCols(cols)

	// One turn, streamed the way production streams it (Begin → Delta → Finish),
	// so the row is COMMITTED through the band's own promote-a-row path.
	answer.Begin()
	answer.Delta(text)
	answer.Finish()

	committed := rowHolding(screen, "自观测")
	if committed < 0 {
		t.Fatalf("the answer was not committed to the screen at all (rows scanned: %d)", rows)
	}

	// THE TRANSITION: the editing line needs a second row, exactly as submit()
	// asks for when the just-submitted message wraps.
	if !reg.SetEditorRows(2) {
		t.Fatalf("SetEditorRows(2) reported no layout change: the band did not shrink, so this case is not exercising the defect")
	}
	live := reg.LiveRow()
	if live != committed {
		// Still worth asserting: the case must actually put the live row where
		// the committed answer sat, or it is not the defect.
		t.Fatalf("the shrink moved the live row to %d but the answer sits on %d: the case did not reproduce the collision", live, committed)
	}

	// A repaint of the editing line after the shrink — what the read loop does
	// next — must not take the committed row with it.
	reg.place(live, 0)
	reg.emit(escEraseLine)
	reg.emit("you>")

	// THE ASSERTION: the answer is STILL ON THE SCREEN. It is not expected to
	// keep its row number — the band gives a row back from BELOW, so the rows it
	// keeps are carried one row UP by the terminal's own promotion path (an LF at
	// the region's bottom), which is exactly how a committed row leaves for the
	// scrollback instead of being discarded. What must never happen is the row
	// being ERASED: that is the artifact-level loss this case pins.
	where := rowHolding(screen, "自观测")
	if where < 0 {
		t.Errorf("the committed answer row is GONE from the settled screen: it was on row %d before the band shrank to the editing line's 2 rows, the live row moved onto it, and the editing-line repaint erased it."+
			"\n  the artifact a reader gets then reports prefix=0 — \"the paint lost it\" — for a line that WAS painted and was then overwritten."+
			"\n  live row after the shrink = %d, committed row = %d", committed, live, committed)
		return
	}
	if where == live {
		t.Errorf("the committed answer is sitting on the live row %d, where the editing line draws: the next repaint erases it (this is the pre-fix collision, unmoved)", live)
	}
	if live > reg.Plan().BandBottom {
		t.Errorf("the live row %d is outside the band [%d,%d]: a scroll there cannot promote a committed row", live, reg.Plan().BandTop, reg.Plan().BandBottom)
	}
	if want := committed - (committed - live + 1); where < want {
		t.Errorf("the answer was carried UP too far: row %d, want at least %d (the band lost %d rows, so the carry is %d)", where, want, committed-live, committed-live)
	}
}

// rowHolding returns the 0-based screen row whose text contains needle, or -1.
func rowHolding(s *Screen, needle string) int {
	for r := 0; r < s.Rows; r++ {
		if strings.Contains(s.Line(r), needle) {
			return r
		}
	}
	return -1
}

// screenWriting is a region sink that feeds every byte into the screen AND keeps
// a copy (so a failure can be diagnosed from the raw stream).
type screenWriting struct {
	screen *Screen
	tee    *strings.Builder
}

func (w *screenWriting) Write(p []byte) (int, error) {
	w.screen.Feed(p)
	if w.tee != nil {
		return w.tee.Write(p)
	}
	return len(p), nil
}
