package tools

// claim_push_receipt_honesty_test.go — the peer-free REGRESSION GUARD for
// the manual push's receipt.
//
// THE DEFECT THIS FILE LOCKS DOWN (measured 2026-09-21 on peerbox, re-measured
// 2026-10-12 from the client host): `gopi task sync --push-only --task-id T`
// rides the CLAIM BROADCAST. The peer mirrors the row's LOCK and answers a
// verdict; it never writes the row into its ledger. The success line said
//
//	push peerbox: accepted (1 row(s))
//
// and exit 0 — TRUE about the LOCK, read as "the row arrived". The follow-up
// found the peer's tasks table missing the row entirely. A host's local policy
// paragraph then pointed at this very command as "the way to get a row into the
// peer's queue", which makes a LOCK receipt the persistence criterion (Rule 15b:
// prove the EFFECT, never the receipt).
//
// WHAT IS ASSERTED, and why each half is needed:
//
//	1. the receipt NAMES the lock and DENIES the write (both directions — a
//	   line that only says "LOCK mirrored" still leaves the operator thinking
//	   it landed, and a line that only denies loses the fact that something
//	   DID cross the wire);
//	2. it hands over the verb that really deposits the row (`task send`), and
//	   that verb is DERIVED FROM the same constants the CLI already owns —
//	   asserted by construction here, so a future edit cannot quietly print a
//	   command the parser refuses;
//	3. it hands over a peer-side check, so the operator settles the question
//	   from the OTHER end rather than trusting either line;
//	4. the OLD wording no longer stands alone: a bare `accepted (N row(s))`
//	   headline is gone.
//
// NO REAL PEER IS TOUCHED. The ssh transport is the claim seam
// (claimSSHExchangeHook) the family already uses, and the assertion runs
// against the REAL renderer (`syncClaimPushReceipt`) plus the REAL push
// function (`SyncTaskPushOnlyPeers`), so this test dies with the wording rather
// than with a copy of it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/taskverbs"
)

const (
	// claimReceiptHonestyPeer is the ONE declared (fake) peer alias.
	claimReceiptHonestyPeer = "gopi-receipt-honesty-peer"
	// claimReceiptHonestyRow is the row the push names.
	claimReceiptHonestyRow = "01a12d93-0cc1-7b37-a528-daad6b6ea3a7"
	// claimReceiptHonestyPeerReason is what a peer answers for an already-
	// pending row. To THIS side it is an opaque string: the receipt quotes it
	// and never interprets it. Spelled here because the constant it used to
	// borrow lived in the peer handler, which was deleted with the claim lock.
	claimReceiptHonestyPeerReason = "already pending \u2014 nothing to revert"
)

// TestClaimPushReceiptNamesTheLockAndDeniesTheWrite is the RED→GREEN proof on
// the renderer itself: the sentence a successful push prints.
func TestClaimPushReceiptNamesTheLockAndDeniesTheWrite(t *testing.T) {
	line := syncClaimPushReceipt(claimReceiptHonestyPeer, 1, claimReceiptHonestyRow)

	// (1a) it names WHAT crossed: the lock, in the family's own vocabulary.
	if !strings.Contains(line, syncClaimLockMirroredMarker) {
		t.Errorf("the receipt does not name the LOCK leg (%q) — the old wording was true about the lock "+
			"and read as the row:\n%s", syncClaimLockMirroredMarker, line)
	}
	// (1b) it DENIES the write. This is the half the defect omitted.
	if !strings.Contains(line, syncClaimNotStoredMarker) {
		t.Errorf("the receipt does not say the row is absent from the peer's ledger (%q) — "+
			"an exit-0 line that implies persistence is the defect this epic exists to close:\n%s",
			syncClaimNotStoredMarker, line)
	}
	// (2) it hands over what the operator DOES — and since 2026-11-06 that is
	// NOT a verb: `task send` was DELETED with the cross-node row legs, so a
	// receipt that printed it was recommending a command that answers with a
	// removal notice and exit 1. The assertion is now that the receipt routes
	// the operator to the ROUTE THAT WORKS (file the row on the host that will
	// run it — Rule 28 / Rule 40) and still names the ROW and the knob, so it
	// remains actionable without spelling a dead command.
	if !strings.Contains(line, syncClaimDepositRecipe) {
		t.Errorf("the receipt does not carry the deposit recipe (%q):\n%s", syncClaimDepositRecipe, line)
	}
	if strings.Contains(line, cliverbs.Task+" "+taskverbs.Send) {
		t.Errorf("the receipt still prints the DELETED verb `%s %s` — that command answers with a removal "+
			"notice and exit 1:\n%s", cliverbs.Task, taskverbs.Send, line)
	}
	if !strings.Contains(line, claimReceiptHonestyRow) {
		t.Errorf("the receipt does not carry the ROW it is about (%q):\n%s", claimReceiptHonestyRow, line)
	}
	// (3) it hands over the peer-side check, naming the peer and the verb.
	if !strings.Contains(line, claimReceiptHonestyPeer) ||
		!strings.Contains(line, taskverbs.Show) {
		t.Errorf("the receipt does not give a peer-side recheck naming the peer (%q) and `task show`:\n%s",
			claimReceiptHonestyPeer, line)
	}
	// (4) the OLD headline is gone: `accepted` may appear only INSIDE the
	// lock clause, never as the verdict the line leads with. A line that opens
	// `push <peer>: accepted` is exactly the pre-fix shape, whatever follows.
	legacyHeadline := "push " + claimReceiptHonestyPeer + ": " + syncClaimAcceptedVerdictWord
	if strings.Contains(line, legacyHeadline) {
		t.Errorf("the receipt still OPENS with the pre-fix headline %q — the verdict may be quoted inside "+
			"the lock clause, never stand alone as the conclusion:\n%s", legacyHeadline, line)
	}
	// ...and the verdict word is still present (dropping it would hide an
	// exchange that really happened — the mirror image of the same lie).
	if !strings.Contains(line, syncClaimAcceptedVerdictWord) {
		t.Errorf("the receipt dropped the peer's own verdict (%q) — the lock exchange DID happen and "+
			"silence about it is its own inaccuracy:\n%s", syncClaimAcceptedVerdictWord, line)
	}
}

// TestClaimPushReceiptDepositCommandIsTheParsersOwnSpelling closes the drift
// door: the command the receipt recommends must be spelled from the CLI's own
// vocabulary, not re-typed here. It asserts the DERIVATION (the printed
// command equals the same constants composed independently), so renaming a verb
// or a flag on either side fails HERE instead of printing a refusal to an
// operator who copy-pastes the receipt.
func TestClaimPushReceiptDepositCommandIsTheParsersOwnSpelling(t *testing.T) {
	line := syncClaimPushReceipt(claimReceiptHonestyPeer, 1, claimReceiptHonestyRow)
	// THE DEPOSIT COMMAND IS GONE, and not because the wording drifted: the
	// verb it named was deleted, so there is no composed spelling left to
	// assert. What is asserted instead is the property that made the old
	// assertion worth having — the receipt recommends NOTHING this binary
	// would refuse. Two halves: the retired spelling must not appear, and the
	// recipe it does print must be the one constant the package owns.
	if strings.Contains(line, taskverbs.Send) {
		t.Fatalf("the receipt prints the DELETED verb %q:\n%s", taskverbs.Send, line)
	}
	if !strings.Contains(line, syncClaimDepositRecipe) {
		t.Fatalf("the receipt does not carry the deposit recipe %q:\n%s", syncClaimDepositRecipe, line)
	}
	// The peer-side check is the SAME sentence the submit forwarder proves a
	// landed row with (task_submit_forward.go's taskSubmitConfirmVerb) — one
	// definition, referenced twice.
	wantCheck := sshCommandWord + " " + claimReceiptHonestyPeer + " " +
		harvestShellQuote(syncRemoteGopi+" "+taskSubmitGroupVerb+" "+taskverbs.Show+" "+claimReceiptHonestyRow)
	if !strings.Contains(line, wantCheck) {
		t.Fatalf("the receipt's peer-side check is not the forwarder's own confirm verb %q:\n%s", wantCheck, line)
	}
}

// TestClaimPushReceiptOnAWholeWindowNamesNoInventedRow pins the window case:
// without --task-id the push carries the whole delta window, so there is no
// single row to name — the receipt must say so with a placeholder rather than
// print a command pointing at a row that does not exist.
func TestClaimPushReceiptOnAWholeWindowNamesNoInventedRow(t *testing.T) {
	line := syncClaimPushReceipt(claimReceiptHonestyPeer, 3, "")
	if !strings.Contains(line, syncClaimRowPlaceholder) {
		t.Errorf("a whole-window push printed no %q placeholder — the recommended command would address "+
			"a row the receipt never named:\n%s", syncClaimRowPlaceholder, line)
	}
	if !strings.Contains(line, syncClaimNotStoredMarker) {
		t.Errorf("a whole-window push's receipt still does not deny the write:\n%s", line)
	}
}

// TestSyncTaskPushOnlyReceiptIsHonestOverTheRealPush is the END-TO-END half:
// the REAL function the CLI calls (`SyncTaskPushOnlyPeers`, which
// `gopi task sync --push-only` reaches through cmd/agent) must PRINT the honest
// receipt — the renderer being correct is not enough if the push path bypasses
// it. The ssh leg is the family's own test seam, so no peer and no network are
// involved.
func TestSyncTaskPushOnlyReceiptIsHonestOverTheRealPush(t *testing.T) {
	claimTestDB(t)
	t.Setenv(configSyncPeersEnv, claimReceiptHonestyPeer)

	// A REAL row, through the real write path — the push's row selection
	// (window export + full-export fallback) resolves it or the run fails
	// before any line is printed, so the fixture cannot fake the row.
	id := claimSeedTask(t, "claim push receipt honesty")

	// The seam answers what a real `--claim-remote` handler answers for a
	// pending row: accepted, with the lock verdict in the reason.
	SetClaimSSHExchangeHookForTest(func(peer string, _ []byte) ([]byte, error) {
		resp := ClaimPushResponse{
			Accepted: true,
			Reason:   id + ": " + claimReceiptHonestyPeerReason,
			Node:     peer,
		}
		return json.Marshal(resp)
	})
	t.Cleanup(func() { SetClaimSSHExchangeHookForTest(nil) })

	lines, err := SyncTaskPushOnlyPeers(id, []string{claimReceiptHonestyPeer})
	if err != nil {
		t.Fatalf("SyncTaskPushOnlyPeers(%s): %v", id, err)
	}
	if len(lines) != 1 {
		t.Fatalf("the push produced %d line(s), want exactly 1:\n%q", len(lines), lines)
	}
	line := lines[0]
	// The deposit half is a RECIPE, not a command: `task send` was DELETED
	// 2026-11-05 and the receipt must not recommend it (asserted just below).
	// What the operator still needs from this line is the row it is about.
	for _, want := range []string{
		syncClaimLockMirroredMarker,
		syncClaimNotStoredMarker,
		syncClaimDepositRecipe,
		id,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the REAL push surface printed a line without %q:\n%s", want, line)
		}
	}
	// The defect's exact shape must be unprintable: a success line whose
	// headline is the bare verdict about a row that never landed.
	if strings.Contains(line, "push "+claimReceiptHonestyPeer+": accepted") {
		t.Fatalf("the REAL push surface still prints the pre-fix headline — the LOCK receipt is "+
			"presented as the row's arrival:\n%s", line)
	}
}

// TestClaimPushReceiptHasNoCLIFaceAnyMore is the INVERTED surface pin.
//
// It used to assert the opposite: that cmd/agent/cli_tasks_sync.go — the file
// behind `gopi task sync --push-only` — still called tools.SyncTaskPushOnlyPeers,
// so the honest receipt could not become dead code behind a second path. THAT
// FILE IS DELETED (2026-11-05, the cross-node sync family removal), and the
// requirement inverts with it: no CLI spelling in this binary may reach the
// primitive, because a receipt only a test can reach is not a user-visible
// surface — and leaving the old assertion would have kept the deletion
// unverified by reading a file that no longer exists.
//
// The properties below are the two halves that survive: the file is gone, and
// nothing under cmd/agent names the push primitive. A future spelling that
// reintroduces a cross-node push must fail here and be told to say why.
func TestClaimPushReceiptHasNoCLIFaceAnyMore(t *testing.T) {
	deleted := filepath.Join("..", "..", "cmd", "agent", "cli_tasks_sync.go")
	if _, err := os.Stat(deleted); err == nil {
		t.Fatalf("%s is back in the tree — the cross-node sync family was deleted 2026-11-05 and this "+
			"test's premise (no CLI face reaches the push primitive) no longer holds: either remove "+
			"the file again or rewrite this pin for the surface you just added", deleted)
	}
	entries, err := os.ReadDir(filepath.Join("..", "..", "cmd", "agent"))
	if err != nil {
		t.Fatalf("read cmd/agent: %v", err)
	}
	var offenders []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join("..", "..", "cmd", "agent", e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.Contains(string(src), "tools.SyncTaskPushOnlyPeers(") {
			offenders = append(offenders, e.Name())
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("cmd/agent still reaches the cross-node push primitive from %v — that family is "+
			"DELETED, so a spelling calling it either does not run or advertises a protocol the binary "+
			"no longer speaks", offenders)
	}
}
