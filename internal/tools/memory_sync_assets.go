// Package tools — memory_sync_assets.go: the distilled_qa ASSET channel of
// peer sync (epic: cross-node memory-asset sync).
//
// THE CLI FACE OF THIS FILE IS LIVE, MANUALLY. Commit 5677cc6e
// (epic cross-node-sync-removal, 2026-11-05) removed every spelling that
// reached this code — `gopi memory sync --peer <alias>`, its `--push` /
// `--pull` / `--every` / `--serve-assets` arms and the `gopi memory memsync`
// sibling. `gopi memory sync` was RESTORED the next day
// (cmd/agent/cli_memory_sync.go) as a MANUAL door: one invocation = one
// session, exit. `--push` / `--pull` / `--dry-run` / `--serve-assets` /
// `--timeout` are typed again; `--every` is NOT (its reader went in batch 5,
// and `memsync` stays deleted).
//
// WHAT READS THE MACHINERY NOW: cmd/agent/cli_memory_sync.go, through
// MemoryAssetsPeerSessionMode (one session), MemoryAssetsServeRemote (the
// `--serve-assets` peer side, ssh'd not typed), MemoryAssetsDryRun (the sizing
// path) and MemoryAssetsImportPatch / MemoryAssetsSessionApplyReport (the
// session report). config.ResolveMemoryPeers is the whitelist this channel
// validates its peer against, and `gopi memory backup` reads the SAME key for
// its one-way ship leg.
//
// THE OTHER CROSS-HOST DOOR is `gopi memory backup` (internal/tools/ledger_backup.go):
// a VACUUM INTO snapshot, verified read-only, shipped over one ssh leg to a
// declared memory_peers peer (peer side `gopi memory backup --serve`). It is a
// ONE-WAY COPY — it imports nothing — so it is not a replacement for the
// bidirectional merge described below; it is the door to use when the peer's
// ledger should be REPLACED rather than converged.
//
// The original description of the channel follows, as the record of what this
// machinery was built for:
//
//	LIVE (manual) — `gopi memory sync --peer <alias>`   one bidirectional session
//
// WHITELIST — memory_peers, NOT sync_peers (2026-09-02 bugfix): the asset
// channel validates its peer against settings.json "memory_peers" /
// GOPI_MEMORY_PEERS — a deliberately DECOUPLED key from the task ledger's
// sync_peers. Single-node task mode (sync_peers=[]) must not disable asset
// sync; nick explicitly keeps this cross-node feature. A deployment declares
// both: sync_peers=[] (no task sync) + memory_peers=["hp"] (asset sync up).
//
// SEMANTICS — unique-only superset merge (deliberately conservative):
//
//	  identity = the question (exact match — the MemoryStore exact-dedup
//	  probe). A row the PEER does not have is copied over; a row that exists
//	  on BOTH sides (same question) is NEVER touched — content divergence
//	  is the out-of-scope conflict zone (no newer-wins, no merge, no
//	  conflict tag — just skipped and counted `existing`).
//
//	Direction — ONE session moves rows BOTH ways (initiator → peer on the
//	  outbound leg, peer → initiator on the reply), so a single
//	  `--peer hp` session (the DELETED spelling; see this file's header) from
//	  either side converges both stores.
//	  No intermediate patch files: the transport is `ssh <peer>` with the
//	  patch on stdin/stdout (the task-sync serve-remote shape).
//
//	Anti-reflow — rows tagged node:<X> never re-export (the SyncExport
//	  gate: a row whose sources carry ANY node:* tag stays home). Every
//	  LOCAL knowledge row travels — brew, hermes-import, source-less
//	  early-dev — regardless of source tag; an explicit source narrows.
//	  Rows imported from a peer carry node:<peer> — so an imported row is
//	  never pushed back toward any peer, and ping-pong loops cannot form.
//
//	Idempotent — re-running after convergence imports ~0 rows: every
//	  outbound row already exists on the peer → skipped `existing`.
//
//	Safety — insert-only: an existing row is never updated/overwritten
//	  (probe first, skip on hit). New rows land through the SAME
//	  MemoryStore write path brew uses (memoryStoreRow): garbage gate,
//	  similarity dedup, FTS triggers, provenance node:<sender> forced.
//
// The wire format is the existing SyncPatch (memsync file format v1) —
// identity is re-established at import, no row ids travel.
package tools

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
)

// memoryAssetSyncSSHTimeout bounds ONE ssh leg of a memory-asset session.
//
// It is NOT the task-sync leg's 15s bound (syncSSHTimeout), and copying that
// value here was the 2026-09-26 defect: the task leg ships a small delta
// window, while THIS leg ships the WHOLE local knowledge corpus (measured:
// 5209 rows / 15 MB out of zpeer) and the peer must apply every row through
// the MemoryStore path (exact-question probe + trigram similarity + FTS
// triggers) before it can answer. Measured on the real pair 2026-09-26:
// `ssh hp gopi memory sync --serve-assets < 15MB` took 47.5s (the measurement is
// kept because it is what sized the bound; the leg is live again as a manual
// door, so the number now sizes a command an operator really can type). The 15s bound
// therefore SIGKILLed a perfectly healthy transfer and reported it as
// `ssh hp: signal: killed (stderr: )` — no timeout, no size, no direction,
// nothing an operator could act on (Rule 15).
//
// The bound is a STALL detector, not a work-size estimate: the leg is
// expected to take as long as the peer needs to apply the corpus. 15
// minutes covers a corpus order(s) of magnitude larger than today's, and
// config.MemorySyncTimeoutEnv exists for the rest.
const memoryAssetSyncSSHTimeout = 15 * time.Minute

// ssh keepalives are what make a generous absolute bound SAFE: the peer's
// sshd answers a keepalive probe even while the peer's gopi is busy applying
// rows, so a half-open connection (laptop suspended, wifi moved, peer
// rebooted) surfaces as ssh's own "Timeout, server … not responding" within
// roughly ServerAliveInterval × ServerAliveCountMax — while a legitimately
// LONG import is never mistaken for a dead one. That distinction is why the
// leg has no application-level idle watchdog: an import that produces no
// stdout for ten minutes is exactly what a large corpus looks like.
const (
	memoryAssetSyncKeepaliveInterval = "60"
	memoryAssetSyncKeepaliveCount    = "5"
)

// memoryAssetImportBatchPause is the IDLE interval the import leaves between two
// batches — the difference between "the lock is held in smaller pieces" and "a
// concurrent writer can actually get in". It is a MEASURED value, not a guess:
//
// Per-batch commits alone were measured INSUFFICIENT on the real pair
// (2026-09-26, zpeer→hp, 51 batches): the gap between one batch's COMMIT and the
// next batch's BEGIN IMMEDIATE is microseconds, so the peer's ledger is locked
// with ~100% duty and a concurrent writer still waited the full
// dbBusyTimeoutMSec and died with SQLITE_BUSY — the probe on hp reported 2 BUSY
// after 15.02s during a 43.8s session with the batched binary applying. The
// busy handler retries on a growing sleep schedule (up to 100ms), so a
// sub-millisecond gap is invisible to it in practice: what a waiting writer
// needs is a window WIDER than its own retry cadence.
//
// 200ms is sized from that: at least one full busy-handler retry cycle inside
// every interval, sampled by a probe whose attempts are 50ms apart. Cost is
// bounded and small — one pause per batch boundary, so a 51-batch session pays
// ≈10s on a ≈44s session (measured), while the benefit is the entire point of
// this fix: the peer's ledger is actually available to the peer's own writers
// while a sync leg is running. It is NOT paid after the LAST batch (nothing
// follows it).
//
// The pause is OUTSIDE the transaction (after the commit, before the next
// BEGIN), so it never widens a lock window — it only separates two of them.
const memoryAssetImportBatchPause = 200 * time.Millisecond

// memoryAssetImportBatchRows bounds how many patch rows ONE transaction of the
// asset import may apply — the size of the peer's write-lock window.
//
// WHY THIS EXISTS (2026-09-26, mechanism 2 of the memsync failure family): the
// import used to apply the WHOLE patch in one BEGIN IMMEDIATE, so a
// `--peer <peer>` leg (the DELETED CLI spelling; see this file's header)
// write-locked the PEER's ledger for the
// whole apply — measured ~40s for a 5209-row/15MB corpus. Every concurrent
// gopi writer on that peer waited dbBusyTimeoutMSec (15s) and then died with
// SQLITE_BUSY. The lock hold is what the batch bounds, so the constant is
// sized by a LOCK BUDGET, not by throughput: at the measured per-row cost of
// the MemoryStore path on this corpus (47.5s / 5209 rows ≈ 9ms), one batch
// holds the write lock for ≈0.3s — two orders of magnitude below the 15s a
// waiting writer tolerates. Throughput is unaffected (the work per row is
// identical; only the commit boundary moves); the price is one extra
// BEGIN/COMMIT per batch, and the peer's exporter stays read-only throughout.
const memoryAssetImportBatchRows = 32

// MemoryAssetSyncKeepaliveHint renders the keepalive knobs the leg runs with,
// from the same constants the ssh command line is built from (Rule 13 — a
// caller-facing description must never outlive the value it describes). The
// CLI's --timeout help quotes it, so the sentence an operator reads cannot
// claim a detector the leg does not actually run.
const MemoryAssetSyncKeepaliveHint = "ServerAliveInterval=" + memoryAssetSyncKeepaliveInterval +
	", ServerAliveCountMax=" + memoryAssetSyncKeepaliveCount

// assetSyncSSHBound resolves the absolute bound for ONE asset ssh leg:
// the GOPI_MEMORY_SYNC_TIMEOUT_SEC override when set to a positive number
// of seconds, else memoryAssetSyncSSHTimeout. A malformed/zero/negative
// override is reported (Rule 15 — never a silent fallback).
func assetSyncSSHBound() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(config.MemorySyncTimeoutEnv))
	if raw == "" {
		return memoryAssetSyncSSHTimeout, nil
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return 0, fmt.Errorf("%s=%q is not a positive number of seconds", config.MemorySyncTimeoutEnv, raw)
	}
	return time.Duration(secs) * time.Second, nil
}

// assetSyncSSHTimeoutError rewrites ssh's opaque kill into an actionable
// sentence: the transfer may simply need longer, and there is a knob for it.
// It is reached only when OUR bound elapsed (a dead connection ends through
// ssh's keepalives instead, with ssh's own message on stderr) — so the text
// names the bound and the knob rather than guessing at the cause.
func assetSyncSSHTimeoutError(peer string, deadline time.Duration, stderr string) error {
	// BOTH spellings of the ONE knob (Rule 2: the env read and the CLI flag are
	// one setting). The `--timeout` half was DELETED 2026-11-05 with the verb,
	// and this sentence correctly dropped it; the verb is back (2026-11-06),
	// so a refusal that named only the env var would send the operator to a
	// shell export when a flag on the command they are running would do.
	knob := fmt.Sprintf("raise the bound with %s --timeout SECONDS (or %s=SECONDS)",
		MemorySyncProgram, config.MemorySyncTimeoutEnv)
	return fmt.Errorf("ssh %s: the asset session did not finish within %s (bound elapsed; ssh stderr: %q) — the leg carries this node's whole local corpus and the peer applies every row before answering, so a large corpus legitimately exceeds a short bound: %s",
		peer, deadline, strings.TrimSpace(stderr), knob)
}

// MemoryAssetSyncStats reports one bidirectional asset session.
type MemoryAssetSyncStats struct {
	Peer string
	// Mode = the direction actually run (assetSyncModeBoth default, or the
	// one-way assetSyncModePush / assetSyncModePull).
	Mode string
	// PushedRows = rows we exported toward the peer (their merge decides
	// imported vs existing — we never know a priori). 0 in a pull-only run.
	PushedRows int
	// PullStats = our merge of the peer's reply rows. nil when the run did
	// not apply the reply (--push), in which case PullSkippedRows carries
	// the reply's size so the operator can still see what was NOT applied.
	PullStats *MemoryAssetImportStats
	// PullSkippedRows = rows the peer offered that a --push run did not apply.
	PullSkippedRows int
	// DurationSec of the whole session.
	DurationSec float64
	// TimeoutSec = the absolute bound the ssh leg ran under (resolved once,
	// so the report can state it and the NEXT run's --timeout is informed).
	TimeoutSec float64
	// DryRun marks a session that skipped the ssh leg (report-only).
	DryRun bool
	// (The former ChildCwdReport field is GONE with its only producer: the
	// store-triggered round was deleted 2026-10-27 (epic
	// memory-store-sync-decouple), so nothing measures a child's cwd any more —
	// there is no child. A hand-run session (the DELETED `gopi memory sync`
	// spelling) runs in the operator's own shell and inherits its directory by
	// definition.)
	// ReplyBody is the peer's reply export, UNINTERPRETED. It is set only
	// by initiatorExchangeDefault (the raw leg); the session parser
	// consumes it into PushedRows/PullStats/PullSkippedRows and clears it,
	// so a caller that holds a stats value never sees raw wire bytes.
	ReplyBody []byte
	// PeerReplyRows is the peer's reply export SIZED, not parsed — the raw
	// count of rows the peer offered us. It is the only reachable signal
	// for a --push leg (whose reply is deliberately never applied and which
	// therefore has no import tally) and it is what the periodic leg's
	// round-under-test reads; the one-shot path additionally decodes the
	// envelope into PullSkippedRows.
	PeerReplyRows int
	// PeerImport is the PEER's own tally of what it imported from the patch
	// WE sent — the answer to "did this push move any knowledge?", which
	// nothing on this side can compute (the peer's store is the one that
	// decides imported-vs-existing). It rides back on the leg's reply
	// (SyncPatch.ImportStats); nil means the peer did not report one
	// (an older binary), and nil is NOT zero.
	PeerImport *SyncImportTally
}

// SyncImportTally is the peer-side import outcome a serve handler reports
// back on the reply patch (SyncPatch.ImportStats). It is deliberately a
// small, additive report: the four classes MemoryAssetsImportPatch already
// counts, so a --push initiator can say whether its push landed new rows
// without a second channel and without applying anything.
type SyncImportTally struct {
	Imported int `json:"imported"`
	Existing int `json:"existing"`
	Deduped  int `json:"deduped"`
	Refused  int `json:"refused"`
}

// syncImportTallyOf renders an import's stats as the reply tally.
func syncImportTallyOf(stats *MemoryAssetImportStats) *SyncImportTally {
	if stats == nil {
		return nil
	}
	return &SyncImportTally{
		Imported: stats.Imported,
		Existing: stats.Existing,
		Deduped:  stats.Deduped,
		Refused:  stats.Refused,
	}
}

// MemoryAssetImportStats reports the import outcome per row class:
//
//	Imported      — new rows stored (peer lacked them)
//	Existing      — the question already existed locally → skipped UNTOUCHED
//	                (the out-of-scope conflict zone, by design)
//	Deduped       — similarity-deduped into an existing row by MemoryStore
//	                (a near-duplicate question, not the identity probe)
//	BodyDiffering — the subset of Deduped whose BODY is not the body of the row
//	                it merged into. Inside Deduped (the fold is unchanged): the
//	                existing row keeps its knowledge and the arrival's text was
//	                not stored, which the operator must be able to see (Rule 15)
//	Refused       — per-row semantic refusals (garbage gate, validation)
type MemoryAssetImportStats struct {
	// Per-row semantic refusals (garbage gate, validation) are NOT a failure:
	// the batch still commits and the run completes — see the merge of
	// `Refused` below.
	Imported      int
	Existing      int
	Deduped       int
	BodyDiffering int
	Refused       int
	// Errors are the per-row diagnostics of rows processed but not stored
	// (refusals, and rows the store skipped). They are reported, never
	// swallowed (Rule 15).
	Errors []string
	// TotalRows / RowsCommitted describe the BATCHED contract (2026-09-26):
	// the patch applies in transactions of at most
	// memoryAssetImportBatchRows rows, so a failure mid-corpus leaves the
	// rows of every earlier batch COMMITTED. RowsCommitted is how much of
	// the patch was PROCESSED and committed (imported, skipped as existing,
	// deduped or refused — the row counts above say which), 0 for a failure
	// inside the first batch. It is what the honest failure message quotes
	// and where a re-run starts from (see the contract ruling on
	// MemoryAssetsImportPatch).
	TotalRows     int
	RowsCommitted int
	// Batches / BatchesFailed are the transaction tally: Batches = committed
	// batches, BatchesFailed = 1 when a batch failed (0 on total success).
	Batches       int
	BatchesFailed int
}

// syncAssetsNodeName returns THIS node's sync identity: the GOPI_SYNC_NODE
// override (the documented dual-node sim rule, memory_sync_delta.go) or the
// hostname. Used to stamp the export patch's Node — the import side turns it
// into node:<Node> provenance — so simulated nodes in tests tag rows with
// their SIMULATED name.
func syncAssetsNodeName() string {
	if n := strings.TrimSpace(os.Getenv("GOPI_SYNC_NODE")); n != "" {
		return n
	}
	host, _ := os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
	return host
}

// MemoryAssetsPeerSession runs ONE bidirectional unique-only asset session
// with peer over ssh. cliPeers is kept for signature compatibility with the
// task-sync session API but is IGNORED — asset sync validates against the
// decoupled memory_peers whitelist (config.ResolveMemoryPeers), never
// sync_peers. Returns per-direction tallies; an unreachable/refusing peer
// is an ERROR (the session fails — unlike task sync's fail-soft tick, this
// verb is explicitly manual).
func MemoryAssetsPeerSession(peer string, cliPeers []string, dbPath string) (*MemoryAssetSyncStats, error) {
	return MemoryAssetsPeerSessionMode(peer, cliPeers, dbPath, "")
}

// MemoryAssetsPeerSessionMode is MemoryAssetsPeerSession with an explicit
// DIRECTION — the one-way push/pull mode the operator asked for:
//
//	""  (or assetSyncModeBoth) — bidirectional: push our rows, then apply
//	    the peer's reply. One invocation converges both stores.
//	assetSyncModePush — one-way outward: push our unique rows; the peer
//	    still answers with its export (the protocol is one ssh leg), but we
//	    do NOT apply it, so nothing the peer holds drifts into this store.
//	assetSyncModePull — one-way inward: pull only. The peer side of the
//	    assets protocol has no "do not import stdin" mode, so the leg is
//	    the same one; the direction is enforced by sending the peer an
//	    EMPTY patch (nothing for it to import → the peer's ledger is
//	    untouched) while we apply its export. Measured before that change:
//	    a nominal --pull wrote our rows to the peer (hp grew by our marker),
//	    which is not what the flag promises.
//
// The modes are SYMMETRIC: HP runs the same verb toward zpeer. Every mode
// keeps the unique-only contract — identity is the exact question, an
// existing row is never touched, and re-running imports ~0.
func MemoryAssetsPeerSessionMode(peer string, cliPeers []string, dbPath, mode string) (*MemoryAssetSyncStats, error) {
	started := time.Now()
	if err := syncAssetsPeerWhitelisted(peer, cliPeers); err != nil {
		return nil, err
	}
	push, pull, err := selectAssetsDirection(mode)
	if err != nil {
		return nil, err
	}
	if !push && !pull {
		// Unreachable through the CLI (selectAssetsDirection refuses an
		// empty selection) — kept explicit so a future mode cannot silently
		// open an ssh leg that does nothing.
		return nil, fmt.Errorf("memsync assets: no direction selected (mode %q)", mode)
	}

	// Push side: export OUR local knowledge rows (any source except
	// node:* — anti-reflow). The peer's import probes each question and
	// keeps only what it lacks.
	patch, _, err := SyncExport(SyncExportOptions{DBPath: dbPath})
	if err != nil {
		return nil, fmt.Errorf("memsync assets: export: %w", err)
	}
	patch.Node = syncAssetsNodeName()
	if !push {
		// --pull: send an EMPTY patch, not merely an ignored one. The peer
		// imports whatever stdin carries — it has no "do not import stdin"
		// mode to set — so the only way to make a pull genuinely one-way is
		// to give it nothing to import. Measured without this (zpeer --pull
		// → hp): hp grew 5734 → 5735 with OUR rows, i.e. a "--pull" had
		// written to the peer. Sending zero rows keeps the peer's ledger
		// untouched by us; its reply (its own export) is still what we apply.
		patch.Rows = nil
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("memsync assets: marshal: %w", err)
	}
	// This side's own report fields. They are NOT stamped on an object of our
	// own: the object the session returns (and the printer renders) is the
	// LEG's, so the caller hands its facts over instead of racing that object
	// (measured defect: a bound stamped on the caller's stats and never
	// printed — `bound=0s`).
	caller := MemoryAssetSyncStats{Peer: peer, Mode: assetSyncModeName(mode)}
	pushRows := len(patch.Rows)
	if !push {
		pushRows = 0
	}
	bound, err := assetSyncSSHBound()
	if err != nil {
		return nil, fmt.Errorf("memsync assets: %w", err)
	}
	// The resolved bound is the caller's copy; the leg stamps the same value
	// itself, and this one is the fallback for a leg that reports none.
	caller.TimeoutSec = bound.Seconds()

	// The ssh leg: stdin = our patch, stdout = the peer's reply export
	// (the peer merged our rows, then exported ITS local rows back). The
	// leg is ALWAYS the same one — a "pull" is this leg with an EMPTY
	// outbound patch, never a different remote command.
	replyStats, err := syncAssetsSSHExchangeMode(peer, body, bound, mode)
	if err != nil {
		return nil, fmt.Errorf("memsync assets: %w", err)
	}
	caller.DurationSec = time.Since(started).Seconds()
	return MemoryAssetsSessionApplyReport(replyStats, caller, pushRows, dbPath, push, pull)
}

// MemoryAssetsSessionApply interprets a leg's OUTCOME: it turns the raw
// reply export into this run's tallies, applying the peer's rows when this
// direction pulls (unique-only, the same import path as the one-shot verb)
// and merely SIZING them when it does not (a --push must not write the
// peer's rows into this store).
//
// It is the ONE place the initiator's interpretation lives, shared by the
// one-shot verb and the per-peer periodic leg so the two can never disagree
// about what a reply means. legStats carries the leg's own report (Mode,
// ReplyBody, duration/bound when the leg measured them); pushRows is what
// THIS side exported (the leg is not told, so cost is a caller fact).
func MemoryAssetsSessionApply(legStats *MemoryAssetSyncStats, pushRows int, dbPath string, push, pull bool) (*MemoryAssetSyncStats, error) {
	return MemoryAssetsSessionApplyReport(legStats, MemoryAssetSyncStats{}, pushRows, dbPath, push, pull)
}

// MemoryAssetsSessionApplyReport is MemoryAssetsSessionApply with the
// CALLER's own report fields: legStats is the leg's object (which is what
// this returns, so its reply-parsing semantics are unchanged), and the
// caller-supplied fields are carried onto it.
//
// WHY THE CALLER'S FIELDS EXIST AT ALL: the leg knows what the WIRE did
// (mode, reply, the bound it ran under); it cannot know what THIS side
// exported, because the outgoing patch is built before the leg is invited.
// That is a caller fact, and it must not be left on an object nobody prints
// — the measured defect is exactly that shape: the caller stamped the bound
// on its OWN stats while the session returned the LEG's object, so the
// printer read 0 (`bound=0s`).
//
// The bound is filled here as a FALLBACK for a leg that did not measure one
// (Mode/Peer/DurationSec/TimeoutSec are "zero = the leg did not report it";
// a real bound is always positive, and an empty peer is no peer).
func MemoryAssetsSessionApplyReport(legStats *MemoryAssetSyncStats, caller MemoryAssetSyncStats, pushRows int, dbPath string, push, pull bool) (*MemoryAssetSyncStats, error) {
	if legStats == nil {
		return nil, fmt.Errorf("memsync assets: leg reported no outcome")
	}
	stats := legStats
	stats.PushedRows = pushRows
	if caller.Peer != "" {
		stats.Peer = caller.Peer
	}
	if caller.Mode != "" {
		stats.Mode = caller.Mode
	}
	if caller.TimeoutSec > 0 {
		stats.TimeoutSec = caller.TimeoutSec
	}
	if caller.DurationSec > 0 {
		stats.DurationSec = caller.DurationSec
	}
	if !push {
		// A pull-only leg ships an EMPTY patch (see the session's own note),
		// so the peer imported nothing from us.
		stats.PushedRows = 0
	}
	reply := stats.ReplyBody
	stats.ReplyBody = nil
	if !pull {
		// --push: the reply is the peer's export; we deliberately do not
		// apply it. Report it as sized, not imported. PeerReplyRows is the
		// count the periodic leg's round-under-test reads (a mocked leg has
		// no envelope at all); an UNPARSABLE envelope leaves it 0 — the
		// one-shot path's PullSkippedRows keeps its historical "0 when the
		// envelope cannot be read" meaning, and neither is a claim that the
		// peer offered nothing.
		var peerPatch SyncPatch
		if err := json.Unmarshal(bytes.TrimSpace(reply), &peerPatch); err == nil {
			stats.PullSkippedRows = len(peerPatch.Rows)
			stats.PeerReplyRows = len(peerPatch.Rows)
			stats.PeerImport = peerPatch.ImportStats
		}
		return stats, nil
	}
	var peerPatch SyncPatch
	if err := json.Unmarshal(bytes.TrimSpace(reply), &peerPatch); err != nil {
		return nil, fmt.Errorf("memsync assets: peer envelope: %w", err)
	}
	if err := syncCheckPatchVersion("assets", peerPatch.Version, syncPatchVersion); err != nil {
		return nil, err
	}
	stats.PeerReplyRows = len(peerPatch.Rows)

	// Pull side: apply the peer's rows through OUR MemoryStore path.
	applied, err := MemoryAssetsImportPatch(&peerPatch, dbPath)
	if err != nil {
		return nil, fmt.Errorf("memsync assets: merge: %w", err)
	}
	stats.PullStats = applied
	return stats, nil
}

// Direction/mode spellings — the ONE definition of each (Rule 1b): the CLI
// admits exactly these, the tool layer resolves exactly these, and the usage
// text is checked against them by TestMemorySyncUsageDocumentsFlags. They are
// EXPORTED because the CLI must not re-type the strings (Rule 13/14): the
// admission list and the resolver are the same values by construction.
const (
	// AssetSyncModeBoth is the default: one bidirectional session.
	AssetSyncModeBoth = "both"
	// AssetSyncModePush / AssetSyncModePull are the one-way modes. Their VALUES
	// are the direction constants of the leaf package (Rule 14: the retired
	// store-triggered knob names the same two directions, and a second literal
	// here would be the drift Rule 13 bans — the direction knob's settings value
	// and this mode string are ONE vocabulary; the CLI side that used to spell
	// `--push`/`--pull` is DELETED with the cross-node sync family, 5677cc6e).
	AssetSyncModePush = config.MemorySyncDirectionPush
	AssetSyncModePull = config.MemorySyncDirectionPull
)

// (The five command-line aliases that USED to be declared here — the verb word
// plus its peer / push / pull / db flag words — are GONE with the only consumers
// they ever had. The store-triggered round
// (`internal/tools/memory_store_trigger.go`) was deleted 2026-10-27 together with
// the resident memory-sync tick, and the verb itself went 2026-11-05
// (`5677cc6e`). Rule 14 is why the words did NOT come back when the verb did two
// days later: the ONE consumer is `cmd/agent/cli_memory_sync.go`, which already
// composes its own flag table and reaches this package through the functions
// above, so an alias here would be a second definition with no second reader.
// The spelling itself lives once, in internal/tools/memory_sync_verb.go — beside
// the argv that carries it over ssh. The "--db" word lives once in
// internal/config's LedgerSourceFlag. See devdoc/memory-store-sync-decouple.md
// and devdoc/memory-sync-manual-door-restored.md.)

// MemorySyncTimeoutEnv re-exports the leaf constants package's spelling so
// callers already in this package (and the CLI's existing import) need not
// reach for two packages (Rule 13: nothing re-types the literal).
const MemorySyncTimeoutEnv = config.MemorySyncTimeoutEnv

// assetSyncModeBoth / assetSyncModePush / assetSyncModePull are the internal
// aliases of the exported spellings (the resolver below reads these).
const (
	assetSyncModeBoth = AssetSyncModeBoth
	assetSyncModePush = AssetSyncModePush
	assetSyncModePull = AssetSyncModePull
)

// assetSyncModeName renders a mode for the report ("" = the bidirectional
// default, which reports its own name rather than an empty cell).
func assetSyncModeName(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return assetSyncModeBoth
	}
	return mode
}

// selectAssetsDirection resolves a mode string to (push, pull). One
// definition of the mapping, so the CLI and the tool layer can never
// disagree about what "--pull" means (Rule 14).
func selectAssetsDirection(mode string) (push, pull bool, err error) {
	switch strings.TrimSpace(mode) {
	case "", assetSyncModeBoth:
		return true, true, nil
	case assetSyncModePush:
		return true, false, nil
	case assetSyncModePull:
		return false, true, nil
	}
	return false, false, fmt.Errorf("unknown direction %q (want %s, %s or %s)",
		mode, assetSyncModePush, assetSyncModePull, assetSyncModeBoth)
}

// MemoryAssetsDryRun reports what a real session toward peer WOULD push,
// without the ssh leg (no peer contact): the memory_peers whitelist check
// runs, then the export is sized and returned. The pull side is unknowable
// without the peer, so a dry run only sizes the push.
func MemoryAssetsDryRun(peer string, cliPeers []string, dbPath string) (*SyncPatch, error) {
	if err := syncAssetsPeerWhitelisted(peer, cliPeers); err != nil {
		return nil, err
	}
	patch, _, err := SyncExport(SyncExportOptions{DBPath: dbPath})
	if err != nil {
		return nil, fmt.Errorf("memsync assets: export: %w", err)
	}
	patch.Node = syncAssetsNodeName()
	return patch, nil
}

// syncAssetsPeerWhitelisted enforces the declared-topology rule: peer must
// be in the MEMORY-ASSET whitelist (settings.json memory_peers /
// GOPI_MEMORY_PEERS), never discovered. Decoupled from the task ledger's
// sync_peers whitelist (2026-09-02 bugfix): single-node task mode clears
// sync_peers — task sync disabled — while memory-asset sync (a deliberately
// kept cross-node feature) validates against ITS OWN key, so the single-node
// config does not silently kill asset sync.
//
// The wording is peer_topology.go's (Rule 14) — the two channels share one
// refusal shape and differ only in the whitelist they name, so the sentence an
// operator learns once applies on both.
func syncAssetsPeerWhitelisted(peer string, _ []string) error {
	if err := RequireDeclaredAssetPeer(peer); err != nil {
		return fmt.Errorf("memsync assets: %w", err)
	}
	return nil
}

// syncAssetsSSHExchangeReply is the raw request/reply exchange of ONE asset
// leg: the initiator's patch in, the peer's post-merge export out, with NO
// interpretation by the initiator (the session that calls it decides what
// the reply MEANS — see syncAssetsSSHExchangeMode).
type syncAssetsSSHExchangeReply func(peer string, pushBody []byte, bound time.Duration, mode string) (*MemoryAssetSyncStats, error)

// initiatorExchangeDefault is the REAL initiator: one ssh leg to the peer,
// whose stdout is the peer's reply export. Its whole output is the leg's
// result, so the mode it is handed is carried THROUGH untouched — the
// per-peer periodic loop reads it back to learn what the leg carried (see
// memorySyncExchange).
//
// It is ALSO the leg's own report, and that report — not the caller's —
// is what MemoryAssetsSessionApply returns and the printer renders. So the
// bound the leg actually ran under (and the wall time it took) is stamped
// HERE, on the object that survives: stamping it on the CALLER's stats
// instead lost it the moment the session swapped the leg's object in (the
// printer then read TimeoutSec=0 and said `bound=0s` — measured).
func initiatorExchangeDefault(peer string, pushBody []byte, bound time.Duration, mode string) (*MemoryAssetSyncStats, error) {
	started := time.Now()
	reply, err := syncAssetsSSHExchangeReplyFn(peer, pushBody, bound)
	if err != nil {
		return nil, err
	}
	return &MemoryAssetSyncStats{
		Mode:       mode,
		ReplyBody:  reply,
		TimeoutSec: bound.Seconds(),
		// The leg's own wall time: the caller's span is measured elsewhere
		// and is not this object's, so it must be carried with the bound it
		// belongs to (both are facts about ONE ssh leg).
		DurationSec: time.Since(started).Seconds(),
	}, nil
}

// syncAssetsSSHExchangeReplyFn is initiatorExchangeDefault's own ssh leg —
// a variable so the dual-DB E2E can route it through a shim (the
// established syncSSHExchangeHook pattern); production never reassigns it.
var syncAssetsSSHExchangeReplyFn = func(peer string, pushBody []byte, bound time.Duration) ([]byte, error) {
	return syncAssetsSSHExchange(peer, pushBody, bound)
}

// initiatorExchangeHook, when non-nil, replaces the REAL initiator (and so
// the ssh leg) in syncAssetsSSHExchangeMode for an in-process test: the
// dual-DB E2E runs the peer's real serve handler against a second database
// and returns the stats OF THE LEG, which is what lets a test count dials
// and simulate an unreachable peer. Nil in production — the seam exists so
// the periodic loop's decision path can be driven without a network (Rule
// 27: no load generated to prove a tick).
var initiatorExchangeHook syncAssetsSSHExchangeReply

// SetInitiatorExchangeHook installs (or clears, with nil) the test seam and
// returns the previous value so a test can restore it. Exported because the
// E2E that needs it lives in another package (cmd/agent); production code
// never calls it — its ONE call site is a test.
func SetInitiatorExchangeHook(h syncAssetsSSHExchangeReply) syncAssetsSSHExchangeReply {
	prev := initiatorExchangeHook
	initiatorExchangeHook = h
	return prev
}

// syncAssetsSSHExchangeMode performs a leg's exchange with `mode`: it
// invites the initiator (real ssh, or the test seam above) to run ONE
// session and reports its stats. Callers interpret them per direction.
func syncAssetsSSHExchangeMode(peer string, pushBody []byte, bound time.Duration, mode string) (*MemoryAssetSyncStats, error) {
	exchange := initiatorExchangeDefault
	if initiatorExchangeHook != nil {
		exchange = initiatorExchangeHook
	}
	return exchange(peer, pushBody, bound, mode)
}

// syncAssetsSSHExchange pipes pushBody to the peer's `--serve-assets` handler —
// the remote argv is built by MemoryAssetsServeRemoteProg, whose spelling was
// DELETED with the cross-node sync family (5677cc6e), so this function is reached
// by no production caller today — and returns stdout, the peer's reply export.
// The remote
// always uses ITS OWN default knowledge base (task-sync precedent: a local
// --db override never leaks into the remote's argv — the peer's DB path is
// the peer's business). The dual-DB E2E routes through syncSSHExchangeHook,
// which swaps the env before calling the serve handler.
//
// The leg is bounded by `bound` (see assetSyncSSHBound — a STALL bound, not
// a work-size estimate), and it carries ssh keepalives so a half-open
// connection is DETECTED instead of silently burning the bound.
// timeoutOut receives the resolved bound so the caller can report it.
func syncAssetsSSHExchange(peer string, pushBody []byte, bound time.Duration) ([]byte, error) {
	// Peer-alias guard BEFORE the test seam (see syncSSHExchange).
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("memsync assets: %w", err)
	}
	if syncSSHExchangeHook != nil {
		// Test seam (the established memory_sync_session.go pattern): the
		// hook runs the peer's real serve handler in-process with the byte
		// pipe abstracted. dbPath is ignored — the hook's caller swaps the
		// env to the peer's DB before invoking.
		return syncSSHExchangeHook(peer, pushBody)
	}
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout="+syncSSHConnectTimeout,
		// Keepalives (see memoryAssetSyncKeepaliveInterval): ssh probes a silent
		// peer and exits 255 when the probes go unanswered, so a dead connection
		// ends the leg long before the absolute bound.
		"-o", "ServerAliveInterval="+memoryAssetSyncKeepaliveInterval,
		"-o", "ServerAliveCountMax="+memoryAssetSyncKeepaliveCount,
		peer,
		// ONE ssh argv element (the remote command string) — the quoted form is
		// load-bearing (syncRemoteGopi's comment, memory_sync_task.go).
		memoryAssetsServeRemoteArgv())
	cmd.Stdin = bytes.NewReader(pushBody)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr != nil {
		// The classic cross-node version-mismatch (2026-09-02 live): the peer's
		// gopi predates --serve-assets and dies with "unknown flag" on stderr.
		// Turn that raw ssh failure into an ACTIONABLE hint — the peer must pull
		// master + rebuild its bin/gopi, then retry.
		errText := strings.TrimSpace(stderr.String())
		if syncAssetsStalePeerHint(errText) {
			return nil, fmt.Errorf("ssh %s: %w (stderr: %s)\n%s", peer, runErr, errText, syncAssetsUpgradeHint(peer))
		}
		// A context kill is OUR bound, not the peer's failure: ssh's own exit
		// carries no cause (`signal: killed`, empty stderr), which is exactly the
		// opaque line this defect produced. Name the bound and the knob (Rule 15).
		if ctx.Err() != nil {
			return nil, assetSyncSSHTimeoutError(peer, bound, errText)
		}
		return nil, fmt.Errorf("ssh %s: %w (stderr: %s)", peer, runErr, errText)
	}
	return stdout.Bytes(), nil
}

// memoryAssetsServeRemoteArgv is the PEER-side remote command this leg sends:
// `<gopi> memory sync --serve-assets`, ONE ssh argv element (the quoting
// contract syncRemoteGopi states). Both words come from their constants
// (Rule 14) — a hand-typed spelling here is what dialled a deleted verb and
// turned every session into a removal notice read as a protocol error.
func memoryAssetsServeRemoteArgv() string {
	return syncRemoteGopi + " " + CliVerbMemoryGroup + " " + MemorySyncVerb + " " + syncFlagPrefixRow + MemorySyncServeAssetsFlag
}

// syncAssetsUpgradeHint returns the actionable message for a stale-peer
// binary: the peer must pull master and rebuild its gopi before this node
// can sync knowledge with it.
func syncAssetsUpgradeHint(peer string) string {
	return fmt.Sprintf("the peer %q is running an outdated gopi that does not support --serve-assets — pull master on %q and rebuild its bin/gopi (e.g. `ssh %s \"cd <repo> && git pull && go build -o bin/gopi ./cmd/agent\"`), then retry this sync", peer, peer, peer)
}

// syncAssetsStalePeerHint reports whether the ssh stderr marks the peer's
// binary as stale: the go flag package prints "unknown flag --serve-assets"
// (or "flag provided but not defined") when the remote gopi predates the
// asset serve handler, then exits 2. A version skew in the other direction
// (peer NEWER, sending a patch we can't parse) is caught downstream by
// syncCheckPatchVersion — this detector is only for the ssh-leg refusal.
func syncAssetsStalePeerHint(stderr string) bool {
	return strings.Contains(stderr, "unknown flag --serve-assets") ||
		strings.Contains(stderr, "flag provided but not defined: -serve-assets")
}

// MemoryAssetsServeRemote is the PEER side of an asset session (gopi memory
// sync --serve-assets): read the initiator's export from stdin, apply its
// unique rows through OUR MemoryStore path, then reply on stdout with OUR
// export (rows the initiator lacks flow back in the same breath). Prints
// NOTHING else on stdout — the stdin/stdout pair IS the protocol; human
// chatter goes to stderr.
func MemoryAssetsServeRemote(stdin []byte, dbPath string) ([]byte, error) {
	var patch SyncPatch
	if err := json.Unmarshal(bytes.TrimSpace(stdin), &patch); err != nil {
		return nil, fmt.Errorf("memsync serve-assets: parse stdin: %w", err)
	}
	if err := syncCheckPatchVersion("serve-assets", patch.Version, syncPatchVersion); err != nil {
		return nil, err
	}
	stats, err := MemoryAssetsImportPatch(&patch, dbPath)
	if err != nil {
		return nil, err
	}
	for _, e := range stats.Errors {
		fmt.Fprintln(os.Stderr, "memsync serve-assets: "+e)
	}
	// Our reply: our own local rows (anti-reflow gate excludes everything
	// we just imported — those carry node:<initiator> and never re-export).
	out, _, err := SyncExport(SyncExportOptions{DBPath: dbPath})
	if err != nil {
		return nil, fmt.Errorf("memsync serve-assets: export: %w", err)
	}
	out.Node = syncAssetsNodeName()
	// The tally of what WE just imported from the initiator rides back on
	// the reply: it is the only place the answer to "did this push move any
	// knowledge?" exists (the initiator's own store cannot know what the
	// peer's did). Additive report field — see SyncPatch.ImportStats.
	out.ImportStats = syncImportTallyOf(stats)
	return json.Marshal(out)
}

// MemoryAssetsImportPatch applies a peer export through the LOCAL MemoryStore
// write path — the assets channel's unique-only merge. Each row:
//
//	① exact-question probe (identity): a hit → skipped `Existing`, the row
//	  is NEVER touched (no sources merge, no content compare — divergence
//	  is the out-of-scope conflict zone).
//	② miss → memoryStoreRow with forced provenance node:<patch.Node>: the
//	  garbage gate, trigram-similarity dedup (→ `Deduped`), FTS triggers
//	  and insert all run exactly as a brew write would.
//
// LOCK CONTRACT — BATCHED, NOT ONE TRANSACTION (2026-09-26). The patch is
// applied in transactions of at most memoryAssetImportBatchRows rows, so the
// local DB is write-locked for ONE BATCH, never for the whole corpus. The
// previous shape (one transaction per patch) held the peer's write lock for
// the entire apply: measured ~40s on a 5209-row corpus, which is what made a
// concurrent gopi writer on the peer die of SQLITE_BUSY after
// dbBusyTimeoutMSec (15s).
//
// THE RULING ON ALL-OR-NOTHING (written, per Rule 7). Per-batch atomicity
// REPLACES per-patch atomicity, and the old contract is genuinely weaker —
// that is stated here rather than glossed:
//
//	What is lost    — after a mid-corpus failure the ledger holds a PREFIX of
//	                  the patch (whole batches only: the failing batch is
//	                  rolled back), instead of nothing. There is no way to
//	                  keep the old guarantee and shrink the lock: a lock
//	                  window and a transaction boundary are the same thing.
//	What is kept    — every batch is atomic, so a partial state is always a
//	                  whole number of batches; a failure is still a FAILURE
//	                  (non-nil error, loud message with the applied count —
//	                  Rule 15, never a silent success).
//	Why it is safe  — this channel's merge is unique-only + insert-only: the
//	                  identity is the exact question, an existing row is
//	                  NEVER updated or overwritten, and a row is written only
//	                  after a probe finds its question absent. So the merge is
//	                  IDEMPOTENT and MONOTONE: re-running the same sync
//	                  completes a partial patch — already-applied rows are
//	                  skipped as `existing` (counted, not duplicated), the
//	                  rest land — and no re-run can corrupt or diverge what a
//	                  previous run wrote. The all-or-nothing promise was
//	                  never load-bearing for correctness here (unlike the task
//	                  ledger's import, whose rows carry state machines and a
//	                  UNIQUE arbiter key): the recovery action for BOTH
//	                  contracts is the same sentence — re-run the sync.
//	                  Measured idempotence: second session imports 0 rows
//	                  (TestMemoryAssetsUniqueOnlySuperset).
//
// On failure the returned stats carry TotalRows / AppliedRows / Batches /
// BatchesFailed, and the error names them — "what landed" is answered, not
// inferred. A non-nil error with stats != nil is a PARTIAL apply.
func MemoryAssetsImportPatch(patch *SyncPatch, dbPath string) (*MemoryAssetImportStats, error) {
	nodeTag := SyncNodeTagPrefix + patch.Node
	if patch.Node == "" {
		nodeTag = SyncNodeTagPrefix + syncTaskUnknownNode
	}
	ctx := context.Background()
	db, err := openStoreDB(ctx, memoryDBPathOr(dbPath))
	if err != nil {
		return nil, fmt.Errorf("memsync assets import: %w", err)
	}
	defer db.Close()

	stats := &MemoryAssetImportStats{TotalRows: len(patch.Rows)}
	batches := (stats.TotalRows + memoryAssetImportBatchRows - 1) / memoryAssetImportBatchRows
	for b := 0; b < batches; b++ {
		start := b * memoryAssetImportBatchRows
		end := start + memoryAssetImportBatchRows
		if end > stats.TotalRows {
			end = stats.TotalRows
		}
		tx, err := db.BeginTx(ctx, nil) // BEGIN IMMEDIATE via _txlock (openDB)
		if err != nil {
			return stats, stats.fail(b+1, batches, batchFailedPreCommit, fmt.Errorf("begin transaction: %w", err))
		}
		// The batch's tally is buffered and merged only AFTER a successful
		// commit: a rolled-back batch's row classifications did not happen,
		// and counting them would overstate what landed (Rule 15).
		batchStats, err := syncAssetsImportRows(ctx, tx, patch.Rows[start:end], start, nodeTag)
		if err != nil {
			_ = tx.Rollback() //stylegate:ignore rollback on the error path; the batch failure is already being returned
			return stats, stats.fail(b+1, batches, batchFailedPreCommit, err)
		}
		// The batch boundary is a PUBLIC seam on BOTH of its sides: the
		// before-commit side runs while this batch's write lock is live, the
		// after-commit side the moment the lock has been released. See
		// memoryImportBatchBoundary for why a measurement needs both.
		if !memoryImportBatchBoundary(b+1, memoryImportBeforeCommit) {
			_ = tx.Rollback() //stylegate:ignore rollback on the seam's refusal path; the failure is returned below
			return stats, stats.fail(b+1, batches, batchFailedPreCommit, fmt.Errorf("batch %d was refused at its pre-commit boundary (memoryImportBatchBoundary)", b+1))
		}
		if err := tx.Commit(); err != nil {
			return stats, stats.fail(b+1, batches, batchFailedCommit, fmt.Errorf("commit: %w", err))
		}
		stats.mergeBatch(batchStats, end)
		if b+1 < batches {
			// The idle interval between two batches (see
			// memoryAssetImportBatchPause): outside the transaction, so it
			// separates lock windows instead of widening one. Without it the
			// lock's duty cycle is ~100% and a concurrent writer on this peer
			// still dies of SQLITE_BUSY — measured with the batched binary.
			time.Sleep(memoryAssetImportBatchPause)
		}
		if !memoryImportBatchBoundary(b+1, memoryImportAfterCommit) {
			return stats, stats.fail(b+1, batches, batchFailedPostCommit, fmt.Errorf("the run was stopped at batch %d's post-commit boundary (memoryImportBatchBoundary)", b+1))
		}
	}
	return stats, nil
}

// memoryImportBoundaryPhase names WHICH SIDE of a batch's commit the boundary
// seam has reached. The distinction is the point: before the commit this batch's
// write lock is LIVE (that is the lock window the batching exists to bound),
// after it the lock has been released. A measurement (and a failure injection,
// for the before side) needs to know which of the two it is standing on.
type memoryImportBoundaryPhase int

const (
	// memoryImportBeforeCommit: the batch's rows are applied, the transaction
	// is open, nothing is durable yet.
	memoryImportBeforeCommit memoryImportBoundaryPhase = iota
	// memoryImportAfterCommit: the batch is durable, its write lock released.
	memoryImportAfterCommit
)

// memoryImportBatchBoundary is the batch-boundary seam, called with the 1-based
// batch index on BOTH sides of the commit. Returning false aborts the import
// loudly: on the before side the batch is rolled back, on the after side the
// committed batch stays (the honest report says so — see
// memoryAssetBatchFailurePoint).
//
// PRODUCTION NEVER ASSIGNS IT (the initial value allows everything), so the seam
// is inert in a real run — and a RELEASE build's counterpart readers return the
// permissive answer with no environment access at all. It exists because the
// defect this change fixes is a LOCK-HOLD property, and a lock hold can only be
// measured by standing at a boundary and asking a second connection "can you
// take the write lock now?" — a question that has no answer from inside the
// import. It is a var, not a parameter, so the production call path gains no
// argument for a test's benefit (the same shape as syncSSHExchangeHook).
var memoryImportBatchBoundary = func(int, memoryImportBoundaryPhase) bool { return true }

// mergeBatch folds a COMMITTED batch's tally into the run's stats. Only ever
// called after tx.Commit(), so every number it carries really landed.
func (s *MemoryAssetImportStats) mergeBatch(b *MemoryAssetImportStats, rowsCommitted int) {
	s.Imported += b.Imported
	s.Existing += b.Existing
	s.Deduped += b.Deduped
	s.BodyDiffering += b.BodyDiffering
	s.Refused += b.Refused
	s.Errors = append(s.Errors, b.Errors...)
	s.Batches++
	s.RowsCommitted = rowsCommitted
}

// memoryAssetBatchFailurePoint says WHERE in a batch's life a failure landed.
// It is not bookkeeping: it is the difference between "this batch is NOT in the
// ledger" and "this batch IS in the ledger and the run stopped after it", which
// is exactly what an operator resuming a partial sync must know (Rule 15).
type memoryAssetBatchFailurePoint int

const (
	// batchFailedPreCommit: the transaction was rolled back cleanly.
	batchFailedPreCommit memoryAssetBatchFailurePoint = iota
	// batchFailedCommit: the COMMIT itself errored — SQLite may have applied
	// the batch before failing, so the batch is ambiguous.
	batchFailedCommit
	// batchFailedPostCommit: the batch IS committed and durable; the run was
	// stopped at its post-commit boundary.
	batchFailedPostCommit
)

// fail records a batch failure and renders the honest mid-corpus failure: it
// names how far the patch got in the units that matter (rows applied of total,
// batch k of n), states that the rest is NOT applied, says exactly what happened
// to the failing batch itself, and names the recovery action. A bare "batch
// failed" would leave the operator unable to tell a 0-row failure from a
// 99%-applied one (Rule 15).
func (s *MemoryAssetImportStats) fail(batch, batches int, at memoryAssetBatchFailurePoint, cause error) error {
	s.BatchesFailed++
	failingBatch := map[memoryAssetBatchFailurePoint]string{
		batchFailedPreCommit:  "rolled back cleanly (the failure landed before the commit)",
		batchFailedCommit:     "AMBIGUOUS — the commit itself failed, so it may or may not have landed",
		batchFailedPostCommit: "COMMITTED and durable (the failure landed after its commit)",
	}[at]
	return fmt.Errorf("memsync assets import: batch %d of %d (max %d rows per transaction) failed: %w — %d of %d rows were processed and committed; the remaining %d are NOT applied. This batch is %s. Re-run the same sync to complete the patch: the merge is unique-only and insert-only, so already-present rows are skipped as `existing` and nothing is duplicated or overwritten",
		batch, batches, memoryAssetImportBatchRows, cause,
		s.RowsCommitted, s.TotalRows, s.TotalRows-s.RowsCommitted, failingBatch)
}

// syncAssetsImportRows applies a slice of the patch's rows one at a time on
// the CALLER'S transaction (one connection — probes see this batch's own
// just-written rows, so a same-patch duplicate is `Existing` on the second
// occurrence). rowOffset is the slice's position in the whole patch: only the
// per-row DIAGNOSTIC indexes use it, so a "row 37" in an error names the
// patch's row, not the batch's.
func syncAssetsImportRows(ctx context.Context, tx *sql.Tx, rows []SyncRow, rowOffset int, nodeTag string) (*MemoryAssetImportStats, error) {
	stats := &MemoryAssetImportStats{TotalRows: len(rows), RowsCommitted: len(rows)}
	for i, r := range rows {
		rowIdx := rowOffset + i
		topic := strings.TrimSpace(r.Topic)
		question := strings.TrimSpace(r.Question)
		answer := strings.TrimSpace(r.Answer)
		if topic == "" || question == "" || answer == "" {
			stats.Refused++
			stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: empty topic/question/answer", rowIdx))
			continue
		}
		// ① Identity probe — exact question (the conservative unique gate).
		// A hit means the row exists on BOTH sides → never touched. Any
		// ownership (brew, human, node:* — same or different sources) skips
		// equally: the conflict zone is out of scope by design.
		exists, err := syncAssetsQuestionExistsQ(ctx, tx, question)
		if err != nil {
			return nil, fmt.Errorf("row %d: existing probe: %w", rowIdx, err)
		}
		if exists {
			stats.Existing++
			continue
		}
		// ② Miss → the shared MemoryStore write path (brew's own path).
		// The row's provenance pointer rides in with it (item 5/5) — the peer
		// captured the note in a session, and dropping the pointer here would
		// make the imported copy look like a row nobody captured. An older
		// sender's patch carries none (nil → zero MemoryOrigin): absent stays
		// absent. The node tag still travels in `sources` as before.
		//
		// A malformed pointer is refused at the ROW level, matching the
		// file-channel import: one misbuilt sender must not take the whole
		// sync session down (see the same guard in memory_sync.go).
		if ptr := r.Origin.memoryOrigin(); !ptr.Complete() {
			stats.Refused++
			stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: %s", rowIdx, ptr.Refusal()))
			continue
		}
		res, err := memoryStoreRow(ctx, tx, topic, question, answer, nodeTag, r.Keywords, r.Origin.memoryOrigin())
		if err != nil {
			if errors.Is(err, ErrGarbageRefused) {
				stats.Refused++
				stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: %v", rowIdx, err))
				continue
			}
			return nil, fmt.Errorf("row %d: %w", rowIdx, err)
		}
		if strings.Contains(res, "deduped into an existing entry") {
			stats.Deduped++
			if syncStoreBodyDiffering(res) {
				stats.BodyDiffering++
				stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: %s", rowIdx, syncBodyDifferingNote(r, res)))
			}
		} else {
			stats.Imported++
		}
	}
	return stats, nil
}

// syncAssetsQuestionExistsQ probes whether ANY distilled_qa row already
// holds the question — the identity gate of the unique-only channel. Mirrors
// the MemoryStore exact-dedup key width (question[:200]) so the probe and
// the store agree on identity for long questions.
func syncAssetsQuestionExistsQ(ctx context.Context, q QueryExecer, question string) (bool, error) {
	probe := truncateRunes(question, dedupQuestionRunes)
	var one int
	err := q.QueryRowContext(ctx,
		`SELECT 1 FROM distilled_qa WHERE substr(question,1,?) = ? LIMIT 1`,
		dedupQuestionRunes, probe).Scan(&one)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}
