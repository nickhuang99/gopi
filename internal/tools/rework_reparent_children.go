package tools

// rework_reparent_children.go — the branch-rework family's ONE answer to
// "the row I just superseded had CHILDREN".
//
// THE DEFECT (this file's whole reason to exist). Every rework lane in this
// package files a NEW row R for work that was refused — a branch whose merge
// did not build, a branch the contract gate refused, a manifest duplicate, a
// branch over the merge ceiling, a merge the verify gate REVERTED. In each of
// those cases an EXISTING row A carried the work that was just falsified, and
// A commonly has children: a row whose `parent_task_id` is A depends on A's
// work being done. Rule 33 releases a child the moment its parent reaches
//
//	{completed, verified, superseded}   (taskParentReleaseStatuses)
//
// so A's own `completed` writeback — which is exactly what happened to A,
// that is WHY the branch went to rework — releases every child of A onto a
// premise the rework just falsified. The children then start (or are handed
// to a worker) against work that is being redone RIGHT NOW, and the ledger
// records nothing that says so.
//
// THE FIX IS THE EDGE, NEVER THE RELEASE SET. Tightening Rule 33 — dropping
// `completed` from the release set, or adding a "rework pending" clause to
// taskParentGateDenied — would park EVERY child of every completed row in
// the system and would make the ordinary case (a parent finishes, its
// children start) impossible to express. The dependency those children
// actually hold is not "A is completed" but "A's work is done and shipped",
// and the row that now carries that promise is R. So the edge moves:
// `parent_task_id: A → R`, through the ONE existing writer of that column,
// TaskReparent (CAS + append-only trail + ghost/self/cycle refusals). Nothing
// in this file writes the column itself, writes a status, or touches
// taskParentGateDenied / taskParentReleaseStatuses.
//
// THE SHARED IMPLEMENTATION, AND ITS CALL SITES. ReworkReparentChildren is
// the ONE implementation; every lane that mints a branch-rework row reaches
// it, so a new lane cannot be added without inheriting the behaviour:
//
//	build gate          buildGateSubmitRework      (task_merge.go)
//	contract gate       contractGateSubmitRework   (task_merge.go)
//	manifest gate       manifestUnionGateSubmitRework (task_merge.go)
//	verify revert       MergeVerifyFileRework      (verify_gate.go)
//
// TWO LANES LEFT THIS TABLE ON 2026-10-31 (task 01a18d61): the merge ceiling
// (MergeCeilingFileRework, merge_ceiling_rework.go) and the hand-over
// escalation (MergeHandoverEscalate, merge_handover_escalate.go) were deleted.
// MEASURED before deleting, not assumed: each file's own identifiers had no
// other production reference (`grep -rnw` over cmd/ and internal/, e.g.
// mergeCeilingDiffName / mergeCeilingPayload / mergeHandoverEscalatePayload ->
// no hits outside the file itself), and their one former caller, the merge
// engine, is the machinery this same retirement is unwinding. The table above
// keeps only the lanes that still mint a rework row, so a reader is not sent to
// a file that does not exist (Rule 15).
//	dirty-holder salvage dirtyHolderSalvageOne     (dirty_holder_salvage.go)
//
// The first four plus the salvage lane know only the BRANCH, not the row that
// owned it; they go through reworkReparentFromBranch, which resolves the
// owner through the existing branch→row lookup (TaskForBranch +
// TaskForBranchResolvedRow — the merger's own answer to "who owns this
// branch", Rule 14: no second lookup is invented here). The verify lane knows
// the row EXACTLY (MergeVerifyReworkOptions.OldTaskID) and calls the shared
// implementation directly.
//
// IDEMPOTENCE, CYCLES, IN-FLIGHT CHILDREN, CROSS-HOST — each is a decision
// this file makes and records, never a silent arm:
//
//   - IDEMPOTENT. A child already pointing at R is reported "unchanged" and
//     writes nothing (TaskReparent's own unchanged arm — no trail entry
//     either). Re-running the same rework therefore moves nothing twice.
//   - CYCLES. Nothing here re-implements a ring check: TaskReparent climbs
//     the parent chain inside the same transaction as its conditional UPDATE,
//     so an edge that would close a ring is refused BY NAME and the refusal
//     is recorded here (a rework row filed as a child of the very row it is
//     meant to supersede is the reachable shape).
//   - IN-FLIGHT CHILDREN MOVE TOO. A claimed / in_progress child is moved and
//     recorded: it is already running on the falsified premise, and the only
//     arm that ever repairs that is the edge it will read next time anything
//     about it is judged. Leaving it pointed at A would keep the lie in the
//     ledger.
//   - TERMINAL CHILDREN ARE LEFT ALONE, BY NAME. A completed / failed /
//     verified child owes no work through the edge; rewriting its parent
//     would rewrite history for no gate benefit. It is counted and its
//     sentence says which status decided it.
//   - CROSS-HOST: A HANDED-OFF CHILD IS NEVER REWRITTEN HERE, AND SAYS SO.
//     A row whose tags carry reworkHandoffTag had its work MIGRATED to a peer
//     (`gopi task send --cancel-source`); the local row is the cancelled
//     record of a move and the peer's own ledger holds the row that the work
//     depends on now. Rewriting the local edge would change nothing on the
//     machine that will run the work, and would quietly contradict the
//     marker. So this file reports it in its own sentence — named, counted,
//     never folded into "unchanged" (Rule 15: unreadable/handled-elsewhere is
//     not the same fact as false) — and the report says the local edge is
//     moot. That IS the cross-host disposition: explicit, and deliberately
//     no-op rather than a half-write that reaches one ledger out of two.
//
// WHAT THIS FILE DELIBERATELY DOES NOT DO. It writes no status (that is the
// rework lane's own act), mints no row, schedule anything, and adds no
// process, timer, script or state file (Rule 35): it runs inside the existing
// submit that already fires, using the writer that already exists.

import (
	"context"
	"fmt"
	"strings"
)

const (
	// reworkReparentMarker opens every line this file's callers log, so one
	// grep over a merger log finds the whole family (Rule 1b: a lane
	// identifies itself with a constant, never a literal at the call site).
	reworkReparentMarker = "rework-child-reparent: "

	// reworkReparentReasonFmt is the --reason EVERY move carries, and it is
	// the reason an operator reading `task show`/the trail will find: which
	// row was superseded, by which row, and why the edge had to follow. A
	// reparent without a reason is refused by the writer, so this is not
	// optional prose (Rule 15).
	reworkReparentReasonFmt = "rework supersession: %s was reworked as %s — the branch's work was refused and is being redone, " +
		"so the dependency this row held on %s is no longer satisfied by it (leaving the edge on the old row would let its release status start this row on the premise the rework just falsified)"

	// reworkReparentNoEdgeFmt is the arm where a caller handed in an
	// incomplete pair: nothing can be moved, and the caller is told which
	// argument was missing rather than being handed an empty success.
	reworkReparentNoEdgeFmt = "nothing reparented: %s"

	// reworkReparentSummaryFmt is the ONE sentence every call site logs (and
	// the tests assert on): the two ends, the child count, and the
	// disposition of every child — including the arms that moved nothing.
	reworkReparentSummaryFmt = "%s → %s: %d child(ren) read — moved %d, already on the rework row %d, handed off to a peer %d, terminal (no work owed) %d, failed %d"

	// The per-child sentences. Each names the child, so a reader can act on
	// it without re-reading the ledger.
	reworkReparentMovedFmt      = "child %s: parent_task_id %s -> %s"
	reworkReparentAlreadyFmt    = "child %s already points at %s — unchanged (idempotent: nothing written, no trail entry)"
	reworkReparentTerminalFmt   = "child %s is %s (terminal) — no work is owed through the edge, left untouched"
	reworkReparentHandedOffFmt  = "child %s carries the %q marker: its work was MIGRATED to a peer, so the local edge is moot and is NOT rewritten here — the peer's own ledger holds the row the work sits under now (this is the cross-host disposition: named, and deliberately no-op)"
	reworkReparentFailedFmt     = "child %s: reparent FAILED (%v) — the edge still points at %s, so that row's release status would still start this child on the falsified premise; fix it by hand or re-run the rework"
	reworkReparentSelfChildFmt  = "child %s IS the rework row itself — skipped (an edge a row holds on itself is not a dependency)"
	reworkReparentNoOwnerFmt    = "no ledger row owns branch %q in %q — nothing to reparent (a branch no row owns has no children to release)"
	reworkReparentLookupErrFmt  = "branch→row lookup for %q failed (%v) — nothing reparented; the review is NOT silently skipped, it is UNPERFORMED"
	reworkReparentNoLocalRowFmt = "the rework row has no local ledger row (the submit was mirrored to a peer) — nothing reparented here; the row that carries the work, and the edges under it, are the peer's"

	// reworkReparentChildScanCap bounds the children ONE row's review reads.
	// A row with more children than this is pathological; the read REFUSES
	// rather than acting on a truncated set (Rule 15 — a partial scan that
	// silently moves some children is worse than one that moves none).
	reworkReparentChildScanCap = 500
)

// ReworkReparentReport is what ONE review did: the two ends, the children it
// read, and the disposition of every one of them. It is a VALUE rather than a
// log side effect so a test drives the real implementation and asserts the
// counts, and so a caller renders the same sentence the log carries (Rule 14:
// one spelling of the outcome).
type ReworkReparentReport struct {
	// Reworked is the superseded row ("" when the caller could not name one).
	Reworked string
	// Rework is the row that now carries the work.
	Rework string
	// Moved / Already / HandedOff / Terminal / Failed are the child ids by
	// disposition, in ledger order. A child appears in exactly one of them.
	Moved     []string
	Already   []string
	HandedOff []string
	Terminal  []string
	Failed    []string
	// Notes carries one sentence per child (plus the read's own notes), the
	// readable half: a count alone never says WHICH child was left behind.
	Notes []string
	// Refused is set when this file did nothing for a reason that is not a
	// child's disposition — an unnamed row, a missing pair, an unreadable
	// ledger. It is a FIELD rather than an empty report, because "there was
	// nothing to do" and "I could not look" must never render alike (Rule 15).
	Refused string
}

// Children is how many children this review read (moved or not).
func (r ReworkReparentReport) Children() int {
	return len(r.Moved) + len(r.Already) + len(r.HandedOff) + len(r.Terminal) + len(r.Failed)
}

// Render is the ONE sentence a call site logs and a test asserts.
func (r ReworkReparentReport) Render() string {
	if r.Refused != "" {
		return reworkReparentMarker + r.Refused
	}
	return reworkReparentMarker + fmt.Sprintf(reworkReparentSummaryFmt,
		r.Reworked, r.Rework, r.Children(),
		len(r.Moved), len(r.Already), len(r.HandedOff), len(r.Terminal), len(r.Failed))
}

// ReworkReparentChildren re-points every LIVE child of reworkedID onto
// reworkID, through TaskReparent (the ONE writer of that column).
//
// It reads the children ONCE, then moves each one on its own — a child that
// refuses (a cycle, a concurrent move, an unreadable row) does NOT abort the
// others, because the alternative is that one pathological child leaves every
// other child released onto the falsified premise. Each refusal is recorded by
// name (Rule 15).
func ReworkReparentChildren(reworkedID, reworkID, reason string) ReworkReparentReport {
	rep := ReworkReparentReport{Reworked: strings.TrimSpace(reworkedID), Rework: strings.TrimSpace(reworkID)}
	if rep.Reworked == "" || rep.Rework == "" {
		rep.Refused = fmt.Sprintf(reworkReparentNoEdgeFmt,
			"the superseded row and the rework row must both be named — unnamed: "+reworkReparentEndsLabel(rep.Reworked, rep.Rework))
		return rep
	}
	if rep.Reworked == rep.Rework {
		rep.Refused = fmt.Sprintf(reworkReparentNoEdgeFmt,
			"the superseded row and the rework row are the same row ("+rep.Rework+") — nothing to move")
		return rep
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		rep.Refused = fmt.Sprintf(reworkReparentNoEdgeFmt,
			"a reparent must carry a reason (the trail records WHY the dependency edge moved)")
		return rep
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		rep.Refused = fmt.Sprintf(reworkReparentNoEdgeFmt, "the ledger could not be opened: "+err.Error())
		return rep
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, COALESCE(tags, '') FROM tasks WHERE parent_task_id = ? ORDER BY id`,
		rep.Reworked)
	if err != nil {
		rep.Refused = fmt.Sprintf(reworkReparentNoEdgeFmt, "the children of "+rep.Reworked+" could not be read: "+err.Error())
		return rep
	}
	type childRow struct{ id, status, tags string }
	var children []childRow
	for rows.Next() {
		var c childRow
		if err := rows.Scan(&c.id, &c.status, &c.tags); err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			rep.Refused = fmt.Sprintf(reworkReparentNoEdgeFmt, "a child row could not be read: "+err.Error())
			return rep
		}
		children = append(children, c)
		if len(children) > reworkReparentChildScanCap {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			rep.Refused = fmt.Sprintf(reworkReparentNoEdgeFmt,
				fmt.Sprintf("%s carries more than %d children — refusing to act on a truncated set", rep.Reworked, reworkReparentChildScanCap))
			return rep
		}
	}
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
	if err := rows.Err(); err != nil {
		rep.Refused = fmt.Sprintf(reworkReparentNoEdgeFmt, "the children of "+rep.Reworked+" could not be read: "+err.Error())
		return rep
	}

	for _, c := range children {
		switch {
		case c.id == rep.Rework:
			rep.Notes = append(rep.Notes, fmt.Sprintf(reworkReparentSelfChildFmt, c.id))
			rep.Already = append(rep.Already, c.id)
		case taskHasTagToken(c.tags, reworkHandoffTag):
			// The cross-host arm, tested FIRST and regardless of status: a row
			// whose work moved to a peer must not have its local edge rewritten
			// (the peer's ledger is where the work reads its dependencies), and
			// the marker says so in so many words.
			rep.HandedOff = append(rep.HandedOff, c.id)
			rep.Notes = append(rep.Notes, fmt.Sprintf(reworkReparentHandedOffFmt, c.id, reworkHandoffTag))
		case !isLiveTaskStatus(c.status):
			rep.Terminal = append(rep.Terminal, c.id)
			rep.Notes = append(rep.Notes, fmt.Sprintf(reworkReparentTerminalFmt, c.id, c.status))
		default:
			out, err := TaskReparent(c.id, rep.Rework, reason)
			if err != nil {
				rep.Failed = append(rep.Failed, c.id)
				rep.Notes = append(rep.Notes, fmt.Sprintf(reworkReparentFailedFmt, c.id, err, rep.Reworked))
				continue
			}
			switch {
			case strings.HasPrefix(out, reparentUnchangedPrefix):
				// TaskReparent's own idempotent arm: the edge the write asked
				// for is the edge the row already carries. Unreachable from a
				// single-threaded review (a child READ under A cannot already
				// point at R), and kept as the truth-check that makes a
				// concurrent re-entry report what the WRITER did instead of
				// claiming a move it did not make (Rule 15).
				rep.Already = append(rep.Already, c.id)
				rep.Notes = append(rep.Notes, fmt.Sprintf(reworkReparentAlreadyFmt, c.id, rep.Rework))
			default:
				rep.Moved = append(rep.Moved, c.id)
				rep.Notes = append(rep.Notes, fmt.Sprintf(reworkReparentMovedFmt, c.id, rep.Reworked, rep.Rework))
			}
		}
	}
	return rep
}

// reworkReparentEndsLabel names WHICH end of the pair was missing, so the
// refusal sentence is actionable rather than "something was empty" (Rule 15).
func reworkReparentEndsLabel(reworkedID, reworkID string) string {
	var missing []string
	if reworkedID == "" {
		missing = append(missing, reworkReparentSupersededLabel)
	}
	if reworkID == "" {
		missing = append(missing, reworkReparentReworkLabel)
	}
	return strings.Join(missing, ", ")
}

// The two ends' names, spelled once (Rule 1b): the refusal sentence and the
// tests both read them, so a reader can grep one token.
const (
	reworkReparentSupersededLabel = "the superseded row"
	reworkReparentReworkLabel     = "the rework row"
)

// reworkReparentFromBranch is the BRIDGE every branch-keyed rework lane calls:
// it resolves the row that OWNS branch into an id (through the existing
// branch→row lookup, never a second one) and hands it to
// ReworkReparentChildren.
//
// reworkID is EXCLUDED from the owner list before the resolved owner is
// picked: the lane stamps the fresh rework row with deliverable.branch BEFORE
// this runs, so the row it just minted is itself an owner of the branch — and
// it is LIVE, which is exactly what TaskForBranchResolvedRow prefers. Without
// the exclusion the "original row" would be the rework row and the review
// would compare R with R.
//
// Every exit logs through taskNotifyLog with reworkReparentMarker, including
// the arms that moved nothing: a review that could not be performed is
// recorded as UNPERFORMED, never as a silent no-op (Rule 15).
func reworkReparentFromBranch(repo, branch, reworkID, lane string) ReworkReparentReport {
	var rep ReworkReparentReport
	if strings.TrimSpace(reworkID) == "" {
		// TaskSubmitReturningID returns "" on the mirror leg (the row was
		// forwarded to a peer): there is no local row to point children at,
		// and this is a fact about THIS ledger, not a failure.
		rep.Refused = reworkReparentNoLocalRowFmt
		taskNotifyLog(reworkReparentMarker+"%s: branch %s: %s", lane, branch, rep.Refused)
		return rep
	}
	if strings.TrimSpace(repo) == "" || strings.TrimSpace(branch) == "" {
		rep.Refused = fmt.Sprintf(reworkReparentNoOwnerFmt, branch, repo)
		taskNotifyLog(reworkReparentMarker+"%s: %s", lane, rep.Refused)
		return rep
	}
	lookup, err := TaskForBranch(TaskForBranchQuery{Branch: branch, Repo: repo}, "")
	if err != nil {
		rep.Refused = fmt.Sprintf(reworkReparentLookupErrFmt, branch, err)
		taskNotifyLog(reworkReparentMarker+"%s: %s", lane, rep.Refused)
		return rep
	}
	kept := make([]TaskForBranchOwner, 0, len(lookup.Owners))
	for _, o := range lookup.Owners {
		if o.TaskID == reworkID {
			continue // the row this call just minted — never its own "original"
		}
		kept = append(kept, o)
	}
	lookup.Owners = kept
	owner, ok := TaskForBranchResolvedRow(lookup)
	if !ok {
		rep.Refused = fmt.Sprintf(reworkReparentNoOwnerFmt, branch, repo)
		taskNotifyLog(reworkReparentMarker+"%s: %s", lane, rep.Refused)
		return rep
	}
	rep = ReworkReparentChildren(owner.TaskID, reworkID,
		fmt.Sprintf(reworkReparentReasonFmt, owner.TaskID, reworkID, owner.TaskID))
	taskNotifyLog(reworkReparentMarker+"%s: branch %s: %s", lane, branch, rep.Render())
	for _, n := range rep.Notes {
		taskNotifyLog(reworkReparentMarker+"%s: %s", lane, n)
	}
	return rep
}
