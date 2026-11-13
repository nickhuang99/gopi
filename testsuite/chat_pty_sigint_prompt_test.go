package testsuite

// chat_pty_sigint_prompt_test.go — a REAL SIGINT delivered AT THE PROMPT must
// leave a session_end, end to end on a real pty (Rule 31: the surface is
// executed, not simulated).
//
// THE MEASURED DEFECT (session b9936006, 2026-10-22). The operator's chat
// stopped writing at 18:32:59 — its own three artifacts froze on the same
// second — and its session.jsonl ended on a `request` record with NO
// session_end anywhere in 1803 lines. The dump filed it as
// `{"category":"orphan-no-session-end"}`, i.e. "process died without
// session_end (kill/panic/power loss)", so the signal, the exit code and the
// last state were all unknowable. There was no signal-provenance.jsonl either.
//
// WHY NOTHING CAUGHT IT. A previous fix (session ab9e0023) made SIGTERM and
// SIGHUP end a chat through the graceful close-out, and that half still works
// (measured on this branch: exit 1, one session_end naming the signal, a
// `signal` dump). SIGINT was the one signal that could not: a raw-mode terminal
// clears ISIG, so the Ctrl+C KEY is a 0x03 byte and never becomes a signal —
// but a REAL SIGINT (kill -INT, a process-group delivery, an ssh teardown) is
// registered by internal/lineedit's CaptureSignals, whose handler is the FATAL
// path. It asked the REPL's own interrupt seam whether the signal was "mine";
// at the prompt the seam answered false ("nothing in flight, let it die"), and
// false means restore attributes, signal.Reset, re-raise. That ran on the
// editor's goroutine and beat the REPL's closeout by 3 ms.
//
// Measured on this branch's binary before the fix, with the harness below's
// exact shape (kill -INT at a booted prompt):
//
//	exit_code:      -2        (killed by the default disposition)
//	vanish_ms:      3
//	session_end:    0         (tail is the session_start record)
//	provenance:     ABSENT
//	dump:           NO DUMP
//
// and after it:
//
//	exit_code:      1
//	vanish_ms:      64
//	session_end:    1         (category "signal", error_body names SIGINT)
//	provenance:     SIGINT
//	dump_category:  signal
//
// The two facts this file pins are therefore the two the operator could not
// have: the session RECORDED its ending, and the process left FAST (the
// close-out must never turn a kill into a hang — Constraint 1).

import (
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/testutil"
)

// The harness's own bounds. Every value is named (Rule 1).
const (
	// sigintPromptExitBudget is the hard ceiling on kill → process gone. The
	// close-out writes one JSONL line and one dump; the measured cost is ~64ms,
	// so this bound is nearly two orders of magnitude of headroom and exists
	// only to fail a run that turned the graceful path into a hang.
	sigintPromptExitBudget = 5 * time.Second
	// sigintPromptSettle is the readiness window for the ARTIFACTS after the
	// child has already exited (the writer ran to completion before exit; this
	// only covers the last flush the kernel owes).
	sigintPromptSettle = 5 * time.Second
	// sigintPromptPollStep is the poll cadence inside that window.
	sigintPromptPollStep = 50 * time.Millisecond
)

// TestChatPTY_SIGINTAtPrompt_RecordsSessionEnd is the regression guard: a real
// SIGINT at the prompt ends the session through the ORDERLY close-out, so the
// session's own log names its ending instead of reading as an unexplained
// disappearance.
func TestChatPTY_SIGINTAtPrompt_RecordsSessionEnd(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := startUsageLLM(t)

	env := testutil.MockEnv(t, home, llm.URL)
	chat := startPTYChat(t, []string{"chat"}, env, work)
	t.Cleanup(func() {
		if chat.cmd.Process != nil {
			_ = chat.cmd.Process.Kill() //stylegate:ignore best-effort teardown of a failed run
		}
		_ = chat.master.Close() //stylegate:ignore the master is the test's own descriptor
	})

	// The signal must arrive in the STEADY STATE a kill finds the process in:
	// booted, parked at the prompt, no turn in flight (that is the arm the
	// defect lived in — a turn in flight was the one case that worked).
	chat.waitFor(t, chatBannerExitFragment, ptyPromptTimeout)

	sent := time.Now()
	if err := chat.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("send SIGINT to the chat process: %v", err)
	}
	code := chat.close(t)
	gone := time.Since(sent)

	// CONSTRAINT 1: the close-out must not turn a kill into a hang.
	if gone > sigintPromptExitBudget {
		t.Errorf("kill -INT → process gone took %s, want under %s — the signal path must not block the exit",
			gone, sigintPromptExitBudget)
	}
	// The graceful path exits 1 (this binary's "the run did not complete"). A
	// NEGATIVE code is the pre-fix shape: Go reports -N for "terminated by signal
	// N", which means NO handler consumed it and nothing was recorded.
	if code < 0 {
		t.Fatalf("chat exit = %d after SIGINT, want a POSITIVE status: a negative code means the process died on the default disposition, so no close-out ran (this is the session-b9936006 shape)", code)
	}

	dir := waitForChatSessionDir(t, home)
	assertSignalSessionEnd(t, home, signameWant)

	t.Logf("measured: exit=%d, kill→gone=%s, session dir=%s", code, gone, dir)
}

// signameWant is the signal this file sends, spelled as the record must name
// it. The spelling is signalrec.SignalName's, but this test asserts on the
// ARTIFACT an operator reads, so it is spelled here rather than imported: a
// rename of the producer's spelling must fail this assertion, not follow it.
const signameWant = "SIGINT"

// assertSignalSessionEnd reads the session's own JSONL and requires exactly one
// session_end whose error block names the signal. The session_end is written by
// the same close-out that closes the CHATS ledger row, so its presence is also
// the difference between a finished session and an in_progress zombie.
func assertSignalSessionEnd(t *testing.T, home, want string) {
	t.Helper()
	deadline := time.Now().Add(sigintPromptSettle)
	for {
		sess := testutil.LoadSession(t, home)
		ends := sess.Counts()[sessionevent.EventTypeSessionEnd]
		if ends == 1 {
			assertSignalEndRecord(t, sess, want)
			return
		}
		if ends > 1 {
			t.Fatalf("%d session_end records, want exactly 1 — a single-session close must not double-write", ends)
		}
		if time.Now().After(deadline) {
			t.Fatalf("NO %s in %s after a real SIGINT at the prompt — the session died without recording how it ended (session b9936006's shape)",
				sessionevent.EventTypeSessionEnd, sess.Path)
		}
		//stylegate:allow-sleep the wait is this loop's POLL CADENCE over an artifact the exited child already wrote; the loop is bounded by sigintPromptSettle and fails BY NAME at the deadline rather than racing it
		time.Sleep(sigintPromptPollStep)
	}
}

// assertSignalEndRecord is the content half: the record must say WHICH signal
// ended the run, not fall through to the unknown default.
func assertSignalEndRecord(t *testing.T, sess *testutil.Session, want string) {
	t.Helper()
	ev := sess.First(sessionevent.EventTypeSessionEnd)
	if ev == nil {
		t.Fatalf("%s carries no %s record after a SIGINT", sess.Path, sessionevent.EventTypeSessionEnd)
	}
	errBlock, _ := ev.Raw["error"].(map[string]any)
	if errBlock == nil {
		t.Fatalf("session_end carries no error block, so the record does not say what ended the session: %v", ev.Raw)
	}
	if got, _ := errBlock["category"].(string); got != signalDumpCategory {
		t.Errorf("session_end error.category = %q, want %q — a handled signal is a KNOWN cause", got, signalDumpCategory)
	}
	body, _ := errBlock["error_body"].(string)
	if body == "" {
		t.Fatalf("session_end error_body is empty — the record does not name the signal: %v", ev.Raw)
	}
	if !strings.Contains(body, want) {
		t.Errorf("session_end error_body does not name %q: %s", want, body)
	}
}
