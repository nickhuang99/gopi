package main

// worker_taskid_one_row_test.go — `gopi worker <task-id>` runs EXACTLY ONE row
// and exits; it never drains the queue.
//
// THE CONTRACT UNDER TEST (epic worker-taskid-one-row): `gopi worker -h` says
//    gopi worker [<task-id>]     run THAT row and nothing else, then exit
// and the briefing that opened this epic ("it is observed gopi worker taskid is
// not following help explanation of executing taskid once and exit, more like
// executing taskid first and then retrieve next to execute until task queue is
// drained") says a reader could not tell whether that promise holds. This file
// is the answer, measured rather than argued:
//
//	the named row runs, and every OTHER pending row in the SAME ledger is still
//	`pending` after the face returns.
//
// WHY THE QUEUE MUST BE NON-EMPTY (the red-proof): on an empty queue a
// drain-the-queue implementation and a one-row implementation produce the same
// observable result, so the test would pass on the very code it is meant to
// pin. The fixture therefore queues THREE rows and names the OLDEST — exactly
// the shape a loop would use to claim "the next one". A face that ran a second
// row would leave row 2 `completed`/`failed` instead of `pending`.
//
// WHAT IS DRIVEN, AND WHAT IS DELIBERATELY NOT: the row EXECUTION here is the
// hermetic synthetic child (taskChildFailTestEnv), because the property is the
// SELECTION — how many rows the face takes — not the child's work; a real model
// call would add a network dependency to a claim-boundary assertion. (The
// shipped binary's own one-row behavior against the same fixture is recorded in
// the task report.) The real-child path is covered by the e2e rigs that drive
// the binary (cli_tasks_test.go and the singleton guard).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// workerOneRowFixtureRows are the three queued rows the fixture seeds, oldest
// first. The test names the FIRST and asserts the other two are untouched.
//
// The PRIORITIES are what make a second round detectable: a claim scan takes the
// lowest priority first, and the named row carries the HIGHEST number of the
// three — so it is the LAST row a claim would pick, and the row a claim would
// pick first (workerOneRowFixtureRows[1]) is left pending by the named face. A
// drain-capable face therefore moves both observations at once.
var workerOneRowFixtureRows = []string{"one-row-1", "one-row-2", "one-row-3"}

// workerOneRowFixturePriorities are the rows' queue weights (lower runs first,
// M4.11): the NAMED row (index 0) is the most urgent-looking of the three, so
// naming it is a deliberate mismatch with the claim order.
var workerOneRowFixturePriorities = []int{30, 10, 20}

// seedWorkerOneRowFixture queues the fixture rows in ONE ledger and returns the
// sandbox home (the worker's own `GOPI_HOME`, so the sandbox precondition on
// the hermetic escape resolves inside a measured temp root).
// The worker-stop latch is process-global, and a sibling test in this package
// that armed a drain leaves it SET — a named round then refuses to start work
// "the operator already asked us to stop", which looks exactly like the defect
// this file pins. Measured: the full-package run made this test fail with "the
// named row is still pending" purely because of that leaked latch. So the
// fixture clears it, the way the drain suite's own tests do.
func seedWorkerOneRowFixture(t *testing.T) string {
	t.Helper()
	resetWorkerStopState()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	// Single-node claim: no peer leg, no ssh spawn — the property is local.
	t.Setenv(config.SkipClaimBroadcastEnv, "1")
	t.Setenv(config.SyncPeersEnv, "")
	// The hermetic child (no model call, no API key) — see the file header.
	t.Setenv(taskChildFailTestEnv, "1")
	// One idle round is enough for any loop face; the assertion is about ROWS,
	// and a bounded knob keeps a regression from hanging the suite.
	t.Setenv(workerIdleExitsEnv, "1")
	t.Setenv(workerIdleIntervalEnv, "1")
	for i, id := range workerOneRowFixtureRows {
		if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
			Summary: "one-row fixture " + id,
			Payload: "run the delivered fix's tests",
			TaskID:  id,
			Source:  "human",
			// The priorities make the claim ORDER differ from the naming order
			// — see workerOneRowFixturePriorities.
			Priority: workerOneRowFixturePriorities[i],
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	return home
}

// TestWorkerExplicitRunsExactlyOneRow is the contract: the NAMED face runs its
// row, writes back ITS row only, and leaves every other pending row alone.
func TestWorkerExplicitRunsExactlyOneRow(t *testing.T) {
	SandboxRepoContext(t)
	_ = seedWorkerOneRowFixture(t)

	// THE NAMED ROW IS THE ONE A CLAIM WOULD NOT PICK FIRST, and the most
	// urgent row is left pending. That asymmetry is the whole red-proof: on a
	// drain-capable named face (the "claim when nothing was named" fallback this
	// epic is about) the round would claim `workerOneRowFixtureClaimedFirst`
	// instead, so BOTH assertions below flip — the named row would stay pending
	// AND the claim winner would move. Naming the row a claim would have chosen
	// anyway made that mutation invisible (measured: the first version of this
	// test passed on the drain-capable face).
	named := workerOneRowFixtureRows[0]
	_, _ = workerRoundNamed(named, &tasksOptions{taskID: named}) //stylegate:ignore claims a round against the test's ledger; the settled state is the assertion

	// ① the named row LEFT pending (it ran: the synthetic child fails it, which
	// is still a terminal writeback of THAT row).
	if got := oneRowTaskStatus(t, named); got == tools.TaskStatusPending {
		t.Fatalf("the named row %s is still %s — the round did not run it (it ran a row the operator did not name?)", named, got)
	}
	// ② every OTHER row is untouched — including the row a claim scan would have
	// selected first. This is the assertion a drain-the-queue face fails.
	for _, id := range workerOneRowFixtureRows[1:] {
		if got := oneRowTaskStatus(t, id); got != tools.TaskStatusPending {
			t.Errorf("row %s is %s — the NAMED face ran a row it was not given (the queue must stay untouched)", id, got)
		}
	}
}

// TestWorkerExplicitNamesNoLoop is the STRUCTURAL half, asserted on the source
// text so a later edit cannot quietly re-couple the faces: the named round's
// body must contain no queue selection, and the only claim call in this file
// must live in the claiming round. A future "explicit == \"\"" style branch
// (the flag-instead-of-two-faces shape) fails here.
func TestWorkerExplicitNamesNoLoop(t *testing.T) {
	src, err := os.ReadFile("cli_tasks_worker.go")
	if err != nil {
		t.Fatalf("read cli_tasks_worker.go: %v", err)
	}
	text := string(src)
	named := funcBody(t, text, "func workerRoundNamed(")
	for _, forbidden := range []string{"TaskNextFiltered", "workerRoundClaiming", "runTaskWorkerLoop"} {
		if strings.Contains(named, forbidden) {
			t.Errorf("workerRoundNamed calls %s — a NAMED round must have no way to select another row:\n%s", forbidden, named)
		}
	}
	claiming := funcBody(t, text, "func workerRoundClaiming(")
	if !strings.Contains(claiming, "TaskNextFiltered") {
		t.Errorf("workerRoundClaiming lost the queue scan — the LOOP face is the only claimer:\n%s", claiming)
	}
	if n := strings.Count(text, "tools.TaskNextFiltered("); n != 1 {
		t.Errorf("tools.TaskNextFiltered( is CALLED %d times in cli_tasks_worker.go, want exactly 1 (the claiming round)", n)
	}
	if n := strings.Count(text, "runTaskWorkerLoop(once)"); n != 1 {
		t.Errorf("runTaskWorkerLoop(once) is referenced %d times in cli_tasks_worker.go, want exactly 1 (the loop face's own entry)", n)
	}
	// The named face's OWN entry must dispatch on a non-empty row BEFORE any
	// loop entry — asserted on the dispatch function's body.
	dispatch := funcBody(t, text, "func runTaskWorker(")
	if !strings.Contains(dispatch, "runTaskWorkerExplicit(") || !strings.Contains(dispatch, "runTaskWorkerLoop(once)") {
		t.Fatalf("runTaskWorker no longer routes both faces:\n%s", dispatch)
	}
	if strings.Index(dispatch, "runTaskWorkerExplicit(") > strings.Index(dispatch, "runTaskWorkerLoop(once)") {
		t.Errorf("runTaskWorker reaches the LOOP before the named face — the named row must be routed first:\n%s", dispatch)
	}
}

// taskStatusOf reads one row's status through the tools layer (the same reader
// the ledger's own verbs use).
func oneRowTaskStatus(t *testing.T, taskID string) string {
	t.Helper()
	status, err := tools.TaskStatusOf(taskID)
	if err != nil {
		t.Fatalf("read status of %s: %v", taskID, err)
	}
	return status
}

// funcBody returns the source of the function whose declaration line starts with
// `prefix`, up to the first line that is exactly "}" — enough for the
// structural assertions above (these functions have no nested top-level braces
// at column 0 in their bodies).
func funcBody(t *testing.T, src, prefix string) string {
	t.Helper()
	i := strings.Index(src, prefix)
	if i < 0 {
		t.Fatalf("%q not found in cli_tasks_worker.go — the function this test pins was renamed or removed", prefix)
	}
	rest := src[i:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}
