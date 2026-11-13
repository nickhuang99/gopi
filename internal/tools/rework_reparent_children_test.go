package tools

// rework_reparent_children_test.go — the RED PROOF for Rule 33's supersession
// edge, driven through the REAL surfaces: the claim funnel (TaskNextFiltered —
// what a dispatcher calls) and TaskReparent (the ONE writer of
// parent_task_id).
//
// The epic's red proof is a PAIR on one fixture:
//
//   BEFORE — the superseded row A is completed, its child B is claimable: the
//            funnel HANDS IT OUT (`[next]`), because `completed` is in
//            taskParentReleaseStatuses. The child starts on a premise the
//            rework just falsified.
//   AFTER  — the rework row R exists and the child's edge moved A → R: the
//            funnel REFUSES B again (parked on R, which is pending), while an
//            unrelated row is still offered — a park, never a head-block.
//
// The release set and taskParentGateDenied are NOT touched by this change:
// TestParentGateCompletedParentReleasesChild (task_parent_gate_test.go) stays
// green beside these, which is the guard that the fix moved the EDGE instead
// of tightening the GATE.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// reworkReparentOtherTag marks fixture rows the funnel is never asked to skip.
const reworkReparentOtherTag = "rework-reparent-fixture"

// reworkReparentFixture submits A (parent) with a child B, walks A to
// `completed` through the ordinary ladder, and returns the ids.
func reworkReparentFixture(t *testing.T) (parentID, childID string) {
	t.Helper()
	parentGateDB(t)
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "superseded row", Payload: "the row whose work is being redone",
		Tags: parentGateParentTag, Source: "auto",
	})
	if err != nil {
		t.Fatalf("submit superseded row: %v", err)
	}
	parentID = taskIDFromSubmitOutput(t, out)
	childID = parentGateChild(t, parentID, "child of the superseded row")
	releaseParent(t, parentID)
	return parentID, childID
}

// reworkReparentRow submits the branch-rework row R (a plain pending row; the
// review only needs its id and its status).
func reworkReparentRow(t *testing.T) string {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "rework row", Payload: "the row that now carries the work", Source: "auto",
	})
	if err != nil {
		t.Fatalf("submit rework row: %v", err)
	}
	return taskIDFromSubmitOutput(t, out)
}

// TestReworkReparentChildIsReleasedUntilTheEdgeMoves is the epic's red proof,
// both halves on ONE fixture: a child is CLAIMABLE while its parent is
// completed (the defect this change exists for), and is PARKED again once the
// edge points at the pending rework row. The child the BEFORE half claims is
// RESERVED for that half (the funnel consumes it), so the AFTER half reads a
// second, untouched child of the same superseded row.
func TestReworkReparentChildIsReleasedUntilTheEdgeMoves(t *testing.T) {
	parentID, claimedChild := reworkReparentFixture(t)
	parkedChild := parentGateChild(t, parentID, "second child of the superseded row")

	// BEFORE: `completed` releases the child — this is the release set doing
	// exactly what it is specified to do, and it is why the EDGE has to move.
	if got := claimNext(t, "rework-reparent-test"); got != claimedChild {
		t.Fatalf("red-proof half one: a completed parent must release the child (that IS the defect): want %s, got %q", claimedChild, got)
	}

	// The unrelated row is submitted AFTER, so a FIFO reading would reach the
	// children first — the assertion below is about eligibility, not ordering.
	unrelatedOut, err := TaskSubmit(TaskSubmitOptions{Summary: "unrelated row", Payload: "unrelated briefing", Source: "auto"})
	if err != nil {
		t.Fatalf("submit unrelated: %v", err)
	}
	unrelatedID := taskIDFromSubmitOutput(t, unrelatedOut)

	reworkID := reworkReparentRow(t)
	rep := ReworkReparentChildren(parentID, reworkID, "rework supersession: test fixture")
	if len(rep.Moved) != 2 || rep.Moved[0] != claimedChild || rep.Moved[1] != parkedChild {
		t.Fatalf("both children must be re-pointed at the rework row: %+v", rep)
	}
	if rep.Rework != reworkID || rep.Reworked != parentID {
		t.Fatalf("the report must name both ends: %+v", rep)
	}

	// AFTER: the edge moved, so the funnel refuses the child again — naming
	// the REWORK row as the unreleased parent — and still offers the
	// unrelated row.
	for _, id := range []string{claimedChild, parkedChild} {
		if row := taskRowByID(t, id); strings.TrimSpace(row.ParentTaskID) != reworkID {
			t.Fatalf("child %s: parent_task_id must be %s after the review, got %q", id, reworkID, row.ParentTaskID)
		}
	}
	if got := claimNext(t, "rework-reparent-test"); got != unrelatedID {
		t.Fatalf("the funnel must skip the re-parked child and offer the unrelated row: want %s, got %q", unrelatedID, got)
	}
	// The child must NEVER come out of the funnel again. The rework row
	// ITSELF is pending and unparented, so the funnel legitimately offers it
	// next — the assertion is about the children, not about an empty queue.
	for i := 0; i < 4; i++ {
		got := claimNext(t, "rework-reparent-test")
		if got == claimedChild || got == parkedChild {
			t.Fatalf("a re-parked child must never be handed out while the rework row is pending (claim %d took %s)", i, got)
		}
		if got == "" {
			break
		}
	}
	// The pending view is the operator's reading surface: it must SAY why.
	list, err := TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(list, "#"+parkedChild) {
		t.Fatalf("the pending view must render the parked child %s:\n%s", parkedChild, list)
	}
	// The operator's reading surface must SAY which parent parked it and which
	// statuses would release it — the sentence is DERIVED from the release set,
	// so this asserts the marker AND the reason, not an exact string.
	wantChild := "#" + parkedChild
	var childLine string
	for _, ln := range strings.Split(list, "\n") {
		if strings.HasPrefix(ln, wantChild) {
			childLine = ln
		}
	}
	if childLine == "" {
		t.Fatalf("no rendered line for the parked child %s:\n%s", parkedChild, list)
	}
	if !strings.Contains(childLine, TaskQueueSkipMarker) {
		t.Fatalf("the parked child must carry %q:\n%s", TaskQueueSkipMarker, childLine)
	}
	if !strings.Contains(childLine, reworkID) || !strings.Contains(childLine, taskParentReleaseSetLabel()) {
		t.Fatalf("the parked child's line must name the REWORK row %s and the release set:\n%s", reworkID, childLine)
	}
}

// TestReworkReparentIsIdempotent pins the re-run contract: a second review of
// the same pair writes NOTHING — no edge move and (through TaskReparent's own
// unchanged arm) no trail entry either.
func TestReworkReparentIsIdempotent(t *testing.T) {
	parentID, childID := reworkReparentFixture(t)
	reworkID := reworkReparentRow(t)

	first := ReworkReparentChildren(parentID, reworkID, "rework supersession: idempotence")
	if len(first.Moved) != 1 {
		t.Fatalf("first review must move the child: %+v", first)
	}
	before := countReparentTrail(t, childID)

	second := ReworkReparentChildren(parentID, reworkID, "rework supersession: idempotence")
	if second.Children() != 0 || len(second.Moved) != 0 {
		t.Fatalf("the second review must find nothing left to move — the child no longer holds an edge on the superseded row: %+v", second)
	}
	if after := countReparentTrail(t, childID); after != before {
		t.Fatalf("an idempotent review must add no trail entry: before=%d after=%d", before, after)
	}
	if row := taskRowByID(t, childID); strings.TrimSpace(row.ParentTaskID) != reworkID {
		t.Fatalf("the edge must be unchanged by the second review, got %q", row.ParentTaskID)
	}
}

// TestReworkReparentLeavesNonLiveChildrenNamed pins the two arms that
// DELIBERATELY move nothing: a terminal child (no work is owed through the
// edge) and a child whose work was HANDED OFF to a peer (the local edge is
// moot). Both are counted and named — never silently folded into "there was
// nothing to do".
func TestReworkReparentLeavesNonLiveChildrenNamed(t *testing.T) {
	parentGateDB(t)
	parentOut, err := TaskSubmit(TaskSubmitOptions{Summary: "superseded row", Payload: "p", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	parentID := taskIDFromSubmitOutput(t, parentOut)

	terminalChild := parentGateChild(t, parentID, "already finished child")
	releaseParent(t, terminalChild) // pending → claimed → in_progress → completed

	handedOffChild := parentGateChild(t, parentID, "child whose work moved to a peer")
	if _, err := TaskUpdate(handedOffChild, TaskStatusClaimed); err != nil {
		t.Fatalf("claim handed-off child: %v", err)
	}
	if _, err := taskAddTag(context.Background(), mustOpenLedger(t), handedOffChild, reworkHandoffTag); err != nil {
		t.Fatalf("stamp handoff marker: %v", err)
	}

	liveChild := parentGateChild(t, parentID, "child still waiting on the work")

	// The superseded row did NOT complete in this fixture, so its children are
	// parked on it; that is the ordinary state and the review still has to
	// judge each child.
	reworkID := reworkReparentRow(t)
	rep := ReworkReparentChildren(parentID, reworkID, "rework supersession: non-live children")

	if len(rep.Moved) != 1 || rep.Moved[0] != liveChild {
		t.Fatalf("exactly the live child must move: %+v", rep)
	}
	if len(rep.Terminal) != 1 || rep.Terminal[0] != terminalChild {
		t.Fatalf("the terminal child must be counted, not moved: %+v", rep)
	}
	if len(rep.HandedOff) != 1 || rep.HandedOff[0] != handedOffChild {
		t.Fatalf("the handed-off child must be counted by its own arm: %+v", rep)
	}
	for _, id := range []string{terminalChild, handedOffChild} {
		if row := taskRowByID(t, id); strings.TrimSpace(row.ParentTaskID) != parentID {
			t.Fatalf("child %s must keep its edge on %s, got %q", id, parentID, row.ParentTaskID)
		}
	}
	joined := rep.Render() + "\n" + strings.Join(rep.Notes, "\n")
	if !strings.Contains(joined, reworkHandoffTag) {
		t.Fatalf("the cross-host disposition must NAME the marker it declined on:\n%s", joined)
	}
	if !strings.Contains(joined, terminalChild) || !strings.Contains(joined, handedOffChild) {
		t.Fatalf("every declined child must be named in the report:\n%s", joined)
	}
}

// TestReworkReparentRefusesAnUnnamedOrSelfEdge pins the zero-write refusals:
// an unpair-able call does nothing and SAYS what was missing.
func TestReworkReparentRefusesAnUnnamedOrSelfEdge(t *testing.T) {
	parentGateDB(t)
	reworkID := reworkReparentRow(t)

	for _, tc := range []struct {
		name             string
		reworked, rework string
	}{
		{"no superseded row", "", reworkID},
		{"no rework row", reworkID, ""},
		{"same row on both ends", reworkID, reworkID},
	} {
		rep := ReworkReparentChildren(tc.reworked, tc.rework, "reason")
		if rep.Refused == "" {
			t.Errorf("%s: must be refused by name, got %+v", tc.name, rep)
		}
		if rep.Children() != 0 || len(rep.Moved) != 0 {
			t.Errorf("%s: a refusal must move nothing, got %+v", tc.name, rep)
		}
	}
	// A reason is required by the WRITER, so the review refuses before it
	// reaches it — a reparent with no reason is what the ledger's append-only
	// rule exists to forbid.
	if rep := ReworkReparentChildren("t-a", reworkID, "   "); rep.Refused == "" {
		t.Fatalf("a reasonless review must be refused: %+v", rep)
	}
}

// TestReworkReparentRefusesACycleByName pins the DAG guard reaching this
// review: when the edge move would close a ring, the CHILD's move fails, the
// failure is recorded by name, and every other child still moves (one
// pathological child must not leave the rest released on the falsified
// premise).
func TestReworkReparentRefusesACycleByName(t *testing.T) {
	parentGateDB(t)
	parentOut, err := TaskSubmit(TaskSubmitOptions{Summary: "superseded row", Payload: "p", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	parentID := taskIDFromSubmitOutput(t, parentOut)
	ordinaryChild := parentGateChild(t, parentID, "an ordinary child")
	ringChild := parentGateChild(t, parentID, "the child the rework row descends from")

	// The rework row is a DESCENDANT of ringChild: moving ringChild's edge onto
	// it would close a cycle (ringChild → rework → … → ringChild).
	reworkOut, err := TaskSubmit(TaskSubmitOptions{
		Summary: "rework row", Payload: "r", Source: "auto", ParentTaskID: ringChild,
	})
	if err != nil {
		t.Fatalf("submit rework row: %v", err)
	}
	reworkID := taskIDFromSubmitOutput(t, reworkOut)

	rep := ReworkReparentChildren(parentID, reworkID, "rework supersession: ring fixture")
	if len(rep.Moved) != 1 || rep.Moved[0] != ordinaryChild {
		t.Fatalf("the ordinary child must still move: %+v", rep)
	}
	if len(rep.Failed) != 1 || rep.Failed[0] != ringChild {
		t.Fatalf("the ring-closing child must be recorded as FAILED, not skipped: %+v", rep)
	}
	if row := taskRowByID(t, ringChild); strings.TrimSpace(row.ParentTaskID) != parentID {
		t.Fatalf("the refused move must leave the edge alone, got %q", row.ParentTaskID)
	}
	if !strings.Contains(strings.Join(rep.Notes, "\n"), "cycle") {
		t.Fatalf("the refusal must carry the writer's own reason (a cycle):\n%s", strings.Join(rep.Notes, "\n"))
	}
}

// TestReworkReparentMovesAChildAlreadyInFlight pins the IN-FLIGHT arm: a child
// that is claimed / in_progress is ALSO re-pointed (and recorded). Leaving it
// on the superseded row would keep the lie in the ledger: it is already running
// on the premise the rework just falsified, and the only arm that ever repairs
// that is the edge it will read the next time anything about it is judged.
func TestReworkReparentMovesAChildAlreadyInFlight(t *testing.T) {
	parentGateDB(t)
	parentOut, err := TaskSubmit(TaskSubmitOptions{Summary: "superseded row", Payload: "p", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	parentID := taskIDFromSubmitOutput(t, parentOut)

	claimedChild := parentGateChild(t, parentID, "child already claimed")
	if _, err := TaskUpdate(claimedChild, TaskStatusClaimed); err != nil {
		t.Fatalf("claim child: %v", err)
	}
	runningChild := parentGateChild(t, parentID, "child already in progress")
	if _, err := TaskUpdate(runningChild, TaskStatusClaimed); err != nil {
		t.Fatalf("claim child: %v", err)
	}
	if _, err := TaskUpdate(runningChild, TaskStatusInProgress); err != nil {
		t.Fatalf("start child: %v", err)
	}

	reworkID := reworkReparentRow(t)
	rep := ReworkReparentChildren(parentID, reworkID, "rework supersession: in-flight children")
	if len(rep.Moved) != 2 {
		t.Fatalf("both in-flight children must move (they are running on the falsified premise): %+v", rep)
	}
	if len(rep.Failed) != 0 || len(rep.Terminal) != 0 {
		t.Fatalf("an in-flight child is not a terminal one: %+v", rep)
	}
	for _, id := range []string{claimedChild, runningChild} {
		if row := taskRowByID(t, id); strings.TrimSpace(row.ParentTaskID) != reworkID {
			t.Fatalf("in-flight child %s: parent_task_id must be %s, got %q", id, reworkID, row.ParentTaskID)
		}
		// The move is RECORDED (Rule 15): the append-only trail says why.
		if n := countReparentTrail(t, id); n != 1 {
			t.Fatalf("in-flight child %s: the move must leave exactly one trail entry, got %d", id, n)
		}
	}
}

// TestReworkReparentFromBranchPicksTheOriginalOwnerNotTheReworkRow pins the
// bridge's exclusion: the rework lane stamps its fresh row with the branch
// BEFORE the review runs, so that row is itself a LIVE owner — and a LIVE
// owner is what TaskForBranchResolvedRow prefers. Without excluding it the
// "original row" would be the rework row and the review would compare R with
// R.
func TestReworkReparentFromBranchPicksTheOriginalOwnerNotTheReworkRow(t *testing.T) {
	parentGateDB(t)
	const repoPath = "/tmp/rework-reparent-fixture-repo"
	const branch = "task/01a11fcaeec270498198582ef59a9511"

	original, childID := submitWithDeliverable(t, "the branch's original row", repoPath, branch)
	releaseParent(t, original)
	if got := claimNext(t, "bridge-test"); got != childID {
		t.Fatalf("the fixture must start red (the completed owner releases the child): got %q", got)
	}

	rework := reworkReparentRow(t)
	// What resolveRowStampDeliverableBranch does at every call site: the fresh
	// row is branch-stamped BEFORE the review, so it owns the branch too.
	if err := MergeVerifySetDeliverableBranchForTest(rework, repoPath, branch); err != nil {
		t.Fatalf("stamp the rework row: %v", err)
	}

	rep := reworkReparentFromBranch(repoPath, branch, rework, reworkLaneName)
	if rep.Refused != "" {
		t.Fatalf("the bridge must resolve an owner, got a refusal: %s", rep.Refused)
	}
	if rep.Reworked != original || rep.Rework != rework {
		t.Fatalf("the bridge must supersede the ORIGINAL row, never the rework row: %+v", rep)
	}
	if len(rep.Moved) != 1 || rep.Moved[0] != childID {
		t.Fatalf("the child must move onto the rework row: %+v", rep)
	}
	if row := taskRowByID(t, childID); strings.TrimSpace(row.ParentTaskID) != rework {
		t.Fatalf("child's parent_task_id must be %s, got %q", rework, row.ParentTaskID)
	}
}

// TestReworkReparentFromBranchSaysWhenThereIsNothingToMove pins the arms that
// are NOT silent: no owner for the branch, and a mirrored submit (no local
// row) each produce their own sentence.
func TestReworkReparentFromBranchSaysWhenThereIsNothingToMove(t *testing.T) {
	parentGateDB(t)

	if rep := reworkReparentFromBranch("/tmp/no-such-repo", "task/no-such-branch", "t-whatever", reworkLaneName); rep.Refused == "" {
		t.Fatalf("a branch no row owns must be refused BY NAME, got %+v", rep)
	}
	if rep := reworkReparentFromBranch("/tmp/no-such-repo", "task/no-such-branch", "", reworkLaneName); rep.Refused == "" {
		t.Fatalf("a mirrored submit (no local row) must say so, got %+v", rep)
	}
	if rep := reworkReparentFromBranch("", "task/x", "t-y", reworkLaneName); rep.Refused == "" {
		t.Fatalf("a missing repo must be refused, got %+v", rep)
	}
}

// --- fixtures ---------------------------------------------------------

// submitWithDeliverable submits a row and stamps a (repo, branch) deliverable
// blob onto it. The stamp is written through the ledger seam rather than
// through TaskSubmit's repo verification, because the fixture repo is a NAME
// the branch lookup compares as a string — no git tree has to exist for the
// review under test (the lookup's git channel is only consulted when the
// ledger names NO owner, and here it names two).
func submitWithDeliverable(t *testing.T, summary, repo, branch string) (taskID, childID string) {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{Summary: summary, Payload: "p", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit %s: %v", summary, err)
	}
	taskID = taskIDFromSubmitOutput(t, out)
	if err := MergeVerifySetDeliverableBranchForTest(taskID, repo, branch); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
	childID = parentGateChild(t, taskID, "child of "+summary)
	return taskID, childID
}

// mustOpenLedger hands back the test-profile ledger handle for the helpers
// that take a *sql.DB.
func mustOpenLedger(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() }) //stylegate:ignore closing a read handle in cleanup cannot fail the test
	return db
}

// countReparentTrail counts the append-only trail entries one row carries —
// the instrument the idempotence test reads (a second review must add NONE).
func countReparentTrail(t *testing.T, taskID string) int {
	t.Helper()
	var n int
	if err := mustOpenLedger(t).QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event = ?`,
		taskID, ReparentEventName).Scan(&n); err != nil {
		t.Fatalf("count trail: %v", err)
	}
	return n
}
