package lineedit

import (
	"fmt"
	"strings"
	"testing"
)

// note_row_info_bar_test.go — the RESERVED LAYOUT's own answer to "does a flood
// of notice rows push the status row off the screen?".
//
// THE REPORT THIS PINS (task 01a1417a, operator 2026-10-16): `gopi chat`'s
// notification rows (`[gopi] 🔎 self-record …`, `📝 …`, `🧠 auto-memory …`) were
// said to have no row bound, so several at once accumulated height and pushed
// the info bar (the status row) out of the visible area. The quoted evidence was
// a `gopi ui_state --target chat` reading whose tail showed the turn rule with
// BLANK rows under it.
//
// WHAT THIS FILE MEASURES, and what it does not. It drives the REAL editor
// (newStatusHarness: the real region, the real read loop, the real band writer,
// the real status publisher) and asserts the property the report cares about —
// the status row is at the layout's OWN row and still carries the status. It
// does NOT re-derive the report's screen reading: that reading is a dump of a
// live session, and where the status row was in it is a question about the
// dump's sections rather than about this arithmetic.
//
// THE STATUS ROW IS NOT PART OF THE BAND'S ARITHMETIC, which is why this test is
// expected GREEN and is kept as the regression guard rather than as a repro of a
// live defect: computePlan resolves `StatusRow = rows - statusRows` FIRST
// (region.go) and the band takes the residual (`free - editingRows - menuRows`),
// so N notice rows are committed INTO the band, one scroll per row, and the
// layout's rows above the status row are consumed by the band's own scroll. A
// note can therefore never move the status row: the row is computed from the
// terminal's height, not from the transcript's length.

const (
	// noteFloodRows is the sample the acceptance names: ten notice rows, the
	// count a turn's self-record + auto-memory + event notices can reach.
	noteFloodRows = 10
	// noteFloodStatusLabel / noteFloodStatusLive are what the status source
	// answers: an ordinary chat status row (model · turn · … · ready). The label
	// and the live half are asserted SEPARATELY because the row's two halves are
	// laid out against opposite margins (label left, live right) — a flood that
	// truncated the row would take the live half first.
	noteFloodStatusLabel = "deepseek-flash"
	noteFloodStatusLive  = "ready"
)

// noteFloodRow is the i'th notice's text: the production shape of a `[gopi]`
// notice row (cmd/agent routes the agent's logger through backgroundWriter into
// the band), one row per notice, no wrapping.
func noteFloodRow(i int) string {
	return fmt.Sprintf("[gopi] notice row %02d of %d", i+1, noteFloodRows)
}

// TestNoticeFloodKeepsTheStatusRowVisible is the acceptance's minimal sample:
// with noteFloodRows notice rows committed one after another, the status row is
// still the layout's LAST row and still carries BOTH halves of the status.
//
// The failure it guards: a note committed BELOW the status row, or a note that
// moved the status row, would leave the last row blank (or carrying a notice) —
// the shape the report describes.
func TestNoticeFloodKeepsTheStatusRowVisible(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)
	h.ed.SetStatus(func() Status {
		return Status{Label: noteFloodStatusLabel, Live: noteFloodStatusLive}
	})

	// The status row is a LAYOUT fact, read before any note: it is what the
	// assertion below compares against, so a plan that moved mid-test cannot hide
	// behind a re-read at the end.
	statusRow := h.ed.region.StatusRow()
	if want := statusTestRows - 1; statusRow != want {
		t.Fatalf("StatusRow = %d, want the screen's last row %d — the row this test asserts on is not the one the layout reserved", statusRow, want)
	}

	for i := 0; i < noteFloodRows; i++ {
		h.ed.Note(noteFloodRow(i), BasicPalette.ToolCall)
	}
	// The LAST notice is the drain proof: notes are committed in order by the one
	// reader, so seeing the last one on screen means every earlier one was
	// committed too — which is what makes "ten rows went in" a fact rather than a
	// hope.
	h.waitContents(t, noteFloodRow(noteFloodRows-1))

	h.replay()
	row := h.screen.Line(statusRow)
	if strings.TrimSpace(row) == "" {
		t.Fatalf("the status row (%d) is BLANK after %d notice rows — the flood pushed the info bar off the screen\n--- screen ---\n%s",
			statusRow, noteFloodRows, h.contents())
	}
	if !strings.Contains(row, noteFloodStatusLabel) {
		t.Errorf("the status row lost its LABEL after %d notice rows: %q does not carry %q", noteFloodRows, row, noteFloodStatusLabel)
	}
	if !strings.Contains(row, noteFloodStatusLive) {
		t.Errorf("the status row lost its LIVE half after %d notice rows: %q does not carry %q", noteFloodRows, row, noteFloodStatusLive)
	}

	// A notice must never BE the status row: the row is the chrome, the notices
	// are transcript. This is the stronger half of the assertion above — a blank
	// check alone would pass if a notice had landed there.
	if strings.Contains(row, "[gopi] notice row") {
		t.Fatalf("a notice row landed ON the status row (%d): %q", statusRow, row)
	}

	// Every notice row is IN THE BAND — i.e. on a row ABOVE the editing line —
	// and the last one is on screen: the rows are committed (scrolling the
	// transcript), not stacked into the chrome.
	grid := h.contents()
	if !strings.Contains(grid, noteFloodRow(noteFloodRows-1)) {
		t.Fatalf("the last notice is not on screen at all after the flood\n--- screen ---\n%s", grid)
	}
}

// TestNoticeFloodLeavesTheStatusRowAtItsOwnRow is the geometry half: the flood
// changes the BAND's content, never the plan. A note that raised the band's row
// count would move the editing line (and the status row with it) — the layout's
// own invariant is that the status row is anchored to the terminal's last row,
// so this asserts the plan is byte-identical before and after.
func TestNoticeFloodLeavesTheStatusRowAtItsOwnRow(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)
	h.ed.SetStatus(func() Status {
		return Status{Label: noteFloodStatusLabel, Live: noteFloodStatusLive}
	})

	before := h.ed.region.Plan()
	for i := 0; i < noteFloodRows; i++ {
		h.ed.Note(noteFloodRow(i), BasicPalette.ToolCall)
	}
	h.waitContents(t, noteFloodRow(noteFloodRows-1))
	after := h.ed.region.Plan()

	if before != after {
		t.Fatalf("the reserved plan MOVED across %d notice rows:\n before %+v\n after  %+v", noteFloodRows, before, after)
	}
	if after.StatusRow != statusTestRows-1 {
		t.Fatalf("StatusRow = %d after the flood, want %d (the status row is anchored to the terminal's last row, never to the transcript's length)",
			after.StatusRow, statusTestRows-1)
	}
}
