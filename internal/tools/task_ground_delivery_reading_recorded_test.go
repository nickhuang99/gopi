package tools

// task_ground_delivery_reading_recorded_test.go — the CLONE CHANNEL's delivered
// shape, and the arm that stops this repair from becoming a blanket pass.
//
// THE DEFECT (measured 2026-11-05, row 01a1a891-c437). `gopi task show` rendered
//
//	delivery (read now): UNCHECKED — no ref in any place this ground can hold
//	task/01a1a891c4377abda1ad7a6efb484f42 carries it ...
//
// for a row whose commit (f43cd829…, and dd369c8d… before it) WAS an ancestor of
// origin/master — locally and on the peer:
//
//	$ git merge-base --is-ancestor f43cd829fbc8c36ec961203418cf4d6e7e37ab89 origin/master; echo $?
//	0
//
// The channel publishes the INTEGRATION branch (`HEAD:refs/heads/<integration>`)
// and its close-out RECLAIMS the run's own `task/<id>` ref, so the row's ADDRESS
// is absent BY DESIGN on every row it delivered. The reading asked only the
// address (`taskGroundDeliveryTip`), got "", and fell to its "nothing is
// anywhere" sentence — for work that had landed.
//
// WHAT THESE ARMS PIN, in the three directions the repair must be right in:
//
//	(A) address gone + the row's landed record names a tip the recorded repo's
//	    integration ref CONTAINS ⇒ MERGED-LOCALLY (never UNCHECKED) — the
//	    measured shape, and the regression this file exists for;
//	(B) the SAME record naming a tip no integration ref contains ⇒ still NOT a
//	    delivery. A record states that a push was attempted; it is not evidence
//	    of arrival, so this reads UNCHECKED and names the refs it asked. Without
//	    this arm the repair would turn every self-reported push into a pass —
//	    the same discipline the sibling delivery-proof arm states as
//	    `!tipFromLandedRecord`;
//	(C) no witness at all ⇒ UNCHECKED, and the why names BOTH the directories it
//	    asked and the landed record it consulted (Rule 15: "asked and had
//	    nothing" must be distinguishable from "nobody asked").
//
// Every arm drives the REAL `TaskShow` digest and asserts on the RENDERED line,
// not on the reading struct, so a render that drops the evidence fails here.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// recordedDeliveryLandOnIntegration publishes the clone ground's commit onto the
// recorded repo's INTEGRATION branch — the transfer the clone channel makes — and
// then destroys the ground, which is what the close-out does to a delivered row.
//
// The push goes through a TEMPORARY ref and is fast-forwarded into the
// integration branch, because the recorded repo is a NON-BARE checkout standing
// on that branch: git refuses to update the checked-out branch of a non-bare
// repository (`receive.denyCurrentBranch`), which is a property of the fixture,
// not of the channel (the channel's own delivery point IS bare).
//
// The destruction is deliberate and it is the point: with the ground still on
// disk the branch axis would answer from the clone's HEAD for the WRONG reason
// (a live ground), and the arm would stop measuring the shape the defect was
// filed on. It removes only the directory this fixture created (Rule 42: the
// caller created it and holds it).
func recordedDeliveryLandOnIntegration(t *testing.T, f *deliveryReadingFixture) string {
	t.Helper()
	tip := f.cloneTip(t)
	const stagingRef = refsHeadsPrefix + recordedDeliveryStagingBranch
	covLifeGit(t, f.clone, covLifeGitBinary, "push", "-q", f.repo, f.branch+":"+stagingRef)
	covLifeGit(t, f.repo, covLifeGitBinary, "merge", "-q", "--ff-only", recordedDeliveryStagingBranch)
	// Assert the transfer landed before destroying the only other copy: a push
	// that silently failed would make arm (A) measure a state it did not build.
	if got := covLifeGit(t, f.repo, covLifeGitBinary, "rev-parse", masterBranchName); got != tip {
		t.Fatalf("fixture precondition: %s must be at the row's tip %s, got %q", masterBranchName, tip, got)
	}
	if err := os.RemoveAll(f.clone); err != nil {
		t.Fatalf("remove the clone ground: %v", err)
	}
	if _, err := os.Stat(f.clone); !os.IsNotExist(err) {
		t.Fatalf("fixture precondition: the ground must be GONE (%s), stat said %v", f.clone, err)
	}
	return tip
}

// recordedDeliveryStagingBranch is the throwaway ref the fixture pushes through
// before fast-forwarding the integration branch (named once — Rule 1b).
const recordedDeliveryStagingBranch = "recorded-delivery-staging"

// recordedDeliverySideBranch is the NON-integration ref arm (B) parks the claimed
// tip on: present in the repo (so containment could be measured there) and
// contained by no integration ref.
const recordedDeliverySideBranch = "recorded-delivery-side"

// recordedDeliveryIsAncestor asks the ancestry question NON-fatally: `git
// merge-base --is-ancestor` answers by EXIT STATUS (0 yes, 1 no), so the fixture
// helper that fails on any non-zero exit cannot be used for it.
func recordedDeliveryIsAncestor(t *testing.T, dir, commit, ref string) bool {
	t.Helper()
	cmd := exec.Command(covLifeGitBinary, "merge-base", "--is-ancestor", commit, ref)
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return true
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		return false
	}
	t.Fatalf("merge-base --is-ancestor %s %s (in %s): %v", commit, ref, dir, err)
	return false
}

// TestTaskShowDeliveryReadsTheLandedRecordWhenTheAddressIsGone is arm (A) — THE
// regression. The row's address ref does not exist anywhere (the channel
// reclaimed it), the ground is gone, and the only witness of the tip is the
// row's own landed record; the commit is nevertheless contained in the recorded
// repo's integration branch, so the reading must say the work landed.
func TestTaskShowDeliveryReadsTheLandedRecordWhenTheAddressIsGone(t *testing.T) {
	f := newDeliveryReadingFixture(t, "task/01a17df2recorded")
	tip := recordedDeliveryLandOnIntegration(t, f)
	if taskBranchExists(f.clone, f.branch) || taskBranchExists(f.repo, f.branch) {
		t.Fatalf("fixture precondition: the row's ADDRESS %s must exist nowhere (that absence IS the defect)", f.branch)
	}
	if got := f.shim.holds(t, f.branch); got != "" {
		t.Fatalf("fixture precondition: the peer must advertise nothing for %s, got %s", f.branch, got)
	}

	const taskID = "t-delivery-recorded"
	deliveryReadingSeedRow(t, taskID, taskStatusCompleted, f.deliverableBlob())
	// The channel's own write, in the channel's own grammar — the same call the
	// delivery loop makes, so this arm cannot pass on a hand-typed detail the
	// real reader would reject.
	TaskDeliveryRecordLanded(taskID, f.branch, f.clone, tip, "pushed master to origin after 1 attempt(s)")

	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := deliveryReadingLine(t, out)
	if got := deliveryReadingVerdict(t, line); got != TaskShowDeliveryMergedLocally {
		t.Fatalf("a delivered clone-channel row (address reclaimed, tip on %s) must read %s, got %q (%q)",
			masterBranchName, TaskShowDeliveryMergedLocally, got, line)
	}
	// The facts an operator needs: the commit, the ref that CONTAINS it, and the
	// witness that named it — a verdict with no evidence behind it is the shape
	// this whole file exists to prevent.
	for _, want := range []string{tip, masterBranchName, "landed record"} {
		if !strings.Contains(line, want) {
			t.Errorf("the %s why must carry %q, got %q", TaskShowDeliveryMergedLocally, want, line)
		}
	}
	// And it must not overclaim the PEER: nothing here asked the peer's own
	// integration branch (that needs a fetch, and this line writes nothing).
	if !strings.Contains(line, "not asked") {
		t.Errorf("the %s why must state that the PEER's integration branch was not asked: %q",
			TaskShowDeliveryMergedLocally, line)
	}
	// READER CONTRACT, unchanged by this repair: the row's deliverable is
	// byte-identical across the show.
	advisorySeedRowDigestUnchanged(t, taskID, f.deliverableBlob())
}

// TestTaskShowDeliveryNeverTrustsTheRecordedTipAsArrival is arm (B), the one
// that makes arm (A) safe. The landed record names a commit, the recorded repo
// can resolve it, and NO integration ref contains it — the run claimed a push
// that did not land. The reading must not become a verdict.
func TestTaskShowDeliveryNeverTrustsTheRecordedTipAsArrival(t *testing.T) {
	f := newDeliveryReadingFixture(t, "task/01a17df2claimed")
	tip := f.cloneTip(t)
	// The commit is IN the recorded repo (so containment could be measured
	// there) and on a branch that is NOT an integration ref.
	covLifeGit(t, f.clone, covLifeGitBinary, "push", "-q", f.repo, f.branch+":"+refsHeadsPrefix+recordedDeliverySideBranch)
	if got := covLifeGit(t, f.repo, covLifeGitBinary, "rev-parse", refsHeadsPrefix+recordedDeliverySideBranch); got != tip {
		t.Fatalf("fixture precondition: the repo must carry the tip at %s%s, got %q", refsHeadsPrefix, recordedDeliverySideBranch, got)
	}
	if recordedDeliveryIsAncestor(t, f.repo, tip, masterBranchName) {
		t.Fatalf("fixture precondition: %s must NOT be contained in %s", tip, masterBranchName)
	}
	if err := os.RemoveAll(f.clone); err != nil {
		t.Fatalf("remove the clone ground: %v", err)
	}

	const taskID = "t-delivery-claimed"
	deliveryReadingSeedRow(t, taskID, taskStatusCompleted, f.deliverableBlob())
	TaskDeliveryRecordLanded(taskID, f.branch, f.clone, tip, "pushed master to origin after 1 attempt(s)")

	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := deliveryReadingLine(t, out)
	if got := deliveryReadingVerdict(t, line); got != TaskShowDeliveryUnchecked {
		t.Fatalf("a record claiming a push no integration ref contains must read %s, never a delivery verdict, got %q (%q)",
			TaskShowDeliveryUnchecked, got, line)
	}
	// The evidence: the tip the record claimed AND the refs that were asked —
	// so "not landed" is distinguishable from "asked the wrong ref".
	for _, want := range []string{tip, masterBranchName} {
		if !strings.Contains(line, want) {
			t.Errorf("the UNCHECKED why must carry %q, got %q", want, line)
		}
	}
	// The sentence must say what the record IS: a claim, not an arrival.
	if !strings.Contains(line, "NOT evidence of arrival") {
		t.Errorf("the UNCHECKED why must deny the record's authority over arrival: %q", line)
	}
	if strings.Contains(line, "only in its own ground") {
		t.Errorf("no directory was read, so the why must not name one as holding the work: %q", line)
	}
}

// TestTaskShowDeliveryNamesTheLandedRecordItAskedWhenNothingIsNamed is arm (C):
// no address ref anywhere, no landed record, nothing readable. The why must name
// the directories asked AND the row's own record — a reader has to be able to
// tell "this witness was asked and had nothing" from "nobody asked it".
func TestTaskShowDeliveryNamesTheLandedRecordItAskedWhenNothingIsNamed(t *testing.T) {
	f := newDeliveryReadingFixture(t, "task/01a17df2nowitness")
	covLifeGit(t, f.clone, covLifeGitBinary, "checkout", "-q", masterBranchName)
	covLifeGit(t, f.clone, covLifeGitBinary, "branch", "-q", "-D", f.branch)
	if err := os.RemoveAll(f.clone); err != nil {
		t.Fatalf("remove the clone ground: %v", err)
	}
	if taskBranchExists(f.repo, f.branch) {
		t.Fatalf("fixture precondition: the recorded repo must not hold %s", f.branch)
	}

	const taskID = "t-delivery-nowitness"
	deliveryReadingSeedRow(t, taskID, taskStatusFailed, f.deliverableBlob())

	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := deliveryReadingLine(t, out)
	if got := deliveryReadingVerdict(t, line); got != TaskShowDeliveryUnchecked {
		t.Fatalf("a reading with no witness at all must be %s, got %q (%q)", TaskShowDeliveryUnchecked, got, line)
	}
	for _, want := range []string{f.clone, f.repo, deliveryProofLandedWitnessText} {
		if !strings.Contains(line, want) {
			t.Errorf("the UNCHECKED why must name %q among the places asked, got %q", want, line)
		}
	}
}
