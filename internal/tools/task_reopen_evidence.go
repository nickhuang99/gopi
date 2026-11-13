package tools

// task_reopen_evidence.go — the TERMINAL-STATE GUARD's evidence reader
// (defect A, epic 01a18f3f).
//
// WHAT IT REPAIRS, in the ledger's own words. The task-run wrapper
// (cmd/agent/cli_tasks.go writebackOutcome) writes the human row's terminal
// status AFTER cmd.Wait, and it downgraded `completed` to `failed` on ONE
// signal — runend.Truncated(). A run that died on a network error (`exit
// status 1`) is not in that set, so the wrapper wrote `completed` onto rows the
// ledger still held as `failed`, OVERWRITING the previous terminal status
// without ever looking at it. Measured on this host's ledger, two rows, same
// shape (read-only SQL over ~/.gopi/memory.db, 2026-10-31):
//
//	01a18799-3304-…  #12005 failed   (read tcp …: connection timed out)
//	                 #12049 delivery-proof-owed
//	                 #12050 delivery-blocked  writeback refused, the row is still open
//	                 #12065 completed         (a 17.2s run closing a row the
//	                                           delivery guard had just refused)
//	01a18ea5-8925-…  #12007 failed → #12059 completed   (same shape)
//
// A terminal status is a VERDICT. Reopening one is a decision about work that
// already has an answer, so it may not be made by a wrapper's arithmetic on an
// exit code — it needs EXPLICIT EVIDENCE recorded on the row, and a REASON.
//
// THE EVIDENCE IS AN EVENT, NOT A TAG, AND ITS POSITION IS THE WHOLE
// CRITERION. `resumed:<session>` is a LINEAGE note: it says a resume happened
// at some point in the row's life, which is true of BOTH rows above (both carry
// `resumed:×1`) and therefore distinguishes nothing. What the guard needs is an
// authorization that came AFTER the failure it would overwrite: a
// `reopen-authorized` entry newer than the newest terminal-status event on the
// trail. Older than the failure, it authorized a DIFFERENT verdict.
//
// WHO WRITES IT. Nothing in this repository writes it automatically — that is
// the point. The operator (or a reviewer) who has looked at the work and
// decided the failed verdict was wrong records it deliberately; the wrapper
// reads it and nothing else. Rule 35: no new dependency, one INSERT through the
// writer that already exists, consumed by the write it guards.

import (
	"context"
	"fmt"
	"strings"
)

const (
	// TaskReopenAuthorizedEvent is the record a human writes to say "this row's
	// terminal verdict is superseded — let it be written again". Its DETAIL is
	// the reason, and it must be non-empty (Rule 15a: an empty note is never a
	// record, so it can never authorize anything).
	TaskReopenAuthorizedEvent = "reopen-authorized"

	// TaskReopenRefusedEvent is the record the guard writes when it refuses.
	// It is the reason the row is still failed, written where `task show` and
	// any later reader find it — a refusal that only reached stderr would
	// belong to a process that has already exited (the measured shape of the
	// writeback-lost defect).
	TaskReopenRefusedEvent = "reopen-refused"

	// TaskReopenUnreadableEvent records the ONE case the guard could not judge:
	// the trail could not be read, so the write went through unguarded. It is a
	// separate name from the refusal because they are different findings with
	// different repairs (Rule 15: a reader must be able to tell them apart).
	TaskReopenUnreadableEvent = "reopen-guard-unreadable"

	// taskReopenTrailScanLimit bounds the newest-first trail walk (Rule 1: a
	// named limit, never an inline one). A row whose authorization sits deeper
	// than this is not a case the guard should guess about.
	taskReopenTrailScanLimit = 64
)

// TaskReopenEvidence reports the reason of an authorization that is NEWER than
// the verdict being superseded (the `prev` event), or "" when there is none.
// A read failure is returned, never flattened into "not authorized": the caller
// must be able to tell "no evidence" from "could not look".
//
// THE STOP CONDITION IS THE SUPERSEDED VERDICT, NOT "any terminal event" — and
// that distinction is a measured one (2026-10-31): the operator's override
// writes its authorization record and THEN performs the transition, so the
// newest trail entry is the NEW terminal event. Stopping at the first terminal
// event therefore threw away the authorization that had just been written, and
// the `--force` door silently failed its own evidence check. Walking instead
// until the verdict being moved OFF is what makes both orders work: a human
// authorizing before a run, and the operator authorizing inside one call.
func TaskReopenEvidence(taskID, prev string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: reopen evidence: task_id must not be empty")
	}
	ctx := context.Background()
	// READ-ONLY OPENER — this is a pure read; it takes no write lock on the
	// ledger the wrapper is about to write into.
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return "", fmt.Errorf("task: reopen evidence for %s: %w", taskID, err)
	}
	defer db.Close()

	// ONE pass over the trail, newest first, bounded by the same scan cap the
	// other newest-first readers use (Rule 1: a named limit, never a magic one).
	rows, err := db.QueryContext(ctx,
		`SELECT COALESCE(event,''), COALESCE(detail,'') FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT ?`,
		taskID, taskReopenTrailScanLimit)
	if err != nil {
		return "", fmt.Errorf("task: reopen evidence for %s: %w", taskID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var event, detail string
		if err := rows.Scan(&event, &detail); err != nil {
			return "", fmt.Errorf("task: reopen evidence for %s: %w", taskID, err)
		}
		// The verdict being SUPERSEDED stops the walk: anything older than it
		// authorized a different one (see the header).
		if prev != "" && event == prev {
			return "", nil
		}
		if event == TaskReopenAuthorizedEvent {
			if reason := strings.TrimSpace(detail); reason != "" {
				return reason, nil
			}
			// An authorization with no reason is not evidence — keep looking
			// (a later, older one may carry one).
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("task: reopen evidence for %s: %w", taskID, err)
	}
	return "", nil
}

// TaskReopenRecord appends one reopen record (authorized or refused) to the
// row's trail. It returns the error rather than logging it: the caller is the
// guard, and an unrecorded refusal is exactly what must not happen silently.
func TaskReopenRecord(taskID, event, detail string) error {
	taskID = taskIDNormalize(taskID)
	detail = strings.TrimSpace(detail)
	if taskID == "" || event == "" || detail == "" {
		return fmt.Errorf("task: reopen record needs a task_id, an event and a non-empty reason")
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskWritebackLostTrailTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return fmt.Errorf("task: reopen record for %s: %w", taskID, err)
	}
	defer db.Close() //stylegate:ignore short-lived reporter handle closed at return
	if err := taskEventWriteErr(ctx, db, taskID, event, detail); err != nil {
		return fmt.Errorf("task: reopen record for %s: %w", taskID, err)
	}
	return nil
}

// taskReopenRefusalNote renders the sentence a REFUSED reopen leaves on the row
// — one spelling, so the trail and the returned error cannot tell two stories
// (Rule 14).
func taskReopenRefusalNote(prev, next string) string {
	return fmt.Sprintf("%s → %s refused: this row already reached a terminal verdict %q, and a "+
		"terminal status is a claim about work that already has an answer — reopening it needs an "+
		"explicit %s record on this row NEWER than that verdict (the record's detail is the reason). "+
		"The row is left unchanged",
		prev, next, prev, TaskReopenAuthorizedEvent)
}

// taskReopenEvidenceRefusal is the guard's decision, in ONE place (Rule 14).
//
// It refuses exactly one shape: a write that would move a row OFF one terminal
// status onto the OTHER one, with no authorization younger than the failure.
// Everything else passes untouched —
//
//   - a same-status write (the idempotent re-land) is not a reopen;
//   - a LIVE row reaching a terminal status is the ordinary close-out;
//   - cancelled/superseded/verified are outside the pair a RUN can write, and
//     the state machine already governs them;
//   - `--force` and GOPI_TASK_DIRTY_CLOSEOUT_ALLOW are the operator's OTHER
//     gates and deliberately do not release this one (a dirty-tree override must
//     not silently become a verdict override).
//
// An unreadable trail is NOT a licence to refuse: the guard asks, and if the
// ledger cannot answer it RECORDS that fact on the row and lets the write
// through — refusing what it cannot see would wedge every close-out on a ledger
// whose read failed (the same three-valued ruling deliveryProofRefusal carries,
// and Rule 15's loud-not-guessing half).
func taskReopenEvidenceRefusal(taskID, prev, next string) (string, bool) {
	if prev == next || !taskStatusTerminalPair(prev) || !taskStatusTerminalPair(next) {
		return "", false
	}
	note, err := TaskReopenEvidence(taskID, prev)
	if err != nil {
		detail := "reopen guard could not read the trail (" + err.Error() + "), so the " +
			prev + " → " + next + " write was allowed unguarded"
		// Recorded, never silent, and never a refusal.
		if rerr := TaskReopenRecord(taskID, TaskReopenUnreadableEvent, detail); rerr != nil {
			taskNotifyLog("%s", detail)
		}
		return "", false
	}
	if strings.TrimSpace(note) != "" {
		return "", false
	}
	refusal := taskReopenRefusalNote(prev, next)
	if rerr := TaskReopenRecord(taskID, TaskReopenRefusedEvent, refusal); rerr != nil {
		// The refusal still stands — the row is untouched either way — but the
		// loss of its record is reported through the one sink that outlives this
		// process.
		taskNotifyLog("reopen refusal record for %s: %v", taskID, rerr)
	}
	return refusal, true
}

// taskStatusTerminalPair reports whether s is one of the two terminal statuses a
// RUN may write (the pair writebackOutcome can produce). completed↔failed is the
// only move this guard governs, and it is deliberately the narrow set: the rule
// exists for the wrapper's arithmetic on an exit code, not for every terminal
// edge in the ledger.
func taskStatusTerminalPair(s string) bool {
	return s == taskStatusCompleted || s == taskStatusFailed
}
