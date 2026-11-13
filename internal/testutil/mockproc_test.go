package testutil

// Reaper tests: the creator cleans up what it spawned, and NOTHING ELSE.
//
// Three boundaries are asserted here, each one a data-loss/liveness hazard if
// it regresses:
//
//   - AFTER the test that spawned a mock, the mock's pid is unreachable —
//     on the normal path AND on the panic path (a failing assertion must not
//     be the moment a daemon escapes);
//   - the reaper never signals a process it did not spawn (a foreign process
//     started OUTSIDE the handle must survive Reap);
//   - a handle whose group cannot be proven safe (our own group, or the
//     degenerate <= 1) refuses to signal AT ALL.
//
// The fixtures are real processes (a re-exec'd test binary, and `sleep`
// where available) so every assertion is made against the kernel's view —
// kill(pid, 0) / kill(-pgid, 0) — not against the reaper's own bookkeeping.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// mockHolderEnvMarker identifies the re-exec'd child whose only job is to
	// sleep (it is the "long-lived daemon" shape in miniature).
	mockHolderEnvMarker = "GOPI_TESTUTIL_MOCKPROC_HOLDER"
	// mockHolderTest is the test the child runs; the marker env gates the body
	// so a normal run of this suite never reaches it.
	mockHolderTest   = "TestHelperMockProcessHolder"
	mockHolderPrefix = "-test.run=^" + mockHolderTest + "$"

	// mockSleepBinary / mockSleepSeconds are the external fixture used where a
	// plain long-lived process is clearer than a re-exec.
	mockSleepBinary = "sleep"
	// shellBinary runs the daemon fixture (a shell that backgrounds a child).
	shellBinary      = "/bin/sh"
	mockSleepSeconds = "120"
	// mockDaemonSeconds is the grandchild's sleep: long enough that seeing it
	// after the reaper ran can only mean the group kill missed it.
	mockDaemonSeconds = "300"

	// mockExitWait bounds waiting for a reaped process to become unreachable
	// (reaping is asynchronous to the signal).
	mockExitWait     = 5 * time.Second
	mockPollInterval = 10 * time.Millisecond
)

// reexecHolderCmd builds a command that re-execs THIS test binary as a
// long-lived mock: `-test.run` picks the holder body, the env marker makes it
// sleep, and the parent keeps full control of the process group.
//
// The child gets a SCRATCH HOME of its own and a pinned TMPDIR: a re-exec'd
// test binary is a suite, so without the pin its TestMain would sweep the
// developer's real scratch tree and could refuse the whole run as unsafe
// (exit 97). Giving it a fixture home keeps this test's footprint inside the
// test — the /tmp ban's own rule, applied to the reaper's fixtures.
func reexecHolderCmd(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], mockHolderPrefix)
	cmd.Env = append(os.Environ(),
		mockHolderEnvMarker+"=1",
		SuiteTempRootEnvName+"="+t.TempDir(),
		TempDirEnvName+"="+t.TempDir(),
	)
	return cmd
}

// TestHelperMockProcessHolder is the mock's body: it sleeps until it is
// killed. Without the marker (a normal suite run) it skips immediately, so the
// helper test is invisible unless a test spawns it.
func TestHelperMockProcessHolder(t *testing.T) {
	if os.Getenv(mockHolderEnvMarker) == "" {
		t.Skip("mock-process holder: only runs when spawned by the reaper tests")
	}
	if os.Getenv(mockHolderExitNowEnv) != "" {
		return // the already-exited fixture: the test ends, the process exits
	}
	time.Sleep(holderSleepWindow) //stylegate:allow-sleep a synthetic long-lived holder: the sleep IS the process lifetime under test, not a wait for a condition
}

// mockHolderExitNowEnv makes the holder exit on its own instead of sleeping —
// the "process is already gone" fixture.
const mockHolderExitNowEnv = "GOPI_TESTUTIL_MOCKPROC_EXIT_NOW"

// holderSleepWindow is the mock's lifetime — far longer than any test below,
// so "the mock is gone" can only be the reaper's doing.
const holderSleepWindow = 60 * time.Second

// waitUnreachable polls until kill(pid, 0) reports ESRCH (the pid is fully
// reaped and can no longer be signalled).
func waitUnreachable(pid int, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(mockPollInterval) //stylegate:allow-sleep poll interval while the process group disappears (kill(2) is the only probe)
	}
}

// groupUnreachable is waitUnreachable for a whole process group.
func groupUnreachable(pgid int, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for {
		if err := syscall.Kill(-pgid, 0); err != nil {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(mockPollInterval) //stylegate:allow-sleep poll interval while the process group disappears (kill(2) is the only probe)
	}
}

// TestStartMockProcessSpawnsOwnGroup pins the spawn CONTRACT the reaper's
// safety rests on: the child leads its own process group, so kill(-pgid)
// addresses exactly the mock and its BLOODLINE — never the test runner and
// never a sibling.
//
// Liveness is probed with kill(-pgid, 0), not kill(pid, 0): a terminated child
// that has not been waited for is a ZOMBIE, and kill(2) on a zombie SUCCEEDS
// (the pid stays a group member until the wait), so a pid probe would read
// "alive" for a process that is already dead and unreaped. Group emptiness is
// the signal that the mock and everything it forked are actually gone.
func TestStartMockProcessSpawnsOwnGroup(t *testing.T) {
	cmd := reexecHolderCmd(t)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // the contract, asserted not assumed
	mp := StartMockProcessCmd(t, cmd)
	if mp.Pid() <= 0 {
		t.Fatalf("mock pid = %d, want > 0", mp.Pid())
	}
	if mp.Pgid() != mp.Pid() {
		t.Fatalf("mock pgid = %d, want %d (its own pid) — the child was not made a group leader",
			mp.Pgid(), mp.Pid())
	}
	if mp.Pgid() == syscall.Getpgrp() {
		t.Fatalf("mock pgid %d equals the TEST's process group — a reap would kill the test run", mp.Pgid())
	}
	if !mp.Alive() {
		t.Fatal("the freshly spawned mock is not alive — the fixture proves nothing")
	}
	// The explicit reap here is also the IDEMPOTENCE proof: t.Cleanup will
	// call Reap again at the end of this test.
	mp.Reap()
	if !waitUnreachable(mp.Pid(), mockExitWait) {
		t.Fatalf("mock pid %d survived an explicit Reap", mp.Pid())
	}
	mp.Reap() // second call: must be a no-op, never a second kill of a recycled pgid
}

// TestReapIsIdempotentOnAlreadyExitedProcess covers the repeated-cleanup and
// already-exited boundary in one place: the mock exits on its own, then Reap
// runs three times — no panic, no failure, no signal to a recycled id.
func TestReapIsIdempotentOnAlreadyExitedProcess(t *testing.T) {
	cmd := reexecHolderCmd(t)
	cmd.Env = append(cmd.Env, mockHolderExitNowEnv+"=1")
	mp := StartMockProcessCmd(t, cmd)
	// The child exits immediately (the marker makes the holder body return
	// without sleeping). Reap it here — an unreaped child is a ZOMBIE, and a
	// zombie still answers kill(pid, 0), so "exited" is only observable once
	// the parent has waited for it.
	if err := mp.Cmd().Wait(); err != nil {
		t.Fatalf("the exit-now mock did not exit cleanly: %v", err)
	}
	if err := syscall.Kill(mp.Pid(), 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("mock pid %d is still reachable after it exited and was reaped (kill(pid,0) = %v)", mp.Pid(), err)
	}
	// Three Reaps on an already-exited, already-reaped group: no panic, no
	// signal to a recycled id, no failure of a test that is already finishing.
	mp.Reap()
	mp.Reap()
	mp.Reap()
	if mp.Alive() {
		t.Fatalf("Alive() reports a live group %d after the process exited and was reaped", mp.Pgid())
	}
}

// TestStartMockProcessCmdKeepsASetsidCallersOwnGroup is the regression guard
// for a spawn that fails OUTRIGHT: setsid(2) already makes the child a session
// leader owning its own group, and a simultaneous setpgid(2) on a session
// leader returns EPERM — so a helper that unconditionally sets Setpgid makes
// every Setsid caller (the X-server fixtures) fail to start with "operation
// not permitted". Measured, not theorised. The own-group guarantee must
// survive the caller's Setsid either way.
func TestStartMockProcessCmdKeepsASetsidCallersOwnGroup(t *testing.T) {
	cmd := reexecHolderCmd(t)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	mp := StartMockProcessCmd(t, cmd) // fatal (no skip) if the spawn is refused
	if mp.Cmd().SysProcAttr.Setpgid {
		t.Errorf("Setpgid was forced on a Setsid command — the pair fails with EPERM; got %+v", mp.Cmd().SysProcAttr)
	}
	pgid, err := syscall.Getpgid(mp.Pid())
	if err != nil {
		t.Fatalf("Getpgid(%d): %v", mp.Pid(), err)
	}
	if pgid != mp.Pid() {
		t.Fatalf("a Setsid caller's mock is in group %d, want its own %d — the reaper would signal someone else", pgid, mp.Pid())
	}
	if got := mp.Pgid(); got != mp.Pid() {
		t.Fatalf("handle pgid = %d, want %d (its own group)", got, mp.Pid())
	}
	mp.Reap()
	if !waitUnreachable(mp.Pid(), mockExitWait) {
		t.Fatalf("mock pid %d survived Reap", mp.Pid())
	}
}

// TestReapNeverTouchesAForeignProcess is the SCOPING proof: a process the
// handle did not spawn — a plain child of THIS test — must be untouched by
// Reap, even though it is alive in the same moment.
// TestStartMockProcessDirRunsTheChildInTheGivenDirectory guards the shape the
// live-CWD holder fixtures depend on: the process is spawned in the requested
// working directory (the GC's holder scan reads /proc/<pid>/cwd, so a holder
// whose cwd silently defaulted would protect nothing), and it is reaped like
// any other mock.
func TestStartMockProcessDirRunsTheChildInTheGivenDirectory(t *testing.T) {
	dir := t.TempDir()
	mp := StartMockProcessDir(t, dir, mockSleepBinary, mockSleepSeconds)
	if !mp.Alive() {
		t.Fatal("the freshly spawned mock is not alive — the fixture proves nothing")
	}
	cwd, err := os.Readlink("/proc/" + strconv.Itoa(mp.Pid()) + "/cwd")
	if err != nil {
		t.Fatalf("read the mock's cwd: %v", err)
	}
	// /proc reports the physical path (a temp dir may be reached through a
	// symlinked root), so compare the resolved forms.
	wantResolved := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		wantResolved = resolved
	}
	if cwd != wantResolved {
		t.Fatalf("mock cwd = %q, want %q — a CWD holder in the wrong directory protects nothing", cwd, wantResolved)
	}
	mp.Reap()
	if !groupUnreachable(mp.Pgid(), mockExitWait) {
		t.Fatalf("mock group %d survived Reap", mp.Pgid())
	}
}

// TestStartMockProcessIsTheCmdFormWithABareCommand pins the convenience
// wrapper every simple call site uses: a bare command must get the SAME
// contract as StartMockProcessCmd (own group, Reap available, cleaned up at
// test end).
func TestStartMockProcessIsTheCmdFormWithABareCommand(t *testing.T) {
	mp := StartMockProcess(t, mockSleepBinary, mockSleepSeconds)
	if mp.Pid() <= 0 || mp.Pgid() != mp.Pid() {
		t.Fatalf("StartMockProcess produced pid=%d pgid=%d, want a leader in its own group", mp.Pid(), mp.Pgid())
	}
	if !mp.Alive() {
		t.Fatal("the wrapper's mock is not alive")
	}
	mp.Reap()
	if !waitUnreachable(mp.Pid(), mockExitWait) {
		t.Fatalf("mock pid %d survived Reap", mp.Pid())
	}
}

func TestReapNeverTouchesAForeignProcess(t *testing.T) {
	foreign := exec.Command(mockSleepBinary, mockSleepSeconds)
	if err := foreign.Start(); err != nil {
		t.Skipf("cannot spawn the foreign fixture (%s): %v", mockSleepBinary, err)
	}
	foreignPid := foreign.Process.Pid
	t.Cleanup(func() {
		_ = foreign.Process.Kill()    //stylegate:ignore kills the fixture process the test spawned
		_, _ = foreign.Process.Wait() //stylegate:ignore reaps the fixture process the test spawned
	})

	mp := StartMockProcessCmd(t, reexecHolderCmd(t))
	mockPid := mp.Pid()
	mp.Reap()

	if !waitUnreachable(mockPid, mockExitWait) {
		t.Fatalf("the mock %d survived its own Reap", mockPid)
	}
	if err := syscall.Kill(foreignPid, 0); err != nil {
		t.Fatalf("the reaper killed a process it did NOT spawn (pid %d): %v — the kill is not group-scoped",
			foreignPid, err)
	}
}

// TestReapRefusesItsOwnProcessGroup is the self-kill guard. Setpgid makes this
// unreachable through StartMockProcessCmd, so the handle is built directly —
// which is exactly why the guard exists (a future edit that loses Setpgid
// would otherwise turn every cleanup into a kill of the whole test binary).
func TestReapRefusesItsOwnProcessGroup(t *testing.T) {
	mp := &MockProcess{cmd: exec.Command(mockSleepBinary, mockSleepSeconds), pgid: syscall.Getpgrp()}
	mp.logf = t.Logf
	mp.Reap() // must return without signalling anything
	if syscall.Kill(syscall.Getpgrp(), 0) != nil {
		t.Fatal("the reaper signalled its OWN process group")
	}
}

// TestReapRefusesDegenerateGroupID covers kill(-0) / kill(-1): both would
// signal this process's group or every process of the user.
func TestReapRefusesDegenerateGroupID(t *testing.T) {
	for _, pgid := range []int{0, 1} {
		mp := &MockProcess{cmd: exec.Command(mockSleepBinary, mockSleepSeconds), pgid: pgid}
		mp.logf = t.Logf
		mp.Reap()
	}
	if syscall.Kill(syscall.Getpgrp(), 0) != nil {
		t.Fatal("a degenerate process group id reached a real signal")
	}
}

// TestStartMockProcessCmdRefusesAPresetPgid closes the other self-kill route:
// a caller that pre-sets SysProcAttr.Pgid asks for the child to JOIN an
// existing group — when that group is the test's, cleanup would kill the run.
// Nothing is started (the refusal fires before Start), which is asserted via
// the fatal-free part: the test simply must not reach the assertion after it.
func TestStartMockProcessCmdRefusesAPresetPgid(t *testing.T) {
	// The refusal is t.Fatalf, which must not be triggered in this test — so
	// the hazard is demonstrated on a SUBPROCESS: the re-exec'd child runs the
	// same check and must fail visibly rather than spawn into a shared group.
	if os.Getenv(refusalProbeEnv) != "" {
		t.Skip("already in the refusal probe child")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+refusalProbeTest+"$")
	cmd.Env = append(os.Environ(), refusalProbeEnv+"=1",
		SuiteTempRootEnvName+"="+t.TempDir(), TempDirEnvName+"="+t.TempDir())
	out, _ := cmd.CombinedOutput() //stylegate:ignore captures the child's output; the assertions below read the text, so a failure surfaces as an empty buffer
	if !strings.Contains(string(out), "must own the group it kills") {
		t.Fatalf("the preset-Pgid refusal never fired in the probe child:\n%s", out)
	}
}

const (
	// refusalProbeEnv gates the child that runs the refusal probe.
	refusalProbeEnv = "GOPI_TESTUTIL_MOCKPROC_REFUSAL_PROBE"
	// refusalProbeTest is the probe body inside the child.
	refusalProbeTest = "TestHelperMockProcessPresetPgidProbe"
)

// TestHelperMockProcessPresetPgidProbe asserts the refusal the parent checks
// for: a command carrying a preset Pgid must be rejected by the helper. It
// runs in a child binary so the t.Fatalf under test does not fail this suite.
func TestHelperMockProcessPresetPgidProbe(t *testing.T) {
	if os.Getenv(refusalProbeEnv) == "" {
		t.Skip("preset-Pgid refusal probe: only runs as a spawned child")
	}
	cmd := exec.Command(mockSleepBinary, mockSleepSeconds)
	cmd.SysProcAttr = &syscall.SysProcAttr{Pgid: syscall.Getpgrp()}
	// t.Fatalf in this child fails only the child (its exit status is 1), and
	// its message is what the parent asserts on.
	StartMockProcessCmd(t, cmd)
	t.Error("StartMockProcessCmd accepted a preset Pgid — the caller could have joined the test's own group")
}

// TestReaperKillsTheWholeSpawnedGroup is the GRANDCHILD leg: a mock that
// forks a detached grandchild (the `nohup ... &` daemon shape) must take the
// grandchild down too — killing only the direct child is the bug that leaves
// daemons behind.
func TestReaperKillsTheWholeSpawnedGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	// The mock starts, records this shell's pid, and execs a long sleep as a
	// BACKGROUND child of the same group; the mock then waits, so both stay
	// alive until the reaper signals the group.
	script := "echo $$ > " + pidFile + "; " + mockSleepBinary + " " + mockDaemonSeconds + " & wait"
	cmd := exec.Command(shellBinary, "-c", script)
	mp := StartMockProcessCmd(t, cmd)
	if !mp.Alive() {
		t.Fatal("the spawned mock is not alive")
	}
	grandchild := readPIDFile(t, pidFile)
	if grandchild == 0 {
		t.Fatal("the grandchild pid file was never written — the fixture is not exercising a daemon")
	}
	mp.Reap()
	if !groupUnreachable(mp.Pgid(), mockExitWait) {
		t.Fatalf("process group %d still has a live member after Reap", mp.Pgid())
	}
	if !waitUnreachable(grandchild, mockExitWait) {
		t.Fatalf("grandchild pid %d (the nohup-style daemon) survived the group reap — killing only the direct child leaks daemons",
			grandchild)
	}
}

// readPIDFile waits briefly for a pid file to appear and returns its pid (0
// when it never appears).
func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(mockExitWait)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(mockPollInterval) //stylegate:allow-sleep poll interval while the mock writes its pid file
	}
	return 0
}

// TestStartReapedProcessReturnsADeadPid pins the shared zombie fixture: the
// pid it returns must already be unreachable, so every caller that builds a
// dead-pid fixture on top of it is honest.
func TestStartReapedProcessReturnsADeadPid(t *testing.T) {
	pid := StartReapedProcess(t, mockSleepBinary, mockSleepSeconds)
	if pid <= 0 {
		t.Fatalf("StartReapedProcess returned pid %d", pid)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("pid %d is still alive — the zombie fixture is not dead", pid)
	}
}

// TestMockProcessReapedOnPanicPath is the deliverable's headline assertion for
// abnormal exits: a test that PANICS after spawning a mock must still have its
// mock reaped. The panic happens in a RE-EXEC'd child binary (a panic in this
// process would take the whole suite down), which spawns the mock, panics, and
// leaves the mock's pid + pgid on disk for the parent to probe.
func TestMockProcessReapedOnPanicPath(t *testing.T) {
	if os.Getenv(panicProbeEnv) != "" {
		t.Skip("already in the panic probe child")
	}
	pidFile := filepath.Join(t.TempDir(), "mock.pid")
	cmd := exec.Command(os.Args[0], "-test.run=^"+panicProbeTest+"$")
	cmd.Env = append(os.Environ(), panicProbeEnv+"=1", panicPidFileEnv+"="+pidFile,
		SuiteTempRootEnvName+"="+t.TempDir(), TempDirEnvName+"="+t.TempDir())
	out, _ := cmd.CombinedOutput() //stylegate:ignore captures the child's output; the assertions below read the text, so a failure surfaces as an empty buffer
	// The child MUST have failed (that is the panic); a clean exit means the
	// panic never happened and the assertion below would be vacuous.
	if code := exitCodeOfErr(cmd); code == 0 {
		t.Fatalf("the panic probe child exited 0 — the panic path was never exercised:\n%s", out)
	}
	mockPid := readPIDFile(t, pidFile)
	if mockPid == 0 {
		t.Fatalf("the panic probe child never recorded its mock pid:\n%s", out)
	}
	if !waitUnreachable(mockPid, mockExitWait) {
		t.Fatalf("mock pid %d outlived the PANICKING test — cleanup does not run on the panic path", mockPid)
	}
}

const (
	// panicProbeEnv gates the child that panics after spawning a mock.
	panicProbeEnv = "GOPI_TESTUTIL_MOCKPROC_PANIC_PROBE"
	// panicProbeTest is the panicking body inside that child.
	panicProbeTest = "TestHelperMockProcessSpawnThenPanic"
	// panicPidFileEnv tells the child where to write the mock's pid.
	panicPidFileEnv = "GOPI_TESTUTIL_MOCKPROC_PIDFILE"
)

// TestHelperMockProcessSpawnThenPanic spawns a long-lived mock and panics. Its
// only purpose is to be run as a child by TestMockProcessReapedOnPanicPath.
func TestHelperMockProcessSpawnThenPanic(t *testing.T) {
	if os.Getenv(panicProbeEnv) == "" {
		t.Skip("panic probe: only runs as a spawned child")
	}
	mp := StartMockProcessCmd(t, reexecHolderCmd(t))
	pidFile := os.Getenv(panicPidFileEnv)
	if pidFile == "" {
		t.Fatal("no pid file configured")
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(mp.Pid())), 0o644); err != nil {
		t.Fatalf("record mock pid: %v", err)
	}
	panic("deliberate panic: the mock must still be reaped by t.Cleanup")
}

// exitCodeOfErr extracts a child's exit code (0 when it exited cleanly).
func exitCodeOfErr(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

// TestAllowStartFailureSkipsVisiblyAndRunsTheHook pins the opt-in deviation
// from the default contract: an OPTIONAL mock (an X server, xev) that cannot be
// spawned must SKIP the test VISIBLY — never fail it, never pass it silently —
// and any OnSkip hook must run BEFORE the skip unwinds so a resource opened for
// the child (a log file) is not left behind.
//
// The probe runs in a CHILD binary and the parent reads the child's own output:
// t.Run reports a skipped subtest as "did not fail", so an in-process assertion
// cannot tell a visible SKIP from a silent success — the child's `--- SKIP`
// line and its exit code can, and they are the same surface a developer sees.
// The default (hard failure on a missing fixture) is NOT exercised here for the
// same reason it exists: it is a t.Fatalf, proven by the preset-Pgid probe
// child (TestStartMockProcessCmdRefusesAPresetPgid) instead of by failing this
// suite.
func TestAllowStartFailureSkipsVisiblyAndRunsTheHook(t *testing.T) {
	if os.Getenv(optionalSkipProbeEnv) != "" {
		t.Skip("already inside the optional-skip probe child")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+optionalSkipProbeTest+"$", "-test.v")
	cmd.Env = append(os.Environ(), optionalSkipProbeEnv+"=1",
		SuiteTempRootEnvName+"="+t.TempDir(), TempDirEnvName+"="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the optional-skip probe child failed (%v) — a missing OPTIONAL fixture must skip, not fail:\n%s", err, out)
	}
	if !strings.Contains(string(out), "--- SKIP") {
		t.Fatalf("the missing optional fixture did not produce a visible SKIP — silence there is the Rule 15a trap:\n%s", out)
	}
	if !strings.Contains(string(out), optionalSkipHookMarker) {
		t.Fatalf("OnSkip did not run before the skip — a resource opened for the mock would leak:\n%s", out)
	}
}

const (
	// optionalSkipProbeEnv gates the child that exercises AllowStartFailure.
	optionalSkipProbeEnv = "GOPI_TESTUTIL_MOCKPROC_SKIP_PROBE"
	// optionalSkipProbeTest is the probe body inside that child.
	optionalSkipProbeTest = "TestHelperMockProcessOptionalSkipProbe"
	// optionalSkipHookMarker is what the OnSkip hook prints, so the parent can
	// prove the hook ran (and ran before the skip).
	optionalSkipHookMarker = "optional-skip hook ran"
	// nonexistentMockName is the fixture the probe tries (never created).
	nonexistentMockName = "gopi-no-such-mock-binary"
)

// TestHelperMockProcessOptionalSkipProbe is the probe body: it asks for an
// optional fixture that does not exist and must come out of it SKIPPED.
func TestHelperMockProcessOptionalSkipProbe(t *testing.T) {
	if os.Getenv(optionalSkipProbeEnv) == "" {
		t.Skip("optional-skip probe: only runs as a spawned child")
	}
	StartMockProcessCmd(t, exec.Command(filepath.Join(t.TempDir(), nonexistentMockName)),
		AllowStartFailure(OnSkip(func() { fmt.Println(optionalSkipHookMarker) })))
	t.Error("a missing optional fixture returned a handle — it must have skipped")
}
