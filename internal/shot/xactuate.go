// xactuate.go — the XTEST actuation path: move the pointer, click, and type
// keystrokes into the LIVE X11 desktop, from pure Go with no CGO and no
// external binary.
//
// XTEST is the X extension that lets a client inject input events into the
// server as if a real input device produced them — it is what xdotool uses
// (see devdoc/screen-capture-and-actuation-tools.md §3 for the API-level
// evidence: XTest.h / xtestproto.h / the xdotool man page's own statements).
// libXtst's XTestFakeMotionEvent / XTestFakeButtonEvent / XTestFakeKeyEvent
// are thin wrappers over ONE X request, X_XTestFakeInput (opcode 1 in the
// extension's minor-opcode space); we emit that request directly.
//
// Wire form (from /usr/include/X11/extensions/xtestproto.h, quoted in §3):
//
//	struct {
//	    CARD8  reqType;        // the XTEST major opcode from QueryExtension
//	    CARD8  xtReqType;      // X_XTestFakeInput == 2
//	    CARD16 length;         // 4 (16 bytes / 4)
//	    CARD8  type;           // event type to fake: KeyPress/KeyRelease/…
//	    CARD8  detail;         // keycode or button number
//	    CARD16 pad;
//	    CARD32 time;           // CurrentTime == 0
//	    CARD32 rootX;          // root coordinates (motion)
//	    CARD32 rootY;
//	    CARD32 pad1;
//	    CARD32 pad2;
//	}
//
// Keystroke translation (keysym → keycode) needs the server's keyboard
// mapping: the GetKeyboardMapping request (opcode 101) returns the
// keysyms-per-keycode table, from which we find the keycode whose first
// column equals the keysym and press it with the shift modifier when the
// keysym only appears in a shifted column.
package shot

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// X11 request constants for actuation (Rule 1: named, never inline).
// ---------------------------------------------------------------------------

const (
	// xtestOpExtensionName is the XTEST extension's wire name (xtestconst.h
	// XTestExtensionName). Named, never inline (Rule 1).
	xtestOpExtensionName = "XTEST"

	// XTEST minor opcodes (xtestproto.h X_XTest*).
	xtestOpGetVersion  = 0
	xtestOpCompareCur  = 1
	xtestOpFakeInput   = 2
	xtestOpGrabControl = 3
	// xtestFakeInputWords is the FakeInput request length in 4-byte units:
	// sz_xXTestFakeInputReq == 36 bytes -> 9 units. (An earlier revision used
	// 4; the request was then silently malformed and every injected event was
	// dropped by the server. The header sizing macro is the authority.)
	xtestFakeInputWords = 9

	// X11 core request opcodes used by actuation.
	x11OpGetKeyboardMapping  = 101
	x11OpGetInputFocus       = 43
	x11OpQueryPointer        = 38
	x11GetKeyboardMappingLen = 2 // 8 bytes / 4
	x11QueryPointerWords     = 2 // 8 bytes / 4

	// X11 event type codes faked through XTEST (from X.h: KeyPress = 2,
	// KeyRelease = 3, ButtonPress = 4, ButtonRelease = 5, MotionNotify = 6).
	xEventKeyPress      = 2
	xEventKeyRelease    = 3
	xEventButtonPress   = 4
	xEventButtonRelease = 5
	xEventMotionNotify  = 6

	// keysymShiftL is XK_Shift_L — resolved from the server's own keyboard
	// mapping (never a hardcoded keycode, which differs across layouts).
	keysymShiftL = 0xffe1

	// x11KeysymFirstShifted is the first keysym in the "shifted" range of a
	// key's keysym list (X11 protocol: column 0 is unshifted, column 1 is the
	// shifted/level-2 symbol).
	x11KeysymFirstShifted = 1

	// Keysym base values (X11/keysymdef.h, quoted in the devdoc §3).
	keysymLowerBase  = 0x0061 // 'a'
	keysymUpperBase  = 0x0041 // 'A'
	SystemKeysymKeys = 0xff00 // XK_ prefix base for the function/edit keys
	SystemKeysymMax  = 0xffff

	// Named edit/navigation keysyms the act_* verbs accept, as the LOW byte of
	// their 0xffxx keysym (XK_Return = 0xff0d, XK_Tab = 0xff09, …).
	keysymReturn = 0xff0d
	keysymTab    = 0xff09
	keysymEscape = 0xff1b
	keysymSpace  = 0x0020
	keysymDelete = 0xffff
	keysymHome   = 0xff50
	keysymEnd    = 0xff57
	// The four ARROW keys. They are here because "press the arrows" is the most
	// ordinary thing a driver of this tool has to do — `gopi chat --replay`'s
	// playback controls step and jump with them — and their absence was not a
	// limitation anybody had measured: `gopi tool act key --key left` answered
	// `unknown key "left"`, so a caller could read a picture of the navigation
	// keys in the recorded session and have no way to press one (measured
	// 2026-10-04 while driving the replay's controls through XTEST, task
	// 01a1071b). XK_Left = 0xff51 … XK_Down = 0xff54.
	keysymLeft  = 0xff51
	keysymUp    = 0xff52
	keysymRight = 0xff53
	keysymDown  = 0xff54

	// xActKeyDelay is the settle delay between the press and the release of
	// one synthetic key/button pair: XTEST injects synchronously, so this is
	// only to let the client's own round-trip finish (xdotool uses 12 ms; the
	// value is a named constant per Rule 1).
	xActKeyDelay = 12 * time.Millisecond

	// xActFocusSettle bounds the wait after activating a window before typing.
	xActFocusSettle = 120 * time.Millisecond
)

// XActuator drives one X11 display through XTEST. It is the single owner of
// the X11 connection used for input, so every verb serialises behind mu.
type XActuator struct {
	c    *x11Client
	xt   byte // XTEST major opcode
	maps map[uint32]keycodePos
	mu   sync.Mutex
}

// keycodePos locates one keysym in the server's keyboard mapping.
type keycodePos struct {
	code      byte
	unshifted bool
}

// XActResult is the machine-readable outcome of one actuation (what the model
// must quote instead of assuming the action happened).
type XActResult struct {
	Action  string `json:"action"`
	Display string `json:"display"`
	Detail  string `json:"detail"`
}

// OpenXActuator connects to the display and verifies XTEST availability.
// display == "" → $DISPLAY.
func OpenXActuator(display string) (*XActuator, error) {
	name := captureDisplayName(display)
	if name == "" {
		return nil, fmt.Errorf("no X11 display: %s is unset and no display was given — actuation is X11-only", x11EnvDisplay)
	}
	c, err := dialX11(name)
	if err != nil {
		return nil, err
	}
	xt, err := c.queryExtension(xtestOpExtensionName)
	if err != nil {
		c.close()
		return nil, err
	}
	if xt == 0 {
		c.close()
		return nil, fmt.Errorf("display %s does not provide the %s extension — synthetic input is impossible (Xvfb/Xorg need it compiled in)", name, xtestOpExtensionName)
	}
	return &XActuator{c: c, xt: xt, maps: map[uint32]keycodePos{}}, nil
}

// Close releases the X11 connection. A nil receiver or an actuator without a
// client is already closed — closing is idempotent, never a panic (Rule 15:
// no silent failure; a crash is the worst failure mode).
func (a *XActuator) Close() {
	if a == nil || a.c == nil {
		return
	}
	a.c.close()
}

// Pointer reports the current pointer position in root coordinates, read
// back from the server (not from our own last request) — the VERIFY half of
// the actuation evidence.
func (a *XActuator) Pointer() (int, int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.c.queryPointer()
}

// Focus reports the window that currently holds the input focus; key events
// land there, so "did my keystroke go anywhere" has a truthful answer.
func (a *XActuator) Focus() (uint32, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.c.inputFocus()
}

// RootSize reports the display's root geometry. A nil receiver or an actuator
// without a client reports the zero geometry rather than panicking: these
// accessors are read-only probes whose contract is a value, never a crash.
func (a *XActuator) RootSize() (int, int) {
	if a == nil || a.c == nil {
		return 0, 0
	}
	return a.c.width, a.c.height
}

// Display reports the display name this actuator is bound to. A nil receiver
// (the "never opened" sentinel OpenXActuator returns alongside its error) or
// an actuator without a client reports "" — the receiver is checked BEFORE
// any field is touched, so the documented empty answer is what callers get
// instead of a nil-pointer SIGSEGV.
func (a *XActuator) Display() string {
	if a == nil || a.c == nil {
		return ""
	}
	return a.c.connectedDisplay
}

// fakeInput emits one XTestFakeInput request (36 bytes; offsets straight from
// /usr/include/X11/extensions/xtestproto.h xXTestFakeInputReq — quoted in the
// devdoc §3):
//
//	0  reqType (XTEST major opcode)   1  xtReqType = 2
//	2  length = 9 (36 bytes / 4)      4  type (event to fake)
//	5  detail (keycode / button)      6  pad
//	8  time (CurrentTime = 0)        12  root window
//	16 pad                           20  pad
//	24 rootX (INT16)                 26  rootY (INT16)
//	28 pad  32 pad  34 pad  35 deviceid
//
// For motion events root/rootX/rootY carry the target; for key and button
// events only type + detail matter (root stays 0).
func (a *XActuator) fakeInput(evType, detail byte, x, y int, withCoords bool) error {
	req := make([]byte, xtestFakeInputWords*4)
	req[0] = a.xt
	req[1] = xtestOpFakeInput
	binary.NativeEndian.PutUint16(req[2:], xtestFakeInputWords)
	req[4] = evType
	req[5] = detail
	// time = CurrentTime (0) at [8:12]; deviceid 0 = the core device.
	if withCoords {
		binary.NativeEndian.PutUint32(req[12:], a.c.root)
		binary.NativeEndian.PutUint16(req[24:], uint16(int16(x)))
		binary.NativeEndian.PutUint16(req[26:], uint16(int16(y)))
	}
	if err := a.c.request(req); err != nil {
		return fmt.Errorf("XTestFakeInput(type %d): %w", evType, err)
	}
	return nil
}

// MovePointer warps the pointer to absolute root coordinates (x, y).
func (a *XActuator) MovePointer(x, y int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if x < 0 || y < 0 || x >= a.c.width || y >= a.c.height {
		return fmt.Errorf("pointer target (%d,%d) is outside the %dx%d root window", x, y, a.c.width, a.c.height)
	}
	if err := a.fakeInput(xEventMotionNotify, 0, x, y, true); err != nil {
		return err
	}
	return a.c.sync()
}

// Click presses and releases a mouse button at the current pointer position.
// button follows X11 convention: 1 left, 2 middle, 3 right, 4/5 wheel.
func (a *XActuator) Click(button int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if button < 1 || button > 5 {
		return fmt.Errorf("mouse button %d out of range 1..5", button)
	}
	if err := a.fakeInput(xEventButtonPress, byte(button), 0, 0, false); err != nil {
		return err
	}
	time.Sleep(xActKeyDelay)
	if err := a.fakeInput(xEventButtonRelease, byte(button), 0, 0, false); err != nil {
		return err
	}
	return a.c.sync()
}

// TypeText translates text to keycodes and types it. Only the printable
// ASCII set plus the edit keys handled by Key() are supported; anything else
// is refused LOUDLY rather than typed as mojibake (Rule 15: no silent
// failure) — arbitrary Unicode needs an input-method path, which is an open
// question in the devdoc (§8), not a silent best-effort here.
func (a *XActuator) TypeText(text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range text {
		keysym, shift, err := keysymForRune(r)
		if err != nil {
			return err
		}
		pos, err := a.keycodeForLocked(keysym)
		if err != nil {
			return err
		}
		if err := a.tapLocked(pos.code, shift || !pos.unshifted); err != nil {
			return err
		}
	}
	return a.c.sync()
}

// Key presses one named key (return, tab, escape, space, delete, home, end).
func (a *XActuator) Key(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	keysym, shift, err := keysymForName(name)
	if err != nil {
		return err
	}
	pos, err := a.keycodeForLocked(keysym)
	if err != nil {
		return err
	}
	if err := a.tapLocked(pos.code, shift || !pos.unshifted); err != nil {
		return err
	}
	return a.c.sync()
}

// tapLocked presses then releases one keycode, holding Shift when needed.
// The Shift keycode is resolved from the server's own mapping rather than
// hardcoded (a hardcoded 50 is wrong on many layouts).
func (a *XActuator) tapLocked(code byte, shifted bool) error {
	shiftCode := byte(0)
	if shifted {
		pos, err := a.keycodeForLocked(keysymShiftL)
		if err != nil {
			return fmt.Errorf("shift modifier unavailable: %w", err)
		}
		shiftCode = pos.code
		if err := a.fakeInput(xEventKeyPress, shiftCode, 0, 0, false); err != nil {
			return err
		}
	}
	if err := a.fakeInput(xEventKeyPress, code, 0, 0, false); err != nil {
		return err
	}
	time.Sleep(xActKeyDelay)
	if err := a.fakeInput(xEventKeyRelease, code, 0, 0, false); err != nil {
		return err
	}
	if shifted {
		if err := a.fakeInput(xEventKeyRelease, shiftCode, 0, 0, false); err != nil {
			return err
		}
	}
	return nil
}

// keycodeForLocked resolves a keysym to a keycode, caching the server's
// keyboard mapping on first use.
func (a *XActuator) keycodeForLocked(keysym uint32) (keycodePos, error) {
	if a.c.keymapDirty {
		// The server announced a keyboard-mapping change (a MappingNotify was
		// drained off the reply stream): every cached keycode may name a key
		// that no longer exists. Drop the cache and resolve against a FRESH
		// fetch — a stale keycode types the wrong character silently.
		a.maps = map[uint32]keycodePos{}
	}
	if pos, ok := a.maps[keysym]; ok {
		return pos, nil
	}
	if err := a.loadKeymapLocked(); err != nil {
		return keycodePos{}, err
	}
	pos, ok := a.maps[keysym]
	if !ok {
		return keycodePos{}, fmt.Errorf("keysym 0x%04x is not in the server's keyboard mapping — cannot type it with XTEST (setxkbmap/xmodmap would be needed)", keysym)
	}
	return pos, nil
}

// loadKeymapLocked fetches GetKeyboardMapping for the first 8 keycodes and
// indexes every keysym it finds (unshifted → column 0, shifted → column 1).
func (a *XActuator) loadKeymapLocked() error {
	const (
		firstKeycode = 8
		count        = 248 // 8..255
	)
	req := make([]byte, x11GetKeyboardMappingLen*4)
	req[0] = x11OpGetKeyboardMapping
	binary.NativeEndian.PutUint16(req[2:], x11GetKeyboardMappingLen)
	req[4] = firstKeycode
	req[5] = count
	if err := a.c.request(req); err != nil {
		return err
	}
	header, data, err := a.c.readReply("GetKeyboardMapping")
	if err != nil {
		return err
	}
	perCode := int(header[1]) // keysyms per keycode (n)
	if perCode <= 0 {
		return errors.New("GetKeyboardMapping returned 0 keysyms per keycode")
	}
	// This IS the refresh the MappingNotify signal asked for: the table now in
	// a.maps reflects the server's current mapping, so the dirty flag is
	// cleared here (and only here).
	a.c.keymapDirty = false
	for i := 0; i < count; i++ {
		base := i * perCode * 4
		if base+perCode*4 > len(data) {
			break
		}
		code := byte(firstKeycode + i)
		for col := 0; col < perCode && col <= x11KeysymFirstShifted; col++ {
			ks := binary.NativeEndian.Uint32(data[base+col*4:])
			if ks == 0 {
				continue
			}
			if _, ok := a.maps[ks]; ok {
				continue
			}
			a.maps[ks] = keycodePos{code: code, unshifted: col == 0}
		}
	}
	if len(a.maps) == 0 {
		return errors.New("keyboard mapping is empty — the display reports no keysyms")
	}
	return nil
}

// keysymForRune maps a printable ASCII rune to its keysym plus whether Shift
// is required.
func keysymForRune(r rune) (uint32, bool, error) {
	switch {
	case r >= 'a' && r <= 'z':
		return uint32(r - 'a' + keysymLowerBase), false, nil
	case r >= 'A' && r <= 'Z':
		return uint32(r - 'A' + keysymUpperBase), true, nil
	case r >= '0' && r <= '9':
		return uint32(r), false, nil
	}
	if ks, ok := asciiShiftedKeysyms[r]; ok {
		return ks, true, nil
	}
	if ks, ok := asciiPlainKeysyms[r]; ok {
		return ks, false, nil
	}
	return 0, false, fmt.Errorf("rune %q (U+%04X) has no X11 keysym mapping in this client — ASCII printable and the named keys only; arbitrary Unicode would need an input method (see devdoc §8)", r, r)
}

// keysymForName maps a named key verb to its keysym. The refusal NAMES the whole
// vocabulary, and it is DERIVED from the map rather than written out beside it:
// a hand-kept list is what let the arrows be missing from the verb while their
// names were already carryable (Rule 13 — a second copy of a vocabulary drifts).
func keysymForName(name string) (uint32, bool, error) {
	if ks, ok := namedKeysyms[name]; ok {
		return ks, false, nil
	}
	return 0, false, fmt.Errorf("unknown key %q (want one of: %s)", name, strings.Join(NamedKeyNames(), ", "))
}

// NamedKeyNames is the named-key vocabulary of the `gopi tool act key` verb, sorted —
// the ONE source every refusal and every usage line reads, so a caller is told
// the vocabulary the parser actually holds (Rule 13: a hand-kept second copy is
// how the arrows came to be missing from the verb's own error message).
func NamedKeyNames() []string {
	names := make([]string, 0, len(namedKeysyms))
	for n := range namedKeysyms {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// namedKeysyms is the named-key vocabulary of the `gopi tool act key` verb.
var namedKeysyms = map[string]uint32{
	"return": keysymReturn,
	"tab":    keysymTab,
	"escape": keysymEscape,
	"space":  keysymSpace,
	"delete": keysymDelete,
	"home":   keysymHome,
	"end":    keysymEnd,
	"left":   keysymLeft,
	"up":     keysymUp,
	"right":  keysymRight,
	"down":   keysymDown,
}

// asciiShiftedKeysyms are the printable ASCII characters that sit in column 1
// of a US-layout key (typed with Shift).
var asciiShiftedKeysyms = map[rune]uint32{
	'!': 0x0021, '@': 0x0040, '#': 0x0023, '$': 0x0024, '%': 0x0025,
	'^': 0x005e, '&': 0x0026, '*': 0x002a, '(': 0x0028, ')': 0x0029,
	'_': 0x005f, '+': 0x002b, '{': 0x007b, '}': 0x007d, '|': 0x007c,
	':': 0x003a, '"': 0x0022, '<': 0x003c, '>': 0x003e, '?': 0x003f,
	'~': 0x007e,
}

// asciiPlainKeysyms are the unshifted printable ASCII keysyms.
var asciiPlainKeysyms = map[rune]uint32{
	' ': keysymSpace, '-': 0x002d, '=': 0x003d, '[': 0x005b, ']': 0x005d,
	'\\': 0x005c, ';': 0x003b, '\'': 0x0027, ',': 0x002c, '.': 0x002e,
	'/': 0x002f, '`': 0x0060, '\t': keysymTab, '\n': keysymReturn,
}

// sync performs a real round-trip with the server (GetInputFocus) so every
// request written so far is known to have been PROCESSED before the caller
// captures the screen again. This is what makes capture→act→verify a real
// barrier instead of a race: XTEST injects synchronously into the server, and
// this round-trip proves the server drained the queue. It returns the focused
// window, which is also the honest answer to "did my key land anywhere?".
func (c *x11Client) sync() error {
	_, err := c.inputFocus()
	return err
}

// queryPointer issues QueryPointer and returns the pointer's root
// coordinates — the machine-readable proof that a move/click actually landed
// (never assumed: the caller quotes the number).
func (c *x11Client) queryPointer() (int, int, error) {
	req := make([]byte, x11QueryPointerWords*4)
	req[0] = x11OpQueryPointer
	binary.NativeEndian.PutUint16(req[2:], x11QueryPointerWords)
	binary.NativeEndian.PutUint32(req[4:], c.root)
	if err := c.request(req); err != nil {
		return 0, 0, err
	}
	header, _, err := c.readReply("QueryPointer")
	if err != nil {
		return 0, 0, err
	}
	return int(binary.NativeEndian.Uint16(header[16:18])), int(binary.NativeEndian.Uint16(header[18:20])), nil
}

// inputFocus issues GetInputFocus and returns the focused window id.
func (c *x11Client) inputFocus() (uint32, error) {
	req := make([]byte, 4)
	req[0] = x11OpGetInputFocus
	binary.NativeEndian.PutUint16(req[2:], 1)
	if err := c.request(req); err != nil {
		return 0, err
	}
	header, _, err := c.readReply("GetInputFocus")
	if err != nil {
		return 0, err
	}
	return binary.NativeEndian.Uint32(header[8:12]), nil
}
