package main

// THE SHARED SEAM FOR A FAILED-BRANCH WRITEBACK (epic writeback-lost,
// 2026-10-27).
//
// writebackOutcome returns the LOCAL ledger's verdict on the terminal write
// (see its doc comment: the remote legs are best-effort and are not part of
// the returned error). The success arm has read that value since epic
// worktree-dirty-residue — taskRunCloseoutLanded turns a non-landing close-out
// into an escalation line and a non-zero exit. The FIVE failure arms did not:
// each one called
//
//	writebackOutcome(target, tools.TaskStatusFailed, …, opts.reportTo, opts.repo)
//
// as an expression statement, dropped the error, printed the child's failure
// and returned. When the ledger refused that write — an illegal transition, a
// busy/read-only ledger, a process killed mid-write — the row stayed
// `in_progress` and NO surface said why: the wrapper's own stderr line belongs
// to a process that has already exited, and the two gates that do record their
// refusals (Rule 30's dirty/residue arms) guard only the DELIVERY claims.
//
// Measured on this host's ledger before the fix: four rows whose holder pid was
// long dead, three labelled `[died …]` and one carrying no claim stamp at all,
// with nothing on any of them accounting for the missing terminal write.
//
// This seam is the one place a failure-branch writeback is allowed to happen.
// It routes a refused write through tools.TaskWritebackLost, which records the
// loss on the row's own trail AND fires the alarm, and it prints the resulting
// sentence on BOTH streams the run's reader may be looking at: the run log
// (execRunFacts, the file an evidence reader finds later) and this process's
// stderr. It is DELIBERATELY void: on these arms the run is already failing and
// about to return, so there is no second decision left to make — the value that
// must not be dropped is the RECORD, and the record is what the seam owns.
//
// What it must never become: a disposer. The row stays open, `in_progress`, with
// its dead holder — that is the operator's ruling (devdoc/zombie-sweep-removed.md,
// 2026-10-26). Reporting is the deliverable; rewriting the status to make the
// report unnecessary would be the retired sweep under a new name.

import (
	"fmt"
	"io"
	"os"

	"github.com/nick/gopi/internal/tools"
)

// writebackFailure runs a terminal writeback on a FAILING branch and gives a
// refused write a record that outlives this process.
//
// The statement-level call is the contract: a caller that needs the ledger's
// verdict for a DECISION (the success arm's landed check) must call
// writebackOutcome itself, as cmd/agent/task_run_success_path_ledger_read_test.go
// documents. Every failure arm goes through here.
func writebackFailure(taskID, status, resultSummary, reportTo, repo string) {
	wbErr := writebackOutcome(taskID, status, resultSummary, reportTo, repo)
	if wbErr == nil {
		return
	}
	loss := tools.TaskWritebackLost(taskID, status, wbErr)
	// The run log first (it is the copy that survives the session directory),
	// then the spawning stream — the same two-destination shape the ground
	// refusals above use, for the same reason: the reader who does not know
	// the log's path yet cannot be told through the file it names.
	//
	// The second write is SKIPPED when the two destinations are the same one:
	// before the spawn seam runs, execRunFacts is os.Stderr itself (the latch's
	// default), and printing unconditionally put the sentence on the operator's
	// stream TWICE — measured on 2026-10-29 in a live refusal proof. One fact,
	// one line per destination that is actually a separate destination.
	fmt.Fprintf(execRunFacts, "%s\n", loss.Line)
	if !writebackFailureSameSink(execRunFacts) {
		fmt.Fprintln(os.Stderr, loss.Line)
	}
}

// writebackFailureSameSink reports whether the run's facts writer IS the
// spawning stderr — the spawn seam's own default (execRunFacts = os.Stderr)
// before it has opened this run's log. A writer of any other type is a distinct
// destination (the run's log, or a test's recorder), so the second write runs.
func writebackFailureSameSink(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && f == os.Stderr
}
