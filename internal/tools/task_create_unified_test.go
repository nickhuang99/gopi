package tools

// task-create-unified epic (2026-09-05): every NEW-row creation — auto
// registration (TaskNewReturningID / extended TaskNewOID), queue submit
// (TaskSubmit) and a successor mint (TaskCloneWithOptions) — must land a
// row through the SINGLE all-column INSERT (insertTaskRow), so the schema a
// bare-prompt auto row carries equals the schema a submit row carries (E1).
// These tests pin the unified contract: (1) insertTaskRow writes every
// operator-visible create column, (2) the three public entry points all
// converge on it, (3) the extended TaskNewOID face stamps the optional
// fields in the same INSERT (no follow-up UPDATE).

import (
	"context"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tasksource"
	"github.com/nickhuang99/gopi/internal/tasktags"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestInsertTaskRowWritesFullColumnSurface pins the single-INSERT contract:
// the unified helper writes task_id/summary/status/session_id/model/
// parent_task_id/tags/payload/source/deliverable/priority/reason in ONE
// INSERT (plus created_at/updated_at). A row born through it must read back
// every field a caller stamped — the exact gap that motivated unification
// (the old auto path's 7-column INSERT left payload/tags/source/deliverable/
// priority/reason all unset, forcing a follow-up UPDATE to widen).
func TestInsertTaskRowWritesFullColumnSurface(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Every create column is stamped non-zero so the test would catch a
	// column silently dropped from the INSERT (a zero value would hide it
	// behind the schema default).
	id, err := insertTaskRow(context.Background(), db, taskCreateFields{
		TaskID:       "t-unified-e2e",
		Summary:      "unified path",
		Status:       taskStatusPending,
		SessionID:    "sess-1",
		Model:        "m-test",
		ParentTaskID: "t-parent",
		Tags:         "bug,merge-conflict",
		Payload:      "the full briefing",
		Source:       tasksource.Merger,
		Deliverable:  `{"repo":"/tmp/repo"}`,
		Priority:     1,
		Reason:       "why this row exists",
	})
	if err != nil {
		t.Fatalf("insertTaskRow: %v", err)
	}
	if id != "t-unified-e2e" {
		t.Fatalf("id = %q, want the explicit task_id returned", id)
	}

	row, err := taskByID(context.Background(), db, "t-unified-e2e")
	if err != nil {
		t.Fatalf("taskByID: %v", err)
	}
	if row == nil {
		t.Fatal("row vanished")
	}
	for _, check := range []struct {
		name string
		got  string
		want string
	}{
		{"summary", row.Summary, "unified path"},
		{"status", row.Status, taskStatusPending},
		{"session_id", row.SessionID, "sess-1"},
		{"model", row.Model, "m-test"},
		{"parent_task_id", row.ParentTaskID, "t-parent"},
		{"tags", row.Tags, "bug,merge-conflict"},
		{"payload", row.Payload, "the full briefing"},
		{"source", row.Source, tasksource.Merger},
		{"deliverable", row.Deliverable, `{"repo":"/tmp/repo"}`},
		{"reason", row.Reason, "why this row exists"},
	} {
		if check.got != check.want {
			t.Errorf("%s = %q, want %q", check.name, check.got, check.want)
		}
	}
	if row.Priority != 1 {
		t.Errorf("priority = %d, want 1", row.Priority)
	}
}

// TestTaskNewOIDStampsOptionalFieldsInSameInsert is the E1/E2 auto-row
// contract: the extended TaskNew face (what auto-registration and
// resolve-conflict registration use) lands payload/tags/source/reason/
// parent_task_id/deliverable in the SAME INSERT — no follow-up UPDATE to
// widen a sparse row. The plain TaskNewReturningID remains a compat alias.
func TestTaskNewOIDStampsOptionalFieldsInSameInsert(t *testing.T) {
	testutil.NewTestDBPath(t)

	id, err := TaskNewOID(TaskNewOptions{
		Summary:      "[auto] resolve conflict",
		Status:       taskStatusInProgress,
		SessionID:    "sess-r",
		Model:        "m",
		ParentTaskID: "t-parent",
		Tags:         "merge-conflict,auto-spawn",
		Payload:      "resolve the merge conflict on feat/x",
		Source:       tasksource.Merger,
		Deliverable:  `{"repo":"/tmp/repo","branch":"feat/x"}`,
		Reason:       "git merge conflict needs llm resolve",
		Priority:     5,
	})
	if err != nil {
		t.Fatalf("TaskNewOID: %v", err)
	}
	if id == "" {
		t.Fatal("TaskNewOID returned empty id")
	}
	row, err := TaskShow(id)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	for _, want := range []string{
		"[auto] resolve conflict", "merge-conflict,auto-spawn",
		"resolve the merge conflict on feat/x",
		`deliverable.repo: /tmp/repo`, TaskShowDeliverableBranchLabel + " feat/x",
		"git merge conflict needs llm resolve",
		"parent_task_id: t-parent",
		"source:         " + tasksource.Merger,
		"status:         in_progress",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("show missing %q, got:\n%s", want, row)
		}
	}
}

// TestAllNewRowPathsConvergeOnInsertTaskRow is the schema-alignment pin
// (E1): an auto-registered row (TaskNewReturningID), a submit row
// (TaskSubmit) and a successor row (TaskCloneWithOptions) all land with the SAME
// full column set — the operator-visible fields a caller does not stamp
// fall back to their defaults instead of being absent columns. It compares
// the created rows' field-coverage (which columns carry non-default values
// vs default) rather than values: submit stamps payload/source, auto stamps
// source only — but BOTH must go through one INSERT shape, so neither can
// carry a column the other's schema lacks.
func TestAllNewRowPathsConvergeOnInsertTaskRow(t *testing.T) {
	testutil.NewTestDBPath(t)

	// Auto-register path: in_progress row, no payload (the JSONL is the
	// briefing — committed renderer contract), source defaults to auto.
	autoID, err := TaskNewReturningID("[auto] bare prompt", "", "sess-auto", "m")
	if err != nil {
		t.Fatalf("TaskNewReturningID: %v", err)
	}
	// Submit path: pending row with the full dispatcher surface.
	queued, err := TaskSubmit(TaskSubmitOptions{
		Summary: "submitted work", Payload: "the brief", Tags: "bug",
		Source: "llm", SessionID: "sess-sub", Model: "m", TaskID: "t-sub-e2e",
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if !strings.Contains(queued, "t-sub-e2e") {
		t.Fatalf("queued = %q, want explicit task_id honoured", queued)
	}
	// Successor path: pending rework child. The source row is driven to a
	// TERMINAL verdict first, because `gopi task clone` has a status gate the
	// retired reopen did not: a row that has never run is refused by name (its
	// ground is UNUSED rather than suspect, so the remedy is to run it). A
	// `failed` row is precisely the state "the room may now be dirty".
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "rework parent", TaskID: "t-rw", Source: tasksource.Worker}); err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	if _, err := TaskUpdate("t-rw", TaskStatusInProgress); err != nil {
		t.Fatalf("progress t-rw: %v", err)
	}
	if _, err := TaskUpdateWithResult("t-rw", TaskStatusFailed, "verify: failed"); err != nil {
		t.Fatalf("fail t-rw: %v", err)
	}
	if _, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{
		TaskID: "t-rw", Reason: "verify: failed"}); err != nil {
		t.Fatalf("clone: %v", err)
	}

	auto := taskRowByID(t, autoID)
	sub := taskRowByID(t, "t-sub-e2e")
	rw := taskRowByID(t, cloneMintedSuccessorOf(t, "t-rw"))

	// Every row must have non-zero (real) values for the always-set columns,
	// and the DB-level defaults for the ones its path leaves unset — the
	// unified schema means a field is EITHER explicitly stamped OR the
	// schema default, never a missing column. No row may be missing a
	// create column the others carry.
	for _, r := range []*Task{auto, sub, rw} {
		if r.Summary == "" || r.Status == "" || r.Source == "" || r.TaskID == "" {
			t.Errorf("row %s has an empty always-set column: %+v", r.TaskID, r)
		}
	}
	if auto.Status != taskStatusInProgress {
		t.Errorf("auto status = %s, want in_progress", auto.Status)
	}
	if sub.Status != taskStatusPending || rw.Status != taskStatusPending {
		t.Errorf("submit/rework must be pending: sub=%s rw=%s", sub.Status, rw.Status)
	}
	// Field-coverage parity: the columns the three paths CAN write are the
	// same set — assert the submit/rework rows (the fullest) carry every
	// field the helper defines, and the auto row's defaults equal the
	// schema defaults (payload "" / tags "" / source "auto" / priority
	// derived).
	// The auto row expressed no source, so the funnel DERIVES one from the
	// process's own evidence (epic task-source-fake-human, 2026-09-23) — the
	// schema default "auto" is no longer what an unstamped row reads.
	if auto.Source != tasksource.DeriveNow("") || auto.Payload != "" || auto.Tags != "" {
		t.Errorf("auto row did not get the DERIVED source/empty payload+tags: source=%q payload=%q tags=%q",
			auto.Source, auto.Payload, auto.Tags)
	}
	// The retired "llm" spelling is STORED canonically (one vocabulary in the
	// ledger); the writer keeps the historical alias and the row records agent.
	if sub.Payload != "the brief" || sub.Tags != "bug" || sub.Source != tasksource.Agent {
		t.Errorf("submit row lost stamped fields (source must be canonical %q): payload=%q tags=%q source=%q",
			tasksource.Agent, sub.Payload, sub.Tags, sub.Source)
	}
	// The successor carries the source's text VERBATIM behind the clone's
	// provenance header (it is a copy, never a rewrite) and the lineage mark
	// that keeps it claimable (Rule 33).
	if rw.ParentTaskID != "t-rw" || !strings.Contains(rw.Payload, TaskCloneMarker) ||
		!taskHasTagToken(rw.Tags, tasktags.ReworkLineageTag) || rw.Source != tasksource.Worker {
		t.Errorf("successor row lost stamped fields (source must be %q): parent=%q payload=%q tags=%q source=%q",
			tasksource.Worker, rw.ParentTaskID, rw.Payload, rw.Tags, rw.Source)
	}
	// Priority derivation parity: the helper derives from the tags it was
	// given — the `bug` tag the SUBMIT path stamped derives the highest tier,
	// and the cloner's own row derives from the SOURCE's tags. This source
	// ("rework parent") carries none, so its copy lands on the default tier:
	// a copy is not automatically a bug, and `gopi task clone` must not
	// invent a tag the source never had.
	if sub.Priority != taskPriorityBug {
		t.Errorf("submit priority = %d, want bug tier %d", sub.Priority, taskPriorityBug)
	}
	if rw.Priority == 0 {
		t.Errorf("successor priority = 0 — the derivation must always answer a tier")
	}
}
