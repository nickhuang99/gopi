package tools

// write_task_area tool — the in-session analog of the task-ledger writeback
// (POC decisive result: a submit tool forces one-item-at-a-time execution,
// 9/9 vs 0/3 on a 5-item chain; devdoc/memory-aware-agent-poc-20260904.md).
//
// The task LEDGER (tasks table) is the CROSS-session queue: worker claim /
// run / writeback, one gopi run per task. write_task_area is SESSION-internal
// — the subtask working area WITHIN one gopi run: the model works one item at
// a time, analyses first, then commits a stable CONCLUSION per item via this
// tool. The framework records the conclusion into an append-only,
// session-scoped area and treats the item's working frame as committed (the
// completed working region may be dropped/trimmed on the next round; the
// conclusion is carried forward as authoritative context, never
// implied-but-missing — H4 in the POC showed a missing-but-referenced
// conclusion turns into fabricated facts).
//
// Storage: the shared gopi memory DB (memoryDBPath) — same DB as the task
// ledger, scoped by session_id. A table is added through the SAME
// ensureKnowledgeBase/migrateDefaultTriggers heal paths as the tasks ledger,
// so a pre-feature DB gains it on its existing migration path and a foreign
// DB is never mutated (the table lives only in gopi-owned DBs).

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// taskAreaWriteTimeout bounds the write path's BOOTSTRAP step: opening the
	// shared memory DB (creating the file and running the full knowledge-base
	// schema init on a cold DB — 40+ DDL statements, measured ~10-17ms on a
	// memory-backed scratch root) PLUS the working-area schema heal
	// (session_conclusions table + index — also DDL, also fsynced on a cold
	// DB), i.e. everything that happens BEFORE the session-scoped row work.
	//
	// It is deliberately larger than taskAreaDBTimeout, and the two are NOT
	// interchangeable: this step is dominated by first-open/DDL cost
	// (file creation + schema statements + a WAL-close checkpoint fsync), not
	// by the session-scoped query work taskAreaDBTimeout was sized for. The
	// 5s value that used to cover BOTH was the root cause of the flake
	// TestT2_E3_ResumeCarriesForwardCommittedConclusions
	// ("write_task_area: schema: context deadline exceeded"/"context deadline
	// exceeded"): on this host a cold DB under I/O load needs 5-7s, and a
	// peer still inside its 15s SQLite busy wait (dbBusyTimeoutMSec) blocks
	// the open for even longer. Measured A/B: the same cold-open cycle takes
	// ~10-17ms idle, 5-7s+ under load — the budget must fit the slow case,
	// since the write path cannot skip the file it is about to write to.
	// (Verified the DDL belongs here too: with the schema heal left on the 5s
	// query budget, an unpinned boot-device scratch root under load still
	// produced "write_task_area: schema: context deadline exceeded".)
	// Analysis: devdoc/write-task-area-schema-timeout.md.
	taskAreaWriteTimeout = 30 * time.Second

	// taskAreaDBTimeout bounds the SESSION-SCOPED row work (insert + count)
	// that runs AFTER the bootstrap, plus the read path's queries. Short: the
	// tool must never stall a round on a hung query; the session ledger is
	// best-effort bookkeeping, not the task itself.
	taskAreaDBTimeout = 5 * time.Second
)

// bootstrapTimeoutEnvName lets an operator widen/confirm the bootstrap budget
// (tests use it to force the timeout branch — Rule 2: this is the ONE place
// it is read).
const bootstrapTimeoutEnvName = "GOPI_TASK_AREA_BOOTSTRAP_TIMEOUT_SEC"

// sessionConclusionsTable is the append-only working-area record. One row per
// committed subtask step; session_id scopes it to the gopi run (the session
// UUID from the session logger). append-only: an item is never rewritten —
// conclusions are stable facts the framework may carry forward verbatim.
func sessionConclusionsTable() string {
	return `CREATE TABLE IF NOT EXISTS session_conclusions (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		task_id    TEXT NOT NULL,
		status     TEXT NOT NULL CHECK (status IN ('completed')),
		conclusion TEXT NOT NULL,
		analysis   TEXT DEFAULT '',
		created_at REAL NOT NULL
	)`
}

// sessionConclusionsIndex scopes reads to one session in creation order.
func sessionConclusionsIndex() string {
	return `CREATE INDEX IF NOT EXISTS idx_session_conclusions_session
		ON session_conclusions(session_id, created_at)`
}

// ensureSessionConclusionsSchema creates the append-only session working-area
// table (idempotent — the ensureSchema pattern, safe on every open).
func ensureSessionConclusionsSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, sessionConclusionsTable()); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, sessionConclusionsIndex())
	return err
}

// WriteTaskAreaOptions carries the write_task_area tool arguments.
type WriteTaskAreaOptions struct {
	SessionID          string // gopi run session UUID ("" = caller forgot it — rejected)
	TaskID             string // the subtask/item id, e.g. "1" .. "5" of the chain
	Status             string // "completed" (only status the tool accepts)
	Conclusion         string // REQUIRED compact final result / decision / key facts
	CompressedAnalysis string // optional; omitted = analysis discarded
}

// WriteTaskArea records one committed subtask step into the session working
// area (append-only). The conclusion is REQUIRED and non-empty (a step
// without a stable conclusion is not committed — free-text "all done" has no
// parseable item completion, which is exactly the failure mode this tool
// removes). Returns a framework-visible receipt the model sees as the
// committed-state signal.
func WriteTaskArea(opts WriteTaskAreaOptions) (string, error) {
	// task_id is whitespace-normalized THROUGH THE SEAM (see taskIDNormalize):
	// the working-area row is keyed by the LEDGER id under that session, so a
	// padded id would key a SECOND row for the same task.
	opts.TaskID = taskIDNormalize(opts.TaskID)
	if strings.TrimSpace(opts.SessionID) == "" {
		return "", fmt.Errorf("write_task_area: session_id must not be empty (framework wiring error, not a model fix)")
	}
	if opts.TaskID == "" {
		return "", fmt.Errorf("write_task_area: task_id must not be empty")
	}
	if strings.TrimSpace(opts.Status) != "completed" {
		return "", fmt.Errorf("write_task_area: status %q not accepted (only \"completed\")", opts.Status)
	}
	if strings.TrimSpace(opts.Conclusion) == "" {
		return "", fmt.Errorf("write_task_area: conclusion is required (commit a compact final result / decision / key facts, not an empty step)")
	}

	db, err := bootstrapWriteAreaDB()
	if err != nil {
		return "", fmt.Errorf("write_task_area: %w", err)
	}
	defer db.Close()
	id, err := insertTaskAreaConclusion(db, opts)
	if err != nil {
		return "", fmt.Errorf("write_task_area: insert: %w", err)
	}
	queryCtx, cancelQuery := context.WithTimeout(context.Background(), taskAreaDBTimeout)
	defer cancelQuery()
	seq, err := sessionConclusionCount(queryCtx, db, opts.SessionID)
	if err != nil {
		seq = -1
	}
	return fmt.Sprintf(
		"write_task_area: committed subtask %q (record #%d, %d total this session). "+
			"Conclusion recorded — this step's working region is committed and may be "+
			"dropped/trimmed on the next round; the conclusion will be carried forward "+
			"as authoritative context.", opts.TaskID, id, seq), nil
}

// bootstrapTimeoutProbe observes the budget the write path resolved (nil in
// production). It exists so a test can prove the budget knob reaches the REAL
// call path instead of only the resolver — the failure recipe documented in
// devdoc/write-task-area-schema-timeout.md is "widen the knob", which is only
// true if the knob is read here.
// bootstrapWriteAreaDB resolves the write path's bootstrap budget and opens
// the shared DB inside it, separating the two failure modes that a single
// deadline used to conflate: the BUDGET expiring (environment too slow — see
// taskAreaWriteTimeout) versus the OPEN ITSELF failing (foreign/owned/corrupt
// DB, a real refusal). Without the ctx.Err() check both surfaced as a bare
// "context deadline exceeded", which is what made the flake look like a
// schema bug (Rule 15: report the truth, name the constant).
var bootstrapTimeoutProbe func(time.Duration)

func bootstrapWriteAreaDB() (*sql.DB, error) {
	budget := taskAreaWriteBootstrapTimeout()
	if bootstrapTimeoutProbe != nil {
		bootstrapTimeoutProbe(budget)
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, classifyBootstrapFailure(ctx, budget, err)
	}
	// The working-area schema lives INSIDE the bootstrap budget, not the query
	// budget: on a cold DB this is a real write (CREATE TABLE + CREATE INDEX,
	// each fsynced — the WAL close-time checkpoint), the same cost class as
	// the open above. Bounding it with the short session-query budget
	// reproduced the original flake verbatim
	// ("write_task_area: schema: context deadline exceeded") on a boot-device
	// scratch root under I/O load, AFTER the open itself had been given the
	// bootstrap budget — i.e. the fix is the cost class, not one wider
	// deadline.
	if err := ensureSessionConclusionsSchema(ctx, db); err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return nil, classifyBootstrapFailure(ctx, budget, err)
	}
	return db, nil
}

// classifyBootstrapFailure decides WHAT a bootstrap failure means. The ctx is
// the ONLY honest witness: when it has expired the deadline ended the
// bootstrap (the open or the working-area schema heal — environment too slow,
// the DB never refused anything), otherwise the call itself failed and its
// own error must reach the caller unmodified (a foreign/read-only/corrupt DB
// is a real refusal, not a timeout).
func classifyBootstrapFailure(ctx context.Context, budget time.Duration, err error) error {
	if ctx.Err() == nil {
		return err
	}
	// no-wrap: the open error stays OUT of the chain on THIS arm by design — the
	// budget is the finding, not the DB. Wrapping would make
	// errors.Is(reported, openErr) true and erase the distinction the whole
	// function exists to draw (pinned by
	// TestWriteTaskAreaBootstrapExpiryIsReportedAsABudget).
	return fmt.Errorf("bootstrap budget %v expired while opening/initializing the memory DB (%v): "+
		"the cost here is the COLD-DB knowledge-base schema init + WAL close sync, not the session query — "+
		"raise %s", budget, err, bootstrapTimeoutEnvName)
}

// taskAreaWriteBootstrapTimeout is the ONE place the bootstrap budget (and
// its env override) is resolved (Rule 2). A non-positive override is ignored
// — a timeout of zero would fire immediately and turn a misconfiguration into
// a silent hard failure.
func taskAreaWriteBootstrapTimeout() time.Duration {
	if v := os.Getenv(bootstrapTimeoutEnvName); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return taskAreaWriteTimeout
}

// insertTaskAreaConclusion bounds the session-scoped insert with the SHORT
// per-query budget, so the long bootstrap budget above never leaks into the
// step it was never sized for. NOTE: SQLite's own busy_timeout
// (dbBusyTimeoutMSec = 15s) outlasts this budget, so a write blocked behind a
// peer's lock reports the busy/step error once the budget expires — a real
// environment signal, but not a query that hung on its own. It is named here
// for that reason: a bare "insert: context deadline exceeded" would tell an
// operator nothing about WHICH budget or WHY.
func insertTaskAreaConclusion(db *sql.DB, opts WriteTaskAreaOptions) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), taskAreaDBTimeout)
	defer cancel()
	res, err := db.ExecContext(ctx,
		`INSERT INTO session_conclusions (session_id, task_id, status, conclusion, analysis, created_at)
		 VALUES (?, ?, 'completed', ?, ?, ?)`,
		strings.TrimSpace(opts.SessionID),
		taskIDNormalize(opts.TaskID),
		strings.TrimSpace(opts.Conclusion),
		strings.TrimSpace(opts.CompressedAnalysis),
		float64(time.Now().UnixNano())/1e9,
	)
	if err != nil {
		// no-wrap: the %v renders the DURATION budget, which is the finding; the
		// error itself IS wrapped with %w on the same line. A second %w for the
		// budget is impossible — it is not an error.
		return 0, fmt.Errorf("the session insert did not finish inside its %v budget (%w); "+
			"if the working-area DB is contended, SQLite's %dms busy wait is the longer of the two",
			taskAreaDBTimeout, err, dbBusyTimeoutMSec)
	}
	id, err := res.LastInsertId()
	if err != nil {
		// The row IS written; only the receipt's record number is unknown.
		// Report the id as -1 rather than fabricating one or failing a write
		// that already succeeded (Rule 15: record, never pretend).
		return -1, nil
	}
	return id, nil
}

// sessionConclusionCount returns how many conclusions this session committed
// (the framework's machine-trusted progress view).
func sessionConclusionCount(ctx context.Context, db *sql.DB, sessionID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_conclusions WHERE session_id = ?`, sessionID).Scan(&n)
	return n, err
}

// SessionConclusion is one committed step in the working area.
type SessionConclusion struct {
	TaskID     string
	Conclusion string
	Analysis   string // empty when the model chose to discard analysis
	CreatedAt  float64
}

// SessionConclusions returns a session's committed conclusions in creation
// order (the machine-trusted completed list, like the task ledger's
// completed view at task level). Absent DB / table (never stored anything)
// yields an empty slice with nil error — a session that never called the
// tool simply has no working area.
func SessionConclusions(sessionID string) ([]SessionConclusion, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskAreaDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		// A session that never wrote anything may predate the table / the
		// gopi DB; treat any open failure as "no conclusions" — the read-only
		// carry-forward must never fail a run.
		return nil, nil
	}
	defer db.Close()
	// The table may not exist yet (a session that never wrote conclusions, or
	// a DB opened before the feature shipped). Read-only carry-forward must
	// never fail a run — absent table = no conclusions.
	ok, err := tableExists(ctx, db, "session_conclusions")
	if err != nil || !ok {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, conclusion, COALESCE(analysis,''), created_at
		   FROM session_conclusions WHERE session_id = ? ORDER BY created_at, id`,
		sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionConclusion
	for rows.Next() {
		var c SessionConclusion
		if err := rows.Scan(&c.TaskID, &c.Conclusion, &c.Analysis, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
