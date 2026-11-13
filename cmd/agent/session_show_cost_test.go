package main

// session_show_cost_test.go — the COST BLOCK ON THE RECORD-BACKED PAGE.
//
// The operator's ruling (2026-11-04): the cost block is CALCULATED, so it does
// not belong inside a log; it belongs on the view backed by a record, and it
// reads what the session RECORDED (its `session_end` verdicts, and for a chat
// its row's token columns) rather than re-summing the per-request rows.
//
// Three properties, each of which alone would let the defect back in:
//
//	1. the block renders for BOTH species — the ruling is about the show page as
//	   a whole, and a species that silently lacks it is the bug that started this;
//	2. a species with no recorded verdict says so, in words (Rule 15);
//	3. the numbers are the RECORDED ones — asserted by contrast: a session whose
//	   parsed `request` rows say one thing and whose verdict says another must
//	   print the verdict's.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/tools"
)

// TestSessionCostBlockRendersForARun — a task run: `session_end` carries all
// three totals, and the passes line names that producer so the number cannot be
// read as a per-request count.
func TestSessionCostBlockRendersForARun(t *testing.T) {
	show := tools.SessionShow{
		SessionID:     "run-cost",
		Kind:          tools.SessionKindRun,
		HasEndTotals:  true,
		EndPasses:     27,
		EndTokens:     2036246,
		EndElapsedSec: 191.4,
	}
	out := sessionCostBlock(show)
	for _, want := range []string{
		gopilog.CostBlockHeader,
		gopilog.CostBlockBasis,
		"27",
		"2,036,246 tokens",
		"191.4s",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("run cost block missing %q:\n%s", want, out)
		}
	}
	// A run's `session_end` records ONE total, so no split may be invented.
	if !strings.Contains(out, gopilog.CostSplitUnrecorded) {
		t.Errorf("a run must state that no per-direction split is recorded:\n%s", out)
	}
}

// TestSessionCostBlockRendersForAChat — a chat session: its ROW holds the
// per-direction columns, and the block prints them as the measurements they are.
func TestSessionCostBlockRendersForAChat(t *testing.T) {
	show := tools.SessionShow{
		SessionID: "chat-cost",
		Kind:      tools.SessionKindChat,
		HasChat:   true,
		Chat: tools.ChatSessionDetail{
			TokensReported: true,
			InputTokens:    5896844,
			OutputTokens:   22329,
			CacheTokens:    5896844,
		},
	}
	out := sessionCostBlock(show)
	for _, want := range []string{"in=5,896,844", "out=22,329", "cache=5,896,844"} {
		if !strings.Contains(out, want) {
			t.Errorf("chat cost block missing the measured split %q:\n%s", want, out)
		}
	}
	// A chat session writes no `session_end` (its process outlives turns), so the
	// block must state that absence rather than printing a 0 for passes.
	if !strings.Contains(out, gopilog.CostUnrecorded) {
		t.Errorf("a chat with no recorded verdict must state the absence:\n%s", out)
	}
}

// TestSessionCostBlockPrintsTheRecordedTotalsNotASum — the defect's own shape,
// asserted by contrast on ONE fixture: the show struct carries a verdict saying
// 27 passes / 2,036,246 tokens. A renderer that re-derived from the rows would
// have to print something else, so any figure other than the recorded one means
// the block is calculating again.
func TestSessionCostBlockPrintsTheRecordedTotalsNotASum(t *testing.T) {
	show := tools.SessionShow{
		HasEndTotals:  true,
		EndPasses:     27,
		EndTokens:     2036246,
		EndElapsedSec: 191.4,
	}
	out := sessionCostBlock(show)
	if !strings.Contains(out, "27") {
		t.Errorf("the recorded pass count 27 is missing — the block is not reading the verdict:\n%s", out)
	}
	if strings.Contains(out, "Passes    0") {
		t.Errorf("the block printed a 0 where the verdict recorded 27:\n%s", out)
	}
}

// TestSessionShowJSONPublishesTheRecordedCost — the machine face carries the
// same recording as the page (Rule 13). Two properties:
//
//  1. a recorded session publishes the three totals;
//  2. an unrecorded one OMITS them and says so with its own boolean — a
//     published `0` would be a measurement nobody took (Rule 15a: this is why
//     the fields are pointers).
func TestSessionShowJSONPublishesTheRecordedCost(t *testing.T) {
	recorded, err := jsonSessionShow(tools.SessionShow{
		SessionID:     "run-cost-json",
		Kind:          tools.SessionKindRun,
		HasEndTotals:  true,
		EndPasses:     27,
		EndTokens:     2036246,
		EndElapsedSec: 191.40867957,
	})
	if err != nil {
		t.Fatalf("jsonSessionShow: %v", err)
	}
	for _, want := range []string{
		`"recorded_cost_recorded": true`,
		`"recorded_cost_passes": 27`,
		`"recorded_cost_tokens": 2036246`,
		`"recorded_cost_elapsed_sec": 191.40867957`,
	} {
		if !strings.Contains(recorded, want) {
			t.Errorf("recorded session JSON missing %s:\n%s", want, recorded)
		}
	}

	unrecorded, err := jsonSessionShow(tools.SessionShow{
		SessionID: "chat-cost-json",
		Kind:      tools.SessionKindChat,
	})
	if err != nil {
		t.Fatalf("jsonSessionShow: %v", err)
	}
	if !strings.Contains(unrecorded, `"recorded_cost_recorded": false`) {
		t.Errorf("unrecorded session JSON must say so:\n%s", unrecorded)
	}
	for _, banned := range []string{"recorded_cost_passes", "recorded_cost_tokens", "recorded_cost_elapsed_sec"} {
		if strings.Contains(unrecorded, banned) {
			t.Errorf("unrecorded session published %q — an absent measurement must be OMITTED, not sent as 0:\n%s", banned, unrecorded)
		}
	}
}
