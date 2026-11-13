package main

// chat_geometry.go — the IDEAL GEOMETRY as the CHAT SURFACE uses it: one
// resolution, one cap, and one honest sentence when the terminal is smaller.
//
// THE KNOB LIVES IN internal/config (chat_geometry.go there): settings key,
// env var, CLI flag and the built-in default, resolved at ONE site so a normal
// chat and the replay/beat filmer cannot hold two ideas of the designed size.
// This file is the SURFACE half — what the surface does with the answer:
//
//   - the canvas: a terminal WIDER than the ideal draws the chrome at the ideal
//     width (lineedit.Editor.SetCanvasCols), because the chrome is designed for
//     it and the info bar's columns are pinned to a fixed cell count anyway;
//   - the degradation: a terminal NARROWER than the ideal is TOLD what it
//     cannot draw (Rule 15 — never a silent downgrade), in the same words the
//     help page uses for the effect claim.

import (
	"fmt"
	"io"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/lineedit"
)

// chatGeometryNoticeFmt is the ONE sentence that states a degraded geometry.
// Its arguments, in order: the terminal's own size, the ideal size, the source
// label that decided it, and what the terminal cannot host.
const chatGeometryNoticeFmt = "[gopi] this terminal is %dx%d, smaller than the chat surface's ideal %dx%d (%s): %s\n"

// The two facts a degraded geometry can cost, spelled once each (Rule 1b/13):
// the startup screen's box, and the info bar's aligned form. They are the two
// pieces of chrome whose loss a reader would otherwise see without explanation
// (the reserved editing line has its own notice, which this one does not
// duplicate).
const (
	chatGeometryLossBanner = "the startup screen's box cannot be drawn (it needs at least %d columns) — the one-line greeting stands in for it"
	chatGeometryLossStatus = "the info bar is drawn in its reduced form (%d of %d pinned columns)"
)

// chatGeometryNotice returns the sentence announcing that THIS terminal cannot
// host the ideal geometry, or "" when it can (or when the ideal is unknown).
//
// It is a pure function of (ideal, terminal size) so the wording, the
// thresholds and the decision are testable without a terminal — which is the
// point of Rule 31's unit half.
func chatGeometryNotice(ideal config.ChatIdealState, cols, rows int) string {
	if cols <= 0 || rows <= 0 {
		// "Unknown" is not a small terminal: a caller with no measurement says
		// nothing rather than claiming a degradation it did not observe.
		return ""
	}
	if cols >= ideal.Cols && rows >= ideal.Rows {
		return ""
	}
	loss := ""
	if cols < chatBannerMinCols() {
		loss = fmt.Sprintf(chatGeometryLossBanner, chatBannerMinCols())
	} else if cols < statusColumnsAtCols {
		loss = fmt.Sprintf(chatGeometryLossStatus, cols, statusColumnsAtCols)
	} else {
		// The width is enough for every piece of chrome; only the ROW count is
		// short of the ideal. The layout itself decides whether that is fatal
		// (RowsMinUsable, announced by the install path), so the honest
		// statement here is the one fact this function measured.
		loss = fmt.Sprintf(chatGeometryLossRows, rows, ideal.Rows)
	}
	return fmt.Sprintf(chatGeometryNoticeFmt, cols, rows, ideal.Cols, ideal.Rows, ideal.Source, loss)
}

// chatGeometryLossRows is the row-count half of the same statement: the chrome
// is designed for `ideal` rows and the terminal has fewer, so the answer band is
// what yields (region.go's own budget), not the editing line.
const chatGeometryLossRows = "the answer band is drawn with the rows that are left (%d of the %d the chrome is designed for)"

// chatGeometryCanvas is the CAP this run declares: the designed width, and
// nothing about the terminal it happens to have started on.
//
// WHY THE TERMINAL'S WIDTH IS NOT PART OF THE VALUE — THE FREEZE THIS FUNCTION
// USED TO CAUSE. It used to return min(ideal, cols), where cols was the terminal
// as measured ONCE at startup. That number was then installed as the canvas, and
// the canvas is only ever re-read as canvasWidth() = min(canvas, the terminal
// NOW). Composing the two mins read at two different TIMES freezes the layout at
// the smaller one for the life of the run: started at 63 columns, dragged to
// 110, the canvas stayed 63 and canvasWidth() = min(63, 110) = 63 — the layout,
// the answer band and the status row kept drawing 63 columns with the right half
// of the screen permanently blank. Measured on a real pty (evidence in this
// row's session directory: the dump's `grid=` stayed 63x30 after TIOCSWINSZ
// widened the pty to 110x30, while the ROWS followed, because rows are read from
// the terminal on every path and the width was not).
//
// SO THE MIN() BELONGS IN EXACTLY ONE PLACE, AND IT IS NOT HERE. A cap is a
// property of the RUN (the designed width); the clamp against the terminal is a
// property of the MOMENT and lives in lineedit.Editor.canvasWidth(), which is
// re-evaluated on every install and every winch. Handing over the ideal keeps
// both properties true at once: a NARROW terminal still gets its own width
// (min(117, 80) = 80 — a row composed for 117 cells on an 80-cell screen
// overflows it, which is why the cap must never be dropped to "no canvas"), and
// a WIDENING terminal now grows up to the ideal instead of being stuck at
// whatever width the window had at launch.
//
// cols is therefore read for ONE thing: whether a measurement exists at all. A
// caller with no measurement (0) declares no canvas, which is the historic
// behaviour byte-for-byte, rather than clamping to a number it did not observe.
func chatGeometryCanvas(ideal config.ChatIdealState, cols int) int {
	if cols <= 0 || ideal.Cols <= 0 {
		return 0 // no measurement, or no ideal: no canvas — the terminal's own width, exactly as before
	}
	return ideal.Cols
}

// chatBeatIdealCols / chatBeatIdealRows are the beat protocol's default
// geometry — read from the ONE knob (config.ChatIdealGeometry) rather than from
// a local 117x26 pair, so the launcher and the film it drives agree by
// construction.
func chatBeatIdealCols() int { return config.ChatIdealCols() }

// chatBeatIdealRows is the same default's row half.
func chatBeatIdealRows() int { return config.ChatIdealRows() }

// chatApplyIdealGeometry resolves the knob ONCE for this run and installs what
// the surface can honour, reporting the degradation on the diagnostic stream.
//
// It is called from the ONE place the interactive input layer is built
// (newChatInputWithPalette), which is the earliest point where the terminal is
// open and measured and the latest one that is still before the first paint —
// so the sentence is never written over by the layout's own emission, and the
// canvas is in force when the layout installs.
//
// The source label rides the notice: "the terminal is smaller than the ideal
// BECAUSE YOU SET IT to 133x40" and "because the built-in default is 117x26" are
// two different facts, and the second one has a remedy (declare a smaller ideal)
// the first does not.
func chatApplyIdealGeometry(ed *lineedit.Editor, term *lineedit.Terminal, errOut io.Writer) {
	ideal := config.ChatIdealGeometry()
	cols, rows := term.Width(0), term.Height(0)
	if notice := chatGeometryNotice(ideal, cols, rows); notice != "" {
		fmt.Fprint(errOut, notice)
	}
	ed.SetCanvasCols(chatGeometryCanvas(ideal, cols))
}
