package main

// THE SUCCESS ARM'S LAST CHECK: did the close-out actually LAND?
//
// THE DEFECT (epic worktree-dirty-residue, 2026-09-27; measured on the live host
// as three parked worktrees — 01a113ac, 01a113c5, 01a11494).
//
// runTaskRunOnce decides a run's outcome from the child's exit code plus
// writebackOutcome. The ledger read it needs EXISTS — tools.TaskRowTerminalAt —
// but only on the FAILURE arm (`if runErr != nil`). The success arm calls
//
//	writebackOutcome(target, tools.TaskStatusCompleted, …)
//
// and then unconditionally prints `task <id> completed (elapsed …)` and returns
// nil. writebackOutcome returns nothing, so a REFUSED write is a stderr line
// ("writeback failed (continuing)") and nothing else: exit code, stdout and the
// return value all still say success, while the ledger kept the row OPEN.
//
// The refusal is not exotic — it is Rule 30's own close-out gate, which refuses
// a `completed` writeback for a run whose OWN worktree is still dirty
// (reclaimRunTrackedDirtRefusal / reclaimRunResidueRefusal, internal/tools). So
// the exact shape the residue machinery produces is: the guard correctly refuses
// the terminal write, the worktree is deliberately kept, and the operator is
// told the task completed. Nothing is left to clean it up, because by the
// ledger's own account the run never finished.
//
// WHAT IS CHECKED HERE. Not the row's terminality at the child's exit — that
// instant is structurally the wrong place to ask (measured while standing in
// this arm, and the reason the child-side close-out it once named was deleted by
// task 01a16f7c: at the child's exit the only writer of a successful run's row
// has not run yet, so "non-terminal" there is the HEALTHY state, and a check at
// that instant reclassifies every healthy run). The question asked here is the
// one the discriminator actually is: after MY writeback, is the row terminal?
// A healthy run: yes (the wrapper is the sole writer of a successful run's
// row). A refused close-out: no — and that is precisely the fact this arm used
// to throw away.
//
// WHAT IT DOES NOT DO (Rule 30's line, kept): nothing is committed, stashed,
// cleaned or deleted, and no second status is invented. The refused row stays
// exactly where the gate left it; this arm only stops the wrapper from calling
// that a success — the exit code, the console and the ledger now agree.

import (
	"fmt"

	"github.com/nick/gopi/internal/tools"
)

// taskRunCloseoutEscalationFmt is the ONE operator-facing shape of the
// escalation (Rule 1b): the row, what the ledger holds instead of a terminal
// status, and why. It is a REPORT, not a remedy — every remedy belongs to the
// gate that refused (whose own sentence is quoted on stderr beside it).
const taskRunCloseoutEscalationFmt = taskProgName + ": task %s was NOT closed out — %s\n"

// taskRunCloseoutEscalationMarker is the stable phrase every escalation line
// carries, so an operator (and the regression test) greps one string instead of
// parsing a sentence (Rule 1b).
const taskRunCloseoutEscalationMarker = "was NOT closed out"

// taskRunCloseoutNotLandedErrFmt is the error the wrapper RETURNS, so the
// process's exit code carries the same verdict as the line above.
const taskRunCloseoutNotLandedErrFmt = "gopi task run: task %s NOT closed out — %s"

// taskRunCloseoutWritebackFact is the middle of the sentence, worded once so
// the line and the diff can never disagree (Rule 1b): the ledger read back,
// plus — when the write reported one — its own error, which on the Rule 30
// refusal is the gate's full sentence naming the worktree and its entries.
const taskRunCloseoutWritebackFact = "the ledger holds %q and the completed writeback reported: %v"

// taskRunCloseoutLanded answers the success arm's one question: after this
// wrapper's own writeback, does the LEDGER hold a decided row?
//
// It returns the same triple tools.TaskRowTerminalAt does, plus the writeback's
// own error, and the combination is what "landed" means:
//
//	landed  ⇔  the write reported no error  AND  the row is terminal
//
// An unreadable ledger is NOT "landed": the wrapper cannot establish its own
// claim, and Rule 15 forbids promoting an unreadable fact into a success. The
// caller escalates in every not-landed case; no status is written here.
func taskRunCloseoutLanded(taskID string, wbErr error) (bool, string, error) {
	closed, status, err := tools.TaskRowTerminalAt(taskID)
	if err != nil {
		return false, "", err
	}
	return closed && wbErr == nil, status, nil
}

// taskRunCloseoutEscalate reports a close-out that did not land and returns the
// error that gives the wrapper a matching exit code.
//
// Order of the two facts matters to the reader: WHAT the ledger holds (the
// door the run must come back through) first, THEN the writeback's own error —
// which, on the Rule-30 refusal, is the gate's full sentence naming the
// worktree, the `git status --porcelain` entries and the two remedies.
func taskRunCloseoutEscalate(taskID string, wbErr error, status string, statusErr error) error {
	var why string
	switch {
	case statusErr != nil:
		why = "the ledger could not be read back to confirm the close-out: " + statusErr.Error()
	case status != "" && wbErr != nil:
		why = fmt.Sprintf(taskRunCloseoutWritebackFact, status, wbErr)
	case status != "":
		why = fmt.Sprintf("the ledger still holds %q — the close-out did not land", status)
	case wbErr != nil:
		why = "the completed writeback reported: " + wbErr.Error()
	default:
		why = "the ledger holds no decided status for this row"
	}
	// THE LINE THAT EXPLAINS THE RUN GOES WHERE THE RUN'S RECORD IS. This arm
	// used to print to os.Stderr alone, and that is the measured reason row
	// 01a16f7c's own output.log carries ZERO hits for its close-out verdict
	// while the row read `completed`: the one sentence that says WHY the run did
	// not close was the one sentence missing from the record an evidence reader
	// opens first (the session dump's grep for "was NOT closed out" returned
	// nothing on 2026-10-26). execRunFacts is the run's own log PLUS the
	// spawning stderr whenever that log exists (cli_tasks_exec_output.go), so
	// the terminal still sees it and the file finally does too — the same face
	// every other close-out line already uses.
	fmt.Fprintf(execRunFacts, taskRunCloseoutEscalationFmt, taskID, why)
	return fmt.Errorf(taskRunCloseoutNotLandedErrFmt, taskID, why)
}
