// x11rect.go — the RECTANGLE-SCOPED capture legs of the hand-rolled X11
// client: screen (whole root window), active window, an arbitrary rectangle of
// the screen, and the window HOSTING this process (pidwindow.go — the one that
// answers "where am I running" rather than "what is focused").
//
// WHY THIS EXISTS (devdoc/screen-capture-window-area.md): the whole-desktop
// capture is the right default for "what is on screen right now", but it is
// the wrong tool for the far more common computer-use question — "what is the
// CURRENT WINDOW doing?". A 1920x1080 PNG of the whole desktop spends most of
// its pixels on unrelated windows, and every downstream reader (vision_analyze,
// a human, an OCR pass) pays for them. `--window` and `--area` are the two
// rect-scoped shapes the common OS screenshot tool exposes (whole screen /
// window / rectangle), and this file implements them on the SAME protocol
// client — no new dependency, no external screenshot binary (Rule 10.7).
//
// The protocol legs are the ones the X11 core spec names for exactly these
// questions, and each is a PLAIN REQUEST/REPLY (no extension required, so they
// work on a bare Xvfb exactly as they do on a full desktop):
//
//	GetGeometry        (3)  — a drawable's own width/height (and its root)
//	QueryTree          (15) — a window's parent + children
//	InternAtom         (16) — resolve _NET_ACTIVE_WINDOW to an atom id
//	GetProperty        (20) — read that atom off the root window
//	TranslateCoords    (40) — a window's origin in ROOT coordinates
//	GetImage           (73) — the pixels, already used for the whole screen
//
// "The active window" is answered by the EWMH property _NET_ACTIVE_WINDOW on
// the root window, with a documented fallback chain (see activeWindow): the
// property is what a window manager maintains, so a WM-less server has no
// notion of "the current window" and the fallback says so through the
// input-focus/walk-up route rather than inventing one.
package shot

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"strings"
)

const (
	// Request opcodes we use (X11 core protocol; the two already used by the
	// whole-screen path are declared beside it in x11.go).
	//
	// x11OpGetGeometry is 14, NOT the 3 that reads like "the third request":
	// X_GetWindowAttributes is 3, and a GetGeometry-shaped decode of that
	// reply is a 44-byte lie (measured 2026-10-03: a wrong opcode here made
	// every window rectangle read 65535x65535, carved out of the attribute
	// reply's fields). The authority is X11/Xproto.h, and both constants below
	// are checked against it by a test.
	x11OpGetGeometry     = 14
	x11OpQueryTree       = 15
	x11OpInternAtom      = 16
	x11OpGetProperty     = 20
	x11OpTranslateCoords = 40

	// x11OpGetWindowAttributes is NOT used by any request this client sends.
	// It is named here, and asserted in the tests, because it is the opcode a
	// first revision of this file mistook for GetGeometry: 3 sits where a
	// "third request" guess lands, and its reply is well-formed, so the
	// mistake produces plausible bytes instead of an error.
	x11OpGetWindowAttributes = 3

	// x11GetGeometryRequestLen / x11QueryTreeRequestLen /
	// x11TranslateCoordsRequestLen are those requests' sizes in 4-byte units.
	x11GetGeometryRequestLen     = 2
	x11QueryTreeRequestLen       = 2
	x11TranslateCoordsRequestLen = 4
	x11GetPropertyRequestLen     = 6 // opcode..long-length inclusive

	// x11InternAtomOnlyIfExists asks the server NOT to create the atom when it
	// is unknown (a missing _NET_ACTIVE_WINDOW is information: no EWMH WM).
	x11InternAtomOnlyIfExists = 1

	// x11PropertyTypeAny is GetProperty's type field meaning "any type".
	x11PropertyTypeAny = 0

	// x11FormatBits32 is the property format |value| GetProperty reports for a
	// window id (an X11 ATOM/WINDOW is a 32-bit value); x11FormatBits8 is the
	// format of a STRING property (a window title).
	x11FormatBits32 = 32
	x11FormatBits8  = 8

	// x11WindowIDFormat is the ONE spelling of an X11 window id in text. Every
	// id a human reads or pastes back (a capture's window_label, a listing's id)
	// is hex with a 0x prefix, and it must be the SAME spelling in both places:
	// a listing that offered decimal would hand back an id the `--window 0x…`
	// path cannot take.
	x11WindowIDFormat = "0x%x"

	// x11WindowWalkMaxDepth bounds the QueryTree walk from a focused window up
	// to its top-level frame. A window tree deeper than this is a broken
	// server, not a layout, so the walk stops and reports what it has.
	x11WindowWalkMaxDepth = 16

	// netActiveWindowName is the EWMH property a window manager sets on the
	// root window to name the focused client window.
	netActiveWindowName = "_NET_ACTIVE_WINDOW"

	// The ROUTE labels a window capture reports: which mechanism answered
	// "which window is current". They are constants (not literals) because the
	// tests assert on them, and a route that is not named is a fallback the
	// reader cannot tell from the authoritative answer (Rule 15).
	x11RouteNetActiveWindow = netActiveWindowName
	x11RouteInputFocus      = "GetInputFocus"
	x11RoutePointer         = "QueryPointer"
)

// windowIDString spells a window id the one way every surface does (see
// x11WindowIDFormat): a capture's window_label and a listing's `id` are
// therefore exactly the string a caller pastes back into `--window 0x…`.
func windowIDString(window uint32) string {
	return fmt.Sprintf(x11WindowIDFormat, window)
}

// Rect is a rectangle in ROOT-window (screen) coordinates.
type Rect struct {
	X int
	Y int
	W int
	H int
}

// String renders the rect in the common OS screenshot geometry spelling
// (WxH+X+Y), which is also the spelling ParseArea accepts — one grammar, one
// rendering, so an error message can be pasted back as an argument.
func (r Rect) String() string {
	return fmt.Sprintf("%dx%d+%d+%d", r.W, r.H, r.X, r.Y)
}

// CaptureModes are the three capture shapes. They are the wire values shared
// with the tool layer and the CLI (Rule 14: one definition, referenced).
//
// CaptureModeScreen is NOT a second definition: the whole-screen surface
// already owns that value as surface.SurfaceScreen, and this names the same
// thing through it rather than restating the literal (the surface gate would
// flag a duplicate, and a duplicate is what would drift).
const (
	CaptureModeScreen = SurfaceScreen
	CaptureModeWindow = "window"
	CaptureModeArea   = "area"

	// CaptureModeSelf is the window that HOSTS the calling process, resolved by
	// pid (_NET_WM_PID) through the process' own tty chain — never by focus.
	// It is a distinct mode because it answers a different question from
	// CaptureModeWindow ("who is running me" vs "what is focused"), and the
	// measured failure it removes (2026-09-30: a capture taken from a terminal
	// returned another application's window, because the terminal was not the
	// ACTIVE window) is a WRONG ANSWER, not a tuning difference.
	// See pidwindow.go.
	CaptureModeSelf = "self"
)

// CaptureOptions selects WHAT to capture. Mode "" means CaptureModeScreen.
// Window is consulted for CaptureModeWindow (0 = the active window); Area for
// CaptureModeArea; Decorations and any of these for CaptureModeSelf (the window
// hosting this process — see pidwindow.go).
type CaptureOptions struct {
	Mode    string
	Display string
	Window  uint32
	Area    Rect

	// Decorations selects the window-manager FRAME over the client area for
	// CaptureModeSelf: a frame capture includes the title bar and borders
	// (_NET_FRAME_EXTENTS), which is what "a screenshot of the window" means to
	// a reader. The default (false) is the client area, i.e. exactly the pixels
	// the application drew.
	Decorations bool
}

// ParseArea parses the area grammar "WxH+X+Y" (offsets optional, so "800x600"
// is the top-left 800x600 rectangle). It is deliberately STRICT: a malformed
// rectangle is refused with the offending text and the grammar, never silently
// turned into a screen capture (Rule 15: a substituted geometry is visible).
func ParseArea(s string) (Rect, error) {
	var r Rect
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return r, fmt.Errorf("area is empty (want WxH+X+Y, e.g. 800x600+100+50)")
	}
	sep := strings.IndexAny(trimmed, "xX")
	if sep <= 0 {
		return r, fmt.Errorf("area %q: want WxH[+X+Y] (e.g. 800x600+100+50)", s)
	}
	w, err := strconv.Atoi(trimmed[:sep])
	if err != nil {
		return r, fmt.Errorf("area %q: width %q is not a number", s, trimmed[:sep])
	}
	rest := trimmed[sep+1:]
	// Split the remainder on '+' — the first field is the height, then X, Y.
	fields := strings.Split(rest, "+")
	h, err := strconv.Atoi(fields[0])
	if err != nil {
		return r, fmt.Errorf("area %q: height %q is not a number", s, fields[0])
	}
	if len(fields) > 3 {
		return r, fmt.Errorf("area %q: want WxH[+X+Y], got %d offset fields", s, len(fields)-1)
	}
	r = Rect{W: w, H: h}
	if len(fields) > 1 {
		x, err := strconv.Atoi(fields[1])
		if err != nil {
			return r, fmt.Errorf("area %q: x %q is not a number", s, fields[1])
		}
		r.X = x
	}
	if len(fields) > 2 {
		y, err := strconv.Atoi(fields[2])
		if err != nil {
			return r, fmt.Errorf("area %q: y %q is not a number", s, fields[2])
		}
		r.Y = y
	}
	if r.W <= 0 || r.H <= 0 {
		return r, fmt.Errorf("area %q: width and height must be positive (got %dx%d)", s, r.W, r.H)
	}
	if r.X < 0 || r.Y < 0 {
		return r, fmt.Errorf("area %q: offsets must not be negative (got +%d+%d)", s, r.X, r.Y)
	}
	return r, nil
}

// CaptureX11 dispatches on the requested mode. Every mode shares one X11
// connection path, one banded GetImage and one geometry echo, so the modes can
// never drift into three different capture implementations.
//
// It goes through CaptureX11Provider — the seam at the X11 boundary — so a
// caller can be driven without a live display (see that variable).
func CaptureX11(opts CaptureOptions) ([]byte, x11Geometry, error) {
	return CaptureX11Provider(opts)
}

// CaptureX11Provider is THE seam at the X11 boundary: the function that
// actually talks to a display server, and therefore the one place a test
// replaces to drive a whole capture path — `gopi tool capture`, `gopi
// screen_capture` and the `screen_capture` tool included — with no display,
// no X socket and no Xvfb (PROJECT.md Rule 31: a surface driven only up to its
// parser has not been driven).
//
// It is exported for the same reason shot.SelfPageProvider is: the consumer
// that must be able to substitute it lives in another package. Production code
// NEVER assigns it; every assignment is a test's, and every test restores it.
//
// The seam sits HERE, on the dispatcher, rather than on each mode function, so
// that a bypass is a single, testable question: does the capture path reach
// this variable? TestCaptureEngineGoesThroughTheX11Seam answers it and fails
// red if a future entry point grabs the display behind the seam's back.
var CaptureX11Provider = captureX11Dispatch

// captureX11Dispatch is the real dispatcher behind CaptureX11Provider. It is
// reached only through that variable, so no code path can quietly skip the
// seam.
func captureX11Dispatch(opts CaptureOptions) ([]byte, x11Geometry, error) {
	switch opts.Mode {
	case "", CaptureModeScreen:
		return CaptureX11Root(opts.Display)
	case CaptureModeWindow:
		return CaptureX11Window(opts.Display, opts.Window)
	case CaptureModeArea:
		return CaptureX11Area(opts.Display, opts.Area)
	case CaptureModeSelf:
		return CaptureX11Self(opts.Display, opts.Decorations)
	default:
		return nil, x11Geometry{}, fmt.Errorf("unknown capture mode %q (want %s|%s|%s|%s)", opts.Mode, CaptureModeScreen, CaptureModeWindow, CaptureModeArea, CaptureModeSelf)
	}
}

// CaptureX11Area grabs the given rectangle of the display's root window.
// display == "" means $DISPLAY. The rectangle is validated against the root
// window's own size (GetGeometry through the shared client), so an area that
// falls off the screen is refused rather than captured as a partial image.
func CaptureX11Area(display string, r Rect) ([]byte, x11Geometry, error) {
	var geom x11Geometry
	if r.W <= 0 || r.H <= 0 {
		return nil, geom, fmt.Errorf("area %s: width and height must be positive", r)
	}
	name := captureDisplayName(display)
	if name == "" {
		return nil, geom, x11ErrNoDisplay()
	}
	c, err := dialX11(name)
	if err != nil {
		return nil, geom, err
	}
	defer c.close()
	if err := r.within(c.width, c.height); err != nil {
		return nil, geom, fmt.Errorf("area %s on a %dx%d display: %w", r, c.width, c.height, err)
	}
	return c.captureRect(name, r, CaptureModeArea, "", 0)
}

// CaptureX11Window grabs the active window (window == 0) or an explicitly
// named window id of the display. display == "" means $DISPLAY.
//
// The captured rectangle is the window's CONTENT rectangle in root
// coordinates: its own size from GetGeometry, its origin from
// TranslateCoordinates(window → root). Window-manager decorations are NOT
// included — the content is what the window is showing, and it is the same
// rectangle an independent client (`xwininfo -id W`) reports, which is what
// makes the pixel-identity oracle in x11rect_test.go possible.
func CaptureX11Window(display string, window uint32) ([]byte, x11Geometry, error) {
	var geom x11Geometry
	name := captureDisplayName(display)
	if name == "" {
		return nil, geom, x11ErrNoDisplay()
	}
	c, err := dialX11(name)
	if err != nil {
		return nil, geom, err
	}
	defer c.close()

	target := window
	label := ""
	if target == 0 {
		target, label, err = c.activeWindow()
		if err != nil {
			return nil, geom, err
		}
	}
	r, err := c.windowRect(target)
	if err != nil {
		return nil, geom, err
	}
	if err := r.within(c.width, c.height); err != nil {
		return nil, geom, fmt.Errorf("window 0x%x at %s: %w", target, r, err)
	}
	return c.captureRect(name, r, CaptureModeWindow, label, target)
}

// within reports why a rectangle cannot be captured from a root window of
// rootW x rootH.
func (r Rect) within(rootW, rootH int) error {
	if rootW <= 0 || rootH <= 0 {
		return fmt.Errorf("the display reports a %dx%d root window", rootW, rootH)
	}
	if r.X < 0 || r.Y < 0 {
		return fmt.Errorf("the rectangle starts outside the root window at +%d+%d", r.X, r.Y)
	}
	if r.X+r.W > rootW || r.Y+r.H > rootH {
		return fmt.Errorf("the rectangle runs past the root window: %s does not fit %dx%d", r, rootW, rootH)
	}
	return nil
}

// captureRect is the ONE capture body: a banded GetImage over r plus the
// geometry echo both rect modes return.
func (c *x11Client) captureRect(display string, r Rect, mode, label string, window uint32) ([]byte, x11Geometry, error) {
	geom := x11Geometry{
		Display: c.connectedDisplay,
		X:       r.X,
		Y:       r.Y,
		Width:   r.W,
		Height:  r.H,
		Depth:   c.depth,
		Mode:    mode,
	}
	if window != 0 {
		geom.Window = windowIDString(window)
	}
	if label != "" {
		geom.WindowLabel = label
	}
	img, bands, err := c.getRectImage(r)
	if err != nil {
		return nil, geom, err
	}
	geom.Bands = bands
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, geom, fmt.Errorf("png encode: %w", err)
	}
	return buf.Bytes(), geom, nil
}

// getRectImage grabs the rectangle r of the root window in horizontal bands.
// The whole-screen path (getRootImage) is this function with the full root
// rectangle, so the banding, the reply framing and the BGRX→RGBA swap exist
// once.
func (c *x11Client) getRectImage(r Rect) (*image.RGBA, int, error) {
	img := image.NewRGBA(image.Rect(0, 0, r.W, r.H))
	bands := 0
	for y := 0; y < r.H; y += x11BandRows {
		h := x11BandRows
		if y+h > r.H {
			h = r.H - y
		}
		if err := c.getImageBandRect(img, r, y, h); err != nil {
			return nil, bands, err
		}
		bands++
	}
	return img, bands, nil
}

// getImageBandRect requests `h` scanlines starting at r.Y+y within the
// rectangle r and copies them into img (whose own origin is the rectangle's
// top-left corner).
func (c *x11Client) getImageBandRect(img *image.RGBA, r Rect, y, h int) error {
	req := make([]byte, x11GetImageRequestLen*4)
	req[0] = x11OpGetImage
	req[1] = x11ImageFormatZPixmap
	binary.NativeEndian.PutUint16(req[2:], x11GetImageRequestLen)
	binary.NativeEndian.PutUint32(req[4:], c.root)
	binary.NativeEndian.PutUint16(req[8:], uint16(r.X))    // x
	binary.NativeEndian.PutUint16(req[10:], uint16(r.Y+y)) // y
	binary.NativeEndian.PutUint16(req[12:], uint16(r.W))   // w
	binary.NativeEndian.PutUint16(req[14:], uint16(h))     // h
	binary.NativeEndian.PutUint32(req[16:], 0xffffffff)    // plane-mask: all planes
	if err := c.request(req); err != nil {
		return err
	}
	_, data, err := c.readReply("GetImage")
	if err != nil {
		return err
	}
	stride := r.W * x11BitsPerPixel4
	want := stride * h
	if len(data) < want {
		return fmt.Errorf("GetImage returned %d bytes, want %d (rect %s band y=%d h=%d) — the server truncated the reply", len(data), want, r, r.Y+y, h)
	}
	for row := 0; row < h; row++ {
		src := data[row*stride : row*stride+stride]
		dst := img.Pix[img.PixOffset(0, y+row) : img.PixOffset(0, y+row)+stride]
		// 32bpp ZPixmap on a little-endian server is B,G,R,X per pixel;
		// image.RGBA wants R,G,B,A. Swap and force alpha opaque.
		for i := 0; i < len(src); i += x11BitsPerPixel4 {
			dst[i+0] = src[i+2]
			dst[i+1] = src[i+1]
			dst[i+2] = src[i+0]
			dst[i+3] = 0xff
		}
	}
	return nil
}

// internAtom resolves a name to an atom id. onlyIfExists asks the server not
// to create it; an unknown atom then comes back as 0 (the caller reads that as
// "this server does not know the property" rather than as an error).
func (c *x11Client) internAtom(name string, onlyIfExists bool) (uint32, error) {
	pad := pad4(len(name))
	req := make([]byte, 8+len(name)+pad)
	req[0] = x11OpInternAtom
	if onlyIfExists {
		req[1] = x11InternAtomOnlyIfExists
	}
	binary.NativeEndian.PutUint16(req[2:], uint16(len(req)/4))
	binary.NativeEndian.PutUint16(req[4:], uint16(len(name)))
	copy(req[8:], name)
	if err := c.request(req); err != nil {
		return 0, err
	}
	header, _, err := c.readReply("InternAtom " + name)
	if err != nil {
		return 0, err
	}
	return binary.NativeEndian.Uint32(header[8:12]), nil
}

// x11GetOtherLen rounds a byte length up to 4-byte units.
func x11GetOtherLen(n int) int { return (n + 3) / 4 }

// activeWindow returns the window a capture of "the current window" means,
// plus a human label naming WHICH route answered (so a fallback is visible in
// the result and never mistaken for the EWMH answer).
//
// Route 1 — _NET_ACTIVE_WINDOW on the root window (the EWMH property a window
// manager maintains). This is the authoritative answer on any desktop.
// Route 2 — GetInputFocus, then QueryTree walking UP to the child of root.
// The focus is a real window id on an ICCCM-style setup with no EWMH property;
// the walk finds the top-level frame the focused window belongs to (the
// focused window itself is often an INPUT-ONLY or an inner widget window).
// A focus of PointerRoot (1) / None (0) is NOT a window: it carries no answer,
// so the walk is not attempted and the error names the missing facility.
func (c *x11Client) activeWindow() (uint32, string, error) {
	if atom, err := c.internAtom(netActiveWindowName, true); err == nil && atom != 0 {
		if win, err := c.windowProperty(c.root, atom); err == nil && win != 0 && win != c.root {
			return win, x11RouteNetActiveWindow, nil
		}
	}
	focus, err := c.inputFocus()
	if err != nil {
		return 0, "", fmt.Errorf("could not determine the active window: %s is unset and GetInputFocus failed: %w", netActiveWindowName, err)
	}
	if focus != 0 && focus != x11FocusPointerRoot {
		win, err := c.topLevelWindow(focus)
		if err != nil {
			return 0, "", err
		}
		return win, x11RouteInputFocus, nil
	}
	// Route 3 — QueryPointer's child. A server with NO window manager (a bare
	// Xvfb, a kiosk client) has no EWMH property and — until something
	// explicitly focuses a window — reports the PointerRoot sentinel from
	// GetInputFocus, which names no window. The pointer's child of root is
	// then the only window the user is actually "in", so it is the honest
	// answer to "the current window".
	child, err := c.pointerWindow()
	if err != nil {
		return 0, "", err
	}
	if child == 0 {
		return 0, "", fmt.Errorf("no active window: nothing maintains %s, the input focus is not a window (focus=0x%x) and the pointer is over the root window itself — name a window id explicitly, or capture the whole screen", netActiveWindowName, focus)
	}
	return child, x11RoutePointer, nil
}

// pointerWindow issues QueryPointer (opcode 38) and returns the child of the
// ROOT window the pointer currently lies in (0 when the pointer is over the
// root window's own background). It is the last-resort answer to "which window
// is the user in" and is deliberately NOT part of captureRect — it only
// resolves an ID; every position still comes from TranslateCoordinates.
func (c *x11Client) pointerWindow() (uint32, error) {
	req := make([]byte, x11QueryPointerWords*4)
	req[0] = x11OpQueryPointer
	binary.NativeEndian.PutUint16(req[2:], x11QueryPointerWords)
	binary.NativeEndian.PutUint32(req[4:], c.root)
	if err := c.request(req); err != nil {
		return 0, err
	}
	header, _, err := c.readReply("QueryPointer")
	if err != nil {
		return 0, err
	}
	if header[1] == 0 {
		return 0, fmt.Errorf("the pointer is not on this display's screen (QueryPointer reports same-screen=false) — name a window id explicitly")
	}
	return binary.NativeEndian.Uint32(header[12:16]), nil
}

// x11FocusPointerRoot is GetInputFocus's PointerRoot sentinel: the focus
// follows the pointer, so it names no window.
const x11FocusPointerRoot = 1

// topLevelWindow walks from w up the QueryTree until it reaches a child of the
// root window (the frame a window manager manages) and returns it. A window
// tree deeper than x11WindowWalkMaxDepth stops the walk and returns the
// deepest window reached, which is still a true ancestor of the requested one.
func (c *x11Client) topLevelWindow(w uint32) (uint32, error) {
	current := w
	for depth := 0; depth < x11WindowWalkMaxDepth; depth++ {
		root, parent, err := c.queryTree(current)
		if err != nil {
			return 0, err
		}
		if root != c.root {
			return 0, fmt.Errorf("window 0x%x belongs to root 0x%x, not this display's root 0x%x", w, root, c.root)
		}
		if parent == 0 || parent == c.root {
			return current, nil
		}
		current = parent
	}
	return current, nil
}

// queryTree returns a window's root, parent and children (QueryTree, opcode
// 15). Unlike GetGeometry the reply's |length| field is the CHILD COUNT, not a
// byte length, so the reply carries the children inline in the payload.
func (c *x11Client) queryTree(window uint32) (root, parent uint32, err error) {
	// The request/parse lives in queryTreeReply so this (parent-oriented) and
	// queryTreeChildren (children-oriented) can never drift into two decodes of
	// one reply — the child COUNT vs byte LENGTH trap is exactly where they
	// would diverge.
	header, _, err := c.queryTreeReply(window)
	if err != nil {
		return 0, 0, err
	}
	return binary.NativeEndian.Uint32(header[8:12]), binary.NativeEndian.Uint32(header[12:16]), nil
}

// queryTreeReply issues QueryTree (opcode 15) and returns the reply header and
// payload, so both the parent and the children readers parse ONE reply shape.
func (c *x11Client) queryTreeReply(window uint32) ([]byte, []byte, error) {
	req := make([]byte, x11QueryTreeRequestLen*4)
	req[0] = x11OpQueryTree
	binary.NativeEndian.PutUint16(req[2:], x11QueryTreeRequestLen)
	binary.NativeEndian.PutUint32(req[4:], window)
	if err := c.request(req); err != nil {
		return nil, nil, err
	}
	return c.readReply("QueryTree")
}

// windowProperty reads a 32-bit property (an atom/window id) off a window.
// A property the server does not have comes back as type None with a zero
// value, which this returns as 0 (the caller decides whether that is fatal).
func (c *x11Client) windowProperty(window, property uint32) (uint32, error) {
	req := make([]byte, x11GetPropertyRequestLen*4)
	req[0] = x11OpGetProperty
	req[1] = 0 // do not delete the property
	binary.NativeEndian.PutUint16(req[2:], x11GetPropertyRequestLen)
	binary.NativeEndian.PutUint32(req[4:], window)
	binary.NativeEndian.PutUint32(req[8:], property)
	binary.NativeEndian.PutUint32(req[12:], x11PropertyTypeAny)
	binary.NativeEndian.PutUint32(req[16:], 0) // long-offset
	binary.NativeEndian.PutUint32(req[20:], 1) // long-length: one 4-byte item
	if err := c.request(req); err != nil {
		return 0, err
	}
	header, payload, err := c.readReply("GetProperty")
	if err != nil {
		return 0, err
	}
	if binary.NativeEndian.Uint32(header[8:12]) == 0 { // type None: absent
		return 0, nil
	}
	if int(header[1]) != x11FormatBits32 {
		return 0, fmt.Errorf("GetProperty returned format %d, want %d (a window id is a 32-bit value)", header[1], x11FormatBits32)
	}
	items := binary.NativeEndian.Uint32(header[16:20])
	if items == 0 {
		return 0, nil
	}
	if len(payload) < 4 {
		return 0, fmt.Errorf("GetProperty returned %d bytes of payload for %d items", len(payload), items)
	}
	return binary.NativeEndian.Uint32(payload[0:4]), nil
}

// windowRect measures a window's CONTENT rectangle in ROOT coordinates: size
// from GetGeometry, origin from TranslateCoordinates(window → root).
//
// TranslateCoordinates is used rather than the window's own GetGeometry x/y
// because GetGeometry reports coordinates relative to the window's PARENT, and
// a reparenting window manager makes the parent a frame (so the raw x/y is the
// decoration offset, not the screen position).
func (c *x11Client) windowRect(window uint32) (Rect, error) {
	w, h, err := c.windowSize(window)
	if err != nil {
		return Rect{}, fmt.Errorf("window 0x%x: %w", window, err)
	}
	// The origin is the point (0,0) IN THE WINDOW'S OWN coordinate space,
	// translated into root coordinates. It is deliberately NOT the x/y that
	// GetGeometry reports: those are relative to the window's PARENT, and
	// passing them here computes parent-origin + parent-relative-offset —
	// measured 2026-10-03 on xfwm4, that reported a window at +50+88 whose
	// true position is +45+59 (the frame's own offset, added twice).
	ax, ay, err := c.translateToRoot(window, 0, 0)
	if err != nil {
		return Rect{}, err
	}
	return Rect{X: ax, Y: ay, W: w, H: h}, nil
}

// x11WindowGeometry is a GetGeometry reply's own fields, with the origin NAMED
// as parent-relative so no caller can mistake it for a root coordinate.
type x11WindowGeometry struct {
	// X/Y are relative to the window's PARENT (GetGeometry's contract).
	X, Y int
	// W/H are the window's content size in pixels.
	W, H int
	// Border is the window's border width.
	Border int
}

// windowSize issues GetGeometry (opcode 14) and returns the drawable's width
// and height in pixels. It deliberately returns no x/y: only the width/height
// are meaningful in ROOT terms, and a parent-relative coordinate that looks
// usable is the trap this code already fell into once (see windowRect).
func (c *x11Client) windowSize(window uint32) (w, h int, err error) {
	geom, err := c.windowGeometry(window)
	if err != nil {
		return 0, 0, err
	}
	return geom.W, geom.H, nil
}

// windowGeometry issues GetGeometry and returns every field of its reply. The
// parent-relative origin is handed back only from here, where it is named as
// such, because the same numbers fed to TranslateCoordinates against the ROOT
// are wrong for any window that is not a direct child of the root (see
// windowRect's comment for the measured incident).
func (c *x11Client) windowGeometry(window uint32) (x11WindowGeometry, error) {
	req := make([]byte, x11GetGeometryRequestLen*4)
	req[0] = x11OpGetGeometry
	binary.NativeEndian.PutUint16(req[2:], x11GetGeometryRequestLen)
	binary.NativeEndian.PutUint32(req[4:], window)
	if err := c.request(req); err != nil {
		return x11WindowGeometry{}, err
	}
	header, _, err := c.readReply("GetGeometry")
	if err != nil {
		return x11WindowGeometry{}, err
	}
	// xGetGeometryReply byte map (X11/Xproto.h xGetGeometryReply, and verified
	// byte-for-byte against a live Xvfb reply):
	//   0 type(1) | 1 depth(1) | 2 sequence(2) | 4 length(4) | 8 root(4)
	//  12 x(INT16, parent-relative) | 14 y(INT16, parent-relative)
	//  16 width(2) | 18 height(2) | 20 borderWidth(2)
	return x11WindowGeometry{
		X:      int(int16(binary.NativeEndian.Uint16(header[12:14]))),
		Y:      int(int16(binary.NativeEndian.Uint16(header[14:16]))),
		W:      int(binary.NativeEndian.Uint16(header[16:18])),
		H:      int(binary.NativeEndian.Uint16(header[18:20])),
		Border: int(binary.NativeEndian.Uint16(header[20:22])),
	}, nil
}

// translateToRoot converts a parent-relative point of `window` into root
// coordinates (TranslateCoordinates, opcode 40). A server answering
// same-screen = false means the window is on a different screen of the
// display: the conversion is impossible and is refused rather than guessed.
func (c *x11Client) translateToRoot(window uint32, x, y int) (int, int, error) {
	req := make([]byte, x11TranslateCoordsRequestLen*4)
	req[0] = x11OpTranslateCoords
	binary.NativeEndian.PutUint16(req[2:], x11TranslateCoordsRequestLen)
	binary.NativeEndian.PutUint32(req[4:], window)
	binary.NativeEndian.PutUint32(req[8:], c.root)
	binary.NativeEndian.PutUint16(req[12:], uint16(x))
	binary.NativeEndian.PutUint16(req[14:], uint16(y))
	if err := c.request(req); err != nil {
		return 0, 0, err
	}
	header, _, err := c.readReply("TranslateCoordinates")
	if err != nil {
		return 0, 0, err
	}
	if header[1] == 0 {
		return 0, 0, fmt.Errorf("window 0x%x is not on the same screen as the root window — the rectangle cannot be expressed in root coordinates", window)
	}
	// xTranslateCoordsReply: type@0, sameScreen@1, seq@2, length@4, child@8,
	// dstX@12, dstY@14 (Xproto.h). dstX/dstY are the requested point converted
	// into the destination drawable, and they are SIGNED.
	return int(int16(binary.NativeEndian.Uint16(header[12:14]))),
		int(int16(binary.NativeEndian.Uint16(header[14:16]))),
		nil
}
