package devworkspace

// push_landed_peer_test.go — `push accepted` must be a MEASUREMENT, not an exit
// code.
//
// THE DEFECT THIS FILE PINS (row 01a1a647, measured 2026-11-04). That row's
// output.log ends
//
//	clone-channel: attempt 2/3: push accepted
//
// and PROJECT.md Rule 6c reads that very line as the evidence of delivery — yet
// `git ls-remote --heads origin` carried no ref for the row. The line rested on
// nothing but `git push`'s exit status, so "git says it succeeded" and "the peer
// kept the commit" were ONE fact in this package. The arm below prints
// `push accepted` only after the peer has been ASKED, through the same
// `ls-remote` read the empty gate already trusts.
//
// THREE ARMS, and the middle one is the one that matters:
//
//	① the ordinary successful push — the peer carries our tip, the leg delivers,
//	  and the gate adds no network step's worth of false negative;
//	② a push git calls successful while the peer DOES NOT keep the ref (a
//	  receiving hook that restores the branch) — the leg must NOT deliver, must
//	  name both the local commit and the peer's tip, and must NOT print
//	  `push accepted`;
//	③ a peer that moved ON TOP of our push between the push and the read — a
//	  real race, and the delivery LANDED, so the gate must not turn it red.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pushRestoringHook installs a `post-receive` hook on the origin that ACCEPTS
// the ref update git was told to make and then puts the branch back where it
// was — the measured shape of "the push succeeded and the peer kept nothing".
//
// It is written into <origin>/.git/hooks/ so it runs on the RECEIVING side, in
// the peer's own repository, which is where this failure lives: the sender's git
// has nothing to complain about, and its exit status is 0.
func pushRestoringHook(t *testing.T, origin string) {
	t.Helper()
	hooks := filepath.Join(origin, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	// `$1` of a post-receive hook is "<old> <new> <ref>" per updated ref. The
	// hook restores the OLD value, so the peer ends where it started while the
	// sender saw a successful push.
	body := "#!/bin/sh\n" +
		"while read old new ref; do\n" +
		"  git update-ref \"$ref\" \"$old\"\n" +
		"done\n"
	if err := os.WriteFile(filepath.Join(hooks, "post-receive"), []byte(body), 0o755); err != nil {
		t.Fatalf("write post-receive: %v", err)
	}
}

// TestPushAcceptedRequiresThePeerToCarryTheCommit is arm ②: git reports success,
// the peer keeps nothing, and the leg must refuse to call that a delivery.
func TestPushAcceptedRequiresThePeerToCarryTheCommit(t *testing.T) {
	origin := newOrigin(t)
	pushRestoringHook(t, origin)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	commitIn(t, dir, "work.txt", "the deliverable nobody received")
	ourTip := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))
	peerBefore := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch))
	if ourTip == peerBefore {
		t.Fatalf("fixture: the run's commit %s equals the peer's tip — nothing was pushed, and this test proves nothing", ourTip)
	}

	// THE PRECONDITION THAT MAKES THE DEFECT EXPRESSIBLE: git's own push must
	// EXIT 0 against this peer. If the hook instead refused the ref, the channel
	// would take its REFUSED arm and the gate below would never be reached.
	out, perr := runCommand(context.Background(), dir, gitBinary, pushVerb, originRemoteName,
		gitHeadRef+":"+refsHeadsPrefix+DefaultBranch)
	if perr != nil {
		t.Fatalf("fixture: the restoring hook must let the push EXIT 0 and undo it afterwards, got %v: %s", perr, tail(out))
	}
	if after := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch)); after != peerBefore {
		t.Fatalf("fixture: the peer moved %s → %s although the hook was meant to undo the push; "+
			"the defect this arm reproduces does not exist here", peerBefore, after)
	}
	// The gate, asked the question the loop asks.
	landed, why := pushLandedOnPeer(context.Background(), dir, DefaultBranch)
	if landed {
		t.Fatalf("the gate reported LANDED although the peer's %s is still %s and the commit %s was never kept: %q",
			DefaultBranch, peerBefore, ourTip, why)
	}
	for _, want := range []string{ourTip, peerBefore} {
		if !strings.Contains(why, want) {
			t.Errorf("the refusal must name both sides of the comparison (%q); got: %s", want, why)
		}
	}
}

// TestPushAcceptedStillDeliversOnAnOrdinaryPush is arm ① and the false-positive
// guard: a healthy push publishes, the peer carries the tip, and the leg's own
// sentence appears.
func TestPushAcceptedStillDeliversOnAnOrdinaryPush(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	commitIn(t, dir, "work.txt", "the deliverable the peer receives")
	ourTip := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))
	// THE PUSH COMES FIRST, because the gate asks a question ABOUT a push: it is
	// reached only on the loop's `pushErr == nil` arm. Asking it before the push
	// would measure the peer's state BEFORE the delivery and read a truthful
	// "your commit is not there yet" as a false positive.
	if _, err := runCommand(context.Background(), dir, gitBinary, pushVerb, originRemoteName,
		gitHeadRef+":"+refsHeadsPrefix+DefaultBranch); err != nil {
		t.Fatalf("fixture: the ordinary push must land: %v", err)
	}

	landed, why := pushLandedOnPeer(context.Background(), dir, DefaultBranch)
	if !landed {
		t.Fatalf("the gate refused a HEALTHY push (this is the false positive the gate must not have): %s", why)
	}
	if peer := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch)); peer != ourTip {
		t.Fatalf("fixture: the peer is at %s, our tip is %s — the two must equal for this arm", peer, ourTip)
	}
}

// TestPushLandedToleratesAPeerThatMovedOnTopOfOurPush is arm ③: a concurrent
// push landed ABOVE ours between our push and the read. Our commit IS in the
// peer's history, so the delivery happened — the gate must say so, and must not
// report the race as a lost delivery.
func TestPushLandedToleratesAPeerThatMovedOnTopOfOurPush(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	commitIn(t, dir, "work.txt", "our deliverable")
	ourTip := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))
	if _, err := runCommand(context.Background(), dir, gitBinary, pushVerb, originRemoteName,
		gitHeadRef+":"+refsHeadsPrefix+DefaultBranch); err != nil {
		t.Fatalf("fixture: our push must land first")
	}
	// SOMEBODY ELSE'S COMMIT LANDS ON TOP — made in the peer itself, so it is a
	// different author and a different tree from anything this ground holds.
	commitIn(t, origin, "theirs.txt", "somebody else's commit landed after ours")
	peerTip := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch))
	if peerTip == ourTip {
		t.Fatalf("fixture: the peer did not move past our commit")
	}

	landed, why := pushLandedOnPeer(context.Background(), dir, DefaultBranch)
	if !landed {
		t.Fatalf("a peer that moved ON TOP of our push (%s above %s) is a delivery that happened, "+
			"not a lost one — the gate must not call it red: %s", peerTip, ourTip, why)
	}
}

// TestDeliveryDoesNotPrintPushAcceptedWhenThePeerKeptNothing is the arm that
// makes the RED PROOF bite: it drives the REAL loop (AttemptDelivery), not the
// gate directly. A test that called the gate itself would stay green with the
// gate removed from the loop — and the defect that matters is precisely "the
// loop prints `push accepted` on an exit code".
//
// The assertions are the epic's, in order: no `push accepted` line, a non-nil
// error naming both sides, and `Pushed` false.
func TestDeliveryDoesNotPrintPushAcceptedWhenThePeerKeptNothing(t *testing.T) {
	origin := newOrigin(t)
	pushRestoringHook(t, origin)
	cfg := cloneCfg(t, origin)
	var log bytes.Buffer
	cfg.Log = &log
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	commitIn(t, dir, "work.txt", "the deliverable the peer throws away")
	peerBefore := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch))

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	// THE LINE ITSELF: whatever else happens, the channel may not write the
	// sentence Rule 6c reads as proof of delivery.
	for _, line := range strings.Split(log.String(), "\n") {
		if strings.Contains(line, "push accepted") {
			t.Fatalf("the channel printed %q although the peer never kept the commit; its whole log:\n%s", line, log.String())
		}
	}
	if blk != nil {
		t.Fatalf("a push the peer discarded is not a retryable refusal — it must not wear the brake's vocabulary: %q", blk.Report())
	}
	if err == nil {
		t.Fatalf("the delivery reported SUCCESS although the peer is still at %s and nothing was kept; pushed=%v log:\n%s",
			peerBefore, res.Pushed, log.String())
	}
	if res.Pushed {
		t.Fatalf("Result.Pushed must stay false: the peer never received the commit")
	}
	if !strings.Contains(err.Error(), "does not carry") {
		t.Errorf("the failure must say the peer does not carry the commit; got: %v", err)
	}
}
