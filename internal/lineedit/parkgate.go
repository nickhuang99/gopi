package lineedit

import "sync"

// The acknowledge window, and the hook a test uses to construct the interleaving
// that produced this package's hardest flake (measured under -race on
// menu_test.go's EscapeClosesTheMenu, ~1 in 30-60 on master: the menu published
// and open, the worker's request finished, but only ONE generation drawn, with
// wakes=0 and every read timeout -1 — i.e. the read loop was parked in an
// UNBOUNDED read when the publish landed).
//
// The window exists because the repaint ADOPTS the publish generation mid-call
// (publishMenuHeight → clampedMenu reads it) and DRAWS later in the same call
// (paintMenu). A publish landing in between is therefore announced to a loop
// that is already past its check, and the draw it asked for does not depend on
// it: the level — "there is a published generation I have not drawn" — is what
// must survive, which is what clampedMenu and repaint's tail maintain.
//
// The hook below is that window, exposed so the regression guard can construct
// the interleaving instead of sampling it. It is nil in production, so the call
// site in repaint is a single nil check and never a behavioural branch
// (ParkGateHookEnabled reports the production shape, Rule 15: a seam that is
// live is announced, not silently active).
var (
	parkGateMu   sync.Mutex
	parkGateHook func()
)

// setParkGateHook installs (or clears) the acknowledge-window hook. Tests only.
func setParkGateHook(h func()) {
	parkGateMu.Lock()
	defer parkGateMu.Unlock()
	parkGateHook = h
}

// runParkGateHook runs the installed hook, if any. It is called from the repaint
// path, inside the acknowledge window.
func runParkGateHook() {
	parkGateMu.Lock()
	h := parkGateHook
	parkGateMu.Unlock()
	if h != nil {
		h()
	}
}

// ParkGateHookEnabled reports whether a hook is installed. Production reports
// false; the guard's own precondition test asserts it is wired when a test wants
// the deterministic interleaving.
func ParkGateHookEnabled() bool {
	parkGateMu.Lock()
	defer parkGateMu.Unlock()
	return parkGateHook != nil
}

// The KEY park probe — the missing LEVEL of the injected-key path, and the
// reason this file grew a second hook.
//
// THE ASYMMETRY IT CLOSES. A publish (a status refresh, the completion worker)
// carries a LEVEL: the read loop re-derives `statusPending() || menuNeedsDraw()
// || bandPending() || answerPending()` at the top of every iteration, so a
// notification that lands in the park window — after the loop has decided on an
// unbounded read and before the read begins — is drawn by the NEXT iteration
// anyway. A source whose signal is a one-byte pipe is the dangerous shape
// (measured): `Terminal.ReadEvent` DRAINS that pipe when it reports EventWake,
// so an edge can be consumed by a loop that then decides it owes nothing.
//
// THE INJECTED-KEY PATH HAD NO LEVEL AT ALL. `Injector.wake` writes one byte
// into the same pipe when a key is queued, and the injected queue was consulted
// only INSIDE `Injector.ReadEvent` — i.e. only if the editor asked the source
// something. An edge that lands in the park window therefore leaves the read
// blocked with NO news to discover and NO further wake coming (measured on a
// pty: the Escape of cmd/agent's TestChatInject_EscapeByNameReachesTheTurnSeam
// sat unread for the whole 3s bound, 1 red in 25 repeats).
//
// The probe asks the LEVEL: e.inputPending() reads `Injector.InputPending()`,
// which is true whenever a read unit is queued — including a key still being
// paced, which `Injector.ReadEvent` would deliver itself. The read loop rewrites
// its wait to injectParkRetryMS on a true answer, so the key is collected by the
// read the loop is about to make instead of waiting for a signal that was
// already spent.
//
// It is nil in production, exactly like the hook above: the default waits and
// the default (unbounded) block are untouched, and the call site is one nil
// check. A test whose source CANNOT lose a signal leaves it uninstalled and is
// unaffected.
var (
	injectParkMu   sync.Mutex
	injectParkHook func() injectParkProbe
)

// injectParkProbe is what the probe answers: whether the wait about to be taken
// must be bounded, and that bound. It is a struct and not a bool because the
// bound is the probe's to state — the park is a real wait, and a test that
// wants to observe it must not have the loop spin.
type injectParkProbe struct {
	pending bool
	retryMS int
}

// setInjectParkHook installs (or clears) the probe. Tests only.
func setInjectParkHook(h func() injectParkProbe) {
	injectParkMu.Lock()
	defer injectParkMu.Unlock()
	injectParkHook = h
}

// runInjectParkHook runs the installed probe and reports whether it answered
// "bound the wait". False without a hook — the production shape.
func runInjectParkHook() (int, bool) {
	injectParkMu.Lock()
	h := injectParkHook
	injectParkMu.Unlock()
	if h == nil {
		return 0, false
	}
	p := h()
	return p.retryMS, p.pending
}

// InjectParkHookEnabled reports whether a probe is installed (the guard's own
// precondition test asserts it is wired when the deterministic interleaving is
// wanted).
func InjectParkHookEnabled() bool {
	injectParkMu.Lock()
	defer injectParkMu.Unlock()
	return injectParkHook != nil
}

// injectParkRetryMS is the bound a read takes when a key is queued while the
// loop was about to block for ever: short enough that the queued key is
// collected at once (the injector paces its keys by hundreds of milliseconds),
// long enough that a loop that finds nothing is not a spin.
const injectParkRetryMS = 5
