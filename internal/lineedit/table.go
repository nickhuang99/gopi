package lineedit

import "strings"

// table.go — markdown TABLES in the answer body: how a table is recognised, and
// which of two FORMS it is painted in at a given width.
//
// WHY THIS FILE EXISTS. internal/lineedit had no table code at all: a table
// arrived as ordinary prose and was wrapped at the band's cell cut, which chops
// a row mid-token and lands its tail as an orphan run at the block's column
// (the operator's reading: `吞字少字`, unreadable). A table is the one markdown
// construct whose meaning IS the column alignment the cut destroys — a row cut
// at 60 columns is no longer a table.
//
// THE POLICY, stated once here because a policy in two places is a policy in
// neither:
//
//  1. A table is recognised as a BLOCK: a pipe row, and under it a DELIMITER row
//     (`|---|:--:|`) with the same number of cells, and then the record rows
//     (same cell count). The delimiter row is SYNTAX, never content: it is the
//     marker that says what the row above it IS — the header, whose cells are
//     the field names of every record below — and painting `|---|---|` would be
//     painting the format at the reader.
//  2. GRID when the grid FITS. The natural grid (every column padded to its
//     widest cell, one band row per table row) is painted when its width is at
//     most the band's CONTENT width — what is left of the terminal after the
//     turn's gutter. This is the form the model wrote and the form a reader
//     scans.
//  3. OTHERWISE TRANSPOSED FIELD:VALUE BLOCKS. One block per record, one band
//     row per field (`  Commits: \`4b9133c0\`, \`d1e6422a\``), each labelled by
//     the header's own cell for that column. A grid that cannot fit is not a
//     grid, and the honest degradation is the form that survives any width down
//     to the gutter's own minimum, because a field NAME is short and it is the
//     label — not the width — that makes a value readable.
//  4. The band's own wrap still applies to the transposed form: a wrapped VALUE
//     continues at the block's column, and the turn marker is drawn once per
//     band row, never twice (see writeGutter / lineGutter).
//
// The transposition is why the operator's own table (whose grid needs ~123 cells
// against a 117-column band's 115) becomes readable instead of chopped: no cell
// of it is ever cut, so no cell of it is ever lost.
//
// This layer produces the lines the band PAINTS. It is text, not layout: the
// renderer still cuts, styles and gutters every line it is handed (writeRow),
// so a table can never be a second owner of the cell arithmetic (Rule 1b).

const (
	// tablePipe is the markdown table's cell delimiter.
	tablePipe = '|'
	// tableMinCells is the fewest cells a piped row can have and still be a
	// table row: `| a |` and a bare `|` are prose punctuation, not a row.
	tableMinCells = 2
	// tableFieldSep separates a transposed field's label from its value.
	tableFieldSep = ": "
	// tableFieldIndent displaces the transposed labels from the block's column,
	// so a reader sees a labelled block rather than a paragraph.
	tableFieldIndent = "  "
	// tableGridPrefix / tableGridJoin / tableGridSuffix are the grid's own
	// geometry: `| `, ` | `, ` |`. Their widths are constants because the
	// fit decision is arithmetic on them.
	tableGridPrefix = "| "
	tableGridJoin   = " | "
	tableGridSuffix = " |"

	tableGridPrefixWidth = 2
	tableGridJoinWidth   = 3
	tableGridSuffixWidth = 2
)

// answerTable is a recognised markdown table: the header's cells (the field
// names) and the records' cells, all rows carrying the same cell count.
type answerTable struct {
	header  []string
	records [][]string
}

// tableCells splits a piped row into its cells, or reports that the row is not a
// piped row at all. The leading and trailing pipe are the row's own delimiters,
// so they are dropped rather than read as empty cells.
func tableCells(line string) ([]string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, string(tablePipe)) {
		return nil, false
	}
	parts := strings.Split(t, string(tablePipe))
	// The text before the first pipe and after the last are the row's own
	// edges: a table row opens and closes with a pipe, so both are empty for a
	// row the model wrote as a table.
	if parts[0] != "" && strings.TrimSpace(parts[0]) != "" {
		return nil, false
	}
	if n := len(parts); n > 0 && strings.TrimSpace(parts[n-1]) == "" {
		parts = parts[:n-1]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	if len(parts) < tableMinCells {
		return nil, false
	}
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells, true
}

// tableDelimiterRow reports whether a row's cells are the markdown alignment
// rule (`---`, `:---:`, `---:`): the syntax line that makes the row above it a
// header. It is the ONLY thing that distinguishes a table from a sentence that
// happens to contain pipes, which is why the recognition requires it.
func tableDelimiterRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if c == "" || strings.Trim(c, "-:") != "" || !strings.Contains(c, "-") {
			return false
		}
	}
	return true
}

// tableBlockAt reads the table starting at lines[i], or reports that there is
// none. A block is a header row, a delimiter row with the same cell count, and
// the run of rows that follow with that same cell count — the row-after-row
// uniformity is what a table IS, so a row that breaks it ends the table rather
// than being absorbed into it.
func tableBlockAt(lines []string, i, contentCols int) (answerTable, int, bool) {
	if i+1 >= len(lines) {
		return answerTable{}, 0, false
	}
	header, ok := tableCells(lines[i])
	if !ok {
		return answerTable{}, 0, false
	}
	delim, ok := tableCells(lines[i+1])
	if !ok || len(delim) != len(header) || !tableDelimiterRow(delim) {
		return answerTable{}, 0, false
	}
	t := answerTable{header: header}
	for j := i + 2; j < len(lines); j++ {
		cells, ok := tableCells(lines[j])
		if !ok || len(cells) != len(header) {
			break
		}
		t.records = append(t.records, cells)
	}
	return t, 2 + len(t.records), true
}

// form renders the table for a band whose CONTENT width is contentCols: the grid
// when it fits, the transposed field/value blocks otherwise. It is the whole of
// the policy, and the only place that decides it.
func (t answerTable) form(contentCols int) []string {
	if t.gridWidth() <= contentCols {
		return t.grid()
	}
	return t.transposed()
}

// colWidths is the grid's column widths: each column is as wide as its widest
// cell, measured as the reader will SEE it (DisplayText drops the emphasis
// markers, which occupy no cell — measuring the raw text would pad for
// characters that are never painted, and the grid would come out ragged).
func (t answerTable) colWidths() []int {
	widths := make([]int, len(t.header))
	measure := func(cells []string) {
		for i, c := range cells {
			if w := displayWidth(DisplayText(c)); w > widths[i] {
				widths[i] = w
			}
		}
	}
	measure(t.header)
	for _, rec := range t.records {
		measure(rec)
	}
	return widths
}

// gridWidth is the width the natural grid would occupy, including its pipes and
// the single space each side of every cell.
func (t answerTable) gridWidth() int {
	widths := t.colWidths()
	sum := 0
	for _, w := range widths {
		sum += w
	}
	return tableGridPrefixWidth + sum + tableGridJoinWidth*(len(widths)-1) + tableGridSuffixWidth
}

// grid renders the table as an aligned grid: one line per table row, every
// column padded to its width so the pipes line up down the block. The delimiter
// row is not among them — it is the syntax that made this a table.
func (t answerTable) grid() []string {
	widths := t.colWidths()
	out := make([]string, 0, len(t.records)+1)
	rows := make([][]string, 0, len(t.records)+1)
	rows = append(rows, t.header)
	rows = append(rows, t.records...)
	for _, r := range rows {
		out = append(out, tableGridLine(r, widths))
	}
	return out
}

// tableGridLine lays one row of the grid: cells padded to the column widths,
// framed by the table's own pipes.
func tableGridLine(cells []string, widths []int) string {
	var sb strings.Builder
	sb.WriteString(tableGridPrefix)
	for i, c := range cells {
		if i > 0 {
			sb.WriteString(tableGridJoin)
		}
		sb.WriteString(c)
		if pad := widths[i] - displayWidth(DisplayText(c)); pad > 0 {
			sb.WriteString(strings.Repeat(" ", pad))
		}
	}
	sb.WriteString(tableGridSuffix)
	return sb.String()
}

// transposed renders the table as one field/value block per record, each field
// labelled by the header's own cell for its column. Nothing is padded and
// nothing is aligned: the label is what makes a value readable, so this form
// survives any width the band can paint a field name in.
func (t answerTable) transposed() []string {
	out := make([]string, 0, len(t.records)*len(t.header))
	for _, rec := range t.records {
		for i, cell := range rec {
			out = append(out, tableFieldIndent+t.header[i]+tableFieldSep+cell)
		}
	}
	return out
}

// looksPipedRow reports whether a line is shaped like a markdown table row —
// its first non-blank character is the pipe — WITHOUT requiring it to parse as
// one. It is deliberately weaker than tableCells, and it is the test every
// withholding decision uses, because the decision has to be made before the row
// is complete: a burst boundary can fall after the row's first two characters,
// and `| ` parses as no table row at all (tableCells needs at least
// tableMinCells cells). Recognising only COMPLETE rows let that fragment be
// painted live, and the rest of the row was then committed as its head — the
// measured headless row (`3b46107`, `d836a0`).
//
// The cost of the weaker test is one line of delay for a prose line that merely
// STARTS with a pipe (a shell pipeline quoted in an answer, say): it is not
// painted until its successor arrives, or by Finish at the end of the answer. It
// is painted as the prose it is — no cell of it is lost — so the trade the
// weaker test buys is "a table row is never chopped" for "a pipe-leading prose
// line waits one line".
func looksPipedRow(s string) bool {
	t := strings.TrimLeft(s, " \t")
	return strings.HasPrefix(t, string(tablePipe))
}

// tableCandidateEnd reports whether the LAST line of lines opens a table whose
// delimiter row has not arrived yet — the one state in which the band must not
// paint a line it is holding. The header row alone is indistinguishable from
// prose containing pipes, so committing it would paint a pipe row that a later
// burst turns out to have been a table header (a repaint the band never does),
// or paint `|Face|Commits|` as prose and then be unable to retract it.
//
// It answers only about the LAST line: every earlier line's fate is already
// settled by its successor.
func tableCandidateEnd(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	_, ok := tableCells(lines[len(lines)-1])
	return ok
}
