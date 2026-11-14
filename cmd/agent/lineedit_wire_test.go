package main

import (
	"bytes"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// chatTestEpoch anchors the fake clock the input tests drive the editor's
// paste-burst windows with. The windows are milliseconds wide (Rule 1 names
// them), so a real clock would make these tests both slow and timing-dependent;
// time is a parameter here, which is also what makes the classification
// declarative rather than a sleep-and-hope.
const chatTestEpoch = 1_700_000_000

// scriptStep is one scripted wake-up of the input source: bytes, a resize, or an
// expired wait.
type scriptStep struct {
	data    []byte
	winch   bool
	timeout bool
	advance time.Duration
}

// recordingWriter keeps every write separately, so a test can replay the screen
// as it stood at any point (the cell-grid assertions read the rendered result,
// never "a write happened").
//
// It is MUTEX-PROTECTED because the editor's read loop writes from its own
// goroutine while the test replays: an unlocked harness is itself a data race
// under -race (measured), and a harness race hides the production one the test
// exists to find. Readers go through snapshot()/count(); fed (how far the
// screen has been replayed) belongs to the test goroutine alone.
type recordingWriter struct {
	mu     sync.Mutex
	writes [][]byte
	// fed counts the writes already replayed into a screen.
	fed int
}

// Write records one chunk verbatim.
func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, append([]byte(nil), p...))
	return len(p), nil
}

// ReplayInto feeds the screen every recorded write it has not seen yet, in
// order, and reports how many were fed.
//
// It is IDEMPOTENT by deliberately counting what has been fed: the screen is a
// STATEFUL grid and the recorded stream moves the cursor RELATIVELY, so feeding
// the same bytes a second time repaints an already-painted screen (the answer
// lands again instead of over it) rather than reproducing it. A test that
// replayed eagerly — every poll, and once more when it dumps the screen — would
// assert against one screen and quote a different one as its evidence. fed is
// touched by the test goroutine only; the writes themselves are read under mu.
func (w *recordingWriter) ReplayInto(s *lineedit.Screen) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for ; w.fed < len(w.writes); w.fed++ {
		s.Feed(w.writes[w.fed])
		n++
	}
	return n
}

// snapshot returns a copy of the writes recorded so far.
func (w *recordingWriter) snapshot() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([][]byte(nil), w.writes...)
}

// count is how many writes have been recorded.
func (w *recordingWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.writes)
}

// reset forgets every recorded write, so an assertion measures the DELTA a call
// produces rather than the editor's startup traffic. It also resets the replay
// cursor, so a rebuilt screen starts from an empty grid.
func (w *recordingWriter) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = nil
	w.fed = 0
}

// scriptSource is a lineedit.Source fed from a fixed script.
//
// It IS a lineedit.Notifier: a status publish writes one byte into a wake
// channel, which ReadEvent reports as EventWake — exactly the short-circuit the
// real terminal's wake pipe provides, so the tests exercise the SAME single-writer
// road production uses (publish → wake → the read loop draws).
type scriptSource struct {
	steps  []scriptStep
	width  int
	height int
	i      int
	clock  *testClock

	// holdAtEnd makes the source WAIT for more steps instead of reporting EOF
	// when the script runs out: the editor is then reading while the test types
	// (and asserts an intermediate screen). The zero value keeps the historic
	// EOF-at-the-end behaviour the paste-burst tests depend on. mu guards the
	// struct for the async shape, where the reader and the pushing test are
	// different goroutines.
	holdAtEnd bool
	mu        sync.Mutex
	wake      chan struct{}
	stop      chan struct{}
	stopped   bool

	// gate, when non-nil, holds every read until the test closes it. It is the
	// barrier that makes an ORDERING invariant assertable instead of sampled:
	// the test queues input and raises a publish while the read loop is parked
	// here, so the loop's next read really does see both and its choice of which
	// to act on first is observed deterministically. Set at construction (before
	// the loop starts), so it never races.
	gate chan struct{}
	// parkOnce guards closing parked (a read loop can reach the gate only once,
	// but the flag keeps that a property of the code rather than of the loop).
	parkOnce sync.Once
	// publishFirst makes ReadEvent report a pending PUBLISH wake ahead of the
	// steps it already holds — the ambiguous single wake the flake was about (a
	// status publish raised while keystrokes are queued). Off by default: the
	// script is then delivered in script order.
	publishFirst bool
	// writes reports how many chunks the editor has written so far. It is how
	// the harness records the ORDER of two independently-driven events: a step
	// delivered at write index N means the loop chose to read input before it
	// had emitted the N-th chunk, and the guard's question ("did the draw
	// overtake the input?") is answerable by comparing that index with the index
	// of the chunk that drew the status. Never nil in the harnesses that set it.
	writes func() int
	// firstStepWrites is the write count at the instant the FIRST scripted step
	// was delivered — the boundary the ordering guard reads.
	firstStepWrites int
	// parked is closed the first time a gated read reaches the gate — the
	// signal that the editor's boot frame has been written and the loop is
	// waiting for a read. A test that must control the ORDER of a publish and a
	// keystroke starts its window there, so the boot repaint cannot race it.
	// nil when the source is not gated.
	parked chan struct{}
	// raised carries the publishes raised while publishFirst is on, so
	// ReadEvent can report one ahead of queued input. Consumed when reported,
	// exactly as Terminal.ReadEvent drains its wake pipe before returning
	// EventWake — the pending DRAW flag is what survives for a late draw.
	raised chan struct{}

	// winchCount counts the resize notifications the source delivered.
	winchCount int

	// ready is closed the first time the read loop reads from this source (see
	// loopReady). Created lazily under mu, so a harness may take the channel
	// before or after the loop starts without a race.
	ready chan struct{}
	// readyOnce guards the close (ReadEvent is reached again on every read of a
	// live loop; only the first one is the boot fact).
	readyOnce sync.Once
}

// InputPending implements the editor's input-readiness seam (lineedit's
// inputBacklog): a step the script has already handed over is waiting to be
// read, so the read loop reads it BEFORE it draws a publish.
//
// It is the harness's half of a real ordering guarantee, not a test convenience:
// the source serves a keystroke and a publish wake from ONE channel, so a single
// EventWake can report both, and the loop must not draw input it has not read.
// Without this the small-height chat test asserted the editing row while the
// last scripted keystroke was still unread (measured: the row read "you> hel"
// while the buffer said "hello" 2.2ms later), which is the flake this closes.
func (s *scriptSource) InputPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.i < len(s.steps)
}

// consumed is how many scripted steps the editor has READ. It is the harness's
// record of what the read loop has actually taken off the wire, which is what
// makes the ordering invariant in the flake test's regression guard assertable
// (the source's index is advanced under mu by the reader goroutine).
func (s *scriptSource) consumed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.i
}

// Notify wakes a blocked read (lineedit.Notifier). It shares the source's own
// nudge channel with push(): both mean "there is something to re-examine", and
// one pending nudge is the same information as two — exactly the real pipe's
// behaviour. A non-blocking send keeps a publisher that outruns the reader from
// ever parking on the editor.
func (s *scriptSource) Notify() {
	if s.raised != nil {
		select {
		case s.raised <- struct{}{}:
		default: // one pending publish is the same information as two
		}
	}
	s.mu.Lock()
	s.initLocked()
	wake := s.wake
	s.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

// consumeRaise reports (and consumes) a publish raised while publishFirst is on.
func (s *scriptSource) consumeRaise() bool {
	if s.raised == nil {
		return false
	}
	select {
	case <-s.raised:
		return true
	default:
		return false
	}
}

// NotifyEnabled reports true: this source really does deliver the wake.
func (s *scriptSource) NotifyEnabled() bool { return true }

// loopReady is closed the FIRST time ReadEvent is reached, i.e. the first time
// the read loop actually asks the source for input. ReadEvent has exactly one
// caller — Editor.Run, the read loop — so the close is a fact about that
// goroutine and not a guess, and it is the observable the harnesses wait on
// before writing into a layout the loop owns.
//
// WHY A HARNESS MUST WAIT FOR IT. Every band row and every answer call is
// ROUTED to the read loop, and the routing decision is a goroutine identity
// (Editor.loopGoid, bandwait.go): while the loop has not entered Run,
// loopGoid == 0, and WriteBandRow's arm for that state QUEUES the row and
// returns true without drawing (the frame stays a LEVEL the loop's next read
// outcome re-derives). A harness that writes from the test goroutine in that
// window therefore reads an EMPTY grid back — measured on this very harness:
// writes before=2 at the write, 9 only after the loop ran, and the row the
// test then asserted was missing from the grid entirely. The row is LATE,
// never lost, so the fix is to stop SAMPLING the boot window and wait for the
// loop to own the renderer.
//
// The window is real and it is not hypothetical: loopGoid is cleared when Run
// returns and set at its start, and a goroutine started by `go func()` is not
// scheduled synchronously, so between `go in.ReadLine()` and the loop's first
// Run the test goroutine has the editor to itself.
func (s *scriptSource) loopReady() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ready == nil {
		s.ready = make(chan struct{})
	}
	return s.ready
}

// markLoopReady closes the boot signal; called from ReadEvent, whose only
// caller is Editor.Run.
func (s *scriptSource) markLoopReady() {
	s.mu.Lock()
	if s.ready == nil {
		s.ready = make(chan struct{})
	}
	ready := s.ready
	s.mu.Unlock()
	s.readyOnce.Do(func() { close(ready) })
}

// ReadEvent delivers the next scripted step.
func (s *scriptSource) ReadEvent(buf []byte, timeoutMS int) (lineedit.Event, error) {
	s.markLoopReady()
	if s.gate != nil {
		if s.parked != nil {
			s.parkOnce.Do(func() { close(s.parked) })
		}
		<-s.gate
	}
	if s.publishFirst && s.consumeRaise() {
		return lineedit.Event{Kind: lineedit.EventWake}, nil
	}
	for {
		s.mu.Lock()
		if s.wake != nil {
			select {
			case <-s.wake:
				s.mu.Unlock()
				return lineedit.Event{Kind: lineedit.EventWake}, nil
			default:
			}
		}
		if s.i < len(s.steps) {
			st := s.steps[s.i]
			s.i++
			if s.i == 1 && s.writes != nil {
				// The FIRST step is the ordering boundary: record what the
				// editor had already written when it chose to read input.
				s.firstStepWrites = s.writes()
			}
			s.mu.Unlock()
			return s.deliver(buf, st)
		}
		if !s.holdAtEnd {
			s.mu.Unlock()
			return lineedit.Event{Kind: lineedit.EventInput, EOF: true}, nil
		}
		if timeoutMS >= 0 {
			// A BOUNDED wait is served the instant the script has nothing more
			// to deliver: the real source would expire it after timeoutMS, and
			// the editor's handling of the expiry is identical. It advances the
			// clock by that wait — exactly what poll(2) would have done — and
			// yields, which is the park that lets the completion WORKER
			// goroutine run (an instant expiry would otherwise starve it). The
			// editor bounds how long it keeps asking, so this cannot spin.
			if s.clock != nil {
				s.clock.advance(time.Duration(timeoutMS) * time.Millisecond)
			}
			s.mu.Unlock()
			runtime.Gosched()
			return lineedit.Event{Kind: lineedit.EventNone}, nil
		}
		s.initLocked()
		wake, stop := s.wake, s.stop
		s.mu.Unlock()
		select {
		case <-wake:
			// A wake means one of TWO things, and they must not be confused:
			// an async push added a step (deliver it), or a publisher raised a
			// bare wake (a status publish — report it as such). Consuming the
			// wake and re-parking would SWALLOW the publish: the token is gone,
			// there is no step to find, and the reader blocks again — the very
			// defect this source exists to let the tests catch.
			s.mu.Lock()
			more := s.i < len(s.steps)
			s.mu.Unlock()
			if more {
				continue
			}
			return lineedit.Event{Kind: lineedit.EventWake}, nil
		case <-stop:
			return lineedit.Event{Kind: lineedit.EventInput, EOF: true}, nil
		}
	}
}

// deliver turns one scripted step into the event the editor sees.
func (s *scriptSource) deliver(buf []byte, st scriptStep) (lineedit.Event, error) {
	if s.clock != nil && st.advance > 0 {
		s.clock.advance(st.advance)
	}
	switch {
	case st.winch:
		s.mu.Lock()
		s.winchCount++
		s.mu.Unlock()
		return lineedit.Event{Kind: lineedit.EventWinch}, nil
	case st.timeout:
		return lineedit.Event{Kind: lineedit.EventNone}, nil
	default:
		return lineedit.Event{Kind: lineedit.EventInput, Bytes: copy(buf, st.data)}, nil
	}
}

// initLocked lazily creates the async channels (caller holds mu).
func (s *scriptSource) initLocked() {
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
		s.stop = make(chan struct{})
	}
}

// push appends one step, waking a reader that is waiting for input.
func (s *scriptSource) push(st scriptStep) {
	s.mu.Lock()
	s.initLocked()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.steps = append(s.steps, st)
	wake := s.wake
	s.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default: // already nudged — the reader re-checks the step list either way
	}
}

// stopSource releases a reader blocked on input (test cleanup).
func (s *scriptSource) stopSource() {
	s.mu.Lock()
	s.initLocked()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stop)
	s.mu.Unlock()
}

// Width reports the scripted width.
func (s *scriptSource) Width(fallback int) int {
	if s.width > 0 {
		return s.width
	}
	return fallback
}

// Height reports the scripted terminal height (the A5 layout needs one).
func (s *scriptSource) Height(fallback int) int {
	if s.height > 0 {
		return s.height
	}
	return fallback
}

// testClock is a manually advanced clock.
//
// It is MUTEX-PROTECTED because it is read and written from different
// goroutines: the editor's read loop advances it (a scripted step's advance, a
// bounded wait's expiry) while the test goroutine's own writes advance it
// through the answer renderer's cadence check. An unlocked clock is a real data
// race under -race (measured), and a harness race hides the production one the
// test exists to find.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

// Now is the current fake time.
func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// advance moves the fake clock forward.
func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newTestInput builds an editor-backed chatInput over a scripted source.
func newTestInput(t *testing.T, steps []scriptStep, width int) (*chatInput, *bytes.Buffer) {
	t.Helper()
	clock := &testClock{t: time.Unix(chatTestEpoch, 0)}
	src := &scriptSource{steps: steps, width: width, clock: clock}
	out := &bytes.Buffer{}
	in := newChatInputWithSource(src, out, chatPrompt)
	in.editor.SetClock(clock.Now)
	return in, out
}

// chatInputRounds bounds the line-reading loops below (a runaway guard for a
// broken editor, not a behaviour value).
const chatInputRounds = 32

// readLines drains the input source, returning every line it yields.
func readLines(t *testing.T, in *chatInput) []string {
	t.Helper()
	var lines []string
	for i := 0; i < chatInputRounds; i++ {
		line, ok, _, err := in.ReadLine()
		if err != nil {
			t.Fatalf("ReadLine: %v", err)
		}
		if !ok {
			return lines
		}
		lines = append(lines, line)
	}
	t.Fatalf("input source did not reach EOF within %d lines", chatInputRounds)
	return nil
}

// lineeditBurst is the third-arg noise protection for gofmt: the burst constant
// referenced by the tests below lives in internal/lineedit; re-exporting it here
// is what keeps the window's VALUE out of this file (Rule 1: one definition).
var lineeditBurst = lineedit.PasteEnterSuppressWindow

// TestChatPasteBurst_ThreeLinePasteIsOneMessage is the A3 regression: a 3-line
// block pasted in one burst must yield EXACTLY ONE input line, carrying all
// three lines — the defect being fixed is that it used to yield three, i.e.
// three model turns. The burst ends with a real Enter (after the window), which
// is what a user does after pasting.
func TestChatPasteBurst_ThreeLinePasteIsOneMessage(t *testing.T) {
	pasted := "line one\nline two\nline three\n"
	steps := []scriptStep{
		// A real terminal delivers a pasted block as ONE read, newlines and all.
		{data: []byte(pasted)},
		{timeout: true, advance: time.Second},
		// The user presses Enter to send it, well after the burst window.
		{data: []byte("\r")},
	}
	in, out := newTestInput(t, steps, 80)

	lines := readLines(t, in)
	if len(lines) != 1 {
		t.Fatalf("a 3-line paste produced %d input lines (%q), want exactly 1 — the pasted block is ONE message (this is the \"pasted N-line block = N model turns\" defect)", len(lines), lines)
	}
	if lines[0] != pasted {
		t.Fatalf("pasted message = %q, want %q (all three lines, order preserved)", lines[0], pasted)
	}
	if got := strings.Count(strings.TrimRight(lines[0], "\n"), "\n"); got != 2 {
		t.Fatalf("message carries %d separating newlines, want 2 (three lines)", got)
	}
	for _, want := range []string{"line one", "line two", "line three"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the rendered screen never showed %q:\n%s", want, out.String())
		}
	}
}

// TestChatPasteBurst_CRLFPasteIsOneMessage is the same defect in the shape a
// real CRLF paste takes: with ICRNL cleared, EVERY pasted CR arrives as the
// Enter byte, 40 microseconds apart. All three CRs must be content.
func TestChatPasteBurst_CRLFPasteIsOneMessage(t *testing.T) {
	pasted := "alpha\r\nbravo\r\ncharlie\r\n"
	steps := []scriptStep{
		// One read: 8 ms between the pasted characters is the burst's own pace,
		// and the trailing CR sits at the end of it.
		{data: []byte(pasted)},
		{timeout: true, advance: time.Second},
		{data: []byte("\r")},
	}
	in, _ := newTestInput(t, steps, 80)

	lines := readLines(t, in)
	if len(lines) != 1 {
		t.Fatalf("CRLF paste produced %d input lines (%q), want exactly 1", len(lines), lines)
	}
	if lines[0] != "alpha\nbravo\ncharlie\n" {
		t.Fatalf("CRLF paste message = %q, want %q", lines[0], "alpha\nbravo\ncharlie\n")
	}
}

// TestChatPasteBurst_LoneEnterClassified is the exactness half of the same
// window: a paste made of newlines, whose classification legitimately needs the
// 120 ms window to expire, must produce TWO lines — not one merged message, and
// not a session that has to be killed. A keystroke after the burst resolves the
// ambiguity immediately, which is what a real terminal does (typing follows the
// paste).
func TestChatPasteBurst_LoneEnterClassified(t *testing.T) {
	steps := []scriptStep{
		{data: []byte("hi")},
		// A real pause, then Enter: one character-arrival is far below the
		// burst minimum, so this Enter is the user's submit.
		{timeout: true, advance: time.Second},
		{data: []byte("\r")},
		// And a second, empty submit (the user pressed Enter again).
		{timeout: true, advance: time.Second},
		{data: []byte("\r")},
	}
	in, _ := newTestInput(t, steps, 80)

	lines := readLines(t, in)
	if len(lines) != 2 {
		t.Fatalf("two lone Enters produced %d lines (%q), want exactly 2 user messages", len(lines), lines)
	}
	if lines[0] != "hi" {
		t.Fatalf("first line = %q, want %q", lines[0], "hi")
	}
	if lines[1] != "" {
		t.Fatalf("second line = %q, want an empty line", lines[1])
	}
}

// TestChatPasteBurst_TypingStillSubmitsOnEnter is the guard against
// over-suppression: ordinary typing followed by Enter must submit on that Enter
// with no paste window anywhere near it.
func TestChatPasteBurst_TypingStillSubmitsOnEnter(t *testing.T) {
	var steps []scriptStep
	for _, ch := range "hello there" {
		steps = append(steps, scriptStep{data: []byte(string(ch)), advance: 60 * time.Millisecond})
	}
	steps = append(steps, scriptStep{data: []byte("\r")})
	in, _ := newTestInput(t, steps, 80)

	lines := readLines(t, in)
	if len(lines) != 1 || lines[0] != "hello there" {
		t.Fatalf("typed line = %q, want exactly [\"hello there\"]", lines)
	}
}

// TestChatPasteBurst_MultilineEditCommands exercises the A1 motions through the
// real key path (escape sequences, not direct Value calls): the buffer is edited
// with arrows/word motions/kill+yank and the rendered screen is checked, so a
// binding wired to the wrong action is caught.
func TestChatPasteBurst_MultilineEditCommands(t *testing.T) {
	// Every keystroke is separated by a human-scale pause, which is what keeps
	// a run of escape sequences from looking like a paste burst.
	const typingPause = 60 * time.Millisecond
	steps := []scriptStep{
		{data: []byte("hello world")},
		{timeout: true, advance: time.Second},
		{data: []byte("\x1b[1;5D"), advance: typingPause}, // Ctrl+Left: word left
		{data: []byte("\x1b[3~"), advance: typingPause},   // Delete: removes 'w'
		{data: []byte("\x15"), advance: typingPause},      // Ctrl+U: kill to start
		{data: []byte("\x19"), advance: typingPause},      // Ctrl+Y: yank it back
		{data: []byte("\r"), advance: typingPause},
	}
	in, out := newTestInput(t, steps, 80)

	lines := readLines(t, in)
	if len(lines) != 1 {
		t.Fatalf("lines = %q, want one", lines)
	}
	if lines[0] != "hello orld" {
		t.Fatalf("edited line = %q, want %q (word-left, delete, kill, yank)", lines[0], "hello orld")
	}
	if !strings.Contains(out.String(), "hello orld") {
		t.Fatalf("redraw never rendered the edited buffer:\n%s", out.String())
	}
}
