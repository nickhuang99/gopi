package testutil

// The REGRESSION ARM for the 2026-10-27 leak (task 01a179d7): a fixture child
// outlived the `go test` binary that spawned it.
//
// The measured shape: SIGKILL (or `go test`'s own -timeout exit, which ends in
// os.Exit and runs no t.Cleanup either) killed a package's test binary while a
// fixture's `sleep 300` was alive. The child stayed, reparented to init, with
// its cwd inside the checkout — and a live process whose cwd is inside a
// finished run's ground is EXACTLY what the ground's live-holder veto reads as
// "still in use", so `gopi task status <row>` exited 1 forever. Two fixtures
// were measured leaking that way (TestSweepKeepsTheWorktreeOfALiveRecordedRunner
// and TestLiveClaimRunnerSurvivesWhenClaimStampIsUnresolvable, both through
// startLiveRunner in internal/tools) plus one through the package-wide run.
//
// Two production properties close it, and this arm measures BOTH, from the
// outside, on real processes:
//
//  1. the child dies when the test binary does — Pdeathsig, which is the
//     kernel's counter and the only one that survives SIGKILL;
//  2. the child's cwd is never inside the repository under test — so even a
//     specimen that somehow stayed could not make a ground look occupied.
//
// It is a REGRESSION arm, so it must be RED when the producer side is removed:
// delete the Pdeathsig assignment in StartMockProcessCmd and arm 1 fails naming
// the surviving pid.

import (
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
	// mockSpawnerChildEnv marks the RE-EXEC'd copy of this test binary as the
	// spawner child: without it the helper test below skips.
	mockSpawnerChildEnv = "GOPI_TESTUTIL_MOCK_SPAWNER_CHILD"
	// mockSpawnerPidFileEnv names the file the spawner child writes its
	// fixture's pid into, so the outer test observes a REAL pid rather than
	// guessing one.
	mockSpawnerPidFileEnv = "GOPI_TESTUTIL_MOCK_SPAWNER_PIDFILE"
	// mockSpawnerChildTestName is the helper test the outer arm re-execs.
	mockSpawnerChildTestName = "TestMockSpawnerChildStaysUntilKilled"
	// mockSpawnerChildHold bounds how long the spawner child blocks, so a
	// broken run cannot leave the suite waiting on it.
	mockSpawnerChildHold = 60 * time.Second
	// mockChildDeathGrace is how long the kernel gets to deliver Pdeathsig and
	// let the child be reaped after its parent is SIGKILLed.
	mockChildDeathGrace = 10 * time.Second
	// mockPidFilePollStep bounds the wait for the spawner child to publish.
	mockPidFilePollStep = 20 * time.Millisecond
	// mockPidFileWait bounds that wait as a whole.
	mockPidFileWait = 20 * time.Second
)

// TestMockSpawnerChildStaysUntilKilled is the SPAWNER half: it is not a test of
// anything — it exists only as the process the outer arm SIGKILLs, so that
// "the fixture's parent is gone" is a real event and not a simulation. It
// skips unless the outer arm asked for it.
func TestMockSpawnerChildStaysUntilKilled(t *testing.T) {
	if os.Getenv(mockSpawnerChildEnv) != "1" {
		t.Skip("helper for TestFixtureChildDiesWithItsKilledTestBinary; nothing to do when run directly")
	}
	pidFile := os.Getenv(mockSpawnerPidFileEnv)
	if pidFile == "" {
		t.Fatalf("%s is set but %s is empty — the outer arm cannot observe the fixture", mockSpawnerChildEnv, mockSpawnerPidFileEnv)
	}
	mp := StartMockProcess(t, MockDaemonBinary, MockDaemonHoldSeconds)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(mp.Pid())), 0o644); err != nil {
		t.Fatalf("publish the fixture pid: %v", err)
	}
	time.Sleep(mockSpawnerChildHold) //stylegate:allow-sleep deliberate HOLD, not a poll: this process exists only to stay alive until the outer arm SIGKILLs it, so the kill path (not a deadline) owns the outcome
}

// TestFixtureChildDiesWithItsKilledTestBinary is the arm itself.
func TestFixtureChildDiesWithItsKilledTestBinary(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "fixture.pid")
	child := exec.Command(os.Args[0], "-test.run", "^"+mockSpawnerChildTestName+"$", "-test.v")
	child.Env = append(os.Environ(), mockSpawnerChildEnv+"=1", mockSpawnerPidFileEnv+"="+pidFile)
	// The spawner child runs in THIS process's cwd on purpose: it is the
	// package directory, i.e. INSIDE the repository under test, which is the
	// shape that leaked. If the producer side stops neutralising the child's
	// cwd, the measurement below sees it.
	if err := child.Start(); err != nil {
		t.Fatalf("start the spawner child: %v", err)
	}
	// The outer arm owns this process and will kill it: a t.Cleanup is the
	// belt-and-braces path for the failures that return early.
	t.Cleanup(func() {
		_ = child.Process.Kill() //stylegate:ignore teardown of the process THIS test started; the verdict is read from /proc, not from its exit status
		_, _ = child.Process.Wait()
	})

	pid := waitFixturePid(t, pidFile, child.Process.Pid)
	repoLocal, cwd := fixtureCwd(t, pid)

	// Kill the SPAWNER the way a watchdog / an operator / `go test -timeout`
	// ends a hung package: SIGKILL, no cleanup code runs in the child.
	if err := child.Process.Kill(); err != nil {
		t.Fatalf("SIGKILL the spawner %d: %v", child.Process.Pid, err)
	}
	_, _ = child.Process.Wait() //stylegate:ignore the fixture child was just SIGKILLed on purpose, so its wait status IS the expected one and the deadline arms below carry the assertions

	// ARM 1 — the fixture must die with its test binary (Pdeathsig).
	deadline := time.Now().Add(mockChildDeathGrace)
	for incarnationAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(mockPidFilePollStep) //stylegate:allow-sleep bounded poll: the deadline above owns the outcome and the loop exits on the first observation
	}
	if incarnationAlive(pid) {
		t.Fatalf("the fixture child pid %d SURVIVED its test binary being SIGKILLed (cwd %q) — a leaked child "+
			"standing in the checkout is what makes a finished ground read IN USE forever; the spawn must arm "+
			"Pdeathsig so the kernel takes it down when the test binary dies", pid, cwd)
	}

	// ARM 2 — and it must never have stood inside the repository under test.
	// Read from the cwd captured while it was alive (the pid is gone now).
	if repoLocal {
		t.Fatalf("the fixture child pid %d was started with cwd %q, INSIDE the work tree under test: the ground "+
			"veto reads /proc/<pid>/cwd, so such a child makes its ground un-freeable even after the test that "+
			"spawned it has ended", pid, cwd)
	}
}

// waitFixturePid blocks until the spawner child publishes its fixture's pid.
func waitFixturePid(t *testing.T, pidFile string, spawnerPid int) int {
	t.Helper()
	deadline := time.Now().Add(mockPidFileWait)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil { // #nosec G304 -- the path is this test's own t.TempDir file
			if n, cerr := strconv.Atoi(strings.TrimSpace(string(b))); cerr == nil && n > 0 {
				return n
			}
		}
		if !incarnationAlive(spawnerPid) {
			t.Fatalf("the spawner child %d exited before publishing a fixture pid — the arm would prove nothing", spawnerPid)
		}
		time.Sleep(mockPidFilePollStep) //stylegate:allow-sleep bounded poll: the deadline above owns the outcome and the loop exits on the first observation
	}
	t.Fatalf("the spawner child never published a fixture pid in %s", mockPidFileWait)
	return 0
}

// fixtureCwd reports the child's cwd and whether it lies inside this package's
// repository (measured, never spelled: the work-tree root comes from git).
func fixtureCwd(t *testing.T, pid int) (repoLocal bool, cwd string) {
	t.Helper()
	deadline := time.Now().Add(mockPidFileWait)
	for time.Now().Before(deadline) {
		if link, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd"); err == nil {
			cwd = strings.TrimSuffix(link, " (deleted)")
			break
		}
		time.Sleep(mockPidFilePollStep) //stylegate:allow-sleep bounded poll: the deadline above owns the outcome and the loop exits on the first observation
	}
	if cwd == "" {
		t.Fatalf("could not read /proc/%d/cwd — the cwd arm cannot be measured", pid)
	}
	return pathInsideWorkTree(cwd), cwd
}

// incarnationAlive is the terminal predicate used here: a pid whose /proc entry
// is gone (or is a zombie) is dead. kill(pid, 0) is NOT used — a zombie answers
// it (memory #6854), and an unreaped child of a SIGKILLed process is exactly a
// zombie.
func incarnationAlive(pid int) bool {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false // the kernel released the pid: gone
	}
	// Field 3 (state) follows the parenthesised comm, which may itself contain
	// spaces or parentheses — split after the LAST ')'.
	s := string(raw)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return true // unparseable: do not claim death on an unreadable entry
	}
	switch s[i+2] {
	case 'Z', 'X':
		return false // a zombie is dead by this predicate
	}
	return true
}

// pathInsideWorkTree measures whether dir lies inside a git work tree, using
// git itself — the same criterion the tree has, never a path prefix guess.
func pathInsideWorkTree(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// TestStartMockProcessCmdArmsTheDeathSignal pins the two properties on the
// producer directly, so the arm is not the only reader of them: the child's
// SysProcAttr carries Pdeathsig, and a child spawned from inside this checkout
// does not inherit the checkout as its cwd.
func TestStartMockProcessCmdArmsTheDeathSignal(t *testing.T) {
	cmd := exec.Command(MockDaemonBinary, MockDaemonHoldSeconds)
	mp := StartMockProcessCmd(t, cmd)
	if got := mp.Cmd().SysProcAttr; got == nil || got.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("StartMockProcessCmd must arm Pdeathsig=SIGKILL on every child it starts (got %+v) — without it "+
			"the kernel takes no action when the test binary is SIGKILLed or exits via go test's -timeout", got)
	}
	link, err := os.Readlink("/proc/" + strconv.Itoa(mp.Pid()) + "/cwd")
	if err != nil {
		t.Fatalf("read the child's cwd: %v", err)
	}
	if pathInsideWorkTree(strings.TrimSuffix(link, " (deleted)")) && pathInsideWorkTree(mustGetwd(t)) {
		t.Fatalf("the child's cwd %q is inside the work tree under test — a fixture child standing there makes "+
			"the ground's (correct) live-holder veto report the finished ground as IN USE forever", link)
	}
}

// mustGetwd returns this process's cwd, skipping nothing: an unreadable cwd is
// an environment fault the caller must see.
func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}
