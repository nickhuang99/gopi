package tools

// zz_redproof_undelivered_dup_test.go — RED PROOF for the duplicated delivery
// point in the keep sentence.
//
// THE DEFECT (measured on this host 2026-10-27): `worktreeUndeliveredDeliveryPoints`
// builds three candidate refs, of which ① and ② both spell
// `refs/remotes/origin/master` once `branch == master` — the shape EVERY clone
// ground has, because a clone ground's deliverable.branch IS the integration
// branch. The self-exclusion at the bottom of that function drops the candidate
// equal to `refs/heads/<branch>`, but nothing dropped the duplicate, so the keep
// sentence named ONE ref TWICE. Measured on the live sweep, all 76 kept grounds
// printed:
//
//	kept: the ground's branch master holds 2 commits that no delivery point has
//	      (refs/remotes/origin/master, refs/remotes/origin/master) — this
//	      directory is that work's only home: push the branch or merge it into
//	      master, then the ground is collectable
//
// That sentence is the operator's whole instruction, and "two refs hold nothing"
// when there is one ref is a false statement about their repository (Rule 15).
//
// WHY IT MATTERS BEYOND PROSE: the same collapse is one keystroke from being a
// CORRECTNESS defect, not a cosmetic one — a dedup done by POSITION (drop the
// 2nd) instead of by VALUE would delete the integration branch's remote point
// for a branch that is NOT master, i.e. it would stop counting the merger's own
// delivery and keep every landed ground forever. This test therefore pins the
// joined value, not the count of arguments.

import (
	"strings"
	"testing"
)

// TestUndeliveredNamesEachDeliveryPointOnce drives the rung on the shape that
// reproduces the collapse — a clone ground standing on master with an unpushed
// commit — and asserts the sentence names exactly ONE delivery point, once.
func TestUndeliveredNamesEachDeliveryPointOnce(t *testing.T) {
	f := newUndeliveredFixture(t)
	// PRECONDITION ①: the scope discriminator. Only a CLONE ground's branch is
	// the integration branch; a linked worktree's is its own branch and the
	// collapse does not happen there at all.
	if !deliveryProofIsCloneGround(f.ground) {
		t.Fatalf("fixture: %s must be a clone ground (.git a DIRECTORY) for this case to mean anything", f.ground)
	}
	// PRECONDITION ②: a fixture that starts delivered keeps every later refusal
	// attributable to the one commit added here and nothing else.
	if p := worktreeUndeliveredCommits(f.ground, masterBranchName); !p.Measured || p.Ahead != 0 {
		t.Fatalf("fixture: a fresh clone's master must be 0 ahead (measured=%v ahead=%d why=%q)",
			p.Measured, p.Ahead, p.Why)
	}

	// The worker's own shape: commit ON master in the ground (never pushed).
	undeliveredEquivCommit(t, f.ground, "master-work.txt",
		"work that only this ground has\n",
		"fix(undelivered): the work this ground alone holds")

	why := worktreeSweepUndeliveredRefusal(f.ground, f.ground, masterBranchName)
	if why == "" {
		t.Fatal("a clone ground standing on master with an unpushed commit must be KEPT — " +
			"removing this directory is the only home of that work")
	}

	open := strings.Index(why, "(")
	closing := strings.Index(why, ")")
	if open < 0 || closing < open {
		t.Fatalf("the keep sentence must name its delivery points in parentheses; got %q", why)
	}
	points := strings.Split(why[open+1:closing], ", ")
	seen := make(map[string]bool, len(points))
	for _, p := range points {
		if seen[p] {
			t.Fatalf("the delivery point %q is named TWICE in the keep sentence %q — the sentence is "+
				"the operator's whole instruction, and reading two refs where there is one is a false "+
				"statement about their repository (Rule 15)", p, why)
		}
		seen[p] = true
	}
	// THE VALUE, not just the count: a dedup by position would pass the loop
	// above while naming the wrong ref, so the surviving point is asserted.
	if len(points) != 1 || points[0] != refsRemotesPrefix+deliveryProofOriginRemote+"/"+IntegrationBranchName {
		t.Fatalf("a clone ground's delivery points must collapse to exactly one "+
			"(refs/remotes/origin/master); got %v", points)
	}

	// AND THE SENTENCE STILL CARRIES THE COUNT AND THE ACTION, so the dedup did
	// not trade one broken instruction for a shorter broken one.
	for _, want := range []string{"1 commit", "push the branch or merge it into " + IntegrationBranchName} {
		if !strings.Contains(why, want) {
			t.Fatalf("the keep sentence must still name %q; got %q", want, why)
		}
	}
}

// TestUndeliveredDeliveryPointsDedupesByValueNotPosition keeps the rung honest
// for the branch that is NOT the integration branch — the case a positional
// dedup would break silently.
//
// A linked worktree on `feat/x` has THREE distinct resolvable points
// (origin/feat/x, origin/master, refs/heads/master), and all three must survive:
// dropping "the second one" would stop counting the merger's delivery, and a
// merged-but-not-pushed branch would then be kept for ever.
func TestUndeliveredDeliveryPointsDedupesByValueNotPosition(t *testing.T) {
	const branch = "feat/not-the-integration-branch"
	points := worktreeUndeliveredDeliveryPoints(branch)
	want := []string{
		refsRemotesPrefix + deliveryProofOriginRemote + "/" + branch,
		refsRemotesPrefix + deliveryProofOriginRemote + "/" + IntegrationBranchName,
		refsHeadsPrefix + IntegrationBranchName,
	}
	if len(points) != len(want) {
		t.Fatalf("a branch that is NOT the integration branch keeps all %d points; got %d (%v)",
			len(want), len(points), points)
	}
	for i, w := range want {
		if points[i] != w {
			t.Fatalf("point %d = %q, want %q (the list's ORDER is what the keep sentence prints, "+
				"so it must not be permuted by the dedup); got %v", i, points[i], w, points)
		}
	}
}

// TestUndeliveredDeliveryPointsNeverNameTheBranchItself pins the older guard this
// change sits beside, so a future refactor of the dedup loop cannot drop it: the
// branch under measurement is never one of its own delivery points (a
// self-referential count answers 0 by construction, which is the deletion this
// whole rung exists to prevent).
func TestUndeliveredDeliveryPointsNeverNameTheBranchItself(t *testing.T) {
	self := refsHeadsPrefix + IntegrationBranchName
	for _, p := range worktreeUndeliveredDeliveryPoints(IntegrationBranchName) {
		if p == self {
			t.Fatalf("the branch under measurement (%q) must never be its own delivery point", self)
		}
	}
}
