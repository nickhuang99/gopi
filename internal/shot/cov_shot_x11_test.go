// cov_shot_x11_test.go — function-level coverage for the four exported
// X11/XTEST layer entry points that had no test of their own: Display, Focus,
// Click and TypeText.
//
// Strategy (tests only — no product changes, Rule 4):
//
//	(a) PROTOCOL-LEVEL tests run against a FAKE X SERVER: a real unix socket at
//	    /tmp/.X11-unix/XN (binding there works unprivileged on this host) that
//	    replays a captured connection-setup reply and then RECORDS every
//	    request the client writes. That is what lets us pin the REQUEST BYTE
//	    LAYOUTS (GetInputFocus, XTestFakeInput for button/key) deterministically
//	    — a real X server never shows us the bytes, and the fake can also
//	    inject malformed replies (short GetKeyboardMapping) that no real server
//	    would ever send.
//	(b) LIVE tests run against a display the tests START THEMSELVES (private
//	    Xvfb on a swept display number) so nothing is injected into a display
//	    another agent is using. They skip — VISIBLY, with the reason — when
//	    Xvfb is unavailable.
//
// Rule 1b/13: every opcode/event/word-count below comes from the constants in
// x11.go / xactuate.go, never a bare literal — except the two that live in the
// PRODUCT itself as function-local constants (the GetKeyboardMapping
// first-keycode/count pair) and the X server's own event-type octet, which
// this client has no constant for and which is therefore declared here, once,
// with the reason.
package shot

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/worktreeroots"
)

// ---------------------------------------------------------------------------
// Fixture constants.
// ---------------------------------------------------------------------------

const (
	// covX11ServerEventType is the X protocol's event-type octet that a
	// connection-setup FAILED reply carries (X11 protocol: the setup reply's
	// first byte is 0 for Failed, 1 for Success). This client has no constant
	// for it — the successful-reply literal lives inline in handshake() as
	// `head[0] != 1` — so it is declared here rather than a bare literal.
	covX11ServerEventType = 0

	// covX11EventTypeMappingNotify is the X11 event code for MappingNotify
	// (X11 protocol, event codes 0-35). It is the type that a pending keyboard
	// mapping change puts on the wire, and the type the read-reply frame
	// model misreads as a reply (see TestLiveDisplayFocusClickTypeObservable).
	// The err is matched by NUMBER so the assertion cannot go stale if the
	// message wording changes.
	covX11EventTypeMappingNotify = 34

	// covKeysymLowerN / covKeysymUpperN are the keysyms TypeText('n') and
	// TypeText('N') resolve to (X11/keysymdef.h: XK_n = 0x006e, XK_N = 0x004e)
	// — the fake server's mapping must advertise exactly these, since keysym
	// resolution happens BEFORE the server mapping is consulted.
	covKeysymLowerN = 0x006e
	covKeysymUpperN = 0x004e

	// covKeysymLowerZ is XK_z (0x007a): mapped by this client, but absent from
	// the fake server's keyboard mapping — the "keysym not in the server's
	// mapping" error path.
	covKeysymLowerZ = 0x007a

	// covKeycodeLowerN and covKeycodeUpperN are the evdev keycodes the fake
	// layout advertises for XK_n and XK_N. They are deliberately DIFFERENT
	// numbers so the TypeText assertions can prove the client resolved each
	// rune through the server's own mapping.
	covKeycodeLowerN = 57
	covKeycodeUpperN = 52

	// covShiftKeycode is the keycode the fake layout uses for XK_Shift_L, and
	// covKeycodeReturn the one it uses for XK_Return (both evdev values).
	covShiftKeycode  = 50
	covKeycodeReturn = 36

	// covKeymapPerCode is the keysyms-per-keycode value the fake
	// GetKeyboardMapping reply advertises (the real Xvfb reply says 7).
	covKeymapPerCode = 7

	// covKeymapMappingRequestWords is the length the GetKeyboardMapping
	// request must carry: 2 (8 bytes / 4). xactuate.go uses the package
	// constant x11GetKeyboardMappingLen for this and firstKeycode/count as
	// block-local constants; the layout fields are pinned here.
	covKeymapFirstKeycode = 8
	covKeymapKeycodeCount = 248

	// covFakeLayoutKeysyms is the number of keysyms the fake fakeKeyboardMap
	// definition writes for each keycode (the columns of one layout row).
	covFakeLayoutKeysyms = 4

	// covSetupVendor is the vendor string of the synthetic connection-setup
	// reply. Its 20-byte length is what the fake body's field map is built
	// around (mirrors "The X.Org Foundation" on a real server).
	covSetupVendor = "The X.Org Foundation"

	// covSetupScreens is the number of SCREENS the synthetic setup reply
	// advertises; covSetupFormats the number of pixmap FORMATS that follow the
	// vendor string before the screen table.
	covSetupScreens = 1
	covSetupFormats = 6

	// covFakeDisplayMin/Max bound the display numbers the FAKE X server may
	// claim. The range is disjoint from covXvfbDisplayMin/Max so a fake server
	// and a private Xvfb started by the same binary can never collide.
	covFakeDisplayMin = 240
	covFakeDisplayMax = 259

	// covFakeRoot is the root window id of the synthetic setup reply.
	covFakeRoot = 0x0000050d

	// covFakeWidth / covFakeHeight / covFakeDepth describe the synthetic root
	// window (24bpp, the only depth class this client accepts).
	covFakeWidth  = 640
	covFakeHeight = 480
	covFakeDepth  = 24

	// covRootWindowID is the window id a REAL display reports when it has no
	// focused window at all (X11: the focus is PointerRoot/the implicit root).
	covRootWindowID = 1

	// covXTestMajorOpcode is the extension-major opcode the fake server
	// assigns to XTEST in its QueryExtension reply (a real server picks its
	// own; the CLIENT must echo whatever the server said).
	covXTestMajorOpcode = 0x84

	// covButtonPressDetailMin/Max bound the mouse buttons X11 defines (1..5:
	// left, middle, right, wheel up, wheel down).
	covButtonPressDetailMin = 1
	covButtonPressDetailMax = 5
)

// ---------------------------------------------------------------------------
// Fake X server.
// ---------------------------------------------------------------------------

// covBtnEvent is one recorded XTestFakeInput event (type + detail).
type covBtnEvent struct {
	Type   byte
	Detail byte
}

// covFakeXServer is a minimal X11 server over a real unix socket. It answers
// the connection-setup handshake with a synthetic (byte-exact) reply, then
// serves exactly the three requests the actuation path emits — QueryExtension,
// GetKeyboardMapping and GetInputFocus — and RECORDS every request body so the
// tests can assert on raw wire bytes.
//
// A request this fake does not implement is answered with an X11 ERROR reply
// (type 0) so a stray request fails loudly instead of hanging the test.
type covFakeXServer struct {
	t        *testing.T
	ln       net.Listener
	display  string
	sockPath string

	// layout is the keyboard mapping this fake server advertises, and the
	// reply-injection switches below let a test hand the client a malformed
	// reply no real server would ever send.
	layout       covFakeLayout
	badKeymapLen int  // truncate the GetKeyboardMapping body to this many bytes
	zeroPerCode  bool // advertise 0 keysyms-per-keycode

	mu       sync.Mutex
	requests [][]byte
	events   []covBtnEvent
	// stray holds frames the server writes IMMEDIATELY BEFORE its reply to the
	// next request it answers (the interleaved-event case a clean request/
	// reply fake cannot produce); consumed on first use, see setStrayFrames.
	stray [][]byte
	// focusReplySeq/focusReplySeqSet override the sequence the GetInputFocus
	// reply carries (the off-sequence frame a desynchronised connection
	// serves), and focusReplyWindow overrides the focused window id it reports
	// (a value only the real reply can produce, so asserting on it proves the
	// reply — not an event frame — was parsed).
	focusReplySeq    uint16
	focusReplySeqSet bool
	focusReplyWindow uint32

	// pidFixture, when installed, answers the requests the pid-window path
	// emits (see pidwindow_test.go). A nil fixture means this server serves
	// only the actuation requests above.
	pidFixture *pdFakeXServer

	// lwFixture, when installed, answers the requests a read-only window
	// listing emits (see x11list_test.go) — QueryTree, GetGeometry,
	// TranslateCoordinates, GetProperty, InternAtom, GetInputFocus and
	// QueryPointer.
	lwFixture *lwFakeFixture

	// conns counts the connections this server has accepted. A listing is
	// supposed to make ONE dial (the windows and the active-window answer read
	// from the same moment), and that is only observable here.
	conns int

	shutdown chan struct{}
	wg       sync.WaitGroup
	serveErr error
}

// covFakeBarrier forces the fake server to have DRAINED everything the client
// wrote so far. Reading a reply to a request written after the request under
// test proves the server parsed the earlier bytes (one ordered stream), which
// makes assertions on the server-side recording deterministic instead of a race
// against the serving goroutine. The client's own sync() does this on its happy
// paths; the error paths return before it, so the test supplies the barrier.
func covFakeBarrier(a *XActuator) error {
	_, err := a.Focus()
	return err
}

// covFakeLayout maps a keysym to an optional shift keycode, i.e. the layout
// the fake server advertises. Column 0 is "unshifted", column 1 "shifted".
type covFakeLayout map[uint32]struct {
	code  byte
	shift uint32
}

// covUsFakeLayout is the minimal US layout the fake server reports: lowercase
// n, its shifted uppercase N (a DIFFERENT keycode, which is what proves the
// client honours the server's own mapping rather than assuming shift-same-key)
// and XK_Shift_L for the modifier.
func covUsFakeLayout() covFakeLayout {
	return covFakeLayout{
		covKeysymLowerN: {code: covKeycodeLowerN}, // XK_n — unshifted
		covKeysymUpperN: {code: covKeycodeUpperN}, // XK_N — its own keycode here
		keysymShiftL:    {code: covShiftKeycode},
		keysymReturn:    {code: covKeycodeReturn},
	}
}

// startCovFakeXServer binds a unix socket at /tmp/.X11-unix/XN, writes the
// matching /tmp/.XN-lock (a real X server keeps one; the tests remove both) and
// starts serving. It skips — visibly — when the socket path cannot be bound.
func startCovFakeXServer(t *testing.T, layout covFakeLayout) *covFakeXServer {
	t.Helper()
	if _, err := os.Stat(x11UnixSocketDir); err != nil {
		t.Skipf("no %s directory — cannot host a fake X server: %v", x11UnixSocketDir, err)
	}
	display, num, err := covClaimFakeDisplay()
	if err != nil {
		t.Skipf("cannot find a free display number for the fake X server: %v", err)
	}
	sockPath := filepath.Join(x11UnixSocketDir, x11UnixSocketPrefix+num)
	lockPath := covXLockPath(num)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Skipf("cannot bind %s (no permission to host a fake X server): %v", sockPath, err)
	}
	s := &covFakeXServer{
		t:        t,
		ln:       ln,
		display:  display,
		sockPath: sockPath,
		layout:   layout,
		shutdown: make(chan struct{}),
	}
	t.Cleanup(func() {
		close(s.shutdown)
		s.ln.Close() //stylegate:ignore test cleanup: the fake X server listener is closed after its shutdown channel, which is what unwinds the accept loop; the close error is not actionable
		s.wg.Wait()
		os.Remove(sockPath)
		os.Remove(lockPath)
	})
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%10d\n", os.Getpid())), 0o644); err != nil {
		t.Errorf("write X lock file: %v", err)
	}
	s.wg.Add(1)
	go s.serve()
	return s
}

// covClaimFakeDisplay returns a ":N" whose socket and lock file do not exist,
// together with the bare number. The range is swept so concurrent test
// binaries never fight over one display number.
func covClaimFakeDisplay() (string, string, error) {
	for n := covFakeDisplayMin; n <= covFakeDisplayMax; n++ {
		num := fmt.Sprint(n)
		if _, err := os.Stat(filepath.Join(x11UnixSocketDir, x11UnixSocketPrefix+num)); err == nil {
			continue
		}
		if _, err := os.Stat(covXLockPath(num)); err == nil {
			continue
		}
		return ":" + num, num, nil
	}
	return "", "", fmt.Errorf("no free display number in :%d-:%d", covFakeDisplayMin, covFakeDisplayMax)
}
func (s *covFakeXServer) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.shutdown:
				return
			default:
			}
			s.mu.Lock()
			if s.serveErr == nil {
				s.serveErr = err
			}
			s.mu.Unlock()
			return
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer c.Close()
			s.mu.Lock()
			s.conns++
			s.mu.Unlock()
			s.serveConn(c)
		}(conn)
	}
}

// serveConn performs the handshake, then services requests until the client
// disconnects.
func (s *covFakeXServer) serveConn(c net.Conn) {
	head := make([]byte, x11SetupRequestLen)
	if _, err := readFullConn(c, head); err != nil {
		return
	}
	// The setup request carries the auth-name/auth-data lengths; both are 0 on
	// this host (no Xauthority entry for a display that does not exist), but
	// read whatever the client announced so the handshake cannot desync.
	naLen := int(binary.LittleEndian.Uint16(head[6:8]))
	daLen := int(binary.LittleEndian.Uint16(head[8:10]))
	if pad := pad4(naLen) + naLen + pad4(daLen) + daLen; pad > 0 {
		if _, err := readFullConn(c, make([]byte, pad)); err != nil {
			return
		}
	}
	if _, err := c.Write(s.setupReply()); err != nil {
		return
	}
	// reqSeq numbers the requests of THIS connection the way a real X server
	// does: X11 requests carry no sequence number, the server counts them
	// itself, and every reply echoes the number of the request it answers.
	// It is what makes the sequence-matching assertions in
	// x11replyframing_test.go possible over a socket.
	var reqSeq uint16
	for {
		rh := make([]byte, 4)
		if _, err := readFullConn(c, rh); err != nil {
			return
		}
		words := int(binary.LittleEndian.Uint16(rh[2:4]))
		if words < 1 {
			return
		}
		req := make([]byte, words*4)
		copy(req, rh)
		if _, err := readFullConn(c, req[4:]); err != nil {
			return
		}
		s.record(req)
		reqSeq++
		seq := reqSeq
		s.mu.Lock()
		stray := s.stray
		s.stray = nil
		focusSeq := s.focusReplySeq
		focusSet := s.focusReplySeqSet
		focusWin := s.focusReplyWindow
		s.mu.Unlock()
		reply, ok := s.replyFor(req, seq, covFocusOverride{seq: focusSeq, set: focusSet, window: focusWin})
		// Frames the server queues on its OWN initiative arrive BEFORE the
		// reply to the request being answered — never after it, or the client
		// would only meet them at its next read.
		for _, frame := range stray {
			if _, err := c.Write(frame); err != nil {
				return
			}
		}
		if !ok {
			// X11 error reply: type 0, code, sequence, everything else zero.
			errReply := make([]byte, x11ReplyHeaderLen)
			errReply[0] = x11ErrorType
			errReply[1] = 3 // BadWindow
			binary.LittleEndian.PutUint16(errReply[2:4], seq)
			if _, err := c.Write(errReply); err != nil {
				return
			}
			continue
		}
		if reply != nil {
			if _, err := c.Write(reply); err != nil {
				return
			}
		}
	}
}

func (s *covFakeXServer) record(req []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(req))
	copy(cp, req)
	s.requests = append(s.requests, cp)
	if len(req) > 5 && req[1] == xtestOpFakeInput {
		s.events = append(s.events, covBtnEvent{Type: req[4], Detail: req[5]})
	}
}

// recordedRequests returns every request the server received, in order — the
// whole request stream of a connection, which is what a WIRE-LEVEL property
// (such as "this path only reads") has to be asserted against.
func (s *covFakeXServer) recordedRequests() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.requests))
	copy(out, s.requests)
	return out
}

// connections returns how many connections this server has accepted. One dial
// is a product property (a listing must read the windows and the focus from the
// same moment), and it is invisible in any single request's bytes.
func (s *covFakeXServer) connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns
}

// covFocusOverride overrides fields of the GetInputFocus reply so a test can
// construct the frames a desynchronised connection serves: a reply that
// belongs to another request (seq/set) and a focused window id that only the
// real reply can carry (window).
type covFocusOverride struct {
	seq    uint16
	set    bool
	window uint32
}

// replyFor returns the reply bytes for a request, or ok=false for an
// unimplemented request (answered as an X11 error). seq is the number this
// server assigned the request and every reply echoes it — the client refuses a
// reply that belongs to another request.
func (s *covFakeXServer) replyFor(req []byte, seq uint16, focus covFocusOverride) ([]byte, bool) {
	// XTestFakeInput is a no-reply request (like the real XTEST extension, its
	// only outcome is the injected event). Writing ANYTHING here — including an
	// error — would leave a stale reply in the stream that the next
	// reply-framed read would mistake for its own answer.
	if req[0] == covXTestMajorOpcode && len(req) > 1 && req[1] == xtestOpFakeInput {
		return nil, true
	}
	if s.pidFixture != nil {
		if reply, ok := s.pidFixture.reply(req, seq); ok {
			return reply, true
		}
	}
	if s.lwFixture != nil {
		if reply, ok := s.lwFixture.reply(req, seq); ok {
			return reply, true
		}
	}
	switch req[0] {
	case x11OpQueryExtension:
		return s.queryExtensionReply(req, seq), true
	case x11OpGetKeyboardMapping:
		return s.keyboardMappingReply(seq), true
	case x11OpGetInputFocus:
		if focus.set {
			seq = focus.seq
		}
		win := uint32(covRootWindowID)
		if focus.window != 0 {
			win = focus.window
		}
		reply := make([]byte, x11ReplyHeaderLen)
		reply[0] = x11ReplyType
		binary.LittleEndian.PutUint16(reply[2:4], seq)
		binary.LittleEndian.PutUint32(reply[8:12], win)
		return reply, true
	}
	return nil, false
}

// queryExtensionReply reports XTEST as present with the fake major opcode.
func (s *covFakeXServer) queryExtensionReply(req []byte, seq uint16) []byte {
	nameLen := int(binary.LittleEndian.Uint16(req[4:6]))
	name := string(req[8 : 8+nameLen])
	reply := make([]byte, x11ReplyHeaderLen)
	reply[0] = x11ReplyType
	binary.LittleEndian.PutUint16(reply[2:4], seq)
	if name == xtestOpExtensionName {
		reply[8] = 1 // present
		reply[9] = covXTestMajorOpcode
		reply[10] = 2 // major version
		reply[11] = 2 // minor version
	}
	return reply
}

// keyboardMappingReply returns a keysyms-per-keycode table whose column 0/1
// entries are the layout's keycodes.
func (s *covFakeXServer) keyboardMappingReply(seq uint16) []byte {
	s.mu.Lock()
	layout := s.layout
	perCode := covKeymapPerCode
	if s.zeroPerCode {
		perCode = 0
	}
	s.mu.Unlock()
	body := make([]byte, covKeymapKeycodeCount*perCode*4)
	if !s.zeroPerCode {
		for ks, entry := range layout {
			for i := 0; i < covKeymapKeycodeCount; i++ {
				if byte(covKeymapFirstKeycode+i) != entry.code {
					continue
				}
				base := i * perCode * 4
				binary.LittleEndian.PutUint32(body[base:], ks)
				if entry.shift != 0 {
					binary.LittleEndian.PutUint32(body[base+4:], entry.shift)
				}
			}
		}
		// A few filler keysyms so the table is not trivially empty.
		for i := 0; i < covKeymapKeycodeCount; i++ {
			base := i * perCode * 4
			if binary.LittleEndian.Uint32(body[base:]) == 0 {
				binary.LittleEndian.PutUint32(body[base:], 0x1000+uint32(i))
			}
		}
	}
	if s.badKeymapLen > 0 && s.badKeymapLen < len(body) {
		body = body[:s.badKeymapLen]
	}
	reply := make([]byte, x11ReplyHeaderLen, x11ReplyHeaderLen+len(body))
	reply[0] = x11ReplyType
	reply[1] = byte(perCode) // keysyms-per-keycode
	binary.LittleEndian.PutUint16(reply[2:4], seq)
	binary.LittleEndian.PutUint32(reply[4:8], uint32(len(body)/4))
	return append(reply, body...)
}

// setupReply builds the connection-setup SUCCESS frame. Field offsets are the
// ones parseSetup documents (release .. max-keycode, then pad to 32, vendor,
// formats, screens) — built by length so the fake can never desync.
func (s *covFakeXServer) setupReply() []byte {
	vendor := []byte(covSetupVendor)
	formats := make([]byte, 8*covSetupFormats)
	screen := make([]byte, 40)
	binary.LittleEndian.PutUint32(screen[0:4], covFakeRoot)
	binary.LittleEndian.PutUint16(screen[20:22], covFakeWidth)
	binary.LittleEndian.PutUint16(screen[22:24], covFakeHeight)
	screen[38] = covFakeDepth

	body := make([]byte, 0, 32+pad4(len(vendor))+len(vendor)+len(formats)+len(screen))
	body = append(body, make([]byte, 32)...) // release .. pad
	binary.LittleEndian.PutUint16(body[16:18], uint16(len(vendor)))
	binary.LittleEndian.PutUint16(body[18:20], uint16(65535)) // maximum-request-length
	body[20] = covSetupScreens
	body[21] = covSetupFormats
	body[22] = nativeByteOrder()
	body = append(body, vendor...)
	body = append(body, make([]byte, pad4(len(vendor)))...)
	body = append(body, formats...)
	body = append(body, screen...)

	head := make([]byte, 8)
	head[0] = 1 // Success
	binary.LittleEndian.PutUint16(head[6:8], uint16(len(body)/4))
	return append(head, body...)
}

// requestsOf returns every recorded request whose first byte matches opcode.
func (s *covFakeXServer) requestsOf(opcode byte) [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out [][]byte
	for _, r := range s.requests {
		if len(r) > 0 && r[0] == opcode {
			out = append(out, r)
		}
	}
	return out
}

// fakeInputEvents returns the recorded XTestFakeInput (type, detail) pairs.
func (s *covFakeXServer) fakeInputEvents() []covBtnEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]covBtnEvent, len(s.events))
	copy(out, s.events)
	return out
}

// openActuator dials the fake and opens an actuator against it.
func (s *covFakeXServer) openActuator(t *testing.T) *XActuator {
	t.Helper()
	a, err := OpenXActuator(s.display)
	if err != nil {
		t.Fatalf("OpenXActuator(%s) against the fake X server: %v", s.display, err)
	}
	t.Cleanup(a.Close)
	return a
}

// readFullConn is a local io.ReadFull over a net.Conn (net.Conn satisfies
// io.Reader; this keeps the fake independent of the product's ioReadFull).
func readFullConn(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// ---------------------------------------------------------------------------
// Wire-layout tests (fake server: request bytes are observable).
// ---------------------------------------------------------------------------

// TestCovX11DisplayReportsConnectedName covers Display(): the actuator must
// echo the display it was actually opened with, never a guess.
func TestCovX11DisplayReportsConnectedName(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)
	if got := a.Display(); got != s.display {
		t.Errorf("Display() = %q, want the display the actuator was opened with (%q)", got, s.display)
	}
	// A zero-value actuator must not panic either: it has no client at all.
	if got := (&XActuator{}).Display(); got != "" {
		t.Errorf("Display() on a zero-value actuator = %q, want \"\"", got)
	}
	// A NIL *XActuator is the natural "never opened" sentinel — OpenXActuator
	// returns (nil, err) — so Display must answer "" instead of dereferencing
	// the receiver (the nil-receiver SIGSEGV this guard closes).
	var nilAct *XActuator
	if got := nilAct.Display(); got != "" {
		t.Errorf("Display() on a nil *XActuator = %q, want \"\"", got)
	}
}

// TestCovX11NilActuatorDoesNotPanic pins the nil-receiver contract of every
// XActuator accessor that does not return an error: the nil *XActuator that
// OpenXActuator hands back with its error must behave as "nothing opened" —
// zero values, never a panic (a SIGSEGV in the actuation path is a hard
// failure where the contract promises a benign value).
func TestCovX11NilActuatorDoesNotPanic(t *testing.T) {
	var a *XActuator
	if got := a.Display(); got != "" {
		t.Errorf("nil.Display() = %q, want \"\"", got)
	}
	if w, h := a.RootSize(); w != 0 || h != 0 {
		t.Errorf("nil.RootSize() = %d,%d, want 0,0", w, h)
	}
	a.Close() // must be a no-op, not a crash

	// The same contract holds for a zero-value actuator (no client field).
	z := &XActuator{}
	if w, h := z.RootSize(); w != 0 || h != 0 {
		t.Errorf("zero.RootSize() = %d,%d, want 0,0", w, h)
	}
	z.Close()
}

// TestCovX11FocusRequestBytes pins the GetInputFocus REQUEST layout: opcode 43
// at byte 0, length 1 (4 bytes / 4) at bytes 2..3, and nothing else set. The
// reply's window id is read from bytes 8..11 — the field the fake fills with
// the root window id.
func TestCovX11FocusRequestBytes(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)
	before := len(s.requestsOf(x11OpGetInputFocus))

	win, err := a.Focus()
	if err != nil {
		t.Fatalf("Focus(): %v", err)
	}
	if win != covRootWindowID {
		t.Errorf("Focus() = 0x%x, want 0x%x (bytes 8..11 of the GetInputFocus reply)", win, covRootWindowID)
	}
	reqs := s.requestsOf(x11OpGetInputFocus)
	if len(reqs) != before+1 {
		t.Fatalf("Focus() emitted %d GetInputFocus requests, want exactly 1", len(reqs)-before)
	}
	req := reqs[len(reqs)-1]
	// Byte 2..3 is the 16-bit request length in 4-byte units, little-endian:
	// 1 word == the whole 4-byte request.
	want := []byte{x11OpGetInputFocus, 0, 1, 0}
	if len(req) != len(want) {
		t.Fatalf("GetInputFocus request is %d bytes, want %d (one 4-byte word): % x", len(req), len(want), req)
	}
	for i, b := range want {
		if req[i] != b {
			t.Errorf("GetInputFocus request byte %d = 0x%02x, want 0x%02x (full request % x)", i, req[i], b, req)
		}
	}
}

// TestCovX11ClickRequestBytes pins the two XTestFakeInput requests a Click
// emits: ButtonPress then ButtonRelease, same button detail, no coordinates
// (root/rootX/rootY stay zero for button events), time = CurrentTime(0), and
// the 36-byte length field (xtestFakeInputWords) that a first revision got
// wrong — with a short length the server DROPS the event silently.
func TestCovX11ClickRequestBytes(t *testing.T) {
	for button := covButtonPressDetailMin; button <= covButtonPressDetailMax; button++ {
		t.Run(fmt.Sprintf("button%d", button), func(t *testing.T) {
			s := startCovFakeXServer(t, covUsFakeLayout())
			a := s.openActuator(t)

			if err := a.Click(button); err != nil {
				t.Fatalf("Click(%d): %v", button, err)
			}
			reqs := s.requestsOf(covXTestMajorOpcode)
			if len(reqs) != 2 {
				t.Fatalf("Click(%d) emitted %d XTestFakeInput requests, want 2 (press+release): %v", button, len(reqs), s.fakeInputEvents())
			}
			wantTypes := []byte{xEventButtonPress, xEventButtonRelease}
			for i, req := range reqs {
				if len(req) != xtestFakeInputWords*4 {
					t.Errorf("XTestFakeInput request %d is %d bytes, want %d (sz_xXTestFakeInputReq): % x", i, len(req), xtestFakeInputWords*4, req)
					continue
				}
				if req[1] != xtestOpFakeInput {
					t.Errorf("request %d xtReqType = %d, want %d (X_XTestFakeInput)", i, req[1], xtestOpFakeInput)
				}
				words := binary.LittleEndian.Uint16(req[2:4])
				if int(words) != xtestFakeInputWords {
					t.Errorf("request %d length field = %d words, want %d — a wrong length makes the server drop the event silently", i, words, xtestFakeInputWords)
				}
				if req[4] != wantTypes[i] {
					t.Errorf("request %d event type = %d, want %d (ButtonPress/ButtonRelease)", i, req[4], wantTypes[i])
				}
				if int(req[5]) != button {
					t.Errorf("request %d detail (button) = %d, want %d", i, req[5], button)
				}
				if req[6] != 0 || req[7] != 0 {
					t.Errorf("request %d pad bytes 6..7 = % x, want zero", i, req[6:8])
				}
				if tm := binary.LittleEndian.Uint32(req[8:12]); tm != 0 {
					t.Errorf("request %d time = %d, want 0 (CurrentTime)", i, tm)
				}
				// Button events carry no coordinates: root + rootX/rootY zero.
				if root := binary.LittleEndian.Uint32(req[12:16]); root != 0 {
					t.Errorf("request %d root = 0x%x, want 0 for a button event", i, root)
				}
				if x := binary.LittleEndian.Uint16(req[24:26]); x != 0 {
					t.Errorf("request %d rootX = %d, want 0 for a button event", i, x)
				}
				if y := binary.LittleEndian.Uint16(req[26:28]); y != 0 {
					t.Errorf("request %d rootY = %d, want 0 for a button event", i, y)
				}
				if dev := req[35]; dev != 0 {
					t.Errorf("request %d deviceid = %d, want 0 (the core device)", i, dev)
				}
			}
		})
	}
}

// TestCovX11ClickRejectsOutOfRangeButton covers the Click guard: X11 defines
// buttons 1..5 and anything else must be a loud refusal (Rule 15), with NO
// bytes on the wire.
func TestCovX11ClickRejectsOutOfRangeButton(t *testing.T) {
	for _, button := range []int{-1, 0, covButtonPressDetailMax + 1, 99} {
		t.Run(fmt.Sprintf("button%d", button), func(t *testing.T) {
			s := startCovFakeXServer(t, covUsFakeLayout())
			a := s.openActuator(t)
			before := len(s.requestsOf(covXTestMajorOpcode))

			err := a.Click(button)
			if err == nil {
				t.Fatalf("Click(%d) was accepted — buttons outside %d..%d must be refused", button, covButtonPressDetailMin, covButtonPressDetailMax)
			}
			if !strings.Contains(err.Error(), "out of range") {
				t.Errorf("Click(%d) error = %v, want an out-of-range refusal", button, err)
			}
			if after := len(s.requestsOf(covXTestMajorOpcode)); after != before {
				t.Errorf("Click(%d) wrote %d XTestFakeInput request(s) despite being out of range", button, after-before)
			}
		})
	}
}

// TestCovX11ClickPropagatesWriteError covers Click's request-failure path: the
// fake accepts the connection, serves the handshake, then stops reading — the
// press write then fails and Click must return it instead of swallowing it
// (Rule 15).
func TestCovX11ClickPropagatesWriteError(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)

	// Force a write error deterministically: the actuator owns its own
	// connection, so close it out from under the next request.
	if a.c == nil || a.c.conn == nil {
		t.Fatal("actuator has no connection to break")
	}
	a.c.conn.(*net.UnixConn).Close() //stylegate:ignore test closes the actuator's own socket to force the write error the next line asserts; a close error cannot break the connection twice
	if err := a.Click(1); err == nil {
		t.Error("Click on a closed connection returned nil — the write error must propagate")
	}
}

// TestCovX11TypeTextRequestBytes is the wire-layout test for TypeText: it
// asserts, byte by byte, the KeyPress/KeyRelease pairs the client emits for a
// string with a shifted character, INCLUDING that the keycodes come from the
// server's own keyboard mapping (the fake's layout deliberately assigns 'n' and
// 'N' two different keycodes, so a hardcoded shift-same-key implementation
// cannot pass).
func TestCovX11TypeTextRequestBytes(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)

	if err := a.TypeText("nN"); err != nil {
		t.Fatalf("TypeText(%q): %v", "nN", err)
	}
	got := s.fakeInputEvents()
	want := []covBtnEvent{
		{Type: xEventKeyPress, Detail: covKeycodeLowerN},
		{Type: xEventKeyRelease, Detail: covKeycodeLowerN},
		{Type: xEventKeyPress, Detail: covShiftKeycode},
		{Type: xEventKeyPress, Detail: covKeycodeUpperN},
		{Type: xEventKeyRelease, Detail: covKeycodeUpperN},
		{Type: xEventKeyRelease, Detail: covShiftKeycode},
	}
	if len(got) != len(want) {
		t.Fatalf("TypeText(%q) emitted %d events, want %d (press/release per rune, Shift wrapped): got %+v", "nN", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = {type %d, detail %d}, want {type %d, detail %d} (Shift_L code %d resolved from the server mapping)",
				i, got[i].Type, got[i].Detail, want[i].Type, want[i].Detail, covShiftKeycode)
		}
	}

	// Byte-level shape of the key events: same 36-byte request, no coordinates.
	for i, req := range s.requestsOf(covXTestMajorOpcode) {
		if len(req) != xtestFakeInputWords*4 {
			t.Errorf("key request %d is %d bytes, want %d", i, len(req), xtestFakeInputWords*4)
			continue
		}
		if req[1] != xtestOpFakeInput {
			t.Errorf("key request %d xtReqType = %d, want %d", i, req[1], xtestOpFakeInput)
		}
		if req[4] != xEventKeyPress && req[4] != xEventKeyRelease {
			t.Errorf("key request %d type = %d, want %d or %d", i, req[4], xEventKeyPress, xEventKeyRelease)
		}
		if root := binary.LittleEndian.Uint32(req[12:16]); root != 0 {
			t.Errorf("key request %d root = 0x%x, want 0 (key events carry no coordinates)", i, root)
		}
	}
}

// TestCovX11TypeTextStopsAtFirstBadRune covers TypeText's error propagation:
// an un-mappable rune must abort the whole call (never type a partial string
// and report success — Rule 15). The fake records only the events of the
// characters that preceded the bad one.
func TestCovX11TypeTextStopsAtFirstBadRune(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)

	err := a.TypeText("n" + string(rune(0x4e2d)) + "n") // 中 — no keysym in this client
	if err == nil {
		t.Fatal("TypeText accepted a CJK rune — it must refuse loudly")
	}
	if !strings.Contains(err.Error(), "no X11 keysym mapping") {
		t.Errorf("TypeText error = %v, want the unmappable-rune refusal", err)
	}
	// The refusal aborts the loop BEFORE the client's own round-trip barrier, so
	// wait for the server to drain the stream before reading the recording.
	if err := covFakeBarrier(a); err != nil {
		t.Fatalf("barrier round-trip on the fake server: %v", err)
	}
	// 'n' typed, then the refusal: exactly one press/release pair, no more.
	got := s.fakeInputEvents()
	if len(got) != 2 {
		t.Errorf("TypeText typed %d events before refusing, want exactly 2 (the leading 'n'): %+v", len(got), got)
	}
}

// TestCovX11TypeTextUnknownKeysym covers the second TypeText error path: the
// rune HAS a keysym (a printable ASCII char this client maps) but the server's
// mapping does not advertise it — the client must say so instead of guessing a
// keycode.
func TestCovX11TypeTextUnknownKeysym(t *testing.T) {
	layout := covUsFakeLayout()
	delete(layout, covKeysymLowerZ) // drop XK_z from the server's mapping
	s := startCovFakeXServer(t, layout)
	a := s.openActuator(t)

	err := a.TypeText("z")
	if err == nil {
		t.Fatal("TypeText typed a keysym the server's mapping does not contain")
	}
	if !strings.Contains(err.Error(), "not in the server's keyboard mapping") {
		t.Errorf("TypeText error = %v, want the not-in-mapping refusal", err)
	}
	if err := covFakeBarrier(a); err != nil {
		t.Fatalf("barrier round-trip on the fake server: %v", err)
	}
	if n := len(s.fakeInputEvents()); n != 0 {
		t.Errorf("TypeText emitted %d events for an unresolvable keysym, want 0", n)
	}
}

// TestCovX11KeyboardMapRequestBytes pins the GetKeyboardMapping request the
// keycode resolver emits on first use, including the first-keycode/count pair
// that xactuate.go declares as block-local constants.
func TestCovX11KeyboardMapRequestBytes(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)

	if _, err := a.Focus(); err != nil { // any round-trip so the client is live
		t.Fatalf("Focus(): %v", err)
	}
	if err := a.TypeText("n"); err != nil {
		t.Fatalf("TypeText(%q): %v", "n", err)
	}
	reqs := s.requestsOf(x11OpGetKeyboardMapping)
	if len(reqs) != 1 {
		t.Fatalf("emitted %d GetKeyboardMapping requests, want 1 (the mapping is cached on first use)", len(reqs))
	}
	req := reqs[0]
	if len(req) != x11GetKeyboardMappingLen*4 {
		t.Fatalf("GetKeyboardMapping request is %d bytes, want %d", len(req), x11GetKeyboardMappingLen*4)
	}
	if words := binary.LittleEndian.Uint16(req[2:4]); int(words) != x11GetKeyboardMappingLen {
		t.Errorf("GetKeyboardMapping length field = %d, want %d", words, x11GetKeyboardMappingLen)
	}
	if req[4] != covKeymapFirstKeycode {
		t.Errorf("GetKeyboardMapping first-keycode = %d, want %d (xactuate.go's firstKeycode)", req[4], covKeymapFirstKeycode)
	}
	if req[5] != covKeymapKeycodeCount {
		t.Errorf("GetKeyboardMapping count = %d, want %d (xactuate.go's count)", req[5], covKeymapKeycodeCount)
	}
}

// TestCovX11KeymapShortReply covers the malformed-reply guard: a
// GetKeyboardMapping reply whose body is shorter than keysyms-per-keycode *
// count * 4 must be handled without reading past the buffer — the resolver
// simply never finds the keysym and reports that loudly.
func TestCovX11KeymapShortReply(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	s.badKeymapLen = 8 // one row of a 248-keycode table
	a := s.openActuator(t)

	err := a.TypeText("z")
	if err == nil {
		t.Fatal("TypeText accepted a truncated keyboard mapping")
	}
	if !strings.Contains(err.Error(), "keyboard mapping") {
		t.Errorf("TypeText error = %v, want a loud keyboard-mapping refusal (a truncated table must not panic or mis-index)", err)
	}
}

// TestCovX11KeymapZeroPerCode covers the other malformed reply: a
// keysyms-per-keycode of 0 would otherwise divide the table by zero. The
// resolver must return an error, never panic.
func TestCovX11KeymapZeroPerCode(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	s.zeroPerCode = true
	a := s.openActuator(t)

	err := a.TypeText("n")
	if err == nil {
		t.Fatal("TypeText accepted a keysyms-per-keycode of 0")
	}
	if !strings.Contains(err.Error(), "0 keysyms per keycode") {
		t.Errorf("TypeText error = %v, want the zero-keysyms-per-keycode error", err)
	}
}

// TestCovX11OpenActuatorNoDisplay covers Display's and TypeText's shared
// entry guard: with no display at all, OpenXActuator must refuse with the
// actionable message — and never dial.
func TestCovX11OpenActuatorNoDisplay(t *testing.T) {
	t.Setenv(x11EnvDisplay, "  ") // whitespace only == unset
	a, err := OpenXActuator("")
	if err == nil {
		a.Close()
		t.Fatal("OpenXActuator(\"\") succeeded with no display")
	}
	if !strings.Contains(err.Error(), x11EnvDisplay) {
		t.Errorf("OpenXActuator error = %v, want it to name %s", err, x11EnvDisplay)
	}
}

// TestCovX11OpenActuatorUnconnectableDisplay covers the connect-failure path
// shared by every verb: a syntactically valid but dead display must surface the
// dial error (never a silent no-op).
func TestCovX11OpenActuatorUnconnectableDisplay(t *testing.T) {
	dead, err := covFreeDisplayNumber()
	if err != nil {
		t.Skipf("cannot find a free display number: %v", err)
	}
	a, err := OpenXActuator(dead)
	if err == nil {
		a.Close()
		t.Fatalf("OpenXActuator(%s) connected to a display that does not exist", dead)
	}
	if !strings.Contains(err.Error(), "connect to X display") {
		t.Errorf("OpenXActuator(%s) error = %v, want a connect failure naming the display", dead, err)
	}
	// Range-checked verbs must fail the same way (there is no connection to
	// publish a result on).
	if _, err := OpenXActuator(dead); err == nil {
		t.Error("second attempt unexpectedly succeeded")
	}
}

// TestCovX11ClickNoDisplay covers Click's and TypeText's shared refusal when
// the display cannot even be opened — the loud counterpart of a silent no-op.
func TestCovX11ClickNoDisplay(t *testing.T) {
	dead, err := covFreeDisplayNumber()
	if err != nil {
		t.Skipf("cannot find a free display number: %v", err)
	}
	a, err := OpenXActuator(dead)
	if err == nil {
		defer a.Close()
		t.Fatalf("OpenXActuator(%s) unexpectedly connected", dead)
	}
	if !strings.Contains(err.Error(), "connect to X display") {
		t.Errorf("error = %v, want a connect failure", err)
	}
}

// ---------------------------------------------------------------------------
// Test-side helpers.
// ---------------------------------------------------------------------------

const (
	// covXvfbDisplayMin/Max bound the display numbers the LIVE tests may start
	// a private Xvfb on. Sockets and lock files are NOT enough to prove a
	// number is free on this host (a stale socket file or another user's server
	// can occupy it), so covFreeDisplayNumber also tries to BIND the socket —
	// the authoritative test.
	covXvfbDisplayMin = 200
	covXvfbDisplayMax = 219

	// covXLockDir spells the DIRECTORY of the X server's lock file. X11
	// mandates the location (a server must be findable by a client that only
	// knows the display number), so this is the same protocol endpoint as
	// x11UnixSocketDir — the ONE /tmp path the protocol fixes — and it is
	// spelled through the shared constant rather than re-inlined (Rules 1b/13).
	//
	// worktree-root-ok: the X11 protocol mandates the location, so deriving it
	// from configuration is impossible; it is a socket/lock NAMESPACE, never a
	// data scratch root (devdoc/tmp-ban-scratch-root.md). Referencing the
	// constant keeps the VALUE single-homed; it does not make it configurable.
	covXLockDir    = worktreeroots.AmbientTempRoot
	covXLockPrefix = "."
	covXLockSuffix = "-lock"

	// covXvfbNoResetFlag keeps the private X server alive after the LAST client
	// disconnects. Without it Xvfb resets the server when the readiness probe
	// closes its connection (and again whenever a test finishes a session), and
	// the next connect can be answered with a connection reset instead of the
	// setup reply — the flaky "display :NNN reachable but not actuable" SKIP.
	covXvfbNoResetFlag = "-noreset"
)

// covFreeDisplayNumber returns a ":N" in the Xvfb range that this process can
// actually bind: neither the socket nor the lock file exists AND a probe bind
// of the socket path succeeds (the probe is closed immediately; Xvfb rebinds
// it a moment later). The range is swept so two concurrent test binaries do
// not collide.
func covFreeDisplayNumber() (string, error) {
	for n := covXvfbDisplayMin; n <= covXvfbDisplayMax; n++ {
		num := fmt.Sprint(n)
		sock := filepath.Join(x11UnixSocketDir, x11UnixSocketPrefix+num)
		lock := covXLockPath(num)
		if _, err := os.Stat(sock); err == nil {
			continue
		}
		if _, err := os.Stat(lock); err == nil {
			continue
		}
		probe, err := net.Listen("unix", sock)
		if err != nil {
			continue // someone holds it, or we may not bind here
		}
		probe.Close() //stylegate:ignore test probe releases the socket it just bound, then removes it, to prove the display is bindable; a close error only leaves a socket the following os.Remove clears
		os.Remove(sock)
		return ":" + num, nil
	}
	return "", fmt.Errorf("no bindable display number in :%d-:%d", covXvfbDisplayMin, covXvfbDisplayMax)
}

// covXLockPath composes an X server's lock-file path for a display number.
// The location is fixed by the X11 protocol (see covXLockDir), so it is
// composed rather than invented.
func covXLockPath(num string) string {
	return filepath.Join(covXLockDir, covXLockPrefix+x11UnixSocketPrefix+num+covXLockSuffix)
}

// covStartPrivateXvfb starts an Xvfb on a swept display for the duration of
// one test, returning the display string. It SKIPS VISIBLY (with the reason)
// when Xvfb is missing or the server never comes up, and it removes the socket
// afterwards so the next run starts clean.
func covStartPrivateXvfb(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("Xvfb is not installed — live actuation cannot be exercised (wire-layout tests still ran)")
	}
	display, err := covFreeDisplayNumber()
	if err != nil {
		t.Skipf("no free display number for a private Xvfb: %v", err)
	}
	num := strings.TrimPrefix(display, ":")
	cmd := exec.Command("Xvfb", display, covXvfbNoResetFlag, "-screen", "0", fmt.Sprintf("%dx%dx%d", covFakeWidth, covFakeHeight, covFakeDepth))
	// Never inherit the test's display, which would make Xvfb attach to an
	// existing server instead of creating one; covXvfbNoResetFlag further
	// keeps the private server alive when the readiness probe / a finished
	// session disconnects its last client. It stays in its own process group
	// (Setsid), so killing the test cannot leave a stray X server holding the
	// socket, and reaping the group cannot touch anything the test did not
	// start — the reaper takes the server down on EVERY exit path, including
	// the panic path a bare t.Cleanup-with-kill would not survive.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	testutil.StartMockProcessCmd(t, cmd, testutil.AllowStartFailure())
	// The socket and the lock file are the protocol-mandated X server
	// endpoints (a real server owns the same two paths); this test removes
	// the pair it created so the next run starts clean.
	t.Cleanup(func() {
		os.Remove(filepath.Join(x11UnixSocketDir, x11UnixSocketPrefix+num))
		os.Remove(filepath.Join(covXLockDir, covXLockPrefix+num+covXLockSuffix))
	})

	// Poll for readiness with a bounded budget — the socket appears when the
	// server is listening, which is deterministic, not a sleep.
	deadline := time.Now().Add(covXvfbStartBudget)
	for {
		if c, err := dialX11(display); err == nil {
			c.close()
			return display
		}
		if time.Now().After(deadline) {
			t.Skipf("Xvfb on %s did not become reachable within %s — live actuation test skipped VIBLY (wire-layout tests still ran)", display, covXvfbStartBudget)
		}
		time.Sleep(covXvfbPollInterval) //stylegate:allow-sleep poll interval while Xvfb/Xev become reachable
	}
}

const (
	// covXvfbStartBudget bounds how long a private Xvfb may take to answer.
	covXvfbStartBudget = 10 * time.Second
	// covXvfbPollInterval is the readiness poll step (bounded, not a busy loop).
	covXvfbPollInterval = 25 * time.Millisecond

	// covXevStartBudget bounds how long the xev oracle may take to map its
	// window, and covXevPollInterval is its poll step.
	covXevStartBudget  = 10 * time.Second
	covXevPollInterval = 25 * time.Millisecond
	// covXevClassHint is the xev binary/class name; the recorder's window id is
	// read from xev's own first line (see covXevWindowRe) so no window-manager
	// title lookup is needed.
	covXevClassHint = "xev"
	// covXevGeometry is the size/position the recorder window is given; the live
	// test injects events at fixed root coordinates, so the window must cover
	// them.
	covXevGeometry = "400x300+0+0"

	// covXdotoolBinary is the independent X client used as an oracle for the
	// input focus and for locating the recorder window.
	covXdotoolBinary = "xdotool"

	// covButton3LogMarker is what the event recorder prints for a delivered
	// button-3 (right) press. covLiveTypedText is the text the live test types
	// and covLastTypedKeysymMarker the keysym line the recorder must log for its
	// last character ('f' -> XK_f = 0x66).
	covButton3LogMarker      = "button 3"
	covLiveTypedText         = "nf"
	covLastTypedKeysymMarker = "keysym 0x66"

	// covUnmappableRune is a CJK character this client has no keysym for - the
	// loud-refusal path.
	covUnmappableRune = "知"

	// covEventDeliveryBudget bounds how long the X server may take to deliver
	// an injected event to the recorder window.
	covEventDeliveryBudget = 5 * time.Second

	// covNamedKeyReturn is one of the named keys the `key` verb accepts; it is
	// used to show the unknown-name guard is not a blanket refusal. (The
	// vocabulary itself lives in xactuate.go's namedKeysyms map.)
	covNamedKeyReturn = "return"

	// covKnownBugName names the product defect this slice discovered and filed
	// but must NOT fix (tests only): the reply-framed reader consumes a pending
	// MappingNotify event as if it were a request reply.
	covKnownBugName = "GetInputFocus reply framing vs MappingNotify"
)

// covStartXev starts xev — the X event recorder — on the given display and
// returns the WINDOW ID of its "Event Tester" window plus the path of the file
// capturing its stdout. xev is the ideal verification surface: it writes one
// block per event it receives, so a button or key that actually reached the
// server is OBSERVABLE in a file, not inferred. The test skips VISIBLY when
// xev is not installed.
func covStartXev(t *testing.T, display string) (windowID string, logPath string) {
	t.Helper()
	if _, err := exec.LookPath(covXevClassHint); err != nil {
		t.Skipf("%s is not installed — the live observable-change oracle is unavailable", covXevClassHint)
	}
	if _, err := exec.LookPath(covXdotoolBinary); err != nil {
		t.Skipf("%s is not installed — cannot look up the reporter window", covXdotoolBinary)
	}
	logPath = filepath.Join(t.TempDir(), "xev.log")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create xev log: %v", err)
	}
	cmd := exec.Command(covXevClassHint, "-geometry", covXevGeometry)
	cmd.Env = []string{"DISPLAY=" + display, "PATH=" + os.Getenv("PATH")}
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The recorder is a long-lived window; the pgid-scoped reaper owns it, so
	// a panic (or a skipped assertion) cannot leave it holding the display.
	testutil.StartMockProcessCmd(t, cmd, testutil.AllowStartFailure(testutil.OnSkip(func() { f.Close() }))) //stylegate:ignore close of a log file already written by the checked calls; the xev child inherits the handle until it exits, so a close error here is not actionable
	t.Cleanup(func() { f.Close() })                                                                         //stylegate:ignore same handle as the line above; the OS reclaims it at process exit

	deadline := time.Now().Add(covXevStartBudget)
	for {
		// xev prints its own window id on its first line — read it from there
		// instead of searching by name (no window manager has to set a title).
		if b, err := os.ReadFile(logPath); err == nil {
			if m := covXevWindowRe.FindSubmatch(b); m != nil {
				return string(m[1]), logPath
			}
		}
		if time.Now().After(deadline) {
			rec, _ := os.ReadFile(logPath) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
			t.Skipf("%s never mapped its window on %s within %s — live observable test skipped VIBLY. Recorder output: %q",
				covXevClassHint, display, covXevStartBudget, string(rec))
		}
		time.Sleep(covXevPollInterval) //stylegate:allow-sleep poll interval while Xvfb/Xev become reachable
	}
}

// covFocusWindow hands the input focus to a window through xdotool, so the
// injected key events have a destination. A display with no window manager has
// no focus policy of its own, which is exactly why the test sets it explicitly
// instead of hoping.
func covFocusWindow(t *testing.T, display, windowID string) {
	t.Helper()
	cmd := exec.Command(covXdotoolBinary, "windowfocus", windowID)
	cmd.Env = []string{"DISPLAY=" + display, "PATH=" + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot set the input focus to %s (%s): %v — live observable test needs a focused window", windowID, strings.TrimSpace(string(out)), err)
	}
}

// covWaitForFileContains polls path until it contains want, returning the
// file's content. It fails the test (with the content it did see) rather than
// hanging when the deadline passes — a bounded wait, never a bare sleep.
func covWaitForFileContains(t *testing.T, path, want string, budget time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(budget)
	var last string
	for {
		if b, err := os.ReadFile(path); err == nil {
			last = string(b)
			if strings.Contains(last, want) {
				return last
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the X server never delivered the expected event %q within %s — the injected request was not honoured. Recorder output so far:\n%s", want, budget, last)
		}
		time.Sleep(covXevPollInterval) //stylegate:allow-sleep poll interval while Xvfb/Xev become reachable
	}
}

// ---------------------------------------------------------------------------
// Live tests (private Xvfb; skip visibly when unavailable).
// ---------------------------------------------------------------------------

// TestLiveDisplayFocusClickTypeObservable exercises Display, Focus, Click and
// TypeText against a REAL X server started by this test, and proves each one
// changed something OBSERVABLE:
//
//   - Focus(): the window the server reports holding the input focus is
//     cross-checked against an INDEPENDENT X client (xdotool getwindowfocus) —
//     a misparsed GetInputFocus reply cannot agree with it;
//   - Click(): the X server must DELIVER the button event to a focused window —
//     xev (the X event recorder) logs ButtonPress/ButtonRelease with the button
//     number that was actually requested, at the pointer position the server
//     reads back through an independent QueryPointer round-trip;
//   - TypeText(): xev must log a KeyPress AND its matching KeyRelease per
//     character, and the refusal of a non-ASCII rune must be loud yet leave the
//     connection usable.
//
// The test never touches $DISPLAY: it starts its own Xvfb, so nothing is
// injected into a display another agent may be using. It skips VISIBLY (with
// the reason) when Xvfb or its oracle is unavailable.
func TestLiveDisplayFocusClickTypeObservable(t *testing.T) {
	display := covStartPrivateXvfb(t)
	windowID, logPath := covStartXev(t, display)
	covFocusWindow(t, display, windowID)

	a, err := OpenXActuator(display)
	if err != nil {
		t.Skipf("display %s reachable but not actuable: %v", display, err)
	}
	defer a.Close()

	// --- Display ---------------------------------------------------------
	if got := a.Display(); got != display {
		t.Errorf("Display() = %q, want %q (the display this actuator was opened with)", got, display)
	}

	// --- Focus: the parsed window id must match an independent client -----
	focused, err := a.Focus()
	if err != nil {
		t.Fatalf("Focus() on %s: %v", display, err)
	}
	if focused == 0 {
		t.Error("Focus() reported window 0 although the server has a focused window")
	}
	if want, ok := covXdotoolFocusedWindow(t, display); ok && want != focused {
		t.Errorf("Focus() = 0x%x but the independent X client reports the focused window as 0x%x — the GetInputFocus reply was misparsed", focused, want)
	}

	// --- Click: xev must RECEIVE the button event ------------------------
	const tx, ty = 77, 55
	if err := a.MovePointer(tx, ty); err != nil {
		t.Fatalf("MovePointer(%d,%d): %v", tx, ty, err)
	}
	gotX, gotY, err := a.Pointer()
	if err != nil {
		t.Fatalf("Pointer(): %v", err)
	}
	if gotX != tx || gotY != ty {
		t.Fatalf("pointer read back at (%d,%d), want (%d,%d) — the motion request was not honoured", gotX, gotY, tx, ty)
	}
	if err := a.Click(3); err != nil {
		t.Fatalf("Click(3): %v", err)
	}
	// The observable half: the server delivered the button event, and the
	// recorder wrote down which button it was.
	log := covWaitForFileContains(t, logPath, "ButtonRelease", covEventDeliveryBudget)
	if !strings.Contains(log, covButton3LogMarker) {
		t.Errorf("the recorder logged a button event that is not %s — the injected detail byte did not match Click(3). Recorder output:\n%s", covButton3LogMarker, log)
	}

	// --- TypeText: xev must RECEIVE one key pair per character -----------
	// The MappingNotify desync used to make this call fail on a fresh display
	// ("GetInputFocus: unexpected reply type 34"): the server queues a
	// MappingNotify (event type 34) the moment a client fetches the keyboard
	// mapping, and the reply-framed reader consumed that event as its answer.
	// The reader now DRAINS interleaved event frames, so the call must succeed
	// outright — a failure here is a regression, and it is reported with the
	// event type it should never surface as (covKnownBugName names the defect).
	typeErr := a.TypeText(covLiveTypedText)
	if typeErr != nil {
		t.Fatalf("TypeText(%q) failed: %v — a drained MappingNotify (event type %d) must never surface as the answer to a round-trip (%s)",
			covLiveTypedText, typeErr, covX11EventTypeMappingNotify, covKnownBugName)
	}
	log = covWaitForFileContains(t, logPath, covLastTypedKeysymMarker, covEventDeliveryBudget)
	if !strings.Contains(log, "KeyRelease") {
		t.Errorf("the recorder logged a KeyPress without a KeyRelease — the press/release pair was not completed. Recorder output:\n%s", log)
	}

	// The refusal path is decided BEFORE any I/O (keysymForRune runs first), so
	// it is unaffected by the desync above — and it must be loud.
	if err := a.TypeText(covUnmappableRune); err == nil {
		t.Errorf("TypeText accepted %q — it must refuse loudly rather than type mojibake", covUnmappableRune)
	}

	// A refused string must not be the thing that breaks a session: a FRESH
	// actuator on the same display round-trips normally.
	fresh, err := OpenXActuator(display)
	if err != nil {
		t.Fatalf("re-opening the actuator on %s: %v", display, err)
	}
	defer fresh.Close()
	if _, err := fresh.Focus(); err != nil {
		t.Errorf("Focus() on a fresh session: %v — opening a session must always leave it usable", err)
	}

}

// TestLiveDisplayFocusChangesWithFocus is the Focus() observable-change test on
// its own: our parsed focus id must equal what an independent X client reports,
// and moving the focus to a second window must move the value with it.
func TestLiveDisplayFocusChangesWithFocus(t *testing.T) {
	display := covStartPrivateXvfb(t)
	firstID, _ := covStartXev(t, display)
	covFocusWindow(t, display, firstID)

	a, err := OpenXActuator(display)
	if err != nil {
		t.Skipf("display %s reachable but not actuable: %v", display, err)
	}
	defer a.Close()

	got, err := a.Focus()
	if err != nil {
		t.Fatalf("Focus(): %v", err)
	}
	want, ok := covXdotoolFocusedWindow(t, display)
	if !ok {
		t.Skip("no independent focus oracle available on this host")
	}
	if want == 0 {
		t.Skip("the display reports no focused window — nothing to compare against")
	}
	if got != want {
		t.Errorf("Focus() = 0x%x, the independent X client says 0x%x — GetInputFocus was answered by the wrong bytes", got, want)
	}
}

// covXevWindowRe extracts the recorder's window id from its own first line
// ("Outer window is 0x200001, inner window is 0x200002").
var covXevWindowRe = regexp.MustCompile(`Outer window is (0x[0-9a-fA-F]+)`)

// covXdotoolFocusedWindow asks an INDEPENDENT X client (xdotool) which window
// currently holds the input focus. ok=false means no oracle is available here,
// in which case the caller degrades to the other assertions instead of failing
// on a missing tool.
func covXdotoolFocusedWindow(t *testing.T, display string) (uint32, bool) {
	t.Helper()
	if _, err := exec.LookPath(covXdotoolBinary); err != nil {
		return 0, false
	}
	cmd := exec.Command(covXdotoolBinary, "getwindowfocus")
	cmd.Env = []string{"DISPLAY=" + display, "PATH=" + os.Getenv("PATH")}
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]), 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(parsed), true
}

// TestKnownBugTypeTextMappingNotifyDesync is the regression guard for the defect
// this slice found and FIXED: on a fresh display the first TypeText failed with
// "GetInputFocus: unexpected reply type 34", because a MappingNotify event
// (type 34, queued when the client's GetKeyboardMapping registered it for
// keyboard-map changes) occupied the reply slot — a no-reply XTestFakeInput sits
// between the fetch and the GetInputFocus round-trip, so nothing was read in
// between. The wrong frame was consumed as the answer and the connection stayed
// frame-shifted (sticky).
//
// The reader now classifies frames (event/reply/error), drains events up to a
// bound and matches the reply's sequence, so BOTH branches below must hold: the
// first TypeText succeeds (the branch this test takes on a live server) and a
// second, independent round-trip on the SAME connection still answers honestly
// (the sticky half). The deterministic fake-server reproduction — where the
// MappingNotify is GUARANTEED to be in the stream rather than merely likely —
// is TestReadReplyDrainsMappingNotifyBeforeFocusReply.
func TestKnownBugTypeTextMappingNotifyDesync(t *testing.T) {
	display := covStartPrivateXvfb(t)
	a, err := OpenXActuator(display)
	if err != nil {
		t.Skipf("display %s reachable but not actuable: %v", display, err)
	}
	defer a.Close()

	first := a.TypeText(covLiveTypedText)
	if first != nil {
		// The fixed reader must not surface the stray event as an error at all.
		if strings.Contains(first.Error(), fmt.Sprint(covX11EventTypeMappingNotify)) {
			t.Fatalf("%s REGRESSED: %v", covKnownBugName, first)
		}
		t.Fatalf("the first TypeText on a fresh display failed: %v", first)
	}
	t.Logf("the first act verb on a fresh display succeeded (the MappingNotify frame was drained) — %s holds", covKnownBugName)

	// The sticky half: an independent round-trip on the SAME connection still
	// answers truthfully (a frame-shifted connection used to answer with the
	// stray event, forever).
	if _, err := a.Focus(); err != nil {
		t.Errorf("a second round-trip on the same connection failed: %v — the connection is frame-shifted again", err)
	}
	// A fresh session on the same display is unaffected (the defect was never
	// about the server).
	fresh, err := OpenXActuator(display)
	if err != nil {
		t.Fatalf("fresh OpenXActuator(%s): %v", display, err)
	}
	defer fresh.Close()
	if _, err := fresh.Focus(); err != nil {
		t.Errorf("a fresh session on the same display failed too: %v", err)
	}
}

// TestCovX11MovePointerRejectsOutOfRangeCoordinate covers the coordinate guard
// named in the briefing: a target outside the root window must be refused
// loudly and must NOT put a motion request on the wire (an out-of-range warp
// would otherwise be clamped silently by the server, hiding the caller's bug).
func TestCovX11MovePointerRejectsOutOfRangeCoordinate(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)
	before := len(s.requestsOf(covXTestMajorOpcode))

	cases := []struct {
		name string
		x, y int
	}{
		{name: "negative-x", x: -1, y: 10},
		{name: "negative-y", x: 10, y: -1},
		{name: "x-at-width", x: covFakeWidth, y: 10},
		{name: "y-at-height", x: 10, y: covFakeHeight},
		{name: "far-outside", x: covFakeWidth * 2, y: covFakeHeight * 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.MovePointer(tc.x, tc.y)
			if err == nil {
				t.Fatalf("MovePointer(%d,%d) was accepted — the root window is %dx%d", tc.x, tc.y, covFakeWidth, covFakeHeight)
			}
			if !strings.Contains(err.Error(), "outside") {
				t.Errorf("MovePointer(%d,%d) error = %v, want an outside-the-root-window refusal", tc.x, tc.y, err)
			}
		})
	}
	if after := len(s.requestsOf(covXTestMajorOpcode)); after != before {
		t.Errorf("%d XTestFakeInput request(s) were written for refused coordinates, want 0", after-before)
	}
	// The boundary itself is legal: the last addressable pixel must be accepted.
	// (No reply follows a motion request, so this only asserts it is not
	// refused by the guard.)
	if err := a.MovePointer(covFakeWidth-1, covFakeHeight-1); err != nil {
		t.Errorf("MovePointer(%d,%d) refused the last addressable pixel: %v", covFakeWidth-1, covFakeHeight-1, err)
	}
}

// TestCovX11KeyRejectsUnknownName covers the unknown-key-name path on the
// public surface (Key is the sibling of TypeText and resolves names through the
// same vocabulary). An unknown name must be refused before any I/O.
func TestCovX11KeyRejectsUnknownName(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)
	before := len(s.requestsOf(covXTestMajorOpcode))

	for _, name := range []string{"f13", "Return", "enter", "", "typo"} {
		t.Run("name_"+name, func(t *testing.T) {
			err := a.Key(name)
			if err == nil {
				t.Fatalf("Key(%q) was accepted — only the named vocabulary maps", name)
			}
			if !strings.Contains(err.Error(), "unknown key") {
				t.Errorf("Key(%q) error = %v, want an unknown-key refusal", name, err)
			}
		})
	}
	if after := len(s.requestsOf(covXTestMajorOpcode)); after != before {
		t.Errorf("%d XTestFakeInput request(s) written for unknown key names, want 0", after-before)
	}
	// A name that IS in the vocabulary still works (so the guard is not a
	// blanket refusal).
	if err := a.Key(covNamedKeyReturn); err != nil {
		t.Errorf("Key(%q) was refused although it is a valid name: %v", covNamedKeyReturn, err)
	}
}
