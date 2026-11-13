package testutil

// The RED proofs for the bounded fixture subprocess (runfixture.go).
//
// What must be provable, and is, below:
//
//	① a child that never returns is KILLED at its budget — the call returns an
//	   error that NAMES the budget, carrying the output captured so far, and it
//	   returns in budget time (not in the child's time);
//	② a child that holds the inherited output pipe open through a GRANDCHILD is
//	   still reaped promptly (the WaitDelay arm) — a `sh -c "sleep … &"` that
//	   outlives its parent must not pin the fixture's own Wait forever;
//	③ the healthy arm is unchanged: a fast child returns its output and a nil
//	   error.
//
// The budgets here are deliberately small (see the constants below) — the arm
// under test is the BOUND, and paying its real 90s to prove it would be a slow
// test that proves the same thing.

import (
	"strings"
	"testing"
	"time"
)

const (
	// fixtureRedProofBudget is the tiny budget the killed-child arm runs under.
	fixtureRedProofBudget = 200 * time.Millisecond
	// fixtureRedProofSlack is how much longer than its budget a bounded call may
	// take before the bound itself is broken (process start, kill, reap).
	fixtureRedProofSlack = 5 * time.Second
	// fixtureRedProofChild is a child that would outlive any budget by far.
	fixtureRedProofChild = "echo started; sleep 30"
	// fixtureRedProofGrandchild leaves a background process holding the
	// inherited stdout pipe, then exits — the pipe-holder case.
	fixtureRedProofGrandchild = "sleep 30 & echo started"
	// fixtureHealthyChild finishes immediately.
	fixtureHealthyChild = "echo ok"
)

// TestFixtureCommandKillsAChildThatOverrunsItsBudget is proof ①.
func TestFixtureCommandKillsAChildThatOverrunsItsBudget(t *testing.T) {
	started := time.Now()
	out, err := runFixtureCommand("/bin/sh", nil, []string{"-c", fixtureRedProofChild}, fixtureRedProofBudget)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatalf("a child that sleeps 30s under a %s budget returned no error (out=%q) — an unbounded fixture "+
			"child is exactly the defect this replaces", fixtureRedProofBudget, out)
	}
	if !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), fixtureRedProofBudget.String()) {
		t.Errorf("the budget error must name the budget (Rule 15: a killed child is reported, never silent); got: %v", err)
	}
	if !strings.Contains(string(out), "started") {
		t.Errorf("the output captured before the kill is lost (%q) — a timeout that hides the child's own words "+
			"leaves the next reader with nothing", out)
	}
	if elapsed > fixtureRedProofBudget+fixtureCommandWaitDelay+fixtureRedProofSlack {
		t.Errorf("the call took %s under a %s budget — the bound did not hold (budget + WaitDelay + slack)", elapsed, fixtureRedProofBudget)
	}
}

// TestFixtureCommandReapsAChildWhoseGrandchildHoldsThePipe is proof ②.
func TestFixtureCommandReapsAChildWhoseGrandchildHoldsThePipe(t *testing.T) {
	started := time.Now()
	out, err := runFixtureCommand("/bin/sh", nil, []string{"-c", fixtureRedProofGrandchild}, fixtureRedProofBudget)
	elapsed := time.Since(started)
	if elapsed > fixtureRedProofBudget+fixtureRedProofSlack {
		t.Errorf("a grandchild holding the inherited pipe pinned the call for %s under a %s budget "+
			"(WaitDelay should have stopped waiting for it)", elapsed, fixtureRedProofBudget)
	}
	if !strings.Contains(string(out), "started") {
		t.Errorf("the captured output is missing the child's own line: %q (err=%v)", out, err)
	}
}

// TestFixtureCommandReturnsAHealthyChildsOutput is proof ③ — the arm that shows
// the bound did not change what a working fixture sees.
func TestFixtureCommandReturnsAHealthyChildsOutput(t *testing.T) {
	out, err := FixtureCommand("/bin/sh", "-c", fixtureHealthyChild)
	if err != nil {
		t.Fatalf("a healthy child failed: %v (out=%q)", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "ok" {
		t.Errorf("output = %q, want %q", got, "ok")
	}
}

// TestFixtureCommandNamesAFailingChild pins the second half of Rule 15: a child
// that exits non-zero is reported WITH its argv and its output, so the caller's
// failure names what ran.
func TestFixtureCommandNamesAFailingChild(t *testing.T) {
	out, err := FixtureCommand("/bin/sh", "-c", "echo boom; exit 3")
	if err == nil {
		t.Fatal("a child exiting 3 returned no error")
	}
	if !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(string(out), "boom") {
		t.Errorf("the error must carry the exit status and the output must be returned; err=%v out=%q", err, out)
	}
}
