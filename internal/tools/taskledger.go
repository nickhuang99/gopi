package tools

// LEDGER SESSION-DIR HINTS — the ADDRESS for a session log whose directory is
// named by a task id the row does not carry (2026-09-15).
//
// The problem this solves (measured on the operator's live ledger, 4
// in_progress rows, 70,300 session dirs): a ledger row's session_id is the
// SESSION that submitted it — a worker session executes MANY rows, so several
// rows point at ONE session (four live rows resolved to a single submitter
// session). That session's log lives in a directory named by ANOTHER task id
// (`sessions/<submitter-task-id>/<session-id>.jsonl`), so the layouts a row can
// name from its own columns — `<session-id>/session.jsonl`, and
// `<task-id>/<session-id>.jsonl` for the task id the row carries — MISS, and
// every probe fell through to the whole-tree scan. The cost fix was therefore
// inert on the real ledger: 6.1-12.5s per `task list --status in_progress` view
// with ~152k getdents64, while the same view for any other --status answered in
// 0.04s (only in_progress rows are probed).
//
// The filesystem is not the only place that knows the directory: the LEDGER
// names it. The row group sharing a session_id includes the session's OWN row,
// and (task-session-log-path) that row's task_id IS the directory holding the
// session log. Resolving the group ONCE per process is one indexed read plus a
// stat per candidate, so the whole view costs a handful of syscalls instead of
// a tree walk per row.
//
// The tree scan stays as the fallback: a hint that matches nothing on disk
// (pruned/re-homed session) or an absent hint falls through to the unchanged
// scan, so no resolution the build had before can regress.

import (
	"context"
	"strings"
	"sync"
)

const (
	// taskLedgerHintLimit bounds the rows inspected per session id. The
	// session's own row is normally the FIRST candidate (newest first), and a
	// session submitted by many rows has no more than a handful; the cap keeps
	// the query bounded on a pathological group rather than enumerating it.
	taskLedgerHintLimit = 8
	// taskLedgerHintRowQuery selects the candidate directory names for one
	// session id: the rows that carry it, newest first. It walks
	// idx_tasks_status-comparable row sets by session_id (a plain column
	// scan over the ledger's small row count — 1,273 rows here — NOT the
	// sessions tree the resolution is trying to avoid).
	//
	// TWO COLUMNS, ONE QUESTION (2026-10-28): a session's rows are found by the
	// SUBMITTER column on every row filed since the split, and by `session_id` on
	// the rows that predate it (there the column still IS the submitter's
	// session) — plus on any row whose run session happens to be this id, which
	// is the ordinary case for a resumed row. Widening the predicate can only
	// find MORE candidates for a LOOKUP whose answer is then filtered against the
	// sessions tree, so a stale/extra hit costs a stat, never a wrong answer.
	taskLedgerHintRowQuery = `SELECT task_id FROM tasks WHERE ` + taskSubmitterColumnName + ` = ? OR session_id = ? ORDER BY created_at DESC, task_id LIMIT ?`
)

// taskSessionHintCache memoises the candidate directory names per session id
// for the process. WHY the memo matters: one `task list` render resolves the
// session log of EVERY in_progress row and several rows share one session (the
// submitter-session shape), so without it a group of N rows would pay N
// identical ledger reads. The cache is per process, and a one-shot CLI cannot
// carry a stale answer into a later command.
var (
	taskSessionHintMu    sync.Mutex
	taskSessionHintCache map[string][]string
)

// taskSessionDirHints returns the candidate session-DIR names for sessionID,
// newest row first: the task ids of the ledger rows carrying that session
// (the session's own row among them — its task id names the directory
// `sessions/<task-id>/<session-id>.jsonl`). Returns nil when the session is
// unknown to the ledger, when the lookup is unavailable (no ledger yet), or
// when the query fails — every one of those means "no address known" and hands
// the question to the caller's costlier passes (Rule 15: a failed hint is
// recorded as no-hint, never as an empty answer that suppresses the scan).
func taskSessionDirHints(sessionID string) []string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	taskSessionHintMu.Lock()
	if hints, ok := taskSessionHintCache[sessionID]; ok {
		taskSessionHintMu.Unlock()
		return hints
	}
	taskSessionHintMu.Unlock()

	hints := taskLedgerHintProbe(sessionID)
	if len(hints) == 0 {
		// A miss is NOT cached: the ledger may not have the row yet (the
		// write is in flight), and a cached miss would suppress a later
		// resolution for the rest of the process.
		return nil
	}
	taskSessionHintMu.Lock()
	if taskSessionHintCache == nil {
		taskSessionHintCache = make(map[string][]string)
	}
	taskSessionHintCache[sessionID] = hints
	taskSessionHintMu.Unlock()
	return hints
}

// taskLedgerHintProbe is the ledger read behind taskSessionDirHints — a SEAM so
// the cost contract can count it: a `task list --status in_progress` view must
// reach the sessions TREE zero times, and the number of ledger hint reads is
// the evidence that the resolution happened by lookup rather than not at all.
// Nothing else may rebind it (Rule 3: a seam, not a new tool).
var taskLedgerHintProbe = queryTaskSessionDirHints

// queryTaskSessionDirHints is taskLedgerHintProbe's production binding: the
// bounded row query above, with the DB opened for READ only and closed before
// returning (the resolution is a lookup, never a ledger write).
func queryTaskSessionDirHints(sessionID string) []string {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, taskLedgerHintRowQuery, sessionID, sessionID, taskLedgerHintLimit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil
		}
		if id := taskIDNormalize(taskID); id != "" {
			out = append(out, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return out
}

// SessionDirHintCounterForTest rebinds the ledger hint seam to a COUNTER and
// returns count() (hint reads so far) plus the restore. Like
// SessionScanCounterForTest it DELEGATES to the production read, so a test's
// count never changes what resolves — it only observes how the resolution was
// reached (lookup vs tree scan).
func SessionDirHintCounterForTest() (count func() int, restore func()) {
	var (
		mu sync.Mutex
		n  int
	)
	prev := taskLedgerHintProbe
	taskLedgerHintProbe = func(sessionID string) []string {
		mu.Lock()
		n++
		mu.Unlock()
		return prev(sessionID)
	}
	clearSessionHintCache()
	return func() int {
			mu.Lock()
			defer mu.Unlock()
			return n
		}, func() {
			taskLedgerHintProbe = prev
			clearSessionHintCache()
		}
}

// clearSessionHintCache drops the process-lifetime hint memo (the restore half
// of SessionDirHintCounterForTest; also the reset between two cost assertions).
func clearSessionHintCache() {
	taskSessionHintMu.Lock()
	taskSessionHintCache = nil
	taskSessionHintMu.Unlock()
}
