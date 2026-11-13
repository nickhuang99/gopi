package tools

// MergerRepoSweep unit tests (epic architecture-fix-v2, config-driven pure
// git sweep): real git in temp repos — the engine IS git orchestration.
// Each test builds a throwaway repo with master + feature branches and
// asserts the sweep lands unprotected work (clean merge), leaves active
// worktree-checked-out bonds untouched, REPORTS already-integrated branches
// without mutating a ref, and never consults a task ledger row for the merge
// decision.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- fixtures (real git) ---

// rsTestRepo initializes a temp git repo with a master commit.
func rsTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rsGit(t, dir, true, "init", "-q", "-b", "master")
	rsGit(t, dir, true, "config", "user.email", "t@t")
	rsGit(t, dir, true, "config", "user.name", "t")
	rsGit(t, dir, true, "commit", "--allow-empty", "-q", "-m", "seed")
	return dir
}

// rsGit runs git -C repo, failing the test on error. fatal=false tolerates a
// non-zero exit (callers that assert a conditional git result).
func rsGit(t *testing.T, repo string, fatal bool, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil && fatal {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// rsMakeBranch creates branch at master HEAD with one divergence commit,
// then returns to master (an unprotected feature branch ahead of master).
func rsMakeBranch(t *testing.T, repo, branch string) {
	t.Helper()
	rsGit(t, repo, true, "checkout", "-q", "-b", branch)
	rsGit(t, repo, true, "commit", "--allow-empty", "-q", "-m", branch+" work")
	rsGit(t, repo, true, "checkout", "-q", "master")
}

// rsBranches lists local branch short names.
func rsBranches(t *testing.T, repo string) []string {
	t.Helper()
	return strings.Fields(rsGit(t, repo, true, "for-each-ref", "--format=%(refname:short)", "refs/heads"))
}

// rsBranchExists is a predicate wrapper (no fatal).
func rsBranchExists(repo, branch string) bool {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// rsIsAncestor reports whether branch's tip is an ancestor of master.
func rsIsAncestor(repo, branch string) bool {
	err := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", branch, "master").Run()
	return err == nil
}

// --- tests ---

func TestMergerRepoSweep_MergesUnprotectedBranch(t *testing.T) {
	repo := rsTestRepo(t)
	rsMakeBranch(t, repo, "feat/clean")
	if !rsBranchExists(repo, "feat/clean") {
		t.Fatal("fixture: feat/clean must exist")
	}

	res := MergerRepoSweep(repo, 0)
	if res.Err != "" {
		t.Fatalf("sweep error: %s", res.Err)
	}
	if res.Merged != 1 {
		t.Fatalf("merged = %d, want 1 (clean unprotected branch landed)", res.Merged)
	}
	// CONVERTED (epic sweep-report-not-act, 2026-09-19): this asserted "the
	// merged branch should have been deleted (branch -d)". The landing still
	// happens; the DELETION is retired — the sweep mutates no ref, so the branch
	// survives the tick that landed it and is REPORTED on every later tick
	// (TestMergerRepoSweep_AlreadyMergedIsReportedNotPruned).
	if !rsBranchExists(repo, "feat/clean") {
		t.Fatal("the landed branch must SURVIVE the tick — the sweep reports, it never acts")
	}
}

func TestMergerRepoSweep_SkipsWorktreeProtectedBranch(t *testing.T) {
	repo := rsTestRepo(t)
	// Create an ahead-of-master feature branch, then check it out in a
	// linked worktree and commit work INSIDE that worktree — the exact
	// active-bond shape: a branch that is genuinely ahead of master and
	// checked out in a linked worktree.
	rsMakeBranch(t, repo, "feat/bond") // back on master after this
	wt := filepath.Join(t.TempDir(), "bond-wt")
	rsGit(t, repo, true, "worktree", "add", "-q", wt, "feat/bond")
	// commit real divergence inside the linked worktree (this path's HEAD =
	// feat/bond; commits land on feat/bond, master (main) stays put).
	rsWorktreeCommit(t, wt, "bond work")
	if rsIsAncestor(repo, "feat/bond") {
		t.Fatal("fixture: feat/bond must be ahead of master")
	}
	if !wtHoldsBranch(repo, "feat/bond") {
		t.Fatal("fixture: feat/bond must be checked out in the linked worktree (protected)")
	}

	res := MergerRepoSweep(repo, 0)
	if res.Err != "" {
		t.Fatalf("sweep error: %s", res.Err)
	}
	if res.Protected != 1 {
		t.Fatalf("protected = %d, want 1 (the worktree-checked-out bond)", res.Protected)
	}
	if res.Merged != 0 {
		t.Fatalf("merged = %d, want 0 (the bonded branch must NOT be merged)", res.Merged)
	}
	if rsIsAncestor(repo, "feat/bond") {
		t.Fatal("feat/bond must NOT have been merged (worktree = active bond work)")
	}
}

// rsWorktreeCommit commits an empty commit inside a linked worktree path
// (its checked-out branch advances).
func rsWorktreeCommit(t *testing.T, wt, msg string) {
	t.Helper()
	out, err := exec.Command("git", "-C", wt, "commit", "--allow-empty", "-q", "-m", msg).CombinedOutput()
	if err != nil {
		t.Fatalf("worktree commit: %v: %s", err, out)
	}
}

// wtHoldsBranch reports whether repo has a linked worktree currently
// checked out on branch.
func wtHoldsBranch(repo, branch string) bool {
	out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(strings.TrimPrefix(line, "branch ")) == "refs/heads/"+branch {
			return true
		}
	}
	return false
}

// TestMergerRepoSweep_AlreadyMergedIsReportedNotPruned is the CONVERTED form of
// "…IsNotReMergedAndIsTidied" (epic sweep-report-not-act, 2026-09-19): the
// already-integrated branch is still not a merge candidate, but the tick now
// REPORTS it (branch, tip, verdict, owner evidence) instead of pruning it.
func TestMergerRepoSweep_AlreadyMergedIsReportedNotPruned(t *testing.T) {
	repo := rsTestRepo(t)
	rsMakeBranch(t, repo, "feat/done")
	// Merge it into master by hand so its tip is now in master (already
	// integrated — the branch still exists as a leftover ref).
	out := rsGit(t, repo, false, "merge", "--no-ff", "-m", "merge feat/done", "feat/done")
	_ = out
	if !rsIsAncestor(repo, "feat/done") {
		t.Fatal("fixture: feat/done should be in master now")
	}

	res := MergerRepoSweep(repo, 0)
	if res.Err != "" {
		t.Fatalf("sweep error: %s", res.Err)
	}
	if res.Merged != 0 {
		t.Fatalf("merged = %d, want 0 (already-merged branch is not a merge candidate)", res.Merged)
	}
	if res.Reported < 1 {
		t.Fatalf("reported = %d, want >=1 (the already-integrated branch is REPORTED)", res.Reported)
	}
	if len(res.WouldPrune) != 1 || res.WouldPrune[0].Branch != "feat/done" {
		t.Fatalf("the report must name the branch, tip and verdict: %+v", res.WouldPrune)
	}
	if res.WouldPrune[0].Tip == "" || res.WouldPrune[0].Verdict == "" {
		t.Errorf("the report entry must carry a tip and a verdict: %+v", res.WouldPrune[0])
	}
	if !strings.Contains(res.WouldPrune[0].Detail, res.WouldPrune[0].Tip) {
		t.Errorf("the operator line must name the tip: %s", res.WouldPrune[0].Detail)
	}
	// The branch ref SURVIVES (the sweep mutates no ref).
	if !rsBranchExists(repo, "feat/done") {
		t.Fatal("already-integrated branch must SURVIVE the tick (the sweep reports, it never acts)")
	}
}

// --- merger self-resolve seam (epic merger-single-owner-architecture) ---

// rsMakeConflictBranch creates a REAL merge conflict: both branch and master
// edit the same line of shared.txt, so branch does not merge into master
// (unprotected, ahead-of-master, NOT worktree-checked-out either).
func rsMakeConflictBranch(t *testing.T, repo, branch string) {
	t.Helper()
	rsWriteFile(t, repo, "shared.txt", "branch version\n", "seed layout", false)
	rsGit(t, repo, true, "checkout", "-q", "-b", branch)
	rsWriteFile(t, repo, "shared.txt", "branch version 2\n", "branch edit", true)
	rsGit(t, repo, true, "checkout", "-q", "master")
	rsWriteFile(t, repo, "shared.txt", "master version\n", "master edit", true)
}

// rsWriteFile stages content at path on the CURRENT branch and commits it
// (real-content merges need actual files, not empty commits).
func rsWriteFile(t *testing.T, repo, path, content, msg string, commit bool) {
	t.Helper()
	blob, err := rsSetBlob(repo, content)
	if err != nil {
		t.Fatalf("write blob: %v", err)
	}
	rsGit(t, repo, true, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+path)
	if commit {
		rsGit(t, repo, true, "commit", "-q", "-m", msg)
	}
}

// rsSetBlob writes content to git's object store and returns the blob sha.
func rsSetBlob(repo, content string) (string, error) {
	cmd := exec.Command("git", "-C", repo, "hash-object", "-w", "--stdin")
	cmd.Stdin = strings.NewReader(content)
	blob, err := cmd.Output()
	return strings.TrimSpace(string(blob)), err
}
