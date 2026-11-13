package testsuite

// chat_notice_turn_pty_test.go — the END-TO-END acceptance of epic 01a0eeea
// (flagship 5): a background job that finishes while the operator is IDLE opens
// a NEW TURN on its own, with no keystroke.
//
// WHY IT MUST BE A PTY. The feature is a LOOP behaviour: the wake has to reach
// the REPL's idle select, the level has to be consulted, and a turn has to be
// minted with no operator message. On a pipe chat is a different program — the
// cooked reader feeds one line per push, so nothing is ever "idle" and the arm
// under test is unreachable (the same reason the /queue acceptance states). The
// case therefore drives the REAL binary on a real pseudo-terminal and types ONE
// line, ever.
//
// WHAT IS ASSERTED, and in which order:
//  1. the console ANNOUNCES the self-opened turn on the screen, with no input
//     from the operator after the first line (the wait IS the proof: the marker
//     cannot appear unless the REPL left its idle select by itself);
//  2. the MODEL was asked a further question whose message is the agent's own
//     notice formatter — read from the request body, never from the screen,
//     because an echo of the job's output is not a turn;
//  3. the session record carries the notice turn as a NEW user message after an
//     assistant message, and the operator's own line appears exactly ONCE — the
//     roles strictly alternate and no past message was rewritten (the epic's
//     second hard invariant).

import (
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// chatNoticeJobCommand is the job the FIRST turn starts. `printf` rather than
	// `echo` so the command line carries no shell punctuation a reader could
	// mistake for the fixture's own quoting.
	chatNoticeJobCommand = "printf chat-notice-job-done"
	// chatNoticeOperatorLine is the ONE line this case types. Nothing else is
	// ever written to the child's terminal.
	chatNoticeOperatorLine = "start the job please"
	// chatNoticeAnnounceMark is the REPL's own announcement of a self-opened
	// turn (chatJobNoticeTurnNotice). It is the screen-side reading.
	chatNoticeAnnounceMark = "opening a turn to report it"
	// chatNoticeMintedMark is the phrase only the AGENT composes
	// (agent.ChatJobNoticeOpenLine). Nothing the operator typed can produce it,
	// which is what makes it evidence that the CONSOLE opened the turn.
	chatNoticeMintedMark = "Background job "
	// chatNoticeOutputMark is the job's own command, read back out of the minted
	// turn's message.
	chatNoticeOutputMark = "chat-notice-job-done"
	// chatNoticeSlowJobCommand is the parked-console case's job: it WAITS before
	// its first byte, so the turn that started it ends — and the console parks —
	// while the job is still running. Without that gap the case would be the one
	// above (the level, not the edge, would open the turn). A timer, not a load
	// generator (Rule 27): one process, one sleep, one printf.
	chatNoticeSlowJobCommand = "sleep 2; printf chat-notice-job-done"
	// chatNoticeSlowJobStarted is the answer to the operator's turn: seeing it
	// means the console has finished that turn and is about to park.
	chatNoticeSlowJobStarted = "Started the slow job"
	// chatNoticeEdgeTimeout bounds the wait for the park to be broken. It is
	// generous relative to the job's own 2s, because it must never be the thing
	// that fails: the failure of interest is "no turn opened at all", and that is
	// named by the wait timing out on a console that could have been waiting
	// forever.
	chatNoticeEdgeTimeout = 45 * time.Second
)

// noticeTurnOpenerRows narrows the HARNESS view down to the rows that OPENED a
// minted notice TURN — the statement the console composed
// (agent.ChatJobNoticeOpenLine).
//
// WHY THE HARNESS VIEW NEEDS NARROWING AT ALL. It is the exact complement of
// Session.UserContents (sessionevent.IsHarnessInjected decides both), and the
// harness speaks more than once about ONE completion: the per-request turn
// stamp (internal/agent/clock.go's turnStampLine, written on EVERY request) is a
// harness row, and so is the round boundary's own injection. The turn stamp
// shares nothing with a notice and is dropped by the mark; the injection is
// dropped by the relation below.
//
// HOW THE OPENER IS TOLD FROM THE BOUNDARY'S RESTATEMENT — by the relation
// production builds, not by a copied string: jobNoticeInjectionMessage is
// composed FROM jobNoticeFact ("<fact> Call job(action=output, job_id=…) …",
// internal/agent/chat_job_notice_turn.go), so the boundary's row CONTAINS the
// opener's row verbatim. The opener is therefore the marked row that another
// marked row contains; the restatement is the container. A row nothing contains
// and that contains nothing is the markerless case's sole marked row — kept,
// because a lone marked notice row can only be an opener.
//
// WHAT RED STILL LOOKS LIKE: a completion delivered as TWO turns leaves two
// opener statements (equal texts, or two different jobs), and neither is a
// container's source — the count comes back 2 and the assertion below fails,
// which is the duplicate the case exists to catch.
func noticeTurnOpenerRows(rows []string) []string {
	var marked []string
	for _, r := range rows {
		if strings.Contains(r, chatNoticeMintedMark) {
			marked = append(marked, r)
		}
	}
	var out []string
	for _, r := range marked {
		restated := false
		for _, other := range marked {
			if other != r && strings.Contains(strings.TrimSpace(other), strings.TrimSpace(r)) {
				restated = true
				break
			}
		}
		if restated || len(marked) == 1 {
			out = append(out, r)
		}
	}
	return out
}

// waitForNoticeRequest waits (bounded) for a model request carrying the agent's
// notice message, and returns every such request. It is a synchronization wait —
// it returns the instant the request arrives — because the console announces the
// turn BEFORE the request is built, so a sample taken at the announcement is a
// sample of a race.
func waitForNoticeRequest(t *testing.T, llm *testutil.MockLLM) []string {
	t.Helper()
	deadline := time.Now().Add(ptyPromptTimeout)
	for {
		if got := llm.RequestsContaining(chatNoticeMintedMark); len(got) > 0 {
			return got
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(ptyHoldPollGap) //stylegate:allow-sleep bounded poll of a real pty's request log; a timeout returns nil rather than failing, so there is no event to wait on
	}
}

// TestChatNoticeTurn_JobCompletionOpensATurnWithNoInput is the epic's headline
// acceptance on a real pty and the real binary.
func TestChatNoticeTurn_JobCompletionOpensATurnWithNoInput(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		// Request 1: the operator's own turn. It issues the job tool call.
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_job",
			Name: "job",
			Args: map[string]interface{}{"action": "start", "command": chatNoticeJobCommand},
		}},
		// Request 2: the answer to the operator's turn. Nothing is typed after it.
		testutil.Resp{Text: "I started the job; its completion will be reported when it lands."},
		// Request 3 onward: the minted turn's own answer. Repeating this entry is
		// deliberate — the mock repeats its last Resp, so a run that minted more
		// than one turn is served rather than hanging, and the assertions below
		// are what decide how many were legitimate.
		testutil.Resp{Text: "The job finished — reported above."},
	)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat", "--tools"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	// THE ONE AND ONLY THING THE OPERATOR TYPES.
	chat.writeGapped(t, chatNoticeOperatorLine)
	chat.write(t, "\r")

	// (1) THE SCREEN READING, and it is a WAIT rather than a race: the console
	// must say, on its own, that it is opening a turn. No input follows the line
	// above — not a key, not an Enter.
	announced := chat.waitFor(t, chatNoticeAnnounceMark, ptyPromptTimeout)
	if !strings.Contains(announced, "job-") {
		t.Fatalf("the console announced a self-opened turn without naming the job that caused it:\n%s", announced)
	}

	// (2) THE WIRE READING: a request exists whose message is the agent's notice
	// formatter. This is what separates "the console opened a turn" from "the
	// terminal echoed the job's output". It is a bounded WAIT rather than a
	// sample: the announcement above is written BEFORE the turn's request is
	// built, so reading the wire at that instant would be reading a race.
	minted := waitForNoticeRequest(t, llm)
	if len(minted) == 0 {
		t.Fatalf("the console announced a turn but no model request carries the agent's notice message (%q): the model was asked nothing about the job (%d request(s) served)",
			chatNoticeMintedMark, llm.RequestCount())
	}
	if !strings.Contains(minted[0], chatNoticeOutputMark) {
		t.Fatalf("the minted turn names no job command — the notice reached the model without saying what finished:\n%s", minted[0])
	}
	if !strings.Contains(minted[0], chatNoticeOperatorLine) {
		t.Fatalf("the minted turn's request does not carry the operator's conversation — the notice must ride the ONE history, not a fresh one:\n%s", minted[0])
	}

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — the session ends at the deliberate /quit", code)
	}

	// (3) THE RECORD: role alternation and an untouched prefix, read from the
	// session rather than from the screen.
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	// TWO ROWS, TWO VIEWS, and the split is the point: the record's "user" rows
	// carry two different things — what the operator typed, and what the harness
	// appended for the model. Session.UserContents() is the OPERATOR-ONLY view
	// (every user row that is not a harness injection, sessionevent.
	// IsHarnessInjected being the predicate), and the minted notice turn is BY
	// CONSTRUCTION on the other side of that filter: the opener is built by
	// agent.ChatJobNoticeOpenLine, whose text opens with the harness prefix —
	// that is what makes the model read it as "the harness is telling you this"
	// rather than as something the operator said. Reading the minted row from the
	// operator view is therefore reading a view it can never contain, and the
	// count collapses to the operator's one line — which is exactly how this test
	// was red. The two views are exact complements (the same predicate decides
	// both), so reading each half from its own view is the precise check; the
	// operator's own row is still asserted byte-for-byte.
	op := sess.UserContents()
	if len(op) != 1 {
		t.Fatalf("session recorded %d operator turn(s) %q, want exactly 1 — a second operator row would mean the wake put words in the operator's mouth, and zero would mean their own line was rewritten away",
			len(op), op)
	}
	if op[0] != chatNoticeOperatorLine {
		t.Fatalf("operator turn 1 = %q, want %q byte-for-byte — the wake must never rewrite a past message",
			op[0], chatNoticeOperatorLine)
	}
	// The minted turn, read from its own view. One TURN, not one harness row:
	// the harness half also carries the turn-stamp clock lines (internal/agent's
	// clock.go stamps EVERY request) and the round boundary's own restatement of
	// this same notice, the mark plus the containment relation is what tells them
	// apart (see noticeTurnOpenerRows).
	mintedRows := noticeTurnOpenerRows(sess.HarnessUserContents())
	if len(mintedRows) != 1 {
		t.Fatalf("session recorded %d minted notice turn(s) %q, want exactly 1 — the notice names the job ONCE per turn (a second would mean the same completion was delivered as two turns); all harness rows were %q",
			len(mintedRows), mintedRows, sess.HarnessUserContents())
	}
	if !strings.Contains(mintedRows[0], chatNoticeOutputMark) {
		t.Fatalf("minted turn = %q, want the agent's formatter, not an echo of the job's output", mintedRows[0])
	}
}

// TestChatNoticeTurn_WakesAParkedConsole is the EDGE's own acceptance, and it is
// a DIFFERENT case from the one above on purpose. There, the job finished while
// the turn that started it was still running, so the console reached its next
// idle check with the level already true — the level alone (no warning edge)
// opens that turn. The edge is load-bearing only when the notice lands while the
// REPL is ALREADY PARKED at its prompt, which is what this case arranges: the
// job's command waits long enough for its own turn to end first.
//
// So the fixture is a slow job, and the wait below is the proof: nothing is typed
// after the first line, the console is idle, and a turn opens anyway.
func TestChatNoticeTurn_WakesAParkedConsole(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_slow_job",
			Name: "job",
			Args: map[string]interface{}{"action": "start", "command": chatNoticeSlowJobCommand},
		}},
		testutil.Resp{Text: "Started the slow job; the console will be idle before it lands."},
		testutil.Resp{Text: "The slow job finished — reported above."},
	)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat", "--tools"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	chat.writeGapped(t, chatNoticeOperatorLine)
	chat.write(t, "\r")

	// The console answers the operator's turn and PARKS — this is the state the
	// edge is about, and it is reached with no further input.
	chat.waitFor(t, chatNoticeSlowJobStarted, ptyPromptTimeout)
	// The job is still running at this point (its command sleeps first), so the
	// next thing that happens on this terminal is NOT the operator's doing.
	chat.waitFor(t, chatNoticeAnnounceMark, chatNoticeEdgeTimeout)

	minted := waitForNoticeRequest(t, llm)
	if len(minted) == 0 {
		t.Fatalf("the parked console announced a turn but no model request carries the notice message (%q)", chatNoticeMintedMark)
	}
	if !strings.Contains(minted[0], chatNoticeOutputMark) {
		t.Fatalf("the minted turn does not name the job that woke the console:\n%s", minted[0])
	}

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — the session ends at the deliberate /quit", code)
	}

	// The record: the operator's line first, byte-identical, and the minted notice
	// turn appended after it — the past was never rewritten.
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	// The same TWO ROWS, TWO VIEWS split as the case above, and for the same
	// measured reason: the operator's line is the operator view's whole content
	// here, and the minted row the wake appended is a harness injection (the
	// opener is built by agent.ChatJobNoticeOpenLine, whose text opens with the
	// harness prefix), so it can only be read from the harness view.
	op := sess.UserContents()
	if len(op) != 1 {
		t.Fatalf("session recorded %d operator turn(s) %q, want exactly 1 (the operator's line; the minted turn is a harness injection and lives in the harness view)",
			len(op), op)
	}
	if op[0] != chatNoticeOperatorLine {
		t.Fatalf("operator turn 1 = %q, want %q byte-for-byte", op[0], chatNoticeOperatorLine)
	}
	mintedRows := noticeTurnOpenerRows(sess.HarnessUserContents())
	if len(mintedRows) != 1 {
		t.Fatalf("session recorded %d minted notice turn(s) %q, want exactly 1; all harness rows were %q",
			len(mintedRows), mintedRows, sess.HarnessUserContents())
	}
	if !strings.Contains(mintedRows[0], chatNoticeOutputMark) {
		t.Fatalf("minted turn = %q, want the agent's minted notice turn", mintedRows[0])
	}
}
