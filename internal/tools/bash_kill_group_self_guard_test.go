package tools

// bash_kill_group_self_guard_test.go — A GROUP SIGNAL MUST NEVER ADDRESS THE
// CALLER'S OWN GROUP (bug 01a163df, measured 2026-10-22).
//
// THE DEFECT. killChildTree's guard was `pgid == pid`: "the child leads the
// group it lives in". That answers whether the child OWNS the group; it does not
// answer whether the process DOING THE KILLING is inside it — and under `gopi
// chat` the caller is the operator's terminal. Measured in a live chat's process
// table: three `parecord` recorders spawned WITHOUT Setpgid shared the chat's own
// group (PGID 1134893 == the chat's pid), so "the group they live in" WAS the
// chat's group. A group signal there is a self-kill, and SIGKILL cannot be
// caught — no handler can soften it, so the guard is the only defence.
//
// THE FIX HAS TWO HALVES and this file pins the one that lives here:
//
//	producer — cmd/agent's voiceRecorderSysProcAttr: a recorder leads its own
//	           group, so the chat's group cannot contain one.
//	consumer — killChildTreeGroupIsTheCallers: a group signal is REFUSED when the
//	           target group is this process's own (os.Getpgrp()), however the
//	           child came to be in it.
//
// WHY THE GEOMETRY IS BUILT WITH A SEAM, stated plainly rather than hidden. The
// two facts that make a group signal self-inflicted cannot coexist in one live
// process: a group id names its leader's pid, so "a child leads the group I am
// in" is a contradiction, and a live fixture can only produce "the child leads a
// group that is NOT mine" — the safe case. The case that must be REFUSED is
// therefore built by lowering the reading of "my group" to the child's group
// (killChildTreeGroupOf), which is exactly the predicate production evaluates
// when a stray process really does share the caller's group. The signal itself is
// recorded through killChildTreeGroupSignal, because BOTH branches end with the
// child dead — the outcome cannot tell "refused the group" from "signalled the
// group", and the refusal is the whole subject.

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

const (
	// killGuardFixtureSleepSec keeps the fixture alive for the case's duration:
	// no case may pass because the process ended on its own.
	killGuardFixtureSleepSec = 300
	// killGuardFixtureWait bounds the wait for the fixture to publish its pid.
	killGuardFixtureWait = 5 * time.Second
)

// killGuardFixture is a process that LEADS ITS OWN GROUP (Setpgid), i.e. the
// shape bash() produces for every command it runs.
type killGuardFixture struct {
	cmd *exec.Cmd
	pid int
}

func startKillGuardFixture(t *testing.T) killGuardFixture {
	t.Helper()
	cmd := exec.Command("sleep", strconv.Itoa(killGuardFixtureSleepSec))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //stylegate:ignore fixture teardown; the process is expected to be gone
		_ = cmd.Wait()         //stylegate:ignore reaping the fixture is the point; a double Wait is expected
	})
	pid := cmd.Process.Pid
	if pgid, err := syscall.Getpgid(pid); err != nil || pgid != pid {
		t.Fatalf("the fixture does not lead its own group (pgid=%d err=%v) — the positive control is not reachable", pgid, err)
	}
	return killGuardFixture{cmd: cmd, pid: pid}
}

// recordGroupSignals swaps the group-signal seam for a recorder, restoring it on
// cleanup, and returns a reader over what was addressed.
func recordGroupSignals(t *testing.T) func() []int {
	t.Helper()
	var seen []int
	orig := killChildTreeGroupSignal
	killChildTreeGroupSignal = func(pgid int, sig syscall.Signal) {
		seen = append(seen, pgid)
		orig(pgid, sig)
	}
	t.Cleanup(func() { killChildTreeGroupSignal = orig })
	return func() []int { return append([]int(nil), seen...) }
}

// TestKillChildTreeRefusesAGroupSignalThatWouldReachTheCaller is THE REFUSAL: the
// target group IS the caller's, so no group signal may be sent — while the child
// is still taken down by the targeted path.
func TestKillChildTreeRefusesAGroupSignalThatWouldReachTheCaller(t *testing.T) {
	fixture := startKillGuardFixture(t)
	groupSignals := recordGroupSignals(t)

	// The geometry of the measured defect: the group being considered is the
	// CALLER's own group. Here the seam is asked "is this group mine?" about the
	// very group the code under test would otherwise signal.
	origGroupOf := killChildTreeGroupOf
	killChildTreeGroupOf = func() int { return fixture.pid }
	t.Cleanup(func() { killChildTreeGroupOf = origGroupOf })

	if !killChildTreeGroupIsTheCallers(fixture.pid) {
		t.Fatal("the guard did not recognise the target group as the caller's own — the refusal is not reachable")
	}

	killChildTree(fixture.pid)

	// why: the audited set is the calls killChildTreeGroupSignal saw, and the total claim is about GROUP signals only — the per-pid kill is expected and asserted below (the child must still die and be reaped), so the action verb names the one call whose absence is the subject, not that nothing happened.
	if got := groupSignals(); len(got) != 0 {
		t.Fatalf("killChildTree sent a group signal to %v — the target group was the CALLER's own (%d), so this is the self-kill the measured parecord defect produced: the operator's terminal dies and SIGKILL cannot be caught", got, fixture.pid)
	}

	// The refusal is not a degraded mode: the child itself is still killed and
	// reaped, and the caller survives.
	reaped := make(chan struct{})
	go func() {
		_, _ = fixture.cmd.Process.Wait() //stylegate:ignore the exit status is not asserted; the reap is the evidence
		close(reaped)
	}()
	select {
	case <-reaped:
	case <-time.After(killGuardFixtureWait):
		t.Fatalf("the child %d survived killChildTree — a refused group signal must still take the child down", fixture.pid)
	}
	if !processExists(os.Getpid()) {
		t.Fatal("this process is gone: the refusal did not prevent the self-kill")
	}
}

// TestKillChildTreeSignalsAGroupItMeasuredAsTheChildsOwn is the POSITIVE
// CONTROL, and the case that keeps the refusal from swallowing the normal path:
// a group the child owns and the caller is not in MUST be signalled as a group —
// that is what takes `nohup … &`-style descendants down, and losing it would
// reinstate bug011's deadlock.
func TestKillChildTreeSignalsAGroupItMeasuredAsTheChildsOwn(t *testing.T) {
	fixture := startKillGuardFixture(t)
	groupSignals := recordGroupSignals(t)

	if own := killChildTreeGroupOf(); fixture.pid == own {
		t.Skipf("the fixture's group equals this process's group (%d): the positive control needs a foreign group", own)
	}
	if killChildTreeGroupIsTheCallers(fixture.pid) {
		t.Fatalf("the guard refuses the group %d, which is neither this process's group (%d) nor its pid (%d) — every group signal would be refused", fixture.pid, killChildTreeGroupOf(), syscall.Getpid())
	}

	killChildTree(fixture.pid)

	if got := groupSignals(); len(got) != 1 || got[0] != fixture.pid {
		t.Fatalf("group signals = %v, want exactly [%d] — a child-owned, caller-foreign group must be addressed as a group", got, fixture.pid)
	}
}

// TestKillChildTreeSelfGuardUsesTheRealProcessGroupReading pins the guard to the
// MEASUREMENT (os.Getpgrp) rather than to a value it could assume: the caller's
// own group and this process's pid are both refused, a different number is not.
func TestKillChildTreeSelfGuardUsesTheRealProcessGroupReading(t *testing.T) {
	own, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("read this process's group: %v", err)
	}
	if !killChildTreeGroupIsTheCallers(own) {
		t.Errorf("the caller's own group %d is not refused — a group signal there kills this process and, under `gopi chat`, the operator's terminal", own)
	}
	if !killChildTreeGroupIsTheCallers(os.Getpid()) {
		t.Errorf("this process's pid %d is not refused: its group's id would be that pid, i.e. the group it lives in", os.Getpid())
	}
	// A number that is neither is NOT refused — the guard must be a predicate,
	// not a blanket refusal (that would break every group kill in the tool).
	foreign := own + os.Getpid() + killGuardFixtureSleepSec
	if foreign == own || foreign == os.Getpid() {
		t.Fatalf("the foreign probe %d collides with a guarded value", foreign)
	}
	if killChildTreeGroupIsTheCallers(foreign) {
		t.Errorf("group %d is refused although it is neither this process's group nor its pid", foreign)
	}
}
