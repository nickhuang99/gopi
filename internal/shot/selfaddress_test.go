package shot

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// itoaPair renders a pointer coordinate the way the product does (SelfFocus
// .Pointer), so the assertion compares like with like.
func itoaPair(x, y int) string { return fmt.Sprintf("%d,%d", x, y) }

// The keysyms the verbatim-typing case needs, with their own keycodes. The
// package's shared US fixture (covUsFakeLayout) advertises only the keys the
// existing tests press, and a test must not widen a fixture other tests share:
// this layout is installed locally, on this test's own server.
const (
	selfKeysymA          = 0x0061 // XK_a
	selfKeysymB          = 0x0062 // XK_b
	selfKeysymBackslash  = 0x005c // XK_backslash
	selfKeycodeA         = 0x26
	selfKeycodeB         = 0x38
	selfKeycodeBackslash = 0x33
)

// selfVerbatimLayout is covUsFakeLayout plus the three keys the `a\b` case
// presses, so "three keypresses for three characters" is a real observation.
func selfVerbatimLayout() covFakeLayout {
	l := covUsFakeLayout()
	l[selfKeysymA] = struct {
		code  byte
		shift uint32
	}{code: selfKeycodeA}
	l[selfKeysymB] = struct {
		code  byte
		shift uint32
	}{code: selfKeycodeB}
	l[selfKeysymBackslash] = struct {
		code  byte
		shift uint32
	}{code: selfKeycodeBackslash}
	return l
}

// selfaddress_test.go — the ADDRESSING half of --self: which window is chosen,
// where the focus and the pointer are put, and what is REPORTED about it.
//
// Two layers, deliberately:
//   - the RULE (pointInside) is pure over a rectangle, so its boundary cases are
//     enumerated rather than sampled;
//   - the WIRE (FocusSelf / TypeIntoSelfWindow / ResolveSelfWindow) runs over
//     the package's fake X server through the REAL dial path, so the request
//     bodies — XSetInputFocus especially, which has no reply and would otherwise
//     be unobservable — are asserted as bytes.
//
// The fixture (pdInstallFakePIDWindows + pdFixtureProcTree) is pidwindow_test.go's,
// not a second one: one resolution chain, one fixture for it.

// selfTestWindowRect is the rectangle the fixture's hosting window reports.
// The test asserts the pointer target against it, so a change to the rectangle
// cannot silently leave the assertion measuring itself.
func selfTestHostRect() Rect {
	return Rect{X: 120, Y: 80, W: 900, H: 560}
}

// selfInstallFakeHostWindow is the shared setup: the fake X server with the
// pid-window fixture installed and THIS process pointed at the fixture tree.
func selfInstallFakeHostWindow(t *testing.T) (string, *covFakeXServer) {
	t.Helper()
	display, fx := pdInstallFakePIDWindows(t)
	srv := fx.owner
	pdInstallFixtureWindows(fx)
	ttyPath := filepath.Join(t.TempDir(), "pts9")
	if err := os.WriteFile(ttyPath, nil, 0o644); err != nil {
		t.Fatalf("fixture tty: %v", err)
	}
	pdPointSelfAtFixture(t, pdFixtureProcTree(t, ttyPath))
	return display, srv
}

// TestPointInsideStaysInsideTheWindow enumerates the boundary cases: a roomy
// window gets the margin (not the exact corner, which can belong to a frame),
// and a window too small for a margin gets its centre — NEVER a coordinate
// outside itself, because a pointer outside the window lands on whatever is
// beneath it, which is the mis-targeting --self exists to remove.
func TestPointInsideStaysInsideTheWindow(t *testing.T) {
	cases := []struct {
		name string
		r    Rect
		want [2]int
	}{
		{"roomy", Rect{X: 100, Y: 50, W: 400, H: 300}, [2]int{100 + x11SelfFocusMargin, 50 + x11SelfFocusMargin}},
		{"exactly-two-margins-wide", Rect{X: 0, Y: 0, W: 2 * x11SelfFocusMargin, H: 100}, [2]int{x11SelfFocusMargin, 50}},
		{"tiny-1x1", Rect{X: 7, Y: 9, W: 1, H: 1}, [2]int{7, 9}},
		{"small-10x10", Rect{X: 640, Y: 400, W: 10, H: 10}, [2]int{645, 405}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, y := pointInside(tc.r)
			if x != tc.want[0] || y != tc.want[1] {
				t.Errorf("pointInside(%s) = %d,%d, want %d,%d", tc.r, x, y, tc.want[0], tc.want[1])
			}
			if x < tc.r.X || y < tc.r.Y || x >= tc.r.X+tc.r.W || y >= tc.r.Y+tc.r.H {
				t.Errorf("pointInside(%s) = %d,%d — OUTSIDE the window, so the pointer would land on another client", tc.r, x, y)
			}
		})
	}
}

// TestFocusSelfSetsTheFocusAndPointsInsideTheWindow drives the whole addressing
// operation over the fake server and pins its three observable effects: the
// XSetInputFocus request names the RESOLVED window, the pointer is moved inside
// that window's rectangle, and no key or button event is injected (addressing is
// not typing).
func TestFocusSelfSetsTheFocusAndPointsInsideTheWindow(t *testing.T) {
	display, srv := selfInstallFakeHostWindow(t)
	a, err := OpenXActuator(display)
	if err != nil {
		t.Fatalf("OpenXActuator(%s): %v", display, err)
	}
	defer a.Close()

	before := len(srv.requestsOf(x11OpSetInputFocus))
	sf, err := a.FocusSelf()
	if err != nil {
		t.Fatalf("FocusSelf: %v", err)
	}

	// (1) the window: the one the chain resolved, and the label says how.
	if sf.Window != pdRootChildClient {
		t.Errorf("addressed window 0x%x, want 0x%x (the window whose _NET_WM_PID is the emulator's)", sf.Window, pdRootChildClient)
	}
	if !strings.Contains(sf.Label, x11RouteNetWMNamePID) {
		t.Errorf("the route label does not name %s: %q", x11RouteNetWMNamePID, sf.Label)
	}

	// (2) the wire: exactly one SetInputFocus, carrying that window id.
	reqs := srv.requestsOf(x11OpSetInputFocus)
	if len(reqs)-before != 1 {
		t.Fatalf("FocusSelf emitted %d XSetInputFocus request(s), want exactly 1", len(reqs)-before)
	}
	req := reqs[len(reqs)-1]
	if got := binary.LittleEndian.Uint32(req[4:8]); got != sf.Window {
		t.Errorf("XSetInputFocus names window 0x%x, want 0x%x", got, sf.Window)
	}
	if got := req[1]; got != xFocusRevertToParent {
		t.Errorf("XSetInputFocus revert-to = %d, want %d (RevertToParent)", got, xFocusRevertToParent)
	}

	// (3) the pointer: moved into the resolved rectangle, not to a hardcoded spot.
	wantX, wantY := pointInside(selfTestHostRect())
	if !sf.PointerMoved {
		t.Fatal("the pointer was not moved: with PointerRoot focus that is the leg that makes typing land")
	}
	if got := sf.Pointer; got != itoaPair(wantX, wantY) {
		t.Errorf("pointer placed at %s, want %s (inside the resolved window)", got, itoaPair(wantX, wantY))
	}
	motions := 0
	for _, ev := range srv.fakeInputEvents() {
		switch ev.Type {
		case xEventMotionNotify:
			motions++
		case xEventKeyPress, xEventKeyRelease, xEventButtonPress, xEventButtonRelease:
			t.Errorf("FocusSelf injected a key/button event (type %d): addressing must not type", ev.Type)
		}
	}
	if motions != 1 {
		t.Errorf("FocusSelf emitted %d motion event(s), want exactly 1", motions)
	}
}

// TestFocusSelfReportsWhichLegWasUsed pins the honest-reporting property: a
// server that keeps the focus it had (the PointerRoot case) is reported as the
// POINTER leg, not silently presented as a successful explicit focus. The mode
// word is the reader's only way to tell the two apart.
func TestFocusSelfReportsWhichLegWasUsed(t *testing.T) {
	display, srv := selfInstallFakeHostWindow(t)
	srv.setFocusReplyWindow(covRootWindowID) // the server refuses to change it
	a, err := OpenXActuator(display)
	if err != nil {
		t.Fatalf("OpenXActuator(%s): %v", display, err)
	}
	defer a.Close()
	sf, err := a.FocusSelf()
	if err != nil {
		t.Fatalf("FocusSelf: %v", err)
	}
	if sf.FocusAfter != covRootWindowID {
		t.Fatalf("the fixture did not keep the focus (0x%x), so this case is not being exercised", sf.FocusAfter)
	}
	if sf.Mode != selfFocusModePointer {
		t.Errorf("mode = %q, want %q — a focus the server did not take must not be reported as taken", sf.Mode, selfFocusModePointer)
	}
}

// TestResolveSelfWindowInjectsNothing is the dry-run contract: resolving the
// address opens the display and reads the chain, and moves neither the focus nor
// the pointer — a "plan" that had already changed the desktop would not be one.
func TestResolveSelfWindowInjectsNothing(t *testing.T) {
	display, srv := selfInstallFakeHostWindow(t)
	if _, err := ResolveSelfWindow(display); err != nil {
		t.Fatalf("ResolveSelfWindow: %v", err)
	}
	if n := len(srv.requestsOf(x11OpSetInputFocus)); n != 0 {
		t.Errorf("the read-only resolver sent %d XSetInputFocus request(s), want 0", n)
	}
	if n := len(srv.fakeInputEvents()); n != 0 {
		t.Errorf("the read-only resolver injected %d input event(s), want 0", n)
	}
}

// TestTypeIntoSelfWindowAddressesThenTypes pins the composition the voice path
// uses and the VERBATIM contract: a backslash in the text is a character to
// type, never the start of an escape sequence (the CLI's decoder belongs to the
// --text flag grammar, not to a transcript a speech model produced).
func TestTypeIntoSelfWindowAddressesThenTypes(t *testing.T) {
	display, srv := selfInstallFakeHostWindow(t)
	srv.setLayout(selfVerbatimLayout())
	a, err := OpenXActuator(display)
	if err != nil {
		t.Fatalf("OpenXActuator(%s): %v", display, err)
	}
	defer a.Close()

	// 'a', '\', 'b' — three keysyms, NOT a Bell escape.
	sf, err := a.TypeIntoSelfWindow(`a\b`)
	if err != nil {
		t.Fatalf("TypeIntoSelfWindow: %v", err)
	}
	if sf.Window != pdRootChildClient {
		t.Errorf("typed into 0x%x, want the resolved 0x%x", sf.Window, pdRootChildClient)
	}
	presses := 0
	for _, ev := range srv.fakeInputEvents() {
		if ev.Type == xEventKeyPress {
			presses++
		}
	}
	if presses != 3 {
		t.Errorf("typed %d keypress(es) for a 3-character string — the text was transformed, not typed verbatim", presses)
	}
}

// TestTypableTextRefusesBeforeAnyDisplay is the pre-flight the voice path relies
// on: the typable question is answered WITHOUT a connection, and the refusal
// names the offending rune so the reader knows which character broke it.
func TestTypableTextRefusesBeforeAnyDisplay(t *testing.T) {
	if err := TypableText("plain ASCII and \t a tab and \n a newline"); err != nil {
		t.Errorf("TypableText refused text this client can type: %v", err)
	}
	err := TypableText("café")
	if err == nil {
		t.Fatal("TypableText accepted a non-ASCII transcript")
	}
	if !strings.Contains(err.Error(), "é") {
		t.Errorf("the refusal does not name the offending rune: %v", err)
	}
}
