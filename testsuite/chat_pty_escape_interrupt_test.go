package testsuite

// chat_pty_escape_interrupt_test.go — THE PURE INTERRUPT (ESCAPE), end to end on
// a real pty, and the two legs of `/queue` the existing harness does not read
// (task 01a0e84b).
//
// WHY THIS FILE EXISTS (and why the unit tests are not enough — Rule 31). The
// feature is a KEY reaching a RUN across four layers with four different ideas of
// what the key means: the terminal's line discipline (raw mode clears ISIG, so
// Ctrl+C is a byte and never a signal), the editor's key decoder (a LONE ESCAPE
// is only known to be lone after the bounded ESC wait expires — an ESC that
// begins an arrow sequence must NOT interrupt), the REPL's turn loop, and the
// agent's run-scoped cancel. A unit test of any one layer passes with the other
// three broken.
//
// WHAT IS ASSERTED, and why each is read from an ARTIFACT rather than from the
// implementation:
//
//	escape-stops — the answer's final sentinel never appears, on the pty or in the
//	               transcript: the stream really was abandoned.
//	escape-mark  — the marker names THE KEY THE OPERATOR PRESSED ("escape", not
//	               "ctrl-c"): the two differ in what they do to the session, so a
//	               record that cannot tell them apart explains nothing.
//	line-kept    — THE PURE INTERRUPT'S defining property, and the one Ctrl+C does
//	               not have: the draft typed into the line WHILE the turn streamed
//	               is still in the buffer, so the next Enter submits it. Ctrl+C
//	               clears the line; Escape must not.
//	still-open   — the session survives (the next prompt is answered by the same
//	               process) and the JSONL records reason=escape with a positive
//	               partial_chars, while session_end appears exactly once, from
//	               /quit.
//
// THE LINE IS TYPED DURING THE TURN, and that is not incidental: an editor
// WITHOUT the turn seam cannot be typed into mid-turn at all (the REPL is blocked
// inside the model call and its own `input.ReadLine` has not been called), so the
// draft that survives the interrupt can only come from a read loop that was
// ALREADY reading when the turn was in flight. That is the same path the pure
// interrupt needs, which is why the two are asserted together.
//
// `/queue` ITSELF IS ALREADY COVERED end to end (testsuite/chat_queue_pty_test.go,
// with a deterministic held request and the request body as ground truth); this
// file adds only the two legs that case does not read — that parking a prompt
// leaves NO interrupt marker anywhere, and that the parked prompt produces ONE
// bubble rather than the duplicate the Hermes design was known for.

import (
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// The pure-interrupt harness's own bounds. Every value is named (Rule 1).
const (
	// escapeAnswerFragments / escapeAnswerGap define the answer the harness aims
	// its key at: fragments × gap is the stream's NOMINAL length (7.2s here).
	//
	// THE KEY'S TIMING IS NOT DERIVED FROM THIS NOMINAL FIGURE, and the reason is
	// measured: the client paints the live band fragment by fragment, so the real
	// answer is much longer than the mock's own cadence implies. Measured on
	// a361a9d at this exact shape and cadence
	// (`go test ./testsuite/ -run 'TestZZEscapeWindow' -count=1 -v`):
	//
	//	ESCAPEWINDOW run=1 served=122ms sentinel=10.179s nominal=7.2s
	//	ESCAPEWINDOW run=2 served=124ms sentinel=10.173s nominal=7.2s
	//
	// — i.e. ≈10.06s of answer after the request is served, 40% more than the
	// nominal 7.2s. The window below is that MEASURED total, and the key delay is
	// derived from it, so the case cannot drift back to aiming past the end.
	escapeAnswerFragments = 120
	escapeAnswerGap       = 60 * time.Millisecond
	// escapeAnswerWindowMeasured is the measured length of one answer from the
	// mock SERVING the request to the sentinel's arrival — the total the key's
	// timing is derived from. Re-measure it with the command above if the answer's
	// rendering changes; the case does not silently adapt, it fails with the
	// sentinel on screen.
	escapeAnswerWindowMeasured = 10060 * time.Millisecond
	// escapeKeyAtNumerator/escapeKeyAtDenominator is where inside the measured
	// window the Escape is aimed: 30% of the way in, far from both the request's
	// start (~120ms) and its end. A RATIO of integers, so the arithmetic stays
	// exact and no float rounding enters a duration.
	escapeKeyAtNumerator   = 3
	escapeKeyAtDenominator = 10
	// escapeDraftTypingCost is how long typing the draft below takes: one
	// keystroke per ptyTypeGap, because the draft is TYPED (writeGapped) so the
	// editor reads keystrokes rather than a paste. It is a DERIVED figure, not a
	// new constant: the draft's length is the input and the harness's own type gap
	// is the rate.
	escapeDraftTypingCost = time.Duration(len(escapeDraftDuringTurn)) * ptyTypeGap
	// escapeKeyAt is where inside the measured window the Escape is aimed, and
	// escapeKeyDelay is what is left to wait once the draft has been typed — the
	// two inputs the case already has to emit, ordered by arithmetic instead of
	// by two independently chosen numbers.
	escapeKeyAt    = escapeAnswerWindowMeasured * escapeKeyAtNumerator / escapeKeyAtDenominator
	escapeKeyDelay = escapeKeyAt - escapeDraftTypingCost
	// escapeAnswerSentinel is the answer's LAST word: if it reaches either
	// artifact, the stream was not stopped.
	escapeAnswerSentinel = "SENTINEL-END-OF-ANSWER"
	// escapeTurnQuestion is the first turn's question.
	escapeTurnQuestion = "please answer at length"
	// escapeDraftDuringTurn is typed at the prompt WHILE the turn streams — the
	// draft the pure interrupt must leave alone. Distinctive (so the transcript
	// cannot satisfy the assertion with the turn's own question) and SHORT (so
	// typing it costs far less than the stream: see escapeAnswerFragments).
	escapeDraftDuringTurn = "draftz"
	// escapeSecondAnswer is the reply to the second turn (the line held across
	// the interrupt).
	escapeSecondAnswer = "second turn answered normally."
	// escapeQueuePrompt is the prompt parked with /queue while the FIRST turn is
	// still streaming.
	escapeQueuePrompt = "parked-while-busy"
	// escapeQueueAnswer is the reply that proves the parked prompt BECAME a turn.
	escapeQueueAnswer = "queued prompt answered."
	// ptyQueueMarkerWindow is how long the queue case watches for a marker after
	// the turn's boundary. It is a WINDOW, not a wait a verdict rests on: the
	// assertion is that no marker appeared in it, and the turn it could have come
	// from has already ended by the time the window opens (the pop note and the
	// second request's body were both observed first), so a marker written for
	// that turn would already be in the reader's buffer.
	ptyQueueMarkerWindow = 500 * time.Millisecond
	// escapeSessionSettle waits for the JSONL to carry the witness; the run writes
	// it before the carrier prints, so this is a readiness wait with a real
	// deadline rather than a sleep the assertion depends on.
	escapeSessionSettle = 5 * time.Second
	// The feature's own vocabulary, spelled as LITERALS.
	//
	// Deliberate, and what makes this a RED PROOF: the file also runs against the
	// pre-fix tree (see the report's Evidence section), where
	// agent.InterruptReasonEscape does not exist — importing it would fail the
	// BUILD there, which proves nothing about behaviour. Spelled as literals, the
	// same file compiles on both trees and fails on the old one for the right
	// reason: Escape is inert, the answer streams to its sentinel, and the JSONL
	// has no witness.
	escapeReasonLiteral  = "escape"
	escapeSourceLiteral  = "operator"
	escapeEventLiteral   = "turn_interrupted"
	escapeMarkerFragment = "[gopi] turn interrupted by "
)

// escapeLongAnswer is the streamed answer: many fragments, sentinel last.
func escapeLongAnswer() string {
	var b strings.Builder
	for i := 0; i < escapeAnswerFragments; i++ {
		if i == escapeAnswerFragments-1 {
			b.WriteString(escapeAnswerSentinel)
			continue
		}
		b.WriteString("fragment-")
		b.WriteByte(byte('a' + i%26))
		b.WriteString(" ")
	}
	return b.String()
}

// drainFor reads whatever the child has already written for the given window and
// returns it, discarding nothing on the way in — it is how a case asserts that
// something did NOT appear, which needs a bounded read rather than a wait for a
// string that must not exist. A window that expires with nothing to read is the
// PASSING case, so the duration bounds the assertion instead of deciding it.
func drainFor(p *ptySession, window time.Duration) string {
	var got strings.Builder
	timer := time.NewTimer(window)
	defer timer.Stop()
	for {
		select {
		case b, ok := <-p.rx:
			if !ok {
				return got.String()
			}
			got.WriteByte(b)
		case <-timer.C:
			return got.String()
		}
	}
}

// TestChatPTY_EscapeDuringATurnIsAPureInterruptAndKeepsTheLine is the acceptance
// test for Escape as the pure interrupt: cancel the turn, keep the line, keep the
// session.
func TestChatPTY_EscapeDuringATurnIsAPureInterruptAndKeepsTheLine(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: escapeLongAnswer(), TextDeltaBytes: 8, TextDeltaDelay: escapeAnswerGap},
		testutil.Resp{Text: escapeSecondAnswer},
	)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	// The turn goes in flight. This waits on the MOCK's request count, never on
	// the streamed text: the live band paints a fragment glyph by glyph, so a
	// contiguous fragment literal only reaches the pty when the stream ENDS (see
	// waitForTurnInFlight's measurement) — a harness waiting on it would aim its
	// Escape at a turn that is already over.
	chat.writeGapped(t, escapeTurnQuestion)
	chat.write(t, "\r")
	waitForTurnInFlight(t, llm, 1)

	// THE DRAFT, typed into the prompt while the turn runs — the line the pure
	// interrupt must preserve. It is typed BEFORE the key so the assertion cannot
	// be satisfied by text that arrived after it.
	chat.writeGapped(t, escapeDraftDuringTurn)

	time.Sleep(escapeKeyDelay) //stylegate:allow-sleep aims the interrupt at escapeKeyAt inside the MEASURED answer window, net of the draft's own typing cost — the DEFECT is about a turn still running, and escapeAnswerWindowMeasured is what bounds it

	// A LONE Escape: the byte, then nothing. The editor's bounded ESC wait is what
	// resolves it as the Escape KEY (as opposed to the prefix of an arrow), so this
	// is the same path an injected `chat_surface action=key escape` takes.
	chat.write(t, "\x1b")

	out := chat.waitFor(t, escapeMarkerFragment, ptyPromptTimeout)
	// The marker is ONE line, but it reaches the reader in several writes (the
	// fragment lands mid-line). Read the rest of the same line before asserting on
	// it, or the reason — which follows the fragment — is not in `out` yet and a
	// correct implementation looks like a wrong one.
	out += drainFor(chat, ptyQueueMarkerWindow)
	if !strings.Contains(out, escapeMarkerFragment+escapeReasonLiteral) {
		t.Errorf("the interrupt marker does not name %q — the operator is told about a key they did not press; output:\n%s",
			escapeReasonLiteral, out)
	}
	if strings.Contains(out, escapeAnswerSentinel) {
		t.Errorf("the answer's final sentinel reached the screen — the stream was NOT stopped by the Escape; output:\n%s", out)
	}
	// THE PURE INTERRUPT keeps what had already streamed: a partial answer on
	// screen, with the marker naming why it stops.
	if !strings.Contains(out, "fragment-") {
		t.Errorf("no partial answer is on screen — the interrupt must KEEP what had already been streamed; output:\n%s", out)
	}

	// THE LINE SURVIVES: submitting it now must send the draft typed during the
	// turn — nothing else was typed after the interrupt.
	chat.write(t, "\r")
	second := chat.waitFor(t, escapeSecondAnswer, ptyPromptTimeout)
	if strings.Contains(second, escapeAnswerSentinel) {
		t.Errorf("the interrupted answer's tail arrived LATE, during the next turn — the stream was never actually abandoned; output:\n%s", second)
	}
	// THE SECOND TURN WAS GIVEN THE DRAFT, read from the WIRE rather than from the
	// screen: request 2's body is the ground truth for "the buffer still held the
	// draft" (a screen reading cannot tell a preserved line from an echo).
	waitForRequestBody(t, llm, 2, escapeDraftDuringTurn)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Errorf("chat exit = %d, want 0 (/quit after a pure interrupt)", code)
	}

	time.Sleep(escapeSessionSettle) //stylegate:allow-sleep the record is durable when the marker is printed; this settles the file's final write before the test reads it
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)

	ev := sess.First(escapeEventLiteral)
	if ev == nil {
		t.Fatalf("the session JSONL has no %q event — the abandoned turn is recorded as nothing at all; types: %v",
			escapeEventLiteral, sess.Types())
	}
	if got, _ := ev.Raw["reason"].(string); got != escapeReasonLiteral {
		t.Errorf("turn_interrupted reason = %q, want %q — the record cannot say which key ended the turn", got, escapeReasonLiteral)
	}
	if got, _ := ev.Raw["source"].(string); got != escapeSourceLiteral {
		t.Errorf("turn_interrupted source = %q, want %q", got, escapeSourceLiteral)
	}
	if partial, ok := ev.Raw["partial_chars"].(float64); !ok || partial <= 0 {
		t.Errorf("turn_interrupted partial_chars = %v, want a positive count of what the operator had already read", ev.Raw["partial_chars"])
	}
	// …and the draft really is a USER TURN of the session (the record's own view
	// of the line, not the wire's).
	messages := strings.Join(sess.UserContents(), "|")
	if !strings.Contains(messages, escapeDraftDuringTurn) {
		t.Errorf("the session's user messages = %q, want the draft typed during the interrupted turn present — the line was CLEARED, which is Ctrl+C's meaning and not this key's", messages)
	}
	// The session ran to its OWN end, and never split.
	if sess.Counts()["session_end"] != 1 {
		t.Errorf("session_end count = %d, want exactly 1 (closed by /quit, not by the interrupt)", sess.Counts()["session_end"])
	}
	if code := sess.SessionEndExitCode(); code != 0 {
		t.Errorf("session_end exit_code = %d, want 0", code)
	}
}

// TestChatPTY_QueueDuringATurnLeavesNoInterruptMarkerAndOneBubble reads the two
// `/queue` legs that the queue's own harness does not: parking a prompt while a
// turn streams must leave NO interrupt anywhere (the Hermes design's "typing is
// itself the interrupt"), and the parked prompt must produce ONE user bubble
// rather than the duplicate that design was known for.
//
// The turn in flight is made DETERMINISTIC by holding the first request open
// (testutil.Resp{Hold: true}) — the same technique chat_queue_pty_test.go uses,
// and for the same reason: "the turn was not interrupted" needs a turn that is
// really still running while the typed line arrives.
func TestChatPTY_QueueDuringATurnLeavesNoInterruptMarkerAndOneBubble(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Hold: true},
		testutil.Resp{Text: escapeQueueAnswer},
	)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	chat.writeGapped(t, escapeTurnQuestion)
	chat.write(t, "\r")
	waitForHeld(t, llm)

	// THE PARK, with the turn still running.
	chat.writeGapped(t, "/queue "+escapeQueuePrompt)
	chat.write(t, "\r")

	// Leg 1 — the line never became a model turn of its own, and the turn that was
	// running was not restarted.
	if n := llm.RequestCount(); n != 1 {
		t.Fatalf("the mock served %d request(s) while the first turn was held — a `/queue` line must never become a model turn", n)
	}

	// Release: the held turn ends (a held response carries no content, so the
	// boundary below is what the parked prompt waits for).
	llm.ReleaseHold(1)

	// The park is VISIBLE at the boundary, and the prompt really became the next
	// turn — the ground truth is the second request's own body.
	chat.waitFor(t, chatQueueQueuedMark, ptyPromptTimeout)
	waitForRequestBody(t, llm, 2, escapeQueuePrompt)
	chat.waitFor(t, escapeQueueAnswer, ptyPromptTimeout)

	// NO INTERRUPT ANYWHERE. The turn ended at the queue boundary, so a marker
	// written for it would already be in the reader's channel by now — the pop
	// note and the second request's body were both observed first. The window is
	// how long this reads the buffer for.
	if out := drainFor(chat, ptyQueueMarkerWindow); strings.Contains(out, escapeMarkerFragment) {
		t.Errorf("a turn-interrupt marker reached the screen after /queue — parking a prompt must not interrupt the turn; output:\n%s", out)
	}

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Errorf("chat exit = %d, want 0 (/quit after a queued turn)", code)
	}

	time.Sleep(escapeSessionSettle) //stylegate:allow-sleep settles the session file's final write before the transcript assertions
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)

	// NO INTERRUPT WITNESS: the parked prompt never stopped the turn.
	if ev := sess.First(escapeEventLiteral); ev != nil {
		t.Errorf("the session records a %q event after /queue — parking must never interrupt; event: %v", escapeEventLiteral, ev.Raw)
	}
	// NO DUPLICATE BUBBLE: the parked prompt appears EXACTLY ONCE among the user
	// records. A queue that both parked the line AND submitted it as typed would
	// show it twice — the Hermes duplicate-bubble defect, which a screen-only
	// reading cannot distinguish from the echo.
	users := sess.UserContents()
	n := 0
	for _, u := range users {
		if strings.Contains(u, escapeQueuePrompt) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the parked prompt appears in %d user records, want exactly 1 — a duplicate bubble means the same line was both parked and submitted; user messages: %v", n, users)
	}
	// …and it is the SECOND turn: the order a queue promises.
	if len(users) < 2 || !strings.Contains(users[1], escapeQueuePrompt) {
		t.Errorf("the parked prompt is not the second user message; user messages: %v", users)
	}
	if sess.Counts()["session_end"] != 1 {
		t.Errorf("session_end count = %d, want exactly 1", sess.Counts()["session_end"])
	}
}
