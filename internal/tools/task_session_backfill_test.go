package tools

// THE DEFERRED session_id STAMP, PINNED AS A TEST (row
// 01a16e1d-e3ef-7c5a-88aa-e68235be8078, 2026-10-25).
//
// The defect: cmd/agent's runTaskRunOnce polls for the child's session JSONL
// for taskSessionPollMax and then gives up quietly on a miss. Its comment says
// the writeback's deferred backfill is the fallback — and the writeback only
// ever backfilled the MODEL. So a run whose session record appeared after the
// poll window kept an EMPTY session_id for its whole life. Measured on the live
// ledger (read-only, 8-day window): 39 rows whose own run directory holds a
// session record while the column is empty, two of them terminal `completed`
// days earlier — every session-keyed join severed.
//
// WHAT THESE ARMS DRIVE: the REAL write path (TaskUpdateWithResult → taskUpdate
// → the state gate), on a scratch ledger, with the row's own run directory
// holding a real session record. Nothing here calls the backfill directly: a
// test that calls the seam proves the seam, not that the door reaches it.
//
// RED WITHOUT THE FIX: the first arm asserts the column after a terminal write
// — with the deferred stamp removed it stays empty and the arm fails with the
// column's value in the message.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// tsbSeedSessionRecord writes a task row's OWN run record where a real run
// leaves it — `sessions/<task-id>/<session-id>.jsonl` under the profile — with
// the session_start event as its first line (the writer syncs that row before
// any other, so it is always first). It returns the directory it created so an
// arm can prove the placement is what the resolver reads.
func tsbSeedSessionRecord(t *testing.T, taskID, sessionID, model string) string {
	t.Helper()
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("resolve the profile dir: %v", err)
	}
	dir := filepath.Join(gd, config.SessionsDirName, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the run directory %s: %v", dir, err)
	}
	line := `{"type":"session_start","sessionId":"` + sessionID + `","model":"` + model + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte(line), 0o644); err != nil {
		t.Fatalf("write the session record: %v", err)
	}
	return dir
}

// tsbRowSession reads one row's session_id back (COALESCE, because the column
// is NULL on a row that was never stamped — the two "empty" shapes must not be
// distinguishable to an assertion that is about the VALUE).
func tsbRowSession(t *testing.T, dbPath, taskID string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var sid string
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(session_id,'') FROM tasks WHERE task_id = ?`, taskID).Scan(&sid); err != nil {
		t.Fatalf("read session_id for %s: %v", taskID, err)
	}
	return sid
}

// TestDeferredSessionStampFillsTheRowFromItsOwnRunDirectory is the POSITIVE
// arm: a terminal writeback on a row whose session_id is empty must fill it
// from the row's own run record — the repair the writeback's own comment has
// promised since bug054 and did not perform.
func TestDeferredSessionStampFillsTheRowFromItsOwnRunDirectory(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	t.Setenv(config.GopiDirEnv, t.TempDir())

	const id = "tsb-stamped"
	const sid = "0f1e2d3c-4b5a-4968-8776-655443322110"
	dir := tsbSeedSessionRecord(t, id, sid, "m-test")

	dpSeedRow(t, db, id, "the row whose session the writeback must stamp", taskStatusInProgress)
	if got := tsbRowSession(t, db, id); got != "" {
		t.Fatalf("fixture precondition: row %s already carries session_id %q — the arm would prove nothing", id, got)
	}

	if _, err := TaskUpdateWithResult(id, taskStatusFailed, "work recorded"); err != nil {
		t.Fatalf("terminal writeback: %v", err)
	}
	if got := tsbRowSession(t, db, id); got != sid {
		t.Fatalf("session_id after the writeback = %q, want %q — the deferred stamp did not reach the row "+
			"(the writeback's own comment claims a missed early poll is repaired here). "+
			"The run record is present at %s", got, sid, dir)
	}
}

// TestDeferredSessionStampNeverOverwritesAStampedRow is the REVERSE guard: the
// column is authoritative once it holds a value — a re-writeback (a retry, a
// verification, a later door) must not move it to a different session. Without
// this arm the fix would be indistinguishable from one that simply assigns.
func TestDeferredSessionStampNeverOverwritesAStampedRow(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	t.Setenv(config.GopiDirEnv, t.TempDir())

	const id = "tsb-kept"
	const own = "11111111-2222-4333-8444-555555555555"
	const other = "99999999-8888-4777-8666-555555555555"
	tsbSeedSessionRecord(t, id, other, "m-other")

	dpSeedRow(t, db, id, "the row that already knows its session", taskStatusInProgress)
	if err := TaskBackfillSession(id, own); err != nil {
		t.Fatalf("pre-stamp the row's own session: %v", err)
	}

	if _, err := TaskUpdateWithResult(id, taskStatusFailed, "work recorded"); err != nil {
		t.Fatalf("terminal writeback: %v", err)
	}
	if got := tsbRowSession(t, db, id); got != own {
		t.Fatalf("session_id = %q after the writeback, want the pre-existing %q — the deferred stamp "+
			"overwrote a column that was already authoritative", got, own)
	}
}

// TestDeferredSessionStampIsSilentWithoutARunRecord is the FAIL-SOFT arm: a row
// with no run directory (a human-typed row, or one whose directory was swept)
// must still close out, and must close out with the column untouched. A repair
// that refuses a writeback because it cannot find a log would turn a cosmetic
// gap into a stuck row — the exact failure class this whole row was filed for.
func TestDeferredSessionStampIsSilentWithoutARunRecord(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	t.Setenv(config.GopiDirEnv, t.TempDir())

	const id = "tsb-absent"
	dpSeedRow(t, db, id, "a row that never ran a child", taskStatusInProgress)

	if _, err := TaskUpdateWithResult(id, taskStatusFailed, "work recorded"); err != nil {
		t.Fatalf("the writeback must not fail for want of a session log, got: %v", err)
	}
	if got := tsbRowSession(t, db, id); got != "" {
		t.Fatalf("session_id = %q with no run record anywhere — the stamp guessed", got)
	}
}

// TestDeferredSessionStampReachesEveryDoor is the REACH arm, and it is the one
// that forced the repair to live in the shared state gate rather than in one
// wrapper. `gopi task update <id> --status …` routes through TaskUpdateReason,
// which calls taskUpdate DIRECTLY — it never passes TaskUpdateWithResult — and
// so does the merger's executor write. A stamp installed in the run writeback
// alone leaves those doors (and the operator's own CLI) with the empty column.
func TestDeferredSessionStampReachesEveryDoor(t *testing.T) {
	for _, door := range []struct {
		name string
		call func(id, status string) error
	}{
		{"task update --status (TaskUpdateReason)", func(id, status string) error {
			_, err := TaskUpdateReason(id, status, "operator move", "")
			return err
		}},
		{"status-only (TaskUpdate)", func(id, status string) error {
			_, err := TaskUpdate(id, status)
			return err
		}},
		{"verify writeback (TaskUpdateWithResult)", func(id, status string) error {
			_, err := TaskUpdateWithResult(id, status, "verdict evidence")
			return err
		}},
	} {
		t.Run(door.name, func(t *testing.T) {
			db := testutil.NewTestDBPath(t)
			t.Setenv(config.GopiDirEnv, t.TempDir())
			id := "tsb-door-" + strings.Fields(door.name)[0]
			sid := "beefbeef-1111-4222-8333-444444444444"
			tsbSeedSessionRecord(t, id, sid, "m-door")
			dpSeedRow(t, db, id, "a row written through one door only", taskStatusInProgress)

			if err := door.call(id, taskStatusFailed); err != nil {
				t.Fatalf("writeback through %s: %v", door.name, err)
			}
			if got := tsbRowSession(t, db, id); got != sid {
				t.Fatalf("session_id = %q after the %s door, want %q — this door bypasses the run "+
					"writeback wrapper, so the stamp must sit in the shared state gate", got, door.name, sid)
			}
		})
	}
}
