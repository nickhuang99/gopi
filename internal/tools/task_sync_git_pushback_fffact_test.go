package tools

// The push-back FF-FACT tests (epic: the third gate must not refuse).
//
// THE SPECIFICATION THIS FILE PINS. The lane's third question used to be a
// GATE: "does this branch contain the peer's master?" — and a "no" REFUSED the
// push. Measured consequence: a branch carrying real work, cut from a base the
// peer has since moved past, is reported as failed with the remedy "merge
// master into it", and the deliverable never crosses. The question is a FACT
// ABOUT ALIGNMENT, not a verdict on whether the work may be handed over: the
// peer's merger decides what it can land, and it says so itself. So the answer
// is reported on the Pushed entry and the branch is pushed either way.
//
// The two REFUSALS that remain are asserted in their own files, and must stay
// red-proof green: internal/tools/task_sync_git_pushback_ancestry_test.go
// (unrelated history) and ..._peeref_test.go (peer-ref NAME COLLISION).
//
// Every test runs against REAL git (bare peer + client clone): "does this
// branch contain that tip" is a git-semantics question and a mock cannot prove
// it.
//
// Run this file's tests:  go test -run TestPushbackFFFact ./internal/tools/

import (
	"context"
	"strings"
	"testing"
)

// pbBehindBranch is the shape the third gate used to refuse: the peer's master
// has moved ahead (an unrelated commit), and this branch carries its own work
// on the OLD base — so it does not contain the peer's master tip, while
// sharing history with it (an aligned-but-behind deliverable, the ordinary
// state of a long-running client host).
//
// It returns the peer-master tip and the branch tip so a caller can assert on
// both.
func pbBehindBranch(t *testing.T, w *pbWorld, branch string) (peerTip, branchTip string) {
	t.Helper()
	c := w.client

	// The peer's master moves ahead of the seed.
	w.git(c, "checkout", "-q", masterBranchName)
	pbWrite(w.t, c, "peer.txt", "peer work")
	w.git(c, "add", "peer.txt")
	w.git(c, "commit", "-qm", "peer master advances")
	w.git(c, "push", "-q", w.server, masterBranchName+":"+masterBranchName)
	peerTip = w.git(c, "rev-parse", masterBranchName)

	// The branch: real work, cut from the OLD master (the seed commit).
	w.git(c, "checkout", "-q", "-b", branch, w.seedCommitA)
	pbWrite(w.t, c, "mine.txt", "my work")
	w.git(c, "add", "mine.txt")
	w.git(c, "commit", "-qm", "my work on the stale base")
	branchTip = w.git(c, "rev-parse", branch)
	w.git(c, "checkout", "-q", masterBranchName)

	// Preconditions, MEASURED (never assumed): the branch shares history with
	// the peer's master, and it does NOT contain it.
	_, code := pbGitMaybe(t, c, "merge-base", "--all", masterBranchName, branch)
	if code != 0 {
		t.Fatalf("fixture: %s must share history with %s (merge-base --all exit=%d)", branch, masterBranchName, code)
	}
	if _, code := pbGitMaybe(t, c, "merge-base", "--is-ancestor", peerTip, branchTip); code != 1 {
		t.Fatalf("fixture: the branch must NOT contain the peer's master "+
			"(merge-base --is-ancestor <peer tip> <branch> exit=%d, want 1)", code)
	}
	return peerTip, branchTip
}

// TestPushbackBehindMasterBranchIsPushedNotRefused is the red-proof: a branch
// that does not contain the peer's master is DELIVERED (the peer's merger
// decides what it can land), and its alignment is stated as a fact on the
// report — never as a refusal, and never with the unperformable remedy the old
// gate line carried.
func TestPushbackBehindMasterBranchIsPushedNotRefused(t *testing.T) {
	w := newPBWorld(t)
	peerTip, branchTip := pbBehindBranch(t, w, "rework/behind")

	flush := gitPushbackPlan(context.Background(), w.client, w.server)

	// ① The deliverable crossed. The assertion is POSITIVE first: the branch
	// is IN Pushed, and nothing failed.
	if !pbHasPrefix(flush.Pushed, "rework/behind") {
		t.Fatalf("a behind-master branch must be delivered, not refused: pushed=%v failed=%v", flush.Pushed, flush.Failed)
	}
	if why := pbFailureFor(flush.Failed, "rework/behind"); why != "" {
		t.Errorf("the branch was refused — the peer's merger owns the landing call: %q", why)
	}
	if len(flush.Failed) != 0 {
		t.Errorf("no branch may fail in this flush: %v", flush.Failed)
	}
	// ② It really reached the peer, at OUR tip.
	if got := pbRefSha(w, "rework/behind"); got != branchTip {
		t.Errorf("the peer's ref did not receive our tip: got %q want %q", got, branchTip)
	}
	// ③ The FACT is on the report: the entry says the branch does not contain
	// the peer's master (with the peer tip it is behind), so a reader is never
	// left to infer alignment from a silent success.
	entry := pbEntryFor(flush.Pushed, "rework/behind")
	for _, want := range []string{pushbackNotFfableFactPhrase, shortSha(peerTip)} {
		if !strings.Contains(entry, want) {
			t.Errorf("the Pushed entry must state the alignment fact (%q) and the peer tip %s: %q",
				want, shortSha(peerTip), entry)
		}
	}
	// ④ The unperformable remedy is gone: "merge master into it" is advice the
	// peer's own landing path does not need, and its loop is what this epic
	// removes.
	if strings.Contains(entry, "merge "+masterBranchName+" into it") {
		t.Errorf("the report still advises the unperformable merge loop: %q", entry)
	}
	if strings.Contains(entry, "push refused") {
		t.Errorf("the entry still reads as a refusal: %q", entry)
	}
}

// TestPushbackFfableBranchIsPushedWithFfFinding is the POSITIVE CONTROL for
// the red-proof above: an ordinary deliverable that DOES contain the peer's
// master is delivered too, and its entry says so. Both branches in one flush
// would pass a gate that pushed everything; here the point is that the two
// findings are distinguishable on the same report.
func TestPushbackFfableBranchIsPushedWithFfFinding(t *testing.T) {
	w := newPBWorld(t)
	c := w.client
	branch := "feat/aligned"
	w.git(c, "checkout", "-q", "-b", branch, masterBranchName)
	pbWrite(w.t, c, "aligned.txt", "work")
	w.git(c, "add", "aligned.txt")
	w.git(c, "commit", "-qm", "aligned work")
	tip := w.git(c, "rev-parse", "HEAD")
	w.git(c, "checkout", "-q", masterBranchName)

	// Precondition, measured: the branch DOES contain the peer's master.
	if _, code := pbGitMaybe(t, c, "merge-base", "--is-ancestor", masterBranchName, branch); code != 0 {
		t.Fatalf("fixture: %s must contain %s (merge-base --is-ancestor exit=%d)", branch, masterBranchName, code)
	}

	flush := gitPushbackPlan(context.Background(), c, w.server)

	if !pbHasPrefix(flush.Pushed, branch) {
		t.Fatalf("an aligned deliverable must be pushed: pushed=%v failed=%v", flush.Pushed, flush.Failed)
	}
	if got := pbRefSha(w, branch); got != tip {
		t.Errorf("the peer's ref did not receive our tip: got %q want %q", got, tip)
	}
	ff, notFf := pbFfFactSplit(flush)
	if !pbContains(ff, branch) {
		t.Errorf("an aligned branch must be reported in the fast-forwardable half: %v", ff)
	}
	if pbContains(notFf, branch) {
		t.Errorf("an aligned branch must not be reported as not-fast-forwardable: %v", notFf)
	}
}

// pbFfFactSplit reads the alignment fact's machine-readable halves off a flush
// (the pair the report renders and a caller acts on).
func pbFfFactSplit(f gitPushbackFlush) (ff, notFf []string) {
	return f.FastForwardable, f.NotFastForwardable
}

// pbHasPrefix reports whether any entry in list starts with prefix (a Pushed
// entry is keyed by the branch name, with a note appended).
func pbHasPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if s == prefix || strings.HasPrefix(s, prefix+" ") {
			return true
		}
	}
	return false
}

// pbEntryFor returns the Pushed entry for a branch ("" when absent).
func pbEntryFor(pushed []string, branch string) string {
	for _, s := range pushed {
		if s == branch || strings.HasPrefix(s, branch+" ") {
			return s
		}
	}
	return ""
}
