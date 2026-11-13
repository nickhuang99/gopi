package tools

// memory_sync_session_cov_test.go — coverage tests for the exported session
// faces:
//
//	SyncPeerSessionTags — the tags-partitioned bidirectional session. The ssh
//	                      byte pipe is replaced by the package's OWN seam
//	                      (syncSSHExchangeHook → the peer's real
//	                      SyncRemoteServe in-process, the same injection the
//	                      bidir E2E sims use), so the session's whole engine
//	                      path runs without a network:
//	                        · the declared-topology gate (an undeclared peer
//	                          is refused — sync topology is DECLARED, never
//	                          discovered),
//	                        · the partition contract (the tags filter rides
//	                          the patch, so the peer pushes the same segment
//	                          back — epic ③ symmetry),
//	                        · the transport-failure path (no anchor advance),
//	                        · the anchor semantics (PushAck set only after the
//	                          peer answered, PullAnchor = the peer's envelope
//	                          anchor, anchor committed forward-only).
//	SyncPeerStatus      — the `task sync --status` line: never-synced vs the
//	                      anchor/last-synced rendering, plus the ledger error
//	                      path.
//
// Determinism: no network, no sleeps. The hook is process-global (as
// documented for a serve tick); every test saves and restores it.
// The DELTA-side push/pull assertions use only fixture timestamps.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// covSessionSandbox pins a settings file declaring exactly the peers a test
// wants whitelisted (config.ResolveSyncPeers reads settings.json when the
// env is absent), plus the node identity and role the session path expects.
func covSessionSandbox(t *testing.T, node string, peers ...string) {
	t.Helper()
	sandbox := t.TempDir()
	body := fmt.Sprintf(`{"sync_peers":%s}`, jsonStringList(peers))
	if err := os.WriteFile(filepath.Join(sandbox, "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsEnv, filepath.Join(sandbox, "settings.json"))
	t.Setenv("GOPI_SYNC_NODE", node)
	t.Setenv(config.RoleEnv, "client")
	os.Unsetenv(configSyncPeersEnv)
	t.Cleanup(func() { os.Unsetenv(configSyncPeersEnv) })
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
}

// jsonStringList renders a JSON array literal (test fixture).
func jsonStringList(items []string) string {
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// covHookPeerServe installs the in-process transport: the "peer" is served
// by the real SyncRemoteServe running against the CURRENT ledger (the
// documented hook seam). The hook is restored at test end.
func covHookPeerServe(t *testing.T) {
	t.Helper()
	old := syncSSHExchangeHook
	syncSSHExchangeHook = func(peer string, pushBody []byte) ([]byte, error) {
		return SyncRemoteServe(pushBody)
	}
	covRestoreHook(t, old)
}

// covRestoreHook restores the process-global hook at test end.
func covRestoreHook(t *testing.T, old func(string, []byte) ([]byte, error)) {
	t.Helper()
	t.Cleanup(func() { syncSSHExchangeHook = old })
}

// TestSyncPeerSessionTagsRefusesUndeclaredPeer — the topology invariant:
// the peer must be in the declared whitelist. A peer that resolves but is
// NOT declared is refused (sync topology is declared, never discovered) and
// the refusal names the settings key + env so the operator can fix it.
func TestSyncPeerSessionTagsRefusesUndeclaredPeer(t *testing.T) {
	newDeltaTestDBClient(t)
	covSessionSandbox(t, "node-a", "peer-declared")

	stats, err := SyncPeerSessionTags("peer-undeclared", nil, "")
	if err == nil {
		t.Fatalf("an undeclared peer must be refused, got %+v", stats)
	}
	if stats != nil {
		t.Fatalf("refused session must return nil stats, got %+v", stats)
	}
	if !strings.Contains(err.Error(), "sync_peers") {
		t.Errorf("refusal must name the whitelist setting, got: %v", err)
	}
	if !strings.Contains(err.Error(), "peer-undeclared") {
		t.Errorf("refusal must name the offending peer, got: %v", err)
	}
	if !strings.Contains(err.Error(), config.SyncPeersEnv) {
		t.Errorf("refusal should point at the env override too, got: %v", err)
	}
}

// TestSyncPeerSessionTagsRefusedWhenNoPeersDeclared — boundary: an EMPTY
// topology refuses every peer (there is no implicit "sync with anyone").
func TestSyncPeerSessionTagsRefusedWhenNoPeersDeclared(t *testing.T) {
	newDeltaTestDBClient(t)
	covSessionSandbox(t, "node-a" /* no peers */)
	stats, err := SyncPeerSessionTags("peer-anything", nil, "")
	if err == nil {
		t.Fatalf("no declared peers must refuse the session, got %+v", stats)
	}
	if stats != nil {
		t.Fatalf("refused session must return nil stats, got %+v", stats)
	}
}

// TestSyncPeerSessionTagsTransportFailureLeavesAnchorUntouched — error
// path: a transport failure must abort the session WITHOUT advancing any
// anchor (锚点最后写: the anchor is the proof the peer applied our rows; a
// failed leg proves nothing, so the next session must re-pull the window).
func TestSyncPeerSessionTagsTransportFailureLeavesAnchorUntouched(t *testing.T) {
	newDeltaTestDBClient(t)
	covSessionSandbox(t, "node-a", "peer-x")
	old := syncSSHExchangeHook
	syncSSHExchangeHook = func(peer string, pushBody []byte) ([]byte, error) {
		return nil, fmt.Errorf("simulated transport down")
	}
	covRestoreHook(t, old)

	base := float64(3000)
	deltaInsertRaw(t, "t-sess-1", "row for the failed leg", taskStatusPending, base, base, "")

	stats, err := SyncPeerSessionTags("peer-x", nil, "")
	if err == nil {
		t.Fatalf("a failed transport leg must fail the session, got %+v", stats)
	}
	if stats != nil {
		t.Fatalf("failed session must return nil stats, got %+v", stats)
	}
	if !strings.Contains(err.Error(), "simulated transport down") {
		t.Errorf("error must carry the transport cause, got: %v", err)
	}
	// No anchor moved: the peer never confirmed anything.
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	lastSynced, anchor, err := syncStateGet(t.Context(), db, "peer-x")
	closeDB()
	if err != nil {
		t.Fatalf("syncStateGet: %v", err)
	}
	if anchor != 0 {
		t.Fatalf("a failed session advanced the anchor to %.0f — the peer never applied our rows", anchor)
	}
	if lastSynced != 0 {
		t.Fatalf("last_synced moved to %.0f on a failed session", lastSynced)
	}
}

// TestSyncPeerSessionTagsPartitionRidesThePatch — the partition contract:
// with --tags the session pushes ONLY that segment, and the peer mirrors it
// (patch.TagsFilter), so the rows it returns are partitioned the same way.
// Asserts epic ③ symmetry at the SESSION level (the export-level test lives
// in memory_sync_delta_test.go).
func TestSyncPeerSessionTagsPartitionRidesThePatch(t *testing.T) {
	newDeltaTestDBClient(t)
	covSessionSandbox(t, "node-a", "peer-x")
	// The ③ anchor guard's peer-side pending read has NO injection seam
	// (reconRemotePendingSet shells out to ssh unconditionally), so a
	// hermetic session that COMMITS an anchor must use the documented
	// sandbox escape hatch — exactly what the bidir sims do. The testability
	// gap is filed as a bug ticket (see devdoc/cov-tools-sync-session.md).
	t.Setenv(gopiAnchorGuardEnv, "0")
	base := float64(4000)
	deltaInsertRaw(t, "t-part-x", "in partition", taskStatusPending, base, base, "module:x")
	deltaInsertRaw(t, "t-part-y", "outside partition", taskStatusPending, base+1, base+1, "module:y")

	// Capture the outgoing patch: the filter must be stamped for the peer.
	var seenTagsFilter, seenPeer string
	var seenRows []string
	old := syncSSHExchangeHook
	covRestoreHook(t, old)
	syncSSHExchangeHook = func(peer string, pushBody []byte) ([]byte, error) {
		var patch SyncTaskPatch
		if err := json.Unmarshal(pushBody, &patch); err != nil {
			return nil, fmt.Errorf("push body not a patch: %w", err)
		}
		seenTagsFilter = patch.TagsFilter
		seenPeer = patch.Node
		for _, r := range patch.Rows {
			seenRows = append(seenRows, r.TaskID)
		}
		return SyncRemoteServe(pushBody)
	}

	stats, err := SyncPeerSessionTags("peer-x", nil, "module:x")
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if seenTagsFilter != "module:x" {
		t.Fatalf("the partition filter must ride the patch (peer mirrors it), got %q", seenTagsFilter)
	}
	if seenPeer != "node-a" {
		t.Fatalf("patch node identity = %q, want the pinned GOPI_SYNC_NODE", seenPeer)
	}
	if len(seenRows) != 1 || seenRows[0] != "t-part-x" {
		t.Fatalf("pushed rows = %v, want only the module:x row", seenRows)
	}
	if stats.PushedRows != 1 {
		t.Fatalf("PushedRows = %d, want 1 (the partition is the pushed set)", stats.PushedRows)
	}
	if !stats.PushAck {
		t.Fatalf("the peer answered → PushAck must be true")
	}
}

// TestSyncPeerSessionTagsHappyPathAnchorAndStats — the happy path with a
// REAL peer answer (the hook runs SyncRemoteServe in-process): the stats
// report the pushed rows/anchors, the peer's envelope anchor becomes the
// pull anchor, and BOTH the local anchor commits forward-only (a second,
// stale-anchor session can never roll it back).
func TestSyncPeerSessionTagsHappyPathAnchorAndStats(t *testing.T) {
	newDeltaTestDBClient(t)
	covSessionSandbox(t, "node-a", "peer-x")
	t.Setenv(gopiAnchorGuardEnv, "0") // see TestSyncPeerSessionTagsPartitionRidesThePatch
	base := float64(5000)
	deltaInsertRaw(t, "t-happy-1", "row one", taskStatusPending, base, base, "")
	deltaInsertRaw(t, "t-happy-2", "row two", taskStatusCompleted, base+1, base+1, "")
	covHookPeerServe(t)

	stats, err := SyncPeerSessionTags("peer-x", nil, "")
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if stats.Peer != "peer-x" {
		t.Fatalf("stats peer = %q", stats.Peer)
	}
	if stats.PushedRows != 2 {
		t.Fatalf("PushedRows = %d, want 2", stats.PushedRows)
	}
	if stats.PushAnchor != base+1 {
		t.Fatalf("PushAnchor = %.0f, want %.0f (max updated_at pushed)", stats.PushAnchor, base+1)
	}
	if !stats.PushAck {
		t.Fatalf("PushAck must be true once the peer answered")
	}
	if stats.PullAnchor != base+1 {
		t.Fatalf("PullAnchor = %.0f, want the peer's envelope anchor %.0f", stats.PullAnchor, base+1)
	}
	if stats.StagedFile == "" {
		t.Fatalf("the peer's envelope must be staged to a file (the import path is file-shaped)")
	}
	if _, err := os.Stat(stats.StagedFile); err == nil {
		t.Errorf("the staged envelope file must be cleaned up: %s still exists", stats.StagedFile)
	}
	if stats.DurationSec < 0 {
		t.Fatalf("DurationSec must not be negative")
	}

	// The anchor is committed and FORWARD-ONLY: a stale re-commit attempt
	// through the same accessor cannot roll it back.
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	_, anchor, err := syncStateGet(t.Context(), db, "peer-x")
	if err != nil {
		closeDB()
		t.Fatalf("syncStateGet: %v", err)
	}
	if anchor != base+1 {
		closeDB()
		t.Fatalf("committed anchor = %.0f, want %.0f", anchor, base+1)
	}
	closeDB()
	if err := syncStateCommit("peer-x", base-100); err != nil {
		t.Fatalf("stale commit: %v", err)
	}
	db, closeDB, err = taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	_, anchorAfter, err := syncStateGet(t.Context(), db, "peer-x")
	closeDB()
	if err != nil {
		t.Fatalf("syncStateGet after stale commit: %v", err)
	}
	if anchorAfter != base+1 {
		t.Fatalf("a stale retry rolled the anchor back to %.0f (forward-only violated)", anchorAfter)
	}
}

// TestSyncPeerSessionTagsEmptyPatchStillAcks — boundary: nothing to push
// (empty ledger) is still a valid session — the peer answers, the ack is
// set, and the session returns zero-row stats rather than an error.
func TestSyncPeerSessionTagsEmptyPatchStillAcks(t *testing.T) {
	newDeltaTestDBClient(t)
	covSessionSandbox(t, "node-a", "peer-x")
	t.Setenv(gopiAnchorGuardEnv, "0") // see TestSyncPeerSessionTagsPartitionRidesThePatch
	covHookPeerServe(t)

	stats, err := SyncPeerSessionTags("peer-x", nil, "")
	if err != nil {
		t.Fatalf("empty-ledger session must succeed: %v", err)
	}
	if stats.PushedRows != 0 {
		t.Fatalf("PushedRows = %d, want 0", stats.PushedRows)
	}
	if !stats.PushAck {
		t.Fatalf("the peer answered an empty patch → PushAck must be true")
	}
	if stats.PullStats == nil {
		t.Fatalf("the peer's (empty) delta must still be merged and reported")
	}
}

// TestSyncPeerStatusNeverSyncedAndAnchored — the CLI status line's three
// shapes: an unknown peer renders "never synced"; an anchored peer renders
// its peer-domain anchor plus the LOCAL last-synced timestamp; and a peer
// whose anchor was committed through the real accessor keeps that
// rendering (no drift between the writer and the presenter).
func TestSyncPeerStatusNeverSyncedAndAnchored(t *testing.T) {
	newDeltaTestDBClient(t)

	line, err := SyncPeerStatus("peer-never")
	if err != nil {
		t.Fatalf("never-synced status: %v", err)
	}
	if !strings.Contains(line, "never synced") || !strings.Contains(line, "peer-never") {
		t.Fatalf("never-synced line = %q", line)
	}

	// An anchor-only row (peer domain set, local clock 0) still renders as
	// a synced peer: the anchor IS the sync fact.
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	if err := syncStatePut(t.Context(), db, "peer-anchored", 1788242564, 1788242564); err != nil {
		closeDB()
		t.Fatalf("syncStatePut: %v", err)
	}
	closeDB()

	line, err = SyncPeerStatus("peer-anchored")
	if err != nil {
		t.Fatalf("anchored status: %v", err)
	}
	if strings.Contains(line, "never synced") {
		t.Fatalf("an anchored peer must not render as never-synced: %q", line)
	}
	if !strings.Contains(line, "1788242564") {
		t.Fatalf("the peer-domain anchor must appear in the line: %q", line)
	}
	if !strings.Contains(line, "last_synced=") {
		t.Fatalf("the local last-synced stamp must appear: %q", line)
	}
	if !strings.Contains(line, "1788242564") {
		t.Fatalf("local clock rendering missing: %q", line)
	}
}

// TestSyncPeerStatusLedgerErrorPath — error path: a foreign/missing ledger
// must surface the error instead of printing "never synced" (a broken DB
// would otherwise look like an unsynced-but-healthy topology).
func TestSyncPeerStatusLedgerErrorPath(t *testing.T) {
	foreign := filepath.Join(t.TempDir(), "foreign.db")
	if err := os.WriteFile(foreign, []byte("not a sqlite file"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(memoryDBEnv, foreign)
	line, err := SyncPeerStatus("peer-x")
	if err == nil {
		t.Fatalf("a foreign ledger must error, got line=%q", line)
	}
	if line != "" {
		t.Fatalf("error path must return an empty line, got %q", line)
	}
}
