package tools

// Red proof for the dirty dead-holder salvage arm (zz-prefixed per the
// repo's convention for A/B artifacts).
//
// It is written against the PRE-FIX API ONLY, so the SAME file compiles on
// both trees and the two runs differ by the fix alone:
//
//	# baseline (master eb802d64)
//	cp internal/tools/zz_redproof_dirty_holder_salvage_test.go <baseline>/internal/tools/
//	cd <baseline> && go test -count=1 -run TestZZRedProof_DirtyDeadHolderWorkIsOrphaned ./internal/tools/
//	--- FAIL: ... ORPHANED WORK: the merger refused the dirty dead-holder worktree
//	    of feat/dead-holder ... and filed NO task for its uncommitted work
//
//	# this branch
//	go test -count=1 -run TestZZRedProof_DirtyDeadHolderWorkIsOrphaned ./internal/tools/
//	--- PASS
//
// The property: the merger pass over a repo whose dead-holder worktree is DIRTY
// must not leave the work with no carrier. Before the fix the refusal was
// rendered (`reclaimSkipDirtyReason`) and forgotten.
import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

func TestZZRedProof_DirtyDeadHolderWorkIsOrphaned(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	t.Setenv(config.GopiDirEnv, t.TempDir())
	t.Setenv(scratch.HomeEnvName, t.TempDir())
	if err := os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-red", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeadHolderRow(t, "t-red", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))
	// The merger's own pass over this repo (the detection+dispatch path): the
	// reclaim arm REFUSES this directory (dirty) — that refusal is correct.
	// The question is whether anything CARRIES the work.
	MergerRepoSweep(repo, 0)
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE summary LIKE '%salvage%'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatalf("ORPHANED WORK: the merger refused the dirty dead-holder worktree of %s "+
			"(holder provably dead, branch %s) and filed NO task for its uncommitted work — "+
			"its only copy is that directory", branch, branch)
	}
}
