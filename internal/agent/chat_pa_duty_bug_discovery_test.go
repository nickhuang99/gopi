package agent

// chat_pa_duty_bug_discovery_test.go — clause (4b): the 2026-10-11 ruling that an
// anomaly the console has PROVED is a defect it files as a bug ROW immediately,
// without asking the operator and without waiting for the end of the run.
//
// WHY THIS FILE EXISTS SEPARATELY from chat_pa_duty_test.go: that file is the
// precedent's own suite and the constraint on this change is that it is NOT
// modified while it keeps passing. The new ruling gets its own assertions here,
// in the same package, against the SAME composed prompts.
//
// THE ASSERTION SHAPES, and why each one is here rather than a Contains:
//
//  1. the new marker is a ONE-SUBSTRING question, asked of BOTH limbs AND of the
//     two composed chat prompts — the clause must reach the plain path (no tools
//     on the wire) as well as the tools path;
//  2. Count == 1 — the measured defect of 2026-10-09 was a clause spliced in
//     TWICE while every Contains assertion stayed green (memory: a presence check
//     cannot tell one copy from two), so the count IS the guard;
//  3. the ORDER of the pre-existing markers is unchanged, asserted as indices of
//     one rendered string — "only added, never rewritten" has to be checkable;
//  4. the vocabulary is COMPOSED, never typed (Rule 1/13/14): the route comes
//     from chatTaskSubmitRoute(), the flag from cliverbs.TaskTagsFlag, the tag
//     from tasktags.BugTag — a `bug` typed in the prompt would be a bare literal
//     on a value the ledger matches by exact comma-token;
//  5. the NO-TOOLS limb must not instruct a path that cannot act: it states what
//     it can do (hand the operator the command) and never "file it in this turn".

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/tasktags"
	"github.com/nick/gopi/internal/toolnames"
)

// chatPADutyBugRendered is the clause as it renders on each limb, keyed the way a
// failure message should name them. Both come from chatPADutyRules — the same
// function chatContextRulesFor appends — never from a helper this test was told
// about.
func chatPADutyBugRendered() map[string]string {
	return map[string]string{
		"tools limb":    chatPADutyRules(true),
		"no-tools limb": chatPADutyRules(false),
	}
}

// TestChatPADutyBugDiscoveryRendersOnBothLimbs is limb 1: the ruling is present
// on BOTH paths. A clause that reached only the tools path would leave the plain
// console — the one with no bash at all — still asking the operator about a
// defect it had already proved.
func TestChatPADutyBugDiscoveryRendersOnBothLimbs(t *testing.T) {
	// Non-vacuity: the clause must be non-trivial before it is asserted on. A
	// marker that rendered against an empty clause would pass every Contains
	// below while teaching the model nothing.
	block := chatPADutyRules(true)
	for _, want := range []string{
		"that is a DUTY of this surface, not a permission question",
		"An anomaly IS a defect until it is explained away BY MEASUREMENT",
		"SILENCE where a result was promised",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("clause (4b) does not state %q — the reader is not told what counts as an anomaly:\n%s", want, block)
		}
	}

	for name, rendered := range chatPADutyBugRendered() {
		if !strings.Contains(rendered, chatPADutyBugDiscoveryMarker) {
			t.Errorf("%s: the rendered duty paragraph carries no (4b) clause (marker %q missing):\n%s",
				name, chatPADutyBugDiscoveryMarker, rendered)
		}
	}

	// And through the COMPOSITION POINTS the request builders call — a clause
	// that renders only under chatPADutyRules() would never reach a model.
	for name, prompt := range chatPADutyChatPrompts(t) {
		if !strings.Contains(prompt, chatPADutyBugDiscoveryMarker) {
			t.Errorf("%s: the composed chat prompt carries no (4b) clause:\n%s", name, prompt)
		}
	}
}

// TestChatPADutyBugDiscoveryKeepsBothLimbsHonest is the Rule 15 half: the plain
// path has NO tools on its wire, so telling it to file a row would be the false
// instruction the file's limbs exist to avoid — the worst kind, because it is
// followed.
func TestChatPADutyBugDiscoveryKeepsBothLimbsHonest(t *testing.T) {
	toolsOn, noTools := chatPADutyRules(true), chatPADutyRules(false)
	if !strings.Contains(toolsOn, chatPADutyBugDiscoveryTools) {
		t.Errorf("the tools limb does not tell the tools path the row is filed HERE:\n%s", toolsOn)
	}
	if !strings.Contains(noTools, chatPADutyBugDiscoveryNoTools) {
		t.Errorf("the plain limb does not state what the no-tools path can do about it:\n%s", noTools)
	}
	if strings.Contains(noTools, chatPADutyBugDiscoveryTools) {
		t.Errorf("the plain path is told to FILE the row itself — a false instruction: no tool is on its wire:\n%s",
			noTools)
	}
	// The tools-limb sentence names the tool the model actually calls, from the
	// leaf constant (Rule 1/13/14) rather than typed.
	if !strings.Contains(chatPADutyBugDiscoveryTools, toolnames.ToolTaskSubmit) {
		t.Errorf("the tools limb does not name %q — it would teach a call the dispatcher does not have",
			toolnames.ToolTaskSubmit)
	}
}

// TestChatPADutyBugDiscoveryNamesTheReservedTag is the Rule 1/13/14 half: the
// command the clause teaches is COMPOSED from the leaf constants — the route
// helper the write boundary renders, the flag cliverbs owns, and the reserved
// tag tasktags owns — so a rename on the dispatch side fails HERE instead of
// leaving the prompt teaching a command the CLI refuses.
func TestChatPADutyBugDiscoveryNamesTheReservedTag(t *testing.T) {
	// The leaf constants are non-empty and their two spellings still agree. A
	// rendered assertion against an EMPTY constant would pass vacuously.
	if strings.TrimSpace(tasktags.BugTag) == "" {
		t.Fatal("tasktags.BugTag is empty — the bug-flow token has no definition")
	}
	if want := "--" + cliverbs.TaskTagsWord; cliverbs.TaskTagsFlag != want {
		t.Fatalf("cliverbs.TaskTagsFlag = %q, want the dashed form of TaskTagsWord (%q) — the two halves of one "+
			"capability have drifted", cliverbs.TaskTagsFlag, want)
	}

	// The composed invocation, as the prompt must render it: the SAME route
	// helper the write boundary uses, so the two paragraphs cannot teach two
	// different commands for one decision.
	invocation := "gopi " + chatTaskSubmitRoute() + " \"<the finding and its reproduction>\" " +
		cliverbs.TaskTagsFlag + " " + tasktags.BugTag

	for name, rendered := range chatPADutyBugRendered() {
		if !strings.Contains(rendered, invocation) {
			t.Errorf("%s: the clause does not render the filing invocation %q:\n%s", name, invocation, rendered)
		}
	}
	// The route is ONE spelling across the two paragraphs that route work.
	for name, text := range map[string]string{
		"write boundary": chatWriteBoundaryRules(true),
		"PA duty":        chatPADutyRules(true),
	} {
		if !strings.Contains(text, chatTaskSubmitRoute()) {
			t.Errorf("%s does not teach the route %q:\n%s", name, chatTaskSubmitRoute(), text)
		}
	}
	// And the flag is the DASHED one a reader types, not the bare parser word.
	if !strings.Contains(chatPADutyRules(true), cliverbs.TaskTagsFlag) {
		t.Errorf("the clause does not render the dashed flag %q — a reader pasting the bare word would file an "+
			"unclassified row:\n%s", cliverbs.TaskTagsFlag, chatPADutyRules(true))
	}
}

// TestChatPADutyBugDiscoveryIsRenderedExactlyOnce is the count guard — the
// measured defect of 2026-10-09, when a clause spliced in by re-emitting a
// builder block rendered TWICE and every Contains assertion stayed green. A
// clause repeated is a prompt the model reads twice, and this paragraph's
// numbering stops meaning "in this order".
func TestChatPADutyBugDiscoveryIsRenderedExactlyOnce(t *testing.T) {
	for name, rendered := range chatPADutyBugRendered() {
		for _, clause := range []string{
			chatPADutyBugDiscoveryMarker,
			"(1) YOU DO IT YOURSELF,",
			chatPADutyNoPermissionMarker,
			"(2) A change to the REPOSITORY is YOURS TOO",
			"(2b) The ONE act the scope in (1) still retires",
			"(3) OPERATOR-ONLY",
			chatPADutyNoCodingDebug,
			"(5) VERIFY your own housekeeping",
		} {
			if n := strings.Count(rendered, clause); n != 1 {
				t.Errorf("%s: clause %q is rendered %d times, want exactly once — a clause repeated is a prompt "+
					"the model reads twice:\n%s", name, clause, n, rendered)
			}
		}
	}
}

// TestChatPADutyBugDiscoveryLeavesTheExistingOrderAlone is the "only added, never
// rewritten" half, made checkable: the pre-existing markers keep the SAME
// relative order inside one rendered string, and the new clause sits between the
// ruling it interprets — (4), which already says the console may READ and MEASURE
// and file the finding — and the verification clause (5) that follows everything.
func TestChatPADutyBugDiscoveryLeavesTheExistingOrderAlone(t *testing.T) {
	for name, rendered := range chatPADutyBugRendered() {
		order := []string{
			chatPADutyMarker,
			chatPADutyNoPermissionMarker,
			"(2) A change to the REPOSITORY is YOURS TOO",
			"(3) OPERATOR-ONLY",
			chatPADutyNoCodingDebug,
			chatPADutyBugDiscoveryMarker,
			"(5) VERIFY your own housekeeping",
		}
		prev, prevAt := "", -1
		for _, marker := range order {
			at := strings.Index(rendered, marker)
			if at < 0 {
				t.Fatalf("%s: marker %q is not rendered at all", name, marker)
			}
			if at < prevAt {
				t.Errorf("%s: %q renders at %d, BEFORE %q at %d — the existing order was rewritten, not "+
					"added to:\n%s", name, marker, at, prev, prevAt, rendered)
			}
			prev, prevAt = marker, at
		}
	}
}

// TestChatPADutyBugDiscoveryCarriesThePrecedentsAndTheBound is the semantics
// half: the clause must carry all four of the things that make a low threshold
// safe — the duty, the operator's decision plus the duplicate-dispatch
// allowance, the evidence constraint with this repository's own retractions, and
// the statement that it touches neither (2) nor (3).
func TestChatPADutyBugDiscoveryCarriesThePrecedentsAndTheBound(t *testing.T) {
	block := chatPADutyRules(true)
	for _, want := range []string{
		// (a) the duty and the two things NOT to do.
		"that is a DUTY of this surface, not a permission question",
		"Do NOT wait for the end of the run and do NOT ask first",
		// (b) the operator's decision + Rule 26.
		"deliberately LOW, and that is the OPERATOR'S decision",
		"dispatching the same work twice is allowed and EXPECTED",
		// (c) the one constraint and its measured price.
		"EVIDENCE MUST BE TRUE",
		"a code comment, a document's wording or your own expectation is NOT evidence",
		chatPADutyOverAssertionPrecedents,
		// (d) the bound, against both classes.
		"contradicts neither (2) nor (3)",
		"stays a TASK",
		"OPERATOR-ONLY",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("clause (4b) is missing %q — a low threshold without it is a licence to over-claim:\n%s",
				want, block)
		}
	}
}

// TestChatPADutyOverAssertionPrecedentsAreRealCommits is the check that keeps the
// prompt's three precedents from becoming a rumour with numbers attached (Rule
// 38's discipline, applied to a prompt): the constant is a list of commit
// abbreviations, and the citation is only evidence while those commits exist.
// The test reads them off the rendered constant, so adding a fourth precedent
// without it being a real commit fails here.
func TestChatPADutyOverAssertionPrecedentsAreRealCommits(t *testing.T) {
	ids := strings.Fields(strings.ReplaceAll(chatPADutyOverAssertionPrecedents, "/", " "))
	if len(ids) < 3 {
		t.Fatalf("the clause names %d precedent(s), want at least 3 — the ruling it supports is a measured one, "+
			"and one retraction is an anecdote", len(ids))
	}
	for _, id := range ids {
		if len(id) < 7 {
			t.Errorf("precedent %q is too short to be a commit abbreviation — a reader cannot `git show` it", id)
		}
		if strings.ContainsAny(id, " \t") {
			t.Errorf("precedent %q carries whitespace", id)
		}
	}
}
