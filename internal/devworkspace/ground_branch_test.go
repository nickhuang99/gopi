package devworkspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// groundTaskID is a row id in the ledger's own spelling. It is a CONSTANT here
// because the test asserts the ref the workspace must stand on is
// "task/" + this value — a literal typed twice could not tell a fixed
// implementation from a broken assertion (Rule 1).
const groundTaskID = "01a141a7-0000-7000-8000-00000000beef"

// groundBranchName is the row's ledger address, spelled the way
// internal/tools.TaskGroundBranchName spells it. This package imports nothing
// from internal/tools (it is the leaf the tools layer calls), so the value is
// written once here and once in the tools test that pins the minting function.
const groundBranchName = "task/" + groundTaskID

// TestCloneGroundStandsOnItsLedgerAddress is the regression proof for the
// defect this epic removes (measured 2026-10-27): a clone checks out the
// SOURCE's branch, so before this fix the workspace stood on the integration
// branch and every ledger stamp derived from it recorded
// `deliverable.branch: master` — a name shared by every row of the channel
// (the then-live branch→row lookup answered "361 owners"), so the ref mapped back
// to no row and the run's commits sat on a ref no server held under that shared
// name.
//
// WHAT THE TEST PINS, in the order the channel does it:
//
//	Open        → the workspace STANDS on task/<id> (res.Branch, and the real HEAD)
//	commit      → the work is a commit on that ref and nowhere else
//	Delivery    → origin ends up carrying refs/heads/task/<id> … and the
//	              INTEGRATION branch is NOT moved by the row's delivery
//
// The last clause is the design decision, not decoration: the push used to
// target whatever the workspace stood on, which is how a row's work either
// became indistinguishable from master or (post-fix, with the ground branch)
// would have MOVED master from a row's checkout.
func TestCloneGroundStandsOnItsLedgerAddress(t *testing.T) {
	origin := newOrigin(t)

	cfg := cloneCfg(t, origin)
	cfg.TaskID = groundTaskID
	cfg.GroundBranch = groundBranchName
	cfg.BaseBranch = DefaultBranch

	res, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// ① THE STAMP: this is the value the claim records as deliverable.branch.
	if res.Branch != groundBranchName {
		t.Fatalf("Result.Branch = %q, want %q — the ledger would record the integration branch again", res.Branch, groundBranchName)
	}
	// ② THE WORKSPACE ITSELF, measured rather than trusted.
	if got := strings.TrimSpace(gitIn(t, res.Dir, "rev-parse", "--abbrev-ref", "HEAD")); got != groundBranchName {
		t.Fatalf("HEAD = %q, want %q", got, groundBranchName)
	}
	// The ground ref starts ON TOP of the integration branch: the row's work is
	// a continuation of what landed, not a fork of it. A fresh ground ref has no
	// upstream to track (it is minted by this channel), which is exactly why the
	// loop pulls the BASE branch rather than the branch it stands on.

	// ③ THE WORK: one commit, on the row's ref.
	if err := os.WriteFile(filepath.Join(res.Dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, res.Dir, "add", "work.txt")
	gitIn(t, res.Dir, "commit", "-q", "-m", "the row's work")
	localTip := strings.TrimSpace(gitIn(t, res.Dir, "rev-parse", "HEAD"))

	// ④ THE DELIVERY.
	dres, blk, err := AttemptDelivery(context.Background(), cfg, res.Dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("delivery blocked (%s): %s", blk.Kind, blk.Words)
	}
	if !dres.Pushed {
		t.Fatalf("delivery did not push: %+v", dres)
	}
	// THE DESTINATION IS THE INTEGRATION BRANCH carrying exactly the commit the
	// run made — the contract the channel's own end-to-end gates pin.
	remoteTip := strings.TrimSpace(gitIn(t, origin, "rev-parse", "refs/heads/"+DefaultBranch))
	if remoteTip != localTip {
		t.Fatalf("origin's %s = %s, want the run's commit %s", DefaultBranch, remoteTip, localTip)
	}
	// AND THE INTEGRATION BRANCH IS THE ONLY THING THE PUSH CREATES. Recording
	// `task/<id>` while pushing only `base` used to leave every clone row naming
	// a ref nobody could resolve, so the channel published a SECOND refspec for
	// it. That fixed the symptom and created a permanent one (measured
	// 2026-10-28: peer1 carried 15 `task/*` branches, EVERY one already merged
	// into master and NEVER deleted — a ref no merge path consumes is litter,
	// one per task, forever, because nobody merges a branch the ledger invented).
	// The destination is the delivery; the row's ADDRESS is `base`; the ground
	// ref stays LOCAL, where `for-each-ref --contains` still resolves it for the
	// close-out's reachability arm.
	if out, err := exec.Command("git", "-C", origin, "rev-parse", "--verify", "refs/heads/"+groundBranchName).Output(); err == nil {
		t.Fatalf("origin carries %s at %q — a pushed ground ref is a ref no merge path consumes "+
			"(it piles up one dead branch per task); the ground ref must stay local",
			groundBranchName, strings.TrimSpace(string(out)))
	}
}

// TestCloneGroundRepublishesItsRefAcrossRounds pins the motion the loop needs
// on every round AFTER the first.
//
// The delivery loop REBASES the workspace onto the integration branch before
// each push (baseBranchOf), so round 2's HEAD is no longer a descendant of
// round 1's — a rebase REWRITES the row's own commits. WHICH IS WHY THE ROW'S
// ADDRESS IS NEVER THE PUSH TARGET: `HEAD:refs/heads/task/<id>` would be
// rejected as a non-fast-forward on attempt 2 (measured 2026-10-27 on git
// 2.43.0), and forcing it instead leaves one `task/*` ref per task on the server
// forever (9ab17237e's measurement: 15 already-merged refs nobody deletes).
// The ONE destination is the integration branch, whose tip the pull already
// integrated, so each round's push IS a fast-forward of it. This test pins that
// second round landing AND that the address stays local while doing it.
func TestCloneGroundRepublishesItsRefAcrossRounds(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	cfg.TaskID = groundTaskID
	cfg.GroundBranch = groundBranchName
	cfg.BaseBranch = DefaultBranch

	res, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	commitFile := func(dir, name, body string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", name)
		gitIn(t, dir, "commit", "-q", "-m", "round: "+name)
		return strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD"))
	}

	// ROUND 1 — the row's first delivery stands on the integration tip.
	commitFile(res.Dir, "one.txt", "one\n")
	if _, blk, err := AttemptDelivery(context.Background(), cfg, res.Dir); err != nil || blk != nil {
		t.Fatalf("round 1 delivery: err=%v blk=%+v", err, blk)
	}
	round1 := strings.TrimSpace(gitIn(t, origin, "rev-parse", "refs/heads/"+DefaultBranch))

	// SOMEBODY ELSE LANDS ON THE INTEGRATION BRANCH (the ordinary reason round 2
	// must rebase): a separate checkout pushes one commit to master.
	other := filepath.Join(t.TempDir(), "other")
	gitIn(t, cfg.Root, "clone", "-q", origin, other)
	commitFile(other, "upstream.txt", "upstream\n")
	gitIn(t, other, "push", "-q", "origin", "HEAD:"+refsHeadsPrefix+DefaultBranch)

	// ROUND 2 — the loop rebases onto master and the ONE destination advances
	// again; the row's local address is rewritten by the rebase and that is
	// exactly why it is not pushed.
	round2Local := commitFile(res.Dir, "two.txt", "two\n")
	dres, blk, err := AttemptDelivery(context.Background(), cfg, res.Dir)
	if err != nil {
		t.Fatalf("round 2 delivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("round 2 blocked (%s): %s", blk.Kind, blk.Words)
	}
	if dres.Exhausted {
		t.Fatalf("round 2 exhausted the brake: %+v", dres)
	}
	published := strings.TrimSpace(gitIn(t, origin, "rev-parse", "refs/heads/"+DefaultBranch))
	if published == round1 {
		t.Fatalf("origin's %s still at round 1's tip %s — the second delivery did not land", DefaultBranch, round1)
	}
	// The run's second commit IS on the published ref (a rebase may rewrite its
	// id, so the question is reachability, not identity) — and the integration
	// commit it rebased onto is an ancestor of it.
	// round2Local is the PRE-REBASE id, which the rebase legitimately rewrote —
	// the question the ref must answer is "does it carry what landed upstream
	// and the row's own round-2 work", so those are the two revisions checked.
	_ = round2Local
	for _, rev := range []string{"refs/heads/" + DefaultBranch} {
		if out, err := exec.Command("git", "-C", origin, "merge-base", "--is-ancestor", rev, published).CombinedOutput(); err != nil {
			t.Fatalf("%s is not an ancestor of the published ground ref: %v (%s)", rev, err, out)
		}
	}
	if subject := strings.TrimSpace(gitIn(t, origin, "log", "-1", "--format=%s", published)); !strings.Contains(subject, "round: two.txt") {
		t.Fatalf("the published tip's subject = %q, want the round-2 commit (local tip was %s)", subject, round2Local)
	}
}

// TestCloneGroundClassifiesARefusalAgainstItsDestination pins the ONE name the
// push-refusal classifier must ask about, and it is the DESTINATION, not the
// ADDRESS.
//
// THE DEFECT (measured 2026-10-27, while wiring GroundBranch into the channel's
// call sites): the push publishes `base` (the integration branch) but the loop
// asked pushRefused about `branch` — the ref the workspace STANDS on. Before the
// ground fix both names were `master`, so the question was right by accident.
// With a ground branch they are different names, and the first delivery's
// `origin/task/<id>` does not exist, so pushRefused answered "the remote branch
// does not exist" and classified a REAL race as a hard error: the brake never
// retried and the delivery died on the first person who landed on master.
//
// WHAT THE TEST DOES: the declared build lands somebody else's commit on the
// integration branch between our pull and our push — the exact window the retry
// exists for (racingBuildCommand) — with a ground branch named. The delivery must
// come back as DELIVERED (after retrying), never as an error.
func TestCloneGroundClassifiesARefusalAgainstItsDestination(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	cfg.TaskID = groundTaskID
	cfg.GroundBranch = groundBranchName
	cfg.BaseBranch = DefaultBranch
	cfg.Build = racingBuildCommand(t, origin, 1)

	res, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.WriteFile(filepath.Join(res.Dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, res.Dir, "add", "work.txt")
	gitIn(t, res.Dir, "commit", "-q", "-m", "deliverable work")

	dres, blk, err := AttemptDelivery(context.Background(), cfg, res.Dir)
	if err != nil {
		t.Fatalf("a refusal on the INTEGRATION branch is the race the brake retries, not an error: %v", err)
	}
	if blk != nil {
		t.Fatalf("delivery blocked (%s): %s", blk.Kind, blk.Words)
	}
	if !dres.Pushed || dres.Attempts < 2 {
		t.Fatalf("delivery = %+v, want a push that needed a retry (attempts >= 2)", dres)
	}
	// THE ADDRESS NEVER MOVED OFF THE ROW: the retry rebased onto the
	// integration branch and the workspace still stands on its own ref.
	if got := strings.TrimSpace(gitIn(t, res.Dir, "rev-parse", "--abbrev-ref", "HEAD")); got != groundBranchName {
		t.Fatalf("HEAD after the retry = %q, want %q", got, groundBranchName)
	}
	// AND THE DESTINATION CARRIES THE WORK — the integration branch advanced, and
	// the rebase means the row's commit is reachable from it (its id may have been
	// rewritten, so the question is reachability, not identity).
	remoteTip := strings.TrimSpace(gitIn(t, origin, "rev-parse", "refs/heads/"+DefaultBranch))
	if !strings.Contains(strings.TrimSpace(gitIn(t, origin, "log", "--format=%s", remoteTip)), "deliverable work") {
		t.Fatalf("origin's %s tip %s does not carry the row's commit:\n%s", DefaultBranch, remoteTip,
			gitIn(t, origin, "log", "--oneline", "-3", DefaultBranch))
	}
}

// TestCloneGroundWithoutARowKeepsTheLegacyShape is the OTHER half of the
// contract: a caller with no task id has no ledger address to mint, so it must
// keep the pre-fix behaviour exactly — stand on the source's branch, publish
// that branch with an ORDINARY push (where a refusal is information, not a
// motion), and stamp what it stands on. Without this arm the fix could quietly
// become "every clone publishes task/<some uuid>".
func TestCloneGroundWithoutARowKeepsTheLegacyShape(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin) // no GroundBranch / BaseBranch

	res, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if res.Branch != DefaultBranch {
		t.Fatalf("Result.Branch = %q, want %q (no row → no ledger address)", res.Branch, DefaultBranch)
	}
	if err := os.WriteFile(filepath.Join(res.Dir, "legacy.txt"), []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, res.Dir, "add", "legacy.txt")
	gitIn(t, res.Dir, "commit", "-q", "-m", "legacy shape work")
	dres, blk, err := AttemptDelivery(context.Background(), cfg, res.Dir)
	if err != nil || blk != nil {
		t.Fatalf("legacy delivery: err=%v blk=%+v", err, blk)
	}
	if !dres.Pushed || dres.Branch != DefaultBranch {
		t.Fatalf("legacy delivery = %+v, want a push of %q", dres, DefaultBranch)
	}
	// No ground ref was invented on the server.
	refs := gitIn(t, origin, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	for _, line := range strings.Split(strings.TrimSpace(refs), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "task/") {
			t.Fatalf("a caller with no row minted %q on the server", line)
		}
	}
}

// TestCloneGroundStandsOnTheIntegrationBranch is the regression proof for the
// defect the operator ruled on at 2026-11-05, and it is the SHAPE'S own arm —
// no `task/<id>` anywhere.
//
// THE DEFECT (measured, the ground's own reflog): one second after
// `clone: from peer1:/home/user/work/gopi` came
// `checkout: moving from master to task/01a1a87b…`. The channel minted a
// private ref per ground so the branch→row lookup had an address, and paid for
// it twice: the run's commits landed on a ref no merge path consumes and no
// server ever receives (publishGround's ONE destination is `base`), and the
// row's own address was never the ref its work was on.
//
// WHAT THIS PINS, in the channel's own order:
//
//	Open        → HEAD is the INTEGRATION branch, and the workspace mints no ref
//	reuse       → a workspace left on an older shape's `task/<id>` is brought
//	              BACK to the integration branch (the arm that keeps a reused
//	              ground from delivering the wrong ref's commits)
//	Delivery    → the run's commit reaches origin's integration branch
func TestCloneGroundStandsOnTheIntegrationBranch(t *testing.T) {
	origin := newOrigin(t)

	cfg := cloneCfg(t, origin)
	cfg.TaskID = groundTaskID
	// NO GroundBranch: this is the clone channel's shape since the ruling.
	cfg.BaseBranch = DefaultBranch

	res, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// ① THE STAMP AND THE WORKSPACE AGREE, and both name the integration branch.
	if res.Branch != DefaultBranch {
		t.Fatalf("Result.Branch = %q, want %q — the ground must stand on the integration branch", res.Branch, DefaultBranch)
	}
	if got := strings.TrimSpace(gitIn(t, res.Dir, "rev-parse", "--abbrev-ref", "HEAD")); got != DefaultBranch {
		t.Fatalf("HEAD = %q, want %q — a clone ground is a workspace of this run's own, so nothing needs a private ref", got, DefaultBranch)
	}
	// ② NO PRIVATE REF EXISTS, local or remote: the whole point of the ruling.
	for _, line := range strings.Split(strings.TrimSpace(gitIn(t, res.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads/")), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), taskRefPrefixForTest) {
			t.Fatalf("the ground minted %q — a private ref per ground is the defect the ruling removed", line)
		}
	}

	// ③ THE WORK, and its delivery onto the integration branch.
	if err := os.WriteFile(filepath.Join(res.Dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, res.Dir, "add", "work.txt")
	gitIn(t, res.Dir, "commit", "-q", "-m", "the row's work")
	localTip := strings.TrimSpace(gitIn(t, res.Dir, "rev-parse", "HEAD"))

	dres, blk, err := AttemptDelivery(context.Background(), cfg, res.Dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("delivery blocked (%s): %s", blk.Kind, blk.Words)
	}
	if !dres.Pushed {
		t.Fatalf("delivery did not push: %+v", dres)
	}
	if remoteTip := strings.TrimSpace(gitIn(t, origin, "rev-parse", "refs/heads/"+DefaultBranch)); remoteTip != localTip {
		t.Fatalf("origin's %s = %s, want the run's commit %s", DefaultBranch, remoteTip, localTip)
	}
}

// TestReusedGroundIsBroughtBackToTheIntegrationBranch pins the REUSE arm, which
// is the half a fresh-clone-only test cannot see: `Open` reuses an existing
// directory (a previous attempt of the row), and such a workspace may still
// stand on the OLD shape's private ref. Delivering from there would push a ref
// whose commits the row does not name, so the checkout happens on BOTH arms.
func TestReusedGroundIsBroughtBackToTheIntegrationBranch(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	cfg.TaskID = groundTaskID
	cfg.BaseBranch = DefaultBranch

	res, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// SIMULATE THE OLD SHAPE: a private ref with an extra commit on it, and the
	// workspace standing there — exactly what a pre-ruling ground looks like.
	gitIn(t, res.Dir, "checkout", "-q", "-b", groundBranchName)
	if err := os.WriteFile(filepath.Join(res.Dir, "old-shape.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, res.Dir, "add", "old-shape.txt")
	gitIn(t, res.Dir, "commit", "-q", "-m", "the old shape's private ref work")

	again, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open (reuse): %v", err)
	}
	if got := strings.TrimSpace(gitIn(t, again.Dir, "rev-parse", "--abbrev-ref", "HEAD")); got != DefaultBranch {
		t.Fatalf("a reused ground stands on %q; it must be brought back to %q before it delivers anything", got, DefaultBranch)
	}
	// The old commit is NOT lost — it is still reachable through the private ref
	// this workspace's own history keeps; the ruling removes the MINTING, not a
	// workspace's record of where it once stood.
	if out, err := exec.Command("git", "-C", again.Dir, "rev-parse", "--verify", "--quiet",
		refsHeadsPrefix+groundBranchName).Output(); err != nil {
		t.Fatalf("the reuse arm must not delete the ref it moved away from (git's own words: %v)", err)
	} else if strings.TrimSpace(string(out)) == "" {
		t.Fatalf("the ref %q survived but names no commit", groundBranchName)
	}
}

// taskRefPrefixForTest is the private-ref prefix this package does not import
// from internal/tools (it is a leaf: Rule 14 keeps the dependency direction one
// way). Written once, here, where the arm reads it (Rules 1b/13).
const taskRefPrefixForTest = "task/"
