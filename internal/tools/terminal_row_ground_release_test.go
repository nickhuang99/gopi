package tools

// terminal_row_ground_release_test.go — a FINISHED row must not keep a ground
// just because the pid number in its claim stamp is alive.
//
// THE DEFECT THESE TESTS CLOSE (measured 2026-11-08, row 01a1b913):
//
//	$ grep -rh "already named by row" ~/.gopi/sessions/*/output.log \
//	    | sed 's/.*already named by row //' | sort | uniq -c | sort -rn | head
//	     39 01a1ab49-5b0a-7cf3-b9cb-2d7fe39eddb5
//	     34 01a1ad8c-b1e6-7df3-9e4d-bd6b09d78d26
//
// 192 of 1826 session logs carried the line
//
//	the clone workspace /home/user/workspace/<row> is NOT recorded as this
//	row's ground — branch master is already named by row 01a1ab49-…
//
// The naming row was `completed` — its life OVER — yet
// finishCleanupOwnerRowLive's terminal arm asked only `pidAlive(pid)` of its
// claim stamp, and on a host with a resident queue worker that pid NUMBER is
// reused across rows and alive almost always (measured: pid 867520 claimed two
// different rows 4.5h apart, alive for both). So the branch axis (`branch` ==
// "master" AND same repo) read as borrowed, taskCreationGroundBorrowed refused
// the stamp, and every later clone-channel row in that worker's tour lost its
// deliverable.branch/worktree: `gopi task show` answered ground (none). The
// commits were never at risk — the LEDGER FACT was.
//
// The fix keeps the terminal veto on the two facts this host can PROVE: the
// row's own recorded runner identity is live, or a live process is STANDING in
// the directory the row names. A live, unrelated claim pid plus an empty
// directory is history.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// terminalGroundSeedRow inserts a TERMINAL row naming a ground, with the claim
// stamp and the recorded runner identity the caller chooses — the two inputs
// finishCleanupOwnerRowLive's terminal arm reads.
func terminalGroundSeedRow(t *testing.T, taskID, status, repo, worktree, branch, claimedBy string, rec procIdentity) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	deliverable := `{"repo":"` + repo + `","branch":"` + branch + `","worktree":"` + worktree + `"}`
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, claimed_by,
		                    `+tasksProcPidColumnName+`, `+tasksProcStartColumnName+`, `+tasksProcCmdColumnName+`,
		                    created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'auto', ?, ?, ?, ?, ?, ?)`,
		taskID, "a FINISHED row whose claim stamp names a pid that is alive", status, deliverable,
		claimedBy, rec.Pid, rec.StartTicks, rec.Cmd, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed terminal row %s: %v", taskID, err)
	}
}

// TestTerminalRowWithALiveClaimPIDDoesNotHoldTheBranchAxis is the RED→GREEN arm
// and the brief's exact shape: a `completed` row names branch "master" of the
// repo, its claim stamp is a LIVE pid belonging to an unrelated process, and the
// directory it recorded is GONE (an earlier sweep collected it). A later
// clone-channel row must still record its own ground.
//
// Pre-fix this failed on the writeback assertion: the terminal arm returned
// pidAlive(claimPid) == true, so taskCreationGroundBorrowed answered "branch
// master is already named by row …" and the borrower's row stayed repo-only.
func TestTerminalRowWithALiveClaimPIDDoesNotHoldTheBranchAxis(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	src, workspace, groundBranch := cloneOwnerFixture(t)

	// A directory that does not exist: the shape of a row whose ground the
	// sweep already collected. Nothing can be standing in it.
	collected := filepath.Join(t.TempDir(), "collected-ground")
	owner := "01a1ab49-0000-7000-8000-0000003eddb5"
	// The claim stamp is THIS process's own host stamp — a measurably LIVE pid
	// that has nothing to do with the row (the resident worker's reused number).
	// The owner names the SAME BRANCH the borrower is about to record — the
	// production shape: the clone channel stands every ground on `master`, so
	// the branch axis is what the terminal row blocked (its directory was
	// already gone, and the directory axis could not match anyway).
	terminalGroundSeedRow(t, owner, taskStatusCompleted, src, collected, groundBranch,
		claimHolderOfPid(os.Getpid()), procIdentity{})

	// PRECONDITION: without the fix's second question this row reads as a
	// holder — that is exactly the defect, pinned here so the test cannot pass
	// by accident on a host where the pid happens to be gone.
	if _, pid, known := claimHolder(claimHolderOfPid(os.Getpid())); !known || !pidAlive(pid) {
		t.Fatalf("fixture: the claim stamp must name a live local pid")
	}

	borrower := "01a1b913-0000-7000-8000-00000028f1005"
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), borrower, src)
	before := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), borrower)

	out := captureStderrDuring(t, func() {
		if _, err := TaskRunGroundForClone(borrower, workspace); err != nil {
			t.Fatalf("the writer must not error on a free ground: %v", err)
		}
	})

	if after := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), borrower); after == before {
		t.Fatalf("a COMPLETED row's live-but-unrelated claim pid must not block the ground stamp:\nstderr=%q\nblob %s", out, after)
	}
	wt, br := TaskDeliverableWorktreeFor(borrower)
	if br != groundBranch {
		t.Fatalf("row %s branch = %q, want %q — the clone channel's ground must be recorded", borrower, br, groundBranch)
	}
	if !sameWorkingPath(wt, workspace) {
		t.Fatalf("row %s worktree = %q, want %q", borrower, wt, workspace)
	}
	if strings.Contains(out, owner) && strings.Contains(out, "already named by row") {
		t.Fatalf("the refusal must be GONE from the stderr of the stamp: %q", out)
	}
}

// TestTerminalRowStillHoldsWhenALiveProcessStandsInTheDirectory is the negative
// control that keeps the fix honest: the SAME completed row, the SAME live claim
// pid, but this time the recorded directory really exists and a live process is
// standing in it. The ground stays refused — a finished row's binding is history
// only while nothing occupies the directory.
//
// Without this arm the fix could pass by dropping the terminal veto altogether,
// which is the direction that frees a directory out from under a process.
func TestTerminalRowStillHoldsWhenALiveProcessStandsInTheDirectory(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	src, workspace, workspaceBranch := cloneOwnerFixture(t)

	occupied := filepath.Join(t.TempDir(), "still-occupied")
	if err := os.MkdirAll(occupied, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	holdDir(t, occupied) // a real child shell whose CWD is the directory

	owner := "01a1ab49-0000-7000-8000-00000000a11ce"
	terminalGroundSeedRow(t, owner, taskStatusCompleted, src, occupied, workspaceBranch,
		claimHolderOfPid(os.Getpid()), procIdentity{})

	if !finishCleanupOwnerRowLive(taskStatusCompleted, claimHolderOfPid(os.Getpid()),
		procIdentity{}, occupied) {
		t.Fatalf("a completed row whose directory a live process stands in must keep the ground")
	}

	borrower := "01a1b913-0000-7000-8000-00000000b0b0b"
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), borrower, src)

	// The branch axis is the one the owner names, so a refusal there is the
	// refusal this test is about.
	why := taskCreationGroundBorrowed(borrower, src, workspace, workspaceBranch)
	if !strings.Contains(why, owner) {
		t.Fatalf("an occupied directory's row must keep the branch axis, got %q", why)
	}
}

// TestTerminalRowStillHoldsWhenItsRecordedRunnerIsAlive pins the OTHER provable
// fact: a completed row that recorded its own runner identity, and that identity
// still resolves to a live process, is still a holder — the terminal arm asks
// the identity ladder BEFORE the claim stamp, because the claim stamp is the
// weakest channel there is (a reused number).
func TestTerminalRowStillHoldsWhenItsRecordedRunnerIsAlive(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	runner := startLiveRunner(t)
	gone := filepath.Join(t.TempDir(), "gone-ground")

	if !finishCleanupOwnerRowLive(taskStatusCompleted, claimHolderOfPid(deadPID(t)),
		runner.identity, gone) {
		t.Fatalf("a completed row whose recorded runner is still running holds its ground")
	}
	// And the same row with a dead runner and an empty directory does not.
	deadRec := procIdentity{}
	if finishCleanupOwnerRowLive(taskStatusCompleted, claimHolderOfPid(deadPID(t)),
		deadRec, gone) {
		t.Fatalf("a completed row with no live runner and an empty directory holds nothing")
	}
}

// TestTerminalForeignClaimStillHoldsTheGround pins the unmeasurable direction
// (Rule 15a): a terminal row whose claim stamp belongs to ANOTHER host cannot be
// judged here, so it keeps the ground exactly as before the fix — the release
// rests on a measurement, never on the absence of one.
func TestTerminalForeignClaimStillHoldsTheGround(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone-foreign")
	if !finishCleanupOwnerRowLive(taskStatusCompleted, "some-other-host:4242", procIdentity{}, gone) {
		t.Fatalf("a foreign claim stamp is unknowable here — never called released")
	}
}
