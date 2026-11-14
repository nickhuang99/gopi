package agent

// reframe_ground_clauses_test.go — the acceptance set for "a reframe keeps the
// clauses that say WHICH TREE this run writes" (the 01a1957a defect).
//
// TWO HALVES, and the split is the point:
//
//	TestReframe_SwapDropsHarnessClausesWithoutTheFix
//	    RED: the swap, with the preservation pass bypassed, really does drop every
//	    one of the five clauses — the measured behaviour of the run that then
//	    `cd`'d into the shared checkout.
//	TestReframe_SwapKeepsHarnessClausesVerbatim
//	    GREEN plus the no-weakening cases: the prose IS still dropped (the
//	    context saving the reframe exists for), a briefing with no clauses is
//	    unchanged, and an unrelated label that merely LOOKS like a clause is not
//	    swept in.

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/taskverdict"
	"github.com/nickhuang99/gopi/internal/tools"
)

// rgcPad repeats line until the text is longer than the swap's own size gate
// (reframeMinRawLength), so a fixture measures the SWAP and not the "small
// briefing" refusal — derived from the constant, never guessed (Rule 1).
func rgcPad(line string) string {
	need := reframeMinRawLength + len(line)
	n := need/len(line) + 1
	return strings.Repeat(line, n)
}

// rgcFixtureBriefing is the shape of the measured briefing: prose, then the
// runner's harness clauses, then the cut-point clause at the tail. The prose
// padding sits at the HEAD on purpose — prose that follows a clause belongs to
// that clause's section (asserted by TestReframe_ClauseLikeProseIsNotSweptIn),
// so a tail pad would be measuring the section boundary, not the swap.
func rgcFixtureBriefing() string {
	return rgcPad("PROSE-PAYLOAD-LINE-THAT-MUST-BE-DROPPED\n") +
		tools.TaskGroundAnnouncementLabel + "\n" +
		"THIS RUN ALREADY HAS ITS WORKTREE. You are standing in it: /ground\n" +
		"checked out on branch task/fixture.\n" +
		tools.TaskBriefingPublishLabel + "\n" +
		"The clone channel's delivery loop publishes it; do not hand-roll the push.\n" +
		tools.TaskBriefingAuthorityLabel + "\n" +
		"Your worktree is /ground. That is the ONLY tree this run writes: /shared is\n" +
		"the repo's MAIN checkout, shared with the operator.\n" +
		tools.TaskGroundEvidenceLabel + "\n" +
		"Evidence goes under /ground/.gopi/sessions/<id>/.\n" +
		tools.GroundCutPointLabel + "\n" +
		"the branch was cut from master@abc123\n"
}

// rgcApply runs applyReframe over a fixture briefing and returns the epic text.
func rgcApply(t *testing.T, briefing string) string {
	t.Helper()
	ag := &Agent{Ctx: &Context{}, Logger: log.New(io.Discard, "", 0)}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: epicGoalHeader + briefing})
	ag.applyReframe(parseReframeInput(reframeValidArgs(taskverdict.Doable)))
	if !ag.reframeApplied {
		t.Fatal("precondition: the swap must have applied (the fixture is a big briefing)")
	}
	return ag.Ctx.Messages[0].Content
}

// TestReframe_SwapDropsHarnessClausesWithoutTheFix is the RED half: it states
// what the swap does when the preservation pass returns nothing — which is
// exactly what every reframe did before this fix, and what req#3 of the measured
// run shows.
func TestReframe_SwapDropsHarnessClausesWithoutTheFix(t *testing.T) {
	briefing := rgcFixtureBriefing()
	// The pre-fix body: the reframed epic and nothing else.
	body := reframedEpicBody(parseReframeInput(reframeValidArgs(taskverdict.Doable)))
	swapped := reframedEpicMessage(body)

	for _, label := range reframePreservedClauseLabels {
		if strings.Contains(swapped, label) {
			t.Fatalf("pre-fix swap was expected to drop %q, it survived", label)
		}
	}
	if !strings.Contains(briefing, tools.TaskGroundAnnouncementLabel) {
		t.Fatal("fixture precondition: the raw briefing must carry the clauses")
	}
	t.Logf("RED reproduced: the pre-fix swap drops all %d harness clauses and keeps %d chars of prose summary",
		len(reframePreservedClauseLabels), len(swapped))
}

// TestReframe_SwapKeepsHarnessClausesVerbatim is the GREEN half plus the
// no-weakening cases.
func TestReframe_SwapKeepsHarnessClausesVerbatim(t *testing.T) {
	briefing := rgcFixtureBriefing()
	got := rgcApply(t, briefing)

	// 1. Every clause survives, with its own words.
	for _, label := range reframePreservedClauseLabels {
		if !strings.Contains(got, label) {
			t.Fatalf("the swap must keep %q, got:\n%s", label, got)
		}
	}
	for _, sentence := range []string{
		"You are standing in it: /ground",
		"do not hand-roll the push",
		"That is the ONLY tree this run writes",
		"MAIN checkout, shared with the operator",
		"Evidence goes under /ground/.gopi/sessions/<id>/",
		"the branch was cut from master@abc123",
	} {
		if !strings.Contains(got, sentence) {
			t.Fatalf("the kept clause must be VERBATIM (missing %q), got:\n%s", sentence, got)
		}
	}

	// 2. No weakening: the prose payload really is still dropped — that saving
	//    is the reason the reframe exists.
	for _, dropped := range []string{"PROSE-PAYLOAD-LINE-THAT-MUST-BE-DROPPED"} {
		if strings.Contains(got, dropped) {
			t.Fatalf("the reframe must still drop the raw prose (%q survived):\n%s", dropped, got)
		}
	}

	// 3. No weakening: the reframed goal still leads, so every mechanism that
	//    recognizes the goal by prefix keeps working.
	if !strings.HasPrefix(got, epicGoalHeader) {
		t.Fatalf("the reframed epic must still lead the message, got:\n%s", got)
	}
}

// TestReframe_BriefingWithoutClausesIsUnchanged pins the boundary: a run with no
// announcement (a chat, a repo-less row) keeps exactly the reframed body it had,
// with no empty section and no stray separator.
func TestReframe_BriefingWithoutClausesIsUnchanged(t *testing.T) {
	body := reframedEpicBody(parseReframeInput(reframeValidArgs(taskverdict.Doable)))
	want := reframedEpicMessage(body)
	got := rgcApply(t, rgcPad("just prose, no harness clauses at all\n"))
	if got != want {
		t.Fatalf("a clause-free briefing must swap exactly as before\n want: %q\n  got: %q", want, got)
	}
}

// TestReframe_ClauseLikeProseIsNotSweptIn pins the other boundary: a clause
// section ends at the NEXT clause label, so ordinary prose that merely mentions
// a label's words (not at the head of its own line) is not carried over.
func TestReframe_ClauseLikeProseIsNotSweptIn(t *testing.T) {
	kept := reframePreservedBriefingClauses(
		tools.TaskGroundAnnouncementLabel + "\nGround: /ground\n" +
			"as the " + tools.TaskBriefingPublishLabel + " says in passing\n" +
			"more prose that belongs to no clause\n")
	if !strings.Contains(kept, "Ground: /ground") {
		t.Fatalf("the announced clause must be kept, got %q", kept)
	}
	if !strings.Contains(kept, "says in passing") {
		// The mention is INSIDE the announced section (the label is not at the
		// head of a line), so it is part of that section — this is the documented
		// behaviour, asserted so a later "tidy-up" cannot silently change it.
		t.Fatalf("a mid-line mention belongs to the section it sits in, got %q", kept)
	}
	if strings.Contains(kept, "more prose that belongs to no clause") == false {
		t.Fatalf("the section runs to the next LABEL, so this line belongs to it, got %q", kept)
	}
}
