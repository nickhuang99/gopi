package main

// chat_status_metrics_unmeasured_test.go — the metrics GROUP's honesty clause,
// asserted as a PREDICATE plus a RED PROOF, in the shape
// chat_status_row_unit_rule_test.go established.
//
// THE DEFECT. The metrics group is ONE reserved column of the status row
// (statusMetricsCol), and until this change its all-unmeasured arm returned "" —
// so on a session where nothing has been measured yet the column kept its cells,
// the top-level separators on both sides were still drawn, and the segment
// between the tool column and the live block was BLANK. The row therefore said
// nothing where it had reserved room to speak, and a reader could not tell
// "nothing has been measured yet" from "this field is broken" — the same
// sentence the tool column's idle indent used to say one column to the left.
//
// THE TWO CLAUSES THE FIX HAS TO HOLD AT ONCE, and why they are one predicate
// rather than two tests:
//
//	STATED   the group's unmeasured state is in WORDS — the reserved column is
//	         never a hole (a field that reserves space and prints nothing is
//	         worse than an absent field).
//	NO ZERO  and it states nothing about its COUNTERS: an unmeasured counter
//	         prints no number at all, so "0/0" / "0" / "0%" can never appear.
//	         Rule 15b's contract — never fabricate a zero — is preserved
//	         exactly: it is the reason the word is a word.
//
// The predicate is written over the COMPOSED TEXT (rather than over a statusOps
// value) precisely so the PRE-FIX shape can be fed to the SAME predicate: a red
// proof that re-implemented the check would prove only that the copy agrees with
// itself.

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// statusMetricsPreFixUnmeasured is the content the group's all-unmeasured arm
// returned BEFORE this change, spelled here as the literal it was. It is the
// red proof's own input: the predicate below must REFUSE it, so the defect is a
// failing assertion in-repo rather than a paragraph in a report.
const statusMetricsPreFixUnmeasured = ""

// statusMetricsHonestyDigit is the character class an unmeasured counter may
// never print. Only digits are excluded from the STATED word: a glyph (the
// group's own seam) could never be a fabricated measurement on its own, while a
// DIGIT is exactly what "the counter is zero" looks like, and it is the reading
// Rule 15b forbids the row to invite.
func statusMetricsHonestyHasDigit(text string) (rune, bool) {
	for _, r := range text {
		if unicode.IsDigit(r) {
			return r, true
		}
	}
	return 0, false
}

// statusMetricsGroupProblems reports every way a composed metrics group breaks
// the clause above. text is the group's own content as the row would draw it
// (the metrics column's cells, trimmed), so the predicate is stated on what a
// reader sees rather than on how it was produced.
func statusMetricsGroupProblems(text string) []string {
	var problems []string
	if strings.TrimSpace(text) == "" {
		problems = append(problems, "the group is BLANK: the column reserves its "+
			"cells and prints nothing, so a reader cannot tell an unmeasured "+
			"read-out from a broken field")
	} else if !strings.ContainsFunc(text, unicode.IsLetter) {
		problems = append(problems, fmt.Sprintf("the group carries no word a reader can "+
			"decode (its cells are %q): a reserved field must state its state in "+
			"words, not only a glyph or an indent", text))
	}
	if r, found := statusMetricsHonestyHasDigit(text); found {
		problems = append(problems, fmt.Sprintf("the group states a NUMBER (%q) although "+
			"nothing was measured: a counter nobody measured must never be rendered as "+
			"a value — least of all as the zero %q that reads as a measurement",
			string(r), text))
	}
	if w := lineedit.DisplayWidth(text); w > statusMetricsCol {
		problems = append(problems, fmt.Sprintf("the group is %d cells, over the column's "+
			"%d-cell reservation: the content does not fit the cells the geometry holds "+
			"for it", w, statusMetricsCol))
	}
	return problems
}

// unmeasuredStatusSource is a session where NOTHING operational has been measured
// yet: a fresh source whose ops source reports the zero statusOps — no context,
// no cache transport, no clock — and whose token total is still zero. It is the
// state the defect was reported on, reached through the source's own wiring
// (setOpsFn + update), never through a hand-built row.
func unmeasuredStatusSource() *chatStatusSource {
	s := newChatStatusSource(statusHonestyModel, true)
	s.setOpsFn(func() statusOps { return statusOps{} })
	s.update(statusLiveIdleTurn, statusLiveIdleTurn, 0, "")
	return s
}

// TestStatusMetricsUnmeasuredGroupStatesItsState pins the fix: with nothing
// measured, the group's content STATES its own state in words, and the predicate
// above accepts it.
//
// It is deliberately NOT an equality against the constant — the assertion is the
// PROPERTY (a reserved column is never a hole and never invents a number), which
// is what a future rewording must keep honouring.
func TestStatusMetricsUnmeasuredGroupStatesItsState(t *testing.T) {
	got := statusMetricsField(statusOps{}, 0)
	if problems := statusMetricsGroupProblems(got); len(problems) > 0 {
		t.Errorf("the all-unmeasured group breaks its honesty clause:\n  %s\ngroup = %q",
			strings.Join(problems, "\n  "), got)
	}
	if got == statusMetricsUnmeasured {
		t.Logf("all-unmeasured group = %q (%d cells of the %d-cell column)",
			got, lineedit.DisplayWidth(got), statusMetricsCol)
	}
	// The word must be the NAMED one (Rule 1/13): a second spelling of the same
	// state would be a mirror of the constant, and the row would then have two
	// vocabularies for one fact.
	if got != statusMetricsUnmeasured {
		t.Errorf("the all-unmeasured group renders %q, want the named constant %q — the row's "+
			"state vocabulary has one definition", got, statusMetricsUnmeasured)
	}
	// The reservation is what makes the word affordable at all: it must fit the
	// column WITHOUT re-partitioning the row's geometry (the compile-time budget
	// guard stays exact).
	if w := lineedit.DisplayWidth(statusMetricsUnmeasured); w > statusMetricsCol {
		t.Fatalf("statusMetricsUnmeasured is %d cells, over the %d-cell column: the word would "+
			"force a re-partition or be cut by the row's own field cut", w, statusMetricsCol)
	}
}

// TestStatusMetricsUnmeasuredGroupOnTheComposedRow drives the same state through
// the REAL row and reads the METRICS COLUMN's cells out of it, located from the
// row's own geometry (statusRowColumns + statusColumnNames) — so the assertion
// cannot drift from what the layout draws.
//
// This is the leg that would have caught the defect as the operator met it: the
// group function returning "" is one thing, and the row carrying a reserved-and-
// blank segment between two separators is what the reader actually sees.
func TestStatusMetricsUnmeasuredGroupOnTheComposedRow(t *testing.T) {
	s := unmeasuredStatusSource()
	s.setAvailCols(statusColumnsAtCols)
	row := s.status().Line(statusColumnsAtCols)

	cell := ""
	for _, f := range statusHonestyFieldCells([]rune(row)) {
		if f.name == statusColumnNameMetrics {
			cell = strings.TrimSpace(f.text)
		}
	}
	if problems := statusMetricsGroupProblems(cell); len(problems) > 0 {
		t.Errorf("the row's metrics column breaks its honesty clause:\n  %s\nrow = %q",
			strings.Join(problems, "\n  "), row)
	}
	// And the two neighboring columns are untouched: the state is stated INSIDE
	// the column's own reservation, so no separator moved and the row is still
	// one screen row inside its pin (the "columns hold still" contract).
	if w := lineedit.DisplayWidth(row); w > statusColumnsAtCols {
		t.Errorf("the row is %d cells at the pin %d:\nrow = %q", w, statusColumnsAtCols, row)
	}
	if !strings.Contains(row, statusNoToolLabel) {
		t.Errorf("the tool column's own idle fact %q left the row:\nrow = %q", statusNoToolLabel, row)
	}
	t.Logf("unmeasured row = %q", row)
}

// TestStatusMetricsHonestyPredicateIsRedOnThePreFixShape is the RED PROOF, kept
// in-repo: the SAME predicate is handed the pre-fix content (the empty string
// the arm returned) and must REFUSE it.
//
// If this test ever passes, the predicate has stopped being about the defect —
// it would accept a blank reserved column, which is the reading the operator
// filed. The pre-fix shape is spelled as the literal it was (never as a call to
// the production function), which is what makes the refusal a statement about
// the shipped behaviour rather than about the current code.
func TestStatusMetricsHonestyPredicateIsRedOnThePreFixShape(t *testing.T) {
	problems := statusMetricsGroupProblems(statusMetricsPreFixUnmeasured)
	if len(problems) == 0 {
		t.Fatalf("the predicate ACCEPTS the pre-fix group content %q: the guard is not "+
			"about the defect it names — a reserved-and-blank column is exactly what the "+
			"reader filed", statusMetricsPreFixUnmeasured)
	}
	// The refusal must NAME the blank: a predicate that rejected the empty string
	// for some other reason (say, an over-wide fixture) would be red for the wrong
	// cause, and a red proof is only worth its name when it fails for the reason
	// it states.
	blank := false
	for _, p := range problems {
		if strings.Contains(p, "BLANK") {
			blank = true
		}
	}
	if !blank {
		t.Errorf("the predicate refuses the pre-fix content but not BY NAME (problems: %v) — "+
			"the red proof must fail for the reason the defect is", problems)
	}

	// THE REGRESSION GUARD, on the live row: the group's arm is no longer the
	// pre-fix content, so the pre-fix row shape cannot reappear. A second red
	// proof on a value the fix DOES produce, so the two cases bracket the change.
	s := unmeasuredStatusSource()
	s.setAvailCols(statusColumnsAtCols)
	if row := s.status().Line(statusColumnsAtCols); len(statusMetricsGroupProblems(statusMetricsField(statusOps{}, 0))) > 0 {
		t.Errorf("the shipped group content is refused by the guard:\nrow = %q", row)
	}
	t.Logf("pre-fix content %q refused with: %s", statusMetricsPreFixUnmeasured, problems[0])
}
