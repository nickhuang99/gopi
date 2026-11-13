package shot

import (
	"encoding/binary"
	"strings"
	"testing"
)

// selfaddressing_gate_test.go — the ANTI-FALLBACK gate for --self.
//
// The whole reason the pid-window chain exists is that "which window is ACTIVE"
// and "which window HOSTS me" are different questions with different answers,
// and answering the wrong one produced the measured mis-capture this feature
// was built to remove (pidwindow.go's header). A fallback is therefore not a
// robustness improvement here — it is the defect, wearing the costume of a
// helpful retry.
//
// The gate is BEHAVIOURAL: it drives the real addressing path over the package's
// fake server and reads the requests off the wire. An innocuous-looking
// `activeWindow() ... // fallback` inside windowRectFor would show up as an
// InternAtom naming _NET_ACTIVE_WINDOW, and this test fails. A source scan would
// also catch that, but only for the spelling it greps; the wire cannot be
// spelled around.

// gateForbiddenNames are the EWMH names an addressing path must never consult.
// netActiveWindowName is the constant, referenced rather than spelled, so this
// gate cannot go stale against a rename (Rule 13).
func gateForbiddenNames() []string { return []string{netActiveWindowName} }

// gateInternedNames returns every atom name this fake server was asked to
// intern, in request order.
func gateInternedNames(t *testing.T, s *covFakeXServer) []string {
	t.Helper()
	var out []string
	for _, req := range s.requestsOf(x11OpInternAtom) {
		if len(req) < 8 {
			continue
		}
		nameLen := int(binary.LittleEndian.Uint16(req[4:6]))
		if 8+nameLen > len(req) {
			t.Fatalf("a recorded InternAtom request carries nameLen %d in a %d-byte body — the recording, not the product, is malformed", nameLen, len(req))
		}
		out = append(out, string(req[8:8+nameLen]))
	}
	return out
}

// gateAssertNoActiveWindow is the assertion both cases below share: the names the
// addressing path interned must contain none of the forbidden questions.
func gateAssertNoActiveWindow(t *testing.T, interned []string) {
	t.Helper()
	if len(interned) == 0 {
		t.Fatal("no atom was interned at all: the addressing path did not run, so this gate would pass vacuously")
	}
	for _, got := range interned {
		for _, forbidden := range gateForbiddenNames() {
			if strings.EqualFold(got, forbidden) {
				t.Errorf("the --self addressing path interned %q — it asked the display WHICH WINDOW IS ACTIVE, which is the fallback this feature exists to remove (the answer is a different window: whatever the user last clicked)", got)
			}
		}
	}
}

// TestSelfAddressingNeverAsksForTheActiveWindow is the gate over RESOLUTION.
func TestSelfAddressingNeverAsksForTheActiveWindow(t *testing.T) {
	display, srv := selfInstallFakeHostWindow(t)
	a, err := OpenXActuator(display)
	if err != nil {
		t.Fatalf("OpenXActuator(%s): %v", display, err)
	}
	defer a.Close()
	if _, err := a.FocusSelf(); err != nil {
		t.Fatalf("FocusSelf: %v", err)
	}
	gateAssertNoActiveWindow(t, gateInternedNames(t, srv))
}

// TestSelfAddressingGateCatchesAFallback is the gate's RED PROOF: with a
// deliberately planted fallback the same assertion fails. A gate nobody has seen
// fail is a gate that may be asserting nothing — this is the run that shows it
// bites (Rule 6: regression tests carry their own evidence).
func TestSelfAddressingGateCatchesAFallback(t *testing.T) {
	display, srv := selfInstallFakeHostWindow(t)
	c, err := dialX11(display)
	if err != nil {
		t.Fatalf("dial %s: %v", display, err)
	}
	defer c.close()
	// The call's OUTCOME is irrelevant — the fake server does not implement
	// QueryPointer, so it errors. What is being proved is that the forbidden
	// QUESTION reaches the wire, and the internet atom is on the wire before any
	// of that matters.
	_, _, _ = c.activeWindow() //stylegate:ignore reads the active window the self-addressing gate checks; a 0,0 reading is the refused case the gate reports
	interned := gateInternedNames(t, srv)
	caught := false
	for _, got := range interned {
		if strings.EqualFold(got, netActiveWindowName) {
			caught = true
		}
	}
	if !caught {
		t.Fatalf("the gate did NOT see the planted fallback: interned=%v", interned)
	}
}
