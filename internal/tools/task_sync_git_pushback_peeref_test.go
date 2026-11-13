package tools

// Peer-ref NAME COLLISION classification tests (epic: a name collision is
// indistinguishable from a transport failure).
//
// The lane used to learn about a colliding peer ref only from git's own push
// refusal, which every harvest rendered through one generic line
// (`pushbackPushRefusedReasonFmt`) — so "the peer already owns this name at a
// divergent tip, a human must decide" read exactly like "the peer is
// unreachable / refused the credential". These tests pin the explicit question
// and its three-way answer, and they run against REAL git: the verdicts are
// ancestry relations (containment), which a mock cannot prove.
//
// Every assertion is made on git's OWN refs (never on the report alone): what
// the peer's refs/heads holds before and after the run is the ground truth.
//
// Run:  go test -run 'TestPushbackPeerRef|TestClassifyPeerRef|TestPeerRefTip|TestPushedEntry' ./internal/tools/

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pbRefSha reads refs/heads/<branch> out of the bare peer repo ("" when it does
// not exist).
func pbRefSha(w *pbWorld, branch string) string {
	out, code := pbGitMaybe(w.t, w.server, "rev-parse", "--verify", "--quiet",
		"refs/heads/"+branch)
	if code != 0 {
		return ""
	}
	return strings.TrimSpace(out)
}

// pbForEachRef snapshots the peer's whole refs/heads namespace — the
// acceptance's proof that a refused push left the peer exactly as it was.
func pbForEachRef(w *pbWorld) string {
	out, code := pbGitMaybe(w.t, w.server, "for-each-ref",
		"--format=%(objectname) %(refname:short)", "refs/heads")
	if code != 0 {
		w.t.Fatalf("for-each-ref on the peer failed (%d): %s", code, out)
	}
	return strings.TrimSpace(out)
}

// pbPeerBranchAt clones the peer, cuts a branch from the peer's master
// carrying its OWN commit and pushes it back, then removes the clone — i.e.
// the peer independently owns refs/heads/<branch> at a tip the client has not
// seen. Returns that tip.
func pbPeerBranchAt(t *testing.T, w *pbWorld, branch, fileName, content, msg string) string {
	t.Helper()
	dup := filepath.Join(w.root, "peer-"+strings.ReplaceAll(branch, "/", "-"))
	if err := os.RemoveAll(dup); err != nil {
		t.Fatalf("clear peer clone dir: %v", err)
	}
	w.git(w.root, "clone", "-q", w.server, dup)
	w.git(dup, "config", "user.email", "t@t")
	w.git(dup, "config", "user.name", "t")
	// Start from the peer's OWN tip of that name when it already owns one:
	// `checkout -b` branches from HEAD (the peer's master) and would leave the
	// branch BEHIND its namesake, so the helper's push would be rejected for a
	// reason that has nothing to do with what the caller is building.
	if _, code := pbGitMaybe(t, dup, "rev-parse", "--verify", "--quiet",
		"refs/remotes/origin/"+branch); code == 0 {
		w.git(dup, "checkout", "-q", "-B", branch, "refs/remotes/origin/"+branch)
	} else {
		w.git(dup, "checkout", "-q", "-b", branch)
	}
	pbWrite(t, dup, fileName, content)
	w.git(dup, "add", fileName)
	w.git(dup, "commit", "-q", "-m", msg)
	w.git(dup, "push", "-q", w.server, branch+":"+branch)
	tip := w.git(dup, "rev-parse", "HEAD")
	if err := os.RemoveAll(dup); err != nil {
		t.Fatalf("remove peer clone: %v", err)
	}
	return tip
}

// pbClientBranchOnMaster cuts a branch from master and adds one commit — the
// FF-ABLE shape (it contains the peer's master tip), so the ff gate lets it
// through and the peer-ref gate is the only thing left to decide it.
func pbClientBranchOnMaster(t *testing.T, w *pbWorld, branch, fileName string) string {
	t.Helper()
	c := w.client
	w.git(c, "checkout", "-q", "-b", branch, masterBranchName)
	pbWrite(t, c, fileName, "work on "+branch)
	w.git(c, "add", fileName)
	w.git(c, "commit", "-q", "-m", "work on "+branch)
	tip := w.git(c, "rev-parse", "HEAD")
	w.git(c, "checkout", "-q", masterBranchName)
	return tip
}

// pbAssertDivergent measures that two tips of one branch name genuinely
// diverged — neither contains the other — by bringing both into one scratch
// clone (merge-base needs both objects in one object store; the client repo
// holds no copy of the peer's tip, so asking it there would exit 128 on an
// unknown object, which is an ERROR, not a measurement).
func pbAssertDivergent(t *testing.T, w *pbWorld, branch, clientTip, peerTip string) {
	t.Helper()
	side := filepath.Join(w.root, "diverge-"+strings.ReplaceAll(branch, "/", "-"))
	if err := os.RemoveAll(side); err != nil {
		t.Fatalf("clear scratch: %v", err)
	}
	w.git(w.root, "clone", "-q", w.server, side)
	w.git(side, "fetch", "-q", w.client, "refs/heads/"+branch)
	for _, c := range []struct{ ancestor, descendant, name string }{
		{peerTip, clientTip, "peer must NOT be an ancestor of ours"},
		{clientTip, peerTip, "ours must NOT be an ancestor of the peer's"},
	} {
		if _, code := pbGitMaybe(t, side, "merge-base", "--is-ancestor", c.ancestor, c.descendant); code != 1 {
			t.Fatalf("fixture: %s (merge-base exit=%d)", c.name, code)
		}
	}
	if err := os.RemoveAll(side); err != nil {
		t.Fatalf("remove scratch: %v", err)
	}
}

// TestPushbackPeerRefCollisionRefusedByName is the epic's red-proof case: the
// peer owns the SAME name at a DIVERGENT tip, so the push can only be
// non-fast-forward. The lane must refuse BY NAME, stating both tips and the
// remedy — never the generic "the peer rejected the push" line, which is
// indistinguishable from a transport or permission failure.
func TestPushbackPeerRefCollisionRefusedByName(t *testing.T) {
	w := newPBWorld(t)
	clientTip := pbClientBranchOnMaster(t, w, "feat/dup", "dup-client.txt")
	peerTip := pbPeerBranchAt(t, w, "feat/dup", "dup-peer.txt", "peer-side work", "peer work")
	before := pbForEachRef(w)

	// Precondition, measured (not assumed): the two tips really diverged —
	// neither contains the other.
	pbAssertDivergent(t, w, "feat/dup", clientTip, peerTip)

	flush := gitPushbackPlan(context.Background(), w.client, w.server)

	if len(flush.Failed) != 1 {
		t.Fatalf("a name collision must be refused (exactly one entry): failed=%v pushed=%v", flush.Failed, flush.Pushed)
	}
	line := flush.Failed[0]
	// ① keyed by the branch's own name (the decision names the name at stake).
	if !strings.HasPrefix(line, "feat/dup: ") {
		t.Fatalf("the collision must be keyed by the branch name, got %q", line)
	}
	// ② a COLLISION verdict, not the anonymous failure the epic is about.
	if !strings.Contains(line, "NAME COLLISION") {
		t.Errorf("the refusal does not name the collision: %q", line)
	}
	if strings.Contains(line, "the peer rejected the push") {
		t.Errorf("the collision was reported as the generic push failure — the defect this gate closes: %q", line)
	}
	// ③ BOTH tips, abbreviated, so either side can be inspected.
	for _, want := range []string{shortSha(peerTip), shortSha(clientTip)} {
		if !strings.Contains(line, want) {
			t.Errorf("the refusal must state tip %q (peer %s, ours %s): %q",
				want, shortSha(peerTip), shortSha(clientTip), line)
		}
	}
	// ④ the remedy, and the statement of what did NOT happen.
	for _, want := range []string{"reconcile the two tips", "re-deliver"} {
		if !strings.Contains(line, want) {
			t.Errorf("the refusal must carry the remedy (%q): %q", want, line)
		}
	}
	if !strings.Contains(line, "nothing was pushed") {
		t.Errorf("the refusal must say nothing was pushed: %q", line)
	}
	// ⑤ NOTHING crossed the wire and the peer's namespace is byte-identical
	//    (for-each-ref over refs/heads is the acceptance's proof).
	if len(flush.Pushed) != 0 {
		t.Fatalf("a refused collision must not push anything: %v", flush.Pushed)
	}
	if after := pbForEachRef(w); after != before {
		t.Fatalf("the peer's refs/heads changed across a refused push:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got := pbRefSha(w, "feat/dup"); got != peerTip {
		t.Fatalf("the peer's own feat/dup moved: got %s want %s", got, peerTip)
	}
	// ⑥ the client's ref is left exactly as it was (a refusal may not delete
	//    or rewrite our side either).
	if got := w.git(w.client, "rev-parse", "feat/dup"); got != clientTip {
		t.Errorf("the refused branch's tip moved: got %s want %s", got, clientTip)
	}
}

// TestPushbackPeerRefAncestorFastForwardedNotCollision is the other half of
// the three-way answer: the peer owns the name, but its tip is CONTAINED in
// ours, so the push fast-forwards the peer's own ref forward along our
// history. That is not a collision — the peer's commits are kept — and the
// report must say which of the two it was.
func TestPushbackPeerRefAncestorFastForwardedNotCollision(t *testing.T) {
	w := newPBWorld(t)
	c := w.client

	// The peer's ref of this name is OURS, one commit behind: create it by
	// pushing our first commit to the peer, then advance locally.
	w.git(c, "checkout", "-q", "-b", "feat/ff", masterBranchName)
	pbWrite(t, c, "ff1.txt", "first")
	w.git(c, "add", "ff1.txt")
	w.git(c, "commit", "-q", "-m", "first")
	peerTip := w.git(c, "rev-parse", "HEAD")
	w.git(c, "push", "-q", w.server, "feat/ff:feat/ff")
	pbWrite(t, c, "ff2.txt", "second")
	w.git(c, "add", "ff2.txt")
	w.git(c, "commit", "-q", "-m", "second")
	clientTip := w.git(c, "rev-parse", "HEAD")
	w.git(c, "checkout", "-q", masterBranchName)

	// Precondition, measured: the peer's tip IS an ancestor of ours.
	if _, code := pbGitMaybe(t, c, "merge-base", "--is-ancestor", peerTip, clientTip); code != 0 {
		t.Fatalf("fixture: peer tip must be an ancestor of ours (merge-base exit=%d)", code)
	}

	flush := gitPushbackPlan(context.Background(), c, w.server)

	if len(flush.Failed) != 0 {
		t.Fatalf("an ancestor peer ref is not a collision: failed=%v", flush.Failed)
	}
	if len(flush.Pushed) != 1 || !strings.HasPrefix(flush.Pushed[0], "feat/ff") {
		t.Fatalf("the branch must be pushed under its own name: pushed=%v", flush.Pushed)
	}
	// The report says it was a FAST-FORWARD, with both tips.
	if !strings.Contains(flush.Pushed[0], "fast-forwarded") ||
		!strings.Contains(flush.Pushed[0], shortSha(peerTip)) ||
		!strings.Contains(flush.Pushed[0], shortSha(clientTip)) {
		t.Errorf("the push must report the fast-forward and both tips: %q (peer %s, ours %s)",
			flush.Pushed[0], shortSha(peerTip), shortSha(clientTip))
	}
	if !pushbackContains(flush.FastForwarded, "feat/ff") {
		t.Errorf("the machine-readable fast-forward list is missing the branch: %v", flush.FastForwarded)
	}
	// The peer's ref really moved forward — and holds OUR tip now.
	if got := pbRefSha(w, "feat/ff"); got != clientTip {
		t.Errorf("the peer's feat/ff was not fast-forwarded: got %s want %s", got, clientTip)
	}
}

// TestPushbackPeerRefAbsentPushesPlainName guards the ordinary case against
// report noise: the peer does not own the name, so the push CREATES it and the
// entry is the bare branch name (no note, nothing in the fast-forward list).
func TestPushbackPeerRefAbsentPushesPlainName(t *testing.T) {
	w := newPBWorld(t)
	pbClientBranchOnMaster(t, w, "feat/new", "new.txt")

	flush := gitPushbackPlan(context.Background(), w.client, w.server)

	if len(flush.Failed) != 0 {
		t.Fatalf("an absent peer ref must not refuse: failed=%v", flush.Failed)
	}
	if len(flush.Pushed) != 1 || flush.Pushed[0] != "feat/new" {
		t.Fatalf("the pushed entry must be the bare branch name, got %v", flush.Pushed)
	}
	if len(flush.FastForwarded) != 0 {
		t.Fatalf("a created ref is not a fast-forward: %v", flush.FastForwarded)
	}
	if got := pbRefSha(w, "feat/new"); got == "" {
		t.Fatal("the branch did not reach the peer")
	}
}

// TestPushbackPeerRefUnrelatedHistoryUnderSameNameIsCollision pins the epic's
// "unrelated history → collision verdict, not an anonymous failure" clause.
// The branch itself shares history with the peer's master (so the ancestry
// gate lets it through and no "no common ancestor" line is printed); it is the
// PEER'S REF OF THAT NAME that comes from a foreign history — one name, two
// incompatible worlds, which is exactly a decision for a human.
func TestPushbackPeerRefUnrelatedHistoryUnderSameNameIsCollision(t *testing.T) {
	w := newPBWorld(t)
	c := w.client
	clientTip := pbClientBranchOnMaster(t, w, "feat/junk", "junk-client.txt")

	// The peer's ref of that name, minted from a scratch repo with NO shared
	// history (the 2026-09-21 incident's shape), pushed straight onto the bare
	// peer under the same name.
	scratch := filepath.Join(w.root, "scratch")
	w.git(w.root, "init", "-q", "-b", masterBranchName, scratch)
	w.git(scratch, "config", "user.email", "t@t")
	w.git(scratch, "config", "user.name", "t")
	pbWrite(t, scratch, "foreign.txt", "foreign work")
	w.git(scratch, "add", "foreign.txt")
	w.git(scratch, "commit", "-q", "-m", "foreign root")
	w.git(scratch, "push", "-q", w.server, masterBranchName+":feat/junk")
	peerTip := w.git(scratch, "rev-parse", "HEAD")
	before := pbForEachRef(w)

	flush := gitPushbackPlan(context.Background(), c, w.server)

	if len(flush.Failed) != 1 {
		t.Fatalf("an unrelated-history name collision must be refused once: failed=%v pushed=%v", flush.Failed, flush.Pushed)
	}
	line := flush.Failed[0]
	if !strings.Contains(line, "NAME COLLISION") {
		t.Errorf("the unrelated peer ref must land in the COLLISION bucket, got %q", line)
	}
	if strings.Contains(line, "shares no common ancestor with") {
		t.Errorf("the refusal blames the BRANCH's ancestry — that is the other gate; here the branch shares master's history and the collision is on the name: %q", line)
	}
	if !strings.Contains(line, shortSha(peerTip)) || !strings.Contains(line, shortSha(clientTip)) {
		t.Errorf("both tips must be stated (peer %s, ours %s): %q", shortSha(peerTip), shortSha(clientTip), line)
	}
	if after := pbForEachRef(w); after != before {
		t.Fatalf("the peer changed across a refused push:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestClassifyPeerRefThreeWay pins the classifier itself (the plan's decision
// is only as good as this function), including the Rule 15 duty: an UNMEASURED
// premise is its own verdict, never "collision".
func TestClassifyPeerRefThreeWay(t *testing.T) {
	w := newPBWorld(t)
	c := w.client
	ctx := context.Background()

	// ① the peer owns feat/ff at a tip CONTAINED IN OURS: we take the peer's
	//    commit as our base and add one of our own on top.
	peerTip := pbPeerBranchAt(t, w, "feat/ff", "ff1.txt", "peer first", "peer first")
	w.git(c, "fetch", "-q", w.server, "refs/heads/feat/ff:refs/heads/feat/ff")
	w.git(c, "checkout", "-q", "feat/ff")
	pbWrite(t, c, "ff2.txt", "ours")
	w.git(c, "add", "ff2.txt")
	w.git(c, "commit", "-q", "-m", "our commit")
	w.git(c, "checkout", "-q", masterBranchName)
	if got, tip := classifyForTest(t, ctx, c, w.server, "feat/ff", peerTip); got != gitPushbackPeerRefFastForward {
		t.Errorf("contained peer tip: want fast-forward, got %s", got)
	} else if tip != shortSha(peerTip) {
		t.Errorf("the fast-forward verdict must carry the peer tip: got %q want %q", tip, shortSha(peerTip))
	}

	// ② the peer advances with its OWN commit from that same base — the two
	//    tips now diverge (neither contains the other) → collision.
	divergent := pbPeerBranchAt(t, w, "feat/ff", "ff3.txt", "peer own", "peer own work")
	if got, tip := classifyForTest(t, ctx, c, w.server, "feat/ff", divergent); got != gitPushbackPeerRefCollision {
		t.Errorf("divergent tip: want collision, got %s", got)
	} else if tip != shortSha(divergent) {
		t.Errorf("the collision verdict must carry the peer tip: got %q want %q", tip, shortSha(divergent))
	}

	// ③ OUR tip is an ancestor of the peer's (the peer owns the name and has
	//    moved PAST us) → still a collision: the push cannot be a fast-forward,
	//    and the peer's ref is not ours to move.
	w.git(c, "branch", "-f", "feat/behind", w.seedCommitA)
	movedOn := pbPeerBranchAt(t, w, "feat/behind", "moved.txt", "peer moved", "peer moved on")
	if got, tip := classifyForTest(t, ctx, c, w.server, "feat/behind", movedOn); got != gitPushbackPeerRefCollision {
		t.Errorf("peer ahead of ours: want collision, got %s", got)
	} else if tip != shortSha(movedOn) {
		t.Errorf("the collision verdict must carry the peer tip: got %q want %q", tip, shortSha(movedOn))
	}

	// ④ UNMEASURED, never a collision (Rule 15): a LOCAL branch the peer does
	//    not own — the fetch finds no such ref there, so the question was not
	//    answered. A wrong verdict here would refuse a perfectly pushable
	//    branch on an unmeasured premise.
	late := pbClientBranchOnMaster(t, w, "feat/localonly", "local.txt")
	if got, _ := classifyForTest(t, ctx, c, w.server, "feat/localonly", late); got != gitPushbackPeerRefUnmeasured {
		t.Errorf("a ref the peer does not own leaves the question UNMEASURED: got %s", got)
	}
	// ⑤ UNMEASURED: a local branch that does not resolve at all.
	if got, _ := classifyForTest(t, ctx, c, w.server, "feat/does-not-exist", late); got != gitPushbackPeerRefUnmeasured {
		t.Errorf("an unknown local branch must be UNMEASURED: got %s", got)
	}
	// ⑥ UNMEASURED: an unreachable peer (the fixture path is gone).
	if got, _ := classifyForTest(t, ctx, c, filepath.Join(w.root, "gone.git"), "feat/ff", late); got != gitPushbackPeerRefUnmeasured {
		t.Errorf("an unreachable peer must be UNMEASURED: got %s", got)
	}
	// ⑦ the ABSENT answer is produced by the classifier too (the plan's
	//    ordinary case): an empty advertised tip is the measured absence of the
	//    peer's ref, which the push CREATES — not a collision and not an
	//    unmeasured premise.
	if got, tip := classifyForTest(t, ctx, c, w.server, "feat/ff", ""); got != gitPushbackPeerRefAbsent {
		t.Errorf("an empty advertised tip is an ABSENCE: got %s", got)
	} else if tip != "" {
		t.Errorf("the absent verdict carries no peer tip, got %q", tip)
	}
}

// TestPeerRefTipMeasuredVsUnmeasured pins the probe's two-state answer: an
// ABSENT ref is a MEASUREMENT ("the peer does not own this name"), while an
// unreachable target is not — the collision verdict may not be built on the
// latter (Rule 15). The probe runs from a real repo (gitPushbackGitRun takes a
// directory, so "." only works when the test process sits in a repo).
func TestPeerRefTipMeasuredVsUnmeasured(t *testing.T) {
	w := newPBWorld(t)
	ctx := context.Background()

	if tip, measured := gitPushbackPeerRefTip(ctx, w.client, w.server, "feat/nope"); !measured || tip != "" {
		t.Errorf("a peer without the ref is a measured ABSENCE: tip=%q measured=%v", tip, measured)
	}
	peerTip := pbPeerBranchAt(t, w, "feat/here", "here.txt", "here", "here")
	if tip, measured := gitPushbackPeerRefTip(ctx, w.client, w.server, "feat/here"); !measured || tip != shortSha(peerTip) {
		t.Errorf("the peer's own tip must be read (abbreviated for the report): tip=%q measured=%v want %s",
			tip, measured, shortSha(peerTip))
	}
	if tip, measured := gitPushbackPeerRefTip(ctx, w.client, filepath.Join(w.root, "gone.git"), "feat/here"); measured || tip != "" {
		t.Errorf("an unreachable peer is UNMEASURED: tip=%q measured=%v", tip, measured)
	}
}

// TestPushedEntryUnmeasuredNote pins the renderer's Rule 15 duty: a push that
// went through with an unanswered peer-ref question says so and never reads as
// a verdict — while the ordinary entries stay machine-friendly.
func TestPushedEntryUnmeasuredNote(t *testing.T) {
	ctx := context.Background()
	w := newPBWorld(t)
	tip := pbClientBranchOnMaster(t, w, "feat/x", "x.txt")

	got := pushbackPushedEntry(ctx, w.client, "feat/x", "", "ls-remote failed")
	if !strings.Contains(got, "UNMEASURED") || !strings.HasPrefix(got, "feat/x") {
		t.Errorf("an unmeasured push must carry the note and stay keyed by the branch: %q", got)
	}
	if strings.Contains(got, "NAME COLLISION") {
		t.Errorf("an unmeasured premise must never be reported as a collision: %q", got)
	}
	// The ordinary case stays the bare name.
	if got := pushbackPushedEntry(ctx, w.client, "feat/x", "", ""); got != "feat/x" {
		t.Errorf("a plain push entry must be the bare branch name: %q", got)
	}
	// A fast-forward entry names the moved ref.
	if ff := pushbackPushedEntry(ctx, w.client, "feat/x", tip, ""); !strings.Contains(ff, "fast-forwarded") ||
		!strings.Contains(ff, shortSha(tip)) {
		t.Errorf("the fast-forward entry must name the moved ref: %q", ff)
	}
}

// TestShortSha pins the abbreviation helper (a report line is only actionable
// if the sha it prints can be pasted back into git).
func TestShortSha(t *testing.T) {
	if got := shortSha("0123456789abcdef"); got != "0123456" {
		t.Errorf("shortSha must abbreviate to gitPushbackShortShaLen: %q", got)
	}
	if got := shortSha("abc"); got != "abc" {
		t.Errorf("a short sha is returned as-is: %q", got)
	}
}

func classifyForTest(t *testing.T, ctx context.Context, repo, target, branch, tip string) (gitPushbackPeerRefVerdict, string) {
	t.Helper()
	return gitPushbackClassifyPeerRef(ctx, repo, target, branch, tip)
}
