// Package tools — memory_sync_task.go: task-row sync (M4 memsync extension,
// devdoc/memsync-task-extension.md). The brew-row channel (memory_sync.go)
// moved knowledge between nodes; this extension moves WORK: task-ledger rows
// travel the same export→JSON patch→import path.
//
// Design (the epic's five mechanics):
//
//	export (zpeer side): SELECT tasks rows (status gate, tag filter, --since,
//	       --limit, oldest-first) → JSON patch rows WITHOUT the
//	       executor-owned columns (claimed_by/claimed_at are execution
//	       state, not work definition — a receiving node claims with its
//	       own name). Live rows travel status-less; terminal rows carry
//	       their terminal status (that status IS the reverse sync-back's
//	       payload).
//	import (HP side): every row goes through the LOCAL taskSubmit write path.
//	       ① content ladder — sha256(summary+NUL+payload), then the
//	          canonical hash, then trigram similarity, checked against
//	          live pending/claimed/in_progress rows AND against rows
//	          already seen in this patch. Since epic 01a0fea1 ③ it is a
//	          WARNING, not a skip: the twin is counted (RowsContentWarn,
//	          with RowsDuped kept for the CLI tally) and named on one
//	          Errors line, and the row is IMPORTED under its own task_id —
//	          identity is the UUID, so a repeated brief is legitimate work
//	          (the former skip cost a row every time a peer re-worded a
//	          payload; dedup is the IMPORTER's responsibility and the file
//	          is never trusted).
//	       ② task_id preserved (zpeer t-* ids stay traceable across nodes);
//	          a colliding task_id is never rewritten — skip+already, or
//	          forward-migrate when the patch carries a terminal status.
//	       ③ forward-only status migration — fresh imports write pending
//	          only; a local row's later states are advanced by local
//	          execution; the reverse direction (HP finished → zpeer) moves
//	          an existing pending row forward into the patch's terminal
//	          status (pending → completed|failed|verified are all legal
//	          taskAllowedTransitions moves; cancelled stays local).
//	       ④ provenance — tags are FORCED to carry the from-node tag
//	       (from-zpeer style, decided at import, never trusted from the
//	       file — the same rule as brew rows' node:<host>).
//	       ⑤ no-overwrite (the nick hard rule) — an existing row's
//	          summary is NEVER rewritten: a same-task_id pending
//	          row only gets a tags union.
package tools

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/tasksource"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Task-sync tunables (Rule 1: every literal in one place).
const (
	// SyncTaskTagPrefix marks rows imported through the task channel.
	// Import prepends "from-<exporter>" to tags (provenance decided at
	// import, never trusted from the file — mirroring brew's node:<host>).
	SyncTaskTagPrefix = "from-"

	// syncTagColumn is the ledger column every tag-token gate reads. Named
	// so the ONE SQL fragment below is parameterized by name, never by a
	// bare literal repeated across four call sites (Rule 1b/Rule 14).
	syncTagColumn = "tags"

	// syncTaskUnknownNode replaces a patch missing its Node field.
	syncTaskUnknownNode = "unknown-node"

	// syncTaskJSONIndent is the task patch's pretty-print indent.
	syncTaskJSONIndent = "  "

	// syncTaskHashPrefix labels the dedup hash inside logs/stats (algorithm
	// versioning: the hash content can evolve without breaking readers).
	syncTaskHashPrefix = "sha256:"

	// syncStatusEventLeg* name WHICH sync leg moved a row's status — the first
	// value in every sync status event's detail (Rule 14: one home, read by the
	// writer AND by the tests that assert the trail entry). Three legs write a
	// status forward: the v2 delta ladder (memory_sync_delta.go), the v1 harvest
	// pull (harvestUpdate) and the v1 import sync-back (syncTaskMoveStatusTx).
	syncStatusEventLegDelta   = "delta"
	syncStatusEventLegHarvest = "harvest"
	syncStatusEventLegImport  = "import"

	// syncStatusEventDetailFmt renders one sync-borne status move:
	// leg, peer whose truth arrived, and the move itself. The EVENT NAME is the
	// row's NEW STATUS — the same convention the local dispatcher's writer uses
	// (taskEventWrite(ctx, db, taskID, status, …) in TaskUpdate), so `task show`
	// renders this move on its `result:` line, which selects by "the event name
	// is not a status" (taskEventLatestDetail).
	syncStatusEventDetailFmt = "sync %s from %s: %s → %s"

	// syncStatusPrevUnknown is the prev slot when a leg cannot name the state it
	// moved away from (the v1 sync-back records the row's status only when the
	// caller read it; "" would render an empty arrow side).
	syncStatusPrevUnknown = "unknown"

	// syncTaskHashSep separates summary from payload inside the dedup hash
	// (a NUL stops "a|bc"+"d" from colliding with "a"+"bcd").
	syncTaskHashSep = "\x00"

	// Content-ladder gate names (M4.8). NAMED because the verdict travels
	// into stats.Errors / the dedup-scan report and is compared in tests:
	// a string declared, compared and printed in three places is a keyword,
	// never a bare literal (Rule 1b/Rule 14).
	syncTaskGateExact     = "exact"
	syncTaskGateCanonical = "canonical"
	syncTaskGateSimilar   = "similar"

	// taskIDRelabelSep joins a colliding task_id and its relabel suffix
	// (t-<sec>-n<node>): the node suffix keeps the provenance after a
	// collision-forced relabel; a still-colliding relabel appends -1..-9.
	taskIDRelabelSep = "-n"

	// --- M4.8 hardening: canonical-hash + similarity dedup ---------------

	// syncTaskCanonPath strips path contexts (dispatch-time absolute paths
	// are the #1 briefing embed that makes twin submissions hash
	// differently — the PA double-submit leak). Built as ONE alternation
	// applied on every whitespace-separated token.
	syncTaskCanonPath = `/[^\s'"]+/`

	// syncTaskCanonTimeMatches are the timestamp shapes the canonicalizer
	// normalizes (a dispatch-time stamp makes twin briefings differ by one
	// number — token waste, double execution). All five common shapes:
	// unix-epoch seconds, unix-milliseconds, ISO T-separator datetime,
	// ISO space-separator datetime, and a bare HH:MM:SS clock.
	syncTaskCanonTime = `1[4-9]\d{8}|2\d{9}|17\d{8}|\d{10,13}` + "|" +
		`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?` + "|" +
		`\d{1,2}:\d{2}:\d{2}`

	// syncTaskCanonCollapse collapses runs of whitespace/newlines to one
	// space — blank-line and indent differences between two hand-typed
	// copies of the same brief are noise, not signal.
	syncTaskCanonCollapse = `\s+`

	// syncTaskCanonMinLen bounds the canonicalizer from BOTH sides: below
	// it, canonicalization erases too much of a tiny payload to mean
	// anything (a 20-char summary minus its paths is a shell); above it,
	// exact-hash covers identity and similarity scan covers the rest.
	syncTaskCanonMinLen = 40

	// syncTaskCanonMaxLen caps the canonicalized text fed to the
	// trigram Jaccard comparison (twin submissions differ only in their
	// head/heart; tail-truncation matches findSimilarDuplicate's recipe).
	syncTaskCanonMaxLen = 400

	// syncTaskSimilarityThreshold is the Jaccard score above which two
	// payloads count as the same work despite their (normalized) hash
	// difference. 0.5 is MemoryStore's brew merge threshold, but CJK runs
	// score lower than English prose at equal meaning overlap (trigram
	// boundaries sit inside every Han character run — one inserted word
	// breaks ~2n grams), so the task channel keeps a wider net: 0.35
	// caught the CJK one-word-edit twin in the M4.8 E2E while
	// short unrelated briefs (a 5-char summary + path) still stayed apart.
	syncTaskSimilarityThreshold = 0.35

	// syncTaskDedupScanMaxRows caps the scan's fingerprint load (a runaway
	// ledger cannot blow the pairwise pass into O(n²) over the whole DB).
	syncTaskDedupScanMaxRows = 2000
)

// M4.13 delta-sync (timestamp-after-records) tunables — every literal the
// protocol needs in one place (Rule 1).
const (
	// syncPatchVersionDelta bumps the patch format for the delta protocol:
	// rows now carry updated_at (the version anchor) plus the merge's
	// decided-on columns; receivers reject unknown versions.
	syncPatchVersionDelta = 2

	// SyncPatchVersionDelta is the SAME value exported for callers outside
	// this package that must build a protocol-valid patch — the CLI contract
	// tests drive the peer side (`task sync --serve-remote` /
	// `--claim-remote`) with a real patch on stdin, and re-typing the literal
	// there would be a mirror of the protocol version (Rule 1b/13/14). It is
	// an alias, not a second definition: the constant above stays the one
	// place the protocol version is decided.
	SyncPatchVersionDelta = syncPatchVersionDelta

	// syncAnchorMarginBackdate is the clock-skew margin subtracted from the
	// stored anchor when selecting delta rows (epic ④: NTP 差几秒 → 锚点
	// 回退余量). Rows updated within the margin are re-sent; application is
	// idempotent, so re-sending costs nothing and a writer whose updated_at
	// lands slightly in the past (skewed clock) is still captured.
	syncAnchorMarginBackdate = 5 * time.Second

	// syncSSHTimeout bounds ONE ssh leg of a peer session (the remote
	// delta export). Mirrors dispatchSSHTimeout: degrade, never hang the
	// caller (a serve-tick sync must not stall the daemon).
	syncSSHTimeout = 15 * time.Second

	// syncPhaseDelta names the exchange in the leg's own error text, so an
	// elapsed ceiling reads as "the delta leg did not finish within …"
	// instead of ssh's opaque `signal: killed`. It lived beside the FULL
	// shape's phase name until that shape was deleted (2026-11-05).
	syncPhaseDelta = "delta"

	// syncRemoteGopi is the remote binary name the initiator invokes on
	// the peer. ONE QUOTED STRING (single ssh argv element): the remote
	// command travels as one string the peer's login shell re-parses —
	// `ssh host "gopi task sync --serve-remote"`. NEVER pass it as
	// separate argv elements: ssh re-joins them into ONE quoted string
	// anyway, but a shim/argv-shape mismatch makes gopi see the whole
	// command as ONE positional prompt → the AGENT LOOP runs on the peer
	// (costly LLM session instead of a serve-remote handshake). The
	// quoted form keeps gopi's argv split (verb=task, flag=--serve-remote).
	//
	// The VALUE is cliverbs.SyncRemoteBinaryName (Rule 14: one definition in a
	// leaf package both layers can import) — this constant is the reader name
	// the sync legs use, and it must not become a second literal of the word.
	syncRemoteGopi = cliverbs.SyncRemoteBinaryName

	// syncTempPrefix names the temp file staging the peer's delta (written
	// locally from ssh stdout, then imported through the file path — the
	// transport never parses rows in memory).
	syncTempPrefix = "gopi-sync-"

	// syncSSHConnectTimeout is the ssh TCP connect timeout (the -o value;
	// the whole exchange is bounded by syncSSHTimeout on top).
	syncSSHConnectTimeout = "10"

	// syncSSHConnectTimeoutSeconds is the SAME connect timeout as a
	// duration (the claim-lock round math composes it with syncSSHTimeout;
	// Rule 2: one value, two typed views — keep this next to the string
	// form so a change to one forces the other).
	syncSSHConnectTimeoutSeconds = 10 * time.Second
)

// SyncTaskRow is one task patch row — the work definition, never execution
// state. claimed_by/claimed_at are deliberately absent: the executor claims
// with its own name at its own node. Status is empty on live rows (the
// receiving node starts them pending); terminal rows carry their terminal
// status because that status is the reverse sync-back's payload.
type SyncTaskRow struct {
	TaskID    string   `json:"task_id"`
	Summary   string   `json:"summary"`
	Payload   string   `json:"payload,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Source    string   `json:"source,omitempty"`
	CreatedAt int64    `json:"created_at,omitempty"`
	Status    string   `json:"status,omitempty"`
	// M4.13 delta protocol: UpdatedAt is the version anchor (every write
	// touches it — M4.5's invariant). The merge compares it row-by-row:
	// newer wins, equal → conflict. 0 on legacy rows (v1 patches) — merge
	// then falls back to the no-overwrite rules (never regress).
	UpdatedAt int64 `json:"updated_at,omitempty"`
	// EndedAt rides on terminal rows (the merge stamps it verbatim so a
	// mirrored terminal history keeps its true end time). 0 = absent.
	EndedAt int64 `json:"ended_at,omitempty"`
	// Deliverable is the row's structured deliverable JSON blob
	// (deliverable{repo,branch,worktree}) — the M4.18 adopt gate's data.
	// Optional: v1 peers omit it and the merge treats it as absent.
	Deliverable string `json:"deliverable,omitempty"`
	// DeliverableVersion stamps the schema version of Deliverable (0 =
	// absent/legacy). It rides separately from the patch version so the
	// blob can evolve (a new field) without breaking older merges: a
	// merge drops blobs it does not understand (DeliverableVersion >
	// deliverableVersionKnown).
	DeliverableVersion int `json:"deliverable_version,omitempty"`
	// RunMethod + RunCmd are the row's run provenance (how the last
	// execution was launched + the re-runnable command line,
	// devdoc/run-method-provenance.md). They travel so a peer's list/show
	// renders the same answer ("how was this run") the origin node shows.
	// Empty on never-run/legacy rows; the merge adopts last-non-empty.
	RunMethod string `json:"run_method,omitempty"`
	RunCmd    string `json:"run_cmd,omitempty"`
	// Reason is the operator-visible WHY of the row's current status
	// (task-update-reason epic): stamped by `task update --reason` (or the
	// status default on a bare transition). It travels so a peer's list/show
	// answers "why is this task in this state" like the origin node. Empty
	// on pre-epic/never-updated rows; the merge adopts last-non-empty.
	Reason string `json:"reason,omitempty"`
	// ParentTaskID is the row's DEPENDENCY EDGE (Rule 33: the row is parked
	// until the parent reaches the release set). It travels because a row
	// shipped without it arrives as five independent tasks: a five-link
	// chain sent one row at a time became five claimable rows on the peer
	// (measured zpeer→hp, 2026-10-07). Absent on pre-fix rows AND on a row
	// whose edge the operator deliberately dropped — and an IMPORT refuses a
	// row whose edge the receiving ledger cannot honour (syncParentVerdict,
	// syncParentUnresolvable), so "absent" never means "a live edge was
	// silently dropped here".
	//
	// The edge is a FACT ABOUT THE ROW, never about this node: it is carried
	// as a bare id, and the receiving ledger resolves it against its own
	// rows. An id the receiver does not hold is refused there by name — the
	// one arm that would otherwise reproduce this very defect (Rule 33: a
	// dangling parent parses as a permanent park with no repair path).
	ParentTaskID string `json:"parent_task_id,omitempty"`
	// ParentTaskIDs is the SECOND-AND-LATER parent list (EPIC multi-parent),
	// carried verbatim from parent_task_ids. It rides this wire for exactly the
	// reason ParentTaskID does — and the two travel TOGETHER, which is what
	// keeps the compatibility split intact: a receiver that understands only
	// ParentTaskID still gets parent 1 and keeps the row parked, instead of
	// receiving a multi-parent row that looks parentless and is claimable at
	// once. Omitted (not "") on a single-parent row, so a peer's payload for an
	// ordinary row is byte-identical to what it was before this field existed.
	ParentTaskIDs string `json:"parent_task_ids,omitempty"`
}

// deliverableVersionKnown is the highest deliverable-blob schema version
// THIS build's merge understands (Rule 1: named, one place). Bumps only
// when the blob's shape changes. v2 = the "attachments" array added by the
// epic-task-reframe Phase-1 (task_session.go); the version must equal
// taskDeliverableVersionWithAttachments in lockstep (a drift would let a
// v2 blob sync under the v1 gate and silently drop the attachments on
// forward-merge).
const deliverableVersionKnown = taskDeliverableVersionWithAttachments

// SyncTaskPatch is the export file format for tasks (versioned).
type SyncTaskPatch struct {
	Version    int           `json:"version"`
	Node       string        `json:"node"`
	ExportedAt int64         `json:"exported_at"`
	Rows       []SyncTaskRow `json:"rows"`
	// TagsFilter is the partition this patch was exported under (v2 session
	// patch only, empty = whole window). The peer's serve-remote applies it
	// to its OWN push — a --tags session is symmetric. Import ignores it
	// (the merge has no tag policy).
	TagsFilter string `json:"tags_filter,omitempty"`
	// Full marks a FULL-RECONCILIATION pass (v2, M4.24): the initiator
	// bypassed its anchor and shipped EVERYTHING; the peer's serve-remote
	// mirrors the bypass on its own push (AfterSource=0) so both sides
	// re-deliver their whole ledger. The rank merge makes the replay
	// idempotent. Never set by a plain delta export.
	Full bool `json:"full,omitempty"`
	// Generator identifies the exporting binary (see SyncPatch.Generator).
	// Informational; import never gates on it.
	Generator string `json:"generator,omitempty"`
}

// syncTaskPatchVersion is the current task patch format version.
const syncTaskPatchVersion = 1

// SyncTaskExportOptions controls the task export side.
type SyncTaskExportOptions struct {
	// Peer is the destination node (M4.24 anti-reflow correction):
	// from-<peer> rows are excluded (reflow), self-tagged rows travel.
	Peer string
	// DBPath overrides the ledger ("" = the default path). Required for
	// tests and CLI --db.
	DBPath string
	// Status filters which rows travel: a valid task status, or "" = every
	// row (the reverse direction exports completed/failed explicitly).
	Status string
	// Tags restricts the export to rows carrying this tag.
	Tags string
	// SinceUnix exports only rows created after this time (0 = all).
	SinceUnix int64
	// Limit caps the row count (0 = unlimited).
	Limit int
}

// syncTaskExportColumns is the v1 file channel's projection — the columns
// SyncTaskExport scans, in the ORDER the Scan below reads them. The two
// parent expressions are the row's dependency edge and ride it verbatim from
// the shared spellings (syncParentColumnExpr / syncParentIDsColumnExpr) that
// the delta export (syncDeltaSelectColumns) and the peer-side harvest window
// (syncRemoteCoalescedProjection) already use: one edge, three transports,
// one spelling (Rule 14).
//
// The parent columns are COALESCEd for the same reason as everywhere else: a
// ledger written before the column existed must scan as "" rather than fail
// the whole export on a NULL.
//
// memory_sync_task_export_wire_test.go is the gate: it asserts this projection
// carries BOTH parent expressions, and that they are the same constants the
// delta and peer-side projections read (a second spelling is how the siblings
// drifted in the first place).
const syncTaskExportColumns = `task_id, summary, COALESCE(payload,''), COALESCE(tags,''),
	                 COALESCE(source,''), created_at, status, updated_at, COALESCE(ended_at,0),
	                 COALESCE(run_method,''), COALESCE(run_cmd,''), COALESCE(reason,''),
	                 ` + syncParentColumnExpr + `, ` + syncParentIDsColumnExpr

// SyncTaskExportStats reports what the export produced.
type SyncTaskExportStats struct {
	RowsExported int
	Scanned      int
	SkippedSince int
}

// SyncTaskExport ledger rows into a JSON patch. The DB is opened read-only
// for selection; ids travel so a receiving node can trace the work back.
func SyncTaskExport(opts SyncTaskExportOptions) (*SyncTaskPatch, *SyncTaskExportStats, error) {
	dbPath := opts.DBPath
	if dbPath == "" {
		dbPath = memoryDBPath()
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("memsync export-tasks: %w", err)
	}
	defer db.Close()

	if opts.Status != "" && !validTaskStatus(opts.Status) {
		return nil, nil, fmt.Errorf("memsync export-tasks: invalid status %q (want one of %s)",
			opts.Status, strings.Join(taskStatusWritable, "|"))
	}

	// The projection is a NAMED constant because a gate reads the very same
	// text: the two parent columns are the row's dependency EDGE (Rule 33),
	// and the defect this constant closes was a SELECT that simply did not
	// name them — the patch row then carried no parent key at all, the
	// receiving ledger materialised an unparented row, and the child became
	// claimable at once with exit=0, refused=0 and an empty stderr (Rule 15:
	// the loss had no observer). Both columns come from the SHARED
	// expressions the delta and harvest transports already read (Rule 14),
	// so no transport can spell an edge differently from another.
	query := `SELECT ` + syncTaskExportColumns + `
	          FROM tasks WHERE 1=1`
	var args []any
	if opts.Status != "" {
		query += " AND status = ?"
		args = append(args, opts.Status)
	}
	if tag := strings.TrimSpace(opts.Tags); tag != "" {
		query += " AND " + syncTagTokenMatchSQL(syncTagColumn)
		args = append(args, syncTagTokenPattern(tag))
	}
	// Anti-reflow (brew double-gate semantics): rows tagged from-<THIS
	// node> are channel-borne — they re-import no-op on the peer anyway,
	// but excluding them here keeps a relay node (HP exporting to zpeer a
	// patch HP itself received from zpeer) from ever shipping someone
	// else's rows a second time. GOPI_SYNC_NODE override respected.
	// M4.20-B EXCEPTION: TERMINAL rows are exempt from the anti-reflow
	// gate — a verdict must reach its origin node too (A completed the
	// row, B mirrored it, B's pull must deliver A's verdict back even
	// though B's copy carries from-A; the import face absorbs the echo
	// idempotently). Terminal verdicts are history, not channel spam.
	// M4.24 anti-reflow correction (nick's anchor-premise doctrine): the
	// gate must exclude rows the PEER sent us (from-<peer> — relaying them
	// back is the reflow), NOT rows tagged from-<self>. Self-tagged rows
	// are either local-born (first export is their WHOLE POINT) or
	// homecoming verdicts (the terminal exemption below already lets them
	// through — with this fix the exemption becomes unnecessary for
	// non-terminal rows too, closing the 27-row silent-loss class).
	// syncGateTag returns the raw node NAME ("" = this node — legacy
	// self-gate); the shared tag-pattern below owns the from- prefix once.
	gateTag := syncGateTag(opts.Peer)
	query += " AND NOT " + syncTagTokenMatchSQL(syncTagColumn)
	args = append(args, syncTagTokenPattern(SyncTaskTagPrefix+gateTag))
	if opts.SinceUnix > 0 {
		query += " AND created_at >= ?"
		args = append(args, float64(opts.SinceUnix))
	}
	// Oldest first — the dispatcher's own claim order (created_at ASC, id
	// ASC tiebreak). The tasks table has no priority column (P3 roadmap);
	// created-order is the flat priority.
	query += " ORDER BY created_at ASC, id ASC"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("memsync export-tasks: %w", err)
	}
	defer rows.Close()

	hostname, _ := os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
	patch := &SyncTaskPatch{Version: syncTaskPatchVersion, Node: hostname, ExportedAt: time.Now().Unix(), Generator: syncGeneratorVersion()}
	stats := &SyncTaskExportStats{}
	for rows.Next() {
		var r SyncTaskRow
		var tags, source string
		var createdAt, updatedAt, endedAt float64 // SQLite REAL — scan into float, cast below
		if err := rows.Scan(&r.TaskID, &r.Summary, &r.Payload, &tags, &source, &createdAt, &r.Status, &updatedAt, &endedAt, &r.RunMethod, &r.RunCmd, &r.Reason,
			&r.ParentTaskID, &r.ParentTaskIDs); err != nil {
			return nil, nil, fmt.Errorf("memsync export-tasks: scan: %w", err)
		}
		r.CreatedAt = int64(createdAt)
		r.UpdatedAt = int64(updatedAt)
		if endedAt > 0 {
			r.EndedAt = int64(endedAt)
		}
		// Live statuses (pending/claimed/in_progress) are execution state —
		// they never travel (the receiving node starts the work pending).
		// Terminal statuses ARE the reverse sync-back's payload and travel
		// verbatim.
		if !terminalTaskStatus(r.Status) {
			r.Status = ""
		}
		r.Tags = splitSources(tags) // reuse the comma-split helper (trimmed, empty-dropping)
		r.Source = source
		stats.Scanned++
		if opts.Limit > 0 && len(patch.Rows) >= opts.Limit {
			break
		}
		patch.Rows = append(patch.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("memsync export-tasks: %w", err)
	}
	if patch.Rows == nil {
		patch.Rows = []SyncTaskRow{} // JSON: [] not null — empty export stays valid
	}
	return patch, stats, nil
}

// WriteSyncTaskPatch serializes the task patch to a JSON file (pretty-printed).
func WriteSyncTaskPatch(patch *SyncTaskPatch, outPath string) error {
	data, err := json.MarshalIndent(patch, "", syncTaskJSONIndent)
	if err != nil {
		return fmt.Errorf("memsync: marshal: %w", err)
	}
	if err := os.WriteFile(outPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("memsync: write %s: %w", outPath, err)
	}
	return nil
}

// SyncTaskImportStats reports the import outcome per row class. The delta
// path (SyncTaskImportDelta) fills RowsMirrored (pending → claimed/
// in_progress forward mirror) and RowsConflict (equal updated_at, differing
// row — both sides keep their row, tagged for a human).
//
// THE CONTENT COUNTERS — ONE RULE, THREE EVENTS (epic 01a0fea1 ③, aligned
// with the delta path's 内容分歧降级 d3f8120d). The rule is shared by both
// channels: a CONTENT signal is never a verdict — it is counted, reported
// (Errors / warn log) and surfaced on that channel's report line, and the
// row travels on its own task_id. Only an id collision carrying a DIFFERENT
// SUMMARY (two genuinely different submissions under one id) is relabelled
// or refused — the SUMMARY is the work definition, the payload is prose.
//
//	counter          event (what was observed)                    report line
//	RowsContentWarn  file channel: the CONTENT ladder matched a    import:
//	                 LIVE twin (exact/canonical/similar work) —    `duped=N`
//	                 the row is imported ANYWAY (Rule 15: a
//	                 degraded check reports, it does not go
//	                 silent).
//	RowsDuped        the RETAINED alias of exactly that event —    (same line)
//	                 the file channel increments both, so the
//	                 CLI's counted line and every caller reading
//	                 the old counter keep seeing it. RowsDuped is
//	                 never a SKIP any more: the twin tally is a
//	                 warning, not a dropped row.
//	RowsDiverged     delta channel: same task_id, equal anchor,     pull:
//	                 SAME status, differing summary/payload — a     `diverged=N`
//	                 peer's restatement of the same row, counted
//	                 as a warning. Never 'conflict' (that tag is
//	                 the must-arbitrate marker, reserved for a
//	                 STATUS divergence), never a human ticket.
//
// RowsContentWarn/RowsDuped (a twin MATCH) and RowsDiverged (a peer
// MISMATCH on one row) are opposite observations of the same question, so
// they stay two counters — one per channel — under the one rule above.
type SyncTaskImportStats struct {
	RowsImported int // new pending rows queued
	RowsDuped    int // file channel: the content ladder matched a live twin — the row is STILL imported (the retained alias of RowsContentWarn)
	RowsAlready  int // task_id collision with nothing to apply — skipped (a row LOST to a concurrent importer's committed insert lands here too: the winner's row IS the work)
	RowsMigrated int // existing pending row migrated to a terminal status
	RowsTagged   int // existing pending row kept, tags unioned only
	RowsRefused  int // errors (validation, state machine, ...)
	// RowsContentWarn is the honest name for the event RowsDuped aliases:
	// the content ladder matched and the row was imported ANYWAY (Rule 15 —
	// a degraded check reports, it does not go silent). The full rule, and
	// where it is printed, are in the type's doc comment above.
	RowsContentWarn int
	Errors          []string
	RowsMirrored    int // M4.13 delta: status mirrored forward (claim mirror)
	RowsConflict    int // M4.13 delta: equal anchor, differing row — tagged
	// RowsDiverged is the delta channel's content warning (d3f8120d): same
	// task_id, equal anchor, SAME status, differing summary/payload. It is
	// the peer-restatement event; RowsContentWarn is the twin-MATCH event.
	// Same rule, same channel split — see the table in the type's doc.
	RowsDiverged int
	// RowsParented counts rows whose dependency EDGE this import adopted onto
	// an existing local row that carried none — the repair arm of the
	// 2026-10-07 migration (five chain links landed unparented). It is its own
	// counter because "the row was already current" and "the row was already
	// current AND the edge landed" are different answers for the operator
	// deciding whether a re-send repaired the chain.
	RowsParented int
	// RowsParentsWidened counts rows whose parent SET this import WIDENED onto
	// an existing local row that already carried at least one parent — the arm
	// that used to drop the remote's extra edges on the floor (a local row
	// with P1 ignored a peer copy carrying {P1,P2,P3}, so the child released
	// as soon as P1 finished and two live dependencies were never known here).
	// It is its own counter, beside RowsParented, because "the row had no edge
	// and gained one" and "the row had an edge and gained MORE" are different
	// answers for an operator asking whether a sync repaired a chain.
	RowsParentsWidened int
	// RowsParentsConflict counts the rows whose local and remote parent sets
	// each hold an edge the other lacks. The local set is kept and the
	// disagreement is REPORTED, never silently merged: a union would park the
	// child behind a parent it never had here (a scheduling decision no sync
	// leg may make), and a drop would be the very silence this pair of
	// counters exists to end (Rule 15).
	RowsParentsConflict int
	RowsResolved        int // M4.13 delta: server ruled a conflict → completed
	RowsRelabeled       int // collision: same id, DIFFERENT work → re-labeled -n<node>
	RowsLostRace        int // M4.20-B: rank-guarded UPDATE hit 0 rows — re-read done, row already newer
	// RowsResumeKept counts the ONE backward edge the ladder deliberately does
	// not adjudicate: a peer's `failed` copy arriving on a row this host resumed
	// into `in_progress` (the resume edge — see syncDeltaApplyNewer). It is its
	// own counter because "the row was already current" (RowsAlready) and "we
	// refused a peer's verdict in favour of a live local run" are different
	// answers for an operator asking whether a resume survived a sync.
	RowsResumeKept int
	// RowsCancelKept counts the SECOND backward edge the ladder deliberately
	// does not adjudicate: a peer's `cancelled` copy arriving on a row this
	// host still holds LIVE. The cancel travels as the `cancel-was-intent` tag
	// and the local row stays live — cancelling local work is the local
	// dispatcher's decision, and `cancelled` has no legal exit in this build,
	// so letting the peer's verdict land would take the work away
	// unrecoverably (measured 2026-10-29, eight rows).
	RowsCancelKept int
}

// syncTaskWorkHash is the dedup key: sha256(summary + NUL + payload). The
// SQL probe matches on the same (summary, payload) column equality — the
// hash names that identity deterministically for stats and in-patch dedup.
func syncTaskWorkHash(summary, payload string) string {
	sum := sha256.Sum256([]byte(summary + syncTaskHashSep + payload))
	return syncTaskHashPrefix + hex.EncodeToString(sum[:])
}

// syncTaskCanonRe is the M4.8 canonicalizer's scrub pattern (built once —
// regexp.MustCompile at init would panic on a bad pattern during tests
// anyway, so the lazy-sync form keeps construction next to the constants).
var (
	syncTaskCanonOnce sync.Once
	syncTaskCanonRe   *regexp.Regexp
)

func syncTaskCanonPattern() *regexp.Regexp {
	syncTaskCanonOnce.Do(func() {
		syncTaskCanonRe = regexp.MustCompile("(" + syncTaskCanonPath + ")|(" +
			syncTaskCanonTime + ")|(" + syncTaskCanonCollapse + ")")
	})
	return syncTaskCanonRe
}

// syncTaskCanonText is the canonical form the hardened hash sees (epic ①:
// hash-input normalization). It strips the three embeds that make a PA
// double-submit of the same brief hash differently — absolute paths
// (dispatch-time workdir/embedded repo paths), timestamps (epoch seconds,
// ISO datetimes, wall clocks), and whitespace runs — then lowercases
// ASCII. Exact identity (syncTaskWorkHash) always runs first; this form is
// the SECOND gate, the near-identity catch.
func syncTaskCanonText(s string) string {
	if len([]rune(strings.TrimSpace(s))) < syncTaskCanonMinLen {
		return strings.TrimSpace(s) // too small to canonicalize meaningfully
	}
	out := syncTaskCanonPattern().ReplaceAllString(s, " ")
	fields := strings.Fields(out)
	for i, f := range fields {
		fields[i] = strings.ToLower(f)
	}
	return strings.Join(fields, " ")
}

// syncTaskCanonHash is the near-identity dedup key: sha256 over the
// canonical text of summary+payload. Two submissions of the same brief —
// one dispatched Monday with /home/user paths and one Tuesday with fresh
// timestamps — normalize to the same key.
func syncTaskCanonHash(summary, payload string) string {
	text := syncTaskCanonText(summary) + " " + syncTaskCanonText(payload)
	text = truncateRunes(text, syncTaskCanonMaxLen)
	sum := sha256.Sum256([]byte(text))
	return syncTaskHashPrefix + hex.EncodeToString(sum[:])
}

// syncTaskCanonNear reports whether two payloads are the same work by
// canonicalized-exact-hash OR trigram-Jaccard similarity (the epic's ② —
// the summary-changed-by-one-word case the hash can never catch). Summary
// and payload are compared on SEPARATE axes and EITHER may veto-or-convict:
// the summary is the human-scannable title (its trigrams densify one-word
// edits — "去重的泄漏点" vs "去重泄漏" scores 0.625 on a 10-char title but
// drowns to 0.23 inside the full payload), while the payload catches
// summaries that match while the body diverges. The Jaccard recipe is
// MemoryStore's brew dedup (trigramSet), reused, not re-derived; the
// threshold is syncTaskSimilarityThreshold (tuned CJK-wide).
func syncTaskCanonNear(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ca, cb := syncTaskCanonText(a), syncTaskCanonText(b)
	if ca == cb {
		return true
	}
	ca = truncateRunes(ca, syncTaskCanonMaxLen)
	cb = truncateRunes(cb, syncTaskCanonMaxLen)
	return jaccard(ca, cb) >= syncTaskSimilarityThreshold
}

// syncTaskCanonNearPair is the pair-level verdict over (summary, payload)
// rows: summary near OR payload near counts as the same work (the title is
// the discriminator one-word edits cannot hide behind; the body is the
// discriminator summary-reuse cannot hide behind).
func syncTaskCanonNearPair(a, b liveDedupRow) bool {
	if syncTaskCanonNear(a.summary, b.summary) {
		return true
	}
	return syncTaskCanonNear(a.payload, b.payload)
}

// importTaskOpenDB opens the ledger for the single-transaction import and
// hands back the *sql.DB (the tx's owner) plus its close func. It is split
// out of importTaskBeginTx so the begin failure path and the success path
// close the handle through ONE defer (no double-close, no leaked handle).
func importTaskOpenDB(ctx context.Context) (*sql.DB, func(), error) {
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, nil, err
	}
	//stylegate:ignore per-step handle; the import's own defer closes it once the tx is done
	return db, func() { db.Close() }, nil
}

// importTaskBeginTx opens the ledger and BEGINs the import's ONE transaction
// (the TaskHarvest single-tx pattern, M4.12). openTasksDB → openDB sets
// `_txlock=immediate` on the DSN, so this BeginTx issues BEGIN IMMEDIATE: the
// write lock is taken at BEGIN, and a concurrent writer (a peer's import, a
// running worker) QUEUES on dbBusyTimeoutMSec up front instead of failing
// mid-transaction after part of the patch is already on the wire.
//
// THE CONTRACT — SQLITE_BUSY vs a PARTIAL import (epic memsync-import-atomic):
//
//	Before this whole-patch transaction, every helper opened its OWN
//	short-lived connection and each statement auto-committed. A mid-patch
//	SQLITE_BUSY (two writers interleaving — the db-sync NORMAL case, measured
//	5-of-6 rows landed) or a crash therefore left a PARTIAL patch applied, and
//	the residue was indistinguishable from real work. From here on the failure
//	mode is all-or-nothing: the BUSY aborts the whole batch, the tx rolls back
//	and ZERO rows land, and the caller gets an explicit error (the CLI exits 1)
//	it can retry the moment the lock clears. A retry is always safe: a patch
//	that did land commits fully, and re-importing it is idempotent (the
//	content ladder reports its warning, the UNIQUE(task_id) arbiter reports
//	already). Exactly-once per task_id holds in EVERY outcome — the loser of a
//	write race wrote nothing.
//
// The returned cleanup rolls back (a no-op after a successful Commit — the tx
// is done) and closes the handle; callers defer it immediately after the call.
func importTaskBeginTx(ctx context.Context) (*sql.Tx, func(), error) {
	db, closeDB, err := importTaskOpenDB(ctx)
	if err != nil {
		return nil, nil, err
	}
	tx, err := db.BeginTx(ctx, nil) // _txlock=immediate (openDB) ⇒ BEGIN IMMEDIATE
	if err != nil {
		closeDB()
		return nil, nil, fmt.Errorf("memsync import-tasks: begin transaction: %w", err)
	}
	return tx, func() {
		//stylegate:ignore rollback after a successful commit returns ErrTxDone (a no-op) and releases the write lock on the error paths
		_ = tx.Rollback()
		closeDB()
	}, nil
}

// SyncTaskImport applies a task patch through the LOCAL taskSubmit write
// path (the identical INSERT TaskSubmit performs), with the epic's five
// mechanics layered on top. Tags are never trusted from the file: the
// from-<exporter> tag is forced on at import.
//
// ATOMICITY: the WHOLE patch is applied inside ONE transaction
// (importTaskBeginTx). A mid-patch storage failure (SQLITE_BUSY after the
// timeout, disk I/O, a constraint) aborts the batch — see the BUSY-vs-partial
// contract on importTaskBeginTx; there is no silent partial import.
//
// Per-row refusals are still accumulated — validation, state-machine and
// channel policy judgments, each marked with errImportRefused, write nothing,
// so continuing is safe. Any other failure out of a tx-scoped writer is a
// STORAGE failure: it aborts the whole tx, rolls back and surfaces as an
// error (exit 1 at the CLI).
func SyncTaskImport(patchPath string) (*SyncTaskImportStats, error) {
	data, err := os.ReadFile(patchPath)
	if err != nil {
		return nil, fmt.Errorf("memsync import-tasks: read %s: %w", patchPath, err)
	}
	var patch SyncTaskPatch
	if err := json.Unmarshal(data, &patch); err != nil {
		return nil, fmt.Errorf("memsync import-tasks: parse %s: %w", patchPath, err)
	}
	if err := syncCheckPatchVersion("import-tasks", patch.Version, syncTaskPatchVersion); err != nil {
		return nil, err
	}
	node := patch.Node
	if node == "" {
		node = syncTaskUnknownNode
	}
	fromTag := SyncTaskTagPrefix + node

	ctx := context.Background()
	tx, closeTx, err := importTaskBeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer closeTx()

	// lifecycle buffers the importer's status moves: their row-addressed
	// notifications are only true AFTER the commit, so they flush past it
	// (the task-notify contract — a rolled-back write announces nothing).
	// A fresh INSERT notifies nothing, exactly as TaskSubmit's own insert
	// does not; only a migrate/union over an existing row is a transition.
	var lifecycle *taskNotifyBus

	stats := &SyncTaskImportStats{}
	liveSeen := []liveDedupRow{} // every live row this import has judged (patch rows + queued rows)
	for i, r := range patch.Rows {
		r.TaskID = strings.TrimSpace(r.TaskID)
		r.Summary = strings.TrimSpace(r.Summary)
		r.Payload = strings.TrimSpace(r.Payload)
		if r.Summary == "" {
			stats.RowsRefused++
			stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: empty summary", i))
			continue
		}
		// Mechanic ① — the CONTENT ladder, degraded to a WARNING (epic
		// 01a0fea1 ③). Rows matching the content of a LIVE local row or of
		// an earlier row in this patch are REPORTED (counted + one Errors
		// line) and then imported under their OWN task_id: identity is the
		// UUID, not the content, so a repeated brief is legitimate work,
		// never a silent skip. The former skip cost a row every time a peer
		// re-worded a payload for the same task.
		//
		// M4.8 hardening keeps its ladder behind the exact rung: the exact
		// sha256(summary+NUL+payload) misses a PA double-submit whose
		// briefings embed a dispatch timestamp/path (leak ①) or whose
		// summary changed by one word (leak ②) — so a near-identity miss
		// falls through to (a) the canonical hash (paths/timestamps/
		// whitespace stripped) and (b) trigram-Jaccard similarity, both
		// against live rows AND already-processed patch rows. Both now
		// report instead of skipping.
		//
		// A row is never its own twin: the same task_id is the same work by
		// identity, so candidates carrying THIS id are excluded (the same-id
		// case is settled by the collision branch below, not by content).
		if r.Status == "" {
			row := liveDedupRow{taskID: r.TaskID, summary: r.Summary, payload: r.Payload}
			hit, twin, why := false, "", ""
			duped, gate, liveTwin, err := syncTaskLiveDedupHitTx(ctx, tx, r.TaskID, r.Summary, r.Payload)
			if err != nil {
				return nil, fmt.Errorf("memsync import-tasks: row %d: %w", i, err)
			}
			if duped {
				hit, twin, why = true, liveTwin, gate
			} else if g, t, ok := syncTaskDedupDecide(row, liveSeen); ok {
				hit, twin, why = true, t, g
			}
			// Register the row for the in-patch half of the ladder, then
			// report at most ONE warning line per row (three passes over
			// one twin would triple-count it).
			liveSeen = append(liveSeen, row)
			if hit {
				recordContentWarn(stats,
					"row %d (%s): content twin of %s (%s gate) — imported anyway (identity is the task_id; the twin is reported, not enforced)", i, r.TaskID, twin, why)
			}
		}
		importedTags := importTaskTags(r.Tags, fromTag)

		existing, err := taskByIDTx(ctx, tx, r.TaskID)
		if err != nil {
			return nil, fmt.Errorf("memsync import-tasks: row %d: %w", i, err)
		}
		if existing == nil {
			if taskTerminalTier(r.Status) || r.Status == taskStatusVerified {
				// M4.20-B: a terminal row with no local counterpart is the
				// missing mirror history — the receiving side MIRRORS the
				// terminal verdict as a non-runnable history row (mirror
				// insert: status carried, never enqueued, never re-run).
				// Refusing it broke bilateral convergence: a node that had
				// never seen the row would NEVER learn the task finished
				// (the verdict re-fused at every exchange). Mirror insert
				// is append-only history — the anti-double-run invariant
				// holds because the row never passes through pending.
				if err := importTaskMirrorTx(ctx, tx, r, importedTags, stats); err != nil {
					if isImportRefusal(err) {
						stats.RowsRefused++
						stats.Errors = append(stats.Errors, fmt.Sprintf("row %d (%s): %v", i, r.TaskID, err))
						continue
					}
					return nil, fmt.Errorf("memsync import-tasks: row %d (%s): %w", i, r.TaskID, err)
				}
				continue
			}
			if err := importTaskFreshTx(ctx, tx, r, importedTags, stats); err != nil {
				if isImportRefusal(err) {
					stats.RowsRefused++
					stats.Errors = append(stats.Errors, fmt.Sprintf("row %d (%s): %v", i, r.TaskID, err))
					continue
				}
				return nil, fmt.Errorf("memsync import-tasks: row %d (%s): %w", i, r.TaskID, err)
			}
			continue
		}
		if err := importTaskExistingTx(ctx, tx, existing, r, importedTags, stats, node, &lifecycle); err != nil {
			if isImportRefusal(err) {
				stats.RowsRefused++
				stats.Errors = append(stats.Errors, fmt.Sprintf("row %d (%s): %v", i, r.TaskID, err))
				continue
			}
			return nil, fmt.Errorf("memsync import-tasks: row %d (%s): %w", i, r.TaskID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("memsync import-tasks: commit: %w", err)
	}
	// Past the commit the writes are ledger truth — announce the ONE status
	// move the patch made (if any) now.
	if lifecycle != nil {
		taskNotifyFlushDeferred(lifecycle)()
	}
	return stats, nil
}

// errImportRefused marks a row-level POLICY refusal: the importer records the
// row's error and CONTINUES, because a refusal writes nothing — continuing
// cannot leave a partial patch. It is a SENTINEL rather than a message match
// on purpose (Rule 15): every OTHER error out of a tx-scoped writer is a
// STORAGE failure (SQLITE_BUSY past the timeout, disk I/O, a constraint, an
// unexpected trigger/schema error) which must abort the WHOLE batch. Reading
// an unknown failure as a refusal is precisely how a partial import hides —
// the rows already inserted earlier in the transaction would commit.
var errImportRefused = errors.New("import row refused")

// importRefusal builds a policy-refusal error (see errImportRefused); the
// sentinel travels with the message, so the loop's classification and the
// operator's log line come from the same value.
func importRefusal(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errImportRefused, fmt.Sprintf(format, args...))
}

// isImportRefusal reports whether the import loop may record a row's error and
// continue: a policy refusal, or a task_id race lost to a concurrent importer
// (whose committed row already IS the work). Everything else aborts the tx.
func isImportRefusal(err error) bool {
	return errors.Is(err, errImportRefused) || errors.Is(err, errImportIDTaken)
}

// errImportIDTaken is the refusal a lost task_id race produces (the winner's
// row is already committed): the row reports already, the tx stays usable.
// A sentinel rather than a message match — the ONE classification the import
// loop must never confuse with a storage failure, which the same SQLite
// INSERT can also raise.
var errImportIDTaken = errors.New("task_id already present in the ledger")

// importTaskInsertTx queues a NEW row inside the import tx — the exact INSERT
// insertTaskRow performs (the ONE TaskSubmit write path), on the tx's
// connection instead of a fresh one. The two insert styles differ only in the
// `source` default (an empty source is rejected here rather than defaulted,
// because the import loop validates the row before it reaches this helper)
// and the status column: the import pre-stamps pending, so the text-boundary
// guards, the task_id uniqueness arbiter, the priority derivation and the
// column set stay the shared ones.
//
// UNIQUE(task_id) is the concurrent-import arbiter: the losing importer is
// told via errImportIDTaken (the row reports already) — a race outcome, never
// a storage failure. A constraint violation does not abort the SQLite
// transaction, so the batch continues safely with nothing written for that
// row.
func importTaskInsertTx(ctx context.Context, tx *sql.Tx, f taskCreateFields) error {
	f.TaskID = taskIDNormalize(f.TaskID)
	if !validTaskSource(f.Source) {
		return importRefusal("invalid source %q", f.Source)
	}
	f.Summary = sanitizeTaskSummary(f.Summary)
	f.Payload = sanitizeTaskPayloadText(f.Payload)
	f.Summary = strings.TrimSpace(f.Summary)
	if f.Summary == "" {
		return importRefusal("task: summary must not be empty")
	}
	f.Tags = normalizeTaskTags(f.Tags)
	f.Priority = taskPriorityForZero(f)
	now := float64(time.Now().Unix())
	_, err := tx.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, session_id, model,
		                    parent_task_id, `+taskParentIDsColumnName+`, tags, payload, source, deliverable, priority, reason,
		                    run_method, run_cmd, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.TaskID, f.Summary, f.Status, f.SessionID, f.Model,
		f.ParentTaskID, f.ParentTaskIDs, f.Tags, f.Payload, f.Source, f.Deliverable, f.Priority, f.Reason,
		strings.TrimSpace(f.RunMethod), strings.TrimSpace(f.RunCmd), now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") ||
			strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("%w: %w", errImportIDTaken, err)
		}
		return fmt.Errorf("insert: %w", err)
	}
	return nil
}

// relabelNodeSuffix maps a node name onto the task_id-safe suffix a
// collision relabel appends (non-alphanumerics → '_', empty → "node"). ONE
// definition: the relabel itself and the idempotency probe that looks for a
// previous relabel must agree on the spelling, or the probe reads an id that
// can never exist (Rule 14).
func relabelNodeSuffix(node string) string {
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, strings.TrimSpace(node))
	if safe == "" {
		safe = "node"
	}
	return safe
}

// taskIDRelabel dedup-proofs a colliding IMPORT id (devdoc/task-id-collision.md):
// two nodes generating t-<unix> in the same second produce distinct work under
// one id — the receiver relabels the incoming row to <id>-n<node> (provenance
// kept in the id itself) and, if even that collides, appends -1..-9. node is
// sanitized (non-alphanumerics → '_', empty → "node"). Returns the new id and
// whether it differs from the input.
func taskIDRelabel(collidingID, node string, exists func(string) (bool, error)) (string, bool, error) {
	safe := relabelNodeSuffix(node)
	base := collidingID + taskIDRelabelSep + safe
	for attempt := 0; ; attempt++ {
		cand := base
		if attempt > 0 {
			cand = base + "-" + strconv.Itoa(attempt)
		}
		taken, err := exists(cand)
		if err != nil {
			return "", false, err
		}
		if !taken {
			return cand, cand != collidingID, nil
		}
		if attempt >= taskIDMaxAttempts {
			return "", false, fmt.Errorf("task relabel: could not generate a unique task_id from %q after %d attempts", collidingID, taskIDMaxAttempts)
		}
	}
}

// syncTaskByID loads one ledger row (nil = no such task_id).
func syncTaskByID(taskID string) (*Task, error) {
	db, closeDB, err := taskSyncDB()
	if err != nil {
		return nil, err
	}
	defer closeDB()
	return taskByID(context.Background(), db, taskID)
}

// importTaskTags merges the row's own tags with the forced from-node tag
// (mechanic ④ — the file's tags describe the work; the channel describes
// the path).
func importTaskTags(rowTags []string, fromTag string) string {
	merged := []string{fromTag}
	for _, t := range rowTags {
		if t = strings.TrimSpace(t); t != "" && t != fromTag {
			merged = append(merged, t)
		}
	}
	return normalizeTaskTags(strings.Join(merged, ","))
}

// syncTaskLiveDedupHit is mechanic ①'s content-ladder probe: it reports
// whether a LIVE local row (pending/claimed/in_progress) carries work the
// CONTENT LADDER calls a twin — exact (summary, payload) equality FIRST,
// then the M4.8 canonical hash, then trigram-Jaccard similarity (the epic's
// leak ① timestamp/path embeds and leak ② one-word summary edits — a plain
// SQL equality probe misses both). Terminal rows don't count: the work
// already happened, and a genuinely new attempt is new work.
//
// It is a WARNING, never a verdict (01a0fea1 identity narrowing): identity
// is the task_id, so the caller imports the row under its OWN id and only
// REPORTS the content twin. Returns the gate that fired (syncTaskGateExact |
// syncTaskGateCanonical | syncTaskGateSimilar), the live twin's task_id, and
// hit.
func syncTaskLiveDedupHitTx(ctx context.Context, tx *sql.Tx, ownID, summary, payload string) (bool, string, string, error) {
	// Fetch only the live rows' text columns — the similarity compare runs
	// in Go (no SQLite extension), bounded by the scan cap (Rule 1).
	rows, err := tx.QueryContext(ctx,
		`SELECT task_id, summary, COALESCE(payload,'') FROM tasks
		 WHERE status IN (?, ?, ?)
		 ORDER BY id ASC LIMIT ?`,
		taskStatusPending, taskStatusClaimed, taskStatusInProgress,
		syncTaskDedupScanMaxRows)
	if err != nil {
		return false, "", "", fmt.Errorf("live dedup probe: %w", err)
	}
	defer rows.Close()
	var live []liveDedupRow
	for rows.Next() {
		var lr liveDedupRow
		if err := rows.Scan(&lr.taskID, &lr.summary, &lr.payload); err != nil {
			return false, "", "", fmt.Errorf("live dedup probe: %w", err)
		}
		if ownID != "" && lr.taskID == ownID {
			continue // the row itself is not its own twin (identity is the id)
		}
		live = append(live, lr)
	}
	if err := rows.Err(); err != nil {
		return false, "", "", fmt.Errorf("live dedup probe: %w", err)
	}
	gate, twin, ok := syncTaskDedupDecide(liveDedupRow{summary: summary, payload: payload}, live)
	return ok, gate, twin, nil
}

// syncTaskDedupDecide is the decision core shared by the live probe and the
// audit scan: exact → canonical → similarity, one ladder. It returns the
// hit's gate name, the candidate's task_id ("" for in-patch rows), and ok.
func syncTaskDedupDecide(incoming liveDedupRow, candidates []liveDedupRow) (string, string, bool) {
	if syncTaskCanonText(incoming.summary) == "" {
		return "", "", false
	}
	incomingHash := syncTaskWorkHash(incoming.summary, incoming.payload)
	incomingCanon := syncTaskCanonHash(incoming.summary, incoming.payload)
	for _, c := range candidates {
		if syncTaskWorkHash(c.summary, c.payload) == incomingHash {
			return syncTaskGateExact, c.taskID, true
		}
		if syncTaskCanonHash(c.summary, c.payload) == incomingCanon {
			return syncTaskGateCanonical, c.taskID, true
		}
		if syncTaskCanonNearPair(incoming, c) {
			return syncTaskGateSimilar, c.taskID, true
		}
	}
	return "", "", false
}

// liveDedupRow is one row fed to the dedup ladder (taskID identifies the
// live twin; patch rows carry their loop index).
type liveDedupRow struct {
	taskID, summary, payload string
}

// ---- M4.8 ③ dedup-scan audit -----------------------------------------------
//
// The import gates stop the NEXT patch; the audit answers "did a PA
// double-submit already land two live copies on THIS ledger?" — the twin
// rows a timestamp/path embed or a one-word summary edit smuggled past the
// exact-hash era. It re-derives every live row's canonical fingerprint and
// walks the pairwise ladder (exact → canonical → similar), reporting each
// twin pair with the gate that caught it. Report-only: the audit never
// mutates rows (which twin is the work is a human/PA ruling — completed
// beats cancelled, and deletion is not a scan-side effect).

// DedupScanPair is one twin pair the audit found (deterministic order:
// older task_id first, so "A vs B" always reads the same way).
type DedupScanPair struct {
	TaskIDA string `json:"task_id_a"`
	TaskIDB string `json:"task_id_b"`
	Gate    string `json:"gate"` // exact | canonical | similar
	Score   string `json:"score,omitempty"`
}

// DedupScanStats is the audit report.
type DedupScanStats struct {
	Scanned int             `json:"scanned"`
	Pairs   []DedupScanPair `json:"pairs"`
}

// TaskDedupScan audits the live ledger for twin work rows (the epic's ③:
// post-import dedup audit, cross-node hash + similarity). All statuses are
// scanned — a terminal twin still documents that the same work ran twice
// (the token waste the epic warns about); --live (ScanLiveOnly) narrows the
// audit to the pending/claimed/in_progress rows the dispatcher could still
// claim twice.
func TaskDedupScan(dbPath string, liveOnly bool) (*DedupScanStats, error) {
	if dbPath == "" {
		dbPath = memoryDBPath()
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("dedup-scan: %w", err)
	}
	defer db.Close()

	query := `SELECT task_id, summary, COALESCE(payload,'') FROM tasks`
	args := []any{syncTaskDedupScanMaxRows}
	if liveOnly {
		query += ` WHERE status IN (?, ?, ?)`
		args = []any{taskStatusPending, taskStatusClaimed, taskStatusInProgress, syncTaskDedupScanMaxRows}
	}
	query += ` ORDER BY created_at ASC, id ASC LIMIT ?`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("dedup-scan: %w", err)
	}
	defer rows.Close()
	var ledger []liveDedupRow
	for rows.Next() {
		var lr liveDedupRow
		if err := rows.Scan(&lr.taskID, &lr.summary, &lr.payload); err != nil {
			return nil, fmt.Errorf("dedup-scan: %w", err)
		}
		ledger = append(ledger, lr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dedup-scan: %w", err)
	}

	stats := &DedupScanStats{Scanned: len(ledger), Pairs: []DedupScanPair{}}
	seenPair := map[string]bool{} // i<j key — each pair once
	for i := range ledger {
		if syncTaskCanonText(ledger[i].summary) == "" {
			continue
		}
		// Pairwise ladder against the LATER rows only (j > i): the earlier
		// rows already covered pairs among themselves.
		for j := i + 1; j < len(ledger); j++ {
			gate, _, ok := syncTaskDedupDecide(ledger[i], ledger[j:j+1])
			if !ok {
				continue
			}
			a, b := ledger[i].taskID, ledger[j].taskID
			if a == b {
				continue
			}
			seenPair[a+"\x00"+b] = true
			stats.Pairs = append(stats.Pairs, DedupScanPair{TaskIDA: a, TaskIDB: b, Gate: gate})
		}
	}
	return stats, nil
}

// importTaskFreshTx queues a NEW row INSIDE the import tx (mechanic ②: the
// patch's task_id is preserved verbatim). The duplicate gates already ran in
// the SyncTaskImport loop — this is the plain enqueue. A lost task_id race is
// reported as RowsAlready and carries no error: the winner's committed row IS
// the work, so the ledger already holds exactly one copy (or will the moment
// the winner commits).
func importTaskFreshTx(ctx context.Context, tx *sql.Tx, r SyncTaskRow, tags string,
	stats *SyncTaskImportStats) error {
	// Mechanic ② — preserve the exporter's task_id (zpeer t-* traceability).
	// M4.11: sync imports are re-submissions on the receiving node — the tags
	// derivation applies there too (fresh node = no legacy priority to
	// respect; rows keep their derived tier).
	source := r.Source
	if source == "" {
		// A row replicated by the sync layer is the resident NODE machinery's
		// record, not the receiving host's own authorship (the operator's
		// vocabulary: server).
		source = tasksource.Server
	}
	// The DEPENDENCY EDGE travels with the row, and it is judged HERE for the
	// same reason the harvest insert and the mirror insert judge it: Rule 33
	// parks a pending row forever on a parent this ledger cannot honour
	// (dangling, or a verdict that does not release), and that parked row is
	// unclaimable with no repair path. Every parent is checked through the ONE
	// judgement (syncParentVerdict), so a multi-parent set is never
	// half-honoured; a refused edge REFUSES THE ROW by name rather than
	// writing the same row with the edge silently dropped — the exact silent
	// loss this channel shipped with (the patch carried no parent key at all
	// and the child arrived claimable, with exit=0 and an empty stderr).
	//
	// The parents are written through taskParentsForWrite, the ONE writer-side
	// split (parent 1 → parent_task_id, parents 2..N → parent_task_ids), so an
	// older peer reading only the first column still sees a parked row.
	parentRows := taskParentIDsOf(r.ParentTaskID, r.ParentTaskIDs)
	for _, parentID := range parentRows {
		if reason, bad := syncParentUnresolvable(parentID, syncRowParentStatusOn(ctx, tx)); bad {
			return importRefusal("%s", reason)
		}
	}
	parentFirst, parentRest := taskParentsForWrite(parentRows)
	err := importTaskInsertTx(ctx, tx, taskCreateFields{
		TaskID:        r.TaskID,
		Summary:       r.Summary,
		Payload:       r.Payload,
		Tags:          tags,
		Source:        source,
		Status:        taskStatusPending,
		Priority:      taskTagPriority(tags),
		ParentTaskID:  parentFirst,
		ParentTaskIDs: parentRest,
	})
	if err != nil {
		if errors.Is(err, errImportIDTaken) {
			stats.RowsAlready++
			return nil
		}
		return err
	}
	stats.RowsImported++
	return nil
}

// syncTaskForwardable reports whether a patch row's status may forward-
// migrate an existing pending row (mechanic ③'s gate — cancelled excluded).
func syncTaskForwardable(status string) bool {
	switch status {
	case taskStatusCompleted, taskStatusFailed, taskStatusVerified, taskStatusSuperseded:
		return true
	}
	return false
}

// importTaskAdoptParentsTx lands the incoming row's dependency EDGE onto an
// EXISTING local row — the repair arm of this channel, in both of its shapes.
//
// Why it is needed at all: the file channel shipped for months without naming
// the parent columns, so every row it replicated arrived with both columns
// empty and the child claimable at once. Re-sending that row now carries the
// edge, but the row already exists on the receiver, so the insert path never
// runs — without this arm the repair would only ever reach rows the receiver
// had never seen, and the ledger would stay silently unparented for exactly
// the rows the defect already corrupted.
//
// TWO SHAPES, because a parent set is a SET fact and "already has a parent" is
// not "is current":
//
//	[empty local]  adopt the whole remote set       → RowsParented
//	[wider remote] append the edges this row lacks  → RowsParentsWidened
//
// The second shape is the same defect one level down, and it was live on this
// channel until now: a child carrying P1 whose peer copy carries {P1, P2}
// released the moment P1 finished and stayed claimable while P2 was live. The
// classification is NOT re-derived here — it is `syncDeltaParentSetRelation`,
// the ONE classifier the delta channel uses, so the two transports cannot
// disagree about what "the remote set adds an edge" means (Rule 14). Its name
// carries the channel it was first written for, not the only channel it judges.
//
// A SUBSET remote set writes NOTHING (a stale peer can never TAKE an edge
// away), and a DISJOINT pair is COUNTED and reported rather than silently
// unioned: neither node declared that pair, and landing the union would be this
// leg inventing a dependency graph (the delta channel's ruling, same reason).
// Every ADDED parent is judged through the ONE verdict (syncParentVerdict) so a
// set is never half-honoured, and a judgement that refuses is REPORTED rather
// than written (Rule 33: an unresolvable parent parks a child forever).
func importTaskAdoptParentsTx(ctx context.Context, tx *sql.Tx, existing *Task, r SyncTaskRow, stats *SyncTaskImportStats) error {
	remoteParents := taskParentIDsOf(r.ParentTaskID, r.ParentTaskIDs)
	if len(remoteParents) == 0 {
		return nil
	}
	localParents := taskParentIDsOf(existing.ParentTaskID, existing.ParentTaskIDs)
	emptyLocal := len(localParents) == 0
	if !emptyLocal {
		switch syncDeltaParentSetRelation(localParents, remoteParents) {
		case syncParentSetNoNewEdge:
			return nil
		case syncParentSetDisjoint:
			// Counted, never silent, never applied — see the doc comment.
			stats.RowsParentsConflict++
			stats.Errors = append(stats.Errors, fmt.Sprintf(
				"row %s: the peer's parent set (%s) and this ledger's (%s) each hold an edge the other lacks — the local set is KEPT and nothing is unioned (neither node declared that pair)",
				r.TaskID, strings.Join(remoteParents, taskParentIDsSeparator), strings.Join(localParents, taskParentIDsSeparator)))
			return nil
		}
	}
	for _, parentID := range remoteParents {
		if !emptyLocal && syncParentSetContains(localParents, parentID) {
			continue // an edge already on the row is not re-judged
		}
		if reason, bad := syncParentUnresolvable(parentID, syncRowParentStatusOn(ctx, tx)); bad {
			stats.Errors = append(stats.Errors, fmt.Sprintf(
				"row %s: the peer's dependency edge was NOT adopted onto the existing row — %s", r.TaskID, reason))
			return nil
		}
	}
	writeSet := remoteParents
	if !emptyLocal {
		writeSet = syncParentSetUnion(localParents, remoteParents)
	}
	first, rest := taskParentsForWrite(writeSet)
	// The guard differs by shape, and so does its meaning: the empty-local arm
	// must not overwrite an edge a concurrent writer landed, while the widen arm
	// must not overwrite a DIFFERENT set a concurrent writer landed. Both are
	// expressed as "the row still looks like the copy I classified".
	guard := `WHERE task_id = ? AND IFNULL(parent_task_id,'') = '' AND IFNULL(` + taskParentIDsColumnName + `,'') = ''`
	if !emptyLocal {
		guard = `WHERE task_id = ? AND IFNULL(parent_task_id,'') = ? AND IFNULL(` + taskParentIDsColumnName + `,'') = ?`
	}
	args := []any{first, rest, existing.TaskID}
	if !emptyLocal {
		// The Task's two parent fields are read verbatim from the two columns
		// (see Task.ParentTaskIDs), so this compare-and-set needs no rendering.
		args = append(args, existing.ParentTaskID, existing.ParentTaskIDs)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE tasks SET parent_task_id = ?, `+taskParentIDsColumnName+` = ? `+guard, args...)
	if err != nil {
		return fmt.Errorf("adopt parent edge: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("adopt parent edge: %w", err)
	}
	if affected == 0 {
		// The row moved between the read and the write: nothing was overwritten
		// and nothing is owed — but the caller must be able to tell this from
		// "the edge landed", so it is not silent (Rule 15).
		stats.Errors = append(stats.Errors, fmt.Sprintf(
			"row %s: the peer's dependency edge was not adopted — this ledger's row's edge changed concurrently (nothing overwritten)", r.TaskID))
		return nil
	}
	if emptyLocal {
		stats.RowsParented++
		stats.Errors = append(stats.Errors, fmt.Sprintf(
			"row %s: dependency edge adopted onto the existing row (%s) — the peer's copy carried the edge this ledger lacked",
			r.TaskID, strings.Join(remoteParents, taskParentIDsSeparator)))
		return nil
	}
	stats.RowsParentsWidened++
	stats.Errors = append(stats.Errors, fmt.Sprintf(
		"row %s: parent set WIDENED on the existing row (%s → %s) — the peer's copy carried edges this ledger lacked, and dropping them would have made the child claimable while they were live",
		r.TaskID, strings.Join(localParents, taskParentIDsSeparator), strings.Join(writeSet, taskParentIDsSeparator)))
	return nil
}

// importTaskExisting handles a task_id collision without ever rewriting the
// row's data (mechanic ⑤ — the nick hard rule):
//   - live patch row + SAME work: tags union ONLY / already (idempotent
//     re-import — the pre-collision semantics, unchanged). "Same work" is
//     the SUMMARY (epic 01a0fea1 ③): the payload is the briefing's free-text
//     restatement, so an edited payload under one task_id is the SAME row,
//     never a relaunch.
//   - live patch row + DIFFERENT summary (the two-nodes-same-second
//     collision, devdoc/task-id-collision.md): the incoming row is
//     RELABELED <id>-n<node> and enqueued as new work — the HP bug report
//     under a colliding t-* id is never silently absorbed as "already".
//   - terminal patch row + pending local row: forward-migrate (mechanic ③,
//     the reverse sync-back pending → completed|failed|verified). A
//     terminal row with a DIFFERENT summary is refused, not relabeled — a
//     relabeled ghost completion would fabricate a finished history. Its
//     PAYLOAD may differ freely: that is the same work re-worded.
//   - anything else (backward move, non-forwardable status, non-pending
//     local row): reported already with the reason.
func importTaskExistingTx(ctx context.Context, tx *sql.Tx, existing *Task, r SyncTaskRow, tags string,
	stats *SyncTaskImportStats, node string, lifecycle **taskNotifyBus) error {
	// The EDGE arrives with the row even when the ROW is already here: a local
	// copy that carries no parent at all has no opinion to defend, so the
	// peer's edge is adopted onto it (the repair arm for every row replicated
	// before this channel carried parents — the same rule the delta adopt arm
	// follows, guarded on the local value still being empty so a concurrent
	// edge is never clobbered). An edge this ledger cannot honour is REPORTED
	// and not written (Rule 33 parks such a row forever, Rule 15 forbids
	// swallowing the refusal).
	if err := importTaskAdoptParentsTx(ctx, tx, existing, r, stats); err != nil {
		return err
	}
	if r.Status == "" {
		// Same work under the same id = the idempotent re-import path.
		if sameWorkIdentity(r.Summary, existing.Summary) {
			if existing.Status != taskStatusPending {
				stats.RowsAlready++
				return nil
			}
			if tagsEqual(existing.Tags, tags) {
				stats.RowsAlready++ // idempotent re-import, nothing to union
				return nil
			}
			if err := taskTagsUnionTx(ctx, tx, existing.TaskID, existing.Tags, tags); err != nil {
				return err
			}
			recordImportTagUnion(existing.TaskID, lifecycle)
			stats.RowsTagged++
			return nil
		}
		// DIFFERENT work under the same id — the silent-loss bug: an
		// existing live local row must not absorb the incoming row (mechanic
		// ⑤), and the incoming row must not be dropped. Relabel + enqueue.
		// The content ladder already ran (and only WARNED), so a collision
		// here is judged on the SUMMARY — the identity axis. Two rows with
		// the same summary and different payloads are the SAME work and
		// took the branch above; only a genuinely divergent definition
		// reaches the relabel.
		if existing.Status != taskStatusPending {
			stats.RowsAlready++ // a live/terminal local row owns the id; report, never rewrite
			return nil
		}
		// Idempotency of the collision itself: a PREVIOUS import of this
		// very patch already relabeled the row to <id>-n<node> and queued
		// it. Forking again (→ <id>-n<node>-1) would multiply one colliding
		// row into a new copy per re-import, so a relabeled twin carrying
		// the SAME work definition is absorbed as an idempotent re-import
		// instead.
		prev, err := taskByIDTx(ctx, tx, r.TaskID+taskIDRelabelSep+relabelNodeSuffix(node))
		if err != nil {
			return err
		}
		if prev != nil && sameWorkIdentity(prev.Summary, r.Summary) {
			stats.RowsAlready++
			return nil
		}
		newID, _, err := taskIDRelabel(r.TaskID, node, func(id string) (bool, error) {
			row, err := taskByIDTx(ctx, tx, id)
			return row != nil, err
		})
		if err != nil {
			return err
		}
		relabeled := r
		relabeled.TaskID = newID
		if err := importTaskFreshTx(ctx, tx, relabeled, tags, stats); err != nil {
			return err
		}
		stats.RowsRelabeled++
		stats.Errors = append(stats.Errors,
			fmt.Sprintf("relabeled %s → %s: same task_id, different work (summary) — new work queued, original row untouched", r.TaskID, newID))
		return nil
	}
	if !syncTaskForwardable(r.Status) {
		// A cancelled patch row: the intent to abandon travels, but the
		// RULING on a terminal collision is completed-wins (the epic's
		// completed-vs-cancelled ruling — work is never dropped, the nick
		// hard rule). See terminalConflictRuling below.
		return terminalConflictRulingTx(ctx, tx, existing, r, tags, stats, lifecycle)
	}
	if existing.Status == r.Status {
		stats.RowsAlready++ // idempotent re-import of the same sync-back
		return nil
	}
	// M4.20-B: the v1 file-channel merge goes through the RANK LADDER too
	// — a terminal patch row meeting a live local row is a forward move
	// (rank 3 > rank ≤2), NOT a "live execution protection" refusal. The
	// start-point atomicity (claim-broadcast M4.18 + worker CAS) makes the
	// terminal verdict the single-runner truth; a locally live row whose
	// peer already delivered terminal is a crashed runner's residue. The
	// rank ladder is shared with the v2 delta channel (one adjudication).
	if syncTaskForwardable(r.Status) && existing.Status != taskStatusPending {
		// M4.20-B rank advance over a live row. If the local row was
		// cancelled, that intent is provenance — it survives the ruling as
		// the cancel-was-intent tag (the nick hard rule: the cancel was
		// only intent, work is never dropped, and audits see WHY).
		if existing.Status == taskStatusCancelled {
			if err := taskTagsUnionTx(ctx, tx, existing.TaskID, existing.Tags,
				syncTaskCancelIntentTag); err != nil {
				return err
			}
		}
		if err := syncTaskMoveStatusTx(ctx, tx, existing.TaskID, existing.Status, r.Status, node); err != nil {
			return err
		}
		recordImportStatusMove(existing.TaskID, existing.Status, r.Status, lifecycle)
		stats.RowsMigrated++
		return nil
	}
	if existing.Status != taskStatusPending {
		if existing.Status == taskStatusCancelled {
			// Terminal-vs-terminal divergence: local was cancelled (the
			// intent to abandon), the patch carries finished work
			// (completed|failed|verified). The ruling is COMPLETED WINS —
			// work is never dropped (the nick hard rule); the cancel was
			// only intent, and it stays on the row as a tag.
			if err := taskTagsUnionTx(ctx, tx, existing.TaskID, existing.Tags,
				syncTaskCancelIntentTag); err != nil {
				return err
			}
			if err := syncTaskMoveStatusTx(ctx, tx, existing.TaskID,
				taskStatusCancelled, r.Status, node); err != nil {
				return err
			}
			recordImportStatusMove(existing.TaskID, taskStatusCancelled, r.Status, lifecycle)
			stats.RowsMigrated++
			return nil
		}
		return importRefusal("cannot migrate to %s: local row is %s (live execution outranks the remote terminal report)", r.Status, existing.Status)
	}
	// A terminal sync-back whose WORK DEFINITION differs from the local
	// pending row: the id collision means the remote finished a DIFFERENT
	// task — migrating this local row would fabricate its completion.
	// Refused loudly (explicit error line), never relabeled into a ghost
	// terminal. The SUMMARY is the definition (epic 01a0fea1 ③); a payload
	// that differs is the same brief re-worded and moves the row forward
	// exactly as before.
	if !sameWorkIdentity(r.Summary, existing.Summary) {
		return importRefusal("terminal sync-back for %s carries DIFFERENT work (summary) than the local pending row — refusing to fabricate a completion; resolve the id collision manually", r.TaskID)
	}
	if err := syncTaskMoveStatusTx(ctx, tx, existing.TaskID, taskStatusPending, r.Status, node); err != nil {
		return err
	}
	recordImportStatusMove(existing.TaskID, taskStatusPending, r.Status, lifecycle)
	stats.RowsMigrated++
	return nil
}

// recordImportStatusMove / recordImportTagUnion buffer the notifications the
// importer's mutations owe, for flush PAST the commit.
//
// Status moves are ONE per row (the import never moves a row twice), so the
// bus holds at most a patch's worth of row-addressed moves.
//
// A tag-union notification is DEFERRED the same way and uses the LIVE row
// status on BOTH sides: the union does not move a row, but the operator's
// subscriber is keyed on a status, and a union only ever happens to a pending
// row (the pending-filtered gate above) — so prev==status==pending, which the
// bus itself drops as a non-move. The call is kept because it is the correct
// statement of intent (nothing to announce) and costs one comparison.
func recordImportStatusMove(taskID, prevStatus, status string, lifecycle **taskNotifyBus) {
	if *lifecycle == nil {
		*lifecycle = &taskNotifyBus{}
	}
	(*lifecycle).observeIDs(taskID, prevStatus, status)
}

// recordImportTagUnion (see recordImportStatusMove) — the union variant.
func recordImportTagUnion(taskID string, lifecycle **taskNotifyBus) {
	recordImportStatusMove(taskID, taskStatusPending, taskStatusPending, lifecycle)
}

// syncTaskCancelIntentTag marks a row whose cancel was OVERRIDDEN by the
// completed-wins ruling (or whose cancel intent met finished work) — the
// intent is never lost, it is preserved as provenance on the row.
const syncTaskCancelIntentTag = "cancel-was-intent"

// terminalConflictRuling applies the completed-vs-cancelled ruling for a
// cancelled patch row colliding with an existing local row. The principle:
// completed (work exists) outranks cancelled (intent to abandon) — the nick
// hard rule "never drop work". The cancel intent is NOT discarded: it lands
// on the row as the cancel-was-intent tag (provenance, visible in
// devdocs and audits). Matrix:
//
//	local completed/failed/verified + cancelled patch → keep local work,
//	  tag cancel-was-intent … already (nothing applied, intent recorded)
//	local cancelled + cancelled patch → idempotent re-import … already
//	local pending/live + cancelled patch → tagged, row untouched … already
//	  (a pending row's cancel must walk the LOCAL state machine — task
//	  update — not arrive sideways through sync)
func terminalConflictRulingTx(ctx context.Context, tx *sql.Tx, existing *Task, r SyncTaskRow, tags string,
	stats *SyncTaskImportStats, lifecycle **taskNotifyBus) error {
	if existing.Status == taskStatusCancelled && tagsEqual(existing.Tags, tags) {
		stats.RowsAlready++ // idempotent re-import of the same cancel
		return nil
	}
	// The intent to cancel is recorded on ANY non-pending row it meets
	// (terminal rows AND pending rows alike — provenance is cheap and
	// never wrong); the row itself is only ever untouched here.
	if err := taskTagsUnionTx(ctx, tx, existing.TaskID, existing.Tags,
		syncTaskCancelIntentTag); err != nil {
		return err
	}
	recordImportTagUnion(existing.TaskID, lifecycle)
	if existing.Status != taskStatusPending {
		stats.RowsAlready++
		return nil
	}
	// Pending local row: the cancel intent lands as a tag; the row stays
	// pending — cancelling local work is the local dispatcher's decision
	// (task update), never a sync-side effect.
	stats.RowsAlready++
	return nil
}

// syncStatusMoveRecord appends the AUDIT trail entry a SYNC-BORNE status move
// owes (epic 01a187f4): the event name is the row's new status (the local
// dispatcher's convention — TaskUpdate writes exactly that), the detail carries
// the LEG, the PEER whose truth arrived, and the move itself.
//
// WHY THIS EXISTS (measured 2026-10-29, rows 01a16f98/01a17866/01a17a10/
// 01a17a6a/01a17a8a/01a17c53/01a17d07/01a17e23): all three sync legs wrote
// `status` and called taskEventWrite ZERO times, so eight rows sat `cancelled`
// on this host with an EMPTY trail while the peer that cancelled them had one
// event each in the same second. "Who changed this row, and from what?" was
// answerable only from the peer's ledger — the local ledger said nothing. The
// notify stream fired; the audit trail did not, and those are different
// channels with different consumers (Rule 15: a move nobody can attribute is
// a silent write).
//
// IDEMPOTENCE IS THE CALLER'S, ON PURPOSE. Only a branch whose UPDATE really
// changed the status calls this (the rank ladder's n>0, the two v1 writers'
// RowsAffected>0). A replayed sync takes the RowsAlready / echo path and never
// reaches here, so the trail grows by one row per REAL move, not per exchange.
func syncStatusMoveRecord(ctx context.Context, q QueryExecer, leg, peer, taskID, prevStatus, status string) {
	if taskID = taskIDNormalize(taskID); taskID == "" {
		return
	}
	status = strings.TrimSpace(status)
	prevStatus = strings.TrimSpace(prevStatus)
	// A MOVE REQUIRES A DIFFERENCE. Belt to the callers' braces: each caller
	// already only reaches here when its guarded UPDATE changed the row, but
	// "same status" must never append a row whatever a future caller does —
	// a re-sync that appended an event every time would turn the audit trail
	// into noise and make "how often did this move" unanswerable.
	if status == "" || prevStatus == status {
		return
	}
	if peer = strings.TrimSpace(peer); peer == "" {
		peer = syncTaskUnknownNode
	}
	if prevStatus == "" {
		prevStatus = syncStatusPrevUnknown
	}
	taskEventWrite(ctx, q, taskID, status, fmt.Sprintf(syncStatusEventDetailFmt, leg, peer, prevStatus, status))
}

// syncTaskMoveStatusTx moves a row forward into a terminal status INSIDE the
// import tx — the reverse sync-back's writer (mechanic ③). It deliberately does NOT walk
// taskTransitionAllowed: the single-node state machine models a LOCAL
// dispatcher lifecycle (pending→claimed→in_progress→completed), but a
// sync-back row completed on ANOTHER node — its remote walk was legal there;
// this row is catching up to distributed truth. The gates here are stricter
// where it matters: the source must be pending (a claimed/in_progress row is
// live local execution — never clobbered, the nick rule) and the target
// must be forwardable (syncTaskForwardable — cancelled stays local).
func syncTaskMoveStatusTx(ctx context.Context, tx *sql.Tx, taskID, fromStatus, status, peer string) error {
	now := float64(time.Now().Unix())
	res, err := tx.ExecContext(ctx,
		`UPDATE tasks SET status = ?, ended_at = ?, updated_at = ? WHERE task_id = ? AND status = ?`,
		status, now, now, taskID, fromStatus)
	if err != nil {
		return fmt.Errorf("forward migration: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return importRefusal("forward migration: no %s row %s to migrate", fromStatus, taskID)
	}
	// The row's status REALLY moved (the guarded UPDATE landed n>0), so the
	// audit trail owes its entry — the same one the local dispatcher writes
	// (epic 01a187f4). Inside the caller's tx: the event and the move commit
	// together or not at all.
	syncStatusMoveRecord(ctx, tx, syncStatusEventLegImport, peer, taskID, fromStatus, status)
	// D2 — the merge-backlog TERMINAL guard's SYNC face
	// (task_merge_backlog_terminal_guard.go): this raw UPDATE is the write
	// site that produced t-1788218295 (cancelled → verified, empty
	// task_events, while the branch stayed 5 commits ahead of master). The
	// sync layer adopts distributed truth and must not refuse — a refusal
	// here would break bilateral convergence — but a governed write adopted
	// WITHOUT merge proof is recorded on the row's trail, never silent.
	mergeBacklogTerminalRecord(ctx, tx, taskID, status)
	return nil
}

// taskTagsUnionTx adds the newly imported tags to an existing row — the ONLY
// mutation an existing row's content ever receives (mechanic ⑤), run INSIDE
// the import tx.
func taskTagsUnionTx(ctx context.Context, tx *sql.Tx, taskID, existingTags, importedTags string) error {
	merged := normalizeTaskTags(strings.TrimSuffix(existingTags+","+importedTags, ","))
	_, err := tx.ExecContext(ctx,
		`UPDATE tasks SET tags = ?, updated_at = ? WHERE task_id = ?`,
		merged, float64(time.Now().Unix()), taskID)
	if err != nil {
		return fmt.Errorf("tags union: %w", err)
	}
	return nil
}

// tagsEqual compares two comma-separated tag strings as sets.
func tagsEqual(a, b string) bool {
	return stringSetEqual(splitSources(a), splitSources(b))
}

// stringSetEqual reports whether two slices hold the same elements
// (order-insensitive, repetition-insensitive — tags are a set).
func stringSetEqual(a, b []string) bool {
	set := make(map[string]int, len(a))
	for _, v := range a {
		set[v]++
	}
	for _, v := range b {
		set[v]--
		if set[v] < 0 {
			return false
		}
	}
	for _, n := range set {
		if n != 0 {
			return false
		}
	}
	return true
}

// ---- HP harvest (M4.12, PULL direction) ------------------------------------
//
// memsync moved patches through FILES (export → transport → import). Harvest
// is the direct peer-to-peer pull: `gopi task harvest --peer hp` SSHes to the
// peer, runs a timestamp-gated SELECT over ITS tasks ledger, and applies the
// rows locally in ONE transaction. No intermediate file, no manual transport —
// HP assets are harvested onto zpeer even when zpeer's ledger is blank.
//
// Per-row semantics (the epic's six counts):
//   new task_id            → INSERT with the from-<peer> tag … harvested
//   same id, remote newer  → adopt remote truth … migrated
//   same id, remote older  → local already ahead … skipped
//   local terminal row     → never demoted, whatever the remote says … skipped
//                            (终态不回退 — the nick hard rule)
//   same ts, same content  → duped (idempotent re-harvest)
//   same ts, diverging     → conflict (two truths, one stamp — human eyes)
//   row-level error        → failed (+ reason in Errors)
//
// The anchor lives in the sync_state table (one row per peer, created on
// demand): the next harvest SELECTs only rows past the anchor, so a re-run
// over a quiet peer transfers zero rows (the idempotency contract). The
// anchor never advances past a FAILED row — the backoff rewinds it below the
// earliest failure so the next harvest re-fetches those rows instead of
// silently losing them past the watermark.

// Harvest tunables (Rule 1 — every literal in one place).
const (
	// harvestRemoteDBPath is the peer-side ledger path (both nodes keep the
	// tasks ledger inside memory.db).
	harvestRemoteDBPath = "~/.gopi/memory.db"

	// harvestSSHTimeout caps the whole remote SELECT (connect + query).
	harvestSSHTimeout = 30 * time.Second

	// harvestSSHConnectTimeout is ssh's own connect phase cap (the fast
	// "peer is down" failure, independent of query time).
	harvestSSHConnectTimeout = 10

	// harvestStateTable is the anchor table name (created on demand inside
	// the apply transaction).
	harvestStateTable = "sync_state"

	// harvestStateCreate is the anchor table DDL. M4.13: identical SQL to
	// syncStateSchemaTable (tasks.go) — ONE shape, two verbs (harvest
	// applies it inside its tx; ensureTasksSchema ensures it at open so
	// the sync verbs find it even without a harvest first). Kept as a
	// literal because the const block needs a constant (Rule 1 anchor);
	// a drift would be caught by the shared sync_state tests.
	harvestStateCreate = `CREATE TABLE IF NOT EXISTS sync_state (
		peer       TEXT PRIMARY KEY,
		anchor     REAL NOT NULL DEFAULT 0,
		updated_at REAL NOT NULL DEFAULT 0
	)`

	// harvestRemoteSelect is the peer-side query. Quote-free on purpose: the
	// command travels through the peer's shell, and '' literals inside a
	// single-quoted command would terminate the quoting. sqlite3 -json emits
	// JSON null for SQL NULL, which unmarshals into Go zero values ("" / 0),
	// so no COALESCE is needed. updated_at is passed as a float (SQLite REAL
	// seconds); the anchor keeps the exact value the next `>` comparison
	// must exclude, so no row is skipped between runs. %.6f is deliberate:
	// the anchor fed into the SELECT is ALWAYS rewound by
	// harvestAnchorRewind first (skew/truncation margin), so a stamp
	// sharing its second with the watermark is re-fetched next run.
	//
	// EVERY EXPRESSION COLUMN CARRIES `AS <wire field>`, AND THAT IS
	// LOAD-BEARING (measured 2026-10-07 — the window leg's own live run).
	// `sqlite3 -json` names a column after its RESULT name: a bare column
	// keeps its own name, while an EXPRESSION is named by its SOURCE TEXT. So
	// `COALESCE(deliverable,'')` arrived as the key "COALESCE(deliverable,'')"
	// and encoding/json — whose match is case-insensitive but does NOT skip
	// punctuation — bound it to nothing. harvestRemoteRow's deliverable /
	// run_method / run_cmd / reason / parent_task_id were therefore zero on
	// EVERY row this window ever landed: a peer's branch provenance, its run
	// provenance and its dependency EDGE (Rule 33) were dropped in transport,
	// silently — no error, no counter, no log (Rule 15). The alias is what
	// makes the key equal the wire field, so the five names below are not
	// decoration. The column NAMES live once, beside their expressions: the
	// COALESCE'd tail is syncRemoteCoalescedProjection (memory_sync_row.go),
	// shared with the one-row leg, and the edge is syncParentColumnNamed
	// (memory_sync_delta.go) — the expression AS its wire field, so no
	// transport can carry one without the other (Rule 1/13/14).
	// The gate that keeps this honest is positional, not substring:
	// memory_sync_harvest_wire_test.go parses THIS SELECT's result names and
	// compares them, index by index, with harvestRemoteRow's json tags.
	harvestRemoteSelect = `SELECT task_id, summary, payload, tags, source,
		model, status, created_at, updated_at, ended_at, ` + syncRemoteCoalescedProjection + `
		FROM tasks WHERE updated_at > %.6f ORDER BY updated_at ASC, id ASC`

	// harvestTsEpsilon absorbs REAL rounding when comparing timestamps
	// (both sides are SQLite REAL seconds). 1e-6 covers a full microsecond
	// of drift — the representable granularity of unix-epoch REALs at
	// ~1.8e9 magnitude is 2.4e-7s (one ULP), so any stamp differing by
	// less than a microsecond is numerically the same second on at least
	// one side of the transfer.
	harvestTsEpsilon = 1e-6

	// harvestFailedAnchorBackoff rewinds the anchor (in seconds) below the
	// earliest failed row, so failed rows re-fetch on the next harvest.
	// Re-fetched already-applied rows are harmless — every apply path is
	// idempotent.
	harvestFailedAnchorBackoff = 1.0

	// harvestAnchorRewind is the clock-skew margin (seconds) the new
	// anchor is rewound by on EVERY harvest: a peer row updated moments
	// before the anchor was taken (write raced the SELECT, or its REAL
	// stamp carries sub-second precision the %f SELECT format truncated)
	// must be re-fetched next run. Application is idempotent, so the
	// rewind only costs a few duplicate rows.
	harvestAnchorRewind = 5.0
)

// HarvestCounts is the harvest report — the epic's six counters. Errors
// carries one line per failed row (index + reason). The unexported fields
// feed the anchor backoff and never reach JSON.
type HarvestCounts struct {
	Harvested int                `json:"harvested"` // new rows inserted
	Migrated  int                `json:"migrated"`  // same id, remote newer, adopted
	Duped     int                `json:"duped"`     // same id+ts, identical content
	Conflict  int                `json:"conflict"`  // same ts, diverging content
	Skipped   int                `json:"skipped"`   // remote older / local terminal / live-owned
	Failed    int                `json:"failed"`    // row errors
	Relabeled int                `json:"relabeled"` // collision: same id, different work → -n<node>
	Errors    []string           `json:"errors,omitempty"`
	Git       *GitPushbackReport `json:"git,omitempty"` // git push-back domain (nil = disabled)

	sawFailed       bool    // any row failed?
	minFailedUpdate float64 // earliest failed row's updated_at (backoff target)
}

// failedRow notes one row failure in the counts and tracks the earliest
// failed row's stamp (the anchor backoff target).
func (c *HarvestCounts) failedRow(updated float64, format string, args ...any) {
	c.Failed++
	c.sawFailed = true
	if updated > 0 && (c.minFailedUpdate == 0 || updated < c.minFailedUpdate) {
		c.minFailedUpdate = updated
	}
	c.Errors = append(c.Errors, fmt.Sprintf(format, args...))
}

// harvestStatusRank is the cross-node status precedence: terminal truth
// outranks live/pending truth regardless of whose clock stamped it (both
// nodes run the same work to completion — the terminal report IS the result,
// equal stamps can never demote it). Higher rank wins; equal rank compares
// content.
func harvestStatusRank(status string) int {
	switch {
	case terminalTaskStatus(status):
		return 2
	case status == taskStatusClaimed || status == taskStatusInProgress:
		return 1
	default:
		return 0
	}
}

// harvestBlockedLocal re-states the hard ownership rules the same-stamp
// ladder must respect even when the remote row wins the rank comparison: a
// local terminal row is never rewritten (终态不回退), and a live local
// execution is never clobbered by a live-status rewrite (the nick rule).
func harvestBlockedLocal(localStatus, remoteStatus string) bool {
	if terminalTaskStatus(localStatus) {
		return true
	}
	return remoteStatus == taskStatusPending &&
		(localStatus == taskStatusClaimed || localStatus == taskStatusInProgress)
}

// harvestRemoteRow is one row of the peer-side SELECT (sqlite3 -json shape).
// Timestamps are SQLite REAL seconds; ended_at arrives COALESCEd to 0.
type harvestRemoteRow struct {
	TaskID    string  `json:"task_id"`
	Summary   string  `json:"summary"`
	Payload   string  `json:"payload"`
	Tags      string  `json:"tags"`
	Source    string  `json:"source"`
	Model     string  `json:"model"`
	Status    string  `json:"status"`
	CreatedAt float64 `json:"created_at"`
	UpdatedAt float64 `json:"updated_at"`
	EndedAt   float64 `json:"ended_at"`
	// Deliverable rides the v1 SELECT too (M4.18 db-sync HP face): the
	// pull side backfills deliverable.branch/worktree so the server's
	// adopt gate pairs the branch with the completed row. "" on older
	// peers / empty rows.
	Deliverable string `json:"deliverable"`
	// RunMethod + RunCmd ride the harvest wire too (run provenance,
	// devdoc/run-method-provenance.md): a peer's list/show renders the
	// same "how was this run" the origin node answers. "" on older peers
	// (the SELECT COALESCEs, so a schema without the columns still scans).
	RunMethod string `json:"run_method"`
	RunCmd    string `json:"run_cmd"`
	// Reason rides the harvest wire too (task-update-reason epic): the
	// "why is this task in this state" the origin node stamped. "" on
	// older peers (the SELECT COALESCEs, so a schema without the column
	// still scans).
	Reason string `json:"reason"`
	// ParentTaskID is the row's dependency EDGE (Rule 33). It rides this wire
	// for the same reason it rides the delta channel: a row shipped without
	// it arrives as work nobody waits for. "" on older peers — and that
	// absence is refused BY NAME at import when it would have carried an edge
	// this ledger cannot resolve (syncParentUnresolvable), never written as a
	// permanently parked row.
	ParentTaskID string `json:"parent_task_id"`
	// ParentTaskIDs carries parents 2..N (EPIC multi-parent) on the harvest
	// channel: a row harvested without it would arrive with only its first
	// parent, and re-exporting it would then DROP the rest from the peer's
	// view — the same silent edge loss this pair exists to prevent. "" on
	// older peers (the SELECT COALESCEs, so a peer whose schema predates the
	// column still scans).
	ParentTaskIDs string `json:"parent_task_ids"`
}

// deliverableValidForImport reports whether a remote deliverable blob may
// land in the local row (valid KNOWN-version JSON; the v1 transport has no
// version field — anything that parses today stays).
func deliverableValidForImport(remote string) bool {
	remote = strings.TrimSpace(remote)
	return remote != "" && json.Valid([]byte(remote))
}

// HarvestOptions controls one harvest pull.
type HarvestOptions struct {
	// Peer is the ssh alias to pull from (required; e.g. "hp").
	Peer string
	// Since overrides the stored anchor for this run (0 = use the anchor;
	// a peer with no anchor row harvests from 0 = full history). NOTE:
	// when --since is given the anchor is NOT persisted and NOT rewound —
	// it is a one-shot manual window, the stored anchor stays authoritative
	// (verified in TestHarvestRerunQuietPeerIsZero / the E2E runs).
	Since int64
	// DBPath overrides the local ledger ("" = default; CLI --db / tests).
	DBPath string
	// RemoteDB overrides the peer-side ledger path ("" = harvestRemoteDBPath).
	RemoteDB string
	// Fetch replaces the ssh transport (tests inject rows without a peer).
	Fetch func(peer string, anchor float64) ([]harvestRemoteRow, error)
	// GitPushback runs AFTER a committed db sync (the row and its branch
	// arrive as independent idempotent domains; a git failure never
	// poisons the db outcome). nil = the default local-repo pusher. Tests
	// inject a fake to avoid touching git.
	GitPushback func() (*GitPushbackReport, error)
}

// NOTE (2026-10-25): this lane used to carry `harvestPeerRepo = "~/work/GoPi"`
// — one of THREE copies of the peer's checkout path (Rules 1/14) that had all
// drifted from the host's DECLARED delivery point. The ssh peer alias still
// supplies host + user; the PATH half now comes from the ONE resolver
// (peer_repo_path.go), which parses the declaration and announces its named
// fallback (Rule 15).

// GitPushbackReport is the git domain of the harvest output — which local
// branches were pushed to the peer's repo and which failed. Pushing does
// NOT delete or merge anything: merge is the peer's decision (git roles
// are asymmetric — the peer is the integrator).
type GitPushbackReport struct {
	BranchesPushed []string `json:"branches_pushed"`   // reached the peer under their own name
	PushFailed     []string `json:"push_failed"`       // refusals + hard failures (each "<branch>: reason")
	Skipped        []string `json:"skipped,omitempty"` // already-integrated branches NOT re-pushed (resurrection guard)
	// BranchesFastForwarded is the machine-readable half of the peer-ref
	// finding: the branches whose push UPDATED a ref the peer already owned of
	// that name (the peer's tip was contained in ours, so git fast-forwarded
	// it). Their BranchesPushed entry carries the from→to note for a reader;
	// this list lets a caller tell "the peer already owned this name" from
	// "the name was new at the peer" without parsing prose. A NAME COLLISION —
	// the peer's ref neither contained in nor containing ours — is never here:
	// it is refused by name and reported in PushFailed.
	BranchesFastForwarded []string `json:"branches_fast_forwarded,omitempty"`
	// BranchesFastForwardable / BranchesNotFastForwardable split BranchesPushed
	// by the alignment FACT this pass measures and the peer's merger acts on:
	// whether a pushed branch's tip CONTAINS the peer's master (so the peer's
	// `git merge --ff-only <branch>` can land it as delivered) or does not (it
	// is delivered, and parks at the peer until it is aligned). Both kinds are
	// pushed — the fact is reported, not enforced — and a pushed branch appears
	// in exactly one of the two lists. Their BranchesPushed entry carries the
	// same fact in prose for a reader.
	BranchesFastForwardable    []string `json:"branches_fast_forwardable,omitempty"`
	BranchesNotFastForwardable []string `json:"branches_not_fast_forwardable,omitempty"`
}

// gitPushbackDefault walks the LOCAL repo (harvest runs on the branch-rich
// node, e.g. HP) and pushes to the peer ONLY the branches whose tip is NOT
// yet an ancestor of the peer's master (merged-branch-flood fix: the server
// adopts a deliverable into master and then deletes the ref — re-pushing an
// already-integrated branch would resurrect the deletion, the 2026-09-02
// stale-flood incident). Unmerged work (the just-finished task's deliverable
// branch) crosses; fully-integrated branches are skipped. A no-branch /
// nothing-unmerged state pushes nothing. Any per-branch failure is reported,
// not fatal — the db sync outcome is independent of the git domain.
func gitPushbackDefault(peer string) (*GitPushbackReport, error) {
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("harvest git-pushback: %w", err)
	}
	// The repo to walk: harvest runs inside the GoPi checkout, so the
	// process working directory IS the branch-rich repo root.
	repo, err := gitPushbackGitRun(context.Background(), ".", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("harvest git-pushback: resolve repo: %w", err)
	}
	target := fmt.Sprintf("%s:%s", peer, peerRepoPathForRepo(repo))
	flush := gitPushbackPlan(context.Background(), repo, target)
	report := &GitPushbackReport{
		BranchesPushed:             flush.Pushed,
		PushFailed:                 flush.Failed,
		Skipped:                    flush.Skipped,
		BranchesFastForwarded:      flush.FastForwarded,
		BranchesFastForwardable:    flush.FastForwardable,
		BranchesNotFastForwardable: flush.NotFastForwardable,
	}
	if report.BranchesPushed == nil {
		report.BranchesPushed = []string{}
	}
	if report.PushFailed == nil {
		report.PushFailed = []string{}
	}
	return report, nil
}

// gitPushbackRun executes the git push-back phase with the injected-or-
// default pusher; a nil function or a disabled run returns a zero report
// so the CLI always has a git domain to print.
func gitPushbackRun(pushback func() (*GitPushbackReport, error)) (*GitPushbackReport, error) {
	if pushback == nil {
		pushback = func() (*GitPushbackReport, error) { return &GitPushbackReport{}, nil }
	}
	return pushback()
}

// DefaultGitPushback is the CLI-facing default pusher: walk the local repo
// and push to <peer>:~/work/GoPi ONLY branches whose tip is not yet an
// ancestor of the peer's master (merged branches are skipped — see
// gitPushbackDefault). Exported so the CLI wires the real transport while
// tests inject their own.
func DefaultGitPushback(peer string) (*GitPushbackReport, error) {
	return gitPushbackDefault(peer)
}

// harvestRemoteTasks SSHes to the peer and runs the timestamp-gated SELECT,
// returning the JSON-decoded rows. exec.Command (no shell) keeps the peer
// alias and SQL argument injection-safe; BatchMode forbids interactive
// password prompts (a peer without key auth fails fast, it does not hang).
func harvestRemoteTasks(peer, remoteDB string, anchor float64) ([]harvestRemoteRow, error) {
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("harvest: %w", err)
	}
	if remoteDB == "" {
		remoteDB = harvestRemoteDBPath
	}
	ctx, cancel := context.WithTimeout(context.Background(), harvestSSHTimeout)
	defer cancel()
	query := fmt.Sprintf(`%s -json %s %s`, syncRemoteSQLiteBinary, remoteDB,
		harvestShellQuote(fmt.Sprintf(harvestRemoteSelect, anchor)))
	out, err := exec.CommandContext(ctx, "ssh",
		"-o", fmt.Sprintf("ConnectTimeout=%d", harvestSSHConnectTimeout),
		"-o", "BatchMode=yes", peer, query).Output()
	if err != nil {
		return nil, fmt.Errorf("harvest: ssh %s: %w", peer, err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return []harvestRemoteRow{}, nil // 0 rows — sqlite3 -json prints nothing
	}
	var rows []harvestRemoteRow
	if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
		return nil, fmt.Errorf("harvest: peer %s returned unparsable JSON: %w", peer, err)
	}
	return rows, nil
}

// harvestShellQuote single-quotes a string for the peer's shell (the whole
// SELECT travels as ONE argv element of the ssh command line — the peer
// re-joins argv with spaces and runs it through sh; an embedded ' must be
// spelled &apos;\&apos;&apos; to survive).
func harvestShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TaskHarvest pulls the peer's task rows past the anchor and applies them in
// ONE local transaction; the anchor advances inside the same commit (the
// epic's ①–⑤: remote SELECT → JSON transport → single-tx apply → anchor →
// counts). Git push-back runs AFTER the committed db pull (order asserted
// by TestHarvestGitPushbackRunsAfterCommit): the report's git domain joins
// the six counters. Counting and mutation live in
// harvestReconcile so a decision and its effect can never drift apart.
func TaskHarvest(opts HarvestOptions) (*HarvestCounts, error) {
	peer := strings.TrimSpace(opts.Peer)
	if peer == "" {
		return nil, fmt.Errorf("harvest: --peer is required (the ssh alias to pull from)")
	}
	// The DECLARED-TOPOLOGY gate (peer_topology.go, Rule 14: ONE definition
	// of the rule and its wording) — the same precondition every sibling
	// peer-taking verb states, and the one this pull was missing: measured
	// 2026-09-23 against a settings.json declaring sync_peers ["a-peer"],
	//
	//	$ gopi task harvest --peer not-declared
	//	gopi task harvest error: harvest: ssh not-declared: exit status 2
	//	[exit=1, and the recording ssh shim recorded ONE leg]
	//
	// — the operator was shown ssh's exit code instead of the condition, and
	// an undeclared alias was dialled. Harvest WRITES (it commits the peer's
	// rows into this ledger) and then pushes local branches back, so a
	// topology typo on this verb is the most expensive of the family. The
	// gate precedes the transport, the local DB open and the git push-back.
	if err := RequireDeclaredPeer(peer, nil); err != nil {
		return nil, fmt.Errorf("harvest: %w", err)
	}
	fetch := opts.Fetch
	if fetch == nil {
		fetch = func(p string, anchor float64) ([]harvestRemoteRow, error) {
			return harvestRemoteTasks(p, opts.RemoteDB, anchor)
		}
	}
	dbPath := opts.DBPath
	if dbPath == "" {
		dbPath = memoryDBPath()
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("harvest: %w", err)
	}
	defer db.Close()

	// Notify bus (task-notify-terminal-gap): every row this pull moves is
	// observed here and delivered only after the transaction below commits.
	bus := &taskNotifyBus{}
	defer taskNotifyFlushDeferred(bus)()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("harvest: begin: %w", err)
	}
	defer tx.Rollback()
	anchor := float64(opts.Since)
	if opts.Since == 0 {
		if anchor, err = harvestAnchorLoad(ctx, tx, peer); err != nil {
			return nil, fmt.Errorf("harvest: %w", err)
		}
	}
	rows, err := fetch(peer, anchor)
	if err != nil {
		return nil, err
	}
	node := peer
	if node == "" {
		node = syncTaskUnknownNode
	}
	counts, err := harvestApplyTx(ctx, tx, bus, rows, peer, node)
	if err != nil {
		return nil, fmt.Errorf("harvest: %w", err)
	}
	// Anchor advance: the max(updated_at) seen — never past a FAILED row
	// (the backoff rewinds below the earliest failure; re-fetched applied
	// rows are absorbed idempotently).
	newAnchor := anchor
	for _, r := range rows {
		if r.UpdatedAt > newAnchor {
			newAnchor = r.UpdatedAt
		}
	}
	if counts.sawFailed && counts.minFailedUpdate-harvestFailedAnchorBackoff < newAnchor {
		newAnchor = counts.minFailedUpdate - harvestFailedAnchorBackoff
	}
	// Skew rewind: never park the anchor at the newest stamp seen — rows
	// committed in the same wall-clock second (or truncated by the %f
	// SELECT format) would fall BEHIND the anchor and never re-fetch.
	// Idempotent apply makes the re-fetched rows free.
	if newAnchor > anchor && newAnchor-anchor < harvestAnchorRewind {
		newAnchor = anchor
	} else if newAnchor > anchor {
		newAnchor -= harvestAnchorRewind
	}
	if opts.Since == 0 && (newAnchor != anchor || len(rows) > 0) {
		if err := harvestAnchorSave(ctx, tx, peer, newAnchor); err != nil {
			return nil, fmt.Errorf("harvest: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("harvest: commit: %w", err)
	}
	// Git push-back AFTER a committed db sync: the report joins the counts
	// (branches_pushed / push_failed). A git failure never poisons the db
	// outcome — the domains are independent, and both are idempotent.
	if opts.GitPushback != nil {
		git, err := opts.GitPushback()
		if err != nil {
			return counts, fmt.Errorf("harvest: git push-back: %w", err)
		}
		counts.Git = git
	}
	return counts, nil
}

// harvestAnchorLoad reads the peer's stored anchor (0 = never harvested).
func harvestAnchorLoad(ctx context.Context, tx *sql.Tx, peer string) (float64, error) {
	if _, err := tx.ExecContext(ctx, harvestStateCreate); err != nil {
		return 0, fmt.Errorf("ensure %s: %w", harvestStateTable, err)
	}
	var anchor float64
	err := tx.QueryRowContext(ctx,
		`SELECT anchor FROM sync_state WHERE peer = ?`, peer).Scan(&anchor)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return anchor, err
}

// harvestAnchorSave upserts the peer's anchor inside the apply transaction
// (the epic's step ④ — only a committed transaction can move it).
func harvestAnchorSave(ctx context.Context, tx *sql.Tx, peer string, anchor float64) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO sync_state (peer, anchor, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(peer) DO UPDATE SET anchor = excluded.anchor,
			updated_at = excluded.updated_at`,
		peer, anchor, float64(time.Now().Unix()))
	if err != nil {
		return fmt.Errorf("save anchor: %w", err)
	}
	return nil
}

// harvestApplyTx applies fetched peer rows inside the caller's transaction.
// Live remote statuses (pending/claimed/in_progress) do not travel as live
// (the M4.8 rule): a live row inserts as pending; terminal rows carry their
// status and ended_at. Every branch is idempotent — a re-run re-decides.
func harvestApplyTx(ctx context.Context, tx *sql.Tx, bus *taskNotifyBus, rows []harvestRemoteRow, peer, node string) (*HarvestCounts, error) {
	counts := &HarvestCounts{}
	fromTag := SyncTaskTagPrefix + peer
	seen := map[string]bool{}
	for i, r := range rows {
		r.TaskID = strings.TrimSpace(r.TaskID)
		r.Summary = strings.TrimSpace(r.Summary)
		if r.TaskID == "" || r.Summary == "" {
			counts.failedRow(0, "row %d: empty task_id or summary", i)
			continue
		}
		if r.Status != "" && !validTaskStatus(r.Status) {
			// Validate the RAW status first — the pending coercion below
			// would otherwise swallow garbage into a "valid" live row.
			counts.failedRow(r.UpdatedAt, "row %d (%s): invalid status %q", i, r.TaskID, r.Status)
			continue
		}
		status := r.Status
		// remoteLive is computed from the RAW status BEFORE coercion — the
		// coercion below overwrites status, and the live provenance must
		// survive it (the marker is what reconcile compares ranks with).
		remoteLive := status == taskStatusClaimed || status == taskStatusInProgress
		if !terminalTaskStatus(status) {
			status = taskStatusPending // execution state never travels
		}
		// A remote live row (claimed/in_progress) means the PEER owns the
		// execution — importing it as pending would let a local runner
		// double-claim it, and rewriting a live local twin to pending
		// would violate the same rule. So the live origin is carried
		// through the reconcile as `remoteLive` (a marker, not a status):
		// at equal stamps a live local row and a remoteLive twin are the
		// SAME rank (both "work not yet finished") — an identical twin
		// absorbs (duped), a diverging one conflicts, and a remote
		// terminal row (stamp+rank) still wins later. A live row never
		// INSERTs as live (pending on import).
		if remoteLive {
			status = taskStatusPending
		}
		if seen[r.TaskID] {
			counts.Duped++ // in-batch repeat (the SELECT never emits one)
			continue
		}
		seen[r.TaskID] = true

		var local harvestLocalRow
		err := tx.QueryRowContext(ctx,
			`SELECT summary, COALESCE(payload,''), COALESCE(tags,''),
			        status, updated_at, COALESCE(ended_at,0) FROM tasks WHERE task_id = ?`,
			r.TaskID).Scan(&local.summary, &local.payload, &local.tags,
			&local.status, &local.updated, &local.ended)
		switch {
		case err == sql.ErrNoRows:
			if err := harvestInsert(ctx, tx, r, status, fromTag); err != nil {
				counts.failedRow(r.UpdatedAt, "row %d (%s): %v", i, r.TaskID, err)
				continue
			}
			counts.Harvested++
		case err != nil:
			counts.failedRow(r.UpdatedAt, "row %d (%s): probe: %v", i, r.TaskID, err)
		default:
			harvestReconcile(ctx, tx, bus, counts, r, status, remoteLive, fromTag, local, node)
		}
	}
	return counts, nil
}

// harvestSameWorkVerdict is the TERMINAL-rank half of the same-stamp
// equality test: same task_id, same summary, and the same VERDICT — but the
// row is not a byte twin (terminal ranks compare the ended-at stamp and the
// tag set, which is how two nodes that both finished one shared row can
// still be told apart from two nodes holding different finished truths).
//
// The payload is deliberately absent from BOTH halves (epic 01a0fea1 ③).
func harvestSameWorkVerdict(r harvestRemoteRow, status, fromTag string, local harvestLocalRow) bool {
	if !harvestSameContent(r, status, fromTag, local) {
		return false
	}
	return harvestTsEqual(r.EndedAt, local.ended)
}

// harvestLocalRow is the probed local counterpart (the reconcile input).
type harvestLocalRow struct {
	summary, payload, tags, status string
	updated                        float64
	ended                          float64
}

// harvestReconcile decides one same-task_id row and, when it wins, writes it
// — count and mutation in one function so the decision and its effect can
// never drift apart (the "same decision, two callsites" bug class).
// The node name rides along for the collision relabel (different work under
// one id → the incoming row lands as <id>-n<node>, never silently absorbed).
func harvestReconcile(ctx context.Context, tx *sql.Tx, bus *taskNotifyBus, counts *HarvestCounts,
	r harvestRemoteRow, status string, remoteLive bool, fromTag string, local harvestLocalRow, node string) {
	// Same id, DIFFERENT work (devdoc/task-id-collision.md): the two-nodes-
	// same-second collision. The incoming live row is relabeled and queued —
	// a conflict line would leave HP's new work stranded on the peer. Only a
	// locally-pending row relabels: a protected local state (terminal — 终态
	// 不回退 / live-owned — the nick rule) keeps the status ladder, a remote
	// TERMINAL report for a different task is refused (inserting a ghost
	// completion fabricates history), and a remoteLive twin keeps the ladder
	// (the peer is executing it — queuing it here invites double execution).
	//   The gate is the SUMMARY — the work definition (epic 01a0fea1 ③). A
	// row whose payload was re-worded under the same task_id and summary is
	// the SAME work and falls through to the stamp ladder below, where it is
	// absorbed (duped) instead of forking a relabeled copy of itself.
	if !remoteLive && status == taskStatusPending &&
		local.status == taskStatusPending &&
		!sameWorkIdentity(r.Summary, local.summary) {
		newID, _, err := taskIDRelabel(r.TaskID, node, func(id string) (bool, error) {
			var one int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE task_id = ?`, id).Scan(&one)
			if err == sql.ErrNoRows {
				return false, nil
			}
			return err == nil, err
		})
		if err != nil {
			counts.failedRow(r.UpdatedAt, "row (%s): relabel: %v", r.TaskID, err)
			return
		}
		relabeled := r
		relabeled.TaskID = newID
		if err := harvestInsert(ctx, tx, relabeled, status, fromTag); err != nil {
			counts.failedRow(r.UpdatedAt, "row (%s): relabel insert: %v", r.TaskID, err)
			return
		}
		counts.Relabeled++
		counts.Errors = append(counts.Errors,
			fmt.Sprintf("relabeled %s → %s: same task_id, different work — new work queued, original row untouched", r.TaskID, newID))
		return
	}
	if harvestTsEqual(r.UpdatedAt, local.updated) {
		// Same stamp: the status ladder decides first (terminal > live >
		// pending — the terminal report is the work's RESULT and outranks
		// the other side's not-yet-finished row, whatever the clock said;
		// this is what makes same-work rows planted on both nodes converge
		// instead of piling up as conflicts). A remoteLive twin counts as
		// the live rank even though its status coerced to pending.
		remoteRank := harvestStatusRank(status)
		if remoteLive && remoteRank == 0 {
			remoteRank = 1 // the peer held claimed/in_progress — live truth
		}
		switch {
		case remoteRank > harvestStatusRank(local.status) &&
			!harvestBlockedLocal(local.status, status):
			if err := harvestUpdate(ctx, tx, bus, local.status, r, status, fromTag, node); err != nil {
				counts.failedRow(r.UpdatedAt, "row (%s): %v", r.TaskID, err)
				return
			}
			counts.Migrated++
		case remoteRank > harvestStatusRank(local.status):
			// Terminal outranks the local live row but the nick rule blocks
			// the rewrite of a locally-owned run — unless the content is
			// identical (the twin-in-flight case: the shared row's own
			// completion result).
			if harvestSameContent(r, status, fromTag, local) {
				if err := harvestUpdate(ctx, tx, bus, local.status, r, status, fromTag, node); err != nil {
					counts.failedRow(r.UpdatedAt, "row (%s): %v", r.TaskID, err)
					return
				}
				counts.Migrated++
			} else {
				counts.Skipped++ // divergent remote claim on a locally-owned run
			}
		case remoteRank == harvestStatusRank(local.status) &&
			harvestSameWorkVerdict(r, status, fromTag, local):
			// Same work at the same stamp — absorbed (duped), never a
			// conflict. For equal LIVE ranks the ended-at stamps are both
			// absent, so this is the plain "both sides hold the row live"
			// absorb; for equal TERMINAL ranks it additionally requires the
			// same finished verdict.
			counts.Duped++
		case remoteRank == harvestStatusRank(local.status):
			// Equal stamp + equal rank + a genuinely different work
			// DEFINITION or verdict: the remaining conflict class is two
			// FINISHED truths that disagree (completed vs failed) — the
			// content axes (payload) are no longer part of the verdict
			// (epic 01a0fea1 ③), so an edited payload no longer parks a
			// row here for a human to adjudicate.
			counts.Conflict++
		default:
			counts.Skipped++ // local already ahead on the ladder
		}
		return
	}
	// Remote strictly newer. 终态不回退: a local terminal row is finished
	// work — the remote report never demotes it.
	if terminalTaskStatus(local.status) {
		counts.Skipped++
		return
	}
	// A live local execution owned by this node is protected from
	// live-status rewrites (the nick rule): the remote twin arrives
	// coerced to pending, and pending must never pull a live row back
	// down the ladder. A remote TERMINAL row (the run's result) is NOT a
	// live rewrite — it lands via harvestUpdate below, migrating the live
	// row to its terminal truth (the twin-in-flight completion, bug038).
	if status == taskStatusPending {
		counts.Skipped++
		return
	}
	// Same-id content gate (task-id-collision epic, DEFECTS 1+4, audit
	// 2026-09-04): this strictly-newer path carries a remote TERMINAL row
	// (status != pending after the guard above). A terminal report for a
	// DIFFERENT task under the SAME task_id (the two-nodes-same-second
	// collision) must never rewrite the local row — whether the local row
	// is a LIVE execution this node owns (DEFECT 1: a silent overwrite of
	// a live run's content by the peer's different work) or a PENDING row
	// (DEFECT 4: fabricating a completion for work that never ran — the
	// exact shape the v1 import channel refuses, "refusing to fabricate a
	// completion"). The equal-stamp ladder and the pending-collision
	// relabel both gate on harvestSameContent; this branch is the only
	// terminal adoption that never content-checked. Gate on the work
	// definition (summary — the immutable identity; the payload is the
	// briefing's free-text restatement and the harness routinely varies
	// it across nodes for the SAME work), leave the row untouched, and
	// record the refusal like the import channel does.
	if !sameWorkIdentity(r.Summary, local.summary) {
		counts.Skipped++
		counts.Errors = append(counts.Errors, fmt.Sprintf(
			"refused %s: remote %s report carries DIFFERENT work (summary %q vs local %q) — same task_id id collision; local row left untouched (no silent overwrite / no fabricated completion)", r.TaskID, status, r.Summary, local.summary))
		return
	}
	if err := harvestUpdate(ctx, tx, bus, local.status, r, status, fromTag, node); err != nil {
		counts.failedRow(r.UpdatedAt, "row (%s): %v", r.TaskID, err)
		return
	}
	counts.Migrated++
}

// harvestInsert queues a brand-new peer row (the TaskSubmit INSERT shape:
// same columns, same defaults — empty session, empty deliverable). The forced
// from-<peer> tag is the provenance (decided at import, the peer file is
// never trusted). A terminal row with no remote ended_at still gets one
// (local now) — a finished row never shows "(not ended)".
//
// The dependency EDGE travels in this lane too: a row that arrives through
// the anchor window carries its parent_task_id, and the edge is written only
// when this ledger can honour it (syncParentUnresolvable) — otherwise the
// row is refused by name, because Rule 33 parks an unresolvable edge forever.
func harvestInsert(ctx context.Context, tx *sql.Tx, r harvestRemoteRow, status, fromTag string) error {
	ended := r.EndedAt
	if ended <= 0 && terminalTaskStatus(status) {
		ended = float64(time.Now().Unix())
	}
	// M4.11: harvested rows derive priority from their tags — a peer's bug
	// row queues as a bug here too.
	deliverable := ""
	if deliverableValidForImport(r.Deliverable) {
		deliverable = strings.TrimSpace(r.Deliverable)
	}
	// EVERY parent the harvested row carries is judged the same way, through
	// the ONE reader, so a set is never half-honoured (Rule 33: an unresolvable
	// parent is a permanent park with no repair path).
	for _, parentID := range taskParentIDsOf(r.ParentTaskID, r.ParentTaskIDs) {
		if reason, bad := syncParentUnresolvable(parentID, syncRowParentStatusOn(ctx, tx)); bad {
			return importRefusal("%s", reason)
		}
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, session_id, model,
		                    parent_task_id, `+taskParentIDsColumnName+`, tags, payload, source, deliverable,
		                    priority, run_method, run_cmd, reason, created_at, updated_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.Summary, status, "", r.Model, strings.TrimSpace(r.ParentTaskID),
		strings.TrimSpace(r.ParentTaskIDs),
		localTagsWith(fromTag, r.Tags), r.Payload, r.Source,
		deliverable,
		taskTagPriority(localTagsWith(fromTag, r.Tags)), r.RunMethod, r.RunCmd, r.Reason,
		r.CreatedAt, r.UpdatedAt, ended)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	return nil
}

// harvestUpdate adopts the remote truth into the local row: work definition,
// status, provenance tags, ended_at, updated_at. Model is kept when the
// remote row carries none. The WHERE clause re-checks non-terminality — even
// mid-transaction a terminal row is never rewritten (终态不回退, enforced at
// the writer too).
func harvestUpdate(ctx context.Context, tx *sql.Tx, bus *taskNotifyBus, prevStatus string, r harvestRemoteRow, status, fromTag, peer string) error {
	ended := r.EndedAt
	if ended <= 0 && terminalTaskStatus(status) {
		ended = float64(time.Now().Unix())
	}
	// M4.18 db-sync: the deliverable backfills when the local blob is empty
	// or a submit-time repo-only PLACEHOLDER (first-truth-wins never rewrites
	// a full blob — but a placeholder must not block the executor's remote
	// completion blob {repo,branch,worktree}; epic task-submit-repo-default,
	// t-1788441090, made placeholders the norm on pending rows). The remote
	// blob is only written when it is valid AND the local row is empty or a
	// repo-only placeholder.
	remoteDeliverable := strings.TrimSpace(r.Deliverable)
	deliverableNew := "" // only applied when the local blob is upgradeable
	if remoteDeliverable != "" && deliverableValidForImport(remoteDeliverable) {
		var localDeliverable string
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(deliverable,'') FROM tasks WHERE task_id = ?`, r.TaskID).Scan(&localDeliverable); err != nil {
			return fmt.Errorf("apply: probe deliverable: %w", err)
		}
		localDeliverable = strings.TrimSpace(localDeliverable)
		if localDeliverable == "" || deliverableRepoOnly(localDeliverable) {
			deliverableNew = remoteDeliverable
		}
	}
	// The dependency EDGE backfills the same way the deliverable does: a local
	// row with no edge and a peer copy that has one gains the edge (the pair
	// only became orderable once the column existed — every row harvested
	// before this change is a repair candidate). It is refused BY NAME when
	// this ledger cannot honour it (Rule 33 parks an unresolvable edge
	// forever). The write itself rides the UPDATE below, guarded on the local
	// value still being empty so a concurrent edge is never clobbered.
	parentNew, parentIDsNew := "", ""
	if len(taskParentIDsOf(r.ParentTaskID, r.ParentTaskIDs)) > 0 {
		var localParent, localParentIDs string
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(parent_task_id,''), COALESCE(`+taskParentIDsColumnName+`,'') FROM tasks WHERE task_id = ?`,
			r.TaskID).Scan(&localParent, &localParentIDs); err != nil {
			return fmt.Errorf("apply: probe parent: %w", err)
		}
		// The set is adopted ONLY while the local row carries no parents at
		// all: an edge is never clobbered, and a partially-filled local set
		// (a row synced by a build that understood only parent 1) is NOT
		// completed here — attribute-level merging of two ledgers' parent sets
		// would be a scheduling decision this leg must not make.
		if len(taskParentIDsOf(localParent, localParentIDs)) == 0 {
			for _, parentID := range taskParentIDsOf(r.ParentTaskID, r.ParentTaskIDs) {
				reason, bad := syncParentUnresolvable(parentID, syncRowParentStatusOn(ctx, tx))
				if bad {
					return importRefusal("%s", reason)
				}
			}
			parentNew, parentIDsNew = taskParentsForWrite(taskParentIDsOf(r.ParentTaskID, r.ParentTaskIDs))
		}
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE tasks SET status = ?, payload = ?, tags = ?,
		        model = COALESCE(NULLIF(?, ''), model),
		        deliverable = CASE WHEN ? != '' THEN ? ELSE deliverable END,
		        parent_task_id = CASE WHEN ? != '' THEN ? ELSE COALESCE(parent_task_id,'') END,
		        `+taskParentIDsColumnName+` = CASE WHEN ? != '' THEN ? ELSE COALESCE(`+taskParentIDsColumnName+`,'') END,
		        run_method = COALESCE(NULLIF(?, ''), run_method),
		        run_cmd = COALESCE(NULLIF(?, ''), run_cmd),
		        reason = COALESCE(NULLIF(?, ''), reason),
		        ended_at = CASE WHEN ? > 0 THEN ? ELSE ended_at END,
		        updated_at = ?
		 WHERE task_id = ? AND status NOT IN (?, ?, ?)`,
		status, r.Payload, localTagsWith(fromTag, r.Tags), r.Model,
		deliverableNew, deliverableNew,
		parentNew, parentNew,
		parentIDsNew, parentIDsNew,
		r.RunMethod, r.RunCmd,
		r.Reason,
		ended, ended, r.UpdatedAt, r.TaskID,
		taskStatusCompleted, taskStatusFailed, taskStatusVerified, taskStatusSuperseded)
	if err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return fmt.Errorf("apply: row vanished or turned terminal mid-transaction")
	}
	// The harvest leg's audit entry (epic 01a187f4): the UPDATE really moved
	// the row (n>0), so the trail owes what the local dispatcher always wrote
	// and this leg never did — WHO moved it (the peer) and FROM WHAT. The
	// writer's own prev==status guard keeps a same-status adopt (the
	// identical-twin path) from appending noise.
	syncStatusMoveRecord(ctx, tx, syncStatusEventLegHarvest, peer, r.TaskID, prevStatus, status)
	// Transition hook (task-notify-terminal-gap): the row moved in the
	// ledger. The observation is buffered on the caller's bus and delivered
	// only after the surrounding transaction commits — a rolled-back apply
	// must never announce a status the row does not have.
	bus.observeIDs(r.TaskID, prevStatus, status)
	return nil
}

// localTagsWith prefixes the forced from-tag onto the row's own tags.
func localTagsWith(fromTag, rowTags string) string {
	merged := []string{fromTag}
	for _, t := range splitSources(rowTags) {
		if t != "" && t != fromTag {
			merged = append(merged, t)
		}
	}
	return normalizeTaskTags(strings.Join(merged, ","))
}

// harvestTsEqual compares two SQLite REAL stamps with the shared epsilon.
func harvestTsEqual(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= harvestTsEpsilon
}

// sameWorkIdentity is the task-id-collision content gate's comparator
// (DEFECTS 1-4, audit 2026-09-04): does a remote row under a colliding
// task_id carry the SAME work as the local row? The work identity is the
// SUMMARY — the task definition. Divergent summary = two genuinely
// different submitted tasks under one id = the collision every channel
// must refuse/relabel rather than silently absorb.
//
// The PAYLOAD is deliberately NOT part of this test (epic 01a0fea1 ③): it
// is the briefing's free-text restatement and the harness and real peers
// routinely rewrite it across nodes for the SAME work, so treating an
// edited payload as "different work" relabeled/refused the very row the
// operator had only re-worded.
func sameWorkIdentity(remoteSummary, localSummary string) bool {
	return strings.TrimSpace(remoteSummary) == strings.TrimSpace(localSummary)
}

// recordContentWarn notes a CONTENT-LADDER hit as a WARNING: the statistics
// and the report line both carry it, and NOTHING is skipped or refused for
// it (epic 01a0fea1 ③ — identity is the task_id). RowsDuped is incremented
// alongside RowsContentWarn so the existing CLI tally line keeps reporting
// the twin count it always reported; the honest name is RowsContentWarn.
// Rule 15: the degradation is announced, never silent.
func recordContentWarn(stats *SyncTaskImportStats, format string, args ...any) {
	stats.RowsDuped++
	stats.RowsContentWarn++
	stats.Errors = append(stats.Errors, fmt.Sprintf(format, args...))
}

// harvestSameContent reports whether the remote row and the local row carry
// the same work at the same stamp (the duped-vs-conflict discriminator).
// Tags compare as sets with the channel tag (from-<peer>) dropped — the
// channel describes the path, not the work.
//
// The PAYLOAD is NOT part of this comparison (epic 01a0fea1 ③): identity is
// the task_id, and the payload is the briefing's free-text restatement that
// both nodes routinely rewrite for the SAME work — comparing it here made
// "the same task with an edited payload" read as a relaunch (relabel /
// conflict) instead of the same row.
func harvestSameContent(r harvestRemoteRow, status, fromTag string, local harvestLocalRow) bool {
	localStatus := local.status
	// The remote live twin arrives coerced to pending — compare it against
	// a live local row as the same rank ("work not yet finished" truth, the
	// distributed claim state itself never travels).
	if harvestStatusRank(status) == 0 && harvestStatusRank(localStatus) == 1 {
		localStatus = status
	}
	if r.Summary != local.summary || status != localStatus {
		return false
	}
	own := make([]string, 0, 8)
	for _, t := range splitSources(local.tags) {
		if t != fromTag {
			own = append(own, t)
		}
	}
	return tagsEqual(strings.Join(own, ","), r.Tags)
}

// importTaskMirror inserts a TERMINAL remote row as a mirror-history row
// (M4.20-B bilateral convergence): status carried as-is (never pending —
// the anti-double-run invariant holds because the row never passes through
// the runnable set), summary/payload/tags/anchors copied from the wire row.
func importTaskMirrorTx(ctx context.Context, tx *sql.Tx, r SyncTaskRow, tags string,
	stats *SyncTaskImportStats) error {
	summary := strings.TrimSpace(r.Summary)
	if summary == "" {
		summary = syncUnknownSummary
	}
	createdAt := float64(r.CreatedAt)
	if createdAt <= 0 {
		createdAt = float64(r.UpdatedAt)
	}
	endedAt := float64(r.UpdatedAt)
	if r.EndedAt > 0 {
		endedAt = float64(r.EndedAt)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tasks
			(task_id, summary, status, session_id, model, parent_task_id, `+taskParentIDsColumnName+`, tags, payload,
			 source, run_method, run_cmd, reason, created_at, updated_at, ended_at)
		 VALUES (?, ?, ?, '', '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, summary, r.Status, strings.TrimSpace(r.ParentTaskID), strings.TrimSpace(r.ParentTaskIDs), tags, r.Payload, tasksource.MirrorTerminal,
		r.RunMethod, r.RunCmd, r.Reason,
		createdAt, float64(r.UpdatedAt), endedAt); err != nil {
		return fmt.Errorf("mirror insert: %w", err)
	}
	stats.RowsImported++
	return nil
}
