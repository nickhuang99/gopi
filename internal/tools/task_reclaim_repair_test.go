package tools

// Regression tests for the SECOND round of the dead-holder reclaim fix
// (2026-09-12): the committed repair was correct in its disposition but ran
// `git worktree prune` PER ROW. Two defects followed, both invisible in the
// one-vanished-row fixture the first round used:
//
//	attribution — `git worktree prune` is REPO-WIDE (it takes no path). With
//	  two vanished rows in ONE repo and only the SECOND still registered, the
//	  FIRST row's prune call drops the second's registration, so the first row
//	  claimed a repair it did not make and the second stayed silent.
//	N+1 — a row whose registration git already dropped has nothing to repair,
//	  yet the old code pruned once per such row per pass: with the real ledger
//	  (137 vanished rows) that is 137 subprocesses per lifecycle pass, forever.
//
// Both tests fail on the pre-fix code and pass after it. They are deliberately
// small: the prunes are COUNTED through the reclaimPruneCall seam rather than
// inferred from output, and the branches are proven untouched by rev-parse.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// reclaimPruneCounter installs a counting (and still real) prune seam for one
// test, returning the counter. The wrapper delegates to the production repair,
// so the assertions stay about REAL git behaviour, not a stub's.
func reclaimPruneCounter(t *testing.T) *int {
	t.Helper()
	prev := reclaimPruneCall
	count := 0
	reclaimPruneCall = func(repo string) (string, error) {
		count++
		return reclaimRepairRegistration(repo)
	}
	t.Cleanup(func() { reclaimPruneCall = prev })
	return &count
}

// removedWorktreeFixture builds a SECOND linked worktree in the same repo as
// deadHolderReclaimFixture and returns repo, its path and branch.
func removedWorktreeFixture(t *testing.T, repo string) (path, branch string) {
	t.Helper()
	path = filepath.Join(filepath.Dir(repo), "wt-b")
	branch = "feat/dead-holder-b"
	gitRun(t, repo, "checkout", "-b", branch)
	gitRun(t, repo, "checkout", "master")
	gitRun(t, repo, finishCleanupWorktreeVerb, "add", path, branch)
	return path, branch
}

// TestDeadHolderReclaimCreditsOnlyTheRowItRepaired is the attribution defect:
// TWO vanished rows in ONE repo, only the SECOND still registered. The pass
// must repair (and CREDIT) exactly the row git dropped — the first row's
// registration was already gone, so it has nothing to claim.
func TestDeadHolderReclaimCreditsOnlyTheRowItRepaired(t *testing.T) {
	repo, wtA, branchA := deadHolderReclaimFixture(t)
	wtB, branchB := removedWorktreeFixture(t, repo)

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-cred-a", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-cred-b", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	dead := deadPID(t)
	seedDeadHolderRow(t, "t-cred-a", taskStatusCompleted, repo, branchA, wtA, hostPid(t, dead))
	seedDeadHolderRow(t, "t-cred-b", taskStatusCompleted, repo, branchB, wtB, hostPid(t, dead))

	// Row A's registration is dropped first (git takes the directory with it),
	// then both directories vanish — the incident's shape, plus a live
	// registration that is NOT row A's.
	gitRun(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, "--force", wtA)
	if err := os.RemoveAll(wtA); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(wtB); err != nil {
		t.Fatal(err)
	}
	if worktreeListed(t, repo, wtA) {
		t.Fatalf("precondition: row A's registration must already be gone: %s", worktreeList(t, repo))
	}
	if !worktreeListed(t, repo, wtB) {
		t.Fatalf("precondition: row B's registration must still be listed: %s", worktreeList(t, repo))
	}

	count := reclaimPruneCounter(t)
	got := ReclaimDeadHolderWorktrees(true)
	joined := strings.Join(got, "\n")

	if !strings.Contains(joined, "t-cred-b") || !strings.Contains(joined, reclaimRepairedMarker) {
		t.Fatalf("the row whose registration git dropped must be credited with the repair:\n%s", joined)
	}
	if strings.Contains(joined, "t-cred-a") {
		t.Errorf("row A's registration was already gone — this pass repaired nothing for it, yet it is reported:\n%s", joined)
	}
	if *count != 1 {
		t.Errorf("a repo-wide prune must run at most once per repo per pass, ran %d time(s)", *count)
	}
	if lit := worktreeList(t, repo); strings.Contains(lit, branchB) {
		t.Errorf("gate [c]'s list must no longer name %s: %s", branchB, lit)
	}
	for _, b := range []string{branchA, branchB} {
		if err := gitBranchExists(t, repo, b); err != nil {
			t.Errorf("the repair must never touch the branch %s: %v", b, err)
		}
	}
}

// TestDeadHolderReclaimDoesNotPruneForAlreadyCleanRows is the N+1 defect: a
// pass whose vanished rows all have CLEAN registrations has no repair to make,
// so it must run ZERO prunes and print nothing — the real ledger holds ~137
// such rows, and the old code spawned one subprocess per row per pass.
func TestDeadHolderReclaimDoesNotPruneForAlreadyCleanRows(t *testing.T) {
	repo, wtA, branchA := deadHolderReclaimFixture(t)
	wtB, branchB := removedWorktreeFixture(t, repo)

	for id := range map[string]bool{"t-clean-a": true, "t-clean-b": true} {
		if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: id, Source: "human"}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	dead := deadPID(t)
	seedDeadHolderRow(t, "t-clean-a", taskStatusCompleted, repo, branchA, wtA, hostPid(t, dead))
	seedDeadHolderRow(t, "t-clean-b", taskStatusCompleted, repo, branchB, wtB, hostPid(t, dead))

	// Both registrations are gone, both directories are gone: nothing is left
	// to repair, in either row.
	gitRun(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, "--force", wtA)
	gitRun(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, "--force", wtB)
	for _, wt := range []string{wtA, wtB} {
		if err := os.RemoveAll(wt); err != nil {
			t.Fatal(err)
		}
	}

	count := reclaimPruneCounter(t)
	got := ReclaimDeadHolderWorktrees(true)
	if len(got) != 0 {
		t.Fatalf("two already-clean rows have nothing to report, got %v", got)
	}
	if *count != 0 {
		t.Errorf("no row was registered — a prune would be a subprocess for nothing, ran %d time(s)", *count)
	}
	// Convergence, asserted the same way for the second pass: still zero.
	if again := ReclaimDeadHolderWorktrees(true); len(again) != 0 {
		t.Errorf("the second pass must be silent, got %v", again)
	}
	if *count != 0 {
		t.Errorf("the second pass must still not prune, ran %d time(s)", *count)
	}
}

// TestDeadHolderReclaimReportOnlyTouchesNothing is the third half of the same
// defect: the repair used to run BEFORE the report-only checks, so a DRY RUN
// (remove=false) and the kill switch (GOPI_DEAD_HOLDER_RECLAIM=0) both pruned
// the repo's registrations while promising to touch nothing. The registration
// is left exactly as it was; the row is NAMED so the operator still sees it.
func TestDeadHolderReclaimReportOnlyTouchesNothing(t *testing.T) {
	for _, tc := range []struct {
		name       string
		remove     bool
		killSwitch bool
	}{
		{name: "dry run", remove: false},
		{name: "kill switch off (removal on)", remove: true, killSwitch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, wt, branch := deadHolderReclaimFixture(t)
			if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-report-only", Source: "human"}); err != nil {
				t.Fatalf("submit: %v", err)
			}
			seedDeadHolderRow(t, "t-report-only", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))
			if err := os.RemoveAll(wt); err != nil {
				t.Fatal(err)
			}
			if tc.killSwitch {
				t.Setenv(ReclaimDeadHolderEnv, "0")
			}
			count := reclaimPruneCounter(t)

			got := ReclaimDeadHolderWorktrees(tc.remove)
			if !strings.Contains(strings.Join(got, "\n"), branch) {
				t.Fatalf("report-only must still NAME the branch it would repair, got %v", got)
			}
			if *count != 0 {
				t.Errorf("report-only must not run a prune at all, ran %d time(s)", *count)
			}
			if !worktreeListed(t, repo, wt) {
				t.Errorf("report-only must leave the registration untouched (gate [c] still sees it): %s", worktreeList(t, repo))
			}
		})
	}
}

// worktreeListed reports whether repo's worktree list names this exact path —
// a SUBSTRING test would be fooled by a sibling whose name starts the same
// (…/wt vs …/wt-b), which is the whole point of comparing parsed paths.
func worktreeListed(t *testing.T, repo, path string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree list --porcelain: %v: %s", err, out)
	}
	for _, got := range reclaimPlanParsePaths(string(out)) {
		if got == path {
			return true
		}
	}
	return false
}

// gitBranchExists fails when a branch git should still hold cannot be resolved.
func gitBranchExists(t *testing.T, repo, branch string) error {
	t.Helper()
	return exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", branch).Run()
}
