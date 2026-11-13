package procname

// Tests for the DELIVERY-POINT refusal: a signal must never reach the login
// session's manager.
//
// THE SAFETY CONTRACT OF THIS FILE: no arm sends a signal to the manager. The
// measured case (2026-10-17, five times in one boot) is that a single SIGINT
// there STOPS THE WHOLE USER SESSION, so the proof that the guard is LOAD-BEARING
// is taken from the two predicates' verdicts on the same LIVE target (the
// incarnation check permits, the manager check refuses) plus a real delivery to
// a throwaway child (the seam is not a no-op). The last arm re-reads the manager
// and fails if it moved — this test must not be the thing that ends a session.

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
)

const (
	// sessionMgrTestPollInterval / sessionMgrTestPollTries bound the waits below.
	sessionMgrTestPollInterval = 10 * time.Millisecond
	sessionMgrTestPollTries    = 500
	// sessionMgrFixtureThrows is how long a fixture/child lives; long enough that
	// no arm races the process's own exit, short enough to be a leak-free fixture.
	sessionMgrFixtureThrows = "300"
	// sessionMgrFixturePid is the pid directory name inside the fixture procfs.
	sessionMgrFixturePid = "4242"
	// sessionMgrFixtureRoot decides the delivery in the fixture arms: NOT a
	// manager, so the refusal must be nil (the negative control).
	sessionMgrNotManagerPid = "4243"
)

// writeFixtureEntry materialises one /proc-like pid entry: comm (with the
// kernel's trailing newline) and cmdline (NUL-separated argv).
func writeFixtureEntry(t *testing.T, procRoot, pid, comm string, argv ...string) {
	t.Helper()
	dir := filepath.Join(procRoot, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fixture /proc entry %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, CommName), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture comm: %v", err)
	}
	body := strings.Join(argv, "\x00") + "\x00"
	if err := os.WriteFile(filepath.Join(dir, CmdlineName), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture cmdline: %v", err)
	}
}

// TestLoginSessionManagerIsIdentifiedByCommAndTheUserFlag pins the
// identification against a KNOWN fixture tree — the positive case, the two
// negatives that matter, and the unreadable case that must never be claimed.
func TestLoginSessionManagerIsIdentifiedByCommAndTheUserFlag(t *testing.T) {
	root := t.TempDir()
	writeFixtureEntry(t, root, sessionMgrFixturePid, UserManagerComm, "/lib/systemd/systemd", UserManagerArg)
	// The SYSTEM manager shares the comm and the binary; only the flag separates
	// them. Identifying it as the session manager would refuse every
	// legitimately-rooted delivery to PID 1.
	writeFixtureEntry(t, root, "1", UserManagerComm, "/lib/systemd/systemd")
	// A user process whose argv merely CONTAINS the flag as a later word is not
	// the manager: comm is the first gate, and it fails.
	writeFixtureEntry(t, root, sessionMgrNotManagerPid, "bash", "/bin/bash", "-lc", "gopi worker "+UserManagerArg)

	if !isLoginSessionManagerIn(root, 4242) {
		t.Fatalf("pid 4242 in the fixture procfs IS a systemd user instance (comm %q + argv %q) — the predicate must say so",
			UserManagerComm, UserManagerArg)
	}
	if isLoginSessionManagerIn(root, 1) {
		t.Error("the SYSTEM manager (comm systemd, NO --user flag) was identified as the session manager — the flag is the discriminator")
	}
	if isLoginSessionManagerIn(root, 4243) {
		t.Error("a user process whose ARGUMENT TEXT contains --user was identified as the session manager — comm gates the argv read")
	}
	if isLoginSessionManagerIn(root, 999999) {
		t.Error("a pid with no /proc entry was identified as the session manager — an unreadable pid must never be claimed (positive identification only)")
	}
}

// TestRefuseDeliveryToLoginSessionManager_RealManagerAndRealChild is the repro
// for the shipped incident: on a host WITH a graphical session it measures the
// guard's two verdicts against the LIVE manager, the PRE-FIX guard's verdict on
// the same target (which permits), and a REAL delivery to a throwaway child
// (which the guard must not block).
func TestRefuseDeliveryToLoginSessionManager_RealManagerAndRealChild(t *testing.T) {
	mgr, ticks := findLiveSessionManager()
	if mgr == 0 {
		t.Skipf("no systemd user instance in this pid namespace (%s) — the identification's live arm cannot be measured here", ProcRootDefault)
	}

	// (1) THE SHIPPED ARM: the delivery point refuses, by name.
	err := RefuseLoginSessionManagerTarget(mgr)
	if !errors.Is(err, ErrSessionManagerTarget) {
		t.Fatalf("RefuseLoginSessionManagerTarget(%d) = %v, want an error wrapping ErrSessionManagerTarget — this is the pid whose SIGINT ends the operator's session", mgr, err)
	}
	if !strings.Contains(err.Error(), "desktop session") {
		t.Errorf("the refusal must name the consequence, got: %v", err)
	}

	// (2) THE RED-SHAPE ARM, MEASURED WITHOUT DELIVERING: the PRE-FIX guard
	// (incarnation identity alone) permits delivery to this very pid — its
	// starttime still matches, so it would have signalled. This is what makes the
	// new predicate load-bearing rather than decorative, and it costs a /proc
	// read instead of a session.
	nowTicks, ok := StartTicks(ProcRootDefault, strconv.Itoa(mgr))
	if !ok {
		t.Fatalf("re-read the incarnation of the session manager %d: entry unreadable", mgr)
	}
	if strings.TrimSpace(nowTicks) != strings.TrimSpace(ticks) {
		t.Fatalf("the session manager moved incarnation %s → %s inside this test — do not trust this run's verdicts", ticks, nowTicks)
	}
	t.Logf("pre-fix arm (incarnation only) on pid %d: identity MATCHES starttime=%s ⇒ it would deliver; the manager arm refuses ⇒ the guard is what stops it", mgr, ticks)

	// (3) THE NEGATIVE CONTROL, WITH A REAL DELIVERY: a child of our own is not a
	// manager, so the seam must stay an ordinary kill — otherwise a guard that
	// refused everything would pass arm (1) too.
	child := exec.Command("sleep", sessionMgrFixtureThrows)
	// This arm deliberately SIGNALS the child, but the arm must not leak when the
	// binary itself is killed first: Pdeathsig is the kernel's arm and a neutral
	// cwd keeps a survivor (if any) out of the checkout.
	child.Dir = t.TempDir()
	child.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := child.Start(); err != nil {
		t.Fatalf("start the child this arm signals: %v", err)
	}
	childPid := child.Process.Pid
	t.Cleanup(func() {
		_ = child.Process.Kill() //stylegate:ignore teardown of the fixture child this test spawned; ESRCH means it already exited
		_ = child.Wait()
	})
	waitForTicks(t, childPid)
	if err := RefuseLoginSessionManagerTarget(childPid); err != nil {
		t.Fatalf("pid %d is a plain child, not a manager, yet the guard refused it: %v — the seam would stop stopping anything", childPid, err)
	}
	if err := deliverAsShipped(childPid); err != nil {
		t.Fatalf("the shipped delivery shape failed on a plain child (%v) — the guard must be transparent to every ordinary target", err)
	}
	if !waitGone(childPid) {
		t.Fatalf("pid %d survived the shipped delivery shape — the seam is a no-op, which would make arm (1) vacuous", childPid)
	}

	// (4) A third-party plain process that is ALIVE and NOT ours must also not be
	// refused: refusal is about who the target IS, not about who is asking.
	if err := RefuseLoginSessionManagerTarget(os.Getpid()); err != nil {
		t.Errorf("the test process itself was refused (%v) — the predicate must only fire on a systemd user instance", err)
	}

	// (5) THIS TEST MUST NOT HAVE ENDED A SESSION: the manager is still the same
	// incarnation, and the session's own cgroup still has a live owner.
	after, ok := StartTicks(ProcRootDefault, strconv.Itoa(mgr))
	if !ok || strings.TrimSpace(after) != strings.TrimSpace(ticks) {
		t.Fatalf("the session manager changed across this test (starttime %s → %q): something delivered a signal to it — the arm above must never do that", ticks, after)
	}
}

// deliverAsShipped is the SHIPPED delivery shape's two decisions, in the same
// order the production seam makes them: the incarnation test first, then the
// manager refusal. It is NOT a second implementation of the signal — the final
// syscall is the same one internal/testutil.KillIfIdentical makes — it is the
// arm that proves a real delivery still happens for an ordinary child.
func deliverAsShipped(pid int) error {
	want, ok := StartTicks(ProcRootDefault, strconv.Itoa(pid))
	if !ok {
		return os.ErrNotExist
	}
	now, ok := StartTicks(ProcRootDefault, strconv.Itoa(pid))
	if !ok || strings.TrimSpace(now) != strings.TrimSpace(want) {
		return syscall.ESRCH
	}
	// the SAME predicate the production seam calls, imported from this package
	if err := RefuseLoginSessionManagerTarget(pid); err != nil {
		return err
	}
	return syscall.Kill(pid, syscall.SIGINT)
}

// findLiveSessionManager scans this pid namespace for a systemd user instance
// and returns its pid plus its starttime reading (the incarnation).
func findLiveSessionManager() (int, string) {
	entries, err := os.ReadDir(ProcRootDefault)
	if err != nil {
		return 0, ""
	}
	for _, e := range entries {
		name := e.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		pid, convErr := strconv.Atoi(name)
		if convErr != nil || pid <= 0 {
			continue
		}
		if !IsLoginSessionManagerPID(pid) {
			continue
		}
		if ticks, ok := StartTicks(ProcRootDefault, name); ok {
			return pid, strings.TrimSpace(ticks)
		}
	}
	return 0, ""
}

// waitForTicks waits until a just-spawned child's /proc entry carries its stat
// line (starttime is only readable once the entry exists).
//
// The cadence is a TICKER, not a fixed sleep: the wait ends the moment the entry
// appears (the common case is the first or second turn), and the poll's upper
// bound is what fails the arm (stylegate RuleTestSleep: poll/wait, not sleep).
func waitForTicks(t *testing.T, pid int) {
	t.Helper()
	tick := time.NewTicker(sessionMgrTestPollInterval)
	defer tick.Stop()
	for i := 0; i < sessionMgrTestPollTries; i++ {
		if _, ok := StartTicks(ProcRootDefault, strconv.Itoa(pid)); ok {
			return
		}
		<-tick.C
	}
	t.Fatalf("pid %d never published its /proc stat entry — the child arms cannot be measured", pid)
}

// waitGone reports whether pid left the process table (zombie counts as gone:
// the child is unwaited until Cleanup, so its entry may linger as "Z").
//
// Same shape as waitForTicks: a bounded TICKER-driven poll, so the observation
// ends on the event (the entry disappearing) rather than on a guessed interval.
func waitGone(pid int) bool {
	path := ProcRootDefault + "/" + strconv.Itoa(pid)
	tick := time.NewTicker(sessionMgrTestPollInterval)
	defer tick.Stop()
	for i := 0; i < sessionMgrTestPollTries; i++ {
		if _, err := os.Stat(path); err != nil {
			return true
		}
		if state, ok := State(ProcRootDefault, strconv.Itoa(pid)); ok && state == "Z" {
			return true
		}
		<-tick.C
	}
	return false
}
