package main

import (
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// chat_status_ctx_label_test.go — the LABEL + FOLD decision (task 01a0ddab),
// pinned as the epic's acceptance asks: the test fails BEFORE the change and
// passes after it.
//
// The operator's words (live `gopi chat`, 2026-09-26):
//
//	"the percentage is still there and I don't know what does that help. also
//	 '13.1k/200.0k'? what is this? cache-hit/cache-miss? '6%'? I think this is
//	 most meaningless percentage."
//
// Two claims, and each is stated as a property of the COMPOSED CELLS at EVERY
// width the aligned geometry can produce — never as prose about one rendering:
//
//	1. The context pair is NAMED. The row carries two token pairs of the same
//	   unit (context used/window and cache served/written), and the words are what
//	   tell them apart; the cache pair's arrows are not enough, because the pair
//	   they distinguish is the one the reader has to FIND first.
//	2. The window percentage is NOT on the aligned row. Both of its operands are
//	   already drawn (the pair states used AND max), so it was a lower-precision
//	   restatement — and unlabelled it read as a cache hit rate.
//
// The UNALIGNED fallback is deliberately NOT covered by claim 2, and that is a
// decision rather than an omission: it has no reserved cells, it does not always
// carry the pair, and statusCtxBase keeps its ratio there.

// ctxLabelRowAt renders the row at cols through the source's own accessor (the
// composer the REPL draws with), with EVERY field given a value so "the field is
// absent" can only mean "the ladder shed it".
func ctxLabelRowAt(t *testing.T, cols int, ops statusOps) string {
	t.Helper()
	s := statusDegradeState(t)
	s.setOpsFn(func() statusOps { return ops })
	s.update(9, 0, 0, "")
	s.setAvailCols(cols)
	return s.status().Line(cols)
}

// ctxLabelOps is the operator's own row as status OPS: 13.1k of a 200.0k window
// (internal/agent/context.go's DefaultContextWindow), the cache pair he read
// beside it, and a running clock.
func ctxLabelOps() statusOps {
	return statusOps{
		ctxUsed:      13100,
		ctxMax:       ctxWindowDeployedForTest,
		ctxMeasured:  true,
		cacheRead:    statusProbeCacheRead,
		cacheWrite:   statusProbeCacheWrite,
		elapsed:      4 * time.Minute,
		elapsedKnown: true,
	}
}

// TestStatusRowMetricsReadOutIsLabelled is claim 1: the metrics group's leading
// read-out reaches the screen WITH the name of the fact it states, at EVERY width
// where the group survives — the row carries more than one number of the same
// shape, and the words are what tell them apart.
//
// THE LEADING READ-OUT IS THE CONTEXT PAIR'S QUALIFIED FORM (task 01a0f8c9, the
// re-partition). The group's leading sub-cell used to be the hit-rate read-out
// (task 01a0e8c7) and it is now the pair — the number that predicts a compaction —
// whenever a window is measured; the hit rate keeps a home inside the group (the
// cache sub-cell's own value) and is asserted by its own guards
// (TestStatusHitRateIsSilentWhenNothingWasMeasured and the emphasis cases).
//
// So this case states the PROPERTY the re-partition promises, over the production
// composer: at every width where the group survives, the group's reserved origin
// holds a read-out that NAMES the fact it states — "ctx" for the pair (with the
// denominator's qualifier), "hit" for the rate where the cache is measured without a
// window (a session that has re-sent a prompt before any context has been counted).
// An unnamed ratio beside a pair of the same shape is the ambiguity the names exist
// to close, and the naming is what this test is for — not which of the two facts
// leads (that is TestStatusMetricsFieldsLeadWithTheContextPair).
func TestStatusRowMetricsReadOutIsLabelled(t *testing.T) {
	ops := ctxLabelOps()
	// The group's reserved start cell, derived from the production columns rather
	// than counted by hand (the same derivation the provenance tests use).
	groupStart := statusModelCol + statusTurnCol + statusToolCol +
		statusColumnSepTotalN*lineedit.StatusColumnSepCells

	seen := 0
	for cols := statusLayoutBudgetCols; cols >= statusWidthMin; cols-- {
		row := ctxLabelRowAt(t, cols, ops)
		readOut := statusCtxField(ops, ops.ctxUsed)
		if readOut == "" {
			t.Fatalf("the fixture composes no read-out — the test would measure nothing")
		}
		if !strings.Contains(row, readOut) {
			// The ladder shed the group at this width: nothing to name.
			continue
		}
		seen++
		// The fact name is the read-out's FIRST cells: the group leads with the
		// word that says what the number is, so the group's reserved origin is
		// where that word starts. WHICH word is the composer's business (the pair's
		// "ctx" where a window is measured, the rate's "hit" where only the cache
		// counters are): both are the fact name of the fact the sub-cell drew, and
		// a read-out carrying neither is the unnamed number this case forbids.
		if !strings.HasPrefix(readOut, statusCtxLabel) && !strings.HasPrefix(readOut, statusHitRateLabel) {
			t.Fatalf("cols=%d: the read-out %q leads with neither fact name (%q / %q) — an unnamed ratio "+
				"beside a pair of the same shape is the ambiguity the names exist to close",
				cols, readOut, statusCtxLabel, statusHitRateLabel)
		}
		if got := fieldStart(row, readOut); got != groupStart {
			t.Fatalf("cols=%d: the read-out starts at cell %d, want the group's reserved cell %d — "+
				"the name must be the sub-cell's FIRST cells, or the number is still the thing the "+
				"reader has to identify\nrow: %q", cols, got, groupStart, row)
		}
	}
	if seen == 0 {
		t.Fatalf("the sweep never saw the read-out on the row: this test measured nothing")
	}
	t.Logf("the labelled read-out held cell %d across %d widths", groupStart, seen)

	// THE FALLBACK, on its own footing: with the cache counters unmeasured the
	// group's leading sub-cell is the context pair, and there it must be the
	// pair's QUALIFIED form — a bare denominator beside the cache pair is the
	// defect bug F closed, and a fallback that reopened it would do so exactly
	// where a reader has no other denominator statement to read.
	fallbackOps := ops
	fallbackOps.cacheRead, fallbackOps.cacheWrite = 0, 0
	pair := statusCtxPair(fallbackOps, fallbackOps.ctxUsed)
	// The expected form is the PRODUCTION composer's, not a hand-assembled
	// label+gap+pair: the sub-cell's FOLD decides where the gap yields (a pair whose
	// honest denominator is four cells closes onto its label rather than losing it),
	// and a test that spelled the spacing itself would pin a spacing the composer no
	// longer draws.
	labelled := statusCtxField(fallbackOps, fallbackOps.ctxUsed)
	fallbackSeen := 0
	for cols := statusLayoutBudgetCols; cols >= statusWidthMin; cols-- {
		row := ctxLabelRowAt(t, cols, fallbackOps)
		if !strings.Contains(row, pair) {
			continue
		}
		fallbackSeen++
		if !strings.Contains(row, labelled) {
			t.Errorf("cols=%d: the fallback read-out carries the pair %q WITHOUT its qualifier — an "+
				"unqualified denominator is the defect this row's qualifier exists to close\nrow: %q",
				cols, pair, row)
		}
	}
	if fallbackSeen == 0 {
		t.Fatalf("the sweep never saw the context pair's fallback form: this half measured nothing")
	}
	t.Logf("the fallback's qualified pair held the screen across %d widths", fallbackSeen)
}

// TestStatusRowCtxPercentageIsOffTheAlignedRow is claim 2, stated as an ABSENCE
// PROOF at every width the aligned geometry produces: the percentage
// statusCtxPct derives is on NO aligned row.
//
// The proof sweeps rather than spot-checks because the claim is "the row does not
// show it", and a claim about one width is a claim a narrower terminal can
// falsify. The probe is the production formatter's own output, so a value that
// happened to be spelled like it would not be mistaken for it (the same reason
// the degradation table derives its probes).
//
// THE ASYMMETRY WITH THE UNALIGNED FALLBACK IS A DECISION, and it is pinned below
// so a later "cleanup" has to argue with a test rather than with a comment. The
// fold's reason is specific to the ALIGNED row: there the pair's two operands are
// reserved cells a reader always sees, and the percentage beside them was a
// lower-precision restatement. The fallback reserves nothing — it cuts a
// concatenated label from the right, so a percentage can be the ONLY complete
// statement of the read-out that survives the cut, and statusCtxBase keeps it.
func TestStatusRowCtxPercentageIsOffTheAlignedRow(t *testing.T) {
	ops := ctxLabelOps()
	pct := statusCtxPct(ops.ctxUsed, ops.ctxMax)
	if pct == "" {
		t.Fatalf("the fixture's pair produces no percentage — the case itself is wrong")
	}
	// The probe is the percentage AS THE ROW WOULD SPELL IT: the derivation is
	// alive (statusCtxPct) and the unaligned label still draws it, so the absence
	// asserted here is about the ALIGNED rows only.
	aligned := 0
	for cols := statusLayoutBudgetCols; cols >= statusWidthMin; cols-- {
		row := ctxLabelRowAt(t, cols, ops)
		if !strings.Contains(row, lineedit.StatusColumnSep) {
			continue // the unaligned fallback: its label keeps the ratio (by decision).
		}
		aligned++
		if strings.Contains(row, pct) {
			t.Fatalf("cols=%d: an aligned row carries the derived percentage %q — both of its operands "+
				"(used and max) are already drawn on this row, so it earns no cells"+
				"\nrow: %q", cols, pct, row)
		}
	}
	if aligned == 0 {
		t.Fatalf("the sweep saw no aligned row: this test measured nothing")
	}
	t.Logf("no aligned row among %d widths carries %q", aligned, pct)

	// The OTHER side of the asymmetry, asserted rather than assumed: the unaligned
	// fallback DOES carry the ratio, and below the aligned floor it still does —
	// there the pair is often absent and the ratio is the only complete read-out
	// left. A "cleanup" that folded both compositions would fail HERE.
	s := statusDegradeState(t)
	s.setOpsFn(func() statusOps { return ctxLabelOps() })
	s.update(9, 0, 0, "")
	s.setAvailCols(statusWidthMin)
	if label := s.status().Label; !strings.Contains(label, pct) {
		t.Errorf("the unaligned label lost its ratio %q: the fold is about the aligned row, where the "+
			"pair's own operands are drawn — the fallback has no reserved pair to state it\nlabel: %q",
			pct, label)
	}
}

// TestStatusRowCtxLabelCellsAreBudgetNeutral pins the ARITHMETIC half of the
// decision: the label, the pair's honest worst case and the denominator's
// qualifier fill the sub-cell, the bar derives to zero (so nothing is
// reserved-but-undrawn), and the row's geometry still comes to the budget exactly —
// the cells each earlier re-partition freed were SPENT (pair 10 → 13 for the pair's
// honest worst case, then slack 1 → 0 for the qualifier that says what the
// denominator IS), never hoarded.
func TestStatusRowCtxLabelCellsAreBudgetNeutral(t *testing.T) {
	// The reservation IS the fact name, its gap, the pair and the qualifier — the four
	// fields the pinned row draws, each declared and counted here rather than summed by
	// hand.
	//
	// RE-PARTITION (task 01a0f8c9). The qualifier USED to be composed INSIDE the
	// label's cells (the label yielded to it, task 01a0e394) and the reservation did not
	// count it. That shape was sized for a SIX-cell pair side — the retired formatCount's
	// "200.0k" — while the ladder the row actually draws tops out at
	// statusCountSideTextWidest ("1M"/"92G", 3); the six cells of that over-reservation
	// are the rate's new home, and the four cells the pinned row really draws are now
	// what this sub-cell reserves. So the invariant stated here is the same one the port
	// preserved — the reservation is exactly the pinned row's content — applied to a
	// reservation that no longer hides a drawn field inside another's cells.
	if got, want := statusCtxCol, statusCtxLabelCells+statusCtxBarGapCells+statusCtxPairCells+
		statusCtxQuantCells; got != want {
		t.Fatalf("statusCtxCol = %d, want %d (label %d + gap %d + pair %d + qualifier %d) — the "+
			"sub-cell's reservation must be exactly what the pinned row draws",
			got, want, statusCtxLabelCells, statusCtxBarGapCells, statusCtxPairCells, statusCtxQuantCells)
	}
	// The qualifier is a DECLARED width, and the cell it takes is one: pinned from
	// the glyph by the geometry tie, so a wider glyph cannot leave this stale.
	if got, want := statusCtxQuantCells, statusCtxQuantCellsPinned; got != want {
		t.Fatalf("statusCtxQuantCells = %d, want the pinned %d", got, want)
	}
	if got, want := statusCtxQuantCellsPinned, lineedit.DisplayWidth(statusCtxQuantGlyph); got != want {
		t.Fatalf("statusCtxQuantCellsPinned = %d, but the glyph %q is %d cells", got, statusCtxQuantGlyph, want)
	}
	if statusCtxBarCells != 0 {
		t.Fatalf("statusCtxBarCells = %d, want 0: a bar would need cells this budget does not have, and a "+
			"reservation nothing draws in is the retired-column defect", statusCtxBarCells)
	}
	// Two-sided: the geometry sums to the pin, and the pin plus the named slack is
	// the budget. Both directions are asserted so neither can drift silently.
	if got, want := statusPinCols, statusColumnsAtCols; got != want {
		t.Fatalf("statusPinCols = %d, statusColumnsAtCols = %d: the geometry's sum and the drawn pin are "+
			"one number", got, want)
	}
	if got, want := statusColumnsAtCols+statusPinSlackCells, statusLayoutBudgetCols; got != want {
		t.Fatalf("the pin %d + slack %d = %d, want the budget %d", statusColumnsAtCols, statusPinSlackCells, got, want)
	}
	// Every sub-cell the group declares is a sub-cell it DRAWS: the count, the
	// widths and the list must agree (a read-out removed from one and not the
	// others is how the retired cache column came about).
	if got, want := len(statusMetricsSubCellWidths()), statusMetricsSubCells; got != want {
		t.Fatalf("statusMetricsSubCellWidths has %d entries, statusMetricsSubCells says %d", got, want)
	}
	if got, want := statusMetricsGroupWidth(statusMetricsSubCellWidths()), statusMetricsCol; got != want {
		t.Fatalf("the group's sub-cells sum to %d, statusMetricsCol says %d", got, want)
	}
}

// TestStatusRowCtxLabourSurvives24Cols is the narrow rung's own assertion: at 24
// columns the group is LONG shed, so the label is not on the row and must not be
// — but nothing may be pushed off it either (the live block's state word is the
// row's last yield), and the row must still fit its terminal.
func TestStatusRowCtxLabourSurvives24Cols(t *testing.T) {
	row := ctxLabelRowAt(t, statusDegradeWidth24, ctxLabelOps())
	if w := lineedit.DisplayWidth(row); w > statusDegradeWidth24 {
		t.Fatalf("the 24-column row is %d cells wide — it runs past its terminal\nrow: %q", w, row)
	}
	if !strings.Contains(row, statusLiveIdle) {
		t.Fatalf("the state word %q is missing at 24 columns: the live block is the row's last yield\nrow: %q",
			statusLiveIdle, row)
	}
	// The label is a SUB-CELL of the metrics group: where the ladder has shed the
	// group there is no sub-cell to carry it, and the row's cells are spent on the
	// identity columns the operator needs to see which session he is in. Asserting
	// its absence here keeps "the label is a sub-cell" a statement the geometry can
	// fail on, rather than a comment.
	if strings.Contains(row, statusCtxLabel) {
		t.Fatalf("the context label is on the 24-column row, where the metrics group is shed: a read-out "+
			"that survives its own column's shed is not a sub-cell\nrow: %q", row)
	}
	t.Logf("24-column row (%d cells): %q", lineedit.DisplayWidth(row), row)
}
