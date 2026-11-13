package main

// cli_tasks_exec_output_test.go — the spawn seam's own contract, and the
// REGRESSION GUARD for the defect the epic removes.
//
// THE DEFECT (child-stream-task-output, 2026-09-17): runTaskRunOnce set
// `cmd.Stdout = os.Stdout`, so where the executor child's text landed depended
// on WHO spawned it — the operator's terminal for `task run`, the launcher's
// logs/worker-startup.log for a worker round's pre-claim window, the task's
// session dir only after the worker had re-pointed its own os.Stdout at CLAIM
// time, or the service log for a serve tick. Three spawn paths, three
// destinations, one of them reachable only through a parent-side re-point.
//
// The fix puts the destination on the CHILD at the ONE seam every path shares,
// derived from the ROW (tools.TaskLogPath → <session-dir>/output.log). These
// tests pin exactly that: the file is opened where `task show` looks for it,
// the same call resolves it for every caller, and a SECOND run of the same row
// cannot truncate the first run's evidence.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// execOutputSandbox points the gopi profile at a temp dir so the seam resolves
// (and creates) inside the test's own tree — never the developer's ~/.gopi.
func execOutputSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	return filepath.Join(home, config.ProfileDirName)
}

// TestExecutorChildOutputLogLivesWhereTaskShowLooks is the acceptance
// assertion of the epic, in ONE comparison: the file the seam opens must be
// the file `task show` renders for the same task id. The two are resolved by
// DIFFERENT code paths on purpose (the seam asks for the write path, the
// renderer globs what is on disk), so agreement here is real agreement and not
// a mirrored literal.
func TestExecutorChildOutputLogLivesWhereTaskShowLooks(t *testing.T) {
	execOutputSandbox(t)
	const taskID = "t-exec-output-seam"

	f, err := taskExecutorOutputLog(taskID)
	if err != nil {
		t.Fatalf("the seam must open the row's output log: %v", err)
	}
	if _, err := f.WriteString("the executor child's own text\n"); err != nil {
		t.Fatalf("write to the run log: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close the run log: %v", err)
	}
	seamPath := taskExecutorOutputLogPath
	if seamPath == "" {
		t.Fatalf("the seam must LATCH the path it opened (the writeback stamp and this assertion both read it)")
	}

	glob, err := tools.TaskLogGlob(taskID)
	if err != nil {
		t.Fatalf("task log glob: %v", err)
	}
	paths, err := filepath.Glob(glob)
	if err != nil {
		t.Fatalf("glob %s: %v", glob, err)
	}
	if len(paths) != 1 || paths[0] != seamPath {
		t.Fatalf("`task show`'s glob %s found %v, want exactly the seam's path %s — a task id must lead to the run's output with no other record",
			glob, paths, seamPath)
	}
	if want := tools.WorkerLogFileName; filepath.Base(seamPath) != want {
		t.Errorf("a row's FIRST run must own the fixed name %q; got %s", want, filepath.Base(seamPath))
	}
	body, err := os.ReadFile(seamPath)
	if err != nil {
		t.Fatalf("read the run log: %v", err)
	}
	if !strings.Contains(string(body), "the executor child's own text") {
		t.Errorf("the run log must hold what was written to it; got:\n%s", body)
	}
	// The header identifies the file when it is opened directly: a reader who
	// finds it on disk (not through `task show`) must be able to tell whose
	// run it records.
	if !strings.Contains(string(body), taskID) {
		t.Errorf("the log must name its own task (%s) in its header; got:\n%s", taskID, body)
	}
}

// TestExecutorOutputLogAppendsOnReRun is the LOSS-PREVENTION half: a row that
// is re-claimed after its first run died must not have its predecessor's
// evidence truncated. tools.TaskLogPath gives the first run the fixed name and
// every later run a pid-suffixed one, so the two runs are two FILES — this
// test pins that the seam honours the resolver (append, never truncate) rather
// than opening the fixed name a second time.
func TestExecutorOutputLogAppendsOnReRun(t *testing.T) {
	execOutputSandbox(t)
	const taskID = "t-exec-output-rerun"

	first, err := taskExecutorOutputLog(taskID)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstPath := taskExecutorOutputLogPath
	if _, err := first.WriteString("RUN-ONE-EVIDENCE\n"); err != nil {
		t.Fatalf("write run one: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close run one: %v", err)
	}

	// The second run of the SAME row: os.Getpid() is unchanged, so the
	// resolver must fall back to the pid-suffixed name.
	second, err := taskExecutorOutputLog(taskID)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	secondPath := taskExecutorOutputLogPath
	if _, err := second.WriteString("RUN-TWO-EVIDENCE\n"); err != nil {
		t.Fatalf("write run two: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close run two: %v", err)
	}

	if secondPath == firstPath {
		t.Fatalf("the second run of %s reopened %s — a re-claimed row would truncate its predecessor's evidence",
			taskID, firstPath)
	}
	one, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("run one's log must survive: %v", err)
	}
	if !strings.Contains(string(one), "RUN-ONE-EVIDENCE") || strings.Contains(string(one), "RUN-TWO-EVIDENCE") {
		t.Errorf("run one's log was written by the second run; got:\n%s", one)
	}
	two, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatalf("run two's log: %v", err)
	}
	if !strings.Contains(string(two), "RUN-TWO-EVIDENCE") {
		t.Errorf("run two's log must hold run two's text; got:\n%s", two)
	}
	glob, err := tools.TaskLogGlob(taskID)
	if err != nil {
		t.Fatalf("task log glob: %v", err)
	}
	paths, err := filepath.Glob(glob)
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("one glob must reach BOTH runs of the row; found %v", paths)
	}
}

// TestExecutorOutputLogReportsAnUnwritableDestination is the Rule 15 half: a
// run whose output cannot be attributed must SAY so, and the seam must hand
// the caller a non-nil error rather than a nil file it would dereference. The
// caller (runTaskRunOnce) then keeps the child on the spawning stream, which
// is why the contract is "error + no file", not "refuse the run".
func TestExecutorOutputLogReportsAnUnwritableDestination(t *testing.T) {
	gd := execOutputSandbox(t)
	// Occlude the sessions tree with a FILE: MkdirAll then fails with ENOTDIR.
	// The gopi dir itself must exist first, or the occlusion would be measuring
	// the profile's absence rather than the containment (the test would pass
	// for the wrong reason).
	if err := os.MkdirAll(gd, 0o755); err != nil {
		t.Fatalf("create the profile root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gd, config.SessionsDirName), []byte("not a dir\n"), 0o644); err != nil {
		t.Fatalf("occlude the sessions root: %v", err)
	}
	f, err := taskExecutorOutputLog("t-exec-output-unwritable")
	if err == nil {
		if f != nil {
			f.Close() //stylegate:ignore close of the file the seam should never have returned; the t.Fatalf below is the assertion
		}
		t.Fatalf("an unusable destination must be reported, not silently accepted")
	}
	if f != nil {
		t.Fatalf("the seam must return no file when it cannot open one; got %v", f)
	}
	if !strings.Contains(err.Error(), config.SessionsDirName) {
		t.Errorf("the error must name the path that failed: %v", err)
	}
}
