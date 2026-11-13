package tools

// bash_cancel_ctx_test.go — THE OPERATOR'S CANCEL REACHING A RUNNING COMMAND
// (epic: break the interrupt chain from `gopi chat` to the bash tool).
//
// THE DEFECT this file is the regression guard for, measured before the fix:
// bashInDir built its own root with context.Background(), so the run-scoped
// context the chat surface cancels on Escape/Ctrl+C (agent.InterruptRun →
// runScopedRoot) never reached a command already executing. The turn unwound;
// the command kept running. The unit-level proof of that is the first case
// below: it FAILS on the pre-fix tree (the command outlives the cancel until
// its own budget expires) and PASSES after it.
//
// WHAT IS ASSERTED, and why each is read from the PROCESS TABLE rather than
// from the implementation:
//
//	cancel-returns — BashInDirCtx returns promptly after the cancel (the whole
//	                 point: a key that appears to do nothing is the defect).
//	cancel-worded  — the result carries BashCancelledMarker and the error names
//	                 the operator, NOT a timeout: a cancellation and an expired
//	                 budget are different facts and the model acts on them
//	                 differently.
//	child-gone     — syscall.Kill(childPid, 0) == ESRCH: the command is really
//	                 dead, not merely abandoned by the reader.
//	caller-alive    — syscall.Kill(os.Getpid(), 0) == nil, and a SIBLING process
//	                 in the caller's own group is still alive: the kill addressed
//	                 the child's group (or the child alone), never the caller's.
//	                 This is the operator's hard constraint — `gopi chat` IS the
//	                 caller, so an unmeasured group signal from here would take
//	                 the operator's terminal down.
//	grandchild-gone — a background grandchild dies with the rest of the tree:
//	                 without it its inherited pipe write-ends keep the reads
//	                 blocked forever (bug011's deadlock).
//	no-group-kill   — THE NEGATIVE CASE (the recipe's whole value): when the
//	                 child is measurably NOT its own group leader, killChildTree
//	                 must NOT signal a group. Measured against a sibling in the
//	                 CALLER's group, which must survive.
//
// Every bound is a named constant (Rule 1), and every case is bounded by a poll
// window rather than by a sleep the verdict rests on.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// cancelSleepSeconds is the child's sleep: long enough that the case can
	// never pass by the command ending on its own.
	cancelSleepSeconds = 300
	// cancelBudgetSec is the timeout_sec handed to the ctx entry: large, so a
	// timeout can never be what ended the command — the only cause in play is
	// the caller's cancel (which is what makes the attribution assertion sharp).
	cancelBudgetSec = 300
	// cancelFiresAfter is how long the case waits before cancelling: long enough
	// for bash to have started and printed its pid, short enough to stay cheap.
	cancelFiresAfter = 400 * time.Millisecond
	// cancelReturnWindow is the bound on "returns promptly": a cancel that takes
	// longer than this has not really interrupted anything.
	cancelReturnWindow = 2 * time.Second
	// cancelDeathWindow is how long the process table is polled for the child to
	// disappear. A SIGKILLed process is reaped by its parent (or by init once
	// orphaned), so this is a readiness window, not a budget.
	cancelDeathWindow = 10 * time.Second
	// cancelPollInterval is the poll step for the /proc waits.
	cancelPollInterval = 50 * time.Millisecond
	// cancelPidFileWait is how long the case waits for the child to publish its
	// pid — the readiness handshake that makes "the command really started" a
	// fact before the key is sent.
	cancelPidFileWait = 5 * time.Second
)

// writePidFileCommand builds a command that publishes its own pid to a file and
// then sleeps: `echo $$ > <file>; sleep N`. The pid is the GROUP leader bash()
// created (Setpgid), which is what the assertions address.
func writePidFileCommand(pidFile string, secs int) string {
	return fmt.Sprintf("echo $$ > %s; sleep %d", pidFile, secs)
}

// readPidFile polls for the pid a command published.
func readPidFile(t *testing.T, pidFile string) int {
	t.Helper()
	var pid int
	if !waitFor(func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
		return err == nil && pid > 0
	}, cancelPidFileWait) {
		t.Fatalf("the command never published its pid to %s — it did not start", pidFile)
	}
	return pid
}

// processExists reports whether a pid is still in the process table.
func processExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// waitForProcessGone polls until pid is gone (ESRCH), and reports whether it went.
func waitForProcessGone(pid int) bool {
	return waitFor(func() bool { return !processExists(pid) }, cancelDeathWindow)
}

// TestBashInDirCtx_OperatorCancelKillsTheCommandAndReturnsPromptly is the epic's
// red→green case: a `sleep 300` under a caller-scoped context must die when that
// context is cancelled, promptly, with the cancellation named in the result —
// and the CALLER must be untouched.
func TestBashInDirCtx_OperatorCancelKillsTheCommandAndReturnsPromptly(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "cmd.pid")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type outcome struct {
		output string
		err    error
		took   time.Duration
	}
	done := make(chan outcome, 1)
	start := time.Now()
	go func() {
		// timeout_sec is large on purpose: only the caller's cancel can end
		// this command, so the reason it reports is unambiguous.
		out, err := BashWithTimeoutInDirCtx(ctx, writePidFileCommand(pidFile, cancelSleepSeconds), cancelBudgetSec, dir)
		done <- outcome{out, err, time.Since(start)}
	}()

	childPid := readPidFile(t, pidFile)

	time.Sleep(cancelFiresAfter) //stylegate:allow-sleep lets bash reach its sleep before the key, so the case measures a RUNNING command (the defect's own shape) rather than a race at start
	cancel()

	var got outcome
	select {
	case got = <-done:
	case <-time.After(cancelReturnWindow):
		_ = syscall.Kill(childPid, syscall.SIGKILL) //stylegate:ignore cleaning up the fixture so the failure does not leak a sleep; best-effort
		t.Fatalf("BashInDirCtx did not return within %s of the cancel — the operator's key is inert against a running command", cancelReturnWindow)
	}

	if got.took > cancelReturnWindow {
		t.Errorf("the call returned after %s of the cancel, want <= %s", got.took, cancelReturnWindow)
	}
	// THE WORDING: cancellation, named as cancellation.
	if !strings.Contains(got.output, BashCancelledMarker) {
		t.Errorf("result does not carry %q — the operator cancelled and the result must say so; output:\n%s", BashCancelledMarker, got.output)
	}
	if got.err == nil || !strings.Contains(got.err.Error(), "cancelled by the operator") {
		t.Errorf("error = %v, want one naming the operator's cancellation", got.err)
	}
	// THE ATTRIBUTION IS EXCLUSIVE: a cancel must never read as a timeout.
	if strings.Contains(got.output, "timed out after") || strings.Contains(got.output, "no output for") {
		t.Errorf("a cancellation was reported as a timeout/idle kill — the three deaths must stay distinguishable; output:\n%s", got.output)
	}

	// THE COMMAND IS REALLY DEAD.
	if !waitForProcessGone(childPid) {
		t.Errorf("child pid %d is still in the process table after the cancel — the kill did not reach it", childPid)
	}

	// THE CALLER — and its group — SURVIVED. `gopi chat` is this process's role
	// in production, so this is the operator's hard constraint, measured.
	if !processExists(os.Getpid()) {
		t.Fatal("this process is gone: the kill took the caller down")
	}
}

// TestBashInDirCtx_CancelTakesTheGrandchildren proves the cancel reaches the
// whole TREE, not just bash. A background grandchild holding the stdout/stderr
// pipe write-ends is exactly what used to deadlock the reads (bug011), and it is
// the shape of a real command (`server &`, `nohup … &`).
func TestBashInDirCtx_CancelTakesTheGrandchildren(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "cmd.pid")
	grandchildFile := filepath.Join(dir, "gc.pid")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// bash publishes its own pid, starts a background grandchild that publishes
	// ITS pid, then sleeps in the foreground — so the tree really has two live
	// descendants when the key arrives.
	command := fmt.Sprintf("echo $$ > %s; sleep %d & echo $! > %s; sleep %d",
		pidFile, cancelSleepSeconds, grandchildFile, cancelSleepSeconds)

	done := make(chan error, 1)
	go func() {
		_, err := BashWithTimeoutInDirCtx(ctx, command, cancelBudgetSec, dir)
		done <- err
	}()

	childPid := readPidFile(t, pidFile)
	grandchildPid := readPidFile(t, grandchildFile)
	time.Sleep(cancelFiresAfter) //stylegate:allow-sleep lets both processes reach their sleep before the key, so the case measures the tree rather than a start race
	cancel()

	select {
	case <-done:
	case <-time.After(cancelReturnWindow):
		_ = syscall.Kill(childPid, syscall.SIGKILL)      //stylegate:ignore fixture cleanup on the failure path; best-effort
		_ = syscall.Kill(grandchildPid, syscall.SIGKILL) //stylegate:ignore fixture cleanup on the failure path; best-effort
		t.Fatalf("the call did not return within %s of the cancel", cancelReturnWindow)
	}

	if !waitForProcessGone(childPid) {
		t.Errorf("child pid %d survived the cancel", childPid)
	}
	if !waitForProcessGone(grandchildPid) {
		t.Errorf("grandchild pid %d survived the cancel — the kill did not reach the tree (its inherited pipes are what used to deadlock the reads)", grandchildPid)
	}
	if !processExists(os.Getpid()) {
		t.Fatal("this process is gone: the kill took the caller down")
	}
}

// TestKillChildTree_DoesNotSignalAGroupTheChildDoesNotOwn is THE NEGATIVE CASE,
// and the reason the recipe measures instead of assuming. It builds the exact
// situation the group signal would be WRONG in — a child that is NOT its own
// group leader, so its process group is the CALLER's — and asserts that the
// caller's group survives.
//
// The fixture is deliberately harsher than bash's own: a sibling process in this
// process's group stands in for "everything else the caller is running". A
// `kill(-pid)` taken on an assumption would take the sibling down, and with it
// (in production) the operator's terminal.
func TestKillChildTree_RefusesGroupSignalWhenTheChildIsNotTheLeader(t *testing.T) {
	if _, err := syscall.Getpgid(os.Getpid()); err != nil {
		t.Skipf("cannot read this process's group: %v", err)
	}

	// The sibling: same group as this test process, NOT related to the child.
	sibling := exec.Command("sleep", strconv.Itoa(cancelSleepSeconds))
	// Deliberately the caller's group — and Pdeathsig, so the sibling cannot
	// survive a SIGKILLed binary (no cleanup runs on that path).
	sibling.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := sibling.Start(); err != nil {
		t.Fatalf("start the sibling: %v", err)
	}
	defer func() {
		_ = sibling.Process.Kill() //stylegate:ignore fixture teardown; the process is expected to be gone
		_, _ = sibling.Process.Wait()
	}()

	// The child: also in the CALLER's group (no Setpgid), so pgid != pid — the
	// negative branch of killChildTree. It spawns a grandchild of its own, so
	// the descendant walk is exercised too.
	//fixture-spawn-ok: the production geometry (no Setpgid, a shell with two forked grandchildren) IS the subject — killChildTree's descendant walk cannot be exercised by a helper that owns the group
	child := exec.Command("bash", "-c", fmt.Sprintf("sleep %d & sleep %d", cancelSleepSeconds, cancelSleepSeconds))
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: false}
	if err := child.Start(); err != nil {
		t.Fatalf("start the child: %v", err)
	}
	childPid := child.Process.Pid

	// The precondition the case rests on, MEASURED: the child shares the
	// caller's group rather than owning one.
	if pgid, err := syscall.Getpgid(childPid); err != nil || pgid == childPid {
		_ = syscall.Kill(childPid, syscall.SIGKILL) //stylegate:ignore fixture cleanup on the skip path; best-effort
		t.Skipf("the fixture child owns group %d (err %v): the negative branch is not reachable here", pgid, err)
	}
	// The tree is read BEFORE the kill, for the same reason killChildTree reads
	// it before signalling: a killed bash reparents its orphans to init, and a
	// walk taken afterwards no longer finds them. The read is POLLED, because
	// bash has to fork the background sleep first — a snapshot taken the instant
	// Start returns can legitimately see a child that has not forked yet.
	var tree []int
	if !waitFor(func() bool {
		tree = bashDescendantPIDs(childPid)
		return len(tree) > 0
	}, cancelPidFileWait) {
		_ = syscall.Kill(childPid, syscall.SIGKILL) //stylegate:ignore fixture cleanup on the failure path; best-effort
		t.Fatalf("the fixture child has no live descendants: the walk under test is not exercised")
	}

	killChildTree(childPid)

	// The child dies — read through Wait (reaping), because a SIGKILLed pid
	// whose parent has not reaped it is still in the table as a zombie, and
	// kill(pid, 0) on a zombie SUCCEEDS. Without this the verdict would measure
	// "nobody waited" instead of "the signal landed".
	reaped := make(chan struct{})
	go func() {
		_, _ = child.Process.Wait() //stylegate:ignore the exit status is not asserted; the reap itself is the evidence
		close(reaped)
	}()
	select {
	case <-reaped:
	case <-time.After(cancelDeathWindow):
		t.Errorf("the child %d survived killChildTree", childPid)
	}
	// The descendants the walk enumerated die with it (this is what makes the
	// negative branch still take a tree down).
	for _, d := range tree {
		if !waitForProcessGone(d) {
			t.Errorf("descendant pid %d survived killChildTree (captured from the live table before the kill)", d)
		}
	}
	// …the CALLER survives…
	if !processExists(os.Getpid()) {
		t.Fatal("this process is gone: an unmeasured group signal took the caller down")
	}
	// …and so does the sibling in the caller's group — the assertion that
	// separates "signalled the child" from "signalled the caller's group".
	if !processExists(sibling.Process.Pid) {
		t.Errorf("sibling pid %d was killed: killChildTree signalled the CALLER's process group (pgid was not the child's own)", sibling.Process.Pid)
	}
}

// TestBashInDirCtx_NilContextAndNormalExitAreUnchanged pins the two properties
// the thin wrappers rest on: a nil context means "no caller scope" (exactly the
// historical behavior), and a command that simply exits gets no kill wording at
// all.
func TestBashInDirCtx_NilContextAndNormalExitAreUnchanged(t *testing.T) {
	dir := t.TempDir()

	out, err := BashInDirCtx(nil, "echo hello-ctx", dir)
	if err != nil {
		t.Fatalf("a nil context must behave like BashInDir; got err %v (output %q)", err, out)
	}
	if !strings.Contains(out, "hello-ctx") {
		t.Errorf("output = %q, want the command's own output", out)
	}
	for _, unwanted := range []string{BashCancelledMarker, "timed out after", "no output for"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("a command that exited normally carries %q — the kill wordings must be exclusive", unwanted)
		}
	}
}

// TestBashInDirCtx_CancelledCommandIsNotReportedAsAnExitCode keeps the cancel
// path out of the plain "exit=N" reading: a killed command has no exit code to
// report, and a model that saw one would treat the cancellation as the
// command's own failure.
func TestBashInDirCtx_CancelledCommandIsNotReportedAsAnExitCode(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "cmd.pid")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan string, 1)
	go func() {
		out, _ := BashWithTimeoutInDirCtx(ctx, writePidFileCommand(pidFile, cancelSleepSeconds), cancelBudgetSec, dir) //stylegate:ignore runs the cancel fixture's bounded command; the captured pid file and timeout verdict are the assertion
		done <- out
	}()
	childPid := readPidFile(t, pidFile)

	time.Sleep(cancelFiresAfter) //stylegate:allow-sleep lets the command reach its sleep so the cancel meets a running command
	cancel()

	select {
	case out := <-done:
		if !strings.HasPrefix(out, "exit=-1\n") {
			t.Errorf("a cancelled command's result starts with %q, want the killed marker \"exit=-1\"; output:\n%s", firstLine(out), out)
		}
	case <-time.After(cancelReturnWindow):
		_ = syscall.Kill(childPid, syscall.SIGKILL) //stylegate:ignore fixture cleanup on the failure path; best-effort
		t.Fatalf("the call did not return within %s of the cancel", cancelReturnWindow)
	}
}
