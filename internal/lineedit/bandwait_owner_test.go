package lineedit

import (
	"bytes"
	"testing"
	"time"
)

// bandwait_owner_test.go — the DRAINER'S IDENTITY bound: a wait for the read loop
// must end when the loop that owed the row is GONE, never by spending the whole
// bound on a drainer that will not come.
//
// THE DEFECT (measured 2026-10-12, task 01a12c43, on cmd/agent's voice PTY cases
// under `-cpu=1 -count=40`). `bandWaitBudget` is a bound on a LIVE loop's answer
// ("a frame is written in microseconds; reaching the bound means the read loop is
// gone, not slow"). But the wait did not check whether the loop it was waiting for
// still existed: it read `bandPending()` alone. So a caller that queued a row while
// a loop ran, and then had that loop EXIT under it (measured: a session teardown
// closed the terminal while the capture loop was still inside its own notice — the
// notice queued the leaving row, the terminal close ended the read loop, and the
// notice was left waiting for a drainer that had already returned), spent all 4000
// steps and then announced "the read loop is stalled?" about a loop that had ended.
// The instrumented run named the invariant exactly: `entryGoid=75 nowGoid=0` —
// the loop WAS the owner at entry, and was gone at the moment of the report.
//
// WHAT THE FIX IS, and what these cases pin: the owner is read ONCE on entry, and
// the wait returns as soon as that identity is no longer the running loop. The
// frame is NOT dropped by that return — it stays the LEVEL the next read outcome
// re-derives (`bandPending()` still true), which is the same answer the seam gives
// for the no-loop state. So the row is late, never lost, and the degradation is not
// reported as a stall (Rule 15: a wrong report is worse than the late row).
//
// WHY THIS IS A PACKAGE TEST INSIDE lineedit. The defect needs a window that a
// black-box driver cannot open deterministically: the loop must VANISH between the
// queue and the wait's first observation. Driving it through a real `Run` would be a
// race against the loop's own drain (the ordinary path drains the frame in
// microseconds and the case would pass or fail by scheduling). Here the loop's
// identity is the editor's own per-run fact, so the window is opened by the two
// stores below — the same fields `Run` maintains (editor.go: noteAudience /
// `defer e.loopGoid.Store(0)`).

const (
	// ownerBandRow is the row the case queues; its text is irrelevant to the bound
	// and is here only so a failure reads as a row rather than a bool.
	ownerBandRow = "owner-bound-marker"
	// ownerGoneID is the identity of a read loop that is NOT this goroutine's. The
	// value is arbitrary — what matters is that it differs from the caller's id and
	// from 0, so `waitBandWrite` is both entered (the seam's `loopGoid == 0` arm is
	// what short-circuits before it) and then abandoned.
	ownerGoneID = 424242
	// ownerEnterWait is the window given to the waiter to reach its loop before the
	// owner is removed. It is a SYNCHRONIZATION AID, not a measured duration: the
	// assertion below is the ELAPSED bound, and it is compared against a value two
	// orders of magnitude away from this one (bandWaitBudget is 4s), so a slow host
	// cannot turn this case red.
	ownerEnterWait = 50 * time.Millisecond
	// ownerBoundWait is how long the case allows the wait to end after its owner is
	// gone. The pre-fix shape spends bandWaitBudget (4s) here and THEN reports the
	// stall; the post-fix shape returns on its next observation.
	ownerBoundWait = 500 * time.Millisecond
)

// ownerHarness builds the layout WITHOUT a read loop (the direct-driver shape) and
// exposes the error channel the degradation is reported on, so "it reported a stall
// that never happened" is an ASSERTION rather than an absence.
//
// A loop IS simulated for the queued frame's sake: `loopGoid` is set by the case,
// because that field is exactly what the seam reads to decide whether a drainer is
// in prospect.
func ownerHarness(t *testing.T) (*Editor, *bytes.Buffer) {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: noloopCols, height: noloopRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	errOut := &bytes.Buffer{}
	ed := NewChatEditor(src, out, promptForTest, nil, errOut, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%dx%d) = false, want true (the layout's floor is %d rows)",
			noloopCols, noloopRows, RowsMinUsable())
	}
	t.Cleanup(func() {
		src.stopSource()
		ed.Close()
	})
	return ed, errOut
}

// TestBandWait_EndsWhenTheOwningReadLoopIsGone is the regression: a wait whose
// owner has left must end at once, must leave the row QUEUED rather than dropped,
// and must NOT announce a stall.
//
// RED ON THE PRE-FIX SHAPE: the wait reads `bandPending()` alone, so it spends all
// 4000 steps (4s), returns false and writes the "the read loop is stalled?" warning
// — into a console whose loop had simply ended.
func TestBandWait_EndsWhenTheOwningReadLoopIsGone(t *testing.T) {
	ed, errOut := ownerHarness(t)
	if !ed.queueBand(bandFrame{text: ownerBandRow, oneRow: true}) {
		t.Fatal("queueBand() = false: the harness has no wake seam, so the case is not on the waiting road at all")
	}
	// A read loop owns the queue (the state every foreign caller's wait starts in).
	ed.loopGoid.Store(ownerGoneID)

	done := make(chan bool, 1)
	go func() { done <- ed.waitBandWrite() }()
	// The loop goes away under the waiter — the measured shape: a teardown ends the
	// read while the producer's row is still queued.
	time.Sleep(ownerEnterWait) //stylegate:allow-sleep synchronization aid: the case asserts the ELAPSED bound (ownerBoundWait), two orders below bandWaitBudget
	ed.loopGoid.Store(0)

	start := time.Now()
	select {
	case <-done:
	case <-time.After(ownerBoundWait):
		t.Fatalf("the wait did not end within %s after its read loop was gone: it is waiting for a drainer that will never come "+
			"(the pre-fix shape spends the whole bandWaitBudget and then blames a stalled loop)", ownerBoundWait)
	}
	if waited := time.Since(start); waited > ownerBoundWait {
		t.Errorf("the wait took %s to notice its owner was gone, want under %s", waited, ownerBoundWait)
	}
	// THE ROW IS LATE, NEVER LOST: the frame stays the level the next read outcome
	// re-derives, which is what makes returning here honest rather than a drop.
	if !ed.bandPending() {
		t.Error("the queued row was dropped: it must stay the level the next read outcome re-derives")
	}
	// AND THE STALL IS NOT ANNOUNCED: a loop that ended is not a loop that is
	// stalled, and a caller reading the diagnostic stream must not be told a defect
	// that never happened (Rule 15).
	if got := errOut.String(); got != "" {
		t.Errorf("the degradation was reported for a loop that had ended, want silence\n--- errOut ---\n%s", got)
	}
}

// TestBandWait_StillBoundsALiveButSilentLoop is the case's own control, and it is
// what keeps the fix from being "never wait": a loop that IS still running and does
// not drain must still spend the bound and REPORT it.
//
// Without this arm, "return as soon as anything looks different" would be a
// defensible reading of the fix — and it would silently turn the seam's one
// synchronization guarantee into a no-op.
func TestBandWait_StillBoundsALiveButSilentLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("spends the full bandWaitBudget by construction — the bound IS the subject")
	}
	ed, errOut := ownerHarness(t)
	if !ed.queueBand(bandFrame{text: ownerBandRow, oneRow: true}) {
		t.Fatal("queueBand() = false: the harness has no wake seam, so the case is not on the waiting road at all")
	}
	// The loop is PRESENT and never leaves: the id is this goroutine's, so it cannot
	// be confused with the waiter's own, and it stays put for the whole wait.
	ed.loopGoid.Store(goid() + 1)

	start := time.Now()
	ok := ed.waitBandWrite()
	waited := time.Since(start)
	if ok {
		t.Error("waitBandWrite() = true with a frame that was never drawn: a wait must not claim a row it did not see drawn")
	}
	if waited < bandWaitBudget {
		t.Errorf("the wait returned after %s, want it to spend the bound (%s) while its loop is alive but silent — "+
			"a bound that ends early on a live loop is not a bound", waited, bandWaitBudget)
	}
	if _, err := errOut.WriteString(""); err != nil {
		t.Fatalf("the harness error channel is unusable: %v", err)
	}
	if got := errOut.String(); got == "" {
		t.Error("the stall was NOT reported: a live loop that never drained must be announced, never silently waited out")
	}
}
