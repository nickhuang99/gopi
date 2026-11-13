//go:build linux

package lineedit

import (
	"io"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// emphasis_wrapspace_ledger_test.go — THE WRAP SPACE'S EFFECT ON THE SPAN LEDGER.
//
// The full account (root cause, the byte-space mismatch, and the recipe) is
// devdoc/answer-wrapspace-span-ledger.md.
//
// THE DEFECT (measured 2026-09-28 on the operator's 117-column chat capture): an
// emphasis span that follows rows the wrap CONSUMED SPACES from is drawn OFF ITS
// OWN CHARACTERS — the marked cells are shifted, so plain prose is emphasised and
// the run's true tail comes out plain. The offset is not content-derived (the
// highlighted string is not in the source), it is a RENDER-LAYER byte
// bookkeeping error, and it ACCUMULATES one character per space-consuming wrap:
// the operator's dump showed 17 columns of displacement over a body that wrapped
// the 117-cell band 17 times.
//
// THE MECHANISM (one seam, two coordinate spaces). writeRow advanced the span
// ledger with `row.skip += len(displayed)` — a count of bytes DRAWN. But the wrap
// path then calls trimWrapSpace(tail), which DROPS the space(s) at the head of
// the continuation row (the gutter has already re-opened the block's column, so
// drawing them would push the row's first character one cell past it). The
// dropped bytes are still part of row.text, and piecesFor indexes the SPANS —
// which are built over row.text, wrap spaces included — by row.skip. So after
// every space-consuming wrap the ledger was one byte SHORT of the text left to
// walk, the span walk started that many characters early, and the style a drawn
// cell carried belonged to a source character BEFORE it.
//
// WHY THE ASSERTION IS PER CELL ON A GRID. The reader saw a screen, so the judge
// is a screen: the span's characters are one rune and the prose another, so "the
// style follows the span it is in" is a statement about CELLS, independent of any
// wrap arithmetic — and independent of WHERE the row boundary fell, which a
// string equality on the source could never be (the wrap legitimately drops the
// spaces it consumes).
//
// The terminal is a REAL pty at the operator's own width (117): the geometry is
// the kernel's answer through production's own reader (Terminal.Width), not a
// literal this file believes.

// The geometry and the fixture's own numbers. Every one is named because the case
// is ABOUT the numbers: the width is the operator's terminal, the unit count is
// the number of space-consuming wraps, and the bold run is long enough that a
// shifted ledger cannot land it inside its own characters by luck.
const (
	// wrapLedgerPtyCols is the operator's capture width. A width the 80-column
	// default would visibly mis-read, and wide enough that the fixture's rows are
	// decided by cutCols rather than by a word wrap.
	wrapLedgerPtyCols = 117
	// wrapLedgerPtyRows hosts the turn's chrome plus 17 wrapped body rows plus the
	// band's tail, so no asserted row has scrolled off.
	wrapLedgerPtyRows = 40
	// wrapLedgerUnitRune fills each wrapped row: a rune that is neither the bold
	// run's rune nor a space, so a cell's expected emphasis is a function of the
	// rune in it.
	wrapLedgerUnitRune = 'y'
	// wrapLedgerSpanRune is the bold run's single rune — the bold span is a run of
	// it, so the grid can be asked "did the marked characters carry the mark".
	wrapLedgerSpanRune = 'B'
	// wrapLedgerSpanRunes is the bold run's length. Longer than one cell by a wide
	// margin, so a one-character ledger error is visible as a marked cell outside
	// the run and an unmarked cell inside it.
	wrapLedgerSpanRunes = 9
	// wrapLedgerUnits is how many space-consuming wraps precede the span. It is
	// the DISPLACEMENT the defect accumulated, in characters (one per consumed
	// wrap space) — the operator's 17.
	wrapLedgerUnits = 17
	// wrapLedgerTail is prose AFTER the bold run: it makes the run an INTERIOR span
	// rather than a suffix, so the assertion reads both edges of the marked region.
	wrapLedgerTail = " tail words after the span"
)

// wrapLedgerGutterCells is the turn chrome's width. body cells per row = cols -
// this, and the fixture's filler is sized from it so each row fills the band
// exactly and the space that follows it is the one the wrap consumes.
func wrapLedgerGutterCells() int { return displayWidth(TurnMarker) }

// wrapLedgerSource builds the fixture: `wrapLedgerUnits` rows of filler, each
// exactly as wide as a body row, each followed by the space that row's wrap
// consumes — and then the marked run and its tail. The result is ONE line (no
// break), so it is one answerRow whose spans are walked across every row it
// wraps onto: the ledger's accumulation is what is under test.
func wrapLedgerSource(cols, units int) string {
	unit := strings.Repeat(string(wrapLedgerUnitRune), cols-wrapLedgerGutterCells()) + " "
	var b strings.Builder
	b.WriteString(strings.Repeat(unit, units))
	b.WriteString(BoldMarker)
	b.WriteString(strings.Repeat(string(wrapLedgerSpanRune), wrapLedgerSpanRunes))
	b.WriteString(BoldMarker)
	b.WriteString(wrapLedgerTail)
	return b.String()
}

// wrapLedgerPTY returns a pty sized to the operator's geometry, the width read
// back through production's own reader so the assertion is anchored on the
// kernel's answer rather than on a literal.
func wrapLedgerPTY(t *testing.T) (*Terminal, *os.File, *os.File) {
	t.Helper()
	master, slave := openInputPendingPTY(t)
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: wrapLedgerPtyRows, Col: wrapLedgerPtyCols}); err != nil {
		t.Fatalf("TIOCSWINSZ on the pty slave: %v", err)
	}
	term, err := Open(slave, io.Discard)
	if err != nil {
		t.Fatalf("lineedit.Open on a pty slave: %v", err)
	}
	if got := term.Width(0); got != wrapLedgerPtyCols {
		t.Fatalf("the pty reports %d columns, want %d — the fixture's geometry is the terminal's", got, wrapLedgerPtyCols)
	}
	return term, master, slave
}

// wrapLedgerScreenUnits drives the SHIPPED AnswerRenderer, at the pty's width,
// inside a real turn (the turn's gutter is what makes the wrap consume its space),
// over `units` wrapped filler rows followed by the marked run, and returns the
// grid its bytes produce.
func wrapLedgerScreenUnits(t *testing.T, units int) *Screen {
	t.Helper()
	term, master, slave := wrapLedgerPTY(t)
	defer slave.Close()  //stylegate:ignore the harness's own pty teardown
	defer master.Close() //stylegate:ignore the harness's own pty teardown
	defer term.Close()
	cols := term.Width(0)
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(wrapLedgerPtyRows, cols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", wrapLedgerPtyRows, cols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(BasicPalette)
	r.BeginTurn(TurnInfo{Index: 1, Prompt: "wrap space ledger"})
	r.Delta(wrapLedgerSource(cols, units) + "\n")
	r.Tick()
	r.Finish()
	r.EndTurn()
	sc := NewScreen(cols, wrapLedgerPtyRows)
	sc.Feed([]byte(cap.bytes()))
	return sc
}

// wrapLedgerMark is one grid cell the measurement read: where it is, and whether
// it carries Bold.
type wrapLedgerMark struct {
	row  int
	col  int
	bold bool
}

// wrapLedgerMeasure reads the whole grid: every cell whose rune is the run's rune
// (the run's own cells, wherever the renderer put them), every cell that carries
// Bold although its rune is NOT one of the run's (a foreign cell the mark landed
// on), and a printable dump of the drawn rows for the failure message.
func wrapLedgerMeasure(sc *Screen) (run []wrapLedgerMark, stray []wrapLedgerMark, dump string) {
	for row := 0; row < sc.Rows; row++ {
		text, styles := sc.StyledLine(row)
		if isWrapLedgerChrome(text) {
			// The turn's OWN chrome, not the answer's body: the echoed PROMPT row
			// and the header's label rule. The prompt is drawn with the palette's
			// Prompt style, which is bold BY DESIGN, so a scan that walks the whole
			// band reads the palette's boldness as the ledger's error — measured
			// 2026-09-29: this fixture reported 19 "foreign" bold cells per case and
			// every one of them sat on the prompt row. The ledger is a statement
			// about the BODY.
			continue
		}
		for i, rn := range []rune(text) {
			if i >= len(styles) {
				break
			}
			if rn == wrapLedgerSpanRune {
				run = append(run, wrapLedgerMark{row: row, col: i, bold: styles[i].Bold})
			}
			if styles[i].Bold && rn != wrapLedgerSpanRune {
				stray = append(stray, wrapLedgerMark{row: row, col: i, bold: true})
			}
		}
	}
	var b strings.Builder
	for row := 0; row < sc.Rows; row++ {
		text, _ := sc.StyledLine(row)
		if strings.TrimRight(text, " ") == "" {
			continue
		}
		b.WriteString(strings.TrimRight(text, " "))
		b.WriteString("\n")
	}
	return run, stray, b.String()
}

// isWrapLedgerChrome reports whether a band row is the turn's own chrome rather
// than one of the answer's body rows: the echoed prompt row (RequestGutter) and
// the header's label rule, which opens with the rule glyph. Both are drawn before
// the answer's first body row and neither carries a cell of it.
func isWrapLedgerChrome(text string) bool {
	return strings.HasPrefix(text, RequestGutter) || strings.HasPrefix(text, TurnRuleGlyph)
}

// wrapLedgerReport renders the measurement for a failure message: where the run's
// cells are, which of them carry the mark, and which foreign cells do.
func wrapLedgerReport(run, stray []wrapLedgerMark) string {
	var b strings.Builder
	for i, m := range run {
		b.WriteString(itoa(i) + ": grid " + itoa(m.row) + ":" + itoa(m.col) + " bold=" + wrapLedgerBoldWord(m.bold) + "\n")
	}
	for i, m := range stray {
		b.WriteString("stray " + itoa(i) + ": grid " + itoa(m.row) + ":" + itoa(m.col) + " carries Bold but is not a rune of the marked run\n")
	}
	return b.String()
}

func wrapLedgerBoldWord(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// TestEmphasisSpanAfterAWrapSpaceKeepsItsOwnCells is the defect's regression, at
// the operator's own width: on a body that wraps the band, an emphasis span's own
// cells carry the mark and no other cell does.
//
// RED on master: the ledger was one byte short per consumed wrap space, so the
// mark landed wrapLedgerUnits characters away from the run's own text — plain
// prose marked, every character of the run plain.
func TestEmphasisSpanAfterAWrapSpaceKeepsItsOwnCells(t *testing.T) {
	sc := wrapLedgerScreenUnits(t, wrapLedgerUnits)
	run, stray, dump := wrapLedgerMeasure(sc)
	if len(run) != wrapLedgerSpanRunes {
		t.Fatalf("the grid spells %d of the marked run's %d characters — the drawn body must still show every character of the answer once:\n%s",
			len(run), wrapLedgerSpanRunes, dump)
	}
	plain := 0
	for _, m := range run {
		if !m.bold {
			plain++
		}
	}
	if plain > 0 || len(stray) > 0 {
		t.Fatalf("the marked run is off its own cells: %d of its %d characters are plain and %d foreign cell(s) carry Bold (the ledger is short by one byte per consumed wrap space, and this body wraps the band %d time(s)):\n%sgrid rows:\n%s",
			plain, wrapLedgerSpanRunes, len(stray), wrapLedgerUnits,
			wrapLedgerReport(run, stray), dump)
	}
}

// TestEmphasisWrapSpaceLedgerHoldsAtEveryWrapCount is the SIZE guard: the error
// was not a constant of the renderer but one character per space-consuming wrap,
// so a fixture measured at ONE wrap count cannot tell "the ledger is right" from
// "the ledger is off by a number that happens to be invisible here". The fixture
// is rendered at several wrap counts and the mark must sit on the run's own cells
// at every one of them, with no foreign cell carrying it.
//
// RED on master: the displacement EQUALS the wrap count (the operator's 17-column
// measurement, reproduced as a function of the input rather than as a constant).
func TestEmphasisWrapSpaceLedgerHoldsAtEveryWrapCount(t *testing.T) {
	for _, units := range []int{1, 2, 5, wrapLedgerUnits, wrapLedgerUnits + 2} {
		t.Run("wraps="+itoa(units), func(t *testing.T) {
			sc := wrapLedgerScreenUnits(t, units)
			run, stray, dump := wrapLedgerMeasure(sc)
			if len(run) != wrapLedgerSpanRunes {
				t.Fatalf("grid spells %d of the run's %d characters:\n%s", len(run), wrapLedgerSpanRunes, dump)
			}
			plain := 0
			for _, m := range run {
				if !m.bold {
					plain++
				}
			}
			if plain > 0 || len(stray) > 0 {
				t.Fatalf("at %d space-consuming wrap(s) the mark is off its own cells: %d of the run's %d characters plain, %d foreign cell(s) marked:\n%sgrid rows:\n%s",
					units, plain, wrapLedgerSpanRunes, len(stray), wrapLedgerReport(run, stray), dump)
			}
		})
	}
}
