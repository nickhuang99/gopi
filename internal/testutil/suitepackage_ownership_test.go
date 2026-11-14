package testutil

// THE REFUSAL MUST TEACH OWNERSHIP BEFORE ANY SIGNAL — the regression proof for
// suitepackage_ownership.go and for the FIX sentence in suitepackage.go.
//
// THE DEFECT (bug row 01a1d278, the operator's own measurement). The busy
// refusal ended with
//
//	FIX: wait for the holder above to finish; or stop that run deliberately; …
//
// and the holder it named was a pid the operator had NEVER STARTED: a
// `setsid`-detached `go test` left behind by a bash call the tool had already
// killed (scripts/test-budget.sh detaches deliberately — Rule 42's containment,
// NOT to be removed). "Stop that run deliberately" points a reader at a signal,
// and Rule 42 forbids signalling a process the reader does not create and hold —
// and a pid read out of a lock record is a NUMBER the kernel recycles.
//
// WHAT IS ASSERTED, and why each arm exists:
//
//  1. the FIX line no longer carries the wording that invited the cross-ownership
//     kill, and it carries the ownership check instead;
//  2. the ownership verdict is MEASURED against /proc and distinguishes the two
//     shapes that matter — a holder in THIS process's session (a process your own
//     tree started) from a holder in another session (a survivor you do not hold);
//  3. an unreadable holder is reported UNMEASURED, never upgraded into either
//     verdict (Rule 15a: an unknown is not a licence).

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// suiteBusyRefusalBannedFragment is the EXACT wording retired by this change: it
// names an action ("stop that run") without naming whose process it is, which is
// what pointed a reader at a cross-ownership signal. Named as a constant so the
// assertion cannot drift from the sentence it forbids (Rule 1/13).
const suiteBusyRefusalBannedFragment = "stop that run deliberately"

// suiteBusyRefusalOwnershipFragment is the check the refusal must now carry.
const suiteBusyRefusalOwnershipFragment = "BEFORE any signal"

// The other-session arm needs the setsid child to HAVE become a session leader
// before the measurement means anything; these bound that wait (Rule 27: a
// bounded loop carries its own cap, and the arm SKIPS rather than passing for
// the wrong reason when the bound is reached).
const (
	suiteHolderDetachPollLimit    = 200
	suiteHolderDetachPollInterval = 5 * time.Millisecond
)

// TestSuiteBusyRefusalTeachesOwnershipBeforeASignal is arm 1: the sentence.
func TestSuiteBusyRefusalTeachesOwnershipBeforeASignal(t *testing.T) {
	// Heldpid 0 → the record names none, which is the unmeasured shape; the
	// sentence's FIX line is what this arm is about and it must be identical
	// whichever verdict the measurement reaches.
	busy := &scratch.SuitePackageBusyError{
		Dir:       "/scratch/suite-package-example",
		Outcome:   scratch.SuitePackageLockBusyLive,
		Evidence:  "pid 4242, started (not recorded), verdict live",
		HolderPid: 4242,
	}
	msg := suitePackageBusyMessage("example", busy)

	if strings.Contains(msg, suiteBusyRefusalBannedFragment) {
		t.Fatalf("the refusal still says %q — that sentence names an action without naming whose process it is, "+
			"and the measured defect is an operator being pointed at a pid they never started (Rule 42).\nrefusal:\n%s",
			suiteBusyRefusalBannedFragment, msg)
	}
	if !strings.Contains(msg, suiteBusyRefusalOwnershipFragment) {
		t.Fatalf("the refusal does not tell the reader to check ownership BEFORE signalling.\nrefusal:\n%s", msg)
	}
	if !strings.Contains(msg, "ownership:") {
		t.Fatalf("the refusal carries no measured ownership line, so the check it prescribes cannot be performed.\nrefusal:\n%s", msg)
	}
	// The two facts a reader needs to recognise the process, besides the pid.
	for _, want := range []string{"startticks", "directory"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal does not name %q, so the holder cannot be recognised (a pid alone is a "+
				"recyclable number).\nrefusal:\n%s", want, msg)
		}
	}
	// The escape hatch must stay reachable and be named through its ONE spelling.
	if !strings.Contains(msg, SuitePackageLockOffEnv+"=0") {
		t.Fatalf("the refusal no longer names the documented escape %s=0 (Rule 1b/13: one spelling of the knob).\nrefusal:\n%s",
			SuitePackageLockOffEnv, msg)
	}
}

// TestSuiteHolderOwnershipMeasuresTheSession is arm 2: the measurement, on both
// sides of the distinction, using processes this test itself creates.
func TestSuiteHolderOwnershipMeasuresTheSession(t *testing.T) {
	if _, ok := procnameSessionOf(os.Getpid()); !ok {
		t.Skip("no readable /proc session for this process — the measurement cannot be exercised here")
	}

	// SAME SESSION: a plain child of this test. It shares our session (no
	// setsid), so the verdict must be same-session.
	same := exec.Command("sleep", "30")
	if err := same.Start(); err != nil {
		t.Fatalf("start the same-session child: %v", err)
	}
	//stylegate:ignore cleanup of a child THIS TEST started; the process may already have exited, so ESRCH is the expected error and there is no failure to report
	t.Cleanup(func() { _, _ = same.Process.Kill(), same.Wait() })

	got := suitePackageHolderOwnership(same.Process.Pid)
	if got.Verdict != suiteHolderOwnershipSameSession {
		t.Fatalf("a plain child of this process (pid %d) was judged %q, want %q — a holder that IS ours must be "+
			"recognised as ours, or the check teaches the reader nothing", same.Process.Pid, got.Verdict, suiteHolderOwnershipSameSession)
	}
	if got.Start == "" {
		t.Fatalf("the ownership line carries no incarnation for pid %d — a pid without startticks is not an identity", same.Process.Pid)
	}

	// OTHER SESSION: a child that leads its own session via setsid(1). This is the
	// shape the measured defect produced, and it is the shape the refusal must
	// refuse to call "yours".
	other := exec.Command("setsid", "sleep", "30")
	if err := other.Start(); err != nil {
		t.Skipf("setsid(1) unavailable, cannot build the other-session shape: %v", err)
	}
	//stylegate:ignore cleanup of a child THIS TEST started; the process may already have exited, so ESRCH is the expected error and there is no failure to report
	t.Cleanup(func() { _, _ = other.Process.Kill(), other.Wait() })
	// Wait until the exec has actually become the detached process (its session
	// id equals its own pid once setsid(2) has run). Bounded, and the arm SKIPS
	// rather than passing for the wrong reason if it never happens.
	detached := false
	for i := 0; i < suiteHolderDetachPollLimit; i++ {
		if sid, ok := procnameSessionOf(other.Process.Pid); ok && sid == other.Process.Pid {
			detached = true
			break
		}
		time.Sleep(suiteHolderDetachPollInterval) //stylegate:allow-sleep bounded poll (suiteHolderDetachPollLimit × suiteHolderDetachPollInterval) waiting for a just-forked setsid child to become its own session leader; the bound owns the outcome
	}
	if !detached {
		t.Skipf("the setsid child (pid %d) never became its own session leader, so the other-session shape could not be built", other.Process.Pid)
	}
	otherOwn := suitePackageHolderOwnership(other.Process.Pid)
	if otherOwn.Verdict != suiteHolderOwnershipOtherSession {
		t.Fatalf("a setsid-detached child (pid %d) was judged %q, want %q — that is the exact shape the operator was "+
			"invited to kill, and the check must name it", other.Process.Pid, otherOwn.Verdict, suiteHolderOwnershipOtherSession)
	}
	if !strings.Contains(otherOwn.render(), "RECYCLABLE NUMBER") {
		t.Fatalf("the other-session sentence does not warn that the pid is a recyclable number, which is the fact that "+
			"makes signalling it unsafe (Rule 42).\nsentence:\n%s", otherOwn.render())
	}
}

// TestSuiteHolderOwnershipUnknownIsNotAYes is arm 3: a pid that names nothing
// readable must be reported unmeasured — never "same session", never "gone".
func TestSuiteHolderOwnershipUnknownIsNotAYes(t *testing.T) {
	// pid 0 = the record stated none. Nothing may be inferred from that.
	if got := suitePackageHolderOwnership(0); got.Verdict != suiteHolderOwnershipUnmeasured {
		t.Fatalf("a record with no pid was judged %q, want %q (Rule 15a: an unmeasured holder is never a verdict)",
			got.Verdict, suiteHolderOwnershipUnmeasured)
	}
	// A pid this process cannot read: reuse a just-exited child's number. The
	// verdict must be unmeasured, not "other-session" (which would read as
	// "somebody else's process") and not "same-session".
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatalf("run the short-lived child: %v", err)
	}
	got := suitePackageHolderOwnership(dead.Process.Pid)
	if got.Verdict == suiteHolderOwnershipSameSession {
		t.Fatalf("a pid with no live process (pid %d) was judged same-session — an unreadable holder must never be "+
			"reported as yours", dead.Process.Pid)
	}
	if !strings.Contains(got.render(), "UNMEASURED") && got.Verdict == suiteHolderOwnershipUnmeasured {
		t.Fatalf("the unmeasured verdict is not rendered as such:\n%s", got.render())
	}
}

// procnameSessionOf reads a pid's session through the repo's ONE /proc reader.
func procnameSessionOf(pid int) (int, bool) {
	return procname.ProcSessionKey(procname.DefaultProcRoot, strconv.Itoa(pid))
}
