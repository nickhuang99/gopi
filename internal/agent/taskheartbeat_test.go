package agent

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// TestTouchTasklistNotThrottledAfterToolcall — regression test for the
// tasklist_issued_at throttle bug (M4.5 ⑦ fix): a [TASK_LIST] block arriving
// <30s after any tool batch must still stamp tasklist_issued_at. Pre-fix the
// tasklist touch shared heartbeatState.lastWrite with the toolcall touch, so
// the tasklist write was silently dropped (live evidence: session 8cbf768f
// emitted 3 [TASK_LIST_BEGIN] blocks, tasklist_issued_at stayed NULL).
//
// Contract restored by the fix: the two touches use SEPARATE throttle clocks;
// the tasklist touch only stamps tasklist_issued_at (never updated_at, the
// M4.13 sync version anchor).
func TestTouchTasklistNotThrottledAfterToolcall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	if _, err := tools.TaskNew("[auto] throttle regression row", "sess-throttle", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}

	setTaskHeartbeatSession("sess-throttle", "sess-throttle")
	defer setTaskHeartbeatSession("", "")

	// The bug sequence: tool batch fires first, tasklist arrives <30s later.
	touchLastToolcallAt()
	touchTasklistIssuedAt() // pre-fix: dropped (shared clock, <30s)

	row, err := tools.TaskShow("sess-throttle")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(row, "sess-throttle") {
		t.Fatalf("row broken:\n%s", row)
	}
	if !strings.Contains(row, "last_toolcall_at") {
		t.Errorf("last_toolcall_at missing after toolcall touch:\n%s", row)
	}
	if !strings.Contains(row, "tasklist_issued_at") {
		t.Errorf("tasklist_issued_at silently dropped <30s after toolcall touch:\n%s", row)
	}
}

// TestTaskShowLivenessColumns — the optional Epic item: an in_progress row
// with non-NULL liveness stamps renders a `liveness:` line in task show, so
// a mid-run row (created_at == updated_at) is self-explanatory. Rows with
// no liveness stamps render no liveness line (raw facts only).
func TestTaskShowLivenessColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	if _, err := tools.TaskNew("[auto] liveness show row", "sess-live", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}

	setTaskHeartbeatSession("sess-live", "sess-live")
	touchLastToolcallAt()
	touchTasklistIssuedAt()

	row, err := tools.TaskShow("sess-live")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(row, "liveness:") {
		t.Errorf("in_progress row with both stamps must render a liveness line:\n%s", row)
	}
	if strings.Contains(row, "tasklist_issued_at=(none)") {
		t.Errorf("tasklist_issued_at was stamped, must not show (none):\n%s", row)
	}

	// A fresh in_progress row with no stamps: the liveness line still
	// renders, with "(none)" — a mid-run snapshot must be self-explanatory
	// (created_at==updated_at is NORMAL for an un-transitioned live row;
	// the liveness line is where "still running" is visible). The old
	// behavior (no line for un-stamped rows) made a fresh live row read as
	// dead — the created==updated confusion this epic reports.
	if _, err := tools.TaskNew("[auto] no stamps row", "sess-bare", "", ""); err != nil {
		t.Fatalf("TaskNew bare: %v", err)
	}
	bare, err := tools.TaskShow("sess-bare")
	if err != nil {
		t.Fatalf("show bare: %v", err)
	}
	if !strings.Contains(bare, "liveness:") {
		t.Errorf("in_progress row must always render a liveness line (created==updated self-explanation):\n%s", bare)
	}
	// The liveness line is a tick-tock READING (epic task-liveness-ticktock):
	// each stamp renders as "<clock> (age <dur> — fresh|stale)", and a NULL
	// column renders as an explicit absence carrying the "unknown" verdict.
	// An unrecorded fact must never read as stale (Rule 15) — that is what
	// this assertion pins, so it checks the shapes rather than one spelling.
	for _, col := range []string{"last_toolcall_at", "tasklist_issued_at"} {
		fact := col + "=(none"
		if !strings.Contains(bare, fact) {
			t.Errorf("un-stamped row must render %s as an absence:\n%s", col, bare)
		}
	}
	if !strings.Contains(bare, "unknown") {
		t.Errorf("an absent liveness fact must carry the unknown verdict:\n%s", bare)
	}
	if strings.Contains(bare, "stale") {
		t.Errorf("an absent liveness fact must never read as stale:\n%s", bare)
	}
}

// TestHeartbeatTouchConsolidatedRow: with task-id consolidation the
// auto-registered row's task_id (t-<unix>) differs from its session UUID —
// the heartbeat must touch the RIGHT row via the retained task_id, not
// assume task_id == session_id. Regression: pre-fix the touch keyed on
// session id as the task_id, so a consolidated row's liveness columns
// stayed NULL and the run looked dead (the created==updated confusion).
func TestHeartbeatTouchConsolidatedRow(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	// Consolidation shape: task_id t-heart-1, session_id sess-uuid-1.
	if _, err := tools.TaskNew("[auto] consolidated heartbeat row", "t-heart-1", "sess-uuid-1", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}

	setTaskHeartbeatSession("sess-uuid-1", "t-heart-1")
	defer setTaskHeartbeatSession("", "")

	touchLastToolcallAt()
	touchTasklistIssuedAt()

	row, err := tools.TaskShow("t-heart-1")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(row, "liveness:") || strings.Contains(row, "last_toolcall_at=(none)") {
		t.Errorf("consolidated row must get liveness stamps via its task_id:\n%s", row)
	}
	// The session_id must NOT have been treated as a task_id (no accidental
	// row keyed by the UUID).
	if _, err := tools.TaskShow("sess-uuid-1"); err == nil {
		t.Errorf("session UUID must not be a task row (consolidation):\n%s", row)
	}
}

// readLastToolcall reads the raw last_toolcall_at column of one row: a
// NullFloat64 so a NULL is an ABSENCE, never a zero (Rule 15 — an unrecorded
// fact must not read as a value).
func readLastToolcall(t *testing.T, path, taskID string) sql.NullFloat64 {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(path))
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close() //stylegate:ignore per-assertion read handle; the OS reclaims it at test end
	var v sql.NullFloat64
	if err := db.QueryRow(`SELECT last_toolcall_at FROM tasks WHERE task_id = ?`, taskID).Scan(&v); err != nil {
		t.Fatalf("read last_toolcall_at of %s: %v", taskID, err)
	}
	return v
}

// withFastHeartbeat zeroes the write throttle and shrinks the keepalive tick
// so a test can observe a REAL ledger write without waiting the production
// 30s / 2min, and restores both seams afterwards. The seams default to the
// named constants (Rule 1) — this changes only the observed run's pace.
func withFastHeartbeat(t *testing.T) {
	t.Helper()
	prevThrottle, prevTick := taskHeartbeatThrottle, runKeepaliveTick
	taskHeartbeatThrottle, runKeepaliveTick = 0, 20*time.Millisecond
	t.Cleanup(func() { taskHeartbeatThrottle, runKeepaliveTick = prevThrottle, prevTick })
}

// TestHeartbeatWrittenAtSessionRegistration is interval (a) of the liveness
// coverage: the moment the run's ledger row exists and the heartbeat latch is
// set, the row carries a liveness fact. Pre-fix the ONLY writers were the two
// tool-batch entries, so a run killed before its first batch left
// last_toolcall_at NULL and its row's only instant was created_at — the
// "registered but never started" shape that reads identically to a dead run.
func TestHeartbeatWrittenAtSessionRegistration(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	if _, err := tools.TaskNew("[auto] registration row", "t-reg", "sess-reg", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if v := readLastToolcall(t, path, "t-reg"); v.Valid {
		t.Fatalf("fixture: last_toolcall_at must start NULL, got %v", v.Float64)
	}

	// The run starts: latch the row. NO tool batch, NO tasklist block.
	setTaskHeartbeatSession("sess-reg", "t-reg")
	defer setTaskHeartbeatSession("", "")

	v := readLastToolcall(t, path, "t-reg")
	if !v.Valid {
		t.Fatalf("registration left last_toolcall_at NULL: a run killed before its first batch is indistinguishable from a dead one")
	}
}

// TestHeartbeatAdvancesWithNoToolBatchDuringPause is interval (c) of the
// liveness coverage — the MINIMAL REPRO: the run is ALIVE but executes no
// tool batch and emits no [TASK_LIST] block for a while (a long model
// inference, or one long tool call such as the measured 10-minute bash). Pre-fix
// nothing wrote during such a pause, so the row's newest liveness instant
// froze while the process was demonstrably running; the keepalive now advances
// it, and stops the moment the run's context is done (no false-live row can
// outlive its run).
func TestHeartbeatAdvancesWithNoToolBatchDuringPause(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	withFastHeartbeat(t)
	if _, err := tools.TaskNew("[auto] keepalive row", "t-keep", "sess-keep", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	setTaskHeartbeatSession("sess-keep", "t-keep")
	defer setTaskHeartbeatSession("", "")

	before := readLastToolcall(t, path, "t-keep")
	if !before.Valid {
		t.Fatalf("no liveness stamp at registration")
	}

	ctx, cancel := context.WithCancel(context.Background())
	startRunKeepalive(ctx)

	// THE PAUSE: no tool batch, no tasklist block, no loop iteration.
	time.Sleep(2500 * time.Millisecond) //stylegate:allow-sleep the pause under test — the keepalive must write with no batch on the run path

	after := readLastToolcall(t, path, "t-keep")
	if !after.Valid || after.Float64 <= before.Float64 {
		t.Fatalf("heartbeat did not advance during a batch-less pause: before=%v after=%v", before, after)
	}

	// It DIES with the run: a cancelled context ends the ticker, so no write
	// can land after the run is gone (the false-live failure mode a detached
	// keeper would have).
	cancel()
	time.Sleep(300 * time.Millisecond) //stylegate:allow-sleep lets any in-flight tick land before the post-cancel reading
	settled := readLastToolcall(t, path, "t-keep")
	time.Sleep(300 * time.Millisecond) //stylegate:allow-sleep same, for the "no further writes" assertion
	if final := readLastToolcall(t, path, "t-keep"); final.Float64 != settled.Float64 {
		t.Errorf("keepalive wrote after its run's context was done: %v → %v", settled, final)
	}
}

// TestHeartbeatWrittenAtIterationStart is interval (b) of the liveness
// coverage: the ReAct loop's round boundary. It is the touch that covers a
// long model inference, which no tool batch can (the batch has not started).
func TestHeartbeatWrittenAtIterationStart(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	withFastHeartbeat(t)
	if _, err := tools.TaskNew("[auto] iteration row", "t-iter", "sess-iter", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	setTaskHeartbeatSession("sess-iter", "t-iter")
	defer setTaskHeartbeatSession("", "")

	before := readLastToolcall(t, path, "t-iter")
	if !before.Valid {
		t.Fatalf("no liveness stamp at registration")
	}
	time.Sleep(1200 * time.Millisecond) //stylegate:allow-sleep crosses a wall-clock second so the next stamp is strictly newer
	touchRunIterationStart()
	after := readLastToolcall(t, path, "t-iter")
	if !after.Valid || after.Float64 <= before.Float64 {
		t.Fatalf("iteration start did not advance the liveness fact: before=%v after=%v", before, after)
	}
}
