package testsuite

// chat_bash_escape_interrupt_pty_test.go — THE OPERATOR'S ESCAPE REACHING A
// RUNNING bash COMMAND, end to end on a real PTY (Rule 31).
//
// WHY THIS FILE EXISTS. The epic that produced it was opened on a measured
// defect: `internal/tools/bash.go` built its own root with
// context.Background(), so the run-scoped context the chat surface cancels on
// Escape (cli_chat.claimTurnInterrupt → agent.InterruptRunBy → runScopedRoot)
// never reached a command already executing. The turn unwound; the command kept
// running to its own budget — an operator watching `sleep 300` press Escape and
// see nothing stop. The layer that proves the fix is the WHOLE CHAIN: terminal
// line discipline → editor key decoder (a LONE Escape is only known to be lone
// after the bounded ESC wait) → the REPL's turn loop → the run-scoped ctx →
// executeToolCtx → BashInDirCtx → the kill. A unit test of any one of those
// passes with the others broken, which is why this case drives the real binary
// on a pty and reads the PROCESS TABLE for its verdict.
//
// WHAT IS ASSERTED, and where each fact is read from:
//
//	command-started — the command published its pid, read from a file the COMMAND
//	                  wrote: ground truth that a real bash is running, not an
//	                  inference from the model's text.
//	command-dead    — no process in the child's process GROUP survives the
//	                  Escape, measured on /proc (kill(pid,0) is not usable here:
//	                  a SIGKILLed child that the chat process has not reaped is a
//	                  ZOMBIE, and kill(pid,0) on a zombie succeeds).
//	chat-survived   — the process that was running the command is still serving
//	                  the session (the next prompt is answered), which is the
//	                  operator's hard constraint stated as an observation: the
//	                  kill took the child's tree, never the caller.
//	cancel-worded   — the session's tool_result for that call carries
//	                  "[command cancelled by the operator]" — the model (and the
//	                  transcript) must be told a human stopped it, not that a
//	                  budget expired.
//
// Everything is spelled as a LITERAL, deliberately: this file must also compile
// and run against the pre-fix tree, where it fails for the right reason (the
// command outlives the key), rather than failing to build against a symbol that
// did not exist yet.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// The case's own bounds, every one named (Rule 1).
const (
	// bashEscapeSleepSeconds is the command's sleep: far longer than any window
	// this case waits, so the only thing that can end it is the kill under test.
	bashEscapeSleepSeconds = 600
	// bashEscapePromptDelay is how long the case waits after submitting the turn
	// before pressing Escape — long enough for the tool round to start (the mock
	// answers instantly) and for the command to publish its pid; the pid file
	// wait below is what actually orders the two.
	bashEscapePromptDelay = 1 * time.Second
	// bashEscapeDeathWindow is how long /proc is polled for the group to vanish
	// after the key. It is a readiness window on a SIGKILL + reap, not a budget.
	bashEscapeDeathWindow = 20 * time.Second
	// bashEscapePollInterval is the poll step for the /proc waits.
	bashEscapePollInterval = 50 * time.Millisecond
	// bashEscapePidWait is how long the case waits for the command to publish its
	// pid before pressing the key.
	bashEscapePidWait = 15 * time.Second
	// bashEscapeMarker is the operator-facing marker the transcript must carry —
	// the epic's own spelling, asserted as a literal so this file is a RED proof
	// on the pre-fix tree.
	bashEscapeMarker = "[command cancelled by the operator]"
	// bashEscapeToolUseID is the scripted tool_use id, so the tool_result this
	// case asserts on can be found by id rather than by position.
	bashEscapeToolUseID = "call_bash_escape_01"
	// bashEscapeAnswer is the mock's second-round answer: its appearance proves
	// the agent loop CONTINUED after the interrupted tool round (the round did
	// not wedge the session).
	bashEscapeAnswer = "after-the-interrupt-answer"
	// bashEscapePolicyArg is the posture this case runs under, spelled as the
	// argv word the chat verb parses.
	//
	// IT IS A PRECONDITION, NOT A CONVENIENCE. The fixture HOME carries no
	// settings.json document, so the chat surface resolves its truth from the
	// built-in defaults: table `prompt *`, posture on-request. A bash call then
	// meets a `prompt` rule and is QUESTIONED rather than run, so no command ever
	// starts and there is no pid to Escape out of — measured, task 01a16885-6754:
	// "the bash command never published its pid … the tool round did not start a
	// command". The case is about an Escape reaching a RUNNING command, so it has
	// to state the posture that lets one run; the approval channel
	// (internal/tools/chat_approval_authorization.go) is a different question and
	// a different case.
	bashEscapePolicyArg = "--policy=allow"
	// bashEscapeDrainWindow bounds the read that looks for output that must NOT
	// appear (the second answer, if it never comes).
	bashEscapeDrainWindow = 500 * time.Millisecond
	// bashEscapeProcRoot is the procfs mount point this case reads process state
	// from, and bashEscapeZombieState is stat's state letter for an already-dead
	// process that still holds a table slot: skipping those is what makes "the
	// group is gone" a real observation instead of a kill(pid, 0) artifact (that
	// call SUCCEEDS on a zombie).
	bashEscapeProcRoot    = "/proc"
	bashEscapeZombieState = "Z"
)

// bashEscapePidPath is where the command publishes its own pid — inside the
// test's own temp dir, so the harness reads a file the COMMAND wrote.
func bashEscapePidPath(dir string) string { return filepath.Join(dir, "bash-escape.pid") }

// bashEscapeCommand is the command the model is scripted to run: it publishes
// bash's own pid (the process GROUP leader bash() created with Setpgid) and then
// sleeps far past the case's window.
func bashEscapeCommand(pidFile string) string {
	return fmt.Sprintf("echo $$ > %s; sleep %d", pidFile, bashEscapeSleepSeconds)
}

// bashEscapeReadPid polls for the pid the command published.
func bashEscapeReadPid(t *testing.T, pidFile string) int {
	t.Helper()
	var pid int
	deadline := time.Now().Add(bashEscapePidWait)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			if n, cerr := strconv.Atoi(strings.TrimSpace(string(data))); cerr == nil && n > 0 {
				pid = n
				break
			}
		}
		time.Sleep(bashEscapePollInterval) //stylegate:allow-sleep poll for the command to publish its pid; the wait is a readiness handshake
	}
	if pid == 0 {
		t.Fatalf("the bash command never published its pid to %s — the tool round did not start a command", pidFile)
	}
	return pid
}

// bashEscapeGroupAlive reports whether ANY process of the group led by pgid is
// still in the process table. It reads /proc directly because kill(pid,0)
// cannot distinguish a live process from a ZOMBIE — a SIGKILLed child whose
// parent has not reaped it fails the "did the group die" question while looking
// exactly like a survivor.
func bashEscapeGroupAlive(pgid int) bool {
	entries, err := os.ReadDir(bashEscapeProcRoot)
	if err != nil {
		return false
	}
	for _, e := range entries {
		pid, perr := strconv.Atoi(e.Name())
		if perr != nil {
			continue
		}
		if bashEscapeIsZombie(pid) {
			continue
		}
		if g, gerr := syscall.Getpgid(pid); gerr == nil && g == pgid {
			return true
		}
	}
	return false
}

// bashEscapeIsZombie reports whether the process is already dead (state Z): it
// holds a table slot but occupies nothing. The field is read through
// internal/procname's ONE stat parse (Rules 13/14) rather than by counting
// columns here — the comm field is parenthesized and may itself contain spaces
// and parentheses.
func bashEscapeIsZombie(pid int) bool {
	raw, ok := procname.StatFieldString(procname.StatStatPath(bashEscapeProcRoot, strconv.Itoa(pid)), procname.StatFieldState)
	return ok && raw == bashEscapeZombieState
}

// bashEscapeWaitForGroupGone polls until no live member of the group remains.
func bashEscapeWaitForGroupGone(pgid int) bool {
	deadline := time.Now().Add(bashEscapeDeathWindow)
	for time.Now().Before(deadline) {
		if !bashEscapeGroupAlive(pgid) {
			return true
		}
		time.Sleep(bashEscapePollInterval) //stylegate:allow-sleep poll for the killed group to be reaped; readiness window, not a budget
	}
	return !bashEscapeGroupAlive(pgid)
}

// TestChatPTY_EscapeKillsARunningBashCommand is the end-to-end acceptance case:
// Escape during a bash tool round kills the command's whole process group, the
// transcript says the operator cancelled it, and the chat process itself is
// untouched.
func TestChatPTY_EscapeKillsARunningBashCommand(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	pidFile := bashEscapePidPath(work)

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   bashEscapeToolUseID,
			Name: "bash",
			Args: map[string]interface{}{"command": bashEscapeCommand(pidFile)},
		}},
		testutil.Resp{Text: bashEscapeAnswer},
	)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat", "--tools", bashEscapePolicyArg}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	// The turn goes in flight; the model's tool_use starts a real bash command.
	chat.writeGapped(t, "run the long command")
	chat.write(t, "\r")

	// GROUND TRUTH that the command is running: the pid the COMMAND wrote.
	childPid := bashEscapeReadPid(t, pidFile)
	childGroup, err := syscall.Getpgid(childPid)
	if err != nil {
		t.Fatalf("read the group of the running command (pid %d): %v", childPid, err)
	}
	// Precondition, MEASURED: the tool child really owns its group (bash sets
	// Setpgid). Without it the group assertion below would be asking about a
	// group the CHAT process is in — i.e. the very hazard the recipe avoids.
	if childGroup != childPid {
		t.Fatalf("the command's group is %d but its pid is %d — the tool child is not its own group leader, so this case cannot assert anything about a group", childGroup, childPid)
	}
	if !bashEscapeGroupAlive(childGroup) {
		t.Fatalf("no live process in group %d right after the pid was published — the command was not running", childGroup)
	}

	time.Sleep(bashEscapePromptDelay) //stylegate:allow-sleep aims the Escape into the middle of the tool round — the DEFECT is about a command still running, so the wait must outlast the round's start

	// A LONE Escape: the byte, then nothing (the editor's bounded ESC wait is
	// what resolves it as the key rather than as an arrow prefix).
	chat.write(t, "\x1b")

	// THE COMMAND DIES — polled on /proc, so a zombie does not read as a survivor.
	if !bashEscapeWaitForGroupGone(childGroup) {
		t.Fatalf("process group %d still has a live member after the Escape — the operator's key did not reach the running command (pid %d)", childGroup, childPid)
	}

	// THE CHAT PROCESSS SURVIVES the kill it just delivered. This is the
	// operator's hard constraint, and it is read as behaviour: the same process
	// answers the next prompt.
	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Errorf("chat exit = %d, want 0 (Escape during a bash round must not take the session down)", code)
	}

	// THE TRANSCRIPT NAMES THE CANCELLATION (Rule 15: the model and the reader
	// must be told a human stopped the command, not that a budget expired).
	sess := testutil.LoadSession(t, home)

	got := bashEscapeToolResultFor(t, sess, bashEscapeToolUseID)
	if !strings.Contains(got, bashEscapeMarker) {
		t.Errorf("the tool_result for %s does not carry %q — the operator's cancellation is not recorded; result:\n%s",
			bashEscapeToolUseID, bashEscapeMarker, got)
	}
	for _, timeoutWord := range []string{"timed out after", "no output for"} {
		if strings.Contains(got, timeoutWord) {
			t.Errorf("the cancelled command's tool_result carries %q — a cancellation must not be reported as a timeout; result:\n%s",
				timeoutWord, got)
		}
	}
}

// bashEscapeToolResultFor returns the text of the tool_result block answering the
// given tool_use id, or fails: a missing result means the round produced nothing
// for the model, which is itself a defect worth reporting rather than skipping.
//
// The blocks live under the event's MESSAGE (`{"message":{"content":[…]}}`), which
// is the session record's own shape — the test-side walk has to match the
// artifact rather than a convenient reading of it.
func bashEscapeToolResultFor(t *testing.T, sess *testutil.Session, toolUseID string) string {
	t.Helper()
	for _, ev := range sess.Events {
		if ev.Type != "tool_result" {
			continue
		}
		msg, _ := ev.Raw["message"].(map[string]interface{})
		if msg == nil {
			continue
		}
		blocks, _ := msg["content"].([]interface{})
		for _, raw := range blocks {
			b, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if id, _ := b["tool_use_id"].(string); id != toolUseID {
				continue
			}
			return toolResultText(b)
		}
	}
	t.Fatalf("the session has no tool_result for tool_use %q; event types: %v", toolUseID, sess.Types())
	return ""
}

// toolResultText flattens a tool_result block's content into one string: the
// content is a LIST of blocks whose text field carries the result.
func toolResultText(b map[string]interface{}) string {
	var sb strings.Builder
	switch c := b["content"].(type) {
	case string:
		sb.WriteString(c)
	case []interface{}:
		for _, item := range c {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if s, ok := m["text"].(string); ok {
				sb.WriteString(s)
			}
		}
	}
	return sb.String()
}
