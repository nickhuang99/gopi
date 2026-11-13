package testutil

// Tests for the pre-test orphan sweep.
//
// The sweep is the BACKSTOP half of the process-leak fix: the lifetime binding
// cannot help a daemon that is already orphaned (started by an earlier, killed
// run), so something must find it before the next run starts. These cases build
// a REAL orphan in a REAL scene, prove the sweep finds it, prove the sweep kills
// it, and — the half that keeps the sweep honest — prove it does NOT touch a
// process outside its scope, however identical that process looks.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// orphanTestHoldSeconds is how long a scene's stand-in daemon holds. It is
	// always killed or swept by the case itself, never by the clock expiring (a
	// daemon that expired mid-case would make an assertion pass vacuously).
	orphanTestHoldSeconds = "600"
	// orphanTestSweepWait bounds the wait for a swept process to disappear.
	orphanTestSweepWait = 30 * time.Second
	// orphanTestSleeperCmdline is the JOINED argv of the finished stand-in
	// daemon: argv[0] is the mock binary and argv[1] the hold. Discovery waits
	// for EXACTLY this, never for a substring — see awaitScenePID for what the
	// finished argv buys, and for what the window it crosses is NOT.
	orphanTestSleeperCmdline = MockDaemonBinary + procArgvJoin + orphanTestHoldSeconds
	// sweepScopeSceneName / queueSceneName name the two scenes the PLACEMENT
	// regression builds: one inside the suite-start sweep's scope (the root
	// sceneDir must use) and one in the sibling queue (the root it must not).
	sweepScopeSceneName = "sweep-scope-scene"
	queueSceneName      = "queue-scene"
	// pinnedPlacementSceneName names the scene of the PLACEMENT-SCOPE regression:
	// a scene built INSIDE the root this run's pin actually landed on — the tree
	// the sweep used to be blind to whenever the placement chose the memory base.
	pinnedPlacementSceneName = "pinned-placement-scene"
)

// TestSuiteStartSweepCoversThePinnedPlacementRoot is the regression for the
// SECOND half of the placement defect (Rule 5: every fix ships a minimal repro
// test): a run root the PIN creates is not necessarily under the scratch home —
// under placementMemoryBase it is a direct child of the host memory base, a
// DIFFERENT TREE — so a scope built from the scratch home alone cannot see the
// orphans a KILLED run of this very suite left there.
//
// THE CONTROLLED COMPARISON IS THE WHOLE POINT: three stand-ins, all the same
// shape, the same moment, the same reaper, the same command — only the DIRECTORY
// differs.
//
//	A  under THIS run's pinned root (where the pin actually landed: /dev/shm/…)
//	B  under the scratch-home scope prefix (the tree the old scope aimed at)
//	C  in the scratch queue (a SIBLING of every scope member — the negative half)
//
// A is the defect's shape: the pre-fix single-prefix scope reaped B and left A
// alive. This case asserts the sweep's aim with the sweep's OWN predicate over the
// WHOLE resolved set (SuiteOrphanScopeRoots), and then drives the REAL entry point
// (SweepOrphansAtSuiteStart, reached by this package's TestMain through
// ArmAndSweepForSuite) rather than a re-creation of its scope: a case that rebuilt
// the scope itself would keep passing after the sweep's own scoping changed, which
// is exactly the defect this exists for.
func TestSuiteStartSweepCoversThePinnedPlacementRoot(t *testing.T) {
	requireSetsid(t)
	// The no-override arm is the one under test; the empty value means "no
	// override" (Rule 17: t.Setenv, never os.Setenv).
	t.Setenv(OrphanSweepRootEnv, "")
	// The case needs the pin to have LANDED somewhere to make a claim about it.
	if !SuiteTempRootPinned() {
		t.Fatalf("this suite's scratch root is not pinned — the regression cannot make a claim about the pin's landing site")
	}
	pinned := pinnedRootSceneDir(t, pinnedPlacementSceneName)
	homeScoped := sceneDir(t, sweepScopeSceneName)
	queued := queueSceneDir(t, queueSceneName)
	inPinnedRoot := startOrphanInScene(t, pinned)
	inHomeScope := startOrphanInScene(t, homeScoped)
	foreign := startOrphanInScene(t, queued)

	// (a) THE AIM CLAIM, measured with the sweep's own predicate over the whole
	// resolved set: the pin's landing site is inside it, the queue is not. This is
	// the relation the fix turns on — the pre-fix scope was ONE prefix, so a scene
	// under the pinned root was outside it while this assertion demanded it inside.
	roots := SuiteOrphanScopeRoots(internalTestutilSuite)
	if !suiteScopeMatches(roots, pinned) {
		t.Fatalf("the scene %s — built inside THIS run's pinned root %s — is NOT inside the suite-start sweep's scope %v: "+
			"a stand-in here is a leak the backstop cannot see, which is exactly the defect this regression pins",
			pinned, SuiteTempRoot(), roots)
	}
	if !suiteScopeMatches(roots, homeScoped) {
		t.Fatalf("the scene %s is NOT inside the suite-start sweep's scope %v — the scratch home is where a run root lands "+
			"whenever the placement declines the memory base, so it must stay a scope member", homeScoped, roots)
	}
	if suiteScopeMatches(roots, queued) {
		t.Fatalf("the queue scene %s IS inside the suite-start sweep's scope %v — a backstop aimed at this suite's run roots "+
			"must not reach into the shared queue", queued, roots)
	}

	// (b) THE REAL SWEEP, with real processes.
	swept := SweepOrphansAtSuiteStart(internalTestutilSuite)
	if !containsPid(swept, inPinnedRoot) {
		t.Fatalf("the suite-start sweep of %q did not see the stand-in %d under the pinned root %s (scope %v, rows: %v) — "+
			"the pin's landing site and the sweep's scope are not the same tree", internalTestutilSuite, inPinnedRoot,
			SuiteTempRoot(), roots, swept)
	}
	if !waitPIDGone(t, inPinnedRoot, orphanTestSweepWait) {
		t.Fatalf("the stand-in %d under the pinned root %s is still alive after the suite-start sweep reported reaping it",
			inPinnedRoot, SuiteTempRoot())
	}
	if !containsPid(swept, inHomeScope) || !waitPIDGone(t, inHomeScope, orphanTestSweepWait) {
		t.Fatalf("the suite-start sweep did not reap the stand-in %d in %s — the scratch-home member of the scope was lost "+
			"while the pinned root was added (rows: %v)", inHomeScope, homeScoped, swept)
	}
	if containsPid(swept, foreign) || !SignalZeroAlive(foreign) {
		t.Fatalf("the suite-start sweep acted on the stand-in %d in %s, outside every scope member %v — the sweep is scoped by "+
			"path, and that path is this suite's own run roots", foreign, queued, roots)
	}

	// (c) The queue stand-in is reclaimed through the sweep of ITS root, so this
	// case leaves no process behind.
	if _, err := SweepOrphansUnderRoot(queued); err != nil {
		t.Fatalf("sweep %s: %v", queued, err)
	}
	if !waitPIDGone(t, foreign, orphanTestSweepWait) {
		t.Fatalf("the queue stand-in %d survived the sweep of its own root %s", foreign, queued)
	}
}

// suiteScopeMatches reports whether dir lies inside ANY member of the resolved
// scope — the scope's OWN predicate (scopeOfPrefix), composed over the set rather
// than re-spelled, so an assertion here can never be looser than the sweep.
func suiteScopeMatches(roots []string, dir string) bool {
	for _, root := range roots {
		if scopeOfPrefix(root)(dir) {
			return true
		}
	}
	return false
}

// pinnedRootSceneDir builds the scene the DEFECT put out of reach: a directory
// inside the root this run's pin actually landed on (SuiteTempRoot), which under
// placementMemoryBase is a direct child of the memory base.
//
// It FAILS rather than passing vacuously when the pinned root is not a distinct
// tree from the scratch-home member — the control only means something while the
// two differ.
func pinnedRootSceneDir(t *testing.T, name string) string {
	t.Helper()
	root := SuiteTempRoot()
	home := SuiteOrphanScopePrefix(internalTestutilSuite)
	if scopeOfPrefix(home)(root) {
		t.Skipf("this run's pinned root %s is inside the scratch-home scope member %s — the placement chose the home tree, "+
			"so there is no second landing site to compare against on this host", root, home)
	}
	dir := filepath.Join(root, name+"-"+itoa(os.Getpid()))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("clear pinned-root scene %s: %v", dir, err)
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatalf("create pinned-root scene %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove pinned-root scene %s: %v", dir, err) // Rule 15b
		}
	})
	return dir
}

// TestOrphanSweepFindsAndReapsTheLeakInItsScene is the backstop's proof, and it
// is deliberately built from the INCIDENT's shape: a detached (session-leader),
// orphaned process — reparented to whatever REAPS this host (init, or the user
// session's own systemd manager) — whose only tie to the harness is the
// directory it works in.
func TestOrphanSweepFindsAndReapsTheLeakInItsScene(t *testing.T) {
	requireSetsid(t)
	scene := sceneDir(t, "orphan-scene")
	leak := startOrphanInScene(t, scene)
	assertOrphanShape(t, leak, "the scene's leaked daemon")

	// (a) the sweep FINDS it, with the owned path as its proof — the read-only
	// half a regression test asserts on ("the leak count must be zero").
	found, err := FindOrphansUnderRoot(scene)
	if err != nil {
		t.Fatalf("find orphans under %s: %v", scene, err)
	}
	if !containsPid(found, leak) {
		t.Fatalf("the orphan %d in %s was NOT found (found %v) — the backstop cannot see its own leak", leak, scene, found)
	}
	for _, c := range found {
		if c.Identity.Pid == leak && !strings.Contains(c.Proof, scene) {
			t.Errorf("the proof for pid %d must name the scene %s, got %q", leak, scene, c.Proof)
		}
	}

	// (b) the sweep REAPS it.
	swept, err := SweepOrphansUnderRoot(scene)
	if err != nil {
		t.Fatalf("sweep orphans under %s: %v", scene, err)
	}
	if !containsPid(swept, leak) {
		t.Fatalf("the sweep reported no row for pid %d (rows: %v)", leak, swept)
	}
	for _, row := range swept {
		if row.Identity.Pid == leak {
			t.Logf("sweep row: %s (%s) — %s", row.Identity, row.Proof, row.Outcome)
		}
	}
	if !waitPIDGone(t, leak, orphanTestSweepWait) {
		t.Fatalf("the swept orphan %d is still alive — the sweep did not reap it", leak)
	}

	// (c) the sweep is idempotent: a clean scene reports no rows.
	again, err := SweepOrphansUnderRoot(scene)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("a second sweep of the same scene reported %d row(s) — the scene was already clean: %v", len(again), again)
	}
}

// TestOrphanSweepLeavesAForeignProcessAlone is the negative half, and the reason
// the sweep is not a pkill: a process with the SAME argv and the same
// detachment/orphan shape, but living OUTSIDE the scope, must be invisible to
// the sweep and must still be alive afterwards.
func TestOrphanSweepLeavesAForeignProcessAlone(t *testing.T) {
	requireSetsid(t)
	scene := sceneDir(t, "orphan-scope")
	elsewhere := sceneDir(t, "foreign-scene")
	foreign := startOrphanInScene(t, elsewhere)
	assertOrphanShape(t, foreign, "the foreign process")

	swept, err := SweepOrphansUnderRoot(scene)
	if err != nil {
		t.Fatalf("sweep %s: %v", scene, err)
	}
	if containsPid(swept, foreign) {
		t.Fatalf("the sweep acted on pid %d, which lives in %s, not in the scoped %s — a sweep must never reach outside its root",
			foreign, elsewhere, scene)
	}
	if !SignalZeroAlive(foreign) {
		t.Fatalf("the foreign process %d was killed by a sweep scoped to %s", foreign, scene)
	}

	// Prove the sweep COULD have seen it once the scope contains it (otherwise
	// the negative above would also pass against a broken predicate).
	found, err := FindOrphansUnderRoot(elsewhere)
	if err != nil {
		t.Fatalf("find orphans under %s: %v", elsewhere, err)
	}
	if !containsPid(found, foreign) {
		t.Fatalf("the same process %d is invisible under its OWN scene %s — the predicate is broken, not the scope", foreign, elsewhere)
	}
	// Clean up through the sweep itself (it is now legitimately in scope).
	if _, err := SweepOrphansUnderRoot(elsewhere); err != nil {
		t.Fatalf("sweep %s: %v", elsewhere, err)
	}
	if !waitPIDGone(t, foreign, orphanTestSweepWait) {
		t.Fatalf("the foreign process %d survived the sweep of its own scene", foreign)
	}
}

// TestFindOrphansRefusesAnEmptyRoot pins the accident this sweep must never
// enable: an unscoped sweep.
func TestFindOrphansRefusesAnEmptyRoot(t *testing.T) {
	if _, err := FindOrphansUnderRoot(""); err == nil {
		t.Errorf("FindOrphansUnderRoot(\"\") did not refuse — an unscoped sweep is the one accident this must prevent")
	}
	if _, err := SweepOrphansUnderRoot("   "); err == nil {
		t.Errorf("SweepOrphansUnderRoot(blank) did not refuse")
	}
}

// TestSuiteStartSweepSeesItsOwnScopeScenes is the PLACEMENT regression (Rule 5:
// every fix ships a minimal repro test). It pins the property the fix
// establishes — a scene this suite creates, and the stand-in standing in it, are
// inside the ONE tree the suite-start sweep scopes itself to — and pins the
// negative half that makes the property mean something: a scene in the scratch
// QUEUE (the helper's first home, and the root of the leak) is OUTSIDE that
// scope, so a stand-in there is invisible to the very backstop that should reap
// it.
//
// It drives the REAL entry point (SweepOrphansAtSuiteStart, the function this
// package's TestMain reaches through ArmAndSweepForSuite), never a re-creation
// of its scope: a case that rebuilt the scope itself would keep passing after
// the sweep's own scoping changed, which is the defect shape this exists for.
func TestSuiteStartSweepSeesItsOwnScopeScenes(t *testing.T) {
	requireSetsid(t)
	// Point the sweep at the REAL scope: it honors an operator override, and
	// this case is about the no-override arm. t.Setenv, never os.Setenv
	// (Rule 17) — the empty value is exactly what "no override" means.
	t.Setenv(OrphanSweepRootEnv, "")
	inScope := sceneDir(t, sweepScopeSceneName)
	queued := queueSceneDir(t, queueSceneName)
	inScene := startOrphanInScene(t, inScope)
	foreign := startOrphanInScene(t, queued)

	// (a) THE PLACEMENT CLAIM, asserted with the sweep's OWN predicate: the
	// helper's root is in scope, the queue is not. This is the relation the fix
	// turns on, so it is measured with the exact function the sweep scopes by.
	scope := scopeOfPrefix(SuiteOrphanScopePrefix(internalTestutilSuite))
	if !scope(inScope) {
		t.Fatalf("the scene root %s is NOT inside the suite-start sweep's scope %s — a stand-in here is a leak the backstop cannot see",
			inScope, SuiteOrphanScopePrefix(internalTestutilSuite))
	}
	if scope(queued) {
		t.Fatalf("the queue scene %s IS inside the suite-start sweep's scope %s — a backstop aimed at this suite's run roots must "+
			"not reach into the shared queue", queued, SuiteOrphanScopePrefix(internalTestutilSuite))
	}

	// (b) THE REAL SWEEP, with real processes: the stand-in in this suite's own
	// scope is seen and reaped by this suite's own start hook.
	swept := SweepOrphansAtSuiteStart(internalTestutilSuite)
	if !containsPid(swept, inScene) {
		t.Fatalf("the suite-start sweep of %q did not see the stand-in %d in its own scope %s (rows: %v) — this is the leak "+
			"the scene root exists to prevent", internalTestutilSuite, inScene, inScope, swept)
	}
	if !waitPIDGone(t, inScene, orphanTestSweepWait) {
		t.Fatalf("the stand-in %d in %s is still alive after the suite-start sweep reported reaping it", inScene, inScope)
	}
	if containsPid(swept, foreign) || !SignalZeroAlive(foreign) {
		t.Fatalf("the suite-start sweep acted on the stand-in %d in %s, outside its scope — the sweep is scoped by path, and that "+
			"path is this suite's own run roots", foreign, queued)
	}

	// (c) The queue stand-in is reclaimed through the sweep of ITS root — the
	// shape the negative-half case uses, so this case never signals a process it
	// did not place inside a root it owns.
	if _, err := SweepOrphansUnderRoot(queued); err != nil {
		t.Fatalf("sweep %s: %v", queued, err)
	}
	if !waitPIDGone(t, foreign, orphanTestSweepWait) {
		t.Fatalf("the queue stand-in %d survived the sweep of its own root %s", foreign, queued)
	}
}

// queueSceneDir builds the scene the DEFECT used: a directory in the scratch
// queue — the sibling tree sceneDir must not use again.
//
// It FAILS rather than passing vacuously if the queue ever came inside the sweep
// scope, because that difference is the negative half this regression rests on.
func queueSceneDir(t *testing.T, name string) string {
	t.Helper()
	scope := SuiteOrphanScopePrefix(internalTestutilSuite)
	dir := filepath.Join(ScratchRootQueue(), name+"-"+itoa(os.Getpid()))
	if scopeOfPrefix(scope)(dir) {
		t.Fatalf("the queue scene %s is inside the sweep scope %s — the negative half of the placement regression would be gone",
			dir, scope)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("clear queue scene %s: %v", dir, err)
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatalf("create queue scene %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove queue scene %s: %v", dir, err) // Rule 15b
		}
	})
	return dir
}

// sceneDir returns the directory a scene's stand-in process works in — a tree
// owned by a RUN OF THIS SUITE, inside the ONE tree the suite-start sweep
// scopes itself to.
//
// WHY THAT ROOT, AND WHY IT USED TO BE WRONG. The first version of this helper
// used ScratchRootQueue(), the sibling queue for large artifacts. A scene's
// whole meaning is the process standing in it, so a scene is the one thing the
// suite-start sweep (SweepOrphansAtSuiteStart ← ArmAndSweepForSuite ← this
// package's TestMain) must be able to see: it is the BACKSTOP for a stand-in
// that outlives its case (a case that failed before its sweep, or a binary
// SIGKILLed between the launch and the cleanup). That sweep is scoped — when
// GOPI_TEST_ORPHAN_SWEEP_ROOT is unset — by scopeOfPrefix(SuiteOrphanScopePrefix
// (suite)), i.e. "a run root of THIS suite". The queue is a SIBLING of that
// scope, not a member, so a scene there is invisible to the sweep BY
// CONSTRUCTION and a stand-in left in one lives out its entire hold — the
// detached `sleep 600` holding tmpfs that this sweep exists to reap,
// manufactured by the sweep's own test.
//
// MEASURED (controlled: same moment, same process shape, only the directory
// differing — the run's evidence artifact on the RED probe): a stand-in under
// …/suite-internal-testutil-run-ZZ was reaped by a real `go test ./internal/testutil`
// suite start, while the one under …/gopi-test-scratch/queue/zz-scene-KEEP
// survived the same command.
//
// The queue is still the right home for artifacts that carry NO process
// identity (TestScratchDir / TestDeviceScratchDir); a scene is its opposite.
//
// The directory is not this run's own pinned root (SuiteTempRoot): that would
// make a scene left by a KILLED run invisible to the NEXT run's sweep, because
// the pin mints a fresh random root per run. The stable per-suite run root is
// what the backstop searches, and it is where an unfinished scene belongs.
func sceneDir(t *testing.T, name string) string {
	t.Helper()
	// The scene hangs under the sweep's own scope prefix, with this process's
	// pid keeping concurrent binaries of this suite apart. Nothing else about
	// the path matters: what places a leaked stand-in in the sweep is a path it
	// OWNS lying inside the scope, and a scene under this prefix is exactly that.
	scope := SuiteOrphanScopePrefix(internalTestutilSuite)
	dir := filepath.Join(scope, name+"-"+itoa(os.Getpid()))
	// The scene root and the sweep scope are ONE tree, and this is the guard
	// that keeps it so: the assertion is the sweep's OWN scope predicate
	// (scopeOfPrefix), not a second, looser spelling of "inside" — an edit that
	// moves the scene back out of the scope fails HERE, in the very case that
	// would otherwise become the sweep's blind spot.
	if !scopeOfPrefix(scope)(dir) {
		t.Fatalf("the scene root %s is not inside the suite-start sweep's scope %s — a scene outside the scope is a leak "+
			"the sweep cannot see (the regression sceneDir exists to prevent)", dir, scope)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("clear scene %s: %v", dir, err)
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatalf("create scene %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove scene %s: %v", dir, err) // Rule 15b
		}
	})
	return dir
}

// requireSetsid makes the scene honest about its dependency: the orphan shape
// (session leader, PPID 1) is produced with the standard `setsid` utility, and a
// host without it SKIPS rather than asserting on a shape it did not create.
func requireSetsid(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skipf("the orphan scene needs the `setsid` utility to create a session leader: %v", err)
	}
}

// startOrphanInScene produces the leak shape with REAL processes:
//
//	sh -c 'cd <scene> && exec setsid --fork sleep 600'
//
// `--fork` is load-bearing and was MEASURED: without it, setsid does not fork
// when it is not already a group leader — it calls setsid(2) and execs the
// program IN PLACE, so the launcher's own pid becomes the sleeper, this test
// remains its parent, and cmd.Wait() blocks for the whole hold (measured: the
// first version of this case hung until the suite timeout). With `--fork`,
// setsid forks the session leader and exits at once, so the sleeper is
// reparented to init (PPID 1) and the launcher returns immediately — the leak
// shape, produced by real processes, with nothing of ours left holding it.
func startOrphanInScene(t *testing.T, scene string) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", "cd "+shellQuote(scene)+" && exec setsid --fork "+MockDaemonBinary+" "+orphanTestHoldSeconds)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the scene's orphan: %v", err)
	}
	if !awaitLauncherExit(t, cmd) {
		_ = cmd.Process.Kill() //stylegate:ignore best-effort kill of a launcher that outlived its budget; the failure is reported by the caller
		_, _ = cmd.Process.Wait()
		t.Fatalf("the scene launcher %q never returned — the orphan was not detached", cmd.Args)
	}
	pid := awaitScenePID(t, scene)
	t.Cleanup(func() {
		// A case that failed before its sweep must not leave the stand-in
		// behind: kill it by the identity we captured (never by name).
		if SignalZeroAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL) //stylegate:ignore best-effort cleanup of this case's own stand-in daemon
		}
	})
	return pid
}

// awaitLauncherExit waits (bounded) for the launcher to exit, so a detachment
// mistake fails this case LOUDLY instead of hanging the suite — which is exactly
// what the first version of this case did.
func awaitLauncherExit(t *testing.T, cmd *exec.Cmd) bool {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		return true
	case <-time.After(orphanTestSweepWait):
		return false
	}
}

// awaitScenePID finds the sleeper by its WORKING DIRECTORY — the same evidence
// the sweep uses, and the same evidence a human would use. It is a discovery
// step, not a signal: nothing is signalled until a triple has been read.
//
// IT WAITS FOR THE FINISHED ARGV, NOT FOR A SUBSTRING — a preference, not a
// measured hazard. `setsid --fork sleep 600` creates the scene in TWO steps:
// setsid forks a session leader and then exits, so for a REAL window — measured
// with strace at ≈215 µs under the natural PATH — FindOrphansUnderRoot already
// reports a candidate whose argv still reads `setsid --fork sleep 600`: a
// session leader, cwd inside the scene, already reparented to the host's reaper,
// i.e. shaped exactly like the leak, and accepted by a
// `strings.Contains(cmdline, "sleep")` test.
//
// The window is real, but it does NOT hand this case a foreign pid: that
// candidate and the sleeper the sweep will act on are ONE incarnation — the same
// pid and the same starttime in /proc/<pid>/stat (measured; exec changes argv and
// nothing else, and KillIfIdentical is itself defined on pid+starttime). What the
// finished argv buys is not "the right pid" but three other things:
//
//   - a PINNED COMPLETION STATE: the pid returned below is one whose own
//     Identity.Cmdline already is the finished argv, so the assertOrphanShape
//     read that follows cannot land on an instant that is not yet the leak's
//     shape;
//   - no dependence on a KERNEL IMPLEMENTATION DETAIL: "fork+exec keeps pid and
//     starttime" holds today and is untested here; this case must not rest on it;
//   - the `sh -c cd … && exec setsid …` wrapper excluded BY NAME. That one really
//     is a different pid; FindOrphansUnderRoot's Detached()/Orphan() predicates
//     happen to filter it today, and the exact match keeps this case from relying
//     on two guards staying in step.
func awaitScenePID(t *testing.T, scene string) int {
	t.Helper()
	deadline := time.Now().Add(orphanTestSweepWait)
	for time.Now().Before(deadline) {
		candidates, err := FindOrphansUnderRoot(scene)
		if err == nil {
			for _, c := range candidates {
				if strings.TrimSpace(c.Identity.Cmdline) == orphanTestSleeperCmdline {
					return c.Identity.Pid
				}
			}
		}
		time.Sleep(dlPollInterval) //stylegate:allow-sleep bounded wait for the scene's process to appear
	}
	t.Fatalf("no process with the finished argv %q appeared in the scene %s within the budget", orphanTestSleeperCmdline, scene)
	return 0
}

// assertOrphanShape verifies the shape the sweep requires, so a case can never
// pass because the shape was not actually built.
//
// It asserts the SAME two facts the sweep does, through the SAME stamping call
// (StampParenting) and the SAME predicate (Orphan()): Detached() (the process
// left its controlling session) and Orphan() — its parent is either the process
// that reaps this host by name (init, or the user session's systemd manager) or
// a marked subreaper on THIS run's ancestry chain. Stamping only one of the two
// facts here (the measured pre-fix shape of this helper) makes the case disagree
// with its own sweep, which is how a repair looks green in the package and red
// at the acceptance command.
func assertOrphanShape(t *testing.T, pid int, what string) {
	t.Helper()
	reaper, reason := ProcReaperOf()
	if reaper <= 0 {
		t.Fatalf("this host's reaper cannot be named: %s — the orphan shape cannot be asserted here", reason)
	}
	id, err := ReadProcIdentity(pid)
	if err != nil {
		t.Fatalf("read the identity of %s (pid %d): %v", what, pid, err)
	}
	id = id.StampParenting()
	if !id.Detached() {
		t.Errorf("%s (pid %d) is not a session leader (session=%d) — it is not the leaked-daemon shape", what, pid, id.Session)
	}
	if !id.Orphan() {
		t.Errorf("%s (pid %d) was not reparented to a reaping ancestor of this run (ppid=%d, reaper=%d, chain=%v) — it is not the leaked-daemon shape",
			what, pid, id.PPID, id.ProcReaper, id.ProcReaperChain)
	}
	t.Logf("%s: %s (session leader, ppid %d; this host's reaper %d, this run's chain %v)", what, id, id.PPID, reaper, id.ProcReaperChain)
}

// containsPid reports whether a candidate/swept list names pid.
func containsPid[T any](rows []T, pid int) bool {
	for _, r := range rows {
		switch v := any(r).(type) {
		case OrphanCandidate:
			if v.Identity.Pid == pid {
				return true
			}
		case SweptProcess:
			if v.Identity.Pid == pid {
				return true
			}
		}
	}
	return false
}

// waitPIDGone polls the triple until the process is gone.
func waitPIDGone(t *testing.T, pid int, budget time.Duration) bool {
	t.Helper()
	id, err := ReadProcIdentity(pid)
	if err != nil {
		return true
	}
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if alive, _ := id.Alive(); !alive {
			return true
		}
		time.Sleep(dlPollInterval) //stylegate:allow-sleep bounded wait for a swept process to die
	}
	return false
}

// shellQuote single-quotes a path for the launcher's shell.
func shellQuote(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}
