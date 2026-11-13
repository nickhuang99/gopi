package tools

// rework_handoff.go — the branch-rework lane's ONE dedup entry, and the half of
// it that a LIVE-only probe cannot see.
//
// THE DEFECT THIS FILE CLOSES (measured on the live ledgers of both nodes,
// 2026-10-07). The lane's identity for a branch is the deterministic summary
// `resolveRowReworkSummaryPrefix + branch`, and its dedup probe read that
// summary through ONE predicate: is a row in a LIVE status? That is correct for
// a row being worked HERE and wrong for a row whose work MOVED:
//
//	zbook  merger files "resolve merge conflict: rework: task/01a113e4…"  (01a11564)
//	zbook  `gopi task send --cancel-source --peer hp` → hp's ledger stores it,
//	       zbook's copy becomes cancelled (the migration the flag exists for)
//	zbook  the NEXT tick's probes, ALL of them, ask "is a LIVE row carrying this
//	       summary?" → NO (the row is cancelled) → the branch is handed a SECOND
//	       rework row (01a1156f, 12 minutes later; then 01a115bb)
//
// Measured shape, both ledger halves of the same branch:
//
//	zbook: 4 rows — cancelled, cancelled, cancelled, pending
//	hp:    2 rows — completed (the migrated work, run here), pending
//
// So "cancelled" was carrying TWO opposite meanings for this lane — "abandoned,
// nobody is doing this" and "someone else is doing this now" — and the probe
// answered with the first. The fix is NOT to widen the live set (Rule 26: a
// cancelled row can also mean the operator deliberately gave up on the branch,
// and folding cancelled into "live" would make that branch un-reworkable
// forever). It is to read the DECISION the ledger already recorded.
//
// WHERE THE DECISION IS. The cancel leg of `gopi task send --cancel-source`
// writes the migration into the row itself, as its own audit trail: the reason
// names the peer, the receipt and the undo, and — from this commit — the tags
// carry the marker this file reads. A row cancelled by HAND (an operator
// abandoning the branch) carries the reason but never the marker.
//
// WHY A TAG, AND NOT THE STATUS OR A NEW STATUS. Tested against the live
// ledgers before choosing:
//
//   - a spent migration and a deliberate abandonment are BOTH `cancelled` today,
//     and the abandonment must stay re-workable, so the status cannot be the
//     discriminator;
//   - `superseded` (minted 2026-10-07 for exactly "the work is elsewhere") is
//     reachable from a live row and is the semantically right status, but an
//     operator cancelling by hand may equally reach it, so a status still cannot
//     carry the fact — and switching the migration's status would change a
//     surface two existing suites pin (the CLI's `--cancel-source` output and
//     the leg's own assertion) for no gain in the probe's precision;
//   - a tag is this family's OWN vocabulary for a row fact no status expresses
//     (`from-<node>` already means "this row travelled"), it is a TOKEN rather
//     than a sentence, and it survives the reason column being rewritten by hand.
//
// The marker is written by the ONE writer of the migration decision
// (SyncRowCancelSource) and read by the ONE gate below, so the two cannot drift
// (Rule 14).

import (
	"context"
	"fmt"
	"strings"
)

const (
	// reworkHandoffTag is the row-level marker of a HANDED-OFF row: the row was
	// sent to a peer by `gopi task send --cancel-source` on a PERSISTED receipt,
	// and the peer's ledger holds the work now. It is appended to the row's tags
	// by the cancel leg, after the status write it belongs to.
	//
	// It marks EVERY migrated row, not only the branch-rework family: "this
	// row's work is on the peer" is a fact about the ROW, and the lane that reads
	// it is the only thing that cares which family the row is in.
	reworkHandoffTag = "handed-off"

	// reworkHandoffRowScanCap bounds the rows ONE summary's probe reads. The
	// lane's identity is deterministic, so a summary normally owns one or two
	// rows; a pathological chain still answers in bounded work (Rule 1), and the
	// LIVE arm is ordered first so a long history can never starve it.
	reworkHandoffRowScanCap = 50

	// reworkGateReasonLiveFormat / reworkGateReasonHandedOffFormat are the ONE
	// spelling of the two ways this lane declines to re-issue a branch. They are
	// what every refusal lane prints as its detail, so a branch's state reads the
	// same wherever the refusal was produced (Rule 14: one reading of the probe,
	// one sentence per answer).
	reworkGateReasonLiveFormat      = "a LIVE branch-rework task already carries %s"
	reworkGateReasonHandedOffFormat = "branch-rework task %s was HANDED OFF to a peer by `gopi task send --cancel-source` and is cancelled HERE by that move (the branch-rework lane's work continues on the peer, whose own row carries it) — one branch, ONE rework row, so nothing is filed here"

	// reworkGateDuplicateDetailFormat is the ONE spelling of a refusal lane's
	// `duplicate` DETAIL: the branch, then the reason (`reworkSummaryBlocked`'s
	// own sentence). The reason is carried VERBATIM because the two arms are not
	// interchangeable to a reader — a live row means wait, a handed-off row means
	// the work is on a peer — and a lane that printed one arm's sentence for the
	// other would be telling an operator to wait for a row nobody here will
	// ever move.
	reworkGateDuplicateDetailFormat = "no new branch-rework row for %s: %s"
)

// reworkSummaryBlocked is the branch-rework lane's ONE dedup entry: it reports
// whether a NEW row under summary must NOT be filed, and says which fact stopped
// it ("" when the lane is free).
//
// It answers TWO questions with ONE read of the ledger:
//
//  1. THE LIVE ARM — a row of this summary is pending/claimed/in_progress: the
//     lane is working the branch right now. Unchanged from the live-only probe
//     this function carries; it stays FIRST, so nothing about the existing
//     semantics moves.
//  2. THE HANDED-OFF ARM — a row of this summary is TERMINAL **and carries
//     reworkHandoffTag**: the row was MIGRATED to a peer, so the work is not
//     abandoned but someone else's now. Filing here would put one branch in two
//     workers' hands (the defect measured above), so the lane declines — and
//     declines in a sentence that says so, rather than reporting the row as
//     merely absent.
//
// WHAT DELIBERATELY DOES *NOT* BLOCK: a plain (human) cancellation, a failed
// row, a completed row. Those are decisions that the branch is NOT being worked,
// so the branch must be able to enter the lane again — the ruling that a
// terminal rework row releases the branch is unchanged. The discriminator is the
// MARKER, never the status alone.
func reworkSummaryBlocked(summary string) (bool, string, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return false, "", nil
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return false, "", fmt.Errorf("open ledger: %w", err)
	}
	defer db.Close()
	// The LIVE arm is ordered first so a summary with a long history cannot bury
	// a running row behind older terminal ones; id DESC inside each arm makes
	// "the newest row of that shape" the deterministic reporter.
	rows, err := db.QueryContext(ctx, `
SELECT task_id, status, tags FROM tasks
WHERE summary = ?
ORDER BY (status IN (`+taskLiveStatusSQLList()+`)) DESC, id DESC
LIMIT ?`, summary, reworkHandoffRowScanCap)
	if err != nil {
		return false, "", fmt.Errorf("dedup query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, status, tags string
		if err := rows.Scan(&id, &status, &tags); err != nil {
			return false, "", fmt.Errorf("dedup scan: %w", err)
		}
		if isLiveTaskStatus(status) {
			return true, fmt.Sprintf(reworkGateReasonLiveFormat, summary), nil
		}
		if terminalTaskStatus(status) && hasTaskTagToken(tags, reworkHandoffTag) {
			return true, fmt.Sprintf(reworkGateReasonHandedOffFormat, id), nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, "", fmt.Errorf("dedup iter: %w", err)
	}
	return false, "", nil
}

// hasTaskTagToken reports whether a comma-separated tags string carries tag as a
// WHOLE token. It answers the substring-match hazard the merge-conflict lane
// already records (task_adoption.go: a `tags LIKE '%x%'` query also matches a
// mid-token occurrence) in Go, where the token boundary is exact.
func hasTaskTagToken(tags, tag string) bool {
	for _, t := range strings.Split(tags, ",") {
		if strings.TrimSpace(t) == tag {
			return true
		}
	}
	return false
}
