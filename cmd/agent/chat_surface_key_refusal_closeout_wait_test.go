package main

// chat_surface_key_refusal_closeout_wait_test.go — the REFUSAL case's closeout leg
// gets its own minimal repro/guard (Rule 5: every fix ships a test that can fail
// for exactly one reason).
//
// WHAT CHANGED AND WHY THE REFUSAL LEG NEEDED ITS OWN GUARD. The refusal case's
// closeout used to end on a FIXED WALL-CLOCK window of its own
// (keyRefusalWait = 10s: `select { case <-exited: case <-time.After(keyRefusalWait) }`),
// which measures the HOST rather than the session — under a whole-package run the
// run was still working through the turn the key had claimed when the window
// expired, and the case failed with `Elapsed 10.78` (measured 2026-10-13, stream
// run-1791875819-1765160.json) while the SAME tree passed the case alone in 12.085s.
// The leg now decides on the run's OWN progress — no model round served AND no byte
// painted for keyRefusalStallWindow — through the wait the sibling case already made
// drivable (waitForCloseoutProgress, chat_surface_bound_e2e_test.go).
//
// WHY A SECOND WAIT GUARD FILE EXISTS AT ALL, and why it is not the sibling's file
// copied. The sibling's file (chat_surface_bound_closeout_wait_test.go) proves the
// criterion at the PROBE's scale (boundProbeStallWindow = 300ms) for the BOUND case.
// This leg is a different caller with DIFFERENT arguments — its own pair of
// constants, passed by the refusal case body — and the failure mode this file exists
// for is a caller-shaped one: the refusal leg asking the wait for something that is
// not a progress criterion (a stall window that is really a wall clock, or a backstop
// that fires before the window it backstops, which turns the ceiling into the
// criterion). So the assertions below are about THE REFUSAL LEG'S OWN ARGUMENTS, at
// the leg's OWN scale, with no pty, no HTTP mock and no model — nothing is
// re-implemented, the probe type is the sibling file's own.

import (
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/heavytest"
)

// TestKeyRefusalCloseoutWaitFiresOnStallAndIsNotTheLegacyWindow is the REFUSAL leg's
// claim, and it fails for exactly one reason: the closeout it drives ended on
// elapsed time instead of on the run's progress.
//
// The stub run produces NOTHING — the goroutine is alive, no round is served, not a
// byte is painted. That is the shape the old fixed window reported as "the REPL did
// not finish after ctrl-c" no matter why, and it must now end closeoutStalled, ended
// by the STALL WINDOW (not by the ceiling, which is deliberately far larger than the
// window: an implementation that still waited on a clock cannot pass this by
// accident), and the gap it reports must be the real silence.
func TestKeyRefusalCloseoutWaitFiresOnStallAndIsNotTheLegacyWindow(t *testing.T) {
	probe := &closeoutProbe{}
	never := make(chan struct{}) // never closed: the run never leaves

	start := time.Now()
	outcome, longest := waitForCloseoutProgress(never, probe.progress, keyRefusalStallWindow, keyRefusalCeiling, keyRefusalStep, nil)
	elapsed := time.Since(start)

	if outcome != closeoutStalled {
		t.Fatalf("a run that produced nothing for the whole stall window ended the refusal leg's closeout as %v, want closeoutStalled — "+
			"the refusal leg's criterion is progress; a wedged session must still FAIL this case however long the host takes to notice", outcome)
	}
	// The ceiling must not be what ended it: it is the hang detector, and a run
	// reported as stalled because 3 minutes elapsed would be the wall clock again.
	if elapsed >= keyRefusalCeiling {
		t.Fatalf("the closeout ended after %v, which is its ceiling (%v): that proves the ceiling fired, not the progress criterion",
			elapsed, keyRefusalCeiling)
	}
	if elapsed < keyRefusalStallWindow {
		t.Fatalf("the closeout ended after %v, before the stall window (%v) elapsed — a stall cannot be declared before the window it is defined by",
			elapsed, keyRefusalStallWindow)
	}
	if longest < keyRefusalStallWindow {
		t.Fatalf("the longest no-progress gap reported was %v, want at least the stall window %v — the gap is the measurement a reader uses "+
			"to size the window from observed behaviour", longest, keyRefusalStallWindow)
	}
}

// TestKeyRefusalCloseoutWaitsOutASlowButMovingRun is the LOAD-ROBUSTNESS half, and it
// is the property the rework exists for: the refusal case's own mock advances a model
// round only when the REPL asks for one, so under whole-package load the measured
// failing run's round count stood still while the editor kept repainting. A run in
// that shape which takes LONGER than the stall window — but is still moving the whole
// time — must NOT be called stuck.
func TestKeyRefusalCloseoutWaitsOutASlowButMovingRun(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionHostCondition)
	probe := &closeoutProbe{}
	left := make(chan struct{})
	// The run is still painting past the stall window and leaves only after that.
	deadline := keyRefusalStallWindow + keyRefusalStallWindow/2
	painting := make(chan struct{})
	go func() {
		defer close(painting)
		stop := time.Now().Add(deadline)
		for time.Now().Before(stop) {
			// ONLY THE PAINT MOVES — the measured failing run's exact shape (the
			// served-round count stood still while the editor repainted).
			probe.paint(1)
			time.Sleep(boundProbeProgressStep) //stylegate:allow-sleep paces the stub run's paint so the wait sees it as continuously progressing
		}
		probe.serve(1)
		close(left)
	}()

	start := time.Now()
	outcome, _ := waitForCloseoutProgress(left, probe.progress, keyRefusalStallWindow, keyRefusalCeiling, keyRefusalStep, nil)
	elapsed := time.Since(start)
	<-painting //stylegate:ignore join the stub run's own goroutine so the case cannot leak it

	if outcome != closeoutLeft {
		t.Fatalf("a run that kept painting for %v — longer than the %v stall window — ended the closeout as %v, want closeoutLeft: that is the "+
			"criterion being a wall clock again, which is the defect this guard exists for", deadline, keyRefusalStallWindow, outcome)
	}
	if elapsed < keyRefusalStallWindow {
		t.Fatalf("the closeout returned after %v, before the stall window %v had passed, so it cannot have been watching a moving run",
			elapsed, keyRefusalStallWindow)
	}
}

// TestKeyRefusalCloseoutBackstopOutlivesItsStallWindow pins the one argument relation
// the wait's meaning depends on, cheaply and without sleeping: the stall window is the
// CRITERION and the ceiling is the BACKSTOP, so a ceiling that is not strictly larger
// would fire first and quietly become the criterion — a wall clock wearing a progress
// criterion's name, which is the whole defect. It also asserts the window is strictly
// below the leg's OWN legacy wall clock (keyRefusalWait): the window is a pass/fail
// bound on silence, and it must be a bound a working run can clear, not the old window
// restated.
func TestKeyRefusalCloseoutBackstopOutlivesItsStallWindow(t *testing.T) {
	if keyRefusalStallWindow <= 0 {
		t.Fatalf("keyRefusalStallWindow = %v: a progress criterion needs a positive window", keyRefusalStallWindow)
	}
	if keyRefusalCeiling <= keyRefusalStallWindow {
		t.Fatalf("keyRefusalCeiling = %v is not larger than keyRefusalStallWindow = %v: the hang detector would fire before the criterion, "+
			"which makes elapsed time — not progress — the thing that ends the refusal leg's closeout",
			keyRefusalCeiling, keyRefusalStallWindow)
	}
	if keyRefusalStallWindow >= keyRefusalWait {
		t.Fatalf("keyRefusalStallWindow = %v is not below the fixed window it replaced (keyRefusalWait = %v): the progress criterion must be a "+
			"tighter bound on SILENCE than the wall clock it replaced, or the leg has only renamed the defect",
			keyRefusalStallWindow, keyRefusalWait)
	}
	if keyRefusalStep <= 0 || keyRefusalStep > keyRefusalStallWindow {
		t.Fatalf("keyRefusalStep = %v: the poll step must be positive and inside the stall window %v, or the criterion cannot be read at its own scale",
			keyRefusalStep, keyRefusalStallWindow)
	}
}
