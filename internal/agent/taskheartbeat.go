package agent

// Task liveness tracking (M4.5 ⑦ 存活追踪 — nick 两轮简化后定案):
// two RAW FACTS written into the ledger row of the current session-task,
// no daemon, no computed heuristics:
//
//	tasklist_issued_at — the harness received a [TASK_LIST] block
//	last_toolcall_at   — a tool batch started (the heartbeat)
//
// task-liveness-coverage (2026-10-16) COMPLETED the coverage of those two
// facts. Before it, the ONLY writers of last_toolcall_at were the two
// tool-batch entries (executeToolBatchCtx / executeToolBatchParallel), so a
// run that was alive and BATTERY-QUIET wrote nothing at all in three
// intervals:
//
//	(a) between session registration and its first tool batch,
//	(b) between two batches (a long model inference),
//	(c) inside ONE long tool call (the measured 10-minute bash).
//
// In those windows "quiet but alive" and "dead" were indistinguishable in the
// ledger, while the zombie sweep's stall leg (COALESCE(last_toolcall_at,
// created_at)) is the PRIMARY candidate query — so the sweep's input was
// blind exactly where a live run looks quiet. This file now touches the row
// at every interval it knows about:
//
//	1. session registration (setTaskHeartbeatSession — the row exists, the run starts)
//	2. every loop-iteration start (touchRunIterationStart — the round boundary)
//	3. every tool-batch start (the two original call sites, unchanged)
//	4. tasklist blocks (unchanged)
//	5. an in-process KEEPALIVE that ticks on its own while the run's context
//	   is alive (startRunKeepalive) — it covers the blind spots 1–4 cannot:
//	   a single call (a bash) that outlives the interval, and a model
//	   inference that takes minutes.
//
// The keepalive is IN-PROCESS and context-bound BY CONSTRUCTION: it is a
// goroutine of this run that dies with the run's own ctx (the same scope the
// tools get), so it can never outlive the run and produce a false-live row —
// no daemon, no tick of its own, no state file (Rule 35). Its writes stay
// best-effort exactly like every other touch: a ledger problem is logged to
// stderr and never blocks or errors a round.
//
// THE STALE DETECTOR THIS FILE USED TO FEED IS DELETED (2026-11-05). It was
// QUERY-SIDE (`tools.DetectStaleTasks`): a row with status=in_progress whose
// recorded liveness instant was older than the threshold was "stale", reported
// through GOPI_TASK_NOTIFY as event=stale — i.e. one process reporting another
// process's health. The operator's ruling retired the whole carrier (detector,
// `gopi task list --sweep`, its persisted throttle stamp, its re-arm UPDATE),
// so what these writes feed now is the DISPLAY: `gopi task list` / `gopi task
// show` read the row's own facts (last_toolcall_at, tasklist_issued_at)
// through ONE arithmetic (tools.taskRecordedLivenessAt) and, separately, the
// recorded process IDENTITY against /proc. Both are readings taken by whoever
// looks; neither is a report one run makes about another.
//
// All writes are BEST-EFFORT: a ledger problem must never break the run
// (the same posture as taskregister.go).

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// taskHeartbeatThrottleMin is the minimum interval between two ledger
	// heartbeat writes — tool batches can fire several times a second and
	// the column only needs minute-resolution freshness.
	taskHeartbeatThrottleMin = 30 * time.Second

	// taskHeartbeatStaleThreshold is the DEFAULT staleness threshold: an
	// in_progress row with no toolcall for this long is stale.
	taskHeartbeatStaleThreshold = 30 * time.Minute

	// runKeepaliveInterval is how often the in-process keepalive ticks while
	// a run is alive, with NO tool batch and no tasklist block required. It
	// MUST be well under the stall grace so that a run that is quiet for a
	// legitimate reason — a long model inference, a single long tool call
	// (the measured 10-minute bash) — keeps a fresh liveness stamp instead of
	// drifting toward the grace with only created_at behind it. The default
	// grace is taskHeartbeatStaleThreshold (30min), so 9 ticks fit inside one
	// grace window at this interval.
	runKeepaliveInterval = 2 * time.Minute
)

// taskHeartbeatThrottle / runKeepaliveTick are the two write-side knobs as
// rebindable seams: they DEFAULT to the named constants above (Rule 1/13 —
// the constant stays the single definition of the production value) and exist
// only so a test can drive a real ledger write without waiting 30 seconds /
// two minutes. Production never rebinds them (the taskLiveHolderProbe
// pattern in internal/tools).
var (
	taskHeartbeatThrottle = taskHeartbeatThrottleMin
	runKeepaliveTick      = runKeepaliveInterval
)

// The heartbeat-stall grace is tunable through GOPI_TASK_STALE_SEC (the legacy
// spelling) / GOPI_TASK_STALL_SEC, READ in exactly one place:
// config.ResolveTaskStallGrace — which owns the name constants
// (config.taskStallSecEnvLegacy / config.TaskStallSecEnv). A second constant
// spelling that name HERE would be a duplicate with no reader: the unread
// duplicate was exactly what the env-reader gate (internal/standards,
// Rule 21) caught in this file.

// taskHeartbeatState throttles the ledger touches — ONE CLOCK PER COLUMN:
// lastToolcallWrite gates last_toolcall_at (tool batches fire several times
// a second, the column only needs minute-resolution freshness);
// lastTasklistWrite gates tasklist_issued_at SEPARATELY — the two raw facts
// are independent, and sharing one clock silently dropped the tasklist stamp
// whenever it arrived <30s after any tool batch (M4.5 fix: 8cbf768f emitted
// 3 [TASK_LIST] blocks, tasklist_issued_at stayed NULL).
var taskHeartbeatState struct {
	mu                sync.Mutex
	lastToolcallWrite time.Time
	lastTasklistWrite time.Time
	sessionID         string
	taskID            string
}

// tasklistThrottleMin is the minimum interval between two tasklist_issued_at
// writes — tasklist blocks are sparse (per response), so the throttle only
// guards against pathological repetition within one response burst.
const tasklistThrottleMin = 30 * time.Second

// taskHeartbeatSession returns the session whose ledger row the touches
// target: the current session id. Resolved once per process (the session id
// is fixed at start); empty when no session log exists yet.
func taskHeartbeatSession() string {
	taskHeartbeatState.mu.Lock()
	defer taskHeartbeatState.mu.Unlock()
	return taskHeartbeatState.sessionID
}

// taskHeartbeatTaskID returns the auto-registered ledger row's task_id the
// touches target ("" when none — plain sessions without a row are never
// touched). With task-id consolidation (task_id != session_id) this is the
// row key; sessionID alone can no longer address the row.
func taskHeartbeatTaskID() string {
	taskHeartbeatState.mu.Lock()
	defer taskHeartbeatState.mu.Unlock()
	return taskHeartbeatState.taskID
}

// setTaskHeartbeatSession latches the current run's session id AND its
// auto-registered ledger row's task_id (called from runEngine right after
// the session logger exists / the start hook ran — see loop.go). An empty
// taskID means no ledger row (registration off or failed) — touches then
// no-op.
func setTaskHeartbeatSession(sessionID, taskID string) {
	taskHeartbeatState.mu.Lock()
	taskHeartbeatState.sessionID = sessionID
	taskHeartbeatState.taskID = taskID
	// New run → both columns write immediately (fresh clocks).
	taskHeartbeatState.lastToolcallWrite = time.Time{}
	taskHeartbeatState.lastTasklistWrite = time.Time{}
	taskHeartbeatState.mu.Unlock()

	// Interval (a): session registration is the run's START, and it is the
	// first moment the ledger row exists and can hold a liveness fact. Without
	// this touch a run killed before its first tool batch left the row with
	// created_at as its ONLY liveness instant — the "registered but never
	// started" shape. The touch is the ordinary toolcall clock (the latch above
	// zeroed it, so this write is never throttled away).
	touchLastToolcallAt()
}

// touchRunIterationStart stamps the row at the START OF A LOOP ITERATION —
// interval (b): between two tool batches the model may think for minutes, and
// pre-fix that window wrote nothing. Called once per iteration of runEngine's
// ReAct loop, so a run that is alive but tool-quiet still advances its own
// liveness instant every round.
func touchRunIterationStart() {
	touchLastToolcallAt()
}

// startRunKeepalive runs an in-process heartbeat ticker until ctx is done.
// It is the coverage for the interval NO caller-side touch can reach: a
// SINGLE tool call that outlives runKeepaliveInterval (the measured
// 10-minute bash), or a model inference long enough to span one, during
// which no loop iteration returns.
//
// It is bound to the RUN'S OWN context (the same scope the run's tools get),
// so it dies with the run: a cancelled, finished or timed-out run leaves no
// ticker behind and can never stamp a row after the run is gone — the
// false-live failure mode a detached daemon would have (Rule 35).
//
// The goroutine is a plain `go` routine with a select on ctx.Done(); it holds
// no state of its own, and every write goes through the ordinary throttled,
// best-effort touch path. It never blocks the run and never errors it.
func startRunKeepalive(ctx context.Context) {
	if ctx == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(runKeepaliveTick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				touchLastToolcallAt()
			}
		}
	}()
}

// touchTasklistIssuedAt records a tasklist block arrival in the ledger.
// Throttled on its OWN clock (tasklistThrottleMin) — sharing the toolcall
// clock used to silently drop this write <30s after any tool batch; the
// two liveness facts are independent.
func touchTasklistIssuedAt() {
	touchHeartbeat(&taskHeartbeatState.lastTasklistWrite, func(db *sqlDBHandle, taskID, sessionID string, now float64) error {
		return db.ExecTouchTasklist(taskID, sessionID, now)
	})
}

// touchLastToolcallAt records a tool-batch start in the ledger (throttled on
// the toolcall clock).
func touchLastToolcallAt() {
	touchHeartbeat(&taskHeartbeatState.lastToolcallWrite, func(db *sqlDBHandle, taskID, sessionID string, now float64) error {
		return db.ExecTouchLastToolcall(taskID, sessionID, now)
	})
}

// touchHeartbeat resolves the target row and runs one touch with the named
// clock's throttle applied; all failures are swallowed with a stderr note.
// The clock param must point INTO taskHeartbeatState (same mutex guards it).
// The touch keys on the auto-registered row's task_id (consolidated: no
// task_id==session_id assumption) falling back to the session_id column for
// pre-consolidation rows.
func touchHeartbeat(clock *time.Time, touch func(*sqlDBHandle, string, string, float64) error) {
	taskHeartbeatState.mu.Lock()
	sessionID := taskHeartbeatState.sessionID
	taskID := taskHeartbeatState.taskID
	now := time.Now()
	if now.Sub(*clock) < taskHeartbeatThrottle {
		taskHeartbeatState.mu.Unlock()
		return
	}
	*clock = now
	taskHeartbeatState.mu.Unlock()
	if sessionID == "" {
		return // no session row to touch (resume/chat without registration)
	}

	h := openHeartbeatDB()
	if h == nil {
		return
	}
	defer h.Close()
	if err := touch(h, taskID, sessionID, float64(now.Unix())); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] task heartbeat: %v\n", err)
	}
}

// sqlDBHandle abstracts the ledger connection for the touch helpers (keeps
// the agent package free of database/sql while staying unit-testable).
type sqlDBHandle = tools.TaskHeartbeatConn

// openHeartbeatDB opens the tasks ledger read-write for heartbeat touches.
func openHeartbeatDB() *sqlDBHandle {
	return tools.OpenTaskHeartbeatConn(context.Background())
}
