package tools

import (
	"context"
	"database/sql"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Repro 1: TaskNextFiltered with a skip-tag on the OLDEST pending row must
// keep claiming later eligible rows — instead it returns Empty (head-blocking).
func TestReproSkipTagHeadBlocking(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	dir := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(dir, "mem.db"))
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "skipped oldest", Payload: "p1", Tags: "pa-queue", Source: "human", SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "eligible newer", Payload: "p2", Source: "human", SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	res, err := TaskNextFiltered("tester", "", "pa-queue")
	if err != nil {
		t.Fatal(err)
	}
	if res.Empty {
		t.Fatalf("BUG REPRODUCED: --skip-tag head-blocks — oldest pending row carries the skipped tag, the eligible newer row was never claimed")
	}
	if res.Task.TaskID == "" || !strings.Contains(res.Task.Summary, "eligible") {
		t.Fatalf("unexpected claim: %+v", res.Task)
	}
	t.Log("claimed the eligible newer row — no bug")
}

// Repro 2: a brew row whose existing twin differs ONLY in topic (same
// question[:200], same answer) is non-brew → the guard must block; instead
// it inserts a second row (guard misses the exact-key topic half).
func TestReproBrewGuardTopicMiss(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(dir, "mem.db"))
	// Seed a non-brew row via the official store path.
	if _, err := MemoryStore("human topic A", "How do we brew sessions safely? mixed 中英 keywords", "answer body describing the brewing loop", "human", []string{"kw1"}); err != nil {
		t.Fatal(err)
	}
	qa := &brewDistillReply{
		Topic:    "totally different brew topic",
		Question: "How do we brew sessions safely? mixed 中英 keywords",
		Answer:   "answer body describing the brewing loop",
		Keywords: []string{"kw1", "kw2", "kw3", "kw4", "kw5", "kw6"},
	}
	blocked, err := brewGuardBlocked(qa)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatalf("BUG REPRODUCED: brewGuardBlocked missed the exact-key twin — MemoryStore dedups on (question[:200], topic-pair), guard only compares question vs the FULL stored column")
	}
	t.Log("guard blocked — no bug")
}

// Repro 3 (bug028, t-1788053263): serve claims whatever pending row is oldest;
// when that row carries an empty payload (human-managed row), runTaskRun
// refuses it and os.Exit(1)s — the daemon dies instead of skipping the row.
// The queue layer must never hand a claimer an empty-payload row: rows whose
// payload is ” are not dispatchable work.
func TestReproServeClaimsEmptyPayloadRow(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	dir := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(dir, "mem.db"))
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	// Oldest pending row is a human-managed ledger entry: no payload.
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "ledger-only row", Tags: "pa-queue", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "dispatchable newer", Payload: "do the work", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	res, err := TaskNextFiltered("tester", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Empty {
		t.Fatal("queue unexpectedly empty")
	}
	if strings.TrimSpace(res.Task.Payload) == "" {
		t.Fatalf("BUG REPRODUCED: serve claimed an empty-payload row — runTaskRun will refuse it and os.Exit(1), killing the daemon")
	}
	t.Log("claimer skipped the empty-payload row — no bug")
}

// Repro 4 (t-1788053263-1): migrating a legacy four-status ledger REBUILDs
// the tasks table via RENAME. SQLite ≥3.25 rewrites same-table trigger
// bodies to follow the rename (ON tasks → ON tasks_old), so the DROP of
// tasks_old silently deletes tasks_ai/tasks_au/tasks_ad; ensureTasksSchema
// then recreates them, but the migrated rows were never indexed into the
// (empty) FTS companions and no 'rebuild' rescue runs — the rows are
// permanently invisible to tasks_fts/tasks_tri search.
func TestReproLegacyMigrationLosesFTSRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	ctx := context.Background()
	db, err := sql.Open("sqlite", LedgerDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	// openTasksDB guards on the distilled_qa + memory_feedback markers.
	for _, marker := range []string{
		`CREATE TABLE distilled_qa (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE memory_feedback (id INTEGER PRIMARY KEY)`,
	} {
		if _, err := db.Exec(marker); err != nil {
			t.Fatal(err)
		}
	}
	// Legacy four-status CHECK base table + ALREADY-current-shape (7-col)
	// FTS companions + 7-col sync triggers: the intermediate-build state —
	// FTS widening landed while the stored CREATE literal still lacked
	// 'pending', so tasksNeedsRebuild fires while migrateTasksFTS's shape
	// check matches (no rescue there — the repro hole).
	ftsCols := "task_id, session_id, summary, parent_task_id, tags, payload, source"
	legacy := []string{
		`CREATE TABLE IF NOT EXISTS tasks (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL UNIQUE,
	summary    TEXT NOT NULL,
	status     TEXT NOT NULL DEFAULT 'in_progress'
	           CHECK (status IN ('in_progress','completed','failed','cancelled')),
	session_id TEXT DEFAULT '',
	model      TEXT DEFAULT '',
	created_at REAL NOT NULL,
	updated_at REAL NOT NULL,
	ended_at   REAL
)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS tasks_fts USING fts5(
	` + ftsCols + `, content='tasks', content_rowid='id'
)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS tasks_tri USING fts5(
	` + ftsCols + `, content='tasks', content_rowid='id',
	tokenize='trigram'
)`,
		`CREATE TRIGGER IF NOT EXISTS tasks_ai AFTER INSERT ON tasks BEGIN
	INSERT INTO tasks_fts(rowid, ` + ftsCols + `)
	VALUES (new.id, new.task_id, new.session_id, new.summary, new.parent_task_id, new.tags, new.payload, new.source);
	INSERT INTO tasks_tri(rowid, ` + ftsCols + `)
	VALUES (new.id, new.task_id, new.session_id, new.summary, new.parent_task_id, new.tags, new.payload, new.source);
END`,
		`CREATE TRIGGER IF NOT EXISTS tasks_au AFTER UPDATE ON tasks BEGIN
	INSERT INTO tasks_fts(tasks_fts, rowid, ` + ftsCols + `)
	VALUES ('delete', old.id, old.task_id, old.session_id, old.summary, old.parent_task_id, old.tags, old.payload, old.source);
	INSERT INTO tasks_fts(rowid, ` + ftsCols + `)
	VALUES (new.id, new.task_id, new.session_id, new.summary, new.parent_task_id, new.tags, new.payload, new.source);
	INSERT INTO tasks_tri(tasks_tri, rowid, ` + ftsCols + `)
	VALUES ('delete', old.id, old.task_id, old.session_id, old.summary, old.parent_task_id, old.tags, old.payload, old.source);
	INSERT INTO tasks_tri(rowid, ` + ftsCols + `)
	VALUES (new.id, new.task_id, new.session_id, new.summary, new.parent_task_id, new.tags, new.payload, new.source);
END`,
		`CREATE TRIGGER IF NOT EXISTS tasks_ad AFTER DELETE ON tasks BEGIN
	INSERT INTO tasks_fts(tasks_fts, rowid, ` + ftsCols + `)
	VALUES ('delete', old.id, old.task_id, old.session_id, old.summary, old.parent_task_id, old.tags, old.payload, old.source);
	INSERT INTO tasks_tri(tasks_tri, rowid, ` + ftsCols + `)
	VALUES ('delete', old.id, old.task_id, old.session_id, old.summary, old.parent_task_id, old.tags, old.payload, old.source);
END`,
		// The intermediate build widened the BASE via the idempotent ALTER
		// pattern (P2 columns) — the 7-col triggers reference new.* columns
		// that must exist. The CHECK text in sqlite_master still lacks
		// 'pending' (ALTER never rewrites a CHECK), so tasksNeedsRebuild
		// still fires on this state.
		`ALTER TABLE tasks ADD COLUMN parent_task_id TEXT DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN tags TEXT DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN payload TEXT DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN source TEXT DEFAULT 'auto'`,
		`ALTER TABLE tasks ADD COLUMN claimed_by TEXT DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN claimed_at REAL`,
		`INSERT INTO tasks (task_id, summary, status, session_id, model, created_at, updated_at)
		 VALUES ('t-legacy-fts', 'rebuild the FTS companion', 'completed', 'sess-old', 'deepseek-old', 1700000000, 1700000001)`,
	}
	for _, stmt := range legacy {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("legacy setup: %v", err)
		}
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	// Reopen through the ledger path — the legacy-CHECK rebuild migration
	// runs inside ensureTasksSchema.
	t.Setenv("GOPI_MEMORY_DB", path)
	db2, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("openTasksDB after migration: %v", err)
	}
	defer db2.Close()

	// Triggers must exist AND point at tasks (not at the renamed tasks_old
	// — the rename-follow rewrite then DROP means they are gone entirely).
	for _, trig := range []string{"tasks_ai", "tasks_au", "tasks_ad"} {
		var sql string
		if err := db2.QueryRowContext(ctx,
			`SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?`, trig,
		).Scan(&sql); err != nil {
			t.Fatalf("BUG REPRODUCED: trigger %s lost by the legacy rebuild migration: %v", trig, err)
		}
		if strings.Contains(sql, "tasks_old") {
			t.Fatalf("trigger %s still references tasks_old: %s", trig, sql)
		}
	}

	// Sync triggers work on FUTURE rows; the MIGRATED rows must already be
	// indexed — search both companions for the legacy row.
	wait := func(idx, query string, wantHit bool) {
		t.Helper()
		var hit int
		rows, err := db2.QueryContext(ctx,
			`SELECT count(*) FROM `+idx+` WHERE `+idx+` MATCH ?`, query)
		if err != nil {
			t.Fatalf("%s MATCH: %v", idx, err)
		}
		rows.Next()
		if err := rows.Scan(&hit); err != nil {
			t.Fatal(err)
		}
		rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
		if wantHit && hit == 0 {
			t.Fatalf("BUG REPRODUCED: %s never indexed the migrated legacy row — no 'rebuild' rescue after the tasks_old DROP", idx)
		}
		if !wantHit && hit != 0 {
			t.Fatalf("%s unexpectedly matches %q (%d hits)", idx, query, hit)
		}
	}
	wait("tasks_fts", "rebuild", true)
	wait("tasks_tri", "companion", true)
	wait("tasks_fts", "nomatchanywhere", false)

	// And the companions must still be LIVE: a fresh insert must be visible
	// (proves the recreated triggers fire — not a frozen stale index).
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "post-migration fresh row for live check", Payload: "fresh",
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var hit int
		if err := db2.QueryRowContext(ctx,
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
	t.Log("legacy row indexed + companions live — no bug")
}
