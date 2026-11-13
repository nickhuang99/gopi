// task_owner_axes.go — THE OWNER AXIS ENUMERATION. One place answers "which
// ledger rows are candidates to own branch B, and through which axis?" — and
// both faces that ask it read the SAME answer.
//
// THE MEASURED DIVERGENCE THIS FILE CLOSES (epic 01a13214, 2026-10-12) — the
// two operator surfaces disagreed about the owner of ONE branch, on the SAME
// ledger:
//
//	$ gopi merge --status
//	  BURIED base-peermaster-01a12b54 — 01a12b54-e636-… (completed) — unowned
//	$ gopi task archive --check
//	    base-peermaster-01a12b54 — unowned — (none) — landed
//
// "unowned" in both owner cells, and yet the two faces formed DIFFERENT verdicts
// about the same branch — because they resolved the owner through different
// MEMBERSHIP sets. `merge --status` resolves through TaskForBranch, whose
// matcher reads the row's PRIMARY axis (deliverable.branch) AND its SECONDARY
// grounds (the branches a run CREATED — taskBackfillDeliverableGrounds); the
// archive face read deliverable.branch only, through its own SQL
// (`LIKE '%"branch":"%'`) and its own JSON parse (deliverableBranchOf). The row
// in question names task/01a12b54e636796fa97e5cfccd3ebb06 on its primary axis
// and base-peermaster-01a12b54 on a GROUND — so one face recognised the branch
// as owned and the other saw nothing.
//
// That is a divergence in the MEMBERSHIP of the candidate set. It is NOT the
// documented divergence in ORDERING preference (archiveOwnerRank: the archive
// face prefers the SAFEST reading — LIVE > DECIDED > ARCHIVABLE — while
// TaskForBranchResolvedRow prefers the LIVE one; both are deliberate, both stay,
// and each face's own doc says why). Two faces may rank one candidate set
// differently; they may not disagree about what the candidates ARE, because
// then "is this branch buried?" depends on which surface the operator asked —
// and the BURIED verdict arms a re-dispatch decision.
//
// WHY THE GROUNDS AXIS IS PART OF THE MEMBERSHIP, not an extra. A ground is
// recorded precisely so the ledger can be asked about the branch a run created
// (task_deliverable_grounds.go states the gap it closed: the run minted a second
// worktree and no record named it, so the branch→row lookup answered "no owner"
// for a LIVE branch). Recognising the axis in one lookup and not in the other
// reinstates exactly that answer, one surface over.
//
// SAFETY DIRECTION (this unification may only add candidates). Every consumer
// downstream treats a candidate as a REASON TO HOLD: the archive gate refuses
// to bury a branch a non-terminal row tracks, the reclaim arm keeps the ref, the
// classification reports the row instead of `(none)`. Adding an axis therefore
// can only make a consumer more cautious — never loosen it — which is the
// direction Rule 15's default-deny asks for.
//
// ONE ENUMERATION, NEVER A SECOND SCAN (Rule 13/14). The SQL and the blob
// parse stay where they already are: taskDeliverableScanRows (task_for_branch.go)
// is the ONE scan of the deliverable-carrying rows, and taskDeliverableParse is
// the ONE parser. This file adds no query, no LIKE needle and no JSON unmarshal
// of its own; it asks that scan for every row and reads the axes the scan already
// derived, so the two faces cannot drift apart again.

package tools

import (
	"context"
	"fmt"
	"strings"
)

// taskOwnerAxisRecord is ONE (axis value, ledger row) pair: the row names this
// branch through its primary axis or through a ground it created. It is the unit
// both faces fold from — the archive face into its most-cautious pick, the
// reclaim arm into "is any row still non-terminal?".
type taskOwnerAxisRecord struct {
	// branch is the AXIS VALUE — the row's own deliverable.branch, or the branch
	// of one of the secondary grounds it created. Empty values are never
	// emitted: a row that names no branch at all owns nothing.
	branch string
	taskID string
	status string
	// shape is the row's OWN text (status, deliverable branch, tags, summary),
	// carried from the same scan so a consumer can ask a question about the row
	// beyond "does it name the branch" — the archive gate's successor predicate
	// is the one such question today (task_rework_successor.go). Same reason as
	// the axes themselves: one scan, no second reader (Rule 13/14).
	shape reworkRowShape
}

// taskDeliverableOwnerAxes is the ONE spelling of "the branch axes a row
// carries": its primary branch (when it has one) followed by the branch of each
// ground it created, de-duplicated, in that order.
//
// It is a function of the PARSED values the scan already holds (never a second
// parse of the blob), and it is what makes the primary axis and a ground the
// same KIND of fact for every consumer — while leaving each consumer free to
// rank them.
func taskDeliverableOwnerAxes(branch string, grounds []taskDeliverableGround) []string {
	var axes []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		for _, seen := range axes {
			if seen == v {
				return
			}
		}
		axes = append(axes, v)
	}
	add(branch)
	for _, g := range grounds {
		add(g.Branch)
	}
	return axes
}

// taskOwnerBranchMatchesAxis is the ONE spelling of "does this row's branch axis
// name the branch the caller asked about?" — a trimmed equality, primary axis and
// secondary ground alike (task_owner_axes.go).
//
// It is deliberately NOT a normalisation: the `task/<id>` spelling tolerance the
// reclaim arm applies (taskIDNormalize, archiveReclaimMatchesBranch) is that
// arm's own question — "may this name refer to the same task?" — and it stays
// there. What this function fixes is the MEMBERSHIP of the candidate set: which
// AXES are read at all.
func taskOwnerBranchMatchesAxis(axis, asked string) bool {
	axis, asked = strings.TrimSpace(axis), strings.TrimSpace(asked)
	return axis != "" && axis == asked
}

// taskOwnerAxesAll enumerates every (axis value, row) the ledger holds — the ONE
// membership answer behind every "who owns this branch?" read in this package.
//
// It is a PURE READ and it obeys the read guard (ledger_read_guard.go): an
// ABSENT ledger names no owner, which is the empty answer, never a refusal and
// never a created file. A ledger that exists but cannot be READ is reported as
// an error, because folding it into an empty list is the reading that lets a
// consumer delete a live deliverable's ground (Rule 15).
//
// The scan is the shared one, driven with an ACCEPT-ALL matcher: this file asks
// for the universe, and each caller narrows it to its own question.
func taskOwnerAxesAll() ([]taskOwnerAxisRecord, error) {
	ctx := context.Background()
	path := memoryDBPath()
	out := []taskOwnerAxisRecord{}
	if ledgerMissingForRead(path) {
		return out, nil
	}
	db, err := openTasksDB(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("task owner axes: open ledger %s: %w", path, err)
	}
	defer db.Close()
	scan, err := taskDeliverableScanRows(ctx, db,
		func(branch, worktree, repo string, grounds []taskDeliverableGround) (bool, string) {
			return true, ""
		})
	if err != nil {
		return nil, fmt.Errorf("task owner axes: scan ledger: %w", err)
	}
	for _, o := range scan.Owners {
		shape := reworkRowShape{Status: o.Status, DeliverableBranch: o.Branch, Tags: o.Tags, Summary: o.Summary}
		for _, axis := range o.axes {
			out = append(out, taskOwnerAxisRecord{branch: axis, taskID: o.TaskID, status: o.Status, shape: shape})
		}
	}
	return out, nil
}
