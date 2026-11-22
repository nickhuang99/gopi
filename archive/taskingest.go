//go:build ignore

package tools

// Task ingest (M4.5 ①) — the RECEIVING end of the cross-node task-state
// mirror. A gopi node that RUNS tasks (e.g. the HP box) already fires
// transition notifications (tasknotify.go: GOPI_TASK_NOTIFY_WEBHOOK);
// this node (the ledger owner, e.g. peer1) accepts those POSTs and mirrors
// the terminal state into its own ledger.
//
// Why not a remote SQLite connection: --db over the network is neither
// supported nor sane (SQLite locking is file-local) — the ingest endpoint
// is the PR-style patch instead: the HP node never writes our DB directly,
// it PROPOSES a state and we apply it under our own rules.
//
// Rules (payload = the taskNotifyPayload JSON, plus an optional
// remote_node field identifying the sender):
//   - 防伪造: a payload for a task_id that does NOT exist locally is
//     refused (404 semantics) — the mirror can only annotate known rows.
//   - 前向迁移限定: only non-terminal local rows (pending/claimed/
//     in_progress) may move to a TERMINAL status (completed/failed/
//     cancelled). This is deliberately WIDER than the local dispatcher
//     table (which forbids pending → completed): across nodes the local
//     row stays pending while the remote claims/runs, so the mirror must
//     accept the remote's final jump. Never backward, never
//     non-terminal → non-terminal (those states are local-only).
//   - 幂等 upsert: a redelivery (local row already in the event's status)
//     is a no-op reporting "already" — repeated POSTs never error.
//   - provenance: source column is NOT touched; the sender is recorded as
//     a "remote:<node>" tag suffix (default node name "remote" when the
//     payload omits it) and the elapsed/tokens stats are recorded as a
//     task_events result detail ONCE (guarded against redelivery
//     duplication). bug056: the mirror's stats never touch the summary —
//     they ride the same append-only audit trail as the local writeback.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// taskIngestBodyLimit caps the decoded JSON body (a notify payload is
// < 4KB; the cap only exists to reject abuse cheaply).
const taskIngestBodyLimit = 1 << 20

// taskIngestStatsFormat is the mirrored run-stats result detail recorded as
// a task_events row (fields mirror the auto-register stats tail). bug056:
// this used to be appended to the local summary — now it rides the audit
// trail, the summary stays the submit-time digest.
const taskIngestStatsFormat = " — elapsed=%.1fs tokens=%d (from %s)"

// taskIngestSQLRaceRetries caps the CAS retry loop when a concurrent local
// writer (TaskNext claim / TaskUpdate / another ingest) wins the row between
// our read and our UPDATE (RowsAffected=0). One retry re-reads under the
// guard; more than one means a livelock no ledger write should produce.
const taskIngestSQLRaceRetries = 2

// taskIngestRaceHook, when non-nil, fires immediately before each guarded
// mirror UPDATE — the deterministic race-injection point for tests (a
// concurrent writer advances the row exactly inside the check→write gap).
// Always nil in production.
var taskIngestRaceHook func()

// taskIngestMirrorableSourceStatuses are the local statuses from which a
// terminal remote event may be mirrored — the SQL-side forward-migration
// guard (the WHERE clause is the CAS; the check-then-write gap is closed by
// RowsAffected). Mirrors the Go-side rule: non-terminal rows only, never
// terminal ones. Reopened is transitional (a re-enqueue follows), so it is
// non-terminal and mirrorable like pending/claimed/in_progress.
// taskIngestMirrorableSourceStatuses are the local statuses from which a
// notify mirror may drive a row forward (M4.20-B: reopened dropped —
// retired; legacy reopened rows are read-only history, reissue replaces
// them). Kept as a constant so the ingest UPDATE stays one SQL string.
const taskIngestMirrorableSourceStatuses = `'pending', 'claimed', 'in_progress'`

// ingestPayload is the accepted request body: the notify payload format
// (M3's taskNotifyPayload) plus the sender identity.
type ingestPayload struct {
	Event          string  `json:"event"`
	PreviousStatus string  `json:"previous_status"`
	TaskID         string  `json:"task_id"`
	Status         string  `json:"status"`
	Summary        string  `json:"summary"`
	SessionID      string  `json:"session_id"`
	Model          string  `json:"model"`
	Tags           string  `json:"tags"`
	Source         string  `json:"source"`
	ClaimedBy      string  `json:"claimed_by"`
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	Tokens         int     `json:"tokens"`
	CreatedAt      float64 `json:"created_at"`
	UpdatedAt      float64 `json:"updated_at"`
	Timestamp      string  `json:"timestamp"`
	RemoteNode     string  `json:"remote_node"`
}

// ingestError carries the HTTP status the caller should answer with —
// refusals are policy (404/409/400), not crashes (500).
type ingestError struct {
	status int
	msg    string
}

func (e *ingestError) Error() string { return e.msg }

// TaskIngest applies one remote transition notification to the local
// ledger. Returns a human-readable result line. Refusals come back as
// *ingestError so the HTTP layer maps them to status codes.
func TaskIngest(p ingestPayload) (string, error) {
	p.TaskID = strings.TrimSpace(p.TaskID)
	p.Status = strings.TrimSpace(p.Status)
	if p.TaskID == "" {
		return "", &ingestError{400, "ingest: task_id must not be empty"}
	}
	if p.Status == "" {
		p.Status = p.Event // the notify payload's alias field
	}
	if p.Status == "" {
		return "", &ingestError{400, "ingest: status/event must not be empty"}
	}
	if p.RemoteNode == "" {
		p.RemoteNode = "remote"
	}
	// Only terminal states may be mirrored (the remote's lifecycle states
	// are its own business — the local queue state stays authoritative).
	if !terminalTaskStatus(p.Status) {
		return "", &ingestError{400, fmt.Sprintf("ingest: only terminal statuses may be mirrored (completed|failed|cancelled), got %q", p.Status)}
	}
	if !validTaskStatus(p.Status) {
		return "", &ingestError{400, fmt.Sprintf("ingest: invalid status %q", p.Status)}
	}

	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()

	t, err := taskByID(ctx, db, p.TaskID)
	if err != nil {
		return "", fmt.Errorf("task: ingest: %v", err)
	}
	if t == nil {
		return "", &ingestError{404, fmt.Sprintf("ingest: no local task with task_id=%q — the mirror only annotates rows that exist here (anti-forgery)", p.TaskID)}
	}
	// 前向迁移限定 + 幂等: terminal local rows only accept their own status
	// again (redelivery); non-terminal rows accept any terminal status.
	// This read-based pre-check is the fast path only — the authoritative
	// enforcement is the SQL guard in the CAS loop below (check-then-write
	// is a race window; the UPDATE's WHERE clause is not).
	if terminalTaskStatus(t.Status) {
		if t.Status == p.Status {
			return fmt.Sprintf("Task already %s: %s (idempotent redelivery ignored)", t.Status, p.TaskID), nil
		}
		return "", &ingestError{409, fmt.Sprintf("ingest: local row %s is already terminal (%s) — refusing %q (forward-only)", p.TaskID, t.Status, p.Status)}
	}
	if p.Status == t.Status {
		return fmt.Sprintf("Task already %s: %s (idempotent redelivery ignored)", t.Status, p.TaskID), nil
	}

	fromStatus := t.Status
	detail, tags, endedAt := ingestMirrorFields(t, p)
	now := float64(time.Now().Unix())

	// CAS mirror write: `AND status IN (non-terminal set)` makes the
	// forward-migration rule part of the UPDATE itself — a local writer
	// (TaskNext claim / TaskUpdate / another ingest) that wins the row in
	// the check→write gap turns this into a 0-row no-op instead of a blind
	// overwrite (same pattern as the TaskNext claim: UPDATE ... WHERE
	// status='pending', exactly one winner). RowsAffected=0 → re-read and
	// re-apply the policy explicitly: conflict reported with the row's
	// current state — never a silent clobber.
	// bug056: the UPDATE touches status/tags/ended_at ONLY — the summary
	// column is immutable; the mirror's run stats land in task_events.
	for attempt := 0; ; attempt++ {
		if taskIngestRaceHook != nil {
			taskIngestRaceHook()
		}
		res, err := db.ExecContext(ctx,
			`UPDATE tasks SET status = ?, tags = ?, ended_at = ?, updated_at = ?
			 WHERE task_id = ? AND status IN (`+taskIngestMirrorableSourceStatuses+`)`,
			p.Status, tags, endedAt, now, p.TaskID)
		if err != nil {
			return "", fmt.Errorf("task: ingest: %v", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return "", fmt.Errorf("task: ingest: %v", err)
		}
		if n == 1 {
			// Won the guarded write. fromStatus is the state our policy
			// checked this attempt against; if a writer advanced the row
			// non-terminally during the statement itself (microseconds),
			// the write still matched the allowed set — the claim columns
			// are untouched by this UPDATE either way.
			if detail != "" {
				taskEventWrite(ctx, db, p.TaskID, p.Status, detail)
			}
			return fmt.Sprintf("Task mirrored: %s %s→%s (node %s, tags=%s)",
				p.TaskID, fromStatus, p.Status, p.RemoteNode, tags), nil
		}
		cur, err := taskByID(ctx, db, p.TaskID)
		if err != nil {
			return "", fmt.Errorf("task: ingest: re-read after CAS loss: %v", err)
		}
		if cur == nil {
			return "", &ingestError{404, fmt.Sprintf("ingest: no local task with task_id=%q — the mirror only annotates rows that exist here (anti-forgery)", p.TaskID)}
		}
		if terminalTaskStatus(cur.Status) {
			if cur.Status == p.Status {
				return fmt.Sprintf("Task already %s: %s (idempotent redelivery ignored)", cur.Status, p.TaskID), nil
			}
			return "", &ingestError{409, fmt.Sprintf(
				"ingest: conflict: local row %s moved to %s while mirroring %q — terminal (forward-only), not mirrored; re-deliver only if this event is still current",
				p.TaskID, cur.Status, p.Status)}
		}
		if attempt >= taskIngestSQLRaceRetries {
			return "", &ingestError{409, fmt.Sprintf(
				"ingest: conflict: local row %s kept moving under the mirror write (now %s, non-terminal) — not mirrored after %d CAS attempts; re-deliver",
				p.TaskID, cur.Status, attempt+1)}
		}
		// Still non-terminal — the local claim/run advanced the row inside
		// the window (e.g. pending→claimed); mirroring stays legal. Refresh
		// the snapshot fields and retry the guarded write.
		fromStatus = cur.Status
		detail, tags, endedAt = ingestMirrorFields(cur, p)
	}
}

// ingestMirrorFields computes the row fields the mirror write overwrites
// beyond status: the run-stats result detail (recorded as a task_events
// row — bug056, the summary stays immutable; the old code appended this to
// the summary with a "(from <node>)" dedup guard), the provenance tag
// (source column untouched, sender goes into tags), and the end timestamp.
// Re-run per CAS retry so a refresh never duplicates the stats against the
// freshest row.
func ingestMirrorFields(t *Task, p ingestPayload) (detail, tags string, endedAt float64) {
	detail = ""
	if (p.ElapsedSeconds > 0 || p.Tokens > 0) && !taskEventHasDetail(t.TaskID, "(from "+p.RemoteNode+")") {
		detail = fmt.Sprintf(taskIngestStatsFormat, p.ElapsedSeconds, p.Tokens, p.RemoteNode)
	}
	tags = normalizeTaskTags(t.Tags)
	remoteTag := "remote:" + p.RemoteNode
	if !tagListHas(tags, remoteTag) {
		if tags == "" {
			tags = remoteTag
		} else {
			tags += "," + remoteTag
		}
	}
	endedAt = float64(time.Now().Unix())
	if p.UpdatedAt > 0 && p.UpdatedAt <= endedAt {
		endedAt = p.UpdatedAt // the remote's own end time when plausible
	}
	return detail, tags, endedAt
}

// tagListHas reports whether the normalized comma tag string contains tag.
func tagListHas(tags, tag string) bool {
	for _, tg := range strings.Split(tags, ",") {
		if strings.TrimSpace(tg) == tag {
			return true
		}
	}
	return false
}

// TaskIngestHTTPHandler is the /task-ingest endpoint: POST a notify payload
// (JSON), get the mirror result. Refusal policies map to status codes:
// 400 bad payload/status, 404 unknown task (anti-forgery), 409 forward-only
// violation. Successes — including idempotent redeliveries — are 200.
func TaskIngestHTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "ingest: POST only", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, taskIngestBodyLimit))
		if err != nil {
			http.Error(w, "ingest: read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		var p ingestPayload
		if err := json.Unmarshal(body, &p); err != nil {
			http.Error(w, "ingest: decode JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		out, err := TaskIngest(p)
		if err != nil {
			if ie, ok := err.(*ingestError); ok {
				http.Error(w, ie.msg, ie.status)
				return
			}
			http.Error(w, "ingest: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, out)
	})
}
