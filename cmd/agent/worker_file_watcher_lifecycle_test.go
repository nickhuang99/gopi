// worker_file_watcher_lifecycle_test.go — the REGRESSION gate for the parked
// file watcher, the cause of a PACKAGE-LEVEL fail with no failing test under it.
//
// THE BUG (measured 2026-10-13, tip 3544b69). workerDaemonCtx.installFileWatcher
// started a goroutine whose only channel was its ticker:
//
//	go func() {
//		t := time.NewTicker(workerFileWatchInterval)
//		defer t.Stop()
//		for range t.C { c.consumeDrainRequests() }   // no exit path at all
//	}()
//
// For a RESIDENT worker that is correct — it stops with the process. For a
// caller that INSTALLS AND RETURNS it is not: the in-process runner faces
// (runTaskWorkerLoop / runTaskWorkerExplicit, which the CLI lifecycle tests call
// directly) left one permanently parked goroutine per call, and the suite's
// close-out sweep turned that into
//
//	suite goroutine sweep: parked repository goroutines: 7
//	FAIL	github.com/nick/gopi/cmd/agent	…	(exit 1)
//
// with NO failing test named — the run's only defect was a leak in the
// instrument-free zone between the last test and TestMain's os.Exit.
//
// THE TWO ASSERTIONS, one per half of the fix:
//
//	① the JOIN: installFileWatcher's stop RETURNS ONLY AFTER the goroutine has
//	   left its loop, so a caller that defers it cannot outlive the watcher.
//	② the LEAK SITE: runTaskWorker — the face the tests actually call — returns
//	   with no watcher parked at all.
//
// Assertion ① is a negative control too: the frame counter must SEE the live
// watcher before the stop, or "0 after" would be indistinguishable from a
// counter that sees nothing.
//
// Rule 27: no load is generated. The watcher's only work is a 2s ticker and a
// stat of its own request directory; the assertions are about goroutine frames.
//
// MERGE NOTE (rework run 01a1346231267263b10df123b9898555, 2026-10-13). This file
// is the UNION of two independently-landed fixes for the SAME leak. The branch it
// came from (task/01a133e324, tip b2366a62) was written against merge base
// 3544b690c; master then landed the same fix itself in ba7592966 ("both stops
// JOIN — the watcher's uninstall and the resident sweep's flight", 16:41, an hour
// after b2366a62 at 15:32). Master's ba7592966 owns
// cmd/agent/cli_worker_control.go and cmd/agent/cli_tasks_worker.go — the same
// exit channel, the same join, the same named no-op (it calls them `uninstall` /
// `noopWorkerFileWatcherStop`, the branch `stop` / `noopFileWatcherStop`) with
// richer comments — and carries its own gate,
// cmd/agent/cli_worker_control_watcher_test.go, whose three tests
// (TestInstallFileWatcherIsUninstallable, TestWorkerLoopLeavesNoWatcherPollBehind,
// TestFileWatcherIsResidentUntilStopped) are a SUPERSET of the two the branch
// brought. Master's side is therefore kept VERBATIM in all three files: nothing
// upstream is reverted, and no second spelling of the stop contract is added.
//
// WHAT THIS FILE ADDS is the one assertion master's gate does not make — the LEAK
// SITE at the face the CLI lifecycle tests actually call (runTaskWorker returns
// with no watcher parked), driven through the suite's ONE frame scanner
// (goroutinesRunning).
//
// DELIBERATE DROPS, each named: (1) the branch's re-landing of the same two
// production files — re-applying those bytes would revert master's later
// revision (the `uninstall` naming and the extra commentary) for no behaviour
// gain; (2) the branch's two superseded tests, replaced by master's superset
// above. NOT dropped: the branch's devdoc. Master's
// devdoc/worker-file-watcher-goroutine-leak.md is the same record of the same
// defect on the same baseline (3544b690c), so it stays the single document; the
// branch's copy is archived at this run's session directory
// (artifacts/branch-doc-worker-file-watcher-lifecycle.md) rather than landed as a
// duplicate row in devdoc/DOC-INDEX.tsv.
package main

import (
	"os"
	"testing"
)

const (
	// workerFileWatcherFrame identifies the watcher in a goroutine dump. It is
	// the anonymous function's own name, which is what runtime.Stack prints.
	workerFileWatcherFrame = "workerDaemonCtx.installFileWatcher.func"
)

// workerFileWatcherParked returns the frames of this process's parked file
// watchers. It reads the live dump through the suite's ONE frame scanner
// (goroutinesRunning, chat_boundary_test.go) rather than a second counter, so
// "which goroutines count as ours" has exactly one answer (Rule 14).
func workerFileWatcherParked(t *testing.T) int {
	t.Helper()
	return len(goroutinesRunning(t, workerFileWatcherFrame))
}

// TestWorkerFileWatcherStopsWithItsInstaller is assertion ①: the stop returned by
// installFileWatcher JOINs, so the installer cannot outlive the goroutine.
func TestWorkerFileWatcherStopsWithItsInstaller(t *testing.T) {
	// A context with a control dir (the REQUEST capability) but no writer, so
	// the watcher is installed and nothing else in this test does I/O. t.TempDir
	// is the private profile: no request file stands, so the watcher is a pure
	// ticker wait — exactly the shape a returning caller used to strand.
	ctx := workerDaemonCtx{dir: t.TempDir(), pid: os.Getpid()}

	before := workerFileWatcherParked(t)
	stop := ctx.installFileWatcher()
	// The watcher is LIVE now: if the counter cannot see it, the assertion after
	// the stop proves nothing (it would read 0 either way).
	if live := workerFileWatcherParked(t); live <= before {
		t.Fatalf("the frame counter sees %d parked watcher(s) after install, want more than the %d before it — "+
			"the assertion below would be vacuous (frame %q)", live, before, workerFileWatcherFrame)
	}
	stop()
	if after := workerFileWatcherParked(t); after != before {
		t.Errorf("installFileWatcher's stop returned with %d parked watcher(s), want the %d that stood before the install "+
			"— the stop must JOIN, not only signal", after, before)
	}
	// Idempotent: a second call is harmless (defer + an explicit call in one
	// function is a shape a caller may write).
	stop()
}

// TestWorkerFileWatcherStopIsCallableWhenInert: an inert context (no control dir:
// the zero value, or a directory that could not be resolved) installs nothing —
// and still hands back a stop, so a caller's defer needs no branch (Rule 15).
func TestWorkerFileWatcherStopIsCallableWhenInert(t *testing.T) {
	ctx := workerDaemonCtx{}
	before := workerFileWatcherParked(t)
	stop := ctx.installFileWatcher()
	stop()
	if got := workerFileWatcherParked(t); got != before {
		t.Errorf("an inert context parked %d watcher(s) (before=%d): a context that consumes no requests must install no goroutine",
			got-before, before)
	}
}

// TestRunTaskWorkerLeavesNoParkedFileWatcher is assertion ② — the LEAK SITE. The
// CLI lifecycle tests drive runTaskWorker directly, in-process; before the fix
// each such call left one watcher parked for the rest of the package run, and the
// close-out sweep reported it as a package-level fail with no test under it.
//
// The test drives the REAL face (not the installer), so it fails if a future
// caller forgets the deferred stop — the leak's actual re-entry path.
func TestRunTaskWorkerLeavesNoParkedFileWatcher(t *testing.T) {
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	// repo-context sandbox: the in-process driver runs the round's own-worktree
	// release under THIS process's cwd — the live repository — so its pin is the
	// only control (no child env vector exists to carry one).
	//
	// It is stated HERE as well as inside workerIdleSandbox for a measured
	// reason, not for symmetry: the guard (TestSpawnersPinTheRepoContext) judges
	// the SPAWNING function's own body, and "workerIdleSandbox delegates to the
	// pin" is a fact the guard cannot see. A site that reads as unpinned is the
	// one a later caller copies, so the pin is spelled at the site itself and the
	// helper's copy stays as the belt to this brace. See
	// repo_context_sandbox_test.go for the three inputs it pins.
	SandboxRepoContext(t)
	// The same hermetic single-node env + idle self-exit the sibling loop tests
	// use, so the call is a bounded number of drained rounds and RETURNS.
	workerIdleSandbox(t)
	resetWorkerStopState()
	t.Setenv(workerIdleExitsEnv, "1")

	before := workerFileWatcherParked(t)
	captureStdout(t, func() { runTaskWorker(false, "", nil) })
	if after := workerFileWatcherParked(t); after != before {
		t.Errorf("runTaskWorker returned with %d parked file watcher(s), want the %d that stood before the call — "+
			"the watcher's stop is not deferred, and the suite's close-out sweep turns this into a package-level fail "+
			"(frame %q)", after, before, workerFileWatcherFrame)
	}
}
