package tools

// The holder-gone marker's EVIDENCE-AGREEMENT arm (taskholder_gone.go's
// taskHolderGoneProof): a row's run is recorded by TWO independent records —
// proc identity (stamped by each EXECUTOR INCARNATION, a short-lived child) and
// claimed_by (stamped once by the long-lived CLAIMANT). They name different
// pids by construction, so the identity record dying is a fact about ONE ATTEMPT,
// not about the run. The marker may only claim "the run is over" when every
// record this host can read agrees.
//
// MEASURED DEFECT THIS CLOSES (2026-11-08, rows 01a1bb5b-0fd3 and
// 01a1bb2d-e7bc): `gopi task show` printed, in ONE output,
//
//	status:      in_progress + holder dead (pid 4122750 gone)
//	claimed_by:  nick-sager:3177317
//
// while pid 3177317 was alive with its /proc/<pid>/cwd standing in the row's own
// session directory — a demonstrably working run called dead by a marker that
// had consulted only the executor incarnation.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// holderGoneRow builds an in_progress row with BOTH records present and
// independent: the identity triple (proc incarnation) and the claim stamp
// (claimant), each naming the pid the caller chose.
func holderGoneRow(t *testing.T, taskID string, identityPid, claimPid int) *Task {
	t.Helper()
	now := time.Now()
	claimed := float64(now.Add(-time.Hour).Unix())
	return &Task{
		TaskID:          taskID,
		Status:          taskStatusInProgress,
		Summary:         "two records, two pids",
		HasProcIdentity: true,
		ProcPid:         identityPid,
		// A recorded start tick the target cannot hold: for the recycled fixture
		// that IS the shape (a live number held by another incarnation); for the
		// dead fixture the number names nothing, so the ladder answers Dead
		// before comparing ticks at all.
		ProcStartTicks:  1,
		ProcCmd:         taskAliveProcessNamePrefix,
		ClaimedBy:       taskStateTestHolder(claimPid),
		HasClaim:        true,
		ClaimedAt:       claimed,
		HasInProgressAt: true,
		InProgressAt:    claimed,
	}
}

// holderGoneClampProbePid runs the PRODUCTION clamp probe's argv state, and
// returns the pid of a process that is REAPED before it returns — the exact
// "gone" shape the marker answers to, built from a real kernel reading instead
// of a guessed high number (taskStateTestDeadPid's fixture is a pid the kernel
// never had; both are real `gone` measurements, this one is the one an executor
// actually leaves behind).
func holderGoneReapedPid(t *testing.T) int {
	t.Helper()
	child := exec.Command("true")
	if err := child.Run(); err != nil { // Run() waits: the child is reaped, so its pid names nothing
		t.Fatalf("spawn the reaped-pid fixture: %v", err)
	}
	pid := child.Process.Pid
	if pidAlive(pid) {
		t.Fatalf("fixture broken: pid %d still reads alive after its own wait", pid)
	}
	return pid
}

// TestHolderGoneMarkerSilentWhenTheClaimantIsAlive is the MINIMAL REPRODUCTION of
// the defect: the recorded executor incarnation is gone, the claimant is running.
// No marker — the run is not over, and printing one is the false failure signal
// Rule 15 forbids.
func TestHolderGoneMarkerSilentWhenTheClaimantIsAlive(t *testing.T) {
	testutil.NewTestDBPath(t)
	deadIdentityPid := holderGoneReapedPid(t)

	live := testutil.StartMockProcessCmd(t, exec.Command("sleep", testutil.MockDaemonHoldSeconds))
	livePid := live.Pid()
	if !pidAlive(livePid) {
		t.Fatalf("fixture broken: the claimant pid %d is not alive", livePid)
	}

	row := holderGoneRow(t, "t-two-records-live-claim", deadIdentityPid, livePid)

	// PRECONDITION: record ① really does prove a death on its own. Without this
	// the test would pass against a fixture whose identity reading never fired,
	// and a guard over nothing is not a pass (Rule 15).
	if got := taskRowProcessLiveness(row).State; got != TaskProcessStateDead {
		t.Fatalf("fixture broken: the identity layer answered %q, want %q", got, TaskProcessStateDead)
	}
	if verdict := taskRowProcessLiveness(&Task{TaskID: "control", Status: taskStatusInProgress,
		ClaimedBy: taskStateTestHolder(livePid), HasClaim: true,
		ClaimedAt:       row.ClaimedAt,
		HasInProgressAt: true, InProgressAt: row.InProgressAt,
		HasProcIdentity: true, ProcPid: livePid, ProcStartTicks: 1}); verdict.State != TaskProcessStateRecycled {
		t.Fatalf("control broken: a live claimant with a mismatched tick must read Recycled, got %q", verdict.State)
	}

	// THE CONTRADICTION: ① measures a death, ② measures life ⇒ no death claim.
	if pid, proof := taskHolderGoneProof(row); proof != "" || pid != 0 {
		t.Fatalf("a row whose CLAIMANT is alive must claim nothing; got pid=%d proof=%q", pid, proof)
	}
	if m := taskHolderGoneMarker(row, taskStateUnknown); m != "" {
		t.Fatalf("the list surface marked a live run: %q", m)
	}
	if m := taskHolderGoneStatusMarker(row); m != "" {
		t.Fatalf("`task show` marked a live run: %q", m)
	}
}

// TestHolderGoneMarkerStillMarksTheTrulyDeadShapes is the zero-false-NEGATIVE
// half: the fix must not silence the marker on a run that really ended. Three
// shapes, each one an agreement of every readably recorded fact:
//
//	① identity Dead, claimant pid Dead          → "gone"
//	② identity Recycled, claimant pid Dead      → "recycled" (the wording follows
//	                                              the RECORDED INCARNATION)
//	③ identity Dead, no usable claim stamp      → "gone" (nothing contradicts it)
func TestHolderGoneMarkerStillMarksTheTrulyDeadShapes(t *testing.T) {
	testutil.NewTestDBPath(t)
	reaped := holderGoneReapedPid(t)

	// ① both records dead.
	gone := holderGoneRow(t, "t-both-dead", reaped, taskStateTestDeadPid(t))
	want := holderGoneMarkerFor(reaped)
	if m := taskHolderGoneMarker(gone, taskStateUnknown); m != want {
		t.Errorf("① both records dead: marker = %q, want %q", m, want)
	}
	if m := taskHolderGoneStatusMarker(gone); m != want {
		t.Errorf("① both records dead (task show): marker = %q, want %q", m, want)
	}

	// ② recycled incarnation, dead claimant: the RUN is over, and the wording
	// reports what was measured about the recorded number.
	self := os.Getpid()
	recycled := holderGoneRow(t, "t-recycled-dead-claim", self, taskStateTestDeadPid(t))
	if got := taskRowProcessLiveness(recycled).State; got != TaskProcessStateRecycled {
		t.Fatalf("② fixture broken: identity layer answered %q, want %q", got, TaskProcessStateRecycled)
	}
	wantRecycled := taskHolderGoneRender(self, taskHolderProofRecycled)
	if m := taskHolderGoneStatusMarker(recycled); m != wantRecycled {
		t.Errorf("② recycled + dead claimant: marker = %q, want %q", m, wantRecycled)
	}

	// ③ no claim stamp at all: the identity reading stands alone.
	stampless := holderGoneRow(t, "t-stampless", reaped, taskStateTestDeadPid(t))
	stampless.ClaimedBy = ""
	stampless.HasClaim = false
	if m := taskHolderGoneStatusMarker(stampless); m != want {
		t.Errorf("③ identity dead, no stamp: marker = %q, want %q", m, want)
	}

	// ④ a STALE claim stamp names a PREVIOUS claimant: its liveness says nothing
	// about this run, so it is neither a veto (here it would be one) nor proof.
	stale := holderGoneRow(t, "t-stale-stamp", reaped, self)
	stale.ClaimedAt = stale.InProgressAt - 3600 // an hour BEFORE the run started
	if got := claimStampFreshness(stale); got != claimStampStale {
		t.Fatalf("④ fixture broken: stamp freshness = %q, want %q", got, claimStampStale)
	}
	if m := taskHolderGoneStatusMarker(stale); m != want {
		t.Errorf("④ identity dead + a STALE stamp: marker = %q, want %q (a previous claimant's pid may not veto)", m, want)
	}
}

// TestHolderGoneMarkerAgreementOnTheRealBinary is the Rule 31 leg for the
// contradiction: the REAL binary, a scaffold ledger (Rule 32), `gopi task show`
// — the exact surface the defect was measured on.
func TestHolderGoneMarkerAgreementOnTheRealBinary(t *testing.T) {
	bin := testutil.GopiBin(t)
	db := testutil.NewTestDBPath(t)

	live := testutil.StartMockProcessCmd(t, exec.Command("sleep", testutil.MockDaemonHoldSeconds))
	livePid := live.Pid()
	ident, ok := taskCaptureProcIdentity(livePid)
	if !ok {
		t.Fatalf("pid %d just started, so its identity must be readable", livePid)
	}
	deadIdentityPid := holderGoneReapedPid(t)

	const aliveClaimRow = "t-agreement-live-claim"
	const deadRunRow = "t-agreement-dead-run"
	for _, id := range []string{aliveClaimRow, deadRunRow} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: "holder agreement fixture " + id, Payload: "in flight", TaskID: id, Source: "human",
		}); err != nil {
			t.Fatalf("submit %s: %v", id, err)
		}
	}

	tdb := openTestTasksDB(t, memoryDBPath())
	// aliveClaimRow: executor incarnation gone, claimant measurably ALIVE.
	// deadRunRow:    executor incarnation RECYCLED, claimant measurably DEAD —
	//                the control that proves the marker still fires on this surface.
	if _, err := tdb.ExecContext(context.Background(),
		`UPDATE tasks SET status=?, claimed_by=?, claimed_at=?, updated_at=?, `+
			tasksInProgressAtColumnName+`=?, `+tasksProcPidColumnName+`=?, `+
			tasksProcStartColumnName+`=?, `+tasksProcCmdColumnName+`=? WHERE task_id=?`,
		taskStatusInProgress, claimStampForTest(livePid), 1000.0, 1000.0, 1000.0,
		deadIdentityPid, 0, taskAliveProcessNamePrefix, aliveClaimRow); err != nil {
		tdb.Close() //stylegate:ignore the fixture DB is being abandoned on a fatal path; t.Fatalf carries the real cause
		t.Fatalf("stamp %s: %v", aliveClaimRow, err)
	}
	if _, err := tdb.ExecContext(context.Background(),
		`UPDATE tasks SET status=?, claimed_by=?, claimed_at=?, updated_at=?, `+
			tasksInProgressAtColumnName+`=?, `+tasksProcPidColumnName+`=?, `+
			tasksProcStartColumnName+`=?, `+tasksProcCmdColumnName+`=? WHERE task_id=?`,
		taskStatusInProgress, claimStampForTest(deadIdentityPid), 1000.0, 1000.0, 1000.0,
		ident.Pid, ident.Start+1, ident.Cmd, deadRunRow); err != nil {
		tdb.Close() //stylegate:ignore the fixture DB is being abandoned on a fatal path; t.Fatalf carries the real cause
		t.Fatalf("stamp %s: %v", deadRunRow, err)
	}
	tdb.Close() //stylegate:ignore the fixture's own handle at the end of its setup; the DB is read back by a child process

	env := []string{
		"GOPI_MEMORY_DB=" + db,
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"LANG=C",
	}

	show := func(id string) string {
		cmd := exec.Command(bin, "task", "show", id)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("task show %s: %v\n%s", id, err, out)
		}
		return string(out)
	}

	// THE DEFECT'S OWN SHAPE: one output carrying `claimed_by:` a live pid must
	// not also carry a death claim.
	liveOut := show(aliveClaimRow)
	if strings.Contains(liveOut, "holder dead") {
		t.Errorf("a row whose claimant %d is ALIVE carries a death claim:\n%s", livePid, liveOut)
	}
	if !strings.Contains(liveOut, "claimed_by:") || !strings.Contains(liveOut, claimStampForTest(livePid)) {
		t.Errorf("precondition: the fixture must render its live claim stamp:\n%s", liveOut)
	}

	// THE CONTROL: a genuinely dead run still says so.
	deadOut := show(deadRunRow)
	wantMarker := taskHolderGoneRender(ident.Pid, taskHolderProofRecycled)
	if !strings.Contains(deadOut, wantMarker) {
		t.Errorf("a genuinely over run lost its marker %q:\n%s", wantMarker, deadOut)
	}
}
