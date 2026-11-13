package lineedit

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// bandGuardModelReads counts the reads of the editing model that happened while a
// foreign band write was in flight. The HOOK that calls it lives in bandgate.go,
// because Value's reads must reach it from production code; this counter is the
// guard's own.
var bandGuardModelReads atomic.Int64

// bandwriter_foreign_test.go — the guard for THE FLOW SEAM'S OWNER.
//
// What this guards (measured 2026-09-28, task 01a0e608): the flow seam —
// Editor.WriteLines and its sibling WriteBandRow — writes the answer band from
// the TURN's goroutine (cmd/agent's chatFlowSeam, the REPL's own notices), while
// the read loop's Editor.Run does e.val.Reset() at the start of each line. Two
// goroutines, one Value. The epic measured 26 `WARNING: DATA RACE` blocks in ONE
// run of `go test -race ./cmd/agent/ -run TestChatSurfaceKey_`, every one of them
// this seam against Run's Reset: WriteLines called e.region.WriteLines and then
// e.repaint(), and repaint reads the mutable editing model
// (clampedMenu → Value.RowsFor → Value.Text).
//
// THE INVARIANT THIS FILE PINS. The read loop is the layout's ONLY writer and
// the editing model's ONLY reader. A band write may therefore be QUEUED from a
// foreign goroutine (data only) but must be WRITTEN by the read loop — so a band
// write performed on the caller's own goroutine is a defect, observable without
// -race, and that is what a guard can be:
//
//  1. TestBandWriter_ForeignGoroutineDoesNotReadTheEditingModel — parks the read
//     loop on its gate (so the queue has exactly one consumer and no race with a
//     Reset), arms a sentinel that panics if Value is READ, and calls WriteLines
//     from a foreign goroutine. It must return with the sentinel untouched.
//     RED-PROOF: revert WriteLines to the pre-fix body (write + repaint on the
//     caller) and this test fails, because the repaint reads Value.
//  2. TestBandWriter_TheReadLoopWritesTheRow — the row really is written, by the
//     loop, with the terminator deciding WHO waits: a foreign write returns only
//     once the drain that wrote it has finished, a write FROM the loop's own
//     goroutine (the submit path in cmd/agent, which flushes while the caller
//     reads its own screen back) returns at once and is written on the next read
//     outcome — which is exactly why the framing terminator is a parameter of
//     the drain rather than a fixed "wait always".
//  3. TestBandWriter_AWriteWithNoWakeSeamIsStillDrawn — the fallback: with no
//     wake channel the frame is a LEVEL the loop re-derives, so it is written on
//     the loop's next own wake (late, never lost) and the degradation is
//     COUNTED (BandNotifyMisses) instead of silent.

const (
	// bandGuardCols / bandGuardSlackRows: the guard is about the OWNER, not about
	// the layout's floor, so it does not run at the floor.
	bandGuardCols      = 60
	bandGuardSlackRows = 4
	// bandGuardText is what the foreign producer writes; bandGuardNotice is what
	// the test edits on the loop's goroutine (the keys).
	bandGuardText   = "band-owner-marker"
	bandGuardNotice = "zz"
	// bandGuardSteps bounds a wait on a level or on a scripted step — a
	// synchronization wait, never a measured duration.
	bandGuardSteps = 600
	bandGuardStep  = 5 * time.Millisecond
	// bandGuardObservations is how many steps the guard lets a goroutine run
	// before it asserts that the call is STILL outstanding — an observation
	// window, not a measured duration.
	bandGuardObservations = 3
)

// bandGuardRows is the geometry the guard opens at (the layout floor plus slack).
var bandGuardRows = RowsMinUsable() + bandGuardSlackRows

// bandGuardHarness is the parked read loop plus the write stream.
type bandGuardHarness struct {
	ed   *Editor
	out  *recordingWriter
	src  *guardedOrderingSource
	done chan struct{}
}

// newBandGuardHarness starts a chat editor whose read loop PARKS on the first
// gated read, so the drain has exactly one consumer and the guard's foreign call
// cannot race a Reset.
func newBandGuardHarness(t *testing.T) *bandGuardHarness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	base := &scriptedSource{width: bandGuardCols, height: bandGuardRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	src := &guardedOrderingSource{
		scriptedSource: base,
		gate:           make(chan struct{}),
		parked:         make(chan struct{}),
		raised:         make(chan struct{}, 1),
		writes:         out.count,
	}
	ed := NewChatEditor(src, out, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", bandGuardRows)
	}
	if !ed.NotifyEnabled() {
		t.Fatal("the harness's source supplied no wake seam — the queued frame could not interrupt the loop and the level wait below would time out for the wrong reason")
	}
	h := &bandGuardHarness{ed: ed, out: out, src: src, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		_, _ = ed.Run() //stylegate:ignore the harness's teardown: a real failure surfaces as a bounded wait that dumps the write stream
	}()
	t.Cleanup(func() {
		src.allowEOF()
		src.stopSource()
		select {
		case <-h.done:
		case <-time.After(bandGuardStep * bandGuardSteps):
		}
		ed.Close()
	})
	return h
}

// waitParked waits until the loop has reached its gated read.
func (h *bandGuardHarness) waitParked(t *testing.T) {
	t.Helper()
	select {
	case <-h.src.parked:
	case <-time.After(bandGuardStep * bandGuardSteps):
		t.Fatalf("the read loop never reached its park — the guard's barrier is not in place\n--- write stream ---\n%s", h.dump())
	}
}

// release opens the gate, letting the parked read report the queued frame.
func (h *bandGuardHarness) release() { close(h.src.gate) }

// clear empties the write stream, so a check reads only what came after it.
func (h *bandGuardHarness) clear() {
	h.out.mu.Lock()
	h.out.writes = nil
	h.out.mu.Unlock()
}

// drew reports whether the recorded stream carries the text.
func (h *bandGuardHarness) drew(text string) bool {
	for _, c := range h.out.snapshot() {
		if strings.Contains(string(c), text) {
			return true
		}
	}
	return false
}

// dump is the write stream rendered as text — the evidence a failure quotes.
func (h *bandGuardHarness) dump() string {
	var b strings.Builder
	for i, w := range h.out.snapshot() {
		b.WriteString("--- chunk ")
		b.WriteString(itoa(i))
		b.WriteString(" ---\n")
		b.WriteString(strings.ReplaceAll(string(w), "\x1b", "<ESC>"))
		b.WriteString("\n")
	}
	return b.String()
}

// waitDrawn waits until the write stream carries the text.
func (h *bandGuardHarness) waitDrawn(t *testing.T, text string) {
	t.Helper()
	for i := 0; i < bandGuardSteps; i++ {
		if h.drew(text) {
			return
		}
		time.Sleep(bandGuardStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's write
	}
	t.Fatalf("the read loop never wrote %q\n--- write stream ---\n%s", text, h.dump())
}

// TestBandWriter_ForeignGoroutineDoesNotReadTheEditingModel is the guard the epic
// asks for: a band write must not read or write the editing model from a foreign
// goroutine.
//
// The barrier makes it deterministic rather than sampled. The read loop is parked
// on the source's gate, so the sentinel cannot fire on the LOOP's own legitimate
// reads (it is blocked before any repaint); the sentinel is armed; the seam is
// called from a goroutine that is not the loop; the gate is released only after
// that call has returned. Anything the sentinel counts therefore happened on the
// CALLER's goroutine, which is the defect stated in the terms of the invariant.
func TestBandWriter_ForeignGoroutineDoesNotReadTheEditingModel(t *testing.T) {
	h := newBandGuardHarness(t)
	h.waitParked(t)
	h.clear()

	bandGuardModelReads.Store(0)
	setBandModelReadHook(func() { bandGuardModelReads.Add(1) })
	armed := true
	defer func() {
		if armed {
			setBandModelReadHook(nil)
		}
	}()

	wrote := make(chan bool, 1)
	go func() { wrote <- h.ed.WriteLines(bandGuardText) }()
	// The foreign call WAITS for the draw it will be read back from, so while the
	// loop is parked it must still be outstanding: a call that returned here
	// either did the work itself (the defect) or gave up early. The wait is on
	// observation, never on a duration.
	time.Sleep(bandGuardObservations * bandGuardStep) //stylegate:allow-sleep the observation window: the parked call is asserted to be OUTSTANDING, and a synthetic state cannot be observed without giving the goroutine its slice
	select {
	case <-wrote:
		setBandModelReadHook(nil)
		armed = false
		t.Fatalf("the foreign WriteLines returned while the read loop was parked and the frame was still queued: a seam that gave up on the draw reports false to a caller that then announces a screen it does not own, and one that drew it itself is the defect this guard exists for\n%s", h.dump())
	default:
	}

	// The frame is DATA, not a terminal byte: nothing was written for it yet.
	if h.drew(bandGuardText) {
		setBandModelReadHook(nil)
		armed = false
		t.Fatalf("the foreign WriteLines wrote the row itself — the band has one writer, and it is the read loop\n%s", h.dump())
	}

	// Disarm BEFORE the assertion: the release below lets the loop read its own
	// keys and drain the queue (which reads the model legitimately), and those
	// reads are the loop's business, not the caller's.
	setBandModelReadHook(nil)
	armed = false
	if n := bandGuardModelReads.Load(); n != 0 {
		t.Fatalf("the foreign WriteLines read the editing model %d time(s) — the read loop is the model's ONLY reader (a read from the turn's goroutine races Run's Value.Reset; measured 2026-09-28: 26 WARNING: DATA RACE blocks in one `go test -race ./cmd/agent/ -run TestChatSurfaceKey_`)\n%s", n, h.dump())
	}

	// And the loop really does write it once the park is released — the queue
	// carries the frame, the level is raised, and nothing else can write it.
	h.release()
	h.waitDrawn(t, bandGuardText)
	if ok := <-wrote; !ok {
		t.Fatalf("the foreign WriteLines reported false after its row was drawn — the caller would then also print the historic flow bytes over the band\n%s", h.dump())
	}
}

// TestBandWriter_TheReadLoopWritesTheRow pins the division of labour and the
// terminator that decides who waits for it.
//
// A FOREIGN write waits for the drain that wrote its row (cmd/agent's chat
// surface ARMS itself by writing a notice and then asserts the notice is on the
// screen), while a write FROM the loop's own goroutine must NOT wait — the loop
// is the only thing that can drain the queue, so waiting there would deadlock
// the submit path, which flushes its row while the caller reads the screen back.
func TestBandWriter_TheReadLoopWritesTheRow(t *testing.T) {
	h := newBandGuardHarness(t)
	h.waitParked(t)
	h.clear()

	// (a) FROM THE LOOP'S OWN GOROUTINE: no wait, and the row is not written yet.
	onLoop := make(chan bool, 1)
	h.src.setAfterRead(func() { onLoop <- h.ed.WriteLines(bandGuardText) })
	h.release()
	select {
	case ok := <-onLoop:
		if !ok {
			t.Fatalf("a band write from the read loop's own goroutine reported false — the submit path's flush would fall back to the historic flow bytes over the band's own rows\n%s", h.dump())
		}
	case <-time.After(bandGuardStep * bandGuardSteps):
		t.Fatal("a band write from the read loop's own goroutine BLOCKED: the loop is the only drainer, so waiting for the drain there is a deadlock (the framing terminator must not apply to the loop's own call)")
	}
	// The row's OWNER is asserted where it is observable: the seam returned
	// without draining (above), and the loop is what writes it (below). Whether
	// the drain happened before this line read the stream is a scheduling detail
	// of a concurrent pair, and asserting it would be exactly the flaky proxy the
	// package's other guards warn about.
	h.src.setAfterRead(nil)
	if !h.ed.bandPending() {
		t.Fatal("the queued frame did not raise the level: an unsignalled queue would be written only when some later read outcome asked, and a level is what carries it past a parked read (Rule 15 — a row must not be lost)")
	}

	// (b) THE LOOP DRAINS IT. Queued input is what makes the loop iterate, and
	// the flush at the end of that read is what writes the row.
	h.src.push(scriptStep{data: []byte(bandGuardNotice)})
	h.waitDrawn(t, bandGuardText)
}

// TestBandWriter_AWriteWithNoWakeSeamIsStillDrawn is the fallback's guard: with
// NO wake channel installed the queued row is still a level the loop re-derives,
// so it is written on the loop's next own wake (late, never lost), and the
// missed notification is COUNTED so a caller can announce the degradation
// instead of leaving it invisible (Rule 15).
func TestBandWriter_AWriteWithNoWakeSeamIsStillDrawn(t *testing.T) {
	h := newBandGuardHarness(t)
	h.waitParked(t)
	h.clear()

	// The seam is removed AFTER the loop parked, so nothing can interrupt the
	// parked read: what carries the row is the level alone.
	h.ed.SetNotification(nil)
	if h.ed.NotifyEnabled() {
		t.Fatal("SetNotification(nil) left an enabled wake seam — the fallback this test measures would not be the fallback")
	}
	// The wait is not available on this road (no channel can carry the draw), so
	// the seam returns at once — reporting the historic true, because the layout
	// does own the screen and will draw the row on its own next wake.
	if ok := h.ed.WriteLines(bandGuardText); !ok {
		t.Fatalf("WriteLines reported false with the layout installed — a degraded wake channel is not a missing layout, and a false here would make the caller write the historic flow bytes as WELL\n%s", h.dump())
	}
	if n := h.ed.BandNotifyMisses(); n == 0 {
		t.Fatal("a band write with no wake seam was not COUNTED: the fallback would be silent, and a caller could not tell a delivered row from one waiting for the loop's next wake")
	}
	if h.drew(bandGuardText) {
		t.Fatalf("the row was written with no wake seam and the loop parked — the write came from a goroutine that is not the read loop\n%s", h.dump())
	}

	// The loop's next own wake draws it: the park is released and input is pushed,
	// so the loop reads a step and its own read outcome drains the queued row
	// (there is no notification to interrupt it — that is the point).
	h.release()
	h.src.push(scriptStep{data: []byte(bandGuardNotice)})
	h.waitDrawn(t, bandGuardText)
}

// TestBandWriter_GuardIsDeterministic repeats the guard's sequence: the barrier
// is a gate, the observable is a COUNT of model reads on the caller's goroutine,
// and the fallback is a level — so no run depends on host load or on sampling a
// window. A guard that flaked would be worse than none (it would be turned off).
func TestBandWriter_GuardIsDeterministic(t *testing.T) {
	const reps = 5
	for i := 0; i < reps; i++ {
		h := newBandGuardHarness(t)
		h.waitParked(t)
		h.clear()
		bandGuardModelReads.Store(0)
		setBandModelReadHook(func() { bandGuardModelReads.Add(1) })
		done := make(chan bool, 1)
		go func() { done <- h.ed.WriteLines(bandGuardText) }()
		time.Sleep(bandGuardObservations * bandGuardStep) //stylegate:allow-sleep the observation window (see test 1): the parked call must still be outstanding
		select {
		case <-done:
			setBandModelReadHook(nil)
			t.Fatalf("rep %d: the foreign band write returned while the loop was parked and the frame was still queued", i)
		default:
		}
		setBandModelReadHook(nil)
		if n := bandGuardModelReads.Load(); n != 0 {
			t.Fatalf("rep %d: the foreign band write read the editing model %d time(s)", i, n)
		}
		h.release()
		h.waitDrawn(t, bandGuardText)
	}
}
