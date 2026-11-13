package lineedit

import (
	"strings"
	"testing"
)

// region_yield_test.go — the layout's YIELD ORDER, asserted as arithmetic and on
// the rendered screen.
//
// The defect these tests close: computePlan placed the editing line at a FIXED
// row (`regionRowsMin`) and let the band/status arithmetic then claim rows the
// editing line needed, while the answer renderer drew whatever height the buffer
// wanted. At a small terminal height the two halves disagreed, and the `you>`
// line the user was typing on left the screen. The property under test is
// therefore not "the numbers look right" but the invariant behind it:
//
//	THE EDITING LINE IS NEVER SACRIFICED — the band yields rows first, and the
//	status row occupies EXACTLY one screen row whatever it carries.

// TestRegionYield_EditingLineSurvivesEveryHeightAboveTheFloor sweeps the whole
// input space the layout accepts (every terminal height above the floor, every
// editing-line wish, every menu height) and fails on any shape where the editing
// line is clipped out of the screen or the parts overlap. A layout change that
// reintroduces the arithmetic of the defect lands here at some (rows, want, menu)
// combination, not only at the one a single example would have pinned.
func TestRegionYield_EditingLineSurvivesEveryHeightAboveTheFloor(t *testing.T) {
	for rows := rowsMinUsable; rows <= a5Rows*2; rows++ {
		budget := editorRowsBudget(rowsMinUsable)
		for want := 1; want <= budget+2; want++ {
			for menu := 0; menu <= menuRowsCap+2; menu++ {
				p := computePlan(rows, want, menu)
				where := func(format string, args ...any) {
					t.Fatalf("rows=%d editorRowsWanted=%d menuRowsWanted=%d -> plan{editor=[%d..%d] rows=%d menu=[%d..] menuRows=%d band=[%d,%d] status=%d}: "+
						format, append([]any{rows, want, menu, p.EditorTop, p.EditorTop + p.EditorRows - 1, p.EditorRows, p.MenuTop, p.MenuRows, p.BandTop, p.BandBottom, p.StatusRow}, args...)...)
				}
				// The editing line keeps at least one row, and every row it
				// draws is on the screen.
				if p.EditorRows < minEditorRows {
					where("the editing line got %d rows, want at least %d", p.EditorRows, minEditorRows)
				}
				if p.EditorTop < 0 || p.EditorTop+p.EditorRows-1 >= rows {
					where("the editing line runs off the %d-row screen", rows)
				}
				// The band is above it (the scrollback condition) and the status
				// row is below everything drawn.
				if p.BandBottom >= p.EditorTop {
					where("the band's last row is not above the editing line")
				}
				if p.BandBottom-p.BandTop+1 < regionRowsMin {
					where("the band kept %d rows, want at least %d", p.BandBottom-p.BandTop+1, regionRowsMin)
				}
				if last := p.EditorTop + p.EditorRows - 1 + p.MenuRows; last > p.StatusRow {
					where("the chrome's last drawn row %d is below the status row %d", last, p.StatusRow)
				}
			}
		}
	}
}

// TestRegionYield_BandGivesRowsToTheChromeFirst is the yield direction stated as
// a value: at the smallest hosting height the band sits at EXACTLY its minimum
// and the editing line keeps its row — the band, not the editing line, absorbs
// the shortage — and every row a taller terminal adds goes to the band until the
// band's own cap takes over.
func TestRegionYield_BandGivesRowsToTheChromeFirst(t *testing.T) {
	// The band sits at exactly its minimum when the editing line asks for the
	// MOST it may have (its derived budget): that is the shape in which the band
	// has nothing left to give — the direction of the yield, at the height where
	// it is visible.
	budget := editorRowsBudget(rowsMinUsable)
	p := computePlan(rowsMinUsable, budget, 0)
	if got := p.BandBottom - p.BandTop + 1; got != regionRowsMin {
		t.Fatalf("band rows at rowsMinUsable=%d with the editing line at its budget %d = %d, want exactly the minimum %d — the band is the part that yields",
			rowsMinUsable, budget, got, regionRowsMin)
	}
	if p.EditorRows != budget {
		t.Fatalf("editing rows at rowsMinUsable = %d, want the derived budget %d", p.EditorRows, budget)
	}
	for extra := 1; extra <= menuRowsCap+statusRows+editorRowsCap; extra++ {
		rows := rowsMinUsable + extra
		p := computePlan(rows, minEditorRows, 0)
		// The band absorbs every row nobody else claimed; the wished ONE editing
		// row is what the editing line takes (the wish is below the budget at
		// every height here), so the band gets the rest.
		want := rows - statusRows - minEditorRows
		if got := p.BandBottom - p.BandTop + 1; got != want {
			t.Fatalf("band rows at rows=%d = %d, want %d (the extra rows belong to the band)", rows, got, want)
		}
		if p.EditorRows != minEditorRows {
			t.Fatalf("editing rows at rows=%d = %d, want the wished %d — extra rows must not move the editing line's height",
				rows, p.EditorRows, minEditorRows)
		}
	}
}

// TestRegionYield_MenuYieldsToTheEditingLineAndTheBand: a menu never takes the
// editing line's rows nor the band's minimum. What it gets at a given height is
// exactly the room left over once the editing line's wish and the band's floor
// are served — the frontier terminal has that room by construction (its height is
// derived from those two floors), so the assertion is the ARITHMETIC rather than a
// number, and the band/editor floors are asserted unharmed at every height.
func TestRegionYield_MenuYieldsToTheEditingLineAndTheBand(t *testing.T) {
	p := computePlan(rowsMinUsable, minEditorRows, menuRowsCap)
	if want := rowsMinUsable - statusRows - minEditorRows - regionRowsMin; p.MenuRows != want {
		t.Fatalf("menu rows at rowsMinUsable with a %d-row menu wanted = %d, want the leftover room %d (the editor's wish and the band's floor are served first)",
			menuRowsCap, p.MenuRows, want)
	}
	if p.EditorRows < minEditorRows || p.BandBottom-p.BandTop+1 < regionRowsMin {
		t.Fatalf("editor=%d band=%d at rowsMinUsable with a menu wanted: the menu must not take rows from either",
			p.EditorRows, p.BandBottom-p.BandTop+1)
	}
	// A terminal BELOW the frontier has no menu at all: the menu is the part with
	// the least claim, and there the layout is not installed (the caller falls
	// back and says so — RowsMinUsable is the number it reports).
	if p := computePlan(rowsMinUsable-1, minEditorRows, menuRowsCap); p.MenuRows != 0 {
		t.Fatalf("menu rows below the frontier = %d, want 0 — a terminal that cannot host the layout hosts no menu", p.MenuRows)
	}
	// At a tall terminal the menu gets its rows and the band keeps its minimum
	// (the band is the residual — it gives, the chrome keeps).
	p = computePlan(a5Rows, minEditorRows, menuRowsCap)
	if p.MenuRows != menuRowsCap {
		t.Fatalf("menu rows at rows=%d = %d, want the full %d", a5Rows, p.MenuRows, menuRowsCap)
	}
	if got := p.BandBottom - p.BandTop + 1; got < regionRowsMin {
		t.Fatalf("band rows = %d, want at least %d", got, regionRowsMin)
	}
}

// TestRegionYield_AWrappedEditingLineCannotOverwriteTheStatusRow is the rendered
// half of the same invariant, at the smallest hosting terminal: the renderer
// draws at most the rows the layout gave the editing line, so a buffer that wants
// far more rows than the floor terminal's budget cannot paint over the status row
// below it.
//
// Without the bound the renderer drew `len(rows)` rows from the editing line's
// first row downwards — the layout's own published EditorRows was never
// consulted — so at rowsMinUsable the surplus rows landed on the status row.
func TestRegionYield_AWrappedEditingLineCannotOverwriteTheStatusRow(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, rowsMinUsable, a5Cols)
	h.answer.SetStatus(func() Status { return Status{Label: "model-x", Live: a5StatusIdle} })
	h.answer.Tick()

	// A buffer several times the row width: it wants more rows than the floor
	// terminal's budget allows.
	h.ed.val.SetText(strings.Repeat("w", a5Cols*editorRowsCap*2))
	h.ed.repaint()
	h.replay()

	p := h.region.Plan()
	budget := editorRowsBudget(rowsMinUsable)
	if p.EditorRows > budget {
		t.Fatalf("this test needs a terminal whose layout clips the editing line (EditorRows=%d, budget=%d)", p.EditorRows, budget)
	}
	// The typed text is on the screen, on the editing line's row. At this floor
	// terminal the editing line's rows are its text window (one row) plus the
	// scroll chrome (the marker row above the text and the key row below it), so
	// the input's row sits below EditorTop — the marked row is what stands there
	// instead, stating how many rows of the user's input the window hides.
	//
	// The window FOLLOWS THE CARET, so at this height the row on screen is the
	// input's LAST one (`wwwww`), not its first: the prompt's row is the row the
	// marker accounts for. What matters, and what is asserted, is that the row
	// drawn is a row of the user's own text — the same cells the wrap produced
	// for that buffer row, by equality.
	win := h.ed.render.Window()
	all, _, _ := layoutText(h.ed.render.prompt, h.ed.val.Text(), h.ed.val.Cursor(), a5Cols)
	textRows := 0
	for i := 0; i < win.rows; i++ {
		off := win.windowOffsetsAt(i)
		if off == windowChromeOffset {
			continue
		}
		textRows++
		if off >= len(all) {
			t.Fatalf("the window's drawn row %d claims buffer row %d of %d", i, off, len(all))
		}
		if got := h.screen.Line(p.EditorTop + i); got != all[off] {
			t.Fatalf("editing row %d = %q, want the input's row %d (%q)\n%s",
				p.EditorTop+i, got, off, all[off], h.screen.Contents())
		}
	}
	if textRows == 0 {
		t.Fatalf("the editing line drew no row of the user's text at all\n%s", h.screen.Contents())
	}
	// The hidden rows are STATED, not silent: the row above the text accounts for
	// them (the tall input's surplus is no longer a silent cut).
	if got := h.screen.Line(p.EditorTop); !windowMarkerText(got) {
		t.Fatalf("editing row %d = %q, want the marker row accounting for the input above the window\n%s",
			p.EditorTop, got, h.screen.Contents())
	}
	// The status row still carries the status: the wrapped buffer did not reach it.
	if got := h.screen.Line(p.StatusRow); !strings.Contains(got, "model-x") {
		t.Fatalf("status row %d = %q — the editing line's surplus rows overwrote it (EditorRows=%d)\n%s",
			p.StatusRow, got, p.EditorRows, h.screen.Contents())
	}
}

// TestRegionYield_ShrinkBelowTheFloorTakesTheLayoutDown: a terminal that shrinks
// under the layout's minimum loses the layout rather than keeping a stale
// geometry whose scrolling band no longer exists. Keeping it would place the
// editing line at a row the terminal does not have. The degradation is announced
// with the height the composer needs (Rule 15), never silent.
func TestRegionYield_ShrinkBelowTheFloorTakesTheLayoutDown(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	if !h.region.Active() {
		t.Fatal("the harness's terminal does not host the layout")
	}
	if h.region.Resize(rowsMinUsable-1, a5Cols) {
		t.Fatalf("Resize to %d rows reported the layout still active — the editing line would be placed on a row the terminal does not have", rowsMinUsable-1)
	}
	if h.region.Active() {
		t.Fatal("the region stayed active after a shrink below the floor")
	}
	if h.ed.Answer() != nil {
		t.Fatal("an answer band was handed out with the layout down — the turn renderer would draw through a dead region")
	}
}

// TestStatusRow_IsExactlyOneScreenRowWithALongToolSummary is the status row's own
// bound: however long the fields are (a long model name AND a long tool digest,
// the combination poc 1b3d54b2's tool row made reachable), the composed row
// occupies exactly ONE screen row at the terminal's width.
//
// The measurement is the terminal's own rule — lay the text out left to right and
// wrap at the right edge — never a byte count: a 60-byte row of ASCII and a
// 60-byte row of double-width characters are not the same number of screen rows,
// and only the wrapped-rows count answers the question a user's terminal asks.
func TestStatusRow_IsExactlyOneScreenRowWithALongToolSummary(t *testing.T) {
	longModel := strings.Repeat("provider/model-name-", 4)
	longSummary := "internal/lineedit/" + strings.Repeat("very-long-file-name.go", 6)
	s := Status{
		Label: longModel + StatusSep + "iter 7" + StatusSep + toolGlyphForTest + " read " + longSummary + StatusSep + "12.3k tokens",
		Live:  a5StatusIdle,
	}
	for _, cols := range []int{20, 34, a5Cols, a5WideCols} {
		row := statusLine(s, cols)
		rows := wrappedRows(row, cols)
		if len(rows) != 1 {
			t.Fatalf("status row at cols=%d wrapped onto %d screen rows: %q\n--- rows ---\n%s",
				cols, len(rows), row, strings.Join(rows, "\n"))
		}
		if !strings.Contains(row, longModel) && !strings.Contains(row, statusEllipsis) {
			t.Fatalf("status row at cols=%d = %q cut the leading field without the %q marker — a cut field must read as continuing",
				cols, row, statusEllipsis)
		}
	}
	// The degenerate end of the range: a row one cell wide still returns a row
	// that occupies one screen row.
	if got := statusLine(s, 1); len(wrappedRows(got, 1)) != 1 {
		t.Fatalf("status row at cols=1 = %q, want exactly one screen row", got)
	}
}

// toolGlyphForTest mirrors cmd/agent's tool marker; the renderer is glyph-
// agnostic, so the test states its own rather than importing the REPL's.
const toolGlyphForTest = "⚙"

// wrappedRows lays text out the way a terminal does — left to right, wrapping at
// the right edge — and returns the screen rows it occupies. It is the measurement
// the status row's "exactly one screen row" claim is about: a display-cell count
// is not row count (a wide rune costs two cells), and a byte count is neither.
func wrappedRows(s string, cols int) []string {
	if cols <= 0 {
		return nil
	}
	var (
		rows []string
		b    strings.Builder
		col  int
	)
	for _, r := range s {
		w := runeCells(r)
		if col+w > cols {
			rows = append(rows, b.String())
			b.Reset()
			col = 0
		}
		b.WriteRune(r)
		col += w
	}
	return append(rows, b.String())
}
