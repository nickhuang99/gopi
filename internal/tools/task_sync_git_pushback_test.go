package tools

// Push-back merged-branch-filter tests (epic bugfix: the default git
// push-back pushed EVERY local non-master branch, resurrecting branches the
// server had adopted into master and deleted — requirement acceptance (a-d)).
//
// All tests run against REAL git: a bare "server" repo + a client repo (the
// filter, the up-to-date no-op, the non-fast-forward collision and the
// server-ref resurrection are all git-semantics — mocks cannot prove them).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pbRun wraps exec git with test env (deterministic identity).
func pbRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func pbWrite(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
}

// pbWorld is a real two-node git world: a bare "server" repo + a client
// working clone sharing an integrated master.
type pbWorld struct {
	t           *testing.T
	root        string
	server      string // bare server path
	client      string // client working clone
	git         func(dir string, args ...string) string
	seedCommitA string // HEAD sha of the seed master
}

func newPBWorld(t *testing.T) *pbWorld {
	w := newPBWorldUnfenced(t)
	w.git(w.client, "config", "user.email", "t@t")
	w.git(w.client, "config", "user.name", "t")
	return w
}

// newPBWorldUnfenced is the SAME two-node world without the author identity
// configured on the client — the slice the AST refactor of newPBWorld (epic
// dead-test-fence, 2026-10-14) deliberately left out of the helper it rewrote.
//
// WHY IT EXISTS AT ALL: newPBWorld used to be the literal below, and one
// caller (pbFenceRun, task_sync_git_pushback_testfence_test.go) needs the
// world BEFORE its committer identity is set — it installs a sentinel ssh and
// a leak-detecting fence first, so that even the fixture's own commits are
// made under the fence. An AST-rewrite refactor that inlined the identity
// lines into the shared constructor would have silently moved that call
// OUTSIDE the fence; keeping the split in a named, self-explaining helper is
// what keeps the rewritten form behaviourally identical to the literal it
// replaced.
func newPBWorldUnfenced(t *testing.T) *pbWorld {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	w := &pbWorld{t: t}
	w.root = t.TempDir()
	w.server = filepath.Join(w.root, "server.git")
	w.client = filepath.Join(w.root, "client")
	w.git = func(dir string, args ...string) string { return pbRun(t, dir, args...) }
	w.git(w.root, "init", "--bare", "-b", "master", w.server)
	w.git(w.root, "clone", "-q", w.server, w.client)
	pbWrite(w.t, w.client, "a.txt", "one")
	w.git(w.client, "add", "a.txt")
	w.git(w.client, "commit", "-m", "seed A")
	w.git(w.client, "push", "-q", "origin", "master")
	w.seedCommitA = w.git(w.client, "rev-parse", "HEAD")
	return w
}

// pbAdoptAndDeleteServerBranch merges the given client branch's commits into
// the bare server's master (via a work clone), then removes the branch ref
// from the server — the adoption-chain action: merge-to-master THEN delete.
func (w *pbWorld) pbAdoptAndDeleteServerBranch(branch string) {
	ad := filepath.Join(w.root, "adopt-"+branch)
	w.git(w.root, "clone", "-q", w.server, ad)
	w.git(ad, "config", "user.email", "t@t")
	w.git(ad, "config", "user.name", "t")
	w.git(ad, "fetch", "-q", w.client, branch+":"+branch)
	w.git(ad, "merge", "-q", "--no-edit", "--no-ff", branch)
	w.git(ad, "push", "-q", w.server, "master:master")
	// delete the server-side ref (adoption deletes the deliverable after merge)
	w.git(w.server, "update-ref", "-d", "refs/heads/"+branch)
}

// serverRefExists reports whether refs/heads/<branch> exists on the bare
// server (used to prove a stale ref is NOT re-created after push-back).
func (w *pbWorld) serverRefExists(branch string) bool {
	out := pbRun(w.t, w.root, "ls-remote", w.server, "refs/heads/"+branch)
	return strings.TrimSpace(out) != ""
}

// --- acceptance cases ---

// TestPushbackSkippedMergedStaleBranchNotResurrected is requirement (a):
// a branch whose tip is already an ancestor of server master (the server
// adopted + deleted it) is NOT pushed — the deleted server ref stays gone.
func TestPushbackSkippedMergedStaleBranchNotResurrected(t *testing.T) {
	w := newPBWorld(t)
	c := w.client
	// 1. a stale branch rooted at the seed (tip IS in server master's history).
	//    Server master = seed A (only the seed was pushed); "feat/stale" branches
	//    from A with no extra work → its tip == A == ancestor of server master.
	w.git(c, "checkout", "-q", "-b", "feat/stale-merged", w.seedCommitA)
	// 2. advance the server master beyond A (a client master commit B pushed),
	//    so A stays a strict ancestor of server master (the merged-then-deleted
	//    stale-branch shape: tip inside current master, ref gone).
	w.git(c, "checkout", "-q", "master")
	pbWrite(w.t, c, "masterB.txt", "master B")
	w.git(c, "add", "masterB.txt")
	w.git(c, "commit", "-q", "-m", "master B")
	w.git(c, "push", "-q", w.server, "master:master")
	// (feat/stale-merged has no server ref; simulate the pre-adoption push +
	// adopt-delete so it reproduces the incident setup: server master contains
	// the tip, the ref is absent.)
	w.git(c, "push", "-q", w.server, "feat/stale-merged:feat/stale-merged")
	// adopt-delete on the server (merge into master + remove ref)
	w.pbAdoptAndDeleteServerBranch("feat/stale-merged")

	// 3. a "current task" DELIVERABLE branch: new work on top of the peer's
	//    CURRENT master (commit B), so it FAST-FORWARDS — the shape the
	//    close-out chain's alignment gate produces. A deliverable that is
	//    merely BEHIND master is DELIVERED too and reported as unaligned
	//    (TestPushbackBehindMasterBranchIsDeliveredWithTheFfFact): the fact is
	//    reported, the peer's merger owns the landing call.
	w.git(c, "checkout", "-q", "-b", "feat/current-task")
	pbWrite(w.t, c, "current.txt", "current work")
	w.git(c, "add", "current.txt")
	w.git(c, "commit", "-q", "-m", "current deliverable")

	// Run the client push-back at the current master state; the only unmerged
	// branch left NOW on the client (feat/current-task and feat/stale-merged both
	// exist; feat/current-task is truly unmerged, feat/stale-merged is merged).
	// We want current-task NOT to be resurrected-skipped here — but the filter
	// must skip feat/stale-merged. Just move the worktree off feat/stale-merged
	// back to master for a clean plan run.
	w.git(c, "checkout", "-q", "master")

	ctx := context.Background()
	flush := gitPushbackPlan(ctx, w.client, w.server)

	// The flush MUST have run (not aborted on a server-master fetch error),
	// otherwise an empty Skipped+Failed would vacuously "not resurrect".
	if len(flush.Failed) != 0 || !pushbackContains(flush.Skipped, "feat/stale-merged") {
		t.Fatalf("push-back must run and report the merged branch in Skipped: failed=%v skipped=%v pushed=%v",
			flush.Failed, flush.Skipped, flush.Pushed)
	}
	// (a) the adopted branch is skipped, not pushed; server ref NOT re-created.
	if pushbackContains(flush.Pushed, "feat/stale-merged") {
		t.Fatalf("adopted-merged branch must NOT be pushed: %+v", flush.Pushed)
	}
	if w.serverRefExists("feat/stale-merged") {
		t.Fatalf("server ref feat/stale-merged was RESURRECTED by the push-back")
	}
	// (b) the FF-able deliverable IS pushed + server ref. (An unmerged branch
	// that is not ff-able is REFUSED instead — see
	// TestPushbackRefusesBranchBehindMasterRatherThanDeliveringIt.)
	if !pushbackContains(flush.Pushed, "feat/current-task") {
		t.Fatalf("an ff-able deliverable must be pushed: %+v (failed=%v)", flush.Pushed, flush.Failed)
	}
	if !w.serverRefExists("feat/current-task") {
		t.Fatalf("current-task branch absent on server after push-back")
	}
}

// TestPushbackNoBranchPushesNothing is requirement (d): a client with only
// master (no non-integration local branches) pushes nothing.
func TestPushbackNoBranchPushesNothing(t *testing.T) {
	w := newPBWorld(t) // client has only master, master == seed
	ctx := context.Background()
	flush := gitPushbackPlan(ctx, w.client, w.server)
	if len(flush.Pushed) != 0 || len(flush.Failed) != 0 {
		t.Fatalf("no-branch client must push nothing: pushed=%v failed=%v", flush.Pushed, flush.Failed)
	}
}

// TestPushbackRefusesCollidingBranchRatherThanForking replaces the former
// collision-rename test: the lane no longer forks.
//
// The old behaviour retried a non-fast-forward push under `<name>-<HHMMSS>`
// and pushed the fork. The rename does not resolve the divergence — every fork
// is a NEW branch on the peer that the peer's `merge --ff-only` landing cannot
// take either, so the peer's merger parks it and files another rework row
// (measured 2026-09-17: one topic reached the peer as five branches on one tip,
// devdoc/rework-ff-convergence.md).
//
// The contract asserted here, in git's OWN refs (never a report alone):
//
//	① the branch is REFUSED and REPORTED under its own name — never a silent
//	   skip and never a bare failure line (Rule 15);
//	② no `<name>-<timestamp>` ref exists on the peer — the workspace is not
//	   multiplied;
//	③ the peer's own same-named branch is untouched.
func TestPushbackRefusesCollidingBranchRatherThanForking(t *testing.T) {
	w := newPBWorld(t)
	c := w.client
	// Server independently created feat/dup with its OWN commit.
	dup := filepath.Join(w.root, "dup")
	w.git(w.root, "clone", "-q", w.server, dup)
	w.git(dup, "config", "user.email", "t@t")
	w.git(dup, "config", "user.name", "t")
	w.git(dup, "checkout", "-q", "-b", "feat/dup")
	pbWrite(w.t, dup, "dup-server.txt", "server-side dup")
	w.git(dup, "add", "dup-server.txt")
	w.git(dup, "commit", "-q", "-m", "server dup work")
	w.git(dup, "push", "-q", w.server, "feat/dup:feat/dup")
	dupTip := w.git(dup, "rev-parse", "HEAD")
	os.RemoveAll(dup)

	// Client independently created the SAME name with DIFFERENT work — both
	// sides forked from the seed, so the client tip does not contain the
	// server's tip and the push can only be non-fast-forward.
	w.git(c, "checkout", "-q", "-b", "feat/dup")
	pbWrite(w.t, c, "dup-client.txt", "client-side dup")
	w.git(c, "add", "dup-client.txt")
	w.git(c, "commit", "-q", "-m", "client dup work")
	w.git(c, "checkout", "-q", "master")

	ctx := context.Background()
	flush := gitPushbackPlan(ctx, w.client, w.server)

	// ① REFUSED, reported under its own name, with the remedy on the line.
	if len(flush.Failed) != 1 {
		t.Fatalf("a non-ff push must be REPORTED as failed: failed=%v pushed=%v", flush.Failed, flush.Pushed)
	}
	if !strings.HasPrefix(flush.Failed[0], "feat/dup: ") {
		t.Fatalf("the refusal must be keyed by the branch's own name, got %q", flush.Failed[0])
	}
	if !strings.Contains(flush.Failed[0], "re-deliver") {
		t.Fatalf("the refusal must carry the remedy (what to do instead), got %q", flush.Failed[0])
	}
	if len(flush.Pushed) != 0 {
		t.Fatalf("nothing may be pushed when the only branch is non-ff: pushed=%v", flush.Pushed)
	}
	// ② NOT multiplied: no timestamped fork was minted on the peer.
	if out := pbRun(t, w.root, "ls-remote", w.server, "refs/heads/feat/dup-*"); strings.TrimSpace(out) != "" {
		t.Fatalf("a rename-fork was minted on the peer (the loop this gate closes):\n%s", out)
	}
	// ③ The peer's own feat/dup is untouched — same tip, still there.
	if got := w.git(w.root, "ls-remote", w.server, "refs/heads/feat/dup"); !strings.Contains(got, dupTip) {
		t.Fatalf("the peer's own feat/dup was clobbered: got %q, want tip %s", got, dupTip)
	}
}

// TestPushbackBehindMasterBranchIsDeliveredWithTheFfFact replaces the former
// "refuse a branch behind master" contract: the alignment question is REPORTED,
// not enforced (see gitPushbackPlan). The shape is the one the refusal used to
// strike — a branch carrying real work, cut from a base the peer has moved past.
//
// What is asserted, in git's own refs:
//
//	① the branch REACHES the peer (the peer's merger owns the landing call);
//	② its Pushed entry states the fact — it does not contain the peer's master;
//	③ the machine-readable halves separate it from an ff-able sibling;
//	④ the lane still does not fork, and the peer's ref is at OUR tip.
func TestPushbackBehindMasterBranchIsDeliveredWithTheFfFact(t *testing.T) {
	w := newPBWorld(t)
	c := w.client

	// The peer's master moves ahead of the seed (an unrelated commit).
	w.git(c, "checkout", "-q", "master")
	pbWrite(w.t, c, "peer.txt", "peer work")
	w.git(c, "add", "peer.txt")
	w.git(c, "commit", "-q", "-m", "peer master advances")
	w.git(c, "push", "-q", w.server, "master:master")
	peerTip := w.git(c, "rev-parse", "master")

	// A branch that carries its own work but was started from the OLD master:
	// it contains new commits and NOT the peer's master tip.
	w.git(c, "checkout", "-q", "-b", "rework/behind", w.seedCommitA)
	pbWrite(w.t, c, "mine.txt", "my work")
	w.git(c, "add", "mine.txt")
	w.git(c, "commit", "-q", "-m", "my work on the stale base")
	behindTip := w.git(c, "rev-parse", "HEAD")
	w.git(c, "checkout", "-q", "master")

	// Precondition, measured: the branch genuinely does not contain the peer's
	// master (the exact question the old gate answered "no" to).
	if _, code := pbGitMaybe(t, c, "merge-base", "--is-ancestor", peerTip, behindTip); code != 1 {
		t.Fatalf("fixture: the branch must not contain the peer's master (exit=%d, want 1)", code)
	}
	if w.serverRefExists("rework/behind") {
		t.Fatalf("fixture: the branch must not exist on the peer yet")
	}

	ctx := context.Background()
	flush := gitPushbackPlan(ctx, w.client, w.server)

	// ① delivered, not refused.
	if len(flush.Failed) != 0 {
		t.Fatalf("an unaligned branch must not be refused — it is a FACT, not a verdict: failed=%v", flush.Failed)
	}
	if !pbHasPrefix(flush.Pushed, "rework/behind") {
		t.Fatalf("the branch must be delivered: pushed=%v", flush.Pushed)
	}
	if got := pbRefSha(w, "rework/behind"); got != behindTip {
		t.Fatalf("the peer's ref must hold our tip: got %q want %q", got, behindTip)
	}
	// ② the fact is on the entry, with the peer tip it is behind.
	entry := pbEntryFor(flush.Pushed, "rework/behind")
	if !strings.Contains(entry, "does not contain the peer's master") ||
		!strings.Contains(entry, shortSha(peerTip)) {
		t.Errorf("the entry must state the alignment fact and the peer tip %s: %q", shortSha(peerTip), entry)
	}
	// ③ the machine-readable half names it, and NOT as fast-forwardable.
	ff, notFf := pbFfFactSplit(flush)
	if !pbContains(notFf, "rework/behind") {
		t.Errorf("the not-fast-forwardable half must name the branch: %v", notFf)
	}
	if pbContains(ff, "rework/behind") {
		t.Errorf("the branch does not contain the peer's master — it cannot be in the ff-able half: %v", ff)
	}
	// ④ no fork was minted in its place.
	if out := pbRun(t, w.root, "ls-remote", w.server, "refs/heads/rework/*"); strings.Count(out, "\n") != 0 {
		t.Fatalf("a fork of the branch was minted:\n%s", out)
	}

	// THE REMEDY THE PEER'S MERGER NEEDS, executed: after aligning with the
	// peer's master the branch becomes ff-able, and the report says so — the
	// fact flips on the same flush, which is what makes it actionable.
	w.git(c, "checkout", "-q", "rework/behind")
	w.git(c, "merge", "-q", "--no-edit", "master")
	w.git(c, "checkout", "-q", "master")
	flush2 := gitPushbackPlan(ctx, w.client, w.server)
	ff2, notFf2 := pbFfFactSplit(flush2)
	if !pbContains(ff2, "rework/behind") {
		t.Fatalf("after aligning with the peer's master the branch must be reported fast-forwardable: ffable=%v not_ffable=%v pushed=%v failed=%v",
			ff2, notFf2, flush2.Pushed, flush2.Failed)
	}
	if pbContains(notFf2, "rework/behind") {
		t.Fatalf("an aligned branch must not be reported not-fast-forwardable: %v", notFf2)
	}
	if !w.serverRefExists("rework/behind") {
		t.Fatalf("aligned branch absent at the peer after the push-back")
	}
}

func pushbackContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
