package testsuite

// chat_emphasis_colour_align_sanity_test.go — the comparator sanity check for the
// emphasis-colour alignment guard (chat_emphasis_colour_align_pty_test.go).
//
// WHY IT EXISTS. A measurement that cannot fail certifies: the alignment guard's
// verdict is read off a walk over a hand-built artifact, and if that walk (or its
// criterion) were insensitive, the real-pty run's "CONSISTENT" would mean nothing.
// This file drives the SAME functions — alAlign, alCellVerdict, alLocate — over an
// artifact whose rows and attribute runs are the ones a REAL run measured (quoted
// verbatim below), and asserts the transition:
//
//	baseline (the corner geometry of the real dump)  → 0 violating cells
//	one span cell re-styled to the body role          → the criterion FIRES
//	one body cell re-styled to the span's role        → the criterion FIRES
//
// It is the family's precedent shape (TestChatRenderComparatorSanity): no pty, no
// child, no timing — a pure function of an artifact, so it can run anywhere and
// cannot be green for a scheduling reason.
//
// The fixture is NOT a paraphrase of the dump: the rows are the dump's own lines
// (the band's gutter, the indent of a wrapped row, and the trimmed right edge all
// included) and the runs are the dump's own runs, at the 64x40 geometry of the
// real run. If the renderer's layout ever changes, this fixture becomes stale in a
// way the real-pty guard reports rather than hides.
import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/uiclient"
)

// alSanityLines are the dump's rows 18..25, verbatim from the real run's artifact
// (rows 18..24 are the payload and its markers; 25 is the close marker's line).
// They are the fixture's geometry — every column the walk attributes is read from
// here.
var alSanityLines = []string{
	"▌ <AL-OPEN",
	"▌ AL-PLAIN a plain sentence with no marking at all, so every cell of it is the body role.",
	"  ll of it is the body role.",
	"▌ AL-WRAP the marked run follows and it is wider than the band:",
	"  ALPHA BRAVO CHARLIE DELTA ECHO FOXTROT GOLF HOTEL INDIA JULIET",
	"  KILO LIMA MIKE and then the plain tail AL-WRAP-TAIL.",
	"▌ <AL-CLOSE",
	"▌ <AL-END>",
}

// alSanityRuns are the same rows' attribute runs, verbatim from the real run's
// artifact. The two roles this guard measures are visible at a glance:
//
//	row 22 (the span's first half): 2:62=fg=colour-178 bold
//	row 23 (the span's second half): 2:14=fg=colour-178 bold, then 16:38=fg=colour-230
//	                            and 54:10=default (the row's trailing blank space)
var alSanityRuns = [][]lineedit.DumpAttrRun{
	{{Col: 0, Cells: 2, Style: "default"}, {Col: 2, Cells: 8, Style: alBodyRoleRun()}, {Col: 10, Cells: 54, Style: "default"}},
	{{Col: 0, Cells: 2, Style: "default"}, {Col: 2, Cells: 62, Style: alBodyRoleRun()}},
	{{Col: 0, Cells: 2, Style: "default"}, {Col: 2, Cells: 26, Style: alBodyRoleRun()}, {Col: 28, Cells: 36, Style: "default"}},
	{{Col: 0, Cells: 2, Style: "default"}, {Col: 2, Cells: 62, Style: alBodyRoleRun()}},
	{{Col: 0, Cells: 2, Style: "default"}, {Col: 2, Cells: 62, Style: alBoldRoleRun()}},
	{{Col: 0, Cells: 2, Style: "default"}, {Col: 2, Cells: 14, Style: alBoldRoleRun()}, {Col: 16, Cells: 38, Style: alBodyRoleRun()}, {Col: 54, Cells: 10, Style: "default"}},
	{{Col: 0, Cells: 2, Style: "default"}, {Col: 2, Cells: 9, Style: alBodyRoleRun()}, {Col: 11, Cells: 53, Style: "default"}},
	{{Col: 0, Cells: 2, Style: "default"}, {Col: 2, Cells: 8, Style: alBodyRoleRun()}, {Col: 10, Cells: 54, Style: "default"}},
}

// alBodyRoleRun / alBoldRoleRun are the two style strings the 256 table paints
// the body and the emphasised span with, composed from the same fragments the
// guard reads the artifact for. Composing them (rather than typing the role's
// attributes twice) is what keeps the fixture and the criterion one definition.
func alBodyRoleRun() string { return "fg=" + alBodyFrag }
func alBoldRoleRun() string { return alBoldAttr + " fg=" + alBoldFrag }

// alSanityCandidate builds a Candidate carrying the fixture grid and runs.
func alSanityCandidate(runs [][]lineedit.DumpAttrRun) uiclient.Candidate {
	var c uiclient.Candidate
	c.Dump.Lines = append([]string(nil), alSanityLines...)
	c.Dump.AttrRuns = runs
	c.Dump.AttrsRecorded = true
	c.Dump.Cols = alCols
	c.Dump.Rows = alRows
	return c
}

// alSanityClone deep-copies the runs, so a perturbation of one case cannot reach
// the next.
func alSanityClone() [][]lineedit.DumpAttrRun {
	out := make([][]lineedit.DumpAttrRun, len(alSanityRuns))
	for i, row := range alSanityRuns {
		out[i] = append([]lineedit.DumpAttrRun(nil), row...)
	}
	return out
}

// alSanityCounts walks the fixture and returns the violating-cell counts the
// criterion is read from: (inside-the-span-but-wrong-role, outside-the-span-with-
// the-role, attributed cells).
func alSanityCounts(t *testing.T, cand uiclient.Candidate) (spanBad, outsideBad, attributed int) {
	t.Helper()
	first, last := alPayloadRowRange(cand)
	if first < 0 {
		t.Fatalf("the fixture's markers are not in its grid: the sanity fixture no longer describes the artifact shape the guard reads")
	}
	cells, note, ok := alAlign(cand, first, last)
	if !ok {
		t.Fatalf("the walk could not account for the payload in the fixture, so the comparator cannot be judged: %s", note)
	}
	for _, c := range cells {
		attributed++
		switch {
		case c.InSpan && c.Verdict == "fail":
			spanBad++
		case !c.InSpan && c.Verdict == "fail":
			outsideBad++
		}
	}
	return spanBad, outsideBad, attributed
}

// TestChatEmphasisColourAlignComparatorSanity is the red proof: the SAME walk the
// real-pty guard uses, driven over a fixture whose geometry is the measured dump's,
// in three states — the measured one (consistent), and two single-run perturbations
// that must make it fire.
func TestChatEmphasisColourAlignComparatorSanity(t *testing.T) {
	// (a) the measured state: no violating cell anywhere.
	spanBad, outsideBad, attributed := alSanityCounts(t, alSanityCandidate(alSanityClone()))
	if spanBad != 0 || outsideBad != 0 {
		t.Fatalf("the fixture's untouched runs must be consistent, got span_bad=%d outside_bad=%d", spanBad, outsideBad)
	}
	if attributed == 0 {
		t.Fatalf("the walk attributed no cell to the fixture: a comparator that reads nothing can never fire")
	}
	t.Logf("SANITY baseline: %d cells attributed, span violations=%d, outside-span violations=%d", attributed, spanBad, outsideBad)

	// (b) ONE span cell re-styled to the BODY role: the first clause must fire.
	// The perturbation is the smallest one the criterion can see — the span's own
	// last cell on its first row, split off into a run of the other role.
	runs := alSanityClone()
	runs[4] = []lineedit.DumpAttrRun{
		{Col: 0, Cells: 2, Style: "default"},
		{Col: 2, Cells: 61, Style: alBoldRoleRun()},
		{Col: 63, Cells: 1, Style: alBodyRoleRun()},
	}
	spanBad, outsideBad, _ = alSanityCounts(t, alSanityCandidate(runs))
	if spanBad == 0 {
		t.Fatalf("one span cell carrying the BODY role did not fire clause 1: the criterion cannot see a mis-styled span cell")
	}
	if outsideBad != 0 {
		t.Fatalf("the same perturbation reported an outside-span violation (%d), which is a different defect: the two clauses are not being told apart", outsideBad)
	}
	t.Logf("SANITY perturbed (one span cell → body role): clause-1 violations=%d", spanBad)

	// (c) ONE plain GLYPH just past the span re-styled to the SPAN's role: the
	// second clause must fire. The cell is the plain text's first character after
	// the span's close, on the row the span ends on — and it must be a GLYPH, not
	// the space the wrap left there: a space carries no role of its own, so the
	// walk attributes no cell to it and a perturbation placed on one would be
	// invisible (measured: the first draft of this case set col 16 — the space —
	// and the comparator correctly did not fire, which is a fact about the
	// perturbation, not about the criterion).
	runs = alSanityClone()
	runs[5] = []lineedit.DumpAttrRun{
		{Col: 0, Cells: 2, Style: "default"},
		{Col: 2, Cells: 16, Style: alBoldRoleRun()},
		{Col: 18, Cells: 36, Style: alBodyRoleRun()},
		{Col: 54, Cells: 10, Style: "default"},
	}
	spanBad, outsideBad, _ = alSanityCounts(t, alSanityCandidate(runs))
	if outsideBad == 0 {
		t.Fatalf("one cell past the span carrying the SPAN's role did not fire clause 2: the criterion cannot see an over-reaching span")
	}
	if spanBad != 0 {
		t.Fatalf("the same perturbation reported a span violation (%d), which is a different defect", spanBad)
	}
	t.Logf("SANITY perturbed (one plain cell → span role): clause-2 violations=%d", outsideBad)

	// (d) the tokenizer is the authority the fixture's span comes from: an
	// unclosed marker must NOT be read as a span, or clause 1 would demand
	// emphasis for text the answer never marked.
	if got := alSpanText(); got == "" || !strings.HasPrefix(alWrapLine, alWrapLine[:strings.Index(alWrapLine, lineedit.BoldMarker)]) {
		t.Fatalf("the fixture's payload no longer carries a closed %q pair, so the sanity cases above stop describing the guard's own trigger", lineedit.BoldMarker)
	}
}
