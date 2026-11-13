package lineedit

import (
	"strings"
	"testing"
	"time"
)

// answer_writer_foreign_test.go — the guard for THE ANSWER RENDERER'S OWNER.
//
// What this guards (measured 2026-09-28, task 01a0e62d): the streamed answer is
// driven from the TURN's goroutine (cmd/agent's chatTurnRenderer → Begin/Delta/
// BeginTurn/Finish/EndTurn/BeginThinking/Note) while the editor's READ LOOP draws
// the same renderer (repaint → paintMenu → AnswerRenderer.Tick → drawStatus, and
// repaint → drainBandFrames → drainNotes → writeRow → commitPrefix). Two
// goroutines, one renderer, and every field of it is written on the way from one
// delta to the next. The epic measured 11 `WARNING: DATA RACE` blocks in ONE run
// of `go test -race ./cmd/agent/ -run TestChatSurfaceKey_` — drawStatus↔
// reanchorLiveRow, Note↔Noting, drainNotes↔writeRow, commitPrefix↔writeRow,
// LineComposer.inFence↔EndStream, writeRow↔writeRow — all of them this ownership
// defect.
//
// THE INVARIANT THIS FILE PINS. While a read loop runs, the LOOP owns the
// renderer: it applies every turn-side call, in the producer's order, on its own
// goroutine (see answerqueue.go). A turn-side call is therefore a PUBLISH — it
// queues an op, raises the level, wakes the loop, and writes NOTHING — and that
// is observable without -race:
//
//  1. TestAnswerWriter_ForeignGoroutineWritesNothing — parks the read loop on its
//     gate so the only goroutine that could write is the caller's, arms nothing
//     and mutates nothing, and calls the turn-side seam from a goroutine that is
//     not the loop. It must return with ZERO bytes written and ZERO lines
//     committed; then the release must show the loop drawing it. RED-PROOF: on
//     the pre-fix tree this test fails on the byte count, because the foreign
//     Delta ran its whole body — buffer, cadence, commit, live write — there.
//  2. TestAnswerWriter_GuardIsDeterministic — repeats it, so the guard is a
//     property and not one lucky interleaving. The barrier is the source's gate
//     and the observable is a COUNTER on the call's own goroutine: no run depends
//     on host load or on sampling a window.
//
// The harness is bandwriter_foreign_test.go's, over the answer renderer instead
// of the band: the SAME parked read loop (guardedOrderingSource), the same
// recording writer, the same bounded waits. What differs is the question.

const (
	// answerGuardCols / answerGuardSlackRows: the guard is about the OWNER, not
	// about the layout's floor, so it does not run at the floor.
	answerGuardCols      = 60
	answerGuardSlackRows = 4
	// answerGuardDelta is what the foreign producer streams; answerGuardKeys is
	// what the read loop is given on its own goroutine, to make it iterate.
	answerGuardDelta = "answer-owner-marker\n"
	answerGuardKeys  = "zz"
	// answerGuardSteps bounds a wait on a level or on a scripted step — a
	// synchronization wait, never a measured duration.
	answerGuardSteps = 600
	answerGuardStep  = 5 * time.Millisecond
)

// answerGuardRows is the geometry the guard opens at (the layout floor plus
// slack).
var answerGuardRows = RowsMinUsable() + answerGuardSlackRows

// answerGuardHarness is the parked read loop plus the paint stream.
type answerGuardHarness struct {
	ed   *Editor
	out  *recordingWriter
	src  *guardedOrderingSource
	done chan struct{}
}

// newAnswerGuardHarness starts a chat editor whose read loop PARKS on the first
// gated read, so the renderer has exactly one goroutine that can apply a call.
func newAnswerGuardHarness(t *testing.T) *answerGuardHarness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	base := &scriptedSource{width: answerGuardCols, height: answerGuardRows, clock: clock, holdAtEnd: true}
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
		t.Fatalf("EnableRegion(%d rows) = false, want true", answerGuardRows)
	}
	if !ed.NotifyEnabled() {
		t.Fatal("the harness's source supplied no wake seam — a published op could not interrupt the loop and the level wait below would time out for the wrong reason")
	}
	if ed.Answer() == nil {
		t.Fatal("no answer renderer on this terminal: the guard would assert the ownership of nothing")
	}
	h := &answerGuardHarness{ed: ed, out: out, src: src, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		_, _ = ed.Run() //stylegate:ignore the harness's teardown: a real failure surfaces as a bounded wait that dumps the write stream
	}()
	t.Cleanup(func() {
		src.allowEOF()
		src.stopSource()
		select {
		case <-h.done:
		case <-time.After(answerGuardStep * answerGuardSteps):
		}
		ed.Close()
	})
	return h
}

// waitParked waits until the loop has reached its gated read: from there on the
// only goroutine that can write to the renderer is the one the guard calls from.
func (h *answerGuardHarness) waitParked(t *testing.T) {
	t.Helper()
	select {
	case <-h.src.parked:
	case <-time.After(answerGuardStep * answerGuardSteps):
		t.Fatalf("the read loop never reached its park — the guard's barrier is not in place\n--- write stream ---\n%s", h.dump())
	}
}

// release opens the gate, letting the parked read report the published op.
func (h *answerGuardHarness) release() { close(h.src.gate) }

// drew reports whether the recorded stream carries the text.
func (h *answerGuardHarness) drew(text string) bool {
	for _, c := range h.out.snapshot() {
		if strings.Contains(string(c), text) {
			return true
		}
	}
	return false
}

// dump is the write stream rendered as text — the evidence a failure quotes.
func (h *answerGuardHarness) dump() string {
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

// waitDrawn waits until the write stream carries the text (the loop's own draw).
func (h *answerGuardHarness) waitDrawn(t *testing.T, text string) {
	t.Helper()
	for i := 0; i < answerGuardSteps; i++ {
		if h.drew(text) {
			return
		}
		time.Sleep(answerGuardStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's write
	}
	t.Fatalf("the read loop never drew %q\n--- write stream ---\n%s", text, h.dump())
}

// TestAnswerWriter_ForeignGoroutineWritesNothing is the guard the epic asks for:
// a turn-side call must not mutate the renderer from the turn's goroutine while
// the read loop owns it.
//
// The barrier makes it deterministic rather than sampled. The read loop is parked
// inside the source's gate, so it holds no lock and writes nothing; the foreign
// call is made; the gate is released only after that call has been OBSERVED to
// write nothing. Anything on the stream before the release is the caller's own —
// which is the defect stated in the terms of the invariant: the renderer has one
// writer per instant, and while the loop runs it is not the turn.
func TestAnswerWriter_ForeignGoroutineWritesNothing(t *testing.T) {
	h := newAnswerGuardHarness(t)
	h.waitParked(t)

	// The stream index BEFORE the call: the recorder's own mutex is what makes
	// this readable while the loop is parked (the loop is blocked in its read, so
	// the only writer that could appear below is the call itself).
	before := h.out.count()
	committedBefore := h.ed.Answer().Committed()

	wrote := make(chan struct{})
	go func() {
		defer close(wrote)
		h.ed.Answer().Delta(answerGuardDelta)
	}()
	// The call returns immediately — it PUBLISHES (a queue, a level, a wake) —
	// and it must not have written a byte on the way.
	select {
	case <-wrote:
	case <-time.After(answerGuardStep * answerGuardSteps):
		t.Fatal("the turn-side Delta blocked — a publish is a queue and a wake, never a wait on the loop's read")
	}

	if got := h.out.count(); got != before {
		t.Fatalf("the turn-side Delta wrote %d chunk(s) while the read loop was parked: the renderer is mutated by two goroutines, which is the defect this guard exists for (the loop repaints these same fields — measured 2026-09-28: 11 WARNING: DATA RACE blocks in one `go test -race ./cmd/agent/ -run TestChatSurfaceKey_`)\n--- write stream ---\n%s", got-before, h.dump())
	}
	if got := h.ed.Answer().Committed(); got != committedBefore {
		t.Fatalf("the turn-side Delta committed %d line(s) itself while the read loop was parked (was %d) — a commit is a WRITE (writeRow/finalizeLiveRow place a cursor and scroll)\n--- write stream ---\n%s", got, committedBefore, h.dump())
	}
	if h.drew(answerGuardDelta[:len(answerGuardDelta)-1]) {
		t.Fatalf("the turn-side Delta's text is on the stream already — it was written by a goroutine that is not the read loop\n--- write stream ---\n%s", h.dump())
	}

	// The release is what lets the ONE owner apply it: the loop reports the
	// raised wake, drains the queue and draws the text.
	h.release()
	h.waitDrawn(t, "answer-owner-marker")
}

// TestAnswerWriter_GuardIsDeterministic repeats the guard's sequence: the barrier
// is a gate, the observable is a byte count on the caller's own goroutine, so no
// run depends on host load or on sampling a window. A guard that flaked would be
// worse than none (it would be turned off).
func TestAnswerWriter_GuardIsDeterministic(t *testing.T) {
	const reps = 5
	for i := 0; i < reps; i++ {
		h := newAnswerGuardHarness(t)
		h.waitParked(t)
		before := h.out.count()
		wrote := make(chan struct{})
		go func() {
			defer close(wrote)
			h.ed.Answer().Delta(answerGuardDelta)
		}()
		select {
		case <-wrote:
		case <-time.After(answerGuardStep * answerGuardSteps):
			t.Fatalf("rep %d: the turn-side Delta blocked", i)
		}
		if got := h.out.count(); got != before {
			t.Fatalf("rep %d: the turn-side Delta wrote %d chunk(s) from its own goroutine\n--- write stream ---\n%s", i, got-before, h.dump())
		}
		h.release()
		h.waitDrawn(t, "answer-owner-marker")
	}
}

// TestAnswerWriter_NoLoopMeansTheCallerOwnsIt pins the OTHER road, which is why
// the routing can be a property of the caller's identity rather than a permanent
// handover: with no read loop running the caller IS the owner and the work is
// done here. Every unit test in this package drives the engine that way (51 call
// sites), and so do the pty cases — a design that refused those calls would turn
// a race fix into a regression of the package's own tests.
func TestAnswerWriter_NoLoopMeansTheCallerOwnsIt(t *testing.T) {
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	out := &recordingWriter{}
	ed := NewChatEditor(&scriptedSource{width: answerGuardCols, height: answerGuardRows, clock: clock, holdAtEnd: true}, out, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", answerGuardRows)
	}
	// NO read loop: not started, so nobody else can reach the renderer.
	r := ed.Answer()
	r.Begin()
	r.Delta(answerGuardDelta)
	r.Finish()
	if got := r.Committed(); got == 0 {
		t.Fatalf("with no read loop running the caller's own Finish committed nothing (%d lines) — a renderer nobody else owns must still do the work, or the whole package's direct-drive tests would be asserting a renderer that never ran", got)
	}
	if !strings.Contains(allWrites(out.snapshot()), "answer-owner-marker") {
		t.Fatalf("with no read loop running the answer never reached the stream\n--- write stream ---\n%s", strings.ReplaceAll(allWrites(out.snapshot()), "\x1b", "<ESC>"))
	}
}

// allWrites joins the recorded chunks (the guard's own reader; the package's
// screens have their own).
func allWrites(chunks [][]byte) string {
	var b strings.Builder
	for _, c := range chunks {
		b.Write(c)
	}
	return b.String()
}
