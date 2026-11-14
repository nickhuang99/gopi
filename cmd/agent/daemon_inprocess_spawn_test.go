package main

// daemon_inprocess_spawn_test.go — the in-process daemon start from inside
// `go test` (epic daemon-inprocess-spawn, 2026-10-04).
//
// THE DEFECT THIS PINS, measured before the fix on task 01a10742: a direct
// in-process `workerDaemonSpawnErr(nil)` from `go test ./cmd/agent`, with the
// cgroup escape on rung ① (systemd-run --user --scope), returned after
// 30.02s with "daemon did not report readiness within 30s".
//
// THE ROOT CAUSE, and it is NOT the readiness byte. The seam re-exec'd
// os.Executable() — which inside `go test` is `<pkg>.test`, not the CLI. The
// spawned child therefore ran Go's generated test main instead of the daemon
// face: measured pid 4091424, whose cmdline was
// `<builddir>/b001/agent.test worker`, whose fd 3 → worker.lock and fd 4 →
// `pipe:[55270799]` (so the ExtraFiles descriptors ARE inherited intact across
// the systemd-run wrapper — the briefing's second hypothesis, FALSIFIED by this
// same observation), whose cgroup sat inside a fresh scope unit, and whose
// daemon log held the cmd/agent SUITE's own output. The suite ran to the end of
// the package, so workerDaemonNoteReady was never reached and NOBODY ever wrote
// a byte: the parent was not losing it, it never had a writer.
//
// WHAT THE FIX DOES (cli_daemon_spawn.go, daemonChildExecutable): the re-exec
// target is resolved from the launcher convention (runcmd.EnvBinary) first, and
// a Go test binary is REFUSED immediate-and-by-name instead of being spawned.
// So the surface has two honest outcomes, and this file measures BOTH:
//
//	ARM A — no binary published: the start FAILS FAST with the cause named,
//	        in a fraction of daemonHandshakeWait (the bound it must never burn).
//	ARM B — the real binary published: the in-process start COMPLETES, and the
//	        daemon it started is observable (live pid + the loop's own banner in
//	        its log), then stopped by EXPLICIT pid (never a pattern: the operator
//	        has live gopi daemons on this host).
//
// Both arms call the PRODUCTION entry point the restart path calls
// (workerDaemonSpawnErr), never a copy of its logic (Rule 31).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/worktreerelease"
)

const (
	// irdFastRefusalCeiling bounds ARM A. It is a small FRACTION of the
	// production handshake bound on purpose: the arm asserts "this fails fast",
	// and a ceiling equal to the bound it excludes would pass by accident, which
	// is exactly how the pre-fix 30.02s read as an ordinary timeout.
	irdFastRefusalCeiling = daemonHandshakeWait / 10
	// irdDaemonStopWait bounds the explicit-pid stop at the end of ARM B.
	irdDaemonStopWait = 20 * time.Second
	// irdStartWait bounds ARM B's start. It sits ABOVE the production handshake
	// bound: the arm asserts the start really completes, and a shorter window
	// would report its own impatience as a product defect (Rule 38 — a bound
	// names what it measures).
	irdStartWait = daemonHandshakeWait + irdDaemonStopWait
	// irdIdleExits / irdIdleSec keep the daemon these arms start short-lived by
	// the LOOP'S OWN knobs (never a test-only bypass), so nothing is left
	// running when the case ends.
	irdIdleExits = "1"
	irdIdleSec   = "1"
	// irdTestBinaryCause is the distinguishing phrase of the refusal. It is the
	// clause that names WHY a test-binary re-exec cannot work, so a regression
	// that reverted to a bare timeout could not satisfy it.
	irdTestBinaryCause = "Go test binary"
	// irdWorkerLoopBanner is the loop's own first line — the proof that a REAL
	// worker loop (not a test SUITE) ran in the child. It is the owning
	// package's constant, referenced, never re-typed (Rules 13/14): a local copy
	// would silently stop matching the banner the loop actually prints.
	irdWorkerLoopBanner = procname.WorkerLoopStartLine
)

// irdProfile pins a private profile for the whole arm: the production code
// resolves GOPI_HOME/GOPI_SETTINGS itself, and this is the surface under test.
// SandboxRepoContext is required beside it because ARM B really starts a worker
// (repo_context_sandbox_guard_test.go judges the SPAWNING function).
func irdProfile(t *testing.T) string {
	t.Helper()
	SandboxRepoContext(t)
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_SETTINGS", wdSettings(t, home))
	// The ledger is pinned INSIDE the private profile (the path the resolver
	// computes from GOPI_HOME), so the daemon these arms start writes rows to the
	// throwaway home and never to the operator's append-only ledger (Rule 32).
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, config.ProfileDirName, "memory.db"))
	t.Setenv(workerIdleExitsEnv, irdIdleExits)
	t.Setenv(workerIdleIntervalEnv, irdIdleSec)
	wdClearAgentSignature(t)
	return home
}

// TestInProcessDaemonStartRefusesATestBinaryReExec is ARM A: with no binary
// published, the in-process start must fail FAST and NAME the cause instead of
// waiting out daemonHandshakeWait.
//
// RED on the pre-fix tree: the call returns after ~30s with "daemon did not
// report readiness within 30s" — a transport symptom, not a cause — and this
// arm fails on BOTH the elapsed ceiling and the wording.
func TestInProcessDaemonStartRefusesATestBinaryReExec(t *testing.T) {
	irdProfile(t)
	// The arm's premise, asserted rather than assumed: the process's own
	// executable must BE the test binary, or the refusal under test is not the
	// one being exercised (worktreerelease.TestProcessExecutableSuffix is the
	// ONE spelling — referenced, never re-typed).
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve this process's executable: %v", err)
	}
	if !strings.HasSuffix(self, worktreerelease.TestProcessExecutableSuffix) {
		t.Skipf("this process's executable %q does not look like a compiled test binary — the arm's premise does not hold here", self)
	}

	start := time.Now()
	reused, spawnErr := workerDaemonSpawnErr(nil)
	elapsed := time.Since(start)

	if reused {
		t.Fatalf("the start reported REUSE from a test binary — nothing was started, so nothing could have been reused")
	}
	if spawnErr == nil {
		t.Fatalf("an in-process daemon start from a test binary was ACCEPTED (took %s) — it would run the test suite "+
			"in the child and never report readiness", elapsed)
	}
	if elapsed >= irdFastRefusalCeiling {
		t.Errorf("the refusal took %s (ceiling %s = %s/%d): the start burned the handshake bound instead of "+
			"failing fast — that burn IS the defect (Rule 38: the bound was reported as the diagnosis)",
			elapsed, irdFastRefusalCeiling, "daemonHandshakeWait", daemonHandshakeWait/irdFastRefusalCeiling)
	}
	if !strings.Contains(spawnErr.Error(), irdTestBinaryCause) {
		t.Errorf("the refusal must NAME the cause (%q), got: %v", irdTestBinaryCause, spawnErr)
	}
	// The remedy must travel with the refusal (Rule 15: a refusal names its
	// correction) — the variable is the product constant, not a re-typed literal.
	if !strings.Contains(spawnErr.Error(), runcmd.EnvBinary) {
		t.Errorf("the refusal must name the escape hatch %s so the caller can act on it, got: %v", runcmd.EnvBinary, spawnErr)
	}
	t.Logf("[arm A] refused in %s (bound %s): %v", elapsed, daemonHandshakeWait, spawnErr)
}

// TestInProcessDaemonStartCompletesWhenTheRealBinaryIsPublished is ARM B: with
// the launcher convention pointing at the real gopi binary, the SAME in-process
// call COMPLETES — the end state the epic asks for ("the in-process call is
// usable").
//
// This is also the arm that proves the fix is a refusal of the wrong TARGET and
// not a blanket refusal of the in-process case: nothing else about the call
// changes, only which binary gets exec'd.
func TestInProcessDaemonStartCompletesWhenTheRealBinaryIsPublished(t *testing.T) {
	// THE BINARY IS RESOLVED FIRST, before the repo-context pin moves this
	// process's cwd: gopiTestBinPath builds the package with `go build .`, which
	// needs to stand in the package directory (it cannot run from the sandbox the
	// pin installs).
	bin := gopiTestBinPath(t)
	irdProfile(t)
	// The launcher convention, referenced through its owning package (Rules
	// 13/14) — this is the same variable a launcher exports for its children.
	t.Setenv(runcmd.EnvBinary, bin)

	start := time.Now()
	reused, spawnErr := workerDaemonSpawnErr(nil)
	elapsed := time.Since(start)
	if spawnErr != nil {
		t.Fatalf("an in-process daemon start with %s=%s must COMPLETE, got after %s: %v",
			runcmd.EnvBinary, bin, elapsed, spawnErr)
	}
	if reused {
		t.Fatalf("the start reported REUSE — nothing was detached, so this arm proved nothing about an in-process start")
	}
	if elapsed >= irdStartWait {
		t.Fatalf("the start took %s (budget %s)", elapsed, irdStartWait)
	}
	t.Logf("[arm B] in-process start completed in %s", elapsed)

	// The daemon must be a REAL worker daemon: its pid alive, and the loop
	// banner (not a test-suite transcript) in its own log. Both are read from
	// the state/log the production surfaces own — never from a fixture's guess.
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve the worker state dir: %v", err)
	}
	pid := wdWaitFreshDaemon(t, dir)
	t.Cleanup(func() { wdStopPID(t, pid) })

	logPath, err := workerDaemonLogPath()
	if err != nil {
		t.Fatalf("resolve the daemon log path: %v", err)
	}
	if !irdWaitLogContains(t, logPath, irdWorkerLoopBanner, wdDaemonWait) {
		t.Fatalf("the started daemon's own log %s never carried the worker loop banner %q — the child did not run the daemon face",
			logPath, irdWorkerLoopBanner)
	}
	t.Cleanup(func() { wdStopPID(t, pid) })
	t.Logf("[arm B] daemon pid %d published state and wrote %q into %s", pid, irdWorkerLoopBanner, logPath)
}

// irdWaitLogContains waits (bounded) for a log file to contain want.
func irdWaitLogContains(t *testing.T, path, want string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		blob, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(blob), want) {
			return true
		}
		time.Sleep(wdPollInterval) //stylegate:allow-sleep bounded wait for the daemon's first log line
	}
	return false
}

// irdLogEvidence renders what the daemon log actually held — or WHY it could not
// be read. An unreadable log must never be reported as one that "held nothing"
// (Rule 15b: a swallowed error hides the failure, and every layer of the fix for
// the original defect was exactly a message that named a transport symptom
// instead of a cause).
func irdLogEvidence(path string) string {
	blob, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("--- %s could not be read: %v ---", path, err)
	}
	if len(blob) == 0 {
		return fmt.Sprintf("--- %s is EMPTY: the child wrote nothing ---", path)
	}
	return fmt.Sprintf("--- %s (tail) ---\n%s", path, string(blob))
}
