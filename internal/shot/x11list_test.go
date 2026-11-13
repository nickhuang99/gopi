// x11list_test.go — the read-only listing, driven over the package's fake X
// server so the test asserts on WIRE FACTS (which opcodes were sent, how many
// connections were dialled, what the JSON says) rather than on a mock's API.
package shot

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

const (
	lwAtomWMName    = 0x00000e01 // _NET_WM_NAME, interned by the listing
	lwAtomWMCheck   = 0x00000e02 // _NET_SUPPORTING_WM_CHECK, interned by the listing
	lwAtomActive    = 0x00000e03 // _NET_ACTIVE_WINDOW (an atom the BASE fake already knows)
	lwAtomPID       = 0x00000e04 // _NET_WM_PID, interned by the listing
	lwPropTypeAtom  = 0x00000d01 // any non-zero type atom: "the property EXISTS"
	lwWinOne        = uint32(0x00020001)
	lwWinTwo        = uint32(0x00020002)
	lwWinFocus      = uint32(0x00020003)
	lwWinOneX       = 37
	lwWinOneY       = 53
	lwWinOneW       = 244
	lwWinOneH       = 160
	lwWinTwoX       = 5
	lwWinTwoY       = 400
	lwWinTwoW       = 300
	lwWinTwoH       = 50
	lwWinOnePID     = uint32(31337)
	lwWinOneTitle   = "fixture-window-one"
	lwFocusWinTitle = "fixture-focus"
)

// lwFakeFixture answers the requests a listing emits. It is installed on
// covFakeXServer (the package's fake server), so the listing talks the REAL
// dial path — the handshake, the request framing, the reply framing — with no
// Xvfb and no display.
type lwFakeFixture struct {
	// roots are the root window's top-level children, in the order the server
	// reports them (deliberately NOT the order the listing prints: the stable
	// sort is one of the properties under test).
	roots []uint32
	// geom holds every window's GetGeometry fields.
	geom map[uint32]pdGeometry
	// children maps a window to its QueryTree children.
	children map[uint32][]uint32
	// parent maps a window to its QueryTree parent.
	parent map[uint32]uint32
	// strProps maps window → property atom → the string it carries.
	strProps map[uint32]map[uint32]string
	// intProps maps window → property atom → the 32-bit value it carries.
	intProps map[uint32]map[uint32]uint32
	// wmCheck answers _NET_SUPPORTING_WM_CHECK on the root ("" = no WM).
	wmCheck bool
	// border is the border width every window in the fixture carries — 1, the
	// value an xterm has, which is what makes the parent-origin correction
	// observable (a fixture with 0 would let a wrong convention pass).
	border int
	// parentOf maps a window to its QueryTree parent (default: the root).
	parentOf map[uint32]uint32
}

// parentOriginFor returns the origin of the parent of `window` in root
// coordinates. The fixture's windows hang off the root, so it is 0,0 — the
// function exists so the correction is expressed, not short-circuited.
func (fx *lwFakeFixture) parentOriginFor(window uint32) (int, int) {
	p, ok := fx.parentOf[window]
	if !ok || p == covFakeRoot {
		return 0, 0
	}
	g := fx.geom[p]
	return g.x, g.y
}

func (fx *lwFakeFixture) reply(req []byte, seq uint16) ([]byte, bool) {
	switch req[0] {
	case x11OpInternAtom:
		return lwInternAtomReply(req, seq), true
	case x11OpQueryTree:
		window := binary.LittleEndian.Uint32(req[4:8])
		parent, ok := fx.parent[window]
		if !ok {
			parent = uint32(covFakeRoot)
		}
		return pdQueryTreeReply(seq, parent, fx.children[window]...), true
	case x11OpGetGeometry:
		// GetGeometry reports the window's own corner RELATIVE TO ITS PARENT and
		// its border width — the parent-relative origin is the trap the listing
		// has to correct (see windowScreenRect), so the fixture serves the
		// parent-relative value here and the absolute one for
		// TranslateCoordinates.
		window := binary.LittleEndian.Uint32(req[4:8])
		g := fx.geom[window]
		px, py := fx.parentOriginFor(window)
		reply := make([]byte, x11ReplyHeaderLen)
		reply[0] = x11ReplyType
		reply[1] = covFakeDepth
		binary.LittleEndian.PutUint16(reply[2:4], seq)
		binary.LittleEndian.PutUint32(reply[8:12], covFakeRoot)
		binary.LittleEndian.PutUint16(reply[12:14], uint16(int16(g.x-px)))
		binary.LittleEndian.PutUint16(reply[14:16], uint16(int16(g.y-py)))
		binary.LittleEndian.PutUint16(reply[16:18], uint16(g.w))
		binary.LittleEndian.PutUint16(reply[18:20], uint16(g.h))
		binary.LittleEndian.PutUint16(reply[20:22], uint16(fx.border))
		return reply, true
	case x11OpTranslateCoords:
		// TranslateCoordinates(window → root) at (0,0). The listing asks for the
		// PARENT's origin and adds the parent-relative GetGeometry value, so the
		// fixture answers the honest root origin for the root itself and the
		// content origin for a real window (a window's bounding box in X11
		// includes its border, so the content origin is the outer corner minus
		// the border).
		window := binary.LittleEndian.Uint32(req[4:8])
		reply := make([]byte, x11ReplyHeaderLen)
		reply[0] = x11ReplyType
		reply[1] = 1 // same-screen
		binary.LittleEndian.PutUint16(reply[2:4], seq)
		if window != covFakeRoot {
			g := fx.geom[window]
			binary.LittleEndian.PutUint16(reply[12:14], uint16(int16(g.x-fx.border)))
			binary.LittleEndian.PutUint16(reply[14:16], uint16(int16(g.y-fx.border)))
		}
		return reply, true
	case x11OpGetProperty:
		return fx.propertyReply(req, seq), true
	case x11OpGetInputFocus:
		reply := make([]byte, x11ReplyHeaderLen)
		reply[0] = x11ReplyType
		binary.LittleEndian.PutUint16(reply[2:4], seq)
		binary.LittleEndian.PutUint32(reply[8:12], lwWinFocus)
		return reply, true
	case x11OpQueryPointer:
		reply := make([]byte, x11ReplyHeaderLen)
		reply[0] = x11ReplyType
		reply[1] = 1 // same-screen
		binary.LittleEndian.PutUint16(reply[2:4], seq)
		return reply, true
	}
	return nil, false
}

// lwInternAtomReply answers InternAtom from a fixed name→id table. Interning is
// answered by NAME, so a reordering of the listing's interning cannot change the
// answer; an unknown name gets id 0, which is exactly "this display has no such
// atom" and is what the listing must read as "no such property".
func lwInternAtomReply(req []byte, seq uint16) []byte {
	nameLen := int(binary.LittleEndian.Uint16(req[4:6]))
	name := ""
	if 8+nameLen <= len(req) {
		name = string(req[8 : 8+nameLen])
	}
	var atom uint32
	switch name {
	case netWMNamePropertyName:
		atom = lwAtomWMName
	case netWMCheckName:
		atom = lwAtomWMCheck
	case netActiveWindowName:
		atom = lwAtomActive
	case netWMNameName:
		atom = lwAtomPID
	}
	reply := make([]byte, x11ReplyHeaderLen)
	reply[0] = x11ReplyType
	binary.LittleEndian.PutUint16(reply[2:4], seq)
	binary.LittleEndian.PutUint32(reply[8:12], atom)
	return reply
}

// propertyReply answers GetProperty for both property formats the listing reads:
// a 32-bit window-id/value property (_NET_SUPPORTING_WM_CHECK,
// _NET_WM_PID, _NET_ACTIVE_WINDOW) and a format-8 string (a title). An absent
// property is answered with type None, which is how a client tells "absent" from
// "empty".
func (fx *lwFakeFixture) propertyReply(req []byte, seq uint16) []byte {
	window := binary.LittleEndian.Uint32(req[4:8])
	property := binary.LittleEndian.Uint32(req[8:12])
	if property == lwAtomWMCheck {
		if !fx.wmCheck {
			return lwAbsentPropertyReply(seq)
		}
		return lwIntPropertyReply(seq, 1)
	}
	if s, ok := fx.strProps[window][property]; ok {
		return lwStringPropertyReply(seq, lwPropTypeAtom, []byte(s))
	}
	if v, ok := fx.intProps[window][property]; ok {
		return lwIntPropertyReply(seq, v)
	}
	return lwAbsentPropertyReply(seq)
}

// lwIntPropertyReply builds a format-32 GetProperty reply carrying one value —
// the shape of _NET_SUPPORTING_WM_CHECK, _NET_WM_PID and _NET_ACTIVE_WINDOW.
func lwIntPropertyReply(seq uint16, value uint32) []byte {
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, value)
	reply := make([]byte, x11ReplyHeaderLen, x11ReplyHeaderLen+4)
	reply[0] = x11ReplyType
	reply[1] = x11FormatBits32
	binary.LittleEndian.PutUint16(reply[2:4], seq)
	binary.LittleEndian.PutUint32(reply[4:8], 1)
	binary.LittleEndian.PutUint32(reply[8:12], lwPropTypeAtom)
	binary.LittleEndian.PutUint32(reply[16:20], 1)
	return append(reply, body...)
}

func lwAbsentPropertyReply(seq uint16) []byte {
	reply := make([]byte, x11ReplyHeaderLen)
	reply[0] = x11ReplyType // type field 0 = None = absent
	binary.LittleEndian.PutUint16(reply[2:4], seq)
	return reply
}

func lwStringPropertyReply(seq uint16, typeAtom uint32, body []byte) []byte {
	// The reply's length field is in 4-BYTE UNITS, and a property's data is
	// padded to a 4-byte boundary — a length that counts bytes desynchronises
	// the client (it would read body/4 times too many bytes and hang).
	nitems := len(body)
	if pad := pad4(len(body)); pad > 0 {
		body = append(body, make([]byte, pad)...)
	}
	reply := make([]byte, x11ReplyHeaderLen, x11ReplyHeaderLen+len(body))
	reply[0] = x11ReplyType
	reply[1] = x11FormatBits8
	binary.LittleEndian.PutUint16(reply[2:4], seq)
	binary.LittleEndian.PutUint32(reply[4:8], uint32(len(body)/4))
	binary.LittleEndian.PutUint32(reply[8:12], typeAtom)
	binary.LittleEndian.PutUint32(reply[16:20], uint32(nitems))
	return append(reply, body...)
}

// lwInstallFixture starts the package's fake X server with a listing fixture
// installed, and a WM answering _NET_SUPPORTING_WM_CHECK.
func lwInstallFixture(t *testing.T) (string, *covFakeXServer, *lwFakeFixture) {
	t.Helper()
	srv := startCovFakeXServer(t, covUsFakeLayout())
	fx := &lwFakeFixture{
		roots:    []uint32{lwWinTwo, lwWinOne}, // bottom-right window FIRST: the sort must reorder
		geom:     map[uint32]pdGeometry{covFakeRoot: {0, 0, covFakeWidth, covFakeHeight}},
		children: map[uint32][]uint32{covFakeRoot: {lwWinTwo, lwWinOne}},
		parent:   map[uint32]uint32{lwWinOne: covFakeRoot, lwWinTwo: covFakeRoot, lwWinFocus: covFakeRoot},
		strProps: map[uint32]map[uint32]string{
			lwWinOne:   {lwAtomWMName: lwWinOneTitle},
			lwWinFocus: {lwAtomWMName: lwFocusWinTitle},
		},
		intProps: map[uint32]map[uint32]uint32{
			lwWinOne:    {lwAtomPID: lwWinOnePID, lwAtomActive: lwWinFocus},
			covFakeRoot: {lwAtomActive: lwWinFocus},
		},
		wmCheck:  true,
		border:   1,
		parentOf: map[uint32]uint32{lwWinOne: covFakeRoot, lwWinTwo: covFakeRoot, lwWinFocus: covFakeRoot},
	}
	fx.geom[lwWinOne] = pdGeometry{lwWinOneX, lwWinOneY, lwWinOneW, lwWinOneH}
	fx.geom[lwWinTwo] = pdGeometry{lwWinTwoX, lwWinTwoY, lwWinTwoW, lwWinTwoH}
	srv.lwFixture = fx
	return srv.display, srv, fx
}

// TestListWindowsReportsEveryWindowFact drives a whole listing end to end over
// the fake display and asserts the payload a caller reads: each top-level
// window's id, root rectangle, pid and title, which one is active, and which
// ROUTE answered the active question.
func TestListWindowsReportsEveryWindowFact(t *testing.T) {
	display, _, _ := lwInstallFixture(t)

	list, err := ListWindows(display)
	if err != nil {
		t.Fatalf("ListWindows(%s): %v", display, err)
	}
	if list.RootW != covFakeWidth || list.RootH != covFakeHeight {
		t.Errorf("root = %dx%d, want %dx%d", list.RootW, list.RootH, covFakeWidth, covFakeHeight)
	}
	if !list.EWMH {
		t.Error("ewmh = false with a WM answering _NET_SUPPORTING_WM_CHECK")
	}
	if list.ActiveID != windowIDString(lwWinFocus) {
		t.Errorf("active = %q, want %q", list.ActiveID, windowIDString(lwWinFocus))
	}
	if list.ActiveRoute != x11RouteNetActiveWindow {
		t.Errorf("active route = %q, want %q — the authoritative answer must be named as such", list.ActiveRoute, x11RouteNetActiveWindow)
	}
	if list.Total != 2 || len(list.Windows) != 2 {
		t.Fatalf("total=%d windows=%d, want 2/2", list.Total, len(list.Windows))
	}
	// STABLE order: top-to-bottom (y=53 before y=400), not the server's own
	// children order (which reported the lower window first).
	if list.Windows[0].ID != windowIDString(lwWinOne) {
		t.Errorf("first listed window = %s, want %s — the listing must sort top-to-bottom, not echo the server's order",
			list.Windows[0].ID, windowIDString(lwWinOne))
	}
	one := list.Windows[0]
	if one.X != lwWinOneX || one.Y != lwWinOneY || one.W != lwWinOneW || one.H != lwWinOneH || !one.Measured {
		t.Errorf("window %s = %+v, want %d,%d %dx%d measured", one.ID, one, lwWinOneX, lwWinOneY, lwWinOneW, lwWinOneH)
	}
	if one.PID != lwWinOnePID {
		t.Errorf("window %s pid = %d, want %d", one.ID, one.PID, lwWinOnePID)
	}
	if one.Name != lwWinOneTitle {
		t.Errorf("window %s title = %q, want %q", one.ID, one.Name, lwWinOneTitle)
	}
	two := list.Windows[1]
	if two.Name != "" || two.PID != 0 {
		t.Errorf("window %s = %+v, want no title and no pid (it publishes neither)", two.ID, two)
	}
}

// TestListWindowsIssuesOnlyReadRequests asserts the READ-ONLY claim from the
// server side: every opcode a whole listing sends is classified, and the only
// ones allowed are the requests a capture already makes. A listing that created,
// mapped, moved or destroyed anything would appear here as an unclassified
// opcode — which is why the assertion is over the recorded request stream and
// not over a comment.
func TestListWindowsIssuesOnlyReadRequests(t *testing.T) {
	display, srv, _ := lwInstallFixture(t)

	if _, err := ListWindows(display); err != nil {
		t.Fatalf("ListWindows(%s): %v", display, err)
	}
	requests := srv.recordedRequests()
	if len(requests) == 0 {
		t.Fatal("the fake server recorded no request — the listing never talked to the display")
	}
	// The read-only vocabulary: the listing may ask, and may never tell.
	readOnly := map[byte]bool{
		x11OpInternAtom:      true, // resolve a property NAME (only-if-exists)
		x11OpQueryTree:       true, // who are the children
		x11OpGetGeometry:     true, // how big
		x11OpTranslateCoords: true, // where, in root coordinates
		x11OpGetProperty:     true, // what does it say
		x11OpGetInputFocus:   true, // the focus fallback
		x11OpQueryPointer:    true, // the pointer fallback
	}
	seen := map[byte]int{}
	for _, req := range requests {
		seen[req[0]]++
		if !readOnly[req[0]] {
			t.Errorf("the listing sent opcode %d, which is not in its read-only vocabulary — a listing must never create, map, move, change or destroy anything on the display", req[0])
		}
	}
	// The positive half: the listing really did enumerate (a listing that sent
	// nothing would pass the classification above for the wrong reason).
	for _, op := range []byte{x11OpQueryTree, x11OpGetGeometry, x11OpGetProperty, x11OpInternAtom} {
		if seen[op] == 0 {
			t.Errorf("the listing never sent opcode %d — the enumeration did not happen", op)
		}
	}
}

// TestListWindowsDialsOnce pins the ONE-connection property: the window set and
// the active-window answer must come from the same moment, so a listing cannot
// report a focus observed before the windows were read. Connection count is
// invisible in any single request's bytes, which is why the server counts them.
func TestListWindowsDialsOnce(t *testing.T) {
	display, srv, _ := lwInstallFixture(t)

	if _, err := ListWindows(display); err != nil {
		t.Fatalf("ListWindows(%s): %v", display, err)
	}
	if got := srv.connections(); got != 1 {
		t.Errorf("the listing dialled the display %d times, want 1 — the windows and the focus must be read from the same moment", got)
	}
}

// TestListWindowsOnADisplayWithNoWM pins the honest FALLBACK: with no
// _NET_SUPPORTING_WM_CHECK, the listing still enumerates every window, reports
// ewmh=false, and names the route that actually answered the active question
// instead of passing a fallback off as the authoritative answer (Rule 15).
func TestListWindowsOnADisplayWithNoWM(t *testing.T) {
	display, _, fx := lwInstallFixture(t)
	fx.wmCheck = false
	delete(fx.intProps[covFakeRoot], lwAtomActive) // no EWMH active window either
	fx.strProps[lwWinFocus][lwAtomWMName] = lwFocusWinTitle

	list, err := ListWindows(display)
	if err != nil {
		t.Fatalf("ListWindows(%s): %v", display, err)
	}
	if list.EWMH {
		t.Error("ewmh = true on a display whose root does not answer _NET_SUPPORTING_WM_CHECK")
	}
	if len(list.Windows) != 2 {
		t.Fatalf("windows = %d, want 2 — a display without a window manager still has windows", len(list.Windows))
	}
	if list.ActiveID != windowIDString(lwWinFocus) {
		t.Errorf("active = %q, want %q — the GetInputFocus fallback must still answer", list.ActiveID, windowIDString(lwWinFocus))
	}
	if list.ActiveRoute != x11RouteInputFocus {
		t.Errorf("active route = %q, want %q — a fallback answer must be labelled as one, never as %q",
			list.ActiveRoute, x11RouteInputFocus, x11RouteNetActiveWindow)
	}
}

// TestListWindowsKeepsAnUnmeasurableWindow pins the completeness property: a
// window whose geometry cannot be read keeps its identity in the listing
// (Measured=false) instead of vanishing from a list whose whole purpose is to
// tell a caller what is there.
func TestListWindowsKeepsAnUnmeasurableWindow(t *testing.T) {
	display, _, fx := lwInstallFixture(t)
	// QueryTree reports three children, but only two have a geometry entry and
	// the third is NOT in the parent map, so its TranslateCoordinates of the
	// parent origin resolves to the root's own origin — the point is that it is
	// still LISTED, with its id and title.
	const ghost = uint32(0x00020009)
	fx.children[covFakeRoot] = []uint32{lwWinOne, ghost}
	fx.strProps[ghost] = map[uint32]string{lwAtomWMName: "ghost-window"}

	list, err := ListWindows(display)
	if err != nil {
		t.Fatalf("ListWindows(%s): %v", display, err)
	}
	if list.Total != 2 || len(list.Windows) != 2 {
		t.Fatalf("total=%d windows=%d, want 2/2", list.Total, len(list.Windows))
	}
	var found bool
	for _, w := range list.Windows {
		if w.ID == windowIDString(ghost) {
			found = true
			if w.Name != "ghost-window" {
				t.Errorf("ghost window title = %q, want %q", w.Name, "ghost-window")
			}
		}
	}
	if !found {
		t.Errorf("a window the server reported is missing from the listing: %+v", list.Windows)
	}
}

// TestListWindowsTruncationIsReported pins Rule 15 on the cap: when the display
// reports more top-level windows than the listing will print, the payload says
// so — a silently shortened list reads exactly like a complete one.
func TestListWindowsTruncationIsReported(t *testing.T) {
	display, _, fx := lwInstallFixture(t)
	children := make([]uint32, 0, x11ListWindowsCap+1)
	for i := 0; i <= x11ListWindowsCap; i++ {
		w := uint32(0x00030000 + i)
		children = append(children, w)
		fx.parent[w] = covFakeRoot
		fx.geom[w] = pdGeometry{i, i, 10, 10}
	}
	fx.children[covFakeRoot] = children

	list, err := ListWindows(display)
	if err != nil {
		t.Fatalf("ListWindows(%s): %v", display, err)
	}
	if !list.Truncated {
		t.Errorf("truncated = false with %d windows reported (cap %d)", list.Total, x11ListWindowsCap)
	}
	if list.Total != x11ListWindowsCap+1 {
		t.Errorf("total = %d, want %d — the report must name how many there really are", list.Total, x11ListWindowsCap+1)
	}
	if len(list.Windows) != x11ListWindowsCap {
		t.Errorf("windows = %d, want the cap %d", len(list.Windows), x11ListWindowsCap)
	}
}

// TestListWindowsRefusesWithNoDisplay pins the failure direction: no display to
// talk to is an error, never an empty listing (an empty listing reads as "this
// display has no windows", which is a different and false statement).
func TestListWindowsRefusesWithNoDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	list, err := ListWindows("")
	if err == nil {
		t.Fatalf("ListWindows with no display returned %+v and no error", list)
	}
	if len(list.Windows) != 0 {
		t.Errorf("a failed listing carried %d windows", len(list.Windows))
	}
}

// TestWindowIDSpellingIsTheOneSpelling pins the single id vocabulary: the
// listing's `id` is byte-identical to the spelling every other surface prints
// (x11WindowIDFormat), so an id copied out of a listing is an id the
// `--window 0x…` / window_label path can take back.
func TestWindowIDSpellingIsTheOneSpelling(t *testing.T) {
	display, _, _ := lwInstallFixture(t)
	list, err := ListWindows(display)
	if err != nil {
		t.Fatalf("ListWindows(%s): %v", display, err)
	}
	for _, w := range list.Windows {
		if !strings.HasPrefix(w.ID, "0x") {
			t.Errorf("window id %q is not in the shared hex spelling (%s)", w.ID, x11WindowIDFormat)
		}
	}
	if got := windowIDString(lwWinOne); got != "0x20001" {
		t.Errorf("windowIDString(0x20001) = %q, want %q", got, "0x20001")
	}
}

// TestSortWindowInfosIsStable pins the order determinism directly: two listings
// of an unchanged display are byte-identical, which is what makes "did that
// window move?" answerable by diffing them.
func TestSortWindowInfosIsStable(t *testing.T) {
	in := []XWindowInfo{
		{ID: "0xb", X: 10, Y: 10},
		{ID: "0xa", X: 10, Y: 10},
		{ID: "0xc", X: 5, Y: 10},
		{ID: "0xd", X: 0, Y: 0},
	}
	sortWindowInfos(in)
	want := []string{"0xd", "0xc", "0xa", "0xb"}
	for i, id := range want {
		if in[i].ID != id {
			t.Fatalf("order = %v, want %v (top-to-bottom, then left-to-right, then by id)", in, want)
		}
	}
}

// TestListWindowsProviderIsTheSeam pins the drift guard: the entry point goes
// through the ONE variable a test may replace, so a future "small local
// shortcut" that enumerates windows itself turns this red instead of splitting
// the engine in two.
func TestListWindowsProviderIsTheSeam(t *testing.T) {
	want := XWindowList{Display: ":seam", RootW: 1, RootH: 2, Total: 3}
	old := ListWindowsProvider
	ListWindowsProvider = func(display string) (XWindowList, error) {
		if display != ":seam-asked" {
			t.Errorf("the seam was asked for %q, want %q", display, ":seam-asked")
		}
		return want, nil
	}
	t.Cleanup(func() { ListWindowsProvider = old })

	got, err := ListWindows(":seam-asked")
	if err != nil {
		t.Fatalf("ListWindows through the seam: %v", err)
	}
	if got.Display != want.Display || got.Total != want.Total {
		t.Errorf("ListWindows bypassed the seam: got %+v, want %+v", got, want)
	}
}

// TestListWindowsJSONPayloadNamesEveryFact pins the wire contract a model reads:
// the JSON keys the tools layer promises, including `total` beside `windows`
// (a truncated list must be visible) and `active_route` beside `active_window`
// (a fallback must be visible as one).
func TestListWindowsJSONPayloadNamesEveryFact(t *testing.T) {
	display, _, _ := lwInstallFixture(t)
	list, err := ListWindows(display)
	if err != nil {
		t.Fatalf("ListWindows(%s): %v", display, err)
	}
	blob, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"display"`, `"root_width"`, `"root_height"`, `"windows"`, `"total"`, `"truncated"`, `"ewmh"`, `"active_window"`, `"active_route"`, `"id"`, `"measured"`} {
		if !strings.Contains(string(blob), key) {
			t.Errorf("the listing JSON does not carry %s: %s", key, blob)
		}
	}
}
