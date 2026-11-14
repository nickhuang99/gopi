package gopilog

// cost_test.go — pins the RECORD-vs-CALCULATION split of the cost block.
//
// The defect being pinned (operator's ruling, 2026-11-04): `gopi session log`
// rendered a `COST:` block whose every number was CALCULATED from the parsed
// `request` rows, so a reader of a LOG took it for something the session wrote.
// The block moved to `gopi session show` and reads what the session RECORDED.
// Four properties are asserted here, and each one alone would let the defect
// back in:
//
//	1. a block never renders a per-request re-derivation — the only inputs are
//	   the recorded totals and the recorded token columns;
//	2. an unrecorded quantity is STATED, never a 0 (Rule 15);
//	3. a species that records no per-direction split says so instead of
//	   inventing one from the rows;
//	4. the log carries the POINTER, and the pointer names the show verb.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// TestRecordedCostRendersTheRecordedTotals — the happy shape: all three totals
// come from `session_end`, and the passes line names that source so the number
// cannot be read as a per-request count.
func TestRecordedCostRendersTheRecordedTotals(t *testing.T) {
	out := FormatRecordedCost(RecordedCost{
		HasEndTotals: true,
		Passes:       27,
		TotalTokens:  2036246,
		ElapsedSec:   191.4,
	})
	for _, want := range []string{
		CostBlockHeader,
		CostBlockBasis,
		"27",
		"2,036,246 tokens",
		"191.4s",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("block missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, CostUnrecorded) {
		t.Errorf("a recorded session must not carry the unrecorded sentence:\n%s", out)
	}
}

// TestRecordedCostStatesNoVerdictAsAnAbsence — a session that wrote no
// `session_end` (still alive, killed before the end hook) recorded NOTHING. The
// block must say that in words: printing `Passes 0` would be a false
// measurement, and a block that vanished would be indistinguishable from a
// session that recorded no cost (Rule 15).
func TestRecordedCostStatesNoVerdictAsAnAbsence(t *testing.T) {
	out := FormatRecordedCost(RecordedCost{HasEndTotals: false})
	if !strings.Contains(out, CostUnrecorded) {
		t.Errorf("unrecorded totals must be STATED, got:\n%s", out)
	}
	if strings.Contains(out, "Passes    0") {
		t.Errorf("an unrecorded pass count rendered as the number 0:\n%s", out)
	}
	// The header still renders — the page must not go silently blank.
	if !strings.Contains(out, CostBlockHeader) {
		t.Errorf("the header must render even with nothing recorded:\n%s", out)
	}
}

// TestRecordedCostSplitsOnlyWhereAColumnRecordsIt — a chat session's row holds
// the per-direction columns; a run records one total in its verdict. The split
// line must distinguish those two, because "no split" and "a split of zero" are
// different answers and only one of them is a measurement.
func TestRecordedCostSplitsOnlyWhereAColumnRecordsIt(t *testing.T) {
	withColumns := FormatRecordedCost(RecordedCost{
		HasEndTotals:    false,
		HasTokenColumns: true,
		InputTokens:     5896844,
		OutputTokens:    22329,
		CacheTokens:     5896844,
	})
	for _, want := range []string{"in=5,896,844", "out=22,329", "cache=5,896,844"} {
		if !strings.Contains(withColumns, want) {
			t.Errorf("measured split missing %q:\n%s", want, withColumns)
		}
	}
	withoutColumns := FormatRecordedCost(RecordedCost{HasEndTotals: true, Passes: 3})
	if !strings.Contains(withoutColumns, CostSplitUnrecorded) {
		t.Errorf("a species with no recorded split must say so, got:\n%s", withoutColumns)
	}
	if strings.Contains(withoutColumns, "in=") {
		t.Errorf("an unrecorded split must not be invented:\n%s", withoutColumns)
	}
}

// TestCostPointerNamesTheShowVerbFromTheConstants — the log's pointer is an
// INSTRUCTION a reader will paste, so its verb must come from
// internal/cliverbs (Rule 13): a rename of the session family has to move the
// instruction with it, and a hand-spelled "session show" inside a string
// literal would not.
func TestCostPointerNamesTheShowVerbFromTheConstants(t *testing.T) {
	line := CostPointerLine()
	if !strings.Contains(line, cliverbs.SessionShowAddress) {
		t.Errorf("pointer %q does not carry the show address %q", line, cliverbs.SessionShowAddress)
	}
	if strings.Contains(line, "...") || strings.Contains(line, "…") {
		t.Errorf("pointer truncates the address it asks the reader to paste: %q", line)
	}
}

// TestCostBlockClosesThePointerChain — the log points at `session show`, so the
// show page must not be a dead end for the one question the log's pointer raises
// (real money). Measured before this arm existed, on
// 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f: `session show` printed "元" 0 times and
// "stats" 0 times, while `session stats <id> --cost` printed
// "total cost 1.7568 元 @ CNY" — a reader following the instruction arrived at a
// page that could not answer it.
//
// Asserted for BOTH species, because the block renders for both and a dead end on
// either one is the same defect. The verb comes from internal/cliverbs (Rule 13),
// so a rename of the session family moves the instruction with it.
func TestCostBlockClosesThePointerChain(t *testing.T) {
	for _, c := range []RecordedCost{
		{HasEndTotals: true, Passes: 27, TotalTokens: 2036246, ElapsedSec: 191.4},
		{HasEndTotals: false},
	} {
		out := FormatRecordedCost(c)
		if !strings.Contains(out, CostMoneyPointer) {
			t.Errorf("block does not carry the money pointer:\n%s", out)
		}
		if !strings.Contains(out, cliverbs.SessionStatsAddress) {
			t.Errorf("the money pointer %q does not name the stats address %q — the instruction must be the one a "+
				"reader can paste", CostMoneyPointer, cliverbs.SessionStatsAddress)
		}
		// The forbidden alternative: computing the money here. A 元 figure in this
		// block would be a second price calculation (the epic's hard constraint),
		// and this renderer has no price input to compute one from.
		if strings.Contains(out, "元") {
			t.Errorf("the block prints a money FIGURE — the price belongs to `session stats --cost`, and a second "+
				"computation is the defect this epic removes:\n%s", out)
		}
	}
}

// TestLogFormatCarriesNoCostBlockButDoesCarryThePointer — the split this change
// exists for, asserted on the rendered LOG rather than on the helper: the
// numbers are gone and the pointer is present. A future edit that restores the
// block "for convenience" or drops the pointer (leaving a silent omission)
// fails here.
func TestLogFormatCarriesNoCostBlockButDoesCarryThePointer(t *testing.T) {
	s := &Session{ID: "sess-cost", Model: "deepseek-v4-flash", Iterations: []Iteration{}}
	out := FormatSession(s)
	if strings.Contains(out, CostBlockHeader) {
		t.Errorf("the LOG still renders %q — that block is calculated, not logged:\n%s", CostBlockHeader, out)
	}
	for _, banned := range []string{"Prompt:", "Completion:", "Total:", "Iterations:"} {
		if strings.Contains(out, banned) {
			t.Errorf("the LOG still renders the calculated quantity %q:\n%s", banned, out)
		}
	}
	if !strings.Contains(out, CostPointerLine()) {
		t.Errorf("the LOG must point at the recorded view, got:\n%s", out)
	}
}
