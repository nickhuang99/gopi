package main

// Worker idle-exit tests (epic brief-worker-idle-exit 2026-09-08): a worker
// WITHOUT --once must not wait forever on a drained queue — after N
// CONSECUTIVE empty rounds (queue drained, no task executed between them)
// it exits 0 on its own. A worker is a task-DRAIN tool that runs and
// self-dies (nick 9/1), not a daemon waiting for Ctrl+C. These tests pin:
//
//	① the env resolver (GOPI_WORKER_IDLE_EXITS: default 3, passthrough, 0
//	   = never, malformed → default);
//	② the loop's idle self-exit: N drained rounds → runTaskWorker(false, "", nil)
//	   RETURNS on its own (no Ctrl+C, no max-round cap);
//	③ the busy face: a task executed between drained rounds RESETS the
//	   streak — only CONSECUTIVE empty rounds accumulate toward the exit;
//	④ the wait-forever opt-out (GOPI_WORKER_IDLE_EXITS=0) still parks on
//	   the drained queue — bounded by GOPI_WORKER_MAX_ROUNDS so the test
//	   terminates (the loop's own round cap is the only way out, exactly
//	   as before this epic).
//
// The loop is driven in-process (runTaskWorker RETURNS on self-exit — no
// os.Exit) through the same hermetic child-failure escape the existing
// worker tests use (taskChildFailTestEnv=1): each claimed row "runs"
// without spawning a model call and lands terminal-failed. A short idle
// interval (GOPI_WORKER_IDLE_SEC=1, the minimum the resolver accepts) keeps
// each drained round ≈ 2s (a verdict = one claim scan + the 1s settled
// recheck). Drained rounds are wall-clock cheap; the assertions are on the
// printed exit/wait lines — the observable self-exit contract.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/flaketest"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/tools"
)

// NOTE: this file declares no time import on purpose — the worker round arms no
// lifecycle clock any more (the throttle, its clock and the task-lifecycle pass
// itself were deleted 2026-10-30: task_lifecycle_tick.go's header carries the
// per-leg verdicts).

// TestWorkerIdleExitsEnv: the self-exit env resolves once (Rule 2) —
// default 3, a positive value passes through, 0 = never, a malformed value
// falls back to the default.
func TestWorkerIdleExitsEnv(t *testing.T) {
	t.Setenv(workerIdleExitsEnv, "")
	if got := workerIdleExits(); got != workerDefaultIdleExits {
		t.Fatalf("absent env: got %d want default %d", got, workerDefaultIdleExits)
	}
	t.Setenv(workerIdleExitsEnv, "5")
	if got := workerIdleExits(); got != 5 {
		t.Fatalf("GOPI_WORKER_IDLE_EXITS=5: got %d want 5", got)
	}
	t.Setenv(workerIdleExitsEnv, "0")
	if got := workerIdleExits(); got != 0 {
		t.Fatalf("GOPI_WORKER_IDLE_EXITS=0: got %d want 0 (wait forever)", got)
	}
	t.Setenv(workerIdleExitsEnv, "not-a-number")
	if got := workerIdleExits(); got != workerDefaultIdleExits {
		t.Fatalf("malformed env: got %d want default %d", got, workerDefaultIdleExits)
	}
}

// workerIdleSandbox arms the hermetic single-node env the worker-loop tests
// need: an empty sandboxed ledger + the child-failure escape + the shortest
// drained-queue wait the resolver accepts (1s). The lifecycle throttle is
// armed so the first round's stale/finish-cleanup sweeps don't add churn to
// a test that pins idle timing (same move as the sibling worker tests).
func workerIdleSandbox(t *testing.T) {
	t.Helper()
	sandbox := t.TempDir()
	t.Setenv("GOPI_HOME", sandbox)
	t.Setenv("GOPI_MEMORY_DB", sandbox+"/memory.db")
	t.Setenv("GOPI_COMPUTE_NODES", "")
	t.Setenv(config.SyncPeersEnv, "")
	t.Setenv("GOPI_SETTINGS", sandbox+"/nonexistent.json")
	t.Setenv(config.SkipClaimBroadcastEnv, "1") // single-node hermetic claim
	t.Setenv(taskChildFailTestEnv, "1")         // child fails without spawning
	t.Setenv(workerIdleIntervalEnv, "1")        // drained-queue wait: 1s (resolver min)
	// repo-context sandbox: the process must not stand in the live repository,
	// or the round's own-worktree release enumerates the developer's worktrees.
	SandboxRepoContext(t)
}

// armWorkerIdleDrainHook installs an idle-drain observer for the duration of
// the test (cli_tasks_worker_idle_hook.go). The loop invokes it
// synchronously right after a drained verdict's waiting line is printed, so
// the observer can commit the next round's task with no wall-clock guess.
// Torn down via t.Cleanup: a stale hook would leak into a sibling test.
func armWorkerIdleDrainHook(t *testing.T, hook workerIdleDrainHook) {
	t.Helper()
	workerIdleDrainHooks = append(workerIdleDrainHooks, hook)
	t.Cleanup(func() { workerIdleDrainHooks = nil })
}

// TestWorkerSelfExitsAfterIdleRounds (acceptance 1): a worker WITHOUT
// --once whose queue is empty must NOT wait forever. With
// GOPI_WORKER_IDLE_EXITS=1 it self-exits after ONE drained round — the
// runTaskWorker(false, "", nil) call RETURNS (the process would exit 0) with the
// self-exit line printed, no Ctrl+C and no max-round cap involved.
func TestWorkerSelfExitsAfterIdleRounds(t *testing.T) {
	// repo-context sandbox: the in-process driver runs the round's own-worktree
	// release under THIS process's cwd — the live repository — so its pin is the
	// only control (no child env vector exists to carry one). See
	// repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	workerIdleSandbox(t)
	t.Setenv(workerIdleExitsEnv, "1") // one drained round → self-exit

	out := captureStdout(t, func() { runTaskWorker(false, "", nil) })
	if !strings.Contains(out, "worker: loop start") {
		t.Fatalf("missing loop-start banner:\n%s", out)
	}
	if !strings.Contains(out, "1 idle round(s) with no new task — exiting 0") {
		t.Errorf("missing self-exit line (worker must die on its own after N idle rounds):\n%s", out)
	}
}

// TestWorkerBusyRoundResetsIdleStreak (acceptance 3): a task executed
// between drained rounds RESETS the streak — only CONSECUTIVE empty rounds
// accumulate toward the exit. Sequence with GOPI_WORKER_IDLE_EXITS=3:
//
//	round 1: queue empty → drained verdict ①  → "waiting (1/3 idle rounds)"
//	   (the idle-drain hook fires HERE, synchronously: the seed task is
//	   committed while round 1 is still waiting, so verdict ① has banked
//	   and printed before the task exists — no wall-clock guess involved)
//	round 2: task claimed + run (busy → streak reset to 0)
//	rounds 3-4: empty → verdicts ① ② → "waiting (1/3)" then "waiting (2/3)"
//	round 5: empty → verdict ③ → self-exit "3 idle round(s)"
//
// The "(1/3 idle rounds)" wait line must therefore appear TWICE — once
// before the busy round and once after it — proving the busy round threw
// the pre-existing drain away. Without the reset the count would be
// 1(before) + busy + 2(after) = 3 and the worker would self-exit after
// round 3 with the task possibly still pending.
// NOT classified heavy: testdata/flakes.tsv:47 registers this test, and a
// registered flake must stay on the path that exposes its flake
// (TestFlakeRegistryMatchesTheTestsItNames refuses both at once: "a flake is a
// DEFECT, not a heaviness class"). It is 8.62s and will be named by every
// heavy-drift report until the flake registry lets it go.
func TestWorkerBusyRoundResetsIdleStreak(t *testing.T) {
	flaketest.Declare("deterministic: the fixture seeded the task row from a goroutine that slept a wall-clock 1.5s and assumed the worker's round would claim the row inside that window — whether the claim and the busy round landed inside the sleep decided the verdict (measured 3/10 FAIL on clean master). Registered: testdata/flakes.tsv, owner 01a08920-a8d3-7272-9ab1-78e6aeabacbc.")
	// repo-context sandbox: the in-process driver runs the round's own-worktree
	// release under THIS process's cwd — the live repository — so its pin is the
	// only control (no child env vector exists to carry one). See
	// repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	workerIdleSandbox(t)
	t.Setenv(workerIdleExitsEnv, "3") // exit needs three CONSECUTIVE empty rounds
	t.Setenv(workerMaxRoundsEnv, "0") // no round cap — only the idle exit may end it

	// Deterministic seeding (no sleep): the hook runs the instant round 1's
	// drained verdict ① has banked and printed its waiting line, and because
	// the loop calls it synchronously the submit lands while round 1 is
	// still inside its wait — round 2's claim scan cannot run before the row
	// is committed, so the task can never be claimed in the round whose
	// verdict has not banked yet. (The former 1.5s sleep exactly raced that
	// claim scan: ~1-in-5 under load round 1 claimed the task itself, the
	// pre-task "(1/3 idle rounds)" line never printed, and the count>=2
	// assertion failed.)
	seeds := 0
	armWorkerIdleDrainHook(t, func(round, streak int) {
		if seeds > 0 || round != 1 {
			return // exactly one seed, at verdict ①
		}
		seeds++
		if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
			Summary: "busy reset probe",
			Payload: "this run must reset the idle streak",
			TaskID:  "t-busy-reset-1",
			Source:  "human",
		}); err != nil {
			t.Errorf("seed: %v", err)
		}
	})

	out := captureStdout(t, func() { runTaskWorker(false, "", nil) })
	if seeds != 1 {
		t.Fatalf("idle-drain hook fired %d seed(s), want exactly 1", seeds)
	}

	// The task row must be terminal — the busy round actually RAN it.
	row, err := tools.TaskShow("t-busy-reset-1")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(row, "failed") {
		t.Errorf("busy task must have been executed to terminal:\n%s", row)
	}
	// The busy round RESET the streak: "(1/3 idle rounds)" prints once
	// before the task and once after (re-accumulation from zero), then the
	// third drained verdict self-exits.
	if got := strings.Count(out, "(1/3 idle rounds)"); got < 2 {
		t.Errorf("busy round must reset the idle streak (1/3 must print before AND after the task), got %d:\n%s", got, out)
	}
	if !strings.Contains(out, "3 idle round(s) with no new task — exiting 0") {
		t.Errorf("worker must self-exit after 3 post-reset drained rounds:\n%s", out)
	}
}

// TestWorkerIdleExitZeroWaitsForever (the GOPI_WORKER_IDLE_EXITS=0 opt-out):
// a worker configured to wait forever parks on the drained queue and does
// NOT self-exit — bounded by GOPI_WORKER_MAX_ROUNDS so the test terminates
// (the loop's own round cap is the only way out, exactly as before this
// epic). This pins that 0 preserves the pre-fix daemon semantics.
func TestWorkerIdleExitZeroWaitsForever(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	// repo-context sandbox: the in-process driver runs the round's own-worktree
	// release under THIS process's cwd — the live repository — so its pin is the
	// only control (no child env vector exists to carry one). See
	// repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	workerIdleSandbox(t)
	t.Setenv(workerIdleExitsEnv, "0") // wait forever
	t.Setenv(workerMaxRoundsEnv, "2") // bound: 2 rounds then the cap exits

	out := captureStdout(t, func() { runTaskWorker(false, "", nil) })
	if strings.Contains(out, "exiting 0") {
		t.Errorf("GOPI_WORKER_IDLE_EXITS=0 must never self-exit on idle:\n%s", out)
	}
	if !strings.Contains(out, "max rounds (2) reached — exiting") {
		t.Errorf("0 = wait forever: only the round cap may end the loop:\n%s", out)
	}
}
