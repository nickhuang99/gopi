package main

// chat_surface_bound_closeout_wait_test.go — THE RED PROOF for the closeout wait's
// criterion (Rule 5: every fix ships a minimal repro that can fail for exactly
// one reason).
//
// WHAT CHANGED AND WHY IT NEEDS ITS OWN GUARD. The ctrl-c closeout wait used to
// end on a FIXED WALL-CLOCK window (boundWait = 20s), which measures the host
// rather than the session: on this 4-core host under a whole-package run the run
// was still working through the turn the key had claimed when the window expired
// (measured 2026-10-05, load 16.4: `bye.` landed one poll after the expiry, the
// mock's served-round count having stood still at 46 while the editor kept
// repainting). The wait now ends on a PROGRESS criterion — no model round served
// AND no byte painted for boundCloseoutStallWindow.
//
// A predicate inside a test body that nothing drives is a predicate nobody has
// proven, so the wait is factored out (waitForCloseoutProgress) and driven here
// directly at its own scale: no pty, no HTTP mock, no model. The THREE exits are
// asserted, and the FIRST one is the case's claim for this file: a run that
// STOPS MOVING must still fail, whatever the wall clock says.

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// closeoutProbe drives waitForCloseoutProgress from a stub run whose progress the
// test owns: served/painted are read under a mutex, so a test can hold them STILL
// (a wedged session) or move them (a working one).
type closeoutProbe struct {
	mu      sync.Mutex
	served  int
	painted int
}

func (p *closeoutProbe) progress() (served, painted int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.served, p.painted
}

func (p *closeoutProbe) paint(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.painted += n
}

func (p *closeoutProbe) serve(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.served += n
}

// TestBoundCloseoutWaitFiresWhenTheRunStopsProgressing is the RED PROOF: with the
// run producing NOTHING — the goroutine alive, no round served, no byte painted,
// the shape of the measured failure before the durable tail can be trusted — the
// wait ends closeoutStalled, and it ends there because the PROGRESS predicate
// expired, not because a wall clock did (the ceiling passed in here is far larger
// than the stall window, so an implementation that still waited on the ceiling
// cannot pass this by accident).
func TestBoundCloseoutWaitFiresWhenTheRunStopsProgressing(t *testing.T) {
	probe := &closeoutProbe{}
	stalled := make(chan struct{}) // never closed: the run never leaves
	stall, ceiling := boundProbeStallWindow, 50*boundProbeStallWindow

	start := time.Now()
	outcome, longest := waitForCloseoutProgress(stalled, probe.progress, stall, ceiling, boundStep, nil)
	elapsed := time.Since(start)

	if outcome != closeoutStalled {
		t.Fatalf("a run that produced nothing for the whole stall window ended the wait as %v, want closeoutStalled — the criterion is "+
			"progress; a wedged session must FAIL this case however long the host takes to notice", outcome)
	}
	// The wait must not have been the ceiling in disguise: the stall window is what
	// ended it.
	if elapsed >= ceiling {
		t.Fatalf("the wait ended after %v, which is its ceiling (%v): this proves the ceiling fired, not the progress criterion", elapsed, ceiling)
	}
	if elapsed < stall {
		t.Fatalf("the wait ended after %v, before the stall window (%v) had elapsed — a stall cannot be declared before the window it is defined by", elapsed, stall)
	}
	// The reported gap is the measurement the window is derived from, so it must be
	// the real silence and not a zero.
	if longest < stall {
		t.Fatalf("the longest no-progress gap reported was %v, want at least the stall window %v — the gap is what a caller reads to size "+
			"the window from observed behaviour", longest, stall)
	}
}

// TestBoundCloseoutWaitStaysAliveWhileTheRunKeepsProgressing is the LOAD-ROBUSTNESS
// half, and it is the property the fix exists for: a run that takes far LONGER than
// the stall window — but is still moving the whole time — must NOT be called stuck.
// It is the measured failing run's own shape (rounds stationary, PAINT moving) with
// the wall clock stretched past every bound the old wait had.
func TestBoundCloseoutWaitStaysAliveWhileTheRunKeepsProgressing(t *testing.T) {
	probe := &closeoutProbe{}
	left := make(chan struct{})
	// The run leaves after several stall windows — i.e. long past the point where a
	// fixed window of that length would have declared it stuck.
	deadline := 4 * boundProbeStallWindow
	var done sync.WaitGroup
	done.Add(1)
	go func() {
		defer done.Done()
		stop := time.Now().Add(deadline)
		for time.Now().Before(stop) {
			// ONLY THE PAINT MOVES, which is exactly what the measured failing run
			// did: its served-round count stood still while the editor repainted.
			probe.paint(1)
			time.Sleep(boundProbeProgressStep) //stylegate:allow-sleep paces the stub run's paint so the wait sees it as continuously progressing
		}
		probe.serve(1)
		close(left)
	}()

	start := time.Now()
	outcome, _ := waitForCloseoutProgress(left, probe.progress, boundProbeStallWindow, 50*boundProbeStallWindow, boundStep, nil)
	done.Wait()
	elapsed := time.Since(start)

	if outcome != closeoutLeft {
		t.Fatalf("a run that kept painting for %v (well past the %v stall window) ended the wait as %v, want closeoutLeft — this is the "+
			"criterion being a wall clock again, which is the defect", deadline, boundProbeStallWindow, outcome)
	}
	if elapsed < deadline {
		t.Fatalf("the wait returned before the run left (%v < %v)", elapsed, deadline)
	}
}

// TestBoundCloseoutWaitCeilingCatchesALivelock is the third exit, and it is
// deliberately NOT the claim (see boundCloseoutCeiling): a run that never stops
// moving can never trip the stall window, so it is the ceiling that bounds it —
// otherwise a session repainting forever would HANG cmd/agent instead of failing
// this case, which trades a flake for a hang.
func TestBoundCloseoutWaitCeilingCatchesALivelock(t *testing.T) {
	probe := &closeoutProbe{}
	never := make(chan struct{})
	stop := make(chan struct{})
	stopped := make(chan struct{})
	defer func() { close(stop); <-stopped }()
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			default:
			}
			probe.paint(1)
			time.Sleep(boundProbeProgressStep) //stylegate:allow-sleep paces the livelock's paint so the ceiling is reached by a run that never stops moving
		}
	}()

	stall := boundProbeStallWindow
	ceiling := 3 * boundProbeStallWindow
	var sawProgress atomic.Int64
	outcome, _ := waitForCloseoutProgress(never, func() (int, int) {
		s, p := probe.progress()
		sawProgress.Store(int64(p))
		return s, p
	}, stall, ceiling, boundStep, nil)

	if outcome != closeoutCeiling {
		t.Fatalf("a run that never stopped moving ended the wait as %v, want closeoutCeiling — the ceiling is the livelock detector; without it "+
			"this case would hang the package", outcome)
	}
	if sawProgress.Load() == 0 {
		t.Fatalf("the run never reported a painted byte, so this leg did not exercise a LIVELOCK — it exercised the stall window")
	}
}
