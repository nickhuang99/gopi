// Package tools — memory_sync.go: node→node knowledge-base sync (M4).
//
// Design (devdoc/memroadmap-m4.md, 方案③ git-PR semantics):
//
//	export: HP runs `gopi memory memsync --export --since <unix>` → a JSON patch file
//	        containing ONLY its own brew rows (sources tag gate), each row
//	        tagged sources=node:hp.
//	import: peer1 runs `gopi memory memsync --import patch.json` → every row goes
//	        through MemoryStore — the SAME dedup (exact + trigram-similarity)
//	        and merge (sources union) path that brew/LLM writes use. A row
//	        already present is merged (sources only), never duplicated; a
//	        conflicting human/agent row wins and the brew patch row is
//	        reported deduped.
//
// Idempotency: re-importing the same patch is a no-op — dedup hits, sources
// merge is a set-union. Node marking: sources carry node:<host> tags so
// provenance survives merges (splitSources keeps them distinct).
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	gopiversion "github.com/nickhuang99/gopi/internal/version"
)

// Sync tunables (Rule 1: every literal in one place).
const (
	// SyncSourcePrefix marks rows exported by the sync channel. Import
	// re-tags every patch row's sources with node:<hostname> (forced, not
	// merged) so provenance is deterministic regardless of what the
	// exporting node's rows carried.
	SyncNodeTagPrefix = "node:"

	// syncExportSourceFilter is the SOURCE TAG a local row must carry to be
	// exported. "" (the default) = NO source filter — every row whose
	// sources do NOT carry a node:* tag travels (the anti-reflow guard —
	// sync never re-exports rows imported from another node). The wide
	// default is deliberate (2026-09-09 epic: knowledge assets = brew +
	// hermes-import + empty-source early-dev rows — the brew-only default
	// leaked ~1095 shared rows); an explicit --source TAG narrows the
	// export to one tag (e.g. memsync --export --source brew keeps the
	// historical brew-only scope).
	syncExportSourceFilter = ""

	// syncJSONIndent is the patch file's pretty-print indent.
	syncJSONIndent = "  "
)

// SyncRow is one patch row — the distilled-QA fields MemoryStore consumes.
// Sources is NOT carried: the import path forces node:<exporter> onto every
// row it applies (provenance is decided at import, not trusted from the file).
type SyncRow struct {
	Topic      string   `json:"topic"`
	Question   string   `json:"question"`
	Answer     string   `json:"answer"`
	Keywords   []string `json:"keywords,omitempty"`
	StoredUnix int64    `json:"stored_unix,omitempty"`
	// Origin is the row's provenance pointer (epic 自动记忆, item 5/5).
	//
	// It rides as ONE OPTIONAL OBJECT rather than three flat keys, and that
	// shape IS the compatibility channel: the five keys above stay
	// byte-identical, so an OLDER receiver's encoding/json drops the unknown
	// `origin` key and loses provenance — never the row. Three flat keys would
	// have been just as silent to an old reader, but they would also have put
	// three new strings into every patch's row shape, where a hand-edited or
	// half-implemented sender could produce a HALF pointer. One object has
	// exactly one meaning: present (a pointer travelled) or absent (it did not).
	//
	// nil and the zero SyncRowOrigin BOTH mean "nothing travelled" and both read
	// back as the zero MemoryOrigin — the state of a row written before item
	// 2/5, a row that recorded nothing, or a patch from an older sender.
	// memory_sync_origin.go owns the value, the conversion, and the sentence a
	// receiver renders when the pointer is not reachable from this node.
	Origin *SyncRowOrigin `json:"origin,omitempty"`
}

// SyncPatch is the export file format (versioned for future evolution).
type SyncPatch struct {
	Version    int    `json:"version"`
	Node       string `json:"node"`
	ExportedAt int64  `json:"exported_at"`
	// Generator identifies the exporting binary (VCS revision, "dev" on
	// unstamped builds). Informational — import never gates on it; it
	// exists so a refusing import can point at which node's build to
	// upgrade (epic: peer1 新 HP 旧 版本错位排障).
	Generator string `json:"generator,omitempty"`
	// ImportStats is the PEER's tally of what it just imported from the
	// patch this node sent — a REPLY-only report field (the peer's serve
	// handler fills it in; an export patch always leaves it nil). It is
	// omitempty and NOT a data field: an older initiator drops it silently
	// and loses a report number, never a row — which is why it needs no
	// format-version bump. A reader must treat its ABSENCE as UNKNOWN, never
	// as zero (the periodic leg's ImportKnown does exactly that).
	ImportStats *SyncImportTally `json:"import_stats,omitempty"`
	Rows        []SyncRow        `json:"rows"`
}

// syncPatchVersion is the current patch format version.
const syncPatchVersion = 1

// syncCheckPatchVersion is the shared import-side format gate (epic: 三节点
// 二进制版本不一致是常态 — 新字段的 patch 被旧二进制读时 Go 的 json 会静默
// 丢弃未知字段, 数据不完整且无报错; 版本数字是唯一可靠的防线). Semantics:
//
//	got == want → nil (current format, fully understood)
//	got >  want → error advising THIS node's binary upgrade (the patch was
//	              produced by a newer gopi — importing it would silently
//	              drop the fields this binary cannot see)
//	got <  want → error advising the PEER's upgrade (defensive: an older
//	              format than the binary's own should not circulate)
//	got == 0    → "missing version" (hand-written or future-format patch)
//	got <  0    → invalid (mangled patch)
func syncCheckPatchVersion(kind string, got, want int) error {
	switch {
	case got == want:
		return nil
	case got < 0:
		return fmt.Errorf("memsync %s: invalid patch version %d (want %d) — refusing", kind, got, want)
	case got == 0:
		return fmt.Errorf("memsync %s: patch is missing its version field (want %d) — refusing: an unknown-format patch would be imported with silently dropped fields", kind, want)
	case got > want:
		return fmt.Errorf("memsync %s: patch format v%d is NEWER than this binary supports (v%d) — refusing import to avoid silent field loss; upgrade this node's gopi binary", kind, got, want)
	default:
		return fmt.Errorf("memsync %s: patch format v%d is older than this binary supports (v%d) — refusing; the exporting node's gopi is outdated, upgrade it", kind, got, want)
	}
}

// syncGeneratorVersion stamps the exporting binary's identity into the patch
// header (informational only — import never gates on it). Falls back to the
// VCS revision the toolchain embeds (go >= 1.18), then "dev", so every patch
// can be traced to the source that produced it.
func syncGeneratorVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case gopiversion.BuildInfoKeyRevision:
			rev = s.Value
		case gopiversion.BuildInfoKeyModified:
			dirty = s.Value == gopiversion.BuildInfoValueModifiedTrue
		}
	}
	if rev == "" {
		return "dev" // no VCS info (e.g. built from a tarball)
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		return rev + "-dirty"
	}
	return rev
}

// SyncExportOptions controls the export side.
type SyncExportOptions struct {
	// DBPath overrides the knowledge base ("" = the default path —
	// memoryDBPath semantics). Required for tests and CLI --db.
	DBPath string
	// SinceUnix exports only rows distilled after this time (0 = all).
	SinceUnix int64
	// Source narrows the export to rows carrying this source tag ("" = all
	// local non-node rows — the wide default: brew + hermes-import +
	// empty-source early-dev rows all travel, node:* never).
	Source string
}

// SyncExportStats reports what the export produced.
type SyncExportStats struct {
	RowsExported int
	Scanned      int
	SkippedTag   int
	SkippedSince int
}

// SyncExport this node's knowledge rows into a PR-style patch file. The
// local DB is opened read-only for row selection; the patch carries no ids —
// identity is re-established on import through MemoryStore's dedup keys.
func SyncExport(opts SyncExportOptions) (*SyncPatch, *SyncExportStats, error) {
	dbPath := opts.DBPath
	if dbPath == "" {
		dbPath = memoryDBPath()
	}
	source := opts.Source
	if source == "" {
		source = syncExportSourceFilter
	}
	ctx := context.Background()
	db, err := openStoreDB(ctx, dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("memsync export: %w", err)
	}
	defer db.Close()

	// Which provenance columns this DB's distilled_qa actually declares. The
	// projection below is built from this probe, so an OLD or FOREIGN schema
	// (pre-item-2/5) exports with the three columns ABSENT instead of failing
	// on `no such column` — the compatibility channel. A probe failure is
	// reported, never swallowed (Rule 15).
	presentOriginCols, err := syncOriginColumnsPresent(ctx, db)
	if err != nil {
		return nil, nil, fmt.Errorf("memsync export: %w", err)
	}

	// Local knowledge rows only, and never rows already carrying a node:*
	// tag — the channel stays one-directional (imported rows are not
	// re-exported, no ping-pong loops). The source-tag filter is optional:
	// no tag (the wide default) = every non-node local row travels; a tag
	// narrows to rows carrying it. Either way node:* rows never export.
	selectSQL := `
		SELECT COALESCE(topic,''), COALESCE(question,''), COALESCE(answer,''),
		       COALESCE(search_kw,''), COALESCE(distilled_at,0)` +
		syncOriginSelectSep + syncOriginColumnExpr(presentOriginCols) + `
		FROM distilled_qa
		WHERE (',' || COALESCE(sources,'') || ',') NOT LIKE '%,node:%'
		  AND COALESCE(question,'') != '' AND COALESCE(answer,'') != ''`
	var args []any
	if source != "" {
		selectSQL += ` AND (',' || COALESCE(sources,'') || ',') LIKE ?`
		args = append(args, "%,"+source+",%")
	}
	selectSQL += ` ORDER BY id`
	rows, err := db.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("memsync export: %w", err)
	}
	defer rows.Close()

	hostname, _ := os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
	patch := &SyncPatch{Version: syncPatchVersion, Node: hostname, ExportedAt: time.Now().Unix(), Generator: syncGeneratorVersion()}
	stats := &SyncExportStats{}
	for rows.Next() {
		var r SyncRow
		var searchKW string
		var distilledAt float64 // SQLite REAL — scan into float, cast below
		var wireOrigin SyncRowOrigin
		if err := rows.Scan(&r.Topic, &r.Question, &r.Answer, &searchKW, &distilledAt,
			&wireOrigin.SessionID, &wireOrigin.Round, &wireOrigin.Host); err != nil {
			return nil, nil, fmt.Errorf("memsync export: scan: %w", err)
		}
		r.StoredUnix = int64(distilledAt)
		// The provenance pointer travels with the row (item 5/5). A row that
		// recorded nothing, or a DB whose schema predates the three columns,
		// produces an EMPTY pointer here — originOfWire turns that into nil, so
		// the JSON key is omitted rather than sent as an empty object an older
		// receiver would have to interpret.
		r.Origin = originOfWire(MemoryOrigin{
			SessionID: wireOrigin.SessionID,
			Round:     wireOrigin.Round,
			Host:      wireOrigin.Host,
		})
		// search_kw holds the comma-joined keywords (MemoryStore wrote them
		// there) — split back into the patch's keyword list.
		for _, kw := range strings.Split(searchKW, ",") {
			if kw = strings.TrimSpace(kw); kw != "" {
				r.Keywords = append(r.Keywords, kw)
			}
		}
		stats.Scanned++
		if opts.SinceUnix > 0 && r.StoredUnix > 0 && r.StoredUnix < opts.SinceUnix {
			stats.SkippedSince++
			continue
		}
		patch.Rows = append(patch.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("memsync export: %w", err)
	}
	if patch.Rows == nil {
		patch.Rows = []SyncRow{} // JSON: [] not null — empty export stays valid
	}
	return patch, stats, nil
}

// WriteSyncPatch serializes the patch to a JSON file (pretty-printed).
func WriteSyncPatch(patch *SyncPatch, outPath string) error {
	data, err := json.MarshalIndent(patch, "", syncJSONIndent)
	if err != nil {
		return fmt.Errorf("memsync: marshal: %w", err)
	}
	if err := os.WriteFile(outPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("memsync: write %s: %w", outPath, err)
	}
	return nil
}

// SyncImportStats reports the import outcome per row class.
type SyncImportStats struct {
	RowsImported int // new rows stored
	RowsDeduped  int // merged into an existing row (idempotent re-import)
	// RowsDedupedDiffering is the subset of RowsDeduped whose BODY was NOT the
	// body of the row it merged into: the exact-key fold kept the existing row's
	// knowledge and the arrival's text was never stored. Those rows are inside
	// RowsDeduped (the fold is unchanged); this count exists so the operator can
	// tell an idempotent re-import (0 here) from a run that silently dropped
	// content (Rule 15 — the fold was never supposed to be invisible).
	RowsDedupedDiffering int
	RowsRefused          int // per-row semantic refusals (garbage gate, validation, human-wins, ...)
	Errors               []string
}

// SyncImport applies a patch file through the store core — the identical
// write path brew uses, so dedup, merge and the garbage gate all apply.
// Sources on every imported row are FORCED to node:<hostname of importing
// side is NOT used>: the tag records the EXPORTING node (patch.Node), so
// the importing DB's rows say where the knowledge came from.
//
// Transaction boundary (foresee-instead-of-fix): the WHOLE patch is applied
// inside ONE BEGIN IMMEDIATE transaction — taken at BEGIN (openDB sets
// _txlock=immediate), so a concurrent writer queues on busy_timeout before
// any row is written. Either every accepted row lands or none does: a
// mid-patch hard DB error (SQLITE_BUSY after the timeout, disk I/O, FTS
// corruption) rolls the patch back and returns an error — never a silent
// partial import dressed up as per-row refusals. Per-row semantic refusals
// (garbage gate, validation, human-wins) still count as RowsRefused inside
// the committed transaction; they are decisions, not failures.
//
// Re-importing the same patch twice is a no-op: the second pass dedups into
// the first pass's rows and the sources union adds nothing.
func SyncImport(patchPath string) (*SyncImportStats, error) {
	data, err := os.ReadFile(patchPath)
	if err != nil {
		return nil, fmt.Errorf("memsync import: read %s: %w", patchPath, err)
	}
	var patch SyncPatch
	if err := json.Unmarshal(data, &patch); err != nil {
		return nil, fmt.Errorf("memsync import: parse %s: %w", patchPath, err)
	}
	if err := syncCheckPatchVersion("import", patch.Version, syncPatchVersion); err != nil {
		return nil, err
	}

	nodeTag := patch.Node
	if nodeTag == "" {
		nodeTag = syncTaskUnknownNode
	}
	nodeTag = SyncNodeTagPrefix + nodeTag

	ctx := context.Background()
	db, err := openStoreDB(ctx, memoryDBPath())
	if err != nil {
		return nil, fmt.Errorf("memsync import: %w", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil) // BEGIN IMMEDIATE via _txlock (openDB)
	if err != nil {
		return nil, fmt.Errorf("memsync import: begin transaction: %w", err)
	}
	stats, txErr := syncImportRows(ctx, tx, &patch, nodeTag)
	if txErr != nil {
		// Hard DB failure mid-patch: roll the WHOLE patch back and surface
		// the error explicitly (non-silent) — the caller's DB is exactly as
		// it was before the import.
		_ = tx.Rollback() //stylegate:ignore rollback on the error path; the original failure is already being returned
		return nil, fmt.Errorf("memsync import: rolled back (no rows applied): %w", txErr)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("memsync import: commit: %w", err)
	}
	return stats, nil
}

// syncImportRows applies every patch row through the store core on the
// CALLER'S transaction. Per-row semantic refusals count into stats; any
// DB-level error aborts the whole patch (the caller rolls back).
func syncImportRows(ctx context.Context, tx *sql.Tx, patch *SyncPatch, nodeTag string) (*SyncImportStats, error) {
	stats := &SyncImportStats{}
	for i, r := range patch.Rows {
		if strings.TrimSpace(r.Topic) == "" || strings.TrimSpace(r.Question) == "" || strings.TrimSpace(r.Answer) == "" {
			stats.RowsRefused++
			stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: empty topic/question/answer", i))
			continue
		}
		// Anti-clobber: MemoryStore's exact dedup pairs (question, topic) —
		// a patch row with the same question but a different topic would
		// INSERT a near-duplicate beside a human's row. If a NON-sync row
		// (no brew/node tag) already owns this question, the human wins:
		// refuse the patch row instead of shadowing it. Probed on the tx —
		// sees this patch's own rows, one connection, no deadlock.
		owned, err := syncQuestionOwnedQ(ctx, tx, r.Question)
		if err != nil {
			return nil, fmt.Errorf("row %d: ownership probe: %w", i, err)
		}
		if owned {
			stats.RowsRefused++
			stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: question already owned by a local non-sync row — human wins", i))
			continue
		}
		// Force the node tag into sources (union with any row-local tags).
		source := nodeTag
		// The row's PROVENANCE POINTER rides in with the row (item 5/5). It is
		// NOT invented here: the sender's own capture recorded it, and without
		// it the receiver's copy would be indistinguishable from a row nobody
		// captured — the silent degradation Rule 15 forbids. A patch from an
		// older sender (or a pre-2/5 exporter) carries no pointer, which reads
		// back as the zero MemoryOrigin: absent stays absent.
		//
		// A MALFORMED pointer (a round with no session) is refused as a PER-ROW
		// verdict, exactly like the garbage gate above — never as a whole-patch
		// abort. The reason is operational: a single bad pointer from one
		// misbuilt sender would otherwise kill the entire sync link between two
		// nodes until a human fixed the far side, which is the "one row takes
		// the whole channel down" failure this channel exists to avoid. The
		// refusal is counted AND attributed, so it is not silent either.
		if ptr := r.Origin.memoryOrigin(); !ptr.Complete() {
			stats.RowsRefused++
			stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: %s", i, ptr.Refusal()))
			continue
		}
		res, err := memoryStoreRow(ctx, tx, r.Topic, r.Question, r.Answer, source, r.Keywords, r.Origin.memoryOrigin())
		if err != nil {
			if errors.Is(err, ErrGarbageRefused) {
				// Per-row semantic verdict, not a failure: a garbage row
				// never poisons its neighbors — count and keep the patch.
				stats.RowsRefused++
				stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: %v", i, err))
				continue
			}
			// DB-level failure: abort the WHOLE patch (caller rolls back
			// everything applied so far — import is all-or-nothing).
			return nil, fmt.Errorf("row %d: %w", i, err)
		}
		if strings.Contains(res, "deduped into an existing entry") {
			stats.RowsDeduped++
			// The fold discarded a body that is NOT the stored one: count it,
			// and NAME it. A row whose knowledge was not stored must not be
			// indistinguishable from an idempotent re-import (Rule 15).
			if syncStoreBodyDiffering(res) {
				stats.RowsDedupedDiffering++
				stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: %s", i, syncBodyDifferingNote(r, res)))
			}
		} else {
			stats.RowsImported++
		}
	}
	return stats, nil
}

// syncBodyDifferingNote renders the per-row diagnostic for a row whose body was
// folded away: it says WHERE the knowledge went (the surviving row's id) and
// that the arrival's text is not in the DB, so an operator can decide to store
// it as its own lesson. It is a NOTE, never a refusal — the fold is the correct
// behaviour and the patch still committed.
func syncBodyDifferingNote(r SyncRow, storeResult string) string {
	return fmt.Sprintf("body not stored — the exact-key fold kept the existing row (%s); patch topic %q, question %q",
		storeResult, r.Topic, truncateRunes(r.Question, dedupQuestionRunes))
}

// SetMemoryDBPath points MemoryStore (and every other memory path that reads
// memoryDBPath) at an explicit DB file. Empty value = keep the default.
// CLI-only: --db on memsync import (tests use t.Setenv on GOPI_MEMORY_DB).
func SetMemoryDBPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return os.Setenv(memoryDBEnv, path)
}

// syncQuestionOwned reports whether the question is already held by a local
// row that carries NO sync tags (neither brew nor node:*) — a human/agent
// row. Such a row always wins over an incoming patch row. Opens its own
// connection: legacy/standalone use outside an import transaction.
func syncQuestionOwned(question string) (bool, error) {
	db, err := openStoreDB(context.Background(), memoryDBPath())
	if err != nil {
		return false, err
	}
	defer db.Close()
	return syncQuestionOwnedQ(context.Background(), db, question)
}

// syncQuestionOwnedQ is the probe on any querier — SyncImport passes its
// transaction so the verdict sees the patch's own just-written rows
// (idempotent re-import) and never touches a second connection mid-transaction.
func syncQuestionOwnedQ(ctx context.Context, q QueryExecer, question string) (bool, error) {
	qq := truncateRunes(strings.TrimSpace(question), dedupQuestionRunes)
	var sources string
	err := q.QueryRowContext(ctx,
		`SELECT COALESCE(sources,'') FROM distilled_qa WHERE question=?`, qq).Scan(&sources)
	if err == nil {
		return !syncRowIsSyncSourced(sources), nil
	}
	return false, nil
}

// syncRowIsSyncSourced reports whether a sources value contains a tag that
// marks a sync-managed row (the brew tag or any node:* tag).
func syncRowIsSyncSourced(sources string) bool {
	for _, s := range splitSources(sources) {
		if s == BrewSourceTag || strings.HasPrefix(s, SyncNodeTagPrefix) {
			return true
		}
	}
	return false
}
