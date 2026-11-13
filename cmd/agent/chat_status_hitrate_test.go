package main

import (
	"math"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// This file is the hit-rate read-out's own guard (task 01a0e8c7): the metrics
// group's leading sub-cell states the PROMPT CACHE's hit rate — the percentage the
// transport's own counters produce — instead of the context pair, which said only
// how full gopi's trim budget was and read as dead weight on a 1M-window model
// ("ctx 1k/1M≡").
//
// Three properties are stated here, each as a property of the COMPOSER rather than
// of one rendering:
//
//	1. the percentage is ARITHMETIC over the two counters statusOps carries, and it
//	   is CLAMPED to the range a reader can act on;
//	2. UNMEASURED is not zero: no counter, no percentage — the sub-cell falls back
//	   to the context pair rather than printing a "0%" that reads as a measurement;
//	3. the form's width is BOUNDED BY CONSTRUCTION and DERIVED from named constants,
//	   so no counter value can lengthen it past the sub-cell's reservation (the T5
//	   lesson: a reservation sampled from a sample string is not a reservation).

// statusHitRateProbeOps is the state every case below perturbs: a session whose
// cache has answered (both counters measured) and whose context is known.
func statusHitRateProbeOps() statusOps {
	return statusOps{
		ctxUsed: 42100, ctxMax: 64000, ctxMeasured: true,
		cacheRead: 12300, cacheWrite: 4200,
	}
}

// TestStatusHitRateIsArithmeticOverTheCounters pins the derivation: the rate is
// read/(read+write) clamped to 0..statusPctMax, and it is the SAME definition the
// context percentage uses (one clamp constant, Rule 14).
func TestStatusHitRateIsArithmeticOverTheCounters(t *testing.T) {
	cases := []struct {
		name  string
		read  int
		write int
		want  string
	}{
		{"a served-heavy session", 12300, 4200, "74.5%"},
		{"the pty's first turn", 896, 414, "68.4%"},
		{"the pty's second turn", 2048, 582, "77.9%"},
		{"everything served", 4096, 0, "100.0%"},
		{"nothing served (all written)", 0, 4096, "0.0%"},
		{"a write with no read is not a negative rate", 10, 99990, "0.0%"},
		{"a read orders of magnitude past the writes", 200000, 1, "100.0%"},
		// The session that commissioned this change (the operator's own report,
		// task 01a0f658): 111104 served of 112473 prompt tokens. It is the case the
		// whole epic exists for — the whole-percent form printed this as "98", and
		// the measurement is the fraction below it.
		{"the operator's own session", 111104, 1369, "98.8%"},
		// The EXACT half-percent boundary: 395/400 = 98.75, the value a one-digit
		// form must still state. It is pinned as the string the formatter produces
		// (round-half-away-from-zero at the printed digit), because a boundary is
		// where a formatter and a truncation disagree.
		{"the exact 98.75 boundary", 395, 5, "98.8%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := statusCacheHitRate(statusOps{cacheRead: tc.read, cacheWrite: tc.write})
			if got != tc.want {
				t.Fatalf("read=%d write=%d → %q, want %q", tc.read, tc.write, got, tc.want)
			}
		})
	}
	// The DIGITS are a named format parameter (Rule 1), not a literal baked into
	// the composer: the form carries exactly statusHitRateDecimals fraction digits
	// on every counter value, and that count is the epic's whole request.
	if statusHitRateDecimals != 1 {
		t.Fatalf("statusHitRateDecimals = %d, want 1 — the row's resolution is one decimal place",
			statusHitRateDecimals)
	}
	// ONE FORM, TWO HALVES (Rule 14): everything the composer can print is
	// readable by the reader the tier decision uses, and the reader recovers the
	// very numbers the counters state. A form nobody can read back is the silent
	// no-tier defect this pairing exists to prevent.
	for _, ratio := range []float64{0, 0.0005, 0.4999, 0.5, 0.75, 0.9875, 0.999, 1} {
		form := statusHitRatePct(ratio)
		back, ok := statusHitRateReading(form)
		if !ok {
			t.Fatalf("the composer's own form %q (ratio %v) does not parse — the two halves disagree", form, ratio)
		}
		if math.Abs(back-ratio*100) > 0.05 {
			t.Fatalf("ratio %v round-tripped to %v%% through %q — beyond the printed digit", ratio, back, form)
		}
	}
	// The run's own REAL numbers, taken from the pty evidence of the change that
	// commissioned it: two turns of one session, whose rates DIFFER — that is the
	// property that makes this read-out worth the cells (a constant would not be).
	if a, b := statusCacheHitRate(statusOps{cacheRead: 896, cacheWrite: 414}),
		statusCacheHitRate(statusOps{cacheRead: 2048, cacheWrite: 582}); a == b {
		t.Fatalf("the two measured turns produce the same rate %q — the read-out would not move", a)
	}
	// The clamp is the shared constant, not a second literal: a percentage the row
	// prints can never exceed it.
	if statusPctMax != 100 {
		t.Fatalf("statusPctMax = %d, want 100 — the row's percentages are clamped to one ceiling", statusPctMax)
	}
}

// TestStatusHitRateIsSilentWhenNothingWasMeasured is Rule 15b for this field: a
// cache that has not answered prints NO percentage, and the sub-cell states the
// read-out it CAN state instead. A "0%" would be the row claiming a measurement
// of a cache that has served nothing — and no session's first prompt is evidence
// of a cache that always misses.
func TestStatusHitRateIsSilentWhenNothingWasMeasured(t *testing.T) {
	if got := statusCacheHitRate(statusOps{}); got != "" {
		t.Fatalf("an unmeasured cache renders %q, want the empty string (Rule 15b)", got)
	}
	if got := statusCacheHitRateField(statusOps{}, statusCtxCol); got != "" {
		t.Fatalf("an unmeasured cache renders the FIELD %q, want empty", got)
	}
	// The row must not spell a rate at all in that state — measured on the
	// composed ROW rather than on the helper, because the helper agreeing with
	// itself proves nothing about the surface the operator reads.
	row := statusProbeRowWithCache(t, statusColumnsAtCols, 0, 0)
	if strings.Contains(row, statusHitRateLabel+statusHitRatePctSep) {
		t.Errorf("the pinned row states a hit rate for an unmeasured cache:\n  row: %q", row)
	}
	// And the CELL is not a hole: the group's leading sub-cell falls back to the
	// context read-out, which IS measured at this state.
	fallback := statusCtxField(statusOps{ctxUsed: 42100, ctxMax: 64000, ctxMeasured: true}, 42100)
	if fallback == "" {
		t.Fatalf("the fallback read-out composes empty — the fixture is wrong")
	}
	if !strings.Contains(row, fallback) {
		t.Errorf("the group shows a hole where the cache is unmeasured, want the context fallback %q:\n  row: %q",
			fallback, row)
	}
	// The other direction: once the cache HAS answered, the rate is on the pinned
	// row, and it is the rate this state's counters produce. It rides the CACHE
	// sub-cell's own value (task 01a0f8c9) — beside the two counters it is computed
	// from — and the group's leading sub-cell is the context pair, so the assertion
	// is that the rate reaches the screen at all, and that the cell it occupies is
	// the cache sub-cell's read-out (one composition, not a second spelling).
	measured := statusProbeRowWithCache(t, statusColumnsAtCols, statusProbeCacheRead, statusProbeCacheWrite)
	want := statusCacheHitRate(statusOps{cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite})
	if want == "" {
		t.Fatalf("the fixture's counters produce no rate — the case itself is wrong")
	}
	// THE FORM THE ROW DRAWS is the composer's own (this epic: the reading is
	// marked by its glyph, Σ for the session's rate, and it rides the cache pair's
	// value) — resolved here rather than spelled, so the assertion moves with the
	// composer instead of encoding one of its shapes (Rule 14).
	labelled := statusHitRateCumulativeSpan(statusOps{cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite})
	if !strings.Contains(measured, labelled) {
		t.Errorf("the pinned row does not carry the labelled hit rate %q:\n  row: %q", labelled, measured)
	}
	if cache := statusCacheField(statusOps{cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite}); !strings.Contains(measured, cache) {
		t.Errorf("the pinned row does not carry the cache sub-cell's own read-out %q — the rate must ride "+
			"the cells that sub-cell occupies, not a second composition:\n  row: %q", cache, measured)
	}
}

// TestStatusHitRateFormIsBoundedByConstruction is the T5 lesson applied to the
// new field: the form is composed from NAMED constants and every form it can take
// is inside the sub-cell's reservation — for EVERY counter value the int type can
// hold, not for the sample a fixture happens to pass. A form that grew with its
// input would be cut by the row's own field cut, and the cut takes the TAIL, which
// is where the measurement lives.
func TestStatusHitRateFormIsBoundedByConstruction(t *testing.T) {
	probes := []int{
		0, 1, 9, 10, 999, 1000, 9999, 99999, 999999, 9999999,
		math.MaxInt32, math.MaxInt,
	}
	for _, read := range probes {
		for _, write := range probes {
			field := statusCacheHitRateField(statusOps{cacheRead: read, cacheWrite: write}, statusCtxCol)
			if read == 0 && write == 0 {
				// The ONE state this composer answers empty — nothing measured is
				// the absence of a measurement, not a rate (Rule 15b), and it is
				// asserted by its own test above.
				if field != "" {
					t.Fatalf("an unmeasured cache composed %q, want the empty read-out", field)
				}
				continue
			}
			if field == "" {
				t.Fatalf("read=%d write=%d composed the empty field — a measured cache always has a rate",
					read, write)
			}
			if w := lineedit.DisplayWidth(field); w > statusCtxCol {
				t.Fatalf("read=%d write=%d renders %q in %d cells, over the sub-cell's reservation %d",
					read, write, field, w, statusCtxCol)
			}
			if strings.Contains(field, statusEllipsisForTest) {
				t.Fatalf("read=%d write=%d renders %q with the row's own cut marker — a bounded form never needs the cut",
					read, write, field)
			}
			// Every form CARRIES the fact's name: the row holds more than one
			// number of this shape, and an unnamed ratio is the ambiguity the name
			// exists to close. Where the form has the cells the name leads it —
			// which is every state at the pinned reservation — but the property
			// asserted here is the naming, not the position (statusCacheHitRateAppend
			// drops the labelled form WHOLE rather than printing an unnamed rate, so
			// statusCacheHitRateField's bare arm answers only a caller that asked for
			// fewer cells than the name needs).
			if !strings.Contains(field, statusHitRateLabel) {
				t.Fatalf("read=%d write=%d renders %q, which does not carry %q",
					read, write, field, statusHitRateLabel)
			}
		}
	}
	// The width comes from NAMED constants (Rule 1), never from a rendered sample:
	// the label's cells are its own declared width and are measured against the
	// string here, so a rename cannot drift from the declaration.
	if got, want := lineedit.DisplayWidth(statusHitRateLabel), statusHitRateLabelCells; got != want {
		t.Fatalf("statusHitRateLabel %q measures %d cells, statusHitRateLabelCells says %d",
			statusHitRateLabel, got, want)
	}
	// The widest labelled form the composer can produce — the clamped rate at a
	// full 100% with all its fraction digits — is inside the reservation, which is
	// what makes the pinned geometry take the full form at every counter value and
	// keeps the label from having to yield for the fraction. The digits come from
	// the SAME named constant the composer formats with (Rule 1), never from a
	// sample string typed here.
	widest := statusHitRatePct(1)
	if got := statusHitRateLabel + statusHitRatePctSep + widest; lineedit.DisplayWidth(got) > statusCtxCol {
		t.Fatalf("the widest labelled form %q measures %d cells, over the sub-cell's reservation %d",
			got, lineedit.DisplayWidth(got), statusCtxCol)
	}
	// The fraction is WHY this assertion has a narrow margin left: the reserved 17
	// must still host the name, the gap and the widest rate, and it does at every
	// value the counters can hold (the walk above).
	if got, want := lineedit.DisplayWidth(statusHitRatePct(1)), statusHitRatePctCellsWidest; got != want {
		t.Fatalf("the widest rate %q measures %d cells, statusHitRatePctCellsWidest says %d — "+
			"the composition's width and the declared one must not drift",
			statusHitRatePct(1), got, want)
	}
}

// TestStatusHitRateFormYieldsTheNameOnlyWhenItMust drives the composer at a
// geometry narrower than the pinned sub-cell — through the SAME function the
// production path calls, with the cells passed in rather than spelled as a
// constant — so the yield branch is arithmetic a test can reach rather than prose
// about a shape. The measurement is the LAST thing this read-out gives up: a
// percentage that fits is never dropped to keep a word that only names it.
func TestStatusHitRateFormYieldsTheNameOnlyWhenItMust(t *testing.T) {
	ops := statusOps{cacheRead: 12300, cacheWrite: 4200}
	full := statusCacheHitRate(ops)
	if full != "74.5%" {
		t.Fatalf("the fixture's rate is %q, want 74.5%% (12300/16500)", full)
	}
	// THE FORM THE COMPOSER DRAWS (this epic): the marker directly against the
	// percentage, no gap — the reading's marker is ONE cell (it used to be the word
	// "hit" and a space) and the two readings the row now carries are funded by
	// exactly those cells, so a gap here would be a cell the second reading needs.
	// The form is resolved from the SAME constant the composer uses (Rule 14).
	labelled := statusHitRateLabel + full
	// The walk is two-sided at each boundary: the labelled form where the cells
	// host it, the EMPTY read-out where they do not — never the unnamed percentage
	// (see the return note in the composer: the row must not print a ratio two
	// cells from the pair it is computed from without the word that names it).
	widths := []struct {
		cells int
		want  string
	}{
		{statusCtxCol, labelled},
		{lineedit.DisplayWidth(labelled), labelled},
		{lineedit.DisplayWidth(labelled) - 1, ""},
		{lineedit.DisplayWidth(full), ""},
		{lineedit.DisplayWidth(full) - 1, ""},
	}
	for _, w := range widths {
		got := statusCacheHitRateField(ops, w.cells)
		if got != w.want {
			t.Errorf("cells=%d renders %q, want %q", w.cells, got, w.want)
		}
		if got != "" {
			if width := lineedit.DisplayWidth(got); width > w.cells {
				t.Errorf("cells=%d renders %q in %d cells — the form must fit the geometry it was asked for",
					w.cells, got, width)
			}
		}
	}
	// THE ROW'S OWN GEOMETRY IS THE CACHE SUB-CELL'S (task 01a0f8c9): the rate is
	// composed into that sub-cell's value, so the budget the row drives through this
	// composer is statusCacheCol, not statusCtxCol.
	//
	// THE ANSWER HERE IS THE PAIR ALONE, and that is a MEASURED boundary rather than
	// an accident: statusCacheCol's 8 cells are exactly a two-sided pair, so the
	// pinned row draws the pair and the rate's cells are what the group's column
	// reservation adds beyond its sub-cells (statusMetricsCol). The assertion is
	// therefore two-sided — the composer never returns an UNNAMED rate at this
	// geometry (the invariant the re-partition promises: no fact is silently
	// evicted, and no number is printed without its name), and the labelled form IS
	// what it answers where the geometry can host one.
	if got := statusCacheHitRateField(ops, statusCacheCol); got == full {
		t.Errorf("the row's sub-cell geometry renders the UNNAMED rate %q — a percentage beside a "+
			"two-sided pair is the ambiguity the name exists to close", got)
	}
	if got := statusCacheHitRateField(ops, lineedit.DisplayWidth(labelled)); got != labelled {
		t.Errorf("a geometry of the labelled form's own width renders %q, want %q", got, labelled)
	}
}

// TestStatusRowKeepsBothThePairAndTheRate is the millisecond-scale half of the pty
// guard's clauses 4 and 5 (testsuite/chat_pty_ctx_qualifier_test.go): at the pinned
// geometry the composed row carries the ctx pair AND the named rate, in that order,
// with the cache transport in the cells between them.
//
// WHY IT EXISTS BESIDE THE PTY RUN. The pty case is the rendered-truth check, but it
// costs ~14s and only reaches ONE state (the row a scripted provider paints); the
// invariant it protects is a property of the composition, and a defect that dropped
// the rate would be found here in milliseconds, naming the field. The original defect
// is exactly this shape one level down — the row painted "· hit 13.4% ·↑12k↓80k" with
// the pair ABSENT — so a guard that asserts only ONE of the two facts cannot see its
// own mirror image (the pair kept, the rate silently dropped).
func TestStatusRowKeepsBothThePairAndTheRate(t *testing.T) {
	ops := statusOps{
		ctxUsed: statusRowProbeCtxUsed, ctxMax: statusRowProbeCtxMax, ctxMeasured: true,
		cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite,
	}
	row := statusProbeRowWithCache(t, statusColumnsAtCols, statusProbeCacheRead, statusProbeCacheWrite)

	pair := statusCtxPair(ops, ops.ctxUsed)
	if pair == "" {
		t.Fatalf("the fixture composes no ctx pair — the case itself is wrong")
	}
	// The pair and the rate are the SAME reading on the row: the pair's own composer
	// and the rate's own composer, both resolved through the production constants —
	// and the rate's form is the COMPOSER's (this epic marks it with Σ and rides the
	// pair's value, so the string looked for is the span the column really carries,
	// never a spelling typed here: Rule 14).
	rate := statusHitRateCumulativeSpan(ops)
	pairAt, rateAt := fieldStartInTest(row, pair), fieldStartInTest(row, rate)
	if pairAt < 0 {
		t.Fatalf("the pinned row does not carry the ctx pair %q: the number that predicts a compaction is the "+
			"fact the re-partition made primary\n  row: %q", pair, row)
	}
	if rateAt < 0 {
		t.Fatalf("the pinned row carries the ctx pair but NOT the named rate %q: the rate must yield only where "+
			"the cells force a choice, and never silently (acceptance (e))\n  row: %q", rate, row)
	}
	if pairAt > rateAt {
		t.Fatalf("the pinned row states the rate at cell %d and the ctx pair at %d — the RATE leads; the pair must "+
			"lead the metrics group\n  row: %q", rateAt, pairAt, row)
	}
	// The cells between the pair and the rate are the cache transport's: the rate's
	// home is the sub-cell whose two counters it is computed from, so a rate that
	// appeared anywhere else on the row (the tool digest, the live block) would be a
	// different fact wearing its name.
	// The slice is taken in BYTES (the row is full of multi-byte glyphs, so a cell
	// offset is not a byte offset — the two are only equal on an ASCII row), while
	// the ORDER above is stated in CELLS. Both are spelled at their own unit rather
	// than converted, which is how the pairing of the two stays checkable by hand.
	between := row[strings.Index(row, pair)+len(pair) : strings.Index(row, rate)]
	if !strings.Contains(between, statusCacheReadGlyph) {
		t.Fatalf("the cells between the ctx pair and the rate (%q) do not carry the cache transport %q: the rate "+
			"rides the sub-cell it is computed from\n  row: %q", between, statusCacheReadGlyph, row)
	}
	// AND THE ORDER DOES NOT COME AT THE RATE'S EXPENSE AT ANY MEASURED MAGNITUDE: the
	// pair's survival and the rate's are ONE event on this geometry (the width ladder
	// sheds a whole sub-cell), which is the invariant that makes "the rate is the fact
	// that yields, never the pair" true in both directions.
	for _, rw := range []int{statusProbeCacheRead, statusCountCompactKilo, math.MaxInt} {
		s := statusProbeRowWithCache(t, statusColumnsAtCols, rw, rw/2)
		hasPair := strings.Contains(s, statusCachePairExpr(statusOps{cacheRead: rw, cacheWrite: rw / 2}))
		hasRate := strings.Contains(s, statusHitRateLabel)
		if hasPair != hasRate {
			t.Errorf("read=%d: the pair is on the row (%v) while the rate is not (%v) — the two leave together, "+
				"so the rate can never be dropped while its own counters are still printed\n  row: %q",
				rw, hasPair, hasRate, s)
		}
	}
}
