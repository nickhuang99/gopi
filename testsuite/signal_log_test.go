package testsuite

// signal_log_test.go — the SIGNAL-VERDICT-EVENT guard (epic
// signalrec-session-log-verdict, 2026-09-17).
//
// THE DEFECT THIS PINS. internal/signalrec documented a session-log verdict
// line that NO process had ever written: the sole production caller passed a
// literal "" for the session-log path (cmd/agent/main.go, `Install("")`), so the
// writer returned immediately every time. The wiring under test is the run's own
// handoff (SetSessionDir, next to the printLogPaths that makes the session dir
// true), and only a REAL `gopi` process can prove it: the path is unknown while
// main() installs the recorder and becomes true hundreds of lines later.
//
// WHAT IT DRIVES. The real binary (testutil.StartGopi) against the scripted mock
// LLM, SIGTERMed mid-run exactly like TestTask_BarePrompt_SIGTERM_WritesSessionEnd
// — the run unwinds through its own deferred close-out, and the recorder appends
// its verdict event into the session JSONL the run itself is writing.
//
// WHAT IT ASSERTS, and why each arm is separate:
//
//  1. the session log carries a signal_verdict event — the wiring exists;
//  2. the file still parses line-by-line (testutil.LoadSession fails on any
//     unparsable line), i.e. the verdict event did not cost the session its
//     readability — the property that decided the format;
//  3. `gopi log --json` — the PRODUCTION session reader, which unlike the
//     tolerant parsers returns a hard error on an unparsable line — exits 0 over
//     that very file. Arm 3 is the one that would have failed for a prose line.

import (
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// signalVerdictEventType is the event the recorder appends, spelled through the
// shared constant (Rule 1b/14): the same value the writer produces.
const signalVerdictEventType = sessionevent.EventTypeSignalVerdict

// TestTask_BarePrompt_SIGTERM_WritesTheSignalVerdictEventIntoTheSessionLog is
// the end-to-end proof that the recorder's verdict reaches a REAL run's own
// session log — and that the session log survives it.
func TestTask_BarePrompt_SIGTERM_WritesTheSignalVerdictEventIntoTheSessionLog(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Hold: true})
	defer llm.Close()

	env := testutil.MockEnv(t, home, llm.URL())
	proc := testutil.StartGopi(t, nil, "start a long analysis and report back\n", env, work)
	defer proc.Stop() // idempotent — no-op once the process has exited

	// Wait until the run is genuinely IN-FLIGHT: the mock holds the stream open.
	// SIGTERM before this point could land while the loop is still in setup (the
	// session logger — and therefore the session-log handoff — may not exist yet)
	// and would test nothing.
	proc.WaitFor("the first LLM request to be held mid-stream", func() bool {
		return llm.HeldCount() >= 1
	})

	proc.Signal(syscall.SIGTERM)
	code := proc.Wait()
	if code == -1 || code == 143 || code == 137 {
		t.Fatalf("gopi exit = %d after SIGTERM, want 1 (graceful closeout; the signal must not kill the process by default disposition)", code)
	}

	// ARM 1 + 2: the session log parses line-by-line AND carries the event.
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	ev := sess.First(signalVerdictEventType)
	if ev == nil {
		t.Fatalf("the run's session log %s carries NO %q event after SIGTERM — the recorder's session-log handoff did not reach the run's own log (events: %v)",
			sess.Path, signalVerdictEventType, sess.Types())
	}
	if sig, _ := ev.Raw["signal"].(string); sig == "" {
		t.Errorf("the verdict event carries no signal name: %v", ev.Raw)
	}
	if _, ok := ev.Raw["pid"]; !ok {
		t.Errorf("the verdict event carries no pid: %v", ev.Raw)
	}
	// The run DID close out (exit 1 above), so the verdict event and the
	// session_end are both in the file — the recorder appended to a session log
	// the run kept writing, which is the interleaving this epic had to get right.
	if ends := sess.Counts()[sessionevent.EventTypeSessionEnd]; ends != 1 {
		t.Errorf("session_end count = %d, want exactly 1: the verdict event must not cost the run its close-out", ends)
	}

	// ARM 3: the PRODUCTION reader over the very file the run wrote. `gopi log
	// --last --json` resolves the newest session and streams it through
	// internal/gopilog.FormatJSONL (ValidateSessionFile + the raw pass-through) —
	// so exit 0 is the production reader's own verdict, and the streamed body
	// must carry the verdict event.
	stdout, stderr, logCode := testutil.RunGopi(t, []string{cliverbs.Session, cliverbs.Log, "--last", "--json"}, "", env, work)
	if logCode != 0 {
		t.Fatalf("`gopi log --last --json` exit = %d over the SIGTERMed session log, want 0 — the session readers must tolerate the verdict event (stderr: %s)",
			logCode, stderr)
	}
	if !strings.Contains(stdout, signalVerdictEventType) {
		t.Errorf("`gopi log --last --json` streamed a session without the %q event (stdout head: %.600s)", signalVerdictEventType, stdout)
	}
}
