package lineedit

import (
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/sgr"
)

// This file is the Screen's ATTRIBUTE side: the per-cell style the grid records
// while it parses SGR, and the read-only API a test (or a future renderer's
// verifier) asserts it with.
//
// The rule it exists for is the one that made this grid worth building in the
// first place (engineering rule 10.2, rendering truth): "the callback fired" is
// not evidence that the right COLUMN is the right colour. Cell(row, col).Fg is.
//
// Nothing here emits anything: the plain Line/Contents/Scrollback accessors are
// unchanged, and these variants are additions. Drawing with colour is V2.

// wideCells is a wide (East Asian wide/fullwidth) rune's display width, and
// wideContinuation is what its second column holds. There is no second rune, so
// the marker is a space — which is what a one-cell-per-rune grid would have left
// in that column anyway, so Line()'s text is byte-identical before and after the
// column model, while Cell() can now answer for the column itself.
const (
	wideCells        = 2
	wideContinuation = ' '
)

// CellKind is what a cell HOLDS. A wide rune's two columns are not
// interchangeable: the first carries the rune, the second only completes it, so
// a caller that wants the runes of a row (rather than its columns) can tell them
// apart. The style, by contrast, covers both columns.
type CellKind int

const (
	// CellEmpty is a cell no rune was written to — a blank.
	CellEmpty CellKind = iota
	// CellRune is a cell holding a printed rune.
	CellRune
	// CellWideContinuation is a wide rune's second column.
	CellWideContinuation
)

// kindNames names each kind for a test failure. The words are the type's own
// vocabulary, not a wire value, so they live here rather than in sgr.
var kindNames = map[CellKind]string{
	CellEmpty:            "empty",
	CellRune:             "rune",
	CellWideContinuation: "wide-continuation",
}

// String renders the kind for a failure message.
func (k CellKind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return unknownKindName
}

// unknownKindName is how a kind outside the enumeration renders.
const unknownKindName = "unknown-kind"

// blankAttrRow is one row of default styles — a fresh row never holds a stale
// colour, exactly as a terminal's erased cell is uncoloured.
func blankAttrRow(cols int) []sgr.Style {
	row := make([]sgr.Style, cols)
	for i := range row {
		row[i] = sgr.Default()
	}
	return row
}

// Cell is one grid column as a value: the rune it holds, what it HOLDS (a wide
// rune's second column is not a rune of its own) and the style it is painted
// with. A value rather than three return values so an expectation can be written
// down once and compared with Equal.
type Cell struct {
	Rune  rune
	Kind  CellKind
	Style sgr.Style
}

// Equal reports whether two cells are identical in rune, kind and style.
func (c Cell) Equal(o Cell) bool {
	return c.Rune == o.Rune && c.Kind == o.Kind && c.Style.Equal(o.Style)
}

// isContinuation reports whether (row, col) is a wide rune's SECOND column. The
// column is a column of a rune, not a character of its own, which is why Line
// skips it and Cell labels it.
func (s *Screen) isContinuation(row, col int) bool {
	if row < 0 || row >= s.Rows || col <= 0 || col >= s.Cols {
		return false
	}
	return s.cells[row][col] == wideContinuation && isWideRune(s.cells[row][col-1])
}

// Cell returns one grid column as a value. A column outside the grid reads as
// (space, CellEmpty, default style), so a caller asking about a column that does
// not exist gets an answer it can compare rather than a panic.
func (s *Screen) Cell(row, col int) Cell {
	if row < 0 || row >= s.Rows || col < 0 || col >= s.Cols {
		return Cell{Rune: ' ', Kind: CellEmpty, Style: sgr.Default()}
	}
	kind := CellRune
	switch {
	case s.isContinuation(row, col):
		kind = CellWideContinuation
	case s.cells[row][col] == ' ':
		kind = CellEmpty
	}
	return Cell{Rune: s.cells[row][col], Kind: kind, Style: s.attrs[row][col]}
}

// Attr returns just the style of one cell — the short form of the question a
// colour assertion asks, so a test reads `sc.Attr(0, 0).Fg` rather than
// unpacking a cell it does not otherwise need.
func (s *Screen) Attr(row, col int) sgr.Style { return s.Cell(row, col).Style }

// CellOf builds the Cell for one rune painted with one style — the INPUT side of
// an attribute assertion: "this is what the editor drew" as a value the grid's
// own answer can be compared against.
func CellOf(r rune, style sgr.Style) Cell {
	kind := CellRune
	if r == ' ' {
		kind = CellEmpty
	}
	return Cell{Rune: r, Kind: kind, Style: style}
}

// CellsOf builds the per-column cells of a string painted with one style,
// counting a wide rune as the two columns it occupies. It is the expected side
// of a whole-line assertion: the editor drew this text, in this colour, so these
// are the cells the grid must hold.
func CellsOf(text string, style sgr.Style) []Cell {
	out := make([]Cell, 0, len([]rune(text)))
	for _, r := range []rune(text) {
		out = append(out, CellOf(r, style))
		if isWideRune(r) {
			out = append(out, CellOf(wideContinuation, style))
		}
	}
	return out
}

// StyledLine returns row (0-based) as its text and the style of each RUNE of
// that text, with trailing blanks trimmed exactly the way Line trims them — so
// the two always describe the same row, and a row nobody painted returns the
// empty string with no styles.
//
// The styles index the TEXT, not the screen's columns: a wide rune's two columns
// carry one shared style, so it contributes one entry, and `styles[i]` describes
// the rune at text position i. Asking about a specific COLUMN is Cell(row, col)'s
// question, and that is where the two columns of a wide rune are visible.
func (s *Screen) StyledLine(row int) (string, []sgr.Style) {
	text := s.Line(row)
	if text == "" {
		return "", nil
	}
	runes := []rune(text)
	styles := make([]sgr.Style, 0, len(runes))
	for col := 0; col < s.Cols && len(styles) < len(runes); col++ {
		if s.isContinuation(row, col) {
			continue
		}
		styles = append(styles, s.attrs[row][col])
	}
	return text, styles
}

// StyledContents returns every row's styled text, newline-joined — the styled
// counterpart of Contents, for an assertion over the whole drawn screen.
func (s *Screen) StyledContents() string {
	rows := make([]string, s.Rows)
	for i := range rows {
		text, styles := s.StyledLine(i)
		rows[i] = renderStyledRow(text, styles)
	}
	return strings.Join(rows, "\n")
}

// styleRunSeparator separates a row's text from its style runs.
const styleRunSeparator = " ~ "

// renderStyledRow renders one styled row as "text ~ run | run | …", where each
// run is "start-end style" (inclusive columns). The run form keeps a 100-column
// screen readable while still naming every column, and it is what makes a
// coloured row distinguishable from an all-default one at a glance.
func renderStyledRow(text string, styles []sgr.Style) string {
	if text == "" || len(styles) == 0 {
		return text
	}
	return text + styleRunSeparator + strings.Join(styleRuns(styles), " | ")
}

// styleRuns groups a row's columns into maximal spans of one style, reporting
// each as "start-end style".
func styleRuns(styles []sgr.Style) []string {
	var runs []string
	start := 0
	for col := 1; col <= len(styles); col++ {
		if col < len(styles) && styles[col].Equal(styles[start]) {
			continue
		}
		runs = append(runs, strconv.Itoa(start)+"-"+strconv.Itoa(col-1)+" "+styles[start].String())
		start = col
	}
	return runs
}
