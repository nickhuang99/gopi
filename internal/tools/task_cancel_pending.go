package tools

// task_cancel_pending.go — THE ONE pending→cancelled conditional write of the
// cross-ledger MOVE, and the reason it is ONE.
//
// WHY A SINGLE WRITER. Moving a row between ledgers has two entry points —
// `gopi task send … --cancel-source` (push) and `gopi task receive --takeover`
// / the peer-side `--handover` (pull) — and BOTH end with the same act: the
// source ledger must stop holding the row as dispatchable work, in ONE
// conditional write, so that of two writers (a mover and a claimer, two movers,
// a mover and an operator's cancel) exactly one wins and the loser is told
// WHICH state beat it. That write used to exist twice: `TaskTransferHandover`
// carried its own `UPDATE … WHERE task_id=? AND status=?`, and the send leg
// reached `TaskUpdateReason` — the ordinary status door, which re-reads the row
// and refuses an illegal TRANSITION, and is therefore NOT the same guarantee
// (two writers can both read `pending`, both pass the transition gate, and both
// believe they own the row). Two spellings of one decision is two places to
// keep in step and two answers for one situation; the MOVE family gets one, and
// it is this file's.
//
// WHY NOT A DELETE. A delete leaves no trace that the row existed here, and a
// moved row is provenance an operator must be able to read (`task show` answers
// "where did it go?" from the reason line). `cancelled` is also the only
// terminal status legal from `pending` (taskAllowedTransitions) and the
// family's existing word for "this row's work is carried elsewhere".
//
// NO LOCK, NO BROADCAST, NO SCHEDULER (Rule 35). The atomicity is the ledger's
// own conditional UPDATE; nothing else is created to make it true.

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	// taskCancelPendingMissFormat is the ONE spelling of a lost CAS (Rule 1b):
	// the handover, the send leg and every test read this sentence, so "who
	// moved it" cannot be worded two ways in two callers. It names the row, the
	// state it is really in and what that means — never a bare "refused".
	taskCancelPendingMissFormat = "task %s is %s, not %s — another writer moved it between the read and the write (a claim, a cancel, a status move); nothing was cancelled by this move"

	// taskCancelPendingAbsent is the OBSERVED state when the ledger holds no
	// such row (Rule 1b: a status is never rendered as an empty string).
	taskCancelPendingAbsent = "absent from this ledger"

	// taskCancelPendingWriteFormat is the failure of the write itself (the
	// ledger could not be opened or the UPDATE errored) — reported, never
	// folded into the CAS-miss path, which is a statement about the ROW.
	taskCancelPendingWriteFormat = "pending→cancelled write on %s failed: %w"
)

// PendingCancelOutcome is what the conditional write observed: it landed, or it
// did not and the ledger says what state beat it. It is a VALUE rather than a
// bare bool because the caller must be able to name the winner (Rule 15b).
type PendingCancelOutcome struct {
	// Cancelled reports that THIS call performed the pending→cancelled write.
	Cancelled bool
	// Observed is the row's status as the losing CAS found it ("" when the
	// write landed); taskCancelPendingAbsent when the row is not here at all.
	Observed string
}

// MissError renders the lost-CAS sentence. It exists so a caller that wants an
// `error` (the handover lifts it into its own refusal type) does not retype the
// wording (Rules 1b/14).
func (o *PendingCancelOutcome) MissError(taskID string) error {
	observed := o.Observed
	if observed == "" {
		observed = taskCancelPendingAbsent
	}
	return fmt.Errorf(taskCancelPendingMissFormat, taskID, observed, taskStatusPending)
}

// taskCancelPendingCAS is THE conditional write: one UPDATE, pinned on
// status=pending, in the ledger the caller already opened. `reason` is
// overwritten on purpose — the row carries its own provenance out of here, and
// the trail is the only place an operator standing on the source ledger can
// learn where its work went.
//
// A lost CAS is NOT an error: it is the ordinary race (a claim, a cancel, a
// second mover), and the caller's next move differs from a transport failure's.
// So the miss returns (outcome, nil) with the observed status, and only a
// ledger failure returns an error.
func taskCancelPendingCAS(ctx context.Context, db *sql.DB, taskID, reason string) (*PendingCancelOutcome, error) {
	now := float64(time.Now().Unix())
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, reason = ?, updated_at = ?, ended_at = ?
		 WHERE task_id = ? AND status = ?`,
		taskStatusCancelled, reason, now, now, taskID, taskStatusPending)
	if err != nil {
		return nil, fmt.Errorf(taskCancelPendingWriteFormat, taskID, err)
	}
	//stylegate:ignore n==1 is the meaningful branch and a driver error yields the same 0 down the miss path below
	if n, _ := res.RowsAffected(); n == 1 {
		// The trail line and the notify bus are the same two side effects the
		// ordinary status door performs (tasks.go's taskUpdate), so a moved row
		// looks like any other cancellation to every reader of the trail.
		//
		// THE SAME THREE-ELEMENT RECORD (epic 01a1a899-d05d): this is a SECOND
		// status-transition writer, and it used to append an EMPTY detail like
		// the main door did — so a cancellation performed by the pending-cancel
		// arm was one more move the ledger could not attribute. It renders the
		// record through the one helper, so the two writers cannot drift.
		taskEventWrite(ctx, db, taskID, taskStatusCancelled, taskStatusEventDetailText(taskStatusEventDetailParts{
			Who:  taskStatusEventWho(),
			From: taskStatusPending,
			To:   taskStatusCancelled,
			Why:  reason,
		}))
		taskNotifyFlushStatusMove(taskID, taskStatusPending, taskStatusCancelled)
		return &PendingCancelOutcome{Cancelled: true}, nil
	}
	// The CAS lost. Read the state back so the refusal NAMES what beat it
	// instead of guessing.
	observed := taskCancelPendingAbsent
	if cur, cerr := taskByID(ctx, db, taskID); cerr == nil && cur != nil {
		observed = cur.Status
	}
	return &PendingCancelOutcome{Observed: observed}, nil
}

// TaskCancelPending is the ledger-opening face of the same write: it pins the
// caller's ledger (`dbPath`, "" = this process's own profile) through
// syncRowWithLedger — the family's ONE pin, because the tools layer resolves
// its ledger from the ENVIRONMENT at call time and a caller's `--db PATH` must
// name the ledger that is actually written (a cancellation that read one ledger
// and wrote another is the namesake-row defect that pin exists to close).
//
// It is exported because BOTH legs of the move call it (the peer-side handover
// and the send leg's source cancellation) and the acceptance of this epic is
// "there is exactly ONE writer" — a caller outside this package must be able to
// point at it.
func TaskCancelPending(dbPath, taskID, reason string) (*PendingCancelOutcome, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("cancel-source: a task id is required (a move names the row it moves)")
	}
	var out *PendingCancelOutcome
	err := syncRowWithLedger(dbPath, func() error {
		ctx := context.Background()
		db, derr := openTasksDB(ctx, memoryDBPath())
		if derr != nil {
			return derr
		}
		defer db.Close() //dead-end:ignore the handle belongs to this write; every error is returned to the caller
		var werr error
		out, werr = taskCancelPendingCAS(ctx, db, taskID, reason)
		return werr
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
