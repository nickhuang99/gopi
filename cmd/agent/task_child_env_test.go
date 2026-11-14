package main

// task_child_env_test.go — the SEAM-level half of the daemon-context-isolation
// epic: the two decisions the executor spawn takes (which cwd, whose
// environment) are pure functions of the launch context and the ledger row, so
// both can be falsified hermetically — no model, no binary, no timing.
//
// The e2e companion (task_executor_repo_cwd_e2e_test.go) proves the two halves
// MEET; these tests pin the decisions themselves, including the cases the e2e
// cannot reach (a row with no repo, a row whose repo is gone, an environment
// that carries none of the names).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestStripDaemonContextEnvRemovesTheSpawnersOwnHandle is Fix B's unit guard:
// the strip removes EXACTLY the execution-context names and nothing else.
func TestStripDaemonContextEnvRemovesTheSpawnersOwnHandle(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		tools.TaskExecutorSessionEnv + "=session-of-the-spawner",
		config.AgentSessionAncestorEnv + "=12345",
		"HERMES_SESSION_ID=hermes-1",
		tools.TaskWrapperEnv + "=t-wrapper",
		tools.TaskWorkdirEnv + "=/tmp/wd",
		"GOPI_HOME=/tmp/home",
	}
	stripped, removed := stripDaemonContextEnv(env)
	for _, kv := range stripped {
		name, _, _ := strings.Cut(kv, "=")
		if isDaemonContextEnv(name) {
			t.Errorf("%s survived the strip", name)
		}
	}
	// The wrapper identity, the workdir handoff and the profile MUST survive:
	// they are the child's contract, not the spawner's context.
	for _, keep := range []string{"PATH=/usr/bin", tools.TaskWrapperEnv + "=t-wrapper",
		tools.TaskWorkdirEnv + "=/tmp/wd", "GOPI_HOME=/tmp/home", "HERMES_SESSION_ID=hermes-1"} {
		if !containsKV(stripped, keep) {
			t.Errorf("the strip must keep %q (it is the child's own contract); got %v", keep, stripped)
		}
	}
	if len(removed) != len(tools.DaemonContextEnvVars) {
		t.Fatalf("the strip must report every name it removed: want %d (%v), got %v",
			len(tools.DaemonContextEnvVars), tools.DaemonContextEnvVars, removed)
	}
	// A NAME match, never a prefix match: a variable that merely STARTS with
	// one of the names is a different variable.
	if _, removed := stripDaemonContextEnv([]string{tools.TaskExecutorSessionEnv + "_EXTRA=x"}); len(removed) != 0 {
		t.Errorf("a prefix-adjacent variable must not be stripped: %v", removed)
	}
	// Presence, not truthiness: an EMPTY handle is still a presence.
	if _, removed := stripDaemonContextEnv([]string{tools.TaskExecutorSessionEnv + "="}); len(removed) != 1 {
		t.Errorf("an empty-valued handle is still the spawner's context and must be stripped; got %v", removed)
	}
}

// TestDaemonEnvLeakVarsMirrorTools pins the ONE mirror in this design:
// internal/config cannot import internal/tools (tools imports config), so the
// guard list the e2e asserts on is a second declaration — a drift would make a
// leak invisible to the tests that guard it. Rule 14 forbids silent mirrors;
// this test makes the mirror loud.
func TestDaemonEnvLeakVarsMirrorTools(t *testing.T) {
	if len(config.DaemonEnvLeakVars) != len(tools.DaemonContextEnvVars) {
		t.Fatalf("config.DaemonEnvLeakVars %v and tools.DaemonContextEnvVars %v have different sizes — one was extended without the other",
			config.DaemonEnvLeakVars, tools.DaemonContextEnvVars)
	}
	for _, name := range tools.DaemonContextEnvVars {
		found := false
		for _, other := range config.DaemonEnvLeakVars {
			if other == name {
				found = true
			}
		}
		if !found {
			t.Errorf("tools.DaemonContextEnvVars carries %q but config.DaemonEnvLeakVars does not — the spawn seam strips one list and the tests assert on the other", name)
		}
	}
}

// TestTaskExecutorCwdPrefersTheTasksRepoOverTheLaunchDir is Fix A's unit
// guard, on the three shapes that matter: a row with a repo, a row whose repo
// is gone, and a row with no repo at all.
func TestTaskExecutorCwdPrefersTheTasksRepoOverTheLaunchDir(t *testing.T) {
	launch := t.TempDir()
	taskRepo := executorCwdProbeRepo(t, "unit-owned-project")
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_HOME", gopiHome)
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	const withRepo = "t-exec-cwd-with-repo"
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "cwd resolution", Payload: "p", TaskID: withRepo, Repo: taskRepo, Source: "auto",
	}); err != nil {
		t.Fatalf("submit row with repo: %v", err)
	}
	got, src := taskExecutorCwd(withRepo, launch)
	if got != taskRepo || src != taskCwdSourceRepo {
		t.Fatalf("a spawner standing ELSEWHERE must send the child into the task's repo (the child's GOPI.md/search roots/guardrails belong to the task's project): got (%q, %q), want (%q, %q)",
			got, src, taskRepo, taskCwdSourceRepo)
	}

	// The GROUND cases: when the spawner ALREADY stands in the task's own
	// repo — or in a directory inside it, which is the task's linked worktree
	// — that directory is KEPT. It is the same project contract AND the
	// branch authority the deliverable backfill reads; rewriting it to the
	// row's repo would hand the child the repo's checked-out branch (master,
	// exempt from backfill), leaving the row without a deliverable branch and
	// starving the merge leg. Regression caught by
	// TestWorkerServerDeliveryRecyclesThenMergerMerges.
	if got, src := taskExecutorCwd(withRepo, taskRepo); got != taskRepo || src != taskCwdSourceTaskGround {
		t.Errorf("a spawner already standing in the task's repo must keep it: got (%q, %q), want (%q, %q)",
			got, src, taskRepo, taskCwdSourceTaskGround)
	}
	worktree := filepath.Join(taskRepo, "wt-linked")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree stand-in: %v", err)
	}
	if got, src := taskExecutorCwd(withRepo, worktree); got != worktree || src != taskCwdSourceTaskGround {
		t.Errorf("a spawner standing INSIDE the task's repo (its linked worktree) must keep that directory — the task's branch lives there: got (%q, %q), want (%q, %q)",
			got, src, worktree, taskCwdSourceTaskGround)
	}

	// The regression's exact shape: the task's LINKED WORKTREE is a SIBLING of
	// the repo (every `git worktree add ~/workspace/gopi-x` does this), so
	// containment cannot recognize it — the shared git repository can, and it
	// must. A worker launched inside that worktree keeps it; rewriting the cwd
	// to the row's repo handed the child master, whose branch is exempt from
	// the deliverable backfill, and the merge leg starved
	// (TestWorkerServerDeliveryRecyclesThenMergerMerges is the end-to-end arm).
	sibling := filepath.Join(filepath.Dir(taskRepo), "wt-sibling")
	chainGit(t, taskRepo, "worktree", "add", "-b", "wt-sibling-branch", sibling)
	if got, src := taskExecutorCwd(withRepo, sibling); got != sibling || src != taskCwdSourceTaskGround {
		t.Errorf("a spawner in the task's SIBLING linked worktree must keep it (same git repository = same project ground): got (%q, %q), want (%q, %q)",
			got, src, sibling, taskCwdSourceTaskGround)
	}

	// A row with NO usable repo: the pre-epic behaviour survives, and the
	// caller is told which one it got. The submit path stamps the cwd as the
	// default repo when --repo is absent (epic task-submit-repo-default), so
	// this row must be submitted from a directory that is NOT a git work tree
	// — which is also exactly the shape that produces a repo-less row in
	// production.
	const noRepo = "t-exec-cwd-no-repo"
	nonGit := t.TempDir()
	t.Chdir(nonGit)
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "cwd fallback", Payload: "p", TaskID: noRepo, Source: "auto",
	}); err != nil {
		t.Fatalf("submit row without repo: %v", err)
	}
	if repo := tools.TaskRepoFor(noRepo); repo != "" {
		t.Fatalf("fixture bug: the repo-less row carries %q — the submit path defaulted the cwd (%s)", repo, nonGit)
	}
	got, src = taskExecutorCwd(noRepo, launch)
	if got != launch || src != taskCwdSourceLaunch {
		t.Fatalf("a row with no deliverable.repo must keep the launch workdir and SAY so: got (%q, %q), want (%q, %q)",
			got, src, launch, taskCwdSourceLaunch)
	}

	// A row whose recorded repo has since disappeared: the fallback must
	// engage rather than chdir the child into nothing.
	gone := executorCwdProbeRepo(t, "unit-vanishing-repo")
	const vanished = "t-exec-cwd-vanished-repo"
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "cwd vanished", Payload: "p", TaskID: vanished, Repo: gone, Source: "auto",
	}); err != nil {
		t.Fatalf("submit row with vanishing repo: %v", err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("remove repo: %v", err)
	}
	got, src = taskExecutorCwd(vanished, launch)
	if got != launch || src != taskCwdSourceLaunch {
		t.Fatalf("a row whose repo is gone must fall back to the launch workdir (never a chdir into a missing directory): got (%q, %q)", got, src)
	}
}

// TestTaskEnvStripLineReportsBothOutcomes pins the Rule 15 half: the strip is
// never silent — an operator reading a worker log can tell WHICH outcome
// happened, and the names of what was removed are in the record.
func TestTaskEnvStripLineReportsBothOutcomes(t *testing.T) {
	clean := taskEnvStripLine(nil, "t-x")
	if !strings.Contains(clean, "already free") {
		t.Errorf("a clean environment must still be reported (otherwise 'nothing leaked' and 'nothing was checked' look identical): %q", clean)
	}
	line := taskEnvStripLine([]string{tools.TaskExecutorSessionEnv}, "t-x")
	if !strings.Contains(line, tools.TaskExecutorSessionEnv) || !strings.Contains(line, "t-x") {
		t.Errorf("the strip record must name the removed variable and the task it was kept from; got %q", line)
	}
}

// containsKV reports whether env holds kv verbatim.
func containsKV(env []string, kv string) bool {
	for _, got := range env {
		if got == kv {
			return true
		}
	}
	return false
}
