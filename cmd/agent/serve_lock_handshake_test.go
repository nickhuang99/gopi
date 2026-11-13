package main

// serve_lock_handshake_test.go — the Rule 5 regression guards for the
// child-process lock holder's readiness handshake (cli_serve_test.go).
//
// THE DEFECT THESE GUARD (measured 2026-10-12 on this host). The handshake
// waited a FIXED 100 × 50ms = 5.00s and then said "helper never signalled
// readiness". Measured on an idle machine, the holder needs 5.25–6.25s to reach
// readiness — the cost is the test BINARY's own suite startup (testutil's
// pins + close-out sweeps; `-test.run='^$'` alone measures 5.02s), paid by the
// helper before its body runs. So the budget sat BELOW its own fixture's floor:
// 1 FAIL / 5 runs measured. The parent's sentence named its own budget and said
// nothing about the child, so a BROKEN fixture (a child that died taking the
// flock with it) and a SLOW one were the same string.
//
// THREE PROPERTIES, one test each:
//  1. the budget is DERIVED (deadline + measured startup), not typed in;
//  2. a child that dies before readiness is reported with ITS OWN stderr and
//     exit status, immediately — never as "never signalled readiness";
//  3. an empty capture is NAMED as empty, so a verdict can never read as a
//     swallowed error.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// recordedFailure is the holderTestSink of the regression case: it RECORDS the
// failure message the handshake produced, so the case can assert on the exact
// text a real *testing.T would print and abort with. It keeps t.Fatalf's
// contract (the case is over) without hard-coding *testing.T.
type recordedFailure struct {
	fired   bool
	message string
}

func (r *recordedFailure) Helper() {}

func (r *recordedFailure) Fatalf(format string, args ...any) {
	r.fired = true
	r.message = fmt.Sprintf(format, args...)
}

// TestServeLockHolderWaitBudget_TracksTheDeadlineAndTheMeasuredStartup pins the
// adaptive budget as a PURE function, which is the half of the fix a fixture
// cannot exercise: what the window IS, given a deadline and a measured startup.
//
// The property under test is the one the fixed constant lacked — the budget is
// a FUNCTION of the run's own configuration and of this machine, so a loaded
// host widens it instead of turning the test red, while a budget can never grow
// to the point of swallowing the test's own deadline.
func TestServeLockHolderWaitBudget_TracksTheDeadlineAndTheMeasuredStartup(t *testing.T) {
	const deadline = 10 * time.Minute

	// No sample measured yet: the floor, never a typed-in window.
	if got := serveLockHolderWaitBudget(deadline, 0, 0); got != serveLockHolderMinWait {
		t.Errorf("with no sample the budget must be the floor %s, got %s", serveLockHolderMinWait, got)
	}

	// A MEASURED, slow startup widens it by the named multiple.
	slow := 20 * time.Second
	wantScaled := slow * serveLockHolderSampleMultiple
	if got := serveLockHolderWaitBudget(deadline, 0, slow); got != wantScaled {
		t.Errorf("a %s measured startup must widen the budget to %s, got %s", slow, wantScaled, got)
	}

	// A fast sample must NOT shrink it below the floor: the budget is about a
	// loaded host, and one quick spawn is not evidence the next one will be.
	if got := serveLockHolderWaitBudget(deadline, 0, time.Millisecond); got != serveLockHolderMinWait {
		t.Errorf("a fast sample must not shrink the budget below the floor %s, got %s", serveLockHolderMinWait, got)
	}

	// The cap: near the end of the test the budget is what is LEFT of the
	// deadline, minus the reserve — so THIS handshake fails naming the child
	// instead of Go's -test.timeout panicking over the whole binary.
	const left = 10 * time.Second
	if got := serveLockHolderWaitBudget(deadline, deadline-serveLockHolderDeadlineReserve-left, slow); got != left {
		t.Errorf("with %s of the deadline left the budget must be %s, got %s", left, left, got)
	}

	// Past the deadline the budget collapses to the poll tick rather than going
	// negative — a negative window would skip the loop entirely and mislabel a
	// LIVE child as one that died.
	if got := serveLockHolderWaitBudget(deadline, deadline, 0); got != serveLockHolderWaitTick {
		t.Errorf("past the deadline the budget must collapse to the tick %s, got %s", serveLockHolderWaitTick, got)
	}
}

// TestServeLockHolderHandshake_ReportsAChildThatDiesBeforeReadiness is the
// minimal reproduction of the unfalsifiable failure: the holder takes the flock
// and then dies, in the child, BEFORE signalling readiness. Pre-fix this
// produced "helper never signalled readiness" after the whole budget — the
// child's exit status and stderr nowhere in it — so the fixture's breakage was
// invisible and the sentence blamed the host.
//
// Post-fix the handshake reports the child's OWN words and status, at once.
func TestServeLockHolderHandshake_ReportsAChildThatDiesBeforeReadiness(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "holder.ready")
	holder := startHolderHandshake(t, holderHandshakeOptions{
		lockPath:        filepath.Join(dir, serveLockFilename),
		readyPath:       ready,
		failBeforeReady: true,
	})
	t.Cleanup(func() { holder.killAndReap(t) })

	rec := &recordedFailure{}
	start := time.Now()
	holder.waitReady(rec, ready)
	elapsed := time.Since(start)

	if !rec.fired {
		t.Fatalf("the handshake must FAIL for a child that died before readiness; it returned after %s", elapsed)
	}
	msg := rec.message

	// 1. The child's own error, verbatim, is in the failure — this is the half
	//    that was missing (the parent used to report only its own budget).
	if !strings.Contains(msg, serveLockHolderFailStderrWord) {
		t.Errorf("the failure must carry the child's own stderr (%q), got:\n%s", serveLockHolderFailStderrWord, msg)
	}
	// 2. Its exit status, too: a crash is not a timeout.
	if !strings.Contains(msg, strconv.Itoa(serveLockHolderFailExitCode)) {
		t.Errorf("the failure must name the child's exit status %d, got:\n%s", serveLockHolderFailExitCode, msg)
	}
	// 3. And it is reported as an EXIT, not as the pre-fix wording.
	if !strings.Contains(msg, "EXITED") {
		t.Errorf("a dead child must be reported as an exit, got:\n%s", msg)
	}
	if strings.Contains(msg, "never signalled readiness") {
		t.Errorf("the pre-fix wording (which blamed the parent's budget) must be gone, got:\n%s", msg)
	}
	// 4. It must be an EVENT, not a budget: the child dies in milliseconds, and
	//    the point is that the parent notices then — never after a full window.
	//    (This is also what makes a BROKEN READER a deterministic failure: a
	//    child whose stderr nobody drains blocks once the pipe buffer fills and
	//    never exits, so the exit event could not fire at all.)
	if elapsed >= serveLockHolderMinWait {
		t.Errorf("a child that died at once must be reported at once, took %s (budget %s)", elapsed, serveLockHolderMinWait)
	}
	// 5. The fixture really did die BEFORE readiness (the marker never landed),
	//    so the case cannot be passing because the child was fine.
	if _, err := os.Stat(ready); err == nil {
		t.Errorf("the failing fixture must never signal readiness, but %s exists", ready)
	}
}

// TestHolderStderr_EmptyCaptureIsNamedNotSilent pins the wording of an empty
// capture: the failure message must never leave a blank section that reads like
// a swallowed error (Rule 15b). The drain in waitReady exists so this case is
// the exception, not the rule.
func TestHolderStderr_EmptyCaptureIsNamedNotSilent(t *testing.T) {
	s := newHolderStderr()
	if got := s.text(); !strings.Contains(got, "wrote nothing") {
		t.Errorf("an empty capture must say so, got %q", got)
	}
	s.buf = []byte("boom\n")
	if got := s.text(); got != "boom\n" {
		t.Errorf("a non-empty capture must be returned verbatim, got %q", got)
	}

	// The cap is a bound, and hitting it is STATED: a capture that silently
	// dropped the tail would read as "the child said only this" (Rule 15b).
	big := newHolderStderr()
	big.capture(strings.NewReader(strings.Repeat("noise\n", serveLockHolderStderrCap)))
	<-big.done
	if got := big.text(); !strings.Contains(got, "truncated") {
		t.Errorf("a capture past the cap must say it was truncated, got %q", got)
	}

	// And a capture under the cap is not marked.
	small := newHolderStderr()
	small.capture(strings.NewReader("one line\n"))
	<-small.done
	if got := small.text(); strings.Contains(got, "truncated") || got != "one line\n" {
		t.Errorf("a short capture must be returned verbatim and unmarked, got %q", got)
	}
}
