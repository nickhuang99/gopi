package main

// END-TO-END COST CONTRACT for the `task list --status in_progress` render
// (epic: re-land the address-based session-log resolution, 2026-09-15).
//
// `task list --status in_progress` pays a per-row session probe that resolves
// the row's session log. On master that resolution WALKED THE WHOLE SESSIONS
// TREE (70,438 session dirs on the operator's host; strace census 601,165
// getdents64 / 299,848 openat for one view) to find a file the row's own
// columns already name — so the view took 6.1-8.5s while every other --status
// answered in 0.04s on the SAME ledger: only in_progress rows get probed.
//
// This test asserts the property at the level the operator experiences it: a
// full render of an in_progress view whose rows are ADDRESSABLE walks the
// sessions tree ZERO times. It counts at the PRODUCTION seam
// (tools.SessionScanCounterForTest), so it cannot pass by an accident of a
// mirrored copy of the wiring.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/tools"
)

// scanCostForeignClaimHost is the claimed_by host of the fixture rows: it
// names NO local process, so the layer that can decide the honest-state label
// from a local pid cannot short-circuit the probe. The session resolution must
// run on this path for the count to mean anything.
const scanCostForeignClaimHost = "scan-cost-foreign-host.invalid"

// seedInProgressRowWithSession plants one in_progress row carrying a session
// id, and lays that session's record at recRel (relative to the sandbox's
// sessions root).
func seedInProgressRowWithSession(t *testing.T, taskID, sessionID, recRel string) {
	t.Helper()
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "scan-cost fixture " + taskID,
		Payload: "task list in_progress session-resolution cost fixture",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
	home, err := config.GopiDir()
	if err != nil {
		t.Fatalf("gopi dir: %v", err)
	}
	rec := filepath.Join(home, config.SessionsDirName, recRel)
	if err := os.MkdirAll(filepath.Dir(rec), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(rec), err)
	}
	if err := os.WriteFile(rec, []byte(`{"type":"session_start","sessionId":"`+sessionID+`"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", rec, err)
	}

	db, err := openRawTasksDB(memoryDBPathForTest(t))
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer db.Close()
	now := time.Now().Unix()
	if _, err := db.Exec(`UPDATE tasks SET status = ?, session_id = ?, claimed_by = ?, created_at = ?, updated_at = ? WHERE task_id = ?`,
		tools.TaskStatusInProgress, sessionID, scanCostForeignClaimHost+":1", now, now, taskID); err != nil {
		t.Fatalf("seed in_progress %s: %v", taskID, err)
	}
}

// TestTaskListInProgressViewWalksNoSessionTree is the end-to-end acceptance:
// with the rows' session records at the layouts their columns name, the render
// resolves every row WITHOUT walking the sessions tree — and it still renders
// the rows, so a zero count means "resolved by address", not "did no work".
func TestTaskListInProgressViewWalksNoSessionTree(t *testing.T) {
	taskListRenderSandbox(t)
	const (
		taskRunRow  = "t-scan-taskrun"
		bareRow     = "t-scan-bare"
		sessTaskRun = "819fc1ad-e248-4fc4-8b20-5c7193668c4d"
		sessBare    = "56055d3a-bb0f-424b-8d42-4416fd1f7893"
	)
	// The task-run layout (the operator's live shape) and the bare layout —
	// both named by the row's own columns, so neither needs a tree walk.
	seedInProgressRowWithSession(t, taskRunRow, sessTaskRun,
		filepath.Join(taskRunRow, sessTaskRun+sessionlayout.JSONLSuffix))
	seedInProgressRowWithSession(t, bareRow, sessBare,
		filepath.Join(sessBare, sessionlayout.RecordName))

	count, restore := tools.SessionScanCounterForTest()
	defer restore()

	out, err := runTaskListCLI(t, []string{"--status", tools.TaskStatusInProgress})
	if err != nil {
		t.Fatalf("task list --status in_progress: %v", err)
	}
	if n := count(); n != 0 {
		t.Fatalf("the in_progress render walked the sessions tree %d time(s); an addressable row must resolve with ZERO tree walks", n)
	}
	for _, want := range []string{taskRunRow, bareRow, "[" + tools.TaskStatusInProgress + "]"} {
		if !strings.Contains(out, want) {
			t.Errorf("the render must still show %q (a zero count must mean 'resolved by address', not 'rendered nothing'):\n%s", want, out)
		}
	}
}
