package main

// task_session_cwd_test.go — the epic task-session-cwd contract, machine-
// checked at the seam that implements it:
//
//	(a) a task-OWNING process (the worker round / the `task run` wrapper)
//	    STANDS in the claimed task's session directory;
//	(b) the EXECUTOR child is handed the start workdir instead (the wrapper's
//	    move is not inherited);
//	(c) a container that cannot be entered is a LOUD refusal — the caller
//	    refuses the round rather than running with its output outside the
//	    task's own directory (Rule 15);
//	(d) the move is REVERSIBLE for a long-lived process (the worker loop):
//	    after the round the process is back where it started, so round 2 does
//	    not inherit round 1's directory.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// cwdTestSandbox points GOPI_HOME at a fresh profile and returns the resolved
// gopi dir. The caller's real cwd is restored by t.Chdir.
func cwdTestSandbox(t *testing.T) string {
	t.Helper()
	// The workdir latches are PROCESS state (one capture per process is
	// exactly right in production, where one process serves one task): a
	// fresh sandbox means a fresh latch.
	taskWorkdirResetTest()
	t.Cleanup(taskWorkdirResetTest)
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("gopi dir: %v", err)
	}
	return gd
}

// TestTaskWorkdirEnterStandsInTheSessionDir is (a)+(b)+(d): the round's
// process moves into <gopi-dir>/sessions/<task-id>/, the executor child is
// told to run in the START workdir instead, and the round's end puts the
// process back.
func TestTaskWorkdirEnterStandsInTheSessionDir(t *testing.T) {
	gd := cwdTestSandbox(t)
	start := t.TempDir()
	t.Chdir(start)

	const taskID = "t-session-cwd-pos"
	line, err := taskWorkdirEnter(taskID)
	if err != nil {
		t.Fatalf("entering the session dir must succeed on a writable profile: %v", err)
	}
	wantDir := filepath.Join(gd, config.SessionsDirName, taskID)
	if wd, _ := os.Getwd(); wd != wantDir { //stylegate:ignore the preceding t.Fatalf already failed the test if the cwd is unreadable; an unreadable cwd here can only make the comparison honestly false
		t.Fatalf("the round's process must STAND in %s; cwd=%s", wantDir, wd)
	}
	if !strings.Contains(line, wantDir) || !strings.Contains(line, start) {
		t.Errorf("the move must be reported with both the destination and the origin; got %q", line)
	}
	// (b) the executor child keeps the operator's working context: the
	// wrapper's move is handed over as DATA, not inherited as cwd.
	if got := taskExecutorWorkdir(); got != start {
		t.Errorf("the executor child must be handed the start workdir %s; got %q", start, got)
	}
	// (d) the round's end returns a long-lived process to where it started.
	taskWorkdirRestore()
	if wd, _ := os.Getwd(); wd != start { //stylegate:ignore a second Getwd read of the same process state; its error is unreachable once the first read succeeded
		t.Fatalf("after the round the process must be back in %s; cwd=%s", start, wd)
	}
}

// TestTaskWorkdirEnterIsIdempotent: the worker round enters the container and
// then runTaskRunOnce enters it again for the same row. The second call must
// be a no-op — and, crucially, must NOT rewrite the recorded start workdir to
// the container (which would leave the executor child running inside the
// session dir and the round-end restore a no-op).
func TestTaskWorkdirEnterIsIdempotent(t *testing.T) {
	_ = cwdTestSandbox(t)
	start := t.TempDir()
	t.Chdir(start)

	const taskID = "t-session-cwd-twice"
	if _, err := taskWorkdirEnter(taskID); err != nil {
		t.Fatalf("first enter: %v", err)
	}
	if _, err := taskWorkdirEnter(taskID); err != nil {
		t.Fatalf("second enter must be accepted (idempotent): %v", err)
	}
	if got := taskExecutorWorkdir(); got != start {
		t.Errorf("re-entering must not re-point the executor workdir; got %q want %q", got, start)
	}
	taskWorkdirRestore()
	if wd, _ := os.Getwd(); wd != start { //stylegate:ignore the cwd was already read successfully in this test; a failure here would be a different bug than the one under test
		t.Errorf("restore must still land in %s; cwd=%s", start, wd)
	}
}

// TestTaskWorkdirEnterRefusesUnusableContainer is (c): with the sessions tree
// unusable (a FILE where the root must be), the enter returns an error naming
// the path, the process does NOT move, and no fallback directory is created —
// the refusal is the whole point (a task whose container cannot be entered is
// a task whose output nobody can find).
func TestTaskWorkdirEnterRefusesUnusableContainer(t *testing.T) {
	gd := cwdTestSandbox(t)
	start := t.TempDir()
	t.Chdir(start)

	if err := os.MkdirAll(gd, 0o755); err != nil {
		t.Fatalf("mkdir gopi dir: %v", err)
	}
	root := filepath.Join(gd, config.SessionsDirName)
	if err := os.WriteFile(root, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("occlude sessions root: %v", err)
	}

	const taskID = "t-session-cwd-neg"
	if _, err := taskWorkdirEnter(taskID); err == nil {
		t.Fatal("entering an unusable container must FAIL, never silently fall back")
	}
	if wd, _ := os.Getwd(); wd != start { //stylegate:ignore the assertion is that the process did NOT move; an unreadable cwd would fail the comparison, which is the conservative direction for a refusal test
		t.Errorf("a refused enter must leave the process where it was; cwd=%s want %s", wd, start)
	}
	msg := taskWorkdirChdirRefuse(taskID, os.ErrPermission)
	for _, want := range []string{taskID, "refusing to run"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal must name %q; got %q", want, msg)
		}
	}
}

// TestTaskWorkdirRestoreSkipsAGoneRoundOrigin is the regression test for the
// measured bug: a round's restore target is where the round started, and that
// directory can be GONE by the time the round ends (a producer releasing its
// own worktree; in a test binary, an earlier test's t.TempDir() left in the
// process-wide override latch and already reclaimed).
//
// The measured consequence was not a cosmetic one: the process was left
// standing somewhere that no longer has a name, so every later operation in
// that process failed with "getwd: no such file or directory" — in the test
// binary, every following test in the package (24 of them, all of which read
// fixtures through a relative path).
//
// The contract this pins: an enter/restore cycle is an UNDO — the process is
// back where it started, and it never moves to a directory that is not there.
func TestTaskWorkdirRestoreSkipsAGoneRoundOrigin(t *testing.T) {
	_ = cwdTestSandbox(t)
	start := t.TempDir()
	t.Chdir(start)
	gone := t.TempDir()
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("remove the would-be origin: %v", err)
	}
	// A stale override pointing at a directory that no longer exists — the
	// exact state an in-process caller leaves behind (resumeChdirToWorktree
	// latches the row's worktree, which its owner then releases).
	taskWorkdirOverride = gone

	if _, err := taskWorkdirEnter("t-session-cwd-gone-origin"); err != nil {
		t.Fatalf("enter: %v", err)
	}
	taskWorkdirRestore()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("after the restore the process must have a readable cwd (this is the bug: %v)", err)
	}
	if wd != start {
		t.Fatalf("the round must be undone back to %s; the process instead ended in %s", start, wd)
	}
}

// TestTaskWorkdirAndOutputLogShareTheOneSessionsRoot: the chdir and the
// executor output-log path must resolve against the SAME tree (Rule 2/14: one
// spelling of the sessions root). The explicit-root knob is retired (epic
// child-stream-task-output), so the property is now "both agree with the
// profile's own <gopi-dir>/sessions" — a second root would put a round's cwd
// and its task's output in different directories.
func TestTaskWorkdirAndOutputLogShareTheOneSessionsRoot(t *testing.T) {
	gd := cwdTestSandbox(t)
	start := t.TempDir()
	t.Chdir(start)

	const taskID = "t-session-cwd-root"
	dir, err := tools.ChdirTaskSessionDir(taskID)
	if err != nil {
		t.Fatalf("chdir into the task container: %v", err)
	}
	if want := filepath.Join(gd, config.SessionsDirName, taskID); dir != want {
		t.Fatalf("the chdir dir must be the profile's sessions tree: dir=%s want %s", dir, want)
	}
	logPath, err := tools.TaskLogPath(taskID, os.Getpid())
	if err != nil {
		t.Fatalf("output-log path: %v", err)
	}
	if got := filepath.Dir(logPath); got != dir {
		t.Fatalf("the task's output log and its workdir must share one directory: log=%s workdir=%s", got, dir)
	}
	if wd, _ := os.Getwd(); wd != dir { //stylegate:ignore ChdirTaskSessionDir just reported the dir it moved to; a cwd read failure cannot make this assertion pass vacuously
		t.Errorf("cwd must follow the reported dir; cwd=%s dir=%s", wd, dir)
	}
	taskWorkdirRestore()
}
