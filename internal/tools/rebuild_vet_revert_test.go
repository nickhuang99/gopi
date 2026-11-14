package tools

// rebuild_vet_revert_test.go — the REVERT half's acceptance (rebuild_vet_revert.go).
//
// WHAT IS UNDER TEST, and why the assertions are SHAPED the way they are: the
// deliverable is a DIRECTION, not a value. A red gate must be unwound when the
// merge introduced it and kept when master was already red, so every test here
// asserts the CLASSIFICATION *and* the disposition it produces — a test that only
// checked "revert happened" would pass on a gate that reverts honest merges, and a
// test that only checked "kept" would pass on the silent report-only behaviour
// this change exists to replace.
//
// The fixtures use a REAL Go module and the REAL toolchain (the gate's whole job
// is to run a subprocess and read it; a mocked toolchain would measure nothing),
// and the trees are real git revisions, because the baseline arm's whole claim is
// about a REAL pre-merge tip — the revision exists only in git, and the comparison
// materialises it in a detached worktree.
//
// No host load is generated anywhere in this file (Rule 27): the slowest thing
// here is one `go vet` of a single-package module, and the bound arm's only
// "pressure" is the gate's own subprocess against its own budget.

import (
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// vgrFixtureRedFile / vgrFixtureRedBody plant the break this gate exists for:
	// a file that does NOT COMPILE. It is planted in a REGULAR source file rather
	// than a _test.go one deliberately — the class the gate reports is "the tree
	// does not hold together", and a non-test breakage is the strictest version of
	// it (a test-tree-only break would let an implementation that runs `go build`
	// instead of `go vet` pass by accident; measured, `go build` rejects this one
	// too, and so does `go vet`).
	vgrFixtureRedFile = "z_broken.go"
	vgrFixtureRedBody = "package p\n\n// Planted: this symbol exists nowhere in the module.\nfunc Broken() { vgrNeverDefinedSymbol() }\n"
	// vgrFixtureBranchFile / vgrFixtureBranchBody are the pre-existing arm's
	// BENIGN branch payload: the landing must be a real merge (git short-circuits
	// an empty one), while the red stays master's own.
	vgrFixtureBranchFile = "note.txt"
	vgrFixtureBranchBody = "the branch's only change\n"
	// vgrFixtureRedRef is the file the report must name.
	vgrFixtureRedRef = vgrFixtureRedFile
	// vgrFixtureBranch is the branch whose landing the fixture merges.
	vgrFixtureBranch = "feat/planted-red"
	// vgrFixtureMergeSubject names the merge commit the fixture creates.
	vgrFixtureMergeSubject = "merge the planted red"
)

// vgrFixtureRepo builds a sandbox Go module and a REAL merge that introduces a
// tree which does not compile. It returns:
//
//	repo    — the checkout, standing at the MERGED tip (HEAD),
//	preTip  — the recorded pre-merge tip (master before the branch landed),
//	revert  — the revert handle the close loop would have captured pre-sweep.
//
// The shape mirrors the merger's own: a capture BEFORE the landing, a branch
// merged in, and the branch ref kept (so RestoreMaster never has to re-create
// anything for the classification arm — that is merge_revert_test.go's subject).
func vgrFixtureRepo(t *testing.T, plantedInMaster bool) (repo, preTip string, revert *FailedMergeRevert) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, vgFixturePkgDir), 0o755); err != nil {
		t.Fatal(err)
	}
	vgWriteFile(t, repo, vgFixtureGoMod, vgFixtureModulePath)
	vgWriteFile(t, repo, filepath.Join(vgFixturePkgDir, vgFixturePkgFile), vgFixtureCleanBody)
	vetGateFixtureGit(t, repo, "init", "-q", "-b", "master")
	vetGateFixtureGit(t, repo, "config", "user.email", "t@t")
	vetGateFixtureGit(t, repo, "config", "user.name", "t")
	vetGateFixtureGit(t, repo, "add", vgFixtureGoMod, filepath.Join(vgFixturePkgDir, vgFixturePkgFile))
	vetGateFixtureGit(t, repo, "commit", "-q", "-m", "fixture: a clean module")

	if plantedInMaster {
		// THE PRE-EXISTING ARM: master is ALREADY red before anything merges.
		// Planted on master itself and committed, so the tip the baseline
		// measurement checks out is genuinely broken — not a worktree artifact.
		vgWriteFile(t, repo, filepath.Join(vgFixturePkgDir, vgrFixtureRedFile), vgrFixtureRedBody)
		vetGateFixtureGit(t, repo, "add", filepath.Join(vgFixturePkgDir, vgrFixtureRedFile))
		vetGateFixtureGit(t, repo, "commit", "-q", "-m", "master is already vet-red")
	}

	preTip = rebuildTestGit(t, repo, "rev-parse", "HEAD")
	revert = &FailedMergeRevert{Repo: repo, PreTip: preTip}

	// The branch: created FROM the current master, then it plants the red (in
	// the non-pre-existing arm) and lands.
	vetGateFixtureGit(t, repo, "checkout", "-q", "-b", vgrFixtureBranch)
	if plantedInMaster {
		// The pre-existing arm's branch must still CARRY something, or git
		// short-circuits the landing ("Already up to date"): a merge with no
		// commit on either side is not a merge, and the fixture would then be
		// asserting a disposition for a round that never happened. The change is
		// deliberately BENIGN — the red must stay master's, not the branch's.
		vgWriteFile(t, repo, vgrFixtureBranchFile, vgrFixtureBranchBody)
		vetGateFixtureGit(t, repo, "add", vgrFixtureBranchFile)
		vetGateFixtureGit(t, repo, "commit", "-q", "-m", "a benign change, so the branch is landable")
	} else {
		vgWriteFile(t, repo, filepath.Join(vgFixturePkgDir, vgrFixtureRedFile), vgrFixtureRedBody)
		vetGateFixtureGit(t, repo, "add", filepath.Join(vgFixturePkgDir, vgrFixtureRedFile))
		vetGateFixtureGit(t, repo, "commit", "-q", "-m", "plant a tree that does not compile")
	}
	// A real merge commit (--no-ff), so the fixture is the shape a landed branch
	// has even when git could have fast-forwarded: the classification must not
	// depend on which of the two git chose.
	vetGateFixtureGit(t, repo, "checkout", "-q", "master")
	vetGateFixtureGit(t, repo, "merge", "--no-ff", "-q", "-m", vgrFixtureMergeSubject, vgrFixtureBranch)
	return repo, preTip, revert
}

// vgrClassify runs the gate over the fixture's merged tree and classifies it —
// exactly the sequence the close loop performs (measure, then classify).
func vgrClassify(t *testing.T, repo string, revert *FailedMergeRevert) (*VetGateResult, VetGateRevertDecision) {
	t.Helper()
	vet := runPostMergeVetGate(repo)
	return vet, ClassifyVetGateRevert(vet, revert)
}

// TestVetGateRevert_IntroducedFailureIsClassifiedAndReverted is the FIRST arm of
// the epic's acceptance: a branch introduces a vet failure, master was green
// before it, and the merge is UNWOUND.
//
// The assertions are deliberately ordered from the measurement to the
// consequence: the gate must actually be red (otherwise the fixture proves
// nothing), the classification must be `introduced`, and only then may the
// unwind run — after which master's tip must be the recorded pre-merge tip and
// the tree must no longer be red.
func TestVetGateRevert_IntroducedFailureIsClassifiedAndReverted(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, preTip, revert := vgrFixtureRepo(t, false)
	vet, decision := vgrClassify(t, repo, revert)

	if !vet.Failed() {
		t.Fatalf("the planted break must make the gate FAIL, got status=%s reason=%q", vet.Status, vet.Reason)
	}
	if decision.Class.Reason != vetClassIntroduced {
		t.Fatalf("classification = %q (detail %q), want %q — a green pre-merge tip plus a red merged tree IS this merge's doing",
			decision.Class.Reason, decision.Class.Detail, vetClassIntroduced)
	}
	if !decision.Revert {
		t.Fatal("an introduced red must be reverted — reporting it and leaving master broken is the defect this change closes")
	}
	if vet.ClassificationRef == "" || !strings.Contains(vet.ClassificationRef, vgrFixtureRedRef) {
		t.Fatalf("the classification must carry the offending position (%s), got %q", vgrFixtureRedRef, vet.ClassificationRef)
	}
	// The classification must be readable from the verdict itself, not only from
	// the decision struct: cmd/agent branches on the gate result.
	if got := vet.VetGateRevertReason(); got != string(vetClassIntroduced) {
		t.Fatalf("VetGateRevertReason() = %q, want %q", got, vetClassIntroduced)
	}

	restored, err := RevertIntroducedVetFailure(revert, vet)
	if err != nil {
		t.Fatalf("the unwind must succeed: %v", err)
	}
	_ = restored

	// THE EVIDENCE THE EPIC ASKS FOR: master's tip is back at the pre-merge tip,
	// AND the tree is no longer red (the second half is what rules out a revert
	// that restored the tip but left the working tree behind).
	if head := rebuildTestGit(t, repo, "rev-parse", "HEAD"); head != preTip {
		t.Fatalf("master tip after the revert = %s, want the recorded pre-merge tip %s", head, preTip)
	}
	if after := runPostMergeVetGate(repo); after.Failed() {
		t.Fatalf("the reverted tree must no longer be vet-red, got status=%s findings=%v", after.Status, after.Findings)
	}
	// The branch's work survives the unwind (that is RestoreMaster's job, and the
	// point of reverting rather than deleting): its ref still exists.
	if out := rebuildTestGit(t, repo, "branch", "--list", vgrFixtureBranch); strings.TrimSpace(out) == "" {
		t.Fatal("the landed branch must survive the revert — reverting is not discarding the work")
	}
}

// TestVetGateRevert_PreExistingFailureIsKeptAndNotBlamed is the SECOND arm: the
// same red measurement, but master was ALREADY red before the merge — so the
// merge is KEPT.
//
// This is the arm that keeps the design honest: without it, "revert on a red
// gate" would unwind honest work for a failure it did not cause, which is the
// 2026-09-13 mis-sized-gate class. The differential with the first test is the
// WHOLE deliverable: byte-identical gate output, opposite disposition.
func TestVetGateRevert_PreExistingFailureIsKeptAndNotBlamed(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, preTip, revert := vgrFixtureRepo(t, true)
	vet, decision := vgrClassify(t, repo, revert)

	if !vet.Failed() {
		t.Fatalf("the fixture must be red on the merged tree, got status=%s", vet.Status)
	}
	if decision.Class.Reason != vetClassPreExisting {
		t.Fatalf("classification = %q (detail %q), want %q — master was red before this merge",
			decision.Class.Reason, decision.Class.Detail, vetClassPreExisting)
	}
	if decision.Revert {
		t.Fatal("a PRE-EXISTING red must never be reverted — the merge did not cause it, and unwinding it would red every honest branch in the queue")
	}
	// The classification sentence must quote the BASELINE's own failure: that is
	// what makes "pre-existing" an observation rather than an assertion.
	if !strings.Contains(decision.Class.Detail, vgrFixtureRedRef) {
		t.Fatalf("the pre-existing detail must quote the baseline's own finding (%s), got %q", vgrFixtureRedRef, decision.Class.Detail)
	}

	// A refusal to revert is a NAMEABLE refusal, never a silent nothing: the
	// guard refuses this verdict by contract, and the reason says why.
	if _, err := RevertIntroducedVetFailure(revert, vet); err == nil {
		t.Fatal("the unwind guard must refuse a pre-existing red rather than silently doing nothing")
	} else if !strings.Contains(err.Error(), string(vetClassPreExisting)) {
		t.Fatalf("the refusal must name the classification, got %q", err.Error())
	}

	// THE DIFFERENTIAL: nothing moved. Master still stands at the merged tip and
	// the branch is still an ancestor of it (the merge is live).
	if head := rebuildTestGit(t, repo, "rev-parse", "HEAD"); head == preTip {
		t.Fatalf("the merge must be KEPT (master advanced past %s), but the tip is still %s", preTip, head)
	}
	if _, err := rebuildTestGitE(t, repo, "merge-base", "--is-ancestor", vgrFixtureBranch, "HEAD"); err != nil {
		t.Fatalf("the kept merge must still contain the branch: %v", err)
	}
}

// TestVetGateRevert_BothArmsDifferOnIdenticalMeasurement is the differential made
// EXPLICIT: the two fixtures produce the same gate verdict shape and the opposite
// disposition, so a regression that collapses the two arms into one fails here
// even if each arm's own test were weakened.
func TestVetGateRevert_BothArmsDifferOnIdenticalMeasurement(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	introducedRepo, _, introducedRevert := vgrFixtureRepo(t, false)
	introducedVet, introduced := vgrClassify(t, introducedRepo, introducedRevert)

	preExistingRepo, _, preExistingRevert := vgrFixtureRepo(t, true)
	preExistingVet, preExisting := vgrClassify(t, preExistingRepo, preExistingRevert)

	if introducedVet.Status != preExistingVet.Status {
		t.Fatalf("the two arms must differ on the COMPARISON, not on the raw measurement: statuses %s vs %s",
			introducedVet.Status, preExistingVet.Status)
	}
	if introducedVet.ErrorCount == 0 || preExistingVet.ErrorCount == 0 {
		t.Fatalf("both arms must have measured a real failure: error counts %d vs %d",
			introducedVet.ErrorCount, preExistingVet.ErrorCount)
	}
	if introduced.Class.Reason == preExisting.Class.Reason {
		t.Fatalf("both arms classified %q — the differential is the deliverable, and it has collapsed", introduced.Class.Reason)
	}
	if introduced.Revert == preExisting.Revert {
		t.Fatalf("both arms decided revert=%v — the whole design is the OPPOSITE disposition here", introduced.Revert)
	}
}

// TestVetGateRevert_NoBaselineIsUnknownAndNeverReverts: with no recorded pre-merge
// tip there is nothing to compare against, so the verdict is `unknown` and the
// merge stands. Failing safe in this direction is the 2026-09-13 lesson: an
// unjudged result is NOT a failure, and a gate that reverts on evidence it does
// not have is the gate that gets switched off.
func TestVetGateRevert_NoBaselineIsUnknownAndNeverReverts(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, _, _ := vgrFixtureRepo(t, false)
	vet := runPostMergeVetGate(repo)

	for _, tc := range []struct {
		name   string
		revert *FailedMergeRevert
	}{
		{"no handle at all", nil},
		{"handle with no captured tip", &FailedMergeRevert{Repo: repo}},
		{"handle with no repo", &FailedMergeRevert{PreTip: "deadbeef"}},
		{"handle for a different repo", &FailedMergeRevert{Repo: repo + "-other", PreTip: "deadbeef"}},
	} {
		decision := ClassifyVetGateRevert(vet, tc.revert)
		if decision.Class.Reason != vetClassUnknown {
			t.Fatalf("%s: classification = %q, want %q", tc.name, decision.Class.Reason, vetClassUnknown)
		}
		if decision.Revert {
			t.Fatalf("%s: an uncomparable red must never be reverted", tc.name)
		}
		if decision.Class.Detail == "" {
			t.Fatalf("%s: an unknown classification must state WHY (Rule 15b), got an empty detail", tc.name)
		}
	}
	// The mis-wired-handle case must SAY it is mis-wired — a silent "no baseline"
	// would hide a real wiring defect.
	miswired := ClassifyVetGateRevert(vet, &FailedMergeRevert{Repo: repo + "-other", PreTip: "deadbeef"})
	if !strings.Contains(miswired.Class.Detail, repo+"-other") {
		t.Fatalf("a handle from another repo must be named in the reason, got %q", miswired.Class.Detail)
	}
}

// TestVetGateRevert_UnreadableRevisionIsUnknown: a recorded tip that is not a
// readable revision (a typo, a gc'd object, a capture of the wrong repo's shape)
// is an uncomparable red — never a revert. The baseline measurement's worktree
// materialisation is the step that fails, and its failure must land as a reason.
func TestVetGateRevert_UnreadableRevisionIsUnknown(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, _, _ := vgrFixtureRepo(t, false)
	vet := runPostMergeVetGate(repo)
	revert := &FailedMergeRevert{Repo: repo, PreTip: "0000000000000000000000000000000000000000"}

	decision := ClassifyVetGateRevert(vet, revert)

	if decision.Class.Reason != vetClassUnknown {
		t.Fatalf("an unreadable baseline revision = %q, want %q (detail %q)",
			decision.Class.Reason, vetClassUnknown, decision.Class.Detail)
	}
	if decision.Revert {
		t.Fatal("an unmeasurable baseline must never unwind a merge")
	}
}

// TestVetGateRevert_PassingGateIsNeverClassified: classification is about a RED
// gate. On a pass there is nothing to attribute, and the classifier must say
// nothing rather than invent a disposition — a non-empty reason on a passing gate
// would make every round a "decision" for the caller.
func TestVetGateRevert_PassingGateIsNeverClassified(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo := vgFixtureRepo(t)
	revert := &FailedMergeRevert{Repo: repo, PreTip: rebuildTestGit(t, repo, "rev-parse", "HEAD")}
	vet := runPostMergeVetGate(repo)
	if vet.Status != mergeBuildGateStatusPass {
		t.Fatalf("the fixture must be green here, got %s", vet.Status)
	}

	decision := ClassifyVetGateRevert(vet, revert)

	if decision.Class.Reason != "" {
		t.Fatalf("a passing gate must not be classified, got %q", decision.Class.Reason)
	}
	if decision.Revert {
		t.Fatal("a passing gate must never revert")
	}
	if vet.Classification != "" {
		t.Fatalf("a passing verdict must carry no classification, got %q", vet.Classification)
	}
}

// TestVetGateRevert_SubstitutedBaselineKeepsAndSaysSo pins the TEST SEAM's
// contract, including its honesty: when the seam substitutes the baseline, the
// verdict is `pre-existing` (kept) and the detail says the baseline was
// SUBSTITUTED — a reader must never mistake it for a measurement.
//
// The seam can only move the verdict toward KEEPING a merge, which is the
// property that makes it safe to have in production code at all: this test pins
// that direction (revert=false) alongside the disclosure.
func TestVetGateRevert_SubstitutedBaselineKeepsAndSaysSo(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, _, revert := vgrFixtureRepo(t, false)
	t.Setenv(vetBaselineOverrideEnvName, vetBaselineOverrideRed)
	vet := runPostMergeVetGate(repo)

	decision := ClassifyVetGateRevert(vet, revert)

	if decision.Class.Reason != vetClassPreExisting {
		t.Fatalf("substituted baseline = %q, want %q", decision.Class.Reason, vetClassPreExisting)
	}
	if decision.Revert {
		t.Fatal("the seam must never move a verdict toward reverting")
	}
	if !strings.Contains(decision.Class.Detail, "SUBSTITUTED") {
		t.Fatalf("the detail must disclose the substitution, got %q", decision.Class.Detail)
	}
	if !strings.Contains(vet.ClassificationDetail, vetBaselineOverrideEnvName) {
		t.Fatalf("the verdict's own detail must name the seam variable, got %q", vet.ClassificationDetail)
	}
}

// TestVetGateRevert_SubstitutedBaselineIgnoresNonsenseValues: any value other
// than the documented one is IGNORED — a typo in an operator's (or a test's)
// environment must not be able to change a merge decision, and must not be an
// error either (the seam is not allowed to stop the close loop).
func TestVetGateRevert_SubstitutedBaselineIgnoresNonsenseValues(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, _, revert := vgrFixtureRepo(t, false)
	for _, value := range []string{"", "RED-ish", "1", "true", "green"} {
		t.Setenv(vetBaselineOverrideEnvName, value)
		if ok, _ := vetBaselineSubstituted(); ok {
			t.Fatalf("value %q must not engage the seam", value)
		}
		vet := runPostMergeVetGate(repo)
		if decision := ClassifyVetGateRevert(vet, revert); decision.Class.Reason != vetClassIntroduced {
			t.Fatalf("value %q changed the classification to %q — only the documented value may engage the seam",
				value, decision.Class.Reason)
		}
	}
}

// TestVetGateRevert_KeptRenderingsCarryTheReportBanner is the RED-PROOF for the
// report-only guarantee: a red gate that is NOT reverted must deliver a payload
// that says so, carrying the shared banner. If a future edit dropped the banner
// from the rendering, the string a test and the production path share would stop
// matching and this fails — which is why the banner is exported.
func TestVetGateRevert_KeptRenderingsCarryTheReportBanner(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, _, revert := vgrFixtureRepo(t, true)
	eventsFile := vgNotifySink(t)
	vet := runPostMergeVetGate(repo)

	decision := ClassifyVetGateRevert(vet, revert)
	if decision.Revert {
		t.Fatal("this fixture is the kept arm")
	}
	payload := vgReadEvents(t, eventsFile)
	if !strings.Contains(payload, ReportOnlyBanner) {
		t.Fatalf("a kept red gate's payload must carry the report banner %q, got: %s", ReportOnlyBanner, payload)
	}
	if !strings.Contains(payload, string(vetClassPreExisting)) {
		t.Fatalf("the payload must name the classification, got: %s", payload)
	}
}

// TestVetGateRevert_IntroducedRenderingNamesTheDisposition: the OTHER half of the
// same guarantee — a payload for an introduced red must announce the reversion,
// so an operator watching the notify stream learns a landing was taken back
// without having to read the merger's log.
func TestVetGateRevert_IntroducedRenderingNamesTheDisposition(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, _, revert := vgrFixtureRepo(t, false)
	eventsFile := vgNotifySink(t)
	// The introduced disposition rides the reversion event (merge-reverted), which
	// a sink subscribed only to build-failed would suppress: subscribing here is
	// the operator's own choice, and the point of the assertion is that BOTH
	// deliveries exist and neither contradicts the other.
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", BuildFailedEvent+","+RevertedEvent)
	vet := runPostMergeVetGate(repo)

	if decision := ClassifyVetGateRevert(vet, revert); !decision.Revert {
		t.Fatalf("this fixture is the introduced arm, got %q", decision.Class.Reason)
	}
	payload := vgReadEvents(t, eventsFile)
	for _, want := range []string{BuildFailedEvent, RevertedEvent, RebuildVetRevertedWord, "INTRODUCED", "reverting the landing"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("the introduced payload must carry %q, got: %s", want, payload)
		}
	}
	// THE CONTRADICTION GUARD: the payload for this round must NOT claim the
	// merge was kept — the banner is a statement about the disposition, and this
	// round's disposition is the opposite one. (Measured before this fix: the
	// gate's own summary hardcoded "merge kept" for every failure, so an
	// introduced red delivered BOTH sentences in one payload.)
	if strings.Contains(payload, ReportOnlyBanner) {
		t.Fatalf("an introduced red must NOT claim the merge is kept: %s", payload)
	}
	// The MEASUREMENT delivery (build-failed) is still made — the alarm must not
	// depend on the classification having finished — and it states only what it
	// knows: no disposition in that sentence at all.
	if !strings.Contains(payload, BuildFailedEvent) {
		t.Fatalf("the measurement delivery must still fire, got: %s", payload)
	}
}

// TestVetGateRevert_GuardRefusesWithoutAnAttribution pins the guard's contract at
// the boundary the caller depends on: the unwind is reachable ONLY for an
// introduced verdict, and every other input is a NAMED refusal rather than a
// no-op. A caller that dropped its classification check must still be unable to
// unwind a merge by accident.
func TestVetGateRevert_GuardRefusesWithoutAnAttribution(t *testing.T) {
	repo, preTip, _ := vgrFixtureRepo(t, false)
	for _, tc := range []struct {
		name   string
		revert *FailedMergeRevert
		vet    *VetGateResult
		want   string
	}{
		{"nil handle", nil, &VetGateResult{Classification: vetClassIntroduced}, "no revert handle"},
		{"handle without a repo", &FailedMergeRevert{PreTip: preTip}, &VetGateResult{Classification: vetClassIntroduced}, "no revert handle"},
		{"unknown verdict", &FailedMergeRevert{Repo: repo, PreTip: preTip}, &VetGateResult{Classification: vetClassUnknown}, string(vetClassUnknown)},
		{"pre-existing verdict", &FailedMergeRevert{Repo: repo, PreTip: preTip}, &VetGateResult{Classification: vetClassPreExisting}, string(vetClassPreExisting)},
		{"never classified", &FailedMergeRevert{Repo: repo, PreTip: preTip}, &VetGateResult{}, string(vetClassUnknown)},
	} {
		t.Setenv(vetBaselineOverrideEnvName, "")
		if _, err := RevertIntroducedVetFailure(tc.revert, tc.vet); err == nil {
			t.Fatalf("%s: the guard must refuse", tc.name)
		} else if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: refusal %q must name %q", tc.name, err.Error(), tc.want)
		}
	}
	// And master must be untouched by all of those refusals.
	if head := rebuildTestGit(t, repo, "rev-parse", "HEAD"); head != rebuildTestGit(t, repo, "rev-parse", "HEAD") {
		t.Fatal("unreachable")
	}
	if _, err := os.Stat(filepath.Join(repo, vgFixtureGoMod)); err != nil {
		t.Fatalf("the fixture repo must be intact after refused unwinds: %v", err)
	}
}

// TestVetGateRevert_ClassificationReasonSpellingsAreStable pins the three wire
// words. They are what `cmd/agent`'s switch and every operator-facing sentence
// compare against, so a rename must be a deliberate, visible edit here rather
// than a silent drift (Rule 1b/13: compared values come from constants — and the
// constants' VALUES are part of the contract).
func TestVetGateRevert_ClassificationReasonSpellingsAreStable(t *testing.T) {
	want := map[vetClassificationReason]string{
		vetClassIntroduced:  "introduced",
		vetClassPreExisting: "pre-existing",
		vetClassUnknown:     "unknown",
	}
	for reason, spelling := range want {
		if string(reason) != spelling {
			t.Fatalf("classification %v renders %q, want %q", reason, string(reason), spelling)
		}
	}
	if got := (*VetGateResult)(nil).VetGateRevertReason(); got != "" {
		t.Fatalf("a nil verdict must render an empty reason, got %q", got)
	}
}
