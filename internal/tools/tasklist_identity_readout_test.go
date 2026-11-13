package tools

// The LIST-vs-SHOW agreement over one ledger row (2026-11-05).
//
// MEASURED DEFECT. A row that reached in_progress WITHOUT a `claimed_by` stamp
// — a status write, a legacy row, a claim whose stamp was later cleared —
// still records WHO runs it in the process-identity columns (`proc_pid`,
// `proc_start_ticks`, `proc_cmd`), which are written by the same transition.
// `gopi task show` read those columns and rendered `(live: gone)`, while
// `gopi task list` rendered `[unknown: no claim stamp]` for the SAME row in
// the SAME second: the list probed a pid ONLY when the claim STRING parsed,
// so the ledger held the answer and the display refused to read it.
//
// The two arms below are the property, and they are deliberately stated as a
// PAIR: a fix that reads the identity layer but reports everything dead would
// pass the first arm and destroy the ledger, so the live arm runs beside it.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// taskListIdentityRow clears the claim STRING from a row that carries a
// recorded process identity — the exact shape the defect was measured on.
// Clearing is honest: the stamp is absent in the incidents (a status write
// mints no claim), and re-deriving one would test a different row.
func taskListIdentityRow(t *testing.T, taskID string) {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	row, err := taskByID(ctx, db, taskID)
	if err != nil || row == nil {
		t.Fatalf("re-read %s: %v", taskID, err)
	}
	if !row.HasProcIdentity || row.ProcPid <= 0 {
		t.Fatalf("fixture precondition: %s must record a process identity (the columns the fix reads)", taskID)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET claimed_by = '', claim_stamp = NULL WHERE task_id = ?`, taskID); err != nil {
		// claim_stamp may not exist on every schema rev; the STRING is what
		// the display gates on, so clearing it is what the arm needs.
		if _, err2 := db.ExecContext(ctx, `UPDATE tasks SET claimed_by = '' WHERE task_id = ?`, taskID); err2 != nil {
			t.Fatalf("clear the claim stamp on %s: %v (then: %v)", taskID, err, err2)
		}
	}
	again, err := taskByID(ctx, db, taskID)
	if err != nil || again == nil {
		t.Fatalf("re-read %s after clearing: %v", taskID, err)
	}
	if strings.TrimSpace(again.ClaimedBy) != "" {
		t.Fatalf("fixture broken: %s still carries a claim stamp %q", taskID, again.ClaimedBy)
	}
}

// TestTaskListReadsTheIdentityLayerWhenTheClaimStampIsAbsent is the red-proof:
// with a DEAD pid recorded and no claim string, the list's state ladder must
// reach the same "died" verdict `task show` already reaches.
func TestTaskListReadsTheIdentityLayerWhenTheClaimStampIsAbsent(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "t-list-ident-dead"
	procIdentitySeedRow(t, taskID, hostPid(t, os.Getpid()))
	taskListIdentityRow(t, taskID)

	dead := deadPID(t)
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	// The pid is DEAD and the recorded tick is a deliberate non-match, which
	// is what a gone process looks like from this side (there is no /proc
	// entry left to compare against — the ladder's first rung already says
	// gone; the tick is what makes the row honest about not knowing more).
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET `+tasksProcPidColumnName+` = ?, `+tasksProcStartColumnName+` = ? WHERE task_id = ?`,
		dead, int64(1), taskID); err != nil {
		t.Fatalf("record a dead identity: %v", err)
	}
	db.Close() //stylegate:ignore fixture handle; the reader below opens its own

	got := taskInProgressStateNow(taskRowByID(t, taskID), time.Now())
	if !strings.HasPrefix(got, taskStateDied) {
		t.Fatalf("DEFECT: `task list` still refuses to read the identity columns — state = %q, "+
			"want %q. A row whose recorded holder is gone must not render as an unknowable "+
			"absence while `task show` resolves the same columns to \"gone\".", got, taskStateDied)
	}
}

// TestTaskListKeepsALiveIdentityHolderRunning is the false-positive guard: the
// SAME claimless shape, recorded against a pid that IS this process, must read
// as running. Without it, "read the identity layer" could be satisfied by
// calling every claimless row dead — which would be worse than the defect.
func TestTaskListKeepsALiveIdentityHolderRunning(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "t-list-ident-live"
	procIdentitySeedRow(t, taskID, hostPid(t, os.Getpid()))
	taskListIdentityRow(t, taskID)

	row := taskRowByID(t, taskID)
	if row.ProcPid != os.Getpid() {
		t.Fatalf("fixture precondition: the row must record THIS process (%d), got %d", os.Getpid(), row.ProcPid)
	}
	if ticks, err := strconv.ParseInt(strconv.FormatInt(row.ProcStartTicks, 10), 10, 64); err != nil || ticks <= 0 {
		t.Fatalf("fixture precondition: the row must record a real start tick, got %d", row.ProcStartTicks)
	}

	got := taskInProgressStateNow(row, time.Now())
	if !strings.HasPrefix(got, taskStateRunning) {
		t.Fatalf("a claimless row whose recorded holder is ALIVE must read as %q, got %q — "+
			"reporting it dead would destroy a running row's ledger entry", taskStateRunning, got)
	}
}

// TestTaskListIdentityReadingAgreesWithTaskShow pins the AGREEMENT itself, not
// just each side: the two faces must derive the holder word from the ONE
// verdict. The assertion is deliberately phrased over both faces' public
// rendering so a future refactor cannot satisfy one and drift the other.
func TestTaskListIdentityReadingAgreesWithTaskShow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "t-list-ident-agree"
	procIdentitySeedRow(t, taskID, hostPid(t, os.Getpid()))
	taskListIdentityRow(t, taskID)

	row := taskRowByID(t, taskID)
	// show side: the ONE ladder verdict, mapped to its display word.
	showWord := taskShowLiveWord(taskRowProcessLiveness(row).State)
	// list side: the same verdict, read through the state ladder.
	listState := taskInProgressStateNow(row, time.Now())

	if showWord != taskShowLiveAlive {
		t.Fatalf("fixture precondition: the recorded holder is this live process, show says %q", showWord)
	}
	if !strings.HasPrefix(listState, taskStateRunning) {
		t.Fatalf("the two faces disagree about one row: task show says %q while task list says %q "+
			"(they must both come from taskRowProcessLiveness — Rule 14)", showWord, listState)
	}
}
