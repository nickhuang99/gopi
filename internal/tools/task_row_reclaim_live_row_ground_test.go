package tools

// Regression guards for the 2026-10-12 incident (rows 01a130f0 / 01a130ee /
// 01a130e4): the task-row-reclaim removal arm freed the worktree ground of ten
// rows, three of which the ledger still reads `in_progress` — its own audit
// record named the disposition `reclaimFreable past the live-session-veto
// channel`.
//
// THE DEFECT, in one line: "the row's holder is provably DEAD" was consumed as
// "the row is finished". They are different claims. A live status means no run
// of that row ever wrote a completion (the completion IS the status
// transition); all the pid layer proves is that whoever held it stopped.
//
// WHAT IT COST (measured on the reporting host): the directory was freed at
// 16:28–16:39 and the row stayed non-terminal, so `gopi worker --resume` could
// not take it up in ANY of its three spellings — the default positional target
// resolves the row's recorded worktree, and --branch/--worktree name the ground
// outright. Ground gone + branch deleted + directory absent = a row that is
// neither finished nor resumable nor re-dispatchable, i.e. permanently open.
// Nothing else on the host ends it either: the stale-report grace (1800s by
// default) only REPORTS the row, and since 2026-10-26 no disposer exists at all
// (devdoc/zombie-sweep-removed.md), so a reclaim minutes after the claim leaves
// the row live with its ground gone — a state nothing will ever clear.
//
// THE FIX is CO-DEATH, not refusal: the pass ENDS the attempt (the row moves to
// the terminal `failed`, written by the ledger's one mutation core,
// TaskUpdateReason) and frees the ground in the same act — or it does neither
// (the write gates the removal; an unclosable row keeps its directory).
// Refusing outright was REJECTED because releasing an orphaned dead-holder row's
// directory is the deliberate, tested contract of the 2026-09-12 orphan epic
// (TestDeadHolderReclaimFreesALiveStatusOrphan,
// TestAcceptance_DeadLocalHolderOrphanReleasesItsDirectory, and the acceptance
// chain that ends with safe-merge exiting 0) — those three still pass
// unchanged, which is the point.
//
// WHY `failed` AND NOT A RESUME STATE: the dispatcher's table
// (taskAllowedTransitions) has NO path from a live status back to a resumable
// one — `in_progress` leaves only for terminal, and the ledger's own word for
// "this attempt ended, the work must run again" is `failed` plus a REISSUED
// pending row (append-only: "the rework itself is a NEW pending row"). `failed`
// is the one terminal state that both closes the row and makes re-dispatch
// reachable; a row left live with no ground makes neither true.
//
// TWO invariants these tests pin (both were false before the fix):
//
//	① the ground IS freed (the orphan epic's whole point — safe-merge gate [c]
//	  must unblock), the ROW is closed in the same pass, and the BRANCH survives
//	  as the deliverable;
//	② the removal arm can never leave a live-status row behind without its
//	  ground (no arm may destroy a ground it cannot also close the row out with)
//	  — the ledger-wide invariant that makes "ground gone, row still open"
//	  unreachable, which is exactly the state the incident produced.

import (
	"os"
	"strings"
	"testing"
)

// reclaimLiveRowGroundFixture builds the incident's shape: a real repo, a real
// clean linked worktree on a feature branch, and a row whose STATUS IS LIVE
// while its claim stamp resolves on this host to a provably dead pid.
//
// The status is the variable under test, so the fixture takes it: `pending` /
// `claimed` / `in_progress` are the three the ledger calls live, and each must
// be closed before its ground is freed.
func reclaimLiveRowGroundFixture(t *testing.T, taskID, status string) (repo, wt, branch string) {
	t.Helper()
	repo, wt, branch = deadHolderReclaimFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "row reclaim: live-status row's ground", Payload: "p", TaskID: taskID, Source: "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	// A provably dead holder: a real child, killed and REAPED (deadPID), so the
	// refusal/free rests on a measured death rather than a chosen constant.
	seedDeadHolderRow(t, taskID, status, repo, branch, wt, hostPid(t, deadPID(t)))
	return repo, wt, branch
}

// TestRowReclaimCoDeathFreesTheGroundAndClosesTheRow is the incident itself.
// The shape is the failing one — an `in_progress` row whose holder is provably
// dead — and the assertion is the incident's exact opposite: the ground is freed
// (the orphan epic's job), the row is CLOSED (what the incident omitted), and
// the branch survives.
func TestRowReclaimCoDeathFreesTheGroundAndClosesTheRow(t *testing.T) {
	const taskID = "t-live-row-ground"
	repo, wt, branch := reclaimLiveRowGroundFixture(t, taskID, taskStatusInProgress)
	freshReclaimRefusalMemo(t)

	// The preconditions, measured: the directory exists and the row is live
	// BEFORE the pass, so both changes afterwards are attributable to the pass.
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("fixture: the ground must exist before the pass: %v", err)
	}
	if !worktreeListed(t, repo, wt) {
		t.Fatalf("fixture: the worktree must be registered before the pass: %s", worktreeList(t, repo))
	}
	if got := mustTaskStatus(t, taskID); got != taskStatusInProgress {
		t.Fatalf("fixture: the row must start live, got %q", got)
	}

	lines := ReclaimDeadHolderWorktrees(true)
	joined := strings.Join(lines, "\n")

	// ① the DIRECTORY is freed — this is the orphan epic's acceptance, and a
	// "fix" that refused here would have reversed it.
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("the orphan's ground must be freed (stat err=%v); pass lines:\n%s", err, joined)
	}
	// ② the ROW IS CLOSED in the same pass. This is the half the incident
	// omitted: a live row whose ground is gone cannot be resumed.
	if got := mustTaskStatus(t, taskID); got != taskStatusFailed {
		t.Fatalf("a dead holder's live-status row must be CLOSED by the same pass that frees its ground, got status %q"+
			" — a live row with no ground is neither finished nor resumable; pass lines:\n%s", got, joined)
	}
	// ③ the recorded WHY is readable (Rule 15: the operator must be able to see
	// who closed the row and on what evidence — `task show` renders the reason).
	if why := reclaimRowReason(t, taskID); !strings.Contains(why, "dead-holder reclaim") {
		t.Errorf("the close-out must carry the dead-holder WHY, got:\n%s", why)
	}
	// ④ the BRANCH survives: it is the deliverable, and this arm never mutates a
	// ref.
	if err := gitBranchExists(t, repo, branch); err != nil {
		t.Fatalf("the branch must survive the release: %v", err)
	}
	// ⑤ the pass reports the release and names the row.
	if !strings.Contains(joined, branch) || !strings.Contains(joined, taskID) {
		t.Errorf("the release must be NAMED and name the row, got:\n%s", joined)
	}
	// Converged: a second pass has nothing left to free.
	if again := ReclaimDeadHolderWorktrees(true); len(again) != 0 {
		t.Errorf("the second pass must find nothing (the row is terminal now, so it is not a candidate): %v", again)
	}
}

// TestRowReclaimClosesEveryLiveStatusItFrees drives all three live statuses
// through one real pass and pins the criterion as a CONDITIONAL that holds for
// every one of them: if the ground was freed, the row must have been closed
// first. It is stated that way on purpose — whether a given status is a
// CANDIDATE at all is the orphan arm's own design (finishCleanupDeadLocalHolder
// requires a claim stamp this host can resolve), and a test that demanded a
// release for a status the arm does not select would be asserting my guess about
// another criterion instead of this one.
//
// The `in_progress` case IS required to be freed: it is the incident's own
// status, and the orphan epic's contract.
func TestRowReclaimClosesEveryLiveStatusItFrees(t *testing.T) {
	inProgressFreed := false
	for _, status := range []string{taskStatusPending, taskStatusClaimed, taskStatusInProgress} {
		if !taskLiveHolderStatus(status) {
			t.Fatalf("precondition: %q must be a live status", status)
		}
		taskID := "t-live-status-" + status
		_, wt, _ := reclaimLiveRowGroundFixture(t, taskID, status)
		freshReclaimRefusalMemo(t)

		ReclaimDeadHolderWorktrees(true)

		_, statErr := os.Stat(wt)
		freed := os.IsNotExist(statErr)
		if status == taskStatusInProgress {
			inProgressFreed = freed
		}
		if !freed {
			continue // this arm did not select the row: nothing was freed, so nothing can be stranded
		}
		if got := mustTaskStatus(t, taskID); taskLiveHolderStatus(got) {
			t.Fatalf("%s: the row was left LIVE (%q) after its ground was freed — that is the incident", status, got)
		}
	}
	if !inProgressFreed {
		t.Fatal("the incident's own status (in_progress, dead holder) must still have its ground freed — " +
			"the 2026-09-12 orphan epic made that a deliberate, tested contract and this fix must not reverse it")
	}
}

// TestRowReclaimNeverStrandsALiveStatusRow is the co-death invariant at the
// ledger level, and it is the half that generalises past this pass's mechanism:
// the arm must never be able to leave a live-status row WITHOUT its ground.
//
// It does not assert on the arm's return lines (an assertion about what an arm
// SAYS can pass while the state it claims to protect is destroyed) — it measures
// the LEDGER against the FILESYSTEM, before and after a real removal pass, over
// every live-status row that records a worktree:
//
//	no live-status row may name a worktree that is MISSING while it named one
//	that EXISTED before the pass.
//
// The pre-fix arm fails this: it removed exactly such a directory while leaving
// the row live, so the row went from "ground present, live" to "ground gone,
// live" in one pass — the state no verb can resume out of and no local sweeper
// reaches at all (the stale report only prints; disposal was removed 2026-10-26).
func TestRowReclaimNeverStrandsALiveStatusRow(t *testing.T) {
	const taskID = "t-live-row-not-stranded"
	_, wt, _ := reclaimLiveRowGroundFixture(t, taskID, taskStatusInProgress)
	freshReclaimRefusalMemo(t)

	before, err := reclaimLiveRowGroundStates()
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	if !before[taskID].grounded {
		t.Fatalf("fixture: %s must be visible to the ledger scan as a live row with a ground", taskID)
	}

	ReclaimDeadHolderWorktrees(true)

	after, err := reclaimLiveRowGroundStates()
	if err != nil {
		t.Fatalf("re-read the ledger: %v", err)
	}
	for id, was := range before {
		if !was.grounded {
			continue // its ground was already gone before this pass: not this pass's doing
		}
		if !after[id].live {
			continue // the row was CLOSED by this pass — the ground is free to go, that is co-death
		}
		if !after[id].grounded {
			t.Fatalf("row %s is still NON-TERMINAL and its ground (%s) was destroyed by the removal pass — "+
				"'ground gone, row open' is unreachable by design: a resume needs the directory and no local "+
				"sweeper ends the row in time, so this row could never be taken up again", id, wt)
		}
	}
}

// reclaimRowGroundState is one row's measured state: is its status still live,
// and does its recorded ground exist on disk right now.
type reclaimRowGroundState struct {
	live     bool
	grounded bool
}

// reclaimLiveRowGroundStates reads the ledger once and reports, per row that
// records a worktree, whether its status is still LIVE and whether that
// directory EXISTS. It is the measurement the co-death invariant is stated
// against — the ledger's own rows against the filesystem, never an arm's own
// report of what it did.
//
// It reads through reclaimDeadHolderRows (the pass's OWN scan) so the set
// measured is the set the arm could act on: a helper with its own query would be
// a second reader that can drift from the arm it is auditing (Rule 14).
func reclaimLiveRowGroundStates() (map[string]reclaimRowGroundState, error) {
	rows, err := reclaimDeadHolderRows()
	if err != nil {
		return nil, err
	}
	out := map[string]reclaimRowGroundState{}
	for _, r := range rows {
		if strings.TrimSpace(r.Worktree) == "" {
			continue
		}
		_, statErr := os.Stat(r.Worktree)
		out[r.TaskID] = reclaimRowGroundState{
			live:     taskLiveHolderStatus(r.Status),
			grounded: statErr == nil,
		}
	}
	return out, nil
}

// reclaimRowReason reads the row's recorded reason — where the ONE mutation core
// (taskUpdateStampedFrom) writes the WHY it was handed, and what `task show`
// renders. Read through the SAME ledger resolver the arm wrote through, never a
// hand-rolled path (Rule 32/14).
func reclaimRowReason(t *testing.T, taskID string) string {
	t.Helper()
	db, err := openTasksDB(t.Context(), os.Getenv(memoryDBEnv))
	if err != nil {
		t.Fatalf("open the ledger: %v", err)
	}
	defer db.Close()
	var reason string
	if err := db.QueryRowContext(t.Context(),
		`SELECT COALESCE(reason,'') FROM tasks WHERE task_id = ?`, taskID).Scan(&reason); err != nil {
		t.Fatalf("read the row's reason: %v", err)
	}
	return reason
}

// mustTaskStatus reads one row's status from the ledger, failing loudly rather
// than returning a zero value a test could mistake for an assertion.
func mustTaskStatus(t *testing.T, taskID string) string {
	t.Helper()
	status, err := TaskStatusOf(taskID)
	if err != nil {
		t.Fatalf("read status of %s: %v", taskID, err)
	}
	return strings.TrimSpace(status)
}
