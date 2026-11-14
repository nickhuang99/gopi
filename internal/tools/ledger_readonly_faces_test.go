package tools

// ledger_readonly_faces_test.go — the regression guard for the PURE-READ ledger
// faces wired to the read-only opener (openTasksDBReadOnly).
//
// THE DEFECT THIS CLOSES (measured live 2026-10-13, task 01a13490; full evidence
// in devdoc/ledger-readonly-faces.md). Every ledger face opened through
// openTasksDB, which runs the tasks DDL batch on EVERY open over a
// `_txlock=immediate` DSN — a BEGIN IMMEDIATE, the ledger's WRITE lock, on every
// invocation. Behind a writer that holds that lock, a PURE READER therefore waited
// out dbBusyTimeoutMSec. Measured on the operator's own ledger copy with a
// separate process holding BEGIN IMMEDIATE, same row, back to back:
//
//	old binary: `task show <row>`  rc=1  43.62s   (and it DELETED the -wal/-shm)
//	new binary: `task show <row>`  rc=1   0.01s   (sidecars intact)
//
// The two properties every converted face must keep are asserted HERE, per face:
//   (a) it finishes in well under dbBusyTimeoutMSec while a writer holds the
//       ledger — i.e. it neither takes nor queues on the write lock;
//   (b) it answers with the SAME reading a write-handle read of the same data
//       gives (a fast wrong answer is not a fix).
//
// The budget is 5s: two orders of magnitude below the observed old-binary stall
// (43s) and the busy timeout (15s), and far above a local read (0.01s), so a
// lock-taking face fails by a wide margin while a loaded host does not redden it.
// Mirrors taskShowLockBudget, which is the same bound for the same reason.

import (
	"context"

	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// readFaceBudget is the per-face bound described above.
const readFaceBudget = 5 * time.Second

// Parent/child/session fixture ids, named once (Rule 1b) because the seeding and
// the assertions must be about the same rows.
const (
	readFaceParentID  = "t-readface-parent"
	readFaceChildID   = "t-readface-child"
	readFaceSessionID = "s-readface-session"
	readFaceEventNeed = "readface-needle"
)

// readFaceFixture builds a DEVICE-BACKED ledger (the operator's own shape: a
// ledger on a block device is the one that carries WAL sidecars and takes the
// schema-build arm) carrying a parent row, a child row, a session id, a tagged
// row and one event; it points the tools layer at it and returns the ledger path
// plus a RELEASE func for the build handle. It skips — never passes — where the
// host offers no block-backed scratch directory (Rule 15).
func readFaceFixture(t *testing.T) (string, func()) {
	t.Helper()
	dir := testutil.TestDeviceScratchDir(t, "read-faces")
	if memBacked, description := testutil.ScratchRootFilesystem(dir); memBacked {
		t.Skipf("the fixture's ledger must be on a block device to exercise the schema-build arm a "+
			"lock-taking opener waits on: %s", description)
	}
	path := filepath.Join(dir, "memory.db")
	t.Setenv(config.MemoryDBEnv, path)
	t.Setenv(config.MemoryDBEnvLegacy, "")
	ctx := context.Background()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("build the device-backed ledger: %v", err)
	}
	release := func() { db.Close() } //stylegate:ignore the fixture's build handle, released by the caller

	if _, err := TaskNew("the parent row of the read-face fixture", readFaceParentID, "", ""); err != nil {
		release()
		t.Fatalf("seed the parent row: %v", err)
	}
	if _, err := TaskNew("the child row of the read-face fixture", readFaceChildID, "", ""); err != nil {
		release()
		t.Fatalf("seed the child row: %v", err)
	}
	// The child is parented through the production entry point, so the row the
	// reader sees was written the way any real row is.
	if _, err := TaskReparent(readFaceChildID, readFaceParentID, "read-face fixture"); err != nil {
		release()
		t.Fatalf("parent the child row: %v", err)
	}
	// A session id and a tag, so the readers that expose those columns have
	// something to disagree about if they break.
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET session_id = ?, tags = ? WHERE task_id = ?`,
		readFaceSessionID, "readface", readFaceParentID); err != nil {
		release()
		t.Fatalf("stamp the parent row's session id and tags: %v", err)
	}
	// One event, and one reframe-telemetry record on the same row.
	taskEventWrite(ctx, db, readFaceParentID, readFaceEventNeed, "read-face fixture event "+readFaceEventNeed)
	if err := TaskReframeRecord(readFaceParentID, "DOABLE", 1, "read-face fixture"); err != nil {
		release()
		t.Fatalf("record the reframe telemetry: %v", err)
	}
	return path, release
}

// readFaceHolder takes the ledger's WRITE lock from a separate connection exactly
// as any gopi writer does (openDB's DSN + BeginTx, which `_txlock=immediate` turns
// into BEGIN IMMEDIATE), and returns a release func. It fails the test unless the
// hold is REAL: a second writer with a no-wait timeout must be refused, otherwise
// a fast reading here would prove nothing (the vacuous shape the task-show guard
// documents).
func readFaceHolder(t *testing.T, path string) func() {
	t.Helper()
	holder, err := openDB(path, false)
	if err != nil {
		t.Fatalf("hold %s: %v", path, err)
	}
	tx, err := holder.BeginTx(context.Background(), nil)
	if err != nil {
		holder.Close() //stylegate:ignore test-owned holder; released when this helper fails the test
		t.Fatalf("holder BEGIN IMMEDIATE: %v", err)
	}
	probe, err := openDB(path, false)
	if err != nil {
		holder.Close() //stylegate:ignore test-owned holder; released when this helper fails the test
		t.Fatalf("probe open: %v", err)
	}
	defer probe.Close() //stylegate:ignore test-owned probe; released when this helper returns
	if _, err := probe.Exec(ledgerNoWaitPragma()); err != nil {
		t.Fatalf("probe %s: %v", path, err)
	}
	if _, err := probe.Exec(`UPDATE tasks SET status = 'completed' WHERE task_id = ?`, readFaceParentID); err == nil {
		t.Fatal("the holder did NOT take the write lock — a concurrent writer succeeded, so a fast read here would prove nothing")
	}
	return func() {
		_ = tx.Rollback() //stylegate:ignore releasing the test's own hold; the assertions are already made
		_ = holder.Close()
	}
}

// readFaceMeasured runs one face under the hold and reports what it answered and
// how long it took, failing the test on either property.
func readFaceMeasured(t *testing.T, face string, run func() (string, error)) string {
	t.Helper()
	start := time.Now()
	got, err := run()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("%s while a writer holds the ledger: %v (after %s) — a pure READ must neither fail nor queue on a writer", face, err, elapsed)
	}
	if elapsed >= readFaceBudget {
		t.Errorf("%s took %s while a writer held the ledger (budget %s) — this face is still taking the write lock", face, elapsed, readFaceBudget)
	}
	t.Logf("%s: %s in %s", face, got, elapsed)
	return got
}

// writeHandleRead runs the SAME reading through the write opener, so the two
// readings can be compared directly: this is the oracle for "the fast answer is
// also the right answer".
func writeHandleRead(t *testing.T, path, sqlText string, args ...any) string {
	t.Helper()
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("write-handle open: %v", err)
	}
	defer db.Close() //stylegate:ignore test-owned oracle handle; released when this helper returns
	var out string
	if err := db.QueryRow(sqlText, args...).Scan(&out); err != nil {
		t.Fatalf("write-handle read %q: %v", sqlText, err)
	}
	return out
}

func TestPureReadFacesServeTheSameReadingUnderAWriterHold(t *testing.T) {
	path, release := readFaceFixture(t)
	defer release()

	// THE ORACLE IS READ FIRST, through the write handle, with NO hold in place:
	// an openTasksDB open cannot even complete behind a writer (measured here:
	// `memory schema init: begin: database is locked (5) (SQLITE_BUSY)` after the
	// full busy timeout) — which is the very defect this file is about, and the
	// reason the expected readings are captured up front rather than computed
	// from inside the hold.
	wantStatus := writeHandleRead(t, path, `SELECT status FROM tasks WHERE task_id = ?`, readFaceParentID)
	wantSession := writeHandleRead(t, path, `SELECT COALESCE(session_id,'') FROM tasks WHERE task_id = ?`, readFaceParentID)
	wantTags := writeHandleRead(t, path, `SELECT COALESCE(tags,'') FROM tasks WHERE task_id = ?`, readFaceParentID)
	wantEvents := writeHandleRead(t, path, `SELECT CAST(COUNT(*) AS TEXT) FROM task_events WHERE task_id = ? AND event = ?`,
		readFaceParentID, ReframeEventName)
	if wantEvents == "0" {
		t.Fatalf("the fixture wrote no reframe telemetry, so that face's assertion would be vacuous")
	}

	releaseHold := readFaceHolder(t, path)
	defer releaseHold()

	// --- the row-level readers -------------------------------------------------
	if got := readFaceMeasured(t, "taskRowExists(parent)", func() (string, error) {
		ok, err := taskRowExists(readFaceParentID)
		return fmt.Sprintf("exists=%v", ok), err
	}); got != "exists=true" {
		t.Errorf("taskRowExists = %s, want exists=true (the write handle sees the row)", got)
	}

	if got := readFaceMeasured(t, "TaskRowStatusAt(parent)", func() (string, error) {
		return TaskRowStatusAt(readFaceParentID)
	}); got != wantStatus {
		t.Errorf("TaskRowStatusAt = %q, want %q — the same status the write handle reads", got, wantStatus)
	}

	if got := readFaceMeasured(t, "TaskSessionID(parent)", func() (string, error) {
		return TaskSessionID(readFaceParentID)
	}); got != wantSession {
		t.Errorf("TaskSessionID = %q, write handle reads %q — the read-only face must answer the same", got, wantSession)
	}

	if got := readFaceMeasured(t, "taskTagsByID(parent)", func() (string, error) {
		return taskTagsByID(readFaceParentID)
	}); got != wantTags {
		t.Errorf("taskTagsByID = %q, write handle reads %q", got, wantTags)
	}

	// --- the event and the branch readers -------------------------------------
	if got := readFaceMeasured(t, "taskEventHasDetail(needle)", func() (string, error) {
		return fmt.Sprintf("present=%v", taskEventHasDetail(readFaceParentID, readFaceEventNeed)), nil
	}); got != "present=true" {
		t.Errorf("taskEventHasDetail = %s, want present=true (the event was written through taskEventWrite)", got)
	}

	if got := readFaceMeasured(t, "ResolveTaskBranch(parent)", func() (string, error) {
		branch, found, err := ResolveTaskBranch(context.Background(), readFaceParentID)
		return fmt.Sprintf("branch=%q found=%v", branch, found), err
	}); got != `branch="" found=true` {
		t.Errorf("ResolveTaskBranch = %s, want branch=\"\" found=true — `found` reports the ROW (source: a nil row "+
			"returns false, anything else true) and this fixture's row carries no branch, so the branch half is empty", got)
	}

	// --- the list-shaped readers ----------------------------------------------
	if got := readFaceMeasured(t, "TaskChildren(parent)", func() (string, error) {
		kids, err := TaskChildren(readFaceParentID)
		if err != nil {
			return "", err
		}
		ids := make([]string, 0, len(kids))
		for _, k := range kids {
			ids = append(ids, k.TaskID)
		}
		return strings.Join(ids, ","), nil
	}); got != readFaceChildID {
		t.Errorf("TaskChildren = %q, want %q (the row the fixture parented)", got, readFaceChildID)
	}

	// The reframe telemetry is a rendered report: the oracle is the count the write
	// handle computed over the same events table, and the report must carry the row
	// the fixture recorded telemetry for.
	got := readFaceMeasured(t, "TaskReframeStats()", func() (string, error) {
		return TaskReframeStats()
	})
	if !strings.Contains(got, readFaceParentID) {
		t.Errorf("TaskReframeStats() does not mention the row it has telemetry for (%s): %q", readFaceParentID, got)
	}
}

// TestPureReadFacesRefuseWrites is the opener's own contract asserted once, apart
// from the timing claims: the handle these faces read through must REJECT a write,
// so a "pure read" wired by mistake to a writable handle cannot pass unnoticed.
func TestPureReadFacesRefuseWrites(t *testing.T) {
	path, release := readFaceFixture(t)
	defer release()
	db, err := openTasksDBReadOnly(context.Background(), path)
	if err != nil {
		t.Fatalf("openTasksDBReadOnly: %v", err)
	}
	defer db.Close() //stylegate:ignore test-owned handle; released when this test returns
	if _, err := db.Exec(`UPDATE tasks SET status = 'completed' WHERE task_id = ?`, readFaceParentID); err == nil {
		t.Fatal("the read-only handle accepted an UPDATE — a pure-read face could then mutate the ledger it reads")
	}
	var got string
	if err := db.QueryRow(`SELECT status FROM tasks WHERE task_id = ?`, readFaceParentID).Scan(&got); err != nil {
		t.Fatalf("read through the read-only handle: %v", err)
	}
	if strings.TrimSpace(got) == "" {
		t.Errorf("the read-only handle returned an empty status for a seeded row: %q", got)
	}

}
