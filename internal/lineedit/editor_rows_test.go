package lineedit

import (
	"testing"
)

// editor_rows_test.go — the editing line's ROW BUDGET is the terminal's, not a
// constant.
//
// The defect these tests close (devdoc/cli-ui-feasibility-study.md §1.1, work
// item U1): the repaint path clipped the editing line to a fixed
// `editorRowsCap` (4 rows) whatever the terminal's height, so on a tall terminal
// rows 5+ of the user's own wrapped input were never painted. The buffer kept
// the text and the cursor advanced past the clip — the user simply could not
// read what they had typed ("my typed text is wrapped without displaying in
// multi-line"). The measured table in the study shows the giveaway: the row
// count did not vary with terminal height at all (3/3/3 nonblank rows at 6, 10
// and 24 rows).
//
// The assertions are on the RENDERED CELL GRID (Rule 10.2), never on a callback
// or on the layout's own numbers: `screen.Line(r)` equality is the truth a user
// would see, and it is what fails when a row is dropped.

// editorRowsReproCols is the narrow terminal the report was measured on: at 40
// columns a 200-character-ish line occupies more rows than the old cap allowed.
const editorRowsReproCols = 40

// editorRowsReproRows is a standard 24-row terminal — the height at which the
// report's "I can't read my own typing" happens.
const editorRowsReproRows = 24

// editorRowsCapOldAbsolute is the value the defect used (region.go's
// `editorRowsCap = 4` before U1). It is named HERE because the repro's own
// premise — "this input is taller than the old cap" — must stay checkable now
// that production no longer carries an absolute cap.
const editorRowsCapOldAbsolute = 4

// editorRowsReproText is the smallest input that shows the clip at 40 columns:
// 180 characters on one line — five rows with the prompt, one more than the old
// absolute cap of 4.
const editorRowsReproText = "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789"

// editorRowsScreen is the screen the editor's own bytes produce after typing
// text on a rows × cols terminal — the state the user is looking at while the
// line is still being edited (the editor is NOT submitted, so the submit-time
// uncapping is not in play).
func editorRowsScreen(t *testing.T, rows, cols int, text string) (*a5Harness, *Screen) {
	t.Helper()
	h := newA5HarnessSized(t, nil, nil, rows, cols)
	h.ed.val.SetText(text)
	h.ed.repaint()
	h.replay()
	return h, h.screen
}

// TestEditorRows_ATallTerminalShowsEveryRowOfTheWrappedInput is the regression
// test for the operator's report, at the screen seam.
//
// Before the fix: `editorRowsCap` was an absolute 4, so on a 24-row terminal the
// screen carried 4 wrapped rows and the fifth row of the user's text was
// invisible WHILE TYPING (it was only painted on submit). The assertion below is
// per-row equality against the exact text the terminal should hold, so a dropped
// row, a shifted row or a stale row all fail.
func TestEditorRows_ATallTerminalShowsEveryRowOfTheWrappedInput(t *testing.T) {
	h, screen := editorRowsScreen(t, editorRowsReproRows, editorRowsReproCols, editorRowsReproText)

	want, _, _ := layoutText(h.ed.render.prompt, editorRowsReproText, len([]rune(editorRowsReproText)), editorRowsReproCols)
	if len(want) <= editorRowsCapOldAbsolute {
		t.Fatalf("the repro needs an input taller than the old absolute cap (%d): rows=%d", editorRowsCapOldAbsolute, len(want))
	}
	p := h.region.Plan()
	if p.EditorRows != len(want) {
		t.Fatalf("the layout gave the editing line %d rows on a %d-row terminal, want the %d rows the buffer occupies "+
			"(the terminal has room: the band keeps its floor of %d, the extra rows are the user's)",
			p.EditorRows, editorRowsReproRows, len(want), regionRowsMin)
	}
	// EVERY row of the wrapped input is on the screen, by equality — the row is
	// one of the layout's editing rows and carries exactly the text the wrap
	// produces.
	for i, row := range want {
		if got := screen.Line(p.EditorTop + i); got != row {
			t.Fatalf("editing row %d (screen row %d) = %q, want %q\n--- screen ---\n%s",
				i, p.EditorTop+i, got, row, screen.Contents())
		}
	}
	// The row the old cap dropped is the LAST one: assert it separately so the
	// failure names the defect.
	lastRow := p.EditorTop + len(want) - 1
	if got, wantRow := screen.Line(lastRow), want[len(want)-1]; got != wantRow {
		t.Fatalf("the user's last row of input is NOT on the screen: screen row %d = %q, want %q — typed text past the old absolute cap of %d rows was invisible while editing\n%s",
			lastRow, got, wantRow, editorRowsCapOldAbsolute, screen.Contents())
	}
	// The band still exists with its floor and still sits above the editing line
	// (the scrollback condition): the editor grew, it did not eat the layout.
	if got := h.region.BandRows(); got < regionRowsMin {
		t.Fatalf("band rows = %d, want at least %d — the editor's growth must not starve the band", got, regionRowsMin)
	}
	if p.BandBottom >= p.EditorTop {
		t.Fatalf("band ends at row %d, the editing line starts at %d — the band must stay above it", p.BandBottom, p.EditorTop)
	}
}

// TestEditorRows_TheBudgetGrowsWithTheTerminalHeight is the property the report
// actually complained about: the editing line's row budget must MOVE with the
// terminal's height. Before the fix it was the same number at every height.
func TestEditorRows_TheBudgetGrowsWithTheTerminalHeight(t *testing.T) {
	heights := []int{rowsMinUsable, 6, 8, 10, 12, editorRowsReproRows, 40}
	seen := make([]int, 0, len(heights))
	for _, rows := range heights {
		h, screen := editorRowsScreen(t, rows, editorRowsReproCols, editorRowsReproText)
		budget := editorRowsBudget(rows)
		p := h.region.Plan()
		want := budget
		if need := h.ed.val.RowsFor(h.ed.render.prompt, editorRowsReproCols); want > need {
			// The budget is a CEILING: a short buffer does not reserve rows it
			// does not occupy (the band absorbs them).
			want = need
		}
		if p.EditorRows != want {
			t.Fatalf("rows=%d: the editing line got %d rows, want min(budget=%d, buffer=%d) = %d",
				rows, p.EditorRows, budget, h.ed.val.RowsFor(h.ed.render.prompt, editorRowsReproCols), want)
		}
		// Every reserved row carries text: the plan is not a promise the drawer
		// ignores (the old head-clip drew fewer rows than it promised).
		for r := p.EditorTop; r < p.EditorTop+p.EditorRows; r++ {
			if screen.Line(r) == "" {
				t.Fatalf("rows=%d: editing row %d is blank although the layout reserved it\n--- screen ---\n%s",
					rows, r, screen.Contents())
			}
		}
		seen = append(seen, p.EditorRows)
	}
	// Monotonic in the terminal's height, and the extremes differ: the old code
	// was the constant 4 at every height.
	for i := 1; i < len(seen); i++ {
		if seen[i] < seen[i-1] {
			t.Fatalf("editing rows fell as the terminal grew: heights=%v rows=%v", heights, seen)
		}
	}
	if seen[len(seen)-1] <= seen[0] {
		t.Fatalf("editing rows did not grow with the terminal: heights=%v rows=%v", heights, seen)
	}
	if seen[len(seen)-1] <= editorRowsCapOldAbsolute {
		t.Fatalf("a %d-row terminal drew only %d editing rows — the old absolute cap of %d is still in force (heights=%v rows=%v)",
			heights[len(heights)-1], seen[len(seen)-1], editorRowsCapOldAbsolute, heights, seen)
	}
}

// TestEditorRows_AShortTerminalDegradesGracefullyAndKeepsTheBandFloor is the
// degradation half: at 6 rows there is room for the band's floor, three editing
// rowsMinUsable is the smallest hosting height — the band's floor + the editing
// line's own floor (its text window plus the scroll chrome) + the status row —
// and that is exactly what the layout gives. The band keeps regionRowsMin, every
// row of the editing span carries either the input or a MARKER that accounts for
// the input it hides (never a blank row, and never a row that hides text in
// silence), and the status row is untouched and still the last row.
//
// (The buffer is not shortened — and since the scrolling window landed this test
// is where the DISCLOSURE contract is pinned at the frontier: the shortest
// terminal that hosts the layout still shows a row of the user's own input, still
// states that 4 rows are hidden above it, and still names the keys that reach
// them.)
func TestEditorRows_AShortTerminalDegradesGracefullyAndKeepsTheBandFloor(t *testing.T) {
	const rows = rowsMinUsable
	h, screen := editorRowsScreen(t, rows, editorRowsReproCols, editorRowsReproText)
	p := h.region.Plan()

	if got := h.region.BandRows(); got < regionRowsMin {
		t.Fatalf("band rows = %d, want at least %d — the band must never be starved", got, regionRowsMin)
	}
	if p.BandTop != editorTopRow {
		t.Fatalf("band top = %d, want %d (the screen's first row — the scrollback condition)", p.BandTop, editorTopRow)
	}
	if want := editorRowsBudget(rows); p.EditorRows != want {
		t.Fatalf("editing rows at %d rows = %d, want the derived budget %d", rows, p.EditorRows, want)
	}
	if p.StatusRow != rows-1 {
		t.Fatalf("status row = %d, want %d (the last row)", p.StatusRow, rows-1)
	}
	// The editing line is inside the screen and carries input (the head of the
	// buffer): clamping is a display bound, never a lost buffer.
	if p.EditorTop < 0 || p.EditorTop+p.EditorRows-1 >= p.StatusRow {
		t.Fatalf("the editing line's span [%d..%d] is not strictly above the status row %d",
			p.EditorTop, p.EditorTop+p.EditorRows-1, p.StatusRow)
	}
	for r := p.EditorTop; r < p.EditorTop+p.EditorRows; r++ {
		if got := screen.Line(r); got == "" {
			t.Fatalf("editing row %d is blank\n--- screen ---\n%s", r, screen.Contents())
		}
	}
	// The editing line shows a WINDOW of the input: at this height the chrome
	// takes two of its rows (the marked row above the text and the key row below
	// it), and the remaining row carries the input's LAST row — the window follows
	// the caret, which is at the end of the text the harness typed (the head
	// window this test used to assert was the arrangement that made the user's
	// own typing move out of sight as the line grew).
	all, cursorRow, _ := layoutText(promptForTest, editorRowsReproText, len([]rune(editorRowsReproText)), editorRowsReproCols)
	win := h.ed.render.Window()
	if win.windowOffsetsAt(0) != windowChromeOffset {
		t.Fatalf("the editing line's first row is %q, want the marker that states the 4 hidden rows above the window\n--- screen ---\n%s",
			screen.Line(p.EditorTop), screen.Contents())
	}
	lastDrawn := win.windowOffsetsAt(win.rows - 2) // the row above the key row is the window's last TEXT row
	if lastDrawn == windowChromeOffset || lastDrawn >= len(all) {
		t.Fatalf("the window's last text row carries buffer row %d (of %d)\n--- screen ---\n%s", lastDrawn, len(all), screen.Contents())
	}
	if got := screen.Line(p.EditorTop + win.rows - 2); got != all[lastDrawn] {
		t.Fatalf("the window's last text row = %q, want the input's row %d (%q)\n--- screen ---\n%s",
			got, lastDrawn, all[lastDrawn], screen.Contents())
	}
	_ = cursorRow
	// The cursor is inside the editing line's span: it is where typing happens,
	// and a cursor below it would write on the menu or the status row.
	if screen.CurRow < p.EditorTop || screen.CurRow > p.EditorTop+p.EditorRows-1 {
		t.Fatalf("the cursor is on row %d, outside the editing line's rows [%d..%d]\n--- screen ---\n%s",
			screen.CurRow, p.EditorTop, p.EditorTop+p.EditorRows-1, screen.Contents())
	}
}

// TestEditorRows_TheBudgetIsTheTerminalsAndNotAConstant is the arithmetic half,
// stated against the ONE definition (editorRowsBudget) rather than a copied
// number: the budget is the rows above the status line less the band's floor,
// clamped by the cosmetic ceiling and floored at EditorRowsMin. It is exactly
// EditorRowsMin at the smallest hosting height — the band's floor and the status
// row are reserved first — and the plan applies the SAME bound, so the number the
// editor asks for and the number the layout reserves cannot disagree.
//
// The floor is EditorRowsMin and not minEditorRows since the scrolling window
// landed (window.go): a hosted layout always shows one row of the user's text
// beside the marker row and the key row, so the mechanism is reachable at EVERY
// hosting height instead of only on a tall terminal.
func TestEditorRows_TheBudgetIsTheTerminalsAndNotAConstant(t *testing.T) {
	// Below the layout floor there is no reserved layout: one row is all a
	// caller drawing through the degenerate plan may claim.
	for rows := 1; rows < rowsMinUsable; rows++ {
		if got := editorRowsBudget(rows); got != minEditorRows {
			t.Fatalf("editorRowsBudget(%d) = %d, want %d (no layout below the floor)", rows, got, minEditorRows)
		}
	}
	if got := editorRowsBudget(rowsMinUsable); got != EditorRowsMin {
		t.Fatalf("editorRowsBudget(%d) = %d, want %d (the band's floor and the status row are reserved first; the editing line still keeps its text row and its scroll chrome)",
			rowsMinUsable, got, EditorRowsMin)
	}
	for rows := rowsMinUsable; rows <= rowsMinUsable+editorRowsCap+2; rows++ {
		got := editorRowsBudget(rows)
		want := rows - statusRows - regionRowsMin
		if want > editorRowsCap {
			want = editorRowsCap
		}
		if want < EditorRowsMin {
			want = EditorRowsMin
		}
		if got != want {
			t.Fatalf("editorRowsBudget(%d) = %d, want %d", rows, got, want)
		}
		// The plan honours the same number, from the SAME definition: asking for
		// more than the budget cannot reserve more than the budget, so the band's
		// floor always survives.
		p := computePlan(rows, got+10, 0)
		if p.EditorRows != got {
			t.Fatalf("computePlan(%d, wish=%d) reserved %d editing rows, want the derived budget %d", rows, got+10, p.EditorRows, got)
		}
		if band := p.BandBottom - p.BandTop + 1; band < regionRowsMin {
			t.Fatalf("computePlan(%d) left the band %d rows, want at least %d", rows, band, regionRowsMin)
		}
	}
	// A tall terminal's budget is genuinely larger than a short one's — the
	// capability the report asked for.
	if editorRowsBudget(editorRowsReproRows) <= editorRowsBudget(6) {
		t.Fatalf("budget at %d rows = %d, at 6 rows = %d — it must grow with the terminal",
			editorRowsReproRows, editorRowsBudget(editorRowsReproRows), editorRowsBudget(6))
	}
}
