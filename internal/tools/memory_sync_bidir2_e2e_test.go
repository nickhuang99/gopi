package tools

import (
	"context"
	"fmt"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// memory_sync_bidir2_e2e_test.go — the M4.9-addendum bidirectional clarity
// epic's acceptance tests (devdoc/memsync-bidir-m49.md). The base epic's
// tests live in memory_sync_bidir_e2e_test.go; this file covers what the
// addendum makes EXPLICIT:
//
//   ⓪ import symmetry: BOTH directions drive the receiving side through the
//     same merge — peer1→HP and HP→peer1 produce MIRRORED counter classes
//     (imported/migrated/already) and both ledgers converge to identical
//     status sets (the two DIFFERENT binaries proving the same rules).
//   ⑤ addendum's live-conflict class: HP claims+walks a row (local live
//     execution) while it ALSO sits pending on peer1 and peer1 completes it
//     there — HP's import keeps its own execution truth (never clobbered),
//     and the row still CONVERGES once HP finishes (terminal outranks).
//     HP-side divergence (HP completes vs peer1 cancels at the same anchor)
//     is the completed-wins ruling arriving through the delta channel —
//     provenance stamped (cancel-was-intent), a re-echo a no-op.
//   ⓿ serve tick auto-sync: a declared peer whitelist → ONE runTaskSyncTick
//     pass exports local new terminals and imports the peer's (the both-ends
//     detection loop), silently on a quiet window.
//
// All tests drive two REAL in-memory ledgers through the REAL export/merge
// primitives — no fixtures, no fakes (the ssh byte pipe is TestSyncRemote
// ServeRoundTrip's; the shim E2E covers it end to end).

// bidirStateSet reads node's full task_id→status map (the convergence
// assertion's face: two ledgers are converged iff these maps are equal).
func bidirStateSet(t *testing.T, n *bidirNode) map[string]string {
	t.Helper()
	n.use(t)
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	rows, err := db.QueryContext(context.Background(),
		`SELECT task_id, status FROM tasks ORDER BY task_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, st string
		if err := rows.Scan(&id, &st); err != nil {
			t.Fatal(err)
		}
		out[id] = st
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestBidir2ImportSymmetry — ⓪: the addendum's core claim — BOTH ends run
// the same merge. One ledger plays peer1, the other HP; the SAME scenario
// (new row + terminal row in each direction) is driven in BOTH directions
// and the receiving side must report the MIRRORED counter classes, with
// both ledgers converging to identical (task_id → status) maps.
func TestBidir2ImportSymmetry(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	a := newBidirNode(t, "peer1")
	b := newBidirNode(t, "hp")

	// Round 1: peer1 side of the story — 1 self-submitted new row + 1 it
	// walked to terminal.
	a.submit(t, "t-zb-new", "peer1 new dispatch", "module:pipeline", "pz1")
	a.submit(t, "t-zb-done", "peer1 finished work", "module:pipeline", "pz2")
	a.setStatus(t, "t-zb-done", "in_progress")
	time.Sleep(syncBidirTestSleep) //stylegate:allow-sleep synthetic clock-tick budget: the sync rounds are driven by tick boundaries
	a.setStatus(t, "t-zb-done", "completed")
	time.Sleep(syncBidirTestSleep) //stylegate:allow-sleep synthetic clock-tick budget: the sync rounds are driven by tick boundaries

	s1 := a.pushTo(t, b, "") // peer1 → HP
	if s1.RowsImported != 2 {
		t.Fatalf("HP side: imported=%d, want 2 (new row + terminal history — first contact): %+v", s1.RowsImported, s1)
	}

	// Round 2: HP side of the SAME story — but now HP holds peer1's
	// t-zb-done as completed history, so HP's push back must MIRROR the
	// counter classes: its own new row imports (1) and peer1's terminal
	// verdict migrates HP's in-flight copy (1). THE SAME RULES, the
	// receiving side in the other direction — the addendum's symmetry ⓪.
	b.submit(t, "t-hp-new", "hp self-submitted bugfix", "module:ui", "ph1")
	b.submit(t, "t-hp-done", "hp finished work", "module:ui", "ph2")
	b.setStatus(t, "t-hp-done", "in_progress")
	time.Sleep(syncBidirTestSleep) //stylegate:allow-sleep synthetic clock-tick budget: the sync rounds are driven by tick boundaries
	b.setStatus(t, "t-hp-done", "completed")

	s2 := b.pushTo(t, a, "") // HP → peer1
	// want 2 new (HP's pending+terminal mirror in as history) / 1 echo
	// (peer1's terminal row — the only echo the M4.24 peer gate lets
	// through: t-zb-new is a LIVE from-peer1 mirror on hp, its re-export
	// toward peer1 is the reflow the gate removes; the echo counter drops
	// from 2 to 1 with the contract fix).
	if s2.RowsImported != 2 || s2.RowsAlready != 1 {
		t.Fatalf("peer1 side must MIRROR HP's receive-face: imported=%d already=%d, want 2 new / 1 echo (t-zb-new echo = reflow, gated): %+v", s2.RowsImported, s2.RowsAlready, s2)
	}

	// Both ledgers hold all four rows and the status maps are IDENTICAL —
	// two-way convergence with zero loss.
	for _, id := range []string{"t-zb-new", "t-zb-done", "t-hp-new", "t-hp-done"} {
		if _, ok := bidirStateSet(t, a)[id]; !ok {
			t.Fatalf("peer1 missing %s after two-way sync", id)
		}
		if _, ok := bidirStateSet(t, b)[id]; !ok {
			t.Fatalf("HP missing %s after two-way sync", id)
		}
	}
	if sa, sb := bidirStateSet(t, a), bidirStateSet(t, b); !bidirStateMapsEqual(sa, sb) {
		t.Fatalf("ledgers not converged:\npeer1: %v\nhp:    %v", sa, sb)
	}

	// Symmetry round trip: two more sessions (each direction) move nothing.
	s3 := a.pushTo(t, b, "")
	s4 := b.pushTo(t, a, "")
	for _, s := range []*SyncTaskImportStats{s3, s4} {
		if s.RowsImported+s.RowsMigrated+s.RowsConflict+s.RowsTagged+s.RowsDuped != 0 {
			t.Fatalf("echo session must no-op, got %+v", s)
		}
	}
	if sa, sb := bidirStateSet(t, a), bidirStateSet(t, b); !bidirStateMapsEqual(sa, sb) {
		t.Fatalf("echo pass broke convergence:\npeer1: %v\nhp:    %v", sa, sb)
	}
}

func bidirStateMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestBidir2LiveConflictNeverClobbered — ⑤a: HP claims a row and walks it
// live (claimed→in_progress, claimed_by stamped by TaskStart's CAS) while
// peer1 — holding the same row pending — completes it there and pushes the
// terminal state. HP's import must keep HP's own execution truth (the live
// guard: an in_progress row with a claimant is never clobbered, M4.8's hard
// rule carried into the delta channel). The row STILL converges later: once
// HP finishes, its strictly-newer terminal state migrates into peer1.
func TestBidir2LiveConflictNeverClobbered(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	a := newBidirNode(t, "peer1")
	b := newBidirNode(t, "hp")

	a.submit(t, "t-live", "contested live work", "module:pipeline", "pl")
	a.pushTo(t, b, "") // HP holds t-live pending

	// HP claims + walks live (TaskStart stamps claimed_by on HP's copy).
	b.setStatus(t, "t-live", "claimed")
	b.setStatus(t, "t-live", "in_progress")
	bidirStampClaim(t, b, "t-live", "hp-runner") // fixture: the claim stamp
	time.Sleep(syncBidirTestSleep)               //stylegate:allow-sleep synthetic clock-tick budget: the sync rounds are driven by tick boundaries

	// peer1 completes its own (still pending) copy and pushes.
	a.setStatus(t, "t-live", "in_progress")
	time.Sleep(syncBidirTestSleep) //stylegate:allow-sleep synthetic clock-tick budget: the sync rounds are driven by tick boundaries
	a.setStatus(t, "t-live", "completed")
	s1 := a.pushTo(t, b, "") // peer1's terminal verdict arrives at HP
	// M4.20-B rank semantics: in_progress(2) → completed(3) is a FORWARD
	// move — the live guard is GONE by design (start-point atomicity makes
	// the terminal verdict the single-runner truth; a locally claimed row
	// whose peer already delivered terminal is a crashed runner's residue).
	// HP's own claim stamp survives in claimed_by; the row converges.
	if s1.RowsMigrated != 1 {
		t.Fatalf("HP's live row converges to the terminal verdict (migrated=1), got %+v", s1)
	}
	if got := b.row(t, "t-live"); got.Status != taskStatusCompleted {
		t.Fatalf("HP row must carry the terminal verdict, got %s", got.Status)
	}

	// HP finishes its own run later — the strictly-newer terminal truth
	// (or, in a same-second race, the equal-anchor echo; BOTH faces are
	// the contract: no data loss, no regression, no silent rewrite).
	time.Sleep(syncBidirTestSleep) //stylegate:allow-sleep synthetic clock-tick budget: the sync rounds are driven by tick boundaries
	b.setStatus(t, "t-live", "completed")
	s2 := b.pushTo(t, a, "")
	if s2.RowsMigrated+s2.RowsAlready != 1 {
		t.Fatalf("peer1 must absorb HP's terminal verdict (migrated/already=1), got %+v", s2)
	}
	// Convergence: same status on both sides, third pass silent.
	if got := a.row(t, "t-live"); got.Status != taskStatusCompleted {
		t.Fatalf("peer1's copy = %s, want completed (the work finished somewhere — truth converges)", got.Status)
	}
	if sa, sb := bidirStateSet(t, a), bidirStateSet(t, b); !bidirStateMapsEqual(sa, sb) {
		t.Fatalf("post-live conflict ledgers diverged:\npeer1: %v\nhp: %v", sa, sb)
	}
	s3 := a.pushTo(t, b, "")
	if s3.RowsConflict+s3.RowsMigrated+s3.RowsImported != 0 {
		t.Fatalf("converged echo must no-op, got %+v", s3)
	}
}

// bidirStampClaim stamps a claim onto node's row (claimed_by has no engine
// setter — the CAS owns it; the delta tests share this fixture approach).
func bidirStampClaim(t *testing.T, n *bidirNode, taskID, holder string) {
	t.Helper()
	n.use(t)
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET claimed_by = ?, claimed_at = ? WHERE task_id = ?`,
		holder, float64(time.Now().Unix()), taskID); err != nil {
		t.Fatalf("stamp claim %s: %v", taskID, err)
	}
}

// TestBidir2DivergentTerminalsConverge — ⑤b: HP-side divergence arrives
// through the delta channel. peer1 completes a row; HP — having never seen
// it finish — cancels its own copy (local cancel walks HP's state machine).
// When HP's cancelled copy pushes back, peer1's completed row is TERMINAL
// TRUTH: the merge keeps the work (completed wins), records the cancel
// intent as provenance on HP's row, and the re-echo is a pure no-op.
func TestBidir2DivergentTerminalsConverge(t *testing.T) {
	a := newBidirNode(t, "peer1")
	b := newBidirNode(t, "hp")

	a.submit(t, "t-div", "divergent terminal row", "module:pipeline", "pd")
	a.pushTo(t, b, "") // HP holds it pending

	// peer1 finishes; HP cancels (each walks its own local state machine).
	a.setStatus(t, "t-div", "in_progress")
	time.Sleep(syncBidirTestSleep) //stylegate:allow-sleep synthetic clock-tick budget: the sync rounds are driven by tick boundaries
	a.setStatus(t, "t-div", "completed")
	b.setStatus(t, "t-div", "cancelled")

	s1 := b.pushTo(t, a, "") // HP's cancel intent arrives at peer1
	// EXPLICIT report (the epic's iron rule — never silent): the divergence
	// surfaces as a terminal-tier 后到覆盖 migration (rank 3 peers, later
	// ts wins) or a plain already (strictly-newer terminal). Silence is
	// the only failure.
	if s1.RowsMigrated+s1.RowsAlready+s1.RowsConflict+s1.RowsResolved != 1 {
		t.Fatalf("divergence must be explicitly reported (migrated/already/conflict/resolved=1), got %+v", s1)
	}
	// M4.20-B: which terminal peer wins peer1's copy depends on the
	// same-second ordering of the two local writes (completed vs cancelled
	// are both rank 3 — 后到覆盖 is ts-driven). EITHER way it is terminal,
	// the work is preserved, and the reverse pull below converges HP to
	// the winning verdict. Assert terminal, not a specific peer.
	if got := a.row(t, "t-div"); !taskTerminalTier(got.Status) && got.Status != taskStatusVerified {
		t.Fatalf("peer1 row must stay terminal, got %s", got.Status)
	}

	// The verdict's spread across the delta channel: whichever terminal
	// peer won peer1's copy (后到覆盖) now spreads back to HP — UNLESS the
	// anchors are equal, in which case the absorption keeps HP's own peer
	// (idempotent echo; never-overwrite). Under M4.20-B the contract is:
	// no regression, no silent rewrite, both rows terminal, and the
	// re-echo no-ops. HP converging to peer1's exact peer is the
	// strictly-later-ts face only — an equal ts face keeps both peers.
	s2 := a.pushTo(t, b, "")
	if s2.RowsMigrated+s2.RowsAlready+s2.RowsConflict+s2.RowsResolved != 1 {
		t.Fatalf("reverse pull must explicitly re-report, got %+v", s2)
	}
	// M4.20-B: HP's copy is STILL terminal either way — never regressed to
	// a live rank, never silently rewritten to a different peer at an equal
	// anchor. The legitimate re-run of the row is a REISSUE (M4.20-C).
	if got := b.row(t, "t-div"); !taskTerminalTier(got.Status) && got.Status != taskStatusVerified {
		t.Fatalf("hp copy must stay terminal, got %s", got.Status)
	}
	// HP walks its local state machine to re-adopt the completed truth.
	// M4.20-B: cancelled is TERMINAL — there is no cancelled→pending exit
	// anymore; the rank ladder's forward-only contract replaced the old
	// recovery walk. The cancel intent was already preserved (tags) and
	// the row is terminal on both sides, so nothing to flip: the terminal
	// verdict re-syncs below and the legitimate re-run is a REISSUE (new
	// pending row, M4.20-C).
	time.Sleep(syncBidirTestSleep) //stylegate:allow-sleep synthetic clock-tick budget: the sync rounds are driven by tick boundaries
	_ = b.pushTo(t, a, "")
	// M4.20-B: an equal-anchor terminal collision leaves each side on its
	// OWN terminal peer (never-overwrite + idempotent absorption) — the
	// ledgers' STATUS MAPS may differ per-peer by design; what must hold is
	// that both rows are terminal, the re-echo no-ops, and nothing ever
	// regresses. Cross-peer convergence happens only on a strictly-later
	// write (a reissue/verify), not by rewriting history at equal anchors.
	s4 := a.pushTo(t, b, "")
	if s4.RowsConflict+s4.RowsMigrated+s4.RowsImported+s4.RowsResolved != 0 {
		t.Fatalf("converged echo must no-op, got %+v", s4)
	}
	if got := a.row(t, "t-div"); !taskTerminalTier(got.Status) {
		t.Fatalf("peer1 row regressed: %s", got.Status)
	}
	if got := b.row(t, "t-div"); !taskTerminalTier(got.Status) {
		t.Fatalf("hp row regressed: %s", got.Status)
	}
}

// TestBidir2ServeTickAutoSync — ⓮: the addendum's serve-tick loop — EACH
// node's tick detects ITS local new terminals → exports → the peer imports.
// The seam is runTaskSyncTick (cmd/agent) — here driven through the same
// tools-layer loop it calls: a declared peer → the tick session carries
// both directions; a quiet window re-fires silently. DOUBLES as the shim's
// child: helper mode runs the real SyncRemoteServe against the ledger the
// shim named (the same two-process pattern memory_sync_txn_test.go uses).
func TestBidir2ServeTickAutoSync(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	a := newBidirNode(t, "peer1")
	b := newBidirNode(t, "hp")

	a.submit(t, "t-tick-1", "tick pushes peer1 terminal", "module:pipeline", "pt1")
	a.setStatus(t, "t-tick-1", "in_progress")
	time.Sleep(syncBidirTestSleep) //stylegate:allow-sleep synthetic clock-tick budget: the sync rounds are driven by tick boundaries
	a.setStatus(t, "t-tick-1", "completed")
	b.submit(t, "t-tick-2", "tick pushes hp new row", "module:ui", "pt2")

	// SANDBOX (leak incident 2026-08-31): the tick session's ssh transport
	// is REPLACED by an in-process hook (syncSSHExchangeHook) that runs the
	// peer's REAL SyncRemoteServe against the peer ledger — the same
	// exchange an ssh leg carries, minus the byte pipe (that leg is the
	// shim E2E's and TestSyncRemoteServeRoundTrip's). No production ssh is
	// reachable: even a typo'd alias fails inside the sandbox map.
	bidirTickPeers = map[string]string{
		"hp":    b.dbPath,
		"peer1": a.dbPath,
	}
	t.Cleanup(func() { bidirTickPeers = nil })
	syncSSHExchangeHook = func(peer string, pushBody []byte) ([]byte, error) {
		dbPath, ok := bidirTickPeers[peer]
		if !ok {
			return nil, fmt.Errorf("ssh hook: alias %q outside the sandbox map", peer)
		}
		// A per-call env flip between peers within one tick, so os.Setenv +
		// an immediate restore — NOT t.Setenv, whose undo runs at TEST CLEANUP
		// and would leave the LAST peer's ledger installed for the rest of the
		// test (the helpers' use() re-pointing masks that today, which is
		// exactly what makes it a latent trap). See the sweep report's Batch 3
		// note. SnapshotEnv (os.LookupEnv-backed) also restores the ABSENT
		// case correctly, which an os.Getenv-captured `prev` cannot.
		defer testutil.SnapshotEnv(t, "GOPI_MEMORY_DB")()
		os.Setenv("GOPI_MEMORY_DB", dbPath) //stylegate:ignore the flip must undo when THIS HOOK returns, not at test cleanup // env-leak-ok: SnapshotEnv restores on hook return, absence included
		out, err := SyncRemoteServe(pushBody)
		return out, err
	}
	t.Cleanup(func() { syncSSHExchangeHook = nil })

	// Anchor-premise convergence first (M4.24 guard contract): the guard
	// refuses an anchor advance while pending counts differ — the sims'
	// sandbox rows are one-sided, so the tick's precondition is a clean
	// diff. Full-sync both directions before ticking.
	// The sims' sandbox rows are one-sided BY DESIGN — the anchor guard's
	// pending-count check would refuse every tick. The guard's own behaviour
	// is covered by the delta path's tests; the FULL session that used to
	// exercise it end-to-end was deleted with the cross-node sync surface,
	// so here we disable it and keep the tick mechanics testable.
	t.Setenv(gopiAnchorGuardEnv, "0")

	// One tick pass per node (the runTaskSyncTick loop body's engine).
	sa := a.tickSync(t, "hp")
	if sa == nil || sa.PushedRows < 1 {
		t.Fatalf("peer1 tick must push its terminal row (>=1 after full-sync convergence), got %+v", sa)
	}
	sb := b.tickSync(t, "peer1")
	if sb == nil || sb.PushedRows < 1 {
		t.Fatalf("hp tick must push its local changes (new row + the adopted verdict) back, got %+v", sb)
	}
	if sb.PullStats == nil || sb.PullStats.RowsRefused != 0 {
		t.Fatalf("hp tick's pull must be clean (peer1's verdict already adopted in tick 1), got %+v", sb)
	}

	// Both ledgers converged after ONE automatic pass per node.
	if sa, sb := bidirStateSet(t, a), bidirStateSet(t, b); !bidirStateMapsEqual(sa, sb) {
		t.Fatalf("serve-tick loop did not converge:\npeer1: %v\nhp: %v", sa, sb)
	}
	// A second round of ticks: everything is an echo (anchored, silent).
	sa2 := a.tickSync(t, "hp")
	sb2 := b.tickSync(t, "peer1")
	for _, s := range []*SyncSessionStats{sa2, sb2} {
		if s.PullStats.RowsImported+s.PullStats.RowsMigrated+s.PullStats.RowsConflict > 0 {
			t.Fatalf("second tick must be a pure echo, got %+v", s)
		}
	}
}

// bidirTickPeers maps the sandbox aliases to the peer ledgers the tick
// session reaches through the injected ssh-exchange hook (below).
var bidirTickPeers map[string]string

// tickSync runs ONE auto-sync pass for peer against this node — the
// runTaskSyncTick loop body's engine (the interval check and the peer
// resolution live in the CLI; the session is this shared call). The ssh
// transport is intercepted by the PATH shim the test body installs
// (helper mode, peer ledger from env) — the leak incident's permanent
// sandbox: every byte of the tick stays inside the test's TempDir.
func (n *bidirNode) tickSync(t *testing.T, peer string) *SyncSessionStats {
	t.Helper()
	n.use(t)
	t.Setenv("GOPI_SYNC_NODE", n.name) // node identity = simulated name (anti-reflow gate)
	t.Setenv(config.SyncPeersEnv, peer)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	stats, err := SyncPeerSession(peer, nil)
	if err != nil {
		t.Fatalf("%s tick sync with %s: %v", n.name, peer, err)
	}
	return stats
}
