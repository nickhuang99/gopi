package main

// worker_daemon_singleton_e2e_test.go — hermetic, real-binary proof for the
// worker DAEMON's single-instance guard and its operator control surface (epic
// worker-daemon-singleton, 2026-09-13).
//
// The epic's contract has two halves that pull in OPPOSITE directions, and both
// must hold at once:
//
//	A. `gopi worker --daemon` is a SINGLETON — a second detached start is
//	   refused (the merger's flock discipline), so an operator can rely on "a
//	   resident worker is running" meaning exactly one process.
//	B. `gopi worker` (FOREGROUND, no --daemon) is NOT gated — it holds no lock,
//	   so a developer can always start one in the front and watch it while the
//	   daemon is up. This is the observability the briefing asks for; a guard
//	   that also blocked the foreground face would have traded one problem for
//	   a worse one.
//
// Cases (all against the REAL binary in a private GOPI_HOME):
//
//	WS1 a second `--daemon`                → refused, non-zero, names the holder
//	WS2 `--status` on a live daemon        → pid + log, exit 0, lock untouched
//	WS3 `--status` when none runs          → "not running", exit 0
//	WS4 `gopi worker` FOREGROUND while the
//	    daemon holds the lock              → starts and keeps running (B)
//	WS5 `--stop`                           → the daemon goes, exit 0
//	WS6 `--restart`                        → a NEW pid replaces the old one
//
// Hermeticity (PROJECT.md Rule 6): a private GOPI_HOME, an empty ledger, no sync
// peers, no claim broadcast, and the resident scratch sweep throttled away.
// Every daemon is stopped by its EXPLICIT pid — never a pattern kill (the
// operator has live merger and worker daemons on this host).

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// wsDaemonIdleSec keeps the probe daemon polling fast WITHOUT letting it
	// self-exit (wsDaemonIdleExits is the loop's own opt-out, never a test-only
	// bypass): the cases below need a daemon that is still there when they look.
	wsDaemonIdleSec   = "1"
	wsDaemonIdleExits = "0"
	// wsSettlePoll is the poll cadence of the sandbox settle (Rule 1).
	wsSettlePoll = 50 * time.Millisecond
	// wsDaemonClaimWait bounds the wait for a freshly started daemon to reach
	// the TOP of its first round — the observable every stop assertion in this
	// file depends on. It is the TEST-side half of the same distinction the
	// binary makes for its operator (workerStartSettleWait): the durable stop
	// request is consumed at a round boundary, and the prologue that precedes
	// the first one is a measurement of the host (resident sweeps, the first
	// ledger read), not of the case. Measured 2026-10-03: 3s at load ~4, 8s at
	// load ~10, and longer than 20s at load 15.9 — where WS5's own
	// `--stop --wait 20s` bound expired during that prologue and reported "pid N
	// is STILL running 20s after the graceful stop request" (21.57s total),
	// which is what made this case red under host load while it was green
	// standalone. Waiting on the state the daemon publishes is the fix; the
	// bound only converts "never opened its loop" into a FAILURE with the
	// daemon's log attached, instead of a red that names the wrong wait.
	wsDaemonClaimWait = 90 * time.Second
	// wsClaimPoll is that wait's poll cadence.
	wsClaimPoll = 100 * time.Millisecond
)

// wdForeground starts a no-flag `gopi worker` as OUR OWN child (so it can be
// reaped) and returns the running command. The caller owns stopping it.
//
// THREE BOUNDS, AND THE TWO IN-PROCESS ONES ARE NOT ENOUGH ON THEIR OWN — that
// is the whole point of this function now. Measured (2026-11-08): this helper
// used to be a raw exec.Command + Start whose ONLY reclaim was the caller's
// `defer` SIGTERM+Wait, so every path that does not run defers — the test
// binary SIGKILLed by a watchdog, `go test`'s own -timeout panic ending in
// os.Exit, a killed CI job — left the worker standing. One such child
// (pid 3634507, spawned by TestWorkerDaemon_ForegroundWorkerIgnoresTheLock) was
// still alive 2 days 4 hours later: PPID 1, exe and cwd "(deleted)" under the
// suite's scratch dir, still carrying GOPI_DAEMON_DEATHSIG_HOLD_FD=200 in its
// environ — the death pipe it inherited and, on the foreground face, never
// watched. So the spawn goes through testutil's ONE reaper, which gives:
//
//	① Pdeathsig=SIGKILL — the KERNEL's bound, the only one that survives the
//	   parent being SIGKILLed, because no test code runs at all on that path;
//	② Setpgid + t.Cleanup(mp.Reap) — the in-process bound, covering a normal
//	   return, a t.Fatal and a panic (testing runs cleanups on every one);
//	③ and the production side now arms the inherited death pipe on the
//	   FOREGROUND face too (runTaskWorker → daemonlifetime.WatchFromEnv), so a
//	   child whose parent vanished while the kernel did not reason about it
//	   still ends itself instead of sweeping forever.
//
// Rule 42: the reaper signals ONLY the process group this test created and
// holds a handle to — never a pid, a name or a pattern. The caller's own
// SIGTERM stays as a best-effort graceful stop; Reap is the guarantee.
func wdForeground(t *testing.T, bin string, env []string) *exec.Cmd {
	t.Helper()
	return wdForegroundSpawn(t, bin, env).Cmd()
}

// wdForegroundSpawn is the spawn itself, returning the reaping handle as well
// as the command. Split out so the regression guard
// (TestWorkerDaemon_ForegroundWorkerIsBoundToItsTest) can assert the props of
// the process this helper really creates instead of re-deriving them from
// source text.
func wdForegroundSpawn(t *testing.T, bin string, env []string) *testutil.MockProcess {
	// repo-context-ok: the env vector it passes to the child is built by the caller (wsDaemonEnv/wdEnv) from a sandbox home + settings, and every caller calls SandboxRepoContext(t) before spawning. The marker lives HERE, on the function that actually spawns: the guard classifies spawners, not their wrappers (measured — putting it on wdForeground instead left this function unmarked and turned TestSpawnersPinTheRepoContext red).
	t.Helper()
	cmd := exec.Command(bin, CliVerbWorker)
	cmd.Env = env
	return testutil.StartMockProcessCmd(t, cmd)
}

// wsDaemonEnv is the environment a long-lived probe daemon runs in.
func wsDaemonEnv(t *testing.T, home, settings string) []string {
	t.Helper()
	return wdEnv(t, home, settings,
		workerIdleIntervalEnv+"="+wsDaemonIdleSec,
		workerIdleExitsEnv+"="+wsDaemonIdleExits)
}

// wsSettleWait bounds how long the refusal's own transient `systemd-run` wrapper
// (mechanism ①) may still be counted as a sandbox process: it is a thin client
// that exits once systemd has started the unit, so the window is short — the
// budget is generous because a slow host must not turn a correct refusal into a
// red.
const wsSettleWait = 10 * time.Second

// wsWaitSoleSandboxPID polls the sandbox process scan until the ONLY marked pid
// is want, returning the leftovers and whether it converged.
func wsWaitSoleSandboxPID(home string, want int) ([]int, bool) {
	deadline := time.Now().Add(wsSettleWait)
	var last []int
	for time.Now().Before(deadline) {
		last = last[:0]
		for _, pid := range wdSandboxPIDs(home) {
			if pid != want {
				last = append(last, pid)
			}
		}
		if len(last) == 0 {
			return last, true
		}
		time.Sleep(wsSettlePoll) //stylegate:allow-sleep poll while the refusal's transient systemd-run wrapper exits
	}
	return last, false
}

// wsStartDaemon starts a resident daemon for the sandbox and registers its reap.
//
// IT WAITS FOR THE DAEMON'S OWN "CLAIM LOOP OPEN" RECORD before returning. The
// record — the `claiming` field the loop publishes as the first thing round 1
// does (workerCtx.markClaiming) — is the observable every stop case in this
// file needs: a durable stop request is consumed at a round boundary, so a
// `--stop` issued during the daemon's start-up prologue (its cgroup escape, the
// resident scratch sweep, the first ledger read) is served by the END OF THAT
// PROLOGUE, and the host decides how long that is.
//
// MEASURED 2026-10-03 (this file's own failing row): at load 15.9 the prologue
// outlasted WS5's `--stop --wait 20s`, the command reported "pid N is STILL
// running 20s after the graceful stop request", and the case took 21.57s — red
// in the package gate, green standalone at 17.6s. The daemon's own log showed
// `rounds 0, note: daemon starting` for the whole window: it had consumed the
// stop request but never reached the boundary that consumes it. Waiting on the
// observable removes the host from the assertion without weakening it: the
// `claiming` record can only be written by a daemon that really reached the top
// of a round, and a daemon that never does still fails, by name.
func wsStartDaemon(t *testing.T, bin string, env []string) (int, string) {
	t.Helper()
	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong)
	if code != 0 {
		t.Fatalf("`gopi worker --daemon` must exit 0, got %d:\n%s", code, out)
	}
	pid, logPath := wdParseStart(t, out)
	t.Cleanup(func() { wdStopPID(t, pid) })
	if !wdProcAlive(pid) {
		t.Fatalf("the reported daemon pid %d is not alive", pid)
	}
	home := wsHomeFromEnv(t, env)
	if !wsWaitClaimLoopOpen(t, home, pid, logPath) {
		t.Fatalf("the daemon pid %d never published its claim-loop-open record within %s — the start-up prologue (cgroup escape, resident sweeps, the first ledger read) did not finish, so no stop could be honoured; daemon log:\n%s",
			pid, wsDaemonClaimWait, wdLogTail(logPath))
	}
	return pid, logPath
}

// wsHomeFromEnv recovers the sandbox HOME these cases pinned into the child's
// environment: the state record the fixture polls lives under it, and deriving
// it from the SAME vector the child received keeps the two in step (a second
// spelling of the path could drift from the one the daemon writes to).
//
// LAST WINS, and that is not a detail (measured 2026-10-03, this fixture's own
// second defect): the vector is built as `os.Environ()` plus the case's own
// overrides, so a `GOPI_HOME` from the SUITE's environment sits EARLIER in the
// slice than the one the case appended — and `exec.Cmd.Env` gives the LAST
// duplicate to the child. Reading the first entry therefore made the fixture
// poll a directory the daemon never writes to (`…/suite-gopi-home/.gopi/worker`
// while the daemon wrote under the case's `t.TempDir()`), so every stop case
// waited its full bound on a file that could not appear, and the failure text
// blamed the daemon's start-up prologue for a bug in the fixture. The child's
// rule is the reader's rule: the LAST occurrence is the one in force.
func wsHomeFromEnv(t *testing.T, env []string) string {
	t.Helper()
	home := ""
	for _, kv := range env {
		if h, ok := strings.CutPrefix(kv, "GOPI_HOME="); ok {
			home = h
		}
	}
	if home == "" {
		t.Fatalf("the fixture's environment carries no %s — it cannot locate the daemon's state record", "GOPI_HOME")
	}
	return home
}

// wsWaitClaimLoopOpen polls the daemon's OWN state record until it says the
// claim loop is open (the `claiming` field), and returns false when the bound
// expires first. It reads the file the binary itself writes — the same
// observable `gopi worker --stop` gates on (workerClaimLoopOpened) — so the
// fixture and the surface under test wait on ONE fact, not on two estimates.
func wsWaitClaimLoopOpen(t *testing.T, home string, pid int, logPath string) bool {
	t.Helper()
	deadline := time.Now().Add(wsDaemonClaimWait)
	for time.Now().Before(deadline) {
		if wsClaimLoopOpen(home, pid) {
			return true
		}
		if !wdProcAlive(pid) {
			t.Logf("the daemon pid %d exited before publishing its claim-loop record; daemon log:\n%s", pid, wdLogTail(logPath))
			return false
		}
		time.Sleep(wsClaimPoll) //stylegate:allow-sleep poll for the daemon's claim-loop record
	}
	return false
}

// wsClaimLoopOpen reads one daemon's state record and reports whether its loop
// is open. The record is read by PATH (worker-state-<pid> under the sandbox's
// own state dir), never by scanning for "any" record, so a sibling instance in
// the same profile cannot answer for this pid — and the pid inside the file is
// checked too. An unreadable or half-written record is false: "no evidence yet"
// is not "evidence of the loop" (Rule 15).
func wsClaimLoopOpen(home string, pid int) bool {
	path := filepath.Join(wdWorkerStateDir(home), workerStateFilePrefix+strconv.Itoa(pid))
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var st workerControlState
	if err := json.Unmarshal(b, &st); err != nil {
		return false
	}
	return st.PID == pid && st.OpenedClaimLoop()
}

// TestWorkerDaemon_SecondStartIsRefused is WS1 — the epic's A half: the
// single-instance flock makes `--daemon` mean "the one resident worker", and a
// second start must say WHY (holder pid) instead of silently becoming a second
// sweep.
func TestWorkerDaemon_SecondStartIsRefused(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wsDaemonEnv(t, home, settings)

	first, _ := wsStartDaemon(t, bin, env)

	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong)
	if code == 0 {
		t.Fatalf("a SECOND `gopi worker --daemon` must be refused, got exit 0:\n%s", out)
	}
	for _, want := range []string{"already running", "refusing a second resident worker", "pid " + strconv.Itoa(first)} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal must name %q, got:\n%s", want, out)
		}
	}
	// The refusal is a REFUSAL, not a partial start: no second process may be
	// left behind in this sandbox.
	//
	// WHY IT SETTLES RATHER THAN SNAPSHOTS (measured 2026-09-17, task
	// cmd-agent-red-reanchor): when the start seam picks mechanism ①
	// (`systemd-run --user --scope`), the refusing attempt has ALREADY been
	// executed by a `systemd-run` wrapper carrying this sandbox's environment —
	// so for the few milliseconds between the refusal and the wrapper's own exit,
	// an instantaneous scan counts the WRAPPER as an extra sandbox process. That
	// is a statement about the wrapper, not about the refusal: the wrapper exits
	// by itself, while a genuinely extra worker (one that took the lock and
	// started sweeping) never converges. The load on the host decides whether the
	// window is visible at all, which is what made this case flaky (observed once
	// in three full runs, load ~25).
	if leftover, ok := wsWaitSoleSandboxPID(home, first); !ok {
		t.Errorf("the refused start left %d extra process(es) in the sandbox (%v) %s after the refusal",
			len(leftover), leftover, wsSettleWait)
	}
}

// TestWorkerDaemon_StatusReportsTheLiveDaemon is WS2/WS3 — the operator's
// read-only answer, on a live daemon and on none. Read-only is asserted on the
// FILESYSTEM (the lock file's bytes and mtime), not on the text: a status probe
// that takes the lock would look like a running daemon.
func TestWorkerDaemon_StatusReportsTheLiveDaemon(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wsDaemonEnv(t, home, settings)

	lockPath := wdWorkerLockPath(home)

	// WS3 first: nothing of OURS is running. The host may already run unrelated
	// gopi-workers (another session's daemon), so the assertion is not "no
	// workers running" but the honest contract: no pid belongs to THIS home's
	// state dir — i.e. nothing points at this test's lock/state.
	code, out := wdRun(t, bin, env, CliVerbWorker, workerControlStatus)
	if code != 0 {
		t.Fatalf("`gopi worker --status` with no daemon must exit 0, got %d:\n%s", code, out)
	}
	// The needle is the state FILE's own path shape — `<home>/.gopi/worker/`
	// WITH the trailing separator — not the bare directory spelling. The bare
	// form is a substring of the lock path this very block legitimately prints
	// (`<home>/.gopi/worker.lock` contains `<home>/.gopi/worker`), so it fired on
	// an output the intent never objected to: the assertion meant "no PID of
	// THIS home was named" and said "this string must not appear". Anchoring on
	// the separator fixes the false positive and weakens nothing — a real state
	// line (`... .gopi/worker/worker-state-<pid>`) and the residue listing's
	// remedy (`rm -f <dir>/worker-state-*`) both still carry the anchored form,
	// which the seeded-state-file proof
	// (TestWorkerDaemon_StatusAssertionCatchesASeededStateFile) verifies.
	if strings.Contains(out, wdWorkerStateDir(home)+string(filepath.Separator)) {
		t.Errorf("status with no daemon must not name THIS home's worker state files, got:\n%s", out)
	}

	pid, _ := wsStartDaemon(t, bin, env)
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("the daemon must have created %s: %v", lockPath, err)
	}
	beforeBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}

	code, out = wdRun(t, bin, env, CliVerbWorker, workerControlStatus)
	if code != 0 {
		t.Fatalf("`gopi worker --status` must exit 0, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "pid "+strconv.Itoa(pid)) {
		t.Errorf("status must name the live daemon pid %d, got:\n%s", pid, out)
	}
	// The state record: the OLD verb printed the daemon log path; the per-worker
	// enumeration answers "what is it running?" with the state record instead,
	// so the assertion follows the daemon's OWN record line — the pid line must
	// carry its state file, and the log stays discoverable from the daemon's
	// launch line (not repeated per worker).
	//
	// The needle is the state file's FULL PATH, not the bare name: the residue
	// listing's remedy line (`rm -f <state dir>/worker-state-*`) carries the bare
	// basename for a dir that names no pid at all, so the name alone would pass
	// on output that never mentioned this daemon. The path is the shape the
	// intent needs — the record line of THIS pid, read from THIS home's dir.
	statePath := filepath.Join(wdWorkerStateDir(home), workerStateFilePrefix+strconv.Itoa(pid))
	if !strings.Contains(out, "state ") || !strings.Contains(out, statePath) {
		t.Errorf("status must name the live daemon's state record %s for pid %d, got:\n%s", statePath, pid, out)
	}
	after, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat lock after status: %v", err)
	}
	afterBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock after status: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) || string(beforeBytes) != string(afterBytes) {
		t.Errorf("--status must be READ-ONLY, but the lock changed: mtime %s → %s, body %q → %q",
			before.ModTime(), after.ModTime(), beforeBytes, afterBytes)
	}
}

// --- the assertion's own helpers, and the proof that it still bites --------

// wdWorkerStateDir is THIS profile's worker STATE directory (the one holding
// `worker-state-<pid>` records) as the binary resolves it, for a sandbox HOME.
// It is built from the HOME plus the two production constants — the profile
// directory name and the component directory name — so a rename of either moves
// the assertion with it (Rules 1b/13: no bare ".gopi", no bare "worker"; the
// profile dir name comes from the leaf package that owns it).
func wdWorkerStateDir(home string) string {
	return filepath.Join(home, testutil.GopiProfileDirName, workerStateDirName)
}

// wdWorkerLockPath is this profile's lock path — the file the no-daemon report
// legitimately names, and the reason the WS3 assertion must anchor on the state
// FILE shape instead of the bare directory spelling.
func wdWorkerLockPath(home string) string {
	return filepath.Join(home, testutil.GopiProfileDirName, workerLockFilename)
}

// TestWorkerDaemon_StatusAssertionCatchesASeededStateFile is the NEGATIVE half
// of the WS3 assertion, and the reason the anchored needle above is not a
// widened check: it seeds a state file of THIS home (a dead pid, the residue
// shape a leaked fixture leaves) and drives the assertion's own needle over the
// REAL binary's report — the report must name the seeded path. It also pins the
// false positive that motivated the anchor: the no-daemon block's lock line
// (`<dir>worker.lock`, built from workerStatusNoWorkersLock, so it cannot drift)
// carries the bare directory spelling yet must NOT fire the needle. Both halves
// are hermetic: the assertion is about THIS home, so unrelated workers on the
// host neither satisfy nor suppress it.
func TestWorkerDaemon_StatusAssertionCatchesASeededStateFile(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wsDaemonEnv(t, home, settings)

	dir := wdWorkerStateDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed the state dir %s: %v", dir, err)
	}
	// A pid that cannot be live on any host (above pid_max): its state file is
	// the residue shape a leaked fixture leaves, and the report must NAME it.
	dead := deadPidForResidueReason
	seeded := filepath.Join(dir, workerStateFilePrefix+strconv.Itoa(dead))
	if err := os.WriteFile(seeded, []byte(fmt.Sprintf(
		`{"pid":%d,"state":"idle","updated":"2026-09-14T00:00:00Z"}`, dead)), 0o644); err != nil {
		t.Fatalf("seed the state record %s: %v", seeded, err)
	}

	// The measurement: the REAL binary, over a profile that holds one state file
	// of THIS home. Hermetic — the assertion is about THIS home, so unrelated
	// workers of the host neither help nor hurt it.
	code, out := wdRun(t, bin, env, CliVerbWorker, workerControlStatus)
	if code != 0 {
		t.Fatalf("`gopi worker --status` must exit 0, got %d:\n%s", code, out)
	}
	needle := dir + string(filepath.Separator)
	if !strings.Contains(out, needle) {
		t.Fatalf("a state file of THIS home must make the report name its path (%s) — otherwise the assertion has nothing to bite on:\n%s",
			needle, out)
	}

	// The false-positive half, built from the production shape rather than
	// copied text: the no-daemon block's lock line. It names `<dir>worker.lock`,
	// whose prefix IS the bare directory spelling — the exact coincidence that
	// made the unanchored assertion fail on an output its intent never objected
	// to. The anchored needle must NOT fire here.
	lockLine := fmt.Sprintf(workerStatusNoWorkersLock, wdWorkerLockPath(home))
	if strings.Contains(lockLine, needle) {
		t.Errorf("the anchored needle %q must not fire on the (legitimate) lock line %q", needle, lockLine)
	}
	if !strings.Contains(lockLine, wdWorkerStateDir(home)) {
		t.Fatalf("premise: the lock line must carry the bare state-dir spelling, got %q", lockLine)
	}
}

// TestWorkerDaemon_ForegroundWorkerIgnoresTheLock is WS4 — the epic's B half,
// and the case that would have caught a guard implemented in the wrong place.
// A foreground worker must START and KEEP RUNNING while the daemon holds the
// lock: the lock is the DAEMON's guard, not the verb's.
func TestWorkerDaemon_ForegroundWorkerIgnoresTheLock(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wsDaemonEnv(t, home, settings)

	wsStartDaemon(t, bin, env)

	// The foreground face: no --daemon, so no lock is taken. It must be alive
	// after the detach line's absence is checked (a foreground worker never
	// prints the daemon start line).
	cmd := wdForeground(t, bin, env)
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM) //stylegate:ignore best-effort SIGTERM to our own probe child; the wait below decides whether it stopped
			_, _ = cmd.Process.Wait()               //stylegate:ignore reap of the probe child on the deferred teardown path — the delivered status is the assertion
		}
	}()
	deadline := time.Now().Add(wdForegroundProbe)
	for time.Now().Before(deadline) {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Fatalf("the FOREGROUND `gopi worker` exited (status %v) while a daemon held the worker lock — the lock must gate --daemon only",
				cmd.ProcessState)
		}
		time.Sleep(wdPollInterval) //stylegate:allow-sleep bounded observation window for the foreground worker
	}
}

// TestWorkerDaemon_StopEndsTheDaemon is WS5: `--stop` is the one primitive — the
// daemon finishes the (idle) round and exits, and the command reports success.
//
// WHAT THIS CASE COSTS, AND WHY (measured 2026-10-03, host load 12-16): wsStartDaemon
// does not return until the daemon has PUBLISHED its claim-loop-open record, so
// the `--stop --wait 20s` below is issued against a daemon that has really
// reached a round boundary. The excluded window is the start-up prologue, and on
// this host it is long: 3s at load ~4, 8s at load ~10, and >20s at load 15.9
// (resident scratch sweep 4.5s, the rest in the first ledger read over a
// 56k-entry sessions tree). Before this change a stop issued into that window
// expired its 20s bound and reported "pid N is STILL running 20s after the
// graceful stop request" while the daemon's own record read `rounds 0, note:
// daemon starting` — the case failed at 21.57s in the package gate and passed
// standalone at 17.6s, i.e. the bound was measuring the host's prologue and
// calling it the worker's round.
func TestWorkerDaemon_StopEndsTheDaemon(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wsDaemonEnv(t, home, settings)

	pid, _ := wsStartDaemon(t, bin, env)

	code, out := wdRun(t, bin, env, CliVerbWorker, workerControlStop, workerControlWait, "20s")
	if code != 0 {
		t.Fatalf("`gopi worker --stop` must exit 0 once the daemon leaves, got %d:\n%s", code, out)
	}
	if !wdWaitPIDGone(t, pid, wdDaemonWait) {
		t.Fatalf("pid %d is still alive after a successful --stop", pid)
	}
	// The end state is observable: a follow-up status agrees — the daemon's pid
	// is GONE from the enumeration (its state file is now residue, not a worker).
	_, out = wdRun(t, bin, env, CliVerbWorker, workerControlStatus)
	if strings.Contains(out, "pid "+strconv.Itoa(pid)+":") {
		t.Errorf("after --stop the status must not list the stopped daemon pid %d, got:\n%s", pid, out)
	}
}

// TestWorkerDaemon_RestartReplacesTheDaemon is WS6: `--restart` is the atomic
// spelling of stop-then-start, and the proof of "replaced" is a DIFFERENT pid
// (a restart that silently kept the old process would be the stale-code trap
// the verb exists to close).
func TestWorkerDaemon_RestartReplacesTheDaemon(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wsDaemonEnv(t, home, settings)

	first, _ := wsStartDaemon(t, bin, env)

	code, out := wdRun(t, bin, env, CliVerbWorker, workerControlRestart, workerControlWait, "20s")
	if code != 0 {
		t.Fatalf("`gopi worker --restart` must exit 0, got %d:\n%s", code, out)
	}
	second, _ := wdParseStart(t, out)
	t.Cleanup(func() { wdStopPID(t, second) })
	if second == first {
		t.Fatalf("--restart must start a NEW process; both the old and the new pid are %d", first)
	}
	if !wdWaitPIDGone(t, first, wdDaemonWait) {
		t.Errorf("the OLD daemon pid %d survived --restart", first)
	}
	if !wdProcAlive(second) {
		t.Errorf("the NEW daemon pid %d is not alive", second)
	}
	// Exactly one resident daemon — the flock's whole point.
	alive := 0
	for _, pid := range wdSandboxPIDs(home) {
		if wdProcAlive(pid) {
			alive++
		}
	}
	if alive != 1 {
		t.Errorf("after --restart exactly ONE daemon must be resident, found %d", alive)
	}
}

// TestWorkerControlParse_NamesOneAction pins the control grammar: each action is
// recognized, --force alone means "stop, interrupt", and a conflict is left for
// the caller to refuse (never a silent precedence).
func TestWorkerControlParse_NamesOneAction(t *testing.T) {
	cases := []struct {
		args   []string
		action workerControlAction
		force  bool
		ok     bool
	}{
		{[]string{workerControlStatus}, workerActionStatus, false, true},
		{[]string{workerControlStop}, workerActionStop, false, true},
		{[]string{workerControlRestart}, workerActionRestart, false, true},
		{[]string{workerControlStart}, workerActionStart, false, true},
		{[]string{workerControlForce}, workerActionStop, true, true},
		{[]string{workerControlStop, workerControlForce}, workerActionStop, true, true},
		{[]string{workerFlagOnce}, "", false, false},
		{[]string{workerControlWait, "5m", workerControlStop}, workerActionStop, false, true},
	}
	for _, c := range cases {
		got, ok := workerControlParse(c.args)
		if ok != c.ok {
			t.Errorf("%v: ok = %v, want %v", c.args, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.action != c.action || got.force != c.force {
			t.Errorf("%v: got action=%q force=%v, want action=%q force=%v",
				c.args, got.action, got.force, c.action, c.force)
		}
	}
	if conf := workerControlConflicts([]string{workerControlStop, workerControlStart}); len(conf) != 2 {
		t.Errorf("two distinct actions must be reported as a conflict, got %v", conf)
	}
	if !workerStatusRequested([]string{workerControlStatus}) {
		t.Error("--status must be recognized as the read-only action")
	}
	if workerStatusRequested([]string{workerControlStop}) {
		t.Error("--stop is NOT the read-only action")
	}
}
