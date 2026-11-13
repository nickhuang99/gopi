package tools

// memory_sync_row_cancel.go — the SOURCE CANCELLATION of the row-send leg
// (`gopi task send --cancel-source`) plus the two constants that seam needs. It
// lives beside memory_sync_row.go (the leg itself) because it is the SAME leg,
// one flag further: the same receipt, the same peer, one extra write on THIS
// ledger.
//
// WHY IT EXISTS. Moving a row to another host was a THREE-STEP manual
// migration — send it, watch the receipt, then remember to cancel the source.
// The third step is the one a human forgets, and forgetting it is not a
// cosmetic omission: the row stays pending HERE while a copy is pending THERE,
// so both dispatchers see work and the row runs twice (the double-execution
// family this ledger keeps paying for). One command now does all three.
//
// THE FLAG IS NO LONGER OPTIONAL (epic task-send-is-a-move). It used to be: a
// send without --cancel-source was a COPY, which left two live copies of one
// row and made "one row globally" a property of the CALLER's spelling rather
// than of the system. SyncRowSendEx now refuses that shape before any peer is
// contacted (syncRowMoveOnlyGate), so this file's cancellation is not one of
// two options — it is what `send` means.
//
// THE WRITE IS THE FAMILY'S ONE CAS (task_cancel_pending.go): the same
// pending→cancelled conditional UPDATE the peer-side handover performs, so the
// pull leg and the push leg cannot disagree about what losing that race means
// either.
//
// THE SAFETY ARGUMENT IS THE ORDER, and there is exactly one of it:
//
//	contact the peer  →  read the RECEIPT  →  cancel SOURCE only if a peer's
//	ledger PERSISTED the row (stored / migrated / already current)
//
// A refusal, an unreachable peer, an unparsable answer — each of those is a
// NON-persisting outcome, and each leaves the source row exactly where it was:
// still pending, still dispatchable. Acting on the text a human reads instead
// of on the receipt is the failure mode this file refuses by construction
// (Rule 15b: prove the effect). The decision itself is ONE comparison —
// SyncRowReceipt.Persisted — so the printer of the receipt line and the
// canceller of the row can never disagree about what "the peer took it" means
// (Rule 14).

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/config"
)

const (
	// syncRowCancelLocalFailFormat is the ONE order in which the LOCAL cancel
	// can decline to run: the peer holds the row while the local write did not
	// land. It names what is true of BOTH ledgers afterwards, because that is
	// the fact the operator needs — the double-dispatch window, reported rather
	// than hidden (Rule 15).
	syncRowCancelLocalFailFormat = "cancel-source: peer %s HOLDS row %s but the LOCAL cancel failed: %v — the row is still pending here AND already queued on the peer, so it may run twice; cancel it here by hand or cancel the peer's copy"

	// syncRowEffectRefused is the fourth effect: a verdict about the row NOT
	// landing. It is an EFFECT like the other three (one vocabulary), which is
	// what makes "cancel only on a persisted receipt" a single comparison.
	syncRowEffectRefused = "refused"
)

// SyncRowCancelSourceOptions selects the OPTIONAL source cancellation that
// follows a send (`--cancel-source`).
type SyncRowCancelSourceOptions struct {
	// TaskID is the row that was sent (required).
	TaskID string
	// Result is the send's outcome — the receipts the cancellation is decided
	// on. nil (or no persisted receipt) means the source is left alone.
	Result *SyncRowSendResult
	// DBPath overrides the local ledger ("" = the default path).
	DBPath string
	// Cancel replaces the ledger write (tests inject a failure to pin the
	// reported window). nil = TaskUpdateReason, the ordinary status door.
	Cancel func(taskID, status, reason string) (string, error)
}

// SyncRowSourceDisposition is what happened to the SOURCE row — the whole
// answer, including the answer "nothing happened".
type SyncRowSourceDisposition struct {
	// At is the peer whose ledger holds the row ("" = none did).
	At string
	// Receipt is that peer's persisted effect ("" = none persisted).
	Receipt string
	// Cancelled reports that the source row was cancelled HERE.
	Cancelled bool
	// Note is the ONE line the caller prints. It is ALWAYS non-empty: a
	// cancellation that did not happen is stated in words, never implied by
	// silence (Rule 15).
	Note string
}

// SyncRowCancelSource cancels the source row of a completed send, and ONLY
// when a peer's ledger really holds it. See the file header for the argument;
// the code below is that argument, in order.
func SyncRowCancelSource(opts SyncRowCancelSourceOptions) SyncRowSourceDisposition {
	taskID := taskIDNormalize(opts.TaskID)
	persisted := opts.Result.PersistedPeers()
	if len(persisted) == 0 {
		// No peer persisted the row: the source is UNCHANGED. The note names
		// why (the peer's own refusal text where there is one) so the operator
		// can tell "the peer said no" from "the peer never answered".
		why := syncRowCancelNoReceipt
		if r := syncRowFirstRefusal(opts.Result); r != "" {
			why = r
		}
		return SyncRowSourceDisposition{Note: fmt.Sprintf(syncRowCancelKeptFormat, taskID, why)}
	}
	peer := persisted[0]
	effect := syncRowPeerEffect(opts.Result, peer)

	cancel := opts.Cancel
	if cancel == nil {
		// THE ONE WRITER — the same pending→cancelled conditional UPDATE the
		// peer-side handover performs (task_cancel_pending.go). Before this,
		// the send leg reached the ordinary status door, which re-reads the row
		// and refuses an illegal TRANSITION: NOT the same guarantee, because two
		// writers can both read `pending`, both pass the transition gate and
		// both believe they own the row. The CAS makes exactly one win and tells
		// the loser which state beat it — and the pull and push legs of one MOVE
		// now share that answer word for word.
		cancel = func(id, status, reason string) (string, error) {
			out, cerr := TaskCancelPending(opts.DBPath, id, reason)
			if cerr != nil {
				return "", cerr
			}
			if !out.Cancelled {
				return "", out.MissError(id)
			}
			return out.Observed, nil
		}
	}
	// THE WRITE RUNS UNDER THE SAME PIN AS THE READ. The task family resolves
	// its ledger from the ENVIRONMENT at call time, so a cancellation that
	// read the caller's `--db` ledger and then wrote the process's own would
	// cancel a namesake row in a DIFFERENT ledger — measured while writing
	// this file's own test (the row read as pending, the write landed
	// elsewhere, and the named ledger never moved). The CAS pins its own
	// ledger for the same reason, so a caller-supplied Cancel (tests) runs
	// under the caller's pin exactly as before.
	cancelErr := syncRowWithLedger(opts.DBPath, func() error {
		_, cerr := cancel(taskID, TaskStatusCancelled, syncRowCancelReason(peer, effect, taskID))
		return cerr
	})
	if cancelErr != nil {
		return SyncRowSourceDisposition{At: peer, Receipt: effect,
			Note: fmt.Sprintf(syncRowCancelLocalFailFormat, peer, taskID, cancelErr)}
	}
	// THE MIGRATION MARKER, written AFTER the status it belongs to. The cancel
	// above leaves a row that is cancelled like any other, and a cancelled row is
	// normally RE-WORKABLE (the branch-rework lane's dedup is live-only on
	// purpose: a branch whose rework the operator abandoned must be able to enter
	// the lane again). A row whose cancel was THIS MOVE is the exception — its
	// work is on the peer — so the fact is stamped on the row here, by the ONE
	// writer of the decision, and read back by the lane's own gate
	// (reworkSummaryBlocked).
	//
	// A failed stamp is REPORTED, never swallowed AND never rolled back: the row
	// really is cancelled here and the peer really does hold it, so undoing the
	// cancel over a tag write would manufacture the double-dispatch the cancel
	// exists to prevent. The note says what could not be durably recorded.
	note := fmt.Sprintf(syncRowCancelLineFormat, taskID, peer, effect)
	if terr := syncRowStampHandoff(taskID, opts.DBPath); terr != nil {
		note += fmt.Sprintf(syncRowHandoffStampFailSuffix, terr)
	}
	return SyncRowSourceDisposition{Cancelled: true, At: peer, Receipt: effect, Note: note}
}

// syncRowStampHandoff appends reworkHandoffTag to the migrated row's tags. It is
// the ONE writer of the migration marker (Rule 14), and it is deliberately a
// READ-THEN-WRITE of the tags column rather than a blind append: taskAddTag —
// the family's own idempotent tag writer — opens the ledger through the ambient
// environment, so it is reached through the same pinned-ledger window the cancel
// just ran under (a marker written into a DIFFERENT ledger than the row is
// exactly the namesake-row defect syncRowWithLedger's header records).
func syncRowStampHandoff(taskID, dbPath string) error {
	return syncRowWithLedger(dbPath, func() error {
		ctx := context.Background()
		db, err := openTasksDB(ctx, memoryDBPath())
		if err != nil {
			return fmt.Errorf("open ledger: %w", err)
		}
		defer db.Close()
		_, err = taskAddTag(ctx, db, taskID, reworkHandoffTag)
		return err
	})
}

// syncRowCancelReason renders the auto-written reason of the cancellation: it
// names the PEER and the RECEIPT that authorised it, so the row's own trail
// answers "why is this cancelled?" without the operator having to remember the
// command that ran — and it carries the undo, because a cancellation on this
// host is a local status write and nothing more (no archive, no delete).
func syncRowCancelReason(peer, effect, taskID string) string {
	return fmt.Sprintf(syncRowCancelReasonFormat, peer, effect, taskID, taskID, TaskStatusPending)
}

// syncRowFirstRefusal renders the first non-persisting outcome of a send, in
// the peer's own words where the peer spoke: a refusal's text, or the
// transport failure this node saw. "" when the send was never attempted.
func syncRowFirstRefusal(res *SyncRowSendResult) string {
	if res == nil {
		return ""
	}
	for _, pr := range res.Receipts {
		if pr.Receipt.Persisted() {
			continue
		}
		if why := strings.TrimSpace(pr.Receipt.Reason); why != "" {
			return fmt.Sprintf(syncRowCancelRefusedFormat, pr.Peer, why)
		}
		return fmt.Sprintf(syncRowCancelRefusedFormat, pr.Peer, syncRowEffectRefused)
	}
	return ""
}

// syncRowPeerEffect is one peer's persisted effect out of a send result.
func syncRowPeerEffect(res *SyncRowSendResult, peer string) string {
	if res == nil {
		return syncRowEffectRefused
	}
	for _, pr := range res.Receipts {
		if pr.Peer == peer {
			return pr.Receipt.Effect()
		}
	}
	return syncRowEffectRefused
}

// syncRowStatusOn reads one row's status out of the ledger the caller named
// ("" = this process's own ledger), returning "" when the row is absent. It is
// the READ half of the gate and of the pre-cancel re-read, in ONE place: the
// tools layer resolves its ledger from the environment, so a caller that
// named a DB pins it for the duration of the call — and a named DB that
// cannot be honoured is an ERROR, never a silent read of the wrong ledger.
func syncRowStatusOn(dbPath, taskID string) (string, error) {
	var status string
	var err error
	if err := syncRowWithLedger(dbPath, func() error {
		row, rerr := taskStatusDirect(taskID)
		if rerr != nil {
			err = rerr
			return nil
		}
		if row != nil {
			status = row.Status
		}
		return nil
	}); err != nil {
		return "", err
	}
	return status, err
}

// syncRowWithLedger pins the ledger for the duration of fn when dbPath names
// one, and restores exactly what it found (unset stays unset). One spelling of
// the pin, used by the gate, the pre-cancel re-read and the cancellation's
// write, so "--db PATH" means the same ledger on every half of the verb.
func syncRowWithLedger(dbPath string, fn func() error) error {
	if strings.TrimSpace(dbPath) == "" {
		return fn()
	}
	prev, had := os.LookupEnv(config.MemoryDBEnv)
	if err := os.Setenv(config.MemoryDBEnv, dbPath); err != nil {
		return fmt.Errorf("row send: ledger %s: %w", dbPath, err)
	}
	defer func() {
		if had {
			_ = os.Setenv(config.MemoryDBEnv, prev) //stylegate:ignore the deferred restore cannot fail in a way the caller could act on; the pinned value is still in place otherwise
			return
		}
		_ = os.Unsetenv(config.MemoryDBEnv) //stylegate:ignore same: the restore's verdict is not actionable here
	}()
	return fn()
}
