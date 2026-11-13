package lineedit

// table_grid_test.go — the markdown TABLE handling of the answer band, and the
// instrument that reads it back.
//
// THE DEFECT THIS PINS (operator's 2nd report, 2026-09-26, session a5e1c3e9).
// A markdown table is not a format the answer body knows: internal/lineedit has
// no table code at all (no delimiter row, no column model), so the rows arrive
// as ordinary prose and are wrapped at the band's cell cut. A row wider than the
// band is chopped mid-token and its tail lands as an orphan run at column 0 of
// the next row — the operator's own reading: `吞字少字`, unreadable. A table is
// the one markdown construct whose meaning is CARRIED by the column alignment
// the cut destroys: `| Non-interactive | 4b9133c0 | chat --once |` cut at 60
// columns is no longer a table.
//
// THE POLICY (stated once, here, because a policy in two places is a policy in
// neither):
//
//  1. A markdown table is recognised as a BLOCK: a delimiter row (`|---|:--:|`)
//     under a header row. The delimiter row is SYNTAX, not content: it is never
//     painted. It is the marker that says "the row above is this table's header,
//     and the rows below are its records", which is exactly what the transposed
//     form needs — and painting `|---|---|` as a row of dashes would be painting
//     the format at the reader.
//  2. GRID when it FITS: if the table's natural grid (each column padded to its
//     widest cell) is at most the band's content width, it is painted as a
//     grid, one row per line, padded so the columns line up. This is the form
//     the model wrote and the form a reader scans.
//  3. OTHERWISE TRANSPOSED FIELD:VALUE BLOCKS: one block per record, one line
//     per FIELD (`  Commits: \`4b9133c0\`, \`d1e6422a\``), each field's value
//     wrapped by the ordinary band wrap. A grid that cannot fit is not a grid:
//     the honest degradation is the field/value form, which survives any width
//     down to the gutter's own minimum, because a field name is short.
//  4. The BAND's own wrap still applies: a wrapped field value starts at the
//     band's column, and the turn marker is drawn once per row, never twice.
//
// The transposition is why the operator's table becomes readable at 60/40/24
// columns: no cell of it is ever cut, so no cell of it is ever lost.

import (
	"strings"
	"testing"
)

// tableGridWidths are the rungs the policy is read at: the operator's own 117,
// and the widths at which a 60-column table must stop being a grid and become
// field/value blocks. 24 is the floor — narrow enough that even a field/value
// line wraps, which is the width at which "no cell is cut" has to be earned.
var tableGridWidths = []int{117, 80, 60, 40, 24}

// operatorTable is the operator's own table, VERBATIM from the answer that
// damaged on his screen (session a5e1c3e9, the "what did today's chat commits
// do" turn): the header, the delimiter row, three records, and the prose list
// item that sat beside the table and stayed whole.
var operatorTable = []string{
	"| Face | Commits | What |",
	"|---|---|---|",
	"| Non-interactive faces | `4b9133c0`, `d1e6422a`, `fbd836a0` | `chat --once`/`-p`, `--once --turns` pipe-back, REPL lines dispatched |",
	"| Status row | `03b46107`, `1fba0698`, `f6925b4c`, `3d025abe`, `51013978` | metrics grouped, 100-col budget, dead code cut |",
	"| Re-partition | `fbd836a0`, `ff678e6b` | the status row re-partitioned & operational |",
	"- `51003978` feat(status): retired the money sub-cell, ctx read-out now absolute",
}

// tableGridRender paints a markdown answer through a REAL editor over a real
// region at an exact geometry and returns the PAINTED GRID — the instrument the
// policy is read from, reused from the in-package harness (newA5HarnessSized)
// that every turn-layout case already drives. No new pty driver, no new screen
// model: the same editor + region + Screen replay production wires.
func tableGridRender(t *testing.T, answer string, cols, rows int, chunk int) *a5Harness {
	t.Helper()
	h := newA5HarnessSized(t, nil, nil, rows, cols)
	band := h.answer
	band.SetCols(cols)
	band.Begin()
	band.BeginTurn(TurnInfo{Index: 1, Prompt: "what did today's chat commits do"})
	for i := 0; i < len(answer); i += chunk {
		j := i + chunk
		if j > len(answer) {
			j = len(answer)
		}
		band.Delta(answer[i:j])
	}
	band.Finish()
	h.replay()
	return h
}

// tableGridAnswer is the operator's table as one answer body.
func tableGridAnswer() string { return strings.Join(operatorTable, "\n") + "\n" }

// tableOnlyAnswer is the table alone: the form cases measure what the TABLE is
// painted as, so the prose line that followed it in the operator's answer (which
// is 80 cells wide and wraps at the narrow rungs) is not in the way.
func tableOnlyAnswer() string { return strings.Join(operatorTable[:5], "\n") + "\n" }

// contentRow returns a painted grid row with the turn's gutter stripped, so a
// case can look at the reader's content rather than at the chrome.
func contentRow(line string) string {
	line = strings.TrimPrefix(line, TurnMarker)
	return strings.TrimRight(line, " ")
}

// tableRowCells splits a grid row into its cells. A row that is not a table row
// (no leading `|`) reports ok=false: the case must not read prose as a table.
func tableRowCells(line string) ([]string, bool) {
	c := contentRow(line)
	if !strings.HasPrefix(strings.TrimSpace(c), "|") {
		return nil, false
	}
	parts := strings.Split(strings.TrimSpace(c), "|")
	// The leading and trailing empty cells are the row's own delimiters.
	if len(parts) >= 2 {
		parts = parts[1 : len(parts)-1]
	}
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells, true
}

// TestTableDelimiterRowIsSyntaxNotContent: the `|---|---|` row is consumed. It is
// the marker that makes the rows around it a table, and a reader who is shown
// `|---|---|` is being shown the format instead of the content.
func TestTableDelimiterRowIsSyntaxNotContent(t *testing.T) {
	h := tableGridRender(t, tableOnlyAnswer(), 117, 40, 1<<20)
	screen := h.screen.Contents()
	if strings.Contains(screen, "---") {
		t.Fatalf("the delimiter row was painted: a markdown table's `|---|---|` is syntax, not content\n%s", screen)
	}
}

// TestTableGridWhenTheGridFits: at a width that can host the natural grid, the
// policy paints the grid — aligned pipes, one band row per table row, the header
// on the first of them.
func TestTableGridWhenTheGridFits(t *testing.T) {
	const cols = 200 // content 198, comfortably past the table's ~123-cell grid
	tbl, _, ok := tableBlockAt(operatorTable[:5], 0, cols)
	if !ok {
		t.Fatal("the operator's own rows were not recognised as a table")
	}
	if tbl.gridWidth() > cols-displayWidth(TurnMarker) {
		t.Fatalf("the table's grid is %d cells wide; this case needs it to FIT %d so the grid form is the one under test",
			tbl.gridWidth(), cols-displayWidth(TurnMarker))
	}
	h := tableGridRender(t, tableOnlyAnswer(), cols, 40, 1<<20)
	screen := h.screen.Contents()
	for _, want := range tbl.grid() {
		if !strings.Contains(screen, want) {
			t.Fatalf("cols=%d: the grid row %q is not on the screen — a table whose grid fits must be painted as a grid\n%s", cols, want, screen)
		}
	}
}

// TestTablePolicyFollowsTheFitRule states the policy as the rule it is, at every
// rung: the grid when the table's natural grid fits the band's content width,
// the transposed field/value blocks otherwise. The expectation is derived from
// the table's OWN measurement, so the case cannot drift from the policy — it
// fails whichever side of the rule the implementation lands on wrongly.
func TestTablePolicyFollowsTheFitRule(t *testing.T) {
	for _, cols := range tableGridWidths {
		tbl, _, ok := tableBlockAt(operatorTable[:5], 0, cols)
		if !ok {
			t.Fatal("the operator's own rows were not recognised as a table")
		}
		content := cols - displayWidth(TurnMarker)
		wantGrid := tbl.gridWidth() <= content
		h := tableGridRender(t, tableOnlyAnswer(), cols, 60, 1<<20)
		screen := h.screen.Contents()
		if wantGrid {
			if !strings.Contains(screen, tbl.grid()[0]) {
				t.Fatalf("cols=%d: the grid fits (%d ≤ %d) but the header was not painted as a grid row\n%s",
					cols, tbl.gridWidth(), content, screen)
			}
			continue
		}
		if strings.Contains(screen, tableGridPrefix+tbl.header[0]+tableGridJoin) {
			t.Fatalf("cols=%d: the table's grid is %d cells against a %d-cell content width — a grid that does not fit must NOT be painted as one\n%s",
				cols, tbl.gridWidth(), content, screen)
		}
		// …and the transposed form is what IS painted: every column labelled by
		// the header's own cell for it, so the reader keeps the field names.
		for _, field := range tbl.header {
			if !strings.Contains(screen, field+tableFieldSep) {
				t.Fatalf("cols=%d: the field label %q is not on the screen — a grid that cannot fit must be transposed into field:value blocks\n%s",
					cols, field, screen)
			}
		}
	}
}

// collapseSpace squeezes whitespace runs to one space, so a comparison is about
// the characters and not about how the band padded a column.
func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// TestTableTransposesWhenTheGridCannotFit: at every rung where the grid cannot
// fit — including the operator's own 117, where his 134-cell row has no grid —
// the policy is the transposed field/value form: the HEADER's field names label
// each record's values, and no cell is ever cut.
func TestTableTransposesWhenTheGridCannotFit(t *testing.T) {
	for _, cols := range tableGridWidths {
		h := tableGridRender(t, tableOnlyAnswer(), cols, 60, 1<<20)
		screen := h.screen.Contents()
		flat := collapseSpace(screen)
		for _, field := range []string{"Face", "Commits", "What"} {
			if !strings.Contains(flat, field) {
				t.Fatalf("cols=%d: the field name %q is not on the screen — a table that cannot fit must be transposed into field:value blocks, not chopped\n%s", cols, field, screen)
			}
		}
		// And no cell of it is cut: the values are what the reader came for.
		// Whitespace is dropped, not collapsed — the band's cell-precise wrap
		// legitimately breaks a word at the column, and a wrap is not a loss.
		bare := noSpace(screen)
		for _, row := range operatorTable[2:5] {
			cells, _ := tableRowCells(row)
			for _, cell := range cells {
				if cell == "" {
					continue
				}
				if !strings.Contains(bare, noSpace(cell)) {
					t.Fatalf("cols=%d: the cell %q was cut — the transposed form must carry every cell whole (this is the 吞字少字 reading)\n%s", cols, cell, screen)
				}
			}
		}
	}
}

// TestTableRowKeepsItsHeadAndOneMarker is the correctness half: the carry
// defects measured on the operator's screen (a row whose HEAD was eaten while a
// neighbour was whole, and the turn marker drawn twice on one row). It is
// checked at every rung and at every delta chunk size, because the carry is a
// function of BOTH — a whole-delta run cannot see it (the carry needs a wrap AND
// a split).
func TestTableRowKeepsItsHeadAndOneMarker(t *testing.T) {
	for _, cols := range tableGridWidths {
		for _, chunk := range []int{1, 3, 7, 64, 1 << 20} {
			h := tableGridRender(t, tableGridAnswer(), cols, 60, chunk)
			screen := h.screen.Contents()
			// (1) each of the table's RECORD rows walks through the screen in
			// order — a fragment walk, not a substring test, because a substring
			// test passes on a RE-ORDERED row.
			for _, row := range operatorTable[2:5] {
				assertTableRowOnScreen(t, screen, row, cols, chunk)
			}
			// (2) no row carries the marker twice: two is paint the model never
			// wrote. The count is deliberately not asserted globally — the band
			// SCROLLS, so older rows are legitimately off the visible grid.
			for _, line := range strings.Split(screen, "\n") {
				if n := strings.Count(line, "\u258c"); n > 1 {
					t.Fatalf("cols=%d chunk=%d: a row carries the turn marker %d times — the marker was carried into the row's body and drawn again\n  row:    %q\n  screen:\n%s",
						cols, chunk, n, line, screen)
				}
			}
		}
	}
}

// assertTableRowOnScreen walks the row's CELLS through the screen IN ORDER: each
// cell must be found at or after the previous one. It is a fragment walk, not a
// substring test, because a substring test passes on a RE-ORDERED row.
//
// The cells — not the raw `| a | b |` row — are what to walk: the policy
// restructures a row (a grid pads it, a transposed block lists the same cells
// under the header's labels), and the row's own pipes are they only characters
// that are legitimately not carried through. Whitespace is dropped entirely
// because the band's cell-precise wrap breaks a word at the column and
// re-indents the continuation: a wrap is not a loss, and a case that counted one
// as a loss would fail on a faithful render.
func assertTableRowOnScreen(t *testing.T, screen, row string, cols, chunk int) {
	t.Helper()
	cells, ok := tableRowCells(row)
	if !ok {
		t.Fatalf("the case's own row is not a table row: %q", row)
	}
	flat := noSpace(screen)
	pos := 0
	for _, cell := range cells {
		if cell == "" {
			continue
		}
		for _, frag := range fragments5(noSpace(cell)) {
			j := strings.Index(flat[pos:], frag)
			if j < 0 {
				t.Fatalf("cols=%d chunk=%d: the row's cell %q lost the fragment %q at/after character %d — the row was truncated or re-ordered (the eaten head)\n  row:    %s\n  screen:\n%s",
					cols, chunk, cell, frag, pos, row, screen)
			}
			pos += j
		}
	}
}

// noSpace drops every whitespace run: the band's wrap and the grid's padding
// both insert spaces that are not in the source row.
func noSpace(s string) string { return strings.Join(strings.Fields(cutReadingFold(s)), "") }

// fragments5 walks s in 5-rune fragments, in order.
func fragments5(s string) []string {
	r := []rune(s)
	out := make([]string, 0, len(r)/5)
	for i := 0; i+5 <= len(r); i += 5 {
		out = append(out, string(r[i:i+5]))
	}
	return out
}
