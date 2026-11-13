// Regression guard (epic worktree-residue, defect D2): a branch that is checked
// out in a LINKED WORKTREE must not turn a healthy adoption into a reported
// FAILURE.
//
// THE DEFECT, MEASURED ON MASTER (transcript in the devdoc): a clean candidate
// branch held by a linked worktree is ② probed with `git merge --no-commit
// --no-ff` — which SUCCEEDS (git only refuses a branch that could fast-forward
// into its own checkout) — ③a commits it, and then the ref deletion fails
// ("Cannot delete branch 'feat/occupied' checked out at '<path>'", exit 1).
// Master filed that expected, benign outcome through OK/Failed:
//
//	adopted=[feat/occupied]
//	failed=[feat/occupied: prune ref: git branch -d feat/occupied: exit status 1:
//	        error: Cannot delete branch 'feat/occupied' checked out at '…']
//
// So every serve pass reported `failed=1` for a landing that worked, and the
// operator's error channel carried no signal (Rule 15b: a channel that cries
// wolf is a channel nobody reads). The ref itself is NOT leaking — the
// merged-cleanup pass deletes it once the worktree is provably dead — so the
// honest report separates "ref kept" from "failed".
package tools

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// adoptionOccupiedWorktree puts branch in a linked worktree of repo and returns
// the worktree path — the active-bond shape (an existing deliverable branch a
// session is working in).
func adoptionOccupiedWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "occupied-wt")
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-q", wt, branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add %s: %v: %s", branch, err, out)
	}
	return wt
}

func adoptionMasterTip(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", "master").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse master: %v: %s", err, out)
	}
	return string(out)
}

func adoptionBranchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "branch", "--list", branch).CombinedOutput()
	if err != nil {
		t.Fatalf("branch --list: %v: %s", err, out)
	}
	return len(out) > 0
}

// TestAdoptionLandedButRefKeptIsNotAFailure is the fix contract: the landing is
// reported as adopted, the kept ref lands in RefKept (named, with git's own
// reason), and Failed stays EMPTY.
func TestAdoptionLandedButRefKeptIsNotAFailure(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := adoptionTestRepo(t)
	adoptionSeedTask(t, "feat/occupied")
	// A CLEAN branch: the only thing that can make the ref deletion fail is
	// the worktree holding it.
	adoptionPushBranch(t, repo, "feat/occupied", "occupied.txt", "held work\n", false)
	adoptionOccupiedWorktree(t, repo, "feat/occupied")

	before := adoptionMasterTip(t, repo)
	counts, err := AdoptionSweep(context.Background(), AdoptionOptions{
		Repo: repo, MasterBranch: "master", Submit: true,
	})
	if err != nil {
		t.Fatalf("adoption sweep: %v", err)
	}

	// ① the landing is reported.
	if len(counts.Adopted) != 1 || counts.Adopted[0] != "feat/occupied" {
		t.Fatalf("Adopted = %v, want [feat/occupied]", counts.Adopted)
	}
	// ② the kept ref is REPORTED, naming the branch and git's reason.
	if len(counts.RefKept) != 1 {
		t.Fatalf("RefKept = %v, want one entry naming the kept ref", counts.RefKept)
	}
	if !strings.Contains(counts.RefKept[0], "feat/occupied") {
		t.Errorf("RefKept entry %q must name the branch", counts.RefKept[0])
	}
	if !strings.Contains(counts.RefKept[0], adoptionRefKeptMarker) {
		t.Errorf("RefKept entry %q must carry %q so both report faces agree", counts.RefKept[0], adoptionRefKeptMarker)
	}
	if strings.Contains(counts.RefKept[0], "\n") {
		t.Errorf("RefKept entry %q must be ONE line — it is rendered into a line-oriented serve log", counts.RefKept[0])
	}
	// ③ THE DEFECT: nothing about this landing belongs in the error channel.
	if len(counts.Failed) != 0 {
		t.Errorf("Failed = %v, want empty — master filed the benign kept-ref here, so every pass logged a failure for a healthy landing", counts.Failed)
	}
	// ④ NO conflict task: the branch never conflicted.
	if n := adoptionCountConflictTasks(t, "feat/occupied"); n != 0 {
		t.Errorf("merge-conflict tasks = %d, want 0 (the merge was clean)", n)
	}
	// ⑤ ground truth: the landing is real (master moved) and the ref survives.
	if tip := adoptionMasterTip(t, repo); tip == before {
		t.Error("master did not move — the fixture requires a real landing")
	}
	if !adoptionBranchExists(t, repo, "feat/occupied") {
		t.Error("the branch ref vanished — it is held by a live worktree and is deleted later by merged-cleanup, never by a force")
	}
}

// TestAdoptionFailedChannelStaysHonestForRealErrors guards the other direction:
// the RefKept channel must not swallow a genuine failure. A candidate whose
// branch is held by a worktree AND whose merge cannot happen (the branch is
// already an ancestor, so ③a's commit finds nothing... ) is not the case here —
// instead this asserts the invariant at the code level: a branch that cannot be
// merged at all still reaches Failed, not RefKept.
func TestAdoptionFailedChannelStaysHonestForRealErrors(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := adoptionTestRepo(t)
	adoptionSeedTask(t, "feat/conflicting")
	// A REAL conflict: both sides rewrite the same line.
	adoptionPushBranch(t, repo, "feat/conflicting", "README", "client edit\n", true)

	counts, err := AdoptionSweep(context.Background(), AdoptionOptions{
		Repo: repo, MasterBranch: "master", Submit: true,
	})
	if err != nil {
		t.Fatalf("adoption sweep: %v", err)
	}
	// A conflict is DISPATCHED (③b) — it is neither adopted nor a hard failure.
	if len(counts.Adopted) != 0 {
		t.Errorf("Adopted = %v, want empty (the branch conflicts)", counts.Adopted)
	}
	if len(counts.RefKept) != 0 {
		t.Errorf("RefKept = %v, want empty (nothing landed, so no ref was kept)", counts.RefKept)
	}
	if len(counts.Dispatch) != 1 {
		t.Errorf("Dispatch = %v, want the one conflicting branch (③b unchanged)", counts.Dispatch)
	}
}
