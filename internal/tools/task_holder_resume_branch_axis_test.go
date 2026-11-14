package tools

// BRANCH-AXIS REGRESSION for the dead-holder RESUME arm (defect B, measured
// 2026-09-16).
//
// The defect: deadHolderResumeRows filtered `deliverable LIKE '%worktree%'`, so
// a row whose deliverable names only `branch` never entered the candidate set —
// the arm was structurally blind to every branch-only row, however dead its
// holder. The arm's own judging had a second half of the same defect: it dropped
// any row whose RECORDED directory differed from git's registered one, so even a
// loaded row could be discarded over a ledger path that is measured wrong on the
// live host (`feat/merger-status-next-tick`, row 01a0a9b5, records
// gopi-next-tick while git registers gopi-merger-next-tick).
//
// These tests are the red-proofs: every one of them FAILS on the pre-change
// loader/judge (no outcome is produced for the row at all). The rows are seeded
// WITHOUT a runner identity on purpose — the arm then answers "unreadable"
// (Rule 15: never a death proof, never a spawn), which keeps the assertions
// about the LOADING/JUDGING axis and starts no process.

import (
	"github.com/nickhuang99/gopi/internal/heavytest"
	"path/filepath"
	"testing"
)

// resumeArmRepo makes a temp git repo with one linked worktree on branch, the
// shape the arm must be able to see without a ledger-recorded path.
func resumeArmRepo(t *testing.T, branch string) (repo, worktree string) {
	t.Helper()
	repo = t.TempDir()
	dhE2EGit(t, repo, "init", "-q")
	dhE2EGit(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	worktree = filepath.Join(t.TempDir(), "wt")
	dhE2EGit(t, repo, "worktree", "add", worktree, "-b", branch)
	return repo, worktree
}

// seedLiveRow submits a row and moves it into a holder-bearing status.
func seedLiveRow(t *testing.T, taskID string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "resume arm fixture " + taskID, Payload: "fixture", TaskID: taskID, Source: "human",
	}); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
	if _, err := TaskUpdateReason(taskID, taskStatusInProgress, "", ""); err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	}
}

// clearRowRunnerIdentity removes the row's recorded runner triple, so the arm
// reaches its "no identity recorded" rung. The status transition above stamps
// the identity of the WRITING process (this test binary, which is alive), and
// these fixtures are about the loading/judging axis — not about liveness — so
// the identity is cleared deliberately. The probe then answers UNREADABLE
// (Rule 15: never a death proof), which starts no process.
func clearRowRunnerIdentity(t *testing.T, taskID string) {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET `+tasksProcPidColumnName+` = 0, `+
		tasksProcStartColumnName+` = 0, `+tasksProcCmdColumnName+` = '' WHERE task_id = ?`, taskID); err != nil {
		t.Fatalf("clear identity %s: %v", taskID, err)
	}
}

// TestDeadHolderResumeRowsLoadsABranchOnlyRow: the row naming ONLY a branch is a
// candidate — the needle the loader used to carry is what hid it.
func TestDeadHolderResumeRowsLoadsABranchOnlyRow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "t-resume-branch-only"
	seedLiveRow(t, taskID)
	seedDeliverableBranchOnly(t, taskID, t.TempDir(), "feat/resume-branch-only")
	clearRowRunnerIdentity(t, taskID)

	rows, err := deadHolderResumeRows()
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	for _, r := range rows {
		if r.TaskID == taskID {
			return // loaded: the branch axis reaches the arm
		}
	}
	t.Fatalf("a row naming only a branch must be a resume candidate (the old LIKE '%%worktree%%' needle dropped it)")
}

// TestDeadHolderResumeArmJudgesTheCheckoutGitRegisters: the checkout is git's
// statement, not the row's recorded path — a stale path in the ledger must not
// hide a checkout git registers (that is the direction which abandons an
// interrupted run).
func TestDeadHolderResumeArmJudgesTheCheckoutGitRegisters(t *testing.T) {
	const branch = "feat/resume-arm-git-path"
	repo, worktree := resumeArmRepo(t, branch)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "t-resume-arm-git-path"
	seedLiveRow(t, taskID)
	// The row records a path that is NOT the registered checkout (the measured
	// ledger shape of row 01a0a9b5).
	seedDeliverable(t, taskID, repo, branch, filepath.Join(t.TempDir(), "stale-ledger-path"))
	clearRowRunnerIdentity(t, taskID)

	var found bool
	for _, oc := range ResumeDeadHolderRuns(repo) {
		if oc.TaskID != taskID {
			continue
		}
		found = true
		if oc.Worktree != worktree {
			t.Errorf("the arm must judge the checkout GIT registers (%s), got %q", worktree, oc.Worktree)
		}
		if oc.Resumed {
			t.Errorf("a row with no recorded runner identity must never be resumed, got %+v", oc)
		}
	}
	if !found {
		t.Fatalf("a branch-only row whose checkout git registers must be judged (the old path-equality filter dropped it)")
	}
}

// TestDeadHolderResumeArmRefusesARowWithNoGround is the conservative half: a row
// with no checkout anywhere holds no ground, so there is no interrupted run in a
// tree to walk back into — the arm must stay silent about it (the classifier
// reports it as owner-record-only, where a ledger-only row is owed a decision).
func TestDeadHolderResumeArmRefusesARowWithNoGround(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	repo := t.TempDir()
	dhE2EGit(t, repo, "init", "-q")
	dhE2EGit(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "t-resume-no-ground"
	seedLiveRow(t, taskID)
	seedDeliverableBranchOnly(t, taskID, repo, "feat/resume-no-ground")
	clearRowRunnerIdentity(t, taskID)

	for _, oc := range ResumeDeadHolderRuns(repo) {
		if oc.TaskID == taskID {
			t.Errorf("a row with no registered checkout holds no ground and must produce no outcome, got %s", oc.Line())
		}
	}
}
