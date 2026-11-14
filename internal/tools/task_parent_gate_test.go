package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// Parent-task dependency GATE (D5): parent_task_id stops being a lineage
// note and becomes an eligibility condition — a pending row is claimable iff
// its parent_task_id is empty OR the parent row's status ∈
// taskParentReleaseStatuses {completed, verified, superseded}. The check lives
// in the ONE shared predicate taskRowClaimable, so the claim funnel
// (TaskNextFiltered) and the pending view's [queue-skip] marker answer
// identically.
//
// `superseded` joined the release set 2026-10-07 (epic
// parent-release-statuses): a row whose work is done ELSEWHERE — migrated to a
// peer ledger's row, or superseded by a successor — is a SATISFIED dependency,
// while `cancelled` (abandoned) keeps parking. The two directions are pinned
// below by tests that run the REAL funnel; the reasoning is
// devdoc/task-parent-release-statuses.md.
//
// These tests drive the REAL entry points (TaskNextFiltered — the funnel
// every dispatcher shares — and TaskList — the operator's reading surface),
// not the predicate in isolation: the epic's red proof is "the funnel refuses
// the child while offering an unrelated row".

// parentGateDB pins a fresh per-test ledger (Rule 32: the scaffold owns the
// path and the environment; no hand-rolled path, no operator-profile write).
func parentGateDB(t *testing.T) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
}

// parentGateChild submits a child row parented to parentID and returns its
// task_id.
func parentGateChild(t *testing.T, parentID, summary string) string {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:      summary,
		Payload:      "briefing for " + summary,
		ParentTaskID: parentID,
		Source:       "auto",
	})
	if err != nil {
		t.Fatalf("submit child %s: %v", summary, err)
	}
	return taskIDFromSubmitOutput(t, out)
}

// taskIDFromSubmitOutput pulls the minted task_id out of a submit result.
// TaskSubmit returns human text ("Task submitted: <id> ..."); the id is the
// last whitespace-free token that the ledger answers to, so ask the ledger
// (never a regex over prose) — the one honest way to read it.
func taskIDFromSubmitOutput(t *testing.T, out string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	for _, f := range strings.Fields(out) {
		tok := strings.Trim(f, ".,:;()\"'")
		if tok == "" {
			continue
		}
		row, err := taskByID(ctx, db, tok)
		if err != nil {
			t.Fatalf("taskByID %q: %v", tok, err)
		}
		if row != nil {
			return row.TaskID
		}
	}
	t.Fatalf("no ledger row named in submit output: %q", out)
	return ""
}

// parentGateParentTag marks the epic-parent row in these fixtures. The parent
// is a PENDING row too, so a dispatcher whose claim order reaches it would
// offer IT first (correctly — it is ordinary work); the funnel under test is
// therefore invoked with this tag skipped, exactly as a real dispatcher does
// with `worker --skip-tag`. That keeps every assertion about the CHILD's
// eligibility instead of about the parent's presence in the queue.
const parentGateParentTag = "proofparent"

// claimNext claims through the REAL funnel — with the fixture parent's tag
// skipped — and returns the claimed id ("" on an empty queue).
func claimNext(t *testing.T, claimedBy string) string {
	t.Helper()
	res, err := TaskNextFiltered(claimedBy, "", parentGateParentTag)
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res.Empty {
		return ""
	}
	return res.Task.TaskID
}

// releaseParent walks a parent row to a RELEASE status through the ordinary
// transition ladder (pending → claimed → in_progress → completed), so the
// test never fabricates a status the ledger's own gate would refuse.
func releaseParent(t *testing.T, parentID string) {
	t.Helper()
	for _, st := range []string{TaskStatusClaimed, TaskStatusInProgress, TaskStatusCompleted} {
		if _, err := TaskUpdate(parentID, st); err != nil {
			t.Fatalf("release parent %s → %s: %v", parentID, st, err)
		}
	}
}

// TestParentGatePendingParentParksChild is the RED PROOF half one: with the
// parent still pending, the funnel REFUSES the child and still OFFERS the
// unrelated row — the gate parks a row, it does not empty the queue behind
// it (the head-block failure mode t-1788053263 measured for another filter).
func TestParentGatePendingParentParksChild(t *testing.T) {
	parentGateDB(t)

	parentOut, err := TaskSubmit(TaskSubmitOptions{Summary: "epic parent", Payload: "parent briefing", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	parentID := taskIDFromSubmitOutput(t, parentOut)
	childID := parentGateChild(t, parentID, "child of a pending parent")

	// The unrelated row is submitted AFTER the child, so a FIFO reading of
	// the queue would reach the child first — which makes this assertion
	// about eligibility, not about ordering.
	unrelatedOut, err := TaskSubmit(TaskSubmitOptions{Summary: "unrelated row", Payload: "unrelated briefing", Source: "auto"})
	if err != nil {
		t.Fatalf("submit unrelated: %v", err)
	}
	unrelatedID := taskIDFromSubmitOutput(t, unrelatedOut)

	if got := claimNext(t, "parent-gate-test"); got != unrelatedID {
		t.Fatalf("funnel must skip the parked child and offer the unrelated row: want %s, got %q", unrelatedID, got)
	}
	if got := claimNext(t, "parent-gate-test"); got != "" {
		t.Fatalf("the parked child must remain unclaimable while its parent is pending: got %q", got)
	}
	// The child is parked, NOT consumed: it is still pending in the ledger.
	if row := taskRowByID(t, childID); row.Status != taskStatusPending {
		t.Fatalf("parked child must stay pending, got status=%s", row.Status)
	}
}

// TestParentGateCompletedParentReleasesChild is the RED PROOF half two: the
// SAME fixture, with the parent flipped to a release status, hands the child
// to the very next claim — the gate is a release, not a permanent block.
func TestParentGateCompletedParentReleasesChild(t *testing.T) {
	parentGateDB(t)

	parentOut, err := TaskSubmit(TaskSubmitOptions{Summary: "epic parent", Payload: "parent briefing", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	parentID := taskIDFromSubmitOutput(t, parentOut)
	childID := parentGateChild(t, parentID, "child of a pending parent")

	if got := claimNext(t, "parent-gate-test"); got != "" {
		t.Fatalf("child must be parked while the parent is pending, got %q", got)
	}

	releaseParent(t, parentID)

	if got := claimNext(t, "parent-gate-test"); got != childID {
		t.Fatalf("parent completed must release the child to the funnel: want %s, got %q", childID, got)
	}
}

// TestParentGateVerifiedParentReleasesChild pins the second member of the
// release set — the set is exactly {completed, verified} and verified is not
// an accident of the completed test above.
func TestParentGateVerifiedParentReleasesChild(t *testing.T) {
	parentGateDB(t)

	parentOut, err := TaskSubmit(TaskSubmitOptions{Summary: "epic parent", Payload: "parent briefing", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	parentID := taskIDFromSubmitOutput(t, parentOut)
	childID := parentGateChild(t, parentID, "child of a pending parent")

	for _, st := range []string{TaskStatusClaimed, TaskStatusInProgress, TaskStatusCompleted, TaskStatusVerified} {
		if _, err := TaskUpdate(parentID, st); err != nil {
			t.Fatalf("parent → %s: %v", st, err)
		}
	}
	if got := claimNext(t, "parent-gate-test"); got != childID {
		t.Fatalf("parent verified must release the child: want %s, got %q", childID, got)
	}
}

// TestParentGateCancelledParentParksChildForever pins the by-design hard
// edge: the abandoned arm is NOT relaxed by the superseded addition — a
// CANCELLED (or failed) parent parks its children indefinitely (no override,
// no cascade cancel, no timeout release). The parked row stays legible
// instead.
func TestParentGateCancelledParentParksChildForever(t *testing.T) {
	for _, terminal := range []string{TaskStatusCancelled, TaskStatusFailed} {
		t.Run(terminal, func(t *testing.T) {
			parentGateDB(t)

			parentOut, err := TaskSubmit(TaskSubmitOptions{Summary: "epic parent", Payload: "parent briefing", Tags: parentGateParentTag, Source: "auto"})
			if err != nil {
				t.Fatalf("submit parent: %v", err)
			}
			parentID := taskIDFromSubmitOutput(t, parentOut)
			childID := parentGateChild(t, parentID, "child of a parent that dies")

			var chain []string
			if terminal == TaskStatusCancelled {
				chain = []string{TaskStatusCancelled} // pending → cancelled is a legal move
			} else {
				chain = []string{TaskStatusClaimed, TaskStatusInProgress, TaskStatusFailed}
			}
			for _, st := range chain {
				if _, err := TaskUpdate(parentID, st); err != nil {
					t.Fatalf("parent → %s: %v", st, err)
				}
			}
			if got := claimNext(t, "parent-gate-test"); got != "" {
				t.Fatalf("a %s parent must park the child forever: got %q", terminal, got)
			}
			if row := taskRowByID(t, childID); row.Status != taskStatusPending {
				t.Fatalf("parked child must stay pending, got status=%s", row.Status)
			}
		})
	}
}

// TestParentGateDanglingParentIsNeverClaimable pins the explicit dangling
// arm: a parent_task_id naming NO row is denied (an unresolvable dependency
// is never assumed satisfied). The gate is denied HERE even though TaskSubmit
// refuses a dangling parent at submit time — the row is planted by direct
// ledger write, which is exactly how such a row arrives in practice (a
// pre-validation row, a peer replica whose parent has not synced, ledger
// surgery).
func TestParentGateDanglingParentIsNeverClaimable(t *testing.T) {
	parentGateDB(t)

	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := insertTaskRow(ctx, db, taskCreateFields{
		TaskID:       "t-dangling-child",
		Summary:      "child of a parent that does not exist",
		Status:       taskStatusPending,
		ParentTaskID: "t-no-such-parent",
		Payload:      "briefing",
		Source:       "auto",
	}); err != nil {
		t.Fatalf("insert dangling child: %v", err)
	}

	// (a) the predicate: dangling ⇒ not claimable, explicitly (not by
	// accident of a missing map entry).
	idx, err := taskParentStatuses(ctx, db)
	if err != nil {
		t.Fatalf("taskParentStatuses: %v", err)
	}
	if _, ok := idx["t-no-such-parent"]; ok {
		t.Fatalf("fixture broken: %q must not exist in the ledger", "t-no-such-parent")
	}
	if denied, reason := taskParentGateDenied("", "", "", []string{"t-no-such-parent"}, idx); !denied {
		t.Fatalf("dangling parent must deny the claim")
	} else if !strings.Contains(reason, "missing") {
		t.Fatalf("dangling reason must name the missing parent, got %q", reason)
	}

	// (b) the funnel, end to end: the row is never handed out.
	if got := claimNext(t, "parent-gate-test"); got != "" {
		t.Fatalf("a dangling parent must make the row unclaimable, got %q", got)
	}

	// (c) the nil-index arm: a caller that could not read the ledger DENIES a
	// parented row rather than assuming its dependency holds.
	if denied, reason := taskParentGateDenied("", "", "", []string{"t-no-such-parent"}, nil); !denied {
		t.Fatalf("an unreadable parent index must deny a parented row")
	} else if reason != taskParentReasonUnread {
		t.Fatalf("unreadable index must say so, got %q", reason)
	}
}

// TestParentGateParentlessRowsUnaffected proves — it does not assert — that a
// row with no parent_task_id behaves exactly as it did before the gate: it is
// claimable in the same order, carrying no [queue-skip] and no extra reason.
func TestParentGateParentlessRowsUnaffected(t *testing.T) {
	parentGateDB(t)

	first, err := TaskSubmit(TaskSubmitOptions{Summary: "parentless one", Payload: "briefing one", Source: "auto"})
	if err != nil {
		t.Fatalf("submit one: %v", err)
	}
	firstID := taskIDFromSubmitOutput(t, first)
	second, err := TaskSubmit(TaskSubmitOptions{Summary: "parentless two", Payload: "briefing two", Source: "auto"})
	if err != nil {
		t.Fatalf("submit two: %v", err)
	}
	secondID := taskIDFromSubmitOutput(t, second)

	// (a) the claim order is untouched: FIFO among priority-5 rows.
	if got := claimNext(t, "parent-gate-test"); got != firstID {
		t.Fatalf("parentless claim order changed: want %s, got %q", firstID, got)
	}
	if got := claimNext(t, "parent-gate-test"); got != secondID {
		t.Fatalf("parentless claim order changed: want %s, got %q", secondID, got)
	}

	// (b) the pending view renders the same verdict: the first row is [next],
	// and NO row carries a dependency reason.
	list, err := TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if strings.Contains(list, taskParentReasonUnreleasedFormat[:12]) || strings.Contains(list, "not released") ||
		strings.Contains(list, "missing from the ledger") {
		t.Fatalf("parentless rows must carry no dependency reason, got:\n%s", list)
	}
	if ln := renderedRowLine(list, firstID); ln != "" && !strings.Contains(ln, TaskQueueNextMarker) {
		t.Fatalf("first parentless row must be [next], line = %q", ln)
	}
}

// TestParentGateListingAgreesWithFunnel is the agreement test: on ONE
// fixture the pending view's marker and the funnel's verdict must not
// disagree, and the parked row must NAME its reason (a parked row that looks
// starved is the operator report this view exists to answer).
//
// The fixture is the REAL chain shape (the same one the A/B CLI proof uses):
// the parent is walked to in_progress — a RUNNING parent — so a non-pending
// row is never a funnel candidate and the pending group holds exactly the
// child and the unrelated row. That keeps the marker assertion about the
// child's eligibility rather than about a skip tag (the pending view renders
// with a nil skip set by design, so a pending parent is legitimately [next]
// there).
func TestParentGateListingAgreesWithFunnel(t *testing.T) {
	parentGateDB(t)

	parentOut, err := TaskSubmit(TaskSubmitOptions{Summary: "epic parent", Payload: "parent briefing", Source: "auto"})
	if err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	parentID := taskIDFromSubmitOutput(t, parentOut)
	childID := parentGateChild(t, parentID, "parked child")
	// The parked child's line survives the locator's ID-COLUMN rule: its parent
	// id sits in its TAIL (the reason), and that is exactly the shape the
	// locator refuses to match on (TestParentGateRowLocatorNamesTheIdColumn).
	if childID == parentID {
		t.Fatalf("fixture error: the child must be its own row, got %q for both", childID)
	}
	list, err := TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if childLine := renderedRowLine(list, childID); childLine == "" || childLine == renderedRowLine(list, parentID) {
		t.Fatalf("the locator must resolve the child's own line by its ID COLUMN, got %q (parent line %q)\n%s",
			childLine, renderedRowLine(list, parentID), list)
	}
	for _, st := range []string{TaskStatusClaimed, TaskStatusInProgress} {
		if _, err := TaskUpdate(parentID, st); err != nil {
			t.Fatalf("parent → %s: %v", st, err)
		}
	}
	unrelatedOut, err := TaskSubmit(TaskSubmitOptions{Summary: "unrelated row", Payload: "unrelated briefing", Source: "auto"})
	if err != nil {
		t.Fatalf("submit unrelated: %v", err)
	}
	unrelatedID := taskIDFromSubmitOutput(t, unrelatedOut)

	list, err = TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	childLine := renderedRowLine(list, childID)
	if childLine == "" {
		t.Fatalf("child missing from the pending view:\n%s", list)
	}
	if !strings.Contains(childLine, TaskQueueSkipMarker) {
		t.Fatalf("parked child must carry %q, line = %q", TaskQueueSkipMarker, childLine)
	}
	// The reason is LEGIBLE and names the parent + its status (not a bare
	// unclaimable marker).
	if !strings.Contains(childLine, parentID) || !strings.Contains(childLine, "status="+taskStatusInProgress) ||
		!strings.Contains(childLine, taskStatusCompleted) {
		t.Fatalf("parked row must name WHY (parent id, live status, release status), line = %q", childLine)
	}
	if ln := renderedRowLine(list, unrelatedID); !strings.Contains(ln, TaskQueueNextMarker) {
		t.Fatalf("the funnel's next row must be [next] in the listing too, line = %q", ln)
	}

	// AGREEMENT: the listing's [next] row IS the row the funnel claims.
	claimed := claimNext(t, "parent-gate-test")
	if claimed != unrelatedID {
		t.Fatalf("listing/funnel disagreement: listing says [next] = %s, funnel claims %q", unrelatedID, claimed)
	}

	// After the release, listing and funnel agree on the CHILD.
	if _, err := TaskUpdate(parentID, TaskStatusCompleted); err != nil {
		t.Fatalf("release parent: %v", err)
	}
	list, err = TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("TaskList 2: %v", err)
	}
	if ln := renderedRowLine(list, childID); !strings.Contains(ln, TaskQueueNextMarker) {
		t.Fatalf("released child must be [next], line = %q", ln)
	}
	if got := claimNext(t, "parent-gate-test"); got != childID {
		t.Fatalf("listing says the child is next, funnel claimed %q", got)
	}
}

// TestParentGateSupersededParentReleasesChild is the POSITIVE half of the
// 2026-10-07 epic: a parent whose work is done ELSEWHERE (migrated to a peer
// ledger's row, or superseded by a successor) must RELEASE its parked child.
// Before this, `cancelled` was the only terminal status such a row could
// carry, and `cancelled` parks by design — so the child waited on a
// dependency that was in fact satisfied, forever, with the queue looking
// clean. Measured on peer1's live ledger: 1 such pending child.
func TestParentGateSupersededParentReleasesChild(t *testing.T) {
	parentGateDB(t)

	parentOut, err := TaskSubmit(TaskSubmitOptions{Summary: "epic parent", Payload: "parent briefing", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	parentID := taskIDFromSubmitOutput(t, parentOut)
	childID := parentGateChild(t, parentID, "child of a parent whose work moved elsewhere")

	// The child is parked while the parent is live — the fixture's own red.
	if got := claimNext(t, "parent-gate-test"); got != "" {
		t.Fatalf("child must be parked while the parent is pending, got %q", got)
	}

	// The parent walks the ordinary ladder to superseded (no fabricated
	// status: the ledger's own transition table must license every step).
	for _, st := range []string{TaskStatusClaimed, TaskStatusInProgress, TaskStatusSuperseded} {
		if _, err := TaskUpdate(parentID, st); err != nil {
			t.Fatalf("parent → %s: %v", st, err)
		}
	}

	// (a) the funnel now hands the child out.
	if got := claimNext(t, "parent-gate-test"); got != childID {
		t.Fatalf("a superseded parent must release the child: want %s, got %q", childID, got)
	}
}

// TestParentGateSupersededParentPendingRowReleasesChild drives the SAME
// release through the row a MIGRATION leaves behind — a parent that never ran
// at all (pending → superseded), which is the shape the live defect has. The
// short chain is asserted explicitly because a migration cancels a QUEUED row,
// not a running one.
func TestParentGateSupersededParentPendingRowReleasesChild(t *testing.T) {
	parentGateDB(t)

	parentOut, err := TaskSubmit(TaskSubmitOptions{Summary: "epic parent", Payload: "parent briefing", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	parentID := taskIDFromSubmitOutput(t, parentOut)
	childID := parentGateChild(t, parentID, "child of a migrated parent")

	if _, err := TaskUpdate(parentID, TaskStatusSuperseded); err != nil {
		t.Fatalf("pending → superseded must be a legal move for a migrated row: %v", err)
	}
	if got := claimNext(t, "parent-gate-test"); got != childID {
		t.Fatalf("a never-run superseded parent must release the child: want %s, got %q", childID, got)
	}
}

// TestParentGateSupersededParentListingFollowsTheMap is the STRING half: the
// [queue-skip] reason an operator reads must name the release set the gate
// actually enforces. The text is DERIVED (taskParentReleaseSetLabel), so this
// test would fail if anyone hand-edited the prose — and it pins the two
// directions in ONE view: a superseded parent releases (no marker), a
// cancelled parent parks and its child's line names `superseded` among the
// ways out.
func TestParentGateSupersededParentListingFollowsTheMap(t *testing.T) {
	parentGateDB(t)

	// Parent A: cancelled → its child stays parked and the reason lists the
	// DERIVED label.
	cancelOut, err := TaskSubmit(TaskSubmitOptions{Summary: "cancelled parent", Payload: "p", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit cancelled parent: %v", err)
	}
	cancelID := taskIDFromSubmitOutput(t, cancelOut)
	parkedChild := parentGateChild(t, cancelID, "child parked by an abandoned parent")
	if _, err := TaskUpdate(cancelID, TaskStatusCancelled); err != nil {
		t.Fatalf("parent → cancelled: %v", err)
	}

	// Parent B: superseded → its child is released.
	superOut, err := TaskSubmit(TaskSubmitOptions{Summary: "superseded parent", Payload: "p", Tags: parentGateParentTag, Source: "auto"})
	if err != nil {
		t.Fatalf("submit superseded parent: %v", err)
	}
	superID := taskIDFromSubmitOutput(t, superOut)
	freedChild := parentGateChild(t, superID, "child freed by a superseded parent")
	if _, err := TaskUpdate(superID, TaskStatusSuperseded); err != nil {
		t.Fatalf("parent → superseded: %v", err)
	}

	list, err := TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	label := taskParentReleaseSetLabel()
	if label != "completed|superseded|verified" {
		t.Fatalf("the derived label must follow the map, got %q", label)
	}

	parkedLine := renderedRowLine(list, parkedChild)
	if !strings.Contains(parkedLine, TaskQueueSkipMarker) {
		t.Fatalf("a cancelled parent's child must stay parked, line = %q", parkedLine)
	}
	if !strings.Contains(parkedLine, label) {
		t.Fatalf("the parked reason must carry the DERIVED release label %q, line = %q", label, parkedLine)
	}
	if !strings.Contains(parkedLine, "status="+taskStatusCancelled) {
		t.Fatalf("the parked reason must name the parent's own status, line = %q", parkedLine)
	}
	if freedLine := renderedRowLine(list, freedChild); strings.Contains(freedLine, TaskQueueSkipMarker) {
		t.Fatalf("a superseded parent's child must NOT be parked, line = %q", freedLine)
	}
}

// TestParentGateDeniedIsTheOnlyDecisionPoint pins the ONE-predicate
// requirement mechanically: the reason renderer and the verdict must never
// answer differently, over every arm of the rule (parentless, released,
// unreleased, dangling, unreadable).
func TestParentGateDeniedIsTheOnlyDecisionPoint(t *testing.T) {
	idx := map[string]string{
		"p-done":       taskStatusCompleted,
		"p-verif":      taskStatusVerified,
		"p-super":      taskStatusSuperseded,
		"p-pending":    taskStatusPending,
		"p-claimed":    taskStatusClaimed,
		"p-inprogress": taskStatusInProgress,
		"p-cancel":     taskStatusCancelled,
		"p-failed":     taskStatusFailed,
	}
	cases := []struct {
		parent string
		want   bool
	}{
		{"", false},
		{"p-done", false},
		{"p-verif", false},
		{"p-super", false},
		{"p-pending", true},
		{"p-claimed", true},
		{"p-inprogress", true},
		{"p-cancel", true},
		{"p-failed", true},
		{"p-missing", true},
	}
	for _, c := range cases {
		denied, reason := taskParentGateDenied("", "", "", []string{c.parent}, idx)
		if denied != c.want {
			t.Errorf("parent %q: denied=%v want %v", c.parent, denied, c.want)
		}
		if got := taskRowClaimable("", "", "payload", []string{c.parent}, idx, nil); got == c.want {
			t.Errorf("parent %q: taskRowClaimable=%v disagrees with the gate (%v)", c.parent, got, c.want)
		}
		if (reason != "") != c.want {
			t.Errorf("parent %q: reason %q must be present exactly when denied", c.parent, reason)
		}
	}
	// The release set is spelled once and is exactly
	// {completed, verified, superseded}. EXHAUSTIVE over taskStatusSet on
	// purpose: a status added to the ledger must be DECIDED here rather than
	// defaulting into "parks forever" unnoticed — the same defect shape this
	// epic fixed (a status silently missing from a set nobody re-read).
	want := map[string]bool{
		taskStatusCompleted:  true,
		taskStatusVerified:   true,
		taskStatusSuperseded: true,
		taskStatusPending:    false,
		taskStatusClaimed:    false,
		taskStatusInProgress: false,
		taskStatusFailed:     false,
		taskStatusCancelled:  false,
		taskStatusReopened:   false,
	}
	if len(want) != len(taskStatusSet) {
		t.Fatalf("every status in taskStatusSet must be decided above: set=%v expectations=%d", taskStatusSet, len(want))
	}
	for _, s := range taskStatusSet {
		if got, ok := taskParentReleaseStatuses[s]; got != want[s] || (!ok && want[s]) {
			t.Fatalf("release set membership of %q = %v (present=%v), want %v — got set %v",
				s, got, ok, want[s], taskParentReleaseStatuses)
		}
	}
	if len(taskParentReleaseStatuses) != 3 {
		t.Fatalf("release set must be exactly %d members, got %v", 3, taskParentReleaseStatuses)
	}
	// The rendered label is DERIVED — it must follow the map, never a
	// hand-edited string (the [queue-skip] text an operator reads).
	if label := taskParentReleaseSetLabel(); label != "completed|superseded|verified" {
		t.Fatalf("release-set label must be derived from the map, got %q", label)
	}
}

// TestParentGateRowLocatorNamesTheIdColumn is `renderedRowLine`'s own
// regression, and it exists because the locator SILENTLY LIED once (measured
// 2026-09-25): the parked child's row carries its parent's id in the
// "[queue-skip] parent <id> not released" suffix, so a locator matching "any
// occurrence of the id" returned the CHILD's line as the PARENT's. The test
// above then read the child's own [queue-skip] as the parent's marker and
// failed while the ledger layer was correct.
//
// The fixture is hand-rendered on purpose: the defect is a PROPERTY OF THE
// RENDER SHAPE ("can another row's id appear in my row's tail?"), and a real
// ledger would only reproduce it while a child of the parent happens to be in
// the view.
func TestParentGateRowLocatorNamesTheIdColumn(t *testing.T) {
	const parentID = "01a0d692-82f3-7d7e-a665-8635d5665d08"
	const childID = "01a0d692-843d-7946-81d4-b1ac24f57400"
	rendered := strings.Join([]string{
		taskListStatusCountSuffix,
		"#" + childID + "  [pending]  parked child  [queue-skip] parent " + parentID + " not released (status=in_progress)",
		"#" + parentID + "  [in_progress]  epic parent",
	}, "\n")
	if got := renderedRowLine(rendered, childID); !strings.Contains(got, "parked child") {
		t.Errorf("the child's own line must be found by the child's id: %q", got)
	}
	got := renderedRowLine(rendered, parentID)
	if !strings.Contains(got, "epic parent") {
		t.Fatalf("the PARENT's id must resolve to the parent's own line, not to a child that names it in its tail: %q", got)
	}
	if strings.Contains(got, TaskQueueSkipMarker) {
		t.Errorf("the parent's line must not carry a child's queue marker: %q", got)
	}
}

// parentGateStatusSuperseded is the migrated parent status READ FROM THE
// CONSTANT the transition table validates writes against, never typed by hand
// (Rule 1/13: a status token is a meaning-carrying string, and it has exactly
// one definition site).
//
// The branch this test was re-authored from spelled the same value as its own
// string LITERAL, with a comment explaining that no such constant existed on
// its ground. That was measured and correct there: its merge-base `380679ad2`
// had neither `taskStatusSuperseded` in `taskStatusSet` nor a third member in
// `taskParentReleaseStatuses`, so the literal was the only way to pin the value
// at all. Master is the other ground — both exist — and there the literal would
// be a bare keyword that ALSO pins a value the ledger's CHECK constraint
// accepts only by coincidence.
const parentGateStatusSuperseded = taskStatusSuperseded

// TestParentGateMigratedSupersededParentCrossesTheLedgerBoundary pins the arm
// the POSITIVE tests above cannot reach from the ordinary ladder: `superseded`
// arriving as a VALUE in a status COLUMN from a migrated/peer row whose
// producer ran another build, rather than as the target of a live transition.
// The two boundary shapes fail differently and are therefore both exercised:
//
//   - the CHECK constraint: the column must ACCEPT the token, or a synced row
//     carrying it cannot be stored at all — the property a cross-ledger
//     migration depends on. (The branch's own arm asserted the opposite, and
//     was right to: it measured `CHECK constraint failed` on ITS ground, whose
//     schema predates the value.)
//
// The status is deliberately in `taskStatusWritable` (it is the migration
// write `pending → superseded`), so nothing here asserts otherwise — the point
// of this arm is the STORAGE and READ boundary, not the remedy list.
//
// The expectation is DERIVED from the release map under test, never from a
// second hand-kept copy of the answer: widening or narrowing
// taskParentReleaseStatuses fails HERE rather than silently, and the child's
// release follows master's semantics ({completed, verified, superseded}).
func TestParentGateMigratedSupersededParentCrossesTheLedgerBoundary(t *testing.T) {
	parentGateDB(t)

	wantReleased := taskParentReleaseStatuses[parentGateStatusSuperseded]
	if !wantReleased {
		t.Fatalf("fixture premise: `%s` must release its children (the migrated/peer arm), got %v from the release set %v",
			parentGateStatusSuperseded, wantReleased, taskParentReleaseStatuses)
	}

	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// (a) the WRITE path a synced or hand-planted row takes.
	const parentID = "t-migrated-superseded-parent"
	const childID = "t-migrated-superseded-child"
	if _, err := insertTaskRow(ctx, db, taskCreateFields{
		TaskID:  parentID,
		Summary: "parent whose work moved to a peer row",
		Status:  parentGateStatusSuperseded,
		Payload: "briefing",
		Source:  "auto",
	}); err != nil {
		t.Fatalf("the ledger's CHECK must accept `%s` for a migrated row: %v", parentGateStatusSuperseded, err)
	}
	if row := taskRowByID(t, parentID); row.Status != parentGateStatusSuperseded {
		t.Fatalf("the stored status must round-trip: wrote %q, read %q", parentGateStatusSuperseded, row.Status)
	}

	// (b) the READ path the gate judges: the index taskParentStatuses BUILDS
	// from stored rows is what the predicate is handed, so the fixture never
	// marks its own homework by feeding the predicate a hand-typed status.
	idx, err := taskParentStatuses(ctx, db)
	if err != nil {
		t.Fatalf("taskParentStatuses: %v", err)
	}
	if got := idx[parentID]; got != parentGateStatusSuperseded {
		t.Fatalf("the status index must carry the migrated row's value: idx[%s] = %q, want %q", parentID, got, parentGateStatusSuperseded)
	}
	denied, reason := taskParentGateDenied("", "", "", []string{parentID}, idx)
	if denied != !wantReleased {
		t.Fatalf("a `%s` parent must release its child (release set %v): denied=%v reason=%q",
			parentGateStatusSuperseded, taskParentReleaseStatuses, denied, reason)
	}
	if denied && !strings.Contains(reason, parentGateStatusSuperseded) {
		t.Errorf("a parked child's reason must NAME the offending status %q, got %q", parentGateStatusSuperseded, reason)
	}

	// The ONE predicate across both faces (Rule 14): the verdict and the
	// renderable reason can never disagree — on the migrated arm, on a parked
	// arm in the SAME index, and on a parentless row.
	if got := taskParentGateReason("", "", "", []string{parentID}, idx); got != reason {
		t.Errorf("renderer and verdict disagree on the migrated arm: gate %q vs reason face %q", reason, got)
	}
	idx[childID] = taskStatusCancelled
	childDenied, childReason := taskParentGateDenied("", "", "", []string{childID}, idx)
	if !childDenied {
		t.Errorf("a cancelled parent must still PARK: the migrated arm does not relax the abandoned one")
	} else if got := taskParentGateReason("", "", "", []string{childID}, idx); got != childReason {
		t.Errorf("renderer and verdict disagree on the parked arm: gate %q vs reason face %q", childReason, got)
	}
	if got := taskParentGateReason("", "", "", []string{""}, idx); got != "" {
		t.Errorf("a parentless row carries no dependency reason, got %q", got)
	}
	if got := taskRowClaimable("", "", "payload", []string{parentID}, idx, nil); got != !denied {
		t.Errorf("taskRowClaimable=%v disagrees with the gate (denied=%v) on the migrated arm", got, denied)
	}

	// (c) the migrated value is DECIDED, not merely tolerated: every status the
	// ledger can store answers consistently through both faces, so a future
	// widening cannot pass by leaving this arm aside.
	for _, st := range taskStatusSet {
		probe := "t-probe-" + st
		idx[probe] = st
		pDenied, pReason := taskParentGateDenied("", "", "", []string{probe}, idx)
		if got := taskRowClaimable("", "", "payload", []string{probe}, idx, nil); got == pDenied {
			t.Errorf("status %q: taskRowClaimable=%v disagrees with the gate (denied=%v)", st, got, pDenied)
		}
		if (pReason != "") != pDenied {
			t.Errorf("status %q: reason %q must be present exactly when denied", st, pReason)
		}
	}
}
