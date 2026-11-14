package tools

// THE RED PROOF for row 01a183fa's repair, written so it COMPILES ON BOTH TREES.
//
// The acceptance requirement is "the new regression test is RED on the unfixed
// code and GREEN after the fix", and a test written in the fix's own vocabulary
// cannot be compiled into the unfixed tree at all — it would fail to build, which
// proves nothing about behaviour (memory #14229's rule for red proofs: use only
// pre-fix API symbols).
//
// So this arm asserts the INVARIANT rather than the new state names: for the
// incident's shape — the delivery point holds the very tip, and no directory on
// this host can read that tip's objects — the arm must NOT emit the deterministic
// absence sentence and must NOT report a measured absence.
//
//	pre-fix   state = "undelivered", note carries "is not at the destination it
//	          declared"                    → this arm FAILS (observed 2026-10-29)
//	post-fix  state = "confirmed" | "unverified", no such sentence → PASSES
//
// The two state strings that a measured absence has ever been spelled with are
// compared as literals, so the file needs no constant from either tree.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// deliveryProofAbsenceSpellings is every value the criterion has ever used for a
// MEASURED absence ("this work is provably not at its destination"). Both are
// listed because the arm has to run against the tree that spells it either way.
var deliveryProofAbsenceSpellings = []string{"undelivered", "absent"}

// deliveryProofAbsenceSentence is the deterministic claim this repair removes from
// the unmeasurable case, spelled out because the pre-fix tree has no constant for
// it that a post-fix tree also has.
const deliveryProofAbsenceSentence = "its deliverable is not at the destination it declared"

// TestDeliveryProofRedProof_UnreadableDestinationIsNeverAnAbsence is the arm.
func TestDeliveryProofRedProof_UnreadableDestinationIsNeverAnAbsence(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, ground, _, tip := dpuoFixture(t)

	id := "dpuo-redproof"
	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "fix: the work landed and the local view was stale", taskStatusInProgress)
	// `master` is the destination the clone channel actually publishes
	// (HEAD:refs/heads/master), so it is the ref that resolves — the row's own
	// task/<id> address is never pushed anywhere.
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)
	dpggRecord(t, db, id, branch, ground, tip)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	for _, absence := range deliveryProofAbsenceSpellings {
		if out.State == absence {
			t.Fatalf("state = %q: the delivery point holds the tip %s and only the LOCAL view was unreadable, "+
				"so this is an absence of evidence — a measured absence was reported instead.\nnote=%s",
				out.State, branchDeleteShortHash(tip), out.Note)
		}
	}
	if strings.Contains(out.Note, deliveryProofAbsenceSentence) {
		t.Fatalf("the arm printed the deterministic sentence %q while its own measurement could not read the tip "+
			"(%s): a sentence about the destination's contents may not be written by a run that did not measure "+
			"them.\nnote=%s", deliveryProofAbsenceSentence, branchDeleteShortHash(tip), out.Note)
	}
}
