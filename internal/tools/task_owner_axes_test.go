// task_owner_axes_test.go — the MEMBERSHIP regression guard for epic 01a13214.
//
// THE MEASURED DEFECT, on the real ledger (2026-10-12):
//
//	$ gopi merge --status
//	  BURIED base-peermaster-01a12b54 — 01a12b54-e636-… (completed) — unowned
//	$ gopi task archive --check
//	    base-peermaster-01a12b54 — unowned — (none) — landed
//
// One branch, one ledger, two owner readings: `merge --status` resolves through
// TaskForBranch, whose matcher reads the row's PRIMARY branch AND the SECONDARY
// grounds it created; the archive face read deliverable.branch only, through its
// own SQL + its own JSON parse. Row 01a12b54-e636-… names
// task/01a12b54e636796fa97e5cfccd3ebb06 on its primary axis and
// base-peermaster-01a12b54 on a ground — so one face saw an owner and the other
// saw none.
//
// RED PROOF: these tests fail on the pre-change tree with an ASSERTION failure,
// not a compile failure — every assertion goes through the OLD tree's own
// exported/unexported readers and its own ledger, and the old archive face
// simply had no key for a ground-only branch. The mutation used to re-run the
// red state on the NEW tree is `taskDeliverableOwnerAxes` returning the primary
// axis alone (the old membership, restored in ONE place — which is the point of
// the shared enumeration).
package tools

import (
	"context"
	"testing"
)

// THE REAL SAMPLE. These two names are not invented: they are the branch names
// of the row the divergence was measured on (`gopi task show
// 01a12b54-e636-796f-a97e-5cfccd3ebb06`), so the fixture reproduces the observed
// shape — a primary branch of its own, and the ground it created.
const (
	ownerAxesMeasuredRowID        = "01a12b54-e636-796f-a97e-5cfccd3ebb06"
	ownerAxesMeasuredPrimary      = "task/01a12b54e636796fa97e5cfccd3ebb06"
	ownerAxesMeasuredGroundBranch = "base-peermaster-01a12b54"
	ownerAxesFixtureRepo          = "/repo"
)

// seedGroundOwnerRow files one row whose deliverable carries BOTH axes: the
// primary branch (deliverable.branch) and a ground the run created
// (deliverable.grounds), exactly as taskBackfillDeliverableGrounds writes them.
func seedGroundOwnerRow(t *testing.T, ctx context.Context, taskID, status, primary, ground string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "axes " + taskID, Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	deliverable := `{"repo":"` + ownerAxesFixtureRepo + `","branch":"` + primary + `","worktree":"/wt-` + taskID + `",` +
		`"grounds":[{"repo":"` + ownerAxesFixtureRepo + `","branch":"` + ground + `","worktree":"/wt-ground-` + taskID + `"}]}`
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, deliverable = ? WHERE task_id = ?`,
		status, deliverable, taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// TestOwnerAxesBothFacesNameTheSameOwnerForTheMeasuredSample is the epic's
// acceptance, asserted at the level where the divergence lived: the SET of rows
// that own a branch, read by each face's own production path.
//
// It pins the exact sample (ownerAxesMeasuredGroundBranch) and requires the
// archive face to name the SAME row the branch lookup names.
func TestOwnerAxesBothFacesNameTheSameOwnerForTheMeasuredSample(t *testing.T) {
	ctx := newArchiveOwnerLedger(t)
	seedGroundOwnerRow(t, ctx, ownerAxesMeasuredRowID, taskStatusCompleted,
		ownerAxesMeasuredPrimary, ownerAxesMeasuredGroundBranch)

	// FACE 1 — the branch lookup (`gopi merge --status` → TaskForBranch).
	lookup, err := TaskForBranch(TaskForBranchQuery{Branch: ownerAxesMeasuredGroundBranch}, "")
	if err != nil {
		t.Fatalf("TaskForBranch: %v", err)
	}
	if len(lookup.Owners) != 1 || lookup.Owners[0].TaskID != ownerAxesMeasuredRowID {
		t.Fatalf("the branch lookup must name %s as owner, got %+v", ownerAxesMeasuredRowID, lookup.Owners)
	}

	// FACE 2 — the archive face (`gopi task archive --check` → archiveBranchOwners,
	// which is archiveReclaimAllOwners + the most-cautious pick).
	owners, err := archiveBranchOwners()
	if err != nil {
		t.Fatalf("archiveBranchOwners: %v", err)
	}
	got, ok := owners[ownerAxesMeasuredGroundBranch]
	if !ok {
		t.Fatalf("THE DIVERGENCE IS BACK: the archive face has no owner for the ground branch %s — "+
			"the two faces disagree about which rows ARE candidates (owners=%+v)",
			ownerAxesMeasuredGroundBranch, owners)
	}
	if got.taskID != ownerAxesMeasuredRowID {
		t.Fatalf("the archive face names %s, the branch lookup names %s — one branch may not have two owners",
			got.taskID, ownerAxesMeasuredRowID)
	}
	if got.status != taskStatusCompleted {
		t.Fatalf("the archive face must read the ROW's status, got %q", got.status)
	}

	// FACE 2's OTHER half — the reclaim arm, which asks the same question for a
	// different purpose ("may a live process still be building here?"). It reads
	// rows rather than the pick, so it is asserted on its own production entry.
	rows, err := archiveReclaimOwnersFromLedger(ownerAxesMeasuredGroundBranch)
	if err != nil {
		t.Fatalf("archiveReclaimOwnersFromLedger: %v", err)
	}
	if len(rows) != 1 || rows[0].TaskID != ownerAxesMeasuredRowID {
		t.Fatalf("the reclaim arm must see the same row as the lookup, got %+v", rows)
	}
}

// TestOwnerAxesEnumerateBothAxesOfARow pins the SHARED enumeration itself: one
// row contributes its primary branch and each ground it created, deduplicated,
// and nothing else.
func TestOwnerAxesEnumerateBothAxesOfARow(t *testing.T) {
	ctx := newArchiveOwnerLedger(t)
	seedGroundOwnerRow(t, ctx, "t-axes-both", taskStatusInProgress, "task/t-axes-both", "feat/axes-ground")

	all, err := taskOwnerAxesAll()
	if err != nil {
		t.Fatalf("taskOwnerAxesAll: %v", err)
	}
	seen := map[string]string{}
	for _, rec := range all {
		seen[rec.branch] = rec.taskID
	}
	for _, axis := range []string{"task/t-axes-both", "feat/axes-ground"} {
		if seen[axis] != "t-axes-both" {
			t.Fatalf("axis %q must be enumerated with its row, got %q (all=%+v)", axis, seen[axis], all)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("the enumeration emitted %d axes, want the row's 2 (%+v)", len(seen), seen)
	}
}

// TestOwnerAxesDeduplicateAPrimaryThatIsAlsoAGround pins the dedup: a blob that
// lists the row's own branch again among its grounds (a shape an older writer
// could leave behind) must not make one row look like two candidates.
func TestOwnerAxesDeduplicateAPrimaryThatIsAlsoAGround(t *testing.T) {
	ctx := newArchiveOwnerLedger(t)
	seedGroundOwnerRow(t, ctx, "t-axes-dup", taskStatusCompleted, "feat/axes-dup", "feat/axes-dup")

	all, err := taskOwnerAxesAll()
	if err != nil {
		t.Fatalf("taskOwnerAxesAll: %v", err)
	}
	n := 0
	for _, rec := range all {
		if rec.branch == "feat/axes-dup" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("one row naming one branch must produce ONE record, got %d (%+v)", n, all)
	}
	owners, err := archiveBranchOwners()
	if err != nil {
		t.Fatalf("archiveBranchOwners: %v", err)
	}
	if got := owners["feat/axes-dup"].taskID; got != "t-axes-dup" {
		t.Fatalf("the deduplicated axis must still be owned, got %q", got)
	}
}

// TestArchiveOwnerRankSurvivesOnAGroundAxis is the boundary half of the epic:
// the AXES are unified, the archive face's RANK is NOT. A branch named only by
// grounds is still described by the SAFEST reading (live > decided > archivable,
// task id as the tie-break) — merging the membership must not smuggle in the
// branch lookup's own preference for the live row as the whole policy.
//
// The decided row is seeded FIRST and its id sorts first, so neither scan order
// nor the tie-break could rescue a rank-blind pick.
func TestArchiveOwnerRankSurvivesOnAGroundAxis(t *testing.T) {
	const ground = "ground/rank-sample"
	ctx := newArchiveOwnerLedger(t)
	seedGroundOwnerRow(t, ctx, "01a100aa-decided", taskStatusCompleted, "task/01a100aa-decided", ground)
	seedGroundOwnerRow(t, ctx, "01a100bb-live", taskStatusInProgress, "task/01a100bb-live", ground)

	owners, err := archiveBranchOwners()
	if err != nil {
		t.Fatalf("archiveBranchOwners: %v", err)
	}
	got, ok := owners[ground]
	if !ok {
		t.Fatalf("a ground-named branch must be owned, got %+v", owners)
	}
	if got.status != taskStatusInProgress || got.taskID != "01a100bb-live" {
		t.Fatalf("the LIVE row must still decide the branch (archiveOwnerRank): got %q for %s",
			got.status, got.taskID)
	}

	// AND THE OTHER DIRECTION, which is the one the rank exists for: with no live
	// row the DECIDED row stands, and the gate then treats it as decided rather
	// than as absent — the membership unification is what makes that possible at
	// all for a ground-only branch.
	if _, err := archiveBranchOwnersForTestSingle(t, taskStatusCompleted); err != nil {
		t.Fatalf("decided-only ground row: %v", err)
	}
}

// archiveBranchOwnersForTestSingle drives the decided-only half of the rank on a
// FRESH ledger (so the live row seeded above cannot mask it): one completed row
// naming a branch through a ground must be visible to the archive face AND read
// as decided by the gate's own predicate.
func archiveBranchOwnersForTestSingle(t *testing.T, status string) (archiveOwner, error) {
	t.Helper()
	ctx := newArchiveOwnerLedger(t)
	const branch = "ground/decided-only"
	seedGroundOwnerRow(t, ctx, "01a100cc-decided-only", status, "task/01a100cc-decided-only", branch)
	owners, err := archiveBranchOwners()
	if err != nil {
		return archiveOwner{}, err
	}
	got, ok := owners[branch]
	if !ok {
		t.Fatalf("a decided ground-only row must own its branch, got %+v", owners)
	}
	if !archiveOwnerDecided(got.status) {
		t.Fatalf("the gate reads the ground row's status as %q, which is not DECIDED", got.status)
	}
	return got, nil
}
