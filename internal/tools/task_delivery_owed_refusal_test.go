package tools

// Regression for THE REFUSAL-SIDE DELIVERY QUEUE (task_delivery_owed_refusal.go).
//
// THE BUG, measured on this host 2026-10-22: the delivery-proof arm REFUSED a
// `completed` writeback and recorded `delivery-proof-owed` on the still-open
// row — a name no consumer scans for. The retry path's scan keys on
// `delivery-blocked`, and the verb's first guard was `taskTerminalStatus`, so
// the operator's own door answered
//
//	<row> … — the row is not a delivery candidate (status=in_progress …)
//
// for exactly the rows that owe a delivery. The refusal was loud on stderr and
// inert in the ledger.
//
// These tests drive the REAL write path (TaskUpdateWithResult → the ONE local
// mutation core) and the REAL verb path (DeliveryRetryTaskID), against a real
// scratch git fixture shaped like clone incident 01a163df.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestDeliveryRefusalPutsTheRowOnTheQueue is THE regression: the refused row
// must be claimable by the delivery queue, which means (a) its newest trail
// event is the retry scan's own key, and (b) the verb's one-row entry point
// names the debt instead of answering "not a candidate".
//
// RED WHEN THE QUEUE WRITER IS REVERTED: the newest event stays
// `delivery-proof-owed`, the scan returns no rows, and the verb returns
// `unreadable` with "not a delivery candidate".
func TestDeliveryRefusalPutsTheRowOnTheQueue(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, masterTip, groundTip := dpCloneChannel(t)
	repo := filepath.Join(filepath.Dir(ground), "seed")

	id := "dq-refused"
	dpSeedRow(t, db, id, "feat: undelivered work", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)

	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err == nil {
		t.Fatalf("fixture must be REFUSED (ground HEAD %s vs delivery point %s); the delivery-proof arm is inert",
			branchDeleteShortHash(groundTip), branchDeleteShortHash(masterTip))
	}
	if got := dpRowStatus(t, db, id); got != taskStatusInProgress {
		t.Fatalf("the refused write must leave the row open, got %q", got)
	}

	// (a) the queue's own predicate sees it.
	ctx := context.Background()
	odb, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer odb.Close()
	ids, err := deliveryRetryOwedIDs(ctx, odb, deliveryRetryMaxRows)
	if err != nil {
		t.Fatalf("delivery retry scan: %v", err)
	}
	if !dqContains(ids, id) {
		t.Fatalf("row %s owes a delivery but the retry scan does not offer it (newest event=%q) — "+
			"the refusal wrote a name no consumer reads", id, dqNewestEvent(t, db, id))
	}

	// (b) the verb's door names the debt rather than refusing to look.
	rep, err := DeliveryRetryTaskID(id)
	if err != nil {
		t.Fatalf("DeliveryRetryTaskID(%s): %v", id, err)
	}
	if rep.Status != deliveryRetryStatusBlocked || !rep.Owed() {
		t.Fatalf("an open row on the delivery queue must report blocked/owed, got status=%q detail=%q",
			rep.Status, rep.Detail)
	}
	if !strings.Contains(rep.Detail, taskDeliveryOpenRefusedPrefix) {
		t.Errorf("the answer must say the row is OPEN and owes a delivery, got: %q", rep.Detail)
	}
}

// TestDeliveryRefusalQueueDoesNotClaimAnUnrelatedOpenRow: every open row in
// the ledger must NOT be on the delivery queue — only one whose newest event is
// the queue's own key. Without this arm the writer could pass the test above
// by putting every open row on the queue.
func TestDeliveryRefusalQueueDoesNotClaimAnUnrelatedOpenRow(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	id := "dq-ordinary"
	dpSeedRow(t, db, id, "ordinary in-progress work", taskStatusInProgress)

	rep, err := DeliveryRetryTaskID(id)
	if err != nil {
		t.Fatalf("DeliveryRetryTaskID(%s): %v", id, err)
	}
	if rep.Status != deliveryRetryStatusUnreadable {
		t.Fatalf("a row with no delivery debt must keep its ordinary answer, got status=%q detail=%q",
			rep.Status, rep.Detail)
	}
	if !strings.Contains(rep.Detail, "not a delivery candidate") {
		t.Errorf("the ordinary refusal wording must be preserved, got: %q", rep.Detail)
	}
}

// dqContains is the set test the scan's answer needs.
func dqContains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// dqNewestEvent reports the row's newest trail event — the exact column the
// scan's SQL keys on, so a failure names what was actually written.
func dqNewestEvent(t *testing.T, dbPath, id string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var event string
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(event,'') FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT 1`, id).Scan(&event); err != nil {
		return "<none>"
	}
	return event
}
