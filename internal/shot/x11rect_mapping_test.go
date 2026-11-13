package shot

import (
	"os"
	"os/exec"
	"testing"
)

// xrectXdotoolBinary is the independent client that UNMAPS a window on demand;
// the mapping oracle itself is xrectXwininfoBinary.
const xrectXdotoolBinary = "xdotool"

// xrectXdotoolWindowVerb is xdotool's own verb for taking a window off screen —
// the deterministic way to produce the state the fixture must not mistake for
// a drawn window.
const xrectXdotoolWindowVerb = "windowunmap"

// TestXrectWindowMappedIsNotASettledRectangle is the minimal repro (Rule 5)
// of the fixture race this file's guard fixes, and it runs in well under a
// second without any load:
//
//	the pre-fix fixture waited only for a SETTLED, in-root rectangle, and
//	measured 2026-10-03 an xterm reports its full 184x134+41+31 geometry —
//	settled by every criterion that predicate has — while the server answers
//	IsUnMapped. A pointer aimed at that rectangle is therefore over the ROOT
//	window, the engine's QueryPointer route correctly refuses to name a
//	window, and the test failed for a reason that has nothing to do with the
//	code under test.
//
// xdotool windowunmap reproduces that exact server state on demand, so the
// predicate the fixture now waits on is pinned red-to-green instead of being
// left to a race on a loaded host.
func TestXrectWindowMappedIsNotASettledRectangle(t *testing.T) {
	display := covStartPrivateXvfb(t)
	xrectStartClient(t, display, xrectXtermClass, "-geometry", xrectXtermGeometry, "-e", "sleep", "60")
	winID, _, ok := xrectAwaitIndependentClient(t, display, xrectXtermClass)
	if !ok {
		t.Skip("no independent window oracle (xdotool/xwininfo) available — the mapping oracle cannot be exercised")
	}
	if _, err := exec.LookPath(xrectXdotoolBinary); err != nil {
		t.Skipf("%s is not installed — the window cannot be unmapped on demand", xrectXdotoolBinary)
	}

	xrectAwaitMappedClient(t, display, winID)
	if !xrectWindowMapped(t, display, winID) {
		t.Fatalf("window %s is mapped on %s but the mapping oracle says otherwise — the fixture's precondition would never be met", winID, display)
	}

	cmd := exec.Command(xrectXdotoolBinary, xrectXdotoolWindowVerb, winID)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "DISPLAY=" + display}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s %s on %s: %v\n%s", xrectXdotoolBinary, xrectXdotoolWindowVerb, winID, display, err, out)
	}

	if xrectWindowMapped(t, display, winID) {
		t.Fatalf("window %s still reports %s after %s %s — the fixture would aim the pointer at a window that is not on screen",
			winID, xrectMapStateMapped, xrectXdotoolBinary, xrectXdotoolWindowVerb)
	}
	// The rectangle is the trap: a window that is off screen still answers the
	// geometry call the pre-fix fixture used as its only criterion.
	rect, ok := xrectAbsoluteGeometry(t, display, winID)
	if !ok {
		t.Skip("xwininfo refused to measure the unmapped window — the pre-fix criterion cannot be reproduced here")
	}
	if !xrectSettledGeometry(rect) {
		t.Fatalf("the unmapped window %s measures %s, which the settle criterion already rejects — this fixture no longer reproduces the race it guards", winID, rect)
	}
}
