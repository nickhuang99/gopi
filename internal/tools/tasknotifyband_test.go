package tools

// Regression guard for the task-notify terminal gap (epic
// task-notify-terminal-gap, 2026-09-12): every operator-relevant status
// transition must fire the configured notify through the ONE shared path,
// including the writers that do NOT funnel through taskUpdate.
//
// RED on the pre-fix tree: each test below drives a real writer
// (RollbackLocalClaim's revert, the rank ladder's terminal advance,
// harvestUpdate's adoption, the server conflict ruling) and asserts the delivered event. Before the fix those
// writers moved tasks.status with NO delivery at all.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// notifyEventsForTest collects EVERY delivered payload's event name from the
// capture file (one JSON object per line).
func notifyEventsForTest(t *testing.T, capture string) []string {
	t.Helper()
	raw, err := os.ReadFile(capture)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no delivery at all — the gap's signature
		}
		t.Fatalf("read capture: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var p taskNotifyPayload
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("payload parse %q: %v", line, err)
		}
		out = append(out, p.Event)
	}
	return out
}

// notifyCaptureForTest points the notify channel at an append-only capture
// file subscribed to every event, and returns its path.
func notifyCaptureForTest(t *testing.T) string {
	t.Helper()
	capture := filepath.Join(t.TempDir(), "notify-capture.jsonl")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+capture+" && echo >> "+capture)
	t.Setenv("GOPI_TASK_NOTIFY_WEBHOOK", "")
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", taskNotifyEventsAll)
	t.Setenv("GOPI_TASK_NOTIFY_PATTERN", "")
	t.Setenv(config.NotifyAsyncEnv, "")
	return capture
}

// hasEvent reports whether the delivered event list carries want.
func hasEvent(events []string, want string) bool {
	for _, e := range events {
		if e == want {
			return true
		}
	}
	return false
}

// TestNotifyGapRollbackLocalClaimFiresPending — the rollback writer
// (RollbackLocalClaim) moves a claimed row back to pending directly, with
// no taskUpdate in the path. Pre-fix: silent.
func TestNotifyGapRollbackLocalClaimFiresPending(t *testing.T) {
	// PROFILE-correct sandbox: this ledger IS the profile's own, so the delivery
	// gate (taskNotifyDelivers) passes its events through.
	taskNotifyProfileSandbox(t)
	capture := notifyCaptureForTest(t)

	newDeltaTestDBClient(t)
	deltaInsertRaw(t, "t-gap-rollback", "rollback target", taskStatusPending, 1, 1, "")
	if err := TaskClaimExplicit("t-gap-rollback", "host:1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := RollbackLocalClaim("t-gap-rollback"); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	events := notifyEventsForTest(t, capture)
	if !hasEvent(events, taskStatusPending) {
		t.Fatalf("rollback must notify pending (a real status move); delivered events: %v", events)
	}
}

// TestNotifyGapHarvestUpdateFiresTerminal — the harvest adoption
// (harvestUpdate, driven through the real TaskHarvest transaction) moves a
// local live row to its remote terminal truth. Pre-fix: silent. The flush
// must happen AFTER the commit, so the row is really terminal when the
// subscriber reads it.
func TestNotifyGapHarvestUpdateFiresTerminal(t *testing.T) {
	// OWN PROFILE's ledger, not testutil.NewTestDBPath: the raw pin moves the
	// ledger while leaving the profile ambient, and the delivery gate
	// (taskNotifyDelivers) correctly withholds such an event — this arm would
	// then measure the gate instead of the harvest alarm it names.
	taskNotifyProfileLedger(t)
	// The pull's own precondition: TaskHarvest refuses an undeclared alias
	// before the transport, so the fixture declares the one it pulls from
	// (harvestDeclarePeer) — otherwise this test would exercise the GATE.
	harvestDeclarePeer(t, harvestFixturePeer)
	capture := notifyCaptureForTest(t)

	seedTaskRow(t, "t-gap-harvest", "harvest target")
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("sync db: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET status = ? WHERE task_id = ?`, taskStatusPending, "t-gap-harvest"); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	closeDB()

	_, err = TaskHarvest(HarvestOptions{
		Peer: harvestFixturePeer,
		Fetch: func(peer string, anchor float64) ([]harvestRemoteRow, error) {
			return []harvestRemoteRow{{
				TaskID:    "t-gap-harvest",
				Summary:   "harvest target",
				Status:    taskStatusCompleted,
				UpdatedAt: 100,
				CreatedAt: 1,
			}}, nil
		},
		GitPushback: func() (*GitPushbackReport, error) { return &GitPushbackReport{}, nil },
	})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}

	events := notifyEventsForTest(t, capture)
	if !hasEvent(events, taskStatusCompleted) {
		t.Fatalf("harvest adoption must notify completed; delivered events: %v", events)
	}
}

// TestNotifyGapServerRulingFiresCompleted — the delta server's conflict
// ruling (syncDeltaServerResolve) writes status=completed directly. Pre-fix
// it fired ONLY the legacy-conflict alarm; the row's own completion was
// silent. The shared funnel must deliver both.
func TestNotifyGapServerRulingFiresCompleted(t *testing.T) {
	newDeltaTestDB(t)
	capture := notifyCaptureForTest(t)
	t.Setenv(config.SyncPeersEnv, "some-other-node") // not self → this node is the server
	t.Setenv(config.RoleEnv, "server")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)

	base := float64(1789000000)
	deltaInsertRaw(t, "t-gap-ruling", "contested", taskStatusInProgress, base, base, "team-a")
	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-gap-ruling", Summary: "contested", Status: taskStatusInProgress,
			UpdatedAt: int64(base), CreatedAt: int64(base), Source: "auto", Tags: []string{"team-b"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsResolved != 1 {
		t.Fatalf("server must rule (RowsResolved=1), got %+v", stats)
	}

	events := notifyEventsForTest(t, capture)
	for _, want := range []string{taskNotifyEventLegacyConflict, taskStatusCompleted} {
		if !hasEvent(events, want) {
			t.Fatalf("server ruling must notify %s; delivered events: %v", want, events)
		}
	}
}
