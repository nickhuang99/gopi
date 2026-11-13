package tools

// main_checkout_restore_test.go — the gate for the main-checkout restore
// (epic: `gopi merge --fast` work in master).
//
// WHY THIS TEST EXISTS, in the epic's own words: the landing every merge path
// performs is `git merge --ff-only <branch>` run in the repo's MAIN checkout, so
// it fast-forwards whatever branch that checkout holds. A worker that finished in
// the main checkout and left it parked therefore makes the next landing move the
// WRONG branch — and, when the parked branch already contains the candidate, git
// answers "Already up to date." with exit status 0 while the integration branch
// never moves. The `gopi merge --fast` arm reads that exit status as a landing and
// PRUNES the branch ref, so the work leaves the merge queue while the report says
// it arrived (reproduced by hand before this test landed; the command-level pin
// is cmd/agent TestMergeFastLandsIntoMasterWhenTheCheckoutIsParked).
//
// The fixture below is the SMALLEST one that separates the two states: master,
// one candidate branch, and a parked branch that CONTAINS the candidate (so a
// fast-forward against the parked branch is a NO-OP that reports success).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mainCheckoutFixture builds the three-branch repo the restore is judged against
// and returns (repo, candidate, parked).
func mainCheckoutFixture(t *testing.T) (repo, candidate, parked string) {
	t.Helper()
	repo = t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", masterBranchName)
	gitRun(t, repo, "config", "user.email", "test@example.invalid")
	gitRun(t, repo, "config", "user.name", "test")
	writeRepoFile(t, repo, "f.txt", "base\n")
	gitRun(t, repo, "add", "f.txt")
	gitRun(t, repo, "commit", "-q", "-m", "base")

	candidate = "feat/candidate"
	gitRun(t, repo, "checkout", "-q", "-b", candidate)
	writeRepoFile(t, repo, "candidate.txt", "candidate\n")
	gitRun(t, repo, "add", "candidate.txt")
	gitRun(t, repo, "commit", "-q", "-m", "candidate work")

	// The parked branch CONTAINS the candidate: this is what makes
	// `merge --ff-only feat/candidate` a silent success when the checkout is
	// parked on it.
	parked = "feat/parked"
	gitRun(t, repo, "checkout", "-q", "-b", parked, candidate)
	writeRepoFile(t, repo, "parked.txt", "parked\n")
	gitRun(t, repo, "add", "parked.txt")
	gitRun(t, repo, "commit", "-q", "-m", "parked work on top")

	gitRun(t, repo, "checkout", "-q", masterBranchName)
	return repo, candidate, parked
}

func writeRepoFile(t *testing.T, repo, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// headBranch reads the branch a directory has checked out.
func headBranch(t *testing.T, dir string) string {
	t.Helper()
	out, err := finishCleanupGitRaw(context.Background(), dir,
		finishCleanupPreMergeRevParseVerb, finishCleanupPreMergeAbbrevRefFlag, finishCleanupPreMergeHeadArg)
	if err != nil {
		t.Fatalf("read HEAD in %s: %v", dir, err)
	}
	return strings.TrimSpace(out)
}

// TestMainCheckoutRestoreMovesAParkedCheckoutBack is the repair's positive arm:
// the checkout is parked on another branch, the tree is clean, and the call puts
// it back on the integration branch and SAYS so.
func TestMainCheckoutRestoreMovesAParkedCheckoutBack(t *testing.T) {
	repo, _, parked := mainCheckoutFixture(t)
	gitRun(t, repo, "checkout", "-q", parked)

	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), repo, IntegrationBranchForRepo(repo))
	if !ok {
		t.Fatalf("the restore refused a clean parked checkout: %s", detail)
	}
	if !strings.Contains(detail, MainCheckoutRestoredMarker) {
		t.Errorf("the repair is not reported: %q", detail)
	}
	if !strings.Contains(detail, parked) {
		t.Errorf("the repair does not name the branch the checkout came FROM: %q", detail)
	}
	if got := headBranch(t, repo); got != masterBranchName {
		t.Errorf("the checkout is on %q, want %s", got, masterBranchName)
	}
	// The move moves no COMMIT: the parked branch keeps its own tip.
	if tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", parked)); tip == "" {
		t.Errorf("the parked branch's ref no longer resolves")
	}
}

// TestMainCheckoutRestoreIsSilentWhenAlreadyOnTheIntegrationBranch is the
// no-op arm: the ordinary state repairs nothing, and a permanent "restored" line
// would train a reader to ignore the one that matters.
func TestMainCheckoutRestoreIsSilentWhenAlreadyOnTheIntegrationBranch(t *testing.T) {
	repo, _, _ := mainCheckoutFixture(t)
	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), repo, IntegrationBranchForRepo(repo))
	if !ok || detail != "" {
		t.Fatalf("ok=%v detail=%q, want ok=true with an EMPTY detail on a checkout already on %s", ok, detail, masterBranchName)
	}
}

// TestMainCheckoutRestoreRefusesUncommittedTrackedWork pins the arm that must
// never be a `checkout --force` in disguise: ONE uncommitted tracked change is
// enough to refuse, the refusal names the file, and the repo is left untouched.
func TestMainCheckoutRestoreRefusesUncommittedTrackedWork(t *testing.T) {
	repo, _, parked := mainCheckoutFixture(t)
	gitRun(t, repo, "checkout", "-q", parked)
	// TRACKED (f.txt exists in the branch) and uncommitted.
	writeRepoFile(t, repo, "f.txt", "somebody's uncommitted edit\n")

	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), repo, IntegrationBranchForRepo(repo))
	if ok {
		t.Fatalf("the restore moved a checkout holding uncommitted tracked work: %q", detail)
	}
	if !strings.Contains(detail, MainCheckoutParkedMarker) || !strings.Contains(detail, MainCheckoutDirtyMarker) {
		t.Errorf("the refusal does not name the parked branch AND the dirty tree: %q", detail)
	}
	if !strings.Contains(detail, "f.txt") {
		t.Errorf("the refusal does not name the uncommitted file: %q", detail)
	}
	if got := headBranch(t, repo); got != parked {
		t.Errorf("the refused checkout moved to %q anyway — a refusal leaves the repo as it was", got)
	}
	if body, err := os.ReadFile(filepath.Join(repo, "f.txt")); err != nil || string(body) != "somebody's uncommitted edit\n" {
		t.Errorf("the refusal touched the uncommitted file: body=%q err=%v", body, err)
	}
}

// TestMainCheckoutRestoreRefusesADetachedHead is the arm a `checkout` cannot
// rescue: a fast-forward in a detached HEAD moves no branch, so the restore
// refuses rather than pretending the repo is usable for a landing.
func TestMainCheckoutRestoreRefusesADetachedHead(t *testing.T) {
	repo, _, _ := mainCheckoutFixture(t)
	gitRun(t, repo, "checkout", "-q", "--detach")

	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), repo, IntegrationBranchForRepo(repo))
	if ok {
		t.Fatalf("the restore accepted a detached HEAD: %q", detail)
	}
	if !strings.Contains(detail, MainCheckoutDetachedMarker) {
		t.Errorf("the refusal does not name the detached HEAD: %q", detail)
	}
}

// TestMainCheckoutRestoreRefusesAWorktreeGround is the Rule 29 arm: handed a
// LINKED worktree belonging to another task, the restore resolves the repo's MAIN
// checkout and judges THAT — it never moves the peer's checkout out from under it.
func TestMainCheckoutRestoreRefusesAWorktreeGround(t *testing.T) {
	repo, candidate, _ := mainCheckoutFixture(t)
	root := t.TempDir()
	wt := filepath.Join(root, "peer-worktree")
	// A linked worktree holding the candidate branch — the shape a live peer has.
	gitRun(t, repo, "worktree", "add", "-q", wt, candidate)

	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), wt, IntegrationBranchForRepo(wt))
	// The MAIN checkout is on master, so the call is a no-op: it reports nothing
	// and (critically) the peer's worktree is left exactly where it was.
	if !ok || detail != "" {
		t.Fatalf("ok=%v detail=%q, want the main checkout to be judged (already on %s) from a linked ground", ok, detail, masterBranchName)
	}
	if got := headBranch(t, wt); got != candidate {
		t.Errorf("the linked worktree's checkout moved to %q — a peer's ground is never touched (Rule 29)", got)
	}
}

// TestMainCheckoutRestoreRepairsWhatTheSweepWouldOtherwiseMiss is the
// INTEGRATION property, and the reason the restore runs BEFORE the candidate
// discovery: a branch checked out in any registered worktree — the main checkout
// included, since git lists it first — is classified worktree-held and EXCLUDED
// from the candidate set. A parked main checkout therefore presents NO candidates
// at all, so a restore placed after the discovery would never fire on the very
// state it exists to repair. This test would pass vacuously with the wrong
// ordering, so it asserts the restore's OWN report, not just the merged count.
func TestMainCheckoutRestoreRepairsWhatTheSweepWouldOtherwiseMiss(t *testing.T) {
	repo, candidate, parked := mainCheckoutFixture(t)
	gitRun(t, repo, "checkout", "-q", parked)

	// The parked state is real: master does NOT yet contain the candidate.
	if _, err := repoSweepGit(repo, "merge-base", "--is-ancestor", candidate, masterBranchName); err == nil {
		t.Fatalf("fixture error: %s already contains %s", masterBranchName, candidate)
	}

	res := MergerRepoSweep(repo, 0)
	if res.Err != "" {
		t.Fatalf("sweep: %s", res.Err)
	}
	if !strings.Contains(res.Checkout, MainCheckoutRestoredMarker) {
		t.Fatalf("the sweep did not report the parked checkout it repaired: %q\noutcomes=%v", res.Checkout, res.Outcomes)
	}
	if got := headBranch(t, repo); got != masterBranchName {
		t.Errorf("the sweep left the checkout on %q", got)
	}
	// THE PROPERTY THE ORDERING BUYS: the parked branch is back in the candidate
	// set and its work (and the candidate's) reached master.
	if _, err := repoSweepGit(repo, "merge-base", "--is-ancestor", candidate, masterBranchName); err != nil {
		t.Errorf("the candidate never landed: master does not contain %s\noutcomes=%v", candidate, res.Outcomes)
	}
	if res.Merged == 0 {
		t.Errorf("the sweep landed nothing after the restore\noutcomes=%v", res.Outcomes)
	}
}

// TestIntegrationBranchForRepoPrefersMasterThenMain pins the resolver: master
// wins whenever it exists, main is the tolerated alternate, and a repo with
// neither falls back to master (so a caller reports a refusal rather than
// inventing a branch).
func TestIntegrationBranchForRepoPrefersMasterThenMain(t *testing.T) {
	master := t.TempDir()
	gitRun(t, master, "init", "-q", "-b", masterBranchName)
	gitRun(t, master, "config", "user.email", "t@example.invalid")
	gitRun(t, master, "config", "user.name", "t")
	writeRepoFile(t, master, "f.txt", "x\n")
	gitRun(t, master, "add", "f.txt")
	gitRun(t, master, "commit", "-q", "-m", "base")
	gitRun(t, master, "branch", mainBranchName)
	if got := IntegrationBranchForRepo(master); got != masterBranchName {
		t.Errorf("with both refs present the resolver chose %q, want %s", got, masterBranchName)
	}

	mainOnly := t.TempDir()
	gitRun(t, mainOnly, "init", "-q", "-b", mainBranchName)
	gitRun(t, mainOnly, "config", "user.email", "t@example.invalid")
	gitRun(t, mainOnly, "config", "user.name", "t")
	writeRepoFile(t, mainOnly, "f.txt", "x\n")
	gitRun(t, mainOnly, "add", "f.txt")
	gitRun(t, mainOnly, "commit", "-q", "-m", "base")
	if got := IntegrationBranchForRepo(mainOnly); got != mainBranchName {
		t.Errorf("a main-only repo resolved to %q, want %s", got, mainBranchName)
	}

	neither := t.TempDir()
	gitRun(t, neither, "init", "-q", "-b", "work")
	if got := IntegrationBranchForRepo(neither); got != masterBranchName {
		t.Errorf("a repo with neither ref resolved to %q, want the %s fallback", got, masterBranchName)
	}
}
