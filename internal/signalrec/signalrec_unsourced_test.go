package signalrec

// signalrec_unsourced_test.go — the regression guard for the UNSOURCED-KILL
// classification (epic signal-provenance-unsourced, 2026-09-17).
//
// THE DEFECT THIS PINS. Before this guard, two deaths that are NOT the same
// fact produced the same trail. An operator's `gopi worker --stop` and an
// unannounced `kill <pid>` from a stranger both:
//
//	(a) wrote a KindSignal record with NO statement about who sent it, and
//	(b) printed the word "draining" on the operator's console,
//
// so a reader could not tell the two apart. Measured on peer1: at
// 2026-09-16T22:23:13-14, EIGHT gopi processes (three worker daemons, their
// three run children, the merger daemon and the web server) took SIGTERM inside
// one second while the tree held ZERO kind=intent records — a mass kill nobody
// announced, which the log spelled exactly like a graceful drain.
//
// THE TWO ARMS, and why BOTH are required. Asserting only the unsourced arm
// would be blind to the opposite failure: a guard that labels EVERY kill
// unsourced is worse than none, because it converts every honest operator stop
// into an alarming event. The sourced arm is the anti-over-firing proof.
//
//   - arm A (UNSOURCED): a victim with no intent filed against it records
//     sourced=false and the session log says so.
//   - arm B (SOURCED): the killer files its intent into the VICTIM's own dir
//     before signalling — the protocol this package implements — and the victim
//     records sourced=true carrying the intent as evidence.
//
// Both arms run the SAME victim binary and differ ONLY in whether the intent was
// filed, so neither can pass by accident of a different code path.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// srUnsourcedVictimEnv is the child hook that runs in TestMain: install the
// recorder, announce readiness, then block until signalled. It is a SEPARATE env
// from the pre-existing victim fixture (srChildEnv) so neither arm of this file
// can disturb the suite the other one guards; the victim is otherwise the same
// process shape, because the classification under test runs in the recorder.
const srUnsourcedVictimEnv = "GOPI_SIGNALREC_TEST_UNSOURCED_VICTIM"

// srSignalVictimAndReadVerdict runs one arm: it starts the victim, signals it,
// waits for the victim's own verdict line, and returns it together with the
// record the victim filed.
//
// fileIntent is the ARM SELECTOR: when non-nil it is called with the victim's
// pid and must file the intent into the victim's dir (arm B); nil means no
// intent is ever filed (arm A). Everything else — the binary, the environment,
// the signal, the assertions' subjects — is identical.
func srSignalVictimAndReadVerdict(t *testing.T, home, dir string, fileIntent func(victimPID int)) (KillVerdict, record) {
	t.Helper()
	ready := srAnticipateChildReady(t, home)
	cmd := exec.Command(os.Args[0])
	cmd.Env = srChildEnvFor(os.Environ(), home, map[string]string{
		srUnsourcedVictimEnv: "1",
		SessionDirEnv:        dir,
		srReadyEnv:           ready[srReadyEnv],
	})
	cmd.Stderr = os.Stderr
	// Bind the victim to this test binary's lifetime before it starts:
	// the child must be armed with the death pipe this process holds, and
	// the guard (run after the readiness handshake) must find its verdict.
	requireFixtureBinding := srBindVictim(t, cmd, "srSignalVictimAndReadVerdict")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the victim: %v", err)
	}
	readyFile := srAwaitChildReady(t, ready)
	requireFixtureBinding()
	defer readyFile.Close()

	if fileIntent != nil {
		fileIntent(cmd.Process.Pid)
	}
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("signal the victim: %v", err)
	}
	// POLL FIRST, KILL AFTER. The handler writes asynchronously, and the victim
	// catches SIGTERM (the recorder is installed), so it does not exit on its
	// own — killing it in the same breath as the signal would reap it mid-write
	// and the arm would assert on an empty file. wantDeath is the victim's own
	// record on top of whatever the arm filed before the signal.
	want := 1
	if fileIntent != nil {
		want = 2 // the filed intent, then the victim's death record
	}
	recs := srWaitForRecord(t, dir, want)
	_ = cmd.Process.Kill() //stylegate:ignore teardown of the fixture child after the record was polled; the kill is not the assertion
	_ = cmd.Wait()         //stylegate:ignore reaping the child just killed; its wait status is not asserted
	if len(recs) < want {
		t.Fatalf("the victim left %d record(s) in %s, want %d — the death record is missing (the signal arrived before the handler wrote, or the handler was not installed)", len(recs), dir, want)
	}
	// The LAST record is the victim's own death record (an arm-B intent may
	// precede it in the same dir — that is the point of arm B).
	r := recs[len(recs)-1]
	if r.Kind != KindSignal {
		t.Fatalf("last record kind = %q, want the victim's death record %q", r.Kind, KindSignal)
	}
	v := KillVerdict{Received: true, Signal: r.Signal, IntentEvidence: r.IntentEvidence}
	if r.Sourced != nil {
		v.Classified = true
		v.Sourced = *r.Sourced
	}
	return v, r
}

// srResetVerdict clears the recorder's published verdict so a test can assert
// on a known starting state. In-package only: os.Args and the published verdict
// are not seams worth exporting, and a test that asserts "no signal arrived"
// must not read a sibling test's delivery.
func srResetVerdict(t *testing.T) {
	t.Helper()
	latestVerdict.mu.Lock()
	latestVerdict.v = KillVerdict{}
	latestVerdict.mu.Unlock()
	t.Cleanup(srResetVerdictLocked)
}

// srResetVerdictLocked is the cleanup half of srResetVerdict.
func srResetVerdictLocked() {
	latestVerdict.mu.Lock()
	latestVerdict.v = KillVerdict{}
	latestVerdict.mu.Unlock()
}

// TestSignalRecUnsourcedKillIsClassified is ARM A: a SIGTERM nobody announced
// must be recorded as UNSOURCED — the machine-readable verdict a reader (an
// operator's jq filter, the ledger event, a test) keys on.
func TestSignalRecUnsourcedKillIsClassified(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "unsourced-session")

	v, r := srSignalVictimAndReadVerdict(t, home, dir, nil)

	if !v.Classified {
		t.Fatalf("the kill was NOT classified at all (record %+v) — an unannounced SIGTERM must carry a verdict", r)
	}
	if v.Sourced {
		t.Fatalf("an announced-by-nobody SIGTERM was recorded as SOURCED (record %+v) — this is the collapse the guard exists to end", r)
	}
	if r.Sourced == nil || *r.Sourced {
		t.Fatalf("record's %s field = %v, want false", SourcedField, r.Sourced)
	}
	if r.Signal != "SIGTERM" {
		t.Errorf("signal = %q, want SIGTERM", r.Signal)
	}
	// The verdict must be readable as the SAME word the session log prints, from
	// the JSON alone: the field name and the vocabulary are shared constants.
	raw, err := os.ReadFile(srProvenancePath(dir))
	if err != nil {
		t.Fatalf("read the provenance file: %v", err)
	}
	if !strings.Contains(string(raw), `"`+SourcedField+`":false`) {
		t.Errorf("the provenance file does not spell %q:false — an operator's grep cannot find the verdict:\n%s", SourcedField, raw)
	}
}

// TestSignalRecAnnouncedKillIsClassifiedSourced is ARM B, the anti-over-firing
// proof: when a gopi stop path files its intent against the victim BEFORE
// signalling — Intent's mirror into the target's dir — the same SIGTERM must be
// recorded as SOURCED, carrying that intent as evidence.
func TestSignalRecAnnouncedKillIsClassifiedSourced(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "sourced-session")

	const wantReason = "operator ran gopi worker --stop"
	v, r := srSignalVictimAndReadVerdict(t, home, dir, func(victimPID int) {
		// The REAL protocol: Intent() writes the sender's own record AND mirrors
		// it into the target's dir, which is the record the victim reads. The
		// target's dir here IS this session dir (childEnv handed it over), so the
		// mirror lands exactly where the victim looks.
		if err := writeIntentToTarget(record{
			TS:        nowStamp(),
			Kind:      KindIntent,
			Role:      "cli",
			PID:       os.Getpid(),
			TargetPID: victimPID,
			Reason:    wantReason,
		}, victimPID); err != nil {
			t.Fatalf("file the intent against %d: %v", victimPID, err)
		}
	})

	if !v.Classified {
		t.Fatalf("a kill WITH a filed intent was not classified (record %+v)", r)
	}
	if !v.Sourced {
		t.Fatalf("a kill whose intent was filed BEFORE the signal was recorded as UNSOURCED (record %+v) — the guard over-fires and would alarm on every honest operator stop", r)
	}
	if !strings.Contains(r.IntentEvidence, wantReason) {
		t.Errorf("intent_evidence = %q, want it to carry the reason %q — the claim must travel with its evidence", r.IntentEvidence, wantReason)
	}
	if r.Sourced == nil || !*r.Sourced {
		t.Fatalf("record's %s field = %v, want true", SourcedField, r.Sourced)
	}
}

// TestSignalRecUnreadableTrailLeavesTheKillUnclassified is the THIRD state's
// guard: when the question cannot be answered from evidence, the record must say
// nothing rather than accuse an operator. It is the difference between "nobody
// announced this" and "we could not find out".
//
// It is an IN-PROCESS test of the classifier's own branch, deliberately: the
// end-to-end shape (a victim whose provenance path cannot be read) is
// self-defeating, because the same fault stops the victim writing its death
// record at all. The branch is what needs pinning, and driving it directly is
// the honest instrument — the two END-TO-END arms live above.
func TestSignalRecUnreadableTrailLeavesTheKillUnclassified(t *testing.T) {
	home := srHome(t)
	dir := srSessionDir(t, home, "unreadable-session")
	// A DIRECTORY at the provenance path: os.ReadFile fails with EISDIR, which
	// is not os.ErrNotExist — the branch under test.
	if err := os.Mkdir(filepath.Join(dir, FileName), 0o755); err != nil {
		t.Fatalf("create the unreadable provenance path: %v", err)
	}
	SetSessionDir(dir, "", "")
	t.Cleanup(func() { SetSessionDir("", "", "") })

	r := record{TS: nowStamp(), Kind: KindSignal, Signal: "SIGTERM", PID: os.Getpid()}
	classifySourced(&r)
	if r.Sourced != nil {
		t.Fatalf("an unreadable trail produced a VERDICT (%s=%t) — a guess here blames an operator for evidence we never read", SourcedField, *r.Sourced)
	}
}

// TestSignalRecNoSessionDirLeavesTheKillUnclassified pins the other
// evidence-free case: a process that was handed no session dir has nowhere an
// intent could have been filed, so the absence of one is not evidence and the
// record must carry no verdict.
func TestSignalRecNoSessionDirLeavesTheKillUnclassified(t *testing.T) {
	home := srHome(t)
	_ = home
	SetSessionDir("", "", "")
	r := record{TS: nowStamp(), Kind: KindSignal, Signal: "SIGTERM", PID: os.Getpid()}
	classifySourced(&r)
	if r.Sourced != nil {
		t.Fatalf("a victim with no session dir produced a VERDICT (%s=%t)", SourcedField, *r.Sourced)
	}
}

// TestSignalRecVerdictWaitIsBoundedButSufficient pins the two halves of the
// reader the stop surfaces use: it returns the zero value IMMEDIATELY when no
// signal arrived (an ordinary exit must never pay a budget), and it returns a
// verdict once one has been classified.
func TestSignalRecVerdictWaitIsBoundedButSufficient(t *testing.T) {
	// Earlier in-process tests in this package deliver REAL signals to this same
	// binary, so the recorder's published state must be reset before the
	// no-signal arm is asserted (otherwise the assertion reads their leftover).
	srResetVerdict(t)

	// No signal in this process: the wait must not block.
	start := time.Now()
	if v := AwaitKillVerdict(DefaultVerdictWait); v.Received {
		t.Fatalf("AwaitKillVerdict reported a signal for a process that received none: %+v", v)
	}
	if elapsed := time.Since(start); elapsed > verdictPollInterval*4 {
		t.Errorf("AwaitKillVerdict blocked %s with no signal to wait for — an ordinary exit must not pay the budget", elapsed)
	}

	// A received-but-unclassified signal: the wait must exhaust its budget and
	// then return what it has (Received true, Classified false) rather than
	// hanging.
	noteSignalReceived("SIGTERM")
	start = time.Now()
	v := AwaitKillVerdict(20 * time.Millisecond)
	elapsed := time.Since(start)
	if !v.Received || v.Signal != "SIGTERM" {
		t.Fatalf("AwaitKillVerdict lost the received signal: %+v", v)
	}
	if v.Classified {
		t.Fatalf("AwaitKillVerdict invented a verdict for an unclassified signal: %+v", v)
	}
	if elapsed < 20*time.Millisecond || elapsed > 2*time.Second {
		t.Errorf("AwaitKillVerdict waited %s for a %s budget — unbounded or skipped", elapsed, 20*time.Millisecond)
	}
}
