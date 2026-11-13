package tools

// task_transfer_eligibility_test.go — THE DEPENDENCY CRITERION and the send
// leg's use of it.
//
// WHAT IT PINS, and why each half needs its own proof:
//
//	the CRITERION (TaskTransferEligible) — master's ONE predicate, asked as a
//	    table over the smallest double that can express "a live child blocks"
//	    and "a finished child does not", plus the forward arms.
//	the LEG (SyncRowSendEx, --cancel-source) — the same two situations driven
//	    through the REAL send path against TWO real ledgers with the peer's own
//	    handler standing in for the ssh hop: the refusal is BY NAME (the
//	    blocking child's id rides the error text), it is a provable NO-OP (the
//	    peer's ledger never gains the row, and the source row is untouched and
//	    still pending here), and the same send SUCCEEDS the moment the blocking
//	    child reaches a terminal status.
//
// The end-to-end half is the MOVE's regression guard: sending a pending parent
// away WITH `--cancel-source` parked every child left behind FOREVER (Rule 33
// has no override, no cascade and no timeout release). The boundary test pins
// the OTHER side: a plain `task send` is a COPY, so the designed chain flow
// (parent first, then each child, the edge riding with it) must still work
// untouched.
//
// THE CRITERION IS NOT DEFINED HERE. This file was carried over from the
// branch that first reached for the dependency gate and it asks MASTER's
// predicate and master's fixture helpers — one definition, one judgment
// (Rule 14), and the reason the branch's own 2-argument TaskTransferEligible
// and its read-interface were dropped in the reconciliation rather than
// merged alongside.

import (
	"strings"
	"testing"
	"time"
)

// taskTransferFakeDependency is the smallest stand-in for the SOURCE ledger's
// answer to "does anything still have to wait for this row?" — the reverse
// half of the predicate, handed in as a value (TaskTransferDependency), which
// is exactly the seam master's contract defines.
type taskTransferFakeDependency struct {
	live        bool
	childID     string
	childStatus string
}

func (d taskTransferFakeDependency) dep() TaskTransferDependency {
	return TaskTransferDependency{Live: d.live, ChildID: d.childID, ChildStatus: d.childStatus}
}

// transferRow is a row in the state a send starts from (pending), with an
// optional edge.
//
// It carries a PAYLOAD because the predicate master's transfer legs ask
// (taskRowClaimable, via taskTransferEligible) refuses a payload-less row
// outright — the refusal happens BEFORE the dependency arms, so a bare row
// would make every assertion below vacuous (it would be testing THAT arm
// instead). The payload is the fixture's obligation, not the subject's.
func transferRow(id, parent string) *Task {
	return &Task{TaskID: id, ParentTaskID: parent, Status: TaskStatusPending, Payload: "the briefing that makes the row dispatchable"}
}

// noSkipSet is the question asked with no exclusions (the leg's own ask: the
// destination's skip set is the export's to price).
var noSkipSet = taskTransferSkipSet("")

// --- the criterion -----------------------------------------------------------

// TestTaskTransferEligibleBlocksOnALiveDependent is THE RED PROOF of the
// epic's defect, at the unit level: a pending row with a live child must not be
// eligible, and the refusal must NAME the child (an operator's next step
// depends on knowing which row to finish).
//
// It is the MOVE criterion: the leg asks it when the row will be cancelled here
// (`--cancel-source`), and the transfer legs ask it before a handover.
func TestTaskTransferEligibleBlocksOnALiveDependent(t *testing.T) {
	const child = "t-transfer-child-pending"
	for _, status := range []string{TaskStatusPending, TaskStatusClaimed, TaskStatusInProgress} {
		t.Run(status, func(t *testing.T) {
			dep := taskTransferFakeDependency{live: true, childID: child, childStatus: status}
			ok, why := TaskTransferEligible(transferRow("t-transfer-parent", ""), dep.dep(), noSkipSet)
			if ok {
				t.Fatalf("a row with a %s child was declared transferable — moving it parks that child forever", status)
			}
			if !strings.Contains(why, child) {
				t.Fatalf("the refusal does not name the blocking child %q: %q", child, why)
			}
			if !strings.Contains(why, status) {
				t.Errorf("the refusal does not name the child's status %s: %q", status, why)
			}
			if !strings.Contains(why, taskParentReleaseSetLabel()) {
				t.Errorf("the refusal does not state Rule 33's release set: %q", why)
			}
		})
	}
}

// TestTaskTransferEligibleIgnoresTerminalDependents is the other side of the
// same criterion, and the reason it is not "has dependents at all": a child
// that has finished, failed, been cancelled or been superseded needs no release
// from its parent. Counting those would freeze every row that was EVER a
// parent — the defect this gate would otherwise create.
//
// The live filter lives in TaskTransferLiveChild's SQL (taskTransferLiveChildStatuses
// — the SAME three values the Go readers compare against, never a second list),
// so the terminal arm is expressed at THIS layer by the value the scan can
// return: Live=false.
func TestTaskTransferEligibleIgnoresTerminalDependents(t *testing.T) {
	// The zero value IS the terminal arm at this layer: no live child found —
	// the documented "may travel" — so a parent whose dependents have all
	// finished is not frozen by them.
	if ok, why := TaskTransferEligible(transferRow("t-transfer-parent", ""), TaskTransferDependency{}, noSkipSet); !ok {
		t.Fatalf("a row with no LIVE dependent was refused: %s", why)
	}
	// And the control: the SAME row with one live child is refused — the zero
	// value above is a pass because the scan found nothing, not because the
	// reverse arm was skipped.
	live := TaskTransferDependency{Live: true, ChildID: "t-transfer-finished-child", ChildStatus: TaskStatusPending}
	if ok, why := TaskTransferEligible(transferRow("t-transfer-parent", ""), live, noSkipSet); ok {
		t.Fatalf("the control arm passed: a live child did not block (%q)", why)
	}
}

// TestTaskTransferEligibleForwardArms pins the candidate's OWN edge as this
// predicate asks it (the FORWARD half): a parentED row is refused outright —
// a dependency edge cannot resolve in a ledger that does not hold the parent,
// and Rule 33 parks such a row (see taskTransferWhyParented) — and the refusal
// names the parent so the operator knows which row to deal with.
func TestTaskTransferEligibleForwardArms(t *testing.T) {
	const parent = "t-transfer-parent"
	ok, why := TaskTransferEligible(transferRow("t-transfer-solo", parent), TaskTransferDependency{}, noSkipSet)
	if ok {
		t.Fatalf("a parented row was declared transferable — the receiving ledger cannot resolve its edge")
	}
	if !strings.Contains(why, parent) {
		t.Fatalf("the refusal does not name the parent %q: %q", parent, why)
	}
	if !strings.Contains(why, "Rule 33") {
		t.Errorf("the refusal does not name Rule 33, the rule it is enforcing: %q", why)
	}
	// The solo row: no edge, no dependent — the state the predicate exists to
	// pass through untouched.
	if ok, why := TaskTransferEligible(transferRow("t-transfer-solo", ""), TaskTransferDependency{}, noSkipSet); !ok {
		t.Fatalf("a solo pending row was refused: %s", why)
	}
}

// TestTaskTransferEligibleDeniesWhatItCannotJudge pins the DENIAL arm this
// predicate owns: a nil row is a refusal, never a pass — "nothing to judge" is
// never evidence that a transfer is safe.
func TestTaskTransferEligibleDeniesWhatItCannotJudge(t *testing.T) {
	if ok, why := TaskTransferEligible(nil, TaskTransferDependency{}, noSkipSet); ok || why == "" {
		t.Fatalf("a nil row was judged eligible (%v, %q)", ok, why)
	}
	// And the status arm: a row that is not QUEUED is refused by name — the
	// same fact syncRowStatusGate states on the leg (one predicate, two
	// callers).
	claimed := transferRow("t-transfer-claimed", "")
	claimed.Status = TaskStatusClaimed
	if ok, why := TaskTransferEligible(claimed, TaskTransferDependency{}, noSkipSet); ok || !strings.Contains(why, TaskStatusClaimed) {
		t.Fatalf("a claimed row was judged eligible (%v, %q)", ok, why)
	}
}

// --- the send leg ------------------------------------------------------------

// TestRowSendRefusesARowWithALiveDependent is THE END-TO-END REGRESSION GUARD.
// It drives the real send leg against two real ledgers with the peer's own
// handler answering (no merge mocked), and asserts the three legs of the
// acceptance:
//
//	① the MOVE (--cancel-source) is REFUSED, and the refusal names the blocking
//	   child;
//	② the refusal is a NO-OP — the peer is never contacted and its ledger never
//	   sees the row;
//	③ once the child reaches a TERMINAL status, the same send SUCCEEDS and the
//	   peer holds the row.
func TestRowSendRefusesARowWithALiveDependent(t *testing.T) {
	const (
		parent = "t-depgate-parent"
		child  = "t-depgate-child"
	)
	senderDB, peerDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() {
		seedChainRow(t, senderDB, parent, "")
		seedChainRow(t, senderDB, child, parent)
	})

	// ① + ② the refusal, with the exchange instrumented so "nothing left this
	// host" is a MEASUREMENT rather than a claim.
	sent := 0
	var err error
	withLedger(t, senderDB, func() {
		_, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       parent,
			Peers:        []string{syncRowTestPeer},
			Exchange:     rowGateStorePeer(t, peerDB, &sent),
			CancelSource: true,
		})
	})
	if err == nil {
		t.Fatalf("a pending row with a live child was MOVED — its child would be parked forever")
	}
	if !strings.Contains(err.Error(), child) {
		t.Fatalf("the refusal does not name the blocking child %s: %v", child, err)
	}
	if !strings.Contains(err.Error(), parent) {
		t.Errorf("the refusal does not name the row it refused: %v", err)
	}
	if sent != 0 {
		t.Fatalf("the refused send contacted the peer %d time(s) — nothing may leave this host", sent)
	}
	if ledgerHas(t, peerDB, parent) {
		t.Fatalf("the peer's ledger holds the refused row")
	}
	// The source row is untouched: still pending, still dispatchable here.
	if got := ledgerStatus(t, senderDB, parent); got != TaskStatusPending {
		t.Fatalf("the refused send moved the source row: status = %q, want %q", got, TaskStatusPending)
	}
	// And the blocking CHILD is still queued here — the gate refused, it did
	// not "fix" the dependency by touching the child.
	if got := ledgerStatus(t, senderDB, child); got != TaskStatusPending {
		t.Fatalf("the gate changed the child: status = %q, want %q", got, TaskStatusPending)
	}

	// ③ the child finishes → the same send is allowed.
	withLedger(t, senderDB, func() {
		rowLegForceStatus(t, child, TaskStatusCompleted, float64(time.Now().Unix()))
	})
	sent = 0
	var res *SyncRowSendResult
	withLedger(t, senderDB, func() {
		res, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       parent,
			Peers:        []string{syncRowTestPeer},
			Exchange:     rowGateStorePeer(t, peerDB, &sent),
			CancelSource: true,
		})
	})
	if err != nil {
		t.Fatalf("the MOVE was still refused after the child reached %s: %v", TaskStatusCompleted, err)
	}
	if got := res.PersistedPeers(); len(got) != 1 || got[0] != syncRowTestPeer {
		t.Fatalf("PersistedPeers = %v, want [%s]", got, syncRowTestPeer)
	}
	if !ledgerHas(t, peerDB, parent) {
		t.Fatalf("the peer's ledger does not hold the row after an allowed send")
	}
	if got := ledgerStatus(t, peerDB, parent); got != TaskStatusPending {
		t.Fatalf("the peer's copy is %q, want %q", got, TaskStatusPending)
	}
}

// TestRowSendCopiesAChainChildWithoutAskingTheMoveCriterion was THE SEVERITY
// BOUNDARY when a send could be a COPY: a plain `task send` left the source row
// HERE, so no edge changed and a chain child had to stay shippable while its
// parent was merely queued. THE COPY SHAPE IS GONE (epic
// task-send-is-a-move): a send without --cancel-source is refused before any
// peer is contacted, so the boundary it pinned no longer has two arms — it has
// one. This test keeps its NAME and its fixtures so the history is readable,
// and asserts what replaced it:
//
//	① a COPLESS send is refused, contacts NOBODY, and changes nothing;
//	② the MOVE of a parent with a live child is still refused, naming the
//	   child (the edge that made the copy arm necessary at all);
//	③ the CHILD of a merely-queued parent is refused as a move too — and the
//	   flow that carries a chain across ledgers is the PULL leg
//	   (`receive --takeover`), whose arrival criterion lands the parent first
//	   and resolves the edge there (task_transfer_eligibility.go's arrival
//	   half). A chain is not stranded by this change; send simply stops being
//	   the verb that half-copies one.
func TestRowSendCopiesAChainChildWithoutAskingTheMoveCriterion(t *testing.T) {
	const (
		parent = "t-depgate-copy-parent"
		child  = "t-depgate-copy-child"
	)
	senderDB, peerDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() {
		seedChainRow(t, senderDB, parent, "")
		seedChainRow(t, senderDB, child, parent)
	})

	// ① no flag: refused before any contact, and the ledger is untouched.
	sent := 0
	var err error
	withLedger(t, senderDB, func() {
		_, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:   parent,
			Peers:    []string{syncRowTestPeer},
			Exchange: rowGateStorePeer(t, peerDB, &sent),
		})
	})
	if err == nil {
		t.Fatalf("a send WITHOUT --cancel-source was allowed: the row would stay pending here and be copied there")
	}
	if !strings.Contains(err.Error(), CancelSourceFlagSpelling) || !strings.Contains(err.Error(), TakeOverFlagSpelling) {
		t.Fatalf("the copy refusal must name BOTH legal shapes (--%s / --%s): %v",
			CancelSourceFlagSpelling, TakeOverFlagSpelling, err)
	}
	if sent != 0 {
		t.Fatalf("the refused COPY send contacted the peer %d time(s)", sent)
	}
	if ledgerHas(t, peerDB, parent) || ledgerHas(t, peerDB, child) {
		t.Fatalf("the refused send still wrote a row onto the peer")
	}
	if got := ledgerStatus(t, senderDB, parent); got != TaskStatusPending {
		t.Fatalf("the refused send changed the source row: status = %q", got)
	}

	// ② the MOVE of the parent: refused, and the child is named.
	before := sent
	var moveErr error
	withLedger(t, senderDB, func() {
		_, moveErr = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       parent,
			Peers:        []string{syncRowTestPeer},
			Exchange:     rowGateStorePeer(t, peerDB, &sent),
			CancelSource: true,
		})
	})
	if moveErr == nil {
		t.Fatalf("moving the parent away was ALLOWED with a live child left behind")
	}
	if !strings.Contains(moveErr.Error(), child) {
		t.Fatalf("the move refusal does not name the blocking child %s: %v", child, moveErr)
	}
	if sent != before {
		t.Fatalf("the refused MOVE contacted the peer (%d → %d exchanges)", before, sent)
	}
	if got := ledgerStatus(t, senderDB, parent); got != TaskStatusPending {
		t.Fatalf("the refused move changed the source row: status = %q", got)
	}

	// ③ the MOVE of the CHILD whose parent is merely queued here: also
	//    refused (the forward half), because cancelling the child here would
	//    leave the peer holding a row whose parent this ledger still holds.
	var childErr error
	withLedger(t, senderDB, func() {
		_, childErr = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       child,
			Peers:        []string{syncRowTestPeer},
			Exchange:     rowGateStorePeer(t, peerDB, &sent),
			CancelSource: true,
		})
	})
	if childErr == nil {
		t.Fatalf("the MOVE of a child whose parent is still queued here was allowed")
	}
	if !strings.Contains(childErr.Error(), parent) {
		t.Fatalf("the refusal does not name the parent %s: %v", parent, childErr)
	}
	if !ledgerHas(t, senderDB, parent) || !ledgerHas(t, senderDB, child) {
		t.Fatalf("a refused chain move removed rows from the source ledger")
	}
}

// TestRowSendRefusesAChildWhoseParentIsUnreleased pins the FORWARD half at the
// leg, in the MOVE shape: a child that still waits on a live parent here may
// not be cancelled here either — the same edge, seen from the child's side.
func TestRowSendRefusesAChildWhoseParentIsUnreleased(t *testing.T) {
	const child = "t-depgate-forward-child"
	for _, tc := range []struct {
		name   string
		parent string
		seed   bool
	}{
		{name: "parent still pending here", parent: "t-depgate-forward-parent-live", seed: true},
		{name: "parent absent from this ledger", parent: "t-depgate-forward-parent-ghost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			senderDB, peerDB := rowLegTwoLedgers(t)
			withLedger(t, senderDB, func() {
				if tc.seed {
					seedChainRow(t, senderDB, tc.parent, "")
				}
				seedChainRow(t, senderDB, child, tc.parent)
			})
			sent := 0
			var err error
			withLedger(t, senderDB, func() {
				_, err = SyncRowSendEx(SyncRowSendOptions{
					TaskID:       child,
					Peers:        []string{syncRowTestPeer},
					Exchange:     rowGateStorePeer(t, peerDB, &sent),
					CancelSource: true,
				})
			})
			if err == nil {
				t.Fatalf("a child whose parent (%s) does not release was MOVED away", tc.parent)
			}
			if !strings.Contains(err.Error(), tc.parent) {
				t.Fatalf("the refusal does not name the parent %s: %v", tc.parent, err)
			}
			if sent != 0 || ledgerHas(t, peerDB, child) {
				t.Fatalf("the refused child reached the peer (exchanges=%d)", sent)
			}
		})
	}
}

// ledgerStatus reads one row's status out of the ledger at path — the PEER's
// own ledger when path is the peer's, so the assertion is about what that
// ledger HOLDS (Rule 15b: prove the effect).
func ledgerStatus(t *testing.T, path, id string) string {
	t.Helper()
	status := ""
	withLedger(t, path, func() {
		row, err := taskStatusDirect(id)
		if err != nil {
			t.Fatalf("read %s in %s: %v", id, path, err)
		}
		if row != nil {
			status = row.Status
		}
	})
	return status
}
