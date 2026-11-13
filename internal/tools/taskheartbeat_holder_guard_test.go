package tools

// The heartbeat of a row whose HOLDER IS DEAD must not advance.
//
// A row's liveness columns (last_toolcall_at / tasklist_issued_at) are written
// by `touch` (taskheartbeat.go). The writer used to be addressed ONLY by row
// identity (task_id / session_id) — nothing about WHO is writing. That is
// enough in the healthy case, where the process writing the heartbeat IS the
// row's holder, but it is a hole the moment a SURVIVOR outlives its holder:
// the measured shape (task 01a163df, 2026-10-22) is a wrapper SIGKILLed while
// the child it launched is still alive with the run's cwd and env, so the
// child keeps stamping the dead holder's row. The stale sweep's candidate
// query (`taskLivenessInstantSQL() < staleSince`) then never sees the row
// again: a dead run stays in_progress forever and is never reclaimed.
//
// The guard these tests pin: a heartbeat write is REFUSED when the row's claim
// stamp (`claimed_by`, the same "hostname:pid" stamp every other liveness
// judgement in this package parses via claimHolder) names a process on THIS
// host that is confirmed dead. Everything else stays writable — the fail-soft
// direction must match pidAlive's: an unreadable /proc, a foreign host, or a
// row with no stamp at all keeps writing, because a heartbeat that is refused
// for the wrong reason turns into a healthy run killed by the stale sweep.
//
// RED PROOF: on the pre-guard tree both rows accept the touch, so
// TestHeartbeatRefusesADeadHolder is red at its dead-holder assertion and
// green everywhere else. See the report's ## Evidence for the measured run.

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// heartbeatGuardDeadRow is the row whose claim stamp points at a pid this
	// test MEASURED dead (a reaped probe process).
	heartbeatGuardDeadRow = "t-hb-holder-dead"

	// heartbeatGuardLiveRow is the control: a healthy run's row, stamped with
	// a pid that is demonstrably alive (this test process).
	heartbeatGuardLiveRow = "t-hb-holder-live"

	// heartbeatGuardForeignRow is stamped from another host: the pid's meaning
	// is unknowable here, so the guard must NOT refuse (fail-soft).
	heartbeatGuardForeignRow = "t-hb-holder-foreign"

	// heartbeatGuardNoStampRow carries no claim stamp at all (an auto-
	// registered session row): nothing to judge, so the write stands.
	heartbeatGuardNoStampRow = "t-hb-holder-nostamp"

	// heartbeatGuardForeignHost is a hostname that is not this machine. The
	// pid beside it (1) is deliberately one that DOES exist locally, so a
	// guard that ignored the host component would refuse the row.
	heartbeatGuardForeignHost = "some-other-host"

	// heartbeatGuardStamp is the liveness value written by the PROBE touches
	// (the pre-existing stamps are distinct from it, so "did it move" is a
	// column comparison rather than a clock read).
	heartbeatGuardStamp = 4000000000.0

	// heartbeatGuardBaseline is the pre-existing liveness value of every
	// fixture row; a touch that lands replaces it with heartbeatGuardStamp.
	heartbeatGuardBaseline = 1000000000.0
)

// heartbeatGuardFixture seeds the four rows this test contrasts and returns
// the ledger path. Each row is an in_progress row keyed by its task_id (the
// shape the heartbeat addresses first) carrying one distinct claim stamp.
func heartbeatGuardFixture(t *testing.T) string {
	t.Helper()
	path := testutil.NewTestDBPath(t)

	for _, id := range []string{
		heartbeatGuardDeadRow, heartbeatGuardLiveRow,
		heartbeatGuardForeignRow, heartbeatGuardNoStampRow,
	} {
		seedTaskRow(t, id, "[test] heartbeat holder guard "+id)
	}

	db := openTestTasksDB(t, path)
	defer db.Close() //stylegate:ignore fixture handle closed at the end of the seeding pass; the assertions open their own
	stamps := []struct {
		id        string
		claimedBy string
	}{
		{heartbeatGuardDeadRow, claimHolderOfPid(heartbeatGuardDeadPid(t))},
		{heartbeatGuardLiveRow, claimHolderOfPid(os.Getpid())},
		{heartbeatGuardForeignRow, heartbeatGuardForeignHost + claimHolderHostPidSep + "1"},
		{heartbeatGuardNoStampRow, ""},
	}
	for _, s := range stamps {
		if _, err := db.Exec(
			`UPDATE tasks SET claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+` = ?,
			 `+taskLastToolcallAtColumn+` = ?, `+taskTasklistIssuedAtColumn+` = ? WHERE task_id = ?`,
			s.claimedBy, heartbeatGuardBaseline, heartbeatGuardBaseline,
			heartbeatGuardBaseline, heartbeatGuardBaseline, s.id); err != nil {
			t.Fatalf("stamp %s: %v", s.id, err)
		}
	}
	return path
}

// heartbeatGuardDeadPid mints a pid this host has MEASURED dead: it starts a
// reaped probe process and returns the pid the kernel gave it. A reaped pid is
// gone (pidAlive false) and nothing else can be holding it at that instant —
// the same fixture the close-out orphan tests use, reused rather than
// re-invented (Rule 13).
func heartbeatGuardDeadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], orphanHolderProbeRunArg)
	if err := cmd.Start(); err != nil {
		t.Skipf("heartbeat guard fixture: cannot start the dead-pid probe (%v)", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait() //stylegate:ignore reaps the child so its pid is genuinely dead; the exit status is not the assertion
	if pidAlive(pid) {
		t.Skipf("probe pid %d is still alive — a reaped pid is this fixture's premise", pid)
	}
	return pid
}

// heartbeatGuardColumn reads one liveness column for a row.
func heartbeatGuardColumn(t *testing.T, db *sql.DB, column, taskID string) (float64, bool) {
	t.Helper()
	var v sql.NullFloat64
	if err := db.QueryRow(
		`SELECT `+column+` FROM tasks WHERE task_id = ?`, taskID).Scan(&v); err != nil {
		t.Fatalf("read %s of %s: %v", column, taskID, err)
	}
	return v.Float64, v.Valid
}

// TestHeartbeatRefusesADeadHolder is the minimal repro + acceptance for the
// epic: a dead holder's row must NOT be kept alive by a survivor, while a live
// holder's row, a foreign-host stamp and a stamp-less row keep writing.
func TestHeartbeatRefusesADeadHolder(t *testing.T) {
	path := heartbeatGuardFixture(t)

	h := OpenTaskHeartbeatConn(context.Background())
	if h == nil {
		t.Fatalf("heartbeat conn nil")
	}
	defer h.Close() //stylegate:ignore the assertions below open their own handle on the same file

	// ① THE GUARD: the dead holder's row refuses the touch, by name.
	err := h.ExecTouchLastToolcall(heartbeatGuardDeadRow, heartbeatGuardDeadRow, heartbeatGuardStamp)
	if err == nil {
		t.Fatalf("heartbeat touch on a DEAD holder's row succeeded — the survivor can keep a dead run alive and the stale sweep will never see it again (task 01a163df)")
	}
	for _, want := range []string{heartbeatGuardDeadRow, "dead"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q — every refusal must say what it refused and why (Rule 15)", err.Error(), want)
		}
	}

	// ② the live holder's row: the healthy path is untouched.
	if err := h.ExecTouchLastToolcall(heartbeatGuardLiveRow, heartbeatGuardLiveRow, heartbeatGuardStamp); err != nil {
		t.Fatalf("heartbeat touch on a LIVE holder's row was refused: %v", err)
	}

	// ③ fail-soft: a foreign-host stamp names no local process, so the guard
	// must not refuse it (refusing here would starve a healthy remote run).
	if err := h.ExecTouchLastToolcall(heartbeatGuardForeignRow, heartbeatGuardForeignRow, heartbeatGuardStamp); err != nil {
		t.Fatalf("heartbeat touch on a foreign-host stamp was refused (fail-soft violated): %v", err)
	}

	// ④ fail-soft: no stamp at all (an auto-registered session row).
	if err := h.ExecTouchLastToolcall(heartbeatGuardNoStampRow, heartbeatGuardNoStampRow, heartbeatGuardStamp); err != nil {
		t.Fatalf("heartbeat touch on a stamp-less row was refused (fail-soft violated): %v", err)
	}

	// The refusal is a REFUSAL, not a partial write: the dead row's column
	// still holds its pre-existing value, so the stale sweep reads the true
	// last sign of life rather than a fabricated one.
	db := openTestTasksDB(t, path)
	defer db.Close() //stylegate:ignore assertion handle closed at the end of the test
	if v, ok := heartbeatGuardColumn(t, db, taskLastToolcallAtColumn, heartbeatGuardDeadRow); !ok || v != heartbeatGuardBaseline {
		t.Errorf("%s of the dead holder's row = %v (valid=%v), want the untouched baseline %v",
			taskLastToolcallAtColumn, v, ok, heartbeatGuardBaseline)
	}
	// ... while every writable row advanced to the probe stamp.
	for _, id := range []string{heartbeatGuardLiveRow, heartbeatGuardForeignRow, heartbeatGuardNoStampRow} {
		if v, ok := heartbeatGuardColumn(t, db, taskLastToolcallAtColumn, id); !ok || v != heartbeatGuardStamp {
			t.Errorf("%s of %s = %v (valid=%v), want %v", taskLastToolcallAtColumn, id, v, ok, heartbeatGuardStamp)
		}
	}
}

// TestHeartbeatGuardAppliesToBothLivenessColumns pins that the guard sits on
// the ONE shared write path (touch), not on one column: tasklist_issued_at is
// the second liveness fact and a survivor could keep a dead row fresh through
// it alone (the measured row 01a163df had BOTH advancing).
func TestHeartbeatGuardAppliesToBothLivenessColumns(t *testing.T) {
	heartbeatGuardFixture(t)

	h := OpenTaskHeartbeatConn(context.Background())
	if h == nil {
		t.Fatalf("heartbeat conn nil")
	}
	defer h.Close() //stylegate:ignore assertion handle opened inside the helper

	if err := h.ExecTouchTasklist(heartbeatGuardDeadRow, heartbeatGuardDeadRow, heartbeatGuardStamp); err == nil {
		t.Fatalf("tasklist_issued_at touch on a DEAD holder's row succeeded — the guard is not on the shared write path")
	}
	if err := h.ExecTouchTasklist(heartbeatGuardLiveRow, heartbeatGuardLiveRow, heartbeatGuardStamp); err != nil {
		t.Fatalf("tasklist_issued_at touch on a LIVE holder's row was refused: %v", err)
	}
}

// TestHeartbeatGuardFindsTheDeadHolderThroughTheSessionLeg pins the SECOND
// addressing leg of touch: a pre-consolidation row is addressed by its
// session_id column, and the guard must read the claim stamp from the row it
// actually updates — a guard that only inspected the task_id leg would leave
// the session-shaped rows (the auto-registered ones) unguarded.
func TestHeartbeatGuardFindsTheDeadHolderThroughTheSessionLeg(t *testing.T) {
	path := testutil.NewTestDBPath(t)

	// A pre-consolidation row: the session_id column carries the row key, so
	// the session-addressed leg is the one that reaches it. seedTaskRow leaves
	// session_id empty (its TaskNew call passes ""), so the fixture stamps it —
	// without that the UPDATE would match no row at all and the assertion below
	// would pass for the wrong reason.
	seedTaskRow(t, heartbeatGuardDeadRow, "[test] session-leg dead holder")
	db := openTestTasksDB(t, path)
	if _, err := db.Exec(
		`UPDATE tasks SET session_id = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+` = ?,
		 `+taskLastToolcallAtColumn+` = ? WHERE task_id = ?`,
		heartbeatGuardDeadRow, claimHolderOfPid(heartbeatGuardDeadPid(t)), heartbeatGuardBaseline, heartbeatGuardBaseline,
		heartbeatGuardBaseline, heartbeatGuardDeadRow); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	db.Close() //stylegate:ignore seeding handle closed before the touch opens its own

	h := OpenTaskHeartbeatConn(context.Background())
	if h == nil {
		t.Fatalf("heartbeat conn nil")
	}
	// taskID empty forces the session_id leg (the legacy addressing shape).
	if err := h.ExecTouchLastToolcall("", heartbeatGuardDeadRow, heartbeatGuardStamp); err == nil {
		t.Fatalf("heartbeat touch through the session_id leg stamped a DEAD holder's row")
	}
	h.Close() //stylegate:ignore assertion handle closed at the end of the test

	db2 := openTestTasksDB(t, path)
	defer db2.Close() //stylegate:ignore assertion handle closed at the end of the test
	if v, ok := heartbeatGuardColumn(t, db2, taskLastToolcallAtColumn, heartbeatGuardDeadRow); !ok || v != heartbeatGuardBaseline {
		t.Errorf("%s through the session leg = %v (valid=%v), want the untouched baseline %v",
			taskLastToolcallAtColumn, v, ok, heartbeatGuardBaseline)
	}
}

// TestHeartbeatGuardLeavesTerminalRowsSilent pins the guard's ORDER of
// questions: a terminal row is a SILENT no-op on the heartbeat path ("a
// finished task is not alive, its timestamps stand"), so the guard must not
// turn that into a reported refusal for a row nobody is trying to revive. The
// guard's read carries the same `status = in_progress` predicate as the UPDATE
// it protects; without it, every finished row still stamped with its (long
// dead) runner would emit an stderr error on every tool batch — a new noise
// channel on the healthy path, which is the fail-soft direction violated.
func TestHeartbeatGuardLeavesTerminalRowsSilent(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	seedTaskRow(t, heartbeatGuardDeadRow, "[test] finished row, dead runner stamp")

	db := openTestTasksDB(t, path)
	if _, err := db.Exec(
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+taskLastToolcallAtColumn+` = ?,
		 `+taskTasklistIssuedAtColumn+` = ? WHERE task_id = ?`,
		TaskStatusCompleted, claimHolderOfPid(heartbeatGuardDeadPid(t)), heartbeatGuardBaseline,
		heartbeatGuardBaseline, heartbeatGuardBaseline, heartbeatGuardDeadRow); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	db.Close() //stylegate:ignore seeding handle closed before the touch opens its own

	h := OpenTaskHeartbeatConn(context.Background())
	if h == nil {
		t.Fatalf("heartbeat conn nil")
	}
	defer h.Close() //stylegate:ignore assertion handle opened at the end of the test

	if err := h.ExecTouchLastToolcall(heartbeatGuardDeadRow, heartbeatGuardDeadRow, heartbeatGuardStamp); err != nil {
		t.Fatalf("heartbeat touch on a TERMINAL row was reported as %v — a finished row is a silent no-op, not a refusal", err)
	}
}
