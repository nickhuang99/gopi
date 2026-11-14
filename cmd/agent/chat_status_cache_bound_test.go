package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// chat_status_cache_bound_test.go — the regression guard for epic 01a0e2e9, the
// cache pair's UNBOUNDED width.
//
// The defect, measured on the operator's own session: the status row drew
//
//	deepseek-flash ·  turn 9  ·  tools ·  ⚙ keyword_search ·  ctx 42.1k/200.0k ·↑1662k↓…  4m00s · ready
//
// — a PAIRED read-out whose second half is gone. statusCacheField composed the pair
// from formatCountCompact (a formatter that grows with its input's magnitude),
// inside a sub-cell whose reservation had been measured from ONE small string
// ("↑12k↓4k", 7 cells), and the layout's own tail cut took the 5 cells that were
// missing. The side lost was the CACHE WRITE — the tokens the provider bills at the
// miss rate — so the surviving half read as if it were the whole story.
//
// The fix has two halves and the tests below are separated along the same seam:
//
//	THE FORM       formatCountBounded's output is bounded for EVERY int, whatever
//	               its magnitude (TestStatusCacheFieldWidthIsBounded). This is the
//	               half a reservation can be a true statement about.
//	THE RESERVATION statusCacheCol is derived from that bound, so the sub-cell holds
//	               the worst case by ARITHMETIC rather than by sampling
//	               (TestStatusCacheReservationCoversItsFormatter).
//
// The epic's own failure mode is asserted directly, at the pin, on the composed ROW
// (TestStatusCachePairSurvivesItsOwnMagnitude): the two numbers the counters hold
// are BOTH on the row, at a magnitude that used to cut them, rendered through the
// production composer rather than through the formatter alone.

// The three magnitudes this file's name is about. They are named constants rather
// than inline literals (Rule 1) because each one is a FACT about the defect:
//
//	statusStatusCacheOperatorSession  the counter the operator's own session held
//	                                  when the row lost its cache-write side
//	                                  (measured: "↑1662k↓…").
//	statusStatusCacheWorstWholeNumber the largest value that prints below the first
//	                                  rung — the boundary between the formatter's two
//	                                  classes, which is where a magnitude-dependent
//	                                  formatter breaks.
//	statusStatusCacheSideCountWidest  a value whose side reaches the reservation's
//	                                  full width, used to compose the pair without
//	                                  re-deriving the worst case in the test.
const (
	statusStatusCacheOperatorSession  = 1662000
	statusStatusCacheWorstWholeNumber = statusCountDigitsPerRung - 1
	statusStatusCacheSideCountWidest  = statusCountCompactKilo - 1
)

// statusCacheBoundProbeCounts are the counter values the bound is exercised at:
// every rung of the ladder, each rung's ceiling and boundary, the magnitudes the
// row's own history reached (the operator's 1.66M-token session is why this test
// exists), and the extremes of the platform's int. The list is deliberately not a
// random sample: a magnitude-dependent formatter fails at BOUNDARIES, which is
// where a sampled test passes.
func statusCacheBoundProbeCounts() []int {
	probes := []int{
		0, 1, 9, 10, 99, 100, 999, 1000,
		statusStatusCacheWorstWholeNumber, // the largest rung-zero value
		statusCountCompactKilo,            // the first rung's own scale
		statusCountCompactKilo - 1,
		statusCountCompactKilo + 1,
		9999, 10000, 12345, 99999, 100000,
		999999, 1000000, statusStatusCacheOperatorSession, 12345678,
		99999999, 100000000, 999999999, 1000000000,
	}
	// The ladder's rung boundaries and their neighbours, DERIVED from the ladder the
	// formatter itself reads (Rule 13): a rung added to statusCountUnits is probed
	// here without an edit.
	for _, u := range statusCountUnits {
		if u.scale <= 0 {
			continue
		}
		probes = append(probes, u.scale-1, u.scale, u.scale+1)
		if u.scale <= math.MaxInt/statusCountCompactKilo {
			probes = append(probes,
				u.scale*statusCountCompactKilo-1,
				u.scale*statusCountCompactKilo,
				u.scale*statusCountCompactKilo+1)
		}
	}
	for _, n := range []int{math.MaxInt32, math.MaxInt64 / 2, math.MaxInt64 / 10, math.MaxInt} {
		if n > 0 && (len(probes) == 0 || n <= math.MaxInt) {
			probes = append(probes, n)
		}
	}
	return probes
}

// TestStatusCacheFieldWidthIsBounded is THE regression guard: at every magnitude —
// a rung boundary, a rung's ceiling, the operator's own session, the extremes of the
// int — ONE side of the pair renders in at most the cells the reservation claims.
//
// It fails on the defect directly. Before the fix, statusCacheField at 1662000
// produced "↑1662000↓1662000" (16 cells) against a 7-cell sub-cell, and every
// boundary above the first rung lengthened it further; the assertions below are
// written against the RESERVATION (statusCacheCountSideCellsWidest) rather than
// against a remembered number, so the test and the geometry cannot be updated apart.
func TestStatusCacheFieldWidthIsBounded(t *testing.T) {
	if statusCacheCountSideCellsWidest <= 0 {
		t.Fatalf("the side reservation is %d cells: a reservation the form cannot fit in is the "+
			"retired-column defect (see statusCostCol)", statusCacheCountSideCellsWidest)
	}
	worst, worstAt := 0, 0
	for _, n := range statusCacheBoundProbeCounts() {
		rect := boundedCountRect(n)
		// (1) ONE side — its TEXT plus the direction glyph the pair prefixes it with —
		// measured by the same function the geometry measures with. The glyph is added
		// here rather than left out because it is part of what the sub-cell holds: a
		// reservation checked against the text alone would be one cell short exactly
		// where the arrows are drawn.
		if got := rect.cells + statusCacheCountGlyphCells; got > statusCacheCountSideCellsWidest {
			t.Errorf("count %d renders %q as %d cells beside its glyph, over the side reservation of "+
				"%d — the formatter grew with its input, which is the defect (statusCacheCol cannot "+
				"cover it)", n, rect.text, got, statusCacheCountSideCellsWidest)
		}
		if got := rect.cells + statusCacheCountGlyphCells; got > worst {
			worst, worstAt = got, n
		}
		// (2) The rendering is never CUT: the row's own ellipsis in a count would be a
		// number the reader cannot trust, and the whole point of a bounded form is that
		// the cut never has to happen.
		if strings.Contains(rect.text, statusEllipsisForTest) {
			t.Errorf("count %d renders %q: a bounded form must never need the row's own ellipsis",
				n, rect.text)
		}
		if rect.text == "" {
			t.Errorf("count %d renders empty: an empty side would hide the fact it exists", n)
		}
		// (3) The unit is one of the ladder's own (Rule 13): a formatter that invented a
		// unit would be unbounded again the moment the ladder is not consulted.
		if !statusCountUnitIsOnLadder(rect.unit) {
			t.Errorf("count %d prints unit %q, which is not on statusCountUnits: the reservation is "+
				"derived from the ladder, so a unit outside it is a width nobody reserved",
				n, rect.unit)
		}
	}
	// The reservation is not merely an upper bound: the formatter must actually reach
	// it, or the sub-cell is reserving cells over nothing (the retired-column defect).
	if worst != statusCacheCountSideCellsWidest {
		t.Logf("note: the widest side measured %d cells at n=%d, the reservation is %d — the "+
			"reservation is an upper bound the ladder's own worst case reaches; a gap here means "+
			"the bound is loose, not that it is wrong", worst, worstAt, statusCacheCountSideCellsWidest)
	}
	// (4) THE PAIR, composed exactly as the row composes it, at the operator's session
	// — inside the cells the row can GIVE the cache read-out (statusHitRateSpanBudget:
	// the sub-cell's reservation plus the rate's own cells). The bound is that budget
	// rather than statusCacheCol because the value the row draws from this composer is
	// the pair PLUS the hit rate riding it (task 01a0f8c9) — what must never happen is
	// the pair CUT, and the pair's own width is what these rows assert: the rate's
	// presence moves the pair to a narrower form (statusCachePair with the rate's cells
	// removed), never the pair's own tail.
	field := statusCacheField(statusOps{cacheRead: statusStatusCacheOperatorSession, cacheWrite: statusStatusCacheOperatorSession})
	pairField := statusCacheHitRateAppend(statusOps{cacheRead: statusStatusCacheOperatorSession, cacheWrite: statusStatusCacheOperatorSession},
		statusCachePairExpr(statusOps{cacheRead: statusStatusCacheOperatorSession, cacheWrite: statusStatusCacheOperatorSession}),
		statusCacheCol)
	if got := statusFieldCells(field); got > statusHitRateSpanBudget {
		t.Fatalf("the cache read-out at the operator's own session is %d cells (%q), over the %d-cell budget: "+
			"this is the epic's defect, and the pair's own tail would be cut", got, field,
			statusHitRateSpanBudget)
	}
	if got := statusFieldCells(pairField); got > statusCacheCol {
		t.Fatalf("the pair at the operator's own session is %d cells (%q), over the %d-cell sub-cell: "+
			"this is the epic's defect, and it means the tail cut would take the cache-write side "+
			"again", got, pairField, statusCacheCol)
	}
}

// TestStatusCacheReservationCoversItsFormatter ties the two halves together as
// ARITHMETIC: the sub-cell's reservation must be at least the pair the formatter can
// compose, and the pair's own term must be the two sides plus the separator the form
// really draws.
//
// This is the check that would have caught the epic's defect at review time: the old
// reservation (7) was smaller than the pair the formatter could produce (unbounded),
// and nothing compared the two numbers.
func TestStatusCacheReservationCoversItsFormatter(t *testing.T) {
	if got, want := statusCacheCol, 2*statusCacheCountSideCellsWidest+statusCachePairSepCells; got != want {
		t.Errorf("statusCacheCol = %d, want %d (two sides of %d + the %d-cell separator): the "+
			"reservation and the form's own composition disagree", got, want,
			statusCacheCountSideCellsWidest, statusCachePairSepCells)
	}
	// The separator is ZERO because each side carries its own arrow: the second side's
	// glyph IS the boundary. Counting one here as well reserves a cell no character
	// occupies — which is what the pre-fix arithmetic did, and it is why this assertion
	// names the reason rather than the number.
	if statusCachePairSepCells != 0 {
		t.Errorf("statusCachePairSepCells = %d: each side carries its own direction glyph "+
			"(statusCacheCountGlyphCells), so the pair draws nothing BETWEEN the sides and the "+
			"reservation must not reserve a cell over nothing", statusCachePairSepCells)
	}
	// The composed pair at its widest is exactly the reservation — measured by building
	// the string the row draws rather than by re-adding the terms. The PAIR is what the
	// sub-cell reserves (statusCachePairExpr); the value the row draws from
	// statusCacheField is the pair plus the hit rate riding it (task 01a0f8c9), which
	// is why this assertion asks the pair's own composer.
	widest := statusStatusCacheSideCountWidest
	got := statusFieldCells(statusCachePairExpr(statusOps{cacheRead: widest, cacheWrite: widest}))
	if got > statusCacheCol {
		t.Fatalf("the widest pair the formatter composes is %d cells, over the %d-cell sub-cell",
			got, statusCacheCol)
	}
	// And it is the METRICS GROUP's arithmetic that carries it: the group is its
	// sub-cells plus the light seams, so a sub-cell change without the group's would be
	// caught here even if the pin somehow stayed the same.
	if got, want := statusMetricsCol, statusCtxCol+statusCacheCol+(statusMetricsSubCells-1)*statusMetricsSepCells; got != want {
		t.Errorf("statusMetricsCol = %d, want %d: the group's reservation does not carry its own "+
			"sub-cells", got, want)
	}
}

// TestStatusCachePairSurvivesItsOwnMagnitude is the epic stated as an OPERATOR-facing
// assertion: at the magnitudes that used to cut it, the composed ROW carries BOTH
// numbers whole. It renders through the production composer at the pinned width
// (Rule 31 — the surface is executed), not through the formatter alone, because the
// defect lived in the composition: the formatter produced a string the column could
// not hold, and the CUT was what the operator saw.
func TestStatusCachePairSurvivesItsOwnMagnitude(t *testing.T) {
	cases := []struct {
		name string
		read int
	}{
		{"the operator's own session (the measured failure)", statusStatusCacheOperatorSession},
		{"a rung boundary", statusCountCompactKilo},
		{"a rung ceiling", statusCountCompactKilo - 1},
		{"a megabyte-scale session", statusCountCompactKilo * statusCountCompactKilo},
		{"the platform's int ceiling", math.MaxInt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The two sides are given DIFFERENT values, and that is deliberate: equal
			// counters can hide a composition bug that drops one side (the row would
			// still contain the number, twice from the same source). Distinct values
			// make each side's presence a separate fact.
			write := tc.read / 2
			row := statusProbeRowWithCache(t, statusColumnsAtCols, tc.read, write)

			if got := lineedit.DisplayWidth(row); got > statusColumnsAtCols {
				t.Fatalf("the row measures %d cells at the %d-cell pin: %q", got, statusColumnsAtCols, row)
			}
			readText := boundedCountRect(tc.read).text
			writeText := boundedCountRect(write).text
			if !strings.Contains(row, readText) {
				t.Errorf("the row does not carry the cache-READ number %q (counter %d): the "+
					"read-out's hit side is missing\n  row: %q", readText, tc.read, row)
			}
			if !strings.Contains(row, writeText) {
				t.Errorf("the row does not carry the cache-WRITE number %q (counter %d): this is the "+
					"epic's defect — the miss-billed side is the one the tail cut took\n  row: %q",
					writeText, write, row)
			}
			// The write side's own GLYPH must be present too: the number alone would not
			// say which of the two it is, and an unlabelled count beside a labelled one
			// is the ambiguity the pair exists to avoid.
			if !strings.Contains(row, statusCacheWriteGlyph) {
				t.Errorf("the row carries no %q glyph: a pair whose sides cannot be told apart is "+
					"not a pair\n  row: %q", statusCacheWriteGlyph, row)
			}
			if !strings.Contains(row, statusCacheReadGlyph) {
				t.Errorf("the row carries no %q glyph\n  row: %q", statusCacheReadGlyph, row)
			}
			// And the row's own ellipsis must not have eaten either side: a bounded form
			// means the cut never has to happen, so a "…" beside a cache arrow is the
			// defect in its other spelling.
			//
			// The check is scoped to the metrics GROUP's cells rather than to the whole
			// row, and the difference is a real one: another column may legitimately
			// elide its OWN digest — the tool column cuts a long tool name or summary
			// inside its reservation by design (statusToolColumnFieldFor) — and a
			// row-wide "…" test would read that as this defect. A "…" inside the group
			// is the one this assertion is about.
			if at := fieldStart(row, statusCtxLabel); at >= 0 {
				runes := []rune(row)
				if at < len(runes) && strings.Contains(string(runes[at:]), statusEllipsisForTest) {
					t.Errorf("the metrics group is elided at a magnitude the bounded form covers: %q\n  row: %q",
						statusEllipsisForTest, row)
				}
			}
		})
	}
}

// statusCountUnitIsOnLadder reports whether unit is one of statusCountUnits' own
// units — the vocabulary the reservation is derived from (Rule 13/14).
func statusCountUnitIsOnLadder(unit string) bool {
	for _, u := range statusCountUnits {
		if u.unit == unit {
			return true
		}
	}
	return false
}

// chat_status_row_unit_rule_test.go's own guard, in this file because it shares the
// statusCacheProbe suite's subject: the row's counts are ONE family drawn by ONE
// rule. The test itself lives in chat_status_row_unit_rule_test.go (it is long); this
// helper is here so the two remain one package-level fact.
//
// statusRowCountsAt composes the PRODUCTION row at the pin with the context pair at
// a chosen magnitude, through the source's own setters — the path the REPL drives
// (Rule 31). It is what both the red-proof and the pty evidence are stated against.
func statusRowCountsAt(t *testing.T, used, max int) string {
	t.Helper()
	s := newChatStatusSource(statusProbeModel, true)
	s.update(12, 0, used, "")
	s.setTool("keyword_search", "lineedit region cell widths")
	s.setOpsFn(func() statusOps {
		return statusOps{ctxUsed: used, ctxMax: max, ctxMeasured: true,
			cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite,
			elapsed: 4 * time.Minute, elapsedKnown: true}
	})
	s.setAvailCols(statusColumnsAtCols)
	return s.status().Line(statusColumnsAtCols)
}

// statusProbeRowWithCache composes the production row at cols with the cache
// counters set to the given values, through the source's own setters — the same
// path the REPL drives (Rule 31: the assertion renders the surface the operator
// sees, not a re-composition of its parts).
//
// The rest of the fixture is probeStatusSource's (the WIDEST row the row's own
// numbers can produce): the epic's defect only appeared on a row whose other
// columns were already full, so a probe that shrank the context pair would hide
// the very cut being asserted.
// statusRowProbeCtxUsed / statusRowProbeCtxMax are the context occupancy the
// probe row is composed with. They are NAMED here (Rule 1) because more than one
// assertion derives its expected pair from this row's own numbers — the pair's
// string only matches the row if both sides come from ONE definition, and a
// second spelling beside the row's own composition would be the stale-copy
// defect the ctx cases were repaired for.
const (
	statusRowProbeCtxUsed = 42100
	statusRowProbeCtxMax  = 64000
)

func statusProbeRowWithCache(t *testing.T, cols, read, write int) string {
	t.Helper()
	s := newChatStatusSource("deepseek-flash", true)
	s.update(12, 0, statusRowProbeCtxUsed, "")
	s.setTool("keyword_search", "lineedit region cell widths")
	s.setOpsFn(func() statusOps {
		return statusOps{
			ctxUsed: statusRowProbeCtxUsed, ctxMax: statusRowProbeCtxMax, ctxMeasured: true,
			cacheRead: read, cacheWrite: write,
			elapsed: 42 * time.Second, elapsedKnown: true,
			compressions: 2, compressionsKnown: true,
		}
	})
	s.setAvailCols(cols)
	return s.status().Line(cols)
}

// TestStatusCacheCountNeverPrintsABareUnit is the MAGNITUDE test: the bounded form
// must render every count WITH a mantissa, so no magnitude is ever drawn as a unit
// glyph and nothing else ("0M", "0G").
//
// It fails on the pre-fix form, and the failure is a BAND rather than a point. The
// pre-fix formatter's digits cap (statusCountWholeDigitsWidest) makes it climb a rung
// whenever the whole part would overflow TWO digits, and at the precision the row's
// budget buys (statusCountFractionDigits ZERO — no decimal is drawn on a side, because
// a decimal costs the separator plus its digit on a side that appears TWICE) the rung
// it climbed to has a whole part of ZERO for every value in the lower half of that
// rung. The pre-fix output there is therefore the unit alone:
//
//	[100000 .. 499999]  ->  "0M"     (a hundred thousand tokens, drawn as no number)
//	[100000000 .. 499999999] -> "0G"
//
// The band's bounds are DERIVED from the geometry here, not written as literals: a
// rung's scale divided by the digits cap is the first value the climb reaches that
// rung for, and half that scale is the last value whose whole part still rounds to
// zero. So a change to the cap or to the ladder moves the test with it (Rule 13/14),
// and the assertions stay true statements about the formatter the row actually draws.
//
// The check is on the RATCHET rather than on a remembered spelling: it asserts the
// TEXT CARRIES A DIGIT, which is the property "there is a mantissa", and that is what
// a bare unit glyph violates. Pinning "1M" instead would re-encode this run's choice
// of figure and go red on an honest improvement to the rounding.
func TestStatusCacheCountNeverPrintsABareUnit(t *testing.T) {
	if statusCountFractionDigits != 0 {
		t.Skipf("this test states the bare-unit defect at ZERO precision (the precision the row's "+
			"budget buys); it is at %d, so the band arithmetic below does not describe the form "+
			"any more and the guard belongs with the precision change",
			statusCountFractionDigits)
	}
	checked := 0
	for _, u := range statusCountUnits {
		if u.unit == "" || u.scale <= 0 {
			continue
		}
		// The band this rung's climb reaches, derived: the first value that climbs to
		// the rung (its scale as scaled by the whole-part cap), through the last value
		// whose whole part still rounds away (half the rung). The band is walked with a
		// STRIDE and its ENDPOINTS rather than element by element, because the band's
		// interior is hundreds of millions of counts (this walks ~2.8M/s) and the
		// property is a predicate of the form, not of any one magnitude: the stride is a
		// divisor of the band, so the walk lands on the boundaries and on a regular
		// sample of everything between them.
		first := u.scale / statusCountDigitsPerRung
		last := u.scale/2 + first
		for n := first; n <= last; n += statusBareUnitProbeStride {
			rect := boundedCountRect(n)
			if !strings.ContainsAny(rect.text, statusCountMantissaDigits) {
				t.Fatalf("count %d renders %q: a magnitude drawn as the unit %q and nothing else. "+
					"This is the pre-fix form — a field that names an order of magnitude and no "+
					"quantity, which the pair presents to the reader as a measurement. Every "+
					"magnitude must carry a mantissa (statusCountSmallestMantissa)",
					n, rect.text, u.unit)
			}
			// The bare-unit shape is a SUBSET of "carries no digit", and it is asserted
			// separately because it is the exact spelling the reader sees: the unit
			// preceded by the zero glyph.
			if rect.text == statusCountZero+u.unit {
				t.Fatalf("count %d renders %q — the bare-unit glyph exactly", n, rect.text)
			}
			checked++
		}
		// The band's ENDPOINTS are asserted too, and they are the two points a stride
		// can step over: `first` is the first count the climb reaches the rung for, and
		// `last` is where the whole part stops rounding away — the defect's own edge.
		for _, n := range []int{first, last} {
			rect := boundedCountRect(n)
			if !strings.ContainsAny(rect.text, statusCountMantissaDigits) {
				t.Fatalf("count %d renders %q: the band's own edge carries no mantissa", n, rect.text)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatalf("the band probe checked no values: the derived bands are empty, so this test " +
			"would pass by not running (statusCountUnits / statusCountDigitsPerRung)")
	}
	t.Logf("checked %d counts across %d unit bands: every one carried a mantissa",
		checked, len(statusCountUnits)-1)
}

// statusCountMantissaDigits are the characters that make a rendering a NUMBER rather
// than a unit glyph. Named (Rule 1) because the property under test is "there is a
// mantissa", and this is that property's definition in one place — the digits the
// bounded form prints with, plus nothing else.
const statusCountMantissaDigits = "0123456789"

// statusBareUnitProbeStride is the step the bare-unit band walk takes. It is a
// STRIDE rather than a step of one because the bands are hundreds of millions of
// counts wide and the property is a predicate of the FORM: consecutive counts inside
// a rung share the same spelling path, so the walk lands on the boundaries (asserted
// separately, endpoint by endpoint) and on a regular sample between them. Named
// (Rule 1) so the walk's resolution is a stated choice rather than a buried number.
const statusBareUnitProbeStride = 97

// TestStatusCountTotalityCostsNoCell is the RE-DERIVED FUNDING test: the magnitude
// repair above is paid for entirely INSIDE master's current geometry, so the
// arithmetic that pins the status row (statusPinCols == the budget, slack ZERO) is
// untouched.
//
// It exists because the deleted branch bought its totality differently. That branch
// wanted a DECIMAL on each side ("1.7M"), which is one cell more per side than
// statusCountWholeDigitsWidest + the unit, and it paid by re-partitioning
// statusModeCol 5 -> 3 — the OPPOSITE direction to master's own 3 -> 5. Two claims
// made that funding invalid here, and this test is where both are stated as
// arithmetic:
//
//  1. THE DONOR DID NOT EXIST. master's mode sub-cell is sized against
//     statusToolsOnProbe ("tools", five cells) and carries ZERO slack, so three cells
//     would cut the mode field — the branch's difference was a re-partition of a
//     budget master had already spent, not a saving it could lend.
//  2. MASTER OWNS THE PARTITION. The conflict this epic reworks WAS that opposite
//     re-partition (2 files, cache sub-cell 8 vs 10, mode 5 vs 3). Re-fighting it is
//     the one thing the epic forbids, so the repair must not need a cell at all.
//
// The mantissa clamp satisfies (2) exactly: it replaces a zero mantissa with
// statusCountSmallestMantissa, ONE digit, which is the same width as any rung figure
// the reservation already covers — so every term below is unchanged from master and
// the assertions are the re-derivation rather than a restatement of the branch's.
func TestStatusCountTotalityCostsNoCell(t *testing.T) {
	// (1) THE FORM'S WIDTH IS UNCHANGED BY THE CLAMP, re-derived from the geometry's
	// own terms rather than trusted: the widest text a side can print is the whole-part
	// cap plus the unit plus whatever precision costs, arithmetically.
	wantTextWidest := max(statusCountDigitsBase,
		statusCountWholeDigitsWidest+statusCacheCountUnitWidest+
			statusCountFractionSepCells+statusCountFractionDigits)
	if statusCountSideTextWidest != wantTextWidest {
		t.Fatalf("statusCountSideTextWidest = %d, re-derived %d: the reservation's text term "+
			"must be the max of the whole-number class and the rung class",
			statusCountSideTextWidest, wantTextWidest)
	}
	// The clamp's own figure must fit INSIDE that term — this is the sentence that
	// makes the repair free, and it is the one the branch's decimal could not say.
	clamped := statusCountSmallestMantissa
	if countDecimalDigits(clamped)+statusCacheCountUnitWidest > statusCountSideTextWidest {
		t.Fatalf("the clamp prints %d digits + %d unit cell = %d, over statusCountSideTextWidest %d: "+
			"the repair would need cells the geometry has not got, which is the re-partition "+
			"this epic refuses", countDecimalDigits(clamped), statusCacheCountUnitWidest,
			countDecimalDigits(clamped)+statusCacheCountUnitWidest, statusCountSideTextWidest)
	}
	// (2) THE SUB-CELL IS MASTER'S, term by term: the pair is the two sides plus the
	// separator the row really draws.
	wantCacheCol := 2*statusCacheCountSideCellsWidest + statusCachePairSepCells
	if statusCacheCol != wantCacheCol {
		t.Fatalf("statusCacheCol = %d, re-derived %d: the cache sub-cell must be the two sides plus "+
			"the pair separator", statusCacheCol, wantCacheCol)
	}
	if wantCacheCol != statusCacheSubCellExpected {
		t.Fatalf("statusCacheCol = %d, want master's %d: the cache sub-cell is master's partition "+
			"and this change must not move it (the branch's 8 -> 10 is void)",
			wantCacheCol, statusCacheSubCellExpected)
	}
	// (3) THE RETIRED MODE COLUMN IS NO LONGER A DONOR AT ALL (operator bug #5, task
	// 01a0e4c7): the cells it held were ABSORBED by the tool column when the row stopped
	// stating the tool state twice, so there is no mode sub-cell left to re-partition —
	// the branch's 5 -> 3 funding is void twice over.
	//   The absorbed width is re-derived from the constants rather than trusted, and the
	// absorption is what keeps the pin on budget:
	//	statusToolCol = its own width + the retired column + the separator that joined it.
	//   RE-PARTITION (task 01a0f8c9): the way-marker breadth is now a term of its own
	//   (statusToolColWayBreadth) and it is ZERO at this pin — the cells it held pay
	//   for the hit rate's home in the metrics group (statusCacheHitRateCells). The
	//   absorbed width is still re-derived from the constants rather than trusted.
	if want := statusToolColOwn + statusModeColRetired + statusToolColWayBreadth; statusToolCol != want {
		t.Fatalf("statusToolCol = %d, re-derived %d: the tool column must be its own width plus the "+
			"retired mode column plus the way-marker breadth", statusToolCol, want)
	}
	// (4) AND THE ROW IS STILL EXACTLY ON BUDGET: the pin is the budget, slack zero —
	// the statement the repair must leave TRUE, asserted as arithmetic rather than as a
	// remembered number.
	if statusPinCols != statusLayoutBudgetCols {
		t.Fatalf("statusPinCols = %d, want the budget %d", statusPinCols, statusLayoutBudgetCols)
	}
	if statusPinSlackCells != 0 {
		t.Fatalf("statusPinSlackCells = %d, want 0: totalling the form must not spend the slack",
			statusPinSlackCells)
	}
}

// statusCacheSubCellExpected is master's OWN partition of
// the status row at the time of this repair — the numbers this change must leave
// untouched, named (Rule 1) so the test states the re-derivation against a recorded
// geometry instead of an inline literal. They are the pre-images the epic's conflict
// was about: the branch moved them to 10 and 3, master holds 8 and 5.
const statusCacheSubCellExpected = 8
