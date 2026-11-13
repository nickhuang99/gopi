package main

import (
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// chat_status_ctx_provenance_test.go — the re-partition's PROVENANCE contract
// (task 01a0dcd1): the shared status row carries the ABSOLUTE context read-out
// and NO money field.
//
// Two claims, and both are stated as properties of the composed CELLS rather
// than as prose about a string:
//
//  1. NO MONEY, at ANY width — not "the money field is absent at the pin", but
//     "no cell of any row this geometry can compose spells a price". The proof is
//     negative on purpose: the money sub-cell was retired, and a retirement that
//     is only asserted at one width is a claim a wider terminal could falsify.
//
//  2. THE ABSOLUTE PAIR, wherever the context read-out reaches the screen — the
//     numerator and its denominator together ("42.1k/64.0k"), with the percentage
//     derived beside it. The operator's criterion is the reason (see statusCtxCol's
//     note): a bare "6%" means 12k on a 200k window and 60k on a 1M one, and the
//     window is CONFIGURATION, so a read-out that cannot state its denominator
//     cannot survive a model change.
//
// WHY A SWEEP RATHER THAN A FEW WIDTHS: the ladder composes a different form per
// width, and the unaligned fallback is a different composition again. A claim
// about "the row" that held only at the pin would be about one of those forms.

// statusRowWidthsUnderProof is every width the provenance sweep renders: the
// budget, the pin, every cell down to the narrowest terminal the row is offered
// on, and the widths the ladder's own steps are defined at. It is built from the
// production constants (never a hand-kept list) so a geometry change moves the
// sweep with it.
func statusRowWidthsUnderProof() []int {
	widths := []int{statusLayoutBudgetCols, statusColumnsAtCols}
	for w := statusColumnsAtCols - 1; w >= statusWidthMin; w-- {
		widths = append(widths, w)
	}
	return widths
}

// TestStatusRowCarriesNoMoneyAtAnyWidth is the retirement's own guard: the row
// this geometry composes spells no price, in no form, at no width.
//
// The glyphs are LITERALS, and that is the point rather than a Rule 1 violation:
// the production constants (statusCostCol, statusCostCurrency,
// statusCostEstimatedMarker, statusCostField) were DELETED with the sub-cell, so a
// test that referenced them would not compile — and a test that referenced
// something else live could not state "this rendering is gone". What is asserted
// is the ABSENCE of the rendering, so the subject has to be spelled out.
func TestStatusRowCarriesNoMoneyAtAnyWidth(t *testing.T) {
	s := statusDegradeState(t)
	// Prices, in every shape this row ever printed one: the currency glyph, the
	// estimate marker, and the bare amount forms the retired field could compose
	// ("~0.00元", "0.12元", "12.34元").
	moneyForms := []string{"元", "¥", "€", "$", "~0.00", "0.12元", "12.34元"}
	for _, cols := range statusRowWidthsUnderProof() {
		s.setAvailCols(cols)
		row := s.status().Line(cols)
		for _, form := range moneyForms {
			if strings.Contains(row, form) {
				t.Fatalf("cols=%d: the row spells a price (%q) — the money sub-cell is RETIRED and the "+
					"row shows only measured read-outs\nrow: %q", cols, form, row)
			}
		}
	}
	// And the UNALIGNED composition too, which is a different string built from a
	// different branch of status(): below the ladder's floor the row is the label,
	// and the retirement has to hold there as well.
	for _, cols := range []int{statusWidthMin, 20, 24, 30, 40} {
		s.setAvailCols(cols)
		label := s.status().Label
		for _, form := range moneyForms {
			if strings.Contains(label, form) {
				t.Fatalf("cols=%d: the UNALIGNED label spells a price (%q): %q", cols, form, label)
			}
		}
	}
}

// TestStatusRowCtxReadOutIsAbsolute pins the Hermes shape on the CELLS: wherever
// the context read-out reaches the row, its numerator and denominator arrive
// TOGETHER, and the percentage is a separate, derivable fact beside them.
//
// The assertion reads the rendered row rather than the helper, because the helper
// agreeing with itself proves nothing about what the terminal shows: this is the
// surface the operator reads.
func TestStatusRowCtxReadOutIsAbsolute(t *testing.T) {
	const (
		pairUsed = 42100
		pairMax  = 64000
	)
	s := newChatStatusSource(statusProbeModel, true)
	s.update(9, 0, pairUsed, "")
	s.setTool("keyword_search", "internal/lineedit/answer.go")
	s.setOpsFn(func() statusOps {
		return statusOps{ctxUsed: pairUsed, ctxMax: pairMax, ctxMeasured: true,
			elapsed: 4 * time.Minute, elapsedKnown: true}
	})
	pair := formatCountBounded(pairUsed) + statusCtxPairSep + formatCountBounded(pairMax)
	labeled := statusCtxLabel + statusCtxBarGap + pair
	pctOf := func(used, max int) string { return statusCtxPct(used, max) }

	// The aligned geometry at its pin: the pair is on the row, LABELLED (task
	// 01a0ddab), and no percentage is — the FOLD, and the two claims are one
	// contract from opposite sides. The label is what tells this pair apart from
	// the cache pair of the same unit; the percentage is the derived restatement
	// of two numbers the row already draws, which is why it does not earn cells.
	s.setAvailCols(statusColumnsAtCols)
	row := s.status().Line(statusColumnsAtCols)
	if !strings.Contains(row, labeled) {
		t.Fatalf("the pinned row does not carry the LABELLED absolute context pair %q — an unlabelled "+
			"pair beside the cache pair of the same unit is the read-out the operator could not identify\nrow: %q",
			labeled, row)
	}
	// The label LEADS its number: the label's cells are the sub-cell's first cells,
	// and the pair starts exactly statusCtxLabelCells+statusCtxBarGapCells later —
	// the row's words precede the number they name, never trail it.
	if got, want := fieldStart(row, labeled), fieldStart(row, pair); got < 0 || want != got+statusCtxLabelCells+statusCtxBarGapCells {
		t.Errorf("the label does not immediately precede the pair it names at %d cells' distance: %q starts "+
			"at cell %d and %q at %d\nrow: %q", statusCtxLabelCells+statusCtxBarGapCells, labeled, got, pair, want, row)
	}
	if pct := pctOf(pairUsed, pairMax); strings.Contains(row, pct) {
		t.Errorf("the pinned row still carries the derived percentage %q: the FOLD (task 01a0ddab) took it "+
			"off the aligned row because both of its operands are already drawn there\nrow: %q", pct, row)
	}
	// The percentage's ABSENCE is asserted at EVERY width the aligned geometry can
	// produce — a claim about one width is a claim a narrower terminal can falsify.
	for cols := statusColumnsAtCols; cols >= statusWidthMin; cols-- {
		narrow := s.status().Line(cols)
		if !strings.Contains(narrow, statusCtxLabel) {
			continue // the group is shed at this width; there is no sub-cell to be free of one.
		}
		if strings.Contains(narrow, pctOf(pairUsed, pairMax)) {
			t.Errorf("cols=%d: a context percentage is on the row: %q", cols, narrow)
		}
	}

	// A DIFFERENT WINDOW, the same used count: the pair changes, so the row states
	// which denominator belongs to which numerator. This is the operator's criterion
	// stated as a test — "what if the model changes?" — and it is the assertion a
	// ratio-only read-out fails: it would print the same "65%" for both.
	bigMax := 1000000
	s.setOpsFn(func() statusOps {
		return statusOps{ctxUsed: pairUsed, ctxMax: bigMax, ctxMeasured: true,
			elapsed: 4 * time.Minute, elapsedKnown: true}
	})
	wideRow := s.status().Line(statusColumnsAtCols)
	widePair := formatCountBounded(pairUsed) + statusCtxPairSep + formatCountBounded(bigMax)
	if !strings.Contains(wideRow, widePair) {
		t.Fatalf("a 1M-token window does not reach the row: the pair %q is missing, so a percentage "+
			"would be a percentage of an unknown window\nrow: %q", widePair, wideRow)
	}
	// The 1M window is also the boundary the pair's reservation stops at: its sides
	// are 6 and 7 cells ("999.9k/1000.0k", 14), one cell past statusCtxPairCells, so
	// this is the case the LABEL yields on — the pair is drawn WHOLE (a cut number
	// is the one thing the sub-cell must never print) and the sub-cell stays inside
	// its reservation.
	if w := lineedit.DisplayWidth(statusCtxField(statusOps{ctxUsed: pairUsed, ctxMax: bigMax}, 0)); w > statusCtxCol {
		t.Errorf("the 1M-window sub-cell renders %d cells, over its %d-cell reservation: %q", w, statusCtxCol,
			statusCtxField(statusOps{ctxUsed: pairUsed, ctxMax: bigMax}, 0))
	}
	// Scoped to the metrics group's cells: the tool column may elide its own
	// digest inside its reservation by design (statusToolColumnFieldFor), and a
	// row-wide test would read that unrelated cut as this pair being truncated.
	if at := fieldStart(wideRow, statusCtxLabel); at >= 0 {
		runes := []rune(wideRow)
		if at < len(runes) && strings.Contains(string(runes[at:]), "…") {
			t.Errorf("the 1M-window pair is CUT on the row: %q — a truncated denominator is worse than no label", wideRow)
		}
	}
	if strings.Contains(wideRow, pctOf(pairUsed, pairMax)) {
		t.Errorf("the row still reports the OLD window's percentage %q beside the new denominator\nrow: %q",
			pctOf(pairUsed, pairMax), wideRow)
	}

	// And the unaligned fallback carries the same pair, from the same helper: the
	// criterion is about the READ-OUT, not about which composition is on screen.
	s.setAvailCols(statusWidthMin)
	if label := s.status().Label; !strings.Contains(label, widePair) {
		t.Errorf("the unaligned label drops the absolute pair %q — below the ladder's floor the read-out "+
			"must still name its denominator\nlabel: %q", widePair, label)
	}
}

// TestStatusMetricsFieldsAreDrawnWithoutMovement is the cell-grid half: the
// re-partition must not have introduced a relayout. The group's sub-cells are
// drawn from the SAME reservations at every width where the group survives, so
// the context pair's start cell is the group's own first cell — measured on the
// rendered row, in display CELLS.
func TestStatusMetricsFieldsAreDrawnWithoutMovement(t *testing.T) {
	s := statusDegradeState(t)
	// The read-out the degrade state's own counters compose — the group's FIRST
	// sub-cell. Since task 01a0f8c9 that is the CONTEXT pair (qualified by its
	// denominator), and the state below therefore carries a measured window: the
	// hit rate no longer leads the group, so a state with only cache counters would
	// compose no leading read-out at all and this case would be measuring the wrong
	// thing. The cache counters are still set, so the ROW the sweep reads is the
	// pinned one (the group draws both sub-cells).
	readOut := statusCtxField(statusOps{
		ctxUsed: statusProbeCtxUsed, ctxMax: statusProbeCtxMax, ctxMeasured: true,
		cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite,
	}, 0)
	// The CACHE sub-cell's own value is the pair plus the hit rate riding it, so the
	// fragment this loop anchors on is the PAIR (statusCachePairExpr) — the part of
	// that value whose position is a contract, and one that no other sub-cell draws.
	cachePair := statusCachePairExpr(statusOps{cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite})
	if readOut == "" {
		t.Fatalf("the degrade state's own counters compose no read-out — the fixture is wrong")
	}

	// The group's reserved start cell: the columns before it plus their
	// separators, read from the same constants the geometry builds from.
	groupStart := statusModelCol + statusTurnCol + statusToolCol +
		statusColumnSepTotalN*lineedit.StatusColumnSepCells

	seen := 0
	for cols := statusColumnsAtCols; cols >= statusWidthMin; cols-- {
		s.setAvailCols(cols)
		row := s.status().Line(cols)
		if !strings.Contains(row, readOut) {
			continue
		}
		seen++
		// The sub-cell's FIRST cells are the read-out's own first cells, so the
		// group's origin is where that read-out starts — the defect this loop
		// exists to catch is a value that MOVED the group, and this is the anchor
		// that would show it.
		if got := fieldStart(row, readOut); got != groupStart {
			t.Fatalf("cols=%d: the labelled context read-out starts at cell %d, want the group's reserved cell %d — a "+
				"value moved the group\nrow: %q", cols, got, groupStart, row)
		}
		// The cache pair, when it is present, sits INSIDE the group's cells and to
		// the right of the context read-out: the group's order is the degradation
		// policy (a tail cut reaches the cache pair first), so the order is a
		// contract rather than a coincidence.
		if strings.Contains(row, cachePair) {
			if got := fieldStart(row, cachePair); got <= groupStart || got >= groupStart+statusMetricsCol {
				t.Fatalf("cols=%d: the cache pair starts at cell %d, outside the group's cells [%d,%d)\nrow: %q",
					cols, got, groupStart, groupStart+statusMetricsCol, row)
			}
		}
	}
	if seen == 0 {
		t.Fatalf("the sweep never saw the group's read-out on the row: the provenance proof measured nothing")
	}
	t.Logf("the metrics read-out held cell %d across %d widths (pin %d down to the narrowest aligned form)",
		groupStart, seen, statusColumnsAtCols)
}
