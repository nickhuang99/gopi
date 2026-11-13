package tools

// The REWORK row's edge semantics — Rule 33's SECOND criterion.
//
// THE DEFECT THESE TESTS CLOSE (measured 2026-10-20/21). A successor minted
// from a judged row (`gopi task clone` mints `<id>-c1`; the verb that measured
// the defect minted `<id>-r1`) carries parent_task_id = the row it just judged,
// and that row is usually `failed`. Judged by the ONE criterion the gate used to
// have — "is the parent's work SATISFIED?" (taskParentReleaseStatuses
// {completed, verified, superseded}) — the successor is parked the instant it is
// created, and Rule 33 has no override, no cascade cancel and no staleness
// release, so it is parked FOREVER. Measured: 01a1597d-dbdb-7496-b845-203f7ab0d05c
// → its successor row sat [queue-skip] with no door out.
//
// THE FIX THESE TESTS PIN is NOT a wider release set. A rework row carries a
// different DEPENDENCY from an ordinary child: it exists BECAUSE the parent
// failed, so its edge means "the parent has reached its verdict, whatever it
// was". tasktags.ReworkLineageTag is stamped at birth (TaskCloneWithOptions via
// taskCloneTags; the verify gate's branch-rework lanes) and the gate judges a
// marked row by taskParentTerminalStatuses. An UNMARKED child of the same failed parent is
// still parked — that is the half that must never relax, and it is asserted
// here in the same arm.
//
// Every test drives the REAL surfaces (insertTaskRow, TaskCloneWithOptions,
// TaskParentStatuses, TaskNextFiltered, TaskList) against a scaffold ledger
// (Rule 32), never the predicate alone: the epic's red proof is "the successor
// row the verb actually minted is offered by the funnel and carries no
// [queue-skip] in the operator's view".

import (
	"context"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tasktags"
)

// reworkGateParentDone marks the parent row terminal through a path that does
// not need a live run: a `failed` verdict is written directly into the ledger,
// the same shape a verify-FAIL / reclaim / zombie sweep leaves behind.
func reworkGateSeedParent(t *testing.T, id, status string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := insertTaskRow(ctx, db, taskCreateFields{
		TaskID:  id,
		Summary: "judged row (" + status + ")",
		Status:  status,
		Payload: "the briefing the judged row was executed with",
		Source:  "auto",
	}); err != nil {
		t.Fatalf("insert parent %s: %v", id, err)
	}
}

// reworkGateClone drives the successor ENGINE the way the verify FAIL verdict
// does — `gopi task clone`, the ONE operator door that mints a successor from a
// judged row. (The verb this helper used to drive was deleted; the EDGE
// SEMANTICS it exercised are the same, which is why this suite survives the
// deletion with only its engine swapped.) It returns the minted successor's id.
func reworkGateClone(t *testing.T, parentID string) string {
	t.Helper()
	out, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{
		TaskID: parentID,
		Reason: "the run never produced a verdict",
	})
	if err != nil {
		t.Fatalf("clone %s: %v", parentID, err)
	}
	// The successor's id is READ OFF THE LEDGER, never derived from the parent's
	// id: since the operator's 2026-11-05 ruling a clone mints a FRESH uuid
	// (taskCloneMintID), so there is no suffix to guess — and the ledger already
	// answered the question through the parent edge.
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var succ string
	if err := db.QueryRowContext(ctx,
		`SELECT task_id FROM tasks WHERE parent_task_id = ? ORDER BY id DESC LIMIT 1`,
		parentID).Scan(&succ); err != nil {
		t.Fatalf("read the successor of %s back off the ledger: %v", parentID, err)
	}
	if !strings.Contains(out, succ) {
		t.Fatalf("clone(%s) reported %q — the successor it named must be the row the ledger holds (%q)", parentID, out, succ)
	}
	return succ
}

// TestReworkParentGateCloneChildIsClaimable proves the whole defect: the row
// `gopi task clone` mints from a FAILED row is (a) stamped with the lineage
// mark at birth, (b) claimable by the ONE shared funnel, and (c) rendered
// without [queue-skip] by the operator's view — while a plain child of the same
// failed parent is parked by all three.
func TestReworkParentGateCloneChildIsClaimable(t *testing.T) {
	parentGateDB(t)

	const failedParent = "t-rework-failed-parent"
	reworkGateSeedParent(t, failedParent, taskStatusFailed)
	succ := reworkGateClone(t, failedParent)

	// (a) THE MARK IS ON THE ROW, not in the caller: read it back off the
	// ledger, so a future lane that mints a successor without going through
	// TaskCloneWithOptions is caught here.
	row := taskRowByID(t, succ)
	if !taskHasTagToken(row.Tags, tasktags.ReworkLineageTag) {
		t.Fatalf("the rework row %s must carry the %q lineage mark at birth; tags=%q", succ, tasktags.ReworkLineageTag, row.Tags)
	}
	if row.ParentTaskID != failedParent {
		t.Fatalf("the rework row must KEEP the parent edge (FailedNeedsReissue's successor test reads it): parent=%q want %q", row.ParentTaskID, failedParent)
	}

	// (b) THE PREDICATE, through the index the ledger BUILDS — never a
	// hand-typed status map marking its own homework.
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	idx, err := taskParentStatuses(ctx, db)
	if err != nil {
		t.Fatalf("taskParentStatuses: %v", err)
	}
	if denied, reason := taskParentGateDenied("", row.Tags, "", []string{row.ParentTaskID}, idx); denied {
		t.Fatalf("a rework row whose parent FAILED must not be dependency-parked: %q", reason)
	}
	if got := taskParentGateReason("", row.Tags, "", []string{row.ParentTaskID}, idx); got != "" {
		t.Fatalf("the render face must agree with the verdict (no reason on a released row), got %q", got)
	}

	// (c) THE FUNNEL: the real claim path offers the successor.

	claimed := claimNext(t, "rework-gate-test")
	if claimed != succ {
		t.Fatalf("TaskNextFiltered offered %q, want the rework row %q — the row is parked by the verdict that asked for it", claimed, succ)
	}
	// Retire the probe row so (d) reads the freshly minted successor rather
	// than a row this test already walked to a verdict.
	if _, err := TaskUpdate(succ, taskStatusClaimed); err != nil {
		t.Fatalf("walk successor to claimed: %v", err)
	}
	if _, err := TaskUpdate(succ, taskStatusSuperseded); err != nil {
		t.Fatalf("retire the probe row: %v", err)
	}

	// (d) THE OPERATOR'S VIEW, on a fresh successor of the same failed parent:
	// `task list --status pending` is where the deadlock was READ.
	succ2 := reworkGateClone(t, failedParent)
	list, err := TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	var seen bool
	for _, l := range strings.Split(list, "\n") {
		if !strings.Contains(l, succ2) {
			continue
		}
		seen = true
		if strings.Contains(l, TaskQueueSkipMarker) {
			t.Fatalf("the rework row must not render %q:\n%s", TaskQueueSkipMarker, l)
		}
	}
	if !seen {
		t.Fatalf("the pending view rendered no line for the rework row %s; view:\n%s", succ2, list)
	}
	// Retire the display successor too: from here the ledger's ONLY pending row
	// is the ordinary child (e) is about, so "the funnel offers nothing" is a
	// statement about THAT row and not about a queue holding other work.
	for _, st := range []string{taskStatusClaimed, taskStatusSuperseded} {
		if _, err := TaskUpdate(succ2, st); err != nil {
			t.Fatalf("retire the display successor (%s): %v", st, err)
		}
	}

	// (e) THE HALF THAT MUST NOT RELAX: an ORDINARY child of the SAME failed
	// parent — no lineage mark — is still parked, by the predicate, the funnel
	// and the view. Releasing it would start a row that builds on a deliverable
	// the parent never delivered.
	plain := parentGateChild(t, failedParent, "an ordinary child of the failed row")
	plainRow := taskRowByID(t, plain)
	if taskHasTagToken(plainRow.Tags, tasktags.ReworkLineageTag) {
		t.Fatalf("the ordinary child must NOT carry the lineage mark; tags=%q", plainRow.Tags)
	}
	denied, reason := taskParentGateDenied("", plainRow.Tags, "", []string{plainRow.ParentTaskID}, idx)
	if !denied {
		t.Fatalf("an ordinary child of a `%s` parent must stay parked — the rework arm must not release it", taskStatusFailed)
	}
	if !strings.Contains(reason, taskStatusFailed) {
		t.Fatalf("the refusal must name the offending status %q, got %q", taskStatusFailed, reason)
	}
	if got := claimNext(t, "rework-gate-test-2"); got != "" {
		t.Fatalf("the funnel must leave the ordinary parked child alone, got %q", got)
	}
}

// TestReworkParentGateARMovesWithTheVerds pins the SET SEPARATION itself: the
// rework criterion releases on every TERMINAL verdict (that is the point),
// while the ordinary criterion keeps failed/cancelled parked. It is the
// regression guard against the tempting wrong fix — adding `failed` to
// taskParentReleaseStatuses — because that change would make the two loops
// below disagree with expectations asserted here.
func TestReworkParentGateARMovesWithTheVerdict(t *testing.T) {
	// EXHAUSTIVE over the status set: a status added to the ledger must be
	// DECIDED here rather than defaulting into "parked forever" unnoticed.
	wantTerminal := map[string]bool{
		taskStatusCompleted:  true,
		taskStatusVerified:   true,
		taskStatusSuperseded: true,
		taskStatusFailed:     true,
		taskStatusCancelled:  true,
		taskStatusPending:    false,
		taskStatusClaimed:    false,
		taskStatusInProgress: false,
		taskStatusReopened:   false, // retired (M4.20-B): not a verdict
	}
	if len(wantTerminal) != len(taskStatusSet) {
		t.Fatalf("every status in taskStatusSet must be decided above: set=%v expectations=%d", taskStatusSet, len(wantTerminal))
	}
	for _, st := range taskStatusSet {
		parent := "t-v-" + st
		idx := map[string]string{parent: st}

		got, ok := taskParentTerminalStatuses[st]
		if ok != wantTerminal[st] || (ok && !got) {
			t.Errorf("status %q: terminal-verdict set membership = %v (present=%v), want %v", st, got, ok, wantTerminal[st])
		}
		// The two criteria must never collapse into one another: a terminal
		// status that is NOT a release member is exactly the asymmetry the
		// rework arm exists for.
		if got := taskParentReleaseStatuses[st]; got && !wantTerminal[st] {
			t.Errorf("status %q: release set is WIDER than the terminal set — the two criteria have merged", st)
		}

		reworkDenied, reworkReason := taskParentGateDenied("", tasktags.ReworkLineageTag, "", []string{parent}, idx)
		if reworkDenied == wantTerminal[st] {
			t.Errorf("status %q: a REWORK row denied=%v, want it released exactly on a terminal verdict (%v); reason=%q",
				st, reworkDenied, !wantTerminal[st], reworkReason)
		}
		plainDenied, _ := taskParentGateDenied("", "", "", []string{parent}, idx)
		if plainDenied {
			if _, released := taskParentReleaseStatuses[st]; released {
				t.Errorf("status %q: an ORDINARY child of a released parent must not be parked", st)
			}
			continue
		}
		if !taskParentReleaseStatuses[st] {
			t.Errorf("status %q: an ordinary child was released by a parent outside the release set %v", st, taskParentReleaseStatuses)
		}
	}

	// The rendered reasons stay DISTINCT: a parked rework row names the
	// criterion its reader has to satisfy, and a parked ordinary row names the
	// release set. If they ever render alike, the "why" a reader acts on is
	// wrong for exactly one of them (Rule 15).
	idx := map[string]string{"p-pending": taskStatusPending}
	reworkReason := taskParentGateReason("", tasktags.ReworkLineageTag, "", []string{"p-pending"}, idx)
	plainReason := taskParentGateReason("", "", "", []string{"p-pending"}, idx)
	if reworkReason == plainReason {
		t.Fatalf("a parked rework row and a parked ordinary row render the same reason %q — the reader cannot tell which criterion applies", reworkReason)
	}
	if !strings.Contains(reworkReason, taskParentTerminalSetLabel()) {
		t.Errorf("the rework reason must name the terminal set %q, got %q", taskParentTerminalSetLabel(), reworkReason)
	}
	if !strings.Contains(plainReason, taskParentReleaseSetLabel()) {
		t.Errorf("the ordinary reason must name the release set %q, got %q", taskParentReleaseSetLabel(), plainReason)
	}

	// The two sets are SEPARATE definitions, and the terminal one is a superset
	// of the release one (every satisfied parent has also reached its verdict).
	for st := range taskParentReleaseStatuses {
		if !taskParentTerminalStatuses[st] {
			t.Errorf("status %q releases ordinary children but is not a terminal verdict — the sets have drifted", st)
		}
	}

	// The terminal set must be EXACTLY the ledger's own "this row's life ended"
	// notion (terminalTaskStatus), pinned here so the two definitions cannot
	// drift apart quietly: a rework row released by a NON-terminal parent would
	// start on a premise still in flight, and a terminal parent missing from the
	// set is the original deadlock in miniature.
	for _, st := range taskStatusSet {
		if got, want := taskParentTerminalStatuses[st], terminalTaskStatus(st); got != want {
			t.Errorf("status %q: terminal-verdict set says %v, the ledger's terminalTaskStatus says %v", st, got, want)
		}
	}
}
