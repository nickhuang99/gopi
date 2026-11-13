package tools

// memory_sync_assets_test.go — the distilled_qa ASSET channel tests: two
// REAL MemoryStore DBs driven through the REAL session engine (export →
// ssh-pipe hook → peer serve handler → reply → local import), the
// established bidir-node E2E pattern (memory_sync_bidir_e2e_test.go)
// applied to the knowledge rows.
//
// Acceptance (epic):
//   ① A{1,2} B{2,3} → after sync both hold {1,2,3}; row 2 (same question,
//     divergent or identical) UNTOUCHED on both sides; second sync imports 0.
//   ② anti-reflow: rows imported from B into A are never re-pushed to B.
//   ③ provenance: imported rows carry node:<peer> in sources.
//   ④ idempotent re-run after convergence.
//
// Rows travel on the wide export gate (SyncExport's default: every LOCAL
// non-node:* row — brew, hermes-import and source-less early-dev alike,
// the same knowledge channel memsync --export uses); simulated stores
// therefore store with source "brew" (and non-brew rows in the wide-gate
// unit tests), exactly like memory_brew_run.go's MemoryStore calls.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// assetSimNode is one simulated node's knowledge store.
type assetSimNode struct {
	name   string
	dbPath string
}

// assetSim is a two-node in-process sim whose ssh leg is a hook that runs
// the peer's REAL serve handler (syncSSHExchangeHook — the same seam the
// task-sync session tests use).
type assetSim struct {
	t     *testing.T
	nodes map[string]*assetSimNode
}

// newAssetSim builds two real temp knowledge DBs (nodes "zbook" and "hp")
// and wires the ssh hook to route each peer name to its DB. The sim pins
// BOTH whitelists: memory_peers declares the asset peers (the channel now
// validates against memory_peers, decoupled 2026-09-02) and sync_peers
// stays declared for task-sync isolation — but the asset sim only ever
// exercises the memory_peers path.
func newAssetSim(t *testing.T) *assetSim {
	t.Helper()
	t.Setenv(config.MemoryPeersEnv, "zbook,hp")
	t.Setenv(config.SyncPeersEnv, "zbook,hp")
	t.Setenv("GOPI_SYNC_NODE", "zbook") // deterministic node identity
	t.Setenv(config.RoleEnv, "client")
	// Sandbox settings resolution: the ambient GOPI_SETTINGS points at the
	// live settings.json (which carries memory_peers=["hp"] post-bugfix).
	// A settings file with NO memory_peers key is what proves the
	// empty-whitelist refusal — point at an empty sandbox file instead.
	sandbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(sandbox, "settings.json"),
		[]byte(`{"sync_peers":[],"memory_peers":[],"compute_nodes":[]}`), 0o644); err != nil {
		t.Fatalf("write sandbox settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", filepath.Join(sandbox, "settings.json"))
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)

	s := &assetSim{t: t, nodes: map[string]*assetSimNode{}}
	for _, name := range []string{"zbook", "hp"} {
		path := filepath.Join(t.TempDir(), name, "memory.db")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		// Touch the schema now (the store DB initializes on first open).
		db, err := openStoreDB(context.Background(), path)
		if err != nil {
			t.Fatalf("init %s store: %v", name, err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		s.nodes[name] = &assetSimNode{name: name, dbPath: path}
	}

	oldHook := syncSSHExchangeHook
	syncSSHExchangeHook = func(peer string, pushBody []byte) ([]byte, error) {
		n := s.nodes[peer]
		if n == nil {
			return nil, fmt.Errorf("no simulated node %q", peer)
		}
		// A per-call env flip, so os.Setenv + an immediate restore — NOT
		// t.Setenv, whose undo runs at TEST CLEANUP and would outlive this
		// call. The restore is SnapshotEnv (os.LookupEnv-backed): an
		// os.Getenv-captured `prev` cannot restore an ABSENT variable — it
		// would leave GOPI_SYNC_NODE set to "" — and GOPI_SYNC_NODE also
		// pointed at a t.TempDir() the framework removes, so a leaked value is
		// a dead path for every sibling test in this binary.
		defer testutil.SnapshotEnv(s.t, "GOPI_MEMORY_DB")()
		defer testutil.SnapshotEnv(s.t, "GOPI_SYNC_NODE")()
		os.Setenv("GOPI_MEMORY_DB", n.dbPath) //stylegate:ignore peer identity for the sync exchange; the peer handler reads it, so the value is restored rather than scoped // env-leak-ok: SnapshotEnv restores on hook return, absence included
		os.Setenv("GOPI_SYNC_NODE", peer)     //stylegate:ignore peer identity for the sync exchange; the peer handler reads it, so the value is restored rather than scoped // env-leak-ok: SnapshotEnv restores on hook return, absence included
		return MemoryAssetsServeRemote(pushBody, "")
	}
	t.Cleanup(func() { syncSSHExchangeHook = oldHook })
	return s
}

// use points the shared DB resolution at this node (t.Setenv pins are
// per-test-scoped — call before every cluster of operations on a node).
func (s *assetSim) use(name string) {
	s.t.Helper()
	s.t.Setenv("GOPI_MEMORY_DB", s.nodes[name].dbPath)
	s.t.Setenv("GOPI_SYNC_NODE", name)
	config.ResetNodeIdentityForTest()
}

// store writes one brew row through the REAL MemoryStore path.
func (s *assetSim) store(name, topic, question, answer string) {
	s.t.Helper()
	s.use(name)
	res, err := MemoryStore(topic, question, answer, BrewSourceTag, nil)
	if err != nil {
		s.t.Fatalf("%s store %q: %v", name, question, err)
	}
	if strings.Contains(res, "deduped") {
		s.t.Fatalf("%s store %q: unexpected dedup on fresh store", name, question)
	}
}

// qCount returns how many distilled_qa rows carry the question.
func (s *assetSim) qCount(name, question string) int {
	s.t.Helper()
	s.use(name)
	db, err := openStoreDB(context.Background(), memoryDBPath())
	if err != nil {
		s.t.Fatal(err)
	}
	defer db.Close()
	var c int
	probe := truncateRunes(question, dedupQuestionRunes)
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM distilled_qa WHERE substr(question,1,?) = ?`,
		dedupQuestionRunes, probe).Scan(&c); err != nil {
		s.t.Fatal(err)
	}
	return c
}

// answerOf returns the stored answer for a question ("" if absent).
func (s *assetSim) answerOf(name, question string) string {
	s.t.Helper()
	s.use(name)
	db, err := openStoreDB(context.Background(), memoryDBPath())
	if err != nil {
		s.t.Fatal(err)
	}
	defer db.Close()
	var ans string
	err = db.QueryRowContext(context.Background(),
		`SELECT answer FROM distilled_qa WHERE question = ?`, question).Scan(&ans)
	if err != nil {
		return ""
	}
	return ans
}

// sourcesOf returns the sources value of the row holding question.
func (s *assetSim) sourcesOf(name, question string) string {
	s.t.Helper()
	s.use(name)
	db, err := openStoreDB(context.Background(), memoryDBPath())
	if err != nil {
		s.t.Fatal(err)
	}
	defer db.Close()
	var src string
	err = db.QueryRowContext(context.Background(),
		`SELECT COALESCE(sources,'') FROM distilled_qa WHERE question = ?`, question).Scan(&src)
	if err != nil {
		return ""
	}
	return src
}

// syncFrom runs ONE real MemoryAssetsPeerSession initiated by name toward
// peer (the ssh hook routes to the peer's DB).
func (s *assetSim) syncFrom(name, peer string) *MemoryAssetSyncStats {
	s.t.Helper()
	s.use(name)
	stats, err := MemoryAssetsPeerSession(peer, nil, "")
	if err != nil {
		s.t.Fatalf("%s sync → %s: %v", name, peer, err)
	}
	return stats
}

// TestMemoryAssetsUniqueOnlySuperset — acceptance ①: A{1,2} B{2,3} (with
// row 2 DIVERGENT — same question, different answer on each side) → after
// one session both hold {1,2,3}; row 2 untouched on BOTH sides (no
// newer-wins, no merge, no overwrite); the second session imports 0.
func TestMemoryAssetsUniqueOnlySuperset(t *testing.T) {
	s := newAssetSim(t)
	// A (zbook): rows 1,2. B (hp): rows 2(divergent),3.
	s.store("zbook", "t1", "q1 what is alpha?", "zbook answer alpha")
	s.store("zbook", "t2", "q2 how to beta?", "zbook answer beta — DIVERGENT")
	s.store("hp", "t2", "q2 how to beta?", "hp answer beta — DIVERGENT")
	s.store("hp", "t3", "q3 how to gamma?", "hp answer gamma")

	stats := s.syncFrom("zbook", "hp")

	// B must have imported row 1 only (row 2 existing-skipped).
	if stats.PullStats.Imported != 1 {
		t.Fatalf("hp imported=%d, want 1 (row 1; row 2 exists on hp): %+v", stats.PullStats.Imported, stats.PullStats)
	}
	if stats.PullStats.Existing != 1 {
		t.Fatalf("hp existing=%d, want 1 (row 2 skipped): %+v", stats.PullStats.Existing, stats.PullStats)
	}
	// Both stores now hold all three questions.
	for _, n := range []string{"zbook", "hp"} {
		for _, q := range []string{"q1 what is alpha?", "q2 how to beta?", "q3 how to gamma?"} {
			if s.qCount(n, q) != 1 {
				t.Fatalf("%s: question %q count=%d, want 1", n, q, s.qCount(n, q))
			}
		}
	}
	// Row 2 divergent copies UNTOUCHED: each side keeps its own answer.
	if got := s.answerOf("zbook", "q2 how to beta?"); got != "zbook answer beta — DIVERGENT" {
		t.Fatalf("zbook row 2 was modified: %q", got)
	}
	if got := s.answerOf("hp", "q2 how to beta?"); got != "hp answer beta — DIVERGENT" {
		t.Fatalf("hp row 2 was modified: %q", got)
	}

	// Second session: converged → 0 imported on both pull sides. (existing
	// counts only rows the peer re-sends that we already hold: hp re-exports
	// its OWN brew rows q2,q3 — q1 is node:zbook there, anti-reflow keeps it
	// home — so 2 re-sent rows find q2,q3 already present.)
	stats2 := s.syncFrom("hp", "zbook") // other direction also converges to 0
	if stats2.PullStats.Imported != 0 {
		t.Fatalf("2nd sync hp→zbook imported=%d, want 0 (converged): %+v", stats2.PullStats.Imported, stats2.PullStats)
	}
	if stats2.PullStats.Existing != 2 {
		t.Fatalf("2nd sync hp→zbook existing=%d, want 2 (q2,q3 re-sent and present)", stats2.PullStats.Existing)
	}
	stats3 := s.syncFrom("zbook", "hp")
	if stats3.PullStats.Imported != 0 {
		t.Fatalf("3rd sync zbook→hp imported=%d, want 0 (converged): %+v", stats3.PullStats.Imported, stats3.PullStats)
	}
}

// TestMemoryAssetsProvenanceAndAntiReflow — acceptance ②+③: rows imported
// from the peer carry node:<peer> in sources, and those imported rows are
// never re-exported toward any peer (SyncExport's no-node:* gate) — no
// ping-pong.
func TestMemoryAssetsProvenanceAndAntiReflow(t *testing.T) {
	s := newAssetSim(t)
	s.store("zbook", "t1", "q1 what is alpha?", "zbook alpha")
	s.store("hp", "t2", "q2 what is beta?", "hp beta")

	s.syncFrom("zbook", "hp")

	// hp's row 2 existed locally (brew) → stays brew. hp's row 1 was
	// IMPORTED from zbook → sources must carry node:zbook.
	if src := s.sourcesOf("hp", "q1 what is alpha?"); !strings.Contains(src, "node:zbook") {
		t.Fatalf("hp row 1 sources=%q, want node:zbook provenance", src)
	}
	if src := s.sourcesOf("hp", "q2 what is beta?"); !strings.Contains(src, BrewSourceTag) {
		t.Fatalf("hp row 2 sources=%q, want brew (untouched local row)", src)
	}
	// zbook's row 2 was imported from hp → carries node:hp.
	if src := s.sourcesOf("zbook", "q2 what is beta?"); !strings.Contains(src, "node:hp") {
		t.Fatalf("zbook row 2 sources=%q, want node:hp provenance", src)
	}

	// Anti-reflow: zbook now exports ONLY its own brew row 1 — row 2
	// (node:hp) must be excluded. Same for hp.
	z := s.exportRows("zbook")
	if len(z) != 1 || z[0].Question != "q1 what is alpha?" {
		t.Fatalf("zbook re-export carries %d rows %+v, want only q1 (node:hp row excluded)", len(z), qs(z))
	}
	h := s.exportRows("hp")
	if len(h) != 1 || h[0].Question != "q2 what is beta?" {
		t.Fatalf("hp re-export carries %d rows %+v, want only q2 (node:zbook row excluded)", len(h), qs(h))
	}
}

// exportRows returns what SyncExport would ship from node (the real
// anti-reflow gate).
func (s *assetSim) exportRows(name string) []SyncRow {
	s.t.Helper()
	s.use(name)
	patch, _, err := SyncExport(SyncExportOptions{})
	if err != nil {
		s.t.Fatalf("%s export: %v", name, err)
	}
	return patch.Rows
}

func qs(rows []SyncRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Question
	}
	return out
}

// TestMemoryAssetsIdenticalRowsExistingSkipped — the identical-content case
// of acceptance ①: the SAME row on both sides (byte-identical) is still
// counted existing and never rewritten.
func TestMemoryAssetsIdenticalRowsExistingSkipped(t *testing.T) {
	s := newAssetSim(t)
	// Both sides brewed the same lesson independently.
	s.store("zbook", "t", "q how to sync?", "shared answer")
	s.store("hp", "t", "q how to sync?", "shared answer")

	stats := s.syncFrom("zbook", "hp")
	if stats.PullStats.Imported != 0 || stats.PullStats.Existing != 1 {
		t.Fatalf("pull stats imported=%d existing=%d, want 0/1", stats.PullStats.Imported, stats.PullStats.Existing)
	}
	// Sources on hp's copy unchanged (still brew — import never touched it).
	if src := s.sourcesOf("hp", "q how to sync?"); src != BrewSourceTag {
		t.Fatalf("hp identical row sources=%q, want brew only", src)
	}
}

// TestMemoryAssetsImportRejectsGarbage — a garbage row (the MemoryStore
// garbage gate) is refused, not imported.
func TestMemoryAssetsImportRejectsGarbage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "garbage", "memory.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv("GOPI_SYNC_NODE", "zbook")
	patch := &SyncPatch{
		Version: syncPatchVersion,
		Node:    "hp",
		Rows: []SyncRow{
			{Topic: "t", Question: "ok question?", Answer: "ok answer"},
			{Topic: "t", Question: "[ASYNC DELEGATION result dump", Answer: "some text here"},
		},
	}
	stats, err := MemoryAssetsImportPatch(patch, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.Imported != 1 {
		t.Fatalf("imported=%d, want 1 (garbage row refused, good row stored)", stats.Imported)
	}
	if stats.Refused != 1 {
		t.Fatalf("refused=%d, want 1 (garbage)", stats.Refused)
	}
}

// TestMemoryAssetsServeRemoteRoundTrip — the wire shape: initiator export
// in → merged → reply export out, with nothing but the reply on stdout.
func TestMemoryAssetsServeRemoteRoundTrip(t *testing.T) {
	s := newAssetSim(t)
	s.store("hp", "t2", "q2 what is beta?", "hp beta")
	s.use("zbook")

	// zbook's export toward hp.
	patch, _, err := SyncExport(SyncExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	patch.Node = "zbook"
	body, _ := json.Marshal(patch) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)

	// The hp serve handler (in-process, hp's DB active).
	s.use("hp")
	reply, err := MemoryAssetsServeRemote(body, "")
	if err != nil {
		t.Fatal(err)
	}
	var out SyncPatch
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(reply))), &out); err != nil {
		t.Fatalf("reply parse: %v", err)
	}
	if out.Node != "hp" {
		t.Fatalf("reply node=%q, want hp", out.Node)
	}
	// hp imported zbook's q1; hp's reply carries its own brew row q2 only
	// (q1 now node:zbook → anti-reflow excluded from the reply).
	got := qs(out.Rows)
	if len(got) != 1 || got[0] != "q2 what is beta?" {
		t.Fatalf("reply rows=%v, want only q2 (imported q1 excluded)", got)
	}
}

// TestMemoryAssetsPeerWhitelistDecoupled — the 2026-09-02 bugfix acceptance:
// the asset channel validates against memory_peers, NEVER sync_peers.
// ① memory_peers=[] → `gopi memory sync --peer hp` refuses with the
//
//	whitelist message (even with sync_peers=hp declared — the keys are
//	separate, task peers do not license asset contact).
//
// ② sync_peers=[] + memory_peers=[hp] → the session RUNS: single-node task
//
//	mode must NOT disable the deliberately-kept memory-asset sync.
//
// ③ an undeclared asset peer is refused even with other memory_peers set.
func TestMemoryAssetsPeerWhitelistDecoupled(t *testing.T) {
	s := newAssetSim(t)
	s.store("zbook", "t1", "q1 what is alpha?", "zbook alpha")
	s.store("hp", "t2", "q2 what is beta?", "hp beta")

	// ① memory_peers cleared → whitelist refusal regardless of sync_peers.
	s.t.Setenv(config.MemoryPeersEnv, "")
	s.t.Setenv(config.SyncPeersEnv, "hp") // task peer alone must NOT license asset sync
	s.use("zbook")
	_, err := MemoryAssetsPeerSession("hp", nil, "")
	if err == nil {
		t.Fatal("memory sync with empty memory_peers must refuse (whitelist message)")
	}
	if !strings.Contains(err.Error(), "memory_peers") || !strings.Contains(err.Error(), "whitelist") {
		t.Fatalf("refusal must name the memory_peers whitelist: %v", err)
	}

	// ② sync_peers cleared (single-node task mode) + memory_peers=[hp] →
	//    asset sync RUNS (the bugfix scenario).
	s.t.Setenv(config.SyncPeersEnv, "")
	s.t.Setenv(config.MemoryPeersEnv, "hp")
	stats, err := MemoryAssetsPeerSession("hp", nil, "")
	if err != nil {
		t.Fatalf("single-node task mode (sync_peers=[]) must still allow asset sync: %v", err)
	}
	if stats.PullStats.Imported != 1 {
		t.Fatalf("hp imported=%d, want 1 (q1 copied over)", stats.PullStats.Imported)
	}
	if s.qCount("hp", "q1 what is alpha?") != 1 {
		t.Fatal("hp missing zbook's row after asset sync in single-node task mode")
	}

	// ③ an undeclared asset peer is refused even when another peer is declared.
	s.t.Setenv(config.MemoryPeersEnv, "hp")
	_, err = MemoryAssetsPeerSession("undeclared", nil, "")
	if err == nil || !strings.Contains(err.Error(), "memory_peers") {
		t.Fatalf("undeclared asset peer must be refused with the whitelist message: %v", err)
	}
}

// TestMemoryAssetsStalePeerHint — the 2026-09-02 live version-mismatch: an
// outdated peer gopi dies on --serve-assets with "unknown flag". The ssh
// leg must surface an ACTIONABLE upgrade hint (pull master + rebuild), not
// just the raw ssh error. Detector + hint are pure functions — unit-tested
// directly; the cmd/agent E2E exercises the full path with a stale shim.
func TestMemoryAssetsStalePeerHint(t *testing.T) {
	if !syncAssetsStalePeerHint("gopi memory sync: unknown flag --serve-assets") {
		t.Error("stale-peer stderr 'unknown flag --serve-assets' must be detected")
	}
	if !syncAssetsStalePeerHint("flag provided but not defined: -serve-assets") {
		t.Error("go flag 'provided but not defined' form must be detected")
	}
	if syncAssetsStalePeerHint("connection refused") {
		t.Error("unrelated ssh failure must not be misread as a stale peer")
	}
	hint := syncAssetsUpgradeHint("hp")
	for _, want := range []string{"hp", "outdated gopi", "--serve-assets", "pull master", "rebuild"} {
		if !strings.Contains(hint, want) {
			t.Errorf("upgrade hint missing %q: %s", want, hint)
		}
	}
}
