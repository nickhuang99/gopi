package shot

// terminalresize.go — RESIZE the filmed terminal's window, so the canvas a run
// DELIVERS is the canvas the surface inside it is drawing.
//
// WHY IT EXISTS (the defect family this closes). The film's own delivery contract
// caps the frame width a phone conversation view accepts (cmd/agent's
// playableGIFMaxWidth), and the cap is applied to the CELL COUNT the surface is
// asked to draw at. That count is now derived from the terminal's MEASURED face
// (terminalface.go) — which is only knowable AFTER the terminal has mapped a
// window. So the order is: start the terminal at the resolved size → read its
// face → decide the deliverable canvas → and then either carry on (the resolved
// size was already deliverable) or move the window to the deliverable one.
//
// The alternative — start the terminal a second time at the new size — costs a
// whole display round trip and re-opens the window-identity question; the
// alternative that needs no move at all is to pass the narrowed canvas to the
// surface, which does not work from here: the producer is a SUBPROCESS in the
// terminal (it owns the pty and re-reads its own size), so a `--cols` the driver
// states for it is a second answer to a question the producer already answers,
// and one that can contradict the pty it is standing on. Moving the window is the
// ONE move that leaves a single source of truth for the size: the server's own
// answer, which the terminal's child re-reads for itself.
//
// WHAT THE TWO SIZES MEAN, because they are NOT the same number and both are
// needed:
//
//	cells    — ConfigureWindow's width/height are in PIXELS on an X11 window, and
//	           the terminal's window manager is the thing that turns pixels back
//	           into a cell count through the resize increment it published. This
//	           method therefore takes CELLS (what the caller decided) and computes
//	           the pixel size from the SAME face it read (base + increment ×
//	           cells), so the window lands on the requested count exactly rather
//	           than one cell off by rounding.
//	face     — the pixel model read from the window BEFORE the move. It stays
//	           valid across the move: a resize changes the count, not the font, so
//	           px-per-cell and the chrome are properties of the client and not of
//	           its current size.
//
// IT DOES NOT GUESS. A face that was never measured cannot be turned into pixels,
// so the resize is REFUSED by name with the reason (Rule 15) — the caller's
// fallback is to leave the window where it is and let the composer's refusal
// state the numbers.
//
// IT IS VERIFIED, NOT ASSUMED. ConfigureWindow is a request; the server's answer
// is a new GetGeometry. The post-condition is checked here — the mapped front
// buffer must be the requested pixel size — so a window manager with a size hint
// of its own (a terminal that refuses an off-increment size, a WM with a
// minimum) is caught and REPORTED rather than photographed at a size nobody asked
// for.

import (
	"encoding/binary"
	"fmt"
)

const (
	// x11OpConfigureWindow is ConfigureWindow (X11/Xproto.h opcode 12). It is
	// checked against the installed header by a test for the same reason
	// x11OpGetGeometry is: a wrong opcode here returns a well-formed reply and a
	// window that did not move.
	x11OpConfigureWindow = 12
	// x11ConfigureWindowRequestLen is the request's size in 4-byte units:
	// opcode(1) | length(2) | window(4) | value-mask(2) | value-list(N).
	x11ConfigureWindowRequestLen = 3
	// x11ConfigMaskWidth and x11ConfigMaskHeight are the two value-list bits this
	// request sets. The mask is what decides how many CARD32s follow it, so a
	// wrong bit would make the server read the value list as something else.
	x11ConfigMaskWidth  = 1 << 2
	x11ConfigMaskHeight = 1 << 3
	// x11ConfigureValueCount is how many values the mask above asks for.
	x11ConfigureValueCount = 2
	// x11ConfigureWindowFixedLen is the request length in 4-byte units with both
	// values present, which is what the header's length field must carry.
	x11ConfigureWindowTotalLen = x11ConfigureWindowRequestLen + x11ConfigureValueCount
)

// Resize moves the filmed terminal's window to the given CELL count, read on the
// face the caller passes, and verifies that the window really is that size
// afterwards. It returns the window's new rectangle (in root coordinates) so the
// caller's frames are grabbed from where the window actually is.
//
// It requires a MEASURED face: without one there is no px-per-cell and no
// chrome, so no pixel size can be computed — refused by name, never guessed.
func (t *FilmTerminal) Resize(face TerminalFace, cols, rows int) (Rect, error) {
	if t == nil || t.Window == 0 {
		return Rect{}, fmt.Errorf("resize the filmed terminal: there is no window to move (the terminal was never started, or never mapped one)")
	}
	if !face.Known() {
		return Rect{}, fmt.Errorf("resize the filmed terminal (window 0x%x) to %dx%d cells: the face is %s, so its pixel size cannot be computed — a resize derived from an unknown cell size would move the window to a size nobody asked for", t.Window, cols, rows, face)
	}
	if cols <= 0 || rows <= 0 {
		return Rect{}, fmt.Errorf("resize the filmed terminal (window 0x%x): %dx%d is not a canvas — the terminal protocol's own refusal would name the same thing", t.Window, cols, rows)
	}
	name := captureDisplayName(t.Display)
	if name == "" {
		return Rect{}, x11ErrNoDisplay()
	}
	c, err := dialX11(name)
	if err != nil {
		return Rect{}, err
	}
	defer c.close()
	wantW, wantH := face.WidthForCols(cols), face.HeightForRows(rows)
	if err := c.configureWindowSize(t.Window, uint32(wantW), uint32(wantH)); err != nil {
		return Rect{}, fmt.Errorf("resize the filmed terminal (window 0x%x) to %dx%d cells = %dx%d px on %s: %w", t.Window, cols, rows, wantW, wantH, face, err)
	}
	// THE SERVER'S ANSWER, not the request's echo: a window manager, a size
	// increment hint or a minimum size can leave the window at another size, and
	// the frames would then be a picture of a canvas nobody asked for.
	got, err := c.windowGeometry(t.Window)
	if err != nil {
		return Rect{}, fmt.Errorf("read back the filmed terminal's size after resizing it to %dx%d px: %w", wantW, wantH, err)
	}
	if got.W != wantW || got.H != wantH {
		return Rect{}, fmt.Errorf("the filmed terminal (window 0x%x) was asked to become %dx%d px (%dx%d cells on %s) but the server reports %dx%d — the window did not take the size, so the frames would be a picture of a canvas this run did not ask for", t.Window, wantW, wantH, cols, rows, face, got.W, got.H)
	}
	// The position is CARRIED OVER rather than re-read: ConfigureWindow did not
	// set x/y (the value mask names only width and height), so the window is where
	// it was, and the frames are grabbed from that rectangle. A caller that needs
	// to re-confirm the position asks the ONE resolver (ReResolveWindow), which is
	// where the window-selection rule lives.
	return Rect{X: t.Geometry.X, Y: t.Geometry.Y, W: got.W, H: got.H}, nil
}

// configureWindowSize sends ConfigureWindow with the two pixel dimensions set.
//
// It is the ONLY writer of the request in this package (Rules 13/14): the request
// length, the mask and the value order are the X11 wire layout, and a second
// caller composing its own would be a second chance to get the byte map wrong —
// the exact mistake x11OpGetWindowAttributes' comment records for GetGeometry.
func (c *x11Client) configureWindowSize(window, width, height uint32) error {
	req := make([]byte, x11ConfigureWindowTotalLen*4)
	req[0] = x11OpConfigureWindow
	binary.NativeEndian.PutUint16(req[2:], uint16(x11ConfigureWindowTotalLen))
	binary.NativeEndian.PutUint32(req[4:], window)
	binary.NativeEndian.PutUint16(req[8:], x11ConfigMaskWidth|x11ConfigMaskHeight)
	binary.NativeEndian.PutUint32(req[12:], width)
	binary.NativeEndian.PutUint32(req[16:], height)
	// ConfigureWindow has NO reply: it is a request whose only answer is its
	// effect, which is why the caller reads the geometry back afterwards. The
	// write is still checked, and any X11 error it provokes arrives as an event
	// frame on the next reply this connection reads — the caller's GetGeometry
	// (readReply drains an error frame that names THIS sequence and refuses).
	if err := c.request(req); err != nil {
		return err
	}
	return nil
}
