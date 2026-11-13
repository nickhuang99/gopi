package lineedit

// The answer renderer draws through the layout the editor owns, and that layout
// MOVES while an answer is in flight: the editing line grows (the submitted
// message is laid out in full), the band contracts by the same rows, and the row
// the answer was mid-way through writing becomes the editing line's FIRST row —
// whose repaint (`ESC[<editorTop>;1H ESC[2K`) erases it.
//
// The renderer used to cache the band's live row and use it for the rest of the
// burst, so the head went to the erasing row while the tail (written after a
// fresh read) went to the band: the frozen artifact then held a row with its
// middle BLANKED, and the dump gate timed out waiting for a needle that row could
// never carry.
//
// These tests CONSTRUCT that interleaving — they move the layout between two
// bursts of one line — instead of sampling it: the defect is a race, and a
// sampled window costs about one hit per 200 iterations.

import (
	"strings"
	"testing"
)

// bandBurstHarness builds the A5 harness and paints ONE partial line, live.
//
// The clock is advanced first because the renderer's flush cadence is a WINDOW
// (answerFlushWindow): with a frozen clock a burst with no line break stays in
// the parking buffer and never reaches the live row, which is a different path
// from the one this file asserts on.
func bandBurstHarness(t *testing.T) (*a5Harness, plan) {
	t.Helper()
	h := newA5HarnessSized(t, nil, nil, a5Rows, a5WideCols)
	h.answer.SetCols(a5WideCols)
	before := h.region.Plan()
	h.answer.Begin()
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: promptForTest})
	h.clock.advance(answerFlushWindow)
	h.answer.Delta("the ")
	h.replay()
	return h, before
}

// TestBandWriteFollowsTheLayoutWhenTheEditingLineGrows is the regression guard:
// after the layout moves, the NEXT write of the in-flight line lands on the BAND,
// and nothing of the answer is left on the row the layout gave away — that row is
// the editing line's now, and the editor's repaint erases it.
//
// The burst that follows the move carries NO line break on purpose: a complete
// line goes through the commit path, which re-reads the live row anyway and would
// repair the split, hiding the defect this guard exists to catch.
func TestBandWriteFollowsTheLayoutWhenTheEditingLineGrows(t *testing.T) {
	h, before := bandBurstHarness(t)
	if got := h.screen.Line(before.BandBottom); !strings.Contains(got, "the") {
		t.Fatalf("the head did not reach the live row %d (%q): the interleaving this guard builds cannot be constructed",
			before.BandBottom, got)
	}

	// THE MOVE: the editing line claims a row the band owns, exactly as submit()
	// does when the message is laid out in full.
	if !h.region.SetEditorRows(before.EditorRows + 1) {
		t.Fatalf("SetEditorRows(%d) did not move the layout: the guard asserts on a move that did not happen",
			before.EditorRows+1)
	}
	after := h.region.Plan()
	if after.EditorTop == before.EditorTop {
		t.Fatalf("the editing line's top row did not move (%d): the guard would be vacuous", after.EditorTop)
	}

	// The continuation of the SAME line — still unfinished — one burst later. This
	// is the write that used to be placed on the row the layout had just given
	// away (the renderer's cached liveRow), where the editor's next repaint
	// erases it.
	h.clock.advance(answerFlushWindow)
	h.answer.Delta("**bold conclusion** follows")
	h.replay()

	// The needle is the line's RAW text: this burst carries no line break, so the
	// line is still IN FLIGHT and the renderer paints it with its markers intact
	// and without emphasis (the decision is taken when its break arrives) — and
	// the head is re-opened on the band with the same provisional form.
	const drawn = "the **bold conclusion** follows"
	found := false
	for row := after.BandTop; row <= after.BandBottom; row++ {
		if strings.Contains(h.screen.Line(row), drawn) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no band row %d..%d carries %q — the continuation was written to a row the layout no longer reserved for it\n--- screen ---\n%s",
			after.BandTop, after.BandBottom, drawn, h.screen.Contents())
	}
	// And nothing of the answer may sit below the band: those rows are the editing
	// line's, and its repaint erases them.
	for row := after.BandBottom + 1; row < h.screenRows; row++ {
		if got := h.screen.Line(row); strings.Contains(got, "bold conclusion") {
			t.Fatalf("row %d carries the answer (%q) but is below the band's last row %d (the editing line owns it, and its repaint erases it)\n--- screen ---\n%s",
				row, got, after.BandBottom, h.screen.Contents())
		}
	}
}

// TestLiveRowIsReadFromTheLayoutNotCached pins the mechanism at its own seam: the
// renderer's view of the band's live row FOLLOWS the layout, so a caller that
// moves the layout cannot leave the renderer painting one row below the band.
func TestLiveRowIsReadFromTheLayoutNotCached(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, a5Rows, a5WideCols)
	before := h.region.Plan()
	if got := h.answer.LiveRow(); got != before.BandBottom {
		t.Fatalf("LiveRow() = %d, want the band's live row %d", got, before.BandBottom)
	}
	if !h.region.SetEditorRows(before.EditorRows + 1) {
		t.Fatal("SetEditorRows did not move the layout")
	}
	after := h.region.Plan()
	if after.BandBottom == before.BandBottom {
		t.Fatalf("the band bottom did not move (%d): the assertion below would be vacuous", after.BandBottom)
	}
	if got := h.answer.LiveRow(); got != after.BandBottom {
		t.Fatalf("LiveRow() = %d after the layout moved, want %d — the renderer is reading a cached row",
			got, after.BandBottom)
	}
	// The width rides the same layout, for the same reason: a row wrapped for the
	// old geometry is cut by the new one.
	if got := h.answer.cols; got != h.region.Cols() {
		t.Fatalf("renderer cols = %d, region cols = %d", got, h.region.Cols())
	}
}
