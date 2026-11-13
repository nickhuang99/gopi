package lineedit

import (
	"os"

	"golang.org/x/sys/unix"
	"sync"
	"testing"
	"time"
)

// scriptedSource is a terminalSource fed from a fixed script of steps: a chunk
// of input bytes, a resize notification, or a wait that expires with no input.
//
// It exists so the editor's POLICY — most importantly the paste-burst
// classification that decides whether a newline submits — is asserted without a
// pty. Only the ioctl layer (raw/termios) stays outside this test's reach, and
// that layer is exercised by the command-level tests over a real pty when one is
// available.
type scriptedSource struct {
	steps  []scriptStep
	width  int
	height int
	i      int
	clock  *fakeClock
	// timeouts records every timeout (ms) the editor asked for, so a test can
	// assert the editor BOUNDED its wait while a suppressed Enter was pending
	// instead of blocking forever on an ambiguity it could resolve.
	timeouts []int
	// winchCount counts the resize notifications the editor consumed.
	winchCount int

	// holdAtEnd makes the source WAIT for more steps instead of reporting EOF
	// when the script runs out. That is the ASYNC shape: the editor is already
	// reading while the test types (and asserts intermediate screen states).
	// The zero value keeps the historic behaviour (EOF at the end of the
	// script), which the synchronous tests depend on. mu guards the whole
	// struct: in the async shape the reader goroutine and the pushing test
	// goroutine touch it concurrently.
	holdAtEnd bool
	mu        sync.Mutex
	wake      chan struct{}
	stop      chan struct{}
	stopped   bool

	// notifyCh carries the editor's completion notifications (production: the
	// terminal's notification pipe). The scripted source stands in for the
	// terminal, so it owns the pipe as well and watches it alongside its
	// script. One long-lived goroutine does the watching (never one per read),
	// and it ends when the test closes the pipe.
	notifyCh chan struct{}
	// notifyPipeW is the pipe's WRITE end, installed by setNotifyPipe. A source
	// that owns a pipe must deliver its notifications THROUGH it (Notify): the
	// pipe is what a PARKED reader watches, and a notification that took the
	// in-process channel instead was never seen by a reader that had already
	// decided to block (measured — the EscapeClosesTheMenu flake).
	notifyPipeW *os.File

	// notify is the PUBLISH channel, distinct from wake (which means "a step was
	// added"). Two different meanings on one channel would let a publish be
	// consumed by a reader looking for a step and then parked again — the
	// publish would vanish with no trace. It is the scripted stand-in for the
	// Notifier seam (Notify/NotifyEnabled); notifyCh above is the real pipe.
	notify chan struct{}
}

// Notify wakes a blocked read (Notifier): a status publish, not a step.
//
// When this source stands in for a TERMINAL it OWNS the notification pipe, and
// the notification must travel through THAT — it is the mechanism the terminal's
// own wakePipe provides, and the reader watches the pipe directly. Sending on
// the in-process channel instead routes the wake to a channel no PARKED reader
// selects on: measured (this package's EscapeClosesTheMenu flake) a completion
// publish raised while the loop was parked in a blocking read was never seen —
// the menu stayed published and open with drawn=1, wakes=0 and every read
// timeout -1. The channel is the fallback for a source with no pipe.
func (s *scriptedSource) Notify() {
	s.mu.Lock()
	s.initLocked()
	notify, pipeW := s.notify, s.notifyPipeW
	s.mu.Unlock()
	if pipeW != nil {
		// One byte is the whole vocabulary (wakePipeBytes in production): a pipe
		// that refuses the byte because it is already full carries the same
		// information, so the write is best-effort by contract.
		_, _ = pipeW.Write([]byte{0}) //stylegate:ignore best-effort notification: a full pipe already holds an unread notification
		return
	}
	select {
	case notify <- struct{}{}:
	default: // a pending wake is the same information as a second one
	}
}

// NotifyEnabled reports true: this source really does deliver the wake.
func (s *scriptedSource) NotifyEnabled() bool { return true }

// scriptStep is one scripted wake-up.
type scriptStep struct {
	data    []byte
	winch   bool
	timeout bool          // a wait that expired with no input
	advance time.Duration // clock movement applied before this step is delivered
	// width, when > 0, changes the scripted geometry BEFORE the step is
	// delivered — a resize, as the source would report it after SIGWINCH.
	width int
}

// ReadEvent delivers the next scripted step.
func (s *scriptedSource) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	// A pending completion notification is delivered BEFORE the wait: it is the
	// "look again" signal, and poll(2) would report it as an immediate wake.
	if s.drainNotify() {
		return Event{Kind: EventWake}, nil
	}
	for {
		s.mu.Lock()
		s.timeouts = append(s.timeouts, timeoutMS)
		if s.i < len(s.steps) {
			st := s.steps[s.i]
			s.i++
			s.mu.Unlock()
			return s.deliver(buf, st)
		}
		if !s.holdAtEnd {
			s.mu.Unlock()
			return Event{Kind: EventInput, EOF: true}, nil
		}
		if timeoutMS >= 0 {
			// A STOP MUST BE OBSERVABLE FROM A BOUNDED WAIT. The editor's own
			// waits are usually unbounded, but a Source DECORATOR (lineedit's key
			// injector) slices the inner read so it can deliver injected bytes
			// while the editor is parked — and under a slice a stop delivered
			// only through the unbounded channel path would never be seen, so a
			// harness teardown would hang. The check is a non-blocking peek that
			// leaves the historic behaviour EXACTLY as it was when no stop is
			// pending, which is why it is safe beside every existing test.
			s.initLocked()
			stop := s.stop
			s.mu.Unlock()
			select {
			case <-stop:
				return Event{Kind: EventInput, EOF: true}, nil
			default:
			}
			s.mu.Lock()
			if s.clock != nil {
				s.clock.advance(time.Duration(timeoutMS) * time.Millisecond)
			}
			s.mu.Unlock()
			time.Sleep(scriptedWaitBudget) //stylegate:allow-sleep standing in for poll(2)'s park — the editor's own bounded wait, and the only thing that lets the completion worker run
			return Event{Kind: EventNone}, nil
		}
		s.initLocked()
		s.initLocked()
		wake, stop, notify, notifyCh := s.wake, s.stop, s.notify, s.notifyCh
		s.mu.Unlock()
		// The notification is watched ALONGSIDE the script wake, which is the
		// property production relies on: a completion publish must be able to
		// interrupt a blocked read, not merely be noticed by the next one.
		if notifyCh != nil {
			select {
			case <-wake:
			case <-stop:
				return Event{Kind: EventInput, EOF: true}, nil
			case <-notifyCh:
				// The notification must reach the EDITOR, not merely unblock
				// this inner loop: swallowing it here left a published menu
				// undrawn (the reader had already passed its own check).
				return Event{Kind: EventWake}, nil
			}
			continue
		}
		select {
		case <-notify:
			// A publish: report it so the read loop can draw the status.
			return Event{Kind: EventWake}, nil
		case <-wake:
			// A step was added: loop and deliver it.
		case <-stop:
			return Event{Kind: EventInput, EOF: true}, nil
		}
	}
}

// deliver turns one scripted step into the event the editor sees.
func (s *scriptedSource) deliver(buf []byte, st scriptStep) (Event, error) {
	if s.clock != nil && st.advance > 0 {
		s.clock.advance(st.advance)
	}
	s.mu.Lock()
	if st.width > 0 {
		s.width = st.width
	}
	s.mu.Unlock()
	switch {
	case st.winch:
		s.mu.Lock()
		s.winchCount++
		s.mu.Unlock()
		return Event{Kind: EventWinch}, nil
	case st.timeout:
		return Event{Kind: EventNone}, nil
	default:
		return Event{Kind: EventInput, Bytes: copy(buf, st.data)}, nil
	}
}

// initLocked lazily creates the async channels (caller holds mu).
func (s *scriptedSource) initLocked() {
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
		s.stop = make(chan struct{})
	}
	if s.notify == nil {
		s.notify = make(chan struct{}, 1)
	}
}

// push appends one step, waking a reader that is waiting for input. It is the
// async counterpart of building the whole script up front, and it is what lets
// a test assert an intermediate SCREEN state.
func (s *scriptedSource) push(st scriptStep) {
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

// stopSource releases a reader blocked on input (the test's cleanup).
func (s *scriptedSource) stopSource() {
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
func (s *scriptedSource) Width(fallback int) int {
	if s.width > 0 {
		return s.width
	}
	return fallback
}

// Height reports the scripted height (a chat region needs one).
func (s *scriptedSource) Height(fallback int) int {
	if s.height > 0 {
		return s.height
	}
	return fallback
}

// fakeClock drives every time window in the editor. The paste-burst windows are
// milliseconds wide, so a real clock would make these tests both slow and
// flaky — the windows are policy, and policy is what is being asserted.
type fakeClock struct{ t time.Time }

// Now is the current fake time.
func (c *fakeClock) Now() time.Time { return c.t }

// advance moves the fake clock forward.
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// recordingWriter keeps every write separately, so a test can replay the screen
// as it stood at any point — in particular at the LAST REDRAW, which is the
// state the user is looking at while editing (the submit path's finishing
// newline is a separate write).
//
// The editor writes from ITS OWN goroutine while the test reads (the A7 tests
// poll the screen for a redraw that has no keystroke behind it), so the record
// is mutex-protected: an unsynchronized append/read pair is a data race, and it
// is not only theoretical — the reader could also miss a chunk that was written
// an instant ago, which is exactly the redraw the poll was waiting for. A
// harness race hides the production one the test exists to find, so every reader
// goes through snapshot() (measured under -race).
type recordingWriter struct {
	mu     sync.Mutex
	writes [][]byte
}

// Write records one chunk verbatim.
func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, append([]byte(nil), p...))
	return len(p), nil
}

// snapshot returns a copy of the recorded chunks in order — the caller may
// replay them freely (the chunks themselves are never mutated after Write).
func (w *recordingWriter) snapshot() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([][]byte(nil), w.writes...)
}

// count is how many chunks have been recorded — the repaint-count assertions'
// input, read without copying the chunks.
func (w *recordingWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.writes)
}

// upToLastRedraw replays every write except the final one (the finish), so a
// test reads the editing screen with the cursor placed where the user sees it.
func (w *recordingWriter) upToLastRedraw() []byte {
	var all []byte
	chunks := w.snapshot()
	last := len(chunks)
	if last > 0 {
		last-- // drop the final (finish) write
	}
	for _, chunk := range chunks[:last] {
		all = append(all, chunk...)
	}
	return all
}

// newScriptedEditor builds an editor over a scripted source with a fake clock.
// The drawn bytes are recorded per write so the test reads them BACK THROUGH
// Screen: assertions are about rendered cells, not about the fact that a write
// happened.
func newScriptedEditor(t *testing.T, steps []scriptStep, width int, hist *History) (*Editor, *scriptedSource, *fakeClock, *recordingWriter) {
	t.Helper()
	clock := &fakeClock{t: time.Unix(editorTestEpoch, 0)}
	src := &scriptedSource{steps: steps, width: width, clock: clock}
	out := &recordingWriter{}
	ed := NewEditor(src, out, promptForTest, hist)
	ed.SetClock(clock.Now)
	return ed, src, clock, out
}

// scriptedWaitBudget is how long the scripted source STANDS IN for a bounded
// wait's park. The editor's waits are 15–150ms of REAL time, and sleeping those
// would make every polling test cost seconds; the scripted source pays a small
// fixed slice of it instead, scaled by the wait being at least one interval.
// It is a real sleep because parking is the mechanism under test (see ReadEvent).
const scriptedWaitBudget = 200 * time.Microsecond

// editorTestEpoch anchors the fake clock (an arbitrary fixed instant: the tests
// only ever measure DIFFERENCES between clock readings).
const editorTestEpoch = 1_700_000_000

// editorScriptRounds bounds runScript's loop: a runaway guard for a broken
// editor, deliberately generous — it is not a behaviour value.
const editorScriptRounds = 64

// promptForTest is the prompt the editor tests draw with.
const promptForTest = "you> "

// runScript runs the editor until the script is exhausted, returning every line
// the user submitted before the end of input.
func runScript(t *testing.T, ed *Editor) []Result {
	t.Helper()
	var results []Result
	for i := 0; i < editorScriptRounds; i++ {
		res, err := ed.Run()
		if err != nil {
			t.Fatalf("editor Run: %v", err)
		}
		if res.EOF || res.Interrupted {
			return results
		}
		results = append(results, res)
	}
	t.Fatalf("editor did not terminate within %d rounds", editorScriptRounds)
	return nil
}

// setNotifyPipe installs the notification pipe the editor's completion worker
// signals, and starts the single watcher goroutine that turns its bytes into
// channel receives. It stands in for the terminal's own pipe.
func (s *scriptedSource) setNotifyPipe(r, w *os.File) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.notifyCh = ch
	s.notifyPipeW = w
	s.mu.Unlock()
	go func() {
		var scratch [1]byte
		for {
			n, err := unix.Read(int(r.Fd()), scratch[:])
			if n <= 0 || err != nil {
				break // the write end is closed: the test is over
			}
			select {
			case ch <- struct{}{}:
			default: // one pending notification is the same information twice
			}
		}
		// The watcher owns the read end and closes it on the way out, so no
		// other goroutine ever closes a descriptor being read (which is itself a
		// race, and one the race detector reports).
		_ = r.Close() //stylegate:ignore the watcher owns this read end and is the only closer; teardown of a pipe the test owns
	}()
}

// drainNotify consumes a pending notification. It reports whether one was there,
// which is how the "the reader must look again" signal is modelled on a read
// that is NOT blocked.
func (s *scriptedSource) drainNotify() bool {
	s.mu.Lock()
	ch := s.notifyCh
	s.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// timeoutCount is how many reads the editor has issued — the observable "the
// read loop came back around" signal a test needs to know a wake was consumed
// without asserting it through a write (which is precisely the thing under test:
// a wake that must NOT write).
func (s *scriptedSource) timeoutCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.timeouts)
}
