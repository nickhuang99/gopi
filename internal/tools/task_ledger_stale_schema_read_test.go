package tools

// task_ledger_stale_schema_read_test.go — the regression guard for the ONLY
// shape the test suite could not see: an OLD ledger opened by a NEW binary.
//
// THE DEFECT (measured 2026-10-29, task 01a184e8-4d91). Adding a column to the
// tasks table is an IDEMPOTENT ALTER, and every one of those ALTERs lives in
// ensureTasksSchema — which is reached only through openTasksDB, the READ-WRITE
// opener. The pure-read faces (openTasksDBReadOnly, mode=ro + query_only) never
// run a migration, while their projection (taskSelectColumns) names the new
// column. So on a ledger filed BEFORE the column existed, the read face's SQL is
// not "a query returning no value" — the STATEMENT ITSELF is invalid:
//
//	$ GOPI_MEMORY_DB=<old ledger> ./bin/gopi task show <id>
//	gopi task show error: task: show: SQL logic error: no such column: submitter_session (1)
//
// WHY THE EXISTING SUITE IS BLIND TO IT, and why this file cannot use the
// ordinary fixture (the reason it exists at all): testutil.NewTestDBPath gives a
// FRESH path, and the first open builds the table from the current schema
// literal — every column present by construction. No test in this package ever
// opened a ledger whose schema predated the binary, so the combination that
// breaks for every operator after a migration ("their memory.db, my new build")
// was unrepresentable. The fixture below builds the ledger with the PRODUCTION
// writer (Rule 32: never a hand-rolled schema) and then removes the column the
// projection requires — producing, by construction from real code, exactly the
// old shape.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// staleSchemaRowID is the row the fixture files before the column is removed, so
// the ledger's data is real ledger data, not a bare schema.
const staleSchemaRowID = "t-stale-schema-row"

// staleSchemaRowSummary is the headline the render must still carry after the
// repair — a healed ledger that answers with an empty/invented row would be a
// different defect.
const staleSchemaRowSummary = "a row filed before the submitter column existed"

// staleSchemaLeggerColumnsQuery lists the tasks table's columns through the
// read-only opener. The probe is a PRAGMA read on the same handle shape the
// faces under test use, so no connection with different visibility can answer it.
const staleSchemaColumnsQuery = `SELECT name FROM pragma_table_info('tasks')`

// staleSchemaLedger builds a ledger with the PRODUCTION writer, files one row
// through the production insert path, and then drops one projected column — the
// "old schema + new binary" shape. It returns the ledger path (already pinned
// into the environment by the scaffold).
func staleSchemaLedger(t *testing.T, column string) string {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	if _, err := TaskNew(staleSchemaRowSummary, staleSchemaRowID, "", ""); err != nil {
		t.Fatalf("file the row through the production writer: %v", err)
	}
	if !staleSchemaHasColumn(t, path, column) {
		t.Fatalf("fixture precondition: the production-built ledger must carry %q before it is removed", column)
	}
	staleSchemaDropColumn(t, path, column)
	if staleSchemaHasColumn(t, path, column) {
		t.Fatalf("fixture: %q must be GONE — otherwise this test would pass on the unfixed code and prove nothing", column)
	}
	return path
}

// staleSchemaHasColumn probes one column of the tasks table.
func staleSchemaHasColumn(t *testing.T, path, column string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerReadOnlyDSN(path))
	if err != nil {
		t.Fatalf("probe handle for %s: %v", path, err)
	}
	defer db.Close() //stylegate:ignore the fixture's own probe handle
	rows, err := db.Query(staleSchemaColumnsQuery)
	if err != nil {
		t.Fatalf("probe the tasks columns of %s: %v", path, err)
	}
	defer rows.Close() //stylegate:ignore the fixture's own probe cursor
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan a tasks column of %s: %v", path, err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the tasks columns of %s: %v", path, err)
	}
	return false
}

// staleSchemaDropColumn removes one column from the tasks table — the one edit
// that turns a current ledger into a pre-column one. It runs on the ledger's own
// writer DSN (never a bare path: the connection contract) and it is how the
// fixture reaches a shape no production path can produce any more.
func staleSchemaDropColumn(t *testing.T, path, column string) {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(path))
	if err != nil {
		t.Fatalf("drop-column handle for %s: %v", path, err)
	}
	defer db.Close() //stylegate:ignore the fixture's own migration handle
	if _, err := db.Exec("ALTER TABLE tasks DROP COLUMN " + column); err != nil {
		t.Fatalf("drop %q from %s: %v", column, path, err)
	}
}

// TestReadOnlyFaceHealsALedgerMissingAProjectedColumn is the regression guard: a
// pure-read face over an OLD-schema ledger must answer — because the read opener
// completes the one-time idempotent schema alignment first — instead of failing
// every SELECT with `no such column`.
//
// THE RED ARM is the whole point: with openTasksDBReadOnly unpatched, TaskShow
// returns the SQL error quoted in this file's header and this test fails at the
// first assertion, before any claim about the render.
func TestReadOnlyFaceHealsALedgerMissingAProjectedColumn(t *testing.T) {
	path := staleSchemaLedger(t, taskSubmitterColumnName)

	out, err := TaskShow(staleSchemaRowID)
	if err != nil {
		t.Fatalf("TaskShow on a ledger one column behind the binary must answer, not fail: %v\n"+
			"(this is the shipped defect: the read opener runs no migration, so the projection's "+
			"SELECT is invalid on the WHOLE table — not just on this row)", err)
	}
	if !strings.Contains(out, staleSchemaRowID) || !strings.Contains(out, staleSchemaRowSummary) {
		t.Fatalf("the healed render must carry the row's identity and headline; got:\n%s", out)
	}
	if !staleSchemaHasColumn(t, path, taskSubmitterColumnName) {
		t.Fatalf("the fix must ALIGN the ledger, not work around it: %q still absent after a read", taskSubmitterColumnName)
	}
	// The healed column is READABLE and its legacy default is the empty string —
	// history is never rewritten, so a pre-column row carries no submitter.
	db, err := sql.Open("sqlite", LedgerReadOnlyDSN(path))
	if err != nil {
		t.Fatalf("read the healed row: %v", err)
	}
	defer db.Close() //stylegate:ignore the fixture's own probe handle
	var submitter string
	if err := db.QueryRow(
		"SELECT COALESCE("+taskSubmitterColumnName+",'') FROM tasks WHERE task_id = ?",
		staleSchemaRowID).Scan(&submitter); err != nil {
		t.Fatalf("the healed column must be queryable by the projection's own spelling: %v", err)
	}
	if submitter != "" {
		t.Fatalf("%s = %q after the repair, want the legacy default \"\" — a migration adds a column, it never invents a fact",
			taskSubmitterColumnName, submitter)
	}
}

// TestEveryReadOnlyFaceHealsTheSameStaleLedger drives the OTHER pure-read faces
// over the same old-shape ledger. They all share openTasksDBReadOnly, so the
// repair is one seam — this arm is what proves the SEAM was patched and not a
// single verb.
func TestEveryReadOnlyFaceHealsTheSameStaleLedger(t *testing.T) {
	path := staleSchemaLedger(t, taskSubmitterColumnName)

	// The row was filed with NO session at all, so both answers are empty — the
	// assertion is that the CALL succeeds, which is impossible while the
	// projection names a column the table lacks.
	if _, err := TaskSessionID(staleSchemaRowID); err != nil {
		t.Fatalf("TaskSessionID (read-only opener) must answer on a stale ledger: %v", err)
	}
	if _, err := TaskSubmitterSession(staleSchemaRowID); err != nil {
		t.Fatalf("TaskSubmitterSession (read-only opener) must answer on a stale ledger: %v", err)
	}
	// The PATH-taking read-only opener: the deliverable selector, which is the
	// seam `gopi task show --branch` resolves through. TaskStrandedRows used to
	// hold this arm and went with its verb (task 01a18799, 2026-10-30 — the
	// stranded readout is retired); this one is the surviving opener of the same
	// shape, so the seam's own guarantee stays asserted and stays asserted by a
	// caller the product still has.
	if _, err := TaskForBranch(TaskForBranchQuery{Branch: staleSchemaRowID}, path); err != nil {
		t.Fatalf("TaskForBranch (read-only opener) must answer on a stale ledger: %v", err)
	}
}

// TestReadOnlyFaceLeavesACurrentLedgerAlone pins the trade the fix is NOT
// allowed to make: an up-to-date ledger must be served WITHOUT the write path
// being entered at all. The counter is a function variable the opener's repair
// leg increments, so "the reader never queued on the writer" stays a measured
// property instead of a comment (the 17.2s stall this opener exists to prevent).
func TestReadOnlyFaceLeavesACurrentLedgerAlone(t *testing.T) {
	path := staleSchemaLedger(t, taskSubmitterColumnName)
	// First read ALIGNS the ledger (the repair leg runs exactly once).
	if _, err := TaskSessionID(staleSchemaRowID); err != nil {
		t.Fatalf("align the ledger: %v", err)
	}
	before := ledgerReadOnlyRepairs.Load()
	if _, err := TaskSessionID(staleSchemaRowID); err != nil {
		t.Fatalf("second read on the aligned ledger: %v", err)
	}
	if after := ledgerReadOnlyRepairs.Load(); after != before {
		t.Fatalf("the repair leg ran %d more time(s) on an already-aligned ledger — every read would then queue on "+
			"the writer, which is the 17.2s stall openTasksDBReadOnly exists to prevent", after-before)
	}
	if !staleSchemaHasColumn(t, path, taskSubmitterColumnName) {
		t.Fatalf("the aligned ledger must keep %q", taskSubmitterColumnName)
	}
}

// TestTaskLedgerRequiredColumnsAreBareIdentifiers pins the derivation the repair
// depends on: the opener decides "is this ledger behind?" by taking the names
// out of taskSelectColumns — the projection's own definition, so the probe cannot
// drift from what the readers actually select. That only works while every
// element is a bare column name; an expression spliced into the projection would
// silently turn the probe into a comparison against nonsense.
func TestTaskLedgerRequiredColumnsAreBareIdentifiers(t *testing.T) {
	cols := taskLedgerRequiredColumns()
	if len(cols) < 2 {
		t.Fatalf("derived %d required column(s) from the projection — the probe would be vacuous", len(cols))
	}
	for _, c := range cols {
		for _, bad := range []string{"(", ")", " ", "\t", "\n", ".", "*", "'"} {
			if strings.Contains(c, bad) {
				t.Errorf("projected element %q is not a bare column name (contains %q) — taskLedgerRequiredColumns "+
					"cannot derive a schema probe from an expression", c, bad)
			}
		}
	}
}
