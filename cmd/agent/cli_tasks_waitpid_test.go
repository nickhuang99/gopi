package main

// Serve-waitpid close-out E2E tests (epic t-1788384486, single-row variant
// task-single-row-exec): when a task-run CHILD dies abnormally (SIGKILL /
// non-zero exit), the PARENT (the `task run` wrapper — serve's direct child,
// or the worker) must close out the claimed wrapper row by its own action
// (cmd.Wait → writebackOutcome). task-single-row-exec REMOVED the auto
// mirror row — a task-run child no longer registers one (the human wrapper
// row is the single execution row), so the parent-side reconcile
// (TaskReconcileMirrorCloseout) is a defensive no-op for new runs. These
// tests drive the REAL binary and assert the SINGLE-ROW shape: wrapper row
// terminal + NO auto mirror row ever minted. Zero real API calls (scripted
// mock LLM); sandboxed GOPI_HOME/GOPI_MEMORY_DB.

import (
	"github.com/nick/gopi/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// childKillPollMax bounds how long the test waits for the child to spawn
// before giving up (Rule 1: named constant).
const childKillPollMax = 30 * time.Second

// childKillPollInterval paces the child-discovery polls.
const childKillPollInterval = 100 * time.Millisecond

// findChildPidByEnv scans /proc for a process whose environ contains the
// needle (e.g. GOPI_TASK_WRAPPER_ID=01a11500-0000-7000-8000-000000000026 — the env the wrapper
// exports to its child BEFORE spawn, cmd/agent/cli_tasks.go). Returns the
// pid, or 0 when not found. Reading /proc/<pid>/environ works for
// same-user processes without privileges.
func findChildPidByEnv(needle string) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		envPath := "/proc/" + e.Name() + "/environ"
		raw, err := os.ReadFile(envPath)
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), needle) {
			return pid
		}
	}
	return 0
}

// waitForChildPid polls for the wrapper's child process (identified by its
// exported GOPI_TASK_WRAPPER_ID env) until it appears or the deadline
// passes. Returns the pid, or 0 on timeout.
func waitForChildPid(needle string) int {
	deadline := time.Now().Add(childKillPollMax)
	for time.Now().Before(deadline) {
		if pid := findChildPidByEnv(needle); pid != 0 {
			return pid
		}
		time.Sleep(childKillPollInterval) //stylegate:allow-sleep poll interval while the waitpid child-kill path settles
	}
	return 0
}

// taskStatusOf reads the status WORD of a task row via TaskShow.
//
// IT ANSWERS THE WORD, NOT THE WHOLE FIELD — the `status:` line carries more
// than one reading. `task show` appends the render-time MEASURED holder facts
// to it (` + holder dead (pid N gone)`, internal/tools/taskholder_gone.go's
// taskHolderGoneMarkerFmt, plus its `recycled` wording), so the raw field is
// `in_progress + holder dead (pid 123 gone)` for a row whose executor is
// provably gone. A caller that compares that field against a status VALUE
// measures the marker logic as a side effect of its own assertion.
//
// Measured (this run, 2026-11-05): TestWrapperSilentFalseSuccessOnRefusedWriteback_RED
// went red on its third assertion — "the escalation must name what the ledger
// holds instead" — because the wrapper's escalation names the LEDGER's own
// answer (tools.TaskRowStatusAt → the bare word) while this helper returned the
// suffixed field; strings.Contains(escalation, "in_progress + holder dead …")
// can never be true. The failure was NOT load-sensitive and NOT a truncation:
// the isolated run is red in ~0.9 s. The split lives ONCE (Rule 14) in
// statusWord (cli_tasks_statusword_split_test.go), which its own regression table
// pins against every spelling the renderer emits, so this helper reuses it
// instead of carrying a second separator.
func taskStatusOf(t *testing.T, taskID string) string {
	t.Helper()
	show, err := tools.TaskShow(taskID)
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(show, "\n") {
		if strings.HasPrefix(ln, "status:") {
			return statusWord(strings.TrimSpace(strings.TrimPrefix(ln, "status:")))
		}
	}
	return ""
}

// autoRowCount counts ledger rows whose summary carries the [auto] prefix
// (the marker of harness-registered mirror/plain rows). task-single-row-exec:
// a task run must leave ZERO such rows — the human wrapper row is the only
// execution row.
func autoRowCount(t *testing.T) int {
	t.Helper()
	out, err := tools.TaskList("")
	if err != nil {
		return 0
	}
	return strings.Count(out, "[auto]")
}

// TestTaskRunChildSIGKILLClosesWrapperRowE2E: serve runs a task whose child
// is SIGKILLed mid-run → the wrapper row lands FAILED with the signal
// reason, and NO auto mirror row was ever minted (single-row exec). The mock
// holds the child alive with a slow bash sleep so the kill lands mid-run.
func TestTaskRunChildSIGKILLClosesWrapperRowE2E(t *testing.T) {
	// repo-context sandbox + a fixture repo for the ROW's deliverable: the
	// wrapper resolves the child's cwd from the row, and the round's
	// own-worktree-release pass unions that with the process cwd.
	SandboxRepoContext(t)
	childDir := RepoContextFixtureRepo(t)
	mock := testutil.NewMockLLM(t, testutil.Resp{ToolUse: &testutil.ToolCall{
		ID:   "call_sleep",
		Name: "bash",
		Args: map[string]interface{}{"command": "sleep 30", "timeout_sec": float64(60)},
	}})
	defer mock.Close()

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath) // parent-process tools.* reads hit the sandbox
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_AUTO_TASK_REGISTER=1",
	)

	// 1. submit the wrapper row — FROM THE FIXTURE REPO: the row's
	// deliverable repo is what the wrapper later hands the child as its cwd, so
	// a submit from the live repository would make the child execute there (the
	// hazard) even though this test process is sandboxed.
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"sigkill e2e", "--payload", "do the work", "--task-id", "01a11500-0000-7000-8000-000000000026"}, "", env, childDir); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	// 2. spawn `gopi task run` — the wrapper spawns the real child.
	bin := testutil.GopiBin(t)
	cmd := exec.Command(bin, "worker", "01a11500-0000-7000-8000-000000000026")
	cmd.Dir = childDir
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	// The wrapper is a long-lived daemon and it spawns the real run as a
	// grandchild: the pgid-scoped reaper takes the WHOLE group (wrapper +
	// whatever it forked) down on every exit path, including a panic.
	mp := testutil.StartMockProcessCmd(t, cmd)

	// 3. wait for the child (env needle: the wrapper exports
	//    GOPI_TASK_WRAPPER_ID to its child before spawn), then SIGKILL it.
	needle := tools.TaskWrapperEnv + "=01a11500-0000-7000-8000-000000000026"
	childPid := waitForChildPid(needle)
	if childPid == 0 {
		t.Fatalf("child gopi never appeared (env %s)", needle)
	}
	// The child must be mid-run (in_progress wrapper row) before the kill.
	deadline := time.Now().Add(childKillPollMax)
	for {
		if taskStatusOf(t, "01a11500-0000-7000-8000-000000000026") == "in_progress" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wrapper row never reached in_progress before the kill")
		}
		time.Sleep(childKillPollInterval) //stylegate:allow-sleep poll interval while the waitpid child-kill path settles
	}
	// No auto mirror row may exist even mid-run (single-row exec).
	if n := autoRowCount(t); n != 0 {
		t.Errorf("task run must not mint auto rows (single-row exec), found %d:\n%s", n, mustTaskList(t))
	}
	// childPid came from a /proc SCAN (`findChildPidByEnv`), so it is a number
	// the kernel may already have handed to somebody else by now. Read the
	// incarnation and deliver only to that one (Rule 42).
	childID, idErr := testutil.ReadProcIdentity(childPid)
	if idErr != nil {
		t.Fatalf("read the child's identity (pid %d): %v", childPid, idErr)
	}
	if err := testutil.KillIfIdentical(childID, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL child %d: %v", childPid, err)
	}

	// 4. the wrapper's wait returns (child died) → writeback FAILED. The
	//    handle shares ONE underlying wait with the reaper (a second
	//    cmd.Wait on the same *exec.Cmd would be an error and a race).
	waitCh := make(chan error, 1)
	go func() { waitCh <- mp.Wait() }()
	select {
	case <-waitCh:
	case <-time.After(childKillPollMax):
		t.Fatal("wrapper did not exit after child SIGKILL")
	}

	// 5. WRAPPER row: terminal failed, never in_progress.
	if st := taskStatusOf(t, "01a11500-0000-7000-8000-000000000026"); st != "failed" {
		t.Errorf("wrapper row status = %q, want failed (SIGKILL child)", st)
	}
	show, _ := tools.TaskShow("01a11500-0000-7000-8000-000000000026") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(show, "signal: killed") {
		t.Errorf("wrapper result must carry the signal reason:\n%s", show)
	}
	// 6. still no auto mirror row — the run left exactly one ledger row.
	if n := autoRowCount(t); n != 0 {
		t.Errorf("task run left %d auto row(s); want 0 (single-row exec):\n%s", n, mustTaskList(t))
	}
}

// mustTaskList renders the current ledger for failure diagnostics.
func mustTaskList(t *testing.T) string {
	t.Helper()
	out, err := tools.TaskList("")
	if err != nil {
		return "(list error: " + err.Error() + ")"
	}
	return out
}

// TestTaskRunChildNonZeroExitClosesWrapperRowE2E: the child exits non-zero
// on its own (a hard LLM error makes the agent loop fail and the child
// process exit 1) → wrapper row FAILED with the exit code, and no auto
// mirror row exists (single-row exec).
func TestTaskRunChildNonZeroExitClosesWrapperRowE2E(t *testing.T) {
	// repo-context sandbox: the wrapper child must not inherit this process's cwd
	// — the live repository — or its own-worktree-release pass would enumerate
	// (and free) the developer's worktrees. See repo_context_sandbox_test.go.
	//
	// The FIXTURE-REPO variant: the child runs the shipped `gopi task submit`,
	// whose CLI face refuses a non-git cwd (see SandboxRepoContextInFixtureRepo).
	SandboxRepoContextInFixtureRepo(t)
	mock := testutil.NewMockLLM(t, testutil.Resp{Fail: true})
	defer mock.Close()

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_AUTO_TASK_REGISTER=1",
	)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"nonzero e2e", "--payload", "do the work", "--task-id", "01a11500-0000-7000-8000-000000000027"}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "01a11500-0000-7000-8000-000000000027"}, "", env, "")
	if code == 0 {
		t.Fatalf("task run must exit non-zero on child failure\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if st := taskStatusOf(t, "01a11500-0000-7000-8000-000000000027"); st != "failed" {
		t.Errorf("wrapper row status = %q, want failed", st)
	}
	// The wrapper's record of the abnormal child exit is asserted on the
	// append-only AUDIT TRAIL, not on the row's current `result`. Since the
	// bounded retry (taskRunAbnormalChildRetry) the row is resumed-and-retried
	// AFTER the wrapper's own writeback, so the fresh attempt's outcome
	// overwrites `result` — while the wrapper's "gopi run failed: exit status
	// 1" line stays on the trail (`task show` renders only the LATEST event's
	// detail). Measured 2026-10-01: the row read
	// `resumed: ×1 … run_method: resume`, and the trail still carried the
	// wrapper's exit-status line.
	show, _ := tools.TaskShow("01a11500-0000-7000-8000-000000000027") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !taskEventDetailContains(t, "01a11500-0000-7000-8000-000000000027", "exit status 1") {
		t.Errorf("the wrapper's abnormal-child-exit record must carry the child exit code on the task_events trail:\n%s", show)
	}
	if n := autoRowCount(t); n != 0 {
		t.Errorf("task run left %d auto row(s); want 0 (single-row exec):\n%s", n, mustTaskList(t))
	}
}

// TestTaskRunNormalExitSingleRowE2E: a NORMAL run — the child completes,
// the wrapper writeback closes the single human row completed, and no auto
// mirror row was ever minted.
func TestTaskRunNormalExitSingleRowE2E(t *testing.T) {
	// repo-context sandbox: the wrapper child must not inherit this process's cwd
	// — the live repository — or its own-worktree-release pass would enumerate
	// (and free) the developer's worktrees. See repo_context_sandbox_test.go.
	//
	// The FIXTURE-REPO variant: the child runs the shipped `gopi task submit`,
	// whose CLI face refuses a non-git cwd (see SandboxRepoContextInFixtureRepo).
	SandboxRepoContextInFixtureRepo(t)
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	work := t.TempDir() // child cwd — the write tool must land HERE, not in the repo
	// The mocked write target is ABSOLUTE and lives in this test's own
	// scratch tree. A relative path in a mocked tool call is resolved against
	// the cwd of the process that EXECUTES the tool, and for `go test` that is
	// the test binary's cwd — the PACKAGE DIRECTORY. A relative "out.txt"
	// therefore dropped an untracked cmd/agent/out.txt into every checkout
	// that ran this package (git status: "?? cmd/agent/out.txt", contents
	// "done"). An absolute path makes the leak impossible by construction and
	// lets the test actually ASSERT the write landed (nothing did before).
	// The whole class is pinned by the static guard
	// TestWriteToolPathsInPackageTestsAreAbsolute below.
	outPath := filepath.Join(work, writeToolOutName)
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_write",
			Name: toolnames.ToolWrite,
			Args: map[string]interface{}{writeToolPathArg: outPath, writeToolContentArg: "done"},
		}},
		testutil.Resp{Text: "Task complete. All work is finished and verified."},
	)
	defer mock.Close()

	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_AUTO_TASK_REGISTER=1",
	)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"normal e2e", "--payload", "do the work", "--task-id", "01a11500-0000-7000-8000-000000000028"}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "01a11500-0000-7000-8000-000000000028"}, "", env, work)
	if code != 0 {
		t.Fatalf("normal run must exit 0\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	// The write tool ran in the CHILD, under the test's scratch dir: assert
	// the artifact is there (the run really exercised the tool) and that no
	// copy escaped into this package's directory (the leak this test used to
	// cause).
	if b, err := os.ReadFile(outPath); err != nil {
		t.Errorf("write tool artifact missing at %s: %v", outPath, err)
	} else if strings.TrimSpace(string(b)) != "done" {
		t.Errorf("%s content = %q, want %q", outPath, string(b), "done")
	}
	if _, err := os.Stat(filepath.Join(packageTestDir(t), writeToolOutName)); err == nil {
		t.Errorf("the run leaked %s into %s — the write path must be absolute",
			writeToolOutName, packageTestDir(t))
	}
	if st := taskStatusOf(t, "01a11500-0000-7000-8000-000000000028"); st != "completed" {
		t.Errorf("wrapper row status = %q, want completed", st)
	}
	show, _ := tools.TaskShow("01a11500-0000-7000-8000-000000000028") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(show, "session_id:") {
		t.Errorf("wrapper row must carry the child session_id:\n%s", show)
	}
	if n := autoRowCount(t); n != 0 {
		t.Errorf("task run left %d auto row(s); want 0 (single-row exec):\n%s", n, mustTaskList(t))
	}
}
