package tools

// task_briefing_run_alternatives_partial_test.go — the PARTIAL-MATCH seam, driven
// through the SAME real fixture tree as its sibling (runAltFixtureTree /
// runAltBriefing), plus ONE arm against this repository's own tree because the
// epic's incident is a fact about a real package and a real count.
//
// THE DEFECT (measured, PA's own run — quoted verbatim in the ruling below):
//
//	$ go test ./internal/standards/ -run 'Rule13|Rule16|NamedLimits|ErrorWrap' -count=1 -v
//	=== RUN   TestGate1ErrorWrapRedProof
//	--- PASS: TestGate1ErrorWrapRedProof (0.00s)
//	ok  	github.com/nickhuang99/gopi/internal/standards	0.736s
//
// Four alternatives, ONE match (`ErrorWrap` is a fragment of
// `TestGate1ErrorWrapRedProof`), three dead (`Rule13`, `Rule16`, `NamedLimits`
// name RULES, which `-run` does not match). The command really ran, really
// passed, really exited 0 — no `no tests to run` marker and no exit-5 emptiness
// verdict — so the sibling clause is silent by construction and every other
// instrument reads green.
//
// FIVE LEGS:
//
//	① THE PARTIAL SHAPE — some alternatives live, some dead: the clause is
//	  stamped, names ONLY the dead ones, supplies a pasteable replacement, and
//	  states the count per alternative beside it.
//	② POSITIVE CONTROL — a wholly live pattern stamps NOTHING (the ordinary
//	  briefing), and a wholly DEAD one stamps nothing HERE either (that is the
//	  sibling's finding, which `go test` itself marks; two clauses saying
//	  "nothing ran" about one run is the conflation this arm refuses).
//	③ THE REAL CASE — this repo's own `internal/standards` and the incident's
//	  exact selector, so the arm is measured against 200+ real test functions
//	  rather than a fixture's two.
//	④ THE VACUUM FLOOR — an unresolvable package is STATED as unmeasured, never
//	  passed as green (Rule 15).
//	⑤ NO PATTERN / IDEMPOTENCE — a briefing naming no `-run` is silent, and a
//	  second pass over a stamped briefing appends nothing and says so.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// partialRealStandardsPackage is the package the incident's selector named. It is
// spelled as a pattern because that is how an acceptance line writes it.
const partialRealStandardsPackage = "./internal/standards"

// partialIncidentSelector is the selector PA ran, VERBATIM: three alternatives
// that name RULES (no test function carries those words) and one fragment that
// happens to match `TestGate1ErrorWrapRedProof`.
const partialIncidentSelector = "Rule13|Rule16|NamedLimits|ErrorWrap"

// ① THE PARTIAL SHAPE.
func TestBriefingRunAltPartial_NamesTheDeadAlternativesBesideTheLiveOnes(t *testing.T) {
	repo := runAltFixtureTree(t)
	// `TestProbeBeta` is live; `TestTaskReclaimSelfDelivery` is dead.
	clause, record := TaskBriefingRunAltPartialRefresh(repo, runAltBriefing("TestProbeAlpha|TestTaskReclaimSelfDelivery"))

	if clause == "" {
		t.Fatalf("no clause stamped for a PARTIALLY live selector; record=%v", record)
	}
	if !strings.Contains(clause, TaskBriefingRunAltPartialLabel) {
		t.Errorf("clause does not carry the exported label:\n%s", clause)
	}
	if !strings.Contains(clause, "TestTaskReclaimSelfDelivery") {
		t.Errorf("clause does not name the DEAD alternative:\n%s", clause)
	}
	if !strings.Contains(clause, "TestProbeAlpha") {
		t.Errorf("clause does not name the LIVE alternative it travelled with:\n%s", clause)
	}
	if !strings.Contains(clause, TaskBriefingRunAltPartialVerifyOnly) {
		t.Errorf("clause does not carry the instruction constant:\n%s", clause)
	}
	if !strings.Contains(clause, "NOT evidence that the work is done") {
		t.Errorf("clause does not deny the completion reading:\n%s", clause)
	}
	// THE REPAIR: the same command reduced to its surviving alternatives, so the
	// reader is handed a selector that cannot print a green over an unrun gate.
	if !strings.Contains(clause, "REPLACEMENT") ||
		!strings.Contains(clause, "-run 'TestProbeAlpha'") {
		t.Errorf("clause does not supply a pasteable replacement carrying only the live alternatives:\n%s", clause)
	}
	// THE CENSUS: the zero is restated beside the count, so it is checkable.
	if !recordNames(t, record, "0 of", "TestTaskReclaimSelfDelivery") {
		t.Errorf("record carries no zero-count census line for the dead alternative: %v", record)
	}
	if !recordNames(t, record, "1 of", "TestProbeAlpha") {
		t.Errorf("record carries no count for the LIVE alternative: %v", record)
	}
}

// ② POSITIVE CONTROL — the two ordinary shapes stamp nothing.
func TestBriefingRunAltPartial_LeavesTheOrdinarySelectorsAlone(t *testing.T) {
	repo := runAltFixtureTree(t)

	// (a) WHOLLY LIVE: every alternative reaches a test.
	clause, record := TaskBriefingRunAltPartialRefresh(repo, runAltBriefing("TestProbeAlpha|TestProbeBeta"))
	if clause != "" {
		t.Fatalf("a wholly live selector was stamped:\n%s", clause)
	}
	if recordNames(t, record, "PARTIAL", "TestProbeAlpha") {
		t.Errorf("a wholly live selector produced a PARTIAL record: %v", record)
	}

	// (b) WHOLLY DEAD: the sibling's finding, not this arm's — stamping here too
	// would report "nothing ran" about a run that ran nothing, twice.
	clause, record = TaskBriefingRunAltPartialRefresh(repo, runAltBriefing("Rule13|Rule16"))
	if clause != "" {
		t.Fatalf("a wholly DEAD selector was stamped by the partial arm (that is the sibling's "+
			"finding, and `go test` prints its own marker for it):\n%s", clause)
	}
	// The census still records the zeros, so nothing is invisible (Rule 15).
	if !recordNames(t, record, "0 of", "Rule13") {
		t.Errorf("a wholly dead selector left no census line: %v", record)
	}
}

// ③ THE REAL CASE — the incident's own package and selector, read off THIS
// repository. It is the arm that makes the fixture legs more than a tautology:
// 200+ real test functions, and exactly one alternative reaching one of them.
func TestBriefingRunAltPartial_TheIncidentSelectorIsCaughtOnTheRealTree(t *testing.T) {
	root := repoRootFromPackageDir(t) // this repository (test-only helper)
	if _, err := os.Stat(filepath.Join(root, "internal", "standards")); err != nil {
		t.Skipf("this tree carries no internal/standards (%v) — the real-tree arm cannot be "+
			"measured here, and a skip is stated rather than read as a pass", err)
	}
	briefing := "## Acceptance\n\n" +
		"go test " + partialRealStandardsPackage + "/ -run '" + partialIncidentSelector + "' -count=1 -v\n"

	clause, record := TaskBriefingRunAltPartialRefresh(root, briefing)
	if clause == "" {
		t.Fatalf("the incident's selector was NOT caught on this repository's own tree — "+
			"record=%v\nThis is the exact command the epic was opened for:\n%s",
			record, briefing)
	}
	for _, dead := range []string{"Rule13", "Rule16", "NamedLimits"} {
		if !strings.Contains(clause, dead) {
			t.Errorf("clause does not name the DEAD alternative %q:\n%s", dead, clause)
		}
	}
	// `ErrorWrap` is the ONE that reaches a test here — it must be reported LIVE,
	// not dead, or the clause would be describing a different run than the one
	// that happened.
	if !recordNames(t, record, "1 of", "ErrorWrap") {
		t.Errorf("record does not carry the live count for %q: %v", "ErrorWrap", record)
	}
	if !strings.Contains(clause, "REPLACEMENT") || !strings.Contains(clause, "-run 'ErrorWrap'") {
		t.Errorf("clause does not hand back the surviving one-alternative command:\n%s", clause)
	}
	t.Logf("the clause an operator now reads for the epic's own selector:\n%s", clause)
}

// ④ THE VACUUM FLOOR — an unresolvable package is STATED, never passed.
func TestBriefingRunAltPartial_StatesWhatItCouldNotJudge(t *testing.T) {
	repo := runAltFixtureTree(t)
	briefing := "## Acceptance\n\ngo test ./internal/nowhere/ -run 'A|B'\n"
	clause, record := TaskBriefingRunAltPartialRefresh(repo, briefing)
	if clause != "" {
		t.Fatalf("an unresolvable package produced a FINDING:\n%s", clause)
	}
	if !recordNames(t, record, "UNMEASURED", "./internal/nowhere/") {
		t.Fatalf("an unresolvable package was not stated as unmeasured: %v", record)
	}
	// And with no ground at all: the same statement, never a silent pass.
	clause, record = TaskBriefingRunAltPartialRefresh("", briefing)
	if clause != "" {
		t.Fatalf("no ground produced a FINDING:\n%s", clause)
	}
	if !recordNames(t, record, "UNMEASURED", runAltNoRepoReason) {
		t.Fatalf("no ground was not stated as unmeasured: %v", record)
	}
}

// ⑤ NO PATTERN IS A NO-OP, and a second pass stamps nothing twice.
func TestBriefingRunAltPartial_IsSilentWithoutAPatternAndIdempotent(t *testing.T) {
	repo := runAltFixtureTree(t)

	clause, record := TaskBriefingRunAltPartialRefresh(repo, "## Acceptance\n\nrun the suite\n")
	// why: correct — the negated set is the CLAUSE plus the CENSUS RECORD, both written by this arm and neither a step that must still run; the guard is runAltPatternsIn's own emptiness answer and TaskBriefingRunAltPartialRefresh returns at its `len(patterns) == 0` branch BEFORE any filesystem read, git query or regexp compile, so both halves stay zero by construction (had that branch been absent the next statement would render a clause). No idempotent member is wrongly suppressed: the record is a slice of formatted lines, not a sequence of effects. The positive carrier in this same test asserts the non-empty half a few lines below (the fixture selector must stamp on its first pass), which is what stops this arm from passing by never stamping at all.
	if clause != "" || len(record) != 0 {
		t.Fatalf("a briefing naming no `-run` produced output: clause=%q record=%v", clause, record)
	}

	briefing := runAltBriefing("TestProbeAlpha|TestTaskReclaimSelfDelivery")
	first, _ := TaskBriefingRunAltPartialRefresh(repo, briefing)
	if first == "" {
		t.Fatal("fixture invariant: the first pass must stamp")
	}
	second, record := TaskBriefingRunAltPartialRefresh(repo, briefing+"\n"+first+"\n")
	if second != "" {
		t.Fatalf("a second pass stamped a second clause:\n%s", second)
	}
	if !recordNames(t, record, "UNMEASURED", TaskBriefingRunAltPartialAlreadyStampedExcerpt) {
		t.Fatalf("the idempotent pass was not stated: %v", record)
	}
}
