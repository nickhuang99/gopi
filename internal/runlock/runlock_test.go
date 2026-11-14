package runlock

// The tests below are the epic's acceptance in executable form, and they are
// deliberately shaped so a green cannot be vacuous:
//
//   - the "second process is refused" arm uses a REAL child process (an
//     os/exec of this test binary), not a second Handle in the same process —
//     a lock that only refuses within one address space would prove nothing;
//   - the kill -9 arm kills that child for real and re-acquires IMMEDIATELY
//     (no sleep, no sweep, no waiting): the kernel's release is what the lock
//     is for;
//   - and the REVERSE CONTROL (TestWithoutTheLockBothProcessesEnter) runs the
//     very same two-child harness with the lock switched off and asserts BOTH
//     children got in. That is what proves the refusal above comes from the
//     lock and not from the harness being serialised by something else.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// Child-mode selector: this test binary re-executes ITSELF with these set, so
// the "other process" in every cross-process arm is a real process.
const (
	helperModeEnv = "RUNLOCK_TEST_HELPER_MODE"
	helperDirEnv  = "RUNLOCK_TEST_HELPER_DIR"
	helperHoldEnv = "RUNLOCK_TEST_HELPER_HOLD_MS"

	helperModeLock   = "lock-and-hold"
	helperModeNoLock = "no-lock-and-hold"
	helperModeTry    = "try-lock-once"

	helperLockedLine = "LOCKED"
	helperHeldLine   = "HELD"
	helperEntered    = "ENTERED"

	defaultHoldMS = 20000
	tryTimeout    = 10 * time.Second
)

// TestMain doubles as the child's entry point: when the helper env is set, the
// process performs the requested mode and exits, so the parent's os/exec sees a
// genuine second process holding (or not holding) the lock.
//
// The PARENT half routes through testutil.RunSuiteMain — the ONE entry point
// that pins this suite's scratch root off the / partition (Rule 22: the
// t.TempDir() fixtures below would otherwise land on /tmp, on the / partition
// behind the 2026-09-11 exhaustion) and installs the notify-env sandbox,
// without which this suite reads the operator's live settings.json. That is
// what internal/testutil's TestEveryTempUsingSuiteIsPinned and
// TestEveryTestMainSuiteIsNotifyHermetic require of every suite, and the pin is
// the ADDITIVE part: this TestMain's own job (the child mode) is untouched.
//
// The CHILD half must NOT take the suite path: that process holds the lock for
// helperHoldMS and has no scratch root of its own to pin, and entering the
// suite entry point there would os.Exit on the pin's verdict instead of running
// the helper. So the child check comes FIRST, and the parent's cleanup (the pin
// tears its own scratch root down) stays with the suite entry point.
func TestMain(m *testing.M) {
	if mode := os.Getenv(helperModeEnv); mode != "" {
		os.Exit(runHelper(mode))
	}
	testutil.RunSuiteMain(m, "runlock")
}

// runHelper is the child body. It prints ONE line the parent waits for, then
// holds until killed (or until the hold budget expires, so a broken parent can
// never leave a child behind forever).
func runHelper(mode string) int {
	dir := os.Getenv(helperDirEnv)
	holdMS := defaultHoldMS
	if v := os.Getenv(helperHoldEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			holdMS = n
		}
	}
	var h *Handle
	switch mode {
	case helperModeLock, helperModeTry:
		hc, err := Lock(dir)
		if err != nil {
			fmt.Printf("%s %v\n", helperHeldLine, err)
			return 1
		}
		h = hc
		defer func() { _ = h.Release() }() //stylegate:ignore the helper process's own teardown; the parent measures the lock, not this release
		fmt.Printf("%s pid=%d path=%s\n", helperLockedLine, os.Getpid(), hc.Path())
	case helperModeNoLock:
		// The reverse control: identical harness, lock switched OFF. The file
		// is still opened so the two arms differ in exactly one thing.
		if _, err := os.OpenFile(PathIn(dir), os.O_CREATE|os.O_RDWR, LockFileMode); err != nil {
			fmt.Printf("%s %v\n", helperHeldLine, err)
			return 1
		}
		fmt.Printf("%s pid=%d\n", helperEntered, os.Getpid())
	default:
		fmt.Printf("%s unknown mode %q\n", helperHeldLine, mode)
		return 2
	}
	if mode == helperModeTry {
		// One attempt, report, exit — leaves nothing behind.
		return 0
	}
	// The hold IS the fixture: this helper's whole job is to keep a live process
	// holding the flock so another process can be refused. There is nothing to
	// poll for — waiting is the subject, not a synchronisation shortcut.
	time.Sleep(time.Duration(holdMS) * time.Millisecond) //stylegate:allow-sleep the helper exists to be a LIVE lock holder for the duration; waiting is the fixture
	return 0
}

// startHelper launches this test binary as a real child process in dir and
// waits for its first stdout line. The returned command's Wait is the caller's.
func startHelper(t *testing.T, dir, mode string) (*exec.Cmd, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestMain")
	cmd.Env = append(os.Environ(),
		helperModeEnv+"="+mode,
		helperDirEnv+"="+dir,
		helperHoldEnv+"="+strconv.Itoa(defaultHoldMS),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper %s: %v", mode, err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			_ = cmd.Process.Kill()    //stylegate:ignore teardown of the helper THIS test started; ALREADY-EXITED is the expected error
			_, _ = cmd.Process.Wait() //stylegate:ignore same teardown; the child was reaped by the kill above
		}
	})
	lineCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		if sc.Scan() {
			lineCh <- sc.Text()
			return
		}
		lineCh <- ""
	}()
	select {
	case line := <-lineCh:
		if line == "" {
			t.Fatalf("helper %s produced no first line", mode)
		}
		return cmd, line
	case <-time.After(tryTimeout):
		t.Fatalf("helper %s did not announce itself within %s", mode, tryTimeout)
		return nil, ""
	}
}

// TestSecondAcquireIsRefusedAndNamesTheHolder is the in-process half of the
// refusal: while one Handle is held, a second Lock on the SAME directory must
// fail with ErrHeld, and the refusal must name the lock path and the holder's
// pid (the file's contents are diagnosis for exactly this).
func TestSecondAcquireIsRefusedAndNamesTheHolder(t *testing.T) {
	dir := t.TempDir()
	first, err := Lock(dir)
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	defer func() { _ = first.Release() }() //stylegate:ignore the test's own teardown

	second, err := Lock(dir)
	if err == nil {
		_ = second.Release() //stylegate:ignore teardown of a handle the test is about to fail on
		t.Fatalf("second Lock on %q SUCCEEDED — two processes could execute the same run directory", dir)
	}
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("second Lock error = %v, want it to wrap ErrHeld (the kernel's EWOULDBLOCK)", err)
	}
	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("second Lock error %v is not a *HeldError", err)
	}
	if want := PathIn(dir); held.Path != want {
		t.Errorf("refusal names %q, want the lock path %q", held.Path, want)
	}
	if held.Holder.PID != os.Getpid() {
		t.Errorf("refusal holder pid = %d, want this process's pid %d (the stamp the first holding wrote)", held.Holder.PID, os.Getpid())
	}
	msg := err.Error()
	if !strings.Contains(msg, LockFileName) {
		t.Errorf("refusal text %q does not name %s", msg, LockFileName)
	}
	if !strings.Contains(msg, "pid") {
		t.Errorf("refusal text %q names no holder pid", msg)
	}
}

// TestCrossProcessSecondProcessIsRefused is the acceptance's real shape: one
// PROCESS holds, a second PROCESS is refused at the lock, exits non-zero, and
// its output names the lock path and the holder's pid.
func TestCrossProcessSecondProcessIsRefused(t *testing.T) {
	dir := t.TempDir()
	holder, line := startHelper(t, dir, helperModeLock)
	if !strings.HasPrefix(line, helperLockedLine) {
		t.Fatalf("holder helper said %q, want %q", line, helperLockedLine)
	}
	if !strings.Contains(line, PathIn(dir)) {
		t.Errorf("holder's own line %q does not name the lock path %q", line, PathIn(dir))
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	second := exec.Command(exe, "-test.run=TestMain")
	second.Env = append(os.Environ(),
		helperModeEnv+"="+helperModeTry,
		helperDirEnv+"="+dir,
	)
	out, runErr := second.CombinedOutput()
	text := string(out)
	if runErr == nil {
		t.Fatalf("the second process ACQUIRED the lock on %q while pid=%d held it — output %q", dir, holder.Process.Pid, text)
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		t.Fatalf("second process error = %v, want a non-zero exit", runErr)
	}
	if code := exitErr.ExitCode(); code == 0 {
		t.Fatalf("second process exit code = 0, want non-zero")
	}
	if !strings.Contains(text, helperHeldLine) {
		t.Errorf("second process output %q does not say it was refused", text)
	}
	if !strings.Contains(text, PathIn(dir)) {
		t.Errorf("refusal %q does not name the lock path %q", text, PathIn(dir))
	}
	if !strings.Contains(text, "pid "+strconv.Itoa(holder.Process.Pid)) {
		t.Errorf("refusal %q does not name the holder pid %d", text, holder.Process.Pid)
	}
}

// TestKillNineReleasesImmediately is the property the whole mechanism exists
// for: SIGKILL the holder and the NEXT attempt succeeds with NO waiting — no
// sleep, no sweep, no cleaner.
func TestKillNineReleasesImmediately(t *testing.T) {
	dir := t.TempDir()
	holder, line := startHelper(t, dir, helperModeLock)
	if !strings.HasPrefix(line, helperLockedLine) {
		t.Fatalf("holder helper said %q", line)
	}
	// Prove the lock is really held first, so a "it worked after the kill"
	// reading cannot be green for the wrong reason.
	if _, err := Lock(dir); !errors.Is(err, ErrHeld) {
		t.Fatalf("before the kill, a second Lock = %v, want ErrHeld", err)
	}
	if err := holder.Process.Kill(); err != nil {
		t.Fatalf("kill -9 holder: %v", err)
	}
	_, _ = holder.Process.Wait() //stylegate:ignore the child was SIGKILLed; its exit status is the point, not an error

	h, err := Lock(dir)
	if err != nil {
		t.Fatalf("Lock immediately after SIGKILL = %v, want success (the kernel releases the flock when the process dies)", err)
	}
	defer func() { _ = h.Release() }() //stylegate:ignore the test's own teardown; the assertion is above
	raw, readErr := os.ReadFile(h.Path())
	if readErr != nil {
		t.Fatalf("read the lock file after re-acquiring: %v", readErr)
	}
	if got := parseHolder(string(raw)).PID; got != os.Getpid() {
		t.Errorf("post-kill holder stamp pid = %d, want this process %d (the new holder owns the stamp)", got, os.Getpid())
	}
}

// TestDifferentDirectoriesDoNotBlock is the reverse of a machine-wide
// singleton: two DIFFERENT run ids execute concurrently and both succeed.
func TestDifferentDirectoriesDoNotBlock(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "aaaaaaaa-1111-2222-3333-444444444444")
	dirB := filepath.Join(root, "bbbbbbbb-1111-2222-3333-444444444444")

	a, lineA := startHelper(t, dirA, helperModeLock)
	if !strings.HasPrefix(lineA, helperLockedLine) {
		t.Fatalf("helper A said %q", lineA)
	}
	_ = a //stylegate:ignore deliberate: `a` is the FIRST holder and is kept alive by this reference; the test asserts the SECOND lock succeeds beside it
	hb, err := Lock(dirB)
	if err != nil {
		t.Fatalf("Lock(different dir) = %v, want success — the lock is per RUN DIRECTORY, not a machine-wide singleton", err)
	}
	defer func() { _ = hb.Release() }() //stylegate:ignore the test's own teardown
	if hb.Dir() != dirB {
		t.Errorf("handle dir = %q, want %q", hb.Dir(), dirB)
	}
}

// TestWithoutTheLockBothProcessesEnter is the REVERSE CONTROL that keeps this
// suite from being vacuously green: the identical two-process harness, with the
// lock switched off, must let BOTH children in. If this arm ever went red, the
// harness itself would be serialising the children and the refusals above would
// prove nothing about the lock.
func TestWithoutTheLockBothProcessesEnter(t *testing.T) {
	dir := t.TempDir()
	first, lineFirst := startHelper(t, dir, helperModeNoLock)
	if !strings.HasPrefix(lineFirst, helperEntered) {
		t.Fatalf("lockless helper 1 said %q, want %q", lineFirst, helperEntered)
	}
	second, lineSecond := startHelper(t, dir, helperModeNoLock)
	if !strings.HasPrefix(lineSecond, helperEntered) {
		t.Fatalf("lockless helper 2 said %q, want %q — WITHOUT the lock both processes must be able to enter", lineSecond, helperEntered)
	}
	if first.Process.Pid == second.Process.Pid {
		t.Fatalf("both entries report pid %d — not two processes", first.Process.Pid)
	}
}

// TestLockFileSurvivesAndCarriesTheHolder pins two contract points: the file is
// NOT deleted by a release (deleting it races — a new inode at the same path
// would let two processes hold the same directory), and its content shows who
// holds it while it is held.
func TestLockFileSurvivesAndCarriesTheHolder(t *testing.T) {
	dir := t.TempDir()
	path := PathIn(dir)

	h, err := Lock(dir)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read lock file while held: %v", readErr)
	}
	stamp := parseHolder(string(raw))
	if stamp.PID != os.Getpid() {
		t.Errorf("stamp pid = %d, want %d", stamp.PID, os.Getpid())
	}
	if stamp.AcquiredAt == "" {
		t.Errorf("stamp carries no acquired_at: %q", string(raw))
	}
	if stamp.Host == "" {
		t.Errorf("stamp carries no host: %q", string(raw))
	}
	if err := h.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("lock file after release: %v — the file must SURVIVE (flock's subject is the inode; unlinking races)", statErr)
	}
	// Releasing twice is ordinary (a defer beside an explicit call) and must
	// not be an error.
	if err := h.Release(); err != nil {
		t.Errorf("second Release = %v, want nil (idempotent)", err)
	}
	// And the directory must be takeable again by a fresh handle.
	again, err := Lock(dir)
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	_ = again.Release() //stylegate:ignore the test's own teardown
}

// TestEmptyDirectoryIsRefused keeps a silent no-op out of the seam: an empty
// (or whitespace) directory is an unaddressable lock, not "lock the current
// directory".
func TestEmptyDirectoryIsRefused(t *testing.T) {
	for _, dir := range []string{"", "   "} {
		h, err := Lock(dir)
		if err == nil {
			_ = h.Release() //stylegate:ignore teardown of a handle the test is about to fail on
			t.Fatalf("Lock(%q) = success, want a refusal naming the empty run directory", dir)
		}
		if !strings.Contains(err.Error(), LockFileName) {
			t.Errorf("Lock(%q) error %q does not name %s", dir, err, LockFileName)
		}
	}
}

// TestParseHolderRoundTrips pins the stamp's parser directly, so a change to
// the written shape cannot silently make refusals holder-less.
func TestParseHolderRoundTrips(t *testing.T) {
	text := strings.Join([]string{
		"pid=4321",
		"startticks=987654",
		"acquired_at=2026-11-05T01:02:03Z",
		"host=host-a",
		"",
	}, stampLineEnd)
	got := parseHolder(text)
	want := Holder{PID: 4321, StartTicks: "987654", AcquiredAt: "2026-11-05T01:02:03Z", Host: "host-a"}
	if got != want {
		t.Fatalf("parseHolder = %+v, want %+v", got, want)
	}
	if s := got.String(); !strings.Contains(s, "pid 4321") || !strings.Contains(s, "host host-a") {
		t.Errorf("Holder.String() = %q, want it to name the pid and the host", s)
	}
	// An empty file is "no holder recorded", not a bogus zero holder.
	if empty := parseHolder(""); empty != (Holder{}) {
		t.Errorf("parseHolder(\"\") = %+v, want the zero Holder", empty)
	}
	if s := (Holder{}).String(); s != "" {
		t.Errorf("zero Holder.String() = %q, want \"\" so a refusal says 'no holder stamp' instead of inventing one", s)
	}
}

// The syscall import IS used by the child helper's hold path only through
// runlock.Lock, so nothing here re-states LOCK_NB: the constant lives in
// runlock.go and a test that mirrored it would be a second definition.
