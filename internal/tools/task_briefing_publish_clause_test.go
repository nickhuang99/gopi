package tools

// task_briefing_publish_clause_test.go — the PROSE half of the clone-channel
// stray-ref defect, pinned as a regression (task 01a1604a; the machine half is
// git_publish_target_guard.go from 01a16033).
//
// WHY PROSE GETS A TEST LIKE ANY OTHER SURFACE. The stray refs on the server
// were not written by gopi's Go code — they were written by a MODEL that read
// PROJECT.md Rule 6c (written for a /tmp clone with no channel behind it) and
// obeyed it. The fix is therefore half code (the guard, which acts AFTER the
// decision) and half prose (this clause, which reaches the model BEFORE the
// decision exists). A clause that can be edited away without a failing test is
// not a fix.
//
// WHAT THESE ASSERT, AND WHY EACH HALF IS NEEDED:
//
//  ① the clause NAMES the ground's branch — the subject it is about, and the one
//     fact that makes it actionable ("the loop pushes THIS branch, not one you
//     name");
//  ② it carries the ROUTE (the delivery loop: clean, pull --rebase, build, push)
//     and the BAN (no `HEAD:<name>`, no `refs/heads/<name>`, no new feat/<name>)
//     — measured 2026-10-21: the run did exactly `git push origin
//     HEAD:refs/heads/task/<id>`;
//  ③ it names the enforcement that now backs it, by the SAME words the guard's
//     refusal uses, so prose and check cannot drift into disagreement;
//  ④ the escape hatch belongs to the OPERATOR, never to the run — a clause that
//     mentioned GOPI_ALLOW_DANGEROUS=1 without that attribution would be handing
//     the run the key to the guard it is being told to respect;
//  ⑤ no branch ⇒ no clause (Rule 15: a record that cannot name its subject is
//     not a record).
//
// Rule 1b/13/14: every string asserted here is read from the constant the
// producer uses or from the clause itself — never re-typed as an independent
// expectation of the same value.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devworkspace"
)

// TestBriefingPublishClauseNamesTheRouteAndTheBan is the clause's behaviour on a
// concrete ground: the branch, the loop, and the destinations a run must not
// choose.
func TestBriefingPublishClauseNamesTheRouteAndTheBan(t *testing.T) {
	const branch = "task/01a1604a-63d9-7269-9da7-1e6148b5ff74-r1"
	clause := TaskBriefingPublishClause(branch)
	if clause == "" {
		t.Fatalf("a clone ground with a branch must render a clause — the run has something to be told")
	}
	if !strings.Contains(clause, TaskBriefingPublishLabel) {
		t.Errorf("the clause must carry its label %q so a reader (and this gate) can address it by name:\n%s",
			TaskBriefingPublishLabel, clause)
	}
	if !strings.Contains(clause, branch) {
		t.Errorf("the clause must NAME the ground's branch %q — the loop pushes THAT branch, and a clause naming no branch is not actionable:\n%s",
			branch, clause)
	}
	// ② THE ROUTE: the four steps the channel actually runs, in order
	// (internal/devworkspace). Asserted as one phrase because the ORDER is the
	// statement — "pull --rebase" before "build" before "push" is what makes
	// "the channel publishes, not you" believable.
	for _, want := range []string{
		"DELIVERY LOOP",
		"clean, pull --rebase, declared build, push",
		"Publishing is the CHANNEL's job, not the run's",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must carry the route %q — a run told only what NOT to do invents its own way to deliver:\n%s", want, clause)
		}
	}
	// ② THE BAN, by the measured spellings. `HEAD:<some-name>` is the incident's
	// exact shape; the two origin shapes (refs/heads/ and a bare feat/<name>)
	// are the other two destinations the guard refuses.
	for _, want := range []string{
		"`HEAD:<some-name>`",
		"`HEAD:refs/heads/<some-name>`",
		"`feat/<name>`",
		"CREATES a ref on the server that nothing merges",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must name the banned destination %q — the measured incident was `HEAD:refs/heads/task/<id>`, and a general \"do not push\" leaves the run guessing which pushes those are:\n%s", want, clause)
		}
	}
	// ② THE SECOND HARM (the guard's other measured case): a push of the
	// integration branch from a checkout that is not it MOVES the server's
	// integration branch. Naming only "creates a ref" would leave that one
	// unexplained, and it is the more destructive of the two.
	if !strings.Contains(clause, "MOVES the server's integration branch") {
		t.Errorf("the clause must name the second harm (a push of the integration branch from another checkout MOVES it) — the guard refuses it, so the prose must explain it:\n%s", clause)
	}
	// ③ THE ENFORCEMENT, in the guard's own words: the two halves must not
	// disagree about what happens to a hand-rolled push.
	for _, want := range []string{
		"git_publish_target_guard.go",
		"refuses such a push before it runs",
		"`git status --porcelain` prints nothing",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must carry %q — the prose and the bash-tool refusal are one instruction stated twice:\n%s", want, clause)
		}
	}
}

// TestBriefingPublishClauseGivesTheEscapeHatchToTheOperator pins the ATTRIBUTION
// of the escape hatch, not its absence. The hatch exists (it is the shared
// GOPI_ALLOW_DANGEROUS of the dangerous-command family), and an operator who has
// decided to write a ref by hand is taking the blast radius deliberately. What
// the clause must never do is offer it to the RUN: a run told "you may set
// GOPI_ALLOW_DANGEROUS=1" is a run told the guard is advisory.
func TestBriefingPublishClauseGivesTheEscapeHatchToTheOperator(t *testing.T) {
	clause := TaskBriefingPublishClause("master")
	if !strings.Contains(clause, "GOPI_ALLOW_DANGEROUS=1") {
		t.Fatalf("the clause must name the escape hatch — a rule whose only exit is undocumented is the silent refusal Rule 15 forbids:\n%s", clause)
	}
	if !strings.Contains(clause, "OPERATOR decision, never a run's") {
		t.Errorf("the escape hatch must be ATTRIBUTED to the operator — anything less hands the run the key to the guard it is being told to respect:\n%s", clause)
	}
}

// TestBriefingPublishClauseRendersNothingWithoutABranch pins the OTHER
// direction. The clause's substance is "the loop pushes the branch you stand
// on", so with no branch there is nothing true to say: the caller binds nothing
// (the announcement clause beside it already renders the branch as "none"), and
// an empty string is the only honest answer.
func TestBriefingPublishClauseRendersNothingWithoutABranch(t *testing.T) {
	for _, tc := range []struct{ name, branch string }{
		{"empty", ""},
		{"blank", "   "},
		{"newline only", "\n"},
	} {
		if got := TaskBriefingPublishClause(tc.branch); got != "" {
			t.Errorf("%s: a ground whose branch could not be read must render no clause; got:\n%s", tc.name, got)
		}
	}
}

// TestBriefingPublishClauseIsCloneChannelOnlyByConstruction pins the SCOPE on
// the text itself: the clause names the clone channel and the ground's clone
// shape, and it must not tell a run to push "its branch back" (Rule 6c's /tmp
// clone half, which is the WRONG instruction for a channel ground and is exactly
// the sentence the measured run obeyed). The scope decision lives at the seam
// (cli_tasks.go renders this only when cloneRun != nil); this asserts that the
// text would not contradict a worktree reader even if it were rendered.
func TestBriefingPublishClauseIsCloneChannelOnlyByConstruction(t *testing.T) {
	clause := TaskBriefingPublishClause("master")
	if !strings.Contains(clause, "This ground is a CLONE standing on branch master") {
		t.Errorf("the clause must identify the ground SHAPE it is about — a worktree reader must be able to tell at once that it is not the subject:\n%s", clause)
	}
	for _, banned := range []string{
		"push its branch back",  // Rule 6a's clone clause, wrong for a channel ground
		"git push origin feat/", // the /tmp-clone route, refused here
	} {
		if strings.Contains(clause, banned) {
			t.Errorf("the clause must NOT carry %q — that is the instruction the measured run obeyed, and a clause that repeats it is the defect restated:\n%s", banned, clause)
		}
	}
}

// ── THE TIMING HALF (task 01a1792e) ─────────────────────────────────────────
//
// The clause's first spelling told the reader the delivery loop "runs after your
// round, every time". The implementation is the opposite shape: the leg is
// latched to ONE attempt per run (internal/agent/loop.go's cloneDeliveryDone) and
// runs from the run's END hook (internal/agent/taskregister.go). Measured cost of
// the wrong sentence, on row 01a17921-fa90: its run watched origin/master for
// twenty rounds, saw it unmoved, wrote "the delivery leg did not run", and filed
// a bug row — while its own output.log ends with
// `clone-channel: attempt 1/3: push accepted` and its commit 036f357 is an
// ancestor of origin/master (ea2007f). The wording produced a false defect
// report, so the wording is pinned here like any other surface.

// TestBriefingPublishClauseStatesTheRealTrigger pins the sentence that replaces
// the false one: ONCE per run, in the END hook, not at a round boundary.
func TestBriefingPublishClauseStatesTheRealTrigger(t *testing.T) {
	const branch = "master"
	clause := TaskBriefingPublishClause(branch)
	for _, want := range []string{
		// the trigger, positively stated
		"ONCE PER RUN",
		"END hook",
		// and its EXCLUSION, which is the half that stops the misreading
		"NOT at a round boundary",
		// the false inference, named so a reader recognises their own thought
		"NOT evidence that the loop is broken",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must state the delivery loop's REAL trigger — %q is missing.\n"+
				"A run told \"every round\" watches for a push that never comes, because there is exactly one attempt and it happens at close-out:\n%s",
				want, clause)
		}
	}
}

// TestBriefingPublishClauseNamesWhatIsEvidenceInstead pins the second half of
// the repair: a run must be told WHICH lines are the evidence, or it falls back
// to inferring from silence — the failure that produced row 01a17921-fa90.
//
// The two phrases asserted here are the machinery's OWN, read from the constants
// the producers use (deliveryAttemptLogFmt's shape in
// internal/devworkspace/devworkspace.go for a landed attempt,
// DeliveryDidNotLandReasonPref for the ledger's non-delivery reason) rather than
// re-typed — Rule 1b/13: a briefing that tells a reader to look for a line
// nothing prints is worse than saying nothing.
func TestBriefingPublishClauseNamesWhatIsEvidenceInstead(t *testing.T) {
	clause := TaskBriefingPublishClause("master")
	for _, want := range []string{
		// devworkspace's landed-attempt line (formatAttemptAccepted in
		// internal/devworkspace/devworkspace.go); the clause quotes it so the run
		// knows which line to look for.
		"push accepted",
		// the close-out's landed line (internal/agent's cloneDeliveryDoneFmt).
		// Quoted, not imported: internal/agent imports THIS package, so the
		// constant cannot travel here without a cycle — the e2e test in
		// cmd/agent is where the two halves meet.
		"clone channel: delivery delivered",
		// the ledger's own non-delivery reason, READ from the constant (this
		// package owns it, so the assertion cannot drift from the producer).
		DeliveryDidNotLandReasonPref,
		"left OPEN at close-out",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must name %q as the EVIDENCE of the leg's outcome — without it a run can only infer from silence, which is the measured defect:\n%s",
				want, clause)
		}
	}
	// the attempt brake is a VALUE from the channel, not a typed 3
	if !strings.Contains(clause, fmt.Sprintf("attempt 1/%d", devworkspace.MaxPushAttempts)) {
		t.Errorf("the clause must quote the channel's own attempt line (\"attempt 1/%d\", devworkspace.MaxPushAttempts) — the run is told to grep for it:\n%s",
			devworkspace.MaxPushAttempts, clause)
	}
}

// TestBriefingPublishClauseRetiredThePerRoundWording is the anti-regression for
// the sentence itself. It asserts the ABSENCE of the retired spelling, because a
// clause that says "every round" anywhere has reinstated the misreading even if
// it also states the real trigger.
func TestBriefingPublishClauseRetiredThePerRoundWording(t *testing.T) {
	clause := TaskBriefingPublishClause("master")
	// The retired sentence is assembled from its pieces rather than typed, so
	// that `grep -rn "<the phrase>" --include=*.go` stays EMPTY in this tree:
	// a literal here would make the anti-regression test itself the last
	// surviving copy of the wording, and the next reader's grep for "does the
	// retired sentence come back anywhere?" would answer yes forever.
	retiredPerRound := "runs after " + "your round"
	for _, banned := range []string{
		retiredPerRound,
		"after " + "your round, every time",
		"every time", // the retired clause's own emphasis
		"when your round ends",
	} {
		if strings.Contains(clause, banned) {
			t.Errorf("the clause must NOT carry the retired per-round wording %q — it is exactly what made row 01a17921-fa90 report a working delivery leg as broken:\n%s",
				banned, clause)
		}
	}
}
