package tools

// Tests for the ONE exported liveness predicate over a task row's recorded
// process identity (epic task-process-identity-convergence).
//
// The four arms the epic names, and the distinction Rule 15b demands:
//
//	live row                    → live=true,  running,    measured=true
//	exited process              → live=false, dead,       measured=true
//	recycled pid (forged start) → live=false, recycled,   measured=true
//	no recorded identity        → live=false, NOT-MEASURED, measured=false
//
// The last arm is the point: "the row records nothing I can measure" must NOT
// be reported as the false that downstream code reads as "dead". The tests
// assert the two are different values, not merely different prose.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// livenessSpawnLiveChild starts a real child process whose identity can be
// captured while it lives (the fixture then kills and reaps it to produce the
// "exited" arm). It returns the pid and the kill+reap func.
func livenessSpawnLiveChild(t *testing.T) (int, func()) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	// Pdeathsig + a neutral cwd: the subject is this child's IDENTITY, never
	// where it stands, and a SIGKILLed test binary runs no cleanup to kill it.
	cmd.Dir = t.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a probe process: %v", err)
	}
	pid := cmd.Process.Pid
	kill := func() {
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		_ = cmd.Wait()         //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion// reap: a zombie would be pidAlive=false anyway, but the arm must be clean
	}
	t.Cleanup(kill)
	return pid, kill
}

// livenessForgeStartTicks rewrites the row's recorded start tick — the shape
// of a pid that the OS handed to a DIFFERENT process (a recycled number).
// Forging it is honest: a genuine wrap is not constructible on demand, and
// the ladder's whole job is the comparison it drives.
func livenessForgeStartTicks(t *testing.T, taskID string, delta int64) {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	row, err := taskByID(ctx, db, taskID)
	if err != nil || row == nil {
		t.Fatalf("re-read %s: %v", taskID, err)
	}
	if !row.HasProcIdentity || row.ProcStartTicks <= 0 {
		t.Skipf("row %s recorded no start tick — the fixture needs one to forge", taskID)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET `+tasksProcStartColumnName+` = ? WHERE task_id = ?`,
		row.ProcStartTicks+delta, taskID); err != nil {
		t.Fatalf("forge start ticks: %v", err)
	}
}

// TestTaskProcessLivenessLiveRow: arm 1 — the row recorded the identity of a
// process that is running, so the predicate answers live.
func TestTaskProcessLivenessLiveRow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	row := procIdentitySeedRow(t, "t-live-running", hostPid(t, os.Getpid()))
	if !row.HasProcIdentity {
		t.Fatalf("fixture precondition: the row must carry a recorded identity")
	}
	live, verdict, measured, reason, err := TaskProcessLiveness("t-live-running")
	if err != nil {
		t.Fatalf("liveness: %v", err)
	}
	if !live || verdict != TaskProcessStateRunning || !measured {
		t.Fatalf("live=%v verdict=%q measured=%v, want true/%s/true (reason: %s)",
			live, verdict, measured, TaskProcessStateRunning, reason)
	}
	if strings.TrimSpace(reason) == "" {
		t.Errorf("a measured verdict must carry its evidence line")
	}
}

// TestTaskProcessLivenessExitedProcessIsGone: arm 2 — the recorded process
// has exited, so the predicate answers the MEASURED death (false + gone +
// measured=true, i.e. the opposite of the not-measured arm below).
func TestTaskProcessLivenessExitedProcessIsGone(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	pid, kill := livenessSpawnLiveChild(t)
	row := procIdentitySeedRow(t, "t-live-exited", hostPid(t, pid))
	if !row.HasProcIdentity {
		t.Skipf("the child's identity could not be captured (pid %d raced us) — the arm needs a recorded identity", pid)
	}
	kill()
	if pidAlive(pid) {
		t.Skipf("pid %d still reports alive after kill+reap", pid)
	}
	live, verdict, measured, reason, err := TaskProcessLiveness("t-live-exited")
	if err != nil {
		t.Fatalf("liveness: %v", err)
	}
	if live || verdict != TaskProcessStateDead || !measured {
		t.Fatalf("live=%v verdict=%q measured=%v, want false/%s/true (reason: %s)",
			live, verdict, measured, TaskProcessStateDead, reason)
	}
}

// TestTaskProcessLivenessRecycledPidIsNotTheRunner: arm 3 — the pid is alive
// but is not the recorded incarnation (forged start tick), so the answer is
// the MEASURED "reused", never "running" (the false-live this epic removes).
func TestTaskProcessLivenessRecycledPidIsNotTheRunner(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	procIdentitySeedRow(t, "t-live-recycled", hostPid(t, os.Getpid()))
	livenessForgeStartTicks(t, "t-live-recycled", 1)
	live, verdict, measured, reason, err := TaskProcessLiveness("t-live-recycled")
	if err != nil {
		t.Fatalf("liveness: %v", err)
	}
	if live || verdict != TaskProcessStateRecycled || !measured {
		t.Fatalf("live=%v verdict=%q measured=%v, want false/%s/true (reason: %s)",
			live, verdict, measured, TaskProcessStateRecycled, reason)
	}
}

// TestTaskProcessLivenessWithoutIdentityIsNotMeasured: arm 4 — the row
// recorded NOTHING to measure. The answer is an explicit not-measured, and
// the test pins the difference from the death verdict: a caller that cannot
// measure must never be handed the false that reads as "dead" (Rule 15b).
func TestTaskProcessLivenessWithoutIdentityIsNotMeasured(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "no identity fixture", Payload: "nothing was ever stamped", TaskID: "t-live-unmeasured", Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	live, verdict, measured, reason, err := TaskProcessLiveness("t-live-unmeasured")
	if err != nil {
		t.Fatalf("liveness: %v", err)
	}
	if verdict != TaskProcessStateNotMeasured || measured {
		t.Fatalf("verdict=%q measured=%v, want %q/false — no recorded identity is NO EVIDENCE, not a death",
			verdict, measured, TaskProcessStateNotMeasured)
	}
	if live {
		t.Errorf("an unmeasurable row cannot be live")
	}
	if verdict == TaskProcessStateDead {
		t.Fatalf("the not-measured answer collapsed into %q — the silent-false-as-dead failure this epic forbids",
			TaskProcessStateDead)
	}
	if strings.TrimSpace(reason) == "" {
		t.Errorf("the not-measured answer must say WHAT is missing")
	}
	// The row-image form (no DB round trip) answers the same way — the
	// renderer and the DB reader read one ladder.
	if v := taskRowProcessLiveness(&Task{}); v.State != TaskProcessStateNotMeasured || v.measured() {
		t.Errorf("row-image form = %q measured=%v, want %q/false",
			v.State, v.measured(), TaskProcessStateNotMeasured)
	}
	// An absent row is an ERROR, never a quiet verdict (Rule 15).
	if _, _, _, _, err := TaskProcessLiveness("t-does-not-exist"); err == nil {
		t.Errorf("an absent row must be an error, not a fabricated verdict")
	}
}

// TestTaskProcessLivenessDistinguishesNotMeasuredFromGone pins the two
// answers side by side on the SAME host, which is what makes the distinction
// observable rather than a claim: one row measured as gone, one row with
// nothing to measure — different verdicts, different measured flags.
func TestTaskProcessLivenessDistinguishesNotMeasuredFromGone(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	_, kill := livenessSpawnLiveChild(t)
	pid := spawnDeadProcess(t) // a pid that is provably gone
	_ = kill
	procIdentitySeedRow(t, "t-side-gone", hostPid(t, os.Getpid()))
	procIdentitySeedRow(t, "t-side-nothing", hostPid(t, pid))

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "unmeasured side", Payload: "p", TaskID: "t-side-unmeasured", Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// Forge the gone row's identity onto the dead pid (the capture path
	// records nothing for a pid that is already gone, so the record is
	// written directly — the exact shape of a row whose runner died AFTER
	// stamping itself).
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET `+tasksProcPidColumnName+` = ?, `+tasksProcStartColumnName+` = 42, `+tasksProcCmdColumnName+` = ? WHERE task_id = ?`,
		pid, "/usr/bin/true", "t-side-gone"); err != nil {
		t.Fatalf("forge identity: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	goneLive, goneVerdict, goneMeasured, _, err := TaskProcessLiveness("t-side-gone")
	if err != nil {
		t.Fatalf("liveness(gone): %v", err)
	}
	noneLive, noneVerdict, noneMeasured, _, err := TaskProcessLiveness("t-side-unmeasured")
	if err != nil {
		t.Fatalf("liveness(unmeasured): %v", err)
	}
	if goneLive || goneVerdict != TaskProcessStateDead || !goneMeasured {
		t.Fatalf("gone row: live=%v verdict=%q measured=%v, want false/%s/true", goneLive, goneVerdict, goneMeasured, TaskProcessStateDead)
	}
	if noneVerdict == goneVerdict || noneMeasured {
		t.Fatalf("unmeasured row: verdict=%q measured=%v — it must differ from the gone row (%q)",
			noneVerdict, noneMeasured, goneVerdict)
	}
	if noneVerdict != TaskProcessStateNotMeasured {
		t.Fatalf("unmeasured row: verdict=%q, want %q", noneVerdict, TaskProcessStateNotMeasured)
	}
	_ = noneLive
}

// TestTaskProcessLivenessRoutesTheDisplayLine: the `task show` process line is
// one of the call sites routed through the predicate (taskproc.go
// taskShowIdentityLine). Before the route it re-derived the ladder INLINE, and
// its first rung was the coarse comm-name guard: a row whose (pid, starttime)
// MATCHED a live process was still rendered "alive-but-recycled" whenever that
// process's comm was not gopi* — i.e. a live, verified incarnation reported as
// "the runner is dead". The ladder's documented precedence (matching ticks =
// the same incarnation, name guard only as a substitute when nothing was
// recorded) now reaches the display through the ONE predicate, so the line and
// the ledger cannot disagree about a row.
//
// This test is the permanent guard; the red-then-green observation for the
// route itself (old inline ladder → red, routed → green) is recorded in the
// task report and the run's evidence directory.
func TestTaskProcessLivenessRoutesTheDisplayLine(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	row := procIdentitySeedRow(t, "t-live-display", hostPid(t, os.Getpid()))
	if !row.HasProcIdentity || row.ProcStartTicks <= 0 {
		t.Skipf("the fixture needs a full identity triple on the row: %+v", row)
	}
	out, err := TaskShow("t-live-display")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(out, "(live: "+taskShowLiveAlive+")") {
		t.Fatalf("a row whose recorded identity IS this live process must render %q, got:\n%s",
			taskShowLiveAlive, out)
	}
	if strings.Contains(out, "(live: "+taskShowLiveGone+")") {
		t.Fatalf("a live incarnation must never render as %q:\n%s", taskShowLiveGone, out)
	}
}
