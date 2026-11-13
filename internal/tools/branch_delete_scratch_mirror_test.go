package tools

// branch_delete_scratch_mirror_test.go — THE RED PROOF for case ④ (epic
// 01a12af7): a tip whose only remaining holder is the pushback lane's per-pass
// mirror is reported as such, with the command that makes it durable — instead
// of being reported (or silently assumed) as "kept by another ref".
//
// THE MEASURED SHAPE. The pushback lane writes refs/remotes/gitPushcheck/master
// with a `+` refspec fetch and DELETES it in the same pass (gitPushbackPruneMirrors).
// Measured on this host: `git for-each-ref refs/remotes/gitPushcheck/` answers
// exactly one ref — master — and a branch was deleted with that ref as its only
// other container. The commit was one fetch from unreachable.
//
// WHAT THESE TESTS PIN.
//
//	① branchDeleteScratchOnlyHolders returns the mirror ref when it is the ONLY
//	   container, and nil when a real ref (a branch, a tag) also holds the tip;
//	② it returns nil when NOTHING holds the tip — that case belongs to the
//	   orphan guard, which already refuses and names it, so this arm must not
//	   double-report it;
//	③ the note it feeds carries the marker, the ref name AND the recovery
//	   command, and the command names the ref it was handed (a hint that cannot
//	   be typed is not a recovery);
//	④ THE GUARD'S DECISION ON A SCRATCH-ONLY HOLDER (amended 2026-10-12, epic
//	   01a12f48). This test used to pin the opposite — that the guard still
//	   allowed a delete whose only other container was the mirror — and that
//	   claim is now MEASURED FALSE as a safety property: a branch removed in
//	   exactly this shape left its tip reachable from no ref, one `git gc` from
//	   gone (epic 01a12f48's own reproduction). The scan's ref SET is unchanged
//	   and the mirror is still not deleted here; what changed is the DECISION a
//	   scratch-only holder buys — none. A durable holder (a tag, a branch,
//	   another remote's tracking ref) still silences the guard exactly as
//	   before, and that control lives beside this test
//	   (archive_ref_holds_tip_test.go, TestOrphanGuardStillAllowsADurableHolder).

import (
	"context"
	"strings"
	"testing"
)

// scratchMirrorFixture builds a repo in the measured shape: a branch whose tip
// no durable ref holds, plus the pushback lane's mirror ref pointing at it.
// Returns the repo, the branch and the tip.
func scratchMirrorFixture(t *testing.T) (repo, branch, tip string) {
	t.Helper()
	repo = branchGuardFixtureRepo(t)
	branch = "fix/mirror-only"
	gitRun(t, repo, "checkout", "-b", branch, "master")
	branchGuardCommit(t, repo, "mirror-only.txt", "work held by a scratch ref")
	tip = strings.TrimSpace(rsGit(t, repo, false, "rev-parse", "refs/heads/"+branch))
	gitRun(t, repo, "checkout", "master")
	// The lane's own mirror: `git update-ref` writes exactly what the `+`
	// refspec fetch leaves behind, without a peer (Rule 27: no peer, no load).
	gitRun(t, repo, "update-ref", gitPushbackCheckRef, tip)
	return repo, branch, tip
}

// TestScratchOnlyHoldersNamesTheLanesMirror is claim ①, both poles.
func TestScratchOnlyHoldersNamesTheLanesMirror(t *testing.T) {
	repo, branch, tip := scratchMirrorFixture(t)
	ctx := context.Background()

	got := branchDeleteScratchOnlyHolders(ctx, repo, tip, branch)
	if len(got) != 1 || got[0] != gitPushbackCheckRef {
		t.Fatalf("the lane's mirror was not named as the only holder: %v", got)
	}

	// POSITIVE CONTROL — a durable holder appears: the same tip is now held by
	// a real branch, and the arm must say NOTHING (the ordinary, unremarkable
	// shape this epic must not turn into noise).
	gitRun(t, repo, "branch", "keep/"+branch, tip)
	if got := branchDeleteScratchOnlyHolders(ctx, repo, tip, branch); got != nil {
		t.Fatalf("a durable holder did not silence the arm: %v", got)
	}
}

// TestScratchOnlyHoldersIsSilentWhenNothingHoldsTheTip is claim ②: the
// no-container case is the ORPHAN GUARD's, and double-reporting it would make
// this arm's sentence wrong (it says "held only by a mirror" — there is no
// mirror there).
func TestScratchOnlyHoldersIsSilentWhenNothingHoldsTheTip(t *testing.T) {
	repo := branchGuardFixtureRepo(t)
	gitRun(t, repo, "checkout", "-b", "fix/nothing", "master")
	branchGuardCommit(t, repo, "nothing.txt", "no other ref")
	tip := strings.TrimSpace(rsGit(t, repo, false, "rev-parse", "refs/heads/fix/nothing"))
	gitRun(t, repo, "checkout", "master")

	if got := branchDeleteScratchOnlyHolders(context.Background(), repo, tip, "fix/nothing"); got != nil {
		t.Fatalf("an unheld tip was reported by the scratch arm: %v", got)
	}
}

// TestScratchOnlyNoteCarriesTheRecoveryCommand is claim ③ — the sentence has to
// be ACTIONABLE, which is the difference between "we told you" and "you can do
// something about it" (this epic's whole point).
func TestScratchOnlyNoteCarriesTheRecoveryCommand(t *testing.T) {
	mirror := gitPushbackCheckRef
	note := branchDeleteMirrorOnlyNote("fix/vanished", []string{mirror})
	if note == "" {
		t.Fatal("the note is empty — a mirror-only holder went unreported")
	}
	for _, want := range []string{branchDeleteMirrorOnlyMarker, mirror, "git branch", "fix/vanished-recovered"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not carry %q: %s", want, note)
		}
	}
	// No holders → no sentence. The caller renders this string on a SUCCESS
	// line, so an empty holder set must add nothing at all.
	if got := branchDeleteMirrorOnlyNote("fix/vanished", nil); got != "" {
		t.Errorf("a note with no holders was rendered: %q", got)
	}
}

// TestBranchDeleteGuardRefusesAScratchOnlyHolder is claim ④ (amended 2026-10-12,
// epic 01a12f48) — the guard's DECISION on a scratch-only holder, asserted on the
// GUARD ITSELF. The scan is the same one it always was, and a durable holder still
// silences it; what changed is that the mirror's presence buys no permission: a
// tip whose only company is a ref its own writer deletes gets the no-other-ref
// refusal, naming the holder and the recovery command.
func TestBranchDeleteGuardRefusesAScratchOnlyHolder(t *testing.T) {
	repo, branch, tip := scratchMirrorFixture(t)
	ctx := context.Background()

	why := branchDeleteOrphanRefusal(repo, branch)
	if why == "" {
		t.Fatalf("the guard allowed a delete whose only other container is %s (a ref its own lane deletes when the pass ends)", gitPushbackCheckRef)
	}
	if !strings.Contains(why, branchDeleteNoOtherRefMarker) || !strings.Contains(why, branchDeleteMirrorOnlyMarker) {
		t.Fatalf("the scratch-only refusal must carry BOTH facts — no durable container, and which ref holds it: %s", why)
	}

	// The report line keeps its own honesty half: the tip captured before the
	// delete is still reported as held only by scratch, with the recovery
	// command (the reading is taken from the tip captured BEFORE the delete —
	// the only moment it can be).
	note := finishCleanupBranchScratchOnlyNote(ctx, repo, tip, branch)
	if !strings.Contains(note, branchDeleteMirrorOnlyMarker) {
		t.Fatalf("the close-out report line did not carry the scratch-only note: %q", note)
	}
	// The mirror is NOT touched by the guard: deciding is not deleting, and the
	// lane that owns that ref prunes it on its own schedule.
	if out := strings.TrimSpace(rsGit(t, repo, false, "rev-parse", "--verify", "--quiet", gitPushbackCheckRef)); out != tip {
		t.Fatalf("the guard mutated the mirror ref (%q, want %q)", out, tip)
	}
	// AFTER the delete the tip is gone from refs/heads: the same question asked
	// afterwards answers from a different ref set — which is why the tip is
	// captured first (this assertion is the reason the parameter exists).
	gitRun(t, repo, "branch", "-D", branch)
	if got := branchDeleteTipOf(ctx, repo, branch); got != "" {
		t.Fatalf("a deleted branch still resolved to %q", got)
	}
}
