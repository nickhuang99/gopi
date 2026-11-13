package testsuite

// chat_queue_pty_test.go — the END-TO-END acceptance of `/queue` (epic
// 01a0e439: a prompt queue in the interactive `gopi chat` REPL).
//
// WHAT THIS FILE PROVES, AND WHY IT MUST BE A PTY. The feature is a LOOP
// behaviour, not a data-structure behaviour: a prompt typed while a turn is in
// flight must (1) not interrupt that turn, (2) become the NEXT turn's input by
// itself — no extra Enter — and (3) be visible on screen when it is parked. The
// queue's own contract is unit-tested (cmd/agent/chat_queue_test.go); what is
// asserted here is that the REPL really consumes it, and that claim only holds on
// the input path that HAS a turn boundary. A pipe makes chat a different program
// (the cooked reader takes one line per push and there is no boundary at all —
// the case the code refuses rather than pretends), so a case driven through stdin
// could never observe the feature it is named for (PROJECT.md Rule 31: the surface
// must be really executed).
//
// THE TURN IN FLIGHT IS MADE DETERMINISTIC, NOT RACED. The mock holds the FIRST
// request open mid-stream (testutil.Resp{Hold: true}), so the turn really is
// still running while the typed lines arrive — the assertion "the turn was not
// interrupted" then has something to be about. Releasing the hold lets the turn
// END, which is the boundary at which the parked prompt must become the next
// turn with no further input.
//
// The ground truth for that is the REQUEST BODY, not the screen: request 2 must
// carry the parked prompt. A screen-only reading cannot tell "the queue ran as a
// turn" from "my text was echoed back at me".

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// The markers this case waits on. They are the REPL's own strings, spelled here
// because a `package main` constant is not importable — the same reason
// chatPromptMark is spelled in chat_pty_test.go (one reading of the same emit).
const (
	// chatQueueQueuedMark is the note a parked prompt writes: it carries the
	// prompt's position and the queue's depth.
	chatQueueQueuedMark = "[gopi] queued #1 of "
	// chatQueuePoppedMark is the note written when a parked prompt becomes a turn.
	chatQueuePoppedMark = "[gopi] → (queued) "
	// chatQueueListHeaderMark opens the list rendering (bare `/queue`), carrying
	// the depth against the bound.
	chatQueueListHeaderMark = "[gopi] queue ("
	// chatQueueClearMark is the clear's own note.
	chatQueueClearMark = "[gopi] queue cleared — "
	// chatQueueFullRefusalMark opens the over-bound refusal; the sentence (which
	// names the bound) is asserted separately.
	chatQueueFullRefusalMark = "refused: the queue is FULL ("
	// chatQueueQuitMark opens the closeout's statement about parked prompts.
	chatQueueQuitMark = "[gopi] 1 parked prompt(s) in the queue will be DROPPED"
	// chatGoodbyeSpelled is the REPL's last line — the session's own end.
	chatGoodbyeSpelled = "bye."
)

// The prompts this case types. Each is a distinctive token, so the request bodies
// can be read for WHICH prompt arrived as which turn — the distinction the
// acceptance turns on.
const (
	chatQueuePromptInFlight = "the turn in flight"
	chatQueuePromptParked   = "parked while busy"
)

// chatQueueBoundSpelled is the queue's ceiling as this case spells it when it
// fills the queue to force the refusal. It must equal cmd/agent's chatQueueBound;
// the assertion is ABOUT that number, so a drift fails here by name rather than
// silently testing a different depth at a different limit.
const chatQueueBoundSpelled = 8

// ptyHoldPollGap is the poll interval while a case waits for the MOCK's own
// recorded state. It is a polling gap, never a duration a case asserts against:
// every wait returns the moment the state it names is observable, and the
// timeout — not this value — is what a hang reports.
const ptyHoldPollGap = 25 * time.Millisecond

// TestChatQueue_ParkedPromptRunsWithoutInterruptingTheTurn is the epic's headline
// acceptance, in three parts, on a real pty and the real binary:
//
//	(1) `/queue X` typed while a turn is in flight does NOT interrupt it — the
//	    in-flight turn's own answer still arrives, and the model was asked once;
//	(2) when that turn ends, X becomes the NEXT turn's input automatically, with
//	    NO extra Enter — proven by the pop note, by the mock's SECOND request
//	    carrying X, and by the session record's user turns;
//	(3) the queue state was VISIBLE: the parked note and the pop note both appear.
func TestChatQueue_ParkedPromptRunsWithoutInterruptingTheTurn(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	// Request 1 is HELD (the turn stays in flight as long as needed); request 2 is
	// the parked prompt's own turn.
	llm := testutil.NewMockLLM(t,
		testutil.Resp{Hold: true},
		testutil.Resp{Text: "answer to the parked prompt."},
	)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	// The turn goes in flight and stays there: the hold is registered before any
	// response bytes, so this wait is the mock's own state, not a race.
	chat.writeGapped(t, chatQueuePromptInFlight)
	chat.write(t, "\r")
	waitForHeld(t, llm)

	// (1) THE PARK, WHILE THE TURN IS STILL RUNNING. The line is typed with the
	// turn in flight and must not interrupt it.
	chat.writeGapped(t, "/queue "+chatQueuePromptParked)
	chat.write(t, "\r")

	// The turn is STILL the only request — that is leg (1). A `/queue` line must
	// be answered by the REPL, never sent to the model as a turn of its own.
	if n := llm.RequestCount(); n != 1 {
		t.Fatalf("the mock served %d request(s) while the first turn was held — a `/queue` line must never become a model turn", n)
	}

	// (2) THE TURN ENDS. Release the hold: the in-flight turn completes (a held
	// response carries no content, so there is no answer text to wait for — see
	// the note above the mock) and the boundary below runs. The pop note can only
	// appear AFTER that turn ended, so waiting for it IS the "the turn was not
	// interrupted" reading: an interrupted turn never reaches a boundary.
	llm.ReleaseHold(1)

	// (3) VISIBILITY, in the order the operator reads it. The park happened while
	// the turn ran — before the console could be written to, because the console
	// has exactly ONE writer and it is the loop — so the park's note row and the
	// pop note are written together at the turn boundary. BOTH must appear, and
	// both must name the parked prompt.
	queued := chat.waitFor(t, chatQueueQueuedMark, ptyPromptTimeout)
	if !strings.Contains(queued, chatQueuePromptParked) {
		t.Fatalf("the queued note does not name the parked prompt %q:\n%s", chatQueuePromptParked, queued)
	}
	popped := chat.waitFor(t, chatQueuePoppedMark, ptyPromptTimeout)
	if !strings.Contains(popped, chatQueuePromptParked) {
		t.Fatalf("the pop note does not name the parked prompt %q: the operator cannot tell what the next turn came from:\n%s",
			chatQueuePromptParked, popped)
	}
	// The parked prompt became a TURN with no further input: the mock's second
	// request carries it. This is the ground truth — the screen alone cannot
	// distinguish "the queue ran" from "my text was echoed at me".
	waitForRequestBody(t, llm, 2, chatQueuePromptParked)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — the session ends at the deliberate /quit", code)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	// TWO user turns, and the second is the parked prompt — the parked prompt ran
	// as its own turn exactly once (a third user event would mean the raw `/queue`
	// line ALSO went to the model).
	//
	// ONE assistant event, not two, and that is the MOCK's shape rather than the
	// REPL's: a held response carries no content at all, so the first turn has
	// nothing to record. Asserting two here would be asserting a fact about the
	// fixture (that a hold produces text) instead of about the feature.
	sess.AssertCounts(t, map[string]int{
		"session_start": 1,
		"user":          2,
		"assistant":     1,
		"session_end":   1,
	})
	// The parked prompt really is the second user turn, read from the record
	// rather than from the screen.
	users := sess.UserContents()
	if len(users) != 2 || !strings.Contains(users[1], chatQueuePromptParked) {
		t.Fatalf("session user turns = %q, want the parked prompt %q as the SECOND one", users, chatQueuePromptParked)
	}
}

// TestChatQueue_OverBoundAddIsRefusedNamingTheBound is the epic's bound
// assertion end to end, through the REAL command: the queue is filled to its
// ceiling, and the next add is refused with a sentence naming the bound — never a
// silent drop, and never a prompt lost to make room.
//
// The turn is NOT held here on purpose: this case is about the FIFO's bound as
// the operator meets it, and each add's own note is the reading that makes the
// fill unambiguous.
func TestChatQueue_OverBoundAddIsRefusedNamingTheBound(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "no turn is expected in this case."})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	for i := 1; i <= chatQueueBoundSpelled; i++ {
		chat.writeGapped(t, fmt.Sprintf("/queue fill-%02d", i))
		chat.write(t, "\r")
		chat.waitFor(t, fmt.Sprintf("[gopi] queued #%d of ", i), ptyPromptTimeout)
	}
	// No turn has run: these were parked, not asked.
	if n := llm.RequestCount(); n != 0 {
		t.Fatalf("the mock served %d request(s) while prompts were only being QUEUED", n)
	}

	// The bound's own refusal — and it must NAME the bound (Rule 15: a refusal the
	// reader cannot act on is not a refusal).
	chat.writeGapped(t, "/queue one-too-many")
	chat.write(t, "\r")
	out := chat.waitFor(t, chatQueueFullRefusalMark, ptyPromptTimeout)
	if !strings.Contains(out, fmt.Sprintf("%d", chatQueueBoundSpelled)) {
		t.Fatalf("the over-bound refusal does not name the bound (%d):\n%s", chatQueueBoundSpelled, out)
	}

	// Nothing was lost to make room: the listing still reads exactly the bound's
	// worth of prompts. The header states the depth AGAINST the bound, so a save
	// made to fit the refused one in would move that number.
	chat.writeGapped(t, "/queue")
	chat.write(t, "\r")
	header := chat.waitFor(t, chatQueueListHeaderMark+fmt.Sprintf("%d of %d", chatQueueBoundSpelled, chatQueueBoundSpelled), ptyPromptTimeout)
	// The rows follow the header, so they are waited for in their OWN window: a
	// `waitFor` returns the output up to its marker and nothing after it, and
	// reading the rows out of the header's window would be reading text that had
	// not been emitted yet.
	rows := chat.waitFor(t, fmt.Sprintf("%d. fill-%02d", chatQueueBoundSpelled, chatQueueBoundSpelled), ptyPromptTimeout)
	for i := 1; i <= chatQueueBoundSpelled; i++ {
		if !strings.Contains(rows, fmt.Sprintf("%d. fill-%02d", i, i)) {
			t.Fatalf("the listing is missing row %d (`%d. fill-%02d`) — an over-bound add must not cost an accepted prompt; header then rows:\n%s\n%s",
				i, i, i, header, rows)
		}
	}
	if strings.Contains(rows, "one-too-many") {
		t.Fatalf("the REFUSED prompt appears in the queue listing — a refusal must park nothing:\n%s", rows)
	}
	if strings.Contains(rows, fmt.Sprintf("%d. ", chatQueueBoundSpelled+1)) {
		t.Fatalf("the listing has a row %d — the refused prompt was parked after all:\n%s", chatQueueBoundSpelled+1, rows)
	}

	// The clear, and its own count.
	chat.writeGapped(t, "/queue clear")
	chat.write(t, "\r")
	cleared := chat.waitFor(t, chatQueueClearMark, ptyPromptTimeout)
	if !strings.Contains(cleared, fmt.Sprintf("%d", chatQueueBoundSpelled)) {
		t.Fatalf("the clear note does not carry the real count (%d):\n%s", chatQueueBoundSpelled, cleared)
	}

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0", code)
	}
}

// TestChatQueue_QuitStatesWhatHappensToParkedPrompts is the epic's third
// assertion: `/quit` with a prompt still parked must SAY what happens to it,
// never fall silent — and the parked prompt must really still be parked when the
// session ends, or the sentence would be describing a queue that had already
// drained.
//
// THE STATE IS BUILT DETERMINISTICALLY, and the construction is the interesting
// part (an earlier draft raced and was replaced). Park the prompt at an IDLE
// prompt, wait for its own `queued #1 of 8` note, and only then quit:
//
//   - at an idle prompt no turn is in flight, so the loop's OWN `/queue` case
//     answers the line and the note is written by the loop — there is no reader
//     in the path and nothing to race;
//   - a parked prompt is taken only at a TURN boundary, and no turn runs before
//     the `/quit`, so the prompt is provably still queued when the closeout runs.
//
// The suppressed alternative is worth recording: typing `/queue X` AND `/quit`
// while a turn is in flight reaches the same sentence only if the reader
// goroutine happens to push `/quit` before the turn's boundary drains the queue.
// The boundary prefers a typed line over a parked one precisely so that this can
// happen at all, but "usually wins a race" is not a state a test may assert in,
// and the sentence is a claim about the queue's CONTENT — so it is asserted where
// the content is fixed.
func TestChatQueue_QuitStatesWhatHappensToParkedPrompts(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	// No model turn is expected at all: the prompt is parked, not asked.
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "no turn is expected in this case."})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	chat.writeGapped(t, "/queue "+chatQueuePromptParked)
	chat.write(t, "\r")
	chat.waitFor(t, chatQueueQueuedMark, ptyPromptTimeout)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")

	// The sentence must be READ before the process leaves: it is written through
	// the layout the closeout is about to take down, so the case waits for it (a
	// missing sentence fails HERE rather than passing quietly).
	out := chat.waitFor(t, chatQueueQuitMark, ptyPromptTimeout)
	if !strings.Contains(out, chatGoodbyeSpelled) {
		out += chat.waitFor(t, chatGoodbyeSpelled, ptyPromptTimeout)
	}
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — /quit with a parked prompt is still a clean exit", code)
	}

	// The parked prompt was DROPPED, not run: the model was never asked it.
	if body := llm.RequestBody(1); strings.Contains(body, chatQueuePromptParked) {
		t.Fatalf("the parked prompt was sent to the model although the session was closed with it queued:\n%s", body)
	}
	if n := llm.RequestCount(); n != 0 {
		t.Fatalf("the mock served %d request(s): a parked prompt must not start a turn of its own", n)
	}
}

// waitForHeld waits until the mock really holds the first request open — the
// state that makes "the turn is in flight" a fact rather than a race against a
// fast mock.
func waitForHeld(t *testing.T, llm *testutil.MockLLM) {
	t.Helper()
	deadline := time.Now().Add(ptyPromptTimeout)
	for time.Now().Before(deadline) {
		if llm.RequestCount() >= 1 && llm.HeldCount() >= 1 {
			return
		}
		time.Sleep(ptyHoldPollGap) //stylegate:allow-sleep polls the mock's own recorded state; the wait ends the moment it is observable
	}
	t.Fatalf("the mock never held a request open (requests=%d held=%d): this case needs a turn really in flight",
		llm.RequestCount(), llm.HeldCount())
}

// waitForRequestBody waits until the mock has served n requests and request n's
// body carries want — the boundary at which a parked prompt has become a turn.
func waitForRequestBody(t *testing.T, llm *testutil.MockLLM, n int, want string) {
	t.Helper()
	deadline := time.Now().Add(ptyPromptTimeout)
	for time.Now().Before(deadline) {
		if llm.RequestCount() >= n && strings.Contains(llm.RequestBody(n), want) {
			return
		}
		time.Sleep(ptyHoldPollGap) //stylegate:allow-sleep polls the mock's own recorded state; the wait ends the moment it is observable
	}
	t.Fatalf("request %d never carried %q (requests=%d):\n%s", n, want, llm.RequestCount(), llm.RequestBody(n))
}
