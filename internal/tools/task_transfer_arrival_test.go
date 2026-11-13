package tools

// task_transfer_arrival_test.go — the ARRIVAL half of the dependency
// criterion, and the pull leg's use of it.
//
// WHAT IT PINS, and why the arrival direction needs its own proofs rather than
// a re-run of the push leg's:
//
//	the CRITERION (TaskTransferArrivalEligible) — as a table over a fake view,
//	    so the smallest sample expresses each outcome: an edge that is free to
//	    land, an edge the merge would not write at all, and the two shapes Rule
//	    33 can never release (a parent this ledger does not hold, a parent whose
//	    verdict releases nothing).
//	the LEG (SyncRowReceive) — the same situations driven through the REAL pull
//	    path against a REAL ledger, with the peer's answer injected where ssh
//	    would be: the refusal names the parent, it changes NOTHING locally, and
//	    the same pull lands the edge the moment the parent can release it.
//	the CHAIN FLOW STAYS INTACT — a child whose parent is merely QUEUED here
//	    still lands (that is the designed case: the edge travels and the
//	    dispatcher parks the row), so the arrival gate cannot be satisfied by
//	    refusing everything it sees.
//	the WIRE (task_transfer_arrival_wire_test.go) — the same edge, through the
//	    REAL `sqlite3 -json` the transport invokes; that file carries the defect
//	    this half was first shipped blind to.
//
// The measured asymmetry it guards (see devdoc/task-pull-dependency-gate.md):
// the merge refuses an unhonourable edge by ROLLING THE ROW BACK, so the pull
// that started it reports its counters about a row that did not land.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- the view's test double --------------------------------------------------

// arrivalFakeView is the read the arrival criterion needs, fed directly: rows
// (the local ledger's state) and statuses (the ledger's answer for a NAMED
// parent). Both are the same ledger in production, which is the point — the
// criterion cannot judge two halves against different ledgers.
type arrivalFakeView struct {
	rows     map[string]*Task
	statuses map[string]string
	// rowsErr / statusesErr make a read FAIL, which must be a denial.
	rowsErr     error
	statusesErr error
}

func (v arrivalFakeView) TaskByID(id string) (*Task, error) {
	if v.rowsErr != nil {
		return nil, v.rowsErr
	}
	return v.rows[id], nil
}

func (v arrivalFakeView) StatusesByID(ids []string) (map[string]string, error) {
	if v.statusesErr != nil {
		return nil, v.statusesErr
	}
	out := map[string]string{}
	for _, id := range ids {
		if st, ok := v.statuses[id]; ok {
			out[id] = st
		}
	}
	return out, nil
}

// arrivalRow renders a row as the wire carries it: an id, an edge, a status and
// a stamp.
func arrivalRow(id, parent, status string, updatedAt int64) SyncTaskRow {
	return SyncTaskRow{
		TaskID:       id,
		Summary:      "arrival probe " + id,
		Payload:      "the briefing that makes the row dispatchable",
		Source:       "human",
		Status:       status,
		CreatedAt:    updatedAt,
		UpdatedAt:    updatedAt,
		ParentTaskID: parent,
	}
}

// --- the criterion -----------------------------------------------------------

// TestTaskTransferArrivalEligibleLandsAChildOnAQueuedParent is the DESIGNED
// case, and the one a careless criterion gets wrong: a child whose parent is
// merely pending here MUST land. The edge is real and unmet, which is exactly
// the state Rule 33 models for this ledger — the row arrives and the dispatcher
// parks it until the parent releases. Refusing here would break the chain flow
// the push leg deliberately preserves.
func TestTaskTransferArrivalEligibleLandsAChildOnAQueuedParent(t *testing.T) {
	const parent = "t-arrival-parent"
	for _, status := range []string{TaskStatusPending, TaskStatusClaimed, TaskStatusInProgress} {
		view := arrivalFakeView{rows: map[string]*Task{}, statuses: map[string]string{parent: status}}
		if ok, why := TaskTransferArrivalEligible(arrivalRow("t-arrival-child", parent, TaskStatusPending, 100), view); !ok {
			t.Fatalf("a child whose parent is %s here was refused: %s", status, why)
		}
	}
	// ...and the release set lands it for the same reason (both arms are the
	// family's ONE verdict, syncParentVerdict).
	for _, status := range []string{TaskStatusCompleted, TaskStatusVerified, TaskStatusSuperseded} {
		view := arrivalFakeView{rows: map[string]*Task{}, statuses: map[string]string{parent: status}}
		if ok, why := TaskTransferArrivalEligible(arrivalRow("t-arrival-child", parent, TaskStatusPending, 100), view); !ok {
			t.Fatalf("a child whose parent is %s here was refused: %s", status, why)
		}
	}
}

// TestTaskTransferArrivalEligibleRefusesWhatCannotBeReleased is THE RED PROOF
// at the unit level: the two shapes Rule 33 can never release — a parent this
// ledger does not hold, and a parent whose verdict releases nothing — are
// refused, and the sentence NAMES the parent. The second arm carries the reason
// an operator needs (the repair is a reissue of the parent, not a re-run of the
// pull).
func TestTaskTransferArrivalEligibleRefusesWhatCannotBeReleased(t *testing.T) {
	const parent = "t-arrival-parent"
	dangling := arrivalFakeView{rows: map[string]*Task{}, statuses: map[string]string{}}
	ok, why := TaskTransferArrivalEligible(arrivalRow("t-arrival-child", parent, TaskStatusPending, 100), dangling)
	if ok {
		t.Fatalf("a row whose parent is not in this ledger was judged landable")
	}
	if !strings.Contains(why, parent) {
		t.Fatalf("the refusal does not name the parent: %q", why)
	}
	if !strings.Contains(why, taskParentReleaseSetLabel()) {
		t.Fatalf("the refusal does not state the release set: %q", why)
	}

	for _, status := range []string{TaskStatusCancelled, TaskStatusFailed} {
		view := arrivalFakeView{rows: map[string]*Task{}, statuses: map[string]string{parent: status}}
		ok, why := TaskTransferArrivalEligible(arrivalRow("t-arrival-child", parent, TaskStatusPending, 100), view)
		if ok {
			t.Fatalf("a row whose parent is %s here was judged landable — that edge parks the row forever", status)
		}
		if !strings.Contains(why, status) {
			t.Fatalf("the refusal does not name the parent's status %q: %q", status, why)
		}
	}

	// No edge at all: nothing to resolve, and the row behaves exactly as it did
	// before this criterion existed.
	if ok, why := TaskTransferArrivalEligible(arrivalRow("t-arrival-solo", "", TaskStatusPending, 100), dangling); !ok {
		t.Fatalf("a row with no edge was refused: %s", why)
	}
}

// TestTaskTransferArrivalEligibleSkipsAnEdgeTheMergeWouldNotWrite pins the
// BOUNDARY, and it exists because the criterion mirrors the merge's own gate on
// the edge backfill (taskArrivalWritesTheEdge) rather than inventing a second
// one:
//
//	① a local row that already carries an edge is never clobbered, so the
//	   arriving edge is not written and cannot park anything;
//	② an incoming row that the merge leaves alone (an idempotent echo — same
//	   rank, nothing newer) writes NO edge either.
//
// Both are PASSES on purpose. A criterion that refused them would refuse the
// ordinary "the peer is behind" pull, and would do it for an edge the merge
// would never have written.
func TestTaskTransferArrivalEligibleSkipsAnEdgeTheMergeWouldNotWrite(t *testing.T) {
	const (
		parent = "t-arrival-ghost"
		child  = "t-arrival-child"
	)
	// ① the local row already has an edge of its own.
	local := &Task{TaskID: child, ParentTaskID: "t-arrival-other", Status: TaskStatusPending, UpdatedAt: 100}
	view := arrivalFakeView{rows: map[string]*Task{child: local}, statuses: map[string]string{}}
	if ok, why := TaskTransferArrivalEligible(arrivalRow(child, parent, TaskStatusPending, 200), view); !ok {
		t.Fatalf("a row that already carries an edge was refused: %s", why)
	}
	// ② the incoming row is not newer and does not outrank the local row.
	edgeless := &Task{TaskID: child, Status: TaskStatusPending, UpdatedAt: 200}
	echoView := arrivalFakeView{rows: map[string]*Task{child: edgeless}, statuses: map[string]string{}}
	if ok, why := TaskTransferArrivalEligible(arrivalRow(child, parent, TaskStatusPending, 100), echoView); !ok {
		t.Fatalf("an incoming echo that the merge writes no edge for was refused: %s", why)
	}
	// The mirror is the thing being pinned — same input, same answer.
	if taskArrivalWritesTheEdge(arrivalRow(child, parent, TaskStatusPending, 100), edgeless) {
		t.Fatalf("taskArrivalWritesTheEdge says the merge would write the edge for an idempotent echo")
	}
	if !taskArrivalWritesTheEdge(arrivalRow(child, parent, TaskStatusInProgress, 200), edgeless) {
		t.Fatalf("taskArrivalWritesTheEdge missed a forward move (a rank upgrade)")
	}
	if !taskArrivalWritesTheEdge(arrivalRow(child, parent, TaskStatusPending, 300), edgeless) {
		t.Fatalf("taskArrivalWritesTheEdge missed a strictly newer anchor")
	}
	if !taskArrivalWritesTheEdge(arrivalRow(child, parent, TaskStatusPending, 300), nil) {
		t.Fatalf("taskArrivalWritesTheEdge treated an ABSENT local row as a no-write — the import inserts it whole")
	}
}

// TestTaskTransferArrivalEligibleDeniesWhatItCannotJudge pins the denial arms.
// "Could not check" is never "clear" — the same rule the push direction
// follows, and the reason the view is injected rather than defaulted.
func TestTaskTransferArrivalEligibleDeniesWhatItCannotJudge(t *testing.T) {
	empty := arrivalFakeView{rows: map[string]*Task{}, statuses: map[string]string{}}
	if ok, why := TaskTransferArrivalEligible(arrivalRow("", "", TaskStatusPending, 1), empty); ok || why == "" {
		t.Fatalf("a row with no id was judged landable (%v, %q)", ok, why)
	}
	if ok, why := TaskTransferArrivalEligible(arrivalRow("t-x", "t-p", TaskStatusPending, 1), nil); ok || !strings.Contains(why, "could not be read") {
		t.Fatalf("a nil view was judged landable (%v, %q) — an unreadable graph is never clear", ok, why)
	}
	failingLocal := arrivalFakeView{rowsErr: fmt.Errorf("db gone")}
	if ok, why := TaskTransferArrivalEligible(arrivalRow("t-x", "t-p", TaskStatusPending, 1), failingLocal); ok || why == "" {
		t.Fatalf("a failing local read was treated as clear (%v, %q)", ok, why)
	}
	// A row with no edge asks the view nothing (the degenerate read is never
	// reached), so the denial belongs to the arms that DO read.
	failingStatuses := arrivalFakeView{rows: map[string]*Task{}, statusesErr: fmt.Errorf("db gone")}
	if ok, why := TaskTransferArrivalEligible(arrivalRow("t-x", "t-p", TaskStatusPending, 1), failingStatuses); ok || why == "" {
		t.Fatalf("a failing parent read was treated as clear (%v, %q)", ok, why)
	}
}

// --- the pull leg ------------------------------------------------------------

// arrivalPullLedger is the sender ledger of the row-leg fixture: the criterion
// reads the ledger the CALLER pinned, and this is that ledger.
func arrivalPullLedger(t *testing.T) string {
	t.Helper()
	sender, _ := rowLegTwoLedgers(t)
	return sender
}

// arrivalWireAnswer is the injected peer: the criterion's subject is the
// ARRIVAL, so the transport answers with the row and the leg is free of ssh.
func arrivalWireAnswer(rows ...SyncTaskRow) func(peer, taskID string) ([]SyncTaskRow, error) {
	return func(peer, taskID string) ([]SyncTaskRow, error) { return rows, nil }
}

// TestRowReceiveRefusesAnArrivalThatCannotLand is THE END-TO-END regression
// guard for this half. It drives the REAL pull path against a REAL ledger and
// asserts the three legs of the acceptance:
//
//	① the pull is REFUSED, and the refusal names the parent it cannot release;
//	② the refusal changes NOTHING locally (the row keeps its absent edge);
//	③ the same pull SUCCEEDS and writes the edge the moment the parent is
//	   released — so the gate is a precondition, not a wall.
//
// The control in the middle is the honest one: the SAME row applied straight
// through the merge authority (the leg's own import, with no criterion in front
// of it) reproduces the defect this criterion exists to prevent — the merge
// refuses the edge and rolls the row back, so the pull's counters describe a row
// that is not here.
func TestRowReceiveRefusesAnArrivalThatCannotLand(t *testing.T) {
	const (
		parent = "t-arrival-refused-parent"
		child  = "t-arrival-refused-child"
	)
	dbPath := arrivalPullLedger(t)
	seedChainRow(t, dbPath, child, "")
	incoming := arrivalRow(child, parent, TaskStatusInProgress, time.Now().Unix())

	var err error
	withLedger(t, dbPath, func() {
		_, _, err = SyncRowReceive(SyncRowReceiveOptions{
			TaskID: child,
			Peer:   syncRowTestPeer,
			Fetch:  arrivalWireAnswer(incoming),
		})
	})
	if err == nil {
		t.Fatalf("the pull landed a row whose parent is not in this ledger")
	}
	if !strings.Contains(err.Error(), parent) {
		t.Fatalf("the refusal does not name the parent it cannot release: %v", err)
	}

	// ② NOTHING CHANGED LOCALLY: no edge, and the row did not move.
	if got := ledgerParent(t, dbPath, child); got != "" {
		t.Fatalf("the refused pull wrote an edge (%q)", got)
	}
	if got := rowLegStatus(t, child); got != TaskStatusPending {
		t.Fatalf("the refused pull moved the row to %q", got)
	}

	// THE CONTROL — the same row, straight through the merge authority, with no
	// criterion in front of it. This is the shape the pull leg used to produce:
	// the import refuses the edge and ROLLS THE ROW BACK, so the row it reports
	// on is not the row that is here.
	withLedger(t, dbPath, func() {
		stats, ierr := SyncTaskImportDelta(&SyncTaskPatch{
			Version: syncPatchVersionDelta, Node: syncRowTestPeer, Rows: []SyncTaskRow{incoming},
		})
		if ierr != nil {
			t.Fatalf("control import: %v", ierr)
		}
		if stats.RowsRefused == 0 {
			t.Fatalf("the control expected the merge to refuse the unhonourable edge: %+v", stats)
		}
	})
	if got := ledgerParent(t, dbPath, child); got != "" {
		t.Fatalf("the control wrote an edge after all (%q) — the criterion would then be guarding nothing", got)
	}

	// ③ RELEASE THE PARENT AND PULL AGAIN — the same call, now landable.
	seedChainRow(t, dbPath, parent, "")
	rowLegForceStatus(t, parent, TaskStatusCompleted, float64(time.Now().Unix()))
	var stats *SyncTaskImportStats
	withLedger(t, dbPath, func() {
		stats, _, err = SyncRowReceive(SyncRowReceiveOptions{
			TaskID: child,
			Peer:   syncRowTestPeer,
			Fetch:  arrivalWireAnswer(incoming),
		})
	})
	if err != nil {
		t.Fatalf("the pull was refused with a released parent: %v", err)
	}
	if got := ledgerParent(t, dbPath, child); got != parent {
		t.Fatalf("the pull did not write the edge: parent_task_id=%q, want %q (stats=%+v)", got, parent, stats)
	}
	if stats.RowsParented == 0 {
		t.Fatalf("the edge write was not counted: %+v", stats)
	}
}

// TestRowReceiveLandsAChildOnAQueuedParent is the CHAIN-FLOW guard: the arrival
// gate must not be satisfied by refusing every row that carries an edge. A
// child whose parent is merely QUEUED here lands, its edge is written, and Rule
// 33 then parks it — which is the designed outcome, not a defect.
func TestRowReceiveLandsAChildOnAQueuedParent(t *testing.T) {
	const (
		parent = "t-arrival-queued-parent"
		child  = "t-arrival-queued-child"
	)
	dbPath := arrivalPullLedger(t)
	seedChainRow(t, dbPath, parent, "")
	seedChainRow(t, dbPath, child, "")
	incoming := arrivalRow(child, parent, TaskStatusInProgress, time.Now().Unix())

	var stats *SyncTaskImportStats
	var err error
	withLedger(t, dbPath, func() {
		stats, _, err = SyncRowReceive(SyncRowReceiveOptions{
			TaskID: child,
			Peer:   syncRowTestPeer,
			Fetch:  arrivalWireAnswer(incoming),
		})
	})
	if err != nil {
		t.Fatalf("a child whose parent is queued here was refused: %v", err)
	}
	if got := ledgerParent(t, dbPath, child); got != parent {
		t.Fatalf("the edge did not land: parent_task_id=%q, want %q (stats=%+v)", got, parent, stats)
	}
	// THE CONSEQUENCE, asked through the ONE predicate the claim funnel and the
	// pending view share: the child is parked while its parent is unreleased,
	// and it says why.
	parents := map[string]string{parent: TaskStatusPending}
	if taskRowClaimable("", "", "the briefing that makes the row dispatchable", []string{parent}, parents, nil) {
		t.Fatalf("the child was claimable while its parent was %s", TaskStatusPending)
	}
	marker := taskQueuePositionSuffix(
		&Task{TaskID: child, ParentTaskID: parent, Payload: "the briefing that makes the row dispatchable"},
		parents, "")
	if !strings.Contains(marker, TaskQueueSkipMarker) || !strings.Contains(marker, parent) {
		t.Fatalf("the pending view marks the child %q, want %s naming the parent", marker, TaskQueueSkipMarker)
	}
}

// TestArrivalViewReadsTheLedgerTheCallerPinned pins the Rule 14 discipline this
// half had to satisfy: the arrival criterion reads the ledger the caller named
// (--db / the pinned env), not a second connection of its own.
func TestArrivalViewReadsTheLedgerTheCallerPinned(t *testing.T) {
	const id = "t-arrival-view-row"
	dbPath := arrivalPullLedger(t)
	seedChainRow(t, dbPath, id, "")
	view := NewTaskLedgerDependencyView("")
	withLedger(t, dbPath, func() {
		row, err := view.TaskByID(id)
		if err != nil {
			t.Fatalf("view read: %v", err)
		}
		if row == nil {
			t.Fatalf("the view did not see the row the caller pinned")
		}
		if row.Status != TaskStatusPending {
			t.Fatalf("the view read status %q, want %q", row.Status, TaskStatusPending)
		}
	})
	// And the batch read the criterion consults answers through the same pin.
	withLedger(t, dbPath, func() {
		statuses, err := view.StatusesByID([]string{id, "t-arrival-absent"})
		if err != nil {
			t.Fatalf("statuses read: %v", err)
		}
		if statuses[id] != TaskStatusPending {
			t.Fatalf("StatusesByID answered %q for %s, want %q", statuses[id], id, TaskStatusPending)
		}
		if _, present := statuses["t-arrival-absent"]; present {
			t.Fatalf("StatusesByID invented a row this ledger does not hold: %+v", statuses)
		}
	})
	// And the sqlPlaceholders helper the batch read is built on is pinned at its
	// boundary (an off-by-one there would be a silent SQL error).
	cases := map[int]string{0: "", 1: "?", 2: "?, ?", 3: "?, ?, ?"}
	for n, want := range cases {
		if got := sqlPlaceholders(n); got != want {
			t.Fatalf("sqlPlaceholders(%d) = %q, want %q", n, got, want)
		}
	}
}
