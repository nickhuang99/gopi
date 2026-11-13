package main

// worker_daemon_stop_esrch_test.go — the ESRCH arm of the sandbox stop helper.
//
// THE DEFECT (epic worker-daemon-stop-esrch): wdStopPID probed the daemon with
// signal 0, then sent SIGTERM, and reported EVERY error from that send with
// t.Errorf. ESRCH is the one errno that means "the target is already gone" —
// precisely the end state the helper exists to reach — so a daemon that exited
// on its own in the window between the probe and the send (an idle budget
// expiring, a `--once` drain finishing, another stop request landing first)
// turned a per-case t.Cleanup net into an unexplainable red. Any OTHER errno
// (EPERM: the pid is not ours; EINVAL: a signal this kernel refuses) is a real
// failure and must still be reported (Rule 15) — this file pins BOTH halves, so
// the fix cannot be widened into "ignore the error".
//
// Rule 42: nothing here signals a process it does not own. The ESRCH arm is
// pid-ADDRESSED at a pid the kernel has no process for, and the refusal arm
// never sends a signal at all — it replaces the helper's signal seam with a
// function that refuses, so the errno comes from code THIS test owns.
//
// Rule 27: no device load and no load generation — a spent pid plus an injected
// error, nothing else.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// wseRefusedPid is the pid the refusal arm reports: THIS process. It is the one
// pid this test is certain is alive (the helper returns before the send for a
// dead one, which would silently skip the branch under test), and no real signal
// is ever sent to it — the injected kill refuses before the syscall.
func wseRefusedPid() int { return os.Getpid() }

// wseSpentPID returns a pid that addresses NO live process. It forks a child
// that exits at once and REAPS it through exec.Cmd.Wait, so the number is spent
// by this test itself — never a pid that was merely "not observed alive", which
// a signal could still reach.
func wseSpentPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("fixture child %q: %v", "true", err)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatalf("fixture child did not report an exited state: %+v", cmd.ProcessState)
	}
	pid := cmd.ProcessState.Pid()
	if pid <= 0 {
		t.Fatalf("fixture child reported pid %d", pid)
	}
	return pid
}

// TestWorkerStopPIDTreatsESRCHAsStopped is the regression arm for the defect: a
// signal to a pid the kernel has no process for returns ESRCH, and the helper
// must treat that as SUCCESS — the daemon IS stopped (it is gone), which is the
// property the whole stop path exists to establish.
func TestWorkerStopPIDTreatsESRCHAsStopped(t *testing.T) {
	pid := wseSpentPID(t)
	// The fixture's own precondition, MEASURED rather than assumed: the kernel
	// really does answer ESRCH for this pid. Without it the case below would pass
	// on a host where the number had been recycled.
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("fixture pid %d is alive — the ESRCH arm cannot be exercised", pid)
	} else if !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("fixture pid %d answered %v, want ESRCH", pid, err)
	}

	sub := &testing.T{}
	wdStopPID(sub, pid)
	if sub.Failed() {
		t.Errorf("wdStopPID reported pid %d as a failure although it is already gone: ESRCH means STOPPED — "+
			"a daemon that exited on its own is not a defective stop", pid)
	}
}

// TestWorkerStopPIDStillReportsANonESRCHError is the other half, and it is why
// the fix may not be "swallow the error": a refusal (EPERM — the shape "this pid
// is not yours") must still reach the operator. The refusal is injected through
// a seam THIS test owns, so no foreign process is signalled (Rule 42).
func TestWorkerStopPIDStillReportsANonESRCHError(t *testing.T) {
	orig := wdStopSignalFn
	t.Cleanup(func() { wdStopSignalFn = orig })
	wdStopSignalFn = func(pid int, sig syscall.Signal) error {
		return fmt.Errorf("injected refusal for pid %d sig %v: %w", pid, sig, syscall.EPERM)
	}

	sub := &testing.T{}
	wdStopPID(sub, wseRefusedPid())
	if !sub.Failed() {
		t.Fatalf("wdStopPID accepted an injected %v from the signal send — a non-ESRCH errno is a real failure "+
			"and must be reported (Rule 15); the ESRCH fix was widened into ignoring the error", syscall.EPERM)
	}
}

// TestWdStopSignalFnDefaultsToKill keeps the seam honest: production must point
// at syscall.Kill, or the two arms above would measure a test-only path and the
// real send would stay unchecked.
func TestWdStopSignalFnDefaultsToKill(t *testing.T) {
	if wdStopSignalFn == nil {
		t.Fatalf("wdStopSignalFn is nil — the stop path would not compile a send at all")
	}
	if wdStopSignalFn(os.Getpid(), 0) != nil {
		t.Fatalf("the default seam cannot probe this very process with signal 0 — it is not syscall.Kill")
	}
}
