package main

// worker_scratch_sweep_offboundary_test.go — the regression guard for the epic
// ws6-restart-20s: THE RESIDENT SWEEP MUST NOT SIT ON A ROUND BOUNDARY.
//
// The bug it pins (measured 2026-10-03, load ~13 on 4 cores): the resident
// scratch sweep ran INLINE at the top of every round of the worker's own loop
// (scratchGCSweep). Its /proc scan — every live pid's cwd/fd/exe plus its whole
// /proc/<pid>/maps, bounded at ProcMapsReadLimit = 4 MiB — cost 12-18s per
// firing, CPU-bound (cpu_ticks 1 -> 93 -> 287 -> 505 -> 676 over 5s, ~1.4
// cores; the loop goroutine was caught by SIGQUIT inside
// scratch.readProcFile -> procMapsPaths). The drain request is honoured at the
// round TOP (consumeDrainRequests), so while the sweep ran, no boundary could
// be reached — and `gopi worker --restart --wait 20s` failed with "pid N is
// STILL running 20s after the graceful stop request" on a daemon that had
// ALREADY consumed the request (state read "draining since <t>", rounds 0,
// note "daemon starting"). A disk-hygiene pass was eating an operator's bound.
//
// HOW THIS TEST PROVES THE PROPERTY WITHOUT MEASURING A DURATION: it drives the
// REAL loop (runTaskWorker) with the sweep BODY replaced by a stub that BLOCKS
// until the test releases it (scratchGCSweepRun — a package-level seam), and
// asserts that the loop still reaches its drained wait and returns while that
// stub is in flight. On the pre-fix (synchronous) call site the loop is stuck
// inside the sweep at the very moment it should be waiting, so the second
// assertion times out. It is a POSITIVE proof of "this wait still bites": the
// stub is entered — asserted, never assumed — and nothing about the sweep's own
// cost is fabricated (Rule 27 bans manufacturing device load; the blocking stub
// is the opposite of load — no device is touched).
//
// The stop travels the PRODUCTION path: a durable request file for this pid,
// written from inside the loop's OWN idle-drain observer (the hook fires
// synchronously while the loop is parked at the drained wait), consumed by
// consumeDrainRequests at the next round top — the boundary the sweep used to
// stand in.
//
// THE CLAIM WAS INVERTED ON 2026-10-26, and it got STRONGER (operator ruling: a
// worker claim → run → report, and a resident sweep is not its business). The
// old test asserted "the stub IS reached, and the loop does not wait on it" —
// which pinned the sweep as PRESENT and merely off the caller's goroutine. The
// ruling removed the sweep from the round ENTIRELY, so "is it reached" now has
// the opposite correct answer, and the test would fail on the CORRECT code with
// "the resident sweep was never reached" (measured: exactly that). Deleting it
// would leave the property unpinned in the direction that matters now, so it is
// inverted instead: the stub is armed and must NEVER be entered, while the loop
// still reaches its drained wait and returns. If a future author puts the sweep
// back on this boundary — inline OR asynchronous — the stub is entered and this
// test fails by name.

import (
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// sweepOffBoundaryIntervalMin keeps the resident sweep ENABLED (0 disables
	// it) at the coarsest cadence: the first tick of a fresh process always
	// fires, and the next one is an hour away, so this test sees exactly one
	// sweep — the one whose body it stubs.
	sweepOffBoundaryIntervalMin = "60"
	// sweepOffBoundaryStubEnterWait bounds how long the stub may take to be
	// reached (a load-tolerant bound, not a performance assertion).
	sweepOffBoundaryStubEnterWait = 20 * time.Second
	// sweepOffBoundaryLoopWait bounds the observations made WHILE the stub is
	// blocked: reaching the drained wait, and returning. Generous on purpose —
	// the property is "it does happen", never "it happens in N ms".
	sweepOffBoundaryLoopWait = 30 * time.Second
	// sweepOffBoundaryIdleExits is the idle-round count at which the loop would
	// self-exit. It must be > 1 so the loop reaches its drained WAIT (the
	// observable moment); 0 is unusable here because that arm takes the
	// idle-drained wait directly and never announces the verdict.
	sweepOffBoundaryIdleExits = "3"
	// sweepOffBoundaryStopReason is the reason recorded for the in-process stop
	// this test requests (the operator-facing wording, not a test-only spelling).
	sweepOffBoundaryStopReason = "test: sweep off the claim boundary"
	// sweepOffBoundaryStubJoinWait bounds the cleanup's JOIN on the stubbed
	// sweep goroutine (see the cleanup's "JOIN, don't just signal" note).
	sweepOffBoundaryStubJoinWait = 10 * time.Second
)

// TestWorkerRoundNeverReachesTheResidentSweep is the boundary guard. See the
// file header: it fails BY NAME if the sweep is ever entered from the round,
// and it fails if the loop did not actually run.
func TestWorkerRoundNeverReachesTheResidentSweep(t *testing.T) {
	// repo-context sandbox: the loop runs the round's own-worktree release under
	// THIS process's cwd — the live repository — so its pin is the only control.
	SandboxRepoContext(t)
	workerIdleSandbox(t)
	t.Setenv(workerIdleExitsEnv, sweepOffBoundaryIdleExits)
	t.Setenv(workerIdleIntervalEnv, "1") // a 1s drained wait, so the round turns over quickly
	// ENABLED sweep: this test must observe a firing tick (workerIdleSandbox does
	// not set this knob, so the ambient value could be anything).
	t.Setenv("GOPI_TEST_SCRATCH_GC_MIN", sweepOffBoundaryIntervalMin)

	// Fresh throttle + latch state: a sibling test in this package may already
	// have ticked the process-wide clock or left a sweep latched.
	scratchGCLastTick = time.Time{}
	scratchGCSweepState.mu.Lock()
	scratchGCSweepState.running = false
	scratchGCSweepState.mu.Unlock()
	// The stop record is PROCESS-WIDE, and this test sets it: without a reset a
	// second run (`-count=2`) inherits `stop=true` and the loop returns at its
	// first round top — before the sweep — so the "the stub must be reached"
	// assertion fails on a code path that is actually correct.
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	finished := make(chan struct{})
	var releaseOnce, finishOnce sync.Once
	prev := scratchGCSweepBody
	scratchGCSweepBody = func(string) {
		defer finishOnce.Do(func() { close(finished) })
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	}
	t.Cleanup(func() {
		scratchGCSweepBody = prev
		releaseOnce.Do(func() { close(release) })
		// JOIN, don't just signal: a stop function that only closes the channel
		// leaves the goroutine runnable when the suite's close-out sweep samples
		// the goroutine table, and the sweep counts it as a leak (measured
		// precedent: suite_stall_watchdog_test.go's "closed the channel but did
		// not JOIN" note).
		select {
		case <-finished:
		case <-time.After(sweepOffBoundaryStubJoinWait):
		}
	})

	stopWritten := make(chan struct{})
	armWorkerIdleDrainHook(t, func(round, streak int) {
		if round != 1 {
			return
		}
		// The stop travels the PRODUCTION door for this face. A durable request
		// FILE is consumed by consumeDrainRequests only on the DAEMON face (its
		// first statement is a `writer == nil` return, and an in-process test has
		// no per-pid state writer) — the door an in-process loop owns is the
		// stop flag the loop's own boundary checks. Setting it HERE proves the
		// loop reached its drained wait — i.e. got PAST the round boundary the
		// sweep sat on.
		workerRequestStop(sweepOffBoundaryStopReason, false)
		close(stopWritten)
	})

	loopDone := make(chan struct{})
	out := captureStdout(t, func() {
		go func() {
			defer close(loopDone)
			runTaskWorker(false, "", nil)
		}()

		// ① THE STUB MAY BE ENTERED — AT START-UP, AND ONLY THERE.
		//
		// THIS ASSERTION WAS INVERTED ON 2026-10-31 (task 01a18f45), for the
		// same reason the 2026-10-26 ruling inverted it the first time: the
		// claim has to describe the code that is correct NOW. Removing the round
		// call left the sweep with NO production caller at all, so a tree grew
		// unbounded (119 `go-build*` / 9.0 GB) while the predicate stayed
		// healthy. The repair gives it ONE caller — the loop's start-up
		// prologue, before the first claim — so "the stub is entered" is now
		// the CORRECT answer and the old fatal here would fail on fixed code
		// (measured: exactly that).
		//
		// WHAT THIS TEST STILL UNIQUELY PINS, and the reason it is not deleted:
		// the stub BLOCKS, so if any call site is SYNCHRONOUS — the round
		// boundary, or a start-up call that forgot scratchGCSweepAsync — the
		// loop stops right here and assertions ② and ③ time out by name. That
		// is the whole 2026-10-03 defect (12-18s of /proc scan in front of the
		// round top, eating `--restart --wait 20s`), and it is pinned better by
		// a blocking stub than by the entry assertion ever was: entry with a
		// synchronous caller is precisely the failure, and entry with an async
		// one is now legal.
		//
		// The static half — "no *Tick/*Sweep/*GC/*Reap call sits in the round's
		// dispatch region" — is TestWorkerRoundIsDispatchOnly, which reads the
		// round's own source and cannot be fooled by timing. This dynamic half
		// pins the ASYNC property the scan cannot see.
		select {
		case <-entered:
			// Legal (start-up prologue). The load-bearing assertions are below:
			// a blocking sweep must not stop this loop from reaching its drained
			// wait or from returning.
		case <-time.After(sweepOffBoundaryLoopWait):
			// Also a pass for THIS test's subject (the sweep may already have
			// fired and completed, or a sibling test's throttle left the clock
			// set). ② and ③ below carry the proof either way.
		}

		// ② THE LOOP STILL REACHES ITS DRAINED WAIT AND RECORDS THE STOP —
		// otherwise "the sweep was never entered" would also be true of a loop
		// that never started.
		select {
		case <-stopWritten:
		case <-time.After(sweepOffBoundaryLoopWait):
			t.Fatal("the worker loop never reached its drained wait — the loop under test did not run, so `the sweep was never entered` would prove nothing")
		}

		// ③ AND IT STILL HONOURS THE STOP: the loop returns (the boundary is
		// reachable) while the sweep is still blocked.
		select {
		case <-loopDone:
		case <-time.After(sweepOffBoundaryLoopWait):
			t.Fatal("the worker loop did not return while the resident sweep was still in flight — a round boundary is still waiting on the sweep")
		}
	})

	if !strings.Contains(out, "worker: loop start") {
		t.Errorf("missing loop-start banner — the loop under test did not run:\n%s", out)
	}
	if !strings.Contains(out, workerDrainVerb) && !strings.Contains(out, "draining") {
		t.Errorf("the loop returned without recording the drain: the stop request did not travel the production path:\n%s", out)
	}
}
