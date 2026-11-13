package tools

// The archive-ref reclaim arm's tests (epic archive-ref-reclaim).
//
// THE BUG THESE PIN: an archive tag is a decision, and every lane that honours
// it SKIPS the branch — none removes it. Measured on this host before the fix:
// refs/heads/feat/capture-self-window and refs/tags/archive/feat/capture-self-
// window resolved to the SAME commit e1701ac8, so the decision had been
// recorded and the decided ref was still there.
//
// Every test below is written to fail BEFORE the arm exists: with no
// ArchiveReclaimDecidedRefs in the tree the package does not compile, and once
// it exists the assertions are about the refs the arm actually moved (read back
// with git, never inferred from the report).

import (
	"strings"
	"testing"
)

// reclaimTagExists reports whether refs/tags/archive/<branch> resolves, and to
// which commit ("" when the tag is absent). Read with git, so a test can never
// mistake the arm's own report for the ref's state.
func reclaimTagExists(t *testing.T, repo, branch string) string {
	t.Helper()
	out, err := repoSweepGit(repo, "rev-parse", "--verify", "--quiet", archiveTagRefPrefix+branch)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// reclaimHeadTip is the same read for the branch ref ("" when it is gone).
func reclaimHeadTip(t *testing.T, repo, branch string) string {
	t.Helper()
	out, err := repoSweepGit(repo, "rev-parse", "--verify", "--quiet", refsHeadsPrefix+branch)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// TestArchiveReclaimRemovesTheDecidedHeadAndKeepsTheTag is THE red-to-green
// case: a branch with an archive tag at its own tip loses its HEAD REF and
// keeps its TAG, at the same commit.
func TestArchiveReclaimRemovesTheDecidedHeadAndKeepsTheTag(t *testing.T) {
	repo := rsTestRepo(t)
	const branch = "feat/capture-self-window"
	rsMakeBranch(t, repo, branch)
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)
	tip := reclaimHeadTip(t, repo, branch)
	if tip == "" {
		t.Fatal("fixture: the branch ref must exist before the arm runs")
	}
	if reclaimTagExists(t, repo, branch) != tip {
		t.Fatal("fixture: the tag must be a lightweight tag on the branch tip")
	}

	res := ArchiveReclaimDecidedRefs(repo)
	if res.Err != "" {
		t.Fatalf("reclaim scan failed: %s", res.Err)
	}
	if res.Deleted != 1 || len(res.Rows) != 1 {
		t.Fatalf("deleted = %d over %d row(s), want exactly 1: %+v", res.Deleted, len(res.Rows), res.Rows)
	}
	if got := res.Rows[0].Verdict; got != ArchiveReclaimDeleted {
		t.Fatalf("verdict = %q, want %q (%s)", got, ArchiveReclaimDeleted, res.Rows[0].Reason)
	}

	// ① THE HEAD REF IS GONE.
	if after := reclaimHeadTip(t, repo, branch); after != "" {
		t.Fatalf("%s%s still resolves to %s — the arm did not reclaim it", refsHeadsPrefix, branch, after)
	}
	// ② THE TAG IS STILL THERE, at the same commit. The decision is durable and
	// this arm is forbidden from touching it.
	if got := reclaimTagExists(t, repo, branch); got != tip {
		t.Fatalf("%s%s = %q, want %q — the tag is the durable half and must never move or vanish",
			archiveTagRefPrefix, branch, got, tip)
	}
	// ③ The ONE predicate agrees, so no caller has to re-derive two probes.
	if !DecidedRefReclaimed(repo, branch) {
		t.Fatalf("DecidedRefReclaimed(%s) = false after the reclaim", branch)
	}
	// ④ IDEMPOTENT: a second pass finds nothing to do and reports nothing.
	again := ArchiveReclaimDecidedRefs(repo)
	if again.Err != "" || again.Deleted != 0 || len(again.Rows) != 0 {
		t.Fatalf("second pass must be a no-op, got %+v", again)
	}
}

// TestArchiveReclaimKeepsTheRefWhenTagAndHeadDisagree is the epic's named red
// path: tag and head at DIFFERENT commits means the branch moved after the
// decision (or the tag was written elsewhere), so the ref is KEPT and the
// divergence REPORTED — never deleted (Rule 15).
func TestArchiveReclaimKeepsTheRefWhenTagAndHeadDisagree(t *testing.T) {
	repo := rsTestRepo(t)
	const branch = "feat/moved-after-decision"
	rsMakeBranch(t, repo, branch)
	// The decision is recorded at the CURRENT tip…
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)
	decided := reclaimHeadTip(t, repo, branch)
	// …and then the branch MOVES: new work the tag knows nothing about.
	rsGit(t, repo, true, "checkout", "-q", branch)
	rsGit(t, repo, true, "commit", "--allow-empty", "-q", "-m", "work after the decision")
	rsGit(t, repo, true, "checkout", "-q", masterBranchName)
	moved := reclaimHeadTip(t, repo, branch)
	if moved == decided {
		t.Fatal("fixture: the branch must have moved off the decided commit")
	}

	res := ArchiveReclaimDecidedRefs(repo)
	if res.Err != "" {
		t.Fatalf("reclaim scan failed: %s", res.Err)
	}
	if res.Deleted != 0 || res.Kept != 1 {
		t.Fatalf("deleted = %d kept = %d, want 0/1: %+v", res.Deleted, res.Kept, res.Rows)
	}
	row := res.Rows[0]
	if row.Verdict != ArchiveReclaimKept {
		t.Fatalf("verdict = %q, want %q", row.Verdict, ArchiveReclaimKept)
	}
	// The reason must NAME the two commits — an operator has to be able to see
	// which ref moved, not just that something did.
	if !strings.Contains(row.Reason, decided) || !strings.Contains(row.Reason, moved) {
		t.Fatalf("the divergence reason must name both commits (%s / %s), got %q", decided, moved, row.Reason)
	}
	// THE REF SURVIVES — this is the whole point of the guard.
	if got := reclaimHeadTip(t, repo, branch); got != moved {
		t.Fatalf("%s must be KEPT at %s, got %q", branch, moved, got)
	}
	if DecidedRefReclaimed(repo, branch) {
		t.Fatalf("a divergent branch must NOT read as reclaimed — the tag no longer describes its tip")
	}
}

// TestArchiveReclaimSkipsAWorktreeHeldBranch pins PROJECT.md Rule 29: a branch
// checked out in a registered worktree is somebody's live ground. Its tag does
// not authorise touching it, and the refusal names the tree.
func TestArchiveReclaimSkipsAWorktreeHeldBranch(t *testing.T) {
	repo := rsTestRepo(t)
	const branch = "feat/held-loser"
	rsMakeBranch(t, repo, branch)
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)
	held := t.TempDir()
	rsGit(t, repo, true, "worktree", "add", "-q", held, branch)
	tip := reclaimHeadTip(t, repo, branch)

	res := ArchiveReclaimDecidedRefs(repo)
	if res.Err != "" {
		t.Fatalf("reclaim scan failed: %s", res.Err)
	}
	if res.Deleted != 0 || res.Skipped != 1 {
		t.Fatalf("deleted = %d skipped = %d, want 0/1: %+v", res.Deleted, res.Skipped, res.Rows)
	}
	if got := reclaimHeadTip(t, repo, branch); got != tip {
		t.Fatalf("a worktree-held branch was touched: %s → %q (want %s)", branch, got, tip)
	}
	if !strings.Contains(res.Rows[0].Reason, held) {
		t.Fatalf("the refusal must name the holding worktree %s, got %q", held, res.Rows[0].Reason)
	}
}

// TestArchiveReclaimNeverTouchesTheIntegrationBranchOrATaskGround pins the
// structural guard: master/main are never candidates, tagged or not — they are
// what everything integrates INTO.
//
// THE `task/*` HALF OF THIS TEST WAS REPLACED (2026-10-12). It used to assert
// that a `task/<id>` branch is refused too, and that assertion ENCODED the
// defect: the arm refused it by NAME PREFIX before checking anything, so a
// ground with no ledger row and no worktree was held out of reclaim forever
// behind a sentence that was false about it. The replacement is
// TestArchiveReclaimTaskGroundRefusalIsAFactNotAName, which asserts the
// protection on the FACTS (a held worktree, a non-terminal ledger row) — and
// asserts that an unowned, unheld ground IS reclaimed, which is the red cell
// this test could never have caught. What is left here is the name test that
// survived: master/main.
func TestArchiveReclaimNeverTouchesTheIntegrationBranchOrATaskGround(t *testing.T) {
	repo := rsTestRepo(t)
	taskBranch := TaskGroundBranchPrefix + "01a0f44c2d0f7a82bde8f76445daa30d"
	rsGit(t, repo, true, "branch", mainBranchName)
	for _, b := range []string{masterBranchName, mainBranchName, taskBranch} {
		if b == taskBranch {
			rsMakeBranch(t, repo, b)
		}
		rsGit(t, repo, true, "tag", "-f", archiveBranchPrefix+b, b)
	}
	before := map[string]string{}
	for _, b := range []string{masterBranchName, mainBranchName} {
		before[b] = reclaimHeadTip(t, repo, b)
		if before[b] == "" {
			t.Fatalf("fixture: %s must exist", b)
		}
	}

	// The ledger is asked ONLY about the task ground, and answers "no row
	// tracks it" — the arm must still leave master/main alone.
	res := ArchiveReclaimDecidedRefsWith(repo, reclaimOwnerSeam())
	if res.Err != "" {
		t.Fatalf("reclaim scan failed: %s", res.Err)
	}
	if res.Deleted != 1 || res.Skipped != 2 {
		t.Fatalf("want the unheld, unowned task ground reclaimed and master/main skipped, got deleted=%d skipped=%d (%+v)",
			res.Deleted, res.Skipped, res.Rows)
	}
	for _, b := range []string{masterBranchName, mainBranchName} {
		if got := reclaimHeadTip(t, repo, b); got != before[b] {
			t.Fatalf("%s was moved: %s → %q", b, before[b], got)
		}
	}
	if got := reclaimHeadTip(t, repo, taskBranch); got != "" {
		t.Fatalf("an unowned, unheld task ground must be reclaimed, %s%s = %q", refsHeadsPrefix, taskBranch, got)
	}
}

// TestArchiveReclaimIgnoresAnUntaggedBranch is the "does nothing without a
// decision" case: a plain branch with no archive tag is somebody's work in
// flight and is invisible to this arm. It is the guard that keeps a reclaim arm
// from silently becoming a branch reaper.
func TestArchiveReclaimIgnoresAnUntaggedBranch(t *testing.T) {
	repo := rsTestRepo(t)
	rsMakeBranch(t, repo, "feat/in-flight")
	tip := reclaimHeadTip(t, repo, "feat/in-flight")

	res := ArchiveReclaimDecidedRefs(repo)
	if res.Err != "" || len(res.Rows) != 0 || res.Deleted != 0 {
		t.Fatalf("an untagged branch must not be a row at all, got %+v", res)
	}
	if got := reclaimHeadTip(t, repo, "feat/in-flight"); got != tip {
		t.Fatalf("an untagged branch was moved: %q", got)
	}
}

// TestFinishCleanupBranchExemptAsksAboutTheTagNotTheName is the §2.5 criterion
// repair: the exemption's archive half is the MARKER (refs/tags/archive/
// <branch>), not the branch-name prefix. Before the repair a branch NAMED
// feat/… and archived was NOT exempt, which is the shape every real archived
// branch has.
func TestFinishCleanupBranchExemptAsksAboutTheTagNotTheName(t *testing.T) {
	repo := rsTestRepo(t)
	const branch = "feat/decided-loser"
	rsMakeBranch(t, repo, branch)

	if finishCleanupBranchExempt(repo, branch) {
		t.Fatalf("%s has no archive tag and must NOT be exempt", branch)
	}
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch)
	if !finishCleanupBranchExempt(repo, branch) {
		t.Fatalf("%s carries the archive MARKER and must be exempt (the tag, not the name)", branch)
	}
	// The graveyard NAME stays an exemption on its own, with no repo to probe.
	if !finishCleanupBranchExempt("", "archive/old-chore") {
		t.Fatal("the archive/ graveyard namespace must stay exempt without a repo probe")
	}
	// master's twin in the tree: the shared predicate must still answer the
	// historical cases (this is what the pre-repair test asserted).
	if !finishCleanupBranchExempt(repo, masterBranchName) || finishCleanupBranchExempt(repo, "feat/real-deliverable") {
		t.Fatal("master must be exempt and a plain feature branch must not")
	}
}
