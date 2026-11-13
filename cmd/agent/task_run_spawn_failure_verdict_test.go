package main

// task_run_spawn_failure_verdict_test.go — the regression guard for the NINE
// HOURS a dead row spent as `claimed` (row 01a1b5e2-ca5e, measured 2026-11-07).
//
// THE DEFECT. `taskRunSpawnChild` forks/execs the run's child. When the exec
// failed it wrote `child spawn FAILED … no child process ever existed` into the
// run log and a `start-failed` event onto the row's trail — and then
// `runTaskRunOnce` returned the error and the process vanished. The row's
// STATUS never moved, so:
//
//	① `task status` / `task list` kept offering a row that had no runner, no
//	   process and no chance of ever moving — and a dispatcher that trusts the
//	   status column would have waited on it forever;
//	② because no status TRANSITION happened, `notifyTaskTransition` was never
//	   reached, so the `failed` event the notify chain exists to report was
//	   never emitted. The worker KNEW the answer and never said it on the
//	   channel anyone reads.
//
// THE FIX is one write by the funnel that owns the verdict: `claimed → failed`
// (a legal edge of taskAllowedTransitions, added by epic 01a1a87b for exactly
// this "failed before exec" case), carrying the cause as the row's reason, and
// routed through the SAME writeback seam every other failure arm in
// runTaskRunOnce uses — so the notify chain fires through its one existing path
// and no new machinery is involved (Rule 35).
//
// The test drives the REAL writer (taskRunSpawnFailure) against a REAL ledger
// on the Rule 32 scaffold, and reads the result back through the production
// projections (`tools.TaskStatusOf`, `tools.TaskShow`) — the same columns an
// operator's `task status` renders. A test that hand-wrote status='failed'
// would prove nothing about the arm under test.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// spawnFailureFixture pins the ledger, arms the notify sink, and returns the id
// of a CLAIMED row — the exact state the funnel is in at the moment the exec
// returns its error.
//
// The ledger comes from testutil.NewTestDBPath (Rule 32): fresh, per-test, and
// placed INSIDE a profile root, which is what lets a transition's event pass
// taskNotifyDelivers' own-ledger gate instead of being suppressed as FOREIGN.
// A hand-rolled path would make every notification assertion below fail for a
// reason that has nothing to do with the code under test.
func spawnFailureFixture(t *testing.T) (taskID, notifyLog string) {
	t.Helper()
	ledger := testutil.NewTestDBPath(t)
	// THE PROFILE PIN, and it is what makes the notify assertion below able to
	// pass at all. gopi resolves the LEDGER and the NOTIFY TARGET through two
	// independent channels (internal/tools/tasknotify.go's taskNotifyDelivers
	// documents the measurement): a ledger that is not the PROFILE ROOT's own is
	// treated as FOREIGN and every event it fires is SUPPRESSED — so a fixture
	// that pins only GOPI_MEMORY_DB would assert on a log the pipeline is
	// forbidden to write. NewTestDBPath already places the ledger inside
	// `<dir>/.gopi/`, so pointing GOPI_HOME at `<dir>` makes the two channels
	// name the same file.
	t.Setenv(config.GopiDirEnv, filepath.Dir(filepath.Dir(ledger)))

	notifyLog = filepath.Join(t.TempDir(), "notify.log")
	t.Setenv(config.NotifyCmdEnv, "cat >> "+notifyLog)
	t.Setenv(config.NotifyEventsEnv, tools.TaskStatusFailed)
	t.Setenv(config.NotifyAsyncEnv, "0")
	t.Setenv(config.NotifyWebhookEnv, "")

	// The writeback seam branches on this latch: a worker ROUND routes through
	// TaskRunWritebackTagged, anything else through the plain writeback. This
	// test is not the worker loop, so the latch must be clear — and it must be
	// restored, because it is package state the rest of the suite shares.
	prevLatch := workerWriteback
	workerWriteback = ""
	t.Cleanup(func() { workerWriteback = prevLatch })

	// THE ROW MUST BE BORN `pending`. TaskNew registers a row already at
	// in_progress (the auto-register start hook's shape), which a claim then
	// REFUSES — so the fixture seeds through the SUBMIT door, exactly as the
	// sibling fixtures in internal/tools do (submitFixture/claimFixture), and
	// claims afterwards. The TaskID knob keeps the id stable for the assertions.
	taskID = "t-spawn-failure-verdict"
	if _, gotID, err := tools.TaskSubmitReturningID(tools.TaskSubmitOptions{
		Summary: "spawn failure verdict fixture",
		Payload: "fixture: a row whose exec fails, observed at the claimed state",
		Source:  "human",
		TaskID:  taskID,
	}); err != nil {
		t.Fatalf("seed the row: %v", err)
	} else if gotID != taskID {
		t.Fatalf("submit returned id %q, want the pinned %q", gotID, taskID)
	}
	if err := tools.TaskClaimExplicitForce(taskID, "fixture-host:1", true); err != nil {
		t.Fatalf("claim the row (the state a spawn failure is observed in): %v", err)
	}
	if got, err := tools.TaskStatusOf(taskID); err != nil || got != tools.TaskStatusClaimed {
		t.Fatalf("fixture precondition: status = %q, err = %v — want %q", got, err, tools.TaskStatusClaimed)
	}
	return taskID, notifyLog
}

// TestSpawnFailureReachesATerminalVerdict is the repro. RED ON THE PRE-FIX
// TREE: the row was still `claimed` here, and the notify log did not exist,
// because the funnel returned the spawn error without writing anything.
func TestSpawnFailureReachesATerminalVerdict(t *testing.T) {
	taskID, notifyLog := spawnFailureFixture(t)

	spawnErr := errors.New(`exec: "gopi": permission denied`)
	gotErr := taskRunSpawnFailure(taskID, spawnErr, "", "")

	// ① THE RUN'S OUTCOME IS UNCHANGED. The writeback adds a record; it must
	// not swallow or replace the error the worker loop acts on.
	if gotErr == nil {
		t.Fatal("taskRunSpawnFailure returned nil — the spawn error must still reach the caller/exit code")
	}
	if !errors.Is(gotErr, spawnErr) {
		t.Fatalf("the returned error does not wrap the spawn error (%v), so a caller matching on it loses the cause: %v",
			spawnErr, gotErr)
	}

	// ② THE ROW REACHES A TERMINAL VERDICT — the half that stayed `claimed`
	// for nine hours.
	status, err := tools.TaskStatusOf(taskID)
	if err != nil {
		t.Fatalf("read the status back: %v", err)
	}
	if status != tools.TaskStatusFailed {
		t.Fatalf("status = %q after a failed exec — a row whose child never existed must reach %q (the legal "+
			"claimed→failed edge), not sit at %q until an operator notices",
			status, tools.TaskStatusFailed, status)
	}

	// ③ THE CAUSE RIDES THE ROW, so `task show` answers "why" without the run
	// log. The sentence is the seam's own, built by ONE function that the
	// trail event and the reason column share.
	show, err := tools.TaskShow(taskID)
	if err != nil {
		t.Fatalf("task show: %v", err)
	}
	if !strings.Contains(show, taskRunSpawnFailureReasonPrefix) {
		t.Fatalf("the row carries no %q reason — a failed row with no cause is the silent-failure shape. task show:\n%s",
			taskRunSpawnFailureReasonPrefix, show)
	}
	if !strings.Contains(show, "permission denied") {
		t.Errorf("the row's reason does not carry the exec error's own words; task show:\n%s", show)
	}

	// ④ THE NOTIFY CHAIN FIRED. This is the consequence of ② and the reason
	// the defect mattered beyond one stale row: no transition, no event.
	body, readErr := os.ReadFile(notifyLog)
	if readErr != nil {
		t.Fatalf("the %q transition never reached the configured notify command (Rule 15 — a suppressed event "+
			"is as invisible as a missing one): %v", tools.TaskStatusFailed, readErr)
	}
	line := string(body)
	if !strings.Contains(line, taskID) {
		t.Fatalf("the delivered payload does not name the row %s: %s", taskID, line)
	}
	if !strings.Contains(line, `"event":"`+tools.TaskStatusFailed+`"`) {
		t.Fatalf("the delivered payload is not a %q event: %s", tools.TaskStatusFailed, line)
	}
}

// TestSpawnFailureVerdictIsWrittenByTheFunnel pins the CALL SITE, not just the
// writer: the defect was a missing line in runTaskRunOnce, so a green on the
// writer alone does not prove the funnel writes the verdict. A future edit that
// drops the call (or restores a bare `return fmt.Errorf(...)`) turns this red.
func TestSpawnFailureVerdictIsWrittenByTheFunnel(t *testing.T) {
	body := seamFuncBody(t, "cli_tasks.go", "func runTaskRunOnce(")
	if !strings.Contains(body, "taskRunSpawnFailure(") {
		t.Fatalf("runTaskRunOnce no longer routes a failed exec through taskRunSpawnFailure — the row would "+
			"stay claimed and no `failed` event would be emitted (row 01a1b5e2-ca5e). Body:\n%s", body)
	}
	// The spawn seam is where the FACT is recorded; it must stay a fact
	// recorder. If it ever grows a FAILURE VERTICT of its own, two writers
	// would race for the same row and the funnel's verdict could be
	// overwritten by a write that happened microseconds earlier.
	//
	// It DOES write `in_progress` on success (TaskStartChildAdmitted) — that is
	// the entire point of epic 01a1a87b, so the probe targets the failure side
	// only: the seam must not perform the `failed` writeback.
	spawn := seamFuncBody(t, "cli_tasks.go", "func taskRunSpawnChild(")
	// The probe matches the CALL, not the WORD: the seam's own prose discusses
	// the failure arm it deliberately does not own, so a bare "failed" match
	// would flag the comment that explains the split.
	if strings.Contains(spawn, "writebackFailure(") || strings.Contains(spawn, "tools.TaskStatusFailed") {
		t.Fatalf("the spawn seam must record the failure FACT (TaskStampStartFailure) and leave the VERDICT "+
			"to the funnel — it now writes a failed verdict of its own, which reintroduces two writers "+
			"for one row. Body:\n%s", spawn)
	}
}
