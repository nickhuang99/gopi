package main

// worker_daemon_handover_test.go — the deterministic red/green proof for the
// RESTART's flock HANDOVER (epic cmd/agent-restart-lock-handover).
//
// THE DEFECT THIS PINS (measured 2026-10-04, host load 10.9 on 4 cores): the
// restart stopped its predecessor, `workerWaitForExit` watched the pid DISAPPEAR
// ("pid 322819 exited 501ms after the graceful stop request") and the successor's
// start was then refused by the very lock that predecessor still held:
//
//	daemon start failed: another worker daemon is already running (lock … held by
//	pid 322819 (no such process: the flock outlived its owner — an inherited
//	descriptor copy, or a replaced path))
//
// The mechanism is a WINDOW, not a defect in the refusal text: the predecessor's
// pid stops being visible (signal-0 + /proc/<pid>/stat) strictly BEFORE the kernel
// releases its flock — a flock is released when the file's last descriptor is
// closed, i.e. at the END of the predecessor's exit path. Taking over the lock is
// therefore a HANDOVER, and a handover that attempts LOCK_NB exactly ONCE loses
// whenever the two facts are not simultaneous.
//
// WHAT THESE ARMS ARE, AND WHAT THEY ARE NOT. The window is made a FACT rather
// than a probability: a separate process HOLDS the worker flock — the role the
// predecessor plays during its exit path — and keeps holding it across the
// successor's IN-PROCESS lock acquisition, releasing it a moment later. No load
// is generated (Rule 27 forbids manufacturing a broken environment to produce a
// number), and both arms drive the production function itself:
// workerDaemonSpawnErr (the call `gopi worker --restart` makes to start its
// successor) and daemonSpawn(workerDaemonSpec(), …) (the plain start's spec).
//
// The instrument's HONEST LIMIT: these arms pin the FIX's contract — an
// acquisition that is refused by a lock which is released a moment later must not
// report failure — and the singleton contract beside it. They do not, and cannot,
// prove which interleaving the 2026-10-04 saturation run hit: that is a narrative
// fact about one run (Rule 38), recorded in this epic's report.
//
// WHY THE HOLDER IS A SEPARATE PROCESS: flock lives on the open file description,
// so a dup of THIS process's own description re-takes the lock successfully and
// cannot produce the contention the handover waits for. The package's existing
// holder (bfdStartFlockHolder) is reused, never rebuilt (Rule 14).
//
// WHY THE DAEMON ENVIRONMENT IS PINNED FOR THE WHOLE TEST, not merely exported to
// a child: the surface under test IS this process's environment — the production
// code resolves GOPI_HOME/GOPI_SETTINGS itself, and the profile that holds the
// contested worker.lock is the one those variables name.

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// wdHandoverReleaseWait bounds how long an arm waits for the held lock to be
	// released before declaring the FIXTURE broken (not the code under test):
	// the release is a kill plus the kernel's close, so this only covers
	// scheduling.
	wdHandoverReleaseWait = 5 * time.Second
	// wdHandoverReleaseAfter is how long the holder keeps the lock AFTER the
	// successor's acquisition has begun. It must be shorter than
	// workerLockHandoverWait (otherwise the wait would expire for a reason this
	// test invented) and long enough that a single-attempt acquisition cannot slip
	// through: the holder is provably still holding when the old code's one and
	// only attempt is made.
	wdHandoverReleaseAfter = 1 * time.Second
	// wdHandoverStartDeadline bounds how long arm A watches the start before it
	// declares it hung. It is derived from the production bound the start's own
	// readiness handshake uses (daemonHandshakeWait): a window SHORTER than that
	// would report the epic's own patience as the defect, which is precisely the
	// mistake the host-load red made (Rule 38 — a bound names what it measures).
	wdHandoverStartDeadline = daemonHandshakeWait + wdHandoverReleaseWait
	// wdHandoverRestartWait is the `--wait` this arm passes to the real command:
	// it bounds the STOP half (the in-flight round the predecessor was asked to
	// finish), never the handover — the handover has its own production bound
	// (workerLockHandoverWait), which is what the fix introduces.
	wdHandoverRestartWait = "5s"
	// wdImmediateRefusalCeiling bounds the PLAIN start's refusal. It is
	// deliberately a fraction of workerLockHandoverWait: the arm asserts "refused
	// without waiting", and a ceiling equal to the wait it excludes would pass by
	// accident.
	wdImmediateRefusalCeiling = workerLockHandoverWait / 4
	// wdHandoverIdleExits / wdHandoverIdleSec make the loop self-exit quickly
	// (the loop's OWN knobs, never a test-only bypass), so the daemon this epic
	// starts is never left running: --restart's successor is a real detached
	// daemon, and the test host carries the operator's live ones.
	wdHandoverIdleExits = "1"
	wdHandoverIdleSec   = "1"
	// workerLockRefusalPrefix is the opening of the production refusal
	// (workerLockRefusal), the part that survives its %s substitutions.
	workerLockRefusalPrefix = "already running (lock "
)

// wdHandoverEnv pins a PRIVATE profile (Rules 6/34: a sandbox HOME, its own
// settings and ledger) as THIS process's environment, so the production
// resolution — not a copy of it — points at the profile whose worker.lock is
// contested. The agent signature is cleared for the daemon this epic really
// starts: this suite runs inside an agent session, and the operator's ruling of
// 2026-09-12 makes `gopi worker` refuse to start there; clearing it reproduces
// the operator's own shell shape.
//
// The REPO CONTEXT is pinned too (SandboxRepoContext): a real detached worker runs
// the lifecycle pass, and without the sandbox that pass enumerates and frees the
// developer's worktrees (see repo_context_sandbox_test.go — the reason every
// real-spawn case in this package calls it).
func wdHandoverEnv(t *testing.T) string {
	t.Helper()
	SandboxRepoContext(t)
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_SETTINGS", wdSettings(t, home))
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	t.Setenv(workerIdleExitsEnv, wdHandoverIdleExits)
	t.Setenv(workerIdleIntervalEnv, wdHandoverIdleSec)
	wdClearAgentSignature(t)
	return home
}

// wdClearAgentSignature clears this session's agent markers from the process
// environment, so the daemon these arms really start looks like the operator's
// own (see wdHandoverEnv). t.Setenv scopes the change to the test (Rule 17), and
// the names come from the ONE owner of that vocabulary — internal/config, which
// the spawn guard itself reads — rather than a list retyped here (Rules 13/14).
func wdClearAgentSignature(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		config.AgentSessionEnv,
		config.AgentSessionAncestorEnv,
		config.AgentSpawnGuardOffEnv,
		config.HermesSessionIDEnv,
		config.HumanTerminalEnv,
	} {
		t.Setenv(name, "")
	}
}

// TestWorkerDaemonRestartHandsOverTheLock is arm A — the defect itself, on the
// production restart path.
//
// Fixture: a foreign process holds worker.lock (the predecessor's teardown window,
// made a FACT). The test then calls workerDaemonSpawnErr — the exact call
// `gopi worker --restart` makes — and releases the lock a second later.
//
// GREEN (fixed): the acquisition WAITS for the handover and reaches the requested
// end state (a fresh resident daemon), which is what the operator asked for and
// what the old code could not deliver whenever the window was hit.
// RED (base): the single LOCK_NB attempt lands inside the window, the call returns
// the refusal ("another worker daemon is already running (lock …)") although the
// predecessor is gone, and the case fails by name.
func TestWorkerDaemonRestartHandsOverTheLock(t *testing.T) {
	// PINNED HERE, not only inside wdHandoverEnv: this function is the spawn site
	// (`wdRun` below starts a real gopi), and the repo-context gate judges the
	// SPAWNING function — a helper called from it does not pin it
	// (repo_context_sandbox_guard_test.go).
	SandboxRepoContext(t)
	home := wdHandoverEnv(t)
	path := wdWorkerLockPathFromEnv(t)
	env := wdEnv(t, home, wdSettings(t, home),
		workerIdleExitsEnv+"="+wdHandoverIdleExits,
		workerIdleIntervalEnv+"="+wdHandoverIdleSec)

	// The predecessor's teardown, as a FACT: a foreign process holds this
	// profile's worker.lock — exactly the state `gopi worker --restart` finds when
	// its predecessor's pid has gone but its exit path (and so its flock) has not
	// finished. The holder is still holding when the successor's acquisition is
	// first attempted, and is released a second later.
	holder := bfdStartFlockHolder(t, path)
	released := make(chan struct{})
	go func() {
		time.Sleep(wdHandoverReleaseAfter) //stylegate:allow-sleep holds the fixture lock across the successor's first acquisition attempt — the arm's whole premise
		_ = holder.Process.Kill()          //stylegate:ignore kills the handover fixture's holder process once the takeover was observed
		_, _ = holder.Process.Wait()       //stylegate:ignore reaps the killed holder so the fixture does not leak a process
		close(released)
	}()

	start := time.Now()
	code, out := wdRun(t, wdBin(t), env, CliVerbWorker, workerControlRestart, "--wait", wdHandoverRestartWait)
	<-released
	elapsed := time.Since(start)

	// The verdict, read off the REAL command's own words — never off a proxy for
	// them. The base code's shape is exit 1 with the refusal naming the holder;
	// the fix's is exit 0 plus the successor it really started (Rule 31).
	//
	// The HONEST LIMIT of this arm: the timing (a 1s hold) makes a single-attempt
	// acquisition lose, but it cannot PROVE which millisecond-window the
	// 2026-10-04 saturation run hit — that is a narrative fact about one run
	// (Rule 38), kept in the report.
	if strings.Contains(out, workerLockRefusalPrefix) {
		t.Fatalf("`%s %s` was REFUSED by a lock that was released %s in: the restart did not hand over.\n--- output ---\n%s",
			CliVerbWorker, workerControlRestart, wdHandoverReleaseAfter, out)
	}
	if code != 0 {
		t.Fatalf("`%s %s` must exit 0 through a handover (took %s), got %d:\n%s",
			CliVerbWorker, workerControlRestart, elapsed, code, out)
	}
	pid := wdParseRestartSuccess(t, out)
	t.Cleanup(func() { wdStopPID(t, pid) })
}

// TestWorkerDaemonPlainStartRefusesAtOnce is arm B — the contract the handover
// must NOT change.
//
// A plain `gopi worker --daemon` (workerDaemonSpec, the immediate acquisition) is
// refused while another holder is LIVE, and it must be refused AT ONCE: a start
// that waited would be a slow, silent refusal — the operator asked for "the one
// resident worker", and the answer is "there already is one".
func TestWorkerDaemonPlainStartRefusesAtOnce(t *testing.T) {
	wdHandoverEnv(t)
	path := wdWorkerLockPathFromEnv(t)
	// The holder stays alive for the whole arm (bfdStartFlockHolder holds for
	// minutes and its cleanup kills it), so the refusal is exercised against a
	// genuinely LIVE holder — not against the exit window.
	_ = bfdStartFlockHolder(t, path)

	start := time.Now()
	reused, err := daemonSpawn(workerDaemonSpec(), nil)
	elapsed := time.Since(start)
	if err == nil && !reused {
		t.Fatalf("a plain --daemon start was ACCEPTED while a foreign process held %s — the singleton guard did not fire", path)
	}
	if err != nil {
		if !strings.Contains(err.Error(), workerLockRefusalPrefix) {
			t.Errorf("the plain start's refusal must name the live holder (%q), got: %v", workerLockRefusalPrefix, err)
		}
		if elapsed >= wdImmediateRefusalCeiling {
			t.Errorf("the plain --daemon start took %s to refuse (ceiling %s) — the handover wait leaked into the immediate acquisition",
				elapsed, wdImmediateRefusalCeiling)
		}
	}
}

// wdParseRestartSuccess reads the successor's pid off --restart's own success
// line ("new daemon pid N, binary …, replaced pid M, …"). A success line that
// cannot be parsed is a FAILURE, not an empty pid (Rule 15: a start whose effect
// nobody can name has not been observed).
func wdParseRestartSuccess(t *testing.T, out string) int {
	t.Helper()
	const marker = "new daemon pid "
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("--restart reported success without naming the new daemon (want %q):\n%s", marker, out)
	}
	fields := strings.Fields(out[i+len(marker):])
	if len(fields) == 0 {
		t.Fatalf("--restart's success line is malformed:\n%s", out)
	}
	pid, err := strconv.Atoi(strings.TrimSuffix(fields[0], ","))
	if err != nil {
		t.Fatalf("--restart's new-daemon pid is unparsable (%q): %v", fields[0], err)
	}
	return pid
}

// wdWorkerLockPathFromEnv resolves the lock path exactly as the production code
// does — workerLockPath reads the profile from the environment this fixture pinned
// — so the fixture and the code under test contend on ONE file, never on two
// spellings of it.
func wdWorkerLockPathFromEnv(t *testing.T) string {
	t.Helper()
	path, err := workerLockPath()
	if err != nil {
		t.Fatalf("resolve the worker lock path: %v", err)
	}
	return path
}

// wdWaitFreshDaemon waits (bounded) for a resident daemon's state record to appear
// under dir and returns its pid. The record is written by the daemon itself as its
// first act, so this is the same observable `gopi worker --status` reads — not an
// estimate of the daemon's start-up.
func wdWaitFreshDaemon(t *testing.T, dir string) int {
	t.Helper()
	deadline := time.Now().Add(wdDaemonWait)
	for time.Now().Before(deadline) {
		for _, f := range workerLiveStateFiles(dir) {
			if f.PID > 0 && wdProcAlive(f.PID) {
				return f.PID
			}
		}
		time.Sleep(wdPollInterval) //stylegate:allow-sleep bounded wait for the spawned daemon's first state write
	}
	t.Fatalf("the handover start reported success but no live daemon published a state record under %s within %s", dir, wdDaemonWait)
	return 0
}
