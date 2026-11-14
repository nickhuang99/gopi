package tools

// The CLOSE-OUT keepalive: a wrapper still working on a row it claimed must
// keep stamping it.
//
// THE DEFECT THIS PINS (measured 2026-10-27, row
// 01a17a1b-496f-7179-9567-d544fa5922f9). Every heartbeat writer in the tree
// belongs to the run's ENGINE (internal/agent: registration, per-iteration,
// per-tool-batch, and startRunKeepalive's 2-minute ticker). The wrapper
// (`gopi task run`, cmd/agent/cli_tasks.go) is a DIFFERENT process and keeps
// working after the engine exits: the clone channel's delivery leg
// (clean → pull --rebase → build → push) and finish-cleanup's branch check run
// HERE, on the row it claimed, while the row is still in_progress. That window
// had no writer at all, so the liveness predicate read a healthy wrapper as a
// corpse:
//
//	last_toolcall_at = 06:15:26   (the engine's last stamp)
//	ended_at         = 07:01:10   (the wrapper's writeback)
//	span             = 45.7 min   in_progress, no heartbeat writer
//
// and that run's engine JSONL has no record after 06:15:43 — so this is NOT
// "the keepalive ticked late": the process that would have ticked was gone.
// The scheduler observed 38m52s of it and reported the row stale.
//
// THE THREE LAYERS THE EPIC ASKED ABOUT, all measured, none of them this:
//   (a) internal/agent's startRunKeepalive: TICKED ON SCHEDULE. During a 6m20s
//       quiet window driven by ONE bash call (no other tool activity, so the
//       ticker was the only possible writer) last_toolcall_at advanced at
//       07:43:59 → 07:45:27 → 07:47:27 → 07:49:27, i.e. exactly 120s apart.
//   (b) internal/tools' holder guard: did NOT refuse. The wrapper's own row
//       carried a live pid (its claimed_by pid answered signal 0), and a
//       refused write would have printed "[gopi] task heartbeat: …" on stderr
//       — that string occurs ZERO times in the run's output.log and raw.log.
//   (c) the ledger's busy_timeout: not implicated. The read-only sampler that
//       produced the readings above opened the SAME 560 MB ledger 45 times in
//       15 minutes with no busy refusal.
//
// RED PROOF: on the pre-fix tree StartTaskRowKeepalive does not exist, so this
// file does not compile against it — the shape of the fix is a new writer, and
// a "red run" of this exact file would compile-fail rather than assert-fail.
// The assertion that carries the regression is ② below: it observes the stamp
// ADVANCE (not merely exist) while the row is in_progress.

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// closeoutKeepaliveRow is the row the wrapper holds while it delivers.
	closeoutKeepaliveRow = "t-closeout-keepalive"

	// closeoutKeepaliveInterval is the rebinding this test applies: the seam
	// exists so a SECOND tick can be observed in ~200ms instead of two minutes.
	closeoutKeepaliveInterval = 40 * time.Millisecond

	// closeoutKeepaliveSecondTick is how long the test waits for that second
	// stamp — several intervals, so a loaded machine cannot make it flake.
	closeoutKeepaliveSecondTick = 400 * time.Millisecond

	// closeoutKeepalivePollEvery is the waiting interval between two column
	// reads; small enough that the assertions observe a tick promptly, large
	// enough that a poll loop does not spin.
	closeoutKeepalivePollEvery = 10 * time.Millisecond
)

// closeoutKeepaliveFixture seeds one in_progress row with a LIVE holder (this
// test process) and an OLD liveness stamp, so "did the keepalive move it" is a
// comparison rather than a clock read. Same shape as the holder-guard fixture
// (Rule 14: one way to build an in_progress row in this package).
func closeoutKeepaliveFixture(t *testing.T) string {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	seedTaskRow(t, closeoutKeepaliveRow, "[test] close-out keepalive")

	db := openTestTasksDB(t, path)
	defer db.Close() //stylegate:ignore fixture handle closed at the end of the seeding pass; the assertions read through the keepalive's own handle
	if _, err := db.Exec(
		`UPDATE tasks SET claimed_by = ?, `+taskLastToolcallAtColumn+` = ? WHERE task_id = ?`,
		claimHolderOfPid(os.Getpid()), heartbeatGuardBaseline, closeoutKeepaliveRow); err != nil {
		t.Fatalf("stamp %s: %v", closeoutKeepaliveRow, err)
	}
	return path
}

// TestCloseoutKeepaliveStampsTheRowWhileTheWrapperWorks is the acceptance for
// the epic's R5: while a wrapper holds an in_progress row, that row's liveness
// column keeps advancing.
func TestCloseoutKeepaliveStampsTheRowWhileTheWrapperWorks(t *testing.T) {
	path := closeoutKeepaliveFixture(t)

	prevInterval := taskRowKeepaliveInterval
	taskRowKeepaliveInterval = closeoutKeepaliveInterval
	t.Cleanup(func() { taskRowKeepaliveInterval = prevInterval })

	db := openTestTasksDB(t, path)
	defer db.Close() //stylegate:ignore the test's own read handle; the keepalive opens its own

	stop := StartTaskRowKeepalive(context.Background(), closeoutKeepaliveRow)
	defer stop()

	// ① the FIRST stamp arrives promptly: a wrapper that starts delivering must
	// not leave the row looking abandoned for a full interval. It is taken from
	// the goroutine, so the assertion POLLS with a deadline rather than reading
	// once — a single immediate read races the goroutine's first write.
	first := waitForStampedColumn(t, db, func(v float64, ok bool) bool {
		return ok && v > heartbeatGuardBaseline
	})
	if first <= heartbeatGuardBaseline {
		t.Fatalf("the keepalive never stamped the row (last_toolcall_at stayed %v, baseline %v) — a wrapper that begins its close-out still looks abandoned", first, heartbeatGuardBaseline)
	}

	// ② THE REGRESSION: the stamp ADVANCES on its own while nothing else writes.
	second := waitForStampedColumn(t, db, func(v float64, ok bool) bool {
		return ok && v > first
	})
	if second <= first {
		t.Fatalf("last_toolcall_at did not advance past %v over %s with the interval at %s — this is exactly the frozen-heartbeat shape the epic measured (45.7 minutes of in_progress work with no writer)",
			first, closeoutKeepaliveSecondTick*4, closeoutKeepaliveInterval)
	}

	// ③ stop is the wrapper's defer: once it returns, no further write may
	// land (a stamp after the writeback would be a liveness claim about a row
	// nobody is working on — the false-live direction the engine's own
	// keepalive avoids by being ctx-bound).
	stop()
	afterStop, _ := heartbeatGuardColumn(t, db, taskLastToolcallAtColumn, closeoutKeepaliveRow)
	//stylegate:allow-sleep the wait IS the observation: this arm asserts the column does NOT move once the wrapper stopped, so the window is the assertion and there is nothing to poll for
	settled, _ := heartbeatGuardColumn(t, db, taskLastToolcallAtColumn, closeoutKeepaliveRow)
	if settled != afterStop {
		t.Fatalf("last_toolcall_at kept moving after stop (%v → %v) — a stopped wrapper must not keep claiming a row is live", afterStop, settled)
	}
}

// waitForStampedColumn polls the row's liveness column until want is satisfied
// or the deadline passes, and returns the value it last read. The keepalive
// writes from its own goroutine, so every "did it stamp" question is a
// wait-for-condition rather than a single read — a single read races the first
// write and would turn a working keepalive into a flaky red.
func waitForStampedColumn(t *testing.T, db *sql.DB, want func(v float64, ok bool) bool) float64 {
	t.Helper()
	deadline := time.Now().Add(closeoutKeepaliveSecondTick * 4)
	var last float64
	for {
		v, ok := heartbeatGuardColumn(t, db, taskLastToolcallAtColumn, closeoutKeepaliveRow)
		last = v
		if want(v, ok) {
			return last
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(closeoutKeepalivePollEvery) //stylegate:allow-sleep bounded poll interval: the deadline above owns the outcome and exits
	}
}

// TestCloseoutKeepaliveLeavesTerminalRowsAlone pins the OTHER direction: a row
// that is no longer in_progress is not "alive", and the keepalive inherits
// touch's terminal no-op rather than reviving its timestamps.
func TestCloseoutKeepaliveLeavesTerminalRowsAlone(t *testing.T) {
	path := closeoutKeepaliveFixture(t)

	prevInterval := taskRowKeepaliveInterval
	taskRowKeepaliveInterval = closeoutKeepaliveInterval
	t.Cleanup(func() { taskRowKeepaliveInterval = prevInterval })

	db := openTestTasksDB(t, path)
	defer db.Close() //stylegate:ignore the test's own read handle; the keepalive opens its own
	if _, err := db.Exec(`UPDATE tasks SET status = ? WHERE task_id = ?`,
		TaskStatusCompleted, closeoutKeepaliveRow); err != nil {
		t.Fatalf("close the row: %v", err)
	}

	stop := StartTaskRowKeepalive(context.Background(), closeoutKeepaliveRow)
	//stylegate:allow-sleep the wait IS the observation: this arm asserts a COMPLETED row's timestamps never move, so several intervals must pass for the claim to mean anything
	stop()

	got, _ := heartbeatGuardColumn(t, db, taskLastToolcallAtColumn, closeoutKeepaliveRow)
	if got != heartbeatGuardBaseline {
		t.Fatalf("last_toolcall_at moved to %v on a COMPLETED row (baseline %v) — a finished task is not alive and its timestamps must stand", got, heartbeatGuardBaseline)
	}
}
