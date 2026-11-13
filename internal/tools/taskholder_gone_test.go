package tools

// The holder-gone marker's two-sided proof (taskholder_gone.go): a row whose
// executor is MEASURABLY gone is marked on the list and show surfaces, and a row
// whose holder is really alive — or whose pid number was REUSED by a stranger —
// is left alone. Without the second half, a marker that fired on every
// in_progress row would be worse than no marker at all.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// holderGoneMarkerFor renders the exact suffix the implementation emits for one
// pid, so a test never hard-codes the wording (a second spelling of the format
// is what Rule 1b forbids).
func holderGoneMarkerFor(pid int) string {
	return fmt.Sprintf(taskHolderGoneMarkerFmt, pid)
}

// stampHolder writes the claim columns + the in_progress transition onto a
// seeded row, so the claim stamp is FRESH for the row's start (the shape whose
// pid layer really votes) and the heartbeat is long stale.
func stampHolder(t *testing.T, taskID, holder string, startedAt time.Time) {
	t.Helper()
	stampHolderWithWorktree(t, taskID, holder, startedAt, "")
}

// stampHolderWithWorktree is stampHolder PLUS the deliverable's worktree — the
// shape that makes the state ladder WITHHOLD the `died` label (recoverable
// working state on disk), which is exactly the row measured on 2026-11-05: its
// worktree was intact, so the list said "unknown: worktree still present" for
// 8h38m while every other measurement said the holder was gone.
func stampHolderWithWorktree(t *testing.T, taskID, holder string, startedAt time.Time, worktree string) {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	started := float64(startedAt.Unix())
	deliverable := ""
	if worktree != "" {
		deliverable = fmt.Sprintf(`{"repo":"/repo","worktree":%q}`, worktree)
	}
	if _, err := db.Exec(`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?,
		updated_at = ?, in_progress_at = ?, deliverable = ? WHERE task_id = ?`,
		taskStatusInProgress, holder, started, started, started, deliverable, taskID); err != nil {
		db.Close() //stylegate:ignore the test is already failing and cannot use the handle
		t.Fatalf("stamp holder on %s: %v", taskID, err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
}

// TestHolderGoneMarkerOnDeadHolderListRow: an in_progress row whose claim stamp
// names a pid this host can prove is gone renders the marker — with no worker
// running, no sweep, and no status write (the row stays in_progress: the
// operator's ruling of 2026-11-05 forbids one process closing another's row).
func TestHolderGoneMarkerOnDeadHolderListRow(t *testing.T) {
	testutil.NewTestDBPath(t)
	deadPid := taskStateTestDeadPid(t)

	seedTaskRow(t, "t-gone-1", "row whose executor died")
	// The measured row's shape: the run left its worktree intact, so the state
	// ladder withholds `died` and the MARKER is what carries the death.
	wt := t.TempDir()
	stampHolderWithWorktree(t, "t-gone-1", taskStateTestHolder(deadPid), time.Now().Add(-9*time.Hour), wt)

	wantMarker := holderGoneMarkerFor(deadPid)
	out, err := TaskList(taskStatusInProgress)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(out, wantMarker) {
		t.Fatalf("dead-holder row is not marked %q:\n%s", wantMarker, out)
	}
	// …and reading it did not WRITE: the row is still in_progress (a read verb
	// that closed a row would be the "another process tidies up" shape the
	// operator's 2026-11-05 ruling forbids).
	row, err := TaskShow("t-gone-1")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(row, "status:         "+taskStatusInProgress) {
		t.Errorf("a read must not move the row out of in_progress:\n%s", row)
	}
	if !strings.Contains(row, wantMarker) {
		t.Errorf("`task show` does not carry the same measured marker:\n%s", row)
	}
}

// TestHolderGoneMarkerLeavesLiveHolderAlone is the other side: a row whose claim
// stamp names a process that is REALLY running must not be marked, or the marker
// would be noise on every healthy row and an operator would learn to ignore it.
func TestHolderGoneMarkerLeavesLiveHolderAlone(t *testing.T) {
	testutil.NewTestDBPath(t)

	// A real, living holder — spawned through testutil.StartMockProcessCmd, the
	// ONE sanctioned spawn: it owns a fresh process group, arms Pdeathsig (so the
	// holder cannot outlive a SIGKILLed test binary and turn some checkout into
	// an `IN USE forever` ground), and registers its own reaping cleanup (Rule
	// 42: it signals only the group it created).
	child := testutil.StartMockProcessCmd(t, exec.Command("sleep", "300"))
	livePid := child.Pid()

	seedTaskRow(t, "t-live-1", "row whose executor is running")
	stampHolder(t, "t-live-1", taskStateTestHolder(livePid), time.Now().Add(-2*time.Minute))

	out, err := TaskList(taskStatusInProgress)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if strings.Contains(out, holderGoneMarkerFor(livePid)) {
		t.Fatalf("a LIVE holder was marked gone (pid %d is running):\n%s", livePid, out)
	}
	if strings.Contains(out, "holder dead") {
		t.Fatalf("a live holder's row carries a holder-dead marker:\n%s", out)
	}
	// The row still renders as a live in_progress row.
	if !strings.Contains(out, "t-live-1") {
		t.Fatalf("the live row vanished from the list:\n%s", out)
	}
}

// TestHolderGoneMarkerMarksARecycledPid: the pid number is alive but it is a
// DIFFERENT incarnation than the row recorded, so the run this row recorded is
// over. It gets the RECYCLED wording, not the "gone" one — a live number is
// never described as gone (Rule 15: say what was measured).
//
// WHY RECYCLED IS A DEATH, and why this test used to assert the opposite
// (corrected 2026-11-05, row 01a1a862): refusing it looked conservative, but
// the state ladder ALREADY decides that shape as settled — taskstate.go's
// `aliveChecked && !alive` arm reads a reused number as "not a live runner",
// with the comment "its number was reused, or it is gone. That is a decidable
// row". So the old contract let ONE row carry two verdicts (Rule 14), and it
// left the defect of this epic visible for exactly the recycled shape:
// measured on a scaffold ledger — verdict="recycled", the list still rendered
// `[unknown: worktree still present, session did not end, age …]` and the
// marker was "". The unrecycled shape (Dead) was already covered.
func TestHolderGoneMarkerMarksARecycledPid(t *testing.T) {
	testutil.NewTestDBPath(t)
	now := time.Now()
	self := os.Getpid()

	row := &Task{
		TaskID: "t-recycled", Status: taskStatusInProgress,
		ClaimedBy: taskStateTestHolder(self), HasClaim: true,
		ClaimedAt:       float64(now.Add(-time.Hour).Unix()),
		HasInProgressAt: true, InProgressAt: float64(now.Add(-time.Hour).Unix()),
	}
	// The RECORDED identity: this live pid, but an incarnation tick it does not
	// hold (no process can carry tick 1 — the fixture's own "different
	// incarnation" shape, and the reader's ladder answers Recycled).
	row.HasProcIdentity = true
	row.ProcPid = self
	row.ProcStartTicks = 1

	if got := taskRowProcessLiveness(row).State; got != TaskProcessStateRecycled {
		t.Fatalf("fixture broken: recorded-identity verdict = %q, want %q", got, TaskProcessStateRecycled)
	}
	pid, proof := taskHolderGoneProof(row)
	if proof != taskHolderProofRecycled {
		t.Fatalf("a RECYCLED pid must prove %q (the run recorded is over), got %q", taskHolderProofRecycled, proof)
	}
	if pid != self {
		t.Fatalf("the proof must name the recorded pid %d, got %d", self, pid)
	}
	want := fmt.Sprintf(taskHolderRecycledMarkerFmt, self)
	if m := taskHolderGoneMarker(row, taskStateUnknown); m != want {
		t.Fatalf("marker on the list = %q, want %q (a live number is RECYCLED, never gone)", m, want)
	}
	if m := taskHolderGoneStatusMarker(row); m != want {
		t.Fatalf("marker on `task show` = %q, want %q — one measurement, one spelling (Rule 14)", m, want)
	}
	// The `died` label already says it; the marker must not repeat the claim.
	if m := taskHolderGoneMarker(row, taskStateDied); m != "" {
		t.Fatalf("a row already labelled %q got the marker %q as well", taskStateDied, m)
	}
}

// TestHolderGoneMarkerKeepsTheTwoProofsApart: the two wordings are chosen from
// the PROOF, so a Dead reading can never render the Recycled text and vice
// versa. This is the Rule 14 arm: one measurement has one spelling per shape.
func TestHolderGoneMarkerKeepsTheTwoProofsApart(t *testing.T) {
	const pid = 4242
	gone := taskHolderGoneRender(pid, taskHolderProofGone)
	recycled := taskHolderGoneRender(pid, taskHolderProofRecycled)
	if gone != fmt.Sprintf(taskHolderGoneMarkerFmt, pid) {
		t.Fatalf("gone proof rendered %q", gone)
	}
	if recycled != fmt.Sprintf(taskHolderRecycledMarkerFmt, pid) {
		t.Fatalf("recycled proof rendered %q", recycled)
	}
	if gone == recycled {
		t.Fatalf("the two proofs must not share a spelling: %q", gone)
	}
	if got := taskHolderGoneRender(pid, ""); got != "" {
		t.Fatalf("no proof must render nothing, got %q", got)
	}
}

// TestHolderGoneMarkerRecycledOnTheRealBinary is the Rule 31 leg for the
// recycled shape: the assertions above drive the renderers in-process, which
// proves the FUNCTIONS but not the SURFACE. This one builds a scaffold ledger
// (Rule 32) and runs the REAL binary — `gopi task list` and `gopi task show` —
// against a row whose recorded pid is alive as a DIFFERENT incarnation and
// whose worktree still exists, so the state ladder says
// `unknown: worktree still present` and the marker is the only thing naming
// the measured death.
//
// Env is passed explicitly: the child must read THIS scaffold's ledger, and
// must not inherit this test binary's suite pin (GOPI_TEST_BIN).
func TestHolderGoneMarkerRecycledOnTheRealBinary(t *testing.T) {
	bin := testutil.GopiBin(t)
	db := testutil.NewTestDBPath(t)

	// A live process whose NUMBER we keep, and an incarnation tick it does not
	// hold — the recycled shape, built from real kernel measurements. Spawned
	// through the sanctioned helper for the same two reasons as the holder
	// above: its own process group and a Pdeathsig that survives a SIGKILLed
	// test binary.
	live := testutil.StartMockProcessCmd(t, exec.Command("sleep", testutil.MockDaemonHoldSeconds))
	ident, ok := taskCaptureProcIdentity(live.Pid())
	if !ok {
		t.Fatalf("pid %d just started, so its identity must be readable", live.Pid())
	}

	wt := t.TempDir() // a REAL directory: the state ladder's worktree hatch
	const taskID = "t-holderrecycled-bin"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "recycled holder fixture", Payload: "in flight", TaskID: taskID, Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	tdb := openTestTasksDB(t, memoryDBPath())
	if _, err := tdb.ExecContext(context.Background(),
		`UPDATE tasks SET deliverable = ? WHERE task_id = ?`,
		`{"repo":"/tmp/fixture-repo","branch":"task/fixture","worktree":"`+wt+`"}`, taskID); err != nil {
		tdb.Close() //stylegate:ignore the fixture DB is being abandoned on a fatal path; t.Fatalf carries the real cause
		t.Fatalf("deliverable: %v", err)
	}
	if _, err := tdb.ExecContext(context.Background(),
		`UPDATE tasks SET status=?, claimed_by=?, claimed_at=?, `+
			tasksInProgressAtColumnName+`=?, updated_at=?, `+
			tasksProcPidColumnName+`=?, `+tasksProcStartColumnName+`=?, `+
			tasksProcCmdColumnName+`=? WHERE task_id=?`,
		taskStatusInProgress, claimStampForTest(ident.Pid), 1000.0, 1000.0, 1000.0,
		ident.Pid, ident.Start+1, ident.Cmd, taskID); err != nil {
		tdb.Close() //stylegate:ignore the fixture DB is being abandoned on a fatal path; t.Fatalf carries the real cause
		t.Fatalf("stamp: %v", err)
	}
	tdb.Close() //stylegate:ignore the fixture's own handle at the end of its setup; the DB is read back by a child process

	env := []string{
		"GOPI_MEMORY_DB=" + db,
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"LANG=C",
	}
	want := fmt.Sprintf(taskHolderRecycledMarkerFmt, ident.Pid)

	listCmd := exec.Command(bin, "task", "list", "--status", taskStatusInProgress)
	listCmd.Env = env
	list, err := listCmd.Output()
	if err != nil {
		t.Fatalf("task list: %v\n%s", err, list)
	}
	var line string
	for _, ln := range strings.Split(string(list), "\n") {
		if strings.Contains(ln, taskID) {
			line = ln
			break
		}
	}
	if line == "" {
		t.Fatalf("row %s not rendered at all:\n%s", taskID, list)
	}
	if !strings.Contains(line, want) {
		t.Errorf("the list line must carry %q; its line:\n%s", want, line)
	}
	if !strings.Contains(line, taskStateUnknown) {
		t.Errorf("precondition: this row must hit the worktree hatch (%q) that hides the death; its line:\n%s",
			taskStateUnknown, line)
	}

	showCmd := exec.Command(bin, "task", "show", taskID)
	showCmd.Env = env
	show, err := showCmd.Output()
	if err != nil {
		t.Fatalf("task show: %v\n%s", err, show)
	}
	if !strings.Contains(string(show), want) {
		t.Errorf("`task show` must carry %q; output:\n%s", want, show)
	}
}
