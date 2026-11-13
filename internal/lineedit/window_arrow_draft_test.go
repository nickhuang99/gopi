package lineedit

import "testing"

// window_arrow_draft_test.go — the ARROW'S two meanings must never cost the user
// their own text.
//
// The defect these tests close (data loss, not a display one): once the editing
// line has more rows than its window can show, Up/Down are the WINDOW's keys
// (window.go) — but the arm that decided that asked "did the window MOVE"
// instead of "is anything hidden". At the near end of the window the scroll
// reported no movement, the key fell through to the history, and `History.Prev`
// REPLACES the buffer (Value.SetText): a multi-line draft the operator had
// written was gone on a keystroke, with no second copy anywhere.
//
// What is fixed, and what these tests pin:
//
//   - a window with hidden rows makes the arrow a WINDOW key and NOTHING else —
//     at the top it is inert, never the history;
//   - a line that fits (the ordinary single-line prompt) keeps the arrow as its
//     history motion, exactly as before;
//   - the scroll itself still works: the hidden rows are reached by pressing Up.

// The arrow's wire spellings, read by the REAL decoder (keys.go) rather than
// constructed as Key values: the sequence a terminal sends is the input this
// defect is about.
const (
	windowArrowUpSeq   = "\x1b[A"
	windowArrowDownSeq = "\x1b[B"
)

// arrowDraftHistoryLine is the history entry a fall-through would substitute for
// the draft: a line that cannot be mistaken for the draft text.
const arrowDraftHistoryLine = "an old submitted command"

// arrowDraftLiveLine is the one-row line the recall tests type: it fits the
// editing line, so the arrow keeps its history meaning there.
const arrowDraftLiveLine = "a line that fits"

// arrowDraftPresses is how many times the arrow is pressed past the window's own
// height: enough to walk the window to its end (so the edge case the defect lives
// at is really reached) and then to press it again several times while it is
// inert.
const arrowDraftPresses = a5Rows * 2

// pressEditorKeys drives one key sequence through the real decoder and the real
// handleKey — the same road the read loop takes, without a goroutine in the way.
func pressEditorKeys(t *testing.T, ed *Editor, seq string) {
	t.Helper()
	for _, k := range ed.dec.Feed([]byte(seq)) {
		if _, done, err := ed.handleKey(k); err != nil || done {
			t.Fatalf("handleKey(%q): done=%v err=%v", seq, done, err)
		}
	}
}

// TestWindow_ArrowKeysNeverReplaceAMultilineDraft is the regression: a draft
// taller than the window survives every arrow press, at both ends and past them.
//
// It fails before the fix at the FIRST press that finds the window already at its
// end: pre-fix the buffer reads the recalled history line, and the text the
// operator wrote exists nowhere.
func TestWindow_ArrowKeysNeverReplaceAMultilineDraft(t *testing.T) {
	hist := NewHistory(0, "")
	if err := hist.Add(arrowDraftHistoryLine); err != nil {
		t.Fatalf("History.Add: %v", err)
	}
	h := newA5HarnessSized(t, nil, hist, a5Rows, a5Cols)
	draft := windowPastedLines(windowThirtyLines)
	h.ed.val.SetText(draft)
	h.ed.repaint()
	h.replay()

	if !h.ed.windowHidesRows() {
		t.Fatalf("the fixture drew no hidden rows: a %d-line draft in a %d-row terminal must outgrow the editing line's window, or this test measures nothing\n--- screen ---\n%s",
			windowThirtyLines, a5Rows, h.screen.Contents())
	}
	win := h.ed.render.Window()
	if win.above == 0 || win.below != 0 {
		t.Fatalf("the fixture's window is not at the caret's end: %d above / %d below, want every hidden row above it (the caret is at the buffer's end)", win.above, win.below)
	}

	// Up: the window's own key. The window walks toward the buffer's head over
	// the hidden rows and is then INERT — never the history. (Walked first, so the
	// presses really reach the edge the defect lives at: a version of the fix that
	// only handled the first press would fail here.)
	for i := 0; i < arrowDraftPresses; i++ {
		pressEditorKeys(t, h.ed, windowArrowUpSeq)
		if got := h.ed.val.Text(); got != draft {
			t.Fatalf("Up press %d replaced the %d-line draft with %q — the operator's own text is gone and there is no second copy of it\n--- screen ---\n%s",
				i+1, windowThirtyLines, got, h.screen.Contents())
		}
	}
	// The mechanism still WORKS: those presses reached the head of the buffer, so
	// the key is not merely dead everywhere (which would also pass the loop above).
	h.ed.repaint()
	h.replay()
	win = h.ed.render.Window()
	if win.above != 0 {
		t.Fatalf("after %d Up presses the window still states %d rows hidden above it — the key never moved the window\n--- screen ---\n%s",
			arrowDraftPresses, win.above, h.screen.Contents())
	}
	if win.below == 0 {
		t.Fatalf("after %d Up presses the window states nothing hidden below it, so the fixture stopped outgrowing the window\n--- screen ---\n%s",
			arrowDraftPresses, h.screen.Contents())
	}
	if got := h.ed.val.Text(); got != draft {
		t.Fatalf("the buffer after scrolling to the head = %q, want the whole %d-line draft", got, windowThirtyLines)
	}

	// Down: the same contract at the other end.
	for i := 0; i < arrowDraftPresses; i++ {
		pressEditorKeys(t, h.ed, windowArrowDownSeq)
		if got := h.ed.val.Text(); got != draft {
			t.Fatalf("Down press %d replaced the %d-line draft with %q\n--- screen ---\n%s",
				i+1, windowThirtyLines, got, h.screen.Contents())
		}
	}
	h.ed.repaint()
	h.replay()
	if win := h.ed.render.Window(); win.below != 0 || win.above == 0 {
		t.Fatalf("after %d Down presses the window states %d above / %d below, want the caret's end reached again (below 0)",
			arrowDraftPresses, win.above, win.below)
	}
}

// TestWindow_ArrowRecallSurvivesOnALineThatFits is the other half of the split:
// with NOTHING hidden the arrow is the history motion it has always been — Up
// shows the previous entry on the editing row, and Down walks back to the live
// line the operator was typing (the pending text History kept when it left it).
func TestWindow_ArrowRecallSurvivesOnALineThatFits(t *testing.T) {
	hist := NewHistory(0, "")
	if err := hist.Add(arrowDraftHistoryLine); err != nil {
		t.Fatalf("History.Add: %v", err)
	}
	h := newA5HarnessSized(t, nil, hist, a5Rows, a5Cols)
	h.ed.val.SetText(arrowDraftLiveLine)
	h.ed.repaint()
	h.replay()
	if h.ed.windowHidesRows() {
		t.Fatalf("a one-row line opened a window with hidden rows — the fixture is wrong, not the key\n--- screen ---\n%s", h.screen.Contents())
	}

	pressEditorKeys(t, h.ed, windowArrowUpSeq)
	if got := h.ed.val.Text(); got != arrowDraftHistoryLine {
		t.Fatalf("Up on a line that fits = %q, want the recalled %q", got, arrowDraftHistoryLine)
	}
	h.ed.repaint()
	h.replay()
	if got := h.screen.Line(h.region.Plan().EditorTop); got != "you> "+arrowDraftHistoryLine {
		t.Fatalf("the editing row = %q, want the recalled line on screen\n--- screen ---\n%s", got, h.screen.Contents())
	}

	pressEditorKeys(t, h.ed, windowArrowDownSeq)
	if got := h.ed.val.Text(); got != arrowDraftLiveLine {
		t.Fatalf("Down past the newest entry = %q, want the live line %q back", got, arrowDraftLiveLine)
	}
}

// TestWindow_ArrowStillDrivesTheMenuSelectionWhenOpen is the precedence guard: an
// open completion menu is a LIST, so the arrow moves its selection — the window's
// hidden-row rule must never take the key away from the menu, and the editing line
// must stay untouched by a selection move.
func TestWindow_ArrowStillDrivesTheMenuSelectionWhenOpen(t *testing.T) {
	h := newA7Harness(t, &stubCompleter{})
	h.typeKeys(t, magicOpensTab)
	h.waitForMenu(t)

	first, ok := h.ed.MenuState().Selected()
	if !ok {
		t.Fatalf("the open menu offers no selection\n--- screen ---\n%s", h.rows())
	}
	h.typeKeys(t, "\x1b[B") // Down: the selection moves
	h.waitFor(t, "the selection to move off the first candidate", func() bool {
		h.replay()
		sel, ok := h.ed.MenuState().Selected()
		return ok && sel.Text != first.Text
	})

	h.typeKeys(t, "\x1b[A") // Up: and back
	h.waitFor(t, "the selection to come back to the first candidate", func() bool {
		h.replay()
		sel, ok := h.ed.MenuState().Selected()
		return ok && sel.Text == first.Text
	})
	if got := h.ed.val.Text(); got != magicOpensTab {
		t.Fatalf("the arrow changed the buffer to %q while the menu was open, want the typed %q untouched", got, magicOpensTab)
	}
	h.assertNotDone(t)
}
