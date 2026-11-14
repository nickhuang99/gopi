package main

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// chat_turn_layout_test.go — the turn structure and the status row's column
// alignment, asserted at the REPL's own seam and on the RENDERED GRID.
//
// Two facts are pinned here, both of them stated as cells rather than as
// intentions:
//
//  1. a two-turn scripted run shows its turns SEPARATED (a closing rule, a
//     numbered header) and its body ALIGNED (one left edge per turn), with the
//     editing line and the status row untouched by any of it;
//  2. the status row's fields hold their COLUMNS while their values change — a
//     tool row appearing, a turn counter growing a digit — which is the
//     "readable status fields" half of the layout work.

// TestChatTurnLayout_TwoTurnsAreSeparatedAndAligned drives two turns through the
// SAME wiring production uses (newChatTurnRenderer → BeginTurn/Write/Finish/
// EndTurn) and reads the chrome off the grid: both headers, the closing rule
// between them, and one left edge for every body line.
func TestChatTurnLayout_TwoTurnsAreSeparatedAndAligned(t *testing.T) {
	h := newStreamHarness(t)
	band := h.in.Answer()
	if band == nil {
		t.Fatal("no answer band on a terminal that hosts the layout")
	}
	status := newChatStatusSource("glm-5.3-flash", false)
	h.in.SetStatus(status.status)

	// The user's line is on the editing row before the turns stream, and it must
	// still be there afterwards.
	const typed = "typed before the turns"
	h.typeKeys(strings.Split(typed, "")...)
	editorTop := h.editorRow(t)
	h.waitFor(t, "the typed line on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(editorTop), typed)
	})
	editorRow := h.screen.Line(editorTop)

	// The renderer is built from the REPL's input layer: master's constructor
	// resolves BOTH the live band and the historic writer from it (h.out is the
	// input layer's own writer here), so the answer cannot land in a band while
	// the fallback goes somewhere else.
	renderer := newChatTurnRenderer(h.in)
	if !renderer.Reserved() {
		t.Fatal("the turn renderer did not take the reserved path")
	}
	// Turn 1.
	renderer.BeginTurn(1, "first question")
	renderer.Write("first answer alpha\n")
	renderer.Finish()
	renderer.EndTurn()
	// The publish is ASYNCHRONOUS — the editor's read loop draws it on the wake —
	// so the wait is on the SCREEN, never on the renderer's draw counter: that
	// counter is incremented for whatever draw the loop services, so an EARLIER
	// publish satisfies it and the assertion then reads a status row that has not
	// been drawn yet. (That is the instrument master retired; chat_stream_test.go
	// carries the measurement. This test is the ported branch test and must wait
	// the same way as the file it now lives beside.)
	publishChatStatus(h.in, status, 1, statusLiveIdleTurn, 1234)
	// Turn 2.
	renderer.BeginTurn(2, "second question")
	renderer.Write("second answer beta\n")
	renderer.Finish()
	renderer.EndTurn()
	publishChatStatus(h.in, status, 2, statusLiveIdleTurn, 4321)
	// BOTH halves of the final publish, because the displayed turn count is the
	// completed count plus the running one — so "turn2" alone would also be
	// satisfied by the mid-turn draw. The idle hint is what distinguishes the
	// finished turn. On the same repaint the band's committed rows arrive, which
	// is why one wait covers both.
	h.waitFor(t, "both turns drawn and the counters published", func() bool {
		h.replay()
		row := h.screen.Line(h.layout().StatusRow)
		return strings.Contains(row, "turn2") && strings.Contains(row, statusLiveIdle)
	})
	h.replay()

	top, bottom := h.screen.ScrollRegion()
	bandText := h.rows(top, bottom)
	lines := splitRows(bandText)

	// 1. Both headers exist, in order.
	h1 := indexOfRow(lines, "turn 1")
	h2 := indexOfRow(lines, "turn 2")
	if h1 < 0 || h2 < 0 {
		t.Fatalf("the band does not show both turn headers (turn 1 row %d, turn 2 row %d)\n--- band ---\n%s", h1, h2, bandText)
	}
	if h2 <= h1 {
		t.Fatalf("turn 2's header (row %d) is not below turn 1's (row %d)\n--- band ---\n%s", h2, h1, bandText)
	}
	// 2. A closing rule sits between the last line of turn 1 and the header of
	//    turn 2 — the separation the before-grid lacked entirely.
	ruleRow := -1
	for r := h1 + 1; r < h2; r++ {
		if isFullRuleRow(lines[r], completionCols) {
			ruleRow = r
		}
	}
	if ruleRow < 0 {
		t.Fatalf("no closing rule between turn 1 (header row %d) and turn 2 (header row %d)\n--- band ---\n%s", h1, h2, bandText)
	}
	// 3. Every body line starts at the same cell (the turn's marker column).
	wantCol := displayCellsForTest(lineedit.TurnMarker)
	for _, body := range []string{"first answer alpha", "second answer beta"} {
		row := indexOfRow(lines, body)
		if row < 0 {
			t.Fatalf("body %q missing from the band\n--- band ---\n%s", body, bandText)
		}
		got := leadingCells(lines[row], body)
		if got != wantCol {
			t.Fatalf("body %q starts at cell %d, want %d (the turn's shared left edge)\n--- band ---\n%s", body, got, wantCol, bandText)
		}
	}
	// 4. The chrome never moved: the editing row kept the user's text, and the
	//    status row (below the band) shows the counters.
	if got := h.screen.Line(editorTop); got != editorRow {
		t.Fatalf("the editing row changed: %q -> %q\n--- screen ---\n%s", editorRow, got, h.screen.Contents())
	}
	statusRow := h.screen.Line(h.layout().StatusRow)
	for _, want := range []string{"glm-5.3-flash", "turn2", statusLiveIdle} {
		if !strings.Contains(statusRow, want) {
			t.Fatalf("status row = %q, want %q\n--- screen ---\n%s", statusRow, want, h.screen.Contents())
		}
	}
	t.Logf("two turns on the grid (band [%d,%d]):\n%s", top, bottom, h.screen.Contents())
}

// TestChatStatus_ColumnsHoldStillWhileValuesChange is the column-alignment
// assertion at the composition seam: the START CELL of every field is identical
// across the states a live turn walks through — no tool, a tool running, a tool
// done, a second tool, thinking, a large context, turn9 → turn10, 999 → 1.0k
// tokens. Without reserved widths every one of those transitions slid the fields
// to their right, which is the "the numbers jump around while it works" defect.
//
// The tool column STATES its idle fact in words (statusNoToolLabel — operator
// bug #5 retired the blank indent), so its CELL is pinned directly in every state
// AND by (a) the fields AFTER it not moving as values change and (b) the tool
// glyph landing on one and the same cell in both tool states. It sits between the
// iter field and the metrics group — the mode column that used to stand between
// them is retired (the tool fact it also stated lives in this one column now).
func TestChatStatus_ColumnsHoldStillWhileValuesChange(t *testing.T) {
	s := newChatStatusSource("deepseek-flash", true)
	const cols = a5WideColsForTest

	// The cell each field starts at in the row as drawn (the aligned layout at
	// the pinned width). A field the state does not carry comes back -1.
	// tokensNow / liveNow are the CURRENT values the row is probed with: a field
	// is located by a string it actually carries (the token COUNT, which starts
	// on the column's own origin — the unit word after it is inside the field).
	tokensNow, liveNow := 999, statusLiveIdle
	starts := func() []int {
		row := s.status().Line(cols)
		return []int{
			fieldStart(row, "deepseek-flash"),
			fieldStart(row, "turn"),
			fieldStart(row, tokenAnchor(tokensNow)),
			fieldStart(row, liveAnchor(liveNow)),
		}
	}
	require := func(name string, got []int) {
		for i, v := range got {
			if v < 0 {
				t.Fatalf("%s: field %d is missing from the status row %q",
					name, i, s.status().Line(cols))
			}
		}
	}
	requireToolCol := func(name, row string) int {
		col := fieldStart(row, toolGlyph)
		if col < 0 {
			t.Fatalf("%s: the tool row is missing from %q", name, row)
		}
		return col
	}

	tokensNow = 999
	s.update(9, 0, 999, "")
	base := starts()
	require("turn9 / 999 tokens / idle", base)

	// The tool column's cell, measured from a real tool row, and the state list.
	// It must sit AFTER the iter field and BEFORE the metrics group: the columns
	// either side of it are iter (base[1]) and the context pair (base[2]).
	s.setTool("bash", "head -3 go.mod")
	toolCol := requireToolCol("a tool running", s.status().Line(cols))
	if toolCol <= base[1] || toolCol >= base[2] {
		t.Fatalf("the tool field starts at cell %d, want it between the iter field (%d) and the token field (%d)\nrow: %q",
			toolCol, base[1], base[2], s.status().Line(cols))
	}
	s.clearTool()
	s.update(9, 0, 999, "")

	states := []struct {
		name   string
		do     func()
		tokens int
		live   string
	}{
		{"turn10 / 1.0k tokens", func() { s.update(10, 0, 1000, "") }, 1000, statusLiveIdle},
		{"a tool running", func() { s.setTool("bash", "head -3 go.mod") }, 1000, statusLiveIdle},
		{"the tool done", func() { s.setToolDone("bash") }, 1000, statusLiveIdle},
		{"a second, longer tool", func() { s.setTool("read", "internal/lineedit/answer.go") }, 1000, statusLiveIdle},
		{"a turn in flight", func() { s.update(10, 11, 1000, "") }, 1000, statusLiveWordSent},
		{"large context", func() { s.update(10, 0, statusTokensHeavy+1000, "") }, statusTokensHeavy + 1000, statusLiveIdle},
	}
	for _, st := range states {
		st.do()
		tokensNow, liveNow = st.tokens, st.live
		row := s.status().Line(cols)
		got := starts()
		require(st.name, got)
		for i := range got {
			if got[i] != base[i] {
				t.Fatalf("after %s: field %d starts at cell %d, want %d — the columns moved\nbefore: %v\nafter:  %v\nrow: %q",
					st.name, i, got[i], base[i], base, got, row)
			}
		}
		// In a tool state the glyph itself is on the tool column: the column
		// exists whether or not it has text.
		if strings.Contains(row, toolGlyph) {
			if col := fieldStart(row, toolGlyph); col != toolCol {
				t.Fatalf("after %s: the tool row starts at cell %d, want %d\nrow: %q", st.name, col, toolCol, row)
			}
		}
	}
	// The row is one screen row at the pinned width and still one at a narrow
	// terminal, where the aligned form degrades to the historic unaligned
	// composition (whole fields dropped, live hint kept).
	s.update(10, 0, 1000, "")
	s.setTool("bash", "head -3 go.mod")
	for _, width := range []int{cols, 60, 40, 24, 12, 1} {
		row := s.status().Line(width)
		if n := wrappedRowsForTest(row, width); n != 1 {
			t.Fatalf("status row at cols=%d occupies %d screen rows: %q", width, n, row)
		}
	}
}

// ── instruments ─────────────────────────────────────────────────────────────

// splitRows splits a replayed band dump into rows, right-trimmed.
func splitRows(band string) []string {
	out := []string{}
	for _, l := range strings.Split(band, "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// indexOfRow is the first row containing sub, or -1.
func indexOfRow(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

// isFullRuleRow reports whether a row is nothing but the turn rule glyph — a
// closing rule (a header carries a label, so it is not one).
func isFullRuleRow(row string, cols int) bool {
	return row == strings.Repeat(lineedit.TurnRuleGlyph, cols)
}

// leadingCells is how many DISPLAY cells precede body inside its row (the body's
// start column, measured in cells rather than bytes so a wide glyph counts two).
func leadingCells(row, body string) int {
	i := strings.Index(row, body)
	if i < 0 {
		return -1
	}
	return displayCellsForTest(row[:i])
}

// fieldStart is the cell a field starts at inside the row, or -1.
func fieldStart(row, field string) int {
	i := strings.Index(row, field)
	if i < 0 {
		return -1
	}
	return displayCellsForTest(row[:i])
}

// displayCellsForTest counts display cells the way the renderer does (a wide
// rune costs two) — the helper is defined HERE so the assertion never inherits a
// different notion of width from the code under test.
func displayCellsForTest(s string) int {
	n := 0
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r':
		case r >= 0x1100 && r <= 0x115F, r >= 0x2E80 && r <= 0xA4CF, r >= 0xAC00 && r <= 0xD7A3,
			r >= 0xF900 && r <= 0xFAFF, r >= 0xFE30 && r <= 0xFE6F, r >= 0xFF00 && r <= 0xFF60,
			r >= 0xFFE0 && r <= 0xFFE6, r >= 0x1F300 && r <= 0x1F64F, r >= 0x1F900 && r <= 0x1F9FF:
			n += 2
		default:
			n++
		}
	}
	return n
}

// wrappedRowsForTest is the terminal's own wrap rule: how many screen rows s
// occupies at width cols.
func wrappedRowsForTest(s string, cols int) int {
	if cols <= 0 {
		return 1
	}
	rows, col := 1, 0
	for _, r := range s {
		w := 1
		if displayCellsForTest(string(r)) == 2 {
			w = 2
		}
		if col+w > cols {
			rows++
			col = 0
		}
		col += w
	}
	return rows
}

// a5WideColsForTest is a terminal wide enough for the whole aligned status row
// (the pinned layout's own width, which is what production composes at).
const a5WideColsForTest = statusColumnsAtCols

// tokenAnchor is the string that starts ON the token column's origin: the count
// itself. The unit word after it is inside the field, so it is the wrong anchor
// (it moves by one cell when the count grows a digit — measured).
func tokenAnchor(tokens int) string { return formatCountBounded(tokens) }

// liveAnchor is the string that starts on the live hint's column origin.
func liveAnchor(live string) string { return live }
