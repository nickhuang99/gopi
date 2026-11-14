// Package tools — memory_sync_delta.go: the M4.13 timestamp-delta sync
// protocol (devdoc/memsync-bidirectional.md, which carries the M4.13 delta
// semantics and points at this file). M4.8's export→JSON→import
// stays the transport encoding (SyncTaskExport/SyncTaskImport untouched);
// this file adds the PROTOCOL layer on top:
//
//	core idea — updated_at is the version anchor. Every write touches it
//	(M4.5 invariant), so "what changed since last sync" = rows with
//	updated_at > the peer's stored anchor (bounded delta, no batch files).
//
//	  export-since  — SELECT * WHERE updated_at > anchor - margin, ALL
//	                  statuses verbatim (v2 rows carry updated_at; the
//	                  merge decides), ordered oldest-first.
//	  import-merge  — per row: unknown task_id → verbatim INSERT with the
//	                  from-<peer> provenance tag (history mirrors too: a
//	                  terminal row for an unseen id is inserted as
//	                  terminal — never re-queued, never re-run); known id
//	                  → newer updated_at wins, but ONLY forward: status
//	                  migrates pending → live-mirror → terminal
//	                  (local live execution with a claimant is never
//	                  clobbered — the M4.8 hard rule); equal updated_at →
//	                  identical row = idempotent no-op, differing row =
//	                  conflict (both sides keep their row, 'conflict'
//	                  tag + tags union ask for a human); older → skip.
//	  anchor        — sync_state(peer, anchor, updated_at) — M4.12 harvest's
//	                  table shape, adopted verbatim (one schema, one upgrade
//	                  path). anchor = the peer-domain cutoff, written ONLY
//	                  after a session's application succeeded;
//	                  failure → anchor stays → next session re-pulls the
//	                  same window (idempotent: re-applying rows no-ops).
//
//	Atomicity note (epic ⑥ "单事务" read pragmatically): application is
//	per-row idempotent rather than one BEGIN..COMMIT — a crash mid-merge
//	leaves applied rows no-op on retry and unapplied rows applied, which
//	IS the epic's testable contract (实测 ②: kill mid-sync → rerun
//	idempotent) without rewriting the M4.8 write path.
//
//	防回流 (epic ⑦): rows carry from-<node> tags; re-delivery to the
//	source node hits same-id + equal-updated_at → no-op. No content
//	re-import happens because the merge never rewrites summary/payload.
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/tasksource"
)

// syncParentColumnExpr is the ONE spelling of "read a row's dependency edge
// into the wire row" (Rule 14): the delta export, the harvest SELECT and the
// row leg's one-row SELECT all carry the same column under the same
// COALESCE, so three transports cannot disagree about a row's parent.
//
// COALESCE is the SHIPPED compat channel of this field, not a convenience.
// harvest and the row leg send this expression to a PEER's sqlite3, and a
// peer running a build from before this column existed has no
// parent_task_id — an uncoalesced column there is a "no such column" error
// that takes the WHOLE transport down, which would break every sync with an
// older node to fix one field. A reader that cannot read the column reads
// ” (the same value pre-fix rows carry), and the import's dangling-parent
// refusal is what turns that absence into a loud, named refusal rather than
// a silently unparented row.
const syncParentColumnExpr = `COALESCE(parent_task_id,'')`

// syncParentColumnAlias is the WIRE FIELD the expression above must arrive
// under — the json tag both wire structs (harvestRemoteRow, SyncTaskRow) bind
// to. It lives here because it is the same shared value as the expression, read
// through a different channel (Rule 1b: a wire key is an identifier, so its
// spelling is a named constant, never a bare literal at the point of use).
const syncParentColumnAlias = "parent_task_id"

// syncColumnAliasKeyword is the ONE spelling of SQL's alias introducer — the
// token that makes a peer's `sqlite3 -json` key an expression column by the WIRE
// FIELD instead of by its source text (Rule 1b/13: the shared projection, the
// edge expression and the gate that reads both must agree on this token, so it
// is a name rather than three bare literals).
const syncColumnAliasKeyword = ` AS `

// syncParentColumnNamed is the expression AS the wire field — the spelling
// every `sqlite3 -json` transport must send, because that channel names an
// output column by its SOURCE TEXT (so a bare expression arrives under the key
// "COALESCE(parent_task_id,”)", which encoding/json cannot bind to the struct
// tag: its match is case-insensitive, but punctuation is not skipped). The
// alias is part of the COLUMN here, next to the expression it names, so a
// transport cannot take the expression without its name.
const syncParentColumnNamed = syncParentColumnExpr + syncColumnAliasKeyword + syncParentColumnAlias

// syncParentState names the FOUR arms of one judgement — can this ledger
// honour the dependency edge the row carries? It is ONE function because the
// claim funnel, the two import lanes and the sender's own read all ask the
// same question, and a second spelling would be a second policy (Rule 14).
type syncParentState string

const (
	// syncParentOK: the edge resolves here (absent, or a parent whose status
	// releases its children) — or the row carries no edge at all.
	syncParentOK syncParentState = ""
	// syncParentDangling: this ledger holds no row under the parent id. The
	// arm that reproduces the defect the parent column was added to fix.
	syncParentDangling syncParentState = "dangling"
	// syncParentTerminal: the parent exists with a verdict that does NOT
	// release (failed/cancelled). Rule 33 parks the child forever on it.
	syncParentTerminal syncParentState = "terminal"
	// syncParentUnreadable: the ledger could not be read, so the edge cannot
	// be judged. Denied, never assumed satisfied.
	syncParentUnreadable syncParentState = "unreadable"
)

// syncParentVerdict is THE judgement (Rule 14): every import arm that
// materialises a row with a parent asks THIS function, so no lane can silently
// drop an edge the others keep. It answers the arm plus the sentence that says
// why, in the receiving ledger's own terms.
//
// The arms, and why each one is what it is:
//
//   - syncParentDangling: the receiving ledger holds no row under the parent
//     id. Rule 33 parks a pending row whose parent names no row FOREVER — a
//     cancelled or failed parent never releases it, there is no override, and
//     the row can be repaired only by ledger surgery or by the edge arriving
//     later. Writing such a row is therefore writing a permanently
//     unclaimable row. An import refuses it BY NAME at the moment it would
//     land — but the arm is also the one a MIGRATING PATCH can cure, so the
//     import SPOOLS these rows and re-asks once the patch's own parents have
//     been settled (a five-link chain shipped as one patch lands in order,
//     not as five refusals).
//   - syncParentTerminal: the row exists and its verdict is `cancelled` or
//     `failed`. Rule 33's release set is exactly {completed, verified,
//     superseded}, so shipping the edge would park the child forever too —
//     and the honest ledger already carries a successor row that does
//     (memory #8354: a failed/cancelled parent is repaired by REISSUE). This
//     arm is NOT healable by waiting: no sibling of this patch changes it.
//   - syncParentUnreadable: the lookup itself failed. An edge that cannot be
//     judged is never assumed satisfied — and, unlike absence, it is not
//     retried, because the retry asks the same broken ledger.
//
// exists is a lookup in the receiving ledger (nil = the caller cannot read it,
// which is taskParentGateDenied's nil-index rule, spelled once here).
func syncParentVerdict(parentID string, exists func(string) (string, bool)) (syncParentState, string) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return syncParentOK, ""
	}
	if exists == nil {
		return syncParentUnreadable, fmt.Sprintf(syncParentUnreadableFormat, parentID, taskParentReleaseSetLabel())
	}
	status, ok := exists(parentID)
	if !ok {
		return syncParentDangling, fmt.Sprintf(syncParentDanglingFormat, parentID, taskParentReleaseSetLabel())
	}
	if taskParentReleaseStatuses[status] {
		return syncParentOK, ""
	}
	if taskParentTerminalUnreleased[status] {
		return syncParentTerminal, fmt.Sprintf(syncParentTerminalFormat, parentID, status, taskParentReleaseSetLabel())
	}
	// Live (pending/claimed/in_progress): the dependency is real and unmet,
	// which is exactly the state Rule 33 models for this ledger. The edge
	// travels and the dispatcher parks the row — on the PARENT'S OWN VERDICT,
	// not on the sender's guess about it.
	return syncParentOK, ""
}

// syncParentUnresolvable is syncParentVerdict's boolean face for the callers
// that only need the sentence (the harvest lanes and the mirror insert).
func syncParentUnresolvable(parentID string, exists func(string) (string, bool)) (string, bool) {
	v, reason := syncParentVerdict(parentID, exists)
	return reason, v != syncParentOK
}

// The refusal sentences of syncParentVerdict — one spelling each (Rule 1b).
// They are rendered in the RECEIVING ledger's terms because that is where the
// judgement is made, and three lanes (the delta import, the harvest window and
// the mirror insert) print the very same words for the very same condition.
const (
	syncParentDanglingFormat   = "parent_task_id %q is not in this ledger — a pending row whose parent names no row is parked permanently (Rule 33 releases only on %s), so the edge is refused rather than written as an unclaimable row: send the parent to this node first, then this row"
	syncParentTerminalFormat   = "parent_task_id %q is %s here — that verdict does not release a child (Rule 33 releases only on %s), so writing the edge would park this row forever; the repair is a REISSUE of the parent (its successor row is the one that carries the work), not a copy of its child"
	syncParentUnreadableFormat = "parent_task_id %q cannot be resolved: this ledger's rows could not be read, and an unresolvable dependency is never assumed satisfied (Rule 33 releases only on %s)"
)

// taskParentTerminalUnreleased is the complement of the release set WITHIN the
// terminal statuses: the verdicts that end a row's life without releasing its
// children. It is a literal pair — NOT computed — because it must stay a strict
// subset of taskParentReleaseStatuses' complement, and a test checks the two
// against each other so a new status added to the release set must decide its
// side here deliberately.
//
// THE REWORK ARM IS DELIBERATELY ABSENT HERE. A shipped edge whose CHILD carries
// tasktags.ReworkLineageTag is released by a `failed`/`cancelled` parent on the
// receiving ledger (taskParentGateDenied's terminal-verdict criterion), so
// refusing to ship it would strand exactly the successor the reissue needs. This
// map is consulted for every child alike because the SENDING side does not read
// the child's tags on this path; the arm it describes is not an unresolvable
// edge — it is the shape the rework arm exists to allow.
var taskParentTerminalUnreleased = map[string]bool{
	taskStatusFailed:    true,
	taskStatusCancelled: true,
}

// syncParentStatusOn is the receiving-ledger lookup syncParentUnresolvable
// takes as its evidence: one row's status by id, on an already-open handle.
// It answers (status, true) when the row EXISTS — including a status this
// build does not know, which is a fact to report, never an absence — and
// ("", false) when the ledger holds no such row. A read ERROR is surfaced
// through the same channel as absence is NOT: the caller (the import) has an
// error return of its own, and the two must not be conflated (Rule 15b: an
// unreadable lookup and a missing row are different facts).
func syncParentStatusOn(ctx context.Context, db *sql.DB) func(string) (string, bool) {
	return func(parentID string) (string, bool) {
		var status string
		err := db.QueryRowContext(ctx,
			`SELECT status FROM tasks WHERE task_id = ?`, parentID).Scan(&status)
		if err != nil {
			// sql.ErrNoRows is the absence arm; every other error leaves the
			// status empty, which syncParentUnresolvable's release-set check
			// then refuses by name (an unread status is never "released").
			return "", false
		}
		return status, true
	}
}

// syncRowParentStatusOn is the same lookup on a *sql.Tx — the harvest lane
// works inside a transaction, and the judgement must be made against the same
// connection that will write the row.
func syncRowParentStatusOn(ctx context.Context, tx *sql.Tx) func(string) (string, bool) {
	return func(parentID string) (string, bool) {
		var status string
		if err := tx.QueryRowContext(ctx,
			`SELECT status FROM tasks WHERE task_id = ?`, parentID).Scan(&status); err != nil {
			return "", false
		}
		return status, true
	}
}

const syncDeltaSelectColumns = `task_id, summary, COALESCE(payload,''), COALESCE(tags,''),
	COALESCE(source,''), created_at, updated_at, status, ended_at, COALESCE(deliverable,''),
	COALESCE(run_method,''), COALESCE(run_cmd,''), COALESCE(reason,''),
	` + syncParentColumnExpr + `,` + syncParentIDsColumnExpr

// SyncTaskDeltaOptions controls the v2 delta export.
type SyncTaskDeltaOptions struct {
	// Peer is the node this export is destined for (M4.24 anti-reflow
	// correction): rows the PEER sent us (from-<peer>) are excluded —
	// relaying them back is the reflow. Self-tagged rows are NOT excluded
	// (local-born rows must reach the peer; homecoming verdicts are
	// absorbed idempotently on import).
	Peer string
	// DBPath overrides the ledger ("" = default). Tests and CLI --db.
	DBPath string
	// AfterSource is the version anchor in THIS node's row domain: only
	// rows with updated_at > AfterSource - syncAnchorMarginBackdate travel
	// (the margin re-sends a skew window; application is idempotent).
	AfterSource float64
	// SkipReflowGate disables the anti-reflow exclusion (rows the PEER
	// sent us — from-<peer> — are normally filtered). Used ONLY by the
	// claim-lock channel (exportClaimPatch): a claim patch is a lock
	// verb aimed at the row's origin and MUST carry the target row even
	// when tagged from-<peer> (the reflow gate would silently empty the
	// acquire patch → "not found" → yield into a stuck zombie). The
	// peer's claim-remote handler owns the CAS/refusal decision; the
	// merge is idempotent, so carrying the row is safe.
	SkipReflowGate bool
	// Tags restricts the delta to rows carrying this tag (epic ③ 模块分区
	// 选段导出: PA tags rows node:<role>+module:<area>; each node syncs
	// only its partition → the two nodes' write sets touch the same rows
	// far less often). Same tag gate as the M4.8 export: exact tag,
	// comma-bounded (no substring hits). Composes with the anchor window:
	// both gates AND together. The peer's session driver passes it through
	// verbatim; an absent tag = the whole window (session default).
	Tags string
}

// syncSelfTag returns THIS node's provenance tag (from-<hostname>, the
// GOPI_SYNC_NODE override respected — the export's node-identity rule).
// Used by the anti-reflow export gate: rows carrying it were imported FROM
// a peer (or echo back our own pushed rows) and never re-export.
func syncSelfTag() string {
	self := os.Getenv("GOPI_SYNC_NODE")
	if self == "" {
		self, _ = os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
	}
	return SyncTaskTagPrefix + self
}

// syncTagTokenPattern renders the SQL pattern argument for
// syncTagTokenMatchSQL: the token wrapped in the separator it is anchored
// on, GLOB-star-extended on both ends.
func syncTagTokenPattern(token string) string {
	return "*," + token + ",*"
}

// syncTagTokenMatchSQL is THE single definition of "this ledger row carries
// the tag token X" in SQL — the one place the SQL side's case policy lives
// (Rule 14). Every tag gate (the M4.8 export partition filter, the M4.24
// anti-reflow exclusion, the v2 delta partition filter, the v2 delta
// anti-reflow exclusion) splices this fragment and passes
// syncTagTokenPattern(token) as its argument.
//
// CASE POLICY (decided 2026-09-12 from the live ledger + the Go-reader
// census — devdoc/sync-tag-case-policy.md): tag tokens are CASE-SENSITIVE
// persisted values. GLOB, not LIKE: SQLite's LIKE folds ASCII case, while
// EVERY Go consumer of these tokens is case-sensitive — hasTag
// (t == want), syncDeltaMergeTags (strings.HasPrefix(t, SyncTaskTagPrefix)),
// syncGateTag's own from-<node> comparison, and the task-tag predicates in
// tasks.go. Under LIKE a row tagged "BUG" (or "FROM-PEERBOX") passed the sync
// gate yet was invisible to the token readers, and — the mirror direction,
// the dangerous one — a wrong-case from-<peer> tag ESCAPED the anti-reflow
// exclusion and reflowed the peer's row back at it. The persisted ledger
// agrees with the readers: the only mixed-case tokens in ~/.gopi/memory.db
// are real node NAMES (from-host-b, worker:host-b) — the
// hostname of the machine, not a keyword — so folding case would silently
// rewrite a row's provenance (from-hp ≠ from-HP: different nodes).
//
// Boundary mode: comma-anchored, so the token is matched WHOLE — 'bug' never
// hits 'debug-ui'/'bugflow'/'bug033', exactly like hasTag's exact compare.
// Raw tags have no surrounding whitespace (import writes normalizeTaskTags
// output and the live ledger confirms 0/1043 spaced rows), so the boundary
// needs no space-strip; do NOT add one to "be safe" — it would make the SQL
// side disagree with splitSources' TrimSpace (it would match 'bug' inside
// 'x, bug ,y', which splitSources reads as ' bug ' ≠ 'bug').
//
// GLOB's * / ? / [ metacharacters are NOT escaped here. That is exact rather
// than lossy for this population: splitSources hashes tag tokens raw into
// from-<node>, so a literal '*' in a node name is already indistinguishable
// at the reader — no consumer could tell a folded match apart from the raw
// value. The live ledger carries 0 metacharacter tags; a synthetic GLOB-bait
// fixture is the one place the difference could show, so the parity test
// excludes it deliberately (see TestSyncTagTokenMatch_SQLGoParity).
//
// The returned string contains a `?` placeholder — callers MUST append the
// matching pattern argument.
func syncTagTokenMatchSQL(column string) string {
	return "(',' || COALESCE(" + column + ",'') || ',') GLOB ?"
}

// syncGateTag normalizes an export gate's peer reference into the raw node
// NAME the gate's tag pattern wraps (the pattern adds the from- prefix):
//
//	"sync-task"     → "sync-task"   (peer named; gate excludes from-<peer>)
//	""              → THIS node's name (GOPI_SYNC_NODE || hostname) — the
//	                  legacy self-gate for peer-unknown callers.
//
// Regression guard (fix/reflow-gate-contract): eef8342 set peerTag to the
// FULL syncSelfTag() (already "from-<node>") while the pattern added
// another "from-" prefix — the pattern searched "from-from-<node>" and
// NEVER matched, silently disarming the zero-arg self-gate in both
// exporters. This helper takes the raw NAME only, so the double prefix
// cannot reappear (tests: TestReflowGateTagNormalization).
func syncGateTag(peer string) string {
	peer = strings.TrimSpace(peer)
	if peer != "" {
		return peer
	}
	self := os.Getenv("GOPI_SYNC_NODE")
	if self == "" {
		self, _ = os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
	}
	return self
}

// SyncTaskExportDelta selects this node's changed rows as a v2 patch — the
// pull side of a session. Every status travels verbatim: the merge on the
// receiving side owns the forward-only decision, the exporter stays dumb
// (one SELECT, no policy).
func SyncTaskExportDelta(opts SyncTaskDeltaOptions) (*SyncTaskPatch, *SyncTaskExportStats, error) {
	dbPath := opts.DBPath
	if dbPath == "" {
		dbPath = memoryDBPath()
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("memsync export-since: %w", err)
	}
	defer db.Close()

	cutoff := opts.AfterSource - syncAnchorMarginBackdate.Seconds()
	query := `SELECT ` + syncDeltaSelectColumns + ` FROM tasks
	          WHERE updated_at > ?`
	var args []any
	args = append(args, cutoff)
	if tag := strings.TrimSpace(opts.Tags); tag != "" {
		// The M4.8 export's tag gate, verbatim — the ONE shared fragment:
		// a comma-anchored, CASE-SENSITIVE exact-tag partition (see
		// syncTagTokenMatchSQL's case-policy note), never a substring.
		query += " AND " + syncTagTokenMatchSQL(syncTagColumn)
		args = append(args, syncTagTokenPattern(tag))
	}
	// Anti-reflow gate (M4.24 correction — mirrors the M4.8 exporter's
	// fix): exclude rows the PEER sent us (from-<peer> — relaying them
	// back is the reflow), NOT rows tagged from-<self>. Self-tagged rows
	// are local-born (their first export is the whole point) — the old
	// syncSelfTag() gate silently dropped them (the 27-row class).
	// syncGateTag returns the raw node NAME ("" = this node — legacy
	// self-gate); the shared tag-pattern below owns the from- prefix once.
	gateTag := syncGateTag(opts.Peer)
	// M4.20-E homecoming exemption (restored on the v2 channel by the
	// reflow-gate contract fix): a TERMINAL row travels even when tagged
	// from-<gate>. The from-tag marks channel provenance, not authorship —
	// the completed/failed verdict on a row the peer once sent us is THIS
	// node's own write (its executor judged it) and must come home, or
	// the origin never learns its task finished (TestBidirEpic4IdempotentRoundTrip).
	// Live rows (pending/claimed/in_progress) keep the strict exclusion —
	// those are the mirror faces the gate exists for (the echo loop).
	// SkipReflowGate (claim-lock channel only) removes the exclusion
	// entirely: the claim acquire verb must reach the origin's copy of a
	// from-<peer> row (see exportClaimPatch's comment for the incident).
	if !opts.SkipReflowGate {
		query += ` AND NOT (status IN (?, ?, ?) AND ` + syncTagTokenMatchSQL(syncTagColumn) + `)`
		args = append(args, taskStatusPending, taskStatusClaimed, taskStatusInProgress)
		args = append(args, syncTagTokenPattern(SyncTaskTagPrefix+gateTag))
	}
	query += ` ORDER BY updated_at ASC, id ASC`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("memsync export-since: %w", err)
	}
	defer rows.Close()

	// Node identity: GOPI_SYNC_NODE overrides the hostname — a same-host
	// two-node simulation (GOPI_HOME isolation) needs distinct node names
	// for provenance (from-<node>) and the anti-reflow echo rule to work.
	hostname := os.Getenv("GOPI_SYNC_NODE")
	if hostname == "" {
		hostname, _ = os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
	}
	patch := &SyncTaskPatch{Version: syncPatchVersionDelta, Node: hostname, ExportedAt: time.Now().Unix()}
	stats := &SyncTaskExportStats{}
	for rows.Next() {
		var r SyncTaskRow
		var tags, source, status string
		var createdAt, updatedAt float64
		var endedAt sql.NullFloat64
		var deliverable string
		if err := rows.Scan(&r.TaskID, &r.Summary, &r.Payload, &tags, &source,
			&createdAt, &updatedAt, &status, &endedAt, &deliverable, &r.RunMethod, &r.RunCmd, &r.Reason,
			&r.ParentTaskID, &r.ParentTaskIDs); err != nil {
			return nil, nil, fmt.Errorf("memsync export-since: scan: %w", err)
		}
		if strings.TrimSpace(deliverable) != "" {
			r.Deliverable = deliverable
			r.DeliverableVersion = deliverableVersionKnown
		}
		r.CreatedAt = int64(createdAt)
		r.UpdatedAt = int64(updatedAt)
		r.Status = status // v2: verbatim — the merge decides, not the exporter
		r.Tags = splitSources(tags)
		r.Source = source
		if endedAt.Valid {
			r.EndedAt = int64(endedAt.Float64)
		}
		stats.Scanned++
		patch.Rows = append(patch.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("memsync export-since: %w", err)
	}
	if patch.Rows == nil {
		patch.Rows = []SyncTaskRow{}
	}
	return patch, stats, nil
}

// syncDeltaMergeTags unions the existing tags with the remote's OWN tags —
// from-* channel tags and 'conflict' never propagate through a merge (rule
// ⑦: provenance describes the channel that delivered the row, not the row;
// a conflict flag is local state). Set semantics: no duplicates, order
// normalized (the existing tags keep their order, new ones append).
func syncDeltaMergeTags(existing string, remote []string) string {
	local := splitSources(existing)
	seen := map[string]bool{}
	for _, t := range local {
		seen[t] = true
	}
	merged := local
	for _, t := range remote {
		if seen[t] || strings.HasPrefix(t, SyncTaskTagPrefix) || t == syncConflictTag {
			continue
		}
		seen[t] = true
		merged = append(merged, t)
	}
	return strings.Join(merged, ",")
}

// hasTag reports whether a comma-separated tag string carries the tag
// (production helper — the merge's conflict-echo check uses it; the harvest
// tests share it).
// hasTag reports whether the comma-canonical tag string carries want.
func hasTag(tags, want string) bool {
	for _, t := range splitSources(tags) {
		if t == want {
			return true
		}
	}
	return false
}

// WorkerTaskTagPrefix is the worker-provenance tag prefix (M4.20 ①): a row
// a WORKER LOOP processed carries `worker:<hostname>`, so sync adjudication
// and ops can tell worker-executed rows from dispatcher rows. The tag rides
// the row's own tags (never re-exported — the anti-reflow gate only strips
// from-<node> tags, so the worker tag travels with the row and documents
// the executor on both nodes).
const WorkerTaskTagPrefix = "worker:"

// WorkerTaskTag is this host's worker-provenance tag (`worker:<hostname>`).
// Exported for the cmd/agent worker loop's writeback path.
func WorkerTaskTag() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = syncUnknownHost
	}
	return WorkerTaskTagPrefix + host
}

// TagWorkerClaim unions tags with the worker-provenance tag (idempotent:
// an existing tag stays, the union is comma-canonical via normalizeTaskTags
// semantics). The writeback calls this when the WORKER loop executes the
// task — a plain `task run` leaves tags untouched.
func TagWorkerClaim(tags string) string {
	if hasTag(tags, WorkerTaskTag()) {
		return tags
	}
	if strings.TrimSpace(tags) == "" {
		return WorkerTaskTag()
	}
	return tags + "," + WorkerTaskTag()
}

// TaskRunWritebackTagged is TaskRunWriteback for the WORKER loop: same
// terminal writeback, plus the worker-provenance tag unioned into the row's
// tags (M4.20 ① — traceable executor). Additive: the dispatcher's
// TaskRunWriteback stays tag-free (its rows carry the submitter's tags).
//
// This function must NOT consult TaskWrapperEnv: that var is exported only to a
// `task run` WRAPPER's CHILD (childenv.go) — it names the PARENT row and is
// visible in a serve daemon only when the daemon itself runs inside a task
// child. A daemon's own terminal writes have no wrapper env, so gating here
// would silently drop the tag (the failure a stale bug report inferred from the
// UNRELATED gate at task_deliverable_worktree.go:104). The host half comes from
// the process env (os.Hostname); the worker/plain distinction is carried by the
// CALLER's latch (cli_tasks_worker.go workerWriteback → writebackOutcome).
// Guard: TestWorkerProvenanceTagReachesLedgerWithoutWrapperEnv.
func TaskRunWritebackTagged(taskID, status, resultSummary string) (string, error) {
	tags, err := taskTagsByID(taskID)
	if err != nil {
		return "", err
	}
	merged := TagWorkerClaim(tags)
	return TaskRunWritebackTags(taskID, status, resultSummary, merged)
}

// TaskRunWritebackTags is the tags-settable writeback primitive (one
// implementation of the terminal write; the CLI's TaskRunWriteback passes
// the row's existing tags unchanged). The UPDATE rides the same
// taskUpdate-with-summary gate — the tags column is the only extra SET.
func TaskRunWritebackTags(taskID, status, resultSummary, tags string) (string, error) {
	taskID = taskIDNormalize(taskID)
	out, err := TaskRunWriteback(taskID, status, resultSummary)
	if err != nil {
		return "", err
	}
	if tags == "" {
		return out, nil
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET tags = ? WHERE task_id = ?`, tags, taskID); err != nil {
		return "", fmt.Errorf("task writeback tags: %w", err)
	}
	return out, nil
}

// taskTagsByID loads a row's tags ("" when the row is gone — the writeback
// error surfaces separately; the tag union never blocks the terminal write).
func taskTagsByID(taskID string) (string, error) {
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
	var tags any
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(tags, '') FROM tasks WHERE task_id = ?`, taskID).Scan(&tags); err != nil {
		return "", fmt.Errorf("task tags: %w", err)
	}
	s, _ := tags.(string)
	return s, nil
}

// syncConflictTag marks an equal-updated_at differing row (epic ③: 双侧保留
// + 标记 conflict 求人工). Both sides add it symmetrically on the same
// collision, so either node's operator sees it.
const syncConflictTag = "conflict"

// syncTagSyncResolved stamps a row the SERVER ruled on: an equal-updated_at
// both-sides-wrote conflict resolves as completed (枚举不值得费思量 — the
// deliverable lives in the git branch, not the status marker).
const syncTagSyncResolved = "sync-resolved"

// syncUnknownSummary is the placeholder a mirrored terminal row carries when
// the wire's summary is empty (the row is history either way, and the ledger
// refuses an empty summary outright).
const syncUnknownSummary = "(mirrored terminal row — summary empty on the wire)"

// syncWireParentVerdict judges a WIRE row's edge against THIS ledger. A ledger
// read that fails is reported as its own error — never as "the parent is
// missing" (Rule 15b: an unreadable lookup and an absent row are different
// facts, and the second one is what a chain migration is made of).
func syncWireParentVerdict(ctx context.Context, db *sql.DB, r SyncTaskRow) (syncParentState, string, error) {
	// EVERY parent, through the ONE reader: a row is honoured only when the
	// whole set is (Rule 33 — the gate is an AND, so a set that arrives with
	// one unresolvable member must be refused whole rather than written with
	// the resolvable half).
	parents := taskParentIDsOf(r.ParentTaskID, r.ParentTaskIDs)
	if len(parents) == 0 {
		return syncParentOK, "", nil
	}
	worst := syncParentOK
	var worstReason string
	idx := syncParentStatusOn(ctx, db)
	for _, parent := range parents {
		v, reason := syncParentVerdict(parent, idx)
		if v != syncParentOK && worst == syncParentOK {
			worst, worstReason = v, reason
		}
	}
	return worst, worstReason, nil
}

// SyncTaskImportDelta applies a v2 patch through the merge. patchPath is
// read as a file; the CLI's ssh-stdout staging writes this same shape.
func SyncTaskImportDelta(patch *SyncTaskPatch) (*SyncTaskImportStats, error) {
	if patch == nil {
		return nil, fmt.Errorf("memsync import-merge: nil patch")
	}
	if err := syncCheckPatchVersion("import-merge", patch.Version, syncPatchVersionDelta); err != nil {
		return nil, err
	}
	node := patch.Node
	if node == "" {
		node = syncTaskUnknownNode
	}
	fromTag := SyncTaskTagPrefix + node

	stats := &SyncTaskImportStats{}
	db, closeDB, err := taskSyncDB()
	if err != nil {
		return nil, err
	}
	defer closeDB()
	ctx := context.Background()
	// Notify bus (task-notify-terminal-gap): every row this import advances
	// is observed here and delivered once the whole patch is applied — the
	// batch's moves are one operator-visible episode, not N.
	bus := &taskNotifyBus{}
	defer taskNotifyFlushDeferred(bus)()

	// ONE settlement loop, run TWICE at most: the second pass is the SPOOL
	// (see syncImportRow / syncParentVerdict). The rows that wait for a parent
	// this same patch carries are exactly the rows a chain migration sends,
	// and refusing them on the first pass would turn a correct five-link
	// migration into five refusals.
	//
	// The LAST pass is the verdict: a row still dangling after the patch has
	// settled is a row whose parent this ledger does not hold, and staying
	// silent about it is exactly the failure this epic repairs — a row that
	// never lands and nobody is told why.
	settle := func(rows []SyncTaskRow, last bool) []SyncTaskRow {
		spooled := make([]SyncTaskRow, 0)
		for i, r := range rows {
			waiting, err := syncImportRow(ctx, db, bus, stats, i, r, fromTag, node, last)
			if err != nil {
				continue
			}
			spooled = append(spooled, waiting...)
		}
		return spooled
	}
	spooled := settle(patch.Rows, false)
	if len(spooled) > 0 {
		// Second pass: the patch's own parents have now settled in ORDER, so a
		// child whose edge pointed at a sibling of this very patch resolves
		// here. Whatever is still waiting is refused by name on this pass.
		settle(spooled, true)
	}
	return stats, nil
}

// syncImportRow settles ONE row of an import patch and answers the rows that
// must be re-asked later (the spool), or an error when the row was refused
// (the refusal is already recorded on stats — the error exists so the caller's
// loop reads as "settled or not", not as a second reporting channel).
//
// The EDGE is judged FIRST and against the receiving ledger, because Rule 33
// parks a pending row whose dependency cannot resolve and a parked row is
// unreachable forever (no override, and cancelled/failed parents never
// release). The three arms are syncParentVerdict's; the migration-healable one
// (dangling) SPOOLS the row for the caller's second pass, and a row still
// dangling at the end is refused BY NAME — never written as an unclaimable row
// and never silently shorn of its edge.
func syncImportRow(ctx context.Context, db *sql.DB, bus *taskNotifyBus, stats *SyncTaskImportStats, i int, r SyncTaskRow, fromTag, node string, last bool) ([]SyncTaskRow, error) {
	refuse := func(format string, args ...any) ([]SyncTaskRow, error) {
		stats.RowsRefused++
		stats.Errors = append(stats.Errors, fmt.Sprintf("row %d (%s): ", i, r.TaskID)+fmt.Sprintf(format, args...))
		return nil, importRefusal(format, args...)
	}
	r.TaskID = strings.TrimSpace(r.TaskID)
	r.Summary = strings.TrimSpace(r.Summary)
	if r.TaskID == "" || r.Summary == "" {
		return refuse("empty task_id or summary")
	}
	verdict, reason, perr := syncWireParentVerdict(ctx, db, r)
	if perr != nil {
		return refuse("%v", perr)
	}
	switch verdict {
	case syncParentOK:
	case syncParentDangling:
		if last {
			// The patch has settled and this ledger still does not hold the
			// parent: the edge cannot resolve HERE, so the row is refused by
			// name rather than written as one Rule 33 parks forever.
			return refuse("%s", reason)
		}
		// The parent may be another row of this very patch — spooled, and
		// re-asked once the patch's parents have settled.
		return []SyncTaskRow{r}, nil
	default:
		return refuse("%s", reason)
	}
	existing, err := taskByID(ctx, db, r.TaskID)
	if err != nil {
		return refuse("%v", err)
	}
	if existing == nil {
		if err := syncDeltaInsertRow(ctx, db, r, fromTag); err != nil {
			return refuse("%v", err)
		}
		stats.RowsImported++
		return nil, nil
	}
	if err := syncDeltaMergeRow(ctx, db, bus, existing, r, node, stats); err != nil {
		return refuse("%v", err)
	}
	return nil, nil
}

// syncDeltaInsertRow mirrors a row this node has never seen — verbatim
// status included: a remote terminal row is HISTORY (insert as terminal,
// never re-queued: importing it pending would double-run finished work);
// a remote claimed/in_progress row mirrors as claim-less so the local
// dispatcher (pending-only claimer) cannot double-run it either.
func syncDeltaInsertRow(ctx context.Context, db *sql.DB, r SyncTaskRow, fromTag string) error {
	status := strings.TrimSpace(r.Status)
	if !validTaskStatus(status) {
		status = taskStatusPending // defensive: a mangled row lands pending
	}
	// The EDGE is a fact about the row, and Rule 33 turns an unresolvable one
	// into a permanently parked row — so the edge is either written whole or
	// refused by name (never silently dropped). Asked against THIS ledger,
	// which is the one that will dispatch the row.
	for _, parentID := range taskParentIDsOf(r.ParentTaskID, r.ParentTaskIDs) {
		if reason, bad := syncParentUnresolvable(parentID, syncParentStatusOn(ctx, db)); bad {
			return importRefusal("%s", reason)
		}
	}
	var endedAt any
	if terminalTaskStatus(status) {
		if r.EndedAt > 0 {
			endedAt = float64(r.EndedAt)
		} else {
			endedAt = float64(r.UpdatedAt) // best known truth
		}
	}
	createdAt := float64(r.CreatedAt)
	if createdAt <= 0 {
		createdAt = float64(r.UpdatedAt)
	}
	updatedAt := float64(r.UpdatedAt)
	if updatedAt <= 0 {
		updatedAt = float64(time.Now().Unix()) // legacy row: anchor it now
	}
	tags := importTaskTags(r.Tags, fromTag)
	// M4.18 db-sync: a verbatim insert carries the peer's deliverable blob
	// (only valid KNOWN-version JSON — a future schema is dropped).
	deliverable := ""
	if deliverableForwardable("", r.Deliverable, r.DeliverableVersion) {
		deliverable = r.Deliverable
	}
	// M4.11: delta inserts derive the priority from tags too (a bug row
	// arriving from a peer must not queue behind feature rows on this node).
	_, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, session_id, model,
		    parent_task_id, `+taskParentIDsColumnName+`, tags, payload, source, claimed_by, claimed_at,
		    verify_evidence, deliverable, priority, run_method, run_cmd, reason,
		    created_at, updated_at, ended_at)
		 VALUES (?, ?, ?, '', '', ?, ?, ?, ?, ?, '', NULL, '', ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.Summary, status, strings.TrimSpace(r.ParentTaskID), strings.TrimSpace(r.ParentTaskIDs), tags, r.Payload, r.Source,
		deliverable, taskTagPriority(tags), r.RunMethod, r.RunCmd, r.Reason, createdAt, updatedAt, endedAt)
	if err != nil {
		return fmt.Errorf("delta insert: %w", err)
	}
	return nil
}

// syncDeltaMergeRow applies the newer-wins forward-only rules onto a known
// row. Content (summary/payload) is NEVER rewritten (the nick rule) — the
// merge moves STATUS and unions TAGS, nothing else.
func syncDeltaMergeRow(ctx context.Context, db *sql.DB, bus *taskNotifyBus, existing *Task, r SyncTaskRow, peer string, stats *SyncTaskImportStats) error {
	// M4.20-B: the merge is RANK-first. syncDeltaApplyNewer owns every
	// forward move (remote rank > local) AND the terminal-tier 后到覆盖
	// (equal rank-3 peers, later timestamp wins); anything strictly behind
	// or an idempotent echo is absorbed there. This function now only
	// handles the residue the rank model cannot order: EQUAL non-terminal
	// ranks with differing content — the legacy conflict backstop (since
	// claim-broadcast M4.18 a normal path produces none; it firing is a
	// BUG signal — start-point atomicity failed somewhere).
	// THE WIDER REMOTE SET — the second half of the edge, and the one this leg
	// used to DROP. It is judged HERE, before the rank ladder, because a
	// parent set is a SET fact the ladder does not order: the same status on
	// both nodes returns early as an echo (RowsAlready) and the two new edges
	// would never be seen. The adopt arm in syncDeltaApplyNewer fires only for
	// a row with NO parents at all, so a row carrying P1 whose peer copy
	// carries {P1, P2, P3} left the two new edges on the floor — the child
	// released the moment P1 finished and stayed claimable while two of its
	// three dependencies were live, silently: the drop is on the RECEIVING
	// side, so no error, no counter, and no peer to re-send them.
	if _, refused := syncDeltaWidenParents(ctx, db, existing.TaskID,
		taskParentIDsOf(r.ParentTaskID, r.ParentTaskIDs), stats); refused {
		return nil
	}

	remote := float64(r.UpdatedAt)
	if remote > existing.UpdatedAt ||
		(remote <= existing.UpdatedAt && statusRankOf(strings.TrimSpace(r.Status)) > statusRankOf(existing.Status)) ||
		(statusRankOf(strings.TrimSpace(r.Status)) == statusRankOf(existing.Status) && taskTerminalTier(strings.TrimSpace(r.Status))) {
		return syncDeltaApplyNewer(ctx, db, bus, existing, r, peer, stats)
	}
	if remote < existing.UpdatedAt {
		stats.RowsAlready++ // local strictly newer on the same rung — nothing to do
		return nil
	}
	// remote == local, same-or-lower rank: the anti-reflow echo (rule ⑦)
	// vs a true conflict. Channel metadata (from-<peer>) never unions here
	// — an equal-anchor row is an echo, the peer didn't write it; only the
	// peer's OWN tags (its local tag edits) propagate. Identical STATUS +
	// tags = the pure no-op every re-sync depends on. Differing STATUS at
	// the same anchor = two nodes each advanced the row differently.
	// RULING (nick, 冲突语义定稿): the task status is a mere enum — the
	// real deliverable lives in the git branch (the merge conflict is LLM
	// work, nothing the task marker should agonize over). So the SERVER
	// side simply writes completed (ended_at=now, tags += sync-resolved)
	// and its anchor advances; the CLIENT side keeps the legacy keep-row
	// + 'conflict' tag — the server's completed verdict reaches it through
	// the next sync's pull as normal forward migration (client never
	// rules). A re-run sees terminal-equal rows → pure no-op (idempotent).
	merged := syncDeltaMergeTags(existing.Tags, r.Tags)
	conflictKnown := hasTag(existing.Tags, syncConflictTag)
	// Rank model note (M4.20-B): rows whose statuses DIFFER at an equal
	// anchor were already adjudicated by the rank ladder above (or covered
	// by 后到覆盖). Reaching here with equal status + differing CONTENT is
	// the one residual the rank ladder cannot order — the legacy conflict
	// backstop. The SERVER still rules (枚举不值得费思量: write completed,
	// tags += sync-resolved, anchor advances); the CLIENT keeps the row +
	// 'conflict' tag (the server's verdict arrives via the next pull as
	// forward migration; client never rules). Since claim-broadcast M4.18
	// a normal path produces NONE — this firing is a BUG signal.
	if existing.Status == strings.TrimSpace(r.Status) && IsServerNode() {
		// M4.16 semantic fix (bug053): the blind server ruling — write
		// completed + sync-resolved + advance the anchor — is ONLY legal
		// for source=auto registration/mirror rows (登记噪音: a pending
		// row on peerbox that a HP mirror cancelled, or an auto-registered
		// row whose peer echo differs). A REAL ticket (source human/llm,
		// non-empty payload — the 登记噪音 vs 真工单 distinction) can NEVER
		// be blind-flattened: bug050/051/052 were all fake-closed by
		// sync-resolved rulings while their implementation never existed —
		// the ruling granted a verdict only the real work merited. A real
		// ticket keeps its row (conflict tag union) and raises a
		// ledger-conflict-human-task alarm for a human/PA to adjudicate —
		// never auto-completed.
		if !syncRowIsMirrorNoise(existing) {
			conflictKnown, already, err := syncDeltaTagConflictNoRuling(ctx, db, existing, merged)
			if err != nil {
				return err
			}
			if already {
				stats.RowsAlready++ // known conflict re-echoed — nothing new
				return nil
			}
			stats.RowsConflict++
			notifyLedgerConflictHumanTask(existing.TaskID,
				fmt.Sprintf("server conflict ruling blocked: task is a real %s ticket (payload non-empty) — pending on one host vs mirror-cancelled on its peer must NOT be flattened to completed; adjudicate manually (bug053 M4.16 semantic fix). conflictKnown=%v", existing.Source, conflictKnown))
			return nil
		}
		return syncDeltaServerResolve(ctx, db, bus, existing, merged, stats)
	}
	if existing.Status == r.Status {
		// 内容分歧 = WARNING (nick, 内容冲突降级): same status but DIFFERING
		// summary/payload is two nodes' free-text restatements of the same
		// task_id — the row's IDENTITY is its task_id (UUID), and payload is
		// a briefing's prose, which differs across nodes by NATURE (the same
		// file says so at the export side). It is therefore NOT 两侧都改了 =
		// bug, and it must NOT carry the syncConflictTag ("conflict"): that
		// tag is the MUST-ARBITRATE-BY-A-HUMAN marker, reserved for the
		// status divergence below (and for the server ruling the bug053/M4.16
		// guard blocks). The merge still NEVER overwrites either side's
		// content (nick rule) — it unions the peer's OWN tags and, when that
		// union changes the row, records one RowsTagged; an unchanged union
		// is the idempotent no-op (RowsAlready). The divergence itself is
		// recorded (warn log + RowsDiverged) and surfaced on the report line,
		// never silently swallowed, never escalated to a defect.
		if existing.Summary != r.Summary || existing.Payload != strings.TrimSpace(r.Payload) {
			// stats.RowsDiverged is the DELTA channel's spelling of the file
			// channel's stats.RowsContentWarn (epic 01a0fea1 ③): ONE
			// judgement — a content signal that never decides the row's fate,
			// counted as a warning and surfaced on the pull line
			// (`diverged=N`), never a 'conflict' tag, never a human ticket.
			stats.RowsDiverged++
			memsyncWarnf("content divergence (a warning, not a conflict): task %s — same status %s, differing summary/payload; both sides keep their own content (never overwritten), tags union only (local=%q peer=%q)",
				existing.TaskID, existing.Status, existing.Summary, r.Summary)
			if tagsEqual(existing.Tags, merged) {
				stats.RowsAlready++ // nothing new: the diverged content never travels back as a write
				return nil
			}
			if _, err := db.ExecContext(ctx,
				`UPDATE tasks SET tags = ? WHERE task_id = ?`, merged, existing.TaskID); err != nil {
				return fmt.Errorf("content divergence tags union: %w", err)
			}
			stats.RowsTagged++
			return nil
		}
		if tagsEqual(existing.Tags, merged) {
			stats.RowsAlready++
			return nil
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE tasks SET tags = ? WHERE task_id = ?`, merged, existing.TaskID); err != nil {
			return fmt.Errorf("tags union: %w", err)
		}
		stats.RowsTagged++
		return nil
	}
	// Equal non-terminal ranks with DIFFERING statuses on a CLIENT reach
	// here only when the rank ladder cannot order them — the legacy
	// conflict backstop (M4.19 ③ re-label: since claim-broadcast M4.18 the
	// normal path has ZERO conflicts; this firing is a BUG signal —
	// start-point atomicity failed somewhere — warn + synthetic notify,
	// never fatal). The server ruling above is the legacy tolerance.
	if tagsEqual(existing.Tags, merged) && conflictKnown {
		stats.RowsAlready++ // a known conflict re-echoed — nothing new
		return nil
	}
	if !conflictKnown {
		merged = normalizeTaskTags(merged + "," + syncConflictTag)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET tags = ? WHERE task_id = ?`, merged, existing.TaskID); err != nil {
		return fmt.Errorf("conflict tag: %w", err)
	}
	stats.RowsConflict++
	notifyLegacyConflict(existing.TaskID, fmt.Sprintf("status conflict (local=%s remote=%s) — conflict-tag path", existing.Status, r.Status))
	return nil
}

// notifyLegacyConflict is the M4.19 ③ semantic re-label: since the M4.18
// claim broadcast, the normal path has ZERO conflicts (start-point
// atomicity guarantees a single runner; terminals push must-succeed). A
// conflict here is therefore a BUG signal — the start-point atomicity
// failed somewhere — and gets a warn log PLUS the legacy-conflict synthetic
// notify event (same pipe as the status transitions / stale event). Never
// fatal: the ruling/tagging itself is the legacy backstop behavior.
func notifyLegacyConflict(taskID, detail string) {
	memsyncWarnf("legacy-conflict: task %s hit a sync delta conflict (%s) — normal path has zero conflicts since claim-broadcast (M4.18); check start-point atomicity", taskID, detail)
	cfg := loadTaskNotifyConfig()
	if !cfg.enabled() || !cfg.events[taskNotifyEventLegacyConflict] {
		return
	}
	payload := taskNotifyPayload{
		Event:     taskNotifyEventLegacyConflict,
		TaskID:    taskID,
		Tags:      syncConflictTag,
		Summary:   detail,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if cfg.async {
		go deliverTaskNotify(cfg, payload)
		return
	}
	deliverTaskNotify(cfg, payload)
}

// syncRowIsMirrorNoise reports whether the LOCAL row is 登记噪音 — the only
// class the server conflict ruling may blind-flatten (bug053 M4.16 semantic
// fix). A REAL ticket is a row a PERSON or an LLM session authored and that
// carries a briefing (真工单); a mirror/registration row is a machine source
// (harness auto-registration, sync replication) or payload-empty bookkeeping.
// The sync-resolved fake-closes of bug050/051/052 were all real tickets
// flattened by a ruling that granted a verdict only the real work merited.
//
// The two real-ticket roles are spelled with internal/tasksource's constants
// and READ through its Normalize (Rule 14): the aliases keep their historical
// membership (633 `llm` rows ARE Agent rows, and they were always protected)
// while a row written today is classified by its canonical value. Written
// with the raw literals this predicate would age into exactly the defect that
// retired them — be17a9bd moved the vocabulary to ONE home, and a predicate
// that outlives its writer has to read that home, not a frozen spelling.
//
// DELIBERATELY NOT WIDENED: whether `merger`/`worker`/`forward` rows with a
// briefing should also be protected is a SYNC-RULING decision (it changes
// which rows the server may flatten), not a provenance rename — the row this
// package writes today keeps the membership its predecessor had.
func syncRowIsMirrorNoise(existing *Task) bool {
	if existing == nil {
		return true // no row = nothing to protect
	}
	src, known := tasksource.Normalize(strings.TrimSpace(existing.Source))
	if !known || (src != tasksource.Human && src != tasksource.Agent) {
		return true // registration/machine source = registration noise
	}
	return strings.TrimSpace(existing.Payload) == "" // no briefing = not a real ticket
}

// syncDeltaTagConflictNoRuling unions the conflict tag onto a real ticket's
// row WITHOUT advancing status/ended_at/anchor — the bug053 guard: a human
// ticket that the server refused to flatten stays exactly where it was
// (pending on peerbox), only tagged so the next sync re-echo is a known
// conflict no-op. Returns (conflictAlreadyKnown, echoNoOp, error) — a
// known-conflict re-echo with equal tags is a pure no-op (RowsAlready).
func syncDeltaTagConflictNoRuling(ctx context.Context, db *sql.DB, existing *Task, merged string) (bool, bool, error) {
	conflictKnown := hasTag(existing.Tags, syncConflictTag)
	if conflictKnown && tagsEqual(existing.Tags, merged) {
		return true, true, nil // known conflict re-echoed — nothing new to write
	}
	merged = normalizeTaskTags(merged + "," + syncConflictTag)
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET tags = ? WHERE task_id = ?`, merged, existing.TaskID); err != nil {
		return conflictKnown, false, fmt.Errorf("conflict tag (human-task block): %w", err)
	}
	return conflictKnown, false, nil
}

// notifyLedgerConflictHumanTask is the bug053 M4.16 alarm: the sync server
// REFUSED to blind-resolve a real human/llm ticket (source manual + payload)
// and needs a human/PA to adjudicate. The row is untouched (status stays,
// anchor stays) — only the conflict tag marks it — so the ticket cannot be
// silently fake-closed again. Same pipe as the other synthetic alarms.
func notifyLedgerConflictHumanTask(taskID, detail string) {
	memsyncWarnf("ledger-conflict-human-task: task %s (%s) — server ruling BLOCKED on a real ticket (bug053 M4.16); adjudicate manually", taskID, detail)
	cfg := loadTaskNotifyConfig()
	if !cfg.enabled() || !cfg.events[taskNotifyEventLedgerConflictHumanTask] {
		return
	}
	payload := taskNotifyPayload{
		Event:     taskNotifyEventLedgerConflictHumanTask,
		TaskID:    taskID,
		Tags:      syncConflictTag,
		Summary:   detail,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if cfg.async {
		go deliverTaskNotify(cfg, payload)
		return
	}
	deliverTaskNotify(cfg, payload)
}

// syncDeltaServerResolve is the SERVER's conflict ruling (nick 定稿): a
// both-sides-wrote-the-same-row collision (equal updated_at, differing
// status) resolves as completed right here — status=completed, ended_at=now,
// tags += sync-resolved (union with BOTH sides' own tags), updated_at=now.
// Token-free pure Go: no LLM, no queue, no consistency pass. Advancing
// updated_at moves the row past the receive anchor, so the verdict
// forward-migrates to the client on the next pull — and a re-run of THIS
// side sees equal-status terminal rows → the echo no-op path (idempotent).
func syncDeltaServerResolve(ctx context.Context, db *sql.DB, bus *taskNotifyBus, existing *Task, mergedTags string, stats *SyncTaskImportStats) error {
	memsyncWarnf("legacy-conflict: task %s hit a sync delta conflict — SERVER ruling completed (the normal path has zero conflicts since claim-broadcast M4.18; check start-point atomicity)", existing.TaskID)
	notifyLegacyConflict(existing.TaskID, fmt.Sprintf("status conflict (local=%s) — server ruling to completed", existing.Status))
	now := float64(time.Now().Unix())
	tags := normalizeTaskTags(mergedTags + "," + syncTagSyncResolved)
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, ended_at = ?, updated_at = ?, tags = ?
		 WHERE task_id = ?`,
		taskStatusCompleted, now, now, tags, existing.TaskID); err != nil {
		return fmt.Errorf("server resolve: %w", err)
	}
	// Transition hook (task-notify-terminal-gap): the server's conflict
	// ruling is a real ledger move to completed.
	bus.observeIDs(existing.TaskID, existing.Status, taskStatusCompleted)
	stats.RowsResolved++
	return nil
}

// syncDeltaApplyNewer moves a known row forward onto the remote's newer
// state. M4.20-B: the decision is RANK-first (statusRankOf), not clock-
// first — the ladder it may walk (all forward, nothing regresses):
//
//	rank(remote) > rank(local): apply (claim mirror, terminal converge,
//	                            completed→verified — every ladder rung
//	                            falls out of the ranks naturally).
//	rank(remote) == rank(local) == terminal-tier (3): PEERS — the later
//	                            updated_at wins (后到覆盖); equal ts =
//	                            idempotent no-op (the old equal-anchor
//	                            conflict residue between terminal peers
//	                            is gone).
//	rank(remote) == rank(local) < 3: keep local (live mirror idempotency).
//	rank(remote) <  rank(local): already-synced truth, never applied.
//
// The UPDATE itself is ATOMIC on the rank: WHERE statusRank(current) <
// statusRank(newer) via the CASE rank expression — a concurrent local
// writer that advanced the row between our read and our write makes the
// UPDATE affect 0 rows, and we RE-READ and report explicitly (RowsLostRace;
// same lineage as the bug035 CAS).
func syncDeltaApplyNewer(ctx context.Context, db *sql.DB, bus *taskNotifyBus, existing *Task, r SyncTaskRow, peer string, stats *SyncTaskImportStats) error {
	status := strings.TrimSpace(r.Status)
	if !validTaskStatus(status) {
		stats.RowsRefused++
		stats.Errors = append(stats.Errors, fmt.Sprintf("row %s: invalid status %q", r.TaskID, status))
		return nil
	}
	localRank := statusRankOf(existing.Status)
	remoteRank := statusRankOf(status)
	// THE ONE EDGE THE RANK LADDER MUST NOT ADJUDICATE: a local RESUME arriving
	// as a peer's `failed` copy. `failed → in_progress` is a legal LOCAL move
	// (the resume edge, taskAllowedTransitions in tasks.go) whose whole purpose
	// is to continue the SAME row — but the ladder ranks `failed` at the
	// terminal tier (3) and `in_progress` at 2, so the peer's pre-resume verdict
	// outranks the live run it was resumed from. Applying it would write
	// `failed` onto a row this host is RUNNING right now: the operator's next
	// `task status` refuses the row's own run, the merge/verify lane reads a
	// verdict nobody pronounced, and a legal resume appears to have been undone.
	//
	// So this ONE edge is kept local, deterministically and without a clock:
	// rank is a statement about which of two STATUSES is later in one lifecycle,
	// and a resume is deliberately a step BACKWARDS through that lifecycle. The
	// peer's verdict is not lost — the resumed run ends in a terminal status of
	// its own, and that writes forward through the ordinary rungs. Nothing else
	// about the ladder moves: every other backward pair is still absorbed above
	// by `remoteRank < localRank`, and every forward pair still applies.
	if status == taskStatusFailed && existing.Status == taskStatusInProgress {
		stats.RowsResumeKept++
		return nil
	}
	// THE SECOND EDGE THE LADDER MUST NOT ADJUDICATE — a peer's `cancelled`
	// arriving on a row this host still holds LIVE (pending/claimed/
	// in_progress). Cancelling local work is the LOCAL dispatcher's decision
	// (`gopi task update`), never a sync-side effect: the v1 file channel says
	// so at terminalConflictRulingTx, and both channels' headers name the
	// contract (`cancelled stays local`). The rank ladder applied it anyway —
	// cancelled ranks 3, every live status ranks ≤2, so one peer's cancel
	// forward-migrated a local pending row.
	//
	// WHY THIS IS A DEFECT AND NOT A TASTE QUESTION (measured 2026-10-29, rows
	// 01a16f98/01a17866/01a17a10/01a17a6a/01a17a8a/01a17c53/01a17d07/01a17e23):
	// a PA cancelled those rows on ONE node to de-duplicate two ledgers, with a
	// reason that said "keep the hosta side, cancel the HP side" — and the
	// ladder wrote `cancelled` onto the KEPT side within the same second, with
	// the peer's reason verbatim, the peer's updated_at verbatim, the peer's
	// `from-<node>` tag filtered out by syncDeltaMergeTags, and ZERO
	// task_events rows (the notify stream fired; the audit trail did not). And
	// because `cancelled` has no legal exit in this build
	// (taskAllowedTransitions[cancelled] is empty; `task update --status
	// pending` refuses), the KEPT side of the de-duplication became
	// UNRECOVERABLE — a sync-borne status took work away and left no way back.
	//
	// So the intent travels as the SAME `cancel-was-intent` tag the v1 arm
	// writes, and the live row stays live here.
	if status == taskStatusCancelled && !terminalTaskStatus(existing.Status) {
		if _, err := db.ExecContext(ctx,
			`UPDATE tasks SET tags = ? WHERE task_id = ?`,
			syncDeltaMergeTags(existing.Tags, []string{syncTaskCancelIntentTag}),
			existing.TaskID); err != nil {
			return fmt.Errorf("delta cancel-intent tag: %w", err)
		}
		stats.RowsCancelKept++
		return nil
	}
	if remoteRank < localRank {
		stats.RowsAlready++ // backward — already-synced truth, never regresses
		return nil
	}
	if remoteRank == localRank {
		if !taskTerminalTier(status) {
			stats.RowsAlready++ // same non-terminal rung: keep local (mirror idempotency)
			return nil
		}
		// Terminal-tier peers: 后到覆盖 — the later write wins a tie.
		if r.UpdatedAt <= int64(existing.UpdatedAt) {
			stats.RowsAlready++ // equal-or-older terminal echo: idempotent no-op
			return nil
		}
	}

	// M4.18 db-sync: the deliverable blob rides on a forward move too —
	// (the adopt gate's ① pairing needs deliverable.branch on the server row).

	// THE EDGE ARRIVES WITH THE ROW — the one residue the rank ladder cannot
	// order, and the repair path for rows written before the edge could
	// travel (the 2026-10-07 migration: five chain links landed unparented).
	// A local row with NO edge and a remote copy WITH one is not a conflict —
	// the local side has no opinion to defend, the edge is a new fact — so it
	// is written here, guarded on the local value still being empty (a
	// concurrent edge is never clobbered). The dependency itself is NOT
	// re-decided: an unresolvable edge is refused by name through the SAME
	// judgement the insert lane uses, so this arm cannot mint a permanently
	// parked row (Rule 33) either.
	remoteParents := taskParentIDsOf(r.ParentTaskID, r.ParentTaskIDs)
	if len(taskParentIDsOf(existing.ParentTaskID, existing.ParentTaskIDs)) == 0 && len(remoteParents) > 0 {
		for _, parentID := range remoteParents {
			if reason, bad := syncParentUnresolvable(parentID, syncParentStatusOn(ctx, db)); bad {
				stats.RowsRefused++
				stats.Errors = append(stats.Errors, fmt.Sprintf("row %s: %s", r.TaskID, reason))
				return nil
			}
		}
		adoptFirst, adoptRest := taskParentsForWrite(remoteParents)
		res, err := db.ExecContext(ctx,
			`UPDATE tasks SET parent_task_id = ?, `+taskParentIDsColumnName+` = ?, updated_at = ? `+
				`WHERE task_id = ? AND IFNULL(parent_task_id,'') = '' AND IFNULL(`+taskParentIDsColumnName+`,'') = ''`,
			adoptFirst, adoptRest, float64(r.UpdatedAt), existing.TaskID)
		if err != nil {
			return fmt.Errorf("delta parent adopt: %w", err)
		}
		if n, rerr := res.RowsAffected(); rerr == nil && n > 0 {
			stats.RowsParented++
		}
	}

	// The parent SET is not handled here: syncDeltaMergeRow widened it BEFORE
	// the rank ladder, because a set is a fact the ladder does not order (an
	// equal-status echo returns as RowsAlready long before this function runs).
	// The adopt arm above remains the one shape this function owns — a local
	// row with NO parents at all.

	// Tags: union the remote's OWN tags (channel metadata stays out —
	// provenance on a status move rides the STATUS, not from-tag spam).
	merged := syncDeltaMergeTags(existing.Tags, r.Tags)

	// M4.18 db-sync: the deliverable blob rides on a forward move too —
	// branch/worktree reach the server with the status (the adopt gate's
	// ① pairing needs deliverable.branch on the server row). The merge
	// stays forward-only: a row that already has a deliverable is not
	// rewritten (newer-wins has no field-level truth; first valid wins).
	if err := deliverableMergeForward(ctx, db, existing, r); err != nil {
		return err
	}

	endedAt := any(nil)
	updatedAt := float64(r.UpdatedAt)
	// M4.20-B rank model: terminal tier (completed/failed/cancelled) or
	// verified both imply ended_at presence (M4.21 supersets the old
	// terminalTaskStatus check with the verify tier).
	if taskTerminalTier(status) || status == taskStatusVerified {
		if r.EndedAt > 0 {
			endedAt = float64(r.EndedAt)
		} else {
			endedAt = updatedAt // best known truth
		}
	}

	// The ATOMIC rank-guarded write (M4.20-B core): the CASE expression
	// re-derives both ranks INSIDE the UPDATE, so the guard sees the row's
	// CURRENT status, not our read's snapshot. The guard passes when the
	// row's rank is strictly below ours, OR (terminal-tier peers) the rank
	// is equal and our timestamp is later — 后到覆盖 at the row level. A
	// 0-row result = a concurrent writer outranked/echoed us → re-read,
	// report, never clobber.
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET
			status = ?,
			ended_at = COALESCE(?, ended_at),
			updated_at = ?,
			tags = ?,
			run_method = COALESCE(NULLIF(?, ''), run_method),
			run_cmd = COALESCE(NULLIF(?, ''), run_cmd),
			reason = COALESCE(NULLIF(?, ''), reason)
		 WHERE task_id = ?
		   AND ((CASE status
		          WHEN 'pending' THEN 0
		          WHEN 'claimed' THEN 1
		          WHEN 'in_progress' THEN 2
		          WHEN 'completed' THEN 3
		          WHEN 'failed' THEN 3
		          WHEN 'cancelled' THEN 3
		          WHEN 'verified' THEN 4
		          ELSE 0
		        END)
		     < (CASE ?
		          WHEN 'pending' THEN 0
		          WHEN 'claimed' THEN 1
		          WHEN 'in_progress' THEN 2
		          WHEN 'completed' THEN 3
		          WHEN 'failed' THEN 3
		          WHEN 'cancelled' THEN 3
		          WHEN 'verified' THEN 4
		          ELSE 0
		        END)
		      OR ((CASE status
		          WHEN 'pending' THEN 0
		          WHEN 'claimed' THEN 1
		          WHEN 'in_progress' THEN 2
		          WHEN 'completed' THEN 3
		          WHEN 'failed' THEN 3
		          WHEN 'cancelled' THEN 3
		          WHEN 'verified' THEN 4
		          ELSE 0
		        END)
		     = (CASE ?
		          WHEN 'pending' THEN 0
		          WHEN 'claimed' THEN 1
		          WHEN 'in_progress' THEN 2
		          WHEN 'completed' THEN 3
		          WHEN 'failed' THEN 3
		          WHEN 'cancelled' THEN 3
		          WHEN 'verified' THEN 4
		          ELSE 0
		        END)
		   AND updated_at < ?))`,
		status, endedAt, updatedAt, merged, r.RunMethod, r.RunCmd, r.Reason, existing.TaskID, status, status, updatedAt)
	if err != nil {
		return fmt.Errorf("delta rank apply: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delta rank apply rows: %w", err)
	}
	if n == 0 {
		// Lost the rank race: a concurrent local write advanced the row
		// between our read and our write. Re-read and report explicitly —
		// the newer state ON THE ROW survives either way (never clobbered).
		stats.RowsLostRace++
		cur, err := taskByID(ctx, db, existing.TaskID)
		if err == nil && cur != nil && statusRankOf(cur.Status) >= remoteRank {
			stats.RowsAlready++ // the row already carries the truth we wanted
			return nil
		}
		stats.RowsRefused++
		stats.Errors = append(stats.Errors,
			fmt.Sprintf("row %s: rank race lost (wanted %s rank %d, row now %s)", r.TaskID, status, remoteRank, curStatusOr(cur)))
		return nil
	}
	// THE AUDIT TRAIL ENTRY a sync-borne status move owes (epic 01a187f4).
	// The rank-guarded UPDATE landed (n>0), so this row's status REALLY
	// changed here — and until this line the delta ladder wrote it with ZERO
	// task_events rows (measured 2026-10-29: eight rows `cancelled` on this
	// host with an empty trail, the cancelling peer holding one event each in
	// the same second, so "who moved this, and from what?" was answerable
	// only from the OTHER node's ledger). Only this branch records: every
	// no-op below/above it (RowsAlready, the lost-race re-read, the resume and
	// cancel-kept edges) returns earlier, so a replayed sync appends nothing.
	syncStatusMoveRecord(ctx, db, syncStatusEventLegDelta, peer, existing.TaskID, existing.Status, status)
	// Transition hook (task-notify-terminal-gap): the rank-guarded UPDATE
	// landed, so the ledger row moved — the operator's stream must see it
	// (this is the path a peer's terminal report arrives through).
	bus.observeIDs(existing.TaskID, existing.Status, status)
	if remoteRank == taskStatusRank[taskStatusClaimed] || remoteRank == taskStatusRank[taskStatusInProgress] {
		stats.RowsMirrored++ // live mirror (claim) rung
		return nil
	}
	stats.RowsMigrated++ // terminal / verified upgrade rung
	return nil
}

// syncParentSetRelation is the classification of a remote parent set against
// the local one (see syncDeltaParentSetRelation).
type syncParentSetRelation int

const (
	// syncParentSetNoNewEdge: the remote set adds no edge this row lacks —
	// either it is equal, or it is smaller, which is a peer that has not seen
	// the newest edge yet. Nothing is written, so a stale peer can never TAKE
	// an edge away from this row.
	syncParentSetNoNewEdge syncParentSetRelation = iota
	// syncParentSetSuperset: the remote set is a STRICT SUPERSET — a parent
	// was ADDED at the origin. This is the one shape the widen arm lands.
	syncParentSetSuperset
	// syncParentSetDisjoint: each side holds an edge the other lacks. Landing
	// the union would park the child behind a chain no single node ever
	// declared, which is a scheduling decision (kept local, counted).
	syncParentSetDisjoint
)

// syncDeltaParentSetRelation classifies a remote parent set against the local
// one. The three answers are the whole decision surface of the widen arm, and
// they live in one place so a later reader cannot re-derive them at the call
// site and get a different answer (Rule 14): equal sets write nothing, a
// STRICT SUPERSET is the one shape a merge may land, and everything else
// (subset, disjoint, partial overlap) is a disagreement between two ledgers
// that this leg keeps LOCAL.
func syncDeltaParentSetRelation(local, remote []string) syncParentSetRelation {
	have := make(map[string]bool, len(local))
	for _, id := range local {
		have[id] = true
	}
	added := 0
	for _, id := range remote {
		if !have[id] {
			added++
		}
	}
	if added == 0 {
		// The remote brings no edge this row lacks — an equal set, or a peer
		// that has not seen the newest edge yet. Nothing is written, so a
		// stale peer can never TAKE an edge away from this row.
		return syncParentSetNoNewEdge
	}
	// The remote does carry a new edge. Landing it is legal only when every
	// LOCAL edge is also on the remote side: otherwise the two nodes disagree
	// about which chain this child belongs to, and the union would park the
	// child behind a parent it never had HERE.
	for _, lid := range local {
		if !syncParentSetContains(remote, lid) {
			return syncParentSetDisjoint
		}
	}
	return syncParentSetSuperset
}

// syncParentSetContains is the membership read the relation needs; it is a
// named helper rather than an inline loop because both sides' sets are small
// and unordered by construction (taskParentIDsOf preserves the caller's order,
// which two nodes never promise to share).
func syncParentSetContains(set []string, id string) bool {
	for _, candidate := range set {
		if candidate == id {
			return true
		}
	}
	return false
}

// syncDeltaWidenParents lands the remote's WIDER parent set onto a local row
// that already carries parents — the arm the delta leg used not to have.
//
// THE DEFECT IT REPAIRS (measured on two nodes): the adopt arm in
// syncDeltaApplyNewer fires only for a row with NO parents at all, so a local
// row carrying P1 whose peer copy carried {P1, P2, P3} arrived, passed every
// rank rung, and left P2/P3 on the floor. The child then released the moment
// P1 finished and became claimable while two of its three dependencies were
// still live — silently, because the RECEIVING side dropped the edges: no
// error, no counter, and no peer to re-send them.
//
// Returns refused=true when an ADDED edge this ledger cannot honour was named
// by syncParentUnresolvable — the row's other merge work must then stop, for
// the same reason the adopt arm stops: the caller records the refusal and the
// child is never written into a state Rule 33 parks forever.
func syncDeltaWidenParents(ctx context.Context, db *sql.DB, taskID string, remoteParents []string, stats *SyncTaskImportStats) (widened, refused bool) {
	if len(remoteParents) == 0 {
		return false, false
	}
	var localParent, localParentIDs string
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(parent_task_id,''), COALESCE(`+taskParentIDsColumnName+`,'') FROM tasks WHERE task_id = ?`,
		taskID).Scan(&localParent, &localParentIDs); err != nil {
		return false, false // the caller re-reads the row anyway; a vanished row is not this arm's refusal
	}
	localParents := taskParentIDsOf(localParent, localParentIDs)
	// THE SET FIX IS INDEPENDENT OF THE STATUS LADDER, which is the whole
	// reason this arm sits here and not in syncDeltaApplyNewer: an equal-status
	// echo returns as RowsAlready before any rank rung's adopt arm runs, so a
	// row whose set GREW at the origin was dropped on every ordinary re-sync
	// (measured: the peer copy of a child with P1 arriving from a node that
	// had since attached P2/P3). A row with NO parents therefore adopts the
	// whole remote set HERE and counts it as RowsParented — the same counter
	// and the same meaning the rank-move adopt arm has, not a second event.
	if len(localParents) == 0 {
		exists := syncParentStatusOn(ctx, db)
		for _, parentID := range remoteParents {
			if reason, bad := syncParentUnresolvable(parentID, exists); bad {
				stats.RowsRefused++
				stats.Errors = append(stats.Errors, fmt.Sprintf("row %s: %s", taskID, reason))
				return false, true
			}
		}
		adoptFirst, adoptRest := taskParentsForWrite(remoteParents)
		res, err := db.ExecContext(ctx,
			`UPDATE tasks SET parent_task_id = ?, `+taskParentIDsColumnName+` = ? `+
				`WHERE task_id = ? AND IFNULL(parent_task_id,'') = '' AND IFNULL(`+taskParentIDsColumnName+`,'') = ''`,
			adoptFirst, adoptRest, taskID)
		if err != nil {
			return false, false
		}
		if n, rerr := res.RowsAffected(); rerr == nil && n > 0 {
			stats.RowsParented++
			return true, false
		}
		return false, false
	}
	switch syncDeltaParentSetRelation(localParents, remoteParents) {
	case syncParentSetNoNewEdge:
		return false, false
	case syncParentSetDisjoint:
		// Counted, never silent, never applied — see the relation's comment.
		stats.RowsParentsConflict++
		return false, false
	}
	exists := syncParentStatusOn(ctx, db)
	for _, parentID := range remoteParents {
		if syncParentSetContains(localParents, parentID) {
			continue // an edge already on the row is not re-judged
		}
		if reason, bad := syncParentUnresolvable(parentID, exists); bad {
			stats.RowsRefused++
			stats.Errors = append(stats.Errors, fmt.Sprintf("row %s: %s", taskID, reason))
			return false, true
		}
	}
	merged := syncParentSetUnion(localParents, remoteParents)
	adoptFirst, adoptRest := taskParentsForWrite(merged)
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET parent_task_id = ?, `+taskParentIDsColumnName+` = ? WHERE task_id = ?`,
		adoptFirst, adoptRest, taskID)
	if err != nil {
		return false, false
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n > 0 {
		stats.RowsParentsWidened++
		return true, false
	}
	return false, false
}

// syncParentSetUnion keeps the LOCAL order first (parent 1 stays parent 1, so
// an older reader's single-parent column keeps meaning what it meant) and
// appends the remote's new edges in the remote's own order.
func syncParentSetUnion(local, remote []string) []string {
	out := make([]string, 0, len(local)+len(remote))
	for _, id := range local {
		out = append(out, id)
	}
	for _, id := range remote {
		if !syncParentSetContains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// deliverableForwardable reports whether a remote deliverable blob may
// overwrite the local row's: a valid, KNOWN-version blob newer than (or
// absent on) the local side. Unknown future versions are DROPPED here (the
// old schema continues to sync; new fields would be silently misread).
// The local blob is upgradeable when it is EMPTY or a submit-time repo-only
// PLACEHOLDER (deliverableRepoOnly — no branch/worktree yet): the remote
// executor's completion blob (repo+branch) is the authoritative delivery
// the adopt gate pairs on, so a placeholder must never block it (epic
// task-submit-repo-default, t-1788441090: TaskSubmit now stamps the cwd
// repo on every row, making repo-only placeholders the NORM for pending
// rows). A full local blob (branch already stamped) stays first-truth-wins.
func deliverableForwardable(existing, remote string, remoteVersion int) bool {
	if remote == "" {
		return false
	}
	if remoteVersion > deliverableVersionKnown {
		return false // from the future — drop rather than misparse
	}
	if !json.Valid([]byte(remote)) {
		return false
	}
	local := strings.TrimSpace(existing)
	if local != "" && !deliverableRepoOnly(local) {
		return false // a real local blob (branch/worktree stamped) is not rewritten
	}
	return true
}

// deliverableMergeForward forwards a row's deliverable blob when the remote
// carries one the local row lacks (db-sync HP face: the deliverable reaches
// the server the completed status rides in). Best-effort: a failed write
// moves the row to RowsRefused but never fails the session.
func deliverableMergeForward(ctx context.Context, db *sql.DB, existing *Task, r SyncTaskRow) error {
	if !deliverableForwardable(existing.Deliverable, r.Deliverable, r.DeliverableVersion) {
		return nil
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ? WHERE task_id = ?`,
		r.Deliverable, existing.TaskID); err != nil {
		return fmt.Errorf("deliverable forward: %w", err)
	}
	return nil
}

// curStatusOr renders a re-read task's status ("gone" when the row vanished
// — a re-read miss is itself reportable, not a silent zero).
func curStatusOr(t *Task) string {
	if t == nil {
		return "gone"
	}
	return t.Status
}

// SyncStateRow is one peer's persisted anchors (observability for `task
// sync --status` and the session driver's resume). Columns are M4.12
// harvest's sync_state shape: anchor = peer-domain cutoff, updated_at =
// local wall clock of the last successful session.
type SyncStateRow struct {
	Peer      string  `json:"peer"`
	Anchor    float64 `json:"anchor"`
	UpdatedAt float64 `json:"updated_at"`
}

// syncEnvEnvelope is the v2 delta envelope a PEER returns over ssh — one
// JSON document per session (epic ⑤: remote executes SELECT, packs JSON).
// It rides through the file-shaped import path unchanged (WriteSyncTaskPatch
// emits this very shape), so the local side never parses rows in memory.
type syncEnvEnvelope struct {
	SyncTaskPatch
	// Anchor is the peer's max(updated_at) over the returned rows — the
	// true end of the delta window in the PEER'S clock domain. The
	// initiator stores it as the peer-domain anchor AFTER successful
	// application (anchor last, epic ⑥). 0 = empty delta (keep the old
	// anchor — advancing into nothing would re-pull nothing anyway).
	Anchor float64 `json:"anchor"`
}

// syncEnvAnchor computes the envelope's anchor: the max updated_at among
// the returned rows (0 on an empty delta).
func syncEnvAnchor(rows []SyncTaskRow) float64 {
	var max float64
	for _, r := range rows {
		if u := float64(r.UpdatedAt); u > max {
			max = u
		}
	}
	return max
}

// syncVersionOrZero nil-safely reads a patch's version (0 when nil) — lets
// the shared version gate handle the nil case through its got==0 branch.
func syncVersionOrZero(patch *SyncTaskPatch) int {
	if patch == nil {
		return 0
	}
	return patch.Version
}

// SyncDeltaEnvelopeFromPatch wraps a v2 patch (usually read back from the
// staged ssh stdout file) into the envelope with its anchor stamped.
func SyncDeltaEnvelopeFromPatch(patch *SyncTaskPatch) (*syncEnvEnvelope, error) {
	if patch == nil || patch.Version != syncPatchVersionDelta {
		// Reuse the shared wording so every channel refuses identically.
		return nil, syncCheckPatchVersion("delta-envelope", syncVersionOrZero(patch), syncPatchVersionDelta)
	}
	return &syncEnvEnvelope{SyncTaskPatch: *patch, Anchor: syncEnvAnchor(patch.Rows)}, nil
}

// syncStateCommit advances a peer's anchors after a successful session
// (epic ⑥: 锚点最后写 — 对端应用成功才更新锚点). updated_at is the local
// wall clock (observability), anchor the PEER-domain delta end (the next
// export-since cutoff). Only moves FORWARD — a stale retry can never roll
// the anchor back into re-pulling old windows.
func syncStateCommit(peer string, peerAnchor float64) error {
	db, closeDB, err := taskSyncDB()
	if err != nil {
		return err
	}
	defer closeDB()
	ctx := context.Background()
	_, prev, err := syncStateGet(ctx, db, peer)
	if err != nil {
		return err
	}
	if peerAnchor < prev {
		peerAnchor = prev // forward-only anchors
	}
	return syncStatePut(ctx, db, peer, float64(time.Now().Unix()), peerAnchor)
}

// SyncStateList returns every peer's anchors, oldest-synced first.
func SyncStateList() ([]SyncStateRow, error) {
	db, closeDB, err := taskSyncDB()
	if err != nil {
		return nil, err
	}
	defer closeDB()
	rows, err := db.QueryContext(context.Background(),
		`SELECT peer, anchor, updated_at FROM sync_state ORDER BY updated_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncStateRow
	for rows.Next() {
		var r SyncStateRow
		if err := rows.Scan(&r.Peer, &r.Anchor, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── EPIC multi-parent: the SECOND-AND-LATER parent column's spellings ──────
//
// These live at the END of the file, not beside the pair above, and that is
// deliberate: CONFIG.md anchors `internal/tools/memory_sync_delta.go:277` (the
// Tags field of SyncExportOptions), and inserting a block above it shifts the
// anchor by exactly its own height. The census gate then reports a MOVED ROW —
// which is a real failure, and the cheap repair is to not move the line.

// syncParentIDsColumnExpr mirrors syncParentColumnExpr for the second-and-later
// parent column: every transport reads it through ONE COALESCE, so a peer build
// that predates the column is not a "no such column" error that takes the whole
// transport down.
const syncParentIDsColumnExpr = `COALESCE(` + taskParentIDsColumnName + `,'')`

// syncParentIDsColumnAlias is the wire field that expression must arrive under.
// It is the column's own name — the json tags of both wire structs spell the
// same string, and none of them types it inline here (Rule 1b).
const syncParentIDsColumnAlias = taskParentIDsColumnName

// syncParentIDsColumnNamed is the expression AS the wire field. Both parent
// columns must ride the harvest window or a reconstructed row loses parents
// 2..N — the edge would silently shrink to one parent on the way to a peer,
// which is exactly the loss the split exists to avoid.
const syncParentIDsColumnNamed = syncParentIDsColumnExpr + syncColumnAliasKeyword + syncParentIDsColumnAlias
