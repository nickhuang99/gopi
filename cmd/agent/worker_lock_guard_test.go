// worker_lock_guard_test.go — the regression guard for the WORKER daemon's
// per-round lock-identity check (workerDaemonGuardIdentity).
//
// The hole it closes, measured 2026-09-17 (see devdoc/serve-lock-acquire-race.md):
// the merger has re-proved, once per tick, that the flock it holds is still the
// file serve.lock NAMES (ensureServeLockIdentity, since the 2026-09-12
// double-merge incident). The worker — the other resident face, holding its own
// worker.lock through the SAME machinery — only ever checked identity ONCE, at
// acquisition, and acquired its daemon lock on the SPAWNING parent before the
// child re-took it. So `rm worker.lock` (or any unlink+recreate) left a resident
// worker locking an inode no name points at while the PATH carried a fresh inode
// whose lock is free: the next `gopi worker --daemon` acquires it, and two
// resident workers sweep the same queue — the exact double-work the lock exists
// to prevent, and a state the daemon could not see by looking at its own
// descriptor (a flock survives its path being replaced: TestServeLockReplacementMechanism).
//
// RED PROOFS (Rule 5) — the two halves are proven separately, because they
// answer different questions and a single mutant could not cover both:
//
//	① the DECISION (repair vs stop, and the guard actually firing) — red-proven
//	   by breaking workerDaemonGuardIdentity's body: with the identity check
//	   stubbed to "always held" neither case ever sees a guard line, and with it
//	   stubbed to "always stop" the repair case never restores (both executed,
//	   see devdoc/serve-lock-acquire-race.md).
//	② the WIRING (the decision is reached once per ROUND, before any work) —
//	   red-proven by deleting the call from runTaskWorkerLoop; that is what
//	   TestWorkerDaemonGuardIsWiredIntoTheRoundLoop asserts, from the source.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

const (
	// workerGuardDaemonHoldSec is how long the guard daemon's round is held
	// open (bounded; the test kills it by pid) — the helper sleeps between
	// publishing the lock pid and its first round.
	workerGuardDaemonHoldSec = 60
	// workerGuardReadyTimeout / workerGuardReadyTick bound the readiness
	// handshake with the daemon helper.
	workerGuardReadyTimeout = 15 * time.Second
	workerGuardReadyTick    = 25 * time.Millisecond
	// workerGuardLineTimeout bounds the wait for the daemon's guard line: the
	// line is printed at the TOP of a round, so this covers one round boundary
	// and nothing else.
	workerGuardLineTimeout = 15 * time.Second
	// workerGuardStoppedMarkerName is the marker the helper writes when it takes
	// the STOP branch (another process owns the fresh lock).
	workerGuardStoppedMarkerName = "guard.stopped"
)

// The daemon helper's env contract (a normal `go test` run sees no worker-lock
// path and skips the body).
const (
	workerGuardDaemonLockEnv   = "GOPI_TEST_WORKER_GUARD_LOCK"
	workerGuardDaemonReadyEnv  = "GOPI_TEST_WORKER_GUARD_READY"
	workerGuardDaemonHoldEnv   = "GOPI_TEST_WORKER_GUARD_HOLD_SEC"
	workerGuardReplacedMarker  = "GOPI_TEST_WORKER_GUARD_REPLACED"
	workerGuardRestoredMarker  = "GOPI_TEST_WORKER_GUARD_RESTORED"
	workerGuardStoppedMarker   = "GOPI_TEST_WORKER_GUARD_STOPPED"
	workerGuardReplaceAfterEnv = "GOPI_TEST_WORKER_GUARD_REPLACE_AFTER_SEC"
)

// TestWorkerDaemonGuard_RepairsAReplacedLock: a resident worker whose
// worker.lock is replaced underneath it re-acquires the current path, so the
// single-instance guard is TRUE again (this daemon and only this daemon).
func TestWorkerDaemonGuard_RepairsAReplacedLock(t *testing.T) {
	path, ready, replaced, restored := workerGuardRig(t, "")
	daemon := workerGuardStartDaemon(t, path, ready, replaced, restored, "")
	workerGuardWaitFile(t, ready, "the daemon reports it holds worker.lock")

	// The incident's mechanism, injected deterministically: the path is
	// unlinked and recreated under the daemon. `os.Remove` needs no privilege
	// and no cooperation, and no flock syscall is invalidated by it.
	workerGuardReplaceLock(t, path, replaced)

	workerGuardWaitFile(t, restored, "the daemon repaired the replaced lock")
	if got := workerGuardLockPID(t, path); got != daemon.Pid {
		t.Fatalf("%s names pid %d after the repair, want the daemon %d — the guard points at the wrong process", serveLockFilename, got, daemon.Pid)
	}
	// The repaired lock is REALLY held: a second acquisition is refused.
	if _, err := acquireWorkerLock(); err == nil {
		t.Error("acquireWorkerLock succeeded after the repair — the daemon's re-acquired guard does not hold")
	}
}

// TestWorkerDaemonGuard_StopsWhenTheFreshLockIsTaken: the daemon must NOT
// continue unguarded — when another process already owns the fresh lock file,
// the daemon stops instead of sweeping beside it.
func TestWorkerDaemonGuard_StopsWhenTheFreshLockIsTaken(t *testing.T) {
	path, ready, replaced, restored := workerGuardRig(t, "")
	daemon := workerGuardStartDaemon(t, path, ready, replaced, restored, "")
	workerGuardWaitFile(t, ready, "the daemon reports it holds worker.lock")

	// The contender: a REAL second holder of the fresh file (this process),
	// created by replacing the path and taking its flock — the exact state a
	// second `gopi worker --daemon` would produce.
	workerGuardReplaceLock(t, path, replaced)
	fresh, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("contender open: %v", err)
	}
	defer fresh.Close() //stylegate:ignore test teardown of the contender's descriptor; the assertions have run
	if err := syscall.Flock(int(fresh.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("contender flock on the fresh file: %v", err)
	}

	// The daemon must take the STOP branch — not the repair one, and not
	// "keep going": the marker it writes is the production decision, and the
	// process must be gone with it.
	workerGuardWaitFile(t, workerGuardStoppedMarkerPath(path), "the daemon took the stop branch for the replaced lock")
	if !workerGuardWaitExit(t, daemon) {
		t.Fatalf("the daemon pid %d kept running while another process held the fresh worker.lock — it would sweep the queue beside that process", daemon.Pid)
	}
}

// TestWorkerDaemonGuardHelperProcess is not a test: it is the resident-daemon
// body the rig drives (it skips unless the lock env is set).
//
// It plays a resident worker faithfully at the level the guard cares about:
// open + flock worker.lock, stamp the pid, then enter a ROUND LOOP whose first
// act is workerDaemonGuardIdentity() — the production call the regression is
// about — publishing its outcome into marker files instead of sweeping a queue.
func TestWorkerDaemonGuardHelperProcess(t *testing.T) {
	path := os.Getenv(workerGuardDaemonLockEnv)
	if path == "" {
		t.Skip("helper process body — driven by the worker-daemon guard tests")
	}
	// The helper re-execs THIS test binary, so its TestMain re-pins the suite's
	// profile and would resolve worker.lock somewhere else entirely — the guard
	// must compare the SAME path the fixture replaced. The profile is therefore
	// re-pinned HERE, from the path the fixture handed over, and the rig proves
	// the production resolver agrees before anything is asserted.
	// t.Setenv, not os.Setenv (Rule 17): the pin is per-TEST state and the
	// framework restores it — the helper runs inside a test function, and the
	// resolution below happens while the pin is in force.
	t.Setenv(config.GopiDirEnv, filepath.Dir(filepath.Dir(path)))
	resolved, rerr := workerLockPath()
	if rerr != nil {
		t.Fatalf("helper resolve workerLockPath: %v", rerr)
	}
	if resolved != path {
		t.Fatalf("helper resolves the lock to %s, but the fixture replaced %s — the rig would prove nothing", resolved, path)
	}
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("helper open %s: %v", path, err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("helper flock %s: %v", path, err)
	}
	if err := lock.Truncate(0); err == nil {
		_, _ = lock.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0) //stylegate:ignore writes a fixture lock payload; the lock state read afterwards is the assertion
	}
	workerDaemonLockRef = lock
	if err := os.WriteFile(os.Getenv(workerGuardDaemonReadyEnv), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatalf("helper ready marker: %v", err)
	}
	holdSec, _ := strconv.Atoi(os.Getenv(workerGuardDaemonHoldEnv)) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
	if holdSec <= 0 {
		holdSec = workerGuardDaemonHoldSec
	}
	// The wait is what lets the test replace the lock BEFORE the first round
	// boundary — the daemon's own round loop below is the production shape.
	time.Sleep(time.Duration(holdSec) * time.Second) //stylegate:allow-sleep the fixture holds the pre-round window open so the replacement lands before the first guard
	for round := 0; round < 5; round++ {
		if workerDaemonGuardIdentity() {
			// The production STOP: the fresh lock belongs to someone else. The
			// daemon face leaves the process here (runTaskWorkerWithDaemon does
			// not return to a wider process), so the fixture exits too — a test
			// binary that lingered in its own teardown would look "still running"
			// to the assertion below.
			_ = os.WriteFile(os.Getenv(workerGuardStoppedMarker), []byte("stopped"), 0o644) //stylegate:ignore writes a fixture file the test then reads
			os.Exit(0)
		}
		//stylegate:ignore path-identity probe in a fixture; the boolean is what the test pins
		if ok, _ := sameFileAsPath(workerDaemonLockRef, path); ok {
			_ = os.WriteFile(os.Getenv(workerGuardRestoredMarker), []byte("restored"), 0o644) //stylegate:ignore writes a fixture file the test then reads
		}
		time.Sleep(workerGuardReadyTick) //stylegate:allow-sleep round tick of the fixture's loop
	}
}

// workerGuardRig pins a private profile and returns the lock path plus the
// three marker paths the helper publishes into.
func workerGuardRig(t *testing.T, _ string) (path, ready, replaced, restored string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)            // the guard resolves ~/.gopi/worker.lock
	t.Setenv(config.GopiDirEnv, dir) // …and GOPI_HOME outranks HOME, so pin it too
	gopiDir := filepath.Join(dir, ".gopi")
	if err := os.MkdirAll(gopiDir, serveLockDirMode); err != nil {
		t.Fatalf("create %s: %v", gopiDir, err)
	}
	return filepath.Join(gopiDir, workerLockFilename),
		filepath.Join(dir, "daemon.ready"),
		filepath.Join(dir, "lock.replaced"),
		filepath.Join(dir, "guard.restored")
}

// workerGuardStoppedMarkerPath is where the helper records the STOP branch.
func workerGuardStoppedMarkerPath(lockPath string) string {
	return filepath.Join(filepath.Dir(lockPath), workerGuardStoppedMarkerName)
}

// workerGuardStartDaemon starts the resident-daemon helper in the test's own
// pinned profile and returns its process (killed by t.Cleanup).
func workerGuardStartDaemon(t *testing.T, path, ready, replaced, restored, _ string) *os.Process {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestWorkerDaemonGuardHelperProcess")
	cmd.Env = append(os.Environ(),
		workerGuardDaemonLockEnv+"="+path,
		workerGuardDaemonReadyEnv+"="+ready,
		workerGuardReplacedMarker+"="+replaced,
		workerGuardRestoredMarker+"="+restored,
		workerGuardStoppedMarker+"="+workerGuardStoppedMarkerPath(path),
		workerGuardDaemonHoldEnv+"=3",
	)
	logFile, lerr := os.Create(filepath.Join(filepath.Dir(path), "daemon.helper.log"))
	if lerr != nil {
		t.Fatalf("helper log: %v", lerr)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the daemon helper: %v", err)
	}
	t.Cleanup(func() {
		_ = logFile.Close() //stylegate:ignore closes the fixture's log file
		if b, rerr := os.ReadFile(logFile.Name()); rerr == nil && t.Failed() {
			t.Logf("helper log (%s):\n%s", logFile.Name(), b)
		}
	})
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //stylegate:ignore teardown of an already-finished helper; the assertions have run
		_, _ = cmd.Process.Wait()
	})
	return cmd.Process
}

// workerGuardWaitFile blocks until the marker exists (bounded, never a bare
// sleep).
func workerGuardWaitFile(t *testing.T, path, what string) {
	t.Helper()
	deadline := time.Now().Add(workerGuardReadyTimeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %s never appeared at %s within %s", what, what, path, workerGuardReadyTimeout)
		}
		time.Sleep(workerGuardReadyTick) //stylegate:allow-sleep poll tick of the helper's marker handshake
	}
}

// workerGuardWaitExit reports whether the process is gone within the timeout.
func workerGuardWaitExit(t *testing.T, p *os.Process) bool {
	t.Helper()
	deadline := time.Now().Add(workerGuardLineTimeout)
	for {
		// mdProcAlive, not a bare signal 0: a process that has exited but not
		// been reaped is a ZOMBIE, and a zombie must not read as "still running"
		// (the same reason mdProcAlive exists for the daemon E2Es).
		if !mdProcAlive(p.Pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(workerGuardReadyTick) //stylegate:allow-sleep bounded poll for the helper's exit
	}
}

// workerGuardReplaceLock replaces the lock file under the daemon and marks that
// the replacement happened (the ordering fact the assertions depend on).
func workerGuardReplaceLock(t *testing.T, path, marker string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("unlink the held lock: %v", err)
	}
	if err := os.WriteFile(path, []byte("0000000"), serveLockFileMode); err != nil {
		t.Fatalf("recreate the lock path: %v", err)
	}
	if err := os.WriteFile(marker, []byte("replaced"), 0o644); err != nil {
		t.Fatalf("replacement marker: %v", err)
	}
}

// workerGuardLockPID reads the pid stamped into the lock file ("" when absent).
func workerGuardLockPID(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("%s does not carry a pid (%q): %v", path, strings.TrimSpace(string(b)), err)
	}
	return pid
}

// workerGuardSummary is a debug aid used in failure messages.
func workerGuardSummary(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("(%s: %v)", path, err)
	}
	return fmt.Sprintf("(%s: mode %s size %d)", path, fi.Mode(), fi.Size())
}

// TestWorkerDaemonGuardIsWiredIntoTheRoundLoop pins the WIRING half: the guard
// must be reached ONCE PER ROUND and BEFORE the round does any work, in the
// production loop. The behavioural cases above drive workerDaemonGuardIdentity
// directly (so they prove the decision), and no mutant of the decision can show
// whether the loop ever calls it — this case is that half, asserted on the
// source the binary is built from.
//
// RED PROOF: delete the `if workerDaemonGuardIdentity() { return }` block from
// runTaskWorkerLoop and this test fails with the missing-call message.
func TestWorkerDaemonGuardIsWiredIntoTheRoundLoop(t *testing.T) {
	src, err := os.ReadFile(workerDaemonLoopSourcePath(t))
	if err != nil {
		t.Fatalf("read %s: %v", workerDaemonLoopSourcePath(t), err)
	}
	body := workerGuardLoopBody(string(src))
	if body == "" {
		t.Fatalf("could not find %s in %s — was the loop renamed? (update this test deliberately)", workerDaemonLoopFunc, workerDaemonLoopSourceFile)
	}
	guardAt := strings.Index(body, workerGuardCallSpelling)
	if guardAt < 0 {
		t.Fatalf("%s does not call %s — the worker daemon would keep sweeping after its lock file was replaced, which is the hole this fix closes",
			workerDaemonLoopFunc, workerGuardCallSpelling)
	}
	roundAt := strings.Index(body, workerRoundCallSpelling)
	if roundAt < 0 {
		t.Fatalf("%s no longer calls %s — this test's ordering claim cannot be checked", workerDaemonLoopFunc, workerRoundCallSpelling)
	}
	if guardAt > roundAt {
		t.Errorf("the lock-identity guard runs AFTER the round's first work (%s at %d, %s at %d) — a daemon must never do a round's work before proving its guard",
			workerGuardCallSpelling, guardAt, workerRoundCallSpelling, roundAt)
	}
	if !strings.Contains(body[guardAt:], workerGuardStopSpelling) {
		t.Errorf("the guard's call site does not STOP when it reports the guard is lost (no %q) — the daemon would continue unguarded", workerGuardStopSpelling)
	}
}

// workerGuardLoopBody returns the body of workerDaemonLoopFunc (from its
// declaration to the next top-level func), or "" when the function is absent.
func workerGuardLoopBody(src string) string {
	start := strings.Index(src, "func "+workerDaemonLoopFunc+"(")
	if start < 0 {
		return ""
	}
	rest := src[start:]
	if end := strings.Index(rest[1:], "\nfunc "); end >= 0 {
		return rest[:end+1]
	}
	return rest
}

const (
	// workerDaemonLoopSourceFile is the file that owns the round loop, and
	// workerDaemonLoopFunc is the loop itself — named so a rename fails this
	// test loudly instead of silently un-gating the wiring.
	workerDaemonLoopSourceFile = "cli_tasks_worker.go"
	workerDaemonLoopFunc       = "runTaskWorkerLoop"
	// The call spellings the wiring is asserted on (kept beside the test so a
	// deliberate rename is a one-line change here).
	workerGuardCallSpelling = "workerDaemonGuardIdentity()"
	// workerRoundCallSpelling is the round's first work in the CURRENT master
	// shape of runTaskWorkerLoop. The tag this test came from spelled it
	// `workerRound()`; master renamed it `workerRoundClaiming()`. The constant
	// exists to make that a one-line change here rather than a silent un-gate.
	workerRoundCallSpelling = "workerRoundClaiming()"
	workerGuardStopSpelling = "return"
)

// workerDaemonLoopSourcePath resolves the loop's source file from THIS test
// file's own location (runtime.Caller) rather than from the process cwd. Every
// other source-scanning gate in this package does the same: a sibling test in
// the same binary is free to move the process cwd (cli_tasks_writeleak_guard_test.go
// packageTestDir documents the identical hazard), and when it does, a relative
// read fails with "no such file or directory" and the gate reads RED for a
// reason that has nothing to do with the code it guards.
func workerDaemonLoopSourcePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not resolve this test file's path — cannot locate " + workerDaemonLoopSourceFile)
	}
	return filepath.Join(filepath.Dir(file), workerDaemonLoopSourceFile)
}
