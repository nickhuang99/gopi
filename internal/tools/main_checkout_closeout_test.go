package tools

// main_checkout_closeout_test.go — the WORKER half of the epic "gopi merge --fast
// work in master": the close-out restore.
//
// The property under test is the one the epic asks the worker to guarantee — a run
// that worked in the MAIN checkout must leave that checkout on the branch it found
// there, so the next landing (another worker's, or a `gopi merge --fast`) does not
// fast-forward the wrong ref. The contrasting case is the linked worktree: a run
// whose ground is its OWN worktree cannot have moved the main checkout, and the
// restore must not go reaching into the repo on its account (Rule 29).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mainCheckoutCloseoutFixture builds a repo whose main checkout is parked on the
// run's branch — the exact state the close-out must repair. It returns
// (repo, branch).
func mainCheckoutCloseoutFixture(t *testing.T) (repo, branch string) {
	t.Helper()
	repo = t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", masterBranchName)
	gitRun(t, repo, "config", "user.email", "test@example.invalid")
	gitRun(t, repo, "config", "user.name", "test")
	writeRepoFile(t, repo, "f.txt", "base\n")
	gitRun(t, repo, "add", "f.txt")
	gitRun(t, repo, "commit", "-q", "-m", "base")

	branch = "feat/parked-by-a-run"
	gitRun(t, repo, "checkout", "-q", "-b", branch)
	writeRepoFile(t, repo, "work.txt", "the run's deliverable\n")
	gitRun(t, repo, "add", "work.txt")
	gitRun(t, repo, "commit", "-q", "-m", "the run's work")
	return repo, branch
}

// TestFinishCleanupRestoreMainCheckoutRepairsAParkedMainCheckout is the
// requirement itself: a row whose deliverable repo is the main checkout, parked on
// the run's branch, is put back on the integration branch at close-out — and the
// step SAYS so.
func TestFinishCleanupRestoreMainCheckoutRepairsAParkedMainCheckout(t *testing.T) {
	repo, branch := mainCheckoutCloseoutFixture(t)

	ok, detail := FinishCleanupRestoreMainCheckout(context.Background(), repo, repo, branch)
	if !ok {
		t.Fatalf("the close-out failed to restore the main checkout: %s", detail)
	}
	if !strings.Contains(detail, MainCheckoutRestoredMarker) {
		t.Errorf("the repair is not reported: %q", detail)
	}
	if !strings.Contains(detail, branch) {
		t.Errorf("the report does not name the branch the checkout came from: %q", detail)
	}
	if got := headBranch(t, repo); got != masterBranchName {
		t.Errorf("the main checkout is on %q, want %s — the run left it parked", got, masterBranchName)
	}
	// The run's work is untouched: the branch ref keeps its tip and the tree is
	// clean, so the deliverable survives the repair.
	if tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", branch)); tip == "" {
		t.Errorf("the run's branch no longer resolves — the repair destroyed the deliverable")
	}
	if got := strings.TrimSpace(gitOut(t, repo, "status", "--porcelain")); got != "" {
		t.Errorf("the repair left the tree dirty:\n%s", got)
	}
}

// TestFinishCleanupRestoreMainCheckoutIsSilentOnTheOrdinaryState pins the no-op:
// a checkout already on the integration branch is not reported, because a report
// that fires on every close-out is a report nobody reads.
func TestFinishCleanupRestoreMainCheckoutIsSilentOnTheOrdinaryState(t *testing.T) {
	repo, branch := mainCheckoutCloseoutFixture(t)
	gitRun(t, repo, "checkout", "-q", masterBranchName)

	ok, detail := FinishCleanupRestoreMainCheckout(context.Background(), repo, repo, branch)
	if !ok || detail != "" {
		t.Fatalf("ok=%v detail=%q, want the silent no-op on a checkout already on %s", ok, detail, masterBranchName)
	}
}

// TestFinishCleanupRestoreMainCheckoutLeavesALinkedWorktreeRunAlone is the Rule 29
// arm: a run whose ground is its OWN linked worktree cannot have moved the main
// checkout, and this arm must not touch the repo on its account. The main checkout
// is deliberately parked on ANOTHER branch first, so the assertion is not vacuous:
// if the restore fired, it would move it.
func TestFinishCleanupRestoreMainCheckoutLeavesALinkedWorktreeRunAlone(t *testing.T) {
	repo, branch := mainCheckoutCloseoutFixture(t)
	other := "feat/somebody-elses-parking"
	gitRun(t, repo, "checkout", "-q", "-b", other, masterBranchName)
	wtRoot := t.TempDir()
	wt := filepath.Join(wtRoot, "run-worktree")
	gitRun(t, repo, "worktree", "add", "-q", wt, branch)

	ok, detail := FinishCleanupRestoreMainCheckout(context.Background(), repo, wt, branch)
	if !ok {
		t.Fatalf("the close-out refused a linked-worktree row: %s", detail)
	}
	if detail != "" {
		t.Errorf("the close-out reported acting on a linked-worktree row: %q", detail)
	}
	if got := headBranch(t, repo); got != other {
		t.Errorf("the main checkout moved to %q — a linked-worktree run owns no part of it (Rule 29)", got)
	}
	if got := headBranch(t, wt); got != branch {
		t.Errorf("the run's own worktree moved to %q", got)
	}
}

// TestFinishCleanupRestoreMainCheckoutRefusesUncommittedWork is the honesty arm at
// the close-out boundary: the repair is not a `checkout --force`. With uncommitted
// tracked work in the parked checkout, the call REFUSES (ok=false) and says why,
// and the caller's report carries that — a close-out that silently left the repo
// parked would be exactly the defect being fixed, one layer up.
func TestFinishCleanupRestoreMainCheckoutRefusesUncommittedWork(t *testing.T) {
	repo, branch := mainCheckoutCloseoutFixture(t)
	writeRepoFile(t, repo, "f.txt", "uncommitted, tracked\n")

	ok, detail := FinishCleanupRestoreMainCheckout(context.Background(), repo, repo, branch)
	if ok {
		t.Fatalf("the close-out moved a checkout holding uncommitted tracked work: %q", detail)
	}
	if !strings.Contains(detail, MainCheckoutParkedMarker) || !strings.Contains(detail, "f.txt") {
		t.Errorf("the refusal does not name the parked branch and the uncommitted file: %q", detail)
	}
	if got := headBranch(t, repo); got != branch {
		t.Errorf("the checkout moved to %q anyway — a refusal leaves it exactly as it was", got)
	}
	if body, err := os.ReadFile(filepath.Join(repo, "f.txt")); err != nil || string(body) != "uncommitted, tracked\n" {
		t.Errorf("the refusal touched the uncommitted file: body=%q err=%v", body, err)
	}
}

// TestFinishCleanupPlanPredictsTheMainCheckoutRestore binds the dry run to the
// apply for this step: a parked checkout is PREDICTED (marker-prefixed, nothing
// moved), because a plan that omitted the step would understate what the apply
// does — the same "plan can never disagree with the run" contract the chain's
// other steps carry.
func TestFinishCleanupPlanPredictsTheMainCheckoutRestore(t *testing.T) {
	repo, _ := mainCheckoutCloseoutFixture(t)

	step := finishCleanupMainCheckoutPlannedStep(context.Background(), repo)
	if step == nil {
		t.Fatalf("the plan predicted nothing for a checkout parked on a branch")
	}
	if step.Step != finishCleanupStepMainCheckout {
		t.Errorf("the predicted step is named %q, want %q", step.Step, finishCleanupStepMainCheckout)
	}
	if !strings.Contains(step.Detail, finishCleanupPlanMarker) {
		t.Errorf("the prediction is not marked as one: %q", step.Detail)
	}
	// PLAN MEANS PLAN: the read that made the prediction moved nothing.
	if got := headBranch(t, repo); got != "feat/parked-by-a-run" {
		t.Errorf("the PLAN moved the checkout to %q — a dry run must act on nothing", got)
	}
}
