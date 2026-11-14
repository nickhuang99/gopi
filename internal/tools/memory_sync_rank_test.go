package tools

import (
	"context"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/testutil"
	"strings"
	"testing"
)

// rankTestPeer is the peer name the rank ladder's calls carry — the detail of
// the audit event names it (epic 01a187f4), so the fixture names one peer
// rather than threading a bare literal through every call.
const rankTestPeer = "peer1"

// TestStatusRankTable: the M4.20-B ladder — every status maps to its
// normative rank (pending 0 → claimed 1 → in_progress 2 → terminal-tier 3
// → verified 4; legacy reopened reads rank 0).
func TestStatusRankTable(t *testing.T) {
	cases := map[string]int{
		TaskStatusPending:    0,
		TaskStatusClaimed:    1,
		TaskStatusInProgress: 2,
		TaskStatusCompleted:  3,
		TaskStatusFailed:     3,
		TaskStatusCancelled:  3,
		TaskStatusVerified:   4,
		TaskStatusReopened:   0, // legacy read-only
		"bogus":              0, // unknown = most conservative
	}
	for status, want := range cases {
		if got := statusRankOf(status); got != want {
			t.Errorf("statusRankOf(%q) = %d, want %d", status, got, want)
		}
	}
	if !taskTerminalTier(TaskStatusCompleted) || !taskTerminalTier(TaskStatusFailed) || !taskTerminalTier(TaskStatusCancelled) {
		t.Error("completed/failed/cancelled must sit on the terminal tier")
	}
	if taskTerminalTier(TaskStatusVerified) || taskTerminalTier(TaskStatusInProgress) {
		t.Error("verified/in_progress are not terminal-tier peers")
	}
}

// rankMatrixTask seeds one row in `status` via the raw SQL path (the state
// machine gate refuses in_progress→pending, and the matrix needs arbitrary
// LOCAL start states; TaskNew gives in_progress rows).
func rankMatrixTask(t *testing.T, id, status string) {
	t.Helper()
	if _, err := TaskNew("rank matrix row "+id, id, "", ""); err != nil {
		t.Fatalf("TaskNew %s: %v", id, err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = ?, updated_at = 100 WHERE task_id = ?`, status, id); err != nil {
		t.Fatalf("seed %s → %s: %v", id, status, err)
	}
}

// runRankApply drives syncDeltaApplyNewer directly against the seeded DB.
func runRankApply(t *testing.T, taskID, remoteStatus string, remoteUpdatedAt int64) (*SyncTaskImportStats, error) {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	ctx := context.Background()
	existing, err := taskByID(ctx, db, taskID)
	if err != nil || existing == nil {
		t.Fatalf("taskByID %s: %v (nil=%v)", taskID, err, existing == nil)
	}
	stats := &SyncTaskImportStats{}
	err = syncDeltaApplyNewer(ctx, db, &taskNotifyBus{}, existing, SyncTaskRow{
		TaskID: taskID, Summary: "rank matrix row " + taskID, Status: remoteStatus,
		UpdatedAt: remoteUpdatedAt, CreatedAt: 1,
	}, rankTestPeer, stats)
	return stats, err
}

// TestRankApplyMatrix: the normative M4.20-B adjudication table.
func TestRankApplyMatrix(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	path := testutil.NewTestDBPath(t)
	if _, err := openTasksDB(context.Background(), path); err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}

	type tc struct {
		name, local, remote string
		remoteTs            int64
		wantStatus          string
		wantMirrored        int
		wantMigrated        int
		wantAlready         int
	}
	cases := []tc{
		// Forward rungs apply.
		{"pending→claimed", TaskStatusPending, TaskStatusClaimed, 200, TaskStatusClaimed, 1, 0, 0},
		{"pending→in_progress", TaskStatusPending, TaskStatusInProgress, 200, TaskStatusInProgress, 1, 0, 0},
		{"pending→completed", TaskStatusPending, TaskStatusCompleted, 200, TaskStatusCompleted, 0, 1, 0},
		{"pending→failed", TaskStatusPending, TaskStatusFailed, 200, TaskStatusFailed, 0, 1, 0},
		{"claimed→completed", TaskStatusClaimed, TaskStatusCompleted, 200, TaskStatusCompleted, 0, 1, 0},
		{"in_progress→completed", TaskStatusInProgress, TaskStatusCompleted, 200, TaskStatusCompleted, 0, 1, 0},
		{"completed→verified", TaskStatusCompleted, TaskStatusVerified, 200, TaskStatusVerified, 0, 1, 0},
		{"failed→completed later", TaskStatusFailed, TaskStatusCompleted, 200, TaskStatusCompleted, 0, 1, 0},
		// Terminal-tier peers: 后到覆盖 — later ts wins.
		{"completed→failed later", TaskStatusCompleted, TaskStatusFailed, 300, TaskStatusFailed, 0, 1, 0},
		// Terminal-tier peers: equal ts = idempotent no-op.
		{"completed→failed equal-ts noop", TaskStatusCompleted, TaskStatusFailed, 100, TaskStatusCompleted, 0, 0, 1},
		// Regression refused.
		{"verified→completed refused", TaskStatusVerified, TaskStatusCompleted, 300, TaskStatusVerified, 0, 0, 1},
		{"verified→in_progress refused", TaskStatusVerified, TaskStatusInProgress, 300, TaskStatusVerified, 0, 0, 1},
		{"completed→in_progress refused", TaskStatusCompleted, TaskStatusInProgress, 300, TaskStatusCompleted, 0, 0, 1},
		{"failed→claimed refused", TaskStatusFailed, TaskStatusClaimed, 300, TaskStatusFailed, 0, 0, 1},
	}
	for i, c := range cases {
		id := "t-rank-" + strings.ToLower(strings.ReplaceAll(c.name, "→", "-"))[:12] + "-" + rankItoa(i)
		rankMatrixTask(t, id, c.local)
		stats, err := runRankApply(t, id, c.remote, c.remoteTs)
		if err != nil {
			t.Fatalf("%s: apply: %v", c.name, err)
		}
		got, err := TaskShow(id)
		if err != nil {
			t.Fatalf("%s: TaskShow: %v", c.name, err)
		}
		if !strings.Contains(got, "status:         "+c.wantStatus) {
			t.Errorf("%s: status want %s got:\n%s", c.name, c.wantStatus, got)
		}
		if stats.RowsMirrored != c.wantMirrored || stats.RowsMigrated != c.wantMigrated || stats.RowsAlready != c.wantAlready {
			t.Errorf("%s: stats mirrored=%d migrated=%d already=%d, want %d/%d/%d",
				c.name, stats.RowsMirrored, stats.RowsMigrated, stats.RowsAlready, c.wantMirrored, c.wantMigrated, c.wantAlready)
		}
	}
}

func rankItoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestBilateralRankConvergence: two nodes advance the same row differently;
// both sides converge to the terminal truth regardless of merge order, and
// nothing regresses (the epic's 双侧模拟前进/倒退拒绝).
func TestBilateralRankConvergence(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	if _, err := openTasksDB(context.Background(), path); err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	rankMatrixTask(t, "t-bilateral", TaskStatusInProgress)

	// Node A's terminal verdict arrives first (ts 300).
	if _, err := runRankApply(t, "t-bilateral", TaskStatusCompleted, 300); err != nil {
		t.Fatalf("A verdict: %v", err)
	}
	// A stale in_progress echo (ts 100) must NOT regress the row.
	stats, err := runRankApply(t, "t-bilateral", TaskStatusInProgress, 100)
	if err != nil {
		t.Fatalf("stale echo: %v", err)
	}
	if stats.RowsAlready == 0 {
		t.Fatalf("stale in_progress echo must be absorbed, got %+v", stats)
	}
	got, _ := TaskShow("t-bilateral") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(got, "status:         completed") {
		t.Fatalf("row regressed after stale echo:\n%s", got)
	}
	// A later terminal peer verdict (failed, ts 400) covers — 后到覆盖.
	if _, err := runRankApply(t, "t-bilateral", TaskStatusFailed, 400); err != nil {
		t.Fatalf("B verdict: %v", err)
	}
	got, _ = TaskShow("t-bilateral") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(got, "status:         failed") {
		t.Fatalf("terminal-tier 后到覆盖 failed:\n%s", got)
	}
}

// TestRankGuardAtomicity: the WHERE-rank UPDATE re-derives the rank INSIDE
// the statement — a concurrent writer that outranks us between our read and
// our write makes the UPDATE hit 0 rows; the re-read then reports the row's
// newer truth (RowsLostRace; bug035 CAS lineage).
func TestRankGuardAtomicity(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	if _, err := openTasksDB(context.Background(), path); err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	rankMatrixTask(t, "t-raceguard", TaskStatusPending)

	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	ctx := context.Background()
	existing, err := taskByID(ctx, db, "t-raceguard")
	if err != nil || existing == nil {
		t.Fatalf("taskByID: %v", err)
	}
	// Concurrent writer: pending → completed (rank 3) AFTER our read, BEFORE
	// our write (simulated by writing directly now).
	if _, err := db.Exec(`UPDATE tasks SET status = ?, updated_at = 500 WHERE task_id = 't-raceguard'`,
		TaskStatusCompleted); err != nil {
		t.Fatalf("concurrent write: %v", err)
	}
	// Our (now stale) apply wants claimed (rank 1) < completed (rank 3):
	// the rank guard must refuse, the completed truth must survive.
	stats := &SyncTaskImportStats{}
	err = syncDeltaApplyNewer(ctx, db, &taskNotifyBus{}, existing, SyncTaskRow{
		TaskID: "t-raceguard", Summary: "rank matrix row t-raceguard", Status: TaskStatusClaimed,
		UpdatedAt: 200, CreatedAt: 1,
	}, rankTestPeer, stats)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, _ := TaskShow("t-raceguard") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(got, "status:         completed") {
		t.Fatalf("rank guard clobbered the newer state:\n%s", got)
	}
	if stats.RowsLostRace == 0 {
		t.Fatalf("RowsLostRace must report the guard hit, got %+v", stats)
	}
}

// TestLegacyReopenedRowSyncs: a legacy DB row in reopened still opens,
// lists, and ranks as pending (read-only legacy tolerance — no crash).
func TestLegacyReopenedRowSyncs(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	if _, err := openTasksDB(context.Background(), path); err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	rankMatrixTask(t, "t-legacy-ro", TaskStatusPending)
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET status = 'reopened' WHERE task_id = 't-legacy-ro'`); err != nil {
		t.Fatalf("legacy stamp: %v", err)
	}
	closeDB()
	// Forward sync still applies over the legacy rank-0 row.
	if _, err := runRankApply(t, "t-legacy-ro", TaskStatusCompleted, 300); err != nil {
		t.Fatalf("apply over legacy reopened: %v", err)
	}
	got, _ := TaskShow("t-legacy-ro") //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if !strings.Contains(got, "status:         completed") {
		t.Fatalf("legacy reopened row did not converge:\n%s", got)
	}
}

// TestResumeEdgeSurvivesSync pins the ONE backward edge the rank ladder must not
// adjudicate: a row this host RESUMED (`failed → in_progress`) arriving beside a
// peer's pre-resume `failed` copy. The peer ranks as terminal (3) and the local
// resume as 2, so the plain ladder would move the row BACKWARDS onto a verdict
// this host has already left — the operator's own `task status` would then refuse
// the row's live run, and a legal resume would look like it was undone.
//
// The assertion is deliberately two-sided: the resume is KEPT **and** the stats
// say so in their own counter (RowsResumeKept), because "nothing happened to this
// row" and "we refused a peer's verdict in favour of a live local run" are
// different answers for whoever asks whether a sync ate a resume.
func TestResumeEdgeSurvivesSync(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	if _, err := openTasksDB(context.Background(), path); err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	// The row's real history: it FAILED, and this host resumed it.
	const id = "t-resume-edge"
	rankMatrixTask(t, id, TaskStatusFailed)
	if _, err := TaskUpdateReason(id, TaskStatusInProgress, "resume: level 1", ""); err != nil {
		t.Fatalf("the resume edge is missing from taskAllowedTransitions: %v", err)
	}

	// A peer that never saw the resume pulls its version back: failed, and NEWER
	// by the clock — so nothing but the edge itself can keep the local resume.
	stats, err := runRankApply(t, id, TaskStatusFailed, 900)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := TaskShow(id)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(got, "status:         "+TaskStatusInProgress) {
		t.Fatalf("the peer's pre-resume `failed` overwrote a live local resume:\n%s", got)
	}
	if stats.RowsResumeKept != 1 {
		t.Errorf("rows_resume_kept = %d, want 1 — a kept resume must be COUNTED, not silently absorbed", stats.RowsResumeKept)
	}
	if stats.RowsMigrated != 0 || stats.RowsMirrored != 0 {
		t.Errorf("mirrored=%d migrated=%d, want 0/0 — the refused write must not be reported as an applied one", stats.RowsMirrored, stats.RowsMigrated)
	}

	// AND THE LADDER STILL WORKS EVERYWHERE ELSE: a peer that advanced PAST the
	// resume (a terminal verdict) still wins, so "keep the resume" is not "freeze
	// the row". completed outranks in_progress on the ordinary rung.
	stats, err = runRankApply(t, id, TaskStatusCompleted, 1000)
	if err != nil {
		t.Fatalf("apply completed: %v", err)
	}
	got, err = TaskShow(id)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(got, "status:         "+TaskStatusCompleted) {
		t.Fatalf("a forward rung after the resume was absorbed:\n%s", got)
	}
	if stats.RowsMigrated != 1 {
		t.Errorf("migrated = %d, want 1 (the forward move must apply normally)", stats.RowsMigrated)
	}
}
