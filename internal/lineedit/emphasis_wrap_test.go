package lineedit

import (
	"strings"
	"testing"
	"time"
)

// emphasis_wrap_test.go — the WRAP half of the emphasis defect, and the residual
// commit 9c6b8da8 left behind.
//
// The chunked fixtures beside this file assert a line that FITS one band row.
// The shape the operator still saw (measured live at 117x26: `Duplicated phrase
// `painting a partial chunk` with a stray ``**.`, `paints markers liter…` glued
// to itself, and span boundaries landing mid-word — `mark|ers`, `stil|l`) is a
// line long enough to FILL a band row. A row that fills the band is promoted
// into the band by writeLive, and the band never repaints a committed row — so
// the defect is once again a decision taken too late: the promoted row was the
// PROVISIONAL spelling (rowForPartial — markers kept literally, because removing
// one is part of a decision not taken yet), and when the line's break finally
// arrived the decoder decided the whole line but painted only its TAIL, against a
// band row still showing the provisional head's characters. Measured on the
// fixture below (60-cell band, one `**` span straddling the wrap):
//
//	chunk=6   row "…here **and the" / " bold conclusion follows** donconclusion follows** done"
//	          — markers on a committed row AND the provisional head re-drawn (the duplication)
//	chunk=6..32 the same, because what a wrapping line cannot do without is its own
//	          whole text, not a marker pair: at chunk=32 the line is still cut twice
//
// The fix is one seam, and it is a MEASUREMENT rather than a special case: a
// flush may not hand a wrapping line to writeLive in pieces (drain →
// unfinishedLineWouldWrap), so a line that would fill its row waits for its break
// and is then decided — and painted — from the WHOLE line (computeRows), exactly
// as a line that fits one row already was.
//
// What is asserted here is the RENDERED GRID (cells plus per-cell attributes),
// never a string equality on the source: the defect is a screen the reader saw,
// so the guard has to read the screen. Every case walks the delta sizes 1..16 —
// the sizes that were RED on master, and the reason a whole-line-in-one-flush
// fixture (chunk >= 32) passed over the defect.

const (
	// emphasisWrapBandCols is the band whose width the fixture is built around:
	// narrower than the 117-cell operator terminal (so the fixture is the small
	// sample, Rule 10.4) and wide enough that the line below wraps ONCE, which is
	// what puts the bold span astride the row boundary.
	emphasisWrapBandCols = 60
	// emphasisWrapRows fits the two rows the wrapped line paints plus the band's
	// own blank rows, so nothing asserted here has scrolled off.
	emphasisWrapRows = 8
)

// emphasisWrapSource is the answer line: 80 cells of prose with ONE bold span
// that no delta size 1..16 can keep on one side of the 60-cell boundary — its
// characters start at column 52 and run to column 74, so the row break lands
// inside the span, and the marked run is the only thing that may carry Bold.
const emphasisWrapSource = "the quick brown fox jumps over the marked run here **and the bold conclusion follows** done"

// emphasisWrapBold is the span as the format layer decides it: the text the
// tokenizer produces, with the markers gone.
const emphasisWrapBold = "and the bold conclusion follows"

// emphasisWrapScreen drives the SHIPPED renderer over the fixture cut into
// chunk-byte deltas — each delta separated by more than answerFlushWindow, so the
// cadence flush paints the unfinished line the way a real stream does — and
// returns the rendered grid.
func emphasisWrapScreen(t *testing.T, chunk int) *Screen {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(emphasisWrapRows, emphasisWrapBandCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", emphasisWrapRows, emphasisWrapBandCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(BasicPalette)
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()
	src := emphasisWrapSource + "\n"
	for i := 0; i < len(src); i += chunk {
		end := i + chunk
		if end > len(src) {
			end = len(src)
		}
		now = now.Add(emphasisChunkGap)
		r.Delta(src[i:end])
	}
	r.Tick()
	r.Finish()
	sc := NewScreen(emphasisWrapBandCols, emphasisWrapRows)
	sc.Feed([]byte(cap.bytes()))
	return sc
}

// wrapDrawnRow is one row the answer painted: its GRID row (what StyledLine is
// asked about) and its trimmed text. The two are kept together because the band
// leaves blank rows between the answer's rows (the region's tail), so a slice
// index is not a grid row — mixing them is what made an early draft of this file
// read the attributes of an empty row.
type wrapDrawnRow struct {
	grid int
	text string
}

// emphasisWrapRowsDrawn returns the grid rows the answer painted, in order.
func emphasisWrapRowsDrawn(sc *Screen) []wrapDrawnRow {
	var rows []wrapDrawnRow
	for i := 0; i < emphasisWrapRows; i++ {
		if l := strings.TrimRight(sc.Line(i), " "); l != "" {
			rows = append(rows, wrapDrawnRow{grid: i, text: l})
		}
	}
	return rows
}

// wrapRowTexts joins the drawn rows' texts — the cells the reader sees, in order.
func wrapRowTexts(rows []wrapDrawnRow) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(cutReadingFold(r.text))
	}
	return b.String()
}

// wrapRowBlock renders the drawn rows for a failure message.
func wrapRowBlock(rows []wrapDrawnRow) string {
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, r.text)
	}
	return strings.Join(lines, "\n")
}

// TestEmphasisWrappedLinePaintsTheDecidedRow is the regression: a line that
// WRAPS must paint the same cells at every delta size — once, with neither a
// marker nor a duplicated fragment left on a committed row.
//
// RED on master for every chunk in 1..16 (the row carried `**` literally and the
// provisional head re-drawn onto the row below); GREEN once the whole-line
// decision is taken before the wrap.
func TestEmphasisWrappedLinePaintsTheDecidedRow(t *testing.T) {
	drawn := DisplayText(emphasisWrapSource)
	for _, chunk := range []int{1, 2, 3, 4, 6, 9, 16} {
		t.Run("chunk="+itoa(chunk), func(t *testing.T) {
			rows := emphasisWrapRowsDrawn(emphasisWrapScreen(t, chunk))
			if len(rows) < 2 {
				t.Fatalf("the line painted %d row(s), want it wrapped over more than one (the fixture is longer than the %d-cell band):\n%s",
					len(rows), emphasisWrapBandCols, wrapRowBlock(rows))
			}
			// The rows are the answer's cells in order: concatenated, they are the
			// drawn line EXACTLY ONCE. That single assertion pins both shapes the
			// operator reported — a fragment drawn twice (the concatenation would be
			// longer than the line) and text lost (it would be shorter) — and it is
			// measured on the grid rather than on the source string, because the
			// source still carries the markers the paint must consume.
			if got := wrapRowTexts(rows); got != drawn {
				t.Fatalf("the committed rows read %q, want %q — the wrapped line must be painted once, decided:\n%s",
					got, drawn, wrapRowBlock(rows))
			}
			for i, row := range rows {
				if strings.Contains(row.text, BoldMarker) {
					t.Fatalf("row %d carries the literal marker %q: a committed band row is never repainted, so a provisional paint's markers may not reach it —\n%s",
						i, BoldMarker, wrapRowBlock(rows))
				}
				if w := displayWidth(row.text); w > emphasisWrapBandCols {
					t.Fatalf("row %d is %d cells wide, want at most %d", i, w, emphasisWrapBandCols)
				}
			}
		})
	}
}

// TestEmphasisWrappedLineKeepsTheBoldRunContiguous is the emphasis half: the
// marked run must carry Bold on every one of its cells ACROSS the row boundary,
// and on no cell outside it.
//
// The boundary is what this adds to the chunked fixtures: a renderer that decides
// the row's emphasis per ROW (rather than per line, and then carries `skip` into
// the continuation) drops the emphasis on the second row, and a provisional row
// that is committed carries the markers as PLAIN cells instead of the styled run.
func TestEmphasisWrappedLineKeepsTheBoldRunContiguous(t *testing.T) {
	for _, chunk := range []int{1, 2, 6, 16} {
		t.Run("chunk="+itoa(chunk), func(t *testing.T) {
			sc := emphasisWrapScreen(t, chunk)
			rows := emphasisWrapRowsDrawn(sc)
			if len(rows) < 2 {
				t.Fatalf("the line painted %d row(s), want more than one:\n%s", len(rows), wrapRowBlock(rows))
			}
			start := strings.Index(cutReadingFold(rows[0].text), emphasisWrapBold[:wrapBoldHeadCells])
			if start < 0 {
				t.Fatalf("row 0 does not carry the marked run's opening cells, so the fixture no longer straddles the row boundary — it is the wrap this guard exists for: %q",
					rows[0].text)
			}
			// Walk the marked run's cells in grid order — the tail of row 0 and the
			// head of the rows below, i.e. the cells the row boundary splits — and
			// require each of them to be the run's own character AND to carry Bold.
			// The rows are walked as SOURCE: the cut label is folded away, because
			// it is a RENDERING glyph standing in the cell of a character that moved
			// to the row below (see cutReadingFold) — the run's own characters are
			// what this case is about, and the label's bytes would be read as one.
			// The attribute array below is the RAW grid's (one entry per drawn cell),
			// and the label is a row's LAST cell, so every column this walk visits
			// keeps its own index.
			texts := make([]string, 0, len(rows))
			for _, r := range rows {
				texts = append(texts, cutReadingFold(r.text))
			}
			seen, ri, col := 0, 0, start
			for seen < len(emphasisWrapBold) {
				if col >= len(texts[ri]) {
					ri, col = ri+1, 0
					if ri >= len(rows) {
						t.Fatalf("the marked run was cut short after %d of its %d cells:\n%s",
							seen, len(emphasisWrapBold), wrapRowBlock(rows))
					}
					continue
				}
				if got := texts[ri][col]; got != emphasisWrapBold[seen] {
					t.Fatalf("cell %d of the marked run (row %d, column %d) is %q, want %q: the run's characters must survive the wrap in order —\n%s",
						seen, ri, col, string(got), string(emphasisWrapBold[seen]), wrapRowBlock(rows))
				}
				_, styles := sc.StyledLine(rows[ri].grid)
				if col >= len(styles) {
					t.Fatalf("the grid records no attribute for row %d column %d (row text %q)", ri, col, rows[ri].text)
				}
				if !styles[col].Bold {
					t.Fatalf("cell %d of the marked run (row %d, column %d) is not Bold: the run crosses the row boundary and every cell of it must stay marked —\n%s",
						seen, ri, col, wrapRowBlock(rows))
				}
				seen, col = seen+1, col+1
			}
			if ri == 0 {
				t.Fatalf("the marked run fits inside row 0, so this case no longer exercises the wrap:\n%s", wrapRowBlock(rows))
			}
			// Nothing outside the run may be bold: a fix that emphasised the row
			// rather than the span would pass every check above.
			if col < len(texts[ri]) {
				if _, styles := sc.StyledLine(rows[ri].grid); col < len(styles) && styles[col].Bold {
					t.Fatalf("the cell after the marked run (row %d, column %d) is Bold, want the row's plain style:\n%s",
						ri, col, wrapRowBlock(rows))
				}
			}
		})
	}
}

// wrapBoldHeadCells is how much of the marked run must still be on the FIRST row
// for the fixture to be the straddling shape this file exists to guard: a span
// wholly inside one row is the chunked fixtures' case, and asserting on it here
// would let the wrap case regress unnoticed.
//
// It is 7 rather than 8 because the band moves a halved token DOWN WHOLE
// (band_word_move.go): the space in front of the run used to be the row's last
// cell and now opens the row below, so `and the` is what remains of the run on
// row 0. The span itself still crosses the boundary — the shape this constant
// exists to require — and the walk below asserts it cell for cell.
const wrapBoldHeadCells = 7
