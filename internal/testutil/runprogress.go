package testutil

// runProgress — the criterion that replaces "how much wall clock has passed"
// when a fixture waits for a `gopi` subprocess to finish.
//
// THE DEFECT THIS FIXES (measured 2026-10-31, whole-package run #2 of four):
// `TestTaskRunSeamFallsBackToSkeleton` FAILED with
// `gopi [worker 01a11500-…] timed out after 3m0s` (180.04s) on the run whose
// loadavg was at its peak (13.81 at start, 8.33 at end, 32 cores), and the
// identical HEAD passed in 9.271s when the same test ran alone on an idle host.
// Four whole-package runs agreed: only the loaded one was red.
//
// The failing fixture's own stack dump said the child was `state=S` — an
// ORDINARY wait. That letter is the reason this file exists: `S` is what the
// kernel reports BOTH for a process that is deadlocked against a lock it will
// never get AND for one that is merely queued behind other CPU work. It cannot
// distinguish "stuck" from "slow", and RunGopi's old inner deadline did not try
// to: it bounded the wait with `subprocessTimeout` worth of ELAPSED TIME and
// nothing else, so on a busy host it condemned a run that was still moving.
// Two different situations collapsed into one verdict, and the verdict was the
// wrong one for the busy host.
//
// THE CRITERION NOW: a run is judged by whether it is MOVING. The wait polls the
// child's process TREE and asks the kernel's own CPU accounting
// (/proc/<pid>/stat fields 14+15, summed over the tree) whether anything has
// been scheduled since the previous sample. Two facts end the wait early:
// progress (the run is alive and working, however slow the host is) and the
// child's exit (it finished). One fact ends it as a FAILURE: a whole stall
// window with NOTHING scheduled anywhere in the tree — that is a run that is not
// moving, and a real hang must still redden. The wall clock survives only as the
// CEILING (subprocessTimeout, unchanged): a run that keeps making progress but
// never terminates is still a defect, and it is reported as its own verdict so
// the two failures never borrow each other's explanation.
//
// Rule 27 compliance: nothing here creates load. The progress signal is read
// FROM the child, and the whole mechanism is exercised by injecting a progress
// reader into waitRunProgress (see runprogress_test.go) — no device pressure, no
// parallel suite, no synthetic contention.

import (
	"strconv"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// runNoProgressStall is how long the child's TREE may go with nothing
	// scheduled at all before the wait is declared a hang. It is the stall
	// criterion, and it is deliberately far shorter than the ceiling: a genuine
	// deadlock is decided in this window instead of costing the full budget, so
	// the suite's red arrives sooner AND for the right reason.
	//
	// The window is a fixed INTERVAL, not a fraction of the budget: a value
	// derived from subprocessTimeout would silently change meaning whenever the
	// budget was retuned (the same trap chat_surface_bound_e2e_test.go records
	// for its progress note pacing).
	runNoProgressStall = 30 * time.Second
	// runProgressStep is how often the tree is sampled. It is the fixture's
	// existing poll interval, referenced rather than re-typed (Rules 13/14):
	// sampling and the rest of the fixture's waits are paced by one number.
	runProgressStep = procPollInterval
	// runKillGrace bounds how long the fixture waits for the child to die after
	// the wait decides it never will. SIGKILL cannot be ignored, so this is only
	// the kernel's teardown latency; a child still present after it is reported
	// rather than silently dropped (Rule 15).
	runKillGrace = 5 * time.Second
	// runProgressDescendLevels bounds the descendant walk. A `gopi worker` is two
	// or three levels deep (worker → executor child → its own child); the bound
	// exists so a pathological tree cannot turn one sample into an unbounded
	// walk, and it is generous enough that a real tree is fully counted.
	runProgressDescendLevels = 8
)

// RunProgressSnapshot is ONE reading of a process tree: the CPU time the tree has
// been scheduled for, how many of its processes are alive, and whether the
// reading could be taken at all.
type RunProgressSnapshot struct {
	// Ticks is the summed utime+stime of the root and every descendant it can
	// reach, in clock ticks (procname.ClockTicksPerSecond). It is a COUNTER, so
	// two readings separated by an interval answer "did anything in this tree get
	// scheduled in between?" (a single reading cannot).
	Ticks uint64
	// Live counts the tree's processes that were readable in this sample — the
	// root plus its descendants. A tree that shrinks is not progress, but a tree
	// that grows IS (a child was forked, i.e. the run moved on to its next step),
	// which is why the count is part of the signal.
	Live int
	// Readable is false when the root could not be read at all. It is
	// deliberately NOT a zero reading: "unreadable" must never be mistaken for
	// "scheduled nothing" (Rule 15), because that would turn a /proc hiccup into
	// a hang verdict.
	Readable bool
}

// runProgressAdvanced reports whether b shows the tree moving relative to a.
//
// An UNREADABLE pair is "cannot answer", never "no progress": the wait keeps
// going and the ceiling — not the stall window — is what ends it. This is the
// one place the distinction lives, so no caller can get it wrong.
func runProgressAdvanced(a, b RunProgressSnapshot) bool {
	if !a.Readable || !b.Readable {
		return true
	}
	return b.Ticks != a.Ticks || b.Live != a.Live
}

// measureRunProgress samples the tree under root on the host's procfs.
func measureRunProgress(root int) RunProgressSnapshot {
	return measureRunProgressAt(procname.ProcRootDefault, root)
}

// measureRunProgressAt is measureRunProgress against an explicit procfs root —
// the same function, so a fixture can drive it over a synthetic /proc tree
// without spawning anything (Rule 27).
func measureRunProgressAt(procRoot string, root int) RunProgressSnapshot {
	if root <= 0 {
		return RunProgressSnapshot{}
	}
	snap := RunProgressSnapshot{}
	level := []int{root}
	seen := make(map[int]bool, runProgressDescendLevels)
	for depth := 0; depth < runProgressDescendLevels && len(level) > 0; depth++ {
		next := make([]int, 0, len(level))
		for _, pid := range level {
			if seen[pid] {
				continue
			}
			seen[pid] = true
			pidStr := strconv.Itoa(pid)
			ticks, ok := procname.CPUTicks(procRoot, pidStr)
			if !ok {
				continue
			}
			if pid == root {
				snap.Readable = true
			}
			snap.Ticks += ticks
			snap.Live++
			next = append(next, procname.Children(procRoot, pidStr)...)
		}
		level = next
	}
	return snap
}

// RunWaitOutcome says WHY the wait for a child ended — the three exits are the
// whole contract, and each reddens with its own diagnosis.
type RunWaitOutcome int

const (
	// RunWaitLeft: the child exited. Its exit code is the caller's business.
	RunWaitLeft RunWaitOutcome = iota
	// RunWaitStalled: nothing in the child's tree was scheduled for the whole
	// stall window. This is the HANG verdict, and it is a real failure.
	RunWaitStalled
	// RunWaitCeiling: the tree was still moving but the run did not terminate
	// within the ceiling. A different defect from a hang (the run does not STOP),
	// reported as its own verdict.
	RunWaitCeiling
)

// String renders the outcome for a failure message.
func (o RunWaitOutcome) String() string {
	switch o {
	case RunWaitLeft:
		return "left"
	case RunWaitStalled:
		return "stalled"
	case RunWaitCeiling:
		return "ceiling"
	}
	return "outcome(" + strconv.Itoa(int(o)) + ")"
}

// RunProgressNote is called while the wait is still running, once per
// noteInterval, so a log shows WHAT the child kept doing up to the moment it
// either left or stopped.
type RunProgressNote func(elapsed, since time.Duration, snap RunProgressSnapshot)

// waitRunProgress waits for exited, ending on the FIRST of: the child exiting,
// a whole stall window with nothing scheduled in its tree, or the ceiling.
//
// The signature takes the progress reader and the three intervals as parameters
// so the criterion itself is drivable without a subprocess (runprogress_test.go):
// the arms that matter are "it keeps moving, so it must NOT be condemned by
// elapsed time" and "nothing moves, so it MUST be".
func waitRunProgress(exited <-chan struct{}, progress func() RunProgressSnapshot, stall, ceiling, step time.Duration, note RunProgressNote) (RunWaitOutcome, time.Duration) {
	start := time.Now()
	last := progress()
	lastChange := start
	nextNote := start.Add(stall)
	for {
		select {
		case <-exited:
			return RunWaitLeft, time.Since(start)
		default:
		}
		now := time.Now()
		current := progress()
		if runProgressAdvanced(last, current) {
			last, lastChange = current, now
		} else if now.Sub(lastChange) >= stall {
			return RunWaitStalled, time.Since(start)
		}
		if now.Sub(start) >= ceiling {
			return RunWaitCeiling, time.Since(start)
		}
		if note != nil && !now.Before(nextNote) {
			note(now.Sub(start), now.Sub(lastChange), current)
			nextNote = now.Add(stall)
		}
		time.Sleep(step)
	}
}

// runTreeState renders one snapshot for a failure message: the numbers, so the
// reader can tell "never scheduled" from "scheduled the whole time".
func runTreeState(snap RunProgressSnapshot) string {
	if !snap.Readable {
		return "the child's /proc entry could not be read at all (so no progress claim is made either way)"
	}
	return "tree cpu ticks=" + strconv.FormatUint(snap.Ticks, 10) + " processes=" + strconv.Itoa(snap.Live)
}
