package main

import (
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// TestStatusCtxPairNeverDrawsDistinctCountersAsOneFigure is the regression guard for
// the operator's `ctx 1M/1M≡` report: the row stated the SAME figure for two counters
// with different values across four fifths of its own range (every used count from
// 100000 to a 200000 window), which is a pair that looks like a measurement and is
// not one — the reader cannot tell "the context is exactly full" from "the formatter
// ran out of digits".
//
// THE MECHANISM UNDER TEST is the pair's own spelling, so the assertions are on
// statusCtxPair (the ONE place the pair is spelled) and then on the RENDERED row — a
// helper agreeing with itself proves nothing about what the terminal shows (Rule 31).
//
// THE EXPECTED SIDES ARE DERIVED FROM THE PAIR'S OWN BUDGET (boundedCountRectDigits at
// statusCountCtxDigitsWidest), never typed: the point of this epic is that the spelling
// follows from one named allowance, so a test that re-typed a side would be a second
// statement of the rule and would go stale the moment the allowance moves.
func TestStatusCtxPairNeverDrawsDistinctCountersAsOneFigure(t *testing.T) {
	// THE DEPLOYED WINDOW IS THE CASE THAT FAILED (internal/agent/context.go's
	// DefaultContextWindow, 200000 — the window statusOps.ctxMax carries in a chat
	// session), and it is walked across the whole band the old form collapsed: at
	// 100000, 120000, 150000 and 199999 the two-digit sides were all "1M" against a
	// "1M" window, so before this fix the pair printed "1M/1M" at every one of them.
	deployed := ctxWindowDeployedForTest
	side := func(n int) string { return boundedCountRectDigits(n, statusCountCtxDigitsWidest).text }
	for _, used := range []int{100000, 120000, 150000, 150052, 175000, 199000} {
		ops := statusOps{ctxUsed: used, ctxMax: deployed, ctxMeasured: true}
		pair := statusCtxPair(ops, 0)
		if want := side(used) + statusCtxPairSep + side(deployed); pair != want {
			t.Fatalf("used=%d/%d renders %q, want %q — the pair must be drawn at its own budget "+
				"(statusCountCtxDigitsWidest = %d), never re-spelled per case",
				used, deployed, pair, want, statusCountCtxDigitsWidest)
		}
		parts := strings.Split(pair, statusCtxPairSep)
		if len(parts) != 2 {
			t.Fatalf("used=%d/%d renders %q, which is not a two-sided pair", used, deployed, pair)
		}
		if parts[0] == parts[1] {
			t.Fatalf("used=%d/%d renders the pair %q: two DIFFERENT counters drawn as one figure, "+
				"although the pair's own budget separates them (%q/%q) — the row is stating an "+
				"equality that is false", used, deployed, pair, side(used), side(deployed))
		}
		if w := statusFieldCells(statusCtxField(ops, 0)); w > statusCtxCol {
			t.Errorf("used=%d/%d: the sub-cell is %d cells, over its %d reservation: %q",
				used, deployed, w, statusCtxCol, statusCtxField(ops, 0))
		}
	}
	// AND WHERE THE PAIR'S OWN BUDGET CANNOT SEPARATE THEM the equal spelling STANDS: a
	// pair of counters within half a unit of one another (199999 against a 200000 window:
	// both round to "200k") is honestly stated by that form, and the row does not pretend
	// otherwise. The label must survive beside it — the equal spelling carries no less
	// information than any other form of these two numbers, so it must not be the one
	// that costs the cell naming the fact.
	near := statusOps{ctxUsed: 199990, ctxMax: deployed, ctxMeasured: true}
	if got, want := statusCtxPair(near, 0), side(199990)+statusCtxPairSep+side(deployed); got != want {
		t.Errorf("a pair the budget cannot separate renders %q, want %q", got, want)
	}
	tightField := statusCtxField(near, 0)
	if !strings.Contains(tightField, statusCtxQuantGlyph) {
		t.Errorf("an inseparable pair dropped the denominator's qualifier: %q", tightField)
	}
	if w := statusFieldCells(tightField); w > statusCtxCol {
		t.Errorf("the inseparable pair renders %d cells, over the sub-cell's %d: %q", w, statusCtxCol, tightField)
	}
	// THE LABEL IS NOT PROMISED AT THIS MAGNITUDE, and the reason is arithmetic rather
	// than taste: a pair of two four-cell sides is NINE cells, the qualifier one more,
	// and the sub-cell is twelve — so the composer's FOLD (statusCtxField) drops the
	// label, exactly as it does for any pair the reserved cells cannot hold. What IS
	// promised is asserted here: the qualifier stays (it closes the denominator's
	// ambiguity), and the row never exceeds its reservation.
	//
	// The label's own survival is asserted ONE case away, at the magnitude the operator
	// reads — where the pair is eight cells and the composer closes the gap instead
	// (TestStatusRowCtxLabelSurvivesTheHonestDenominator below).

	// THE PAIR IS NOT WIDER THAN ITS OWN BOUND, for every int the counters can hold —
	// the spelling is reachable, so its width is a contract and not a sample.
	for _, n := range []int{0, 999, 9999, 99999, 100000, 200000, 999999, 1000000,
		statusCountCompactKilo * statusCountCompactKilo * statusCountCompactKilo, 1 << 40, 1<<62 - 1} {
		if got := boundedCountRectDigits(n, statusCountCtxDigitsWidest).cells; got > statusCtxPairSideCellsDrawn {
			t.Errorf("the pair's form renders %d as %q (%d cells), over its per-side bound %d",
				n, boundedCountRectDigits(n, statusCountCtxDigitsWidest).text, got, statusCtxPairSideCellsDrawn)
		}
	}
	if got, want := statusCtxPairCellsDrawn, 2*statusCtxPairSideCellsDrawn+statusCtxPairSepCells; got != want {
		t.Errorf("statusCtxPairCellsDrawn = %d, want %d (two sides + the separator)", got, want)
	}
	// The pair must FIT the sub-cell it rides in — that is the measurement the recovery
	// rests on (it reaches the cells the FOLD leaves, and moves no column's reservation),
	// so it is asserted as arithmetic rather than argued in prose.
	if got := statusCtxPairCellsDrawn + statusCtxQuantCells; got > statusCtxCol {
		t.Fatalf("the drawn pair plus the qualifier is %d cells, over the sub-cell's %d: the "+
			"recovery would have to re-partition the row, which is not what this fix does",
			got, statusCtxCol)
	}
}

// TestStatusRowCtxPairOnTheRenderedRow is the surface half: the pair at the DEPLOYED
// window, on the row the operator reads, at the pinned geometry. It is deliberately
// the same shape as the row is composed in production (setOpsFn + setAvailCols, the
// wiring the REPL installs) rather than a direct call to the composer — the defect
// reached the screen, so the guard is stated at the screen.
func TestStatusRowCtxPairOnTheRenderedRow(t *testing.T) {
	const (
		used = 150052 // the magnitude the operator's own session reached (compaction fires at 150000)
		max  = ctxWindowDeployedForTest
	)
	s := newChatStatusSource(statusProbeModel, true)
	s.update(9, 0, used, "")
	s.setTool("keyword_search", "internal/lineedit/answer.go")
	s.setOpsFn(func() statusOps {
		return statusOps{ctxUsed: used, ctxMax: max, ctxMeasured: true, elapsed: 4 * time.Minute}
	})
	s.setAvailCols(statusColumnsAtCols)
	row := s.status().Line(statusColumnsAtCols)

	pair := statusCtxPair(statusOps{ctxUsed: used, ctxMax: max, ctxMeasured: true}, 0)
	if !strings.Contains(row, pair) {
		t.Fatalf("the pinned row does not carry its pair %q:\nrow: %q", pair, row)
	}
	if parts := strings.Split(pair, statusCtxPairSep); parts[0] == parts[1] {
		t.Errorf("the row's pair %q draws two different counters as one figure:\nrow: %q", pair, row)
	}
	// And the row still carries the qualifier that says WHAT the denominator is (bug F's
	// cell): the recovery must not spend it — that would trade one silent field for
	// another, which is the defect class this row keeps producing.
	if !strings.Contains(row, statusCtxQuantGlyph) {
		t.Errorf("the row dropped the denominator's qualifier %q:\nrow: %q", statusCtxQuantGlyph, row)
	}
	if got := lineedit.DisplayWidth(row); got > statusLayoutBudgetCols {
		t.Errorf("the row is %d cells, over the %d-cell budget: %q", got, statusLayoutBudgetCols, row)
	}
}
