package tools

import "testing"

// TestTaskGroundBranchNameIsTheWorktreeChannelsOwnSpelling pins the ONE spelling
// the clone channel hands to internal/devworkspace.
//
// WHY THE TWO CHANNELS MUST AGREE (the defect this closes, measured
// 2026-10-27): a clone checks out the SOURCE's branch, so before this fix a
// clone ground stood on `master` and recorded `deliverable.branch: master` —
// while a worktree ground stood on `task/<row-uuid>`. One ledger field therefore
// meant two different things depending on which channel minted the ground, and
// the (then-live) branch→row lookup answered "361 owners of branch master": the ref
// mapped back to no row at all. The exported name must be the DERIVED name the
// worktree channel already mints, not a second spelling invented for the clone
// channel — otherwise the two channels drift again and the ledger field keeps
// two meanings.
func TestTaskGroundBranchNameIsTheWorktreeChannelsOwnSpelling(t *testing.T) {
	const id = "01a11554-b3b1-731e-8425-2ce4986786f9"
	want := taskGroundBranchFor(id, "", "") // the worktree channel's minting path
	if got := TaskGroundBranchName(id); got != want {
		t.Fatalf("TaskGroundBranchName(%q) = %q, but the worktree channel mints %q — one ledger field, one spelling", id, got, want)
	}
	if want != TaskGroundBranchPrefix+taskGroundIDStem(id) {
		t.Fatalf("the derived name changed shape: %q", want)
	}
	// A blank id has no ledger address to mint, so the clone channel is handed
	// "" and keeps its legacy shape (stand on the source's branch). This is the
	// backward-compatibility arm: a caller with no row must NOT be given
	// "task/" plus a fallback stem, which would publish a ref no row owns.
	if got := TaskGroundBranchName(""); got != "" {
		t.Fatalf("a blank task id must mint no branch; got %q", got)
	}
	// Two different rows never share a ref (the property `for-branch` relies on).
	other := "01a141a7-0000-7000-8000-00000000beef"
	if TaskGroundBranchName(id) == TaskGroundBranchName(other) {
		t.Fatalf("two rows minted the same ref")
	}
}
