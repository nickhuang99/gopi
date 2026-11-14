package testutil

// The child-side probe for daemon lifetime binding.
//
// The mechanism (internal/daemonlifetime) can only be proven in a REAL child:
// what is being tested is "the kernel closes the write end when the holder dies
// and the watchdog therefore kills the child", and that is a fact about two
// processes and a pipe. So the probe re-execs THIS test binary (the established
// pattern in this package: same binary, guard variable, no second artifact), and
// the child installs the PRODUCTION watchdog — daemonlifetime.WatchFromEnv — on
// the fd it was handed, then blocks forever.
//
// A test process that runs the suite still reaches m.Run; a probe process exits
// before it, because starting a whole suite inside a child that is deliberately
// killed would be a spectacular own goal.

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/daemonlifetime"
)

const (
	// LifetimeProbeGuardEnv marks a re-exec'd probe child. Its presence is the
	// ONLY thing that stops a probe from running the suite (see
	// RunDaemonLifetimeProbeChild).
	LifetimeProbeGuardEnv = "GOPI_TEST_DAEMON_LIFETIME_PROBE"
	// lifetimeProbeReadyEnv carries the SPAWN-SIDE fd number of the pipe the
	// probe child writes its "watchdog armed" byte on (the same parent-writes-
	// the-child-fd-number convention as the daemon handshake).
	lifetimeProbeReadyEnv = "GOPI_TEST_DAEMON_LIFETIME_PROBE_READY_FD"
	// lifetimeProbeReadyByte is the single byte that proves the child is alive
	// AND armed before the test acts. One byte, like the daemon handshake: the
	// caller's read returns the instant it lands.
	lifetimeProbeReadyByte = "P"
	// lifetimeProbeReadyWait bounds the wait for that byte.
	lifetimeProbeReadyWait = 30 * time.Second
	// lifetimeProbeDeathWait bounds how long a released probe may take to die.
	// The watchdog reacts to a kernel-level HUP, so this is generous.
	lifetimeProbeDeathWait = 30 * time.Second
)

// RunDaemonLifetimeProbeChild is a suite's TestMain guard: it must be called
// FIRST, and it returns true when THIS process is a re-exec'd probe child, in
// which case TestMain must return without running any test.
func RunDaemonLifetimeProbeChild() bool {
	if os.Getenv(LifetimeProbeGuardEnv) == "" {
		return false
	}
	runLifetimeProbe()
	return true
}

// runLifetimeProbe is the child body: arm the PRODUCTION watchdog, report
// readiness, then block until the watchdog kills this process.
func runLifetimeProbe() {
	// The probe plays the DAEMON's role here, and the daemon's fd statement is
	// made by whatever started it. Two shapes reach this point: an explicit
	// EnvName from a launcher, or the inherited HOLD fd of the suite-level pipe —
	// adoption handles the second without the probe choosing any number.
	if _, err := daemonlifetime.AdoptInheritedHoldFD(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon lifetime probe: %v\n", err)
		reportProbeState(false)
		os.Exit(probeArmFailedExit)
	}
	armed := daemonlifetime.WatchFromEnv()
	reportProbeState(armed)
	if !armed {
		// A probe whose watchdog could not arm must NOT look like a passing
		// test: it reports NOT-ARMED (which the waiter refuses as readiness) and
		// exits non-zero (Rule 15 — a broken binding is never a silent success).
		os.Exit(probeArmFailedExit)
	}
	// The watchdog is the only thing that can end this process. The wait is a
	// timer loop, NOT `select {}`: with the watchdog goroutine the only other
	// one, a bare select lets the runtime declare "all goroutines are asleep -
	// deadlock!" and exit — which would look exactly like the watchdog firing.
	for {
		time.Sleep(probeForeverSleep)
	}
}

// probeForeverSleep is the probe's wait interval.
const probeForeverSleep = time.Hour

// probeArmFailedExit is the exit code of a probe whose watchdog did not arm.
const probeArmFailedExit = 98

// reportProbeState writes the readiness byte on the inherited report pipe, if
// one was handed over. A missing report pipe is not an error: an operator
// running the probe by hand still gets the stderr line.
func reportProbeState(armed bool) {
	fmt.Fprintf(os.Stderr, "daemon lifetime probe: pid %d armed=%v\n", os.Getpid(), armed)
	if !armed {
		// An UNARMED probe must not be mistaken for a ready one: it says so in a
		// word the waiter does not accept as readiness, and it exits non-zero.
		writeReadiness("NOT-ARMED")
	}
	writeReadiness(lifetimeProbeReadyByte)
}

// writeReadiness reports to the caller's readiness pipe, whose fd number the
// caller states in the environment. A caller that supplies no pipe (the holder
// fixture starts its child without one) gets the stderr line only — the probe
// must not guess an fd and write into whatever happens to be there.
// OWNERSHIP: the descriptor is BORROWED — the caller states its own fd number
// in lifetimeProbeReadyEnv and this process never opened it. The wrapper is
// CLOSED here on every exit (`defer f.Close()`), and that is correct: os.File.Close
// clears the finalizer (os.(*file).close → runtime.SetFinalizer(file, nil),
// GOROOT/src/os/file_unix.go), so the descriptor is released exactly once, by
// this call, after the readiness word has been written. Only a DROPPED borrowed
// wrapper needs borrowedfd.Detach.
func writeReadiness(word string) {
	raw := os.Getenv(lifetimeProbeReadyEnv)
	if raw == "" {
		return
	}
	fd, err := strconv.Atoi(raw)
	if err != nil || fd < 0 {
		fmt.Fprintf(os.Stderr, "daemon lifetime probe: %s=%q is not an fd number\n", lifetimeProbeReadyEnv, raw)
		return
	}
	f := os.NewFile(uintptr(fd), lifetimeProbeReadyEnv)
	if f == nil {
		return
	}
	defer f.Close()
	if _, err := f.WriteString(word); err != nil {
		fmt.Fprintf(os.Stderr, "daemon lifetime probe: readiness write failed: %v\n", err)
	}
}

// LifetimeProbe is one running probe child.
type LifetimeProbe struct {
	Cmd *exec.Cmd
	Pid int
}

var (
	probeExeOnce sync.Once
	probeExePath string
	probeExeErr  error
)

// probeSelfPath locates this test binary (once) — the artifact every probe is a
// re-exec of.
func probeSelfPath() (string, error) {
	probeExeOnce.Do(func() {
		probeExePath, probeExeErr = os.Executable()
	})
	return probeExePath, probeExeErr
}

// StartLifetimeProbe spawns a probe child whose lifetime is bound to the
// SUITE-level death pipe: both the death-pipe fd and the readiness fd are
// numbers this process already holds (the first is inherited because its
// FD_CLOEXEC is cleared; the second is passed through ExtraFiles, which numbers
// it from 3).
//
// The write end of the death pipe is NOT passed to anybody, and it cannot be:
// os.Pipe sets O_CLOEXEC on it, so no descendant of this process ever holds it.
// That is what makes "the holder died" a fact only this process's death can
// create.
func StartLifetimeProbe(t *testing.T, holder int) *LifetimeProbe {
	t.Helper()
	self, err := probeSelfPath()
	if err != nil {
		t.Fatalf("locate this test binary: %v", err)
	}
	readyR, readyW, err := os.Pipe()
	if err != nil {
		t.Fatalf("probe readiness pipe: %v", err)
	}
	cmd := exec.Command(self, "-test.run", probeNoTestPattern)
	cmd.Env = append(os.Environ(),
		LifetimeProbeGuardEnv+"=1",
		lifetimeProbeReadyEnv+"="+strconv.Itoa(ExtraFilesOffset(0)),
	)
	cmd.ExtraFiles = []*os.File{readyW}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the lifetime probe: %v", err)
	}
	_ = readyW.Close() //stylegate:ignore the child holds its own dup; the parent's copy would keep the readiness read open forever
	if !awaitProbeReady(t, readyR, cmd) {
		t.Fatalf("probe pid %d never reported readiness", cmd.Process.Pid)
	}
	_ = readyR.Close() //stylegate:ignore readiness arrived, the pipe is done with
	t.Logf("lifetime probe started: pid %d, bound to the suite death pipe (holder fd %d)", cmd.Process.Pid, holder)
	return &LifetimeProbe{Cmd: cmd, Pid: cmd.Process.Pid}
}

// probeNoTestPattern is the -test.run pattern a probe child is started with: it
// matches no test, so even a probe that somehow reached m.Run would execute
// nothing harmful.
const probeNoTestPattern = "^$"

// awaitProbeReady waits for the readiness byte, and reports the child's early
// death as a failure naming what it printed.
func awaitProbeReady(t *testing.T, r *os.File, cmd *exec.Cmd) bool {
	t.Helper()
	deadline := time.Now().Add(lifetimeProbeReadyWait)
	buf := make([]byte, 1)
	for time.Now().Before(deadline) {
		n, err := r.Read(buf)
		if n == 1 && string(buf[:1]) == lifetimeProbeReadyByte {
			return true
		}
		if err != nil {
			return false
		}
	}
	return false
}

// WaitForProbeDeath waits for the probe to exit and reports (exitCode, died,
// reason). `died` is false when the budget expired, in which case the probe has
// been killed and reaped so no test can leave it behind.
func WaitForProbeDeath(t *testing.T, p *LifetimeProbe) (int, bool, string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.Cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return 0, true, "exited 0 on its own (NOT the watchdog)"
		}
		code := p.Cmd.ProcessState.ExitCode()
		ws, ok := p.Cmd.ProcessState.Sys().(interface{ Signaled() bool })
		if ok && ws.Signaled() {
			return code, true, "died of a signal (the watchdog's SIGKILL)"
		}
		return code, true, "exited non-zero"
	case <-time.After(lifetimeProbeDeathWait):
		_ = p.Cmd.Process.Kill() //stylegate:ignore best-effort kill of a probe that outlived its budget; the caller fails on the timeout
		_, _ = p.Cmd.Process.Wait()
		return 0, false, "still alive after the budget"
	}
}

// ProbeAlive reports whether the probe is still running (used by the negative
// half of the proof: an armed holder must keep its probe alive).
func ProbeAlive(pid int) bool {
	return SignalZeroAlive(pid)
}
