// Package tools — memory_sync_session.go: the M4.13 peer session driver —
// one `gopi task sync --peer <name>` runs a BIDIRECTIONAL delta exchange
// with that peer over ssh (epic ⑤: initiator pulls the peer's delta AND
// pushes its own — one session, both directions):
//
//	① push (local → peer): export our delta (updated_at > last anchor we
//	   synced TO THEM, their clock domain) → pipe JSON over ssh → the peer's
//	   remote handler (task sync --serve-remote) merges it in one pass and
//	   prints ITS export envelope back on stdout → we merge that locally.
//	   One ssh round trip, both directions, no helper files on either side.
//	② pull (peer → local): the envelope returned in ① IS the peer's delta
//	   (their remote handler exports in the same breath). Merge locally,
//	   then commit BOTH anchors: last_synced_at_source = max(anchor pushed
//	   to them was acked, anchor pulled from them).
//
//	The anchors are keyed by DIRECTION in one sync_state row per peer:
//	last_synced_at_source = what we've RECEIVED from the peer up to (their
//	clock), and the pushed-side anchor rides the same row's last_synced_at
//	+ the envelope ack. Simpler and symmetric: each side tracks ONLY what it
//	has received (the peer symmetrically tracks what it received from us) —
//	a row we pushed is THEIR receive-problem, and their next export to us
//	sees our rows through OUR anchor tracking of them. Anti-reflow makes
//	everything idempotent anyway: an echo row is same-id + equal-anchor →
//	no-op.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// SyncSessionStats reports one bidirectional peer session.
type SyncSessionStats struct {
	Peer        string
	PushedRows  int                  // rows we sent (their merge counts them)
	PullStats   *SyncTaskImportStats // our merge of the peer's delta
	PullAnchor  float64              // peer-domain anchor we advanced to
	PushAck     bool                 // the peer acknowledged our push
	PushAnchor  float64              // the anchor our pushed delta ended at
	StagedFile  string               // where the peer envelope was staged
	DurationSec float64
	// LegBoundSec = the absolute ceiling the ssh leg ran under, resolved once
	// so the report states it (a full pass's ceiling is computed from the
	// work, so an operator needs to see the number the run used).
	LegBoundSec float64
}

// SyncPeerSession runs one bidirectional delta exchange with peer.
//
// Transport (epic ⑤): `ssh <peer> gopi task sync --serve-remote` with the
// local delta patch on stdin; the remote merges stdin and prints its delta
// envelope on stdout. ssh runs BatchMode (a daemon must never prompt).
func SyncPeerSession(peer string, cliPeers []string) (*SyncSessionStats, error) {
	return syncPeerSession(peer, cliPeers, "")
}

// SyncPeerSessionTags is SyncPeerSession with a partition filter (the CLI's
// --tags; "" = the whole window). The filter rides in the patch, so the
// peer's push is partitioned the same way (epic ③ 模块分区对称).
func SyncPeerSessionTags(peer string, cliPeers []string, tagsFilter string) (*SyncSessionStats, error) {
	return syncPeerSession(peer, cliPeers, tagsFilter)
}

// syncPeerSession is SyncPeerSession with an explicit tags partition filter
// ("" = the whole window). CLI `task sync --peer p --tags module:x` lands
// here; serve ticks always pass "" (auto-sync is whole-window — a partition
// filter left unattended would silently strand a module's rows).
func syncPeerSession(peer string, cliPeers []string, tagsFilter string) (*SyncSessionStats, error) {
	started := time.Now()
	// The declared-topology gate (Rule 14: ONE definition of the rule and its
	// wording — peer_topology.go). Reached before the transport and before
	// any export, so an undeclared alias costs no outbound connection.
	if err := syncPeerDeclared(peer, cliPeers); err != nil {
		return nil, err
	}

	stats := &SyncSessionStats{Peer: peer}

	// Push side: our delta FOR this peer. The push piggybacks on the same
	// session so the peer advances ITS receive-anchor for us when it
	// merges; our own anchor tracks what we RECEIVE (see file top).
	// TagsFilter (--tags) rides IN the patch (patch.TagsFilter): the peer's
	// serve-remote reads it and filters ITS push the same way — a partition
	// session is symmetric (epic ③), and the filter survives the ssh leg
	// without new protocol state.
	_, ourAnchor, err := syncStateListPeer(peer)
	if err != nil {
		return nil, fmt.Errorf("memsync sync: %w", err)
	}
	patch, exportStats, err := SyncTaskExportDelta(SyncTaskDeltaOptions{AfterSource: ourAnchor, Tags: tagsFilter, Peer: peer})
	if err != nil {
		return nil, fmt.Errorf("memsync sync: %w", err)
	}
	if tagsFilter != "" {
		patch.TagsFilter = tagsFilter // the peer mirrors the partition on its push
	}
	pushBody, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("memsync sync: marshal: %w", err)
	}
	stats.PushedRows = len(patch.Rows)
	stats.PushAnchor = syncEnvAnchor(patch.Rows)
	_ = exportStats

	// The ssh leg: stdin = our patch, stdout = the peer's envelope. A DELTA
	// session ships a window and the peer applies a handful of rows, so its
	// ceiling is the stall detector (syncSSHTimeout) — the work-sized model
	// the FULL pass used died with that pass (memory_sync_full_bound.go,
	// deleted with the cross-node sync surface).
	stdout, err := syncSSHExchange(peer, pushBody, syncPhaseDelta, syncSSHTimeout, len(patch.Rows))
	stats.LegBoundSec = syncSSHTimeout.Seconds()
	if err != nil {
		return nil, fmt.Errorf("memsync sync: %w", err)
	}

	// Stage the peer's envelope to a temp file — the import path is
	// file-shaped end to end (epic: 传输编码 = M4.8's patch file).
	staged, err := scratch.CreateTemp(scratch.RuntimeClassMemsync, syncTempPrefix+"*.json")
	if err != nil {
		return nil, fmt.Errorf("memsync sync: stage: %w", err)
	}
	defer os.Remove(staged.Name())
	stats.StagedFile = staged.Name()
	if _, err := staged.Write(bytes.TrimSpace(stdout)); err != nil {
		staged.Close() //stylegate:ignore staging file closed before use; the consuming call is checked
		return nil, fmt.Errorf("memsync sync: stage: %w", err)
	}
	if err := staged.Close(); err != nil {
		return nil, fmt.Errorf("memsync sync: stage: %w", err)
	}

	// Pull side: parse + merge the peer's delta, then commit the anchor.
	envelopeData, err := os.ReadFile(staged.Name())
	if err != nil {
		return nil, fmt.Errorf("memsync sync: %w", err)
	}
	var patchIn SyncTaskPatch
	if err := json.Unmarshal(envelopeData, &patchIn); err != nil {
		return nil, fmt.Errorf("memsync sync: peer envelope: %w", err)
	}
	envelope, err := SyncDeltaEnvelopeFromPatch(&patchIn)
	if err != nil {
		return nil, fmt.Errorf("memsync sync: peer envelope: %w", err)
	}
	pullStats, err := SyncTaskImportDelta(&envelope.SyncTaskPatch)
	if err != nil {
		return nil, fmt.Errorf("memsync sync: merge: %w", err)
	}
	stats.PullStats = pullStats
	stats.PullAnchor = envelope.Anchor
	stats.PushAck = true // the peer answered = our push was consumed

	// Anchor last (epic ⑥): both directions succeeded → advance — but
	// THROUGH the M4.24 guard (reconcile-before-increment): a pending-count
	// mismatch means the pre-anchor timeline is not identical and the
	// advance is refused (the anchor stays, the next session re-pulls).
	if err := guardSyncStateCommit(peer, envelope.Anchor); err != nil {
		return nil, fmt.Errorf("memsync sync: %w", err)
	}
	stats.DurationSec = time.Since(started).Seconds()
	return stats, nil
}

// syncSSHExchangeHook replaces the ssh transport when non-nil (the
// HarvestOptions.Fetch seam pattern): tests run the peer's real
// SyncRemoteServe in-process — the session's full engine path with the byte
// pipe abstracted. Production never sets it (nil → the ssh exec below).
// It is deliberately package-level: a tick session is process-global state.
var syncSSHExchangeHook func(peer string, pushBody []byte) ([]byte, error)

// syncSSHExchange pipes pushBody to `ssh <peer> <remote sync handler>` and
// returns stdout (the peer's delta envelope).
//
// `bound` is the leg's ceiling and the CALLER owns it: a delta session ships a
// window and the peer applies a handful of rows, so syncSSHTimeout (a stall
// detector) is the ceiling. The anchor-bypassing FULL shape that needed a
// work-sized bound was DELETED with the cross-node sync surface (2026-11-05);
// this parameter stays because the ceiling is still the caller's to name.
//
// `phase` names the pass in the leg's own error text, so an elapsed ceiling
// reads as "the delta leg did not finish within …" instead of ssh's opaque
// `signal: killed`.
//
// `rows` is the number of ledger rows the patch replays — reported by the
// error.
func syncSSHExchange(peer string, pushBody []byte, phase string, bound time.Duration, rows int) ([]byte, error) {
	// Peer-alias guard BEFORE the test seam: the option-shaped alias
	// (-oProxyCommand=…) must be refused on every path, hooks included.
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("memsync sync: %w", err)
	}
	if syncSSHExchangeHook != nil {
		return syncSSHExchangeHook(peer, pushBody)
	}
	// THE PRODUCTION PATH IS GONE, and this is where it used to be: the leg
	// composed `ssh <peer> gopi task sync --serve-remote`, a DELETED verb
	// (2026-11-05) that now answers with a removal notice + exit 1. Dialling
	// would turn a removal into a protocol error at the far end of a
	// connection, so the refusal happens HERE — before any ssh argv exists,
	// with the phase/bound/row reporting machinery below left unreachable on
	// purpose (remote_leg_retired.go states the mechanism once).
	return nil, remoteLegRetired("memory sync session ("+phase+")", peer)
}

// NOTE ON WHAT IS NOT HERE: syncSSHTimeoutError used to rewrite the opaque
// kill of OUR OWN ceiling into an actionable sentence ("did not finish within
// …", with the bound, the elapsed time, the phase and the row/byte counts). Its
// only caller was the ssh invocation above, so when that invocation went it
// went with it (Rule 43: a symbol whose remaining references all live inside
// the mechanism being deleted is deleted WITH it). The delta leg's ceiling is
// now unreachable in production, and a function kept for a path that cannot run
// is a claim that the path runs.

// SyncRemoteServe is the PEER side of a session (gopi task sync
// --serve-remote): read the initiator's patch from stdin, merge it, then
// print OUR delta envelope (our own version-2 patch) on stdout. The
// initiator's receive-anchor for us is ITS committed anchor — but the
// peer tracks its own receives symmetrically: it merges stdin rows and
// exports rows newer than what it last received FROM the initiator.
func SyncRemoteServe(stdin []byte) ([]byte, error) {
	var patchIn SyncTaskPatch
	if err := json.Unmarshal(bytes.TrimSpace(stdin), &patchIn); err != nil {
		return nil, fmt.Errorf("memsync serve-remote: parse stdin: %w", err)
	}
	if err := syncCheckPatchVersion("serve-remote", patchIn.Version, syncPatchVersionDelta); err != nil {
		return nil, err
	}
	stats, err := SyncTaskImportDelta(&patchIn)
	if err != nil {
		return nil, err
	}
	if stats.RowsRefused > 0 {
		// Refused rows are logged to stderr (the initiator surfaces them);
		// they do NOT abort the session — the pull still runs.
		for _, e := range stats.Errors {
			fmt.Fprintln(os.Stderr, "memsync serve-remote: "+e)
		}
	}
	// Our push side: export rows newer than the anchor we last received
	// from this initiator (identified by the patch's Node). The
	// initiator's TagsFilter (patch.TagsFilter) partitions OUR push too —
	// a --tags session is symmetric (epic ③).
	fromPeer := patchIn.Node
	if fromPeer == "" {
		fromPeer = syncTaskUnknownNode
	}
	_, anchor, err := syncStateListPeer(fromPeer)
	if err != nil {
		return nil, err
	}
	// A FULL pass (patch.Full) mirrors the initiator's anchor bypass: our
	// push ships EVERYTHING too (both sides re-deliver their whole ledger
	// — the rank merge dedupes the replay). A delta pass keeps its window.
	// Peer=fromPeer (the initiator identified by patch.Node) drives the
	// M4.24 anti-reflow gate: rows carrying from-<initiator> are excluded
	// (relaying them back IS the reflow); our own tags/status advances on
	// rows the initiator once sent us now travel (the dead-wire fix).
	after := anchor
	if patchIn.Full {
		after = 0
	}
	out, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{AfterSource: after, Tags: strings.TrimSpace(patchIn.TagsFilter), Peer: fromPeer})
	if err != nil {
		return nil, err
	}
	// Commit the receive anchor for the initiator (their clock domain —
	// the max updated_at among the rows they sent us).
	data, err := json.Marshal(&syncEnvEnvelope{SyncTaskPatch: *out, Anchor: syncEnvAnchor(patchIn.Rows)})
	if err != nil {
		return nil, err
	}
	if err := syncStateCommit(fromPeer, syncEnvAnchor(patchIn.Rows)); err != nil {
		return nil, err
	}
	return data, nil
}

// syncStateListPeer is a one-peer SyncStateList convenience (both anchors 0
// = never synced).
func syncStateListPeer(peer string) (lastSynced, lastSyncedSource float64, err error) {
	db, closeDB, err := taskSyncDB()
	if err != nil {
		return 0, 0, err
	}
	defer closeDB()
	return syncStateGet(context.Background(), db, peer)
}

// SyncPeerStatus renders one peer's anchor row for `gopi task sync --status`.
func SyncPeerStatus(peer string) (string, error) {
	synced, source, err := syncStateListPeer(peer)
	if err != nil {
		return "", err
	}
	if synced == 0 && source == 0 {
		return fmt.Sprintf("peer %s: never synced", peer), nil
	}
	return fmt.Sprintf("peer %s: anchor=%.0f last_synced=%s",
		peer, source, time.Unix(int64(synced), 0).Format(time.RFC3339)), nil
}
