package testsuite

// term_once_signal_test.go — the regression guard for `gopi cli "<prompt>"
// --once` OWNING its terminating signals (epic 0x01a0d9e0).
//
// THE DEFECT THIS PINS (measured on master, 2026-09-26). `--once` registered no
// SIGTERM handler at all, so the only handler in the process was the provenance
// recorder's (signalrec.Install, early in main). Its argv table cannot classify
// this face — the mode word is `cli`, which owns no entry — so it FORWARDED: a
// provenance record, then signal.Reset(SIGTERM) (global) and a self-kill one
// grace later. Measured: the run exited 143 with session.jsonl holding
// session_start + signal_verdict and NO session_end — the one-shot turn was
// killed mid-turn and its closeout (CloseChatSession → session_end /
// CloseChatRow) never ran.
//
// WHAT IT DRIVES. The real binary (testutil.StartGopi) against the scripted mock
// LLM with a Hold response, SIGTERMed while the first model round is genuinely
// held open — the same in-flight seam TestTask_BarePrompt_SIGTERM_WritesSessionEnd
// uses, one carrier over.
//
// WHY THE ARMS CANNOT BE DECIDED BY SCHEDULING. The recorder's grace is
// internal/signalrec's defaultForwardGrace (250ms) and this face's grace is
// internal/signalrec's termOnceSignalUnwindWait (5s) — the same package owns
// both numbers, so the guard reads the second through the first's yardstick
// instead of duplicating it. The measured unwind is ~1.5s: an order of magnitude
// past the recorder's window, so which arm runs is a property of the code, never
// of which goroutine won a race.
//
// THE THREE ARMS, each a different way the fix can be wrong:
//
//  1. the exit status is the face's own "the turn failed" status — NOT
//     -1/143/137, which is a process that died OF the signal;
//  2. the session JSONL carries exactly ONE session_end with that status — the
//     closeout the re-raise used to take away;
//  3. the provenance record is still filed — the fix buys the graceful exit
//     with the CLAIM, never by costing the evidence (signalrec's rule: a claim
//     suppresses the re-raise only).

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/signalrec"
	"github.com/nick/gopi/internal/testutil"
)

// The argv this guard drives and the stream/ledger facts it asserts. Named
// (Rule 1b): a spelling carrying meaning is not typed inline.
const (
	// termOnceCLIVerb is the verb under test (`gopi cli … --once`).
	termOnceCLIVerb = "cli"
	// termOnceFlag is that face's single-turn switch.
	termOnceFlag = "--once"
	// termOncePrompt is a prompt long enough to be substantive (the carrier
	// warns about short answers being re-issued) and stable across runs.
	termOncePrompt = "start a long analysis and report back"
	// termOnceAbortNotice is the sentence the carrier prints when a signal
	// ends the turn — the same wording cmd/agent/cli_term_once.go declares
	// (termOnceSignalAbortReason).
	termOnceAbortNotice = "aborting the turn and writing the session closeout"
	// termOnceExitGraceful is the status a SIGTERM-interrupted one-shot turn
	// reports: the face's own "the turn failed" status, reached through its
	// closeout.
	termOnceExitGraceful = 1
	// termOnceSessionEndWanted is how many session_end events a single-session
	// run may leave: exactly one.
	termOnceSessionEndWanted = 1
	// termOnceSignalRecordName is the recorder's per-session provenance file.
	termOnceSignalRecordName = "signal-provenance.jsonl"
	// termOnceSessionsDir is the directory sessions live under in a GOPI_HOME.
	termOnceSessionsDir = "sessions"
	// termOnceGoHomeRel is the GOPI_HOME-relative root of the session tree.
	termOnceGoHomeRel = ".gopi"
)

// TestTermOnce_SIGTERM_WritesSessionEnd is the red→green guard for the epic: a
// SIGTERM arriving mid-turn must not kill the one-shot carrier, and the turn
// must still reach its closeout.
func TestTermOnce_SIGTERM_WritesSessionEnd(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Hold: true})
	defer llm.Close()

	env := testutil.MockEnv(t, home, llm.URL())
	proc := testutil.StartGopi(t, []string{termOnceCLIVerb, termOncePrompt, termOnceFlag}, "", env, work)
	defer proc.Stop() // idempotent — no-op once the process has exited

	// SIGTERM must land while the turn is genuinely running: the mock has
	// accepted the first model request and is holding the stream open. Any
	// earlier and the signal would arrive during setup (before the carrier's
	// handler exists) and would test nothing.
	proc.WaitFor("the first LLM request to be held mid-stream", func() bool {
		return llm.HeldCount() >= 1
	})

	proc.Signal(syscall.SIGTERM)
	code := proc.Wait()

	// Arm 1: -1 (Go's "terminated by a signal") or a shell's 143/137 means
	// nothing consumed the signal and the recorder's re-raise won.
	if code < 0 || code == 128+int(syscall.SIGTERM) || code == 128+int(syscall.SIGKILL) {
		t.Fatalf("gopi cli --once exit = %d after SIGTERM, want %d — the recorder re-raised the signal and killed the turn before its closeout (exit -1/143/137 is the default disposition)\n--- stderr ---\n%s",
			code, termOnceExitGraceful, proc.Stderr())
	}
	if code != termOnceExitGraceful {
		t.Fatalf("gopi cli --once exit = %d, want %d (a signal-interrupted turn reports the face's own failure status)\n--- stderr ---\n%s",
			code, termOnceExitGraceful, proc.Stderr())
	}

	// Arm 2: the closeout really ran inside the process — the session JSONL
	// ends with exactly one session_end carrying that status.
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	if ends := sess.Counts()[sessionevent.EventTypeSessionEnd]; ends != termOnceSessionEndWanted {
		t.Errorf("session_end count = %d, want exactly %d — the SIGTERM must not leave the one-shot session open (events: %v)",
			ends, termOnceSessionEndWanted, sess.Types())
	}
	if got := sess.SessionEndExitCode(); got != termOnceExitGraceful {
		t.Errorf("session_end exit_code = %d, want %d", got, termOnceExitGraceful)
	}

	// The operator sees why the turn stopped, on stderr, instead of inferring
	// it from an exit status.
	if !strings.Contains(proc.Stderr(), termOnceAbortNotice) {
		t.Errorf("stderr = %q, want the abort notice %q", proc.Stderr(), termOnceAbortNotice)
	}

	// Arm 3: the provenance record survives. The claim suppresses the RE-RAISE
	// only; a fix that bought the graceful exit by costing the evidence would
	// undo the epic that put the recorder there.
	if got := termOnceSignalRecords(t, home); len(got) == 0 {
		t.Errorf("no %s under %s — the ownership claim must silence the re-raise, never the provenance record",
			termOnceSignalRecordName, filepath.Join(home, termOnceGoHomeRel, termOnceSessionsDir))
	} else if !strings.Contains(got[0], signalrec.SignalSIGTERM) {
		t.Errorf("provenance record = %q, want it to name %s", got[0], signalrec.SignalSIGTERM)
	}
}

// termOnceSignalRecords returns the content of every per-session provenance
// record under a GOPI_HOME. The recorder is what files them; this guard reads
// them the way an operator would.
func termOnceSignalRecords(t *testing.T, home string) []string {
	t.Helper()
	pattern := filepath.Join(home, termOnceGoHomeRel, termOnceSessionsDir, "*", termOnceSignalRecordName)
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	var out []string
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		out = append(out, string(b))
	}
	return out
}
