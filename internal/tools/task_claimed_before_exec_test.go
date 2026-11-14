package tools

import (
	"context"
	"os"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestClaimedThenInProgressIsWrittenOnlyAfterAChildExists pins the STATE
// MEANING this epic establishes (operator ruling 2026-11-05, row 01a1a87b):
//
//	claimed      ownership decided; whether a child exists is NOT claimed
//	in_progress  a child process was exec'd successfully; its pid is recorded
//
// Both arms run on the Rule 32 scaffold so neither can touch the operator's
// ledger, and both drive the REAL production writers
// (TaskClaimExplicitForce / TaskRequireClaimed / TaskStartChildAdmitted /
// TaskStampStartFailure) rather than a hand-rolled UPDATE — a test that wrote
// the columns itself would prove nothing about the code that does.
func TestClaimedThenInProgressIsWrittenOnlyAfterAChildExists(t *testing.T) {
	testutil.NewTestDBPath(t)

	// Arm 1 — a run that never reaches exec stays `claimed`, and says why.
	id := submitFixture(t, "claimed-before-exec arm 1 (exec never happened)")
	claimFixture(t, id)
	if got := rowStatusOf(t, id); got != TaskStatusClaimed {
		t.Fatalf("after the claim the row must read %q (ownership decided, no child yet), got %q", TaskStatusClaimed, got)
	}
	// The launch seam's assertion admits the claim it just won.
	if _, err := TaskRequireClaimed(id, false); err != nil {
		t.Fatalf("the launch-seam assertion refused the row this runner just claimed: %v", err)
	}
	// The exec fails: the failure branch records WHY and does NOT advance.
	if err := TaskStampStartFailure(id, "start child: exec: \"gopi\": permission denied"); err != nil {
		t.Fatalf("start-failure record: %v", err)
	}
	if got := rowStatusOf(t, id); got != TaskStatusClaimed {
		t.Fatalf("a failed exec must leave the row at %q, got %q — an in_progress row with no child is the defect this epic removes",
			TaskStatusClaimed, got)
	}
	if detail := eventDetail(t, id, TaskStartFailureEvent); detail == "" {
		t.Fatalf("the failed exec left no %q record on the row's trail — a claimed row with no cause is the silent-failure shape",
			TaskStartFailureEvent)
	}

	// Arm 2 — the success path advances, and the recorded pid is the CHILD's.
	id2 := submitFixture(t, "claimed-before-exec arm 2 (a child was exec'd)")
	claimFixture(t, id2)
	// The CHILD here is this very test process standing in for a spawned one:
	// the writer's contract is "record the pid the caller exec'd", so any live
	// pid proves the wiring. Using os.Getpid() also lets the row's identity be
	// verified through /proc, which the assertion below does.
	childPid := os.Getpid()
	if _, err := TaskStartChildAdmitted(id2, "fixture-host", childPid, false); err != nil {
		t.Fatalf("start (child admitted): %v", err)
	}
	row := rowByID(t, id2)
	if row.Status != TaskStatusInProgress {
		t.Fatalf("after a successful exec the row must read %q, got %q", TaskStatusInProgress, row.Status)
	}
	if !row.HasProcIdentity || row.ProcPid != childPid {
		t.Fatalf("the row must record the CHILD's process identity (pid %d), got has=%v pid=%d cmd=%q",
			childPid, row.HasProcIdentity, row.ProcPid, row.ProcCmd)
	}
	// And that identity is MEASURABLE — the whole point of recording it: the
	// ladder must answer "running" about the pid the row names, not "unknown".
	if v := taskRowProcessLiveness(row); v.State != TaskProcessStateRunning {
		t.Fatalf("the recorded child identity does not verify against /proc: state=%q reason=%s", v.State, v.Reason)
	}
}

// TestRequireClaimedRefusesAnotherRunner pins the gate the moved transition had
// to keep: the old claimed→in_progress CAS doubled as "exactly one of two
// racing runners proceeds". That property now lives in the claim plus this
// assertion, so it is tested through the same door a runner uses.
func TestRequireClaimedRefusesAnotherRunner(t *testing.T) {
	testutil.NewTestDBPath(t)
	id := submitFixture(t, "launch-seam assertion")
	// pending: not a claim anyone won yet.
	if _, err := TaskRequireClaimed(id, false); err == nil {
		t.Fatalf("a pending row must not pass the launch-seam assertion — nothing has been claimed")
	}
	claimFixture(t, id)
	if _, err := TaskRequireClaimed(id, false); err != nil {
		t.Fatalf("a claimed row must pass: %v", err)
	}
	// A row that moved on without this runner (exec'd elsewhere) is refused.
	if _, err := TaskStartChildAdmitted(id, "other-host", os.Getpid(), false); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := TaskRequireClaimed(id, false); err == nil {
		t.Fatalf("a row already %q must refuse a second launcher — that is the double-run gate", TaskStatusInProgress)
	}
	if _, err := TaskRequireClaimed(id, true); err != nil {
		t.Fatalf("--takeover must relax the assertion (the operator's own door): %v", err)
	}
}

// TestLockPhaseIsTheNonStatusLockSignal pins deliverable B: the claim-lock
// protocol's progress is carried by its own field, it is silent by default (the
// single-node shape writes nothing), and an unknown value is REFUSED rather than
// stored (Rule 15).
func TestLockPhaseIsTheNonStatusLockSignal(t *testing.T) {
	testutil.NewTestDBPath(t)
	id := submitFixture(t, "lock phase field")
	claimFixture(t, id)
	// THE SINGLE-NODE SHAPE: no protocol ran, so nothing is recorded and
	// `task show` prints no lock line at all.
	if got := rowByID(t, id).LockPhase; got != "" {
		t.Fatalf("a row that never ran the lock protocol must carry no lock phase, got %q", got)
	}
	if err := TaskStampLockPhase(id, ""); err != nil {
		t.Fatalf("stamping the empty phase must be a no-op, not an error: %v", err)
	}
	if err := TaskStampLockPhase(id, "half"); err == nil {
		t.Fatalf("an unknown lock phase must be REFUSED, not stored")
	}
	if err := TaskStampLockPhase(id, TaskLockPhaseLocal); err != nil {
		t.Fatalf("stamp local: %v", err)
	}
	// Phase-1 only: still claimed, and the lock field says why.
	row := rowByID(t, id)
	if row.Status != TaskStatusClaimed || row.LockPhase != TaskLockPhaseLocal {
		t.Fatalf("after Phase-1 the row must read %q + lock_phase=%q, got %q + %q",
			TaskStatusClaimed, TaskLockPhaseLocal, row.Status, row.LockPhase)
	}
	if err := TaskStampLockPhase(id, TaskLockPhaseBoth); err != nil {
		t.Fatalf("stamp both: %v", err)
	}
	if got := rowByID(t, id).LockPhase; got != TaskLockPhaseBoth {
		t.Fatalf("双锁齐 must be recorded on the lock field, got %q", got)
	}
	// A yield gives the lock up: the field must not survive as a stale claim.
	if err := TaskReleaseLockPhase(id); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := rowByID(t, id).LockPhase; got != "" {
		t.Fatalf("a released lock must leave no phase behind, got %q", got)
	}
}

// submitFixture files a minimal dispatchable row through the real submit door.
func submitFixture(t *testing.T, summary string) string {
	t.Helper()
	// TaskSubmit returns the human-facing CONFIRMATION, not the id — the id comes
	// from TaskSubmitReturningID's SECOND return (the door the auto-enqueue uses).
	_, id, err := TaskSubmitReturningID(TaskSubmitOptions{Summary: summary, Payload: "fixture", Source: "human"})
	if err != nil {
		t.Fatalf("submit %q: %v", summary, err)
	}
	if id == "" {
		t.Fatalf("submit %q returned no row id", summary)
	}
	return id
}

// claimFixture wins the pending→claimed CAS as a foreign host, so the row is not
// this process's own claim (the writers under test are then exercised exactly as
// a runner's would be).
func claimFixture(t *testing.T, id string) {
	t.Helper()
	if err := TaskClaimExplicitForce(id, "fixture-host", true); err != nil {
		t.Fatalf("claim %s: %v", id, err)
	}
}

// rowByID reads a row through the production projection (the same reader
// `task show` uses), so an assertion rides the columns an operator sees rather
// than a private SELECT.
func rowByID(t *testing.T, id string) *Task {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	row, err := taskByID(ctx, db, id)
	if err != nil || row == nil {
		t.Fatalf("read row %s: %v", id, err)
	}
	return row
}

// rowStatusOf is rowByID's status-only face.
func rowStatusOf(t *testing.T, id string) string {
	t.Helper()
	return rowByID(t, id).Status
}

// eventDetail reads the newest detail recorded under one event name on a row's
// append-only trail ("" when the row carries no such event).
func eventDetail(t *testing.T, id, event string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var detail string
	err = db.QueryRowContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id DESC LIMIT 1`,
		id, event).Scan(&detail)
	if err != nil {
		return ""
	}
	return detail
}
