package lineedit

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// rule42_fatal_signal_test.go — the FATAL path's exit status, measured on a REAL
// child process (PROJECT.md Rule 42, plus Rule 31: a surface is executed, not
// simulated).
//
// WHAT WAS TRUE BEFORE THIS FILE. CaptureSignals' fatal path ended with
// `signal.Reset(sigs...)` + self-kill. Reset is `cancel(sig, disableSignal)` over
// EVERY registered channel, so it dismantled subscriptions this package never
// installed. The replacement must keep the thing the Reset was there for: the
// process still dies by the signal's OWN action, so the caller's `go test
// -timeout` / shell / editor sees the status it sent rather than a silent
// survival.
//
// WHY A CHILD PROCESS. The property under test is "the kernel ended this process,
// not `return 0`". An in-process assertion cannot observe that (there is no
// process left to assert from), and an exit CODE cannot distinguish the two: both
// a signal death and an explicit `os.Exit(130)` report 130 to a shell. The only
// honest observation is a real child reaped by its parent and read through
// syscall.WaitStatus (memory: a signal death makes ExitCode() report -1, and -1
// alone does not say which action produced it).
//
// THE SIGNAL, and why not SIGINT: this fixture runs inside a `go test` binary the
// harness also drives, and a child that dies by a signal needs a signal whose
// default action is fatal. SIGTERM is used, and the child is re-executed (not
// forked in-process), so the fixture cannot take the parent suite with it.
const (
	// rule42ChildEnv marks the CHILD half: it opens a terminal on its own pty,
	// arms CaptureSignals for SIGTERM, reports readiness, then parks.
	rule42ChildEnv = "GOPI_LINEEDIT_RULE42_CHILD"
	// rule42ChildReadyEnv names the field the child writes its ready marker
	// into, so the parent signals a child that has actually ARMED the handler.
	rule42ChildReadyEnv = "GOPI_LINEEDIT_RULE42_READY"
	// rule42ReadyWait bounds the parent's wait for that marker.
	rule42ReadyWait = 60 * time.Second
	// rule42ExitWait bounds the wait for the child to die after the signal. A
	// bound, not a delay: with the default disposition restored the child is
	// gone in microseconds, and without it the child never dies at all.
	rule42ExitWait = 30 * time.Second
)

// rule42ChildMain is the child half. It returns true when it ran as the child (so
// the parent's TestMain-free tests are unaffected — this package has no TestMain,
// so the arm is entered from the test below).
func rule42ChildMain() bool {
	readyPath := os.Getenv(rule42ChildReadyEnv)
	if os.Getenv(rule42ChildEnv) == "" || readyPath == "" {
		return false
	}
	master, slave, ok := rule42OpenPTY()
	if !ok {
		fmt.Fprint(os.Stderr, "child: no pty available\n")
		os.Exit(3)
	}
	defer func() { _ = master.Close() }() //stylegate:ignore the child's own fixture teardown
	term, err := Open(slave, slave)
	if err != nil {
		fmt.Fprintf(os.Stderr, "child: Open: %v\n", err)
		os.Exit(3)
	}
	// THE PRODUCTION ARM: the same call the chat wire makes.
	unsubscribe := term.CaptureSignals(syscall.SIGTERM)
	defer unsubscribe()
	if err := os.WriteFile(readyPath, []byte("armed\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "child: ready marker: %v\n", err)
		os.Exit(3)
	}
	// PARK. Nothing may end this child except the signal, or the assertion
	// below would be about the park expiring rather than about the disposition.
	select {}
}

// rule42OpenPTY is the fixture's pty, factored so the child half can use it
// without a *testing.T.
func rule42OpenPTY() (master, slave *os.File, ok bool) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, false
	}
	fd := int(m.Fd())
	n, err := PtyNumber(fd)
	if err != nil {
		_ = m.Close() //stylegate:ignore the fixture's own descriptor
		return nil, nil, false
	}
	if err := PtyUnlock(fd); err != nil {
		_ = m.Close() //stylegate:ignore the fixture's own descriptor
		return nil, nil, false
	}
	s, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR, 0)
	if err != nil {
		_ = m.Close() //stylegate:ignore the fixture's own descriptor
		return nil, nil, false
	}
	return m, s, true
}

// TestFatalSignalPathStillDiesByTheSignal drives the fatal path on a REAL child
// and asserts the KERNEL ended it (WaitStatus.Signaled + the signal it sent).
//
// This is the guard for the removal of `signal.Reset`: if a future change left a
// subscription of this package's registered (or restored a blanket reset that
// cancelled the wrong channel), the child would survive, the wait would time out,
// and this test fails by name instead of the defect reaching an operator as an
// editor that will not close.
func TestFatalSignalPathStillDiesByTheSignal(t *testing.T) {
	// THE CHILD HALF, entered FIRST: the child is started with -test.run naming
	// this very test, so the arm has to be taken before any assertion. It either
	// returns immediately (this is the parent) or never returns (the child parks
	// until the signal kills it).
	if rule42ChildMain() {
		return
	}
	if _, _, ok := rule42OpenPTY(); !ok {
		t.Skipf("/dev/ptmx is unavailable on this host — the terminal CaptureSignals arms cannot be " +
			"exercised, and this test would then prove nothing about the fatal path")
	}
	ready := filepath.Join(t.TempDir(), "armed")
	cmd := exec.Command(os.Args[0], "-test.run", "^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), rule42ChildEnv+"=1", rule42ChildReadyEnv+"="+ready)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	// Its own process group: the signal below is aimed at exactly this child,
	// and the teardown kill cannot reach the parent's group (Rule 42's ban is on
	// signalling what the caller does not own; this child IS the caller's).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the child: %v", err)
	}
	childPid := cmd.Process.Pid
	// Rule 15b: the child is PARKED, so a failure before the signal must not
	// leave it running.
	defer func() {
		_ = syscall.Kill(-childPid, syscall.SIGKILL) //stylegate:ignore teardown of the fixture's own child; ESRCH means it already died
		_, _ = cmd.Process.Wait()
	}()

	deadline := time.Now().Add(rule42ReadyWait)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child did not arm CaptureSignals within %s — it never reached the fatal path, "+
				"so this test would prove nothing. Child output:\n%s", rule42ReadyWait, out.String())
		}
		runtime.Gosched()
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signalling the child: %v", err)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case <-waitErr:
	case <-time.After(rule42ExitWait):
		t.Fatalf("the child %d SURVIVED SIGTERM for %s — the fatal path no longer reaches the "+
			"signal's default action, i.e. the process is unkillable by the signal it was sent "+
			"(the failure mode a blanket signal.Reset was banned for). Child output:\n%s",
			childPid, rule42ExitWait, out.String())
	}
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("the child produced no WaitStatus — a signal death cannot be distinguished from " +
			"an exit code without it")
	}
	if !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("the child ended as %v (signaled=%v signal=%v), want a death BY SIGTERM — the fatal "+
			"path asked for a signal death and must not leave an exit code in its place. Child "+
			"output:\n%s", cmd.ProcessState, ws.Signaled(), ws.Signal(), out.String())
	}
	t.Logf("child %d died by %v (signaled=%v) — the kernel's own action, after the terminal was restored",
		childPid, ws.Signal(), ws.Signaled())
}
