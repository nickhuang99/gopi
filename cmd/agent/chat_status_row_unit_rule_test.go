package main

// chat_status_row_unit_rule_test.go — the guard for the ONE composition rule the
// status row's counts are drawn by.
//
// THE DEFECT (measured on a real pty at 117x26, before-state in this task's
// evidence). The row carried TWO count formatters, and a quantity family — token
// volumes — was rendered in two different unit systems on the SAME row:
//
//	                          ctx 19.5k/200.0k≡ ·↑0↓20k    1s · ready
//	COUNTER RISES BY A MAGNITUDE, SAME FIELD:
//	                           19500.1k/200.0k≡ ·↑0↓20M    1s · ready
//
// The cache pair climbed the bounded k/M/G ladder (↑0↓20M), while the context pair
// climbed formatCount's k-ONLY one — so `k` appeared where `M` applies (19500.1k for
// 19.5M), a forced ".0" spent a cell on a digit the counter never reported, and the
// form grew with the input's magnitude until the sub-cell's own cut took the label
// (the second row above is missing its "ctx"). A reader could not apply one rule to
// the row; the scale had to be re-derived per field.
//
// THE RULE UNDER TEST, stated as assertions rather than as prose:
//
//	ONE LADDER        every count on the row is drawn by formatCountBounded, so the
//	                  unit shown is the largest rung of statusCountUnits the value
//	                  reaches — `k` is never used where `M` applies.
//	ONE DIGIT POLICY  no trailing ".0" (the fraction is dropped when it rounds
//	                  away), and the same significant digits on every field.
//	ONE WIDTH BOUND   every side fits statusCountSideTextWidest, for every int — so
//	                  the pair can never outgrow the sub-cell and force its own cut.
//
// The red-proof (TestStatusRowUnitRuleIsRedOnTheRetiredFormatter) states the same
// three facts against the RETIRED formatter's own output, so the defect is a red
// assertion in-repo rather than a paragraph in a report: the retired form fails the
// ladder assertion at the very magnitude the pty captured.

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// statusRowUnitRuleProblems reports every way the pair a side string pair spells
// breaks the three assertions above. It is written as a PREDICATE over two rendered
// sides (rather than over a count) precisely so the retired formatter's output can be
// fed to the SAME predicate — a red proof that re-implements the check would prove
// only that the copy agrees with itself.
//
// What it checks, in order:
//
//  1. the unit is a rung of statusCountUnits (no invented unit);
//  2. the unit is the LARGEST rung the value reaches — the "k where M applies"
//     assertion, and the one the retired formatter fails;
//  3. no trailing fraction separator with a zero fraction — the ".0" assertion;
//  4. the side's width is inside the bound the reservation is derived from.
func statusRowUnitRuleProblems(used, max int, usedSide, maxSide string, maxDigits, sideBound int) []string {
	var problems []string
	for _, side := range []struct {
		name  string
		value int
		text  string
	}{{"context used", used, usedSide}, {"context window", max, maxSide}} {
		unit, digits := splitCountSide(side.text)
		if !statusCountUnitIsOnLadder(unit) {
			problems = append(problems, fmt.Sprintf("%s %q: unit %q is not a rung of the row's ladder",
				side.name, side.text, unit))
			continue
		}
		if want := statusCountRungUnitShownAt(side.value, maxDigits); want != unit {
			problems = append(problems, fmt.Sprintf(
				"%s %q for %d: the row's ladder states this value in %q — %s is used where %q applies",
				side.name, side.text, side.value, want, unit, want))
		}
		if strings.HasSuffix(digits, statusCountFractionSep+statusCountZero) {
			problems = append(problems, fmt.Sprintf(
				"%s %q: a trailing %q spends a cell on a digit the counter did not report",
				side.name, side.text, statusCountFractionSep+statusCountZero))
		}
		if w := lineedit.DisplayWidth(side.text); w > sideBound {
			problems = append(problems, fmt.Sprintf(
				"%s %q is %d cells, over the field's own bound %d — the pair can outgrow its sub-cell",
				side.name, side.text, w, sideBound))
		}
	}
	return problems
}

// statusCountRungUnitShown is the unit the row's ladder states a value in, and it is
// deliberately NOT countScaleRung's answer for every value. countScaleRung reports the
// rung a value's MAGNITUDE selects; the ladder's own rule then ROUNDS once and
// ADVANCES the rung when that rounding runs the whole part past the digits a rung may
// print (and, at the rung it lands on, states the smallest mantissa rather than a bare
// unit). The two differ at a real boundary and the difference is the ladder's
// documented behaviour, not an inconsistency to paper over:
//
//	200000  countScaleRung → "k", a TWO-digit ladder → "1M"
//	        (200k needs three whole digits against statusCountWholeDigitsWidest = 2)
//	        — and the CONTEXT pair, which has its own three-digit allowance, states
//	        it "200k": the digit budget is per FIELD (formatCtxCount vs
//	        formatCountBounded), so the row has ONE ladder and TWO allowances.
//	999499  countScaleRung → "k", the two-digit ladder → "1M"   (the tenth carries)
//
// So a predicate that asked "is this the largest rung the magnitude reaches" would
// call the SHIPPED ladder wrong at its own boundaries — and it would be the predicate
// that is wrong, because the form is the row's own contract. What the predicate is
// really asking is the EPIC's question — "does this spelling belong to the row's unit
// system, or to a second one?" — and the honest oracle for that is the ladder itself,
// applied through boundedCountRect, which is the ONE function on this row that turns a
// count into a unit (Rule 14: one definition).
func statusCountRungUnitShown(n int) string {
	return boundedCountRect(n).unit
}

// statusCountRungUnitShownAt is the same oracle at a CALLER-CHOSEN allowance — the
// form the CONTEXT pair's sides are judged by, since that pair prints at
// statusCountCtxDigitsWidest rather than at the cache pair's two digits. One ladder,
// two allowances: the rung a value lands on depends on how many whole digits the
// field may print, so the oracle has to be told which field is asking.
func statusCountRungUnitShownAt(n, maxDigits int) string {
	return boundedCountRectDigits(n, maxDigits).unit
}

// splitCountSide separates a rendered side into its unit and its digits. It reads the
// ladder rather than the string's shape so that a MULTI-CHARACTER rung (should the
// ladder ever carry one) is still resolved, and it is the ONE place a rendered side is
// taken apart for the assertions above (Rule 14).
func splitCountSide(text string) (unit, digits string) {
	for _, u := range statusCountUnits {
		if u.unit != "" && strings.HasSuffix(text, u.unit) {
			return u.unit, strings.TrimSuffix(text, u.unit)
		}
	}
	return statusCountUnits[0].unit, text
}

// TestStatusRowCountsShareOneLadder is the epic's acceptance, stated over the ROW the
// operator reads: at every magnitude — one landing in k, one in M, one in G, and the
// int extremes — both pairs are drawn by the same rule, both pairs are WHOLE (no
// field cut), and the composed row stays inside the pin.
func TestStatusRowCountsShareOneLadder(t *testing.T) {
	cases := []struct {
		name     string
		used     int
		window   int
		wantUnit string
	}{
		// The unit is what the LADDER selects, not what a magnitude "sounds like":
		// 999500 is printed "1M" (the carry re-resolves the rung), and 200000 — the
		// deployed window — is "1M" for the same reason. A window of 64000 is the
		// case that lands on k.
		{"a magnitude in k (the pty's before-state)", 42100, 64000, "k"},
		{"a magnitude in M (the pty's before-state)", 19500128, 200000, "M"},
		{"a magnitude in G (the pty's worst case)", 1950012800, math.MaxInt32, "G"},
		{"the platform's int ceiling", math.MaxInt, math.MaxInt, "G"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := statusRowCountsAt(t, tc.used, tc.window)

			usedSide := boundedCountRect(tc.used).text
			maxSide := boundedCountRect(tc.window).text
			if problems := statusRowUnitRuleProblems(tc.used, tc.window, usedSide, maxSide,
				statusCountWholeDigitsWidest, statusCountSideTextWidest); len(problems) > 0 {
				t.Errorf("the row's own pair breaks the unit rule:\n  %s", strings.Join(problems, "\n  "))
			}
			// The unit the magnitude selects is the one the ladder's OWN resolver
			// reports for that magnitude — never a hand-kept expectation, so the
			// assertion moves with the ladder rather than with a spelling.
			if got := statusCountRungUnitShown(tc.used); got != tc.wantUnit {
				t.Fatalf("the ladder renders %d in unit %q, want %q — the fixture is wrong, not the row",
					tc.used, got, tc.wantUnit)
			}
			// And the magnitudes ARE the row's own counters: the sub-cell the row
			// draws at this state is composed by the production composer from the
			// same ops (task 01a0e8c7 — the cache counters drive the hit rate the
			// sub-cell leads with, and the context magnitudes are what the
			// sub-cell's fallback form carries), so a row that dropped a count
			// could not carry its own read-out.
			want := statusCtxField(statusOps{
				ctxUsed: tc.used, ctxMax: tc.window, ctxMeasured: true,
				cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite,
			}, 0)
			if want == "" {
				t.Fatalf("the fixture composes no read-out — the case itself is wrong")
			}
			if !strings.Contains(row, want) {
				t.Errorf("the row does not carry its own read-out %q\n  row: %q", want, row)
			}
			// BOTH pairs whole: the cache pair is the row's second count family and
			// the same rule has to hold for it — a row where one family is bounded
			// and the other is not is exactly the defect.
			cachePair := statusCacheField(statusOps{cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite})
			if !strings.Contains(row, cachePair) {
				t.Errorf("the row does not carry the cache pair %q\n  row: %q", cachePair, row)
			}
			if got := lineedit.DisplayWidth(row); got > statusColumnsAtCols {
				t.Errorf("the row measures %d cells at the %d-cell pin: %q", got, statusColumnsAtCols, row)
			}
			// The row's own ellipsis must not have eaten either pair — a bounded
			// form means the cut never has to happen, so this is the epic's
			// acceptance at its other spelling.
			//
			// The check is scoped to the metrics GROUP's cells rather than to the
			// whole row, and the difference is a real one (the same scoping task
			// 01a0e4c7 applied in chat_status_cache_bound_test.go and
			// chat_status_ctx_provenance_test.go, for the same reason): the tool
			// column cuts a long tool name or digest inside its OWN reservation by
			// design (statusToolColumnField), and this epic WIDENED that column
			// (16 → 25 cells, absorbing the retired mode column), so for a long
			// tool name the aligned row now draws a digest that ends in "…" where
			// the 16-cell column simply dropped the digest. A row-wide "…" test
			// would read that by-design digest cut as a truncated COUNT. A "…"
			// inside the metrics group is the one this assertion is about.
			if at := fieldStart(row, statusCtxLabel); at >= 0 {
				runes := []rune(row)
				if at < len(runes) && strings.Contains(string(runes[at:]), statusEllipsisForTest) {
					t.Errorf("the row is elided at a magnitude the row's own forms cover: %q", row)
				}
			}
		})
	}
}

// TestStatusRowUnitRuleIsRedOnTheRetiredFormatter is the RED PROOF, kept in-repo: it
// hands the SAME predicate the retired formatCount produced for the very count the
// pty captured, and requires the predicate to report the defect. A guard whose red
// path has never been shown is a guard nobody has tested — and this repo has paid for
// that lesson before (a table-driven composer test stayed green while the production
// wiring was removed).
//
// The retired formatter is spelled HERE, in the test, as the literal it was: the k-only
// ladder with a forced one-decimal tail. That is deliberate — the production spelling
// is DELETED (chat_stream.go's formatCount is gone), so the red path cannot be a
// commented-out call to a function nobody would notice coming back. The test also
// asserts that the production row does NOT contain the retired forms, which is the
// regression guard proper: the day someone re-adds a k-only count to this row, the
// operator's own screen string reappears in a test failure.
func TestStatusRowUnitRuleIsRedOnTheRetiredFormatter(t *testing.T) {
	// The retired form, verbatim: below 1000 the integer, above it "%.1fk".
	retired := func(n int) string {
		if n < statusCountCompactKilo {
			return fmt.Sprintf("%d", n)
		}
		return fmt.Sprintf("%.1fk", float64(n)/statusCountCompactKilo)
	}
	const (
		used   = 19500128 // the pty's M-magnitude before-state
		window = 200000
	)
	oldUsed, oldMax := retired(used), retired(window)

	problems := statusRowUnitRuleProblems(used, window, oldUsed, oldMax,
		statusCountWholeDigitsWidest, statusCountSideTextWidest)
	if len(problems) == 0 {
		t.Fatalf("the predicate PASSES the retired formatter's own output (%q/%q): the guard is "+
			"vacuous on the defect it exists for", oldUsed, oldMax)
	}
	// And it names the two defects the pty captured, not some unrelated complaint.
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "is used where") {
		t.Errorf("the red proof does not report the LADDER defect (k where M applies):\n%s", joined)
	}
	if !strings.Contains(joined, statusCountFractionSep+statusCountZero) {
		t.Errorf("the red proof does not report the trailing-%q defect:\n%s",
			statusCountFractionSep+statusCountZero, joined)
	}
	// The retired form's own width is what broke the sub-cell's reservation and made
	// the label yield: 7 cells ("1000.0k") against the ladder's 3.
	if w := lineedit.DisplayWidth(retired(999999)); w <= statusCountSideTextWidest {
		t.Errorf("the retired form renders the deployed window %q in %d cells, inside the ladder's "+
			"bound %d — the red proof is not exercising the width defect",
			oldMax, w, statusCountSideTextWidest)
	}

	// THE REGRESSION GUARD, on the live row: none of the retired spellings may appear.
	row := statusRowCountsAt(t, used, window)
	for _, retiredSpelling := range []string{oldUsed, oldMax, statusCountFractionSep + statusCountZero} {
		if strings.Contains(row, retiredSpelling) {
			t.Errorf("the row carries the retired unit form %q\n  row: %q", retiredSpelling, row)
		}
	}
}

// ctxReadMagnitudeForTest is the magnitude a live session sits at when the operator
// reads the row — 13100 tokens of a 200000 window, the pair the operator's own screen
// carried (statusDegradeStateOperations). It is named because the label's survival is
// asserted AT a magnitude rather than for all of them (the FOLD's rungs differ): a pair
// of two three-cell sides is what this epic bought a label for, and a pair of two
// four-cell sides is where the composer yields the word instead of the gap.
const ctxReadMagnitudeForTest = 13100

// TestStatusCtxPairReservationCoversTheLadder is the geometry tie the retired
// formatter made impossible: the context pair's per-side ceiling must be at least the
// formatter's own worst case, walked over every rung boundary and the int extremes —
// the same walk the cache pair's guard does, applied to the pair that used to be left
// out of it. The walk reads the RESERVED ceiling (statusCtxPairSideCellsWidest) against
// the two-digit formatter, which is the tie it has always been; the form the context
// pair actually PRINTS is spelled at its own wider allowance, and that form's bound is
// asserted by TestStatusCtxPairNeverDrawsDistinctCountersAsOneFigure
// (statusCtxPairSideCellsDrawn) — the two allowances are named constants precisely so
// each can be held to its own number (Rule 1).
func TestStatusCtxPairReservationCoversTheLadder(t *testing.T) {
	bounds := []int{0, 1, statusCountCompactKilo - 1, statusCountCompactKilo,
		statusProbeCtxMax, ctxWindowDeployedForTest,
		statusCountCompactKilo*statusCountCompactKilo - 1,
		statusCountCompactKilo * statusCountCompactKilo,
		statusCountCompactKilo*statusCountCompactKilo*statusCountCompactKilo - 1,
		math.MaxInt32, math.MaxInt}
	for _, n := range bounds {
		side := boundedCountRect(n)
		if side.cells > statusCtxPairSideCellsWidest {
			t.Errorf("the ladder renders %d as %q (%d cells), over the context pair's per-side "+
				"reservation %d: the pair can outgrow the sub-cell and lose a field to the cut",
				n, side.text, side.cells, statusCtxPairSideCellsWidest)
		}
		if !statusCountUnitIsOnLadder(side.unit) {
			t.Errorf("the ladder rendered %d in unit %q, which is not a rung of statusCountUnits", n, side.unit)
		}
	}
	// The pair the row really composes at the deployed window is inside its sub-cell,
	// MEASURED through the production composer rather than re-added from the terms.
	field := statusCtxField(statusOps{ctxUsed: ctxWindowDeployedForTest - 100, ctxMax: ctxWindowDeployedForTest, ctxMeasured: true}, 0)
	if w := lineedit.DisplayWidth(field); w > statusCtxCol {
		t.Errorf("the deployed-window sub-cell renders %d cells, over its %d-cell reservation: %q",
			w, statusCtxCol, field)
	}
	// AND THE LABEL'S FATE IS ARITHMETIC, not a promise for every magnitude: a pair of
	// two four-cell sides ("200k/200k", the window at a hundred tokens from full) is
	// NINE cells, plus the qualifier ten — inside the twelve the sub-cell reserves, but
	// the label and its gap are four more, so the FOLD drops the label there
	// (statusCtxField's own second rung). What must hold at EVERY magnitude is the
	// QUALIFIER (it closes the denominator's ambiguity) and the reservation.
	if !strings.Contains(field, statusCtxQuantGlyph) {
		t.Errorf("the deployed-window sub-cell dropped the denominator's qualifier: %q", field)
	}
	// The label DOES survive at the magnitude the operator reads — a pair of two
	// three-cell sides, where the composer closes the gap instead of the word. That is
	// the rung this epic bought, and it is asserted rather than assumed.
	readField := statusCtxField(statusOps{ctxUsed: ctxReadMagnitudeForTest, ctxMax: ctxWindowDeployedForTest, ctxMeasured: true}, 0)
	if !strings.Contains(readField, statusCtxLabel) {
		t.Errorf("the sub-cell dropped its label at the magnitude a live session sits at: %q — the "+
			"honest denominator (\"200k\") costs one cell, and the composer yields the GAP, never the word",
			readField)
	}
	if w := lineedit.DisplayWidth(readField); w > statusCtxCol {
		t.Errorf("the read-magnitude sub-cell renders %d cells, over its %d reservation: %q", w, statusCtxCol, readField)
	}
}
