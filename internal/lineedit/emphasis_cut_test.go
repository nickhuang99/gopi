package lineedit

import (
	"strings"
	"testing"
)

// emphasis_cut_test.go — the ROW CUT's effect on intra-line emphasis.
//
// emphasis_test.go pins the tokenizer, the marker arithmetic and the emphasis of
// a line that fits ONE row. This file pins the case the operator reported: a row
// cut (writeRow's cutProbeCols) landing INSIDE a bold span, so the span continues onto
// the row the cut created.
//
// The defect is a BOOKKEEPING one, and it is why the assertion is per CELL: the
// cut is arithmetic on the row's plain characters (cutProbeCols), but the pieces the
// cut is split into were computed by walking the row's spans from their FIRST
// byte — so the second row of a wrapped line was styled as if its text were the
// row's text, and a bold span's tail emphasised a run of plain characters (and
// the bold characters that continued the span came out plain). On screen that
// reads as a bold PREFIX or SUFFIX of a word: the span's cells are not the span's
// characters any more.
//
// The grid is the judge, never an OCR pass and never a byte count: Screen's own
// attribute parser (screenattr.go) is asked which cells carry Bold.

// cutRows / cutProbeCols are the geometry these assertions draw at. The width is
// deliberately narrow (and the line deliberately longer than it) so the row cut
// lands inside the span rather than by luck outside it.
const (
	cutRows      = 14
	cutProbeCols = 40
	// cutBoldRunes / cutPlainRunes size the two runs: together they are longer
	// than one row, so the bold span is CUT by the wrap and the plain run starts
	// on a later row.
	cutBoldRunes  = 50
	cutPlainRunes = 60
	// cutBoldRune / cutPlainRune are the two characters the assertion reads: the
	// bold span is a run of one, the plain remainder a run of the other, so the
	// expected emphasis of a cell is a function of the RUNE in it — which is what
	// makes the check independent of WHERE the cut fell.
	cutBoldRune  = 'o'
	cutPlainRune = 'x'
)

// renderEmphasisAt is renderEmphasis at an arbitrary geometry: the SHIPPED
// AnswerRenderer driven through its public surface, and the screen its bytes
// produce.
func renderEmphasisAt(t *testing.T, pal Palette, marked bool, rows, cols int, deltas ...string) *emphasisProbe {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(rows, cols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", rows, cols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(pal)
	if marked {
		r.BeginTurn(TurnInfo{Index: 1, Prompt: "colour my day, please"})
	} else {
		r.Begin()
	}
	for _, d := range deltas {
		r.Delta(d)
	}
	r.Tick()
	r.Finish()
	if marked {
		r.EndTurn()
	}
	sc := NewScreen(cols, rows)
	sc.Feed([]byte(cap.bytes()))
	return &emphasisProbe{cap: cap, screen: sc}
}

// TestEmphasisRowCutKeepsTheWholeSpanBold is the defect's regression guard: a
// bold span longer than the row is CUT by the wrap, and every cell of it — on
// every row the span occupies — must still carry Bold, while the plain run
// written on the same row must carry none.
//
// The assertion is stated on the rune in the cell, so it cannot be satisfied (or
// broken) by where the cut happened to fall: a cell holding the bold run's
// character is bold, a cell holding the plain run's character is not.
func TestEmphasisRowCutKeepsTheWholeSpanBold(t *testing.T) {
	line := BoldMarker + strings.Repeat(string(cutBoldRune), cutBoldRunes) + BoldMarker +
		" " + strings.Repeat(string(cutPlainRune), cutPlainRunes)
	probe := renderEmphasisAt(t, BasicPalette, false, cutRows, cutProbeCols, line+"\n")

	var seenBold, seenPlain int
	for row := 0; row < cutRows; row++ {
		text, styles := probe.screen.StyledLine(row)
		runes := []rune(text)
		if len(runes) != len(styles) {
			t.Fatalf("row %d has %d runes but %d styles: %q", row, len(runes), len(styles), text)
		}
		for i, rn := range runes {
			var want bool
			switch rn {
			case cutBoldRune:
				want, seenBold = true, seenBold+1
			case cutPlainRune:
				seenPlain++
			default:
				continue
			}
			if got := styles[i].Bold; got != want {
				t.Errorf("row %d col %d (%q): Bold = %v, want %v — the cell's emphasis does not follow the span it is in\nrow text: %q\ngrid:\n%s",
					row, i, string(rn), got, want, text, probe.screen.StyledContents())
			}
		}
	}
	// The line really was drawn: a test that found no cell of either run would
	// pass vacuously.
	if seenBold != cutBoldRunes || seenPlain != cutPlainRunes {
		t.Fatalf("found %d bold-run cell(s) and %d plain-run cell(s), want %d and %d — the line did not reach the screen as composed\n%s",
			seenBold, seenPlain, cutBoldRunes, cutPlainRunes, probe.screen.StyledContents())
	}
}

// TestEmphasisBurstBoundaryInsideASpanKeepsTheEvidence wraps the OTHER input to
// the same bookkeeping: the span is completed by a LATER burst, so the row the
// commit writes is reconciled with the text the live row already shows (head).
// The emphasis of the decided line must reach every cell of the span exactly
// once, on whichever row the cut put it.
func TestEmphasisBurstBoundaryInsideASpanKeepsTheEvidence(t *testing.T) {
	// The first burst paints an unfinished line (its markers literal, as the
	// provisional paint requires); the second closes it and adds the plain tail,
	// which is longer than the row the head leaves behind.
	head := "prefix " + BoldMarker + "bo"
	tail := "ld" + BoldMarker + " " + strings.Repeat(string(cutPlainRune), cutPlainRunes) + "\n"
	probe := renderEmphasisAt(t, BasicPalette, false, cutRows, cutProbeCols, head, tail)

	var boldCells []int
	for row := 0; row < cutRows; row++ {
		text, styles := probe.screen.StyledLine(row)
		runes := []rune(text)
		if len(runes) != len(styles) {
			t.Fatalf("row %d has %d runes but %d styles: %q", row, len(runes), len(styles), text)
		}
		for i, rn := range runes {
			if styles[i].Bold {
				boldCells = append(boldCells, row*cutProbeCols+i)
				continue
			}
			if rn == 'b' || rn == 'o' || rn == 'l' || rn == 'd' {
				t.Errorf("row %d col %d (%q) is not bold: %q — the span lost a cell to the cut",
					row, i, string(rn), text)
			}
		}
	}
	// The other direction is asserted cell-by-cell above (a plain cell that came
	// out bold is reported there, and the plain run is long enough to share rows
	// with the span's tail whenever the cut falls inside the span). What is left
	// is the vacuity guard: the span must really have reached the grid.
	if len(boldCells) == 0 {
		t.Fatalf("no bold cell on the grid at all:\n%s", probe.screen.StyledContents())
	}
	t.Logf("bold cells: %d, first at row %d col %d", len(boldCells), boldCells[0]/cutProbeCols, boldCells[0]%cutProbeCols)
}
