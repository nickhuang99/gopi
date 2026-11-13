package lineedit

// interrupt_turn_test.go — WHAT A TURN-ABANDONING KEY MEANS, at the layer that
// sees the key. Two keys reach the seam and they mean the same thing while a turn
// is in flight: Ctrl+C (task 01a0e4390's per-turn interrupt) and a LONE ESCAPE
// (task 01a0e84b's pure interrupt).
//
// WHY THIS FILE EXISTS (the measured defect): a raw-mode terminal clears ISIG,
// so Ctrl+C arrives as the 0x03 BYTE and is classified by the editor's key
// decoder — it is never a signal on that path. The editor's only answer was
// Result.Interrupted, which the chat REPL reads as "end the session". An answer
// that was streaming when the operator pressed Ctrl+C therefore could not be
// stopped at all: the session died and the turn was recorded as nothing.
//
// THE TWO KEYS DIFFER IN WHAT THEY DO WITH THE LINE, and that difference is a
// criterion, not a detail:
//
//   - Ctrl+C's historic meaning is "clear the line and start over", so a claimed
//     Ctrl+C DROPS the buffer (the abandoned turn's half-typed line must not
//     become the next message).
//   - Escape's historic meaning is "close the completion menu" — it never
//     touched the buffer at all, so a claimed Escape KEEPS whatever was typed.
//     That is the "pure interrupt": cancel the turn, carry no new input, and
//     leave the operator's draft exactly where it was.
//
// The properties asserted here are the seam's contract, one per arm:
//  1. DURING a turn the key is CLAIMED by the caller's interrupt action and the
//     editor KEEPS READING — the session survives.
//  2. AT the prompt (no turn) each key keeps its historic meaning exactly:
//     Result.Interrupted for Ctrl+C, inert MenuClose for Escape.
//  3. A claim that comes back FALSE (the turn ended in the same instant) is NOT
//     swallowed: the key falls back to its historic meaning (Rule 15).
//  4. The origin travels with the claim, so the caller can name the key.

import (
	"testing"
)

// ctrlCByte is the raw-mode Ctrl+C the terminal delivers as input (ISIG off, so
// no signal is generated — see the file comment).
const ctrlCByte = 0x03

// loneEscapeSteps is how a LONE Escape is scripted: the 0x1b byte on one read,
// then a BOUNDED WAIT that expires — which is exactly how a keyboard produces
// the key. The decoder cannot know an ESC is lone until it has waited for the
// rest of a sequence and got nothing, so the wait is part of the key rather than
// a property of the harness (measured: scripting the byte alone feeds it into
// the decoder's pending buffer and the read loop blocks forever waiting for a
// continuation that never comes).
//
// The gap before the byte is a7KeyGap — the same key gap the menu tests use —
// because a burst of bytes arriving back-to-back is a PASTE, and a pasted ESC is
// content rather than a key.
func loneEscapeSteps() []scriptStep {
	return []scriptStep{
		{data: []byte{escByte}, advance: a7KeyGap},
		{timeout: true}, // the expiry IS the key: the decoder resolves a lone ESC when its bounded wait ends with nothing after it
	}
}

// escScript builds a script whose one LONE ESCAPE sits between the text typed
// before it and the steps after it — the shape all four Escape cases share.
func escScript(before []byte, after ...scriptStep) []scriptStep {
	steps := append([]scriptStep{{data: before}}, loneEscapeSteps()...)
	return append(steps, after...)
}

// escapeStep is the trailing CR every Escape case ends with: the submit that
// shows what the buffer holds when the interrupt is over.
func escapeStep() scriptStep { return scriptStep{data: []byte("\r"), advance: a7KeyGap} }

// TestCtrlCDuringATurnIsClaimedAndTheSessionSurvives: one Ctrl+C while a turn is
// in flight does not end Run, does not return a line, and clears the buffer.
func TestCtrlCDuringATurnIsClaimedAndTheSessionSurvives(t *testing.T) {
	turnActive := true
	claims := 0
	// The steps are separated by a7KeyGap — the same key gap the menu tests use
	// — because the editor classifies a burst of bytes arriving back-to-back as a
	// PASTE, and a pasted CR is content rather than a submit (Value.Enter). Real
	// keystrokes are separated by a gap; a test that does not separate them is
	// asserting about a paste.
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{
		{data: []byte("half a question")},
		{data: []byte{ctrlCByte}, advance: a7KeyGap},
		{data: []byte("the next prompt"), advance: a7KeyGap},
		{data: []byte("\r"), advance: a7KeyGap},
	}, 80, nil)
	ed.SetMinTurnActive(func() bool { return turnActive })
	ed.SetInterruptTurn(func(origin KeyOrigin) bool {
		if !turnActive {
			return false
		}
		turnActive = false
		claims++
		return true
	})

	res, err := ed.Run()
	if err != nil {
		t.Fatalf("editor Run: %v", err)
	}
	if res.Interrupted {
		t.Fatal("a Ctrl+C DURING a turn reported Result.Interrupted — the REPL reads that as 'end the session', which is the defect this feature removes")
	}
	if res.EOF {
		t.Fatal("a Ctrl+C during a turn ended the input")
	}
	if claims != 1 {
		t.Fatalf("the caller's interrupt action ran %d times, want exactly 1 — the key must be claimed once per press", claims)
	}
	// The buffer is DROPPED: the abandoned turn's half-typed line must not become
	// the next message. This is the assertion that catches a "just keep reading"
	// implementation, which would silently submit "half a questionthe next prompt".
	if res.Line != "the next prompt" {
		t.Fatalf("line after the interrupt = %q, want %q — the abandoned input leaked into the next message", res.Line, "the next prompt")
	}
}

// TestCtrlCAtThePromptStillEndsTheSession: the semantics this feature must NOT
// change. With no turn in flight the key is not claimed, and Run reports exactly
// what it always did.
func TestCtrlCAtThePromptStillEndsTheSession(t *testing.T) {
	claims := 0
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{
		{data: []byte{ctrlCByte}},
	}, 80, nil)
	ed.SetMinTurnActive(func() bool { return false })
	ed.SetInterruptTurn(func(KeyOrigin) bool { claims++; return true })

	res, err := ed.Run()
	if err != nil {
		t.Fatalf("editor Run: %v", err)
	}
	if !res.Interrupted {
		t.Fatal("Ctrl+C at the prompt no longer reports Result.Interrupted — the at-the-prompt exit changed")
	}
	if claims != 0 {
		t.Fatalf("the interrupt action ran %d times with no turn in flight, want 0", claims)
	}
}

// TestAnUnclaimedCtrlCFallsBackToThePromptMeaning: the action says it did NOT
// claim the key (the turn ended in the same instant the key arrived), so the key
// must keep its prompt meaning rather than vanish.
func TestAnUnclaimedCtrlCFallsBackToThePromptMeaning(t *testing.T) {
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{
		{data: []byte{ctrlCByte}},
	}, 80, nil)
	ed.SetMinTurnActive(func() bool { return true })
	ed.SetInterruptTurn(func(KeyOrigin) bool { return false }) // nothing to abandon after all

	res, err := ed.Run()
	if err != nil {
		t.Fatalf("editor Run: %v", err)
	}
	if !res.Interrupted {
		t.Fatal("a Ctrl+C the caller refused to claim was SWALLOWED — the operator's key did nothing at all (Rule 15)")
	}
}

// TestWithoutAHandlerNothingChanges: every caller that installs no seam keeps the
// historic behaviour byte-for-byte, which is what makes this an addition rather
// than a change to the editor's contract.
func TestWithoutAHandlerNothingChanges(t *testing.T) {
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{
		{data: []byte{ctrlCByte}},
	}, 80, nil)
	res, err := ed.Run()
	if err != nil {
		t.Fatalf("editor Run: %v", err)
	}
	if !res.Interrupted {
		t.Fatal("Ctrl+C without an installed seam no longer reports Result.Interrupted")
	}
}

// ---------------------------------------------------------------------------
// ESCAPE — the pure interrupt (task 01a0e84b)
// ---------------------------------------------------------------------------

// TestEscapeDuringATurnIsClaimedAndKeepsTheLine is the pure-interrupt criterion,
// and its two halves are what make it "pure": the turn is claimed (the caller
// cancels it), and the LINE SURVIVES — the operator's draft is still in the
// buffer when the next Enter submits, exactly as if Escape had never been
// pressed. It is the property Ctrl+C's arm deliberately does NOT have.
func TestEscapeDuringATurnIsClaimedAndKeepsTheLine(t *testing.T) {
	turnActive := true
	origins := make([]KeyOrigin, 0, 1)
	ed, _, _, _ := newScriptedEditor(t, escScript([]byte("a draft I was typing"),
		scriptStep{data: []byte(" and finished"), advance: a7KeyGap},
		escapeStep()), 80, nil)
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
	if res.Interrupted {
		t.Fatal("Escape reported Result.Interrupted — Escape must NEVER end the session (that is Ctrl+C's and Ctrl+D's meaning); it is the pure interrupt")
	}
	if res.EOF {
		t.Fatal("Escape ended the input")
	}
	if len(origins) != 1 || origins[0] != OriginEscape {
		t.Fatalf("the claim reported origins %v, want exactly [%q] — the caller cannot name the key otherwise", origins, OriginEscape)
	}
	// THE WHOLE LINE, both halves: the draft typed before the interrupt and the
	// text typed after it. A buffer-resetting implementation would submit only
	// " and finished"; one that dropped the keystroke entirely would lose nothing
	// here but would fail the claim assertion above.
	if want := "a draft I was typing and finished"; res.Line != want {
		t.Fatalf("line after the Escape interrupt = %q, want %q — the pure interrupt must keep the operator's draft", res.Line, want)
	}
}

// TestEscapeAtThePromptIsInertAndKeepsTheLine: the idle case, and the idempotence
// the epic asks for. Escape with no turn in flight does nothing at all — it does
// not exit, does not report an error, does not touch the buffer, and does not
// even ASK the caller to claim it (the predicate is consulted first, so an idle
// Escape costs the caller nothing).
func TestEscapeAtThePromptIsInertAndKeepsTheLine(t *testing.T) {
	claims := 0
	ed, _, _, _ := newScriptedEditor(t, escScript([]byte("still here"), escapeStep()), 80, nil)
	ed.SetMinTurnActive(func() bool { return false })
	ed.SetInterruptTurn(func(KeyOrigin) bool { claims++; return true })

	res, err := ed.Run()
	if err != nil {
		t.Fatalf("editor Run: %v", err)
	}
	if res.Interrupted || res.EOF {
		t.Fatal("an idle Escape ended the input — Escape is not an exit key, and pressing it at the prompt must be a no-op")
	}
	if claims != 0 {
		t.Fatalf("an idle Escape ran the interrupt action %d times, want 0 (the predicate is asked first)", claims)
	}
	if res.Line != "still here" {
		t.Fatalf("line after the idle Escape = %q, want %q — the idle key must not edit the buffer", res.Line, "still here")
	}
}

// TestAnUnclaimedEscapeStillKeepsTheLine: the race the seam already handles for
// Ctrl+C, asserted for Escape because Escape's fallback is visible in a different
// place — the turn ended in the same instant, so the key is NOT claimed, and it
// falls through to the menu's close key while leaving the line alone (Rule 15:
// nothing is swallowed, and nothing is invented either).
func TestAnUnclaimedEscapeStillKeepsTheLine(t *testing.T) {
	ed, _, _, _ := newScriptedEditor(t, escScript([]byte("draft"), escapeStep()), 80, nil)
	ed.SetMinTurnActive(func() bool { return true })
	ed.SetInterruptTurn(func(KeyOrigin) bool { return false }) // nothing to abandon after all

	res, err := ed.Run()
	if err != nil {
		t.Fatalf("editor Run: %v", err)
	}
	if res.Interrupted || res.EOF {
		t.Fatal("an unclaimed Escape ended the input")
	}
	if res.Line != "draft" {
		t.Fatalf("line after an unclaimed Escape = %q, want %q", res.Line, "draft")
	}
}

// TestEscapeWithoutAHandlerIsStillTheMenuKey: the historic contract, byte for
// byte. A caller that installs no seam gets the pre-feature editor: Escape closes
// the completion menu and is otherwise inert. This is the case that proves the
// feature is an ADDITION — the 0x1b byte had a meaning before it and still has it.
func TestEscapeWithoutAHandlerIsStillTheMenuKey(t *testing.T) {
	steps := append([]scriptStep{{data: []byte("hel")}, {data: []byte("\t"), advance: a7KeyGap}}, loneEscapeSteps()...)
	steps = append(steps, scriptStep{data: []byte("\r"), advance: a7KeyGap})
	ed, _, _, _ := newScriptedEditor(t, steps, 80, nil)

	res, err := ed.Run()
	if err != nil {
		t.Fatalf("editor Run: %v", err)
	}
	if res.Interrupted || res.EOF {
		t.Fatal("Escape without an installed seam ended the input")
	}
	if res.Line != "hel" {
		t.Fatalf("line = %q, want %q — the line is what a lone Escape must leave alone", res.Line, "hel")
	}
}
