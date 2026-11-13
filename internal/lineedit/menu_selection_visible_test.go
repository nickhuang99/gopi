package lineedit

import (
	"os"
	"strings"
	"testing"
	"time"
)

// menu_selection_visible_test.go — the regression for the reported "the menu's
// SELECTED row is not visible on a small terminal" (task 01a0f253).
//
// THE DEFECT (measured 2026-09-30). The editor sizes the menu it publishes with
// `region.MaxMenuRows(editorRows)` (editor.clampedMenu) so a menu taller than the
// room shrinks instead of overrunning the status row. That number was computed
// from `computePlan(rows, editorRows, 0)` — a plan with NO MENU IN IT — where the
// menu's rows go to the BAND (computePlan's step 4 is the residual) and MenuTop
// therefore lands ON the status row: `StatusRow - MenuTop` is ZERO at every
// terminal height, the clamp became `WithPage(0)`, and the page kept its full
// length. Measured on a real 117x8 pty with the production five-command `/` menu:
// the menu was drawn on rows 3..7 while the layout's status row IS row 7, so the
// status line was gone from the grid (evidence:
// out/small8-117x8-small8-menuopen.grid.txt, out/small10-…, in the session dir).
//
// The report's own symptom — the selection marker "invisible" on the menu's first
// row — is what a menu drawn past the room LOOKS LIKE once the rows the layout
// reserved and the rows painted disagree (the invariant these assertions pin:
// every painted menu row lies in [MenuTop, StatusRow-1]).
//
// WHY THIS GEOMETRY. The case needs the menu's wanted page to EXCEED the room the
// layout leaves, which needs a terminal SHORT enough that
// `rows - statusRows - editorRows - regionRowsMin < completionPageRows`: the
// package's own a7 screen (12 rows) always has room, so this case owns its
// geometry — the same reason the one-candidate case owned its own vocabulary.

const (
	// menuRoomRows is the terminal height at which the full five-row page does
	// NOT fit above the status row (the case's whole point). It is above
	// RowsMinUsable, so the reserved layout is installed and the plan is real.
	menuRoomRows = 8
	// menuRoomCols keeps the stub labels on one line, as a7Cols does.
	menuRoomCols = 40
	// magicSlash is the single character that opens the slash menu (the same
	// one the a7 tests type, spelled here so this case owns its own vocabulary).
	magicSlash = "/"
)

// menuRoomVocab is a FULL page: as many candidates as completionPageRows, which
// is the "the menu's rows == the capacity" shape the report asks to be pinned —
// with fewer offers the page would fit whatever the room, and the clamp would
// never be reached (the trap the one-candidate menu shipped through).
var menuRoomVocab = []Candidate{
	{Text: "/magic-one", Hint: "first"},
	{Text: "/magic-two", Hint: "second"},
	{Text: "/magic-three", Hint: "third"},
	{Text: "/magic-four", Hint: "fourth"},
	{Text: "/magic-five", Hint: "fifth"},
}

// newMenuRoomHarness is newA7Harness at an ARBITRARY geometry. It is a copy of
// that constructor's body with the two dimensions parameterised, because the
// harness's own helpers (menuTopRow) walk to the package's a7Rows constant — a
// case that needs a SHORTER screen cannot borrow them. Everything else — the
// notification pipe that really interrupts the parked read, the cleanup ORDER —
// is deliberately identical to the package's harness.
func newMenuRoomHarness(t *testing.T, stub *stubCompleter, rows, cols int) *a7Harness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: cols, height: rows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("notification pipe: %v", err)
	}
	src.setNotifyPipe(r, w)
	ed := NewChatEditor(src, out, promptForTest, nil, nil, stub.complete)
	ed.SetCompletionNotify(func() { _, _ = w.Write([]byte{0}) }) //stylegate:ignore best-effort notification: a pipe that refuses the byte is already unreadable and the fallback cadence covers it
	ed.SetClock(clock.Now)
	t.Cleanup(func() { _ = w.Close() }) //stylegate:ignore teardown of a pipe the test owns: an error here has nothing left to be reported to
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", rows)
	}
	h := &a7Harness{
		ed: ed, out: out, screen: NewScreen(cols, rows), region: ed.region,
		stub: stub, src: src, done: make(chan Result, 1), exited: make(chan struct{}),
		notifyW: w,
	}
	go func() {
		defer close(h.exited)
		res, err := ed.Run()
		if err != nil {
			h.done <- Result{Line: "ERR: " + err.Error()}
			return
		}
		h.done <- res
	}()
	t.Cleanup(func() {
		h.stop()
		ed.Close()
	})
	return h
}

// menuRowOffersCandidate reports whether a rendered row shows any of the stub's
// offers. The menu's rows are found by their CONTENT (the labels), never by the
// selection marker alone: after Down the marker is on a later row, and a
// marker-only search would mistake the menu's top for the row the marker moved
// to.
func (h *a7Harness) menuRowOffersCandidate(row int) bool {
	line := h.screen.Line(row)
	for _, c := range h.stub.offers() {
		if strings.Contains(line, c.Text) {
			return true
		}
	}
	return false
}

// menuRowsOnScreen collects the rows the menu occupies, walking down from the
// editing line. A menu is CONTIGUOUS below the editing line (one erase-and-write
// per row), so the walk stops at the first row that offers no candidate.
func (h *a7Harness) menuRowsOnScreen() []int {
	h.replay()
	var rows []int
	for row := h.region.EditorTop() + 1; row < h.screen.Rows; row++ {
		if !h.menuRowOffersCandidate(row) {
			break
		}
		rows = append(rows, row)
	}
	return rows
}

// TestA7_MenuStaysInsideTheRoomAndShowsItsSelectedRow is the regression: on a
// terminal too short for the full page, the painted menu must (1) START on the
// layout's own MenuTop row and show an offer there — the selected row is visible,
// which is the operator's report — and (2) END above the status row, so the
// layout's reservation and the painted rows cannot disagree.
func TestA7_MenuStaysInsideTheRoomAndShowsItsSelectedRow(t *testing.T) {
	stub := &stubCompleter{result: menuRoomVocab}
	h := newMenuRoomHarness(t, stub, menuRoomRows, menuRoomCols)
	h.typeKeys(t, magicSlash)
	h.waitFor(t, "the completion menu to open below the editing line", func() bool {
		return len(h.menuRowsOnScreen()) > 0
	})

	statusRow := h.region.StatusRow()
	editorRows := h.region.EditingRows()
	want := h.region.MaxMenuRows(editorRows)
	rows := h.menuRowsOnScreen()
	if len(rows) == 0 {
		t.Fatalf("no menu rows on screen after typing %q\n--- screen ---\n%s", magicSlash, h.screen.Contents())
	}

	// (1) THE SELECTED ROW IS DRAWN. The menu's first row is the layout's own
	// MenuTop, and it must carry an offer — the default selection's row, which
	// the operator could not see.
	if rows[0] != h.region.Plan().MenuTop {
		t.Fatalf("the menu's first painted row is %d; the layout's MenuTop is %d — the layout reserves rows the paint does not use\n--- screen ---\n%s",
			rows[0], h.region.Plan().MenuTop, h.screen.Contents())
	}
	if !h.menuRowOffersCandidate(rows[0]) {
		t.Fatalf("the menu's first row (%d) offers no candidate — the row the default selection sits on is blank\n--- screen ---\n%s",
			rows[0], h.screen.Contents())
	}

	// (2) THE MENU LEAVES THE STATUS ROW ALONE. This is the half the inert clamp
	// broke: with the room mis-resolved the page kept its full length and its
	// last row was painted ON the status row.
	if last := rows[len(rows)-1]; last >= statusRow {
		t.Fatalf("the menu is painted through row %d, and the layout's status row is %d — the menu covers the status line (painted %d rows, the room holds %d)\n--- screen ---\n%s",
			last, statusRow, len(rows), want, h.screen.Contents())
	}
	if len(rows) > want {
		t.Fatalf("the menu painted %d rows; the layout hosts %d — the clamp did not shrink the page\n--- screen ---\n%s",
			len(rows), want, h.screen.Contents())
	}
}
