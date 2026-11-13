package tools

// THE CLONE GROUND IS NOT "ANOTHER REPO'S LINKED WORKTREE" — the smallest
// reproduction of the misjudgement in reclaimDispositionJudged.
//
// THE DEFECT. The disposition asks "does MY repo's worktree list name this
// path?" and, when it does not, asks the DIRECTORY's own repo the same
// question — reading "yes" as proof that the path belongs to somebody else:
//
//	registered := reclaimWorktreeRegisteredIn(r.Repo, r.Worktree)
//	if !registered {
//		if reclaimWorktreeRegisteredIn(r.Worktree, r.Worktree) {
//			return reclaimSkip, reclaimUnreachableForeignGroundReason(r.Repo), reclaimStageForeign
//		}
//
// But `git worktree list` ALWAYS names the repository's own main checkout, and a
// CLONE ground's main checkout is exactly r.Worktree. So every clone-channel
// ground answered "yes" to the second question and was classified
// reclaimStageForeign — a stage that is permanently silent for ANY status and
// writes an append-only `reclaim-unreachable` marker. Measured on this host
// 2026-10-27: 113 of 114 markers carried that false sentence, and the sentence
// is false twice over — the directory is neither a LINKED WORKTREE nor ANOTHER
// repo's (it is its own repository).
//
// THE FIX these tests pin: MEASURE THE SHAPE before judging foreign, with the
// repository's ONE shape discriminator (devworkspace.GroundModelOf). CLONE ⇒
// this row's own repository, with its own stage and wording; only a LINKED
// worktree can be foreign.
//
// Nothing here asserts a removal: Rule 42 — a corrected criterion must never
// make a directory deletable, so the clone ground is checked to STILL EXIST.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devworkspace"
)

// reclaimCloneGroundFixture builds the incident's exact shape at the smallest
// sample: a REAL repository (the row's repo) plus a REAL CLONE of it (the row's
// recorded worktree), so the second `worktree list` answers "yes" — which is
// what produced the misjudgement. It returns the repo, the clone and the branch
// checked out in the clone.
func reclaimCloneGroundFixture(t *testing.T) (repo, clone, branch string) {
	t.Helper()
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	clone = filepath.Join(root, "clone-ground")
	branch = "master"
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", branch)
	base := filepath.Join(repo, "a.txt")
	if err := os.WriteFile(base, []byte("base"), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")
	gitRun(t, root, "clone", repo, clone)

	// THE PRECONDITION THE WHOLE DEFECT RESTS ON, measured rather than assumed:
	// the clone's own list names the clone, and the row's repo does not.
	if got := devworkspace.GroundModelOf(clone); got != devworkspace.GroundModelClone {
		t.Fatalf("%s must be a CLONE ground (.git is a directory), measured %q", clone, got)
	}
	if worktreeListed(t, repo, clone) {
		t.Fatalf("precondition: the row's repo must NOT list the clone as a worktree: %s", worktreeList(t, repo))
	}
	if !worktreeListed(t, clone, clone) {
		t.Fatalf("precondition: the clone's OWN repo must list it (that is the fact the old criterion misread): %s",
			worktreeList(t, clone))
	}
	return repo, clone, branch
}

// TestReclaimCloneGroundDispositionNamesItsOwnRepo is the defect at its own
// rung — the DISPOSITION, with no pass machinery in the way. A clone ground must
// not be reported as foreign, and the wording it does get must say what the
// directory actually is.
func TestReclaimCloneGroundDispositionNamesItsOwnRepo(t *testing.T) {
	repo, clone, branch := reclaimCloneGroundFixture(t)
	row := reclaimDeadHolderRow{
		TaskID: "t-clone-disposition", Repo: repo, Worktree: clone,
		Branch: branch, Status: taskStatusCompleted,
	}

	verdict, why, stage := reclaimDispositionJudged(row, newReclaimPrunePlan())
	if verdict != reclaimSkip {
		t.Fatalf("a clone ground is not this arm's to free: verdict must be reject, got %v (%q)", verdict, why)
	}
	if stage == reclaimStageForeign {
		t.Fatalf("a CLONE ground must never reach %v — the stage is permanently silent for ANY status and records "+
			"a sentence that is false twice over (not a linked worktree, not another repo's): %q",
			stage, why)
	}
	if strings.Contains(why, reclaimUnreachableForeignGroundToken) {
		t.Fatalf("the wording must not claim the clone ground is ANOTHER repo's linked worktree (Rule 15: the "+
			"sentence an operator reads has to be true): %q", why)
	}
	if !strings.Contains(why, reclaimUnreachableCloneGroundToken) {
		t.Fatalf("the clone ground's refusal must name what it IS (%q), got %q",
			reclaimUnreachableCloneGroundToken, why)
	}
	if stage != reclaimStageClone {
		t.Fatalf("the clone ground gets its OWN stage (a finished row's clone is not this arm's — the clone "+
			"collect leg owns it), measured %v", stage)
	}
	if _, err := os.Stat(clone); err != nil {
		t.Fatalf("Rule 42: the corrected criterion must leave the directory in place: %v", err)
	}
}

// TestReclaimCloneGroundLiveRowMarkerIsNotForeign drives the SAME shape through
// the real pass with a LIVE-status row — the status for which the false sentence
// is DURABLE: the marker is written to the row's append-only trail, where it
// outlives every later pass (113 such markers were measured on this host).
func TestReclaimCloneGroundLiveRowMarkerIsNotForeign(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc is unavailable — the dead-holder shape cannot be modelled")
	}
	freshReclaimRefusalMemo(t)
	fossilLedgerEnv(t) // one throwaway ledger for this test
	repo, clone, branch := reclaimCloneGroundFixture(t)

	const id = "t-clone-ground-live"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "clone ground", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusInProgress, repo, branch, clone, hostPid(t, deadPID(t)))

	stderr := captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })
	if strings.Contains(stderr, reclaimUnreachableForeignGroundToken) {
		t.Fatalf("the operator-facing refusal must not describe the clone ground as ANOTHER repo's linked "+
			"worktree:\n%s", stderr)
	}
	for _, marker := range reclaimMarkerEvents(t, id) {
		if strings.Contains(marker, reclaimUnreachableForeignGroundToken) {
			t.Fatalf("the trail marker must not carry the false sentence — it is append-only and no later pass "+
				"can withdraw it: %q", marker)
		}
	}
	if _, err := os.Stat(clone); err != nil {
		t.Fatalf("Rule 42: no directory may be removed because the criterion changed: %v", err)
	}
}
