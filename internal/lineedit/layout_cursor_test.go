package lineedit

import (
	"strings"
	"testing"
)

// layoutCursorWidths is the boundary set the wide-terminal defect was found on:
// the sliver widths a terminal can report, then 40/80/200 ± one — the widths at
// which a full row, a partial row, and the "wide terminal" case disagree.
var layoutCursorWidths = []int{1, 2, 39, 40, 41, 79, 80, 81, 199, 200}

// layoutCursorText is the buffer the defect was measured with: 100 unbroken
// runes, long enough to wrap at every width in the set.
var layoutCursorText = strings.Repeat("a", 100)

// TestLayoutTextCursorStaysInsideItsRow is the U3 gate: for every (width, cursor)
// the reported cursor cell must be a cell OF THE ROW it names.
//
// The pre-fix failure (measured on master): a row that filled every column —
// `you> ` plus 35 runes at width 40 — was reported at column 40, which is not a
// cell of any row. The terminal-side effect is worse than an off-by-one: a
// cursor addressed to column == width sits in the PENDING-WRAP state, and the
// next repaint's CR+LF then arrives as a bare LF (raw mode cleared OPOST), so
// the line walks one blank row down per wrapped row.
func TestLayoutTextCursorStaysInsideItsRow(t *testing.T) {
	for _, width := range layoutCursorWidths {
		for _, cursor := range layoutCursorSet(len(layoutCursorText), width) {
			rows, row, col := layoutText(promptForTest, layoutCursorText, cursor, width)
			if row < 0 || row >= len(rows) {
				t.Fatalf("width=%d cursor=%d: cursorRow %d outside the %d drawn rows",
					width, cursor, row, len(rows))
			}
			if col < 0 || col >= width {
				t.Errorf("width=%d cursor=%d: cursorCol %d outside [0,%d) — reported (row=%d, col=%d) over %d rows",
					width, cursor, col, width, row, col, len(rows))
				continue
			}
			assertCursorCellIsTheRowsOwnCell(t, width, cursor, rows, row, col)
		}
	}
}

// TestLayoutTextCursorStaysInsideItsRowCJK runs the same gate over WIDE runes:
// two cells each, so a row boundary must never split one and the cursor must
// land on a cell boundary rather than inside a double-width character.
func TestLayoutTextCursorStaysInsideItsRowCJK(t *testing.T) {
	text := strings.Repeat("知", 40)
	for _, width := range layoutCursorWidths {
		for _, cursor := range layoutCursorSet(len([]rune(text)), width) {
			rows, row, col := layoutText(promptForTest, text, cursor, width)
			if row < 0 || row >= len(rows) {
				t.Fatalf("CJK width=%d cursor=%d: cursorRow %d outside the %d drawn rows",
					width, cursor, row, len(rows))
			}
			if col < 0 || col >= width {
				t.Errorf("CJK width=%d cursor=%d: cursorCol %d outside [0,%d)", width, cursor, col, width)
				continue
			}
			if width < 2 {
				// A 1-column terminal cannot host a 2-cell rune at all: the
				// rune must be written whole (splitting it would be worse), so
				// only the cursor-cell invariant is checked, not the row width.
				if col != 0 {
					t.Errorf("CJK width=%d cursor=%d: cursorCol %d on a 1-column terminal, want 0", width, cursor, col)
				}
				continue
			}
			assertCursorCellIsTheRowsOwnCell(t, width, cursor, rows, row, col)
		}
	}
}

// TestLayoutTextFullRowCursorHoldsThePendingWrapCell is the minimal regression
// (Rule 5) for the defect as measured: a row that fills every column.
//
// Red before the fix — `layoutText("you> ", strings.Repeat("a", 35), 35, 40)`
// returned col 40 on a row that is 40 cells wide — green after, because the
// cursor's cell on a full row is its LAST cell, which is where a terminal puts
// it (the cell holding the pending wrap).
func TestLayoutTextFullRowCursorHoldsThePendingWrapCell(t *testing.T) {
	const width = 40
	const typed = 35 // promptForTest (5 cells) + 35 = exactly width
	text := strings.Repeat("a", typed)
	rows, row, col := layoutText(promptForTest, text, typed, width)
	if len(rows) != 1 {
		t.Fatalf("layout produced %d rows, want 1 (the buffer fits exactly)", len(rows))
	}
	if row != 0 {
		t.Fatalf("cursor row = %d, want 0", row)
	}
	if col != width-1 {
		t.Fatalf("cursor col = %d on a full %d-cell row, want its last cell %d "+
			"(col == width arms the pending wrap and the next CR+LF drops a row)", col, width, width-1)
	}
	if displayWidth(rows[0]) != width {
		t.Fatalf("row 0 is %d cells, want the full %d", displayWidth(rows[0]), width)
	}
}

// TestLayoutTextCursorRowMatchesTheRowTheCursorTextIsOn pins the ROW half of the
// invariant at the wrap boundaries: the row layoutText names must be the row the
// cursor's own text lands on, not row 0 by default.
func TestLayoutTextCursorRowMatchesTheRowTheCursorTextIsOn(t *testing.T) {
	const width = 40
	// 5 prompt cells + 100 runes at 40 columns: rows of 40, 40, 25.
	for _, tc := range []struct {
		cursor int
		want   int
		why    string
	}{
		{cursor: 0, want: 0, why: "before the first rune"},
		{cursor: 35, want: 0, why: "the last position of row 0"},
		{cursor: 36, want: 1, why: "the first position of row 1"},
		{cursor: 75, want: 1, why: "the last position of row 1"},
		{cursor: 76, want: 2, why: "the first position of row 2"},
		{cursor: 100, want: 2, why: "at the end of the text"},
	} {
		rows, row, col := layoutText(promptForTest, layoutCursorText, tc.cursor, width)
		if row != tc.want {
			t.Errorf("width=%d cursor=%d (%s): cursorRow %d, want %d (%d rows drawn)",
				width, tc.cursor, tc.why, row, tc.want, len(rows))
		}
		if col >= width {
			t.Errorf("width=%d cursor=%d (%s): cursorCol %d outside the row", width, tc.cursor, tc.why, col)
		}
	}
}

// TestLayoutTextNoCursorReportsTheWrapArmedCell is the exhaustive sweep behind
// the table above: every cursor position of every buffer length at the widths a
// terminal reports, asserting no report ever names column == width.
func TestLayoutTextNoCursorReportsTheWrapArmedCell(t *testing.T) {
	for _, width := range layoutCursorWidths {
		for n := 0; n <= 2*width+3; n++ {
			text := strings.Repeat("a", n)
			for cursor := 0; cursor <= n; cursor++ {
				rows, row, col := layoutText(promptForTest, text, cursor, width)
				if col < 0 || col >= width {
					t.Fatalf("width=%d n=%d cursor=%d: cursorCol %d outside [0,%d) (row=%d of %d)",
						width, n, cursor, col, width, row, len(rows))
				}
			}
		}
	}
}

// TestLayoutTextNewlineResetsTheCursorColumn pins the multiline case: a newline
// resets the cell column, so the cursor on the row AFTER the break is measured
// from that row's start — and its cell is inside that row.
func TestLayoutTextNewlineResetsTheCursorColumn(t *testing.T) {
	const width = 80
	text := "first\nsecond"
	rows, row, col := layoutText(promptForTest, text, len([]rune(text)), width)
	if len(rows) != 2 {
		t.Fatalf("layout produced %d rows, want 2", len(rows))
	}
	if row != 1 {
		t.Fatalf("cursor row = %d, want 1 (the row after the break)", row)
	}
	if want := displayWidth("second"); col != want {
		t.Fatalf("cursor col = %d, want %d (the width of the row it is on)", col, want)
	}
}

// layoutCursorSet is the cursor positions the gate probes at a given text length
// and width: the ends, the two wrap edges (width-1 and width of buffer typed),
// and the neighbours of each.
func layoutCursorSet(textRunes, width int) []int {
	set := map[int]bool{0: true, 1: true, textRunes: true}
	for _, n := range []int{width - 1, width, width + 1, 2*width - 1, 2 * width, 2*width + 1} {
		if n >= 0 {
			set[n] = true
		}
	}
	out := make([]int, 0, len(set))
	for n := range set {
		if n <= textRunes {
			out = append(out, n)
		}
	}
	return out
}
