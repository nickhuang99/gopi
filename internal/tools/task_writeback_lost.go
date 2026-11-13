package tools

// A RUN'S TERMINAL WRITEBACK THAT DID NOT LAND, recorded where it survives the
// process that tried it.
//
// THE DEFECT THIS CLOSES (measured on this host's ledger, 2026-10-27): the
// wrapper (`gopi task run` / a worker round) writes the row's terminal status
// through writebackOutcome, whose local write is
// tools.TaskRunWriteback → TaskUpdateWithResult → the state machine + the
// Rule 30 close-out gates. That write CAN be refused (an illegal transition, a
// busy ledger, a gate's refusal) and the function returns the ledger's verdict
// as an error — but every FAILED-branch call site in
// cmd/agent/cli_tasks.go discarded it, and the two gates that DO record their
// own refusal (Rule 30's dirty/residue arms) cover only the delivery claims
// (`completed`/`verified`). So a refused `failed` writeback left the row
// `in_progress`, the process exited with the child's error and nothing on the
// row said why. The observable result was four rows on one peer whose holder
// pid was long dead (three labelled `[died …]`, one with no claim stamp at
// all) and no account of the failed write anywhere.
//
// WHAT THIS FUNCTION DOES — and, deliberately, does NOT do. It records the
// loss in TWO places that outlive the dying process:
//
//	1. the row's own trail (task_events, event = WritebackLostEvent), which
//	   `task show` renders because the name is in verdictTrailEventNames; and
//	2. the notify pipeline (the same event name, registered in
//	   taskNotifyAlarmEvents so `GOPI_TASK_NOTIFY_EVENTS=all` — this host's
//	   setting — delivers it to the operator's sink).
//
// It does NOT close, cancel or downgrade the row, and it does not retry the
// write. A row whose terminal write was refused is exactly the state the
// operator ruled on (2026-10-26, devdoc/zombie-sweep-removed.md): the sweep
// that used to dispose of such rows is GONE and must not be resurrected in a
// new spelling — reporting is the whole deliverable, and rewriting a row's
// status to make a report unnecessary would be that sweep wearing this file's
// name.

import (
	"context"
	"fmt"
	"time"
)

// WritebackLostEvent names BOTH the notify alarm and the ledger trail event
// written when a run's terminal writeback does not land. One constant, two
// consumers (Rule 1b/13): the registry entry in taskNotifyAlarmEvents and the
// membership entry in verdictTrailEventNames both reference THIS name, so a
// rename cannot make the alarm and the render disagree.
const WritebackLostEvent = "writeback-lost"

// taskWritebackLostTrailTimeout bounds the trail write. It is short on
// purpose: the caller is a run that is already ending (often after its child
// failed), so a ledger that cannot answer quickly is itself part of the story
// the caller must report rather than wait out.
const taskWritebackLostTrailTimeout = 5 * time.Second

// taskWritebackLostLineFmt is the operator-facing sentence. It names the row,
// the status that did not land, and the ledger's own words — never a
// re-derived guess about why (Rule 15: the refusal text belongs to the gate
// that produced it).
const taskWritebackLostLineFmt = "task %s: the %s writeback DID NOT LAND — the row stays OPEN (its state is the ledger's, not this run's); the ledger said: %v"

// taskWritebackLostTrailMissFmt is appended when even the trail write failed:
// a reporter that hides its own failure is the defect it exists to catch.
const taskWritebackLostTrailMissFmt = " (the trail entry could NOT be written either: %s)"

// TaskWritebackLoss is what TaskWritebackLost measured, returned rather than
// logged so the caller can print ONE sentence (the wrapper writes it to the run
// log AND stderr) without re-deriving any of it.
type TaskWritebackLoss struct {
	// TaskID and Status are the write that did not land.
	TaskID string
	Status string
	// Line is the complete, operator-facing sentence, including the trail
	// outcome (see TrailRecorded).
	Line string
	// TrailRecorded reports whether the loss reached the row's own
	// task_events trail. False means the ledger could not be read/written at
	// all — and TrailNote then carries why (the honest half: on the defect's
	// own shape the last witness is the notify alarm plus stderr).
	TrailRecorded bool
	// TrailNote is the reason the trail write failed ("" when it landed).
	TrailNote string
}

// TaskWritebackLost records that `status` could not be written onto taskID, and
// fires the alarm. wbErr is the ledger's own refusal (never nil — a caller with
// no error has nothing to report).
//
// Both legs are best-effort and neither can fail the caller: the run is already
// ending, and a reporting failure must degrade to a LOUDER report (the sentence
// says so) rather than to a second error the caller cannot act on.
func TaskWritebackLost(taskID, status string, wbErr error) TaskWritebackLoss {
	loss := TaskWritebackLoss{TaskID: taskID, Status: status}
	line := fmt.Sprintf(taskWritebackLostLineFmt, taskID, status, wbErr)
	if note, ok := taskWritebackLostTrail(taskID, line); ok {
		loss.TrailRecorded = true
	} else {
		loss.TrailNote = note
		line += fmt.Sprintf(taskWritebackLostTrailMissFmt, note)
	}
	loss.Line = line
	// The alarm is fired AFTER the trail attempt so the summary carries the
	// trail's outcome: the subscriber who asked for everything gets the same
	// sentence the row (or its absence) is described by.
	NotifySyntheticEvent(WritebackLostEvent, taskID, "", line)
	return loss
}

// taskWritebackLostTrail appends the loss to the row's trail. It reports why it
// could not, rather than swallowing the reason: a ledger that refuses this
// insert is the one case where the operator has no per-row record at all.
func taskWritebackLostTrail(taskID, detail string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), taskWritebackLostTrailTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err.Error(), false
	}
	defer db.Close() //stylegate:ignore short-lived reporter handle closed at return
	if err := taskEventWriteErr(ctx, db, taskID, WritebackLostEvent, detail); err != nil {
		return err.Error(), false
	}
	return "", true
}
