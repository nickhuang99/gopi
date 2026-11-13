package tools

// worktree_sweep_undelivered_test.go — the UNDELIVERED-COMMITS rung
// (worktree_sweep_undelivered.go), red-proofed in both directions.
//
// WHAT IS PINNED, in one sentence: a CLONE-CHANNEL ground (its `.git` is a
// DIRECTORY, so `os.RemoveAll` takes its objects with it) whose branch carries
// commits no delivery point has is REFUSED — with the count and the refs it was
// counted against named — and the same ground is collectable the moment those
// commits reach a delivery point. Plus the two directions that keep the rung
// honest rather than merely strict:
//
//	① a LINKED worktree gets NO refusal, at any commit count: `git worktree
//	  remove` deletes the directory while the branch and its objects stay in the
//	  main repo, so refusing there would stop the release pass without
//	  protecting anything;
//	② an UNMEASURABLE probe is a KEEP — never a zero (Rule 15a).
//
// Every case drives the real probe against real git repositories; nothing here
// stubs `git`, because the whole point of the rung is that it asks git itself
// rather than trusting a proxy.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// undeliveredFixture is one clone-channel ground plus the main repo it was
// cloned from — the exact two-repository shape internal/devworkspace produces
// and `devworkspace.Reclaim` deletes with `os.RemoveAll`.
type undeliveredFixture struct {
	main   string // the "server": where master is pushed
	ground string // the clone-channel workspace
	branch string
}

// newUndeliveredFixture builds main + clone with one committed file on master,
// then checks the two preconditions the fixture's meaning depends on (a clone
// ground is a clone ground; the branch starts 0 ahead).
func newUndeliveredFixture(t *testing.T) undeliveredFixture {
	t.Helper()
	main := t.TempDir()
	sweepGit(t, main, "init", "-q", "-b", masterBranchName)
	sweepGit(t, main, "config", "user.email", "undelivered@test")
	sweepGit(t, main, "config", "user.name", "undelivered")
	if err := os.WriteFile(filepath.Join(main, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, main, "add", "seed.txt")
	sweepGit(t, main, "commit", "-q", "-m", "seed")

	parent := t.TempDir()
	ground := filepath.Join(parent, "ground")
	sweepGit(t, parent, "clone", "-q", main, ground)
	sweepGit(t, ground, "config", "user.email", "undelivered@test")
	sweepGit(t, ground, "config", "user.name", "undelivered")

	f := undeliveredFixture{main: main, ground: ground, branch: "task/undelivered-fixture"}
	// PRECONDITION ①: the discriminator the rung keys on. A clone's `.git` is a
	// directory; if this ever stops being true the file's whole scope has moved
	// and the test must say so rather than pass on the wrong shape.
	if !deliveryProofIsCloneGround(ground) {
		t.Fatalf("fixture: %s is not recognised as a clone ground (.git must be a DIRECTORY)", ground)
	}
	// PRECONDITION ②: the fixture starts DELIVERED, so every later refusal is
	// caused by the commit the case adds and nothing else.
	if p := worktreeUndeliveredCommits(ground, masterBranchName); !p.Measured || p.Ahead != 0 {
		t.Fatalf("fixture: a fresh clone's master must be 0 ahead (measured=%v ahead=%d why=%q)",
			p.Measured, p.Ahead, p.Why)
	}
	return f
}

// commitOnUndeliveredBranch creates the branch in the ground and adds one commit
// to it — work that exists in NO delivery point yet.
func commitOnUndeliveredBranch(t *testing.T, f undeliveredFixture) {
	t.Helper()
	sweepGit(t, f.ground, "checkout", "-q", "-b", f.branch)
	if err := os.WriteFile(filepath.Join(f.ground, "work.txt"), []byte("work that nobody else has\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, f.ground, "add", "work.txt")
	sweepGit(t, f.ground, "commit", "-q", "-m", "work nobody has")
}

// TestUndeliveredRefusesACloneGroundCarryingWork is the RED half: the ground
// holds a commit no delivery point has, so the rung must refuse AND the refusal
// must name the count, the branch and the refs it counted against.
func TestUndeliveredRefusesACloneGroundCarryingWork(t *testing.T) {
	f := newUndeliveredFixture(t)
	commitOnUndeliveredBranch(t, f)

	p := worktreeUndeliveredCommits(f.ground, f.branch)
	if !p.Measured {
		t.Fatalf("the probe must ANSWER for a real branch (why=%q)", p.Why)
	}
	if p.Ahead != 1 {
		t.Fatalf("ahead = %d, want 1 (the one commit this ground alone holds)", p.Ahead)
	}
	why := worktreeSweepUndeliveredRefusal(f.ground, f.ground, f.branch)
	if why == "" {
		t.Fatal("the rung must REFUSE a clone ground whose branch carries an undelivered commit — removing this directory destroys that commit")
	}
	for _, want := range []string{f.branch, "1 commit", "push the branch or merge it into " + IntegrationBranchName} {
		if !strings.Contains(why, want) {
			t.Fatalf("the refusal must name %q so the operator can act on it; got %q", want, why)
		}
	}
	// The refs the question was asked against are printed, so the measurement is
	// reproducible by hand (Rule 15: a refusal nobody can check is noise). The
	// ground has no `refs/remotes/origin/<branch>` yet (nothing pushed it), so
	// the points are the integration branch's two spellings — and the refusal
	// must name exactly the ones that RESOLVED, never a ref that answers nothing.
	if len(p.Points) == 0 {
		t.Fatalf("the probe answered ahead=%d with NO delivery point resolved — that pair is self-contradictory", p.Ahead)
	}
	for _, point := range p.Points {
		if !strings.Contains(why, point) {
			t.Fatalf("the refusal must name the delivery point %q it counted against; got %q", point, why)
		}
	}
}

// TestUndeliveredAllowsTheSameGroundOnceDelivered is the GREEN half: pushing the
// very same branch to origin makes the ground collectable, with no other change.
// It is the direction that proves the rung measures DELIVERY and not "a branch
// exists" — a check that refused unconditionally would pass the red half alone.
func TestUndeliveredAllowsTheSameGroundOnceDelivered(t *testing.T) {
	f := newUndeliveredFixture(t)
	commitOnUndeliveredBranch(t, f)
	if why := worktreeSweepUndeliveredRefusal(f.ground, f.ground, f.branch); why == "" {
		t.Fatal("precondition: the undelivered branch must be refused before it is pushed")
	}

	// DELIVER IT — the clone channel's own contract: push the branch.
	sweepGit(t, f.ground, "push", "-q", deliveryProofOriginRemote, f.branch)

	p := worktreeUndeliveredCommits(f.ground, f.branch)
	if !p.Measured || p.Ahead != 0 {
		t.Fatalf("after the push the probe must answer 0 (measured=%v ahead=%d why=%q)", p.Measured, p.Ahead, p.Why)
	}
	if why := worktreeSweepUndeliveredRefusal(f.ground, f.ground, f.branch); why != "" {
		t.Fatalf("a delivered ground must not be refused, got %q", why)
	}
}

// TestUndeliveredJudgesALinkedWorktreeToo pins the SCOPE decision that a first
// draft of this file got wrong in the direction that would have left the
// operator's own symptom unfixed: a LINKED worktree is judged too.
//
// The reasoning, and the measurement behind it: `git worktree remove` deletes
// the DIRECTORY while the branch survives in the main repo — so a reading that
// stops at "the ref is safe" says nothing is lost. What that reading misses is
// the delivery PATH: the row's recorded deliverable IS the directory, the push
// is a single shot taken from inside it, and nothing re-runs it. The operator's
// `[closeout-refused]: the recorded worktree directory is not there` is exactly
// that state (task_finish_cleanup.go's ④ comment: `settled=7 refused=3`, every
// one that sentence). So a linked ground carrying an undelivered commit is
// REFUSED, and the same ground becomes collectable once the commit is merged.
func TestUndeliveredJudgesALinkedWorktreeToo(t *testing.T) {
	repo := sweepInitRepo(t)
	wt := filepath.Join(sweepFixtureRoot(t), "wt-undelivered-scope")
	sweepGit(t, repo, "checkout", "-q", "-b", "feat/undelivered-scope")
	if err := os.WriteFile(filepath.Join(repo, "w.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, repo, "add", "w.txt")
	sweepGit(t, repo, "commit", "-q", "-m", "work on a branch")
	sweepGit(t, repo, "checkout", "-q", masterBranchName)
	sweepGit(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeAddVerb, wt, "feat/undelivered-scope")

	// PRECONDITION: a LINKED worktree (.git is a FILE) sharing the main repo's
	// refs — the shape whose refs must therefore be read from `repo`.
	if deliveryProofIsCloneGround(wt) {
		t.Fatalf("fixture: %s must be a LINKED worktree (.git is a file), not a clone ground", wt)
	}
	if p := worktreeUndeliveredCommits(repo, "feat/undelivered-scope"); p.Ahead != 1 {
		t.Fatalf("fixture: the branch must be 1 ahead of the integration branch, got %d", p.Ahead)
	}

	why := worktreeSweepUndeliveredRefusal(wt, repo, "feat/undelivered-scope")
	if why == "" {
		t.Fatal("a linked ground whose branch carries an undelivered commit must be REFUSED: the directory is the row's recorded deliverable and the push is taken from inside it")
	}
	for _, want := range []string{"feat/undelivered-scope", "1 commit"} {
		if !strings.Contains(why, want) {
			t.Fatalf("the refusal must name %q; got %q", want, why)
		}
	}

	// THE OTHER DIRECTION: the merger's delivery — fast-forward the branch into
	// the integration branch — makes the very same ground collectable.
	sweepGit(t, repo, finishCleanupCheckoutVerb, masterBranchName)
	sweepGit(t, repo, "merge", "-q", "--ff-only", "feat/undelivered-scope")
	if why := worktreeSweepUndeliveredRefusal(wt, repo, "feat/undelivered-scope"); why != "" {
		t.Fatalf("a ground whose branch is merged into %s must not be refused, got %q", masterBranchName, why)
	}
}

// TestUndeliveredUnmeasurableKeeps pins Rule 15a's direction and the two
// distinctions that keep the rung from parking every cleaned-up registration:
//
//	① an ABSENT branch is an ANSWER (measured, ahead=0) — the registration holds
//	  nothing to deliver and the dead-registration rung owns the rest;
//	② a branch that EXISTS with commits, in a repository where NO delivery point
//	  resolves, is UNMEASURABLE — a keep whose sentence says the measurement
//	  failed. Never a silent zero, because a zero there would mean "nothing is
//	  missing" on evidence nobody gathered.
func TestUndeliveredUnmeasurableKeeps(t *testing.T) {
	// The fixture's integration branch is `main`, not `master`: refs/heads/master
	// does not resolve, there is no remote, so worktreeUndeliveredDeliveryPoints
	// finds nothing at all — the one shape that is genuinely unanswerable.
	ground := t.TempDir()
	sweepGit(t, ground, "init", "-q", "-b", "main")
	sweepGit(t, ground, "config", "user.email", "undelivered@test")
	sweepGit(t, ground, "config", "user.name", "undelivered")
	if err := os.WriteFile(filepath.Join(ground, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, ground, "add", "seed.txt")
	sweepGit(t, ground, "commit", "-q", "-m", "seed")

	// ① the absent branch.
	if p := worktreeUndeliveredCommits(ground, "feat/never-existed"); !p.Measured || p.Ahead != 0 {
		t.Fatalf("an absent branch must answer measured=true, ahead=0 (measured=%v ahead=%d why=%q)",
			p.Measured, p.Ahead, p.Why)
	}
	if why := worktreeSweepUndeliveredRefusal(ground, ground, "feat/never-existed"); why != "" {
		t.Fatalf("an absent branch must produce no keep, got %q", why)
	}

	// ② a branch that exists, carries work, and has no delivery point to be
	// counted against.
	sweepGit(t, ground, "checkout", "-q", "-b", "feat/no-delivery-point")
	if err := os.WriteFile(filepath.Join(ground, "lonely.txt"), []byte("nowhere else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, ground, "add", "lonely.txt")
	sweepGit(t, ground, "commit", "-q", "-m", "work with no delivery point")

	p := worktreeUndeliveredCommits(ground, "feat/no-delivery-point")
	if p.Measured {
		t.Fatalf("this fixture has no %q and no remote — the probe must NOT answer (it answered ahead=%d)", masterBranchName, p.Ahead)
	}
	if len(p.Points) != 0 {
		t.Fatalf("no delivery point resolves here, yet the probe collected %v", p.Points)
	}
	why := worktreeSweepUndeliveredRefusal(ground, ground, "feat/no-delivery-point")
	if why == "" {
		t.Fatal("an unmeasurable probe must KEEP the ground (Rule 15a): the question \"is this work's only home\" was not answered")
	}
	if !strings.Contains(why, "could not measure") {
		t.Fatalf("the keep's sentence must say the measurement failed, got %q", why)
	}
}

// TestUndeliveredCountsEveryCommitNotJustTheFirst pins the count's ACCURACY, the
// dimension a one-commit fixture cannot see.
//
// MEASURED DEFECT (2026-10-23, found by running the rung on this host, not by
// reading it): the probe's argv carried a stray `-1` — git's "limit the number
// of commits shown" — beside `--count`. `rev-list --count` therefore answered
// with the LIMIT, not with the count: a real branch holding NINE undelivered
// commits was reported as "holds 1 commit that no delivery point has"
// (measured in /home/nick/work/gopi:
// `git rev-list --count feat/suite-closeout-sweep-gate --not refs/remotes/origin/master refs/heads/master -1`
// → 1, while the same command WITHOUT `-1` → 9).
//
// The sentence stayed plausible, which is why it survived a green suite: every
// other case here commits EXACTLY ONE commit, so a count clamped to 1 and a
// count that is right are indistinguishable. The operator-facing consequence is
// not cosmetic — "1 commit" on a branch holding nine invites the reader to
// judge the work as nearly-delivered, and Rule 15's line is that a false
// sentence on an operator's screen is the defect, not the inconvenience.
//
// So this case builds a branch with a count NO clamp can fake: >1, and asserted
// exactly.
func TestUndeliveredCountsEveryCommitNotJustTheFirst(t *testing.T) {
	f := newUndeliveredFixture(t)
	sweepGit(t, f.ground, "checkout", "-q", "-b", f.branch)
	const commits = 3 // >1: a clamp to one cannot pass, and the number is asserted exactly
	for i := 0; i < commits; i++ {
		name := filepath.Join(f.ground, "work-"+strconv.Itoa(i)+".txt")
		if err := os.WriteFile(name, []byte("work that nobody else has\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		sweepGit(t, f.ground, "add", filepath.Base(name))
		sweepGit(t, f.ground, "commit", "-q", "-m", "work nobody has, part "+strconv.Itoa(i))
	}

	p := worktreeUndeliveredCommits(f.ground, f.branch)
	if !p.Measured {
		t.Fatalf("the probe must ANSWER for a real branch (why=%q)", p.Why)
	}
	if p.Ahead != commits {
		t.Fatalf("ahead = %d, want %d — the rung must count EVERY undelivered commit: a count clamped "+
			"to one tells the operator this is the work's only home without telling them how much "+
			"work it is", p.Ahead, commits)
	}

	// The COUNT reaches the operator's sentence, not just the struct: the refusal
	// must print the real number, in the plural form that number requires.
	why := worktreeSweepUndeliveredRefusal(f.ground, f.ground, f.branch)
	if why == "" {
		t.Fatal("an undelivered branch must be refused")
	}
	if want := strconv.Itoa(commits) + " commits"; !strings.Contains(why, want) {
		t.Fatalf("the refusal must name %q (the real count, pluralised); got %q", want, why)
	}

	// And the other direction, on the same fixture: delivering all three makes it
	// collectable — so the case measures DELIVERY, not merely "more than zero".
	sweepGit(t, f.ground, "push", "-q", deliveryProofOriginRemote, f.branch)
	if p := worktreeUndeliveredCommits(f.ground, f.branch); !p.Measured || p.Ahead != 0 {
		t.Fatalf("after pushing all %d commits the probe must answer 0 (measured=%v ahead=%d why=%q)",
			commits, p.Measured, p.Ahead, p.Why)
	}
}

// TestUndeliveredSaysNothingForADirectoryThatIsGone pins the direction that a
// REAL RUN of the rung exposed (2026-10-23), not a reading of it.
//
// MEASURED: `gopi task sweep-worktrees --dry-run` on this host printed
//
//	keep [run-ground-anchor] /home/nick/workspace/gopi-closeout-sweep  kept: the
//	ground's branch feat/suite-closeout-sweep-gate holds 9 commits that no
//	delivery point has (...) — this directory is that work's only home: push the
//	branch or merge it into master, then the ground is collectable
//
// while that directory had not existed for a day — the removal sink records it:
//
//	{"ts":"2026-10-22T21:13:48.776365076Z","kind":"worktree-removal",
//	 "arm":"task-self-own-sweep","target":"/home/nick/workspace/gopi-closeout-sweep"}
//
// Two defects in one sentence, and the second is the expensive one:
//
//	① the sentence is FALSE — "this directory is that work's only home" about a
//	  directory that is not there (Rule 15: a false sentence on the operator's
//	  screen is the defect, not the inconvenience);
//	② the keep is PERMANENT. Every other rung on this axis relaxes — the age
//	  floor elapses, the holder scan finds no live process — but a commit count
//	  is a fixed fact about a ref, so a registration whose directory is gone
//	  would be parked by this rung FOREVER. Measured on the same run: 16 of the
//	  31 keeps named a directory that os.Stat could not see.
//
// The rung's own question is "would REMOVING THIS DIRECTORY destroy work?", and
// that question is answered — vacuously — by a directory that is already
// removed: no removal can destroy what a previous removal took, and a loss that
// already happened is not undone by refusing later. The shape belongs to the
// dead-registration rung, which owns "the anchor's directory is gone".
func TestUndeliveredSaysNothingForADirectoryThatIsGone(t *testing.T) {
	// THE SHAPE IS THE LINKED WORKTREE'S ON PURPOSE, and a first draft of this
	// case got it wrong in a way that made the red proof unable to go red: it
	// removed a CLONE ground, so `repo` and `ground` were the SAME vanished path
	// and the probe short-circuited on "the branch does not resolve here" — an
	// existing rung answering Measured=true/ahead=0 — so the new guard was never
	// reached (measured: the case PASSED with the guard disabled). What an
	// operator actually sees is the opposite shape: the MAIN REPO IS ALIVE and
	// only the ground directory is gone, so the branch still resolves and its
	// undelivered commits are still countable. That is this fixture.
	repo := sweepInitRepo(t)
	branch := "feat/undelivered-gone"
	sweepGit(t, repo, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "gone.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, repo, "add", "gone.txt")
	sweepGit(t, repo, "commit", "-q", "-m", "work on a branch")
	sweepGit(t, repo, "checkout", "-q", masterBranchName)
	wt := filepath.Join(sweepFixtureRoot(t), "wt-undelivered-gone")
	sweepGit(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeAddVerb, wt, branch)

	// PRECONDITION: while the directory IS there, the rung refuses — so the
	// silence below is caused by the removal and by nothing else.
	if why := worktreeSweepUndeliveredRefusal(wt, repo, branch); why == "" {
		t.Fatal("precondition: a linked ground carrying undelivered work must be refused while it exists")
	}

	// THE REMOVAL. `git worktree remove` is what this axis does; the branch and
	// its objects stay in the main repo, so the commit is STILL undelivered and
	// still countable — which is exactly why the short-circuit above does not
	// rescue this case.
	sweepGit(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, wt)
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("fixture: the ground must be gone, stat says %v", err)
	}
	// ... and the branch does still resolve, so the probe WOULD answer ahead>0
	// if the gone-directory guard did not speak first.
	if p := worktreeUndeliveredCommits(repo, branch); !p.Measured || p.Ahead != 1 {
		t.Fatalf("fixture: the branch must still be 1 ahead after the directory is gone "+
			"(measured=%v ahead=%d why=%q)", p.Measured, p.Ahead, p.Why)
	}

	if why := worktreeSweepUndeliveredRefusal(wt, repo, branch); why != "" {
		t.Fatalf("a ground whose directory is GONE must get no sentence from this rung — its keep would "+
			"be permanent (a commit count never relaxes) and it would tell the operator that a "+
			"non-existent directory is the work's only home; got %q", why)
	}

	// AND THE SAME ANSWER THROUGH THE AXIS THE SWEEP ACTUALLY CALLS, so this is
	// not merely a fact about the helper: the anchor classifier must not park a
	// gone directory on this rung either.
	a := RunGroundAnchor{
		Worktree:   wt,
		Branch:     branch,
		Repo:       repo,
		TaskID:     "01a00000-0000-7000-8000-000000000000",
		PID:        "999999999",
		StartTicks: "1",
		Session:    "1",
	}
	if v := RunGroundAnchorLiveness(a); strings.Contains(v.Reason, "only home") {
		t.Fatalf("the axis must not report a gone directory as the work's only home; reason %q", v.Reason)
	}
}
