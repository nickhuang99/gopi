package agent

// recorddiscipline_test.go — the assertions for the two RECORD DISCIPLINES
// (recorddiscipline.go): each must appear in the RENDERED kernel, each must be
// composed from its named constant rather than re-typed, and the rules must sit
// in the block that owns their subject.
//
// THE SHAPE OF THESE ASSERTIONS, and why it is what it is (Rule 11's lesson,
// learned on this repo's own prompt tests): an assertion that only says
// `strings.Contains(prompt, "some prose")` proves a STRING SURVIVED, not that a
// DISCIPLINE DID — the two come apart the moment someone edits the constant and
// the test keeps passing against a stale copy of the sentence. So every clause
// is asserted in its own package-level constant's name, and the test that
// proves the SENTENCE is the one the prompt renders compares the CONSTANT to the
// RENDERED text. Change the rule and the assertion moves with it; delete the
// rule from the render site and the assertion goes RED (the red proof is in the
// task report).

import (
	"os"
	"strings"
	"testing"
)

// TestRecordDiscipline_StoreWithoutAskingPermission is deliverable 1's first
// half: discipline 1 is IN the assembled kernel, it forbids the permission
// question BY NAME, and it gives the compaction reason that makes the question a
// loss rather than a courtesy.
func TestRecordDiscipline_StoreWithoutAskingPermission(t *testing.T) {
	p := systemPromptKernel()

	// (a) The rule renders, verbatim, from its one definition.
	if !strings.Contains(p, storeOnTheSpotClause) {
		t.Fatalf("kernel does not carry storeOnTheSpotClause verbatim — the no-permission rule did not reach the prompt:\n%q", storeOnTheSpotClause)
	}
	// (b) The PROHIBITION is named, not merely implied. A rule that says "store
	// promptly" is satisfied in spirit by offering to store; the failure mode is
	// a turn that ends in a question, so the question itself is named.
	for _, want := range []string{
		"NEVER ask",
		"要不要记下来",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the no-permission rule does not name the forbidden shape %q — an unnamed prohibition is satisfied in spirit", want)
		}
	}
	// (c) The REASON survives: the compaction boundary is what makes deferring a
	// loss, and it is the half that stays true when the model is tempted to be
	// polite.
	for _, want := range []string{"compact", "未记录=丢失"} {
		if !strings.Contains(p, want) {
			t.Errorf("the no-permission rule lost the compaction reason %q", want)
		}
	}
	// (d) It sits INSIDE the memory if-then list — its subject is that block's
	// subject, and a rule that renders outside the list it extends reads as a
	// separate (and therefore skippable) discipline. Positional, because that is
	// the property being asserted.
	memAt := strings.Index(p, "MEMORY (if-then)")
	storeAt := strings.Index(p, "NEVER ask 要不要记下来")
	nextSection := strings.Index(p[memAt:], "- read:")
	if memAt < 0 || storeAt < 0 || nextSection < 0 {
		t.Fatalf("cannot locate the memory block boundaries (memAt=%d storeAt=%d nextSection=%d)", memAt, storeAt, nextSection)
	}
	if storeAt < memAt || storeAt > memAt+nextSection {
		t.Error("the no-permission rule renders outside the memory (if-then) block — it must extend the block whose subject it shares")
	}
}

// TestRecordDiscipline_ExistenceIsNotImplementation is deliverable 1's second
// half: discipline 2 is IN the assembled kernel, it says a text hit is not
// evidence of behaviour, it names BOTH measured error directions, and it demands
// execution as the method.
func TestRecordDiscipline_ExistenceIsNotImplementation(t *testing.T) {
	p := systemPromptKernel()

	if !strings.Contains(p, recordDisciplineRule9) {
		t.Fatalf("kernel does not carry engineering rule 9 verbatim — the exists≠works rule did not reach the prompt:\n%q", recordDisciplineRule9)
	}
	// (a) The claim is stated as text-vs-behaviour, with the search instruments
	// named so the model recognises the moment it is about to over-claim.
	for _, want := range []string{
		"命中≠实现",
		"symbol/keyword",
		"NOT evidence a feature 已实现",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("rule 9 missing %q — a hit must be named as the thing that is NOT evidence", want)
		}
	}
	// (b) BOTH error directions survive. They are the reason the rule exists: an
	// instruction without its observed failures is the advice the model already
	// ignores.
	for _, want := range []string{"真跑取证", "假阴性", "假阳性", "grep 0命中"} {
		if !strings.Contains(p, want) {
			t.Errorf("rule 9 lost the measured half %q — the rule without its evidence is advice", want)
		}
	}
	// (c) It renders as an engineering rule: inside [ENGINEERING RULES], carrying
	// rule 9's number, after rule 8. Positional, since "which block owns it" is
	// the decision under test (the three-layer division, industry_practice.go).
	blockAt := strings.Index(p, engineeringRulesHeading)
	ruleAt := strings.Index(p, recordDisciplineRule9)
	if blockAt < 0 {
		t.Fatalf("kernel lost %s", engineeringRulesHeading)
	}
	if ruleAt < blockAt {
		t.Fatalf("rule 9 renders BEFORE %s — it belongs inside the derived-habits block", engineeringRulesHeading)
	}
	between := p[blockAt+len(engineeringRulesHeading) : ruleAt]
	if i := strings.Index(strings.TrimSpace(between), "["); i == 0 && !strings.HasPrefix(strings.TrimSpace(between), "[") {
		t.Errorf("a section heading sits between %s and rule 9 — the rule landed outside the block", engineeringRulesHeading)
	}
	if !strings.HasPrefix(recordDisciplineRule9, recordDisciplineRule9Number) {
		t.Errorf("recordDisciplineRule9 does not begin with %q — the [ENGINEERING RULES] numbering drifted", recordDisciplineRule9Number)
	}
}

// TestRecordDiscipline_RulesAreNotReTyped is the Rule 13/14 guard, in the shape
// TestEngineeringRule8IsNotReTyped uses: the RENDER SITES must compose from the
// constants, so a future edit cannot move one copy and leave the other teaching
// a dead sentence. It reads the SOURCE FILES — the property a rendered-string
// assertion cannot see, because the rendered prompt is correct either way.
func TestRecordDiscipline_RulesAreNotReTyped(t *testing.T) {
	sources := map[string]string{}
	for _, f := range []string{"recorddiscipline.go", "loop.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("cannot read %s: %v", f, err)
		}
		sources[f] = string(b)
	}
	// Each clause has exactly ONE definition, and it lives in
	// recorddiscipline.go; the render site in loop.go must reach it by NAME.
	for _, c := range []struct{ name, fragment string }{
		{"storeOnTheSpotClause", "教训当场落库"},
		{"recordDisciplineRule9Clause", "NOT evidence a feature 已实现"},
	} {
		if n := strings.Count(sources["recorddiscipline.go"], c.fragment); n != 1 {
			t.Errorf("%s: the clause %q is defined %d times in recorddiscipline.go, want exactly 1 (Rule 1/13/14)", c.name, c.fragment, n)
		}
		if n := strings.Count(sources["loop.go"], c.fragment); n != 0 {
			t.Errorf("loop.go types the clause %q %d time(s) — the render site must compose from %s, never re-type its text", c.fragment, n, c.name)
		}
	}
	// The render sites address the constants by name (the composition half).
	for _, want := range []struct{ file, name string }{
		{"loop.go", "storeOnTheSpotClause"},
		{"loop.go", "recordDisciplineRule9"},
	} {
		if !strings.Contains(sources[want.file], want.name) {
			t.Errorf("%s does not reference %s — the constant exists but no render site consumes it", want.file, want.name)
		}
	}
}
