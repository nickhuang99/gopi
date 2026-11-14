package tools

// task_finish_premerge_prune_scope_test.go — WHAT THE BRANCH-CHECK PRUNE MAY
// DELETE, AND WHAT IT MUST NEVER TOUCH.
//
// THE QUESTION THIS FILE ANSWERS (measured 2026-10-30, epic 01a183d5). The HP
// ledger reads ZERO rows carrying `delivery-proof-unverified`, while two rows
// (01a17de7-fe23 / 01a17de9-e772) were judged by exactly that arm — the arm
// whose sentence is "the tip this run produced cannot be named". A zero count is
// indistinguishable from "the writer never ran" and "something deleted it", so
// the question had to be settled by measurement rather than by reading:
//
//	$ grep -rn 'DELETE FROM task_events' --include=*.go internal/ cmd/
//	internal/tools/task_finish_premerge.go:861:  … AND event IN (?, ?, ?) …
//
// There is exactly ONE deleting writer on the whole `task_events` table, and its
// set is the three `branch-checks-*` verdict names (bound: the newest
// finishCleanupBranchCheckEventKeep per row). `delivery-proof-unverified` is NOT
// in it, so the arm's own records are not collectable by it — and a "fix" that
// widened the set, or a future refactor that replaced the named constants with a
// prefix/`LIKE` scan, would silently make them collectable and destroy the only
// durable account of an unjudged close.
//
// THE TWO PROPERTIES PINNED HERE.
//
//  1. MEMBERSHIP: the prune's set is exactly the branch-checks family. It is
//     asserted by DRIVING the prune (not by reading the constants back), so a
//     widened set — whatever spelling produced it — turns this red.
//  2. SURVIVAL: with far more branch-check records on one row than the bound
//     allows, a `delivery-proof-unverified` record on that SAME row is still
//     there afterwards. This is the fact the ledger question needed: the prune
//     is per-NAME, never a row-wide "keep the newest N".
//
// A third property is checked because it is the reason the set must stay named:
// the trail is bounded to keep a failing delivery from growing the ledger
// without end, so the bound itself has to hold — an unbounded prune is as wrong
// as an over-wide one (both lose the audit).

import (
	"context"
	"database/sql"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// pstRowID is the fixture row. A literal, not a generated id: the assertion is
// about one row's trail, and a generated id would hide a cross-row mistake.
const pstRowID = "01a183d5-pst-prune-scope"

// pstPruneRowCtx opens the sandbox ledger and inserts the row the trail hangs on.
func pstPruneRowCtx(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	path := testutil.NewTestDBPath(t)
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("open the sandbox ledger: %v", err)
	}
	t.Cleanup(func() { db.Close() }) //stylegate:ignore the fixture's own handle
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, payload, deliverable, tags, source, created_at, updated_at)
		 VALUES (?, 'prune scope fixture', 'in_progress', '', '', '', 'auto', ?, ?)`,
		pstRowID, dpSeedClock, dpSeedClock); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	return ctx, db
}

// pstEventNames reads back every event name on the row, oldest first.
func pstEventNames(t *testing.T, c *sql.DB) []string {
	t.Helper()
	rows, err := c.QueryContext(context.Background(),
		`SELECT event FROM task_events WHERE task_id = ? ORDER BY id`, pstRowID)
	if err != nil {
		t.Fatalf("read the row's trail: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ev string
		if err := rows.Scan(&ev); err != nil {
			t.Fatalf("scan the row's trail: %v", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the row's trail: %v", err)
	}
	return out
}

// pstCount counts one event NAME on the row, whatever its detail — the prune
// (and its bound) are per name, so a detail-keyed count would measure something
// else entirely.
func pstCount(t *testing.T, c *sql.DB, event string) int {
	t.Helper()
	var n int
	if err := c.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event = ?`, pstRowID, event).Scan(&n); err != nil {
		t.Fatalf("count %q for %s: %v", event, pstRowID, err)
	}
	return n
}

// pstDetailFor gives each event name a DISTINCT detail. The delivery-proof
// writer's own idempotence cap (reclaimRunResidueRefusalLimit) keys on
// (event, detail), so a fixture that reused one detail would measure the cap
// instead of the prune.
func pstDetailFor(event string) string { return "prune-scope fixture detail for " + event }

// TestBranchCheckPruneDeletesOnlyItsOwnFamily is property 1: every OTHER trail
// name on the row survives a prune, and the prune's own family is the only thing
// it bounds.
//
// RED ON A TREE WHERE THE SET IS WIDENED — including the plausible-but-wrong
// "keep the newest N events of this row" rewrite, which would collect the
// delivery-proof record along with everything else.
func TestBranchCheckPruneDeletesOnlyItsOwnFamily(t *testing.T) {
	ctx, c := pstPruneRowCtx(t)

	// The record whose survival the epic's question hangs on, plus the two
	// neighbours that share its subject (a row's delivery claim) and a plain
	// status transition as the control.
	kept := []string{DeliveryProofUnverifiedEvent, deliveryProofOwedEvent, deliverableTipUnreachableEvent, taskStatusCompleted}
	for _, ev := range kept {
		taskEventWrite(ctx, c, pstRowID, ev, pstDetailFor(ev))
	}

	// MORE branch-check records than the bound allows, so the prune has work to
	// do: a prune that deleted nothing would pass every "kept" assertion above
	// while proving nothing.
	for i := 0; i < finishCleanupBranchCheckEventKeep+2; i++ {
		finishCleanupBranchCheckReason(pstRowID, finishCleanupPreMergeResult{
			Status: finishCleanupPreMergeStatusSkipped,
			Detail: "abstained: prune-scope fixture",
		})
	}

	for _, ev := range kept {
		if n := pstCount(t, c, ev); n != 1 {
			t.Errorf("%q must survive the branch-check prune (the set is the branch-checks family ONLY), its %d record(s) are gone",
				ev, 1-n)
		}
	}
	if n := pstCount(t, c, finishCleanupPreMergeUnverifiedEvent); n != finishCleanupBranchCheckEventKeep {
		t.Errorf("the prune must bound its OWN family to %d — an unbounded trail grows without end, an over-eager one loses the audit; got %d",
			finishCleanupBranchCheckEventKeep, n)
	}
}

// TestBranchCheckPruneSurvivesAlongsideADeliveryProofRecord is property 2 stated
// as the ledger question asked it: on ONE row that carries both a
// delivery-proof-unverified record and a rolled-over branch-check trail, the
// delivery-proof record is still the newest-kept fact of its own name.
//
// It also pins the READ side the operator actually uses: `task show` must still
// find the verdict, which it can only do if the record is present. A prune that
// ate it would leave the row rendering as a clean, proven close — the exact
// confusion the verdict list in tasks.go exists to prevent.
func TestBranchCheckPruneSurvivesAlongsideADeliveryProofRecord(t *testing.T) {
	ctx, c := pstPruneRowCtx(t)
	taskEventWrite(ctx, c, pstRowID, DeliveryProofUnverifiedEvent, pstDetailFor(DeliveryProofUnverifiedEvent))

	for i := 0; i < finishCleanupBranchCheckEventKeep+3; i++ {
		finishCleanupBranchCheckReason(pstRowID, finishCleanupPreMergeResult{
			Status: finishCleanupPreMergeStatusRefused,
			Detail: "refused: prune-scope fixture",
		})
	}

	event, detail := taskEventLatestVerdict(ctx, c, pstRowID)
	if event != DeliveryProofUnverifiedEvent {
		t.Fatalf("`task show` must still render %q as the row's verdict, got %q — a prune that collects it makes an unjudged close indistinguishable from a proven one",
			DeliveryProofUnverifiedEvent, event)
	}
	if detail != pstDetailFor(DeliveryProofUnverifiedEvent) {
		t.Errorf("the rendered verdict must be the record's own detail, got %q", detail)
	}
}
