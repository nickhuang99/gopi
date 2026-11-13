package tools

// The CREATION seam's ONE-OWNER rule — the fix for the 2026-10-03 borrowed-ground
// leak (epic: non-worker `gopi <verb>` auto rows).
//
// The seam under test is the ROW-NAMING one (TaskDeliverableWorktreeBashForRow).
// That is not a convenience: it is the exact call internal/agent's executor loop
// makes after every bash round (loop.go, `tools.TaskDeliverableWorktreeBashForRow
// (a.autoTaskRowID, workdir, command)`) — the env-keyed wrapper seam
// (TaskDeliverableWorktreeBash) cannot see an auto row at all, which is the leak
// this epic is about.
//
// WHAT WENT WRONG, MEASURED. A bare `gopi replay` run (run_method=auto, pid
// 611399) stood in the worktree of task 01a10016 and the creation seam stamped
// that worktree AND its branch onto the replay run's own row 01a10025 — where
// 01a10016's row already named them. Two rows owned one directory, ownership
// lied in BOTH directions (each blocks the other's recycle), and the borrower's
// row was never dispatchable. The probe (taskGroundOwningDir) answers "which
// ground does this directory belong to"; it never asked "does another row
// already own it", which is the half the CLAIM seam has had since the
// 2026-09-12 foreign-owner hijack.
//
// Each case below maps to one acceptance claim:
//
//	(a) TestCreationSeamRefusesABorrowedWorktree — a run standing in another
//	    row's worktree records NOTHING, and says so (Rule 15).
//	(b) TestCreationSeamRefusesABorrowedBranch — the branch axis of the same
//	    rule, on the main checkout (no worktree to hijack).
//	(c) TestCreationSeamStillRecordsAnUnownedGround — the positive control:
//	    the guard refuses a BORROWED ground, never the row's own.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreationSeamRefusesABorrowedWorktree(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)

	// The OWNER: the row that legitimately created f.wtDir (the shape every
	// worker row has once its creation round ran).
	owner := "t-borrow-owner"
	deliverableWTSeedWrapperRow(t, db, owner, f.clientDir)
	if err := taskBackfillDeliverableWorktree(owner, f.wtDir, f.featBranch); err != nil {
		t.Fatalf("seed the owner's ground: %v", err)
	}

	// The BORROWER: a second row whose run merely STANDS in that worktree.
	borrower := "t-borrow-run"
	deliverableWTSeedWrapperRow(t, db, borrower, f.clientDir)
	before := deliverableWTRowBlob(t, db, borrower)

	out := captureStderrDuring(t, func() {
		TaskDeliverableWorktreeBashForRow(borrower, f.wtDir, "git -C "+f.wtDir+" status")
	})

	if after := deliverableWTRowBlob(t, db, borrower); after != before {
		t.Fatalf("a run standing in another row's worktree must not record it as its ground:\nbefore %s\nafter  %s", before, after)
	}
	prefix := strings.Split(fmt.Sprintf(taskWorktreeBorrowedGroundLineFmt, borrower, "X"), "X")[0]
	if !strings.Contains(out, prefix) {
		t.Fatalf("the refusal must be REPORTED, not silent (Rule 15): stderr=%q", out)
	}
	if !strings.Contains(out, owner) {
		t.Fatalf("the refusal must name the owning row %s so the reader can follow it: stderr=%q", owner, out)
	}
}

func TestCreationSeamRefusesABorrowedBranch(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)

	// The BRANCH axis: the run works in the repo's MAIN checkout (no worktree
	// exists to hijack), on a non-exempt branch another row already names.
	const branch = "feat/borrow-branch"
	deliverableWTGit(t, f.clientDir, "checkout", "-b", branch)

	owner := "t-borrow-branch-owner"
	deliverableWTSeedWrapperRow(t, db, owner, f.clientDir)
	if _, err := taskBackfillDeliverableWorktreeAt(owner, "", branch, &f.clientDir, taskWorktreeStampMomentClaim); err != nil {
		t.Fatalf("seed the branch owner: %v", err)
	}

	borrower := "t-borrow-branch-run"
	deliverableWTSeedWrapperRow(t, db, borrower, f.clientDir)
	before := deliverableWTRowBlob(t, db, borrower)

	out := captureStderrDuring(t, func() {
		TaskDeliverableWorktreeBashForRow(borrower, f.clientDir, "cd "+f.clientDir+" && git commit --allow-empty -m borrow")
	})

	if after := deliverableWTRowBlob(t, db, borrower); after != before {
		t.Fatalf("a branch another row names must not be recorded as this run's ground:\nbefore %s\nafter  %s", before, after)
	}
	if !strings.Contains(out, owner) {
		t.Fatalf("the refusal must name the owning row %s: stderr=%q", owner, out)
	}
}

func TestCreationSeamStillRecordsAnUnownedGround(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)

	// Positive control: NO row names this worktree, so it IS this run's own
	// ground and the seam must record it exactly as before the guard existed.
	// Without this arm the fix could pass by refusing everything.
	taskID := "t-borrow-control"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)

	TaskDeliverableWorktreeBashForRow(taskID, f.wtDir, "git -C "+f.wtDir+" status")

	if wt, branch := TaskDeliverableWorktreeFor(taskID); wt != f.wtDir || branch != f.featBranch {
		t.Fatalf("an UNOWNED ground must still be recorded: worktree=%q branch=%q, want %q %q", wt, branch, f.wtDir, f.featBranch)
	}
}
