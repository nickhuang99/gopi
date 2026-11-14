package tools

import (
	"context"
	"testing"
	"time"
)

// TestSyncDeltaPeerCancelKeepsLiveLocalRow — the RED PROOF for the defect
// measured on 2026-10-29 (rows 01a16f98/01a17866/01a17a10/01a17a6a/01a17a8a/
// 01a17c53/01a17d07/01a17e23): a PA cancelled eight rows on ONE node to
// de-duplicate two ledgers, with a reason that said "keep the host-b side,
// cancel the HP side" — and the v2 rank ladder wrote `cancelled` onto the
// KEPT side, because cancelled ranks 3 and pending ranks 0.
//
// Two facts make that a data loss rather than a merge opinion:
//
//  1. `cancelled` has no legal exit in this build (taskAllowedTransitions
//     gives it an EMPTY list, and `task update <id> --status pending` refuses
//     by name), so a sync-borne cancel of a live row is UNRECOVERABLE — the
//     only repair is a reissue, which mints a new id and abandons the row.
//  2. Both channels' own headers state the opposite contract: the v1 arm
//     keeps a pending row pending ("cancelling local work is the local
//     dispatcher's decision — never a sync-side effect"), and the delta
//     channel's header says "cancelled stays local".
//
// The assertion is therefore about the LOCAL row, not about convergence: same
// id, both sides pending, the peer's cancel arrives → the local row is STILL
// pending, and the intent travels as the same `cancel-was-intent` tag the v1
// arm writes (work is never dropped, and the audit sees why).
func TestSyncDeltaPeerCancelKeepsLiveLocalRow(t *testing.T) {
	newDeltaTestDB(t)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-cancel-keep", "the work the PA said to KEEP on this side",
		taskStatusPending, base, base, "bug")

	patch := deltaPatch("peer-node",
		SyncTaskRow{
			TaskID: "t-cancel-keep", Summary: "the work the PA said to KEEP on this side",
			Status: taskStatusCancelled, CreatedAt: int64(base), UpdatedAt: int64(base) + 5,
			Source: "human", Tags: []string{"docs"},
			Reason: "DUPLICATE-ACROSS-HOSTS: keep THIS side, cancel the peer's copy",
		})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsCancelKept != 1 {
		t.Fatalf("a peer cancel on a LIVE local row must be COUNTED as kept, not silently absorbed — got %+v", stats)
	}

	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	row, err := taskByID(context.Background(), db, "t-cancel-keep")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if row.Status != taskStatusPending {
		t.Fatalf("a peer's cancelled copy CANCELLED this host's live row (%s) — cancelling local work is the "+
			"local dispatcher's decision, and cancelled has no legal exit, so this is unrecoverable loss", row.Status)
	}
	if !taskHasTagToken(row.Tags, syncTaskCancelIntentTag) {
		t.Fatalf("the cancel INTENT must still reach this ledger as %q (work is never dropped, the audit sees "+
			"why) — tags = %q", syncTaskCancelIntentTag, row.Tags)
	}
	if row.Reason != "" {
		t.Fatalf("a kept row's own columns must not be rewritten by the discarded cancel — reason = %q", row.Reason)
	}
}

// TestSyncDeltaPeerCancelStillLandsOnATerminalLocalRow — the negative control
// that keeps the new arm NARROW: a local row that is ALREADY terminal (here
// completed) is not "live local work", so the peer's cancel must be decided by
// the pre-existing ladder (terminal-tier 后到覆盖), not held back. Without this
// arm a guard that simply drops every cancelled peer row would pass the test
// above and quietly break the channel.
func TestSyncDeltaPeerCancelStillLandsOnATerminalLocalRow(t *testing.T) {
	newDeltaTestDB(t)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-cancel-terminal", "a row this host already finished",
		taskStatusCompleted, base, base, "docs")

	patch := deltaPatch("peer-node",
		SyncTaskRow{
			TaskID: "t-cancel-terminal", Summary: "a row this host already finished",
			Status: taskStatusCancelled, CreatedAt: int64(base), UpdatedAt: int64(base) + 5,
			Source: "human",
		})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsCancelKept != 0 {
		t.Fatalf("the cancel-kept arm must not fire on an already-terminal row — got %+v", stats)
	}
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	row, err := taskByID(context.Background(), db, "t-cancel-terminal")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if row.Status != taskStatusCancelled {
		t.Fatalf("a LATER terminal peer write still wins 后到覆盖 (completed→cancelled at rank 3), got %s", row.Status)
	}
}
