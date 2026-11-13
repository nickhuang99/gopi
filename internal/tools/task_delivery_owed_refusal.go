package tools

// task_delivery_owed_refusal.go — the REFUSAL-SIDE half of the delivery queue:
// a `completed`/`verified` writeback the delivery proof REFUSES is put on the
// delivery queue IN THE SAME BREATH, so the existing retry path
// (`task_delivery_retry.go`, whose scan keys on `delivery-blocked`) comes back
// to the row.
//
// THE DEFECT THIS CLOSES, measured on this host (2026-10-22). The proof arm
// refuses the terminal write and records `delivery-proof-owed` — a name NO
// consumer scans for. The retry pass's scan (`deliveryRetryOwedIDs`) selects
// rows whose NEWEST trail event is exactly `delivery-blocked`, so a refused
// writeback produced a row that is:
//
//	$ gopi task retry-delivery --task <row>     # the operator's own door
//	<row> … — the row is not a delivery candidate (status=in_progress …)
//
// — the verb reads the ledger ROW, finds it open, and answers "not a
// candidate"; the resident pass never sees it at all. Two writers existed for
// "this row owes a delivery" (the disposal-side handoff, since DELETED — see
// finish_cleanup_moved_helpers_test.go's note at the old reclaimSelfDeliveryLine
// site — wrote the event for `failed` rows) and this path wrote a THIRD,
// unconsumed spelling. The
// result was a queue with no consumer: the refusal was loud on stderr and
// inert in the ledger.
//
// SO THE HANDOFF IS A RECORD, NOT A STATUS (exactly the shape the `failed`-row
// handoff already has). No status is minted, no transition is added: the row
// stays OPEN — which is the point of the refusal — and its trail gains the
// record the retry path already reads. `gopi task retry-delivery` consequently
// sees a row that is open AND owes a delivery, and answers the honest thing
// ("the deliverable is not at its destination yet") instead of "not a
// candidate".
//
// RULE 35 — NO NEW DEPENDENCY: one `task_events` INSERT through the existing
// writer. CORRECTED 2026-10-30 (task 01a18ef7): this used to add "consumed by
// the pass that already ticks", and that pass had no production caller and was
// deleted — NOTHING consumes the record automatically. It sits on the row where
// `task show` and the (retired) verb both read it, and the operator is the
// consumer: `gopi task update <id>` / `gopi task reopen <id>` /
// `gopi task finish-cleanup --task <id>`.

import (
	"context"
	"database/sql"
	"strings"
)

// why the refusal's own note is not enough on its own, in the row's words
const taskDeliveryRefusedPreamblePrefix = "writeback refused, the row is still open — "

// taskDeliveryRefusedQueue puts a refused row on the delivery queue. It is
// called ONLY from the refusal arm of `deliveryProofRefusal` (the one close-out
// seam), so the queue's writer and its consumer can never disagree about which
// event means "owed": the name comes from the retry path's own constant
// (Rule 14), never a second spelling.
//
// It returns the sentence to RECORD and to hand back to the caller, or "" when
// nothing was written (an empty note is never a record — Rule 15a).
//
// A ledger that cannot be written is reported through the existing notify log
// and does NOT change the refusal: the refusal already happened, and turning a
// loud refusal into a crash because its own annotation failed would be the
// wrong failure.
func taskDeliveryRefusedQueue(taskID, note string) string {
	taskID = taskIDNormalize(taskID)
	note = strings.TrimSpace(note)
	if taskID == "" || note == "" {
		return ""
	}
	detail := taskDeliveryRefusedPreamblePrefix + note
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("delivery queue for %s: %v", taskID, err)
		return ""
	}
	defer db.Close()
	// The SAME cap the other arms use (Rule 13: one spelling), so a re-landed
	// refusal cannot drip identical records down the trail.
	if taskEventCountForDetailOn(ctx, db, taskID, deliveryRetryBlockedEvent, detail) >= reclaimRunResidueRefusalLimit {
		return detail
	}
	taskEventWrite(ctx, db, taskID, deliveryRetryBlockedEvent, detail)
	return detail
}

// taskDeliveryOpenRefusedPrefix is how an OPEN row's debt is rendered to an
// operator: the row is not terminal BY DESIGN (the close-out seam refused it),
// so a reader must not mistake "still open" for "not a delivery candidate".
const taskDeliveryOpenRefusedPrefix = "the row is OPEN and owes a delivery — the close-out seam refused its terminal write: "

// deliveryRetryOpenOwedReason answers whether an OPEN row is on the delivery
// queue, and WHY — "" means it owes nothing and the caller keeps its ordinary
// path. The evidence is the row's own newest trail entry, which is exactly the
// predicate the resident pass scans by (Rule 14: one spelling), so the verb and
// the pass agree by construction.
func deliveryRetryOpenOwedReason(ctx context.Context, db *sql.DB, taskID string) string {
	if db == nil || strings.TrimSpace(taskID) == "" {
		return ""
	}
	var event, detail string
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(event,''), COALESCE(detail,'') FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT 1`,
		taskID).Scan(&event, &detail)
	if err != nil {
		// A ledger this read cannot answer is not a licence to claim a debt
		// (Rule 15: report, never guess) — the caller's ordinary path decides.
		if err != sql.ErrNoRows {
			taskNotifyLog("delivery retry: read newest event for %s: %v", taskID, err)
		}
		return ""
	}
	if event != deliveryRetryBlockedEvent {
		return ""
	}
	if d := strings.TrimSpace(detail); d != "" {
		return taskDeliveryOpenRefusedPrefix + d
	}
	return taskDeliveryOpenRefusedPrefix + "the row carries a " + deliveryRetryBlockedEvent + " record with no detail"
}
