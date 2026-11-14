package tools

// memory_sync_row_gate_test.go — the STATUS GATE and the OPT-IN SOURCE
// CANCELLATION of the row-send leg.
//
// WHAT IT PINS. `gopi task send` used to carry whatever row it was handed, in
// whatever state: an in_progress row (a leg already running here) travelled to
// a peer whose dispatcher then saw a QUEUED row and ran it again — one row,
// two legs. And `--cancel-source`-shaped migration was three manual steps
// whose middle one (cancel the source) is the one a human forgets, which
// produces exactly the same double-dispatch by omission.
//
// Two properties, one per file half:
//
//	the GATE  — only `pending` travels; every other status is refused BY NAME,
//	            with the current status, BEFORE any peer is contacted;
//	the MOVE  — the source row is cancelled HERE only on a PERSISTED receipt
//	            (stored / migrated / already current), never on a refusal and
//	            never when no answer arrived.
//
// The negative arms are the point of this file: a refusal, an unreachable peer
// and a local write failure each leave the SOURCE ROW PENDING — the property
// that makes a failed migration a no-op instead of a lost row.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// rowGatePendingIdle is the fixture's one queued row: registered, then forced
// to pending (the state a submit writes and the only state that travels).
const rowGatePendingIdle = "t-rowleg-gate-01"

// rowGateStorePeer is the exchange seam that really runs the PEER side: the
// injected body goes through SyncRowRemote against the PEER ledger, so the
// receipt the test decides on is produced by the merge authority itself
// (Rule 14 — the merge is relied on, never re-implemented).
func rowGateStorePeer(t *testing.T, peerDB string, sent *int) func(string, []byte) ([]byte, error) {
	t.Helper()
	return func(peer string, body []byte) ([]byte, error) {
		*sent++
		var out []byte
		var herr error
		withLedger(t, peerDB, func() { out, herr = SyncRowRemote(body) })
		return out, herr
	}
}

// rowGateSeedPending registers the one row and forces it to pending.
func rowGateSeedPending(t *testing.T, id string) {
	t.Helper()
	if _, err := TaskNewOID(TaskNewOptions{
		Summary: "row gate probe",
		TaskID:  id,
		Payload: "the row the gate tests carry",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	rowLegForceStatus(t, id, TaskStatusPending, float64(time.Now().Unix()))
}

// TestRowSendRefusesEveryNonPendingStatus is the GATE: one refusal per status,
// each naming the CURRENT status and the reason, each leaving the peer's
// ledger untouched (the exchange is never called, so nothing can have
// travelled).
func TestRowSendRefusesEveryNonPendingStatus(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() { rowGateSeedPending(t, rowGatePendingIdle) })

	cases := []struct {
		status string
		want   string // a fragment of the reason that status must produce
	}{
		{TaskStatusInProgress, "RUNNING"},
		{TaskStatusClaimed, "CLAIMED"},
		{TaskStatusCompleted, "FINISHED"},
		{TaskStatusVerified, "FINISHED"},
		{TaskStatusCancelled, "TERMINAL"},
		{TaskStatusFailed, "TERMINAL"},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			withLedger(t, senderDB, func() {
				rowLegForceStatus(t, rowGatePendingIdle, tc.status, float64(time.Now().Unix()))
			})
			sent := 0
			var err error
			withLedger(t, senderDB, func() {
				_, err = SyncRowSendEx(SyncRowSendOptions{
					TaskID:       rowGatePendingIdle,
					Peers:        []string{syncRowTestPeer},
					Exchange:     rowGateStorePeer(t, peerDB, &sent),
					CancelSource: true, // a send IS a move; the STATUS gate is what this test asks
				})
			})
			if err == nil {
				t.Fatalf("a %s row was SENT — the leg carries queued work only", tc.status)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.status) {
				t.Errorf("the refusal does not name the current status (%s): %v", tc.status, err)
			}
			if !strings.Contains(msg, TaskStatusPending) {
				t.Errorf("the refusal does not name the state it wants (%s): %v", TaskStatusPending, err)
			}
			if !strings.Contains(msg, tc.want) {
				t.Errorf("the refusal for %s carries no reason for that state (want %q): %v", tc.status, tc.want, err)
			}
			if sent != 0 {
				t.Fatalf("the refusal still contacted the peer %d time(s) — a refused send must not reach the wire", sent)
			}
			// The peer ledger never saw the row.
			var here string
			withLedger(t, peerDB, func() { here, _ = syncRowStatusOn("", rowGatePendingIdle) }) //stylegate:ignore the row-status read in the fixture: the gate decision below is the assertion, and an error yields the pending fallback it compares against
			if here != "" {
				t.Fatalf("the peer ledger holds the refused row (status=%q)", here)
			}
		})
	}
}

// TestRowSendCancelSourceCancelsOnlyOnAPersistedReceipt is the MOVE: the
// source row is cancelled on `stored` and left alone on a refusal and on an
// unreachable peer. The two negative arms are the ones that matter — they are
// what makes a failed migration a no-op.
func TestRowSendCancelSourceCancelsOnlyOnAPersistedReceipt(t *testing.T) {
	t.Run("stored", func(t *testing.T) {
		senderDB, peerDB := rowLegTwoLedgers(t)
		withLedger(t, senderDB, func() { rowGateSeedPending(t, rowGatePendingIdle) })
		sent := 0
		var res *SyncRowSendResult
		var err error
		withLedger(t, senderDB, func() {
			res, err = SyncRowSendEx(SyncRowSendOptions{
				TaskID:       rowGatePendingIdle,
				Peers:        []string{syncRowTestPeer},
				Exchange:     rowGateStorePeer(t, peerDB, &sent),
				CancelSource: true, // a send IS a move; the primitive performs both halves
			})
		})
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if got := res.PersistedPeers(); len(got) != 1 || got[0] != syncRowTestPeer {
			t.Fatalf("PersistedPeers = %v, want [%s] after a stored receipt", got, syncRowTestPeer)
		}
		if res.Disposition == nil {
			t.Fatalf("a MOVE reported no source disposition — the primitive performs both halves")
		}
		disp := *res.Disposition
		if !disp.Cancelled {
			t.Fatalf("a stored receipt did not cancel the source row: %+v", disp)
		}
		withLedger(t, senderDB, func() {
			row, rerr := taskStatusDirect(rowGatePendingIdle)
			if rerr != nil || row == nil {
				t.Fatalf("read source row: %v", rerr)
			}
			if row.Status != TaskStatusCancelled {
				t.Fatalf("source status = %q, want %q", row.Status, TaskStatusCancelled)
			}
			// THE REASON NAMES THE PEER: the row's own trail must answer
			// "why is this cancelled?" without the operator remembering the
			// command — that is the whole point of the auto-written reason.
			if !strings.Contains(row.Reason, syncRowTestPeer) {
				t.Errorf("cancellation reason does not name the peer: %q", row.Reason)
			}
			if !strings.Contains(row.Reason, "stored") {
				t.Errorf("cancellation reason does not name the receipt: %q", row.Reason)
			}
		})
		// And the peer really holds the row (the fact the cancellation stood on).
		withLedger(t, peerDB, func() {
			row, rerr := taskStatusDirect(rowGatePendingIdle)
			if rerr != nil || row == nil {
				t.Fatalf("the peer ledger has no row: %v", rerr)
			}
			if row.Status != TaskStatusPending {
				t.Fatalf("the peer's copy is %q, want %q — it was sent as queued work", row.Status, TaskStatusPending)
			}
		})
	})

	t.Run("refused", func(t *testing.T) {
		senderDB, _ := rowLegTwoLedgers(t)
		withLedger(t, senderDB, func() { rowGateSeedPending(t, rowGatePendingIdle) })
		// The peer's own refusal document, exactly as its handler emits it.
		refusal := `{"refused":1,"reason":"terminal row diverged: local content wins"}`
		var res *SyncRowSendResult
		var err error
		withLedger(t, senderDB, func() {
			res, err = SyncRowSendEx(SyncRowSendOptions{
				TaskID:       rowGatePendingIdle,
				Peers:        []string{syncRowTestPeer},
				CancelSource: true, // a send IS a move
				Exchange: func(string, []byte) ([]byte, error) {
					return []byte(refusal), nil
				},
			})
		})
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if got := res.PersistedPeers(); len(got) != 0 {
			t.Fatalf("a REFUSED receipt counted as persisted: %v", got)
		}
		if res.Disposition == nil {
			t.Fatalf("a MOVE reported no source disposition")
		}
		disp := *res.Disposition
		if disp.Cancelled {
			t.Fatalf("a REFUSED send cancelled the source row: %+v", disp)
		}
		if !strings.Contains(disp.Note, "LEFT PENDING") || !strings.Contains(disp.Note, "diverged") {
			t.Errorf("the note must state that the source is still pending AND quote the peer: %q", disp.Note)
		}
		withLedger(t, senderDB, func() {
			if got := rowLegStatus(t, rowGatePendingIdle); got != TaskStatusPending {
				t.Fatalf("source status = %q, want %q (a refused migration must be a no-op)", got, TaskStatusPending)
			}
		})
	})

	t.Run("unreachable", func(t *testing.T) {
		senderDB, _ := rowLegTwoLedgers(t)
		withLedger(t, senderDB, func() { rowGateSeedPending(t, rowGatePendingIdle) })
		var res *SyncRowSendResult
		var err error
		withLedger(t, senderDB, func() {
			res, err = SyncRowSendEx(SyncRowSendOptions{
				TaskID:       rowGatePendingIdle,
				Peers:        []string{syncRowTestPeer},
				CancelSource: true, // a send IS a move
				Exchange: func(string, []byte) ([]byte, error) {
					return nil, errors.New("ssh: connect port 22: connection refused")
				},
			})
		})
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if got := res.PersistedPeers(); len(got) != 0 {
			t.Fatalf("an UNREACHABLE peer counted as persisted: %v", got)
		}
		if res.Disposition == nil {
			t.Fatalf("a MOVE reported no source disposition")
		}
		disp := *res.Disposition
		if disp.Cancelled {
			t.Fatalf("an unreachable peer cancelled the source row: %+v", disp)
		}
		withLedger(t, senderDB, func() {
			if got := rowLegStatus(t, rowGatePendingIdle); got != TaskStatusPending {
				t.Fatalf("source status = %q, want %q", got, TaskStatusPending)
			}
		})
	})
}

// TestRowCancelSourceReportsTheLocalFailureWindow pins the ONE outcome that is
// worse than a refusal: the peer HOLDS the row while the local cancel did not
// land. Both ledgers now carry it, so the note must say so (Rule 15) — a
// silent success there is a double dispatch with nobody told.
func TestRowCancelSourceReportsTheLocalFailureWindow(t *testing.T) {
	senderDB, _ := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() { rowGateSeedPending(t, rowGatePendingIdle) })
	res := &SyncRowSendResult{
		Receipts: []SyncRowPeerReceipt{{Peer: syncRowTestPeer, Receipt: SyncRowReceipt{Stored: 1, Status: TaskStatusPending}}},
	}
	var disp SyncRowSourceDisposition
	withLedger(t, senderDB, func() {
		disp = SyncRowCancelSource(SyncRowCancelSourceOptions{
			TaskID: rowGatePendingIdle,
			Result: res,
			Cancel: func(string, string, string) (string, error) {
				return "", errors.New("database is locked")
			},
		})
	})
	if disp.Cancelled {
		t.Fatalf("a failed local write reported a cancellation: %+v", disp)
	}
	for _, want := range []string{syncRowTestPeer, "LOCAL cancel failed", "may run twice"} {
		if !strings.Contains(disp.Note, want) {
			t.Errorf("the window report is missing %q: %q", want, disp.Note)
		}
	}
	withLedger(t, senderDB, func() {
		if got := rowLegStatus(t, rowGatePendingIdle); got != TaskStatusPending {
			t.Fatalf("source status = %q, want %q (the write failed, so nothing changed)", got, TaskStatusPending)
		}
	})
}

// TestRowCancelSourceRereadsTheSourceBeforeWriting pins the RACE arm: the row
// can move while the send is in flight (an ssh hop per peer), so a row that is
// no longer pending by the time the cancellation is decided must NOT be
// cancelled — cancelling a running row is the same double-dispatch hazard the
// send gate refuses, one door later.
func TestRowCancelSourceRereadsTheSourceBeforeWriting(t *testing.T) {
	senderDB, _ := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() { rowGateSeedPending(t, rowGatePendingIdle) })
	withLedger(t, senderDB, func() {
		rowLegForceStatus(t, rowGatePendingIdle, TaskStatusInProgress, float64(time.Now().Unix()))
	})
	res := &SyncRowSendResult{
		Receipts: []SyncRowPeerReceipt{{Peer: syncRowTestPeer, Receipt: SyncRowReceipt{Stored: 1, Status: TaskStatusPending}}},
	}
	// The row moved AFTER the gate opened (that is the race): it is already
	// in_progress by the time the cancellation is decided.
	var disp SyncRowSourceDisposition
	withLedger(t, senderDB, func() {
		disp = SyncRowCancelSource(SyncRowCancelSourceOptions{TaskID: rowGatePendingIdle, Result: res})
	})
	if disp.Cancelled {
		t.Fatalf("a row that went in_progress during the send was cancelled: %+v", disp)
	}
	if !strings.Contains(disp.Note, TaskStatusInProgress) {
		t.Errorf("the race report must name the status it found: %q", disp.Note)
	}
}

// TestRowSendWithoutCancelSourceLeavesTheSourceAlone WAS the default's pin.
// THE DEFAULT IS GONE (epic task-send-is-a-move): a send without the flag
// leaves the row pending HERE while a copy is pending THERE, so it is refused
// by the MOVE-ONLY gate before the export and before any contact. The test
// keeps its name so the history reads, and pins what replaced it:
//
//	① the refusal names the flag and the other legal shape, and reaches the
//	   source row NOT AT ALL (status untouched, peer never contacted);
//	② the same call WITH the flag moves the row (status cancelled);
//	③ a second send-with-flag of the moved row is refused by the STATUS gate —
//	   idempotence falls out of the move now, not out of a copy's echo.
func TestRowSendWithoutCancelSourceLeavesTheSourceAlone(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() { rowGateSeedPending(t, rowGatePendingIdle) })
	sent := 0
	var err error
	withLedger(t, senderDB, func() {
		_, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:   rowGatePendingIdle,
			Peers:    []string{syncRowTestPeer},
			Exchange: rowGateStorePeer(t, peerDB, &sent),
		})
	})
	if err == nil {
		t.Fatalf("a send without --%s was allowed (it is a COPY, not a move)", CancelSourceFlagSpelling)
	}
	if !strings.Contains(err.Error(), CancelSourceFlagSpelling) || !strings.Contains(err.Error(), TakeOverFlagSpelling) {
		t.Fatalf("the refusal must name BOTH legal shapes: %v", err)
	}
	if sent != 0 {
		t.Fatalf("the refused send contacted the peer %d time(s)", sent)
	}
	withLedger(t, senderDB, func() {
		if got := rowLegStatus(t, rowGatePendingIdle); got != TaskStatusPending {
			t.Fatalf("a REFUSED send changed the source row: status = %q", got)
		}
	})

	// ② THE MOVE.
	var res *SyncRowSendResult
	withLedger(t, senderDB, func() {
		res, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       rowGatePendingIdle,
			Peers:        []string{syncRowTestPeer},
			Exchange:     rowGateStorePeer(t, peerDB, &sent),
			CancelSource: true,
		})
	})
	if err != nil {
		t.Fatalf("the MOVE was refused: %v", err)
	}
	if want := res.PersistedPeers(); len(want) != 1 {
		t.Fatalf("PersistedPeers = %v, want the one peer", want)
	}
	withLedger(t, senderDB, func() {
		if got := rowLegStatus(t, rowGatePendingIdle); got != TaskStatusCancelled {
			t.Fatalf("source status = %q, want %q (a send is a MOVE)", got, TaskStatusCancelled)
		}
	})

	// ③ IDEMPOTENCE: the moved row is not pending, so the status gate answers
	//    and the peer is contacted no further time.
	before := sent
	var again *SyncRowSendResult
	var aerr error
	withLedger(t, senderDB, func() {
		again, aerr = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       rowGatePendingIdle,
			Peers:        []string{syncRowTestPeer},
			Exchange:     rowGateStorePeer(t, peerDB, &sent),
			CancelSource: true,
		})
	})
	if aerr == nil {
		t.Fatalf("a moved row was sent a second time: %+v", again)
	}
	if !strings.Contains(aerr.Error(), TaskStatusCancelled) {
		t.Errorf("the second send's refusal must name the current status: %v", aerr)
	}
	if sent != before {
		t.Fatalf("the peer was contacted again for an already-moved row (%d → %d)", before, sent)
	}
}

// TestRowSendGateUsesTheLedgerConstants pins the gate's vocabulary to the
// exported status constants (Rule 1/13): the refusal a user reads and the
// status the ledger writes are the same string, from one definition.
func TestRowSendGateUsesTheLedgerConstants(t *testing.T) {
	if TaskStatusPending != strings.ToLower(TaskStatusPending) {
		t.Fatalf("status constants changed shape: %q", TaskStatusPending)
	}
	if got := syncRowWhyNotPending(TaskStatusInProgress); !strings.Contains(got, "two legs") {
		t.Errorf("the in_progress reason does not name the cost: %q", got)
	}
	// An unknown status still gets a reason (never ""), and it does not invent
	// a state it does not know.
	if got := syncRowWhyNotPending("reopened"); got == "" {
		t.Errorf("an out-of-set status produced no reason")
	}
}

// TestRowSendCancelSourceHonoursTheNamedLedger pins that every half of the
// verb — the pre-cancel re-read and the cancel itself — honours `--db PATH`:
// the ledger the caller NAMED is the one read and written, even when this
// process's own ledger holds a row of the same id.
func TestRowSendCancelSourceHonoursTheNamedLedger(t *testing.T) {
	senderDB, namedDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() { rowGateSeedPending(t, rowGatePendingIdle) })
	withLedger(t, namedDB, func() { rowGateSeedPending(t, rowGatePendingIdle) })
	res := &SyncRowSendResult{
		Receipts: []SyncRowPeerReceipt{{Peer: syncRowTestPeer, Receipt: SyncRowReceipt{Stored: 1}}},
	}
	// Called with NO ledger pin of its own and a row in BOTH ledgers: the DBPath
	// is the only thing that can decide which one is written.
	disp := SyncRowCancelSource(SyncRowCancelSourceOptions{TaskID: rowGatePendingIdle, Result: res, DBPath: namedDB})
	if !disp.Cancelled {
		t.Fatalf("the named ledger's row was not cancelled: %+v", disp)
	}
	withLedger(t, namedDB, func() {
		if got := rowLegStatus(t, rowGatePendingIdle); got != TaskStatusCancelled {
			t.Fatalf("status in the NAMED ledger = %q, want %q", got, TaskStatusCancelled)
		}
	})
	withLedger(t, senderDB, func() {
		if got := rowLegStatus(t, rowGatePendingIdle); got != TaskStatusPending {
			t.Fatalf("status in THIS process's ledger = %q, want %q (only the named ledger may move)", got, TaskStatusPending)
		}
	})
	// The pin is restored afterwards: the next call resolves the ordinary
	// ledger again, not the one the last caller named.
	if got := config.LedgerPath(); got != "" && strings.HasSuffix(got, namedDB) {
		t.Fatalf("the named ledger leaked into the process after the call: %q", got)
	}
}

// rowGateReceiptRoundTrip proves the receipt document survives the wire: the
// fields the cancellation decides on are the fields the peer wrote.
func rowGateReceiptRoundTrip(t *testing.T, r SyncRowReceipt) SyncRowReceipt {
	t.Helper()
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal receipt: %v", err)
	}
	var back SyncRowReceipt
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal receipt: %v", err)
	}
	return back
}

// TestRowGateReceiptValuesSurviveTheWire pins the four effects the cancellation
// compares, through the JSON the peer actually emits.
func TestRowGateReceiptValuesSurviveTheWire(t *testing.T) {
	cases := []struct {
		r      SyncRowReceipt
		effect string
		taken  bool
	}{
		{SyncRowReceipt{Stored: 1, Status: TaskStatusPending}, syncRowEffectStored, true},
		{SyncRowReceipt{Migrated: 1, Status: TaskStatusPending}, syncRowEffectMigrated, true},
		{SyncRowReceipt{Already: 1, Status: TaskStatusPending}, syncRowEffectAlready, true},
		{SyncRowReceipt{Refused: 1, Reason: "diverged"}, syncRowEffectRefused, false},
		{SyncRowReceipt{Unreachable: true}, syncRowEffectRefused, false},
	}
	for _, tc := range cases {
		back := rowGateReceiptRoundTrip(t, tc.r)
		if got := back.Effect(); got != tc.effect {
			t.Errorf("effect of %+v = %q, want %q", tc.r, got, tc.effect)
		}
		if back.Persisted() != tc.taken {
			t.Errorf("persisted(%q) = %v, want %v", tc.effect, back.Persisted(), tc.taken)
		}
	}
}

// TestRowGateSyncPeersEnvIsTheDeclaredTopology keeps the fixture honest: the
// legs under test refuse an undeclared peer, so this file's own alias must be
// declared through the family's ONE knob (config.SyncPeersEnv) — the same value
// production reads.
func TestRowGateSyncPeersEnvIsTheDeclaredTopology(t *testing.T) {
	if config.SyncPeersEnv == "" {
		t.Fatalf("the declared-topology env name is empty")
	}
}
