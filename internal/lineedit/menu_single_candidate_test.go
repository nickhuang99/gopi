package lineedit

import (
	"strings"
	"testing"
)

// menu_single_candidate_test.go — the ONE-CANDIDATE menu (task 01a0e2e6).
//
// The operator's report: typing "/q" (a prefix that matches exactly one command,
// /quit) opened the menu with that sole row already marked active, and Arrow-Down
// changed NOTHING on screen. Menu.MoveSelection did (Index+delta)%n with n == 1 —
// a no-op — so the screen was byte-identical before and after the press, and an
// inert key reads as a broken shortcut.
//
// The decision this file pins, both halves:
//
//  1. a one-candidate menu is a one-ITEM list: Up/Down do not move the selection
//     (it CLAMPS — there is nowhere to move to), and
//  2. the menu STATES the reason on a row of its own, so the press is never a
//     silent no-op.
//
// Every assertion reads the RENDERED CELL GRID (Rule 10.2) — the bytes the editor
// wrote, replayed into a vt100 screen — never the menu struct's own state: "the
// selection did not move" is exactly what the defect looked like internally, and
// the whole point is what the user can see.
//
// The three-candidate stub every historic test is written against CANNOT reach
// this case (its every prefix matches < 3 candidates only in a shrinking window,
// and never exactly one on a settled line). That is why the defect shipped with
// a green suite, and why the vocabulary here is its OWN one-entry list.

// singleCandidate is the one-candidate harness's only offer: the operator's own
// case — "/q" reaching the single command "/quit".
var singleCandidate = []Candidate{{Text: "/quit", Hint: "end the session"}}

// singlePrefix is what the test types. It is a strict prefix of the single
// candidate, so the menu really opens on it (and Tab has something to complete).
const singlePrefix = "/qu"

// singleHarness is the a7 harness whose completer offers exactly one candidate.
func singleHarness(t *testing.T) *a7Harness {
	t.Helper()
	return newA7Harness(t, &stubCompleter{result: singleCandidate})
}

// singleRow is the sole candidate's screen row, located by its text (the layout's
// own row arithmetic is what the menu publishes, and the grid is what is read).
func singleRow(t *testing.T, h *a7Harness) int {
	t.Helper()
	h.replay()
	for row := 0; row < a7Rows; row++ {
		if strings.Contains(h.screen.Line(row), singleCandidate[0].Text) {
			return row
		}
	}
	t.Fatalf("no screen row offers %q\n--- screen ---\n%s", singleCandidate[0].Text, h.screen.Contents())
	return -1
}

// menuNoteVisible is the one-candidate note as the SCREEN shows it: the menu states
// the reason Up/Down cannot move, on the row below the sole candidate.
func menuNoteVisible(t *testing.T, h *a7Harness) string {
	t.Helper()
	h.replay()
	for row := 0; row < a7Rows; row++ {
		if line := h.screen.Line(row); strings.Contains(line, completionMenuNote) {
			return line
		}
	}
	return ""
}

// TestChatCompletion_OneCandidateMenuStatesWhyTheArrowsCannotMove is the
// regression for the defect this file's header describes.
//
// A prefix matching exactly one command opens a menu of ONE row; both Up and Down
// must leave that row selected AND the screen must SAY why (the note row). The
// buffer is untouched by an arrow press, so this also pins "the arrow keys do not
// disturb the typed line".
func TestChatCompletion_OneCandidateMenuStatesWhyTheArrowsCannotMove(t *testing.T) {
	h := singleHarness(t)
	h.typeKeys(t, singlePrefix)
	h.waitForMenu(t)

	row := singleRow(t, h)
	if got := h.screen.Line(row); !strings.HasPrefix(got, menuMarkerActive) {
		t.Fatalf("the sole candidate row = %q, want the selection marker %q on it (the list's only item is selected)\n--- screen ---\n%s",
			got, menuMarkerActive, h.rows())
	}
	// The menu's own row count INCLUDES the note row — the count cmd/agent's
	// renderer reads (Menu.Rows()) and the painted rows are one arithmetic.
	if got, want := h.ed.MenuState().Rows(), len(singleCandidate)+completionNoteRow; got != want {
		t.Fatalf("Menu.Rows() = %d for a %d-candidate menu, want %d — the note row must be inside the ONE row definition, or the layout reserves fewer rows than are painted",
			got, len(singleCandidate), want)
	}
	if note := menuNoteVisible(t, h); note == "" {
		t.Fatalf("the one-candidate menu states no reason on screen; want a row carrying %q\n--- screen ---\n%s",
			completionMenuNote, h.rows())
	}

	// What the row SAYS, asserted as the rendered cells (Rule 10.2): the reason
	// first, then the reassurance that the list has one item.
	if note := menuNoteVisible(t, h); !strings.HasPrefix(strings.TrimLeft(note, " "), completionMenuNote) {
		t.Fatalf("the note row = %q, want it to LEAD with the reason %q (a narrow terminal truncates the row's tail, never its reason)",
			note, completionMenuNote)
	}

	// DOWN: the selection does not move (it is the only item) and the screen
	// still states the reason. The press must not be a silent no-op, and the
	// assertion is on the SCREEN. "The key was handled at all" is measured off
	// the write stream: MenuSelect repaints unconditionally (the buffer did not
	// change, so the read loop's own repaint gate would not fire), so a press the
	// editor never consumed leaves the stream flat.
	before := h.rows()
	beforeWrites := h.out.count()
	h.typeKeys(t, "\x1b[B")
	h.waitFor(t, "the Down press to be handled", func() bool { return h.out.count() > beforeWrites })
	h.waitForQuietWrites(t)
	if got := h.screen.Line(singleRow(t, h)); !strings.HasPrefix(got, menuMarkerActive) {
		t.Fatalf("after Down the sole candidate row = %q, want the marker %q still on it\n--- screen ---\n%s",
			got, menuMarkerActive, h.rows())
	}
	if note := menuNoteVisible(t, h); note == "" {
		t.Fatalf("after Down the menu no longer states the reason (screen changed from\n%s\nto\n%s)", before, h.rows())
	}
	if got := h.screen.Line(h.editRow()); !strings.Contains(got, singlePrefix) {
		t.Fatalf("editing row after Down = %q, want the typed %q unchanged", got, singlePrefix)
	}

	// UP: the same contract, the other key.
	beforeWrites = h.out.count()
	h.typeKeys(t, "\x1b[A")
	h.waitFor(t, "the Up press to be handled", func() bool { return h.out.count() > beforeWrites })
	h.waitForQuietWrites(t)
	if got := h.screen.Line(singleRow(t, h)); !strings.HasPrefix(got, menuMarkerActive) {
		t.Fatalf("after Up the sole candidate row = %q, want the marker %q still on it\n--- screen ---\n%s",
			got, menuMarkerActive, h.rows())
	}
	if menuNoteVisible(t, h) == "" {
		t.Fatalf("after Up the menu no longer states the reason\n--- screen ---\n%s", h.rows())
	}
	// The candidate is still reachable: the press that does nothing visible must
	// not have cost the user the completion.
	h.typeKeys(t, "\t")
	h.waitFor(t, "Tab to complete the sole candidate onto the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.editRow()), singleCandidate[0].Text)
	})
	if got := strings.TrimRight(h.screen.Line(h.editRow()), " "); !strings.HasSuffix(got, singleCandidate[0].Text) {
		t.Fatalf("editing row after Tab = %q, want the sole candidate %q", got, singleCandidate[0].Text)
	}
}

// TestChatCompletion_NoteRowIsOnlyForAOneCandidateMenu is the other half of the
// decision: the note is a property of a ONE-ITEM list, never a decoration of
// every menu. A menu with two or more candidates keeps wrapping exactly as it did
// (Down from the last row returns to the first) and draws NO note row, and once
// the list narrows back to one it returns — the row follows the LIST, not a key.
func TestChatCompletion_NoteRowIsOnlyForAOneCandidateMenu(t *testing.T) {
	stub := &stubCompleter{result: []Candidate{
		{Text: "/magic-one"},
		{Text: "/magic-two"},
		{Text: "/magic-three"},
	}}
	h := newA7Harness(t, stub)
	h.typeKeys(t, magicOpensTab)
	h.waitForMenu(t)

	if n := h.ed.MenuState().Rows(); n != len(stub.offers()) {
		t.Fatalf("Menu.Rows() = %d for a %d-candidate menu, want the candidate count and no note row",
			n, len(stub.offers()))
	}
	for row := 0; row < a7Rows; row++ {
		if strings.Contains(h.screen.Line(row), completionMenuNote) {
			t.Fatalf("row %d = %q: a %d-candidate menu must draw NO one-candidate note\n--- screen ---\n%s",
				row, h.screen.Line(row), len(stub.offers()), h.rows())
		}
	}

	// WRAPPING IS UNCHANGED for 2+ candidates: Up from the FIRST row wraps to the
	// LAST row (the behaviour the one-item clamp must not have disturbed).
	h.typeKeys(t, "\x1b[A")
	h.waitFor(t, "the selection marker to wrap to the LAST menu row", func() bool {
		h.replay()
		top := h.menuTop(t)
		return strings.HasPrefix(h.screen.Line(top+len(stub.offers())-1), menuMarkerActive)
	})

	// Narrowing the list to ONE candidate brings the note back: the row follows
	// the list, not the history of which keys were pressed.
	// The suffix that completes the FIRST candidate's own name: the line becomes
	// "/magic-one" and exactly one offer is left.
	h.typeKeys(t, "ic-one")
	h.waitFor(t, "the one-candidate note row after narrowing to one candidate", func() bool {
		m := h.ed.MenuState()
		return m.Open && len(m.Candidates) == 1 && menuNoteVisible(t, h) != ""
	})
	if got := h.screen.Line(h.editRow()); !strings.Contains(got, magicOpensTab+"ic-one") {
		t.Fatalf("editing row = %q, want the narrowed line kept while the note appeared", got)
	}

	// Escape closes the menu: the note row goes with it (the rows are ERASED, not
	// merely left behind — a closed menu reserves nothing).
	h.typeKeys(t, "\x1b")
	h.src.push(scriptStep{timeout: true, advance: escWaitWindow()})
	h.waitFor(t, "the menu and its note row to be erased", func() bool {
		return !h.ed.MenuState().Open && menuNoteVisible(t, h) == ""
	})
	if n := h.ed.MenuState().Rows(); n != 0 {
		t.Fatalf("a closed menu reports Rows() = %d, want 0 (the note row is not free-standing)", n)
	}
}

// TestChatCompletion_NoteRowRespectsTheNarrowWidth is the note's cell truth on a
// terminal too narrow for the whole sentence: the row is TRUNCATED like every
// other menu row (it shares truncateCols), so it can never wrap onto — or erase —
// the rows below it. The width is the smallest one on which a menu row fits at
// all, which is where a row that forgot its bound would show.
func TestChatCompletion_NoteRowRespectsTheNarrowWidth(t *testing.T) {
	m := Menu{Open: true, Candidates: singleCandidate}
	for _, width := range []int{menuNarrowWidth, a7Cols} {
		lines := m.MenuLines(width)
		if len(lines) != len(singleCandidate)+completionNoteRow {
			t.Fatalf("MenuLines(%d) = %d rows, want %d (one candidate + the note)", width, len(lines), len(singleCandidate)+completionNoteRow)
		}
		for i, line := range lines {
			if got := cellsOf(line); got > width {
				t.Fatalf("MenuLines(%d) row %d = %q occupies %d cells — a menu row must never exceed the terminal width",
					width, i, line, got)
			}
		}
		// Truncation may eat the row's TAIL — never its reason: what is left on
		// screen LEADS with the reason (or is a leading fragment of it), so a
		// reader on a narrow terminal still reads why the arrows cannot move.
		head := strings.TrimLeft(lines[len(lines)-1], " ")
		if !strings.HasPrefix(head, completionMenuNote) && !strings.HasPrefix(completionMenuNote, head) {
			t.Fatalf("MenuLines(%d) note row = %q, want it to lead with the reason %q (truncation must eat the tail, not the reason)",
				width, lines[len(lines)-1], completionMenuNote)
		}
	}
}

// menuNarrowWidth is the smallest width the note's assertion runs at: narrower
// than the note's own text, so the truncation is what is being measured.
const menuNarrowWidth = 12

// cellsOf is the row's width in terminal cells (the same measure truncateCols
// applies — one definition, referenced, never a byte count).
func cellsOf(s string) int {
	n := 0
	for _, r := range s {
		n += runeCells(r)
	}
	return n
}
