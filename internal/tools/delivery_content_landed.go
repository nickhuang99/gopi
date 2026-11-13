package tools

// delivery_content_landed.go — THE ONE CONTENT-FINGERPRINT CRITERION of
// "did this run's work land at the delivery point?".
//
// THE DEFECT THIS SEAM CLOSES, stated as it was measured. On one night four
// separate faces of this machinery answered that question wrongly, and every one
// of them was wrong for the same reason: the criterion each had built for itself
// was COMMIT IDENTITY (a sha, a title, a ref, a worktree file, a ledger status)
// while this repository lands work by cherry-pick / rebase — which REWRITES the
// sha and keeps the work. The four faces and their own private instruments:
//
//	deliveryProofMeasure (task_delivery_proof.go)  ancestry → cherry → content → record
//	mergeBacklogPatchSuperseded (merge-backlog)    its own `git cherry`, no content leg
//	worktree sweep undelivered                     its own subject+content legs
//	archive_landed                                 its own path-presence leg
//
// One of them (the close-out chain) got the criterion right; the others each got
// a weaker one, so each was wrong in its own way. This file is the ONE seam the
// readers now share, and it exists so the next reader has a shape to call
// instead of an example to copy (Rule 13/14: one definition, referenced).
//
// THE CRITERION, in the order it is asked — every leg answers about WORK, never
// about identity:
//
//	① ANCESTRY (the fast path). `merge-base --is-ancestor <tip> <destRef>`: the
//	  very commit is in the destination's history. Answers "landed" for the
//	  ordinary merge; it answers NOTHING about the rebased shape, which is why it
//	  is only the first leg.
//	② PATCH EQUIVALENCE, via `git cherry <destRef> <tip>`. `git cherry` IS a
//	  `git patch-id --stable` comparison (it computes the id of both sides and
//	  matches ids), so a commit REPLAYED by a rebase keeps its id and is reported
//	  `-` (present) even though its sha changed. This leg is what makes the
//	  criterion independent of commit identity, and it is the leg the red proof
//	  turns off (deliveryContentPatchIDOff).
//	③ LINE CONTENT, via deliveryProofContentLanded: every line the run ADDED is
//	  at the destination — now or in the bounded history behind its tip. It is
//	  the leg that survives a rebase which RE-DERIVED the content (a regenerated
//	  table measured against a moved base), where no patch id can ever match.
//
// WHAT IT DELIBERATELY DOES NOT READ — the four things measured to be wrong the
// same night: the commit HASH (a rebase changes it), the commit TITLE (a re-land
// changes it), a WORKTREE FILE (a checkout is not a delivery), and the ledger's
// `completed` status (a claim, not a measurement). None of them appears below.
//
// THREE-VALUED, and Unknown is NEVER collapsed into either verdict
// (memory #3342/#3339): `judged=false` means the question COULD NOT BE ASKED —
// an unreadable object, a range wider than the bounded walk, a git failure — and
// every caller must keep it apart from `landed=false`. A guard that refuses what
// it cannot see wedges the ledger; a guard that passes what it cannot see hides
// the defect.

import (
	"context"
	"fmt"
	"strings"
)

// The seam's git vocabulary, each spelled once (Rules 1/13).
const (
	// deliveryContentMergeBaseVerb is git's `merge-base`, asked with the
	// `--is-ancestor` flag below for leg ①.
	deliveryContentMergeBaseVerb = "merge-base"
	// deliveryContentIsAncestorFlag turns merge-base into a yes/no exit status:
	// exit 0 = an ancestor, exit 1 = not, anything else = git could not answer.
	deliveryContentIsAncestorFlag = "--is-ancestor"
	// deliveryContentCherryVerb is git's `cherry` — leg ②'s patch-id comparison.
	deliveryContentCherryVerb = "cherry"
)

// deliveryContentPatchIDOff is the red proof's switch for LEG ②: the A/B that
// proves the patch-id leg is doing the work rather than the line-content leg
// quietly covering for it. It is a package variable, not a parameter, for the
// same reason deliveryProofContentArmOff is one: a production caller must not be
// able to choose a weaker criterion, and a test must be able to remove ONE leg
// and watch the same fixture flip.
var deliveryContentPatchIDOff bool

// deliveryContentPatchIDEnabled reports whether leg ② may run. Its single
// falsifier is the red proof's own switch above.
func deliveryContentPatchIDEnabled() bool {
	return !deliveryContentPatchIDOff
}

// DeliveryContentLanded is THE seam: does the WORK of `tip` live at `destRef`?
//
//	tipDir   the directory that HOLDS tip's objects (the run's ground, or the
//	         repo a caller is standing in)
//	destDir  the directory that HOLDS destRef and its tree (may be the same one;
//	         the ground and the recorded repo need not share an object store, and
//	         this seam NEVER fetches, pushes or moves a ref)
//	destRef  the delivery point, as a git ref (`refs/remotes/origin/master`,
//	         `master`, an archive tag — whatever the caller's destination is)
//	tip      the commit under judgement — a sha or any ref that resolves to one
//
// `tip` may be a RANGE tip: the patch leg answers for every commit reachable
// from tip and from nothing in destRef, which is the same question
// `git cherry <destRef> <tip>` asks — so a caller judging a whole branch and a
// caller judging one pushed commit call this one function.
func DeliveryContentLanded(ctx context.Context, tipDir, destDir, destRef, tip string) (landed bool, why string, judged bool) {
	if strings.TrimSpace(destRef) == "" || strings.TrimSpace(tip) == "" {
		return false, "", false
	}
	// Leg ① + ② answer from tipDir alone.
	ok, why, answered := deliveryContentAncestryOrPatch(ctx, tipDir, destRef, tip)
	if ok {
		return true, why, true
	}
	// Neither leg could even be asked, and there is no directory to run the
	// content leg in: the question is unmeasured, never "not landed".
	if !answered && strings.TrimSpace(destDir) == "" {
		return false, "", false
	}
	// Leg ③ needs both sides readable.
	if strings.TrimSpace(destDir) == "" {
		return false, "", false
	}
	// The two ref arguments are both destRef ON PURPOSE. `rangeRef` is the base
	// the added-line RANGE is taken from, and for a caller asking "is this work
	// at destRef" the range that matters is exactly "the commits destRef does
	// not hold" — i.e. destRef itself. A caller with a DIFFERENT base (the
	// close-out chain passes the row's own branch ref there) keeps calling
	// deliveryProofContentLanded directly: that is a narrower question than this
	// seam answers, and giving it a parameter would let a caller widen the range
	// until the answer turned green.
	return deliveryProofContentLandedWith(ctx, tipDir, destDir, destRef, destRef, tip, false)
}

// deliveryContentAncestryOrPatch is legs ① and ② — the two legs that need only
// the directory holding the tip. It is what deliveryProofTipContained was,
// extracted so the seam and that (pre-existing, test-pinned) reader cannot drift:
// there is ONE spelling of "is the patch already upstream" in this package.
//
// answered=false means neither leg could be taken (`git cherry` failed, or the
// objects are unreadable) — the caller must not read it as "not landed".
func deliveryContentAncestryOrPatch(ctx context.Context, at, destRef, tip string) (landed bool, why string, answered bool) {
	if _, err := branchDeleteGit(ctx, at, deliveryContentMergeBaseVerb, deliveryContentIsAncestorFlag, tip, destRef); err == nil {
		return true, fmt.Sprintf("%s is an ancestor of %s", branchDeleteShortHash(tip), destRef), true
	}
	if !deliveryContentPatchIDEnabled() {
		return false, "", false
	}
	out := deliveryProofGitRead(ctx, at, deliveryContentCherryVerb, destRef, tip)
	if out == "" {
		return false, "", false
	}
	absent, present := 0, 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, deliveryProofCherryAbsentMark):
			absent++
		case strings.HasPrefix(line, deliveryProofCherryPresentMark):
			present++
		}
	}
	if absent == 0 && present > 0 {
		return true, fmt.Sprintf("patch-equivalent: every patch of %s is already in %s (git cherry reports %s, no absent patch)",
			branchDeleteShortHash(tip), destRef, plural(present)), true
	}
	// Legs ① and ② both ran and both answered NO: the patch set is genuinely
	// short of the destination, and only the content leg can still rescue it.
	return false, "", true
}
