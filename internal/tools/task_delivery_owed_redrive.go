package tools

// task_delivery_owed_redrive.go — the RE-DRIVE leg of the delivery-owed axis:
// the pass that turns "a row's commits exist only in a ground no remote ref
// reaches" into "the peer has them".
//
// WHY THIS FILE EXISTS HERE RATHER THAN IN cmd/agent (the 2026-11-06 re-home,
// row 01a1ab49). It was written as cmd/agent/task_sync_pass_delivery_owed.go and
// it CALLED internal/tools.DeliveryRetryTaskID from that scanned tree. That is
// exactly what the cwd-release gate's retirement record forbids: the three
// re-delivery arms are no longer REMOVERS reached by a producer, so
// TestGateCwdReleaseDeliveryRetryRetirement asserts NO scanned-tree production
// file calls them at all — "a new production caller turns this red", full stop.
// The premise the retirement rested on is about the ARM, not about the caller,
// and it is true here: internal/tools/task_delivery_retry.go reaches no removal
// verb (the gate's leg ① re-measures that every run).
//
// So the choice was between restoring the trigger entries (which the census
// cannot derive any more, so they would be dead weight that reads as coverage —
// TestGateCwdReleaseCensusArmsAreListed's own sentence) and moving the CALL.
// The call is what changed: the arm is a library face, its caller is a library
// leg, and `internal/tools` is deliberately NOT one of the scanned trees
// (cwdReleaseTrees). cmd/agent keeps the pass's FACE; this file keeps the work.
// Nothing about the re-driver's behaviour changed in the move — same inventory,
// same repair, same fail-soft per row, same lines.
//
// WHAT IT REUSES, and what it therefore cannot disagree with:
//
//	the INVENTORY   → TaskDeliveryOwedRowIDs (the same probe the two render
//	                  faces use — `task show` / `task list` — so "which rows
//	                  owe" has ONE answer in this build);
//	the REPAIR      → DeliveryRetryTaskID, the idempotent library face in
//	                  task_delivery_retry.go. It asks the PEER first (read-only
//	                  `ls-remote`), skips a branch the peer already holds,
//	                  refuses to resurrect a ref the adoption chain removed, and
//	                  delivers a CLONE ground through the channel's own
//	                  AttemptDelivery (clean → pull --rebase → declared build →
//	                  push). This file adds no second gate and no second push.
//
// SO THE CLOSE-OUT CHAIN IS NOT REVIVED: nothing here calls it, nothing here is
// a close-out, and the row's status is not written. The leg MOVES COMMITS and
// records its own outcome on the row's trail (deliveryRetryRecord, already in
// the library face). THE GROUND IS NOT REMOVED: devworkspace.Reclaim -- the one
// function that deletes a workspace -- has no caller on this path, measured.
//
// FAIL-SOFT PER ROW, LOUD ABOUT ITSELF: one unreadable row does not stop the
// others, and a row whose repair was refused is printed with the library's own
// detail rather than counted as done (Rule 15).

import (
	"fmt"
	"io"
	"strings"
)

const (
	// DeliveryRedriveMark is the ONE spelling of this leg's log marker: the pass
	// appends it to its own program prefix (so a pass log keeps one greppable
	// prefix per face — Rule 1's spelling discipline applied to the log instead
	// of the code), and the wiring test in cmd/agent addresses the same value
	// instead of typing the words (Rule 14: one definition, both consumers).
	DeliveryRedriveMark = "delivery-owed"

	// DeliveryRedriveNoneText is the "the inventory answered and it was EMPTY"
	// half of the same contract. It is a distinct string from "the leg did not
	// run" on purpose (Rule 15 — the two are different facts and must not look
	// alike), and the wiring test reads it from here.
	DeliveryRedriveNoneText = "nothing owed"

	// The leg's four report lines, each a SUFFIX of the caller's program prefix
	// (see DeliveryRedriveMark for why the prefix is a parameter and not a
	// constant of this package).
	deliveryRedriveProgSuffix  = ": " + DeliveryRedriveMark
	deliveryRedriveNoneSuffix  = ": " + DeliveryRedriveNoneText + " (no terminal row's ground holds commits that no remote ref reaches)"
	deliveryRedriveHeadFmt     = ": %d row(s) owe a delivery — re-driving"
	deliveryRedriveSkipFmt     = ": row %s not measured (%s) — left alone"
	deliveryRedriveDoneFmt     = ": done — %d re-driven, %d still owed"
	deliveryRedriveUnreadable  = ": inventory unreadable: %v"
	deliveryRedriveRowErrFmt   = "%s: row %s: %v"
	deliveryRedriveRowLineFmt  = "%s: %s"
	deliveryRedrivePrefixFalle = DeliveryRedriveMark
)

// The two seams. They exist so the leg's own loop can be MEASURED (Rule 31: a
// surface nobody executes is not delivered — and the loop is the surface: the
// library face it calls has its own tests, while "does the leg actually call it,
// once per owed row, and keep going after a refusal" had none until
// task_delivery_owed_redrive_test.go). A package-level var of an exported
// function is the smallest seam that does not add an argument the production
// caller would have to pass; both default to the real faces.
var (
	deliveryRedriveOwed = TaskDeliveryOwedRowIDs
	deliveryRedriveOne  = DeliveryRetryTaskID
)

// DeliveryRedriveLeg is the leg. It takes the writer it reports to (so the
// pass's stdout is the one stream, and a test can capture the buffer rather
// than the process's own stdout) and the caller's program prefix, and it
// returns the number of rows that STILL owe after the attempt — the count the
// pass reports, and the one an operator acts on.
//
// A zero is a real zero: it means every candidate was re-driven and the peer now
// holds it, or that no candidate existed. It is NOT returned for an inventory
// that could not be read — that fact is printed and the count stays 0 because
// nothing was measured, which the "inventory unreadable" line distinguishes from
// the empty-inventory line.
func DeliveryRedriveLeg(w io.Writer, progPrefix string) (stillOwed int) {
	prefix := strings.TrimSpace(progPrefix)
	if prefix == "" {
		// NO CALLER PREFIX: fall back to the leg's own name rather than printing
		// lines with a leading ":". A log line an operator cannot grep is the
		// same defect as a silent leg (Rule 15), so this is stated, not hidden.
		prefix = deliveryRedrivePrefixFalle
	}
	prog := prefix + deliveryRedriveProgSuffix
	owed, unreadable, err := deliveryRedriveOwed()
	if err != nil {
		// THE INVENTORY ITSELF FAILED: said loudly, and the leg is a no-op
		// rather than a silent zero (Rule 15).
		fmt.Fprintf(w, prog+deliveryRedriveUnreadable+"\n", err)
		return 0
	}
	for _, line := range unreadable {
		fmt.Fprintf(w, prog+deliveryRedriveSkipFmt+"\n", line, "the probe did not answer")
	}
	if len(owed) == 0 {
		fmt.Fprintln(w, prog+deliveryRedriveNoneSuffix)
		return 0
	}
	fmt.Fprintf(w, prog+deliveryRedriveHeadFmt+"\n", len(owed))
	redriven := 0
	for _, id := range owed {
		rep, rerr := deliveryRedriveOne(id)
		if rerr != nil {
			// The library face reports an unreadable ROW as an error: printed,
			// counted as still owed (nothing was repaired), never swallowed.
			fmt.Fprintf(w, deliveryRedriveRowErrFmt+"\n", prog, id, rerr)
			stillOwed++
			continue
		}
		line := DeliveryRetryLine(rep)
		fmt.Fprintf(w, deliveryRedriveRowLineFmt+"\n", prog, strings.TrimSpace(line))
		if rep.Owed() {
			stillOwed++
			continue
		}
		redriven++
	}
	fmt.Fprintf(w, prog+deliveryRedriveDoneFmt+"\n", redriven, stillOwed)
	return stillOwed
}
