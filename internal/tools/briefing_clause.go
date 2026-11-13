package tools

// briefing_clause.go — the ONE way a briefing acquires a freshness clause, and
// the ONE place the "did anything get appended?" decision is made.
//
// WHY IT IS A FUNCTION AND NOT AN `if clause != ""` AT EACH SEAM. Four clauses
// now ride this seam (base revision, landed/unlanded, gate assertion, figure —
// epic 01a14242 added the third), and each one's call site decides whether to
// append a marker line to the run's facts. A caller that re-derives that
// decision from the text (`if clause != ""`) is asking a SECOND question whose
// answer can drift from the text it appends: the marker says "stamped into the
// child's briefing" while the child received the briefing unchanged — a
// fabricated claim on the run's record, which is the same defect class the
// clauses exist to prevent.
//
// So the appended text and the flag are returned by ONE call, and the flag
// cannot disagree with the bytes because the bytes are produced by it. The
// no-op arm is a BYTE-IDENTICAL return of the input, which is the property a
// seam that degrades every briefing it touches would violate.

import "strings"

// BriefingWithClause appends a freshness clause to a briefing, separated by a
// blank line, and reports whether anything was appended at all.
//
// An empty (or whitespace-only) clause is the ordinary case — the briefing
// asserts nothing this clause covers — and returns the briefing UNCHANGED and
// false. The returned string is the exact text the child must receive; a caller
// that appends anything else has built a second answer (Rule 13).
func BriefingWithClause(briefing, clause string) (string, bool) {
	c := strings.TrimRight(strings.TrimSpace(clause), "\n")
	if c == "" {
		return briefing, false
	}
	return strings.Join([]string{briefing, c}, "\n\n"), true
}
