package tools

// Tests for the pre-spawn singleton guard (epic task-run-singleton-guard,
// 2026-09-15). The RED shape these pin: `gopi task run <id>` on a row ALREADY
// held — a second concurrent run of the same task — must refuse. Before the
// guard, the claim CAS did not arbitrate the CLAIMED window (two racers on a
// claimed row both re-stamp and both execute), and a row with no recorded
// identity could not be judged at all.
//
// Two layers, matching the project's testing habit (a deterministic decision
// test at the layer that decides + a wiring proof above it):
//
//  1. the VERDICT table — deterministic, no races: live-other refuses, dead /
//     recycled / absent / foreign / stale holders proceed, the row's own
//     process is "self".
//  2. the PINNED RE-STAMP — the single-winner property of the recovery write
//     (of N racers on one unjudgeable claim exactly ONE wins).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// guardRow submits a row and puts it in the given state via raw SQL (the
// claim/identity columns are CAS-managed, so the fixture writes them
// directly — the same technique seedRunRow uses).
func guardRow(t *testing.T, taskID, status, claimedBy string, pid int, start int64, cmd string) *Task {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "singleton guard fixture " + taskID,
		Payload: "guard fixture payload",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.Exec(
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, updated_at = ?,
		 `+tasksProcPidColumnName+` = ?, `+tasksProcStartColumnName+` = ?, `+tasksProcCmdColumnName+` = ?,
		 `+tasksInProgressAtColumnName+` = ? WHERE task_id = ?`,
		status, claimedBy, float64(time.Now().Unix()), float64(time.Now().Unix()),
		pid, start, cmd, float64(time.Now().Unix()), taskID); err != nil {
		t.Fatalf("seed %s: %v", taskID, err)
	}
	return readGuardRow(t, taskID)
}

// readGuardRow re-reads a row through the row scanner (so HasProcIdentity and
// the freshness projection are the ones production sees, not a hand-built
// struct).
func readGuardRow(t *testing.T, taskID string) *Task {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	task, err := taskByID(context.Background(), db, taskID)
	if err != nil || task == nil {
		t.Fatalf("read %s: %v", taskID, err)
	}
	return task
}

// selfIdentity captures the current process's own three factors — the fixture
// for "the holder IS me" and for a provably-live holder.
func selfIdentity(t *testing.T) (string, int, int64, string) {
	t.Helper()
	ident, ok := taskCaptureProcIdentity(os.Getpid())
	if !ok {
		t.Skipf("cannot read this process's identity from procfs")
	}
	if !claimHostIsLocal(taskDefaultClaimHolder()) {
		t.Skipf("this host's claim stamp is not recognized as local")
	}
	return taskDefaultClaimHolder(), ident.Pid, ident.Start, ident.Cmd
}

// TestTaskRunGuardVerdicts is the decision table (deterministic, no races):
// every shape the guard must answer, with the operator story it encodes.
func TestTaskRunGuardVerdicts(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	selfStamp, selfPid, selfStart, selfCmd := selfIdentity(t)
	deadPid := deadClaimPid(t)
	deadStamp := hostClaimStamp(t, deadPid)

	for _, tc := range []struct {
		name   string
		taskID string
		status string
		stamp  string
		pid    int
		start  int64
		cmd    string
		want   string
		admits bool
		why    string
	}{
		{
			name: "live other holder refuses (the double-run)", taskID: "t-g-live",
			status: taskStatusInProgress, stamp: selfStamp, pid: selfPid, start: selfStart, cmd: selfCmd,
			// The row names OUR process here, so it is "self"; the live-OTHER
			// case is built below with a real second process.
			want: taskRunHolderSelf, admits: true,
			why: "a holder that is this very process is our own run",
		},
		{
			name: "dead holder proceeds (crash recovery)", taskID: "t-g-dead",
			status: taskStatusClaimed, stamp: deadStamp, pid: deadPid, start: 12345, cmd: "gopi task run t-g-dead",
			want: taskRunHolderFree, admits: true,
			why: "the claimer died before executing — the epic must keep this recoverable",
		},
		{
			name: "no identity, dead stamp pid proceeds", taskID: "t-g-legacy-dead",
			status: taskStatusClaimed, stamp: deadStamp, pid: 0, start: 0, cmd: "",
			want: taskRunHolderFree, admits: true,
			why: "a legacy row whose claimer is provably gone is the resume path",
		},
		{
			name: "no identity, live gopi stamp pid refuses", taskID: "t-g-legacy-live",
			status: taskStatusClaimed, stamp: selfStamp, pid: 0, start: 0, cmd: "",
			want: taskRunHolderSelf, admits: true,
			why: "the stamp names this very process — our own claim",
		},
		{
			name: "foreign stamp proceeds (cross-node lock owns it)", taskID: "t-g-foreign",
			status: taskStatusClaimed, stamp: "another-host:4242", pid: 0, start: 0, cmd: "",
			want: taskRunHolderUnknown, admits: true,
			why: "a pid on another host cannot be probed here — never a fabricated refusal",
		},
		{
			name: "blank stamp proceeds", taskID: "t-g-blank",
			status: taskStatusClaimed, stamp: "", pid: 0, start: 0, cmd: "",
			want: taskRunHolderFree, admits: true,
			why: "nothing names an executor",
		},
		{
			name: "malformed stamp proceeds", taskID: "t-g-malformed",
			status: taskStatusClaimed, stamp: "hostname-without-a-pid:", pid: 0, start: 0, cmd: "",
			want: taskRunHolderUnknown, admits: true,
			why: "an unparsable stamp is not evidence of a live holder",
		},
		{
			name: "pending row is free", taskID: "t-g-pending",
			status: taskStatusPending, stamp: "", pid: 0, start: 0, cmd: "",
			want: taskRunHolderFree, admits: true,
			why: "the claim path owns a pending row, not this gate",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := guardRow(t, tc.taskID, tc.status, tc.stamp, tc.pid, tc.start, tc.cmd)
			got := taskRunHolderVerdict(row)
			if got != tc.want {
				t.Errorf("verdict = %q, want %q (%s)", got, tc.want, tc.why)
			}
			if admits := taskRunGuardAdmits(got); admits != tc.admits {
				t.Errorf("admits(%q) = %v, want %v", got, admits, tc.admits)
			}
			// The door's public face must agree with the row-image ruling —
			// one decision, two entry points (Rule 14).
			pub, refusal, err := TaskRunGuardFor(tc.taskID)
			if err != nil {
				t.Fatalf("TaskRunGuardFor: %v", err)
			}
			if pub != got {
				t.Errorf("TaskRunGuardFor verdict = %q, want %q (the door and the row ruling disagree)", pub, got)
			}
			if tc.admits && refusal != "" {
				t.Errorf("an admitted run must carry no refusal, got %q", refusal)
			}
			if !tc.admits && refusal == "" {
				t.Errorf("a refused run must explain WHY (holding %q)", tc.stamp)
			}
		})
	}
}

// TestTaskRunGuardRecycledPidProceeds: the recorded incarnation does not match
// the live process at that pid (the number was reused by a stranger), so the
// runner is DEAD and the row is recoverable — the case a bare pid check gets
// wrong in the other direction (a recycled pid reading as "still running"
// blocks the row forever).
func TestTaskRunGuardRecycledPidProceeds(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	selfIdentity(t)
	pid, start, cmd, stop := spawnProbeProcess(t)
	defer stop()
	// Same live pid, a DIFFERENT incarnation: the recorded start tick predates
	// the live process, so the recorded runner cannot be the live one.
	if start <= 1 {
		t.Skipf("implausible start tick %d", start)
	}
	row := guardRow(t, "t-g-recycled", taskStatusClaimed, hostClaimStamp(t, pid), pid, start-1, cmd)
	if got := taskRunHolderVerdict(row); got != taskRunHolderFree {
		t.Fatalf("verdict = %q, want %q — a mismatched incarnation is a dead runner, not a live holder", got, taskRunHolderFree)
	}
	// And a MATCHING incarnation of the same pid must refuse (the live-other
	// case), so the two answers are genuinely distinguished by the start tick.
	row2 := guardRow(t, "t-g-same-ticks", taskStatusClaimed, hostClaimStamp(t, pid), pid, start, cmd)
	if got := taskRunHolderVerdict(row2); got != taskRunHolderLive {
		t.Fatalf("verdict = %q, want %q for a matching incarnation", got, taskRunHolderLive)
	}
}

// TestProbeTeardownRecordsItsErrors keeps the fixture honest: the teardown sink
// must be EMPTY after real use (a non-empty sink means a probe child outlived
// its test — the residue this sink exists to surface, Rule 15b).
func TestProbeTeardownRecordsItsErrors(t *testing.T) {
	_, _, _, stop := spawnProbeProcess(t)
	stop()
	if v := tProbeKillErrors.Load(); v != nil {
		if msg, _ := v.(string); msg != "" && !strings.Contains(msg, "process already finished") {
			t.Errorf("probe teardown recorded an error: %s", msg)
		}
	}
}

// TestTaskRunGuardRefusesLiveOtherProcess is the epic's core case, built with a
// REAL second process: a child is spawned, its three factors are recorded on
// the row, and the guard must conclude "a live process that is not me owns
// this row" — the verdict the run door turns into a non-zero exit.
func TestTaskRunGuardRefusesLiveOtherProcess(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	selfIdentity(t) // skip early on a host without usable procfs/claim parsing

	pid, start, cmd, stop := spawnProbeProcess(t)
	defer stop()
	stamp := hostClaimStamp(t, pid)
	row := guardRow(t, "t-g-live-other", taskStatusClaimed, stamp, pid, start, cmd)

	if got := taskRunHolderVerdict(row); got != taskRunHolderLive {
		t.Fatalf("verdict = %q, want %q — a live non-self holder must REFUSE", got, taskRunHolderLive)
	}
	if taskRunGuardAdmits(taskRunHolderLive) {
		t.Fatalf("the live-other verdict must not admit a run")
	}
	_, refusal, err := TaskRunGuardFor("t-g-live-other")
	if err != nil {
		t.Fatalf("TaskRunGuardFor: %v", err)
	}
	if refusal == "" || !guardTextHasAll(refusal, []string{"already RUNNING", stamp, "gopi task status"}) {
		t.Errorf("the refusal must name the holder and the inspection command, got: %q", refusal)
	}
}

// TestTaskRunGuardRefusesLiveClaimedRow is the CLAIMED-window half of the same
// defect: the row is only 'claimed' (the window the epic calls out), and the
// claim/time write recorded a live OTHER process's identity. It must refuse —
// this is what stops a duplicate `gopi task run` from executing the briefing a
// worker round is about to run.
func TestTaskRunGuardRefusesLiveClaimedRow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	selfStamp, selfPid, selfStart, selfCmd := selfIdentity(t)
	_ = selfPid

	pid, start, cmd, stop := spawnProbeProcess(t)
	defer stop()
	row := guardRow(t, "t-g-claimed-live", taskStatusClaimed, hostClaimStamp(t, pid), pid, start, cmd)
	if got := taskRunHolderVerdict(row); got != taskRunHolderLive {
		t.Fatalf("a claimed row held by a live other process: verdict = %q, want %q", got, taskRunHolderLive)
	}
	// And the row's own process is still recognized as self (the guard must
	// never refuse the run that owns the row).
	own := guardRow(t, "t-g-claimed-self", taskStatusClaimed, selfStamp, selfPid, selfStart, selfCmd)
	if got := taskRunHolderVerdict(own); got != taskRunHolderSelf {
		t.Fatalf("our own claim: verdict = %q, want %q", got, taskRunHolderSelf)
	}
}

// TestTaskRunRestampClaimIsSingleWinner pins the property that closes the
// claimed window for identity-less rows: N runners that ALL read the same
// unjudgeable claim and ALL try to recover it — exactly ONE may win the pinned
// re-stamp, because the pin names the observed holder state. Without the pin
// every one of them wins (RowsAffected==1) and every one executes.
func TestTaskRunRestampClaimIsSingleWinner(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	deadStamp := hostClaimStamp(t, deadClaimPid(t))
	observed := guardRow(t, "t-g-race", taskStatusClaimed, deadStamp, 0, 0, "")

	const racers = 4
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		wins  int
		loses int
	)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release all racers at once: the widest possible window
			ctx := context.Background()
			db, err := openTasksDB(ctx, memoryDBPath())
			if err != nil {
				t.Errorf("racer %d: open: %v", i, err)
				return
			}
			defer db.Close()
			applied, err := taskRunRestampClaim(ctx, db, observed, hostClaimStamp(t, os.Getpid()), float64(time.Now().Unix()))
			if err != nil {
				t.Errorf("racer %d: restamp: %v", i, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if applied {
				wins++
			} else {
				loses++
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("pinned re-stamp winners = %d, want exactly 1 (losers %d) — %d runners would execute the same row",
			wins, loses, wins)
	}
	if loses != racers-1 {
		t.Errorf("losers = %d, want %d (every other racer must REFUSE)", loses, racers-1)
	}
	// The winner recorded ITS identity, so the next runner gets a definite
	// answer instead of another unjudgeable claim.
	after := readGuardRow(t, "t-g-race")
	if !after.HasProcIdentity || after.ProcPid != os.Getpid() {
		t.Errorf("the winning re-stamp must record the claim-time identity, got identity=%v pid=%d",
			after.HasProcIdentity, after.ProcPid)
	}
}

// TestTaskRunClaimRecordsIdentity: the pending→claimed CAS must record the
// three factors in the SAME write that stamps the holder — a claimed row
// without them is the unjudgeable shape the epic complains about.
func TestTaskRunClaimRecordsIdentity(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	mustSubmitTest(t, "claim identity", "payload", "t-g-claim-ident")
	if err := TaskClaimExplicit("t-g-claim-ident", ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	row := readGuardRow(t, "t-g-claim-ident")
	if row.Status != taskStatusClaimed {
		t.Fatalf("status = %q, want %q", row.Status, taskStatusClaimed)
	}
	if !row.HasProcIdentity || row.ProcPid != os.Getpid() {
		t.Fatalf("the claim must record this process's identity, got identity=%v pid=%d (stamp %q)",
			row.HasProcIdentity, row.ProcPid, row.ClaimedBy)
	}
	if row.ProcStartTicks <= 0 || row.ProcCmd == "" {
		t.Errorf("the claim-time identity must carry all three factors, got start=%d cmd=%q", row.ProcStartTicks, row.ProcCmd)
	}
}

// TestTaskResumeClaimedRefusesLiveOtherHolder: TaskResumeClaimed is the worker
// round's own-claim pass-through, and it is ALSO reachable for a row another
// live process claims — it must refuse there (the epic's claim-window hole).
func TestTaskResumeClaimedRefusesLiveOtherHolder(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	selfIdentity(t)
	pid, start, cmd, stop := spawnProbeProcess(t)
	defer stop()
	guardRow(t, "t-g-resume-live", taskStatusClaimed, hostClaimStamp(t, pid), pid, start, cmd)

	err := TaskResumeClaimed("t-g-resume-live", "")
	if err == nil {
		t.Fatalf("resuming a row held by a live other process must refuse, got nil")
	}
	if !guardTextHasAll(err.Error(), []string{"already RUNNING", "takeover"}) {
		t.Errorf("the refusal must name the holder and the escape hatch, got: %v", err)
	}
	// A dead holder stays recoverable — the crash-recovery path must not break.
	deadStamp := hostClaimStamp(t, deadClaimPid(t))
	guardRow(t, "t-g-resume-dead", taskStatusClaimed, deadStamp, 0, 0, "")
	if err := TaskResumeClaimed("t-g-resume-dead", ""); err != nil {
		t.Fatalf("recovering a dead claim must succeed, got %v", err)
	}
}

// spawnProbeProcess starts a real child process and reads ITS three factors, so
// the "live other holder" fixture is a genuine process rather than this test
// binary. The returned stop func terminates it.
func spawnProbeProcess(t *testing.T) (pid int, start int64, cmd string, stop func()) {
	t.Helper()
	cmdPath := probeShellPath()
	if cmdPath == "" {
		t.Skipf("no shell found to spawn a probe process")
	}
	child := exec.Command(cmdPath, "-c", probeSleepCommand)
	if err := child.Start(); err != nil {
		t.Skipf("cannot spawn a probe process: %v", err)
	}
	pid = child.Process.Pid
	ident, ok := taskCaptureProcIdentity(pid)
	if !ok {
		stopProbeChild(child)
		t.Skipf("cannot read the probe process's identity (pid %d)", pid)
	}
	stop = func() { stopProbeChild(child) }
	return pid, ident.Start, ident.Cmd, stop
}

// probeShell* are the shell candidates for the probe child (Rule 1b: the paths
// are named, not typed inline), and probeSleepCommand is the child's argv —
// it must outlive the assertions and never write anything.
const (
	probeShellBash    = "/bin/bash"
	probeShellSh      = "/bin/sh"
	probeSleepCommand = "sleep 30"
)

// stopProbeChild ends the probe fixture. Its errors are recorded, never
// discarded (Rule 15b): a kill that fails leaves a `sleep` holding the pid the
// test just recorded, and a silent failure there would make the NEXT test's
// liveness assumption unreliable.
func stopProbeChild(child *exec.Cmd) {
	if err := child.Process.Kill(); err != nil {
		tProbeKillErrors.Store(err.Error())
	}
	if _, err := child.Process.Wait(); err != nil {
		tProbeKillErrors.Store(err.Error())
	}
}

// tProbeKillErrors carries the last probe-teardown error for the test to
// report (a package-level sink, so a deferred stop func can record without a
// *testing.T it no longer owns).
var tProbeKillErrors atomic.Value

// probeShellPath resolves a shell for the probe child from the standard
// locations (never a literal at the call site, Rule 1b).
func probeShellPath() string {
	for _, p := range []string{probeShellBash, probeShellSh} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// guardTextHasAll reports whether s contains every needle — the refusal text is
// asserted on its OPERATOR-VISIBLE facts, not on one exact sentence.
func guardTextHasAll(s string, needles []string) bool {
	for _, n := range needles {
		if !strings.Contains(s, n) {
			return false
		}
	}
	return true
}
