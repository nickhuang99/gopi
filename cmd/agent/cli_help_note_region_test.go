package main

// cli_help_note_region_test.go — the guard for the class "a `-h` page promises a
// note it does not carry".
//
// The predicate it runs lives in cli_help_note_region.go (helpNotePageOffenses),
// with the decision it implements; this file is the GATE and its RED PROOF.
//
// WHAT IS GUARDED, AND WHY EVERY PUBLISHED PAGE IS. The defect is a property of
// the PUBLISHED surface, not of one verb's page: guarding only the page that
// happened to carry the instance would leave the next page free to grow one. So
// the gate walks publishedVerbHelpPages — the SAME list the usage-budget gate and
// the form census walk (Rules 13/14: one list, no second copy) — and reads each
// page off the REAL binary through helpPage, which is the surface the operator
// sees (Rule 31: a usage constant that stopped being printed cannot satisfy this
// while the terminal shows something else).
//
// WHAT IT DELIBERATELY DOES NOT DO. It is not a spell-checker and not a grammar
// of English: it does not require any page to carry a region, and it says nothing
// about a pointer whose target is a real `-h` flag row or a man page rather than
// a note. It answers exactly one question — "does this page point FORWARD at a
// note it does not carry?" — and refuses that promise instead of guessing whether
// the reader meant something else.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskverbs"
)

// TestPublishedHelpPagesResolveTheirNotePointers is the gate.
func TestPublishedHelpPagesResolveTheirNotePointers(t *testing.T) {
	for _, verb := range publishedVerbHelpPages {
		verb := verb
		t.Run(verb, func(t *testing.T) {
			page := helpPage(t, []string{verb})
			for _, offense := range helpNotePageOffenses(page) {
				t.Errorf("`gopi %s -h` %s", verb, offense)
			}
		})
	}
	// The SUB-ATOM pages (`gopi task merge -h` …) are pages too, and one of them
	// carried this class's first instance. They are addressed through the parser's
	// own merger-verb table (internal/taskverbs.MergerVerbs — the list the
	// dispatcher reads), so a new atom is scanned the day it exists.
	for _, verb := range taskverbs.MergerVerbs {
		verb := verb
		t.Run("task-"+verb, func(t *testing.T) {
			page := taskMergerAtomUsage(verb)
			if page == "" {
				return // not an atom this build publishes a page for
			}
			for _, offense := range helpNotePageOffenses(page) {
				t.Errorf("`gopi task %s -h` %s", verb, offense)
			}
		})
	}
	// And the page every reader opens first. It is read off the REAL binary like
	// the rest (its line budget lives in usage_test.go, a different policy).
	t.Run("top-level", func(t *testing.T) {
		page := helpPage(t, nil)
		for _, offense := range helpNotePageOffenses(page) {
			t.Errorf("`gopi -h` %s", offense)
		}
	})
}

// TestHelpNotePointerPredicateIsNotBlind is the RED PROOF, and it runs the SAME
// predicate the gate runs. Its arms are the shapes that actually occurred:
//
//	(control)  a page that promises nothing forward — green;
//	(a) the LIVE INSTANCE verbatim: the ui_state line as it stood;
//	(b) THE RE-SPELLING — the same promise worded so the OLD guard's one phrase
//	    ("note below") never matched;
//	(c) a pointer beside a CLOSED, NON-EMPTY region — green: the sentence is not
//	    the defect, the missing region is;
//	(d) a BLANKED region (marks kept, note emptied);
//	(e) a DELETED region, and (f) a MALFORMED mark; (g) a page that printed
//	    nothing.
//
// The last block MEASURES the recognizer against the marks the writer renders, so
// a reader that stopped recognizing them cannot make every arm pass by accident
// (Rule 15).
func TestHelpNotePointerPredicateIsNotBlind(t *testing.T) {
	// The marks are built from the ONE published vocabulary the pages render
	// (cli_help_note_region.go) — a test that spelled its own marks would prove
	// nothing about the pages.
	begin := helpNoteBeginPrefix + uiStateNoteRegionSlug + helpNoteBeginSuffix
	end := helpNoteEndPrefix + uiStateNoteRegionSlug + helpNoteEndSuffix
	note := "SELF-OBSERVATION NOTE: this page is a ROUTE. The contract lives in `man gopi-" + CliVerbUIState + "`."
	closed := begin + "\n" + note + "\n" + end
	blanked := begin + "\n\n" + end
	malformed := helpNoteBeginPrefix + helpNoteBeginSuffix + "\n" + note + "\n" + helpNoteEndPrefix + helpNoteEndSuffix

	arms := []struct {
		name    string
		page    string
		wantRed bool
		why     string
	}{
		{"control-plain-page",
			"Usage:\n  gopi " + CliVerbUIState + " --pid N\n\nReads the dump. Exit 0 on agreement.\n", false,
			"a page that promises nothing forward is the CONTROL — without it the poisons below are indistinguishable from a predicate that always refuses"},
		{"live-instance",
			"the artifact at all - see the note below). Exit " + uiStateCompareExitCodeStr + " = a line the two sides disagree about.\n", true,
			"the measured instance: the pointer, with no region anywhere on the 160-line page"},
		{"respelled-pointer",
			"and the grid, one more time, see the self-check note just under this row.\n", true,
			"THE CLASS MUTATION — the same promise spelled so the phrase-match guard stayed green"},
		{"respelled-under",
			"and the grid, one more time, see the self-check note under it.\n", true,
			"the loose half: the re-spelling a strict vocabulary would miss entirely"},
		{"respelled-outside-the-vocabulary",
			"and the grid, one more time, see the self-check note after this row.\n", true,
			"the MEASURED MISS of the first, seven-word vocabulary: 0 offenses on a region-free page while 'just under' on the SAME page gave 1 (2026-10-16) — the arm exists so the widened binder families cannot narrow back to that list"},
		{"respelled-later-on-this-page",
			"and the grid, one more time, see the self-check note later on this page.\n", true,
			"the same family, another member — the net is bound by FAMILIES, not by the one spelling a report happened to quote"},
		{"pointer-with-real-region",
			"the artifact at all - see the note just under this row).\n\n" + closed + "\n", false,
			"the GOOD case: the same sentence beside a real marked note must stay green, or the fix would delete the behaviour it protects"},
		{"blanked-region",
			"the artifact at all - see the note below).\n\n" + blanked + "\n", true,
			"marks with nothing between them is the defect wearing the fix's clothes"},
		{"deleted-region",
			"and the grid, one more time, see the self-check note under this row.\n", true,
			"the mutation from the briefing: the region removed, the promise left standing"},
		{"malformed-mark",
			"the artifact at all - see the note below).\n\n" + malformed + "\n", true,
			"a mark that names no region can be addressed by nobody — a reader or a gate"},
		{"empty-page",
			"", true,
			"a page this guard cannot see must not pass — a measurement of nothing is not green (Rule 15)"},
	}

	for _, arm := range arms {
		arm := arm
		t.Run(arm.name, func(t *testing.T) {
			offenses := helpNotePageOffenses(arm.page)
			if red := len(offenses) > 0; red != arm.wantRed {
				t.Errorf("arm %q: red=%v want %v — %s\noffenses:\n%s",
					arm.name, red, arm.wantRed, arm.why, strings.Join(offenses, "\n"))
			}
		})
	}

	// The recognizer must recognize what the writer writes — including the EMPTY
	// region (the mark, not the body, is what is recognized). If the marks stopped
	// being recognized, arm (c) would go red for the wrong reason; if the mark
	// scan were too eager, arm (a) would go green for the wrong reason.
	for _, arm := range []struct{ name, page string }{{"closed", closed}, {"blanked", blanked}} {
		if got := helpNoteRegions(arm.page); len(got) != 1 {
			t.Errorf("the recognizer found %d regions in the %s page, want 1 — %q and %q are the spellings the writer renders",
				len(got), arm.name, begin, end)
		}
	}
	if got := helpNoteRegions(closed); len(got) == 1 && got[0].slug != uiStateNoteRegionSlug {
		t.Errorf("the region's slug was read as %q, want %q — a mark must name its region",
			got[0].slug, uiStateNoteRegionSlug)
	}
	if got := helpNotePageOffenses(closed); len(got) != 0 {
		t.Errorf("a closed, non-empty region is an offense: %s", strings.Join(got, "\n"))
	}
	// The pointer scan is bounded by the sentence and by proximity: ordinary prose
	// must not be read as a forward promise.
	for _, prose := range []string{
		"the registry under ~/.gopi holds the dumps; exit 0 on agreement.",
		"see the deprecation note pointing here — the grouped form is the one.",
		"see the note above this row (an already-made promise is not a forward one).",
		"see `man gopi-task` for the note on adoption.",
	} {
		if helpNotePointerRE.MatchString(prose) {
			t.Errorf("the scan over-matched legitimate prose: %q — a note word must sit within helpNotePointerBinderProximity of the binder, in the same sentence",
				prose)
		}
	}
	if helpNotePointerBinderProximity < 40 {
		t.Errorf("helpNotePointerBinderProximity is %d — too small to span a real promise", helpNotePointerBinderProximity)
	}

	// CROSS-CHECK against the instrument this guard replaces: the phrase the OLD
	// guard matched must NOT be what decides the arms, or the class would be back.
	respelled := arms[2]
	if strings.Contains(respelled.page, "note below") {
		t.Fatal("the re-spelling arm still carries the old guard's one phrase — it would prove nothing")
	}
	_ = fmt.Sprintf("%v", closed)
}
