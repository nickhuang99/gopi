// x11.go — the X11 backend of the capture path: a pure-Go, ZERO-dependency
// X11 protocol client that grabs the root window (the live desktop) with a
// GetImage request and returns a PNG.
//
// Why hand-rolled instead of a library: gopi ships a single static binary
// (CGO_ENABLED=0), so a libX11/libXtst binding is out by construction, and
// jezek/xgb is not in the offline module cache (no proxy on this box — see
// devdoc/screen-capture-and-actuation-tools.md §4). The capture path needs
// exactly three X11 requests (QueryExtension, GetImage, plus the connection
// setup handshake), so ~300 lines over net.Conn + encoding/binary + image/png
// is cheaper than any dependency — and it keeps the binary static.
//
// Protocol references (all quoted in the devdoc):
//   - X Window System Protocol, "Connection Setup" and "GetImage" (opcode 73)
//   - /usr/include/X11/extensions/xtestproto.h (XTEST request encodings)
//   - Xau / Xauthority file format (for MIT-MAGIC-COOKIE-1 auth)
package shot

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// X11 wire constants (Rule 1: named, never inline).
// ---------------------------------------------------------------------------

const (
	// x11ByteOrderLSBFirst / MSBFirst are the two setup-handshake byte-order
	// codes ('l' / 'B').
	x11ByteOrderLSBFirst = 0x6c
	x11ByteOrderMSBFirst = 0x42

	// x11ProtocolMajor / Minor are the protocol version we speak.
	x11ProtocolMajor = 11
	x11ProtocolMinor = 0

	x11SetupRequestLen = 12 // bytes of the client connection-setup request

	// Request opcodes we use.
	x11OpQueryExtension = 98
	x11OpGetImage       = 73

	// x11ImageFormatZPixmap is the GetImage format meaning "raw pixel values"
	// (the other two formats are XYPixmap / Bitmap).
	x11ImageFormatZPixmap = 2

	// x11GetImageRequestLen is the GetImage request size in 4-byte units
	// (20 bytes: opcode, format, length, drawable, x, y, w, h, plane-mask).
	x11GetImageRequestLen = 5

	// x11ReplyHeaderLen is the size of every X11 reply header in bytes.
	x11ReplyHeaderLen = 32

	// Reply/event type bytes. The type byte is shared by replies, errors and
	// events — anything >= x11EventTypeBase is an EVENT, not an answer, and
	// the send-event bit (0x80) is part of the byte, so classification masks
	// it off first.
	x11ReplyType       = 1
	x11ErrorType       = 0
	x11EventTypeBase   = 2
	x11SendEventMask   = 0x80
	x11EventTypeMasked = 0x7f
	// x11FakeEventSequence is the sequence field of an event frame: events
	// are not answers, so they carry sequence 0 (X11 events have no reply
	// sequence; a non-zero value there is a stale reply, not an event).
	x11FakeEventSequence = 0
	// x11EventTypeMappingNotify is the event the server queues for a client
	// that fetched the keyboard mapping and whose map then changed (X11
	// event code 34) — the frame that broke GetInputFocus framing.
	x11EventTypeMappingNotify = 34
	// The MappingNotify request field names WHAT changed; only the two
	// keyboard-mapping values invalidate a keycode cache (2 is the pointer
	// mapping).
	x11MappingModifier = 0
	x11MappingKeyboard = 1
	x11MappingPointer  = 2
	// x11MaxDrainedEvents bounds how many interleaved event frames one reply
	// read may discard before it gives up. Bounded on purpose: an unbounded
	// drain turns a broken/desynchronised stream into a hang (and the bound
	// is what makes the failure loud instead of silent).
	x11MaxDrainedEvents = 16

	// x11FamilyLocal is the Xauthority address-family value for a local
	// (unix-socket) connection; 0 is Internet, 6 is Internet6.
	x11FamilyLocal = 256
	// x11FamilyInternet is the Xauthority address-family value for a TCP
	// connection.
	x11FamilyInternet = 0
	// x11AuthCookieName is the only auth protocol this client implements.
	x11AuthCookieName = "MIT-MAGIC-COOKIE-1"

	// x11AuthMaxCandidates caps how many Xauthority credentials ONE display
	// gets. An Xauthority file legitimately holds several entries for the same
	// display NUMBER (one per address spelling — see readXAuthorityEntries),
	// and only the server can say which one it wants, so the client tries them
	// in priority order until one is accepted. The cap keeps that search a
	// fixed number of round trips on the failure path and prevents a huge
	// hand-maintained file from turning a refused display into a long stall.
	x11AuthMaxCandidates = 8

	// x11SetupReplySuccess / x11SetupReplyAuthenticate are the connection-setup
	// reply's status bytes: 1 = here is the connection, 2 = the server wants a
	// different auth protocol (which this client does not implement), 0 =
	// refused.
	x11SetupReplySuccess      = 1
	x11SetupReplyAuthenticate = 2

	// x11SetupReplyReasonOffset is the byte of the FAILED connection-setup
	// reply that holds the reason string's LENGTH in bytes (the 8-byte reply
	// prefix is: status(1), reason-length(1), protocol-major(2),
	// protocol-minor(2), length-of-additional-data-in-4-byte-units(2)).
	x11SetupReplyReasonOffset = 1

	// x11AuthDotFile is the Xauthority file looked up under the user's home
	// directory when XAUTHORITY is unset (the name xauth(1) itself writes).
	x11AuthDotFile = ".Xauthority"

	// x11AuthLabelSuffix badges the display name echoed by a capture that
	// needed a NON-FIRST credential from the Xauthority file (Rule 15: a
	// credential substitution is visible, never silent — the same discipline as
	// x11RouteLabelFallback).
	x11AuthLabelSuffix = "@auth:"

	// x11BitsPerPixel4 is the bytes-per-pixel of the depth-24/32bpp
	// TrueColor root window we require (the standard Xorg/Xvfb case).
	x11BitsPerPixel4 = 4

	// x11BandRows is the number of scanlines requested per GetImage request.
	// The image is grabbed in horizontal bands so that a full-HD grab never
	// depends on the server's single-reply size ceiling (the classic
	// XGetImage/BadAlloc trap that MIT-SHM was introduced to dodge).
	x11BandRows = 128

	// x11DialTimeout bounds the initial connect to the X socket.
	x11DialTimeout = 5 * time.Second

	// x11FallbackDialTimeout bounds a connect on a route that is NOT the
	// display's primary one. A fallback route is a local address (this
	// machine's own socket directory, or loopback TCP), where a dead listener
	// refuses at once — the short bound keeps an unreachable display's error
	// path fast instead of paying x11DialTimeout per route (a TCP connect to
	// an address nothing listens on otherwise waits out the SYN retry).
	x11FallbackDialTimeout = 1 * time.Second

	// x11IOTimeout bounds a single request/response exchange.
	x11IOTimeout = 20 * time.Second

	// x11HostLocalhost / x11HostLoopback / x11HostUnix are the DISPLAY host
	// spellings this package gives meaning to (Rule 1b): "localhost" and
	// "127.0.0.1" both name THIS machine reached over TCP, and "unix" is the
	// explicit spelling of the local socket family (an empty host means the
	// same). Every other host names a real remote X server.
	x11HostLocalhost = "localhost"
	x11HostLoopback  = "127.0.0.1"
	x11HostUnix      = "unix"

	// x11HintMaxDisplays caps how many live displays the unreachable-display
	// diagnosis names. The list is a REMEDY (the operator's next command), not
	// an inventory: a handful is actionable, a full listing is noise, and the
	// bound keeps the error path's cost fixed.
	x11HintMaxDisplays = 4

	// x11UnixSocketDir / x11UnixSocketPrefix name the abstract unix socket
	// path of a local X server (/tmp/.X11-unix/X<n>).
	// x11UnixSocketDir is the ONE /tmp path GoPi keeps: the X11 protocol
	// mandates the Unix socket directory, so a headless capture cannot move
	// it. This is a SOCKET NAMESPACE, never a data scratch root — nothing is
	// written into it (devdoc/tmp-ban-scratch-root.md, remaining /tmp
	// writers).
	//
	// The VALUE is the system temp root, whose single definition is
	// worktreeroots.AmbientTempRoot (Rule 14); this constant could reference it
	// without changing what a socket client binds. It keeps the literal
	// because here the literal IS THE SUBJECT: the gate's own boundary (see
	// internal/standards/worktree_root_gate.go) exempts a site whose point is
	// the standard location itself, and swapping in a name would hide the one
	// path a reviewer must be able to see at a glance.
	// worktree-root-ok: the X11 protocol mandates this socket directory (a
	// socket NAMESPACE, not a data root — see the block above), so the literal
	// is the reviewed exception the rule allows rather than a worktree path.
	x11UnixSocketDir    = "/tmp/.X11-unix"
	x11UnixSocketPrefix = "X"

	x11TCPPortBase = 6000 // display N → TCP port 6000+N
)

// x11EnvDisplay is the environment variable that names the display to
// capture when the caller passes none. Reading the environment happens in
// exactly ONE place (Rule 2).
const x11EnvDisplay = "DISPLAY"

// DisplayEnvName exports x11EnvDisplay's value for the callers that must
// WRITE the variable instead of reading it — this package's own tests, and the
// launcher-side/announcement tests of other packages. It is an ALIAS, not a
// second definition (Rule 14): the value has exactly one holder, above.
//
// env-reader-exempt: an alias of x11EnvDisplay; the reader is os.Getenv(x11EnvDisplay)
// in captureDisplayName, and no code reads a knob named "DISPLAY" through this
// identifier.
const DisplayEnvName = x11EnvDisplay

// x11EnvAuthFile optionally overrides the Xauthority path.
const x11EnvAuthFile = "XAUTHORITY"

// x11Geometry reports what was captured (echoed to the model as ground truth).
// The fields BELOW the original five are appended, never inserted between
// them: the whole-screen capture's JSON payload is a contract the CLI, the
// tool result and the tests already quote, so a new field adds to it.
type x11Geometry struct {
	Display string `json:"display"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Depth   int    `json:"depth"`
	Bands   int    `json:"bands"`

	// X / Y are the captured rectangle's origin in ROOT (screen) coordinates
	// — always 0,0 for a whole-screen capture, and the point the capture was
	// taken from for the window/area modes (see x11rect.go). They are echoed
	// because a rect capture's width/height alone cannot say WHERE it was.
	X int `json:"x"`
	Y int `json:"y"`

	// Mode names the capture shape (CaptureModeScreen|Window|Area) so a
	// consumer reading only the payload never has to infer it from the fields
	// that happen to be set.
	Mode string `json:"mode,omitempty"`

	// Window is the window id a window capture used, and WindowLabel names the
	// ROUTE that resolved it (the EWMH property, or the focus fallback) —
	// a substituted window is visible rather than silent (Rule 15).
	Window      string `json:"window,omitempty"`
	WindowLabel string `json:"window_label,omitempty"`
}

// x11ErrNoDisplay is the ONE wording for "there is no display to talk to",
// shared by every entry point: the same condition must not be described two
// ways depending on which call surfaced it.
func x11ErrNoDisplay() error {
	return fmt.Errorf("no X11 display: %s is unset and no display was given (X11-only path; a Wayland session needs a portal, which gopi does not implement yet)", x11EnvDisplay)
}

// The X11 error codes this package CLASSIFIES, named after the protocol's own
// vocabulary (X11/Xproto.h) rather than compared as bare numbers (Rule 1):
//
//	BadWindow   — the id names no window at all (never created, or destroyed)
//	BadDrawable — the id is not a drawable the server will draw into / read
//	              from right now (measured: the server sends this for a window
//	              whose client is gone, while an UNMAPPED window still answers)
//
// They are the same failure for a caller: "the window I resolved is not there
// at this instant". They are kept as two constants because the server sends
// two different codes and a report that prints "code 9" for a BadWindow would
// be describing a different fact.
const (
	X11ErrorBadWindow   = 3
	X11ErrorBadDrawable = 9
)

// X11Error is an X11 protocol ERROR reply, TYPED so a caller can tell one
// server refusal from another without matching the text of a message (Rule
// 15b: an error a caller has to re-parse a string to classify is an error that
// was not carried). readReply is the ONE place such a reply becomes an error,
// so every reader of the display — a window listing, a window rectangle, a
// GetImage band — sees the same type carrying the same code.
//
// WHY THIS EXISTS (measured 2026-10-04, task 01a103f4): the film driver met
// "GetGeometry: X11 error reply (code 9)" while it was photographing a terminal
// window that xfce4-terminal had momentarily taken off the server, and the
// message was the only thing that knew WHICH refusal it was — so "the window is
// gone for a moment" (a transient to retry) and "this display is misbehaving"
// (fatal) arrived as the same shape and could only have been told apart by
// matching the string. A code is the server's own word for it, and it is now
// carried.
type X11Error struct {
	// Request is the name of the request the client was awaiting when the
	// error arrived. readReply only returns an error whose sequence matches
	// the request being awaited, so this names the request that CAUSED it.
	Request string
	// Code is the X11 error code (X11ErrorBadWindow, X11ErrorBadDrawable, …).
	Code uint8
	// Sequence is the request's sequence number, echoed from the error frame.
	Sequence uint16
}

// Error renders the frame the way the client's diagnostics always did, with
// the protocol's own name for the code added: a reader who has never seen the
// numeric table can still read the failure, and one who has does not lose the
// number (Rule 15a: the required value is not dropped for prose).
func (e *X11Error) Error() string {
	if e == nil {
		return "X11 error reply (nil error frame)"
	}
	return fmt.Sprintf("%s: X11 error reply (code %d, %s, sequence %d)", e.Request, e.Code, e.ClassName(), e.Sequence)
}

// ClassName is the protocol's name for the error code, for the codes this
// package classifies. An unclassified code says so rather than guessing at a
// name (Rule 15).
func (e *X11Error) ClassName() string {
	if e == nil {
		return "no error frame"
	}
	switch e.Code {
	case X11ErrorBadWindow:
		return "BadWindow"
	case X11ErrorBadDrawable:
		return "BadDrawable"
	}
	return "unclassified"
}

// NewX11Error builds the typed error for an error reply. It is EXPORTED so a
// test can inject exactly the frame the server sends: the criterion this type
// exists for — "a window that is momentarily not there is a transient" — must
// be provable with no display and no server (Rule 31), and a test that
// injected some other error shape would be proving a different thing.
func NewX11Error(reqName string, code uint8, sequence uint16) *X11Error {
	return &X11Error{Request: reqName, Code: code, Sequence: sequence}
}

// IsX11Error reports whether err (or anything it wraps) is an X11 error reply.
func IsX11Error(err error) bool {
	var xe *X11Error
	return errors.As(err, &xe)
}

// X11ErrorCode returns the protocol code an X11 error carries, and whether err
// was one at all. The second return is not decoration: code 0 is not a valid
// X11 error code, and a caller that received "0" would otherwise have to treat
// a non-X11 error as a classified one.
func X11ErrorCode(err error) (uint8, bool) {
	var xe *X11Error
	if !errors.As(err, &xe) {
		return 0, false
	}
	return xe.Code, true
}

// IsWindowVanishedError reports whether err says the window is not on the
// server RIGHT NOW — the server's BadWindow (the id names nothing) or
// BadDrawable (the id is not a drawable at this instant). The two are one
// answer to one question, and this is the ONLY place that judgement is made
// (Rule 13/14): a caller that needs it asks, and never re-implements the test.
func IsWindowVanishedError(err error) bool {
	code, ok := X11ErrorCode(err)
	if !ok {
		return false
	}
	return code == X11ErrorBadWindow || code == X11ErrorBadDrawable
}

// x11Client is a synchronous X11 connection: one request → one reply at a
// time, in sequence order (no pipelining, no event queue).
type x11Client struct {
	conn   net.Conn
	seq    uint16
	root   uint32
	width  int
	height int
	depth  int
	bpp    int
	imgMSB bool // server image byte order is MSBFirst
	// keymapDirty records that the server announced a keyboard/pointer
	// mapping change (a MappingNotify event drained off the stream).
	// Consumers that cache keycodes MUST invalidate their cache when it is
	// set and clear it when they refetch — a stale keycode table types the
	// wrong characters with no error anywhere.
	keymapDirty bool
	// connectedDisplay is the display string this client was opened with —
	// echoed into every tool result so the model knows WHICH screen it saw.
	// When the display was reached through a FALLBACK route (x11Routes), the
	// string carries the "@route" suffix naming that route, so an echo of the
	// requested display can never be mistaken for the route that answered
	// (Rule 15: a route substitution is visible, never silent).
	connectedDisplay string
	extOp            map[string]byte
	maxReq           int
	release          int
}

// captureDisplayName returns the display to capture: the explicit argument,
// else $DISPLAY. Empty means "no X11 display available".
func captureDisplayName(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	return strings.TrimSpace(os.Getenv(x11EnvDisplay))
}

// CaptureX11Root grabs the root window of the given display (empty → $DISPLAY)
// and returns the encoded PNG plus the captured geometry.
func CaptureX11Root(display string) ([]byte, x11Geometry, error) {
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

	img, bands, err := c.getRootImage()
	if err != nil {
		return nil, geom, err
	}
	geom = x11Geometry{
		Display: c.connectedDisplay,
		Width:   c.width,
		Height:  c.height,
		Depth:   c.depth,
		Bands:   bands,
		Mode:    CaptureModeScreen,
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, geom, fmt.Errorf("png encode: %w", err)
	}
	return buf.Bytes(), geom, nil
}

// ProbeDisplay reports whether an X11 display can be reached and authenticated,
// WITHOUT capturing or injecting anything: it runs the connection setup
// handshake (socket dial + MIT-MAGIC-COOKIE-1 auth) and closes. It returns the
// display name it probed. It is the ONE reachability predicate — a caller that
// must decide "will a GUI tool ever work from this process?" asks this, never a
// hand-rolled socket test (Rule 2/14: the display grammar, the socket path and
// the auth lookup stay in this file).
//
// WHY IT EXISTS (devdoc/worker-display-ownership.md): a detached process
// INHERITS DISPLAY and can never re-acquire a sourceless one — a daemon
// launched from an SSH-forwarded display (localhost:N) loses that socket when
// the forward dies and then fails every capture with exit 1, silently and
// forever. The launcher is the DISPLAY owner, so the process it starts is the
// right place to ask the question out loud.
// It is deliberately weaker than OpenXActuator: no XTEST extension is required,
// so a display that can be captured (screen_capture) but not actuated is still
// reported reachable — the predicate answers "is this display alive", which is
// the question the announcement asks.
//
// An empty display resolves through the ONE reader (captureDisplayName →
// $DISPLAY), so a caller never repeats the lookup.
func ProbeDisplay(display string) (string, error) {
	name := captureDisplayName(display)
	if name == "" {
		return "", x11ErrNoDisplay()
	}
	c, err := dialX11(name)
	if err != nil {
		return name, err
	}
	c.close()
	return name, nil
}

// x11Display is a parsed DISPLAY string: [host]:display[.screen].
type x11Display struct {
	host   string
	number int
	screen int
}

// parseX11Display parses a DISPLAY value ("", ":0", "localhost:10.0",
// "unix:99"). An empty host means the local unix socket.
func parseX11Display(display string) (x11Display, error) {
	d := x11Display{}
	s := strings.TrimSpace(display)
	if s == "" {
		return d, errors.New("empty DISPLAY")
	}
	// Strip the screen suffix (after the LAST '.'), if any.
	if i := strings.LastIndex(s, "."); i > strings.LastIndex(s, ":") {
		n, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return d, fmt.Errorf("malformed DISPLAY screen %q in %q", s[i+1:], display)
		}
		d.screen = n
		s = s[:i]
	}
	host, numStr := "", s
	if i := strings.LastIndex(s, ":"); i >= 0 {
		host, numStr = s[:i], s[i+1:]
	} else {
		return d, fmt.Errorf("malformed DISPLAY %q (want [host]:N[.screen])", display)
	}
	n, err := strconv.Atoi(numStr)
	if err != nil || n < 0 {
		return d, fmt.Errorf("malformed DISPLAY number %q in %q", numStr, display)
	}
	if host == x11HostUnix {
		host = ""
	}
	d.host, d.number = host, n
	return d, nil
}

// x11Route is ONE way to reach a display: a transport plus the address it
// dials. Resolution (dialX11) builds the routes for a DISPLAY in priority
// order and takes the first that answers.
type x11Route struct {
	network string
	addr    string
	// fallback marks a route that is NOT the display's primary one — the
	// alternative transport for a display on THIS machine (the local unix
	// socket for a `localhost` display, or loopback TCP for a local one),
	// tried only after the primary. A fallback route is always a local
	// address, which is what makes its shorter dial bound honest (see
	// x11FallbackDialTimeout).
	fallback bool
}

// x11RouteLabelFallback is the badge appended to the display name echoed by
// every result (Rule 1b: a value the user reads, never an inline literal).
const x11RouteLabelFallback = "@fallback"

// connectedDisplayName is the name echoed for a display reached on route r and,
// when authBadge is non-empty, with the Xauthority entry that was ACCEPTED: the
// display as the caller wrote it, plus the fallback badge when r was not the
// display's primary route, plus the credential badge when the credential that
// worked was not the first candidate (Rule 15: a route or credential
// substitution is visible, never silent).
func connectedDisplayName(display string, r x11Route, authBadge string) string {
	name := display
	if r.fallback {
		name += x11RouteLabelFallback
	}
	if authBadge != "" {
		name += x11AuthLabelSuffix + authBadge
	}
	return name
}

// x11Routes returns the routes to reach a parsed DISPLAY, in priority order.
//
// The PRIMARY route is the transport the DISPLAY string names, tried FIRST and
// as written:
//
//	host ""/"unix"           → the local unix socket /tmp/.X11-unix/X<N>
//	host localhost/127.0.0.1 → TCP, port 6000+N
//	any other host           → TCP, port 6000+N
//
// THE FALLBACK ROUTE is the fix for "gopi tool act info: connect … no such file or
// directory". A display whose host is THIS machine gets a SECOND route to the
// same display NUMBER over the other local transport. `DISPLAY=localhost:N`
// reaching a dead SSH forward is the everyday shape: the forward is gone, so
// nothing listens on 6000+N and the dial fails — while the same number often
// names a LIVE local X server (the host's durable Xvfb) whose unix socket
// answers. That server is the display the operator meant in every sense that
// matters to actuation, and refusing to look for it made `gopi tool act`,
// `screen_capture` and the worker's display preflight report a broken GUI
// capability on a host where the GUI demonstrably worked (measured on peer1:
// DISPLAY=localhost:17.0 dead, :1 and :98 live).
//
// A REMOTE host gets NO fallback: its unix socket is not ours to invent, so no
// second route is fabricated there.
func x11Routes(d x11Display) []x11Route {
	sockPath := filepath.Join(x11UnixSocketDir, x11UnixSocketPrefix+strconv.Itoa(d.number))
	localSocket := x11Route{network: "unix", addr: sockPath, fallback: true}
	// A `localhost` display reaches this machine over TCP; the other two local
	// spellings reach it over the unix socket, so the TCP fallback for them is
	// loopback rather than the name the display carried.
	loopbackTCP := x11Route{network: "tcp", addr: net.JoinHostPort(x11HostLoopback, strconv.Itoa(x11TCPPortBase+d.number)), fallback: true}
	switch d.host {
	case "":
		return []x11Route{{network: "unix", addr: sockPath}, loopbackTCP}
	case x11HostLocalhost, x11HostLoopback:
		return []x11Route{{network: "tcp", addr: net.JoinHostPort(d.host, strconv.Itoa(x11TCPPortBase+d.number))}, localSocket}
	default:
		return []x11Route{{network: "tcp", addr: net.JoinHostPort(d.host, strconv.Itoa(x11TCPPortBase+d.number))}}
	}
}

// dialX11 connects to the display, performs the connection-setup handshake and
// records the root window geometry: the first route that ANSWERS wins (see
// x11Routes for the order and why a second route exists for a local display).
// When no route answers, the error names every route tried with its own
// failure and, if this host has live displays, names them as the remedy.
func dialX11(display string) (*x11Client, error) {
	return dialX11Routes(display, true)
}

// x11AuthRetryable reports whether offering a DIFFERENT Xauthority credential
// could change the outcome of a failed setup exchange — i.e. whether the dial
// loop should try the next candidate instead of abandoning the route.
//
// Two shapes are retryable, and they are the same condition seen from two
// servers:
//
//   - the server sent a REFUSAL (setup reply status 0) or demanded another auth
//     protocol (status 2) — its reason string says so;
//   - the peer CLOSED the connection during the setup exchange, with no reply
//     byte at all. That is how an X11 forward answers a wrong cookie (measured:
//     a live sshd forward on a display that answers fine with the right cookie
//     returns EOF with a wrong one) — the forwarder drops the connection rather
//     than speaking the protocol's refusal.
//
// A read TIMEOUT is deliberately NOT retryable: a server that answers nothing is
// not asking for another credential, and retrying would only multiply the wait
// by the number of candidates.
func x11AuthRetryable(err error) bool {
	var ref *x11SetupRefusalError
	if errors.As(err, &ref) {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed)
}

// x11CredentialBadge names the accepted credential in one short token for the
// display name a result echoes (Rule 15: a credential substitution is visible,
// never silent). A Local entry is named by its address spelling ("peer1"),
// an Internet one has none and says so.
func x11CredentialBadge(c x11Credential) string {
	if c.address != "" {
		return c.address
	}
	return x11CredentialBadgeNoAddress
}

// x11CredentialBadgeNoAddress is the badge for an Internet-family Xauthority
// entry, whose display field carries no address (xauth lists it as ":N").
const x11CredentialBadgeNoAddress = "no-address"

// dialX11Routes is dialX11 with the live-display hint switchable: the hint's
// own reachability probes call it with withHint == false, so a hint can never
// recurse into another hint.
//
// Within a route, the Xauthority credentials for the display number are tried in
// priority order (readXAuthorityEntries) until one is ACCEPTED, because only the
// server knows which of several entries for the same number its forward holds.
// The first credential is the common case and costs exactly what it always did;
// a substitution is recorded in the display name the result echoes.
func dialX11Routes(display string, withHint bool) (*x11Client, error) {
	d, err := parseX11Display(display)
	if err != nil {
		return nil, err
	}
	routes := x11Routes(d)
	creds := readXAuthorityEntries(d)
	if len(creds) == 0 {
		// No credential at all: one unauthenticated attempt. A server that
		// accepts local clients needs none, and one that does not gets a
		// refusal whose report says "no credential" rather than nothing.
		creds = []x11Credential{{}}
	}
	var failures []string
	for _, r := range routes {
		timeout := x11DialTimeout
		if r.fallback {
			timeout = x11FallbackDialTimeout
		}
		for ci, cred := range creds {
			conn, err := net.DialTimeout(r.network, r.addr, timeout)
			if err != nil {
				// An unreachable route is not a credential problem: no cookie
				// can make a dead socket answer.
				failures = append(failures, fmt.Sprintf("%s %s: %v", r.network, r.addr, err))
				break
			}
			badge := ""
			if ci > 0 {
				badge = x11CredentialBadge(cred)
			}
			c := &x11Client{conn: conn, extOp: map[string]byte{}, connectedDisplay: connectedDisplayName(display, r, badge)}
			if err := c.handshake(display, d, cred); err != nil {
				conn.Close() //stylegate:ignore transport teardown while trying the NEXT route/credential: this attempt is reported in the aggregate error below
				if ci == 0 {
					failures = append(failures, fmt.Sprintf("%s %s: %v%s", r.network, r.addr, err, x11CredentialNote(len(creds))))
				}
				if x11AuthRetryable(err) {
					continue
				}
				break
			}
			return c, nil
		}
	}
	return nil, x11DialError(display, failures, withHint)
}

// x11CredentialNote records that several Xauthority credentials existed for the
// display, so a reader of the error knows the failure is not "the one cookie was
// wrong" but "every candidate was refused".
func x11CredentialNote(n int) string {
	if n <= 1 {
		return ""
	}
	return fmt.Sprintf(" (%d Xauthority credentials for this display number were offered in turn)", n)
}

// x11DialError is the ONE wording for "no route to this display answered". It
// names every route that was tried WITH its own failure, so a reader can tell a
// dead socket (ENOENT: the server is not there) from a live server refusing
// (ECONNREFUSED) — and, when this host has live displays, names them as the
// remedy. Rule 15: the failure is loud and actionable, never a bare dial error.
func x11DialError(display string, failures []string, withHint bool) error {
	var b strings.Builder
	fmt.Fprintf(&b, "connect to X display %s: no route answered", display)
	for _, f := range failures {
		b.WriteString("; ")
		b.WriteString(f)
	}
	if withHint {
		if alt := x11LiveDisplayHint(); alt != "" {
			fmt.Fprintf(&b, " (live display(s) on this host: %s — pass --display <D>, or export DISPLAY=<D>)", alt)
		}
	}
	return errors.New(b.String())
}

// x11LiveDisplayHint returns up to x11HintMaxDisplays display names (":N") that
// answer an X11 setup handshake on this host RIGHT NOW, joined for one error
// line, or "" when none does. The unix socket directory is the one place a
// local X server advertises itself; a server that has a socket but refuses the
// handshake is not reachable and is not named.
//
// Best effort BY DESIGN: the hint is a remedy appended to an error that has
// already been decided, so a missing/unreadable directory (or a probe error)
// contributes nothing and can never change the reported failure. That is the
// only reason no error is propagated here.
func x11LiveDisplayHint() string {
	entries, err := os.ReadDir(x11UnixSocketDir)
	if err != nil {
		return ""
	}
	names := make([]string, 0, x11HintMaxDisplays)
	for _, e := range entries {
		num := strings.TrimPrefix(e.Name(), x11UnixSocketPrefix)
		if num == e.Name() || num == "" {
			continue
		}
		if _, err := strconv.Atoi(num); err != nil {
			continue
		}
		name := ":" + num
		if c, err := dialX11Routes(name, false); err == nil {
			c.close()
			names = append(names, name)
		}
		if len(names) >= x11HintMaxDisplays {
			break
		}
	}
	return strings.Join(names, ", ")
}

//stylegate:ignore CDP socket teardown at the end of a capture session
func (c *x11Client) close() { c.conn.Close() }

// request writes one X11 request (single write — requests are atomic).
func (c *x11Client) request(req []byte) error {
	c.seq++
	return c.write(req)
}

func (c *x11Client) write(b []byte) error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(x11IOTimeout)); err != nil {
		return err
	}
	_, err := c.conn.Write(b)
	return err
}

// readReply reads the reply to the request currently being awaited and
// returns its header and payload.
//
// The X11 stream is NOT "one reply per read": interleaved EVENT frames (and,
// on a desynchronised connection, stale replies) share it. The reader
// therefore classifies every frame instead of assuming the next 32 bytes are
// its answer:
//
//   - an EVENT is drained (bounded by x11MaxDrainedEvents) and never returned
//     as an answer — a MappingNotify (queued the moment a client fetches the
//     keyboard mapping) is recorded as a dirty keymap so the cached keycodes
//     are refetched instead of going stale;
//   - a frame whose sequence (header[2:4]) is not the request being awaited is
//     refused — returning it would answer a different request;
//   - an X11 error is turned into a Go error (Rule 15: never swallowed).
func (c *x11Client) readReply(reqName string) ([]byte, []byte, error) {
	if err := c.conn.SetReadDeadline(time.Now().Add(x11IOTimeout)); err != nil {
		return nil, nil, err
	}
	for drained := 0; ; drained++ {
		if drained > x11MaxDrainedEvents {
			return nil, nil, fmt.Errorf("%s: %d stray event frames drained without reaching the reply — the X11 connection is desynchronised", reqName, x11MaxDrainedEvents)
		}
		header := make([]byte, x11ReplyHeaderLen)
		if _, err := ioReadFull(c.conn, header); err != nil {
			return nil, nil, fmt.Errorf("%s: read reply: %w", reqName, err)
		}
		frameSeq := binary.NativeEndian.Uint16(header[2:4])
		switch header[0] & x11EventTypeMasked {
		case x11ErrorType:
			// Errors carry the sequence of the request that caused them; a
			// stale error belongs to an already-answered request, so it is
			// drained rather than blamed on the caller.
			if frameSeq != c.seq {
				continue
			}
			// The header read above is x11ReplyHeaderLen (32) bytes, which is the
			// WHOLE error frame — an X11 error carries nothing after it, so the
			// exchange is drained by the read that found it and the next reply is
			// framed correctly without a second read here.
			return nil, nil, NewX11Error(reqName, header[1], frameSeq)
		case x11ReplyType:
			if frameSeq != c.seq {
				return nil, nil, fmt.Errorf("%s: reply carries sequence %d but request %d is awaited — the X11 stream is desynchronised", reqName, frameSeq, c.seq)
			}
			extra := int(binary.NativeEndian.Uint32(header[4:8])) * 4
			if extra == 0 {
				return header, nil, nil
			}
			payload := make([]byte, extra)
			if _, err := ioReadFull(c.conn, payload); err != nil {
				return nil, nil, fmt.Errorf("%s: read %d-byte payload: %w", reqName, extra, err)
			}
			return header, payload, nil
		default: // >= x11EventTypeBase: an event frame
			c.noteEvent(header)
			continue
		}
	}
}

// noteEvent records a drained event frame. A MappingNotify for the MODIFIER
// or KEYBOARD mapping invalidates the keyboard mapping this client's callers
// cache, so it is flagged dirty (xactuate.go refetches on the next keycode
// resolution — a stale keycode table types the wrong characters silently).
func (c *x11Client) noteEvent(header []byte) {
	if header[0]&x11EventTypeMasked != x11EventTypeMappingNotify {
		return
	}
	switch header[1] {
	case x11MappingModifier, x11MappingKeyboard:
		c.keymapDirty = true
	}
}

// handshake performs the X11 connection setup exchange with ONE credential and
// captures the root window geometry from the setup reply's screen 0.
//
// A refusal is returned as *x11SetupRefusalError carrying the SERVER's own
// reason string and the credential that was offered — the caller then knows
// whether offering a DIFFERENT Xauthority entry can help (it can, on an auth
// refusal) or whether the display is simply not answerable (it cannot).
//
//nolint:name-ok // X11 protocol handshake step, mirrors the wire spec's name ("Handshake").
func (c *x11Client) handshake(display string, d x11Display, cred x11Credential) error {
	authName, authData := cred.authName, cred.data
	req := make([]byte, x11SetupRequestLen)
	req[0] = nativeByteOrder()
	req[1] = 0
	binary.NativeEndian.PutUint16(req[2:], x11ProtocolMajor)
	binary.NativeEndian.PutUint16(req[4:], x11ProtocolMinor)
	binary.NativeEndian.PutUint16(req[6:], uint16(len(authName)))
	binary.NativeEndian.PutUint16(req[8:], uint16(len(authData)))
	if err := c.write(req); err != nil {
		return fmt.Errorf("setup request: %w", err)
	}
	if len(authName) > 0 {
		pad := pad4(len(authName))
		buf := make([]byte, len(authName)+pad+len(authData)+pad4(len(authData)))
		copy(buf, authName)
		copy(buf[len(authName)+pad:], authData)
		if err := c.write(buf); err != nil {
			return fmt.Errorf("setup auth: %w", err)
		}
	}

	head := make([]byte, 8)
	if err := c.conn.SetReadDeadline(time.Now().Add(x11IOTimeout)); err != nil {
		return err
	}
	if _, err := ioReadFull(c.conn, head); err != nil {
		return fmt.Errorf("setup reply: %w", err)
	}
	if head[0] != x11SetupReplySuccess {
		return &x11SetupRefusalError{
			display:  display,
			number:   d.number,
			status:   head[0],
			cred:     cred,
			reason:   c.readSetupFailureReason(head),
			offered:  x11CredentialLabel(cred, d.number),
			hadCreds: cred.authName != "",
		}
	}
	body := make([]byte, int(binary.NativeEndian.Uint16(head[6:8]))*4)
	if _, err := ioReadFull(c.conn, body); err != nil {
		return fmt.Errorf("setup body: %w", err)
	}
	return c.parseSetup(display, body)
}

// readSetupFailureReason reads the reason string a REFUSED connection-setup
// reply carries ("wrong authentication", "Server is already active for display
// N", …). It is the server's own diagnosis and it is the ONE piece of evidence
// that tells an operator WHICH of several Xauthority entries was the wrong one,
// so it is read rather than replaced with a guess. A server that sends no
// reason (the reply ends at the 8-byte prefix) yields "".
func (c *x11Client) readSetupFailureReason(head []byte) string {
	reasonLen := int(head[x11SetupReplyReasonOffset])
	if reasonLen == 0 {
		return ""
	}
	// After the 8-byte prefix the reply carries its reason string padded to a
	// 4-byte boundary, then any additional auth-protocol data (unread).
	buf := make([]byte, reasonLen+pad4(reasonLen))
	if err := c.conn.SetReadDeadline(time.Now().Add(x11IOTimeout)); err != nil {
		return ""
	}
	if _, err := ioReadFull(c.conn, buf); err != nil {
		return "" // an unreadable reason must not mask the refusal itself
	}
	return string(buf[:reasonLen])
}

// x11SetupRefusalError is the server REFUSING a connection setup (reply status
// 0) or asking for a different auth protocol (status 2). It is a distinct type
// because it is the ONE failure a different Xauthority credential can fix: the
// dial loop tries the next candidate for this error and for no other (a
// byte-order or protocol error is not a credential problem, and re-dialling
// would only multiply the work).
type x11SetupRefusalError struct {
	display string
	number  int
	// status is the setup reply's status byte; reason is the server's own
	// reason string; offered labels the credential that was refused.
	status  byte
	reason  string
	offered string
	cred    x11Credential
	// hadCreds records whether the refused attempt carried a credential at all
	// (a server may refuse an unauthenticated local client).
	hadCreds bool
}

func (e *x11SetupRefusalError) Error() string {
	reason := e.reason
	if reason == "" {
		reason = "no reason given by the server"
	}
	if e.status == x11SetupReplyAuthenticate {
		reason = "the server wants a different auth protocol"
	}
	offered := e.offered
	if !e.hadCreds {
		offered = "no credential"
	}
	return fmt.Sprintf("X display %s refused the connection: %s (offered %s)", e.display, reason, offered)
}

// parseSetup reads the fields of the connection-setup reply we care about:
// release, max request length, image byte order, and screen 0's root geometry.
func (c *x11Client) parseSetup(display string, body []byte) error {
	// fixed is the size of the setup reply's fixed part AFTER the 8-byte reply
	// prefix: release..max-keycode (28 bytes) + 4 bytes of pad, i.e. the vendor
	// string starts at body[32] (byte-verified against the live :99 reply —
	// "The X.Org Foundation" is exactly there).
	const fixed = 32
	if len(body) < fixed {
		return fmt.Errorf("setup body too short (%d bytes)", len(body))
	}
	// Connection-setup reply layout (X11 protocol, AFTER the 8-byte reply
	// prefix), field by field — every offset below is load-bearing and was
	// verified byte-for-byte against a live Xvfb :99 reply:
	//   0  release                        4  resource-id-base
	//   8  resource-id-mask              12  motion-buffer-size
	//  16  length-of-vendor (2)          18  maximum-request-length (2, in
	//                                         4-byte units)
	//  20  number-of-SCREENS             21  number-of-FORMATS
	//  22  image-byte-order              23  bitmap-format-bit-order
	//  24  bitmap-format-scanline-unit   25  bitmap-format-scanline-pad
	//  26  min-keycode                   27  max-keycode
	//  28  pad(4)                        32  vendor string, then the 8-byte
	//                                          formats, then the screens.
	// Live check (Xvfb :99): release 0x00b8a58c = 12101004, vendorLen 0x14 =
	// 20 ("The X.Org Foundation"), maxReq 0xffff, nScreens 1, nFormats 6,
	// imageByteOrder 0 (LSBFirst).
	// A first revision of this parser read length-of-vendor from byte 14 and
	// number-of-FORMATS from byte 20 (which is number-of-SCREENS) — a 2-byte
	// shift that put the screen table ~900 bytes off, so every GetImage
	// answered an X error instead of an image. Keep this map next to the
	// struct; it is the one place this client can silently lie.
	c.release = int(binary.NativeEndian.Uint32(body[0:4]))
	vendorLen := int(binary.NativeEndian.Uint16(body[16:18]))
	c.maxReq = int(binary.NativeEndian.Uint16(body[18:20])) * 4
	nScreens := int(body[20])
	nFormats := int(body[21])
	c.imgMSB = body[22] == x11ByteOrderMSBFirst
	if c.imgMSB != (nativeByteOrder() == x11ByteOrderMSBFirst) {
		return fmt.Errorf("display %s uses the opposite image byte order — this client only handles native-endian X servers", display)
	}
	off := fixed + pad4(vendorLen) + vendorLen
	// Skip the formats (8 bytes each) to reach the first screen.
	off += 8 * nFormats
	if len(body) < off+40 {
		return fmt.Errorf("setup body truncated before screen 0 (%d < %d)", len(body), off+40)
	}
	s := body[off : off+40]
	// xWindowRoot layout (X11 protocol, connection setup): root window (0),
	// default colormap (4), white pixel (8), black pixel (12),
	// current input masks (16), width in pixels (20), height in pixels (22),
	// width in mm (24), height in mm (26), min/max installed maps (28),
	// root visual (32), backing store (36), save unders (37),
	// root depth (38), number of allowed depths (39).
	c.root = binary.NativeEndian.Uint32(s[0:4])
	c.width = int(binary.NativeEndian.Uint16(s[20:22]))
	c.height = int(binary.NativeEndian.Uint16(s[22:24]))
	c.depth = int(s[38])
	c.bpp = x11BitsPerPixel4 * 8
	if nScreens == 0 {
		return fmt.Errorf("display %s reports no screens", display)
	}
	if c.depth != 24 && c.depth != 30 && c.depth != 32 {
		return fmt.Errorf("display %s root depth %d unsupported (want 24/30/32 — this client reads 32bpp ZPixmap); parsed root=0x%x %dx%d", display, c.depth, c.root, c.width, c.height)
	}
	if c.width <= 0 || c.height <= 0 {
		return fmt.Errorf("display %s reports a %dx%d root window", display, c.width, c.height)
	}
	return nil
}

// queryExtension returns the major opcode of a named extension (0 when the
// server does not provide it). Used by the actuation prototype's readiness
// check and the capture diagnostics (XTEST availability).
func (c *x11Client) queryExtension(name string) (byte, error) {
	pad := pad4(len(name))
	req := make([]byte, 8+len(name)+pad)
	req[0] = x11OpQueryExtension
	binary.NativeEndian.PutUint16(req[2:], uint16(len(req)/4))
	binary.NativeEndian.PutUint16(req[4:], uint16(len(name)))
	copy(req[8:], name)
	if err := c.request(req); err != nil {
		return 0, err
	}
	header, _, err := c.readReply("QueryExtension " + name)
	if err != nil {
		return 0, err
	}
	if header[8] == 0 {
		return 0, nil
	}
	op := header[9]
	c.extOp[name] = op
	return op, nil
}

// getRootImage grabs the whole root window in horizontal bands and returns it
// as an RGBA image (plus the band count, for the report).
// name-ok: X11 protocol step (GetImage), mirrors the wire spec's name.
func (c *x11Client) getRootImage() (*image.RGBA, int, error) {
	img := image.NewRGBA(image.Rect(0, 0, c.width, c.height))
	bands := 0
	for y := 0; y < c.height; y += x11BandRows {
		h := x11BandRows
		if y+h > c.height {
			h = c.height - y
		}
		if err := c.getImageBand(img, y, h); err != nil {
			return nil, bands, err
		}
		bands++
	}
	return img, bands, nil
}

// getImageBand requests `h` scanlines starting at y and copies them into img.
func (c *x11Client) getImageBand(img *image.RGBA, y, h int) error {
	req := make([]byte, x11GetImageRequestLen*4)
	req[0] = x11OpGetImage
	req[1] = x11ImageFormatZPixmap
	binary.NativeEndian.PutUint16(req[2:], x11GetImageRequestLen)
	binary.NativeEndian.PutUint32(req[4:], c.root)
	binary.NativeEndian.PutUint16(req[8:], 0)          // x
	binary.NativeEndian.PutUint16(req[10:], uint16(y)) // y
	binary.NativeEndian.PutUint16(req[12:], uint16(c.width))
	binary.NativeEndian.PutUint16(req[14:], uint16(h))
	binary.NativeEndian.PutUint32(req[16:], 0xffffffff) // plane-mask: all planes
	if err := c.request(req); err != nil {
		return err
	}
	_, data, err := c.readReply("GetImage")
	if err != nil {
		return err
	}
	stride := c.width * x11BitsPerPixel4
	want := stride * h
	if len(data) < want {
		return fmt.Errorf("GetImage returned %d bytes, want %d (band y=%d h=%d) — the server truncated the reply", len(data), want, y, h)
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

// x11Credential is ONE MIT-MAGIC-COOKIE-1 entry of the Xauthority file: the
// credential to offer, the file's own address spelling it came from (the
// "display" field — a hostname for a Local entry, an empty string for an
// Internet one) and its family. The credential is tried in priority order (see
// readXAuthority) and the spelling is what makes a substituted credential
// visible in the result and the error.
type x11Credential struct {
	authName string
	data     []byte
	address  string
	family   int
}

// x11CredentialLabel names the entry the way xauth(1) lists it, so a reader can
// find the accepted credential in their own file: "addr:number" for a Local
// entry, ":number" for an Internet one.
func x11CredentialLabel(c x11Credential, number int) string {
	return fmt.Sprintf("%s:%d", c.address, number)
}

// readXAuthorityEntries returns the MIT-MAGIC-COOKIE-1 credentials for the
// display NUMBER, in the order they should be TRIED.
//
// Priority order is what this function is about. An Xauthority file legitimately
// carries SEVERAL entries for one display number — the same number is forwarded
// to this host under different client-side address spellings (a per-session
// `localhost:N` entry injected by sshd's xauth, plus whatever spelling a real
// client host used: on this host `host-b:N`, `peer1:N`, `localhost:N`
// and an empty Internet-name entry all coexist). Only the SERVER knows which
// cookie its forward demon holds, so the client must offer them in the most
// likely order:
//
//  1. the address spellings of THIS host (its short hostname and the
//     fully-qualified name the Xauthority file itself may carry), Local family
//     first — the cookie of a display that lives on this machine;
//  2. the `localhost:N` spelling sshd's own xauth add writes for every forward;
//  3. every other Local entry with the same number (a foreign client host's
//     spelling — tried LAST, because it usually belongs to someone else's
//     session);
//  4. the Internet-family entries (empty address) as a final fallback.
//
// Reading the first matching entry instead of trying them is what shipped the
// "X11 connection rejected because of wrong authentication" defect: the file
// happened to list a foreign `host-b:11` entry first, so a live,
// answerable forward on localhost:11 was refused on a correct display. Every
// candidate is cheap (one setup exchange), so the search is bounded by
// x11AuthMaxCandidates and the connection still fails loudly when none is
// accepted (Rule 15).
//
// A missing file or no entry for the number yields no credentials — the
// connection then succeeds only if the server accepts unauthenticated local
// clients (the caller gets an actionable error otherwise, never a silent
// fallback).
func readXAuthorityEntries(d x11Display) []x11Credential {
	path := strings.TrimSpace(os.Getenv(x11EnvAuthFile))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(home, x11AuthDotFile)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	num := strconv.Itoa(d.number)
	var local, localhost, internet []x11Credential
	for off := 0; off+2 <= len(raw); {
		family := int(binary.BigEndian.Uint16(raw[off:]))
		off += 2
		fields := make([][]byte, 4)
		ok := true
		for i := 0; i < 4; i++ {
			if off+2 > len(raw) {
				ok = false
				break
			}
			n := int(binary.BigEndian.Uint16(raw[off:]))
			off += 2
			if off+n > len(raw) {
				ok = false
				break
			}
			fields[i] = raw[off : off+n]
			off += n
		}
		if !ok {
			break
		}
		_, number, name, data := fields[0], fields[1], fields[2], fields[3]
		if string(name) != x11AuthCookieName || string(number) != num {
			continue
		}
		c := x11Credential{authName: string(name), data: data, address: string(fields[0]), family: family}
		switch {
		case family == x11FamilyInternet:
			internet = append(internet, c)
		case c.address == x11HostLocalhost:
			localhost = append(localhost, c)
		default:
			local = append(local, c)
		}
	}
	// Priority inside the Local group: this host's own spellings first, then
	// the rest (a foreign client host's entries) in file order.
	self := local[:0:0]
	other := make([]x11Credential, 0, len(local))
	thisHost := x11HostAliases(d)
	for _, c := range local {
		if _, ok := thisHost[strings.ToLower(c.address)]; ok {
			self = append(self, c)
			continue
		}
		other = append(other, c)
	}
	out := make([]x11Credential, 0, len(self)+len(localhost)+len(other)+len(internet))
	out = append(out, self...)
	out = append(out, localhost...)
	out = append(out, other...)
	out = append(out, internet...)
	if len(out) > x11AuthMaxCandidates {
		out = out[:x11AuthMaxCandidates]
	}
	return out
}

// x11HostAliases returns the lower-cased names that mean THIS host in an
// Xauthority address field. os.Hostname answers the short name and the short
// name is what xauth writes, but a file may carry the FQDN too, so both the
// short name and its first dot-separated label are accepted. The display's own
// host is included when it names this machine — a remote host's name never
// qualifies.
func x11HostAliases(d x11Display) map[string]struct{} {
	aliases := make(map[string]struct{}, 4)
	add := func(h string) {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" || h == x11HostUnix {
			return
		}
		aliases[h] = struct{}{}
	}
	if hn, err := os.Hostname(); err == nil {
		add(hn)
		if i := strings.IndexByte(hn, '.'); i > 0 {
			add(hn[:i])
		}
	}
	switch d.host {
	case "", x11HostLocalhost, x11HostLoopback:
		add(x11HostLocalhost)
	}
	return aliases
}

// readXAuthority returns the credential this client offers first, kept for the
// callers that want the single most likely one. It is the head of
// readXAuthorityEntries' priority order — the same single source of truth, not
// a second lookup.
func readXAuthority(d x11Display) (string, []byte) {
	if cs := readXAuthorityEntries(d); len(cs) > 0 {
		return cs[0].authName, cs[0].data
	}
	return "", nil
}

// nativeByteOrder returns this host's X11 setup byte-order code.
func nativeByteOrder() byte {
	var b [2]byte
	binary.NativeEndian.PutUint16(b[:], 1)
	if b[0] == 1 {
		return x11ByteOrderLSBFirst
	}
	return x11ByteOrderMSBFirst
}

// pad4 returns the padding needed to round n up to a 4-byte boundary.
func pad4(n int) int { return (4 - n%4) % 4 }

// ioReadFull fills buf from r, propagating short reads as an error.
func ioReadFull(r net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if total > 0 {
				return total, fmt.Errorf("short read: got %d of %d bytes: %w", total, len(buf), err)
			}
			return total, err
		}
	}
	return total, nil
}

// Geometry is the EXPORTED name for the capture geometry a caller receives.
// It is a type alias, not a new struct: the value is built and filled by the
// capture code (internal/shot), and a caller outside this package needs to be
// able to NAME the parameter type when it passes the geometry on (Rule 14 —
// one definition). The alias adds a name, never a second shape, so the fields
// the tool result is built from cannot drift from the fields the capture sets.
type Geometry = x11Geometry
