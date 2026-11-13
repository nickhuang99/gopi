// selfaddress.go — ADDRESSING: point the input at the window that HOSTS this
// process, instead of at whatever the user last clicked.
//
// The resolution itself lives in pidwindow.go and is NOT re-implemented here
// (the epic's constraint: one implementation, no second copy of the chain).
// What this file adds is the second half of "--self": once the hosting window
// is KNOWN, put the input focus there EXPLICITLY, because knowing the window is
// not the same as typing into it.
//
// WHY THE FOCUS LEG EXISTS AT ALL — the measured reason. XTEST injects events
// "as if a real device produced them", so a faked key follows the SAME routing
// a real keypress would: it goes to the window that holds the input focus. On a
// desktop with a window manager that is the window the user clicked; on a bare
// Xvfb (no WM) the focus is PointerRoot — a constant meaning "whoever the
// pointer is over". So "type into MY window" needs one of two things to be
// true, and neither was expressed before this file:
//
//	leg A — XSetInputFocus(that window): the server routes keys there, and the
//	        focus can be READ BACK (GetInputFocus) to prove it took.
//	leg B — the pointer is inside that window's rectangle: with PointerRoot
//	        focus, the window under the pointer receives the keys.
//
// The order is try-A-then-B, and BOTH are attempted (never one silently
// substituted for the other): leg A is the precise statement and it is what a
// supporting server records as the focus; leg B is the one that works where the
// server ignores a focus request (PointerRoot is common), and a reader can see
// from the report which of the two the run actually relied on.
//
// WHAT IS *NOT* HERE: a fallback to _NET_ACTIVE_WINDOW. That is the defect this
// whole path exists to remove (see pidwindow.go's header): "the active window"
// is a different question with a different answer, and answering the wrong
// question is worse than refusing. When the hosting window cannot be resolved,
// the ONE refusal path (pidwindow.go's noOwnWindowError) names every leg, why
// each failed, and what the caller can do — it never guesses a window.

package shot

import (
	"encoding/binary"
	"fmt"
)

const (
	// x11OpSetInputFocus is the core protocol's SetInputFocus request (opcode
	// 42; X11 protocol "SetInputFocus": CARD8 opcode, CARD8 revert-to,
	// CARD16 length, WINDOW focus, Time time — 12 bytes, so length = 3).
	x11OpSetInputFocus = 42
	// x11SetInputFocusWords is that request's length field in 4-byte units.
	x11SetInputFocusWords = 3

	// xFocusRevertToParent is the revert-to value (X.h: RevertToParent = 1):
	// when the focused window goes away, the focus returns to its parent
	// rather than being dropped into limbo — the choice that leaves a live
	// server usable if OUR window dies mid-run.
	xFocusRevertToParent = 1

	// x11SelfFocusMargin is how far inside the hosting window's rectangle leg B
	// puts the pointer. A window border/corner can belong to a frame rather
	// than the client, and the topmost row may be a title bar, so the pointer
	// is placed a margin in from the top-left — deliberately NOT at the exact
	// origin.
	x11SelfFocusMargin = 8

	// selfFocusModeExplicit / selfFocusModePointer are the two mode words the
	// JSON evidence carries. They are named constants because the CLI, the
	// tests and the devdoc all quote them (Rule 1b), and because a reader must
	// be able to tell WHICH leg the run depended on — the alternative is the
	// silent-choice defect this file's header rejects.
	selfFocusModeExplicit = "explicit XSetInputFocus"
	selfFocusModePointer  = "pointer-position (XTest motion; XSetInputFocus was not honoured)"
)

// selfWindow is one resolution of "the window that hosts this process": the
// window id, its rectangle in root coordinates, and the route label naming
// every step of the chain that produced it.
//
// It is a struct rather than three return values so the CLI, the voice filler
// and the tests all carry the same three facts together — a label separated
// from its window is exactly how a report ends up describing a different window
// than the one addressed.
type selfWindow struct {
	Window uint32
	Rect   Rect
	Label  string
}

// selfWindowForThisProcess resolves the hosting window of THIS process: the
// single call site of pidwindow.go's chain for an ADDRESSING caller (the
// capture path keeps its own call — the chain is shared, not the mode word).
func (c *x11Client) selfWindowForThisProcess() (selfWindow, error) {
	r, window, label, err := c.windowRectFor(false)
	if err != nil {
		return selfWindow{}, err
	}
	return selfWindow{Window: window, Rect: r, Label: label}, nil
}

// setInputFocus sends SetInputFocus(window) and then reads the focus BACK from
// the server. The readback is the point: the request has no reply of its own,
// so a client that trusted the write would report a focus it never obtained
// (the server is allowed to ignore it — see selfFocusModePointer).
func (c *x11Client) setInputFocus(window uint32) (uint32, error) {
	req := make([]byte, x11SetInputFocusWords*4)
	req[0] = x11OpSetInputFocus
	req[1] = xFocusRevertToParent
	binary.NativeEndian.PutUint16(req[2:], x11SetInputFocusWords)
	binary.NativeEndian.PutUint32(req[4:], window)
	binary.NativeEndian.PutUint32(req[8:], 0) // CurrentTime
	if err := c.request(req); err != nil {
		return 0, fmt.Errorf("XSetInputFocus(0x%x): %w", window, err)
	}
	return c.inputFocus()
}

// pointInside returns a coordinate comfortably inside r: the margin in from the
// top-left corner, or the centre when the window is too small to admit a margin
// (a 10x10 window would otherwise get a coordinate outside itself — the
// pointer would land on whatever is beneath, which is the exact mis-targeting
// this path exists to prevent).
func pointInside(r Rect) (int, int) {
	if r.W > 2*x11SelfFocusMargin && r.H > 2*x11SelfFocusMargin {
		return r.X + x11SelfFocusMargin, r.Y + x11SelfFocusMargin
	}
	return r.X + r.W/2, r.Y + r.H/2
}

// SelfFocus is the machine-readable outcome of addressing this process's own
// window: what was resolved, how the focus was taken, and what the server
// reported afterwards.
type SelfFocus struct {
	// Window is the resolved hosting window and Label is the route that
	// produced it (both from pidwindow.go's chain, unmodified).
	Window uint32
	Label  string
	Rect   Rect
	// Display is the display the resolution happened on, and RootW/RootH its
	// root geometry — carried here so a --dry-run report has the same shape as
	// a real run's without opening a second connection.
	Display string
	RootW   int
	RootH   int
	// Mode names the leg the run RELIED ON (selfFocusModeExplicit when the
	// server accepted XSetInputFocus, selfFocusModePointer when it did not and
	// the pointer was placed inside the rectangle instead).
	Mode string
	// FocusBefore / FocusAfter are the server's own GetInputFocus answers
	// around the request — the evidence, not our intent.
	FocusBefore uint32
	FocusAfter  uint32
	// PointerMoved is true when leg B warped the pointer (always attempted:
	// with PointerRoot focus it is the leg that makes the typing land).
	PointerMoved bool
	// Pointer is the pointer position read back after the move ("x,y"),
	// empty when no move was made.
	Pointer string
}

// SelfAddressSymbol is the ONE spelling of the addressing flag's symbol: the
// CLI's parser, its membership check, its help text and the voice_input flag all
// reference this constant (Rule 1b/14) — the same value the capture verb already
// uses, so `--self` means one thing across the CLI.
const SelfAddressSymbol = "self"

// ResolveSelfWindow resolves the hosting window of this process WITHOUT
// touching the input at all: no focus request, no pointer move, no injected
// event. It is what `--dry-run` needs — the resolved window, its rectangle and
// the route label, with the display opened read-only in effect (a connection is
// still required: the window list lives on the server).
func ResolveSelfWindow(display string) (SelfFocus, error) {
	var out SelfFocus
	a, err := OpenXActuator(display)
	if err != nil {
		return out, err
	}
	defer a.Close()
	a.mu.Lock()
	defer a.mu.Unlock()
	res, err := a.c.selfWindowForThisProcess()
	if err != nil {
		return out, err
	}
	out.Window, out.Label, out.Rect = res.Window, res.Label, res.Rect
	out.Display, out.RootW, out.RootH = a.c.connectedDisplay, a.c.width, a.c.height
	return out, nil
}

// TypableText reports whether every rune of text can be pressed by this
// client's key path, WITHOUT opening a display: it runs the same keysym table
// TypeText uses (keysymForRune), so a caller can refuse untypeable text before
// spending anything on it — the voice path uses it to check a transcript before
// it opens the microphone.
//
// It is a WIDER promise than "the server has a keycode for it" (that needs a
// connection and is checked at injection time); it is the pure half — the
// printable-ASCII + Tab + newline set. A refusal names the offending rune, so
// the reader learns which character broke it.
func TypableText(text string) error {
	for _, r := range text {
		if _, _, err := keysymForRune(r); err != nil {
			return err
		}
	}
	return nil
}

// TypeIntoSelfWindow is the WHOLE voice_input --self operation in one call:
// resolve the window that hosts this process, take the input there, then type
// text verbatim.
//
// VERBATIM is the point: unlike `gopi tool act type --text`, nothing here decodes
// escape sequences. A transcript is arbitrary text a speech model produced, and
// a backslash in it is a character to type, not the start of a CLI escape — the
// shell-facing decoder belongs to the flag grammar, not to this path.
func (a *XActuator) TypeIntoSelfWindow(text string) (SelfFocus, error) {
	sf, err := a.FocusSelf()
	if err != nil {
		return sf, err
	}
	if err := a.TypeText(text); err != nil {
		return sf, err
	}
	return sf, nil
}

// FocusSelf resolves the window that hosts this process and points the input at
// it: XSetInputFocus first, then — always — the pointer inside the window's
// rectangle, then a readback of both the focus and the pointer. It returns the
// resolution described above, or the ONE refusal path's error (pidwindow.go),
// so a caller never has to guess which leg failed.
//
// It injects no key or button event: it only moves the POINTER and sets the
// FOCUS, which is why `--dry-run` can afford to... no — it is why the CLI asks
// for it only when it is about to type, and why the dry run performs the
// resolution WITHOUT calling this. See cmd/agent/cli_act.go.
func (a *XActuator) FocusSelf() (SelfFocus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out SelfFocus
	res, err := a.c.selfWindowForThisProcess()
	if err != nil {
		return out, err
	}
	out.Window, out.Label, out.Rect = res.Window, res.Label, res.Rect
	out.Display, out.RootW, out.RootH = a.c.connectedDisplay, a.c.width, a.c.height

	if before, err := a.c.inputFocus(); err == nil {
		out.FocusBefore = before
	} else {
		// A server that will not answer GetInputFocus is not a reason to
		// refuse the address: the focus readback is EVIDENCE, and its absence
		// is reported as 0x0 rather than fabricated.
		out.FocusBefore = 0
	}
	after, err := a.c.setInputFocus(res.Window)
	if err != nil {
		return out, err
	}
	out.FocusAfter = after

	x, y := pointInside(res.Rect)
	if x < 0 || y < 0 || x >= a.c.width || y >= a.c.height {
		return out, fmt.Errorf("window 0x%x at %s lies outside the %dx%d root window, so its interior cannot be pointed at",
			res.Window, res.Rect, a.c.width, a.c.height)
	}
	if err := a.fakeInput(xEventMotionNotify, 0, x, y, true); err != nil {
		return out, err
	}
	if err := a.c.sync(); err != nil {
		return out, err
	}
	out.PointerMoved = true
	out.Pointer = fmt.Sprintf("%d,%d", x, y)

	// WHICH LEG WON, stated rather than assumed: the server's own readback
	// decides. It equals the window → the explicit focus took. It does not
	// (PointerRoot is 0x1, None is 0x0) → this run is relying on the pointer
	// position, and says so.
	if out.FocusAfter == res.Window {
		out.Mode = selfFocusModeExplicit
	} else {
		out.Mode = selfFocusModePointer
	}
	return out, nil
}
