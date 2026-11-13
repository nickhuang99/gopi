package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TaskRepoGuardWarnings unit tests (M4.12 repo guardrail): a pending task
// whose payload has no deliverable.repo AND whose summary/payload mentions a
// known foreign-project token must produce a warning — the vcmi-class
// cross-project disaster prevention (guard is advisory, never a refusal).

func seedRepoGuardRow(t *testing.T, summary, payload, deliverable string) string {
	t.Helper()
	// Isolated throwaway DB — seeding into the shared default would leave
	// UNIQUE(task_id) landmines behind for every later run (and any run
	// whose cleanup was skipped). t.TempDir() is auto-removed.
	dbPath := filepath.Join(t.TempDir(), "tasks-test.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	tid := "t-repoguard-" + t.Name()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	_, err = db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, payload, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, 'pending', ?, ?, 'auto', 1000000000, 1000000000)`,
		tid, summary, payload, deliverable)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// rowid: the real lookup is by task_id (see TaskRepoGuardWarn), but the
	// FTS/tri triggers fire on rowid — closing the writer here is fine since
	// the reader re-opens the same file.
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM tasks WHERE task_id = ?`, tid) //stylegate:ignore auxiliary DDL/PRAGMA/FTS statement; the primary statement's result is what is checked
	})
	return tid
}

func TestTaskRepoGuardWarningsNoRepoForeignSummary(t *testing.T) {
	tid := seedRepoGuardRow(t, "vcmi install script fix", "payload text", "")
	got := TaskRepoGuardWarn(tid)
	if len(got) == 0 {
		t.Fatalf("want a warning for no-repo + foreign token, got none")
	}
	if !strings.Contains(got[0], "deliverable.repo") {
		t.Errorf("warning should mention deliverable.repo, got: %q", got[0])
	}
}

func TestTaskRepoGuardWarningsWithRepoNoWarn(t *testing.T) {
	tid := seedRepoGuardRow(t, "vcmi install script fix", "payload text", `{"repo":"/home/x/proj"}`)
	if got := TaskRepoGuardWarn(tid); len(got) != 0 {
		t.Fatalf("task with deliverable.repo must NOT warn, got: %v", got)
	}
}

func TestTaskRepoGuardWarningsUnrelatedSummaryNoWarn(t *testing.T) {
	tid := seedRepoGuardRow(t, "fix gopi memory sync", "payload text", "")
	if got := TaskRepoGuardWarn(tid); len(got) != 0 {
		t.Fatalf("no-repo but unrelated summary must NOT warn, got: %v", got)
	}
}

func TestTaskRepoGuardWarningsNullDeliverable(t *testing.T) {
	tid := seedRepoGuardRow(t, "evennia pipeline tuning", "payload text", "")
	got := TaskRepoGuardWarn(tid)
	if len(got) == 0 {
		t.Fatalf("NULL deliverable + foreign token must warn, got none")
	}
}

func TestTaskRepoGuardWarningsForeignTokenInPayload(t *testing.T) {
	tid := seedRepoGuardRow(t, "ci script empty file", "shellcheck for vcmi before_install", "")
	if got := TaskRepoGuardWarn(tid); len(got) == 0 {
		t.Fatalf("foreign token in payload must warn, got none")
	}
}
