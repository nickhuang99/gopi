package tools

// memory_sync_row_id_resolve_test.go — the MOVE leg's id→row step, pinned.
//
// THE DEFECT (row 01a117082d5473c7a8ed247ec357795d). The send/move leg handed a
// TYPED task id straight to `task_id = ?`. A row id copied out of a listing in
// its short form therefore read as "this ledger holds no such row" — a sentence
// about a row sitting in this very ledger — and on the MOVE path that answer
// reached the dependency gate as "nothing to judge", so the criterion was never
// asked and the operator was told `not found` instead of being shown the
// candidate the operand actually names. The ledger's ids are uuid v7 and
// internal/fullid owns the project's answer to a prefix: it is REFUSED, by
// name, with every full id it could have meant.
//
// WHAT THESE TESTS PIN, one property each:
//
//	① a truncated uuid id no longer reads as absence — the leg refuses it and
//	  NAMES the full id (the red-proof: the old sentence is gone);
//	② an operand that names MORE THAN ONE row reports the ambiguity instead of
//	  quietly choosing one (Rule: never pick the first hit);
//	③ the FULL id still walks the ordinary path, with the MOVE criterion really
//	  asked — the refusal names the blocking CHILD, which is the property the
//	  short form could not reach;
//	④ a refusal is a NO-OP: the peer is never contacted (the instrumented
//	  exchange counts it) and neither ledger moves.

import (
	"strings"
	"testing"
)

// The fixture ids are canonical uuids, because the shape is what the resolver
// judges (fullid.TruncatedUUID refuses anything else — a legacy `t-<epoch>` or
// a test's slug id keeps the leg's ordinary "not found" answer, which is the
// deliberate non-change asserted at the end).
const (
	rowIDResolveShortA   = "01a11708-1111-7111-8111-111111111111"
	rowIDResolveShortB   = "01a11708-2222-7222-8222-222222222222"
	rowIDResolveChild    = "01a11708-3333-7333-8333-333333333333"
	rowIDResolvePrefixAB = "01a11708" // names BOTH of the rows above
)

// rowIDResolveSend drives ONE MOVE for the named operand with the peer's side
// instrumented, and answers the send's whole outcome plus how many times the
// peer was reached. Both faces matter and they are NOT interchangeable: a
// RESOLVER refusal is a hard error (the command exits non-zero and no peer is
// contacted), while the leg's own per-peer outcomes — including "not found in
// this ledger" — are fail-soft RECEIPT LINES with exit 0. That asymmetry is the
// defect's signature: it is why a short id used to leave the operator with a
// printed sentence and a zero status.
func rowIDResolveSend(t *testing.T, senderDB, peerDB, operand string) (*SyncRowSendResult, error, int) {
	t.Helper()
	sent := 0
	var res *SyncRowSendResult
	var err error
	withLedger(t, senderDB, func() {
		res, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       operand,
			Peers:        []string{syncRowTestPeer},
			Exchange:     rowGateStorePeer(t, peerDB, &sent),
			CancelSource: true,
		})
	})
	return res, err, sent
}

// rowIDResolveLines is every line a send result printed — the fail-soft face of
// one send, joined for the substring assertions below.
func rowIDResolveLines(res *SyncRowSendResult) string {
	if res == nil {
		return ""
	}
	return strings.Join(res.Lines, "\n")
}

// TestRowSendRefusesATruncatedIDAndNamesTheFullOne is the red→green guard: a
// short id must produce fullid's refusal naming the row, never the leg's
// "not found in this ledger" (which is what the exact match produced).
func TestRowSendRefusesATruncatedIDAndNamesTheFullOne(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() { seedChainRow(t, senderDB, rowIDResolveShortA, "") })

	_, err, sent := rowIDResolveSend(t, senderDB, peerDB, rowIDResolvePrefixAB)
	if err == nil {
		t.Fatalf("a truncated task id drove a MOVE — the operand %q was accepted", rowIDResolvePrefixAB)
	}
	// ① the full id is NAMED (the operator can act on the refusal).
	if !strings.Contains(err.Error(), rowIDResolveShortA) {
		t.Errorf("the refusal does not name the row the operand means (%s): %v", rowIDResolveShortA, err)
	}
	// ①b the sentence says the operand is not a full id — the property that
	// separates this refusal from the ledger's "no such row".
	if !strings.Contains(err.Error(), "not a full id") {
		t.Errorf("the refusal does not say the operand is not a full id: %v", err)
	}
	// ①c THE RED-PROOF: the pre-fix answer. This is the assertion that fails
	// against the exact-match read.
	if strings.Contains(err.Error(), "not found in this ledger") {
		t.Errorf("the short id was reported as absence rather than as a prefix of %s: %v", rowIDResolveShortA, err)
	}
	// ④ nothing left this host, on either ledger.
	if sent != 0 {
		t.Errorf("the refused MOVE contacted the peer %d time(s) — nothing may leave this host", sent)
	}
	if ledgerHas(t, peerDB, rowIDResolveShortA) {
		t.Errorf("the peer's ledger holds the row")
	}
	if got := ledgerStatus(t, senderDB, rowIDResolveShortA); got != TaskStatusPending {
		t.Errorf("the refused MOVE moved the source row: status = %q, want %q", got, TaskStatusPending)
	}
}

// TestRowSendRefusesAnAmbiguousPrefixByName pins ②: an operand naming two rows
// is refused with BOTH — never resolved to whichever row the query reached
// first (which is how a short id silently means "some row").
func TestRowSendRefusesAnAmbiguousPrefixByName(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() {
		seedChainRow(t, senderDB, rowIDResolveShortA, "")
		seedChainRow(t, senderDB, rowIDResolveShortB, "")
	})

	_, err, sent := rowIDResolveSend(t, senderDB, peerDB, rowIDResolvePrefixAB)
	if err == nil {
		t.Fatalf("an ambiguous operand drove a MOVE — %q was resolved to one row", rowIDResolvePrefixAB)
	}
	for _, want := range []string{rowIDResolveShortA, rowIDResolveShortB} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the ambiguity refusal does not name %s: %v", want, err)
		}
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("the refusal does not call the operand ambiguous: %v", err)
	}
	if sent != 0 {
		t.Errorf("the refused MOVE contacted the peer %d time(s)", sent)
	}
}

// TestRowSendAsksTheMoveCriterionForATruncatedID is the arm the whole row is
// about: with a live child on the ledger, the MOVE is refused and the CHILD is
// named. The full id reaches it because the leg resolved the operand first —
// under the exact match the same command answered "not found in this ledger",
// i.e. the criterion was never asked at all.
//
// It asserts the full-id shape (the criterion's own sentence). The truncated
// operand's answer is the resolver's refusal, pinned above — the point of this
// test is that the criterion is now REACHABLE, not skipped.
func TestRowSendAsksTheMoveCriterionForATruncatedID(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() {
		seedChainRow(t, senderDB, rowIDResolveShortA, "")
		seedChainRow(t, senderDB, rowIDResolveChild, rowIDResolveShortA)
	})

	// ③ the criterion, asked with the ledger's own spelling: the child is named.
	_, err, sent := rowIDResolveSend(t, senderDB, peerDB, rowIDResolveShortA)
	if err == nil {
		t.Fatalf("a row with a live child was MOVED")
	}
	if !strings.Contains(err.Error(), rowIDResolveChild) {
		t.Errorf("the criterion's refusal does not name the blocking child %s: %v", rowIDResolveChild, err)
	}
	if !strings.Contains(err.Error(), taskParentReleaseSetLabel()) {
		t.Errorf("the refusal does not name Rule 33's release set: %v", err)
	}
	if sent != 0 {
		t.Errorf("the refused MOVE contacted the peer %d time(s)", sent)
	}
	if got := ledgerStatus(t, senderDB, rowIDResolveShortA); got != TaskStatusPending {
		t.Errorf("source status = %q, want %q", got, TaskStatusPending)
	}

	// And the TRUNCATED spelling of the same row is refused by the resolver
	// before the criterion can be skipped — the regression the row is about.
	short := rowIDResolveShortA[:truncatedOperandLen]
	_, errShort, sentShort := rowIDResolveSend(t, senderDB, peerDB, short)
	if errShort == nil {
		t.Fatalf("the truncated spelling %q was accepted", short)
	}
	if !strings.Contains(errShort.Error(), rowIDResolveShortA) {
		t.Errorf("the truncated spelling does not name the row it means: %v", errShort)
	}
	if sentShort != 0 {
		t.Errorf("the truncated MOVE contacted the peer %d time(s)", sentShort)
	}
}

// truncatedOperandLen is the length of the operand these tests type: long
// enough to be the uuid shape cut short (fullid.ShortPrefixLen is 8 — the
// chunk a listing used to print), short enough to be a prefix of the fixture.
const truncatedOperandLen = 12

// TestRowTransferGateRefusesATruncatedIDWithTheResolverSentence pins the gate's
// OWN face of the same rule: the dependency criterion's entry point must hand
// back the resolver's sentence (the operand, with its candidates), NOT the
// "the dependencies could not be read" frame — nothing failed, the operator
// typed an id the ledger cannot resolve, and that distinction is what makes the
// refusal actionable.
func TestRowTransferGateRefusesATruncatedIDWithTheResolverSentence(t *testing.T) {
	senderDB, _ := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() { seedChainRow(t, senderDB, rowIDResolveShortA, "") })

	var err error
	withLedger(t, senderDB, func() { err = syncRowTransferGate(rowIDResolvePrefixAB, "") })
	if err == nil {
		t.Fatalf("the MOVE criterion accepted the truncated operand %q", rowIDResolvePrefixAB)
	}
	if !strings.Contains(err.Error(), "not a full id") {
		t.Errorf("the gate did not hand back the resolver's sentence: %v", err)
	}
	if !strings.Contains(err.Error(), rowIDResolveShortA) {
		t.Errorf("the gate's refusal does not name the row the operand means: %v", err)
	}
	if strings.Contains(err.Error(), "could not be read") {
		t.Errorf("a resolver refusal was reported as a read failure: %v", err)
	}
}

// TestRowSendStillReportsAbsenceForANonUUIDID is the deliberate NON-change: an
// operand that is not the uuid shape cut short (a legacy `t-<epoch>` id, a
// probe's slug id, a typo) keeps the leg's own missing-row answer. Two refusals
// for one fact would be two places to keep in step, and a stale slug id is not
// an ambiguity anybody is part of.
func TestRowSendStillReportsAbsenceForANonUUIDID(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() { seedChainRow(t, senderDB, rowIDResolveShortA, "") })

	res, err, sent := rowIDResolveSend(t, senderDB, peerDB, "t-not-a-uuid-here")
	if err != nil {
		t.Fatalf("a nonexistent row must keep the leg's ordinary per-peer answer, not a hard error: %v", err)
	}
	lines := rowIDResolveLines(res)
	if !strings.Contains(lines, "not found in this ledger") {
		t.Errorf("a non-uuid operand lost the leg's own absence sentence:\n%s", lines)
	}
	if strings.Contains(lines, "not a full id") {
		t.Errorf("a non-uuid operand was reported as a truncated id:\n%s", lines)
	}
	// The absence is an EXPORT failure, so the peer is never even reached
	// (worth pinning: it is what makes "the operand matched no row" provably
	// a no-op on both ledgers rather than a hope).
	if sent != 0 {
		t.Errorf("a nonexistent row reached the peer %d time(s), want 0", sent)
	}
	if got := ledgerStatus(t, senderDB, rowIDResolveShortA); got != TaskStatusPending {
		t.Errorf("a missing row was cancelled here anyway: status = %q", got)
	}
}
