package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// ---------------------------------------------------------------------------
// M4.8 deliverable closure (t-1788089584): task_submit stamps the verified
// git repo into the row's deliverable block. Submit-side truth = repo only;
// branch/worktree are the executor's backfill (M4.9).
// ---------------------------------------------------------------------------

// mustGitInit prepares dir as a real git work tree (skips when git is
// absent — the verification shells out to the git binary by design).
func mustGitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed — deliverable verification needs it")
	}
	cmd := exec.Command("git", "-C", dir, "init", "-q")
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", dir, err, out)
	}
}

// TestTaskSubmitRepoStampDeliverable: a verified --repo lands in the row's
// deliverable block and shows up in `task show` as deliverable.repo.
func TestTaskSubmitRepoStampDeliverable(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	repo := t.TempDir()
	mustGitInit(t, repo)

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "repo-carrying work", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := strings.TrimSpace(strings.TrimPrefix(strings.Fields(out)[2], ""))

	show, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "deliverable.repo: "+repo) {
		t.Errorf("TaskShow missing deliverable.repo line:\n%s", show)
	}

	// The stored blob must be exactly the repo JSON (branch/worktree are
	// backfilled later by the executor — M4.9 — never stamped here).
	t2 := &Task{}
	db, err := openTasksDB(t.Context(), path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&t2.Deliverable); err != nil {
		t.Fatalf("read deliverable: %v", err)
	}
	var deliv struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal([]byte(t2.Deliverable), &deliv); err != nil {
		t.Fatalf("deliverable not JSON: %q: %v", t2.Deliverable, err)
	}
	if deliv.Repo != repo {
		t.Errorf("deliverable.repo = %q, want %q", deliv.Repo, repo)
	}
}

// TestTaskSubmitRepoNonRepoAborts: a non-repo directory aborts the WHOLE
// submit — error back, and no task row written (verification precedes the
// INSERT; a half-created row would break the M4.9 backfill loop).
func TestTaskSubmitRepoNonRepoAborts(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	notRepo := t.TempDir() // no git init

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "broken repo work", Repo: notRepo})
	if err == nil {
		t.Fatalf("TaskSubmit with non-repo dir = nil error, out=%q — must abort", out)
	}
	if !strings.Contains(err.Error(), "not a git work tree") {
		t.Errorf("error = %v, want it to name the failed verification", err)
	}

	db, err := openTasksDB(t.Context(), path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("tasks count = %d, want 0 — a failed verification must not leave a row", n)
	}
}

// TestTaskSubmitRepoBareRepoAborts: a bare repo is NOT a work tree — the
// submit must abort (the executor has to branch in the repo).
func TestTaskSubmitRepoBareRepoAborts(t *testing.T) {
	testutil.NewTestDBPath(t)
	bareRepo := filepath.Join(t.TempDir(), "bare.git")
	cmd := exec.Command("git", "init", "--bare", "-q", bareRepo)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "bare work", Repo: bareRepo}); err == nil {
		t.Fatal("TaskSubmit with bare repo = nil error — must abort (executor cannot branch there)")
	}
}

// TestTaskSubmitNoRepoDefaultsToCwd: no --repo → TaskSubmit resolves a default
// repo from the process cwd and stamps it on the row.
//
// THE DEFAULT IS THE DURABLE REPOSITORY, not the cwd string (epic 01a0d55b,
// measured live 2026-09-25): taskSubmitResolveDefaultRepo maps the cwd through
// DurableRepoForSubmit to the repository's MAIN checkout. This test used to
// assert `deliverable.repo == cwd`, which held only until that commit landed; it
// was then red in BOTH environments (measured 2026-09-25 on master + this
// worktree: with this session's env the row rendered /home/user/work/GoPi while
// the test demanded <worktree>/internal/tools, and under `env -i` the same). A
// stale assertion, not an env defect — family A of
// devdoc/internal-tools-whole-package-triage-20260925.md, where the
// env-sensitivity of the other three rows had already hidden this one behind a
// failure count.
//
// WHAT IT PINS NOW: the row's repo is DurableRepoForSubmit(cwd) — taken from the
// ONE resolution the submit engine calls (Rule 13), so the test cannot drift into
// a second opinion about what the default is. The property epic 01a0d55b exists
// for is still exercised on every run, because a package test's cwd is
// <checkout>/internal/tools: the mapping must move off the cwd, and a
// cwd-shaped default — the defect that made a reframe's roadmap children
// unrunnable, their repo being the parent's ground, deleted with its run — shows
// up here immediately. Skips when the cwd is not a work tree (the engine shells
// out to git rev-parse by design).
func TestTaskSubmitNoRepoDefaultsToCwd(t *testing.T) {
	testutil.NewTestDBPath(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := VerifyGitWorkTree(cwd); err != nil {
		t.Skipf("test cwd %s is not a git work tree — the cwd default needs one", cwd)
	}
	want, err := DurableRepoForSubmit(cwd)
	if err != nil {
		t.Fatalf("DurableRepoForSubmit(%s): %v", cwd, err)
	}
	// THE PROPERTY, asserted independently of how `want` was derived. The whole
	// point of the durable default (epic 01a0d55b) is the resolved repo is NOT a
	// path that dies with the run, and a package test's cwd is exactly such a
	// path: `<checkout>/internal/tools` — or, for a worker-run gopi, its per-run
	// GROUND. `git rev-parse --show-toplevel` is a DIFFERENT question from the
	// common-dir the resolution above asks (the worktree's own root, not the
	// repository's main checkout), so requiring the two to differ keeps this test
	// sensitive to a revert to the raw-cwd default — without it, a reader that
	// computed its expectation from the same function could not notice, and the
	// assertion would ratify whatever production did.
	if top, err := repoRootOf(cwd); err == nil && top != cwd {
		if want == cwd {
			t.Errorf("the default repo resolved to the submitting cwd %s, but the durable default "+
				"must name the repository's MAIN checkout (%s is only this work tree's root) — the "+
				"raw-cwd default is the defect epic 01a0d55b removed:\n", cwd, top)
		}
	}

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "plain work"})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := strings.Fields(out)[2]

	show, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, TaskShowDeliverableRepoLabel+" "+want) {
		t.Errorf("TaskShow must stamp %s %s (the durable repo for cwd %s) without --repo:\n%s",
			TaskShowDeliverableRepoLabel, want, cwd, show)
	}
}

// TestTaskSubmitNoRepoNonWorkTreeCwdStillQueues: TaskSubmit called from a
// NON-git cwd with no --repo must NOT abort (the internal auto-submit paths
// branch-scan/notify/dispatch run from odd cwds and must never be blocked) —
// the row is created with an EMPTY deliverable and a warning logged. This is
// the auto-internal face; the strict CLI/tool abort stays with the explicit
// --repo callers (their rows abort BEFORE the engine is reached).
func TestTaskSubmitNoRepoNonWorkTreeCwdStillQueues(t *testing.T) {
	testutil.NewTestDBPath(t)
	nonRepo := t.TempDir() // no git init
	t.Chdir(nonRepo)       // Go 1.24: chdir for the duration of the test

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "auto work from odd cwd"})
	if err != nil {
		t.Fatalf("TaskSubmit from a non-git cwd must NOT abort (auto paths), got: %v", err)
	}
	taskID := strings.Fields(out)[2]

	show, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(show, "deliverable") {
		t.Errorf("TaskShow must NOT render a deliverable block from a non-git cwd:\n%s", show)
	}
}

// TestTaskSubmitRepoExplicitOverridesCwd: the engine takes opts.Repo as the
// sole truth (cwd defaulting belongs to the callers) — a temp git repo far
// away from the test process's cwd must be the one stamped.
func TestTaskSubmitRepoExplicitOverridesCwd(t *testing.T) {
	testutil.NewTestDBPath(t)
	repo := t.TempDir()
	mustGitInit(t, repo)
	cwd, _ := os.Getwd() //stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback
	if cwd == repo {
		t.Skip("degenerate: cwd equals the test repo dir")
	}

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "explicit repo", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := strings.Fields(out)[2]
	show, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "deliverable.repo: "+repo) {
		t.Errorf("explicit repo not stamped (cwd leaked in?):\n%s", show)
	}
}
