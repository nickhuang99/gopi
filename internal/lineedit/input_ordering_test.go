package lineedit

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// input_ordering_test.go — the read loop's ORDERING seam, guarded at the package
// level and asserted off the WRITE STREAM.
//
// What this guards (commit 65d7e278, "a draw never overtakes queued input"): the
// EventWake arm draws a publish only when the source reports no queued input
// (`if e.inputPending() { continue }`, editor.go). That rule is the fix for the
// cmd/agent small-height flake — the loop serviced a publish's wake while the
// harness's keystrokes were still unread, so a caller waiting on the draw saw an
// editing row reading "you> hel" — and until this file NOTHING asserted it: the
// shipped cmd/agent guard can only be made red by NEUTRALIZING the call site
// (`if false`), i.e. it guards that the DECISION exists, never that its answer is
// right.
//
// The guard is deterministic, not sampled. It parks the read loop on the source's
// own gate, queues keystrokes and raises a publish behind it, then releases the
// gated read ONCE, so the loop's next read reports the wake while the source says
// input is waiting — the one instant at which the ORDER is observable. The order
// is read off the write stream: the index at which the loop took its FIRST
// keystroke, against the index of the chunk that first draws the status. There is
// no wall-clock threshold, no sleep-as-proof and no host load (PROJECT.md Rule 27):
// the barrier is the gate and the measurement is an index into a byte stream.

const (
	// orderingSlackRows is the headroom above the layout's floor: the guard is
	// about the read loop's ORDER, not about the floor, so it does not run at it.
	orderingSlackRows = 4
	// orderingCols is a width the status source's whole label fits on.
	orderingCols = 60
	// orderingDrawWait bounds a wait on the read loop's draw — a synchronization
	// wait, never a measured duration. A host under load makes it slower to trip,
	// which is what a bound is for; it can never make the assertion pass.
	orderingDrawWait = 3 * time.Second
	// orderingPollStep is how often such a wait re-reads the write stream.
	orderingPollStep = time.Millisecond
	// orderingStatusText / orderingStatusLive / orderingStatusNext are the status
	// values the guard reads back off the reserved status row.
	orderingStatusText = "ordering-source"
	orderingStatusLive = "ready"
	orderingStatusNext = "ready #1"
	// orderingTyped is what the harness queues behind the publish.
	orderingTyped = "hello"
	// orderingKeepAliveMS is the wait the harness serves the editor once the queue
	// is DRAINED, so the read keeps the production cadence instead of inventing a
	// timeout of the guard's own (Rule 1/Rule 2: no second source of truth for a
	// wait). It is the editor's OWN fast-poll value.
	orderingKeepAliveMS = completionPollMS
	// orderingReps is how many times the determinism test repeats the sequence.
	orderingReps = 5
)

// orderingRows is the geometry the guard opens at (the layout floor plus slack).
var orderingRows = RowsMinUsable() + orderingSlackRows

// guardedOrderingSource is a scripted source that can PARK the read loop and
// answer the input-readiness seam honestly.
//
// The seam is the whole point: *scriptedSource deliberately does NOT implement
// inputBacklog (it keeps the historic unread arm), so the guard needs a source
// whose answer is TRUE while a script step is already handed over and FALSE once
// the loop has read it — the shape the production (*Terminal).InputPending
// answers off a real descriptor.
type guardedOrderingSource struct {
	*scriptedSource

	// gate holds every read until the test closes it — the barrier that makes the
	// ordering assertable instead of sampled. Installed before the read loop
	// starts, so it never races.
	gate chan struct{}
	// parked is closed the first time a gated read reaches the gate: the boot
	// frame is written and the loop is waiting, so the test opens its window
	// there instead of racing the boot repaint.
	parked   chan struct{}
	parkOnce sync.Once
	// raised carries the publishes raised while the gate is shut, so the FIRST
	// read after the release reports one AHEAD of the queued steps: the AMBIGUOUS
	// single wake the flake was about (a publish and keystrokes reaching the loop
	// as one event). Consumed when reported, exactly as Terminal.ReadEvent drains
	// its wake pipe before returning EventWake.
	raised chan struct{}
	// firstReadWrites is the write-stream index at the instant the loop took its
	// FIRST keystroke — the boundary the guard compares the draw's index against.
	// Recorded ONCE (firstReadRecorded latches it): the sample is taken at the
	// same instant production's InputPending answers, so the two seams measure
	// alike.
	//
	// IT IS ATOMIC for the reason blind is: ReadEvent STORES it on the read
	// loop's own goroutine while the test goroutine LOADS it (measure reads the
	// boundary to build a verdict). Same value, same assertions — only the way it
	// is read and written changed. The latch is a SECOND atomic rather than a -1
	// sentinel in this one, so a zero-value struct is already correct: chunk
	// index 0 is a legal sample, and "not recorded yet" must not be spelled with
	// a value the index can take.
	firstReadWrites   atomic.Int64
	firstReadRecorded atomic.Bool
	recordOnce        sync.Once
	// writes reports how many chunks the editor has written so far (the recording
	// writer's count). Never nil in this harness.
	writes func() int
	// blind makes the source answer the readiness seam FALSE whatever its queue
	// holds — the regressed seam, expressed in the harness (the red-proof shape
	// cmd/agent's scriptSource uses). The guard's negative control rides on it:
	// same sequence, same production code, only the seam's answer differs.
	//
	// It is ATOMIC because the case flips it from the TEST goroutine while the
	// read loop is already asking the seam from ITS OWN goroutine: the field is
	// the harness's, the read is production's, and the two need a happens-before
	// edge. The ASSERTION is unchanged (the seam answers false, so the draw is
	// free to overtake the queued input) — only the way the flag is read is.
	blind atomic.Bool
	// afterRead, when set, runs ON THE READ LOOP'S OWN GOROUTINE once a gated read
	// has been released. It is how bandwriter_foreign_test.go calls the band seam
	// from the loop itself — the shape cmd/agent's submit path has, where the
	// flush must not wait for a drain only this goroutine can perform.
	//
	// IT IS ATOMIC FOR THE SAME REASON blind IS, and this one is measured: the
	// test goroutine INSTALLS the hook and then CLEARS it (setting it back to nil
	// between phases) while ReadEvent watches it from the loop's own goroutine.
	// Two accesses to a func value with no happens-before edge is the same race a
	// plain bool was, so the field is published through a pointer and both ends
	// go through setAfterRead / runAfterRead. The hook's BODY is unchanged.
	afterRead atomic.Pointer[func()]
}

// setAfterRead installs the post-read hook (nil clears it). It is the ONLY
// writer — see afterRead for why the field cannot be assigned directly.
func (s *guardedOrderingSource) setAfterRead(fn func()) {
	if fn == nil {
		s.afterRead.Store(nil)
		return
	}
	s.afterRead.Store(&fn)
}

// runAfterRead runs the installed hook, if any, on the CALLER's goroutine (the
// read loop's, from ReadEvent).
func (s *guardedOrderingSource) runAfterRead() {
	if fn := s.afterRead.Load(); fn != nil {
		(*fn)()
	}
}

// firstReadWritesValue is the recorded boundary: the chunk index at which the
// loop took its first keystroke. The second result is false when the loop has
// not taken one yet — the "no sample" case the ordering verdict treats as an
// overtake. Two atomics rather than a -1 sentinel, because index 0 is a real
// sample (see the field's comment).
func (s *guardedOrderingSource) firstReadWritesValue() (int, bool) {
	if !s.firstReadRecorded.Load() {
		return 0, false
	}
	return int(s.firstReadWrites.Load()), true
}

// InputPending implements the editor's input-readiness seam (lineedit's
// inputBacklog): a step the script has already handed over is waiting to be read,
// so the read loop must read it BEFORE it draws a publish.
func (s *guardedOrderingSource) InputPending() bool {
	if s.blind.Load() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.i < len(s.steps)
}

// Notify records the publish for the gated read (see raised) instead of handing
// it to the inner loop, so the release can report it as the ambiguous wake.
func (s *guardedOrderingSource) Notify() {
	select {
	case s.raised <- struct{}{}:
	default: // one pending publish is the same information as a second one
	}
}

// ReadEvent parks on the gate first, then reports a raised publish AHEAD of the
// steps it already holds, then delivers the script as usual.
func (s *guardedOrderingSource) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	s.parkOnce.Do(func() { close(s.parked) })
	<-s.gate
	s.runAfterRead()
	select {
	case <-s.raised:
		// The ambiguous wake: the publisher raised, and input is queued too.
		return Event{Kind: EventWake}, nil
	default:
	}
	if s.hasPendingStep() {
		if s.writes != nil {
			s.recordOnce.Do(func() {
				s.firstReadWrites.Store(int64(s.writes()))
				s.firstReadRecorded.Store(true)
			})
		}
	} else {
		// NOTHING pending: the editor is about to block on input that is not
		// there. Serving its own cadence keeps that shape (a publish is what
		// interrupts the block), without the guard inventing a wait of its own.
		timeoutMS = orderingKeepAliveMS
	}
	return s.scriptedSource.ReadEvent(buf, timeoutMS)
}

// hasPendingStep reports whether the script holds a step not yet delivered.
func (s *guardedOrderingSource) hasPendingStep() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.i < len(s.steps)
}

// consumedSteps is how many scripted steps the loop has READ — the harness's
// record of what the read loop actually took off the wire, which is what makes
// the ordering invariant assertable (the index is advanced under the source's
// mutex by the reader goroutine; cmd/agent's scriptSource carries the same
// accessor under the name consumed).
func (s *guardedOrderingSource) consumedSteps() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.i
}

// allowEOF lets the source report end-of-input once its queue is drained, so the
// read loop RETURNS and the teardown joins it. Without it the harness's bounded
// post-drain waits keep the loop alive (a bounded arm never consults the source's
// stop channel), and the cleanup would have to close the editor out from under a
// running loop — the harness race the package's other guards warn about.
func (s *guardedOrderingSource) allowEOF() {
	s.mu.Lock()
	s.holdAtEnd = false
	s.mu.Unlock()
}

// orderingHarness is the parked read loop plus everything the guard reads back.
type orderingHarness struct {
	ed   *Editor
	out  *recordingWriter
	src  *guardedOrderingSource
	done chan struct{}
}

// newOrderingHarness starts a chat editor reading over a guarded source and
// registers its teardown: the source is stopped and the loop RETURNED before the
// editor is closed (closing first races the harness itself — the loop reads the
// region Close mutates).
func newOrderingHarness(t *testing.T) *orderingHarness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	base := &scriptedSource{width: orderingCols, height: orderingRows, clock: clock, holdAtEnd: true}
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
		t.Fatalf("EnableRegion(%d rows) = false, want true", orderingRows)
	}
	if !ed.NotifyEnabled() {
		t.Fatal("the harness's source supplied no wake seam — a publish could never reach the read loop and every wait below would time out for the wrong reason")
	}
	h := &orderingHarness{ed: ed, out: out, src: src, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		_, _ = ed.Run() //stylegate:ignore the harness's teardown: a real failure surfaces as a bounded waitStatusRow timeout that dumps the write stream
	}()
	t.Cleanup(func() {
		src.allowEOF()
		src.stopSource()
		select {
		case <-h.done:
		case <-time.After(orderingDrawWait):
		}
		ed.Close()
	})
	return h
}

// release opens the gate the loop is parked on.
func (h *orderingHarness) release() { close(h.src.gate) }

// waitParked waits until the boot frame is written and the loop has reached its
// park — the moment the ordering window may open.
func (h *orderingHarness) waitParked(t *testing.T) {
	t.Helper()
	select {
	case <-h.src.parked:
	case <-time.After(orderingDrawWait):
		t.Fatalf("the read loop never reached its park — the guard's ordering barrier is not in place\n--- write stream ---\n%s", h.dump())
	}
}

// window opens the measurement window: the write stream is cleared, so the boot
// frame is not part of the ordering the guard reads.
func (h *orderingHarness) window() {
	h.out.mu.Lock()
	h.out.writes = nil
	h.out.mu.Unlock()
}

// queue pushes the keystrokes the loop must read.
func (h *orderingHarness) queue(typed string) {
	for _, r := range typed {
		h.src.push(scriptStep{data: []byte(string(r))})
	}
}

// waitStatusRow waits until the reserved status row carries want.
func (h *orderingHarness) waitStatusRow(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(orderingDrawWait)
	for time.Now().Before(deadline) {
		if strings.Contains(h.row(orderingRows-1), want) {
			return
		}
		time.Sleep(orderingPollStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's draw (see orderingDrawWait)
	}
	t.Fatalf("timed out waiting for %q on the status row\n--- write stream ---\n%s", want, h.dump())
}

// waitConsumed waits until the loop has read n scripted steps.
func (h *orderingHarness) waitConsumed(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(orderingDrawWait)
	for time.Now().Before(deadline) && h.src.consumedSteps() < n {
		time.Sleep(orderingPollStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's drain (see orderingDrawWait)
	}
}

// statusChunkIndex is the index of the chunk that first draws the status text.
// It is the value the ordering assertion is stated in (a draw is located in the
// byte stream, never inferred from a counter).
func (h *orderingHarness) statusChunkIndex(text string) int {
	for i, c := range h.out.snapshot() {
		if strings.Contains(string(c), text) {
			return i
		}
	}
	return -1
}

// screen replays the write stream onto a FRESH screen: the chunks are the
// terminal's own bytes, so a fresh replay is idempotent by construction (the
// editor writes while the test reads, and an incremental feed would repaint an
// already-painted grid).
func (h *orderingHarness) screen() *Screen {
	s := NewScreen(orderingCols, orderingRows)
	for _, w := range h.out.snapshot() {
		s.Feed(w)
	}
	return s
}

// row is one rendered screen row.
func (h *orderingHarness) row(n int) string { return h.screen().Line(n) }

// dump is the write stream rendered as text — the evidence a failure quotes.
func (h *orderingHarness) dump() string {
	var b bytes.Buffer
	for i, w := range h.out.snapshot() {
		b.WriteString("--- chunk " + strconv.Itoa(i) + " ---\n")
		b.Write(bytes.ReplaceAll(w, []byte{'\x1b'}, []byte("<ESC>")))
		b.WriteByte('\n')
	}
	b.WriteString("--- rendered screen ---\n")
	b.WriteString(h.screen().Contents())
	return b.String()
}

// orderingVerdict is what the guard observed: where the draw landed and when the
// loop read its first queued keystroke.
type orderingVerdict struct {
	drawIdx int
	// firstReadWrites is the chunk index at which the loop took its first queued
	// keystroke; firstReadKnown says whether that sample exists at all. Two fields
	// instead of a -1 sentinel because chunk index 0 is a legal sample.
	firstReadWrites int
	firstReadKnown  bool
	drawChunks      int
}

// overtook reports whether the draw came before the read of queued input — the
// ordering the guard exists to forbid. No sample at all counts as an overtake:
// the read loop never read the queued input, so the draw that preceded it (or
// that skipped it entirely) is exactly the defect.
func (v orderingVerdict) overtook() bool {
	return !v.firstReadKnown || v.firstReadWrites > v.drawIdx
}

// measure reads the ordering off the write stream: the draw's chunk index and
// the index at which the loop took its first queued keystroke.
func (h *orderingHarness) measure(statusText string) orderingVerdict {
	v := orderingVerdict{drawIdx: h.statusChunkIndex(statusText)}
	v.firstReadWrites, v.firstReadKnown = h.src.firstReadWritesValue()
	for _, c := range h.out.snapshot() {
		if strings.Contains(string(c), statusText) {
			v.drawChunks++
		}
	}
	return v
}

// TestReadLoop_QueuedInputIsReadBeforeAPublishIsDrawn is the package-level
// ordering guard: a publish the read loop drew must not have been drawn ahead of
// input that was already queued.
//
// The sequence is fixed, not sampled: the loop is parked on the source's gate;
// the keystrokes are queued and the publish is raised behind it; the gate is
// released ONCE, so the loop's next read reports the wake while the source says
// input is waiting; the loop must therefore read the queued input before it
// draws. Red-proof: with the ordering gate neutralized (the source answering
// "nothing pending", or the production call site forced to `if false`), the same
// sequence draws the status first and this test fails with the message below.
func TestReadLoop_QueuedInputIsReadBeforeAPublishIsDrawn(t *testing.T) {
	h := newOrderingHarness(t)
	status := &testStatus{}
	h.ed.SetStatus(status.status)
	status.set(1) // the value the publish announces (orderingStatusNext)
	h.waitParked(t)
	h.window()

	// The keystrokes are queued INSIDE the window and the publish is raised after
	// them, so the released read finds input the source already holds beside the
	// pending publish — the interleaving the flake was about (one wake reporting
	// two independent events).
	h.queue(orderingTyped)
	h.ed.PublishStatus()
	h.release()

	h.waitStatusRow(t, orderingStatusNext)
	v := h.measure(orderingStatusNext)
	if v.drawIdx < 0 {
		t.Fatalf("the status row shows %q but no recorded chunk carries it — the guard cannot locate the draw\n%s", orderingStatusNext, h.dump())
	}
	if got := h.src.consumedSteps(); got < 1 {
		t.Fatalf("the status draw overtook queued input: %q was drawn with none of the %d queued keystrokes read\n%s",
			orderingStatusNext, len(orderingTyped), h.dump())
	}
	if v.overtook() {
		t.Fatalf("the status draw overtook queued input — the draw landed at write-stream index %d, and the read loop only read its first keystroke after %d chunk(s) had been written\n%s",
			v.drawIdx, v.firstReadWrites, h.dump())
	}

	// The draw is not the whole story: the queue must DRAIN (a loop that read one
	// keystroke and stopped would pass the ordering check above).
	h.waitConsumed(t, len(orderingTyped))
	if got := h.src.consumedSteps(); got != len(orderingTyped) {
		t.Fatalf("the read loop consumed %d of %d queued keystrokes\n%s", got, len(orderingTyped), h.dump())
	}
	t.Logf("ordering: the first keystroke was read at write index %d, the status draw landed at index %d (%d chunk(s) carry it) — the read came first",
		v.firstReadWrites, v.drawIdx, v.drawChunks)
}

// TestReadLoop_OrderingGuardIsDeterministic repeats the guard's sequence: the
// barrier is a gate and the assertion is an index into a byte stream, so the
// verdict may not move between repetitions inside ONE process. That is the
// instrument this flake class prescribes (`go test -count=N` over the test
// itself) and it needs no host load (Rule 27).
func TestReadLoop_OrderingGuardIsDeterministic(t *testing.T) {
	for i := 0; i < orderingReps; i++ {
		rep := i
		t.Run("rep"+strconv.Itoa(rep), func(t *testing.T) {
			h := newOrderingHarness(t)
			status := &testStatus{}
			h.ed.SetStatus(status.status)
			status.set(1) // the value the publish announces (orderingStatusNext)
			h.waitParked(t)
			h.window()
			h.queue(orderingTyped)
			h.ed.PublishStatus()
			h.release()
			h.waitStatusRow(t, orderingStatusNext)
			v := h.measure(orderingStatusNext)
			if v.drawIdx < 0 {
				t.Fatalf("rep %d: no chunk carries the status text\n%s", rep, h.dump())
			}
			if v.overtook() {
				t.Fatalf("rep %d: the status draw overtook queued input — draw at index %d, first keystroke read at write index %d\n%s",
					rep, v.drawIdx, v.firstReadWrites, h.dump())
			}
		})
	}
}

// TestReadLoop_NoOrderingGateLetsTheDrawOvertake is the guard's NEGATIVE control,
// stated in the harness rather than by editing production: the same sequence over
// a source whose readiness seam answers FALSE — the answer a regressed seam gives
// — produces exactly the interleaving the guard exists to catch, so the guard's
// verdict is load-bearing on the seam's answer and not on its own bookkeeping.
//
// (cmd/agent's scriptSource can be flipped the same way — its InputPending
// returning false is the red-proof recorded in devdoc/chat-small-height-flake.md;
// this control is the package-level twin, and it is what makes the RED-PROOF
// reproducible from inside internal/lineedit alone.)
func TestReadLoop_NoOrderingGateLetsTheDrawOvertake(t *testing.T) {
	h := newOrderingHarness(t)
	h.src.blind.Store(true)
	status := &testStatus{}
	h.ed.SetStatus(status.status)
	status.set(1) // the value the publish announces (orderingStatusNext)
	h.waitParked(t)
	h.window()
	h.queue(orderingTyped)
	h.ed.PublishStatus()
	h.release()

	h.waitStatusRow(t, orderingStatusNext)
	v := h.measure(orderingStatusNext)
	if v.drawIdx < 0 {
		t.Fatalf("no chunk carries the status text\n%s", h.dump())
	}
	if !v.overtook() {
		t.Fatalf("the control did NOT reproduce the interleaving: the draw landed at write index %d and the first keystroke was read at write index %d — a guard green here is a guard that cannot see the defect\n%s",
			v.drawIdx, v.firstReadWrites, h.dump())
	}
	t.Logf("negative control: the draw landed at write index %d with the first keystroke read only at write index %d — the seam's answer is what orders them",
		v.drawIdx, v.firstReadWrites)
}

// orderingSourceIsTheSeam is a compile-time proof that the guard's source stands
// on the same interface production implements, so the editor asks for it through
// the identical type assertion it uses for *Terminal.
var _ inputBacklog = (*guardedOrderingSource)(nil)
