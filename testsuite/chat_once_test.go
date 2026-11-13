package testsuite

// chat_once_test.go — the END-TO-END surface of `gopi chat --once`
// (cmd/agent/chat_once.go). Every case drives the REAL binary through
// testutil.RunGopi, so what is asserted is what a caller's shell sees: argv in,
// stdout/stderr/exit out. A unit test cannot see this face's contract — the
// whole deliverable is a process that takes one prompt, runs one turn and
// leaves an honest exit status behind (PROJECT.md Rule 31).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
)

// chatOnceAnswer is the mock's one-turn answer, comfortably longer than the
// engine's degenerate-round floor (minFinalAnswerChars), so a completed turn
// really produces the result instead of being re-issued.
const chatOnceAnswer = "Done — the one-shot chat turn answered through the real REPL."

// chatOnceProbeFile is the file the --tools case's mock asks the engine to
// read, and the marker the answer's tool RESULT must carry.
const (
	chatOnceProbeFile   = "chat-once-probe.txt"
	chatOnceProbeMarker = "chat-once-tool-probe-content"
)

// TestChatOnce_FlagSpelling_OneTurnThenExit is the headline case: `-p` (the
// familiar spelling) must run ONE turn, put the answer on stdout, and exit 0
// without a terminal anywhere in the picture.
func TestChatOnce_FlagSpelling_OneTurnThenExit(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "-p", "say OK"}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat --once exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, chatOnceAnswer) {
		t.Errorf("stdout = %q, want the turn's answer", stdout)
	}
	// The answer is on stdout and the chrome is not (a driver pipes stdout).
	if strings.Contains(stdout, "you>") || strings.Contains(stdout, "bye.") {
		t.Errorf("stdout carries the REPL's chrome, not just the answer: %q", stdout)
	}
	// Exactly ONE turn: the exchange reached the engine once, and the run left
	// one durable session behind (the REPL's own closeout).
	if n := llm.RequestCount(); n == 0 {
		t.Fatal("the mock served no request — no turn ran")
	}
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertCounts(t, map[string]int{
		"session_start": 1,
		"user":          1,
		"assistant":     1,
		"session_end":   1,
	})
	if got := sess.SessionEndExitCode(); got != 0 {
		t.Errorf("session_end exit_code = %d, want 0", got)
	}
}

// TestChatOnce_StdinPrompt is the invocation the operator asked for:
//
//	echo 'bootstrap starter prompt to push flywheel' | gopi chat --once --tools
//
// The prompt arrives on stdin, the answer leaves on stdout, and the process
// exits with nothing left running — no TTY, no editor, no prompt loop.
func TestChatOnce_StdinPrompt(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"},
		"bootstrap starter prompt to push the flywheel\n",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat --once exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, chatOnceAnswer) {
		t.Errorf("stdout = %q, want the answer for the piped prompt", stdout)
	}
	// The piped prompt really reached the model (this is the whole point — the
	// answer being present cannot prove WHICH prompt produced it).
	body := llm.RequestBody(1)
	if !strings.Contains(body, "bootstrap starter prompt") {
		t.Errorf("the model request does not carry the piped prompt: %.400s", body)
	}
	if n := llm.RequestCount(); n != 1 {
		t.Errorf("the engine was called %d times, want exactly 1 (one prompt, one turn)", n)
	}
}

// TestChatOnce_NoPromptIsAUsageError: an empty stdin (or a `--once` with
// nothing at all) must fail LOUDLY with the usage class — a driver must be able
// to tell "my prompt never arrived" from "the model had nothing to say".
func TestChatOnce_NoPromptIsAUsageError(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code == 0 {
		t.Fatalf("gopi chat --once with no prompt exit = 0, want a usage failure\n--- stdout ---\n%s", stdout)
	}
	if !strings.Contains(stderr, "needs a prompt") {
		t.Errorf("stderr = %q, want it to say the prompt never arrived", stderr)
	}
	if n := llm.RequestCount(); n != 0 {
		t.Errorf("the engine was called %d times for a run with no prompt, want 0", n)
	}
}

// TestChatOnce_FailedTurnIsNonZeroAndSaysWhy: the mock refuses the turn (HTTP
// 400). Exit must be non-zero, stdout must stay empty, and stderr must name the
// failure — the same honesty `gopi cli --once` carries (a completed-but-empty
// turn and a failed turn are both failures, never a silent 0).
func TestChatOnce_FailedTurnIsNonZeroAndSaysWhy(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Fail: true})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "-p", "make it fail"}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code == 0 {
		t.Fatalf("gopi chat --once on a refused turn exit = 0, want non-zero\n--- stderr ---\n%s", stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout = %q, want empty — a failed turn produced no answer", stdout)
	}
	if !strings.Contains(stderr, "turn failed") {
		t.Errorf("stderr = %q, want the failure named (Rule 15: never a silent failure)", stderr)
	}
}

// TestChatOnce_ToolsTurnReallyCallsATool is the case that makes `--tools` mean
// something on this face (and the proof Rule 11 asks for — the tool was not
// merely advertised, it RAN). The mock's first round asks for `read`; the
// script's second round answers. Assertions: the write/read landed, the TOOL
// ACTIVITY is visible to the caller on stderr, and stdout still carries only
// the answer.
func TestChatOnce_ToolsTurnReallyCallsATool(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	// The probe the model asks to read lives in the run's cwd.
	probe := filepath.Join(work, chatOnceProbeFile)
	if err := os.WriteFile(probe, []byte(chatOnceProbeMarker+"\n"), 0o644); err != nil {
		t.Fatalf("write probe: %v", err)
	}

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_chat_once_read",
			Name: "read",
			Args: map[string]interface{}{"path": probe},
		}},
		testutil.Resp{Text: chatOnceAnswer},
	)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "--tools", "-p", "read the probe file"},
		"", testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat --once --tools exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	// (a) the toolset was ON the wire — the request carries the tool definitions.
	body := llm.RequestBody(1)
	if !strings.Contains(body, `"name":"read"`) {
		t.Errorf("the --tools turn's request carries no tool definitions: %.400s", body)
	}
	// (b) the SECOND round carries the tool's RESULT — the tool really ran
	// through the engine's own execution path, not merely appeared in the wire
	// definition list.
	if n := llm.RequestCount(); n < 2 {
		t.Fatalf("the mock served %d requests, want >= 2 (the tool round and the answer round)", n)
	}
	second := llm.RequestBody(2)
	if !strings.Contains(second, "tool_result") || !strings.Contains(second, chatOnceProbeMarker) {
		t.Errorf("the follow-up round carries no tool_result with the probe's content: %.400s", second)
	}
	// (c) the caller can SEE that the model acted — without this, a driver
	// cannot tell a tool turn from a plain chat turn.
	//
	// THE TWO GLYPHS ARE DIFFERENT ON PURPOSE. The tool-CALL row opens with the
	// call marker and the RESULT row continues the SAME note below it, so it
	// carries the note-continuation gutter (internal/lineedit, NoteContinuation),
	// not the call marker again. This assertion used to demand the call marker on
	// BOTH rows, which the product has never printed: it failed with "stderr does
	// not report the tool result" while the result row — `↳ result read` — was
	// right there in the output (measured, task 01a16885-6754). Asserting a
	// rendered shape the surface does not draw is the same class of defect as a
	// command nobody ran: it cannot fail for the reason it claims to.
	if !strings.Contains(stderr, lineedit.ToolGlyph+" call read") {
		t.Errorf("stderr does not report the tool call — a driver cannot observe the tool activity:\n%s", stderr)
	}
	if !strings.Contains(stderr, lineedit.NoteContinuation+"result read") {
		t.Errorf("stderr does not report the tool result (want %q):\n%s",
			lineedit.NoteContinuation+"result read", stderr)
	}
	// (d) stdout stays the answer alone: the activity must not pollute it.
	if strings.Contains(stdout, "⚙") {
		t.Errorf("the tool activity leaked into stdout, which a driver parses as the answer: %q", stdout)
	}
	if !strings.Contains(stdout, chatOnceAnswer) {
		t.Errorf("stdout = %q, want the turn's answer", stdout)
	}
}

// TestChatOnce_HelpCarriesTheOneShotContract: `-h` reaches the chat page and
// that page documents the one-shot face (its spellings, its prompt sources and
// its exit statuses). It is the surface a caller finds the flag through, so the
// assertion is on the REAL binary's output, not on the constant.
func TestChatOnce_HelpCarriesTheOneShotContract(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "-h"}, "",
		testutil.MockEnv(t, home, ""), work)

	if code != 0 {
		t.Fatalf("gopi chat --once -h exit = %d, want 0\n--- stderr ---\n%s", code, stderr)
	}
	for _, want := range []string{"--once, -p", "--prompt TEXT", "stdin", "Exit status"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("`gopi chat --once -h` does not carry %q:\n%s", want, stdout)
		}
	}
}

// TestChatOnce_InteractiveShapeIsUntouched is the regression guard on the OTHER
// face of this verb: `gopi chat` with no `--once` must still be the interactive
// REPL (two piped turns, /quit, bye.), byte for byte as before. The one-shot
// addition must not have captured the interactive path.
func TestChatOnce_InteractiveShapeIsUntouched(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: "first interactive answer."},
		testutil.Resp{Text: "second interactive answer."},
	)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat"},
		"hello there\nand again\n/quit\n", testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("interactive gopi chat exit = %d, want 0\n--- stderr ---\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "first interactive answer.") || !strings.Contains(stdout, "second interactive answer.") {
		t.Errorf("stdout = %q, want BOTH interactive turns", stdout)
	}
	if !strings.Contains(stderr, "bye.") {
		t.Errorf("stderr = %q, want the interactive goodbye", stderr)
	}
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertCounts(t, map[string]int{"session_start": 1, "user": 2, "assistant": 2, "session_end": 1})
}
