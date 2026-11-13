package shot

// terminalface.go — THE FILMED TERMINAL'S OWN FACE, read off its window.
//
// WHY THIS FILE EXISTS (measured 2026-10-04). Every pixel figure this driver
// used to derive was derived from ONE face — xterm's built-in `fixed` 6x13 with
// its 4 px border (cmd/agent's chatRefCellW/chatRefCellH/chatRefBorderPx) — and
// the film's DEFAULT terminal is NOT xterm (it is xfce4-terminal, because a CJK
// surface needs a TrueType face and xterm has no CJK glyph at all). The two
// terminals give DIFFERENT pixels for the same cell count:
//
//	xterm           82x26 → 4  + 6x82  = 496, 4  + 13x26 = 342 px
//	xfce4-terminal  82x26 → 16 + 10x82 = 836, 27 + 19x26 = 521 px
//
// so a limit derived on the reference face ("500 px / 6 px per cell → 82
// columns") is a limit about a terminal the film is not being shot in: the same
// 82 columns arrive 836 px wide and are refused. The reference face is a fact
// about xterm; the film's frame size is a fact about the terminal that is
// actually on the display.
//
// WHERE THE REAL FACE COMES FROM — and why NOT a font table. A terminal client
// PUBLISHES its own geometry model in WM_NORMAL_HINTS (ICCCM's XSizeHints, a
// PREDEFINED atom, so this costs one GetProperty and no InternAtom): the resize
// INCREMENT is one character cell, and the BASE SIZE is the chrome around the
// grid. A window's size is then `base + increment × cells`, exactly:
//
//	xfce4-terminal --geometry=82x26  →  base 16x27, incr 10x19 → 836x521 (the
//	                                    window the server reports is 836x521)
//	xterm -geometry 82x26            →  base 4x4,   incr 6x13  → 496x342 (the
//	                                    server reports 496x342)
//
// Both were read with this file's own parser. So the face is a MEASUREMENT the
// terminal makes about itself, not a table this repository keeps: no px-per-cell
// knob, no font table, no second source that can drift from the font that is
// really drawing (Rule 35 — the alternative was rejected for exactly this
// reason: copying a terminal's font metrics into gopi is a new dependency and it
// would still be wrong on the next operator's machine).
//
// WHAT THIS FILE DOES NOT DO. It does not decide anything: it reports what the
// client said, with its provenance (which window, on which display), and it
// distinguishes "the client published a face" from "the client published none"
// (Measured false + Why) — the two are different facts and a caller acting on
// the second as if it were the first would derive pixels from nothing (Rule 15).

import (
	"fmt"
	"strings"
)

const (
	// x11AtomWMNormalHintsID is WM_NORMAL_HINTS. It is a PREDEFINED atom
	// (`#define XA_WM_NORMAL_HINTS ((Atom) 40)` in X11/Xatom.h, checked against
	// the installed header by a test — the same check x11AtomWMNameID has), so
	// the request needs no InternAtom round trip and no atom the display might
	// not have. It is 40 and NOT 191: 191 is WM_TAKE_FOCUS's neighbourhood, and
	// a wrong atom here would come back type-None on every window — a face
	// "unmeasurable on every terminal", which reads like a client that
	// publishes nothing rather than like a bug in this file.
	x11AtomWMNormalHintsID = 40

	// x11WMNormalHintsItems is how many CARD32 items ICCCM's XSizeHints carries
	// as WM_NORMAL_HINTS: flags, x, y, width, height, min/max width and height,
	// the two resize increments, four aspect fields, the two base sizes and the
	// gravity. A client that publishes fewer is not publishing this property.
	x11WMNormalHintsItems = 18

	// The XSizeHints field indices this file reads, by name rather than by a
	// bare offset inside the parser (Rule 1b: the layout is the spec's, and a
	// reader must be able to check it against ICCCM's XSizeHints without
	// counting).
	x11HintFlagsIndex   = 0
	x11HintWidthIncIdx  = 9
	x11HintHeightIncIdx = 10
	x11HintBaseWidthIdx = 15
	x11HintBaseHeightIx = 16

	// The two flag bits that say a field above is DECLARED. ICCCM leaves an
	// undeclared field as garbage, so the bit is read first — but it is NOT the
	// last word, and the reason is measured rather than theoretical:
	//
	//   xfce4-terminal (VTE) on this host publishes flags=848 (0x350) with the
	//   resize increment fields filled 10, 19, and 0x350 does NOT carry
	//   PResizeInc (0x40 = 64). Its window is 836x521 for 82x26 cells, which is
	//   16+10x82 by 27+19x26 — the fields describe the window exactly.
	//
	// So a reader that insisted on the bit would call this host's DEFAULT
	// terminal unmeasurable while its own numbers were sitting in the property
	// (xprop reads them the same way, which is where the "10 by 19" line in every
	// measurement of this window comes from). An undeclared field is therefore
	// accepted when it is PLAUSIBLE, and implausible values — the real risk of a
	// garbage field: a cell of 0 px, or of 100000 — are refused by the bounds
	// below. The end-to-end read against a live window is the guard that a
	// plausible-but-wrong reading cannot survive: the model must PREDICT the
	// window the server actually mapped.
	x11HintPResizeInc = 1 << 6
	x11HintPBaseSize  = 1 << 8

	// filmFaceMinCellPx / filmFaceMaxCellPx bound a plausible character cell. One
	// pixel is the smallest a glyph can be drawn in; 64 px is beyond any
	// legible terminal font at the sizes a window this driver photographs would
	// use, so a value above it is far more likely to be an uninitialised field
	// than a real face.
	filmFaceMinCellPx = 1
	filmFaceMaxCellPx = 64
	// filmFaceMaxChromePx bounds the non-grid chrome (a border, a scrollbar, a
	// title strip). A terminal window's client area is the grid plus a frame of
	// this order; a larger value is not chrome.
	filmFaceMaxChromePx = 512
)

// TerminalFace is ONE terminal's pixel model, as the terminal itself publishes
// it: one character cell in pixels, and the chrome around the grid.
//
// The zero value is NOT a face (Measured=false): a caller that reads no face
// derives pixels from nothing, and that must be visible rather than looking like
// a terminal whose cells are zero pixels wide (Rule 15a — a zero value that
// means "unknown" is a lie waiting to be believed).
type TerminalFace struct {
	// CellW / CellH are one character cell in pixels (WM_NORMAL_HINTS' resize
	// increment): the step the cell grid advances the window by.
	CellW, CellH int
	// BorderW / BorderH are the pixels around the grid (WM_NORMAL_HINTS' base
	// size): what a window of N cells adds to N × the cell size. They are the
	// reason a face's parity becomes the frame's parity — measured
	// 2026-10-04: xfce4-terminal's 27 px border makes an even row count an ODD
	// frame height (19×26+27 = 521).
	BorderW, BorderH int
	// Window is the window the face was read from, and Display the display it
	// was on: the answer carries its provenance, so a reader can ask the same
	// window the same question (Rule 15).
	Window  uint32
	Display string
	// Measured says whether the numbers above came from the client. False means
	// the client published no usable WM_NORMAL_HINTS, and Why says which of the
	// two shapes that was.
	Measured bool
	// Why states the reason when Measured is false (empty when it is true).
	Why string
}

// Known reports whether this face carries a measured cell size.
func (f TerminalFace) Known() bool { return f.Measured && f.CellW > 0 && f.CellH > 0 }

// WidthForCols is the model's own prediction: the pixel width a window of cols
// cells comes out at on this face. It is the SAME arithmetic the face was read
// with (base + increment × cells) and the caller is expected to check it against
// the window the server actually mapped — a model that is never checked is a
// guess wearing a measurement's clothes.
func (f TerminalFace) WidthForCols(cols int) int { return f.BorderW + f.CellW*cols }

// HeightForRows is WidthForCols' row half.
func (f TerminalFace) HeightForRows(rows int) int { return f.BorderH + f.CellH*rows }

// String renders the face the way a refusal and a report print it: the per-cell
// step, the chrome, and WHERE the reading came from. An unmeasured face renders
// as the absence it is, with its reason — never as a face of zeros.
func (f TerminalFace) String() string {
	if !f.Known() {
		why := f.Why
		if why == "" {
			why = "the client published no WM_NORMAL_HINTS"
		}
		return "an UNMEASURED face (" + why + ")"
	}
	base := fmt.Sprintf("%dx%d px per cell + %dx%d px border", f.CellW, f.CellH, f.BorderW, f.BorderH)
	if f.BorderW == 0 && f.BorderH == 0 {
		base = fmt.Sprintf("%dx%d px per cell (the client published no base size)", f.CellW, f.CellH)
	}
	if f.Display == "" {
		return base
	}
	return fmt.Sprintf("%s — read from window 0x%x on %s", base, f.Window, f.Display)
}

// Face reads this terminal's window's own geometry model (WM_NORMAL_HINTS).
//
// An error means the PROPERTY COULD NOT BE READ (no display, a broken
// connection, a property of the wrong format). A client that simply published no
// usable hints is NOT an error — it is an answer, returned with Measured=false
// and Why set, because the two have different consequences: an unreadable
// display is a failure to report, while "this client does not say" is a
// degradation the caller names and works around.
func (t *FilmTerminal) Face() (TerminalFace, error) {
	if t == nil || t.Window == 0 {
		return TerminalFace{}, fmt.Errorf("read the film terminal's face: there is no window to read it from (the terminal was not started, or never mapped a window)")
	}
	name := captureDisplayName(t.Display)
	if name == "" {
		return TerminalFace{}, x11ErrNoDisplay()
	}
	c, err := dialX11(name)
	if err != nil {
		return TerminalFace{}, err
	}
	defer c.close()
	items, err := c.windowPropertyItems(t.Window, x11AtomWMNormalHintsID, x11WMNormalHintsItems)
	if err != nil {
		return TerminalFace{}, fmt.Errorf("read %s off window 0x%x on %s: %w", "WM_NORMAL_HINTS", t.Window, t.Display, err)
	}
	face, why := faceFromHints(t.Window, t.Display, items)
	if why != "" {
		face.Measured, face.Why = false, why
	}
	return face, nil
}

// faceFromHints turns the property's items into a face, or says why it cannot:
// an absent property, a truncated one, and one whose resize increment is not
// declared are three different facts, and only the first of them is "the client
// said nothing".
//
// The BASE SIZE is optional in the ICCCM sense (a client that declares no base
// size has a base of zero), so a face without it is still measured — the chrome
// is simply zero, which is reported in String() rather than passed off as a
// reading. The resize INCREMENT is not optional: without it there is no cell
// size, and a face without a cell size is not a face.
func faceFromHints(window uint32, display string, items []uint32) (TerminalFace, string) {
	if len(items) == 0 {
		return TerminalFace{}, "the window publishes no WM_NORMAL_HINTS property"
	}
	if len(items) < x11WMNormalHintsItems {
		return TerminalFace{}, fmt.Sprintf("the window's WM_NORMAL_HINTS carries %d item(s), want %d (ICCCM's XSizeHints)", len(items), x11WMNormalHintsItems)
	}
	flags := items[x11HintFlagsIndex]
	f := TerminalFace{
		Window:   window,
		Display:  display,
		CellW:    int(items[x11HintWidthIncIdx]),
		CellH:    int(items[x11HintHeightIncIdx]),
		Measured: true,
	}
	if !declaredCellSize(f.CellW, f.CellH) {
		return TerminalFace{}, fmt.Sprintf("the window's WM_NORMAL_HINTS states no usable character cell: the resize increment is %dx%d px and its PResizeInc bit is %v — %s",
			f.CellW, f.CellH, flags&x11HintPResizeInc != 0, filmFaceCellBoundReason)
	}
	// The chrome is read whenever it is PLAUSIBLE — the PBaseSize bit alone is not
	// enough either way (a client that sets it and leaves the fields unset has
	// both happened in the wild), and a garbage chrome shifts every derived pixel
	// figure without ever looking wrong.
	if plausibleChrome(int(items[x11HintBaseWidthIdx]), int(items[x11HintBaseHeightIx])) {
		f.BorderW = int(items[x11HintBaseWidthIdx])
		f.BorderH = int(items[x11HintBaseHeightIx])
	}
	return f, ""
}

// filmFaceCellBoundReason is the ONE wording of the plausibility bound, so the
// parser's refusal and the test that pins it name the same rule.
const filmFaceCellBoundReason = "a character cell must be " + "1..64 px per axis, and the fields are read even when the PResizeInc bit is clear (measured: VTE publishes its increment without that bit)"

// declaredCellSize reports whether two increment fields are a plausible cell —
// DECLARED or not. The bit is not required (see x11HintPResizeInc's comment for
// the measurement); plausibility is, because an undeclared field is the one that
// can be garbage.
func declaredCellSize(cellW, cellH int) bool {
	return cellW >= filmFaceMinCellPx && cellW <= filmFaceMaxCellPx &&
		cellH >= filmFaceMinCellPx && cellH <= filmFaceMaxCellPx
}

// plausibleChrome reports whether two base-size fields could be a window's
// non-grid chrome. It exists for the same reason as declaredCellSize: the base
// fields are read even when PBaseSize is clear, and a garbage value there would
// shift every derived pixel figure without ever looking wrong.
func plausibleChrome(borderW, borderH int) bool {
	return borderW >= 0 && borderW <= filmFaceMaxChromePx && borderH >= 0 && borderH <= filmFaceMaxChromePx
}

// FaceNotice is the sentence a driver prints after reading the face: the
// measured model, and the frame the requested cell count comes out at ON IT. It
// is printed for the same reason the window identity is — a report that cannot
// state the face cannot be checked against the frame, and the reference face's
// number is exactly the figure that misled a caller before this file existed.
func FaceNotice(face TerminalFace, cols, rows int) string {
	if !face.Known() {
		return fmt.Sprintf("the filmed terminal's face could not be measured (%s) — pixel figures for this run fall back to the driver's reference face", strings.TrimSuffix(face.String(), "]"))
	}
	return fmt.Sprintf("the filmed terminal's own face is %s — %dx%d cells are %dx%d px ON IT", face, cols, rows, face.WidthForCols(cols), face.HeightForRows(rows))
}
