package main

// cli_tasks_worker_tag_flow_test.go — the FLOW face of the M4.20 ① worker
// provenance tag: the workerWriteback latch must route the terminal write
// through the TAGGED writeback, and a plain (non-worker) run must stay
// tag-free. The bug report claimed the tag never lands in the worker flow;
// these guards pin the wiring hermetically (no child engine, no network) so
// a regression to a gate/env-dependent resolution fails here.
//
// The real end-to-end evidence (a live `gopi worker --once` run stamping
// `repro,worker:<host>` on its claimed row) lives in
// devdoc/worker-provenance-tag.md §3.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// TestWorkerWritebackOutcomeStampsProvenanceTag: with the worker loop's latch
// set (workerRoundRun's state), the shared writebackOutcome writes the row
// through TaskRunWritebackTagged — tags end with worker:<host>. Without the
// latch (a plain `task run`), the row's tags are left exactly as submitted.
func TestWorkerWritebackOutcomeStampsProvenanceTag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	// The DAEMON case the report describes: no wrapper env is set in this
	// process — the tag must not depend on it.
	t.Setenv(tools.TaskWrapperEnv, "")

	const workerRow = "t-wflow-worker-1"
	const plainRow = "t-wflow-plain-1"
	for _, id := range []string{workerRow, plainRow} {
		if _, err := tools.TaskNew("flow row "+id, id, "", ""); err != nil {
			t.Fatalf("TaskNew %s: %v", id, err)
		}
	}

	// Clear any latch a previous test left behind (package-level state).
	prev := workerWriteback
	workerWriteback = ""
	defer func() { workerWriteback = prev }()

	// ① Plain run: no latch → tag-free writeback.
	writebackOutcome(plainRow, tools.TaskStatusCompleted, "plain done", "", "")
	plainTags, err := taskRowTags(t, path, plainRow)
	if err != nil {
		t.Fatalf("read plain row: %v", err)
	}
	if strings.Contains(plainTags, tools.WorkerTaskTagPrefix) {
		t.Fatalf("plain run must stay tag-free, got tags=%q", plainTags)
	}

	// ② Worker round: the latch routes through the tagged writeback.
	workerWriteback = workerRow
	writebackOutcome(workerRow, tools.TaskStatusCompleted, "worker done", "", "")
	workerTags, err := taskRowTags(t, path, workerRow)
	if err != nil {
		t.Fatalf("read worker row: %v", err)
	}
	if !strings.HasSuffix(workerTags, tools.WorkerTaskTag()) {
		t.Fatalf("worker row tags = %q, want suffix %q", workerTags, tools.WorkerTaskTag())
	}

	// ③ The latch is per-round state: clearing it returns to tag-free writes
	// (the NEXT row must not inherit the previous round's provenance).
	workerWriteback = ""
	const nextRow = "t-wflow-next-1"
	if _, err := tools.TaskNew("flow row "+nextRow, nextRow, "", ""); err != nil {
		t.Fatalf("TaskNew %s: %v", nextRow, err)
	}
	writebackOutcome(nextRow, tools.TaskStatusFailed, "failed outside a worker round", "", "")
	nextTags, err := taskRowTags(t, path, nextRow)
	if err != nil {
		t.Fatalf("read next row: %v", err)
	}
	if strings.Contains(nextTags, tools.WorkerTaskTagPrefix) {
		t.Fatalf("cleared latch still tagged the next row: tags=%q", nextTags)
	}
}
