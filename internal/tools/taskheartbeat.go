package tools

// Task heartbeat storage (M4.5 ⑦ backing): the two raw-fact liveness
// columns — tasklist_issued_at (a [TASK_LIST] block arrived) and
// last_toolcall_at (a tool batch started) — plus the query-side stale
// detector that turns them into events. All writes target the session's
// auto-registered ledger row (its t-<unix> task_id; session_id column holds
// the session UUID after consolidation) and are best-effort.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"syscall"
	"time"
)

// TaskHeartbeatNotifyEvent is the event name the stale detector fires
// through the GOPI_TASK_NOTIFY pipeline (cfg.events must list it —
// GOPI_TASK_NOTIFY_EVENTS=stale,...). Exported so a caller pinning its own
// allow-list names the constant instead of a bare "stale" (Rule 1b).
const TaskHeartbeatNotifyEvent = "stale"

// TaskHeartbeatConn is the agent-facing handle for the liveness touches
// (keeps database/sql out of internal/agent).
type TaskHeartbeatConn struct {
	ctx context.Context
	db  *sql.DB
}

// NotifySyntheticEvent fires one synthetic alarm event through the
// GO_TASK_NOTIFY pipeline WITHOUT a ledger row behind it — the agent-loop
// deadlock epic's loop-stuck (and any future session-level alarm) has a
// session id, not a task row, so the transition/stale helpers do not fit.
// Payload shape mirrors taskNotifyPayload (empty ledger fields), so
// subscribers parse both with the same reader. Delivery failures are logged
// to stderr and NEVER fatal (side-channel contract). Honors the events
// filter: an operator subscribed to specific events does not receive
// unsubscribed alarms; "all" receives everything.
func NotifySyntheticEvent(event, sessionID, model, summary string) {
	cfg := loadTaskNotifyConfig()
	if !cfg.enabled() || !cfg.events[event] {
		return
	}
	now := float64(time.Now().Unix())
	payload := taskNotifyPayload{
		Event:     event,
		TaskID:    sessionID, // session-level alarm: task_id == session id
		Status:    event,
		Summary:   summary,
		SessionID: sessionID,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if cfg.async {
		go deliverTaskNotify(cfg, payload)
		return
	}
	deliverTaskNotify(cfg, payload)
}

// OpenTaskHeartbeatConn opens the tasks ledger for heartbeat writes; nil
// when the DB cannot be opened (callers treat it as a no-op).
func OpenTaskHeartbeatConn(ctx context.Context) *TaskHeartbeatConn {
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil
	}
	return &TaskHeartbeatConn{ctx: ctx, db: db}
}

// Close releases the connection.
func (h *TaskHeartbeatConn) Close() error {
	if h == nil || h.db == nil {
		return nil
	}
	return h.db.Close()
}

func (h *TaskHeartbeatConn) close() error { return h.Close() }

// execTouchLastToolcall stamps the last_toolcall_at column on the session's row.
func (h *TaskHeartbeatConn) ExecTouchLastToolcall(taskID, sessionID string, now float64) error {
	return h.touch(taskLastToolcallAtColumn, taskID, sessionID, now)
}

// execTouchTasklist stamps tasklist_issued_at on the session's row.
func (h *TaskHeartbeatConn) ExecTouchTasklist(taskID, sessionID string, now float64) error {
	return h.touch(taskTasklistIssuedAtColumn, taskID, sessionID, now)
}

// ErrTaskHeartbeatHolderDead is the refusal a heartbeat write returns when the
// row's claim stamp names a holder on THIS host that is confirmed dead. It is
// an error, not a silent skip, because the condition it reports is the whole
// point of the guard: a row being kept alive by something that is NOT its
// holder must be visible to whoever reads the run's stderr.
var ErrTaskHeartbeatHolderDead = errors.New("heartbeat refused: the row's claimed holder is dead")

// touch writes one liveness column — by the auto-registered row's task_id
// (consolidated: task_id != session_id, so the session UUID alone cannot
// address the row), falling back to the session_id column when the row was
// registered pre-consolidation (task_id == session_id). No-op when the row
// is terminal (a finished task is not "alive", its timestamps stand).
//
// HOLDER GUARD (epic dead-holder-heartbeat, task 01a163df): a liveness write
// is what tells a READER "this row is being executed right now", so the writer
// must be attributable to the row's HOLDER. Address-only writes were not: the
// measured shape is a wrapper SIGKILLed while the child it launched survives
// with the run's cwd and env, and that survivor kept stamping the dead holder's
// row — so every reader saw a fresh heartbeat for a run that had ended. (It
// also defeated the stale sweep's candidate query, which was deleted
// 2026-11-05; the guard is kept because the DEFECT it fixes is independent of
// that consumer: a dead row must not read fresh to anyone.)
// taskHeartbeatHolderRefusal performs the check; every refusal names the row,
// the stamp, the pid and the measured reason (Rule 15).
func (h *TaskHeartbeatConn) touch(column, taskID, sessionID string, now float64) error {
	if taskID != "" {
		refusal, err := h.holderRefusal(taskID)
		if err != nil {
			return fmt.Errorf("touch %s: %w", column, err)
		}
		if refusal != nil {
			return fmt.Errorf("touch %s: %w", column, refusal)
		}
		res, err := h.db.ExecContext(h.ctx,
			`UPDATE tasks SET `+column+` = ? WHERE task_id = ? AND status = ?`,
			now, taskID, taskStatusInProgress)
		if err != nil {
			return fmt.Errorf("touch %s: %w", column, err)
		}
		//stylegate:ignore n==0 is the meaningful branch; a driver error yields the same 0 and same path
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}
	}
	// Fall back to the session_id column (legacy rows registered with
	// task_id = session_id; externally-submitted rows carrying this session
	// as submitter are never touched — they are not running).
	if sessionID == "" {
		return nil
	}
	refusal, err := h.holderRefusalBySession(sessionID)
	if err != nil {
		return fmt.Errorf("touch %s: %w", column, err)
	}
	if refusal != nil {
		return fmt.Errorf("touch %s: %w", column, refusal)
	}
	_, err = h.db.ExecContext(h.ctx,
		`UPDATE tasks SET `+column+` = ? WHERE session_id = ? AND status = ? AND task_id = ?`,
		now, sessionID, taskStatusInProgress, sessionID)
	if err != nil {
		return fmt.Errorf("touch %s: %w", column, err)
	}
	return nil
}

// holderRefusal reads the claim stamp of the row a touch is about to stamp and
// answers whether the write must be refused. A row that cannot be read is NOT
// a refusal: an unreadable ledger is a database error the caller already
// reports, and the fail-soft direction of this whole guard is "only a
// MEASURED-dead holder refuses a write" (see taskHeartbeatHolderRefusal).
func (h *TaskHeartbeatConn) holderRefusal(taskID string) (error, error) {
	var claimedBy sql.NullString
	// The predicate matches the UPDATE's own `status = in_progress`, on
	// purpose: a terminal row is a SILENT no-op on this path (a finished task
	// is not "alive", its timestamps stand), so the guard must not turn that
	// into a reported refusal for a row nobody is trying to revive.
	err := h.db.QueryRowContext(h.ctx,
		`SELECT `+taskClaimedByColumn+` FROM tasks WHERE task_id = ? AND status = ?`,
		taskID, taskStatusInProgress).Scan(&claimedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // nothing to guard: the UPDATE below is the one that reports it
	}
	if err != nil {
		return nil, err
	}
	return taskHeartbeatHolderRefusal(taskID, claimedBy.String), nil
}

// holderRefusalBySession is holderRefusal for the session-addressed leg, and it
// reads the SAME row the UPDATE targets (session_id = ?, task_id = ?) — the
// guard must never judge a different row than the one it stamps.
func (h *TaskHeartbeatConn) holderRefusalBySession(sessionID string) (error, error) {
	var claimedBy sql.NullString
	err := h.db.QueryRowContext(h.ctx,
		`SELECT `+taskClaimedByColumn+` FROM tasks WHERE session_id = ? AND task_id = ? AND status = ?`,
		sessionID, sessionID, taskStatusInProgress).Scan(&claimedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return taskHeartbeatHolderRefusal(sessionID, claimedBy.String), nil
}

// taskHeartbeatHolderRefusal decides whether one heartbeat write is refused.
//
// THE CRITERION IS "IS THE HOLDER ALIVE", not "is the writer the holder": in
// the healthy path the writer IS the holder, and a process cannot compare
// itself against a stamp it just wrote without inventing a second identity for
// the same fact. What the ledger can decide is narrower and sufficient — the
// stamped holder is GONE — and that single condition is exactly the shape the
// epic is about (the holder died, something else kept writing).
//
// FAIL-SOFT DIRECTION, deliberately the same as pidAlive's: a write is refused
// ONLY on a positive measurement of death. Every unknown keeps writing, because
// a heartbeat that is not written turns a healthy run into one the stale sweep
// kills:
//
//   - no stamp on the row            → write (an auto-registered session row has
//     no holder to judge; refusing here would starve every plain session)
//   - a stamp this host cannot parse → write (nothing was claimed, so nothing
//     is known to be dead)
//   - a FOREIGN-host stamp           → write (the pid names a different process
//     here; claimHolder's local=false is "do not act on this number", Rule 15)
//   - an UNREADABLE /proc            → write (degraded liveness, never death:
//     see taskHolderConfirmedDead)
//
// The refusal is WORDED for the reader who meets it in a log: which row, which
// stamp, which pid, and which measurement said dead.
func taskHeartbeatHolderRefusal(rowKey, claimedBy string) error {
	host, pid, local := claimHolder(claimedBy)
	if !local {
		return nil // no stamp, an unparsable stamp, or a foreign host: nothing measurable
	}
	if !taskHolderConfirmedDead(pid) {
		return nil
	}
	return fmt.Errorf("%w (row %s, holder %s:%d — process %d does not exist on this host; a heartbeat is a liveness claim and only its holder may make it)",
		ErrTaskHeartbeatHolderDead, rowKey, host, pid, pid)
}

// taskHolderConfirmedDead reports whether pid is MEASURABLY gone from this
// host. It is pidAlive's complement with one addition that the fail-soft
// contract needs: pidAlive answers "alive?" as `signal 0 failed → dead`, which
// folds ESRCH (the process does not exist — a MEASUREMENT of death) together
// with EPERM (it exists and is not ours — alive) and every other probe failure.
// A guard that refused on `!pidAlive` would therefore refuse on an unreadable
// or restricted /proc, which is the reverse of the direction this guard
// requires. So the death is established the same way pidAlive establishes it —
// the process liveness probe, plus the /proc state (a Z is terminated-but-not-
// reaped, which is exactly a SIGKILLed wrapper) — and a probe that returns ANY
// other failure is "cannot tell", which keeps the write.
func taskHolderConfirmedDead(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return pidIsZombie(pid) // exists; only an unreaped corpse counts as dead
	}
	return errors.Is(err, syscall.ESRCH)
}
