package tools

// archived_branch_retirement_test.go — the RETIRED-BRANCH repro, driven through
// the REAL surfaces (Rule 31) in the shape that was measured:
//
//	retirement on host A  →  a LIVE merge-backlog row naming that branch  →
//	handover to host B    →  the row is closed / refused BY NAME
//
// The fixture is the measured row (task 01a1446e's bug row, filed by run
// 01a141c4 and read back from this host's ledger on 2026-10-16):
//
//	summary:     merge task/probe-zombie into master
//	deliverable: {"branch":"task/01a141c4615b7b0d9f47a8c5e946ce07","repo":…}
//	refs:        refs/tags/archive/task/probe-zombie
//	             refs/gopi/archive-reason/task/probe-zombie  (reason: unrelated-history)
//
// The branch is an ORPHAN (archOrphanBranch: a second root commit), because that
// is the only shape for which "no merge lane can land it" is true — an ordinary
// branch off a shared commit is a fast-forward question, not an unrelated-history
// one.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// retirementTestRowDeliverable renders the deliverable blob a merge-backlog row
// carries: the run's own GROUND (task/<id>) with the repo the question is asked
// in. It is not repoDeliv (repo_resolve_row_test.go), whose repo is a fixed
// /tmp/t — the seam resolves deliverable.repo and asks git THERE, so the fixture
// must name the real temp repo.
func retirementTestRowDeliverable(repo, ground string) string {
	return fmt.Sprintf(`{"repo":%q,"branch":%q}`, repo, ground)
}

// retirementTestSeedInputs is the fixture's own shape constants, so a reader can
// map them onto the measured row above line by line.
const (
	retirementTestBranch = "task/probe-zombie"
	retirementTestGround = "task/01a141c4615b7b0d9f47a8c5e946ce07"
)

// TestRetiredBranchIsRefusedAtBothSeams is the defect's repro. It asserts the
// two seams the epic names, each on the state the OTHER one left untouched:
//
//	① the DISPATCH/CLAIM seam (TaskNextFiltered — the one funnel every
//	   dispatcher shares): the live row is never handed to a solver, and it is
//	   closed carrying the DECISION (tag + code + gloss), not a guess;
//	② the HANDOVER seam (TaskTransferHandover — the write that moved this row
//	   across hosts): a retired branch's row is REFUSED by name, and the refusal
//	   writes NOTHING — the row is still pending afterwards, so the source
//	   ledger has not silently cancelled work on an answer it did not get.
func TestRetiredBranchIsRefusedAtBothSeams(t *testing.T) {
	testutil.NewTestDBPath(t)
	repo := archTestRepo(t)
	archOrphanBranch(t, repo, retirementTestBranch, "unrelated root")

	// HOST A — the DECIDING pass: the branch is retired (tag + reason record,
	// through the project's one archive writer, exactly as `gopi merge`'s
	// unrelated-history verdict does it).
	if err := RetireUnrelatedHistoryBranch(repo, retirementTestBranch); err != nil {
		t.Fatalf("retire %s: %v", retirementTestBranch, err)
	}
	// The premise, read back from the record itself (never from the writer's
	// return value): the ONE predicate answers YES, and it names the decision.
	rec, retired := ArchivedBranchExclusion(repo, retirementTestBranch)
	if !retired {
		t.Fatalf("the retirement just written does not read back as an exclusion — the predicate and the writer disagree "+
			"(tag %s, record %s)", ArchiveBranchPrefix+retirementTestBranch, archiveReasonRefPrefix+retirementTestBranch)
	}
	if rec.Reason != ArchiveReasonUnrelatedHistory || !strings.Contains(rec.Tag, ArchiveBranchPrefix+retirementTestBranch) {
		t.Fatalf("the predicate answered %+v, want reason %q and the tag naming the branch",
			rec, ArchiveReasonUnrelatedHistory)
	}

	// ① THE DISPATCH/CLAIM SEAM. The row is filed AFTER the retirement, which is
	// the measured situation: the deciding pass had already finished on the
	// other host, so nothing on this side ever re-read the decision.
	dispatchID := newRow(t, "retired", mergeBacklogSummaryVerbFilled(retirementTestBranch),
		taskStatusPending, mergeBacklogTag, "", "merger",
		retirementTestRowDeliverable(repo, retirementTestGround), "")

	res, err := TaskNextFiltered("retirement-pin", "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res != nil && !res.Empty && res.Task != nil {
		t.Fatalf("a session was dispatched onto a retired branch: row %s was claimed", res.Task.TaskID)
	}
	status, reason := recRowState(t, dispatchID)
	if status != taskStatusCancelled {
		t.Fatalf("row %s status = %q, want %q — the claim funnel must close it, not leave it claimable",
			dispatchID, status, taskStatusCancelled)
	}
	for _, want := range []string{MergeBacklogRetiredMarker, retirementTestBranch,
		ArchiveBranchPrefix + retirementTestBranch, ArchiveReasonUnrelatedHistory} {
		if !strings.Contains(reason, want) {
			t.Errorf("the closed row's reason %q does not name %q — a close that does not say WHY is a silent close",
				reason, want)
		}
	}
	// The decision SURVIVES the refusal: the tag is the decision, and no seam is
	// allowed to drop it (the epic's own constraint — never delete the tag).
	if !archRefResolves(t, repo, ArchiveBranchPrefix+retirementTestBranch) {
		t.Errorf("the archive tag is gone after the guard ran — the decision must be preserved")
	}

	// ② THE HANDOVER SEAM. A second row in the same shape — the one an
	// initiator asks the peer to sign over.
	handoverID := newRow(t, "retiredhand", mergeBacklogSummaryVerbFilled(retirementTestBranch),
		taskStatusPending, mergeBacklogTag, "the row a handover would carry", "merger",
		retirementTestRowDeliverable(repo, retirementTestGround), "")

	herr := TaskTransferHandover(handoverID, syncRowTestPeer, "", "")
	if herr == nil {
		t.Fatalf("the handover SIGNED a retired branch's row (%s) — the receiving ledger would claim it and spend a "+
			"session on a void premise", handoverID)
	}
	var inel *SyncTransferIneligible
	if !errors.As(herr, &inel) {
		t.Fatalf("handover refusal = %T (%v), want *SyncTransferIneligible — a refusal must be an ANSWER, not a transport error",
			herr, herr)
	}
	for _, want := range []string{retirementTestBranch, ArchiveBranchPrefix + retirementTestBranch,
		ArchiveReasonUnrelatedHistory, ArchiveReasonDoc(ArchiveReasonUnrelatedHistory)} {
		if !strings.Contains(inel.Why, want) {
			t.Errorf("the refusal %q does not name %q — it must name the DECISION, not restate the why",
				inel.Why, want)
		}
	}
	if status, _ := recRowState(t, handoverID); status != taskStatusPending {
		t.Errorf("row %s status = %q after a REFUSED handover, want it untouched (%q)",
			handoverID, status, taskStatusPending)
	}
}

// TestRetiredBranchRefusalSpellingsAndFalsePositives is the predicate's own
// boundary test, and it exists so the two failure modes Rule 15's default-deny
// direction names are BOTH pinned:
//
//   - a branch that is archived WITHOUT a reason from the closed vocabulary is
//     not a decided exclusion (no credit for a typo) — the row stays claimable,
//     because cancelling work on an unrecorded record is the opposite error;
//   - the SPELLING arms: the comparison is by canonical spec, so the ground
//     spelling of a `task/<id>` branch answers YES, and a DIFFERENT branch never
//     does. The measured reason this arm exists: the live namespaces carry
//     `refs/tags/archive/task/probe-zombie` beside `refs/tags/archive/<task-id>-20261007`,
//     i.e. both spellings are in use, and a verbatim comparison would answer NO
//     for one of them.
func TestRetiredBranchRefusalSpellingsAndFalsePositives(t *testing.T) {
	repo := archTestRepo(t)
	// A tag with NO companion reason record (the 2026-09-16 21:04 shape) on its
	// own branch, so the writer's idempotence arm (a branch whose tag already
	// exists is left alone, so no record is ever written for it) cannot mask the
	// predicate's arm under test.
	const unrecorded = "task/no-reason-probe"
	archOrphanBranch(t, repo, unrecorded, "unrelated root")
	archGit(t, repo, "tag", ArchiveBranchPrefix+unrecorded, unrecorded)
	if _, ok := ArchivedBranchExclusion(repo, unrecorded); ok {
		t.Error("a tag with no reason record was read as a DECIDED exclusion — an unrecorded decision is not one")
	}
	archOrphanBranch(t, repo, retirementTestBranch, "unrelated root")
	if err := RetireUnrelatedHistoryBranch(repo, retirementTestBranch); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if _, ok := ArchivedBranchExclusion(repo, retirementTestBranch); !ok {
		t.Error("the recorded decision did not read back")
	}
	// The GROUND spelling of the same branch (the task-id vs ground rename).
	if got, ok := ArchivedBranchExclusion(repo, "probe-zombie"); !ok {
		t.Error("the ground spelling `probe-zombie` did not resolve to the decision recorded for " +
			retirementTestBranch + " — the canonical spec is what makes the two spellings one branch")
	} else if got.Branch != retirementTestBranch {
		t.Errorf("the answer names the branch as %q, want the RECORD's own spelling %q (a `git tag -d` needs that one)",
			got.Branch, retirementTestBranch)
	}
	// FALSE POSITIVES, one per over-reach the spec could have made:
	archOrphanBranch(t, repo, "unrelated/task/probe-zombie", "an unrelated branch that ends with the same segments")
	for _, branch := range []string{"probe-zombie-2", "task/probe-zombie-2", "unrelated/task/probe-zombie",
		"probe", "", "no-reason-probe"} {
		if _, ok := ArchivedBranchExclusion(repo, branch); ok {
			t.Errorf("branch %q was folded onto the retired %s — the spec must not merge unrelated names",
				branch, retirementTestBranch)
		}
	}
}

// TestRetirementReconcilesTheRowSpelledWithTheGroundName pins the RECONCILER's
// half of the same predicate: a row whose summary names the branch by its GROUND
// spelling is closed by a retirement recorded under the `task/<id>` spelling.
//
// It is the arm the deciding pass itself reads (archiveFamilyRowsForBranch →
// CanonicalBranchRetirementSpec, ONE canonicalization for both seams), and the
// honest statement of its scope is in the report: the MEASURED row
// (01a141c4) spelled the retired branch exactly, so the spelling arm is
// robustness for the two live namespaces this host carries, not the mechanism
// that closed the measured row — that mechanism is the two seams.
func TestRetirementReconcilesTheRowSpelledWithTheGroundName(t *testing.T) {
	testutil.NewTestDBPath(t)
	repo := archTestRepo(t)
	archOrphanBranch(t, repo, retirementTestBranch, "unrelated root")
	rowID := newRow(t, "retiredground", mergeBacklogSummaryVerbFilled("probe-zombie"),
		taskStatusPending, mergeBacklogTag, "", "merger", "", "")

	rec, err := ReconcileUnrelatedHistoryRetirement(repo, retirementTestBranch)
	if err != nil {
		t.Fatalf("ReconcileUnrelatedHistoryRetirement: %v", err)
	}
	if len(rec.Closed) != 1 || rec.Closed[0].TaskID != rowID {
		t.Fatalf("closed = %+v, want exactly the row that names the branch by its ground spelling (%s)", rec.Closed, rowID)
	}
	status, reason := recRowState(t, rowID)
	if status != taskStatusCancelled {
		t.Errorf("row %s status = %q, want %q", rowID, status, taskStatusCancelled)
	}
	if !strings.Contains(reason, ArchiveReasonUnrelatedHistory) || !strings.Contains(reason, retirementTestBranch) {
		t.Errorf("row %s reason %q does not name the decision", rowID, reason)
	}
}
