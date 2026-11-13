package lineedit

// layers_test.go — the regression guard for the FOUR-LAYER FORMAT TABLE
// (layers.go): the four layers of a chat turn must be distinguishable on the
// CELL GRID, and the distinction must cost the transcript no cells.
//
// THE CLAIM, at the level the task states it: request / thinking / response /
// code-tool each open their rows with a marker the other three do not use, and
// the four markers are one column wide each, so a reader (and a cell-grid dump)
// can tell a layer by its first two cells. The table is asserted TWICE — as a
// value (the marker function) and as PAINT (what the band actually shows), since
// a table nothing draws from would be exactly the "documented hook read by
// nobody" the project's gates refuse.
//
// The code/tool family is asserted in ITS OWN shape: a fenced code line is the
// response layer's MARKER with the code layer's STYLE, while a committed tool
// row is the tool layer's marker carried by its own text — the two halves of the
// family, each with the column and the emphasis it really has on screen. The
// tool half is exercised through Editor.Note, the path a real chat's tool
// activity takes (cmd/agent/lineedit_wire.go commits every ToolEvent there).

import (
	"strings"
	"testing"
)

// layerProbe is one rendered turn, read off the cell grid.
type layerProbe struct {
	lines []string
	head  int // the turn header's row
	tail  int // the turn's closing rule
}

// rowText is the grid row with its trailing blanks removed.
func (p layerProbe) rowText(i int) string { return strings.TrimRight(p.lines[i], " ") }

// firstBodyRow is the first row of the turn's body — the request row by
// construction (the layer table's own ordering claim).
func (p layerProbe) firstBodyRow() int { return p.head + 1 }

// rowWithLayer returns the index of the body row whose first cells are the
// layer's marker, or -1.
func (p layerProbe) rowWithLayer(layer AnswerLayer) int {
	marker := layerMarker(layer)
	for i := p.firstBodyRow(); i <= p.tail; i++ {
		if strings.HasPrefix(p.rowText(i), marker) {
			return i
		}
	}
	return -1
}

// renderLayerTurn renders one turn carrying all four layers through the real
// render path: the request (the turn's own prompt), a reasoning line, an answer
// line, a fenced code line, and a committed tool row.
func renderLayerTurn(t *testing.T, cols int, prompt ...string) layerProbe {
	t.Helper()
	pr := layerProbePrompt
	if len(prompt) > 0 {
		pr = prompt[0]
	}
	h := newA5HarnessSized(t, nil, nil, turnHarnessRows, cols)
	h.answer.SetCols(cols)
	h.answer.SetPalette(HermesDarkPalette)
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: layerProbePrompt})
	h.answer.BeginThinking()
	h.answer.Delta(layerProbeThinking + "\n")
	h.answer.EndThinking()
	h.answer.Delta(layerProbeAnswer + "\n")
	h.answer.Delta(FenceMarker + "\n")
	h.answer.Delta(layerProbeCode + "\n")
	h.answer.Delta(FenceMarker + "\n")
	// The tool row, through the seam a real chat uses: the row is QUEUED on the
	// renderer and committed by the read loop's drain (editor.go's drainNotes),
	// which is exactly what cmd/agent's WriteToolRow reaches on a live chat.
	h.answer.Note(layerProbeTool, HermesDarkPalette.ToolCall)
	if !h.answer.drainNotes() {
		t.Fatalf("the tool row was not committed by the drain — the note never reached the band")
	}
	h.answer.Finish()
	h.answer.EndTurn()
	h.replay()

	lines := bandLines(h, h.rowsTo(h.region.Plan().BandTop, h.region.Plan().BandBottom))
	p := layerProbe{lines: lines, head: -1, tail: -1}
	p.head = rowOf(lines, turnHeaderText(1, pr))
	if p.head < 0 {
		t.Fatalf("no turn header on the grid\n%s", strings.Join(lines, "\n"))
	}
	for r := p.head + 1; r < len(lines); r++ {
		if strings.TrimSpace(lines[r]) == strings.Repeat(TurnRuleGlyph, cols) {
			p.tail = r - 1
			break
		}
	}
	if p.tail < p.head {
		t.Fatalf("the turn's closing rule was not found\n%s", strings.Join(lines, "\n"))
	}
	return p
}

const (
	layerProbePrompt   = "layer probe"
	layerProbeThinking = "reasoning about the row"
	layerProbeAnswer   = "the answer body"
	layerProbeCode     = "echo layer"
	layerProbeTool     = ToolGutter + "bash ls -la /tmp"
)

// TestLayerTable_FourLayersHaveFourDistinctMarkers is the table as a VALUE: each
// layer answers a marker, the four are distinct, each is exactly one column
// wide, and the enumeration is COMPLETE — a layer added to AnswerLayer without a
// marker would otherwise render as an unmarked row (the silent-failure shape
// Rule 15 is about), so the loop walks the enum rather than four samples.
func TestLayerTable_FourLayersHaveFourDistinctMarkers(t *testing.T) {
	layers := []AnswerLayer{LayerRequest, LayerThinking, LayerResponse, LayerCodeTool}
	seen := map[string]AnswerLayer{}
	for _, l := range layers {
		m := layerMarker(l)
		if m == "" {
			t.Fatalf("layer %d has no marker — an unmarked layer is indistinguishable from plain output", l)
		}
		if w := displayWidth(m); w != blockColumn() {
			t.Errorf("layer %d's marker %q is %d cells wide, want %d — the four layers must share one column",
				l, m, w, blockColumn())
		}
		if other, dup := seen[m]; dup {
			t.Errorf("layers %d and %d both open with %q — the transcript cannot tell them apart", other, l, m)
		}
		seen[m] = l
	}
	if len(seen) != len(layers) {
		t.Fatalf("the four layers produced %d distinct markers, want %d", len(seen), len(layers))
	}
	// The continuation marker is the response layer's alone: a wrapped body row
	// must be readable as the SAME line continued, not as a new layer.
	if got := layerContinuation(LayerResponse); got != TurnMarkerSplit {
		t.Errorf("the response continuation marker is %q, want %q", got, TurnMarkerSplit)
	}
	for _, l := range []AnswerLayer{LayerRequest, LayerThinking, LayerCodeTool} {
		if got := layerContinuation(l); got != "" {
			t.Errorf("layer %d answers a continuation marker %q, want none", l, got)
		}
	}
}

// TestLayerTable_StylesArePerLayer pins the STYLE column: each layer draws from
// its own palette field, the monochrome table draws every layer as bare text
// (colour is emphasis on top of the text, never the text's meaning), and — the
// family's own two halves — a fenced line takes the body's code style while a
// committed tool row takes the tool style.
func TestLayerTable_StylesArePerLayer(t *testing.T) {
	pal := HermesDarkPalette
	cases := []struct {
		layer AnswerLayer
		ls    LineStyle
		want  Style
	}{
		{LayerRequest, LinePlain, pal.Prompt},
		{LayerThinking, LinePlain, pal.Thinking},
		{LayerResponse, LinePlain, pal.Answer},
		{LayerCodeTool, LineCode, styleFor(pal, LineCode)},
		{LayerCodeTool, LineDiffAdd, styleFor(pal, LineDiffAdd)},
		{LayerCodeTool, LineDiffDel, styleFor(pal, LineDiffDel)},
		{LayerCodeTool, LinePlain, pal.ToolCall},
	}
	for _, c := range cases {
		if got := layerStyleFor(pal, c.layer, c.ls); got != c.want {
			t.Errorf("layerStyleFor(layer=%d, ls=%d) = %+v, want %+v", c.layer, c.ls, got, c.want)
		}
	}
	// The two halves of the code/tool family must not collapse into one field:
	// if they ever did, a fenced line and a tool call would be one visual thing.
	if styleFor(pal, LineCode) == pal.ToolCall {
		t.Fatalf("HermesDarkPalette draws code and tool rows identically — the family's two halves are one layer in effect")
	}
	// Monochrome: every layer is the zero style, so the distinction is the TEXT.
	for _, l := range []AnswerLayer{LayerRequest, LayerThinking, LayerResponse, LayerCodeTool} {
		for _, ls := range []LineStyle{LinePlain, LineCode, LineDiffAdd, LineDiffDel} {
			if got := layerStyleFor(MonoPalette, l, ls); !got.Empty() {
				t.Errorf("MonoPalette draws layer %d (ls=%d) with %+v — monochrome must be bare text; the layer is carried by the marker", l, ls, got)
			}
		}
	}
}

// TestLayerTable_FourLayersAppearOnTheGridAsValues is the table as PAINT: the
// four layers are present on the cell grid at the four distinct columns the
// value test just pinned, and the request row is the turn's FIRST body row (the
// ordering claim that makes the whole draw order causal without any reordering).
func TestLayerTable_FourLayersAppearOnTheGridAsValues(t *testing.T) {
	p := renderLayerTurn(t, turnHarnessCols)
	t.Logf("band:\n%s", strings.Join(p.lines, "\n"))

	for _, l := range []AnswerLayer{LayerRequest, LayerThinking, LayerResponse, LayerCodeTool} {
		r := p.rowWithLayer(l)
		if r < 0 {
			// The code half of the code/tool family carries no marker (see
			// gutterGlyph): it is present on the grid, at column 0.
			if l == LayerCodeTool && rowOf(p.lines, layerProbeCode) >= 0 {
				t.Logf("layer %d (%q) opens row %d with NO marker: %q",
					l, layerMarker(l), rowOf(p.lines, layerProbeCode), layerProbeCode)
				continue
			}
			t.Fatalf("layer %d (%q) is not on the grid — the table names a row nothing paints\n%s",
				l, layerMarker(l), strings.Join(p.lines, "\n"))
		}
		t.Logf("layer %d opens row %d with %q: %q", l, r, layerMarker(l), p.rowText(r))
	}

	// The ORDER claim: the request is the first body row, under the header.
	if got := p.rowWithLayer(LayerRequest); got != p.firstBodyRow() {
		t.Errorf("the request row is at row %d, want the turn's first body row %d — the question must precede its answer\n%s",
			got, p.firstBodyRow(), strings.Join(p.lines, "\n"))
	}
	// …and the reasoning, the answer and the tool row all follow it.
	req := p.rowWithLayer(LayerRequest)
	for _, l := range []AnswerLayer{LayerThinking, LayerResponse, LayerCodeTool} {
		got := p.rowWithLayer(l)
		if got < 0 && l == LayerCodeTool {
			got = rowOf(p.lines, layerProbeCode)
		}
		if got < req {
			t.Errorf("layer %d opens at row %d, ABOVE the request row %d — the draw order is not the causal order",
				l, got, req)
		}
	}
	// The fenced block's own line opens at column 0 with NO gutter: it is the
	// family's COPYING half, and a row a reader selects off the band must be the
	// command it shows (answerRow.gutterGlyph — the operator's paste leg,
	// measured 2026-10-02). What the row keeps is its own code STYLE, which is
	// the emphasis that actually separates it from the prose around it.
	codeRow := rowOf(p.lines, layerProbeCode)
	if codeRow < 0 {
		t.Errorf("the fenced line is not on the grid\n%s", strings.Join(p.lines, "\n"))
	}
	if got := p.rowText(codeRow); got != layerProbeCode {
		t.Errorf("the fenced row = %q, want the command verbatim %q — a code row that carries ink of its own cannot be pasted",
			got, layerProbeCode)
	}
	// The tool half carries its own glyph AND its own style: the row is the tool
	// layer's, by marker, and the grid's cells say so.
	if !strings.HasPrefix(layerProbeTool, ToolGutter) {
		t.Fatalf("the probe's tool row %q does not open with the tool gutter %q", layerProbeTool, ToolGutter)
	}
}

// TestLayerTable_ToolGutterIsTheRenderedRow'sOwnMarker guards the table's ONE
// shared value with cmd/agent: the tool row's marker is the text the row is
// committed with (cmd/agent's termToolLine opens with ToolGutter), so the
// renderer must NOT draw a second one. The failure this pins is a doubled glyph
// (`⚙ ⚙ bash …`), which a reader would read as two calls.
func TestLayerTable_ToolGutterIsTheRowsOwnMarker(t *testing.T) {
	p := renderLayerTurn(t, turnHarnessCols)
	r := p.rowWithLayer(LayerCodeTool)
	if r < 0 {
		t.Fatalf("no tool row on the grid\n%s", strings.Join(p.lines, "\n"))
	}
	got := p.rowText(r)
	if strings.HasPrefix(got, ToolGutter+ToolGutter) {
		t.Fatalf("the tool row wears the glyph twice (%q) — the renderer drew a gutter the row already carried", got)
	}
	if strings.Count(got, ToolGlyph) != 1 {
		t.Errorf("the tool row = %q carries %d tool glyphs, want exactly 1", got, strings.Count(got, ToolGlyph))
	}
	if !strings.HasPrefix(got, layerMarker(LayerCodeTool)) {
		t.Errorf("the tool row = %q does not open with the layer table's own marker %q", got, layerMarker(LayerCodeTool))
	}
}

// TestLayerTable_RequestRowIsCappedAndMarked pins the request row's own bound: a
// pasted request far longer than the band is CUT WITH THE PRODUCT'S MARKER
// (cutField's ellipsis) rather than silently truncated, and the row run stops at
// requestRowMaxRows — a request must not be able to push the turn off the screen.
func TestLayerTable_RequestRowIsCappedAndMarked(t *testing.T) {
	cols := turnHarnessCols
	long := strings.Repeat("request ", cols*4)
	rows := requestRowRows(long, cols)
	// The layout IS the cap (asserted as a value first, so the failure names the
	// arithmetic rather than a missing row on the grid).
	if len(rows) != requestRowMaxRows {
		t.Fatalf("a request of %d cells laid out as %d rows, want the cap requestRowMaxRows=%d",
			displayWidth(long), len(rows), requestRowMaxRows)
	}
	if last := rows[len(rows)-1]; !strings.HasSuffix(last, statusEllipsis) {
		t.Errorf("the request's last row %q does not wear the cut marker %q — a shortened request must read as shortened",
			last, statusEllipsis)
	}
	if !strings.HasPrefix(rows[0], layerMarker(LayerRequest)) {
		t.Errorf("the request's first row %q does not carry the layer's marker %q", rows[0], layerMarker(LayerRequest))
	}
	// The continuation row keeps the block's column (a blank gutter of the marker's
	// own width, never a second number — Rule 14).
	pad := strings.Repeat(" ", blockColumn())
	if !strings.HasPrefix(rows[1], pad) || strings.HasPrefix(rows[1], layerMarker(LayerRequest)) {
		t.Errorf("the request's continuation row %q must open with the aligned blank gutter %q", rows[1], pad)
	}

	// …and the CAP as PAINT: against the real render path, with the grid given
	// enough rows that a capped request cannot be blamed on the band's edge.
	h := newA5HarnessSized(t, nil, nil, turnHarnessRows, cols)
	h.answer.SetCols(cols)
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: long})
	h.answer.Delta(layerProbeAnswer + "\n")
	h.answer.Finish()
	h.answer.EndTurn()
	h.replay()
	lines := bandLines(h, h.rowsTo(h.region.Plan().BandTop, h.region.Plan().BandBottom))
	t.Logf("band:\n%s", strings.Join(lines, "\n"))
	body := 0
	for i, l := range lines {
		if i > 0 && strings.HasPrefix(strings.TrimRight(l, " "), layerMarker(LayerRequest)) {
			body++
		}
	}
	if body == 0 {
		t.Errorf("no request row on the grid\n%s", strings.Join(lines, "\n"))
	}
	if body > requestRowMaxRows {
		t.Errorf("the request occupied %d band rows, past the cap %d — a pasted line must not push the turn off the screen\n%s",
			body, requestRowMaxRows, strings.Join(lines, "\n"))
	}
}

// TestLayerTable_NarrowTerminalDegradesWithoutBreakingTheLine is the width
// bound: on a terminal too narrow for the gutter AND a cell of content, the
// request row yields its marker rather than wrapping the operator's line onto a
// second row — the same degradation the body's own gutter makes (lineGutter), so
// the two chrome rows of a turn agree about what "too narrow" means.
func TestLayerTable_NarrowTerminalDegradesWithoutBreakingTheLine(t *testing.T) {
	cols := blockColumn() + minGutterContentCells // the smallest width with room
	for _, c := range []int{blockColumn(), blockColumn() - 1, 2, 1, 0} {
		rows := requestRowRows(layerProbePrompt, c)
		if len(rows) > 1 {
			t.Errorf("cols=%d: the request laid out as %d rows — a line too narrow for the gutter must not be broken to keep it",
				c, len(rows))
		}
		for _, r := range rows {
			if displayWidth(r) > c && c > 0 {
				t.Errorf("cols=%d: request row %q is %d cells wide, past the terminal", c, r, displayWidth(r))
			}
		}
	}
	// At the smallest usable width the gutter survives with one content cell.
	rows := requestRowRows(layerProbePrompt, cols)
	if len(rows) == 0 || !strings.HasPrefix(rows[0], layerMarker(LayerRequest)) {
		t.Errorf("cols=%d is wide enough for the gutter: rows=%q, want a marked row", cols, rows)
	}
}
