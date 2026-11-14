package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitSyncFixture builds a bare "server" repo + a client work repo, commits
// a branch on the client and returns both paths (the finish-cleanup test
// fixture contract: real git, real push semantics — mocks cannot prove
// up-to-date idempotency or --ff-only refusal).
func gitSyncFixture(t *testing.T) (client, server string) {
	t.Helper()
	root := t.TempDir()
	server = filepath.Join(root, "server.git")
	client = filepath.Join(root, "client")
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), dir, err, out)
		}
	}
	run(root, "init", "--bare", "-b", "master", server)
	run(root, "clone", server, client)
	run(client, "config", "user.email", "t@t")
	run(client, "config", "user.name", "t")
	os.WriteFile(filepath.Join(client, "a.txt"), []byte("one"), 0o644)
	run(client, "add", "a.txt")
	run(client, "commit", "-m", "seed commit")
	run(client, "push", "origin", "master")
	// a deliverable branch ahead of master
	run(client, "checkout", "-b", "feat/gitchan-x")
	os.WriteFile(filepath.Join(client, "b.txt"), []byte("two"), 0o644)
	run(client, "add", "b.txt")
	run(client, "commit", "-m", "feat work")
	run(client, "checkout", "master")
	return client, server
}

func TestGitSyncClientPassPushAndPull(t *testing.T) {
	client, server := gitSyncFixture(t)
	rep := GitSyncClientPass(client, server)
	if rep.Skipped {
		t.Fatalf("unexpected skip: %s", rep.SkipReason)
	}
	if len(rep.BranchesPushed) != 1 || rep.BranchesPushed[0] != "feat/gitchan-x" {
		t.Fatalf("pushed = %v, want [feat/gitchan-x]", rep.BranchesPushed)
	}
	if len(rep.PushFailed) != 0 {
		t.Fatalf("push_failed = %v", rep.PushFailed)
	}
	// The step renders its OWN no-op sentence now (gitSyncPullUpToDateDetail),
	// so this assertion no longer has to accept a localized spelling of git's
	// prose as well as an English one.
	if rep.PullFF != gitSyncPullUpToDateDetail {
		t.Fatalf("pull = %q, want %q", rep.PullFF, gitSyncPullUpToDateDetail)
	}
	// idempotent re-tick: the same pass again pushes nothing new (git
	// up-to-date detection = exit 0) and reports no failure.
	rep2 := GitSyncClientPass(client, server)
	if len(rep2.PushFailed) != 0 {
		t.Fatalf("re-tick push_failed = %v (idempotency broken)", rep2.PushFailed)
	}
	if len(rep2.BranchesPushed) != 1 {
		t.Fatalf("re-tick pushed = %v, want the same branch re-listed as no-op success", rep2.BranchesPushed)
	}
}

func TestGitSyncClientPassNoPeerSkips(t *testing.T) {
	rep := GitSyncClientPass("", "")
	if !rep.Skipped || rep.Role != "client" {
		t.Fatalf("want skipped client pass, got %+v", rep)
	}
}

func TestGitSyncClientPassDivergedMasterRefused(t *testing.T) {
	client, server := gitSyncFixture(t)
	// Server master advances (the "server merged something" case): the
	// client's ff-only pull must REFUSE (diverged), be reported, and NOT
	// fail the pass — fail-soft per step.
	srun := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = server
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v on server: %v: %s", args, err, out)
		}
	}
	srun("clone", server, filepath.Join(filepath.Dir(server), "seed2"))
	// advance the bare master via a temp clone push
	tmp := filepath.Join(filepath.Dir(server), "advancer")
	cmd := exec.Command("git", "clone", server, tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone advancer: %v: %s", err, out)
	}
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), dir, err, out)
		}
	}
	os.WriteFile(filepath.Join(tmp, "c.txt"), []byte("server-side"), 0o644)
	run(tmp, "add", "c.txt")
	run(tmp, "commit", "-m", "server advance")
	run(tmp, "push", "origin", "master")
	os.RemoveAll(tmp)
	_ = srun

	// Client diverges: a local commit on master that the server lacks.
	os.WriteFile(filepath.Join(client, "d.txt"), []byte("client-side"), 0o644)
	run(client, "add", "d.txt")
	run(client, "commit", "-m", "client divergence")

	rep := GitSyncClientPass(client, server)
	if rep.Skipped {
		t.Fatalf("unexpected skip: %s", rep.SkipReason)
	}
	if !strings.Contains(rep.PullFF, "skipped") {
		t.Fatalf("pull should be reported skipped (diverged), got %q", rep.PullFF)
	}
	for _, st := range rep.Steps {
		if st.Step == "pull-master" && st.OK {
			t.Fatalf("diverged pull must not be OK: %+v", rep.Steps)
		}
	}
}

func TestGitSyncPushLocalBranchesExcludesMaster(t *testing.T) {
	client, server := gitSyncFixture(t)
	pushed, failed := GitSyncPushLocalBranches(client, server)
	if len(failed) != 0 {
		t.Fatalf("failed = %v", failed)
	}
	for _, b := range pushed {
		if b == "master" || b == "main" {
			t.Fatalf("integration branch %q pushed — asymmetry violated", b)
		}
	}
}

// TestGitSyncClientPassPrunesThePeersDeletedBranches pins the deletion
// contract's client half: once the PEER has deleted a branch (its adoption
// chain does `branch -r -d` after landing it) and the client's own close-out
// has reaped the local ref (step ④ — the branch has been delivered), the
// client's MIRROR of the peer's branch must go too.
//
// Measured 2026-09-26 on HP: `git ls-remote --heads peer1` = 3 refs while
// `git branch -a` printed 187 — 184 local ghost refs, the oldest from
// 2026-09-16. The operator symptom is "my branches stay in the repo". Two
// independent causes were measured, and the fixture reproduces BOTH:
//
//  1. the client arm never asked for a prune at all; and
//  2. the pass fetches the sync target ("<alias>:<path>"), and fetching a
//     URL updates NO remote-tracking ref — the refspec lives on the REMOTE,
//     so the prune has to be handed the remote's NAME. The repo pruned 182
//     refs the moment the fetch named the remote instead of its URL.
//
// The remote below is therefore a NAMED one ("peer"), which is the shape
// sync_peers declares; a bare-path target is covered by the fallback in
// gitSyncPruneSource.
func TestGitSyncClientPassPrunesThePeersDeletedBranches(t *testing.T) {
	client, server := gitSyncFixture(t)
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), dir, err, out)
		}
	}
	// The peer as sync_peers declares it: a configured remote NAME (the
	// pass's own target is "<name>:<path>", gitSyncServerTarget's shape).
	run(client, "remote", "add", "peer", server)
	target := "peer" + sshPeerAliasTargetSeparator + server
	mirror := "refs/remotes/peer/feat/pruned-x"

	// A client-visible branch the peer adopts (fetches), then DELETES.
	run(client, "checkout", "-b", "feat/pruned-x")
	os.WriteFile(filepath.Join(client, "c.txt"), []byte("three"), 0o644)
	run(client, "add", "c.txt")
	run(client, "commit", "-m", "feat work 2")
	run(client, "checkout", "master")
	run(client, "push", server, "feat/pruned-x")
	// A pass mirrors it (the push above mirrors it too — either way it is here).
	GitSyncClientPass(client, target)
	if out, err := exec.Command("git", "-C", client, "rev-parse", "--verify", mirror).Output(); err != nil {
		t.Fatalf("precondition: the client never mirrored the branch (%v: %s)", err, out)
	}
	// The close-out's sequence: the peer lands + reaps the branch (its ③a) and
	// the client reaps the LOCAL deliverable ref (step ④). The mirror is what
	// is left, and it is the ref with no deleter.
	run(server, "branch", "-D", "feat/pruned-x")
	run(client, "branch", "-D", "feat/pruned-x")
	rep := GitSyncClientPass(client, target)
	if out, err := exec.Command("git", "-C", client, "rev-parse", "--verify", mirror).CombinedOutput(); err == nil {
		t.Fatalf("the peer deleted %s and the client reaped its local ref, but the mirror survived (%s) — the pass must fetch --prune from the NAMED remote", mirror, out)
	}
	// And the REPORT must say so: git writes its "[deleted]" lines to STDERR
	// while gitSyncRun captures stdout, so a marker-count printed "0 pruned"
	// on the very pass that reaped 182 refs (measured). The count is surveyed
	// from the repo's own ref state instead.
	detail := ""
	for _, st := range rep.Steps {
		if st.Step == "fetch-prune" {
			detail = st.Detail
		}
	}
	if detail != "1"+gitSyncPruneDetailSuffix {
		t.Fatalf("fetch-prune detail = %q, want %q (one ghost reaped)", detail, "1"+gitSyncPruneDetailSuffix)
	}
}

// gitSyncStep3Fixture builds the shape that made step ③ a no-op (measured
// 2026-10-12, the incident this test is the regression guard for):
//
//	peer.git     a bare "server" whose master ADVANCES after the checkout exists
//	main/        a clone of it, HOLDING refs/heads/master (the holder worktree)
//	wt/          a LINKED worktree of the same repository, on task/step3-x —
//	             a branch with NO upstream, which is every worker's ground
//	adv/         a second clone, used to push new commits to the peer
//
// It returns the three directories plus the peer's tip.
func gitSyncStep3Fixture(t *testing.T) (main, wt, adv, peer, peerTip string) {
	t.Helper()
	root := t.TempDir()
	peer = filepath.Join(root, "peer.git")
	main = filepath.Join(root, "main")
	wt = filepath.Join(root, "wt")
	adv = filepath.Join(root, "adv")

	run := func(dir string, args ...string) string {
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
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run(root, "init", "-q", "--bare", "-b", IntegrationBranchName, peer)
	run(root, "clone", "-q", peer, main)
	run(main, "config", "user.email", "t@t")
	run(main, "config", "user.name", "t")
	write(filepath.Join(main, "a.txt"), "one")
	run(main, "add", "a.txt")
	run(main, "commit", "-m", "seed commit")
	run(main, "push", "origin", IntegrationBranchName)
	run(main, "worktree", "add", "-q", "-b", "task/step3-x", wt)

	// The peer advances — the "the server merged something" case.
	run(root, "clone", "-q", peer, adv)
	run(adv, "config", "user.email", "t@t")
	run(adv, "config", "user.name", "t")
	write(filepath.Join(adv, "c.txt"), "server-side")
	run(adv, "add", "c.txt")
	run(adv, "commit", "-m", "peer advance")
	run(adv, "push", "origin", IntegrationBranchName)

	peerTip = strings.TrimSpace(strings.SplitN(
		run(wt, "ls-remote", "--refs", peer, refsHeadsPrefix+IntegrationBranchName), "\t", 2)[0])
	return main, wt, adv, peer, peerTip
}

// TestGitSyncClientPassAdvancesTheIntegrationRefFromAnyCwd is the regression
// guard for step ③'s shape (Rule 5; the defect measured 2026-10-12 on the
// real host: refs/heads/master moved 0 times while the peer's remote ref moved
// 4, leaving the local integration ref 70 commits behind).
//
// THE DEFECT: the step ran `git pull --ff-only` with NO refspec, so the merge
// leg fast-forwarded whatever branch the CALLING worktree had checked out. In a
// linked worktree that branch is task/<id> (no upstream): git exits 1 with
// "There is no tracking information for the current branch" and the local
// integration ref never moves — while the fetch leg and the prune step make the
// pass look alive.
//
// WHY THE OLD SHAPE IS PINNED FIRST (the red half): the test does not merely
// assert the new behaviour, it asserts the OLD command's failure on the SAME
// fixture, so the guard still fails if the refspec-less pull comes back and a
// future change makes the pass "work" by some other accident.
func TestGitSyncClientPassAdvancesTheIntegrationRefFromAnyCwd(t *testing.T) {
	main, wt, _, peer, peerTip := gitSyncStep3Fixture(t)
	local := refsHeadsPrefix + IntegrationBranchName

	revParse := func(dir string, args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir, gitPeerRevParseVerb}, args...)...)
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	porcelain := func(dir string) string {
		out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
		if err != nil {
			t.Fatalf("status --porcelain in %s: %v", dir, err)
		}
		return string(out)
	}

	// PRECONDITIONS — the fixture must actually reproduce the incident, or the
	// assertions below would pass for the wrong reason.
	if got := revParse(wt, gitSyncAbbrevRefFlag, gitSyncHeadRef); got != "task/step3-x" {
		t.Fatalf("precondition: the caller worktree is on %q, want task/step3-x", got)
	}
	before := revParse(wt, local)
	if before == "" || before == peerTip {
		t.Fatalf("precondition: local %s = %q, peer tip = %q — they must differ", local, before, peerTip)
	}

	// ── RED HALF: the old shape, on this very fixture ────────────────────────
	cmd := exec.Command("git", "-C", wt, "pull", "--ff-only")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("precondition: `git pull --ff-only` in a linked worktree with no upstream "+
			"SUCCEEDED (%s) — the fixture no longer reproduces the incident", strings.TrimSpace(string(out)))
	}
	if after := revParse(wt, local); after != before {
		t.Fatalf("precondition: the refspec-less pull moved %s (%s → %s) — that is the defect, not the fixture", local, before, after)
	}

	// ── GREEN HALF: the pass, called with the caller inside that worktree ────
	rep := GitSyncClientPass(wt, peer)
	if rep.Skipped {
		t.Fatalf("the pass skipped: %s", rep.SkipReason)
	}
	stepOK, stepDetail := false, ""
	for _, st := range rep.Steps {
		if st.Step == gitSyncStepPullBranchName {
			stepOK, stepDetail = st.OK, st.Detail
		}
	}
	if !stepOK {
		t.Fatalf("step %q reported a failure from a worktree that is not the holder: %q — "+
			"the whole point of the fix is that the pass finds the branch's holder itself",
			gitSyncStepPullBranchName, stepDetail)
	}
	// THE ASSERTION THE INCIDENT IS ABOUT.
	if after := revParse(wt, local); after != peerTip {
		t.Fatalf("local %s = %s after the pass, want the peer tip %s — the pass must advance the "+
			"INTEGRATION REF, not the caller's branch", local, after, peerTip)
	}
	if !strings.Contains(rep.PullFF, gitSyncFfDetailPrefix) {
		t.Fatalf("pass line reports pull=%q, want it to name the new tip (%q)", rep.PullFF, gitSyncFfDetailPrefix)
	}
	if !strings.Contains(rep.PullFF, peerTip) {
		t.Fatalf("pass line reports pull=%q, want it to carry the tip %s", rep.PullFF, peerTip)
	}
	// The holder's index and working tree moved WITH the ref: the merge ran
	// there, so the main checkout is at the new tip and clean — the property a
	// bare `update-ref` write would have destroyed (the shared-ref incident).
	if got := revParse(main, "HEAD"); got != peerTip {
		t.Fatalf("the holder checkout's HEAD = %s, want %s (the merge must run in the worktree that holds the branch)", got, peerTip)
	}
	if got := porcelain(main); got != "" {
		t.Fatalf("the holder checkout is DIRTY after the step:\n%s", got)
	}
	// The caller's own ground is untouched: it was never the holder, and the
	// pass must not have switched it.
	if got := revParse(wt, gitSyncAbbrevRefFlag, gitSyncHeadRef); got != "task/step3-x" {
		t.Fatalf("the caller worktree moved to %q — the pass must not switch a run's ground", got)
	}

	// A second pass is idempotent and reports up-to-date (the peer has not moved).
	rep2 := GitSyncClientPass(wt, peer)
	if rep2.PullFF != gitSyncPullUpToDateDetail {
		t.Fatalf("re-tick pull = %q, want %q", rep2.PullFF, gitSyncPullUpToDateDetail)
	}
}

// TestGitSyncClientPassCreatesTheIntegrationRefWhenNobodyHoldsIt pins the other
// half of the holder predicate: with no worktree holding the branch, git accepts
// the plain (unforced) refspec fetch and the step CREATES the ref — the route for
// a host whose checkout sits on some other branch.
func TestGitSyncClientPassCreatesTheIntegrationRefWhenNobodyHoldsIt(t *testing.T) {
	main, _, _, peer, peerTip := gitSyncStep3Fixture(t)
	local := refsHeadsPrefix + IntegrationBranchName

	// Nobody holds the branch: detach the only checkout that does.
	detach := exec.Command("git", "-C", main, "checkout", "-q", "--detach")
	if out, err := detach.CombinedOutput(); err != nil {
		t.Fatalf("detach: %v: %s", err, out)
	}
	// The local ref is BEHIND the peer already (this checkout never fetched the
	// peer's advance — the fixture's seed commit is a root commit, so rewinding
	// it is not even possible). Assert the precondition rather than build it.
	if out, err := exec.Command("git", "-C", main, "rev-parse", local).Output(); err != nil || strings.TrimSpace(string(out)) == peerTip {
		t.Fatalf("precondition: local %s must resolve to something other than the peer tip %s (err=%v)", local, peerTip, err)
	}

	rep := GitSyncClientPass(main, peer)
	if rep.Skipped {
		t.Fatalf("the pass skipped: %s", rep.SkipReason)
	}
	for _, st := range rep.Steps {
		if st.Step == gitSyncStepPullBranchName && !st.OK {
			t.Fatalf("step %q failed with nobody holding the branch: %s", gitSyncStepPullBranchName, st.Detail)
		}
	}
	out, err := exec.Command("git", "-C", main, "rev-parse", local).Output()
	if err != nil {
		t.Fatalf("the integration ref does not resolve after the pass: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != peerTip {
		t.Fatalf("local %s = %s, want the peer tip %s", local, got, peerTip)
	}
}

// TestGitSyncClientPassNeverReportsSuccessWhileTheIntegrationRefStandsStill is
// the SECOND arm of the same defect, and the one Rule 15 is about: a caller
// worktree whose own branch HAS an upstream. There the old refspec-less
// `git pull --ff-only` SUCCEEDS — it fast-forwards the CALLER's branch — and the
// pass prints a success while the integration ref stands still. Nothing in the
// report said so, and that is why the frozen ref went unnoticed for as long as it
// did (measured 2026-10-12: refs/heads/master moved 0 times while the peer's
// remote ref moved 4).
//
// The assertion is a PROPERTY, not a wording: after a pass that did not report a
// failure, the local integration ref must have moved to the peer's tip.
func TestGitSyncClientPassNeverReportsSuccessWhileTheIntegrationRefStandsStill(t *testing.T) {
	_, wt, _, peer, peerTip := gitSyncStep3Fixture(t)
	local := refsHeadsPrefix + IntegrationBranchName

	// Give the caller's own branch an UPSTREAM, so the fetch leg of a
	// refspec-less pull has somewhere to merge: this is the arm that reported
	// success. (Measured: `git pull --ff-only` in a linked worktree whose branch
	// has an upstream exits 0 and updates THAT branch.)
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = wt
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), wt, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// The clone's configured remote ("origin") IS the peer bare repo, so the
	// branch is published there and then FETCHED back to create the
	// remote-tracking ref the upstream is set to (a push alone creates no
	// remote-tracking ref).
	run("push", "-q", "origin", "task/step3-x")
	run("fetch", "-q", "origin")
	run("branch", "--set-upstream-to=origin/task/step3-x", "task/step3-x")

	rep := GitSyncClientPass(wt, peer)
	for _, st := range rep.Steps {
		if st.Step == gitSyncStepPullBranchName && !st.OK {
			t.Fatalf("step %q failed on a caller branch that has an upstream: %s", gitSyncStepPullBranchName, st.Detail)
		}
	}
	out, err := exec.Command("git", "-C", wt, "rev-parse", local).Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", local, err)
	}
	if got := strings.TrimSpace(string(out)); got != peerTip {
		t.Fatalf("the pass reported step %q OK (pull=%q) while %s = %s, not the peer tip %s — "+
			"a pass that says it synchronised must have moved the INTEGRATION ref, "+
			"and a caller-side branch is not that ref",
			gitSyncStepPullBranchName, rep.PullFF, local, got, peerTip)
	}
}
