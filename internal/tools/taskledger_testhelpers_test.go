package tools

// Shared ledger test helpers.
//
// WHY THIS FILE EXISTS (Rule 43): these three helpers lived inside the
// zombie-sweep mechanism's own test file. The mechanism is GONE
// (devdoc/zombie-sweep-removed.md), but the helpers were never part of it —
// they are generic ledger readers that tests elsewhere in this package had
// been calling all along, so they were MOVED here in the same commit as the
// deletion rather than carried out with it. Deleting them would have broken
// `go build ./...` in files that have nothing to do with zombie disposal.

import (
	"database/sql"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// ledgerTestDB opens the ledger the fixture pinned (testutil.NewTestDBPath set
// the env; memoryDBPath resolves it through the ONE resolver).
func ledgerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(memoryDBPath()))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	t.Cleanup(func() { db.Close() }) //stylegate:ignore the fixture's own handle; Close's error carries no state the test can act on
	return db
}

// taskEventsFor reads the task_events rows for one task (detail column).
func taskEventsFor(t *testing.T, taskID string) []string {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	rows, err := db.Query(`SELECT detail FROM task_events WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		t.Fatalf("query task_events: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d sql.NullString
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if d.Valid {
			out = append(out, d.String)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate task_events: %v", err)
	}
	return out
}

// fixtureProfileHome points the PROFILE at a fixture home so any code that
// resolves <GOPI_HOME>/.gopi writes there instead of the operator's live tree.
func fixtureProfileHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	return home
}
