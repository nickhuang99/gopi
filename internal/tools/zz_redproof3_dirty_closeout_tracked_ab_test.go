package tools

// Red proof #3 for the close-out check: the TRACKED shape (epic "a run must
// leave its worktree clean", 2026-09-16).
//
// Written against the PRE-FIX API ONLY — every symbol it calls (TaskSubmit /
// deadHolderReclaimFixture / seedDeliverable / TaskStart / TaskRunWriteback /
// openTestTasksDB / reclaimSkipDirtyReason) exists on master — so the SAME file
// compiles on both trees and the two runs differ by the fix alone:
//
//	# baseline (master ba53da3d) — copy THIS FILE ONLY; nothing else is needed
//	git worktree add --detach <scratch>/wtclean-ab master
//	cp internal/tools/zz_redproof3_dirty_closeout_tracked_ab_test.go <scratch>/wtclean-ab/internal/tools/
//	cd <scratch>/wtclean-ab && go test -count=1 -v \
//	  -run TestZZRedProof3_TrackedDirtyCloseoutIsVisible ./internal/tools/
//	--- FAIL: ... UNCOMMITTED DELIVERABLE: the run closed out `completed` ...
//
//	# this branch — the record is filed AND the writeback is now REFUSED (the
//	# binding gate; see zz_redproof4_dirty_closeout_binding_ab_test.go for the
//	# outcome-level A/B of that half):
//	go test -count=1 -v -run TestZZRedProof3_TrackedDirtyCloseoutIsVisible ./internal/tools/
//	--- PASS
//
// The property: a run that reports ready (`completed`) while its OWN recorded
// worktree still holds a TRACKED change — real work, never committed — must
// leave an evidenced record on its own trail, in the classifier's own words and
// naming the offending entry. Pre-fix the close-out is SILENT for this shape:
// the reconciliation half only ever spoke for untracked-only residue, and the
// same tree is later rendered by the sweep as the generic "uncommitted work"
// — indistinguishable from a live sibling session's in-progress edits. The
// branch meanwhile is invisible to every merge path while its row reads
// `completed` (measured 2026-09-16, row 01a0aa8b).
//
// The writeback's own error is tolerated HERE on purpose: this file pins the
// RECORD (the trail must carry the shape on every tree that has the record arm),
// while the stronger outcome — the writeback itself is refused — is pinned
// separately by zz_redproof4. Both must hold; neither replaces the other.
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZZRedProof3_TrackedDirtyCloseoutIsVisible(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-dirty-ab", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-dirty-ab", repo, branch, wt)
	if _, err := TaskStart("t-dirty-ab", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// work.txt is COMMITTED on the fixture branch: modifying it is an
	// uncommitted DELIVERABLE, not the run's scratch.
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("deliverable\nnever committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The record is the property under test; the refusal is the stronger half
	// (zz_redproof4). A refused writeback writes nothing, so the record is filed
	// by the refusal path itself — both outcomes are tolerated here so neither
	// masks the property this file pins.
	_, _ = TaskRunWriteback("t-dirty-ab", TaskStatusCompleted, "done — elapsed=1s") //stylegate:ignore writeback in a run fixture; the ledger state read back afterwards is the assertion
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND detail LIKE ?`,
		"t-dirty-ab", "%"+reclaimSkipDirtyReason+"%").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatalf("UNCOMMITTED DELIVERABLE: the run closed out `%s` while its OWN worktree %s still held "+
			"a TRACKED change (work.txt) and NOTHING on the row's trail says so — the branch is then "+
			"invisible to every merge path while the row reads completed", TaskStatusCompleted, wt)
	}
	var detail string
	if err := db.QueryRowContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? AND detail LIKE ? ORDER BY id DESC LIMIT 1`,
		"t-dirty-ab", "%"+reclaimSkipDirtyReason+"%").Scan(&detail); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{wt, "work.txt"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("the close-out record must name %q — got %q", want, detail)
		}
	}
}
