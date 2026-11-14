// worker_stop_group_self_guard_test.go — A STOP MUST NEVER ADDRESS THE WORKER'S
// OWN PROCESS GROUP (the second half of task 01a163df's guard family, measured
// 2026-10-22; this consumer had only the first half).
//
// THE DEFECT. signalRunChild decided "send a group signal" with `pgid == pid`:
// "the CHILD leads the group it lives in". That answers whether the child OWNS
// the group; it does not answer whether the worker doing the killing is INSIDE
// it — and the worker is normally a group leader itself (started from a shell),
// so when a run child is spawned without Setpgid the hazard geometry is the same
// one that took out `gopi chat` through its parecord recorders: a group signal
// there is a self-kill of the worker, its round and the operator's terminal, and
// SIGKILL cannot be caught — the guard is the only defence.
//
// THE FIX HAS TWO HALVES and this file pins the CONSUMER half:
//
//	guard    — runChildSignalGroupIsTheCallers: a group signal is REFUSED when
//	           the target group is this process's own (runChildSignalGroupOf, a
//	           seam over syscall.Getpgrp — see the seam's comment for why the
//	           measurement must be lowerable).
//	targeted — the child AND its enumerated descendants are still signalled one
//	           by one, so refusing the group never leaves a stop undelivered
//	           (bug011's pipe-writer deadlock must not come back).
//
// WHY THE SIGNAL ITSELF IS A SEAM: both branches end with the child dead, so the
// OUTCOME cannot tell "refused the group" from "signalled the group" — and the
// refusal is the whole subject. runChildGroupSignal records which group was
// addressed. The positive control pins the other direction: a group the child
// owns and the caller is NOT in must still be signalled as a group.

package main

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// groupGuardFixtureSleepSec keeps the fixture alive for the whole case: no
	// case may pass because a process ended on its own.
	groupGuardFixtureSleepSec = 300
	// groupGuardWait bounds the wait for the fixture's grandchild to appear, for
	// the signalled child to be reaped, and for the grandchild to reach its own
	// terminal exit (see grandchildGoneWithin).
	groupGuardWait = 10 * time.Second
	// groupGuardPollInterval is the sample interval of the bounded waits below.
	// It is a poll interval, not a tolerance: every one of them exits on its
	// first matching observation, and only the deadline can make one fail.
	groupGuardPollInterval = 2 * time.Millisecond
)

// groupGuardFixture is a run child in the production geometry that MATTERS here:
// it leads its own group (Setpgid) and it has a grandchild, so the targeted path
// has a descendant to prove it still reaches.
type groupGuardFixture struct {
	cmd   *exec.Cmd
	pid   int
	grand int
}

// startGroupGuardFixture starts `bash -c 'sleep N & wait'` in its own group and
// waits (bounded) for the grandchild to exist.
func startGroupGuardFixture(t *testing.T) groupGuardFixture {
	t.Helper()
	//fixture-spawn-ok: the fixture must own a process GROUP with a forked grandchild (the targeted-vs-group signal distinction is the subject), so the group cannot be delegated to a helper; the cleanup kills the pid and the grandchild explicitly
	cmd := exec.Command("bash", "-c", "sleep "+strconv.Itoa(groupGuardFixtureSleepSec)+" & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the fixture: %v", err)
	}
	pid := cmd.Process.Pid
	fixture := groupGuardFixture{cmd: cmd, pid: pid}
	t.Cleanup(func() {
		// Take down what this case leaves behind, WITHOUT a group signal: the
		// fixture's own group may be the geometry under test.
		// OWNED HANDLE: `pid` is `cmd.Process.Pid` of the fixture started above
		// and the `*exec.Cmd` is live until the Wait below, so an incarnation
		// read could add nothing (Rule 42) — and a group signal is deliberately
		// NOT used here, because the fixture's own group geometry is the subject.
		_ = syscall.Kill(pid, syscall.SIGKILL) //stylegate:ignore fixture teardown of a probe this case created; ESRCH (already dead) is the expected error
		//stylegate:ignore fixture teardown of the probe's own grandchild; ESRCH is the expected error and a recycled pid is refused by the guard
		if grandID, grandErr := testutil.ReadProcIdentity(fixture.grand); grandErr == nil {
			_ = testutil.KillIfIdentical(grandID, syscall.SIGKILL)
		}
		_ = cmd.Wait() //stylegate:ignore reaping the fixture is the point; a second Wait (the case already reaped it) is expected
	})
	if pgid, err := syscall.Getpgid(pid); err != nil || pgid != pid {
		t.Fatalf("the fixture does not lead its own group (pgid=%d err=%v) — neither the positive control nor the refusal is reachable", pgid, err)
	}
	deadline := time.Now().Add(groupGuardWait)
	for time.Now().Before(deadline) {
		if kids := descendantPIDs(pid); len(kids) > 0 {
			fixture.grand = kids[0]
			return fixture
		}
		time.Sleep(50 * time.Millisecond) //stylegate:allow-sleep bounded poll of the real process table, capped by groupGuardWait
	}
	t.Fatalf("the fixture never produced a grandchild — the descendant half of the targeted path would go unobserved")
	return fixture
}

// startGroupMember starts a process that JOINS pgid (Setpgid+Pgid, no exec-time
// group of its own) and is our child, NOT a descendant of the fixture. It is the
// SENTINEL that makes the refusal checkable from the PROCESS TABLE alone: the
// targeted path (kill the child, then each descendant) can never reach it, while
// ANY signal addressed to the whole group reaches it — whether that signal went
// through the recorded seam or a bare syscall.Kill. Without it the refusal
// assertion would rest entirely on the seam, and a red arm that reverts the guard
// to a DIRECT group signal would silence the recorder and pass vacuously
// (measured: that is exactly what the first red arm did).
func startGroupMember(t *testing.T, pgid int) int {
	t.Helper()
	cmd := exec.Command("sleep", strconv.Itoa(groupGuardFixtureSleepSec))
	// A group id names its leader's pid, so joining one requires that leader to
	// be alive: the fixture must be started first (it is).
	// Joining the group is the SUBJECT; Pdeathsig adds the lifetime arm (a
	// member of someone else's group gets no group teardown of its own).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pgid, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the group sentinel in group %d: %v", pgid, err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		// OWNED HANDLE: same shape as the fixture teardown above — `pid` is the
		// `cmd.Process.Pid` of the sentinel started in this function.
		_ = syscall.Kill(pid, syscall.SIGKILL) //stylegate:ignore teardown of the probe's own sentinel; ESRCH (already dead) is the expected error
		_ = cmd.Wait()                         //stylegate:ignore reaping the sentinel is the point; a second Wait is expected
	})
	if got, err := syscall.Getpgid(pid); err != nil || got != pgid {
		t.Fatalf("the sentinel does not live in group %d (pgid=%d err=%v) — the process-table half of the refusal would be unobservable", pgid, got, err)
	}
	return pid
}

// recordRunChildGroupSignals swaps the group-signal seam for a recorder (the
// original is still called, so production behaviour is unchanged) and returns a
// reader over the groups that were addressed.
func recordRunChildGroupSignals(t *testing.T) func() []int {
	t.Helper()
	var seen []int
	orig := runChildGroupSignal
	runChildGroupSignal = func(pgid int, sig syscall.Signal) bool {
		seen = append(seen, pgid)
		return orig(pgid, sig)
	}
	t.Cleanup(func() { runChildGroupSignal = orig })
	return func() []int { return append([]int(nil), seen...) }
}

// reapWithin waits (bounded) for the fixture's direct child to be reaped, which
// is only possible if the stop actually reached it.
func reapWithin(t *testing.T, cmd *exec.Cmd, pid int) {
	t.Helper()
	reaped := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait() //stylegate:ignore the exit status is not the subject; the reap is the evidence
		close(reaped)
	}()
	select {
	case <-reaped:
	case <-time.After(groupGuardWait):
		t.Fatalf("the child %d survived the stop — a refused group signal must still take the child down", pid)
	}
}

// grandchildGoneWithin waits (bounded) for the fixture's GRANDCHILD to reach a
// terminal state, and reports whether it did.
//
// WHY THIS WAIT MUST EXIST (measured 2026-10-25, 60 rounds of this fixture's own
// ordering, /proc reads only — no load): the direct child's reap is NOT evidence
// about the grandchild. At the instant `cmd.Process.Wait()` returned, the
// grandchild's state letter was R (running, signal merely QUEUED by kill(2) and
// not yet acted on) in 4/60 rounds and Z in 44/60, and the repo's predicate read
// ALIVE in 5/60 = 8.3% — surviving up to 1.17ms further. Its SIGCHLD does not
// reach this process (it belongs to the dying child / init), so nothing here is
// woken when it exits. Sampling ONCE therefore turns the assertion into a race
// and the case measured 1/10 FAIL (this run) and 2/5 FAIL (the briefing's).
//
// The predicate is NOT the defect: procAliveForTest already treats a zombie as
// gone, so nothing here is re-implemented — only the number of samples is.
// This REJECTS the process as fast as before (the first dead sample wins) and
// never widens: the deadline is the same groupGuardWait, and a grandchild that
// genuinely ignored SIGTERM still fails the case.
func grandchildGoneWithin(pid int) bool {
	deadline := time.Now().Add(groupGuardWait)
	for {
		if !procAliveForTest(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		//stylegate:allow-sleep bounded poll of the real process table: the loop exits on its first dead sample and the deadline above owns failure
		time.Sleep(groupGuardPollInterval)
	}
}

// TestSignalRunChildRefusesAGroupSignalThatWouldReachTheWorker is THE REFUSAL: the
// target group IS the worker's own, so no group signal may be sent — while the
// child and its grandchild are still taken down by the targeted path.
func TestSignalRunChildRefusesAGroupSignalThatWouldReachTheWorker(t *testing.T) {
	fixture := startGroupGuardFixture(t)
	groupSignals := recordRunChildGroupSignals(t)
	// The sentinel shares the fixture's group and is NOT its descendant, so it
	// only dies if a signal is addressed to the WHOLE group (see startGroupMember
	// for why the process table, not just the seam, has to be asserted).
	sentinel := startGroupMember(t, fixture.pid)

	// The geometry of the measured defect: the group under consideration is the
	// WORKER's own. Lowering the "my group" reading to the child's group is what
	// makes that geometry observable in one live process (see the seam comment).
	origGroupOf := runChildSignalGroupOf
	runChildSignalGroupOf = func() int { return fixture.pid }
	t.Cleanup(func() { runChildSignalGroupOf = origGroupOf })

	if !runChildSignalGroupIsTheCallers(fixture.pid) {
		t.Fatal("the guard did not recognise the target group as the worker's own — the refusal is not reachable")
	}

	if !signalRunChild(fixture.pid, syscall.SIGTERM) {
		t.Error("the stop reported that NO signal was delivered — the refusal must degrade to the targeted path, not to silence (Rule 15)")
	}

	// why: the audited set is the groups runChildGroupSignal saw, and the total claim is about GROUP signals only — the per-pid kills are expected and asserted just below by the child and grandchild dying, so the verb names the one call whose absence is the subject.
	if got := groupSignals(); len(got) != 0 {
		t.Fatalf("signalRunChild sent a group signal to %v — the target group (%d) was the WORKER's own, so this is the self-kill the guard exists to refuse (SIGKILL there takes the worker and the operator's terminal, and it cannot be caught)", got, fixture.pid)
	}

	reapWithin(t, fixture.cmd, fixture.pid)
	// why: the child's reap says nothing about the grandchild — kill(2) only queues, and its SIGCHLD never reaches this process — so the assertion has to wait for that process's OWN terminal exit (measured 8.3% alive at the reap instant; see grandchildGoneWithin).
	if !grandchildGoneWithin(fixture.grand) {
		t.Errorf("the grandchild %d survived the stop (still alive %s after the child was reaped): refusing the group signal must not leave the child's descendants running (that is bug011's deadlock returning)", fixture.grand, groupGuardWait)
	}
	// The process-table half: an intact sentinel is the evidence that no group
	// was addressed, independent of which call site produced the signal.
	if !procAliveForTest(sentinel) {
		t.Fatalf("the sentinel %d (a member of the target group %d, not a descendant of the child) was signalled — the stop addressed the WHOLE group, which is the self-kill this guard exists to refuse", sentinel, fixture.pid)
	}
	if !procAliveForTest(os.Getpid()) {
		t.Fatal("this process is gone: the refusal did not prevent the self-kill")
	}
}

// TestSignalRunChildSignalsAGroupItMeasuredAsTheChildsOwn is the POSITIVE CONTROL,
// and the case that keeps the refusal from swallowing the normal path: a group the
// child owns and the worker is NOT in MUST be addressed as a group — losing that
// would make every descendant-bearing stop a per-pid enumeration.
func TestSignalRunChildSignalsAGroupItMeasuredAsTheChildsOwn(t *testing.T) {
	fixture := startGroupGuardFixture(t)
	groupSignals := recordRunChildGroupSignals(t)

	if own := runChildSignalGroupOf(); fixture.pid == own || fixture.pid == os.Getpid() {
		t.Skipf("the fixture's group equals this process's group/pid (%d): the positive control needs a foreign group", own)
	}
	if runChildSignalGroupIsTheCallers(fixture.pid) {
		t.Fatalf("the guard refuses the group %d, which is neither this process's group (%d) nor its pid (%d) — every group stop would be refused", fixture.pid, runChildSignalGroupOf(), os.Getpid())
	}

	if !signalRunChild(fixture.pid, syscall.SIGTERM) {
		t.Error("the stop reported that NO signal was delivered for a child-owned group")
	}
	if got := groupSignals(); len(got) != 1 || got[0] != fixture.pid {
		t.Fatalf("group signals = %v, want exactly [%d] — a child-owned, worker-foreign group must be addressed as a group", got, fixture.pid)
	}
	reapWithin(t, fixture.cmd, fixture.pid)
}

// TestRunChildSignalGroupGuardUsesTheRealProcessGroupReading pins the guard to the
// MEASUREMENT (syscall.Getpgrp) rather than to an assumed value: the worker's own
// group and its own pid are both refused, a different number is not — a blanket
// refusal would break every group stop in the worker.
func TestRunChildSignalGroupGuardUsesTheRealProcessGroupReading(t *testing.T) {
	own, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("read this process's group: %v", err)
	}
	if !runChildSignalGroupIsTheCallers(own) {
		t.Errorf("the worker's own group %d is not refused — a group signal there kills the worker mid-round and the operator's terminal with it", own)
	}
	if !runChildSignalGroupIsTheCallers(os.Getpid()) {
		t.Errorf("this process's pid %d is not refused: that is the id of the group it lives in when it leads one", os.Getpid())
	}
	foreign := own + os.Getpid() + groupGuardFixtureSleepSec
	if foreign == own || foreign == os.Getpid() {
		t.Fatalf("the foreign probe %d collides with a guarded value", foreign)
	}
	if runChildSignalGroupIsTheCallers(foreign) {
		t.Errorf("group %d is refused although it is neither this process's group nor its pid", foreign)
	}
}
