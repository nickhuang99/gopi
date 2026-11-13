// x11rect_test.go — guards for the rectangle-scoped capture modes (window /
// area), spoken against a REAL X server.
//
// WHY LIVE, and why the oracle is PIXELS: the window/area modes are a claim
// about a RECTANGLE — "the pixels I hand you are the ones that window occupies
// on that screen". No fake X server can decide that claim: the failure modes
// this code actually has are wire-level (a request whose declared length
// disagrees with the bytes sent is silently DROPPED by the server; a reply
// field read at the wrong offset yields a plausible-looking wrong geometry).
// So the guards here are:
//
//	route 1   xwininfo's INDEPENDENT measurement of the same window id must
//	          equal the geometry we report (an independent X11 client, not a
//	          mirror of our own arithmetic);
//	route 2   the window capture must be PIXEL-IDENTICAL (absolute error 0) to
//	          the corresponding CROP of the whole-screen capture — the same
//	          client, the same server, two different code paths, therefore a
//	          real cross-check of the rectangle rather than of "it looks right"
//	          (Rule 10.2);
//	route 3   the area mode must reproduce that very crop.
//
// The fixture is a private Xvfb plus xfwm4 plus a single xterm. The WINDOW
// MANAGER is the interesting case on purpose: it REPARENTS the client window
// into a frame, so the window's own GetGeometry coordinates are
// frame-relative and only TranslateCoordinates yields the screen position —
// the exact bug a WM-less fixture would hide. The WM-less route (the
// GetInputFocus fallback) is covered separately below.
//
// Every test SKIPS VISIBLY when Xvfb / xfwm4 / xterm is unavailable, so the
// suite still passes on a machine with no X at all.
package shot

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// xrectXtermClass is the window class the fixture's only client window
	// belongs to. xrectXtermX/Y place its top-left corner well inside the
	// private 640x480 root (covFakeWidth/covFakeHeight) so its rectangle is
	// never clipped by the screen edge, and xrectXtermGeometry is that
	// placement in xterm's own character-geometry spelling.
	xrectXtermClass = "xterm"
	xrectXtermX     = 40
	xrectXtermY     = 30
	xrectXtermCols  = 30
	xrectXtermRows  = 10

	// xrectMinWindowSide is the smallest side a settled client window can have
	// in the fixture. A window a window manager is still placing is
	// TRANSIENTLY tiny (measured: 1x257 while xfwm4 negotiated the frame), and
	// comparing against such a rectangle is comparing against a moving target,
	// not against the layout under test.
	xrectMinWindowSide = 20

	// xrectSettleSamples is how many CONSECUTIVE identical measurements mean
	// "the layout stopped changing" — the fit criterion for the fixture, in
	// place of a fixed sleep.
	xrectSettleSamples = 3

	// xrectRoutePointer is the label the engine reports when the active window
	// was resolved from the pointer (the WM-less route). Referencing the
	// constant keeps the test and the engine from drifting apart silently.
	xrectRoutePointer = x11RoutePointer

	// xrectMapStateLabel is the xwininfo report line carrying a window's map
	// state, and xrectMapStateMapped is the value that line carries for a
	// window that is actually on screen. The pair exists because a live
	// rectangle is NOT evidence that a window has pixels yet: measured
	// 2026-10-03 on a WM-less Xvfb, a client that has been created but not yet
	// mapped reports a full-size, in-root geometry while xwininfo answers
	// "IsUnMapped" — so a fixture that waits only for a settled rectangle aims
	// the pointer at a window that is not under it and reads the ROOT instead.
	xrectMapStateLabel  = "Map State"
	xrectMapStateMapped = "IsViewable"

	// xrectPixelSettleFrames is how many CONSECUTIVE pixel-identical frames of
	// the client window mean "its content stopped changing". The WM-less
	// xterm repaints itself when the pointer enters it (measured 2026-10-03:
	// the cursor is redrawn hollow -> filled 3.9-4.5ms after the move, exactly
	// the 44 pixels of the cursor cell the pixel oracle compares), so the
	// fixture must wait for the CONTENT to settle rather than sample once and
	// compare two frames drawn from different instants.
	xrectPixelSettleFrames = 2

	// xrectWindowManager is the window manager the fixture runs. Any EWMH
	// manager would do; this one is already used by the project's live X11
	// tests, so the host is known to have it.
	xrectWindowManager = "xfwm4"

	// xrectReadyBudget bounds how long the fixture may take to reach "the WM
	// has a client window mapped and focused", and xrectPollStep is the poll
	// step (a bounded loop, never a busy wait).
	xrectReadyBudget = 20 * time.Second
	xrectPollStep    = 50 * time.Millisecond

	// xrectXwininfoBudget bounds the independent oracle's own execution, and
	// xrectXwininfoBinary is the independent client that measures it.
	xrectXwininfoBudget = 10 * time.Second
	xrectXwininfoBinary = "xwininfo"
)

// xrectXtermGeometry is the fixture client's placement in xterm's own
// spelling, built from the constants above so the requested position and the
// position the pointer oracle aims at cannot drift apart.
var xrectXtermGeometry = fmt.Sprintf("%dx%d+%d+%d", xrectXtermCols, xrectXtermRows, xrectXtermX, xrectXtermY)

// xrectWindowFromGeometry extracts the window id the engine reported, as an
// xwininfo / xdotool argument.
func xrectWindowFromGeometry(t *testing.T, geom x11Geometry) string {
	t.Helper()
	if geom.Window == "" {
		t.Fatalf("the window capture reported no window id (geometry %+v) — the result cannot name what it captured", geom)
	}
	return geom.Window
}

// xrectRectOf is the captured rectangle as a Rect.
func xrectRectOf(geom x11Geometry) Rect {
	return Rect{X: geom.X, Y: geom.Y, W: geom.Width, H: geom.Height}
}

// xrectSettled reports whether a measurement is a FINISHED layout rather than
// a window manager mid-negotiation: big enough to be a client window and
// entirely inside the root.
func xrectSettled(geom x11Geometry) bool {
	r := xrectRectOf(geom)
	if r.W < xrectMinWindowSide || r.H < xrectMinWindowSide {
		return false
	}
	return r.within(covFakeWidth, covFakeHeight) == nil
}

// xrectStartClient starts an X client (the WM or the xterm) on the private
// display and returns the running process. It SKIPS VISIBLY when the binary is
// not installed, and lets the shared mock-process helper own the process
// group, the reaping and the failure reporting (so a test panic cannot strand
// a stray client holding the display).
func xrectStartClient(t *testing.T, display, binary string, args ...string) *testutil.MockProcess {
	t.Helper()
	if _, err := exec.LookPath(binary); err != nil {
		t.Skipf("%s is not installed — the live rect-capture fixture cannot be built", binary)
	}
	cmd := exec.Command(binary, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "DISPLAY=" + display}
	return testutil.StartMockProcessCmd(t, cmd, testutil.AllowStartFailure())
}

// xrectAwaitWindowManager waits (bounded) until the window manager has
// established its EWMH root property. This is a DEPENDENCY, not a delay: a
// client mapped into a half-initialised session is never focused by the WM
// (measured: the active window stayed an xfwm4-internal 1-pixel-wide window
// for the whole poll budget, because the client's map request arrived before
// the WM was ready to manage it). The predicate is the WM's own announcement —
// it interns _NET_ACTIVE_WINDOW on the root window when it takes over.
func xrectAwaitWindowManager(t *testing.T, display string) {
	t.Helper()
	c, err := dialX11(display)
	if err != nil {
		t.Fatalf("connect to %s to await the window manager: %v", display, err)
	}
	defer c.close()
	deadline := time.Now().Add(xrectReadyBudget)
	for time.Now().Before(deadline) {
		if atom, err := c.internAtom(netActiveWindowName, true); err == nil && atom != 0 {
			return
		}
		time.Sleep(xrectPollStep) //stylegate:allow-sleep bounded poll for the WM's own EWMH announcement
	}
	t.Fatalf("%s never announced %s within %s — %s is not managing this display, so a client mapped now would never be focused",
		display, netActiveWindowName, xrectReadyBudget, xrectWindowManager)
}

// xrectStartFixture brings up a private Xvfb, a window manager and one xterm,
// then waits (bounded) until a window capture returns a SETTLED rectangle —
// the exact production path, not a paraphrase of it.
func xrectStartFixture(t *testing.T) (display string, geom x11Geometry) {
	t.Helper()
	display = covStartPrivateXvfb(t)
	xrectStartClient(t, display, xrectWindowManager, "--display="+display)
	xrectAwaitWindowManager(t, display)
	xrectStartClient(t, display, xrectXtermClass, "-geometry", xrectXtermGeometry, "-e", "sleep", "600")
	return display, xrectAwaitSettledWindow(t, display)
}

// xrectAwaitSettledWindow polls the ENGINE (CaptureX11Window) until it returns
// the same settled rectangle xrectSettleSamples times in a row. Three agreeing
// measurements is what distinguishes "the window manager finished placing the
// client" from "we happened to look between two placements" — a fixed sleep
// would be a guess on a loaded host, and comparing pixels of a rectangle that
// is still moving would fail for a reason that has nothing to do with the code
// under test.
func xrectAwaitSettledWindow(t *testing.T, display string) x11Geometry {
	t.Helper()
	deadline := time.Now().Add(xrectReadyBudget)
	var (
		last       x11Geometry
		identical  int
		lastErr    error
		settledFor int
	)
	for time.Now().Before(deadline) {
		_, current, err := CaptureX11Window(display, 0)
		if err == nil && xrectSettled(current) {
			if identical > 0 && xrectRectOf(current) == xrectRectOf(last) {
				identical++
			} else {
				identical = 1
			}
			last = current
			lastErr = nil
			if identical >= xrectSettleSamples {
				t.Logf("fixture settled on %s: window %s rect %s", display, last.Window, xrectRectOf(last))
				return last
			}
		} else {
			identical = 0
			if err != nil {
				lastErr = err
			} else {
				lastErr = fmt.Errorf("window capture returned %+v, which is not a settled rectangle", current)
			}
		}
		settledFor++
		time.Sleep(xrectPollStep) //stylegate:allow-sleep bounded poll while the WM maps, focuses and places its first client
	}
	t.Fatalf("no settled active window on %s within %s (%d polls, last: %v) — the fixture (%s + %s) never reached a capturable, stable state",
		display, xrectReadyBudget, settledFor, lastErr, xrectWindowManager, xrectXtermClass)
	return last
}

// xrectRequireSamePixels asserts two captures are pixel-identical and reports
// a bounded sample of the first differing pixels — a count alone says "wrong",
// these say WHERE, which is what turns a red test into a diagnosable one.
func xrectRequireSamePixels(t *testing.T, what string, want, got image.Image) {
	t.Helper()
	diff, err := xrectPixelDiff(want, got)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if diff == 0 {
		return
	}
	bounds := want.Bounds()
	var samples []string
	for y := 0; y < bounds.Dy() && len(samples) < 5; y++ {
		for x := 0; x < bounds.Dx(); x++ {
			r1, g1, b1, _ := want.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			r2, g2, b2, _ := got.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 {
				samples = append(samples, fmt.Sprintf("(%d,%d) want=%02x%02x%02x got=%02x%02x%02x",
					x, y, r1>>8, g1>>8, b1>>8, r2>>8, g2>>8, b2>>8))
				if len(samples) >= 5 {
					break
				}
			}
		}
	}
	t.Fatalf("%s: %d of %d pixels differ — first: %s", what, diff, bounds.Dx()*bounds.Dy(), strings.Join(samples, ", "))
}

// xrectAbsoluteGeometry is the INDEPENDENT oracle: xwininfo's own measurement
// of the window's absolute position and size. It returns ok=false (with the
// reason logged) when xwininfo is unavailable or cannot measure the window —
// the pixel half of the guard still runs in that case, so the test degrades to
// "weaker but not vacuous" rather than skipping entirely.
func xrectAbsoluteGeometry(t *testing.T, display, win string) (Rect, bool) {
	t.Helper()
	if _, err := exec.LookPath(xrectXwininfoBinary); err != nil {
		t.Logf("xwininfo is not installed — the independent geometry oracle is skipped (the pixel-identity oracle still runs)")
		return Rect{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), xrectXwininfoBudget)
	defer cancel()
	cmd := exec.CommandContext(ctx, xrectXwininfoBinary, "-id", win)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "DISPLAY=" + display}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("xwininfo -id %s failed (%v) — independent geometry oracle skipped:\n%s", win, err, out)
		return Rect{}, false
	}
	return xrectParseXwininfo(t, string(out))
}

// xrectParseXwininfo pulls the window's CONTENT rectangle out of an `xwininfo`
// report.
//
// The border matters and is why this is not a plain field copy: xwininfo's
// "Absolute upper-left X/Y" is the window's OUTER corner, while GetGeometry's
// width/height — and therefore the rectangle this engine captures and reports
// — describe the area INSIDE the border (a window's own coordinate space
// starts there). Measured 2026-10-03 on a WM-less server, where xterm's border
// is 1px: the outer corner is 40,30 and the content origin is 41,31. With a
// reparenting window manager the client's border is 0, so the two coincide.
func xrectParseXwininfo(t *testing.T, report string) (Rect, bool) {
	t.Helper()
	var r Rect
	var border int
	fields := map[string]*int{
		"Absolute upper-left X": &r.X,
		"Absolute upper-left Y": &r.Y,
		"Width":                 &r.W,
		"Height":                &r.H,
		"Border width":          &border,
	}
	for label, dst := range fields {
		// xwininfo prints "  Absolute upper-left X:  45"; the exact spacing is
		// its business, so the pattern is whitespace-tolerant.
		re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(label) + `:\s+(-?\d+)\s*$`)
		m := re.FindStringSubmatch(report)
		if m == nil {
			t.Logf("xwininfo report has no %q line — independent geometry oracle skipped", label)
			return Rect{}, false
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Logf("xwininfo %q is not a number (%q) — independent geometry oracle skipped", label, m[1])
			return Rect{}, false
		}
		*dst = n
	}
	r.X += border
	r.Y += border
	return r, true
}

// xrectDecode decodes a captured PNG into pixels.
func xrectDecode(t *testing.T, what string, pngBytes []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("decode %s: %v", what, err)
	}
	return img
}

// xrectPixelDiff counts the pixels whose RGBA values differ between two images
// of the SAME size. It is the render-truth instrument (Rule 10.2): a rectangle
// claim is settled by pixels, never by a plausible-looking dimension.
func xrectPixelDiff(a, b image.Image) (int, error) {
	ab, bb := a.Bounds(), b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		return 0, fmt.Errorf("size mismatch: %dx%d vs %dx%d", ab.Dx(), ab.Dy(), bb.Dx(), bb.Dy())
	}
	diff := 0
	for y := 0; y < ab.Dy(); y++ {
		for x := 0; x < ab.Dx(); x++ {
			r1, g1, b1, a1 := a.At(ab.Min.X+x, ab.Min.Y+y).RGBA()
			r2, g2, b2, a2 := b.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				diff++
			}
		}
	}
	return diff, nil
}

// TestCaptureWindowRectMatchesIndependentGeometryAndScreenCrop is the primary
// guard for the --window mode: the reported rectangle must equal an
// independent client's measurement of that window, and the captured pixels
// must be byte-identical to the same rectangle cropped out of the whole-screen
// capture.
func TestCaptureWindowRectMatchesIndependentGeometryAndScreenCrop(t *testing.T) {
	display, geom := xrectStartFixture(t)
	win := xrectWindowFromGeometry(t, geom)

	t.Run("geometry matches xwininfo", func(t *testing.T) {
		want, ok := xrectAbsoluteGeometry(t, display, win)
		if !ok {
			t.Skip("xwininfo unavailable or refused — geometry cross-check skipped VIBLY (the pixel oracle below still ran)")
		}
		got := Rect{X: geom.X, Y: geom.Y, W: geom.Width, H: geom.Height}
		if got != want {
			t.Fatalf("window %s: reported rect %s, xwininfo measures %s — the capture is not of the rectangle the server attributes to that window",
				win, got, want)
		}
	})

	t.Run("pixels equal the screen crop", func(t *testing.T) {
		rect := Rect{X: geom.X, Y: geom.Y, W: geom.Width, H: geom.Height}
		fullPNG, fullGeom, err := CaptureX11Root(display)
		if err != nil {
			t.Fatalf("whole-screen capture: %v", err)
		}
		full := xrectDecode(t, "screen capture", fullPNG)
		if fullGeom.Width != covFakeWidth || fullGeom.Height != covFakeHeight {
			t.Fatalf("screen capture reports %dx%d, the fixture root is %dx%d", fullGeom.Width, fullGeom.Height, covFakeWidth, covFakeHeight)
		}

		windowPNG, _, err := CaptureX11Window(display, 0)
		if err != nil {
			t.Fatalf("window capture: %v", err)
		}
		window := xrectDecode(t, "window capture", windowPNG)

		// The crop is taken with image.Rect, from the FULL capture, at the
		// rectangle the engine reported — two independent code paths over one
		// server state, which is what makes an absolute error of 0 meaningful.
		cropped := image.NewRGBA(image.Rect(0, 0, rect.W, rect.H))
		for y := 0; y < rect.H; y++ {
			for x := 0; x < rect.W; x++ {
				cropped.Set(x, y, full.At(rect.X+x, rect.Y+y))
			}
		}
		xrectRequireSamePixels(t, fmt.Sprintf("window capture vs the screen crop of %s", rect), cropped, window)
	})

	t.Run("area mode reproduces the same crop", func(t *testing.T) {
		rect := Rect{X: geom.X, Y: geom.Y, W: geom.Width, H: geom.Height}
		windowPNG, _, err := CaptureX11Window(display, 0)
		if err != nil {
			t.Fatalf("window capture: %v", err)
		}
		areaPNG, areaGeom, err := CaptureX11Area(display, rect)
		if err != nil {
			t.Fatalf("area capture of %s: %v", rect, err)
		}
		if areaGeom.X != rect.X || areaGeom.Y != rect.Y || areaGeom.Width != rect.W || areaGeom.Height != rect.H {
			t.Fatalf("area capture echoes %dx%d+%d+%d, asked for %s", areaGeom.Width, areaGeom.Height, areaGeom.X, areaGeom.Y, rect)
		}
		diff, err := xrectPixelDiff(xrectDecode(t, "window capture", windowPNG), xrectDecode(t, "area capture", areaPNG))
		if err != nil {
			t.Fatalf("area capture vs window capture: %v", err)
		}
		if diff != 0 {
			t.Fatalf("--area %s and --window disagree in %d pixels — the two modes are not reading the same rectangle", rect, diff)
		}
	})
	t.Run("area string round-trips through the CLI grammar", func(t *testing.T) {
		rect := Rect{X: geom.X, Y: geom.Y, W: geom.Width, H: geom.Height}
		parsed, err := ParseArea(rect.String())
		if err != nil {
			t.Fatalf("ParseArea(%q) (the rendering of the captured rect): %v", rect.String(), err)
		}
		if parsed != rect {
			t.Fatalf("ParseArea(%q) = %s, want %s — the reported geometry cannot be fed back as --area", rect.String(), parsed, rect)
		}
	})
}

// TestCaptureWindowWithoutWindowManagerUsesPointerFallback covers the
// documented fallback route: with no window manager there is no
// _NET_ACTIVE_WINDOW and nothing has taken the input focus, so the engine must
// resolve the current window through QueryPointer's child of root — and must
// then produce the very same pixels as the screen crop of that rectangle.
//
// The fixture aims the pointer with the project's own XTEST actuator at a
// point that an INDEPENDENT client (xwininfo) attributes to the client window
// — the test never hands the engine the answer it is checking for.
func TestCaptureWindowWithoutWindowManagerUsesPointerFallback(t *testing.T) {
	display := covStartPrivateXvfb(t)
	xrectStartClient(t, display, xrectXtermClass, "-geometry", xrectXtermGeometry, "-e", "sleep", "600")
	winID, winRect, ok := xrectAwaitIndependentClient(t, display, xrectXtermClass)
	if !ok {
		t.Skip("no independent window oracle (xdotool/xwininfo) available — the pointer-route fixture cannot be aimed")
	}
	// A settled rectangle is not yet a window with pixels: the client is
	// reported with its full geometry the moment it is created, and only later
	// does the server map it. Aiming the pointer at the rectangle before that
	// leaves the pointer over the ROOT, so the fixture waits for the
	// observable that the server itself publishes — the window's map state.
	xrectAwaitMappedClient(t, display, winID)

	// A WM-less server never takes the focus on its own (measured: GetInputFocus
	// answers the PointerRoot sentinel), so the fixture puts the pointer into
	// the client window itself.
	act, err := OpenXActuator(display)
	if err != nil {
		t.Fatalf("open actuator on the WM-less display %s: %v", display, err)
	}
	defer act.Close()
	px, py := winRect.X+winRect.W/2, winRect.Y+winRect.H/2
	if err := act.MovePointer(px, py); err != nil {
		t.Fatalf("move the pointer to %d,%d on %s: %v", px, py, display, err)
	}
	gotX, gotY, err := act.Pointer()
	if err != nil {
		t.Fatalf("read the pointer back on %s: %v", display, err)
	}
	if gotX != px || gotY != py {
		t.Fatalf("pointer reads %d,%d after moving to %d,%d — the fixture cannot aim the pointer oracle", gotX, gotY, px, py)
	}
	// Entering the pointer is what FOCUSES the client on a WM-less server, and
	// the client repaints itself as a result (measured: xterm redraws its
	// cursor hollow -> filled within ~4ms). Comparing a frame captured before
	// that repaint with one captured after is comparing two different instants
	// of the same window, so the fixture waits until the resolved window is
	// the oracle's own and its content has stopped changing.
	xrectAwaitPointerRoute(t, display, winID)
	xrectAwaitStablePixels(t, display)

	windowPNG, geom, err := CaptureX11Window(display, 0)
	if err != nil {
		t.Fatalf("window capture on the WM-less display %s: %v", display, err)
	}
	if geom.Window != winID {
		t.Fatalf("the pointer route resolved window %s, but the pointer is inside %s (xwininfo) — the wrong window was captured", geom.Window, winID)
	}
	if geom.WindowLabel != xrectRoutePointer {
		t.Fatalf("window capture on a WM-less display reports route %q, want %q — a fallback that is not announced cannot be told from the EWMH answer",
			geom.WindowLabel, xrectRoutePointer)
	}
	rect := xrectRectOf(geom)
	if rect != winRect {
		t.Fatalf("the pointer route reports %s for %s, xwininfo measures %s — the resolved rectangle is not the window's", rect, winID, winRect)
	}
	fullPNG, _, err := CaptureX11Root(display)
	if err != nil {
		t.Fatalf("whole-screen capture: %v", err)
	}
	full := xrectDecode(t, "screen capture", fullPNG)
	cropped := image.NewRGBA(image.Rect(0, 0, rect.W, rect.H))
	for y := 0; y < rect.H; y++ {
		for x := 0; x < rect.W; x++ {
			cropped.Set(x, y, full.At(rect.X+x, rect.Y+y))
		}
	}
	xrectRequireSamePixels(t, fmt.Sprintf("WM-less window capture vs the screen crop of %s", rect), cropped, xrectDecode(t, "window capture", windowPNG))
}

// xrectAwaitIndependentClient finds the fixture's client window through tools
// that are NOT this package (xdotool for the id, xwininfo for the rectangle)
// and waits (bounded) until it reports a settled, on-screen rectangle. It
// returns ok=false when those tools are unavailable.
func xrectAwaitIndependentClient(t *testing.T, display, class string) (string, Rect, bool) {
	t.Helper()
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Logf("xdotool is not installed — the WM-less fixture cannot be aimed at its client window")
		return "", Rect{}, false
	}
	deadline := time.Now().Add(xrectReadyBudget)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), xrectXwininfoBudget)
		cmd := exec.CommandContext(ctx, "xdotool", "search", "--class", class)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "DISPLAY=" + display}
		out, err := cmd.Output()
		cancel()
		if err == nil {
			for _, id := range strings.Fields(string(out)) {
				dec, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
				if err != nil {
					continue
				}
				hexID := fmt.Sprintf("0x%x", dec)
				if !xrectWindowMapped(t, display, hexID) {
					continue
				}
				rect, ok := xrectAbsoluteGeometry(t, display, hexID)
				if ok && xrectSettledGeometry(rect) {
					return hexID, rect, true
				}
			}
		}
		time.Sleep(xrectPollStep) //stylegate:allow-sleep bounded poll while the client maps on a WM-less server
	}
	t.Logf("no settled %s window found through xdotool on %s within %s", class, display, xrectReadyBudget)
	return "", Rect{}, false
}

// xrectWindowMapped asks an INDEPENDENT client (xwininfo) whether the window
// is actually on screen. The map state is the server's own observable for
// "this window has pixels", and it is what a settled rectangle does NOT
// promise: measured 2026-10-03, a freshly created WM-less client reports its
// full 184x134+41+31 while xwininfo answers "IsUnMapped", so the pointer aimed
// at that rectangle lands on the ROOT window and the engine correctly refuses
// to resolve a window at all.
func xrectWindowMapped(t *testing.T, display, win string) bool {
	t.Helper()
	if _, err := exec.LookPath(xrectXwininfoBinary); err != nil {
		return true // no oracle: the caller's own settle check remains the criterion
	}
	ctx, cancel := context.WithTimeout(context.Background(), xrectXwininfoBudget)
	defer cancel()
	cmd := exec.CommandContext(ctx, xrectXwininfoBinary, "-id", win)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "DISPLAY=" + display}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) != xrectMapStateLabel {
			continue
		}
		return strings.TrimSpace(value) == xrectMapStateMapped
	}
	return false
}

// xrectAwaitMappedClient waits (bounded) until the fixture's client window is
// MAPPED — the precondition the pointer route needs to resolve it at all. It
// is a dependency on the server's own state, not a delay: the pointer can only
// be inside a window that exists on screen.
func xrectAwaitMappedClient(t *testing.T, display, win string) {
	t.Helper()
	deadline := time.Now().Add(xrectReadyBudget)
	for time.Now().Before(deadline) {
		if xrectWindowMapped(t, display, win) {
			t.Logf("fixture client %s is mapped on %s", win, display)
			return
		}
		time.Sleep(xrectPollStep) //stylegate:allow-sleep bounded poll for the client's own map state
	}
	t.Fatalf("client window %s on %s never became %s within %s — a pointer aimed at it would read the root window instead",
		win, display, xrectMapStateMapped, xrectReadyBudget)
}

// xrectAwaitPointerRoute waits (bounded) until the engine's WM-less route
// resolves the window the independent oracle named. Until the pointer is
// genuinely inside a mapped client the route correctly answers "no active
// window" (it falls back from _NET_ACTIVE_WINDOW to GetInputFocus to
// QueryPointer, and the pointer is still over the root), so this waits on the
// ACTUAL condition under test instead of racing it.
func xrectAwaitPointerRoute(t *testing.T, display, want string) {
	t.Helper()
	deadline := time.Now().Add(xrectReadyBudget)
	var lastErr error
	for time.Now().Before(deadline) {
		_, geom, err := CaptureX11Window(display, 0)
		if err == nil && geom.Window == want {
			return
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("the pointer route resolved %s, the oracle names %s", geom.Window, want)
		}
		time.Sleep(xrectPollStep) //stylegate:allow-sleep bounded poll until the pointer route resolves the client
	}
	t.Fatalf("the pointer route never resolved %s on %s within %s (last: %v) — the fixture cannot be aimed at its client window",
		want, display, xrectReadyBudget, lastErr)
}

// xrectAwaitStablePixels waits (bounded) until the resolved window's CAPTURED
// CONTENT stops changing: xrectPixelSettleFrames consecutive pixel-identical
// frames. It is the fit criterion for the pixel oracle, and it replaces an
// unconditional single sample: entering the pointer FOCUSES the client on a
// WM-less server and the client repaints as a result (measured 2026-10-03:
// xterm redraws its cursor hollow -> filled within ~4ms, exactly the 44 pixels
// of the cursor cell), so two captures taken around that repaint differ in the
// window's own content, not in the rectangle either capture is claiming.
func xrectAwaitStablePixels(t *testing.T, display string) {
	t.Helper()
	deadline := time.Now().Add(xrectReadyBudget)
	var prev []byte
	identical := 0
	for time.Now().Before(deadline) {
		pngBytes, geom, err := CaptureX11Window(display, 0)
		if err != nil {
			identical = 0
			prev = nil
			time.Sleep(xrectPollStep) //stylegate:allow-sleep bounded poll while the client repaints after focus
			continue
		}
		if prev != nil && bytes.Equal(prev, pngBytes) {
			identical++
			if identical >= xrectPixelSettleFrames {
				t.Logf("fixture client content settled on %s: %s after %d identical frames", display, xrectRectOf(geom), identical)
				return
			}
		} else {
			identical = 1
		}
		prev = pngBytes
		time.Sleep(xrectPollStep) //stylegate:allow-sleep bounded poll while the client repaints after focus
	}
	t.Fatalf("the client window on %s never produced %d identical frames within %s — the pixel oracle would compare two instants of a moving capture",
		display, xrectPixelSettleFrames, xrectReadyBudget)
}

// xrectSettledGeometry is xrectSettled for an already-measured rectangle.
func xrectSettledGeometry(r Rect) bool {
	if r.W < xrectMinWindowSide || r.H < xrectMinWindowSide {
		return false
	}
	return r.within(covFakeWidth, covFakeHeight) == nil
}

// TestCaptureWindowRefusesAnUnreachableWindow pins the failure side: a window
// id that does not exist must be refused loudly (an X11 error), never answered
// with a plausible rectangle.
func TestCaptureWindowRefusesAnUnreachableWindow(t *testing.T) {
	display := covStartPrivateXvfb(t)
	const noSuchWindow = 0x7f000001
	_, geom, err := CaptureX11Window(display, noSuchWindow)
	if err == nil {
		t.Fatalf("capturing window 0x%x succeeded (geometry %+v) — a nonexistent window must be refused", noSuchWindow, geom)
	}
	if !strings.Contains(err.Error(), "0x7f000001") {
		t.Errorf("the refusal does not name the window id it failed on: %v — a caller cannot tell WHICH id was wrong", err)
	}
}

// TestCaptureAreaRefusesOffScreenRectangles pins the boundary contract: a
// rectangle that runs past the root window is refused by name instead of being
// captured as a silently clipped (or padded) image.
func TestCaptureAreaRefusesOffScreenRectangles(t *testing.T) {
	display := covStartPrivateXvfb(t)
	cases := []struct {
		name string
		rect Rect
	}{
		{"width past the right edge", Rect{X: covFakeWidth - 10, Y: 0, W: 20, H: 10}},
		{"height past the bottom edge", Rect{X: 0, Y: covFakeHeight - 10, W: 10, H: 20}},
		{"entirely off screen", Rect{X: covFakeWidth + 1, Y: covFakeHeight + 1, W: 10, H: 10}},
		{"zero width", Rect{X: 0, Y: 0, W: 0, H: 10}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := CaptureX11Area(display, tc.rect)
			if err == nil {
				t.Fatalf("capturing %s on a %dx%d root succeeded — an out-of-range area must be refused", tc.rect, covFakeWidth, covFakeHeight)
			}
			if !strings.Contains(err.Error(), tc.rect.String()) {
				t.Errorf("the refusal does not quote the rectangle it rejected: %v", err)
			}
		})
	}
}

// TestParseArea covers the CLI grammar. The valid cases matter as much as the
// refusals: "800x600" (no offsets) is the documented shorthand for the
// top-left rectangle, and every malformed spelling must be refused with the
// offending text rather than silently become a screen capture.
func TestParseArea(t *testing.T) {
	valid := map[string]Rect{
		"800x600":          {X: 0, Y: 0, W: 800, H: 600},
		"800x600+100+50":   {X: 100, Y: 50, W: 800, H: 600},
		"800X600+100+50":   {X: 100, Y: 50, W: 800, H: 600},
		"  1920x1080+0+0 ": {X: 0, Y: 0, W: 1920, H: 1080},
		"1x1+0+0":          {X: 0, Y: 0, W: 1, H: 1},
		"640x480+10":       {X: 10, Y: 0, W: 640, H: 480},
	}
	for in, want := range valid {
		got, err := ParseArea(in)
		if err != nil {
			t.Errorf("ParseArea(%q): unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseArea(%q) = %s, want %s", in, got, want)
		}
		if got.String() != fmt.Sprintf("%dx%d+%d+%d", want.W, want.H, want.X, want.Y) {
			t.Errorf("ParseArea(%q).String() = %q — the rendering must round-trip through ParseArea", in, got.String())
		}
	}

	invalid := []string{
		"", " ", "800", "x600", "800x", "abcx600", "800xabc", "800x600+",
		"800x600+1+2+3", "800x600+a+2", "800x600+1+b", "0x600", "800x0",
		"-800x600", "800x600+-10+0", "800x600+10+-5",
	}
	for _, in := range invalid {
		got, err := ParseArea(in)
		if err == nil {
			t.Errorf("ParseArea(%q) = %s, want an error — a malformed rectangle must never be guessed", in, got)
			continue
		}
		if !strings.Contains(err.Error(), "area") {
			t.Errorf("ParseArea(%q) error does not say it is about the area: %v", in, err)
		}
	}
}

// TestRectWithin pins the boundary predicate: the exact edges are INSIDE and
// one pixel past either edge is outside, so an off-by-one can neither clip a
// capture nor refuse a fitting one.
func TestRectWithin(t *testing.T) {
	cases := []struct {
		name string
		rect Rect
		ok   bool
	}{
		{"exact fit", Rect{X: 0, Y: 0, W: 640, H: 480}, true},
		{"bottom-right corner", Rect{X: 630, Y: 470, W: 10, H: 10}, true},
		{"one past the right edge", Rect{X: 631, Y: 0, W: 10, H: 10}, false},
		{"one past the bottom edge", Rect{X: 0, Y: 471, W: 10, H: 10}, false},
		{"negative origin", Rect{X: -1, Y: 0, W: 10, H: 10}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rect.within(640, 480)
			if tc.ok && err != nil {
				t.Fatalf("%s on 640x480 was refused: %v", tc.rect, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("%s on 640x480 was accepted, want a refusal", tc.rect)
			}
		})
	}
}

// TestCaptureX11DispatchRejectsUnknownMode pins Rule 15 on the mode selector: a
// misspelled mode must refuse by name rather than fall through to a default
// capture (a silent substitution of "the whole screen" for what was asked).
func TestCaptureX11DispatchRejectsUnknownMode(t *testing.T) {
	_, _, err := CaptureX11(CaptureOptions{Mode: "windoww", Display: ":0"})
	if err == nil {
		t.Fatal("an unknown capture mode was accepted")
	}
	if !strings.Contains(err.Error(), "windoww") {
		t.Errorf("the refusal does not quote the unknown mode: %v", err)
	}
	for _, mode := range []string{CaptureModeScreen, CaptureModeWindow, CaptureModeArea} {
		if !strings.Contains(err.Error(), mode) {
			t.Errorf("the refusal does not list the legal mode %q: %v", mode, err)
		}
	}
}

// TestCaptureModesAreTheWireValuesTheCLICompares guards the one thing a
// re-spelling would break silently: these three strings travel to the CLI, the
// tool result and the model's arguments, so their VALUES are a contract.
func TestCaptureModesAreTheWireValuesTheCLICompares(t *testing.T) {
	want := map[string]string{
		CaptureModeScreen: "screen",
		CaptureModeWindow: "window",
		CaptureModeArea:   "area",
	}
	for got, wantValue := range want {
		if got != wantValue {
			t.Errorf("capture mode %q must stay spelled %q (the CLI flag and the tool argument are compared against it)", got, wantValue)
		}
	}
}

// xrectScratchIsOffTheRootPartition keeps Rule 22 honest for this fixture: the
// private Xvfb writes its log and socket where the shared scratch policy says,
// never into the source tree (a scratch directory inside the checkout is how
// untracked fixtures end up in a commit — Rule 32).
func TestXrectFixtureWritesNoFilesIntoTheSourceTree(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "xrect") && strings.HasSuffix(name, ".log") {
			t.Fatalf("the rect-capture fixture left %s in the source tree — fixture output belongs under the shared scratch root", name)
		}
	}
}

// TestX11OpcodeConstantsMatchTheProtocolHeader is the structural oracle that
// would have caught the bug this file was written around in one second.
//
// The wire requests here are spoken by NUMBER, and a number that is off by a
// few is not a compile error — it is a well-formed request for a DIFFERENT
// thing. Measured 2026-10-03: a first revision declared GetGeometry as opcode
// 3 (which is GetWindowAttributes, 14 being the real GetGeometry), so every
// window rectangle was decoded out of an attributes reply and read something
// like 65535x65535 while every whole-screen test stayed green (that path never
// issues the request at all). The authority is X11/Xproto.h, and it is read
// here rather than trusted: a constant drifting from the header fails the
// build.
func TestX11OpcodeConstantsMatchTheProtocolHeader(t *testing.T) {
	const header = "/usr/include/X11/Xproto.h"
	src, err := os.ReadFile(header)
	if err != nil {
		t.Skipf("%s is not available (no X11 development headers) — the opcode oracle is skipped VIBLY", header)
	}
	want := map[string]byte{
		"GetGeometry":     x11OpGetGeometry,
		"QueryTree":       x11OpQueryTree,
		"InternAtom":      x11OpInternAtom,
		"GetProperty":     x11OpGetProperty,
		"TranslateCoords": x11OpTranslateCoords,
		"GetImage":        x11OpGetImage,
		"QueryExtension":  x11OpQueryExtension,
		"QueryPointer":    x11OpQueryPointer,
		"GetInputFocus":   x11OpGetInputFocus,
	}
	// GetWindowAttributes is the opcode a first revision MISTOOK for
	// GetGeometry (3); naming it here keeps the collision this guard is about
	// visible in the test itself.
	_ = x11OpGetWindowAttributes
	for name, got := range want {
		name := strings.TrimSuffix(name, "s")
		value, ok := x11HeaderOpcode(string(src), name)
		if !ok {
			continue // the header does not define this name — the mapping is the authority
		}
		if value != got {
			t.Errorf("x11 opcode %s = %d, but %s says %d — the request would be a different request (a wrong opcode is well-formed and silently answers something else)",
				name, got, header, value)
		}
	}
}

// x11HeaderOpcode pulls "#define X_<name> <n>" out of an X11 protocol header.
// The trailing space is load-bearing: without it a prefix collision (X_QueryT
// vs X_QueryTree) or a substring match (X_GetGeometry inside a comment) would
// be accepted.
func x11HeaderOpcode(src, name string) (byte, bool) {
	re := regexp.MustCompile(`(?m)^#define\s+X_` + regexp.QuoteMeta(name) + `\s+\(?(\d+)\)?\s*$`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n > 255 {
		return 0, false
	}
	return byte(n), true
}

// TestWindowSizeOfTheRootMatchesTheSetupReply is the BEHAVIOURAL oracle for
// the reply decode: the root window's own GetGeometry must report the very
// width/height the connection setup already told us (x11Client.width/height,
// parsed independently in parseSetup). It needs no external tool and catches a
// wrong opcode AND a wrong field offset in one assertion — both bugs measured
// on 2026-10-03 would have shown up here immediately.
func TestWindowSizeOfTheRootMatchesTheSetupReply(t *testing.T) {
	display := covStartPrivateXvfb(t)
	c, err := dialX11(display)
	if err != nil {
		t.Fatalf("connect to %s: %v", display, err)
	}
	defer c.close()
	w, h, err := c.windowSize(c.root)
	if err != nil {
		t.Fatalf("GetGeometry(root 0x%x): %v", c.root, err)
	}
	if w != c.width || h != c.height {
		t.Fatalf("GetGeometry(root) reports %dx%d, the connection setup reports %dx%d — the request or the reply decode is wrong",
			w, h, c.width, c.height)
	}
}
