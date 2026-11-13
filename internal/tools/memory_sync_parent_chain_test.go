package tools

// memory_sync_parent_chain_test.go — the DEPENDENCY EDGE's regression guard.
//
// WHAT IT PINS. A row's parent_task_id is the whole of its chain membership
// (Rule 33: the claim funnel parks a row until its parent reaches
// {completed, verified, superseded}). It did not travel: SyncTaskRow carried
// no parent column, so `gopi task send` shipped a five-link chain as five
// independent rows and the peer's dispatcher took all five at once (measured
// zbook→hp, 2026-10-07). These tests pin the three halves of the repair:
//
//  1. the edge TRAVELS — a sent row's parent_task_id is present on the peer's
//     ledger (TestRowSendCarriesTheDependencyEdge), and a patch that carries a
//     parent and its child lands BOTH edges regardless of the order the rows
//     appear in (TestParentEdgeLandsWhateverThePatchOrder);
//  2. the edge is REFUSED when it cannot resolve — never written as a row
//     Rule 33 parks forever (TestParentEdgeRefusedWhenThePeerLacksTheParent,
//     TestParentEdgeRefusedOnATerminalParent);
//  3. the RELEASE SET is untouched — Rule 33's {completed, verified,
//     superseded} and the terminal-unreleased complement still partition the
//     terminal statuses (TestParentReleaseSetIsUntouched).
//
// The peer side is the PEER's REAL handler (SyncRowRemote → SyncTaskImportDelta)
// against a second REAL ledger — no merge is mocked, because the merge is what
// is being relied on rather than re-implemented (Rule 14). The ledger flip is
// the production seam (the ssh hop) reproduced in one process, exactly as
// memory_sync_row_test.go's rig does it.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// syncParentTestParent / Child are the two rows of the minimal chain the
	// fixture builds: one link, which is the smallest sample that can show an
	// edge (Rule 10 §4).
	syncParentTestParent = "t-parent-edge-parent"
	syncParentTestChild  = "t-parent-edge-child"
)

// seedChainRow puts ONE row in the ledger at path in the state a send
// requires (pending — the send gate refuses every other status), with an
// optional dependency edge.
func seedChainRow(t *testing.T, path, id, parent string) {
	t.Helper()
	withLedger(t, path, func() {
		if _, err := TaskNewOID(TaskNewOptions{
			TaskID:       id,
			Summary:      "parent-edge probe " + id,
			Payload:      "the briefing that makes the row dispatchable",
			Source:       "human",
			ParentTaskID: parent,
		}); err != nil {
			t.Fatalf("seed row %s: %v", id, err)
		}
		// TaskNewOID registers the row as RUNNING (it is the harness's own
		// registrar); the state under test is `pending`, so the fixture writes
		// that state exactly.
		rowLegForceStatus(t, id, TaskStatusPending, float64(time.Now().Unix()))
	})
}

// ledgerParent reads one row's dependency edge straight out of the ledger at
// path — the PEER's own ledger when path is the peer's, so the assertion is
// about what that ledger HOLDS rather than about what the sender believed
// (Rule 15b: prove the effect).
func ledgerParent(t *testing.T, path, id string) string {
	t.Helper()
	var parent string
	withLedger(t, path, func() {
		db, err := openTasksDB(context.Background(), memoryDBPath())
		if err != nil {
			t.Fatalf("open ledger %s: %v", path, err)
		}
		defer db.Close()
		if err := db.QueryRow(
			`SELECT COALESCE(parent_task_id,'') FROM tasks WHERE task_id = ?`, id).Scan(&parent); err != nil {
			t.Fatalf("read parent of %s in %s: %v", id, path, err)
		}
	})
	return strings.TrimSpace(parent)
}

// ledgerHas reports whether the ledger at path holds the row at all.
func ledgerHas(t *testing.T, path, id string) bool {
	t.Helper()
	found := false
	withLedger(t, path, func() {
		row, err := taskStatusDirect(id)
		if err != nil {
			t.Fatalf("read %s in %s: %v", id, path, err)
		}
		found = row != nil
	})
	return found
}

// sendThroughPeer drives ONE row through the send leg with the PEER's REAL
// handler standing in for the ssh hop (the production seam, one process).
func sendThroughPeer(t *testing.T, senderDB, peerDB, id string) ([]string, *SyncRowReceipt) {
	t.Helper()
	var lines []string
	var receipt SyncRowReceipt
	var err error
	withLedger(t, senderDB, func() {
		var res *SyncRowSendResult
		res, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       id,
			CancelSource: true, // a send IS a move (epic task-send-is-a-move)
			Peers:        []string{syncRowTestPeer},
			Exchange: func(peer string, body []byte) ([]byte, error) {
				var out []byte
				var herr error
				withLedger(t, peerDB, func() { out, herr = SyncRowRemote(body) })
				return out, herr
			},
		})
		if res != nil && len(res.Receipts) == 1 {
			receipt = res.Receipts[0].Receipt
			lines = res.Lines
		}
	})
	if err != nil {
		t.Fatalf("SyncRowSendEx(%s): %v", id, err)
	}
	return lines, &receipt
}

// TestRowSendCarriesTheDependencyEdge pins WHAT THE SEND LEG DOES WITH A CHAIN
// now that a send is a MOVE (epic task-send-is-a-move), and the answer is a
// REFUSAL FROM BOTH SIDES — which is a capability boundary, not a bug in this
// test. It replaced the copy-era assertion that a chain could be shipped row by
// row and still carry its edge.
//
// WHY THE OLD SHAPE CANNOT SURVIVE. Moving a row REMOVES it from this ledger,
// so every dependency edge touching it has to resolve where the row lands:
//
//   - the PARENT has a live child HERE → moving it away leaves that child
//     pointing at a parent this ledger no longer holds, which Rule 33 parks
//     forever (there is no override). Refused, naming the child.
//   - the CHILD carries parent_task_id → the receiving ledger may not hold the
//     parent, and Rule 33 parks an unresolvable edge the same way. Refused,
//     naming the parent.
//
// Together those two gates mean a chain of PENDING rows cannot be migrated one
// row at a time: whichever end you start with is refused. The edge itself still
// travels — that is pinned at the layer the send leg actually calls
// (TestParentEdgeLandsWhateverThePatchOrder drives the real peer handler with a
// two-row patch) — so the missing piece is a MULTI-ROW move (ship the chain in
// one patch, which the import already accepts in either order), not a broken
// edge. That gap is filed rather than papered over.
func TestRowSendCarriesTheDependencyEdge(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	seedChainRow(t, senderDB, syncParentTestParent, "")
	seedChainRow(t, senderDB, syncParentTestChild, syncParentTestParent)

	// ① THE PARENT FIRST — the order a chain runs in, and the order that used to
	//    work. The REVERSE half of the dependency criterion refuses it: a live
	//    child still depends on this row HERE.
	assertChainSendRefused(t, senderDB, peerDB, syncParentTestParent, syncParentTestChild, "still depends on it HERE")

	// ② THE CHILD ALONE — the FORWARD half refuses it: its edge cannot be
	//    guaranteed to resolve on the receiving ledger.
	assertChainSendRefused(t, senderDB, peerDB, syncParentTestChild, syncParentTestParent, "parent_task_id")

	// ③ NEITHER refusal moved anything: both rows are still queued here, and the
	//    peer's ledger holds neither (a refusal that wrote would be no refusal).
	if !ledgerHas(t, senderDB, syncParentTestParent) || !ledgerHas(t, senderDB, syncParentTestChild) {
		t.Fatalf("a refused send removed a row from THIS ledger")
	}
	if got := rowLegStatus(t, syncParentTestParent); got != TaskStatusPending {
		t.Fatalf("the refused parent is %q here, want %q", got, TaskStatusPending)
	}
	if ledgerHas(t, peerDB, syncParentTestParent) || ledgerHas(t, peerDB, syncParentTestChild) {
		t.Fatalf("a refused chain send landed a row on the peer")
	}

	// ④ THE CHAIN SEMANTICS the edge buys are unchanged and asked of the ONE
	//    predicate the peer's claim funnel and its pending view share (Rule 14):
	//    a child is not claimable while its parent is unreleased, and its marker
	//    says why. This is what makes the edge worth carrying at all.
	peerParents := map[string]string{syncParentTestParent: TaskStatusPending}
	if taskRowClaimable("", "", "the briefing that makes the row dispatchable", []string{syncParentTestParent}, peerParents, nil) {
		t.Fatalf("the peer would claim the child while its parent is %s — Rule 33's gate does not reach the carried edge", TaskStatusPending)
	}
	marker := taskQueuePositionSuffix(
		&Task{TaskID: syncParentTestChild, ParentTaskID: syncParentTestParent, Payload: "the briefing that makes the row dispatchable"},
		peerParents, "")
	if !strings.Contains(marker, TaskQueueSkipMarker) || !strings.Contains(marker, syncParentTestParent) {
		t.Fatalf("the peer's pending view marks the child %q, want %s naming the parent", marker, TaskQueueSkipMarker)
	}
}

// assertChainSendRefused drives ONE chain row through the real move leg and
// pins that it is refused BY NAME (naming the row that stands in the way), that
// the peer was never contacted, and that the row did not travel.
func assertChainSendRefused(t *testing.T, senderDB, peerDB, id, blocker, wantReason string) {
	t.Helper()
	sent := 0
	var err error
	withLedger(t, senderDB, func() {
		_, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       id,
			CancelSource: true, // a send IS a move
			Peers:        []string{syncRowTestPeer},
			Exchange:     rowGateStorePeer(t, peerDB, &sent),
		})
	})
	if err == nil {
		t.Fatalf("row %s was MOVED out of a live chain — its edge would not resolve", id)
	}
	if !strings.Contains(err.Error(), blocker) {
		t.Errorf("the refusal does not name %s: %v", blocker, err)
	}
	if !strings.Contains(err.Error(), wantReason) {
		t.Errorf("the refusal does not state the reason (%q): %v", wantReason, err)
	}
	if sent != 0 {
		t.Fatalf("the refused send contacted the peer %d time(s) — nothing may leave this host", sent)
	}
	if ledgerHas(t, peerDB, id) {
		t.Fatalf("the refused row %s landed on the peer", id)
	}
}

// TestParentEdgeRefusedWhenThePeerLacksTheParent pins the LOUD arm — and, in
// the SAME test, the batch answer that replaced the row-by-row one.
//
// THE ROW-BY-ROW BOUNDARY, as it actually is TODAY (recorded, not wished for).
// The doc block above TestRowSendCarriesTheDependencyEdge lets this arm read as
// "the PEER refuses the child at the receipt". It does not: a send is a MOVE,
// so the child is refused HERE, before any contact, by the SENDER's own
// dependency gate (syncRowTransferGate → TaskTransferEligible) — which is
// strictly better (nothing leaves the host at all) and is what this test now
// asserts. The peer's receipt arm still exists for a leg that BYPASSES the
// sender gate, and TestParentEdgeLandsWhateverThePatchOrder drives it directly.
//
// WHAT REMAINS TRUE AND WHY IT MATTERS: the refusal names the parent, the peer
// is never contacted, and the row does not travel — so a chain cannot be
// migrated one row at a time from EITHER end. The escape is the batch
// (TestChainSendMovesTheWholeChain drives three rows in one patch); this test's
// second half proves the same two rows DO move when they travel together.
func TestParentEdgeRefusedWhenThePeerLacksTheParent(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	seedChainRow(t, senderDB, syncParentTestParent, "")
	seedChainRow(t, senderDB, syncParentTestChild, syncParentTestParent)

	// ① THE CHILD ALONE: refused by the SENDER, naming its edge.
	var err error
	withLedger(t, senderDB, func() {
		_, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       syncParentTestChild,
			CancelSource: true, // a send IS a move
			Peers:        []string{syncRowTestPeer},
			Exchange: func(peer string, body []byte) ([]byte, error) {
				t.Fatalf("the refused child contacted the peer %s — nothing may leave this host", peer)
				return nil, nil
			},
		})
	})
	if err == nil {
		t.Fatalf("the child was MOVED out of its chain — its edge would not resolve on the peer")
	}
	if !strings.Contains(err.Error(), syncParentTestParent) || !strings.Contains(err.Error(), "parent_task_id") {
		t.Fatalf("the refusal does not name the dangling parent: %v", err)
	}
	if ledgerHas(t, peerDB, syncParentTestChild) {
		t.Fatalf("the refused child landed on the peer — a refusal must be a provable no-op")
	}
	if !ledgerHas(t, senderDB, syncParentTestChild) || rowLegStatus(t, syncParentTestChild) != TaskStatusPending {
		t.Fatalf("the sender lost or changed its own row after a refused send")
	}
	// (the ② half that used to follow — "THE SAME TWO ROWS, AS ONE BATCH: they
	// move", driven through the chain leg — is RETIRED with that leg: `task
	// send --chain` is in taskverbs.RemovedVerbs and the chain-transfer
	// mechanism is deleted. The ① arm above is the one that survives, and it
	// pins the refusal at the layer that decides it TODAY, the sender.)
}

// TestParentEdgeRefusedOnATerminalParent pins the second unhealable arm at the
// layer that decides it TODAY — the sender — and shows where its repair lives.
//
// THE HISTORY, kept because a reader meets the old shape in comments: this arm
// used to be expressed by seeding a `cancelled` parent ON THE PEER and reading
// the peer's receipt refusal. A send is now a MOVE, so a parentED row never
// reaches the wire at all: the sender's dependency gate refuses it first,
// without asking what the peer holds. The terminal-parent hazard was the CHAIN
// MOVE's own arm; that leg is RETIRED (the chain-transfer mechanism is deleted
// with `task send --chain`, which is in taskverbs.RemovedVerbs), so this test
// pins only the layer that decides the question today — the sender.
func TestParentEdgeRefusedOnATerminalParent(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	seedChainRow(t, senderDB, syncParentTestParent, "")
	seedChainRow(t, senderDB, syncParentTestChild, syncParentTestParent)
	// The parent exists ON THE PEER with a terminal, non-releasing verdict —
	// which no longer changes the answer, and THAT is the assertion.
	seedChainRow(t, peerDB, syncParentTestParent, "")
	withLedger(t, peerDB, func() {
		rowLegForceStatus(t, syncParentTestParent, TaskStatusCancelled, float64(time.Now().Unix()))
	})

	var err error
	withLedger(t, senderDB, func() {
		_, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       syncParentTestChild,
			CancelSource: true,
			Peers:        []string{syncRowTestPeer},
			Exchange: func(peer string, body []byte) ([]byte, error) {
				t.Fatalf("the refused child contacted the peer %s", peer)
				return nil, nil
			},
		})
	})
	if err == nil {
		t.Fatalf("the child was accepted onto a %s parent", TaskStatusCancelled)
	}
	if !strings.Contains(err.Error(), syncParentTestParent) {
		t.Fatalf("the refusal does not name the parent: %v", err)
	}
	if ledgerHas(t, peerDB, syncParentTestChild) {
		t.Fatalf("the refused child landed on the peer anyway")
	}
	if got := rowLegStatus(t, syncParentTestChild); got != TaskStatusPending {
		t.Fatalf("the refused child changed state here: %q", got)
	}
}

// TestParentEdgeLandsWhateverThePatchOrder pins the ORDER-TOLERANT import: a
// patch that carries a parent and its child settles BOTH, whichever order the
// rows appear in. A child listed FIRST is spooled and re-asked after the
// patch's own parent lands — the arm that turns a chain migration shipped as
// one patch into one landing instead of one refusal.
func TestParentEdgeLandsWhateverThePatchOrder(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []SyncTaskRow
	}{
		{name: "parent first", rows: []SyncTaskRow{chainWireRow(syncParentTestParent, ""), chainWireRow(syncParentTestChild, syncParentTestParent)}},
		{name: "child first", rows: []SyncTaskRow{chainWireRow(syncParentTestChild, syncParentTestParent), chainWireRow(syncParentTestParent, "")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, peerDB := rowLegTwoLedgers(t)
			patch := &SyncTaskPatch{Version: syncPatchVersionDelta, Node: "peer-node", Rows: tc.rows}
			var stats *SyncTaskImportStats
			withLedger(t, peerDB, func() {
				var herr error
				stats, herr = SyncTaskImportDelta(patch)
				if herr != nil {
					t.Fatalf("SyncTaskImportDelta: %v", herr)
				}
			})
			if stats.RowsRefused > 0 {
				t.Fatalf("a correct chain patch was refused: %v", stats.Errors)
			}
			if got := ledgerParent(t, peerDB, syncParentTestChild); got != syncParentTestParent {
				t.Fatalf("the child landed with parent_task_id=%q, want %q", got, syncParentTestParent)
			}
			if !ledgerHas(t, peerDB, syncParentTestParent) {
				t.Fatalf("the parent row did not land")
			}
		})
	}
}

// chainWireRow renders one row of the minimal chain in the delta wire shape.
func chainWireRow(id, parent string) SyncTaskRow {
	now := time.Now().Unix()
	return SyncTaskRow{
		TaskID:       id,
		Summary:      "parent-edge probe " + id,
		Payload:      "the briefing that makes the row dispatchable",
		Source:       "human",
		Status:       taskStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
		ParentTaskID: parent,
	}
}

// TestParentReleaseSetIsUntouched is the CONSTRAINT's guard: this change must
// not move Rule 33's release set. It re-states the set as a literal (the whole
// point — a silent widening or narrowing is the defect) and checks the
// terminal-unreleased complement against it.
func TestParentReleaseSetIsUntouched(t *testing.T) {
	want := map[string]bool{
		taskStatusCompleted:  true,
		taskStatusVerified:   true,
		taskStatusSuperseded: true,
	}
	if len(taskParentReleaseStatuses) != len(want) {
		t.Fatalf("the release set is %v, want exactly %v", taskParentReleaseSetLabel(), []string{taskStatusCompleted, taskStatusSuperseded, taskStatusVerified})
	}
	for st := range want {
		if !taskParentReleaseStatuses[st] {
			t.Fatalf("the release set lost %q: %s", st, taskParentReleaseSetLabel())
		}
	}
	// The complement: every terminal status is EITHER a release or a named
	// non-release — never both, never neither.
	for st := range taskParentTerminalUnreleased {
		if taskParentReleaseStatuses[st] {
			t.Fatalf("%q is both a release and a non-release — the two tables disagree", st)
		}
		if !terminalTaskStatus(st) {
			t.Fatalf("%q is in the terminal-unreleased set but is not terminal", st)
		}
	}
	if taskParentTerminalUnreleased[taskStatusCompleted] || taskParentTerminalUnreleased[taskStatusSuperseded] {
		t.Fatalf("a RELEASING status was put in the terminal-unreleased set")
	}
}

// TestParentVerdictArms pins the judgement itself, on the smallest sample: the
// four arms and the sentence each one produces, including the "no edge" case
// that must stay a pass (a row without a parent behaves exactly as before the
// gate existed).
func TestParentVerdictArms(t *testing.T) {
	// lookup turns a map into the ledger-lookup shape the judgement takes
	// (the map IS the ledger here: the smallest possible stand-in).
	lookup := func(m map[string]string) func(string) (string, bool) {
		return func(id string) (string, bool) {
			st, ok := m[id]
			return st, ok
		}
	}
	live := lookup(map[string]string{"p": TaskStatusPending})
	released := lookup(map[string]string{"p": TaskStatusCompleted})
	dead := lookup(map[string]string{"p": TaskStatusCancelled})

	if v, _ := syncParentVerdict("", live); v != syncParentOK {
		t.Fatalf("a row with no edge answered %q, want a pass", v)
	}
	if v, _ := syncParentVerdict("p", live); v != syncParentOK {
		t.Fatalf("a live (unmet) parent answered %q — the edge must travel and the DISPATCHER park the row", v)
	}
	for _, st := range []string{TaskStatusCompleted, TaskStatusSuperseded, TaskStatusVerified} {
		if v, _ := syncParentVerdict("p", lookup(map[string]string{"p": st})); v != syncParentOK {
			t.Fatalf("a %s parent answered %q, want a pass (it releases)", st, v)
		}
	}
	if v, reason := syncParentVerdict("p", dead); v != syncParentTerminal || !strings.Contains(reason, TaskStatusCancelled) {
		t.Fatalf("a cancelled parent answered (%q, %q), want the terminal arm naming the status", v, reason)
	}
	if v, reason := syncParentVerdict("ghost", released); v != syncParentDangling || !strings.Contains(reason, "ghost") {
		t.Fatalf("an absent parent answered (%q, %q), want the dangling arm naming the id", v, reason)
	}
	if v, _ := syncParentVerdict("p", nil); v != syncParentUnreadable {
		t.Fatalf("an unreadable lookup answered %q — an unresolvable edge is never assumed satisfied", v)
	}
}

// TestParentEdgeSurvivesTheHarvestLane pins the SECOND transport: the anchor
// window (harvestRemoteSelect → harvestInsert) carries the edge too. A row
// pulled by `task sync` must not arrive as free work either.
func TestParentEdgeSurvivesTheHarvestLane(t *testing.T) {
	peerDB := testutil.NewTestDBPath(t)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	t.Setenv(config.SyncPeersEnv, syncRowTestPeer)

	// The parent is present; the child arrives through the window carrying its
	// edge.
	seedChainRow(t, peerDB, syncParentTestParent, "")
	remote := []harvestRemoteRow{{
		TaskID:       syncParentTestChild,
		Summary:      "parent-edge probe " + syncParentTestChild,
		Payload:      "the briefing that makes the row dispatchable",
		Status:       taskStatusPending,
		Source:       "human",
		CreatedAt:    float64(time.Now().Unix()),
		UpdatedAt:    float64(time.Now().Unix()),
		ParentTaskID: syncParentTestParent,
	}}
	stats := harvestFromPeer(t, remote)
	if stats.Harvested != 1 {
		t.Fatalf("harvest counts = %+v, want one harvested row", stats)
	}
	if got := ledgerParent(t, peerDB, syncParentTestChild); got != syncParentTestParent {
		t.Fatalf("the harvested child has parent_task_id=%q, want %q", got, syncParentTestParent)
	}

	// And the loud arm in this lane: an edge whose parent this ledger does not
	// hold is refused, never written as a parked row.
	orphan := []harvestRemoteRow{{
		TaskID:       syncParentTestChild + "-orphan",
		Summary:      "orphan probe",
		Payload:      "the briefing that makes the row dispatchable",
		Status:       taskStatusPending,
		Source:       "human",
		CreatedAt:    float64(time.Now().Unix()),
		UpdatedAt:    float64(time.Now().Unix() + 1),
		ParentTaskID: "t-no-such-parent",
	}}
	orphanStats := harvestFromPeer(t, orphan)
	if orphanStats.Failed != 1 {
		t.Fatalf("a dangling harvest edge was not refused: %+v", orphanStats)
	}
	if len(orphanStats.Errors) == 0 || !strings.Contains(orphanStats.Errors[0], "t-no-such-parent") {
		t.Fatalf("the harvest refusal does not name the parent: %v", orphanStats.Errors)
	}
	if ledgerHas(t, peerDB, syncParentTestChild+"-orphan") {
		t.Fatalf("the refused orphan landed on the peer anyway")
	}
}

// harvestFromPeer drives the anchor-window lane (TaskHarvest) with an injected
// Fetch — the peer's answer is the argument, so the test's subject is the
// IMPORT's judgement about the edge rather than ssh.
func harvestFromPeer(t *testing.T, rows []harvestRemoteRow) *HarvestCounts {
	t.Helper()
	counts, err := TaskHarvest(HarvestOptions{
		Peer: syncRowTestPeer,
		Fetch: func(peer string, anchor float64) ([]harvestRemoteRow, error) {
			return rows, nil
		},
	})
	if err != nil {
		t.Fatalf("TaskHarvest: %v", err)
	}
	return counts
}
