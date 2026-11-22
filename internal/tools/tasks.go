// Task ledger — task-as-memory. gopi 执行过的任务本身也是记忆的一部分:
// alongside distilled_qa (蒸馏经验) and memory_feedback (反馈事件), the
// tasks table records every executed task so later sessions can trace what
// was done (跨会话追溯/审计). Nick's iron rule: the ledger is append-only —
// records may be created, summaries edited and status changed, but NEVER
// deleted. There is deliberately no delete path anywhere in this file.
//
// Service position: the ledger is an AUDIT/TRAIL layer, not runtime control.
// It does not touch loop.go's tasklist/todoState/heartbeat/nudge machinery —
// the Stateless philosophy stands (runtime task state stays LLM-carried each
// round; the framework only reads as a fallback). Tasks live in the same
// memory.db as the rest of gopi's memory, opened read-write via the shared
// GOPI_MEMORY_DB resolution, and are exposed as CLI subcommands (gopi task
// ...), NOT as new LLM tools (Rule 3 — gopi session log / checkpoint precedent).
package tools

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/fullid"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/runend"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionid"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/tasksource"
	"github.com/nickhuang99/gopi/internal/tasktags"
)

// Task status values — the CHECK constraint on tasks.status mirrors these
// exactly (hardcoded in the CREATE TABLE literal, like the rest of the
// schema). Terminal statuses stamp ended_at; in_progress clears it.
//
// P2 task dispatcher (devdoc/task-dispatcher-p2.md): the ledger was upgraded
// from a passive audit layer to a WORK QUEUE — pending is the enqueue state
// (task_submit / gopi task submit), claimed is the atomic-pickup CAS state
// (task next). The four legacy statuses keep their exact meanings, so the
// auto-register harness hook (taskregister.go) is untouched.
//
// P2.5 verification closure (devdoc/task-verify-loop.md): verified and
// reopened complete the acceptance loop. verified is a terminal outcome
// (an independent gopi instance ran the fix's own tests green and stamped
// verify_evidence); reopened is the rejection path back into the queue —
// a fresh pending row is enqueued referencing the failed verification, so
// the claim→run→verify cycle can repeat without rewriting history.
const (
	taskStatusPending    = "pending"
	taskStatusClaimed    = "claimed"
	taskStatusInProgress = "in_progress"
	taskStatusCompleted  = "completed"
	taskStatusFailed     = "failed"
	taskStatusCancelled  = "cancelled"
	taskStatusVerified   = "verified"
	// taskStatusSuperseded (epic parent-release-statuses, 2026-10-07) is the
	// terminal status for a row whose WORK IS DONE ELSEWHERE: a hosted
	// migration (`gopi task send --cancel-source` moves the row to a peer
	// ledger, where a NEW row carries the work) and a supersession (the
	// dependency chain was re-cut and a successor row carries it).
	//
	// WHY IT EXISTS: `cancelled` was carrying BOTH meanings — "abandoned, no
	// work" and "the work is someone else's now" — and Rule 33's dependency
	// gate releases a parked child only on a release-set status, so a child
	// whose parent was MIGRATED (not abandoned) was parked forever while the
	// queue looked clean (Rule 15 silent failure). Measured on peer1's live
	// ledger 2026-10-07: 426 cancelled rows, exactly 1 parking a pending
	// child, and its reason is the three-step migration recipe verbatim.
	//
	// It is a DIFFERENT NAME for the decision `cancelled` already encodes —
	// never a licence to call abandoned work done. A row whose work is NOT
	// done stays `cancelled` (or `failed`), and both keep parking children.
	taskStatusSuperseded = "superseded"
	// taskStatusReopened is LEGACY-ONLY (M4.20-B: reopened 退役): the state no
	// longer participates in any transition — the only backward special case is
	// REISSUE (new pending row + old row terminal, append-only). The literal
	// stays in the set so legacy DB rows still open/list/sync without crashing;
	// taskAllowedTransitions has no reopened key (neither source nor target).
	taskStatusReopened = "reopened"
)

// taskStatusSet is the full CHECK-constrained status set (single source of
// truth mirrored by taskSchemaStatusCHECK below). P2.5 extends the P2 set
// with verified/reopened — the enqueue→run→verify loop's two new states.
var taskStatusSet = []string{
	taskStatusPending, taskStatusClaimed,
	taskStatusInProgress, taskStatusCompleted,
	taskStatusFailed, taskStatusCancelled,
	taskStatusVerified, taskStatusReopened,
	taskStatusSuperseded,
}

// taskStatusWritable is taskStatusSet WITHOUT the retired legacy statuses —
// the list every operator-facing remedy names. The distinction is the point
// (task fix/task-update-error-message): `reopened` stays in taskStatusSet so
// legacy rows keep opening/listing/syncing, but taskAllowedTransitions has no
// reopened key, so ANY `--status reopened` write is refused a step later.
// Offering it as a remedy therefore walked the operator into a SECOND failure
// — the exact shape that made a successful cancel look like a bug. A remedy
// must name only what the command can actually write.
var taskStatusWritable = func() []string {
	out := make([]string, 0, len(taskStatusSet))
	for _, s := range taskStatusSet {
		if s == taskStatusReopened {
			continue
		}
		out = append(out, s)
	}
	return out
}()

// taskStatusListForCLI is the acceptable-token list for a status FILTER
// (task list --status): the writable set, plus the retired token — a legacy
// row's status must stay queryable, so a read-only filter is deliberately
// wider than a write. Never used for a write's remedy list.
var taskStatusListForCLI = func() []string {
	return append(append([]string{}, taskStatusWritable...), taskStatusReopened)
}()

// Exported aliases for the status values above — the auto-register harness
// hook (internal/agent/taskregister.go) needs the terminal names to flip a
// session's record. These are ALIASES of the constants above, not a second
// set of literals, so the strings mirroring the SQL CHECK constraint have a
// single source of truth (Rule 2).
const (
	TaskStatusPending    = taskStatusPending
	TaskStatusClaimed    = taskStatusClaimed
	TaskStatusInProgress = taskStatusInProgress
	TaskStatusCompleted  = taskStatusCompleted
	TaskStatusFailed     = taskStatusFailed
	TaskStatusCancelled  = taskStatusCancelled
	TaskStatusVerified   = taskStatusVerified
	TaskStatusReopened   = taskStatusReopened
	TaskStatusSuperseded = taskStatusSuperseded
)

// TaskRowExists reports whether a task id names a row IN THIS LEDGER — the
// address resolver of a verb that accepts two different operands under one
// spelling (`gopi task archive <task-id>` retires a ROW; `gopi task archive
// <branch>` retires a BRANCH, epic verb-restructure 2026-10-05).
//
// WHY A SEPARATE PREDICATE AND NOT TaskStatusOf. This question is asked while
// PARSING an argv, where "no such row" is the ANSWER that selects the other
// operand — not a failure. TaskStatusOf returns an error for a missing row AND
// for an unreadable ledger, so a caller could not tell "this is a branch name"
// from "the database is broken", and the second one must never silently become
// a branch archive (Rule 15: no silent failure, in either direction).
//
// MEASURED LIMIT, stated so a later reader does not mistake it for a defect: an
// unreadable ledger is reported as (false, nil) — the same answer as "no such
// row" — because a verb that manufactures a refusal for a branch name the
// moment the ledger is unavailable would be worse than the ambiguity. The
// branch scan that follows answers for itself, and the row arm is reachable by
// every caller whose ledger reads.
func TaskRowExists(taskID string) bool {
	ok, _ := taskRowExists(taskID) //stylegate:ignore TaskRowExists answers with the boolean alone; the caller's branch scan answers for itself and the ledger-unavailable case is why the error is not propagated
	return ok
}

// taskRowExists is the error-reporting half: (exists, err) where err is the
// ledger failure a SELECT could not complete. It is what tests assert on and
// what TaskRowExists deliberately flattens.
func taskRowExists(taskID string) (bool, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return false, nil
	}
	ctx := context.Background()
	// READ-ONLY OPENER — this face is a PURE READ: its SELECTs are the whole of
	// what it does, and it calls no writer (openTasksDBReadOnly skips the tasks DDL
	// batch and takes no BEGIN IMMEDIATE, so a reader never queues on the writer
	// that holds the ledger).
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return false, fmt.Errorf("task row exists: %w", err)
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return false, fmt.Errorf("task row exists: %w", err)
	}
	return t != nil, nil
}

// TaskStatusOf reads a task row's CURRENT status — the verdict channel the
// merger's post-merge verify gate keys on (`gopi task verify` writes
// verified/failed onto the judged row). Returns an error when the ledger or
// the row is unavailable, so a caller can never mistake "no ledger" for a
// verdict.
//
// TWO defects were repaired here on 2026-11-21 (epic failed-resume-no-ground),
// and both are the same shape: a pure READ was shaped like a WRITER, so it
// answered with the writer's silence.
//
//  1. It opened through openTasksDB, the READ-WRITE opener, whose every open
//     runs the tasks DDL batch over a `_txlock=immediate` DSN — a BEGIN
//     IMMEDIATE, i.e. THE LEDGER'S WRITE LOCK — to read one row that nothing
//     writes. Behind a live writer that wait is dbBusyTimeoutMSec, and the
//     measured cost was 14.93s AND a failure (see ledgerReadTimeout's comment).
//     openTasksDBReadOnly is the opener built for exactly this: mode=ro +
//     query_only, no DDL, no write lock, so a reader never queues on a writer.
//  2. It reported an UNREADABLE ledger with the row's own absence. A caller
//     that must act differently on the two — the resume ground resolver is one,
//     and it renders a typed refusal naming the REPAIR — could not tell them
//     apart. ErrTaskRowAbsent is that distinction, and it is a sentinel so the
//     caller asks with errors.Is rather than by matching English (Rule 15).
func TaskStatusOf(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task status: empty task_id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), ledgerReadTimeout)
	defer cancel()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return "", fmt.Errorf("task status: open ledger: %w", err)
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", fmt.Errorf("task status: read row: %w", err)
	}
	if t == nil {
		return "", fmt.Errorf("task status: no task with task_id=%q%s: %w",
			taskID, taskIDPrefixRefusal(ctx, db, taskID), ErrTaskRowAbsent)
	}
	return t.Status, nil
}

// ErrTaskRowAbsent marks "this ledger holds no such row" — an ANSWER about the
// ledger's contents, as opposed to a failure to read them.
//
// It exists because the two facts have DIFFERENT REPAIRS and were being
// rendered with one sentence: a row that is absent is a wrong id (or another
// host's ledger), while a ledger that cannot be read is a lock, a permission or
// a schema the binary is behind. A caller that has to refuse — the resume
// ground resolver renders "recorded path / model / detail / repair" from a TYPED
// refusal — wraps this sentinel when it means the first (Rule 15).
var ErrTaskRowAbsent = errors.New("ledger holds no such row")

// TaskStatusVerified and TaskStatusFailed are the two verdict values the
// verify loop writes; aliased here so the gate compares against the SAME
// constants the ledger uses (Rule 14: never a bare literal).
// shape (t-<unix seconds>, numeric suffix on collision). Auto-generated
// task_ids are now uuid v7 (taskIDGenerate); these constants remain only
// so taskHashDisplay can recognize and render pre-uuid rows (the
// append-only ledger is never rewritten — legacy rows keep t- ids).
const (
	taskIDPrefix      = sessionid.TaskIDPrefix
	taskIDMaxAttempts = 10
)

// taskIDCollisionSep separates the unix-seconds body from the collision
// counter (t-<unix>-N). A task_id containing it is NOT a pure-number body,
// so taskHashDisplay falls back to the full id.
const taskIDCollisionSep = "-"

// taskDurationTruncMilli is the sub-second truncation for the running-duration
// render (38.2s not 38.283s): the list is a glance view, millisecond precision
// is already noise (Rule 1: no bare literals for formatting thresholds).
const taskDurationTruncMilli = 10

// tasksModelColumn is the tasks.model column added after the ledger's first
// release (commit 19a6eab stored model only as a show-time session-JSONL
// lookup — no persistence, so a pruned session log erased the model). The
// correct root-cause shape is model AS DATA: captured at task-creation time,
// stored in the row, rendered by list/show from the DB. ensureTasksSchema
// migrates pre-column ledgers in place (idempotent, ensureSchema pattern —
// the same self-healing style as tasksSchemaTable itself; no migration
// scripts). Existing rows keep model=” (rendered as "(unknown)") — the
// append-only ledger is never rewritten, so no backfill of old rows.
const tasksModelColumn = "ALTER TABLE tasks ADD COLUMN model TEXT DEFAULT ''"

// tasksInProgressAtColumnName is the execution-START stamp column
// (claimed-holder-freshness epic, 2026-09-12): the moment the row entered
// its CURRENT in_progress stretch. It is the REFERENT the claim stamp has
// always lacked — claimed_by/claimed_at record a claim (pending→claimed CAS,
// crash-recovery re-stamp), NOT "the process now executing", so without this
// column an in_progress row whose claim stamp predates the run it is credited
// with is indistinguishable from a live holder, and the zombie chain's pid
// layer vetoes the stale event forever (a SIGKILLed wrapper's stamp keeps
// vetoing). One name, referenced by the schema literal, the ALTER migration,
// the rebuild copy and the row projection (Rule 1b/13: no bare literal).
const tasksInProgressAtColumnName = "in_progress_at"

// The two M4.5 heartbeat columns (task-heartbeat epic): the ONLY columns that
// tick while a run is alive. taskLastToolcallAtColumn is stamped when a tool
// batch starts (the primary heartbeat); taskTasklistIssuedAtColumn when the
// harness receives a [TASK_LIST] block. Neither is updated_at — updated_at is
// the M4.13 sync/state anchor, frozen between transitions, and reading it as a
// heartbeat is exactly the "updated_at suspicious" confusion this epic
// answers. One name per column, referenced by the schema, the ALTER
// migrations, the projections, the SQL writer (taskheartbeat.go) and the
// sweeps (Rule 1b/13: a column name is never typed inline twice).
const (
	taskLastToolcallAtColumn   = "last_toolcall_at"
	taskTasklistIssuedAtColumn = "tasklist_issued_at"

	// taskClaimedByColumn is the row's HOLDER stamp ("hostname:pid"). The
	// heartbeat guard reads it before every liveness write (taskheartbeat.go:
	// taskHeartbeatHolderRefusal), so the column name is a referenced constant
	// rather than a spelling repeated at each new read site (Rule 1/13).
	taskClaimedByColumn = "claimed_by"
)

// The row's liveness facts — `last_toolcall_at` (a tool batch started) and
// `tasklist_issued_at` (a [TASK_LIST] block arrived) — are read through ONE
// arithmetic, taskRecordedLivenessAt, and the display is the only consumer
// left: "how stale is this run's tick-tock" is answered from the facts the row
// ITSELF recorded, and an absence renders as an absence (Rule 15) instead of
// being substituted by created_at.
//
// THE SQL HALF AND THE ROW-AGE FALLBACK ARE GONE WITH THE STALE SWEEP
// (2026-11-05). `taskLivenessInstantSQL` / `taskLastLivenessAt` / `taskStallHit`
// existed to feed ONE consumer — DetectStaleTasks' candidate query, which
// answered "how old is this row?" and took created_at as a fallback heartbeat
// when the row had never ticked. That detector, its `gopi task list --sweep`
// trigger, its persisted throttle stamp and its re-arm UPDATE are deleted (the
// operator's ruling: no process watches another process for it), so the
// question they answered has no asker. Keeping them would leave three exported
// spellings of a fallback that measures a ROW's age while reading like a
// HEARTBEAT — the exact substitution the display was fixed to stop making.

// taskRecordedLivenessAt is the newest liveness fact the row ITSELF recorded
// — last_toolcall_at or tasklist_issued_at, whichever is newer, and 0 when it
// recorded NEITHER. It is the display's subject: created_at is the row's
// creation, not its heartbeat, so it is never a substitute here (a row claimed
// three seconds ago carries a three-second-old claim stamp and a day-old
// created_at, and reporting the latter as "heartbeat stale" is a false
// verdict about a healthy run). taskLivenessKnown answers the same question as
// a boolean; the two agree by construction because this one is their shared
// arithmetic.
func taskRecordedLivenessAt(t *Task) float64 {
	if t == nil {
		return 0
	}
	var last float64
	if t.HasToolcall && t.LastToolcallAt > last {
		last = t.LastToolcallAt
	}
	if t.HasTasklistIssue && t.TasklistIssuedAt > last {
		last = t.TasklistIssuedAt
	}
	return last
}

// taskLivenessKnown reports whether the row recorded ANY liveness fact of its
// own (a tool batch or a tasklist block). created_at alone does not count: it
// is the row's CREATION, not a heartbeat, and the display renders that
// distinction (an absent fact is an absence, never a stale fact — Rule 15).
func taskLivenessKnown(t *Task) bool {
	return taskRecordedLivenessAt(t) > 0
}

// Process-identity columns (epic task-process-identity, 2026-09-13): the
// pid, its starttime tick count and its command line as recorded when the
// row entered in_progress. They exist because a pid is NOT an identity — the
// OS recycles numbers, so a stale row's pid can be owned by an unrelated
// process and every liveness reader then reports a dead run as "running"
// (taskproc.go carries the full argument and the verification ladder). One
// name per column, referenced by the schema literal, the ALTER migration, the
// rebuild copy, the row projection and the two writers (Rule 1b/13: a column
// name is never typed inline twice).
const (
	tasksProcPidColumnName   = "proc_pid"
	tasksProcStartColumnName = "proc_start_ticks"
	tasksProcCmdColumnName   = "proc_cmd"
)

// tasksLockPhaseColumnName is the column that carries the CLAIM-LOCK PROTOCOL's
// own progress — the fact the M4.21 lock exists to protect, which used to be
// expressed by borrowing the row's STATUS (claimed→in_progress was written when
// "both locks were acquired", not when a child process existed). Borrowing the
// process-lifecycle status for a lock-progress fact is exactly what made
// `claimed` and `in_progress` equivalent, so the lock fact now has its own
// column and the status keeps its own meaning:
//
//	''      no lock protocol ran — the single-node path (no sync peer declared),
//	        the sync switch off, or a skipped broadcast. RENDERED AS NOTHING:
//	        the default shape must carry no noise (epic constraint 5).
//	local   Phase-1 won the LOCAL CAS; no peer has confirmed yet.
//	both    every declared peer confirmed its side (双锁齐) — the lock is
//	        complete and execution may begin.
//
// It is written by ONE writer (TaskStampLockPhase) and it is NOT a status: a
// row reads `claimed` + lock_phase=local while the peer leg is still running,
// and `in_progress` only once a child process has actually been exec'd.
const tasksLockPhaseColumnName = "lock_phase"

const (
	// TaskLockPhaseLocal is Phase-1 only: the local CAS is won, no peer
	// confirmation has landed.
	TaskLockPhaseLocal = "local"
	// TaskLockPhaseBoth is 双锁齐: every declared peer confirmed.
	TaskLockPhaseBoth = "both"
)

// taskSubmitterColumnName is the column that carries the SUBMITTING session —
// the anti-self-reference key — split out of `session_id` (2026-10-28). ONE
// name, referenced by the schema literal, the ALTER migration, the INSERT and
// the claim funnel's exclusion predicate (Rule 1b/13: no bare literal).
//
// WHY THE SPLIT: `session_id` carried two meanings at once — ① the session that
// FILED the row (claim eligibility refuses a row filed by the claiming session)
// and ② the RUN's session, the uuid of its record under sessions/<task-id>/.
// The INSERT wrote ①, the deferred stamp fills ② only while the cell is EMPTY —
// so for every submitted row ② was unwritable for the row's whole life, and the
// column kept naming a session that never ran the row.
const taskSubmitterColumnName = "submitter_session"

// taskSubmitterColumnAlter is the idempotent in-place migration for the
// column above (the tasksP2Columns / tasksProcIdentityAlters pattern: a
// "duplicate column name" error IS the already-migrated case).
var taskSubmitterColumnAlter = "ALTER TABLE tasks ADD COLUMN " + taskSubmitterColumnName + " TEXT DEFAULT ''"

// tasksProcIdentityAlters are the three ALTER statements that add the
// identity columns to a pre-existing ledger — the tasksP2Columns pattern
// (idempotent, self-healing: a duplicate-column error is the "already
// migrated" case and is skipped by the migrator).
func tasksProcIdentityAlters() []string {
	return []string{
		"ALTER TABLE tasks ADD COLUMN " + tasksProcPidColumnName + " INTEGER",
		"ALTER TABLE tasks ADD COLUMN " + tasksProcStartColumnName + " INTEGER",
		"ALTER TABLE tasks ADD COLUMN " + tasksProcCmdColumnName + " TEXT DEFAULT ''",
		// The claim-lock progress column (see tasksLockPhaseColumnName): the
		// lock's own fact, kept out of the status on purpose.
		"ALTER TABLE tasks ADD COLUMN " + tasksLockPhaseColumnName + " TEXT DEFAULT ''",
	}
}

// taskSchemaStatusCHECK renders the CHECK constraint from taskStatusSet
// (single source of truth — adding a status to the slice extends both the
// CREATE literal and the rebuild migration's check).
func taskSchemaStatusCHECK() string {
	quoted := make([]string, len(taskStatusSet))
	for i, s := range taskStatusSet {
		quoted[i] = "'" + s + "'"
	}
	return strings.Join(quoted, ",")
}

// taskLiveStatusSQLList renders the NON-TERMINAL status set as an SQL literal
// list — the SAME three constants the Go readers compare a row's status
// against (Rule 1b/13: a status value is never typed inline in a query; the
// shape taskSchemaStatusCHECK already uses for the schema CHECK). Consumers:
// the branch-adoption ⑤ same-branch-once dedup gate.
func taskLiveStatusSQLList() string {
	return "'" + taskStatusPending + "','" + taskStatusClaimed + "','" + taskStatusInProgress + "'"
}

// taskTerminalStatusSQLList renders the TERMINAL status set as an SQL literal
// list — the complement of taskLiveStatusSQLList, derived from the SAME status
// constants (Rule 1b/13: never typed inline) so the two lists cannot drift.
//
// It exists for the resolve-row identity guard (resolveConflictRowIdentity in
// task_merge.go): a merge-conflict row that has already reached one of these
// statuses is a CONCLUDED decision about its branch, and the branch must never
// be handed to a second resolver. "Terminal" is exactly the predicate the
// ledger already applies when it stamps ended_at (terminalTaskStatus below is
// the Go half of the same pair) — so this list IS terminalTaskStatus's SQL
// spelling, and it carries every value that function admits.
//
// MEASURED TENSION, stated rather than hidden: this list is ALSO read as
// "concluded decisions" (task_merge.go's per-branch count line), and
// `superseded` belongs there for exactly that reason — but `cancelled` and
// `failed` are concluded too, so the two readings are the same set and one
// spelling serves both.
func taskTerminalStatusSQLList() string {
	return "'" + taskStatusCompleted + "','" + taskStatusFailed + "','" +
		taskStatusCancelled + "','" + taskStatusVerified + "','" + taskStatusSuperseded + "'"
}

// taskSchemaTable / tasksSchemaIndexStatus / tasksSchemaIndexTaskID are the
// hardcoded schema literals (the ensureSchema pattern: CREATE IF NOT EXISTS
// idempotent self-healing — a pre-tasks memory.db gets the table on first
// task use, zero migration scripts). status carries a CHECK constraint so a
// bad value is rejected by the engine itself, not just the CLI front-end.
//
// P2 dispatcher columns (devdoc/task-dispatcher-p2.md, R6.1): parent_task_id
// (divide-and-conquer split tree), tags (comma-separated; 'bug' reserved),
// payload (the raw request text = the execution briefing), source
// (internal/tasksource's vocabulary — who enqueued it; the column DEFAULT is
// empty, and the schema comment's old `llm|human|auto` list is retired with
// the last value it named), claimed_by/claimed_at (atomic pickup CAS trail). claim lease fields and verify/reopened states are P2.5 and
// deliberately absent. model already existed (P0).
func tasksSchemaTable() string {
	return `CREATE TABLE IF NOT EXISTS tasks (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id        TEXT NOT NULL UNIQUE,
	summary        TEXT NOT NULL,
	status         TEXT NOT NULL DEFAULT 'in_progress'
	               CHECK (status IN (` + taskSchemaStatusCHECK() + `)),
	session_id     TEXT DEFAULT '',
	` + taskSubmitterColumnName + ` TEXT DEFAULT '',
	model          TEXT DEFAULT '',
	parent_task_id TEXT DEFAULT '',
	` + taskParentIDsColumnName + ` TEXT DEFAULT '',
	tags           TEXT DEFAULT '',
	payload        TEXT DEFAULT '',
	source         TEXT DEFAULT '',
	claimed_by     TEXT DEFAULT '',
	claimed_at     REAL,
	priority       INTEGER,
	verify_evidence TEXT DEFAULT '',
	deliverable    TEXT DEFAULT '',
	run_method     TEXT DEFAULT '',
	run_cmd        TEXT DEFAULT '',
	reason         TEXT DEFAULT '',
	created_at     REAL NOT NULL,
	updated_at     REAL NOT NULL,
	ended_at       REAL,
	` + taskTasklistIssuedAtColumn + ` REAL,
	` + taskLastToolcallAtColumn + `   REAL,
	` + tasksInProgressAtColumnName + ` REAL,
	` + tasksProcPidColumnName + ` INTEGER,
	` + tasksProcStartColumnName + ` INTEGER,
	` + tasksProcCmdColumnName + ` TEXT DEFAULT '',
	` + tasksLockPhaseColumnName + ` TEXT DEFAULT ''
)`
}

const (
	tasksSchemaIndexStatus = "CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)"
	tasksSchemaIndexTaskID = "CREATE INDEX IF NOT EXISTS idx_tasks_task_id ON tasks(task_id)"
)

// tasksSchemaEventsTable is the append-only task_events audit table
// (bug056: writeback can no longer overwrite the immutable summary — the
// result digest / run stats / transition trail move HERE, a separate
// operator-visible carrier). Every status transition AND every harness
// writeback-and-result appends one row: the full state-change history is
// preserved untouched, and show renders the latest result. No FTS5
// companion (Epic C: audit data is not search-target content — indexing it
// would churn on every event and bloat the search corpus). The row's
// content is what the old writeback pushed into summary (run stats / result
// digest / failure note), so nothing the summary carried is lost — it just
// lives in the right place now.
func tasksSchemaEventsTable() string {
	return `CREATE TABLE IF NOT EXISTS task_events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL,
	event      TEXT NOT NULL,            -- status transition target (claimed/in_progress/completed/failed/verified/…)
	detail     TEXT DEFAULT '',          -- the result digest / run stats / failure note (old writeback summary body)
	created_at REAL NOT NULL
)`
}

const tasksSchemaIndexEventTask = "CREATE INDEX IF NOT EXISTS idx_task_events_task_id ON task_events(task_id, id)"

// taskEventWrite appends one append-only event row (the single writer
// shared by the writeback path and the transition recorder). Called AFTER
// the status UPDATE commits — an event write failure is logged, never
// allowed to roll back the transition (the audit trail is best-effort, the
// ledger write stands).
func taskEventWrite(ctx context.Context, q QueryExecer, taskID, event, detail string) {
	_, err := q.ExecContext(ctx,
		`INSERT INTO task_events (task_id, event, detail, created_at) VALUES (?, ?, ?, ?)`,
		taskID, event, detail, float64(time.Now().Unix()))
	if err != nil {
		taskNotifyLog("task_events append for %s (%s) failed: %v", taskID, event, err)
	}
}

// taskEventWriteErr is taskEventWrite for a caller that must STATE a failure
// instead of logging it: the reissue's payload-copy trail entry is an AUDIT row
// another reader derives a fact from (tools.TaskBriefingAuthoringAt), so a
// silent loss would leave that reader answering "no copy happened" about a row
// whose payload was copied. The caller renders the error on its own line
// (Rule 15) and continues — the row it just minted is not undone by a lost
// audit entry.
func taskEventWriteErr(ctx context.Context, q QueryExecer, taskID, event, detail string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO task_events (task_id, event, detail, created_at) VALUES (?, ?, ?, ?)`,
		taskID, event, detail, float64(time.Now().Unix()))
	if err != nil {
		return fmt.Errorf("append task_events row for %s (%s): %w", taskID, event, err)
	}
	return nil
}

// taskEventLatestDetail returns the newest event's detail that a `result:` line
// may RENDER (show's digest) — "" when no such event exists yet.
//
// WHY "RENDERABLE" AND NOT SIMPLY "NEWEST" (epic cmd-agent-e2e-seam-stability,
// measured 2026-09-25, twice by hand and once by the E2E):
//
//	`task show` renders the newest event's detail as `result:`. The close-out
//	chain appends MORE events after the run's writeback — align-before-delivery
//	and premerge-refused are two — whose detail is not a run digest at all but
//	the delivery sync's one-line verdict ("branch already contains the
//	integration branch — nothing to merge, no rebuild"). Taking them made the
//	render drop the digest the writeback had just recorded: the report path
//	(task-report: <path>) and Rule 31's ran-line finding vanished from `task
//	show`, so the tests asserting they are RECORDED read the absence as "the
//	ledger never recorded it".
//
// THE DISCRIMINATOR IS THE EVENT NAME, not a marker inside the text: a digest
// is written under the row's own STATUS (taskEventWrite(ctx, db, taskID,
// status, summary) in the transition writer — the event name IS the new status
// value), and no status name is ever used as a delivery step's event name. So
// "the event name is a known status" selects the run's digests, and a purely
// mechanical step (whose detail is verdict text, not a run result) is skipped.
//
// The ORDER BY stays `id DESC`: this is still the NEWEST renderable digest, so
// a row that ran several times shows its most recent run. Nothing is dropped
// from the append-only trail — this is a read-side selector, and every event
// remains queryable in task_events.
func taskEventLatestDetail(ctx context.Context, db *sql.DB, taskID string) string {
	const scanCap = 32
	rows, err := db.QueryContext(ctx,
		`SELECT event, detail FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT ?`, taskID, scanCap)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var event, detail string
		if err := rows.Scan(&event, &detail); err != nil {
			return ""
		}
		if detail == "" || !validTaskStatus(event) {
			continue
		}
		{
			// A STATUS event carries a THREE-ELEMENT TRANSITION RECORD, and the
			// run's digest (when the transition was a writeback) rides its
			// marked trailer line (epic 01a1a899-d05d). `result:` shows the
			// DIGEST and nothing else, so the record is stripped here; an event
			// with no trailer is a plain transition — it recorded who/why/from→to
			// and no run result, so it is NOT a `result:` candidate at all.
			//
			// A row written before the record existed carries the bare digest
			// (or an empty detail, skipped above), and it is returned unchanged:
			// the read side never rewrites history, it renders what is there.
			if d := taskStatusEventStripTrailer(detail); d != detail {
				if d == "" {
					continue
				}
				return d
			}
			if taskStatusEventIsTransitionOnly(detail) {
				// A plain transition (no digest trailer) is a lifecycle record,
				// never a run result — keep scanning for an older status event
				// that DID carry one, so a row's `result:` is not erased by a
				// later bare status move.
				continue
			}
			return detail
		}
	}
	return ""
}

// taskEventLatestTransition returns the newest recorded LIFECYCLE TRANSITION on
// a row — the event name (which IS the new status) and its rendered detail — or
// two empty strings when the row's trail holds no status event.
//
// THE LEGACY RENDER IS THE POINT (epic 01a1a899-d05d). An event whose detail is
// EMPTY predates the three-element record, and this program did not watch it: it
// cannot say who performed the move or on what grounds. The renderer therefore
// says exactly that (taskStatusEventLegacyDetail — `who=(unrecorded, pre-<date>
// format)`), because the alternative is to attribute an unwatched transition to
// the operator, which would be an invented culprit rather than a recorded fact.
//
// The window is the show-side bound, newest first: this is a READ of the trail
// (nothing is dropped, every event stays queryable) and a row that moved several
// times shows its most recent move.
func taskEventLatestTransition(ctx context.Context, db *sql.DB, taskID string) (event, detail string) {
	rows, err := db.QueryContext(ctx,
		`SELECT event, detail FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT ?`,
		taskID, taskShowVerdictRowLimit)
	if err != nil {
		return "", ""
	}
	defer rows.Close()
	for rows.Next() {
		var name, body string
		if err := rows.Scan(&name, &body); err != nil {
			return "", ""
		}
		if !validTaskStatus(name) {
			continue
		}
		// An EMPTY detail is the pre-record shape: nothing about who or why was
		// written down, and the renderer says exactly that.
		if body == "" {
			return name, taskStatusEventLegacyDetail(name)
		}
		// A detail that is not a RECORD — a pre-format run digest, or the sync
		// lane's own move sentence — recorded SOMETHING, so it is carried in the
		// why slot while the who slot states that no identity was recorded. The
		// two legacy shapes stay distinct on purpose: claiming pre-<date> for a
		// sync move written today would mis-date the gap.
		if !taskStatusEventIsTransitionDetail(body) {
			if len(body) > taskTransitionDetailRenderLimit {
				body = body[:taskTransitionDetailRenderLimit] + taskListCloseoutEllipsis
			}
			return name, taskStatusEventUnclassifiedDetail(name, body)
		}
		body = taskStatusEventRecordBody(body)
		if len(body) > taskTransitionDetailRenderLimit {
			body = body[:taskTransitionDetailRenderLimit] + taskListCloseoutEllipsis
		}
		return name, body
	}
	return "", ""
}

// taskEventLatestDeliveryOwed returns the newest recorded DELIVERY DEBT on a
// row (the event name and its detail), or two empty strings when the row owes
// nothing it recorded.
//
// WHY A THIRD SELECTOR RATHER THAN A verdictTrailEventNames ENTRY: the verdict
// reader keeps the NEWEST judgment and nothing older, by design — and on this
// ledger the delivery record is routinely the OLDER one. Measured 2026-11-01:
// 98 of 135 `delivery-proof-owed` records are followed by a later verdict
// (`reclaim-unreachable` on every one of them), because the reclaim arm stamps
// its permanent-skip marker days after the refusal. Rendering the delivery
// debt through the verdict line would therefore have shown 37 of 135 records
// and re-labelled the other 63 already-rendered rows. A separate axis makes
// the fact UNSHADOWABLE: no later judgment can take it off the row.
//
// The window is newest-first and bounded (taskShowDeliveryOwedRowLimit). The
// bound is measured, not guessed: across the 135 live occurrences the number
// of newer events on the same row has median 3, p90 5 and MAX 8 — so the
// window holds every record on this ledger with room to spare, and a
// pathological trail still costs one bounded scan rather than a full walk.
// The same discipline as taskShowVerdictRowLimit, spelled as its own constant
// because the two windows bound different populations.
func taskEventLatestDeliveryOwed(ctx context.Context, db *sql.DB, taskID string) (event, detail string) {
	rows, err := db.QueryContext(ctx,
		`SELECT event, detail FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT ?`,
		taskID, taskShowDeliveryOwedRowLimit)
	if err != nil {
		return "", ""
	}
	defer rows.Close()
	for rows.Next() {
		var name, body string
		if err := rows.Scan(&name, &body); err != nil {
			return "", ""
		}
		if strings.TrimSpace(body) == "" || !taskShowDeliveryOwedEventName(name) {
			continue
		}
		return name, body
	}
	return "", ""
}

// taskEventLatestVerdict returns the newest JUDGED VERDICT recorded on a row:
// the event name (the durable, grep-able marker in task_events) and its detail,
// or two empty strings when the row carries no verdict.
//
// WHY A SECOND SELECTOR (epic verify-gate regression, measured 2026-09-25):
// taskEventLatestDetail above answers "what did the RUN record?" — it takes the
// newest event named after a status. A verdict is written under its OWN
// semantic event name (MergeVerifyEvent = "merge-verify"), so it is not a
// digest and that selector skips it BY DESIGN — which cost the judged row its
// verdict: after the merge was reverted and rework filed, `task show` on the
// row the verdict ruled on rendered no trace of it, and the operator's audit
// path (the test TestVerifyGateRecordsVerdictEvent) read the absence as "the
// verdict was never recorded". Both faces are real facts about the row and
// neither may hide the other: the digest keeps `result:`, the verdict gets its
// own line.
//
// The window is newest-first and bounded (taskShowVerdictRowLimit): a verdict
// is the newest ruling by construction, and the transition writer prunes
// nothing, so a row that ran fifty times must not turn `task show` into a full
// trail scan. The scan STOPS at the first verdict — the newest one is the
// operator's answer, and every older one stays queryable in task_events.
func taskEventLatestVerdict(ctx context.Context, db *sql.DB, taskID string) (event, detail string) {
	rows, err := db.QueryContext(ctx,
		`SELECT event, detail FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT ?`,
		taskID, taskShowVerdictRowLimit)
	if err != nil {
		return "", ""
	}
	defer rows.Close()
	for rows.Next() {
		var name, body string
		if err := rows.Scan(&name, &body); err != nil {
			return "", ""
		}
		if strings.TrimSpace(body) == "" || !taskShowVerdictEventName(name) {
			continue
		}
		return name, body
	}
	return "", ""
}

// verdictTrailEventNames is the NAMED list of event names that are verdicts
// about the row but do NOT share the merge- lane prefix — each is a lane's own
// ruling on a judged row, and `task show` must render it or the audit path
// loses it exactly the way it lost the merge-verify verdict.
//
// Membership test: the event records a JUDGMENT ON the row (a verdict, a
// refusal), never a run digest and never a status transition. A run digest is
// written under the row's status and belongs on `result:`; a transition is not
// a verdict at all. Anything that fails both tests stays out of the list.
var verdictTrailEventNames = []string{
	finishCleanupRemovalRefusedEvent, // "finish-cleanup-removal-refused" — the delivery step that stranded this row
	ReframeEventName,                 // "reframe" — the epic verdict + its roadmap cardinality
	MergeAnchorGateEventName,         // "merge-anchor-gate" — the merge-time published-anchor lane's per-merge ruling (WHICH arm let a branch through, and why)
	// THE CLOSE-OUT GATES THAT REFUSED THE WRITEBACK ITSELF (measured
	// 2026-10-17, task 01a1492f). Both are written by the Rule 30 gate in
	// task_reclaim.go and both were OUTSIDE this list, so `task show` did not
	// render them: the row kept `status: in_progress` with NO reason at all,
	// and the only surviving account of why was the wrapper's stderr line
	// "writeback failed (continuing): …" — a channel that belongs to a process
	// which has already exited. The operator's question ("why is this row still
	// open?") was answerable only by querying task_events by hand.
	//
	// Membership is the list's own test — a JUDGMENT ON the row, a refusal —
	// and these are the most literal case of it in the ledger: the gate
	// declined to close the row.
	reclaimRunTrackedDirtRefusalEvent, // "completed-unclean-tracked-work-refused" — uncommitted trackED work in the run's own ground
	reclaimRunResidueRefusedEvent,     // "completed-unclean-refused" — the run's own untracked residue (GOPI_TASK_RUN_RESIDUE_ENFORCE)
	// The reclaim arm's PERMANENT-SKIP marker (task_reclaim.go): the row's
	// ground cannot be reconciled from here, so the arm stopped reporting it
	// after the first pass. It is a judgment ON the row — the arm declined to
	// free the ground and named why — and rendering it is what makes an
	// operator's "why is this branch still blocked?" answerable from the row
	// itself instead of from a log line that was printed once, days ago.
	reclaimUnreachableEventName, // "reclaim-unreachable"
	// THE REMOVAL'S OWN OUTCOME (task_reclaim.go, epic 01a17e1e). A
	// dead-holder reclaim closes the row FIRST and removes the ground AFTER, so
	// the row's reason column cannot carry the removal's fate — it is written
	// before one is known. The outcome therefore gets its own trail event, and
	// it must be RENDERED or the only surviving account of a failed removal is
	// a taskNotifyLog line belonging to a process that has already exited: the
	// operator asking "why is this branch still withheld from merging?" would
	// have no answer on the row itself. Membership is the list's own test — the
	// arm measured what the removal did and is telling the row.
	reclaimGroundOutcomeEvent, // "reclaim-ground-outcome"
	// THE OPERATOR'S OWN WAIVER, and the reason it must be VISIBLE here (epic
	// 01a17dcd): `gopi task update --force` (or GOPI_TASK_DELIVERY_PROOF_ALLOW)
	// lets a provably-undelivered close through and records
	// `delivery-proof-unverified` on the row. That record is a JUDGMENT ON the
	// row — the arm measured the delivery claim and let it pass unproven — and
	// a row that closed unproven while `task show` renders nothing about it is
	// exactly the confusion the epic exists to prevent: the operator's waiver
	// would be indistinguishable from a proven close on the one surface a
	// reviewer reads. The PROVEN close writes no such record, so its absence is
	// the positive signal.
	DeliveryProofUnverifiedEvent, // "delivery-proof-unverified"
	// THE RESUME'S DOWNGRADES (epic 01a17df9-99c4): a resume that could not
	// continue the session behind its target. The first is the operator's
	// explicit opt-in (a NEW session was started), the second is the in-place
	// recovery (the record was rebuilt from the checkpoint). Both are judgments
	// ON the row written by a run that is already gone by the time anyone reads
	// them, and both change what `resumed:<session>` MEANS on the row — so a
	// reviewer who reads only `task show` must see them or the lineage claim
	// (`this row's session continued`) is unverifiable from the row itself.
	ResumeNewSessionEvent,    // "resume-new-session"
	ResumeRecordRebuiltEvent, // "resume-record-rebuilt"
	// THE TERMINAL WRITEBACK THAT NEVER LANDED (task_writeback_lost.go,
	// measured 2026-10-27). A run whose `failed`/`completed` writeback was
	// REFUSED leaves its row open BY DESIGN — the state is the ledger's, not
	// the dying process's — so the row's own render is the only place the
	// operator can read why it is still `in_progress` after its holder died.
	// Membership is this list's test: the entry is a JUDGMENT ON the row (the
	// ledger declined the write the run asked for), not a run digest.
	WritebackLostEvent, // "writeback-lost"
	// WHAT A PERMANENT REFUSAL LEAVES BEHIND (task_reclaim_stranded_commits.go,
	// measured 2026-11-01 on row 01a16043-2b43). The `reclaim-unreachable`
	// marker above records the REFUSAL and then promises the arm will never
	// report the row again — so for a CLONE ground, whose directory is the only
	// repository its commits exist in, that marker was the last thing the arm
	// would ever say about work that has no destination.
	//
	// CORRECTION (the first version of this comment overstated it): the ledger
	// did already record the tip on 65 of the 82 undelivered grounds, through
	// `delivery-proof-owed`. What was missing was the RENDER — that event is not
	// in this list, so `task show` printed nothing about it — plus the 8 rows
	// with no delivery record at all. This member closes the half THIS arm can
	// close: it MEASURES what the refusal strands and tells the row, and it is
	// rendered because it is in this list. Membership is this list's own test.
	reclaimStrandedCommitsEvent, // "reclaim-stranded-commits"
	// THE PRODUCTS THAT LANDED OUTSIDE THE RUN'S GROUND (task_row_repo_products.go,
	// measured 2026-11-22 on row 01a20059-d6c4). The arm measures the repository
	// the ROW DECLARES — the one directory no close-out arm had ever read — and
	// files what it can attribute to this run. It is a JUDGMENT ON the row (the
	// close-out measured where the work actually went and is telling the row),
	// and it must be RENDERED: the row it was written for closed `completed`,
	// which is the one status no operator re-reads. Membership is this list's
	// own test.
	TaskRowRepoProductsEvent, // "products-in-row-repo"
}

// taskShowVerdictEventName is the exact membership test the verdict reader
// applies: the merge-verify verdict plus every name lane owners registered in
// verdictTrailEventNames. Both sides reference the exported/unexported
// constants, never a re-typed literal (Rule 1b).
func taskShowVerdictEventName(event string) bool {
	if event == MergeVerifyEvent {
		return true
	}
	for _, name := range verdictTrailEventNames {
		if event == name {
			return true
		}
	}
	return false
}

// contentRevisionEventNames is the NAMED list of trail events that record an
// EDIT of the row's own content rather than a run's result or a judgment on
// the row. Membership test, the complement of verdictTrailEventNames': the
// event's detail is the row's COMPARATIVE HISTORY (old → new), it is written
// by an operator-facing edit door, and it is large by construction. Anything
// that fails that test stays out of the list.
//
// Why the list exists at all: `task show` renders the newest event's detail on
// `result:` (taskEventLatestDetail skips only events whose name is a STATUS
// value). A revision whose name is not a status therefore lands there by
// default — and `result:` is the line the close-out chain, the report seam and
// Rule 31's ran-line check read. Rendering a briefing diff in that slot both
// buries the run digest and is unreadable. The revision stays fully in the
// trail (`task_events`, queryable) and its CURRENT value is the payload column
// itself, so suppressing the render loses no fact (Rule 15: this is a read-side
// selection, never a deletion).
//
// TaskReparent's "reparented" entry is the second member for exactly that
// reason: it carries the old edge, the new edge and the reason — three values
// on a line whose reader is looking for a run's verdict.
var contentRevisionEventNames = []string{
	PayloadRevisionEventName, // "payload_revised" — TaskSetPayload
	ReparentEventName,        // "reparented" — TaskReparent
}

// taskContentRevisionEventName is the exact membership test the result-line
// reader applies, referencing the constant (never a re-typed literal).
func taskContentRevisionEventName(event string) bool {
	for _, name := range contentRevisionEventNames {
		if event == name {
			return true
		}
	}
	return false
}

// taskEventLatest returns the most recent event's detail for a task
// (show's `result:` line) — "" when no events exist yet.
func taskEventLatest(ctx context.Context, db *sql.DB, taskID string) (event, detail string) {
	row := db.QueryRowContext(ctx,
		`SELECT event, detail FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT 1`, taskID)
	_ = row.Scan(&event, &detail) //stylegate:ignore existence probe; sql.ErrNoRows yields empty strings by design
	return event, detail
}

// taskEventHasDetail reports whether the task already has an event whose
// detail contains needle — the redelivery dedup guard (the old summary
// append used strings.Contains on the summary; the events trail keeps the
// same idempotency against the audit rows).
func taskEventHasDetail(taskID, needle string) bool {
	ctx := context.Background()
	// READ-ONLY OPENER — this face is a PURE READ: its SELECTs are the whole of
	// what it does, and it calls no writer (openTasksDBReadOnly skips the tasks DDL
	// batch and takes no BEGIN IMMEDIATE, so a reader never queues on the writer
	// that holds the ledger).
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return false // best-effort: on DB failure treat as not-yet-recorded
	}
	defer db.Close()
	var one int
	err = db.QueryRowContext(ctx,
		`SELECT 1 FROM task_events WHERE task_id = ? AND detail LIKE ? ESCAPE '\' LIMIT 1`,
		taskID, "%"+likeEscape(needle)+"%").Scan(&one)
	return err == nil
}

// FTS5 companions for the tasks ledger, mirroring the distilled_qa_fts /
// distilled_qa_tri pattern in memory.go: an external-content unicode61 index
// over the searchable content columns for word/ASCII queries, plus a trigram
// index for CJK substring recall (unicode61 treats a contiguous CJK run as
// one token; trigram indexes every 3-char substring so any ≥3-char CJK query
// matches inside longer runs). Both are kept in sync by ai/au/ad triggers —
// the ledger is append-only, so the DELETE arms never fire in practice, but
// they are declared anyway (an external-content index silently corrupts when
// a row ever does disappear — defense in depth, zero cost).
//
// P2 dispatcher indexing scope: the CONTENT columns are indexed (task_id,
// session_id, summary + the new parent_task_id/tags/payload/source — a
// task_submit's briefing text, tags and split-tree linkage are all
// searchable, per the epic's FTS requirement). The CAS operational columns
// (claimed_by/claimed_at) are deliberately NOT indexed: they churn on every
// pickup, which would rewrite the whole row's FTS entry on each claim.
var tasksFTSColumns = []string{
	"task_id", "session_id", "summary", "parent_task_id", "tags", "payload", "source",
}

func tasksFTSDefs() (fts, tri, ai, au, ad string) {
	cols := strings.Join(tasksFTSColumns, ", ")
	newCols := make([]string, len(tasksFTSColumns))
	oldCols := make([]string, len(tasksFTSColumns))
	for i, c := range tasksFTSColumns {
		newCols[i] = "new." + c
		oldCols[i] = "old." + c
	}
	newVals, oldVals := strings.Join(newCols, ", "), strings.Join(oldCols, ", ")

	fts = `CREATE VIRTUAL TABLE IF NOT EXISTS tasks_fts USING fts5(
	` + cols + `, content='tasks', content_rowid='id'
)`

	tri = `CREATE VIRTUAL TABLE IF NOT EXISTS tasks_tri USING fts5(
	` + cols + `, content='tasks', content_rowid='id',
	tokenize='trigram'
)`

	ai = `CREATE TRIGGER IF NOT EXISTS tasks_ai AFTER INSERT ON tasks BEGIN
	INSERT INTO tasks_fts(rowid, ` + cols + `)
	VALUES (new.id, ` + newVals + `);
	INSERT INTO tasks_tri(rowid, ` + cols + `)
	VALUES (new.id, ` + newVals + `);
END`

	au = `CREATE TRIGGER IF NOT EXISTS tasks_au AFTER UPDATE ON tasks BEGIN
	INSERT INTO tasks_fts(tasks_fts, rowid, ` + cols + `)
	VALUES ('delete', old.id, ` + oldVals + `);
	INSERT INTO tasks_fts(rowid, ` + cols + `)
	VALUES (new.id, ` + newVals + `);
	INSERT INTO tasks_tri(tasks_tri, rowid, ` + cols + `)
	VALUES ('delete', old.id, ` + oldVals + `);
	INSERT INTO tasks_tri(rowid, ` + cols + `)
	VALUES (new.id, ` + newVals + `);
END`

	ad = `CREATE TRIGGER IF NOT EXISTS tasks_ad AFTER DELETE ON tasks BEGIN
	INSERT INTO tasks_fts(tasks_fts, rowid, ` + cols + `)
	VALUES ('delete', old.id, ` + oldVals + `);
	INSERT INTO tasks_tri(tasks_tri, rowid, ` + cols + `)
	VALUES ('delete', old.id, ` + oldVals + `);
END`
	return fts, tri, ai, au, ad
}

// Task mirrors one tasks row. EndedAt is 0 with HasEnded false when the task
// is still in progress (the column is NULL). The M4.5 liveness columns
// (tasklist_issued_at / last_toolcall_at) are raw facts: when the harness
// received a [TASK_LIST] block, and when the last tool batch started — the
// stale detector compares the toolcall heartbeat against a threshold.
// taskParentIDsColumnName is the schema name of the SECOND-AND-LATER parent
// column (multi-parent dependency, EPIC multi-parent). `parent_task_id` — the
// single-value column — is NOT changed by that epic: its type, its meaning, its
// writer and its refusal wording stay byte-for-byte what they were, so a row
// written by an older binary reads back exactly as it always did.
//
// THE WIRE SHAPE: a COMMA-SEPARATED list of task ids, holding parents 2..N —
// never parent 1. Parent 1 lives in `parent_task_id`, and that split is the
// whole compatibility argument: an OLD gopi (another machine, an older sync
// leg) that knows nothing about this column still reads parent 1 out of the
// column it has always read, so a multi-parent row never arrives looking like
// a row with NO parent (which would be claimable at once and the dependency
// would vanish silently). A comma rather than JSON because the column is
// consumed by the same gate that parses `tags`, and an id is a uuid: it cannot
// contain a comma, so the encoding is reversible without an escape rule. See
// taskParentIDsOf for the ONE reader, and taskParentsForWrite for the ONE
// writer.
const taskParentIDsColumnName = "parent_task_ids"

// taskParentIDsSeparator splits taskParentIDsColumnName's value.
const taskParentIDsSeparator = ","

// Task.ParentTaskID notes: the row's FIRST parent (the only parent a pre-epic
// row has, and the only one an older binary can see). Read every parent
// through taskParentIDsOf — never by reading these two fields directly — so
// the "old field first, then the new list" order exists in exactly one place.
type Task struct {
	ID        int64
	TaskID    string
	Summary   string
	Status    string
	SessionID string
	// SubmitterSession is the session that FILED the row — the
	// anti-self-reference key (claim eligibility refuses a row filed by the
	// claiming session). It lives in its own column since the split out of
	// SessionID (taskSubmitterColumnName's doc carries the argument). EMPTY on
	// legacy rows, which still carry the submitter in SessionID — read it through
	// taskSubmitterSessionOf, which answers for both shapes.
	SubmitterSession string
	Model            string
	ParentTaskID     string
	// ParentTaskIDs is the second-and-later parent list, verbatim from
	// parent_task_ids (EPIC multi-parent). Read every parent through
	// taskParentIDsOf — that function is the ONE place the "old column first,
	// then this list" order is applied; a caller reading the two fields itself
	// would re-derive a rule the gate then disagrees with (Rule 14).
	ParentTaskIDs  string
	Tags           string
	Payload        string
	Source         string
	ClaimedBy      string
	ClaimedAt      float64
	HasClaim       bool
	VerifyEvidence string
	Deliverable    string
	RunMethod      string
	RunCmd         string
	Reason         string
	CreatedAt      float64
	UpdatedAt      float64
	EndedAt        float64
	HasEnded       bool

	TasklistIssuedAt float64
	HasTasklistIssue bool
	LastToolcallAt   float64
	HasToolcall      bool

	// InProgressAt is the execution-START stamp of the row's CURRENT
	// in_progress stretch (NULL = the row never entered in_progress on a
	// build that carried the column — the legacy shape). It exists so the
	// claim stamp has a referent: "who holds this row" is only answerable
	// against "since when is the row being executed", and a claim stamp
	// that PREDATES this stamp cannot be the executing process's (see
	// claimStampFresh — the zombie chain's staleness axis).
	InProgressAt    float64
	HasInProgressAt bool

	// Process IDENTITY of the row's executing process — the "big three"
	// (pid, start ticks, command line) captured atomically with the
	// in_progress transition (epic task-process-identity, 2026-09-13). See
	// taskproc.go for why three factors and how they are verified. All zero
	// on a row that never carried an identity (legacy / not in_progress) —
	// HasProcIdentity is the single gate every reader must check, never
	// ProcPid alone (pid 0 is not a process).
	ProcPid         int
	ProcStartTicks  int64
	ProcCmd         string
	HasProcIdentity bool

	// LockPhase is the CLAIM-LOCK PROTOCOL's progress, never the row's status
	// (see tasksLockPhaseColumnName): "" = no protocol ran (single-node /
	// sync off / skipped broadcast — rendered as nothing), "local" = Phase-1
	// only, "both" = 双锁齐. It exists so a `claimed` row can say WHY it is
	// still claimed without borrowing `in_progress`, which now means exactly
	// "a child process was exec'd and is being waited on".
	LockPhase string

	// Priority is the M4.11 queue weight (lower runs first). 0 on legacy
	// rows missing the column (NULL) — display falls back to the default.
	Priority int
}

// taskSelectColumns is the SELECT projection shared by every task row reader
// (taskByID, TaskList, the claim path) — one column list, one scan order.
var taskSelectColumns = "id, task_id, summary, status, session_id, " + taskSubmitterColumnName + ", model, " +
	"parent_task_id, " + taskParentIDsColumnName + ", tags, payload, source, claimed_by, claimed_at, " +
	"verify_evidence, deliverable, run_method, run_cmd, reason, priority, created_at, updated_at, ended_at, " +
	taskTasklistIssuedAtColumn + ", " + taskLastToolcallAtColumn + ", " + tasksInProgressAtColumnName + ", " +
	tasksProcIdentityColumns + ", " + tasksLockPhaseColumnName

// tasksProcIdentityColumns is the process-identity projection suffix (epic
// task-process-identity): the three columns every row reader scans, in ONE
// place so the four read sites (scanTask, scanTaskRow, the rebuild copy and
// the sync patch) cannot drift out of order (Rule 14).
const tasksProcIdentityColumns = tasksProcPidColumnName + ", " +
	tasksProcStartColumnName + ", " + tasksProcCmdColumnName

// scanTask scans one row (in taskSelectColumns order) into a Task.
func scanTask(rows *sql.Rows) (*Task, error) {
	return scanTaskInto(rows)
}

// taskRowScanner is the interface both scan sites share (*sql.Rows and
// *sql.Row) — one scan implementation for the ONE projection, so the two
// readers can never drift out of column order.
type taskRowScanner interface {
	Scan(dest ...any) error
}

// scanTaskInto scans one row (in taskSelectColumns order) into a Task. Every
// nullable column/value is decoded through its sql.Null* carrier so a legacy
// NULL reads as "absent", never as a zero that a consumer would take for a
// fact (Rule 15).
func scanTaskInto(row taskRowScanner) (*Task, error) {
	t := &Task{}
	var endedAt, claimedAt, issuedAt, toolcallAt, inProgressAt sql.NullFloat64
	var priority, procPid, procStart sql.NullInt64
	var procCmd, submitter, lockPhase sql.NullString
	if err := row.Scan(&t.ID, &t.TaskID, &t.Summary, &t.Status, &t.SessionID,
		&submitter,
		&t.Model, &t.ParentTaskID, &t.ParentTaskIDs, &t.Tags, &t.Payload, &t.Source,
		&t.ClaimedBy, &claimedAt, &t.VerifyEvidence, &t.Deliverable, &t.RunMethod, &t.RunCmd, &t.Reason, &priority, &t.CreatedAt, &t.UpdatedAt,
		&endedAt, &issuedAt, &toolcallAt, &inProgressAt,
		&procPid, &procStart, &procCmd, &lockPhase); err != nil {
		return nil, err
	}
	if priority.Valid {
		t.Priority = int(priority.Int64)
	}
	if endedAt.Valid {
		t.EndedAt = endedAt.Float64
		t.HasEnded = true
	}
	if claimedAt.Valid {
		t.ClaimedAt = claimedAt.Float64
		t.HasClaim = true
	}
	if issuedAt.Valid {
		t.TasklistIssuedAt = issuedAt.Float64
		t.HasTasklistIssue = true
	}
	if toolcallAt.Valid {
		t.LastToolcallAt = toolcallAt.Float64
		t.HasToolcall = true
	}
	if inProgressAt.Valid {
		t.InProgressAt = inProgressAt.Float64
		t.HasInProgressAt = true
	}
	if procPid.Valid && procPid.Int64 > 0 {
		t.ProcPid = int(procPid.Int64)
		t.HasProcIdentity = true
	}
	if procStart.Valid {
		t.ProcStartTicks = procStart.Int64
	}
	if procCmd.Valid {
		t.ProcCmd = procCmd.String
	}
	if lockPhase.Valid {
		t.LockPhase = strings.TrimSpace(lockPhase.String)
	}
	if submitter.Valid {
		t.SubmitterSession = submitter.String
	}
	return t, nil
}

// validTaskStatus reports whether s is one of the six CHECK-constrained
// status values (single source of truth for Go-side validation; the SQL
// CHECK is the engine-level backstop).
func validTaskStatus(s string) bool {
	for _, v := range taskStatusSet {
		if s == v {
			return true
		}
	}
	return false
}

// validTaskSource reports whether s is an accepted ledger provenance value
// (source column). The vocabulary and its retired aliases live in ONE place —
// internal/tasksource (Rule 14) — so a new value is added there and every
// reader follows. The literal set used to be spelled here, and drifted from
// what the writers actually stamped.
func validTaskSource(s string) bool {
	return tasksource.Known(s)
}

// terminalTaskStatus reports whether s ends the task's life (stamps ended_at).
// P2.5: verified joins the terminal set — the verification verdict is final
// unless reopened; reopened is transitional (a re-enqueue follows), so it
// does NOT stamp ended_at (the replacement pending row carries the timeline).
func terminalTaskStatus(s string) bool {
	return s == taskStatusCompleted || s == taskStatusFailed ||
		s == taskStatusCancelled || s == taskStatusVerified ||
		s == taskStatusSuperseded
}

// tasksP2Columns are the dispatcher columns added after the four-status
// ledger's first release (P2, devdoc/task-dispatcher-p2.md). Like model
// before them, they migrate pre-column ledgers in place via idempotent
// ALTER TABLE (the ensureSchema pattern — no migration scripts). Rows from
// the old schema keep the ” / NULL defaults.
var tasksP2Columns = []string{
	"ALTER TABLE tasks ADD COLUMN parent_task_id TEXT DEFAULT ''",
	// Multi-parent (EPIC multi-parent): parents 2..N ride here, parent 1 stays
	// in parent_task_id above. Idempotent ALTER like every other P2 column —
	// pre-column ledgers gain an empty cell, and an empty cell MEANS "this row
	// has exactly the parents parent_task_id names", so no row is rewritten.
	"ALTER TABLE tasks ADD COLUMN " + taskParentIDsColumnName + " TEXT DEFAULT ''",
	"ALTER TABLE tasks ADD COLUMN tags           TEXT DEFAULT ''",
	"ALTER TABLE tasks ADD COLUMN payload        TEXT DEFAULT ''",
	"ALTER TABLE tasks ADD COLUMN source         TEXT DEFAULT ''",
	"ALTER TABLE tasks ADD COLUMN claimed_by     TEXT DEFAULT ''",
	"ALTER TABLE tasks ADD COLUMN claimed_at     REAL",
	// M4.5 liveness columns (task-heartbeat): raw facts, no daemon — when
	// the harness last received a [TASK_LIST] block, and when the last tool
	// batch started (the heartbeat the stale detector reads).
	"ALTER TABLE tasks ADD COLUMN " + taskTasklistIssuedAtColumn + " REAL",
	"ALTER TABLE tasks ADD COLUMN " + taskLastToolcallAtColumn + "   REAL",
	// P2.5 verification closure (devdoc/task-verify-loop.md): the black-box
	// verification verdict — exit code, test result line, elapsed, verifier
	// session — stamped by `gopi task verify`. '' = never verified.
	"ALTER TABLE tasks ADD COLUMN verify_evidence TEXT DEFAULT ''",
	// M4.8 deliverable closure (t-1788089584): the structured deliverable
	// JSON blob — submit stamps deliverable={"repo":...} (the git repo the
	// work happens in), the executor backfills branch/worktree on finish.
	"ALTER TABLE tasks ADD COLUMN deliverable TEXT DEFAULT ''",
	// M4.11 priority (t-1788087991): queue position weight — lower runs
	// first (TaskNextFiltered ORDER BY priority ASC, created_at ASC).
	// NULL = legacy row (predates the column); every submit path stamps an
	// explicit value (derived from tags or the --priority override). The
	// legacy backfill (below) re-tiers NULL pending bug rows to 1.
	"ALTER TABLE tasks ADD COLUMN priority INTEGER",
	// Run provenance (internal/runcmd, devdoc/run-method-provenance.md): how
	// THIS task's execution was launched and the exact re-runnable command
	// line. run_method ∈ runcmd.Method* ('' = never run / legacy row);
	// child = the AUTO mirror row of a `task run` child (historical —
	// task-single-row-exec removed mirror minting; legacy rows keep it).
	// run_cmd is the verbatim launcher argv (runcmd.Command) — the command an
	// operator can paste to reproduce the run; on a crashed row it IS the
	// resume recipe. Written at launch (or at create, when the creating
	// process is the run itself); never cleared by writeback.
	"ALTER TABLE tasks ADD COLUMN run_method TEXT DEFAULT ''",
	"ALTER TABLE tasks ADD COLUMN run_cmd    TEXT DEFAULT ''",
	// task-update-reason epic: the operator-visible WHY of the row's current
	// status — `task update --reason "..."` stamps it (a lazy caller gets a
	// status-specific default). Rides the sync wires so a peer's show/list
	// answers "why is this task in this state" like the origin node. Same-
	// status no-op updates never touch it (a custom reason survives).
	"ALTER TABLE tasks ADD COLUMN reason TEXT DEFAULT ''",
	// Claim-holder freshness (2026-09-12 epic): WHEN the row entered its
	// CURRENT in_progress stretch. claimed_by/claimed_at alone cannot
	// answer "is the stamp the process actually executing?" — the claim
	// ladder stamps at different moments (pending→claimed CAS, crash-
	// recovery re-stamp) and an in_progress row can carry a stamp written
	// BEFORE the run it is credited with. One column, written by exactly
	// ONE writer (taskUpdateStamped) in the same transaction as the
	// transition it belongs to, gives the stamp a referent (Rule 2/14).
	// NULL = a row that went in_progress on a build without the column:
	// the freshness axis then reports "unknown" and the pid layer keeps its
	// pre-existing vote (see claimStampFresh).
	"ALTER TABLE tasks ADD COLUMN " + tasksInProgressAtColumnName + " REAL",
}

// tasksNeedsRebuild probes the live CHECK constraint on tasks.status: a
// legacy DB is one whose constraint does not admit EVERY value the Go
// vocabulary can write. The required set is DERIVED from taskStatusSet — the
// SAME slice taskSchemaStatusCHECK renders the CREATE literal from — never a
// hand-picked probe list.
//
// WHY DERIVED (measured 2026-10-12, task 01a12f76, against this host's live
// ledger: a memory.db whose CHECK enumerates EIGHT statuses while the
// worktree's taskStatusSet holds nine). `superseded` (epic
// parent-release-statuses, commit 7c32e88f4, 2026-10-07) was added to
// taskStatusSet, taskAllowedTransitions, taskStatusRank and the parent-release
// set — but this probe was a hand-picked pair ({pending, verified}, plus the
// legacy four), so it never learned the new member. Every ledger created
// between P2.5 and that epic therefore kept an eight-value CHECK while the
// transition table advertised `pending → superseded`: the write landed on
// `CHECK constraint failed: status IN (… 'verified','reopened') (275)`,
// meaning the illegal-transition error's OWN prescribed remedy walked the
// caller into a second, contradictory refusal. A probe list drifts from the
// set by exactly the status somebody forgot (Rule 15 — a silent divergence); a
// walk over the set cannot.
//
// The walk is EXHAUSTIVE and PER-MEMBER: any single missing member arms the
// rebuild, so the next status added to taskStatusSet is covered by the
// declaration alone. SQLite cannot ALTER a CHECK, so arming the rebuild IS the
// remedy (tasksRebuildCheckConstraint re-renders the CREATE literal from the
// same slice). The former tasksStatusesBeforeP2 list is deleted, not kept: all
// four of its members are members of taskStatusSet, so the derived walk
// subsumes its "anomalous legacy value missing" arm — keeping a second,
// now-unreferenced list is the very drift hazard this fix removes.
func tasksNeedsRebuild(ctx context.Context, db *sql.DB) (bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='tasks'`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var ddl string
	if rows.Next() {
		if err := rows.Scan(&ddl); err != nil {
			return false, err
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if ddl == "" {
		return false, nil // no tasks table yet — nothing to rebuild
	}
	if !strings.Contains(ddl, "CHECK") {
		return false, nil // constraint-free legacy table: ALTER widens it fine
	}
	for _, s := range taskStatusSet {
		if !strings.Contains(ddl, "'"+s+"'") {
			return true, nil // constraint omits a writable status → rebuild from taskStatusSet
		}
	}
	return false, nil
}

// taskFTSColExpr renders the external-content FTS row expression for the
// given prefix ("old." / "new." / "") — the trigger bodies and the rebuild's
// repopulation all derive from tasksFTSColumns so the indexed column set has
// ONE source of truth.
func taskFTSColExpr(prefix string) string {
	cols := make([]string, len(tasksFTSColumns))
	for i, c := range tasksFTSColumns {
		cols[i] = prefix + c
	}
	return strings.Join(cols, ", ")
}

// tasksRebuildCheckConstraint migrates a legacy four-status tasks table to
// the P2 status set. SQLite cannot ALTER a CHECK constraint, so the migration
// is the classic rename-recreate-copy-drop, ONE transaction: the FTS/trigram
// external-content indexes keep their definitions (they reference 'tasks' by
// name), the ai trigger repopulates them as the copy INSERTs stream in, and
// the ad-triggered DELETE arms never fire (rows are only inserted). The
// copy SELECT carries every legacy column positionally — legacy rows keep
// id, timestamps and data verbatim (append-only: nothing is rewritten,
// nothing deleted).
func tasksRebuildCheckConstraint(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op on Commit

	_, err = tx.ExecContext(ctx, `ALTER TABLE tasks RENAME TO tasks_old`)
	if err != nil {
		return fmt.Errorf("tasks rebuild rename: %w", err)
	}
	if _, err := tx.ExecContext(ctx, tasksSchemaTable()); err != nil {
		return fmt.Errorf("tasks rebuild create: %w", err)
	}
	// The copy SELECT must adapt to WHICH columns the old table actually
	// has: a pre-P2 ledger may predate model too (added by the same
	// ALTER-after-create pattern), and every missing column is filled with
	// its default (model/tags/payload/claimed_by='', source=<the schema
	// default>, claimed_at/ended_at NULL) — legacy rows keep their data verbatim,
	// missing columns are defaults, nothing errors.
	oldCols := map[string]bool{}
	colRows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info('tasks_old')`)
	if err != nil {
		return fmt.Errorf("tasks rebuild probe: %w", err)
	}
	for colRows.Next() {
		var c string
		if err := colRows.Scan(&c); err != nil {
			colRows.Close() //stylegate:ignore read-only cursor whose iteration already ended
			return fmt.Errorf("tasks rebuild probe: %w", err)
		}
		oldCols[c] = true
	}
	colRowsErr := colRows.Err()
	colRows.Close() //stylegate:ignore read-only cursor whose iteration already ended
	if colRowsErr != nil {
		return fmt.Errorf("tasks rebuild probe: %w", colRowsErr)
	}

	colExpr := func(name, fallback string) string {
		if oldCols[name] {
			return name
		}
		return fallback + " AS " + name
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tasks
		(id, task_id, summary, status, session_id, `+taskSubmitterColumnName+`, model,
		 parent_task_id, `+taskParentIDsColumnName+`, tags, payload, source, claimed_by, claimed_at,
		 verify_evidence, deliverable, run_method, run_cmd, reason,
		 created_at, updated_at, ended_at,
		 `+taskTasklistIssuedAtColumn+`, `+taskLastToolcallAtColumn+`, `+tasksInProgressAtColumnName+`,
		 `+tasksProcPidColumnName+`, `+tasksProcStartColumnName+`, `+tasksProcCmdColumnName+`,
		 `+tasksLockPhaseColumnName+`)
		SELECT id, task_id, summary, status, session_id,
		       `+colExpr(taskSubmitterColumnName, "''")+`,
		       `+colExpr("model", "''")+`,
		       `+colExpr("parent_task_id", "''")+`,
		       `+colExpr(taskParentIDsColumnName, "''")+`,
		       `+colExpr("tags", "''")+`,
		       `+colExpr("payload", "''")+`,
		       `+colExpr("source", "''")+`,
		       `+colExpr("claimed_by", "''")+`,
		       `+colExpr("claimed_at", "NULL")+`,
		       `+colExpr("verify_evidence", "''")+`,
		       `+colExpr("deliverable", "''")+`,
		       `+colExpr("run_method", "''")+`,
		       `+colExpr("run_cmd", "''")+`,
		       `+colExpr("reason", "''")+`,
		       created_at, updated_at,
		       `+colExpr("ended_at", "NULL")+`,
		       `+colExpr(taskTasklistIssuedAtColumn, "NULL")+`,
		       `+colExpr(taskLastToolcallAtColumn, "NULL")+`,
		       `+colExpr(tasksInProgressAtColumnName, "NULL")+`,
		       `+colExpr(tasksProcPidColumnName, "NULL")+`,
		       `+colExpr(tasksProcStartColumnName, "NULL")+`,
		       `+colExpr(tasksProcCmdColumnName, "''")+`,
		       `+colExpr(tasksLockPhaseColumnName, "''")+`
		FROM tasks_old`)
	if err != nil {
		return fmt.Errorf("tasks rebuild copy: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE tasks_old`); err != nil {
		return fmt.Errorf("tasks rebuild drop: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tasks rebuild commit: %w", err)
	}
	// FTS rescue, AFTER the commit (the 'rebuild' command must see the new
	// tasks table's rows): SQLite ≥3.25 rewrites same-table trigger bodies
	// to follow the RENAME (ON tasks → ON tasks_old), so the DROP of
	// tasks_old above silently deleted tasks_ai/tasks_au/tasks_ad, and the
	// copy INSERTs that ran under those (renamed-away) triggers never
	// indexed the migrated rows into tasks_fts/tasks_tri. The trigger
	// re-creation in ensureTasksSchema restores FUTURE sync, but the
	// migrated rows stay invisible to search forever — an external-content
	// FTS5 index holds only what the ('rebuild') command put there. So:
	// explicitly drop/recreate the companions and repopulate every row from
	// tasks via the 'rebuild' command (t-1788053263-1; isolation-proven on
	// sqlite 3.37.2 — fts count survives the rename, but FRESH inserts are
	// silently NOT indexed and no trigger survives the tasks_old DROP).
	// Idempotent: a rebuild that didn't happen is a no-op re-index.
	fts, tri, ai, au, ad := tasksFTSDefs()
	for _, idx := range []string{"tasks_fts", "tasks_tri"} {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+idx); err != nil {
			return fmt.Errorf("tasks rebuild fts drop %s: %w", idx, err)
		}
	}
	for _, stmt := range []string{fts, tri} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("tasks rebuild fts create: %w", err)
		}
	}
	for _, idx := range []string{"tasks_fts", "tasks_tri"} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO `+idx+`(`+idx+`) VALUES('rebuild')`); err != nil {
			return fmt.Errorf("tasks rebuild fts repopulate %s: %w", idx, err)
		}
	}
	// Sync triggers LAST — the 'rebuild' command itself must not fire them
	// (they were already deleted with tasks_old; re-creating them here
	// restores the AFTER INSERT/UPDATE/DELETE sync for future rows).
	for _, stmt := range []string{ai, au, ad} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("tasks rebuild fts triggers: %w", err)
		}
	}
	return nil
}

// ensureTasksSchema creates the tasks ledger table and its indexes.
// Idempotent — safe to run every start (the ensureSchema pattern). The
// status index is value-signal from day one: the --status filter walks it
// instead of a full scan (the memory-lowquality lesson — ranking/filter
// columns must be live on day one, never inert decoration).
// M4.13 sync_state — one row per sync PEER, the delta-sync version anchor's
// persistence (epic memsync-timestamp-delta: 断线/重启恢复继续，不重复推).
// M4.12's task harvest created this table FIRST with (peer, anchor,
// updated_at); M4.13 keeps THAT shape (one upgrade path, no rival schema):
//
//	anchor      — the delta cutoff in the PEER'S row domain: rows with
//	              updated_at > anchor - margin are re-exported next session.
//	              Both verbs read/write it (harvest applies; sync commits).
//	updated_at  — the LOCAL wall clock of the last successful session
//	              (observability only; never a cutoff — the two domains
//	              never mix).
//
// Legacy M4.8-era two-column rows (last_synced_at/last_synced_at_source,
// never shipped in a release) are migrated in place: last_synced_at_source
// → anchor. syncStateGet/syncStatePut are the ONLY sync_state accessors
// outside harvest (Rule 2).
func syncStateSchemaTable() string {
	return `CREATE TABLE IF NOT EXISTS sync_state (
	peer       TEXT PRIMARY KEY,
	anchor     REAL NOT NULL DEFAULT 0,
	updated_at REAL NOT NULL DEFAULT 0
)`
}

// syncStateGet reads one peer's anchors (both 0 = never synced). Returns
// (localClock, peerDomainAnchor).
func syncStateGet(ctx context.Context, db *sql.DB, peer string) (lastSynced, anchor float64, err error) {
	row := db.QueryRowContext(ctx,
		`SELECT updated_at, anchor FROM sync_state WHERE peer = ?`, peer)
	if err = row.Scan(&lastSynced, &anchor); err == sql.ErrNoRows {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	return lastSynced, anchor, nil
}

// syncStatePut upserts one peer's anchors (the ONLY sync_state writer
// shared by harvest and the M4.13 session driver).
func syncStatePut(ctx context.Context, db *sql.DB, peer string, lastSynced, anchor float64) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO sync_state (peer, anchor, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(peer) DO UPDATE SET
		   anchor = excluded.anchor,
		   updated_at = excluded.updated_at`,
		peer, anchor, lastSynced)
	return err
}

func ensureTasksSchema(ctx context.Context, db *sql.DB) error {
	// Legacy four-status DBs must rebuild BEFORE any ALTER touches them:
	// the rebuild's INSERT carries the full P2 column list, which only
	// exists on a table created from the new literal.
	needRebuild, err := tasksNeedsRebuild(ctx, db)
	if err != nil {
		return err
	}
	if needRebuild {
		if err := tasksRebuildCheckConstraint(ctx, db); err != nil {
			return err
		}
	}
	// The schema DDL below is published as ONE batch on the paths that pay a
	// device sync for every implicit commit (see ledgerBuildBatchNeeded and
	// firstOpenSyncMode): a fresh ledger's first open then costs one commit
	// instead of ~a dozen, and each removed commit is a removed sync on the
	// device's journal queue. On a memory filesystem the statements keep their
	// original autocommit form, byte for byte — nothing is paid there either
	// way, so the working path is not perturbed.
	//
	// The ALTER statements keep their per-statement "duplicate column name"
	// tolerance inside the batch: a failed statement does not abort an explicit
	// SQLite transaction, and every other error still returns (the batch's own
	// deferred rollback releases the write lock, so a partial schema is never
	// published).
	ddl := func(q QueryExecer) error {
		for _, s := range []string{
			tasksSchemaTable(), tasksSchemaIndexStatus, tasksSchemaIndexTaskID,
			tasksSchemaEventsTable(), tasksSchemaIndexEventTask, // bug056 append-only audit trail
			syncStateSchemaTable(), // M4.13 delta-sync version anchors (peer-keyed)
		} {
			if _, err := q.ExecContext(ctx, s); err != nil {
				return err
			}
		}
		fts, tri, ai, au, ad := tasksFTSDefs()
		for _, s := range []string{fts, tri, ai, au, ad} {
			if _, err := q.ExecContext(ctx, s); err != nil {
				return err
			}
		}
		// model column migration: CREATE TABLE IF NOT EXISTS does NOT touch an
		// existing table, so a pre-model ledger (created before this column was
		// added) must be widened in place. "duplicate column name" is the
		// expected no-op on already-migrated DBs — swallow it (the ensureSchema
		// pattern keeps its zero-migration-script promise).
		if _, err := q.ExecContext(ctx, tasksModelColumn); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
		// P2 dispatcher columns: same idempotent ALTER pattern. On a rebuilt
		// table these are all present already (every ALTER is a swallowed
		// no-op); on a fresh-P2-but-missing-one-column table (e.g. built by an
		// intermediate dev build) it heals just the missing one.
		for _, col := range tasksP2Columns {
			if _, err := q.ExecContext(ctx, col); err != nil {
				if !strings.Contains(err.Error(), "duplicate column name") {
					return err
				}
			}
		}
		// Process-identity columns (epic task-process-identity): same idempotent
		// ALTER pattern, one definition in tasksProcIdentityAlters so the schema
		// literal, the ALTER list and the rebuild copy name the same columns.
		for _, col := range tasksProcIdentityAlters() {
			if _, err := q.ExecContext(ctx, col); err != nil {
				if !strings.Contains(err.Error(), "duplicate column name") {
					return err
				}
			}
		}
		// Submitter-provenance column (row-identity epic, 2026-10-28): the
		// same idempotent ALTER pattern. It splits the SUBMITTER's session out
		// of `session_id`, which now carries the row's own id (the run
		// identity). Legacy rows keep submitter_session='' — historical rows are
		// never rewritten, so their `session_id` keeps its submitter value and
		// the readers treat it accordingly (see taskSubmitterSessionOf).
		if _, err := q.ExecContext(ctx, taskSubmitterColumnAlter); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
		return nil
	}
	if ledgerBuildBatchNeeded(memoryDBPath()) {
		if err := execSchemaBatch(ctx, db, ddl); err != nil {
			return err
		}
	} else {
		// The autocommit form: on a memory-backed ledger the batch buys no
		// measurable time, and keeping the original statement sequence here is
		// what keeps this change free of any effect on the working path.
		if err := ddl(db); err != nil {
			return err
		}
	}
	// M4.11 backfill (t-1788087991): rows predating the priority column
	// are NULL — the legacy bug-tagged pendings would queue behind feature
	// rows forever. Each NULL row re-derives through taskTagPriority (the
	// ONE mapping — zero SQL/Go parity risk); explicitly stamped rows
	// (non-NULL) are never touched. Idempotent: after one pass no NULL
	// bug rows remain.
	rows, err := db.QueryContext(ctx,
		`SELECT id, COALESCE(tags,'') FROM tasks WHERE priority IS NULL`)
	if err != nil {
		return fmt.Errorf("task priority backfill select: %w", err)
	}
	type idPrio struct {
		id   int64
		prio int
	}
	var updates []idPrio
	for rows.Next() {
		var id int64
		var tags string
		if err := rows.Scan(&id, &tags); err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			return fmt.Errorf("task priority backfill scan: %w", err)
		}
		updates = append(updates, idPrio{id, taskTagPriority(tags)})
	}
	rowsErr := rows.Err()
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
	if rowsErr != nil {
		return fmt.Errorf("task priority backfill rows: %w", rowsErr)
	}
	for _, u := range updates {
		if _, err := db.ExecContext(ctx,
			`UPDATE tasks SET priority = ? WHERE id = ?`, u.prio, u.id); err != nil {
			return fmt.Errorf("task priority backfill update: %w", err)
		}
	}
	// FTS index-shape migration: an existing ledger's tasks_fts/tasks_tri
	// were built over the P0 column set (task_id, session_id, summary).
	// CREATE VIRTUAL TABLE IF NOT EXISTS will NOT widen them, and the new
	// triggers reference the P2 columns — an unmigrated index would corrupt
	// on the first ai firing. Drop + recreate + repopulate from the base
	// table (the rebuild command rebuilds an external-content index from
	// its content table by design).
	return migrateTasksFTS(ctx, db)
}

// migrateTasksFTS reconciles the FTS/trigram index shapes with the current
// tasksFTSColumns set: indexes missing any column are dropped, recreated
// with the new shape, and repopulated (INSERT INTO tasks_fts(tasks_fts)
// VALUES('rebuild') reads every row back from tasks). Idempotent.
func migrateTasksFTS(ctx context.Context, db *sql.DB) error {
	for _, idx := range []struct{ name string }{
		{"tasks_fts"}, {"tasks_tri"},
	} {
		rows, err := db.QueryContext(ctx,
			`SELECT name FROM pragma_table_info(?)`, idx.name)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
				return err
			}
			have[col] = true
		}
		rowsErr := rows.Err()
		rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
		if rowsErr != nil {
			return rowsErr
		}
		missing := false
		for _, c := range tasksFTSColumns {
			if !have[c] {
				missing = true
				break
			}
		}
		if !missing {
			continue
		}
		fts, tri, ai, au, ad := tasksFTSDefs()
		var defs []string
		if idx.name == "tasks_fts" {
			defs = []string{
				"DROP TRIGGER IF EXISTS tasks_ai", "DROP TRIGGER IF EXISTS tasks_au",
				"DROP TRIGGER IF EXISTS tasks_ad",
				"DROP TABLE IF EXISTS tasks_fts", fts,
			}
		} else {
			defs = []string{"DROP TABLE IF EXISTS tasks_tri", tri}
		}
		for _, stmt := range defs {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO `+idx.name+`(`+idx.name+`) VALUES('rebuild')`); err != nil {
			return err
		}
		// Recreate the (dropped) sync triggers last, so the rebuild itself
		// does not double-fire them.
		for _, stmt := range []string{ai, au, ad} {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

// openTasksDB opens gopi's own memory DB read-write for the tasks ledger.
// A missing file is initialized as a full knowledge base (distilled_qa +
// feedback + tasks). An existing file is accepted only when it is a
// gopi-owned DB: the default path is healed in place (migrateDefaultTriggers
// pattern — a pre-tasks ~/.gopi/memory.db gains the table), and a custom
// --db file is accepted when it carries the gopi-specific memory_feedback
// table. A foreign distilled DB (hermes' production distilled_knowledge.db
// has distilled_qa but no memory_feedback) is REJECTED — the ledger never
// creates tables inside a foreign SQLite DB (the "never mutate foreign DBs"
// rule, extended from rows to schema).
func openTasksDB(ctx context.Context, path string) (*sql.DB, error) {
	if !fileExists(path) {
		return ensureKnowledgeBase(ctx, path, false)
	}
	db, err := openDB(path, false)
	if err != nil {
		return nil, err
	}
	ok, err := tableExists(ctx, db, "distilled_qa")
	if err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return nil, err
	}
	if !ok {
		// The file itself answers the race the poll can only guess at: zero
		// schema objects = a first open nobody has published yet (the build
		// commits atomically, so a concurrent initializer is observable as an
		// EMPTY file, never as a half-built schema) — join that
		// initialization instead of refusing a ledger that is merely not
		// built. Same recovery as openStoreDB/openSearchDB.
		if db2, err := adoptUninitializedKnowledgeBase(ctx, path, false, db); err != nil || db2 != nil {
			return db2, err
		}
		// The file carries objects. Before calling it foreign, ask WHOSE they
		// are: a file holding ONLY gopi's own objects is gopi's ledger in a
		// PARTIAL shape — the shape an opener that predates the union rule
		// left behind (measured 2026-10-27: `gopi session list` on a fresh
		// custom path publishes a file with 10 chats-family objects and no
		// distilled_qa, after which this function refused it for ever with
		// "missing distilled_qa table"). ADOPT it: complete the ledger with
		// the union build. The refusal below is unchanged for every file that
		// carries an object gopi did not mint — that is what keeps hermes'
		// distilled_knowledge.db (and every other foreign DB) refused.
		ownedOnly, censusErr := ledgerHoldsOnlyGopiObjects(ctx, db)
		if censusErr == nil && ownedOnly {
			db.Close() //stylegate:ignore probe handle closed before its replacement is opened
			return ensureKnowledgeBase(ctx, path, false)
		}
		// The probe did not recognize the file as ours: it may still be a
		// concurrent first-open mid-CREATE (WAL readers no longer block on the
		// writer). Give it a brief window before declaring the file foreign.
		if waitForDistilledSchema(ctx, db) {
			db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
			return openTasksDB(ctx, path)
		}
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		if isDefaultMemoryDB(path) {
			return ensureKnowledgeBase(ctx, path, false)
		}
		return nil, fmt.Errorf("gopi task: %s is not a distilled knowledge base (missing distilled_qa table)", path)
	}
	// gopi-owned DB check: the memory_feedback table is gopi-specific (the
	// hermes production copy has distilled_qa but no feedback table). The
	// table name is a DB schema identifier, deliberately NOT the toolnames
	// constant: renaming the feedback tool must not migrate the gopi DB
	// table name out from under existing deployments.
	owned, err := tableExists(ctx, db, "memory_feedback")
	if err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return nil, err
	}
	if !owned && !isDefaultMemoryDB(path) {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return nil, fmt.Errorf("gopi task: %s is a foreign distilled DB (no memory_feedback) — the tasks ledger only lands in gopi's own memory db", path)
	}
	if err := ensureTasksSchema(ctx, db); err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return nil, err
	}
	return db, nil
}

// taskIDNormalize is the ONE seam for task_id whitespace normalization —
// Rule 14's single source of truth for a value that thirty ledger entry
// points need (the Rule 13 anti-drift gate is TestTaskIDNormalizeIsTheSingleSeam).
//
// The defect this closes: an operator-supplied id carries copy-paste
// whitespace ("  t-1 \n "), and the trim was SPLIT among the entry points —
// TaskShow trimmed, TaskResumeCount did not; taskUpdate trimmed, taskSessionDir
// did not. So one padded id resolved through one accessor and read as
// "no task with task_id" / "0 resumptions" / a SECOND filesystem container
// under the padded name through its sibling. The rule lives here once and
// every entry point REACHES it instead of re-typing strings.TrimSpace.
//
// Empty-after-trim is not a lookup: each entry point keeps its own refusal
// (error for the readers, 0 for the count) — a blank id names no row.
func taskIDNormalize(taskID string) string {
	return strings.TrimSpace(taskID)
}

// taskIDPrefixCandidateCap bounds how many full ids a truncated-id refusal
// NAMES at the ledger seams. A uuid-v7 prefix is rarely ambiguous (a
// 8-character head named 35 of 2409 rows on this host, and a 12-character head
// — the first dash group an operator copies — is generally unique), so the cap
// is headroom, not a working limit: it keeps ONE operand from rendering an
// unbounded refusal, and the count line always states the REAL total, so a
// truncated list is never read as the whole set (Rule 15). A named constant,
// never a literal (Rule 1).
const taskIDPrefixCandidateCap = 12

// taskIDNearMissSuffix renders the REPAIR clause for an operand that resolves to
// no row BECAUSE its dashes are wrong: the same uuid's 16 bytes in a
// non-canonical grouping (measured 2026-10-12: `01a12f96-c2bf-732f-8cd24b63a367d4af`
// — one dash dropped — while the row lives under the canonical spelling, and
// both spellings' session containers stand side by side). It is EMPTY for every
// other operand, so an ordinary unknown id keeps its ordinary answer.
//
// A SHAPE alone does not earn the advice — the LEDGER FACT decides (measured
// 2026-10-12, bug 01a130e4): `task show 00000000-0000-0000-0000-000000000000`
// answered "it is … a non-canonical spelling (dashes dropped or moved); did you
// mean %q?" quoting the operand ITSELF, because a canonical uuid is never
// "re-grouped" by sessionid.CanonicalSpellingOf and the two spellings are
// therefore equal. The same false advice met an id that exists on ANOTHER host's
// ledger (`01a12cb3-53c8-720b-a04e-bccdbd538ba2`), while the true answer — this
// row is not in THIS ledger — was replaced by "change your spelling", which sent
// the reporting run looking for a typo that was not there.
//
// So the clause is printed only when the spelling it names is a DIFFERENT one
// (`canonical != taskID`) — i.e. re-grouping the operand is a real change, the
// honest "you typed a near miss" case — AND the ledger holds that canonical row,
// i.e. the advice is a reachable repair. The fix belongs to whichever row the
// replay names, never to the ledger that failed to hold it.
//
// The rule is internal/sessionid's, never a second uuid comparison here: this
// only phrases it for the LEDGER's own refusal, so the two faces an operator
// meets (`task show`'s lookup and the session-dir seam) say the same thing about
// the same mistake.
func taskIDNearMissSuffix(ctx context.Context, db *sql.DB, taskID string) string {
	canonical, ok := sessionid.CanonicalSpellingOf(taskID)
	if !ok {
		// A spelling sessionid cannot re-group at all (a legacy `t-…` id, a
		// name) is not this clause's operand.
		return ""
	}
	if canonical == taskID {
		// The operand IS the canonical spelling: there is nothing to re-spell,
		// and a "did you mean <the same id>" is not an answer — the row is
		// simply not in this ledger (Rule 15: a wrong "did you mean" is worse
		// than none).
		return ""
	}
	// The advice must be a REPAIR: name the canonical row only when this ledger
	// really holds it. A candidate that is absent (the row lives on a peer's
	// ledger, or was pruned) leaves the operator with the ordinary answer.
	// A failed read offers no advice either — the refusal it decorates already
	// stands, so the emptiness itself is the error's record (Rule 15).
	row, err := taskByID(ctx, db, canonical)
	if err != nil || row == nil {
		return ""
	}
	return fmt.Sprintf(" — it is a uuid's 16 bytes in a non-canonical spelling (dashes dropped or moved); did you mean %q?", canonical)
}

// taskIDPrefixRefusal renders the refusal a TASK-id OPERAND THAT IS A PREFIX
// gets at a ledger reader — the negation of the "no task with task_id" sentence
// the seam otherwise prints.
//
// WHY IT EXISTS (Rule 41 anomaly, measured 2026-10-13 on this host). A 12-char
// prefix (the first dash group an operator copies out of a listing — the shape
// internal/fullid.ShortPrefixLen describes) reached `gopi task show` and got
// "task: no task with task_id=\"01a132d6-5ac1\"" — the SAME sentence the seam
// prints for a typo, for a row that lives on a peer's ledger, and for a
// near-miss spelling. The sentence was literally true and the operator could not
// act on it: nothing said the operand was a PREFIX of a row this ledger holds.
// The rule was already implemented next door, for exactly this operand shape:
// internal/fullid owns "a prefix is NEVER resolved; it is refused, and the
// refusal names every full id it could have meant" (fullid.Refusal), reached by
// the session-resume seam and the row send/move leg (syncRowResolveTaskID) —
// but not by the ledger show seam. This closes that gap; it is a MESSAGE, never
// a resolution path: nothing here looks the prefix up and returns a row.
//
// An operand that is NOT the uuid shape cut short (`t-<epoch>`, a name, an
// empty string) gets "" — the caller keeps its ordinary answer, because two
// refusals for one fact would be two places to keep in step, and a wrong
// "did you mean" is worse than none (Rule 15; bug 01a130e4's precedent).
//
// The candidate read is the scan both ledger seams already share
// (taskIDPrefixCandidates). Candidates are CAPPED at taskIDPrefixCandidateCap
// for rendering only: the count line carries the real total, so a capped
// refusal can never be read as the whole candidate set.
func taskIDPrefixRefusal(ctx context.Context, db *sql.DB, taskID string) string {
	if !fullid.CanBePrefix(taskID) {
		// Not the shape of a canonical id cut short: there is no prefix to
		// explain, and the seam's ordinary answer already stands.
		return ""
	}
	cands, err := taskIDPrefixCandidates(ctx, db, taskID)
	if err != nil {
		// A failed read offers no advice — the ordinary refusal it decorates
		// already stands, so the emptiness itself is the error's record
		// (Rule 15). Nothing is silently widened into a "the prefix names
		// nothing" claim the read did not establish.
		return ""
	}
	if len(cands) == 0 {
		// The shape is a prefix, but THIS ledger holds no row it could have
		// meant (pruned, or the row lives on a peer). An empty candidate list
		// still earns the shape sentence (fullid.Refusal's zero-candidate arm):
		// that the operand is not a full id is a fact about the OPERAND,
		// independent of which ledger is asked.
		return " — " + fullid.Refusal(fullid.KindTask, taskID, nil).Error()
	}
	named, remainder := cands, ""
	if len(named) > taskIDPrefixCandidateCap {
		total := len(named)
		named = named[:taskIDPrefixCandidateCap]
		// The count fullid.Refusal renders is the length of the list it is
		// GIVEN, so a capped list states the capped count. This clause carries
		// the real total, in the same sentence, so the two numbers cannot be
		// read as a contradiction or as the whole set (Rule 15).
		remainder = fmt.Sprintf(" — %d of %d identities sharing that prefix are named above, in claim order", taskIDPrefixCandidateCap, total)
	}
	return " — " + fullid.Refusal(fullid.KindTask, taskID, named).Error() + remainder
}

// taskByID fetches one task row by task_id. Returns (nil, nil) when absent.
func taskByID(ctx context.Context, db *sql.DB, taskID string) (*Task, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+taskSelectColumns+` FROM tasks WHERE task_id = ?`, taskID)
	t, err := scanTaskRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// taskByIDTx is taskByID for a row seen INSIDE an open transaction (the
// import's single-tx path — check-then-act must share the tx's snapshot, not
// peek through a second connection, which would not see the tx's own
// uncommitted writes).
func taskByIDTx(ctx context.Context, tx *sql.Tx, taskID string) (*Task, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT `+taskSelectColumns+` FROM tasks WHERE task_id = ?`, taskID)
	t, err := scanTaskRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// scanTaskRow adapts scanTask to a single-row QueryRow (its error carries
// sql.ErrNoRows through for the caller to interpret).
func scanTaskRow(row *sql.Row) (*Task, error) {
	return scanTaskInto(row)
}

// taskByIDOn is taskByID over whichever handle the caller already owns — its
// *sql.Tx inside an open transaction or an *sql.DB elsewhere (QueryExecer is
// satisfied by both). Same reason as taskByIDTx: a guard that must READ the
// row and then WRITE to it has to do both on one connection — a second
// connection neither sees the transaction's uncommitted writes nor gets to
// write while it is open.
func taskByIDOn(ctx context.Context, q QueryExecer, taskID string) (*Task, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+taskSelectColumns+` FROM tasks WHERE task_id = ?`, taskID)
	t, err := scanTaskRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// taskIDExists reports whether a task_id is already taken (used by the
// auto-generation collision loop).
func taskIDExists(ctx context.Context, db *sql.DB, taskID string) (bool, error) {
	var one int
	err := db.QueryRowContext(ctx, "SELECT 1 FROM tasks WHERE task_id = ?", taskID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// taskModelUnknown is the model value rendered when a task row carries no
// model: rows created before model persistence (model=”) or tasks created
// without any model context. Pre-persistence rows still resolve their model
// at show time from the session JSONL (taskModelFromSession) so history
// keeps displaying; new rows are persist-the-model-at-create-time by design.
const taskModelUnknown = "(unknown)"

// taskIDGenerate mints a unique auto task_id (uuid v7 — time-ordered, no
// clock collision in practice) against an open ledger. Shared by
// TaskNew, TaskSubmit and auto-registration so every auto-id path mints
// the same shape. uuid v7 was chosen over v4 for time sortability; the
// collision-retry loop is retained as defense-in-depth (a v7 collision is
// ~impossible, but the loop makes the invariant unconditional).
func taskIDGenerate(ctx context.Context, db *sql.DB) (string, error) {
	for attempt := 0; ; attempt++ {
		id, err := uuid.NewV7()
		if err != nil {
			return "", fmt.Errorf("task: mint uuid: %w", err)
		}
		cand := id.String()
		taken, err := taskIDExists(ctx, db, cand)
		if err != nil {
			return "", err
		}
		if !taken {
			return cand, nil
		}
		if attempt >= taskIDMaxAttempts {
			return "", fmt.Errorf("task: could not generate a unique task_id after %d attempts", taskIDMaxAttempts)
		}
	}
}

// taskCreateFields is the single all-column NEW-row surface behind every
// ledger INSERT (task-create-unified epic, 2026-09-05). Auto-registration,
// bare-prompt / raw sessions (TaskNewReturningID), queue submit (TaskSubmit)
// and `gopi task clone` all converge here so ONE code
// path and ONE INSERT carry the full schema — every creation is a row with
// the same fields, differences reduced to zero-value defaults (a field a
// caller does not set lands as its column default, never as a missing
// column). The 13 operator-visible create columns are named explicitly; a
// zero value (zero string for a TEXT column, 0 priority) means "use the
// schema default — pick it up defaulted, exactly as a column omitted from
// the INSERT already did."
//
//	column           field           caller sets it when…
//	task_id          TaskID          caller wants a fixed id ("" = auto)
//	summary          Summary         always (required)
//	status           Status          always (in_progress / pending / …)
//	session_id       SessionID       the session that owns / submitted
//	model            Model           captured at creation (task-show-model)
//	parent_task_id   ParentTaskID    the split child's tree link
//	tags             Tags            comma-canonical tag list
//	payload          Payload         the execution briefing (submit)
//	source           Source          who enqueued (llm|human|auto)
//	deliverable      Deliverable     the repo JSON (M4.8)
//	priority         Priority        0 = derive (bug→1, docs→8, else 5)
//	reason           Reason          the operator-visible WHY
type taskCreateFields struct {
	TaskID  string
	Summary string
	Status  string
	// SessionID is the row's RUN SESSION — the uuid naming its record under
	// sessions/<task-id>/. EMPTY for a row that is being FILED (its run has not
	// started, so the id does not exist yet); set by the auto-register path,
	// where the creating process IS the run. The deferred stamp fills it later
	// while it is empty, so leaving it zero here is what makes the column
	// writable at all.
	SessionID string
	// SubmitterSession is who FILED the row — the anti-self-reference key. It is
	// a DIFFERENT fact from SessionID and lives in its own column
	// (taskSubmitterColumnName): one cell could not carry both, and the attempt
	// cost every submitted row its run session (2026-10-28).
	SubmitterSession string
	Model            string
	ParentTaskID     string
	// ParentTaskIDs is the second-and-later parent list written to
	// parent_task_ids (EPIC multi-parent). insertTaskRow is the ONE writer, and
	// TaskSubmit folds the caller's parents through taskParentsForWrite so the
	// "parent 1 to the old column, the rest here" split is decided once.
	ParentTaskIDs string
	Tags          string
	Payload       string
	Source        string
	Deliverable   string
	Priority      int
	Reason        string
	// RunMethod/RunCmd are the row's RUN PROVENANCE (internal/runcmd): HOW
	// the execution was launched and the verbatim re-runnable command line.
	// They are part of the CREATE surface on purpose — the rows this epic
	// fixes are born in_progress by the process that IS the run (the
	// harness auto-register hook, the merger's resolve row), so a
	// create-then-stamp pair would leave a window in which `task show`
	// answers "no command known" for a row that is already running. A
	// zero value (both empty) writes the column defaults, exactly like
	// every other unset field here.
	RunMethod string
	RunCmd    string
}

// taskPriorityForZero resolves a zero Priority per M4.11 (ONE mapping in
// taskTagPriority). A caller that did not pick a priority gets the same
// tier the ensureSchema backfill derives from the tags — so a UNIFIED row
// is never "priority=0" dangling above the backfill's derivation.
func taskPriorityForZero(f taskCreateFields) int {
	if f.Priority > 0 {
		return f.Priority
	}
	return taskTagPriority(f.Tags)
}

// insertTaskRow is THE single row-creating INSERT behind every operator /
// agent NEW-row path (task-create-unified). It owns task_id generation /
// uniqueness, the status/summary/source invariants the callers already
// guard, and writes ONE full-column row through `taskCreateFields` — fields
// a caller left zero-valued fall back to their column defaults (empty
// strings / derived priority), so no caller can silently drop a column the
// unified schema expects. Returns the ledger task_id. created_at/updated_at
// are both stamped NOW (a fresh row has no update history yet).
//
// The TRIGGER-synced FTS companions index a FIXED column set
// (tasksFTSColumns: task_id/session_id/summary/parent_task_id/tags/payload/
// source) regardless of which columns an INSERT names — the old 7-column
// INSERT and the unified 14-column INSERT hit the same AFTER INSERT
// trigger, so the FTS surface (and its search behaviour) is untouched by
// unification. The columns the helper ADDS over the old sparse path
// (deliverable/priority/reason) are NOT search columns — they land in the
// tasks row only.
func insertTaskRow(ctx context.Context, db *sql.DB, f taskCreateFields) (string, error) {
	// TEXT BOUNDARY (epic binary-summary-guard): the ledger's summary/payload
	// are DISPLAY TEXT — every row's headline is read by `task list`/`show`,
	// serialised by sync and redirected into files by callers. A caller can
	// hand this engine arbitrary bytes (the motivating case: a bare-prompt run
	// that read a binary as its text input file and stored an ELF image as its
	// summary — devdoc/binary-summary-guard.md), and a NUL in a text column is
	// a data-integrity defect wherever it lands. So the guard is applied to
	// EVERY row at the ONE insert path (Rule 2: one writer), not at the call
	// sites: identity for ordinary text, and it never silently drops the
	// evidence (see tasksummarytext.go).
	f.Summary = sanitizeTaskSummary(f.Summary)
	f.Payload = sanitizeTaskPayloadText(f.Payload)
	f.Summary = strings.TrimSpace(f.Summary)
	if f.Summary == "" {
		return "", fmt.Errorf("task: summary must not be empty")
	}
	f.Tags = normalizeTaskTags(f.Tags)
	f.Source = strings.TrimSpace(f.Source)
	if f.Source == "" || tasksource.IsHumanClaim(f.Source) {
		// The default is DERIVED from this process's own evidence, never
		// spelled here (Rule 14) — and a stated `human` is held to the same
		// evidence, because this insert is the last shared stop before the row
		// is written. A literal default is how rows came to claim provenances
		// their writers never had.
		f.Source = tasksource.DeriveNow(f.Source)
	} else if value, ok := tasksource.Normalize(f.Source); ok {
		// Canonicalize a retired spelling for a caller that bypassed
		// TaskSubmit (this function is also reached by the auto-registration
		// and reissue faces). An UNKNOWN value is left for the caller's own
		// validation to refuse — never silently rewritten.
		f.Source = value
	}
	f.TaskID = taskIDNormalize(f.TaskID)
	if f.Status == "" {
		f.Status = taskStatusInProgress
	}
	var err error
	if f.TaskID == "" {
		f.TaskID, err = taskIDGenerate(ctx, db)
		if err != nil {
			return "", err
		}
	} else {
		taken, err := taskIDExists(ctx, db, f.TaskID)
		if err != nil {
			return "", err
		}
		if taken {
			return "", fmt.Errorf("task: task_id %q already exists", f.TaskID)
		}
	}
	f.Priority = taskPriorityForZero(f)
	now := float64(time.Now().Unix())
	// THE ROW'S TWO SESSION FACTS, written to their OWN columns (2026-10-28).
	//
	//   session_id        — the RUN session (empty until the run starts, except
	//                       on the auto-register path where f.SessionID states it);
	//   submitter_session — who FILED the row.
	//
	// Until the split BOTH wrote `session_id`: the subset of callers that pass a
	// submitter made the cell non-empty at birth, so the deferred stamp — whose
	// guard is "fill only while empty" — could never land the run's session, and
	// the column named a session that never ran the row for the row's whole life.
	// The two facts are now separate fields, and each keeps its own meaning.
	_, err = db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, session_id, `+taskSubmitterColumnName+`, model,
		                    parent_task_id, `+taskParentIDsColumnName+`, tags, payload, source, deliverable, priority, reason,
		                    run_method, run_cmd, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.TaskID, f.Summary, f.Status, strings.TrimSpace(f.SessionID), strings.TrimSpace(f.SubmitterSession), f.Model,
		f.ParentTaskID, f.ParentTaskIDs, f.Tags, f.Payload, f.Source, f.Deliverable, f.Priority, f.Reason,
		strings.TrimSpace(f.RunMethod), strings.TrimSpace(f.RunCmd), now, now)
	if err != nil {
		return "", err
	}
	return f.TaskID, nil
}

// TaskNewOptions is the extended NEW-row create surface (task-create-unified
// epic, 2026-09-05): the plain 4-string TaskNew / TaskNewReturningID entry
// points stay for compatibility — they map to a zero-valued TaskNewOptions
// with only summary/task_id/session_id/model filled — while callers that
// need the FULL schema on creation (auto-register bare-prompt/raw rows,
// resolve-conflict registration) go through TaskNewOID and stamp
// payload/tags/source/deliverable/reason/priority/parent_task_id in
// the same INSERT. Backward compatible by construction: existing callers of
// the 4-string entry points are unchanged and behave identically.
type TaskNewOptions = taskCreateFields

// TaskNew creates a task ledger record. taskID defaults to t-<unix seconds>
// (with a numeric suffix on collision); an explicit taskID must be unique —
// a duplicate is an error, never silently mangled. summary is required.
// sessionID tags the originating session (may be empty — the CLI has no
// session context; the harness can fill it later). model is the name of the
// model running the task — captured AT CREATION and persisted in the row
// (the root-cause fix for task-show-model: model is ledger DATA, not a
// display-time lookup). The CLI passes --model; the auto-register harness
// passes the agent's own model. Returns the rendered confirmation, mirroring
// the memory tool contract.
func TaskNew(summary, taskID, sessionID, model string) (string, error) {
	id, err := TaskNewReturningID(summary, taskID, sessionID, model)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Task created: %s (status=%s, model=%s)", id, taskStatusInProgress, model), nil
}

// TaskNewReturningID is TaskNew for callers that need the ROW's task_id
// back (the auto-register start hook: with task_id auto-generated as
// t-<unix> instead of the session UUID, the end hook must know which row
// to close). Same contract as TaskNew — taskID "" auto-generates; an
// explicit id is used verbatim. Returns the ledger task_id.
func TaskNewReturningID(summary, taskID, sessionID, model string) (string, error) {
	return TaskNewOID(TaskNewOptions{
		Summary:   summary,
		TaskID:    taskID,
		SessionID: sessionID,
		Model:     model,
	})
}

// TaskNewOID is the extended TaskNewReturningID (task-create-unified):
// identical contract — returns the ledger task_id — but the caller stamps
// the FULL create surface (source/payload/tags/deliverable/reason/priority/
// parent_task_id) through TaskNewOptions so a bare-prompt/raw/resolve row
// is born with every schema column it needs instead of a sparse
// 7-column INSERT that a later UPDATE must widen. TaskNewReturningID is a
// pure compatibility alias onto it.
func TaskNewOID(opts TaskNewOptions) (string, error) {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	opts.Status = taskStatusInProgress
	id, err := insertTaskRow(ctx, db, opts)
	if err != nil {
		return "", err
	}
	return id, nil
}

// taskRunningAnchor picks the wall-clock the running suffix anchors on — the
// REAL execution start, NEVER updated_at. updated_at is a write-churn column
// (status transitions, tags/verify/model backfills, sync merges, and the
// stale-sweep re-arm bump all touch it), so a zombie row's updated_at resets
// every stale window and would make the running clock lie ("restarting
// forever" on a long-dead row). Anchor order (first present wins):
//
//	claimed_at          — the claim moment (M4.5 double-lock): authoritative
//	tasklist_issued_at  — the harness's [TASK_LIST] bookkeeping of the run
//	created_at          — row creation (auto-registration == execution start)
//
// All three are monotonic across the stale re-arm bump, so a dead row shows
// its true age ("running 4h13m since 08:02") instead of resetting every
// ~10-minute stale window.
func taskRunningAnchor(t *Task) float64 {
	if t.HasClaim && t.ClaimedAt > 0 {
		return t.ClaimedAt
	}
	if t.HasTasklistIssue && t.TasklistIssuedAt > 0 {
		return t.TasklistIssuedAt
	}
	return t.CreatedAt
}

// taskRunningSuffix renders the in_progress-only row tail: the execution-start
// wall-clock plus the running duration — symmetric with a terminal row's
// ended_at/elapsed story. Field is inapplicable to pending (no claim) and
// terminal rows (elapsed already known), so it renders ONLY on in_progress.
func taskRunningSuffix(t *Task, now time.Time) string {
	if t.Status != taskStatusInProgress {
		return ""
	}
	return taskStateSuffix(t, now)
}

// taskStateSuffix renders the in_progress row tail as an HONEST state label
// plus the elapsed story: "[<state> for <dur> since <HH:MM:SS>]", where
// <state> is one of running / finished-but-unrecorded / died / unknown
// (<reason>) — see taskstate.go for the evidence ladder. The pre-existing
// label is running, so a demonstrably live row renders exactly as before
// this change ("[running 4h13m since 08:02:15]").
//
// The DB is NOT touched: the label is a display-layer interpretation of
// signals the ledger already carries. Liveness/freshness are probed HERE
// (the caller passes the raw row), and the session layers run inside
// taskProbeRowState — only for in_progress rows, keeping list cost O(rows).
// taskHeartbeatStaleSuffix is the list-side marker appended to a RUNNING
// row's suffix once its heartbeat breaches the stall grace. The word "stale"
// is the same verdict `task show` renders (taskTickTockStale) — one
// vocabulary for one fact; the leading " + " keeps it additive to the
// pre-existing "[running <dur> since <clock>]" wording.
const taskHeartbeatStaleSuffix = " + heartbeat " + taskTickTockStale + " "

func taskStateSuffix(t *Task, now time.Time) string {
	if t.Status != taskStatusInProgress {
		return ""
	}
	anchor := taskRunningAnchor(t)
	clock := time.Unix(int64(anchor), 0).Format("15:04:05")
	elapsed := now.Sub(time.Unix(int64(anchor), 0))
	if elapsed < 0 {
		elapsed = 0 // clock skew guard: never render a negative duration
	}
	state := taskInProgressStateNow(t, now)
	// THE MEASURED HOLDER-GONE FACT rides the label, whichever branch renders it
	// (taskholder_gone.go carries the why and the zero-false-positive contract):
	// the state ladder withholds `died` when the run left recoverable working
	// state on disk, and that withholding must not also hide that nothing names
	// a live process any more (measured 2026-11-05: a row read "unknown: worktree
	// still present" for 8h38m after its executor died).
	holderGone := taskHolderGoneMarker(t, state)
	// The elapsed story belongs to elapsed states (running / died); the
	// session-history states (finished / unknown) describe what is KNOWN, not
	// how long the row has been stuck — their age is framed separately.
	if taskStateCarriesElapsed(state) {
		return fmt.Sprintf("  [%s %s since %s%s%s]", state, taskFormatDuration(elapsed), clock,
			taskHeartbeatStaleMarker(t, state, now), holderGone)
	}
	return fmt.Sprintf("  [%s, age %s%s]", state, taskFormatDuration(elapsed), holderGone)
}

// taskHeartbeatStaleMarker appends "+ heartbeat stale <age>" to a RUNNING
// row's suffix when the heartbeat has breached the stall grace. The elapsed
// window next to it is anchored on the CLAIM (when the run started), so without
// this marker a row whose tool batches stopped 40 minutes ago still reads
// "running 1h30m since …" — the frozen-comment trap this epic reports. The
// marker names the orthogonal fact (the process is alive, its tick-tock is
// not) so the operator sees the difference between a working run and a wedged
// one on the LIST surface, without a second `task show`. Empty for the died
// state (its own label already says the process is gone) and for fresh or
// unmeasurable heartbeats (unknown never renders as stale — Rule 15).
func taskHeartbeatStaleMarker(t *Task, state string, now time.Time) string {
	if state != taskStateRunning {
		return ""
	}
	age, stale, known := taskHeartbeatReading(t, now)
	if !known || !stale {
		return ""
	}
	return taskHeartbeatStaleSuffix + taskFormatDuration(age)
}

// taskHeartbeatReading is the ONE place the display computes the heartbeat
// age and its stall verdict: presence, the clock-skew clamp and the comparison
// against the sweep's own grace (taskStallGraceSec). Show and list both read
// it, so the two surfaces can never disagree about whether a row's tick-tock is
// stale. `known` false = no threshold or NO RECORDED HEARTBEAT: the caller then
// renders neither fresh nor stale.
//
// It reads taskRecordedLivenessAt, NOT taskLastLivenessAt: the fallback that
// suits the zombie sweep ("how old is this row") is a FALSE HEARTBEAT on the
// display ("how stale is this run's tick-tock"), and taking it produced a
// measured false verdict — a row claimed 3 seconds ago rendered
// "+ heartbeat stale 3h56m" because its created_at was 3h56m old. A row that
// has recorded no liveness fact has no heartbeat to be stale, so `known` is
// false and nothing is rendered (Rule 15); the row still shows its own age via
// taskStateSuffix's elapsed story, which is anchored on the claim, not on
// created_at.
func taskHeartbeatReading(t *Task, now time.Time) (age time.Duration, stale bool, known bool) {
	graceSec, thresholdKnown := taskStallGraceSec()
	hb := taskRecordedLivenessAt(t)
	if !thresholdKnown || hb <= 0 {
		return 0, false, false
	}
	age = now.Sub(time.Unix(int64(hb), 0))
	if age < taskTickTockAgeNegative {
		age = taskTickTockAgeNegative // clock skew: never a negative age
	}
	return age, age > time.Duration(graceSec)*time.Second, true
}

// taskStateCarriesElapsed reports whether a state label renders with the
// "for <dur> since <HH:MM:SS>" elapsed story (the running story, preserved
// verbatim for the states that are about a PROCESS) rather than the
// row-age framing used by the session-history states.
func taskStateCarriesElapsed(state string) bool {
	return state == taskStateRunning || state == taskStateDied
}

// taskInProgressStateNow probes the per-row signals and maps them to the
// display state: the claim-stamp / pid layers run here (cheap: one signal-0
// probe), the heartbeat age comes from the ledger, the session + worktree
// layers from taskProbeRowState.
func taskInProgressStateNow(t *Task, now time.Time) string {
	freshness := claimStampFreshness(t)
	alive, checked := false, false
	if host, pid, known := claimHolder(t.ClaimedBy); known && freshness != claimStampStale {
		// The stamp names an executor pid ON THIS HOST: liveness is
		// authoritative — but only through taskLiveHolderProbe, which also
		// refuses a RECYCLED pid (taskAliveProcessNamePrefix). When the row
		// carries a recorded process identity for that same pid, the
		// three-factor verdict is used instead: it is strictly stronger
		// (a reused pid with a matching name no longer reads as alive).
		alive, checked = taskLiveHolderProbe(pid), true
		if t.HasProcIdentity && t.ProcPid == pid {
			alive = taskRowProcessLiveness(t).State == TaskProcessStateRunning
		}
		_ = host
	} else if t.HasProcIdentity && t.ProcPid > 0 {
		// THE IDENTITY LAYER, when the claim STAMP cannot vote (2026-11-05).
		// A row that reached in_progress without a `claimed_by` stamp (a
		// status write, a legacy row, a claim whose stamp was cleared) still
		// records WHO is running it — `proc_pid` + `proc_start_ticks` +
		// `proc_cmd` are written at the same moment the claim is. Without this
		// branch the list reported such a row as unknown-by-absence ("no claim
		// stamp") while `task show` resolved the SAME row's holder to "gone"
		// from the SAME columns: two faces of one ledger disagreeing about one
		// row, which is the defect this epic exists to remove (Rule 14's
		// single-source duty applied to the read-out).
		//
		// The ladder itself decides what a measurement is: a running pid with
		// a matching start tick votes alive, a gone/recycled one votes dead,
		// and a FOREIGN host or an unreadable /proc votes NOT MEASURED — in
		// which case `checked` stays false and the row keeps its unknown
		// label rather than being called dead on evidence from another
		// machine (Rule 15).
		switch taskRowProcessLiveness(t).State {
		case TaskProcessStateRunning:
			alive, checked = true, true
		case TaskProcessStateDead, TaskProcessStateRecycled:
			alive, checked = false, true
		}
	}
	st := taskProbeRowState(t, now)
	// The stall verdict comes from the SAME reading the display renders
	// (taskHeartbeatReading): one computation, so the list's state ladder and
	// the "+ heartbeat stale" marker can never disagree about one row.
	_, heartbeatStale, thresholdKnown := taskHeartbeatReading(t, now)
	return taskInProgressState(t, alive, checked, freshness, heartbeatStale, thresholdKnown, st, now)
}

// taskStallGraceSec resolves the heartbeat-stall grace through the config
// package's single resolver (Rule 2: CLI > env > settings > default), the
// same source the stale detector uses — the display must judge "is the
// heartbeat stale" by the SAME threshold the detector reports by, or the
// list would contradict the notify stream. A resolution error (malformed
// settings) returns thresholdKnown=false: the caller then renders unknown
// rather than asserting staleness on a threshold it could not read.
func taskStallGraceSec() (float64, bool) {
	sec, err := config.ResolveTaskStallGrace(0)
	if err != nil || sec <= 0 {
		// A disabled (0) stall grace means the detector itself never
		// reports staleness — the display has no threshold to apply.
		return 0, false
	}
	return float64(sec), true
}

// taskLiveHolderProbe reports whether the claim stamp's pid is a LIVE
// executor of this row: signal-0 liveness (minus the unreaped-zombie state)
// AND the pid-reuse guard (the process's comm name must be ours). It is a
// package-level seam so the display layer's tests can drive the LIVE branch
// deterministically — a `go test` process is not named `gopi`, so a unit
// test could otherwise never construct an honest "running" row. Production
// binds the real probe; nothing else may rebind it.
var taskLiveHolderProbe = func(pid int) bool {
	return pidAlive(pid) && taskPidIsRunner(pid)
}

// taskPidIsRunner reports whether the process holding pid is a gopi process:
// /proc/<pid>/comm's name starts with taskAliveProcessNamePrefix. This is the
// PID-REUSE guard — a live pid alone says "somebody owns this number", not
// "our runner is alive". Best-effort by contract: an unreadable comm file
// (race with process exit, procfs absent) returns FALSE, which can only
// downgrade a stale-pid "running" into the remaining evidence layers, never
// manufacture a live runner.
func taskPidIsRunner(pid int) bool {
	b, err := os.ReadFile(filepath.Join(procRootName, strconv.Itoa(pid), procCommName))
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(b)), taskAliveProcessNamePrefix)
}

// taskHashDisplay renders the "#" column's value for a task row: the task's
// FULL id, always — for a legacy `t-<unix seconds>` id, a custom id and a uuid
// auto id alike.
//
// It used to print a TRUNCATED identity: the digits of a legacy id, or the
// first 8 characters of a uuid id. That made the "#" column LOOK like an
// identifier while being an ambiguous prefix — on this host `01a0b51d` names 35
// rows and `01a0c039` names 2, and the flake registry's owner column was being
// resolved through such a chunk. The column is now the id, verbatim, so the
// token an operator copies out of `gopi task list` is the token every
// resolution path accepts (epic full-id-no-prefix, 2026-09-23).
//
// The "#" itself is KEPT: it is the row marker every render-side consumer keys
// on (FilterArchivedRows, countRenderedTaskListRows, the ledger owner parser)
// and it sets the identity apart from the rest of the row without introducing a
// second, truncated identity.
func taskHashDisplay(taskID string) string {
	return taskID
}

// taskFormatDuration renders a duration compactly (2h13m / 45m12s / 38s),
// matching Go's natural Duration.String for the magnitudes a ledger row hits.
func taskFormatDuration(d time.Duration) string {
	switch {
	case d >= time.Minute:
		return d.Truncate(time.Second).String() // 2h13m5s
	case d >= time.Second:
		return d.Truncate(time.Millisecond * taskDurationTruncMilli).String() // 38.2s
	default:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
}

// TaskList lists ledger records, newest first (created_at DESC, id DESC
// tiebreak) — for EVERY status filter, so the order never depends on --status;
// the filtered views print that order as their own header note
// (TaskListOrderNote) beside the claim order a dispatcher uses.
// statusFilter "" lists all; a non-empty filter must be a valid
// status (rejected otherwise — no silent empty results for typos). The
// status predicate walks idx_tasks_status. Returns a rendered table.
// in_progress rows append a running suffix "[running <dur> since <HH:MM:SS>]"
// anchored at the real execution start (taskRunningAnchor: claimed_at →
// tasklist_issued_at → created_at — never updated_at); pending/terminal rows
// carry no suffix.
func TaskList(statusFilter string) (string, error) {
	statusFilter = strings.TrimSpace(statusFilter)
	if statusFilter != "" && !validTaskStatus(statusFilter) {
		// A FILTER (read-only) accepts the retired legacy token too — a
		// legacy row must stay queryable. Only the write path's remedy list
		// is narrowed to what `--status` can actually write.
		return "", fmt.Errorf("%s", invalidStatusRefusal(statusFilter, taskStatusListForCLI))
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()

	query := `SELECT ` + taskSelectColumns + ` FROM tasks`
	args := []any{}
	if statusFilter != "" {
		query += ` WHERE status = ?`
		args = append(args, statusFilter)
	}
	// ORDER: EVERY view is newest-first (taskListOrderNewestFirst) — the
	// chronological ledger convention, and the order the help text
	// advertises for the verb. The PENDING view used to be the exception
	// (rendered in taskClaimOrderBy, priority-first), which made the same
	// invocation contradict its own documentation: `gopi task list -h`
	// promises "newest first" while `--status pending` printed priority-first
	// with nothing said (defect measured 2026-09-26; the epic
	// task-list-order-surface). The two orders are now DIFFERENT and BOTH
	// NAMED on the surface: this render is created_at DESC, the claim order
	// is taskListOrderNote's text, which each filtered view prints as its
	// header note and cmd/agent's family help carries as prose. The pending
	// view keeps its queue MARKERS ([next] on the row the claim funnel takes
	// next, taskQueuePositionSuffix) — the marker, not the row ORDER, is what
	// tells an operator which row runs next.
	query += taskListOrderNewestFirst

	// Dependency gate's parent-status index, read BEFORE the row cursor opens.
	// The ledger openers pin a SINGLE connection (internal/tools/memory.go
	// SetMaxOpenConns(1) — SQLite serializes writers), so a parent-status
	// query issued while the row cursor is open would block on the connection
	// that cursor holds: a deadlock, not a slow path. One query per render,
	// ahead of the walk, is the only correct shape here. Gated on the queue
	// actually carrying a parented row so a parentless pending group (the
	// common case) never pays for the whole-table map.
	var parentStatus map[string]string
	// nextClaimID is the row the CLAIM FUNNEL takes next (taskNextClaimID):
	// non-empty only for the pending view, whose rows mark it [next]. It is
	// resolved by the funnel's OWN order (taskClaimOrderBy), never by a
	// position in this render — with the rows newest-first, "the top row" is
	// a different row from the one that gets claimed (Rule 33: [next] must
	// name the row the claim funnel actually takes).
	nextClaimID := ""
	if statusFilter == taskStatusPending {
		indexNeeded, perr := taskDependencyIndexNeeded(ctx, db)
		if perr != nil {
			return "", fmt.Errorf("task: list: parent index: %w", perr)
		}
		if indexNeeded {
			ps, perr := taskParentStatuses(ctx, db)
			if perr != nil {
				return "", fmt.Errorf("task: list: parent status: %w", perr)
			}
			parentStatus = ps
		}
		nextClaimID, perr = taskNextClaimID(ctx, db)
		if perr != nil {
			return "", fmt.Errorf("task: list: %w", perr)
		}
	}

	// The failed-needs-reissue line, read BEFORE the row cursor opens — the
	// same ordering constraint the parent-status index above obeys: the
	// ledger openers pin a SINGLE connection (internal/tools/memory.go
	// SetMaxOpenConns(1)), so a second query issued while the row cursor is
	// open would block on the connection.
	//
	// It rides the RAW render (not the TaskListVisible funnel) because it is a
	// property of the LEDGER, not of a row: every view — filtered, unfiltered,
	// conversational — must show a failure nobody is going to look at again.
	// An EMPTY ledger keeps the zero contract ("" note), so "No tasks found."
	// stays byte-identical to a ledger that never had a failed row.
	reissueNote, rerr := failedReissueNote(ctx, db)
	if rerr != nil {
		return "", fmt.Errorf("task: list: %w", rerr)
	}

	// The close-out refusals, read BEFORE the row cursor opens — the same
	// single-connection constraint the two indexes above obey. A row whose
	// trail ENDS at a refused close-out wears a marker on its own line and the
	// render carries one count line, so an operator learns from ONE invocation
	// what used to take one `task show` per row (task_list_closeout_signal.go
	// carries the incident and the boundary: a reader and a marker, no gate
	// changed).
	//
	// SCOPED TO THE VIEW'S OWN POPULATION (epic 01a12bc0). The read is narrowed
	// by the SAME status predicate the row walk below applies, so the map — and
	// therefore the header count and the marked rows — come from one
	// population. Unfiltered, there is no predicate to share and the read
	// stays ledger-global (every row is about to be walked anyway).
	closeoutSignals, cerr := taskListCloseoutSignals(ctx, db, statusFilter)
	if cerr != nil {
		return "", cerr
	}
	// The header's SCOPE PHRASE is chosen once, with the scope itself (Rule 1b):
	// a filtered render counted its own view, an unfiltered one counted the
	// ledger. The count is resolved AFTER the walk (see the header block below)
	// so it is the number of markers this render actually printed.
	closeoutScope := taskListCloseoutScopeLedger
	if statusFilter != "" {
		closeoutScope = taskListCloseoutScopeView
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("task: list: %w", err)
	}
	defer rows.Close()

	var out strings.Builder
	count := 0
	// markedCloseout: how many rows this render actually PRINTED the marker on.
	// The header's number is this one, never len(closeoutSignals) — the map is
	// the query's answer and this is what reached the screen, and epic
	// 01a12bc0 is precisely the defect of rendering the former above the
	// latter. With the read scoped to the same population they are equal by
	// construction, which is the property the tests pin.
	markedCloseout := 0
	// pendingView: the STATUS-FILTERED pending list carries the QUEUE MARKERS
	// ([next] / [queue-skip]) — the claim funnel's own verdict, per row. The
	// rows themselves are in the SAME newest-first order as every other view
	// (see the ORDER BY above), which is exactly why the marker — not the row
	// position — is what names the row claimed next.
	pendingView := statusFilter == taskStatusPending
	now := time.Now() // one clock read per list render — durations stay comparable across rows
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return "", fmt.Errorf("task: list: %w", err)
		}
		// list renders the PERSISTED model only (DB truth, no per-row JSONL
		// reads — a list must stay O(rows), not O(rows × session files)).
		// Pre-persistence rows (model='') render (unknown) here; `task show`
		// still resolves them via the legacy session-JSONL fallback.
		model := t.Model
		if model == "" {
			model = taskModelUnknown
		}
		// task-submit-model epic ②: a row whose model column was left empty
		// at submit gets backfilled at WRITEBACK time (the executing
		// session's model, stamped from the child session's session_start).
		// The backfill happened on a later write to the row, so the value
		// just scanned may be stale in a long-lived listing process — but
		// the CLI is one-shot, and a stale "(unknown)" self-heals on the
		// next list. No extra read needed here.
		// resumed marker (unchanged) — see above.
		resumed := taskResumedTagCount(t.Tags)
		// task-single-row-exec: the [dup-of <wrapper>] marker is GONE — auto
		// mirror rows are no longer minted for task-run children (the human
		// wrapper row is the single execution row), so there is no duplicate
		// to disambiguate. A legacy pre-fix row carrying wrapper:<id> simply
		// renders as itself (historical audit stays intact).
		// running suffix: in_progress rows show [running <dur> since <time>]
		// anchored at the real execution start (taskRunningAnchor — claimed_at
		// → tasklist_issued_at → created_at). Never updated_at: the stale
		// sweep's re-arm bump rewrites updated_at every ~10 min on zombie rows,
		// which would reset the display and make a dead task look restarted.
		// THE LABEL IS HONEST (task-list-state-honesty): running / died /
		// finished-but-unrecorded / unknown (<reason>) — see taskStateSuffix.
		// # column (cross-node reconciliation): renders the task_id's numeric
		// part, NOT the local SQLite rowid. rowid is allocated per-node at
		// INSERT, so sync'd replicas of the same task show different #s
		// (peer1 #180 vs HP #220); the t-<unix> task_id is the shared primary
		// key, so its digits are identical on every node.
		// via suffix (run provenance): a compact "how was this run launched"
		// marker — [via auto] (a self-registering bare run), [via manual],
		// [via worker], [via dispatch], [via resume], [via merger]. Follows
		// the [running …]/[resumed×N] suffix pattern; the value is whatever
		// runcmd.Method* the stamp wrote, and the full command line belongs
		// in `task show` (run_cmd), not here.
		via := ""
		if t.RunMethod != "" {
			via = fmt.Sprintf(" [via %s]", t.RunMethod)
		}
		// render-time TEXT BOUNDARY (epic binary-summary-guard): rows written
		// BEFORE the write-path guard — or synced in from a node running an
		// older binary — can still hold control bytes. Sanitising here is what
		// makes `gopi task list > file` produce a TEXT file (and `read`/grep/
		// diff work on it) regardless of what the column holds; it is identity
		// for a row a current binary wrote.
		// queue-position suffix (pending view only): "[next]" on the row the
		// claim funnel would pick next, "[queue-skip]" on a row no dispatcher
		// can claim (merger-owned / payload-less ledger row). Without it a
		// naive FIFO reading of the list invites exactly the operator report
		// this view answers (2026-09-15) — the row at the top would look
		// starved while in truth it is unclaimable.
		queue := ""
		if pendingView {
			queue = taskQueuePositionSuffix(t, parentStatus, nextClaimID)
		}
		// ground tail (live rows only): branch + worktree, read off the row's
		// OWN deliverable blob — the derived-name rule is only useful if the
		// reader can SEE the name, and until this tail existed that took one
		// `task show` per row (tasklist_ground.go owns the shape and the why).
		// Last on the line by design: the where-clause closes the row, after
		// the state and provenance markers.
		ground := taskListGroundSuffix(t)
		// close-out tail: the row's trail ENDS at a refused close-out, so the
		// work was judged done and the machine then declined to collect it —
		// the branch may still be local, the worktree may still be there, and
		// nothing re-runs the delivery (Rule 6d). Read off the map resolved
		// before the cursor opened; the reason rides the marker so the row
		// stays one line.
		closeout := ""
		if reason, refused := closeoutSignals[t.TaskID]; refused {
			closeout = taskListCloseoutSignalSuffix(reason)
			markedCloseout++
		}
		// delivery-owed tail: the row's ground holds commits that NO remote ref
		// reaches — the delivery is outstanding, and until this tail existed no
		// listing said so (epic 01a1a8df; measured victims 01a1a87b, 01a1a876).
		// It renders LAST because it is the most actionable fact on the line: a
		// reader scanning a failed census sees the debt before the metadata.
		// Read-only, terminal rows only, and "" whenever nothing is owed, so an
		// ordinary listing is byte-identical to the render that never asked
		// (task_delivery_owed_reading.go owns the probe and the cost).
		owedTail := taskDeliveryOwedListSuffix(t)
		// ONE identity per row: the full task id, marked by "#". It used to be
		// a truncated chunk plus the full id beside it, which made the row
		// carry an ambiguous token that LOOKED like the row's address (epic
		// full-id-no-prefix, 2026-09-23).
		fmt.Fprintf(&out, taskListRowFormat,
			taskHashDisplay(t.TaskID), t.Status, sanitizeTaskSummary(t.Summary), model, t.Source, formatTaskTime(t.CreatedAt),
			taskRunningSuffix(t, now),
			map[bool]string{true: taskResolvedRowMarker, false: ""}[isTaskResolved(t.Tags)],
			map[bool]string{true: fmt.Sprintf(" [resumed×%d]", resumed), false: ""}[resumed > 0],
			via,
			queue,
			ground,
			closeout,
			owedTail)
		count++
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("task: list: %w", err)
	}
	if count == 0 {
		return taskListEmptyResult + "\n" + reissueNote, nil
	}
	prefix := fmt.Sprintf(taskListCountSuffix, count)
	if statusFilter != "" {
		prefix = fmt.Sprintf(taskListStatusCountSuffix, count, statusFilter)
	}
	// The order the rows are ABOUT to be read in, stated on the rendered
	// surface (TaskListOrderNote) — the rendered half of the fix for the epic
	// task-list-order-surface: the verb's help documents this order, and the
	// verb's own output says so too, so a reader never has to infer it. Only a
	// FILTERED view carries it: the unfiltered verb already prints exactly
	// what its help promises, and a note on every line of output would be
	// noise.
	if statusFilter != "" {
		prefix += TaskListOrderNote
	}
	// The close-out note rides the header, ahead of the rows (the shape the
	// failed-needs-reissue alarm takes, one paragraph below in this file): the
	// COUNT is what was invisible for nine hours, and the rows carry the
	// per-row marker and reason. It is "" when no row is stranded, so the
	// clean case is byte-identical to the render that never asked.
	//
	// COUNTED FROM WHAT WAS PRINTED, scoped to what was walked (epic
	// 01a12bc0). markedCloseout increments in the row loop above, so the number
	// in the sentence is the number of marker lines on screen under it — the
	// arithmetic a reader can redo by eye — and closeoutScope names that
	// population in words.
	prefix += taskListCloseoutNote(markedCloseout, closeoutScope)
	return prefix + out.String() + reissueNote, nil
}

// failedReissueNote is the list render's READ of the failed-needs-reissue
// mechanism (internal/tools/taskreissue.go — the ONE predicate): it returns
// the rendered count line, or "" when every failed row has been reissued or
// tagged `resolved`. It NEVER fails the render for a missing ledger — TaskList
// answers "No tasks found." over a path that is not a database at all (the
// documented empty-ledger behaviour the CLI relies on), and a read that
// refuses to render because a SIDE note could not be computed would break
// `task list` on every fresh profile.
//
// The specific tolerated failure is the ABSENT LEDGER, and it is tolerated
// because its meaning is unambiguous here: no rows exist, so there is nothing
// to reissue and "" is not a guess. Every OTHER error is returned, wrapped
// (Rule 16), and reaches the operator (Rule 15) — a note that could not be
// computed is never silently an empty one.
func failedReissueNote(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := FailedNeedsReissue(ctx, db)
	if err != nil {
		if isMissingTableErr(err) {
			return "", nil // an empty/unmigrated ledger has no failed rows to report
		}
		return "", err
	}
	return FailedReissueNote(rows), nil
}

// taskMissingTableMarker is the sqlite error fragment that names an ABSENT
// table ("no such table: tasks") — the shape a ledger that has not been
// through ensureTasksSchema answers with, and the ONE test for it (the same
// fragment cmd/agent's chat prefetch keys on; Rule 14 keeps a shared value in
// one place, and this one is package-local because it is read only here).
const taskMissingTableMarker = "no such table"

// isMissingTableErr reports whether err is that absent-table shape.
func isMissingTableErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), taskMissingTableMarker)
}

// taskListOrderNewestFirst is THE order the ledger view renders in —
// newest-first (created_at DESC), tie-broken by rowid (id DESC) so two rows
// sharing a second still render deterministically.
const taskListOrderNewestFirst = ` ORDER BY created_at DESC, id DESC`

// The two order TOKENS a surface may name (Rule 14: one spelling each — the
// rendered note and the help text reference these consts instead of retyping
// "newest first"/"priority-first" beside each other, which is how the two
// channels drifted apart in the first place).
const (
	// TaskListOrderNewestFirstPhrase names the rendered ledger order.
	TaskListOrderNewestFirstPhrase = "newest first"
	// TaskListOrderClaimPhrase names the DISPATCHER's queue order — a
	// different order, deliberately (M4.11 task-priority: bugs before
	// features before docs), and the one a reader must not confuse with the
	// render order.
	TaskListOrderClaimPhrase = "priority-first"
)

// TaskListOrderNote is the rendered ledger's ORDER LINE: the sentence printed
// under a filtered view's count header, spelling the order the rows below it
// are in. Both order phrases are spelled once, in the consts above.
//
// It exists because the rendered order and the claim order are NOT the same
// thing, and the surface used to say neither: the help promised "newest first"
// while `--status pending` silently printed the claim order (defect measured
// 2026-09-26). An earlier operator report (2026-09-15) called the reverse
// mismatch — a newest-first render over a priority-first dispatcher — a
// starvation bug. Naming BOTH orders, on the rendered surface and in the help,
// is what makes the two stories distinguishable instead of contradictory; the
// queue marker on each pending row (taskQueuePositionSuffix) then points at
// the row a dispatcher takes next — the note deliberately does NOT spell that
// marker's token, which lives in exactly one place (Rule 13).
//
// It rides the count header rather than each row: one line per render, and
// FilterArchivedRows rewrites only the header's leading integer (the whole
// remainder of the header stays byte-identical), so the note survives the
// archived-row filter untouched.
const TaskListOrderNote = "ordered " + TaskListOrderNewestFirstPhrase +
	" (created_at DESC); pending rows are CLAIMED " + TaskListOrderClaimPhrase +
	", and each pending row is marked with its queue position\n"

// taskListCountSuffix / taskListStatusCountSuffix are the ONE place the
// rendered ledger's count header is spelled: "<n> task(s):" without a status
// filter, "<n> task(s) with status=<s>:" with one. The trailing colon and
// the leading integer are a wire contract — FilterArchivedRows re-derives the
// count from the surviving rows by rewriting the header's leading digits, and
// cmd/agent's dispatch load probe parses the same leading integer.
const (
	taskListCountSuffix       = "%d task(s):\n"
	taskListStatusCountSuffix = "%d task(s) with status=%s:\n"
)

// taskListEmptyResult is the rendered ledger's zero-row text (no status
// filter or an empty post-archive view) — the standard "nothing here" line.
const taskListEmptyResult = "No tasks found."

// TaskListAllFlag is the ONE spelling of the flag that brings archived
// (resolved) tombstones back into a list view: `gopi task list --all` and the
// conversational `/task list --all`. It is named here (Rule 1b/13) because the
// DISCLOSURE LINE below tells the reader which command reveals the rows the
// filter hid — an instruction that must name the real flag, not a re-typed
// string that can drift from the parser's whitelist (cmd/agent's
// taskFlagMember, the slash parser's slashTaskAllFlag).
const TaskListAllFlag = "--all"

// TaskListArchiveHiddenPrefix is the rendered marker that opens the archived-
// row DISCLOSURE line — the "[hidden] " token a reader (and a test) greps for.
// It is deliberately a bracketed word rather than bare digits: the line rides
// under a count header, and a line leading with a number would be readable as
// a second header by anything that parses the render (taskListCountPrefix).
const TaskListArchiveHiddenPrefix = "[hidden] "

// taskListArchiveHiddenFormat spells the disclosure line's body ONCE
// (Rule 1/13: the count and the named values are the only variables).
//
// THE WORDING STATES THE CRITERION THE CODE ACTUALLY APPLIES. The filter is
// isTaskResolved, which compares a whole comma-separated tag token with ==
// (`resolved`), NOT a substring match. The line used to say "tags contain
// %q", which reads as LIKE '%resolved%' — a reader recomputing by that rule
// counted 43 rows where the filter hid 9, and every one of the extra 34 was
// tagged `sync-resolved` (a sync echo, NOT an archive mark). The code was
// right and the sentence was wrong, so the sentence says the tag must EQUAL
// the marker and names the near-miss tag that is deliberately NOT hidden.
const taskListArchiveHiddenFormat = "%s%d archived row(s) hidden — a row is hidden only when one of its tags EQUALS %q exactly; a different tag such as %q is NOT hidden. %s shows them"

// TaskListArchiveHiddenNote renders the archived-row disclosure for a view
// that dropped n rows — and "" for n == 0, because a view that hid nothing
// must not claim it did (Rule 15 cuts both ways).
//
// WHY THIS EXISTS (measured 2026-09-29, docs/archive/demo/materials/01-list/
// 05-finding.md F1.2). The archived-row filter dropped rows SILENTLY, so
// `gopi task list --status completed` over an archived ledger printed exactly
// "No tasks found." — byte-identical to the output for a ledger that never had
// a completed row. A READ COMMAND THAT UNDER-REPORTS IS A READ COMMAND THAT
// LIES BY OMISSION: the operator cannot tell "there are none" from "they are
// hidden from you", and the archived rows are precisely the durable交付 record
// an auditor comes back for. The count is disclosed instead of the filter
// being removed: the tombstone hygiene is deliberate (it is what keeps a
// handled failure out of the working view), so the fix is to SAY what the
// filter did, on the surface, in numbers.
//
// It is a FUNCTION, not a const, so the zero case cannot be rendered by
// accident: a caller that appends it unconditionally gets "" — never a line
// announcing that zero rows were hidden.
func TaskListArchiveHiddenNote(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf(taskListArchiveHiddenFormat, TaskListArchiveHiddenPrefix, n,
		taskTagResolved, syncTagSyncResolved, TaskListAllFlag)
}

// TaskListRowPrefix starts every rendered TaskList row ("#<hash>  <id> …");
// the POST-filter row count (the number the header must equal) counts exactly
// the lines carrying it.
const TaskListRowPrefix = "#"

// taskSessionJSONLExt is the constant suffix of the session JSONL files
// (<gopi-dir>/sessions/<session-id>.jsonl — the gopi data root via
// config.GopiDir(): $GOPI_HOME/.gopi when GOPI_HOME is set, else ~/.gopi).
// The VALUE comes from internal/sessionlayout, the shared leaf package
// (Rule 14) — imported rather than mirrored from internal/gopilog, which
// would create an import cycle (gopilog → internal/agent → internal/tools).
// See taskModelFromSession.
const taskSessionJSONLExt = sessionlayout.JSONLSuffix

// taskBareSessionRecordName is the file name of the bare-session layout's
// record (sessions/<session-id>/session.jsonl) — the value of
// internal/sessionlayout.RecordName, imported so the guess taskSessionLogPath
// falls back on can never disagree with the classifier that decides which file
// IS the record.
const taskBareSessionRecordName = sessionlayout.RecordName

// taskFindSessionJSONL locates the session JSONL for a session id in the gopi
// data root's sessions dir, EXACT ID only, across the session-dir-unify layouts
// (legacy flat <uuid>.jsonl, task-run <task_id>/<uuid>.jsonl and bare
// <uuid>/session.jsonl). Returns "" when no record's session id equals id.
//
// The prefix branch is GONE (epic full-id-no-prefix, 2026-09-23) and it must
// not come back: its first-hit pick meant `gopi task show <row>` could attach
// ANOTHER task's transcript to the row — a truncated id in a listing collided
// with a session id and the scan returned whichever the walk visited first.
// An exact match has no such ordering dependence. Callers hold session ids the
// ledger wrote (session_start's sessionId), which are always complete.
//
// ADDRESSED FIRST, WALK AS FALLBACK (epic session-log-addressed-first,
// 2026-10-07). Two of the three layouts are DERIVED from the id alone — the
// bare session dir `<id>/session.jsonl` and the legacy flat `<id>.jsonl` — so
// they are probed with two stats BEFORE the walk. The walk answers a question
// the call site already knows the answer to whenever the log is in one of
// those two layouts, and the row-keyed sibling (taskFindSessionJSONLForRow)
// cannot help: it is called with the ROW's task id, while this one is called
// by the three `task show` audit lookups (session log / model / briefing) with
// a session id whose directory is named by ANOTHER row — a context-loss
// re-home puts one session's log under a task directory the row does not
// carry (measured 2026-10-07: 84 of the 623 rows carrying a session_id, 13%,
// resolve ONLY by walking).
//
// The order is the deep scan's OWN precedence as far as it can be honoured, so
// which file resolves cannot change: the pre-existing resolution asked for the
// flat record first and the bare dir second (`taskFindSessionJSONLAddressablePath`
// keeps that order), and the walk stays AFTER both, exactly where the flat pass
// already sat. The task-run layout (`<task-id>/<session-id>.jsonl`) is NOT
// addressable from a session id and therefore stays on the walk — that is what
// keeps this a reordering and not a claim that every log is addressable.
func taskFindSessionJSONL(dir, id string) string {
	if p := taskFindSessionJSONLAddressablePath(dir, id); p != "" {
		return p
	}
	// One level down: task-run dirs (<task_id>/<uuid>.jsonl) and bare session
	// dirs (<uuid>/session.jsonl). The candidates come from the SHARED
	// classification (internal/sessionlayout.Records), so a session dir's
	// side files (signal-provenance.jsonl) are never probed for a session id:
	// the id is the session_start event's sessionId, and a provenance record
	// carries no such event — reading it would be a wasted open whose basename
	// a prefix match could still mistake for an id.
	for _, r := range taskSessionTreeRecords(dir) {
		sid := taskSessionStartID(r.Path)
		if sid == "" {
			continue
		}
		if sid == id {
			return r.Path
		}
	}
	return ""
}

// taskFindSessionJSONLForRow is taskFindSessionJSONL with the ROW's own
// identity supplied, so the log is found by LOOKUP instead of by a scan.
//
// WHY THIS EXISTS (measured 2026-09-14): taskFindSessionJSONL's deep pass
// walks every session dir under the root reading each record's first line to
// recover a session id it was already given. That is O(entire sessions tree)
// for a fact that is a plain path: on this host 70,438 entries → ~287k openat
// / ~575k getdents64 for a `task list` showing 6 rows (8.5s wall, ~2s per
// in_progress row), and the cost grows with the operator's history. The row
// ALREADY knows its task id and session id, and the session-dir-unify layouts
// place the log at deterministic paths, so every normal row is resolved with
// TWO stats (plus one directory readdir for the task-run layout).
//
// THE LEDGER'S OWN ADDRESS (added 2026-09-15 — see taskledger.go): the row's
// session_id is the session that SUBMITTED it, and one worker session executes
// MANY rows, so the log's directory is named by a task id the row does NOT
// carry (`sessions/<submitter-task-id>/<session-id>.jsonl`). The row's own
// columns therefore name nothing on disk for exactly the rows that made this
// view slow. The session's row group in the LEDGER names the directory, so the
// hint candidates are probed before any scanning pass — one bounded,
// memoised ledger read turns the whole group's resolution into stats.
//
// The full scan stays as the FALLBACK (behaviour-identical for the layouts
// the fast path cannot name), so no existing resolution can regress.
func taskFindSessionJSONLForRow(dir, taskID, sessionID string) string {
	taskID = taskIDNormalize(taskID)
	if p := taskFindSessionJSONLInTaskDir(dir, taskID, sessionID); p != "" {
		return p
	}
	// Cost order matters: the flat pass readdirs the whole root (70k+ entries
	// here) and the deep pass walks every session dir, so every lookup that
	// can answer is tried before them — the row's own columns, then the
	// ledger-named directories of the session's own row group (taskledger.go),
	// each probe bounded to ONE readdir of ONE named directory.
	for _, hinted := range taskSessionDirHints(sessionID) {
		if hinted == strings.TrimSpace(taskID) {
			continue // already probed above — the row's own address
		}
		if p := taskFindSessionJSONLInTaskDir(dir, hinted, sessionID); p != "" {
			return p
		}
	}
	if p := findSessionJSONLFlat(dir, sessionID); p != "" {
		return p
	}
	return taskFindSessionJSONLDeepOnly(dir, sessionID)
}

// taskFindSessionJSONLAddressablePath answers the layouts the SESSION ID ITSELF
// NAMES — the legacy flat `<id>.jsonl` and the bare session dir
// `<id>/session.jsonl` — in the order the pre-existing resolution asked for
// them (flat, then bare), so those two can never swap places. "" means "not at
// an addressable layout" and hands the question to the walk.
//
// It is the ONE place the addressed-vs-walk ORDER for taskFindSessionJSONL is
// decided, so the cheap pass and the walk cannot disagree about which of the
// two addressable layouts wins.
func taskFindSessionJSONLAddressablePath(dir, id string) string {
	if id == "" {
		return ""
	}
	if flat := findSessionJSONLFlat(dir, id); flat != "" {
		return flat
	}
	return sessionBareRecordPath(dir, id)
}

// taskFindSessionJSONLDeepOnly is taskFindSessionJSONL's scanning half —
// the layout-keyed walk over session DIRS — without its flat re-pass (the
// caller has already run it). Nothing new should call this directly; it
// exists so a resolution that is already known to be a lookup can fall back
// exactly once.
//
// It honours the SCAN's precedence where the scan HAS one — a single
// record per layout resolves to that record, and the task-run dir is reached
// when nothing addressable exists — and it is EXACT (epic full-id-no-prefix):
// a truncated id must not be handed the first record that shares its prefix.
// It does NOT own the order of the two ADDRESSABLE layouts (the flat record
// and the bare dir) when both are present: that order belongs to
// taskFindSessionJSONL's call site, which asks for the flat record first
// (`taskFindSessionJSONLAddressablePath`) and puts this walk AFTER both, exactly
// where the flat pass already sat. The walk's own `os.ReadDir` order is
// arbitrary (measured 2026-10-07), so it could never have been the authority
// there.
func taskFindSessionJSONLDeepOnly(dir, id string) string {
	for _, r := range taskSessionTreeRecords(dir) {
		sid := taskSessionStartID(r.Path)
		if sid == "" {
			continue
		}
		if sid == id {
			return r.Path
		}
	}
	return ""
}

// taskFindSessionJSONLInTaskDir answers the deterministic layouts for one
// row: <dir>/<session-id>/session.jsonl (bare session dir) and
// <dir>/<task-id>/… (the task-run layout, whose dir is named by the task id),
// in the SCAN's precedence order (see taskFindSessionJSONLDeepOnly: the bare
// dir sorts before the legacy flat record of the same id, and before the
// task-run dirs whenever the session id sorts before the task id). "" means
// "not at a deterministic location" and hands the question to the costlier
// passes. The flat layout is deliberately NOT answered here: it costs a
// readdir of the whole root, so its turn comes after these two stats (and
// after the bare record, which sorts before it — taskFindSessionJSONLAddressablePath).
func taskFindSessionJSONLInTaskDir(dir, taskID, sessionID string) string {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return ""
	}
	tid := taskIDNormalize(taskID)
	if tid == "" {
		// No task dir to consider: the bare layout is the only address.
		return sessionBareRecordPath(dir, id)
	}
	// The scan visits the sessions root in readdir (BYTE-SORTED) order and
	// returns the first record whose id matches, so the file the row's own
	// dirs yield is decided by which DIR NAME sorts first — the bare session
	// dir <session-id> or the task-run dir <task-id>. Probing in that order is
	// what makes this resolution behaviour-identical rather than merely
	// cost-identical.
	if id < tid {
		// The bare session dir sorts BEFORE the task-run dir: the scan would
		// reach it first.
		if bare := sessionBareRecordPath(dir, id); bare != "" {
			return bare
		}
		return taskRunRecordPath(dir, tid, id)
	}
	// The task-run dir sorts first (or ties — an id equal to its task id).
	if run := taskRunRecordPath(dir, tid, id); run != "" {
		return run
	}
	return sessionBareRecordPath(dir, id)
}

// sessionBareRecordPath returns the bare session dir's record when it exists
// as a regular file, else "". It is the ONE-probe form of the bare layout
// (sessions/<session-id>/session.jsonl), shared so the addressed path and the
// scan's own precedence can never disagree about which file that layout names.
func sessionBareRecordPath(dir, id string) string {
	bare := filepath.Join(dir, id, taskBareSessionRecordName)
	if fi, err := os.Stat(bare); err == nil && !fi.IsDir() {
		return bare
	}
	return ""
}

// taskRunRecordPath returns the record held by ONE task-run dir
// (sessions/<task-id>/…) for the row's session: the run file named by the
// session id, else the single record the dir holds. "" when the dir carries no
// record for this session — the row's dirs are then ruled out as addresses.
func taskRunRecordPath(dir, taskID, sessionID string) string {
	taskDir := filepath.Join(dir, taskID)
	if fi, err := os.Stat(taskDir); err != nil || !fi.IsDir() {
		return ""
	}
	// The task-run dir holds the run's own log plus side files; pick the one
	// whose basename IS the row's session, else the single record it holds.
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), taskSessionJSONLExt) {
			continue
		}
		if strings.TrimSuffix(e.Name(), taskSessionJSONLExt) == sessionID {
			return filepath.Join(taskDir, e.Name())
		}
	}
	if r, ok := sessionlayout.RecordInDir(taskDir); ok {
		return r.Path
	}
	return ""
}

// taskSessionTreeWalk is the sessions-tree WALK seam — the raw cost of the
// resolution path: the layout-keyed scan over session DIRS under the sessions
// root, including the first-line read of every session-dir record. It is the
// ONLY whole-tree step this path can reach, so a COUNTER on it is the cost
// property itself ("resolving a row-addressed session log walks the tree ZERO
// times") rather than the timing of a machine. The memo sits ABOVE this seam
// (taskSessionTreeRecords), so a counting binding still observes the real
// number of walks. Nothing else may rebind it.
var taskSessionTreeWalk = walkSessionTreeRecords

// walkSessionTreeRecords is taskSessionTreeWalk's production binding: the
// records held BY SESSION DIRS under the sessions root. The flat layout's own
// records (bare *.jsonl at the ROOT) are left to findSessionJSONLFlat, which
// runs first and must keep its exact-then-prefix precedence; the candidates
// here come from the SHARED classification (internal/sessionlayout.Records),
// so a session dir's side files (signal-provenance.jsonl) are never probed for
// a session id. An unreadable root returns nil (the caller's scan finds
// nothing rather than failing a render).
func walkSessionTreeRecords(dir string) []sessionlayout.Record {
	records, err := sessionlayout.Records(dir)
	if err != nil {
		return nil
	}
	var out []sessionlayout.Record
	for _, r := range records {
		if r.Dir != dir {
			out = append(out, r)
		}
	}
	return out
}

// taskSessionTreeCache/taskSessionTreeMu memoise the tree WALK per process,
// keyed by the sessions root. WHY: the walk is the only expensive step on the
// resolution path (70,438 session dirs on the operator's host, each record's
// first line read) and ONE `gopi task list` render resolves the session log of
// every in_progress row; without the memo each row whose file is not directly
// addressable re-walked the whole tree (measured 2026-09-14: two such rows cost
// two walks — the per-row shape that made the view O(rows × tree)). A one-shot
// CLI process cannot carry a stale answer into a later command, and the
// addressed probes run FIRST and are never cached, so they stay authoritative.
// taskSessionTreeEntry is ONE cached walk together with the ROOT FINGERPRINT it
// was taken at (see taskSessionRootFingerprint). The pair is the whole point:
// a cached list without a fingerprint is an answer about a tree that may not
// exist any more.
type taskSessionTreeEntry struct {
	recs []sessionlayout.Record
	sig  string
}

var (
	taskSessionTreeMu    sync.Mutex
	taskSessionTreeCache map[string]taskSessionTreeEntry
)

// taskSessionRootFingerprint is the cheap FACT that says whether a cached walk
// still describes the tree: the sessions root's own entry count, the FNV-1a
// hash of those entry names, and the root's mtime. It costs ONE getdents and
// ONE stat — no recursion, no file opens — where the walk it guards recurses
// every session dir.
//
// WHY THE MEMO NEEDED ONE (measured 2026-10-29, the defect this repairs). The
// memo was "once per process, for ever", which is sound for the one-shot CLI it
// was written for and WRONG for a process that outlives one answer — and
// `cmd/agent`'s suite is exactly that process: its cases share one pinned
// profile, so a walk taken while serving an earlier case froze the sessions
// tree, and a later case that seeded `sessions/<id>/` into that same root had
// it resolved against the FROZEN list. Reproduced at the seam, in
// internal/tools (TestSessionTreeMemoDoesNotOutliveTheTreeItDescribes):
// one warm walk, then a session dir seeded afterwards, then a lookup that
// answers "" — while the same lookup with the memo cleared finds the file. The
// production shape of the same bug is a long-lived face (chat / serve / worker)
// resolving a session log it has never seen because the process resolved one
// hours earlier.
//
// WHAT IT COVERS: a session DIRECTORY appearing or vanishing under the root
// (the shape that broke, and the one the sessions tree actually grows by), and
// a dir replaced by another of the same name (the root's mtime moves).
//
// WHAT IT DOES NOT COVER, stated rather than implied: a new RUN LOG written
// inside an ALREADY-EXISTING session dir leaves the root's own entries and
// mtime untouched, so that one shape can still answer from a stale list. It is
// bounded by design — the same-named run files are exactly what the ADDRESSED
// probes answer without the memo at all (taskFindSessionJSONL asks the flat
// record and the id's own bare dir FIRST, and those are never cached), and the
// ROW-addressed resolution (`sessions/<task-id>/…`) goes through
// rundir.RecordsDir, which has no memo. A fingerprint that covered it would
// have to stat every session dir, i.e. cost what the memo exists to avoid.
//
// "" means the root cannot be read: the caller then does NOT serve the cache
// (Rule 15 — an unverifiable answer is not an answer).
func taskSessionRootFingerprint(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	h := fnv.New64a()
	for _, e := range entries {
		_, _ = h.Write([]byte(e.Name())) //stylegate:ignore hash writes cannot fail (hash.Hash.Write is total)
		_, _ = h.Write([]byte{0})        //stylegate:ignore same: the separator byte always writes
	}
	sig := fmt.Sprintf("%d:%x", len(entries), h.Sum64())
	if fi, err := os.Stat(dir); err == nil {
		sig += fmt.Sprintf(":%d", fi.ModTime().UnixNano())
	}
	return sig
}

// taskSessionTreeRecords is the memoised walk the resolution path calls: at
// most ONE walk per root per root STATE. A walk that read nothing is NOT cached
// — the cache covers a SUCCESSFUL walk only, so a transient failure (or a
// counting stand-in in a test) never becomes a permanent empty answer for the
// rest of the process (Rule 15). A hit is served only when the root's
// fingerprint is the one the walk was taken at; otherwise the walk runs again
// and replaces the entry, so the memo can never outlive the tree it describes.
func taskSessionTreeRecords(dir string) []sessionlayout.Record {
	sig := taskSessionRootFingerprint(dir)
	taskSessionTreeMu.Lock()
	defer taskSessionTreeMu.Unlock()
	if sig != "" {
		if e, ok := taskSessionTreeCache[dir]; ok && e.sig == sig {
			return e.recs
		}
	}
	out := taskSessionTreeWalk(dir)
	if out == nil {
		return nil
	}
	if taskSessionTreeCache == nil {
		taskSessionTreeCache = make(map[string]taskSessionTreeEntry)
	}
	taskSessionTreeCache[dir] = taskSessionTreeEntry{recs: out, sig: sig}
	return out
}

// SessionScanCounterForTest rebinds the sessions-tree WALK seam to a COUNTER
// and returns count() (walks so far) plus the restore. It exists so a caller
// OUTSIDE this package — a CLI-level test asserting the end-to-end cost of a
// `task list` render — counts walks at the PRODUCTION seam instead of
// re-implementing the wiring. The counting stand-in DELEGATES to the production
// walk, so a resolution that falls back still returns the right file and the
// ONLY thing the count can change is the cost signal: a red proof then fails on
// the walk count, never on a wrong path. Restoring puts the production walk
// back AND clears the memo, so a test never leaves a cached walk behind for its
// siblings.
func SessionScanCounterForTest() (count func() int, restore func()) {
	var (
		mu sync.Mutex
		n  int
	)
	prev := taskSessionTreeWalk
	taskSessionTreeWalk = func(dir string) []sessionlayout.Record {
		mu.Lock()
		n++
		mu.Unlock()
		return prev(dir)
	}
	clearSessionTreeCache()
	return func() int {
			mu.Lock()
			defer mu.Unlock()
			return n
		}, func() {
			taskSessionTreeWalk = prev
			clearSessionTreeCache()
		}
}

// clearSessionTreeCache drops the process-lifetime memo (the restore half of
// SessionScanCounterForTest; also the reset between two cost assertions).
func clearSessionTreeCache() {
	taskSessionTreeMu.Lock()
	taskSessionTreeCache = nil
	taskSessionTreeMu.Unlock()
}

// taskFindSessionJSONLByTask finds the session JSONL of a RUN whose record is
// filed under the row's TASK ID — `sessions/<task_id>/<uuid>.jsonl` — which is
// the layout a task run writes and the layout the reconciler must read.
//
// WHY IT IS SEPARATE FROM taskFindSessionJSONL. That resolver answers "the
// session named by this SESSION id", and a task row's session_id column is the
// SUBMITTING session (the dispatcher), not the run's own — so looking a row's
// session_id up finds the wrong file, or nothing at all. Measured on the live
// ledger (2026-09-27): of 271 GoPi rows carrying a session_id, 34 resolve to a
// flat `sessions/<uuid>.jsonl` and 215 resolve ONLY through their own task
// directory; the row this epic reconciles is one of the 215.
//
// The TASK ID is the key and the session record is verified against it: the
// returned file's own session_start must carry that taskId (the same identity
// check taskFindSessionJSONL applies to a session id), so a task directory
// holding several runs yields the one belonging to this row — never the first
// file that happens to sort first. Several runs of one task (a resume) are read
// in full by the caller, which scans every record it is given.
func taskFindSessionJSONLByTask(root, taskID string) []string {
	// The whitespace rule is the ONE seam (taskIDNormalize), never re-typed
	// here: this entry point was re-inlined with the raw trim by the
	// taskid-whitespace-normalize-residual epic's own contract ("every task_id
	// entry point reaches the rule"), and TestTaskIDNormalizeIsTheSingleSeam is
	// the gate that says so (Rules 13/14).
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return nil
	}
	// THE SHAPE GATE (bug row 01a12f3c-d888-720e-b7b5-f458f55e98aa,
	// 2026-10-12): this function turns the id into a PATH ELEMENT by joining it
	// under the sessions root, so it asks the SAME question the writers ask —
	// internal/sessionid, one definition, both sides of the agent/tools
	// boundary (Rules 13/14). Without it a caller holding another spelling of
	// the row searched a directory that holds no run of this row and got an
	// empty answer indistinguishable from "this row has no run record" — the
	// silent join, on the READ side.
	//
	// A refusal is REPORTED rather than silently dropped: this is a read seam
	// reached from a diagnosis path, and its caller (the ground reconciler)
	// turns "no paths" into a displayed verdict — an inadmissible id would
	// otherwise read there as evidence that the row ran nowhere, which is a
	// worse failure than the defect (Rule 15). The report names the operand and
	// internal/sessionid's own sentence, and the answer stays EMPTY: this seam
	// reports and never mints a directory (it is a reader, not a writer).
	if !sessionid.IsAdmissible(taskID) {
		fmt.Fprintf(os.Stderr, "taskFindSessionJSONLByTask: %v — no session directory was searched\n", sessionidRefusal(taskID))
		return nil
	}
	dir := filepath.Join(root, taskID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // no per-task run directory: the row ran under another layout
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), taskSessionJSONLExt) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if taskSessionTaskID(path) == taskID {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// taskSessionTaskID reads the session_start event's taskId from a session JSONL
// (first line; "" when unreadable) — the task-record counterpart of
// taskSessionStartID, through the same one-line read shape.
func taskSessionTaskID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)
	if !sc.Scan() {
		return ""
	}
	var ev struct {
		TaskID string `json:"taskId"`
	}
	if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
		return ""
	}
	return strings.TrimSpace(ev.TaskID)
}

// findSessionJSONLFlat is taskFindSessionJSONL's legacy top-level pass: EXACT
// basename match over sessions/<uuid>.jsonl files — every bare *.jsonl at the
// sessions ROOT is a legacy flat record, exactly the layout key
// sessionlayout applies at the root (the flat layout has no session dir for a
// side file to live in, so the two agree by construction).
//
// It used to REMEMBER the first prefix hit as a fallback, which is the silent
// mis-resolution the epic removed: a truncated id handed back whichever record
// happened to sort first. One pass, exact only (epic full-id-no-prefix).
func findSessionJSONLFlat(dir, id string) string {
	if id == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), taskSessionJSONLExt) {
			continue
		}
		if strings.TrimSuffix(e.Name(), taskSessionJSONLExt) == id {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// taskSessionStartID reads the session_start event's sessionId from a session
// JSONL (first line; "" when unreadable).
func taskSessionStartID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)
	if !sc.Scan() {
		return ""
	}
	var ev struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
		return ""
	}
	return strings.TrimSpace(ev.SessionID)
}

// taskSessionLogPath resolves the full path of a session's JSONL audit log
// under the gopi data root's sessions tree (config.GopiDir(): $GOPI_HOME/.gopi
// when GOPI_HOME is set, else ~/.gopi), the same resolution
// taskModelFromSession uses. Session-dir-unify S1: a session log may live in
// any of the three layouts — legacy flat sessions/<session-id>.jsonl,
// task-run sessions/<task_id>/<run-uuid>.jsonl, or bare
// sessions/<session-id>/session.jsonl — so the resolution first LOCATES the
// actual file (exact then prefix, all layouts); when none exists it returns
// the deterministic bare-session guess (sessions/<id>/session.jsonl — where
// new bare sessions land) so the "(not found)" marker stays meaningful.
// Returns "" for an empty session id (rows without a session have no log to
// point at).
func taskSessionLogPath(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	gd, err := config.GopiDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(gd, config.SessionsDirName)
	if p := taskFindSessionJSONL(dir, sessionID); p != "" {
		return p
	}
	return filepath.Join(dir, sessionID, taskBareSessionRecordName)
}

// taskModelFromSession resolves the model that ran a task's session by
// reading the session_start event from its JSONL log (in the gopi data
// root — config.GopiDir(): $GOPI_HOME/.gopi when GOPI_HOME is set, else
// ~/.gopi; the sessions dir mirrors NewSessionLogger's resolution exactly,
// so a GOPI_HOME-sandboxed run resolves correctly too).
// Returns "" when the session cannot be located or carries no model — the
// caller renders "(unknown)". session_start is always the first line of the
// file (NewSessionLogger writes+syncs it before any other event), so only
// that one line is read — a long session is never fully parsed for a single
// show. This is a read-only audit-layer lookup; it never mutates the ledger.
func taskModelFromSession(sessionID string) string {
	model, _ := taskSessionStartEvent(sessionID)
	return model
}

// taskSessionStartEvent reads the session_start event (always the first
// line — NewSessionLogger writes+syncs it before any other event) from a
// session's JSONL and returns the parsed fields the audit layer needs:
// model and rawLogPath. Locates the file exactly like taskModelFromSession
// (exact-then-prefix). Returns empty fields when the session cannot be
// located or carries no event — callers render "(unknown)"/no-line.
func taskSessionStartEvent(sessionID string) (model, rawLogPath string) {
	if strings.TrimSpace(sessionID) == "" {
		return "", ""
	}
	gd, err := config.GopiDir()
	if err != nil {
		return "", ""
	}
	dir := filepath.Join(gd, config.SessionsDirName)
	path := taskFindSessionJSONL(dir, sessionID)
	if path == "" {
		return "", ""
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)
	if !sc.Scan() {
		return "", ""
	}
	var ev struct {
		Model      string `json:"model"`
		RawLogPath string `json:"rawLogPath"`
	}
	if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
		return "", ""
	}
	return ev.Model, ev.RawLogPath
}

// taskRawLogPathFromSession resolves the full path of the session's RAW HTTP
// log from the rawLogPath field recorded in the session_start event (written
// by NewSessionLogger since the resume feature). The recorded path is
// AUTHORITATIVE — it survives custom GOPI_RAW_LOG_DIR setups that static
// config resolution cannot reconstruct (gopilog.ResolveRawLogForSession
// prefers it over timestamp pairing for exactly this reason). Returns "" for
// pre-resume sessions (no field) or when the session JSONL cannot be found —
// the caller renders no raw_log line then (timestamp pairing is not an
// option from a single row without scanning all raw logs).
//
// A moved raw log is followed: the re-home sweep (epic task-session-cwd) puts
// a located log INSIDE its session directory, so when the recorded absolute
// path no longer exists the same BASENAME beside the session JSONL is tried
// next — the record still names the file, only its directory changed.
func taskRawLogPathFromSession(sessionID string) string {
	_, rawLogPath := taskSessionStartEvent(sessionID)
	if rawLogPath == "" {
		return ""
	}
	if _, err := os.Stat(rawLogPath); err == nil {
		return rawLogPath
	}
	if jsonl := taskSessionLogPath(sessionID); jsonl != "" {
		sib := filepath.Join(filepath.Dir(jsonl), filepath.Base(rawLogPath))
		if sib != rawLogPath {
			if _, err := os.Stat(sib); err == nil {
				return sib
			}
		}
	}
	return rawLogPath
}

// TaskShow renders one task's full record. A missing task_id is an error
// (script-friendly exit-1, mirroring code-search's miss semantics).

// Briefing display constants (task-show-briefing fix): the briefing is what
// the task actually asks for — without it `gopi task show` shows only ledger
// metadata and a code-name summary means nothing to the reader.
const (
	// taskBriefingMaxChars caps the session-derived briefing shown by
	// `task show`. The DB payload column is shown in full (it is authored
	// as the briefing on purpose); only the JSONL fallback is capped — the
	// session log keeps the full text.
	taskBriefingMaxChars = 800

	// taskBriefingEllipsis marks a truncated session-derived briefing.
	taskBriefingEllipsis = " …[truncated — full text: session JSONL]"

	// taskEpicPrefix is the kernel's wrapper around the first user message
	// (runEngine frames every task input with it). THE VALUE IS NOT DEFINED
	// HERE (Rule 14): it is sessionevent.HarnessEpicGoalFrameMarker, in the
	// leaf package the session-log readers also import, so the frame a WRITER
	// puts on a row and the frame a READER skips cannot drift apart.
	taskEpicPrefix = sessionevent.HarnessEpicGoalFrameMarker

	// taskEpicFramingMarker begins the task-list instruction paragraph the
	// kernel appends after the user text. Cutting there (paragraph-bounded)
	// leaves the user's actual task text.
	taskEpicFramingMarker = "\n\nBased on this Epic"

	// taskTaskListMarker is the task-list instruction block some framings
	// append; same cut semantics as taskEpicFramingMarker.
	taskTaskListMarker = "[TASK_LIST_BEGIN]"
)

// taskSubmittedBy renders the provenance line for `task show` — WHO put
// this row in the ledger. session_id is the SUBMITTER's session for llm
// rows (stamped by the task_submit tool from the submitting session) and
// the run's own session for auto-registered rows (the session UUID — the
// row's t-<unix> task_id is decoupled from it after consolidation).
// prioOr maps a row's raw priority to the display value: 0 (legacy NULL
// column) falls back to the default tier — a bare 0 would read as
// "highest priority".
func prioOr(p int) int {
	if p <= 0 {
		return taskPriorityDefault
	}
	return p
}

// taskSubmittedBy renders the submitter provenance line for a row's source.
//
// THE VOCABULARY IS internal/tasksource's (Rule 14) — this switch compares the
// constants, never their spellings, so a retired value or a future one cannot
// leave a row rendering a blank provenance. The two ALIASES are still resolved
// for HISTORY: the operator's ledger carries ~633 `llm` and ~993 `auto` rows
// written before the vocabulary was canonicalized (tasksource's package doc),
// and `task show` on an old row must keep explaining it.
func taskSubmittedBy(t *Task) string {
	value, _ := tasksource.Normalize(t.Source)
	switch value {
	case tasksource.Agent:
		return fmt.Sprintf("LLM session %s (task_submit)", orDash(taskSubmitterSessionOf(t)))
	case tasksource.Human:
		return "human (gopi task submit CLI)"
	case tasksource.Server, tasksource.Merger:
		// A row that CARRIES a session id IS a run row: the resident
		// machinery registered it at start (internal/agent/taskregister.go),
		// and "harness auto-registration" is what that mechanism has always
		// been called in this rendering.
		if sub := taskSubmitterSessionOf(t); sub != "" {
			return "harness auto-registration (row IS the run: session " + sub + ")"
		}
		// No session to point at. The row's own recorded writer decides the
		// sentence — a `merger` row is machine-enqueued work, and calling it
		// external tooling would send its reader looking for a run that never
		// existed; only the retired "auto" spelling covers both, so it keeps
		// the label it has always had.
		if t.Source != tasksource.Server {
			return "harness/external tooling (source=auto, no session recorded)"
		}
		return "resident node machinery (" + tasksource.Server + ", no session recorded)"
	case tasksource.Worker:
		return "task executor follow-up row (" + tasksource.Worker + ")"
	case tasksource.Forward:
		// The local mirror's copy of a row that lives on the authoritative
		// ledger: the fact this ledger may record is the JOURNEY, not the
		// author (which the peer's copy carries). Before tasksource.Forward
		// existed this row read "auto" → merger → a merge-path enqueue that
		// never happened.
		return "forwarded to the authoritative ledger (" + tasksource.Forward + "; this ledger holds the mirror copy)"
	case tasksource.AgencyProposal:
		return "unattended improvement miner (" + tasksource.AgencyProposal + ")"
	case tasksource.MirrorTerminal:
		// The mirror-history copy of a row that finished on another node:
		// this ledger never enqueued it and never ran it. Before the value
		// existed the writer spelled the raw literal "sync", which is not
		// ledger vocabulary — Normalize left it unrecognized and the row
		// fell through to orDash, narrating a provenance nothing had ever
		// validated.
		return "mirrored terminal history (" + tasksource.MirrorTerminal + "; this ledger never ran it)"
	}
	return orDash(t.Source)
}

// stripEpicFraming removes the kernel's [EPIC GOAL] wrapper and the
// appended task-list instruction paragraph from a session's first user
// message, leaving the user's actual task text.
func stripEpicFraming(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, taskEpicPrefix)
	s = strings.TrimSpace(s)
	for _, marker := range []string{taskEpicFramingMarker, taskTaskListMarker} {
		if i := strings.Index(s, marker); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

// capTaskBriefing bounds the session-derived briefing for display.
func capTaskBriefing(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > taskBriefingMaxChars {
		return string(r[:taskBriefingMaxChars]) + taskBriefingEllipsis
	}
	return s
}

// taskBriefingFromSession recovers a run's briefing from its session JSONL
// (the first plain-text user message): auto-registered rows keep the
// briefing only as the summary prefix + the session log, never as a payload
// column — so a code-name summary would otherwise be unexplainable at show
// time. Returns "" when the session file is missing or its first user
// message is not plain text. Read-only audit-layer lookup — same posture
// as taskModelFromSession, never mutates the ledger.
func taskBriefingFromSession(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	gd, err := config.GopiDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(gd, config.SessionsDirName)
	path := taskFindSessionJSONL(dir, sessionID)
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)
	var ev struct {
		Type    string `json:"type"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	for sc.Scan() {
		ev.Type, ev.Message.Content = "", nil
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue // not a JSON event line — skip
		}
		if ev.Type != "user" {
			continue // session_start and later events precede the briefing
		}
		var text string
		if err := json.Unmarshal(ev.Message.Content, &text); err != nil {
			return "" // block-style content (tool rounds) — not a plain briefing
		}
		return capTaskBriefing(stripEpicFraming(text))
	}
	return ""
}

// taskShowLabelWidth is the task-show field gutter: every "label:" line pads
// its value to this column so the block reads as a table. The run-provenance
// labels are the WIDEST in the block (run_method / run_cmd → a 16-column
// gutter), so the renderer's column is defined by them — one constant,
// referenced by every runcmd.Field call that renders into the block.
const taskShowLabelWidth = 16

// taskShowQuotedIndent is the gutter that marks AUTHORED text inside the
// digest — the payload (the briefing), an event detail, a reason, the bounded
// report body. Every one of those is prose the LEDGER did not write: it came
// from an operator, a model or a run's own transcription of a terminal.
//
// WHY THE RENDERER OWNS THIS AND NOT THE AUTHOR (the measured defect,
// 2026-10-29, rows 01a184e8 / 01a187c3 / 01a17e76): these bodies used to be
// appended at COLUMN 0, so a briefing that quoted a shell transcript
//
//	$ gopi task status 01a1848b-b125-763d-b75c-813d3d905df1
//	status:      completed                   ← 正常
//
// contributed a line byte-indistinguishable from the digest's own `status:`
// field. `grep -c '^status:'` then read 2, 2 and 5 on three REAL rows — and
// the ledger was never wrong: taskByID is an EQUALITY (`WHERE task_id = ?`),
// so a same-prefix sibling can never reach the read. The rows were right; the
// RENDER let someone else's text wear the digest's clothes.
//
// So the invariant this constant encodes is the fix, not a style choice:
// EXACTLY ONE place in `task show` prints a field label at column 0, and it is
// the digest itself. Any other body is quoted under this gutter, where no
// `grep -c '^status:'` (or any other field probe) can mistake it for a field.
// A future body added at column 0 is the same defect returning.
const taskShowQuotedIndent = "    "

// taskShowIndentLines prefixes every NON-EMPTY line of an authored body with
// the quote gutter. Empty lines stay empty (no trailing whitespace), so the
// indentation marks TEXT and nothing else; the body's own trailing newline is
// preserved, so a caller's `fmt.Fprintf(…"%s\n")` keeps its spacing.
func taskShowIndentLines(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if ln == "" {
			continue
		}
		lines[i] = taskShowQuotedIndent + ln
	}
	return strings.Join(lines, "\n")
}

// taskShowIndentAfterFirst is the same gate for a value that CONTINUES a
// digest line already printed: the first line stays where the padded label put
// it, and every following line is quoted. A multi-line `result:` detail, a
// `reason:` that wraps and a `verify_evidence:` transcript are the shapes it
// covers.
//
// The first line needs no gutter — it begins after `label:` + padding, which
// is already past column 0 — and a body with no newline is returned VERBATIM,
// so the ordinary single-line case costs nothing and cannot drift.
func taskShowIndentAfterFirst(s string) string {
	i := strings.IndexByte(s, '\n')
	if i < 0 {
		return s
	}
	return s[:i+1] + taskShowIndentLines(s[i+1:])
}

// The deliverable labels of `task show`, in the renderer's own spelling
// (Rule 1b: a caller that must READ the row's resolved ground back — the
// hermeticity guard in cmd/agent, a script's grep — uses these, never a
// re-typed copy that a renderer change would silently orphan).
const (
	TaskShowDeliverableRepoLabel     = "deliverable.repo:"
	TaskShowDeliverableWorktreeLabel = "deliverable.worktree:"
	// TaskShowDeliverableBranchLabel is the same contract for the BRANCH line
	// (it was the one deliverable field `task show` printed from a bare
	// literal, so a reader had to know the spelling to find it). Referenced by
	// the renderer, by ResolveTaskBranch (the by-task-id lookup the config
	// brief uses) and by every caller that greps the field.
	TaskShowDeliverableBranchLabel = "deliverable.branch:"
	// TaskShowDeliverableGroundsLabel renders the SECONDARY grounds of a row —
	// the directories a run CREATED that are not the row's own ground (epic
	// 01a0dff1). One line per ground, because the axis is plural: before it
	// existed, a run's second ground appeared in NO render at all, and
	// the branch→row lookup could answer with a row whose recorded
	// worktree/branch named something else entirely — an answer an operator had
	// no way to check. The provenance sentence on the owner answer points HERE.
	TaskShowDeliverableGroundsLabel = "deliverable.grounds:"
)

// The VERDICT line of `task show` — the second render face of the row's
// append-only trail (epic verify-gate, epic cmd-agent-e2e-seam-stability).
//
// The trail carries TWO kinds of detail, and one line cannot serve both:
//
//   - a run DIGEST, written under the row's own status (the transition writer
//     stamps the new status as the event name) — it renders on `result:`, and
//     taskEventLatestDetail selects it so a later mechanical close-out step
//     cannot displace it;
//   - a JUDGED VERDICT about the row, written under its own semantic event name
//     (merge-verify / finish-cleanup-removal-refused) —
//     it renders on its OWN line, because on `result:` it would both displace
//     the digest (62bfdf58) and be displaced by it (this regression's mirror).
//
// "merge-handover-failed" was a third member of that list until 2026-11-01 and
// is named here so a reader who finds the string in git history knows what
// happened to it: its writer (internal/tools/merge_handover_counter.go) went
// with the top-level `gopi merge` verb, and an event no code writes is not a
// verdict this renderer can be asked to show.
//
// Both land, one line each, so neither face is lost to the other. The labels
// are the renderer's own spelling and a caller reads them back through these
// constants (Rule 1b/14) — never a re-typed copy the renderer change orphans.
const (
	// TaskShowVerdictLabel is the `task show` label for a judged verdict about
	// the row (the event names live in the verdict set — see
	// taskEventLatestVerdict).
	TaskShowVerdictLabel = "verdict:"
	// TaskShowVerdictNameLabel carries WHICH event the verdict came from, so a
	// `where event = ...` query and the rendered line name the same thing.
	TaskShowVerdictNameLabel = "verdict.event:"
	// TaskShowDeliveryOwedLabel is the `task show` label for a recorded
	// DELIVERY DEBT (see deliveryOwedTrailEventNames). Distinct from
	// TaskShowVerdictLabel on purpose: a verdict is a judgment on the row, this
	// is the row's own unconfirmed claim about its deliverable, and collapsing
	// the two would make a later verdict silently unrender this fact — the very
	// defect the axis exists to close.
	TaskShowDeliveryOwedLabel = "delivery-owed:"
	// TaskShowDeliveryOwedNameLabel carries WHICH event the delivery debt came
	// from, so a `where event = ...` query and the rendered line name the same
	// thing (the same contract TaskShowVerdictNameLabel has). Both labels stay
	// SHORTER than taskShowLabelWidth so the row keeps its alignment: widening
	// the shared label width to fit one new label would re-space every existing
	// line of `task show`.
	TaskShowDeliveryOwedNameLabel = "delivery.event:"
)

// taskShowDeliveryOwedRowLimit bounds the delivery-debt lookup. Measured on
// the live ledger (2026-11-01, 135 delivery-proof-owed occurrences): the count
// of NEWER events on the same row is median 3, p90 5, MAX 8 — so this window
// holds every record that exists, and is 4× the measured maximum rather than
// an arbitrary round number (Rule 1: a limit is a named, justified constant).
const taskShowDeliveryOwedRowLimit = taskShowVerdictRowLimit

// taskShowVerdictRowLimit bounds the verdict lookup: the verdict is by
// construction the newest ruling on the row, so a short newest-first window
// always contains it. It exists so a pathological trail (thousands of run
// transitions — the transition writer prunes nothing) costs one bounded scan
// rather than an unbounded query on the read path.
const taskShowVerdictRowLimit = 32

// The DELIVERY-OWED line of `task show` — the trail's THIRD render face.
//
// WHY IT IS NOT A MEMBER OF verdictTrailEventNames, and why "just add the name"
// is not the fix (the defect this axis exists to close; measured 2026-11-01 on
// the live ledger):
//
//	occurrences of delivery-proof-owed   135
//	which are the NEWEST verdict-family   37   ← what `verdict:` can show
//	SHADOWED by a later verdict           98   ← written, and still invisible
//
// The shadow is not an accident of reporting order, it is the RECLAIM ARM's
// own design: a stranded clone ground gets `delivery-proof-owed` when the
// writeback is refused, and days later the reclaim pass stamps
// `reclaim-unreachable` on the same row ("this pass will not report this row
// again"). The newest-wins selector keeps the LATER of the two, so on 98 of 135
// records the delivery fact — the one that says WHY the work is still not
// anywhere a merger can see it — is displaced by the fact that nobody will
// look again. Adding the name to the verdict list would therefore move 63
// already-rendered rows' `verdict:` line to a different event while showing
// the operator almost nothing new: it fixes the minority and re-labels the
// majority.
//
// So the delivery record gets its OWN axis, selected by its OWN name, rendered
// on its OWN line. Three faces, three lines, none able to displace another:
//
//	result:   the run digest         (taskEventLatestDetail)
//	verdict:  the newest judgment    (taskEventLatestVerdict)
//	delivery: the delivery debt      (taskEventLatestDeliveryOwed, below)
//
// Membership test for deliveryOwedTrailEventNames: the event is this arm's own
// record that the row OWES a delivery — a claim it made about its deliverable
// that the measurement could not confirm. Not a run digest, not a status
// transition, and (the whole point) not interchangeable with a verdict: a
// later verdict must be able to render WITHOUT taking this fact off the row.
var deliveryOwedTrailEventNames = []string{
	// "delivery-proof-owed" — the writeback was REFUSED because the row's tip
	// is not at its declared delivery point (task_delivery_proof.go).
	deliveryProofOwedEvent,
}

// taskShowDeliveryOwedEventName is the exact membership test the delivery-owed
// reader applies, referencing the constant (Rule 1b: never a re-typed literal).
func taskShowDeliveryOwedEventName(event string) bool {
	for _, name := range deliveryOwedTrailEventNames {
		if event == name {
			return true
		}
	}
	return false
}

// TaskDeliverableSpec is the STRUCTURED deliverable blob (deliverable.repo /
// branch / worktree) as the ledger stores it. It is a named type because
// three surfaces must agree on the field spellings (Rule 14): the by-task-id
// mutations in this package (task_deliverable_branch.go), `task show`'s
// renderer, and the resolver below. The blob the agent records at submit time
// (taskregister / the submit engine) marshals to exactly these keys.
type TaskDeliverableSpec struct {
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"`
}

// TaskDeliverableBranch reads the deliverable blob's branch field. A row with
// no blob, an unparsable blob, or a blob carrying no branch returns "" — the
// caller reports "not recorded", never a guess (Rule 15: the safe direction;
// the blob is the recorded truth, not a reconstruction).
func TaskDeliverableBranch(deliverableJSON string) string {
	if strings.TrimSpace(deliverableJSON) == "" {
		return ""
	}
	var spec TaskDeliverableSpec
	if err := json.Unmarshal([]byte(deliverableJSON), &spec); err != nil {
		return ""
	}
	return strings.TrimSpace(spec.Branch)
}

// ResolveTaskBranch returns the branch recorded on taskID's deliverable blob.
// found=false when the row does not exist; branch == "" when it exists but
// records no branch. One indexed ledger lookup (GOPI_MEMORY_DB-routed, the
// same DB every other task read opens), so a caller never has to shell out to
// `gopi task show` and re-parse rendered text.
func ResolveTaskBranch(ctx context.Context, taskID string) (branch string, found bool, err error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", false, fmt.Errorf("task: task_id must not be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// READ-ONLY OPENER — this face is a PURE READ: its SELECTs are the whole of
	// what it does, and it calls no writer (openTasksDBReadOnly skips the tasks DDL
	// batch and takes no BEGIN IMMEDIATE, so a reader never queues on the writer
	// that holds the ledger).
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return "", false, err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", false, fmt.Errorf("task: branch lookup: %w", err)
	}
	if t == nil {
		return "", false, nil
	}
	return TaskDeliverableBranch(t.Deliverable), true, nil
}

func TaskShow(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	return taskShowWithDB(ctx, db, taskID)
}

// openTasksDBReadOnly opens the tasks ledger for a PURE READER: no schema
// migration, no journal-mode switch, no write lock — the DSN is production's
// own read-only contract (LedgerReadOnlyDSN: mode=ro + query_only + the same
// busy timeout), so the handle CANNOT write the ledger and cannot queue on
// another process's write lock.
//
// WHY THIS EXISTS (measured 2026-10-13, the bug this seam closes).
// `gopi task show` renders a row it only READS, but it opened the ledger
// through openTasksDB — which runs the tasks DDL batch on EVERY open
// (ensureTasksSchema: CREATE TABLE IF NOT EXISTS, the P2/proc-identity ALTERs,
// then migrateTasksFTS's index-shape probe) over a `_txlock=immediate` DSN,
// i.e. a BEGIN IMMEDIATE — the ledger's WRITE lock — on every invocation. On a
// host whose ledger carries the resident worker/merger daemons (this one runs
// three) that lock is regularly held, and the read verb then waits out
// dbBusyTimeoutMSec before its first SELECT. Measured on a ledger copy with one
// `BEGIN IMMEDIATE` holder held by a separate process, interleaved A/B, three
// reps (evidence: lock-ab.txt, session dir):
//
//	unlocked:   57ms / 219ms /   52ms   (rc=0)
//	locked:  17226ms / 17126ms / 17130ms  (rc=0)
//
// — a ~17.2s (dbBusyTimeoutMSec ≈ 15s + the batch's bounded retries) stall, not
// a hang, and not confined to one row. Under the operator's 15s/40s `timeout`
// wrappers that is exactly the reported "hangs until timeout", and it is also
// the whole of the reporter's second half (a row unreadable while `task list`
// lists it): `task list` does not read the run directory, so its own read is
// short enough to finish inside a 15s kill where show's advisory resolution is
// not. The strace of the same run pins the mechanism — fcntl(F_SETLK,
// F_WRLCK, …) = -1 EAGAIN followed by the busy-handler's 100ms nanosleeps —
// and the row's own digest shows the run directories the same verb reads
// (evidence: lock-mechanism.txt).
//
// The read-only handle is also the CORRECT handle for this verb on its own
// merits: show resolves nothing, migrates nothing and switches no journal mode,
// so a verb that may not write must not take the write lock. Its reads are the
// same reads (taskByID + the advisory/resolution helpers, all read-only by
// contract), so the digest it renders is byte-for-byte what the read-write
// handle rendered — measured, not assumed
// (TestTaskShowReadOnlyOpenerHoldsTheSameDigest).
func openTasksDBReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	if !fileExists(path) {
		// NO LEDGER YET — the one case where this reader must behave exactly as
		// openTasksDB always did (create it, build its schema, answer "no task
		// with task_id"): a read-only open of a path that does not exist fails
		// with SQLite's "unable to open database file", and the FIX must not
		// turn the ordinary roll-up of a fresh profile into an error (measured:
		// TestTaskShowNotFound / TestTaskShowDoesNotOfferASpellingRepair…
		// reddened with that message before this arm existed).
		//
		// The fallback cannot reintroduce the defect: the write lock it may take
		// is a lock on a file that DOES NOT EXIST, so no other process can be
		// holding it — there is nothing to queue behind. It is reached once per
		// profile, on the very first `gopi task` open.
		return openTasksDB(ctx, path)
	}
	db, err := sql.Open("sqlite", LedgerReadOnlyDSN(path))
	if err != nil {
		return nil, err
	}
	// One connection, exactly as openDB pins it (SQLite serializes; a pool
	// would also make the handle-level pragma below non-deterministic).
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(ledgerReadQueryOnlyPragma); err != nil {
		db.Close() //stylegate:ignore handle closed after its setup failed; the caller gets the error and no handle
		return nil, fmt.Errorf("task: read-only ledger open %s: %w", path, err)
	}
	return alignReadOnlyLedgerSchema(ctx, db, path)
}

// TaskSessionID returns the RUN session recorded on a task row — the uuid of the
// record under sessions/<task-id>/, which is the key under which that run's
// write_task_area conclusions were recorded. Empty when the row has none (rows
// are filed before their run session exists, and the deferred stamp fills it).
// Used by the resume path to recover the ORIGINAL session's id (memory-aware T2
// resume carry-forward): `gopi session resume <log> --task <id>` links to a task
// whose session_id column holds the crashed run's session.
//
// NOT the submitter: that is a different fact and lives in its own column —
// TaskSubmitterSession answers it for both storage shapes (2026-10-28).
func TaskSessionID(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: task_id must not be empty")
	}
	ctx := context.Background()
	// READ-ONLY OPENER — this face is a PURE READ: its SELECTs are the whole of
	// what it does, and it calls no writer (openTasksDBReadOnly skips the tasks DDL
	// batch and takes no BEGIN IMMEDIATE, so a reader never queues on the writer
	// that holds the ledger).
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", fmt.Errorf("task: session_id lookup: %w", err)
	}
	if t == nil {
		return "", fmt.Errorf("task: no task with task_id=%q%s", taskID, taskIDPrefixRefusal(ctx, db, taskID))
	}
	return t.SessionID, nil
}

// TaskSubmitterSession returns the session that FILED a task row — the
// anti-self-reference identity — resolved through taskSubmitterSessionOf so the
// two storage shapes (post-split column, legacy `session_id`) answer alike.
// Empty when the row carries none. An absent row returns an error.
func TaskSubmitterSession(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", fmt.Errorf("task: submitter_session lookup: %w", err)
	}
	if t == nil {
		return "", fmt.Errorf("task: no task with task_id=%q%s", taskID, taskIDPrefixRefusal(ctx, db, taskID))
	}
	return taskSubmitterSessionOf(t), nil
}

// taskSubmitterSessionOf answers the ONE question the anti-self-reference guard
// asks: which session FILED this row. Two storage shapes exist because the
// submitter moved out of `session_id` (row-identity epic, 2026-10-28) without
// rewriting history:
//
//   - post-split rows: the submitter column carries it, `session_id` carries the
//     row's own id;
//   - pre-split rows: the submitter column is EMPTY and `session_id` IS the
//     submitter's session (historical rows are never rewritten).
//
// Because `session_id` now normally holds the row id, the legacy arm must not
// read it as a submitter when it equals the row id — that value is the row's own
// run identity, and treating it as a submitter would make a row unclaimable by
// the run that holds it.
func taskSubmitterSessionOf(t *Task) string {
	if t == nil {
		return ""
	}
	if s := strings.TrimSpace(t.SubmitterSession); s != "" {
		return s
	}
	if s := strings.TrimSpace(t.SessionID); s != "" && s != t.TaskID {
		return s // legacy row: the column still holds the submitter
	}
	return ""
}

// TaskCreatedAt returns the epoch second a row was CREATED, and whether the row
// carries one at all.
//
// It is the authoring moment of a row's briefing — the ONLY anchor a sentence of
// the shape "<surface> is RED on master" carries, because such a sentence names
// no base revision (the dispatch seam's gate-assertion half re-anchors the moment
// through this value; taskreport.GateAssertionsIn is its detector). A row read
// without a value, or with a value <= 0, must be distinguishable from a row read
// at epoch 0 (Rule 15a: the required semantic is not the zero value), which is
// why the second return is a presence flag rather than a sentinel epoch.
//
// SAME READ-ONLY OPENER as every other row lookup on this read path
// (openTasksDBReadOnly: no DDL batch, no BEGIN IMMEDIATE), so a reader never
// queues on the writer that holds the ledger.
func TaskCreatedAt(taskID string) (float64, bool, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return 0, false, fmt.Errorf("task: created_at lookup: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return 0, false, err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return 0, false, fmt.Errorf("task: created_at lookup: %w", err)
	}
	if t == nil {
		return 0, false, fmt.Errorf("task: no task with task_id=%q%s", taskID, taskIDPrefixRefusal(ctx, db, taskID))
	}
	if t.CreatedAt <= 0 {
		return 0, false, nil
	}
	return t.CreatedAt, true, nil
}

// taskShowWithDB renders the `gopi task show` digest for one row against an
// open DB handle (the row lookup is done by the caller).
func taskShowWithDB(ctx context.Context, db *sql.DB, taskID string) (string, error) {
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", fmt.Errorf("task: show: %w", err)
	}
	if t == nil {
		return "", fmt.Errorf("task: no task with task_id=%q%s%s", taskID, taskIDNearMissSuffix(ctx, db, taskID), taskIDPrefixRefusal(ctx, db, taskID))
	}
	// The dependency gate's id → status index, read ONCE for the parent block
	// below. A multi-parent row renders each parent's status, and a second
	// per-parent query would be the same read N times; a row with one parent
	// never touches it (the block is only rendered for a multi-parent set).
	var parentStatus map[string]string
	if parents := taskParentIDsOf(t.ParentTaskID, t.ParentTaskIDs); len(parents) > 1 {
		ps, perr := taskParentStatuses(ctx, db)
		if perr != nil {
			return "", fmt.Errorf("task: show: parent status: %w", perr)
		}
		parentStatus = ps
	}
	var b strings.Builder
	fmt.Fprintf(&b, "task_id:        %s\n", t.TaskID)
	// The status line carries the MEASURED holder-gone fact when there is one
	// (taskholder_gone.go): "in_progress" alone is the word this epic reports as
	// ambiguous — it means both "running now" and "was claimed", and a dead
	// holder cannot be told from a working one without the measurement. The
	// verdict/holder lines below state it in full; this makes it the FIRST thing
	// read, which is what an operator scanning a list of rows needs.
	showGone := taskHolderGoneStatusMarker(t)
	fmt.Fprintf(&b, "status:         %s%s\n", t.Status, showGone)
	// THE SESSION LAYER, MEASURED, BESIDE THE DECLARATION (tasksessionfacts.go).
	// `status:` is the ledger's WORD — it says what someone wrote, not what
	// happened. The line below reads the run's own artifacts (rundir's record,
	// the raw log's request count, the crash document's category) and states
	// them next to it, so a row that never got as far as its first model call
	// is distinguishable from one that ran for twenty minutes and failed. It is
	// a READ: no state value is added, no table written, no history rewritten.
	b.WriteString(taskSessionFactsLine(t.TaskID, t.SessionID) + "\n")
	// Same render-time text boundary as TaskList (see tasksummarytext.go).
	fmt.Fprintf(&b, "summary:        %s\n", sanitizeTaskSummary(t.Summary))
	// Session log path (tail -f convenience): the run's JSONL audit trail.
	// RESOLVED FROM THE ROW'S RUN DIRECTORY (epic run-identity-unify): the
	// directory is named by the row's own task id, so the newest run log is a
	// directory read, never a tree search. The session_id-keyed resolution is
	// kept as the FALLBACK for a row whose directory holds no run log (a
	// re-homed/pre-layout run) — it can still answer, at the cost the epic
	// exists to remove. Rendered when the file exists; a missing file (pruned
	// session / no session recorded) shows the resolved path with a
	// "(not found)" marker so the absence is explicit rather than silent.
	if path := taskRunNewestLog(t.TaskID); path != "" {
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(&b, "session_log:    %s\n", path)
		} else {
			fmt.Fprintf(&b, "session_log:    %s (not found — session pruned or not recorded)\n", path)
		}
	} else if path := taskSessionLogPath(t.SessionID); path != "" {
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(&b, "session_log:    %s\n", path)
		} else {
			fmt.Fprintf(&b, "session_log:    %s (not found — session pruned or not recorded)\n", path)
		}
	}
	// Raw log path (tail -f convenience — the byte-exact HTTP level): the
	// session's raw HTTP log, resolved from the rawLogPath field recorded in
	// its session_start event. This is the authoritative full path for a
	// background worker run — `tail -f <path>` follows the raw → REQUEST /
	// ← CHUNK / ← RESPONSE stream live. The row's run directory is consulted
	// FIRST (its newest run is the one that actually executed the row — the
	// ledger's session_id column frequently names another run, or one that no
	// longer exists), and the session_id-keyed lookup stays as the fallback.
	// Rendered only when recorded (pre-resume sessions carry no rawLogPath
	// field — no line then, the absence is the honest signal; a
	// recorded-but-missing file shows the path with a "(not found)" marker).
	rawPath := taskRunNewestRawLog(t.TaskID)
	if rawPath == "" {
		rawPath = taskRawLogPathFromSession(t.SessionID)
	}
	if rawPath != "" {
		if _, err := os.Stat(rawPath); err == nil {
			fmt.Fprintf(&b, "raw_log:        %s\n", rawPath)
		} else {
			fmt.Fprintf(&b, "raw_log:        %s (not found — raw log pruned or raw logging off)\n", rawPath)
		}
	}
	// Run output log (epic worker-log-session-attribution, re-pointed by
	// child-stream-task-output): the executor child's own stdout/stderr for
	// the run that executed this row, written to the task's session dir by
	// the spawn seam that knew the row — whatever spawned it. This line is
	// the THIRD leg of the task→output walk, next to session_log/raw_log
	// above — with it, a failed task id alone reaches every byte the run
	// produced, no other record consulted.
	if lg := taskWorkerOutputLogLine(t.TaskID); lg != "" {
		b.WriteString(lg)
	}
	// THE RUN BLOCK (epic run-identity-unify) is the row's RUN identity:
	// run_dir + run_log + a per-run table read off the runs' own logs — which
	// run executed the row, how many there were, how each ended. It answers
	// the question the bare uuid used to be asked and could not answer
	// (the operator's word for that line: "completely meaningless").
	for _, line := range taskRunLines(t.TaskID, t.SessionID) {
		b.WriteString(line + "\n")
	}
	// The ledger COLUMN, labelled by what it actually is (epic
	// run-identity-unify): the session that submitted/executed the row. It is
	// NOT the run's address — the run directory is named by the row's own id
	// (see run_dir above) — and for a multi-run row it names whichever run the
	// stamping seam saw first. Always rendered, so an EMPTY column is an
	// explicit absence rather than a line that quietly disappears; kept
	// visible because it is the anti-self-reference key and the ledger's own
	// field, never presented as the run.
	// The line keeps the ledger column's OWN rendering — present whether or not
	// the column is filled (an empty column shows an empty value, never a
	// vanished line). Its ROLE is carried by the run block above: `run_dir:` is
	// the row's address, and every run in it is listed under it. That block is
	// the answer to "which run is this?"; this value is the ledger's field.
	fmt.Fprintf(&b, "session_id:     %s\n", t.SessionID)
	// DB-first: the model captured at creation time is the source of truth.
	// Legacy fallback, in the order the addresses became trustworthy: the
	// row's own run directory (the run that executed it), then the
	// session_id-keyed JSONL (pre-persistence rows, or a run whose log lives
	// outside the row's directory) — kept only so history keeps displaying
	// after session-log pruning.
	model := t.Model
	if model == "" {
		model = taskModelFromRunDir(t.TaskID)
	}
	if model == "" {
		model = taskModelFromSession(t.SessionID)
	}
	if model == "" {
		model = taskModelUnknown
	}
	fmt.Fprintf(&b, "model:          %s\n", model)
	// THE PARENT BLOCK (EPIC multi-parent). The FIRST line keeps its exact
	// spelling and meaning — `parent_task_id: <id>` — so a historical row and
	// every existing reader of this output (docs, tests, grep, an operator's
	// habit) see byte-for-byte what they always saw. The rest of the set is
	// rendered as ONE extra line, and ONLY when the set has more than one
	// member: a single-parent row prints exactly one line, as before.
	fmt.Fprintf(&b, "parent_task_id: %s\n", orDash(t.ParentTaskID))
	if parents := taskParentIDsOf(t.ParentTaskID, t.ParentTaskIDs); len(parents) > 1 {
		fmt.Fprintf(&b, "parent_task_ids: %s\n", strings.Join(parents[1:], taskParentIDsSeparator))
		// Per-parent status, so "which producer am I still waiting for?" is
		// answered by the row itself instead of by a second command per parent.
		// A status that cannot be read is SAID (Rule 15), never rendered as
		// "released" by omission.
		for _, parentID := range parents {
			fmt.Fprintf(&b, "  parent %s: %s\n", parentID, taskParentStatusLabel(parentStatus, parentID))
		}
	}
	fmt.Fprintf(&b, "tags:           %s\n", orDash(t.Tags))
	fmt.Fprintf(&b, "priority:       %d\n", prioOr(t.Priority))
	// Resume lineage (M4.7): how many fresh-session continuations this task
	// has had, each recorded as a resumed:<session-id> tag.
	if n := taskResumedTagCount(t.Tags); n > 0 {
		fmt.Fprintf(&b, "resumed:        ×%d (see resumed:<session-id> tags above)\n", n)
	}
	fmt.Fprintf(&b, "source:         %s\n", orDash(t.Source))
	// M4.11 priority: 0 = legacy row predating the column (NULL) — show
	// the default tier marker rather than a bare 0 (which would read as
	// "highest").
	fmt.Fprintf(&b, "submitted_by:   %s\n", taskSubmittedBy(t))
	// Run provenance (internal/runcmd, devdoc/run-method-provenance.md):
	// HOW this execution was launched and the VERBATIM re-runnable command
	// line (an operator pastes run_cmd to reproduce the run; on a crashed
	// in_progress row it IS the resume recipe). The renderer is
	// runcmd.Field — the same one the run_cmd string is built with — so the
	// label spelling and the alignment live in one place.
	b.WriteString(runcmd.Field(runcmd.FieldRunMethod, t.RunMethod, taskShowLabelWidth))
	b.WriteString(runcmd.Field(runcmd.FieldRunCmd, t.RunCmd, taskShowLabelWidth))
	// task-update-reason epic: the operator-visible WHY of the row's current
	// status — stamped by `task update --reason` (or the status default on
	// a bare transition). Rendered when the column carries a value; a blank
	// column is the honest "no reason recorded yet" absence (pre-epic rows
	// and never-updated rows) rather than a synthetic line.
	if t.Reason != "" {
		// The reason is AUTHORED text (a run's own stderr tail, an operator's
		// note) — its continuation lines are quoted, never at column 0. The
		// first line rides the padded label (see taskShowIndentAfterFirst).
		fmt.Fprintf(&b, "reason:         %s\n", taskShowIndentAfterFirst(t.Reason))
	}
	// M4.8 deliverable closure: the structured deliverable blob currently
	// carries the repo (stamped at submit); branch/worktree are the
	// executor's backfill (M4.9 + bugfix epic 2026-09-03 — worktree now
	// stamped at CREATION time, not only at close-out, so a mid-run show
	// reveals where the task is actually working). Rendered field-wise —
	// an unparsable blob falls through to the raw string (forward compat).
	if t.Deliverable != "" {
		var deliv struct {
			Repo     string                  `json:"repo"`
			Branch   string                  `json:"branch"`
			Worktree string                  `json:"worktree"`
			Grounds  []taskDeliverableGround `json:"grounds"`
		}
		if err := json.Unmarshal([]byte(t.Deliverable), &deliv); err == nil && deliv.Repo != "" {
			fmt.Fprintf(&b, "%s %s\n", TaskShowDeliverableRepoLabel, deliv.Repo)
			if deliv.Branch != "" {
				fmt.Fprintf(&b, "%s %s\n", TaskShowDeliverableBranchLabel, deliv.Branch)
			}
			if deliv.Worktree != "" {
				fmt.Fprintf(&b, "%s %s\n", TaskShowDeliverableWorktreeLabel, deliv.Worktree)
			}
			// The SECONDARY grounds, one line each: the ground a run CREATED
			// that is not this row's own. Rendered from the recorded blob (never
			// re-derived), so the operator re-reads exactly what a lookup
			// matched — `for-branch` answers from this same axis.
			for _, g := range deliv.Grounds {
				fmt.Fprintf(&b, "%s %s\n", TaskShowDeliverableGroundsLabel, taskDeliverableGroundLine(g))
			}
		} else {
			fmt.Fprintf(&b, "deliverable:    %s\n", taskShowIndentAfterFirst(t.Deliverable))
		}
	}
	// READ-ONLY ADVISORY GROUND (epic 01a0bb4d follow-up, gap B): what the row
	// RECORDS above is the adoptable field — written only by seams that have
	// evidence, never re-pointed. A row whose ground exists but was never
	// recorded (the measured 2-of-131 in-flight shape) therefore shows no
	// ground at all, and the reader cannot tell that from a run with none.
	// This line resolves the ground NOW from the row's own session through the
	// EXISTING readers and verifies it, and it is ADVISORY by construction: it
	// never writes and never changes the recorded deliverable — the block above
	// is exactly what it was before the show (task_ground_advisory.go).
	b.WriteString(taskShowGroundAdvisory(t))
	// THE DELIVERY AXIS (epic 01a17e7b, half B): the ground line above answers
	// "where did the run work"; this one answers the question a signal-killed
	// run makes urgent — "did what it produced ever LEAVE". Read-only by
	// construction (no ledger write, no push, no change of delivery timing),
	// fail-safe unchecked, and silent for a row with no delivery promise
	// (non-terminal, or no branch recorded) — see
	// task_ground_delivery_reading.go.
	if line := taskShowDeliveryReading(t); line != "" {
		b.WriteString(line + "\n")
	}
	// THE MEASURED DELIVERY DEBT (epic 01a1a8df), BESIDE the reading above: the
	// reading asks the PEER what it holds; this asks THIS ROW'S OWN GROUND how
	// many commits it carries that no remote ref reaches — the fact that is
	// true even when no peer resolves at all. It renders only when there is an
	// actual debt (or the debt could not be measured), so a row that owes
	// nothing is unchanged (task_delivery_owed_reading.go).
	b.WriteString(taskShowDeliveryOwedReading(t))
	// Briefing text resolution is hoisted ABOVE the manifest block: the
	// false-claim marker below scans the same prose `task show` prints (the
	// payload column, else the session JSONL's first user message), so the
	// marker and the printed briefing can never disagree about the text. The
	// print itself stays where it always was — the block below is a pure
	// read, and an empty payload still costs the same single session read.
	briefingText := t.Payload
	if briefingText == "" {
		briefingText = taskBriefingFromSession(t.SessionID)
	}
	// Phase-1 session-dir attachments: render the manifest entries the
	// submit copied into the task session dir. One line per attachment —
	// [attachmentN] index (the EPIC-block label the run briefing injects),
	// the replica absolute path (readable via file-read), and the original
	// absolute path (the bidirectional mapping — which source file the
	// replica mirrors). A manifest present but unparsable falls through to
	// a marker line (the row's deliverable blob is the fallback renderer).
	//
	// POSITIONAL PAIRING CONTRACT (attachment-index): the index returned by
	// taskDeliverableAttachmentsRaw has one entry per manifest entry, in
	// manifest order (an empty replica_rel_path keeps its slot — see
	// TaskAttachmentReplicaMissingLabel), so atts[i] always pairs with
	// idx[i] and the line numbering matches TaskDeliverableAttachments'
	// replicaPaths. The length guard below is the no-silent-mislabel
	// backstop (Rule 15): a future face that shortens the index renders the
	// explicit missing label instead of attributing another entry's replica
	// to this attachment.
	showAtts, showIdx, showAttErr := taskDeliverableAttachmentsRaw(t.TaskID, t.Deliverable)
	if showAttErr == nil && len(showAtts) > 0 {
		aligned := len(showIdx) == len(showAtts)
		for i, a := range showAtts {
			n := i + 1
			replica := TaskAttachmentReplicaMissingLabel
			if aligned {
				replica = showIdx[i].Path
			}
			fmt.Fprintf(&b, "attachment%d:    %s\n", n, replica)
			fmt.Fprintf(&b, "  from:         %s (sha256 %s, %d bytes)\n", a.OriginalAbsPath, a.SHA256, a.FileSize)
		}
	} else if showAttErr != nil && t.Deliverable != "" {
		fmt.Fprintf(&b, "attachments:    (unparsable manifest: %v)\n", showAttErr)
	}
	// FALSE-CLAIM marker (internal/tools/task_attachment_claim.go): the row's
	// deliverable carries NO attachments manifest, yet the prose `task show`
	// prints (payload/briefing + summary) asserts that evidence IS attached.
	// Without this line the false claim is invisible — show printed nothing
	// where the [attachmentN:] block would be, so a fabricated evidence claim
	// was indistinguishable from a row that merely mentions a path (the
	// measured defect: task 01a0b53b, payload "(attached to the originating
	// session)" on a row with no manifest). Rule 15: the absence must be
	// LOUD, not silent.
	//
	// Fires ONLY when the manifest read succeeded and found nothing — an
	// unparsable manifest is a row that HAS a claim (rendered above) and
	// must not also be told "NONE". Always a warning, never a refusal: the
	// row stays legal and dispatchable.
	if len(showAtts) == 0 && showAttErr == nil {
		if phrase, ok := TaskAttachmentClaimScan(t.Summary, briefingText); ok {
			b.WriteString(TaskAttachmentClaimMarker(phrase))
			b.WriteString("\n")
		}
	}
	// P2.5: the verify verdict's evidence (the test command + output tail
	// the independent instance stamped on a verified writeback).
	if t.VerifyEvidence != "" {
		fmt.Fprintf(&b, "verify_evidence: %s\n", taskShowIndentAfterFirst(t.VerifyEvidence))
	}
	// Briefing display: the payload column is authored as the briefing on
	// purpose (TaskSubmitOptions.Payload docs). Auto-registered rows never
	// write a payload — their briefing lives in the first user message of
	// the session JSONL, so a code-name summary stays explainable at show
	// time instead of forcing a memory_search round-trip.
	if t.Payload != "" {
		fmt.Fprintf(&b, "briefing:\n%s\n", taskShowIndentLines(sanitizeTaskPayloadText(briefingText)))
	} else if briefingText != "" {
		fmt.Fprintf(&b, "briefing:       (from session JSONL)\n%s\n", taskShowIndentLines(briefingText))
	} else {
		fmt.Fprintf(&b, "briefing:       (none — summary is the briefing)\n")
	}
	// bug056: the run result / stats digest is NOT in the summary anymore
	// (summary is immutable). It lives in the append-only task_events
	// trail — show renders the latest digest event's detail as the result
	// line, so the operator still sees "gopi run completed — run: elapsed=…"
	// AND the close-out steps that follow it (align-before-delivery) do not
	// overwrite what the run recorded (see taskEventLatestDetail).
	ev, detail := taskEventLatest(ctx, db, t.TaskID)
	// THE RAW NEWEST EVENT IS ALSO A FALLBACK SLOT, and a transition record must
	// not land there either (epic 01a1a899-d05d): the fallback exists to render a
	// non-status event's detail when no digest was recorded, and a status
	// event's detail is now the three-element record — a LIFECYCLE fact, which
	// belongs on `transition:` below, never on the line the close-out chain and
	// Rule 31's ran-line check read as the run's result.
	if taskStatusEventIsTransitionDetail(detail) {
		detail = ""
	}
	if d := taskEventLatestDetail(ctx, db, t.TaskID); d != "" {
		detail = d
	} else if taskContentRevisionEventName(ev) {
		// A CONTENT REVISION is not a run digest, and this is the one place
		// that has to say so. Its detail is the old/new briefing pair under
		// a required --reason (TaskSetPayload) — rendering it on `result:`
		// would put a page of prose where the run's stats belong, and (the
		// mechanical half) `result:` is the line the close-out chain and
		// Rule 31's ran-line check READ, so a revision landing there would
		// hide the very digest the reader came for. The row's payload column
		// already carries the CURRENT value and the trail keeps the old one,
		// so nothing is lost by rendering neither here: the revision is
		// recorded, not announced.
		ev, detail = "", ""
	}
	// The trail's OTHER face: a judged verdict about this row (the merge-verify
	// gate's FAIL, a delivery refusal, a reframe ruling) is written under its
	// own event name and is not a run digest — taskEventLatestDetail skips it
	// by design, and before this line the skip left it invisible on the row it
	// ruled on. It renders on its OWN line so neither face displaces the other:
	// the digest keeps `result:` and the verdict is read where the row's status
	// is read. The newest non-status exception (an event that is neither a
	// digest nor a verdict) still renders as the result line, names its event,
	// exactly as before.
	verdictEvent, verdictDetail := taskEventLatestVerdict(ctx, db, t.TaskID)
	switch {
	case detail != "":
		fmt.Fprintf(&b, "result:         %s\n", taskShowIndentAfterFirst(detail))
	case ev != "" && !taskShowVerdictEventName(ev):
		fmt.Fprintf(&b, "result:         (%s)\n", ev)
	}
	// THE LIFECYCLE FACE (epic 01a1a899-d05d): who moved this row, from what
	// state to what state, on what grounds — the fact the ledger could not state
	// at all before this record existed (the detail was empty on every plain
	// transition). It renders on its OWN label rather than on `result:` for the
	// reason above: `result:` is the run digest's slot, and the close-out chain
	// and Rule 31's ran-line check read it. Three faces, three lines, none able
	// to displace another.
	//
	// A row whose newest status event predates the record renders
	// `who=(unrecorded, pre-<date> format)` — never an invented executor.
	if trEvent, trDetail := taskEventLatestTransition(ctx, db, t.TaskID); trDetail != "" {
		fmt.Fprintf(&b, "%-*s%s\n", taskShowLabelWidth, TaskShowTransitionLabel, taskShowIndentAfterFirst(trDetail))
		fmt.Fprintf(&b, "%-*s%s\n", taskShowLabelWidth, TaskShowTransitionNameLabel, trEvent)
	}
	if verdictDetail != "" {
		fmt.Fprintf(&b, "%-*s%s\n", taskShowLabelWidth, TaskShowVerdictLabel, taskShowIndentAfterFirst(verdictDetail))
		fmt.Fprintf(&b, "%-*s%s\n", taskShowLabelWidth, TaskShowVerdictNameLabel, verdictEvent)
	}
	// The trail's THIRD face, and the reason it is not folded into `verdict:`
	// (see deliveryOwedTrailEventNames): a recorded DELIVERY DEBT is a fact
	// about the row's deliverable, and the verdict selector keeps only the
	// NEWEST judgment — so rendering it there would let any later verdict
	// (routinely `reclaim-unreachable`, which the reclaim arm writes days
	// afterwards and promises never to revisit) take it off the row again.
	// Its own line makes it unshadowable, and no existing row's `verdict:` line
	// moves by a single byte: this arm only ADDS a label.
	if owedEvent, owedDetail := taskEventLatestDeliveryOwed(ctx, db, t.TaskID); owedDetail != "" {
		fmt.Fprintf(&b, "%-*s%s\n", taskShowLabelWidth, TaskShowDeliveryOwedLabel, taskShowIndentAfterFirst(owedDetail))
		fmt.Fprintf(&b, "%-*s%s\n", taskShowLabelWidth, TaskShowDeliveryOwedNameLabel, owedEvent)
	}
	// Truncation line (runend): an operator must be able to tell a run the
	// ENGINE cut short (wallclock cap / stuck net / iteration budget) from a
	// run the model finished — WITHOUT opening the session log. The marker is
	// on the result digest above already; this line says what it MEANS, right
	// where the status is read. The token is found in either slot the
	// close-out writes (the reason column, then the trail), so a row closed by
	// any writer reports what its own record carries.
	if tok := taskRunEndToken(ctx, db, t); tok != "" {
		fmt.Fprintf(&b, "truncated:      %s (%s) — %s\n", runend.TruncationMarker, tok, runend.TruncationExplain)
	}
	// Bounded task report (epic task-report): the run's human-readable story,
	// rendered right after the result digest it is recorded in. The block is
	// the FOURTH walk leg next to session_log/raw_log/task_output — with it,
	// "what did this task actually do?" is answered from the same output an
	// operator already reads, without opening a 10k-line jsonl. A row with no
	// report (pre-seam history / pruned directory) degrades to an explicit
	// line — never a silent absence and never a fabricated narrative.
	if block := TaskReportShowBlock(t.TaskID, t.CreatedAt, detail); block != "" {
		b.WriteString(block)
	}
	// Split tree (reframe TOO_BIG auto-enqueue): the roadmap children this
	// row decomposed into, in roadmap order — the operator's view of "was
	// the epic actually broken down, and is the work claimable?". One line
	// per child: id, status, summary. A parent with no children renders
	// nothing (no empty header noise on the common case).
	if kids, err := taskChildrenWithDB(ctx, db, t.TaskID); err == nil && len(kids) > 0 {
		fmt.Fprintf(&b, "children:       %d\n", len(kids))
		for _, k := range kids {
			fmt.Fprintf(&b, "  %s  [%s]  %s\n", k.TaskID, k.Status, sanitizeTaskSummary(k.Summary))
		}
	}
	if t.HasClaim {
		fmt.Fprintf(&b, "claimed_by:     %s (at %s)\n", t.ClaimedBy, formatTaskTime(t.ClaimedAt))
	}
	// THE CLAIM-LOCK AXIS: recorded only when the claim-lock protocol actually
	// ran, so the single-node shape (no peer declared) shows no extra line at
	// all — that is constraint 5, and it is why this is not rendered as
	// "(none)" the way the liveness stamps are. It answers the question the
	// status no longer answers: a row that reads `claimed` while another host
	// has not confirmed says so here, instead of claiming `in_progress` for a
	// process that does not exist.
	if t.LockPhase != "" {
		fmt.Fprintf(&b, "lock_phase:     %s\n", t.LockPhase)
	}
	// M4.5 liveness facts: on a mid-run (in_progress) row these say the
	// harness is actually receiving the model's bookkeeping — without
	// them a fresh created_at==updated_at row reads as "dead" when it is
	// merely un-transitioned. Rendered for EVERY in_progress row (with
	// "(none)" for not-yet-recorded stamps) so a mid-run snapshot is
	// self-explanatory: updated_at only moves on status transitions (it is
	// the M4.13 sync version anchor), so created_at==updated_at is NORMAL
	// while the run is alive — the liveness line is where "still running"
	// is visible. Terminal/pending rows render no liveness line (they are
	// not alive; a pending row has not started).
	if t.Status == taskStatusInProgress {
		// Tick-tock: a bare wall-clock stamp ("13:16:43") does not answer
		// "is this still running?" — the operator has to subtract it from
		// their own clock. Each stamp therefore carries its AGE against
		// ONE clock read and an explicit fresh/stale verdict measured on
		// the SAME stall grace the zombie sweep uses (taskTickTock), so
		// the line is a tick-tock reading, not a puzzle. `now` is a
		// display-local read for the age arithmetic only; its value is in
		// a separate heartbeat column.
		now := time.Now()
		b.WriteString(taskShowTickTockLine(t, now))
		// The process IDENTITY triple (epic task-process-identity): what the
		// row recorded about the process executing it, plus the live-side
		// check, so `task show` answers "is this row's holder real?" without
		// the operator reading /proc by hand. `gopi task status <id>` prints
		// the same facts with the definite verdict; this line is the record.
		fmt.Fprintf(&b, "process:        %s\n", taskShowIdentityLine(t))
	}
	fmt.Fprintf(&b, "created_at:     %s\n", formatTaskTime(t.CreatedAt))
	fmt.Fprintf(&b, "updated_at:     %s  (%s)\n", formatTaskTime(t.UpdatedAt), taskUpdatedAtAnchorNote)
	if t.HasEnded {
		fmt.Fprintf(&b, "ended_at:       %s\n", formatTaskTime(t.EndedAt))
	} else {
		b.WriteString("ended_at:       (not ended)\n")
	}
	return b.String(), nil
}

// orDash renders empty ledger fields as "—" so show output keeps its
// column alignment (an empty value is a meaningful absence, not a glitch).
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// taskTickTockClockFormat is the clock stamped once per liveness line so the
// ages on that line can be read against a KNOWN instant (the operator's own
// clock may differ from the host's).
const taskTickTockClockFormat = "15:04:05"

// taskTickTockUnknown / taskTickTockStale / taskTickTockFresh are the three
// verdict words a tick-tock stamp can carry, in ascending order of evidence:
// no fact recorded → the fact is older than the zombie sweep's own stall
// grace (that run would be swept as dead) → the fact is inside the grace.
const (
	taskTickTockUnknown = "unknown"
	taskTickTockStale   = "stale"
	taskTickTockFresh   = "fresh"
)

// taskTickTockAgeNegative guards the age arithmetic against clock skew
// (a stamp minted by another host's clock can sit in the future here): a
// negative age renders as zero, never as "-2m".
var taskTickTockAgeNegative = time.Duration(0)

// taskTickTock renders ONE liveness stamp as a tick-tock reading: the
// wall-clock form, its AGE against `now`, and an explicit verdict. A bare
// timestamp does not answer "is this run still alive?" — the reader has to
// subtract it from their own clock and then know the stall grace; both facts
// are supplied here so `task show` states the answer instead of posing the
// question.
//
// The grace is the SAME one the stale detector uses (taskStallGraceSec →
// config.ResolveTaskStallGrace, Rule 2 — one resolution for detection,
// listing and display), so a "stale" verdict here means exactly "the stale
// report would flag this row". A stamp equal to the row's start (the run
// has emitted nothing since it began) therefore reports its true age — hours
// old means stale, seconds old means fresh, with no special case invented.
//
// `present` false means the column is NULL: the fact has not been recorded
// (e.g. no tasklist block arrived this run) — an absence, rendered as such,
// never as a stale fact.
func taskTickTock(present bool, ts float64, now time.Time) string {
	if !present {
		return "(none — " + taskTickTockUnknown + ")"
	}
	if ts <= 0 {
		return "(unset — " + taskTickTockUnknown + ")"
	}
	stamp := time.Unix(int64(ts), 0)
	age := now.Sub(stamp)
	if age < taskTickTockAgeNegative {
		age = taskTickTockAgeNegative
	}
	graceSec, known := taskStallGraceSec()
	verdict := taskTickTockUnknown
	switch {
	case !known || graceSec <= 0:
		// No grace configured: the age is still shown, but "fresh vs stale"
		// would be invented — say so instead.
	case age > time.Duration(graceSec)*time.Second:
		verdict = taskTickTockStale
	default:
		verdict = taskTickTockFresh
	}
	return fmt.Sprintf("%s (age %s — %s)",
		stamp.Format(taskTickTockClockFormat), taskFormatDuration(age), verdict)
}

// taskUpdatedAtAnchorNote is the operator-facing note rendered beside the
// `updated_at` row of `task show`. updated_at is the M4.13 delta-sync version
// anchor: its writers are status transitions, claims, tags, deliverable,
// run-provenance, verify-evidence and the zombie re-arm bump — NO toolcall or
// agent-output path writes it. On a mid-run row it therefore stays at the
// row's first transition for hours while the run is perfectly alive, which is
// exactly the "updated_at suspicious / not updated since its first run" report
// this epic answers. Naming the column's real meaning on the surface that
// displays it is the operator-facing half of that finding; the tick-tock
// stamps on the liveness line are the heartbeat, this column is not one.
const taskUpdatedAtAnchorNote = "state/sync anchor — not a heartbeat"

// taskShowTickTockLine renders the `task show` liveness line: the two M4.5
// heartbeat columns as tick-tock readings plus the clock they were measured
// against. Extracted from the show renderer so the operator-facing answer
// ("is this row still running?") is unit-testable without building a ledger.
func taskShowTickTockLine(t *Task, now time.Time) string {
	return fmt.Sprintf("liveness:       %s=%s %s=%s  (clock %s)\n",
		taskLastToolcallAtColumn, taskTickTock(t.HasToolcall, t.LastToolcallAt, now),
		taskTasklistIssuedAtColumn, taskTickTock(t.HasTasklistIssue, t.TasklistIssuedAt, now),
		now.Format(taskTickTockClockFormat))
}

// taskAllowedTransitions is the P2 dispatcher state machine — the explicit
// migration table (the ~/work/tasks validTransitions pattern). The P2 loop:
// pending → claimed → in_progress → completed|failed, plus the recoveries
// a real queue needs: claimed reverts to pending (claim timeout/abandon),
// cancelled is terminal, and completed/failed judge onward to verified
// (M4.20-B: terminal rows never retry backwards — rework is REISSUED as a
// NEW pending row, append-only).
// P2.5 (devdoc/task-verify-loop.md) closes the acceptance loop: verified is
// reached ONLY from completed/failed (the verifier judges a delivered
// artifact; blocked is intentionally not a status). reopened is a judgment
// terminal — the rework itself is a NEW pending row (append-only ledger:
// history is never rewritten, the queue is extended instead).
// bugfix-task-p1: completed↔failed peer moves (rank-3 平级) are legal —
// the verify loop's FAIL verdict must land on a completed row as failed
// (a successor is offered), matching the M4.20-B design the sync layer already
// implements (completed→failed later wins on the rank ladder).
var taskAllowedTransitions = map[string][]string{
	taskStatusPending: {taskStatusClaimed, taskStatusInProgress, taskStatusCancelled, taskStatusSuperseded},
	// `claimed → terminal` is the EXEC-NEVER-HAPPENED edge (epic 01a1a87b).
	// It exists because `in_progress` now means "a child process was exec'd and
	// is being waited on": a run that fails BEFORE the exec, and the two
	// bookkeeping closers that never spawn a child at all (the merge executor's
	// claim-start, the merge-verify staleness guard's landed close), must be
	// able to reach a verdict WITHOUT asserting a process that does not exist.
	// The alternative — letting them write in_progress anyway — is exactly the
	// false claim this epic removes, one caller at a time.
	taskStatusClaimed:    {taskStatusInProgress, taskStatusCompleted, taskStatusFailed, taskStatusVerified, taskStatusCancelled, taskStatusSuperseded},
	taskStatusInProgress: {taskStatusCompleted, taskStatusFailed, taskStatusCancelled, taskStatusSuperseded},
	// Terminal-tier PEER moves (rank 3 平级, M4.20-B): completed↔failed are
	// the same lifecycle rank — the verify loop's FAIL verdict lands on a
	// completed row as failed (the judged artifact is reworked), and a
	// failed-labeled artifact a verifier accepts moves failed→verified.
	// Cancelled is excluded: an abandoned row never becomes a success/fail
	// verdict. Rank-tie 后到覆盖 governs the SYNC layer; the local gate
	// allows the peer move outright (the verdict IS the later write).
	taskStatusCompleted: {taskStatusVerified, taskStatusFailed},
	// `failed → in_progress` is the RESUME EDGE, and it exists because LEVEL 1
	// of the two-level policy is a resume, not a copy (internal/tools/
	// task_run_retry.go's header carries the ruling). A failure judges ONE
	// attempt; the work is unfinished, so the row may go back to running with
	// the SAME task id, the SAME session id and the SAME directory — which is
	// the whole point of resuming rather than minting `<id>-r1`. `pending` is
	// deliberately NOT on this list: re-queueing a failed row for a fresh
	// dispatcher would hand it a NEW run with no context, i.e. the copy the
	// operator ruled out as the default. A copy is `gopi task clone`, an
	// explicit LEVEL-2 act.
	//
	// THE SYNC LAYER DOES NOT FOLLOW, AND MUST NOT (see the resume-edge arm in
	// memory_sync_delta.go): taskStatusRank below keeps failed at the terminal
	// rank (3), which is a decision about CONVERGENCE, not about legality here.
	// A peer that never saw this resume still reports failed at rank 3 — the
	// ladder would pull this row FORWARD onto a rank it already left, turning a
	// live local run back into a failure. The rank table is untouched; the sync
	// arm identifies this ONE edge (local in_progress + remote failed) and keeps
	// the local resume. NO CLOCK IS CONSULTED for it on purpose — "the newer
	// write wins" cannot decide a case where the two sides are describing two
	// different attempts, so the edge is decided by SHAPE and the count is
	// reported (RowsResumeKept) rather than silently absorbed.
	taskStatusFailed:    {taskStatusInProgress, taskStatusVerified, taskStatusCompleted},
	taskStatusCancelled: {},
	taskStatusVerified:  {},
	// superseded sits exactly where cancelled sits — reachable from any LIVE
	// status, leaving to nothing — and differs ONLY in the dependency gate's
	// release set (taskParentReleaseStatuses): the row's work is carried by a
	// successor, so the children it was blocking may run. It is deliberately
	// NOT reachable from a terminal status: a row that already reached a
	// verdict is not re-labeled (append-only), and completed/verified are
	// already release members.
	taskStatusSuperseded: {},
	// M4.20-B: reopened is retired — no transition targets it (reissue =
	// new pending row, the old row stays terminal) and nothing leaves it
	// (legacy rows are read-only history; a legacy reopened row that must
	// run again gets a reissue row, never a status flip).
}

// statusRank is the M4.20-B lifecycle ladder (核心裁决件): the ORDINAL of
// every status on the forward-only path. Sync adjudication converges both
// sides to the LARGER rank — never regress — replacing updated_at clock
// comparison for in-row status merges (clocks stay only for the pull
// window: anchors / AfterSource, and for terminal-tier tiebreaks).
//
//	pending(0) → claimed(1) → in_progress(2) → terminal-tier(3) → verified(4)
//
// The terminal tier is deliberately ONE rank: completed/failed/cancelled
// are peers (后到覆盖 — the later write wins a tie), so the old
// equal-anchor conflict residue between terminal statuses disappears;
// verified outranks them all (the verify loop's upgrade).
var taskStatusRank = map[string]int{
	taskStatusPending:    0,
	taskStatusClaimed:    1,
	taskStatusInProgress: 2,
	taskStatusCompleted:  3,
	taskStatusFailed:     3,
	taskStatusCancelled:  3,
	taskStatusSuperseded: 3,
	taskStatusVerified:   4,
	// legacy reopened reads as pending-rank: it is a rework marker, and
	// any forward state on a peer outranks it (rank-0 mirror semantics).
	taskStatusReopened: 0,
}

// statusRankOf returns the lifecycle rank of s (unknown/legacy statuses
// rank as pending — the most conservative convergence target).
func statusRankOf(s string) int {
	if r, ok := taskStatusRank[s]; ok {
		return r
	}
	return taskStatusRank[taskStatusPending]
}

// taskTerminalTier reports whether s sits on the terminal tier (rank 3:
// completed/failed/cancelled are peers — 后到覆盖 applies within the tier).
func taskTerminalTier(s string) bool {
	return statusRankOf(s) == taskStatusRank[taskStatusCompleted]
}

// taskTransitionAllowed reports whether status → next is on the migration
// table. Same-status updates are allowed as no-ops (idempotent writeback).
func taskTransitionAllowed(status, next string) bool {
	if status == next {
		return true
	}
	for _, v := range taskAllowedTransitions[status] {
		if v == next {
			return true
		}
	}
	return false
}

// RecordRunEnd writes the run's END REASON onto its ledger row as an
// append-only task_events NOTE (event runend.EventName) — the write that makes
// a truncation survive to a reader who is not the writer.
//
// WHO CALLS IT: nothing in production today, and that is recorded rather than
// hidden (task 01a16f7c). Its one caller was the child-side run close-out
// (internal/agent's closeOutRunRow), deleted together with the pid-liveness
// verdict it fed: the wrapper-child note existed so the child's REFUSED
// close-out could still tell the wrapper's post-Wait writeback that the run was
// cut short. With the close-out gone, a `task run` / `worker` child that the
// engine truncated reaches the wrapper's writeback through the WRAPPER's own
// read of the child's result (tools.RunEndReasonFor below also reads the
// row's reason column), and the auto-row arm writes the reason itself. The
// writer is kept because the ledger FORMAT it writes (an append-only runend
// note) is still read by RunEndReasonFor for every row; retiring the writer is
// its own decision with its own evidence, and guessing at it here would delete
// a documented half of the read path (see devdoc/run-closeout-boundary.md).
//
// A finished run writes nothing, and the caller is expected to reach here only
// for a truncation: an ordinary run's trail is unchanged.
func RecordRunEnd(taskID string, reason runend.Reason) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" || !reason.Truncated() {
		return
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		// Never silent (Rule 15): the caller (a run's end hook) must be able to
		// see that its truncation did not reach the ledger.
		taskNotifyLog("run end record for %s failed: %v", taskID, err)
		return
	}
	defer db.Close()
	// A no-status event, so every reader of the trail sees a note rather than
	// a transition (taskStatus defaults are untouched by it).
	taskEventWrite(ctx, db, taskID, runend.EventName, reason.Detail())
}

// DeliveryDidNotLandEventName is the task_events event under which a bare-prompt
// run's UNSUCCESSFUL clone-channel delivery leg records its outcome, and
// DeliveryDidNotLandReasonPrefix is the ONE sentence the row's reason column
// carries for the same fact (Rules 1b/14: the writer and every test reference
// these constants, never a re-typed literal).
//
// WHY THE ROW NEEDS IT AT ALL (epic raw-prompt-clone-channel-delivery-leg): the
// agent's end hook closes the row `completed` the moment the session ends, so a
// delivery that was blocked (a step refused) or broken (not about the work at
// all) would otherwise leave `completed` standing while the run's commits sit in
// a workspace nobody merges. The reason column is the ledger's WHY slot and the
// trail is what `task show` renders, which is why both are written and neither
// is inferred from stderr (a channel belonging to a process that has exited).
const (
	DeliveryDidNotLandEventName  = "clone-delivery-not-landed"
	DeliveryLandedEventName      = "clone-delivery-landed"
	DeliveryDidNotLandReasonPref = devworkspace.CloneChannelPrefix + "delivery did not land — "
)

// AppendTaskTrailEvent appends ONE consultation-shaped row to a task's
// append-only `task_events` trail — a note, never a status transition (the
// event name is the caller's, and nothing here touches the tasks row).
//
// It is the exported door for a caller OUTSIDE this package that must leave an
// audit record without moving a row's status: the agent's clone-delivery leg
// records its outcome AFTER the end hook has already closed the row, so a
// transition door would either be a no-op or fight the status machine.
// Best-effort like every trail write in this file — a failure is REPORTED
// through the notify log (Rule 15) and never rolls anything back, because the
// ledger row itself already stands.
func AppendTaskTrailEvent(taskID, event, detail string) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" || strings.TrimSpace(event) == "" {
		taskNotifyLog("task_events append skipped: empty task id or event name")
		return
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("task_events append for %s (%s) failed: %v", taskID, event, err)
		return
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, event, detail)
}

// RunEndReasonFor reports why the row's run stopped, as the row itself
// records it — the truncation token written by the run's close-out (the
// agent's end hook) or runend.Finished for a row that carries no truncation.
//
// It is the SECOND door to the same fact (`gopi task show` reads it directly
// too): the CLI's wrapper writeback has to decide a task-run row's status
// AFTER the child exited, and it must agree with what the child recorded
// rather than re-derive it from the exit code — which is precisely how a run
// the wallclock cap cut short used to close `completed`.
//
// Two slots are consulted because both are written by the same call and either
// can be the one that survived a partial history: the tasks.reason column
// first (the current transition's WHY), then the newest task_events detail
// (the append-only trail — a row closed before this vocabulary existed has no
// reason token, and a row re-closed later carries its newest event).
func RunEndReasonFor(taskID string) runend.Reason {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return runend.Finished
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		// Never silent (Rule 15): an unreadable ledger means the caller cannot
		// see a truncation, and it must be able to say so.
		taskNotifyLog("run end reason for %s: ledger unreadable: %v", taskID, err)
		return runend.Finished
	}
	defer db.Close()
	if t, err := taskByID(ctx, db, taskID); err == nil && t != nil {
		if tok := runend.TokenIn(t.Reason); tok != "" {
			return runend.Reason(tok)
		}
	} else if err != nil {
		taskNotifyLog("run end reason for %s: read: %v", taskID, err)
	}
	_, detail := taskEventLatest(ctx, db, taskID)
	if tok := runend.TokenIn(detail); tok != "" {
		return runend.Reason(tok)
	}
	return runend.Finished
}

// taskRunEndToken is the SHOW-side resolver: the row's truncation token, read
// from the same two slots RunEndReasonFor walks (reason column, then newest
// task_events detail) but through the db handle `show` already holds — one
// connection per render, and the ordering rule (the current transition's WHY
// before the trail) stays in one place: both callers delegate to runend.TokenIn
// over the SAME marker text.
func taskRunEndToken(ctx context.Context, db *sql.DB, t *Task) string {
	if t != nil {
		if tok := runend.TokenIn(t.Reason); tok != "" {
			return tok
		}
	}
	if t == nil {
		return ""
	}
	_, detail := taskEventLatest(ctx, db, t.TaskID)
	return runend.TokenIn(detail)
}

// taskTransitionAdvice renders the operator-facing tail of an illegal
// transition refusal. Two defects it closes (both measured, task
// fix/task-update-error-message — the operator read the pair of messages
// below as "the same command failed, then succeeded" and filed a bug):
//
//  1. an EMPTY allow-list rendered as "(allowed from cancelled: )" — a blank
//     where the answer should be. A terminal status is terminal; say so.
//  2. "reopened" is offered by NO surface that a --status write can honour
//     (taskAllowedTransitions has no reopened key, so Reopened→anything is
//     refused one step later). The invalid-status list therefore no longer
//     offers it — see taskStatusListForCLI — and a retired-status target
//     carries the M4.20-B reissue advice (it had one already when the ROW
//     was reopened; status == reopened reaches this same branch).
func taskTransitionAdvice(from, to string) string {
	if to == taskStatusReopened || from == taskStatusReopened {
		return retiredReopenedStatusHint
	}
	allowed := taskAllowedTransitions[from]
	if len(allowed) == 0 {
		return fmt.Sprintf(" — %s is TERMINAL: no status write leaves it (a rejected/unfinished row is repaired by REISSUE: submit a NEW task and leave this row as history)", from)
	}
	return " (allowed from " + from + ": " + strings.Join(allowed, "|") + ")"
}

// retiredReopenedStatusHint is the M4.20-B advice for the retired `reopened`
// status, in ONE place: the illegal-transition refusal and the invalid-status
// refusal (when the token IS a retired status) both carry it.
const retiredReopenedStatusHint = " — reopened is retired (M4.20-B): continue the row with `gopi session resume`, or copy it into a clean room with `gopi task clone`, and leave the judged row terminal"

// statusBytesNear reports whether two status spellings are one edit apart —
// a DELETION, INSERTION, SUBSTITUTION or TRANSPOSITION of a single byte.
// This is what turns "canceled" (the American spelling, and the word in the
// operator's own report) into an actionable refusal instead of a bare list,
// with no dependency and no proximity heuristic beyond the distance itself.
func statusBytesNear(a, b string) bool {
	if a == b {
		return false
	}
	if la, lb := len(a), len(b); la-lb > 1 || lb-la > 1 {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	// |len(a)-len(b)| <= 1, a is the shorter (or equal) string.
	if len(a) == len(b) {
		diff := make([]int, 0, 2)
		for i := 0; i < len(a); i++ {
			if a[i] != b[i] {
				diff = append(diff, i)
			}
		}
		switch len(diff) {
		case 0:
			return false
		case 1:
			return true
		case 2:
			return diff[1] == diff[0]+1 && a[diff[0]] == b[diff[1]] && a[diff[1]] == b[diff[0]]
		}
		return false
	}
	// One insertion in b (a is a subsequence of b with exactly one skip).
	i, j, skips := 0, 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			i++
			j++
			continue
		}
		skips++
		if skips > 1 {
			return false
		}
		j++
	}
	return true
}

// taskStatusNearMiss returns the WRITABLE status closest to an unknown token
// ("" when none is within one edit) — the "did you mean" half of the
// invalid-status refusal. Compared against taskStatusWritable, never against
// the full CHECK set: the retired `reopened` must never be the suggestion,
// or the remedy would be a second refusal.
func taskStatusNearMiss(unknown string) string {
	unknown = strings.ToLower(strings.TrimSpace(unknown))
	for _, s := range taskStatusWritable {
		if statusBytesNear(unknown, s) {
			return s
		}
	}
	return ""
}

// invalidStatusRefusal is the ONE invalid-status error message for a
// `--status` write and the idempotent `task list --status` filter alike.
// acceptable is the caller's list — the WRITABLE set for a write, plus the
// retired token for a read-only filter (taskStatusListForCLI); a one-edit
// near miss is named outright.
func invalidStatusRefusal(unknown string, acceptable []string) string {
	msg := fmt.Sprintf("task: invalid status %q (want one of %s)",
		unknown, strings.Join(acceptable, "|"))
	if near := taskStatusNearMiss(unknown); near != "" {
		msg += fmt.Sprintf(" — did you mean %q?", near)
	}
	if strings.EqualFold(strings.TrimSpace(unknown), taskStatusReopened) {
		msg += retiredReopenedStatusHint
	}
	return msg
}

// TaskUpdate edits a ledger record's STATUS — the only mutable face since
// M4.5 (task content is immutable: "cancel and re-submit" is the amendment
// path for a wrong summary). A status change to a terminal value stamps
// ended_at; a move back to in_progress clears it. Status changes are gated
// by the dispatcher state machine (taskAllowedTransitions) — an illegal
// transition is an error naming the allowed moves, never a silent rewrite.
// Missing tasks error — and there is NO delete path (append-only ledger).
// The harness-only stats append lives in TaskUpdateWithResult below.
func TaskUpdate(taskID, status string) (string, error) {
	return taskUpdate(taskID, status, "", "")
}

// TaskUpdateWithResult is the HARNESS-ONLY status write that records a
// result digest (task run's writeback, the auto-register end hook's run
// stats). bug056: the result NO LONGER overwrites the immutable summary —
// it is appended as a task_events row (the append-only audit trail), and
// show renders the latest event's detail as its `result:` line. The
// summary column keeps the submit-time payload digest untouched. The
// callers (TaskRunWriteback, taskregister.go, and the CLI update verb's
// --result flag — bugfix-task-p1 restores the CLI face for the verify
// loop's verdict evidence) pass the FULL digest they want recorded — it
// lands in task_events.detail, not tasks.summary.
func TaskUpdateWithResult(taskID, status, newSummary string) (string, error) {
	taskID = taskIDNormalize(taskID)
	status = strings.TrimSpace(status)
	if strings.TrimSpace(newSummary) == "" {
		return "", fmt.Errorf("task: TaskUpdateWithResult requires the new summary (plain TaskUpdate is status-only)")
	}
	if status == "" {
		return "", fmt.Errorf("task: nothing to update (set --status; task content is immutable — cancel and re-submit)")
	}
	// Model backfill (task-submit-model epic ②): a row whose model column
	// is still empty gets the EXECUTING session's model stamped here. The
	// child gopi exports its session id via GOPI_EXECUTOR_SESSION_ID
	// (printLogPaths); the writeback resolves that session's model from its
	// session_start event. The early stamp (bug054) already backfills BOTH
	// session_id and model at child-start; this deferred path is the
	// fallback for writebacks that never saw the early stamp (e.g. auto
	// rows whose session resolved only after the run) — the guarded
	// UPDATE is a no-op when the columns are already filled.
	if m := taskExecutorSessionModel(); m != "" {
		if err := taskBackfillModel(taskID, m); err != nil {
			taskNotifyLog("model backfill for %s: %v", taskID, err)
		}
	}
	// (The deferred session_id stamp is NOT here: it lives in taskUpdate's
	// state gate — the ONE write every door shares, which is also the only
	// place it can serve `task update --status` and the merger's writes. See
	// taskRunSessionFromRowDir.)
	return taskUpdate(taskID, status, newSummary, "")
}

// TaskUpdateReason is the CLI `task update --reason` face: a status move
// that records the operator-visible WHY. A blank reason resolves to the
// status-specific default (taskDefaultReason — the lazy caller still gets a
// meaning); a supplied reason is written verbatim. Same-status no-op updates
// never stamp a default over an existing reason (only an explicit one).
// The optional digest ("" = none) rides task_events like TaskUpdateWithResult.
func TaskUpdateReason(taskID, status, reason, digest string) (string, error) {
	taskID = taskIDNormalize(taskID)
	status = strings.TrimSpace(status)
	if status == "" {
		return "", fmt.Errorf("task: nothing to update (set --status; task content is immutable — cancel and re-submit)")
	}
	return taskUpdate(taskID, status, digest, reason)
}

// TaskUpdateReasonForced is TaskUpdateReason with the operator's delivery-proof
// escape (`gopi task update --force`) switched on: the same write, the same
// reason/digest handling, and the release reaches the ONE decision point in
// deliveryProofRefusal — never the dirty/residue/tip gates. Kept as its own
// entry point (rather than a fifth parameter on TaskUpdateReason) so every
// existing caller is unable to pass it by accident: the escape is the
// operator's typed flag, and nothing inherits it.
func TaskUpdateReasonForced(taskID, status, reason, digest string, deliveryForce bool) (string, error) {
	taskID = taskIDNormalize(taskID)
	status = strings.TrimSpace(status)
	if status == "" {
		return "", fmt.Errorf("task: nothing to update (set --status; task content is immutable — cancel and re-submit)")
	}
	return taskUpdateDeliveryForced(taskID, status, digest, reason, deliveryForce)
}

// PayloadRevisionEventName is the event name under which a payload revision
// is recorded in the append-only task_events trail (the CLI face
// `gopi task update <id> --payload TEXT|@FILE`, epic 01a0fea1 item [2/5]).
// The ROW's payload column holds the CURRENT briefing (that is what
// taskRunBriefing hands the child); the trail holds every value it has ever
// had, so an audit reader can always recover the previous briefing. One
// spelling, referenced by the writer and by the tests (Rules 1b/13).
//
// It is deliberately NOT a member of verdictTrailEventNames: a revision is
// neither a judgment ON the row nor a run digest, so `task show` must not
// render it on the `result:` or the verdict line.
const PayloadRevisionEventName = "payload_revised"

// ReparentEventName is the event name under which a TREE-EDGE move is
// recorded in the append-only task_events trail (the CLI face
// `gopi task update <id> --parent-task-id X --reason WHY` / `--clear-parent
// --reason WHY`). The ROW's parent_task_id column holds the edge that is
// currently in force; the trail holds every edge the row has ever carried, so
// a reader asking "who was this row's producer when it ran?" can always
// recover the previous answer — the same contract PayloadRevisionEventName
// gives the briefing.
//
// It is deliberately NOT a member of verdictTrailEventNames (an edge move is
// neither a judgment ON the row nor a run digest); it IS a member of
// contentRevisionEventNames, for the same reason the payload revision is: the
// detail carries whole values (old edge + new edge + reason) and rendering
// that on `task show`'s `result:` line would bury the run digest.
const ReparentEventName = "reparented"

// taskParentMissingFormat is the ONE spelling of "the parent id you named is
// not in the ledger", shared by the two faces that validate it: TaskSubmit
// (which prefixes it with "task_submit: ") and TaskReparent (which prefixes it
// with "task update: "). Rule 14 — a caller that greps for the refusal must
// find the same sentence whichever door it came through, and the two faces
// must not drift on the wording.
const taskParentMissingFormat = "parent_task_id %q does not exist"

// taskReparentWalkMax bounds the upward walk the cycle check performs. A
// ledger edge chain is a few links deep by construction (a split tree, a
// linear dependency chain); the cap exists so a ledger that ALREADY contains a
// cycle — written by an older binary, a peer replica, or direct surgery —
// terminates the walk with a refusal instead of spinning. The walk's own
// visited-set is the primary guard; this is the backstop for a chain longer
// than any tree this ledger can legitimately hold, and hitting it is reported,
// never treated as "no cycle found" (Rule 15).
const taskReparentWalkMax = 512

// TaskSetPayload replaces a ledger row's payload column — the briefing the
// dispatcher hands the executing instance (TaskSubmitOptions.Payload docs).
// It exists because a wrong or stale briefing otherwise had exactly one
// remedy: cancel the row and re-submit it, which mints a new task id and
// breaks every reference to the old one (epic 01a0fea1 item [2/5];
// devdoc/task-payload-revision.md is the design record — what was opened,
// what was deliberately left immutable, and the measured FTS/refusal evidence).
//
// THE WRITE IS an INSERT plus ONE targeted UPDATE, both in a single
// transaction, and the order is the point:
//
//  1. task_events gets a NEW row (event=payload_revised) carrying the OLD
//     and the NEW value with their sha256s and byte counts, plus the actor
//     and the operator's --reason. This is the audit half: the ledger is
//     append-only, so nothing an operator types can erase a briefing that
//     was once in force.
//  2. tasks.payload is updated WHERE task_id = ?. A row that does not exist
//     matches no row and the whole transaction rolls back — "the row must
//     exist before it can be edited", which is what keeps this door from
//     resurrecting a deleted id (there is no delete path, and this is not
//     one either: no row is ever removed).
//
// The UPDATE touches ONE column — payload — plus updated_at. It is
// deliberately NOT routed through taskUpdateStampedFrom: that function's
// contract is a STATUS transition (state machine, ended_at, claim trail,
// notify bus), and a revision must change none of those. (This list used to
// end with "verify-closure enqueue"; that leg was deleted with the
// self-verification gate on 2026-10-25 — see
// devdoc/no-self-verification-gate.md — so naming it here would describe a
// side effect this ledger no longer has.)
//
// `tasks_au` then maintains tasks_fts/tasks_tri because the payload
// column IS indexed (tasksFTSColumns), so a search for the new briefing
// finds the row and a search for the old one no longer does — the FTS index
// is not a second source of truth here, it is the same one, refreshed by the
// trigger the update fires.
//
// NOT a sync path: nothing in this function (or its callers) reads or writes
// a peer, a sync anchor or a conflict record — internal/tools/memory_sync_*
// is not imported or called from here (epic 01a0dd9d item [3/5] narrows the
// identity judgment to task_id(UUID) + summary; until that lands, a multi-
// node profile may judge a payload edit as a content collision — single-node
// profiles, sync_peers = [], never reach that judgment at all).
//
// An empty payload is REFUSED by name. Two reasons, both measured: the
// payload is the row's ONLY briefing, so an empty one makes the row
// invisible to every dispatcher (bug028's payload guard in TaskNextFiltered
// — `gopi task submit` mirrors the positional summary into it for exactly
// this reason), and memory_sync_delta's real-ticket guard reads "payload
// non-empty" as "this is a real ticket that must not be flattened". A
// revision that blanks the column would silently take the row out of the
// queue and out of that guard; the caller who really wants the row to stop
// being dispatchable has `--status cancelled`, which says so.
func TaskSetPayload(taskID, payload, reason string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: task_id must not be empty")
	}
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", fmt.Errorf("task: %s: refusing to blank the payload column — the payload IS the row's briefing (an empty one is invisible to every dispatcher; bug028). Use `--status cancelled` to retire the row, or pass the new briefing text.", taskID)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf("task: %s: a payload revision must carry --reason — the trail records WHY the briefing changed, and an unexplained revision of an audit row is what the ledger's append-only rule exists to prevent.", taskID)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("task: payload: begin: %w", err)
	}
	// The rollback is the defer's whole job: once Commit below has succeeded it
	// is a documented no-op, and a rollback failure on the error paths would
	// replace the real cause with a teardown detail nobody can act on.
	defer func() { _ = tx.Rollback() }() //stylegate:ignore the commit below owns the verdict; a rollback failure would mask the error being returned

	// The existence probe and the audit write are the SAME read: a missing
	// row must leave no trail entry behind, and a present one must be
	// recorded BEFORE its column moves (a crash between the two steps is
	// impossible — one transaction — but the ORDER is still the contract:
	// the value being replaced is captured from the row that still holds it).
	cur, err := taskByIDTx(ctx, tx, taskID)
	if err != nil {
		return "", fmt.Errorf("task: payload: %w", err)
	}
	if cur == nil {
		return "", fmt.Errorf("task: no task with task_id=%q", taskID)
	}
	old := cur.Payload
	if old == payload {
		// Idempotent: re-sending the briefing a row already carries is not a
		// revision. Nothing is written (no trail row either) and the caller
		// is told so rather than shown a diff-shaped success.
		return fmt.Sprintf("Task payload unchanged: %s (already %d bytes)", taskID, len(payload)), nil
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO task_events (task_id, event, detail, created_at) VALUES (?, ?, ?, ?)`,
		taskID, PayloadRevisionEventName, payloadRevisionDetail(old, payload, reason), float64(time.Now().Unix())); err != nil {
		return "", fmt.Errorf("task: payload: audit trail: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE tasks SET payload = ?, updated_at = ? WHERE task_id = ?`,
		payload, float64(time.Now().Unix()), taskID)
	if err != nil {
		return "", fmt.Errorf("task: payload: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("task: payload: %w", err)
	}
	if n == 0 {
		// The row vanished between the read and the write (the ledger has no
		// delete path, so this is a foreign writer or a corrupted profile):
		// refusing rolls the trail row back with it (Rule 15 — no silent
		// claim of a revision that did not land).
		return "", fmt.Errorf("task: %s: payload revision matched no row — nothing was written (the trail entry is rolled back with it)", taskID)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("task: payload: commit: %w", err)
	}
	return fmt.Sprintf("Task payload updated: %s (payload=%d bytes, was %d)", taskID, len(payload), len(old)), nil
}

// payloadRevisionDetail renders the append-only trail entry for one payload
// revision: the reason, then both values in full, each preceded by the
// sha256 and byte count that make a later comparison (and the operator's
// claim "I only changed one line") checkable without re-reading the trail
// entry's bytes. The values are included VERBATIM — an audit record that
// summarizes the thing it audits is not an audit record.
//
// No cap is applied to the recorded text (the detail column is TEXT and this
// is the row's own history; the payload itself is capped at submit time by
// taskPayloadInlineMaxBytes, which spills an oversized briefing into an
// attachment and leaves a pointer here).
func payloadRevisionDetail(oldPayload, newPayload, reason string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "reason: %s\n", reason)
	fmt.Fprintf(&b, "old (sha256 %s, %d bytes):\n%s\n", payloadRevisionDigest(oldPayload), len(oldPayload), oldPayload)
	fmt.Fprintf(&b, "new (sha256 %s, %d bytes):\n%s", payloadRevisionDigest(newPayload), len(newPayload), newPayload)
	return b.String()
}

// payloadRevisionDigest is the sha256 of one payload value, rendered the way
// every other digest in this package is (hex, lowercase). An empty value is
// hashed as the empty string rather than reported as "-": the digest of ""
// is itself a fact, and a reader comparing it to the empty-string constant
// is doing the right arithmetic.
func payloadRevisionDigest(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// TaskReparent moves a ledger row's dependency EDGE — the parent_task_id that
// Rule 33's gate reads — to a new producer, or clears it entirely. It is the
// write path the ledger was missing: before it, parent_task_id could be set
// ONLY at INSERT (TaskNew/taskCreateFields is its single writer, the generic
// UPDATE set-list carries no parent column), so an edge decided at creation
// time could never be repaired, and the only remedy for a wrong edge was
// `cancel and re-submit`, which mints a new task id and breaks every
// reference to the old one.
//
// WHAT IT DOES NOT CHANGE (the whole point of the narrow shape): the gate's
// RELEASE SET {completed, verified} (taskParentReleaseStatuses), the
// no-override / no-cascade / no-timeout rules, TaskNext's selection, and the
// row's status or summary. This function writes ONE column — parent_task_id —
// plus updated_at. It is deliberately NOT routed through
// taskUpdateStampedFrom, whose contract is a STATUS transition (state machine,
// ended_at, claim trail, notify bus — the verify-closure enqueue leg that used
// to sit there was deleted 2026-10-25, devdoc/no-self-verification-gate.md):
// moving an edge must consume no transition, which is exactly what makes
// `--parent-task-id X --status Y` mean "set the edge, then transition" rather
// than "the edge move ate the transition".
//
// THE VALIDATION, in the order it runs — all four arms are REFUSALS with zero
// writes, and none of them is pre-empted by the caller (Rule 14: one
// definition, one message):
//
//  1. EMPTY row id → refuse ("task_id must not be empty").
//  2. MISSING row → refuse ("no task with task_id=%q"). There is no delete
//     path in this ledger and this is not one either: no row is ever removed,
//     so addressing a ghost is a typo, not an instruction.
//  3. MISSING --reason → refuse. This is an append-only audit ledger and the
//     trail records WHY the edge moved; an unexplained rewrite of a row's
//     production dependency is what the append-only rule exists to prevent.
//     Same rule and same shape as TaskSetPayload's reason requirement.
//  4. SELF-REFERENCE (parentID == taskID) → refuse. A row its own producer is
//     a one-node cycle: the gate would park it forever (its parent's status is
//     read from the row itself, in_progress, never in the release set). Named
//     separately from the walk below because the walk would report it as "the
//     target already depends on this row" — true, and less legible.
//  5. GHOST PARENT (the named parent names no row) → refuse with the SAME
//     sentence TaskSubmit uses (taskParentMissingFormat, "parent_task_id %q
//     does not exist"). An edge to nothing is the same unnavigable split tree
//     whether it is created at insert time or moved afterwards, and a caller
//     grepping for that refusal must find it through both doors.
//  6. CYCLE → refuse, naming the RING: the walk climbs from the target parent
//     through parent_task_id links; if it reaches the row being moved, the
//     move would close a cycle and the refusal renders the path it took. Rule
//     33's gate reasons about a DAG; an edge that closes a ring would park
//     every row on it — including the row being moved, for good.
//
// THE WRITE is an INSERT plus ONE CONDITIONAL UPDATE, both inside ONE
// transaction, and the CAS is the point:
//
//	the UPDATE's WHERE clause pins the row's OBSERVED parent_task_id (the
//	value the validations above were computed against) and its id. A
//	concurrent writer that moved the same edge between the read and the write
//	makes the UPDATE match NOTHING, the transaction rolls back with the trail
//	entry, and the caller is told the row moved under it — instead of the
//	read-modify-write race in which the loser's stale cycle check certifies an
//	edge derived from a tree that no longer exists. RowsAffected == 0 is
//	never swallowed (Rule 15).
//
// The cycle walk shares the UPDATE's transaction and connection deliberately:
// reading the chain through a SECOND connection would not see this
// transaction's uncommitted writes, and — more importantly — the two
// concurrent movers must be serialized by the SAME write lock. openTasksDB
// opens with _txlock=immediate, so the BEGIN takes the write lock up front and
// the second mover queues on busy_timeout before it can read a stale chain.
// The walk itself is read-only over rows this transaction has not touched, so
// the lock costs it nothing.
//
// SETTING the edge is idempotent: re-parenting onto the edge the row already
// carries writes nothing (no trail entry either) and says so, the same shape
// TaskSetPayload uses for an unchanged payload. CLEARING is expressed by
// passing an empty parentID — the same call, not a second function: the
// operation is "make the observed column this value", and "" is a value.
//
// NOT a sync path: nothing here reads or writes a peer, a sync anchor or a
// conflict record. The exported row shape is SyncTaskRow, which carries NO
// parent column at all (M4.13 delta + the harvest/mirror legs are insert-only
// for parent_task_id), so an edge moved here is not forwarded by the delta
// transport — see devdoc/task-reparent.md for the measured boundary and what
// that means for a multi-node profile.
func TaskReparent(taskID, parentID, reason string) (string, error) {
	return TaskReparentParents(taskID, []string{parentID}, reason)
}

// TaskReparentParents is the multi-parent face of TaskReparent (EPIC
// multi-parent): it replaces the row's WHOLE parent set with parentIDs, whose
// first element lands in parent_task_id and the rest in parent_task_ids — the
// same split TaskSubmit performs (taskParentsForWrite), so a row's parents read
// back identically however they were written.
//
// An EMPTY parentIDs clears every parent — the CLI's `--clear-parent`, which
// clears the LIST now that a row can carry more than one. Every named parent is
// validated exactly as the single-parent face validated its one: self-reference,
// existence (with the SHARED sentence, taskParentMissingFormat), and the cycle
// walk, which is run for EACH new parent and, in the loaded edit arms, from the
// row being moved. The cycle walk is NOT a DAG feature of the gate (Rule 33 is
// unchanged, one AND over the stated parents); it is the pre-existing refusal
// that stops an edit from creating a ring the gate would park forever, and it
// must cover every parent or the second one becomes a hole.
func TaskReparentParents(taskID string, parentIDs []string, reason string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: task_id must not be empty")
	}
	normalized := make([]string, 0, len(parentIDs))
	for _, id := range parentIDs {
		if id = strings.TrimSpace(id); id != "" {
			normalized = append(normalized, id)
		}
	}
	first, rest := taskParentsForWrite(normalized)
	parentIDs = taskParentIDsOf(first, rest)
	parentID := first
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf("task: %s: a reparent must carry --reason — the trail records WHY the dependency edge moved, and an unexplained rewrite of a row's production dependency is what the ledger's append-only rule exists to prevent.", taskID)
	}
	for _, id := range parentIDs {
		if id == taskID {
			return "", fmt.Errorf("task: %s: refusing to make the row its own parent — a self-referencing edge parks the row forever (Rule 33 reads the parent's status from the row itself), which is a cycle of one.", taskID)
		}
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("task: reparent: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once Commit succeeded //stylegate:ignore defer tx.Rollback() is a no-op once Commit succeeded (ErrTxDone) — the canonical idiom, and the error would replace the function's own verdict

	// The row itself, read INSIDE the transaction (one connection: the
	// existence probe, the cycle walk and the conditional UPDATE must share
	// the tx's snapshot and its write lock).
	cur, err := taskByIDTx(ctx, tx, taskID)
	if err != nil {
		return "", fmt.Errorf("task: reparent: %w", err)
	}
	if cur == nil {
		return "", fmt.Errorf("task: no task with task_id=%q", taskID)
	}
	// The row's CURRENT parent set, read through the ONE reader — so "did this
	// move change anything?" is asked about the same list the gate judges.
	oldIDs := taskParentIDsOf(cur.ParentTaskID, cur.ParentTaskIDs)
	if sameTaskParentSet(oldIDs, parentIDs) {
		// Idempotent: re-sending the parent set the row already carries is not
		// a move. Nothing is written (no trail row either) and the caller is
		// told so rather than shown a diff-shaped success.
		return reparentUnchangedResult(taskID, parentIDs), nil
	}
	// EVERY new parent is validated. The loop is not "the first one matters":
	// a second parent that names no row would park the child forever, and a
	// second parent that closes a ring would park every row on it — and Rule 33
	// has no override, no cascade and no timeout, so an unvalidated slot is a
	// permanent park with no repair path.
	for _, parentID := range parentIDs {
		// Existence, with the submit face's own sentence (Rule 14).
		parent, err := taskByIDTx(ctx, tx, parentID)
		if err != nil {
			return "", fmt.Errorf("task: reparent: %w", err)
		}
		if parent == nil {
			return "", fmt.Errorf("task update: "+taskParentMissingFormat, parentID)
		}
		// Cycle check: climb from the target parent toward the root. Being
		// reached from the target means the moved row is already an ANCESTOR
		// of it, so the edge would close a ring.
		cyclic, path, err := taskParentCycleClosed(ctx, tx, taskID, parentID)
		if err != nil {
			return "", fmt.Errorf("task: reparent: %w", err)
		}
		if cyclic {
			return "", fmt.Errorf("task: %s: refusing to reparent onto %s — it would close a dependency cycle: %s. Rule 33's gate requires a DAG: every row on a ring would park forever. Re-point the edge at a row outside the ring, or clear it with --clear-parent.", taskID, parentID, strings.Join(path, " -> "))
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO task_events (task_id, event, detail, created_at) VALUES (?, ?, ?, ?)`,
		taskID, ReparentEventName, reparentDetail(oldIDs, parentIDs, reason), float64(time.Now().Unix())); err != nil {
		return "", fmt.Errorf("task: reparent: audit trail: %w", err)
	}
	// The CAS: pin the row's id AND BOTH cells every validation above was
	// computed against. A concurrent mover that changed any parent between the
	// read and this write matches no row.
	res, err := tx.ExecContext(ctx,
		`UPDATE tasks SET parent_task_id = ?, `+taskParentIDsColumnName+` = ?, updated_at = ? `+
			`WHERE task_id = ? AND IFNULL(parent_task_id, '') = ? AND IFNULL(`+taskParentIDsColumnName+`, '') = ?`,
		parentID, rest, float64(time.Now().Unix()), taskID,
		strings.TrimSpace(cur.ParentTaskID), strings.TrimSpace(cur.ParentTaskIDs))
	if err != nil {
		return "", fmt.Errorf("task: reparent: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("task: reparent: %w", err)
	}
	if n == 0 {
		// The edge moved under us (or the row vanished): refusing rolls the
		// trail entry back with it, so the ledger never claims a move whose
		// validations were computed against a different tree (Rule 15).
		return "", fmt.Errorf("task: %s: the dependency edge moved before this update landed (expected parent set %v) — nothing was written and no state changed; re-read the row and retry", taskID, oldIDs)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("task: reparent: commit: %w", err)
	}
	return reparentMovedResult(taskID, oldIDs, parentIDs), nil
}

// sameTaskParentSet reports whether two parent slices hold the same ids in the
// same order — the idempotency test TaskReparentParents needs. Order matters:
// the FIRST parent is the one that lands in parent_task_id, so reordering is a
// real move (a reader of an older binary sees a different producer).
func sameTaskParentSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// reparentUnchangedResult renders the idempotent arm: the parent set asked for
// is the set the row already carries.
func reparentUnchangedResult(taskID string, parentIDs []string) string {
	if len(parentIDs) == 0 {
		return fmt.Sprintf(reparentUnchangedFmt+" (already has no parent_task_id)", taskID)
	}
	if len(parentIDs) == 1 {
		return fmt.Sprintf(reparentUnchangedFmt+" (parent_task_id already %s)", taskID, parentIDs[0])
	}
	return fmt.Sprintf(reparentUnchangedFmt+" (already the parent set %s)", taskID, strings.Join(parentIDs, taskParentIDsSeparator))
}

// reparentUnchangedFmt opens the IDEMPOTENT arm's line — the edge the caller
// asked for is the edge the row already carries, so nothing was written and no
// trail entry was made. The PREFIX is its own constant because a CALLER has to
// tell that arm apart from a move without matching the format's verbs (Rule 13:
// a value another reader compares against is a constant, never a literal typed
// at the comparison site); internal/tools/rework_reparent_children.go reads the
// prefix, the renderer below reads the format.
const reparentUnchangedPrefix = "Task parent unchanged: "

const reparentUnchangedFmt = reparentUnchangedPrefix + "%s"

// reparentMovedResult renders the move, naming both ends so an operator
// scanning stdout sees the edge traversal without a second command. The
// cleared arm says "(none)" rather than printing an empty field: an empty
// value on a line whose whole subject is which value changed is invisible.
func reparentMovedResult(taskID string, oldIDs, newIDs []string) string {
	return fmt.Sprintf("Task parent updated: %s (parent_task_id %s -> %s)", taskID,
		reparentSetLabel(oldIDs), reparentSetLabel(newIDs))
}

// reparentSetLabel renders a parent SET for the two operator-facing lines: one
// id as the bare id (byte-identical to the single-parent output every earlier
// release printed), several as a comma list, none as reparentNoneLabel (an
// empty field on a line whose whole subject is which value changed is
// invisible).
func reparentSetLabel(ids []string) string {
	if len(ids) == 0 {
		return reparentNoneLabel
	}
	return strings.Join(ids, taskParentIDsSeparator)
}

// reparentNoneLabel is how the absent edge renders on both stdout and the
// trail (Rule 1b: one spelling, so a reader diffing the two never has to ask
// whether "" and "-" mean the same thing — they do).
const reparentNoneLabel = "(none)"

// reparentDetail renders the append-only trail entry for one edge move: the
// reason, then both ends spelled out. Unlike the payload revision there is no
// digest to carry — an edge is a task id, and the id IS the value.
func reparentDetail(oldIDs, newIDs []string, reason string) string {
	from, to := reparentSetLabel(oldIDs), reparentSetLabel(newIDs)
	var b strings.Builder
	fmt.Fprintf(&b, "reason: %s\n", reason)
	fmt.Fprintf(&b, "old parent_task_id: %s\n", from)
	fmt.Fprintf(&b, "new parent_task_id: %s", to)
	return b.String()
}

// taskParentCycleClosed reports whether making taskID a child of parentID
// would close a dependency cycle, and if so the PATH it walked (rendered
// "parentID -> ... -> taskID" by the caller's refusal).
//
// The walk climbs parent_task_id links starting at parentID: if it ever
// reaches taskID, taskID is already an ancestor of parentID, so the new edge
// would close the ring. It follows the SAME column the gate reads — that is
// the whole argument for putting the check here rather than beside it: the
// edge that would park a row must be validated against the edge graph the
// gate walks, not against a parallel notion of "tree".
//
// Termination has TWO guards, because the ledger's input is not trustworthy:
//
//   - a VISITED set (the primary one): a chain that already contains a cycle
//     cannot loop forever, and if the walk re-enters a node it has seen, the
//     reason is reported instead of being read as "no cycle" (Rule 15 — the
//     unanswerable case must be loud). Encountering the row being moved is
//     checked FIRST, so a legitimate ring is reported as a ring.
//   - taskReparentWalkMax (the backstop): a chain deeper than any tree this
//     ledger can legitimately hold, refused by name.
//
// An empty parent id ends the walk at the root (a parentless row is the
// terminal case, not a missing one).
func taskParentCycleClosed(ctx context.Context, tx *sql.Tx, taskID, parentID string) (bool, []string, error) {
	return taskParentCycleClosedFrom(ctx, tx, taskID, []string{parentID})
}

// taskParentCycleClosedFrom is the MULTI-PARENT face of the cycle walk (epic
// multi-parent): starting at taskID, can every ancestor reachable from the
// candidate parents lead back to taskID?
//
// A row now has a SET of parents, so a ring can close through parent 2 just as
// easily as through parent 1 — and the single-chain walk above, which follows
// only parent_task_id, would MISS it and write a cycle the gate then parks
// forever (Rule 33 has no override, no cascade and no timeout). So the walk is
// a graph search over EVERY parent of every row it visits, through the ONE
// reader (taskParentIDsOf): the same edges the gate ANDs over, so the check and
// the gate cannot disagree about what a row's parents are.
//
// The path returned is the search path that closed the ring, which is what the
// refusal prints; the per-id "already contains a cycle" arm is the pre-existing
// detection of a ledger whose graph is already broken (a separate defect, and it
// stays a refusal rather than a repair, unchanged).
func taskParentCycleClosedFrom(ctx context.Context, tx *sql.Tx, taskID string, parentIDs []string) (bool, []string, error) {
	for _, parentID := range parentIDs {
		// Path per ROOT: what the refusal renders is one chain, and mixing two
		// roots' prefixes would print an edge that does not exist.
		closed, path, err := taskParentCycleClosedChain(ctx, tx, taskID, parentID)
		if err != nil || closed {
			return closed, path, err
		}
	}
	return false, nil, nil
}

// taskParentCycleClosedChain walks one root's ancestor graph.
func taskParentCycleClosedChain(ctx context.Context, tx *sql.Tx, taskID, rootID string) (bool, []string, error) {
	path := []string{rootID}
	visited := map[string]bool{rootID: true}
	type frame struct {
		id   string
		path []string
	}
	stack := []frame{{id: rootID, path: path}}
	steps := 0
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		cur := f.id
		if cur == taskID {
			return true, append(f.path, taskID), nil
		}
		steps++
		if steps > taskReparentWalkMax {
			return false, nil, fmt.Errorf("the dependency chain from %q exceeds %d links — refusing rather than reading an unbounded walk as 'no cycle'", rootID, taskReparentWalkMax)
		}
		row, err := taskByIDTx(ctx, tx, cur)
		if err != nil {
			return false, nil, err
		}
		if row == nil {
			// A dangling link mid-chain: the ledger already carries an edge
			// to nothing (possible on a replica whose parent has not synced,
			// or after direct surgery). The gate treats it as parked; for the
			// cycle walk it is simply the end of the chain — the new edge
			// does not close a ring here, and refusing would block a repair
			// of exactly the rows that need one.
			continue
		}
		for _, next := range taskParentIDsOf(row.ParentTaskID, row.ParentTaskIDs) {
			if next == taskID {
				return true, append(f.path, next), nil
			}
			if visited[next] {
				continue
			}
			visited[next] = true
			// Grow the path only where the branch actually goes: the stack
			// carries its own copy, so a sibling branch's prefix is never
			// rendered as this one's.
			grown := make([]string, len(f.path), len(f.path)+1)
			copy(grown, f.path)
			stack = append(stack, frame{id: next, path: append(grown, next)})
		}
	}
	return false, nil, nil
}

// TaskChildSessionEnv names the env var the `task run` CLI sets to the
// CHILD gopi's session id (resolved from the newest session JSONL created
// during the run window) — the writeback (running in the parent) reads it
// to backfill the executor's model. Rule 2: one writer (runTaskRun), one
// reader (taskExecutorSessionModel).
const TaskChildSessionEnv = "GOPI_TASK_CHILD_SESSION"

// TaskExecutorSessionEnv names the env var `gopi` exports at startup with
// its OWN session id (printLogPaths in package main). The task-run writeback
// reads it to resolve the EXECUTING session's model — the child session
// JSONL's session_start event carries the model that actually ran the task.
// Rule 2: exactly one writer (printLogPaths), exactly one reader
// (taskExecutorSessionModel below).
const TaskExecutorSessionEnv = "GOPI_EXECUTOR_SESSION_ID"

// DaemonContextEnvVars are the EXECUTION-CONTEXT environment variables a
// spawned executor child must NEVER inherit from the process that spawns it
// (daemon-context-isolation epic, 2026-09-13). They are stripped from the
// `task run` child's environment at the ONE spawn seam
// (cmd.Dir/env in cli_tasks.go), so each task a resident daemon executes
// starts with a context of its own:
//
//	TaskExecutorSessionEnv — the SPAWNER's agent-session handle. gopi
//	  exports it for every AGENT process (printLogPaths) and exports the
//	  agent-chain anchor (config.AgentSessionAncestorEnv) beside it. A
//	  resident daemon that was itself started from an agent's shell carries
//	  the pair; without this strip the executor child would (a) inherit a
//	  foreign session handle and (b) read as a CHILD of that agent session
//	  rather than as a fresh session of its own (it re-exports its own
//	  handle at startup, but an inherited value reaching it first is exactly
//	  the leak this epic removes — the agent-spawn-guard's inheritance
//	  signal, AgentSessionAncestorEnv, is the same inheritance).
//
// DELIBERATELY NOT in the set: GOPI_TASK_WRAPPER_ID / GOPI_TASK_RUN_ID /
// GOPI_TASK_RUN_OUTCOME (the wrapper-child identity the child NEEDS — its
// auto-register gate and its SIGPIPE latch), GOPI_TASK_WORKDIR (the workdir
// handoff, re-set at the seam), GOPI_TASK_CHILD_SESSION (a PARENT-side read:
// the child sets it and the writeback reads it, so stripping it from the
// child's env is a no-op for the child), GOPI_HOME / GOPI_MEMORY_DB /
// GOPI_WORKER_* (profile + loop config a child legitimately inherits).
//
// Rule 1b/14: one definition site (here), referenced by the spawn seam and
// by config.DaemonEnvLeakVars — a second list would drift.
var DaemonContextEnvVars = []string{
	TaskExecutorSessionEnv,
	config.AgentSessionAncestorEnv,
}

// HeavyTestOptInEnv / OperatorTerminalEnv are the heavy-test boundary's two env
// names, declared here because THIS package owns the spawn seams that must drop
// them — the same reason DaemonContextEnvVars lives here (Rule 1b/14: one
// definition site, referenced by the seam and by the mirror test in the leaf
// package that cannot import us).
//
// WHY A SPAWN SEAM MUST DROP THEM (2026-09-17): the heavy set is the most
// expensive thing this repo can be asked to do, and both names are ordinary
// INHERITABLE variables — a value that leaked in from a dev shell, a briefing
// that said "run the full suite", or a wrapper script travels down to every
// process a work tree spawns. GOPI_HEAVY_TESTS reaching a child is the
// inheritance the boundary refuses at the test itself (internal/heavytest's
// loud REFUSAL, the load-bearing half). GOPI_HUMAN_TERMINAL reaching a child is
// worse: it is the operator's declaration, and an INHERITED declaration would
// let a child claim to be the operator — agentguard documents that it must be
// set per-invocation precisely so no descendant can inherit it, and a seam that
// forwards it silently re-arms the heavy set below a human's back. So both are
// dropped here.
//
// They are NOT in DaemonContextEnvVars: that list means "execution context of
// the spawner" and is also the one config.DaemonEnvLeakVars mirrors for the
// child's own verification, while these two are SPEND-BOUNDARY names whose
// consumer is the test helper. Keeping the lists apart keeps each one's meaning
// readable; the seam references both.
// SpendBoundaryEnvVars are the env names a spawn seam must strip so no child
// inherits a claim it cannot honestly make. THE ONE NAME IS THE DECLARATION,
// and the omission of the opt-in is deliberate and load-bearing:
//
//   - config.HumanTerminalEnv is the operator's own signature, and an INHERITED
//     declaration is the dangerous value: it is what switches the heavy
//     boundary's refusal OFF. agentguard documents it as per-invocation for
//     exactly this reason, so a seam that forwarded it would let a descendant
//     answer "an operator said so" on the operator's behalf. Dropping it here
//     means the operator route cannot travel; the operator must invoke it at
//     the terminal he is sitting at.
//   - GOPI_HEAVY_TESTS is NOT in the list because its literal has exactly ONE
//     legal home (internal/heavytest/heavy.go — the Rule 13 gate
//     TestHeavyTestInventoryMatchesItsCallSites, §heavyAssertOneOptInName,
//     rejects a second spelling anywhere under internal/, cmd/ or testsuite/),
//     and the leaf package that owns it imports `testing`, which no production
//     package may pull into the binary. Its enforcement is therefore at the
//     test itself: a process that carries the opt-in without the declaration is
//     refused LOUDLY by internal/heavytest (the load-bearing half), whatever
//     environment it was handed.
var SpendBoundaryEnvVars = []string{config.HumanTerminalEnv}

// TaskRepoFor returns the repo a task's execution must stand in — the
// deliverable.repo its submitter stamped (M4.8) — refined by the task's
// DELIVERABLE WORKTREE when the executor already created one
// (deliverable.worktree, backfilled at creation time). "" when the row is
// gone, carries no repo, or the repo is not a usable directory.
//
// ORDER MATTERS (repos, like worktrees, can be transient): a task whose
// worktree exists works IN the worktree (Rule 6a — that is where its commits
// land, so that is where its GOPI.md/build/test contract belongs); the
// recorded repo is the fallback, because a worktree the close-out already
// released must not send the next run into a removed directory. Both are
// probed for existence (liveDir); a stale path falls through to the next
// candidate and, when nothing is usable, the CALLER falls back to its own
// start workdir and SAYS SO (Rule 15).
//
// It is the ONE reader of the row's repo for the executor-cwd seam (Rule 2):
// cmd/agent's taskExecutorWorkdir and the wrapper's push leg both consume it.
func TaskRepoFor(taskID string) string {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return ""
	}
	repo, _, worktree := taskRowDeliverable(taskID)
	if wt := liveDir(worktree); wt != "" {
		return wt
	}
	return liveDir(repo)
}

// TaskRepoForLaunch returns the directory an executor for taskID must stand
// in, GIVEN the directory its spawner was launched in (launchDir), or "" when
// the row names no ground of its own and the caller must keep launchDir.
//
// It exists because the child's cwd carries TWO jobs that pull in opposite
// directions, and only a ground-aware rule satisfies both:
//
//	① It is the child's PROJECT — the GOPI.md it loads, the tree
//	   code_search indexes, the base relative guard targets resolve against.
//	   That must be the TASK's repo, never the spawner's (a resident daemon
//	   was launched from some directory of its own).
//	② It is what the task's BRANCH is read from (deliverable branch
//	   backfill: `git -C <child cwd> branch --show-current`). A task worked
//	   in a LINKED WORKTREE must read its own branch — pointing the child at
//	   the row's deliverable REPO instead hands it the repo's checked-out
//	   branch (master), which is exempt from backfill, so the row ends with
//	   no deliverable branch and the merge leg starves.
//
// Rule: when launchDir ALREADY IS the row's ground (the common shape — a
// worker is started inside the workspace/worktree it serves), launchDir is
// returned unchanged: it is simultaneously the task's project, its branch
// authority, and the pre-epic behaviour. The row's own ground is used only
// when the spawner stands elsewhere (the resident-daemon leak: launched from
// the operator's shell or another project). Nothing usable → "" and the
// caller falls back to its launch workdir and SAYS SO (Rule 15).
func TaskRepoForLaunch(taskID, launchDir string) string {
	ground := TaskRepoFor(taskID)
	if strings.TrimSpace(launchDir) == "" {
		return ground
	}
	if launchDirIsTaskGround(taskID, launchDir, ground) {
		return filepath.Clean(strings.TrimSpace(launchDir))
	}
	return ground
}

// launchDirIsTaskGround reports whether the directory a spawner was launched
// in is PROVABLY the task's own ground — the three shapes, in order:
//
//	(a) it IS the ground, or lies INSIDE it (a linked worktree the row's repo
//	    encloses — the path deliverable.worktree records);
//	(b) it shares the task's git repository (one common dir, the object store
//	    every linked worktree of the repo shares — so a worktree parked
//	    anywhere, sibling included, is the same project);
//	(c) a row that RECORDS a worktree of its own has named its ground: another
//	    directory that merely shares the repository (a sibling task's
//	    worktree) is NOT this task's ground, so (b) does not apply — the
//	    recorded worktree wins.
//
// Anything else (the resident-daemon leak: a spawner launched from the
// operator's shell or from an unrelated project) is not the task's ground and
// the caller uses the row's own repo instead.
func launchDirIsTaskGround(taskID, launchDir, ground string) bool {
	if SameDir(launchDir, ground) || pathWithin(launchDir, ground) {
		return true
	}
	if _, _, worktree := taskRowDeliverable(taskID); liveDir(worktree) != "" {
		return false // (c): the row's own worktree is its ground, not this one
	}
	return sharesGitRepo(launchDir, ground) // (b)
}

// pathWithin reports whether dir lies INSIDE parent (both resolved through
// their stat identity first, so a symlinked spelling compares as the same
// tree). `.` and `..` segments are cleaned, never string-prefix matched: a
// sibling directory whose name merely starts with the parent's name is not
// inside it.
func pathWithin(dir, parent string) bool {
	dir, parent = SamePath(dir), SamePath(parent)
	if dir == "" || parent == "" || parent == string(filepath.Separator) {
		return false
	}
	return strings.HasPrefix(dir, parent+string(filepath.Separator))
}

// sharesGitRepo reports whether two directories belong to the SAME git
// repository — `rev-parse --git-common-dir` answers with the shared .git
// directory, which every linked worktree of a repository names alike. False
// when either directory is not a work tree, or when git cannot answer (a
// probe failure must never be read as "same repository").
func sharesGitRepo(a, b string) bool {
	commonA, commonB := gitCommonDir(a), gitCommonDir(b)
	return commonA != "" && commonA == commonB
}

// gitCommonDir returns the absolute shared-git-directory of the work tree at
// dir, or "" when dir is not in one (or git cannot answer within the probe
// budget — a wedged repository must not stall a spawn).
func gitCommonDir(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), taskGroundGitProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SamePath returns the canonical spelling of path for comparison: the
// symlink-resolved absolute form when it can be resolved, else the cleaned
// input. "" for an empty path.
func SamePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

// SameDir reports whether two paths name the SAME directory: an exact match,
// or two paths whose stat identities agree (os.SameFile follows symlinks, so
// a workspace reached through a link and the row's recorded path are one
// ground — a difference of spelling must never send an executor out of the
// tree it was launched in). False when either path is empty or does not
// exist: a path that is not there cannot be the ground.
func SameDir(a, b string) bool {
	a, b = filepath.Clean(strings.TrimSpace(a)), filepath.Clean(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(fa, fb)
}

// taskGroundGitProbeTimeout bounds the git probe TaskRepoForLaunch's
// ground predicate runs (git-common-dir / repo identity). A wedged or
// enormous repository must not stall a task spawn — a probe that cannot
// answer within it is read as "not the same ground", which keeps the
// pre-epic launch directory rather than inventing one (Rule 15: the seam
// reports which source it used either way).
const taskGroundGitProbeTimeout = 10 * time.Second

// liveDir returns dir when it NAMES an existing directory, else "" — the one
// predicate behind TaskRepoFor's fallback chain (an empty, missing or
// file path is not a directory to chdir into).
func liveDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// taskExecutorSessionModel resolves the model of the session that executed
// a task run's child gopi. The child exports its OWN session id via
// TaskExecutorSessionEnv (printLogPaths) — but the WRITEBACK runs in the
// PARENT `task run` process, which cannot see the child's env. Two cases:
//  1. GOPI_TASK_CHILD_SESSION is set (the parent passes its child's id
//     explicitly after the child exits — the `task run` CLI sets it from
//     the newest session JSONL created during the run window).
//  2. The env var names the CURRENT process's session (writebacks that
//     run inside the executing session itself, e.g. the auto-register end
//     hook — TaskExecutorSessionEnv, set by printLogPaths).
//
// Returns "" when neither resolves — the backfill stays best-effort.
func taskExecutorSessionModel() string {
	if sid := strings.TrimSpace(os.Getenv(TaskChildSessionEnv)); sid != "" {
		if m := taskModelFromSession(sid); m != "" {
			return m
		}
	}
	sid := strings.TrimSpace(os.Getenv(TaskExecutorSessionEnv))
	if sid == "" {
		return ""
	}
	return taskModelFromSession(sid)
}

// taskBackfillModel stamps model into a task row ONLY when the column is
// still empty (never overwrites a submit-time --model value or an auto
// row's creation stamp). Single UPDATE guarded by model=”.
func taskBackfillModel(taskID, model string) error {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET model = ?, updated_at = ? WHERE task_id = ? AND (model IS NULL OR model = '')`,
		model, float64(time.Now().Unix()), taskID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		taskNotifyLog("task %s: model backfilled to %q at writeback", taskID, model)
	}
	return nil
}

// tasksSessionFillSQL is the ONE spelling of "fill session_id only while it is
// empty" (Rule 14: a value spelled twice drifts — TaskBackfillSession's own
// UPDATE carries the same predicate for both columns and is the reference).
//
// THE GUARD IS RIGHT, AND IT WAS STARVED (2026-10-28). "Fill only while empty"
// is what makes a run's session id write-once. It failed not because the
// predicate was wrong but because the cell was never empty: the INSERT put the
// SUBMITTER's session there, so every submitted row was born full and its run's
// session could never land. The fix is upstream of this line (the submitter now
// has its own column, so the cell starts empty); the guard keeps its meaning.
const tasksSessionFillSQL = `session_id = CASE WHEN (session_id IS NULL OR session_id = '') THEN ? ELSE session_id END`

// taskRunSessionFromRowDir answers the session id the ROW'S OWN RUN DIRECTORY
// holds: `sessions/<task-id>/<session-id>.jsonl` (the task-run layout). The
// file is chosen with the SHARED session vocabulary (internal/sessionlayout):
// `IsJSONL` says it is a log, `IsSideFile` excludes the session's side files
// (signal-provenance.jsonl), so a side file can never be mistaken for the run's
// log. When a resumed row's directory holds several runs, the NEWEST record
// wins — the column names the session that executed most recently, which is
// the one every session-keyed reader wants.
//
// WHY THE DIRECTORY AND NOT THE ENVIRONMENT. The deferred session_id stamp
// runs inside the WRAPPER's writeback, and the wrapper's own environment names
// the WRAPPER's session (TaskExecutorSessionEnv), not the child's — stamping
// that would put a wrong id in the column, which is worse than an empty one
// because an empty column is at least obviously missing. The child's log is the
// row's own artifact: sessions/<task-id>/ is keyed by the row id, so this
// answer belongs to this row by construction.
//
// Returns "" when the row has no run directory, or when it holds no record —
// both are the ordinary "there is nothing to stamp" cases (a human-typed row
// that never ran a child, a run directory already swept), and the stamp is
// skipped rather than guessed.
func taskRunSessionFromRowDir(taskID string) string {
	tid := taskIDNormalize(taskID)
	if tid == "" {
		return ""
	}
	gd, err := config.GopiDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(gd, config.SessionsDirName, tid)
	// "Which record does this run directory stand for" is ONE question with ONE
	// answer (sessionlayout.NewestRecordInDir — Rules 13/14): this loop used to
	// re-derive it, and internal/gopilog's task-id fallback asks the same
	// question of the same directory.
	rec, ok := sessionlayout.NewestRecordInDir(dir)
	if !ok {
		return ""
	}
	return sessionlayout.SessionID(rec.Path)
}

// TaskBackfillSession stamps the EXECUTING child session's id AND model into
// a task row while it is still in_progress (bug054: the session_id stamp
// never existed — task run only resolved the child session after the run
// finished, so `gopi task show` mid-run showed session_id empty and model
// (unknown), exactly when tailing logs is most needed).
//
// Called by the `task run` CLI as soon as the child session JSONL is
// detected (runTaskRun's poll loop). The model is resolved from the child
// session JSONL's session_start event (taskModelFromSession — the same
// authoritative source the writeback backfill uses), so the caller passes
// only the session id. The stamp is guarded on BOTH columns being empty —
// it never overwrites a submit-time --session-id/--model value, an auto
// row's creation stamp, or a previous stamp. One UPDATE, best-effort: a
// miss (no child session file yet / session_start not flushed / DB locked)
// never blocks the run — the writeback's deferred backfills stay as the
// fallback. An empty sessionID is a silent no-op by contract.
//
// SOURCE GATE (epic 01a17df9 ①): the id is stamped ONLY when its record lives in
// this row's own run directory. The caller polls for "the newest session record
// in the run window", and a continuously written CHAT session is newer than the
// row's own log inside that window — stamping it put a stranger's id in the
// column, and the fill-only-while-empty guard made it permanent. A refused id is
// REPORTED (the error names the id, the directory that would have to hold it and
// where the id actually lives) and NOTHING is written; the writeback's deferred
// stamp fills the column later from the row's own directory.
func TaskBackfillSession(taskID, sessionID string) error {
	taskID = taskIDNormalize(taskID)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil // nothing to stamp — a silent no-op by contract
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	// THE GUARD IS UNCHANGED; WHAT FED IT WAS NOT. "Fill only while empty" is the
	// right rule for a write-once run session — the row must not be moved to a
	// different session by a later stamp. It starved because the INSERT wrote the
	// SUBMITTER into the same cell, so the run's session never had an empty cell
	// to land in. The submitter now has its own column (taskSubmitterColumnName),
	// so this predicate does what it always said it did.
	if refusal := taskSessionStampRefusal(ctx, db, taskID, sessionID); refusal != "" {
		return fmt.Errorf("%s", refusal)
	}
	model := taskModelFromSession(sessionID) // "" when not resolvable yet
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET session_id = CASE WHEN (session_id IS NULL OR session_id = '') THEN ? ELSE session_id END,
		                        model      = CASE WHEN (model      IS NULL OR model      = '') THEN ? ELSE model      END,
		                        updated_at = ?
		 WHERE task_id = ?
		   AND ((session_id IS NULL OR session_id = '') OR (model IS NULL OR model = ''))`,
		sessionID, model, float64(time.Now().Unix()), taskID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		taskNotifyLog("task %s: session %q model %q stamped at start", taskID, sessionID, model)
	}
	return nil
}

// taskDefaultReason is the status-specific fallback stamped when a caller
// moves a task's status WITHOUT supplying a --reason (the task-update-reason
// epic's "lazy caller still gets a meaning"). A real state change never
// leaves reason blank — every transition explains itself. Same-status
// no-op updates never stamp these (they would clobber a custom reason).
// statusAfterRace re-reads a row's status for the lost-CAS refusal message —
// the operator must learn WHERE the row actually went, not just that the move
// failed. Best-effort: an unreadable row renders "moved on" (never a wrong
// status).
func statusAfterRace(ctx context.Context, db *sql.DB, taskID string) string {
	cur, err := taskByID(ctx, db, taskID)
	if err != nil || cur == nil {
		return "moved on"
	}
	return cur.Status
}

// taskReasonFailedNoCause is the failed row's fallback when the writer
// supplied NEITHER a reason NOR a result digest. It used to read "task
// failed" — a sentence that asserts a cause the row does not have, and the
// reason 179 of 330 failed rows in the live ledger carried (measured
// 2026-10-07), which made reissue-vs-resolve undecidable from the ledger
// alone. The replacement states the ONE fact such a row can honestly carry:
// that no cause was recorded, and where the reader must look for one. A
// fallback may name its own emptiness; it must never impersonate a diagnosis
// (Rule 15 — a value that reads like information and carries none is worse
// than a declared blank).
const taskReasonFailedNoCause = taskReasonFailedPrefix +
	"no cause recorded by the writer (neither a reason nor a result digest accompanied the status write)" +
	" — read this row's task_events trail / session dir before deciding reissue vs resolve"

// taskReasonFailedPrefix heads every failed row's reason — whether derived
// from the writer's digest or the no-cause fallback — so a reader (and a
// triage query) can select the class in one predicate (Rule 1b/14).
const taskReasonFailedPrefix = "failed: "

// taskReasonDigestMaxRunes caps the reason derived from a result digest. The
// reason column is a one-line operator-facing WHY, and a digest can be long
// (the ledger's longest recorded reason is ~2.1k runes); the head of the
// digest is what decides reissue-vs-resolve, the tail is prose.
const taskReasonDigestMaxRunes = 300

// taskReasonDigestEllipsis marks a capped derived reason, so a reader can
// tell "the digest ended here" from "the digest was empty".
const taskReasonDigestEllipsis = "…"

// taskReasonStatsPrefix is the leading run-stats run the run-stats digests
// begin with (internal/agent's autoTaskStatsFormat renders it; the string is
// mirrored here because the ledger layer may not import the agent layer).
// A digest shaped "— elapsed=… tokens=N | <the WHY>" carries its WHY AFTER
// this run, so the derivation drops the run rather than quoting it as the
// cause. A digest that does not begin with it is used as it stands.
const taskReasonStatsPrefix = "— elapsed="

// taskReasonSegmentSeparator separates the runs of a composed digest
// (stats | hint | error record). One spelling, referenced by the derivation
// below.
const taskReasonSegmentSeparator = " | "

// taskFailedReason is the ONLY reader of the two fallbacks above: it turns
// the digest a failed write ALREADY carries into the row's reason column.
//
// WHY THIS EXISTS (epic failed-reason-attribution, measured 2026-10-07): the
// reason column and the task_events digest were written from the SAME call by
// the same caller — `TaskUpdateWithResult(id, failed, digest)` — and only the
// digest carried the cause. The reason fell back to a fact-free default, so
// 179/330 failed rows recorded "task failed" while their OWN trail, written in
// the same statement, said "zombie disposal (…)", "gopi run failed: …" or
// "error(category=network …)". The information was not never-collected; it was
// collected and DISCARDED on the way to the column a reader consults.
//
// The derivation is deliberately mechanical — cap, drop the stats run, keep
// the first line — because it must not invent: a caller that wants a
// different WHY passes an explicit --reason, which always wins.
func taskFailedReason(digest string) string {
	line := strings.TrimSpace(digest)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if strings.HasPrefix(line, taskReasonStatsPrefix) {
		if j := strings.Index(line, taskReasonSegmentSeparator); j >= 0 {
			line = strings.TrimSpace(line[j+len(taskReasonSegmentSeparator):])
		}
	}
	if line == "" {
		return ""
	}
	if r := []rune(line); len(r) > taskReasonDigestMaxRunes {
		line = strings.TrimSpace(string(r[:taskReasonDigestMaxRunes])) + taskReasonDigestEllipsis
	}
	return taskReasonFailedPrefix + line
}

func taskDefaultReason(status string) string {
	switch status {
	case taskStatusPending:
		return "returned to the queue"
	case taskStatusClaimed:
		return "claim picked up for execution"
	case taskStatusInProgress:
		return "work started"
	case taskStatusCompleted:
		return "task finished"
	case taskStatusFailed:
		return taskReasonFailedNoCause
	case taskStatusCancelled:
		return "task cancelled"
	case taskStatusVerified:
		return "verification passed"
	case taskStatusSuperseded:
		return "work carried by a successor row — this row is retired as superseded"
	}
	return ""
}

// taskUpdate is the shared mutation core (status required; the summary write
// is pre-gated by the two wrappers above). reason is the operator-visible WHY
// of the transition: a caller-supplied --reason when given, otherwise the
// status-specific taskDefaultReason — a real state change always records why
// it happened (the epic's tracking-rationale). A same-status no-op update
// (idempotent writeback re-landing the same state) NEVER overwrites reason —
// it only SETs reason when the caller supplied an explicit one.
func taskUpdate(taskID, status, summary, reason string) (string, error) {
	taskID = taskIDNormalize(taskID)
	return taskUpdateStamped(taskID, status, summary, reason, "")
}

// taskUpdateStamped is taskUpdate with the CLAIM-HOLDER stamp folded into the
// same atomic write (claimed-holder-freshness epic, 2026-09-12). A non-empty
// holder means the caller IS the process that will execute the row from this
// transition on, so the transition CAS and the "who holds it" stamp cannot
// come apart: claimed_by/claimed_at are rewritten in the SAME UPDATE that
// moves the row to in_progress, and [in_progress_at] is pinned to the same
// instant — the referent the zombie chain's freshness axis compares against.
// A same-status no-op (an idempotent writeback re-landing in_progress) never
// touches the claim trail: the row did not (re)start, so its stamp stays.
// The empty holder is exactly the old taskUpdate behaviour (byte-identical
// SQL for every non-start caller).
func taskUpdateStamped(taskID, status, summary, reason, holder string) (string, error) {
	return taskUpdateStampedFrom(taskID, status, summary, reason, holder, "", false)
}

// taskUpdateStampedFrom is taskUpdateStamped plus the two single-winner
// levers the run door needs (epic task-run-singleton-guard):
//
//	expectFrom — when non-empty AND the transition is claimed→in_progress, the
//	             UPDATE is a CAS pinned to that status, so of two runners that
//	             both reached the start, exactly ONE advances the row. The
//	             loser matches no row and is told so (Rule 15: a lost CAS is a
//	             refusal, never a silent success).
//	force      — the operator's --takeover: relaxes the CAS to the row id, for
//	             the deliberate takeover of a row whose holder is not trusted.
func taskUpdateStampedFrom(taskID, status, summary, reason, holder, expectFrom string, force bool) (string, error) {
	return taskUpdateStampedCore(taskID, status, summary, reason, holder, expectFrom, force, false, false, taskExecPidNone)
}

// taskUpdateDeliveryForced is the operator's `gopi task update --force` door:
// the ordinary write, with the DELIVERY-PROOF escape switched on for this one
// call. It is deliberately a separate funnel rather than a field on every
// caller's argument list, because exactly ONE surface (`task update`) may ask
// for it — the harness writeback, the merger's executor and the lifecycle
// tick must never inherit a release they did not request.
//
// The release reaches exactly one gate: deliveryProofRefusal's allowance
// (task_delivery_proof.go), the same decision point GOPI_TASK_DELIVERY_PROOF_ALLOW
// feeds, so the flag cannot waive the dirty-close-out gate, the residue gate,
// the tip gate or anything else (Rule 35's "not a master switch", and the
// epic's own constraint).
func taskUpdateDeliveryForced(taskID, status, summary, reason string, deliveryForce bool) (string, error) {
	if !deliveryForce {
		return taskUpdateStamped(taskID, status, summary, reason, "")
	}
	return taskUpdateStampedCore(taskID, status, summary, reason, "", "", false, false, true, taskExecPidNone)
}

// taskUpdatePreservingUpdatedAt is the ONE deviation from taskUpdateStampedFrom:
// the write does NOT touch updated_at.
//
// WHY A ROW WOULD WANT ITS CLOCK LEFT ALONE (task 01a12b01, memory #8179):
// updated_at is a WRITE-CHURN column — the stale sweep re-arms it every stall
// window (taskZombieBumpSQL), the sync layer uses it as its version anchor, and
// the display uses it as a re-arm marker — so it carries NO information about
// when a row's execution last did anything. A disposer that stamps it rewrites
// the one number an operator would use to date the abandonment: the row would
// read as "touched a second ago" while its entire point is that nobody touched
// it for thirty days. `reason` carries the true story, and ended_at carries the
// termination moment; updated_at is preserved verbatim so a reader can still see
// how long the row sat there before anyone closed it.
//
// Everything else is IDENTICAL to the ordinary path — the state machine
// (in_progress→cancelled), the claim/identity clearing, ended_at, the audit
// event, the notify bus — because this is the SAME function with one clock
// column skipped, never a second update path (Rule 14: one writer).
func taskUpdatePreservingUpdatedAt(taskID, status, summary, reason, expectFrom string) (string, error) {
	return taskUpdateStampedCore(taskID, status, summary, reason, "", expectFrom, false, true, false, taskExecPidNone)
}

// taskUpdateStampedCore is the ONE writer all of the above funnel into.
// preserveUpdatedAt (see taskUpdatePreservingUpdatedAt) skips the one column
// whose value is a re-arm marker rather than a fact about the row's execution.
// deliveryForce is `gopi task update --force`: it is handed VERBATIM to the one
// delivery-proof decision point and to nothing else.
func taskUpdateStampedCore(taskID, status, summary, reason, holder, expectFrom string, force, preserveUpdatedAt, deliveryForce bool, execPid int) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: task_id must not be empty")
	}
	status = strings.TrimSpace(status)
	summary = strings.TrimSpace(summary)
	reason = strings.TrimSpace(reason)
	holder = strings.TrimSpace(holder)
	if status == "" {
		return "", fmt.Errorf("task: nothing to update (set --status; task content is immutable — cancel and re-submit)")
	}
	if !validTaskStatus(status) {
		return "", fmt.Errorf("%s", invalidStatusRefusal(status, taskStatusWritable))
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()

	// State-machine gate: load the current status and refuse illegal moves
	// BEFORE mutating anything (a rejected update must leave the row — and
	// its FTS entry — exactly as it was).
	var prevStatus string
	{
		cur, err := taskByID(ctx, db, taskID)
		if err != nil {
			return "", fmt.Errorf("task: update: %w", err)
		}
		if cur == nil {
			return "", fmt.Errorf("task: no task with task_id=%q", taskID)
		}
		prevStatus = cur.Status
		if !taskTransitionAllowed(cur.Status, status) {
			return "", fmt.Errorf("task: illegal status transition %s → %s%s",
				cur.Status, status, taskTransitionAdvice(cur.Status, status))
		}
	}

	// THE TERMINAL-STATE GUARD (defect A, epic 01a18f3f) — the one rule the
	// state machine above cannot express, because the transition itself is
	// LEGAL: `failed → completed` is an allowed edge (a failed attempt whose
	// work is later found delivered), and it is exactly that legality which let
	// the wrapper overwrite a verdict it never read.
	//
	// A terminal status is a VERDICT on work that already has an answer, so
	// writing a DIFFERENT terminal status over it needs explicit evidence — an
	// authorization recorded on THIS row NEWER than the failure it supersedes —
	// and the refusal is recorded with its reason, not merely returned (Rule 15:
	// the wrapper's stderr belongs to a process that has already exited).
	//
	// It sits in this state gate, not in the wrapper, for the reason every other
	// terminal guard does: the rule must mean the same thing on every door (the
	// harness writeback, `task update`, the merger's executor, the sync layer's
	// local face). The operator's own door is `gopi task update <id>
	// --status completed --reason "…"` preceded by the authorization record —
	// see TaskReopenAuthorizedEvent.
	// THE OPERATOR'S DOOR — and it is a DOOR, not a bypass. `--force` is the
	// operator's explicit override, and on THIS guard its meaning is: "I have
	// looked at the failed verdict and I am superseding it". The reason is the
	// evidence, so a bare `--force` is refused by name: an override with no
	// sentence behind it is the silent rewrite this guard exists to stop, only
	// with the operator's fingerprint on it.
	//
	// It writes the SAME record a reviewer would write by hand, so the two paths
	// cannot drift (Rule 14) and the trail answers "who superseded this verdict,
	// and why" for both. The wrapper never passes --force, which is why the
	// defect stays fixed.
	if deliveryForce && prevStatus != status && taskStatusTerminalPair(prevStatus) && taskStatusTerminalPair(status) {
		// Rule 15: the read can fail (an unopenable ledger), and swallowing it
		// would let this arm treat "could not read" as "no evidence exists" —
		// the two are different rulings and only one of them is an override.
		// The failure is REPORTED through the sink that outlives this process;
		// the ruling it produces is unchanged (an unreadable trail still yields
		// no note, so the --reason requirement below still stands), which is the
		// same three-valued shape taskReopenEvidenceRefusal carries at
		// task_reopen_evidence.go:188.
		note, evidenceErr := TaskReopenEvidence(taskID, prevStatus)
		if evidenceErr != nil {
			taskNotifyLog("task: %s: reopen evidence could not be read (%v), so the --%s override "+
				"still requires --reason", taskID, evidenceErr, cliverbs.TaskUpdateForceWord)
		}
		if strings.TrimSpace(note) == "" {
			if reason == "" {
				return "", fmt.Errorf("task: %s REFUSED the %s writeback — --%s overrides a terminal "+
					"verdict %q and must carry --reason: the reason IS the evidence, and an override "+
					"with no sentence behind it is the silent rewrite the guard exists to stop",
					taskID, status, cliverbs.TaskUpdateForceWord, prevStatus)
			}
			if err := TaskReopenRecord(taskID, TaskReopenAuthorizedEvent,
				"operator override via --"+cliverbs.TaskUpdateForceWord+": "+reason); err != nil {
				return "", fmt.Errorf("task: %s: the override could not be recorded, so the %s "+
					"writeback is refused (an unrecorded override is the defect): %w", taskID, status, err)
			}
		}
	}

	if note, refused := taskReopenEvidenceRefusal(taskID, prevStatus, status); refused {
		return "", fmt.Errorf("task: %s REFUSED the %s writeback — %s", taskID, status, note)
	}

	now := float64(time.Now().Unix())
	// Build the UPDATE dynamically: ended_at participates only when the
	// status changes (a summary-only edit must not disturb the lifecycle).
	// bug056: summary is IMMUTABLE — there is NO summary SET clause here
	// anymore. The old `if summary != ""` arm wrote the writeback's result
	// digest over the submit-time summary (bug051's false-close symptom
	// 3128f17f). Result digests now ride task_events (TaskUpdateWithResult).
	// reason: resolved to the caller's value, else the status default; on a
	// same-status no-op a blank reason is NOT resolved (no default stamps
	// over an existing one) — only an explicit caller reason is written.
	stateChanged := prevStatus != status
	// THE WIP SNAPSHOT (epic: a run's uncommitted work survives an interruption).
	// It runs FIRST, before any gate refuses and before the UPDATE lands, because
	// the state it is protecting is about to stop being protected: `failed` is
	// the terminal marker that makes the run's worktree reclaimable, and a power
	// cut between this function's entry and its return would leave `wip/`
	// missing from a run whose work survives only in a directory the sweep may
	// remove.
	//
	// It is deliberately OUTSIDE every refusal arm: a refused writeback leaves
	// the row open and the run may still be re-driven, so a snapshot taken on
	// the way to a refusal costs one read and never changes a verdict. It never
	// blocks the transition (a failed copy is filed on the trail and printed,
	// see SnapshotBeforeFailed) — losing the ledger's record of what happened in
	// order to preserve a copy of it would be the wrong trade.
	if stateChanged && status == taskStatusFailed {
		SnapshotBeforeFailed(taskID)
	}
	// Arm 1's ENFORCEMENT half — the REFUSAL face (operator switch, default
	// off): a run that claims `completed` while its OWN recorded worktree still
	// carries untracked-only scratch is refused HERE, before any write, so the
	// row stays open for the run to clean its own scratch (and the attributed
	// record is already on its trail). It sits in this state gate rather than in
	// one caller so the switch means the same thing on every door. Nothing is
	// deleted and no branch is touched either way (see recollect below).
	if stateChanged && taskStatusClaimsDelivery(status) {
		// THE ROW'S OWN REPOSITORY (epic measured 2026-11-22, row 01a20059-d6c4):
		// every arm below measures the GROUND, so a run whose products landed in
		// the repository the ROW declares — while the ground is a clone of some
		// other repository — left no trace at all and closed `completed`: the
		// ground was clean (Rule 30 passed), the delivery leg's "carries no
		// commit to deliver" was TRUE OF THE GROUND, and the proof could not see
		// the row's repository. This arm takes the one reading nobody took.
		//
		// IT RUNS FIRST, BEFORE EVERY GATE THAT CAN REFUSE, and the order is
		// load-bearing: the fact it records stays true whether or not something
		// below refuses this write, and a refusal that returned early would
		// leave the row's trail silent about the very thing that makes the row
		// confusing. It is a RECORD, never a refusal — a row may legitimately
		// declare a repository it does not commit in, and the binding gates
		// below must keep meaning exactly what they mean. Nothing is deleted,
		// moved, committed or stashed by it.
		TaskRowRepoProductsRecord(taskID)
		// The BINDING gate (Rule 30's tracked shape): a run whose OWN recorded
		// worktree still holds an uncommitted TRACKED change has not delivered
		// — the branch would be invisible to every merge path — so the claim is
		// refused here and the row stays OPEN. Nothing is committed, stashed or
		// deleted by this arm (the operator's override is the escape). It
		// guards BOTH delivery claims (`completed` and the verifier's
		// `verified`), so a dirty row cannot wear a clean bill of health.
		if note, refused := reclaimRunTrackedDirtRefusal(taskID); refused {
			return "", fmt.Errorf("task: %s REFUSED the %s writeback — %s", taskID, status, note)
		}
		if note, refused := reclaimRunResidueRefusal(taskID); refused {
			return "", fmt.Errorf("task: %s REFUSED the %s writeback — %s", taskID, status, note)
		}
		// THE DELIVERABLE-TIP criterion (epic 01a13bbb): a row may claim a
		// delivery only while its own recorded branch's tip is still reachable
		// from some ref — the local repo's, or the delivery point the
		// removal-audit sink recorded for it. The incident this closes is a
		// `completed` row whose branch had been force-deleted with nothing left
		// holding its work; the measurement is a READ, and it deletes, moves and
		// restores nothing. The RECORD half runs from the writeback
		// (deliverableTipAtWriteback), so a row that is merely being re-stamped
		// by a non-delivery door still carries the fact if it is a defect.
		if note, refused := deliverableTipRefusal(taskID); refused {
			return "", fmt.Errorf("task: %s REFUSED the %s writeback — %s", taskID, status, note)
		}
		// THE DELIVERY-PROOF criterion (task_delivery_proof.go): the tip the run
		// actually produced must be contained in the DESTINATION the row declared
		// (the clone channel's origin/<branch>), by ancestry OR by patch
		// equivalence — a committed-but-unpushed ground is exactly the shape that
		// reached `completed` on 01a163df. Three-valued, with Unknown RECORDED
		// and never refused (a guard that refuses what it cannot see wedges the
		// ledger); a proven-undelivered claim leaves the row OPEN with its trail
		// carrying the reason. READ-only: nothing is deleted, moved or pushed.
		if note, refused := deliveryProofRefusal(taskID, deliveryForce); refused {
			return "", fmt.Errorf("task: %s REFUSED the %s writeback — %s", taskID, status, note)
		}
		// The MAIN-CHECKOUT arm (epic 01a0db25): a row whose OWN briefing named
		// the repo's main checkout — the shape measured on 01a0dabe — gets the
		// shared tree measured and its attributable deposits RECORDED on this
		// row's trail. It is a RECORD, never a refusal, and it measures only
		// when the briefing named that checkout: the shared tree belongs to the
		// operator and to every sibling run, so it is never blamed on a run that
		// was not pointed at it. Nothing is deleted, moved or written by it —
		// see task_main_checkout_deposit.go.
		TaskMainCheckoutDepositRecord(taskID)
	}
	// D2 — the merge-backlog TERMINAL guard (task_merge_backlog_terminal_guard.go):
	// a family row may reach a success terminal only with merge PROOF or an
	// explicit archive decision. It sits in this state gate rather than in
	// one caller for the same reason the residue refusal does: the rule must
	// mean the same thing on every door, and a refused write must leave the
	// row untouched. The NOTE is recorded on the row's trail by the guard
	// itself, so the still-open row explains why it is still open (Rule 15).
	// It runs AFTER effReason is resolved so the reason supplied by THIS
	// write (an explicit archive decision) is the one judged.
	effReason := reason
	if stateChanged && effReason == "" {
		effReason = taskDefaultReason(status)
		// The FAILED row's reason is derived from the digest this SAME call
		// carries (epic failed-reason-attribution): the digest is already in
		// hand here as `summary`, and for every failed writeback it holds the
		// cause — the column a reader consults must not fall back to a
		// fact-free sentence while the caller's own words ride the trail.
		// A no-op derivation (blank digest) leaves the declared fallback in
		// place, so a row can never close with an EMPTY reason.
		if status == taskStatusFailed {
			if derived := taskFailedReason(summary); derived != "" {
				effReason = derived
			}
		}
	}
	if stateChanged {
		if note, refused := mergeBacklogTerminalRefusalOn(ctx, db, taskID, status, effReason); refused {
			return "", fmt.Errorf("task: %s REFUSED the %s writeback — %s", taskID, status, note)
		}
	}
	var sets []string
	var args []any
	sets = append(sets, "status = ?")
	args = append(args, status)
	// THE DEFERRED session_id STAMP — the promise `TaskUpdateWithResult`'s own
	// comment has carried since bug054, with no code behind it (measured
	// 2026-10-25, task 01a16e1d's defect).
	//
	// The early stamp (cmd/agent runTaskRunOnce → TaskBackfillSession) polls
	// for the child's session JSONL for taskSessionPollMax and then gives up
	// QUIETLY on a miss; until this clause existed nothing repaired that, so a
	// run whose session record appeared after the poll window kept an EMPTY
	// session_id for its whole life. Measured on the live ledger (read-only,
	// eight-day window): 39 claimed rows whose own run directory holds a
	// session record while the column is empty — every session-keyed join
	// (task show's run line, the heartbeat's session leg, model/log
	// resolution) silently severed for them.
	//
	// It belongs HERE and not in a caller: taskUpdate is the ONE state gate
	// every door shares (the run writeback, `task update --status`, the auto
	// register hook, the merger's executor, the sync layer), so the repair
	// cannot be missed by a path that bypasses one wrapper. It rides the SAME
	// UPDATE as the transition — one write, no second handle, no lock window
	// — and the CASE makes it a no-op for a row that already carries its
	// session: an authoritative value is never overwritten (pin:
	// TestDeferredSessionStampNeverOverwritesAStampedRow).
	if sid := taskRunSessionFromRowDir(taskID); sid != "" {
		sets = append(sets, tasksSessionFillSQL)
		args = append(args, sid)
	}
	// Claim-holder stamp + execution-start referent (see taskUpdateStamped):
	// written in the SAME UPDATE as the transition they belong to, so the
	// row can never carry a start without a referent. A terminal/holding
	// transition CLEARS the referent instead (the stretch is over — leaving
	// a stale one behind would let it corroborate a later run's stamp).
	if holder != "" && stateChanged {
		sets = append(sets, "claimed_by = ?", "claimed_at = ?")
		args = append(args, holder, now)
	}
	if stateChanged {
		if status == taskStatusInProgress {
			sets = append(sets, tasksInProgressAtColumnName+" = ?")
			args = append(args, now)
			// Process identity (epic task-process-identity): capture the
			// EXECUTING process's pid + start ticks + command line in the
			// SAME UPDATE as the transition, so a row can never claim a
			// holder without the three facts that make the holder
			// verifiable later. The pid captured is the one the transition
			// stamps as holder (the caller's `holder`, or this process when
			// it omitted one — taskDefaultClaimHolder), NEVER an arbitrary
			// number: the two must describe the same process or the verdict
			// ladder would compare against a stranger.
			identPid := os.Getpid()
			if execPid > 0 {
				// THE EXECUTED CHILD IS THE ROW'S PROCESS (epic
				// claimed-then-in_progress): `in_progress` now means "a child
				// process was exec'd and is being waited on", so the identity
				// columns must describe THAT process — not the wrapper, which
				// is the launcher. The wrapper stays in claimed_by (the holder
				// stamp keeps its meaning: who took the row), and the pid
				// triple names the process an operator can `kill`/`tail`.
				identPid = execPid
			} else if h := claimPidOf(holder); h > 0 {
				identPid = h
				// The holder is another pid on THIS host (the merge
				// executor's children, a worker's wrapper). Its identity is
				// readable only while it lives; a foreign-host holder has no
				// readable identity here and records none.
				if !claimHostIsLocal(holder) {
					identPid = 0
				}
			}
			ident, ok := taskCaptureProcIdentity(identPid)
			if ok {
				sets = append(sets, tasksProcPidColumnName+" = ?",
					tasksProcStartColumnName+" = ?", tasksProcCmdColumnName+" = ?")
				args = append(args, ident.Pid, ident.Start, ident.Cmd)
			} else {
				// No readable identity (the pid is already gone, or procfs
				// is absent). Record NOTHING rather than a zero-valued
				// pseudo-identity: the verdict ladder then falls back to the
				// name-guarded pid check (the pre-existing evidence), never
				// to a match against zeros (Rule 15).
				sets = append(sets, tasksProcPidColumnName+" = NULL",
					tasksProcStartColumnName+" = NULL", tasksProcCmdColumnName+" = ''")
			}
		} else {
			sets = append(sets, tasksInProgressAtColumnName+" = NULL",
				tasksProcPidColumnName+" = NULL",
				tasksProcStartColumnName+" = NULL",
				tasksProcCmdColumnName+" = ''")
		}
	}
	if effReason != "" {
		sets = append(sets, "reason = ?")
		args = append(args, effReason)
	}
	if terminalTaskStatus(status) {
		sets = append(sets, "ended_at = ?")
		args = append(args, now)
	} else {
		sets = append(sets, "ended_at = NULL")
	}
	// updated_at is the row's write clock — stamped on every write (M4.5's
	// invariant, which the sync layer reads as its version anchor). The ONE
	// caller that skips it is the unowned-zombie disposer: for that write the
	// column is a re-arm marker an operator would misread as "recently
	// touched", so it is preserved verbatim
	// (taskUpdatePreservingUpdatedAt).
	if !preserveUpdatedAt {
		sets = append(sets, "updated_at = ?")
		args = append(args, now)
	}
	args = append(args, taskID)

	where := "task_id = ?"
	if expectFrom != "" && !force && prevStatus == expectFrom {
		// The start is a CAS on the state the caller observed: a second runner
		// advancing a row that already moved on matches NOTHING here.
		where = "task_id = ? AND status = ?"
		args = append(args, expectFrom)
	}
	res, err := db.ExecContext(ctx,
		"UPDATE tasks SET "+strings.Join(sets, ", ")+" WHERE "+where, args...)
	if err != nil {
		return "", fmt.Errorf("task: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("task: update: %w", err)
	}
	if n == 0 {
		if expectFrom != "" && !force && prevStatus == expectFrom {
			// The row left the expected state between the read and the write:
			// another runner advanced it first. Refusing is the whole point —
			// proceeding would execute the row twice.
			return "", fmt.Errorf("task: %s was %s but is %s now — another runner advanced it first (refusing to start a second execution)", taskID, expectFrom, statusAfterRace(ctx, db, taskID))
		}
		return "", fmt.Errorf("task: no task with task_id=%q", taskID)
	}
	var parts []string
	parts = append(parts, "status="+status)
	if effReason != "" {
		parts = append(parts, "reason="+effReason)
	}

	// bug056: append an audit event for the status transition — the
	// append-only trail records every move (the old code only ever touched
	// summary; the trail now captures the full lifecycle). The detail is
	// the summary text when this was a writeback (TaskUpdateWithResult);
	// a plain status move records an empty detail. Event write is
	// best-effort — never fails the transition.
	//
	// The audit-trail event is written for EVERY update (bug056 + the
	// documented contract of TestTaskUpdateWithResultKeepsSummaryImmutable
	// step ④): the trail is append-only, history is never rewritten, and a
	// repeated writeback appends another event row rather than erasing the
	// previous one. DO NOT "optimise" this into a same-status no-op — the
	// idempotence guards for a re-issued terminal write live at the CALLERS
	// (the run writeback's own terminal-row check, the dirty-close-out gate),
	// never in this append.
	// THE RECORD IS THE THREE ELEMENTS, ALWAYS (epic 01a1a899-d05d). This
	// writer used to put the writeback summary in the detail and the EMPTY
	// STRING on every other transition, so the ledger could not answer "who
	// turned this row into a failure, and on what grounds?" — measured on row
	// 01a1a194e1, whose two `failed` events both carried a blank detail and
	// whose only surviving account was a caller's exited stderr. The detail is
	// now `who=<identity> <from> → <to> why=<grounds>`, and a writeback's
	// digest rides it as a MARKED trailer line (task_status_event_detail.go):
	// the trail records the transition, and `task show`'s `result:` still
	// renders the run's summary because the reader strips the trailer
	// (taskEventLatestDetail below).
	//
	// NO forked writer: the event name stays the row's NEW STATUS and the
	// append stays best-effort — a lost audit row never rolls back a transition
	// (Rule 15: it is LOGGED, not swallowed).
	eventFrom := prevStatus
	if eventFrom == "" {
		eventFrom = TaskStatusEventPrevUnknown
	}
	taskEventWrite(ctx, db, taskID, status, taskStatusEventDetailText(taskStatusEventDetailParts{
		Who:    taskStatusEventWho(),
		From:   eventFrom,
		To:     status,
		Why:    effReason,
		Digest: summary,
	}))

	// Transition hook: fire configured notifications AFTER the row is
	// committed (a side-channel must never roll back the ledger write).
	// Same-status updates (taskTransitionAllowed's no-op case) do not
	// notify — only real status moves do. The recorded transition rides
	// the write's bus when the caller opened one (so a transactional
	// caller flushes past its commit); with no bus the flush is immediate
	// — the classic taskUpdate behaviour, unchanged.
	//
	// The post-write re-read feeds the transition notify: the bus must observe
	// the row as COMMITTED, not as the caller asked for it. (It used to feed the
	// verified-closure gate as a second consumer; that gate was deleted with the
	// self-verification removal of 2026-10-25 — see
	// devdoc/no-self-verification-gate.md.)
	transition := taskNotifyBus{}
	updated, updErr := taskByID(ctx, db, taskID)
	if updErr != nil {
		taskNotifyLog("post-update re-read of %s: %v", taskID, updErr)
	} else if updated != nil {
		transition.observe(taskNotifyPrevCopy(updated, prevStatus), updated)
	}
	taskNotifyFlush(&transition)

	out := fmt.Sprintf("Task updated: %s (%s)", taskID, strings.Join(parts, ", "))

	// VERIFICATION IS NOT THIS PROGRAM'S JOB (operator's ruling, 2026-10-25):
	//
	//	"verify 是由测试程序来做的，不是 gopi 内部的逻辑，一个程序不能自己检验自己"
	//
	// The verified-closure gate used to sit at exactly this point and enqueue an
	// INDEPENDENT verification row every time a dispatched run closed as
	// completed. It was removed wholesale, and the reason is structural rather
	// than a judgement about its quality: an in-process gate that judges the
	// program's own output is the program checking itself, and a verdict
	// produced that way cannot mean anything a reader should act on. The
	// evidence a fix works is a TEST PROGRAM's result (a test run, a measured
	// command output), which lives outside this write path entirely.
	//
	// What the removal costs, stated honestly so nobody re-adds it as a "fix":
	// a fix can now reach `completed` with no machine-generated follow-up row
	// asking anyone to look at it. That is the point — the row's own report
	// carries the `Ran:`/`Result:` evidence a reviewer reads, and the suite
	// carries the proof. Nothing in this program may manufacture a second
	// authority over its own output.
	//
	// `gopi task verify` remains as an OPERATOR verb (a human asking for a
	// check); it no longer has an automatic trigger, and it must not grow one.

	// Arm 1's ENFORCEMENT half (the run's OWN untracked residue at close-out)
	// is a SIDE-CHANNEL of this write, and that placement is the mechanism's
	// point: this is the ONE choke point every harness close-out passes through
	// (TaskRunWriteback, the auto-register end hook, the merger's executor), so
	// the record cannot be missed by a door that bypasses the wrapper. It runs
	// AFTER the terminal fact has committed and returned above — a failed
	// measurement can never roll back the outcome (the same contract the
	// deliverable.branch backfill follows), and the caller's `out` is already
	// final.
	reclaimRunResidueAtWriteback(taskID, status)

	// The deliverable-tip criterion's RECORD half. It sits on the SAME seam and
	// for the SAME reason (this is the one choke point every harness close-out
	// passes through), it runs AFTER the terminal fact has committed — a failed
	// measurement can never roll back the outcome — and it is silent for the
	// control case, a row whose deliverable records no branch.
	deliverableTipAtWriteback(taskID, status)

	return out, nil
}

// gopiExecutablePath resolves the gopi binary for task verify's child: the
// same running executable (os.Executable) so a worktree build verifies with
// its own build; "gopi" on PATH as the fallback. Mirrors gopiExecutable in
// cmd/agent/cli_tasks.go — the tools package cannot import package main,
// and Rule 3 forbids new top-level seams for this.
func gopiExecutablePath() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "gopi"
	}
	return exe
}

// taskTerminalClaimStatuses are the writebacks that CLAIM a run delivered:
// `completed` (the run says it is done) and `verified` (an independent verifier
// accepted it). The binding dirty-close-out gate reads THIS list, so the same
// rule is enforced at both terminal claims rather than only at the first one —
// a dirty row that some path marks `completed` outside the gate (a legacy row,
// an operator's manual move) is still refused the `verified` stamp it would
// otherwise wear as a clean bill of health.
var taskTerminalClaimStatuses = []string{taskStatusCompleted, taskStatusVerified}

// taskStatusClaimsDelivery reports whether s is a status that asserts the run
// delivered its work into a ref — the precondition of the Rule-30 binding gate.
func taskStatusClaimsDelivery(s string) bool {
	for _, v := range taskTerminalClaimStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// formatTaskTime renders a unix-seconds REAL timestamp as local time
// (2006-01-02 15:04:05) for the ledger views.
func formatTaskTime(sec float64) string {
	return time.Unix(int64(sec), 0).Format("2006-01-02 15:04:05")
}

// taskSearchMaxResults caps the rendered search output (newest/rank-first).
const taskSearchMaxResults = 20

// ftsQuote wraps s as a bareword-free FTS5 phrase: the query is user input,
// so every character is folded into a double-quoted phrase ("" doubling per
// the SQL literal convention) — no FTS5 syntax injection, and hyphens in
// task_id / session_id (t-1737… / UUID v4) match literally instead of acting
// as NOT operators.
func ftsQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// TaskSearch full-text searches the tasks ledger over task_id, session_id and
// summary. Strategy, mirroring distilled_qa's dual-index recall: the
// unicode61 index (tasks_fts) answers word/ASCII queries; the trigram index
// (tasks_tri) answers ≥3-char CJK substring queries; a LIKE scan recalls
// shorter CJK fragments (trigram's lower bound is 3 chars). Tiers are tried
// in order and the first hit-bearing tier wins (mixed tiers would double-rank
// rows present in both). Matches are BM25-ranked; the rendered list shows
// task_id, status, session and summary so a hit can be fed straight into
// `gopi task show`.
func TaskSearch(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("task: search query must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()

	phrase := ftsQuote(query)
	likePattern := "%" + likeEscape(query) + "%"
	tiers := []struct {
		name string
		sql  string
		args []any
	}{
		{"fts", `SELECT t.id, t.task_id, t.summary, t.status, t.session_id, t.created_at
		         FROM tasks t JOIN tasks_fts f ON t.id = f.rowid
		         WHERE tasks_fts MATCH ? ORDER BY bm25(tasks_fts), t.created_at DESC`, []any{phrase}},
		{"trigram", `SELECT t.id, t.task_id, t.summary, t.status, t.session_id, t.created_at
		         FROM tasks t JOIN tasks_tri f ON t.id = f.rowid
		         WHERE tasks_tri MATCH ? ORDER BY bm25(tasks_tri), t.created_at DESC`, []any{phrase}}, {"like", `SELECT t.id, t.task_id, t.summary, t.status, t.session_id, t.created_at
		         FROM tasks t
		         WHERE t.summary LIKE ? ESCAPE '\' OR t.task_id LIKE ? ESCAPE '\' OR t.session_id LIKE ? ESCAPE '\'
		            OR t.tags LIKE ? ESCAPE '\' OR t.payload LIKE ? ESCAPE '\' OR t.source LIKE ? ESCAPE '\'
		         ORDER BY t.created_at DESC, t.id DESC`, []any{likePattern, likePattern, likePattern, likePattern, likePattern, likePattern}},
	}

	for _, tier := range tiers {
		rows, err := db.QueryContext(ctx, tier.sql, tier.args...)
		if err != nil {
			return "", fmt.Errorf("task: search: %w", err)
		}
		var out strings.Builder
		count := 0
		for rows.Next() {
			var id int64
			var taskID, summary, status, sessionID string
			var createdAt float64
			if err := rows.Scan(&id, &taskID, &summary, &status, &sessionID, &createdAt); err != nil {
				rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
				return "", fmt.Errorf("task: search: %w", err)
			}
			fmt.Fprintf(&out, taskSearchRowFormat,
				taskID, status, summary, sessionID, formatTaskTime(createdAt))
			count++
			if count >= taskSearchMaxResults {
				break
			}
		}
		rowsErr := rows.Err()
		rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
		if rowsErr != nil {
			return "", fmt.Errorf("task: search: %w", rowsErr)
		}
		if count == 0 {
			continue // next recall tier
		}
		more := ""
		if count == taskSearchMaxResults {
			more = fmt.Sprintf(" (capped at %d)", taskSearchMaxResults)
		}
		return fmt.Sprintf("%d task(s) matching %q%s:\n%s", count, query, more, out.String()), nil
	}
	return fmt.Sprintf("No tasks matching %q.", query), nil
}

// taskSyncDB opens the ledger for one sync step (the syncQuestionOwned
// pattern: short-lived handles — each helper owns and closes its own).
func taskSyncDB() (*sql.DB, func(), error) {
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		return nil, nil, err
	}
	//stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	return db, func() { db.Close() }, nil
}

// taskStatusDirect fetches one task row straight from the local ledger
// (fresh read, no cache) by opening the sync DB itself. Returns
// (nil, err) when the row is absent — callers use it to report a row's
// ACTUAL state after a raced writeback (the rollback-yield twin).
func taskStatusDirect(taskID string) (*Task, error) {
	db, closeDB, err := taskSyncDB()
	if err != nil {
		return nil, err
	}
	defer closeDB()
	t, err := taskByID(context.Background(), db, taskID)
	if err != nil || t == nil {
		return nil, err
	}
	return t, nil
}

// taskIDPrefixCandidates names, in the ledger's own claim order, every row id
// that begins with prefix. The ORDER is the queue's own (taskClaimOrderBy —
// Rule 13: the list is derived from the constant `task next` orders by), so a
// refusal's candidates read in the order a dispatcher would have taken them.
// likeEscape keeps `%`/`_` in an operand from widening the match.
//
// This is the ONE candidate scan for a truncated task id (Rule 14): the row
// send/move leg (syncRowResolveTaskID) and the `task show` seam
// (taskIDPrefixRefusal) both reach it, so the two faces an operator meets name
// the SAME candidate set for the same operand.
func taskIDPrefixCandidates(ctx context.Context, q QueryExecer, prefix string) ([]string, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT task_id FROM tasks WHERE task_id LIKE ? ESCAPE '\'`+taskClaimOrderBy,
		likeEscape(prefix)+"%")
	if err != nil {
		return nil, fmt.Errorf("task id candidates of %q: %w", prefix, err)
	}
	defer rows.Close() //dead-end:ignore read-only cursor, drained before the caller's next read on the pinned connection
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("task id candidates of %q: %w", prefix, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("task id candidates of %q: %w", prefix, err)
	}
	return ids, nil
}

// likeEscape escapes %, _ and \ for a LIKE ... ESCAPE '\' pattern.
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ---------------------------------------------------------------------------
// P2 task dispatcher — the ledger as a WORK QUEUE (devdoc/task-dispatcher-p2.md).
//
// The audit-layer philosophy above is deliberately EXTENDED here (R6): tasks
// can be ENQUEUED (pending) and atomically CLAIMED (claimed) by another gopi
// instance or a serve loop, then executed and written back. The queue rides
// the same table, the same memory.db, the same FTS companions — no second
// engine, no second ledger.
//
// Anti-self-reference (ADDENDUM #5): an LLM-submitted task carries the
// submitting session's id (source='llm', session_id=submitter). Claim
// selection excludes tasks whose session_id matches the exclude-session
// argument, so a running session's serve/next never picks up its own
// just-submitted work — another session (or the human) does.
// ---------------------------------------------------------------------------

// TaskSubmitOptions carries the fields of a submitted (pending)
// task. Summary is required; everything else defaults cleanly.
type TaskSubmitOptions struct {
	// Summary is the one-line ledger headline (required).
	Summary string
	// Payload is the raw request text — the briefing the executing instance
	// will run with. Empty payload means summary IS the briefing.
	Payload string
	// Tags is a comma-separated tag string (spaces trimmed around each).
	// 'bug' is the reserved tag for the bug-flow entry point.
	Tags string
	// ParentTaskID links a split child to its parent (divide-and-conquer
	// tree). A non-empty value must reference an EXISTING task — a dangling
	// parent id is rejected (the tree must be navigable).
	//
	// MULTI-PARENT (EPIC multi-parent): this field is the row's FIRST parent
	// and keeps exactly the meaning it always had. Parents 2..N ride
	// ParentTaskIDs below; TaskSubmit folds BOTH into the two columns through
	// taskParentsForWrite, so "which parent is first" is decided in one place
	// and a single-parent submit is the same code path it always was.
	ParentTaskID string
	// ParentTaskIDs carries the SECOND-AND-LATER parents of a multi-parent row
	// (comma-separated, the parent_task_ids column's own encoding). Every id
	// here is validated exactly like ParentTaskID — a dangling one is refused
	// BY NAME, never dropped (Rule 15: a dependency the caller stated and the
	// ledger discarded is the one failure mode this field exists to prevent).
	ParentTaskIDs string
	// Source is who enqueued this — internal/tasksource's vocabulary (Rule 14):
	// an LLM session (`agent`), the operator at his terminal (`human`), the
	// merge path (`merger`), the resident machinery (`server`), a mirror leg's
	// forward (`forward`), the improvement miner (`agency-proposal`). An empty
	// value is DERIVED from this process's own signature (tasksource.DeriveNow)
	// — never defaulted to a role the caller did not claim.
	Source string
	// Repo is the git repo the work happens in (M4.8 deliverable closure,
	// t-1788089584). A non-empty value is validated with
	// `git rev-parse --is-inside-work-tree` BEFORE the row is written — a
	// bad repo aborts the whole submit (submitter forgot the repo path =
	// illegal operation, not a warning). When empty, TaskSubmit resolves
	// the process cwd as the default (epic task-submit-repo-default,
	// t-1788441090): if the cwd IS a git work tree the repo is verified
	// and stamped like an explicit one; if it is NOT a work tree the row
	// is still created but with an EMPTY deliverable and a stderr warning
	// (internal auto-submit paths must never be blocked by an odd cwd —
	// they just carry no repo context). Callers may still pre-resolve the
	// cwd themselves (CLI --repo default, task_submit tool) — then the
	// explicit value flows through the strict verify-or-abort path.
	Repo string
	// RepoNote carries the ONE line describing a substitution the caller ALREADY
	// made while resolving Repo (DurableRepoForSubmitWithNote). It exists so a
	// substitution is reported through the submit's own confirmation instead of
	// a bare stderr write the model never sees (Rule 15: a caller that can say
	// "the row names a different repository than you typed, and here is why"
	// must say it). Empty = nothing to report, the ordinary case.
	RepoNote string
	// SessionID is the submitting/creating session (anti-self-reference key).
	SessionID string
	// Model is captured at creation like TaskNew (per-task model routing is
	// P3; the column is already ledger data).
	Model string
	// TaskID overrides the auto t-<unix> id (CLI --task-id convenience).
	TaskID string
	// RefuseUngroundableID makes this submit REFUSE a row id that can never
	// name a ground (epic 01a12b5c) — the operator faces set it, and library
	// callers (test fixtures above all) leave it false.
	//
	// WHY AN OPT-IN AND NOT AN UNCONDITIONAL CHECK — MEASURED, twice. The
	// natural form (test the dispatch gate's own condition: non-uuid id AND
	// repo-aware) was tried first and a full budget run over
	// `./internal/tools ./cmd/agent` came back with 410 failing tests in both
	// independent runs: a fixture submits a slug id (`t-tie-zzz`,
	// `t-attrib-live-runner`, `t-start-stamp`) against an EXPLICIT repo — the
	// main checkout — and never resolves a ground at all. The engine cannot
	// tell an operator's typo from a test's seed row; the CALLER can, and that
	// is the only fact this field carries. The condition itself is still ONE
	// definition (TaskSubmitGroundNameDenied, the gate's own) and the sentence
	// is still ONE (taskGroundNameRefusal, served through
	// TaskSubmitGroundNameRefusal) — the opt-in governs WHEN it is asked, not
	// what it says.
	RefuseUngroundableID bool
	// Priority is the queue position weight (M4.11, lower runs first).
	// Zero value = derive from tags: bug→1, docs/doc-only→8, else→5
	// (taskTagPriority). An explicit value (CLI --priority / tool schema)
	// ALWAYS overrides the tags derivation — that is rule ④.
	Priority int
	// Attachments lists the absolute source paths of files to archive into
	// the task's session-dir attachments area (epic-task-reframe Phase 1:
	// `task submit --attachment <path>` / task_submit tool "attachments").
	// Each is copied at submit time to
	// <gopi-dir>/<sessions>/<task_id>/attachments/<abs-path-minus-root>
	// (bidirectional mapping preserved: /tmp/log.txt →
	// attachments/tmp/log.txt — same-name files in different dirs never
	// collide). Copies dereference symlinks and never recurse into
	// directories. The manifest (original_abs_path / replica_rel_path /
	// sha256 / file_size / submitted_at) rides in the row's deliverable
	// JSON "attachments" array; the run briefing injects the replica
	// paths as an [attachmentN:] index for the executing LLM's file-read.
	// A path that is not absolute, not a readable regular file, or whose
	// copy fails ABORTS the whole submit (an attachment the session never
	// receives must not be silently dropped from the briefing).
	Attachments []string
}

// mergeBacklogScanSummaryVerb renders the branch-scan decision row's
// summary ("merge-backlog: <branch> unmerged — merge/archive decision
// needed"). Single source (Rule 1/14): the scan submits with it and the
// dedupe predicate parses it — a mirrored literal would silently desync
// the mask (cf. the Rule 13 bypass findings).
const mergeBacklogScanSummaryVerb = "merge-backlog: %s unmerged"

// TaskOpenMergeBacklogBranches returns the branch names ALREADY COVERED by
// a merge-backlog decision row in the ledger — the branch-scan's idempotence
// mask. Exported for the cmd/agent serve branch-scan; the SQL lives here
// with the rest of the ledger SQL.
//
// Two coverage faces, because the branch-scan's own re-arm semantics made
// the LIVE face alone insufficient (the duplicate-dispatch bug, legacy-t
// audit 2026-09-10: one unmerged branch produced a NEW row on every tick —
// 75 rows for hp/master, ~40-130s apart):
//
//	① LIVE (pending/claimed/in_progress): the row is queued or executing —
//	   re-submitting would double-queue the same decision.
//	② TERMINAL (completed/failed/cancelled/verified): the decision was
//	   already made. A branch whose decision-owed row reached a terminal
//	   state WITHOUT the branch being merged (cancelled by the sync yield
//	   path, closed by a worker that did not merge, superseded by the
//	   configured merger sweep) must NOT be re-armed — the branch is simply
//	   already covered. Only a branch that gains NEW COMMITS is a fresh
//	   decision, and that case is covered by the audit's branch+tip dedup
//	   (AuditMergeBacklog), NOT by re-submitting the same summary verbatim.
//
// Reporting a decided branch as uncovered is exactly what amplified ~51
// deleted rows into ~600 duplicates in the legacy audit: the summary is a
// pure function of the branch name, so a re-submit of an unchanged branch
// carries zero new information.
func TaskOpenMergeBacklogBranches() (map[string]bool, error) {
	tracked := map[string]bool{}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return tracked, err
	}
	defer db.Close()
	// The key is the SUMMARY prefix, not the tags column: the cross-node
	// delta union REPLACES tags wholesale, so a row that travelled through
	// the sync-resolve/yield path keeps its summary but loses its tags
	// (legacy ledger: 574 of 617 scan rows carried the summary with the tag
	// stripped — a tags-fenced mask would have re-minted every one of them).
	// The summary is the only field the row keeps for its whole life.
	rows, err := db.QueryContext(ctx,
		`SELECT summary FROM tasks WHERE summary LIKE ?`,
		mergeBacklogTag+": %")
	if err != nil {
		return tracked, fmt.Errorf("task: merge-backlog query: %w", err)
	}
	defer rows.Close()
	prefix := mergeBacklogTag + ": "
	for rows.Next() {
		var summary string
		if err := rows.Scan(&summary); err != nil {
			return tracked, fmt.Errorf("task: merge-backlog scan: %w", err)
		}
		if rest := strings.TrimPrefix(summary, prefix); rest != summary {
			if fields := strings.Fields(rest); len(fields) > 0 {
				tracked[fields[0]] = true
			}
		}
	}
	return tracked, rows.Err()
}

// TaskMergeBacklogBranchCovered reports whether the ledger already carries
// a merge-backlog decision row for branch (the single-branch face of
// TaskOpenMergeBacklogBranches, for callers that probe one branch at a
// time).
func TaskMergeBacklogBranchCovered(branch string) (bool, error) {
	tracked, err := TaskOpenMergeBacklogBranches()
	if err != nil {
		return false, err
	}
	return tracked[branch], nil
}

// normalizeTaskTags splits a comma-separated tag string, trims each part,
// drops empties and rejoins canonically ("a, b,,c " → "a,b,c").
func normalizeTaskTags(s string) string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

// Task-level tunables (Rule 1: every literal in one place).
const (
	// taskDeliverableReposTimeout caps the `git rev-parse` verification —
	// a repo on a hung network mount must not stall task_submit.
	taskDeliverableReposTimeout = 10 * time.Second
)

// mergeBacklogTag is the tag that marks auto-submitted merge-backlog rows
// (serve branch-scan / audit). Single source of truth (Rule 2): the dedupe
// query above and the audit tag-builder share it, and cmd/agent consumes
// the exported alias MergeBacklogTag below — no mirrored literals.
const mergeBacklogTag = "merge-backlog"

// MergeBacklogTag is the exported alias of mergeBacklogTag for the cmd/agent
// submit face (the branch-scan task submission). Alias of the constant, not
// a second literal — mirrors the TaskStatus* exported-alias precedent.
const MergeBacklogTag = mergeBacklogTag

// MergeBacklogScanSummaryVerb is the exported alias of the branch-scan
// summary format (Rule 14: one definition, both faces import it — the
// cmd/agent scan builds with it, the dedupe predicate above parses it).
const MergeBacklogScanSummaryVerb = mergeBacklogScanSummaryVerb

// MergeBacklogTipLine is the exported alias of the merge-backlog payload's
// "branch tip: <sha>" format (Rule 14: one definition, both faces import it).
// internal/tools mints the line through the single authoring helper
// MergeBacklogPayloadTipLine and the cmd/agent branch-scan stamps it with that
// same helper, while the READ side -- mergeBacklogDedupCheck and the
// merge-backlog terminal guard -- parses the line back by rendering this format
// with an empty tip as the prefix. A mirrored literal on either face would
// silently desync author from parser (cf. the Rule 13 bypass findings).
const MergeBacklogTipLine = mergeBacklogTipLine

// archiveTagRefPrefix and archiveBranchPrefix are the loser-archive
// namespace constants — single source of truth (Rule 2). The loser-archive
// convention (PROJECT.md Rule 9): a DECIDED loser is tagged refs/tags/archive/
// <branch> and its branch deleted; the tag keeps the tip alive so every
// merge/cleanup/scan path must recognize the namespace to exempt it. One
// spelling for the FULL ref form (git for-each-ref) and one for the SHORT
// branch/tag form (git tag -l, branch-name prefix checks) — safe-merge
// guard, finish-cleanup exemption, branch-scan reachability filter and the
// merger sweep all derive from these (no mirrored literals, cf.
// feat/merge-backlog-single-source aa517b1).
const (
	archiveTagRefPrefix = "refs/tags/archive/"
	archiveBranchPrefix = "archive/"
)

// ArchiveTagRefPrefix / ArchiveBranchPrefix are the exported aliases of the
// archive-namespace constants above for the cmd/agent branch-scan face
// (archiveTagTips). Aliases of the constants, not a second set of literals —
// mirrors the TaskStatus* exported-alias precedent.
const (
	ArchiveTagRefPrefix = archiveTagRefPrefix
	ArchiveBranchPrefix = archiveBranchPrefix
)

// taskRepoVerifiable reports whether dir is a usable git work tree
// (`git -C <dir> rev-parse --is-inside-work-tree` exits 0). Bare repos and
// non-repos both fail — a submitted task must name a directory the
// executor can branch in.
func taskRepoVerifiable(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), taskDeliverableReposTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("repo %q is not a git work tree (git rev-parse failed: %w: %s)",
			dir, err, strings.TrimSpace(stderr.String()))
	}
	if !strings.Contains(strings.TrimSpace(string(out)), "true") {
		return fmt.Errorf("repo %q is not inside a git work tree (rev-parse: %s)",
			dir, strings.TrimSpace(string(out)))
	}
	return nil
}

// taskDeliverableRepoJSON renders the deliverable JSON a submit stamps at
// creation: {"repo":"<path>"} — branch/worktree are the executor's backfill
// (M4.9), the submit-side truth is the repo only.
func taskDeliverableRepoJSON(repo string) string {
	b, err := json.Marshal(map[string]string{"repo": repo})
	if err != nil {
		return ""
	}
	return string(b)
}

// taskDeliverableSubmitJSON renders the submit-time deliverable for a
// TaskSubmit call, version-dependent:
//
//   - NO attachments: the plain v1 repo-only blob (taskDeliverableRepoJSON)
//     — the dominant shape (every repo-stamped row since M4.8). Keeping it
//     byte-identical to v1 means the repoOnly/forwardable gates
//     (deliverableRepoOnly, deliverableForwardable) and the sync tests see
//     exactly what they always saw: a no-attachment submit changes nothing
//     downstream (Phase-1 regression contract).
//   - WITH attachments: a v2 blob carrying the repo plus the attachments
//     manifest array — {"repo":...,"attachments":[...]} (branch/worktree
//     still the executor's later backfill).
//
// Returns "" when repo is empty (a repo-less submit stays repo-less — the
// deliverable column is only meaningful when there is a repo to pair, and
// the auto mirror rows that skip the repo stamp carry no blob at all).
func taskDeliverableSubmitJSON(repo string, atts []TaskAttachment) string {
	if repo == "" {
		return ""
	}
	if len(atts) > 0 {
		return taskDeliverableRenderV2(repo, atts)
	}
	return taskDeliverableRepoJSON(repo)
}

// taskSubmitResolveDefaultRepo supplies the engine's repo default (epic
// task-submit-repo-default, t-1788441090): an empty Repo resolves to the
// process cwd. Returns "" (with the reason on stderr) when the cwd is not
// a git work tree — the row then goes out WITHOUT a deliverable rather than
// aborting, so internal auto-submit paths (branch-scan merge-backlog, push
// notify, conflict dispatch, adoption) keep working from odd cwds. Explicit
// callers (CLI, llm tool) pre-resolve AND abort on a non-repo cwd upstream
// of this — the strict face is unchanged there.
func taskSubmitResolveDefaultRepo() string {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "task_submit: warning: resolve default repo (cwd): %v — row submitted without a deliverable.repo\n", err)
		return ""
	}
	durable, durableNote, err := DurableRepoForSubmitWithNote(cwd)
	if err != nil {
		// THE DEAD-WORKSPACE REPAIR (epic 01a15629). A cwd that IS a workspace
		// path the channel already reclaimed cannot be recovered by git — there
		// is nothing there to resolve. The ledger row that recorded the path can
		// still name the repository, so the value is repaired and the fact is
		// printed (Rule 15). It runs ONLY on the failure arm, so a normal submit
		// pays nothing for it.
		if repaired, note := RepairDurableRepoForSubmit(cwd); repaired != cwd {
			fmt.Fprintln(os.Stderr, note)
			return repaired
		}
		fmt.Fprintf(os.Stderr, "task_submit: warning: cwd %s carries no durable repo — row submitted without a deliverable.repo (%v)\n", cwd, err)
		return ""
	}
	if durableNote != "" {
		fmt.Fprintln(os.Stderr, durableNote)
	}
	return durable
}

// TaskDurableRepoErrFmt is the ONE spelling of "this directory's durable
// repository could not be named": the directory, and the measured reason
// (Rule 1b — the message shape has one home, and the refusal and the warning
// render the same sentence).
const TaskDurableRepoErrFmt = "no durable repository for %s: %s"

// DurableRepoForSubmit resolves the DURABLE repository a row must name when it
// is submitted from dir — the repository's MAIN checkout, never a path that
// exists only for the life of one run.
//
// WHY THIS EXISTS (epic 01a0d55b, measured live 2026-09-25): tools.TaskSubmit's
// default repo was the submitting PROCESS CWD. For a worker-run gopi that cwd
// is the run's GROUND — a linked worktree the worker creates for that one run
// (TaskRunGroundFor / TaskGroundDirPrefix) and DELETES on finish. So every row
// auto-submitted from inside a run (notably the TOO_BIG reframe's roadmap
// children, internal/agent/reframe.go's dispatchReframeRoadmap) was born naming
// a directory that died with its parent:
//
//	parent 01a0d526 deliverable = {"branch":"task/<uuid>","repo":"/home/user/work/GoPi",
//	                               "worktree":"/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e"}
//	child  01a0d528 deliverable = {"repo":"/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e"}
//	                                   ^ the parent's WORKTREE, stamped as the child's REPO
//
// and the four children then refused to start, correctly, with "the repo is not
// a git work tree ... refusing to start the run in the repo's main checkout".
//
// The answer is `git rev-parse --path-format=absolute --git-common-dir` (via
// mergeExecMainRepo, the existing ONE resolution of "which main checkout owns
// this path" — Rule 13/14, no second implementation): a repository's main
// checkout and every LINKED WORKTREE of it answer with the same common dir, so
// the main checkout is recovered from any position inside the repository. It
// outlives every run's ground, so a child born with it stays runnable.
//
// A directory that is not a work tree, or whose repository cannot be named, is
// an ERROR (never a guess): the caller decides between the strict face (abort
// the submit — the CLI, the task_submit tool) and the lenient one (warn and
// submit repo-less — the internal auto-submit paths).
func DurableRepoForSubmit(dir string) (string, error) {
	repo, note, err := DurableRepoForSubmitWithNote(dir)
	if note != "" {
		// A SUBSTITUTION NOBODY CAN READ IS A SILENT ONE (Rule 15): the callers
		// that can carry the line into their own output call the WithNote face;
		// every other caller still gets it, on stderr.
		fmt.Fprintln(os.Stderr, note)
	}
	return repo, err
}

// DurableRepoForSubmitWithNote is DurableRepoForSubmit plus the ONE line
// describing a substitution it made. An empty note means the answer is the
// caller's own path (or the unchanged linked-worktree normalization).
//
// THE CLONE-GROUND ARM (epic 01a160a2) is the reason it exists. A clone-channel
// workspace is a repository of its OWN — `git rev-parse --git-common-dir`
// answers the workspace itself — so the linked-worktree resolution below returns
// it verbatim and a row submitted from inside such a run was born naming the
// directory the run's own end deletes (measured: 01a1604a, 01a15b96, 01a16053,
// all refused at spawn with "the directory no longer exists on disk"). The
// workspace's ORIGIN is the repository that outlives it, and cloneGroundSourceRepo
// (task_submit_clone_ground.go) is the ONE arm that answers with it.
func DurableRepoForSubmitWithNote(dir string) (repo, note string, err error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", "", fmt.Errorf(TaskDurableRepoErrFmt, `""`, "an empty directory was given")
	}
	if verr := taskRepoVerifiable(dir); verr != nil {
		return "", "", verr
	}
	main, err := mergeExecMainRepo(dir)
	if err != nil {
		return "", "", fmt.Errorf(TaskDurableRepoErrFmt, dir, err.Error())
	}
	main = strings.TrimSpace(main)
	if main == "" {
		return "", "", fmt.Errorf(TaskDurableRepoErrFmt, dir, "git named no common directory")
	}
	// THE CLONE WORKSPACE IS NOT A DURABLE ANSWER, even though it IS a "main
	// checkout" of its own. Asked BEFORE the value is handed back, so no caller
	// can stamp it (the answer is either the source repository or a refusal).
	if src, srcNote, applies, serr := cloneGroundSourceRepo(main); applies {
		if serr != nil {
			return "", "", serr
		}
		return src, srcNote, nil
	}
	return main, "", nil
}

// VerifyGitWorkTree is the exported face of taskRepoVerifiable — the
// worktree-mode skip in the agent-package tests uses it to decide whether
// the cwd can carry a deliverable.repo stamp at all.
func VerifyGitWorkTree(dir string) error { return taskRepoVerifiable(dir) }

// TaskAutoDeliverableJSON renders the deliverable blob a RUN row is born with
// when its creator has no explicit repo: the creator's own git work tree,
// resolved by the SAME default the submit engine uses
// (taskSubmitResolveDefaultRepo) and rendered by the SAME renderer
// (taskDeliverableSubmitJSON) — Rules 13/14, one default and one renderer.
// "" when the process stands in no work tree: the row then carries no
// deliverable, which is the honest state rather than a guessed path.
//
// WHY THIS EXISTS (epic raw-prompt-worktree-release, 2026-09-14): the
// harness's auto-registration hook mints the ledger row for a run with NO
// human wrapper row (a bare prompt / chat). That row used to be born with an
// EMPTY deliverable, and an empty blob is not "no information" — it disables
// the whole worktree lifecycle for that run at three points:
//
//	① internal/tools/task_deliverable_worktree.go's creation-time backfill
//	   returns early on a blank blob ("no deliverable blob (auto mirror rows)"),
//	   so the Rule-6 worktree the executor created is recorded NOWHERE;
//	② every reclaim arm selects rows by `deliverable LIKE '%worktree%'`
//	   (ReclaimOwnFinishedWorktrees) or by a parsed deliverable.worktree
//	   (the finish-cleanup arms) — neither matches a blank blob;
//	③ so the directory stays registered, repoSweepProtectedBranches keeps
//	   reading the branch as ACTIVE BOND WORK, and the merger never merges
//	   the finished deliverable — silently.
//
// A repo stamp is the entry ticket to that lifecycle: it is what lets ①
// record the worktree and ② release it. The WRAPPER-row faces (`task run`,
// worker) already had one; the bare-prompt face is the face this closes.
func TaskAutoDeliverableJSON() string {
	return taskDeliverableSubmitJSON(taskSubmitResolveDefaultRepo(), nil)
}

// TaskDeliverableRepoJSONFor renders the deliverable blob for a row whose repo
// the CALLER already resolved — the same renderer, the same v1 shape, so a
// caller that knows its repository does not have to re-spell the JSON
// (Rule 13/14: one renderer, never a second `{"repo":…}` builder).
//
// It exists for the ONE caller that has a repo the cwd does not name: the
// bare-prompt seam with the CLONE CHANNEL on (epic raw-prompt-clone-channel).
// There the row must name the declared `dev_repo` source repository, not the
// process's own work tree, because that source repo is where the channel's
// workspace is cloned FROM — the ground and the row must agree.
//
// Repo-path verification is NOT repeated here: every caller of this function
// resolves and validates its path through its own channel's reader
// (devworkspace.RepoPathReadable for the clone channel), and a second check
// here would be a second answer to "is this repo usable".
func TaskDeliverableRepoJSONFor(repo string) string {
	return taskDeliverableSubmitJSON(strings.TrimSpace(repo), nil)
}

// TaskSubmit enqueues a pending task (the task_submit engine). Mirrors
// TaskNew's id/summary rules; the created row starts status=pending with the
// dispatcher provenance fields filled. Returns the rendered confirmation
// including the task_id (the tool contract: the LLM can then poll
// `gopi task show <id>`).
func TaskSubmit(opts TaskSubmitOptions) (string, error) {
	out, _, err := TaskSubmitReturningID(opts)
	return out, err
}

// TaskSubmitReturningID is TaskSubmit for callers that need the created ROW's
// id back — an auto-enqueue that must LINK its next submission to this one
// (internal/agent's reframe roadmap: the chain's parent is the PREVIOUS child,
// so the id has to come out of the submit, not out of the confirmation prose
// an LLM reads).
//
// CONTRACT: TaskSubmit is a thin wrapper over this function, so the two cannot
// drift; the confirmation string is byte-for-byte the one TaskSubmit always
// returned (its first line is still "Task queued: <id> …"), and taskID is the
// SAME id that line prints.
//
// The MIRROR LEG returns taskID == "" (epic task-submit-returning-id): on a
// declared client node TaskSubmit does not create the row locally — it forwards
// a SyncTaskRow through the peer's import (taskSubmitRemote), and that wire
// carries no parent column nor a receipt the caller could chain onto. An empty
// id is the honest answer there: the caller keeps its previous parent rather
// than inventing a link nobody wrote (Rule 15), and dispatchReframeRoadmap
// reports it.
//
// TaskSubmit enqueues a pending task (the task_submit engine). Mirrors
// TaskNew's id/summary rules; the created row starts status=pending with the
// dispatcher provenance fields filled. Returns the rendered confirmation
// including the task_id (the tool contract: the LLM can then poll
// `gopi task show <id>`).
func TaskSubmitReturningID(opts TaskSubmitOptions) (string, string, error) {
	opts.Summary = strings.TrimSpace(opts.Summary)
	if opts.Summary == "" {
		return "", "", fmt.Errorf("task_submit: summary must not be empty")
	}
	opts.Payload = strings.TrimSpace(opts.Payload)
	// THE CREATOR IS STAMPED HERE, ONCE — the same funnel every submit face
	// passes through (task_fixture_reclaim.go carries the reasoning). A row
	// whose submitter is unrecorded can never be proved abandoned, so
	// `submitted_by` renders the empty placeholder and no machine can answer
	// "is the session that filed this still around?".
	opts.SessionID = TaskSubmitCreatorSession(opts.SessionID)
	// THE PAYLOAD FILE REFERENCE IS READ HERE, ONCE (task_payload_fileref.go):
	// this function is the funnel every submit face passes through (the
	// task_submit TOOL, `gopi task submit`, the reframe roadmap's children,
	// verify-rework, the merge backlog), so a leading `@<existing path>` becomes
	// the file's CONTENT on all of them by construction — the faces cannot
	// disagree about it. Measured defect this repairs: the CLI expanded the
	// reference in its own argument parser while the tool handed the SPELLING
	// to this engine, so three bug rows were born at 88 bytes
	// (= len("@"+path)) while their files held 2677/3406/3742 bytes, and each
	// reported success. The reader is config.ExpandFileRef — the CLI's own — so
	// there is no second implementation to drift (Rule 14).
	expandedPayload, payloadFileRefNote, pfErr := taskSubmitExpandPayloadFileRef(opts.Payload)
	if pfErr != nil {
		// A reference naming an unreadable file REFUSES the submit, loudly,
		// naming the path and the escape: storing the spelling is the defect,
		// and nothing has been written at this point (no row, no session dir).
		return "", "", pfErr
	}
	opts.Payload = expandedPayload
	opts.Tags = normalizeTaskTags(opts.Tags)
	// THE PARENT FOLD (EPIC multi-parent): one list, old column first. A
	// caller that named only ParentTaskID keeps exactly the previous behaviour
	// (a one-element list), and a caller that named several gets parent 1 in
	// parent_task_id and the rest in parent_task_ids — so an OLDER binary on
	// another machine still reads a parent instead of seeing an unparented
	// (instantly claimable) row.
	parentIDs := taskParentIDsOf(opts.ParentTaskID, opts.ParentTaskIDs)
	opts.ParentTaskID, opts.ParentTaskIDs = taskParentsForWrite(parentIDs)
	// M4.8 deliverable closure: a supplied repo is verified BEFORE the
	// enqueue — `git rev-parse --is-inside-work-tree` must succeed in that
	// directory. Failure aborts the whole submit (the caller's task queue
	// is untouched — verification precedes the INSERT), because a task row
	// pointing at a non-repo breaks the branch/worktree backfill loop
	// (M4.9) before it starts. When NO repo is supplied, the default is
	// the process cwd (epic task-submit-repo-default, t-1788441090): a
	// work-tree cwd is stamped, a non-work-tree cwd logs a warning and the
	// row goes out without a deliverable — never an abort, so internal
	// auto-submit paths cannot be blocked by an odd cwd.
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = taskSubmitResolveDefaultRepo()
	} else {
		// AN EXPLICIT repo goes through the SAME durable resolution the default
		// does — a submitted row must name a path that OUTLIVES the submitter's
		// run.
		//
		// WHY THIS IS NOT ONLY ABOUT DEFAULTS (measured 2026-10-07: 30 rows, 24
		// vanished paths). The default arm has been durable since epic 01a0d55b;
		// the EXPLICIT arm stamped the caller's string verbatim. So every
		// submitter that named its own repo — the `task_submit` tool's `repo`
		// parameter, `gopi task submit --repo <path>` — and happened to be
		// standing in a run-scoped GROUND (the linked worktree a worker creates
		// for one run and deletes on finish) minted rows born naming a directory
		// their own parent was about to delete. Each of those rows then refused
		// to start, correctly and permanently:
		//
		//	cannot resolve the ground for task <id> in repo
		//	/home/user/workspace/gopi-<uuid>: the repo is not a git work tree …
		//
		// The main checkout is recovered from ANY position inside the repository
		// (mergeExecMainRepo → `git rev-parse --git-common-dir`, the existing ONE
		// resolution), so a linked worktree normalizes to the repository that
		// owns it while a main checkout is IDENTITY (SameDir-checked below:
		// returning the resolved value unconditionally would restamp an
		// operator's spelling — a symlinked path, a `~`-relative one — for no
		// gain).
		durable, durableNote, derr := DurableRepoForSubmitWithNote(repo)
		if derr != nil {
			// THE DEAD-WORKSPACE REPAIR (epic 01a15629): the caller named a
			// path that a previous run's workspace reclamation deleted. The
			// ledger row that recorded it names the repository, so the submit is
			// repaired — LOUDLY, and only when the repair is provable (the
			// recovered path is a live git work tree). Anything else keeps the
			// refusal it always had, rather than inventing a repository.
			if repaired, note := RepairDurableRepoForSubmit(repo); repaired != repo {
				fmt.Fprintln(os.Stderr, note)
				durable = repaired
			} else {
				return "", "", fmt.Errorf("task_submit: %w", derr)
			}
		}
		if !SameDir(durable, repo) {
			// REPORTED, never silent (Rule 15): the row now names a DIFFERENT
			// directory than the caller typed, and a caller that meant something
			// else by it must be able to see that immediately. The message names
			// both paths — the clone-ground arm's own sentence when it fired
			// (opts.RepoNote, rendered in the confirmation the submitter reads),
			// the linked-worktree sentence otherwise.
			if durableNote != "" {
				opts.RepoNote = durableNote
			} else {
				fmt.Fprintf(os.Stderr, "task_submit: repo %s is a linked worktree — stamping the DURABLE main checkout %s so the row outlives the run\n", repo, durable)
			}
			repo = durable
		}
	}
	if repo != "" {
		if err := taskRepoVerifiable(repo); err != nil {
			return "", "", fmt.Errorf("task_submit: %w", err)
		}
	}
	// THE SUBMIT MOMENT REFUSES A ROW THAT COULD NEVER START, for the faces
	// that SPEAK FOR AN OPERATOR (epic 01a12b5c; Rule 15). Everything above is
	// pure resolution: no row, no session dir, no attachment copy has happened
	// yet, so this refusal writes NOTHING — which is exactly the state the
	// defect needed ("Task queued: t-foo" for a row that dispatch then refuses,
	// forever).
	//
	// WHY IT IS OPT-IN (opts.RefuseUngroundableID) AND NOT UNCONDITIONAL — the
	// MEASURED reason, not a preference. The first cut tested the DISPATCH
	// GATE's own condition here (non-uuid id AND repo-aware) on the theory that
	// only the calling face could be reached, and a full budget run over
	// `./internal/tools ./cmd/agent` came back with 410 failing tests: a
	// fixture submits a slug id (`t-tie-zzz`, `t-attrib-live-runner`,
	// `t-start-stamp`) against an EXPLICIT repo (the main checkout, so the row
	// is repo-aware by construction) and never resolves a ground at all. The
	// condition is genuinely the gate's; what it cannot distinguish is an
	// operator typing a bad id from a test seeding a row. So the CALLER
	// declares which it is — the one fact the engine cannot measure — and the
	// refusal keeps its single definition (TaskSubmitGroundNameDenied) and its
	// single sentence (taskGroundNameRefusal, via TaskSubmitGroundNameRefusal).
	//
	// The dispatch gate is NOT changed by this: a row that already exists with
	// a non-uuid id keeps its current verdict there, which is what makes this an
	// additive early refusal rather than a moved gate.
	if opts.RefuseUngroundableID {
		if err := TaskSubmitGroundNameRefusal(opts.TaskID, repo); err != nil {
			return "", "", err
		}
	}
	// The caller's OWN spelling is validated FIRST: a typo (`--source hman`)
	// must be refused loudly, never silently rewritten into a plausible role
	// (Rule 15 — a guessed value hides the caller's mistake and records a
	// provenance nobody chose).
	if opts.Source != "" && !validTaskSource(opts.Source) {
		return "", "", fmt.Errorf("task_submit: invalid source %q (want %s)", opts.Source, tasksource.Policy())
	}
	if opts.Source == "" {
		// A caller that expressed no role gets THIS PROCESS's role, measured
		// (internal/tasksource). The old literal fallback is what let an agent
		// session's row land as a machine or human claim it could not support.
		opts.Source = tasksource.DeriveNow(opts.Source)
	} else if tasksource.IsHumanClaim(opts.Source) {
		// The human claim is the ONE value re-derived even when it was stated:
		// a stated `human` from a process with an agent chain above it is not a
		// claim the row may carry. This is the row-create FUNNEL every writer
		// passes through, so the evidence requirement holds for libcallers too
		// and not only for the CLI.
		opts.Source = tasksource.DeriveNow(opts.Source)
	} else if value, ok := tasksource.Normalize(opts.Source); ok {
		// A retired spelling is STORED as the value it always meant, so a row
		// written today never carries an alias (the ledger converges on one
		// vocabulary instead of accumulating a third spelling).
		opts.Source = value
	}
	// M4.11 priority: explicit value wins (rule ④); zero value derives
	// from tags (bug→1, docs→8, else 5 — the ONE mapping, taskTagPriority).
	if opts.Priority <= 0 {
		opts.Priority = taskTagPriority(opts.Tags)
	}
	// Closing assertion: whatever the ladder above produced must be a value the
	// ledger accepts. It can only fire if a row-create branch is added without
	// teaching the vocabulary about it (Rule 15 — never write an unvalidated
	// provenance).
	if !validTaskSource(opts.Source) {
		return "", "", fmt.Errorf("task_submit: derived source %q is not a ledger value (want %s)", opts.Source, tasksource.Policy())
	}

	ctx := context.Background()
	localDBPath := memoryDBPath()
	// LEDGER ADDRESS (epic remote-submit-central-ledger): which ledger is
	// authoritative for THIS row is decided BEFORE anything is opened. On a
	// mirror leg (a declared client — HP / RPi5) the local ledger is NOT the
	// queue, so the gate either resolves the authoritative peer or refuses
	// LOUDLY, naming the path it would have written: the old unconditional
	// local insert made a remote submit a black hole (Rule 15).
	fwdPeer, fwdErr := guardTaskSubmitLedgerAddress(localDBPath)
	if fwdErr != nil {
		return "", "", fwdErr
	}
	if fwdPeer != "" {
		out, err := taskSubmitRemote(opts, ctx, localDBPath, fwdPeer)
		if err != nil {
			return out, "", err
		}
		if payloadFileRefNote != "" {
			out += "\n" + payloadFileRefNote
		}
		return out, "", nil
	}
	db, err := openTasksDB(ctx, localDBPath)
	if err != nil {
		return "", "", err
	}
	defer db.Close()

	// Parent linkage must be real: a dangling parent id would create an
	// unnavigable split tree (and a silent divide-and-conquer failure). EVERY
	// parent is validated, and the FIRST dangling one is refused by name —
	// the same sentence (taskParentMissingFormat) whichever column it would
	// have landed in, because the refusal is about the id the caller typed,
	// not about which slot it was going into.
	for _, parentID := range parentIDs {
		parent, err := taskByID(ctx, db, parentID)
		if err != nil {
			return "", "", fmt.Errorf("task_submit: %w", err)
		}
		if parent == nil {
			// MERGE NOTE (union, epic 01a10ab4): master's side is the named
			// constant (Rule 1/13 — the literal "parent_task_id %q does not
			// exist" has exactly one definition site, taskParentMissingFormat
			// at internal/tools/tasks.go), the branch's side is the
			// three-value return TaskSubmitReturningID requires. Both are
			// kept: no master behaviour was reverted.
			return "", "", fmt.Errorf("task_submit: "+taskParentMissingFormat, parentID)
		}
	}

	taskID := taskIDNormalize(opts.TaskID)
	// uuid-taskid: every AUTO submit mints its id UP FRONT (uuid v7 — the
	// same generator insertTaskRow would use) so the sessions/<uuid>/
	// container can be created and stamped BEFORE the row exists — the
	// submit contract is now "mint uuid → build sessions/<uuid>/
	// (metadata.json + attachments/) → copy attachments → insert row
	// returning the uuid". An explicit --task-id skips the mint (test
	// fixtures keep t-* ids); its collision is pre-checked HERE so a
	// container is never created under a live task's id.
	//
	// THE OPERATOR FACE'S EXEMPTION HAS A CONDITION, and it is enforced above
	// (epic 01a12b5c): a non-uuid id is accepted unless the submitter DECLARED
	// itself an operator face (RefuseUngroundableID) and the row names a repo —
	// a repo-aware row whose id is not a full uuid can never name a ground, so
	// TaskSubmitGroundNameRefusal refuses it, before this container, before the
	// attachment copies and before the INSERT. A fixture that leaves the flag
	// false keeps its `t-*` ids: the flag is what distinguishes a human's typed
	// id from a test's seed row, the one fact the engine cannot measure (see
	// internal/tools/task_submit_ground_name.go for the measured 410-test
	// reason the unconditional form was rejected).
	minted := taskID == ""
	if minted {
		id, err := taskIDGenerate(ctx, db)
		if err != nil {
			return "", "", fmt.Errorf("task_submit: mint task id: %w", err)
		}
		taskID = id
	} else {
		taken, err := taskIDExists(ctx, db, taskID)
		if err != nil {
			return "", "", err
		}
		if taken {
			return "", "", fmt.Errorf("task: task_id %q already exists", taskID)
		}
	}
	// Phase-1 attachment archiving: the replica paths in the attachments
	// manifest — and the [attachmentN:] EPIC index derived from them at run
	// time — must be fully determined BEFORE the row is inserted (the EPIC
	// block is composed from the stored manifest, and a claimed task never
	// re-copies). The copies land in the session container, and only then
	// is the row inserted carrying the manifest in its deliverable. A copy
	// failure aborts the submit — the queue is untouched, and the partially
	// created session dir is harmless (idempotent recreate on retry).
	// manifest array. Payload cap (epic "task payload length must be caped"):
	// an oversized payload/summary is never TRUNCATED — it is preserved verbatim as a
	// task attachment and the ledger column is replaced by a one-line
	// pointer. The conversion runs HERE, on the same side of the INSERT (and
	// for the same reason) as the attachment copies below: the replica must
	// exist and be hashed before the row is claimable, because a claimed
	// task never re-materializes its briefing. Doing it in the ENGINE (not
	// the CLI) is what makes the cap hold for every submit face — the CLI
	// (@file expansion / positional argv), the task_submit tool, and the
	// internal auto-submit paths (reports, merge-backlog, adoption) whose
	// payloads are assembled from other tasks' text and can grow without
	// bound.
	//
	// Both columns share the one budget: the summary is embedded verbatim in
	// the child's briefing too (the positional form puts the whole requested
	// text there), so an oversized summary is the same context hazard as an
	// oversized payload. An oversized summary WITH a distinct payload is
	// preserved in the SAME attachment (nothing is dropped) and the summary
	// column keeps a clipped first line as the ledger headline.
	spilledText := ""
	summarySpill := taskInlineSpillCandidate(opts.Summary)
	payloadSpill := taskInlineSpillCandidate(opts.Payload)
	switch {
	case summarySpill != "" && payloadSpill != "" && opts.Payload != opts.Summary:
		spilledText = opts.Summary + taskSpilledTextMerge + opts.Payload
	case summarySpill != "":
		spilledText = opts.Summary
	case payloadSpill != "":
		spilledText = opts.Payload
	}
	var spillAtt TaskAttachment
	if spilledText != "" {
		spillAtt, err = taskSpillPayload(taskID, spilledText)
		if err != nil {
			return "", "", fmt.Errorf("task_submit: %w", err)
		}
	}
	var attManifest []TaskAttachment
	if len(opts.Attachments) > 0 {
		attManifest, err = TaskCopyAttachments(taskID, opts.Attachments)
		if err != nil {
			return "", "", fmt.Errorf("task_submit: attachments: %w", err)
		}
	}
	// A spilled payload occupies the FIRST manifest slot (the submitter's
	// --attachment args keep their order and are indexed from 2 on): the
	// pointer rendered into the ledger column, the [attachmentN:] index the
	// child reads, and task show's per-entry lines must all name the same
	// entry.
	if spilledText != "" {
		attManifest = append([]TaskAttachment{spillAtt}, attManifest...)
		if opts.Summary == "" {
			// Summary-only row (the engine's registration shape — no payload
			// at all): the pointer lands in the summary column and the row
			// stays payload-less.
			opts.Summary = taskSummaryPointerText(taskID, spilledText, spillAtt)
		} else {
			// A payload column that was non-empty ALWAYS receives the
			// pointer — including the positional form, whose mirrored
			// payload equals the summary. Dropping it back to empty would
			// make the row invisible to every dispatcher (TaskNextFiltered's
			// bug028 payload guard).
			opts.Payload = taskPayloadPointerText(taskID, spilledText, spillAtt)
			if summarySpill != "" {
				opts.Summary = taskSummaryHeadline(opts.Summary)
			}
		}
	}
	// Minted submits always materialize their container + skeleton
	// metadata.json now (SPEC db-cleanup-uuid-taskid item 2: the pending
	// row's session dir is visible the moment the uuid is returned). The
	// container was already created by TaskCopyAttachments when attachments
	// exist (idempotent re-ensure here); the metadata write is best-effort
	// — the container + row are the truth, a metadata failure is logged and
	// never fails the enqueue (matches run-time writeMetadataJSON's
	// contract). Explicit --task-id submits without attachments create no
	// container (unchanged behavior for fixtures).
	if minted {
		if _, err := taskEnsureTaskSessionDirs(taskID); err != nil {
			return "", "", err
		}
		taskWriteSubmitMetadata(taskID, opts, attManifest)
	}
	id, err := insertTaskRow(ctx, db, taskCreateFields{
		TaskID:  taskID,
		Summary: opts.Summary,
		Status:  taskStatusPending,
		// THE FILER, NOT THE RUN (2026-10-28). TaskSubmit's SessionID has always
		// been read as "who filed this row" (it is the anti-self-reference key and
		// the value `submitted_by` renders), and the row is queued pending — its
		// run does not exist yet. So it fills the submitter column and leaves the
		// run-session cell empty for the run that will start.
		SubmitterSession: opts.SessionID,
		Model:            opts.Model,
		ParentTaskID:     opts.ParentTaskID,
		ParentTaskIDs:    opts.ParentTaskIDs,
		Tags:             opts.Tags,
		Payload:          opts.Payload,
		Source:           opts.Source,
		Deliverable:      taskDeliverableSubmitJSON(repo, attManifest),
		Priority:         opts.Priority,
	})
	if err != nil {
		return "", "", fmt.Errorf("task_submit: insert: %w", err)
	}
	taskID = id
	out := fmt.Sprintf("Task queued: %s (status=pending, source=%s, tags=%s)", taskID, opts.Source, orDash(opts.Tags))
	// A REPO SUBSTITUTION the caller's own resolution made is reported HERE,
	// beside the id and the tags, because this string is what the tool returns
	// to the model and the CLI prints to the operator (Rule 15 — a silent
	// relocation of a row's repository is exactly the kind of quiet rewrite the
	// clone-ground arm must never perform).
	if opts.RepoNote != "" {
		out += "\n" + opts.RepoNote
	}
	// THE PAYLOAD WAS A FILE REFERENCE AND WAS READ (task_payload_fileref.go):
	// reported exactly where the repo substitution and the spill note are,
	// because this string is what the tool returns to the model and the CLI
	// prints to the operator. A 2.7 KB briefing and a 88-byte path both "queue
	// successfully" — the one thing that tells them apart is this line.
	if payloadFileRefNote != "" {
		out += "\n" + payloadFileRefNote
	}
	// The spill is reported where the submitter looks (the confirmation the
	// CLI prints and the task_submit tool returns): a payload that was
	// converted to an attachment must never look like an ordinary inline
	// submit (Rule 15).
	if spilledText != "" {
		out += "\n" + taskSubmitPayloadSpillNote(taskID, spilledText, spillAtt)
	}
	// FALSE-CLAIM warning (Rule 15): the prose this row now carries asserts
	// that evidence is attached, but the manifest is empty — so the row's
	// reader would look for an [attachmentN:] block that will never appear
	// (the measured defect: task 01a0b53b). Report it where the submitter
	// looks, the same place the spill note lands, and NEVER refuse the row:
	// the claim is a diagnosability defect, not a reason to block a
	// dispatch, and the PROSE (not this warning) is what the reader sees —
	// the read-time marker in TaskShow is the reader-facing half of the same
	// predicate.
	//
	// Scanned on the rows' FINAL columns (after a payload spill replaced the
	// text with a one-line pointer): a spilled payload's pointer is not the
	// author's prose, and a claim inside the spilled TEXT is not lost — that
	// text is preserved verbatim as attachment1, so the manifest is
	// non-empty and the claim is true. Scanning the pre-spill text here
	// would report a mismatch that does not exist.
	if len(attManifest) == 0 {
		if phrase, ok := TaskAttachmentClaimScan(opts.Summary, opts.Payload); ok {
			out += "\n" + TaskSubmitAttachmentClaimWarningText(phrase)
		}
	}
	// HOST-OWNERSHIP warning (Rule 40): a hermes-agent row is HP's work, and
	// a hermes-agent PATH exists on both hosts — so the repo verification
	// above (which passed) says nothing about WHICH host this row was filed
	// on. The measured misroute (2026-10-03, 01a0fe63 → 01a0fe67) is exactly
	// that: verified repo, wrong ledger, a row no worker would ever claim.
	// Reported where the submitter looks and NEVER refused — the row is
	// dispatched by the ledger that holds it, and a submit that blocks on a
	// hostname guess would block the legitimate HP-side submit too.
	if w := TaskHermesHostWarning(repo); w != "" {
		out += "\n" + w
	}
	// REPO-DECLARED warning (the uppercase-checkout family, rows 01a159a9 and
	// the repeats). The git verification above answers "is this a work tree"
	// and says nothing about "does this repository declare a build contract" —
	// two different questions, and the settings `repos` table answers only the
	// second. A submit that passes the first and fails the second queues a row
	// that dispatch refuses TERMINALLY in the clone channel ("declares no build
	// command in settings repos"). Reported where the submitter still has the
	// command in front of them, and NEVER refused — the same shape as the host
	// warning above, for the same reason: the row is dispatchable on a host
	// whose clone channel is off.
	if w := TaskSubmitRepoUndeclaredWarning(repo); w != "" {
		out += "\n" + w
	}
	// BASE-STALENESS warning (epic 01a13715, the authoring-time half of
	// epic 01a136a7): a payload that declares the revision its evidence was
	// measured at (`base <rev>`) can be filed with a base the integration
	// branch has ALREADY moved past — the measured case is row 01a136a7-dccd,
	// which copied `base 2f697e5` 14 minutes after the repair it demanded
	// became master's tip. The dispatch seam re-measures the ASSERTION half
	// (task_briefing_premise.go); a base DECLARATION asserts nothing, so
	// nothing disagreed with it. This is the same measurement one step
	// earlier, where the author can still fix it: a warning, NEVER a refusal
	// (the row queues either way — a moved base is a fact about the
	// briefing's AGE, not about the work). Measured on the same DURABLE repo
	// the row's deliverable stamps, and on the payload column as it stands
	// after the spill decision.
	if w := TaskSubmitBaseStalenessWarning(repo, opts.Payload); w != "" {
		out += "\n" + w
	}
	// DEAD-TEST-NAME warning (bug row 01a1ac1a, the WRITE FACE of the
	// `[no tests to run]` family). The two dispatch-seam clauses
	// (TaskBriefingRunAlternativesRefresh, TaskBriefingProseTestNamesRefresh)
	// measure a briefing's test names AFTER it is filed; scripts/test-budget.sh
	// measures the run. The MOMENT the names are authored — this INSERT — was
	// the one that told nobody, and the measured cost is a row whose whole
	// acceptance command was `go test ./cmd/agent/ -run 'TestResumeSeamProbe'`:
	// `ok … 0.589s [no tests to run]`, exit 0, indistinguishable from a pass
	// four milliseconds away. Warned where the author still has the command in
	// front of them, and NEVER refused — a briefing may legitimately name a test
	// the child is being asked to WRITE (the same boundary the dispatch clause
	// states), so a refusal here would block exactly that epic. Same shape as
	// the host, repo-declared and base-staleness warnings above, and the same
	// columns: the summary plus the payload as they stand after the spill.
	if w := TaskSubmitDeadTestNameWarning(repo, opts.Summary, opts.Payload); w != "" {
		out += "\n" + w
	}
	// PROSE-DEPENDENCY warning (the WRITE FACE of the "dependency lives only in
	// the prose" family, measured 2026-11-22 on rows 01a20059-*; defect row
	// 01a20076-96f6). A briefing that says it waits on another row while
	// parent_task_id / parent_task_ids / `task_group:` are ALL empty is offered
	// `[next]` at once by `gopi task list --status pending`, so a dispatcher
	// starts it in PARALLEL with the work it says it waits for — and the
	// collision surfaces only at merge time. Step 4 of the measured epic also
	// showed the deeper half: it waited on step 1, not on the chain tail, and a
	// parent edge can express only "my predecessor is X", so the submitter's
	// intent was unrepresentable in the ledger even when it remembered.
	//
	// Warned where the author still has the command in front of them, and NEVER
	// refused — the same shape as the host, repo-declared, base-staleness and
	// dead-test-name warnings above: a consumer that waits on a SET expresses
	// that in prose (Rule 33b), a briefing may quote another row's edge while
	// being about something else, and a refusal would let a wording nit block a
	// dispatch through the funnel every internal auto-submit path passes.
	//
	// The ledger half is read off the FINAL columns (opts.ParentTaskID /
	// opts.ParentTaskIDs after the parent fold above, and the payload as it
	// stands after a spill decision) — the same columns the row will carry, so
	// the warning and the dispatch gate can never disagree about whether an edge
	// exists (Rule 14).
	if w := TaskSubmitProseDependencyWarning(opts.Summary, opts.Payload, opts.ParentTaskID, opts.ParentTaskIDs); w != "" {
		out += "\n" + w
	}
	// Bug-flow adoption stats (P2.5, Rule 11): every 'bug'-tagged submission
	// is echoed to stdout as a one-line running tally so a `gopi task
	// submit --tags bug` (the tool and the CLI share this engine) makes the
	// adopt rate measurable in the session log itself. Stats are advisory —
	// a stats failure NEVER fails the enqueue (the task row is already in).
	if hasBugTag(opts.Tags) {
		if stats, statsErr := TaskBugAdoptionStats(ctx, db); statsErr == nil {
			out += "\n" + stats
		}
	}
	return out, taskID, nil
}

// taskSubmitRemote is the MIRROR-LEG arm of TaskSubmit: this node's ledger
// taskParentsForForwardLabel renders the parents a remote submit would have
// carried, in the ONE spelling the forward refusal quotes (Rule 1b): the old
// column's id first, then the rest, so the sentence names exactly what the
// caller asked for and which of the two columns it came from.
func taskParentsForForwardLabel(first, rest string) string {
	ids := taskParentIDsOf(first, rest)
	if len(ids) == 1 {
		return fmt.Sprintf("parent_task_id %q", ids[0])
	}
	return fmt.Sprintf("parent_task_id %q and parent_task_ids %q", ids[0], strings.Join(ids[1:], taskParentIDsSeparator))
}

// is a mirror of a central one, so the row must be created on the
// AUTHORITATIVE node. The local ledger is NOT opened/created by this
// function (a gate failure must not leave a black-hole row behind); the only
// local write is the echo that mirrors the accepted row back.
//
// Sequence: verify the requested repo → mint an id unique in BOTH ledgers →
// forward the row through the peer's session-import verb → echo the receipt
// into the local ledger. Every failure is a loud refusal naming the
// authoritative ledger, the local path and the reason.
func taskSubmitRemote(opts TaskSubmitOptions, ctx context.Context, localDBPath, peer string) (string, error) {
	// The sync row carries the work DEFINITION (summary/payload/tags/source/
	// status/deliverable): the peer's import writes those columns and leaves
	// session/parent EMPTY by design (harvestInsert). A parent_task_id would
	// therefore be dropped ON THE WAY — a divide-and-conquer tree silently
	// losing its edge. Rule 15b: refuse and say why, naming the two honest
	// remedies, rather than forward a row whose tree link vanishes.
	// EVERY parent is checked, not just the first: the refusal's whole point is
	// that the wire cannot carry the edge, and a row with three parents would
	// otherwise lose two of them silently (Rule 15).
	if first, rest := opts.ParentTaskID, opts.ParentTaskIDs; first != "" || rest != "" {
		return "", taskSubmitForwardRefusal(localDBPath, peer,
			fmt.Errorf("%s cannot be forwarded: the sync row carries no parent column, so the child would arrive on the authoritative ledger WITHOUT its tree link (submit the parented row on the authoritative node itself, or submit the child unparented)", taskParentsForForwardLabel(first, rest)))
	}
	// Same class as the parent arm above: SyncTaskRow carries no priority
	// column, and the peer's import DERIVES priority from the tags
	// (taskTagPriority). An explicit --priority therefore cannot travel —
	// forwarding it would silently down-rank/lose the submitter's choice on
	// the ledger that actually dispatches the row. The comparison is against
	// the tag-derived tier, never `> 0`: TaskSubmit already defaulted the
	// field from the tags, so an equality here means "the tags say the same
	// thing" and the row forwards with the tier it would have had anyway.
	if opts.Priority != taskTagPriority(opts.Tags) {
		return "", taskSubmitForwardRefusal(localDBPath, peer,
			fmt.Errorf("priority %d cannot be forwarded: the sync row carries no priority column, so the peer's import would re-derive %d from the tags alone and DROP the explicit choice (submit with tags that derive this tier, or submit on the authoritative node itself)", opts.Priority, taskTagPriority(opts.Tags)))
	}
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = taskSubmitResolveDefaultRepo()
	}
	opts.Repo = repo
	if repo != "" {
		if err := taskRepoVerifiable(repo); err != nil {
			return "", fmt.Errorf("task_submit: %w", err)
		}
	}
	localIDs, err := taskSubmitLocalIDs(ctx, localDBPath)
	if err != nil {
		return "", err
	}
	taskID := taskIDNormalize(opts.TaskID)
	if taskID == "" {
		taskID, err = taskSubmitMintForwardID(localIDs)
		if err != nil {
			return "", err
		}
	} else if localIDs[taskID] {
		return "", fmt.Errorf("task: task_id %q already exists", taskID)
	}
	opts.TaskID = taskID
	// Attachment archiving + the payload spill happen on THIS node's session
	// container (the container is named by the task id, which the peer's
	// copy shares), through the SAME primitives the local arm uses.
	spilledText := ""
	summarySpill := taskInlineSpillCandidate(opts.Summary)
	payloadSpill := taskInlineSpillCandidate(opts.Payload)
	switch {
	case summarySpill != "" && payloadSpill != "" && opts.Payload != opts.Summary:
		spilledText = opts.Summary + taskSpilledTextMerge + opts.Payload
	case summarySpill != "":
		spilledText = opts.Summary
	case payloadSpill != "":
		spilledText = opts.Payload
	}
	var spillAtt TaskAttachment
	if spilledText != "" {
		spillAtt, err = taskSpillPayload(taskID, spilledText)
		if err != nil {
			return "", fmt.Errorf("task_submit: %w", err)
		}
	}
	var attManifest []TaskAttachment
	if len(opts.Attachments) > 0 {
		attManifest, err = TaskCopyAttachments(taskID, opts.Attachments)
		if err != nil {
			return "", fmt.Errorf("task_submit: attachments: %w", err)
		}
	}
	if spilledText != "" {
		attManifest = append([]TaskAttachment{spillAtt}, attManifest...)
		if opts.Summary == "" {
			opts.Summary = taskSummaryPointerText(taskID, spilledText, spillAtt)
		} else {
			opts.Payload = taskPayloadPointerText(taskID, spilledText, spillAtt)
			if summarySpill != "" {
				opts.Summary = taskSummaryHeadline(opts.Summary)
			}
		}
	}
	if _, err := taskEnsureTaskSessionDirs(taskID); err != nil {
		return "", err
	}
	taskWriteSubmitMetadata(taskID, opts, attManifest)
	out, err := taskSubmitForward(opts, repo, attManifest, localDBPath, peer)
	if err != nil {
		return "", err
	}
	if spilledText != "" {
		out += "\n" + taskSubmitPayloadSpillNote(taskID, spilledText, spillAtt)
	}
	return out + "\nledger: " + taskSubmitLedgerTargets(localDBPath, peer), nil
}

// TaskNextResult is one atomic pickup outcome. Task is the claimed row
// (payload included — the briefing for task run); Empty is true when no
// eligible task exists (a normal, non-error outcome).
type TaskNextResult struct {
	Task  *Task
	Empty bool
}

// TaskNext atomically claims the oldest eligible pending task (CAS conditional
// UPDATE — the /home/user/work/tasks pool.go Claim pattern). Two concurrent claimers
// race on the same statement: exactly one sees RowsAffected()==1, the loser
// re-selects and retries until it claims a different task or the queue is
// empty. excludeSession skips tasks derived from that session
// (anti-self-reference, ADDENDUM #5); claimedBy stamps the CAS trail.
// Non-legacy statuses (claimed/in_progress by another instance) are never
// eligible — a claimed row is owned until it is written back.
func TaskNext(claimedBy, excludeSession string) (*TaskNextResult, error) {
	return TaskNextFiltered(claimedBy, excludeSession, "")
}

// taskGuardBannedTokens are the known FOREIGN-project words (vcmi, evennia):
// a pending row with no deliverable.repo whose text mentions one of these is
// likely cross-project work — executing it here would make the runner hunt
// for files in the gopi tree (the t-1788062477 vcmi-class disaster). The
// list is small and advisory; the structured deliverable.repo stamp (M4.9)
// is the real closure.
var taskGuardBannedTokens = []string{"vcmi", "evennia"}

// TaskRepoGuardWarnings inspects one task row and returns repo-guardrail
// warnings (empty = clean). The guard fires only when BOTH hold: (a) the
// deliverable stamp carries no "repo" key (the row never said where the
// work happens), and (b) summary or payload mentions a known foreign-project
// token. It is ADVISORY — a warning never blocks the claim; TaskStart/serve
// prints it at pickup time so the runner knows before it starts hunting.
func TaskRepoGuardWarnings(t *Task) []string {
	if t == nil {
		return nil
	}
	// (a) deliverable.repo present? The stamp is a JSON blob (M4.8) — a
	// row with any repo recorded is guarded by the real mechanism.
	var deliv map[string]interface{}
	if strings.TrimSpace(t.Deliverable) != "" {
		_ = json.Unmarshal([]byte(t.Deliverable), &deliv) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
	}
	if _, ok := deliv["repo"]; ok {
		return nil
	}
	// (b) foreign-project token in summary or payload?
	hay := strings.ToLower(t.Summary + "\n" + t.Payload)
	for _, tok := range taskGuardBannedTokens {
		if strings.Contains(hay, tok) {
			return []string{
				fmt.Sprintf("task %s has no deliverable.repo but its text mentions %q — cross-project guard: verify this work actually belongs in the current repo (gopi) before executing; consider cancel + resubmit with --repo in the owning project's ledger", t.TaskID, tok),
			}
		}
	}
	return nil
}

// TaskRepoGuardWarn is the convenience entry for the CLI pickup path:
// loads the row by task_id and returns its guard warnings (a missing row is
// a caller error upstream — here it just yields no warnings).
func TaskRepoGuardWarn(taskID string) []string {
	taskID = taskIDNormalize(taskID)
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil || t == nil {
		return nil
	}
	return TaskRepoGuardWarnings(t)
}

// TaskNextFiltered is TaskNext with a tag-skip filter: skipTags (comma
// separated) makes rows carrying ANY of those tags ineligible — the serve
// daemon's --skip-tag anti-misclaim guard (a daemon must never swallow rows
// tagged for human/other-dispatcher handling, e.g. pa-queue).
func TaskNextFiltered(claimedBy, excludeSession, skipTags string) (*TaskNextResult, error) {
	claimedBy = strings.TrimSpace(claimedBy)
	if claimedBy == "" {
		claimedBy = taskDefaultClaimHolder()
	}
	excludeSession = strings.TrimSpace(excludeSession)
	skipSet := map[string]bool{}
	for _, tg := range strings.Split(skipTags, ",") {
		if tg = strings.TrimSpace(tg); tg != "" {
			skipSet[tg] = true
		}
	}

	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// DISPATCH-TIME STALENESS GUARD (epic merge-verify-rework-staleness):
	// a merge-verify rework row's briefing is frozen at filing time, so its
	// premise can already be FALSE by the time it is claimed (measured: row
	// 01a09e9b sat pending ~7h while its fix was re-landed on master as
	// fc9b06d4). The judgement runs HERE — the single claim funnel every
	// dispatcher shares (worker round / serve dispatch / `task run next`) —
	// and a stale row is CLOSED as a no-op instead of being dispatched, so no
	// worker is spawned for work already shipped. Runs at most once per claim
	// attempt (the lane's rows are tag-narrowed).
	if note := MergeVerifyStaleReworkNoOpSummary(mergeVerifyStaleSweepPending(db)); note != "" {
		// stderr, never stdout: this funnel's callers pipe stdout (the CLI's
		// human output, machine consumers) and a guard line on stdout would
		// corrupt a caller's stream. The same line is recorded on the ledger
		// (the cancelled row's reason + event), so nothing depends on it
		// being visible here.
		fmt.Fprintln(os.Stderr, note)
	}

	// DISPATCH-TIME GHOST-REF GUARD (epic safe-merge-existence-ancestry, defect
	// ②): a merge-backlog row's whole premise is "this branch is unmerged work
	// waiting to land", and NOTHING re-checked that the branch still EXISTS
	// before a session was spent on it. Measured 2026-09-23 (row
	// 01a0c614-00aa-7e17-8be5-b37ef5a0fcec): an e2e fixture's cleanup deleted
	// `feat/e2e-orphan-probe1` and left the live merge row behind, so a solver
	// was dispatched — and paid for — onto a ref that cannot be merged by any
	// channel. The judgement runs HERE, at the single claim funnel every
	// dispatcher shares, and a ghost row is CLOSED (cancelled, by name) instead
	// of being dispatched. It sits at claim time rather than inside
	// taskRowClaimable on purpose: that predicate is pure over ROW FIELDS (Rule
	// 14) and answers the pending view's [queue-skip] marker too — folding a git
	// query into it would make the marker depend on the reader's own cwd.
	if note := MergeBacklogGhostSweepSummary(mergeBacklogGhostSweepPending(db)); note != "" {
		fmt.Fprintln(os.Stderr, note) // same stderr discipline as the line above
	}

	now := float64(time.Now().Unix())
	for attempt := 0; attempt < 10; attempt++ {
		// Selection: eligible pending in priority order (M4.11: priority
		// ASC — 1 bug first, 8 docs last — created_at ASC tiebreak). All
		// rows are walked so no LIMIT 1; the filters below decide the
		// first SURVIVING row. excludeSession enforces anti-self-reference.
		//
		// THE PREDICATE READS THE SUBMITTER, IN TWO STORAGE SHAPES (2026-10-28).
		// The disqualifying fact is "the claiming session FILED this row", and it
		// used to be read off `session_id` — where the INSERT wrote the submitter.
		// That column now holds the row's RUN SESSION, so the submitter moved to
		// its own column and this predicate reads THAT.
		//
		// THE LEGACY ARM IS NOT DEAD WEIGHT: rows written before the split carry
		// an EMPTY submitter column and still hold the submitter in `session_id`
		// (historical rows are never rewritten), so the second arm keeps them
		// covered. It is fenced with "and this is not simply the row's own id"
		// only for the degenerate case where an auto/human row's session id and
		// task id coincide — the fence cannot hide a submitter, because a value
		// equal to the row's own id is by definition not a foreign session.
		sel := `SELECT ` + taskSelectColumns + ` FROM tasks WHERE status = ?`
		args := []any{taskStatusPending}
		if excludeSession != "" {
			sel += ` AND (` + taskSubmitterColumnName + ` IS NULL OR ` + taskSubmitterColumnName + ` = '' OR ` +
				taskSubmitterColumnName + ` != ?)` +
				` AND (session_id IS NULL OR session_id = '' OR session_id != ? OR session_id = task_id)`
			args = append(args, excludeSession, excludeSession)
		}
		// M4.11 queue order — the ONE claim-order definition (Rule 14):
		// shared with TaskList's pending view, so what the operator READS as
		// "next" is what this funnel CLAIMS next.
		sel += taskClaimOrderBy
		// No LIMIT 1: filtered rows must be walked past (the oldest eligible
		// row is the first row that survives both filters below).
		//
		// DEPENDENCY GATE INDEX, read BEFORE the row cursor opens. The ledger
		// openers pin a SINGLE connection (internal/tools/memory.go
		// SetMaxOpenConns(1) — SQLite serializes writers), so a parent-status
		// query issued while the candidate cursor is open would block on a
		// connection that cursor holds. The read is conditional on the queue
		// actually carrying a parented row (probed with a cheap COUNT before
		// the map query), which keeps a parentless queue — the common case,
		// measured 0 of 23 pending rows on 2026-09-23 — paying one extra
		// aggregate and never the whole-table map. Rebuilt per attempt rather
		// than carried across retries: a retry happens because another claimer
		// moved the queue, so the parent statuses must be as fresh as the row
		// set.
		var parentStatus map[string]string
		indexNeeded, perr := taskDependencyIndexNeeded(ctx, db)
		if perr != nil {
			return nil, fmt.Errorf("task next: parent index: %w", perr)
		}
		if indexNeeded {
			ps, perr := taskParentStatuses(ctx, db)
			if perr != nil {
				return nil, fmt.Errorf("task next: parent status: %w", perr)
			}
			parentStatus = ps
		}
		rows, err := db.QueryContext(ctx, sel, args...)
		if err != nil {
			return nil, fmt.Errorf("task next: select: %w", err)
		}
		var cand *Task
		for rows.Next() {
			c, err := scanTask(rows)
			if err != nil {
				rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
				return nil, fmt.Errorf("task next: scan: %w", err)
			}
			// Eligibility filters — a filtered row is SKIPPED (continue
			// scanning), never a queue-empty verdict (ONE predicate, shared
			// with the pending view's [queue-skip] marker — Rule 14):
			//   - --skip-tag anti-misclaim: a row tagged with ANY skipped
			//     tag belongs to another dispatcher/human (pa-queue);
			//     returning Empty here head-blocked the whole queue behind
			//     an old tagged row (t-1788053263). NOTE: the pending LIST
			//     renders with no skip set (it shows the whole ledger group),
			//     so the marker only claims unclaimability for the
			//     skip-set-independent reasons.
			//   - payload-less rows are ledger entries, not dispatchable
			//     work: serving one hard-refuses in runTaskRun and the
			//     daemon os.Exit(1)s (bug028, e3ca35ca bare-briefing incident).
			//   - merger-resolve rows are NEVER worker-dispatchable (see the
			//     long note in taskRowClaimable).
			//   - a row whose PARENT has not been released (parent status
			//     outside {completed, verified}, or a parent id naming no row
			//     at all) is dependency-parked (see the long note in
			//     taskRowClaimable).
			if !taskRowClaimable(c.TaskID, c.Tags, c.Payload, taskParentIDsOf(c.ParentTaskID, c.ParentTaskIDs), parentStatus, skipSet) {
				continue
			}
			cand = c
			break
		}
		rowsErr := rows.Err()
		rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
		if rowsErr != nil {
			return nil, fmt.Errorf("task next: %w", rowsErr)
		}
		if cand == nil {
			return &TaskNextResult{Empty: true}, nil
		}

		// CAS: conditional UPDATE — exactly one concurrent claimer wins. The
		// claim-time process identity rides the same statement (epic
		// task-run-singleton-guard) so the claimed window is guarded too.
		identSet, identArgs := taskRunClaimIdentitySet(claimedBy)
		res, err := db.ExecContext(ctx,
			`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, updated_at = ?, `+identSet+`
			 WHERE task_id = ? AND status = ?`,
			append([]any{taskStatusClaimed, claimedBy, now, now}, append(identArgs, cand.TaskID, taskStatusPending)...)...)
		if err != nil {
			return nil, fmt.Errorf("task next: claim: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("task next: claim rows: %w", err)
		}
		if n == 1 {
			cand.Status = taskStatusClaimed
			cand.ClaimedBy = claimedBy
			cand.ClaimedAt = now
			cand.HasClaim = true
			// Transition hook: pending → claimed (the CAS won, so the row
			// is committed — notify with the in-memory candidate, enriched
			// with claim fields, no re-read needed). The predecessor is the
			// pre-claim candidate snapshot so the payload's
			// previous_status is carried by the row itself, not a literal.
			claimBus := taskNotifyBus{}
			claimBus.observe(taskNotifyPrevCopy(cand, taskStatusPending), cand)
			taskNotifyFlush(&claimBus)
			return &TaskNextResult{Task: cand}, nil
		}
		// Lost the race (another instance claimed it between SELECT and
		// UPDATE): loop and pick the next eligible row.
	}
	return nil, fmt.Errorf("task next: claim race did not settle after 10 attempts")
}

// claimHolderHostPidSep is the ONE separator joining the host and pid
// components of a claim-holder stamp (`host:pid`). Single source of truth
// (Rule 13): the renderer below, the parser (claimHolder) and the
// self-ownership matcher (claimHostIsSelf) all build/read the stamp through
// this constant, so the format can never drift piecewise.
const claimHolderHostPidSep = ":"

// taskDefaultClaimHolder is the CAS trail default when no --as holder is
// given: hostname:pid identifies the claiming instance (single-machine
// dispatcher; multi-host would add a hostname prefix anyway).
func taskDefaultClaimHolder() string {
	host, err := os.Hostname()
	if err != nil {
		host = syncUnknownHost
	}
	return fmt.Sprintf("%s%s%d", host, claimHolderHostPidSep, os.Getpid())
}

// matchesSkipTags reports whether the comma-separated row tags contain any
// tag in the skip set (the --skip-tag filter).
func matchesSkipTags(rowTags string, skipSet map[string]bool) bool {
	if len(skipSet) == 0 {
		return false
	}
	for _, tg := range strings.Split(rowTags, ",") {
		if skipSet[strings.TrimSpace(tg)] {
			return true
		}
	}
	return false
}

// taskParentReleaseStatuses is the EXACTLY-closed RELEASE SET of the
// dependency gate: a child becomes claimable only once its parent row's
// status is one of these. Every other parent status — pending, claimed,
// in_progress, failed, cancelled — parks the child indefinitely. There is
// deliberately NO override, NO cascade cancel and NO timeout/staleness
// release (a failed parent, or a plainly cancelled one, parks its children
// FOREVER, by design: the parked rows are visible in the pending view as
// [queue-skip] with the parent's status named, and the operator's resolution
// is to finish the parent or cancel the child). Rule 1b/14: the set is
// spelled here once and read by both the predicate and the rendered reason.
//
// WHY superseded IS A MEMBER AND cancelled IS NOT (epic
// parent-release-statuses, 2026-10-07). The gate asks one question: "is the
// dependency the child waited on SATISFIED?" — not "did the parent's own run
// finish?" A completed/verified parent's work is DONE here; a superseded
// parent's work is DONE ELSEWHERE (migrated to a peer ledger's row, or the
// chain was re-cut and a successor carries it), so the blocked children are
// unblocked. A cancelled parent says the opposite — ABANDONED, nothing
// delivered — and its children stay parked, which is the conservative arm
// that must not be relaxed (a child released by an abandoned parent builds on
// a premise that never held). `failed` is in the same boat.
//
// THE RISK THIS OPENS, named so a later reader can weigh it: `cancelled` is
// the ONLY status the ledger has for "retire this row", so a caller that
// cancels a row whose work was NOT done and writes `superseded` anyway would
// release children onto nothing. That is why the status is named for the
// DECISION (the work is superseded elsewhere) and not for the mechanism
// ("migration"): a migration is supersession only because a successor row
// exists and carries the work — see devdoc/task-parent-release-statuses.md.
var taskParentReleaseStatuses = map[string]bool{
	taskStatusCompleted:  true,
	taskStatusVerified:   true,
	taskStatusSuperseded: true,
}

// taskParentIDsOf is the ONE reader of a row's dependency parents (EPIC
// multi-parent). A row's parents are, IN THIS ORDER:
//
//  1. parent_task_id  — the single-value column that has always existed
//     (parent 1; the ONLY parent a pre-epic row has, and the only one an
//     older binary or an older sync leg can even see);
//  2. parent_task_ids — parents 2..N, comma-separated.
//
// THE ORDER IS THE COMPATIBILITY CONTRACT, not a preference: reading the old
// field first and then accumulating the new list is what makes a historical
// row (no second column) come out as exactly [parent_task_id] — byte-for-byte
// the single parent behaviour of every release before this one. Every
// consumer of a row's parents goes through THIS function; a caller that reads
// the two columns itself would be a second definition of the rule (Rule 14)
// and the gate would then be able to disagree with what a renderer shows.
//
// Deduplicated and whitespace-trimmed, so a hand-edited cell or a list that
// repeats parent 1 cannot make the gate judge one parent twice. Empty ids are
// dropped ("" is "no parent", never a parent named "").
func taskParentIDsOf(parentTaskID, parentTaskIDs string) []string {
	out := make([]string, 0, 1+strings.Count(parentTaskIDs, taskParentIDsSeparator))
	seen := make(map[string]bool, cap(out))
	for _, raw := range append([]string{parentTaskID},
		strings.Split(parentTaskIDs, taskParentIDsSeparator)...) {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// TaskParentIDsJoin renders parents 2..N as the encoding
// TaskSubmitOptions.ParentTaskIDs expects (EPIC multi-parent). The CLI holds
// the caller's typed order as a slice and must hand parents 2..N to the engine
// without retyping the separator — the spelling lives in exactly one place
// (Rule 1b/13), here, beside the column name it encodes for.
func TaskParentIDsJoin(parents []string) string {
	return strings.Join(parents, taskParentIDsSeparator)
}

// taskParentsForWrite is the ONE writer-side split (EPIC multi-parent): parent
// 1 goes to parent_task_id, parents 2..N go to parent_task_ids.
//
// WHY THE FIRST PARENT DOES NOT MOVE: an older binary on another machine (or
// an older sync leg) knows only parent_task_id. Writing parent 1 there means
// such a reader still sees "this row has a parent" and keeps the dependency —
// a row whose parents ALL lived in the new column would arrive looking
// PARENTLESS, i.e. claimable at once, and the dependency would vanish with no
// error anywhere. The second column is an ADDITION, never a replacement.
//
// The caller's order is preserved and duplicates are dropped through
// taskParentIDsOf, so the two columns and the reader can never disagree about
// which parent is "first".
func taskParentsForWrite(parents []string) (first, rest string) {
	ids := taskParentIDsOf("", strings.Join(parents, taskParentIDsSeparator))
	if len(ids) == 0 {
		return "", ""
	}
	return ids[0], strings.Join(ids[1:], taskParentIDsSeparator)
}

// taskParentsReasonSeparator joins the per-parent refusal reasons of an
// AND-gate verdict (see taskParentGateDenied) — one separator, so a reader
// parsing the pending view's marker can split them by the same token the
// gate used to join.
const taskParentsReasonSeparator = ";"

// taskParentGateDenied reports whether a row's dependency edges park it, and
// if so how to say WHY for EVERY edge that blocks.
//
// TWO EDGES, ONE GATE. The PARENT edge is an AND over all parent ids (EPIC
// multi-parent): a row becomes claimable only when EVERY parent it names has
// been released. One unreleased parent is enough to keep it parked, and the
// verdict NAMES that parent along with the others that also block, so a row
// waiting on two producers reads which two rather than one anonymous "not
// released". The GROUP-COMPLETION edge (taskGroupGateDenied) is the second
// edge a row may DECLARE in its payload, for the dependency the parent column
// cannot express — "wait for a SET that is not my ancestors".
//
// The per-parent judgement is taskParentGateDeniedOne below; this function adds
// nothing to the RULE (the release set, the dangling arm, the rework arm are all
// unchanged, Rule 33), it only applies it to each edge and ANDs the answers.
// A single-parent, group-less row takes this exact path and produces the exact
// wording of every release before the group arm existed.
func taskParentGateDenied(rowTaskID, rowTags, payload string, parentIDs []string, parentStatus map[string]string) (bool, string) {
	reasons := make([]string, 0, len(parentIDs)+1)
	if len(parentIDs) > 0 {
		if parentStatus == nil {
			// ONE sentence for the parent edge, not one per parent: the index
			// is absent, so every parent is equally unjudgeable and repeating
			// the same refusal once per id says nothing a reader can act on.
			reasons = append(reasons, taskParentReasonUnread)
		} else {
			for _, parentID := range parentIDs {
				if denied, reason := taskParentGateDeniedOne(rowTags, parentID, parentStatus); denied {
					reasons = append(reasons, reason)
				}
			}
		}
	}
	// The GROUP-COMPLETION edge is judged against the SAME id → status index
	// (a second map would be a second answer to "what status is that row"),
	// and only when the row actually declares one.
	if groups := taskGroupDependenciesOf(payload); len(groups) > 0 {
		if denied, reason := taskGroupGateDenied(rowTaskID, groups, parentStatus); denied {
			reasons = append(reasons, reason)
		}
	}
	if len(reasons) == 0 {
		return false, ""
	}
	return true, strings.Join(reasons, taskParentsReasonSeparator)
}

// taskParentStatusLabel renders ONE parent's status for the row's parent block:
// the ledger's own status word when the parent exists, and an explicit absence
// when it does not. An unreadable index is SAID rather than rendered as a
// satisfied dependency (Rule 15: "I could not read this" is never "fine").
func taskParentStatusLabel(parentStatus map[string]string, parentID string) string {
	if parentStatus == nil {
		return taskParentStatusUnreadLabel
	}
	if st, ok := parentStatus[parentID]; ok {
		return st
	}
	return taskParentStatusMissingLabel
}

const (
	taskParentStatusUnreadLabel  = "status unreadable"
	taskParentStatusMissingLabel = "missing from the ledger"
)

// taskParentGateDeniedOne judges ONE parent — the whole of the pre-multi-parent
// gate, unchanged, so the AND wrapper above adds no new rule.
//
// taskParentGateDeniedOne is the single-parent gate: does ONE parent park the
// row, and if so why. This is the WHOLE of the rule that existed before
// multi-parent; taskParentGateDenied above ANDs it over a row's parents.
//
// The gate: parentID empty → not parked (every row that carries no tree link
// behaves exactly as before this gate existed). Otherwise the parent status
// is looked up in the caller-supplied index taskParentStatuses builds:
//
//   - parent row absent from the ledger (DANGLING id) → parked. A row whose
//     parent names no existing row is an unnavigable link — the same defect
//     TaskSubmit and the sync row already refuse at submit time — and it can
//     only be reached by a row that predates that validation, by a peer
//     replica whose parent has not synced yet, or by direct ledger surgery.
//     Denying is the conservative arm: an unresolvable dependency is never
//     assumed satisfied.
//   - parent row present with a status outside taskParentReleaseStatuses →
//     parked, naming the status.
//   - parent row present and released → not parked.
//
// THE SECOND EDGE SEMANTICS: a REWORK row (rowTags carries
// tasktags.ReworkLineageTag) holds a different dependency than an ordinary
// child, so it is judged by a different criterion — the parent has reached a
// TERMINAL VERDICT (taskParentTerminalStatuses), not that the verdict was a
// success. The verify FAIL verdict mints `R` with
// parent_task_id = the row they just judged, and that row is usually
// `failed`; judged by the release set alone, R is born [queue-skip] and stays
// there forever (Rule 33 has no override, no cascade, no timeout). The
// rework's own briefing says the parent is terminal and must never re-run —
// the edge's meaning is "the verdict is in", not "the verdict was good".
//
// WHAT THIS ARM DELIBERATELY DOES NOT DO: it does not widen
// taskParentReleaseStatuses. `failed`/`cancelled` stay out of the release
// set, so an ORDINARY child of a failed parent is still parked — releasing
// those would start rows built on a deliverable that was never delivered.
// The tag, never the set, is what carries the difference (precedent:
// rework_reparent_children.go, "THE FIX IS THE EDGE, NEVER THE RELEASE SET";
// the full reasoning, the measured red proof and the rejected alternative are
// devdoc/rework-parent-gate-edge-semantics.md).
// The REWORK row keeps its parent edge, so FailedNeedsReissue still sees the
// failed row has a successor and stops counting it as an orphan.
//
// A nil index is a CALLER ERROR (the predicate's read face), not a licence:
// a row that carries a parent id must be judged against real statuses, so
// the nil case denies and says the status could not be read.
func taskParentGateDeniedOne(rowTags, parentID string, parentStatus map[string]string) (bool, string) {
	if parentID == "" {
		return false, ""
	}
	if parentStatus == nil {
		return true, taskParentReasonUnread
	}
	st, ok := parentStatus[parentID]
	if !ok {
		return true, fmt.Sprintf(taskParentReasonDanglingFormat, parentID)
	}
	if taskParentReleaseStatuses[st] {
		return false, ""
	}
	// THE REWORK ARM (see the function head): a rework row waits for the
	// parent's VERDICT, whichever it was, so any terminal parent releases it.
	if taskHasTagToken(rowTags, tasktags.ReworkLineageTag) && taskParentTerminalStatuses[st] {
		return false, ""
	}
	if taskHasTagToken(rowTags, tasktags.ReworkLineageTag) {
		return true, fmt.Sprintf(taskParentReasonUnreleasedReworkFormat, parentID, st, taskParentTerminalSetLabel())
	}
	return true, fmt.Sprintf(taskParentReasonUnreleasedFormat, parentID, st, taskParentReleaseSetLabel())
}

// ── GROUP-COMPLETION DEPENDENCY (Rule 33's third edge) ──────────────────────
//
// THE DEFECT THIS CLOSES (measured 2026-11-05, this row's own briefing): a
// DISPATCHER parent's work is to SUBMIT its children, so its close-out means
// "everything is dispatched", never "the batch is done". A row that CONSUMES
// that batch is a SIBLING of the producers under the same parent, so the
// parent's release made it claimable at once — the pending view offered it
// [next] with 0 of 23 inputs on disk. parent_task_id cannot express the edge
// ("wait for a SET that is not my ancestors"), so a row may DECLARE it in its
// payload.
//
// THE SPELLING (Rule 1b: one constant, read by the writer's doc and by the
// gate): a payload LINE
//
//	task_group: <prefix>[,<prefix>...]
//
// names one or more GROUPS. A group is the set of ledger rows whose task_id
// BEGINS with the prefix — the ledger's own naming convention, because a
// dispatcher submits its children in one session-id family. `task_group:` with
// nothing after it declares an EMPTY group, which parks the row: an empty
// dependency is a premise that never held, and it is NOT the same row as one
// that declares no group at all (that one has no edge and stays claimable).
//
// THE CRITERION IS TERMINAL, NOT RELEASED. Not taskParentReleaseStatuses: a
// producer that FAILED still finished its attempt, so a consumer waits for the
// batch to be OVER, not for it to be good. That is the same distinction the
// REWORK edge below already draws, and it is what makes this arm usable at
// all — the release set could never release a group whose members were
// cancelled.
//
// SELF-EXCLUSION: a consumer whose own id falls inside the group does not
// block itself (a dispatcher's own successor is the ordinary case), and the
// reason says "(self excluded)" so the arithmetic a reader does on the ledger
// reconciles with the count in the refusal.
const (
	// TaskGroupDependencyKey is the payload line that declares a
	// group-completion dependency (see the block comment above).
	TaskGroupDependencyKey = "task_group:"
	// taskGroupDependencyLineSep splits a payload into the lines the arm is
	// parsed from: the declaration is judged LINE BY LINE, so a `task_group:`
	// sitting inside a pasted example cannot re-activate an arm the caller
	// withdrew, and one malformed line cannot silence the others.
	taskGroupDependencyLineSep = "\n"
	// taskGroupDependencyEntrySep separates the group prefixes on ONE line.
	taskGroupDependencyEntrySep = ","
	// taskGroupMissingNamed is how many un-finished member ids the refusal
	// spells out before it says "and N more"; the full set is one
	// `gopi task list --status pending` away.
	taskGroupMissingNamed = 3
	// taskGroupMembersJoin separates the member ids inside a refusal.
	taskGroupMembersJoin = ", "
)

// taskGroupDependenciesOf is the ONE reader of a row's declared group
// dependencies (Rule 14). It returns the prefixes in the caller's order, with
// blanks dropped and repeats collapsed, so a hand-edited payload cannot make
// the gate judge one group twice.
//
// An ABSENT declaration and an EMPTY one are different answers, and the
// difference is preserved: no `task_group:` line at all → no groups (nil);
// `task_group:` with nothing after it → one empty prefix (""), which the gate
// parks by name. Dropping the empty entry here would silently turn a declared
// dependency into "no dependency", which is the exact failure mode this arm
// exists to end (Rule 15).
func taskGroupDependenciesOf(payload string) []string {
	var out []string
	for _, line := range strings.Split(payload, taskGroupDependencyLineSep) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, TaskGroupDependencyKey) {
			continue
		}
		rest := strings.TrimPrefix(trimmed, TaskGroupDependencyKey)
		entries := strings.Split(rest, taskGroupDependencyEntrySep)
		if strings.TrimSpace(rest) == "" {
			// The declaration exists and names nothing — one empty group.
			out = append(out, "")
			continue
		}
		for _, raw := range entries {
			prefix := strings.TrimSpace(raw)
			if prefix == "" || taskGroupPrefixSeen(out, prefix) {
				continue
			}
			out = append(out, prefix)
		}
	}
	return out
}

// taskGroupPrefixSeen reports whether prefix is already in the parsed list —
// the dedupe half of taskGroupDependenciesOf, kept beside it so the reader is
// one function.
func taskGroupPrefixSeen(prefixes []string, prefix string) bool {
	for _, p := range prefixes {
		if p == prefix {
			return true
		}
	}
	return false
}

// taskGroupGateDenied judges EVERY group a row declares, ANDed like the parent
// gate and joined by the same separator, so a row waiting on three batches
// reads all three rather than one anonymous "not released".
//
// ledgerStatus is the id → status index (taskLedgerStatuses). A nil index is a
// CALLER ERROR (Rule 15): a row that declares a group must be judged against
// real statuses, so the nil case denies and SAYS the ledger could not be read
// rather than assuming the dependency holds.
func taskGroupGateDenied(rowTaskID string, groupPrefixes []string, ledgerStatus map[string]string) (bool, string) {
	if len(groupPrefixes) == 0 {
		return false, ""
	}
	if ledgerStatus == nil {
		return true, taskGroupReasonUnreadable
	}
	reasons := make([]string, 0, len(groupPrefixes))
	for _, prefix := range groupPrefixes {
		if denied, reason := taskGroupGateDeniedOne(rowTaskID, prefix, ledgerStatus); denied {
			reasons = append(reasons, reason)
		}
	}
	if len(reasons) == 0 {
		return false, ""
	}
	return true, strings.Join(reasons, taskParentsReasonSeparator)
}

// taskGroupGateDeniedOne judges ONE group: every member TERMINAL, the row
// itself excluded, and the refusal naming HOW MANY are still outstanding.
//
// Three states the caller must be able to tell apart (deliverable: a refusal
// names its own cause, Rule 15):
//   - EMPTY group (no member at all, or only the row itself) → denied, and it
//     is denied even when the declared prefix is nonsense, because a
//     dependency that resolves to nothing is never satisfied. This is the arm
//     that refuses to become "no dependency".
//   - INCOMPLETE → denied, naming the count and the first
//     taskGroupMissingNamed member ids.
//   - COMPLETE → released.
func taskGroupGateDeniedOne(rowTaskID, prefix string, ledgerStatus map[string]string) (bool, string) {
	members, selfFound := 0, false
	missing := make([]string, 0, len(ledgerStatus))
	for id, st := range ledgerStatus {
		if prefix == "" || !strings.HasPrefix(id, prefix) {
			continue
		}
		if id == rowTaskID {
			selfFound = true
			continue
		}
		members++
		if !taskParentTerminalStatuses[st] {
			missing = append(missing, id)
		}
	}
	// Map iteration order is random; the refusal must be the same sentence on
	// every render (a reason that reshuffles looks like new evidence).
	sort.Strings(missing)
	if members == 0 {
		if selfFound {
			return true, fmt.Sprintf(taskGroupReasonEmptySelfFormat, prefix)
		}
		return true, fmt.Sprintf(taskGroupReasonEmptyFormat, prefix)
	}
	if len(missing) == 0 {
		return false, ""
	}
	named := missing
	more := ""
	if len(named) > taskGroupMissingNamed {
		more = fmt.Sprintf(taskGroupReasonMoreFormat, len(named)-taskGroupMissingNamed)
		named = named[:taskGroupMissingNamed]
	}
	if selfFound {
		return true, fmt.Sprintf(taskGroupReasonIncompleteSelfFormat,
			prefix, len(missing), members, strings.Join(named, taskGroupMembersJoin), more)
	}
	return true, fmt.Sprintf(taskGroupReasonIncompleteFormat,
		prefix, len(missing), members, strings.Join(named, taskGroupMembersJoin), more)
}

// The rendered reasons of a group-parked row — one spelling each (Rule 1b),
// read by the pending view's marker through taskParentGateReason so the
// verdict and its explanation can never describe different rules.
const (
	taskGroupReasonIncompleteFormat     = " group %s waits for %d of %d row(s) to reach a terminal status (still open: %s%s)"
	taskGroupReasonIncompleteSelfFormat = " group %s waits for %d of %d row(s) to reach a terminal status, self excluded (still open: %s%s)"
	taskGroupReasonMoreFormat           = ", and %d more"
	taskGroupReasonEmptyFormat          = " group %s matches NO row in the ledger — an empty dependency is never satisfied (its rows were never submitted, or the prefix is wrong)"
	taskGroupReasonEmptySelfFormat      = " group %s matches only THIS row (self excluded) — an empty dependency is never satisfied (its rows were never submitted, or the prefix is wrong)"
	taskGroupReasonUnreadable           = " group status unreadable — the ledger could not be read, so the declared group dependencies cannot be judged (a row declaring one is DENIED rather than assumed satisfied)"
)

// taskParentTerminalStatuses is the TERMINAL-VERDICT set — the criterion a
// REWORK row's edge is judged by (taskParentGateDenied's second arm), as
// opposed to taskParentReleaseStatuses, which asks whether the parent's work
// was SATISFIED.
//
// The two sets answer two different questions about the same parent, and they
// are deliberately SEPARATE definitions rather than one set with a flag (Rule
// 14: a value has one meaning): widening this one can never release an
// ordinary child, and widening the release set can never be smuggled in by
// editing a rework rule. Members are spelled from the status constants and are
// EXACTLY the rows terminalTaskStatus calls terminal (the ledger's own
// definition of "this row's life ended"), a coincidence of two definitions
// that a test PINS rather than trusts — so a status added to the ledger's
// terminal notion cannot be silently missing here. `reopened` is excluded BY
// NAME: it is retired (M4.20-B) and
// legacy rows carrying it mean "reissued", i.e. the rework exists already;
// treating it as a verdict would release a second successor onto the same
// failure.
var taskParentTerminalStatuses = map[string]bool{
	taskStatusCompleted:  true,
	taskStatusFailed:     true,
	taskStatusCancelled:  true,
	taskStatusVerified:   true,
	taskStatusSuperseded: true,
}

// taskParentTerminalSetLabel names taskParentTerminalStatuses for a rendered
// reason, with the same derive-don't-retype discipline as
// taskParentReleaseSetLabel (Rule 13).
func taskParentTerminalSetLabel() string {
	names := make([]string, 0, len(taskParentTerminalStatuses))
	for st := range taskParentTerminalStatuses {
		names = append(names, st)
	}
	sort.Strings(names)
	return strings.Join(names, "|")
}

// TaskParentReleaseSetLabel is the EXPORTED face of taskParentReleaseSetLabel
// (Rule 14): the release set is a value SHARED across packages — internal/agent
// teaches it to the model (the task_submit chain note, the reframe chain note,
// the parent_task_id tool description), and a consumer outside this package
// cannot reach the unexported name. One definition, one rendering, so no
// model-facing sentence can describe a different set than the gate enforces.
func TaskParentReleaseSetLabel() string { return taskParentReleaseSetLabel() }

// taskParentReleaseSetLabel names the release set inside a rendered reason
// ("completed|superseded|verified"), DERIVED from taskParentReleaseStatuses so
// the prose an operator reads cannot describe a different set than the gate
// enforces.
func taskParentReleaseSetLabel() string {
	names := make([]string, 0, len(taskParentReleaseStatuses))
	for st := range taskParentReleaseStatuses {
		names = append(names, st)
	}
	sort.Strings(names)
	return strings.Join(names, "|")
}

// The rendered reasons of a parked row — what the pending view shows beside
// [queue-skip], so the operator reads WHY (Rule 1b: one spelling each).
const (
	taskParentReasonUnreleasedFormat = " parent %s not released (status=%s; releases on %s)"
	// taskParentReasonUnreleasedReworkFormat is the SAME arm for a REWORK row,
	// which waits on a different criterion (taskParentTerminalStatuses). It is
	// a separate spelling rather than a flag inside the general one because a
	// parked rework row that named the RELEASE SET would send its reader to a
	// repair that would not work: the parent cannot become `completed` (it
	// already reached its verdict and failed), while it CAN still be judged.
	taskParentReasonUnreleasedReworkFormat = " parent %s not released for this rework row (status=%s; a rework waits for the parent's verdict: releases on %s)"
	taskParentReasonDanglingFormat         = " parent %s missing from the ledger"
	taskParentReasonUnread                 = " parent status unreadable"
)

// taskPendingParentedCount reports how many PENDING rows carry a parent id —
// the cheap probe that decides whether the dependency gate's status index is
// worth loading at all (see the funnel in TaskNextFiltered). It exists because
// the index must be read BEFORE the candidate cursor opens (the ledger pins ONE
// connection), so the "is it needed?" question has to be answerable without
// walking the rows the map exists to judge.
func taskPendingParentedCount(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status = ? AND parent_task_id IS NOT NULL AND parent_task_id != ''`,
		taskStatusPending).Scan(&n)
	return n, err
}

// taskPendingGroupDeclaringCount reports how many PENDING rows DECLARE a
// group-completion dependency in their payload — the second half of the same
// "is the id → status index needed?" question taskPendingParentedCount answers
// for parents (see taskDependencyIndexNeeded).
//
// THE MATCH IS A SUBSTRING PROBE, NOT A PARSE, on purpose: a false positive
// costs one whole-table map read (the ledger is a few MB, the read happens
// once per claim attempt / once per render), while a false NEGATIVE would hand
// the gate a nil index and make it deny every group-declaring row. The cheap
// probe may therefore over-count but must never under-count, which is why the
// SQL asks for the KEY (instr finds `task_group:` anywhere in the payload,
// including a trailing line with no newline) rather than for a well-formed
// declaration.
func taskPendingGroupDeclaringCount(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status = ? AND instr(payload, ?) > 0`,
		taskStatusPending, TaskGroupDependencyKey).Scan(&n)
	return n, err
}

// taskDependencyIndexNeeded answers the ONE question every reader of the
// dependency gate has to ask before loading the id → status index: does the
// pending queue carry a row that will be JUDGED against it?
//
// It exists because the gate has TWO edges and the probe used to know about
// only one. A queue with no parented row but one group-declaring row would
// have skipped the index read, and the gate (handed nil) would then have
// parked that row — the conservative verdict, but WRONG: the row's group may
// be complete, and a wrong [queue-skip] is exactly the "dispatcher starves a
// satisfied row" defect the pending view exists to prevent. ONE definition
// (Rule 14), read by the claim funnel, the pending render, the [next] marker
// resolver and the `task show` parent block.
func taskDependencyIndexNeeded(ctx context.Context, db *sql.DB) (bool, error) {
	n, err := taskPendingParentedCount(ctx, db)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	g, err := taskPendingGroupDeclaringCount(ctx, db)
	if err != nil {
		return false, err
	}
	return g > 0, nil
}

// taskParentStatuses loads the task_id → status index the dependency gate
// reads — the SQL EXISTS form of the check, materialised as a map.
//
// WHY a map and not an EXISTS sub-query in the selection, or a per-row
// handle lookup:
//   - The predicate taskRowClaimable is a PURE string/map function shared by
//     TWO readers (the claim funnel and the pending view's marker). An EXISTS
//     clause welded into TaskNextFiltered's SELECT would implement the gate
//     in ONE of them and leave the other to re-derive it — the exact drift
//     Rule 14 forbids; the view would then mark rows [next] that the funnel
//     refuses.
//   - A per-row lookup (SELECT status WHERE task_id = ?) is one statement per
//     parented row and cannot distinguish "no such row" from "not fetched"
//     without a second error convention.
//   - The map is exactly the EXISTS semantics: membership IS row existence
//     (the dangling arm), and the value IS the release test. It is read only
//     when a row in the claim order actually carries a parent id (lazy, see
//     the funnel), and it is ONE query for the whole walk — no per-row
//     statement on the claim path.
//
// It deliberately covers the WHOLE tasks table rather than a candidate-id
// filter: the ledger is small (a year of rows is a few MB of summaries), the
// funnel is one-shot per claim, and a filtered IN(...) would re-derive the
// candidate set in a second place.
func taskParentStatuses(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT task_id, status FROM tasks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, st string
		if err := rows.Scan(&id, &st); err != nil {
			return nil, err
		}
		out[id] = st
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// taskRowClaimable reports whether a PENDING row can be claimed by a
// dispatcher — the eligibility ladder TaskNextFiltered walks its claim order
// with, and (Rule 14: ONE definition) the same predicate the pending view's
// [queue-skip] marker uses. Four reasons a row is skipped, none of which
// makes the queue empty:
//
//   - skip-tag anti-misclaim: a row carrying ANY tag in skipSet belongs to
//     another dispatcher or a human.
//   - payload-less rows are ledger entries, not dispatchable work (serving
//     one hard-refuses in runTaskRun, bug028 / e3ca35ca).
//   - merger-resolve rows are NEVER worker-dispatchable (row-per-resolve
//     single-owner, 2026-09-05): the merger alone resolves conflicts in its
//     own process/worktree and owns the tracking row from register →
//     terminal. Defense in depth on top of the in_progress birth state.
//   - DEPENDENCY-PARKED rows: a non-empty parent_task_id whose parent is not
//     released (see taskParentGateDenied for the release set and the dangling
//     arm). parentStatus is the id → status index; pass nil on a path that
//     cannot read the ledger, which denies any parented row rather than
//     assuming its dependency is satisfied.
//   - FIXTURE rows: a row carrying the reserved fixture tag (tasktags.FixtureTag)
//     is QUEUE DECORATION, never a unit of work — see taskFixtureRowReason for
//     the incident this closes and why the arm lives HERE rather than at the
//     dispatch sites.
//
// taskParentGateDenied is the ONE place the parent rule is decided, so the
// funnel and the marker cannot answer it differently; taskParentGateReason
// below is its read-only face for callers that only render.
func taskRowClaimable(rowID, rowTags, payload string, parentIDs []string, parentStatus map[string]string, skipSet map[string]bool) bool {
	if matchesSkipTags(rowTags, skipSet) {
		return false
	}
	if strings.TrimSpace(payload) == "" {
		return false
	}
	if resolveRowIsMergerOwned(rowTags) {
		return false
	}
	if taskHasTagToken(rowTags, tasktags.FixtureTag) {
		return false
	}
	denied, _ := taskParentGateDenied(rowID, rowTags, payload, parentIDs, parentStatus)
	return !denied
}

// The rendered reason a FIXTURE row is unclaimable — ONE spelling (Rule 1b),
// read by the pending view's marker and by any refusal that has to name it.
//
// WHY IT SAYS "fixture" AND NOTHING ABOUT PARENTS: the pending view's marker
// appends the reason after TaskQueueSkipMarker, and a reader who saw the parent
// sentence on a row with no parent would go looking for a dependency that does
// not exist (Rule 15: a refusal names its OWN cause). The fixture arm is
// independent of the parent arm — a fixture may carry either edge, and this
// sentence is the one that always applies to it.
const taskFixtureRowReason = " fixture row — the reserved " + tasktags.FixtureTag + " tag marks queue decoration, never work: no dispatcher will claim it (drop the tag to make the row real work)"

// taskParentGateReason returns the renderable reason a row is dependency-
// parked ("" when it is not) — the read-only face of the gate for a caller
// that wants to NAME the reason rather than only the verdict. It takes the
// row's tags because the gate judges a rework row by a different criterion
// and must say so (taskRowClaimable docs: ONE predicate, so the reason can
// never describe a rule the verdict did not apply).
func taskParentGateReason(rowTaskID, rowTags, payload string, parentIDs []string, parentStatus map[string]string) string {
	if denied, reason := taskParentGateDenied(rowTaskID, rowTags, payload, parentIDs, parentStatus); denied {
		return reason
	}
	return ""
}

// TaskQueueSkipMarker / TaskQueueNextMarker are the pending view's queue
// markers: which row the claim funnel would take next, and which rows no
// dispatcher can take at all. Rule 1b: the tokens live here, not inline.
const (
	TaskQueueNextMarker = " [next]"
	TaskQueueSkipMarker = " [queue-skip]"
)

// taskQueuePositionSuffix renders a pending row's queue marker — the FIGURATIVE
// queue position of the row, which is a different thing from its line number in
// the render:
// TaskQueueNextMarker on the row the CLAIM FUNNEL takes next — named by
// nextClaimID, resolved with the funnel's own order (taskNextClaimID) and NOT
// by a position in this render — and TaskQueueSkipMarker on a row that is in
// no dispatcher's reach, followed, for the DEPENDENCY arm only, by the gate's
// own reason (taskParentGateReason) so a parked row names WHY it is parked
// instead of looking starved.
//
// WHY THE MARKER IS NOT "THE FIRST CLAIMABLE ROW IN THIS RENDER": the pending
// view renders newest-first (taskListOrderNewestFirst), so the row that will be
// claimed can sit SEVERAL rows below the top. Stamping the first claimable row
// the render walks past would label the NEWEST dispatchable row "[next]" while
// the funnel takes a different one — a marker contradicting the queue, which
// is exactly what Rule 33 forbids.
//
// The skip set is nil here on purpose: the list shows the WHOLE pending
// group, and a tag this dispatcher skips is normally another dispatcher's
// claimable work, so the list must not label it unclaimable. Only the
// skip-set-independent reasons mark a row [queue-skip].
//
// parentStatus is the dependency gate's id → status index (nil when the
// caller could not read the ledger — the predicate then denies parented rows
// and the reason says the status was unreadable).
func taskQueuePositionSuffix(t *Task, ledgerStatus map[string]string, nextClaimID string) string {
	if t.TaskID == nextClaimID {
		return TaskQueueNextMarker
	}
	if !taskRowClaimable(t.TaskID, t.Tags, t.Payload, taskParentIDsOf(t.ParentTaskID, t.ParentTaskIDs), ledgerStatus, nil) {
		return TaskQueueSkipMarker + taskQueueSkipReason(t, ledgerStatus)
	}
	return ""
}

// taskQueueSkipReason renders WHY a pending row is in no dispatcher's reach —
// the read-only face of the same arms taskRowClaimable refuses with, asked
// again only to NAME the cause (Rule 15: a refusal says why; Rule 14: the
// naming never re-decides the verdict).
//
// ORDER IS THE CAUSE'S ORDER, and the fixture arm comes first because it is
// the one that always applies to a fixture row: a fixture that ALSO carries an
// un-released parent would otherwise be marked with the parent's sentence, and
// a reader who fixed the parent would expect the row to become claimable — it
// would not (the fixture tag alone refuses it). The parent sentence is the
// fallback because it is the one arm with a REPAIR in the operator's hands.
func taskQueueSkipReason(t *Task, ledgerStatus map[string]string) string {
	if taskHasTagToken(t.Tags, tasktags.FixtureTag) {
		return taskFixtureRowReason
	}
	reason := taskParentGateReason(t.TaskID, t.Tags, t.Payload,
		taskParentIDsOf(t.ParentTaskID, t.ParentTaskIDs), ledgerStatus)
	if reason == "" && len(taskGroupDependenciesOf(t.Payload)) > 0 {
		// Defence in depth, not a second rule (Rule 14): taskParentGateReason
		// IS the gate's face, so it can only be empty here if the row was
		// marked unclaimable by an arm that cannot name itself. Say THAT rather
		// than render a bare [queue-skip] with no cause (Rule 15).
		return taskQueueSkipUnnamed
	}
	return reason
}

// taskQueueSkipUnnamed is the last-resort sentence for a row the predicate
// refused and no arm could explain — an unnamed refusal is still a refusal,
// but it must never render as a silent marker.
const taskQueueSkipUnnamed = " unclaimable, and no arm of the eligibility ladder named a cause — report this: the pending view and the claim funnel disagree"

// taskNextClaimID resolves the task_id the claim funnel takes next — the row
// every dispatcher's first attempt would return — or "" when the pending queue
// holds no dispatchable row. It is the pending view's own read of the queue
// (the [next] marker), and it exists so the marker can be order-INDEPENDENT:
// the listing renders newest-first while the funnel claims
// priority-first, so the row to mark cannot be found by walking the render.
//
// The row is chosen by the SAME predicate the funnel applies with no skip set
// (taskRowClaimable) over the SAME order (taskClaimOrderBy) — the list shows
// the whole ledger group, so a tag one dispatcher skips is still claimable
// work for another. Reading the funnel's row here (rather than calling
// TaskNextFiltered, which CLAIMS by capping created_at) is what keeps
// `task list` a pure read: the ledger is untouched (Rule 15's read-path
// contract: no sweep, no write, no notify).
func taskNextClaimID(ctx context.Context, db *sql.DB) (string, error) {
	// The queue order has ONE definition (taskClaimOrderBy), reached and never
	// re-typed. This function IS TaskList's pending-view reader of that order:
	// it decides which row wears the [next] marker, so the marker names the row
	// the claim funnel takes — which only holds if both answer from the same
	// clause (Rules 13/14).
	query := `SELECT task_id, tags, payload, parent_task_id, ` + taskParentIDsColumnName + ` FROM tasks WHERE status = ?` + taskClaimOrderBy
	rows, err := db.QueryContext(ctx, query, taskStatusPending)
	if err != nil {
		return "", fmt.Errorf("task next claim: %w", err)
	}
	defer rows.Close()
	// The dependency gate's index belongs to the SAME connection-pinned ledger
	// and must be read AFTER this cursor closes (see the deadlock note in
	// TaskList); so collect the candidates first, then resolve.
	type claimCandidate struct {
		id, tags, payload, parentID, parentIDs string
	}
	var candidates []claimCandidate
	for rows.Next() {
		var c claimCandidate
		if err := rows.Scan(&c.id, &c.tags, &c.payload, &c.parentID, &c.parentIDs); err != nil {
			return "", fmt.Errorf("task next claim: scan: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("task next claim: %w", err)
	}
	rows.Close() //stylegate:ignore read-only cursor; closed before the parent-status read (single-connection ledger)
	indexNeeded, err := taskDependencyIndexNeeded(ctx, db)
	if err != nil {
		return "", fmt.Errorf("task next claim: parent index: %w", err)
	}
	var parentStatus map[string]string
	if indexNeeded {
		ps, err := taskParentStatuses(ctx, db)
		if err != nil {
			return "", fmt.Errorf("task next claim: parent status: %w", err)
		}
		parentStatus = ps
	}
	for _, c := range candidates {
		if taskRowClaimable(c.id, c.tags, c.payload, taskParentIDsOf(c.parentID, c.parentIDs), parentStatus, nil) {
			return c.id, nil
		}
	}
	return "", nil
}

// taskTagResolved marks an archived (治理归档) row: appended to tags by
// TaskArchive, stripped of nothing — tombstones stay fully searchable.
const taskTagResolved = "resolved"

// TaskTagResolved is the EXPORTED spelling of taskTagResolved, for the
// operator-facing text that must name the marker without re-typing it
// (Rule 1/13/14: the value has ONE definition and every writer references it —
// the help text in cmd/agent/commands.go is such a writer).
const TaskTagResolved = taskTagResolved

// taskTagResumedPrefix prefixes the resume-linkage tag: `resumed:<session-id>`
// (M4.7 resume observability — a `gopi session resume --task T` continuation runs in
// a FRESH session, and without the tag that session is an orphan row-less
// run the ledger cannot see).
const taskTagResumedPrefix = "resumed:"

// taskTagWrapperPrefix legacy wrapper-linkage tag (bug057) — KEPT only for
// historical rows: pre-task-single-row-exec auto mirror rows carry
// `wrapper:<human-task-id>` and the tag must survive (append-only audit;
// TaskReconcileMirrorCloseout still resolves in_progress legacy mirrors by
// it). New executions never mint it.
const taskTagWrapperPrefix = "wrapper:"

// TaskWrapperEnv names the env var `task run` sets on the CHILD gopi
// process BEFORE spawn: the human wrapper row's task_id. The child reads it
// in its auto-registration start hook (maybeRegisterSessionTask) as the
// WRAPPER-CHILD GATE — a task-run child already has a human ledger row (the
// run's single source of execution truth), so it must NOT mint an auto
// mirror row (task-single-row-exec). The env is also the SIGPIPE-ignore
// latch (cmd/agent/main.go) and the executor's deliverable-worktree backfill
// key (task_deliverable_worktree.go). Deliberately a PRE-spawn env
// (cmd.Env). Rule 2: one writer (runTaskRunOnce), readers here + main.go +
// task_deliverable_worktree.go.
const TaskWrapperEnv = "GOPI_TASK_WRAPPER_ID"

// AutoRegisterOffEnv is the env-var NAME that disables agent-session
// auto-registration. It exists so process SPAWNERS (the round's child env in
// cmd/agent, the merger's resolve-conflict child) can turn auto-registration
// off for a child WITHOUT importing internal/agent (an import cycle: cmd
// → tools → ... never agent). Semantics live in internal/agent; this is only
// the spelling — and since 2026-10-29 the SPELLING itself has ONE home,
// config.TaskAutoRegisterEnv (Rule 14): internal/testutil also has to drop
// this variable from a suite's process environment, and it can import neither
// this package nor internal/agent. Both production sides are aliases of the
// config constant now, so the two literals that used to be held in step by a
// comment can no longer drift (Rule 13).
//
// The merger resolver child sets it so it mints ZERO ledger rows (merger never
// submits — the resolve is git + a session file, not a queue row), while that
// child's session JSONL is still written (the audit trail).
const AutoRegisterOffEnv = config.TaskAutoRegisterEnv

// AutoRegisterOffValue is the VALUE AutoRegisterOffEnv must carry to turn
// auto-registration OFF — the env NAME above and this env VALUE are one pair,
// assembled by spawners as AutoRegisterOffEnv+"="+AutoRegisterOffValue.
// Naming the value is what stops the 2026-09-10 defect: the merger's
// resolve-conflict child was spawned with AutoRegisterOffEnv+"=1" (the ON
// value behind an "Off" name) so every resolve child auto-registered its own
// ledger row (observed rows 01a089a1 / 01a089a7-26cb) — one merger lineage
// looking like several merger processes. internal/agent owns the semantics
// (autoTaskRegisterEnabled); agent.AutoTaskRegisterOff binds this value to
// that semantics so the two packages cannot drift.
const AutoRegisterOffValue = "0"

// MergeResolveRoleEnv names the env var the merger's resolve-conflict child
// carries: the branch it is resolving. It distinguishes a resolver child
// session in the audit trail (the session JSONL / logs show which branch a
// resolve child handled) and gives the merger a deterministic per-branch
// handle. Set by cmd/agent before spawn (one writer); informational.
const MergeResolveRoleEnv = "GOPI_MERGE_RESOLVE_BRANCH"

// TaskArchive archives a TERMINAL task (completed/failed/cancelled) as
// resolved: "resolved" is appended to the row's tags (the tags-resolved
// scheme — zero schema change, FTS-searchable, visible in show). Idempotent:
// an already-archived row is a no-op reporting so. A non-terminal row is
// REFUSED (a pending/claimed/in_progress task cannot be filed as resolved —
// archive is a graveyard, not a shortcut out of the queue). The row keeps
// its status, timestamps and everything else; show/search reach it normally.
// It is hidden from EVERY LIST view — whatever --status says, and whether or
// not a status was given at all (TaskListVisible) — unless includeArchived
// brings it back; and the view that hid it SAYS SO (TaskListArchiveHiddenNote),
// so the drop is never silent. (This comment claimed "only the default
// --status failed view hides it" until 2026-09-29: the filter has always run
// for every status, and a comment that scopes it to one view is the same
// defect the help text carried — measured, docs/archive/demo/materials/01-list/
// 05-finding.md F1.1.)
func TaskArchive(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()

	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", fmt.Errorf("task: archive: %w", err)
	}
	if t == nil {
		return "", fmt.Errorf("task: no task with task_id=%q", taskID)
	}
	if !terminalTaskStatus(t.Status) {
		return "", fmt.Errorf("task: archive refused: %s is %s (not terminal) — only completed/failed/cancelled rows can be archived as resolved", taskID, t.Status)
	}
	added, err := taskAddTag(ctx, db, taskID, taskTagResolved)
	if err != nil {
		return "", fmt.Errorf("task: archive: %w", err)
	}
	if !added {
		return fmt.Sprintf("Task already archived: %s (tags=%s)", taskID, orDash(normalizeTaskTags(t.Tags))), nil
	}
	// t is a pre-mutation snapshot — re-read so the confirmation shows the
	// tags as they now stand on the row.
	if t2, err := taskByID(ctx, db, taskID); err == nil && t2 != nil {
		t = t2
	}
	return fmt.Sprintf("Task archived: %s (status=%s, tags=%s)", taskID, t.Status, normalizeTaskTags(t.Tags)), nil
}

// taskAddTag appends one tag to a row's comma-separated tags (idempotent —
// an existing tag is left untouched) and bumps updated_at. Used by the
// archive tombstone and the resume linkage (TaskTagResumed).
func taskAddTag(ctx context.Context, db *sql.DB, taskID, tag string) (bool, error) {
	taskID = taskIDNormalize(taskID)
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return false, err
	}
	if t == nil {
		return false, fmt.Errorf("task: no task with task_id=%q", taskID)
	}
	tags := normalizeTaskTags(t.Tags)
	for _, tg := range strings.Split(tags, ",") {
		if tg == tag {
			return false, nil
		}
	}
	newTags := tags
	if newTags == "" {
		newTags = tag
	} else {
		newTags += "," + tag
	}
	now := float64(time.Now().Unix())
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET tags = ?, updated_at = ? WHERE task_id = ?`,
		newTags, now, taskID); err != nil {
		return false, fmt.Errorf("task: tag: %w", err)
	}
	return true, nil
}

// TaskTagResumed links a resume continuation session to its source task row:
// the `resumed:<session-id>` tag records that the task was resumed in a fresh
// session and lets `gopi task show` / `gopi task list` answer "how many times
// has this task been resumed" (the M4.7 dispatch-retry dependency — a failed
// task's resume spawns a new session the ledger would otherwise never see).
// Idempotent: re-tagging the same session is a no-op.
//
// THE SECOND POSITIONAL IS VALIDATED AGAINST THIS PROFILE'S SESSION ARTIFACTS
// (F16.1): the row's tags are an AUDIT record, and a `resumed:` tag naming no
// session on disk is a stamp that reads as history while recording nothing.
// So an id that no session artifact names is REFUSED BY NAME and NOTHING is
// written (Rule 15) — the all-zero uuid, a typo and a pruned id are the same
// answer, and the zero-value path is the only refusal. A SHORT id (the chunk a
// listing printed) is resolved to the ONE canonical full id whose artifact is
// on disk; a chunk that names several, or an operand that names none, is
// refused with the ids it could have meant. See task_resumed_session.go for
// the shapes checked and their cost.
//
// `gopi session resume --task T` (the in-repo caller) passes the id of the session it
// has just opened, whose JSONL exists before this call — the check admits it
// and, being idempotent, the re-run of a resume stamps the same tag once.
func TaskTagResumed(taskID, sessionID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	sessionID = strings.TrimSpace(sessionID)
	if taskID == "" || sessionID == "" {
		return "", fmt.Errorf("task: tag-resumed requires both a task_id and a session_id")
	}
	resolved, err := taskResumedSessionArtifact(taskID, sessionID)
	if err != nil {
		return "", err
	}
	if resolved == "" {
		return "", fmt.Errorf("task: tag-resumed: %s %q names no session of this profile "+
			"(no session log, raw log or evidence directory on disk) — nothing tagged, and a row's "+
			"resume lineage must name a real session", taskResumedLabel, sessionID)
	}
	sessionID = resolved
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()

	tag := taskTagResumedPrefix + sessionID
	added, err := taskAddTag(ctx, db, taskID, tag)
	if err != nil {
		return "", err
	}
	if added {
		return fmt.Sprintf("Task %s tagged %s", taskID, tag), nil
	}
	return fmt.Sprintf("Task %s already carries %s", taskID, tag), nil
}

// TaskRunProvenance returns a row's stamped run provenance (run_method +
// run_cmd — devdoc/run-method-provenance.md), or "" for both when the row
// has none / does not exist. Used by callers answering "how was this run".
func TaskRunProvenance(taskID string) (method, cmdline string) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", ""
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", ""
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil || t == nil {
		return "", ""
	}
	return t.RunMethod, t.RunCmd
}

// TaskLauncherCommand renders the row's own run_cmd from the CALLER's
// launcher identity plus the verb/flag tail of the command it is about to
// record: runcmd.Launcher() supplies argv[0] (the gopi binary the launcher
// published, else the conventional name) and runcmd.Command does the
// basename + shell-quoting. Callers that ARE the run (the harness
// auto-register hook, the merger's resolve row) have no argv of their own
// to read, so this is their ONE source of the string (Rule 2 — the same
// renderer cmd/agent's own launch site uses).
func TaskLauncherCommand(args ...string) string {
	return runcmd.Command(append([]string{runcmd.Launcher()}, args...))
}

// TaskReconcileMirrorCloseout is the serve-waitpid close-out half (epic
// t-1788384486): the wrapper (task run / serve tick / worker round) closes
// the child's AUTO mirror row ITSELF when the child died without running
// its END hook. The mirror row (task_id = child session id, tagged
// wrapper:<wrapperTaskID>) is normally closed by the child's own
// finishSessionTaskRecorded — but a child killed abnormally (SIGKILL,
// crash, GLM-1214, serve restart kill) never runs that hook, leaving the
// mirror row in_progress forever (the recurring zombie auto-row class). The
// PARENT owns the close-out: it knows the child exited (cmd.Wait returned),
// so it reconciles the mirror row to the same terminal state, by its own
// action — never trusting the child's self-report.
//
// Guards (the double-write / false-close protections):
//   - only rows STILL in_progress are touched (a mirror the child already
//     closed terminal is left exactly as-is — acceptance (d), no
//     double-write);
//   - only rows carrying the wrapper:<wrapperTaskID> tag are
//     candidates (never a foreign row, never an untagged auto row);
//   - no mirror row / already-terminal row → no-op, nil.
//
// The terminal write goes through TaskUpdateWithResult so the digest lands
// in the append-only task_events trail (same as the child's own close).
// Best-effort by contract: a reconcile failure is logged and returned, never
// fatal to the wrapper (the wrapper row's own writeback is the authoritative
// terminal fact).
func TaskReconcileMirrorCloseout(wrapperTaskID, status, reason string) error {
	wrapperTaskID = taskIDNormalize(wrapperTaskID)
	status = strings.TrimSpace(status)
	reason = strings.TrimSpace(reason)
	if wrapperTaskID == "" {
		return fmt.Errorf("task reconcile: wrapper task_id must not be empty")
	}
	if status != taskStatusCompleted && status != taskStatusFailed {
		return fmt.Errorf("task reconcile: status must be completed|failed, got %q", status)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()

	// Find the mirror: the row tagged wrapper:<wrapperTaskID> that is
	// STILL in_progress (the only shape needing reconciliation — legacy
	// task-single-row-exec rows mint no mirror, so this matches only
	// pre-fix rows). The comma-boundary guard (a wrapper:t-1 tag never
	// matches wrapper:t-12) keeps the LIKE matching exact.
	// THE TAG IS THE MIRROR'S IDENTITY; no source predicate belongs here. The
	// lookup used to add `and source = 'auto'` — a SECOND key for the same
	// fact, spelled with the retired conflation. be17a9bd (derive provenance
	// instead of defaulting it) retired that spelling on the write side
	// (internal/tasksource.Normalize maps "auto" onto its current role, and
	// every writer now names its own), so the predicate silently stopped
	// matching the very rows it was meant to select: measured 2026-09-25,
	// TestReconcileMirrorCloseout_Exit0ClosesCompleted — a wrapper-tagged
	// in_progress mirror row stayed in_progress while the wording read as if
	// the arm had run. A historical row is selected exactly as it is
	// identified, and `wrapper:<id>` was written by no other shape.
	needle := taskTagWrapperPrefix + wrapperTaskID
	rows, err := db.QueryContext(ctx,
		`SELECT task_id FROM tasks WHERE status = ? AND (tags = ? OR tags LIKE ? OR tags LIKE ?) ORDER BY id DESC LIMIT 1`,
		taskStatusInProgress, needle, needle+",%", "%,"+needle)
	if err != nil {
		return fmt.Errorf("task reconcile: scan: %w", err)
	}
	found := false
	var mirrorID string
	if rows.Next() {
		found = true
		if err := rows.Scan(&mirrorID); err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			return fmt.Errorf("task reconcile: scan: %w", err)
		}
	}
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
	if !found {
		return nil // no in_progress mirror — nothing to close (child already closed it, or never registered)
	}

	out, err := TaskUpdateWithResult(mirrorID, status, reason)
	if err != nil {
		return fmt.Errorf("task reconcile: close mirror %s: %w", mirrorID, err)
	}
	taskNotifyLog("task reconcile: closed mirror %s → %s (wrapper %s, parent-side close-out): %s", mirrorID, status, wrapperTaskID, out)
	return nil
}

// TaskResumeCount counts the `resumed:` tags on a task row — the "how many
// times has this task been resumed" figure for show/list rendering.
// task_id is whitespace-normalized (see taskIDNormalize): a padded id
// resolves the SAME row its sibling accessors resolve. An unknown or blank
// id still yields 0 (a blank id names no row — it is not an error here).
func TaskResumeCount(taskID string) int {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return 0
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return 0
	}
	defer db.Close()

	t, err := taskByID(ctx, db, taskID)
	if err != nil || t == nil {
		return 0
	}
	return taskResumedTagCount(t.Tags)
}

// taskResumedTagCount counts the `resumed:` tags in a comma-separated tags
// string (the in-hand variant used by the list renderer — no DB round-trip).
func taskResumedTagCount(tags string) int {
	n := 0
	for _, tg := range strings.Split(normalizeTaskTags(tags), ",") {
		if strings.HasPrefix(tg, taskTagResumedPrefix) {
			n++
		}
	}
	return n
}

// isTaskResolved reports whether a row's tags carry the resolved marker
// (the archived-tombstone test used by the list filter).
func isTaskResolved(tags string) bool {
	for _, tg := range strings.Split(tags, ",") {
		if strings.TrimSpace(tg) == taskTagResolved {
			return true
		}
	}
	return false
}

// FilterArchivedRows drops resolved (archived) rows from a rendered TaskList
// block — the list-view hygiene (NOT a failed-view rule: it applies to every
// --status, and to the unfiltered view too — TaskListVisible is the one
// funnel): an already-handled row must not re-clutter a working view, while
// --all and show/search keep the full ledger. TaskList annotates archived rows with the trailing " [resolved]"
// marker (tags are otherwise not rendered); this filter drops those lines.
//
// The count header is rewritten to the POST-filter row count: TaskList stamps
// the pre-filter count (its SELECT's row count), so a partial filter would
// otherwise claim 68 above 7 rows after an archiving pass. A view that loses
// every row degrades to the standard empty text (no dangling "N task(s):"
// prefix above nothing).
//
// The ORDER NOTE (TaskListOrderNote) rides that header and survives the
// rewrite: rewriteTaskListCount replaces only the header's leading digits and
// returns the rest byte-identical, so the printed order still describes the
// rows below it after a filter drops some.
//
// THE DISCLOSURE (TaskListArchiveHiddenNote). Every path that drops a row
// appends one line saying how many it dropped and which command shows them —
// including the path that drops EVERY row, where the line rides under the
// empty text instead of standing in for it. A read command that under-reports
// silently is a defect of the same standing as a wrong answer (Rule 15); the
// count is what makes the difference between "there are none" and "they are
// hidden from you" visible to the caller.
func FilterArchivedRows(rendered string) string {
	lines := strings.Split(rendered, "\n")
	var kept []string
	dropped := 0
	for _, ln := range lines {
		if strings.HasPrefix(ln, TaskListRowPrefix) && isRenderedRowArchived(ln) {
			dropped++
			continue
		}
		kept = append(kept, ln)
	}
	if dropped == 0 {
		return rendered
	}
	out := strings.Join(kept, "\n")
	// Recount the rows that will actually render (dropped rows are gone from
	// `kept`) and re-stamp the header's leading integer. A view that lost every
	// row degrades to the standard empty text rather than a dangling
	// "N task(s):" prefix above nothing — and carries the disclosure below it,
	// so an all-hidden view is never byte-identical to an empty ledger.
	rows := countRenderedTaskListRows(kept)
	if rows == 0 {
		return taskListEmptyResult + "\n" + TaskListArchiveHiddenNote(dropped)
	}
	return rewriteTaskListCount(out, rows) + TaskListArchiveHiddenNote(dropped)
}

// TaskListVisible renders the ledger the way EVERY operator-facing view must:
// the archived (resolved) rows are dropped unless includeArchived is set. It
// is the ONE funnel for the "may this view see a tombstone?" decision — the
// defect it closes is not a wrong filter but a MISSING one: the rule lived in
// each caller, so the CLI's `task list` dropped archived rows (FilterArchivedRows)
// while the conversational `/task list` called TaskList directly and rendered
// them with their " [resolved]" marker. The same archived column, two
// operator surfaces, opposite verdicts (measured 2026-09-29). A caller that
// renders a ledger view must come through here; `TaskList` stays the RAW
// render (its row-level [resolved] marker is the datum this filter reads, and
// `task show`/`task search` deliberately keep the full ledger).
//
// includeArchived=true is the `--all` view: the tombstones come back, count
// header included.
func TaskListVisible(statusFilter string, includeArchived bool) (string, error) {
	rendered, err := TaskList(statusFilter)
	if err != nil {
		return "", err
	}
	if includeArchived {
		return rendered, nil
	}
	return FilterArchivedRows(rendered), nil
}

// countRenderedTaskListRows counts the rendered rows ("#"-prefixed lines) in
// a TaskList body — the number the count header must equal.
func countRenderedTaskListRows(lines []string) int {
	n := 0
	for _, ln := range lines {
		if strings.HasPrefix(ln, TaskListRowPrefix) {
			n++
		}
	}
	return n
}

// rewriteTaskListCount replaces the leading integer of a rendered TaskList
// header with n, leaving the rest of the header (and the whole body) byte-
// identical. A block that does not open with the "<n> task(s)…" header is
// returned unchanged (defensive: never mangle an unexpected render).
func rewriteTaskListCount(rendered string, n int) string {
	nl := strings.Index(rendered, "\n")
	if nl < 0 {
		return rendered
	}
	header, rest := rendered[:nl], rendered[nl:]
	_, tail, ok := taskListCountPrefix(header)
	if !ok {
		return rendered
	}
	return strconv.Itoa(n) + tail + rest
}

// taskListCountMarker is the header fragment between the rendered count and
// the header's tail — the anchor both the header parser and the count
// rewriter work against (one spelling for "the header is a count header").
const taskListCountMarker = " task(s)"

// taskListCountPrefix parses a rendered TaskList header's leading count and
// returns the header tail that follows the digits (" task(s)…").
func taskListCountPrefix(header string) (int, string, bool) {
	i := 0
	for i < len(header) && header[i] >= '0' && header[i] <= '9' {
		i++
	}
	if i == 0 || !strings.HasPrefix(header[i:], taskListCountMarker) {
		return 0, "", false
	}
	n, err := strconv.Atoi(header[:i])
	if err != nil {
		return 0, "", false
	}
	return n, header[i:], true
}

// taskResolvedRowMarker is the trailing annotation TaskList puts on archived
// rows so FilterArchivedRows can spot them (keep in sync with TaskList's
// Fprintf).
const taskResolvedRowMarker = " [resolved]"

// isRenderedRowArchived checks a rendered TaskList row for the resolved
// marker appended by TaskList.
func isRenderedRowArchived(line string) bool {
	return strings.Contains(line, taskResolvedRowMarker)
}

// TaskTags returns a task's comma-separated tag string (the ledger's
// canonical form — see normalizeTaskTags), so a caller can tell which
// reframe verdict auto-enqueued a row (ReframeRoadmapTag). An absent row
// returns an error.
func TaskTags(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", fmt.Errorf("task: tags lookup: %w", err)
	}
	if t == nil {
		return "", fmt.Errorf("task: no task with task_id=%q", taskID)
	}
	return t.Tags, nil
}

// TaskBriefing returns a task's summary and payload in ONE row read (the
// execution-briefing source for `task run`): taskRunBriefing composes the
// child briefing from BOTH fields, so a single round-trip must yield the
// pair — two separate accessors would race the ledger and double the DB
// opens on every dispatch. A missing task errors; an existing task returns
// the pair verbatim (either may be "" — composition/refusal is the
// caller's policy, see taskRunBriefing).
func TaskBriefing(taskID string) (summary, payload string, err error) {
	taskID = taskIDNormalize(taskID)
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", "", err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", "", fmt.Errorf("task: briefing: %w", err)
	}
	if t == nil {
		return "", "", fmt.Errorf("task: no task with task_id=%q", taskID)
	}
	return t.Summary, t.Payload, nil
}

// TaskClaimExplicit is the Phase-1 CAS claim for an EXPLICIT task_id
// (`task run <id>`): a pending row is claimed through the same
// conditional-write ladder as TaskNextFiltered (exactly one winner); a
// claimed row is the crash-recovery case (the previous holder died between
// claim and start — the claim trail is re-stamped). Anything else refuses:
// the lock protocol never starts from in_progress/terminal rows.
func TaskClaimExplicit(taskID, claimedBy string) error {
	taskID = taskIDNormalize(taskID)
	return TaskClaimExplicitForce(taskID, claimedBy, false)
}

// TaskClaimExplicitForce is TaskClaimExplicit with the operator's --takeover
// override (epic task-run-singleton-guard): force=true SKIPS the pre-claim
// singleton guard, so a run may deliberately take over a row whose holder is
// live. It exists as a separate entry point (Rule 3: no new tool, one
// implementation) so every automatic caller keeps the guard and only the
// operator's explicit flag can bypass it — and so the bypass is grep-able.
func TaskClaimExplicitForce(taskID, claimedBy string, force bool) error {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return fmt.Errorf("task: claim: task_id must not be empty")
	}
	if claimedBy == "" {
		claimedBy = taskDefaultClaimHolder()
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	now := float64(time.Now().Unix())
	// Shape ①: pending → claimed (the Phase-1 CAS). The CLAIM-time identity
	// rides the SAME statement (epic task-run-singleton-guard): a claimed row
	// must carry the three facts that make its holder checkable, or a second
	// runner cannot tell a dead claimer from a live one and the claimed window
	// stays a double-run hole (taskrunguard.go).
	identSet, identArgs := taskRunClaimIdentitySet(claimedBy)
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, updated_at = ?, `+identSet+`
		 WHERE task_id = ? AND status = ?`,
		append([]any{taskStatusClaimed, claimedBy, now, now}, append(identArgs, taskID, taskStatusPending)...)...)
	if err != nil {
		return fmt.Errorf("task: claim: %w", err)
	}
	//stylegate:ignore n==0 is the meaningful branch; a driver error yields the same 0 and same path
	if n, _ := res.RowsAffected(); n == 1 {
		// Transition hook (task-notify-terminal-gap): the Phase-1 CAS won,
		// so pending → claimed is on the ledger. The row is re-read for the
		// payload (this function returns no row image; the claim fields are
		// stamped by the UPDATE above).
		taskNotifyFlushStatusMove(taskID, taskStatusPending, taskStatusClaimed)
		return nil // won the local lock
	}
	// Shape ②: claimed (crash-recovery — re-stamp the claim trail). The
	// conditional UPDATE is itself a CAS: RowsAffected==0 means the row is
	// NOT in a claimable state (in_progress = a live/other runner holds it,
	// terminal = finished, missing = ghost) — refusing here is what stops
	// `task run <id>` from double-executing an already-running task. Only
	// 'claimed' is recoverable: claimed is transient and its sole durable
	// justification is a holder that died between the Phase-1 CAS and the
	// Phase-3 advance (resume legitimately re-stamps the trail).
	//
	// SINGLETON GUARD (epic task-run-singleton-guard, 2026-09-15): the CAS on
	// status = claimed alone does NOT arbitrate this branch — two runners
	// racing a claimed row both see 'claimed' and both re-stamp, because a
	// bare `hostname:pid` stamp cannot tell "my own claim" from "another
	// live runner's". The row is therefore read FIRST and judged by the
	// three-factor identity (taskrunguard.go): a live OTHER holder refuses
	// the claim outright, a dead/recycled/own holder re-stamps as before.
	prev, err := taskRunGuardResolvable(db, taskID)
	if err != nil {
		return err
	}
	if !force {
		verdict, refusal, rErr := taskRunGuardRuling(prev)
		if rErr != nil {
			return fmt.Errorf("task: claim recover: %w", rErr)
		}
		if !taskRunGuardAdmits(verdict) {
			return fmt.Errorf("task: claim: %s", refusal)
		}
	}
	// The re-stamp rewrites claimed_by, so the identity columns must move WITH
	// it — a stamp and an identity that describe different processes is the
	// shape taskproc's verdict ladder refuses to judge (it answers unknown),
	// and the claimed window is exactly where a second runner needs the
	// answer. Rule 2: the capture lives in taskRunIdentityForHolder.
	recovered, err := taskRunRestampClaim(ctx, db, prev, claimedBy, now)
	if err != nil {
		return err
	}
	if recovered {
		return nil // crash-recovery resume won the re-stamp
	}
	// Shape ③: in_progress whose holder is PROVABLY GONE and whose run lock is
	// FREE — the revival `gopi task status` already prints the recipe for. The
	// two measurements live in ONE place (taskRunReviveStrandedClaim), which is
	// the same evidence `task status` renders, so the printed instruction and
	// the gate can never disagree again. A LIVE holder, a HELD lock, or an
	// unmeasurable lock all decline and fall through to the refusal below, which
	// then names the blocker it measured.
	revived, reviveBlocked, rvErr := taskRunReviveStrandedClaim(ctx, db, prev, claimedBy, now)
	if rvErr != nil {
		return rvErr
	}
	if revived {
		return nil // the stranded row is claimed again — the run proceeds
	}
	// RowsAffected==0 — the row exists but is not claimable. Report the
	// actual status and holder so the refusal names WHO holds it (the
	// M4.5 "already live-claimed" refusal shape): in_progress means a
	// runner is executing this task RIGHT NOW — double-running would
	// double-execute the briefing (P0). Terminal states are finished;
	// a missing row is a ghost id.
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return fmt.Errorf("task: claim: %w", err)
	}
	if t == nil {
		return fmt.Errorf("task: claim: no task with task_id=%q — refusing to run a ghost id", taskID)
	}
	holder := t.ClaimedBy
	if holder == "" {
		holder = "(none)"
	}
	if reviveBlocked != "" {
		return fmt.Errorf("task: claim: task %s is %s (holder %s) — refusing to run: the row is not revivable because %s; only pending or claimed rows, or an in_progress row whose holder is dead AND whose run.lock is free, are claimable",
			taskID, t.Status, holder, reviveBlocked)
	}
	return fmt.Errorf("task: claim: task %s is %s (holder %s) — refusing to run: only pending or claimed (crash-recovery) rows are claimable, never an in_progress or finished row",
		taskID, t.Status, holder)
}

// TaskStart transitions a claimed task to in_progress — the lock
// protocol's Phase-3 advance (M4.21: 双锁齐 = claimed→in_progress on both
// sides; the runner only calls this AFTER the remote lock round succeeded,
// so an in_progress row always means BOTH locks were held). The old
// pending→in_progress passthrough is REMOVED: a pending row must go
// through the Phase-1 CAS claim first (TaskNextFiltered) — a direct start
// would bypass the two-phase lock's exactly-one-winner guarantee. A
// claimed row started here is the CRASH-RECOVERY path (⑥ claimed 瞬时化:
// `task run <id>` resumes a row a dead claimer left claimed).
//
// THE `by` ARGUMENT IS THE HOLDER STAMP (claimed-holder-freshness epic,
// 2026-09-12): it used to be discarded outright (TaskStart ignored it and
// called TaskUpdate), so the claimed_by column kept whatever the Phase-1 CAS
// wrote — possibly a DIFFERENT process, possibly a long-dead one. Every
// liveness consumer reads that column (zombieVerdict's pid veto, the
// finish-cleanup ownership gate), so an in_progress row could carry a dead
// holder's stamp and veto its own stale detection forever. Resolution: `by`
// is re-stamped here, atomically with claimed→in_progress, and the caller
// passes the identity of the process that will actually EXECUTE the row (an
// empty `by` falls back to this process — see taskDefaultClaimHolder).
func TaskStart(taskID, by string) (string, error) {
	taskID = taskIDNormalize(taskID)
	return TaskStartForce(taskID, by, false)
}

// TaskStartForce is TaskStart with the operator's --takeover override (epic
// task-run-singleton-guard). force=false keeps the status CAS that makes the
// claimed→in_progress advance single-winner: two runners that both somehow got
// past the guard cannot both execute, because the loser's CAS matches no row
// and is reported as a refusal instead of being retried away. force=true — the
// explicit `--takeover` run — relaxes the CAS to the row id, because the
// operator has just asserted that the recorded holder is not to be trusted.
//
// The export exists so the override is ONE named call site the CLI passes a
// flag into (Rule 2: one writer per decision), never a second implementation.
func TaskStartForce(taskID, by string, force bool) (string, error) {
	holder := strings.TrimSpace(by)
	if holder == "" {
		holder = taskDefaultClaimHolder()
	}
	return taskUpdateStampedFrom(taskID, taskStatusInProgress, "", "", holder, taskStatusClaimed, force)
}

// taskExecPidNone is the execPid sentinel meaning "no child process was exec'd
// by this caller, so the identity columns keep their pre-existing rule" (the
// holder's own pid, or this process). Rule 1: the zero of the parameter is a
// named value, never a bare 0 at four call sites.
const taskExecPidNone = 0

// taskRunTakeoverWord is the operator flag that relaxes the launch-seam
// assertion — the same `--takeover` that always relaxed the start CAS. Rule 1b:
// the spelling the refusal prints comes from one named value.
const taskRunTakeoverWord = "takeover"

// TaskStartChildAdmitted is the ORDINARY run door's start: `claimed` →
// `in_progress` written AFTER the child process has been exec'd, with the
// CHILD's pid captured in the row's process-identity columns.
//
// WHY THIS EXISTS (operator ruling, epic 01a1a87b). The row's two statuses
// had become equivalent because the writing moment was borrowed:
//
//	claimed      was "the local lock is half-acquired"
//	in_progress  was "both locks are acquired"  ← a LOCK fact
//
// so a row could read `in_progress` with no child in existence at all, and the
// one question an in_progress row must answer — "who is running it, and is
// that process still there?" — had no answer. The states now mean:
//
//	claimed      the row's ownership is decided (claimed_by names the holder);
//	             whether a child exists is NOT claimed — lock_phase says what
//	             the claim-lock protocol has actually completed.
//	in_progress  a child process was exec'd successfully and is being waited on;
//	             proc_pid/proc_start_ticks/proc_cmd name THAT child, so
//	             /proc/<pid>/stat's start ticks verify it (Rule: never a pid
//	             number alone).
//
// The CAS is unchanged from TaskStartForce — pinned to `claimed` unless
// --takeover relaxes it — so the single-winner property two racing runners
// relied on is preserved verbatim; only the MOMENT moved.
func TaskStartChildAdmitted(taskID, by string, childPid int, force bool) (string, error) {
	holder := strings.TrimSpace(by)
	if holder == "" {
		holder = taskDefaultClaimHolder()
	}
	return taskUpdateStampedCore(taskID, taskStatusInProgress, "", "", holder, taskStatusClaimed, force, false, false, childPid)
}

// TaskRequireClaimed is the LAUNCH-SEAM assertion that replaces the old
// claimed→in_progress CAS at `gopi task run`'s pre-spawn point.
//
// WHAT IT IS FOR, and why it is not a status write. The old code advanced the
// row to `in_progress` BEFORE any child existed, and that write doubled as the
// "of two racing runners, exactly one proceeds" gate. Moving the transition to
// after a successful exec must not weaken that gate, so the gate itself stays
// here — as an ASSERTION: the row must still be `claimed` (the winner's claim is
// the one in force). The single-winner property is not lost: the real
// exactly-one-winner CAS is the pending→claimed claim the caller already ran
// (TaskClaimExplicitForce), and this assertion re-reads its outcome at the
// moment execution is about to start. --takeover relaxes it for the operator,
// exactly as it relaxed the CAS.
//
// It returns the row's status so a caller can say what it found, and it REFUSES
// by name — an unreadable ledger and a wrong status are different rulings and
// must not collapse into one error (Rule 15).
func TaskRequireClaimed(taskID string, force bool) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", fmt.Errorf("task: %s: the claim could not be re-read at the launch seam: %w", taskID, err)
	}
	defer db.Close()
	row, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", fmt.Errorf("task: %s: the claim could not be re-read at the launch seam: %w", taskID, err)
	}
	if row == nil {
		return "", fmt.Errorf("task: %s: no ledger row names it, so there is no claim to re-read", taskID)
	}
	status := row.Status
	if force {
		return status, nil
	}
	if status != taskStatusClaimed {
		return status, fmt.Errorf("task: %s: launch refused — the row is %q, not %q: another runner won the claim "+
			"and this one must not execute (--%s is the operator's own override)",
			taskID, status, taskStatusClaimed, taskRunTakeoverWord)
	}
	return status, nil
}

// TaskStampStartFailure records WHY a run stopped at `claimed`: the start was
// admitted, the exec was NOT. The status stays `claimed` (constraint: the
// failure branch never enters in_progress) and the reason carries the cause, so
// a reader sees "owned, never exec'd — and here is why" instead of an
// `in_progress` row whose holder never had a child.
//
// It writes through taskEventWrite rather than a raw UPDATE for one reason: the
// row's `reason` column must not look like a terminal verdict, and the append-
// only trail is where "this attempt failed at the exec seam" belongs. The
// STATUS is deliberately untouched — a same-status no-op would rewrite the
// clock and make the row look freshly touched (see taskUpdatePreservingUpdatedAt's
// argument about updated_at being a re-arm marker).
func TaskStampStartFailure(taskID, reason string) error {
	taskID = taskIDNormalize(taskID)
	reason = strings.TrimSpace(reason)
	if taskID == "" {
		return fmt.Errorf("task: task_id must not be empty")
	}
	if reason == "" {
		// Rule 15: a failure record with no cause is the silent-failure shape.
		return fmt.Errorf("task: start-failure record for %s needs a reason", taskID)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	taskEventWriteErr2 := taskEventWriteErr(ctx, db, taskID, TaskStartFailureEvent, reason)
	if taskEventWriteErr2 != nil {
		return taskEventWriteErr2
	}
	return nil
}

// TaskReleaseLockPhase removes the lock-progress record — for the paths where the
// lock was given up (a yield reverts the row to pending, so a surviving
// `lock_phase=local` would describe a lock nobody holds). Clearing a column
// that is already empty is a no-op, so callers need not pre-check.
func TaskReleaseLockPhase(taskID string) error {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return fmt.Errorf("task: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET `+tasksLockPhaseColumnName+` = '' WHERE task_id = ?`, taskID); err != nil {
		return fmt.Errorf("task: clear lock phase %s: %w", taskID, err)
	}
	return nil
}

// TaskStartFailureEvent is the append-only trail marker for a run that was
// ADMITTED (the row is `claimed`, the local CAS won) and then failed to exec
// its child. It is a named constant because readers grep for it (Rule 1b).
const TaskStartFailureEvent = "start-failed"

// TaskStampLockPhase writes the claim-lock protocol's own progress onto the
// row — the fact that used to be expressed by advancing the STATUS.
//
// It writes NOTHING for the empty phase: the single-node path declares no peer,
// so there is no lock progress to report, and a row that never ran the protocol
// must not grow a field saying so on every run (constraint 5: no noise in the
// single-node path). An unreadable ledger is returned, never swallowed.
func TaskStampLockPhase(taskID, phase string) error {
	taskID = taskIDNormalize(taskID)
	phase = strings.TrimSpace(phase)
	if taskID == "" {
		return fmt.Errorf("task: task_id must not be empty")
	}
	if phase == "" {
		return nil // no protocol ran: nothing to record, and no noise
	}
	switch phase {
	case TaskLockPhaseLocal, TaskLockPhaseBoth:
	default:
		return fmt.Errorf("task: unknown lock phase %q (want %q or %q)", phase, TaskLockPhaseLocal, TaskLockPhaseBoth)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET `+tasksLockPhaseColumnName+` = ? WHERE task_id = ?`, phase, taskID); err != nil {
		return fmt.Errorf("task: stamp lock phase %s=%s: %w", taskID, phase, err)
	}
	return nil
}

// TaskStampRun records HOW a task's execution was launched (run provenance,
// devdoc/run-method-provenance.md): run_method ∈ runcmd.Method* (auto|child|
// manual|worker|dispatch|resume|merger) and run_cmd = the exact re-runnable
// command line rendered by runcmd.Command from the launcher's real argv (on
// a crashed row the run_cmd IS the resume recipe). Semantics: stamped ONCE
// at run start (right after the claim/start transition), NEVER cleared by
// writeback. Best-effort: a later no-op stamp (empty method — the caller
// does not know its own context) must NOT wipe an earlier stamp, so empty
// method is a no-op and empty run_cmd never overwrites a filled one. Errors
// are returned for callers to log-and-continue (a stamp failure must not
// fail the run).
func TaskStampRun(taskID, method, cmdline string) error {
	taskID = taskIDNormalize(taskID)
	method = strings.TrimSpace(method)
	if method == "" {
		return nil // no-op stamp: nothing to record, nothing to clear
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	// updated_at moves so the sync version anchor reflects the stamp; the
	// WHERE task_id guards against racing a row deletion. cmdline is
	// preserved when empty (keep an earlier stamp's command) and
	// overwritten when a re-run supplies a newer one (last-non-empty-wins
	// — a resume of a crashed row should record ITS launch command).
	if cmdline == "" {
		_, err = db.ExecContext(ctx,
			`UPDATE tasks SET run_method = ?, updated_at = ? WHERE task_id = ?`,
			method, float64(time.Now().Unix()), taskID)
	} else {
		_, err = db.ExecContext(ctx,
			`UPDATE tasks SET run_method = ?, run_cmd = ?, updated_at = ? WHERE task_id = ?`,
			method, cmdline, float64(time.Now().Unix()), taskID)
	}
	if err != nil {
		return fmt.Errorf("task: stamp run %s=%s: %w", taskID, method, err)
	}
	return nil
}

// TaskStampSelfClaim marks an in_progress row as OWNED by the current
// runner process (claimed_by = hostname:pid, claimed_at = now). This is the
// auto-registration ownership fix (stale-zombie epic, 2026-09-05): the
// auto-register start hook inserts a row directly in_progress with NO claim
// stamp (only wrapper/task-run rows carried host:pid via the CAS claim
// ladder), so when the runner was killed the row stayed at "suspected" in
// the staleness evidence chain forever — zombieVerdict only upgrades to
// CONFIRMED when a claimed pid is verified dead. Stamping the row with the
// runner's own pid at birth brings auto rows under the trusted pid-death
// layer: a dead runner → CONFIRMED, so the stale report names the dead
// holder instead of "cannot tell"; a LIVE runner → the pid veto suppresses
// the stale event (a slow live round is never falsely reported). No
// cleanup rides this: disposal was removed 2026-10-26
// (devdoc/zombie-sweep-removed.md). The row IS the run, so the runner owns
// it — the stamp is birth
// provenance, not a lock. Claimed rows are never exported in sync deltas
// (claimed_by stays home), so the stamp has no multi-node reach. Guarded to
// in_progress (a row that went terminal meanwhile keeps its timestamps and
// claim trail untouched).
//
// in_progress_at is stamped in the SAME write (claimed-holder-freshness
// epic): this function is a RESUME path (the auto-register hook runs when a
// session starts executing a row that may already be in_progress), so the
// holder stamp it writes must be the referent's equal, never older. A stamp
// older than the row's in_progress transition is exactly the shape
// claimStampFresh reports as UNVERIFIED — writing both from one `now` removes
// the whole class (including the clock-skew window between two calls).
func TaskStampSelfClaim(taskID string) error {
	taskID = taskIDNormalize(taskID)
	pid := os.Getpid()
	return TaskStampHolder(taskID, taskDefaultClaimHolder(), pid)
}

// TaskStampHolder is TaskStampSelfClaim for an EXPLICIT holder process: it
// writes claimed_by/claimed_at/in_progress_at plus the triple captured from
// pid — ONE statement, so the row can never show a holder without the three
// facts that make the holder verifiable (the same invariant
// taskUpdateStampedFrom's start path keeps).
//
// WHY THE PARAMETER (epic dead-holder-resume, 2026-09-16): recovering a dead
// holder needs the RECOVERING process to become the recorded holder, and the
// recovering process is a detached CHILD (the resume run) whose pid only its
// PARENT knew. The parent therefore performs the ownership handoff on the
// child's behalf — atomically, in the spawn's immediate aftermath — instead of
// letting the child race to self-stamp after a heavy agent bootstrap. The child
// re-stamping itself is NOT relied upon: the ledger never has to pass through
// an "old dead pid" state after a successful spawn.
//
// A pid with no readable identity records NOTHING (the Rule-15 shape shared
// with taskUpdateStampedFrom: neither a zero-valued pseudo-identity nor a
// cleared triple — the previous evidence is left in place rather than
// replaced by a worse one).
func TaskStampHolder(taskID, holder string, pid int) error {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	now := float64(time.Now().Unix())
	ident, ok := taskCaptureProcIdentity(pid)
	if !ok {
		return fmt.Errorf("task: holder stamp %s: no readable identity for pid %d (pid gone or procfs unreadable)", taskID, pid)
	}
	// THE STAMP IS ALSO THE RESUME (level 1 of the two-level policy,
	// task_run_retry.go): a `failed` row being entered by a run is a resume, so
	// the statement MOVES the row to in_progress in the same write that records
	// the holder. The pre-fix shape pinned this UPDATE to `status=in_progress`,
	// so on a failed row the ownership handoff matched ZERO rows and was
	// reported as success — the row kept saying `failed` (and `holder: (none)`)
	// while a live run was executing it, which read to the operator as "the
	// resume failed again" (the 2026-09-13 incident, cli_resume_failed_row_test).
	//
	// ONE statement, no read-then-write: the status it moves FROM is the status
	// it matched on, so two concurrent resumers cannot both win — the first one
	// leaves the row in_progress and the second's WHERE no longer matches (its
	// own guard, taskRunGuardRuling, refuses it anyway; this is the belt to that
	// brace). A row on any OTHER status is still untouched: the terminal and
	// pending verdicts are not this function's to change.
	//
	// THAT LEAVES A HOLE THIS FUNCTION CANNOT CLOSE BY ITSELF, and it is stated
	// rather than papered over: a caller that stamps a `completed` row gets the
	// same nil error and the same untouched row — the silent no-op that was the
	// old defect, now confined to the statuses the state machine forbids. The
	// REAL guard is upstream and it is the one that decides: the resume gate
	// refuses a finished row before any stamp (TaskRunResumeStatusGate, read by
	// `gopi task status`, `gopi session resume` and `gopi worker --resume`). The
	// stamp is therefore never the place a finished row is prevented — it is the
	// place a FAILED row is moved, and the caller that reaches it has already
	// been told "admitted".
	_, err = db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+` = ?, updated_at = ?,
		 `+tasksProcPidColumnName+` = ?, `+tasksProcStartColumnName+` = ?, `+tasksProcCmdColumnName+` = ?
		 WHERE task_id = ? AND status IN (?, ?)`,
		taskStatusInProgress, holder, now, now, now,
		ident.Pid, ident.Start, ident.Cmd, taskID, taskStatusInProgress, taskStatusFailed)
	if err != nil {
		return fmt.Errorf("task: holder stamp %s: %w", taskID, err)
	}
	return nil
}

// TaskResumeClaimed reclaims a CLAIMED row for execution: the crash-recovery
// shape (M4.21: the previous holder died between the Phase-1 CAS claim and
// the Phase-3 advance — claimed is transient and its only durable
// justification is this recovery). The claim trail is re-stamped so the
// audit shows who recovered it. Superseded as `task run <id>`'s entry by
// TaskClaimExplicit (which claims pending rows through the same Phase-1 CAS
// ladder too); kept for the recovery-only callers and its test matrix.
func TaskResumeClaimed(taskID, claimedBy string) error {
	taskID = taskIDNormalize(taskID)
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return fmt.Errorf("task: resume: %w", err)
	}
	if t == nil {
		return fmt.Errorf("task: no task with task_id=%q", taskID)
	}
	if t.Status != taskStatusClaimed {
		return fmt.Errorf("task: %s is %s, not claimed — nothing to recover", taskID, t.Status)
	}
	if claimedBy == "" {
		claimedBy = taskDefaultClaimHolder()
	}
	// SINGLETON GUARD (epic task-run-singleton-guard, 2026-09-15): re-stamping
	// the claim trail must not steal a row from a LIVE holder. Before this
	// gate, the only arbitration was the conditional UPDATE on status =
	// claimed — so a runner re-stamping a row ANOTHER live runner had just
	// claimed (and was about to execute) won the CAS and executed too. The
	// three-factor identity questions the holder, not the status: a live other
	// process refuses the recovery, a dead/recycled/own holder recovers as
	// before.
	verdict, refusal, rErr := taskRunGuardRuling(t)
	if rErr != nil {
		return fmt.Errorf("task: resume: %w", rErr)
	}
	if !taskRunGuardAdmits(verdict) {
		return fmt.Errorf("task: resume: %s", refusal)
	}
	// No in_progress_at write here, and that is the point: this row is still
	// CLAIMED — its execution stretch has not begun, so there is no referent
	// to pin yet (a stamp written now would claim a stretch nobody started,
	// and the real start would then have to move it). The holder stamp lands
	// with the transition it belongs to: TaskStart/TaskStampSelfClaim write
	// claimed_by + in_progress_at together.
	//
	// The re-stamp is PINNED to the holder state just judged (taskRunHolderPin)
	// and carries the claim-time identity in the same statement: without the
	// pin, two operators recovering the SAME dead claim would both win their
	// conditional UPDATE (both read 'claimed' before either wrote) and both
	// execute the row — the exact single-winner property the epic requires.
	now := float64(time.Now().Unix())
	applied, rErr := taskRunRestampClaim(ctx, db, t, claimedBy, now)
	if rErr != nil {
		return fmt.Errorf("task: resume: %w", rErr)
	}
	// applied==false is the LOST RACE, not a success (Rule 15: an ignored
	// result is a silent failure). The pin matched no row, so the claim moved
	// between the guard's read and this write — another runner recovered it
	// first (or the row left the claimed state). This caller must NOT proceed
	// to execute: that is precisely the double-run the epic forbids.
	if !applied {
		return fmt.Errorf("task: resume: %s moved on between the guard's verdict and this re-stamp (another runner recovered it first) — refusing this run", taskID)
	}
	return nil
}

// TaskRunWriteback closes out an executed task: status completed|failed and
// the outcome recorded as a task_events row (run stats / result digest).
// bug056: the outcome NEVER overwrites the summary — the submit-time
// payload digest is immutable; the digest lives in the audit trail and
// show renders it as `result:`. This is the HARNESS path — it goes through
// TaskUpdateWithResult (the CLI update verb's plain path stays status-only,
// content immutable; its --result flag now reaches this same write —
// bugfix-task-p1). The state machine inside taskUpdate gates it (claimed →
// completed is legal, completed → completed is an idempotent no-op).
//
// Deliverable.branch backfill (bugfix epic 2026-09-02 — adoption merge gate
// never fired): a COMPLETED task run leaves the branch it executed on in
// the row's deliverable JSON so the adoption sweep can pair it (Option A —
// backfill at completion; see task_deliverable_branch.go). Best-effort and
// AFTER the terminal write commits: a resolution failure never rolls back
// the outcome — the branch backfill is the adoption chain's input, not the
// task's result.
func TaskRunWriteback(taskID, status, resultSummary string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task writeback: task_id must not be empty")
	}
	if status != taskStatusCompleted && status != taskStatusFailed {
		return "", fmt.Errorf("task writeback: status must be completed|failed, got %q", status)
	}
	out, err := TaskUpdateWithResult(taskID, status, resultSummary)
	if err != nil {
		return "", err
	}
	// bugfix epic 2026-09-02 (Option A): only a COMPLETED run pairs a branch
	// — a failed run's branch holds no adoptable delivery. Resolve the
	// executed branch from the child session's recorded workdir and merge it
	// into the deliverable blob. Side-channel by contract: every failure is
	// logged and ignored, never an error return (the writeback outcome above
	// is the authoritative terminal fact).
	if status == taskStatusCompleted {
		if branch := taskWritebackBranch(); branch != "" {
			if berr := taskBackfillDeliverableBranch(taskID, branch); berr != nil {
				taskNotifyLog("task %s: deliverable.branch backfill skipped: %v", taskID, berr)
			}
		}
	}
	// Arm 1's ENFORCEMENT half (the run's OWN untracked residue at close-out)
	// lands one layer down, in taskUpdate — the ONE state gate every door
	// shares (this writeback, the auto-register end hook, the merger's
	// executor, the CLI's own `task update`) — so neither the record nor the
	// operator's enforce switch can be missed by a path that bypasses this
	// wrapper.
	return out, nil
}

// Run-lineage env (worker-writeback epic, 2026-09-12; boundary corrected by
// task 01a16f7c, 2026-10-25).
//
// ONE batch of variables, TWO different questions — and only one of them
// survives. This comment names the boundary so a later reader does not
// re-merge them.
//
// WHAT THESE VARIABLES ARE NOW: a LINEAGE RECORD. cmd/agent's spawn seam
// (cli_tasks.go runTaskRunOnce) stamps them on the child's environment before
// Start; the child inherits them into everything it spawns, and the kernel
// keeps them after reparenting. So a process that still carries them is a
// process this run's tree produced — the evidence
// task_finish_cleanup_holder_orphan.go reads to decide whether
// a worktree-pinning orphan belongs to a run or to a third party.
//
// THE OTHER READER IS GONE (deleted 2026-11-05 in 386731b5). What this comment
// used to name — the WORKER-side run close-out reap (`runCloseoutLineageNamesRun`)
// and the environ census it shared with the job tool (`tools.JobDescendantPIDs`)
// — was the mechanism that asked "does this process carry MY run id?" and then
// SIGNALLED it. Both symbols are deleted, together with `taskRunReapOwnChildren`
// and `taskRunReleaseOwnWorktrees`: the reap judged ownership from an INHERITED
// env record, so a nested run's children read as the parent's leftovers
// (measured on 01a1a876 — five close-outs, five non-empty batches, 2/5/3/4/3 —
// and the live rows 01a1a87b / 01a1a885 went silent within a minute of one of
// them). WHAT READS THE ENVIRON NOW is the job tool's own per-spawn nonce
// (`jobDescendantMarkerEnv`, internal/tools/job_kill_reap.go:jobMarkerPids) —
// a value JobStart MINTS for one job, never one this seam stamps — and
// internal/tools/procsignal.go is the only incarnation-guarded sender left.
// So: nothing in this binary may read these variables to decide what to signal.
//
// WHAT THEY ARE NOT: an answer to "is my parent still alive?". The CHILD-side
// close-out that asked that (internal/agent's closeOutRunRow →
// TaskRunCloseoutStaleRun) is DELETED, and it must not be reintroduced: the
// wrapper is by definition the process that WAITs for its child, so a wrapper
// that is gone cannot be asked and a live one refuses the write — the answer
// was known before the question, and the write it guarded was a no-op on every
// healthy run. Closing the wrapper's row is the WRAPPER's own post-Wait
// writeback, and a wrapper killed before it reaches that line is what the
// ledger's stale/zombie machinery dispositions.
//
// Rule 2: TaskRunIDEnv and TaskRunWrapperPIDEnv each have exactly one writer
// (cmd/agent/cli_tasks.go, pre-spawn) and their readers are the lineage scans
// named above.
const (
	// TaskRunIDEnv carries the run's ledger row id to the child — the
	// row this run is executing. It is read as a LINEAGE MARKER (a process
	// whose exec-time environment names this row was spawned by this run) and
	// as the run identity several diagnostics print.
	TaskRunIDEnv = "GOPI_TASK_RUN_ID"
	// TaskRunOutcomeEnv carries the wrapper's outcome for the row
	// ("completed" when the child was launched, "failed" when the wrapper
	// never launched it).
	//
	// env-reader-exempt: no production reader remains. Its ONE reader was the
	// deleted child-side close-out (internal/agent closeOutRunRow, removed by
	// task 01a16f7c together with the pid-liveness verdict it fed); the value
	// is still STAMPED by the spawn seam because the variable is part of the
	// run's published lineage record — internal/tools' env sanitizer pins its
	// presence (task.go's "DELIBERATELY NOT in the set" clause) and the
	// lineage/reap tests assert the stamp. Deleting the constant would silently
	// drop the stamp; see devdoc/run-closeout-boundary.md.
	TaskRunOutcomeEnv = "GOPI_TASK_RUN_OUTCOME"
	// TaskRunWrapperPIDEnv carries the WRAPPER process's pid to the child, so
	// a later scan can recognise the descendants of that run by the pid the
	// run published — a lineage marker, never a liveness verdict.
	TaskRunWrapperPIDEnv = "GOPI_TASK_RUN_WRAPPER_PID"
)

// RunCloseoutPair is the seam's single constructor for the pre-spawn OUTCOME
// pair (row id + what that row becomes), so the writer that stamps the child's
// environment and every reader of that pair agree by construction rather than
// by two hand-assembled literals (Rule 1b).
func RunCloseoutPair(taskID, outcome string) (envTaskID, envOutcome string) {
	return taskIDNormalize(taskID), strings.TrimSpace(outcome)
}

// hasBugTag reports whether a normalized comma-separated tag string contains
// the reserved bug-flow tag (exact word match: "bug" inside "bugflow" or
// "debug" does NOT count — the tag set is comma-split, not substring).
// It is the narrower question — "does this row carry the reserved marker"
// (the tally echo on submit, the rework child's tag append) — as opposed to
// the bug-family predicate's "is this row bug work".
// The token match itself is taskHasTagToken (one implementation of "is this
// token in the set", shared with the closure gate).
func hasBugTag(tags string) bool {
	return taskHasTagToken(tags, tasktags.BugTag)
}

// M4.11 priority tiers (t-1788087991): lower runs first. The tier values
// are the queue's semantic vocabulary — bugs jump the line, docs go last.
// Rule 1: every tier literal lives here.
const (
	taskPriorityBug     = 1 // tags contains bug — highest (LLM-reported bugs are bugs too)
	taskPriorityDefault = 5 // standard tier (feature / P1.5 / 新能力 / unmapped tags)
	taskPriorityDocs    = 8 // tags contain docs / doc-only — lowest
)

// taskClaimOrderBy is THE queue order — the single ORDER BY that decides
// which pending row a dispatcher claims (Rule 14: one definition, referenced
// by every reader). Its readers are the DISPATCH side:
//
//   - TaskNextFiltered (the claim funnel: worker round / serve dispatch) —
//     the authority on execution order;
//   - taskNextClaimID (TaskList's [next] marker resolution) — so the prompted
//     row is the row the funnel takes, whatever order the render is in.
//
// It is NOT the render order of `gopi task list`: every view of that verb is
// newest-first (taskListOrderNewestFirst), and the two orders are both NAMED
// on the surface (TaskListOrderNote + cmd/agent's tasksUsage). An earlier
// revision rendered the pending view in this clause so its top row WAS the
// next claim (operator report 2026-09-15); that made the render contradict the
// verb's own help ("List tasks, newest first") without saying so — the defect
// measured 2026-09-26, epic task-list-order-surface. The marker resolves it:
// the list says which order it is in, and [next] says which row runs next.
//
// Inside a priority tier the order is created_at ASC — oldest first, i.e.
// FIFO. The tier itself is a deliberate, documented exception to FIFO
// (M4.11 task-priority: bugs=1 run before feature=5 before docs=8), so this
// clause is FIFO *within* a tier, never across tiers. COALESCE: a legacy NULL
// priority means the default tier.
//
// A var, not a const: the tier value is interpolated from taskPriorityDefault
// with strconv.Itoa, which is not a constant expression — interpolating it
// keeps the tier literal in exactly one place (Rule 1/13) instead of
// mirroring a "5" into this string.
var taskClaimOrderBy = ` ORDER BY COALESCE(priority, ` + strconv.Itoa(taskPriorityDefault) + `) ASC, created_at ASC, id ASC`

// Task-ledger tag values (Rule 1b/13: the token is declared once and
// REFERENCED by every reader — the Go predicates AND the SQL population
// fragment below, which are the two faces of the same rule).
const (
	// bugTag is the reserved bug-flow tag: the entry point of the
	// bug-submit → dispatch → verify closure.
	bugTag = "bug"
	// bugFamilyPrefix is the token prefix every bug-family tag carries —
	// the family is bug, bug033, bug037… (see hasBugTagFamily).
	bugFamilyPrefix = bugTag

	taskTagDocs    = "docs"
	taskTagDocOnly = "doc-only"
)

// AppendTaskTag returns `tags` with `tag` appended if it is not already
// present as a comma-token — the ONE spelling of "add a reserved tag to a
// row's tag string" (Rule 1b/13), so a caller that must mark a row with a
// protocol value (the CLI's `submit --fixture`, tools' own submit faces)
// never retypes the joining convention and never double-appends.
//
// Matching is the comma-token rule the whole family uses (taskHasTagToken):
// "bug" inside "bugflow" is a different tag, and an empty input yields the tag
// alone with no leading comma.
func AppendTaskTag(tags, tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return normalizeTaskTags(tags)
	}
	if taskHasTagToken(tags, tag) {
		return normalizeTaskTags(tags)
	}
	return normalizeTaskTags(tags + "," + tag)
}

// hasBugTagFamily reports whether a tag string carries at least one
// BUG-FAMILY tag — a token equal to bugTag or STARTING with it ("bug",
// "bug033", "bug037", "bugflow"), which is the family the queue already
// ranks as bug work. A token merely CONTAINING the word mid-token
// ("debug", "debug-ui", "wbug") is NOT bug work — the tag set is
// comma-split, never substring-matched. Matching is case-sensitive, on the
// comma-canonical form produced by normalizeTaskTags.
//
// This predicate is THE definition of "this row is bug work" (Rule 14): the
// queue's priority tier (taskTagPriority) and the bug-flow adoption tally
// (taskBugPopulationWhere, its SQL twin) both resolve to it, so the
// population the tally COUNTS can never diverge from the population the
// queue RECOGNIZES. The two faces are pinned together by
// TestBugPopulationMatchesTokenRule.
func hasBugTagFamily(tags string) bool {
	for _, t := range strings.Split(tags, ",") {
		if strings.HasPrefix(strings.TrimSpace(t), bugFamilyPrefix) {
			return true
		}
	}
	return false
}

// taskBugPopulationWhere is the SQL twin of hasBugTagFamily — the WHERE
// fragment selecting the rows carrying at least one bug-family tag. It is
// a comma-anchored GLOB, not a LIKE '%bug%' substring match: the tag set is
// comma-separated, so a bug-family token is one that follows the row's
// opening boundary comma (implicitly supplied by the ','||…||',' wrapper)
// and starts with bugTag. The tokens "debug" / "debug-ui" / "wbug" are
// excluded — matching hasBugTagFamily exactly.
//
// Case sensitivity is deliberate: SQLite LIKE is case-INsensitive while the
// Go predicate is case-sensitive, so a LIKE-based twin would count "BUG"
// rows the Go rule rejects. GLOB is case-sensitive and preserves both.
// Embedded spaces are stripped before tokenizing because the Go twin trims
// each token; every row written through the insertTaskRow funnel is already
// normalized, so the strip only ever rescues an un-normalized (imported)
// row into agreement rather than out of it.
//
// The fragment references the tasks table's bare tags column — callers use
// it as `... WHERE ` + taskBugPopulationWhere (no table alias).
const taskBugPopulationWhere = "(','||replace(COALESCE(tags,''),' ','')||',') GLOB '*," +
	bugFamilyPrefix + "*'"

// taskTagPriority maps tags to the default priority (the ONE definition —
// TaskSubmit fills priority from this whenever the submitter does not pass
// --priority explicitly). Precedence: bug beats docs beats default — a row
// tagged both bug and docs is a bug. The bug test is hasBugTagFamily (the
// single source of truth) so the priority tier and the bug-flow population
// query can never drift apart.
func taskTagPriority(tags string) int {
	if hasBugTagFamily(tags) {
		return taskPriorityBug
	}
	for _, t := range strings.Split(tags, ",") {
		t = strings.TrimSpace(t)
		if t == taskTagDocs || t == taskTagDocOnly {
			return taskPriorityDocs
		}
	}
	return taskPriorityDefault
}

// TaskBugAdoptionStats renders the bug-flow adoption tally (Rule 11 proof
// surface): all bug-tagged tasks ever, plus the acceptance loop's outcome
// split (verified / reopened). The tally is computed fresh on every
// bug-tagged submission, so `gopi task submit --tags bug ...` leaves a
// countable line in the invoking session's log — the adoption-rate
// measurement the epic asks for.
func TaskBugAdoptionStats(ctx context.Context, db *sql.DB) (string, error) {
	// The population is the SAME one the queue's priority tier recognizes:
	// the shared taskBugPopulationWhere fragment (Rule 14). An earlier
	// `tags LIKE '%bug%'` counted rows whose only "bug" was mid-token
	// ("debug-ui") — a measurement that over-counts is worse than one that
	// errors.
	rows, err := db.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM tasks WHERE `+taskBugPopulationWhere+` GROUP BY status`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	counts := map[string]int{}
	total := 0
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return "", err
		}
		counts[st] = n
		total += n
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	verified := counts[taskStatusVerified]
	reopened := counts[taskStatusReopened]
	// M4.20-B: reopened is retired — legacy rows aside, the FAIL verdict
	// now lands on the row as failed. The tally counts failed bug rows as
	// the "reissued" bucket (the rework child carries the bug tag forward).
	failed := counts[taskStatusFailed]
	return fmt.Sprintf("[bug-flow] tasks with tag bug: %d (verified %d, reopened %d, other %d)",
		total, verified, reopened+failed, total-verified-reopened-failed), nil
}
