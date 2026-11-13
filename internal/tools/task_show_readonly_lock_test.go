package tools

// task_show_readonly_lock_test.go — the regression guard for a READ verb
// taking, and waiting on, the ledger's WRITE lock.
//
// THE DEFECT (measured live 2026-10-13, task 01a133ee-2d09). `gopi task show`
// renders a row it only reads, but it opened the ledger through openTasksDB,
// which runs the whole tasks DDL batch (CREATE TABLE IF NOT EXISTS, the
// P2/proc-identity ALTERs, the FTS index-shape probe) on EVERY open, over a
// `_txlock=immediate` DSN — a BEGIN IMMEDIATE, i.e. the ledger's write lock, on
// every invocation. Behind the resident worker/merger daemons that hold that
// lock on a live host, the read verb then waited out dbBusyTimeoutMSec
// (measured on the live ledger's own copy: 17.2s locked vs 57ms unlocked,
// interleaved A/B, three reps) — which is the reported "task show hangs on an
// existing row" once an operator's `timeout 15` wrapper sits in front of it.
//
// WHY THE LEDGER HERE IS DEVICE-BACKED, MEASURED AND NOT ASSUMED. A fresh
// ledger on a MEMORY filesystem (this suite's scratch root is /dev/shm) builds
// its schema in the autocommit arm — `ledgerBuildBatchNeeded` says a memory
// filesystem pays no device sync, so ensureTasksSchema takes the
// non-transactional path and the read verb never asks for the write lock. The
// first version of this test therefore passed with the production change
// reverted: a vacuous red proof, caught by running it
// (TestTaskShowDoesNotWaitOnAWriterHold — PASS in the red arm). The subject here
// is a ledger on a BLOCK DEVICE, which is the operator's own shape
// (~/.gopi/memory.db on ext4), so the fixture takes its directory from
// testutil.TestDeviceScratchDir and skips — never passes — where no device
// exists to measure (Rule 15: an unmeasurable property is reported, not
// assumed).

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// taskShowLockRowID is the row this test renders while the lock is held. A
// distinct, explicit id keeps the assertion independent of how the ledger
// generates auto ids (one definition, referenced at both call sites).
const taskShowLockRowID = "t-show-readonly-lock"

// taskShowLockBudget is the assertion's bound: a show that READS must not spend
// anything like dbBusyTimeoutMSec (15s) queueing behind a writer. The bound
// sits far below the timeout, so a lock-waiting show fails by a wide margin
// (the defect's own reading was 17s), and far above a local read (40-220ms
// measured on the live ledger) so a loaded host does not redden it spuriously.
// Named, never inlined (Rule 1).
const taskShowLockBudget = 5 * time.Second

// taskShowLockLedgerFile is the ledger this fixture builds inside its
// device-backed directory.
const taskShowLockLedgerFile = "memory.db"

// taskShowLockHolder opens the ledger through PRODUCTION's own opener (openDB:
// one pooled connection, busy_timeout, `_txlock=immediate`) — so the lock this
// test's holder takes is the same lock a resident gopi daemon takes on the
// operator's ledger, and the file carries no hand-rolled DSN of its own (the
// cmd/agent ledger-DSN contract gate reads every opener's DSN and is right to
// refuse a second spelling).
func taskShowLockHolder(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := openDB(path, false)
	if err != nil {
		t.Fatalf("hold %s: %v", path, err)
	}
	return db
}

// taskShowLockFixture builds a device-backed ledger carrying one row and points
// the tools layer at it, returning the ledger path. It skips (never passes)
// when the host offers no block-backed scratch directory.
func taskShowLockFixture(t *testing.T) string {
	t.Helper()
	dir := testutil.TestDeviceScratchDir(t, "show-lock")
	if memBacked, description := testutil.ScratchRootFilesystem(dir); memBacked {
		t.Skipf("the fixture's ledger must be on a block device to exercise the schema-build arm "+
			"(a memory filesystem takes the autocommit path and never asks for the write lock): %s", description)
	}
	path := filepath.Join(dir, taskShowLockLedgerFile)
	// The env contract: the tools layer resolves THIS ledger (t.Setenv, never
	// os.Setenv — Rule 17).
	t.Setenv(config.MemoryDBEnv, path)
	t.Setenv(config.MemoryDBEnvLegacy, "")
	// Build the ledger through the production write opener: show only ever
	// reads, and a reader must not be the thing that creates a ledger.
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("build the device-backed ledger: %v", err)
	}
	if err := db.Close(); err != nil { //stylegate:ignore the fixture's build handle; the assertions use its own openers
		t.Fatalf("close the build handle: %v", err)
	}
	if _, err := TaskNew("a row that must be readable behind a writer", taskShowLockRowID, "", ""); err != nil {
		t.Fatalf("seed the row: %v", err)
	}
	return path
}

func TestTaskShowDoesNotWaitOnAWriterHold(t *testing.T) {
	path := taskShowLockFixture(t)

	// The holder: a second connection on the same ledger file, taking the
	// write lock exactly as any gopi writer does (openDB's DSN + BeginTx,
	// which `_txlock=immediate` turns into BEGIN IMMEDIATE).
	holder := taskShowLockHolder(t, path)
	defer holder.Close() //stylegate:ignore test-owned holder; released at test end
	tx, err := holder.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("holder BEGIN IMMEDIATE: %v", err)
	}
	defer func() { _ = tx.Rollback() }() //stylegate:ignore releasing the test's own hold; the assertions are already made

	// Prove the hold is REAL before asserting anything about show: a second
	// writer with a short timeout must be refused. Without this the test would
	// report "show was fast" for a run in which nobody held the lock at all
	// (the vacuous shape that made this test's first version pass in the red
	// arm).
	probe := taskShowLockHolder(t, path)
	defer probe.Close() //stylegate:ignore test-owned probe; released at test end
	if _, err := probe.Exec(ledgerNoWaitPragma()); err != nil {
		t.Fatalf("probe %s: %v", path, err)
	}
	if _, err := probe.Exec(`UPDATE tasks SET status = 'completed' WHERE task_id = ?`, taskShowLockRowID); err == nil {
		t.Fatal("the holder did NOT take the write lock — a concurrent writer succeeded, so a fast show here would prove nothing")
	}

	start := time.Now()
	digest, err := TaskShow(taskShowLockRowID)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("task show while another connection holds the write lock: %v (after %s) — a read verb must neither fail nor queue on a writer", err, elapsed)
	}
	if elapsed >= taskShowLockBudget {
		t.Fatalf("task show took %s while a writer held the ledger — the read verb is taking (and then waiting for) the write lock; the bound is %s",
			elapsed, taskShowLockBudget)
	}
	if !strings.Contains(digest, taskShowLockRowID) {
		t.Fatalf("digest rendered under the hold does not carry the row id %q: %q", taskShowLockRowID, digest)
	}
}

// TestTaskShowReadsThroughAReadOnlyHandle is the opener's own contract,
// asserted apart from the timing claim: the handle `task show` reads through
// must REFUSE a write. Without it the timing test could pass on an opener that
// is merely fast, and the property the fix rests on — a read verb holds no
// write lock — would be untested.
func TestTaskShowReadsThroughAReadOnlyHandle(t *testing.T) {
	path := taskShowLockFixture(t)
	db, err := openTasksDBReadOnly(context.Background(), path)
	if err != nil {
		t.Fatalf("openTasksDBReadOnly: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = 'completed' WHERE task_id = ?`, taskShowLockRowID); err == nil {
		t.Fatal("the read-only handle accepted an UPDATE — a read verb could then mutate the row it is rendering")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE task_id = ?`, taskShowLockRowID).Scan(&n); err != nil {
		t.Fatalf("read through the read-only handle: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows for %q = %d, want 1", taskShowLockRowID, n)
	}
}
