package tools

// memory_sync_status_event_test.go — the regression guard for epic 01a187f4:
// EVERY sync leg that moves a row's status must leave an audit entry in
// task_events, exactly the way the local dispatcher's writer does.
//
// THE DEFECT THIS CLOSES (measured 2026-10-29): three legs wrote `status` and
// called taskEventWrite zero times — the v2 delta ladder (syncDeltaApplyNewer),
// the v1 harvest pull (harvestUpdate) and the v1 import sync-back
// (syncTaskMoveStatusTx). Eight rows therefore sat `cancelled` on this host
// with an EMPTY trail, while the peer that cancelled them held one event each
// in the same second: "who moved this row, and from what?" could be answered
// only from the other node's ledger. The notify stream fired; the audit did not.
//
// THE NEGATIVE CONTROL IS PART OF THE CONTRACT, not decoration: a replayed
// sync (equal stamps, equal status) takes the RowsAlready/echo path and must
// append NOTHING. Without that half, "write an event" would be satisfied by a
// writer that appends on every exchange and turns the trail into noise.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// syncEventPeer is the peer name the fixtures import from — the detail must
// name it, so one constant serves the seeding and the assertion (Rule 1b).
const syncEventPeer = "peer1"

// syncEventFixture pins a fresh ledger (Rule 32 scaffold) and returns its path
// with the schema built.
func syncEventFixture(t *testing.T) string {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	if _, err := openTasksDB(context.Background(), path); err != nil {
		t.Fatalf("build the ledger: %v", err)
	}
	return path
}

// syncEventSeed writes one row in an exact status/stamp (TaskSubmit would
// stamp now — these assertions need the ladder's inputs to be pinned).
func syncEventSeed(t *testing.T, path, taskID, summary, status string, updated float64) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("open the seed handle: %v", err)
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
		 VALUES (?, ?, ?, '', '', '', '', ?, 'auto', '', ?, ?, ?)`,
		taskID, summary, status, "payload "+summary, updated-10, updated, ended); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
}

// syncEventTrail reads the row's whole audit trail, oldest first — the
// assertion side of "the ledger says who moved it".
func syncEventTrail(t *testing.T, path, taskID string) (events, details []string) {
	t.Helper()
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open the read handle: %v", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(),
		`SELECT COALESCE(event,''), COALESCE(detail,'') FROM task_events WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		t.Fatalf("read the trail of %s: %v", taskID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var ev, detail string
		if err := rows.Scan(&ev, &detail); err != nil {
			t.Fatalf("scan the trail of %s: %v", taskID, err)
		}
		events = append(events, ev)
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("walk the trail of %s: %v", taskID, err)
	}
	return events, details
}

// syncEventRequireOneMove asserts the ONE entry a real status move owes: the
// event name is the new status (the local writer's convention), and the detail
// names the leg, the peer and the state it moved away from.
func syncEventRequireOneMove(t *testing.T, path, taskID, leg, prevStatus, status string) {
	t.Helper()
	events, details := syncEventTrail(t, path, taskID)
	if len(events) != 1 {
		t.Fatalf("%s trail = %v, want exactly ONE entry after one real status move", taskID, details)
	}
	if events[0] != status {
		t.Fatalf("%s event name = %q, want the new status %q", taskID, events[0], status)
	}
	want := "sync " + leg + " from " + syncEventPeer + ": " + prevStatus + " → " + status
	if details[0] != want {
		t.Fatalf("%s detail = %q, want %q", taskID, details[0], want)
	}
	if !strings.Contains(details[0], syncEventPeer) || !strings.Contains(details[0], prevStatus) {
		t.Fatalf("%s detail %q must name the peer and the previous status", taskID, details[0])
	}
}

// TestSyncDeltaStatusMoveRecordsTrailEvent — the v2 delta ladder's forward move
// records its entry, and the IDEMPOTENT REPLAY of the same truth records none
// (the negative control: equal stamps + equal status → RowsAlready).
func TestSyncDeltaStatusMoveRecordsTrailEvent(t *testing.T) {
	path := syncEventFixture(t)
	const id = "t-delta-status"
	syncEventSeed(t, path, id, "delta status row", TaskStatusPending, 100)

	apply := func(updatedAt int64) *SyncTaskImportStats {
		t.Helper()
		db, closeDB, err := taskSyncDB()
		if err != nil {
			t.Fatalf("taskSyncDB: %v", err)
		}
		defer closeDB()
		ctx := context.Background()
		existing, err := taskByID(ctx, db, id)
		if err != nil || existing == nil {
			t.Fatalf("taskByID %s: %v", id, err)
		}
		stats := &SyncTaskImportStats{}
		if err := syncDeltaApplyNewer(ctx, db, &taskNotifyBus{}, existing, SyncTaskRow{
			TaskID: id, Summary: "delta status row", Status: TaskStatusCompleted,
			UpdatedAt: updatedAt, EndedAt: updatedAt, CreatedAt: 1,
		}, syncEventPeer, stats); err != nil {
			t.Fatalf("apply delta: %v", err)
		}
		return stats
	}

	first := apply(500)
	if first.RowsMigrated != 1 {
		t.Fatalf("first apply = %+v, want the row migrated", first)
	}
	syncEventRequireOneMove(t, path, id, syncStatusEventLegDelta, TaskStatusPending, TaskStatusCompleted)

	// NEGATIVE CONTROL: the very same row, re-delivered. The ladder's
	// terminal-tier rule answers RowsAlready (equal status, not newer) and must
	// append nothing.
	second := apply(500)
	if second.RowsAlready != 1 || second.RowsMigrated != 0 {
		t.Fatalf("replay = %+v, want already=1 migrated=0", second)
	}
	if events, details := syncEventTrail(t, path, id); len(events) != 1 {
		t.Fatalf("replay appended to the trail: %v — an idempotent sync must write NO event", details)
	}
}

// TestSyncHarvestStatusMoveRecordsTrailEvent — the v1 harvest pull's forward
// move records its entry, and re-harvesting the same row records none.
func TestSyncHarvestStatusMoveRecordsTrailEvent(t *testing.T) {
	path := syncEventFixture(t)
	harvestDeclarePeer(t, syncEventPeer)
	const id = "t-harvest-status"
	syncEventSeed(t, path, id, "harvest status row", TaskStatusPending, 100)

	remote := harvestRemote(id, "harvest status row", TaskStatusCompleted, 500)
	run := func() *HarvestCounts {
		t.Helper()
		counts, err := TaskHarvest(HarvestOptions{
			Peer:  syncEventPeer,
			Fetch: harvestRows([]harvestRemoteRow{remote}, nil),
		})
		if err != nil {
			t.Fatalf("harvest: %v", err)
		}
		return counts
	}

	if counts := run(); counts.Migrated != 1 {
		t.Fatalf("first harvest = %+v, want migrated=1", counts)
	}
	syncEventRequireOneMove(t, path, id, syncStatusEventLegHarvest, TaskStatusPending, TaskStatusCompleted)

	// NEGATIVE CONTROL: the same remote truth re-arrives (the anchor rewind
	// makes the peer re-send it by design) and is absorbed — no second entry.
	run()
	if events, details := syncEventTrail(t, path, id); len(events) != 1 {
		t.Fatalf("re-harvest appended to the trail: %v — an idempotent pull must write NO event", details)
	}
}

// TestSyncImportStatusMoveRecordsTrailEvent — the v1 file-channel sync-back
// (pending → completed through syncTaskMoveStatusTx, inside the import tx)
// records its entry, and a re-import of the same patch records none.
func TestSyncImportStatusMoveRecordsTrailEvent(t *testing.T) {
	path := syncEventFixture(t)
	const id = "t-import-status"
	syncEventSeed(t, path, id, "import status row", TaskStatusPending, 100)

	patch := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: syncEventPeer, ExportedAt: 500,
		Rows: []SyncTaskRow{{
			TaskID: id, Summary: "import status row", Payload: "payload import status row",
			Status: TaskStatusCompleted, UpdatedAt: 500, EndedAt: 500, CreatedAt: 90,
		}},
	}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncTaskPatch(&patch, patchPath); err != nil {
		t.Fatalf("write patch: %v", err)
	}

	if stats, err := SyncTaskImport(patchPath); err != nil {
		t.Fatalf("import: %v", err)
	} else if stats.RowsMigrated != 1 {
		t.Fatalf("first import = %+v, want migrated=1", stats)
	}
	syncEventRequireOneMove(t, path, id, syncStatusEventLegImport, TaskStatusPending, TaskStatusCompleted)

	// NEGATIVE CONTROL: re-importing the same patch is the idempotent path.
	if stats, err := SyncTaskImport(patchPath); err != nil {
		t.Fatalf("re-import: %v", err)
	} else if stats.RowsAlready != 1 || stats.RowsMigrated != 0 {
		t.Fatalf("re-import = %+v, want already=1 migrated=0", stats)
	}
	if events, details := syncEventTrail(t, path, id); len(events) != 1 {
		t.Fatalf("re-import appended to the trail: %v — an idempotent import must write NO event", details)
	}
}
