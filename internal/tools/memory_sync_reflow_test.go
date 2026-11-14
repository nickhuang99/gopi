package tools

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// Anti-reflow export gate (brew double-gate semantics, epic ⑦ hardening):
// rows tagged from-<THIS node> arrived through the task-sync channel — the
// exporter never re-exports them. Without the gate a relay/loop topology
// (HP holding rows it received FROM zpeer) ships them back, and any local
// touch on hp (tag edit bumps updated_at) makes the echo look like NEWER
// work to zpeer's merge — the peer's own state gets overwritten by its own
// stale mirror round-tripping through hp.

// TestReflowExportExcludesFromPeerRows — the delta exporter (v2 session
// push/pull path) drops every row carrying from-<PEER> (M4.24: relaying a
// peer's row back is the reflow); local-born rows and rows from a THIRD
// node still travel. Zero-arg semantics (the declared legacy fallback):
// without a Peer the gate excludes from-<self> — the v1-era contract kept
// for peer-unknown callers (the v1 CLI file channel).
func TestReflowExportExcludesFromPeerRows(t *testing.T) {
	n := newBidirNode(t, "hp")
	n.use(t)
	t.Setenv("GOPI_SYNC_NODE", "hp")

	// hp's own row (never synced) — must export.
	n.submit(t, "t-hp-own", "hp original work", "module:p", "p1")
	// A row hp received from zpeer (from-zpeer tag = the PEER's provenance)
	// — must NOT export toward zpeer. It stays PENDING on hp (untouched
	// mirror) — the echo-loop face the gate exists for.
	deltaInsertRaw(t, "t-echo-peer", "echoed row", taskStatusPending, 1, 1, SyncTaskTagPrefix+"zpeer")
	// A row from a third node — must export (relay legitimacy: forwarding
	// it to zpeer is the M4.24 repair path for third-node rows). Terminal,
	// but from-hp2 ≠ from-zpeer, so the gate tag doesn't bite anyway.
	deltaInsertRaw(t, "t-third", "third-node row", taskStatusCompleted, 2, 2, SyncTaskTagPrefix+"hp2")

	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{Peer: "zpeer"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range patch.Rows {
		got[r.TaskID] = true
	}
	if got["t-echo-peer"] {
		t.Fatalf("from-<peer> row t-echo-peer re-exported — anti-reflow gate missing")
	}
	if !got["t-hp-own"] {
		t.Fatalf("own row t-hp-own must export (rows: %v)", patch.Rows)
	}
	if !got["t-third"] {
		t.Fatalf("third-node from-hp2 row must export (relay legitimacy)")
	}
}

// TestReflowExportLegacySelfFallback — the zero-arg contract (Peer=""
// falls back to the self-gate): a peer-unknown export excludes from-<self>
// rows and ships everything else. The v1 CLI file channel and the
// zero-arg unit tests depend on this declared semantics.
func TestReflowExportLegacySelfFallback(t *testing.T) {
	claimTestDB(t)
	t.Setenv("GOPI_SYNC_NODE", "hp")
	// The identity cache may hold the AMBIENT node (GOPI_SYNC_NODE is in
	// /home/user/.profile on this host) — reset so the exporter re-reads the env.
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	base := float64(time.Now().Unix())

	deltaInsertRaw(t, "t-echo-self", "echoed row", taskStatusPending, base, base, SyncTaskTagPrefix+"hp")
	deltaInsertRaw(t, "t-foreign", "foreign row", taskStatusCompleted, base+1, base+1, SyncTaskTagPrefix+"zpeer")

	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range patch.Rows {
		got[r.TaskID] = true
	}
	if got["t-echo-self"] {
		t.Fatalf("zero-arg export re-exported from-<self> row — legacy fallback broken")
	}
	if !got["t-foreign"] {
		t.Fatalf("zero-arg export must ship foreign rows (rows: %v)", patch.Rows)
	}
}

// TestReflowExportGateV1Export — the M4.8 one-shot exporter carries the same
// gate with the declared zero-arg semantics (the CLI file channel has no
// session peer): a from-<self> row never travels the file channel.
func TestReflowExportGateV1Export(t *testing.T) {
	dbPath := taskSyncTestDB(t)
	t.Setenv("GOPI_SYNC_NODE", "hp")
	// The exporter resolves node identity via GOPI_SYNC_NODE only — but the
	// ambient settings.json (sync_peers) must not leak into the sandbox:
	// pin an empty peer list so the legacy fallback is the self-gate.
	t.Setenv(config.SyncPeersEnv, "")
	t.Setenv("GOPI_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))
	taskSyncSeed(t, 1, "work")
	db, err := openTasksDB(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// The seed row went in seconds ago; the channel row rides the same
	// window (the v1 exporter has no anchor, only SinceUnix/Limit).
	deltaInsertRaw(t, "t-v1-self", "channel row", taskStatusPending,
		float64(time.Now().Unix()), float64(time.Now().Unix()), SyncTaskTagPrefix+"hp")
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	patch, _, err := SyncTaskExport(SyncTaskExportOptions{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range patch.Rows {
		if r.TaskID == "t-v1-self" {
			t.Fatalf("v1 export re-exported a from-<self> row")
		}
	}
	if len(patch.Rows) != 1 {
		t.Fatalf("v1 export rows=%d, want 1 (the untagged local row)", len(patch.Rows))
	}
}

// TestReflowEchoStaleMirrorNoReturn — the epic's死循环 end-to-end: zpeer
// self-submits with a briefing → hp imports it (from-zpeer) → hp locally
// tags the row (its anchor advances; WITHOUT the gate hp re-exports it, and
// zpeer's merge would see its own row as "remote newer" and let hp's stale
// mirror tags overwrite zpeer's newer local tags). With the peer gate
// (pushTo exports with Peer=dst): hp's re-export toward zpeer excludes the
// from-zpeer row — zpeer's next session is a pure echo.
func TestReflowEchoStaleMirrorNoReturn(t *testing.T) {
	hp := newBidirNode(t, "hp")
	zb := newBidirNode(t, "zpeer")

	zb.submit(t, "t-z1", "zpeer briefing row", "module:x", "briefing-v1")
	s1 := zb.pushTo(t, hp, "")
	if s1.RowsImported != 1 {
		t.Fatalf("hp imported=%d want 1: %+v", s1.RowsImported, s1)
	}

	// hp locally touches the channel-borne row via taskAddTag (the one
	// production tag-edit path — bumps updated_at, opening hp's re-export
	// window).
	time.Sleep(syncBidirTestSleep) //stylegate:allow-sleep synthetic clock-tick budget for the reflow pass
	hp.use(t)
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := taskAddTag(context.Background(), db, "t-z1", "hp-touched"); err != nil {
		t.Fatal(err)
	}
	closeDB()

	// hp → zpeer re-delivery of the SAME id: with the export gate the row
	// never leaves hp → nothing to merge.
	s2 := hp.pushTo(t, zb, "")
	t.Logf("echo push stats: %+v", s2)
	if s2.RowsImported+s2.RowsConflict+s2.RowsTagged+s2.RowsMigrated+s2.RowsDuped != 0 {
		t.Fatalf("echo re-delivery must no-op, got: %+v", s2)
	}
	if zb.rowCount(t) != 1 {
		t.Fatalf("zpeer row count drifted: %d", zb.rowCount(t))
	}
	// zpeer's row content is untouched by the round trip.
	zr := zb.row(t, "t-z1")
	if zr.Payload != "briefing-v1" || !hasTag(zr.Tags, "module:x") {
		t.Fatalf("zpeer row mutated by reflow: %+v", zr)
	}
}
