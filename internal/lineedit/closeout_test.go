package lineedit

import (
	"strings"
	"testing"
	"time"
)

// closeout_test.go — the editor's CLOSE-OUT: what the user is left looking at
// when the chat REPL ends.
//
// The defect (task 01a0d508, attachment tmp/ui_cleanup.txt): on /quit the
// reserved layout was not cleaned up. The scroll region was taken down, so the
// terminal's frame was restored — but the ROWS the layout owned kept whatever
// was last painted on them and the cursor stayed parked on the editing row, so
// the shell's own next prompt was drawn INSIDE the leftover layout. Measured on
// the real PTY (termgrid.py, 100x30, before the fix): after /quit the grid still
// held the `you>` editing row and the status row, and the next command ran on
// the `you>` row.
//
// The INSTRUMENT question comes first and is the reason the defect survived:
// every other assertion in this package reads the screen while the layout is
// LIVE (Line/Contents with the layout installed), so "is anything of ours left
// afterwards" had nothing to read it with. The assertions below read the
// rendered cell grid AFTER the teardown, and the cursor's position IN it —
// because the cursor is exactly the half a repaint cannot fix.

const (
	// closeoutBandText is a committed answer line's marker: a non-empty string,
	// so "the row is gone" is an assertion about content, not about blanks.
	closeoutBandText = "committed-answer-line"
	// closeoutStatusText is the status row's own text (the row's left label), so
	// the row the close-out must erase is driven through the production road
	// (the status source).
	closeoutStatusText = "closeout-status"
	// closeoutReadyText is the status row's live half, so the row is a complete
	// status line rather than a label with an empty right side.
	closeoutReadyText = "ready"
	// closeoutTyped is the text on the editing row — a marker, so "the editing
	// row is gone" is an assertion about content and not about a blank prompt.
	closeoutTyped = "typed-here"
	// closeoutWait bounds a wait for the editor's read loop to apply what the
	// test published. Generous: a passing run never comes near it, and the wait
	// is synchronization — the cell comparisons are the assertion.
	closeoutWait     = 3 * time.Second
	closeoutPollStep = time.Millisecond
)

// TestEditorCloseOutClearsTheReservedLayoutAndParksTheCursor is the close-out
// contract, asserted on the cell grid AFTER the teardown.
//
// It drives the REAL editor path (a5Harness: the editor production builds, with
// the reserved layout enabled and a scripted source), paints the layout's rows
// through the editor's own renderer, closes out, and then reads the screen.
//
// RED PROOF: revert Editor.CloseOut to `DisableRegion()` (the pre-fix
// behaviour) and the erasure assertions fire — the band line, the status row and
// the prompt are all still painted, and the cursor is still on the editing row.
func TestEditorCloseOutClearsTheReservedLayoutAndParksTheCursor(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	if !h.ed.RegionActive() {
		t.Fatal("the reserved layout is not active on this geometry — the test would prove nothing")
	}
	p := h.region.Plan()

	// Paint the rows the leftover screen carried, through production's own roads:
	// the prompt + a typed line on the editing row (the editor's repaint), an
	// answer line committed in the band (the answer renderer), and the status row
	// (the answer renderer's status draw).
	h.answer.SetCols(a5Cols)
	h.ed.prompt = promptForTest
	h.ed.render.Prompt(promptForTest)
	h.ed.val.SetText(closeoutTyped)
	h.ed.repaint()
	h.answer.SetStatus(func() Status { return Status{Label: closeoutStatusText, Live: closeoutReadyText} })
	h.answer.Begin()
	h.answer.Delta(closeoutBandText + "\n")
	h.answer.Tick()
	h.replay()

	for _, want := range []string{closeoutBandText, closeoutStatusText, promptForTest} {
		if row := screenRowOf(h.screen, want); row < 0 {
			t.Fatalf("%q is not on the screen before the close-out — the setup proved nothing\n--- screen ---\n%s",
				want, h.screen.Contents())
		}
	}

	if !h.ed.CloseOut() {
		t.Fatal("CloseOut() = false, want true — an ACTIVE layout must report that it owned the screen")
	}
	h.replay()
	t.Logf("screen after CloseOut (band bottom %d, editor top %d, status row %d):\n%s",
		p.BandBottom, p.EditorTop, p.StatusRow, h.screen.Contents())

	// Nothing the layout drew survives anywhere on the screen. The prompt is
	// checked as TEXT on some row rather than by row number: asking about the
	// layout's own rows is the wrong question, because the whole complaint is
	// that those rows are stale.
	for _, want := range []string{closeoutBandText, closeoutStatusText, promptForTest, closeoutTyped} {
		if row := screenRowOf(h.screen, want); row >= 0 {
			t.Fatalf("after CloseOut the screen still carries %q at row %d — the layout's rows were not erased\n--- screen ---\n%s",
				want, row, h.screen.Contents())
		}
	}
	// The cursor is at the screen's first cell: this is the half a repaint cannot
	// fix, and the reason the shell's own prompt landed inside the leftovers.
	if h.screen.CurRow != 0 || h.screen.CurCol != 0 {
		t.Fatalf("after CloseOut the cursor is at (%d,%d), want (0,0) — the next program draws where the editor left it\n--- screen ---\n%s",
			h.screen.CurRow, h.screen.CurCol, h.screen.Contents())
	}
	// The layout is no longer published as active.
	if h.ed.RegionActive() {
		t.Fatal("after CloseOut the layout is still published as ACTIVE")
	}
	if top, bottom := h.screen.ScrollRegion(); top != 0 || bottom != h.screen.Rows-1 {
		t.Fatalf("after CloseOut the scroll region = [%d,%d], want the whole screen [0,%d]",
			top, bottom, h.screen.Rows-1)
	}
}

// TestEditorCloseOutWithoutARegionIsANoOp is CloseOut's other half: on the
// historic path (no reserved layout — the cooked input, or a terminal that could
// not host one) nothing of ours is on the screen, so the close-out must claim
// nothing and write nothing. A close-out that erased the screen here would wipe
// the scrollback of a session that never installed a layout.
func TestEditorCloseOutWithoutARegionIsANoOp(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	h.ed.DisableRegion()
	h.replay()
	before := h.out.count()
	if h.ed.CloseOut() {
		t.Fatal("CloseOut() = true with no active layout, want false — it must not claim the screen")
	}
	h.replay()
	if got := h.out.count(); got != before {
		t.Fatalf("CloseOut wrote %d chunk(s) with no active layout, want none", got-before)
	}
}

// screenRowOf is the screen row whose text contains want, or -1 — the cell
// grid's own answer to "is this still on screen".
func screenRowOf(s *Screen, want string) int {
	for row := 0; row < s.Rows; row++ {
		if strings.Contains(s.Line(row), want) {
			return row
		}
	}
	return -1
}
