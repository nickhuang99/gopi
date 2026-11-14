package tools

// The GROUND-IN-USE regression guard (measured defect 2026-10-07, row
// 01a11561). The shape under test is the one the ledger CANNOT see:
//
//	a TERMINAL row (the resume path's ordinary target) whose ownership handoff
//	is structurally impossible — TaskStampSelfClaim is pinned
//	`WHERE status='in_progress'`, so on a `failed` row it matches ZERO rows —
//	while a live run is standing in the directory the row itself RECORDS as its
//	deliverable.worktree.
//
// RED BEFORE THE FIX, GREEN AFTER, and the fixture is built so the difference
// is exactly the fix:
//
//	pre-fix:  verdict n/a, holder (none), resumable YES — the ledger's answer,
//	          with a live process working in the row's own ground.
//	post-fix: the same verdict/holder facts, PLUS ground_in_use and
//	          resumable NO (TaskGroundInUse, task_ground_in_use.go).
//
// The live process is REAL (an `sleep` whose cwd is the ground), never a forged
// pid: a synthetic pid would prove nothing about /proc, which is the whole
// evidence channel here (Rule 24).

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// groundInUseHolderVisibleWait bounds the fork→visible-in-/proc window
	// before the fixture declares itself unable to measure (Rule 1: no bare
	// numbers).
	groundInUseHolderVisibleWait = 5 * time.Second
	// groundInUseHolderVisibleTick is the poll interval inside that window.
	groundInUseHolderVisibleTick = 10 * time.Millisecond
	// groundInUseStandInSeconds is how long the stand-in holder lives — far
	// past any assertion in this file, and it is killed explicitly by stop().
	groundInUseStandInSeconds = "300"
)

// groundInUseFixture seeds the shape under test: a row on the given status whose
// deliverable names ground as its worktree. It goes through PRODUCTION seams only
// — TaskSubmit creates the row, TaskUpdate moves the status, and
// TaskStampDeliverableGroundForTest writes the deliverable (the exported face of
// the executor's own backfill; the deliverable cannot be set through the submit
// API) — so the fixture cannot drift from the writers it stands in for.
func groundInUseFixture(t *testing.T, taskID, status, ground string) {
	t.Helper()
	testutil.NewTestDBPath(t) // pins GOPI_MEMORY_DB at a fresh per-test ledger

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "ground-in-use fixture " + taskID,
		Payload: "ground-in-use fixture",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	if err := TaskStampDeliverableGroundForTest(taskID, ground); err != nil {
		t.Fatalf("stamp %s's ground: %v", taskID, err)
	}
	if status != taskStatusPending {
		// The status machine is walked the way a real run walks it (Rule 31:
		// exercise the production writer, not a back door). pending → failed is
		// refused on purpose, so the fixture goes through in_progress — which is
		// also the transition that CLEARS the holder, i.e. exactly the shape
		// under test (a terminal row with no claim stamp).
		if _, err := TaskUpdate(taskID, taskStatusInProgress); err != nil {
			t.Fatalf("move %s to %s: %v", taskID, taskStatusInProgress, err)
		}
		if status != taskStatusInProgress {
			if _, err := TaskUpdate(taskID, status); err != nil {
				t.Fatalf("move %s to %s: %v", taskID, status, err)
			}
		}
	}
	if row := taskRowByID(t, taskID); row.Status != status {
		t.Fatalf("fixture row %s is %q, want %q", taskID, row.Status, status)
	}
}

// groundInUseLiveChild starts a REAL process whose cwd IS the ground, so the
// shared cwd scan sees a genuine holder. It returns a stop func that kills the
// process and WAITS for it, so the counter-case can prove the veto clears.
func groundInUseLiveChild(t *testing.T, ground string) func() {
	t.Helper()
	cmd := exec.Command("sleep", groundInUseStandInSeconds)
	// The cwd IS this fixture's subject, so it passes Dir explicitly — and arms
	// Pdeathsig so a SIGKILLed test binary cannot leave the holder standing in
	// the ground (measured leak path, task 01a179d7).
	cmd.Dir = ground
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn the stand-in holder: %v", err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Kill() //stylegate:ignore teardown of the helper this test spawned; no verdict depends on its exit status
		_, _ = cmd.Process.Wait()
	}
	t.Cleanup(stop)
	// The process must be OBSERVED alive in the ground before any assertion
	// runs. This is a HARD requirement, never a skip (a skip here would let the
	// whole test pass for the wrong reason — measured while red-proving: with
	// the criterion disabled this check self-skipped and the suite stayed
	// green). The poll reads the RAW /proc scan, not the criterion under test,
	// so the fixture's visibility is independent of the fix: in the red state
	// the fixture still confirms (the process IS standing there) and the
	// ASSERTION is what fails, which is the honest red proof.
	deadline := time.Now().Add(groundInUseHolderVisibleWait)
	for {
		if _, why := finishCleanupWorktreeInUse(ground); why != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("spawned a holder with cwd=%s but /proc does not show it there after %s — this fixture cannot measure what it claims to", ground, groundInUseHolderVisibleWait)
		}
		time.Sleep(groundInUseHolderVisibleTick) //stylegate:allow-sleep polls /proc for the spawned holder's cwd, which the kernel exposes a moment after fork
	}
	return stop
}

// TestGroundInUseTerminalRowIsNotResumable is the defect itself: a FAILED row
// whose recorded ground holds a live process must read resumable=no and carry
// the ground axis, while the ledger's own holder answer stays n/a (the two
// facts are different and both must be reported — Rule 15).
func TestGroundInUseTerminalRowIsNotResumable(t *testing.T) {
	ground := t.TempDir()
	const taskID = "t-ground-in-use"
	groundInUseFixture(t, taskID, taskStatusFailed, ground)
	groundInUseLiveChild(t, ground)

	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	}
	// The LEDGER half is unchanged and must stay unchanged: the row has no
	// holder (the terminal transition cleared it), so the identity verdict is
	// n/a. This half is a pin, not a bug.
	if st.Verdict != taskProcessStateNotApplicable {
		t.Errorf("verdict = %q, want %q (the ledger's own answer is unchanged by this fix)", st.Verdict, taskProcessStateNotApplicable)
	}
	if st.Holder != "" {
		t.Errorf("holder = %q, want empty (the fixture is the holderless terminal shape)", st.Holder)
	}
	// The GROUND half is the fix.
	if st.Ground != ground {
		t.Fatalf("ground = %q, want the row's recorded worktree %q", st.Ground, ground)
	}
	if !st.GroundInUse {
		t.Fatalf("ground_in_use = false while a live process stands in %s — the resume gate would admit a second run into a working directory", ground)
	}
	if st.GroundHolder == "" || st.GroundReason == "" {
		t.Errorf("the ground verdict must NAME the holder and the reason (Rule 15), got holder=%q reason=%q", st.GroundHolder, st.GroundReason)
	}
	if st.Resumable {
		t.Fatalf("resumable = true on a row whose recorded ground is in use — this is the defect")
	}
}

// TestGroundInUseClearsWhenTheHolderIsGone is the counter-case the fix must not
// break: once the process is gone, the ground is free and the terminal row is
// resumable again. Without it, "refuse whenever a worktree is recorded" would
// pass the test above and strand every ordinary recovery.
//
// The fixture is a `completed` row, not a `failed` one: since a failed attempt
// became a RESUME (the two-level policy, 2026-10-29), the status axis no longer
// refuses a failed row at all, so it could not demonstrate "the ground axis
// cleared and the STATUS axis is still what refuses". A completed row keeps
// both halves visible and is the same terminal shape the original test meant.
func TestGroundInUseClearsWhenTheHolderIsGone(t *testing.T) {
	ground := t.TempDir()
	const taskID = "t-ground-freed"
	groundInUseFixture(t, taskID, taskStatusCompleted, ground)
	stop := groundInUseLiveChild(t, ground)
	stop()

	if _, why := TaskGroundInUse(ground); why != "" {
		t.Fatalf("the ground still reads in use after the holder was reaped: %s", why)
	}
	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	}
	if st.GroundInUse {
		t.Fatalf("ground_in_use = true with nobody standing in %s", ground)
	}
	// THE GROUND AXIS cleared — that is this test's own subject, and it must.
	//
	// The ROW STILL READS unresumable, and that is the STATUS axis (added
	// 2026-10-16, TaskRunResumeStatusGate): a `completed` row's work is
	// finished, so no holder-free ground can reopen it for a resume. The two
	// refusals are distinct — a free ground with a finished status must not be
	// mistaken for "this row is now resumable". Recovery from a finished row
	// that needs work again is a COPY (`gopi task clone`), and the report says so.
	if st.StatusRefusal == "" {
		t.Fatalf("a completed row with a free ground carries no status refusal — the two axes must both be reported")
	}
	if st.Resumable {
		t.Fatalf("resumable = true on a COMPLETED row whose ground merely emptied — the status axis must still refuse")
	}
}

// TestGroundInUseIgnoresAVanishedGround pins the explicit absence: a row whose
// recorded directory no longer exists has no occupation to report, and must not
// read "in use" from a path it cannot even stat.
func TestGroundInUseIgnoresAVanishedGround(t *testing.T) {
	holder, reason := TaskGroundInUse(filepath.Join(t.TempDir(), "gone"))
	if holder != taskGroundInUseNoHolder || reason != taskGroundInUseNoHolder {
		t.Fatalf("a vanished ground answered holder=%q reason=%q, want the explicit absence", holder, reason)
	}
}

// TestGroundInUseWireKeepsTheFalse pins the machine contract (Rule 15a): false is
// the SEMANTIC value ("the ground is free — a resume may proceed"), so it must
// travel. It was MEASURED missing on the wire 2026-10-07 — the field carried
// `omitempty`, so a free ground answered `resumable: true` with no
// `ground_in_use` key at all, and a machine consumer could not tell "free" from
// "this build does not answer that question". A required value that IS the zero
// value gets a round-trip test, which is this one.
func TestGroundInUseWireKeepsTheFalse(t *testing.T) {
	free := &TaskProcessStatus{Ground: "/somewhere", Resumable: true}
	blob, err := TaskProcessStatusJSON(free)
	if err != nil {
		t.Fatalf("encode a free ground: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(blob), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	val, present := got["ground_in_use"]
	if !present {
		t.Fatalf("the wire omits ground_in_use when the ground is FREE — the answer a consumer asked for is gone: %s", blob)
	}
	if val != false {
		t.Fatalf("ground_in_use = %v on a free ground, want false", val)
	}

	used := &TaskProcessStatus{Ground: "/somewhere", GroundInUse: true, GroundHolder: "pid 1 (sleep)"}
	blob, err = TaskProcessStatusJSON(used)
	if err != nil {
		t.Fatalf("encode an occupied ground: %v", err)
	}
	if err := json.Unmarshal([]byte(blob), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["ground_in_use"] != true {
		t.Fatalf("ground_in_use = %v on an occupied ground, want true", got["ground_in_use"])
	}
}
