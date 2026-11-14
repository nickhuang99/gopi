package lineedit

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// The probe the attribute tests are built on: one row that starts red, returns
// to the default colour, and ends bold. A grid that DISCARDS SGR renders it
// identically to an uncoloured row (that was the pre-change behaviour, see
// devdoc/sgr-cell-attrs.md), so every assertion below is a red-proof: it cannot
// pass without per-cell attribute data.
const (
	sgrProbe        = "\x1b[31mred\x1b[0m plain"
	sgrProbeText    = "red plain"
	sgrProbeRedEnd  = 2 // "red" occupies columns 0..2
	sgrProbePlainAt = 3 // " plain" starts here
	sgrProbeCols    = 20
	sgrProbeRows    = 3
)

// redStyle is the style SGR 31 leaves on a cell.
func redStyle() sgr.Style {
	s := sgr.Default()
	s.Fg = sgr.Red
	return s
}

// TestScreenRecordsPerCellAttributes is the epic's own case: `\x1b[31mred\x1b[0m
// plain` must leave the first three columns red and the rest default, ASSERTED AS
// ATTRIBUTES on the grid (no pixels, no terminal).
func TestScreenRecordsPerCellAttributes(t *testing.T) {
	sc := NewScreen(sgrProbeCols, sgrProbeRows)
	sc.Feed([]byte(sgrProbe))

	// The text is unchanged by the colour — the plain accessor still sees it.
	if got := sc.Line(0); got != sgrProbeText {
		t.Fatalf("Line(0) = %q, want %q", got, sgrProbeText)
	}
	for col := 0; col <= sgrProbeRedEnd; col++ {
		cell := sc.Cell(0, col)
		if !cell.Style.Equal(redStyle()) {
			t.Errorf("cell (0,%d) = %q %s, want the probe's red %s",
				col, cell.Rune, cell.Kind, redStyle())
		}
	}
	for col := sgrProbePlainAt; col < len(sgrProbeText); col++ {
		cell := sc.Cell(0, col)
		if !cell.Style.Equal(sgr.Default()) {
			t.Errorf("cell (0,%d) = %q %s, want default after the SGR reset",
				col, cell.Rune, cell.Kind)
		}
	}
	// "red" and default must be DISTINGUISHABLE — the whole point of the probe,
	// and exactly what the pre-change grid could not say.
	if sc.Attr(0, 0).Equal(sc.Attr(0, sgrProbePlainAt)) {
		t.Fatal("the red cell compares equal to a default cell: attributes are not recorded")
	}
	// The pen survives the reset in force: the reset itself cleared it, so a
	// trailing write is default too.
	if got := sc.Attr(0, sgrProbePlainAt); !got.Equal(sgr.Default()) {
		t.Fatalf("attr after reset = %s, want default", got)
	}
}

// TestScreenAttrRoundTripThroughFeed feeds each attribute's own sequence through
// the parser and asserts the cell carries it — the emit/parse agreement between
// an sgr.Style and this grid. A style is rendered with sgr (the same constant
// package the parser reads), so a parameter number that drifts on either side
// fails here rather than showing up as a wrong colour in a terminal.
func TestScreenAttrRoundTripThroughFeed(t *testing.T) {
	base := sgr.Default()
	cases := []struct {
		name  string
		style sgr.Style
	}{
		{"bold", func() sgr.Style { s := base; s.Bold = true; return s }()},
		{"dim", func() sgr.Style { s := base; s.Dim = true; return s }()},
		{"italic", func() sgr.Style { s := base; s.Italic = true; return s }()},
		{"underline", func() sgr.Style { s := base; s.Underline = true; return s }()},
		{"reverse", func() sgr.Style { s := base; s.Reverse = true; return s }()},
		{"bold+dim+underline", func() sgr.Style {
			s := base
			s.Bold, s.Dim, s.Underline = true, true, true
			return s
		}()},
		{"fg named", func() sgr.Style { s := base; s.Fg = sgr.Blue; return s }()},
		{"fg bright", func() sgr.Style { s := base; s.Fg = sgr.BrightWhite; return s }()},
		{"fg 256", func() sgr.Style { s := base; s.Fg = 208; return s }()},
		{"bg 256", func() sgr.Style { s := base; s.Bg = 17; return s }()},
		{"bg truecolour", func() sgr.Style { s := base; s.BgRGB = sgr.RGB(18, 52, 86); return s }()},
		{"fg truecolour", func() sgr.Style { s := base; s.FgRGB = sgr.RGB(200, 100, 50); return s }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := NewScreen(sgrProbeCols, sgrProbeRows)
			// A preceding colour proves the style is applied ABSOLUTELY: a
			// specification that only set its differences would inherit green.
			sc.Feed([]byte(sgr.FgSeq(sgr.Green) + tc.style.Seq() + "X"))
			cell := sc.Cell(0, 0)
			if cell.Rune != 'X' {
				t.Fatalf("cell (0,0) rune = %q, want 'X'", cell.Rune)
			}
			if !cell.Style.Equal(tc.style) {
				t.Fatalf("cell style = %s, want %s (seq %q)", cell.Style, tc.style, tc.style.Seq())
			}
		})
	}
}

// TestScreenSGRResetClearsAttributes asserts SGR 0 returns the pen to default:
// the attributes set before it must not reach the cells written after it.
func TestScreenSGRResetClearsAttributes(t *testing.T) {
	sc := NewScreen(sgrProbeCols, sgrProbeRows)
	sc.Feed([]byte(sgr.BoldSeq() + sgr.DimSeq() + sgr.FgSeq(sgr.Red) + "A" + sgr.ResetSeq() + "B"))

	if got := sc.Attr(0, 0); !got.Bold || !got.Dim || got.Fg != sgr.Red {
		t.Fatalf("attr before the reset = %s, want bold+dim+red", got)
	}
	if got := sc.Attr(0, 1); !got.Equal(sgr.Default()) {
		t.Fatalf("attr after SGR 0 = %s, want default", got)
	}
}

// TestScreenSGRBareResetIsReset asserts the CSI convention the parser relies on:
// a bare `CSI m` carries no parameter, which a terminal reads as SGR 0. Getting
// this wrong would leave a style in force across a reset.
func TestScreenSGRBareResetIsReset(t *testing.T) {
	sc := NewScreen(sgrProbeCols, sgrProbeRows)
	sc.Feed([]byte(sgr.BoldSeq() + "A" + sgr.CSI + string(sgr.FinalSGR) + "B"))

	if got := sc.Attr(0, 0); !got.Bold {
		t.Fatalf("attr of the bold cell = %s, want bold", got)
	}
	if got := sc.Attr(0, 1); !got.Equal(sgr.Default()) {
		t.Fatalf("attr after a bare CSI m = %s, want default (bare SGR is SGR 0)", got)
	}
}

// TestScreenExtendedColors asserts the two wide colour spaces are representable
// and distinguishable from each other and from the default: a 256-palette index,
// a truecolour triple (fg and bg), and the 39/49 restore.
func TestScreenExtendedColors(t *testing.T) {
	sc := NewScreen(sgrProbeCols, sgrProbeRows)
	sc.Feed([]byte("\x1b[38;5;208mO\x1b[38;2;18;52;86mT\x1b[48;5;17mB\x1b[48;2;1;2;3mC\x1b[39;49mD"))

	palette := sc.Attr(0, 0)
	if palette.Fg != 208 || palette.FgRGB != sgr.NoRGB {
		t.Errorf("256-colour cell fg = %d (rgb %d), want palette 208 with no RGB", palette.Fg, palette.FgRGB)
	}
	truecolor := sc.Attr(0, 1)
	if truecolor.FgRGB != sgr.RGB(18, 52, 86) || truecolor.Fg != sgr.DefaultColor {
		t.Errorf("truecolour cell = %s, want rgb #123456 and no palette index", truecolor)
	}
	if bg := sc.Attr(0, 2); bg.Bg != 17 || bg.BgRGB != sgr.NoRGB {
		t.Errorf("256-colour background cell = %s, want bg palette 17", bg)
	}
	if bg := sc.Attr(0, 3); bg.BgRGB != sgr.RGB(1, 2, 3) {
		t.Errorf("truecolour background cell = %s, want bg rgb #010203", bg)
	}
	if got := sc.Attr(0, 4); !got.Equal(sgr.Default()) {
		t.Errorf("cell after SGR 39;49 = %s, want default", got)
	}
	// The palette space and the truecolour space must never be confused.
	if sc.Attr(0, 0).Equal(sc.Attr(0, 1)) {
		t.Fatal("a 256-palette cell compares equal to a truecolour cell")
	}
}

// TestScreenWideRuneOccupiesTwoAttributedColumns asserts the column model on a
// CJK character: it occupies TWO cells, both painted with the style in force, the
// second marked as the continuation, and the cursor advances two columns — while
// Line still reports one rune of text.
func TestScreenWideRuneOccupiesTwoAttributedColumns(t *testing.T) {
	sc := NewScreen(sgrProbeCols, sgrProbeRows)
	sc.Feed([]byte(sgr.FgSeq(sgr.Red) + "知" + sgr.ResetSeq() + "x"))

	if got := sc.Line(0); got != "知x" {
		t.Fatalf("Line(0) = %q, want %q (a wide rune is ONE rune of text)", got, "知x")
	}
	first := sc.Cell(0, 0)
	if first.Rune != '知' || first.Kind != CellRune {
		t.Fatalf("cell (0,0) = %q %s, want the wide rune itself", first.Rune, first.Kind)
	}
	if !first.Style.Equal(redStyle()) {
		t.Errorf("cell (0,0) style = %s, want red", first.Style)
	}
	second := sc.Cell(0, 1)
	if second.Kind != CellWideContinuation {
		t.Fatalf("cell (0,1) = %q %s, want the wide rune's continuation column", second.Rune, second.Kind)
	}
	if !second.Style.Equal(redStyle()) {
		t.Errorf("cell (0,1) style = %s, want the same red (both columns are the rune's)", second.Style)
	}
	// The next rune starts two columns later, NOT in the continuation column.
	third := sc.Cell(0, 2)
	if third.Rune != 'x' || third.Kind != CellRune {
		t.Fatalf("cell (0,2) = %q %s, want 'x' (the wide rune must not consume one column)", third.Rune, third.Kind)
	}
	if !third.Style.Equal(sgr.Default()) {
		t.Errorf("cell (0,2) style = %s, want default (the reset preceded it)", third.Style)
	}
	if sc.CurCol != 3 {
		t.Errorf("cursor column = %d, want 3 (two for 知, one for x)", sc.CurCol)
	}
}

// TestScreenWideRuneNeverStraddlesTheEdge asserts the wrap rule for a wide rune
// at the last column: wrapping WHOLE (as layoutText does) rather than splitting
// it, so the drawing and the grid agree about where the next row starts.
func TestScreenWideRuneNeverStraddlesTheEdge(t *testing.T) {
	const cols = 3
	sc := NewScreen(cols, sgrProbeRows)
	sc.Feed([]byte("ab知"))

	if got := sc.Line(0); got != "ab" {
		t.Fatalf("Line(0) = %q, want %q (the wide rune wrapped whole)", got, "ab")
	}
	if got := sc.Line(1); got != "知" {
		t.Fatalf("Line(1) = %q, want %q", got, "知")
	}
	if sc.CurRow != 1 || sc.CurCol != 2 {
		t.Fatalf("cursor at (%d,%d), want (1,2)", sc.CurRow, sc.CurCol)
	}
}

// TestScreenStyledLineMatchesPlainLine asserts the styled variant describes the
// SAME row as the plain one: the text is identical, and the styles are per
// COLUMN (a wide rune contributes one rune of text and two columns of style).
func TestScreenStyledLineMatchesPlainLine(t *testing.T) {
	sc := NewScreen(sgrProbeCols, sgrProbeRows)
	sc.Feed([]byte(sgr.FgSeq(sgr.Red) + "知" + sgr.FgSeq(sgr.Blue) + "b" + sgr.ResetSeq()))

	text, styles := sc.StyledLine(0)
	if text != sc.Line(0) {
		t.Fatalf("StyledLine text %q != Line %q", text, sc.Line(0))
	}
	if text != "知b" {
		t.Fatalf("text = %q, want %q", text, "知b")
	}
	blue := sgr.Default()
	blue.Fg = sgr.Blue
	want := []sgr.Style{redStyle(), blue}
	if len(styles) != len(want) {
		t.Fatalf("styles = %v, want one entry per rune (%d)", styles, len(want))
	}
	for i := range want {
		if !styles[i].Equal(want[i]) {
			t.Errorf("style[%d] = %s, want %s", i, styles[i], want[i])
		}
	}
	// The wide rune's SECOND column is still visible where it belongs — as a
	// column, through Cell.
	if got := sc.Cell(0, 1); got.Kind != CellWideContinuation || !got.Style.Equal(redStyle()) {
		t.Errorf("cell (0,1) = %s, want the wide rune's continuation in its own red", got.Kind)
	}
	// An untouched row has no styles at all — the empty row is empty in both
	// senses, which is what lets a caller loop over StyledLine's result.
	if got, styles := sc.StyledLine(1); got != "" || len(styles) != 0 {
		t.Errorf("untouched row = %q %v, want empty text and no styles", got, styles)
	}
}

// TestScreenStyledContentsRendersStyleRuns asserts the whole-screen styled dump
// names every column: a 20-column row is covered by runs, the coloured span is
// readable by eye, and an untouched row renders as nothing at all.
func TestScreenStyledContentsRendersStyleRuns(t *testing.T) {
	sc := NewScreen(sgrProbeCols, sgrProbeRows)
	sc.Feed([]byte(sgrProbe))

	dump := sc.StyledContents()
	for _, want := range []string{
		sgrProbeText,                 // the text itself
		"0-2 " + redStyle().String(), // the red span, both ends inclusive
		"3-8 default",                // the rest of the row
	} {
		if !strings.Contains(dump, want) {
			t.Errorf("styled dump %q does not contain %q", dump, want)
		}
	}
	if !strings.Contains(dump, styleRunSeparator) {
		t.Errorf("styled dump %q carries no run separator", dump)
	}
}

// TestScreenEraseClearsStylesButKeepsThePen asserts the terminal's erase rule:
// erasing a cell drops its style (a blank cell carries no colour) while the pen
// in force survives, so the next rune written is still red.
func TestScreenEraseClearsStylesButKeepsThePen(t *testing.T) {
	sc := NewScreen(sgrProbeCols, sgrProbeRows)
	// EL 2 erases the whole line; the cursor stays where it was, so "X" lands on
	// column 3 — the column after the erased text — and must still be red.
	sc.Feed([]byte(sgr.FgSeq(sgr.Red) + "red" + escCSI + "2K" + "X"))

	if got := sc.Cell(0, 3); got.Rune != 'X' || !got.Style.Equal(redStyle()) {
		t.Fatalf("cell (0,3) = %q %s, want 'X' still red (the pen survives an erase)", got.Rune, got.Style)
	}
	if got := sc.Attr(0, 0); !got.Equal(sgr.Default()) {
		t.Fatalf("attr of the erased cell = %s, want default (an erased cell has no colour)", got)
	}
	// Erase-in-display clears the styles of the whole screen.
	sc.Feed([]byte(escCSI + "2J" + "Y"))
	if got := sc.Attr(0, 0); !got.Equal(sgr.Default()) {
		t.Fatalf("attr after ED 2 = %s, want default", got)
	}
}

// TestScreenScrollKeepsCellStyles asserts a coloured row keeps its colour as it
// moves: scrollRows shifts the attribute grid with the rune grid, so a style
// cannot stay behind on a row it was never painted on.
func TestScreenScrollKeepsCellStyles(t *testing.T) {
	const rows = 2
	sc := NewScreen(sgrProbeCols, rows)
	// CR+LF, not a bare LF: a real terminal moves down WITHOUT returning the
	// column, so a test that fed "\n" would leave the second row indented.
	sc.Feed([]byte(sgr.FgSeq(sgr.Red) + "red" + escCarriage + escNewline + sgr.FgSeq(sgr.Blue) + "blue"))

	if got := sc.Line(1); got != "blue" {
		t.Fatalf("row 1 = %q, want %q", got, "blue")
	}
	// Scroll once: the red row is saved to the scrollback, the blue row rises.
	sc.Feed([]byte(escCarriage + escNewline))
	if got := sc.Line(0); got != "blue" {
		t.Fatalf("row 0 after scrolling = %q, want %q", got, "blue")
	}
	if got := sc.Attr(0, 0); got.Fg != sgr.Blue {
		t.Fatalf("row 0 attr after scrolling = %s, want the blue it was painted with", got)
	}
	if got := sc.Attr(0, 0); got.Fg == sgr.Red {
		t.Fatal("the scrolled row kept the red of the row above it: the attribute grid did not shift")
	}
}

// TestScreenAttrOutOfRangeIsDefault asserts the read API's edge behaviour: a
// column outside the grid answers with the default cell rather than panicking,
// so a caller comparing a whole row end to end cannot index out of bounds.
func TestScreenAttrOutOfRangeIsDefault(t *testing.T) {
	sc := NewScreen(4, 2)
	for _, c := range []struct{ row, col int }{{-1, 0}, {0, -1}, {2, 0}, {0, 4}} {
		cell := sc.Cell(c.row, c.col)
		if !cell.Style.Equal(sgr.Default()) || cell.Kind != CellEmpty {
			t.Errorf("Cell(%d,%d) = %q %s, want the default empty cell", c.row, c.col, cell.Rune, cell.Kind)
		}
	}
}
