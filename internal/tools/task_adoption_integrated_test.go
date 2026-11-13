// Regression guard: the adoption chain must never render an
// ALREADY-INTEGRATED candidate as a failed adoption, and its failure entries
// must carry git's own cause.
//
// THE DEFECT, MEASURED (operator screenshot, 2026-09-14 — the merger's report
// on this repo):
//
//	[serve] [adopt] scanned=11 adopted=0 ref-kept=0 dispatched=0 duped=0 skipped=9 failed=2
//	[serve] [adopt] fix/ledger-fixture-leak: commit: git commit --no-verify -m adoption: merge fix/ledger-fixture-leak (b9ae5cbc): exit status 1:
//	[serve] [adopt] fix/worker-ownership-pointer-master-red: commit: git commit --no-verify -m adoption: merge fix/worker-ownership-pointer-master-red (67445327): exit status 1:
//
// Both branches were ANCESTORS of master (verified at HEAD fc6a9373), so git's
// probe answered "Already up to date." and staged nothing; `git commit` then
// failed with its explanation on STDOUT ("nothing added to commit but
// untracked files present"), which adoptionGit discarded (stderr-only error
// rendering). The operator read a bare `exit status 1: ` tail and a `failed=2`
// counter for two healthy landings — and the tail was empty in EVERY such pass,
// forever, because the branch ref is never consumed.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// adoptionIntegratedFixture builds a repo whose branch's tip is ALREADY
// CONTAINED in master: the branch is created, a commit is made on it, master
// advances THROUGH it (a merge), and the branch ref is left behind — the exact
// leftover shape the operator's report showed. Returns the repo path.
func adoptionIntegratedFixture(t *testing.T, branch string) string {
	t.Helper()
	repo := adoptionTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "landed.txt"), []byte("landed work\n"), 0o644); err != nil {
		t.Fatalf("write landed file: %v", err)
	}
	if _, err := adoptionGit(repo, "add", "landed.txt"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := adoptionGit(repo, "commit", "-m", "landed work"); err != nil {
		t.Fatalf("commit on master: %v", err)
	}
	if _, err := adoptionGit(repo, "branch", branch); err != nil {
		t.Fatalf("branch %s: %v", branch, err)
	}
	return repo
}

// TestAdoptionAlreadyIntegratedIsNotAFailure is the fix contract: a candidate
// whose tip master already contains is reported as already-integrated (with
// the prune command), NEVER through Failed, and the leftover ref is cleared so
// the report converges instead of repeating every serve pass.
func TestAdoptionAlreadyIntegratedIsNotAFailure(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const branch = "fix/already-landed"
	repo := adoptionIntegratedFixture(t, branch)
	adoptionSeedTask(t, branch)

	counts, err := AdoptionSweep(context.Background(), AdoptionOptions{Repo: repo, Submit: true})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	// The operator's symptom: a healthy landing wearing the error channel.
	if len(counts.Failed) != 0 {
		t.Fatalf("Failed = %v, want empty — the branch's work IS in master", counts.Failed)
	}
	if len(counts.Adopted) != 0 {
		t.Fatalf("Adopted = %v, want empty — nothing was merged, so nothing was adopted", counts.Adopted)
	}
	if len(counts.AlreadyIntegrated) != 1 {
		t.Fatalf("AlreadyIntegrated = %v, want one entry", counts.AlreadyIntegrated)
	}
	line := counts.AlreadyIntegrated[0]
	for _, want := range []string{branch, adoptionAlreadyIntegratedMarker, "master", adoptionPruneCommand(branch, false)} {
		if !strings.Contains(line, want) {
			t.Errorf("entry %q must carry %q (an operator reaches the remedy from the line alone)", line, want)
		}
	}
	if strings.Contains(line, "exit status 1") {
		t.Errorf("entry %q must not carry a git exit status — the cause is what belongs there", line)
	}
	// The remedy is the ACTUAL command for this branch, and it works: the
	// leftover ref is what keeps the entry repeating on every pass.
	if adoptionBranchExists(t, repo, branch) {
		t.Errorf("the leftover ref %s must be pruned by the pass (convergence), not left for a later one", branch)
	}
	// Second pass: nothing left to report (the defect repeated forever).
	counts2, err := AdoptionSweep(context.Background(), AdoptionOptions{Repo: repo, Submit: true})
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if counts2.Scanned != 0 || len(counts2.AlreadyIntegrated) != 0 || len(counts2.Failed) != 0 {
		t.Fatalf("second pass must be a clean no-op, got %+v", counts2)
	}
}

// TestAdoptionAlreadyIntegratedKeepsTheRefWhenGitRefuses: the prune is a
// best-effort WART. A ref git refuses to delete (here: checked out in a linked
// worktree) is reported through RefKept — never as a failure — and the entry
// still reaches the operator.
func TestAdoptionAlreadyIntegratedKeepsTheRefWhenGitRefuses(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const branch = "fix/landed-occupied"
	repo := adoptionIntegratedFixture(t, branch)
	adoptionSeedTask(t, branch)
	adoptionOccupiedWorktree(t, repo, branch)

	counts, err := AdoptionSweep(context.Background(), AdoptionOptions{Repo: repo, Submit: true})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(counts.Failed) != 0 {
		t.Fatalf("Failed = %v, want empty (a kept ref is not a failure)", counts.Failed)
	}
	if len(counts.AlreadyIntegrated) != 1 {
		t.Fatalf("AlreadyIntegrated = %v, want one entry (the operator must still see it)", counts.AlreadyIntegrated)
	}
	if len(counts.RefKept) != 1 || !strings.Contains(counts.RefKept[0], branch) {
		t.Fatalf("RefKept = %v, want one entry naming %s", counts.RefKept, branch)
	}
	if !adoptionBranchExists(t, repo, branch) {
		t.Fatalf("git refused the prune, so %s must still exist (no force)", branch)
	}
}

// TestAdoptionGitErrorCarriesGitCause is the second half of the same defect,
// at the seam that dropped it: adoptionGit must put git's EXPLANATION into the
// error even when git wrote it to stdout (the `git commit` with nothing staged
// shape), so no report entry can end in an empty ": " tail again.
func TestAdoptionGitErrorCarriesGitCause(t *testing.T) {
	repo := adoptionTestRepo(t)
	// Nothing staged, no merge in progress: git explains on STDOUT, and exits 1.
	_, err := adoptionGit(repo, "commit", "--no-verify", "-m", adoptionSummaryPrefix+"feat/nothing")
	if err == nil {
		t.Fatal("a commit with nothing staged must fail")
	}
	msg := err.Error()
	// The old rendering was "...: exit status 1: " — an empty tail, because
	// only stderr was consulted and git had used stdout.
	if strings.HasSuffix(strings.TrimSpace(msg), ":") {
		t.Fatalf("error ends in an empty detail tail — git's stdout cause was dropped: %q", msg)
	}
	if !strings.Contains(msg, "nothing") {
		t.Fatalf("error must carry git's own explanation (stdout), got %q", msg)
	}
}
