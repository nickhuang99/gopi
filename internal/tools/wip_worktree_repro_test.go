package tools

// Bug 01a08e8f regression: a freshly created linked worktree whose branch
// sits AT the master tip with ZERO commits of its own (the Rule 6a opening
// move: `git worktree add -b X <path>` then read code) is a live session's
// ground — the merged-branch cleanup must leave it alone.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// newWIPTipFixture builds the victim shape: a linked worktree created at the
// integration tip on a brand-new branch, no commits, no tree changes.
func newWIPTipFixture(t *testing.T) (*mergedCleanupFixture, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	f := &mergedCleanupFixture{t: t, root: root,
		clientDir: filepath.Join(root, "client"), wtDir: filepath.Join(root, "wt"),
		featBranch: "feat/fresh-wip"}
	git := func(dir string, args ...string) string { return mergedCleanupFixtureGit(t, dir, args...) }
	git(root, "init", f.clientDir)
	git(f.clientDir, "config", "user.email", "t@t")
	git(f.clientDir, "config", "user.name", "t")
	git(f.clientDir, "checkout", "-b", "master")
	os.WriteFile(filepath.Join(f.clientDir, "base.txt"), []byte("base\n"), 0o644)
	git(f.clientDir, "add", "base.txt")
	git(f.clientDir, "commit", "-m", "base")
	// The victim shape: worktree + branch at the tip, ZERO commits.
	git(f.clientDir, "worktree", "add", f.wtDir, "-b", f.featBranch)
	git(f.clientDir, "checkout", "master")
	return f, f.wtDir
}

// TestMergedCleanupFreshWIPTipWorktreeSurvives is the RED test for
// 01a08e8f: before the fix the sweep reaped the pair (worktree removed +
// branch -d), the branch being trivially "merged" at the tip.
func TestMergedCleanupFreshWIPTipWorktreeSurvives(t *testing.T) {
	f, wt := newWIPTipFixture(t)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("fresh WIP worktree at the tip must not be reaped: %+v", rep.Removed)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree must survive the sweep: %v", err)
	}
	if out, err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).CombinedOutput(); err != nil {
		t.Fatalf("branch must survive the sweep: %v (%s)", err, out)
	}
	if len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0].Detail, mergedCleanupNoWorkMarker) {
		t.Fatalf("want one no-work skip, got skipped=%+v", rep.Skipped)
	}
}

// TestMergedCleanupFreshWIPTipSurvivesWithLiveRow: the same shape, with the
// session's own ledger row (the environment row a session run gets) naming
// the worktree it works in.
func TestMergedCleanupFreshWIPTipSurvivesWithLiveRow(t *testing.T) {
	f, wt := newWIPTipFixture(t)
	db := testutil.NewTestDBPath(t)
	mergedCleanupSeedLiveRow(t, db, "feat/other-name") // row's branch differs from the checkout's

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("live-owned worktree must not be reaped: %+v", rep.Removed)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree must survive: %v", err)
	}
}

// TestMergedCleanupUncommittedEditsAtTipSurvives: a session that created the
// worktree and EDITED files (uncommitted) is protected too — the reap must
// never delete a dirty tree.
func TestMergedCleanupUncommittedEditsAtTipSurvives(t *testing.T) {
	f, wt := newWIPTipFixture(t)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	os.WriteFile(filepath.Join(wt, "base.txt"), []byte("edited by the session\n"), 0o644)

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("edited WIP worktree must not be reaped: %+v", rep.Removed)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree must survive: %v", err)
	}
}

// TestMergedCleanupProgressedWorktreeStillReported: the legitimate recycle is
// NOT broken by the new guard — a worktree whose branch DID commit work that
// is in master is still reaped.
func TestMergedCleanupProgressedWorktreeStillReported(t *testing.T) {
	f := newMergedCleanupFixture(t)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	rep := mergedCleanupRun(t, f)
	// CONVERTED (epic sweep-report-not-act, 2026-09-19): a merged worktree with
	// its own commit passed every guard and was reaped. It still passes every
	// guard — it is now REPORTED, and nothing is taken away.
	if len(rep.Reported) != 1 {
		t.Fatalf("a merged worktree WITH its own commit must still be REPORTED: reported=%+v skipped=%+v failed=%+v",
			rep.Reported, rep.Skipped, rep.Failed)
	}
	if len(rep.Removed) != 0 {
		t.Fatalf("the reaper mutates nothing: removed=%+v", rep.Removed)
	}
}

// TestMergedCleanupReapFiresSignal (Rule 15 — the disposal path must not be
// silent): every reaped pair announced itself on the operator channel as the
// worktree-reaped event, naming the branch and the worktree.
//
// HONEST STATUS (epic sweep-report-not-act, 2026-09-19): with the mutating arms
// retired, the production disposition no longer reaps, so this event no longer
// fires — the pair is reported on the branch sweeper's own report line and, when
// a LIVE owner names it, on the `sweep-would-prune-live-branch` notify event
// (see TestRepoSweepWouldPruneWarnsOnLiveOwner). The signal is kept here because
// the event name and its payload contract still exist for the disposal path.
func TestMergedCleanupReapFiresSignal(t *testing.T) {
	f := newMergedCleanupFixture(t)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+eventsFile)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", mergedCleanupReapedEvent)

	rep := mergedCleanupRun(t, f)
	if len(rep.Reported) != 1 || len(rep.Removed) != 0 {
		t.Fatalf("want the merged pair reported and nothing removed, got reported=%+v removed=%+v skipped=%+v",
			rep.Reported, rep.Removed, rep.Skipped)
	}
	// CONVERTED (epic sweep-report-not-act, 2026-09-19): this asserted the
	// worktree-reaped event. The event notified a REAP; the production
	// disposition reaps nothing, so nothing fires — and the ground is still
	// there to prove it. The pair's visibility now comes from the report line
	// plus the `sweep-would-prune-live-branch` alarm when a LIVE row names the
	// branch (see TestRepoSweepWouldPruneWarnsOnLiveOwner).
	//stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if data, _ := os.ReadFile(eventsFile); strings.Contains(string(data), `"event":"`+mergedCleanupReapedEvent+`"`) {
		t.Fatalf("the retried disposal must not announce a reap it did not perform: %s", data)
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("the worktree must survive the pass: %v", err)
	}
}

// TestMergedCleanupFreshWIPAfterMasterAdvances is the regression test for the
// hole the FIRST version of the fix had (measured on the host): the guard was
// "worktree HEAD == master now", so the very next concurrent commit to master
// made the fresh worktree reapable again. The discriminator must be a
// PROPERTY of the worktree, not of the current tip.
func TestMergedCleanupFreshWIPAfterMasterAdvances(t *testing.T) {
	f, wt := newWIPTipFixture(t)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	// Master advances AFTER the worktree was created at the (then) tip —
	// exactly what a parallel session's merge does.
	mergedCleanupFixtureGit(t, f.clientDir, "commit", "--allow-empty", "-m", "someone else advanced master")

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("a fresh WIP worktree must stay protected after master advances: %+v", rep.Removed)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree must survive: %v", err)
	}
	if out, err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).CombinedOutput(); err != nil {
		t.Fatalf("branch must survive: %v (%s)", err, out)
	}
}

// TestMergedCleanupWIPWithCommitIsProtected: as soon as the session commits
// in the worktree, the pair is ordinary WIP — protected by the existing
// recommit guard (④), never reaped.
func TestMergedCleanupWIPWithCommitIsProtected(t *testing.T) {
	f, wt := newWIPTipFixture(t)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	os.WriteFile(filepath.Join(wt, "work.txt"), []byte("real work\n"), 0o644)
	mergedCleanupFixtureGit(t, wt, "add", "work.txt")
	mergedCleanupFixtureGit(t, wt, "commit", "-m", "session work")

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("a worktree with its own commit must never be reaped: %+v", rep.Removed)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree must survive: %v", err)
	}
}
