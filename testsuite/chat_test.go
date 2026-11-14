package testsuite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// chatOpsAnswer is the mock's final answer for a tool-enabled chat turn:
// ChatOps exits on the FIRST no-tool round whose content is substantive
// (>= minFinalAnswerChars = 40 chars — internal/agent/chat_ops.go), so the
// answer must be comfortably longer than that.
const chatOpsAnswer = "Done. The ops file chat-ops.txt has been written with the requested content and verified."

// TestChat_HappyPath_TwoTurnsSameSession drives `gopi chat` non-interactively:
// two plain (no-tools) turns piped via stdin, then /quit. Asserts the exit is
// clean (0, "bye." on stderr), both answers reach stdout, and the session
// JSONL holds BOTH turns in ONE session (one session_start, 2 user, 2
// assistant, one session_end — the Epic's single-session consolidation).
func TestChat_HappyPath_TwoTurnsSameSession(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: "hello from the mock — first answer."},
		testutil.Resp{Text: "two plus two is four."},
	)
	defer llm.Close()

	stdin := "hello there\nwhat is two plus two\n/quit\n"
	stdout, stderr, code := testutil.RunGopi(t, []string{"chat"}, stdin,
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat exit = %d, want 0 (clean /quit)\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "first answer") || !strings.Contains(stdout, "four") {
		t.Errorf("stdout = %q, want both streamed answers", stdout)
	}
	if !strings.Contains(stderr, "bye.") {
		t.Errorf("stderr = %q, want the clean-exit bye message", stderr)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertEventOrder(t, "session_start", "user", "assistant", "user", "assistant", "session_end")
	sess.AssertCounts(t, map[string]int{
		"session_start": 1,
		"user":          2,
		"assistant":     2,
		"session_end":   1,
	})
	if code := sess.SessionEndExitCode(); code != 0 {
		t.Errorf("session_end exit_code = %d, want 0", code)
	}
	if got := sess.UserContents(); len(got) != 2 || got[0] != "hello there" || got[1] != "what is two plus two" {
		t.Errorf("user messages = %v, want the two piped turns", got)
	}
}

// TestChat_ToolsToggle_ToolExecutes verifies the /tools slash command flips
// the chat into tool-enabled ops mode mid-session: after the toggle the next
// turn must put the toolset on the wire (asserted on the mock's request body)
// and actually execute a tool (the write lands in the sandbox cwd). The whole
// conversation still stays ONE session.
func TestChat_ToolsToggle_ToolExecutes(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: "plain chat answer."},
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_ops_write",
			Name: "write",
			Args: map[string]interface{}{
				"path":    "chat-ops.txt",
				"content": "ops mode executed this",
			},
		}},
		testutil.Resp{Text: chatOpsAnswer},
	)
	defer llm.Close()

	stdin := "just chatting\n/tools\nwrite chat-ops.txt\n/quit\n"
	// chatGroundPolicyArg declares the posture this case runs under: the built-in
	// chat-policy table is `prompt *`, and a piped `gopi chat` is UNATTENDED, so
	// without it the write comes back `[NEEDS OPERATOR APPROVAL — command NOT
	// run]` and the file is never created. Measured on a PRISTINE HEAD extract
	// (/dev/shm/head-6435dfd): the same failure, the tool_use carrying
	// "path":"chat-ops.txt" verbatim — the refusal, not a misresolved path, is
	// what leaves the file absent. See chat_ground_test.go's constant comment.
	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", chatGroundPolicyArg}, stdin,
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "tools ENABLED") {
		t.Errorf("stderr = %q, want the /tools ENABLED message", stderr)
	}
	// The tool-enabled turn REALLY executed: the write landed in the cwd.
	data, err := os.ReadFile(filepath.Join(work, "chat-ops.txt"))
	if err != nil || !strings.Contains(string(data), "ops mode executed this") {
		t.Errorf("chat-ops.txt = %q, err %v — the ops turn must have executed the write tool", string(data), err)
	}

	// Wire-shape proof of the toggle: the mock's 2nd request (the ops turn's
	// first round) must carry the toolset with the write tool definition.
	body := llm.RequestBody(2)
	if body == "" {
		t.Fatalf("mock received only %d requests, want >= 2 (plain turn + ops round)", llm.RequestCount())
	}
	if !strings.Contains(body, `"name":"write"`) || !strings.Contains(body, `"tools"`) {
		t.Errorf("ops-turn request body does not carry the toolset (write def): %.400s", body)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertCounts(t, map[string]int{"session_start": 1, "user": 2, "session_end": 1})
	sess.ToolUseResultPairs(t)
	tus := sess.ToolUses()
	if len(tus) != 1 || tus[0].Name != "write" {
		t.Errorf("tool_use blocks = %+v, want the single write call", tus)
	}
}

// TestChat_ErrorScenario_TurnFailure: the mock fails the first turn with a
// 400. A failed turn is a failed TURN: the REPL must surface it on stderr, KEEP
// THE SESSION OPEN, and carry on — the second turn (which the mock answers) must
// really run, and the session must end with the exit code of the deliberate
// close-out (/quit → 0), not of the earlier turn error.
//
// This test used to pin the opposite: the REPL returned 1 on the first turn
// error, so a single 400 (or the ReAct round cap) ended the conversation with the
// input prompt still on screen — bug 01a0d513, measured on session
// bbc4efd8-6bdd-4d6c-aa16-86bae2cecc00.
func TestChat_ErrorScenario_TurnFailure(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Fail: true},
		testutil.Resp{Text: "the session survived the failed turn."},
	)
	defer llm.Close()

	stdin := "make it fail\ntry again\n/quit\n"
	stdout, stderr, code := testutil.RunGopi(t, []string{"chat"}, stdin,
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat exit = %d, want 0 (the failed turn is not the session's exit code; /quit is)\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "turn failed:") {
		t.Errorf("stderr = %q, want the failed-turn notice", stderr)
	}
	if !strings.Contains(stderr, "bye.") {
		t.Errorf("stderr = %q, want the clean-exit bye at /quit", stderr)
	}
	// The REPL really continued: the mock served the SECOND turn.
	if !strings.Contains(stdout, "the session survived the failed turn.") {
		t.Errorf("stdout = %q, want the second turn's answer — the REPL did not come back to the prompt", stdout)
	}
	if n := llm.RequestCount(); n < 2 {
		t.Fatalf("the mock served %d requests, want >= 2 (the failed turn AND the retry)", n)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	if code := sess.SessionEndExitCode(); code != 0 {
		t.Errorf("session_end exit_code = %d, want 0 (deliberate /quit after a failed turn)", code)
	}
	if got := sess.UserContents(); len(got) != 2 || got[1] != "try again" {
		t.Errorf("user messages = %v, want both turns in ONE session", got)
	}
}

// TestChat_RoundCap_FailsOneTurnButKeepsTheSession is the regression test for
// the reported bug, driven through the REAL binary on the exact code path that
// ended session bbc4efd8: a tool-enabled turn whose model keeps issuing tool
// calls hits the bounded ReAct cap (maxChatOpsToolRounds = 40).
//
// Before the fix the process exited 1 there, with the input prompt on screen and
// no further output at all — the operator's "it exited without notice". Now the
// turn fails, the failure is NAMED on the terminal, and the session stays open
// until the user quits: exit 0, `bye.`, one session, one session_end.
func TestChat_RoundCap_FailsOneTurnButKeepsTheSession(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	// The always-looping model: every round issues one more tool call, so the
	// turn can only end at the cap. The script is built to length so it is the
	// CAP (not the script) that ends the turn — the mock repeats its last entry
	// beyond the script anyway.
	script := make([]testutil.Resp, 0, 40)
	for i := 0; i < 40; i++ {
		script = append(script, testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   fmt.Sprintf("call_cap_%02d", i),
			Name: "read",
			Args: map[string]interface{}{"path": "/nonexistent-round-cap-probe"},
		}})
	}
	llm := testutil.NewMockLLM(t, script...)
	defer llm.Close()

	stdin := "loop forever\n/quit\n"
	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--tools"}, stdin,
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat exit = %d, want 0 — a turn at the round cap must not end the session\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	// The notice the operator did not get: what failed, the cap's size, and the
	// fact that the session survives it.
	for _, want := range []string{"turn failed:", "40 tool rounds", "the session is still open"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want the round-cap notice to carry %q", stderr, want)
		}
	}
	if !strings.Contains(stderr, "bye.") {
		t.Errorf("stderr = %q, want the clean-exit bye (the session reached /quit)", stderr)
	}
	// The turn really hit the cap: 40 requests, no more and no fewer.
	if n := llm.RequestCount(); n != 40 {
		t.Errorf("the mock served %d requests, want exactly 40 (the cap)", n)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	if got := sess.SessionEndExitCode(); code != 0 || got != 0 {
		t.Errorf("session_end exit_code = %d, want 0 — the session ended at /quit, not at the cap", got)
	}
	// ONE user message: the failed turn consumed exactly one.
	if got := sess.UserContents(); len(got) != 1 || got[0] != "loop forever" {
		t.Errorf("user messages = %v, want the single turn the cap ended", got)
	}
}
