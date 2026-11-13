package tools

// RED PROOF for the BINDING half of Rule 30's close-out check (epic "a dirty
// close-out IS not completed", measured case 01a0af50).
//
// Written against the PRE-FIX API ONLY — every symbol it calls (TaskSubmit,
// deadHolderReclaimFixture, seedDeliverable, TaskStart, TaskRunWriteback,
// openTestTasksDB, reclaimSkipDirtyReason) EXISTS on master — so this ONE file
// compiles UNCHANGED on both trees and the two runs differ by the fix alone:
//
//	# baseline (pristine master) — copy THIS FILE ONLY into a detached worktree:
//	git worktree add --detach <scratch>/dirty-ab master
//	grep -rl 'func deadHolderReclaimFixture' /dev/null; # provided by master
//	cp internal/tools/zz_redproof4_dirty_closeout_binding_ab_test.go <scratch>/dirty-ab/internal/tools/
//	cd <scratch>/dirty-ab && go test -count=1 -v \
//	  -run TestZZRedProof4_DirtyCloseoutIsRefused ./internal/tools/
//	--- FAIL: ... the run closed out `completed` while its OWN worktree held
//	            UNCOMMITTED TRACKED WORK (work.txt) ...
//
//	# this branch — no copy at all (the file is already here):
//	go test -count=1 -v -run TestZZRedProof4_DirtyCloseoutIsRefused ./internal/tools/
//	--- PASS
//
// The property: a run that writes a tracked change into its OWN worktree and
// exits without committing must NOT be able to write back `completed`. Pre-fix
// the framework detects the dirt, REPORTS it, and files the row `completed`
// anyway — `completed` is the one status a human does not look at, so the
// finished work becomes invisible until somebody rescues it by hand (~1000
// lines in case 01a0af50).
//
// The A/B is deliberately phrased as a ROW-STATUS claim, not as "an error came
// back": pre-fix the writeback succeeds silently, so the red proof must fail on
// the OUTCOME (the row), which is what the row's reader sees.
import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestZZRedProof4_DirtyCloseoutIsRefused(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s4", Payload: "p", TaskID: "t-dirty-ab4", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-dirty-ab4", repo, branch, wt)
	if _, err := TaskStart("t-dirty-ab4", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// THE MEASURED CASE in miniature: a tracked file (work.txt is committed on
	// the fixture branch) edited and NOT committed — the 398-line implementation
	// that sat in the abandoned worktree — plus the untracked deliverable files
	// that rode with it.
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("deliverable\nreal edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "new_note.md"), []byte("devdoc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The writeback's own error is NOT the assertion (pre-fix there is none):
	// the ROW is, because the row is what the operator and the merge queue read.
	wbOut, wbErr := TaskRunWriteback("t-dirty-ab4", TaskStatusCompleted, "done — all green")
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id = ?`, "t-dirty-ab4").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == TaskStatusCompleted {
		t.Fatalf("UNCOMMITTED DELIVERABLE: the run closed out `%s` while its OWN worktree %s still "+
			"held UNCOMMITTED TRACKED WORK (%s) — the branch is invisible to every merge path and "+
			"the row is the one status nobody re-reads (writeback out=%q err=%v)",
			TaskStatusCompleted, wt, reclaimSkipDirtyReason, wbOut, wbErr)
	}
	if wbErr == nil {
		t.Fatalf("the refusal must be REPORTED to the caller (Rule 15), got a silent success with out=%q", wbOut)
	}
	// And the bytes stay where they are: the refusal is a STATUS decision, never
	// a commit, a stash or a delete.
	if _, err := os.Stat(filepath.Join(wt, "new_note.md")); err != nil {
		t.Fatalf("the deliverable must survive the refusal untouched: %v", err)
	}
}
