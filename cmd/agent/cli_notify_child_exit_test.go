package main

// cli_notify_child_exit_test.go — the ONE exit event a notify fixture's console
// child produces, and the guard that keeps it awaitable more than once.
//
// THE DEFECT THIS FILE CLOSES (measured 2026-10-13, task 01a13233, on the real
// test binary — `agent.test -test.run='^TestNotifyDeliverReachesARunningChatSession$'
// -test.v`, SIGQUIT taken inside the silent window):
//
//	=== RUN   TestNotifyDeliverReachesARunningChatSession      t+1.6s
//	    cli_notify_deliver_test.go:321: session: … same session       t+14.1s
//	SIGQUIT -> goroutine 4 … cli_notify_deliver_test.go:257 (the DEFER) t+20.0s
//	--- PASS: TestNotifyDeliverReachesARunningChatSession (31.49s)
//
// Every one of these cases finished its assertions in ~2s and then sat SILENT
// for a flat ~30.0s, because the child's exit was WRITTEN to `done` once and
// READ twice:
//
//	done := make(chan error, 1)
//	go func() { done <- cmd.Wait() }()   // ONE value, buffered
//	… select { case <-done: … }          // the body's wait CONSUMES it
//	defer func() { select { case <-done: … case <-time.After(notifyTestTimeout): } }()
//	                                     // the defer waits on an EMPTY channel
//
// The body's select drained the single buffered value, so the deferred cleanup
// could never see it — its `<-done` arm is unready forever and the ONLY arm
// left is `time.After(notifyTestTimeout)`: the full 30s bound, paid once per
// test case, to learn something already known. The goroutine dump names the
// frame exactly: the test goroutine is parked in the DEFER at
// cli_notify_deliver_test.go:257, not in any poll loop.
//
// THE FIX IS THE SHAPE, not the bound: an exit is a CLOSED CHANNEL plus a
// retained status, so an arbitrary number of awaits all return immediately (a
// closed channel is always ready; a buffered channel holds one value). Lowering
// the bound would have hidden the defect behind a shorter wait — the registry's
// own rule: a duration is not a mechanism.

import (
	"os/exec"
	"sync"
	"testing"
	"time"
)

// notifyChildExitProbeBound bounds the regression guard's own awaits. It is
// deliberately far below notifyTestTimeout: the guard asserts that a SECOND
// await returns promptly, and a pre-fix shape (a drained single-value channel)
// burns this whole bound instead — which is what makes it red. It is a WAIT,
// never a load (Rule 27).
const notifyChildExitProbeBound = 2 * time.Second

// notifyChildExit is a child process's exit as a REPLAYABLE event.
//
// A channel carrying the status once is the wrong shape for a lifecycle with
// more than one observer: the first reader takes the value and every later
// reader blocks until its own deadline. Here the channel is CLOSED once and the
// status is retained under a mutex, so `waitFor` is idempotent — the body may
// check "has it exited?" and the deferred cleanup may ask again, and both
// answer immediately from the same fact.
type notifyChildExit struct {
	cmd  *exec.Cmd
	once sync.Once
	wait chan struct{}
	mu   sync.Mutex
	err  error
}

// armNotifyChildExit starts the ONE goroutine that reaps cmd and publishes its
// exit. It must be called exactly once per child, right after cmd.Start.
func armNotifyChildExit(cmd *exec.Cmd) *notifyChildExit {
	e := &notifyChildExit{cmd: cmd, wait: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		e.mu.Lock()
		e.err = err
		e.mu.Unlock()
		e.once.Do(func() { close(e.wait) })
	}()
	return e
}

// exited reports whether the child's exit has already been observed, without
// waiting. It is the idempotent half: the deferred cleanup asks this first, so
// a child whose exit the body already consumed costs the defer nothing.
func (e *notifyChildExit) exited() bool {
	select {
	case <-e.wait:
		return true
	default:
		return false
	}
}

// waitFor waits up to bound for the child to exit and reports whether it did.
// Calling it again after a true answer returns true immediately — that is the
// whole point (see the file head): the exit event is replayed, never consumed.
func (e *notifyChildExit) waitFor(bound time.Duration) bool {
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case <-e.wait:
		return true
	case <-timer.C:
		return false
	}
}

// status returns the retained exit status. It is only meaningful after waitFor
// (or exited) has reported true.
func (e *notifyChildExit) status() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

// TestNotifyChildExitIsAwaitableMoreThanOnce is the regression guard for the
// ~30s silent window: it arms the helper on a REAL trivial child and awaits the
// SAME exit THREE times, each inside notifyChildExitProbeBound.
//
// RED PROOF (run for real on this branch, see the report): replacing the
// helper's `close(e.wait)` + retained status with the pre-fix shape — the
// goroutine sending the status into a capacity-1 channel, `waitFor` reading
// from it — reddens the SECOND and THIRD awaits, because the first read drained
// the only value the channel will ever hold and each later await spends its
// whole bound. The mutation is the mechanism, so the guard cannot be satisfied
// by widening a number.
//
// The child is `true`/`false`-free by construction: it is started and awaited,
// and its exit status is asserted rather than its output, so the guard is about
// the EVENT, not about what the child did.
func TestNotifyChildExitIsAwaitableMoreThanOnce(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the trivial child: %v", err)
	}
	exit := armNotifyChildExit(cmd)

	for i := 1; i <= 3; i++ {
		start := time.Now()
		if !exit.waitFor(notifyChildExitProbeBound) {
			t.Fatalf("await #%d did not observe the child's exit within %s — the exit event was consumed by an earlier await instead of being replayed",
				i, notifyChildExitProbeBound)
		}
		if elapsed := time.Since(start); elapsed > notifyChildExitProbeBound/2 {
			t.Errorf("await #%d took %s — a replayed exit must return at once; a wait this long means the event was drained and this await spent its bound",
				i, elapsed)
		}
		if err := exit.status(); err != nil {
			t.Errorf("await #%d reports exit status %v, want nil (the retained status must survive every await)", i, err)
		}
	}
}
