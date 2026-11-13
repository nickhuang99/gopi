package main

// The seam's tests, in two arms:
//
//   - a SOURCE-SHAPE arm pinning the two properties a refactor could silently
//     lose: the run funnel takes the lock BEFORE the singleton guard and the
//     claim CAS (so a refused run writes nothing), and the worker round takes it
//     BEFORE it enters the run directory (so the move is guarded, not just the
//     spawn);
//   - a REAL BINARY arm that drives the shipped `gopi` with a lock already held
//     by another PROCESS, so the refusal is measured, not asserted from a
//     paraphrase: non-zero exit, the lock path named, the holder's pid named.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/runlock"
)

// seamTaskID is a canonical task-id spelling (the shape tools.TaskSessionDir
// turns into a path element), so the seam resolves a directory instead of
// refusing the id.
const seamTaskID = "01a1a88b-41e5-77dd-aa29-99465db1c069"

// seamFuncBody returns the text of one top-level function from a source file —
// the ONE way these tests read a function's shape (a paraphrase would pass
// while the file said something else).
func seamFuncBody(t *testing.T, path, signature string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(raw)
	start := strings.Index(text, signature)
	if start < 0 {
		t.Fatalf("%s does not contain %q — the seam this test pins is gone", path, signature)
	}
	body := text[start:]
	if end := strings.Index(body, "\n}\n"); end >= 0 {
		return body[:end]
	}
	return body
}

// TestRunFunnelTakesTheLockBeforeTheClaim pins the ORDER inside runTaskRunOnce:
// the lock precedes the singleton guard, the claim CAS and the workdir move.
// The order is the whole point — a lock taken after the claim would leave a
// claimed row behind on every refusal, which is the stale-row shape the epic
// exists to remove.
func TestRunFunnelTakesTheLockBeforeTheClaim(t *testing.T) {
	body := seamFuncBody(t, "cli_tasks.go", "func runTaskRunOnce(")
	lockAt := strings.Index(body, "taskRunTakeLock(target)")
	if lockAt < 0 {
		t.Fatalf("runTaskRunOnce does not take the run lock at all")
	}
	for _, later := range []string{
		"TaskRunGuardFor(",
		"TaskClaimExplicitForce(",
		"TaskResumeClaimed(",
		"taskWorkdirEnter(",
	} {
		at := strings.Index(body, later)
		if at < 0 {
			continue // the arm may move; the ones present must still be after
		}
		if at < lockAt {
			t.Errorf("%s appears BEFORE the run lock in runTaskRunOnce — a refused run would then leave a claimed/written ledger row behind", later)
		}
	}
	if !strings.Contains(body, "defer func() { runLockRelease() }()") {
		t.Errorf("runTaskRunOnce does not defer the lock release — a held descriptor would outlive the run")
	}
}

// TestWorkerRoundLocksBeforeEnteringTheDirectory pins the worker arm: the lock
// is taken BEFORE taskWorkdirEnter (the move that makes the run directory the
// round's own), and a refusal rolls the claim back instead of leaving it.
func TestWorkerRoundLocksBeforeEnteringTheDirectory(t *testing.T) {
	body := seamFuncBody(t, "cli_tasks_worker.go", "func workerRoundRun(")
	lockAt := strings.Index(body, "taskRunTakeLock(target)")
	if lockAt < 0 {
		t.Fatalf("workerRoundRun does not take the run lock")
	}
	enterAt := strings.Index(body, "taskWorkdirEnter(target)")
	if enterAt < 0 {
		t.Fatalf("workerRoundRun no longer enters the run directory — this test's subject moved")
	}
	if lockAt > enterAt {
		t.Errorf("the worker takes the run lock AFTER taskWorkdirEnter — the move into the run directory would be unguarded")
	}
	refusal := body[lockAt:]
	if !strings.Contains(refusal, "RevertClaimUnattributed(target)") {
		t.Errorf("the worker's lock refusal does not roll the claim back — a claimed row that will not run is the stale-row defect")
	}
	if !strings.Contains(refusal, "defer relRunLock()") {
		t.Errorf("the worker does not defer the release — round 2 would inherit round 1's held directory")
	}
}

// TestLockHandOffIsNotReentrancy pins the ONE process-wide fact the two seams
// share: a funnel that finds the directory already held by THIS process gets a
// no-op release instead of a second flock (flock is per open file description,
// so a second open in the same process would refuse itself — a self-deadlock).
func TestLockHandOffIsNotReentrancy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.SessionsRootEnv, dir)

	rel, err := taskRunTakeLock(seamTaskID)
	if err != nil {
		t.Fatalf("first taskRunTakeLock: %v", err)
	}
	heldDir := runLockHeldDir
	if heldDir == "" {
		t.Fatalf("the lock was taken but runLockHeldDir is empty — the hand-off cannot work")
	}
	// A second attempt in the SAME process must be the hand-off, not a refusal.
	rel2, err := taskRunTakeLock(seamTaskID)
	if err != nil {
		rel()
		t.Fatalf("second taskRunTakeLock in the same process = %v, want the hand-off (a self-refusal would deadlock every worker round)", err)
	}
	rel2() // releasing the hand-off must NOT free the directory
	if runLockHeldDir != heldDir {
		t.Errorf("the hand-off release cleared the held directory — the outer holder lost its ownership")
	}
	// And a DIFFERENT process would still be refused: prove it with a real one.
	exe, xerr := os.Executable()
	if xerr != nil {
		rel()
		t.Fatalf("os.Executable: %v", xerr)
	}
	// -test.v because the probe reports through t.Logf, and a PASSING
	// test's log is suppressed by default: without the flag the arm would
	// read an empty output and call a correct refusal a defect.
	probe := exec.Command(exe, "-test.v", "-test.run=TestZZNoSuchTestRunLockProbe")
	probe.Env = append(os.Environ(),
		runlockProbeEnv+"="+heldDir,
	)
	// The probe's exit status is NOT the assertion: a probe that acquired the
	// lock fails via t.Fatalf (which the parent reads as output), and the parent
	// judges the text. An error here means the probe died, which the same text
	// check catches.
	out, _ := probe.CombinedOutput() //stylegate:ignore the probe reports through its output; its exit code is read from the text assertion below
	rel()
	if !strings.Contains(string(out), runlockProbeRefused) {
		t.Fatalf("a genuinely separate process was NOT refused while this one held the lock (output %q) — the lock is not cross-process", string(out))
	}
}

// The probe: cmd/agent's TestMain re-execs this binary; a helper process needs a
// mode of its own. It is a variable + an init-free switch so the probe costs one
// process and no test state.
const (
	runlockProbeEnv     = "RUNLOCKSEAM_PROBE_DIR"
	runlockProbeRefused = "PROBE-REFUSED"
)

// TestZZNoSuchTestRunLockProbe is never run as a test: it is the probe body the
// arm above launches (its name is matched by -test.run). When the env is set the
// process tries the lock and prints PROBE-REFUSED on a refusal.
func TestZZNoSuchTestRunLockProbe(t *testing.T) {
	dir := os.Getenv(runlockProbeEnv)
	if dir == "" {
		t.Skip("probe body: only meaningful when launched with " + runlockProbeEnv)
	}
	h, err := runlock.Lock(dir)
	if err != nil {
		t.Logf("%s %v", runlockProbeRefused, err)
		return
	}
	_ = h.Release() //stylegate:ignore teardown right before an unconditional failure
	t.Fatalf("the probe ACQUIRED %q while another process held it", filepath.Join(dir, runlock.LockFileName))
}

// TestSuccessfulLockAnnouncementSaysTheHolderIsThisProcess pins the ONE line an
// operator reads while a run executes. Its predecessor read
//
//	gopi worker: run lock held: /…/run.lock (pid 4242)
//
// which has the SAME SHAPE as runlock.ErrHeld's refusal ("run directory is
// already locked by another process … (held by pid …)"), so the success line
// was read as "somebody else holds this directory" while the pid printed beside
// it is the caller's own — the misreading this arm exists to prevent.
//
// It asserts the TEXT, and the text only: a phrase is not reachable through any
// behaviour (PROJECT.md Rule 31), and the sibling assertions in this file
// already cover the lock's effect.
func TestSuccessfulLockAnnouncementSaysTheHolderIsThisProcess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.SessionsRootEnv, dir)

	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatalf("pipe: %v", perr)
	}
	// os.Stderr is the *os.File global the announcement writes to; the env
	// helper does not cover it, hence the explicit swap-and-restore pair.
	prev := os.Stderr
	os.Stderr = w

	rel, err := taskRunTakeLock(seamTaskID)
	os.Stderr = prev
	if cerr := w.Close(); cerr != nil {
		t.Fatalf("close the capture pipe: %v", cerr)
	}
	captured, rerr := io.ReadAll(r)
	if cerr := r.Close(); cerr != nil {
		t.Fatalf("close the capture reader: %v", cerr)
	}
	if rerr != nil {
		t.Fatalf("read the captured stderr: %v", rerr)
	}
	if err != nil {
		t.Fatalf("taskRunTakeLock(%s) = %v, want the lock", seamTaskID, err)
	}
	defer rel()

	line := string(captured)
	for _, want := range []string{
		"run lock held by THIS process",
		fmt.Sprintf("(pid %d)", os.Getpid()),
		"no other process is executing this directory",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the success announcement does not carry %q — the holder's identity stays misreadable\n  got: %q", want, line)
		}
	}
	// The refusal's own vocabulary must NOT appear: "another process" in a
	// SUCCESS line is precisely the contradiction this test removes.
	if strings.Contains(line, "another process") {
		t.Errorf("the success announcement uses the refusal's wording (\"another process\")\n  got: %q", line)
	}
	if !strings.Contains(line, runlock.LockFileName) {
		t.Errorf("the announcement no longer names the lock file — an operator cannot stat what it prints\n  got: %q", line)
	}
}
