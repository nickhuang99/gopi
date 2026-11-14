package testutil

// Mock-process reaping: the OTHER half of the scratch hygiene rule.
//
// The 2026-09-11 peer1 exhaustion (devdoc/peer1-resource-exhaustion-20260911.md)
// had two mechanisms: throwaway DATA with no bound (part A, the scratch root +
// age/holder GC in scratchroot.go) and throwaway PROCESSES with no owner. The
// second one is this file. A test that spawns a long-lived mock (an X server, a
// `gopi web` server, a merger daemon, a `task run` wrapper) and then fails with
// a t.Fatal — or panics — leaves the child running under the test runner's
// process group, where nothing reaps it: it keeps its scratch files open,
// keeps polling, and survives the whole run.
//
// HARD RULE — the reaper kills ONLY the process group it spawned:
//
//   - the child is started with Setpgid, so it (and everything it forks,
//     including `nohup`-detached grandchildren of the SAME group) is a group
//     of its own whose id equals the child's pid;
//   - cleanup signals exactly kill(-pgid) for that id, never a pid, never a
//     name, never a pattern (`pkill -f` in an agent shell kills the session
//     itself — memory #1749), and never a global process scan;
//   - a handle that cannot prove its own group id (<= 1, or equal to THIS
//     process's group) refuses to signal at all and reports why instead
//     (Rule 15: no silent failure, and no accidental self-kill).
//
// Nothing global is scanned: the reaper is scoped to the handle the test owns.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const (
	// reaperSIGTERMGrace is how long a process group gets to die politely
	// (SIGTERM lets a mock remove its socket file / flush its log) before the
	// reaper escalates to SIGKILL. Small on purpose: it is paid once per
	// cleanup, and a well-behaved mock dies immediately.
	reaperSIGTERMGrace = 2 * time.Second
	// reaperSIGKILLGrace is how long the group gets after SIGKILL. SIGKILL
	// cannot be ignored, so this is only the kernel's scheduling latency; a
	// group still alive after it is reported, never silently dropped.
	reaperSIGKILLGrace = 2 * time.Second
	// reaperPollInterval is the liveness poll step; the loop exits as soon as
	// the group is gone, so this bounds latency, not duration.
	reaperPollInterval = 10 * time.Millisecond
	// reaperMinPGID is the smallest process-group id that may be signalled.
	// kill(-0) signals THIS process's own group (the go test binary and every
	// sibling test), kill(-1) signals every process the user owns; both are
	// catastrophic and both are refused by construction (Rule 1a: the guard
	// compares a named constant, not a bare literal).
	reaperMinPGID = 2
)

const (
	// MockDaemonBinary and MockDaemonHoldSeconds are the portable long-lived
	// stand-in the fixtures spawn when they need "a real process that is alive
	// right now" (a zombie-detection fixture, a CWD holder, a lock holder).
	// They are constants, not inline literals, so every call site names the
	// same fixture and a longer hold is one edit (Rules 1a/1b).
	MockDaemonBinary = "sleep"
	// MockDaemonHoldSeconds outlives any single test: the fixture is always
	// killed by its own reaper, never by running out of time (a sleep that
	// expired mid-test would make a liveness assertion pass vacuously).
	MockDaemonHoldSeconds = "300"
)

// MockProcess is a spawned mock/daemon whose whole process group is reaped by
// the test that spawned it. The zero value is not usable; obtain one from
// StartMockProcess / StartMockProcessCmd.
type MockProcess struct {
	cmd  *exec.Cmd
	pgid int
	// reaped makes Reap idempotent across the callers that can race for it
	// (the explicit Reap by a fixture, the t.Cleanup registration, and a
	// re-entrant cleanup on the panic path).
	reaped atomic.Bool
	// waitOnce starts the single Wait watcher; the reaper must be able to poll
	// the group WHILE reaping, because a zombie leader is still a group member.
	waitOnce sync.Once
	// waitDone is closed once the direct child has been reaped, and waitErr
	// carries that wait's result. Written exactly once, by the watcher
	// goroutine; a second Wait on an *exec.Cmd is both an error and a data
	// race, so every caller in this file shares THIS one.
	waitDone chan struct{}
	waitErr  error
	// logf reports reaper decisions (a refusal, a stubborn group) without
	// failing a test that is already finishing. Never nil after Start.
	logf func(format string, args ...any)
}

// StartMockProcess starts path with args in its OWN process group and
// registers a cleanup that reaps that whole group when the test ends —
// normally, on t.Fatal, or on a PANIC (testing runs t.Cleanup on every exit
// path, which is exactly why the kill lives there and not in a defer).
//
// It is the single sanctioned way for a test to spawn a helper process: the
// scratch root is the single sanctioned place to write DATA, and both exist so
// no test has a reason to invent its own cleanup.
//
// A spawn failure is FATAL by default: on Linux a missing fixture binary is an
// environment fault that must be visible, not a test that quietly proves
// nothing (Rule 15a — no silent skip). A caller that genuinely needs to skip
// (an optional third-party binary) passes AllowStartFailure.
func StartMockProcess(t *testing.T, path string, args ...string) *MockProcess {
	t.Helper()
	return StartMockProcessCmd(t, exec.Command(path, args...))
}

// StartMockProcessCmd is StartMockProcess for a caller that must configure the
// command first (Env, Dir, Stdout/Stderr wiring, a re-exec'd test binary).
// The caller fills cmd in; this function owns the process-group setup, the
// start, and the cleanup registration.
func StartMockProcessCmd(t *testing.T, cmd *exec.Cmd, opts ...MockOption) *MockProcess {
	t.Helper()
	var o MockSpawnOptions
	for _, opt := range opts {
		opt(&o)
	}
	mp := &MockProcess{cmd: cmd, logf: t.Logf}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Setpgid puts the child in a brand-new process group whose id is the
	// child's own pid: the reaper can then address "the child and everything
	// it fork'ed" with one scoped kill, and can never address anything else.
	//
	// Setsid already supersedes it: setsid(2) makes the child a session leader
	// in a new session whose group leader it is (pgid == pid, the same
	// contract), and setpgid(2) on a session leader FAILS with EPERM — asking
	// for both makes cmd.Start() fail outright (measured). So the flag is set
	// only when the caller did not already claim a session, and the
	// own-group guarantee holds either way.
	if !cmd.SysProcAttr.Setsid {
		cmd.SysProcAttr.Setpgid = true
	}
	if cmd.SysProcAttr.Pgid != 0 {
		// An explicit Pgid would make the child JOIN an existing group (the
		// test binary's, when Pgid is this process's group) — the exact shape
		// where cleanup would kill the whole test run. Refuse loudly.
		t.Fatalf("StartMockProcessCmd: SysProcAttr.Pgid=%d preset — the reaper must own the group it kills",
			cmd.SysProcAttr.Pgid)
	}
	// A CLEANUP CANNOT RUN IF THE TEST BINARY IS KILLED. t.Cleanup covers
	// "the test ended" — normally, by t.Fatal, by a panic — but NOT the two
	// paths that dominate a stuck suite: `go test`'s own -timeout panic ends in
	// an os.Exit, and a SIGKILL (from a watchdog, an operator, a CI killer)
	// ends in nothing at all. Measured 2026-10-27 (task 01a179d7): SIGKILLing a
	// `go test` binary mid-suite left its `sleep 300` fixture standing, cwd
	// inside the checkout, and a leaked fixture child with a cwd inside a
	// ground is exactly what makes that ground read IN USE forever.
	//
	// Pdeathsig is the KERNEL's answer and the only one that survives SIGKILL:
	// the child is sent this signal when the THREAD that forked it dies, which
	// for the test binary means the process. SIGKILL is chosen over SIGTERM so
	// a child that traps signals cannot outlive its test.
	if cmd.SysProcAttr.Pdeathsig == 0 {
		cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
	}
	// …and a child must not STAND IN THE REPOSITORY UNDER TEST while it waits
	// to die. A process with a cwd inside a checkout is indistinguishable, to
	// the ground's (correct) live-holder veto, from a run still working there:
	// the veto reads /proc/<pid>/cwd off the directory it is asked about, so a
	// leaked fixture makes `gopi task status <row>` exit 1 forever and blocks
	// both reclaim and close-out. The child's cwd is never its subject — the
	// fixtures that need one pass Dir= explicitly (StartMockProcessDir) — so
	// the default becomes a directory outside every checkout.
	if cmd.Dir == "" && mockChildCwdIsRepoLocal() {
		cmd.Dir = neutralChildDirValue()
	}
	if err := cmd.Start(); err != nil {
		if o.AllowStartFailure {
			if o.OnSkip != nil {
				o.OnSkip()
			}
			t.Skipf("cannot start mock process %s: %v", cmd.Path, err)
		}
		t.Fatalf("start mock process %s: %v", cmd.Path, err)
	}
	// A group leader's pgid IS its pid (guaranteed by Setpgid above).
	mp.pgid = cmd.Process.Pid
	t.Cleanup(mp.Reap)
	return mp
}

// mockChildCwdIsRepoLocal reports whether THIS process's working directory lies
// inside a git work tree, measured ONCE per run (a work tree can stand in for a
// checkout only if the measurement says so; the answer cannot change mid-run
// except by a chdir, which a suite does not do).
var mockChildCwdIsRepoLocal = sync.OnceValue(func() bool {
	out, err := exec.Command("git", "rev-parse", "--is-inside-work-tree").Output()
	if err != nil {
		return false // no git, no work tree: nothing a child could inherit
	}
	return strings.TrimSpace(string(out)) == "true"
})

// neutralChildDirValue creates (once) and returns the directory a fixture child
// is started in when the test process itself stands inside a checkout: an owned
// subdirectory of the shared scratch home, which is off `/` (Rule 22) and
// outside every work tree, so the child can never be mistaken for a holder of
// the ground under test.
var neutralChildDirValue = sync.OnceValue(func() string {
	if dir, err := os.MkdirTemp(ScratchRootQueue(), "fixture-child-cwd-"); err == nil {
		return dir // reaping is the scratch home's age GC, exactly like TestScratchDir
	}
	// The scratch home refused us: the child's cwd is a hygiene property, never
	// a fixture's subject, so fall back to the process's own temp dir rather
	// than failing the suite. It is still outside the checkout (a t.TempDir is
	// under TMPDIR, which PinSuiteTempRoot has pinned outside the tree).
	return os.TempDir()
})

// StartMockProcessDir is StartMockProcess with a working directory — the shape
// the live-CWD holder fixtures need (a process whose CWD is a given tree).
func StartMockProcessDir(t *testing.T, dir, path string, args ...string) *MockProcess {
	t.Helper()
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	return StartMockProcessCmd(t, cmd)
}

// Pid returns the mock's pid (0 before a successful Start).
func (m *MockProcess) Pid() int {
	if m == nil || m.cmd == nil || m.cmd.Process == nil {
		return 0
	}
	return m.cmd.Process.Pid
}

// Pgid returns the process-group id the reaper will signal (0 before Start).
func (m *MockProcess) Pgid() int { return m.pgid }

// Cmd exposes the underlying command so a fixture can wire Env/Stdout before
// the spawn (via StartMockProcessCmd) or read its state after.
func (m *MockProcess) Cmd() *exec.Cmd { return m.cmd }

// Alive reports whether the mock's process group still has a member. It is the
// honest liveness probe for a fixture that asserts "the mock is up": kill the
// member and the group check reports it down.
func (m *MockProcess) Alive() bool {
	if m == nil || m.pgid < reaperMinPGID {
		return false
	}
	return syscall.Kill(-m.pgid, 0) == nil
}

// Reap kills the mock's process group (SIGTERM, then SIGKILL) and reaps the
// direct child. It is:
//
//   - IDEMPOTENT: the second and every later call is a no-op (an explicit Reap
//     by a fixture plus the t.Cleanup registration is the normal pattern);
//   - PANIC-SAFE: it never fails the test and never panics itself, because
//     it also runs while a test is unwinding;
//   - EXITED-SAFE: a group that is already gone is not signalled at all (a
//     fully-reaped pgid can be recycled by the kernel, so signalling it late
//     would risk an unrelated process);
//   - SCOPED: it signals one process group — never a pid lookup by name, never
//     a system-wide scan.
func (m *MockProcess) Reap() {
	if m == nil || m.cmd == nil {
		return
	}
	if !m.reaped.CompareAndSwap(false, true) {
		return // already reaped (or being reaped) — idempotent by construction
	}
	if m.pgid < reaperMinPGID {
		m.report("refusing to signal process group %d (must be >= %d): refusing beats killing the test run", m.pgid, reaperMinPGID)
		m.reapStraight()
		return
	}
	if m.pgid == syscall.Getpgrp() {
		// Belt and braces: Setpgid makes this impossible, but a signal to our
		// OWN group would take the go test binary (and every sibling test)
		// down, so it is refused explicitly rather than trusted.
		m.report("refusing to signal process group %d: it is THIS test's group", m.pgid)
		m.reapStraight()
		return
	}
	if syscall.Kill(-m.pgid, 0) != nil {
		// The whole group is already gone. Do not signal a stale id — a
		// fully-reaped pgid can be recycled, and signalling it late would hit
		// an unrelated process.
		m.reapStraight()
		return
	}
	_ = syscall.Kill(-m.pgid, syscall.SIGTERM) //stylegate:ignore the mock may already be racing to exit, and ESRCH is the expected outcome — the escalation below is the checked step
	if !m.reapAndGroupGone(reaperSIGTERMGrace) {
		// SIGTERM ignored (or trapped by the mock): escalate. SIGKILL cannot
		// be ignored, so this terminates the group unconditionally.
		_ = syscall.Kill(-m.pgid, syscall.SIGKILL) //stylegate:ignore SIGKILL cannot fail on a live group; the survival check below is the checked step
		if !m.reapAndGroupGone(reaperSIGKILLGrace) {
			m.report("mock process group %d survived SIGTERM and SIGKILL — a pid in that group is unkillable", m.pgid)
		}
	}
}

// reapAndGroupGone reaps the direct child and waits for the group to empty.
//
// The direct child MUST be reaped while polling, not after: a zombie is still
// a member of its process group, so kill(-pgid, 0) keeps succeeding until the
// zombie is waited for — a reaper that polls first would burn its whole grace
// window on a process that is already dead. Reaping is done in a goroutine so
// a child that traps SIGTERM cannot block the escalation decision.
func (m *MockProcess) reapAndGroupGone(window time.Duration) bool {
	deadline := time.Now().Add(window)
	for {
		reaped := m.tryReap()
		if reaped && syscall.Kill(-m.pgid, 0) != nil {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(reaperPollInterval)
	}
}

// startWait launches the single Wait watcher (once) and returns a channel
// closed when the direct child has been reaped. Sharing it is mandatory, not
// an optimisation: a second Wait on the same *exec.Cmd returns an error and
// races the first.
func (m *MockProcess) startWait() chan struct{} {
	m.waitOnce.Do(func() {
		m.waitDone = make(chan struct{})
		go func() {
			m.waitErr = m.cmd.Wait() // deliberate: the error is reported via Wait
			close(m.waitDone)
		}()
	})
	return m.waitDone
}

// Wait blocks until the mock's DIRECT child has exited and returns its wait
// error. It does NOT signal anything (unlike Reap), which is what a fixture
// needs when it must observe the child's own exit status — e.g. the
// signal-shutdown test that SIGTERMs a `gopi web` server and asserts the
// process wrote its session_end before exiting. Safe alongside Reap: both
// share one underlying wait.
func (m *MockProcess) Wait() error {
	if m == nil || m.cmd == nil {
		return nil
	}
	<-m.startWait()
	return m.waitErr
}

// tryReap reports whether the direct child has been reaped, starting the
// single Wait watcher if it is not running yet (non-blocking).
func (m *MockProcess) tryReap() bool {
	select {
	case <-m.startWait():
		return true
	default:
		return false
	}
}

// reapStraight starts the direct-child wait on the paths where the group is
// already gone (or must not be signalled). It never blocks on a live process —
// these refusal paths must not hang a suite — so it only ARMS the wait; the
// child is dead already in every case that reaches here.
func (m *MockProcess) reapStraight() {
	m.startWait()
}

// report routes a reaper decision through the test's log when one is attached,
// else to stderr — a reaper outcome is never swallowed (Rule 15b).
func (m *MockProcess) report(format string, args ...any) {
	if m.logf != nil {
		m.logf("testutil reaper: "+format, args...)
		return
	}
	fmt.Fprintf(os.Stderr, "testutil reaper: "+format+"\n", args...)
}

// StartReapedProcess spawns path with args, waits for it to exit and returns a
// pid that is PROVABLY dead (the process was reaped, so signal 0 reports
// ESRCH) — the fixture shape every zombie-detection test needs. Callers used
// to hand-roll "spawn sleep 30, kill, wait" in six places; they share this one
// group-scoped implementation instead.
func StartReapedProcess(t *testing.T, path string, args ...string) int {
	t.Helper()
	mp := StartMockProcess(t, path, args...)
	pid := mp.Pid()
	mp.Reap()
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process %d is still reachable after Reap (kill(pid,0) = %v) — the fixture needs a dead pid", pid, err)
	}
	return pid
}

// MockOption tunes a spawn before it happens.
type MockOption func(*MockSpawnOptions)

// MockSpawnOptions carries the opt-in deviations from the default spawn
// contract (which is: hard-fail a missing fixture).
type MockSpawnOptions struct {
	// AllowStartFailure turns a spawn failure into a VISIBLE SKIP.
	AllowStartFailure bool
	// OnSkip runs just before that skip unwinds — the hook a caller needs when
	// it opened a resource for the child (a log file) that must not be left
	// behind by a skipped test.
	OnSkip func()
}

// AllowStartFailure downgrades a spawn failure to a VISIBLE SKIP (t.Skipf
// names the binary and the error). It exists for the OPTIONAL third-party
// mocks — an X server, xev — whose absence must not fail a suite that has
// other coverage; the test is skipped, never silently passed, and OnSkip (if
// given) runs first. Everything else keeps the default: a missing fixture is
// a hard failure, because silence there would let a suite prove nothing
// (Rule 15a).
func AllowStartFailure(onSkip ...MockOption) MockOption {
	return func(o *MockSpawnOptions) {
		o.AllowStartFailure = true
		for _, opt := range onSkip {
			opt(o)
		}
	}
}

// OnSkip registers a hook to run immediately before a spawn-failure skip.
func OnSkip(fn func()) MockOption {
	return func(o *MockSpawnOptions) { o.OnSkip = fn }
}
