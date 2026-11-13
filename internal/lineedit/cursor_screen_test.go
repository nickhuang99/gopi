package lineedit

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// assertCursorCellIsTheRowsOwnCell is the Rule 10.2 half of the U3 gate: the
// reported cursor column is replayed through the vt100 cell grid and must land
// on the cell it names. A cell-forward past a full row CLAMPS to that row's last
// cell (screen.go, case 'C'), so a report of `col == width` — addressing a cell
// one past the row — comes back as width-1 and fails here.
func assertCursorCellIsTheRowsOwnCell(t *testing.T, width, cursor int, rows []string, row, col int) {
	t.Helper()
	sc := NewScreen(width, len(rows))
	// The same three moves Redraw makes: address the row, draw it, return to
	// column 0, then place the cell the layout reported.
	sc.Feed([]byte(escCSI + strconv.Itoa(row+1) + ";1H"))
	sc.Feed([]byte(rows[row]))
	sc.Feed([]byte(escCarriage))
	if col > 0 {
		// NOT `ESC[0C` — a zero parameter means CUF with its default of 1 cell,
		// so a cell-0 address must emit no sequence at all (this is what
		// Redraw's moveCursor does).
		sc.Feed([]byte(escCSI + strconv.Itoa(col) + "C"))
	}
	if sc.CurRow != row {
		t.Errorf("width=%d cursor=%d: a real draw puts the cursor on row %d, layoutText names row %d",
			width, cursor, sc.CurRow, row)
	}
	if sc.CurCol != col {
		t.Errorf("width=%d cursor=%d: reported (row=%d,col=%d) but the cell grid puts the cursor at col %d "+
			"(col == width arms the pending wrap: the next CR+LF would drop a row)",
			width, cursor, row, col, sc.CurCol)
	}
}

// typedRowsScreen drives a REAL editor through the scripted source at width and
// returns the screen as it stood at the last redraw of `typed` runes.
func typedRowsScreen(t *testing.T, typed string, width int) *Screen {
	t.Helper()
	const typingPause = 5 * time.Millisecond
	steps := []scriptStep{
		{data: []byte(typed), advance: typingPause},
		{timeout: true},
	}
	in, _, _, out := newScriptedEditor(t, steps, width, nil)
	if _, err := in.Run(); err != nil {
		t.Fatalf("width=%d Run: %v", width, err)
	}
	return editingScreen(t, out, width, 24)
}

// TestEditorScreenCursorAgreesWithLayoutText is acceptance 1 at the editor
// level: for every width a terminal reports and every cursor position, a real
// redraw must leave the screen's cursor cell exactly where layoutText says the
// cursor is (Screen.CurRow/CurCol vs. the layout's return).
func TestEditorScreenCursorAgreesWithLayoutText(t *testing.T) {
	for _, width := range []int{39, 40, 41, 79, 80, 81, 199, 200} {
		for _, typed := range []int{0, 1, width - 6, width - 5, width - 4, width, width + 1, 2*width - 5, 100} {
			if typed < 0 {
				continue
			}
			text := strings.Repeat("a", typed)
			sc := typedRowsScreen(t, text, width)
			rows, row, col := layoutText(promptForTest, text, typed, width)
			if len(rows) > editorRowsCap {
				// The display cap clips the drawn rows; the layout is asserted
				// against the cell grid in TestLayoutTextCursorStaysInsideItsRow.
				continue
			}
			if sc.CurRow != row || sc.CurCol != col {
				t.Errorf("width=%d typed=%d: screen cursor=(%d,%d), layoutText says (row=%d,col=%d) over %d rows",
					width, typed, sc.CurRow, sc.CurCol, row, col, len(rows))
			}
			if sc.CurCol >= width {
				t.Errorf("width=%d typed=%d: the screen's cursor column %d is outside the row", width, typed, sc.CurCol)
			}
		}
	}
}

// TestEditorScreenCursorOnAFullRowIsTheLastCell is the defect as the operator
// sees it: a row that fills the terminal width must leave the cursor ON that row
// and inside it — not armed at the wrap, which pushes the NEXT repaint one blank
// row down per wrapped row.
func TestEditorScreenCursorOnAFullRowIsTheLastCell(t *testing.T) {
	const width = 40
	// promptForTest is 5 cells, so 35 runes fill row 0 exactly.
	const typed = width - 5
	sc := typedRowsScreen(t, strings.Repeat("a", typed), width)
	if got := sc.Line(0); got != promptForTest+strings.Repeat("a", typed) {
		t.Fatalf("row 0 = %q, want the full width filled", got)
	}
	if sc.CurRow != 0 {
		t.Fatalf("cursor row = %d, want 0 (row 0 is full; the wrap must not be armed)", sc.CurRow)
	}
	if sc.CurCol != width-1 {
		t.Fatalf("cursor col = %d, want %d (the last cell of a full row)", sc.CurCol, width-1)
	}
}

// TestEditorScreenRepaintsStayOnTheirRows is the symptom the pending-wrap column
// produced: with the wrap armed, each repaint's CR+LF advanced the cursor a row
// and the editing line walked down the screen, leaving blank rows behind it.
func TestEditorScreenRepaintsStayOnTheirRows(t *testing.T) {
	const width = 40
	const typed = width - 5 // exactly one full row
	steps := []scriptStep{
		{data: []byte(strings.Repeat("a", typed)), advance: 5 * time.Millisecond},
		{timeout: true, advance: time.Second},
		// one more keystroke: the repaint that would walk if the wrap were armed
		{data: []byte("b"), advance: 5 * time.Millisecond},
		{timeout: true, advance: time.Second},
		{data: []byte("c"), advance: 5 * time.Millisecond},
		{timeout: true},
	}
	in, _, _, out := newScriptedEditor(t, steps, width, nil)
	if _, err := in.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sc := editingScreen(t, out, width, 24)
	// layoutText says what this redraw must produce: row 0 full, the two extra
	// runes wrapped onto row 1, the cursor at the end of row 1.
	rows, wantRow, wantCol := layoutText(promptForTest, strings.Repeat("a", typed)+"bc", typed+2, width)
	if got := sc.Line(0); got != rows[0] {
		t.Fatalf("row 0 = %q, want %q", got, rows[0])
	}
	if len(rows) > 1 {
		if got := sc.Line(1); got != rows[1] {
			t.Fatalf("row 1 = %q, want %q (the wrapped remainder)", got, rows[1])
		}
	}
	if sc.CurRow != wantRow || sc.CurCol != wantCol {
		t.Fatalf("cursor at (%d,%d), want (%d,%d) — a walking repaint would put it a row lower",
			sc.CurRow, sc.CurCol, wantRow, wantCol)
	}
	for row := len(rows); row < len(rows)+3 && row < 24; row++ {
		if got := sc.Line(row); got != "" {
			t.Fatalf("row %d = %q, want it blank — a repaint walked the editing line down", row, got)
		}
	}
}

// TestEditorScreenCursorNeverSitsPastTheRow checks the whole visible screen of a
// real redraw for the defect's signature: the cursor must never be addressed to
// a column outside the row it is on (a pending-wrap column shows up as
// CurCol == width).
func TestEditorScreenCursorNeverSitsPastTheRow(t *testing.T) {
	for _, width := range []int{2, 39, 40, 41, 79, 80, 199, 200} {
		for _, typed := range []int{0, 1, width - 6, width - 5, width - 4, width} {
			if typed < 0 {
				continue
			}
			sc := typedRowsScreen(t, strings.Repeat("a", typed), width)
			if sc.CurCol >= width {
				t.Errorf("width=%d typed=%d: screen cursor column %d is not a cell of the row", width, typed, sc.CurCol)
			}
		}
	}
}
