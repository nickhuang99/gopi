// Copyright (c) 2026 The GoPi Authors. All rights reserved.
//
// The Ctrl+D arm of the turn-interrupt seam (task 01a1650f).
//
// WHAT WAS BROKEN, measured on a real pty before this test existed: with a turn
// in flight, Ctrl+D was read as end-of-input, QUEUED, and the reader goroutine
// then retired — so from that instant no goroutine was reading the terminal at
// all. The turn was still blocked inside ag.Chat against a peer that never
// answered, so the queued marker was never consumed either, and the Ctrl+C the
// operator pressed 30 seconds later reached nobody:
//
//	t+5.356s sent key=ctrl-d (b'\x04')
//	t+35.399s still alive — sent key2=ctrl-c
//	t+65.445s still alive after 30.0s — sending SIGQUIT for a goroutine dump
//
// The cause was in this file's subject: KeyEOF's empty-line arm returned the
// marker WITHOUT consulting the turn seam, while its sibling KeyInterrupt arm
// did. The test below pins the rule that makes the console stay alive: the key
// claims the turn it interrupts, and the marker still ends the session.

package lineedit

import (
	"testing"
)

// ctrlDByte is the raw-mode Ctrl+D the terminal delivers as input on an empty
// line: end of input, the "leave" key.
const ctrlDByte = 0x04

// ctrlDStep is one Ctrl+D keystroke, spaced like every other KEY in these tests
// (a burst would be a paste, and a pasted 0x04 is content, not a key).
func ctrlDStep() scriptStep { return scriptStep{data: []byte{ctrlDByte}, advance: a7KeyGap} }

// TestCtrlDDuringATurnIsClaimedAndStillEndsTheInput is the regression guard for
// the deaf-console defect.
//
// Both halves must hold at once, and each is load-bearing:
//   - the TURN IS CLAIMED, with OriginEOF, so the caller can cancel the turn the
//     loop is blocked in — without this the marker cannot be read until a turn
//     returns that may never return, and (measured) nobody reads the terminal
//     afterwards;
//   - the input STILL ENDS (Result.EOF, EOFKindKey), which is the key's prompt
//     meaning and must not be lost to the cancel: the operator asked to leave.
func TestCtrlDDuringATurnIsClaimedAndStillEndsTheInput(t *testing.T) {
	turnActive := true
	origins := make([]KeyOrigin, 0, 1)
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{ctrlDStep()}, 80, nil)
	ed.SetMinTurnActive(func() bool { return turnActive })
	ed.SetInterruptTurn(func(origin KeyOrigin) bool {
		origins = append(origins, origin)
		if !turnActive {
			return false
		}
		turnActive = false
		return true
	})

	res, err := ed.Run()
	if err != nil {
		t.Fatalf("editor Run: %v", err)
	}
	if len(origins) != 1 || origins[0] != OriginEOF {
		t.Fatalf("the claim reported origins %v, want exactly [%q] — Ctrl+D mid-turn must cancel the turn, otherwise the queued end-of-input marker cannot be read and the terminal is left with no reader at all", origins, OriginEOF)
	}
	if !res.EOF {
		t.Fatal("Ctrl+D during a turn did not end the input — the operator asked to leave, and the cancel must not swallow that")
	}
	if res.EOFKind != EOFKindKey {
		t.Fatalf("EOFKind = %q, want %q", res.EOFKind, EOFKindKey)
	}
}

// TestCtrlDAtThePromptDoesNotAskTheCaller: at the prompt there is no turn to
// abandon, so the key costs the caller nothing — the predicate is asked first
// (the same order Ctrl+C's arm uses), and the historic meaning is unchanged.
// This is what keeps the feature an ADDITION for every caller that has no turn
// in flight.
func TestCtrlDAtThePromptDoesNotAskTheCaller(t *testing.T) {
	claims := 0
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{ctrlDStep()}, 80, nil)
	ed.SetMinTurnActive(func() bool { return false })
	ed.SetInterruptTurn(func(KeyOrigin) bool { claims++; return true })

	res, err := ed.Run()
	if err != nil {
		t.Fatalf("editor Run: %v", err)
	}
	if claims != 0 {
		t.Fatalf("an idle Ctrl+D ran the interrupt action %d times, want 0 (the predicate is asked first)", claims)
	}
	if !res.EOF {
		t.Fatal("Ctrl+D at the prompt no longer reports Result.EOF")
	}
}

// TestCtrlDWithTextStillDeletesForward: the key's OTHER meaning is untouched. A
// non-empty buffer makes 0x04 a forward delete, and that must not claim a turn
// any more than it may end the input.
func TestCtrlDWithTextStillDeletesForward(t *testing.T) {
	claims := 0
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{
		{data: []byte("abc")},
		{data: []byte("\x1b[D"), advance: a7KeyGap},
		ctrlDStep(),
		{data: []byte("\r"), advance: a7KeyGap},
	}, 80, nil)
	ed.SetMinTurnActive(func() bool { return true })
	ed.SetInterruptTurn(func(KeyOrigin) bool { claims++; return true })

	res, err := ed.Run()
	if err != nil {
		t.Fatalf("editor Run: %v", err)
	}
	if claims != 0 {
		t.Fatalf("Ctrl+D with text in the buffer claimed the turn %d times, want 0 — with content the key is a forward delete, not an exit", claims)
	}
	if res.EOF || res.Interrupted {
		t.Fatal("Ctrl+D with text in the buffer ended the input")
	}
	if want := "ab"; res.Line != want {
		t.Fatalf("line = %q, want %q — 0x04 must still delete the character under the cursor", res.Line, want)
	}
}
