package main

// daemon_test_leak_e2e_test.go — the ACCEPTANCE proof for the epic
// "test-process-killed ⇒ daemon must not survive".
//
// THE INCIDENT: cmd/agent's e2e cases start REAL gopi daemons (`gopi worker
// --daemon`) and stopped them only on t.Cleanup. A test PROCESS that is killed
// outright — `go test -timeout` after the panic dump, `kill -9`, the OOM killer
// — runs no cleanup, so every daemon it had started survived as PPID=1, kept its
// (deleted) sandbox on /dev/shm open, and ran on for hours until the tmpfs was
// full. Nothing in the process's world could notice: the sandbox is gone, the
// test binary is gone, and the daemon has no controlling terminal.
//
// WHAT THIS FILE PROVES, END TO END, WITH REAL PROCESSES:
//
//	1. a spawner process (this suite's own test binary, re-exec'd) starts a REAL
//	   `gopi worker --daemon` bound to its lifetime;
//	2. the spawner is SIGKILLed — no handler, no defer, no t.Cleanup: exactly
//	   what the harness does to a leaky test binary;
//	3. the daemon is gone, and the sweep finds ZERO orphans in the sandbox.
//
// It also proves the properties that keep the fix honest: the daemon IS alive
// while its spawner lives, and an UNBOUND daemon (the production shape) survives
// its spawner — the mechanism must not change an operator's daemon.
//
// Hygiene: every daemon this file starts is bound, and every case that kills a
// spawner asserts the daemon died; the leftover-assertion uses the triple
// (pid+starttime+cmdline), so a recycled pid can never be mistaken for a
// survivor. Every sandbox lives under the suite's scratch root — never /tmp.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/daemonlifetime"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// dtlPollInterval is the poll cadence for this file's process waits.
	dtlPollInterval = 100 * time.Millisecond
	// dtlDaemonWait bounds "the daemon starts / the daemon dies" waits. The
	// binding is a kernel HUP, so a healthy daemon dies immediately; the budget
	// only covers scheduling.
	dtlDaemonWait = 60 * time.Second
	// dtlSpawnerWait bounds the wait for the spawner to report its daemon.
	dtlSpawnerWait = 120 * time.Second
	// dtlIdleSec / dtlIdleExits keep the daemon's own idle budget short so it
	// cannot be mistaken for a binding that worked (a daemon that self-exits
	// looks identical to a daemon that was killed — the bound case asserts the
	// daemon is ALIVE first, and the unbound control case outlives this budget).
	dtlIdleSec   = "30"
	dtlIdleExits = "5"
)

// spawnerEnvGuard marks this suite's re-exec'd spawner child.
const spawnerEnvGuard = "GOPI_TEST_DAEMON_LEAK_SPAWNER"

// RunDaemonLeakSpawnerChild dispatches the spawner child. It must be called FIRST in
// TestMain: a spawner child must never run the suite (it is about to be killed).
func RunDaemonLeakSpawnerChild() bool {
	if os.Getenv(spawnerEnvGuard) == "" {
		return false
	}
	runDaemonLeakSpawner()
	return true
}

// runDaemonLeakSpawner is the child's whole life:
//
//	read its report fd binding from the environment, arm the daemon lifetime,
//	start a REAL `gopi worker --daemon` bound to it, report the daemon's pid,
//	then block. It never cleans up — a SIGKILLed spawner cannot, which is the
//	entire point.
func runDaemonLeakSpawner() {
	// predicate-excluded, and the reason is doubled: this spawner is reached only
	// through TestMain's RunDaemonLeakSpawnerChild guard and runs the daemon against
	// the spawner env (sandbox home + settings) the parent test handed it — AND the
	// invocation is a DAEMON start, whose child resolves its own working directory in
	// production (rcsDaemonFlagSpellings), so it never stands in this process's cwd.
	reportFD, err := reportFDForSpawner()
	if err != nil {
		spawnerFail(err.Error())
	}
	bin := os.Getenv(spawnerBinEnv)
	home := os.Getenv(spawnerHomeEnv)
	settings := os.Getenv(spawnerSettingsEnv)

	bound := os.Getenv(spawnerBindEnv) == "1"
	var daemonPid int
	if bound {
		// This process IS the daemon's lifetime holder, so it arms its OWN pipe
		// (exactly what a test binary's TestMain does for a suite). Inheriting
		// the suite's pipe instead would bind the daemon to the TEST BINARY, and
		// killing this spawner would then prove nothing — the write end would
		// still be open in the suite.
		//
		// ARMED FIRST, ENVIRONMENT SECOND, and the order is load-bearing: arming
		// EXPORTS the holder variable, so an environment captured before it does
		// not carry the binding and the daemon starts unbound with everything
		// looking right (measured — this was the first thing to fail after the
		// spawner took over the holding).
		if err := daemonlifetime.ArmThisProcess(); err != nil {
			spawnerFail("arm this spawner's binding: " + err.Error())
		}
	}
	cmd := exec.Command(bin, CliVerbWorker, daemonFlagLong)
	cmd.Env = dtlEnv(home, settings)
	if !bound {
		// UNBOUND, the production shape: no binding is armed and any inherited
		// variable is dropped, so the daemon is started with nothing to watch —
		// exactly what an operator's own `gopi worker --daemon` sees.
		cmd.Env = withoutEnv(cmd.Env, daemonlifetime.HoldEnvName)
	}
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		spawnerFail("start the daemon: " + err.Error())
	}
	// The daemon detaches (setsid) and reports its own pid in the parent's
	// success line; the parent (this process) has already exited by then, so the
	// pid is resolved by the SANDBOX's worker-state file, which the daemon
	// restamps with its own pid.
	daemonPid, err = awaitDaemonPid(home)
	if err != nil {
		spawnerFail(err.Error())
	}
	reportDaemonPid(reportFD, daemonPid, bound)
	// Wait for the launcher to finish so nothing of ours lingers as a child of
	// its own (the daemon is not our child: it is detached).
	_ = cmd.Wait() //stylegate:ignore the launcher we spawned is reaped so nothing of ours lingers; the daemon is detached and not our child
	_ = cmd.Wait() //stylegate:ignore reaping a process this test spawned; the case asserts through the fixture's own files, not through this status
	waitForever()
}

// waitForever blocks this process until it is killed, WITHOUT tripping Go's
// deadlock detector.
//
// `select {}` is the obvious spelling and it is WRONG here: when the blocking
// main goroutine is the only one left, the runtime reports "all goroutines are
// asleep - deadlock!" and exits the process. For a fixture whose whole job is to
// stay alive until it is killed, that is fatal in the most confusing way
// possible — the child exits, its death closes the death pipe, and the daemon it
// was supposed to keep alive dies with it (measured: this is what made the
// NEGATIVE case fail — "the bound daemon died while its spawner was alive" — and
// it would have silently turned every bound-daemon case green for the wrong
// reason if the assertion had been weaker). A timer keeps the scheduler awake,
// so the process simply waits.
func waitForever() {
	for {
		time.Sleep(foreverSleep) //stylegate:allow-sleep this is a process wait, not a poll: the caller's whole purpose is to stay alive until it is SIGKILLed
	}
}

// foreverSleep is the interval of the wait loop above.
const foreverSleep = time.Hour

// withoutEnv returns env minus the named variable (an environment that carries
// the binding is an environment whose daemons are bound).
func withoutEnv(env []string, name string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); k == name {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// awaitDaemonPid resolves the detached daemon's pid from the sandbox's worker
// state file, which the daemon restamps with its OWN pid after inheriting the
// lock.
func awaitDaemonPid(home string) (int, error) {
	pattern := filepath.Join(home, ".gopi", "worker", "worker-state-*")
	deadline := time.Now().Add(dtlDaemonWait)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(pattern) //stylegate:ignore bounded poll for the sandbox's residue; a malformed pattern is impossible here and an empty result is the expected iteration
		for _, m := range matches {
			// The pid is the file NAME's suffix (worker-state-<pid>) — the
			// daemon renames it to its own pid, which is the most direct
			// evidence there is; the body is only a fallback, because the
			// daemon may not have flushed it yet.
			pid := pidFromStateFileName(filepath.Base(m))
			if pid <= 0 {
				b, err := os.ReadFile(m)
				if err != nil {
					continue
				}
				pid = pidFromStateFile(string(b))
			}
			// Verify the pid is really a live process before reporting it: a
			// stale state file from an earlier daemon would otherwise be
			// reported as this one's.
			if pid > 0 {
				if _, err := testutil.ReadProcIdentity(pid); err == nil {
					return pid, nil
				}
			}
		}
		time.Sleep(dtlPollInterval) //stylegate:allow-sleep bounded wait for the sandbox's worker-state file
	}
	return 0, errors.New("the daemon never stamped a pid into " + pattern)
}

// pidFromStateFileName reads the pid out of a "worker-state-<pid>" file name.
func pidFromStateFileName(base string) int {
	i := strings.LastIndex(base, "-")
	if i < 0 || i+1 >= len(base) {
		return 0
	}
	return atoiSimple(base[i+1:])
}

// pidFromStateFile reads the "pid": N field out of a worker state file (JSON).
// It is the fallback for a name that does not carry the pid.
func pidFromStateFile(raw string) int {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.Trim(line, ","))
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			key, value, ok = strings.Cut(line, "=")
		}
		if !ok || strings.TrimSpace(strings.Trim(key, `"`)) != "pid" {
			continue
		}
		return atoiSimple(strings.Trim(strings.TrimSpace(value), `",`))
	}
	return 0
}

// --- plumbing between the case and its spawner child ---

const (
	// spawnerBinEnv / spawnerHomeEnv / spawnerSettingsEnv carry the fixture's
	// inputs to the child (the suite's own sandbox paths).
	spawnerBinEnv      = "GOPI_TEST_DAEMON_LEAK_BIN"
	spawnerHomeEnv     = "GOPI_TEST_DAEMON_LEAK_HOME"
	spawnerSettingsEnv = "GOPI_TEST_DAEMON_LEAK_SETTINGS"
	// spawnerReportEnv carries the fd number the child reports its daemon on
	// (the parent writes the number the child WILL see — os/exec renumbers
	// ExtraFiles from 3).
	spawnerReportEnv = "GOPI_TEST_DAEMON_LEAK_REPORT_FD"
	// spawnerBindEnv tells the spawner child whether to bind the daemon it
	// starts to its own lifetime.
	spawnerBindEnv = "GOPI_TEST_DAEMON_LEAK_BIND"
	// spawnerReportChildFD is the report pipe's number inside the child
	// (ExtraFiles[0]).
	spawnerReportChildFD = 3
)

// dtlEnv is the daemon's environment: the suite's sandbox profile, no compute
// peers, and the scratch sweep throttled out of the way.
func dtlEnv(home, settings string) []string {
	return stripAgentSignature(append(os.Environ(),
		"GOPI_HOME="+home,
		"GOPI_SETTINGS="+settings,
		workerIdleIntervalEnv+"="+dtlIdleSec,
		workerIdleExitsEnv+"="+dtlIdleExits,
	))
}

// spawnerFail reports and exits non-zero: a spawner that cannot do its job must
// not look like a case that passed.
func spawnerFail(msg string) {
	_, _ = os.Stderr.WriteString("daemon-leak spawner: " + msg + "\n") //stylegate:ignore spawner reporting on stderr before os.Exit; a write failure cannot be reported by a process already exiting
	_, _ = os.Stderr.WriteString("daemon-leak spawner: " + msg + "\n") //stylegate:ignore last-gasp diagnostic on a failing path whose exit code is the signal; a broken stderr cannot change it
	os.Exit(97)
}

// reportFDForSpawner reads the report fd number out of the environment.
func reportFDForSpawner() (int, error) {
	n := atoiSimple(os.Getenv(spawnerReportEnv))
	if n <= 0 {
		return 0, errors.New(spawnerReportEnv + " is not set to a usable fd")
	}
	return n, nil
}

// reportDaemonPid writes "<pid> <bound>" on the report pipe.
func reportDaemonPid(fd, pid int, bound bool) {
	f := os.NewFile(uintptr(fd), spawnerReportEnv)
	if f == nil {
		spawnerFail("report fd is not usable")
	}
	defer f.Close()
	boundFlag := "0"
	if bound {
		boundFlag = "1"
	}
	if _, err := f.WriteString(itoaSimple(pid) + " " + boundFlag + "\n"); err != nil {
		spawnerFail("report write: " + err.Error())
	}
}

// itoaSimple / atoiSimple keep this file free of a strconv import at its call
// sites (the numbers are small and non-negative).
func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 && i > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func atoiSimple(s string) int {
	s = strings.TrimSpace(s)
	n := 0
	seen := false
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			if seen {
				break
			}
			continue
		}
		seen = true
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// --- the cases ---

// TestDaemonDiesWhenItsSpawnerIsSIGKILLed is the epic's acceptance case.
func TestDaemonDiesWhenItsSpawnerIsSIGKILLed(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	fixture := startLeakSpawner(t, bin, home, settings, true)

	pid, bound := fixture.awaitReport(t)
	if !bound {
		t.Fatalf("the spawner reported an UNBOUND daemon — this case must exercise the binding")
	}
	// The daemon must be alive BEFORE the kill: without this, "it is gone
	// afterwards" would also pass for a daemon that never started.
	id := awaitProcIdentity(t, pid)
	if reaper, reason := testutil.ProcReaperOf(); reaper > 0 {
		if id.PPID != reaper {
			t.Logf("daemon %d ppid=%d (a detached daemon is normally reparented to the process that reaps this host: %d)", pid, id.PPID, reaper)
		}
	} else {
		t.Logf("daemon %d ppid=%d (this host's reaper cannot be named: %s)", pid, id.PPID, reason)
	}
	t.Logf("daemon pid %d is alive and bound: %s", pid, id)

	// SIGKILL the spawner: no handler, no defer, no t.Cleanup — the harness's
	// kill, reproduced.
	// The spawner is OUR child, but the pid is still a NUMBER the kernel
	// recycles: read the incarnation at delivery time and let the guard
	// refuse a pid that now names somebody else (Rule 42).
	sigID, sigErr := testutil.ReadProcIdentity(fixture.Pid)
	if sigErr != nil {
		t.Fatalf("read the spawner's identity (pid %d): %v", fixture.Pid, sigErr)
	}
	if err := testutil.KillIfIdentical(sigID, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL the spawner pid %d: %v", fixture.Pid, err)
	}
	_, _ = fixture.Cmd.Process.Wait() //stylegate:ignore fixture teardown reap of a child the test already asserted on; the exit status is not the subject
	_, _ = fixture.Cmd.Process.Wait() //stylegate:ignore reaping the SIGKILLed spawner so the leak observation that follows is not raced by a zombie; its status carries no assertion
	t.Logf("spawner pid %d SIGKILLed; the daemon pid %d must follow it with no cleanup code running", fixture.Pid, pid)

	// The daemon must die on its own (the kernel closed the spawner's write end).
	if !awaitIdentityGone(t, id, dtlDaemonWait) {
		stopByPid(t, pid) // leave no leak behind on failure
		t.Fatalf("daemon pid %d SURVIVED its spawner's SIGKILL — this is exactly the leak the epic is about (%s)", pid, id)
	}
	t.Logf("daemon pid %d died with its spawner — bound, verified by the triple", pid)

	// And the sandbox must be free of orphans: the sweeper's own verdict on the
	// scene, which is what the next suite's TestMain would run.
	assertNoOrphansInSandbox(t, home)
}

// TestBoundDaemonStaysAliveWhileItsSpawnerLives is the negative half. Without
// it a watchdog that fired immediately would satisfy the case above.
func TestBoundDaemonStaysAliveWhileItsSpawnerLives(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	fixture := startLeakSpawner(t, bin, home, settings, true)

	pid, bound := fixture.awaitReport(t)
	if !bound {
		t.Fatalf("the spawner reported an UNBOUND daemon")
	}
	id := awaitProcIdentity(t, pid)
	// Observe for longer than the daemon's idle budget so a "still alive" here
	// cannot be an artefact of it not having started yet.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if alive, reason := id.Alive(); !alive {
			t.Fatalf("the bound daemon %d died while its spawner was alive: %s", pid, reason)
		}
		time.Sleep(dtlPollInterval) //stylegate:allow-sleep bounded observation of a live daemon
	}
	t.Logf("daemon pid %d stayed alive for %s while its spawner held the death pipe", pid, 3*time.Second)

	// Cleanup: kill the spawner (which kills the daemon THROUGH the binding —
	// the point) and assert the daemon followed, so this case leaves nothing.
	// The spawner is OUR child, but the pid is still a NUMBER the kernel
	// recycles: read the incarnation at delivery time and let the guard
	// refuse a pid that now names somebody else (Rule 42).
	sigID, sigErr := testutil.ReadProcIdentity(fixture.Pid)
	if sigErr != nil {
		t.Fatalf("read the spawner's identity (pid %d): %v", fixture.Pid, sigErr)
	}
	if err := testutil.KillIfIdentical(sigID, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL the spawner: %v", err)
	}
	_, _ = fixture.Cmd.Process.Wait() //stylegate:ignore fixture teardown reap of a child the test already asserted on; the exit status is not the subject
	_, _ = fixture.Cmd.Process.Wait() //stylegate:ignore reaping the SIGKILLed spawner so the leak observation that follows is not raced by a zombie; its status carries no assertion
	if !awaitIdentityGone(t, id, dtlDaemonWait) {
		stopByPid(t, pid)
		t.Fatalf("daemon %d survived its spawner even in the control case", pid)
	}
}

// TestUnboundDaemonSurvivesItsSpawner is the PRODUCTION-shape control: with no
// binding armed (nothing sets the spawn-side variable), a daemon must behave
// exactly as before — i.e. outlive the process that started it. This is the
// property that keeps an operator's `gopi worker --daemon` unchanged, and it is
// why "the daemon died" in the case above is evidence rather than an accident.
func TestUnboundDaemonSurvivesItsSpawner(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	fixture := startLeakSpawner(t, bin, home, settings, false)

	pid, bound := fixture.awaitReport(t)
	if bound {
		t.Fatalf("the control case armed the binding — it must start an unbound daemon")
	}
	id := awaitProcIdentity(t, pid)
	// The spawner is OUR child, but the pid is still a NUMBER the kernel
	// recycles: read the incarnation at delivery time and let the guard
	// refuse a pid that now names somebody else (Rule 42).
	sigID, sigErr := testutil.ReadProcIdentity(fixture.Pid)
	if sigErr != nil {
		t.Fatalf("read the spawner's identity (pid %d): %v", fixture.Pid, sigErr)
	}
	if err := testutil.KillIfIdentical(sigID, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL the spawner: %v", err)
	}
	_, _ = fixture.Cmd.Process.Wait() //stylegate:ignore fixture teardown reap of a child the test already asserted on; the exit status is not the subject
	_, _ = fixture.Cmd.Process.Wait() //stylegate:ignore reaping the SIGKILLed spawner so the leak observation that follows is not raced by a zombie; its status carries no assertion
	// The daemon must OUTLIVE it (that is the production contract). Observe for
	// a window well inside its idle budget.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if alive, reason := id.Alive(); !alive {
			t.Fatalf("the UNBOUND daemon %d died with its spawner (%s) — the binding must be opt-in, or operator daemons change behaviour", pid, reason)
		}
		time.Sleep(dtlPollInterval) //stylegate:allow-sleep bounded observation of an unbound daemon
	}
	t.Logf("unbound daemon pid %d survived its spawner as before — the mechanism is opt-in", pid)
	stopByPid(t, pid) // this case's daemon has no binding to clean it up: stop it explicitly
	if !awaitIdentityGone(t, id, dtlDaemonWait) {
		t.Errorf("the unbound daemon %d did not stop on request", pid)
	}
}

// --- fixtures ---

// leakSpawner is a running spawner child.
type leakSpawner struct {
	Cmd    *exec.Cmd
	Pid    int
	report *os.File
}

// startLeakSpawner starts the re-exec'd spawner with (or without) a death pipe.
func startLeakSpawner(t *testing.T, bin, home, settings string, bound bool) *leakSpawner {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate this test binary: %v", err)
	}
	reportR, reportW, err := os.Pipe()
	if err != nil {
		t.Fatalf("report pipe: %v", err)
	}
	extra := []*os.File{reportW}
	env := append(stripAgentSignature(os.Environ()),
		spawnerEnvGuard+"=1",
		spawnerBinEnv+"="+bin,
		spawnerHomeEnv+"="+home,
		spawnerSettingsEnv+"="+settings,
		spawnerReportEnv+"="+itoaSimple(spawnerReportChildFD),
	)
	bindFlag := "0"
	if bound {
		// The death pipe is created BY THE SPAWNER (in the child), not here:
		// the binding is "the pipe's write end dies with the process it is
		// meant to track", so only the spawner may hold it. This process only
		// says whether the binding is wanted.
		bindFlag = "1"
	}
	env = append(env, spawnerBindEnv+"="+bindFlag)
	cmd := exec.Command(self, "-test.run", "^$")
	cmd.Env = env
	cmd.ExtraFiles = extra
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the spawner: %v", err)
	}
	_ = reportW.Close() //stylegate:ignore the child holds the report write end; the parent's copy would keep the read open forever
	s := &leakSpawner{Cmd: cmd, Pid: cmd.Process.Pid, report: reportR}
	t.Cleanup(func() {
		// A spawner still alive at cleanup (a failing case) must not be left
		// behind: kill and reap it, and its bound daemon follows via the pipe.
		if sigID, sigErr := testutil.ReadProcIdentity(s.Pid); sigErr == nil {
			_ = testutil.KillIfIdentical(sigID, syscall.SIGKILL) //stylegate:ignore best-effort cleanup of a failed-case spawner; the guard refuses a recycled pid
			_, _ = s.Cmd.Process.Wait()
		}
		_ = s.report.Close() //stylegate:ignore teardown close of a read end that carries no more data
	})
	return s
}

// awaitReport reads "<pid> <bound>" from the spawner.
func (s *leakSpawner) awaitReport(t *testing.T) (int, bool) {
	t.Helper()
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		buf := make([]byte, 64)
		n, err := s.report.Read(buf)
		ch <- res{line: string(buf[:n]), err: err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read the spawner's report: %v", r.err)
		}
		fields := strings.Fields(r.line)
		if len(fields) < 2 {
			t.Fatalf("the spawner reported %q, want \"<pid> <bound>\"", r.line)
		}
		return atoiSimple(fields[0]), fields[1] == "1"
	case <-time.After(dtlSpawnerWait):
		t.Fatalf("the spawner never reported within %s", dtlSpawnerWait)
		return 0, false
	}
}

// awaitProcIdentity reads a live process's triple, with a bounded wait (the
// daemon may still be finishing its startup).
func awaitProcIdentity(t *testing.T, pid int) testutil.ProcIdentity {
	t.Helper()
	deadline := time.Now().Add(dtlDaemonWait)
	for time.Now().Before(deadline) {
		id, err := testutil.ReadProcIdentity(pid)
		if err == nil {
			return id
		}
		time.Sleep(dtlPollInterval) //stylegate:allow-sleep bounded wait for a /proc entry
	}
	t.Fatalf("pid %d has no readable /proc entry after %s", pid, dtlDaemonWait)
	return testutil.ProcIdentity{}
}

// awaitIdentityGone polls the TRIPLE until the process is gone (a recycled pid
// must never read as a survivor).
func awaitIdentityGone(t *testing.T, id testutil.ProcIdentity, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if alive, _ := id.Alive(); !alive {
			return true
		}
		time.Sleep(dtlPollInterval) //stylegate:allow-sleep bounded wait for a bound daemon to die
	}
	return false
}

// stopByPid stops a daemon by EXPLICIT pid with provenance (the house rule: a
// SIGKILL leaves the victim no chance to record anything, so the killer does).
// The provenance is written to the TEST LOG before the signal — deliberately not
// to a session-scoped sink, which a leaked fixture has no session directory for
// (measured: signalrec refuses without a session dir, and that refusal must not
// be the reason a cleanup is skipped).
func stopByPid(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return
	}
	t.Logf("stopping daemon pid %d (SIGTERM) — this case's own probe, no binding to clean it up", pid)
	sigID, sigErr := testutil.ReadProcIdentity(pid)
	if sigErr != nil {
		return // already gone: there is no incarnation left to address
	}
	//stylegate:ignore best-effort stop of this case's own probe daemon by explicit pid; the guard refuses a recycled one
	_ = testutil.KillIfIdentical(sigID, syscall.SIGTERM)
}

// assertNoOrphansInSandbox runs the sweep over the sandbox and requires zero
// rows: the backstop's own verdict, which is what a later suite would see.
func assertNoOrphansInSandbox(t *testing.T, home string) {
	t.Helper()
	// Give the kernel a moment to finish the teardown the case just triggered
	// before declaring the scene clean.
	deadline := time.Now().Add(dtlDaemonWait)
	for time.Now().Before(deadline) {
		orphans, err := testutil.FindOrphansUnderRoot(home)
		if err != nil {
			t.Fatalf("sweep %s: %v", home, err)
		}
		if len(orphans) == 0 {
			return
		}
		time.Sleep(dtlPollInterval) //stylegate:allow-sleep bounded wait for the sandbox to drain
	}
	orphans, _ := testutil.FindOrphansUnderRoot(home) //stylegate:ignore diagnostic re-sweep immediately before the t.Fatalf it explains; its error cannot be reported separately
	for _, o := range orphans {
		t.Errorf("orphan left in %s: %s (proof %s)", home, o.Identity, o.Proof)
	}
	t.Fatalf("the sandbox %s still holds %d orphan daemon(s) after the spawner's SIGKILL", home, len(orphans))
}
