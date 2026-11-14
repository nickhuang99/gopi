package main

// clone_landed_record_branch_test.go — WHICH BRANCH the clone channel's landed
// record NAMES (cmd/agent/devworkspace_channel.go, `landedRecordBranchOf`).
//
// THE DEFECT THIS PINS (measured 2026-10-27, after `90f6a96` wired the channel
// to GroundBranch/BaseBranch): a post-fix clone ground STANDS on — and the
// claim records — the row's own ledger address `task/<id>`, while
// `publishGround` pushes ONE destination (the integration branch). The landed
// record was naming the INTEGRATION branch, so for exactly the rows the record
// exists for, the delivery proof's reader looked for `task/<id>` in a record
// that said `master` and answered "no record of this branch" — the third arm
// unreachable, silently.
//
// The next round's reader is `deliveryProofLandedRecorded(taskID, branch, tip)`
// (internal/tools), and `branch` there is the row's `deliverable.branch`. So the
// ONE property this file asserts is: the name the channel writes is the name
// the row declares — the ADDRESS — and the integration branch is only the
// fallback for a result that never named what it stood on.
//
// WHICH NAME IS RIGHT IS A QUESTION ABOUT THE CONFIG, not about `master` or
// `task/<id>`: the operator's ruling of 2026-11-05 retired the per-ground
// private ref, so the clone channel's own shape now stands on — and records —
// the INTEGRATION branch, while a caller that still names a private address
// records that instead. Both directions are pinned below, which is what keeps
// this from being a fix that merely swaps one hard-coded name for another.

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestLandedRecordNamesTheRowsOwnAddress is the regression: a ground-branch run
// records `task/<id>`, i.e. what the claim stamps as `deliverable.branch`.
func TestLandedRecordNamesTheRowsOwnAddress(t *testing.T) {
	const taskID = "01a17abb-7de0-7479-a8a7-20340f489b5d"
	own := tools.TaskGroundBranchName(taskID)
	if own == "" {
		t.Fatalf("tools.TaskGroundBranchName(%q) is empty — the fixture premise is broken", taskID)
	}
	cfg := devworkspace.Config{
		TaskID:       taskID,
		GroundBranch: own,
		BaseBranch:   devworkspace.DefaultBranch,
	}
	// The result AttemptDelivery returns on a delivered push: Branch is what the
	// workspace STOOD ON (the address), the Detail names what the push MOVED.
	res := devworkspace.Result{Branch: own, Pushed: true,
		Detail: "pushed " + devworkspace.DefaultBranch + " to origin after 1 attempt(s)"}

	if got := landedRecordBranchOf(cfg, res); got != own {
		t.Fatalf("the landed record names %q, but the row declares %q — "+
			"the delivery proof's reader matches on deliverable.branch, so this record is evidence about a branch no row claims",
			got, own)
	}
}

// TestLandedRecordFallsBackToTheIntegrationBranch is the OTHER direction, and it
// is not decoration: a legacy ground (opened before the GroundBranch wiring)
// stands on `master` and its row records `master`, so `master` is the correct
// name there. Pinning both directions is what keeps this from being a fix that
// merely swaps one hard-coded name for another.
func TestLandedRecordFallsBackToTheIntegrationBranch(t *testing.T) {
	cfg := devworkspace.Config{TaskID: "01a17abb-7de0-7479-a8a7-20340f489b5d",
		BaseBranch: devworkspace.DefaultBranch}
	// A result that named no branch at all (the shape an early-exit caller
	// returns): nothing better is known, so the integration branch is named.
	res := devworkspace.Result{Pushed: true}

	if got := landedRecordBranchOf(cfg, res); got != devworkspace.DefaultBranch {
		t.Fatalf("with no branch on the result the record must name the integration branch %q, got %q",
			devworkspace.DefaultBranch, got)
	}
}
