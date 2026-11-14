package main

// Serve/worker-split acceptance tests (epic 2026-09-03, nick's final
// design): serve → merger (a PURE merge pass — server-only, branch merge
// + conflict resolve) and worker → the general task-management loop (runs
// on BOTH client and server). The core acceptance invariants pinned here:
//
//	(a) merger with a pending task does NOT run it — the row survives a
//	    merger --once pass untouched (task execution is the worker's job);
//	(b) worker long-running face: a drained queue is WAITED OUT and
//	    re-checked — a task submitted during the wait is picked up by the
//	    SAME worker process (no restart needed);
//	(c) --once preserves drain-exit (exit 0 when the queue drains).
//
// The claim/run machinery itself is covered by the existing single-node
// tests (TestRunWorkerClaimLocalOnlyWithNoPeers etc.) — this file pins the
// SPLIT: which process owns which pass.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestWorkerWaitsAndPicksUpNewTaskWithoutRestart (acceptance b): the
// long-running worker face. Task t-worker-wait-1 is claimed and executed
// first; while the worker is WAITING on the drained queue (short idle via
// GOPI_WORKER_IDLE_SEC=1), a goroutine submits t-worker-wait-2 — the SAME
// worker process must pick it up on its next re-check (no restart). The
// worker is bounded by GOPI_WORKER_MAX_ROUNDS so the test terminates: the
// loop's own round cap ends it after both tasks ran.
//
// Both rows execute through the hermetic child-failure escape
// (taskChildFailTestEnv=1) — the child "runs" without spawning a model
// call, landing each row terminal-failed. The claim + pick-up-without-
// restart is the behavior under test, not the child outcome.
func TestWorkerWaitsAndPicksUpNewTaskWithoutRestart(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	// THE CULPRIT (fulldry.log:522-524): this test drives the real worker loop
	// from the PACKAGE's working directory, i.e. the live repository, so the
	// round's own-worktree-release pass enumerated the developer's worktrees and
	// freed every clean unowned one. The repo context is a sandbox now.
	SandboxRepoContext(t)
	sandbox := t.TempDir()
	t.Setenv("GOPI_HOME", sandbox)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(sandbox, "memory.db"))
	t.Setenv("GOPI_COMPUTE_NODES", "")
	t.Setenv(config.SyncPeersEnv, "")
	t.Setenv("GOPI_SETTINGS", filepath.Join(sandbox, "nonexistent.json"))
	t.Setenv(config.SkipClaimBroadcastEnv, "1") // single-node hermetic claim
	t.Setenv(taskChildFailTestEnv, "1")         // child fails without spawning
	t.Setenv(workerIdleIntervalEnv, "1")        // short drained-queue wait
	t.Setenv(workerMaxRoundsEnv, "3")           // bound: both tasks + a drain probe

	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "worker wait pick-up 1",
		Payload: "first row — drains the queue",
		TaskID:  "t-worker-wait-1",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed 1: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Let the worker claim + execute task 1 and enter its drained
		// wait, then queue task 2 — the wait must surface it.
		time.Sleep(500 * time.Millisecond) //stylegate:allow-sleep settle window for the split serve/worker processes to reach their states
		if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
			Summary: "worker wait pick-up 2",
			Payload: "second row — submitted during the drained wait",
			TaskID:  "t-worker-wait-2",
			Source:  "human",
		}); err != nil {
			t.Errorf("seed 2: %v", err)
		}
	}()

	// Must RETURN (not os.Exit). The worker runs task 1, waits, re-checks,
	// picks up task 2, then hits the round cap and returns.
	runTaskWorker(false, "", nil)

	<-done
	// Both rows must be terminal (claimed + executed by the SAME process —
	// a restart-free pick-up is exactly what the long-running face adds).
	for _, id := range []string{"t-worker-wait-1", "t-worker-wait-2"} {
		row, err := tools.TaskShow(id)
		if err != nil {
			t.Fatalf("show %s: %v", id, err)
		}
		if !strings.Contains(row, "status:") || !strings.Contains(row, "failed") {
			t.Fatalf("worker must execute %s to terminal, got:\n%s", id, row)
		}
	}
}
