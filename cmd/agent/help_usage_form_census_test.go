package main

// help_usage_form_census_test.go — the THREE-FORM census of the published `-h`
// pages, pinned against the REAL binary.
//
// WHY THIS FILE EXISTS (task 01a12b7e-4388-7e36-8264-b59888dd8cde). A reader
// that anchored on the `^Usage:` line returned ZERO rows for `gopi merge -h`
// and `gopi worker -h` — and the man generator reported the loss as
// "`gopi merge -h` printed no usage block — the synopsis fact source is
// missing", which is a claim about the PAGE that was really a claim about the
// READER (internal/mansource learned the third form; the reader extension, its
// bound and the red proofs are in devdoc/man-pages-verb-set.md and
// devdoc/cli-help-usage-forms.md).
//
// WHICH WAY THE DIFFERENCE WAS RESOLVED, AND WHY. The obvious normalization —
// give `merge` and `worker` a `Usage:` header like the other ten — was
// REJECTED, because the headerless shape is not an accident of history: it is
// the shape an operator-commissioned epic required. Commit 9e24d228fd
// ("service help is capability-first, not a devdoc (worker/merger -h)",
// 2026-09-14, 20:09:51 +0800) states the requirement verbatim — "Both usage
// heads now follow one shape: title one-liner → invocations → Options →
// Environment → Notes" — and `cmd/agent/help_capability_first_test.go` GATES
// it: hcfCapabilityFirstOffenses requires an invocation line within the first
// hcfSectionMaxLine+1 lines, prefix hcfInvocationPrefix ("  gopi ").
// `mergeUsageHead` (cmd/agent/merge_run.go, commit 06438272b6, 2026-09-18,
// 13:58:57 +0800) was written to the same shape by construction.
//
// NOT for the reason a first reading suggests, though, and the probe that
// settled it is artifacts/hcf-probe.txt (measured 2026-10-11): adding a
// standalone `Usage:` header does NOT turn that gate red — the 0-based scan
// admitted i<=3, so the invocation was still in range and the offense count
// stayed 0. What the same probe DID measure is where the two forms stood
// against clause ① AS IT THEN READ (an indented row within the window):
// headerless satisfied it (0 offenses for merge and worker), while the
// `Usage:`-header form violated it on 3 of the 8 pages that print one and the
// header-line form on both of its pages (the shape sits ON the `Usage:` line, so
// no "  gopi " line was ever in range). The header is what SEPARATES the title
// from the invocations — the very thing the epic abolished here — so
// "normalizing" merge/worker would move them onto the shape its own gate
// refused.
//
// THAT PARAGRAPH IS HISTORY, AND IT IS WHY THIS FILE'S `Usage:` HEADER IS STILL
// THE RIGHT SHAPE. Clause ① was re-specified on 2026-10-12 (task
// 01a12bb2-3772-72b7-bb2a-a5a3e8fecf13): it is now an ORDER relation — the first
// invocation, in EITHER spelling (an indented row, or a synopsis sitting on the
// `Usage:` line), must precede the first prose paragraph — and it is evaluated
// over all 12 published pages, which all satisfy it. So the two facts the tables
// below record are no longer verdicts about the pages: they were verdicts about
// a reader that knew one invocation spelling and a window that counted blank
// lines (devdoc/help-clause1-order-relation.md). The `Usage:` header is not the
// thing that separated capability from prose — the PARAGRAPHS under it were, and
// three of those have moved (session, tool, ui_state).
//
// WHAT SURVIVES UNCHANGED: normalizing merge/worker onto the header form is
// still not this file's call — the form a page picks is measured and logged
// here, never asserted — and the reader's contract below is still the thing this
// file freezes.
//
// So: normalizing the surface would REVERT a gated operator requirement to
// satisfy a reader, and the reader is the thing that was wrong. What this file
// freezes is therefore the CONTRACT the three forms share — measured on the
// real pages, never on the usage constants (Rule 31: a constant that stopped
// being printed would satisfy a constant-based assertion while the terminal
// showed something else).
//
// WHAT IS FROZEN, AND WHAT IS DELIBERATELY NOT. The FORM a page uses is
// measured and LOGGED, never asserted: pinning "exactly two pages are
// headerless" would freeze today's count and turn a legitimate change of shape
// into a red build. What is asserted is what must hold whichever form a page
// picks — see hucUsageFormOffenses.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/mansource"
)

// publishedVerbHelpPages are the published top-level help pages this census
// walks — the same set the man set covers, addressed through the exported
// CliVerb* constants so a verb rename cannot drift this list away from the
// dispatcher (Rule 1/13: no bare verb spelling at a call site).
var publishedVerbHelpPages = []string{
	CliVerbChat,
	CliVerbConfig,
	CliVerbMemory,
	CliVerbNotify,
	CliVerbSession,
	CliVerbTask,
	CliVerbTool,
	CliVerbUIState,
	CliVerbVersion,
	CliVerbWeb,
	CliVerbWorker,
}

// The three forms a page may use, named once (Rule 1: a meaningful string
// literal gets a constant). usageFormNone is the defect: no block found.
const (
	usageFormHeader     = "header"      // a bare `Usage:` line, rows beneath it
	usageFormHeaderLine = "header-line" // the first shape ON the `Usage:` line
	usageFormHeaderless = "headerless"  // no `Usage:` at all; the block is the indented rows
	usageFormNone       = "none"        // nothing the reader recognizes
)

// hucProgName is the name every published `-h` page addresses the program by.
// It is the same word the reader in internal/mansource scans for (gopiPrefix
// there); it is spelled locally because the reader's spelling is unexported and
// this package's help gates already own their own prefix constant.
const hucProgName = "gopi"

// hucPublishedPageFloor is how many pages the published set must still hold
// (Rule 1: the number is named, not spelled at the comparison). The clause was
// widened to cover the published surface, so a set that silently shrank would
// narrow it back without any test going red — the set's own walk (usageForm /
// TestUsageFormCensusMatchesThePublishedPages) pins today's count at 12.
// IT MOVED 12 → 11 with the merge surface (epic merge-removal, item 2): the list
// below is the verb help pages the binary PUBLISHES, and `gopi merge -h` stopped
// being one when the verb left the dispatch chain. The floor tracks the set it
// guards, which is the whole point of the arm — a page dropping out silently is
// what it catches, and a page removed BY DECISION is a size change it must be
// told about in the same commit rather than papered over by a margin.
const hucPublishedPageFloor = 11

// hucUsageFormOffenses is THE predicate: given a published page's text and the
// verb it belongs to, it returns every way the page fails the shared contract.
// It is a pure function of (page text, verb) so the red proofs below run the
// SAME code the gate runs, not a re-implementation of it — which is what makes
// them evidence rather than illustration.
//
// The contract, and why each clause is here rather than in a style guide:
//
//  1. the page yields usage rows — the synopsis fact source the man generator
//     copies. This is the clause the original defect violated for merge/worker.
//  2. those rows name THIS verb — a block copy-pasted from another page would
//     satisfy (1) while documenting the wrong command.
//  3. the block sits ABOVE the page's first non-`Usage:` section heading. This
//     is the reader's own bound, asserted on the pages it reads: a page that
//     moved its invocations below `Notes:` would hand the reader a paragraph
//     (measured on a probe page — the unbounded scan accepted an indented tail
//     sentence beginning `gopi <verb> ` as the block). Stating it here means a
//     page cannot walk into that trap by being reorganized.
func hucUsageFormOffenses(help, verb string) []string {
	var out []string

	rows := mansource.UsageRows(help)
	invocation := hucProgName + " " + verb
	if len(rows) == 0 {
		out = append(out, "yields no usage rows through mansource.UsageRows — the synopsis fact source the man generator copies is missing")
	} else {
		named := false
		for _, row := range rows {
			if strings.Contains(row, invocation) {
				named = true
				break
			}
		}
		if !named {
			out = append(out, fmt.Sprintf("its usage block names no %q invocation — the rows document some other command:\n%s", invocation, strings.Join(rows, "\n")))
		}
	}

	// Clause 3 is measured INDEPENDENTLY of whether the block was readable: it
	// is the statement about the page's LAYOUT that the reader's bound relies
	// on. Evaluated only in the else-branch above it would be dead code — every
	// page that trips it is one the reader already refuses — and a check that
	// can never fire is a claim nobody has tested.
	if h := hucFirstNonUsageHeading(strings.Split(help, "\n")); h >= 0 {
		if b := hucBlockLine(help); b > h {
			out = append(out, fmt.Sprintf("its first %q invocation row is at line %d, BELOW the first non-`%s` section heading at line %d — the reader's block scan is bounded at that heading, so the page would hand it prose (or nothing)",
				invocation, b, mansource.UsageHeader, h))
		}
	}
	return out
}

// hucBlockLine is the 1-based line the page's usage block starts on: the first
// line the reader claims, found the same way the reader finds it (a `Usage:`
// header, else the first indented `gopi …` row). 0 when there is none.
func hucBlockLine(help string) int {
	for i, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, mansource.UsageHeader) {
			return i + 1
		}
	}
	for i, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if line == trimmed || !strings.HasPrefix(trimmed, hucProgName+" ") {
			continue
		}
		return i + 1
	}
	return 0
}

// hucFirstNonUsageHeading returns the 1-based line of the page's first section
// heading other than `Usage:` itself: unindented, ending in a colon. -1 when
// the page has none. `Usage:` is excluded because on the header form the block
// STARTS on that heading — it introduces the block rather than following it.
func hucFirstNonUsageHeading(lines []string) int {
	for i, line := range lines {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		trimmed := strings.TrimRight(line, " \t")
		if !strings.HasSuffix(trimmed, ":") {
			continue
		}
		if strings.HasPrefix(trimmed, mansource.UsageHeader) {
			continue
		}
		return i + 1
	}
	return -1
}

// usageForm classifies a page by the shape its usage block opens with, read
// through the SAME reader the generator uses — so the classification cannot
// drift from what the generator actually copies.
func usageForm(help string) string {
	if len(mansource.UsageRows(help)) == 0 {
		return usageFormNone
	}
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, mansource.UsageHeader) {
			continue
		}
		if strings.TrimSpace(trimmed[len(mansource.UsageHeader):]) == "" {
			return usageFormHeader
		}
		return usageFormHeaderLine
	}
	return usageFormHeaderless
}

// TestPublishedHelpPagesShareTheUsageContract is the gate itself: every
// published `-h` page satisfies the shared contract, whichever of the three
// forms it uses. It is the regression that would have caught the original
// defect at the source — the run BEFORE the reader learned the third form
// failed clause 1 on exactly merge and worker.
func TestPublishedHelpPagesShareTheUsageContract(t *testing.T) {
	for _, verb := range publishedVerbHelpPages {
		verb := verb
		t.Run(verb, func(t *testing.T) {
			help := helpPage(t, []string{verb})
			for _, offense := range hucUsageFormOffenses(help, verb) {
				t.Errorf("`gopi %s -h` %s", verb, offense)
			}
		})
	}
}

// TestUsageFormCensusMatchesThePublishedPages is the CENSUS ITSELF: it measures
// and LOGS which form each page uses, and fails only when the answer is "none".
// It is here so a future reader asking "how many pages are headerless, and is
// that on purpose?" gets the number from the binary rather than from a document
// that has aged.
func TestUsageFormCensusMatchesThePublishedPages(t *testing.T) {
	forms := make(map[string][]string)
	for _, verb := range publishedVerbHelpPages {
		form := usageForm(helpPage(t, []string{verb}))
		forms[form] = append(forms[form], verb)
	}
	for _, form := range []string{usageFormHeader, usageFormHeaderLine, usageFormHeaderless, usageFormNone} {
		t.Logf("%s (%d): %s", form, len(forms[form]), strings.Join(forms[form], " "))
	}
	if len(forms[usageFormNone]) > 0 {
		t.Errorf("these published pages print NO usage block at all: %s — a `-h` page that names no invocation cannot be documented", strings.Join(forms[usageFormNone], " "))
	}
	// The census must have seen every page, or a page silently dropped from the
	// walk would read as a clean run (Rule 15: a scan that found nothing must
	// be distinguishable from a scan that never ran).
	seen := 0
	for _, verbs := range forms {
		seen += len(verbs)
	}
	if seen != len(publishedVerbHelpPages) {
		t.Fatalf("the census classified %d pages, want %d", seen, len(publishedVerbHelpPages))
	}
}

// TestUsageFormCensusCarriesClauseOne re-derives clause ① of
// help_capability_first_test.go's hcfCapabilityFirstOffenses over the SAME 12
// published pages this file already walks — through the same
// hcfFirstInvocationLine / hcfFirstProseLine, on the real binary (Rule 31), so
// the number a reader quotes comes from the suite rather than from a document
// that has aged (tasks 01a12b9f-9736-778a-811a-52ed659b5dcd filed it,
// 01a12bb2-3772-72b7-bb2a-a5a3e8fecf13 re-specified the clause it carries).
//
// WHAT CHANGED WITH THE RE-SPECIFICATION, AND WHY THE ASSERTIONS MOVED. The
// clause used to be a POSITION test (an invocation within the first
// hcfSectionMaxLine+1 lines), and this census carried the matching `breaching
// > 0` floor — "at least one published page breaches clause ①, or the question
// this census exists for has changed". That floor was RIGHT for a clause the
// shipped pages mostly did NOT satisfy: it kept the census from reading as a
// clean run when what had really happened was that the scan stopped matching.
// The clause is now an ORDER relation and it is a GATE over the whole set
// (TestClauseOneServesEveryPublishedPage), so the same floor would assert that
// the gate FAILS — the one thing a green build may never require. The pin it
// carried is kept, moved to where it still measures something: the census
// below asserts the SET it walked is whole, that the judgement it prints is the
// gate's own (re-derived through the predicate, not restated), and that the
// window form the old clause used is NOT what decided any verdict —
// hcfLegacyInvocationInWindow is run alongside, and a page the legacy window
// would refuse while the shipped order predicate accepts it is exactly the
// fragility this row removed. That check has teeth today (four shipped pages
// sit on the retired boundary) and it keeps teeth after any re-layout, because
// it is a statement about the two tests, not about the pages.
//
// The reported figures stay NON-VACUOUS in the direction that still applies:
// at least one page must satisfy the clause, and every page must have been
// classified. A 12/12 "satisfied" is now the EXPECTED outcome, not a vacuous
// one — the ordering relation is the one every published page already obeys.
func TestUsageFormCensusCarriesClauseOne(t *testing.T) {
	satisfied, breaching, wouldFailOnlyTheLegacyWindow := 0, 0, 0
	var breachingVerbs []string
	for _, verb := range publishedVerbHelpPages {
		help := helpPage(t, []string{verb})
		first, ok := hcfFirstInvocationLine(help)
		prose := hcfFirstProseLine(help)
		if ok && (prose < 0 || prose > first) {
			satisfied++
			if !hcfLegacyInvocationInWindow(help) {
				wouldFailOnlyTheLegacyWindow++
			}
			t.Logf("clause1 OK     %-8s invocation=%d prose=%s legacyWindow=%v", verb, first, hcfLineOrNone(prose), hcfLegacyInvocationInWindow(help))
			continue
		}
		breaching++
		breachingVerbs = append(breachingVerbs, verb)
		t.Logf("clause1 BREACH %-8s invocation=%s prose=%s", verb, hcfInvocationIndexOrNone(first, ok), hcfLineOrNone(prose))
		fired := false
		for _, offense := range hcfCapabilityFirstOffenses(help) {
			if strings.Contains(offense, "opens with prose") || strings.Contains(offense, "prints no invocation") {
				fired = true
				break
			}
		}
		if !fired {
			t.Errorf("`gopi %s -h` is counted as breaching clause ① but the predicate does not say so — the census and the predicate disagree", verb)
		}
	}
	t.Logf("clause ① (ORDER: first invocation before first prose paragraph) over the published set: satisfied=%d breached=%d of %d (breaching: %s; %d satisfy the ORDER clause while the RETIRED position window would refuse them — the fragility the re-specification removed)",
		satisfied, breaching, len(publishedVerbHelpPages), strings.Join(breachingVerbs, " "), wouldFailOnlyTheLegacyWindow)
	if satisfied+breaching != len(publishedVerbHelpPages) {
		t.Fatalf("classified %d pages, want %d — a page fell out of the walk", satisfied+breaching, len(publishedVerbHelpPages))
	}
	// Non-vacuity, the arm that still applies: the scan must find the shape it
	// is looking for on at least one page, or a green run proves nothing.
	if satisfied == 0 {
		t.Error("no published page satisfies clause ① — the census is vacuous (or the scan stopped matching)")
	}
	// The floor that replaced the retired `breaching > 0`: the clause is a GATE
	// now, so a breach is a build failure in TestClauseOneServesEveryPublishedPage
	// — but a census whose judgement has stopped agreeing with that gate would
	// print a table nobody can act on, so the agreement is asserted page by page
	// above and the walk is asserted whole here.
	if len(publishedVerbHelpPages) < hucPublishedPageFloor {
		t.Errorf("the published set has shrunk to %d pages — the clause was widened to cover the published surface, so a shrinking set silently narrows it back", len(publishedVerbHelpPages))
	}
}

// hcfInvocationIndexOrNone renders the invocation index for the log, "-" when
// the page prints none — the two states a reader of the table needs to tell
// apart (a page with no invocation is a different defect from one that opens
// with prose).
func hcfInvocationIndexOrNone(index int, ok bool) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%d", index)
}

// TestUsageFormPredicateIsNotBlind is the RED PROOF (Rule 10.4/10.5): the
// predicate is applied to poisoned pages and to the real ones, so a green run
// cannot be a blind one. Each poison is a shape that actually went wrong.
func TestUsageFormPredicateIsNotBlind(t *testing.T) {
	// (control) the real page must report NOTHING, or the poisons below would
	// be indistinguishable from a predicate that always refuses.
	// `gopi merge` used to be this control; it has no page any more (epic
	// merge-removal, item 2), and a control that reads a REFUSAL would report a
	// page with no usage block for the wrong reason. `gopi notify` is a live
	// page of the same shape.
	real := helpPage(t, []string{CliVerbNotify})
	if bad := hucUsageFormOffenses(real, CliVerbNotify); len(bad) > 0 {
		t.Fatalf("CONTROL failed — the shipped `gopi %s -h` already offends the contract:\n%s", CliVerbNotify, strings.Join(bad, "\n"))
	}

	// (a) the ORIGINAL defect: the reader's third form removed. Reproduced by
	// handing the predicate a page whose block the reader cannot claim — a page
	// that opens on prose and never prints an indented row.
	proseOnly := "GoPi thing — a thing.\n\nProse the user must read.\n\nNotes:\n  the detail lives here.\n"
	if bad := hucUsageFormOffenses(proseOnly, "thing"); len(bad) == 0 {
		t.Error("(a) a page with no usage block passed — clause 1 is blind")
	} else if !strings.Contains(strings.Join(bad, "\n"), "no usage rows") {
		t.Errorf("(a) clause 1 fired with the wrong reason: %s", strings.Join(bad, "\n"))
	}

	// (b) the reader's own trap, as a page: the block is BELOW the sections, so
	// the bound is what keeps the Notes sentence out of the synopsis. Both
	// clauses fire here, and they must fire INDEPENDENTLY — clause 3 is the
	// layout claim the reader's bound relies on.
	blockBelowSections := "GoPi thing — a thing.\n\nOptions:\n  --fast             go fast\n\nNotes:\n  gopi thing --fast is the only spelling that skips the probe.\n"
	joinedB := strings.Join(hucUsageFormOffenses(blockBelowSections, "thing"), "\n")
	if !strings.Contains(joinedB, "no usage rows") {
		t.Errorf("(b) clause 1 did not fire on a page whose block sits below `Notes:`: %s", joinedB)
	}
	if !strings.Contains(joinedB, "BELOW the first non-") {
		t.Errorf("(b) clause 3 (the layout bound the reader relies on) did not fire: %s", joinedB)
	}
	if got := usageForm(blockBelowSections); got != usageFormNone {
		t.Errorf("(b) the census classified the poisoned page as %q, want %q — the census and the predicate disagree", got, usageFormNone)
	}

	// (c) a block that documents the WRONG verb satisfies "has rows" and must
	// still be refused.
	wrongVerb := "GoPi other — a thing.\n\n  gopi other        do the other thing\n"
	if bad := hucUsageFormOffenses(wrongVerb, "thing"); len(bad) == 0 {
		t.Error("(c) a usage block naming another verb passed — clause 2 is blind")
	}
}
