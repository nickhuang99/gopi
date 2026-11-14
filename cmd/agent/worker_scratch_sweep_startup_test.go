package main

// worker_scratch_sweep_startup_test.go — the OTHER half of the sweep's
// placement, and the regression guard for the defect that left a whole tree
// unbounded.
//
// THE TWO FACTS ARE OPPOSITE AND BOTH MUST HOLD:
//
//	worker_scratch_sweep_offboundary_test.go  the sweep is NEVER entered from the
//	                                          ROUND boundary (2026-10-26 ruling:
//	                                          a worker claims, runs, reports).
//	this file                                 the sweep IS entered at START-UP,
//	                                          before the first claim.
//
// WHY THE SECOND FACT NEEDED ITS OWN TEST (measured 2026-10-31, task
// 01a18f45): the removal of the round call left `scratchGCSweepAsync` with NO
// production caller at all, and the test above — correctly — only asserts it is
// not on the round. So the suite was fully green on a tree where nothing in the
// system ever ran the sweep: /home/user/workspace accumulated 119 `go-build*`
// directories / 9.0 GB, growing ~1.4 GB/h, while `gopi tool scratch-gc trees
// --dry-run` reported the predicate healthy (`tree=go-tmp entries=119
// collectable=1` — only 1 had aged past the 24h window, the other 118 sat
// inside it waiting for a caller that did not exist). A test that pins where a
// call must NOT be does not pin that it is called at all; that gap is what this
// file closes.
//
// HOW IT PROVES THE PROPERTY WITHOUT MEASURING A DURATION: it drives the REAL
// loop (runTaskWorker) with the sweep BODY replaced by a stub that BLOCKS until
// the test releases it (scratchGCSweepBody — the seam the sibling test uses),
// and asserts the stub IS entered while the loop is still on its way to the
// first claim. Rule 27 is respected: the stub's cost is fabricated SLEEP of the
// goroutine, never device load.
//
// IT ALSO PINS THE OFF-SWITCH SHAPE: the call is guarded by `!once`, so the
// --once face (the unit/E2E seam, where a worker must not sweep real state —
// Rule 6) must NOT reach it. That half is asserted by the second case, because
// "guard it with !once" is exactly the kind of condition a later author deletes
// while the loop still looks right.

import (
	"sync"
	"testing"
	"time"
)

const (
	// scratchStartupIntervalMin keeps the resident sweep ENABLED at the
	// coarsest cadence (0 would disable it, and then a missing call and a
	// disabled sweep would be indistinguishable).
	scratchStartupIntervalMin = "60"
	// scratchStartupEnterWait bounds how long the stub may take to be reached.
	// Generous on purpose: the property is "it happens", never "in N ms".
	scratchStartupEnterWait = 30 * time.Second
	// scratchStartupLoopWait bounds the observation made WHILE the stub is
	// blocked (the loop reached its drained wait — i.e. got past start-up).
	scratchStartupLoopWait = 30 * time.Second
	// scratchStartupIdleExits must be > 1 so the loop reaches its drained WAIT
	// (the observable moment) instead of taking the direct self-exit arm.
	scratchStartupIdleExits = "3"
	// scratchStartupJoinWait bounds cleanup's JOIN on the stubbed goroutine —
	// signalling without joining leaves a runnable goroutine the suite's
	// close-out sweep reports as a leak.
	scratchStartupJoinWait = 10 * time.Second
)

// TestWorkerStartupReachesTheResidentSweep is the positive half: the loop's
// start-up prologue must ENTER the resident sweep.
func TestWorkerStartupReachesTheResidentSweep(t *testing.T) {
	// repo-context sandbox: the loop releases the round's own worktree under
	// THIS process's cwd, so its pin is the only control.
	SandboxRepoContext(t)
	workerIdleSandbox(t)
	t.Setenv(workerIdleExitsEnv, scratchStartupIdleExits)
	t.Setenv(workerIdleIntervalEnv, "1") // a 1s drained wait, so the round turns over quickly
	t.Setenv("GOPI_TEST_SCRATCH_GC_MIN", scratchStartupIntervalMin)

	// Fresh throttle + latch: a sibling test in this package may have ticked
	// the process-wide clock or left a flight latched.
	scratchGCLastTick = time.Time{}
	resetScratchGCSweepFlight()
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
		// JOIN, don't just signal (see the sibling test's note): a stop that
		// only closes the channel leaves the goroutine runnable.
		select {
		case <-finished:
		case <-time.After(scratchStartupJoinWait):
		}
	})

	stopWritten := make(chan struct{})
	var stopOnce sync.Once
	armWorkerIdleDrainHook(t, func(round, streak int) {
		if round != 1 {
			return
		}
		// The drained wait can be observed more than once for the same round
		// (the loop re-enters it while it waits), so the publish is once-only:
		// a second close would panic the hook rather than measure anything.
		stopOnce.Do(func() {
			workerRequestStop("test: startup sweep reached", false)
			close(stopWritten)
		})
	})

	loopDone := make(chan struct{})
	captureStdout(t, func() {
		go func() {
			defer close(loopDone)
			runTaskWorker(false, "", nil)
		}()

		// ① THE STUB MUST BE ENTERED — this is the claim the sibling test
		// deliberately does NOT make, and its absence was the defect.
		select {
		case <-entered:
			// reached: the start-up prologue called the sweep.
		case <-loopDone:
			t.Fatal("the worker loop returned WITHOUT ever entering the resident scratch sweep — " +
				"nothing in the system runs `gopi tool scratch-gc tick` on a host that only keeps workers, " +
				"so declared trees (go-tmp, raw logs, the build cache) accumulate with no bound " +
				"(measured 2026-10-31: 119 go-build* dirs / 9.0 GB in the TMPDIR tree, predicate healthy, no caller)")
		case <-time.After(scratchStartupEnterWait):
			t.Fatal("the worker loop never entered the resident scratch sweep within the wait — " +
				"the start-up call site in runTaskWorkerLoop is missing or unreachable")
		}

		// ② THE LOOP STILL RUNS: entering the sweep must not mean the loop
		// wedged on it (the sweep is async by construction; a synchronous call
		// site would hang RIGHT HERE, on this assertion).
		select {
		case <-stopWritten:
		case <-time.After(scratchStartupLoopWait):
			t.Fatal("the worker loop never reached its drained wait while the sweep was in flight — " +
				"the start-up sweep is BLOCKING the claim loop (it must go through scratchGCSweepAsync)")
		}
		select {
		case <-loopDone:
		case <-time.After(scratchStartupLoopWait):
			t.Fatal("the worker loop did not return while the sweep was still in flight")
		}
	})
}

// TestWorkerOnceFaceNeverReachesTheResidentSweep is the negative half: the
// `--once` face is the unit/E2E seam, and a test worker must not sweep real
// state (Rule 6). The guard is the `!once` the call site shares with the shot
// reclamation above it.
func TestWorkerOnceFaceNeverReachesTheResidentSweep(t *testing.T) {
	SandboxRepoContext(t)
	workerIdleSandbox(t)
	t.Setenv("GOPI_TEST_SCRATCH_GC_MIN", scratchStartupIntervalMin)

	scratchGCLastTick = time.Time{}
	resetScratchGCSweepFlight()
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState)

	entered := make(chan struct{}, 1)
	prev := scratchGCSweepBody
	scratchGCSweepBody = func(string) {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	t.Cleanup(func() { scratchGCSweepBody = prev })

	captureStdout(t, func() {
		// The --once face: one claim attempt, then it exits on a drained queue.
		runTaskWorker(true, "", nil)
	})

	select {
	case <-entered:
		t.Fatal("the resident scratch sweep was ENTERED from the --once face: that face is the unit/E2E seam, " +
			"and a test worker must not sweep real state (Rule 6) — the start-up call site lost its `!once` guard")
	default:
		// correct: the seam did not sweep.
	}
}
