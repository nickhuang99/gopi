package tools

// task_briefing_run_alternatives_over_the_epic_test.go — RULE 31's leg: the
// clause's producer executed against the REAL acceptance line that produced this
// epic, over THIS repository's own test corpus, with the counts printed.
//
// The fixture legs in task_briefing_run_alternatives_test.go prove the PREDICATE
// on a tree built to carry each shape. This file proves the thing the epic
// actually asked for and that no fixture can: that the names which made the
// incident (TestWorktreeRemovalGroundGate, TestTaskReclaimSelfDelivery) are
// reported as ZERO REACH in `./internal/tools/`, while the family that DOES
// exist there (TestEveryWorktreeRemovalArmQuestionsTheGround) is reported with a
// NON-ZERO count.
//
// ONE ALTERNATIVE JOINED THE DEAD SIDE ON 2026-10-31 (task 01a19052):
// `TestSelfDelivery` was live when this arm was written; `task_reclaim_self_delivery_test.go`
// — the only file that declared it — was deleted by the alone-deletion probe
// (DEAD-ALONE, zero production references), so the selector now has THREE dead
// alternatives and ONE live one. The arm is updated rather than dropped, because
// the shape it measures is unchanged and the tree is what moved: an acceptance
// line naming a test nobody implements is still a check that cannot fail.
//
// It reads the live tree, so it is written to be HONEST WHEN THE TREE CHANGES:
// the dead names are asserted dead (that is a fact about this repo's test
// corpus, and if it ever stops being true the incident's shape is gone and this
// arm should be revisited), and the live family is asserted to reach
// at least one test (a count, not a fixed number — adding a sibling test must not
// break it).

import (
	"strings"
	"testing"
)

// epicAcceptanceRunPattern is the epic's acceptance selector, VERBATIM — the
// line this whole clause exists to have caught. It is spelled once so the
// evidence arm and the report quote the same string.
const epicAcceptanceRunPattern = "TestWorktreeRemovalGroundGate|TestTaskReclaimSelfDelivery|TestEveryWorktreeRemovalArmQuestionsTheGround|TestSelfDelivery"

// epicAcceptanceRepoRoot is the ground this arm measures, spelled relative to
// this package's own directory (a test binary runs in its package directory).
const epicAcceptanceRepoRoot = "../.."

// epicAcceptancePackage is the package pattern the same command names — the
// package whose own test corpus is counted.
const epicAcceptancePackage = "./internal/tools/"

// TestBriefingRunAlt_OverTheEpicsAcceptanceLine names the dead
// alternatives and counts the live one, against the real tree.
func TestBriefingRunAlt_OverTheEpicsAcceptanceLine(t *testing.T) {
	// The clause is given the REPOSITORY ROOT — two levels up from this
	// package's own directory, which is a test binary's working directory — so
	// the scan reads the tree under test, not a copy of it.
	briefing := "## Acceptance\n\ngo test " + epicAcceptancePackage + " -run '" + epicAcceptanceRunPattern + "'\n"
	clause, record := TaskBriefingRunAlternativesRefresh(epicAcceptanceRepoRoot, briefing)

	if clause == "" {
		t.Fatalf("the epic's acceptance line stamped nothing — the two dead alternatives went unreported; record=%v", record)
	}
	// 1) the ZERO-reach alternatives are NAMED.
	for _, dead := range []string{"TestWorktreeRemovalGroundGate", "TestTaskReclaimSelfDelivery", "TestSelfDelivery"} {
		if !strings.Contains(clause, "- alternative \""+dead+"\"") {
			t.Errorf("clause does not name %q as dead:\n%s", dead, clause)
		}
		if !recordNames(t, record, runAltZeroReachArrow+dead+`"`) {
			t.Errorf("record carries no ZERO REACH line for %q: %v", dead, record)
		}
	}
	// 2) the LIVE family is NOT named as dead, and its census line carries a
	//    count — the honest half of the measurement.
	for _, live := range []string{"TestEveryWorktreeRemovalArmQuestionsTheGround"} {
		if strings.Contains(clause, "- alternative \""+live+"\"") {
			t.Errorf("clause names %q as dead, but the package implements it:\n%s", live, clause)
		}
		if !recordNames(t, record, runAltCensusArrow+live+`"`, "top-level test function(s) of") {
			t.Errorf("no census line for the live alternative %q: %v", live, record)
		}
		if recordNames(t, record, runAltZeroReachArrow+live+`"`) {
			t.Errorf("record reports the live alternative %q as ZERO REACH: %v", live, record)
		}
	}
	// 3) every census line prints a number, so a zero is visible against its
	//    neighbours in the acceptance output (the epic's honest minimum).
	if !recordNames(t, record, runAltCensusArrow, "top-level test function(s) of") {
		t.Errorf("census arithmetic missing from the record: %v", record)
	}
	// 4) the clause never claims completion.
	if !strings.Contains(clause, "NOT evidence that the work is done") {
		t.Errorf("clause does not deny the completion reading:\n%s", clause)
	}
}

// runAltCensusArrow is the census line's own opening token, spelled once so the
// assertion above reads the line the producer writes rather than a paraphrase.
const runAltCensusArrow = "briefing -run census → \""

// runAltZeroReachArrow is the ZERO REACH line's own opening, spelled once: the
// quoted alternative follows it, and the QUOTE is load-bearing — the line also
// quotes the whole briefing line, which carries every alternative's name, so an
// unquoted token would match inside a neighbour's line.
const runAltZeroReachArrow = "briefing -run census → ZERO REACH \""
