package scratch_test

// suite_package_lock_test.go — the cross-run exclusion's contract, driven through
// REAL processes.
//
// What has to be true, and why each arm is shaped the way it is:
//
//   - "the second run SEES the first and says so": a lock taken in one process must
//     be refused in ANOTHER process, and the refusal must carry the holder's pid,
//     its start instant and the tree it works in. Two handles inside one address
//     space would prove nothing (the same trap internal/runlock's tests call out).
//   - "it enters normally once the lock is released": the release must be visible to
//     the very next process, with no sweep and no waiting.
//   - "REVERSE CONTROL": the same two-child harness with the lock switched off
//     (GOPI_SUITE_PACKAGE_LOCK=0) must have BOTH children take it. Without this arm a
//     green above could be the harness serialising itself rather than the lock
//     refusing anybody.
//   - "an abandoned lock is broken by the EXISTING ladder": a directory whose
//     recorded owner is provably dead is broken on the next take — that is
//     OwnerVerdictFor's judgement, which the sweep already applies to every entry in
//     this tree, not a second liveness implementation.
//   - "the sweep never collects it": the family is excluded at any age, because a
//     package lock directory is the flock's own subject.
//   - "a nested suite is not a sibling": the holder being an ANCESTOR of this process
//     means a gate spawned `go test` as a child, which must proceed.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// The helper's protocol: which mode the child runs, which lock to address, and the
// ONE line the parent waits for. Constants for the usual reason (Rules 1/13).
const (
	splHelperModeEnv  = "GOPI_SPL_HELPER_MODE"
	splHelperHomeEnv  = "GOPI_SPL_HELPER_HOME"
	splHelperSuiteEnv = "GOPI_SPL_HELPER_SUITE"
	splHelperModeHold = "hold"
	splHelperModeTry  = "try"
	splAcquiredLine   = "SPL-ACQUIRED"
	splHeldLine       = "SPL-HELD"
	splBusyLine       = "SPL-BUSY"

	// splSuiteName is the package name the fixtures lock — never a real suite's, so a
	// test run can never collide with a real suite's exclusion.
	splSuiteName = "fixture-package"
)

// runSuitePackageLockHelper is the CHILD entry point: it must run BEFORE any pin,
// because it is a plain lock probe with no suite of its own.
//
// It returns (true, exitCode) when it handled the child mode and the process must
// exit with that code: a REFUSAL must not exit 0, or the parent test would read
// "refused" as "succeeded" (the very silent failure this mechanism is about).
func runSuitePackageLockHelper() (bool, int) {
	mode := os.Getenv(splHelperModeEnv)
	if mode == "" {
		return false, 0
	}
	home := os.Getenv(splHelperHomeEnv)
	suite := os.Getenv(splHelperSuiteEnv)
	lock, err := scratch.AcquireSuitePackageLock(home, suite, "child-workdir")
	var busy *scratch.SuitePackageBusyError
	switch {
	case errors.As(err, &busy):
		fmt.Printf("%s %s %s\n", splBusyLine, busy.Outcome, busy.Evidence)
		return true, splBusyExitCode
	case err != nil:
		fmt.Printf("SPL-ERROR %v\n", err)
		return true, splErrorExitCode
	}
	fmt.Printf("%s %s\n", splAcquiredLine, lock.Outcome)
	if mode == splHelperModeHold {
		// Hold until the parent kills us (or the budget expires, so a broken parent
		// can never leave a child behind forever) — the hold IS the lock's lifetime,
		// exactly as a real suite's run is.
		fmt.Printf("%s pid=%d\n", splHeldLine, os.Getpid())
		time.Sleep(splHoldBudget)
		return true, 0
	}
	return true, 0
}

// The helper's exit codes: 0 = took the lock (or held it), 1 = the mechanism
// refused, 2 = an infrastructure failure it must not dress up as a refusal.
const (
	splBusyExitCode  = 1
	splErrorExitCode = 2
)

// splHoldBudget bounds the holder child's life.
const splHoldBudget = 20 * time.Second

// splLockHome makes a fixture scratch home for one test, inside this suite's own
// pinned root (never the real scratch home — Rule 6: a test never touches the
// developer's live state, and never a live suite's lock).
func splLockHome(t *testing.T) string {
	t.Helper()
	// NOT t.TempDir(): the child processes resolve the same path, and a fixture whose
	// name carries the test's own name keeps two parallel arms from meeting.
	home := filepath.Join(os.Getenv("TMPDIR"), "spl-home-"+splSafeName(t.Name()))
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create fixture lock home %s: %v", home, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) }) //stylegate:ignore the fixture home is this test's own tree, created above
	return home
}

// splSafeName flattens a subtest name into one path element.
func splSafeName(name string) string {
	return strings.NewReplacer("/", "_", " ", "_").Replace(name)
}

// splSpawn starts a helper child in the given mode and returns its command plus a
// reader for its stdout.
func splSpawn(t *testing.T, home, suite, mode string, env ...string) *exec.Cmd {
	t.Helper()
	// The child is THIS test binary, re-executed: it inherits the pin's TMPDIR, and
	// the helper branch runs before any suite machinery.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(),
		splHelperModeEnv+"="+mode,
		splHelperHomeEnv+"="+home,
		splHelperSuiteEnv+"="+suite,
	)
	cmd.Env = append(cmd.Env, env...)
	return cmd
}

// splStartHolder starts the holder child and returns it together with its stdout
// pipe. The pipe is taken BEFORE Start on purpose: exec refuses StdoutPipe after the
// process has started ("StdoutPipe after process started"), which is a harness
// failure that would look like the mechanism not reporting.
func splStartHolder(t *testing.T, home string) (*exec.Cmd, io.Reader) {
	t.Helper()
	holder := splSpawn(t, home, splSuiteName, splHelperModeHold)
	pipe, err := holder.StdoutPipe()
	if err != nil {
		t.Fatalf("take the holder child's stdout pipe: %v", err)
	}
	if err := holder.Start(); err != nil {
		t.Fatalf("start the holder child: %v", err)
	}
	return holder, pipe
}

// TestSuitePackageLock_SecondRunNamesTheFirst is the epic's core assertion: while one
// process holds the package, a second process is REFUSED, and the refusal names the
// holder — pid, when it started, and the tree it works in.
func TestSuitePackageLock_SecondRunNamesTheFirst(t *testing.T) {
	home := splLockHome(t)

	holder, holderOut := splStartHolder(t, home)
	defer func() {
		_ = holder.Process.Kill() //stylegate:ignore this test's own child; the kill is the teardown
		_ = holder.Wait()         //stylegate:ignore reaping this test's own child
	}()
	// Wait for the holder to say it holds the lock, so the second take below cannot
	// race the first one's creation.
	if out, err := readUntil(holderOut, splHeldLine); err != nil {
		t.Fatalf("holder child did not report the lock: %v\n%s", err, out)
	}

	second := splSpawn(t, home, splSuiteName, splHelperModeTry)
	raw, err := second.CombinedOutput()
	text := string(raw)
	if err == nil {
		t.Fatalf("a second process took the package lock while the first held it — the exclusion does not exist\n%s", text)
	}
	if !strings.Contains(text, splBusyLine) {
		t.Fatalf("the second process was refused but did not report the holder (want a %q line):\n%s", splBusyLine, text)
	}
	// The refusal must NAME the holder: its pid (a number somebody can act on), its
	// start instant, and the directory it works in.
	wantPid := fmt.Sprintf("pid %d", holder.Process.Pid)
	if !strings.Contains(text, wantPid) {
		t.Errorf("the refusal does not name the holder's %q:\n%s", wantPid, text)
	}
	for _, want := range []string{"started ", "directory child-workdir", "verdict live"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not carry %q — an operator cannot recognise the sibling run from this sentence:\n%s", want, text)
		}
	}
}

// TestSuitePackageLock_EntersAfterRelease is the "and then it works" half: the very
// next process enters once the holder is gone, with no sweep and no waiting.
func TestSuitePackageLock_EntersAfterRelease(t *testing.T) {
	home := splLockHome(t)

	holder, holderOut := splStartHolder(t, home)
	if out, err := readUntil(holderOut, splHeldLine); err != nil {
		t.Fatalf("holder child did not report the lock: %v\n%s", err, out)
	}
	// Kill it — the CRASH shape, which is the one a suite can actually suffer.
	if err := holder.Process.Kill(); err != nil {
		t.Fatalf("kill the holder child: %v", err)
	}
	_ = holder.Wait() //stylegate:ignore reaping this test's own child

	try := splSpawn(t, home, splSuiteName, splHelperModeTry)
	raw, err := try.CombinedOutput()
	text := string(raw)
	if err != nil {
		t.Fatalf("the next run could not enter after the holder died — an abandoned lock is not being broken: %v\n%s", err, text)
	}
	if !strings.Contains(text, splAcquiredLine) {
		t.Fatalf("the next run did not report taking the lock:\n%s", text)
	}
	if !strings.Contains(text, string(scratch.SuitePackageLockAcquired)) {
		t.Errorf("the take did not report the %q outcome:\n%s", scratch.SuitePackageLockAcquired, text)
	}
}

// TestSuitePackageLock_ReverseControlBothEnter is the arm that makes the two above
// mean something: the SAME harness with the lock switched off has both children take
// it. If this arm failed, the green above could be the harness serialising itself.
func TestSuitePackageLock_ReverseControlBothEnter(t *testing.T) {
	home := splLockHome(t)

	holder := splSpawn(t, home, splSuiteName, splHelperModeTry, scratch.SuitePackageLockOffEnvName+"=0")
	holderOut, err := holder.CombinedOutput()
	if err != nil {
		t.Fatalf("the escape-hold child failed: %v\n%s", err, holderOut)
	}
	first := string(holderOut)
	if !strings.Contains(first, string(scratch.SuitePackageLockOff)) {
		t.Fatalf("with %s=0 the take did not report the %q outcome — the escape is not visible:\n%s",
			scratch.SuitePackageLockOffEnvName, scratch.SuitePackageLockOff, first)
	}

	second := splSpawn(t, home, splSuiteName, splHelperModeTry, scratch.SuitePackageLockOffEnvName+"=0")
	raw, err := second.CombinedOutput()
	text := string(raw)
	if err != nil {
		t.Fatalf("the second escape child failed: %v\n%s", err, text)
	}
	if !strings.Contains(text, splAcquiredLine) {
		t.Fatalf("with the lock disabled the second process was still refused — the refusal is not coming from the lock:\n%s", text)
	}
}

// TestSuitePackageLock_AbandonedLockIsBroken pins that a lock directory whose
// recorded owner is provably dead is broken by the EXISTING ladder on the next take,
// and that an UNMEASURABLE record is refused instead (Rule 15a: unknown is never a
// yes).
func TestSuitePackageLock_AbandonedLockIsBroken(t *testing.T) {
	home := splLockHome(t)
	dir := scratch.SuitePackageLockDir(home, splSuiteName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the fixture lock dir: %v", err)
	}
	// A record naming a pid that cannot exist, with an incarnation that cannot match
	// it: the ladder answers GONE, which is the crash shape.
	body := fmt.Sprintf("%s=%d\n%s=%d\n%s=ghost-workdir\n",
		scratch.HolderKeyPID, 4194304, scratch.HolderKeyStart, 1, scratch.SuitePackageLockWorkDirKey)
	if err := os.WriteFile(filepath.Join(dir, scratch.HolderFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write the fixture holder record: %v", err)
	}

	try := splSpawn(t, home, splSuiteName, splHelperModeTry)
	raw, err := try.CombinedOutput()
	text := string(raw)
	if err != nil {
		t.Fatalf("a lock left by a dead process was not broken — a killed run would wedge the package forever: %v\n%s", err, text)
	}
	if !strings.Contains(text, splAcquiredLine) {
		t.Fatalf("the take did not report the lock:\n%s", text)
	}
}

// TestSuitePackageLock_UnmeasurableRecordRefuses pins the other side of the ladder: a
// lock directory with NO readable record is refused, not broken.
func TestSuitePackageLock_UnmeasurableRecordRefuses(t *testing.T) {
	home := splLockHome(t)
	dir := scratch.SuitePackageLockDir(home, splSuiteName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the fixture lock dir: %v", err)
	}
	// No .holder at all: nothing recorded an incarnation.
	try := splSpawn(t, home, splSuiteName, splHelperModeTry)
	raw, err := try.CombinedOutput()
	text := string(raw)
	if err == nil {
		t.Fatalf("a lock directory with no owner record was taken — an unmeasured holder is not a licence to run:\n%s", text)
	}
	if !strings.Contains(text, scratch.SuitePackageLockBusyUnmeasured) {
		t.Fatalf("the refusal did not use the %q verdict (it reported something else, which hides WHY):\n%s",
			scratch.SuitePackageLockBusyUnmeasured, text)
	}
}

// TestSuitePackageLock_SweepNeverCollectsTheFamily pins the exclusion: an aged lock
// directory is never a sweep candidate, because its existence IS the lock.
func TestSuitePackageLock_SweepNeverCollectsTheFamily(t *testing.T) {
	home := splLockHome(t)
	dir := scratch.SuitePackageLockDir(home, splSuiteName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the fixture lock dir: %v", err)
	}
	// Aged far past every window the sweep applies, so ONLY the namespace arm can save
	// it — the shape that makes this a real assertion.
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("age the fixture lock dir: %v", err)
	}
	verdicts, err := scratch.SweepHomeVerdicts(home, time.Minute)
	if err != nil {
		t.Fatalf("sweep verdicts over %s: %v", home, err)
	}
	for _, v := range verdicts {
		if v.Path != dir {
			continue
		}
		if v.Collectable {
			t.Fatalf("the sweep considers the package lock directory collectable (%s) — removing it re-opens "+
				"the \"second run takes a fresh directory\" hole", v.Reason)
		}
		if v.Reason != scratch.EntryKeepPackageLock {
			t.Errorf("the package lock was kept for the WRONG reason %q (want %q)", v.Reason, scratch.EntryKeepPackageLock)
		}
		return
	}
	t.Fatalf("the sweep did not report the package lock directory at all — a verdict list that skips it cannot "+
		"prove the exclusion is in force (home %s)", home)
}

// TestSuitePackageLock_NestedSuiteProceeds pins the one exception: a holder that is an
// ANCESTOR of this process is not a sibling RUN. Without it, every gate that spawns
// `go test ./cmd/agent` as a child would be refused by its own parent.
func TestSuitePackageLock_NestedSuiteProceeds(t *testing.T) {
	home := splLockHome(t)
	dir := scratch.SuitePackageLockDir(home, splSuiteName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the fixture lock dir: %v", err)
	}
	// Write a record naming THIS test process — the parent that would spawn the child.
	body, err := scratch.OwnerRecordText(os.Getpid())
	if err != nil {
		t.Fatalf("render this process's owner record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, scratch.HolderFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write the fixture holder record: %v", err)
	}
	try := splSpawn(t, home, splSuiteName, splHelperModeTry)
	raw, err := try.CombinedOutput()
	text := string(raw)
	if err != nil {
		t.Fatalf("a child of the lock's holder was refused — a suite started BY a suite must proceed: %v\n%s", err, text)
	}
	if !strings.Contains(text, string(scratch.SuitePackageLockNested)) {
		t.Fatalf("the nested take did not report the %q outcome, so the reader cannot tell it from a real take:\n%s",
			scratch.SuitePackageLockNested, text)
	}
}

// readUntil waits for one marker line from a child's stdout. It reads the marker from
// a pipe so a child that never speaks fails the test instead of hanging it forever.
func readUntil(stdout io.Reader, marker string) (string, error) {
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		var seen strings.Builder
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				seen.Write(buf[:n])
				if strings.Contains(seen.String(), marker) {
					done <- seen.String()
					return
				}
			}
			if readErr != nil {
				done <- seen.String()
				return
			}
		}
	}()
	select {
	case out := <-done:
		if !strings.Contains(out, marker) {
			return out, fmt.Errorf("child exited without printing %q", marker)
		}
		return out, nil
	case <-time.After(splHoldBudget):
		return "", fmt.Errorf("child did not print %q within %v", marker, splHoldBudget)
	}
}
