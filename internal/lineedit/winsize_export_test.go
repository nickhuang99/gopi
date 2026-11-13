package lineedit

import (
	"os"
	"strings"
	"testing"
)

// winsize_export_test.go — the two exports the fallback chat renderer wraps
// through (Rule 14: one wrap policy, one width source). They are asserted here
// because a caller outside this package now depends on their contract, and the
// contract's edges are the parts a caller cannot see: what "0" means, and what a
// wide rune does at an odd width.

// TestWrapToCols_BoundsEveryRow pins the row bound and the no-loss invariant: the
// rows are the input's own bytes, each within the width.
func TestWrapToCols_BoundsEveryRow(t *testing.T) {
	const width = 10
	in := strings.Repeat("ab", 17) // 34 cells: three full rows + a tail
	rows := WrapToCols(in, width)
	if len(rows) != 4 {
		t.Fatalf("WrapToCols(%d cells, %d) = %d rows, want 4", len(in), width, len(rows))
	}
	var joined strings.Builder
	for i, row := range rows {
		if len(row) > width {
			t.Fatalf("row %d = %q is %d cells wide, want at most %d", i, row, len(row), width)
		}
		if i < len(rows)-1 && len(row) != width {
			t.Fatalf("row %d = %q is %d cells, want a full %d-cell row (text must not be cut short)", i, row, len(row), width)
		}
		joined.WriteString(row)
	}
	if joined.String() != in {
		t.Fatalf("the rows carry %q, want the input %q", joined.String(), in)
	}
}

// TestWrapToCols_ZeroMeansNoWrapping pins the sentinel: a non-positive width is
// "leave the text alone", never a slice of one cell per row.
func TestWrapToCols_ZeroMeansNoWrapping(t *testing.T) {
	in := "a longer line that would wrap at any real width"
	for _, width := range []int{0, -1} {
		rows := WrapToCols(in, width)
		if len(rows) != 1 || rows[0] != in {
			t.Fatalf("WrapToCols(in, %d) = %q, want the input unchanged as one row", width, rows)
		}
	}
}

// TestWrapToCols_NeverSplitsAWideRune pins the policy the two terminal paths
// share: at an odd width a wide rune starts the next row instead of being cut
// across the edge (a cut wide rune would paint one cell of a two-cell glyph).
func TestWrapToCols_NeverSplitsAWideRune(t *testing.T) {
	const width = 7 // holds three wide runes (6 cells), not four (8)
	rows := WrapToCols(strings.Repeat("知", 7), width)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (3+3+1 wide runes at %d cells)", len(rows), width)
	}
	if got := len([]rune(rows[0])); got != 3 {
		t.Fatalf("first row holds %d runes, want 3 — a split wide rune would hold 3.5", got)
	}
	var joined strings.Builder
	for _, row := range rows {
		joined.WriteString(row)
	}
	if got := joined.String(); got != strings.Repeat("知", 7) {
		t.Fatalf("the rows carry %q, want the input unchanged", got)
	}
}

// TestTerminalCols_UnknownIsZero pins the width source's sentinel: a descriptor
// that is not a terminal answers 0 ("unknown"), which is what tells a caller to
// leave the stream alone rather than wrap to a sliver.
func TestTerminalCols_UnknownIsZero(t *testing.T) {
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer rd.Close() //stylegate:ignore the harness's own fixture teardown
	defer wr.Close() //stylegate:ignore the harness's own fixture teardown
	if got := TerminalCols(int(rd.Fd())); got != 0 {
		t.Fatalf("TerminalCols(pipe read end) = %d, want 0", got)
	}
	file, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	defer file.Close() //stylegate:ignore the fixture is removed with the temp dir
	if got := TerminalCols(int(file.Fd())); got != 0 {
		t.Fatalf("TerminalCols(regular file) = %d, want 0", got)
	}
}
