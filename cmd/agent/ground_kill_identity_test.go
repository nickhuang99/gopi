package main

// ground_kill_identity_test.go — the regression guard for the SIGINT that
// reached `systemd --user` and stopped the operator's GNOME session, five times
// on 2026-10-17 (Rule 41 evidence: `journalctl -b | grep 'Received SIG'`).
//
// THE DEFECT, EXACTLY. `groundKill` (task_ground_e2e_test.go) holds its target's
// pid across a `t.Cleanup` — by teardown the walk's executor child is usually
// long gone — and the pre-fix body began with
//
//	proc, err := os.FindProcess(pid)   // on unix this ALWAYS succeeds
//	_ = proc.Signal(os.Interrupt)      // → SIGINT to whoever owns that NUMBER now
//
// A pid is a number the kernel RECYCLES. `os.FindProcess` is not a lookup (Go's
// own doc: "on Unix systems … it is not a valid test for the existence of the
// process"), so the very first act of the teardown was a SIGINT addressed to a
// number with no claim on the process behind it. Repeatedly that number had been
// handed to `systemd --user` — and SIGINT to the user manager stops the whole
// user session, which is what took the desktop down.
//
// WHAT THIS TEST MEASURES, AND WHY BOTH HALVES ARE NEEDED. A guard that merely
// "returns an error" proves nothing about the defect unless the unguarded shape
// is shown DELIVERING on the same live pid. So the walk drives both shapes
// against ONE live fixture process:
//
//	① GUARDED (shipped): a record whose incarnation is not the live process is
//	   REFUSED, and the live process is still there afterwards (measured through
//	   /proc, never inferred from the error).
//	② UNGUARDED (pre-fix): a bare `syscall.Kill(pid, SIGTERM)` — the pid-only
//	   shape the five journalctl lines came from — DELIVERS to that same pid.
//	   Without this arm the guard could pass while protecting against nothing.
//
// Neither arm can reach a stranger: both are aimed at a fixture this test
// spawned, and its own group is reaped by StartMockProcess.

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// groundKillStaleTickDelta is what makes a record STALE: the same pid number
// with a different starttime is, by the kernel's own definition, a different
// incarnation (procname's VerdictRecycled reasoning). One tick is enough — the
// comparison is exact equality, and a bigger number would only make the test
// look like it depended on a magnitude.
const groundKillStaleTickDelta = 1

// groundKillExitPollStep / groundKillExitPollBound bound the wait for a
// signalled fixture to leave the process table. The wait is bounded and its
// failure is REPORTED; the assertion is on the measurement, not on the sleep.
const (
	groundKillExitPollStep  = 20 * time.Millisecond
	groundKillExitPollBound = 5 * time.Second
)

// TestGroundKillSignalsOnlyTheRecordedIncarnation is ① followed by ②.
func TestGroundKillSignalsOnlyTheRecordedIncarnation(t *testing.T) {
	mp := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	pid := mp.Pid()
	// CAPTURE, not a re-check: the fixture was spawned a few microseconds ago, so
	// a bare read can land inside the ARGV PUBLICATION WINDOW and store an empty
	// cmdline while the child is already running (measured 194/200 spawns, see
	// ArgvUnpublished). Arm ①'s later id.Alive() is a RE-CHECK and compares that
	// stored cmdline against the now-published one — the two halves would carry
	// the SAME starttime and differ only in cmdline, which Alive reports as
	// "pid N was REUSED" and which used to fail this test on ~3/8 runs. Only the
	// capture may wait; Alive/KillIfIdentical still observe NOW.
	id, err := testutil.AwaitProcIdentity(pid)
	if err != nil {
		t.Fatalf("read the fixture's identity (pid %d): %v", pid, err)
	}

	// The STALE record: the same number, an incarnation that is not the live
	// process — i.e. exactly the situation at teardown time, when the pid the
	// caller is holding has been recycled to somebody else.
	stale := id
	stale.StartTime = id.StartTime + groundKillStaleTickDelta

	// ① GUARDED. The shipped delivery path must refuse, and the live process
	// must be measurably untouched — the error alone is not the claim.
	if err := groundKillSignal(stale, syscall.SIGTERM); err == nil {
		t.Fatalf("the identity guard delivered to a pid whose incarnation is not the recorded one: pid %d now started at %d, the record says %d",
			pid, id.StartTime, stale.StartTime)
	}
	if alive, why := id.Alive(); !alive {
		t.Fatalf("the guarded signal reached the live pid anyway (pid %d, %s) — the refusal is not a refusal", pid, why)
	}

	// ② UNGUARDED — the pre-fix shape, on the SAME live pid. A bare number
	// delivers, which is the defect this test exists to keep out.
	//
	// THIS SITE ONLY WORKS BECAUSE IT IS UNGUARDED, and that is the point: it is
	// the positive control for arm ①. `id` above IS this process's own fixture
	// incarnation (read with testutil.ReadProcIdentity a few lines up, and the
	// arm below asserts the pid is still alive by it), so the ownership and
	// incarnation facts are asserted HERE — routing the delivery through
	// testutil.KillIfIdentical would make arm ① unfalsifiable by removing the
	// only shape that proves the guard is doing something.
	// signal-identity-ok: `id` is this fixture's own recorded incarnation and the line below proves the bare form still delivers — that is arm ②'s subject
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		t.Fatalf("the pid-only shape could not deliver to the live fixture (pid %d): %v — the guard has nothing to protect against and this walk proves nothing", pid, err)
	}
	if alive, why := groundKillWaitGone(t, id); alive {
		t.Errorf("the pid-only shape did not deliver (pid %d still alive: %s) — the two shapes are indistinguishable here, so arm ① is vacuous", pid, why)
	}
}

// TestGroundKillGoesThroughTheIdentityPath is the SECOND half of the fix: the
// teardown must (a) DELIVER through the identity-checked path when the process
// is there, and (b) not reach that path at all when it is not.
//
// The two arms are the same measurement in opposite directions, and NEITHER is
// decoration: without ② a pre-fix body that signals a raw pid would pass this
// test vacuously (it never calls the recorded path, so the counter reads zero),
// and without ① the zero would be indistinguishable from "groundKill is dead
// code". A guard is only a guard if the thing it guards is the thing that runs.
func TestGroundKillGoesThroughTheIdentityPath(t *testing.T) {
	calls := 0
	saved := groundKillSignal
	groundKillSignal = func(id testutil.ProcIdentity, sig syscall.Signal) error {
		calls++
		return saved(id, sig)
	}
	t.Cleanup(func() { groundKillSignal = saved })

	// ① A LIVE pid: the delivery path runs, and the process is measurably gone
	// afterwards (a reading of /proc, not a belief about the counter).
	live := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	// CAPTURE through the waiting entry point, for the same reason as arm ① of
	// the walk above: this id is later RE-CHECKED with Alive(), and an empty
	// captured cmdline would read as a reuse of the pid it belongs to.
	liveID, err := testutil.AwaitProcIdentity(live.Pid())
	if err != nil {
		t.Fatalf("read the live fixture's identity (pid %d): %v", live.Pid(), err)
	}
	groundKill(t, live.Pid())
	if calls == 0 {
		t.Fatalf("groundKill stopped pid %d without once going through the identity-checked delivery path — a teardown that signals a bare pid number is exactly what sent SIGINT to `systemd --user` and took the operator's session down", live.Pid())
	}
	if alive, why := groundKillWaitGone(t, liveID); alive {
		t.Errorf("the live fixture survived its teardown (pid %d: %s)", live.Pid(), why)
	}

	// ② An EXITED pid: the same teardown must return WITHOUT delivering. The
	// counter is reset so this arm's zero cannot be arm ①'s count.
	calls = 0
	exited := exec.Command("/bin/true")
	if err := exited.Start(); err != nil {
		t.Skipf("cannot start /bin/true: %v", err)
	}
	gonePid := exited.Process.Pid
	_ = exited.Wait() //stylegate:ignore the Wait exists to make the pid EXITED, not to judge its status: /bin/true's exit code is not this arm's subject, and the arm asserts on the delivery counter below
	groundKill(t, gonePid)
	if calls != 0 {
		t.Errorf("groundKill reached the delivery path %d time(s) for pid %d, whose process had already exited — an exited pid is the state the caller wants, never a signal to send",
			calls, gonePid)
	}
}

// groundKillWaitGone polls until the recorded incarnation has left the process
// table, returning its last reading so the caller can print WHY it was still
// considered alive.
func groundKillWaitGone(t *testing.T, id testutil.ProcIdentity) (bool, string) {
	t.Helper()
	deadline := time.Now().Add(groundKillExitPollBound)
	var why string
	for {
		alive, reason := id.Alive()
		if !alive {
			return false, reason
		}
		why = reason
		if !time.Now().Before(deadline) {
			return true, why
		}
		time.Sleep(groundKillExitPollStep) //stylegate:allow-sleep bounded wait for a just-signalled fixture to leave the process table
	}
}
