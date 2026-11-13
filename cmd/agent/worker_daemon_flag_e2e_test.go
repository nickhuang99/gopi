package main

// worker_daemon_flag_e2e_test.go — hermetic, real-binary proof for the
// `gopi worker --daemon` self-daemonization (epic worker-daemon-flag).
//
// The property under test is an OS-LEVEL one (a detached process that outlives
// the invoking shell and still exits on its own), so every case runs the REAL
// binary inside a private GOPI_HOME:
//
//	W1 `gopi worker --daemon`            → returns fast, exit 0, names pid +
//	                                       log; the daemon is ALIVE with
//	                                       PPID=1 and its own SID (real detach)
//	W2 `gopi worker` (no flag)           → stays in the FOREGROUND (still
//	                                       running after the probe window,
//	                                       never prints the detach line)
//	W3 `gopi worker --daemon` with an
//	   unwritable log dir                → non-zero, a message saying why, and
//	                                       NO orphan left behind
//	W4 `gopi worker --daemon` (empty
//	   queue, idle budget 2×1s)          → the daemon SELF-EXITS after the
//	                                       budget, and its log carries the exit
//	                                       line (the drain-and-self-die
//	                                       contract survives the flag)
//	W5 `gopi worker --daemon --once`     → the detached one-shot drains and exits
//	W6 the child argv (pure)             → carries the loop flags, never --daemon
//
// Hermeticity (PROJECT.md Rule 6): a private GOPI_HOME with an EMPTY repos table,
// an empty ledger, no sync peers, no claim broadcast. Every daemon is stopped
// by its EXPLICIT pid — never a pattern kill (the operator has live merger and
// worker daemons on this host, and a `pkill -f gopi` is forbidden).
//
// TWO DELIBERATE DEPARTURES FROM THE MERGER'S E2E:
//
//  1. This suite predates the worker's singleton flock, so it carries no
//     "second start is refused" case: that assertion lives in
//     worker_daemon_singleton_e2e_test.go (TestWorkerDaemon_SecondStartIsRefused).
//     The pre-singleton rationale ("multi-instance is by design") was superseded
//     for the DAEMON face by the worker-daemon-singleton epic — do not restore it.
//  2. The daemon child is ENVIRONMENTALLY findable (workerDaemonEnv), which is
//     how a case reaps a process this test did not fork when the pid parse
//     fails; the merger's flock made that findable through serve.lock instead.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/cgroupscope"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// wdPromptBudget bounds how long `--daemon` may hold the invoking shell. It
	// must stay well BELOW daemonHandshakeWait, otherwise the assertion could
	// be satisfied by a child that never answered (the parent would still
	// return — just with a failure).
	wdPromptBudget = 15 * time.Second
	// wdForegroundProbe is how long a no-flag invocation must still be running
	// before we call it foreground.
	wdForegroundProbe = 3 * time.Second
	// wdPollInterval is the E2E poll cadence (Rule 1: one named literal).
	wdPollInterval = 100 * time.Millisecond
	// wdDaemonWait bounds the wait for a detached daemon to disappear (either
	// because it self-exited on the idle budget, or because we signalled it).
	wdDaemonWait = 30 * time.Second
	// wdIdleSec / wdIdleExits keep W4's idle budget tiny AND explicit: the
	// daemon must still exit by ITSELF, but the case cannot spend 90s on the
	// default. They are the loop's own knobs (never a test-only bypass).
	wdIdleSec   = "1"
	wdIdleExits = "2"
	// wdScratchGCThrottleMin disables the resident scratch sweep for the probe
	// daemon: an E2E must never sweep anything real (Rule 6).
	wdScratchGCThrottleMin = "100000"
	// wdBinDirName / wdBinName name the suite-lifetime probe binary (see wdBin:
	// it must outlive an individual test, because a DETACHED daemon keeps
	// running against it).
	//
	// The binary is named procname.NameGopi — the SAME name every production
	// gopi process carries (the operator's daemon is `bin/gopi`, comm "gopi";
	// measured: the launcher's workers report comm=gopi) — because the daemon's
	// own state record is ATTESTED by comparing the live pid's comm with that
	// name before `gopi worker --status` may render it. A probe binary called
	// "gopi-worker-daemon" made comm "gopi-worker-dae" (the kernel truncates to
	// 15 chars), so the fixture's daemon was reported as a RECYCLED pid — a
	// fixture-only artifact that hid the live daemon's real state line from
	// these E2E cases (measured 2026-09-14).
	wdBinDirName = "worker-daemon-bin"
	wdBinName    = procname.NameGopi
)

var (
	wdBinCache   string
	wdBinBuildEr error
)

// wdSettings writes the sandbox settings: an EMPTY repos table, so no pass ever
// touches a real repository.
func wdSettings(t *testing.T, home string) string {
	t.Helper()
	gopiDir := filepath.Join(home, ".gopi")
	if err := os.MkdirAll(gopiDir, 0o755); err != nil {
		t.Fatalf("mkdir gopi dir: %v", err)
	}
	path := filepath.Join(gopiDir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"repos":{}}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return path
}

// wdEnv is the child's environment: a private profile, an empty ledger, no
// compute peers, no claim broadcast, scratch sweep off, and an idle budget the
// case controls.
//
// The AGENT SIGNATURE IS STRIPPED (stripAgentSignature, the same helper the
// other worker E2E files use): these cases run the real binary from an agent's
// shell — this suite's own — and the operator ruling of 2026-09-12 makes
// `gopi worker` refuse to start there. Stripping it produces the OPERATOR'S
// shell shape, which is the only shape in which "the flag works" is a
// meaningful assertion; the guard's own refusal is asserted where it belongs
// (agent_spawn_guard_test.go), and the daemon branch deliberately sits BEHIND
// the guard.
//
// No peak-hour exemption is needed here (and none exists): this rig starts a
// DETACHED `gopi worker --daemon`, a spending start the (removed) peak-hour
// cost guard used to refuse inside a 2x window (devdoc/peak-guard-removal.md) —
// which is why this rig and the daemon branch both sat behind its exemption.
func wdEnv(t *testing.T, home, settings string, extra ...string) []string {
	t.Helper()
	return stripAgentSignature(append(os.Environ(),
		append([]string{
			"GOPI_HOME=" + home,
			"GOPI_SETTINGS=" + settings,
			"GOPI_MEMORY_DB=" + filepath.Join(home, "memory.db"),
			"GOPI_COMPUTE_NODES=",
			"GOPI_SYNC_PEERS=",
			config.SkipClaimBroadcastEnv + "=1",
			"GOPI_TEST_SCRATCH_GC_MIN=" + wdScratchGCThrottleMin,
		}, extra...)...))
}

// wdBin returns the binary under test: the package's ONE build slot
// (gopibin_test.go), which lives in the suite scratch root.
//
// The requirement this file really has is the PATH's LIFETIME: it starts
// DETACHED daemons whose /proc/<pid>/exe keeps pointing at the binary, so a
// t.TempDir()-scoped path would be unlinked while the daemon still runs,
// leaving an unreadable `ps` entry. The shared slot is built into
// testutil.SuiteTempRoot() and removed only when the suite ends, so it meets
// that requirement — and it does not rebuild a package the suite already built
// (this file's own cache and `go build` were a duplicate of the shared slot,
// name for name).
func wdBin(t *testing.T) string {
	t.Helper()
	return gopiTestBinPath(t)
}

// wdRun runs the binary to completion and returns exit code + combined output.
func wdRun(t *testing.T, bin string, env []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			t.Fatalf("run %v: %v\n%s", args, err, out)
		}
		code = ee.ExitCode()
	}
	return code, string(out)
}

// asExitError (the package's existing helper, agent_spawn_guard_test.go) is
// reused rather than redefined: a second copy would not compile.

// wdParseStart extracts pid + log path from the launcher's success line.
func wdParseStart(t *testing.T, out string) (int, string) {
	t.Helper()
	const marker = CliVerbWorker + " daemon started: pid "
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("--daemon success line missing (want %q), got:\n%s", marker, out)
	}
	// Only the SUCCESS LINE is parsed — never the tail of the whole output.
	// The post-start cgroup notes (the degraded "…and will end with that scope."
	// note, and the operator's next-step hint beside it) are printed AFTER this
	// line by design, and reading the output's last token made the log path
	// resolve to their final word ("scope." / "it.") — a parse that broke on
	// master as well, and broke differently as the notes' wording changed.
	rest := out[i+len(marker):]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	// Parsed by TOKEN, not by a copied prefix, so the wording can evolve
	// without silently breaking the assertion: pid first, path last.
	fields := strings.Fields(rest)
	if len(fields) < 3 {
		t.Fatalf("--daemon success line is malformed (%v):\n%s", fields, out)
	}
	pid, err := strconv.Atoi(strings.TrimSuffix(fields[0], ","))
	if err != nil {
		t.Fatalf("--daemon pid unparsable (%q): %v", fields[0], err)
	}
	return pid, fields[len(fields)-1]
}

// wdIsZombie reads the state letter out of /proc/<pid>/stat (an unreadable
// entry is reported as NOT a zombie: prefer "alive" to accepting a live daemon
// as dead).
func wdIsZombie(pid int) bool {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	// The state letter follows the (quoted, possibly space-bearing) comm field
	// — find the LAST ')' so a comm like "gopi (x)" cannot shift it.
	i := strings.LastIndex(string(b), ")")
	if i < 0 || i+2 >= len(b) {
		return false
	}
	return b[i+2] == 'Z'
}

// wdProcAlive reports whether pid is a live, RUNNING process. Signal 0 alone
// is not enough: it also succeeds for a ZOMBIE, which is a dead process that
// would read as "still running".
func wdProcAlive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	return !wdIsZombie(pid)
}

// wdMarks reports whether a process's environment carries the daemon child
// marker AND the sandbox home — i.e. "this is the daemon THIS case started".
// It is the reaping net for the case where a success line was unparsable, and
// it can never match the test process itself (the test never sets either).
func wdMarks(pid int, home string) bool {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		return false
	}
	env := string(b)
	return strings.Contains(env, workerDaemonEnv+"=") && strings.Contains(env, "GOPI_HOME="+home)
}

// wdSandboxPIDs lists every live process started from this sandbox: an orphan
// detector for W3 and a reaping net for the other cases.
func wdSandboxPIDs(home string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if wdMarks(pid, home) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// wdStopSignalFn is the send seam of wdStopPID. Production points at
// syscall.Kill; it exists so the errno branches (ESRCH accepted, every other
// errno refused) can be exercised by a case that OWNS its input, instead of by
// signalling something this test does not own (Rule 42).
var wdStopSignalFn = syscall.Kill

// wdStopPID stops a daemon by EXPLICIT pid (SIGTERM) and waits for it to go.
// Never a pattern kill: the operator runs live merger/worker daemons here.
//
// THE SEND IS AUTHORITATIVE — there is deliberately no `wdProcAlive` pre-check
// before it. A liveness probe followed by a send is itself the race this
// function used to report as a failure: the daemon may exit in between (its
// idle budget expiring, a `--once` drain finishing, another stop request
// landing first), and then the send answers ESRCH. "The target is gone" is
// exactly the end state a stop is FOR, so ESRCH is SUCCESS, not a failure.
// Every other errno (EPERM: the pid is not ours; EINVAL: a signal this kernel
// refuses) is still a real failure and is reported (Rule 15) — the fix is one
// errno wide, never "ignore the error", and there is no retry masking it.
func wdStopPID(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	if err := wdStopSignalFn(pid, syscall.SIGTERM); err != nil {
		// The ONE accepted errno: the process is already gone, which is the
		// requested end state (target gone == stopped). No retry and no wait:
		// there is nothing left to wait for.
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		t.Errorf("stop worker daemon pid %d: %v", pid, err)
		return
	}
	deadline := time.Now().Add(wdDaemonWait)
	for time.Now().Before(deadline) {
		if !wdProcAlive(pid) {
			return
		}
		time.Sleep(wdPollInterval) //stylegate:allow-sleep bounded wait for the probed daemon to exit
	}
	t.Errorf("worker daemon pid %d still alive after SIGTERM", pid)
}

// wdReap stops every process this sandbox started — the t.Cleanup net.
func wdReap(t *testing.T, home string) {
	t.Helper()
	for _, pid := range wdSandboxPIDs(home) {
		wdStopPID(t, pid)
	}
}

// wdProcessFieldPPID is the `ps -o` field naming a process's PARENT. A named
// constant (Rule 1b) because it is a value the detach assertions compare on:
// who reaps a detached daemon is the FACT those assertions ask about, and the
// field that carries it is named in one place.
const wdProcessFieldPPID = "ppid"

// wdProcessFieldPGID is the `ps -o` field naming a process's process GROUP. It
// is read by the foreground-reap guard, which must measure the group id from
// the KERNEL rather than trust the SysProcAttr it asked for: the reaper
// addresses exactly that number, so "is this child its own group leader, and
// not a member of the test's group" is a fact about the running process.
const wdProcessFieldPGID = "pgid"

// wdProcessField reads one numeric field out of `ps -o <field> -p pid`.
func wdProcessField(t *testing.T, pid int, field string) int {
	t.Helper()
	out, err := exec.Command("ps", "-o", field, "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatalf("ps -o %s -p %d: %v", field, pid, err)
	}
	lines := strings.Fields(string(out))
	if len(lines) < 2 {
		t.Fatalf("ps -o %s -p %d gave no value:\n%s", field, pid, out)
	}
	n, err := strconv.Atoi(lines[len(lines)-1])
	if err != nil {
		t.Fatalf("ps -o %s -p %d is not a number (%q)", field, pid, lines[len(lines)-1])
	}
	return n
}

// TestWorkerDaemon_DetachesAndKeepsRunning is W1: the invoking shell returns
// quickly with exit 0 and the worker it started is a real detached process —
// reparented to init (PPID 1) and its own session leader (SID == its pid).
// Without setsid the caller's process group would still own it, and the whole
// point of the flag (an agent shell or a terminal that must stay responsive) is
// lost.
func TestWorkerDaemon_DetachesAndKeepsRunning(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wdEnv(t, home, settings, workerIdleIntervalEnv+"=30", workerIdleExitsEnv+"=3")

	start := time.Now()
	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong)
	elapsed := time.Since(start)
	if code != 0 {
		t.Fatalf("`gopi worker --daemon` must exit 0, got %d after %s:\n%s", code, elapsed, out)
	}
	if elapsed > wdPromptBudget {
		t.Errorf("--daemon held the invoking shell %s (budget %s) — it must return as soon as the child is up", elapsed, wdPromptBudget)
	}
	pid, logPath := wdParseStart(t, out)
	t.Cleanup(func() { wdStopPID(t, pid) })
	if pid <= 0 {
		t.Fatalf("success line named no usable pid:\n%s", out)
	}
	// The success line's own log path must be the daemon's log (this is the
	// operator's only pointer to it).
	if want, err := workerDaemonLogPath(); err == nil && filepath.Base(logPath) != filepath.Base(want) {
		t.Errorf("success line named log %q, want a %q file", logPath, filepath.Base(want))
	}

	if !wdProcAlive(pid) {
		t.Fatalf("worker daemon pid %d is not alive right after the launcher returned", pid)
	}
	// THE DETACH VERDICT IS CGROUP-ONLY (Rule 24). PPID and SID are NOT evidence:
	// a process inherits its cgroup from its launcher, so `setsid` (or a
	// reparent to init) can leave a daemon that still dies with the login
	// session systemd is about to tear down.
	//
	// These two assertions were `ppid == 1` and `sid == pid`, and BOTH fail
	// against a daemon that detached the STRONGEST way: since the seam's ladder
	// gained rung ① (systemd-run --scope --user), the daemon is started by the
	// transient service manager (so its parent is the user manager, not init)
	// and placed in its own scope whose leader pid the runner picks (so it is
	// never a session leader). Measured 2026-09-17: pid 1372993 alive with
	// SID=1354878, the suite's own session — while its cgroup had genuinely left
	// the session scope. Rule 24 records exactly this trap: the "it looks
	// independent" reading is not independence.
	// Detachment is NOT "SID == pid" on every host: that is the criterion of the
	// DEGRADED path only. When the start seam picks mechanism ① (`systemd-run
	// --user --scope`, what this host's user bus selects), systemd-run execs the
	// daemon and the SESSION is still the launcher's — the daemon is detached by
	// its CGROUP, not by setsid (Rule 24: process independence is judged by
	// cgroup, never by SID). Asserting SID == pid unconditionally demanded the
	// degraded mechanism on a host that has a better one and failed a perfectly
	// detached daemon (measured 2026-09-17, baseline bf9fbef8: this case red with
	// the daemon alive, PPID 1, inside its own transient scope).
	//
	// WHICH criterion applies is read from the daemon's own environment (the
	// parent stamps the mechanism there on purpose), never predicted here.
	if mode := wdDaemonScopeMode(t, pid); mode == daemonScopeModeSysd {
		rel := dcCgroupOf(t, pid)
		if cgroupscope.InSessionScope(rel) {
			t.Errorf("worker daemon pid %d was detached by %s but its cgroup is still the login session scope %q", pid, mode, rel)
		}
		unit := dcDaemonEnvValue(t, pid, daemonScopeUnitEnv)
		if unit == "" {
			t.Fatalf("worker daemon pid %d was detached by %s but carries no %s stamp", pid, mode, daemonScopeUnitEnv)
		}
		if !strings.Contains(rel, unit) {
			t.Errorf("worker daemon pid %d cgroup %q does not name the scope unit %q it was started under — the unit is the proof of the %s detach", pid, rel, unit, mode)
		}
	} else if sid := wdProcessField(t, pid, "sid"); sid != pid {
		t.Errorf("worker daemon pid %d has SID=%d, want its own pid (setsid did not take)", pid, sid)
	}
	// The banner is the launch evidence (the merger E2E's tick-boundary
	// analogue): a worker that answers the handshake but never runs the loop
	// would otherwise look identical here.
	if !wdWaitLogContains(t, logPath, "worker: loop start", wdDaemonWait) {
		t.Errorf("worker daemon log %s never showed the loop banner", logPath)
	}
}

// wdDaemonScopeMode reads the cgroup-escape mechanism a live worker daemon was
// started with, out of its own environment (stamped by daemonScopeWrap). Empty
// means the variable is absent — the daemon did not come through the seam.
func wdDaemonScopeMode(t *testing.T, pid int) string {
	t.Helper()
	return dcDaemonEnvValue(t, pid, daemonScopeModeEnv)
}

// TestWorkerDaemon_NoFlagStaysForeground is W2: without the flag the worker is
// the SAME blocking foreground loop it always was (it must still be running
// after the probe window and must not print the detach line, which would mean
// the flag had become a no-op default).
func TestWorkerDaemon_NoFlagStaysForeground(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	// An idle budget long enough that the foreground probe cannot be satisfied
	// by the loop's own self-exit.
	env := wdEnv(t, home, settings, workerIdleIntervalEnv+"=30", workerIdleExitsEnv+"=3")

	cmd := exec.Command(bin, CliVerbWorker)
	cmd.Env = env
	stdout := &strings.Builder{}
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	// ROUTE THROUGH THE REAPER (see wdForegroundSpawn's doc): Pdeathsig=SIGKILL
	// is the bound that survives this test binary being SIGKILLed, and
	// t.Cleanup(mp.Reap) covers a return, a t.Fatal and a panic. The `defer`
	// below stays as the graceful stop; Reap is the guarantee.
	mp := testutil.StartMockProcessCmd(t, cmd)
	pid := mp.Pid()
	// This child is OUR OWN process, so it must be REAPED: stopping it without
	// Wait would leave a zombie, and a zombie still answers kill(pid, 0).
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM) //stylegate:ignore best-effort stop of our own probe child; mp.Wait below is what decides
		done := make(chan error, 1)
		go func() { done <- mp.Wait() }()
		select {
		case <-done:
		case <-time.After(wdDaemonWait):
			t.Errorf("foreground worker pid %d did not exit on SIGTERM", pid)
		}
	}()

	time.Sleep(wdForegroundProbe) //stylegate:allow-sleep deliberate foreground probe window (see wdForegroundProbe)
	if cmd.ProcessState != nil {
		t.Fatalf("no-flag `gopi worker` exited within %s — it must stay in the foreground:\n%s", wdForegroundProbe, stdout.String())
	}
	if strings.Contains(stdout.String(), "daemon started") {
		t.Errorf("no-flag invocation announced a detached daemon — --daemon must be opt-in:\n%s", stdout.String())
	}
	if !wdProcAlive(pid) {
		t.Errorf("foreground worker pid %d is not alive", pid)
	}
	// And it is NOT detached: it is our child, so its parent is this test.
	if ppid := wdProcessField(t, pid, wdProcessFieldPPID); ppid != os.Getpid() {
		t.Errorf("foreground worker pid %d has PPID=%d, want this process %d (it must not detach)", pid, ppid, os.Getpid())
	}
}

// TestWorkerDaemon_FailedStartIsLoudAndLeavesNoOrphan is W3 — the negative case
// the acceptance criteria single out. `.gopi/logs` is a FILE, so the daemon's
// log cannot be created: the start must fail with a non-zero exit AND a message
// saying what could not be created, and it must leave no process behind (a
// detached orphan whose output went nowhere is the worst outcome — the caller
// cannot even read why).
func TestWorkerDaemon_FailedStartIsLoudAndLeavesNoOrphan(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wdEnv(t, home, settings)

	// The obstruction: the log DIRECTORY path is occupied by a regular file.
	if err := os.WriteFile(filepath.Join(home, ".gopi", daemonLogDirName), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("occupy the log directory path: %v", err)
	}

	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong)
	if code == 0 {
		t.Fatalf("a daemon start that cannot succeed MUST exit non-zero (silent success is the defect):\n%s", out)
	}
	if !strings.Contains(out, "daemon start failed") {
		t.Errorf("the failure must be reported on stderr, got:\n%s", out)
	}
	if !strings.Contains(out, daemonLogDirName) {
		t.Errorf("the failure must name what could not be created, got:\n%s", out)
	}
	if strings.Contains(out, "daemon started") {
		t.Errorf("a failed start must not also print a success line:\n%s", out)
	}
	// No orphan: nothing from this sandbox survives the refusal.
	t.Cleanup(func() { wdReap(t, home) })
	if pids := wdSandboxPIDs(home); len(pids) != 0 {
		t.Errorf("a failed --daemon start left %v behind — no orphan may survive a refused start", pids)
	}
}

// TestWorkerDaemon_SelfExitsOnIdleQueue is W4 — the flag's hard constraint: a
// DETACHED worker is still a drain-and-self-die tool. With an empty queue and a
// 2×1s idle budget it must exit on its own (the pid disappears, without anyone
// signalling it) and its log must carry the exit line.
func TestWorkerDaemon_SelfExitsOnIdleQueue(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wdEnv(t, home, settings, workerIdleIntervalEnv+"="+wdIdleSec, workerIdleExitsEnv+"="+wdIdleExits)

	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong)
	if code != 0 {
		t.Fatalf("`gopi worker --daemon` must exit 0, got %d:\n%s", code, out)
	}
	pid, logPath := wdParseStart(t, out)
	t.Cleanup(func() { wdStopPID(t, pid) })

	// Nobody signals it: the process must vanish on its own.
	if !wdWaitPIDGone(t, pid, wdDaemonWait) {
		t.Fatalf("detached worker pid %d never self-exited on the %s×%s idle budget — detaching must not change the loop's lifetime", pid, wdIdleSec, wdIdleExits)
	}
	// The exit is 0 by contract (a drained queue is success), and the line is
	// the operator's only evidence that the daemon ENDED rather than crashed.
	if !wdWaitLogContains(t, logPath, "exiting 0", wdDaemonWait) {
		t.Errorf("worker daemon log %s never carried the idle self-exit line:\n%s", logPath, wdLogTail(logPath))
	}
	if !wdWaitLogContains(t, logPath, "worker: loop start", wdDaemonWait) {
		t.Errorf("worker daemon log %s never showed the loop banner", logPath)
	}
}

// TestWorkerDaemon_OnceDrainsAndExits is W5: the `--once × --daemon` rule —
// the two flags compose, and the detached one-shot drains the (empty) queue and
// exits by itself. `--once` is exported into the child's argv by the loop-flag
// list, so this also proves the transcribing builder carries it.
func TestWorkerDaemon_OnceDrainsAndExits(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wdEnv(t, home, settings)

	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong, workerFlagOnce)
	if code != 0 {
		t.Fatalf("`gopi worker --daemon --once` must exit 0, got %d:\n%s", code, out)
	}
	pid, logPath := wdParseStart(t, out)
	t.Cleanup(func() { wdStopPID(t, pid) })
	if !wdWaitPIDGone(t, pid, wdDaemonWait) {
		t.Fatalf("detached --once worker pid %d never drained-and-exited", pid)
	}
	if !wdWaitLogContains(t, logPath, "--once", wdDaemonWait) {
		t.Errorf("the --once mode did not survive the re-exec (child argv dropped it):\n%s", wdLogTail(logPath))
	}
}

// TestWorkerDaemonChildArgs_TranscribesLoopFlagsOnly pins the child argv: it
// carries the loop flags and NEVER --daemon (the child is identified by
// workerDaemonEnv, and a re-exec that forwarded --daemon would fork-bomb
// itself).
func TestWorkerDaemonChildArgs_TranscribesLoopFlagsOnly(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"daemon only", []string{"--daemon"}, ""},
		{"once", []string{"--daemon", "--once"}, "--once"},
		{"once after daemon", []string{"--once", "--daemon"}, "--once"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := workerDaemonChildArgs(tc.args)
			if len(got) < 2 || got[0] != daemonSpecArgv0(CliVerbWorker) || got[1] != CliVerbWorker {
				t.Fatalf("child argv must start with [%s %s], got %v", daemonSpecArgv0(CliVerbWorker), CliVerbWorker, got)
			}
			for _, a := range got {
				if a == workerDaemonFlagLong || strings.HasPrefix(a, workerDaemonFlagLong+workerFlagValueSep) {
					t.Fatalf("child argv still carries %s — the re-exec would recurse: %v", workerDaemonFlagLong, got)
				}
			}
			if diff := strings.Join(got[2:], " "); diff != tc.want {
				t.Errorf("loop flags = %q, want %q", diff, tc.want)
			}
		})
	}
}

// TestWorkerScanner_AcceptsTheDaemonVocabulary pins the ONE scanner both the
// CLI face and the daemon's pre-detach validation use: the accepted words are
// understood, the daemon flag is seen, and an unknown word still yields itself
// (which is what makes the refusal possible before anything is detached).
func TestWorkerScanner_AcceptsTheDaemonVocabulary(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantOnce     bool
		wantDmn      bool
		wantExplicit string
		wantBad      string
	}{
		{"daemon", []string{"--daemon"}, false, true, "", ""},
		{"daemon=value", []string{"--daemon=true"}, false, true, "", ""},
		{"once", []string{"--once"}, true, false, "", ""},
		{"both", []string{"--once", "--daemon"}, true, true, "", ""},
		// The ROW IS POSITIONAL (epic worker-positional-taskid): a bare word is
		// the row to run — that is the whole point of the spelling, so it must
		// come back as `explicit`, not as an offending word.
		{"row", []string{"t-abc"}, false, false, "t-abc", ""},
		{"row with run flags", []string{"t-abc", "--takeover", "--as", "runner:1"}, false, false, "t-abc", ""},
		// The retired row-selector flag is NOT part of the vocabulary: the
		// scanner yields it as the offending word, which is what makes the CLI
		// face refuse it with the replacement named — never silently ignore it
		// (a recorded command that types it must hear what replaced it).
		{"retired id pair", []string{workerIdRetiredFlag, "t-1"}, false, false, "", workerIdRetiredFlag},
		{"retired id equals", []string{workerIdRetiredFlag + "=t-1"}, false, false, "", workerIdRetiredFlag + "=t-1"},
		// A SECOND positional: the one shape the positional grammar could fold
		// silently into "one row" — refused like a stray word (Rule 15).
		{"second row", []string{"t-1", "t-2"}, false, false, "t-1", "t-2"},
		// The retired output-root option is NOT part of the vocabulary either:
		// the scanner yields it as the offending word, which is what makes the
		// CLI face refuse it with the replacement named (epic
		// child-stream-task-output).
		{"retired root pair", []string{workerLogRootRetiredFlag, "/d"}, false, false, "", workerLogRootRetiredFlag},
		{"retired root equals", []string{workerLogRootRetiredFlag + "=/d"}, false, false, "", workerLogRootRetiredFlag + "=/d"},
		{"unknown flag", []string{"--daemon", "--nope"}, false, true, "", "--nope"},
		{"empty", nil, false, false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, bad := scanWorkerArgs(tc.args)
			if bad != tc.wantBad {
				t.Fatalf("scanWorkerArgs(%v) bad = %q, want %q", tc.args, bad, tc.wantBad)
			}
			if got.once != tc.wantOnce || got.daemon != tc.wantDmn {
				t.Errorf("scanWorkerArgs(%v) = %+v, want once=%v daemon=%v",
					tc.args, got, tc.wantOnce, tc.wantDmn)
			}
			if got.explicit != tc.wantExplicit {
				t.Errorf("scanWorkerArgs(%v) explicit = %q, want %q", tc.args, got.explicit, tc.wantExplicit)
			}
		})
	}
}

// TestWorkerDaemonChild_RequiresTheEnvMarker: the child marker is an env var
// (never a flag), so a user typing --daemon twice can never be mistaken for the
// detached child — the recursion is structurally impossible.
func TestWorkerDaemonChild_RequiresTheEnvMarker(t *testing.T) {
	t.Setenv(workerDaemonEnv, "")
	if workerDaemonChild() {
		t.Fatalf("workerDaemonChild() must be false without %s set", workerDaemonEnv)
	}
	t.Setenv(workerDaemonEnv, daemonMarkerValue)
	if !workerDaemonChild() {
		t.Fatalf("workerDaemonChild() must be true when %s=%s", workerDaemonEnv, daemonMarkerValue)
	}
}

// wdWaitPIDGone polls until pid is no longer a live RUNNING process.
func wdWaitPIDGone(t *testing.T, pid int, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if !wdProcAlive(pid) {
			return true
		}
		time.Sleep(wdPollInterval) //stylegate:allow-sleep poll for the daemon's self-exit
	}
	return false
}

// wdWaitLogContains polls a log file for a substring.
func wdWaitLogContains(t *testing.T, path, want string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), want) {
			return true
		}
		time.Sleep(wdPollInterval) //stylegate:allow-sleep poll for the daemon's log line
	}
	return false
}

// wdLogTail returns the last lines of a log for a failure message (the daemon's
// own words are the diagnosis).
func wdLogTail(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "(log unreadable: " + err.Error() + ")"
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.Join(lines, "\n")
}
