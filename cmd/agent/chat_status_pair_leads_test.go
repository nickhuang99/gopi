package main

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// TestStatusMetricsGroupLeadsWithTheCtxPairAndRidesTheRateOnTheCacheSubCell is the
// CELL-ADDRESSED half of the re-partition's guard (task 01a0f8c9, [2/4]): it drives
// the REAL composer at the pinned width and states, in the row's own cells, the two
// facts the re-partition is:
//
//	(1) the metrics group's FIRST cells are the CONTEXT sub-cell's own qualified form
//	    — the sub-cell begins exactly on the group's first cell, and what fills those
//	    cells is the production composer's answer for this state (label + pair +
//	    quantifier), never a rendering this file spells;
//	(2) the labelled hit rate is IN the CACHE sub-cell — its own cell range lies
//	    inside the cache sub-cell's cells, to the right of the group's first cell,
//	    riding the two counters it is computed from.
//
// WHY BESIDE THE EXISTING PAIR-AND-RATE CASE. TestStatusRowKeepsBothThePairAndTheRate
// asserts the facts are PRESENT and ORDERED (the pair, then the rate, with the cache
// transport between them); it says nothing about WHICH CELLS either fact occupies, so
// it is satisfied by a row that leads the group with the rate's own field and states
// the pair later — the exact inverse of the re-partition. This case is addressed at
// the geometry instead: the group's first cell and the cache sub-cell's first cell
// are DERIVED from the production constants (statusCtxCol, statusMetricsSepCells,
// statusMetricsFormCol) and the two composers are asked what they draw, so a lead
// that regressed, or a rate that drifted out of the cache sub-cell's cells, is a
// failure that names the cell it moved to.
//
// Both expected values come from the production composers — statusCtxField/
// statusCtxPair for the context sub-cell and statusCacheField (with the pair trimmed
// off for the rate) for the cache sub-cell — so this file carries no second spelling
// of the row's own composition.
func TestStatusMetricsGroupLeadsWithTheCtxPairAndRidesTheRateOnTheCacheSubCell(t *testing.T) {
	ops := statusOps{
		ctxUsed: statusRowProbeCtxUsed, ctxMax: statusRowProbeCtxMax, ctxMeasured: true,
		cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite,
	}
	// THE REAL COMPOSER at the pinned width: the same source the status line paints
	// from, with both sub-cells' counters measured — the state the re-partition is
	// about (a session with a window AND a prompt cache).
	row := statusProbeRowWithCache(t, statusColumnsAtCols, statusProbeCacheRead, statusProbeCacheWrite)

	// THE GEOMETRY, from the row's own constants: where the metrics group starts
	// (the columns before it plus one separator per join) and where its second
	// sub-cell starts (the first sub-cell's reservation plus one seam).
	groupStart := statusModelCol + statusTurnCol + statusToolCol +
		statusColumnSepTotalN*lineedit.StatusColumnSepCells
	cacheSubCellStart := groupStart + statusCtxCol + statusMetricsSepCells
	groupEnd := groupStart + statusMetricsFormCol
	if !(groupStart < cacheSubCellStart && cacheSubCellStart < groupEnd) {
		t.Fatalf("the derived geometry is not ordered: group [%d,%d), cache sub-cell from %d — the constants "+
			"this case addresses the row by have drifted", groupStart, groupEnd, cacheSubCellStart)
	}

	// ── ARM 1: the group's first cells are the CONTEXT sub-cell's qualified form ──
	ctxField := statusCtxField(ops, ops.ctxUsed)
	pair := statusCtxPair(ops, ops.ctxUsed)
	if ctxField == "" || pair == "" {
		t.Fatalf("the fixture composes no context read-out (%q / %q) — the case itself is wrong", ctxField, pair)
	}
	// The QUALIFIED form: the pair's own denominator carries the cell that says what
	// the denominator is, and this sub-cell is what leads the group.
	if !strings.Contains(ctxField, pair) {
		t.Fatalf("the context sub-cell %q does not carry its own pair %q", ctxField, pair)
	}
	if !strings.Contains(ctxField, statusCtxQuantGlyph) {
		t.Fatalf("the context sub-cell %q carries an UNQUALIFIED denominator: the qualifier %q is what the "+
			"re-partition kept beside the pair (statusCtxField)", ctxField, statusCtxQuantGlyph)
	}
	// It is the PAIR that leads, not the rate: the rate's own field inside this
	// sub-cell is the pre-re-partition shape (task 01a0f8c9's defect — the row
	// painted "hit 75.4% ·↑2M↓1M", the pair evicted).
	if strings.Contains(ctxField, statusHitRateLabel) {
		t.Fatalf("the context sub-cell %q carries the hit rate's own fact name %q: the rate leads the group "+
			"again, which is the defect the re-partition exists to close", ctxField, statusHitRateLabel)
	}
	// THE CELL ADDRESS: the sub-cell's own text begins on the group's FIRST cell.
	if got := fieldStartInTest(row, ctxField); got != groupStart {
		t.Fatalf("the metrics group's first cells are not the context sub-cell %q: it starts at cell %d, want the "+
			"group's own first cell %d\n  row: %q", ctxField, got, groupStart, row)
	}
	// ...and the pair is inside the sub-cell's own cells — never a cut number, never
	// a number outside the reservation it was composed for.
	pairStart := fieldStartInTest(row, pair)
	if pairStart < groupStart || pairStart+lineedit.DisplayWidth(pair) > groupStart+statusCtxCol {
		t.Fatalf("the pair %q sits at cells %d..%d, outside the context sub-cell's [%d,%d)\n  row: %q",
			pair, pairStart, pairStart+lineedit.DisplayWidth(pair), groupStart, groupStart+statusCtxCol, row)
	}

	// ── ARM 2: the labelled rate is in the CACHE sub-cell ────────────────────────
	// Both expected strings are the cache sub-cell's OWN composition: the field the
	// row draws there, and — splitting the pair off it — the labelled rate riding its
	// value. No concatenation is spelled here; the composer answers.
	cacheField := statusCacheField(ops)
	rate := strings.TrimPrefix(cacheField, statusCachePairExpr(ops))
	if cacheField == "" || rate == cacheField {
		t.Fatalf("the cache sub-cell composes %q, which does not carry the rate beyond the pair %q — the case "+
			"itself is wrong", cacheField, statusCachePairExpr(ops))
	}
	if !strings.Contains(rate, statusHitRateLabel) {
		t.Fatalf("the cache sub-cell's value %q carries a rate with no fact name: the labelled form is the one "+
			"the row is allowed to print beside a two-sided pair", cacheField)
	}
	// The cache sub-cell's value begins on the cache sub-cell's first cell.
	if got := fieldStartInTest(row, cacheField); got != cacheSubCellStart {
		t.Fatalf("the cache sub-cell's read-out %q starts at cell %d, want the sub-cell's own first cell %d "+
			"(group %d + ctx sub-cell %d + one seam %d)\n  row: %q",
			cacheField, got, cacheSubCellStart, groupStart, statusCtxCol, statusMetricsSepCells, row)
	}
	// The RATE's own cells lie INSIDE the cache sub-cell's range — right of its first
	// cell, and no further than the group's own drawn width allows (the rate's cells
	// are the group's overhang, statusMetricsFormCol - statusMetricsCol).
	rateStart := fieldStartInTest(row, rate)
	if rateStart < 0 {
		t.Fatalf("the pinned row does not carry the labelled rate %q at all: the rate must yield only where the "+
			"cells force a choice, and never silently\n  row: %q", rate, row)
	}
	if rateStart < cacheSubCellStart+lineedit.DisplayWidth(statusCachePairExpr(ops)) {
		t.Fatalf("the rate %q starts at cell %d, inside the pair's own cells (the pair runs to cell %d): the rate "+
			"rides the counters it is computed from\n  row: %q", rate, rateStart,
			cacheSubCellStart+lineedit.DisplayWidth(statusCachePairExpr(ops)), row)
	}
	if rateStart+lineedit.DisplayWidth(rate) > groupEnd {
		t.Fatalf("the rate %q runs to cell %d, past the group's drawn width %d — the row's geometry reserves no "+
			"cell for it there\n  row: %q", rate, rateStart+lineedit.DisplayWidth(rate), groupEnd, row)
	}
	// THE ORDER, stated at the cells: the pair leads the rate, and the rate is to the
	// right of the group's first cell — the two arms of this case are one assertion
	// about the row's left-to-right reading.
	if pairStart >= rateStart {
		t.Fatalf("the pair sits at cell %d and the rate at %d: the RATE leads the group's read-out\n  row: %q",
			pairStart, rateStart, row)
	}
	t.Logf("pin row: %d cells (group [%d,%d), cache sub-cell from %d)", lineedit.DisplayWidth(row),
		groupStart, groupEnd, cacheSubCellStart)
	t.Logf("lead: %-18q at cell %d | rate: %-12q at cell %d", ctxField, groupStart, rate, rateStart)
	t.Logf("row: %q", row)
}
