package tools

// memory_sync_row_test.go — the ROW legs' regression guard.
//
// WHAT IT PINS. The single-row push the family advertised
// (`task sync --push-only --task-id T`) rode the CLAIM BROADCAST, whose peer
// handler mirrors the row's LOCK and answers a verdict: it NEVER stored the
// row, so the operator read "accepted (1 row(s))" about a row the peer's own
// `gopi task show T` denied (measured 2026-09-21, re-measured by the task
// that added these legs). The two legs under test are the repair:
//
//	send    — the peer's ledger GAINS the row (SyncRowRemote →
//	          SyncTaskImportDelta), and the receipt reports what it did.
//	receive — the peer's row is applied HERE through the same authority,
//	          so a locally newer/terminal row is never silently overwritten.
//
// The assertions are the three properties the epic names: 落库 (the peer
// really stores it), 幂等 (a re-run is already/no-op), 不静默覆盖 (a newer
// local row survives an older incoming one).
//
// Both legs are driven through the PEER's REAL handler (SyncRowRemote) and the
// REAL merge (SyncTaskImportDelta) — no merge is mocked, because the merge is
// exactly what is being relied on rather than re-implemented (Rule 14).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// syncRowTestID is the one row the fixture carries (minimal sample).
	syncRowTestID = "t-rowleg-01"
	// syncRowTestPeer is the ONE declared (fake) alias: the exchange is
	// injected, so nothing here dials ssh.
	syncRowTestPeer = "gopi-rowleg-peer"
	// syncRowUndeclaredPeer is an alias that is NOT in the declared topology —
	// the fixture for the pull leg's pre-contact gate.
	syncRowUndeclaredPeer = "gopi-rowleg-undeclared"
	// syncRowArityOversizeSlack is how far past the handler's bound the arity
	// test reaches: ONE row is enough to be "over" (Rule 1 — a named slack, not
	// a figure that silently grows with the bound).
	syncRowArityOversizeSlack = 1
	// syncRowArityChainRows is the size of the ADMITTED multi-row fixture: the
	// parent-plus-child chain, the smallest sample that can show both a
	// multi-row patch and its dependency edge (Rule 10 §4).
	syncRowArityChainRows = 2
)

// rowLegTwoLedgers is the two-ledger topology this file's SEND test needs: a
// SENDER ledger (the row's home) and a PEER ledger (where the row must
// arrive). The row leg resolves its ledger from the environment at call time,
// so the fixture flips GOPI_MEMORY_DB between the two legs — the same seam
// production uses across an ssh hop, reproduced in one process.
//
// The flip is PER-CALL, not test-scoped: t.Setenv's scope is the whole test,
// so a second ledger needs the snapshot/restore pair (Rule 17 — the
// hand-rolled prev:=os.Getenv form is lossy and the shape gate refuses it).
func rowLegTwoLedgers(t *testing.T) (senderDB, peerDB string) {
	t.Helper()
	senderDB = testutil.NewTestDBPath(t) // pins GOPI_MEMORY_DB = the sender's
	peerDir := t.TempDir()
	peerDB = filepath.Join(peerDir, config.MemoryDBFileName)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	// Create the peer ledger's schema so the peer side has somewhere to write.
	db, err := openTasksDB(context.Background(), peerDB)
	if err != nil {
		t.Fatalf("create peer ledger: %v", err)
	}
	//stylegate:ignore test fixture handle closed at the end of its own helper
	defer db.Close()

	// The declared topology (a constant for the whole test, so t.Setenv's
	// scope is the right one — the per-call flip is only ever the LEDGER).
	t.Setenv(config.SyncPeersEnv, syncRowTestPeer)
	return senderDB, peerDB
}

// withLedger runs fn with GOPI_MEMORY_DB pointed at path, restoring the
// previous value afterwards (the per-call flip).
func withLedger(t *testing.T, path string, fn func()) {
	t.Helper()
	restore := testutil.SnapshotEnv(t, config.MemoryDBEnv)
	// env-leak-ok: a PER-CALL ledger flip is the reviewed shape (Rule 17) —
	// the scope is ONE leg of ONE call, not the test, and the value is put
	// back through testutil.SnapshotEnv (the lossy hand-rolled
	// prev := os.Getenv form is what the style gate refuses).
	if err := os.Setenv(config.MemoryDBEnv, path); err != nil { //stylegate:ignore the helper's contract IS the per-call unwind — the value must be gone when this call returns, while t.Setenv's undo would ride t.Cleanup to the end of the test and leave a later leg's reads pointed at an earlier leg's ledger (measured)
		t.Fatalf("pin ledger %s: %v", path, err)
	}
	defer restore()
	fn()
}

// rowLegTestDB seeds an isolated ledger with ONE row (the single-ledger
// fixture for the RECEIVE half, which reads and writes the same ledger).
func rowLegTestDB(t *testing.T) {
	t.Helper()
	testutil.NewTestDBPath(t)
	t.Setenv(config.SyncPeersEnv, syncRowTestPeer)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	if _, err := TaskNewOID(TaskNewOptions{
		Summary: "row leg probe",
		TaskID:  syncRowTestID,
		Payload: "the one row the legs carry",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	// A row that may be SENT is a QUEUED row: TaskNewOID registers a row that
	// is already running (it is the harness's own registrar), while the send
	// leg's gate carries `pending` — the state a submit writes. Forcing it is
	// the fixture's job: this file's subject is the leg, and the state under
	// test must be exact.
	rowLegForceStatus(t, syncRowTestID, TaskStatusPending, float64(time.Now().Unix()))
}

// rowLegForceStatus writes a status straight into the ledger (the fixture
// must be EXACTLY the state under test, and the state machine refuses the
// shortcuts a test would otherwise need).
func rowLegForceStatus(t *testing.T, id, status string, updatedAt float64) {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = ?, updated_at = ? WHERE task_id = ?`,
		status, updatedAt, id); err != nil {
		t.Fatalf("force status %s: %v", status, err)
	}
}

// rowLegStatus reads one row's status out of this ledger.
func rowLegStatus(t *testing.T, id string) string {
	t.Helper()
	row, err := taskStatusDirect(id)
	if err != nil {
		t.Fatalf("taskStatusDirect(%s): %v", id, err)
	}
	if row == nil {
		t.Fatalf("row %s is absent from the ledger", id)
	}
	return row.Status
}

// TestRowSendStoresTheRowOnThePeer is the PUSH half's 落库 proof: the wire
// carries EXACTLY the requested row, the PEER's real handler applies it, and
// the receipt says `stored`. The exchange seam returns the real handler's
// answer (so the peer's merge REALLY runs — it is the authority under test,
// never mocked).
func TestRowSendStoresTheRowOnThePeer(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() {
		if _, err := TaskNewOID(TaskNewOptions{
			Summary: "row leg probe",
			TaskID:  syncRowTestID,
			Payload: "the one row the legs carry",
			Source:  "human",
		}); err != nil {
			t.Fatalf("seed row: %v", err)
		}
		// The row that TRAVELS is a queued one (see rowLegTestDB).
		rowLegForceStatus(t, syncRowTestID, TaskStatusPending, float64(time.Now().Unix()))
	})

	var shipped []byte
	var lines []string
	var err error
	withLedger(t, senderDB, func() {
		lines, err = SyncRowSend(SyncRowSendOptions{
			TaskID:       syncRowTestID,
			Peers:        []string{syncRowTestPeer},
			CancelSource: true, // a send IS a move (epic task-send-is-a-move)
			Exchange: func(peer string, body []byte) ([]byte, error) {
				shipped = body
				// The PEER side runs against the PEER ledger — the ssh hop's
				// own effect, reproduced by the same env seam production uses.
				var out []byte
				var herr error
				withLedger(t, peerDB, func() { out, herr = SyncRowRemote(body) })
				return out, herr
			},
		})
	})
	if err != nil {
		t.Fatalf("SyncRowSend: %v", err)
	}
	if len(shipped) == 0 {
		t.Fatalf("the send leg shipped no patch at all")
	}
	var patch SyncTaskPatch
	if err := json.Unmarshal(shipped, &patch); err != nil {
		t.Fatalf("shipped patch does not parse: %v", err)
	}
	if len(patch.Rows) != syncRowWireMaxRows || patch.Rows[0].TaskID != syncRowTestID {
		t.Fatalf("the wire patch carries %d row(s) (first=%q), want exactly [%s]",
			len(patch.Rows), firstRowID(patch), syncRowTestID)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "stored") {
		t.Fatalf("receipt line = %v, want one line reporting the row was STORED", lines)
	}
	// 落库: the row EXISTS in the peer ledger. Its status is the wire's own
	// (the merge owns that — this leg asserts PRESENCE, never re-decides
	// what the merge should have written).
	var peerStatus string
	withLedger(t, peerDB, func() { peerStatus = rowLegStatus(t, syncRowTestID) })
	if !validTaskStatus(peerStatus) {
		t.Fatalf("the row is not in the PEER ledger after a stored receipt: %q", peerStatus)
	}
	// The sender's own ledger holds the row, and it is no longer QUEUED there:
	// the move's cancellation is what makes "one row globally" true (epic
	// task-send-is-a-move). The primitive performs that second half and its
	// post-condition; this is the ledger's own word.
	withLedger(t, senderDB, func() {
		if got := rowLegStatus(t, syncRowTestID); got != TaskStatusCancelled {
			t.Fatalf("sender ledger status = %q, want %q — a send is a MOVE", got, TaskStatusCancelled)
		}
	})

	// IDEMPOTENCE at the SEND surface now reads as a REFUSAL: the row is no
	// longer pending here, so the second send is the status gate's answer rather
	// than a second copy on the peer (the peer is contacted zero more times).
	sent := 0
	var again *SyncRowSendResult
	withLedger(t, senderDB, func() {
		again, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       syncRowTestID,
			Peers:        []string{syncRowTestPeer},
			CancelSource: true,
			Exchange: func(string, []byte) ([]byte, error) {
				sent++
				return nil, errors.New("the peer must not be contacted a second time")
			},
		})
	})
	if err == nil {
		t.Fatalf("a moved row was sent a second time: %+v", again)
	}
	if sent != 0 {
		t.Fatalf("the second (already-moved) send reached the peer %d time(s)", sent)
	}
	if !strings.Contains(err.Error(), TaskStatusCancelled) {
		t.Errorf("the second send's refusal must name the current status: %v", err)
	}
}

// TestRowRemoteStoresAndReceipts drives the peer handler directly and pins the
// receipt's shape: stored on first arrival, already on the re-send, with the
// row's status read BACK from the ledger.
func TestRowRemoteStoresAndReceipts(t *testing.T) {
	t.Setenv(config.SyncPeersEnv, syncRowTestPeer)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)

	patch := &SyncTaskPatch{Version: syncPatchVersionDelta, Node: "peer-node", Rows: []SyncTaskRow{{
		TaskID:    syncRowTestID,
		Summary:   "row leg probe",
		Payload:   "the one row the legs carry",
		Status:    taskStatusPending,
		UpdatedAt: time.Now().Unix(),
	}}}
	body, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}

	out, err := SyncRowRemote(body)
	if err != nil {
		t.Fatalf("SyncRowRemote: %v", err)
	}
	var first SyncRowReceipt
	if err := json.Unmarshal(out, &first); err != nil {
		t.Fatalf("receipt does not parse: %v (%s)", err, out)
	}
	if first.Stored != 1 {
		t.Fatalf("first send receipt = %+v, want stored=1 — the peer did not STORE the row", first)
	}
	if rowLegStatus(t, syncRowTestID) != taskStatusPending {
		t.Fatalf("the row is not in the ledger as pending after a stored receipt: %s", rowLegStatus(t, syncRowTestID))
	}

	// IDEMPOTENCE: the same patch again must not store a second time, and the
	// receipt must say so rather than reporting a fresh write.
	out2, err := SyncRowRemote(body)
	if err != nil {
		t.Fatalf("SyncRowRemote (re-send): %v", err)
	}
	var second SyncRowReceipt
	if err := json.Unmarshal(out2, &second); err != nil {
		t.Fatalf("re-send receipt does not parse: %v (%s)", err, out2)
	}
	if second.Stored != 0 || second.Already == 0 {
		t.Fatalf("re-send receipt = %+v, want already>0 and stored=0 (idempotent no-op)", second)
	}
	if second.Status != taskStatusPending {
		t.Fatalf("re-send receipt status = %q, want the ledger's own %q", second.Status, taskStatusPending)
	}
}

// TestRowRemoteArityIsTheChainBound pins the peer-side ARITY check at the bound
// the handler REALLY has. What the property is, and what it is not:
//
//   - A PATCH IS NEVER SILENTLY TRUNCATED. Rows that arrive are applied as they
//     arrived, and a patch the handler will not take is a REFUSAL BY NAME, never
//     an implicit subset (the 2026-09-21 whole-window incident: a window was
//     applied as if it had been the selected row).
//   - THE WINDOW ITSELF IS NO LONGER A DEFECT. 4556dcf (2026-10-29, "move a
//     whole dependency chain in ONE patch") widened the bound from
//     `!= syncRowWireMaxRows` (exactly 1) to `1..syncRowPatchMaxRows`, because
//     Rule 33 makes a chain of pending rows immovable ONE ROW AT A TIME: moving
//     the parent strands its live child here, moving the child strands its edge
//     on the peer. The chain travels as ONE patch, so a two-row chain is now a
//     LEGAL caller, not a sender defect. This test asserted the pre-chain
//     contract and went red the moment the handler grew the multi-row path.
//
// So the three halves here are the handler's contract as it stands: an EMPTY
// patch is refused (a sender that sent nothing is a selection defect, never a
// no-op to merge), an OVERSIZED patch is refused naming syncRowPatchMaxRows,
// and a BOUNDED CHAIN is admitted AND APPLIED WHOLE — the last arm is what
// keeps "never silently truncated" a measured property rather than a sentence.
func TestRowRemoteArityIsTheChainBound(t *testing.T) {
	t.Setenv(config.SyncPeersEnv, syncRowTestPeer)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)

	send := func(t *testing.T, rows []SyncTaskRow) (SyncRowReceipt, error) {
		t.Helper()
		body, err := json.Marshal(&SyncTaskPatch{Version: syncPatchVersionDelta, Node: "peer-node", Rows: rows})
		if err != nil {
			t.Fatalf("marshal a %d-row patch: %v", len(rows), err)
		}
		out, err := SyncRowRemote(body)
		if err != nil {
			return SyncRowReceipt{}, err
		}
		var receipt SyncRowReceipt
		if uerr := json.Unmarshal(out, &receipt); uerr != nil {
			t.Fatalf("receipt does not parse: %v (%s)", uerr, out)
		}
		return receipt, nil
	}
	// refusedAtTheBound asserts the refusal is the ARITY gate's, not some later
	// validation that happened to error: it must name the CURRENT bound and the
	// count that arrived. Naming the bound is what ties the sentence to the
	// constant — move syncRowPatchMaxRows and this arm goes red until the
	// handler's own text follows it (Rule 13: the constant is referenced, never
	// re-typed into a second sentence that can drift).
	refusedAtTheBound := func(t *testing.T, rows int, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("a %d-row patch was accepted — the handler's arity check must refuse it by name", rows)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("1..%d", syncRowPatchMaxRows)) {
			t.Fatalf("the refusal does not name the bound 1..%d: %v", syncRowPatchMaxRows, err)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("got %d", rows)) {
			t.Fatalf("the refusal does not name the count that arrived (%d): %v", rows, err)
		}
	}

	t.Run("empty patch is refused", func(t *testing.T) {
		_, err := send(t, nil)
		refusedAtTheBound(t, 0, err)
	})

	t.Run("oversized patch is refused", func(t *testing.T) {
		// One row past the bound. The arity gate runs BEFORE the import, so
		// these rows never need to be a legal chain — they must only be
		// COUNTED, which is the whole point of a fat sender being refused
		// instead of half-applied.
		rows := make([]SyncTaskRow, 0, syncRowPatchMaxRows+syncRowArityOversizeSlack)
		for i := 0; i < syncRowPatchMaxRows+syncRowArityOversizeSlack; i++ {
			rows = append(rows, chainWireRow(fmt.Sprintf("t-oversize-%02d", i), ""))
		}
		_, err := send(t, rows)
		refusedAtTheBound(t, syncRowPatchMaxRows+syncRowArityOversizeSlack, err)
	})

	t.Run("a bounded chain is admitted whole", func(t *testing.T) {
		_, peerDB := rowLegTwoLedgers(t)
		// The minimal chain: a parent and its child, the smallest sample that
		// can show a multi-row patch AND its dependency edge (Rule 10 §4).
		rows := []SyncTaskRow{
			chainWireRow(syncParentTestParent, ""),
			chainWireRow(syncParentTestChild, syncParentTestParent),
		}
		if len(rows) != syncRowArityChainRows {
			t.Fatalf("the fixture is meant to carry the minimal 2-row chain, it carries %d", len(rows))
		}
		var receipt SyncRowReceipt
		withLedger(t, peerDB, func() {
			var err error
			receipt, err = send(t, rows)
			if err != nil {
				t.Fatalf("a %d-row chain patch was refused: %v — the handler admits 1..%d by design", len(rows), err, syncRowPatchMaxRows)
			}
		})
		// APPLIED WHOLE — the anti-truncation half. Every row of the patch is
		// on the peer's ledger, and the receipt counts the WHOLE patch; a
		// handler that took the first row and dropped the rest fails here
		// without any sentence having to assert it.
		if got := receipt.Stored + receipt.Already + receipt.Migrated; got != len(rows) {
			t.Fatalf("receipt accounts for %d of %d rows (%+v) — the patch was partially applied", got, len(rows), receipt)
		}
		if receipt.Refused != 0 {
			t.Fatalf("a legal chain patch was refused per-row: %+v (%s)", receipt, receipt.Reason)
		}
		withLedger(t, peerDB, func() {
			for _, row := range rows {
				if !ledgerHas(t, peerDB, row.TaskID) {
					t.Fatalf("row %s did not land — the patch was truncated to its first row", row.TaskID)
				}
			}
			if got := ledgerParent(t, peerDB, syncParentTestChild); got != syncParentTestParent {
				t.Fatalf("the child landed with parent_task_id=%q, want %q — a chain patch carries its edge", got, syncParentTestParent)
			}
		})
	})
}

// TestRowReceiveNeverOverwritesANewerLocalRow is the 不静默覆盖 proof: the
// local row is TERMINAL and newer, the peer's row is an older pending one, and
// after the pull the local ledger still holds its own truth.
func TestRowReceiveNeverOverwritesANewerLocalRow(t *testing.T) {
	rowLegTestDB(t)
	now := float64(time.Now().Unix())
	rowLegForceStatus(t, syncRowTestID, taskStatusCompleted, now)

	older := SyncTaskRow{
		TaskID:    syncRowTestID,
		Summary:   "row leg probe",
		Payload:   "the one row the legs carry",
		Status:    taskStatusPending,
		UpdatedAt: int64(now) - 600, // the peer's stale copy
	}
	stats, status, err := SyncRowReceive(SyncRowReceiveOptions{
		TaskID: syncRowTestID,
		Peer:   syncRowTestPeer,
		Fetch: func(peer, id string) ([]SyncTaskRow, error) {
			return []SyncTaskRow{older}, nil
		},
	})
	if err != nil {
		t.Fatalf("SyncRowReceive: %v", err)
	}
	if status != taskStatusCompleted {
		t.Fatalf("the local row became %q after pulling an older pending copy — a terminal row must never regress", status)
	}
	if stats.RowsImported != 0 || stats.RowsMigrated != 0 {
		t.Fatalf("the pull applied a backward move: %+v", stats)
	}
	if got := rowLegStatus(t, syncRowTestID); got != taskStatusCompleted {
		t.Fatalf("ledger status = %q, want %q (never silently overwritten)", got, taskStatusCompleted)
	}

	// IDEMPOTENCE: the same pull again is absorbed the same way.
	_, status2, err := SyncRowReceive(SyncRowReceiveOptions{
		TaskID: syncRowTestID,
		Peer:   syncRowTestPeer,
		Fetch: func(peer, id string) ([]SyncTaskRow, error) {
			return []SyncTaskRow{older}, nil
		},
	})
	if err != nil {
		t.Fatalf("SyncRowReceive (re-run): %v", err)
	}
	if status2 != taskStatusCompleted {
		t.Fatalf("re-run changed the row to %q", status2)
	}
}

// TestRowReceiveAppliesANewerPeerRow is the positive half of the pull: a
// NEWER peer row moves this ledger forward through the ordinary merge (so the
// leg is not merely refusing everything).
func TestRowReceiveAppliesANewerPeerRow(t *testing.T) {
	rowLegTestDB(t)
	rowLegForceStatus(t, syncRowTestID, taskStatusPending, float64(time.Now().Unix())-600)

	newer := SyncTaskRow{
		TaskID:    syncRowTestID,
		Summary:   "row leg probe",
		Payload:   "the one row the legs carry",
		Status:    taskStatusCompleted,
		UpdatedAt: time.Now().Unix(),
		EndedAt:   time.Now().Unix(),
	}
	stats, status, err := SyncRowReceive(SyncRowReceiveOptions{
		TaskID: syncRowTestID,
		Peer:   syncRowTestPeer,
		Fetch: func(peer, id string) ([]SyncTaskRow, error) {
			return []SyncTaskRow{newer}, nil
		},
	})
	if err != nil {
		t.Fatalf("SyncRowReceive: %v", err)
	}
	if status != taskStatusCompleted {
		t.Fatalf("status = %q, want %q (the newer peer row must land)", status, taskStatusCompleted)
	}
	if stats.RowsMigrated+stats.RowsImported == 0 {
		t.Fatalf("the newer row produced no forward move: %+v", stats)
	}
}

// TestRowLegsRequireTheirRow pins the "a row leg carries exactly one row"
// precondition on BOTH legs — the id is the argument, so its absence is a
// refusal by name rather than a window-shaped fallback.
func TestRowLegsRequireTheirRow(t *testing.T) {
	t.Setenv(config.SyncPeersEnv, syncRowTestPeer)
	if _, err := SyncRowSend(SyncRowSendOptions{Peers: []string{syncRowTestPeer}}); err == nil {
		t.Errorf("Send with no task id was accepted — a row leg has no window to fall back to")
	}
	if _, _, err := SyncRowReceive(SyncRowReceiveOptions{Peer: syncRowTestPeer}); err == nil {
		t.Errorf("Receive with no task id was accepted")
	}
	if _, _, err := SyncRowReceive(SyncRowReceiveOptions{TaskID: syncRowTestID}); err == nil {
		t.Errorf("Receive with no peer was accepted")
	}
}

// TestRowReceiveRefusesAnUndeclaredPeerBeforeContact pins the PULL leg's
// declared-topology gate: an alias that is not in sync_peers is refused BY NAME,
// and the refusal happens BEFORE the fetch runs.
//
// The "before" half is the whole point and it is MEASURED, not asserted in
// prose: the Fetch seam is the leg's only door to a peer, so a counter that
// stays zero proves no row was asked for. Before this gate the receive leg was
// the ONE peer-taking face in the family without it (send, diff, harvest, sync
// and the reconcile readers all called RequireDeclaredPeer) — so a pull could
// dial an alias nobody declared and the operator would learn it only from an
// ssh error, which is exactly what "topology is declared, never discovered"
// exists to prevent.
func TestRowReceiveRefusesAnUndeclaredPeerBeforeContact(t *testing.T) {
	t.Setenv(config.SyncPeersEnv, syncRowTestPeer)
	calls := 0
	_, _, err := SyncRowReceive(SyncRowReceiveOptions{
		TaskID: syncRowTestID,
		Peer:   syncRowUndeclaredPeer,
		Fetch: func(peer, id string) ([]SyncTaskRow, error) {
			calls++
			return nil, nil
		},
	})
	if err == nil {
		t.Fatalf("Receive accepted an undeclared peer %q — topology is declared, never discovered", syncRowUndeclaredPeer)
	}
	if !strings.Contains(err.Error(), syncRowUndeclaredPeer) {
		t.Errorf("the refusal does not name the alias: %v", err)
	}
	if calls != 0 {
		t.Errorf("the fetch ran %d time(s) before the topology gate refused — the gate must run FIRST", calls)
	}
	// And the DECLARED peer still passes the same gate (the refusal is about
	// the declaration, not about pulling at all).
	if _, _, err := SyncRowReceive(SyncRowReceiveOptions{
		TaskID: syncRowTestID,
		Peer:   syncRowTestPeer,
		Fetch:  func(peer, id string) ([]SyncTaskRow, error) { return nil, nil },
	}); err != nil {
		t.Errorf("a declared peer was refused: %v", err)
	}
}

// firstRowID names the first row of a patch (a diagnostic helper).
func firstRowID(p SyncTaskPatch) string {
	if len(p.Rows) == 0 {
		return ""
	}
	return p.Rows[0].TaskID
}
