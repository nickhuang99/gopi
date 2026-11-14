package main

// The status field `task show` renders is NOT one reading: the status WORD plus
// the render-time MEASURED holder facts chained after it (internal/tools/
// taskholder_gone.go). taskStatusOf answers the WORD, and this file pins that
// at the helper itself because six test files compare its answer against a
// status VALUE.
//
// THE DEFECT IT GUARDS (measured 2026-11-05, task row
// 01a1a8c6-9a4e-76b4-8687-a09f65fba456). TestWrapperSilentFalseSuccessOnRefusedWriteback_RED
// went red on its third assertion — "the escalation must name what the ledger
// holds instead" — and the failure looked load-sensitive because it only
// appeared in whole-package runs. It was neither: the isolated run is red in
// ~0.9 s. The wrapper's escalation names the LEDGER's own answer
// (tools.TaskRowStatusAt → the bare word) while taskStatusOf returned the whole
// field, marker included, so the assertion compared
//
//	strings.Contains(stderr, "in_progress + holder dead (pid N gone)")
//
// against a line that says `in_progress` — false by construction. The marker
// arrived in 506b1742 / 1f381c39 (2026-11-05 05:50 / 06:11), AFTER the row this
// test was written for, and a test that measures the marker logic as a side
// effect of its own status assertion is what that commit made possible.

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// statusWordTestDeadPid returns a pid number this host can prove is gone, so
// the fixture renders the marker rather than sitting on an unanswerable probe
// (Rule 15: an unanswerable probe marks nothing).
func statusWordTestDeadPid(t *testing.T) int {
	t.Helper()
	for pid := 1 << 30; pid < (1<<30)+64; pid++ {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err != nil {
			return pid
		}
	}
	t.Fatalf("no free pid number above %d — cannot build the dead-holder fixture", 1<<30)
	return 0
}

// statusWordTestHolder spells the claim stamp the way the worker does
// (`<host>:<pid>`), so claimHolder resolves it as a LOCAL stamp and the pid
// layer is the one that votes.
func statusWordTestHolder(t *testing.T, pid int) string {
	t.Helper()
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}
	return host + ":" + strconv.Itoa(pid)
}

// TestTaskStatusOfAnswersTheWordNotTheHolderMarker pins the split at the HELPER:
// a row whose executor is provably gone renders `in_progress + holder dead
// (pid N gone)`, and taskStatusOf must answer `in_progress` — the same value
// the ledger holds and therefore the same value the wrapper's escalation can
// name.
//
// Both halves are asserted on purpose. Without the marker assertion the test
// would pass vacuously if the renderer ever stopped emitting it (the failure
// that would come back is the one this file exists to prevent); without the
// word assertion it would not test the helper at all.
func TestTaskStatusOfAnswersTheWordNotTheHolderMarker(t *testing.T) {
	testutil.NewTestDBPath(t)
	deadPid := statusWordTestDeadPid(t)
	const id = "01a11500-0000-7000-8000-0000000000d1"

	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "status-word fixture",
		Payload: "the row whose holder is provably gone",
		TaskID:  id,
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed submit %s: %v", id, err)
	}

	// Claim it with a DEAD pid, freshly stamped: `task show` measures the pid
	// at render time, and a stamp that predates the row's own start would be
	// read as a PREVIOUS claimant's (taskzombie.go's claimStamp* axis).
	db, err := openRawTasksDB(memoryDBPathForTest(t))
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	now := float64(time.Now().Unix())
	if _, err := db.Exec(`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?,
		created_at = ?, updated_at = ?, in_progress_at = ? WHERE task_id = ?`,
		tools.TaskStatusInProgress, statusWordTestHolder(t, deadPid),
		now, now, now, now, id); err != nil {
		db.Close() //stylegate:ignore the test is already failing and cannot use the handle
		t.Fatalf("stamp dead holder on %s: %v", id, err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	// ① the RAW field really carries the measurement — otherwise ② proves
	// nothing about the split.
	raw := rawStatusField(t, id)
	if want := " + holder dead (pid " + strconv.Itoa(deadPid); !strings.Contains(raw, want) {
		t.Fatalf("the fixture did not render the holder-gone marker (want a field containing %q): %q\n"+
			"the split below would be untested — fix the fixture, not the helper", want, raw)
	}
	// ② the helper answers the WORD, which is what the ledger holds and what an
	//    escalation line can name.
	if got := taskStatusOf(t, id); got != tools.TaskStatusInProgress {
		t.Fatalf("taskStatusOf() = %q, want %q — the helper is returning the rendered field "+
			"(status word + measured marker) instead of the status word; a caller comparing it "+
			"against a ledger status value then goes red for a MEASUREMENT it never asked about.\n"+
			"raw status field: %q", got, tools.TaskStatusInProgress, raw)
	}
}

// rawStatusField returns the `status:` line of `task show` VERBATIM — the
// unsplit reading, so a test can prove the marker was there.
func rawStatusField(t *testing.T, taskID string) string {
	t.Helper()
	show, err := tools.TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow %s: %v", taskID, err)
	}
	for _, ln := range strings.Split(show, "\n") {
		if strings.HasPrefix(ln, "status:") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "status:"))
		}
	}
	t.Fatalf("`task show %s` printed no status line:\n%s", taskID, show)
	return ""
}
