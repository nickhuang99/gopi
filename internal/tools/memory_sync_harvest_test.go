package tools

// Tests for the HP harvest PULL channel (M4.12): anchor-based incremental
// fetch, single-tx apply with the six counts, and the two hard rules —
// newer-wins and 终态不回退 (a local terminal row is never demoted).
// The ssh transport is injected via HarvestOptions.Fetch; the ledger and the
// sync_state anchor are exercised for real.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// harvestFixturePeer is the alias every harvest transport test's injector
// stands for. It is DECLARED (harvestTestDB below) because TaskHarvest gates
// on the declared topology before the transport: an undeclared alias is
// refused by the contract, so a transport test that never declares its peer
// would pass for the wrong reason (it would be testing the gate, not the
// pull). Rule 1b: the alias lives here, not as a bare literal per call.
const harvestFixturePeer = "hp"

// harvestDeclarePeer declares a topology for the calling test — the harness
// half of the declared-topology gate's precondition.
func harvestDeclarePeer(t *testing.T, peers ...string) {
	t.Helper()
	t.Setenv(config.SyncPeersEnv, strings.Join(peers, ","))
}

// harvestTestDB prepares an isolated ledger (the taskSyncTestDB pattern) and
// returns its path. It also DECLARES harvestFixturePeer so the fixtures below
// exercise the pull path rather than the gate (harvestDeclarePeer).
func harvestTestDB(t *testing.T) string {
	t.Helper()
	harvestDeclarePeer(t, harvestFixturePeer)
	return taskSyncTestDB(t)
}

// harvestSeedRow inserts a local row with full control over status and the
// timestamps (TaskSubmit would stamp now — the tests need exact stamps).
func harvestSeedRow(t *testing.T, taskID, summary, status string, updated float64, tags string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, taskSyncTestDBPath(t))
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer db.Close()
	ended := 0.0
	if terminalTaskStatus(status) {
		ended = updated
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, session_id, model,
		                    parent_task_id, tags, payload, source, deliverable,
		                    created_at, updated_at, ended_at)
		 VALUES (?, ?, ?, '', '', '', ?, ?, 'auto', '', ?, ?, ?)`,
		taskID, summary, status, tags, "seed payload "+summary,
		updated-10, updated, ended); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
}

// taskSyncTestDBPath re-derives the GOPI_MEMORY_DB the helper set.
func taskSyncTestDBPath(t *testing.T) string {
	t.Helper()
	return os.Getenv("GOPI_MEMORY_DB")
}

// harvestAnchor reads the stored anchor for a peer directly (the assertion
// side of step ④).
func harvestAnchor(t *testing.T, peer string) float64 {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, taskSyncTestDBPath(t))
	if err != nil {
		t.Fatalf("open anchor db: %v", err)
	}
	defer db.Close()
	var anchor float64
	if err := db.QueryRowContext(ctx,
		`SELECT anchor FROM sync_state WHERE peer = ?`, peer).Scan(&anchor); err != nil {
		t.Fatalf("anchor for %s: %v", peer, err)
	}
	return anchor
}

// harvestRows builds an injector returning the given rows on every call.
func harvestRows(rows []harvestRemoteRow, seenAnchor *float64) func(string, float64) ([]harvestRemoteRow, error) {
	return func(peer string, anchor float64) ([]harvestRemoteRow, error) {
		if seenAnchor != nil {
			*seenAnchor = anchor
		}
		return rows, nil
	}
}

// harvestRemote builds one remote row (zero values = live pending row).
func harvestRemote(id, summary, status string, updated float64) harvestRemoteRow {
	return harvestRemoteRow{
		TaskID: id, Summary: summary, Payload: "payload " + summary,
		Status: status, CreatedAt: updated - 10, UpdatedAt: updated,
		EndedAt: map[bool]float64{true: updated}[terminalTaskStatus(status)],
	}
}

// harvestLocal checks one local row's (status, tags) — the assertion side.
func harvestLocal(t *testing.T, taskID string) (string, string, float64) {
	t.Helper()
	row, err := syncTaskByID(taskID)
	if err != nil || row == nil {
		t.Fatalf("local %s: %v", taskID, err)
	}
	return row.Status, row.Tags, row.UpdatedAt
}

// TestHarvestInsertNewRows: a blank ledger harvests everything; terminal
// rows keep status/ended_at, live rows land pending, tags carry from-hp.
func TestHarvestInsertNewRows(t *testing.T) {
	harvestTestDB(t)
	rows := []harvestRemoteRow{
		harvestRemote("t-hp-1", "done work", "completed", 1788000100),
		harvestRemote("t-hp-2", "live work", "in_progress", 1788000200),
	}
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Fetch: harvestRows(rows, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if counts.Harvested != 2 || counts.Migrated+counts.Duped+counts.Conflict+counts.Skipped+counts.Failed != 0 {
		t.Fatalf("counts = %+v, want harvested=2 only", counts)
	}
	if st, tags, _ := harvestLocal(t, "t-hp-1"); st != "completed" || !hasTag(tags, "from-hp") {
		t.Fatalf("t-hp-1 = (%q,%q), want completed with from-hp", st, tags)
	}
	// Run provenance rides the harvest wire too (devdoc/run-method-
	// provenance.md): a peer's stamped row lands method + cmdline so a
	// harvested list/show answers "how was this run" like the origin.
	// NOTE: this must be a FRESH row — a local terminal row is never
	// rewritten on the harvest face (终态不回退), so a provenance
	// backfill to an already-harvested terminal row is correctly skipped.
	withProv := harvestRemote("t-hp-3", "provenance work", "completed", 1788000400)
	withProv.RunMethod = "manual"
	withProv.RunCmd = "gopi task run t-hp-3 --report-to hp"
	counts2, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Fetch: harvestRows([]harvestRemoteRow{withProv}, nil)})
	if err != nil {
		t.Fatalf("harvest prov: %v", err)
	}
	if counts2.Harvested != 1 {
		t.Fatalf("prov row not harvested: %+v", counts2)
	}
	row, err := syncTaskByID("t-hp-3")
	if err != nil || row == nil {
		t.Fatalf("re-read t-hp-3: %v", err)
	}
	if row.RunMethod != "manual" || row.RunCmd != "gopi task run t-hp-3 --report-to hp" {
		t.Fatalf("harvested provenance = %q / %q, want manual stamp", row.RunMethod, row.RunCmd)
	}
	if st, tags, _ := harvestLocal(t, "t-hp-2"); st != "pending" || !hasTag(tags, "from-hp") {
		t.Fatalf("t-hp-2 = (%q,%q), want pending (execution state never travels) with from-hp", st, tags)
	}
	// Anchor = max updated_at − the skew rewind (1788000400 − 5, the
	// provenance row's stamp): the never-park-at-the-edge contract —
	// same-second commits must re-fetch.
	if anchor := harvestAnchor(t, "hp"); anchor != 1788000395 {
		t.Fatalf("anchor = %v, want 1788000395 (max updated_at rewound)", anchor)
	}
}

// TestHarvestAnchorRewindSameSecondRecovers: a row whose stamp lands
// BETWEEN the rewind line and the newest stamp (the sub-second / same-
// second commit the %f SELECT format truncates — bug038's E2E signature)
// is re-fetched and applied on the NEXT harvest instead of being skipped
// as "remote older" forever.
func TestHarvestAnchorRewindSameSecondRecovers(t *testing.T) {
	harvestTestDB(t)
	// Run 1: the peer's terminal row at T (its REAL stamp is T−1e-6 in
	// truth, but the peer SELECT truncates it to T; the anchor logic must
	// not park at T regardless).
	rows := []harvestRemoteRow{harvestRemote("t-hp-1", "done work", "completed", 1788000100)}
	if _, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Fetch: harvestRows(rows, nil)}); err != nil {
		t.Fatalf("first harvest: %v", err)
	}
	if anchor := harvestAnchor(t, "hp"); anchor != 1788000095 {
		t.Fatalf("anchor = %v, want 1788000095 (rewound 5s)", anchor)
	}
	// Run 2: the same row re-arrives (anchor rewind made the peer re-send
	// it) and is absorbed idempotently — then a LATER row at the exact
	// truncation boundary (the stamp the first run's SELECT truncated)
	// still travels because the fetch window extends past the anchor.
	later := harvestRemoteRow{
		TaskID: "t-hp-1", Summary: "done work", Payload: "payload done work",
		Status: "completed", CreatedAt: 1788000090, UpdatedAt: 1788000099.999999,
		EndedAt: 1788000099.999999,
	}
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer,
		Fetch: func(peer string, anchor float64) ([]harvestRemoteRow, error) {
			// The peer SELECT: anchor 1788000095 < updated_at 1788000099.999999.
			if 1788000099.999999 > anchor {
				return []harvestRemoteRow{later}, nil
			}
			return []harvestRemoteRow{}, nil
		}})
	if err != nil {
		t.Fatalf("second harvest: %v", err)
	}
	if st, _, _ := harvestLocal(t, "t-hp-1"); st != "completed" {
		t.Fatalf("t-hp-1 = %q, want the truncated-stamp terminal row adopted", st)
	}
	if counts.Migrated+counts.Duped != 1 {
		t.Fatalf("counts = %+v, want the truncated row applied exactly once", counts)
	}
}

// TestHarvestRerunQuietPeerIsZero: anchor advanced, the peer returns nothing
// — the idempotency contract (epic 实测 ②).
func TestHarvestRerunQuietPeerIsZero(t *testing.T) {
	harvestTestDB(t)
	rows := []harvestRemoteRow{harvestRemote("t-hp-1", "done work", "completed", 1788000100)}
	if _, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Fetch: harvestRows(rows, nil)}); err != nil {
		t.Fatalf("first harvest: %v", err)
	}
	// Second run: the injector now returns nothing (the anchor moved past).
	var fetchedAnchor float64
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer,
		Fetch: func(peer string, anchor float64) ([]harvestRemoteRow, error) {
			fetchedAnchor = anchor
			return []harvestRemoteRow{}, nil
		}})
	if err != nil {
		t.Fatalf("second harvest: %v", err)
	}
	if fetchedAnchor != 1788000095 {
		t.Fatalf("fetch anchor = %v, want the stored 1788000095 (rewound)", fetchedAnchor)
	}
	if counts.Harvested+counts.Migrated+counts.Duped+counts.Conflict+counts.Skipped+counts.Failed != 0 {
		t.Fatalf("quiet rerun changed counts: %+v", counts)
	}
}

// TestHarvestRerunSameRowsDuped: a full re-delivery (anchor override 0) hits
// the same-stamp same-content branch — duped, no data rewritten.
func TestHarvestRerunSameRowsDuped(t *testing.T) {
	harvestTestDB(t)
	rows := []harvestRemoteRow{
		harvestRemote("t-hp-1", "done work", "completed", 1788000100),
		harvestRemote("t-hp-2", "live work", "", 1788000200),
	}
	for run := 1; run <= 2; run++ {
		counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows(rows, nil)})
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if run == 1 && counts.Harvested != 2 {
			t.Fatalf("run 1 counts = %+v, want harvested=2", counts)
		}
		if run == 2 && (counts.Duped != 2 || counts.Harvested != 0) {
			t.Fatalf("run 2 counts = %+v, want duped=2 harvested=0", counts)
		}
	}
}

// TestHarvestTerminalNoDemotion: a local completed row keeps its verdict no
// matter what the peer says — older stamp, newer stamp, live status (the
// epic 实测 ③, the 终态不回退 rule).
func TestHarvestTerminalNoDemotion(t *testing.T) {
	harvestTestDB(t)
	harvestSeedRow(t, "t-local-1", "local finished", "completed", 1788000500, "")
	cases := []struct {
		name string
		row  harvestRemoteRow
	}{
		{"older live report", harvestRemote("t-local-1", "local finished", "in_progress", 1788000100)},
		{"older terminal report", harvestRemote("t-local-1", "local finished", "failed", 1788000200)},
		{"newer live report", harvestRemote("t-local-1", "local finished", "in_progress", 1788000900)},
		{"newer terminal report", harvestRemote("t-local-1", "local finished", "failed", 1788000900)},
	}
	for _, tc := range cases {
		counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows([]harvestRemoteRow{tc.row}, nil)})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if counts.Skipped != 1 {
			t.Fatalf("%s: counts = %+v, want skipped=1", tc.name, counts)
		}
		if st, _, _ := harvestLocal(t, "t-local-1"); st != "completed" {
			t.Fatalf("%s: local status = %q, want completed (终态不回退)", tc.name, st)
		}
	}
}

// TestHarvestNewerWinsMigration: local pending, remote strictly newer →
// the row adopts the remote truth (status, ended_at, provenance tags).
func TestHarvestNewerWinsMigration(t *testing.T) {
	harvestTestDB(t)
	harvestSeedRow(t, "t-both-1", "shared work", "pending", 1788000100, "")
	remote := harvestRemote("t-both-1", "shared work", "completed", 1788000800)
	remote.Tags = "hp-side"
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows([]harvestRemoteRow{remote}, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if counts.Migrated != 1 {
		t.Fatalf("counts = %+v, want migrated=1", counts)
	}
	st, tags, updated := harvestLocal(t, "t-both-1")
	if st != "completed" || !hasTag(tags, "from-hp") || !hasTag(tags, "hp-side") || updated != 1788000800 {
		t.Fatalf("row = (%q,%q,%v), want completed + from-hp + hp-side + stamp 1788000800", st, tags, updated)
	}
}

// TestHarvestConflictSameStamp: equal stamps + equal rank + diverging work
// under one id — post-collision-fix (devdoc/task-id-collision.md) this is
// the two-nodes-same-second collision: the incoming row RELABELS to
// t-both-2-nhp and queues (HP's work is never stranded as a conflict
// line). The conflict counter is now reserved for FINISHED divergent
// truths (TestHarvestConflictSameStampBothTerminal).
func TestHarvestConflictSameStamp(t *testing.T) {
	harvestTestDB(t)
	harvestSeedRow(t, "t-both-2", "zpeer version", "pending", 1788000300, "")
	remote := harvestRemote("t-both-2", "hp version", "pending", 1788000300)
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows([]harvestRemoteRow{remote}, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if counts.Relabeled != 1 || counts.Conflict != 0 {
		t.Fatalf("counts = %+v, want relabel=1 conflict=0", counts)
	}
	row, err := syncTaskByID("t-both-2")
	if err != nil || row == nil {
		t.Fatalf("local t-both-2: %v", err)
	}
	if row.Status != "pending" || row.Summary != "zpeer version" {
		t.Fatalf("relabel rewrote the local row: (%q,%q)", row.Status, row.Summary)
	}
	incoming, err := syncTaskByID("t-both-2-nhp")
	if err != nil || incoming == nil {
		t.Fatalf("relabeled row t-both-2-nhp missing: %v", err)
	}
	if incoming.Summary != "hp version" || incoming.Status != "pending" {
		t.Fatalf("relabeled row wrong: (%q,%q)", incoming.Status, incoming.Summary)
	}
}

// TestHarvestLadderSameStampTerminalWins: the same work planted on both
// nodes (the shared-session memsync reality) — zpeer still pending, HP
// already completed, same updated_at: the terminal result adopts the row
// (status outranks content at equal stamps; the conflict counter is for two
// FINISHED truths, not for "one side hasn't finished yet").
func TestHarvestLadderSameStampTerminalWins(t *testing.T) {
	harvestTestDB(t)
	harvestSeedRow(t, "t-both-3", "same work both nodes", "pending", 1788000300, "")
	remote := harvestRemote("t-both-3", "same work both nodes", "completed", 1788000300)
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows([]harvestRemoteRow{remote}, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if counts.Migrated != 1 {
		t.Fatalf("counts = %+v, want migrated=1", counts)
	}
	if st, _, _ := harvestLocal(t, "t-both-3"); st != "completed" {
		t.Fatalf("local = %q, want completed (terminal result outranks pending)", st)
	}
}

// TestHarvestConflictSameStampBothTerminal: both sides finished but differ
// (completed vs failed) — a real conflict, nothing is rewritten.
func TestHarvestConflictSameStampBothTerminal(t *testing.T) {
	harvestTestDB(t)
	harvestSeedRow(t, "t-both-4", "diverging verdicts", "failed", 1788000300, "")
	remote := harvestRemote("t-both-4", "diverging verdicts", "completed", 1788000300)
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows([]harvestRemoteRow{remote}, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if counts.Conflict != 1 {
		t.Fatalf("counts = %+v, want conflict=1", counts)
	}
	if st, _, _ := harvestLocal(t, "t-both-4"); st != "failed" {
		t.Fatalf("local = %q, want failed (equal-rank conflict never rewrites)", st)
	}
}

// TestHarvestLiveLocalSkipped: a claimed/in_progress row is owned by this
// node — a newer live-status remote report never rewrites it. But a newer
// TERMINAL remote report IS the result and must migrate the row (the E2E
// bug038 path: HP completed the shared row; zpeer's live twin receives it).
func TestHarvestLiveLocalSkipped(t *testing.T) {
	harvestTestDB(t)
	harvestSeedRow(t, "t-live-1", "being executed here", "in_progress", 1788000100, "")
	remote := harvestRemote("t-live-1", "being executed here", "pending", 1788000900)
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows([]harvestRemoteRow{remote}, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if counts.Skipped != 1 {
		t.Fatalf("counts = %+v, want skipped=1", counts)
	}
	if st, _, _ := harvestLocal(t, "t-live-1"); st != "in_progress" {
		t.Fatalf("local = %q, want in_progress (live local execution owns the row)", st)
	}

	// Terminal remote result lands on a live local twin (migrated).
	harvestSeedRow(t, "t-live-2", "shared execution", "in_progress", 1788000100, "")
	result := harvestRemote("t-live-2", "shared execution", "completed", 1788000900)
	counts, err = TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows([]harvestRemoteRow{result}, nil)})
	if err != nil {
		t.Fatalf("terminal harvest: %v", err)
	}
	if counts.Migrated != 1 {
		t.Fatalf("counts = %+v, want migrated=1 (terminal result lands)", counts)
	}
	if st, _, _ := harvestLocal(t, "t-live-2"); st != "completed" {
		t.Fatalf("local = %q, want completed (result outranks the live twin)", st)
	}
}

// TestHarvestSameStampLiveTwinAbsorbed: the remote live twin arrives coerced
// to pending at the SAME stamp as the local live row — same work content
// absorbs as duped; a diverging same-stamp twin conflicts (the E2E 8-row
// duped signature: zpeer's pending twins vs HP's in_progress twins).
func TestHarvestSameStampLiveTwinAbsorbed(t *testing.T) {
	harvestTestDB(t)
	harvestSeedRow(t, "t-twin-1", "twin work", "in_progress", 1788000100, "seed-tag")
	// Same stamp, same content, remote live (coerced to pending). The seed
	// writes payload "seed payload <summary>" — the twin carries the same
	// shape (one source of truth for the twin's content).
	twin := harvestRemoteRow{
		TaskID: "t-twin-1", Summary: "twin work", Payload: "seed payload twin work",
		Tags: "seed-tag", Status: "in_progress",
		CreatedAt: 1788000090, UpdatedAt: 1788000100,
	}
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows([]harvestRemoteRow{twin}, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	t.Logf("counts=%+v", counts)
	if counts.Duped != 1 {
		t.Fatalf("counts = %+v, want duped=1 (identical live twin absorbed)", counts)
	}
	if st, _, _ := harvestLocal(t, "t-twin-1"); st != "in_progress" {
		t.Fatalf("local = %q, want in_progress (duped never rewrites)", st)
	}
}

// TestHarvestAnchorRewindOnFailure: a failed row keeps the anchor below its
// stamp — the next fetch re-requests it instead of losing it past the mark.
func TestHarvestAnchorRewindOnFailure(t *testing.T) {
	harvestTestDB(t)
	rows := []harvestRemoteRow{
		harvestRemote("t-hp-1", "good work", "completed", 1788000100),
		{TaskID: "t-hp-bad", Summary: "bad status", Status: "exploded", UpdatedAt: 1788000200, CreatedAt: 1788000190},
		harvestRemote("t-hp-3", "later work", "completed", 1788000300),
	}
	var fetchedAnchor float64
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Fetch: harvestRows(rows, &fetchedAnchor)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if counts.Failed != 1 || counts.Harvested != 2 {
		t.Fatalf("counts = %+v, want failed=1 harvested=2", counts)
	}
	// Anchor = earliest failed stamp − backoff (1788000200 − 1), minus the
	// skew rewind on top (1788000199 − 5).
	if anchor := harvestAnchor(t, "hp"); anchor != 1788000194 {
		t.Fatalf("anchor = %v, want 1788000194 (rewound below the failure)", anchor)
	}
}

// TestHarvestPeerRequired: no peer is a usage error, never a full pull.
func TestHarvestPeerRequired(t *testing.T) {
	if _, err := TaskHarvest(HarvestOptions{}); err == nil {
		t.Fatal("want an error for missing peer")
	}
}

// TestHarvestStrictlyNewerTerminalOverDivergentLive: DEFECT 1 — the audit
// (2026-09-04) strictly-newer branch of harvestReconcile has NO
// harvestSameContent gate. A remote row that is strictly newer AND terminal
// (completed) meets a locally LIVE row (in_progress — a run this node
// owns) carrying DIFFERENT work content under the SAME task_id (the
// same-second id-collision shape the epic protects elsewhere). The equal-
// stamp path and the pending-collision relabel both gate on
// harvestSameContent, but this branch does NOT: harvestUpdate overwrites
// payload/summary/status unconditionally → this node's live run's content
// is silently replaced by the peer's different work and marked completed —
// the silent-loss the task-id-collision epic exists to prevent.
func TestHarvestStrictlyNewerTerminalOverDivergentLive(t *testing.T) {
	harvestTestDB(t)
	const id = "t-collide"
	// Local: a live in_progress row with THIS node's own work content.
	harvestSeedRow(t, id, "local live work", taskStatusInProgress, 1788000100, "worker:zpeer")
	// Remote: strictly newer (stamp +10s), terminal, DIFFERENT content.
	rows := []harvestRemoteRow{
		{TaskID: id, Summary: "peer different work", Payload: "peer payload",
			Status: taskStatusCompleted, CreatedAt: 1788000105, UpdatedAt: 1788000110, EndedAt: 1788000110},
	}
	// Pin node so relabel would use a stable name (irrelevant here — the
	// defect path is the overwrite, not the relabel).
	t.Setenv("GOPI_SYNC_NODE", "zpeer")
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Fetch: harvestRows(rows, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	// The fix: the divergent strictly-newer terminal report is refused —
	// Skipped (never Migrated) and recorded in Errors, the local live run
	// left completely untouched (status + content).
	st, _, _ := harvestLocal(t, id)
	if counts.Migrated != 0 || counts.Skipped != 1 {
		t.Fatalf("counts = %+v, want the divergent overwrite SKIPPED (Migrated=0, Skipped=1)", counts)
	}
	if st != taskStatusInProgress {
		t.Fatalf("status = %s, want in_progress (the local live run must not be clobbered)", st)
	}
	row, err := syncTaskByID(id)
	if err != nil || row == nil {
		t.Fatalf("re-read: %v", err)
	}
	if row.Summary != "local live work" {
		t.Fatalf("local live row content was clobbered: summary=%q, want %q (silent overwrite)", row.Summary, "local live work")
	}
	refused := false
	for _, e := range counts.Errors {
		if strings.Contains(e, "DIFFERENT work") {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("refusal not recorded in counts.Errors: %v", counts.Errors)
	}
}

// TestHarvestNewerTerminalOverDivergentPendingFabricatesCompletion:
// DEFECT 4 — the audit (2026-09-04). A local PENDING row (work A — queued,
// never run) meets a strictly-newer remote TERMINAL row carrying DIFFERENT
// content (work B) under the SAME task_id (the same-second id-collision
// shape). The strictly-newer branch of harvestReconcile migrates it:
// pending → completed, adopting the peer's content B — fabricating a
// completion for work A that never ran. The v1 import channel REFUSES this
// exact shape (importTaskExisting: "terminal sync-back ... carries DIFFERENT
// work ... refusing to fabricate a completion"), and the harvest relabel
// comment above claims the same intent ("a remote TERMINAL report for a
// different task is refused (inserting a ghost completion fabricates
// history)") — but the relabel branch only fires for remote-live/pending
// rows, and the strictly-newer terminal path never content-checks.
func TestHarvestNewerTerminalOverDivergentPendingFabricatesCompletion(t *testing.T) {
	harvestTestDB(t)
	const id = "t-fab"
	// Local: this node's own queued work A (pending, never run).
	harvestSeedRow(t, id, "local queued work A", taskStatusPending, 1788000100, "")
	// Remote: strictly newer (stamp +10s), terminal, DIFFERENT content B.
	rows := []harvestRemoteRow{
		{TaskID: id, Summary: "peer different work B", Payload: "peer payload B",
			Status: taskStatusCompleted, CreatedAt: 1788000105, UpdatedAt: 1788000110, EndedAt: 1788000110},
	}
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows(rows, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	// DEFECT: the local pending row A was fabricated-completed with the
	// peer's content B — the collision guard must have refused or relabeled.
	if counts.Migrated != 0 {
		t.Fatalf("counts = %+v — a divergent terminal report over local queued work must NOT migrate (ghost completion), want Migrated=0 (refused/relabeled)", counts)
	}
	st, _, _ := harvestLocal(t, id)
	if st != taskStatusPending {
		t.Fatalf("status = %s, want pending — local queued work A must not be fabricated completed by peer work B", st)
	}
}
