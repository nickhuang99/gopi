package tools

import "fmt"

// THE HOLDER-GONE MARKER — one measured fact, rendered on the LIST surface.
//
// THE DEFECT IT CLOSES (measured 2026-11-05, row 01a1a60e-b419-76d3-ae2c-0a368bdc09f3):
// the row's executor died at 20:17:11 and the row stayed in_progress for 8h38m.
// `gopi task status` answered honestly — `verdict: dead`, `holder: … but its
// holder is GONE` — and `gopi task show` could be asked row by row, but the
// surface an operator actually glances at said:
//
//	[unknown: worktree still present, session did not end, age 9h24m21s]
//
// which is TRUE and also says nothing about the one thing that had been
// MEASURED: nothing names a live process any more. The state ladder withholds
// the `died` verdict when the run left recoverable working state on disk (the
// worktree hatch, taskstate.go) — a deliberate refusal to call a row settled —
// and the fact that the holder is measurably gone was dropped on the floor
// between the measurement and the render.
//
// WHY A MARKER AND NOT A STATUS WRITE. The operator's ruling of 2026-11-05
// (row 01a1a876) states the principle: a process closes its OWN row, and no
// process watches another one's. `gopi task list --sweep` reaching a verdict on
// a dead holder would be exactly the "another process tidies up after you"
// shape that ruling forbids, and it would also be a WRITE hanging off a READ
// verb (the read-side sweep's own header states why a plain `task list` never
// writes). So this file adds NO transition, NO sweep and NO timer (Rule 35): it
// MEASURES at render time, through the liveness ladder the rest of this package
// already uses, and prints what it measured.
//
// THE OPERABLE RESOLUTION stays the operator's one command, and it is legal
// today (taskAllowedTransitions: in_progress → failed/completed/cancelled/
// superseded):
//
//	gopi task update <id> --status failed --reason "holder died, see marker"
//
// ZERO FALSE POSITIVES is the whole contract, and it is bought by reusing the
// existing evidence chain rather than inventing a second one (Rules 13/14):
//
//	recorded identity (pid + start ticks + cmd) → taskRowProcessLiveness, whose
//	  Dead answer means "the kernel has no such process" and whose RECYCLED
//	  answer means the number lives on as a different incarnation — BOTH say
//	  the run this row recorded is over, and both are accepted (see the
//	  recyclable-number note below);
//	the claim stamp's pid           → taskHolderLiveClaimPid + pidAlive, and it
//	  is a VETO on the reading above: the two records are stamped by two
//	  different processes (executor incarnation vs claimant), so they disagree
//	  by construction and each one alone is only half the evidence. A claimant
//	  this host measures ALIVE suppresses the marker — that is the defect this
//	  epic closes, and the reason ② can no longer be consulted only when ① is
//	  absent;
//	no recorded identity            → pidGone on the claim stamp's pid, which
//	  answers true only when the probe PROVED the process gone (an unanswerable
//	  probe answers false and therefore marks nothing — Rule 15);
//	foreign stamp, malformed stamp, or a stamp from a PREVIOUS claimant
//	  → nothing to judge on this host, so nothing is claimed (Rule 15a) — and
//	  the identity reading is then allowed to stand alone, because no evidence
//	  contradicts it.

const (
	// taskHolderGoneMarkerFmt is the suffix appended to an in_progress row's
	// rendered state when the recorded pid names NO process: ONE %d, that pid.
	// It BEGINS with StatusWordMarkerSep (statusword.go), the ONE definition of
	// the boundary a `status:` reader splits on — writing the separator inline
	// here would let the renderer and its readers disagree about where the
	// status WORD ends (Rule 14). The leading space belongs to the separator, so
	// the marker's own text starts at "holder".
	taskHolderGoneMarkerFmt = StatusWordMarkerSep + "holder dead (pid %d gone)"

	// taskHolderRecycledMarkerFmt is the SAME fact measured the other way: the
	// pid number still exists, but as a different incarnation than the row
	// recorded — so the run is over while the number is not free. It gets its
	// own wording because "gone" would be a false statement about a live
	// number (Rule 15: say what was measured, not a near-enough word).
	taskHolderRecycledMarkerFmt = StatusWordMarkerSep + "holder dead (pid %d recycled)"
)

// taskHolderGoneMarker renders the measured-holder-gone suffix for a row whose
// state label has ALREADY been decided. The one case it suppresses is the
// `died` label, whose own text is the same claim — the caller passes that
// verdict rather than this function re-deriving it, so the two surfaces cannot
// disagree about whether the row already says the holder is gone.
func taskHolderGoneMarker(t *Task, state string) string {
	if t == nil || t.Status != taskStatusInProgress || state == taskStateDied {
		return ""
	}
	pid, proof := taskHolderGoneProof(t)
	return taskHolderGoneRender(pid, proof)
}

// taskHolderGoneStatusMarker is the same fact for the `task show` STATUS line,
// which renders before the state label and has no label to defer to: every
// in_progress row gets the marker when it was measured, `died` included.
func taskHolderGoneStatusMarker(t *Task) string {
	if t == nil || t.Status != taskStatusInProgress {
		return ""
	}
	pid, proof := taskHolderGoneProof(t)
	return taskHolderGoneRender(pid, proof)
}

// taskHolderGoneRender turns a PROOF into the marker text, and an empty proof
// into nothing at all — the one place the two wordings are chosen, so the list
// and `task show` cannot spell one measurement two ways (Rule 14).
func taskHolderGoneRender(pid int, proof string) string {
	switch proof {
	case taskHolderProofGone:
		return fmt.Sprintf(taskHolderGoneMarkerFmt, pid)
	case taskHolderProofRecycled:
		return fmt.Sprintf(taskHolderRecycledMarkerFmt, pid)
	}
	return ""
}

// taskHolderGoneProof answers "is the process that holds this row PROVABLY
// gone?", returning that pid. Two evidence shapes, strongest first:
//
//	① the row RECORDED the incarnation — the identity ladder decides, and BOTH
//	   its Dead answer (the pid names no process at all) and its Recycled
//	   answer (the number survives as a DIFFERENT incarnation) are accepted:
//	   each says the run this row recorded is over. The number being alive as
//	   a stranger is not the recorded run continuing, and the state ladder
//	   already decides the same way ("its number was reused, or it is gone.
//	   That is a decidable row") — refusing Recycled here while the
//	   label calls it dead would let ONE row carry two verdicts (Rule 14).
//	   A recycled row that ALSO has a worktree on disk still renders
//	   `unknown: worktree still present`, which is exactly the shape this
//	   marker exists to annotate (measured 2026-11-05, row 01a1a862);
//	② the row carries no identity — the claim stamp's pid, probed through
//	   pidGone, which refuses (answers false) on every unmeasurable reading.
//
// THE TWO FACTS ARE INDEPENDENT RECORDS, NOT ONE, and that is why ① may not
// decide alone (defect measured 2026-11-08, rows 01a1bb5b-0fd3 / 01a1bb2d-e7bc).
// The ledger records the run's life TWICE, through two different processes:
//
//	proc_pid / proc_start_ticks (①)  stamped by each EXECUTOR INCARNATION — the
//	                                 short-lived child a worker spawns per
//	                                 attempt. It is renewed whenever the
//	                                 execution body is replaced, e.g. after a
//	                                 checkpoint resume or a tool restarted in a
//	                                 fresh process;
//	claimed_by (②)                   stamped by the CLAIMANT — the long-lived
//	                                 `gopi worker` that took the row. It is written
//	                                 once at claim time and does not move when the
//	                                 executor child is replaced.
//
// So the two numbers are, BY CONSTRUCTION, different pids naming different
// processes — and the executor's death therefore does NOT prove the run is over.
// A row can carry "the child that wrote my last identity stamp is gone" and "the
// worker that owns me is alive" at the same time; the first is a fact about one
// ATTEMPT, the second is the fact about the run. Consulting ① alone printed
// `holder dead` over a run that was demonstrably working (the claim pid's
// /proc/<pid>/cwd stood in the row's own session directory).
//
// So the assertion is made only when ALL recorded evidence agrees that the run
// ended: ① must have measured a death AND (when this host can read ②) the
// claimant must not be measurably alive. The direction of every unmeasurable
// reading is preserved — "I could not measure it" adds no death claim, it only
// removes a veto.
func taskHolderGoneProof(t *Task) (int, string) {
	if t.HasProcIdentity && t.ProcPid > 0 {
		// Two different proofs of the SAME fact: the recorded incarnation is
		// over. Dead = the number names nothing; Recycled = the number names a
		// different incarnation. Neither is the process this row recorded, and
		// the ORDER matters only for the wording — they are one verdict.
		proof := taskHolderIdentityProof(t)
		if proof == "" {
			return 0, ""
		}
		// THE CONTRADICTION CHECK (this epic's fix): a claimant this host MEASURES
		// alive is evidence the run continues, so ① alone may not call it dead.
		// Unmeasurable claimant readings (no stamp, a foreign host, a stale stamp
		// naming a previous claimant, an unanswerable probe) leave ① standing.
		//
		// THE ONE EXEMPTION, and it is a measurement rather than a convenience: a
		// claim stamp naming the SAME number as the recorded incarnation is not a
		// second record at all — it is the same process, so its "alive" reading is
		// the very stranger the ladder already answered Recycled about, and it
		// carries no new evidence. Letting it veto would silence every recycled row,
		// which taskstate.go settled the other way ("its number was reused, or it
		// is gone. That is a decidable row"). The veto therefore requires TWO
		// DIFFERENT numbers — the shape in which the two records really are two
		// processes.
		if pid, ok := taskHolderLiveClaimPid(t); ok && pid != t.ProcPid && pidAlive(pid) {
			return 0, ""
		}
		return t.ProcPid, proof
	}
	pid, ok := taskHolderLiveClaimPid(t)
	if !ok {
		return 0, ""
	}
	if !pidGone(pid) {
		return 0, ""
	}
	return pid, taskHolderProofGone
}

// taskHolderIdentityProof is ① of taskHolderGoneProof as its own reading: which
// death, if any, the row's RECORDED incarnation proves. Empty means the ladder
// did not measure a death (Running, Unknown, NotMeasured) — and an unmeasured
// arm never becomes a death claim here (Rule 15).
func taskHolderIdentityProof(t *Task) string {
	switch taskRowProcessLiveness(t).State {
	case TaskProcessStateDead:
		return taskHolderProofGone
	case TaskProcessStateRecycled:
		return taskHolderProofRecycled
	}
	return ""
}

// taskHolderLiveClaimPid is ②: the pid this row's claim stamp names, and whether
// this host may act on it at all. ok=false for every case where the number means
// nothing HERE — no stamp, an unparsable or FOREIGN stamp (that pid names a
// different process on that host), or a stamp that PREDATES this row's start and
// therefore names a previous claimant (taskzombie.go's claimStamp* axis). Both
// emitters of the proof share it, so "no claim stamp" and "the claimant is gone"
// can never be spelled two ways (Rule 14).
func taskHolderLiveClaimPid(t *Task) (int, bool) {
	_, pid, local := claimHolder(t.ClaimedBy)
	if !local || pid <= 0 {
		return 0, false
	}
	if claimStampFreshness(t) == claimStampStale {
		return 0, false
	}
	return pid, true
}

// The two measured shapes, named once (Rule 1) and consumed by
// taskHolderGoneRender's switch.
const (
	// taskHolderProofGone: the recorded pid names no process any more.
	taskHolderProofGone = "gone"
	// taskHolderProofRecycled: the recorded pid number exists, held by a
	// DIFFERENT incarnation than the row recorded.
	taskHolderProofRecycled = "recycled"
)
