package testutil

// worker_reap_test.go — the regression guard for the LEAK half of the
// 2026-09-13 miscount ("why three workers when I ran `gopi worker --daemon`
// once?").
//
// THE DEFECT: TestGlmNightWatchOperatorPathStartsWorkersUnattended starts REAL
// detached workers through the real launcher, in a sandbox, and then returned —
// by design, as its own comment said ("Nothing is killed by this test"). The
// sandbox's HOME/GOPI_HOME is deleted with the test, so those workers poll a
// queue directory that no longer exists, forever, belonging to NO profile —
// while every profile's `gopi worker --status` counts them (the scan is
// machine-wide by argv shape). The operator's report showed exactly that: two
// fixtures with `--worker-log-root /dev/shm/gopi-test-scratch/…` sitting "idle"
// beside a pending queue that was not empty.
//
// THE FIX IS THE IDENTITY, NOT A PATTERN. The sandbox's own GOPI_HOME is the
// identity: comm == the gopi name AND argv[1] == the worker verb AND the
// worker's OWN environment resolves to THIS test's gopi dir. (It used to be the
// `--worker-log-root <sandbox>` argv pair; that flag is retired — epic
// child-stream-task-output — and the environment is the sturdier identity: it
// says what the process IS, not what its launcher typed.) A pattern ("all gopi
// workers") is the 04:12 incident class — it finds and kills the machine's real
// fleet.
//
// WHAT THIS TEST PINS (with three REAL processes, because identity is a fact
// about a running process and a fixture cannot stand in for it):
//
//	① the sandbox's own worker is FOUND by probeWorkerPIDs;
//	② a worker in a DIFFERENT profile is NOT — the epic's root cause in one
//	   assertion (same shape, different profile);
//	③ a FOREGROUND worker of the SAME profile is NOT — the detachment test;
//	④ the reap stops ① and leaves ② and ③ running (they are not this test's
//	   processes to stop), and ① is gone afterwards.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/procname"
)

// reapIdentityWait bounds the wait for a freshly started worker to appear with
// its exec'd argv (100ms × 50 = 5s). Named, never inline (Rule 1a).
const reapIdentityWait = probeReapPollInterval * probeReapPollBudget

func TestProbeWorkerReapIdentifiesBySandboxIdentity(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	root := repoRootForProcnameGate(t)
	bin := opPathWorkerBin(t, root) // skips when no binary can be built

	home := t.TempDir()
	gopiDir := filepath.Join(home, config.ProfileDirName)
	otherHome := filepath.Join(t.TempDir(), "another-profile-home")

	mine := startDetachedTestWorker(t, bin, home)
	other := startDetachedTestWorker(t, bin, otherHome)
	foreground := startForegroundTestWorker(t, bin, home)

	// ② / ③ the discrimination, before any kill: exactly ONE of the three
	// processes wears this sandbox's identity.
	got := probeWorkerPIDs(gopiDir)
	if len(got) != 1 || got[0] != mine {
		t.Fatalf("probeWorkerPIDs(%s) = %v, want exactly the sandbox's own worker %d — the other-profile worker (%d) "+
			"and the foreground one (%d) must NOT be reap candidates", gopiDir, got, mine, other, foreground)
	}

	// ④ the reap stops it and leaves the others alone.
	if reaped := reapProbeWorkers(t, gopiDir, bin); reaped != 1 {
		t.Fatalf("reapProbeWorkers stopped %d worker(s), want 1", reaped)
	}
	if left := probeWorkerPIDs(gopiDir); len(left) != 0 {
		t.Fatalf("the sandbox worker %d survived the reap: %v", mine, left)
	}
	// The OTHER profile's worker must still be alive — the assertion that the
	// identity, not a name pattern, decided the kill.
	if err := syscall.Kill(other, 0); err != nil {
		t.Fatalf("the other-profile worker %d was stopped by a reap that does not own it: %v", other, err)
	}
	for _, pid := range []int{other, foreground} {
		if err := syscall.Kill(pid, 0); err != nil {
			t.Fatalf("pid %d was stopped by a reap that does not own it: %v", pid, err)
		}
	}
}

// startDetachedTestWorker starts a REAL gopi worker in its own session — the
// launcher's shape (setsid, and no output flag: the destination belongs to the
// row, epic child-stream-task-output) — against an EMPTY profile (GOPI_HOME
// points at the test's own temp dir, so no real queue can be drained), and
// returns its pid. It is killed on cleanup, so a failure anywhere below cannot
// leak a process of its own.
func startDetachedTestWorker(t *testing.T, bin, home string) int {
	t.Helper()
	cmd := exec.Command(bin, procname.VerbWorker)
	cmd.Env = testWorkerEnv(home)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	// Setsid=true makes THIS child a session leader (pid == session id), the
	// post-setsid signature the reap's detachment test reads — no `setsid`
	// wrapper process in between, so the pid is the worker's own.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start a detached worker: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL) //stylegate:ignore best-effort teardown of the test's own detached worker
		_, _ = cmd.Process.Wait()              //stylegate:ignore reaping the test's own detached worker
	})
	waitForProcIdentity(t, pid)
	return pid
}

// startForegroundTestWorker starts the same worker shape WITHOUT setsid — the
// operator's own foreground invocation, in the SAME profile, so only the
// detachment test can keep it out of the reap.
func startForegroundTestWorker(t *testing.T, bin, home string) int {
	t.Helper()
	cmd := exec.Command(bin, procname.VerbWorker)
	cmd.Env = testWorkerEnv(home)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start a foreground worker: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()    //stylegate:ignore best-effort teardown of the test's own worker
		_, _ = cmd.Process.Wait() //stylegate:ignore reaping the test's own worker on cleanup
	})
	waitForProcIdentity(t, pid)
	return pid
}

// testWorkerEnv is the closed environment a test worker runs in: an EMPTY
// profile (its own temp GopiDir), so the worker finds no task and can never
// touch a real queue, plus the PATH it needs to run at all.
func testWorkerEnv(home string) []string {
	return []string{
		procname.EnvHome + "=" + home,
		config.GopiDirEnv + "=" + home,
		"PATH=" + os.Getenv("PATH"),
		procname.EnvProcRoot + "=" + reapProcRoot,
	}
}

// waitForProcIdentity polls until the kernel reports the child as a worker of
// the expected profile (a process is only observable at its exec'd shape, and
// its environ is written once at exec). It is bounded: an identity that never
// appears is a FAILURE, never a silent skip into an assertion that means
// nothing.
func waitForProcIdentity(t *testing.T, pid int) {
	t.Helper()
	gopiDir := filepath.Join(os.Getenv(procname.EnvHome), config.ProfileDirName)
	deadline := time.Now().Add(reapIdentityWait)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(filepath.Join(reapProcRoot, strconv.Itoa(pid), procCmdlineName)); err == nil {
			fields := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
			if len(fields) > 1 && fields[1] == procname.VerbWorker {
				return
			}
		}
		time.Sleep(probeReapPollInterval) //stylegate:allow-sleep bounded wait for a freshly started worker to appear with its argv; budget reapIdentityWait
	}
	t.Fatalf("pid %d never appeared as a %s process (profile %s)", pid, procname.VerbWorker, gopiDir)
}
