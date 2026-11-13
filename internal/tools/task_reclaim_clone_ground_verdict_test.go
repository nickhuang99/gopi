package tools

// THE `reclaim-unreachable` MARKER MUST NOT CONTRADICT THE ROW IT DESCRIBES.
//
// THE DEFECT (epic 01a17dd0, measured 2026-10-27). The marker's tail read
//
//	(row status and directory left untouched; this pass will not report this row again)
//
// — true about THIS ARM, false as a statement about the ROW, and append-only, so
// it can never be withdrawn. Rows 01a17a67 / 01a17a5f carry theirs from
// 11:58:37 (event 1793073517) while their status read `completed` by
// 22:42:21 / 22:42:26 (1793112141 / 1793112146), moved by an operator's
// `gopi task update <id> --status completed` with GOPI_TASK_DELIVERY_PROOF_ALLOW=1
// (session 66243525-…, that row's events 10101/10102 and 10103/10104) — NOT by
// this arm. `task show` renders the verdict line directly under `status:`, so
// the operator was shown a contradiction in two adjacent lines.
//
// WHAT IS PINNED HERE, and why each assertion can only be green on the fix:
//
//	① the marker never promises what a LATER actor can falsify — it names THIS
//	  ARM as the author and states what the arm did (no status transition);
//	② it records the status the arm OBSERVED, and that snapshot equals the
//	  status measured before AND after the pass (the arm's own conduct);
//	③ after ANOTHER actor moves the row (the shape that produced the incident),
//	  the marker's snapshot still describes what the arm saw — it neither
//	  rewrites itself nor claims the row is still at that status;
//	④ the directory is still there (Rule 42) and the row's status moved only
//	  because the test moved it.

import (
	"context"
	"os"
	"strings"
	"testing"
)

// reclaimUnreachableRetiredTail is the sentence the marker USED to end with.
// It lives HERE, in the test, not in production: production has exactly ONE
// writer of the tail (reclaimUnreachableTrailTail) and no reason to carry a
// spelling it must never emit — a constant in the shipping file is a value a
// later author can reach for "because it is already there". The regression
// guard names the retired sentence and proves it is gone.
const reclaimUnreachableRetiredTail = "row status and directory left untouched"

// reclaimMarkerTailStatus extracts the status quoted in the marker's own
// snapshot sentence, so the test compares against the constant's rendering
// rather than re-spelling the wording (Rule 13).
func reclaimMarkerTailStatus(t *testing.T, detail string) string {
	t.Helper()
	_, tail, ok := strings.Cut(detail, "the row's status was ")
	if !ok {
		t.Fatalf("the marker must say which status it OBSERVED (a reader has to be able to compare its "+
			"snapshot against the row's current one instead of reading a promise): %q", detail)
	}
	status, _, ok := strings.Cut(tail, " when this arm saw it")
	if !ok {
		t.Fatalf("the observed status must be pinned to the moment the arm saw it (a snapshot, not a "+
			"claim about the row's future): %q", detail)
	}
	return strings.Trim(status, `"`)
}

// TestReclaimUnreachableMarkerDoesNotPromiseTheRowsFuture is the incident's own
// regression: a clone-ground row (the shape of all three measured rows), one
// pass of the real arm, then ANOTHER actor closing the row exactly as the
// operator did at 22:42:21.
func TestReclaimUnreachableMarkerDoesNotPromiseTheRowsFuture(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc is unavailable — the dead-holder shape cannot be modelled")
	}
	freshReclaimRefusalMemo(t)
	fossilLedgerEnv(t)
	repo, clone, branch := reclaimCloneGroundFixture(t)

	const id = "t-clone-verdict-tail"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "clone verdict tail", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusInProgress, repo, branch, clone, hostPid(t, deadPID(t)))

	before := reclaimRowStatus(t, id)
	captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })
	if after := reclaimRowStatus(t, id); after != before {
		t.Fatalf("this arm writes a task_events note and nothing else — the status must be unchanged by the "+
			"pass, %q → %q", before, after)
	}
	markers := reclaimMarkerEvents(t, id)
	if len(markers) != 1 {
		t.Fatalf("want exactly one %s marker, got %d: %v", reclaimUnreachableMarker, len(markers), markers)
	}
	detail := markers[0]

	// ① THE RETIRED CLAIM. A tail that promises the row's status is untouched
	// reads as a contradiction the moment anything else moves the row — and the
	// marker cannot be withdrawn. Assert the arm's AUTHORSHIP is what the
	// sentence states.
	if strings.Contains(detail, reclaimUnreachableRetiredTail) {
		t.Fatalf("the marker must not carry the retired promise %q: it is a claim about the ROW, is "+
			"append-only, and 01a17a67 / 01a17a5f falsified it 10h44m after it was written (measured "+
			"2026-10-27): %q", reclaimUnreachableRetiredTail, detail)
	}
	if !strings.Contains(detail, reclaimUnreachableTrailArmClaim) {
		t.Fatalf("the marker must say what THIS ARM did (%q) so its scope is visible to a reader who sees "+
			"the row in a different state later: %q", reclaimUnreachableTrailArmClaim, detail)
	}

	// ② THE SNAPSHOT IS THE ARM'S OWN MEASUREMENT.
	if got := reclaimMarkerTailStatus(t, detail); got != before {
		t.Fatalf("the marker's recorded status must be the one the arm actually observed (%q), got %q — a "+
			"snapshot that disagrees with the ledger at write time is the same class of defect one step "+
			"over: %q", before, got, detail)
	}

	// ③ ANOTHER ACTOR MOVES THE ROW — the incident's exact shape, and the reason
	// the old wording became a contradiction.
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE task_id = ?`, taskStatusCompleted, id); err != nil {
		t.Fatalf("simulate the other actor: %v", err)
	}
	// The close is part of the arrangement, not a teardown: the OTHER actor writes
	// through its own handle and the row must be readable without it, so a close
	// that failed would leave the write unflushed and this arm would be asserting
	// against a transaction nobody committed. Checking it is the only way this
	// arm can tell "the other actor's write landed" from "it was still buffered".
	if err := db.Close(); err != nil {
		t.Fatalf("close the other actor's handle: %v", err)
	}
	if got := reclaimRowStatus(t, id); got != taskStatusCompleted {
		t.Fatalf("the other actor's write must land (this test models the operator's `task update --status "+
			"completed`): %q", got)
	}
	markers = reclaimMarkerEvents(t, id)
	if len(markers) != 1 {
		t.Fatalf("a status change by another actor must not write or drop a marker of this arm, got %d: %v",
			len(markers), markers)
	}
	// The marker still describes the ARM's moment: the snapshot still names the
	// status the arm saw (it is evidence, and evidence is not rewritten) …
	if got := reclaimMarkerTailStatus(t, markers[0]); got != before {
		t.Fatalf("the arm's snapshot must survive another actor's transition unchanged — it is a record of "+
			"what the arm saw, got %q want %q: %q", got, before, markers[0])
	}
	// … and it does NOT claim the row is still in that status.
	if strings.Contains(markers[0], reclaimUnreachableRetiredTail) {
		t.Fatalf("after another actor moved the row the retired promise is now a live contradiction on the "+
			"row's own trail: %q", markers[0])
	}

	// ④ Rule 42: the arm removed nothing, and the corrected wording changed no
	// criterion.
	if _, err := os.Stat(clone); err != nil {
		t.Fatalf("this arm must leave the clone ground in place: %v", err)
	}
}

// TestReclaimUnreachableTrailTailNamesUnknownStatus pins the honest half of
// reclaimUnreachableTrailTail: a candidate whose status could not be read is
// NAMED unread, never rendered as an empty pair of quotes a reader would take
// for a value (Rule 15).
func TestReclaimUnreachableTrailTailNamesUnknownStatus(t *testing.T) {
	tail := reclaimUnreachableTrailTail("")
	if !strings.Contains(tail, reclaimUnreachableTrailStatusUnknown) {
		t.Fatalf("an unknown status must be named %q in the tail, got %q",
			reclaimUnreachableTrailStatusUnknown, tail)
	}
	if strings.Contains(tail, `""`) {
		t.Fatalf("an unreadable status must not render as empty quotes (that reads as a value): %q", tail)
	}
	known := reclaimUnreachableTrailTail(taskStatusInProgress)
	if !strings.Contains(known, `"`+taskStatusInProgress+`"`) {
		t.Fatalf("a known status must be quoted verbatim, got %q", known)
	}
	if strings.Contains(known, reclaimUnreachableRetiredTail) {
		t.Fatalf("no spelling of the tail may revive the retired promise: %q", known)
	}
}
