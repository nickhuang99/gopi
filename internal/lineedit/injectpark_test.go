package lineedit

// injectpark_test.go — the DETERMINISTIC reproduction of the lost-key park
// window, and the regression guard for the LEVEL that closes it.
//
// WHY A FAKE SOURCE AND NOT A pty RUN. The window is a few microseconds wide and
// the red in cmd/agent was observed ONCE in 25 repeats (measurement below, no
// load manufactured to widen it — Rule 27). Sampling it is not a test: it is a
// lottery. This case CONSTRUCTS the interleaving instead, exactly as the
// park-window hook above was built to do for the publish path, and it makes the
// window visible with a marker instead of a race:
//
//  1. Arm a source that reports EventNone for a BOUNDED wait and BLOCKS for an
//     unbounded one — the shape `Terminal.ReadEvent(-1)` has, where a signal is
//     the only thing that can end the wait.
//  2. That source's wake seam SWALLOWS the first Notification (a one-shot
//     "signal lost"). `Terminal` demonstrates the mechanism for real: it DRAINS
//     its wake pipe when it reports EventWake, so one byte is exactly one edge,
//     and an edge consumed by a loop that then decides it owes nothing is gone.
//  3. Use the read loop's own park hook to land the injection IN the window —
//     after the wait was decided, before the source was asked.
//
// WITH the level re-derived in the park window the editor's read is bounded, the
// source never enters its unbounded wait, and the Escape lands as KeyEscape on
// the turn seam. WITHOUT it (revert `injectParkWait`) the loop blocks for good
// and the key is never read — the case fails on the claim, which is the exact
// product symptom: the injected key NAME never reached the turn seam.

import (
	"io"
	"sync"
	"testing"
	"time"
)

// injectParkProbeWait bounds the guard's own wait for the turn seam. It is
// generous on purpose: the guard asserts a MECHANISM (the key is read at all),
// so it must be slow only when the mechanism is broken, never when the host is.
// The red shape is an unbounded block, which no bound can rescue.
const injectParkProbeWait = 2 * time.Second

// parkWindowSource is a Source whose wake seam can lose one signal — the
// one-byte-pipe shape (see the guard's header). It reports EventNone on a
// bounded wait and PARKS until a signal actually arrives on an unbounded one, so
// a loop that takes an unbounded wait with no signal left is stuck exactly as
// the pty read loop was.
type parkWindowSource struct {
	mu       sync.Mutex
	lost     bool // the next Notify is swallowed
	signals  int
	ch       chan struct{}
	firstAsk bool // the first ReadEvent is the read loop's own opening call
}

func newParkWindowSource() *parkWindowSource {
	return &parkWindowSource{lost: true, ch: make(chan struct{}, 1)}
}

// ReadEvent: a bounded wait reports "nothing" (the editor's own cadence); an
// unbounded one blocks until a signal that SURVIVED arrives.
func (s *parkWindowSource) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	if timeoutMS >= 0 {
		return Event{Kind: EventNone}, nil
	}
	for {
		select {
		case <-s.ch:
			return Event{Kind: EventWake}, nil
		case <-time.After(10 * time.Millisecond):
			// A real terminal parks in poll(2) for ever; this source re-checks
			// instead, so the case fails on its own claim rather than hanging
			// the suite. Nothing about the interleaving depends on it (the
			// swallow is what the mechanism under test does).
		}
	}
}

func (s *parkWindowSource) Width(fallback int) int  { return fallback }
func (s *parkWindowSource) Height(fallback int) int { return fallback }

// Notify / NotifyEnabled: the wake seam, through the accessor spelling the
// terminal uses (the ONE resolver, SourceNotifier).
func (s *parkWindowSource) WakeNotifier() Notifier { return parkWindowNotifier{s} }

// InputPending is deliberately ABSENT: a source with no backlog. The injector
// sits ABOVE this one and is the source the editor asks (see the guard).
type parkWindowNotifier struct{ s *parkWindowSource }

// Notify writes the signal — or swallows it once, which is what a drained pipe
// looks like to the waiter.
func (n parkWindowNotifier) Notify() {
	n.s.mu.Lock()
	defer n.s.mu.Unlock()
	if n.s.lost {
		n.s.lost = false
		return
	}
	n.s.signals++
	select {
	case n.s.ch <- struct{}{}:
	default:
	}
}

// NotifyEnabled reports true: this sink really does signal (once).
func (n parkWindowNotifier) NotifyEnabled() bool { return true }

// TestInjector_QueuedKeyIsReadWhenItsWakeIsLostInTheParkWindow is the guard.
func TestInjector_QueuedKeyIsReadWhenItsWakeIsLostInTheParkWindow(t *testing.T) {
	if InjectParkHookEnabled() {
		// The probe is installed for the duration of this case only; a leaked
		// hook would change every other case's waits (Rule 15: announce it).
		setInjectParkHook(nil)
	}
	src := newParkWindowSource()
	inj := NewInjector(src, injectKeyDefaultCeiling)
	ed := NewChatEditor(inj, io.Discard, "you> ", nil, io.Discard, nil)
	if !ed.EnableRegion() {
		t.Fatal("the reserved layout was not installed: the case needs the read loop the chat shape has")
	}

	var mu sync.Mutex
	var origins []KeyOrigin
	ed.SetMinTurnActive(func() bool { return true })
	ed.SetInterruptTurn(func(o KeyOrigin) bool {
		mu.Lock()
		origins = append(origins, o)
		mu.Unlock()
		return true
	})

	// THE WINDOW: the probe arms the injection at the moment the read loop has
	// decided its wait and not yet asked the source. Everything after that is the
	// case: the key is queued, its notification is swallowed, and only a LEVEL
	// re-derived at that point can bring the loop back for it.
	armed := false
	setInjectParkHook(func() injectParkProbe {
		if armed {
			return injectParkProbe{}
		}
		armed = true
		res := inj.InjectKey(KeyNameEscape)
		if !res.Accepted {
			t.Errorf("injecting %q refused: %s", KeyNameEscape, res.Why)
		}
		return injectParkProbe{pending: inj.InputPending(), retryMS: injectParkRetryMS}
	})
	defer setInjectParkHook(nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Run returns when the editor is closed; the assertions are on the seam.
		_, _ = ed.Run() //stylegate:ignore ends the injected-line editor's loop so the park under test is released; the park verdict is the assertion
	}()
	defer func() {
		ed.Close()
		select {
		case <-done:
		case <-time.After(injectParkProbeWait):
		}
	}()

	deadline := time.Now().Add(injectParkProbeWait)
	for {
		mu.Lock()
		n := len(origins)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the injected key NAME %q never reached the turn seam within %v — its wake was lost in the read loop's park window and no LEVEL brought the loop back for it",
				KeyNameEscape, injectParkProbeWait)
		}
		time.Sleep(injectParkRetryMS * time.Millisecond) //stylegate:allow-sleep bounded poll (injectParkProbeWait) for the key to reach the turn seam
	}
	mu.Lock()
	got := origins
	mu.Unlock()
	if len(got) != 1 || got[0] != OriginEscape {
		t.Fatalf("the turn seam was told %v, want exactly [%q]", got, OriginEscape)
	}
}
