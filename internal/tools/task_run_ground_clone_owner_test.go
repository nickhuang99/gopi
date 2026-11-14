package tools

// task_run_ground_clone_owner_test.go — the CLONE channel's arm of the
// ONE-GROUND-ONE-OWNING-ROW rule.
//
// THE DEFECT THESE TESTS CLOSE (measured 2026-11-01, rows 01a19648-4fbe →
// 01a196bf-8cbb, evidence in the child run's own session log):
//
//	$ grep -o '"workdir":"[^"]*"' ~/.gopi/sessions/d55a93dc-.../session.jsonl
//	"workdir":"/home/user/workspace/01a19648-4fbe-72ea-b3e9-02b219aaa4bf"
//	$ head -2 ~/.gopi/sessions/d55a93dc-.../console.log | tail -1
//	[task-notify] task 01a196bf-8cbb-…: deliverable.worktree backfilled to
//	"/home/user/workspace/01a19648-4fbe-…" (branch "task/01a196484fbe…") at claim
//
// A READ-ONLY question-and-answer run (`gopi --prompt '… 第 1 到 6 行是谁写的…只回答结论，不要改文件'`)
// was started from the bash tool of a RUNNING RUN, so it inherited that run's
// GOPI_RUN_GROUND, adopted its workspace, and had its directory and branch
// stamped onto its own row. Its close-out then failed with
// `clone channel: delivery did not land` — on a dirty tree its neighbour was
// still editing, which it had never touched.
//
// THE MISSING ARM, and why it is this one: the WORKTREE creation seam
// (TaskDeliverableWorktreeBashForRow) and the CLAIM seam
// (taskWorktreeStampClaimer) both ask taskCreationGroundBorrowed before
// stamping. The clone channel's writer — TaskRunGroundForClone, the ONLY path
// that records a clone workspace — did not, so the rule had a hole exactly
// where a workspace is handed over rather than created.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cloneOwnerSeedRow inserts a row whose deliverable NAMES a ground, with a
// claim stamp this host can see is alive (the case the rule must refuse: a
// LIVE owner keeps its ground).
func cloneOwnerSeedRow(t *testing.T, taskID, repo, worktree, branch string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	deliverable := `{"repo":"` + repo + `","branch":"` + branch + `","worktree":"` + worktree + `"}`
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, claimed_by, created_at, updated_at)
		 VALUES (?, ?, 'in_progress', ?, 'auto', ?, ?, ?)`,
		taskID, "a live row that owns the ground", deliverable,
		claimHolderOfPid(os.Getpid()), nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed row %s: %v", taskID, err)
	}
}

// cloneOwnerFixture builds the two-repository shape the channel produces: a
// source repo and a CLONE of it standing on its own branch — the directory a
// run adopts.
func cloneOwnerFixture(t *testing.T) (src, workspace, branch string) {
	t.Helper()
	src = groundTestRepo(t, "clone-owner-src")
	root := t.TempDir()
	workspace = filepath.Join(root, "01a19648-0000-7000-8000-00000000f4be")
	groundTestGitT(t, root, "clone", "-q", src, workspace)
	branch = "task/01a196480000700080000000000000f4be"
	groundTestGitT(t, workspace, "checkout", "-q", "-b", branch)
	return src, workspace, branch
}

// TestCloneGroundWriterRefusesAnotherLiveRowsWorkspace is the RED→GREEN arm.
//
// Pre-fix this failed on the writeback assertion: TaskRunGroundForClone stamped
// the owner's directory AND branch onto the borrower's row, making two rows
// name one directory — the state that blocks both rows' recycle and makes the
// borrower's delivery leg publish from a tree it does not own.
func TestCloneGroundWriterRefusesAnotherLiveRowsWorkspace(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	src, workspace, branch := cloneOwnerFixture(t)

	owner := "01a19648-0000-7000-8000-00000000f4be"
	cloneOwnerSeedRow(t, owner, src, workspace, branch)

	borrower := "01a196bf-0000-7000-8000-000000008cbb"
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), borrower, src)
	before := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), borrower)

	out := captureStderrDuring(t, func() {
		if _, err := TaskRunGroundForClone(borrower, workspace); err != nil {
			t.Errorf("the writer must not error on a refused ground — the run keeps working where it stands: %v", err)
		}
	})

	if after := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), borrower); after != before {
		t.Fatalf("a clone workspace another LIVE row names must not be recorded as this row's ground:\nbefore %s\nafter  %s", before, after)
	}
	if wt, br := TaskDeliverableWorktreeFor(borrower); wt != "" || br != "" {
		t.Fatalf("row %s ground = (%q, %q), want neither axis recorded", borrower, wt, br)
	}
	// RULE 15: the refusal is REPORTED, and it names the row that owns it.
	if !strings.Contains(out, owner) {
		t.Fatalf("the refusal must name the owning row %s so a reader can follow it: stderr=%q", owner, out)
	}
}

// TestCloneGroundWriterRecordsAnUnownedWorkspace is the positive control: with
// no other row naming the directory, the seam records the ground EXACTLY as
// before the guard existed. Without this arm the fix could pass by refusing
// everything — which would leave every clone row with no ground at all.
func TestCloneGroundWriterRecordsAnUnownedWorkspace(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	src, workspace, branch := cloneOwnerFixture(t)

	taskID := "01a196c3-0000-7000-8000-000000996d5c"
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), taskID, src)

	res, err := TaskRunGroundForClone(taskID, workspace)
	if err != nil {
		t.Fatalf("TaskRunGroundForClone: %v", err)
	}
	if wt, br := TaskDeliverableWorktreeFor(taskID); wt != workspace || br != branch {
		t.Fatalf("row ground = (%q, %q), want the clone workspace %q on branch %q — an UNOWNED ground must still be recorded", wt, br, workspace, branch)
	}
	if !strings.Contains(res.Detail, workspace) {
		t.Fatalf("the resolution's Detail must name the recorded workspace: %q", res.Detail)
	}
}
