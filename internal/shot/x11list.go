// x11list.go — `capture --list-windows`: what is ON this display, so a caller
// can name one instead of guessing.
//
// WHY IT EXISTS: `--window` answers "which window is ACTIVE", which is whatever
// the user last clicked — a capture taken from a session whose terminal is not
// focused photographs a neighbouring program, and `--self` refuses on a display
// it cannot locate this process on (an X-forwarded display, a container
// boundary). The honest next step after such a refusal is not a guess but a
// LIST — this display's top-level windows with their ids, rectangles, pids and
// titles. The caller reads it, picks the id, and asks for it explicitly with
// `--area WxH+X+Y` (the id's rectangle) or `--window` after focusing it.
//
// It is READ-ONLY, and that is a property of the wire, not a promise: the
// listing issues the SAME core requests a capture already makes — QueryTree,
// GetGeometry, TranslateCoordinates, GetProperty, InternAtom(only-if-exists) —
// plus the focus fallbacks a capture already makes (GetInputFocus, and
// QueryPointer only when the focus is not a window). No extension is queried,
// no external binary is spawned, and NOTHING is written to the display: there is
// no opcode in this file that creates, maps, moves, changes or destroys
// anything. TestListWindowsIssuesOnlyReadRequests asserts that from the server
// side (the whole request stream of a listing is recorded and classified).
//
// It is also ONE dial: the windows AND the active-window answer come off a
// single connection (see ListWindows), so a listing cannot report a window set
// and a focus that were read from two different moments.
package shot

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// The atoms and bounds a listing reads with.
const (
	// netWMNamePropertyName is the EWMH UTF-8 title a modern client sets. It is
	// the counterpart of the (possibly Latin-1) ICCCM WM_NAME and is interned
	// with onlyIfExists, so a display without EWMH answers "no atom" instead of
	// growing one.
	netWMNamePropertyName = "_NET_WM_NAME"

	// netWMCheckName is the root-window property that tells a client an EWMH
	// window manager is running. It is read to report the honest route of the
	// active-window answer: without it, "active" is the pointer fallback and
	// must be labelled as such rather than passed off as the authoritative
	// answer (Rule 15). It is a READ — the listing never sets it.
	netWMCheckName = "_NET_SUPPORTING_WM_CHECK"

	// x11AtomWMNameID is the ICCCM WM_NAME property (atom 39, one of the
	// PRE-DEFINED atoms: it needs no InternAtom round trip).
	x11AtomWMNameID = 39

	// x11NameMaxBytes bounds how much of a window title is read. A title is a
	// LABEL: an unbounded read of a hostile property is not a listing, it is
	// whatever a client decided to publish.
	x11NameMaxBytes = 256

	// x11ListWindowsCap bounds how many top-level windows a listing reports. A
	// display with more than this is answered from the first ones rather than
	// with an unbounded reply — and the cap is REPORTED (see
	// XWindowList.Truncated), because a silently shortened list reads exactly
	// like a complete one.
	x11ListWindowsCap = 512

	// x11GetPropertyNameMaxItems is the long-length of the title read, in
	// 4-byte items: one item past x11NameMaxBytes/4, which is how the request
	// asks the server for "up to 256 bytes or the whole thing, whichever ends
	// first" without a second round trip just to learn the size.
	x11GetPropertyNameMaxItems = x11NameMaxBytes/4 + 1
)

// XWindowInfo is one top-level window of a display, as reported by
// ListWindows.
type XWindowInfo struct {
	// ID is the window id in the X11 hex spelling (0x…), which is exactly what
	// a caller pastes back — X11 ids are hex everywhere a human meets them.
	ID string `json:"id"`
	// X/Y/W/H are the window's rectangle in ROOT (screen) coordinates: size
	// from GetGeometry, origin from TranslateCoordinates. A window whose
	// rectangle could not be measured keeps its identity and reports a zero
	// rectangle (Measured=false) rather than being dropped.
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"width"`
	H int `json:"height"`
	// PID is the _NET_WM_PID the client publishes, 0 when it publishes none
	// (a window-manager frame, or any client that does not set it).
	PID uint32 `json:"pid,omitempty"`
	// Name is the window's title: the EWMH UTF-8 _NET_WM_NAME when present,
	// else the ICCCM WM_NAME. Empty when the window publishes neither.
	Name string `json:"name,omitempty"`
	// Active marks the window the display reports as focused/current.
	Active bool `json:"active,omitempty"`
	// Measured says whether X/Y/W/H came from the server. False means the
	// rectangle is absent — the window is listed as present but unmeasurable.
	Measured bool `json:"measured"`
}

// XWindowList is one listing of a display: the windows, which one is active and
// HOW that was decided, and whether the list was cut short.
type XWindowList struct {
	// Display is the display that was actually reached, with the "@route"
	// suffix when it answered through a fallback (see x11Client.connectedDisplay)
	// — an echo of the requested spelling must never be mistaken for the route
	// that answered.
	Display string `json:"display"`
	// RootW/RootH are the root window's size, so a caller can tell whether a
	// window lies inside the screen and can spell a whole-screen --area.
	RootW int `json:"root_width"`
	RootH int `json:"root_height"`
	// Windows are the top-level windows in STABLE order (see ListWindows).
	Windows []XWindowInfo `json:"windows"`
	// ActiveID is the active window's id ("" when the display has none), and
	// ActiveRoute names the mechanism that answered — so the fallback
	// (QueryPointer, or GetInputFocus on a WM-less display) is visible.
	ActiveID    string `json:"active_window,omitempty"`
	ActiveRoute string `json:"active_route,omitempty"`
	// EWMH says whether a window manager answered _NET_SUPPORTING_WM_CHECK.
	// False means "there is no active window in the EWMH sense here", which is
	// why ActiveRoute fell back.
	EWMH bool `json:"ewmh"`
	// Total is how many top-level windows the display reported, and Truncated
	// says Windows was cut to x11ListWindowsCap. Both are reported because a
	// shortened list is otherwise indistinguishable from a complete one.
	Total     int  `json:"total"`
	Truncated bool `json:"truncated"`
}

// ListWindows enumerates a display's top-level windows together with the
// active-window answer, over ONE connection.
//
// It goes through ListWindowsProvider — the seam at the X11 boundary — so a
// whole read-only listing (the CLI verb, the tools-layer payload) can be driven
// with no display at all (PROJECT.md Rule 31: a surface driven only up to its
// parser has not been driven).
func ListWindows(display string) (XWindowList, error) {
	return ListWindowsProvider(display)
}

// ListWindowsProvider is THE seam at the X11 boundary for the read-only
// listing, the sibling of CaptureX11Provider: the function that actually talks
// to a display server, and therefore the one place a test replaces to drive a
// whole listing with no display, no X socket and no Xvfb.
//
// It is exported for the same reason CaptureX11Provider is: the consumer that
// must be able to substitute it lives in another package (internal/tools,
// cmd/agent). Production code NEVER assigns it; every assignment is a test's,
// and every test restores it.
//
// The seam sits HERE, on the entry point, rather than on each request helper, so
// that a bypass is a single, testable question: does the listing path reach this
// variable? A future "small local shortcut" that enumerates windows itself turns
// the drift guard red instead of silently splitting the engine in two.
var ListWindowsProvider = listWindowsX11

// listWindowsX11 is the real listing behind ListWindowsProvider. It is reached
// only through that variable, so no code path can quietly skip the seam.
//
// display == "" means $DISPLAY. Every window reports the facts a caller needs to
// choose one: the id, the rectangle in root coordinates, the _NET_WM_PID when
// the client publishes one, and the title.
func listWindowsX11(display string) (XWindowList, error) {
	var list XWindowList
	name := captureDisplayName(display)
	if name == "" {
		return list, x11ErrNoDisplay()
	}
	c, err := dialX11(name)
	if err != nil {
		return list, err
	}
	defer c.close()
	list.Display = c.connectedDisplay
	rootRect, err := c.windowRect(c.root)
	if err != nil {
		return list, fmt.Errorf("measuring the root window of %s: %w", list.Display, err)
	}
	list.RootW, list.RootH = rootRect.W, rootRect.H
	list.EWMH = c.ewmhManager()

	activeID, activeRoute := c.activeWindowBestEffort()
	list.ActiveID, list.ActiveRoute = activeID, activeRoute

	children, err := c.queryTreeChildren(c.root)
	if err != nil {
		return list, fmt.Errorf("listing the windows of %s: %w", list.Display, err)
	}
	list.Total = len(children)
	// The two property atoms a listing needs are interned ONCE for the whole
	// listing: asking per window would multiply a read-only enumeration's round
	// trips by the window count for answers that cannot change mid-listing.
	pidAtom := c.internAtomBestEffort(netWMNameName)
	titleAtom := c.internAtomBestEffort(netWMNamePropertyName)

	if len(children) > x11ListWindowsCap {
		children = children[:x11ListWindowsCap]
		list.Truncated = true
	}
	list.Windows = make([]XWindowInfo, 0, len(children))
	for _, w := range children {
		info := XWindowInfo{
			ID:     windowIDString(w),
			Name:   c.windowTitle(w, titleAtom),
			Active: activeID != "" && windowIDString(w) == activeID,
		}
		if pid, err := c.windowPIDForAtom(w, pidAtom); err == nil {
			info.PID = pid
		}
		// An unmeasurable window is still INFORMATION (it is there): it keeps
		// its id and title with Measured=false rather than vanishing from a
		// listing whose whole purpose is to be complete.
		if r, err := c.windowScreenRect(w); err == nil {
			info.X, info.Y, info.W, info.H = r.X, r.Y, r.W, r.H
			info.Measured = true
		}
		list.Windows = append(list.Windows, info)
	}
	sortWindowInfos(list.Windows)
	return list, nil
}

// internAtomBestEffort resolves a property NAME for a READ, where "this display
// does not have that atom" is an ANSWER rather than a failure: an atom that was
// never interned cannot have been set on any window, so the property the caller
// is about to read necessarily has nothing in it. The error is converted here,
// once, with that reasoning — which is why this is a named function and not a
// discarded return at each of the two call sites (a bare `_, _ =` would hide a
// genuinely broken connection behind the same 0).
func (c *x11Client) internAtomBestEffort(name string) uint32 {
	atom, err := c.internAtom(name, true)
	if err != nil {
		return 0
	}
	return atom
}

// sortWindowInfos puts a listing in a STABLE order: top to bottom, left to
// right, then by id. Two listings of an unchanged display are then identical byte
// for byte (a listing that reorders itself cannot be diffed, and "did that
// window move?" becomes unanswerable).
func sortWindowInfos(windows []XWindowInfo) {
	sort.SliceStable(windows, func(i, j int) bool {
		a, b := windows[i], windows[j]
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		if a.X != b.X {
			return a.X < b.X
		}
		return a.ID < b.ID
	})
}

// windowScreenRect measures a top-level window's OUTER rectangle in root
// coordinates, in the convention every X11 tool reports: GetGeometry gives the
// window's own upper-left corner relative to its PARENT, and its content size;
// that box translated to the root is the rectangle a human reads out of xwininfo
// or `xdotool getwindowgeometry --shell`.
//
// Measured 2026-10-01 on a bare Xvfb with an xterm (border 1) at -geometry
// +37+53: GetGeometry reports 1,1 484x316 for a window whose parent is the root
// at 0,0, and BOTH xwininfo (`Absolute upper-left 37,53 / Width 484 / Height
// 316`) and `xdotool getwindowgeometry --shell` (X=37 Y=53 WIDTH=484 HEIGHT=316)
// report the outer corner. The correction is the parent's own origin
// (TranslateCoordinates(parent → root) at (0,0)), which is exact for any window
// that is not the root itself — the same correction the capture path already
// needed once, because GetGeometry's x/y are parent-relative and using them as
// root coordinates silently reports every window at its parent's position.
//
// This is deliberately NOT windowRect: that one answers "where is this window's
// CONTENT" (TranslateCoordinates(window → root) at (0,0), which is the origin a
// capture needs), and for a bordered window the two differ by the border width.
// Recycling windowRect here would have reported a listing rectangle one pixel
// off in each direction — measured against xwininfo on the fixture above before
// this function existed.
func (c *x11Client) windowScreenRect(window uint32) (Rect, error) {
	geom, err := c.windowGeometry(window)
	if err != nil {
		return Rect{}, fmt.Errorf("window %s: %w", windowIDString(window), err)
	}
	root, parent, err := c.queryTree(window)
	if err != nil {
		return Rect{}, fmt.Errorf("window %s: %w", windowIDString(window), err)
	}
	if root != c.root {
		return Rect{}, fmt.Errorf("window %s belongs to root 0x%x, not this display's root 0x%x", windowIDString(window), root, c.root)
	}
	px, py := 0, 0
	if parent != 0 {
		px, py, err = c.translateToRoot(parent, 0, 0)
		if err != nil {
			return Rect{}, fmt.Errorf("window %s: %w", windowIDString(window), err)
		}
	}
	return Rect{X: px + geom.X, Y: py + geom.Y, W: geom.W, H: geom.H}, nil
}

// activeWindowBestEffort answers "which window is the display's current one"
// without failing the listing. A display with no window manager has no EWMH
// active window at all, and that is a fact about the display, not an error in
// the listing: the windows are still enumerable, and the caller is told which
// route answered (or that none did).
func (c *x11Client) activeWindowBestEffort() (string, string) {
	if atom, err := c.internAtom(netActiveWindowName, true); err == nil && atom != 0 {
		if win, err := c.windowProperty(c.root, atom); err == nil && win != 0 && win != c.root {
			return windowIDString(win), x11RouteNetActiveWindow
		}
	}
	if focus, err := c.inputFocus(); err == nil && focus != 0 && focus != x11FocusPointerRoot {
		if top, err := c.topLevelWindow(focus); err == nil {
			return windowIDString(top), x11RouteInputFocus
		}
	}
	if child, err := c.pointerWindow(); err == nil && child != 0 {
		return windowIDString(child), x11RoutePointer
	}
	return "", ""
}

// ewmhManager reports whether a window manager answered the root window's
// _NET_SUPPORTING_WM_CHECK. It is read-only and best effort: an absent property
// is the answer ("no EWMH WM"), not an error.
func (c *x11Client) ewmhManager() bool {
	atom, err := c.internAtom(netWMCheckName, true)
	if err != nil || atom == 0 {
		return false
	}
	// A window id is a 32-bit property value; a zero one names no window, which
	// is precisely "nothing answered".
	win, err := c.windowProperty(c.root, atom)
	return err == nil && win != 0
}

// windowTitle reads a window's title, preferring the EWMH UTF-8 _NET_WM_NAME
// (wmNameAtom, already interned by the caller) and falling back to the ICCCM
// WM_NAME, which is a PRE-DEFINED atom and needs no intern at all. The read is
// bounded (x11NameMaxBytes) and stops at the first NUL, so a padded or hostile
// property becomes a label instead of an unbounded string.
func (c *x11Client) windowTitle(window, wmNameAtom uint32) string {
	if wmNameAtom != 0 {
		if s := c.windowText(window, wmNameAtom); s != "" {
			return s
		}
	}
	return c.windowText(window, x11AtomWMNameID)
}

// windowText reads a format-8 property as a string. A missing property, a
// non-string type and a read error all yield "" — the caller is building a
// LABEL, not making a decision that must not fail silently.
func (c *x11Client) windowText(window, property uint32) string {
	req := make([]byte, x11GetPropertyRequestLen*4)
	req[0] = x11OpGetProperty
	req[1] = 0 // do not delete the property
	binary.NativeEndian.PutUint16(req[2:], x11GetPropertyRequestLen)
	binary.NativeEndian.PutUint32(req[4:], window)
	binary.NativeEndian.PutUint32(req[8:], property)
	binary.NativeEndian.PutUint32(req[12:], x11PropertyTypeAny)
	binary.NativeEndian.PutUint32(req[16:], 0) // long-offset
	binary.NativeEndian.PutUint32(req[20:], x11GetPropertyNameMaxItems)
	if err := c.request(req); err != nil {
		return ""
	}
	header, payload, err := c.readReply("GetProperty")
	if err != nil {
		return ""
	}
	if binary.NativeEndian.Uint32(header[8:12]) == 0 { // type None: absent
		return ""
	}
	if int(header[1]) != x11FormatBits8 {
		return ""
	}
	if len(payload) > x11NameMaxBytes {
		payload = payload[:x11NameMaxBytes]
	}
	s := string(payload)
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
