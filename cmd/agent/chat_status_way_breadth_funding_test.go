package main

import (
	"testing"
	"time"
)

// The status row's geometry is a ZERO-SLACK budget (task 01a0e2e9): statusPinCols
// == statusLayoutBudgetCols and statusPinSlackCells is 0, and the doctrine since
// that task is that no column may reserve a cell NOTHING DRAWS (the retired-column
// defect, at the row's scale). Task 01a0faa7 (item 4/4 of epic 01a0f8c9) asks for
// the tool column's way-marker breadth to be restored to 4 and for the rate's
// cells to be paid by "a different named term that nothing draws".
//
// THIS TEST IS THAT CLAIM, MEASURED. A DONOR IS ONLY A DONOR IF ITS CELLS ARE
// NOT DRAWN, and that is a property of the PRODUCTION COMPOSER at reachable
// states — not of a comment. The table below drives each field's own composer
// over the value grid and records the widest form it can actually produce; a
// reservation WIDER than that (or one whose excess is not returned) is the air a
// future re-partition may spend, and a reservation EQUAL to it holds nothing to
// give. Measured at this pin: EVERY term is exactly drawn — which is why the
// breadth could not be restored from air, and why this test exists rather than a
// re-partition (see devdoc/status-row-way-breadth-funding.md).
//
// It is a guard, not a receipt: a future change that widens a reservation past
// what its composer can draw, or narrows one below it, fails HERE with the term's
// name and both numbers.
func TestStatusReservationsAreFullyDrawn(t *testing.T) {
	// statusAirProbeGrid is the value grid the composers are driven over. It
	// carries every rung boundary of the bounded count ladder (999/1000, 99999/
	// 100000, 999999/1000000) plus the production magnitudes this row is read at
	// (a 200k window, a 1M window, the operator's 1.66M-token cache session) — a
	// boundary that is not probed is a width that is not measured (Rule 1: the
	// resolution of a walk is a stated number).
	grid := []int{0, 1, 2, 9, 10, 99, 100, 999, 1000, 9999, 10000, 12000,
		42100, 64000, 99999, 100000, 200000, 999999, 1000000, 1662000,
		99999999, 100000000, 1000000000, 1 << 40, 1<<62 - 1}

	// widestCtx is the context sub-cell's widest DRAWN form, over every
	// (used, window) pair — the two sides are independent, so the grid is a
	// cross-product rather than a diagonal.
	widestCtx, ctxAt := 0, ""
	for _, used := range grid {
		for _, window := range grid {
			v := statusCtxField(statusOps{ctxMeasured: true, ctxUsed: used, ctxMax: window}, 0)
			if c := statusFieldCells(v); c > widestCtx {
				widestCtx, ctxAt = c, v
			}
		}
	}
	// widestCache is the cache sub-cell's value — the pair PLUS the labelled rate
	// riding it — over every (served, written) pair.
	widestCache, cacheAt := 0, ""
	for _, read := range grid {
		for _, write := range grid {
			v := statusCacheField(statusOps{cacheRead: read, cacheWrite: write})
			if c := statusFieldCells(v); c > widestCache {
				widestCache, cacheAt = c, v
			}
		}
	}
	// widestGroup is the metrics group at those two widest forms, JOINED — the
	// seam is spent only between two read-outs that are both present, so the
	// group's widest drawn form is their sum plus one seam.
	widestGroup := widestCtx + statusMetricsSepCells + widestCache

	// widestTool is the tool column's widest drawn digest, over the real tool
	// names a session reports and a digest of each of the three shapes the column
	// composes (none / a word / a long line).
	widestTool, toolAt := 0, ""
	for _, name := range []string{"bash", "read", "write", "edit", "code_search",
		"keyword_search", "web_search", "vision_analyze", "context_trim"} {
		for _, digest := range []string{"", "x", "retrieval: memory search hits"} {
			v := statusToolColumnFieldFor(name, digest, true)
			if c := statusFieldCells(v); c > widestTool {
				widestTool, toolAt = c, v
			}
		}
	}
	// widestLive is the live block through its own composer — the block weighs
	// every suffix against statusLiveCol and leaves an unfitting one OFF, so the
	// composer's own output is the drawn width.
	//
	// THE VOICE PHASES ARE PART OF THIS PROBE (task 01a116b0): the block's
	// reservation gained a cell for the `/voice` field, so the audit must drive the
	// state that field is drawn at — otherwise the widest form with the field on is
	// never measured and the new cell reads as "reserved, nothing draws" (this audit
	// is what would have caught a field that only ever rendered narrower than its
	// account).
	widestLive, liveAt := 0, ""
	for _, voice := range []voiceChatLive{voiceChatLiveIdle, voiceChatLiveRecording, voiceChatLiveTranscribing} {
		for _, hint := range []string{statusLiveIdle, statusLiveWordSent, statusLiveWordThinking, statusLiveWordResponding} {
			for _, el := range []time.Duration{time.Second, 59 * time.Second,
				59*time.Minute + 59*time.Second, time.Hour + 2*time.Minute, 100 * time.Hour} {
				v := statusLiveFieldVoice(hint, "", statusOps{elapsed: el, elapsedKnown: true}, voice)
				if c := statusFieldCells(v); c > widestLive {
					widestLive, liveAt = c, v
				}
			}
		}
	}

	// A NOTE ON WHAT "DRAWN" MEANS FOR THE CAPPED COMPOSERS. The tool column and
	// the live block compose INTO their own reservation (statusToolColumnField
	// truncates to statusToolCol; statusLiveField weighs every suffix and leaves
	// an unfitting one OFF), so for those two the measured width can never exceed
	// the reservation — their equality says the cells are REACHABLE at a real
	// state (a 14-cell tool name with a digest; "1h02m · ready"), not that a
	// wider form is impossible. For the group, the sub-cells and the ctx pair the
	// composers are NOT capped by the term being audited (statusCtxField and
	// statusCacheField compose freely and are bounded only by the formatter), so
	// their equality is the strong statement: no reachable state draws less.
	cases := []struct {
		name     string
		reserved int
		drawn    int
		at       string
	}{
		{"ctx sub-cell (statusCtxCol)", statusCtxCol, widestCtx, ctxAt},
		{"cache value (statusCacheCol + statusCacheHitRateCells)", statusCacheCol + statusCacheHitRateCells, widestCache, cacheAt},
		{"metrics group (statusMetricsFormCol)", statusMetricsFormCol, widestGroup, ctxAt + " · " + cacheAt},
		{"tool column (statusToolCol)", statusToolCol, widestTool, toolAt},
		{"live block (statusLiveCol)", statusLiveCol, widestLive, liveAt},
	}
	for _, c := range cases {
		if c.drawn > c.reserved {
			t.Errorf("%s: reserved %d, but the production composer DRAWS %d (%q) — the geometry is over budget, "+
				"and statusPinCols = statusLayoutBudgetCols can no longer hold", c.name, c.reserved, c.drawn, c.at)
		}
		if c.drawn < c.reserved {
			t.Logf("%s: reserved %d, widest drawn %d (%q) — %d cell(s) nothing draws; THIS term is a donor",
				c.name, c.reserved, c.drawn, c.at, c.reserved-c.drawn)
		}
	}

	// THE PIN HOLDS TO ITS LAST CELL: every reservation above is drawn by some
	// reachable state, so the row's budget is spent rather than hoarded — the
	// property task 01a0e2e9 established and every later re-partition inherits.
	if statusPinCols != statusLayoutBudgetCols || statusPinSlackCells != 0 {
		t.Errorf("the geometry is no longer zero-slack: statusPinCols = %d, budget = %d, slack = %d",
			statusPinCols, statusLayoutBudgetCols, statusPinSlackCells)
	}
	if statusToolCol != statusToolColOwn+statusModeColRetired+statusToolColWayBreadth {
		t.Errorf("statusToolCol = %d, want its own width %d + the retired mode cells %d + the way breadth %d",
			statusToolCol, statusToolColOwn, statusModeColRetired, statusToolColWayBreadth)
	}
	t.Logf("air audit: ctx %d/%d, cache %d/%d, group %d/%d, tool %d/%d, live %d/%d",
		widestCtx, statusCtxCol,
		widestCache, statusCacheCol+statusCacheHitRateCells,
		widestGroup, statusMetricsFormCol,
		widestTool, statusToolCol,
		widestLive, statusLiveCol)
}
