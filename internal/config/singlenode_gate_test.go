package config

import (
	"os"
	"testing"
)

// writeTempSettingsFile writes a settings JSON blob for the temp-dir tests.
func writeTempSettingsFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

// Single-node topology gate tests (epic 2026-09-02): HasSyncPeers is the
// single go/no-go for whether cross-node machinery (git-sync pass, claim
// remote leg, terminal push) may engage. Empty peers = single-node mode.

// TestHasSyncPeersFalseWhenEnvEmpty: GOPI_SYNC_PEERS empty (or absent) with
// no settings fallback ⇒ no peers ⇒ HasSyncPeers false ⇒ single-node.
// GOPI_SYNC_PEERS is the highest-precedence source, so unsetting it forces
// the "no peers" answer regardless of the ambient settings.json. Error
// resolution (missing settings file) must ALSO read "no peers" (fail-soft
// to the single-node default — never wedge a serve pass on a config slip).
func TestHasSyncPeersFalseWhenEnvEmpty(t *testing.T) {
	t.Setenv(SyncPeersEnv, "")
	// Point settings at a nonexistent file: ResolveSyncPeers would error,
	// and HasSyncPeers must treat that error as "no peers".
	t.Setenv("GOPI_SETTINGS", t.TempDir()+"/nonexistent.json")
	if HasSyncPeers() {
		t.Fatal("empty sync_peers must read as single-node (no peers)")
	}
}

// TestHasSyncPeersTrueWhenEnvSet: GOPI_SYNC_PEERS=a ⇒ a declared peer ⇒ the
// cross-node machinery may engage.
func TestHasSyncPeersTrueWhenEnvSet(t *testing.T) {
	t.Setenv(SyncPeersEnv, "peer1")
	if !HasSyncPeers() {
		t.Fatal("a declared GOPI_SYNC_PEERS peer must read as has-peers")
	}
}

// TestHasSyncPeersFalseWhenSettingsHasEmptyArray: the DEPLOYMENT shape — a
// real settings.json with `"sync_peers": []` (no env override) — must read
// as no-peer all the way through the file resolver. This is the exact
// single-node settings we ship (~/.gopi/settings.json sync_peers emptied).
func TestHasSyncPeersFalseWhenSettingsHasEmptyArray(t *testing.T) {
	// isolate from any GOPI_SYNC_PEERS env in the environment
	t.Setenv(SyncPeersEnv, "")
	path := t.TempDir() + "/settings.json"
	if err := writeTempSettingsFile(path, `{"sync_peers":[],"compute_nodes":[]}`); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", path)
	if HasSyncPeers() {
		t.Fatal("settings with empty sync_peers array must read as no-peer (single-node)")
	}
}

// TestHasSyncPeersTrueWhenSettingsHasPeer: a settings.json carrying a peer
// (no env override) ⇒ has peers. Confirms the file source feeds the gate.
func TestHasSyncPeersTrueWhenSettingsHasPeer(t *testing.T) {
	t.Setenv(SyncPeersEnv, "")
	path := t.TempDir() + "/settings.json"
	if err := writeTempSettingsFile(path, `{"sync_peers":["peer1"],"compute_nodes":[]}`); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", path)
	if !HasSyncPeers() {
		t.Fatal("settings with a declared peer must read as has-peers")
	}
}

// ---------------------------------------------------------------------------
// Memory-asset peer whitelist decoupling (2026-09-02 bugfix): memory_peers
// is a SEPARATE settings key from sync_peers so single-node task mode
// (sync_peers=[]) does not disable the deliberately-kept memory-asset sync.
// ---------------------------------------------------------------------------

// TestResolveMemoryPeersEmptyWhenEnvAndSettingsEmpty: no GOPI_MEMORY_PEERS,
// settings with no memory_peers key ⇒ empty ⇒ asset sync refuses all peers.
func TestResolveMemoryPeersEmptyWhenEnvAndSettingsEmpty(t *testing.T) {
	t.Setenv(MemoryPeersEnv, "")
	t.Setenv(SyncPeersEnv, "") // isolation: sync peers must not leak into memory peers
	path := t.TempDir() + "/settings.json"
	if err := writeTempSettingsFile(path, `{"sync_peers":["hp"],"compute_nodes":[]}`); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", path)
	peers, err := ResolveMemoryPeers()
	if err != nil {
		t.Fatalf("ResolveMemoryPeers: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("ResolveMemoryPeers = %v, want empty — sync_peers=[hp] must not license asset sync", peers)
	}
	if HasMemoryPeers() {
		t.Fatal("HasMemoryPeers must be false when only sync_peers is declared")
	}
}

// TestResolveMemoryPeersFromSettings: settings memory_peers=["hp"] with
// sync_peers=[] (the single-node task shape) ⇒ asset sync has its peer.
func TestResolveMemoryPeersFromSettings(t *testing.T) {
	t.Setenv(MemoryPeersEnv, "")
	t.Setenv(SyncPeersEnv, "")
	path := t.TempDir() + "/settings.json"
	if err := writeTempSettingsFile(path, `{"sync_peers":[],"memory_peers":["hp"],"compute_nodes":[]}`); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", path)
	peers, err := ResolveMemoryPeers()
	if err != nil {
		t.Fatalf("ResolveMemoryPeers: %v", err)
	}
	if len(peers) != 1 || peers[0] != "hp" {
		t.Fatalf("ResolveMemoryPeers = %v, want [hp]", peers)
	}
	// The decoupling invariant: HasSyncPeers false (task single-node) while
	// HasMemoryPeers true (asset sync available).
	if HasSyncPeers() {
		t.Fatal("sync_peers=[] must read as no task peers (single-node task mode)")
	}
	if !HasMemoryPeers() {
		t.Fatal("memory_peers=[hp] must read as has memory peers (asset sync available)")
	}
}

// TestResolveMemoryPeersEnvOverride: GOPI_MEMORY_PEERS beats settings
// memory_peers; and the env var only feeds the memory whitelist — it must
// never enable task sync (HasSyncPeers stays false with sync_peers=[]).
func TestResolveMemoryPeersEnvOverride(t *testing.T) {
	t.Setenv(MemoryPeersEnv, "peer1")
	t.Setenv(SyncPeersEnv, "")
	path := t.TempDir() + "/settings.json"
	if err := writeTempSettingsFile(path, `{"sync_peers":[],"memory_peers":["hp"]}`); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", path)
	peers, err := ResolveMemoryPeers()
	if err != nil {
		t.Fatalf("ResolveMemoryPeers: %v", err)
	}
	if len(peers) != 1 || peers[0] != "peer1" {
		t.Fatalf("ResolveMemoryPeers = %v, want [peer1] (env override)", peers)
	}
	if HasSyncPeers() {
		t.Fatal("GOPI_MEMORY_PEERS must not license task sync")
	}
}

// TestNormalizePeerListRejectsDuplicates: both whitelists share the
// normalizePeerList invariants (non-empty, no duplicates).
func TestNormalizePeerListRejectsDuplicates(t *testing.T) {
	if _, err := normalizePeerList("memory peer", []string{"hp", "hp"}); err == nil {
		t.Fatal("duplicate memory peer must error")
	}
	if _, err := normalizePeerList("memory peer", []string{"", "hp"}); err == nil {
		t.Fatal("empty memory peer must error")
	}
	if _, err := normalizePeerList("sync peer", []string{"hp", "hp"}); err == nil {
		t.Fatal("duplicate sync peer must error")
	}
}
