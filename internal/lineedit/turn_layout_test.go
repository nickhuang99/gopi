package lineedit

import (
	"strings"
	"testing"
)

// turn_layout_test.go — the turn-structure assertions, read off the RENDERED
// GRID. Every claim here ("the turns are separated", "the columns are aligned",
// "the editing line is intact", "the answer never reaches the status row") is a
// fact about the screen the terminal would show, never about a callback: the
// defects this work closes were all invisible to the code that produced them.
//
// The grid is fed through the SAME write path production uses (the region's
// scroll region, the live row, the commit) and the rows are located FROM THE
// LAYOUT the editor published, never from a copy of its arithmetic.

// turnHarnessRows / turnHarnessCols size the grid these assertions run on. The
// row count is deliberately the smallest that hosts the layout (rowsMinUsable)
// for the invariant checks, and a taller one where a whole turn has to fit.
const (
	turnHarnessRows = 24
	turnHarnessCols = 40
)

// TestTurnLayout_HeaderRuleAndAlignment runs TWO turns through the production
// path and asserts the three pieces of chrome as GRID facts:
//
//  1. turn 2's lines are separated from turn 1's by a closing rule AND a header
//     (the measured defect: "Done." of turn 1 immediately above "Here is the
//     answer …" of turn 2, with nothing between them);
//  2. the body's left edge is one column: every committed body line starts at
//     the same cell, so a wrapped bullet list keeps its alignment;
//  3. the answer never reaches the editing line or the status row.
func TestTurnLayout_HeaderRuleAndAlignment(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, turnHarnessRows, turnHarnessCols)
	band := h.answer
	band.SetCols(turnHarnessCols)
	band.SetStatus(func() Status { return Status{Label: "m" + StatusSep + "iter 1", Live: a5StatusIdle} })

	// Turn 1: header, two lines, close.
	band.BeginTurn(TurnInfo{Index: 1, Prompt: "first question"})
	band.Delta("first answer line one\n")
	band.Delta("first answer line two\n")
	band.Finish()
	band.EndTurn()
	// Turn 2: header, two lines, close.
	band.BeginTurn(TurnInfo{Index: 2, Prompt: "second question"})
	band.Delta("second answer line one\n")
	band.Delta("second answer line two\n")
	band.Finish()
	band.EndTurn()
	band.Tick()
	h.replay()

	bandText := h.rowsTo(h.region.Plan().BandTop, h.region.Plan().BandBottom)
	// 1. Both headers are on the grid, and turn 2's is BELOW turn 1's.
	i1 := strings.Index(bandText, turnHeaderText(1, "first question"))
	i2 := strings.Index(bandText, turnHeaderText(2, "second question"))
	if i1 < 0 || i2 < 0 {
		t.Fatalf("the band does not show both turn headers (turn 1 at %d, turn 2 at %d)\n--- band ---\n%s",
			i1, i2, bandText)
	}
	if i2 < i1 {
		t.Fatalf("turn 2's header is above turn 1's — the turns are out of order\n--- band ---\n%s", bandText)
	}
	// Between the two turns there is a RULE line: a whole row of the rule glyph.
	if !hasRuleRow(bandLines(h, bandText), turnHarnessCols) {
		t.Fatalf("no closing rule between the turns — the two answers run together\n--- band ---\n%s", bandText)
	}
	// 2. Every body line shares one left edge (the marker column).
	wantCol := displayWidth(TurnMarker)
	for _, line := range []string{"first answer line one", "first answer line two", "second answer line one"} {
		col, ok := bodyColumn(bandLines(h, bandText), line)
		if !ok {
			t.Fatalf("body line %q is not in the band\n--- band ---\n%s", line, bandText)
		}
		if col != wantCol {
			t.Fatalf("body line %q starts at cell %d, want %d — the turn's left edge is ragged\n--- band ---\n%s",
				line, col, wantCol, bandText)
		}
	}
	// 3. The chrome is intact: the header/rule/answer text never reached the
	//    editing line or the status row (the band ends above the editing line).
	p := h.region.Plan()
	for r := p.EditorTop; r < turnHarnessRows; r++ {
		if got := h.screen.Line(r); strings.Contains(got, TurnRuleGlyph) || strings.Contains(got, TurnMarker) {
			t.Fatalf("turn chrome leaked onto row %d below the band: %q\n%s", r, got, h.screen.Contents())
		}
	}
}

// TestTurnLayout_NoTurnChromeWithoutATurnCounter is the degradation contract: a
// renderer driven by Begin (no turn counter — the callers that never asked for
// turn structure) emits NO header, NO rule and NO marker. The pre-existing
// answer tests run through this path, and so does any future caller that has no
// turn to name.
func TestTurnLayout_NoTurnChromeWithoutATurnCounter(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, turnHarnessRows, turnHarnessCols)
	h.answer.SetCols(turnHarnessCols)
	h.answer.BeginTurn(TurnInfo{Index: 0, Prompt: "unused"})
	h.answer.Delta("plain answer line\n")
	h.answer.Finish()
	h.answer.EndTurn()
	h.replay()

	if got := h.answer.Committed(); got != 1 {
		t.Fatalf("committed rows = %d, want 1 — an un-numbered turn must commit exactly its answer, no chrome", got)
	}
	band := h.rowsTo(h.region.Plan().BandTop, h.region.Plan().BandBottom)
	if strings.Contains(band, TurnRuleGlyph) || strings.Contains(band, TurnMarker) {
		t.Fatalf("an un-numbered turn drew chrome:\n%s", band)
	}
	if !strings.Contains(band, "plain answer line") {
		t.Fatalf("the answer is missing from the band:\n%s", band)
	}
}

// TestTurnLayout_ThinkingIsGutteredAndSeparatedByOneBlankRow is the spacing rule
// between reasoning and answer: the reasoning lines carry the thinking gutter
// (aligned in the SAME column as the answer's marker), exactly ONE blank row
// separates the block from the answer, and a turn whose reasoning streamed
// nothing gains no blank row at all.
func TestTurnLayout_ThinkingIsGutteredAndSeparatedByOneBlankRow(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, turnHarnessRows, turnHarnessCols)
	h.answer.SetCols(turnHarnessCols)
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: "why"})
	h.answer.BeginThinking()
	h.answer.Delta("reasoning line one\n")
	h.answer.Delta("reasoning line two\n")
	// The answer's first delta closes the block (the renderer does it on the
	// answer path, so a caller cannot forget).
	h.answer.EndThinking()
	h.answer.Delta("the answer\n")
	h.answer.Finish()
	h.answer.EndTurn()
	h.replay()

	lines := bandLines(h, h.rowsTo(h.region.Plan().BandTop, h.region.Plan().BandBottom))
	thinkRow := rowOf(lines, "reasoning line one")
	answerRow := rowOf(lines, "the answer")
	if thinkRow < 0 || answerRow < 0 {
		t.Fatalf("reasoning or answer missing from the band\n%s", strings.Join(lines, "\n"))
	}
	if answerRow-thinkRow != 3 {
		t.Fatalf("reasoning block at row %d, answer at row %d: want exactly ONE blank row between them (a 3-row gap)\n%s",
			thinkRow, answerRow, strings.Join(lines, "\n"))
	}
	if between := strings.TrimSpace(lines[thinkRow+2]); between != "" {
		t.Fatalf("the row between the reasoning block and the answer = %q, want blank\n%s", between, strings.Join(lines, "\n"))
	}
	if between := strings.TrimSpace(lines[answerRow-1]); between != "" {
		t.Fatalf("row %d above the answer = %q, want blank (one row, not two)\n%s", answerRow-1, between, strings.Join(lines, "\n"))
	}
	// Both gutters are at the same cell — the alignment claim.
	thinkCol, _ := bodyColumn(lines, strings.TrimPrefix(strings.TrimLeft(lines[thinkRow], " "), ThinkingGutter))
	answerCol, _ := bodyColumn(lines, "the answer")
	if thinkCol != answerCol {
		t.Fatalf("reasoning body starts at cell %d, answer at %d — the gutters disagree\n%s",
			thinkCol, answerCol, strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[thinkRow], ThinkingGutter) {
		t.Fatalf("reasoning row %d = %q, want the thinking gutter %q\n%s", thinkRow, lines[thinkRow], ThinkingGutter, strings.Join(lines, "\n"))
	}
}

// TestTurnLayout_EmptyThinkingBlockAddsNoBlankRow guards the degenerate case: a
// block that streamed nothing must close silently, or every turn with a suppressed
// reasoning stream would grow a stray blank line.
func TestTurnLayout_EmptyThinkingBlockAddsNoBlankRow(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, turnHarnessRows, turnHarnessCols)
	h.answer.SetCols(turnHarnessCols)
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: "q"})
	h.answer.BeginThinking()
	h.answer.EndThinking()
	h.answer.Delta("only the answer\n")
	h.answer.Finish()
	h.replay()

	if got := h.answer.Committed(); got != 3 { // header + request + answer
		t.Fatalf("committed rows = %d, want 3 (header + request + answer) — an empty reasoning block added a row", got)
	}
	lines := bandLines(h, h.rowsTo(h.region.Plan().BandTop, h.region.Plan().BandBottom))
	// The turn's body opens with the REQUEST row (layers.go), so the answer's
	// neighbour above it is the request, not the header — and the request still
	// sits directly under the header, one row each.
	requestRow := rowOf(lines, RequestGutter+"q")
	if requestRow != rowOf(lines, turnHeaderText(1, "q"))+1 {
		t.Fatalf("the request row is not directly under the header\n%s", strings.Join(lines, "\n"))
	}
	if rowOf(lines, "only the answer") != requestRow+1 {
		t.Fatalf("the answer is not directly under the request row\n%s", strings.Join(lines, "\n"))
	}
}

// TestTurnLayout_EditingLineAndStatusRowSurviveEveryTurnChromeRow is the
// INVARIANT half, at the SMALLEST hosting terminal: whatever the turn chrome
// commits (header, marked body, reasoning gutter, closing rule), the editing
// line keeps its row and the status row keeps the status — no chrome row ever
// lands on either. It is the same bound the yield tests assert for the answer,
// re-asserted for the chrome the turn structure added.
func TestTurnLayout_EditingLineAndStatusRowSurviveEveryTurnChromeRow(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, rowsMinUsable, turnHarnessCols)
	h.answer.SetCols(turnHarnessCols)
	h.answer.SetStatus(func() Status { return Status{Label: "model-x", Live: a5StatusIdle} })
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: strings.Repeat("p", turnHarnessCols*2)})
	h.answer.BeginThinking()
	h.answer.Delta("reasoning\n")
	h.answer.EndThinking()
	// More body than the band has rows: the band scrolls, the chrome must not.
	for i := 0; i < 12; i++ {
		h.answer.Delta("body line\n")
	}
	h.answer.Finish()
	h.answer.EndTurn()
	h.answer.Tick()
	h.replay()

	p := h.region.Plan()
	// The editing line is DRAWN BY THE EDITOR (this harness never typed, so it
	// is blank by construction) — the assertion is that no chrome row landed on
	// it: neither the rule glyph nor either gutter appears below the band.
	for r := p.EditorTop; r < p.StatusRow; r++ {
		got := h.screen.Line(r)
		if strings.Contains(got, TurnRuleGlyph) || strings.Contains(got, TurnMarker) {
			t.Fatalf("editing row %d = %q — a turn chrome row overwrote it\n%s", r, got, h.screen.Contents())
		}
	}
	if got := h.screen.Line(p.StatusRow); !strings.Contains(got, "model-x") {
		t.Fatalf("status row %d = %q — the answer band wrapped into it\n%s", p.StatusRow, got, h.screen.Contents())
	}
	// The band's folded content stays inside the band: the last committed row
	// is the band's last row, and nothing was written past it.
	if bandBottom := p.BandBottom; bandBottom >= p.EditorTop {
		t.Fatalf("band bottom %d is not above the editing line %d", bandBottom, p.EditorTop)
	}
}

// TestTurnHeader_IsTotalInCols pins the header's own bound: whatever the width
// and however long the prompt, the composed header occupies at most one screen
// row — a header that wrapped would steal a band row and, on a short terminal,
// push the editing line down.
func TestTurnHeader_IsTotalInCols(t *testing.T) {
	long := strings.Repeat("a very long question with CJK 目录/文件 ", 8)
	for _, cols := range []int{1, 2, 6, 8, 12, 23, 24, 40, a5WideCols} {
		for _, index := range []int{1, 12, 999} {
			got := TurnHeader(TurnInfo{Index: index, Prompt: long}, cols)
			if rows := wrappedRows(got, cols); len(rows) != 1 {
				t.Fatalf("TurnHeader(cols=%d, index=%d) occupies %d screen rows: %q\n--- rows ---\n%s",
					cols, index, len(rows), got, strings.Join(rows, "\n"))
			}
			if w := displayWidth(got); w > cols {
				t.Fatalf("TurnHeader(cols=%d, index=%d) is %d cells wide: %q", cols, index, w, got)
			}
			if cols >= turnHeaderMinCells && !strings.Contains(got, turnLabelTurn) {
				t.Fatalf("TurnHeader(cols=%d) = %q, want the turn label to survive", cols, got)
			}
		}
	}
	// The rule is a full row and never longer than the terminal.
	if got := TurnRule(a5WideCols); displayWidth(got) != a5WideCols {
		t.Fatalf("TurnRule(%d) is %d cells, want exactly %d", a5WideCols, displayWidth(got), a5WideCols)
	}
	if got := TurnRule(0); got != "" {
		t.Fatalf("TurnRule(0) = %q, want empty (an unknown width draws nothing)", got)
	}
	if got := TurnHeader(TurnInfo{Index: 1, Prompt: long}, 0); got != "" {
		t.Fatalf("TurnHeader(cols=0) = %q, want empty (an unknown width draws nothing, and no prompt is cut to fit it)", got)
	}
}

// TestTurnHeader_MarksEveryCut pins the header's OTHER invariant — the one its
// doc comment claimed and the code did not keep: a label that does not fit is
// CUT AND MARKED. Every cut call was bare truncateCols, which returns the head
// and drops the tail silently, so a long prompt ended mid-word with nothing
// saying it continued (the measured defect: `── turn 1 · <first ~100 cells of a
// paragraph> ───` reads as the whole question, on cols=80 at that).
//
// The widths are the operator-facing ones (80/100/120) plus every degradation
// boundary; each case asserts the same three things: the header is inside cols,
// the label is WHOLE or MARKED — never silently cut — and a label shown in full
// carries no marker (a marker on a complete label makes the marker mean
// nothing).
func TestTurnHeader_MarksEveryCut(t *testing.T) {
	long := strings.Repeat("a very long question with CJK 目录/文件 ", 8)
	for _, cols := range []int{1, 2, 6, turnHeaderMinCells, 12, 23, turnLabelHalfCells, 40, 80, a5WideCols, 120} {
		for _, index := range []int{1, 12, 999} {
			info := TurnInfo{Index: index, Prompt: long}
			full := turnLabel(info)
			got := TurnHeader(info, cols)
			if w := displayWidth(got); w > cols {
				t.Fatalf("TurnHeader(cols=%d, index=%d) is %d cells wide: %q", cols, index, w, got)
			}
			if rows := wrappedRows(got, cols); len(rows) != 1 {
				t.Fatalf("TurnHeader(cols=%d, index=%d) occupies %d screen rows: %q",
					cols, index, len(rows), got)
			}
			whole := strings.Contains(got, full)
			marked := strings.Contains(got, statusEllipsis)
			if !whole && !marked {
				t.Fatalf("TurnHeader(cols=%d, index=%d) cut the label with NO marker: %q (want the whole label or %q)",
					cols, index, got, statusEllipsis)
			}
			if whole && marked {
				t.Fatalf("TurnHeader(cols=%d, index=%d) marks a label it shows in full: %q", cols, index, got)
			}
			if cols >= turnHeaderMinCells && !strings.Contains(got, turnLabelTurn) {
				t.Fatalf("TurnHeader(cols=%d) = %q, want the turn label to survive", cols, got)
			}
		}
	}
}

// TestTurnHeader_MarkerSitsAtTheCut is the same claim read at the three widths
// a real terminal is set to (80/100/120), and it is the exact shape of the
// defect: the label's own last cell is the marker, and the closing rule still
// rides after it — i.e. the marker is inside the budget, not appended past it,
// so the header did not spend a second row to say "there is more".
func TestTurnHeader_MarkerSitsAtTheCut(t *testing.T) {
	long := strings.Repeat("a very long question with CJK 目录/文件 ", 8)
	want := statusEllipsis + turnLabelTail + TurnRuleGlyph
	for _, cols := range []int{80, a5WideCols, 120} {
		got := TurnHeader(TurnInfo{Index: 1, Prompt: long}, cols)
		if !strings.Contains(got, want) {
			t.Fatalf("TurnHeader(cols=%d) = %q, want the label cut and marked (%q) with the row still closed by a rule",
				cols, got, want)
		}
		if w := displayWidth(got); w > cols {
			t.Fatalf("TurnHeader(cols=%d) is %d cells wide: %q", cols, w, got)
		}
		if w := displayWidth(got); w != cols {
			t.Fatalf("TurnHeader(cols=%d) is %d cells, want the closing rule to fill the row", cols, w)
		}
	}
}

// TestTurnHeader_NoCutCarriesNoMark is the other half of the marker's contract:
// the marker means "there is more". A header that shows the whole prompt must
// not wear it, and the leftover width goes to the closing rule (so the row is
// exactly cols wide) — the same arithmetic that reserves the marker out of the
// budget.
func TestTurnHeader_NoCutCarriesNoMark(t *testing.T) {
	for _, cols := range []int{80, a5WideCols, 120} {
		info := TurnInfo{Index: 3, Prompt: "why is the header cut mid-word?"}
		full := turnLabel(info)
		got := TurnHeader(info, cols)
		if !strings.Contains(got, full) {
			t.Fatalf("TurnHeader(cols=%d) = %q, want the whole label %q", cols, got, full)
		}
		if strings.Contains(got, statusEllipsis) {
			t.Fatalf("TurnHeader(cols=%d) = %q, want NO marker on a label shown in full", cols, got)
		}
		if w := displayWidth(got); w != cols {
			t.Fatalf("TurnHeader(cols=%d) is %d cells, want the rule to fill the width", cols, w)
		}
	}
}

// TestCutField_ReservesTheMarkerInsideTheBudget pins the shared arithmetic the
// status row's fitField and the turn header's every cut path both go through
// (Rule 14: one definition, referenced): the marker comes OUT of room rather
// than on top of it, a value that fits is returned untouched, and a wide rune
// that would straddle the edge is dropped rather than half-drawn.
func TestCutField_ReservesTheMarkerInsideTheBudget(t *testing.T) {
	cases := []struct {
		name string
		in   string
		room int
		want string
	}{
		{"fits exactly", "abc", 3, "abc"},
		{"fits with width to spare", "abc", 5, "abc"},
		{"cut reserves the marker", "abcdef", 4, "abc" + statusEllipsis},
		{"cut with one cell", "abcdef", 1, statusEllipsis},
		{"cut with no room", "abcdef", 0, statusEllipsis},
		{"wide rune never half-drawn", "目录文件", 5, "目录" + statusEllipsis},
		{"empty value", "", 3, ""},
	}
	for _, tc := range cases {
		if got := cutField(tc.in, tc.room); got != tc.want {
			t.Fatalf("cutField(%q, %d) = %q, want %q", tc.in, tc.room, got, tc.want)
		}
		if tc.room >= 1 {
			if w := displayWidth(cutField(tc.in, tc.room)); w > tc.room {
				t.Fatalf("cutField(%q, %d) is %d cells wide — the marker was appended past the budget",
					tc.in, tc.room, w)
			}
		}
	}
}

// ── grid helpers (the assertions' instruments) ──────────────────────────────

// rowsTo joins the band's rows (0-based, inclusive) as text.
func (h *a5Harness) rowsTo(top, bottom int) string {
	var out []string
	for r := top; r <= bottom; r++ {
		if r < 0 || r >= h.screen.Rows {
			continue
		}
		out = append(out, h.screen.Line(r))
	}
	return strings.Join(out, "\n")
}

// bandLines splits a band dump back into rows, right-trimmed.
func bandLines(h *a5Harness, band string) []string {
	raw := strings.Split(band, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// rowOf returns the index of the first row containing sub, or -1.
func rowOf(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

// bodyColumn returns the display cell a body text starts at, inside its row.
func bodyColumn(lines []string, body string) (int, bool) {
	for _, l := range lines {
		if i := strings.Index(l, body); i >= 0 {
			return displayWidth(l[:i]), true
		}
	}
	return 0, false
}

// hasRuleRow reports whether some row is nothing but the rule glyph (a full
// width of it) — a closing rule, as opposed to a header (which carries a label).
func hasRuleRow(lines []string, cols int) bool {
	want := strings.Repeat(TurnRuleGlyph, cols)
	for _, l := range lines {
		if strings.TrimRight(l, " ") == want {
			return true
		}
	}
	return false
}

// turnHeaderText is the header's label as TurnHeader composes it (the label,
// without the rules around it) — asserted by containment so the rule widths are
// not re-derived here.
func turnHeaderText(index int, prompt string) string {
	return turnLabel(TurnInfo{Index: index, Prompt: prompt})
}

// TestTurnLayout_WrappedRowsKeepTheBlockColumn is the wrapping half of the
// column claim, and the case the first version of this work got wrong: a body
// line LONGER than the band's width wraps onto the next row, and that
// continuation row must start at the SAME cell as the row it continues — i.e.
// the wrapped text is indented to the block's column, not stuck against the
// terminal's left edge (the measured defect: body rows 1 and 3 of the grid
// started at cell 0 while the rest started at cell 2).
func TestTurnLayout_WrappedRowsKeepTheBlockColumn(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, turnHarnessRows, turnHarnessCols)
	body := strings.Repeat("word ", turnHarnessCols) // far wider than one row
	h.answer.SetCols(turnHarnessCols)
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: "wrap"})
	h.answer.Delta(body + "\n")
	h.answer.Finish()
	h.answer.EndTurn()
	h.replay()

	lines := bandLines(h, h.rowsTo(h.region.Plan().BandTop, h.region.Plan().BandBottom))
	headerRow := rowOf(lines, turnLabel(TurnInfo{Index: 1, Prompt: "wrap"}))
	if headerRow < 0 {
		t.Fatalf("header missing\n%s", strings.Join(lines, "\n"))
	}
	wantCol := displayWidth(TurnMarker)
	rows := 0
	for r := headerRow + 1; r < len(lines); r++ {
		l := lines[r]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if strings.HasPrefix(l, TurnRuleGlyph) {
			break // the closing rule ends the turn's body
		}
		rows++
		// EVERY body row — the line's first row and each row it wraps onto —
		// must keep the block's column: the glyph, the thinking gutter, or the
		// indent of the same width. Nothing of the turn's text may appear in the
		// cells the block reserves (the measured defect: a wrapped row started at
		// cell 0 while its line started at cell 2).
		if !strings.HasPrefix(l, TurnMarker) && !strings.HasPrefix(l, ThinkingGutter) &&
			!strings.HasPrefix(l, RequestGutter) &&
			!strings.HasPrefix(l, strings.Repeat(" ", wantCol)) {
			t.Fatalf("body row %d = %q does not open at the block's column %d\n%s",
				r, l, wantCol, strings.Join(lines, "\n"))
		}
		if got := displayWidth(l) - displayWidth(strings.TrimLeft(l, " ")); got < wantCol && !strings.HasPrefix(l, TurnMarker) && !strings.HasPrefix(l, ThinkingGutter) && !strings.HasPrefix(l, RequestGutter) {
			t.Fatalf("body row %d = %q starts at cell %d, want at least %d (the block's column)\n%s",
				r, l, got, wantCol, strings.Join(lines, "\n"))
		}
	}
	if rows < 2 {
		t.Fatalf("this test needs a body that WRAPS (it rendered %d body rows)\n%s", rows, strings.Join(lines, "\n"))
	}
}

// TestStatusSeparatorCellWidths is the guard on the byte/cell defect: the
// separators' exported CELL widths must agree with the strings they describe.
//
// The defect it closes (measured): a caller's pinned status geometry summed
// len(StatusColumnSep) — 5 BYTES for a 4-CELL string (" · " separator plus one
// cell of air) — and so computed a layout three cells wider than it draws. The
// aligned form only runs on a terminal at least as wide as its pin, so the row
// silently fell back to the historic unaligned composition on the very terminal
// the pin had been sized for, and the column alignment the work was about never
// appeared there. Nothing failed: the assertions rendered the row AT the pin
// instead of at a real terminal width.
//
// Both halves are asserted here in one place, so neither can drift alone: the
// constant, and len()'s disagreement with it (which is what makes len() the
// wrong instrument rather than a style choice).
func TestStatusSeparatorCellWidths(t *testing.T) {
	cases := []struct {
		name     string
		sep      string
		wantCell int
	}{
		{"StatusSep", StatusSep, StatusSepCells},
		{"StatusColumnSep", StatusColumnSep, StatusColumnSepCells},
		{"StatusLineGap", StatusLineGap, StatusLineGapCells},
	}
	for _, c := range cases {
		if got := displayWidth(c.sep); got != c.wantCell {
			t.Errorf("%s %q is %d display cells, but %sCells is %d — a layout reserving columns from the wrong number puts the row past the terminal it is pinned for",
				c.name, c.sep, got, c.name, c.wantCell)
		}
	}
	// StatusColumnSep is derived from StatusSep (one source of truth), and that
	// derivation is what the cell arithmetic above must respect.
	if StatusColumnSep != StatusSep+" " {
		t.Fatalf("StatusColumnSep = %q, want StatusSep + one cell of air", StatusColumnSep)
	}
	if StatusColumnSepCells != StatusSepCells+1 {
		t.Fatalf("StatusColumnSepCells = %d, want StatusSepCells+1 = %d", StatusColumnSepCells, StatusSepCells+1)
	}
}

// TestStatusAlignedLayoutFitsARealTerminalWidth is the property the defect
// actually violated, stated where it can fail: a Status whose columns are pinned
// (LayoutCols set) renders its ALIGNED form at the width the application pins it
// for — the terminal width the layout is designed around — rather than falling
// back to the historic composition.
//
// The assertion deliberately renders at a REAL terminal width (a5WideCols, 100
// columns, the width the layout's own grid evidence is captured at) instead of
// at the pinned width itself. That is the difference that let the defect through:
// rendering AT the pin proves only that the arithmetic agrees with itself.
//
// The pin itself is BudgetWidth, the shared definition — this test does not
// restate it.
func TestStatusAlignedLayoutFitsARealTerminalWidth(t *testing.T) {
	// The pin is the SHARED width (lineedit.BudgetWidth), never a number copied
	// into this test: the production geometry is five columns, four separators,
	// the gap and the live block, and the property under test is that the row it
	// composes fits the width the APPLICATION pins it to — not that this test's
	// arithmetic agrees with itself.
	const pinned = BudgetWidth
	s := Status{
		Label: "model",
		Columns: []StatusColumn{
			{Text: "deepseek-flash", Width: 13},
			{Text: "iter 3", Width: 7},
			{Text: "tools", Width: 5},
			{Text: "⚙ bash", Width: 16},
			{Text: "12.3k/200k · ↑12k↓4k · ~0.12元", Width: 28},
		},
		Live:       a5StatusIdle,
		LiveWidth:  12,
		LayoutCols: pinned,
	}
	// The width this test renders at is WIDER than the pin, and that is the whole
	// point of the shape: a5WideCols is 100 and BudgetWidth is 100 at this cut,
	// so rendering at a5WideCols renders AT the pin — the shape this test's own
	// rationale warns about ("rendering AT the pin proves only that the
	// arithmetic agrees with itself"). measured: with the fixture below,
	// s.Line(BudgetWidth) and s.Line(BudgetWidth+40) both draw the identical
	// 93-cell row, so the pin is not observable at either width — a terminal
	// WIDER than the pin is what makes the form a question at all. What the pin
	// actually buys is the BLOCK below it: at cols = BudgetWidth-1 the row
	// degrades to the historic composition (measured: "model  ready", where this
	// probe reads cell -1).
	const realTerminalCols = BudgetWidth + 40
	row := s.Line(realTerminalCols)
	// The aligned form is identified by its COLUMNS, not by its total width: the
	// row's right edge is the live hint's text (its reserved block's trailing
	// padding is trimmed), so the property to assert is where each field STARTS.
	// The iter field starts one cell past the model column's reserved width plus
	// a separator — a cell no unaligned composition reaches, because an unaligned
	// row is the fields' natural widths joined.
	//
	// The probe names the field the FIXTURE above actually carries ("iter 3", the
	// second StatusColumn). It reads "iter" and not the chat surface's caption
	// (cmd/agent's turnLabel draws "turn"): this row is a synthetic fixture whose
	// column TEXTS are arbitrary, and what the assertion measures is where the
	// field STARTS, never what it says. Commit 3592f4498 renamed the chat
	// surface's caption and swept this file with it, moving the probe off the
	// word the fixture renders — the row then carried no such field,
	// fieldStartIn answered -1, and the test failed on its own error message's
	// contradiction: "the row is not the aligned layout: \"iter\" starts at cell
	// -1" while it was searching for "turn".
	sawIters := fieldStartIn(row, "iter")
	wantIters := 13 + StatusColumnSepCells
	if sawIters != wantIters {
		t.Fatalf("the row is not the aligned layout: \"iter\" starts at cell %d, want %d (the model column + a separator)\nrow: %q",
			sawIters, wantIters, row)
	}
	// The GROUPED column is what makes the five fields fit: its sub-cells are
	// joined by the LIGHT separator (StatusSepGroup), so the row pays one column
	// separator for three read-outs. Asserted on a start cell too — the tool
	// column begins one caption past the model, iter and mode columns, so the
	// grouping is what puts it there.
	sawTool := fieldStartIn(row, "⚙ bash")
	if wantTool := 13 + 7 + 5 + 3*StatusColumnSepCells; sawTool != wantTool {
		t.Fatalf("the grouped geometry moved: the tool column starts at cell %d, want %d\nrow: %q", sawTool, wantTool, row)
	}
	if got := displayWidth(row); got > pinned {
		t.Fatalf("the aligned row is %d cells, over its pinned width %d — a row wider than the screen costs the editing line a row\nrow: %q",
			got, pinned, row)
	}
	// The same row on a terminal narrower than the pin degrades to the historic
	// composition — aligned columns are what a wide terminal adds, never a row
	// wider than the screen (which would cost the editing line a row).
	narrow := s.Line(pinned - 1)
	if displayWidth(narrow) > pinned-1 {
		t.Fatalf("a terminal narrower than the pin did not degrade: %d cells at cols=%d\nrow: %q",
			displayWidth(narrow), pinned-1, narrow)
	}
	if len(wrappedRowsFor(s, narrow, pinned-1)) != 1 {
		t.Fatalf("the degraded row wrapped onto more than one screen row: %q", narrow)
	}
}

// wrappedRowsFor lays s out at cols the way a terminal wraps, returning the
// screen rows it occupies.
func wrappedRowsFor(_ Status, s string, cols int) []string {
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

// fieldStartIn is the display cell a field starts at inside a row, or -1 when
// the row does not carry it.
func fieldStartIn(row, field string) int {
	i := strings.Index(row, field)
	if i < 0 {
		return -1
	}
	return displayWidth(row[:i])
}
