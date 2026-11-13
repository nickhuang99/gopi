package tools

// task_signal_verdict.go — the LEDGER half of the unsourced-kill guard (epic
// signal-provenance-unsourced, 2026-09-17).
//
// THE GAP THIS CLOSES. A gopi run that dies by signal has always written a
// session-log line and a session-provenance record, and both spelled the same
// thing whatever the sender was: an operator's `gopi worker --stop` and an
// unannounced `kill` from a stranger produced byte-identical trails ("draining"
// either way). The 2026-09-16T22:23:13-14 cohort — three worker daemons, their
// three run children, the merger daemon and the web server, all SIGTERMed
// inside one second with ZERO intent records in the tree — was therefore
// invisible as an EVENT: nothing in the ledger said "eight gopi processes were
// killed by someone we do not know".
//
// WHAT IS RECORDED. One append-only task_events row on the run's own ledger
// row, carrying the machine-readable verdict (sourced true/false) and the
// signal, so `gopi task show <id>` history answers "was this run stopped
// gracefully, or was it killed?" without reading a log. A graceful operator
// drain and an unannounced kill remain TWO different events, by name.
//
// WHY BEST-EFFORT. This runs on a shutdown path, so it may not block or fail
// the exit: the write is bounded by ledgerSignalEventTimeout, an empty task id
// is skipped (a bare prompt has no row — inventing one would file evidence
// against an unrelated task), and every failure is REPORTED to stderr and
// recorded in the session provenance record's reason (Rule 15: recorded, never
// swallowed).

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nick/gopi/internal/signalrec"
)

const (
	// TaskSignalUnsourcedEvent is the task_events.event value for a run killed
	// by a signal that NO known gopi stop path announced — the 2026-09-12/16
	// mass-kill shape. Built from the two shared constants (Rule 1b/13: the
	// prefix and the verdict word each have exactly one definition, in
	// internal/signalrec, and a consumer references them).
	TaskSignalUnsourcedEvent = signalrec.EventPrefix + signalrec.VerdictUnsourced
	// TaskSignalSourcedEvent is the counterpart for a kill a known gopi stop
	// path announced before delivering it (an operator's `--stop`/`--force`, a
	// bounded-wait child stop, the merger's own stop).
	TaskSignalSourcedEvent = signalrec.EventPrefix + signalrec.VerdictSourced

	// ledgerSignalEventTimeout bounds the ledger write on the shutdown path. A
	// signal handler must never hang the exit it is explaining, and the evidence
	// that matters most (the session record) is already on disk.
	ledgerSignalEventTimeout = 3 * time.Second
)

// RecordSignalVerdict appends the kill-verdict event to the ledger row a run
// was executing. taskID "" is a no-op (see the file comment). It returns the
// error so the caller can report it in the same breath as the exit.
func RecordSignalVerdict(taskID, signal string, sourced bool, intentEvidence string) error {
	id := taskIDNormalize(taskID)
	if id == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), ledgerSignalEventTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return fmt.Errorf("open ledger for the signal verdict: %w", err)
	}
	defer func() { _ = db.Close() }() //stylegate:ignore closing a read-mostly ledger handle after this one best-effort append; the append below is the checked step
	taskEventWrite(ctx, db, id, signalVerdictEvent(sourced), signalVerdictDetail(signal, sourced, intentEvidence))
	return nil
}

// signalVerdictEvent maps the verdict flag to its event name (one vocabulary:
// the event name IS the operator's word, so a `where event = ...` filter needs
// no parsing).
func signalVerdictEvent(sourced bool) string {
	if sourced {
		return TaskSignalSourcedEvent
	}
	return TaskSignalUnsourcedEvent
}

// signalVerdictDetail renders the event's detail: the signal, the verdict in
// the same spelling the session record's JSON field carries, and — for a
// sourced kill — the intent record the verdict rests on (the evidence travels
// with the claim, so the ledger line is auditable on its own).
func signalVerdictDetail(signal string, sourced bool, intentEvidence string) string {
	detail := fmt.Sprintf("%s %s=%t", signal, signalrec.SourcedField, sourced)
	if ev := strings.TrimSpace(intentEvidence); ev != "" {
		detail += " intent=" + ev
	}
	if !sourced {
		detail += " no known gopi stop path announced this kill"
	}
	return detail
}

// ReportSignalVerdictFailure reports a ledger write that did not happen (Rule
// 15b). It exists so the two callers (the worker/merger stop surface) share one
// wording: a reader must be able to tell "the ledger has no event" from "the
// event was never written".
func ReportSignalVerdictFailure(taskID string, err error) {
	fmt.Fprintf(os.Stderr, "[gopi] signal provenance: the ledger event for task %s was NOT written: %v\n", taskID, err)
}
