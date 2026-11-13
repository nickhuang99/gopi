package main

// Manual-run delivery-time recycle E2E tests (epic t-1788397248): a task
// executed via `gopi task run` must recycle its worktree at close-out —
// the same resource-responsibility principle the worker loop follows. A
// task whose child worked in a linked worktree on a feature branch must,
// on terminal completion, have that worktree removed (server mode: branch
// PRESERVED as the merger's input).
//
// These tests drive the REAL binary through the FULL wrapper path (submit →
// task run → real child → mock LLM → terminal writeback → close-out
// recycle), then assert the worktree vanished while the branch survived.
// Zero real API calls (scripted mock LLM); sandboxed GOPI_HOME /
// GOPI_MEMORY_DB; the git world is a throwaway temp repo + worktree.
//
// The recycle resolves the branch from the child session's recorded
// workdir (taskWritebackBranch) and the worktree from the repo's linked
// worktree list keyed by that branch — see TaskFinishCleanupSessionID's
// server-mode arm (finishCleanupServerRecycle). A run on the MAIN checkout
// (branch master) has no linked worktree and correctly skips.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// live-CWD holder for the FOREIGN-holder E2E (Rule 7 zero-dep): the holder is
// THIS test binary re-exec'd, marker-gated to sit in the CWD its parent gave
// it — no shell, no external binary, no sleep-command assumptions (a shell
// running a single command may exec it, and `sh -c wait` exits at once).
const (
	recycleHolderEnvMarker = "GOPI_RECYCLE_E2E_CWD_HOLDER"
	recycleHolderChildArg  = "-test.run=TestHelperRecycleCwdHolder"
)

// recycleProcWaitAttempts / recycleProcWaitInterval bound the poll for a
// freshly started holder's procfs CWD (it is observable a moment after
// Start()); recycleHolderHoldTime is how long the holder blocks.
const (
	recycleProcWaitAttempts = 50
	recycleProcWaitInterval = 20 * time.Millisecond
	recycleHolderHoldTime   = 60 * time.Second
)

// TestHelperRecycleCwdHolder is not a test of its own: re-exec'd as the
// foreign holder. Without the marker it skips instantly, so a normal run is
// unaffected.
func TestHelperRecycleCwdHolder(t *testing.T) {
	if os.Getenv(recycleHolderEnvMarker) == "" {
		t.Skip("holder helper — only runs as the re-exec'd child a recycle E2E starts")
	}
	time.Sleep(recycleHolderHoldTime) //stylegate:allow-sleep the holder process must stay alive while the recycle check reads its cwd
}

// startRecycleHolder starts a FOREIGN process (outside the gopi run's own
// process tree — the incident's second live session) whose CWD is dir, waits
// until procfs shows that CWD, and returns its pid. Killed at cleanup.
func startRecycleHolder(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], recycleHolderChildArg)
	cmd.Dir = dir
	// The holder is a re-exec'd SUITE binary, so its TestMain would pin a
	// scratch root in the SHARED scratch home — and the reaper kills it before
	// its own TestMain can clean that root up, leaving one orphan
	// suite-cmd-agent-run-* directory per holder. Pinning the child's scratch
	// inside this test's fixture makes the creator own exactly what it created
	// (it is removed with t.TempDir()).
	cmd.Env = append(os.Environ(), recycleHolderEnvMarker+"=1",
		testutil.SuiteTempRootEnvName+"="+t.TempDir(),
		testutil.TempDirEnvName+"="+t.TempDir())
	mp := testutil.StartMockProcessCmd(t, cmd)
	pid := mp.Pid()
	for i := 0; i < recycleProcWaitAttempts; i++ {
		if target, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd")); err == nil && target != "" {
			if resolved, rerr := filepath.EvalSymlinks(dir); rerr == nil && resolved == target {
				return pid
			}
			if target == dir {
				return cmd.Process.Pid
			}
		}
		time.Sleep(recycleProcWaitInterval) //stylegate:allow-sleep the holder process must stay alive while the recycle check reads its cwd
	}
	t.Skipf("procfs CWD of holder pid %d unreadable — guard untestable on this host", pid)
	return 0
}

// e2eGitRepo is a throwaway git repo whose deliverable row a run can name.
func e2eGitRepo(t *testing.T) string {
	t.Helper()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	git(root, "init", repo)
	git(repo, "config", "user.email", "t@t")
	git(repo, "config", "user.name", "t")
	git(repo, "checkout", "-b", "master")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello"), 0o644)
	git(repo, "add", "a.txt")
	git(repo, "commit", "-m", "init")
	return repo
}

// e2eRecycleShape wires a manual-run scenario whose child works in a linked
// worktree on a feature branch: it returns the repo, the worktree dir, and
// the feature branch name. The test then runs `gopi task run` inside the
// worktree so the child session's workdir (and the writeback's branch
// backfill) point at the feature branch.
// TestManualRunRecyclesItsWorktree is the epic's acceptance (a): a manual
// `gopi task run` whose child executes in a linked worktree on a feature
// branch must remove that worktree at close-out and KEEP the branch (the
// merger's input). This drives the REAL binary + REAL child + mock LLM.
func TestManualRunRecyclesItsWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	feat := "feat/recycle-e2e"
	git(root, "init", repo)
	git(repo, "config", "user.email", "t@t")
	git(repo, "config", "user.name", "t")
	git(repo, "checkout", "-b", "master")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello"), 0o644)
	git(repo, "add", "a.txt")
	git(repo, "commit", "-m", "init")
	git(repo, "checkout", "-b", feat)
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("deliverable work"), 0o644)
	git(repo, "add", "b.txt")
	git(repo, "commit", "-m", "feature work")
	git(repo, "checkout", "master")
	git(repo, "worktree", "add", wt, feat)
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		t.Fatalf("worktree not created: %v", err)
	}

	// Sandboxed gopi env + mock LLM (no real API calls).
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "do the deliverable work — done"})
	defer mock.Close()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_AUTO_TASK_REGISTER=1",
	)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"recycle e2e", "--payload", "do the deliverable work", "--task-id", "01a11500-0000-7000-8000-000000000011",
		"--repo", repo},
		"", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	// The run executes with its CWD inside the worktree (the child session
	// workdir → branch backfill → feat/recycle-e2e).
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "01a11500-0000-7000-8000-000000000011"},
		"", env, wt)
	if code != 0 {
		t.Fatalf("task run must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	// Acceptance (a): THE GROUND THE ROW RECORDS IS GONE after the run.
	//
	// WHAT THIS ASSERTS AND WHY IT IS NOT `wt` (epic cmd-agent-e2e-seam
	// -stability, measured 2026-09-25). The epic's obligation is "a run
	// recycles the worktree IT held", and the row records exactly one: the
	// ground `gopi worker` created for the unit of work
	// (deliverable.worktree — `.../gopi-trecyclee2e`, on branch
	// `task/trecyclee2e`). The fixture ALSO creates `wt` beforehand on
	// `feat/recycle-e2e`, and no row ever names it: it is not the ground, so
	// no ledger-keyed arm can release it, and `gopi task run`'s release pass
	// does not go looking for directories a row does not own.
	//
	// The earlier form of this check asserted exactly that row-unowned
	// directory — so it was red on a tree where the epic's actual obligation
	// was being met, and the release line beside it named a REAL reason the
	// check never read ("kept: … — younger than the 5m0s floor — a concurrent
	// round may be creating it", i.e. the producer pass declined to touch a
	// seconds-old directory of a repo it does not own). Asserting the row's
	// recorded ground keeps the epic's meaning — a run must not leave its own
	// checkout registered — and makes the check read the artifact the row
	// itself names.
	show, _, _ := runGopiMapped(t, []string{"task", "show", "01a11500-0000-7000-8000-000000000011"}, "", env, "")
	ground := recycleRowRecordedWorktree(t, show)
	if ground == "" {
		t.Fatalf("the completed row records no deliverable.worktree — the recycle has no ground to be measured against:\n%s", show)
	}
	if _, err := os.Stat(ground); !os.IsNotExist(err) {
		// The child's captured output is the ONLY witness of WHY the release
		// did not happen (the reclaim lines and their refusals are printed to
		// the run's own stdout/stderr, which RunGopi captures). An assertion
		// that names the residue without the reason turns the next reader into
		// a re-runner of a 4-minute test.
		t.Errorf("manual run left ITS OWN recorded ground behind (%s exists)\nchild stdout:\n%s\nchild stderr:\n%s", ground, stdout, stderr)
	}
	if filepath.Clean(ground) == filepath.Clean(wt) {
		t.Errorf("the fixture's hand-made worktree %s IS the row's recorded ground — the two are supposed to be distinct "+
			"(the ground is the directory the worker created and the row records)", wt)
	}
	// Acceptance (b), and the reason this test found the defect at all: the
	// release must be VISIBLE, on the run's own output, as a release OF THIS
	// DIRECTORY. A green assertion on os.Stat alone says the directory is gone
	// without saying who removed it — and a directory that vanished for another
	// reason (a fixture that moved, an unrelated sweep) would read as a pass.
	// Measured RED on master f4451b9d: the run printed the KEPT sentence for
	// this very path ("younger than the 5m0s floor (3s)") while the directory
	// stayed — the defect this test exists for.
	//
	// Asserted on the PATH inside a release line rather than on either pass's
	// marker string, because the ground here can be freed by EITHER producer pass
	// (the ledger-keyed one names the branch, the ownership-independent one names
	// the directory) and the fact that matters is the same in both: the close-out
	// reported THIS directory released. The alternative — matching one marker —
	// would have this test fail whenever the two passes swap which of them
	// reaches the ground first, which is not a defect.
	if !strings.Contains(stdout+stderr, tools.ReclaimSelfOwnFreedPrefix+wt) {
		t.Errorf("the close-out must report the release of its own ground (%q not found)\nchild stdout:\n%s\nchild stderr:\n%s",
			tools.ReclaimSelfOwnFreedPrefix+wt, stdout, stderr)
	}
	// Branch PRESERVED server-mode: the merger's input must survive.
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", feat).Run(); err != nil {
		t.Errorf("branch %s must survive the manual-run recycle (merger input)", feat)
	}
	if rowBranch := recycleRowRecordedBranch(t, show); rowBranch != "" {
		if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", rowBranch).Run(); err != nil {
			t.Errorf("the row's own branch %s must survive the manual-run recycle (it is the merger's input)", rowBranch)
		}
	}
	// The wrapper row is terminal completed.
	if !strings.Contains(show, "completed") {
		t.Errorf("wrapper row must end completed:\n%s", show)
	}
}

// recycleRowRecordedWorktree / recycleRowRecordedBranch read the row's recorded
// deliverable out of `gopi task show` — the ONE surface an operator has, so the
// acceptance below measures the same artifact the ledger hands a reader (and the
// replay-source arm of the epic stays true to what was recorded).
func recycleRowRecordedWorktree(t *testing.T, show string) string {
	t.Helper()
	return recycleRowRecordedField(t, show, tools.TaskShowDeliverableWorktreeLabel)
}

func recycleRowRecordedBranch(t *testing.T, show string) string {
	t.Helper()
	return recycleRowRecordedField(t, show, tools.TaskShowDeliverableBranchLabel)
}

func recycleRowRecordedField(t *testing.T, show, label string) string {
	t.Helper()
	for _, ln := range strings.Split(show, "\n") {
		if !strings.HasPrefix(ln, label) {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(ln, label))
		if v == "" || v == "—" {
			return ""
		}
		return v
	}
	return ""
}

// TestManualRunMainCheckoutSkipsRecycle: a manual run whose child executes
// on the MAIN checkout (branch master — no linked worktree) must NOT be
// recycled (nothing to remove) and must complete normally — the no-op
// guard on the close-out recycle.
func TestManualRunMainCheckoutSkipsRecycle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := e2eGitRepo(t)
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "do the work on master — done"})
	defer mock.Close()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_AUTO_TASK_REGISTER=1",
	)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"master run e2e", "--payload", "do the work on master", "--task-id", "01a11500-0000-7000-8000-000000000012",
		"--repo", repo},
		"", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "01a11500-0000-7000-8000-000000000012"},
		"", env, repo)
	if code != 0 {
		t.Fatalf("task run must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	show, _, _ := runGopiMapped(t, []string{"task", "show", "01a11500-0000-7000-8000-000000000012"}, "", env, "")
	if !strings.Contains(show, "completed") {
		t.Errorf("wrapper row must end completed:\n%s", show)
	}
}

// TestManualRunKeepsWorktreeHeldByForeignProcess is the INCIDENT E2E
// (2026-09-10, row 01a088ef): the run's close-out must NOT recycle a worktree
// a DIFFERENT live process is sitting in — while still recycling its own (the
// test above). A foreign process is started in the worktree this test just
// created, then the real binary runs the same submit → task run → close-out
// path; at close-out the guard must skip, so the directory SURVIVES the run
// and the branch stays preserved (server mode). This is the end-to-end proof
// the unit-level skip tests cannot give: the skip decision is made by the
// real binary, inside the real run, with the real process table.
func TestManualRunKeepsWorktreeHeldByForeignProcess(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	feat := "feat/recycle-held-e2e"
	git(root, "init", repo)
	git(repo, "config", "user.email", "t@t")
	git(repo, "config", "user.name", "t")
	git(repo, "checkout", "-b", "master")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello"), 0o644)
	git(repo, "add", "a.txt")
	git(repo, "commit", "-m", "init")
	git(repo, "checkout", "-b", feat)
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("deliverable work"), 0o644)
	git(repo, "add", "b.txt")
	git(repo, "commit", "-m", "feature work")
	git(repo, "checkout", "master")
	git(repo, "worktree", "add", wt, feat)

	// The foreign holder: NOT this test's ancestor chain, NOT the gopi
	// child's — exactly the second-live-session shape the guard protects.
	holderPID := startRecycleHolder(t, wt)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "do the deliverable work — done"})
	defer mock.Close()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_AUTO_TASK_REGISTER=1",
	)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"recycle held e2e", "--payload", "do the deliverable work", "--task-id", "01a11500-0000-7000-8000-000000000013",
		"--repo", repo},
		"", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "01a11500-0000-7000-8000-000000000013"},
		"", env, wt)
	if code != 0 {
		t.Fatalf("task run must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	// The run completed (finish-cleanup is fail-soft, never fatal)...
	show, _, _ := runGopiMapped(t, []string{"task", "show", "01a11500-0000-7000-8000-000000000013"}, "", env, "")
	if !strings.Contains(show, "completed") {
		t.Errorf("wrapper row must end completed:\n%s", show)
	}
	// ...but the held worktree SURVIVED: the guard refused the removal.
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree held by pid %d must SURVIVE the close-out: %v", holderPID, err)
	}
	if out := git(repo, "worktree", "list"); !strings.Contains(out, wt) {
		t.Errorf("worktree must still be registered:\n%s", out)
	}
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", feat).Run(); err != nil {
		t.Errorf("branch must survive (merger input): %v", err)
	}
}
