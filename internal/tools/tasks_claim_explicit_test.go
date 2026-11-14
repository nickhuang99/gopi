package tools

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// Regression tests for the P0 double-execution bug (review/cli-task-core):
// TaskClaimExplicit shape ② (crash-recovery re-stamp of a claimed row) used
// to ignore RowsAffected and return nil for ANY row that was not pending —
// so `task run <id>` on an ALREADY-RUNNING (in_progress) task claimed it and
// proceeded to execute the briefing a SECOND time. The fix: shape ②'s
// conditional UPDATE is itself a CAS — RowsAffected==0 means the row is not
// claimable and MUST refuse (naming the current status + holder), never
// fall through to execution.

func mustSubmitTest(t *testing.T, summary, payload, taskID string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: summary, Payload: payload, TaskID: taskID,
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
}

func TestTaskClaimExplicitRefusesInProgress(t *testing.T) {
	testutil.NewTestDBPath(t)

	// Submit a row, then drive it to in_progress exactly as a first runner
	// would (claim CAS → start). It is now executing the briefing.
	mustSubmitTest(t, "already running", "real briefing payload", "t-inprog")
	if _, err := TaskNext("runner-1", ""); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, err := TaskStart("t-inprog", "runner-1"); err != nil {
		t.Fatalf("first start: %v", err)
	}

	// A SECOND `task run t-inprog` (TaskClaimExplicit) must REFUSE — the
	// row is in_progress, not claimable. Before the fix this returned nil
	// and double-executed the briefing.
	err := TaskClaimExplicit("t-inprog", "runner-2")
	if err == nil {
		t.Fatalf("TaskClaimExplicit on an in_progress row must refuse, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "in_progress") {
		t.Errorf("refusal must name the current status, got: %v", err)
	}
	if !strings.Contains(msg, "runner-1") {
		t.Errorf("refusal must name the current holder, got: %v", err)
	}

	// The row is untouched: still in_progress under the original holder.
	show, err := TaskShow("t-inprog")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(show, "in_progress") || !strings.Contains(show, "runner-1") {
		t.Errorf("in_progress row must be untouched by the refused claim:\n%s", show)
	}
}

func TestTaskClaimExplicitRefusesTerminalAndGhost(t *testing.T) {
	testutil.NewTestDBPath(t)

	// Terminal row (completed): finished work must not be re-run.
	mustSubmitTest(t, "finished", "brief done", "t-done")
	if _, err := TaskNext("runner-x", ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart("t-done", "runner-x"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := TaskRunWriteback("t-done", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	if err := TaskClaimExplicit("t-done", "runner-y"); err == nil ||
		!strings.Contains(err.Error(), "completed") {
		t.Errorf("claim of a completed row must refuse naming completed, got %v", err)
	}

	// Ghost id: a missing row is not claimable either.
	if err := TaskClaimExplicit("t-ghost", "runner-y"); err == nil ||
		!strings.Contains(err.Error(), "no task") {
		t.Errorf("claim of a missing row must refuse, got %v", err)
	}
}

func TestTaskClaimExplicitLegitimateShapes(t *testing.T) {
	testutil.NewTestDBPath(t)

	// Shape ①: a pending row is claimable (Phase-1 CAS) — unchanged.
	mustSubmitTest(t, "queued", "brief pending", "t-pend")
	if err := TaskClaimExplicit("t-pend", "runner-a"); err != nil {
		t.Fatalf("claim of a pending row must succeed, got %v", err)
	}

	// Shape ② crash recovery: a CLAIMED row whose holder died between the
	// CAS and TaskStart is the LEGITIMATE resume path — re-stamp must
	// succeed (this is what kept resume semantics working).
	mustSubmitTest(t, "crashed claimer", "brief resume", "t-crash")
	if _, err := TaskNext("dead-holder:9", ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := TaskClaimExplicit("t-crash", "recovery:2"); err != nil {
		t.Fatalf("resume of a claimed row must succeed, got %v", err)
	}
	// The trail now names the recovery holder and the row still advances.
	if _, err := TaskStart("t-crash", "recovery:2"); err != nil {
		t.Fatalf("start after resume: %v", err)
	}
}
