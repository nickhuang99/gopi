package uiclient

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// ui_status_surface_contract_test.go — the `gopi ui` surface's half of the
// re-partition contract (task 01a0dcd1), stated at the level this surface really
// has: CELLS on its rendered status row.
//
// THE TWO SURFACES ARE NOT ONE IMPLEMENTATION, and this file is where that is
// made explicit rather than assumed away. `gopi chat` composes the shared status
// row (cmd/agent/chat_stream.go) through internal/lineedit's Status compositor —
// the grouped metrics column, the pinned cell arithmetic, the ladder. THIS
// surface (internal/uiclient) is a bubbletea model with its own `statusLine()`:
// state · turns · tool events, truncated to the window width. It shares no cell
// geometry with that row, so "the shared status row" is a claim about the
// PRODUCER, not about both screens — and the honest contract for this surface is
// the one below, asserted on its own rendering.
//
// What it DOES share is the property the epic is about, and that is asserted here
// in terms this surface can carry: no cell of its status row spells a price. A
// money read-out added here would not arrive through chat_stream.go, and without
// this guard nothing would notice.

// TestUIStatusRowCarriesNoMoney is this surface's absence proof. The glyphs are
// LITERALS on purpose: the money sub-cell and its constants were deleted from
// cmd/agent, so the subject of the claim has to be spelled here — and a claim
// about the ABSENCE of a rendering cannot be made by referencing live code.
func TestUIStatusRowCarriesNoMoney(t *testing.T) {
	m := newTestModel(t)
	m.streaming = true
	m.turns = 3
	m.toolEvents = 7
	m.width = 120

	row := m.statusLine()
	if w := lineedit.DisplayWidth(row); w > m.width {
		t.Fatalf("the ui status row is %d cells wide on a %d-column window: %q", w, m.width, row)
	}
	for _, form := range []string{"元", "¥", "€", "$", "cost", "spend"} {
		if strings.Contains(strings.ToLower(row), form) {
			t.Errorf("the ui status row spells a money read-out (%q): %q\n"+
				"the row shows measured facts only (task 01a0dcd1; see the RETIRED note on "+
				"statusCostCol in cmd/agent/chat_stream.go)", form, row)
		}
	}
	// The row still carries what it always did — this is a contract test, not a
	// rewrite: the absence proof must not have been satisfied by emptying the row.
	// The count is a REPL-turn count and names itself as one: the bare word
	// `turns` is THE turn count — the one `chats.turn_count` records — and this
	// row's number is that quantity for a live session (the operator's ruling,
	// 2026-10-27: every input face is a turn, so no qualifier is needed).
	for _, want := range []string{"streaming", sessionevent.CounterUnitTurn + " 3", "tool events 7"} {
		if !strings.Contains(row, want) {
			t.Errorf("the ui status row lost %q: %q", want, row)
		}
	}
}

// TestUIStatusRowIsMeasuredInCells pins the property the money guard rests on:
// this surface's row is a CELL-measured string, so a wide glyph cannot push it
// past the window. It is the same instrument (lineedit.DisplayWidth) the shared
// row's assertions use, which is what makes the two surfaces' evidence
// comparable rather than merely parallel.
func TestUIStatusRowIsMeasuredInCells(t *testing.T) {
	m := newTestModel(t)
	m.streaming = true
	m.turns = 12345
	m.toolEvents = 678
	for _, width := range []int{120, 80, 60, 40, 24, 12, 1} {
		m.width = width
		row := m.statusLine()
		if w := lineedit.DisplayWidth(row); w > width {
			t.Errorf("width %d: the ui status row measures %d cells: %q", width, w, row)
		}
	}
}
