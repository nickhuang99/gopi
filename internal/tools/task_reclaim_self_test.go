// Producer-side (self) worktree release — regression tests for the gap that
// left live row 01a095b2's directory parked (2026-09-12).
//
// THE REPRO, at the layer where the defect lives: a row whose holder is THIS
// process — the shape every worker round leaves behind, because the claim
// stamp is the long-lived worker PID — and whose worktree is CLEAN and
// unoccupied. Before the fix NOTHING released it: ReclaimDeadHolderWorktrees
// requires a provably DEAD pid, so it never even looked, `git worktree list`
// kept naming the directory and scripts/safe-merge.sh gate [c] kept refusing
// the branch. The tests below FAIL on the pre-fix tree (the directory is still
// on disk after the pass) and pass with the producer-side release.
//
// The negative cases pin the two guards that must survive the new arms:
// a directory a LIVE process sits in, and one carrying uncommitted work.
// Both use a row held by THIS process, so the only thing refusing is the
// guard under test.
package tools

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// selfReleasePollInterval is how often the fixture waits for a freshly started
// holder's procfs CWD to become observable (the child shell exists a moment
// before it chdirs).
const selfReleasePollInterval = 20 * time.Millisecond

// selfReleaseFixture builds a repo + linked worktree on a feat branch and a
// row held by THIS process naming it — the worker's own shape. Returns
// (repo, worktree, branch, tip).
func selfReleaseFixture(t *testing.T, taskID string) (repo, wt, branch, tip string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	t.Setenv(config.SyncPeersEnv, "")

	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	wt = filepath.Join(root, "wt")
	branch = "feat/self-release"
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")
	gitRun(t, repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "deliverable.txt"), []byte("the delivery"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "deliverable.txt")
	gitRun(t, repo, "commit", "-m", "the deliverable")
	tip = gitOut(t, repo, "rev-parse", branch)
	gitRun(t, repo, "checkout", "master")
	gitRun(t, repo, finishCleanupWorktreeVerb, "add", wt, branch)

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "self release", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedSelfHolderRow(t, taskID, repo, branch, wt)
	return repo, wt, branch, tip
}

// seedSelfHolderRow stamps a row as OWNED BY THIS PROCESS, in progress, with
// the deliverable naming repo/branch/worktree. The claim stamp comes from the
// production renderer (TaskClaimOwner) rather than a hand-built string, so the
// test cannot drift from the shape the release pass matches on.
func seedSelfHolderRow(t *testing.T, taskID, repo, branch, wt string) {
	t.Helper()
	stamp := TaskClaimOwner("")
	if stamp == "" {
		t.Skip("no hostname on this machine — the self-claim shape is untestable here")
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+
			` = ?, updated_at = ?, deliverable = ? WHERE task_id = ?`,
		taskStatusInProgress, stamp, 1000.0, 2000.0, 2000.0,
		`{"repo":"`+repo+`","branch":"`+branch+`","worktree":"`+wt+`"}`, taskID); err != nil {
		t.Fatalf("seed self-held row: %v", err)
	}
}

// TestReclaimOwnFinishedWorktreesFreesItsOwnDirectory is THE repro: a clean,
// unoccupied worktree named by a row THIS process holds must be gone after the
// release pass, the branch must survive, and the SAME directory must then be
// invisible to git's worktree list (which is what unblocks safe-merge gate [c]).
func TestReclaimOwnFinishedWorktreesFreesItsOwnDirectory(t *testing.T) {
	repo, wt, branch, tip := selfReleaseFixture(t, "t-self-release")

	// Precondition, stated as the defect: the dead-holder sweep — the only
	// release arm before this fix — does NOT see this row (its holder is
	// alive), so it leaves the directory exactly as it found it.
	if got := ReclaimDeadHolderWorktrees(false); len(got) != 0 {
		t.Fatalf("precondition: a LIVE holder's row must be invisible to the dead-holder arm, got %v", got)
	}
	if worktreeListed(t, repo, wt) == false {
		t.Fatalf("precondition: git must list the worktree before the release: %s", worktreeList(t, repo))
	}

	got := ReclaimOwnFinishedWorktrees()
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, ReclaimSelfReleasedMarker) || !strings.Contains(joined, "t-self-release") {
		t.Fatalf("the producer release must report the row it freed, got %v", got)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("the released worktree must be GONE (stat err=%v) — this is the repro: before the fix it stayed on disk", err)
	}
	if worktreeListed(t, repo, wt) {
		t.Fatalf("git must no longer list the released worktree: %s", worktreeList(t, repo))
	}
	// The BRANCH is the deliverable: never touched, byte-identical tip.
	if now := gitOut(t, repo, "rev-parse", branch); now != tip {
		t.Errorf("the branch tip must be unchanged by the release: %s → %s", tip, now)
	}
	if err := exec.Command("git", "-C", repo, "cat-file", "-e", branch+":deliverable.txt").Run(); err != nil {
		t.Errorf("the deliverable must still be on the branch: %v", err)
	}
}

// TestReclaimOwnFinishedWorktreesRefusesALiveProcessInside: the cwd guard is
// the one that must survive the new arm — a directory somebody is WORKING in
// is never freed, even when its row belongs to this very process. The refusal
// must be NAMED (Rule 15), not silent.
func TestReclaimOwnFinishedWorktreesRefusesALiveProcessInside(t *testing.T) {
	repo, wt, branch, _ := selfReleaseFixture(t, "t-self-inuse")
	_ = repo

	// A live process whose cwd is the worktree, held from a CHILD shell so it
	// is not this process's own ancestry (the self-chain excludes self only).
	//fixture-spawn-ok: the child's CWD inside this directory is exactly what is measured (the live-holder veto reads /proc/<pid>/cwd), so it cannot be parked away; a helper would also make it this process's own child, which the self-chain exclusion is testing against
	holder := exec.Command("sh", "-c", "cd \"$1\" && sleep 30", "sh", wt)
	if err := holder.Start(); err != nil {
		t.Fatalf("hold dir: %v", err)
	}
	t.Cleanup(func() {
		_ = holder.Process.Kill() //stylegate:ignore teardown of the fixture holder; the reclaim verdict is what the test asserts
		_ = holder.Wait()         //stylegate:ignore reap of the killed fixture holder; its cwd is read from /proc
	})
	waitForProcCwd(t, holder.Process.Pid, wt)

	got := ReclaimOwnFinishedWorktrees()
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, reclaimSelfRefusedMarker) {
		t.Fatalf("a refusal must be NAMED, got %v", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("a worktree a live process works in must SURVIVE the release: %v", err)
	}
	if !worktreeListed(t, repo, wt) {
		t.Errorf("the surviving worktree must still be registered: %s", worktreeList(t, repo))
	}
	if err := gitBranchExists(t, repo, branch); err != nil {
		t.Errorf("the branch must be untouched: %v", err)
	}
}

// TestReclaimOwnFinishedWorktreesRefusesADirtyWorktree: uncommitted work is
// work — a dirty directory is a human's call, never a close-out's.
func TestReclaimOwnFinishedWorktreesRefusesADirtyWorktree(t *testing.T) {
	_, wt, branch, _ := selfReleaseFixture(t, "t-self-dirty")
	if err := os.WriteFile(filepath.Join(wt, "uncommitted.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ReclaimOwnFinishedWorktrees()
	if joined := strings.Join(got, "\n"); !strings.Contains(joined, reclaimSelfRefusedMarker) {
		t.Fatalf("a dirty directory must be refused BY NAME, got %v", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the dirty worktree must survive: %v", err)
	}
	_ = branch
}

// TestReclaimOwnFinishedWorktreesIgnoresAnotherRowsDirectory: the ownership
// axis — a row held by SOMEBODY ELSE (a live peer worker) is never in this
// process's candidate set, however finished it looks.
func TestReclaimOwnFinishedWorktreesIgnoresAnotherRowsDirectory(t *testing.T) {
	repo, wt, branch, _ := selfReleaseFixture(t, "t-self-foreign")
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET claimed_by = ? WHERE task_id = ?`, "someone-else:"+"1", "t-self-foreign"); err != nil {
		t.Fatalf("re-stamp: %v", err)
	}

	if got := ReclaimOwnFinishedWorktrees(); len(got) != 0 {
		t.Fatalf("another holder's row must never be touched by this process, got %v", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the peer's directory must survive: %v", err)
	}
	if !worktreeListed(t, repo, wt) {
		t.Errorf("the peer's worktree must still be registered: %s", worktreeList(t, repo))
	}
	if err := gitBranchExists(t, repo, branch); err != nil {
		t.Errorf("the peer's branch must be untouched: %v", err)
	}
}

// TestReclaimOwnFinishedWorktreesRepairsAVanishedDirectory is the second half
// of the release: the directory is ALREADY gone but git still holds the
// registration, which is what makes safe-merge gate [c] refuse the branch. The
// producer's pass must PRUNE it (the same repair the sweep runs) and say so.
func TestReclaimOwnFinishedWorktreesRepairsAVanishedDirectory(t *testing.T) {
	repo, wt, branch, tip := selfReleaseFixture(t, "t-self-vanished")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	got := ReclaimOwnFinishedWorktrees()
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, reclaimRepairedMarker) {
		t.Fatalf("a vanished-but-registered worktree must be repaired and named, got %v", got)
	}
	if worktreeListed(t, repo, wt) {
		t.Fatalf("the stale registration must be gone: %s", worktreeList(t, repo))
	}
	if now := gitOut(t, repo, "rev-parse", branch); now != tip {
		t.Errorf("the repair must not touch the branch: %s → %s", tip, now)
	}
	// Idempotent: the second pass has nothing left to say about this row.
	if again := ReclaimOwnFinishedWorktrees(); len(again) != 0 {
		t.Errorf("a repaired row must not be re-reported, got %v", again)
	}
}

// TestReclaimOwnFinishedWorktreesKillSwitchIsReportOnly: the env knob turns the
// REMOVAL off without turning the disclosure off — the directory is named as
// withheld, never silently kept.
func TestReclaimOwnFinishedWorktreesKillSwitchIsReportOnly(t *testing.T) {
	t.Setenv(ReclaimSelfEnv, "off")
	_, wt, branch, _ := selfReleaseFixture(t, "t-self-off")

	got := ReclaimOwnFinishedWorktrees()
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, reclaimSelfRefusedMarker) || !strings.Contains(joined, ReclaimSelfEnv) {
		t.Fatalf("report-only mode must name the row AND the switch, got %v", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("report-only mode must not remove anything: %v", err)
	}
	_ = branch
}

// gitOut runs one git command in dir and returns its trimmed stdout, failing
// the test on error — the read half gitRun (which returns nothing) omits.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// TWO streams, like the production parsed readers (git_parsed_output.go):
	// the RETURNED value is PARSED by the arms, so git's stderr — a
	// `GIT_TRACE=1` trace line in particular — must never become part of it.
	// Measured 2026-10-26: with CombinedOutput here, every tip comparison in
	// TestSelfDelivery compared a trace line instead of an object id, so the
	// acceptance run failed for the TEST's reason, not the code's.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String())
}

// waitForProcCwd blocks until pid's cwd reads as dir (the child's shell execs
// before its cwd is observable).
func waitForProcCwd(t *testing.T, pid int, dir string) {
	t.Helper()
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		want = dir
	}
	for i := 0; i < 100; i++ {
		if target, err := os.Readlink(filepath.Join("/proc", itoa(pid), "cwd")); err == nil {
			if target == want || target == dir {
				return
			}
			if resolved, rerr := filepath.EvalSymlinks(target); rerr == nil && resolved == want {
				return
			}
		}
		time.Sleep(selfReleasePollInterval) //stylegate:allow-sleep poll interval while the self-release signal lands on the ledger; bounded by the loop's deadline
	}
	t.Fatalf("pid %d never entered %s", pid, dir)
}
