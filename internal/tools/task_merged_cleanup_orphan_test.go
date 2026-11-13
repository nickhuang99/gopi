package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The orphan-ref lane's tests. The shape under test is the one EVERY worktree
// removal in the system leaves behind (measured, git 2.34.1, probes P1–P5):
// `git worktree remove --force` drops the registration and NOT the branch, so
// a merged delivery branch survives its own cleanup with no worktree at all —
// and nothing collected that shape before this lane existed.

// mergedCleanupOrphanFixture is the frozen pair from the fixture above with
// its worktree taken away the way the merge executor / adoption chain / build
// gate take it: `worktree remove --force`. The branch is left, unregistered.
//
// The force flag is local to the TEST (the production sweep never removes a
// worktree with --force and never adds one — its lane is ref-only; the
// fixture is reproducing the OTHER component's call, so it spells the
// argument rather than borrowing a production constant that must keep
// meaning "the sweep's own, non-forced call").
const (
	mergedCleanupFixtureWorktreeForceFlag = "--force"
)

func mergedCleanupOrphanFixture(t *testing.T) *mergedCleanupFixture {
	t.Helper()
	f := newMergedCleanupFixture(t)
	mergedCleanupFixtureGit(t, f.clientDir, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb,
		mergedCleanupFixtureWorktreeForceFlag, f.wtDir)
	// The premise of the lane, asserted rather than assumed: the registration
	// is gone AND the branch is still there.
	if out := mergedCleanupFixtureGit(t, f.clientDir, finishCleanupWorktreeVerb,
		finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag); strings.Contains(out, f.featBranch) {
		t.Fatalf("worktree remove left a registration: %s", out)
	}

	return f
}

// TestMergedCleanupReportsOrphanMergedBranchWithNoWorktree is the lane's reason
// to exist, CONVERTED (epic sweep-report-not-act, 2026-09-19): a merged branch
// whose worktree a prior removal already took is REPORTED (ref-only — there is
// nothing else left to name). It used to be reaped; the lane mutates no ref now.
func TestMergedCleanupReportsOrphanMergedBranchWithNoWorktree(t *testing.T) {
	f := mergedCleanupOrphanFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	// The delivery shape: a row that WENT TERMINAL on this branch.
	mergedCleanupSeedTerminalRow(t, db, f.featBranch)

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("the reaper mutates nothing: removed=%+v", rep.Removed)
	}
	if len(rep.Reported) != 1 {
		t.Fatalf("want the orphan ref REPORTED: reported=%+v skipped=%+v failed=%+v notes=%+v",
			rep.Reported, rep.Skipped, rep.Failed, rep.Notes)
	}
	if rep.Reported[0].Branch != f.featBranch {
		t.Errorf("reported branch = %q, want %q", rep.Reported[0].Branch, f.featBranch)
	}
	if rep.Reported[0].Worktree != mergedCleanupOrphanWorktreeLabel {
		t.Errorf("the report must be labelled as the ref-only shape %q, got %q",
			mergedCleanupOrphanWorktreeLabel, rep.Reported[0].Worktree)
	}
	// Physical truth: the ref SURVIVES (the worktree was already gone and the
	// lane must not have created or touched any directory).
	assertBranchPresent(t, f.clientDir, f.featBranch)
	if _, err := os.Stat(f.wtDir); !os.IsNotExist(err) {
		t.Errorf("the orphan lane re-created a worktree: %s", f.wtDir)
	}
}

// TestMergedCleanupOrphanLaneIsRefOnly: the lane's blast radius. It must
// touch the ref and NOTHING else — no worktree created, no worktree verb run,
// the main checkout and its files exactly as they were. (The measurement this
// lane exists for: `worktree remove` drops the registration and leaves the
// ref; the lane is the mirror image — it drops the ref and leaves the
// working tree alone.)
// it drops the ref and leaves the working tree alone.) — CONVERTED: the lane no
// longer drops the ref either (epic sweep-report-not-act, 2026-09-19).
func TestMergedCleanupOrphanLaneIsRefOnly(t *testing.T) {
	f := mergedCleanupOrphanFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	mergedCleanupSeedTerminalRow(t, db, f.featBranch)

	before := mergedCleanupFixtureGit(t, f.clientDir, finishCleanupWorktreeVerb,
		finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	rep := mergedCleanupRun(t, f)
	after := mergedCleanupFixtureGit(t, f.clientDir, finishCleanupWorktreeVerb,
		finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)

	if before != after {
		t.Errorf("the ref-only lane changed the worktree list:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if strings.Count(after, "worktree ") != 1 || !strings.Contains(after, f.clientDir) {
		t.Errorf("want exactly the main checkout registered, got:\n%s", after)
	}
	if len(rep.Removed) != 0 || len(rep.Reported) != 1 || rep.Reported[0].Branch != f.featBranch {
		t.Fatalf("want the orphan ref REPORTED and nothing removed: removed=%+v reported=%+v skipped=%+v failed=%+v",
			rep.Removed, rep.Reported, rep.Skipped, rep.Failed)
	}
	// The main checkout is intact — the lane ran no verb that could touch it.
	for _, name := range []string{"base.txt", "feat.txt"} {
		if _, err := os.Stat(filepath.Join(f.clientDir, name)); err != nil {
			t.Errorf("main checkout lost %s: %v", name, err)
		}
	}
	assertBranchPresent(t, f.clientDir, f.featBranch)
}

// TestMergedCleanupOrphanBranchLiveRowKept: the lane's live guard. A row
// still OWING the branch protects it even with no worktree — the row's own
// chain owns whatever remains (nothing in this pass may decide the row's
// branch is surplus).
func TestMergedCleanupOrphanBranchLiveRowKept(t *testing.T) {
	f := mergedCleanupOrphanFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	mergedCleanupSeedLiveRow(t, db, f.featBranch)

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("a live row's branch was reaped: removed=%+v", rep.Removed)
	}
	if !mergedCleanupSkippedWith(rep, f.featBranch, mergedCleanupLiveOwnerMarker) {
		t.Fatalf("want the live-owner skip naming %q, got skipped=%+v notes=%+v",
			mergedCleanupLiveOwnerMarker, rep.Skipped, rep.Notes)
	}
}

// TestMergedCleanupOrphanLaneRefusesAnUnmergedRef: the lane's last line of
// defence, tested on BOTH sides of the race it guards.
//
// (a) AFTER the ref has moved off master, the coarse discovery (`branch
// --merged master`) no longer lists it, so the pass is EMPTY — nothing
// removed, nothing even reached (the convention the pair lane's
// TestMergedCleanupRecommitAfterMergeSkipped pins).
//
// (b) The race window itself, driven directly: discovery listed the branch,
// THEN the ref moved. The lane is handed the branch anyway and must refuse it
// on the recheck (its ② guard) — the window in which a stale `--merged`
// snapshot would otherwise authorise a delete.
func TestMergedCleanupOrphanLaneRefusesAnUnmergedRef(t *testing.T) {
	f := mergedCleanupOrphanFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	mergedCleanupSeedTerminalRow(t, db, f.featBranch)

	// The recommit is built in a throwaway clone and pushed back: no `branch
	// -d` runs anywhere, so the fixture repo's checkout is untouched and the
	// branch simply gains a commit master does not contain.
	post := filepath.Join(f.root, "post-merge-work")
	mergedCleanupFixtureGit(t, f.clientDir, "clone", "--quiet", f.clientDir, post)
	mergedCleanupFixtureGit(t, post, "checkout", "--quiet", "-b", f.featBranch, "origin/"+f.featBranch)
	os.WriteFile(filepath.Join(post, "after.txt"), []byte("after\n"), 0o644)
	mergedCleanupFixtureGit(t, post, "add", "after.txt")
	mergedCleanupFixtureGit(t, post, "commit", "-m", "work after the merge")
	mergedCleanupFixtureGit(t, post, "push", "--quiet", "origin", f.featBranch)
	mergedCleanupFixtureGit(t, f.clientDir, "checkout", mergedCleanupIntegrationBranch)

	// (a) the coarse discovery drops it → an empty pass.
	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 || len(rep.Skipped) != 0 || len(rep.Failed) != 0 {
		t.Fatalf("a moved ref must not be reached at all: removed=%+v skipped=%+v failed=%+v",
			rep.Removed, rep.Skipped, rep.Failed)
	}

	// (b) the race window, driven directly.
	rep2 := &MergedCleanupReport{Repo: f.clientDir}
	mergedCleanupOrphanBranch(f.clientDir, f.featBranch, rep2, map[string]bool{}, nil, MergedCleanupOptions{})
	if len(rep2.Removed) != 0 || len(rep2.Reported) != 0 {
		t.Fatalf("the lane deleted a ref that left master: removed=%+v reported=%+v", rep2.Removed, rep2.Reported)
	}
	if !mergedCleanupSkippedWith(rep2, f.featBranch, "no longer an ancestor") {
		t.Fatalf("want the unmerged skip, got skipped=%+v notes=%+v failed=%+v",
			rep2.Skipped, rep2.Notes, rep2.Failed)
	}
	if out, err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).CombinedOutput(); err != nil {
		t.Errorf("the unmerged branch was deleted: %s", out)
	}
}

// mergedCleanupSkippedWith reports whether a step about branch carries want in
// its detail.
func mergedCleanupSkippedWith(rep *MergedCleanupReport, branch, want string) bool {
	for _, s := range rep.Skipped {
		if s.Branch == branch && strings.Contains(s.Detail, want) {
			return true
		}
	}
	return false
}
