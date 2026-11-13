package lineedit

// screen_cell_reconcile_test.go — CELL-LEVEL RECONCILIATION of a paint byte
// stream against the text it was meant to draw, at EVERY chunk boundary.
//
// WHY THIS FILE EXISTS (task 01a0e825, an independent verification of the
// delivered fix e06a2aed). The fix for the escape-fragment family asserted ONE
// direction: "no CSI parameter run reaches a cell". That assertion is
// incomplete, and the incompleteness is not hypothetical — the live evidence
// this task was opened for shows the other direction on the SAME row:
//
//	artifact row 011 (session 884eca28, pre-fix frame; the verbatim bytes are
//	quoted in devdoc/escape-introducer-cell-reconcile.md):
//	  `  path]` 当 下 文 回 显 ） 我 建 议 1m──────────…`
//	the answer that row was drawn from (the session's own transcript):
//	  `  path]` 当**主场景**来定义它的显示形态。另外第三步（终端上 下文 回显）我建议**先不立**，等你确认那是你想要的。`
//
// Two things are wrong there, and only the FIRST is covered by "no fragment in
// a cell":
//
//	LOST  — the answer's own cells are missing, and the text that SHOULD have
//	        been positioned by the lost placement is not at its intended column
//	        either: real characters of the answer are absent from the row;
//	EXTRA — `1m`, the parameter run of `ESC[1m`, was painted as cells with no
//	        counterpart in the answer.
//
// So the criterion here is the RECONCILIATION, both directions at once: the
// cells the producer meant to draw and the cells the grid holds must agree
// COLUMN BY COLUMN — 0 lost, 0 extra. A grid carrying a fragment fails; so does
// a grid that dropped real text and left no fragment behind, which a
// fragment-only assertion can never see.
//
// THE ORACLE IS THE PRODUCER'S TEXT, never the grid: `expected` is the text the
// producer meant to draw, and the painted side is read back through the
// package's public cell API (Screen.Cell). A defect in the interpreter cannot
// hide inside the oracle.
//
// THE CHUNK BOUNDARY IS THE VARIABLE, NOT THE FIXTURE: a terminal is a byte
// stream and the split between two reads is the OS's choice, so every fixture
// is fed at EVERY 2-way split offset (plus a 3-way grid) and must reconcile
// identically at all of them — the criterion screen_chunk_carry_test.go asserts
// for whole-row text, applied here to per-CELL content and its attributes.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sgr"
)

// recCols/recRows is the geometry the reconciliation cases paint into: wide
// enough for the fixture's row, short enough that a stray placement or a stray
// row is visible.
const (
	recCols = 60
	recRows = 3
)

// recFixture is one reconciliation case: the text the producer meant to draw,
// the byte stream that draws it, the style that text carries, and the row it
// must land on.
type recFixture struct {
	name     string
	expected string    // the text that must occupy the row's leading cells
	stream   string    // the bytes that draw it (placements, SGRs, text)
	style    sgr.Style // the style `expected` is painted in
	row      int       // the grid row `expected` must land on
}

// recPlacement is the cursor placement the chat band positions its rows with,
// taken from its ONE definition (sgr.CursorSeq) so the fixture cannot drift away
// from what the producer writes.
func recPlacement(row, col int) string { return sgr.CursorSeq(row, col) }

// recFixtures are the reconciliation cases. Each is a shape a real producer
// writes; the read offsets are the test's variable, never the fixture's.
func recFixtures() []recFixture {
	def := sgr.Default()
	bold := def
	bold.Bold = true

	// The third sequence's style comes from the ONE definition of what an SGR
	// parameter MEANS (sgr.ApplyParam), never a hand-built Style: the oracle has
	// to agree with the interpreter about `38;5;230` for the style leg to be
	// about the interpreter rather than about the test.
	col230 := sgr.Default()
	sgr.ApplyParam(&col230, []int{38, 5, 230}, 0)

	return []recFixture{
		{
			// The plainest case: text drawn at a placed column. It is the
			// instrument's CONTROL — a case that fails on a whole-stream feed
			// means the reconciliation reader is wrong, not the interpreter.
			name:     "placed text",
			expected: "abc",
			stream:   recPlacement(1, 1) + "abc",
			style:    def,
			row:      0,
		},
		{
			// An SGR whose PARAMETERS are what leaks when the introducer is lost:
			// the producer of the observed `1m` cell run, plus the style leg
			// (the `abc` must be bold and nothing else must be).
			name:     "emphasis span after a placement",
			expected: "abc",
			stream:   recPlacement(1, 1) + sgr.BoldSeq() + "abc" + sgr.ResetSeq(),
			style:    bold,
			row:      0,
		},
		{
			// The full observed shape: a placement, a second placement (the
			// band's own editing row), an SGR, then text. Its parameter run, with
			// every introducer removed, is the literal `;61H24;73H5;230m` the live
			// artifact was opened for.
			name:     "the observed fragment's three sequences",
			expected: "x",
			stream:   recPlacement(1, 1) + recPlacement(2, 1) + sgr.FgSeq(230) + "x" + sgr.ResetSeq(),
			style:    col230,
			row:      1,
		},
		{
			// A wide (CJK) rune split across two reads: the other hold path, and
			// the one that costs REAL TEXT rather than a placement.
			name:     "wide rune text",
			expected: "主场景",
			stream:   recPlacement(1, 1) + "主场景",
			style:    def,
			row:      0,
		},
	}
}

// recReading is one reconciliation verdict.
type recReading struct {
	lostRunes int    // produced cells whose rune is not where the producer put it
	lostStyle int    // produced cells drawn in the wrong style
	extra     int    // grid cells past the produced text, with no counterpart
	got       string // the row's leading cells, as read back
	tail      string // the row's cells past the produced text
}

// blank reports whether the reading is a pass: nothing lost, nothing extra.
func (r recReading) blank() bool { return r.lostRunes == 0 && r.lostStyle == 0 && r.extra == 0 }

// recReconcile compares the grid row against the produced text COLUMN BY COLUMN
// and returns both verdicts: how many produced cells are missing from the grid
// (wrong rune, and wrong style), and how many grid cells have no counterpart in
// the produced text.
//
// The comparison is positional and cell-exact. A wide rune is ONE rune in the
// produced text and TWO columns in the grid (its second column is the
// continuation cell), so the produced text is walked by RUNE and compared
// against the grid at the rune's column — the one asymmetry a cell comparison
// must model, and the reason the expected side is not spelled with
// continuation cells by hand.
func recReconcile(s *Screen, f recFixture) recReading {
	var out recReading
	var got, tail strings.Builder

	// The walk advances by the rune's own CELL WIDTH, because that is what the
	// grid stores: a wide rune's second column is its continuation cell, so the
	// next produced rune starts two columns later, not one.
	col := 0
	for _, r := range []rune(f.expected) {
		cell := s.Cell(f.row, col)
		got.WriteRune(cell.Rune)
		if cell.Rune != r {
			out.lostRunes++
		}
		if !cell.Style.Equal(f.style) {
			out.lostStyle++
		}
		col += recCellWidth(r)
	}

	// The columns a wide rune adds are part of the produced cell run: past the
	// last produced column the row must hold NOTHING the producer did not draw —
	// a cell there is a character the producer never wrote, which is where a
	// fragment's parameters show up and where `1m` showed up on the live
	// artifact.
	firstFree := col
	for c := firstFree; c < s.Cols; c++ {
		cell := s.Cell(f.row, c)
		if cell.Kind == CellEmpty {
			continue
		}
		tail.WriteRune(cell.Rune)
		out.extra++
	}
	out.got = got.String()
	out.tail = tail.String()
	return out
}

// recCellWidth is how many grid columns one produced rune occupies — the width
// the GRID itself uses, read off a one-cell probe rather than re-derived here,
// so the test and the interpreter cannot disagree about "wide".
func recCellWidth(r rune) int {
	probe := NewScreen(2, 1)
	probe.Feed([]byte(string(r)))
	if probe.isContinuation(0, 1) {
		return 2
	}
	return 1
}

// recPaint feeds a byte stream the way a reader hands it over: in chunks split
// at offsets a and b of a fresh Screen. b < 0 means a 2-way split at a; a >=
// len(stream) means the whole stream in one feed (the reference).
func recPaint(stream string, a, b int) *Screen {
	s := NewScreen(recCols, recRows)
	buf := []byte(stream)
	cut := func(n int) int {
		if n < 0 {
			return len(buf)
		}
		if n > len(buf) {
			return len(buf)
		}
		return n
	}
	first := cut(a)
	s.Feed(buf[:first])
	if first >= len(buf) {
		return s
	}
	second := cut(b)
	if second < first {
		second = first
	}
	s.Feed(buf[first:second])
	if second < len(buf) {
		s.Feed(buf[second:])
	}
	return s
}

// recReport renders a reading for a failure message: the two directions the epic
// asks for, as numbers.
func recReport(f recFixture, at string, r recReading) string {
	return "read split " + at + ": the painted cells do not reconcile with the produced text\n" +
		"  lost runes:  " + strconv.Itoa(r.lostRunes) + "\n" +
		"  lost styles: " + strconv.Itoa(r.lostStyle) + "\n" +
		"  extra cells: " + strconv.Itoa(r.extra) + "\n" +
		"  got:  " + strconv.Quote(r.got) + "\n" +
		"  want: " + strconv.Quote(f.expected) + "\n" +
		"  cells past the produced text: " + strconv.Quote(r.tail) + "\n" +
		"  stream: " + strconv.Quote(f.stream)
}

// TestScreenReconcilesThePaintedCellsWithTheProducedText is the guard: for every
// fixture, at EVERY 2-way split offset and a 3-way grid, the grid must carry
// exactly the produced text — 0 lost, 0 extra — column for column.
func TestScreenReconcilesThePaintedCellsWithTheProducedText(t *testing.T) {
	for _, f := range recFixtures() {
		t.Run(f.name, func(t *testing.T) {
			// The reference: the whole stream in one feed. A fixture that does
			// not reconcile here is mis-specified (the oracle and the stream
			// disagree) and no split offset could be blamed for it.
			if r := recReconcile(recPaint(f.stream, len(f.stream), -1), f); !r.blank() {
				t.Fatalf("REFERENCE (whole stream in one feed) does not reconcile with its own text — the fixture is mis-specified, not the interpreter:\n%s",
					recReport(f, "none", r))
			}
			// Every 2-way boundary.
			for at := 1; at < len(f.stream); at++ {
				if r := recReconcile(recPaint(f.stream, at, -1), f); !r.blank() {
					t.Fatalf("%s", recReport(f, "at byte "+strconv.Itoa(at)+" of "+strconv.Itoa(len(f.stream)), r))
				}
			}
			// A 3-way grid over the same space: two independent boundaries in one
			// stream is where a carry policy that is right once can still be wrong.
			for a := 1; a < len(f.stream); a += 3 {
				for b := a; b < len(f.stream); b += 5 {
					if r := recReconcile(recPaint(f.stream, a, b), f); !r.blank() {
						t.Fatalf("%s", recReport(f, "at bytes "+strconv.Itoa(a)+" and "+strconv.Itoa(b)+" of "+strconv.Itoa(len(f.stream)), r))
					}
				}
			}
		})
	}
}

// TestScreenReconcilesAfterAnOverlongHold pins the OTHER carry-policy boundary:
// a stream that never completes its sequence (an unterminated OSC — a real
// bubbletea background query is `ESC]11;?`), whose payload is longer than the
// carry bound. Bounding the held state by keeping the LAST maxPendingEscape bytes
// throws the introducer away and re-offers the body as ordinary characters, so the
// held sequence's own payload paints as cells.
//
// THE CRITERION is the same reconciliation: the row must carry the text that was
// drawn after the stream resynchronized, and NOTHING of the held payload.
func TestScreenReconcilesAfterAnOverlongHold(t *testing.T) {
	const resyncText = "abc"
	s := NewScreen(recCols, recRows)
	// An unterminated OSC carrying far more payload than the bound allows.
	s.Feed([]byte("\x1b]11;?" + strings.Repeat("SEMI;9", maxPendingEscape)))
	if len(s.pending) > maxPendingEscape {
		t.Fatalf("the carry buffer grew to %d bytes, bound is %d: a stream that never completes a sequence must not grow the parser's state without limit",
			len(s.pending), maxPendingEscape)
	}
	// The stream resynchronizes: ordinary text, drawn at the home column.
	s.Feed([]byte(resyncText))

	f := recFixture{name: "text after an overlong hold", expected: resyncText, style: sgr.Default(), row: 0}
	if r := recReconcile(s, f); !r.blank() {
		t.Fatalf("%s\n  (the held sequence's payload was re-offered without its introducer and painted as cells)",
			recReport(f, "after an overlong hold of "+strconv.Itoa(maxPendingEscape), r))
	}
}

// TestScreenReconcilesAcrossAReMeasure pins the boundary a REAL reader inserts:
// the layout re-measures the terminal between two reads (the dump sink resizes
// its grid when the geometry is published), and the re-measure lands wherever
// the reader happened to be — including between a sequence's introducer and its
// parameters.
//
// This leg isolates the direction a fragment assertion cannot see: the text the
// lost placement was positioning is drawn in the WRONG COLUMN (lost at the
// intended one) while the sequence's parameters become cells (extra).
func TestScreenReconcilesAcrossAReMeasure(t *testing.T) {
	const (
		wantText = "abc"
		placeCol = 5 // the 1-based column the placement puts the text at
	)
	place := recPlacement(1, placeCol)
	for split := 1; split <= len(place); split++ {
		s := NewScreen(recCols, recRows)
		s.Feed([]byte(place[:split])) // the introducer (or part of it) alone
		s.Resize(recCols-1, recRows)  // the re-measure lands HERE
		s.Feed([]byte(place[split:] + wantText))

		f := recFixture{
			name:     "placed text after a re-measure",
			expected: strings.Repeat(" ", placeCol-1) + wantText,
			style:    sgr.Default(),
			row:      0,
		}
		if r := recReconcile(s, f); !r.blank() {
			t.Fatalf("%s", recReport(f, "at byte "+strconv.Itoa(split)+" of "+strconv.Quote(place)+" (with a re-measure in between)", r))
		}
		if got := s.Line(0); got != f.expected {
			t.Fatalf("a re-measure between a placement's introducer and its parameters (split at byte %d of %q):\n  row:  %q\n  want: %q\n  — the placement was discarded, so the text it positioned landed in the wrong column and the sequence's parameters painted as cells",
				split, place, got, f.expected)
		}
	}
}
