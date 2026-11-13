package tools

// Regression guards for the 2026-09-21/22 incident: a worktree was freed while
// its run was still live. The two `zz_redproof_*` files beside this one are the
// pre-fix reproductions (they fail on master, pass here); these are the
// permanent guards, named for the invariant each one holds.

import (
	"os"
	"os/exec"

	"strings"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestReclaimRefusesALiveRunnersGroundWhenTheRowIsTerminal pins the exact shape
// that beat the old guard: the row's status is TERMINAL, so the live-status arm
// of the veto can never fire, and the run's liveness is only visible through the
// row's recorded runner identity. Before the fix the pass freed the directory
// and printed a release note for a run that was demonstrably alive.
func TestReclaimRefusesALiveRunnersGroundWhenTheRowIsTerminal(t *testing.T) {
	repo, wt, _ := zzRedProofFixture(t, "t-regress-terminal-live", taskStatusCompleted)
	pid := zzRedProofLastPid(t, "t-regress-terminal-live")
	if !pidAlive(pid) {
		t.Fatalf("fixture: recorded runner pid %d must be alive", pid)
	}

	lines := ReclaimOwnFinishedWorktrees()
	joined := strings.Join(lines, "\n")

	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("a TERMINAL-status row whose recorded runner is alive must keep its ground, got err=%v; lines=%v", err, lines)
	}
	if !strings.Contains(joined, reclaimSelfRefusedMarker) {
		t.Errorf("the refusal must be NAMED, got %v", lines)
	}
	if !strings.Contains(joined, "t-regress-terminal-live") {
		t.Errorf("the refusal must name the row, got %v", lines)
	}
	if !strings.Contains(joined, "still has a run standing") {
		t.Errorf("the refusal must say WHY (a run is standing), got %v", lines)
	}
	if !worktreeListed(t, repo, wt) {
		t.Errorf("the registration must survive: %s", worktreeList(t, repo))
	}
}

// TestReclaimFreesATerminalRowsGroundOnceItsRunnerIsGone is the NEGATIVE half,
// and it is the half that keeps the fix from being a blanket wall: the same
// fixture with the runner killed must free the directory. A guard that refuses
// forever is a stopped release pass, not a guard.
func TestReclaimFreesATerminalRowsGroundOnceItsRunnerIsGone(t *testing.T) {
	repo, wt, _ := zzRedProofFixture(t, "t-regress-runners-gone", taskStatusCompleted)
	pid := zzRedProofLastPid(t, "t-regress-runners-gone")

	// Kill the recorded runner and WAIT for the pid to leave the table, so the
	// test measures "the runner is gone" rather than "the kill was requested"
	// (Rule 15: a verdict rests on something the host saw).
	//
	// The delivery is IDENTITY-checked (pid + starttime): the runner's pid was
	// recorded when the fixture was built, so signalling the bare number could
	// reach whichever process the kernel has since handed that number to — the
	// measured shape that put SIGINT into `systemd --user` on 2026-10-17
	// (cmd/agent/ground_kill_identity_test.go).
	runner, rerr := testutil.ReadProcIdentity(pid)
	if rerr != nil {
		t.Fatalf("read recorded runner %d: %v", pid, rerr)
	}
	if err := testutil.KillIfIdentical(runner, syscall.SIGKILL); err != nil {
		t.Fatalf("kill recorded runner %d: %v", pid, err)
	}
	waitPIDGone(t, pid)

	lines := ReclaimOwnFinishedWorktrees()
	joined := strings.Join(lines, "\n")

	if _, err := os.Stat(wt); err == nil {
		t.Fatalf("with the runner provably gone the ground must be freed, got lines=%v; worktree list: %s",
			lines, worktreeList(t, repo))
	}
	if !strings.Contains(joined, ReclaimSelfReleasedMarker) {
		t.Errorf("the release must be NAMED, got %v", lines)
	}
	if strings.Contains(joined, reclaimSelfRefusedMarker) {
		t.Errorf("nothing here should refuse, got %v", lines)
	}
}

// TestReclaimRefusesAGroundALiveHolderStillClaims covers the OTHER row shape the
// dead-holder scan sees: a row whose status is live, whose CLAIM pid is
// provably dead on this host, but whose recorded RUNNER is alive. That is the
// 2026-09-14 shape (one row, two sessions — the claim stamp names whoever took
// the row, the runner names the process actually working), and reading the dead
// claim as "the run is over" is what would free the ground. It pins that the
// identity channel is asked at the CANDIDATE SCAN, before the disposition: the
// refusal is not a skip reason here, it is the row never becoming a candidate.
func TestReclaimRefusesAGroundALiveHolderStillClaims(t *testing.T) {
	// A real live process plays the runner; a pid that provably does not exist
	// plays the dead claim holder.
	// Spawned through the shared helper so the stand-in cannot outlive this
	// test BINARY (Pdeathsig; a t.Cleanup never runs on a SIGKILL or on go
	// test's -timeout exit) and its cwd is never inside the checkout.
	runnerMP := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	runner := runnerMP.Cmd()
	ident, ok := taskCaptureProcIdentity(runner.Process.Pid)
	if !ok {
		t.Skip("cannot capture the runner identity")
	}
	live := procIdentity{Pid: ident.Pid, StartTicks: ident.Start, Cmd: ident.Cmd}
	if !finishCleanupProcessHolderLive(live) {
		t.Skip("the stand-in runner does not read as live here")
	}

	// The claim stamp names a pid this host cannot find, so `claimHolder`'s
	// ok=true + `pidAlive` false is the dead-claim half — the shape the scan
	// must defeat with the identity channel.
	deadPid := freePIDForTest(t)

	cand, ok := reclaimDeadHolderCandidate("t-regress-deadclaim-live-runner", taskStatusInProgress,
		claimStampForTest(deadPid), `{"repo":"/repo","branch":"b","worktree":"/wt"}`,
		procIdentity{Pid: ident.Pid, StartTicks: ident.Start, Cmd: ident.Cmd})
	if ok {
		t.Fatalf("a row whose recorded runner %d is ALIVE must never become a reclaim candidate (got %+v)", ident.Pid, cand)
	}
	if cand.TaskID != "" || cand.Worktree != "" {
		t.Fatalf("a refused candidate must be the zero value, got %+v", cand)
	}
}

// freePIDForTest returns a pid in this host's pid space that is provably not
// running, so "the claim holder is dead" is measured rather than assumed.
func freePIDForTest(t *testing.T) int {
	t.Helper()
	for pid := 999999; pid > 1; pid-- {
		if !pidAlive(pid) {
			return pid
		}
	}
	t.Skip("no free pid in this host's range — the dead-claim half cannot be set up")
	return 0
}

// waitPIDGone blocks until pid leaves the process table (bounded), so a test
// that asserts "the runner is dead" observes the death rather than assuming it.
func waitPIDGone(t *testing.T, pid int) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if !pidAlive(pid) {
			return
		}
		// A zombie is dead by pidAlive's contract; anything else is still there.
		sleepTicks()
	}
	t.Fatalf("pid %d still looks alive after the kill — the negative case cannot be measured", pid)
}

// sleepTicks waits one short slice between pid polls without importing a clock
// constant into the assertion itself.
func sleepTicks() {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		_ = exec.Command("sleep", "0.02").Run() //stylegate:ignore a bounded poll interval, not a measured operation
	}()
	<-ch
}

// TestReclaimDispositionSkipsAnInProgressRowWithALiveRunner is acceptance row 1
// in the shape the briefing names it: an `in_progress` row whose recorded runner
// is a live process must yield reclaimSkip and say why — through
// reclaimDisposition, the shared decision every arm consumes.
func TestReclaimDispositionSkipsAnInProgressRowWithALiveRunner(t *testing.T) {
	// Spawned through the shared helper so the stand-in cannot outlive this
	// test BINARY (Pdeathsig; a t.Cleanup never runs on a SIGKILL or on go
	// test's -timeout exit) and its cwd is never inside the checkout.
	runnerMP := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	runner := runnerMP.Cmd()
	ident, ok := taskCaptureProcIdentity(runner.Process.Pid)
	if !ok {
		t.Skip("cannot capture the runner identity")
	}
	start, ok := procStartTicks(runner.Process.Pid)
	if !ok {
		t.Skip("cannot read the runner's start ticks")
	}
	row := reclaimDeadHolderRow{
		TaskID:    "t-accept-live",
		Status:    taskStatusInProgress,
		ClaimedBy: claimStampForTest(freePIDForTest(t)),
		Repo:      "/nonexistent/repo",
		Worktree:  "/nonexistent/ground",
		Ident:     procIdentity{Pid: ident.Pid, StartTicks: start, Cmd: ident.Cmd},
	}
	disp, why := reclaimDisposition(row, nil)
	if disp != reclaimSkip {
		t.Fatalf("an in_progress row with a LIVE runner must be reclaimSkip, got %v (%s)", disp, why)
	}
	if !strings.Contains(why, "t-accept-live") {
		t.Errorf("the refusal must name the row, got %q", why)
	}
	if !strings.Contains(why, "still") && !strings.Contains(why, "live") {
		t.Errorf("the refusal must say WHY, got %q", why)
	}
}
