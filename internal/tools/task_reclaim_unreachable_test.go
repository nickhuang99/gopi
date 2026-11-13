package tools

// The PERMANENT-SKIP marker — the durable half of the convergence contract in
// task_reclaim_converge_test.go.
//
// THE DEFECT THESE TESTS PIN (epic: dead-holder 回收器永久性跳过): the reclaim's
// three PERMANENT refusals — the repo directory is gone, the directory is a
// linked worktree of ANOTHER repo, the directory is not this repo's worktree at
// all — cannot be cleared by any later pass, so a host running the merger (or a
// worker) re-printed the same sentence on every tick, forever. The in-process
// memo (reclaimRefusalSeen) silenced a repeat INSIDE one process only; a
// restarted merger, a second process, or a fresh worker start printed it again.
// Measured live 2026-09-15: one fixture row made the merger's 5-minute loop
// emit the line every tick (devdoc/reclaim-fossil-20260915.md), so an operator
// could not tell a permanent fossil from a real, actionable refusal.
//
// THE CONTRACT (four halves, each asserted below):
//
//	① the FIRST pass REPORTS the row (Rule 15 — a refusal is disclosed),
//	② the SAME pass records `reclaim-unreachable` on the row's OWN trail
//	  (the ledger is where a later process learns the row was already named),
//	③ the SECOND pass is SILENT for that row, in a process whose in-memory memo
//	  is EMPTY (a fresh merger) — the marker is the carrier, not the memo,
//	④ neither the row's STATUS nor the directory moves (Rule 42: nothing is
//	  deleted, no row is rewritten).
//
// Plus the aggregate: a pass that withheld N rows prints ONE summary line, so
// the condition stays visible without the per-row flood.

import (
	"context"
	"os"
	"strings"
	"testing"
)

// reclaimMarkerEvents returns the `reclaim-unreachable` trail entries recorded
// for taskID — the marker as a READER sees it (event name + detail), not as
// this code believes it wrote it.
func reclaimMarkerEvents(t *testing.T, taskID string) []string {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	rows, err := db.QueryContext(context.Background(),
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id`,
		taskID, reclaimUnreachableEventName)
	if err != nil {
		t.Fatalf("read marker events for %s: %v", taskID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatalf("scan marker event for %s: %v", taskID, err)
		}
		out = append(out, detail)
	}
	return out
}

// reclaimRowStatus reads the row's status straight from the ledger, so the
// "nothing about the row moved" half is measured and not assumed.
func reclaimRowStatus(t *testing.T, taskID string) string {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var status string
	if err := db.QueryRowContext(context.Background(),
		`SELECT status FROM tasks WHERE task_id = ?`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status for %s: %v", taskID, err)
	}
	return status
}

// TestReclaimUnreachableMarkerSilencesTheSecondPass is the epic's own
// regression: two rows whose repo is gone, two consecutive passes of the SAME
// arm. The first pass names each row once and records the marker; the second
// pass — after the in-memory memo is THROWN AWAY, standing in for a restarted
// merger — says nothing about them.
func TestReclaimUnreachableMarkerSilencesTheSecondPass(t *testing.T) {
	freshReclaimRefusalMemo(t)
	root := fossilLedgerEnv(t)
	const rowA, rowB = "t-unreach-a", "t-unreach-b"
	_, wtA := seedFossilRow(t, root, rowA)
	_, wtB := seedFossilRow(t, root, rowB)

	first := captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })
	for _, id := range []string{rowA, rowB} {
		if !strings.Contains(first, id) {
			t.Fatalf("the FIRST pass must report %s (Rule 15 — a permanent refusal is disclosed at least "+
				"once):\n%s", id, first)
		}
		markers := reclaimMarkerEvents(t, id)
		if len(markers) != 1 {
			t.Fatalf("%s must carry exactly ONE %s marker after the first pass, got %d: %v",
				id, reclaimUnreachableMarker, len(markers), markers)
		}
		if !strings.Contains(markers[0], reclaimUnreachableMarker) {
			t.Errorf("%s's marker must spell %q so an operator can grep it, got %q",
				id, reclaimUnreachableMarker, markers[0])
		}
	}
	// A FRESH PROCESS SHAPE: the memo this process built is dropped, exactly as
	// a restarted merger would have none. Only the ledger marker stands between
	// the row and another flood of lines.
	freshReclaimRefusalMemo(t)
	second := captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })
	for _, id := range []string{rowA, rowB} {
		if strings.Contains(second, id) {
			t.Fatalf("%s was reported on the first pass and its marker is on its trail — a pass in a "+
				"process with an EMPTY memo must stay silent for it (this is the flood the epic ends):\n%s",
				id, second)
		}
		if n := len(reclaimMarkerEvents(t, id)); n != 1 {
			t.Errorf("%s must not accumulate a second marker per pass, got %d", id, n)
		}
	}
	// The condition stays VISIBLE: one aggregate line, no per-row sentence.
	if !strings.Contains(second, "previously recorded") {
		t.Errorf("a pass that stays silent about recorded rows must say so in ONE summary line "+
			"(Rule 15: the skip is disclosed, never swallowed):\n%s", second)
	}

	// ④ nothing about the rows or their directories moved.
	for _, id := range []string{rowA, rowB} {
		if got := reclaimRowStatus(t, id); got != taskStatusInProgress {
			t.Errorf("%s: the marker must never change the row's STATUS, got %q", id, got)
		}
	}
	for _, wt := range []string{wtA, wtB} {
		if _, err := os.Stat(wt); err == nil {
			t.Errorf("%s must not exist (the fixture never created it) — nothing was made or removed", wt)
		}
	}
}

// TestReclaimUnreachableMarkerIsWrittenThroughTheTrail pins ②'s own requirement:
// the marker is a task_events append (a note), never a tasks-row rewrite. A row
// whose status could be moved by this arm would be the very thing the epic
// forbids, so the row's status is compared before and after.
func TestReclaimUnreachableMarkerIsWrittenThroughTheTrail(t *testing.T) {
	freshReclaimRefusalMemo(t)
	root := fossilLedgerEnv(t)
	const id = "t-unreach-trail"
	seedFossilRow(t, root, id)

	before := reclaimRowStatus(t, id)
	captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })
	if after := reclaimRowStatus(t, id); after != before {
		t.Fatalf("the marker is a trail note: status must be unchanged, %q → %q", before, after)
	}
	if n := len(reclaimMarkerEvents(t, id)); n != 1 {
		t.Fatalf("the marker must be exactly one task_events row, got %d", n)
	}
}

// TestReclaimPermanentRefusalClassification is the classifier's own unit: all
// FOUR permanent wordings are recognised — through the SAME constructors the
// arm emits them with, so wording and classifier cannot drift — and a refusal
// whose grounds can clear is NOT classified as permanent (a transient refusal
// that returned must still be reported; silencing it would be the opposite
// defect).
func TestReclaimPermanentRefusalClassification(t *testing.T) {
	for _, why := range []string{
		reclaimUnreachableRepoGoneReason("/gone/repo"),
		reclaimUnreachableForeignGroundReason("/gone/repo"),
		reclaimUnreachableCloneGroundReason("/gone/repo"),
		reclaimUnreachableUnregisteredReason("/gone/repo"),
	} {
		if !reclaimRefusalIsPermanent(why) {
			t.Errorf("a permanent refusal must be classified permanent: %q", why)
		}
	}
	// THE CLONE WORDING MUST NOT ALSO READ AS THE FOREIGN ONE: two stages, two
	// sentences. If the tokens overlapped, the fix could be undone by a test
	// that greps for the wrong one and still passes.
	if reclaimRefusalIsPermanent(reclaimUnreachableCloneGroundReason("/gone/repo")) &&
		strings.Contains(reclaimUnreachableCloneGroundReason("/gone/repo"), reclaimUnreachableForeignGroundToken) {
		t.Errorf("the clone ground's wording must not carry the foreign token — they are different shapes and "+
			"different stages: %q", reclaimUnreachableCloneGroundReason("/gone/repo"))
	}
	for _, why := range []string{
		reclaimSkipDirtyReason,
		"worktree status unreadable: chdir /gone/wt: no such file or directory",
		"a live process (pid 123) is working inside /gone/wt",
		"",
	} {
		if reclaimRefusalIsPermanent(why) {
			t.Errorf("a refusal whose grounds can clear must NOT be marked permanently skipped: %q", why)
		}
	}
}
