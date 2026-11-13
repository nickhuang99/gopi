package testutil

// Tests for the daemon lifetime binding: proof that a daemon child dies when the
// process holding its death pipe dies — by the KERNEL's report, with no cleanup
// hook running in between.
//
// Every case below uses a REAL child process and a REAL pipe; nothing is
// simulated. The one case that matters most (the leaked-daemon incident's shape)
// is TestDaemonLifetimeBindsWinnerToKilledHolder: a CHILD holds the pipe and
// starts a daemon; the child is SIGKILLed (SIGKILL — no handler, no defer, no
// t.Cleanup can run); the daemon must be gone. That test's own parent performs
// the kill, so it is the closest thing to the original incident that can run
// inside a test suite.

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/daemonlifetime"
)

const (
	// dlPollInterval is the liveness poll cadence for these cases (Rule 1a).
	dlPollInterval = 20 * time.Millisecond
	// dlWaitBudget bounds every "it must be gone / it must still be there"
	// wait. The mechanism is a kernel HUP, so anything slow here is a real
	// failure, not scheduling.
	dlWaitBudget = 30 * time.Second
	// dlAliveGrace is how long the NEGATIVE half waits before it concludes the
	// holder is still holding (an armed holder must NOT kill its daemon).
	dlAliveGrace = time.Second
)

// TestDaemonLifetimeProbeDiesWhenHolderReleasesThePipe proves the base case: the
// watchdog (production code, in a real child) kills the child the instant the
// pipe's write end is closed.
func TestDaemonLifetimeProbeDiesWhenItsHolderIsSIGKILLed(t *testing.T) {
	holder := startLifetimeHolder(t)
	daemonPid := holder.awaitDaemonPid(t)
	// A LIVE, NON-ZOMBIE process — signal 0 alone would accept the corpse of a
	// fixture child that exited early, and the case would then pass for the
	// wrong reason (measured: that is exactly how a child exiting unarmed made
	// this proof look green).
	id := awaitProcIdentityOrFail(t, daemonPid)
	if alive, reason := id.Alive(); !alive {
		t.Fatalf("the holder's bound child (pid %d) is not alive before the kill: %s", daemonPid, reason)
	}
	// SIGKILL the holder: no handler, no defer, no t.Cleanup — the only thing
	// that runs is the kernel closing its descriptors.
	if err := syscall.Kill(holder.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL the holder pid %d: %v", holder.Pid, err)
	}
	_, _ = holder.Cmd.Process.Wait() //stylegate:ignore reap of the holder process this test started; the lifetime verdict is the assertion, not the exit status
	t.Logf("holder pid %d SIGKILLed; its bound child pid %d must follow with no cleanup code running", holder.Pid, daemonPid)
	assertIdentityGone(t, daemonPid)
}

// TestDaemonLifetimeProbeSurvivesWhileTheHolderLives is the NEGATIVE half, and
// the one that keeps the mechanism honest: an armed holder that is still alive
// must NOT take its daemon down. Without this, "the probe died" could be
// satisfied by a watchdog that fires immediately.
func TestDaemonLifetimeBoundChildSurvivesWhileItsHolderLives(t *testing.T) {
	holder := startLifetimeHolder(t)
	daemonPid := holder.awaitDaemonPid(t)
	id := awaitProcIdentityOrFail(t, daemonPid)
	// Observe past the child's startup: the binding must not fire on its own.
	deadline := time.Now().Add(dlAliveGrace)
	for time.Now().Before(deadline) {
		if alive, reason := id.Alive(); !alive {
			t.Fatalf("the bound child %d died while its holder was alive: %s — the watchdog fires too early", daemonPid, reason)
		}
		time.Sleep(dlPollInterval) //stylegate:allow-sleep bounded observation window for the negative half
	}
	t.Logf("bound child pid %d stayed alive for %s while its holder held the write end", daemonPid, dlAliveGrace)
	// Cleanup through the binding: killing the holder must take the child down.
	if err := syscall.Kill(holder.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL the holder: %v", err)
	}
	_, _ = holder.Cmd.Process.Wait() //stylegate:ignore reap of the holder process this test started; the lifetime verdict is the assertion, not the exit status
	assertIdentityGone(t, daemonPid)
}

// TestDaemonLifetimeWatchIsInertWithoutTheEnv proves the arming is opt-in: a
// process with no death-pipe variable runs the production entry point and is
// NOT bound to anything — the property that keeps an operator's own daemon
// unbound.
func TestDaemonLifetimeWatchIsInertWithoutTheEnv(t *testing.T) {
	// The variable must be ABSENT for the assertion, and SnapshotEnv is the
	// package's own reviewed restore (it records presence, not just value, so an
	// unset variable comes back UNSET).
	defer SnapshotEnv(t, daemonlifetime.EnvName)()
	// The case needs the variable ABSENT, which t.Setenv cannot express (it can
	// only set a value, and the empty string is not absence — the production
	// reader distinguishes them). The reviewed pair is os.Unsetenv plus the
	// SnapshotEnv restore above, which is presence-aware.
	// env-leak-ok: SnapshotEnv(t, …) above restores presence, not just value.
	if err := os.Unsetenv(daemonlifetime.EnvName); err != nil {
		t.Fatalf("unset %s: %v", daemonlifetime.EnvName, err)
	}
	if daemonlifetime.WatchFromEnv() {
		t.Errorf("WatchFromEnv armed with no %s in the environment — an unarmed process must stay unbound", daemonlifetime.EnvName)
	}
	// The fd-shape guards. Both cases are REFUSALS, and both matter: the
	// watchdog reads the descriptor and then SIGKILLs this process, so arming it
	// on the wrong descriptor is not a subtle bug — it kills a healthy process.
	//
	// (a) a value below fd 3 can never be the binding (ExtraFiles start at 3).
	// This one was MEASURED: under `go test` fd 1 IS a pipe, so a shape test
	// alone armed the watchdog on stdout and the watchdog killed the test binary
	// the moment stdout changed hands.
	t.Setenv(daemonlifetime.EnvName, "1")
	if daemonlifetime.WatchFromEnv() {
		t.Errorf("WatchFromEnv armed on fd 1 (a standard stream) — the watchdog would kill a healthy process")
	}
	// (b) a value naming a descriptor that is not a pipe at all.
	f, err := os.CreateTemp(t.TempDir(), "notapipe")
	if err != nil {
		t.Fatalf("create a non-pipe descriptor: %v", err)
	}
	defer f.Close()
	t.Setenv(daemonlifetime.EnvName, strconv.Itoa(int(f.Fd())))
	if daemonlifetime.WatchFromEnv() {
		t.Errorf("WatchFromEnv armed on a regular file descriptor (%d) — the watchdog would read an unrelated descriptor", f.Fd())
	}
}

// TestDaemonLifetimeBindsWinnerToKilledHolder is the INCIDENT SHAPE, end to end:
//
//	a holder child process arms the binding and starts a daemon;
//	the holder is SIGKILLed by its parent (no cleanup hook can run — this is
//	exactly what `go test -timeout` / kill -9 does to a test binary);
//	the daemon must be dead.
//
// The holder is a re-exec of this test binary, so the code under test is the
// production watchdog, and the kill is a real SIGKILL.
func TestDaemonLifetimeBindsChildToKilledHolder(t *testing.T) {
	holder := startLifetimeHolder(t)
	// The holder starts the bound child and reports its pid — proof the child
	// exists BEFORE the kill, so "it is gone" cannot pass vacuously.
	daemonPid := holder.awaitDaemonPid(t)
	id := awaitProcIdentityOrFail(t, daemonPid)
	if alive, reason := id.Alive(); !alive {
		t.Fatalf("the holder's bound child (pid %d) is not alive before the kill — the case would prove nothing: %s", daemonPid, reason)
	}
	// SIGKILL the holder: no handler, no defer, no t.Cleanup, no cooperation.
	if err := syscall.Kill(holder.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL the holder pid %d: %v", holder.Pid, err)
	}
	_, _ = holder.Cmd.Process.Wait() //stylegate:ignore reap of the holder process this test started so it cannot linger as a zombie; the lifetime verdict is the assertion, not the exit status
	t.Logf("holder pid %d SIGKILLed; waiting for its bound child pid %d to follow", holder.Pid, daemonPid)
	// The child must die with NOBODY acting on it: the kernel closed the
	// holder's write end, and the child's own watchdog did the rest.
	if !awaitIdentityGone(t, id, dlWaitBudget) {
		_ = syscall.Kill(daemonPid, syscall.SIGKILL) //stylegate:ignore best-effort cleanup of the leaked child so this failing test does not leave one behind
		t.Fatalf("child pid %d SURVIVED its holder's SIGKILL — this is the leak the epic is about", daemonPid)
	}
	t.Logf("child pid %d died with its holder pid %d — the binding held under SIGKILL", daemonPid, holder.Pid)
}

// lifetimeHolder is a child process that owns a daemon's death pipe.
type lifetimeHolder struct {
	Cmd *exec.Cmd
	Pid int
	// report is the pipe the holder writes its daemon's pid on (test side).
	report *os.File
}

const (
	// lifetimeHolderGuardEnv marks the re-exec'd holder child.
	lifetimeHolderGuardEnv = "GOPI_TEST_LIFETIME_HOLDER"
	// lifetimeHolderDaemonReadyEnv carries the SPAWN-SIDE fd the holder reports
	// its daemon's pid on.
	lifetimeHolderDaemonReadyEnv = "GOPI_TEST_LIFETIME_HOLDER_DAEMON_FD"
	// lifetimeHolderDaemonFD is that fd number inside the holder.
	lifetimeHolderDaemonFD = 3
)

// runLifetimeHolderChild is the holder child's entry point (called from this
// package's TestMain). It returns true when this process is a holder.
func runLifetimeHolderChild() bool {
	if os.Getenv(lifetimeHolderGuardEnv) == "" {
		return false
	}
	runLifetimeHolder()
	return true
}

// runLifetimeHolder: arm the binding, start a PROBE daemon bound to it, report
// the daemon's pid, then block. The holder does NOT clean up — that is the point
// (a killed holder cannot clean up; the kernel's HUP is what must do it).
func runLifetimeHolder() {
	// The holder IS the process whose death must take the child down, so it must
	// hold the pipe itself: it arms its OWN binding (a pipe whose write end lives
	// in this process and whose read end is exported for the child to inherit).
	// Inheriting the SUITE's pipe instead would bind the child to the test binary
	// — killing the holder would then prove nothing, because the write end would
	// still be open in the suite (measured: two cases reported "still reachable
	// after its holder released the death pipe" for exactly that reason).
	if err := daemonlifetime.ArmThisProcess(); err != nil {
		failHolder("arm this holder's binding: " + err.Error())
	}
	exe, err := holderProbePath()
	if err != nil {
		failHolder("locate the probe binary: " + err.Error())
	}
	// The probe DAEMON runs the SAME production watchdog entry point a real
	// `gopi worker --daemon` child runs (daemonlifetime.WatchFromEnv), bound to
	// the pipe this holder owns.
	cmd := exec.Command(exe, daemonProbeArgs()...)
	// The child inherits the SUITE death pipe's fd from this holder's own
	// environment (its FD_CLOEXEC is cleared), so the binding is passed on
	// without any pipe being created here and without any fd number being
	// recomputed — the number travels with the variable.
	cmd.Env = append(os.Environ(), probeDaemonEnv()...)
	// The child's diagnostics must reach the test log: a fixture child that
	// refuses to arm and exits is invisible otherwise, and "the child is gone"
	// then looks exactly like the binding working.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		failHolder("start the bound child: " + err.Error())
	}
	reportDaemonPid(cmd.Process.Pid)
	// A killed holder runs no cleanup; the daemon must die from the HUP. The
	// wait is a timer loop, not `select {}` (see cmd/agent's WaitForever): a bare
	// select would make the runtime exit this holder by deadlock detection, which
	// looks identical to the binding working.
	for {
		time.Sleep(holderForeverSleep) //stylegate:allow-sleep this is a process wait, not a poll: the fixture must stay alive until it is SIGKILLed
	}
}

// holderForeverSleep is the holder's wait interval.
const holderForeverSleep = time.Hour

// reportDaemonPid writes the daemon's pid on the holder's report pipe.
func reportDaemonPid(pid int) {
	f := os.NewFile(uintptr(lifetimeHolderDaemonFD), lifetimeHolderDaemonReadyEnv)
	if f == nil {
		failHolder("report fd is not usable")
	}
	defer f.Close()
	if _, err := f.WriteString(itoa(pid) + "\n"); err != nil {
		failHolder("report write: " + err.Error())
	}
}

// failHolder reports and exits non-zero: a holder that cannot do its job must
// not look like a passing case.
func failHolder(msg string) {
	_, _ = os.Stderr.WriteString("lifetime holder: " + msg + "\n") //stylegate:ignore holder reporting on stderr before os.Exit; a write failure cannot be reported by a process already exiting
	os.Exit(99)
}

// startLifetimeHolder starts the holder child.
func startLifetimeHolder(t *testing.T) *lifetimeHolder {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate this test binary: %v", err)
	}
	reportR, reportW, err := os.Pipe()
	if err != nil {
		t.Fatalf("holder report pipe: %v", err)
	}
	cmd := exec.Command(self, "-test.run", probeNoTestPattern)
	cmd.Env = append(os.Environ(), lifetimeHolderGuardEnv+"=1")
	cmd.ExtraFiles = []*os.File{reportW}
	// The holder's (and, through it, its child's) diagnostics go to this test's
	// log.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the lifetime holder: %v", err)
	}
	_ = reportW.Close() //stylegate:ignore the holder owns the write end; the parent's copy would keep the report read open forever
	h := &lifetimeHolder{Cmd: cmd, Pid: cmd.Process.Pid, report: reportR}
	t.Cleanup(func() {
		// A holder that is still alive at cleanup (a failing case) must not be
		// left behind: kill and reap it, and let its daemon's binding do the
		// rest.
		if SignalZeroAlive(h.Pid) {
			_ = syscall.Kill(h.Pid, syscall.SIGKILL) //stylegate:ignore best-effort cleanup of a failed-case holder
			_, _ = h.Cmd.Process.Wait()
		}
		_ = h.report.Close() //stylegate:ignore test teardown close; the read end carries no data after the case
	})
	return h
}

// awaitDaemonPid reads the daemon's pid from the holder's report pipe.
func (h *lifetimeHolder) awaitDaemonPid(t *testing.T) int {
	t.Helper()
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		buf := make([]byte, 32)
		n, err := h.report.Read(buf)
		ch <- res{line: string(buf[:n]), err: err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read the holder's daemon pid: %v", r.err)
		}
		pid := parseIntField(t, r.line)
		if pid <= 0 {
			t.Fatalf("holder reported a non-pid %q", r.line)
		}
		return pid
	case <-time.After(dlWaitBudget):
		t.Fatalf("the holder never reported its daemon's pid within %s", dlWaitBudget)
		return 0
	}
}

// awaitProcIdentityOrFail reads a live process's triple (the caller has already
// seen it alive, so this is the identity capture, not a liveness probe). It
// waits for the argv to be published — a child read the instant it was spawned
// has an empty /proc/<pid>/cmdline (measured; ArgvUnpublished), and storing that
// as the identity makes the caller's own Alive() re-check report REUSE with a
// matching starttime.
func awaitProcIdentityOrFail(t *testing.T, pid int) ProcIdentity {
	t.Helper()
	id, err := AwaitProcIdentity(pid)
	if err != nil {
		t.Fatalf("read the identity of pid %d: %v", pid, err)
	}
	return id
}

// idOf reads an identity, or reports that the process is already gone (→ an
// identity that can never match, which makes the assertion pass).
func idOf(t *testing.T, pid int) ProcIdentity {
	t.Helper()
	id, err := ReadProcIdentity(pid)
	if err != nil {
		return ProcIdentity{Pid: pid, StartTime: ^uint64(0)} // an impossible incarnation
	}
	return id
}

// assertIdentityGone fails when a pid is still reachable after its binding was
// released — the leak assertion every case here ends on.
func assertIdentityGone(t *testing.T, pid int) {
	t.Helper()
	if !awaitIdentityGone(t, idOf(t, pid), dlWaitBudget) {
		t.Errorf("pid %d is still reachable after its holder released the death pipe", pid)
	}
}

// awaitIdentityGone polls until the process is gone (or stops being the same
// incarnation), reporting false when the budget expires. It takes an IDENTITY
// when the caller has one (a recycled pid must not read as a survivor) and falls
// back to reading one when the process may already be gone.
func awaitIdentityGone(t *testing.T, id ProcIdentity, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if alive, _ := id.Alive(); !alive {
			return true
		}
		time.Sleep(dlPollInterval) //stylegate:allow-sleep bounded wait for a bound process to die
	}
	return false
}

// daemonProbeArgs builds the argv of the probe DAEMON a holder starts: this test
// binary, run in the probe-child mode, with the death pipe armed — i.e. the same
// production entry point a real gopi daemon runs.
func daemonProbeArgs() []string { return []string{"-test.run", probeNoTestPattern} }

// probeDaemonEnv is the child-side environment for the holder's probe: the guard
// that routes it into the watchdog, and NOTHING about fds — the death pipe's
// number arrives through the inherited contract variable (HoldEnvName) and the
// production seam converts it (see cmd/agent's daemonDeathPipe).
func probeDaemonEnv() []string {
	return []string{LifetimeProbeGuardEnv + "=1"}
}

// holderProbePath is the executable the holder re-execs (its own binary).
func holderProbePath() (string, error) { return os.Executable() }

// runLifetimeHolderAndProbeChild is the umbrella guard a suite's TestMain calls
// FIRST: it dispatches both fixture children (the probe and the holder) and
// returns true when this process is one of them.
func runLifetimeHolderAndProbeChild() bool {
	if RunDaemonLifetimeProbeChild() {
		return true
	}
	return runLifetimeHolderChild()
}

// itoa is a tiny helper so this file needs no strconv import for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// parseIntField reads the first integer in s (the holder's pid line).
func parseIntField(t *testing.T, s string) int {
	t.Helper()
	n := 0
	seen := false
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			seen = true
			n = n*10 + int(s[i]-'0')
			continue
		}
		if seen {
			break
		}
	}
	return n
}
