package main

// child_stop.go — the ONE place a gopi parent deliberately stops a child it
// spawned, and the place that makes such a stop (a) GRACEFUL and (b) TRACEABLE.
//
// WHY (epic: all gopi processes killed by signal, 2026-09-13). Both bounded
// waits in the merger — the resolve child's cap and the verify child's wait cap
// — ended with a bare SIGKILL of the child's whole process group. Two problems
// with that, and they are different problems:
//
//  1. IT IS NOT GRACEFUL. A SIGKILLed agent child never runs a defer: no
//     session_end, no ledger close-out, no crash dump. The row it was executing
//     is left in_progress with a dead holder — the zombie shape the operator
//     then has to reason about — and the reason the child sees downstream is
//     only "the process was killed". A SIGTERM first gives the child the same
//     wind-down every other gopi stop path already honours (the worker's
//     installWorkerStopSignals, main.go's agent Interrupt, the merger's own
//     graceful stop), and the wait caps exist to bound wall-clock, not to
//     forbear a graceful exit.
//
//  2. IT LEAVES NO TRACE OF WHO KILLED IT. A SIGKILL is uncatchable, so the
//     victim cannot record anything, and the fleet logs only ever showed the
//     victim's terminal line. When the whole fleet died on 2026-09-12/13, the
//     investigation could not name a sender. A deliberate kill must therefore
//     write its INTENT (internal/signalrec) before signalling — that line is
//     the only possible evidence for a SIGKILL.
//
// ORDER IS THE CONTRACT: record intent → SIGTERM → wait a bounded grace →
// SIGKILL whatever is left. Never SIGKILL first. The grace is short and bounded
// (the caller's cap has already elapsed by definition), so a wedged child still
// cannot hold the merger.

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/signalrec"
)

// childExit is the observable exit of ONE spawned child: a channel that is
// CLOSED when the child is reaped, plus the wait error carried beside it.
//
// WHY IT EXISTS (2026-09-16, bug "merger is losing track"): both bounded waits
// (the resolve child's cap, the verify child's wait cap) used a
// `done := make(chan error, 1)` filled by exactly one `child.Wait()`. ONE value
// means ONE receiver may consume it — while the timeout branch needed TWO
// independent waiters on the same exit: the grace probe handed to
// stopChildGracefully, and the consume after the graceful stop. The grace
// waiter is already parked on the channel when the cap fires, so it wins the
// value and the second `<-done` blocks FOREVER. Measured on the live machine
// twice (06:03 and 14:31 on 2026-09-16): the merger froze mid-pass in
// futex_wait holding serve.lock, its state file stopped advancing, and
// `merger --status` could only say "unknown — unparsable". A channel one
// process fills and two processes wait on is not a signal, it is a race.
//
// This type is the shape that cannot be read wrongly: `ch` is CLOSED on exit,
// so a close wakes EVERY waiter (including future ones), waiting is
// idempotent, and no waiter can steal another's signal. The exit status rides
// a separate field, written BEFORE the close (the close is the only
// synchronization a reader needs).
type childExit struct {
	once sync.Once
	ch   chan struct{}
	// werr is the `Wait()` error, written before ch is closed. A reader that
	// observed ch closed has the happens-before edge that makes this read
	// safe — reading it earlier is what `exited()` guards against.
	werr error
}

// newChildExit returns the exit record for a child that has NOT been reaped
// yet (spawn the waiter with wait immediately after the child starts).
func newChildExit() *childExit {
	return &childExit{ch: make(chan struct{})}
}

// wait reaps the child and publishes the exit to every waiter. Exactly ONE
// goroutine may run it per child (the child's owner); once guards the close so
// a duplicate call can never panic on a double close.
//
// It takes the child's own Wait method rather than the child itself: the
// caller's child is an *exec.Cmd (Wait() error) while os.Process.Wait returns
// (*ProcessState, error) — the seam keeps this file free of the exec
// dependency and both callers pass their own correct wait.
func (e *childExit) wait(waitFn func() error) {
	werr := waitFn()
	e.once.Do(func() {
		e.werr = werr
		close(e.ch)
	})
}

// done is the channel a bounded wait selects on (and the one
// stopChildGracefully polls through waitChildExitGone).
func (e *childExit) done() <-chan struct{} { return e.ch }

// exited reports whether the child has been reaped. It does NOT consume
// anything, so it is safe to ask repeatedly and from several callers.
func (e *childExit) exited() bool {
	select {
	case <-e.ch:
		return true
	default:
		return false
	}
}

// err is the child's Wait() error, meaningful ONLY once exited() is true (nil
// while the child runs would be a claim about an exit that has not happened —
// Rule 15: absent evidence is never a verdict).
func (e *childExit) err() error { return e.werr }

// awaitChildExit is THE bounded wait on a spawned merger child, in one place
// so the resolve and verify paths cannot drift into two shapes of it.
//
// It waits up to cap for the child's exit. When the cap expires it stops the
// child GRACEFULLY (stopChildGracefully: intent → SIGTERM group → bounded grace
// → SIGKILL) and reports what that cost. It ALWAYS RETURNS: the wait and the
// stop are both bounded, and the exit signal is a CLOSE (childExit), so a
// waiter — the caller, the grace probe, or both — can never be stranded on a
// value another waiter already took.
//
// Returns (exitedBeforeCap, outcome, err): whether the child exited on its own
// inside the cap, the stop outcome label ("" when it did), and the child's wait
// error (nil when the child has not exited at all — an exit status that does
// not exist yet is never invented, Rule 15).
func awaitChildExit(exit *childExit, child *os.Process, kind, reason string, cap time.Duration) (bool, string, error) {
	select {
	case <-exit.done():
		return true, "", exit.err()
	case <-time.After(cap):
	}
	outcome := stopChildGracefully(child, child.Pid, kind, reason, waitChildExitGone(exit.done()))
	// The exit status is read ONLY once the child is provably reaped: the
	// close is what publishes it, and reading it while the waiter goroutine may
	// still be writing would be reading an unfinished record.
	if exit.exited() {
		return false, outcome, exit.err()
	}
	return false, outcome, nil
}

// childStopGrace is how long a child is given to wind down on SIGTERM before
// the escalation to SIGKILL. Named, and SHORT: the caller reached this path
// because its own bounded wait already expired, so the child has no budget
// left — but the difference between 0 (yesterday's SIGKILL) and a few seconds
// is the entire close-out chain (session_end + ledger row writeback), and that
// chain only needs to be *entered*, not completed, for the row to stop being a
// mystery.
const childStopGrace = 5 * time.Second

// childStopPoll is the poll interval while waiting for the child to die.
const childStopPoll = 100 * time.Millisecond

// stopChildGracefully terminates a child (and its process group when it leads
// one) with provenance and a graceful-then-forced sequence.
//
// groupPID is the pid to signal NEGATIVELY (the group id) when the child is a
// group leader; pass 0 when the child is not a group leader, in which case only
// the pid itself is signalled.
//
// kind/reason name WHY (they land verbatim in the provenance record and in the
// caller's report). waitGone is called between the SIGTERM and the SIGKILL —
// the caller's own "has it exited?" probe (typically a channel receive or a
// Wait), so this helper never invents a second wait mechanism.
//
// Returns the escalation label for the caller's report: stopped | killed.
func stopChildGracefully(child *os.Process, groupPID int, kind, reason string, waitGone func(time.Duration) bool) string {
	if child == nil {
		return childStopStopped
	}
	// 0. Nothing to stop? Then record NOTHING and signal NOTHING. This is the
	// common race on a bounded wait — the child exits in the millisecond
	// between the cap firing and this call — and an intent line written for a
	// process that was already gone would be a false record ("the merger
	// killed it") in the one file post-mortems are read from.
	if !childAlive(child.Pid) {
		return childStopStopped
	}
	// 1. Intent FIRST: a SIGKILL leaves the victim no chance to record
	// anything, so this line is the only evidence that will exist.
	if err := signalrec.Intent(kind, reason, child.Pid, groupPID); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] %s: could not record kill intent for pid %d: %v\n", kind, child.Pid, err)
	}
	// 2. Graceful: the group when the child leads one (a build/test grandchild
	// must come along), else the bare pid.
	signalChild(child.Pid, groupPID, syscall.SIGTERM)
	if waitGone != nil && waitGone(childStopGrace) {
		return childStopStopped
	}
	// 3. Forced: the grace expired. This is the escalation the caller's cap
	// always permitted — it is now a LAST resort rather than the first move.
	signalChild(child.Pid, groupPID, syscall.SIGKILL)
	if waitGone != nil {
		waitGone(childStopGrace)
	}
	return childStopKilled
}

// childStopStopped / childStopKilled are the two outcome labels.
const (
	childStopStopped = "stopped gracefully (SIGTERM)"
	childStopKilled  = "killed (SIGKILL after the grace expired)"
)

// childAlive reports whether the pid still exists. Signal 0 performs the
// permission/existence check without delivering anything. A ZOMBIE still
// answers (it exists until reaped), which is the honest answer here: the stop
// path is about a process that has not been waited on.
func childAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// signalChild delivers sig to a child's process group (preferred: it is what
// reaps grandchildren) or to the bare pid when there is no group.
func signalChild(pid, groupPID int, sig syscall.Signal) {
	target := pid
	if groupPID > 0 && groupPID == pid {
		target = -pid
	}
	//stylegate:ignore best-effort stop of a child we are abandoning; ESRCH
	// (already gone) is the expected benign error and the caller's wait is the
	// real check.
	if err := syscall.Kill(target, sig); err != nil && target != pid {
		_ = syscall.Kill(pid, sig) //stylegate:ignore pid fallback after a failed group signal
	}
}

// signalChildOnly delivers sig to the BARE pid — never to a process group.
//
// It exists for one caller shape (the daemon launcher's failed-start cleanup,
// cli_daemon_spawn.go): a process that has been moved into ITS OWN cgroup is
// frequently NOT a process-group leader of anything we own — it may even share
// a session with an unrelated process that landed in the same scope, so a
// negative-pid signal would reach a bystander. The explicit pid is the only
// target that is provably ours. Still never a pattern (04:12 incident).
func signalChildOnly(pid int, sig syscall.Signal) {
	//stylegate:ignore best-effort stop of a child whose start already failed;
	// ESRCH is the expected benign error and the caller's bounded wait is the
	// real check.
	_ = syscall.Kill(pid, sig)
}

// waitChildExitGone adapts an exit channel into the predicate
// stopChildGracefully expects: true when the child's waiter reported within
// budget.
func waitChildExitGone(done <-chan struct{}) func(time.Duration) bool {
	return func(budget time.Duration) bool {
		select {
		case <-done:
			return true
		case <-time.After(budget):
			return false
		}
	}
}
