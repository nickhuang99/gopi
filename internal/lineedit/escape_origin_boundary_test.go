package lineedit

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

// escape_origin_boundary_test.go — A LONE ESCAPE MUST NOT FUSE ACROSS A READ
// ORIGIN BOUNDARY. This is the minimal reproduction of the silent drop measured
// on a real pty in `gopi chat --replay` (36 Esc presses, 8 of them lost, 7/20 at
// the 2000 ms tier): the watcher's Esc was typed, the replay driver's very next
// INJECTED keystroke arrived while the decoder was still holding the bare ESC,
// and `Decoder.Feed` joined the two bytes into ONE key — Alt+<rune>.
//
// WHY THAT IS A DROP AND NOT A MISREADING. The fused key carries the ORIGIN OF
// THE READ THAT COMPLETED IT (injected), and `cmd/agent`'s replay control layer
// declines every key that is not typed (`ReadOriginTyped`, chat_replay_control.go
// GateKey) precisely so a recording's own spaces and arrows are not claimed as
// watcher input. So the Esc reached NOBODY: no state word, no LEAVING, the replay
// kept typing, and the process lived to its deadline. Nothing in the transcript
// said a key had been dropped (Rule 15) — the key did not exist any more.
//
// THE FIX THIS CASE GUARDS is stated as a fact about provenance rather than as a
// timing rule: a byte a HUMAN typed can never be the continuation of a sequence,
// and an INJECTED byte arriving in the ESC-resolution window is a different
// producer's writing, not the rest of the watcher's Escape key. The editor
// resolves the held ESC as the Escape key whenever the incoming read's origin
// differs from the origin that left the ESC pending (editor.go, applyRead).
//
// THE CASE IS DETERMINISTIC BY CONSTRUCTION: the source the editor reads through
// blocks its bounded (ESC-resolution) wait until this test has both queued the
// injected key and released the wait, so the interleaving the defect needs is
// created by the test instead of hoped for. It fails on an unfixed tree (the
// gate never sees a KeyEscape) and passes after the fix.

const (
	// escFuseWidth/Height are the geometry the source reports.
	escFuseWidth  = 80
	escFuseHeight = 24
	// escFuseWait bounds a wait for a state the read loop sets. It is a
	// deadline that turns a hang into a failure, never a measured duration.
	escFuseWait = 3 * time.Second
	// escFuseInjectedKey is what the "driver" types into the same line while the
	// watcher's Esc is pending — one printable rune, which is exactly what turns
	// a held ESC into Alt+<rune>.
	escFuseInjectedKey = "k"
)

// escFuseSource is the terminal under the editor: it delivers ONE typed read
// (the watcher's Esc) and then blocks every bounded read until the test releases
// it, so the injected key is already queued when the resolution window ends.
type escFuseSource struct {
	typed chan []byte
	// bounded is signalled when a BOUNDED read starts; the read then waits for
	// release (or for stop, so the editor can end).
	bounded chan struct{}
	release chan struct{}
	stop    chan struct{}
	// wake is the PRODUCTION WAKE SEAM (raw.go's wakePipe): a notification from
	// the injector makes a PARKED bounded read return at once with EventWake,
	// instead of waiting out its own bound. Without it the interleaving this
	// case is about cannot happen at all — which is why the first version of
	// this case passed against the unfixed tree.
	wake chan struct{}
}

func newEscFuseSource() *escFuseSource {
	return &escFuseSource{
		typed:   make(chan []byte, 1),
		bounded: make(chan struct{}, 1),
		release: make(chan struct{}),
		stop:    make(chan struct{}),
		wake:    make(chan struct{}, 1),
	}
}

// Notify is the source's wake seam (lineedit.Notifier): the injector calls it
// when it queues a key, and a reader parked in a bounded wait must return.
func (s *escFuseSource) Notify() {
	select {
	case s.wake <- struct{}{}:
	default: // a pending wake is the same information as a second one
	}
}

// NotifyEnabled reports that this source really delivers the wake.
func (s *escFuseSource) NotifyEnabled() bool { return true }

func (s *escFuseSource) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	if timeoutMS < 0 {
		// An unbounded wait parks until the test types something.
		select {
		case b := <-s.typed:
			n := copy(buf, b)
			return Event{Kind: EventInput, Bytes: n}, nil
		case <-s.stop:
			return Event{Kind: EventInput, EOF: true}, nil
		}
	}
	select {
	case b := <-s.typed:
		n := copy(buf, b)
		return Event{Kind: EventInput, Bytes: n}, nil
	default:
	}
	// A bounded wait: report that it started, then hold it until the test has
	// queued the injected key. This is the defect's own interleaving, made
	// deterministic.
	select {
	case s.bounded <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
	case <-s.stop:
		return Event{Kind: EventInput, EOF: true}, nil
	case <-s.wake:
		// THE PRODUCTION SHAPE: a key was queued while this bound was open, so
		// the read returns NOW — the editor loops, the injector takes the key
		// on the next read, and the held ESC is still in the decoder.
		return Event{Kind: EventWake}, nil
	}
	return Event{Kind: EventNone}, nil
}

func (s *escFuseSource) Width(fallback int) int  { return escFuseWidth }
func (s *escFuseSource) Height(fallback int) int { return escFuseHeight }

// escFuseRecorder records every key the gate is offered, in arrival order.
type escFuseRecorder struct {
	mu   sync.Mutex
	seen []Key
}

func (r *escFuseRecorder) GateKey(k Key) bool {
	r.mu.Lock()
	r.seen = append(r.seen, k)
	r.mu.Unlock()
	// The production predicate this reproduces: claim the lone Escape only.
	return k.Code == KeyEscape
}

func (r *escFuseRecorder) KeyUnclaimed(Key) {}
func (r *escFuseRecorder) KeyRelease(Key)   {}

func (r *escFuseRecorder) snapshot() []Key {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Key(nil), r.seen...)
}

// TestTypedEscDoesNotFuseWithAnInjectedKey is the minimal reproduction described
// in the file header: RED before the origin-boundary fix (no KeyEscape is ever
// offered to the gate — the key was fused into Alt+k and declined by origin),
// GREEN after it (the typed KeyEscape is offered FIRST, and the injected byte is
// applied as its own ordinary key).
func TestTypedEscDoesNotFuseWithAnInjectedKey(t *testing.T) {
	src := newEscFuseSource()
	src.typed <- []byte{escByte} // the watcher's Esc, typed provenance
	inj := NewInjector(src, injectKeyDefaultCeiling)
	ed := NewEditor(inj, discardWriter{}, "", NewHistory(0, ""))
	gate := &escFuseRecorder{}
	ed.SetKeyGate(gate)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = ed.Run() //stylegate:ignore the editor is killed by the cleanup below; Run's error is the expected exit, and the assertion is on the recorded key events
	}()
	t.Cleanup(func() {
		close(src.stop)
		ed.Close()
		select {
		case <-done:
		case <-time.After(escFuseWait):
			t.Errorf("the editor's read loop never returned")
		}
	})

	// ① the ESC-resolution window must be open (the decoder holds the lone byte).
	deadline := time.Now().Add(escFuseWait)
	for time.Now().Before(deadline) && !ed.dec.BareEscape() {
		time.Sleep(time.Millisecond) //stylegate:allow-sleep bounded poll of the decoder state the read loop sets
	}
	if !ed.dec.BareEscape() {
		t.Fatalf("the decoder never held the lone ESC — this case's premise fails")
	}

	// ② the driver types while the watcher's Esc is pending: queue an injected
	// key INSIDE the resolution window the source is holding open.
	select {
	case <-src.bounded:
	case <-time.After(escFuseWait):
		t.Fatalf("the editor never issued a bounded ESC-resolution read")
	}
	injected := make(chan InjectResult, 1)
	go func() { injected <- inj.InjectBytes(escFuseInjectedKey) }()
	// Give the queue the moment it needs; the injector's own drain wait cannot
	// finish while the source holds the read open, which is the point.
	time.Sleep(time.Millisecond) //stylegate:allow-sleep lets the injector's goroutine queue the key before the window closes
	close(src.release)
	res := <-injected
	if !res.Accepted {
		t.Fatalf("the injected key was refused, so the case's premise fails: %s", res.Why)
	}

	// ③ the Esc must have reached the gate as an ESCAPE, typed.
	for time.Now().Before(deadline) {
		for _, k := range gate.snapshot() {
			if k.Code == KeyEscape {
				if k.Origin != ReadOriginTyped {
					t.Fatalf("the Escape reached the gate with origin %v, want typed", k.Origin)
				}
				return
			}
		}
		time.Sleep(time.Millisecond) //stylegate:allow-sleep bounded poll of the gate's own record
	}
	t.Fatalf("the watcher's Escape NEVER reached the key gate — it was fused with the driver's injected %q into one key "+
		"(the silent drop: no state word, no LEAVING, the replay keeps typing). Keys the gate saw: %v",
		escFuseInjectedKey, describeKeys(gate.snapshot()))
}

// describeKeys renders a key list for a failure message (a bare %v of a struct
// slice hides the code, which is what the reader needs).
func describeKeys(keys []Key) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, describeKey(k)+" origin="+strconv.Itoa(int(k.Origin)))
	}
	return out
}
