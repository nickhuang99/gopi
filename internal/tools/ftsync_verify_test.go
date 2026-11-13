package tools

// Independent acceptance verification for the legacy four-status CHECK
// rebuild migration (rename-recreate-copy-drop): SQLite ≥3.25 rewrites
// same-table trigger bodies to follow the RENAME (ON tasks → ON tasks_old),
// so the tasks_old DROP silently deleted tasks_ai/tasks_au/tasks_ad and the
// copy INSERTs never indexed the migrated rows into tasks_fts/tasks_tri.
//
// Fixed by the post-commit FTS rescue in tasksRebuildCheckConstraint:
// drop/recreate both companions + VALUES('rebuild') repopulation, triggers
// re-created LAST. These tests assert the Epic acceptance criteria:
//   1. count(tasks_fts) == count(tasks) (and tasks_tri) after migration
//   2. all three sync triggers exist with tbl_name='tasks' (not tasks_old)
//   3. fresh inserts are still indexed (triggers live), and
//      ensureTasksSchema is idempotent (second run changes nothing)
//
// Two legacy shapes are covered because they reach the hole differently:
//   - classic: 4-status CHECK + OLD 3-col FTS companions — shape drift makes
//     migrateTasksFTS drop/rebuild them (this path was already rescued)
//   - intermediate: 4-status CHECK but FTS companions ALREADY 7-col —
//     migrateTasksFTS's shape check matches (no rescue there); only the
//     rebuild migration's own rescue saves the migrated rows

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ftsVerifyCols7 = "task_id, session_id, summary, parent_task_id, tags, payload, source"
const ftsVerifyCols3 = "task_id, session_id, summary"

// ftsVerifyTriggerDefs builds ai/au/ad trigger DDL for the given content
// column list (the legacy DB's triggers must reference ITS OWN column shape).
func ftsVerifyTriggerDefs(cols string) []string {
	return []string{
		`CREATE TRIGGER tasks_ai AFTER INSERT ON tasks BEGIN
	INSERT INTO tasks_fts(rowid, ` + cols + `) VALUES (new.id, ` + ftsVerifyNewRefs(cols) + `);
	INSERT INTO tasks_tri(rowid, ` + cols + `) VALUES (new.id, ` + ftsVerifyNewRefs(cols) + `);
END`,
		`CREATE TRIGGER tasks_au AFTER UPDATE ON tasks BEGIN
	INSERT INTO tasks_fts(tasks_fts, rowid, ` + cols + `) VALUES ('delete', old.id, ` + ftsVerifyOldRefs(cols) + `);
	INSERT INTO tasks_fts(rowid, ` + cols + `) VALUES (new.id, ` + ftsVerifyNewRefs(cols) + `);
	INSERT INTO tasks_tri(tasks_tri, rowid, ` + cols + `) VALUES ('delete', old.id, ` + ftsVerifyOldRefs(cols) + `);
	INSERT INTO tasks_tri(rowid, ` + cols + `) VALUES (new.id, ` + ftsVerifyNewRefs(cols) + `);
END`,
		`CREATE TRIGGER tasks_ad AFTER DELETE ON tasks BEGIN
	INSERT INTO tasks_fts(tasks_fts, rowid, ` + cols + `) VALUES ('delete', old.id, ` + ftsVerifyOldRefs(cols) + `);
	INSERT INTO tasks_tri(tasks_tri, rowid, ` + cols + `) VALUES ('delete', old.id, ` + ftsVerifyOldRefs(cols) + `);
END`,
	}
}

func ftsVerifyNewRefs(cols string) string {
	return strings.ReplaceAll("new."+cols, ", ", ", new.")
}

func ftsVerifyOldRefs(cols string) string {
	return strings.ReplaceAll("old."+cols, ", ", ", old.")
}

// ftsVerifySeedLegacyDB creates a bare marker DB (openTasksDB refuses a file
// without the distilled_qa/memory_feedback markers) plus a legacy four-status
// CHECK tasks table, FTS companions and sync triggers of the given content
// shape, then seeds 3 terminal-state rows (completed/failed/cancelled — the
// rows most likely to be invisible after a broken migration).
func ftsVerifySeedLegacyDB(t *testing.T, path, ftsCols string, withP2Columns bool) {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, marker := range []string{
		`CREATE TABLE distilled_qa (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE memory_feedback (id INTEGER PRIMARY KEY)`,
	} {
		if _, err := db.Exec(marker); err != nil {
			t.Fatal(err)
		}
	}
	p2Cols := ""
	if withP2Columns {
		p2Cols = `,
	parent_task_id TEXT DEFAULT '',
	tags           TEXT DEFAULT '',
	payload        TEXT DEFAULT '',
	source         TEXT DEFAULT 'auto',
	claimed_by     TEXT DEFAULT '',
	claimed_at     REAL`
	}
	legacy := []string{
		`CREATE TABLE tasks (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL UNIQUE,
	summary    TEXT NOT NULL,
	status     TEXT NOT NULL DEFAULT 'in_progress'
	           CHECK (status IN ('in_progress','completed','failed','cancelled')),
	session_id TEXT DEFAULT '',
	model      TEXT DEFAULT '',
	created_at REAL NOT NULL,
	updated_at REAL NOT NULL,
	ended_at   REAL` + p2Cols + `)`,
		`CREATE VIRTUAL TABLE tasks_fts USING fts5(
	` + ftsCols + `, content='tasks', content_rowid='id')`,
		`CREATE VIRTUAL TABLE tasks_tri USING fts5(
	` + ftsCols + `, content='tasks', content_rowid='id', tokenize='trigram')`,
	}
	legacy = append(legacy, ftsVerifyTriggerDefs(ftsCols)...)
	legacy = append(legacy,
		`INSERT INTO tasks (task_id, summary, status, session_id, model, created_at, updated_at, ended_at)
		 VALUES ('t-legacy-1', 'harvest the brew rows', 'completed', 'sess-old', 'deepseek-old', 1700000000, 1700000001, 1700000002)`,
		`INSERT INTO tasks (task_id, summary, status, session_id, model, created_at, updated_at, ended_at)
		 VALUES ('t-legacy-2', 'trigram companion rebuild failed', 'failed', 'sess-old', 'deepseek-old', 1700000003, 1700000004, 1700000005)`,
		`INSERT INTO tasks (task_id, summary, status, session_id, model, created_at, updated_at, ended_at)
		 VALUES ('t-legacy-3', 'cancelled legacy entry', 'cancelled', 'sess-old', 'deepseek-old', 1700000006, 1700000007, 1700000008)`,
	)
	for _, stmt := range legacy {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("legacy setup: %v", err)
		}
	}
}

// ftsVerifyAssertAcceptance runs the three Epic acceptance assertions on an
// an opened (migrated) DB. wantRows >= 0 also asserts the ABSOLUTE row
// count BEFORE the fresh submit (parity is checked both before and after
// it). NOTE: each call submits one fresh row, so a caller that invokes
// this twice must expect the count to grow — parity, not a fixed number,
// is the invariant under test (count(tasks_fts)==count(tasks), criterion 1
// of the Epic, "旧行全部可召回").
func ftsVerifyAssertAcceptance(t *testing.T, ctx context.Context, db *sql.DB, wantRows int) {
	t.Helper()

	// Criterion 1: the migrated rows must be in BOTH indexes — exact parity,
	// not just recall.
	for _, idx := range []string{"tasks_fts", "tasks_tri"} {
		var idxCount, rowCount int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+idx).Scan(&idxCount); err != nil {
			t.Fatalf("count %s: %v", idx, err)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tasks`).Scan(&rowCount); err != nil {
			t.Fatal(err)
		}
		if idxCount != rowCount {
			t.Fatalf("BUG: %s has %d rows but tasks has %d — migrated rows lost from the index", idx, idxCount, rowCount)
		}
		if wantRows >= 0 && idxCount != wantRows {
			t.Fatalf("%s has %d rows before the fresh submit, want %d", idx, idxCount, wantRows)
		}
	}

	// Criterion 2: all three sync triggers exist and reference tasks (the
	// rename-follow rewrite + tasks_old DROP would have deleted them).
	for _, trig := range []string{"tasks_ai", "tasks_au", "tasks_ad"} {
		var ddl, tbl string
		if err := db.QueryRowContext(ctx,
			`SELECT sql, tbl_name FROM sqlite_master WHERE type='trigger' AND name=?`, trig,
		).Scan(&ddl, &tbl); err != nil {
			t.Fatalf("trigger %s missing after migration: %v", trig, err)
		}
		if tbl != "tasks" {
			t.Fatalf("trigger %s tbl_name=%q, want tasks", trig, tbl)
		}
		if strings.Contains(ddl, "tasks_old") {
			t.Fatalf("trigger %s still references tasks_old: %s", trig, ddl)
		}
	}

	// Criterion 3a: the companions are LIVE — a fresh insert must be indexed
	// by the re-created ai trigger. NOTE: the summary must NOT begin with
	// "bug"/"doc" — TaskSubmit derives priority from tags and a literal tag
	// here would skew nothing, but keep the seed neutral anyway.
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "post-migration fresh row for live check", Payload: "fresh",
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var hit int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM tasks_fts WHERE tasks_fts MATCH 'fresh'`).Scan(&hit); err != nil {
			t.Fatal(err)
		}
		if hit > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tasks_fts not live after migration — fresh insert never indexed")
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep poll interval while the FTS index picks up the fresh insert
	}

	// Criterion 3b: index parity still holds INCLUDING the fresh row
	// (criteria 1 + 3 together: no row left behind, nothing double-written).
	for _, idx := range []string{"tasks_fts", "tasks_tri"} {
		var idxCount, rowCount int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+idx).Scan(&idxCount); err != nil {
			t.Fatalf("count %s: %v", idx, err)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tasks`).Scan(&rowCount); err != nil {
			t.Fatal(err)
		}
		if idxCount != rowCount {
			t.Fatalf("after fresh insert: %s has %d rows but tasks has %d — live sync broke parity", idx, idxCount, rowCount)
		}
	}
}

// ftsVerifyAssertIdempotent runs ensureTasksSchema a second time and checks
// nothing regressed (counts still in parity, triggers still present).
func ftsVerifyAssertIdempotent(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if err := ensureTasksSchema(ctx, db); err != nil {
		t.Fatalf("second ensureTasksSchema: %v", err)
	}
	ftsVerifyAssertAcceptance(t, ctx, db, -1)
}

// TestTasksMigrationFTSParityClassic — legacy 4-status CHECK + OLD 3-col FTS
// companions: migrateTasksFTS's shape drift rescues this path (3→7 cols
// forces a drop/rebuild/repopulate). After migration the counts must be in
// parity and everything must still work.
func TestTasksMigrationFTSParityClassic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	ftsVerifySeedLegacyDB(t, path, ftsVerifyCols3, false)

	t.Setenv("GOPI_MEMORY_DB", path)
	ctx := context.Background()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ftsVerifyAssertAcceptance(t, ctx, db, 3) // 3 legacy rows, pre-fresh
	ftsVerifyAssertIdempotent(t, ctx, db)
}

// TestTasksMigrationFTSParityIntermediate — the REAL hole: 4-status CHECK
// (tasksNeedsRebuild fires) but FTS companions already widened to 7 cols
// (migrateTasksFTS's shape check matches → no rescue there). Without the
// rebuild migration's own FTS rescue the migrated rows are permanently
// invisible to tasks_fts/tasks_tri search.
func TestTasksMigrationFTSParityIntermediate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	ftsVerifySeedLegacyDB(t, path, ftsVerifyCols7, true)

	t.Setenv("GOPI_MEMORY_DB", path)
	ctx := context.Background()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ftsVerifyAssertAcceptance(t, ctx, db, 3)
	ftsVerifyAssertIdempotent(t, ctx, db)
}
