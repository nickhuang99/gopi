package main

// worker_hangup_drain_red_test.go — the RED proof of the hangup-drain fix,
// kept as a standalone minimal repro so the transition is measured on a test
// that compiles against BOTH the pre-fix and the post-fix tree (it names no new
// symbol). The full contract arms live in worker_hangup_drain_test.go.
//
// WHY THE OBSERVATION IS A REAP AND NOT kill(pid, 0): a child that was
// signalled becomes a ZOMBIE, and `kill(pid, 0)` SUCCEEDS on a zombie — the
// liveness probe that looks right reports "still alive" for a corpse (measured
// in this run: the first version of this file passed on the pre-fix tree for
// exactly that reason). Reaping is the kernel's own answer: cmd.Wait returns the
// instant the child dies, signalled or not.

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// TestRedProof_HangupSurvivesTheRoundChild: pre-fix this fails (the forwarder
// SIGTERMs the in-flight child one settle window after the hangup); post-fix it
// passes (nothing is forwarded).
func TestRedProof_HangupSurvivesTheRoundChild(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState)
	workerCtx = workerDaemonCtx{}
	cmd, _ := startSyntheticRunChild(t)

	uninstall := installWorkerStopSignals()
	defer uninstall()
	stopForwarder := startWorkerStopForwarder()
	defer stopForwarder()

	keep := make(chan os.Signal, 1)
	signal.Notify(keep, syscall.SIGHUP)
	defer signal.Stop(keep)

	done := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(done) }() //stylegate:ignore the wait error IS the signal under test — the done channel is what the assertion reads

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("deliver SIGHUP: %v", err)
	}
	bound := workerStopDrainSettle + 3*time.Second
	select {
	case <-done:
		t.Fatalf("RED: the in-flight run child was SIGNALLED by a terminal hangup (it exited within %s "+
			"of SIGHUP) — a tab closing is not the operator asking for the work to stop", bound)
	case <-time.After(bound):
		t.Logf("GREEN: the run child (pid %d) survived %s after SIGHUP", cmd.Process.Pid, bound)
	}
}
