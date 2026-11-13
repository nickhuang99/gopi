// Process-identity tests (epic task-process-identity, 2026-09-13).
//
// The epic's core claim is that a task row's in_progress status alone cannot
// tell a live run from a worker killed by SIGKILL/Ctrl-C, and that the fix is
// to record the executing process's IDENTITY — pid + process start time +
// command line — so the ledger can be checked against the live OS process.
//
// The tests below pin the three properties that make that work:
//
//	① the capture: a row entering in_progress RECORDS the three factors of
//	   the process named as holder, in the same UPDATE as the transition;
//	② the verdict: all three factors must agree for "running" — a pid that
//	   was REUSED by an unrelated process is reported recycled, never running
//	   (the false-live this epic exists to remove);
//	③ the consumers: `task show` renders the record, `task status` answers
//	   with the definite verdict, and the resume gate refuses a live holder
//	   while reclaiming a dead one.
//
// Everything is deterministic: the ledger is a sandboxed sqlite file, "dead"
// processes are real spawned-then-reaped children (their pid is then free for
// the OS to reuse — which is exactly what the recycled test needs), and the
// one seam (taskProcIdentityRead) can be pointed at a synthetic identity so
// the mismatching-start-time branch is reachable without waiting for a real
// pid wrap.
package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// procIdentitySeedRow submits a row and drives it to in_progress through the
// REAL start transition, so the identity capture runs exactly as in
// production. Raw SQL is used only where a test needs to FORGE a shape the
// API can no longer produce (a legacy row with no identity).
func procIdentitySeedRow(t *testing.T, taskID, holder string) *Task {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "identity fixture " + taskID,
		Payload: "process-identity fixture",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	if err := TaskClaimExplicit(taskID, holder); err != nil {
		t.Fatalf("claim %s: %v", taskID, err)
	}
	if _, err := TaskStart(taskID, holder); err != nil {
		t.Fatalf("start %s: %v", taskID, err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	row, err := taskByID(ctx, db, taskID)
	if err != nil || row == nil {
		t.Fatalf("re-read %s: %v", taskID, err)
	}
	return row
}

// TestTaskStartRecordsProcessIdentity is property ①: the row that entered
// in_progress carries the three factors of THIS process — the pid, a
// non-zero start tick count, and the command line that names the test binary.
// Before this epic the row carried a pid only (in claimed_by), which the OS
// is free to hand to an unrelated process.
func TestTaskStartRecordsProcessIdentity(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	row := procIdentitySeedRow(t, "t-ident-capture", hostPid(t, os.Getpid()))

	if !row.HasProcIdentity {
		t.Fatalf("the in_progress transition must record the process identity (%s is NULL)",
			tasksProcPidColumnName)
	}
	if row.ProcPid != os.Getpid() {
		t.Errorf("%s = %d, want this process %d", tasksProcPidColumnName, row.ProcPid, os.Getpid())
	}
	if row.ProcStartTicks <= 0 {
		t.Errorf("%s = %d, want the process's start tick count (>0) — it is the factor that defeats pid reuse",
			tasksProcStartColumnName, row.ProcStartTicks)
	}
	if row.ProcCmd == "" || !strings.Contains(row.ProcCmd, testBinaryMarker) {
		t.Errorf("%s = %q, want the running command line (it must name the test binary %q)",
			tasksProcCmdColumnName, row.ProcCmd, testBinaryMarker)
	}
	// The recorded start must agree with a fresh read of /proc — the record
	// is an observation of the live process, not a fabricated value.
	if live, ok := procStartTicks(os.Getpid()); !ok || live != row.ProcStartTicks {
		t.Errorf("recorded start %d ≠ live /proc start %d (ok=%v)", row.ProcStartTicks, live, ok)
	}
}

// testBinaryMarker is the substring every Go test binary's argv contains —
// the assertion that the recorded command line describes a real process
// rather than an empty placeholder.
const testBinaryMarker = ".test"

// TestTaskStatusRunningForOwnRow is property ②'s positive half: a row whose
// recorded identity IS this live process reads "running" — a resume would
// double-run it and must be refused (Resumable false, the double-run guard).
// The field is asserted in the direction its message states AND cross-checked
// against the production predicate (taskVerdictResumable), so the polarity
// here cannot silently invert again.
func TestTaskStatusRunningForOwnRow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	procIdentitySeedRow(t, "t-ident-running", hostPid(t, os.Getpid()))

	st, err := TaskProcessStatusFor("t-ident-running")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Verdict != TaskProcessStateRunning {
		t.Fatalf("verdict = %q, want %q (all three factors match this live process) — note=%s",
			st.Verdict, TaskProcessStateRunning, st.Note)
	}
	// The double-run guard, asserted POSITIVELY against the field: the row's
	// holder is a verified-live process, so it must NOT be resumable. (This
	// line previously read `if !st.Resumable`, which demanded the row BE
	// resumable — the exact inverse of both the message and the guard.)
	if st.Resumable {
		t.Errorf("resumable=true for a row whose holder is verified live — a resume would double-run it (the double-run guard demands false); note=%s",
			st.Note)
	}
	// Pin the field to the production predicate that computes it, so this
	// assertion cannot be re-inverted into a pass while the guard is intact,
	// and so a guard regression cannot hide behind a re-inverted `if`.
	if taskVerdictResumable(st.Verdict) {
		t.Errorf("taskVerdictResumable(%q) = true, want false — production must refuse a resume while the holder is live",
			st.Verdict)
	}
	if !st.IdentityRecorded || st.RecordedPid != os.Getpid() {
		t.Errorf("identity_recorded=%v recorded_pid=%d, want true/%d", st.IdentityRecorded, st.RecordedPid, os.Getpid())
	}
}

// TestTaskStatusDeadForReapedChild is the epic's headline case: the worker is
// gone (killed/crashed — simulated by a spawned-then-reaped child, whose pid
// is then dead), the row still says in_progress, and the verdict says DEAD —
// resumable, so a zombie can be picked up again.
func TestTaskStatusDeadForReapedChild(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	pid := spawnDeadProcess(t)
	procIdentitySeedRow(t, "t-ident-dead", hostPid(t, pid))

	st, err := TaskProcessStatusFor("t-ident-dead")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Verdict != TaskProcessStateDead {
		t.Fatalf("verdict = %q, want %q — a gone holder pid must be DEAD (note=%s)", st.Verdict, TaskProcessStateDead, st.Note)
	}
	if !st.Resumable {
		t.Errorf("a dead holder must be resumable (this is the zombie-recovery path)")
	}
}

// spawnDeadProcess spawns a real process and reaps it, returning the pid it
// had. The pid is now free for the OS to recycle — which is exactly the
// situation the start-time factor exists for.
func spawnDeadProcess(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err) // 'true' exits 0
	}
	if pidAlive(pid) {
		t.Skipf("pid %d is still alive after the reap (pid recycling raced us)", pid)
	}
	return pid
}

// TestTaskStatusRecycledWhenStartTimeDiffers is the FALSE-LIVE regression
// guard — the reason this epic uses three factors instead of a pid. A row
// recorded pid N with the runner's start time; the OS later handed N to a
// DIFFERENT process. The name-guarded probe alone (taskPidIsRunner) cannot
// see the difference when the stranger happens to be a gopi binary too, so
// without the start-time comparison the row would read "running" forever and
// never be reclaimable.
//
// The test drives the comparison directly (taskIdentityVerdict) with an
// identity whose recorded start is off by one tick and a pid that is really
// alive — the exact shape of a reused pid, constructible without waiting for
// a wrap.
func TestTaskStatusRecycledWhenStartTimeDiffers(t *testing.T) {
	pid := os.Getpid()
	liveStart, ok := procStartTicks(pid)
	if !ok {
		t.Skip("no procfs on this host — the identity layer degrades to unknown by design")
	}
	rec := taskProcIdentity{Pid: pid, Start: liveStart + 1, HasStr: true, Cmd: procCmdline(pid)}
	if got := taskIdentityVerdict(pid, rec).State; got != TaskProcessStateRecycled {
		t.Fatalf("verdict = %q, want %q — a pid alive with a DIFFERENT start time is a reused number, never a live runner",
			got, TaskProcessStateRecycled)
	}
	// And the agreeing case must still read running (the check is a
	// comparison, not a blanket refusal).
	rec.Start = liveStart
	if got := taskIdentityVerdict(pid, rec).State; got != TaskProcessStateRunning {
		t.Fatalf("verdict = %q, want %q for a fully matching identity", got, TaskProcessStateRunning)
	}
}

// TestTaskStatusRunningWhenCmdlineDiffersUnderMatchingTicks pins the
// incarnation rule (epic task-run-singleton-guard, 2026-09-15): (pid, starttime)
// is unique per process INCARNATION, so when the recorded and live start ticks
// agree the live process IS the recorded one — even if its command line reads
// differently. That shape is not hypothetical: a shell's exec optimisation
// rewrites argv in place under the SAME pid and start tick (`/bin/bash -c
// "sleep 30"` becomes `/bin/sleep 30`; measured while building the singleton
// guard's E2E fixture). Reporting it as recycled ("the runner is dead") would
// declare a LIVE runner dead and invite a second run of its row — the very
// double-run this epic removes. The command line keeps its power only where it
// is the sole factor (see the next test).
func TestTaskStatusRunningWhenCmdlineDiffersUnderMatchingTicks(t *testing.T) {
	pid := os.Getpid()
	liveStart, ok := procStartTicks(pid)
	if !ok {
		t.Skip("no procfs on this host")
	}
	rec := taskProcIdentity{Pid: pid, Start: liveStart, HasStr: true, Cmd: "/usr/bin/somewhere-else --not-me"}
	if got := taskIdentityVerdict(pid, rec).State; got != TaskProcessStateRunning {
		t.Fatalf("verdict = %q, want %q — matching start ticks mean the SAME incarnation",
			got, TaskProcessStateRunning)
	}
}

// TestTaskStatusRecycledWhenCmdlineDiffersWithoutStartTick keeps the third
// factor's original power on the shape it was designed for: a row stamped
// WITHOUT a readable start tick (HasStr false) has only pid + command line, and
// there a differing command line is the best evidence available — a different
// program owns the number.
func TestTaskStatusRecycledWhenCmdlineDiffersWithoutStartTick(t *testing.T) {
	pid := os.Getpid()
	if !pidAlive(pid) {
		t.Skip("this process is not alive?")
	}
	rec := taskProcIdentity{Pid: pid, Cmd: "/usr/bin/somewhere-else --not-me"}
	if got := taskIdentityVerdict(pid, rec).State; got != TaskProcessStateRecycled {
		t.Fatalf("verdict = %q, want %q — with no start tick the command line is the identity",
			got, TaskProcessStateRecycled)
	}
}

// TestTaskStatusRecycledWhenNotAGopiProcess: the pre-existing name guard is
// still a factor whenever the row has NO recorded identity — an alive pid
// whose comm is not gopi* is a stranger, not the runner.
func TestTaskStatusRecycledWhenNotAGopiProcess(t *testing.T) {
	// Read this test binary's own comm and assert the guard's polarity
	// directly rather than guessing another process's name.
	name := strings.TrimSpace(string(mustReadFile(t, filepath.Join(procRootName, strconv.Itoa(os.Getpid()), procCommName))))
	if strings.HasPrefix(name, taskAliveProcessNamePrefix) {
		t.Skipf("this test binary's comm (%q) already carries the runner prefix — the guard's negative case is unreachable here", name)
	}
	if taskPidIsRunner(os.Getpid()) {
		t.Fatalf("comm %q must NOT be accepted as a gopi runner", name)
	}
	// No recorded identity → the name guard is the only evidence left, and
	// its vote is "recycled" (the number belongs to a stranger).
	if got := taskIdentityVerdict(os.Getpid(), taskProcIdentity{Pid: os.Getpid()}).State; got != TaskProcessStateRecycled {
		t.Fatalf("verdict = %q, want %q for an alive pid that is not a runner", got, TaskProcessStateRecycled)
	}
}

// mustReadFile reads a file or fails the test (fixture helper).
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// TestTaskStatusNoHolderIsReclaimable covers the epic's "fake in_progress"
// shape: a row with NO claim stamp at all (nothing names an executor). The
// verdict must say so plainly and allow the reclaim — assigning a worker is
// the remediation.
func TestTaskStatusNoHolderIsReclaimable(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-ident-noholder", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	// The forged shape: in_progress with no claim stamp and no identity —
	// exactly what a legacy auto-registered row looks like.
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = '', claimed_at = NULL, `+tasksInProgressAtColumnName+` = NULL,
		 `+tasksProcPidColumnName+` = NULL, `+tasksProcStartColumnName+` = NULL, `+tasksProcCmdColumnName+` = ''
		 WHERE task_id = ?`,
		taskStatusInProgress, "t-ident-noholder"); err != nil {
		t.Fatalf("forge: %v", err)
	}
	st, err := TaskProcessStatusFor("t-ident-noholder")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Verdict != TaskProcessStateNoHolder {
		t.Fatalf("verdict = %q, want %q — no claim stamp names an executor (note=%s)", st.Verdict, TaskProcessStateNoHolder, st.Note)
	}
	if !st.Resumable {
		t.Errorf("a no-holder row must be resumable (assigning a worker is the remediation)")
	}
}

// TestTaskStatusForeignHolderRefusesResume: a claim stamp minted on ANOTHER
// host is unverifiable here — resuming it locally could double-run it, so the
// verdict must block the resume rather than guess (Rule 15: no fabricated
// answer in either direction).
func TestTaskStatusForeignHolderRefusesResume(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-ident-foreign", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	now := float64(1234567890)
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+` = ? WHERE task_id = ?`,
		taskStatusInProgress, "some-other-host.example:4242", now, now, "t-ident-foreign"); err != nil {
		t.Fatalf("forge: %v", err)
	}
	st, err := TaskProcessStatusFor("t-ident-foreign")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	// The verdict is UNKNOWN since the host dimension was added: this host can
	// measure nothing about a pid another kernel owns, and "not measured" is the
	// one honest word for it. The CLASSIFICATION (this stamp is foreign) rides
	// the flag, which is what a consumer branches on.
	if st.Verdict != TaskProcessStateUnknown {
		t.Fatalf("verdict = %q, want %q (note=%s)", st.Verdict, TaskProcessStateUnknown, st.Note)
	}
	if !st.Foreign {
		t.Fatalf("holder_foreign = false on a stamp minted elsewhere — the reason is unreadable to a JSON consumer")
	}
	if st.Resumable {
		t.Errorf("a foreign holder must NOT be resumable — this host cannot prove the run is over")
	}
}

// TestTaskStatusNonInProgressIsNotApplicable: a completed row has no holder
// to verify. The verdict must be n/a (never a bogus "dead"/"running") and the
// row resumable (nothing can double-run).
func TestTaskStatusNonInProgressIsNotApplicable(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-ident-done", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := TaskClaimExplicit("t-ident-done", hostPid(t, os.Getpid())); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart("t-ident-done", hostPid(t, os.Getpid())); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := TaskRunWriteback("t-ident-done", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	st, err := TaskProcessStatusFor("t-ident-done")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Verdict != TaskProcessStateNotApplicable {
		t.Fatalf("verdict = %q, want %q for a terminal row", st.Verdict, TaskProcessStateNotApplicable)
	}
	// The HOLDER axis answered: no process can double-run this row. The ROW
	// still reads unresumable because of the STATUS axis (2026-10-16,
	// TaskRunResumeStatusGate): a completed row is finished work, not an
	// interrupted run. Asserting both keeps the two questions separate — a
	// future reader who "fixes" this back to resumable would re-open the
	// failed-row replay defect on the completed arm.
	if st.Resumable {
		t.Errorf("a terminal row is resumable — no process can double-run it (this assertion is deliberate: the status axis refuses finished work)")
	}
	if st.StatusRefusal == "" {
		t.Errorf("a terminal row must carry the status refusal")
	}
}

// TestIdentityClearedOnTerminalTransition is the leak guard: leaving
// in_progress must CLEAR the identity, or a later reader could compare a
// fresh pid against a stale record and read a stranger as this row's holder.
func TestIdentityClearedOnTerminalTransition(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	row := procIdentitySeedRow(t, "t-ident-clear", hostPid(t, os.Getpid()))
	if !row.HasProcIdentity {
		t.Fatalf("fixture precondition: the row must carry an identity before the terminal move")
	}
	if _, err := TaskRunWriteback("t-ident-clear", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	after, err := taskByID(ctx, db, "t-ident-clear")
	if err != nil || after == nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.HasProcIdentity || after.ProcPid != 0 || after.ProcCmd != "" {
		t.Errorf("terminal transition must clear the identity (pid=%d cmd=%q), got pid=%d cmd=%q",
			0, "", after.ProcPid, after.ProcCmd)
	}
}

// TestTaskShowRendersProcessIdentity pins the deliverable's first half: the
// three factors are visible in `task show`, so the operator does not need
// /proc to answer "was this row's holder real?".
func TestTaskShowRendersProcessIdentity(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	procIdentitySeedRow(t, "t-ident-show", hostPid(t, os.Getpid()))

	out, err := TaskShow("t-ident-show")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(out, "process:") {
		t.Fatalf("`task show` must render the process identity line, got:\n%s", out)
	}
	if !strings.Contains(out, "pid="+strconv.Itoa(os.Getpid())) {
		t.Errorf("the identity line must carry the recorded pid %d, got:\n%s", os.Getpid(), out)
	}
	if !strings.Contains(out, "cmd=") {
		t.Errorf("the identity line must carry the recorded command line, got:\n%s", out)
	}
}

// TestTaskStatusReportAndJSON pins the CLI-facing contract of the verdict:
// the human report names the verdict and the facts, and the JSON face
// round-trips through encoding/json with the verdict field intact (scripts
// read that field rather than the prose).
func TestTaskStatusReportAndJSON(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	pid := spawnDeadProcess(t)
	procIdentitySeedRow(t, "t-ident-report", hostPid(t, pid))

	st, err := TaskProcessStatusFor("t-ident-report")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	report := TaskProcessStatusReport(st)
	for _, want := range []string{"verdict:", TaskProcessStateDead, "resumable:", "holder:"} {
		if !strings.Contains(report, want) {
			t.Errorf("report must contain %q, got:\n%s", want, report)
		}
	}
	blob, err := TaskProcessStatusJSON(st)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	var back TaskProcessStatus
	if err := json.Unmarshal([]byte(blob), &back); err != nil {
		t.Fatalf("json round-trip: %v\n%s", err, blob)
	}
	if back.Verdict != TaskProcessStateDead || back.TaskID != "t-ident-report" {
		t.Errorf("json round-trip lost the verdict: %+v", back)
	}
}

// TestProcIdentityCaptureDegradesSafely: the capture must never fabricate an
// identity for a pid it could not read (Rule 15) — a gone pid records
// nothing, which the readers then treat as "no identity evidence".
func TestProcIdentityCaptureDegradesSafely(t *testing.T) {
	if _, ok := taskCaptureProcIdentity(0); ok {
		t.Errorf("pid 0 is not a process — the capture must refuse it")
	}
	if _, ok := taskCaptureProcIdentity(1 << 30); ok {
		t.Errorf("an out-of-range pid must not produce an identity")
	}
	ident, ok := taskCaptureProcIdentity(os.Getpid())
	if !ok || ident.Pid != os.Getpid() || !ident.HasStr {
		t.Fatalf("this process's identity must be readable: %+v ok=%v", ident, ok)
	}
}

// TestProcCmdlineTruncationIsStable: the recorded command line is bounded, so
// a stranger's megabyte argv cannot bloat the row — and the comparison still
// sees the same string the record holds (the truncation is not a source of
// false "recycled" verdicts).
func TestProcCmdlineTruncationIsStable(t *testing.T) {
	cmd := procCmdline(os.Getpid())
	if len(cmd) > procIdentityCmdlineMax {
		t.Fatalf("command line %d bytes exceeds the %d-byte bound", len(cmd), procIdentityCmdlineMax)
	}
	if strings.ContainsRune(cmd, 0) {
		t.Errorf("the recorded command line must be NUL-joined into a plain string, got %q", cmd)
	}
}
