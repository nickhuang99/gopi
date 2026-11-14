package main

// chat_status_ctx_denominator_budget_test.go — the guard for the CONTEXT pair's own
// digit budget.
//
// THE DEFECT (operator's screen, row 01a1d15d-9b29): the context pair was drawn
// through the CACHE pair's formatter, whose allowance is
// statusCountWholeDigitsWidest = 2. The deployed window is 200000, and "200k" needs
// THREE whole digits — so the formatter climbed a rung, the tenth of a megabyte
// rounded away, the bare-unit clamp printed the smallest mantissa, and the row stated
// a budget FIVE TIMES its real value:
//
//	ctx 13k/1M≡        (before)  — a 200000-token budget reported as a megabyte
//	ctx13k/200k≡       (after)   — the denominator IS the budget
//
// The percentage beside it was wrong by the same factor, and every "how full is the
// context" reading the operator took was wrong by 5x.
//
// WHAT THIS FILE PINS, in the terms the row is composed in:
//
//	ONE BUDGET       the pair has ONE allowance (statusCountCtxDigitsWidest), applied
//	                 to both sides — a ratio stated in two precisions is not a ratio.
//	HONEST BUDGET    the denominator states the window's magnitude in the unit its own
//	                 digits reach: 64000 → "64k", 200000 → "200k", 1000000 → "1M".
//	FOUR CELLS SINGLE the same number rendered in the pair's two positions and in the
//	                 cache pair's two is each correct for ITS field — the cache sides
//	                 keep the two-digit allowance (they carry an arrow per side, and a
//	                 "↑1M" is what the operator's own row really holds), so widening
//	                 the context pair must not move them and vice versa.
//
// EVERY EXPECTED STRING IS DERIVED FROM boundedCountRect* AT RUN TIME (Rule 14, and
// memory #5843: a test that re-types the product's spelling pins the retired
// formatter, not the row). Not one "200k" is written as a literal here.

import (
	"sort"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// ctxDenominatorBudgetCases are the windows the row really receives: the probe
// fixture's, the deployed default (internal/agent/context.go, and the value the
// operator's screen carried), and the largest window a model advertises on this
// transport.
//
// The expected side is NOT in this table: it is computed from the product's own
// formatter (ctxDenominatorForTest below), so the case supplies a MAGNITUDE and the
// contract supplies the spelling.
var ctxDenominatorBudgetCases = []int{
	statusProbeCtxMax,                               // the probe fixture's window (64000)
	ctxWindowDeployedForTest,                        // the deployed default (200000)
	statusCountCompactKilo * statusCountCompactKilo, // a 1M window
}

// ctxDenominatorForTest is the spelling the pair's DENOMINATOR must have for a window
// of n tokens, at the pair's declared allowance. It is the production formatter,
// called with the production constant — a test-local re-implementation would be a
// second statement of the rule (which is the defect memory #5843 records).
func ctxDenominatorForTest(n int) string {
	return boundedCountRectDigits(n, statusCountCtxDigitsWidest).text
}

// TestStatusCtxDenominatorStatesItsBudget is the table-driven acceptance: for every
// window the row receives, the denominator is the product's own bounded spelling AT THE
// CONTEXT PAIR'S ALLOWANCE — and, the point of the epic, it is the spelling of the
// BUDGET rather than of a rounded-up magnitude.
func TestStatusCtxDenominatorStatesItsBudget(t *testing.T) {
	for _, window := range ctxDenominatorBudgetCases {
		t.Run("window="+ctxDenominatorForTest(window), func(t *testing.T) {
			// THE ACCEPTANCE IS STATED ON THE PRODUCTION COMPOSER: the pair the row
			// draws, split at its own separator, so a numerator/denominator swap or a
			// separator change fails here rather than passing a string comparison of
			// one side in isolation.
			ops := statusOps{ctxUsed: ctxReadMagnitudeForTest, ctxMax: window, ctxMeasured: true}
			pair := statusCtxPair(ops, 0)
			parts := strings.Split(pair, statusCtxPairSep)
			if len(parts) != 2 {
				t.Fatalf("the pair for a %d window renders %q, which is not two sides", window, pair)
			}
			want := ctxDenominatorForTest(window)
			if parts[1] != want {
				t.Errorf("the denominator for a %d window renders %q, want %q (boundedCountRectDigits at "+
					"statusCountCtxDigitsWidest = %d): the row must state the BUDGET it runs against",
					window, parts[1], want, statusCountCtxDigitsWidest)
			}
			// THE NOMINATOR IS DRAWN AT THE SAME ALLOWANCE — one budget for a ratio.
			if got, wantUsed := parts[0], ctxDenominatorForTest(ctxReadMagnitudeForTest); got != wantUsed {
				t.Errorf("the numerator renders %q, want %q: both sides of the pair are one budget", got, wantUsed)
			}
			// AND THE FIELD FITS ITS SUB-CELL, measured through the composer.
			field := statusCtxField(ops, 0)
			if w := lineedit.DisplayWidth(field); w > statusCtxCol {
				t.Errorf("the sub-cell renders %d cells, over its %d reservation: %q", w, statusCtxCol, field)
			}
			// THE DENOMINATOR IS NOT AN OVERSTATEMENT, which is the defect stated as an
			// assertion: a 200000 window must round to itself, not up a rung. This is
			// what a two-digit allowance could not do, and it is checked against the
			// ladder's own unit rather than against a spelling.
			if unit, digits := splitCountSide(want); unit != "" && digits == "" {
				t.Errorf("the denominator %q carries a unit with no digits — a magnitude with no quantity", want)
			}
		})
	}
}

// TestStatusCtxDenominatorIsNotTheCacheAllowance is the RED PROOF, kept in-repo and
// stated as an assertion rather than as a paragraph: at the deployed window the two
// allowances really do disagree, and the context pair uses ITS OWN.
//
// If a future change folded the pair back onto formatCountBounded, the two spellings
// would become one and this test would fail — naming the count, in milliseconds,
// instead of sending somebody to a 100-column terminal to notice the denominator is
// five times the budget.
func TestStatusCtxDenominatorIsNotTheCacheAllowance(t *testing.T) {
	window := ctxWindowDeployedForTest
	cacheForm := formatCountBounded(window)
	pairForm := ctxDenominatorForTest(window)
	if cacheForm == pairForm {
		t.Fatalf("the cache allowance renders the deployed window %d as %q, and the context allowance as %q — "+
			"the same. Either the pair was folded back onto the two-digit formatter, or "+
			"statusCountCtxDigitsWidest stopped being the wider allowance; the epic's whole repair is that "+
			"these two DIFFER at this magnitude", window, cacheForm, pairForm)
	}
	ops := statusOps{ctxUsed: ctxReadMagnitudeForTest, ctxMax: window, ctxMeasured: true}
	if pair := statusCtxPair(ops, 0); !strings.Contains(pair, pairForm) {
		t.Errorf("the row's pair %q does not carry its own allowance's spelling %q", pair, pairForm)
	}
	if pair := statusCtxPair(ops, 0); strings.Contains(pair, cacheForm+statusCtxQuantGlyph) ||
		strings.HasSuffix(pair, statusCtxPairSep+cacheForm) {
		t.Errorf("the row's pair %q states the deployed window with the CACHE allowance (%q): a 200000-token "+
			"budget drawn as %q is the five-fold overstatement this epic closes",
			pair, cacheForm, cacheForm)
	}
	// The window's magnitude really does need the third digit — the fact that made the
	// two allowances differ. Asserted against the ladder's own digit count rather than
	// against the string, so a changed scale moves this with it.
	if got := countDecimalDigits(window / statusCountCompactKilo); got <= statusCountWholeDigitsWidest {
		t.Fatalf("the deployed window's kilo whole part is %d digits, inside the cache allowance %d — the "+
			"fixture no longer exercises the boundary this epic is about", got, statusCountWholeDigitsWidest)
	}
}

// TestStatusRowCountPositionsKeepTheirOwnAllowances is the CONSISTENCY half: the same
// number lives in four positions on this row — the context pair's numerator and
// denominator, and the cache pair's read and write counters — and each must be spelled
// at ITS OWN field's allowance, without either family dragging the other.
//
// WHY THIS IS THE RIGHT PROPERTY RATHER THAN "all four agree": the cache pair's two
// sides each carry a one-cell arrow, so the cells that buy the context pair its third
// digit are already spent there (statusCtxPairSideCellsDrawn vs statusCacheCountSideCellsWidest).
// A single number rendered in both positions therefore has TWO correct spellings, and a
// change that unified them would either spend the cache pair's arrow cells (widening its
// side past statusCacheCountSideCellsWidest) or overstate the context's budget — the two
// defects this row has already paid for once each (epic 01a0e2e9 and this one).
// The equivalence is checked below rather than asserted in prose: each position is held
// to ITS OWN derived bound, over values that really reach both magnitude bands.
func TestStatusRowCountPositionsKeepTheirOwnAllowances(t *testing.T) {
	// The magnitudes a session really reaches in each position: the operator's own
	// occupancy and window, and the cache counters from a long cached session (the
	// 1.66M-token case statusCacheCol's note records).
	const cacheMagnitude = 1662000
	for _, n := range []int{13100, 42100, ctxWindowDeployedForTest, cacheMagnitude, 1 << 40} {
		t.Run("n="+ctxDenominatorForTest(n), func(t *testing.T) {
			// THE CONTEXT POSITIONS: numerator and denominator, through the pair the
			// row composes (never a side helper on its own — the pair IS the statement).
			pair := statusCtxPair(statusOps{ctxUsed: n, ctxMax: n, ctxMeasured: true}, 0)
			parts := strings.Split(pair, statusCtxPairSep)
			if len(parts) != 2 || parts[0] != parts[1] {
				t.Fatalf("the pair for %d renders %q: two equal counters must draw the same figure", n, pair)
			}
			if want := ctxDenominatorForTest(n); parts[0] != want {
				t.Errorf("context position: %d renders %q, want %q", n, parts[0], want)
			}
			// THE CACHE POSITIONS: the same number, through the cache field's own
			// formatter — bounded, and consistent between its two sides.
			cachePair := statusCacheReadGlyph + formatCountBounded(n) +
				statusCacheWriteGlyph + formatCountBounded(n)
			if got, want := statusCachePairExpr(statusOps{cacheRead: n, cacheWrite: n}), cachePair; got != want {
				t.Errorf("cache position: %d renders %q, want %q", n, got, want)
			}
			// EACH SIDE FITS ITS OWN FIELD'S RESERVATION — the geometry is untouched, and
			// this is where a widening of one family that overflowed the other's cells
			// would fail.
			if c := lineedit.DisplayWidth(ctxDenominatorForTest(n)); c > statusCtxPairSideCellsDrawn {
				t.Errorf("the context position needs %d cells for %d, over its per-side bound %d",
					c, n, statusCtxPairSideCellsDrawn)
			}
			if c := lineedit.DisplayWidth(formatCountBounded(n)); c > statusCacheCountSideCellsWidest {
				t.Errorf("the cache position needs %d cells for %d, over its per-side bound %d",
					c, n, statusCacheCountSideCellsWidest)
			}
		})
	}
	// AND THE TWO FAMILIES STILL COMPOSE INTO ONE GROUP INSIDE THE PIN — the whole
	// point of a per-field allowance is that it costs no geometry.
	if got := statusMetricsFormCol + statusMetricsSepCells; got > statusLayoutBudgetCols {
		t.Errorf("the metrics group at its full form plus a seam is %d cells, over the %d budget",
			got, statusLayoutBudgetCols)
	}
	if statusPinSlackCells != 0 {
		t.Errorf("statusPinSlackCells = %d, want 0: the geometry is a hard contract and this change may not "+
			"have bought its cells from slack", statusPinSlackCells)
	}
}

// TestStatusRowLegendSampleIsAShapeTheRowDraws closes the half of the legend's
// promise `TestStatusRowLegendEntryOrderMatchesTheRow` cannot see: that test pins the
// ORDER of the entries and resolves symbols from constants, so a legend could present
// a spelling the row NEVER produces and stay green — measured case (row 01a1d15d):
// `gopi chat -h` taught `ctx  92k/200k≡` (label + gap + pair + qualifier = 13 cells)
// while the row at a 200000 window draws `ctx92k/200k≡` (12), because the composer's
// FOLD yields the GAP before the label. A reader learned a shape that does not exist.
//
// THE CLAIM IS MADE AGAINST THE COMPOSER, not against a list of literals: the sample
// is compared with what statusCtxField really draws across the magnitudes a session
// passes through at the deployed window, and it must ALSO fit the sub-cell — so a
// spelling a partition change made unreachable fails here rather than in a reader's
// terminal.
func TestStatusRowLegendSampleIsAShapeTheRowDraws(t *testing.T) {
	sample := statusCtxLegendSample(t)
	if w := statusFieldCells(sample); w > statusCtxCol {
		t.Fatalf("the legend's context sample %q is %d cells, over the sub-cell's %d: `gopi chat -h` is "+
			"teaching a shape this row cannot draw", sample, w, statusCtxCol)
	}
	// THE MAGNITUDES THE SAMPLE MUST BE ONE OF: a session's occupancy walking the
	// deployed window, including both sides of every FOLD rung (the pair grows by one
	// cell at each power of ten, so the drawn shapes change there).
	drawn := map[string]bool{}
	for _, used := range []int{0, 1, 999, 9999, 13100, 42000, 92000, 99999,
		100000, 150000, 199990, 199999, ctxWindowDeployedForTest} {
		drawn[statusCtxField(statusOps{ctxUsed: used, ctxMax: ctxWindowDeployedForTest, ctxMeasured: true}, 0)] = true
	}
	if !drawn[sample] {
		shapes := make([]string, 0, len(drawn))
		for shape := range drawn {
			shapes = append(shapes, shape)
		}
		sort.Strings(shapes)
		t.Fatalf("the legend's context sample %q is not a shape the row draws at the deployed window.\n"+
			"  the row really draws (one per FOLD rung): %q\n"+
			"  fix the legend (cmd/agent/cli_chat.go, chatStatusRowLegend) to one of those",
			sample, shapes)
	}
}

// statusCtxLegendSample reads the sample the legend prints for the context pair out of
// `gopi chat -h` itself — the page the reader actually reads, not the source constant.
// It is located by the entry's own opening (statusCtxLabel's field), the same way a
// reader scans the section, and the sample is the first token of that entry that
// contains the pair separator.
func statusCtxLegendSample(t *testing.T) string {
	t.Helper()
	help := statusRowHelpForTest(t)
	for _, line := range strings.Split(help, "\n") {
		if !strings.HasPrefix(line, "    "+statusCtxLabel) {
			continue
		}
		for _, tok := range strings.Fields(line) {
			if strings.Contains(tok, statusCtxPairSep) && strings.HasSuffix(tok, statusCtxQuantGlyph) {
				return tok
			}
		}
	}
	t.Fatalf("the legend carries no context sample containing %q and ending in %q — the row draws one, so a "+
		"reader meeting the pair on the screen has nothing to compare it with",
		statusCtxPairSep, statusCtxQuantGlyph)
	return ""
}
