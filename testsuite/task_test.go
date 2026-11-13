// Package testsuite holds the three-mode E2E smoke suite for gopi: single
// task (stdin/positional), chat REPL, and web server. Every test drives the
// REAL bin/gopi binary as a subprocess against a scripted mock LLM
// (internal/testutil), with a sandboxed GOPI_HOME — zero real API calls,
// green in a keyless/offline environment. Run via scripts/e2e_smoke.sh
// (builds the binary first); a plain `go test ./...` skips these tests when
// bin/gopi is absent.
package testsuite

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// taskCompleteAnswer is the mock's final answer for a task run: a completed
// (empty) task list + a substantive closing statement, so the loop's R3
// first-end_turn trust can fire (devdoc/bug022 — a first end_turn is only
// trusted when the task list is all-completed AND the answer is substantive,
// ≥40 chars of non-task-list text; the empty [TASK_LIST] is trivially
// all-completed). The answer must be long enough that the run exits without
// an extra confirmation round — and if the completion-store enforcement
// (maybeEnforceCompletionStore) injects one more round, the mock repeats the
// last script entry and the loop still exits on the 2nd consecutive end_turn.
const taskCompleteAnswer = "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\n" +
	"Task complete. The plan file was written to disk and all requested work is finished."

// TestTask_HappyPath_Stdin drives the canonical single-task path
// `echo task | gopi`: the mock first answers with a tool_use (write), the
// real agent loop must EXECUTE the tool (file lands in the sandbox cwd), then
// answers with the final text. Asserts the session JSONL event sequence
// session_start → user → assistant(tool_use) → tool_result → assistant(text)
// → session_end with a single session id and a tool_use/tool_result pair.
func TestTask_HappyPath_Stdin_ToolExecutesAndSessionLogs(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_write_plan",
			Name: "write",
			Args: map[string]interface{}{
				"path":    "plan.txt",
				"content": "1. build the suite\n2. verify green",
			},
		}},
		testutil.Resp{Text: taskCompleteAnswer},
	)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, nil, "write the task plan to plan.txt\n",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}

	// The tool REALLY executed: the write landed in the sandbox cwd.
	data, err := os.ReadFile(filepath.Join(work, "plan.txt"))
	if err != nil {
		t.Fatalf("plan.txt not written by the executed write tool: %v (stderr: %s)", err, stderr)
	}
	if got := string(data); !strings.Contains(got, "1. build the suite") {
		t.Errorf("plan.txt content = %q, want the tool_use input", got)
	}

	// The final answer streamed to stdout.
	if !strings.Contains(stdout, "Task complete.") {
		t.Errorf("stdout = %q, want the mock's final answer", stdout)
	}

	// The mock was actually driven (tool round + at least one answer round).
	if n := llm.RequestCount(); n < 2 {
		t.Errorf("mock served %d requests, want >= 2 (tool round + answer round)", n)
	}

	// Session JSONL structure. The task-start checkpoint injection (M2
	// enforcement, 621a60e) logs its own user event — so the run's user
	// events are [checkpoint, prompt]. Assert on the PROMPT content, not
	// the count (the injection set is harness policy, asserted in the
	// agent package).
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertEventOrder(t,
		"session_start", "assistant", "tool_result", "assistant", "session_end")
	sess.AssertCounts(t, map[string]int{
		"session_start": 1,
		"tool_result":   1,
		"session_end":   1,
	})
	sess.ToolUseResultPairs(t)
	// The task-start checkpoint injection (621a60e) logs its own user event
	// before the prompt — assert the stdin prompt is among the user events.
	hasPrompt := false
	for _, u := range sess.UserContents() {
		if strings.Contains(u, "write the task plan to plan.txt") {
			hasPrompt = true
		}
	}
	if !hasPrompt {
		t.Errorf("user messages = %v, want the stdin prompt among them", sess.UserContents())
	}

	// The recorded tool_use block carries the right name + input.
	tus := sess.ToolUses()
	if len(tus) != 1 {
		t.Fatalf("session recorded %d tool_use blocks, want 1: %v", len(tus), sess.Summary())
	}
	if tus[0].Name != "write" || tus[0].ID != "call_write_plan" {
		t.Errorf("tool_use = %+v, want name=write id=call_write_plan", tus[0])
	}
	if got := tus[0].Input["path"]; got != "plan.txt" {
		t.Errorf("tool_use input path = %v, want plan.txt", got)
	}
	if code := sess.SessionEndExitCode(); code != 0 {
		t.Errorf("session_end exit_code = %d, want 0", code)
	}
}

// TestTask_HappyPath_Positional drives the positional variant — the prompt
// arrives as an argv argument instead of piped stdin — and a second full run
// in a fresh sandbox (the "2 happy-path rounds" of the smoke contract).
func TestTask_HappyPath_Positional(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_write_marker",
			Name: "write",
			Args: map[string]interface{}{
				"path":    "marker.txt",
				"content": "positional run ok",
			},
		}},
		testutil.Resp{Text: taskCompleteAnswer},
	)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"write marker.txt with positional run ok"},
		"", // no stdin — the positional must win
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(work, "marker.txt")); err != nil {
		t.Fatalf("marker.txt not written: %v", err)
	}
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.ToolUseResultPairs(t)
	// The task-start checkpoint injection (621a60e) logs an extra user event
	// before the prompt — assert on the PROMPT being present, not the count.
	users := sess.UserContents()
	hasPrompt := false
	for _, u := range users {
		if strings.Contains(u, "positional run ok") {
			hasPrompt = true
		}
	}
	if !hasPrompt {
		t.Errorf("user messages = %v, want the positional prompt among them", users)
	}
}

// TestTask_ErrorScenario_LLMFailure: the mock answers HTTP 400 (non-retryable
// — the client only retries 429/5xx). The run must fail with a nonzero exit
// and an error on stderr, and the session JSONL must still record the
// session_end with exit_code=1 (the loop's deferred finalizer runs on error).
func TestTask_ErrorScenario_LLMFailure(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Fail: true})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, nil, "make the model fail\n",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code == 0 {
		t.Fatalf("gopi exit = 0, want nonzero on LLM failure\n--- stdout ---\n%s\n--- stderr ---\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "Error") {
		t.Errorf("stderr = %q, want an error message", stderr)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertEventOrder(t, "session_start", "user", "session_end")
	if code := sess.SessionEndExitCode(); code != 1 {
		t.Errorf("session_end exit_code = %d, want 1 (failed run)", code)
	}
}

// TestTask_BarePrompt_SIGTERM_WritesSessionEnd is the regression guard for
// the chat-once/bare-prompt SIGTERM zombie bug (2026-09-05 evidence
// t-1788514086/1788514159/1788514437): a bare-prompt `gopi "..."` process
// killed by SIGTERM used to die by the Go runtime's default signal
// disposition — runEngine's deferred session_end + auto-ledger row close
// never ran, leaving an in_progress zombie with the task text done but no
// session_end.
//
// The fix (cli_web bug003 pattern extended to the run/chat paths): main.go /
// cli_chat.go install signal.Notify(os.Interrupt, SIGTERM) and call
// Agent.Interrupt(), which cancels the run ctx so the loop unwinds through
// its normal end-of-run defer — session_end exit_code=1 + auto row failed.
//
// This test drives the REAL binary against the mock LLM with a Hold response:
// the run blocks mid-LLM-round-trip (HeldCount reaches 1), the test SIGTERMs
// the process, and asserts the session JSONL still ends with exactly one
// session_end carrying exit_code=1 (not a signal-killed death with no
// session_end, and not exit 143/137 from the default disposition).
func TestTask_BarePrompt_SIGTERM_WritesSessionEnd(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Hold: true})
	defer llm.Close()

	proc := testutil.StartGopi(t, nil, "start a long analysis and report back\n",
		testutil.MockEnv(t, home, llm.URL()), work)
	defer proc.Stop() // idempotent — no-op once the process has exited

	// Wait until the run is genuinely IN-FLIGHT: the mock has accepted the
	// first LLM request and is holding the stream open. SIGTERM before this
	// point could land while the loop is still in setup (before the signal
	// handler even exists) and would test nothing.
	proc.WaitFor("the first LLM request to be held mid-stream", func() bool {
		return llm.HeldCount() >= 1
	})

	// SIGTERM — the graceful-closeout trigger under test.
	proc.Signal(syscall.SIGTERM)

	code := proc.Wait()
	if code == -1 || code == 143 || code == 137 {
		t.Fatalf("gopi exit = %d after SIGTERM, want 1 (graceful closeout; the signal must not kill the process by default disposition)", code)
	}

	// The session JSONL must carry exactly one session_end with exit_code=1
	// — the runEngine defer wrote it during the graceful unwind.
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	if ends := sess.Counts()["session_end"]; ends != 1 {
		t.Errorf("session_end count = %d, want exactly 1 (the SIGTERM must not leave the session open)", ends)
	}
	if code := sess.SessionEndExitCode(); code != 1 {
		t.Errorf("session_end exit_code = %d, want 1 (signal-interrupted run)", code)
	}

	// The interrupt notice went to stderr — the user sees why the session
	// ended instead of a silent kill.
	if !strings.Contains(proc.Stderr(), "finalizing session") {
		t.Errorf("stderr = %q, want the 'finalizing session' notice", proc.Stderr())
	}
}
