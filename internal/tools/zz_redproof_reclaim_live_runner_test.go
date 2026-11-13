package tools

// RED PROOF for the 2026-09-21/22 incident (rows 01a0c574 / 01a0c545): a
// task-self-reclaim pass freed a worktree whose run was still alive.
//
// This file is deliberately written against the PRE-FIX API ONLY — no field
// and no helper added by the fix — so the very same file compiles and runs on
// both trees:
//
//	pre-fix tree (master bf36bf69): FAILS — the row's own recorded runner
//	  identity (proc_pid + proc_start_ticks + proc_cmd) is still a live
//	  process, yet `reclaimLiveSessionRefusal` returns "" (it returns "" for a
//	  TERMINAL status before it ever looks at the identity), so the pass frees
//	  the directory the live run is working in.
//	post-fix tree: PASSES — the live-run veto is evaluated per row on the
//	  shared identity triple, and the pass refuses BY NAME.
//
// Reproduce:
//
//	git worktree add --detach /tmp/gopi-redproof bf36bf69
//	cp internal/tools/zz_redproof_reclaim_live_runner_test.go /tmp/gopi-redproof/internal/tools/
//	cd /tmp/gopi-redproof && go test -run TestZZRedProofSelfReclaimKeepsALiveRunnersGround ./internal/tools/

import (
	"context"
	"github.com/nick/gopi/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// zzRedProofFixture builds a repo + linked worktree on a feat branch and a row
// that (a) this process holds, and (b) records a LIVE runner identity in its
// own proc_pid/proc_start_ticks/proc_cmd columns — the exact shape the ledger
// had for 01a0c574 (the wrapper had already written the row's terminal status
// while its run was still standing in the directory).
func zzRedProofFixture(t *testing.T, taskID, status string) (repo, wt, branch string) {
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
	branch = "feat/redproof-live-runner"
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
	gitRun(t, repo, "checkout", "master")
	gitRun(t, repo, finishCleanupWorktreeVerb, "add", wt, branch)

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "redproof live runner", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// A REAL live process, never a forged pid: the point of this proof is that
	// the row's recorded runner is demonstrably alive when the pass runs.
	// Through the shared helper: Pdeathsig keeps the stand-in from outliving
	// this test BINARY (a t.Cleanup does not run on SIGKILL or on go test's
	// -timeout exit) and its cwd is kept out of the checkout.
	cmd := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds).Cmd()
	start, ok := procStartTicks(cmd.Process.Pid)
	if !ok {
		t.Skipf("cannot read start ticks for pid %d", cmd.Process.Pid)
	}
	ident, ok := taskCaptureProcIdentity(cmd.Process.Pid)
	if !ok {
		t.Skip("cannot capture the runner identity")
	}

	stamp := TaskClaimOwner("")
	if stamp == "" {
		t.Skip("no hostname on this machine — the self-claim shape is untestable here")
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+` = ?,
		 updated_at = ?, deliverable = ?, `+tasksProcPidColumnName+` = ?,
		 `+tasksProcStartColumnName+` = ?, `+tasksProcCmdColumnName+` = ? WHERE task_id = ?`,
		status, stamp, 1000.0, 2000.0, 2000.0,
		`{"repo":"`+repo+`","branch":"`+branch+`","worktree":"`+wt+`"}`,
		ident.Pid, start, ident.Cmd, taskID); err != nil {
		t.Fatalf("seed the live-runner row: %v", err)
	}
	return repo, wt, branch
}

// TestZZRedProofSelfReclaimKeepsALiveRunnersGround is the measured defect: the
// pass must NOT free a directory whose recorded runner is still running, and
// the refusal must NAME that fact.
//
// The row status is `completed` on purpose — that is the shape that beat the
// existing guard (reclaimLiveSessionRefusal short-circuits on a terminal
// status), and it is the shape the wrapper produces when it writes the row's
// terminal state while the run it launched is still standing.
func TestZZRedProofSelfReclaimKeepsALiveRunnersGround(t *testing.T) {
	repo, wt, branch := zzRedProofFixture(t, "t-redproof-live", taskStatusCompleted)

	if !pidAlive(zzRedProofLastPid(t, "t-redproof-live")) {
		t.Fatalf("fixture: the recorded runner must be alive")
	}

	lines := ReclaimOwnFinishedWorktrees()
	joined := strings.Join(lines, "\n")

	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the live run's ground was FREED (directory gone, stat err=%v) — this is the red proof; the pass reported: %v", err, lines)
	}
	if !worktreeListed(t, repo, wt) {
		t.Fatalf("the live run's registration was dropped: %s", worktreeList(t, repo))
	}
	if !strings.Contains(joined, reclaimSelfRefusedMarker) {
		t.Errorf("the refusal must be NAMED, got %v", lines)
	}
	if !strings.Contains(joined, "t-redproof-live") {
		t.Errorf("the refusal must name the row, got %v", lines)
	}
	if !strings.Contains(joined, "still") && !strings.Contains(joined, "live") {
		t.Errorf("the refusal must say WHY (the run is still live), got %v", lines)
	}
	if err := gitBranchExists(t, repo, branch); err != nil {
		t.Errorf("the branch must be untouched: %v", err)
	}
}

// zzRedProofLastPid reads the row's recorded runner pid back through the
// PRE-FIX API surface only (a plain SELECT), so this file compiles unchanged
// on both trees.
func zzRedProofLastPid(t *testing.T, taskID string) int {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var pid int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(`+tasksProcPidColumnName+`,0) FROM tasks WHERE task_id = ?`, taskID).Scan(&pid); err != nil {
		t.Fatalf("read runner pid: %v", err)
	}
	return pid
}
