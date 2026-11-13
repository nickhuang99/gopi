package lineedit

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// The publish-vs-park race, asserted DETERMINISTICALLY rather than sampled.
//
// The defect (measured under -race on TestChatCompletion_EscapeClosesTheMenu,
// ~1 in 30-60 on master): the completion worker published candidates while the
// read loop was parked in a BLOCKING read. The publish IS announced — the wake
// seam raises a notification — but a notification that arrives while the loop is
// parked is consumed by a loop that was already past the check it was meant to
// inform. DRAWN stayed 1 while rev/fin were 2 and every read timeout was -1: the
// menu was published and open and the screen never showed it until the next
// keystroke.
//
// What must carry the draw is a LEVEL — "there is a published generation I have
// not drawn" — a fact the loop re-derives on its next read outcome whatever woke
// it. These guards CONSTRUCT the window instead of racing for it: the parkgate
// hook (parkgate.go) runs in the read loop's own park window, and the assertion
// is the RENDERED SCREEN — a candidate text that only the late publish can put
// there.

// The late publish's vocabulary. Distinct from menuStubResult on purpose: the
// guard's assertion is that THIS text reaches the screen, so a draw that
// happened before the late publish cannot be mistaken for it (a test that
// re-drew the same candidates would pass without drawing anything).
const (
	lateCandidateOne = "/magic-late-one"
	lateCandidateTwo = "/magic-late-two"
)

// swapCompleter answers with whichever vocabulary is currently installed, so a
// test can change what a publish will carry without going through SetCompleter
// (which cancels the in-flight request and publishes a closed menu — a different
// event from the worker's own late result).
type swapCompleter struct {
	mu    sync.Mutex
	vocab []Candidate
}

// complete satisfies the Completer contract: the installed vocabulary, filtered
// by the prefix being completed.
func (s *swapCompleter) complete(line string, cursor, limit int) []Candidate {
	s.mu.Lock()
	vocab := append([]Candidate(nil), s.vocab...)
	s.mu.Unlock()
	out := make([]Candidate, 0, len(vocab))
	for _, c := range vocab {
		if strings.HasPrefix(c.Text, line) {
			out = append(out, c)
		}
	}
	return out
}

// set replaces the vocabulary the next publish will carry.
func (s *swapCompleter) set(vocab []Candidate) {
	s.mu.Lock()
	s.vocab = vocab
	s.mu.Unlock()
}

// lateVocab is the vocabulary the late publish carries.
func lateVocab() []Candidate {
	return []Candidate{{Text: lateCandidateOne}, {Text: lateCandidateTwo}}
}

// TestChatCompletion_ParkGateHookIsWired is the guard's own precondition: the
// window hook must really be reached by the read loop, or the guard below would
// pass vacuously (a hook that never runs raises no publish).
func TestChatCompletion_ParkGateHookIsWired(t *testing.T) {
	var reached atomic.Bool
	h := newA7Harness(t, &stubCompleter{})
	h.setParkGate(func() { reached.Store(true) })
	defer h.setParkGate(nil)

	h.typeKeys(t, magicOpensTab)
	h.waitFor(t, "the park window hook to be reached", reached.Load)
}

// TestChatCompletion_PublishInTheParkWindowIsStillDrawn is the guard.
//
// The loop is held in its park window (the wait has been decided and the read
// has not begun) while a completion publish carrying a NEW candidate list is
// raised there. The loop's very next read outcome is an IDLE wait — the outcome
// a publish raised during a park actually meets — and that outcome must be what
// draws the published candidates: no keystroke is typed, no second publish is
// raised, and nothing in the idle wait carries the publish.
func TestChatCompletion_PublishInTheParkWindowIsStillDrawn(t *testing.T) {
	swap := &swapCompleter{}
	swap.set(menuStubResult)
	h := newA7Harness(t, &stubCompleter{})
	h.ed.SetCompleter(swap.complete) // one cancel at setup, before anything is drawn

	h.typeKeys(t, magicOpensTab)
	h.waitForDrawnCandidates(t, menuStubResult[0].Text)

	// The window is armed for the NEXT park only: the loop enters its park
	// window on every iteration, and a hook that fired on all of them would
	// raise the publish long before the state it is meant to test.
	var armed atomic.Bool
	armed.Store(true)
	h.setParkGate(func() {
		if !armed.CompareAndSwap(true, false) {
			return
		}
		// INSIDE the park window: the loop has committed to this read and has
		// not entered it. Raise the late publish through the editor's OWN
		// completion seam — the same publish path production's worker drives
		// off-path when its computation finishes. NOTHING IS PUSHED HERE: the
		// loop is about to be parked with an empty script, so the notification
		// the publish raises is the ONLY thing that can end this read. That is
		// the property under test — a notification a parked read must see.
		swap.set(lateVocab())
		h.ed.CompleterRequest()
	})
	defer h.setParkGate(nil)

	// Deliver one idle wait first, so the hook's iteration is reached while the
	// loop is still running (the park window is where the wait has been decided
	// and the read has not begun).
	h.src.push(scriptStep{timeout: true, advance: escWaitWindow()})

	h.waitForDrawnCandidates(t, lateCandidateOne)
	h.assertNotDone(t)
}

// TestChatCompletion_PublishWhileParkedIsDrawn asserts the same level from the
// other end: the loop is parked with nothing outstanding, the publish lands, and
// the loop's next read outcome must draw it. The step delivered here is an IDLE
// wait — the outcome a publish raised during a park actually meets — so this
// pins that the idle arm asks the completion level too.
func TestChatCompletion_PublishWhileParkedIsDrawn(t *testing.T) {
	swap := &swapCompleter{}
	swap.set(menuStubResult)
	h := newA7Harness(t, &stubCompleter{})
	h.ed.SetCompleter(swap.complete)

	h.typeKeys(t, magicOpensTab)
	h.waitForDrawnCandidates(t, menuStubResult[0].Text)

	// Park the loop on an idle bounded wait (delivered by the script), then
	// publish a new candidate list from the TEST goroutine while the loop is
	// heading into — or already inside — that read.
	h.src.push(scriptStep{timeout: true, advance: escWaitWindow()})
	swap.set(lateVocab())
	h.ed.CompleterRequest()

	h.waitForDrawnCandidates(t, lateCandidateOne)
	h.assertNotDone(t)
}

// waitForDrawnCandidates waits until the completion menu carrying want is ON
// THE SCREEN.
//
// The synchronisation is the SCREEN, never the editor's completion bookkeeping:
// menuRev / menuDirty are the READ LOOP's own state, written by its goroutine in
// clampedMenu and read by it in the arms — a test goroutine that reads them is a
// data race, and this package's suite caught exactly that under -race. The screen
// is the only surface both goroutines may touch, and it is also the surface the
// guard is really about.
func (h *a7Harness) waitForDrawnCandidates(t *testing.T, want string) {
	t.Helper()
	h.waitFor(t, "the completion menu carrying "+want+" to be drawn", func() bool {
		h.replay()
		for row := 0; row < a7Rows; row++ {
			if strings.Contains(h.screen.Line(row), want) {
				return true
			}
		}
		return false
	})
}

// setParkGate installs (or clears) the park-window hook on the package seam.
func (h *a7Harness) setParkGate(f func()) { setParkGateHook(f) }
