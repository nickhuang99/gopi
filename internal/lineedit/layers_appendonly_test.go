package lineedit

// layers_appendonly_test.go — the APPEND-ONLY proof for the four-layer format
// (task 01a0e834's acceptance): a second turn adds rows and rewrites nothing.
//
// WHY THIS IS A SEPARATE CLAIM. The task's constraint is not "the layers exist"
// but "the layers are added as rows" — no cursor placement onto a committed row,
// no scroll region, no collapse. A unit test that only reads the rows of ONE
// turn cannot tell an appended row from a repainted one. This file therefore
// renders TWO turns and compares the FIRST turn's rows — text AND attrs, cell by
// cell — before and after the second turn painted. A row that was quietly
// re-drawn to make room for the new turn fails it.
//
// The second half is the accounting: the rows the turn ADDS to the band are
// exactly the rows it SHOWS (its chrome and its four layers, blank separators
// included). That is what makes "append-only" a measured cost rather than a
// slogan: a layer that cost the band a hidden row would show up as a delta
// larger than the turn's own visible height.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sgr"
)

// gridSnapshot is the whole screen read as cells: each row's text and its
// per-cell styles. It is the unit the append-only claim is made of — the text
// alone would let a row keep its characters while its attrs are rewritten under
// it (which is exactly the "stale re-paint" shape, and it is invisible to a
// string comparison).
type gridSnapshot struct {
	text   []string
	styles [][]sgr.Style
}

func snapshotGrid(h *a5Harness) gridSnapshot {
	rows := h.screen.Rows
	s := gridSnapshot{text: make([]string, rows), styles: make([][]sgr.Style, rows)}
	for r := 0; r < rows; r++ {
		t, st := h.screen.StyledLine(r)
		s.text[r] = strings.TrimRight(t, " ")
		s.styles[r] = st
	}
	return s
}

// styleEqual compares two rows' per-cell styles over the cells the text covers:
// trailing cells the row never wrote are the screen's background, and a row that
// shrank legitimately leaves them.
func styleEqual(a, b []sgr.Style, cells int) bool {
	n := cells
	if len(a) < n {
		n = len(a)
	}
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// renderOneLayerTurn paints one turn carrying all four layers and returns the
// row its header landed on plus the renderer's committed count.
func renderOneLayerTurn(t *testing.T, h *a5Harness, index int, prompt, answer string) (header, committed int) {
	t.Helper()
	h.answer.BeginTurn(TurnInfo{Index: index, Prompt: prompt})
	h.answer.BeginThinking()
	h.answer.Delta(layerProbeThinking + " " + prompt + "\n")
	h.answer.EndThinking()
	h.answer.Delta(answer + "\n")
	h.answer.Delta(FenceMarker + "\n")
	h.answer.Delta(layerProbeCode + "\n")
	h.answer.Delta(FenceMarker + "\n")
	h.answer.Note(layerProbeTool, HermesDarkPalette.ToolCall)
	h.answer.drainNotes()
	h.answer.Finish()
	h.answer.EndTurn()
	h.replay()
	return rowOf(bandLines(h, h.rowsTo(h.region.Plan().BandTop, h.region.Plan().BandBottom)),
		turnHeaderText(index, prompt)), h.answer.Committed()
}

// TestLayers_SecondTurnRewritesNoRowOfTheFirst is the append-only proof at CELL
// level: after turn 2 paints, every row turn 1 painted shows the same text and
// the same attrs — and turn 2's own rows are BELOW them.
func TestLayers_SecondTurnRewritesNoRowOfTheFirst(t *testing.T) {
	const rows, cols = turnHarnessRows, turnHarnessCols
	h := newA5HarnessSized(t, nil, nil, rows, cols)
	h.answer.SetCols(cols)
	h.answer.SetPalette(HermesDarkPalette)

	if row, _ := renderOneLayerTurn(t, h, 1, layerProbePrompt, layerProbeAnswer); row < 0 {
		t.Fatalf("turn 1's header was not found\n%s", h.screen.Contents())
	}
	before := snapshotGrid(h)
	committedBefore := h.answer.Committed()

	head2, committedAfter := renderOneLayerTurn(t, h, 2, layerProbePrompt+" two", layerProbeAnswer+" two")
	if head2 < 0 {
		t.Fatalf("turn 2's header was not found\n%s", h.screen.Contents())
	}
	after := snapshotGrid(h)

	// (a) EVERY row that held text before still holds the SAME text and attrs.
	// Rows ABOVE turn 2's header are the ones the claim is about: rows below it
	// are turn 2's own, which turn 1 never painted.
	for r := 0; r < len(before.text); r++ {
		if before.text[r] == "" {
			continue
		}
		if r >= head2 {
			continue
		}
		if before.text[r] != after.text[r] {
			t.Errorf("grid row %d was rewritten by turn 2:\n before=%q\n after =%q", r, before.text[r], after.text[r])
			continue
		}
		if !styleEqual(before.styles[r], after.styles[r], displayWidth(before.text[r])) {
			t.Errorf("grid row %d kept its text %q but its ATTRS changed — the row was re-painted, not left alone",
				r, before.text[r])
		}
	}
	// (b) …and the rows the first turn painted are still the same COUNT: nothing
	// of turn 1 was consumed to make room.
	kept := 0
	for r := 0; r < len(before.text); r++ {
		if before.text[r] != "" {
			kept++
		}
	}
	still := 0
	for r := 0; r < len(before.text); r++ {
		if r < head2 && after.text[r] != "" {
			still++
		}
	}
	if still != kept {
		t.Errorf("turn 1 painted %d non-blank rows and %d survive unmoved above turn 2's header %d — the new turn consumed the old one's rows",
			kept, still, head2)
	}

	// (c) THE ACCOUNTING: the rows the turn added equal the rows it shows. The
	// delta is the band's own count of committed rows; the visible height is
	// measured off the grid between the two headers.
	delta := committedAfter - committedBefore
	// The turn's visible span on the grid: its header through its closing rule,
	// BLANK ROWS INCLUDED — the separator the reasoning block costs is a row the
	// reader sees, and counting only non-blank rows would under-count the turn
	// by exactly the separators it drew.
	visible := 0
	for r := head2; r < len(after.text); r++ {
		visible++
		if strings.HasPrefix(after.text[r], strings.Repeat(TurnRuleGlyph, cols)) {
			break // the closing rule is the turn's last row
		}
	}
	if delta != visible {
		t.Errorf("turn 2 committed %d rows but shows %d — an appended row the reader cannot see (or a visible row that cost no commit)\n%s",
			delta, visible, h.screen.Contents())
	}
	if delta <= 0 {
		t.Fatalf("turn 2 committed %d rows, want a positive count — it painted rows", delta)
	}
	t.Logf("append-only: turn 2 committed %d rows and shows %d; turn 1's %d non-blank rows are unmoved and byte-identical",
		delta, visible, kept)
}

// TestLayers_LayerRowsAreTheOnlyRowsTheBandGrowsBy bounds the LAYER cost itself:
// the request row is exactly ONE band row (the operator's line is short here),
// so the layer's own price is measurable and does not depend on the body's
// wrapping.
func TestLayers_LayerRowsAreTheOnlyRowsTheBandGrowsBy(t *testing.T) {
	const cols = turnHarnessCols
	h := newA5HarnessSized(t, nil, nil, turnHarnessRows, cols)
	h.answer.SetCols(cols)

	// The header + the request row.
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: layerProbePrompt})
	turnRows := h.answer.Committed()
	if turnRows != 2 {
		t.Fatalf("BeginTurn committed %d rows, want 2 (the header and the request row)", turnRows)
	}
	h.answer.Delta(layerProbeAnswer + "\n")
	h.answer.Finish()
	afterBody := h.answer.Committed()
	if got := afterBody - turnRows; got != 1 {
		t.Errorf("the answer's own row cost %d commits, want 1 — the request layer must be the ONLY extra row BeginTurn added",
			got)
	}
	rows := requestRowRows(layerProbePrompt, cols)
	if len(rows) != 1 {
		t.Fatalf("the probe's request laid out as %d rows, want 1", len(rows))
	}
	if !strings.HasPrefix(rows[0], layerMarker(LayerRequest)) {
		t.Errorf("the request row %q does not open with the layer's marker", rows[0])
	}
}
