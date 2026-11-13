package tools

// Red proof for arm 1's ENFORCEMENT half (the run's OWN untracked residue at
// close-out). This file is written against the PRE-FIX API ONLY — every symbol
// it calls (TaskSubmit / seedDeadHolderRow / TaskStart / TaskRunWriteback /
// openTestTasksDB / reclaimUntrackedResidueMarker) exists on master — so the
// SAME file compiles on both trees and the two runs differ by the fix alone:
//
//	# baseline (master 30450fc6) — copy THIS FILE ONLY; nothing else is needed
//	git worktree add --detach <scratch>/arm1-red master
//	cp internal/tools/zz_redproof_run_untracked_residue_test.go <scratch>/arm1-red/internal/tools/
//	cd <scratch>/arm1-red && go test -count=1 -v -run 'TestZZRedProof_RunOwnUntrackedResidue' ./internal/tools/
//	--- FAIL: ... UNNAMED RESIDUE: the run closed out `completed` …
//
//	# this branch
//	go test -count=1 -run 'TestZZRedProof_RunOwnUntrackedResidue' ./internal/tools/
//	--- PASS
//
// The property: a run that closes out `completed` while its OWN recorded
// worktree still carries untracked-only scratch must leave an evidenced,
// ATTRIBUTED record on the row's trail (the classifier's own wording + the
// offending entry + the run's worktree). Pre-fix nothing is recorded at all,
// and the same residue is later rendered by the sweep as the generic
// "uncommitted work" wording — indistinguishable from somebody's tracked work.
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZZRedProof_RunOwnUntrackedResidueIsRecordedAndAttributed(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-residue", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-residue", repo, branch, wt)
	if _, err := TaskStart("t-residue", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// The run's own scratch, left behind in its own worktree: untracked-only.
	if err := os.WriteFile(filepath.Join(wt, "build.out"), []byte("residue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-residue", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND detail LIKE ?`,
		"t-residue", "%"+reclaimUntrackedResidueMarker+"%").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatalf("UNNAMED RESIDUE: the run closed out `%s` while its OWN worktree %s still "+
			"carried untracked-only scratch (build.out) and NOTHING on the row's trail says so — "+
			"the same tree is later rendered as the generic %q, i.e. as somebody's uncommitted work",
			TaskStatusCompleted, wt, reclaimSkipDirtyReason)
	}
	// Attributed: the record names the run's OWN worktree and the entry, so an
	// operator can tell THIS run's scratch from somebody's work.
	var detail string
	if err := db.QueryRowContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT 1`,
		"t-residue").Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, wt) || !strings.Contains(detail, "build.out") {
		t.Fatalf("the close-out record must name the run's own worktree (%s) and the offending "+
			"entry (build.out); got %q", wt, detail)
	}
}
