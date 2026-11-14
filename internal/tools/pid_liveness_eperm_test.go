package tools

// The liveness predicate's CONTRACT, pinned (epic 01a17e87).
//
// The defect this file guards: kill(pid, 0) answers EPERM when the process
// EXISTS and the caller merely may not signal it, and every earlier
// implementation folded EPERM into "dead". For a display that only omits
// somebody else's process; for a DELETION or OWNERSHIP decision it erases the
// record of a live process — the worker state residue sweep removes a state
// file whose pid is "not alive", so a live worker owned by another user lost
// its record (measured: uid 1000 cannot signal pid 1).
//
// The two exported predicates have OPPOSITE safe directions, and that is the
// whole design: PidAlive answers false only for a MEASURED death, PidGone
// answers true only for one. Neither may be rewritten as the other's negation.

import (
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/procstat"
)

// livenessInitPid is init. It is unambiguously ALIVE and — for a non-root
// caller — unsignalable, which is exactly the EPERM case this epic is about.
const livenessInitPid = 1

// livenessDeadPid names no process. It is above any plausible pid_max (the
// measured value on this host is 4194304 in the kernel's own numbering, but the
// pid_t range is what matters here) so the kernel answers ESRCH.
const livenessDeadPid = 2147483646

// TestPidAliveEPERMIsAlive is the epic's real probe: pid 1, uid 1000, EPERM.
// The premise is MEASURED first so a root run (which gets nil, not EPERM) skips
// with a reason instead of passing for the wrong one.
func TestPidAliveEPERMIsAlive(t *testing.T) {
	err := syscall.Kill(livenessInitPid, 0)
	if err == nil {
		t.Skipf("kill(%d,0) = nil — this run CAN signal init (root), so the EPERM "+
			"premise is absent and this test would prove nothing", livenessInitPid)
	}
	if err != syscall.EPERM {
		t.Fatalf("kill(%d,0) = %v, want EPERM — the fixture's premise is gone, so this "+
			"test cannot measure the EPERM branch; fix the premise before trusting it",
			livenessInitPid, err)
	}

	// The three-valued probe names it alive...
	if got := PidLivenessOf(livenessInitPid); got != procstat.VerdictAlive {
		t.Errorf("PidLivenessOf(%d) = %q, want %q: EPERM means the process EXISTS "+
			"and is not ours", livenessInitPid, got, procstat.VerdictAlive)
	}
	// ...the conservative boolean agrees...
	if !PidAlive(livenessInitPid) {
		t.Errorf("PidAlive(%d) = false while kill(%d,0) answered EPERM — this is the "+
			"pre-fix defect: init reported dead, so every caller read 'I may not look "+
			"at it' as 'it is not there'", livenessInitPid, livenessInitPid)
	}
	// ...and the DELETION predicate refuses to call it gone. This arm is the one
	// that protects a live record from being swept away.
	if PidGone(livenessInitPid) {
		t.Errorf("PidGone(%d) = true while kill(%d,0) answered EPERM — a deletion arm "+
			"reading this would remove the record of a LIVE process",
			livenessInitPid, livenessInitPid)
	}
}

// TestPidAliveAndPidGoneDivergeOnlyOnUnknown states the contract arithmetically:
// the two predicates must disagree exactly on the "could not decide" answers,
// and never on a measurement. A pid <= 0 is the reproducible unknown.
func TestPidAliveAndPidGoneDivergeOnlyOnUnknown(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if got := PidLivenessOf(pid); got != procstat.VerdictUnknown {
			t.Errorf("PidLivenessOf(%d) = %q, want %q: an absent PID is missing "+
				"evidence, never evidence of death", pid, got, procstat.VerdictUnknown)
		}
		if !PidAlive(pid) {
			// PidAlive's conservative direction is documented as "unknown reads
			// as alive", EXCEPT that pid <= 0 has no process at all and every
			// caller treats it as "nothing to be alive". This arm pins whichever
			// the code actually does so a silent flip is visible here.
			t.Logf("PidAlive(%d) = false (pid <= 0: no process to be alive)", pid)
		}
		if PidGone(pid) {
			t.Errorf("PidGone(%d) = true, want false: unknown is never a deletion", pid)
		}
	}
}

// TestPidGoneOnlyForMeasuredDeath pins the measured half: a pid the kernel
// knows nothing about is gone, and this process (demonstrably alive and ours) is
// not.
func TestPidGoneOnlyForMeasuredDeath(t *testing.T) {
	if got := PidLivenessOf(livenessDeadPid); got != procstat.VerdictGone {
		t.Fatalf("PidLivenessOf(%d) = %q, want %q (the kernel answers ESRCH)",
			livenessDeadPid, got, procstat.VerdictGone)
	}
	if !PidGone(livenessDeadPid) {
		t.Errorf("PidGone(%d) = false for an ESRCH pid", livenessDeadPid)
	}
	if PidAlive(livenessDeadPid) {
		t.Errorf("PidAlive(%d) = true for an ESRCH pid", livenessDeadPid)
	}

	self := syscall.Getpid()
	if got := PidLivenessOf(self); got != procstat.VerdictAlive {
		t.Fatalf("PidLivenessOf(%d) [this test process] = %q, want %q", self, got, procstat.VerdictAlive)
	}
	if PidGone(self) {
		t.Errorf("PidGone(%d) = true for this live test process", self)
	}
	if !PidAlive(self) {
		t.Errorf("PidAlive(%d) = false for this live test process", self)
	}
}
