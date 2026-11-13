package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// suiteSweepFlightJoinWait bounds the close-out's wait for the resident scratch
// sweep to stop before the goroutine dump is taken. It is a REPORTING bound, not
// a verdict: a sweep that does not finish inside it is sampled anyway and — if
// its frames are in the dump — reported as parked exactly like any other
// goroutine, because a wedged sweep IS a defect. Loosening the sweep instead
// would hide both the false positive and every genuine leak with it.
const suiteSweepFlightJoinWait = 30 * time.Second

// suiteSweepFlightWaitBound bounds every wait in this file's regression test:
// wall-clock room for a scheduler turn, an order of magnitude below anything
// that could be mistaken for the sweep's own cost.
const suiteSweepFlightWaitBound = 10 * time.Second

// suiteSweepFlightSettleInterval is the poll interval between two goroutine
// scans in the green half of the regression test: a goroutine's exit and the
// dump that observes it are not the same instant.
const suiteSweepFlightSettleInterval = 5 * time.Millisecond

// suiteSweepFlightHoldObservation is how long the regression test watches a join
// that must NOT return yet (the sweep body is still held). It is a scheduler
// allowance, not a measurement of any work: the difference under test is
// "blocked on a channel" versus "returned immediately", so any value above a
// couple of scheduler turns distinguishes them.
const suiteSweepFlightHoldObservation = 200 * time.Millisecond

// joinResidentSweepBeforeSampling waits for the asynchronous resident sweep — an
// INSTRUMENT of this process, started by the worker loop's tick, reading /proc —
// to finish before the close-out measures parked goroutines.
//
// WHY THE CLOSE-OUT OWES IT (measured 2026-10-13, task 01a1343): the sweep is
// off the caller's goroutine by design (12-18s of /proc scanning used to sit
// between an operator's --stop and the round boundary that honours it), so at the
// instant the suite ends it can still be RUNNABLE inside
// scratchGCTick → BaseSuiteRunReport → pathHeldByProcesses. Sampling there
// reported:
//
//	suite goroutine sweep: parked repository goroutines: 1
//	  internal/scratch.readProcFile(...)
//	FAIL github.com/nick/gopi/cmd/agent      (a fully GREEN -run set, exit 1)
//
// That frame is not a leak — a resident instrument of a host process is
// withdrawn by the kernel when the process exits. What was missing was the
// ability to WAIT for it, and that is what the tick now publishes
// (scratchGCSweepInFlightDone / joinScratchGCSweepFlight). This function is the
// consumer: the close-out stops the instrument BEFORE it measures, so anything
// left in the dump is work somebody started and never ended.
func joinResidentSweepBeforeSampling() {
	deadline := time.Now().Add(suiteSweepFlightJoinWait)
	for {
		if done := scratchGCSweepInFlightDone(); done != nil {
			select {
			case <-done:
				// A flight ended — and that is NOT yet the end of the story:
				// the tick fires from a worker loop that may still be alive
				// (this package drives the loop IN-PROCESS), so a later round
				// can start the NEXT sweep immediately. Keep waiting until the
				// process is quiet; the bound, not the count of flights, is
				// what ends this loop.
				continue
			case <-time.After(time.Until(deadline)):
				warnSweepStillFlying()
				return
			}
		}
		// No flight is registered — but the goroutine may still be in its final
		// instructions (the latch is cleared by the goroutine itself and the
		// dump can be taken a few instructions later), so the dump is the
		// arbiter: only its absence of the frame means the instrument is gone.
		if len(residentSweepGoroutineBlocks()) == 0 {
			return
		}
		if time.Now().After(deadline) {
			warnSweepStillFlying()
			return
		}
		time.Sleep(suiteSweepFlightSettleInterval) //stylegate:allow-sleep wait for another sweep tick — the retry around it is what makes the observation bounded
	}
}

// warnSweepStillFlying reports a bounded wait that ran out. The sampling that
// follows decides what happens next: if the sweep's frames are in the dump it is
// reported as parked exactly like any other goroutine, because a wedged sweep IS
// a defect — the bound is a REPORTING bound, never an exemption.
func warnSweepStillFlying() {
	fmt.Fprintf(os.Stderr, "suite goroutine sweep: the resident scratch sweep did not stop within %s; sampling anyway "+
		"(a wedged sweep is reported as parked, exactly like any other leaked goroutine)\n", suiteSweepFlightJoinWait)
}

// sweepFlightDumpBytes bounds the goroutine dump taken by
// sweepFlightGoroutines. The flying sweep is one goroutine near the top of the
// list; an unbounded dump of a busy test binary is a lot of text for one
// assertion.
const sweepFlightDumpBytes = 1 << 20

// sweepFlightFrameMarker identifies the asynchronous sweep's GOROUTINE in a
// runtime.Stack dump. The goroutine runs scratchGCSweepAsync's own closure, so
// the closure's frame — and the `created by ...scratchGCSweepAsync` line — carry
// that name. It is the frame the close-out sweep reported, and the frame this
// file's regression test asserts on.
const sweepFlightFrameMarker = "scratchGCSweepAsync"

// sweepFlightGoroutines returns the dump blocks belonging to the asynchronous
// resident sweep. It is the EXISTING scanner (goroutinesRunning,
// chat_boundary_test.go) and not a second counter (Rule 14): that helper already
// answers exactly "which goroutines carry this frame right now".
func sweepFlightGoroutines(t *testing.T) []string {
	t.Helper()
	return goroutinesRunning(t, sweepFlightFrameMarker)
}

// residentSweepGoroutineBlocks is the SAME question with the same marker, for the
// one caller that has no *testing.T — the close-out's bounded wait
// (joinResidentSweepBeforeSampling, testmain_test.go). The frame marker and the
// block rule are shared with sweepFlightGoroutines above; only the *testing.T is
// missing, which is why this is a few lines rather than an injected helper.
func residentSweepGoroutineBlocks() []string {
	buf := make([]byte, sweepFlightDumpBytes)
	n := runtime.Stack(buf, true)
	var out []string
	for _, block := range strings.Split(string(buf[:n]), "\n\n") {
		if strings.Contains(block, sweepFlightFrameMarker) {
			out = append(out, block)
		}
	}
	return out
}

// TestResidentSweepFlightIsJoinable is the regression guard for the close-out
// defect above: a fully green suite reported a parked repository goroutine whose
// frame was this package's OWN asynchronous sweep, mid-flight.
//
// WHAT IT PINS: the tick's flight is JOINABLE — a caller can wait until the
// goroutine has ENDED — and the join is what the close-out uses. The window is
// made deterministic with the package's existing seam (scratchGCSweepBody), so
// the assertion is about the JOIN and not about how long a /proc scan happens to
// take on this host (Rule 27: no load is created and no timing is asserted
// beyond wall-clock bounds that are orders of magnitude away from the work).
//
// THE NEGATIVE CONTROL IS THE POINT: "0 goroutines after the join" proves
// nothing unless the scanner could see the goroutine BEFORE it. Without step ②
// a broken scanner, a misspelled frame marker or a body stub that never ran
// would all pass this test while proving nothing.
func TestResidentSweepFlightIsJoinable(t *testing.T) {
	// This case must not inherit a sibling's flight, and it must not leave one:
	// both directions of the process-wide latch are reset.
	resetScratchGCSweepFlight()
	t.Cleanup(resetScratchGCSweepFlight)

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce, enteredOnce sync.Once
	var bodies int32
	prev := scratchGCSweepBody
	scratchGCSweepBody = func(string) {
		atomic.AddInt32(&bodies, 1)
		enteredOnce.Do(func() { close(entered) })
		<-release
	}
	t.Cleanup(func() {
		scratchGCSweepBody = prev
		releaseOnce.Do(func() { close(release) })
	})

	// ① START: the tick returns the flight's join, and the sweep is off this
	// goroutine (that asynchrony is the whole reason the close-out can sample it
	// in flight — see scratchGCSweepAsync's header).
	join := scratchGCSweepAsync(workerRoleLabel)
	if join == nil {
		t.Fatal("scratchGCSweepAsync returned no join: nothing can wait for the flight, so a close-out sampling it can only report it as parked")
	}
	select {
	case <-entered:
	case <-time.After(suiteSweepFlightWaitBound):
		t.Fatal("the resident sweep body never ran: this test cannot observe a flight that never started")
	}

	// ② NEGATIVE CONTROL: while the sweep is in flight it MUST be visible to the
	// scanner — otherwise the "0 after the join" below is indistinguishable from
	// a scanner that sees nothing at all.
	if running := sweepFlightGoroutines(t); len(running) == 0 {
		t.Fatalf("the in-flight sweep is not visible to the goroutine scanner (%q): the green assertion below would prove nothing", sweepFlightFrameMarker)
	}

	// A second tick while the flight is in progress is a no-op (single-flight),
	// but it is still JOINABLE — the same handle, not a fresh nil.
	second := scratchGCSweepAsync(workerRoleLabel)
	if second == nil {
		t.Fatal("a tick arriving mid-flight returned no join: the join must describe the IN-FLIGHT sweep, not only the one this call started")
	}
	if got := atomic.LoadInt32(&bodies); got != 1 {
		t.Fatalf("the single-flight latch ran %d sweep bodies concurrently; it must run exactly one", got)
	}

	// ③ END: releasing the body lets the flight finish, and the join must not
	// return until the GOROUTINE is gone (the close-out's whole requirement).
	//
	// THE DIFFERENCE IS ASSERTED, NOT AN ABSOLUTE (the guard shape that catches
	// this defect): while the sweep body is still held, `join` must NOT return.
	// An implementation whose "join" is a signal — or a no-op — returns at once
	// and is red HERE, deterministically (a channel that is unblocked is not a
	// timing coincidence). Asserting only "0 goroutines afterwards" would be
	// green on the broken shape too, because the test releases the body itself.
	joinReturned := make(chan struct{})
	go func() {
		defer close(joinReturned)
		join()
	}()
	select {
	case <-joinReturned:
		t.Fatal("the join returned while the sweep body was still running: it does not wait for the flight, so a close-out that samples the process can only report the sweep as parked")
	case <-time.After(suiteSweepFlightHoldObservation):
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-joinReturned:
	case <-time.After(suiteSweepFlightWaitBound):
		t.Fatal("the join did not return after the sweep body finished: the tick is not joinable, so a close-out can only sample it mid-flight")
	}
	second() // the handle a mid-flight tick returned joins the same flight

	// ④ GREEN: after the join there is no sweep goroutine left to report. The
	// scan is retried to its own bound because a goroutine's exit and the dump
	// that observes it are not the same instant; the JOIN is what makes the
	// window bounded rather than open-ended (the close-out does not retry).
	deadline := time.Now().Add(suiteSweepFlightWaitBound)
	for len(sweepFlightGoroutines(t)) > 0 && time.Now().Before(deadline) {
		runtime.Gosched()
		time.Sleep(suiteSweepFlightSettleInterval) //stylegate:allow-sleep a goroutine's exit and the dump that observes it are not the same instant; the JOIN and deadline above bound it
	}
	if running := sweepFlightGoroutines(t); len(running) > 0 {
		t.Fatalf("the resident sweep goroutine is still parked after its join returned (%d block(s)):\n%s",
			len(running), strings.Join(running, "\n"))
	}

	// ⑤ The idle face the close-out relies on: nothing in flight is not a wait.
	resetScratchGCSweepFlight()
	if done := scratchGCSweepInFlightDone(); done != nil {
		t.Fatalf("the idle latch still publishes an in-flight handle (%v): a close-out would wait on a flight that does not exist", done)
	}
	joinedIdle := make(chan struct{})
	go func() {
		defer close(joinedIdle)
		joinScratchGCSweepFlight()
	}()
	select {
	case <-joinedIdle:
	case <-time.After(suiteSweepFlightWaitBound):
		t.Fatal("joining with no sweep in flight blocked: the close-out would hang on an idle process")
	}
}
