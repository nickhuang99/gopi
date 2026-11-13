package main

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The poll's goroutine is identified in a stack dump by this frame — the same
// marker the suite's close-out sweep prints when it reports a PARKED repository
// goroutine (suite_stall_watchdog_test.go). Counting it is how this test sees
// what that sweep sees.
//
// The marker names the METHOD, not the closure's ordinal suffix (`…func2`) and
// not the file's line number: only a RUNNING goroutine appears in a stack dump,
// and installFileWatcher's other closure is the no-requests early return, which
// never becomes a goroutine — so this string cannot match anything but the poll.
const fileWatcherGoroutineMarker = "workerDaemonCtx.installFileWatcher"

// fileWatcherStartDeadline bounds the wait for the poll to REACH the stack: a
// goroutine that was never scheduled cannot be observed stopping. It is the only
// wait left in this file — the teardown itself is measured WITHOUT one (see
// TestInstallFileWatcherIsUninstallable), because a settle window between
// uninstall() and the count is exactly what hides a stop that does not join.
const fileWatcherStartDeadline = 2 * time.Second

// countFileWatcherGoroutines returns how many of this process's goroutines are
// parked inside the durable-request poll.
func countFileWatcherGoroutines() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), fileWatcherGoroutineMarker)
}

// TestInstallFileWatcherIsUninstallable is the regression guard for a leak the
// suite's own close-out sweep caught and failed the run over (2026-10-13):
//
//	suite goroutine sweep: parked repository goroutines: 6
//	  github.com/nick/gopi/cmd/agent.workerDaemonCtx.installFileWatcher.func1()
//
// The watcher used to be ends-only-by-exit — a `for range t.C` loop with no exit
// path — which is correct for a production worker (the process exits when the
// loop returns) and wrong for this package's suite, which drives the worker
// IN-PROCESS (TestRunTaskWorker_OnceSweepsZombieFirst,
// TestRunTaskWorker_SurvivesFailedRun). Each such case parked one goroutine for
// the life of the test binary; TestMain's sweep counted them and turned an
// otherwise green run red (testmain_test.go: `parked > 0 && code == 0` → code 1).
//
// WHAT THIS PINS (two things, and the second is why the first was not enough):
//
//  1. installFileWatcher hands the caller the means to end the poll. Deleting
//     the stop channel (or returning a no-op uninstall) makes this red.
//  2. The uninstall JOINS — the poll has LEFT the stack by the time uninstall()
//     RETURNS. The first version of this test waited up to two seconds after
//     uninstall() before counting, and that window is precisely what let a
//     signal-only teardown (`close(stopped); return`) pass: TestMain's close-out
//     sweep runs the instruction AFTER the deferred uninstall, with NO window at
//     all, so a stop that merely makes the goroutine RUNNABLE was measured as
//     green here and as `parked repository goroutines: N` there. The count below
//     is therefore taken the instant uninstall() returns, and it is an exact
//     equality, not a "no more than before": this test's own install must take
//     the number back to where it started.
//
// A third call site shape is pinned by the penultimate step: teardown twice
// (deferred stop plus an explicit early stop) is ordinary caller code, and a
// bare close(stopped) would panic on it.
func TestInstallFileWatcherIsUninstallable(t *testing.T) {
	// A request-capable ctx is the condition the watcher installs under; its dir
	// is a fresh empty directory, so the poll's own consumption reads only
	// absent request files (an absent file is not a stop).
	ctx := workerDaemonCtx{dir: t.TempDir(), pid: os.Getpid()}
	if !ctx.consumesRequests() {
		t.Fatalf("this fixture must be installable: consumesRequests() is false for dir %q", ctx.dir)
	}

	before := countFileWatcherGoroutines()
	uninstall := ctx.installFileWatcher()
	if uninstall == nil {
		t.Fatal("installFileWatcher must return an uninstall, not nil: a caller that cannot end the poll leaves it parked")
	}

	// The goroutine must really be running, or the count below would prove
	// nothing (a poll that never started cannot be observed stopping). This is
	// the ONLY wait in this test, and it is before the teardown.
	deadline := time.Now().Add(fileWatcherStartDeadline)
	for countFileWatcherGoroutines() <= before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond) //stylegate:allow-sleep bounded poll tick — the watcher's start has no completion channel to wait on, only the observed count (deadline above)
	}
	if started := countFileWatcherGoroutines(); started <= before {
		t.Fatalf("the poll did not start: %d parked goroutine(s), was %d before the install", started, before)
	}

	uninstall()

	// NO settle window: this is the instant TestMain's close-out sweep samples.
	if parked := countFileWatcherGoroutines(); parked != before {
		t.Fatalf("uninstall() returned with the poll still on the stack: %d goroutine(s), was %d before the install "+
			"(the stop function signalled the loop but did not JOIN it — the poll is only RUNNABLE, and the suite's "+
			"close-out sweep, which measures the very next instruction, reports it as a parked repository goroutine)",
			parked, before)
	}

	// The same uninstall twice is the caller shape a deferred stop plus an
	// explicit stop produces; it must be a no-op, never `close of closed channel`.
	uninstall()
	if parked := countFileWatcherGoroutines(); parked != before {
		t.Fatalf("the second uninstall() call disturbed the count: %d goroutine(s), was %d before the install", parked, before)
	}
}

// fileWatcherLoopDrainDeadline bounds the wait for the in-process loop below to
// RETURN on its own (one drained round at the shortest wait the resolver
// accepts). Generous on purpose: the property is "it returns", never "in N ms".
const fileWatcherLoopDrainDeadline = 60 * time.Second

// TestWorkerLoopLeavesNoWatcherPollBehind pins the CALLER half of the join, on
// the real caller — the test above proves installFileWatcher's uninstall joins,
// but nothing there would fail if a call site stopped arming the teardown
// altogether (`defer workerCtx.installFileWatcher()()` evaluates the call at the
// defer statement: install, stop, and then run the row with NO watcher at all —
// silently dropping the durable-stop door an operator's `--stop` depends on, and
// leaving the unit test that measures the uninstall perfectly green).
//
// So this drives `runTaskWorker` itself, in-process, and asserts both halves of
// the contract at once: the poll IS armed for the duration of the run, and after
// the run returns there is no poll on the stack — the instant TestMain's
// close-out sweep measures.
func TestWorkerLoopLeavesNoWatcherPollBehind(t *testing.T) {
	SandboxRepoContext(t)
	workerIdleSandbox(t)
	t.Setenv(workerIdleExitsEnv, "1") // one drained round → the loop returns

	// The loop only installs under a request-capable context (an inert profile
	// has nothing to watch). Resolve it the way the loop does, and say so rather
	// than passing vacuously if this profile cannot arm the poll.
	if probe := workerCtxFor(os.Getpid()); !probe.consumesRequests() {
		t.Skipf("this profile resolves no control dir, so the loop installs no poll — there is nothing to observe")
	}

	before := countFileWatcherGoroutines()
	loopDone := make(chan struct{})
	captureStdout(t, func() {
		go func() {
			defer close(loopDone)
			runTaskWorker(false, "", nil)
		}()

		// ① THE POLL IS ARMED WHILE THE LOOP RUNS. Without this the test proves
		// nothing: a loop that never installed a watcher would pass the teardown
		// assertion trivially — and that is exactly the silent-drop shape above.
		deadline := time.Now().Add(fileWatcherStartDeadline)
		for countFileWatcherGoroutines() <= before && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond) //stylegate:allow-sleep bounded poll tick — the loop's armed watcher is observed through the goroutine count, never signalled (deadline above)
		}
		if armed := countFileWatcherGoroutines(); armed <= before {
			t.Fatalf("the running loop has no file-watcher poll on the stack (%d, was %d before): the durable stop "+
				"request no longer has a reader while a row runs", armed, before)
		}

		// ② AND IT IS GONE WHEN THE LOOP RETURNS — measured with no settle
		// window, because this is the instruction TestMain's close-out sweep
		// follows.
		select {
		case <-loopDone:
		case <-time.After(fileWatcherLoopDrainDeadline):
			t.Fatal("the in-process loop did not return after its idle round — the measurement below cannot be taken")
		}
		if parked := countFileWatcherGoroutines(); parked > before {
			t.Fatalf("runTaskWorker returned with the file-watcher poll still on the stack (%d, was %d before): the "+
				"caller's teardown did not JOIN the poll it installed, so the suite's close-out sweep reports it as a "+
				"parked repository goroutine and the whole package exits non-zero", parked, before)
		}
	})
}

// TestFileWatcherIsResidentUntilStopped is the SEMANTICS companion: the join
// changes only how a RETURNING caller ends the poll, never how a RESIDENT one
// behaves. In production nothing calls the uninstall — the process exits when
// the loop returns — so the poll must be a resident that keeps polling across
// its ticks, exactly as before.
//
// WHAT THIS PINS: after two full tick intervals the poll is still on the stack
// (a tick does not end it), and it ends when — and only when — the uninstall is
// called. It asserts presence, never timing, so a loaded host cannot flake it.
func TestFileWatcherIsResidentUntilStopped(t *testing.T) {
	ctx := workerDaemonCtx{dir: t.TempDir(), pid: os.Getpid()}

	before := countFileWatcherGoroutines()
	uninstall := ctx.installFileWatcher()
	defer uninstall()

	deadline := time.Now().Add(fileWatcherStartDeadline)
	for countFileWatcherGoroutines() <= before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond) //stylegate:allow-sleep bounded poll tick — see the deadline above: the install is proven by the observed count, not by a channel
	}
	if started := countFileWatcherGoroutines(); started <= before {
		t.Fatalf("the poll did not start: %d parked goroutine(s), was %d before the install", started, before)
	}

	// Two ticks later it must STILL be polling — the resident shape.
	time.Sleep(2*workerFileWatchInterval + workerFileWatchInterval/2) //stylegate:allow-sleep negative observation — the window is the assertion: it must show the resident watcher did NOT stop by itself
	if resident := countFileWatcherGoroutines(); resident <= before {
		t.Fatalf("the poll ended on its own after %s: a resident watcher that stops by itself would leave a "+
			"production worker unable to consume a durable stop request", 2*workerFileWatchInterval)
	}
}
