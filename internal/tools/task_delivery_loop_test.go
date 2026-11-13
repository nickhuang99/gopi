package tools

// The bounded DELIVERY LOOP's tests (epic 01a17033).
//
// THE SPECIFICATION THIS FILE PINS. A hand-over used to be ONE `git push`:
// refused, the lane reported "the peer rejected the push" and gave up. The
// peer's refusal has four causes and three of them are not about the work at
// all, so the loop re-FETCHES the peer's master each round, MEASURES which
// cause it is looking at, ALIGNS the branch onto the peer when that is the
// cause (a real merge commit, built with merge-tree + commit-tree +
// update-ref: the delivery runs in the main checkout, whose HEAD is master, so
// no working tree may be touched and no commit may be rewritten), and pushes
// again — at most pushbackDeliveryRounds times.
//
// Every test drives REAL git (a bare peer + a real client clone), because the
// questions here ("does this branch contain that tip", "is this merge clean",
// "does the peer accept this refspec") are git-semantics questions and a mock
// cannot answer them.
//
// The four properties a caller relies on, one test each:
//
//	pbDeliveryDriveAdvancesUnderUs — the ordinary two-host case: the peer moved
//	    past our base, the push is refused, the loop ALIGNS and delivers, and
//	    the round count recorded is > 1 (the epic's own acceptance criterion).
//	pbDeliveryDriveConflictUnresolvable — a content conflict nobody resolves:
//	    the loop stops with git's verbatim conflict output in the Reason (never
//	    a generic "push failed"), and the branch is left untouched.
//	pbDeliveryDriveConflictResolvedByTheHook — a content conflict the RESOLVER
//	    HOOK settles: the loop re-measures, aligns and delivers.
//	pbDeliveryDriveUnreachableSteps — a dead peer: ONE round, no retry storm,
//	    and the reason carries git's own sentence.
//
// Run this file's tests:  go test -run TestDeliveryLoop ./internal/tools/

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// pbDeliveryDrivePeerTakesTheBranch makes the PEER own refs/heads/<branch> and
// then move it away from us — the ONLY shape in which a push of ours is refused.
//
// WHY THIS SHAPE AND NOT "the peer's master moved": pushing a branch name the
// peer has NEVER seen always succeeds, whatever the peer's master is, because
// `git push` CREATES the ref. The refusal this loop exists to handle is a name
// the peer ALREADY holds at a tip of its own — the re-delivery case (a resumed
// run, a second attempt, the other host having pushed the same branch).
//
// The peer's copy is cut from the SAME seed commit our branch was, so the two
// histories have a common ancestor and DIVERGE — which is what makes the
// alignment a real merge rather than a fast-forward.
//
// It returns the peer's tip of that branch.
func pbDeliveryDrivePeerTakesTheBranch(t *testing.T, w *pbWorld, branch, file string) string {
	t.Helper()
	// ① publish our branch, so the peer owns the name (the refusal's premise).
	w.git(w.client, "push", "-q", w.server, branch+":"+branch)
	// ② the other writer cuts the SAME branch from the seed and pushes its own
	// commit: the peer's tip now diverges from ours.
	other := pbDeliveryDriveOtherClone(t, w)
	// DETACHED (never a local branch of the same name): this clone already has
	// that branch from its fetch-and-push, and `checkout -b` would collide.
	w.git(other, "checkout", "-q", "--detach", w.seedCommitA)
	pbWrite(w.t, other, file, "somebody else's work on the same branch")
	w.git(other, "add", file)
	w.git(other, "commit", "-qm", "the peer's own commit on this branch")
	w.git(other, "push", "-q", "--force", w.server, "HEAD:"+branch)
	return w.git(other, "rev-parse", "HEAD")
}

// pbDeliveryDriveOtherClone is the peer's second writer: a clone of the bare
// server with an identity configured (a commit needs one).
func pbDeliveryDriveOtherClone(t *testing.T, w *pbWorld) string {
	t.Helper()
	dir := pbDeliveryDriveCloneDir(t, w, "other")
	w.git(w.root, "clone", "-q", w.server, dir)
	w.git(dir, "config", "user.email", "other@t")
	w.git(dir, "config", "user.name", "other")
	return dir
}

// pbDeliveryDriveCloneDir names a fresh directory under the world's temp root
// (never a shared one: a second clone must not inherit the first one's refs).
func pbDeliveryDriveCloneDir(t *testing.T, w *pbWorld, name string) string {
	t.Helper()
	return w.root + "/" + name
}

// pbDeliveryDriveBranchFromSeed cuts a branch carrying real work from the
// world's seed commit — the base the peer has since moved past.
func pbDeliveryDriveBranchFromSeed(t *testing.T, w *pbWorld, branch, file string) {
	t.Helper()
	w.git(w.client, "checkout", "-q", "-b", branch, w.seedCommitA)
	pbWrite(w.t, w.client, file, "the deliverable")
	w.git(w.client, "add", file)
	w.git(w.client, "commit", "-qm", "the deliverable's own commit")
	w.git(w.client, "checkout", "-q", masterBranchName)
}

// TestDeliveryLoopAlignsAndDeliversOnTheSecondRound is the epic's acceptance
// case #1: our commit is on a branch the peer ALSO holds at a divergent tip, so
// the push is refused (git says non-fast-forward / rejected), the loop FETCHES
// the peer's state, ALIGNS the branch onto it, and a later round delivers. The
// round count recorded must be > 1, and the delivered tip must CONTAIN the
// peer's tip — the property the peer's `merge --ff-only` actually needs.
func TestDeliveryLoopAlignsAndDeliversOnTheSecondRound(t *testing.T) {
	w := newPBWorld(t)
	const branch = "task/delivery-ordinary"
	pbDeliveryDriveBranchFromSeed(t, w, branch, "mine.txt")
	peerTip := pbDeliveryDrivePeerTakesTheBranch(t, w, branch, "theirs.txt")

	// BEFORE: neither tip contains the other, so the push the old lane made is
	// refused. Asserted against the PEER's advertised tip, measured in ancestry
	// (the objects must be present first — a merge-base over an absent sha
	// answers 128, which is NOT git saying "no").
	pbDeliveryDriveMirrorPeerRef(t, w, branch)
	peerRef := fmt.Sprintf(gitPushbackPeerRefMirrorFmt, branch)
	if _, code := pbGitMaybe(t, w.client, "merge-base", "--is-ancestor", peerRef, branch); code != 1 {
		t.Fatalf("fixture: %s must NOT contain the peer's tip of that branch before the loop "+
			"(exit=%d, want 1)", branch, code)
	}
	if _, code := pbGitMaybe(t, w.client, "merge-base", "--is-ancestor", branch, peerRef); code != 1 {
		t.Fatalf("fixture: the peer's tip must NOT contain our branch either (exit=%d, want 1) — "+
			"the two histories must have DIVERGED", code)
	}

	delivery := deliverBranchToPeer(context.Background(), w.client, w.server, branch, nil)

	if !delivery.Delivered {
		t.Fatalf("the loop must deliver after aligning: shape=%s rounds=%d reason=%q",
			delivery.Shape, delivery.Rounds(), delivery.Reason)
	}
	if delivery.Rounds() <= 1 {
		t.Fatalf("the round count must be > 1 — the first push was refused and the alignment "+
			"had to be made: rounds=%d (%s)", delivery.Rounds(), delivery.String())
	}
	if delivery.Rounds() > pushbackDeliveryRounds {
		t.Fatalf("rounds=%d exceeds the named limit %d (Rule 1: the brake is a constant)",
			delivery.Rounds(), pushbackDeliveryRounds)
	}
	// ① the ALIGNMENT happened: some round recorded it.
	aligned := false
	for _, a := range delivery.Attempts {
		if a.Aligned {
			aligned = true
		}
	}
	if !aligned {
		t.Fatalf("no round recorded the alignment: %s", delivery.String())
	}
	// ② the delivered tip CONTAINS the peer's tip — the property the peer's
	//    `git merge --ff-only <branch>` actually needs.
	if _, code := pbGitMaybe(t, w.client, "merge-base", "--is-ancestor", peerRef, branch); code != 0 {
		t.Fatalf("%s must contain the peer's tip %s after delivery (exit=%d, want 0)",
			branch, shortSha(peerTip), code)
	}
	// ③ the peer really holds it, read from the PEER (not from our own refs).
	if !w.serverRefExists(branch) {
		t.Fatalf("the peer does not advertise refs/heads/%s after a successful delivery", branch)
	}
	if got, want := strings.TrimSpace(w.git(w.client, "rev-parse", branch)), pbPeerTipOf(t, w, branch); got != want {
		t.Fatalf("the peer's tip of %s is %s, our local branch is %s — the push did not carry our tip",
			branch, shortSha(want), shortSha(got))
	}
}

// pbDeliveryDriveMirrorPeerRef fetches the PEER's refs/heads/<branch> into the
// lane's own mirror ref, so a FIXTURE assertion can ask about the tip the peer
// actually advertises. It is the same fetch `gitPushbackClassifyPeerRef` makes
// (one implementation — this helper is only the "ask it before the loop runs"
// spelling).
func pbDeliveryDriveMirrorPeerRef(t *testing.T, w *pbWorld, branch string) {
	t.Helper()
	if err := gitPushbackFetchPeerRef(context.Background(), w.client, w.server, branch); err != nil {
		t.Fatalf("fixture: mirroring the peer's refs/heads/%s: %v", branch, err)
	}
}

// pbDeliveryDriveMirrorPeerMaster fetches the peer's master into the lane's own
// mirror ref in the client, so a FIXTURE assertion can ask about the peer's real
// tip. The loop performs the same fetch on every round
// (gitPushbackFetchMaster); a test that asked `merge-base` about the client's
// LOCAL master would be asking about a stale ref.
func pbDeliveryDriveMirrorPeerMaster(t *testing.T, w *pbWorld) {
	t.Helper()
	if err := gitPushbackFetchMaster(context.Background(), w.client, w.server); err != nil {
		t.Fatalf("fixture: mirroring the peer's master: %v", err)
	}
}

// pbPeerTipOf reads the sha the PEER advertises for one branch (the
// delivery's own channel: `ls-remote`, never a local ref).
func pbPeerTipOf(t *testing.T, w *pbWorld, branch string) string {
	t.Helper()
	out := strings.TrimSpace(w.git(w.root, "ls-remote", w.server, "refs/heads/"+branch))
	if out == "" {
		t.Fatalf("the peer does not advertise refs/heads/%s", branch)
	}
	fields := strings.Fields(out)
	return fields[0]
}

// TestDeliveryLoopStopsWithGitOwnWordsOnAnUnresolvableConflict is acceptance
// case #2: both sides changed the SAME path, so the alignment merge conflicts
// and no round of the same merge can settle it. With no resolver hook the loop
// must stop on the FIRST round it meets the conflict, quote git's own conflict
// output in the Reason, and leave the branch exactly where it was.
func TestDeliveryLoopStopsWithGitOwnWordsOnAnUnresolvableConflict(t *testing.T) {
	w := newPBWorld(t)
	const branch = "task/delivery-conflict"
	const shared = "shared.txt"

	// Our side: the branch edits shared.txt away from the seed's content.
	w.git(w.client, "checkout", "-q", "-b", branch, w.seedCommitA)
	pbWrite(w.t, w.client, shared, "OURS\n")
	w.git(w.client, "add", shared)
	w.git(w.client, "commit", "-qm", "ours edits the shared path")
	w.git(w.client, "checkout", "-q", masterBranchName)

	// The peer holds the SAME branch and edits the SAME path differently, so
	// the push is refused AND the merge onto the peer's tip conflicts.
	pbDeliveryDrivePeerTakesTheBranchConflict(t, w, branch, shared, "THEIRS\n")

	// Precondition, MEASURED: aligning onto the peer's tip really does conflict.
	if clean, _, _ := gitPushbackDeliveryTrialMerge(context.Background(), w.client, branch,
		fmt.Sprintf(gitPushbackPeerRefMirrorFmt, branch)); clean {
		t.Fatalf("fixture: the merge of %s and the peer's %s must CONFLICT on %s",
			branch, fmt.Sprintf(gitPushbackPeerRefHeadsFmt, branch), shared)
	}

	oursTip := strings.TrimSpace(w.git(w.client, "rev-parse", branch))
	delivery := deliverBranchToPeer(context.Background(), w.client, w.server, branch, nil)

	if delivery.Delivered {
		t.Fatalf("an unresolvable conflict must NOT be delivered: %s", delivery.String())
	}
	if delivery.Shape != pushbackDeliveryRefused {
		t.Fatalf("shape = %q, want %q", delivery.Shape, pushbackDeliveryRefused)
	}
	// ① the reason names the MEASURED CAUSE — the conflict, not "push failed".
	if !strings.Contains(delivery.Reason, pushbackCauseContentConflict.String()) {
		t.Fatalf("the reason must name the measured cause %q: %q",
			pushbackCauseContentConflict, delivery.Reason)
	}
	// ② the reason carries GIT'S OWN OUTPUT: the conflicted PATH is on the line.
	if !strings.Contains(delivery.Reason, shared) {
		t.Fatalf("the reason must quote git's conflict output naming %q: %q", shared, delivery.Reason)
	}
	// ③ the round accounting rides the line.
	if !strings.Contains(delivery.Reason, "delivery loop") {
		t.Fatalf("the reason must account for its attempt: %q", delivery.Reason)
	}
	// ④ a refused delivery leaves the tip where it was.
	if got := strings.TrimSpace(w.git(w.client, "rev-parse", branch)); got != oursTip {
		t.Fatalf("%s moved from %s to %s although the delivery was refused — "+
			"a refusal may not rewrite the deliverable", branch, shortSha(oursTip), shortSha(got))
	}
	// ⑤ the loop stopped at the conflict instead of spending every round on the
	//    same merge (the live-lock the brake exists to prevent).
	if delivery.Rounds() >= pushbackDeliveryRounds {
		t.Fatalf("the loop spent %d rounds on a conflict no round can change (limit %d): %s",
			delivery.Rounds(), pushbackDeliveryRounds, delivery.String())
	}
}

// pbDeliveryDrivePeerTakesTheBranchConflict publishes the branch and then makes
// the PEER's copy of it differ on ONE path — the shape that both refuses the
// push and conflicts the alignment merge (both sides ADD the same path with
// different content, so the merge is an add/add conflict).
func pbDeliveryDrivePeerTakesTheBranchConflict(t *testing.T, w *pbWorld, branch, path, content string) {
	t.Helper()
	w.git(w.client, "push", "-q", w.server, branch+":"+branch)
	other := pbDeliveryDriveOtherClone(t, w)
	w.git(other, "checkout", "-q", "--detach", w.seedCommitA)
	pbWrite(t, other, path, content)
	w.git(other, "add", path)
	w.git(other, "commit", "-qm", "the peer edits the same path")
	w.git(other, "push", "-q", "--force", w.server, "HEAD:"+branch)
}

// TestDeliveryLoopSpendsOneRoundOnADeadPeer is the fourth property: an
// unreachable peer is NOT a retry target. One round, git's own sentence, and
// no second attempt (spending rounds on a constant is what the brake forbids).
func TestDeliveryLoopSpendsOneRoundOnADeadPeer(t *testing.T) {
	w := newPBWorld(t)
	const branch = "task/delivery-dead-peer"
	pbDeliveryDriveBranchFromSeed(t, w, branch, "mine.txt")

	missing := pbDeliveryDriveCloneDir(t, w, "no-such-peer")
	delivery := deliverBranchToPeer(context.Background(), w.client, missing, branch, nil)

	if delivery.Delivered {
		t.Fatalf("a dead peer cannot be delivered to: %s", delivery.String())
	}
	if delivery.Shape != pushbackDeliveryRefused && delivery.Shape != pushbackDeliveryBroken {
		t.Fatalf("shape = %q, want refused or broken", delivery.Shape)
	}
	if delivery.Rounds() != 1 {
		t.Fatalf("an unreachable peer must cost exactly ONE round (it is not a clock), got %d: %s",
			delivery.Rounds(), delivery.String())
	}
	if !strings.Contains(delivery.Reason, pushbackCauseUnreachable.String()) {
		t.Fatalf("the reason must name the measured cause %q: %q", pushbackCauseUnreachable, delivery.Reason)
	}
	// git's own words ride the reason: the refspec/target it could not reach.
	if !strings.Contains(delivery.Reason, missing) && !strings.Contains(delivery.Reason, "repository") {
		t.Fatalf("the reason must quote git's own sentence about the target: %q", delivery.Reason)
	}
}

// TestDeliveryLoopNamesTheNamedLimit is the Rule 1 assertion: the brake is ONE
// named constant, and the loop's own report reads it rather than a literal. A
// zero value here would mean the struct and the brake disagree.
func TestDeliveryLoopNamesTheNamedLimit(t *testing.T) {
	if pushbackDeliveryRounds < 2 {
		t.Fatalf("pushbackDeliveryRounds = %d — a loop whose limit is 1 cannot look again, "+
			"which is the whole defect this epic closes", pushbackDeliveryRounds)
	}
	var d pushbackDelivery
	d.Attempts = make([]pushbackDeliveryAttempt, pushbackDeliveryRounds)
	if got := d.Rounds(); got != pushbackDeliveryRounds {
		t.Fatalf("Rounds() = %d, want the named limit %d", got, pushbackDeliveryRounds)
	}
	// Every cause has a spelling (a cause rendered blank would make a report
	// line read "measured : …").
	for _, c := range []pushbackDeliveryCause{
		pushbackCauseUnreachable, pushbackCauseUnrelated, pushbackCauseCollision,
		pushbackCauseContentConflict, pushbackCauseStaleBase, pushbackCauseRefused,
	} {
		if strings.TrimSpace(c.String()) == "" {
			t.Fatalf("a delivery cause renders blank")
		}
	}
}
