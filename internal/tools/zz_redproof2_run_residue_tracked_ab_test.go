// Red proof #2 for arm 1's ENFORCEMENT half: the TRACKED-CHANGE half of the
// contract. Written against the PRE-FIX API ONLY (TaskSubmit /
// deadHolderReclaimFixture / seedDeliverable / TaskStart / TaskRunWriteback /
// openTestTasksDB / reclaimUntrackedResidueMarker all exist on master), so this
// ONE file compiles UNCHANGED on master and on the branch and the two runs
// differ by the fix alone:
//
//	# baseline (master 30450fc6) — ONE file copied, nothing else:
//	git worktree add --detach <scratch>/arm1-ab master
//	cp internal/tools/zz_redproof2_run_residue_tracked_ab_test.go <scratch>/arm1-ab/internal/tools/
//	cd <scratch>/arm1-ab && go test -count=1 -v \
//	  -run TestZZRedProof2_RunOwnUntrackedResidue_TrackedChangeStaysTodays ./internal/tools/
//	--- PASS
//
//	# this branch — no copy at all (it is already here):
//	go test -count=1 -v -run TestZZRedProof2_RunOwnUntrackedResidue_TrackedChangeStaysTodays ./internal/tools/
//	--- PASS
//
// PASS on BOTH trees is the point: the property pre-fix handling guarantees for
// somebody's uncommitted WORK must survive the fix untouched, so a red proof for
// this half is a GREEN-ON-BOTH proof. (The "watch it go red" half of the pair is
// zz_redproof_run_untracked_residue_test.go — it FAILs on master, PASSes here.)
//
// The property: a worktree whose dirt includes a TRACKED modification carries
// somebody's WORK (not the run's hygiene) — the close-out must not be disturbed
// and the run's own-residue wording must never be filed against it. Today's
// refusal for that tree (the sweep's reclaimSkipDirtyReason / dirty-holder
// refusal) is left exactly as it was.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestZZRedProof2_RunOwnUntrackedResidue_TrackedChangeStaysTodays(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s2", Payload: "p", TaskID: "t-tracked-ab", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-tracked-ab", repo, branch, wt)
	if _, err := TaskStart("t-tracked-ab", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// A TRACKED modification (work.txt is committed on the fixture branch) plus
	// the same untracked scratch: somebody's WORK is present, so the arm's
	// hygiene claim does not apply and today's handling is what must happen.
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("deliverable\nreal edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "build.out"), []byte("residue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The writeback is REFUSED on this tree (the binding gate for the tracked
	// shape) — that is now the expected outcome, and it is NOT what this file
	// pins. What it pins is unchanged: the run's own UNTRACKED-residue wording
	// (reclaimUntrackedResidueMarker) may never be filed against somebody's
	// uncommitted work.
	_, _ = TaskRunWriteback("t-tracked-ab", TaskStatusCompleted, "done") //stylegate:ignore writeback in a run fixture; the ledger state read back afterwards is the assertion
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND detail LIKE ?`,
		"t-tracked-ab", "%"+reclaimUntrackedResidueMarker+"%").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a TRACKED change must keep today's refusal — the run's own residue wording "+
			"(%s) may never be filed against somebody's uncommitted work (found %d)",
			reclaimUntrackedResidueMarker, n)
	}
}
