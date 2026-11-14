package tools

// archive_ref_reclaim_audit_test.go — the per-PATH RED/GREEN proof for the
// archive-reclaim arm's DECIDED-EXCLUSION deletion.
//
// WHY THIS FILE EXISTS AT ALL (measured 2026-10-12): the arm at
// archive_ref_reclaim.go deletes a branch ref with `git update-ref -d` instead of
// `git branch -d/-D` (deliberately — see that file's header: git's merged-only
// check is the wrong question for a branch the merge path will never take). The
// structural census that owes every removal a `removal-audit.log` record
// classified sites by the porcelain verb pair alone, so this arm's deletions were
// the one class of local ref removal in the tree that left NO line behind. The
// observation that produced the investigation: 11 refs/heads (16 → 5) on
// /home/user/work/GoPi disappeared at 09:45–09:46 CST with no `kind=branch-delete`
// record for any of them, and running this arm for real against a fixture repo
// removed its ref and wrote nothing.
//
// THE ASSERTION IS ON THE REAL PATH: ArchiveReclaimDecidedRefs is driven against
// a real git repository, the ref's absence is read back with git (never inferred
// from the arm's own report), and the record is read out of the sink the emitter
// actually wrote to. The RED half disables the emit and requires the SAME
// assertion to fail — a proof that cannot go red has stopped proving anything.

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/removalaudit"
)

// TestArchiveReclaimBranchDeleteIsAudited is the resident defect's own
// regression guard: the decided-exclusion reclaim arm must leave a
// `kind=branch-delete` record naming ITS OWN arm, the ref it removed and the tip
// that ref pointed at, BEFORE the ref is gone.
func TestArchiveReclaimBranchDeleteIsAudited(t *testing.T) {
	repo := rsTestRepo(t)
	const branch = "feat/decided-and-audited"
	rsMakeBranch(t, repo, branch)
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)
	tip := reclaimHeadTip(t, repo, branch)
	if tip == "" {
		t.Fatal("fixture: the branch ref must exist before the arm runs")
	}

	sink := auditSink(t)
	res := ArchiveReclaimDecidedRefs(repo)
	if res.Err != "" {
		t.Fatalf("the reclaim scan failed: %s", res.Err)
	}
	if res.Deleted != 1 {
		t.Fatalf("deleted=%d, want 1 — the fixture must exercise the deletion path (rows: %+v)", res.Deleted, res.Rows)
	}
	if got := reclaimHeadTip(t, repo, branch); got != "" {
		t.Fatalf("%s%s still resolves to %s — the ref must be gone for this test to prove anything about its record",
			refsHeadsPrefix, branch, got)
	}
	if got := reclaimTagExists(t, repo, branch); got != tip {
		t.Fatalf("the archive tag must survive the reclaim: got %q, want %q", got, tip)
	}

	if why := auditProbe(t, sink, removalaudit.ArmArchiveReclaimBranch, branch); why != "" {
		t.Fatalf("%s — the arm removed %s%s with no removal-audit record naming it", why, refsHeadsPrefix, branch)
	}
	// The record must carry the DELETED TIP: "which commit left, and did it
	// reach anywhere else?" is the question the record exists to answer, and
	// only the caller can capture the tip (a moment later the ref is gone).
	recs, err := removalaudit.Records(sink)
	if err != nil {
		t.Fatalf("read the audit sink: %v", err)
	}
	var found bool
	for _, r := range recs {
		if r.Arm != string(removalaudit.ArmArchiveReclaimBranch) || r.Target != branch {
			continue
		}
		found = true
		if r.Sha != tip {
			t.Errorf("the record's sha = %q, want the deleted ref's tip %q (a record that cannot name the "+
				"commit is not an accounting of the deletion)", r.Sha, tip)
		}
	}
	if !found {
		t.Fatalf("no record for arm %s target %s among %d record(s)", removalaudit.ArmArchiveReclaimBranch, branch, len(recs))
	}

	// RED: with the emit switched off, the SAME assertion must fail. Without
	// this half the test would pass on a tree where the arm never emitted —
	// which is exactly the state this file was written against. The RED half
	// takes a FRESH sink: the GREEN half's record is still on disk in the same
	// file, so probing the shared sink would read a line the disabled emit never
	// wrote (measured: the assertion passed, and proved nothing).
	auditEmitOff(t)
	redSink := auditSink(t)
	repo2 := rsTestRepo(t)
	rsMakeBranch(t, repo2, branch)
	rsGit(t, repo2, true, "tag", archiveBranchPrefix+branch, branch)
	res2 := ArchiveReclaimDecidedRefs(repo2)
	if res2.Deleted != 1 {
		t.Fatalf("RED half: deleted=%d, want 1", res2.Deleted)
	}
	if got := reclaimHeadTip(t, repo2, branch); got != "" {
		t.Fatalf("RED half: %s%s still resolves to %s", refsHeadsPrefix, branch, got)
	}
	if why := auditProbe(t, redSink, removalaudit.ArmArchiveReclaimBranch, branch); why == "" {
		t.Fatal("RED half: the assertion still passes with the emit disabled — it is not measuring the record")
	}
}
