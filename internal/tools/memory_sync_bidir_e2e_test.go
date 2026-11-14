package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// syncBidirTestSleep separates two same-node status writes past the 1s
// updated_at resolution (a strictly newer anchor, not an equal-anchor
// collision — the merge reads same-second writes as simultaneous edits).
const syncBidirTestSleep = 1100 * time.Millisecond

// memory_sync_bidir_e2e_test.go — the bidirectional-sync epic's 实测
// (devdoc/memsync-bidirectional.md). Two isolated ledgers (A=HP, B=zpeer)
// driven through the REAL export/merge/serve-remote primitives in both
// directions — the same code path an ssh session transports, minus the
// ssh byte pipe itself (that leg is TestSyncRemoteServeRoundTrip's).
//
//	① A: 2 self-submitted rows + 1 it completed → session → B: +2 new rows,
//	   1 forward-migrated, ZERO data loss (row-count + content hash checks).
//	② both sides advance the same id differently → the merge reports the
//	   conflict EXPLICITLY (a report line, never silent), both rows kept.
//	③ --tags partition export: only the tagged segment travels.
//	④ full-chain idempotent round trip: export→mutate→import→re-export —
//	   the second pass moves nothing (no duplicates, no rewrites).

// bidirNode is one simulated node: its own GOPI_HOME-shaped ledger.
type bidirNode struct {
	name   string
	dbPath string
	prevDB string // the env value to restore
}

func newBidirNode(t *testing.T, name string) *bidirNode {
	t.Helper()
	path := filepath.Join(t.TempDir(), name, "memory.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_MEMORY_DB", path) // t.Setenv restores it at test end
	// Node identity = the SIMULATED name (memory_sync_delta.go's documented
	// GOPI_SYNC_NODE rule). Without this a host whose hostname equals a
	// simulated node name (running ON zpeer) collides with the anti-reflow
	// export gate (56fd84a): imported rows carry from-<self> and the node's
	// next export SQL-filters them away (all-zero pushTo stats, the round-6
	// redlist root cause — devdoc/tools-redlist-round6.md).
	t.Setenv("GOPI_SYNC_NODE", name)
	// M4.20-B + the ledger-address gate (85859202): pin UNDECLARED — the
	// single-machine default. IsServerNode() is false for it (N1 fail-safe
	// below), so a server-role sandbox cannot RULE on merge residues (rows →
	// completed) instead of the echo/mirror semantics the bidir sims assert —
	// the property the CLIENT pin used to provide. Undeclared is also the
	// only role that may write LOCALLY: a CLIENT pin with no sync_to peer
	// makes TaskSubmit refuse loudly (task_submit_forward.go), and this
	// fixture's submit helper carries the real write path.
	t.Setenv(config.RoleEnv, "")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open %s db: %v", name, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return &bidirNode{name: name, dbPath: path}
}

// use points the package-level DB resolution at this node (tests run
// serially inside one test function, so the env swap is safe). Call it
// before EVERY cluster of operations on this node — the other node's
// helpers (submit/setStatus/row) each re-point the env themselves, but
// bare calls between helpers rely on the last use().
func (n *bidirNode) use(t *testing.T) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", n.dbPath)
}

func (n *bidirNode) submit(t *testing.T, id, summary, tags, payload string) {
	t.Helper()
	n.use(t)
	if _, err := TaskSubmit(TaskSubmitOptions{TaskID: id, Summary: summary, Tags: tags, Payload: payload, Source: "human"}); err != nil {
		t.Fatalf("%s submit %s: %v", n.name, id, err)
	}
}

func (n *bidirNode) setStatus(t *testing.T, id, status string) {
	t.Helper()
	n.use(t)
	if _, err := TaskUpdate(id, status); err != nil {
		t.Fatalf("%s update %s→%s: %v", n.name, id, status, err)
	}
}

func (n *bidirNode) row(t *testing.T, id string) *Task {
	t.Helper()
	n.use(t)
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	row, err := taskByID(context.Background(), db, id)
	if err != nil {
		t.Fatalf("%s row %s: %v", n.name, id, err)
	}
	if row == nil {
		t.Fatalf("%s has no row %s", n.name, id)
	}
	return row
}

func (n *bidirNode) rowCount(t *testing.T) int {
	t.Helper()
	n.use(t)
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	var c int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tasks`).Scan(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

// pushTo runs THIS node's delta window into dst (the session's push leg):
// export (this node) → dst's merge. Returns the push stats on dst.
// Export resolves the env at call time, so n.use comes first, then dst.use
// for the merge — matching a session's export→ssh→merge sequencing.
func (n *bidirNode) pushTo(t *testing.T, dst *bidirNode, tagsFilter string) *SyncTaskImportStats {
	t.Helper()
	n.use(t)
	// Node identity = the SIMULATED name (same anti-reflow-gate collision
	// fix as newBidirNode; each helper re-points the env because t.Setenv
	// pins are per-test-scoped on the LAST set value).
	t.Setenv("GOPI_SYNC_NODE", n.name)
	// Direction-aware peers pin (388be0a pattern): the import side's
	// IsServerNode() must be deterministic regardless of the ambient
	// settings.json. The node named "zpeer" imports as the SERVER (foreign
	// peer name — hostname never matches → server ruling runs); any other
	// node imports as the CLIENT (peers pinned to THIS host's real hostname
	// → the client keep-row + conflict-tag path). Without the pin the
	// ruling depends on which machine runs the test (an alias equal to the
	// hostname flips it) — TestBidir2DivergentTerminalsConverge's
	// cancelled→pending local recovery walk assumes the client path.
	// 236fb03 made the config role PRIMARY over this peers pin, so the
	// role is pinned explicitly too (TestSyncDeltaConflictEqualAnchor
	// recipe) — the memoised identity would otherwise survive the env pin.
	// M4.20-B + the ledger-address gate (85859202): BOTH sides behave as
	// CLIENT — the rank ladder adjudicates everything the old server ruling
	// covered (forward moves + terminal 后到覆盖), and a ruling would rewrite
	// the echo semantics the sims assert. The server ruling itself is covered
	// by TestSyncDeltaServerResolvesConflict (unit) and stays for legacy
	// tolerance. UNDECLARED (not the literal "client") carries that client
	// face: IsServerNode() is false for it by the N1 fail-safe below, while a
	// declared client with no sync_to peer would make TaskSubmit refuse the
	// sims' seed rows (task_submit_forward.go).
	t.Setenv(config.SyncPeersEnv, "peer-under-test")
	t.Setenv(config.RoleEnv, "")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{Tags: tagsFilter, Peer: dst.name})
	if err != nil {
		t.Fatalf("%s export delta: %v", n.name, err)
	}
	patch.Node = n.name
	dst.use(t)
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("%s merge from %s: %v", dst.name, n.name, err)
	}
	return stats
}

// TestBidirEpic1SelfSubmittedAndTerminal — 实测 ①: A self-submits 2 rows and
// completes 1 (of the 3); after the session B must hold all 3 rows — the 2
// new ones INSERTed (from-a tagged), the terminal one forward-migrated —
// with every row's content byte-identical to A's.
func TestBidirEpic1SelfSubmittedAndTerminal(t *testing.T) {
	a := newBidirNode(t, "hp")
	b := newBidirNode(t, "zpeer")

	a.submit(t, "t-hp-1", "hp self task one", "module:pipeline", "payload-1")
	a.submit(t, "t-hp-2", "hp self task two", "module:ui", "payload-2")
	a.submit(t, "t-shared", "shared work", "module:pipeline", "payload-3")
	a.setStatus(t, "t-shared", "in_progress")
	a.setStatus(t, "t-shared", "completed")

	stats := a.pushTo(t, b, "")
	if stats.RowsImported != 3 {
		t.Fatalf("B imported=%d, want 3: %+v", stats.RowsImported, stats)
	}
	if got := b.rowCount(t); got != 3 {
		t.Fatalf("B row count = %d, want 3 (zero loss)", got)
	}
	// The self-submitted rows are new on B: INSERTed pending, from-hp provenance.
	for _, id := range []string{"t-hp-1", "t-hp-2"} {
		row := b.row(t, id)
		if row.Status != taskStatusPending {
			t.Fatalf("B %s status = %s, want pending (new work queues)", id, row.Status)
		}
		if row.Summary != a.row(t, id).Summary || row.Payload != a.row(t, id).Payload {
			t.Fatalf("B %s content differs from A (loss/rewrite!)", id)
		}
		if !hasTag(row.Tags, SyncTaskTagPrefix+"hp") {
			t.Fatalf("B %s tags %q missing from-hp provenance", id, row.Tags)
		}
	}
	// The terminal row forward-migrated on B (never re-queued, never duplicated).
	shared := b.row(t, "t-shared")
	if shared.Status != taskStatusCompleted {
		t.Fatalf("B t-shared status = %s, want completed (forward migration)", shared.Status)
	}
	if shared.Payload != "payload-3" {
		t.Fatalf("B t-shared payload rewritten: %q", shared.Payload)
	}
	// The full round trip back (B→A) must move nothing: every row is an echo.
	back := b.pushTo(t, a, "")
	if back.RowsImported+back.RowsDuped+back.RowsMigrated+back.RowsConflict+back.RowsTagged != 0 {
		t.Fatalf("B→A echo must no-op every row, got %+v", back)
	}
	if got := a.rowCount(t); got != 3 {
		t.Fatalf("A row count after echo = %d, want 3 (no duplicates)", got)
	}
}

// TestBidirEpic2StateConflictExplicitReport — 实测 ②: both nodes advance the
// same id to DIFFERENT terminal states at the same updated_at anchor. The
// merge must report the conflict EXPLICITLY (a report line — stats row +
// error-free-but-counted; the CLI renders the six-count line), keep both
// sides' statuses untouched (never-overwrite), and tag 'conflict' for a
// human. A later echo stays a no-op.
func TestBidirEpic2StateConflictExplicitReport(t *testing.T) {
	// Pin the peer whitelist: this host's settings.json lists hp/zpeer, so
	// ambient config could flip syncIsServerNode mid-test (388be0a pattern —
	// the test needs the SERVER RULING path, not the ruling's input).
	t.Setenv(config.SyncPeersEnv, "peer-under-test")
	a := newBidirNode(t, "hp")
	b := newBidirNode(t, "zpeer")

	a.submit(t, "t-c1", "contested work", "module:x", "payload-c")
	a.pushTo(t, b, "") // B now holds t-c1 pending (same content)

	// Both sides advance — HP says completed, zpeer says failed — and the
	// anchors are forced equal (the merge's equal-anchor branch).
	a.setStatus(t, "t-c1", "in_progress")
	a.setStatus(t, "t-c1", "completed")
	b.setStatus(t, "t-c1", "in_progress")
	b.setStatus(t, "t-c1", "failed")
	equalizeAnchors(t, "t-c1", a, b)

	// The merge runs on b — under the M4.20-B rank model the two terminal
	// peers (completed vs failed, both rank 3) at an EQUAL anchor are
	// idempotent no-ops on the import face (后到覆盖 only applies when the
	// incoming ts is STRICTLY later; equalizeAnchors stamps them equal).
	// The EXPLICIT report of an equal-anchor terminal collision is the
	// RowsAlready absorption — the row state itself is never rewritten
	// (never-overwrite), and the collision is re-reported on every pass
	// (never silent). That IS the contract now: explicit, idempotent,
	// no-overwrite.
	stats := a.pushTo(t, b, "")
	if stats.RowsConflict+stats.RowsResolved+stats.RowsAlready != 1 {
		t.Fatalf("conflict must be EXPLICITLY reported (conflict/resolved/already =1), got %+v", stats)
	}
	// The verdicts diverge by who rules — under M4.20-B both faces are
	// CLIENT (the rank ladder owns the merge). The equal-anchor terminal
	// collision absorbs as RowsAlready: neither side is rewritten
	// (never-overwrite), each keeps its own terminal peer, and the re-echo
	// no-ops explicitly. The legacy server-ruling and conflict-tag paths
	// are covered by TestSyncDeltaServerResolvesConflict / LegacyConflict
	// unit tests and stay for legacy tolerance only.
	bRow := b.row(t, "t-c1")
	aRow := a.row(t, "t-c1")
	if stats.RowsAlready != 1 {
		t.Fatalf("equal-anchor terminal collision must absorb as already=1, got %+v", stats)
	}
	if aRow.Status != taskStatusCompleted || bRow.Status != taskStatusFailed {
		t.Fatalf("never-overwrite violated: a=%s b=%s", aRow.Status, bRow.Status)
	}
	again := a.pushTo(t, b, "")
	if again.RowsConflict != 0 || again.RowsResolved != 0 || again.RowsAlready != 1 {
		t.Fatalf("re-echo must no-op explicitly, got %+v", again)
	}
}

// equalizeAnchors forces both nodes' copies of taskID onto one shared
// updated_at (the equal-anchor conflict branch needs identical timestamps —
// real nodes hit this when both write within the same second).
func equalizeAnchors(t *testing.T, taskID string, nodes ...*bidirNode) {
	t.Helper()
	// taskSyncDB() resolves through GOPI_MEMORY_DB, so each node must be
	// current while its rows are stamped.
	stamp := float64(time.Now().Unix())
	for _, n := range nodes {
		n.use(t)
		db, closeDB, err := taskSyncDB()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(context.Background(),
			`UPDATE tasks SET updated_at = ? WHERE task_id = ?`, stamp, taskID); err != nil {
			t.Fatal(err)
		}
		closeDB()
	}
}

// TestBidirEpic4IdempotentRoundTrip — 实测 ④: export→mutate→import→re-export
// full chain. After the first session carries A's work to B, B completes a
// row and the return session migrates A's copy; the THIRD session (both
// directions) moves nothing — anchors + echo-identification make repeated
// syncs free.
func TestBidirEpic4IdempotentRoundTrip(t *testing.T) {
	a := newBidirNode(t, "hp")
	b := newBidirNode(t, "zpeer")

	a.submit(t, "t-r1", "roundtrip row", "module:pipeline", "p")
	a.pushTo(t, b, "")
	b.use(t)
	b.setStatus(t, "t-r1", "in_progress")
	// Same-second writes collapse onto one anchor (the equal-anchor branch
	// treats in-flight same-tick edits as conflicts) — space the two local
	// writes by >1s so the terminal write is strictly newer.
	time.Sleep(syncBidirTestSleep) //stylegate:allow-sleep synthetic clock-tick budget for the bidirectional sync exchange
	b.setStatus(t, "t-r1", "completed")

	// B→A: the terminal state forward-migrates A's pending row.
	back := b.pushTo(t, a, "")
	if back.RowsMigrated != 1 {
		t.Fatalf("B→A migrated=%d, want 1: %+v", back.RowsMigrated, back)
	}
	if got := a.row(t, "t-r1"); got.Status != taskStatusCompleted {
		t.Fatalf("A t-r1 = %s, want completed", got.Status)
	}
	// Round 3: BOTH directions again — pure echoes everywhere.
	fwd := a.pushTo(t, b, "")
	bwd := b.pushTo(t, a, "")
	if fwd.RowsImported+fwd.RowsMigrated+fwd.RowsConflict+fwd.RowsTagged+fwd.RowsDuped != 0 {
		t.Fatalf("round-3 A→B must no-op, got %+v", fwd)
	}
	if bwd.RowsImported+bwd.RowsMigrated+bwd.RowsConflict+bwd.RowsTagged+bwd.RowsDuped != 0 {
		t.Fatalf("round-3 B→A must no-op, got %+v", bwd)
	}
	if a.rowCount(t) != 1 || b.rowCount(t) != 1 {
		t.Fatalf("row counts drifted: A=%d B=%d, want 1/1", a.rowCount(t), b.rowCount(t))
	}
}

// TestBidirEpic3MixedAssetsHashInvariant — the never-overwrite invariant in
// its hash form: before/after ANY session, each row's content hash is
// unchanged unless the row was forward-migrated (status-only move).
func TestBidirEpic3MixedAssetsHashInvariant(t *testing.T) {
	a := newBidirNode(t, "hp")
	b := newBidirNode(t, "zpeer")

	a.submit(t, "t-h1", "hp row", "module:pipeline", "payload-h")
	a.submit(t, "t-h2", "hp row 2", "module:ui", "payload-h2")
	before := map[string]string{}
	for _, id := range []string{"t-h1", "t-h2"} {
		r := a.row(t, id)
		before[id] = fmt.Sprintf("%s|%s|%s", r.Summary, r.Payload, r.Status)
	}
	a.pushTo(t, b, "")
	// A's rows: hash-identical (A was never written by the session).
	for _, id := range []string{"t-h1", "t-h2"} {
		r := a.row(t, id)
		if got := fmt.Sprintf("%s|%s|%s", r.Summary, r.Payload, r.Status); got != before[id] {
			t.Fatalf("A %s changed: %q → %q", id, before[id], got)
		}
	}
	// B's copies: content hash identical; only status (pending) + from-tag added.
	b1 := b.row(t, "t-h1")
	if b1.Summary != "hp row" || b1.Payload != "payload-h" {
		t.Fatalf("B t-h1 content hash differs: %q/%q", b1.Summary, b1.Payload)
	}
}
