package lineedit

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests here pin the SINGLE-WRITER contract of the status row — the defect
// the publish path exists to remove. Its shape (measured in cmd/agent, round 7):
// the caller refreshed the status from ITS OWN goroutine (StatusTick →
// AnswerRenderer.drawStatus → an absolute cursor placement plus text) while the
// editor's read loop was mid-repaint on the same fd. Two goroutines, two byte
// streams of absolute cursor motion, one terminal.
//
// A locking test cannot see that: the bytes are not corrupted, the ORDER is. So
// these assertions are about WHO writes and how much — never about "a write
// happened".

const (
	// statusPublishStorm is how many publishes a storm test issues: far more
	// than the single draw they are allowed to produce.
	statusPublishStorm = 50
	// statusKeystrokeStorm is how many keystrokes race the publishes.
	statusKeystrokeStorm = 20
	// statusTestRows / statusTestCols is the geometry the harness opens at (the
	// reserved layout needs RowsMinUsable rows and a row wide enough for the
	// status text).
	statusTestRows = 12
	statusTestCols = 48
	// statusDrawWait bounds waiting for the read loop's draw. Generous: it is a
	// synchronization wait, and a passing run never comes near it.
	statusDrawWait = 3 * time.Second
	// statusPollStep is how long such a wait sleeps between checks.
	statusPollStep = time.Millisecond
	// testStatusText / testStatusLive are the status values the assertions read.
	testStatusText = "status-source"
	testStatusLive = "ready"
)

// TestStatusPublishWritesNothingFromThePublisher is the defect's direct guard:
// a publish issued by ANOTHER goroutine must produce ZERO bytes. The old design
// wrote the whole status row (cursor placement + erase + text) from the caller —
// exactly the second byte stream the fix removes.
//
// The read loop is STOPPED first, so the only goroutine that could write is the
// publisher: with the loop running, a write observed after a publish would prove
// nothing either way (the loop legitimately draws it). This makes the assertion
// about the CALLER, which is where the defect lived.
func TestStatusPublishWritesNothingFromThePublisher(t *testing.T) {
	h := newStatusHarness(t)
	h.stopAndWait()
	h.drainWrites()

	for i := 0; i < statusPublishStorm; i++ {
		h.ed.PublishStatus()
	}
	if got := h.out.count(); got != 0 {
		t.Fatalf("publishing the status wrote %d chunk(s) outside the read loop — the caller is a second writer on the terminal fd (want 0)", got)
	}
	// The publish is not DROPPED either: the flag stays raised for the loop.
	if !h.answer.StatusPublishPending() {
		t.Fatal("the publish left no pending flag — the read loop would never draw the status")
	}
}

// TestStatusPublishIsDrawnByTheReadLoop is the other half: the read loop DOES
// draw what a publisher raised, with no keystroke to trigger it. Without this
// the fix would be "nothing writes" rather than "exactly one writer writes" —
// and the row would go stale during a turn, which is the row's whole purpose.
func TestStatusPublishIsDrawnByTheReadLoop(t *testing.T) {
	h := newStatusHarness(t)
	status := &testStatus{}
	h.ed.SetStatus(status.status)

	h.ed.PublishStatus()
	h.waitRow(t, testStatusLive)
	if !strings.Contains(h.lastRow(), testStatusText) {
		t.Fatalf("the status row = %q, want it to carry the published status %q\n--- screen ---\n%s",
			h.lastRow(), testStatusText, h.contents())
	}
}

// TestStatusPublishDoesNotRepaintUnchangedValues pins the bounds: the read loop
// draws on every publish, so a publish storm of IDENTICAL values must not become
// a write storm (the turn loop publishes at every turn boundary, and an
// unbounded repaint would then rewrite the row on every one of them).
func TestStatusPublishDoesNotRepaintUnchangedValues(t *testing.T) {
	h := newStatusHarness(t)
	status := &testStatus{}
	h.ed.SetStatus(status.status)

	h.ed.PublishStatus()
	h.waitRow(t, testStatusLive)
	draws := h.answer.StatusWrites()
	h.drainWrites()

	// A storm of publishes whose VALUE does not change.
	for i := 0; i < statusPublishStorm; i++ {
		h.ed.PublishStatus()
	}
	// Drain proof: the last publish changes the value, so seeing it on the row
	// proves the loop consumed EVERY publish queued before it (they are raised in
	// order, and the flag is sticky — nothing can be skipped over).
	status.set(1)
	h.ed.PublishStatus()
	h.waitRow(t, testStatusLive+" #1")

	if got := h.answer.StatusWrites(); got != draws+1 {
		t.Fatalf("the status row was DRAWN %d time(s) for %d publishes of an unchanged value plus 1 change, want 1 — one draw per VISIBLE change, not per publish",
			got-draws, statusPublishStorm)
	}
}

// TestStatusPublishSurvivesAConcurrentRepaint is the two-goroutine shape that
// produced the defect: publishes landing while the read loop draws the editing
// line. Every recorded chunk must be a WHOLE sequence (a cursor placement, an
// erase-to-end-of-line, or the scroll newline) — a status draw split across two
// chunks, or spliced into a keystroke repaint, would show up as a fragment.
func TestStatusPublishSurvivesAConcurrentRepaint(t *testing.T) {
	h := newStatusHarness(t)
	status := &testStatus{}
	h.ed.SetStatus(status.status)

	const publishes = statusPublishStorm
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < publishes; i++ {
			status.set(i)
			h.ed.PublishStatus()
		}
	}()
	for i := 0; i < statusKeystrokeStorm; i++ {
		h.src.push(scriptStep{data: []byte("a")})
	}
	wg.Wait()
	// Drain proof: a final CHANGED publish, seen on the row, proves every earlier
	// one was consumed too.
	status.set(statusPublishStorm)
	h.ed.PublishStatus()
	h.waitRow(t, testStatusLive+" #"+strconv.Itoa(statusPublishStorm))

	for i, w := range h.out.snapshot() {
		s := string(w)
		switch {
		case s == "":
		case strings.HasPrefix(s, escCSI), strings.HasPrefix(s, escNewline):
		default:
			t.Fatalf("write %d does not begin a sequence: %q — a draw was interleaved into another (two writers on one fd)", i, s)
		}
	}
	// The line really was drawn (the loop was repainting, not idle). The editing
	// row comes from the LAYOUT, never from an assumption: the reserved layout
	// anchors the answer band at the screen's FIRST row (the scrollback
	// condition), so the editing line is NOT row 0 and asserting on row 0 would
	// read the band instead.
	layout, ok := h.ed.Layout()
	if !ok {
		t.Fatal("the reserved layout is not installed — the editing row cannot be located")
	}
	// The keystrokes reach the row through the SAME read loop that is drawing the
	// storm, so the wait is on the assertion's own subject rather than on the
	// storm's last draw: the loop can draw a publish while the push()ed keys are
	// still unread (measured flaky on master — 1 of 3 full-package runs), and an
	// assertion taken at that instant reads a row the loop has not finished.
	deadline := time.Now().Add(statusDrawWait)
	for {
		h.replay()
		if strings.Contains(h.screen.Line(layout.EditorTop), "a") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the editing row = %q, want the storm's keystrokes on it — the read loop drew the publishes but never the keys",
				h.screen.Line(layout.EditorTop))
		}
		//stylegate:allow-sleep bounded poll interval while the read loop draws the storm's publishes; the deadline above owns the failure
		time.Sleep(statusPollStep)
	}
}

// testStatus is a status source the publisher mutates: its field is written
// under a mutex and read under the same one, so the composed line is atomic
// rather than torn (a torn line would render as a fragment on the row).
type testStatus struct {
	mu sync.Mutex
	n  int
}

// set replaces the counter the status text is composed from.
func (s *testStatus) set(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n = n
}

// status renders the source's current value.
func (s *testStatus) status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := testStatusLive
	if s.n > 0 {
		live += " #" + strconv.Itoa(s.n)
	}
	return Status{Label: testStatusText, Live: live}
}

// statusHarness is an editor whose read loop is RUNNING — the real shape: the
// publisher is never the reader.
type statusHarness struct {
	ed     *Editor
	out    *recordingWriter
	src    *scriptedSource
	screen *Screen
	answer *AnswerRenderer
	// done is closed when the read loop returns (stopAndWait).
	done chan struct{}
}

// newStatusHarness starts an editor reading, with the reserved layout enabled.
func newStatusHarness(t *testing.T) *statusHarness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: statusTestCols, height: statusTestRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	ed := NewChatEditor(src, out, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", statusTestRows)
	}
	if !ed.NotifyEnabled() {
		t.Fatal("the harness's source supplied no wake seam — a publish could never reach the read loop, and every wait below would time out for the wrong reason")
	}
	h := &statusHarness{ed: ed, out: out, src: src, screen: NewScreen(statusTestCols, statusTestRows), answer: ed.Answer(), done: make(chan struct{})}
	go func() {
		defer close(h.done)
		_, _ = ed.Run() //stylegate:ignore the harness's teardown: a real failure surfaces as a bounded waitRow timeout that dumps the screen
	}()
	t.Cleanup(func() {
		// Stop the SOURCE and wait for the read loop to return BEFORE tearing the
		// editor down: DisableRegion mutates the region, and the loop reads it,
		// so closing first would race the harness itself (measured under -race).
		src.stopSource()
		select {
		case <-h.done:
		case <-time.After(statusDrawWait):
		}
		ed.Close()
	})
	return h
}

// stopAndWait ends the read loop and waits for it to return, so a test can
// attribute every subsequent write to a publisher rather than to the loop.
func (h *statusHarness) stopAndWait() {
	h.src.stopSource()
	select {
	case <-h.done:
	case <-time.After(statusDrawWait):
		h.replay()
		h.ed.Close()
		h.src.stopSource()
		panic("the editor's read loop did not return after its source stopped — cannot attribute writes")
	}
}

// drainWrites forgets the writes recorded so far: the assertions measure the
// DELTA a publish produces, never the editor's startup traffic.
func (h *statusHarness) drainWrites() {
	h.out.mu.Lock()
	defer h.out.mu.Unlock()
	h.out.writes = nil
}

// replay renders the write log onto a FRESH screen — the read loop writes while
// the test reads, so an incremental feed would apply the already-seen writes
// again.
func (h *statusHarness) replay() {
	screen := NewScreen(statusTestCols, statusTestRows)
	for _, w := range h.out.snapshot() {
		screen.Feed(w)
	}
	h.screen = screen
}

// contents is a failure dump.
func (h *statusHarness) contents() string {
	h.replay()
	return h.screen.Contents()
}

// lastRow is the bottom screen row, where the layout reserves the status.
func (h *statusHarness) lastRow() string {
	h.replay()
	return h.screen.Line(statusTestRows - 1)
}

// waitRow waits until the reserved status row carries want. A value ON THE ROW
// is the drain proof the publish tests need: publishes are consumed in order and
// the pending flag is sticky, so seeing the LAST one drawn means every earlier
// one was drawn too (or coalesced into it).
func (h *statusHarness) waitRow(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(statusDrawWait)
	for time.Now().Before(deadline) {
		if strings.Contains(h.lastRow(), want) {
			return
		}
		time.Sleep(statusPollStep) //stylegate:allow-sleep bounded synchronization wait on the editor's read loop (see statusDrawWait)
	}
	t.Fatalf("timed out waiting for %q on the status row (draws so far: %d)\n--- screen ---\n%s",
		want, h.answer.StatusWrites(), h.contents())
}
