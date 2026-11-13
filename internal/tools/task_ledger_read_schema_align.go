package tools

// task_ledger_read_schema_align.go — the ONE-TIME schema alignment a PURE READER
// performs before it projects taskSelectColumns.
//
// THE DEFECT THIS CLOSES (measured 2026-10-29, task 01a184e8-4d91; evidence in
// that run's session dir). Every tasks-table migration in this package is an
// idempotent ALTER, and every one of them lives in ensureTasksSchema — which is
// reached only through openTasksDB, the READ-WRITE opener. The pure-read faces
// (openTasksDBReadOnly: mode=ro + query_only, the opener that exists so a reader
// never queues on the ledger's write lock) ran no migration at all, while their
// SELECT projection named the newest column. On the ONE combination every
// operator hits after a migration — an EXISTING ledger plus a NEW binary — the
// statement itself is invalid, so the whole table becomes unreadable:
//
//	$ GOPI_MEMORY_DB=<old ledger> ./bin/gopi task show <id>
//	gopi task show error: task: show: SQL logic error: no such column: submitter_session (1)
//
// and `task list` dies the same way. Not "a column reads empty": every row, every
// verb, at the SQL layer.
//
// WHY THE FIX IS NOT "openTasksDBReadOnly should call openTasksDB". That is the
// defect the read-only opener was created to remove (its own doc comment carries
// the measurement): openTasksDB runs the whole DDL batch over a
// `_txlock=immediate` DSN — a BEGIN IMMEDIATE, the ledger's WRITE lock — on EVERY
// open, and behind the resident worker/merger daemons the read verb waited out
// dbBusyTimeoutMSec (measured 17.2s locked vs 57ms unlocked). So the alignment is
// CONDITIONAL and ONE-TIME: a reader probes the ledger's own schema, and only
// when a projected column is actually absent does it hand the ledger to the
// writer's migration once. An up-to-date ledger — the case on every invocation
// after the first — costs one metadata query and never touches the write path
// (pinned by TestReadOnlyFaceLeavesACurrentLedgerAlone).

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

// ledgerReadOnlyRepairs counts the times the reader-initiated alignment ran in
// this process. It is a MEASURED property, not a log line: the guard test asserts
// it does not move on an already-aligned ledger, so "the reader still never
// queues on the writer" cannot silently regress into "the reader now migrates on
// every read".
var ledgerReadOnlyRepairs atomic.Int64

// taskLedgerRequiredColumns derives the columns a task-row reader actually
// SELECTs, from taskSelectColumns itself — never from a second hand-maintained
// list (Rule 2/13: one definition of the projection). A column appended to the
// projection is therefore covered by the probe automatically, which is exactly
// the failure mode being fixed (a column added to the projection, no migration
// leg for the reader).
//
// It is only valid while every element is a BARE column name; the packable
// invariant is pinned by TestTaskLedgerRequiredColumnsAreBareIdentifiers.
func taskLedgerRequiredColumns() []string {
	parts := strings.Split(taskSelectColumns, ",")
	cols := make([]string, 0, len(parts))
	for _, p := range parts {
		if c := strings.TrimSpace(p); c != "" {
			cols = append(cols, c)
		}
	}
	return cols
}

// taskLedgerMissingColumns answers which of the given columns the tasks table
// does not have. exists reports whether the tasks table is there at all: a reader
// must not CREATE one (that would make a pure read a write, and would turn "this
// file is not a ledger" into an empty ledger), so the caller distinguishes
// "no table" from "table behind the binary".
func taskLedgerMissingColumns(ctx context.Context, q QueryExecer, required []string) (missing []string, exists bool, err error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM pragma_table_info('tasks')`)
	if err != nil {
		return nil, false, fmt.Errorf("ledger schema probe: %w", err)
	}
	defer rows.Close() //stylegate:ignore read-only metadata cursor, closed at return of the probe
	have := make(map[string]bool, len(required))
	n := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, false, fmt.Errorf("ledger schema probe scan: %w", err)
		}
		have[name] = true
		n++
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("ledger schema probe rows: %w", err)
	}
	if n == 0 {
		return nil, false, nil
	}
	for _, c := range required {
		if !have[c] {
			missing = append(missing, c)
		}
	}
	return missing, true, nil
}

// alignLedgerSchemaForRead hands the ledger to the WRITER's one-time migration
// because a projected column is missing, and reports the refusal in a way an
// operator can act on when the alignment itself is impossible (a read-only file,
// a foreign database, no permission). Rule 15: the reader must never degrade into
// "every SELECT is invalid" with no explanation of why.
func alignLedgerSchemaForRead(ctx context.Context, path string, missing []string) error {
	ledgerReadOnlyRepairs.Add(1)
	// The reader is about to do a WRITE, and it says so — one line, on stderr,
	// so the operator can see the migration happen instead of inferring it from
	// a sudden improvement. It fires once per ledger, not per read.
	fmt.Fprintf(os.Stderr, "gopi task: ledger schema is behind this binary (missing column(s): %s) — "+
		"running the one-time alignment on %s\n", strings.Join(missing, ", "), path)
	db, err := openTasksDB(ctx, path)
	if err != nil {
		return fmt.Errorf("ledger schema alignment cannot run, so the ledger stays unreadable by this binary "+
			"(the projection needs column(s) %s; reason: %w)", strings.Join(missing, ", "), err)
	}
	if err := db.Close(); err != nil { //stylegate:ignore the alignment handle's close error is reported by the caller's own error, and the repair already committed
		return fmt.Errorf("ledger schema alignment close: %w", err)
	}
	return nil
}

// alignReadOnlyLedgerSchema is the read-only opener's last step: it asks the
// handle it just opened whether the ledger's tasks table carries every column
// the projection selects, and — ONLY when one is missing — closes that handle,
// runs the writer's one-time migration, and returns a fresh read-only handle.
//
// The order matters and is the whole fix: the probe uses the READ-ONLY handle the
// caller already has (a metadata query, no lock), so a current ledger never
// enters the write path at all — not even once per process, and never with a
// BEGIN IMMEDIATE. Only a ledger that is genuinely behind the binary pays the
// write lock, once, and then reads again through the same read-only contract.
func alignReadOnlyLedgerSchema(ctx context.Context, db *sql.DB, path string) (*sql.DB, error) {
	missing, err := ledgerNeedsReadAlignment(ctx, db)
	if err != nil {
		db.Close() //stylegate:ignore handle closed after its probe failed; the caller gets the error and no handle
		return nil, fmt.Errorf("task: read-only ledger open %s: %w", path, err)
	}
	if len(missing) == 0 {
		// CURRENT LEDGER — the ordinary case, and the one this opener exists
		// for: no write lock, no queueing behind the resident writers.
		return db, nil
	}
	db.Close() //stylegate:ignore this handle's work is done; the aligned one is opened below
	if err := alignLedgerSchemaForRead(ctx, path, missing); err != nil {
		return nil, err
	}
	aligned, err := sql.Open("sqlite", LedgerReadOnlyDSN(path))
	if err != nil {
		return nil, fmt.Errorf("task: read-only ledger reopen after alignment %s: %w", path, err)
	}
	aligned.SetMaxOpenConns(1)
	if _, err := aligned.Exec(ledgerReadQueryOnlyPragma); err != nil {
		aligned.Close() //stylegate:ignore handle closed after its setup failed; the caller gets the error and no handle
		return nil, fmt.Errorf("task: read-only ledger reopen after alignment %s: %w", path, err)
	}
	return aligned, nil
}

// ledgerNeedsReadAlignment is the ONE predicate the read-only opener consults:
// it opens the ledger read-only, asks whether the tasks table carries every
// projected column, and returns the missing ones (or nil when the ledger is
// current, or when it has no tasks table to align).
//
// A probe FAILURE is an error, never "assume current": assuming would hand a
// broken handle to the caller, and assuming "behind" would put a write lock on
// the read path for a reason nothing measured.
func ledgerNeedsReadAlignment(ctx context.Context, db *sql.DB) ([]string, error) {
	missing, _, err := taskLedgerMissingColumns(ctx, db, taskLedgerRequiredColumns())
	if err != nil {
		return nil, err
	}
	return missing, nil
}
