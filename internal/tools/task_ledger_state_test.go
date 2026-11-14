package tools

// P2.5 verification-closure tests (devdoc/task-verify-loop.md): the
// verified/failed state machine (M4.20-B: reopened retired), the bug-flow adoption tally, the
// re-enqueue path (append-only + anti-loop cap) and the pre-P2.5 CHECK
// rebuild migration. Every test runs against its own GOPI_MEMORY_DB — never
// the real ~/.gopi db.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// taskIDFromQueued extracts the generated task id from TaskSubmit's
// "Task queued: t-<unix> (status=pending, …)" confirmation — tests never
// assume the t-N shape the auto-generated ids do not have.
func taskIDFromQueued(queued string) string {
	const marker = "Task queued: "
	i := strings.Index(queued, marker)
	if i < 0 {
		return ""
	}
	rest := queued[i+len(marker):]
	if j := strings.IndexByte(rest, ' '); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// newVerifyTestDB points GOPI_MEMORY_DB at a fresh temp file and returns the
// resolved path (the task_dispatch_test.go pattern).
func newVerifyTestDB(t *testing.T) string {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	return path
}

// TestEnsureTasksSchemaRebuildsPreP25CHECK pins the in-place migration: a
// ledger whose CHECK admits pending (P2) but not verified (P2.5) must be
// rebuilt so verify-loop status writes succeed on old DBs (SQLite cannot
// ALTER a CHECK — the rename-recreate-copy-drop rebuild path).
func TestEnsureTasksSchemaRebuildsPreP25CHECK(t *testing.T) {
	path := newVerifyTestDB(t)

	legacy := `CREATE TABLE tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL UNIQUE,
		summary TEXT NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('pending','claimed','in_progress','completed','failed','cancelled')),
		session_id TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		parent_task_id TEXT NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '',
		payload TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT 'auto',
		claimed_by TEXT NOT NULL DEFAULT '',
		claimed_at REAL,
		created_at REAL NOT NULL,
		updated_at REAL NOT NULL,
		ended_at REAL
	)`
	// openTasksDB refuses a bare file that is not a distilled knowledge
	// base — init the KB shell first, THEN overlay the legacy tasks table.
	if err := initKnowledgeBase(context.Background(), path); err != nil {
		t.Fatalf("init KB shell: %v", err)
	}
	db := testOpenDB(t, path)
	if _, err := db.Exec(`DROP TABLE tasks`); err != nil {
		// a fresh KB has no tasks table yet — nothing to drop, fine.
		t.Logf("no pre-existing tasks table: %v", err)
	}
	if _, err := db.Exec(legacy); err != nil {
		t.Fatalf("seed legacy table: %v", err)
	}
	// A REAL pre-P2.5 ledger carries its P2-shape FTS companions + sync
	// triggers (the memory.go mirror pattern) — seed those too, or the
	// rebuild's copy has no ai trigger to feed and the first au firing
	// corrupts (SQLITE_CORRUPT 267, the external-content desync symptom).
	fts, tri, ai, au, ad := tasksFTSDefs()
	for _, ddl := range []string{fts, tri, ai, au, ad} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("seed FTS defs: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO tasks (task_id, summary, status, created_at, updated_at)
		VALUES ('t-legacy', 'legacy row', 'pending', 1700000000, 1700000000)`); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	// First task-engine use on the legacy DB: ensureTasksSchema runs inside
	// the open path and must rebuild the CHECK to the P2.5 set, data intact.
	if _, err := TaskList(""); err != nil {
		t.Fatalf("TaskList on legacy db (triggers ensureTasksSchema): %v", err)
	}
	// legacy row survives the rebuild; verify via the legal transition walk.
	if _, err := TaskUpdate("t-legacy", TaskStatusInProgress); err != nil {
		t.Fatalf("legacy row must survive the rebuild: %v", err)
	}
	if _, err := TaskUpdateWithResult("t-legacy", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("legacy row must survive the rebuild: %v", err)
	}
	if _, err := TaskUpdateWithResult("t-legacy", TaskStatusVerified, "green"); err != nil {
		t.Fatalf("pre-P2.5 CHECK was not widened: %v", err)
	}
}

// testOpenDB opens the env-resolved ledger for the direct-SQL engine helpers
// (the verify loop takes the handle so one caller can batch several row reads
// on one connection).
func testOpenDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := openDB(path, false)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	//stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTaskBugAdoptionStatsAndTallyEcho(t *testing.T) {
	newVerifyTestDB(t)

	// Two bug tasks (one verified, one failed-by-verify) + one non-bug task that
	// must NOT enter the tally (substring "bug" inside other tags excluded
	// by the LIKE? No — LIKE '%bug%' over tags is the coarse pre-filter; the
	// exact-word rule applies to the ECHO trigger, the tally counts the
	// bug-tagged rows it finds).
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "fix leak", TaskID: "t-bug1", Tags: "bug", Source: "llm"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "fix crash", TaskID: "t-bug2", Tags: "bug,ui", Source: "llm"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "normal feature", TaskID: "t-feat", Tags: "feature", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := TaskUpdate("t-bug2", TaskStatusInProgress); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := TaskUpdateWithResult("t-bug2", TaskStatusCompleted, "delivered"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := TaskUpdateWithResult("t-bug2", TaskStatusVerified, "green"); err != nil {
		t.Fatalf("update: %v", err)
	}

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "third bug", TaskID: "t-bug3", Tags: "bug", Source: "llm"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !strings.Contains(out, "[bug-flow]") {
		t.Fatalf("bug-tagged submit must echo the tally line, got:\n%s", out)
	}
	// 3 bug-tagged rows: 1 verified + 2 pending (pending is 'other' in the
	// verified/failed outcome split — the tally measures outcomes).
	if !strings.Contains(out, "tag bug: 3") || !strings.Contains(out, "verified 1") ||
		!strings.Contains(out, "other 2") {
		t.Fatalf("tally wrong, got:\n%s", out)
	}

	// A non-bug task carrying a tag that merely CONTAINS "bug" mid-token
	// ("debug-ui") is NOT bug work — the shared population query excludes
	// it, and so does the echo trigger (hasBugTagFamily). The submit below
	// exercises the echo side; TestBugPopulationMatchesTokenRule pins the
	// tally side against the same row.
	quiet, err := TaskSubmit(TaskSubmitOptions{Summary: "plain", Tags: "debug-ui", Source: "human"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if strings.Contains(quiet, "[bug-flow]") {
		t.Fatalf("non-bug submit must not echo a tally, got:\n%s", quiet)
	}
	if !strings.Contains(quiet, "tags=debug-ui") {
		t.Fatalf("mid-token tag must survive un-normalized in the confirmation, got:\n%s", quiet)
	}

	// A terminal-tier bug row counts in the reissued bucket (M4.20-B): the
	// tally's reopened counter reads legacy reopened rows + failed rows.
	// Direct fixture stamp: the rank ladder forbids completed→failed, and
	// the reissued bucket contract is about the tally's COUNTING, not the
	// local transition gate (which M4.20-C's reissue replaces).
	db, closeDB, err2 := taskSyncDB()
	if err2 != nil {
		t.Fatalf("taskSyncDB: %v", err2)
	}
	defer closeDB()
	if _, err := db.Exec(`UPDATE tasks SET status = 'failed' WHERE task_id = 't-bug3'`); err != nil {
		t.Fatalf("stamp failed: %v", err)
	}
	out2, err := TaskBugAdoptionStats(context.Background(), db)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(out2, "tag bug: 3") || !strings.Contains(out2, "verified 1") ||
		!strings.Contains(out2, "reopened 1") || !strings.Contains(out2, "other 1") {
		t.Fatalf("failed verdict must land in the reissued bucket, got:\n%s", out2)
	}

	// hasBugTag: exact comma-separated word match only — the NARROWER
	// question ("does this row carry the reserved marker"), distinct from
	// hasBugTagFamily's "is this row bug work" (pinned in
	// TestBugPopulationMatchesTokenRule).
	for _, c := range []struct {
		tags string
		want bool
	}{
		{"bug", true}, {"bug,ui", true}, {"ui,bug", true}, {"ui, bug ,x", true},
		{"", false}, {"bugflow", false}, {"debug", false}, {"ui", false},
	} {
		if got := hasBugTag(c.tags); got != c.want {
			t.Errorf("hasBugTag(%q)=%v want %v", c.tags, got, c.want)
		}
	}
}

// TestBugPopulationMatchesTokenRule pins the ONE definition of "bug work"
// (Rule 14) across its two faces: the Go token rule (hasBugTagFamily — the
// queue's priority tier) and its SQL twin taskBugPopulationWhere (the
// bug-flow adoption tally's population). A row whose only "bug" sits
// mid-token ("debug-ui", "wbug") must be excluded from BOTH readers, and a
// bug-family token ("bug", "bug033", "bugflow") included by BOTH.
//
// Regression guard: before the fix the tally ran `tags LIKE '%bug%'` and
// counted the mid-token row — a measurement that silently over-counts its
// own population. This test fails on that query (the SQL face needs the
// fragment to see the mid-token rows excluded).
func TestBugPopulationMatchesTokenRule(t *testing.T) {
	newVerifyTestDB(t)
	ctx := context.Background()

	cases := []struct {
		id, tags string
		want     bool
	}{
		{"t-exact", "bug", true},
		{"t-with-other", "bug,ui", true},
		{"t-leading-other", "ui,bug", true},
		{"t-spaced", "ui, bug ,x", true},
		{"t-numbered", "bug033,serve", true},
		{"t-family-word", "bugflow", true}, // family: the token STARTS with bug
		{"t-mid-token", "debug-ui", false}, // contains bug, is not bug work
		{"t-prefixed", "wbug", false},
		{"t-uppercase", "BUG", false}, // the token rule is case-sensitive
		{"t-empty", "", false},
		{"t-docs", "docs", false},
	}
	for _, c := range cases {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: "row " + c.id, TaskID: c.id, Tags: c.tags, Source: "llm"}); err != nil {
			t.Fatalf("submit %s: %v", c.id, err)
		}
	}

	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()

	bugRows := 0
	for _, c := range cases {
		// Go face: the token rule and the priority tier must agree.
		if got := hasBugTagFamily(c.tags); got != c.want {
			t.Errorf("hasBugTagFamily(%q)=%v want %v", c.tags, got, c.want)
		}
		if got := taskTagPriority(c.tags) == taskPriorityBug; got != c.want {
			t.Errorf("taskTagPriority(%q) bug tier=%v want %v", c.tags, got, c.want)
		}
		// SQL face: the shared population fragment must agree with it.
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM tasks WHERE task_id = ? AND `+taskBugPopulationWhere,
			c.id).Scan(&n); err != nil {
			t.Fatalf("population query %s: %v", c.id, err)
		}
		if got := n == 1; got != c.want {
			t.Errorf("SQL population for tag %q (%s) = %d want %v", c.tags, c.id, n, c.want)
		}
		if c.want {
			bugRows++
		}
	}

	// The tally READS that population: exactly the bug-family rows, never
	// the mid-token ones (the LIKE '%bug%' query reported all of them).
	out, err := TaskBugAdoptionStats(ctx, db)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	want := fmt.Sprintf("tag bug: %d", bugRows)
	if !strings.Contains(out, want) || !strings.Contains(out, fmt.Sprintf("other %d", bugRows)) {
		t.Fatalf("tally must count exactly the %d bug-family rows, got:\n%s", bugRows, out)
	}
	if !strings.Contains(out, "verified 0") {
		t.Fatalf("no row has a verdict yet, got:\n%s", out)
	}

	// And the narrow marker predicate stays narrow (the submit echo): a
	// family token that is not the reserved tag itself does not echo.
	for _, c := range []struct {
		tags string
		want bool
	}{
		{"bug", true}, {"ui, bug ,x", true},
		{"bugflow", false}, {"bug033", false}, {"debug-ui", false}, {"BUG", false},
	} {
		if got := hasBugTag(c.tags); got != c.want {
			t.Errorf("hasBugTag(%q)=%v want %v", c.tags, got, c.want)
		}
	}
}
