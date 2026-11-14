// serve_lock_diagnosis_budget_test.go — the two halves of the fix that a
// fixture cannot exercise on its own.
//
// serve_lock_diagnosis_liveness_test.go used to wait a FIXED 100 × 25ms = 2.50s
// for its re-exec'd helper's readiness marker and then fail with "the
// lock-holder helper never published its pids". Measured 2026-10-12: that
// helper — this very test binary, re-exec'd — needs ~5.25-6.25s to reach its
// body on an IDLE machine, because it pays the suite's whole TestMain startup
// (testutil's pins plus the close-out sweeps), which `agent.test
// -test.run='^$'` measures at 5.02s. The window sat BELOW its own fixture's
// floor: 2 FAIL / 3 runs under load. And the parent's sentence named only its
// own budget, so a helper that DIED taking the flock and one that was merely
// SLOW were the same string — a broken fixture would have been "fixed" by
// widening the window.
//
// THREE PROPERTIES, one test each:
//  1. the window is DERIVED (the run's deadline, this run's own measured suite
//     startup, and a startup sample an earlier handshake measured), never typed
//     in below the fixture's floor;
//  2. a helper that dies before readiness is reported with ITS OWN stderr and
//     exit status, at once — never as "never published its pids";
//  3. an empty capture is NAMED as empty, so a verdict cannot read as a
//     swallowed error (Rule 15b).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/heavytest"
)

// diagnosisRecordedFailure is the diagnosisTestSink of the regression case: it
// RECORDS the message the handshake produced, so the case can assert on the
// exact text a real *testing.T would print. It keeps Fatalf's contract (the
// case is over) without hard-coding *testing.T — and it is why the handshake's
// exit arm RETURNS after Fatalf: a recording sink does not end the goroutine, so
// without that return the loop keeps polling and the later timeout verdict
// overwrites the correct exit message.
type diagnosisRecordedFailure struct {
	fired   bool
	message string
}

func (r *diagnosisRecordedFailure) Helper() {}

func (r *diagnosisRecordedFailure) Fatalf(format string, args ...any) {
	r.fired = true
	r.message = fmt.Sprintf(format, args...)
}

// TestServeLockDiagnosisReadyBudget_DerivesFromTheRunAndTheMachine pins the
// window as a PURE function: what it IS, given the run's deadline, this run's
// measured suite startup, and a measured startup sample.
//
// The property the fixed constant lacked is that the window is a FUNCTION of
// the run's own configuration and of this machine — so a loaded host widens it
// instead of turning the case red — while it can never grow to swallow the
// run's own deadline.
func TestServeLockDiagnosisReadyBudget_DerivesFromTheRunAndTheMachine(t *testing.T) {
	const deadline = 10 * time.Minute

	// The floor IS the measured fixture floor with room to spare, and it is
	// ABOVE that measured startup cost: a window under it is a race against the
	// binary's own TestMain, which is exactly the defect this replaces.
	measuredFloor := diagnosisMeasuredSuiteStartupFloor * diagnosisReadyFloorMultiple
	if diagnosisReadyFloor != measuredFloor {
		t.Fatalf("the named floor %s must BE diagnosisReadyFloorMultiple × the measured fixture floor %s, got %s",
			diagnosisReadyFloor, diagnosisMeasuredSuiteStartupFloor, measuredFloor)
	}
	if measuredFloor <= diagnosisMeasuredSuiteStartupFloor {
		t.Fatalf("the derived floor %s must exceed the measured suite startup %s — a window below the fixture's own floor is the defect",
			measuredFloor, diagnosisMeasuredSuiteStartupFloor)
	}

	// With no sample measured yet: the floor, never a typed-in window. And the
	// pre-fix window (100 × 25ms) must be BELOW it, so the case cannot silently
	// regress to it.
	preFix := 100 * diagnosisPollTick
	if got := diagnosisReadyBudget(deadline, 0, 0, measuredFloor); got != measuredFloor {
		t.Errorf("with no sample the window must be the floor %s, got %s", measuredFloor, got)
	}
	if preFix >= measuredFloor {
		t.Errorf("the pre-fix window %s must sit BELOW the derived floor %s, else this guard proves nothing", preFix, measuredFloor)
	}

	// A MEASURED, slow startup widens it by the named multiple — the
	// self-calibrating half: the window tracks the machine rather than the host.
	slow := 20 * time.Second
	wantScaled := slow * diagnosisReadySampleMultiple
	if got := diagnosisReadyBudget(deadline, 0, slow, measuredFloor); got != wantScaled {
		t.Errorf("a %s measured startup must widen the window to %s, got %s", slow, wantScaled, got)
	}

	// A fast sample must NOT shrink it below the floor: one quick spawn is not
	// evidence that the next one will be quick on a loaded host.
	if got := diagnosisReadyBudget(deadline, 0, time.Millisecond, measuredFloor); got != measuredFloor {
		t.Errorf("a fast sample must not shrink the window below the floor %s, got %s", measuredFloor, got)
	}

	// The cap: near the end of the run the window is what is LEFT of the
	// deadline minus the reserve — so THIS handshake fails naming the helper
	// instead of Go's -test.timeout panicking over the whole binary.
	const left = 10 * time.Second
	if got := diagnosisReadyBudget(deadline, deadline-diagnosisReadyDeadlineReserve-left, slow, measuredFloor); got != left {
		t.Errorf("with %s of the deadline left the window must be %s, got %s", left, left, got)
	}

	// Past the deadline it collapses to the poll tick rather than going
	// negative — a negative window would skip the loop entirely and mislabel a
	// LIVE helper as one that died.
	if got := diagnosisReadyBudget(deadline, deadline, 0, measuredFloor); got != diagnosisPollTick {
		t.Errorf("past the deadline the window must collapse to the tick %s, got %s", diagnosisPollTick, got)
	}

	// The run's own measurement only GROWS the floor, never undercuts the
	// measured one — so the window can never be typed (or computed) below the
	// cost the helper actually pays.
	if floor := diagnosisFloorNow(); floor < diagnosisMeasuredSuiteStartupFloor {
		t.Errorf("the floor this run derives (%s) must not sit below the measured fixture floor %s", floor, diagnosisMeasuredSuiteStartupFloor)
	}
	if diagnosisSuiteStartup() <= 0 {
		t.Errorf("this run's own suite startup must have been measured by TestMain (diagnosisTestStart is zero)")
	}
}

// TestServeLockDiagnosisHandshake_ReportsAHelperThatDiesBeforeReadiness is the
// minimal reproduction of the unfalsifiable failure: the helper takes the flock
// and then dies, in the CHILD, BEFORE publishing its pids. Pre-fix this produced
// "never published its pids" after the whole window — the child's exit status
// and stderr nowhere in it — so a broken fixture was invisible and the sentence
// blamed the host.
//
// Post-fix the handshake reports the child's OWN words and status, at once.
//
// RED PROOF (Rule 5, recorded in devdoc/serve-lock-diagnosis-budget.md): delete
// the exit event — `go func() { _ = cmd.Wait() }()` instead of
// `go func() { h.exited <- cmd.Wait() }()` — and this case goes red on the exit
// wording (the wait runs out and reports the helper as still RUNNING).
func TestServeLockDiagnosisHandshake_ReportsAHelperThatDiesBeforeReadiness(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	dir := t.TempDir()
	lockPath := filepath.Join(dir, serveLockFilename)
	marker := filepath.Join(dir, "helper.pids")
	const stderrWord = "diagnosis fixture: refusing to publish readiness"
	failEnv := diagnosisFailStderrEnv + "=" + stderrWord
	h := startDiagnosisHelper(t,
		diagnosisModeEnv+"="+diagnosisModeFail,
		diagnosisPathEnv+"="+lockPath,
		diagnosisMarkerEnv+"="+marker,
		failEnv,
	)
	t.Cleanup(h.killAndReap)

	rec := &diagnosisRecordedFailure{}
	start := time.Now()
	diagnosisWaitMarker(rec, h, marker)
	elapsed := time.Since(start)

	if !rec.fired {
		t.Fatalf("the handshake must FAIL for a helper that died before readiness; it returned after %s", elapsed)
	}
	msg := rec.message

	// 1. The child's own words, verbatim: this is the half that was missing —
	//    the parent used to report only its own budget.
	if !strings.Contains(msg, stderrWord) {
		t.Errorf("the failure must carry the helper's own stderr (%q), got:\n%s", stderrWord, msg)
	}
	// 2. Its exit status, too: a crash is not a timeout.
	if !strings.Contains(msg, strconv.Itoa(diagnosisFailExitCode)) {
		t.Errorf("the failure must name the helper's exit status %d, got:\n%s", diagnosisFailExitCode, msg)
	}
	// 3. Reported as an EXIT, and not as the pre-fix wording.
	if !strings.Contains(msg, "EXITED") {
		t.Errorf("a dead helper must be reported as an exit, got:\n%s", msg)
	}
	if strings.Contains(msg, "never published its pids") {
		t.Errorf("the pre-fix wording (which blamed the parent's window) must be gone, got:\n%s", msg)
	}
	// 4. It must be the EVENT arm's verdict, not the window running out. The
	//    discriminator is the WORDING, never a wall clock: how long the child
	//    took to reach its body is the load's business (measured 0.8s idle, >10s
	//    under load 17), and an assertion phrased as "reported within N seconds"
	//    would be the very defect this fix removes, one level down. With the
	//    death event wired the parent says EXITED; without it, the identical
	//    child is reported as "did not publish its pids … within <budget>".
	if !strings.Contains(msg, "EXITED before publishing") {
		t.Errorf("a dead helper must be reported through the EXIT arm, got:\n%s", msg)
	}
	if strings.Contains(msg, "did not publish its pids") {
		t.Errorf("the window ran out over a child that had already died — the death event is not wired, got:\n%s", msg)
	}
	//    (A helper whose stderr nobody drains blocks once the pipe buffer fills
	//    and never exits at all, so the exit report also proves the reader is
	//    wired — that is what the mutation below breaks.)
	t.Logf("the dead helper was reported in %s", elapsed.Round(time.Millisecond))
	// 5. The fixture really did die BEFORE readiness, so the case cannot pass
	//    because the helper was fine all along.
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("the failing fixture must never publish its pids, but %s exists", marker)
	}
}

// TestServeLockDiagnosisStderr_EmptyCaptureIsNamedNotSilent pins the wording of
// an empty capture: a failure message must never leave a blank section that
// reads like a swallowed error (Rule 15b). The drain in the handshake exists so
// this case is the exception, not the rule.
func TestServeLockDiagnosisStderr_EmptyCaptureIsNamedNotSilent(t *testing.T) {
	s := newDiagnosisStderr()
	if got := s.text(); !strings.Contains(got, "wrote nothing") {
		t.Errorf("an empty capture must say so, got %q", got)
	}
	s.buf = []byte("boom\n")
	if got := s.text(); got != "boom\n" {
		t.Errorf("a non-empty capture must be returned verbatim, got %q", got)
	}

	// The cap is a bound, and hitting it is STATED: a capture that silently
	// dropped the tail would read as "the helper said only this" (Rule 15b).
	big := newDiagnosisStderr()
	big.capture(strings.NewReader(strings.Repeat("noise\n", diagnosisStderrCap)))
	<-big.done
	if got := big.text(); !strings.Contains(got, "truncated") {
		t.Errorf("a capture past the cap must say it was truncated, got %q", got)
	}

	// And a capture under the cap is not marked.
	small := newDiagnosisStderr()
	small.capture(strings.NewReader("one line\n"))
	<-small.done
	if got := small.text(); strings.Contains(got, "truncated") || got != "one line\n" {
		t.Errorf("a short capture must be returned verbatim and unmarked, got %q", got)
	}
}
