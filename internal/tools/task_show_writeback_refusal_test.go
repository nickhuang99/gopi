// task_show_writeback_refusal_test.go — THE MINIMAL REPRO for the epic's second
// defect: a REFUSED `completed` writeback must leave its reason READABLE ON THE
// ROW, not only on the wrapper's stderr.
//
// THE MEASURED SHAPE (2026-10-17, task 01a1492f). The Rule 30 close-out gate in
// task_reclaim.go writes a refusal to the row's trail under its own event name
// (`completed-unclean-tracked-work-refused`, `completed-unclean-refused`) and
// hands the sentence back to the caller, whose only consumer prints it on
// stderr ("writeback failed (continuing): …"). `task show` renders a verdict
// line from `verdictTrailEventNames` — and neither of those two event names was
// in it. Measured on this host's own ledger: the single row carrying
// `completed-unclean-tracked-work-refused` had the event on its trail and NO
// `verdict:`/`verdict.event:` line on its show.
//
// So the row stayed open (correct — Rule 30 refuses, the worktree is kept) and
// the ledger held the WHY in a place the surface did not print. An operator
// asking "why is this row still in_progress?" had to query task_events by hand.
//
// This test drives the REAL render (`taskShowWithDB`) over a fixture row and
// asserts the refusal sentence reaches the row's own output.
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestTaskShowRendersTheWritebackRefusalReason is the repro: the gate's own
// sentence is on the trail, so `task show` must print it.
//
// RED ON THE PRE-FIX TREE: before the two event names joined
// verdictTrailEventNames, the show printed the status and no verdict line, and
// this assertion failed with the whole render quoted.
func TestTaskShowRendersTheWritebackRefusalReason(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", path)

	const id = "01a20000-0000-7000-8000-000000000101"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "writeback refusal fixture", Payload: "p", TaskID: id, Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	ctx := context.Background()
	db := openTestTasksDB(t, path)
	defer db.Close() //stylegate:ignore test fixture handle
	// The row stays OPEN — that is what a refusal means, and the fixture must
	// reproduce it rather than assert around it.
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE task_id = ?`, taskStatusInProgress, id); err != nil {
		t.Fatalf("stamp status: %v", err)
	}

	// The gate's sentence, written through the SAME writer the gate uses.
	const note = "run " + id + " left UNCOMMITTED TRACKED WORK in its OWN worktree /tmp/wt: worktree is DIRTY (uncommitted work) — entries: M cmd/agent/cli_tasks.go; the completed writeback is REFUSED, so the row stays open"
	taskEventWrite(ctx, db, id, reclaimRunTrackedDirtRefusalEvent, note)

	out, err := taskShowWithDB(ctx, db, id)
	if err != nil {
		t.Fatalf("task show: %v", err)
	}
	for _, want := range []string{TaskShowVerdictLabel, reclaimRunTrackedDirtRefusalEvent, "UNCOMMITTED TRACKED WORK"} {
		if !strings.Contains(out, want) {
			t.Errorf("`task show` does not carry %q — the refusal reason is stderr-only again:\n%s", want, out)
		}
	}
	// The row is still open: this change renders the reason, it never closes it.
	if !strings.Contains(out, "status:         "+taskStatusInProgress) {
		t.Errorf("show no longer reports the row as %s:\n%s", taskStatusInProgress, out)
	}
}

// TestTaskShowRendersTheResidueRefusalReason is the same arm for the UNTRACKED
// refusal (the env-armed sibling), because the two are written by two different
// gates and a fix that covered one would leave the other silent.
func TestTaskShowRendersTheResidueRefusalReason(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", path)

	const id = "01a20000-0000-7000-8000-000000000102"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "residue refusal fixture", Payload: "p", TaskID: id, Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, path)
	defer db.Close() //stylegate:ignore test fixture handle
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE task_id = ?`, taskStatusInProgress, id); err != nil {
		t.Fatalf("stamp status: %v", err)
	}
	const note = "run " + id + " left UNTRACKED RESIDUE in its own ground; the completed writeback is REFUSED, so the row stays open"
	taskEventWrite(ctx, db, id, reclaimRunResidueRefusedEvent, note)

	out, err := taskShowWithDB(ctx, db, id)
	if err != nil {
		t.Fatalf("task show: %v", err)
	}
	for _, want := range []string{TaskShowVerdictLabel, reclaimRunResidueRefusedEvent, "UNTRACKED RESIDUE"} {
		if !strings.Contains(out, want) {
			t.Errorf("`task show` does not carry %q:\n%s", want, out)
		}
	}
}
