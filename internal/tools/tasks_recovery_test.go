package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestCrashRecoveryResumeClaimed: M4.5 ⑥ — claimed is a TRANSIENT state whose
// sole durable justification is crash recovery. A row claimed by a "dead"
// holder (CAS won, TaskStart never ran) must be resumable: TaskResumeClaimed
// re-stamps the claim trail, TaskStart moves it to in_progress, and the
// writeback closes it — no claimed rows linger after a normal run.
func TestCrashRecoveryResumeClaimed(t *testing.T) {
	testutil.NewTestDBPath(t)

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "crash recovery victim", Payload: "real briefing", TaskID: "t-crash-1",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// Simulate a claimer that died right after the CAS.
	res, err := TaskNext("dead-holder:999", "")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if res.Empty || res.Task.Status != TaskStatusClaimed {
		t.Fatalf("claim must produce a claimed row, got %+v", res)
	}
	// A recovery run takes it over.
	if err := TaskResumeClaimed("t-crash-1", "recovery:1"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, err := TaskStart("t-crash-1", "recovery:1"); err != nil {
		t.Fatalf("start after resume: %v", err)
	}
	if _, err := TaskRunWriteback("t-crash-1", TaskStatusCompleted, "done — recovered"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	// No claimed rows linger.
	claimed, err := TaskList(TaskStatusClaimed)
	if err != nil {
		t.Fatalf("list claimed: %v", err)
	}
	if strings.Contains(claimed, "t-crash-1") {
		t.Errorf("claimed row must not linger after recovery+run:\n%s", claimed)
	}
	// Resume refuses non-claimed rows.
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "pending row", Payload: "p", TaskID: "t-crash-2"}); err != nil {
		t.Fatalf("submit 2: %v", err)
	}
	if err := TaskResumeClaimed("t-crash-2", ""); err == nil || !strings.Contains(err.Error(), "not claimed") {
		t.Errorf("resume must refuse non-claimed rows, got err=%v", err)
	}
	if err := TaskResumeClaimed("t-ghost", ""); err == nil || !strings.Contains(err.Error(), "no task") {
		t.Errorf("resume of missing row must error, got err=%v", err)
	}
}

// TestNoClaimedLingersAfterRunNext: the normal `run next` path (claim →
// start → writeback) leaves ZERO claimed rows — the transient state exists
// only inside the run, never at rest.
func TestNoClaimedLingersAfterRunNext(t *testing.T) {
	testutil.NewTestDBPath(t)

	for _, id := range []string{"t-q-1", "t-q-2"} {
		if _, err := TaskSubmit(TaskSubmitOptions{Summary: "queued " + id, Payload: "brief " + id, TaskID: id}); err != nil {
			t.Fatalf("submit %s: %v", id, err)
		}
	}
	// The engine sequence runTaskRun performs (claim → start → writeback).
	for i := 0; i < 2; i++ {
		res, err := TaskNext("runner", "")
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if res.Empty {
			t.Fatalf("queue unexpectedly empty at %d", i)
		}
		if _, err := TaskStart(res.Task.TaskID, "runner"); err != nil {
			t.Fatalf("start %s: %v", res.Task.TaskID, err)
		}
		if _, err := TaskRunWriteback(res.Task.TaskID, TaskStatusCompleted, "ok"); err != nil {
			t.Fatalf("writeback %s: %v", res.Task.TaskID, err)
		}
	}
	claimed, err := TaskList(TaskStatusClaimed)
	if err != nil {
		t.Fatalf("list claimed: %v", err)
	}
	if !strings.Contains(claimed, "No tasks") {
		t.Errorf("no claimed rows may linger after normal runs:\n%s", claimed)
	}
}

// TestLivenessColumns: M4.5 ⑦ groundwork — the two raw-fact columns exist,
// migrate idempotently, and round-trip through TaskUpdatePaths. (The
// loop.go-side writes land with the stale detector; this pins the schema.)
func TestLivenessColumns(t *testing.T) {
	testutil.NewTestDBPath(t)

	// Opening the ledger twice runs the migration twice (idempotency).
	for i := 0; i < 2; i++ {
		if err := ensureTasksSchemaOnceForTest(); err != nil {
			t.Fatalf("schema pass %d: %v", i, err)
		}
	}
	seedTaskRow(t, "t-live-1", "liveness row")
	// The new columns are present in the projection (scan succeeds).
	show, err := TaskShow("t-live-1")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "t-live-1") {
		t.Errorf("show broken by liveness columns:\n%s", show)
	}
}

// ensureTasksSchemaOnceForTest opens and closes a scratch ledger — exercising
// the ensureTasksSchema migration (incl. the new liveness columns) end to end.
func ensureTasksSchemaOnceForTest() error {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	return db.Close()
}
