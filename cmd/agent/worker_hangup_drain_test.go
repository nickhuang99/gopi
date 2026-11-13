package main

// worker_hangup_drain_test.go — the regression guard for the DRAIN semantics of
// a terminal hangup (epic: SIGHUP-is-a-drain, measured 2026-10-27).
//
// THE DEFECT IT PINS. A pty teardown — the operator closing the tab, an ssh
// drop, a session scope going away — delivers SIGHUP to the foreground worker.
// The worker treated that exactly like SIGTERM ("finish the in-flight round and
// leave") and, because the round cannot finish by itself while it is blocked in
// cmd.Wait(), the forwarder reached into the in-flight run child with SIGTERM:
// a round that had been running 38 minutes closed out `failed: run interrupted
// by signal`. The terminal's disappearance is not the operator asking for the
// work to stop, so the hangup must NOT be forwarded: the round is allowed to
// finish, and the worker simply claims nothing new afterwards.
//
// TWO ARMS, ONE RIG, and the second arm is what makes the first one mean
// something: a test that only proved "SIGHUP is harmless" would also pass if the
// forwarder were dead in every direction. So the SAME rig proves SIGTERM still
// reaches the child — the supervisor's contract is unchanged.
//
// WHAT A UNIT TEST CANNOT SEE: that the round's own boundary treats the hangup
// as a drain (awaitWorkerStopChild's `drain` argument). That half is measured by
// the REAL-execution evidence in the task report (a live worker, a live row, a
// real SIGHUP, /proc liveness), and pinned here by the shape assertion at the
// bottom.

import (
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// workerHangupObserveBound is how long the SIGHUP arm watches the child
	// AFTER the forwarder's own settle window has passed. The pre-fix forward
	// happens at workerStopDrainSettle, so a window that clears it by a margin
	// is enough to tell "not forwarded" from "not forwarded yet" — and it stays
	// far inside the synthetic child's own hold
	// (workerStopForwardHoldSeconds), so survival cannot be luck.
	workerHangupObserveBound = workerStopDrainSettle + 3*time.Second
	// workerTerminateObserveBound is the control arm's window: pre-fix and
	// post-fix alike the SIGTERM forward lands at workerStopDrainSettle, so the
	// control only has to outlast that with the same margin.
	workerTerminateObserveBound = workerStopDrainSettle + 3*time.Second
	// workerHangupPollInterval is the liveness poll (a bounded loop, never a
	// spin).
	workerHangupPollInterval = 100 * time.Millisecond
	// workerHangupSignalSpelling is the name the termination record must carry
	// for a hangup. It comes from the provenance vocabulary, not a second
	// spelling invented here (Rule 14).
	workerHangupSignalName = workerDrainSignalName
)

// keepTestAliveThroughHangup registers SIGHUP on a channel of the TEST's own so
// the test binary cannot die of the signal it sends itself (the default
// disposition is replaced by the registration; the production handler keeps its
// own registration and still receives it). Measured precedent: without this the
// very first red run aborted the whole binary with "signal: hangup".
func keepTestAliveThroughHangup(t *testing.T) {
	t.Helper()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	t.Cleanup(func() { signal.Stop(ch) })
}

// TestWorkerHangupDoesNotSignalTheRoundChild is the RED-proof: pre-fix the
// in-flight child is SIGTERMed one settle window after the hangup, so the round
// dies as a failure.
func TestWorkerHangupDoesNotSignalTheRoundChild(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	// The FOREGROUND face: no durable request file anywhere — the shape of a
	// worker started in a terminal tab.
	workerCtx = workerDaemonCtx{}
	cmd, _ := startSyntheticRunChild(t)

	uninstall := installWorkerStopSignals()
	defer uninstall()
	stopForwarder := startWorkerStopForwarder()
	defer stopForwarder()
	keepTestAliveThroughHangup(t)

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("could not deliver SIGHUP to this process: %v", err)
	}

	// Watch PAST the forwarder's settle: the pre-fix forward is due there.
	deadline := time.Now().Add(workerHangupObserveBound)
	for time.Now().Before(deadline) {
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("the in-flight run child was SIGNALLED by a terminal hangup (pid gone: %v). A tab "+
				"closing is not the operator asking for the work to stop: the round must be allowed to "+
				"finish, and the worker must only stop CLAIMING new rows.", err)
		}
		time.Sleep(workerHangupPollInterval) //stylegate:allow-sleep the poll interval of the bounded watch above, not a fixed wait: the deadline is a constant, the child's survival is re-read every tick, and an event channel does not exist for "the kernel must not have signalled it yet"
	}

	if !workerStopRequested() {
		t.Error("the hangup recorded no stop request — the worker would keep claiming rows against a " +
			"terminal session that no longer exists")
	}
	if !workerHangupDrainRequested() {
		t.Error("the hangup was not recorded as a DRAIN — without the flag every downstream arm " +
			"(the forwarder, the round boundary) falls back to the bare-signal contract and kills the round")
	}
	if workerStopEscalated() {
		t.Error("a hangup escalated the stop — escalation is the operator's force spelling only")
	}
	if got := workerStopSignalSpelling(); got != workerHangupSignalName {
		t.Errorf("the recorded signal spelling is %q, want %q — the termination record must name the "+
			"signal that was caught", got, workerHangupSignalName)
	}
}

// TestWorkerTerminateStillSignalsTheRoundChild is the CONTROL arm: SIGTERM is a
// supervisor's stop and keeps its pre-existing contract — it reaches the child,
// or the round would never end (the measured 2026-09-17 futex hang). A suite
// that only carried the SIGHUP arm could not tell "the hangup was made a drain"
// from "nothing is ever forwarded".
func TestWorkerTerminateStillSignalsTheRoundChild(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	workerCtx = workerDaemonCtx{}
	cmd, _ := startSyntheticRunChild(t)

	uninstall := installWorkerStopSignals()
	defer uninstall()
	stopForwarder := startWorkerStopForwarder()
	defer stopForwarder()

	done := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(done) }() //stylegate:ignore the wait error IS the signal under test — the done channel is what the assertion reads

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("could not deliver SIGTERM to this process: %v", err)
	}
	select {
	case <-done:
		// The child was signalled: the supervision contract is intact.
	case <-time.After(workerTerminateObserveBound):
		t.Fatalf("SIGTERM never reached the in-flight run child within %s — the supervisor's stop is "+
			"broken while the hangup arm passes, which would make that arm's green meaningless",
			workerTerminateObserveBound)
	}
	if workerHangupDrainRequested() {
		t.Error("SIGTERM was recorded as a hangup drain — only a terminal teardown has that semantics")
	}
}

// TestWorkerHangupIsADrainAtTheRoundBoundary pins the SHAPE the real-execution
// evidence stands on: the round boundary (and the explicit face) ask the ONE
// drain predicate that ORs the durable-file request with the hangup, so a hangup
// round is waited for WITHOUT a deadline instead of being terminated after
// workerStopGraceDefault. The predicate is asserted directly (a unit test cannot
// cheaply drive the real loop to its boundary), and the call sites are pinned by
// source so a future edit cannot put the file-only predicate back.
func TestWorkerHangupIsADrainAtTheRoundBoundary(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)

	if workerRoundMustFinish() {
		t.Fatal("a clean record reported a drain shape")
	}
	workerRequestStopWithSignal("SIGHUP received", false, workerHangupSignalName)
	workerMarkHangupDrain()
	if !workerRoundMustFinish() {
		t.Fatal("a recorded hangup did not make the round's must-finish predicate true — the boundary " +
			"would hand the child to the bounded bare-signal arm and SIGTERM it after the grace window")
	}

	// The call sites: both boundary arms must ask the ORing predicate, never the
	// file-only one (Rules 13/14: exactly one reader of a shared decision).
	src, err := os.ReadFile("cli_tasks_worker.go")
	if err != nil {
		t.Fatalf("read the loop's source: %v", err)
	}
	text := string(src)
	if strings.Contains(text, "awaitWorkerStopChild(workerDrainRequestedByFile())") {
		t.Error("cli_tasks_worker.go still passes the FILE-ONLY predicate to awaitWorkerStopChild — a " +
			"hangup round would be terminated after the grace window, which is the defect")
	}
	if strings.Contains(text, "if !workerDrainRequestedByFile() {\n\t\t\tawaitWorkerStopChild(false)") {
		t.Error("the explicit face still decides the child's wait from the file-only predicate")
	}
	if n := strings.Count(text, "awaitWorkerStopChild(workerRoundMustFinish())"); n == 0 {
		t.Error("no call site passes workerRoundMustFinish() to awaitWorkerStopChild — the hangup drain " +
			"never reaches the round boundary")
	}
}
