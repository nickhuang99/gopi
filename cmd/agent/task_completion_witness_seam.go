package main

// task_completion_witness_seam.go — THE CLOSE-OUT'S COMPLETION-WITNESS SEAM.
//
// WHAT IT ADDS, AND WHY IT IS A SEAM RATHER THAN A GATE. The epic this closes
// (01a182bf) asks one question the ledger could not answer: of the rows that
// read `completed` after 20c15007c, which ones could be told apart from a claim
// that delivered nothing? The measurement lives in
// internal/tools/task_completion_witness.go (the criterion, three arms, three
// valued); this file is the ONE place a run tells it the two facts it cannot
// derive for itself.
//
//	① taskRunConclusion — the sentence the clone channel returned for the
//	   `nothing to deliver` exit. It is the CONCLUSION the epic requires the
//	   ledger to be able to name: a run that measured its ground and found it
//	   held nothing past the integration point completed on a MEASUREMENT, and
//	   that is a different fact from a claim with nothing behind it.
//
//	② the write-back call — after the terminal write has LANDED (never before:
//	   the arm annotates a completed row, it does not gate one), the arm reads
//	   the row's witnesses and records ONE trail entry when there are none. It
//	   changes no status.
//
// THE CONSTRAINT IT OBEYS. `completed` is never turned back into `failed`: the
// witnessless verdict RECORDS the absence and the claim stands (row 01a18244
// fixed a defect in exactly the other direction — an items=0 counter-proof run
// recorded as a failed delivery). And a row with no repository at all is
// `unknown`, never `witnessless` — "this row names no repo" is a different fact
// from "this row's repo holds nothing" (Rule 15).
//
// The latch is a package-level variable for the same reason
// taskExecutorOutputLogPath is: one `task run` / worker round per process, so
// there is exactly one value to carry and no second reader to synchronise.

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/tools"
)

// taskRunConclusion is the CURRENT run's own measurement, when its close-out
// took the `nothing to deliver` exit ("" otherwise). Single-threaded per
// process — the same latch reasoning as workerWriteback and
// taskExecutorOutputLogPath.
var taskRunConclusion string

// recordCompletionWitness runs the completion witness for a row this process
// has just written, and reports its verdict on the run's fact stream.
//
// It is called for EVERY terminal write (the call site does not pre-filter): the
// arm itself ignores a status that claims no delivery, so the "which statuses
// are judged" rule lives in ONE place — `taskStatusClaimsDelivery` — rather than
// being spelled a second time here (Rule 14).
//
// A failure is REPORTED, never swallowed (Rule 15): the witness is a read plus a
// best-effort append, and a caller that could not measure must say so rather
// than let the row look judged.
func recordCompletionWitness(taskID, status string) {
	if taskID == "" {
		return
	}
	out, err := tools.TaskCompletionWitnessAtWriteback(taskID, status, taskRunConclusion)
	if err != nil {
		fmt.Fprintf(execRunFacts, "%s: completion witness for %s failed (the claim stands): %v\n",
			taskProgName, taskID, err)
		return
	}
	if out.Verdict == "" {
		return
	}
	fmt.Fprintf(execRunFacts, "%s: completion witness for %s: %s\n", taskProgName, taskID, out.Verdict)
}
