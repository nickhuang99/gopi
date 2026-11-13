package testsuite

// chat_pty_interrupt_test.go — THE PER-TURN CTRL+C, end to end on a real pty.
//
// WHY THIS FILE EXISTS (and why the unit tests are not enough — Rule 31). The
// feature is a KEY reaching a RUN, across four layers that each have their own
// idea of what Ctrl+C means: the terminal's line discipline (raw mode clears
// ISIG, so the key is a byte and never a signal), the editor's key decoder, the
// REPL's turn loop, and the agent's run-scoped cancel. Only a real pty exercises
// that path: ISIG really is off, the editor really is parked in its read loop,
// and the run really is streaming when the byte arrives. A unit test of any one
// layer passes with the other three still broken.
//
// The assertions are the four things the operator was promised, and each is
// read from an artifact rather than from the implementation:
//  1. THE STREAM STOPS: the answer's own final sentinel never appears — not on
//     the pty, and not in the session transcript. The mock streams the answer in
//     many delayed fragments, so "it stopped" is a measurement of the fragment
//     the stream never reached, not a guess about timing.
//  2. THE PARTIAL ANSWER SURVIVES: what had arrived is still on the screen, and
//     the JSONL's turn_interrupted witness carries its size (partial_chars > 0).
//  3. THE TURN IS RECORDED AS INTERRUPTED, in its own event, naming ctrl-c and
//     the operator — and the session is still open (no session_end yet).
//  4. THE SESSION SURVIVES: the next prompt is answered normally by the same
//     session, and /quit then closes it cleanly.

import (
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// The interrupt harness's own bounds. Every value is named (Rule 1).
const (
	// interruptAnswerFragments is how many fragments the long answer is split
	// into. Enough that a cancel landing a fraction of the way in is
	// unambiguously EARLY rather than "the mock happened to be finished".
	interruptAnswerFragments = 24
	// interruptAnswerGap is the mock's delay between two fragments.
	interruptAnswerGap = 45 * time.Millisecond
	// interruptAnswerWindowMeasured is THE MEASURED length of one answer, from
	// the mock SERVING the request to the answer's final sentinel reaching the
	// pty — the total the key delay below is derived from. It is larger than the
	// stream's nominal length (interruptAnswerFragments × interruptAnswerGap =
	// 1.08s) because the client adds its own per-fragment paint cost, and the
	// harness owns that difference as a measured figure rather than a guess.
	//
	// Measured on a361a9d, three runs, same shape and cadence as this case
	// (`go test ./testsuite/ -run 'TestZZTurnWindow' -count=1 -v`):
	//
	//	WINDOW run=1 served=136ms firstText=139ms sentinel=1.688s nominal=1.08s
	//	WINDOW run=2 served=128ms firstText=130ms sentinel=1.685s nominal=1.08s
	//	WINDOW run=3 served=128ms firstText=128ms sentinel=1.681s nominal=1.08s
	//
	// Re-measure it with that command if the answer's rendering changes; the
	// case does not silently adapt, it fails with the sentinel on screen.
	interruptAnswerWindowMeasured = 1685 * time.Millisecond
	// interruptKeyAtFraction is where inside the measured window the interrupt is
	// aimed: a quarter of the way in, which is far from both ends (the request
	// needs ~130ms to be served and the tail is the sentinel this case asserts
	// the absence of).
	interruptKeyQuarter = 4
	// interruptKeyDelay is therefore DERIVED, never chosen: a quarter of the
	// measured answer (≈420ms from the moment the request is served). It is
	// measured from the SERVED instant, not from the submit keystroke, because
	// the served instant is the one point in this flow that is exact.
	interruptKeyDelay = interruptAnswerWindowMeasured / interruptKeyQuarter
	// interruptAnswerSentinel is the answer's LAST word, and the whole point of
	// the long answer: if it ever reaches either artifact, the stream was not
	// stopped. Its own text carries it, so the assertion cannot be satisfied by a
	// renderer that dropped the tail for some other reason.
	interruptAnswerSentinel = "SENTINEL-END-OF-ANSWER"
	// interruptSecondAnswer is the next turn's answer: short, and text no other
	// turn can produce.
	interruptSecondAnswer = "second turn answered normally."
	// interruptSessionSettle is how long to wait for the JSONL to carry the
	// interrupt witness after the marker is on screen. The run writes the record
	// before the carrier prints, so this is a readiness wait with a real
	// deadline, not a sleep the assertion depends on.
	interruptSessionSettle = 5 * time.Second
	// chatBannerExitFragment is the banner's exit half, checked by the harness so
	// the feature's own sentence cannot replace what was already there.
	chatBannerExitFragment = "Ctrl+D or /quit to exit."
	// chatBannerCtrlCFragment is the banner's interrupt half (spelled here as the
	// harness would see it, because the sentence lives in package main).
	chatBannerCtrlCFragment = "Ctrl+C interrupts the running turn"
	// The next three are the feature's OWN vocabulary, spelled as LITERALS.
	//
	// That is deliberate, and it is what makes this harness a RED PROOF: the test
	// is run against the pre-fix tree as well (see the report's Evidence section),
	// where agent.InterruptReasonCtrlC / sessionevent.EventTypeTurnInterrupted do
	// not exist yet — importing them would fail the BUILD there, which proves
	// nothing about behaviour. Spelled as literals, the same file compiles on both
	// trees and FAILS on the old one for the right reason: the marker never
	// appears, the answer streams to its sentinel, and the JSONL has no witness.
	// The values are pinned where they are defined (internal/agent's constants)
	// and asserted against these literals by cmd/agent's interrupt tests, so the
	// spelling cannot drift silently.
	interruptReasonLiteral = "ctrl-c"
	interruptSourceLiteral = "operator"
	interruptEventLiteral  = "turn_interrupted"
)

// interruptLongAnswer is the streamed answer: many fragments, with the sentinel
// on the very last one.
func interruptLongAnswer() string {
	var b strings.Builder
	for i := 0; i < interruptAnswerFragments; i++ {
		if i == interruptAnswerFragments-1 {
			b.WriteString(interruptAnswerSentinel)
			continue
		}
		b.WriteString("fragment-")
		b.WriteByte(byte('a' + i%26))
		b.WriteString(" ")
	}
	return b.String()
}

// TestChatPTY_CtrlCDuringATurnInterruptsTheTurnNotTheSession is the acceptance
// test for the per-turn interrupt (task 01a0e4390).
func TestChatPTY_CtrlCDuringATurnInterruptsTheTurnNotTheSession(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	answer := interruptLongAnswer()
	llm := testutil.NewMockLLM(t,
		testutil.Resp{
			Text:           answer,
			TextDeltaBytes: 8,
			TextDeltaDelay: interruptAnswerGap,
		},
		testutil.Resp{Text: interruptSecondAnswer},
	)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	// The banner is read FIRST and in full: it is printed BEFORE the prompt, so a
	// wait on the prompt would consume it (the harness reads until its marker and
	// the surplus is discarded). The banner states what the key now does — the
	// operator learns the feature exists at the one moment they are looking at
	// the screen — and its exit half must still be there.
	banner := chat.waitFor(t, chatBannerExitFragment, ptyPromptTimeout)
	if !strings.Contains(banner, chatBannerCtrlCFragment) {
		t.Errorf("the start-up banner does not say what Ctrl+C does; output:\n%s", banner)
	}
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	chat.writeGapped(t, "please answer at length")
	chat.write(t, "\r")
	// Wait for the turn to be really in flight (the mock has the request), then
	// interrupt it inside the measured answer window. It cannot wait on streamed
	// TEXT: the live band paints a fragment glyph by glyph, so a contiguous
	// fragment literal only reaches the pty when the stream ENDS (see
	// waitForTurnInFlight's measurement).
	waitForTurnInFlight(t, llm, 1)
	time.Sleep(interruptKeyDelay) //stylegate:allow-sleep aims the interrupt into the middle of the measured answer window — the DEFECT is about a turn that is still running, so the wait has to outlast the request, and interruptAnswerWindowMeasured is what bounds it
	chat.write(t, "\x03")

	// 1 + 2 + 3: the marker names the interruption and its partial size.
	out := chat.waitFor(t, interruptReasonLiteral, ptyPromptTimeout)
	if !strings.Contains(out, "turn interrupted by") {
		t.Errorf("the interrupt marker did not reach the screen; output so far:\n%s", out)
	}
	if strings.Contains(out, interruptAnswerSentinel) {
		t.Errorf("the answer's final sentinel reached the screen — the stream was NOT stopped by the interrupt; output:\n%s", out)
	}

	// 4: the session is ALIVE — the next prompt is answered by the same process,
	// in the same session. A Ctrl+C that had ended the session would fail on this
	// wait (the child would be gone and the pty would close).
	chat.writeGapped(t, "second question")
	chat.write(t, "\r")
	second := chat.waitFor(t, interruptSecondAnswer, ptyPromptTimeout)
	if strings.Contains(second, interruptAnswerSentinel) {
		t.Errorf("the interrupted answer's tail arrived LATE, during the next turn — the stream was never actually abandoned; output:\n%s", second)
	}

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	code := chat.close(t)
	if code != 0 {
		t.Errorf("chat exit = %d, want 0 (/quit after an interrupt)", code)
	}

	// THE ARTIFACT: the transcript is where a reader finds out why the answer
	// stopped, so every claim above has to be true of the file too.
	time.Sleep(interruptSessionSettle) //stylegate:allow-sleep the record is durable when the marker is printed; this settles the file's final write before the test reads it
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)

	ev := sess.First(interruptEventLiteral)
	if ev == nil {
		t.Fatalf("the session JSONL has no %q event — the interrupted turn is recorded as nothing at all; types: %v",
			interruptEventLiteral, sess.Types())
	}
	if got, _ := ev.Raw["reason"].(string); got != interruptReasonLiteral {
		t.Errorf("turn_interrupted reason = %q, want %q", got, interruptReasonLiteral)
	}
	if got, _ := ev.Raw["source"].(string); got != interruptSourceLiteral {
		t.Errorf("turn_interrupted source = %q, want %q", got, interruptSourceLiteral)
	}
	partial, ok := ev.Raw["partial_chars"].(float64)
	if !ok || partial <= 0 {
		t.Errorf("turn_interrupted partial_chars = %v, want a positive count of what the operator had already read", ev.Raw["partial_chars"])
	}

	// The partial answer stays ON SCREEN and OFF the transcript, and the tail
	// never lands anywhere: that pairing is what makes "interrupted" a fact
	// rather than a rendering.
	for _, typ := range []string{"assistant"} {
		for _, a := range sess.Events {
			if a.Type != typ {
				continue
			}
			if strings.Contains(strings.Join(mapValues(a.Raw), " "), interruptAnswerSentinel) {
				t.Errorf("the transcript holds the answer's sentinel — the abandoned stream was recorded as a completed answer")
			}
		}
	}
	// The session ran to its OWN end: the interrupt did not close it, /quit did.
	if sess.Counts()["session_end"] != 1 {
		t.Errorf("session_end count = %d, want exactly 1 (the session was closed by /quit, not by the interrupt)", sess.Counts()["session_end"])
	}
	if code := sess.SessionEndExitCode(); code != 0 {
		t.Errorf("session_end exit_code = %d, want 0", code)
	}
	// Both turns are in ONE session: the interrupt did not split or restart it.
	// The count is the OPERATOR's turns, not the raw user rows: the harness
	// writes its own user row per request (the turn stamp), so the raw census
	// is 2x the turns (see internal/testutil's UserContents).
	if got := sess.UserContents(); len(got) != 2 {
		t.Errorf("user turns = %d, want 2 (the interrupted turn's question and the next one) — types: %v, raw user rows: %d",
			len(got), sess.Types(), sess.Counts()["user"])
	}
	messages := strings.Join(sess.UserContents(), "|")
	if !strings.Contains(messages, "second question") {
		t.Errorf("the session's user messages = %q, want the second question present — the session was not continued", messages)
	}
}

// mapValues flattens an event's decoded fields so a search over an assistant
// record's content does not depend on which nesting the logger used.
func mapValues(raw map[string]interface{}) []string {
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		switch tv := v.(type) {
		case string:
			out = append(out, tv)
		case []interface{}:
			for _, e := range tv {
				if m, ok := e.(map[string]interface{}); ok {
					out = append(out, mapValues(m)...)
					continue
				}
				if s, ok := e.(string); ok {
					out = append(out, s)
				}
			}
		case map[string]interface{}:
			out = append(out, mapValues(tv)...)
		}
	}
	return out
}
