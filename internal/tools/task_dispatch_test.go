package tools

import (
	"context"
	"database/sql"
	"github.com/nick/gopi/internal/heavytest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// P2 dispatcher tests: atomic claim race, state machine, FTS scope, legacy
// schema migration. GOPI_MEMORY_DB per test (never the real /home/user/.gopi db).
// ---------------------------------------------------------------------------

func TestTaskSubmitAndNextLifecycle(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "write the parser", Payload: "parse the config format X", Tags: " demo , split ,",
		Source: "human", SessionID: "sess-a", Model: "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if !strings.Contains(out, "status=pending") || !strings.Contains(out, "demo,split") {
		t.Fatalf("submit output: %q", out)
	}
	if out2, err := TaskSubmit(TaskSubmitOptions{Summary: "orphan", ParentTaskID: "t-ghost"}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("dangling parent must error, got %v / %q", err, out2)
	}

	// Claim: oldest first, payload carried.
	res, err := TaskNext("worker-1", "")
	if err != nil {
		t.Fatalf("TaskNext: %v", err)
	}
	// session_id is the RUN session, which does not exist yet for a pending row
	// (it is filled by the deferred stamp when the run starts); the session that
	// FILED the row is its own fact since the split (2026-10-28).
	if res.Empty || res.Task.Status != "claimed" || res.Task.ClaimedBy != "worker-1" ||
		res.Task.Payload != "parse the config format X" || res.Task.SubmitterSession != "sess-a" {
		t.Fatalf("claim result: empty=%v task=%+v", res.Empty, res.Task)
	}

	// Anti-self-reference: a pending task from sess-a is skipped by sess-a.
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "self work", Payload: "self work briefing", Source: "llm", SessionID: "sess-b"}); err != nil {
		t.Fatalf("submit 2: %v", err)
	}
	res, err = TaskNext("worker-2", "sess-b")
	if err != nil {
		t.Fatalf("TaskNext exclude: %v", err)
	}
	if !res.Empty {
		t.Fatalf("exclude-session must skip the sess-b task, claimed %+v", res.Task)
	}
	res, err = TaskNext("worker-2", "")
	if err != nil || res.Empty || res.Task.Summary != "self work" {
		t.Fatalf("claim without exclude: err=%v empty=%v", err, res.Empty)
	}

	// Run writeback completes the loop.
	if _, err := TaskStart(res.Task.TaskID, "worker-2"); err != nil {
		t.Fatalf("TaskStart: %v", err)
	}
	if _, err := TaskRunWriteback(res.Task.TaskID, TaskStatusCompleted, "done — elapsed=5s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	tk, err := TaskShow(res.Task.TaskID)
	if err != nil || !strings.Contains(tk, "status:         completed") || !strings.Contains(tk, "done — elapsed=5s") {
		t.Fatalf("show after writeback: %q err=%v", tk, err)
	}
}

// TestTaskNextConcurrentClaimRace: N goroutines race on the same queue —
// every task is claimed EXACTLY once (the CAS conditional-UPDATE guarantee —
// the correctness property under test), and the queue drains to empty.
// (Per-worker starvation is NOT asserted: OS scheduling may serialize
// goroutines; CAS correctness, not fairness, is the contract.)
func TestTaskNextConcurrentClaimRace(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const tasksN, workersN = 8, 4
	for i := 0; i < tasksN; i++ {
		if _, err := TaskSubmit(TaskSubmitOptions{Summary: "race task", Payload: "race briefing", TaskID: "t-race-" + string(rune('a'+i))}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}

	var mu sync.Mutex
	claimed := map[string]bool{}
	var wg sync.WaitGroup
	for w := 0; w < workersN; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				res, err := TaskNext("worker-"+string(rune('A'+w)), "")
				if err != nil {
					t.Errorf("worker %d: %v", w, err)
					return
				}
				if res.Empty {
					return
				}
				mu.Lock()
				if claimed[res.Task.TaskID] {
					t.Errorf("task %s claimed TWICE — CAS broken", res.Task.TaskID)
				}
				claimed[res.Task.TaskID] = true
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	if len(claimed) != tasksN {
		t.Errorf("claimed %d distinct tasks, want %d", len(claimed), tasksN)
	}
}

// TestTaskStateMachineRejectsIllegalTransitions: the migration table is the
// gate — illegal moves error naming the allowed set, legal loop moves pass.
func TestTaskStateMachineRejectsIllegalTransitions(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", TaskID: "t-sm"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// pending → completed is NOT on the table (must pass through claim/run).
	if _, err := TaskUpdate("t-sm", "completed"); err == nil ||
		!strings.Contains(err.Error(), "illegal status transition pending → completed") {
		t.Fatalf("pending→completed must be refused, got %v", err)
	}
	// pending → verified is judged-only: an artifact verdict can never land
	// on a queue row (P2.5 state machine).
	if _, err := TaskUpdate("t-sm", "verified"); err == nil ||
		!strings.Contains(err.Error(), "illegal status transition pending → verified") {
		t.Fatalf("pending→verified must be refused, got %v", err)
	}
	// The legal loop: pending → claimed → in_progress → completed.
	for _, step := range []string{TaskStatusClaimed, TaskStatusInProgress, TaskStatusCompleted} {
		if _, err := TaskUpdate("t-sm", step); err != nil {
			t.Fatalf("legal step %s refused: %v", step, err)
		}
	}
	// M4.20-B: completed → in_progress (rework) is now REFUSED — terminal
	// rows never reopen (the rank ladder is forward-only; the legitimate
	// rework is a REISSUE). failed → pending (retry) is refused equally.
	if _, err := TaskUpdate("t-sm", TaskStatusInProgress); err == nil {
		t.Fatalf("completed → in_progress must be REFUSED under the rank model (reissue instead)")
	}
	if _, err := TaskUpdate("t-sm", TaskStatusVerified); err != nil {
		t.Fatalf("completed → verified refused: %v", err)
	}
	if _, err := TaskUpdate("t-sm", TaskStatusPending); err == nil {
		t.Fatalf("verified → pending must be REFUSED (terminal, no exits)")
	}
}

// TestTaskSearchFindsNewColumns: payload/tags/source are in the FTS scope —
// an ASCII word hits the unicode61 index, a CJK fragment hits trigram.
func TestTaskSearchFindsNewColumns(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "fix indexer panic",
		Payload: "repro: run goroutineleak scenario then inspect pprof output",
		Tags:    "bug,并发",
		Source:  "llm",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	for _, q := range []string{"goroutineleak", "bug", "并发", "pprof"} {
		out, err := TaskSearch(q)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if !strings.Contains(out, "fix indexer panic") {
			t.Errorf("search %q missed the task:\n%s", q, out)
		}
	}
}

// TestEnsureTasksSchemaMigratesLegacyFourStatusDB: a pre-P2 ledger (four-
// status CHECK, three-column FTS) is migrated IN PLACE — old rows survive
// verbatim, pending becomes insertable, the FTS indexes gain the new columns.
func TestEnsureTasksSchemaMigratesLegacyFourStatusDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	ctx := context.Background()
	db, err := sql.Open("sqlite", LedgerDSN(path))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// The ledger only lands in a gopi-owned DB (openTasksDB guards on the
	// distilled_qa + memory_feedback tables) — seed both markers.
	if _, err := db.Exec(`CREATE TABLE distilled_qa (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed distilled_qa guard: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE memory_feedback (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed feedback guard: %v", err)
	}
	// Build the LEGACY four-status table + its 3-column FTS companions
	// (verbatim pre-P2 literals).
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
	task_id, session_id, summary, content='tasks', content_rowid='id'
)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS tasks_tri USING fts5(
	task_id, session_id, summary, content='tasks', content_rowid='id',
	tokenize='trigram'
)`,
		`CREATE TRIGGER IF NOT EXISTS tasks_ai AFTER INSERT ON tasks BEGIN
	INSERT INTO tasks_fts(rowid, task_id, session_id, summary)
	VALUES (new.id, new.task_id, new.session_id, new.summary);
	INSERT INTO tasks_tri(rowid, task_id, session_id, summary)
	VALUES (new.id, new.task_id, new.session_id, new.summary);
END`,
		`CREATE TRIGGER IF NOT EXISTS tasks_au AFTER UPDATE ON tasks BEGIN
	INSERT INTO tasks_fts(tasks_fts, rowid, task_id, session_id, summary)
	VALUES ('delete', old.id, old.task_id, old.session_id, old.summary);
	INSERT INTO tasks_fts(rowid, task_id, session_id, summary)
	VALUES (new.id, new.task_id, new.session_id, new.summary);
	INSERT INTO tasks_tri(tasks_tri, rowid, task_id, session_id, summary)
	VALUES ('delete', old.id, old.task_id, old.session_id, old.summary);
	INSERT INTO tasks_tri(rowid, task_id, session_id, summary)
	VALUES (new.id, new.task_id, new.session_id, new.summary);
END`,
		`CREATE TRIGGER IF NOT EXISTS tasks_ad AFTER DELETE ON tasks BEGIN
	INSERT INTO tasks_fts(tasks_fts, rowid, task_id, session_id, summary)
	VALUES ('delete', old.id, old.task_id, old.session_id, old.summary);
	INSERT INTO tasks_tri(tasks_tri, rowid, task_id, session_id, summary)
	VALUES ('delete', old.id, old.task_id, old.session_id, old.summary);
END`,
		`INSERT INTO tasks (task_id, summary, status, session_id, model, created_at, updated_at)
		 VALUES ('t-legacy', 'the legacy row', 'completed', 'sess-old', 'deepseek-old', 1700000000, 1700000001)`,
	}
	for _, stmt := range legacy {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("legacy setup: %v", err)
		}
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	// Reopen through the tasks ledger path: migration must run silently.
	t.Setenv("GOPI_MEMORY_DB", path)
	db2, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("openTasksDB after migration: %v", err)
	}
	defer db2.Close()

	// 1) Legacy row survived verbatim (append-only, zero data loss).
	tk, err := taskByID(ctx, db2, "t-legacy")
	if err != nil || tk == nil {
		t.Fatalf("legacy row lost: %v", err)
	}
	if tk.Summary != "the legacy row" || tk.Status != "completed" || tk.Model != "deepseek-old" ||
		tk.SessionID != "sess-old" {
		t.Fatalf("legacy row mangled: %+v", tk)
	}
	// The P2 ALTER adds the column with the EMPTY default, so a pre-P2 row's
	// provenance reads empty — never recorded — instead of the retired "auto"
	// alias. That alias is not a neutral filler: tasksource.Normalize resolves
	// it to Merger, so leaving it on this ALTER would hand every historical row
	// a merge-path narration no merge path ever made.
	if tk.Source != "" || tk.ParentTaskID != "" || tk.Tags != "" || tk.Payload != "" {
		t.Fatalf("legacy row must get P2 defaults (source empty — its provenance was never recorded): %+v", tk)
	}
	if tk.RunMethod != "" || tk.RunCmd != "" {
		t.Fatalf("legacy row must default run provenance to '': %+v", tk)
	}

	// 2) The CHECK now admits pending — insert one and claim it.
	res, err := TaskNext("w", "")
	if err != nil || !res.Empty {
		t.Fatalf("migrated queue should be empty, got %+v err=%v", res, err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "post-migration task"}); err != nil {
		t.Fatalf("pending insert on migrated table: %v", err)
	}

	// 3) FTS indexes carry the new columns (pragma_table_info).
	for _, idx := range []string{"tasks_fts", "tasks_tri"} {
		rows, err := db2.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, idx)
		if err != nil {
			t.Fatalf("pragma %s: %v", idx, err)
		}
		cols := map[string]bool{}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
				t.Fatalf("pragma scan: %v", err)
			}
			cols[c] = true
		}
		rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
		for _, want := range tasksFTSColumns {
			if !cols[want] {
				t.Errorf("%s missing migrated column %q (have %v)", idx, want, cols)
			}
		}
	}

	// 4) The legacy row is still searchable post-migration (FTS rebuilt).
	out, err := TaskSearch("legacy row")
	if err != nil || !strings.Contains(out, "t-legacy") {
		t.Errorf("post-migration search for legacy row failed: %q err=%v", out, err)
	}
}
