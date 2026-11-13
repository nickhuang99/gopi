// worker_round_retry_orphan_test.go — the regression guard for task 01a15bd1:
// a finished round must not leave the retry ladder's handed-off resume running.
//
// THE DEFECT (measured 2026-10-21): the failure path's retry ladder spawns the
// recovery run with SysProcAttr{Setsid: true} and returns without waiting, so a
// foreground `gopi worker <row>` printed "run … failed (continuing)" and left
// pid 3380552 alive for 8m27s — orphaned (ppid=systemd --user), its own session,
// cwd = the round's worktree. The round could not release the ground because a
// process it started was standing in it.
//
// THREE ARMS, and each pins a DIFFERENT half (a passing unit test of the wait
// alone would prove nothing about whether anything calls it):
//
//	① the LADDER PUBLISHES the handed-off pid (behavioral: the real
//	  taskRunAbnormalChildRetry against a real ledger, with a real child as the
//	  "resume" — the production decision path, only the spawn seam stubbed);
//	② the WAIT actually blocks on a live process and returns when it exits
//	  (behavioral, real child, real /proc);
//	③ the ROUND CALLS the wait, before it releases the ground (source gate —
//	  the call site is what makes ①+② matter, and a deleted call is exactly the
//	  regression this file exists for).
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

const (
	// orphanFixtureProgram is the "resume" the ladder hands off in these tests:
	// a real process that outlives the ladder call by design (coreutils, not a
	// shell builtin, so it is a process and not a fork of this one).
	orphanFixtureProgram = "sleep"
	// orphanFixtureHoldSec is its lifetime when arm ② must observe a LIVE pid.
	orphanFixtureHoldSec = "30"
	// orphanFixtureShortSec is its lifetime when arm ② must observe the EXIT
	// (long enough to prove the wait blocked, short enough to keep the suite
	// quick).
	orphanFixtureShortSec = "0.4"
	// orphanWaitFloor is the minimum time arm ② must have spent waiting for
	// that short-lived child: it is under the child's lifetime on purpose (a
	// scheduler hiccup may delay the exit) but far above "did not wait at all".
	orphanWaitFloor = 250 * time.Millisecond
	// orphanWaitCeiling bounds arm ② so a broken wait cannot hang the suite.
	orphanWaitCeiling = 30 * time.Second
)

// TestLadderPublishesTheHandedOffResume — arm ①.
//
// The ladder's whole point is to hand a resume OFF; the pid it hands off is what
// the round must then wait for. Without this publication the round has nothing
// to wait on, and the fix would be dead code with a green test beside it.
func TestLadderPublishesTheHandedOffResume(t *testing.T) {
	f := newRetryFixture(t, retryFixtureRowID)
	child := startOrphanFixture(t, orphanFixtureShortSec)

	// Replace the fixture's stub with one that reports the REAL child as the
	// resume it started: same contract (pid > 0 = an attempt was spent), real
	// process to observe afterwards.
	taskRunResumeSpawn = func(taskID, target string) (string, string, int, error) {
		f.spawns++
		return "gopi session resume " + target + " --task " + taskID, t.TempDir() + "/resume.log", child.Process.Pid, nil
	}
	taskRunAbnormalChildRetry(retryFixtureRowID, false, "", nil)

	if f.spawns != 1 {
		t.Fatalf("the ladder must start exactly one resume on an open row, started %d", f.spawns)
	}
	if got := workerRoundRetryResumePID(); got != child.Process.Pid {
		t.Fatalf("the ladder handed off pid %d but the round was told %d — the round would wait on nothing "+
			"(workerRoundNoteRetryResume is the ONE writer; taskRunAbnormalChildRetry must call it)",
			child.Process.Pid, got)
	}
}

// TestRoundWaitsForTheHandedOffResume — arm ②.
//
// The wait must OBSERVE the process: block while it lives, return once it is
// gone. A function that returns immediately would satisfy any assertion about
// its return value, so the assertion here is on TIME plus on the child's own
// state — the two facts that separate "waited" from "looked".
func TestRoundWaitsForTheHandedOffResume(t *testing.T) {
	t.Setenv(workerRoundRetryWaitEnv, orphanWaitSecBudget())
	// A zombie must read as GONE: the fixture child is never reaped by us (the
	// kernel keeps it as a zombie while this test runs), so a `kill(pid, 0)`
	// probe would report it alive forever and this arm would hang. That is the
	// same defect the production liveness rule rejects (taskzombie.go).
	child := startOrphanFixture(t, orphanFixtureShortSec)

	workerRoundResetRetryResume()
	workerRoundNoteRetryResume(child.Process.Pid)
	if got := workerRoundRetryResumePID(); got != child.Process.Pid {
		t.Fatalf("the round did not record the handed-off pid: got %d, want %d", got, child.Process.Pid)
	}

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		workerRoundAwaitRetryResume()
		done <- time.Since(start)
	}()

	select {
	case elapsed := <-done:
		if elapsed < orphanWaitFloor {
			t.Fatalf("the round returned after %s, but the process it handed off (pid %d) lives %ss — "+
				"the wait did not observe it (a returned-immediately wait is the defect this arm guards)",
				elapsed, child.Process.Pid, orphanFixtureShortSec)
		}
		if processRunning(child.Process.Pid) {
			t.Fatalf("the round returned while pid %d was still running", child.Process.Pid)
		}
	case <-time.After(orphanWaitCeiling):
		t.Fatalf("the round's wait did not return within %s for a child living %ss — it is waiting on "+
			"something it cannot measure (a pid that is not its own, or a liveness probe that never says gone)",
			orphanWaitCeiling, orphanFixtureShortSec)
	}

	// The wait consumed the round's pid exactly once: a second round must not
	// inherit it (a recycled number is somebody else's process).
	workerRoundResetRetryResume()
	if got := workerRoundRetryResumePID(); got != 0 {
		t.Fatalf("a finished round left pid %d for the next round to wait on", got)
	}
}

// TestRoundCallsTheWaitBeforeItReleasesTheGround — arm ③.
//
// The call site, not the function, is what fixes the measurement: a round that
// never calls the wait leaves the resume running and the worktree pinned, no
// matter how correct the wait is. It is pinned on the SOURCE because the round
// needs a real claim, a real ground and a real executor child to reach its tail —
// the ordering (`wait` BEFORE the own-worktree release) is the property, and it
// is visible in the text.
func TestRoundCallsTheWaitBeforeItReleasesTheGround(t *testing.T) {
	src, err := os.ReadFile(workerRoundSourceFile)
	if err != nil {
		t.Fatalf("read %s: %v", workerRoundSourceFile, err)
	}
	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, workerRoundSourceFile, src, 0)
	if perr != nil {
		t.Fatalf("parse %s: %v", workerRoundSourceFile, perr)
	}
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == workerRoundFuncName {
			fn = fd
			break
		}
	}
	if fn == nil {
		t.Fatalf("%s: no func %s — the round moved; re-point this gate", workerRoundSourceFile, workerRoundFuncName)
	}
	waitLine, releaseLine := 0, 0
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch calleeName(call) {
		case workerRoundWaitCallName:
			waitLine = fset.Position(call.Pos()).Line
		case workerRoundReleaseCallName:
			// The release is DEFERRED: its call expression's position is where
			// the defer statement reads, which is the ordering this arm wants.
			releaseLine = fset.Position(call.Pos()).Line
		}
		return true
	})
	if waitLine == 0 {
		t.Fatalf("%s: %s never calls %s — the round's retry resume is left running and the row's "+
			"worktree stays pinned by it (task 01a15bd1, measured 8m27s)", workerRoundSourceFile, workerRoundFuncName, workerRoundWaitCallName)
	}
	if releaseLine == 0 {
		t.Fatalf("%s: %s no longer calls %s — this gate's ordering assertion has no anchor",
			workerRoundSourceFile, workerRoundFuncName, workerRoundReleaseCallName)
	}
	if waitLine > releaseLine {
		t.Fatalf("%s: %s calls %s at line %d, AFTER the ground release at line %d — the release is "+
			"exactly the pass a survivor's cwd blocks, so the wait must come first",
			workerRoundSourceFile, workerRoundFuncName, workerRoundWaitCallName, waitLine, releaseLine)
	}
}

// The names arms ①–③ assert. Rule 1b: spelled once, here.
const (
	retryFixtureRowID          = "01a15bd1-0000-7000-8000-00000000f1f1"
	workerRoundSourceFile      = "cli_tasks_worker.go"
	workerRoundFuncName        = "workerRoundRun"
	workerRoundWaitCallName    = "workerRoundAwaitRetryResume"
	workerRoundReleaseCallName = "workerRoundOwnWorktrees"
)

// startOrphanFixture starts a real child the tests can observe, and reaps it at
// teardown (not reaping would leave a zombie per arm, and a zombie is
// deliberately read as GONE by the production probe).
func startOrphanFixture(t *testing.T, lifetime string) *exec.Cmd {
	t.Helper()
	child := exec.Command(orphanFixtureProgram, lifetime)
	if err := child.Start(); err != nil {
		t.Fatalf("start fixture %s %s: %v", orphanFixtureProgram, lifetime, err)
	}
	t.Cleanup(func() {
		//stylegate:ignore reaping the fixture child these arms spawned; its exit status is not the assertion
		_ = child.Wait()
	})
	return child
}

// orphanWaitSecBudget renders a wait budget long enough for arm ② and short
// enough that a broken wait fails instead of hanging — the env spelling the
// budget reader parses (whole seconds).
func orphanWaitSecBudget() string {
	return strconv.Itoa(int(orphanWaitCeiling / time.Second))
}

// calleeName returns the simple name a call invokes ("" for anything else).
func calleeName(call *ast.CallExpr) string {
	if id, ok := call.Fun.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
