package tools

// zz_redproof_landed_record_address_test.go — WHICH NAME the third arm accepts
// as "this row's address", and the proof that accepting only ONE of them leaves
// the arm unreachable for a whole class of rows.
//
// THE DEFECT THIS PINS (measured 2026-10-27, on row
// 01a17abb-7de0-7479-a8a7-20340f489b5d). The landed record's WRITER and its
// READER derive the row's address at two different times, so across the
// `90f6a96` wiring boundary they disagree:
//
//	$ gopi task show 01a17abb-7de0-7479-a8a7-20340f489b5d | grep deliverable.branch
//	deliverable.branch: master          ← stamped at CLAIM time, before the wiring
//	                                    ← the writer records what the workspace
//	                                      STOOD ON when it pushed: task/<id>
//
// A reader comparing only `deliverable.branch` therefore answers "no record of
// this branch" for a row that HAS one — the third arm unreachable, silently, in
// the direction opposite to the `landedRecordBranchOf` defect. The fix is that
// BOTH spellings are the row's own address; nothing else is.

import (
	"fmt"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

const zzraTipHex = "0123456789abcdef0123456789abcdef01234567"

// zzraRecord writes ONE landed record in the writer's own grammar, for `branch`.
func zzraRecord(id, branch, tip string) {
	AppendTaskTrailEvent(id, DeliveryLandedEventName, fmt.Sprintf(
		"clone channel: delivered %s from %s at %s — pushed to origin after 1 attempt(s)",
		branch, "/tmp/zzra-ground", tip))
}

// TestZZRedProof_LandedRecordAcceptsTheRowsDerivedAddress is the RED half: the
// post-fix writer names `task/<id>`, the row still declares `master`, and the
// record must STILL be read as evidence about this row.
func TestZZRedProof_LandedRecordAcceptsTheRowsDerivedAddress(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	const id = "zzra-old-shape-row"

	dpSeedRow(t, db, id, "the row claimed before the ground-branch wiring", taskStatusInProgress)
	// THE OLD SHAPE: deliverable.branch is the integration branch, stamped at
	// claim time and never rewritten.
	dpWriteDelivery(t, db, id, "/tmp/zzra-repo", masterBranchName, "/tmp/zzra-ground")

	// THE NEW WRITER: the ground stood on the row's own address when it pushed.
	own := TaskGroundBranchName(id)
	if own == "" {
		t.Fatalf("TaskGroundBranchName(%q) is empty — the fixture premise is broken", id)
	}
	if own == masterBranchName {
		t.Fatalf("the fixture premise is broken: the derived address must differ from %q", masterBranchName)
	}
	zzraRecord(id, own, zzraTipHex)

	ok, detail := deliveryProofLandedRecorded(id, masterBranchName, zzraTipHex)
	if !ok {
		t.Fatalf("the landed record names %q (what the post-fix channel's workspace STANDED on) and the row "+
			"declares %q (what the claim stamped before the wiring): BOTH are this row's own address, so the record "+
			"is evidence about THIS row. Answering false makes the third arm unreachable for every row claimed "+
			"before 90f6a96 — the exact class it exists to rescue", own, masterBranchName)
	}
	if detail == "" {
		t.Error("a hit must carry the record's own detail back as the evidence sentence")
	}
}

// TestZZRedProof_LandedRecordStillRefusesAForeignBranch is the mirror that keeps
// the fix from being a loophole: accepting the derived address must not turn
// into accepting ANY branch.
func TestZZRedProof_LandedRecordStillRefusesAForeignBranch(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	const id = "zzra-foreign-branch-row"

	dpSeedRow(t, db, id, "the row whose record names somebody else's ref", taskStatusInProgress)
	dpWriteDelivery(t, db, id, "/tmp/zzra-repo", masterBranchName, "/tmp/zzra-ground")
	zzraRecord(id, "some/other/rows/branch", zzraTipHex)

	if ok, detail := deliveryProofLandedRecorded(id, masterBranchName, zzraTipHex); ok {
		t.Fatalf("a record naming \"some/other/rows/branch\" is evidence about a branch this row never claimed, "+
			"so it must answer false (got a hit: %s)", detail)
	}
}

// TestZZRedProof_LandedRecordStillRefusesAnotherTip keeps the OTHER half of the
// narrowness under the widened address set: the address matches, the commit
// does not.
func TestZZRedProof_LandedRecordStillRefusesAnotherTip(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	const id = "zzra-other-tip-row"

	dpSeedRow(t, db, id, "the row whose record names another commit", taskStatusInProgress)
	dpWriteDelivery(t, db, id, "/tmp/zzra-repo", masterBranchName, "/tmp/zzra-ground")
	zzraRecord(id, TaskGroundBranchName(id), "ffffffffffffffffffffffffffffffffffffffff")

	if ok, detail := deliveryProofLandedRecorded(id, masterBranchName, zzraTipHex); ok {
		t.Fatalf("the record names a DIFFERENT commit of the same address — \"this row was delivered once\" is "+
			"not \"THIS commit was delivered\", so it must answer false (got a hit: %s)", detail)
	}
}
