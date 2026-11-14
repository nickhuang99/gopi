package main

// Worker-round writeback E2E: the claimed ledger row of a `gopi worker` round
// must reach a terminal state, and on the HEALTHY path it must be reached by
// the WRAPPER's own post-Wait writeback and by nobody else.
//
// WHERE THE SECOND WRITER WENT (task 01a16f7c, 2026-10-25). This file used to
// prove a CHILD-side run close-out (internal/agent closeOutRunRow →
// tools.TaskRunCloseoutStaleRun) was live end to end and never double-wrote the
// healthy path. That seam is deleted: it asked "is the pid my wrapper published
// still alive?" — a question whose answer is fixed by construction, because the
// wrapper is the process that WAITs for the child and its own gate refused
// every write while the wrapper lived — and the write it guarded was a no-op on
// every healthy run. Its deterministic proofs lived in
// internal/tools/task_run_closeout_test.go, which is gone with it.
//
// WHAT REMAINS IS THE PART THAT WAS NEVER ABOUT THAT SEAM: the wiring. The real
// binary, a real worker round, a real child, the real ledger — and the property
// the deleted seam's first broken cut violated: ONE effective terminal write,
// carrying the WRAPPER's own result, never a generic digest invented by a child
// that raced ahead of it. (The child does still record one thing on a run the
// ENGINE cut short — the truncation note, internal/agent
// recordRunTruncationOnWrapperRow — but a finished run records nothing, which
// the arm below asserts.)
//
// The WRAPPER'S DEATH is still not staged here, deliberately: making a real
// `task run` wrapper die inside its post-Wait window from a test requires the
// child to kill its own parent at a chosen instant, which is inherently racy,
// and a flaky proof is worse than none. A wrapper killed in that window is a
// row whose claim holder is dead, which is the ledger's own stale/zombie
// disposition — a different mechanism, tested where it lives.

import (
	"database/sql"
	"errors"
	"github.com/nickhuang99/gopi/internal/config"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// wrapperFailureDetailProbe is the substring the WRAPPER's own failed-run
// writeback stamps into the row's detail (cli_tasks.go's synthetic/abnormal
// child sentence). It is the surviving witness of the property this E2E
// guards: the terminal write carries the WRAPPER's result. (The child close-out
// digest it used to be contrasted with has no producer any more — task
// 01a16f7c deleted it.)
const wrapperFailureDetailProbe = "gopi run failed"

// ledgerDB opens the SANDBOXED ledger the binary under test writes to.
func ledgerDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := os.Getenv("GOPI_MEMORY_DB")
	if dbPath == "" {
		t.Fatalf("GOPI_MEMORY_DB not set — the test must read the same ledger the child writes")
	}
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() }) //stylegate:ignore a read-only ledger handle's teardown; the assertions already ran against the same DB and a Close error here can change no verdict
	return db
}

// taskEventDetailContains reports whether any task_events row of the task
// carries needle in its detail — the append-only trail's own answer, read from
// the same ledger the binary just wrote to. It is the E2E twin of the
// unexported taskEventHasDetail (internal/tools), reached over the DB because
// `task show` renders only the LATEST event's detail (which, on the healthy
// path, is the wrapper's own writeback — not the close-out).
func taskEventDetailContains(t *testing.T, taskID, needle string) bool {
	t.Helper()
	var one int
	err := ledgerDB(t).QueryRow(
		`SELECT 1 FROM task_events WHERE task_id = ? AND detail LIKE ? LIMIT 1`,
		taskID, "%"+needle+"%").Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatalf("query task_events for %s: %v", taskID, err)
	}
	return true
}

// eventCountFor counts a row's transition events of one status in the
// append-only audit trail — the ledger's own witness of "how many effective
// writes" a row received.
func eventCountFor(t *testing.T, taskID, status string) int {
	t.Helper()
	var n int
	if err := ledgerDB(t).QueryRow(
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event = ?`,
		taskID, status).Scan(&n); err != nil {
		t.Fatalf("count %s events for %s: %v", status, taskID, err)
	}
	return n
}

// TestWorkerRunHealthyPathKeepsTheWrapperResultE2E is the REGRESSION guard: on
// the healthy path (the wrapper survives and performs its own post-Wait
// writeback — the majority case) the child's close-out must be a SILENT no-op.
// This is what the first cut of the fix broke: the child raced ahead of the
// live wrapper, and by closing the row it silently replaced the wrapper's
// richer detail (child exit status, elapsed time, deliverable branch) with the
// fallback's generic digest — and closed a run's row while it was still being
// observed. A run that fails must therefore keep the WRAPPER's failure detail
// and exactly one failed transition event.
func TestWorkerRunHealthyPathKeepsTheWrapperResultE2E(t *testing.T) {
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	work := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := testutil.MockEnv(t, gopiHome, "http://127.0.0.1:1")
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_AUTO_TASK_REGISTER=1",
		taskChildFailTestEnv+"=1", // hermetic failed child; the wrapper survives
		"GOPI_WORKER_MAX_ROUNDS=1",
		"GOPI_WORKER_IDLE_EXITS=1",
	)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"healthy path e2e", "--payload", "do the work", "--task-id", "84d1a496-ca1f-5aa0-9896-fd3eeab20798"}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	stdout, stderr, code := runGopiMapped(t, []string{"worker"}, "", env, work)
	if code != 0 {
		t.Fatalf("worker must exit 0 on a drained queue\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if st := taskStatusOf(t, "84d1a496-ca1f-5aa0-9896-fd3eeab20798"); st != tools.TaskStatusFailed {
		t.Fatalf("row status = %q, want %q", st, tools.TaskStatusFailed)
	}
	// The wrapper's writeback owns the result on the healthy path: the row's
	// terminal record must be the WRAPPER's own sentence.
	if !taskEventDetailContains(t, "84d1a496-ca1f-5aa0-9896-fd3eeab20798", wrapperFailureDetailProbe) {
		t.Fatalf("the row's terminal write does not carry the wrapper's own result — something else wrote it:\n%s", mustTaskList(t))
	}
	if n := eventCountFor(t, "84d1a496-ca1f-5aa0-9896-fd3eeab20798", tools.TaskStatusFailed); n != 1 {
		t.Errorf("failed transition events = %d, want exactly 1 (one effective write)", n)
	}
	if n := autoRowCount(t); n != 0 {
		t.Errorf("worker round left %d [auto] row(s); want 0 (single-row exec):\n%s", n, mustTaskList(t))
	}
}
