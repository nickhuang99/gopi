// Deliverable.branch backfill regression tests (bugfix epic 2026-09-02 —
// "adoption merge gate never fires"): a completed task run must leave
// deliverable.branch set, and AdoptionSweep must then adopt the branch
// (probe-merge lands, master advances). Real git in t.TempDir (Rule 4 最小
// 样本 — the chain IS git) + a real child session JSONL whose
// session_start.workdir points at the branch's worktree.
package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// branchBackfillFixture builds the full single-node shape:
//
//	repo (master root commit) + linked worktree checked out on feat/x
//
// Returns the repo dir and the worktree path.
func branchBackfillFixture(t *testing.T) (repo, wt string) {
	t.Helper()
	repo = t.TempDir()
	git := func(dir string, args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git(repo, "init", "-b", "master")
	git(repo, "config", "user.email", "test@gopi")
	git(repo, "config", "user.name", "test")
	git(repo, "commit", "--allow-empty", "-m", "root")
	// A commit on the branch so the probe merge is not a no-op.
	wt = filepath.Join(t.TempDir(), "wt")
	git(repo, "worktree", "add", "-b", "feat/backfill-e2e", wt, "master")
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(wt, "add", "work.txt")
	git(wt, "commit", "-qm", "feat: backfill e2e work")
	return repo, wt
}

// branchBackfillSeedSession writes a child session JSONL whose
// session_start.workdir is the worktree, into the gopi sessions dir.
func branchBackfillSeedSession(t *testing.T, workdir, model string) string {
	t.Helper()
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	// The gopi root must be named here: GOPI_HOME (pinned at the suite's
	// sandbox by testutil.PinSuiteGopiProfile) wins over HOME for
	// config.GopiDir, so HOME alone would not point the lookup at this fixture.
	t.Setenv(config.GopiDirEnv, sessHome)
	sessDir := filepath.Join(sessHome, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessID := "child-e2e-1"
	line := `{"type":"session_start","sessionId":"child-e2e-1","model":"` + model +
		`","timestamp":"x","workdir":"` + workdir + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(sessDir, sessID+".jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	return sessID
}

// TestDeliverableBranchBackfilledAtWriteback: a task submitted with a repo,
// executed in a worktree branch (the child session's workdir), written back
// completed → the row's deliverable carries branch=<worktree branch>. The
// acceptance criterion ①: "A completed task run leaves deliverable.branch
// set".
func TestDeliverableBranchBackfilledAtWriteback(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := branchBackfillFixture(t)

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "backfill e2e work", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam: %v", err)
	}
	// The child ran inside the worktree (PROJECT.md Rule 6a) — its session
	// records the worktree as workdir. The writeback (parent) resolves the
	// branch from that session.
	sessID := branchBackfillSeedSession(t, wt, "glm-exec")
	t.Setenv(TaskChildSessionEnv, sessID)

	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done — run: elapsed=1s"); err != nil {
		t.Fatalf("TaskRunWriteback: %v", err)
	}

	// ① A completed task run leaves deliverable.branch set.
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var deliv string
	if err := db.QueryRow(`SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&deliv); err != nil {
		t.Fatalf("read deliverable: %v", err)
	}
	branch, err := taskDeliverableBranch(deliv)
	if err != nil {
		t.Fatalf("deliverable parse: %q: %v", deliv, err)
	}
	if branch != "feat/backfill-e2e" {
		t.Fatalf("deliverable.branch = %q, want feat/backfill-e2e (deliverable=%q)", branch, deliv)
	}
	// repo must survive the merge (backfill merges, never replaces).
	var d struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal([]byte(deliv), &d); err != nil || d.Repo != repo {
		t.Fatalf("deliverable.repo lost: %q err=%v", deliv, err)
	}
}

// TestDeliverableBranchWritebackNoBranchWhenUnresolvable: a writeback with
// NO child session (or a non-repo workdir) must leave the deliverable
// untouched — the backfill is env-gated best-effort, never a cwd guess.
func TestDeliverableBranchWritebackNoBranchWhenUnresolvable(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := t.TempDir()
	mustGitInit(t, repo)
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "sessionless work", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam: %v", err)
	}
	os.Unsetenv(TaskChildSessionEnv)
	os.Unsetenv(TaskExecutorSessionEnv)
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("TaskRunWriteback: %v", err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var deliv string
	if err := db.QueryRow(`SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&deliv); err != nil {
		t.Fatalf("read deliverable: %v", err)
	}
	if deliv != `{"repo":"`+repo+`"}` {
		t.Errorf("deliverable mutated without a resolvable session: %q", deliv)
	}
}

// TestDeliverableBranchFailedRunNeverBackfills: a FAILED run must not pair a
// branch (a failed branch holds no adoptable delivery).
func TestDeliverableBranchFailedRunNeverBackfills(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := branchBackfillFixture(t)
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "doomed work", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam: %v", err)
	}
	sessID := branchBackfillSeedSession(t, wt, "glm-exec")
	t.Setenv(TaskChildSessionEnv, sessID)
	if _, err := TaskRunWriteback(taskID, TaskStatusFailed, "gopi run failed: boom"); err != nil {
		t.Fatalf("TaskRunWriteback: %v", err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var deliv string
	if err := db.QueryRow(`SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&deliv); err != nil {
		t.Fatalf("read deliverable: %v", err)
	}
	if strings.Contains(deliv, "branch") {
		t.Errorf("failed run must not backfill branch: %q", deliv)
	}
}

// TestDeliverableBranchBackfillClosesAdoptionLoop — acceptance criterion ②:
// once the completed task's deliverable carries the branch, AdoptionSweep
// adopts it (probe-merge lands on master, branch merged + pruned). This is
// the END-TO-END shape the Epic's fix must produce: writeback backfill →
// next adoption tick adopts.
func TestDeliverableBranchBackfillClosesAdoptionLoop(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := branchBackfillFixture(t)

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "adopt-loop work", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam: %v", err)
	}
	sessID := branchBackfillSeedSession(t, wt, "glm-exec")
	t.Setenv(TaskChildSessionEnv, sessID)
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done — run: elapsed=1s"); err != nil {
		t.Fatalf("TaskRunWriteback: %v", err)
	}

	counts, err := AdoptionSweep(context.Background(), AdoptionOptions{Repo: repo, Submit: true})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(counts.Adopted) != 1 || counts.Adopted[0] != "feat/backfill-e2e" {
		t.Fatalf("want feat/backfill-e2e adopted, got %+v (failed=%v)", counts, counts.Failed)
	}
	// master advanced: the merge commit is on master.
	out2, err := adoptionGit(repo, "log", "--oneline", "-1")
	if err != nil || !strings.Contains(out2, "adoption: merge feat/backfill-e2e") {
		t.Fatalf("adoption merge commit missing: %q err=%v", out2, err)
	}
}

// branchBackfillSeedSessionWithWorktreeAdd writes a child session JSONL that
// mirrors the 2026-09-09 failing shape (t-1788813641 / 2ab6c869): the child
// LAUNCHED from the MAIN repo (session_start.workdir = repo, master checked
// out — Rule 6a solo shape) and created its own linked worktree mid-session
// via an inline bash `git worktree add ... -b <branch>`. Returns the session
// id. cmd is the exact bash command line recorded as a tool_use; pass ""
// to write a session with NO worktree-add (writeback must then leave the
// deliverable branch-less — master exemption preserved).
func branchBackfillSeedSessionWithWorktreeAdd(t *testing.T, workdir, model, cmd string) string {
	t.Helper()
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	// The gopi root must be named here: GOPI_HOME (pinned at the suite's
	// sandbox by testutil.PinSuiteGopiProfile) wins over HOME for
	// config.GopiDir, so HOME alone would not point the lookup at this fixture.
	t.Setenv(config.GopiDirEnv, sessHome)
	sessDir := filepath.Join(sessHome, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessID := "child-master-launch-1"
	var sb strings.Builder
	sb.WriteString(`{"type":"session_start","sessionId":"child-master-launch-1","model":"` + model +
		`","timestamp":"x","workdir":"` + workdir + `"}` + "\n")
	if cmd != "" {
		enc, _ := json.Marshal(cmd) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		sb.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"call_0","name":"bash","input":{"command":` + string(enc) + `}}]},"timestamp":"x"}` + "\n")
	}
	if err := os.WriteFile(filepath.Join(sessDir, sessID+".jsonl"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return sessID
}

// TestDeliverableBranchMasterLaunchWorktreeAddBackfill — the 2026-09-09
// repro: the executor launches from the MAIN repo (master checked out,
// session_start.workdir = repo) and creates its OWN linked worktree for the
// delivery (Rule 6a solo shape — t-1788813641). The writeback must NOT
// exempt away the branch: it resolves the branch from the session's
// `git worktree add ... -b feat/x` creation moment and backfills it.
func TestDeliverableBranchMasterLaunchWorktreeAddBackfill(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := branchBackfillFixture(t)
	// The main repo is on master (launch dir) and the child's session records
	// THAT dir — the worktree was created mid-session by the command below
	// (branch feat/backfill-e2e, which the fixture actually created on the
	// repo, so the writeback's existence-verify passes).
	cmd := "cd " + repo + " && git worktree add " + wt + " -b feat/backfill-e2e && cd " + wt + " && git log --oneline -1"

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "main-launch worktree work", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam: %v", err)
	}
	// The child launched in the MAIN repo (branch master) — this is the bug
	// shape: session_start.workdir = repo, not the worktree.
	sessID := branchBackfillSeedSessionWithWorktreeAdd(t, repo, "glm-exec", cmd)
	t.Setenv(TaskChildSessionEnv, sessID)
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done — run: elapsed=1s"); err != nil {
		t.Fatalf("TaskRunWriteback: %v", err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var deliv string
	if err := db.QueryRow(`SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&deliv); err != nil {
		t.Fatalf("read deliverable: %v", err)
	}
	branch, err := taskDeliverableBranch(deliv)
	if err != nil {
		t.Fatalf("deliverable parse: %q: %v", deliv, err)
	}
	if branch != "feat/backfill-e2e" {
		t.Fatalf("deliverable.branch = %q, want feat/backfill-e2e (the worktree-add branch, NOT master — deliverable=%q)", branch, deliv)
	}
}

// TestDeliverableBranchMasterLaunchNoWorktreeNoBackfill — master exemption
// preserved: the executor launches from the MAIN repo (master) and its
// session contains NO worktree-add (true master work — e.g. a config/docs
// change committed straight to master). The writeback must leave the
// deliverable branch-less (no adoptable branch, never mislabeled).
func TestDeliverableBranchMasterLaunchNoWorktreeNoBackfill(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := t.TempDir()
	mustGitInit(t, repo)
	// Master must have a commit for branch --show-current to resolve.
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("config", "user.email", "test@gopi")
	git("config", "user.name", "test")
	git("commit", "--allow-empty", "-m", "root")

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "master direct work", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam: %v", err)
	}
	sessID := branchBackfillSeedSessionWithWorktreeAdd(t, repo, "glm-exec", "")
	t.Setenv(TaskChildSessionEnv, sessID)
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("TaskRunWriteback: %v", err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var deliv string
	if err := db.QueryRow(`SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&deliv); err != nil {
		t.Fatalf("read deliverable: %v", err)
	}
	if strings.Contains(deliv, "branch") {
		t.Errorf("master work with no worktree-add must not backfill a branch: %q", deliv)
	}
}

// TestWorktreeAddBranchFromCommand — the JSONL-command parser unit test:
// both argument orders (path-then-branch and branch-then-path) and the
// `-C repo` form must yield the branch; a branch-name tail glued to shell
// syntax (`feat/x && ...`) must still parse; non-add commands yield "".
func TestWorktreeAddBranchFromCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want string
	}{
		{"cd /r && git worktree add /w -b feat/prompt-review && cd /w && git log -1", "feat/prompt-review"},
		{"cd /r && git worktree add -b feat/prompt-review /w 2>&1 | tail -1", "feat/prompt-review"},
		{"git -C /r worktree add /w -B fix/rewrite && echo ok", "fix/rewrite"},
		{"git worktree add /w --track -b feat/x", "feat/x"},
		{"git worktree add /w", ""},               // no new branch — no claim
		{"git worktree prune", ""},                // not an add
		{"git commit -m 'worktree add docs'", ""}, // word salad — no add verb
		{"git worktree list", ""},                 // not an add
	}
	for _, c := range cases {
		got := taskWorktreeAddBranchFromCommand(c.cmd)
		if got != c.want {
			t.Errorf("taskWorktreeAddBranchFromCommand(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}
