package tools

// REGRESSION for row 01a183fa-91e9-7a79-901f-57fba1129af6: the delivery point
// held the very tip under judgement, that tip's objects were NOT readable in any
// directory this host could measure from, the content leg therefore never ran —
// and the arm answered with the DETERMINISTIC sentence
//
//	its deliverable is not at the destination it declared:
//	the tip eaaf6b43ab0e … is in NEITHER the ancestry NOR the patches NOR the
//	content of refs/remotes/origin/master (zbook:/home/nick/work/gopi = eaaf6b43ab0e)
//
// whose own parenthetical named the destination as the SAME SHA. The row's
// `completed` writeback was refused, it stayed `in_progress`, its ground was never
// released and its worker was reported failed — for work that had landed.
//
// THE FIXTURE IS THE INCIDENT'S OWN SHAPE, and every element of it is load
// bearing:
//
//	the delivery point's master holds the run's tip        ← `= eaaf6b43` in the sentence
//	the GROUND (the row's declared directory) does NOT hold that tip's objects
//	the recorded repo does not hold them either
//	so: the live witness cannot resolve the sha it was told, the two cached refs
//	    point at the pre-push base, and no patch or content comparison can run.
//
// That is exactly "the destination is the tip and nothing here can read it".
//
// BEFORE THE REPAIR this fixture is REFUSED (state `undelivered`, the marker
// sentence, the row left open). AFTER IT the arm refreshes the destination view
// once, which brings the tip's objects into the ground and makes the containment
// question ANSWERABLE — so the same fixture is `confirmed` and the write lands.
// The second arm below pins the other direction: with the refresh switched off the
// same unreadable view must be `unverified` (recorded, NOT refused, and never the
// absence sentence), because refusing what cannot be seen is what wedged the row.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestDeliveryProof_DestinationIsTheTipButUnreadableIsNotAbsent is the repair's
// acceptance arm: the exact incident shape must be CONFIRMED (the refresh made the
// question answerable), never reported as a measured absence, and the write must
// land.
func TestDeliveryProof_DestinationIsTheTipButUnreadableIsNotAbsent(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, ground, _, tip := dpuoFixture(t)

	id := "dpuo-delivered"
	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "fix: the work landed and the local view was stale", taskStatusInProgress)
	// The declared branch is the INTEGRATION branch: the clone channel publishes
	// `HEAD:refs/heads/master`, so that is the destination that resolves (the row's
	// own `task/<id>` address is never pushed anywhere) — the incident row's shape.
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)
	// The tip is named the way the incident's row named it: the channel's own
	// landed record, because no directory here holds its objects.
	dpggRecord(t, db, id, branch, ground, tip)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.State == deliveryProofAbsent {
		t.Fatalf("the destination IS the tip (%s) and only the local view was unreadable — reporting a measured "+
			"absence is the incident's defect; state=%q note=%s", branchDeleteShortHash(tip), out.State, out.Note)
	}
	if strings.Contains(out.Note, deliveryProofMarker) {
		t.Errorf("the deterministic absence sentence %q must never be written for an unreadable view; note=%s",
			deliveryProofMarker, out.Note)
	}
	if out.State != deliveryProofConfirmed && out.State != deliveryProofUnverified {
		t.Fatalf("state = %q, want %q or %q", out.State, deliveryProofConfirmed, deliveryProofUnverified)
	}
	// AND THE REFRESH IS WHAT MADE IT ANSWERABLE: it must have run, and it must
	// have run in the ground (the run's own directory).
	if !strings.Contains(out.Refresh, "refreshed") {
		t.Errorf("the pre-judgement refresh did not run: %q", out.Refresh)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusInProgress {
		t.Fatalf("fixture: the row must start open, got %q", got)
	}
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err != nil {
		t.Fatalf("a delivered row must not be refused: %v", err)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusCompleted {
		t.Errorf("row status = %q, want %q", got, taskStatusCompleted)
	}
	if n := dpEventCount(t, db, id, deliveryProofOwedEvent); n != 0 {
		t.Errorf("no %s record may be written for a delivered row, got %d", deliveryProofOwedEvent, n)
	}
}

// TestDeliveryProof_UnreadableViewIsUnverifiedNotAbsent is the repair's
// DEGRADATION arm: with the pre-judgement refresh switched off, the same shape is
// unmeasurable — and an unmeasurable question is `unverified` (recorded on the
// trail), never the absence sentence and never a refusal.
func TestDeliveryProof_UnreadableViewIsUnverifiedNotAbsent(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, ground, _, tip := dpuoFixture(t)

	deliveryProofRefreshOff = true
	defer func() { deliveryProofRefreshOff = false }()

	id := "dpuo-unreadable"
	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "fix: nothing local can read the destination's tip", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)
	dpggRecord(t, db, id, branch, ground, tip)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.State == deliveryProofAbsent {
		t.Fatalf("state = %q: an unreadable object is an absence of EVIDENCE, not evidence of absence "+
			"(note=%s)", out.State, out.Note)
	}
	if out.State != deliveryProofUnverified {
		t.Fatalf("state = %q, want %q; note=%s", out.State, deliveryProofUnverified, out.Note)
	}
	if strings.Contains(out.Note, deliveryProofMarker) {
		t.Errorf("the unverified note must not carry the deterministic absence sentence %q: %s",
			deliveryProofMarker, out.Note)
	}
	if !containsAll(out.Note, "UNVERIFIED", "NOT disproved") {
		t.Errorf("the unverified note must state what it is and what it is not, got: %s", out.Note)
	}
	// THE ROW IS NOT JUDGED DEAD: the write is allowed, and the question is on the
	// trail as the loud half (Rule 15).
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err != nil {
		t.Fatalf("an unverifiable delivery claim must not refuse the write (that refusal is the incident): %v", err)
	}
	if n := dpEventCount(t, db, id, DeliveryProofUnverifiedEvent); n == 0 {
		t.Errorf("the unverified verdict must be recorded on the row's trail as %s", DeliveryProofUnverifiedEvent)
	}
	if n := dpEventCount(t, db, id, deliveryProofOwedEvent); n != 0 {
		t.Errorf("an unverified verdict must not be recorded as an owed delivery (%s), got %d",
			deliveryProofOwedEvent, n)
	}
}
