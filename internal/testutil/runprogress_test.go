package testutil

// runprogress_test.go — the criterion that replaced a fixed wall-clock deadline
// when a fixture waits for a `gopi` child, driven at its own scale.
//
// WHY THESE ARMS AND NOT THE SUBPROCESS (the same discipline
// cmd/agent/chat_surface_bound_closeout_wait_test.go states for its closeout
// wait): the thing under test is a DECISION — "is this run moving, stuck, or
// merely slow?" — so the arms inject the progress reader and the three intervals
// instead of waiting out a real 30s stall window, and each arm FAILS FOR EXACTLY
// ONE REASON.
//
// THE ARMS:
//
//	1. it keeps moving          → must NOT be condemned by elapsed time (the
//	                              measured incident: a busy host is slow, not hung);
//	2. nothing moves            → MUST end RunWaitStalled (a hang still fails);
//	3. moving past the ceiling  → MUST end RunWaitCeiling (the run does not STOP,
//	                              which is a different defect from a hang);
//	4. unreadable /proc         → MUST NOT be read as "scheduled nothing";
//	5. a REAL, genuinely stuck  → MUST end RunWaitStalled with the kernel as the
//	   child (`sleep`, spawned through the reaping helper)   judge — no injected
//	                              reader, no synthetic progress;
//	6. the tree measurement      → sums a synthetic /proc tree's tick columns and
//	                              follows its children file, so a real tree's
//	                              accounting is pinned without a live tree.
//
// Rule 27: nothing here creates load. Arms 1–4 are pure functions of injected
// readings; arm 5 spawns one IDLE child (a `sleep`, which is scheduled for no
// CPU time at all — that is exactly what makes it a hang specimen); arm 6 reads
// a fixture directory tree.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/procname"
)

// The arm scale: every interval below is the FUNCTION's parameter, deliberately
// small so an arm ends in milliseconds. None of them is a production value —
// runNoProgressStall/subprocessTimeout are what RunGopi passes (Rules 13/14:
// those constants are read at their ONE definition site, never re-typed here).
const (
	probeStep        = 2 * time.Millisecond
	probeStallWindow = 60 * time.Millisecond
	probeCeiling     = 400 * time.Millisecond
	// probeMovingCeiling bounds arm 1: the run "moves" the whole time, so the
	// stall window can never fire and only the ceiling (or the exit) ends it.
	probeMovingCeiling = 300 * time.Millisecond
	// probeExitDelay is when arm 1's child "exits" — far past its stall window,
	// which is the point: elapsed time alone must not condemn it.
	probeExitDelay = 150 * time.Millisecond
	// probeHangCeiling bounds arm 5 so a broken implementation cannot hang the
	// suite; it is many stall windows away, so reaching it would be a DIFFERENT
	// verdict than the one asserted.
	probeHangCeiling = 5 * time.Second
)

// TestRunProgressIsNotCondemnedByElapsedTime is arm 1 — the measured incident,
// reduced. A child whose ticks keep advancing must never be reported as stalled,
// no matter how long it has been running relative to the stall window.
func TestRunProgressIsNotCondemnedByElapsedTime(t *testing.T) {
	exited := make(chan struct{})
	go func() {
		// A TIMER, not a sleep: the arm needs "the child exits after this
		// interval", which is a deadline on its own channel (and keeps this
		// fixture clear of the test-sleep rule, whose subject is polling).
		<-time.After(probeExitDelay)
		close(exited)
	}()
	// "Moving" here means the tick counter climbs on every sample — the shape of
	// a run that is working slowly on a busy host.
	var ticks uint64
	progress := func() RunProgressSnapshot {
		ticks++
		return RunProgressSnapshot{Ticks: ticks, Live: 2, Readable: true}
	}
	outcome, elapsed := waitRunProgress(exited, progress, probeStallWindow, probeMovingCeiling, probeStep, nil)
	if outcome != RunWaitLeft {
		t.Fatalf("a run whose tree was scheduled on every sample ended the wait as %v after %v, want RunWaitLeft — elapsed time alone must never condemn a run that is still moving (that is the whole defect: the old wait bounded this fixture with %v of wall clock and nothing else)",
			outcome, elapsed.Round(time.Millisecond), probeStallWindow)
	}
	if elapsed < probeExitDelay {
		t.Fatalf("the wait returned after %v, before the child exited at %v — it ended for a reason other than the exit",
			elapsed.Round(time.Millisecond), probeExitDelay)
	}
}

// TestRunProgressEndsStalledWhenNothingMoves is arm 2 — the assertion that must
// SURVIVE the fix: a run whose tree is scheduled for nothing, for a whole stall
// window, is a hang and must fail.
func TestRunProgressEndsStalledWhenNothingMoves(t *testing.T) {
	frozen := RunProgressSnapshot{Ticks: 42, Live: 3, Readable: true}
	outcome, elapsed := waitRunProgress(nil, func() RunProgressSnapshot { return frozen },
		probeStallWindow, probeCeiling, probeStep, nil)
	if outcome != RunWaitStalled {
		t.Fatalf("a tree that scheduled nothing for the whole stall window ended the wait as %v after %v, want RunWaitStalled — a real hang must still FAIL, and it must be decided by the stall criterion rather than by burning the whole budget",
			outcome, elapsed.Round(time.Millisecond))
	}
	if elapsed > probeCeiling {
		t.Fatalf("the hang verdict took %v, past the ceiling %v — the stall window did not end it", elapsed.Round(time.Millisecond), probeCeiling)
	}
}

// TestRunProgressEndsCeilingWhenItNeverStops is arm 3 — the OTHER failure. A run
// that keeps moving but never terminates is not a hang, and the message must be
// able to say which one happened.
func TestRunProgressEndsCeilingWhenItNeverStops(t *testing.T) {
	var ticks uint64
	progress := func() RunProgressSnapshot {
		ticks++
		return RunProgressSnapshot{Ticks: ticks, Live: 1, Readable: true}
	}
	outcome, elapsed := waitRunProgress(nil, progress, probeStallWindow, probeCeiling, probeStep, nil)
	if outcome != RunWaitCeiling {
		t.Fatalf("a run that never stopped moving and never exited ended the wait as %v after %v, want RunWaitCeiling — a run that does not terminate is a defect, but it is NOT a stall and must not be reported as one",
			outcome, elapsed.Round(time.Millisecond))
	}
	if elapsed < probeCeiling {
		t.Fatalf("the ceiling verdict came at %v, before the ceiling %v — it ended early for a reason the arm did not set up",
			elapsed.Round(time.Millisecond), probeCeiling)
	}
}

// TestRunProgressDoesNotReadUnreadableAsNoProgress is arm 4. "Cannot read
// /proc/<pid>" is not "scheduled nothing": treating the two as the same would
// turn a /proc hiccup, a permission boundary or a pid the fixture lost into a
// hang verdict (Rule 15).
func TestRunProgressDoesNotReadUnreadableAsNoProgress(t *testing.T) {
	blank := RunProgressSnapshot{Readable: false}
	outcome, _ := waitRunProgress(nil, func() RunProgressSnapshot { return blank },
		probeStallWindow, probeCeiling, probeStep, nil)
	if outcome == RunWaitStalled {
		t.Fatalf("an UNREADABLE process tree ended the wait as RunWaitStalled — no measurement was taken, so no progress claim can be made in either direction; the ceiling is what must end this wait")
	}
	if outcome != RunWaitCeiling {
		t.Fatalf("an unreadable tree ended the wait as %v, want RunWaitCeiling", outcome)
	}
}

// TestRunProgressDetectsARealStuckChild is arm 5, with the KERNEL as the judge:
// a genuinely stuck real process (an idle `sleep`, spawned through the reaping
// helper) must end the wait RunWaitStalled. This is the arm that proves the
// criterion is not merely a shape the injected readings agree with.
func TestRunProgressDetectsARealStuckChild(t *testing.T) {
	if _, err := os.Stat(procname.ProcRootDefault); err != nil {
		t.Skipf("no procfs at %s — a progress criterion cannot be measured on this host", procname.ProcRootDefault)
	}
	// Through the reaping helper, never a hand-rolled exec: a fixture that spawns
	// `sleep` directly is the source gate's subject (fixture_spawn_gate_test.go).
	holder := StartMockProcess(t, "sleep", "30")
	pid := holder.Pid()
	outcome, elapsed := waitRunProgress(nil, func() RunProgressSnapshot { return measureRunProgress(pid) },
		probeStallWindow, probeHangCeiling, probeStep, nil)
	if outcome != RunWaitStalled {
		t.Fatalf("a REAL idle process (pid %d) — scheduled for no CPU time at all — ended the wait as %v after %v, want RunWaitStalled. The stall criterion did not see what the kernel reports, so a true hang would not fail",
			pid, outcome, elapsed.Round(time.Millisecond))
	}
	// And the specimen really was measurable: an unreadable tree would have made
	// the arm above pass for the wrong reason.
	if snap := measureRunProgress(pid); !snap.Readable || snap.Live < 1 {
		t.Fatalf("the hang specimen pid %d read back as %s — the arm passed without measuring its subject", pid, runTreeState(snap))
	}
}

// TestMeasureRunProgressFollowsTheTree is arm 6: the measurement itself. A
// synthetic procfs root (fixture files, no processes) pins the two facts the
// signal depends on — the tick columns are SUMMED over the tree, and the walk
// follows each process's children file.
func TestMeasureRunProgressFollowsTheTree(t *testing.T) {
	root := t.TempDir()
	// A three-process tree: 100 → 200, and 200 → 300. utime/stime are fields 14
	// and 15, so each line must carry 13 columns before them.
	writeStat := func(pid int, utime, stime uint64, children ...int) {
		dir := filepath.Join(root, fmt.Sprint(pid))
		threadDir := filepath.Join(dir, procname.TaskName, fmt.Sprint(pid))
		if err := os.MkdirAll(threadDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", threadDir, err)
		}
		// Build the line column by column: every field between the state letter
		// and the first CPU column, then the two columns the reader sums. The
		// count is DERIVED from the shared field numbers (Rules 1/13) rather than
		// padded by a literal, so a renumber in procname cannot leave this
		// fixture agreeing with itself and wrong about the kernel — measured
		// once already: a hand-counted pad put utime in field 13 and the summed
		// total came out 39 instead of 72.
		line := fmt.Sprintf("%d (gopi) S", pid)
		for field := procname.StatFieldState + 1; field < procname.StatFieldUTime; field++ {
			line += " 0"
		}
		line += fmt.Sprintf(" %d %d 0 0 0\n", utime, stime)
		if err := os.WriteFile(filepath.Join(dir, procname.StatName), []byte(line), 0o644); err != nil {
			t.Fatalf("write stat of %d: %v", pid, err)
		}
		kids := ""
		for _, c := range children {
			kids += fmt.Sprintf("%d ", c)
		}
		if err := os.WriteFile(filepath.Join(dir, procname.TaskName, fmt.Sprint(pid), procname.ChildrenName), []byte(kids), 0o644); err != nil {
			t.Fatalf("write children of %d: %v", pid, err)
		}
	}
	writeStat(100, 5, 7, 200)
	writeStat(200, 11, 13, 300)
	writeStat(300, 17, 19)

	snap := measureRunProgressAt(root, 100)
	if !snap.Readable {
		t.Fatalf("the synthetic tree's root was unreadable — the fixture wrote %s", filepath.Join(root, "100", procname.StatName))
	}
	if snap.Live != 3 {
		t.Fatalf("the walk counted %d live processes, want 3 — it did not follow the children file (got %s)", snap.Live, runTreeState(snap))
	}
	const want = uint64(5 + 7 + 11 + 13 + 17 + 19)
	if snap.Ticks != want {
		t.Fatalf("the tree's tick total is %d, want %d — the measurement must SUM utime+stime over the tree, not read the root alone (utime is stat field %d, stime field %d)",
			snap.Ticks, want, procname.StatFieldUTime, procname.StatFieldStime)
	}
	// A root that is not there is UNREADABLE, never a zero reading.
	if gone := measureRunProgressAt(root, 999); gone.Readable || gone.Ticks != 0 {
		t.Fatalf("a tree rooted at a pid with no /proc entry read back as %s, want unreadable", runTreeState(gone))
	}
}

// TestRunProgressReadsThisProcess'sOwnTree keeps the measurement honest against
// the REAL /proc: this process exists, has been scheduled, and its tree is
// readable, so the reading must say so.
func TestRunProgressReadsThisProcessOwnTree(t *testing.T) {
	if _, err := os.Stat(procname.ProcRootDefault); err != nil {
		t.Skipf("no procfs at %s", procname.ProcRootDefault)
	}
	snap := measureRunProgress(os.Getpid())
	if !snap.Readable || snap.Live < 1 || snap.Ticks == 0 {
		t.Fatalf("this test process read back as %s — a live, scheduled process must be readable with non-zero ticks", runTreeState(snap))
	}
}
