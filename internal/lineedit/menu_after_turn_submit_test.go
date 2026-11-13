package lineedit

// menu_after_turn_submit_test.go — DOES A MENU THAT IS STILL OPEN AFTER A TURN
// SWALLOW THE NEXT SUBMIT?
//
// THE CLAIM UNDER TEST. An end-to-end pty case (testsuite/
// chat_info_pty_test.go) FAILED deterministically with a screen that carried the
// `/`-prefix completion menu where the `/info` block was expected, and the first
// reading of that failure was "the menu was still Open after the model turn and
// ate the Enter". That reading is a claim about THIS package, and it is
// decidable here, deterministically, with no pty and no model: run ONE turn
// through the answer band, then type `/` `info` Enter, and ask what was
// SUBMITTED.
//
// WHY THIS IS THE RIGHT LEVEL. The pty case can only report "the screen never
// carried the block": it cannot tell a menu that swallowed the submit from a
// harness whose own completion detector never matched (that turned out to be
// the real cause — a stale `gopi log ` prefix where the product prints
// `gopi session log `). A lineedit test reads the SUBMITTED LINE, which is the
// product fact, so the two causes cannot be confused.
//
// THE PRECONDITION IS ASSERTED, NOT ASSUMED. The interesting shape is not "a
// closed menu submits"; it is "an OPEN menu whose only candidate IS the typed
// word submits". So the case pins that the menu really is open and drawn, with
// `/info` as its only offer, immediately before the Enter — the exact state the
// pty stream showed. A test that skipped that check could pass with no menu at
// all and prove nothing.
//
// The one-candidate shape is production's own: the completer offers exactly the
// word that was typed, so accepting it does not change the buffer, and Enter's
// contract (`handleKey`: "a selection that does NOT change the line is not a
// choice") is what lets the submit through.

import (
	"strings"
	"testing"
	"time"
)

const (
	// turnMenuOffer is the ONLY candidate the completer offers. It is the word
	// the test types, which is what makes "accept the candidate" a no-op on the
	// buffer — production's `/info` case.
	turnMenuOffer = "/info"
	// turnMenuTyped is the same word split into the keystrokes an operator
	// types: the slash that OPENS the menu, then the rest.
	turnMenuSlash = "/"
	turnMenuWord  = "info"
	// turnMenuAnswer is the turn's own body: the text the band carries, so the
	// turn really happened (the band is drawn, the layout moved, the editing
	// line is not row 0).
	turnMenuAnswer = "TURN-OK"
	// turnMenuAsk is the turn's prompt label.
	turnMenuAsk = "answer with the token"
)

// TestChatCompletion_SubmitAfterATurnIsTheTypedCommand is the deterministic
// answer to the pty case's question: after ONE real turn, does typing `/info`
// and pressing Enter SUBMIT `/info` — or does the still-open menu consume it?
func TestChatCompletion_SubmitAfterATurnIsTheTypedCommand(t *testing.T) {
	stub := &stubCompleter{result: []Candidate{{Text: turnMenuOffer, Hint: "this session's id"}}}
	h := newA7Harness(t, stub)

	// ---- ONE turn: the band is drawn, the layout moves, the turn ends -------
	//
	// These are the TURN GOROUTINE's own calls, made from this goroutine while
	// the read loop owns the renderer — so they QUEUE and are applied by the
	// loop, which is production's routing exactly (answerqueue.go).
	h.ed.Answer().BeginTurn(TurnInfo{Index: 1, Prompt: turnMenuAsk})
	h.ed.Answer().Delta(turnMenuAnswer)
	h.ed.Answer().EndTurn()
	h.ed.Answer().Finish()
	h.waitFor(t, "the turn's own answer to reach the band", func() bool {
		h.replay()
		for row := 0; row < a7Rows; row++ {
			if strings.Contains(h.screen.Line(row), turnMenuAnswer) {
				return true
			}
		}
		return false
	})
	h.assertNotDone(t)

	// ---- the product surface: `/` opens the menu, `info` narrows it ---------
	h.typeKeys(t, turnMenuSlash, turnMenuWord)
	h.waitFor(t, "the completion menu to be open and drawn AFTER the turn", func() bool {
		m := h.ed.MenuState()
		if !m.Open || len(m.Candidates) != 1 || m.Candidates[0].Text != turnMenuOffer {
			return false
		}
		top := h.menuTopRow()
		return top >= 0 && strings.Contains(h.screen.Line(top), turnMenuOffer)
	})

	// THE PRECONDITION, stated so the reading cannot be vacuous: the menu IS
	// open, it offers exactly the word on the line, and the editing row carries
	// that word. This is the state the pty stream showed.
	if got := strings.TrimRight(h.screenAtLine(), " "); !strings.HasSuffix(got, turnMenuOffer) {
		t.Fatalf("editing row after typing = %q, want it to end with the typed word %q\n--- screen ---\n%s",
			got, turnMenuOffer, h.rows())
	}

	// ---- THE KEY: Enter, with the menu open -------------------------------
	h.typeKeys(t, "\r")
	select {
	case res := <-h.done:
		if res.Line != turnMenuOffer {
			t.Fatalf("submitted %q, want the typed command %q — the open menu either consumed the Enter "+
				"(no submit at all) or replaced the line with a candidate\n--- screen ---\n%s",
				res.Line, turnMenuOffer, h.rows())
		}
	case <-time.After(a7Wait):
		t.Fatalf("Enter never submitted anything while a one-candidate menu offering exactly the typed word "+
			"was open: the menu swallowed the submit key\n--- menu ---\nopen=%v candidates=%v\n--- screen ---\n%s",
			h.ed.MenuState().Open, h.ed.MenuState().Candidates, h.rows())
	}
}
