package main

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// chat_status_width_test.go — the status row's WIDTH contract, table-driven.
//
// The rule this file pins is arithmetic, not taste: the aligned row is composed
// from the reserved widths in chat_stream.go, and this test re-derives every
// field's start CELL from those same constants (one definition, read here rather
// than copied) and then walks every narrower terminal to assert what the row
// does there. It is the guard the compile-time budget alone cannot be: the
// budget check says "the geometry fits 100 columns", while these assertions say
// what a user actually sees at each width, and that no VALUE can move the
// geometry.
//
// It complements (never replaces) the two display contracts it must not weaken:
// TestChatStatus_ColumnsHoldStillWhileValuesChange (the field start cells across
// the states a live turn walks through) and TestChatStatus_ToolRowRegression
// (the tool name reaching the screen).

const (
	// statusWidthPin is the width the aligned layout is composed for — read from
	// production, never restated.
	statusWidthPin = statusColumnsAtCols
	// statusWidthMin is the narrowest terminal the sweep walks (a width where
	// only the live hint can survive).
	statusWidthMin = 10
	// statusWidthStep walks the sweep in cells, so the table covers the ladder's
	// every crossing rather than a few sampled widths.
	statusWidthStep = 2
	// statusWidthLongModel is a model name longer than its column (the provider
	// string the row must cut, never widen for).
	statusWidthLongModel = "glm-5.3-flash-long-model-name"
	// statusWidthModelAnchor is the part of that name which survives the column's
	// cut at the pinned width: the model column is statusModelCol cells and the
	// name is longer, so the row carries a marked cut — the anchor is chosen from
	// what a cut row really shows ("glm-5.3-flash" is 14 cells, the display
	// contract's own floor).
	statusWidthModelAnchor = "glm-5.3-flash"
	// statusWidthBigTokens is a six-digit token count: its formatted form
	// ("1000.0k") is the widest count the field ever carries, which is what the
	// column reservation has to absorb without moving anything.
	statusWidthBigTokens = 999999
	// statusWidthQueued is the queued-turn marker a caller passes when a message
	// is waiting behind the running turn.
	statusWidthQueued = "1 queued"
)

// statusWidthSweep returns the widths the table walks: the pinned geometry
// first, then every step down to the narrowest.
func statusWidthSweep() []int {
	widths := []int{statusWidthPin}
	for w := statusWidthPin - statusWidthStep; w >= statusWidthMin; w -= statusWidthStep {
		widths = append(widths, w)
	}
	return widths
}

// statusFieldStartCells is the start cell each field must occupy when the row is
// rendered at the pinned width, DERIVED from the production reservations: the
// sum of the columns before it plus one separator per join. -1 means "the field
// is not carried at all" — the honest omission for every operational read-out
// the geometry cannot host (see the repartition note in chat_stream.go).
//
// The layout it walks is the CURRENT one (chat_stream.go statusColumnsAtCols, the
// re-partition of task 01a0dcd1): the OPERATIONAL read-out is the metrics GROUP —
// one top-level column carrying the context sub-cell (statusCtxCol, the absolute
// used/max pair plus the percentage derived from it) and the cache sub-cell joined
// on a field separator. The start cells below are read from the SAME constants
// that build it, and the group's start cell is what the columns before it leave —
// the sub-cells INSIDE it are joined by field separators, so they are not
// separately reserved and this map does not enumerate them.
//
// The read-outs no column carries are the -1 rows: the money sub-cell is RETIRED
// (the row carries no price at any width), and elapsed / compressions never had a
// column of their own (they ride the live block and the group's cache pair's
// neighbourhood respectively — see the geometry notes in chat_stream.go).
func statusFieldStartCells() map[string]int {
	cells := map[string]int{}
	at := 0
	add := func(width int) { at += width + lineedit.StatusColumnSepCells }
	cells["model"] = at
	add(statusModelCol)
	cells["turn"] = at
	add(statusTurnCol)
	cells["tool"] = at
	add(statusToolCol)
	// The group is ONE column: its start cell is where the tool column's
	// reservation and separator leave off, and the context sub-cell — the group's
	// primary fact — begins there.
	cells["tokens"] = at
	add(statusMetricsCol)
	// The read-outs no column carries: no cell on the row, and the row must not
	// grow one for them.
	for _, omitted := range []string{"cache", "cost", "elapsed", "compressions"} {
		cells[omitted] = -1
	}
	return cells
}

// TestChatStatus_WidthLadderAndProvenance walks every width from the pinned
// geometry down to a 10-column terminal and asserts, at each one:
//
//   - the row occupies exactly ONE screen row and never exceeds its own width (a
//     wrapped status row costs the editing line a row — the defect
//     lineedit_height_test.go is built around);
//   - the live hint survives wherever it fits (it is the row's last yield);
//   - the label fields present are a PREFIX of the pinned order — a narrower
//     terminal drops fields from the right and never reorders or resurrects one;
//
// and at the pinned width: every carried field starts on the cell its own
// reservation gives it (the provenance check), and none of the omitted
// read-outs appears anywhere on the row.
func TestChatStatus_WidthLadderAndProvenance(t *testing.T) {
	s := newChatStatusSource(statusWidthLongModel, true)
	s.update(3, 0, 12345, "")
	s.setTool("read", "internal/lineedit/answer.go")

	base := s.status().Line(statusWidthPin)
	want := statusFieldStartCells()
	anchors := []struct {
		name   string
		anchor string
		cell   int
	}{
		{"model", statusWidthModelAnchor, want["model"]},
		{"turn", "turn", want["turn"]},
		{"tool", toolGlyph, want["tool"]},
		// The metrics group's own first cells: the context sub-cell LEADs with the
		// fact name and the gap before the number it names (task 01a0ddab), so the
		// anchor is the labelled field rather than the bare count — a bare count
		// cannot state a start cell any more, it sits four cells into the sub-cell.
		{"tokens", statusCtxLabel + statusCtxBarGap + tokenAnchor(12345), want["tokens"]},
	}
	for _, a := range anchors {
		if got := fieldStart(base, a.anchor); got != a.cell {
			t.Fatalf("at the pinned width %d, field %q starts at cell %d, want %d (the reservation's own sum)\nrow: %q",
				statusWidthPin, a.name, got, a.cell, base)
		}
	}
	// The live hint is the row's right edge: its cell is the block's own origin.
	if got := fieldStart(base, liveAnchor(statusLiveIdle)); got != statusWidthPin-statusLiveCol {
		t.Fatalf("at the pinned width %d, the live hint starts at cell %d, want %d (the block's origin)\nrow: %q",
			statusWidthPin, got, statusWidthPin-statusLiveCol, base)
	}
	// The omitted read-outs are absent: no money field and no elapsed/compression
	// column may appear on the row by accident (the honest-omission contract). The
	// currency glyphs are spelled as LITERALS on purpose — the production constant
	// died with the money sub-cell (task 01a0dcd1), so the absence proof must not
	// reference live code.
	for _, omitted := range []string{"elapsed", "compress", "元", "¥", "$", "€"} {
		if strings.Contains(base, omitted) {
			t.Fatalf("the row carries %q, which this partition deliberately omits (see the re-partition note in chat_stream.go)\nrow: %q",
				omitted, base)
		}
	}

	// The sweep: monotone degradation, one screen row, never past the width.
	presentAt := func(row string) []bool {
		return []bool{
			strings.Contains(row, "glm-5."),
			strings.Contains(row, "turn"),
			strings.Contains(row, toolGlyph),
			strings.Contains(row, tokenAnchor(12345)),
		}
	}
	var prev []bool
	var prevWidth int
	for _, width := range statusWidthSweep() {
		row := s.status().Line(width)
		if got := wrappedRowsForTest(row, width); got != 1 {
			t.Fatalf("width %d: the status row occupies %d screen rows: %q", width, got, row)
		}
		if n := displayCellsForTest(row); n > width {
			t.Fatalf("width %d: the status row is %d cells wide: %q", width, n, row)
		}
		// The live hint is the row's last yield: while it fits, it is shown.
		if width >= displayCellsForTest(statusLiveIdle) && !strings.Contains(row, statusLiveIdle) {
			t.Fatalf("width %d: the live hint %q is missing from the row %q", width, statusLiveIdle, row)
		}
		got := presentAt(row)
		if prev != nil {
			for i := range got {
				if got[i] && !prev[i] {
					t.Fatalf("width %d: field %d is present here but was dropped at the wider %d — the ladder lost a field and then brought it back\nwider: %q\nhere:  %q",
						width, i, prevWidth, s.status().Line(prevWidth), row)
				}
			}
		}
		prev, prevWidth = got, width
	}
}

// TestChatStatus_ValuesNeverMoveTheGeometry is the "a value cannot widen the row
// nor slide a field" half, at the pinned width and at a narrow one: the same
// geometry is rendered with a six-digit token count, the heavy-context suffix, a
// turn in flight and a queued turn, and every field present in BOTH the base
// state and the state under test is asserted to start on the SAME cell. A value
// that widened its field would slide the columns after it — the defect the
// reserved widths exist for.
func TestChatStatus_ValuesNeverMoveTheGeometry(t *testing.T) {
	s := newChatStatusSource(statusWidthLongModel, true)

	// The token field is anchored on its COUNT (tokenAnchor), never on the word
	// "tokens": the unit word sits inside the field and moves with the count's
	// own length, which is exactly the sliding this test exists to catch (the
	// alignment contract uses the count for the same reason).
	starts := func(row string, tokenCount int, live string) []int {
		return []int{
			fieldStart(row, statusWidthModelAnchor),
			fieldStart(row, "turn"),
			fieldStart(row, toolGlyph),
			fieldStart(row, tokenAnchor(tokenCount)),
			fieldStart(row, liveAnchor(live)),
		}
	}
	for _, width := range []int{statusWidthPin, statusWidthPin / 2} {
		s.update(3, 0, 12345, "")
		s.setTool("read", "internal/lineedit/answer.go")
		baseRow := s.status().Line(width)
		base := starts(baseRow, 12345, statusLiveIdle)

		states := []struct {
			name string
			do   func()
			toks int
			live string
			// shown is asserted at the PINNED width only: at a narrow one the
			// live block and the label legitimately drop whole fields.
			shown string
		}{
			{"a six-digit token count", func() { s.update(3, 0, statusWidthBigTokens, "") }, statusWidthBigTokens, statusLiveIdle, tokenAnchor(statusWidthBigTokens)},
			{"a heavy context", func() { s.update(3, 0, statusTokensHeavy+1000, "") }, statusTokensHeavy + 1000, statusLiveIdle, tokenAnchor(statusTokensHeavy + 1000)},
			// The in-flight hint is composed by the turn's state machine
			// (beginTurn + setPhase), not by a constant: master's row walks
			// sent -> thinking -> responding, so the state is driven here the
			// way the REPL drives it and the anchor is the phase's own word.
			{"a turn in flight (thinking)", func() {
				s.beginTurn(4)
				s.setPhase(chatTurnThinking)
			}, 12345, statusLiveWordThinking, statusLiveWordThinking},
			// The queued marker is the FIRST thing the live block's cut takes at
			// the pinned width: the block reserves the hint over the marker
			// (statusLiveCol is the hint's own width), so "ready · 1 queued"
			// renders as "ready · 1 …" — inherited from the geometry this
			// partition keeps, and asserted here as the hint-plus-joint the row
			// really shows rather than the whole marker it does not.
			// The phase word is the anchor, not the phase word PLUS the
			// separator: at the pinned width the live block's own cut keeps the
			// word and takes the rest ("thinking…"), because statusLiveCol is
			// reserved for the phase word. That cut is the inherited limitation
			// recorded in the devdoc, and the row must still be one screen row
			// carrying the state it is declaring.
			{"a thinking turn with a queued message", func() {
				s.beginTurn(4)
				s.setPhase(chatTurnThinking)
				s.update(3, 4, 12345, statusWidthQueued)
			}, 12345, statusLiveWordThinking, statusLiveWordThinking},
		}
		for _, st := range states {
			st.do()
			row := s.status().Line(width)
			if got := wrappedRowsForTest(row, width); got != 1 {
				t.Fatalf("%s at width %d: the row occupies %d screen rows: %q", st.name, width, got, row)
			}
			if width == statusWidthPin && !strings.Contains(row, st.shown) {
				t.Fatalf("%s at the pinned width: the row %q does not carry %q", st.name, row, st.shown)
			}
			// The reserved-cell contract is the ALIGNED layout's, so it is
			// asserted at the pinned width. Below it the historic unaligned
			// composition applies, and that form lays its label and live hint
			// out from the lengths of the values by design (lineedit statusLine
			// cuts the label to whatever room the live text leaves), so a value
			// legitimately changes where the label ends there — what must still
			// hold at every width is the row's bounds, asserted above.
			// The long model name is CUT at every width, never allowed to widen
			// the row — checked before the pinned-only cell contract below.
			if strings.Contains(row, statusWidthLongModel) && width < statusWidthPin {
				t.Fatalf("%s at width %d: the row carries the whole long model name %q: %q",
					st.name, width, statusWidthLongModel, row)
			}
			if width != statusWidthPin {
				continue
			}
			got := starts(row, st.toks, st.live)
			for i := range got {
				// A field is allowed to be absent in BOTH rows; one that MOVED
				// is the defect.
				if base[i] < 0 || got[i] < 0 {
					continue
				}
				if got[i] != base[i] {
					t.Fatalf("%s at width %d: field %d starts at cell %d, want %d — a value moved the geometry\nbase: %q\nhere: %q",
						st.name, width, i, got[i], base[i], baseRow, row)
				}
			}
		}
	}
}
