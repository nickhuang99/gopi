// Package tools — memory_sync_reconcile.go: the M4.24 reconciliation layer
// (devdoc/sync-reconcile.md). The timestamp-delta protocol has one
// self-evident premise (nick: common sense — the anchor is a SYNCHRONIZED
// STARTING TIMELINE; rows on either side BEFORE the anchor must be
// identical). This file mechanizes that premise in three pieces:
//
//	① diff --peer  — pull the peer's FULL (task_id,status) row-set over
//	                  ssh (harvest's sqlite3 -json channel), compare against
//	                  ours both directions: missing ids per side + same-id
//	                  status mismatches. The reconciliation IS the anchor
//	                  semantics' self-check made executable.
//	② (the anchor-bypassing FULL session, `SyncFullReconcile`) is DELETED —
//	                  the cross-node sync surface went on 2026-11-05, so no
//	                  production caller remained. Work crosses hosts through
//	                  the git ref a row's deliverable publishes to.
//	③ anchor guard — an anchor advance is only lawful when "everything
//	                  before this moment is identical on both sides".
//	                  Before committing an anchor we compare pending counts;
//	                  a mismatch → the anchor DOES NOT move + a warning.
//	                  Reconciliation takes precedence over increments.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// Reconciliation tunables — Rule 1: every literal in one place.
const (
	// reconSSHTimeout bounds the diff's ssh leg (one sqlite3 -json query —
	// a bounded catalog read; the session's syncSSHTimeout is the analog).
	reconSSHTimeout = 15 * time.Second

	// reconSSHConnectTimeout is the ssh TCP connect timeout (-o value).
	reconSSHConnectTimeout = "10"

	// reconRemoteDB is the peer-side DB path (harvest's path, verbatim —
	// one schema, one remote layout).
	reconRemoteDB = harvestRemoteDBPath

	// gopiAnchorGuardEnv is the anchor guard's sim/sandbox escape hatch:
	// "0" disables the pre-anchor comparison (Rule 1b: the literal is
	// referenced, never typed at the use site).
	gopiAnchorGuardEnv = "GOPI_ANCHOR_GUARD"
)

// TaskDiffStats is the both-direction set comparison of one peer.
type TaskDiffStats struct {
	Peer           string
	LocalRows      int // our full (task_id,status) row count
	PeerRows       int // the peer's full row count
	MissingOnPeer  []string
	MissingOnLocal []string
	StatusMismatch []string // "task_id: local_status≠peer_status"
	DiffCount      int      // total differing rows (the headline number)
}

// IsClean reports whether the two ledgers agree row-for-row (the
// "identical before the anchor" premise, verified).
//
// It reads the ONE definition of the total (DiffCountTotal, Rule 14), not the
// stored DiffCount field: a report whose classes were populated but whose
// headline number was not set would otherwise announce "the ledgers agree"
// while carrying non-empty StatusMismatch — the same defect class as the alarm
// field this change repairs, one layer down. Measured 2026-10-27 on a
// hand-built report (Stats{StatusMismatch: 4 entries}) — Summary() answered
// "ledgers agree (11 rows)".
func (d *TaskDiffStats) IsClean() bool { return d.DiffCountTotal() == 0 }

// DiffCountTotal is THE definition of "how many rows differ" — the sum of the
// three classes reconCompare accumulates. Rule 14: one definition, and the
// count is DERIVED from the class slices rather than stored beside them, so a
// reader can never see a total that disagrees with the parts it is printed
// next to. Every consumer (Stats.DiffCount, the alarm, the summary) refers to
// this function; none re-adds the classes itself.
func (d *TaskDiffStats) DiffCountTotal() int {
	return len(d.MissingOnPeer) + len(d.MissingOnLocal) + len(d.StatusMismatch)
}

// WithDiffCount re-derives DiffCount from the current class slices and returns
// the receiver, so the headline number is always the total this report
// carries.
func (d *TaskDiffStats) WithDiffCount() *TaskDiffStats {
	d.DiffCount = d.DiffCountTotal()
	return d
}

// TaskDiffPeer runs the ① reconciliation: our full row-set vs the peer's,
// both directions. Transport = harvest's ssh sqlite3 -json channel (one
// catalog query, no session machinery — a diff must never write).
//
// the DECLARED-TOPOLOGY gate comes first, exactly as it does on every other
// peer-taking verb: an alias the node does not declare is refused with the
// whitelist message (RequireDeclaredPeer, peer_topology.go) instead of being
// dialed and having ssh's exit code reported back — the operator is told the
// real condition ("this alias is not part of the topology"), and an
// undeclared alias never makes an outbound connection attempt.
func TaskDiffPeer(peer string) (*TaskDiffStats, error) {
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	if err := RequireDeclaredPeer(peer, nil); err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	peerRows, err := reconRemoteRowSets(peer, reconRemoteDB)
	if err != nil {
		return nil, err
	}
	localRows, err := reconLocalRowSets()
	if err != nil {
		return nil, err
	}
	return reconCompare(peer, localRows, peerRows), nil
}

// reconRowSet is one row of the compared catalog: id + status only.
type reconRowSet struct {
	TaskID string `json:"task_id"` // sqlite3 -json emits snake_case keys
	Status string `json:"status"`
}

// reconLocalRowSets reads OUR full (task_id,status) catalog.
func reconLocalRowSets() ([]reconRowSet, error) {
	db, closeDB, err := taskSyncDB()
	if err != nil {
		return nil, err
	}
	defer closeDB()
	rows, err := db.QueryContext(context.Background(),
		`SELECT task_id, status FROM tasks ORDER BY task_id`)
	if err != nil {
		return nil, fmt.Errorf("diff: local catalog: %w", err)
	}
	defer rows.Close()
	var out []reconRowSet
	for rows.Next() {
		var r reconRowSet
		if err := rows.Scan(&r.TaskID, &r.Status); err != nil {
			return nil, fmt.Errorf("diff: local scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// reconRemoteRowSetsHook replaces the peer-side READ of the catalog diff when
// non-nil (the ledgerAgreementPeerRowsHook / reconRemotePendingSetHook seam
// pattern): tests inject the peer's catalog in-process so the real local read
// + the real reconCompare run, with only the ssh byte pipe replaced. It sits
// AFTER both guards (alias syntax + declared topology), so a refusal test sees
// the guard fire and observes that the transport was never entered.
// Production never sets it (nil → the real ssh exec below).
var reconRemoteRowSetsHook func(peer string) ([]reconRowSet, error)

// SetReconRemoteRowSetsForTest installs (or, with nil, removes) the
// peer-catalog transport seam. The argument is the ALREADY-VALIDATED alias
// (the hook sees exactly what the ssh leg would have been handed).
func SetReconRemoteRowSetsForTest(fn func(peer string) ([]reconRowSet, error)) {
	reconRemoteRowSetsHook = fn
}

// reconRemoteRowSets pulls the PEER's full (task_id,status) catalog over
// ssh sqlite3 -json — the harvest channel (quote-free SQL, one argv
// element; memory_sync_task.go harvestRemoteTasks is the precedent).
func reconRemoteRowSets(peer, remoteDB string) ([]reconRowSet, error) {
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	if reconRemoteRowSetsHook != nil {
		return reconRemoteRowSetsHook(peer)
	}
	ctx, cancel := context.WithTimeout(context.Background(), reconSSHTimeout)
	defer cancel()
	query := fmt.Sprintf(`sqlite3 -json %s %s`, remoteDB,
		harvestShellQuote(`SELECT task_id, status FROM tasks ORDER BY task_id`))
	out, err := exec.CommandContext(ctx, "ssh",
		"-o", fmt.Sprintf("ConnectTimeout=%s", reconSSHConnectTimeout),
		"-o", "BatchMode=yes", peer, query).Output()
	if err != nil {
		return nil, fmt.Errorf("diff: ssh %s: %w", peer, err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return []reconRowSet{}, nil // sqlite3 -json prints nothing on 0 rows
	}
	var rows []reconRowSet
	if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
		return nil, fmt.Errorf("diff: peer %s returned unparsable JSON: %w", peer, err)
	}
	return rows, nil
}

// reconCompare is the pure set logic (unit-tested without ssh): both-
// direction id-set difference + same-id status mismatch. Statuses compare
// trimmed (the ledger's own writes never carry padding).
func reconCompare(peer string, local, remote []reconRowSet) *TaskDiffStats {
	stats := &TaskDiffStats{Peer: peer, LocalRows: len(local), PeerRows: len(remote)}
	localIdx := make(map[string]string, len(local))
	for _, r := range local {
		localIdx[r.TaskID] = strings.TrimSpace(r.Status)
	}
	remoteIdx := make(map[string]string, len(remote))
	for _, r := range remote {
		remoteIdx[r.TaskID] = strings.TrimSpace(r.Status)
	}
	for _, r := range local {
		peerStatus, ok := remoteIdx[r.TaskID]
		if !ok {
			stats.MissingOnPeer = append(stats.MissingOnPeer, r.TaskID)
			continue
		}
		if peerStatus != strings.TrimSpace(r.Status) {
			stats.StatusMismatch = append(stats.StatusMismatch,
				fmt.Sprintf("%s: %s≠%s", r.TaskID, strings.TrimSpace(r.Status), peerStatus))
		}
	}
	for _, r := range remote {
		if _, ok := localIdx[r.TaskID]; !ok {
			stats.MissingOnLocal = append(stats.MissingOnLocal, r.TaskID)
		}
	}
	return stats.WithDiffCount()
}

func syncImportEnvelope(stdout []byte, stats *SyncSessionStats) (*SyncTaskImportStats, float64, error) {
	staged, err := scratch.CreateTemp(scratch.RuntimeClassMemsync, syncTempPrefix+"*.json")
	if err != nil {
		return nil, 0, fmt.Errorf("memsync sync: stage: %w", err)
	}
	defer os.Remove(staged.Name())
	stats.StagedFile = staged.Name()
	if _, err := staged.Write(bytes.TrimSpace(stdout)); err != nil {
		staged.Close() //stylegate:ignore staging file closed before use; the consuming call is checked
		return nil, 0, fmt.Errorf("memsync sync: stage: %w", err)
	}
	if err := staged.Close(); err != nil {
		return nil, 0, fmt.Errorf("memsync sync: stage: %w", err)
	}
	envelopeData, err := os.ReadFile(staged.Name())
	if err != nil {
		return nil, 0, fmt.Errorf("memsync sync: %w", err)
	}
	var patchIn SyncTaskPatch
	if err := json.Unmarshal(envelopeData, &patchIn); err != nil {
		return nil, 0, fmt.Errorf("memsync sync: peer envelope: %w", err)
	}
	envelope, err := SyncDeltaEnvelopeFromPatch(&patchIn)
	if err != nil {
		return nil, 0, fmt.Errorf("memsync sync: peer envelope: %w", err)
	}
	pullStats, err := SyncTaskImportDelta(&envelope.SyncTaskPatch)
	if err != nil {
		return nil, 0, fmt.Errorf("memsync sync: merge: %w", err)
	}
	return pullStats, envelope.Anchor, nil
}

// syncPeerDeclared is the whitelist gate both session paths share
// (sync topology is declared, never discovered). The decision and the
// wording live in peer_topology.go (Rule 14) — this wrapper only adds the
// session verbs' own phase prefix, which is the shape their callers and
// tests already expect.
func syncPeerDeclared(peer string, cliPeers []string) error {
	if err := RequireDeclaredPeer(peer, cliPeers); err != nil {
		return fmt.Errorf("memsync sync: %w", err)
	}
	return nil
}

// syncPeerDeclaredIn reports whether peer is a member of a peer list already
// resolved (the pure half of syncPeerDeclared, shared with the push-only
// face so both enforce ONE membership contract).
func syncPeerDeclaredIn(peers []string, peer string) bool {
	for _, p := range peers {
		if p == peer {
			return true
		}
	}
	return false
}

// syncAnchorGuard is ③: the anchor-advance precondition (nick's premise,
// mechanized). An anchor may only advance when our PENDING count and the
// peer's agree on what existed before that moment — the cheap, writable
// proof of "identical before the anchor". Returns an error naming the
// mismatch; the caller keeps the old anchor (reconciliation takes
// precedence over increments — a silent loss is worse than a delayed one).
//
// pending-only by design: terminal rows migrate freely across a session
// (the pull happens BEFORE the guard), so the pre-anchor difference that
// actually matters is live work the delta window never delivered.
func syncAnchorGuard(peer string, newAnchor float64) error {
	if newAnchor <= 0 {
		return nil // an empty/zero anchor commits freely (nothing to guard)
	}
	// Test/sim escape hatch (documented): GOPI_ANCHOR_GUARD=0 disables the
	// guard (sandbox topologies carry one-sided rows by design). The
	// default (unset or any other value) keeps the guard ON — production
	// never sets this.
	if os.Getenv(gopiAnchorGuardEnv) == "0" {
		return nil
	}
	localPending, err := reconLocalPendingSet()
	if err != nil {
		return err
	}
	remotePending, err := reconRemotePendingSet(peer)
	if err != nil {
		return fmt.Errorf("anchor guard: %w", err)
	}
	if localPending == remotePending {
		return nil
	}
	return fmt.Errorf(
		"anchor guard: refusing advance to %.0f — pending counts differ (local=%d peer=%d): the pre-anchor timeline is NOT identical (rows created on one side before the peer's anchor moved sit outside the delta window; run `gopi task diff --peer %s` then `gopi task sync --full %s`)",
		newAnchor, localPending, remotePending, peer, peer)
}

// reconLocalPendingSet counts OUR pending rows (the guard's local half).
func reconLocalPendingSet() (int, error) {
	db, closeDB, err := taskSyncDB()
	if err != nil {
		return 0, err
	}
	defer closeDB()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM tasks WHERE status='pending'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("anchor guard: local pending count: %w", err)
	}
	return n, nil
}

// reconRemotePendingSetHook replaces the ssh transport of the anchor guard's
// REMOTE half when non-nil (the syncSSHExchangeHook seam pattern in
// memory_sync_session.go): tests inject the peer's pending count in-process,
// so syncAnchorGuard's compare/mismatch/repair-hint branches run without an
// ssh leg. Production never sets it (nil → the ssh exec below). Deliberately
// package-level: the guard reads process-global topology state.
var reconRemotePendingSetHook func(peer string) (int, error)

// reconRemotePendingSet counts the PEER's pending rows (ssh sqlite3 -json,
// the diff channel; the SQL is quote-free per the harvest rule).
func reconRemotePendingSet(peer string) (int, error) {
	if reconRemotePendingSetHook != nil {
		return reconRemotePendingSetHook(peer)
	}
	// peer-alias guard (ssh option-injection): only the real ssh leg needs
	// the canonical whitelisted alias, so the test seam above stays free to
	// inject fabricated peer names.
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return 0, fmt.Errorf("anchor guard: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), reconSSHTimeout)
	defer cancel()
	query := fmt.Sprintf(`sqlite3 -json %s %s`, reconRemoteDB,
		harvestShellQuote(`SELECT COUNT(*) AS n FROM tasks WHERE status='pending'`))
	out, err := exec.CommandContext(ctx, "ssh",
		"-o", fmt.Sprintf("ConnectTimeout=%s", reconSSHConnectTimeout),
		"-o", "BatchMode=yes", peer, query).Output()
	if err != nil {
		return 0, fmt.Errorf("ssh %s: %w", peer, err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0, nil
	}
	var rows []struct {
		N int `json:"n"`
	}
	if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
		return 0, fmt.Errorf("peer %s pending count unparsable: %w", peer, err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].N, nil
}

// guardSyncStateCommit is the delta session's commit wrapped with ③: the
// anchor advances ONLY through the guard.
func guardSyncStateCommit(peer string, peerAnchor float64) error {
	if err := syncAnchorGuard(peer, peerAnchor); err != nil {
		return err
	}
	return syncStateCommit(peer, peerAnchor)
}
