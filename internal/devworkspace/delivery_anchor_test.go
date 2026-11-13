package devworkspace

// delivery_anchor_test.go — the TWO facts the delivery leg never established,
// proved on real git (no fakes: the "remote" is a local repository two
// directories away, exactly as the channel's own tests do it).
//
//	① THE ANCHOR: the directory about to be published from must be the directory
//	  the row records as its deliverable. Measured defect 01a17dee: the leg
//	  published from an empty ground while the run's two commits sat in ANOTHER
//	  row's ground. The refusal must name BOTH paths and must run NO git step —
//	  neither a pull (which moves the ground), nor a build, nor a push.
//	② NOTHING TO DELIVER: its own blocker kind, decided BEFORE the pull, so the
//	  operator is never handed the pull's vocabulary for an empty workspace.
//	③ THE CONTROL: a run whose commits ARE in its own deliverable.worktree still
//	  delivers — a gate that refuses everything satisfies ① and ② while breaking
//	  the channel.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// commitIn leaves one commit in dir — the shape an executor's own work has.
func commitIn(t *testing.T, dir, name, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(subject+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", name)
	gitIn(t, dir, "commit", "-q", "-m", subject)
}

// TestDeliveryRefusesWhenThePublishDirIsNotTheRunsWorktree is arm ①: the row's
// recorded ground is one directory, the leg is asked to publish another. The
// attempt must STOP there, naming both, with pull/build/push never run.
func TestDeliveryRefusesWhenThePublishDirIsNotTheRunsWorktree(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)

	// THE ROW'S RECORDED GROUND IS SOMEWHERE ELSE, and THAT is where this run's
	// commit lives — the measured shape of defect 01a17dee (its own ground was
	// empty: 0 commits ahead, 0 dirty lines, while the real commits sat in
	// another row's ground). The leg is asked to publish from the empty one.
	elsewhere := filepath.Join(cfg.Root, "01a17da2-0000-7000-8000-00000000other")
	gitIn(t, cfg.Root, "clone", "-q", origin, elsewhere)
	commitIn(t, elsewhere, "work.txt", "deliverable work")

	// TWO MEASUREMENTS TAKEN BEFORE, so "nothing ran" is proved rather than
	// asserted: the publish directory's tip and the tracking ref a pull moves.
	headBefore := strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD"))
	trackingBefore := strings.TrimSpace(gitIn(t, dir, "rev-parse", "origin/"+DefaultBranch))
	// Origin moves on, so a pull WOULD have something to fetch: without this the
	// "no pull ran" reading could be explained by there being nothing to fetch.
	commitIn(t, origin, "theirs.txt", "theirs moves first")

	res, blk, err := AttemptDelivery(context.Background(), cfgWithWorktree(cfg, elsewhere), dir)
	if blk != nil {
		t.Fatalf("a wrong directory is not a blocker about this work — it is an error, got blocker %q: %s", blk.Kind, blk.Report())
	}
	if err == nil {
		t.Fatalf("the delivery was ALLOWED from %s although the row's ground %s is where the commits are (pushed=%v)",
			dir, elsewhere, res.Pushed)
	}
	// BOTH PATHS, VERBATIM: an operator copies these into a command.
	for _, want := range []string{elsewhere, dir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got: %v", want, err)
		}
	}
	if !strings.Contains(err.Error(), "refusing to publish") {
		t.Errorf("the refusal must SAY what it refuses to do, got: %v", err)
	}
	// AND IT MUST NOT SAY IT TWICE: an operator reading a duplicated path cannot
	// tell whether two directories are involved.
	if n := strings.Count(err.Error(), dir); n != 1 {
		t.Errorf("the publish directory %q is named %d times in the refusal, want exactly once: %v", dir, n, err)
	}
	t.Logf("the refusal: %v", err)

	// NOTHING RAN.
	if after := strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD")); after != headBefore {
		t.Fatalf("the workspace's HEAD moved %s → %s although the delivery was refused", headBefore, after)
	}
	if after := strings.TrimSpace(gitIn(t, dir, "rev-parse", "origin/"+DefaultBranch)); after != trackingBefore {
		t.Fatalf("origin/%s in the workspace moved %s → %s: a fetch/pull RAN although the anchor gate refused",
			DefaultBranch, trackingBefore, after)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build-stamp.txt")); statErr == nil {
		t.Fatal("the declared build RAN although the anchor gate refused")
	}
	if log := gitIn(t, origin, "log", "--oneline", DefaultBranch); strings.Contains(log, "deliverable work") {
		t.Fatalf("the deliverable reached origin although the anchor gate refused:\n%s", log)
	}
}

// cfgWithWorktree is the ONE place these tests attach the row's recorded ground
// (Rule 14: the field's meaning is exercised by name, not by position).
func cfgWithWorktree(cfg Config, recorded string) Config {
	cfg.Worktree = recorded
	return cfg
}

// TestDeliverySaysNothingToDeliverInsteadOfBlaimingThePull is arm ②: a clean
// workspace with nothing ahead of its base gets its OWN outcome, and the pull's
// vocabulary never appears — that collapse is what sent an operator chasing a
// network ghost in 01a17dee.
func TestDeliverySaysNothingToDeliverInsteadOfBlaimingThePull(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	// THE PRECONDITION, MEASURED: the workspace carries nothing to publish.
	baseTip, integration, berr := deliveryBaseTip(context.Background(), dir, DefaultBranch, "")
	if berr != nil {
		t.Fatalf("fixture precondition: base tip: %v", berr)
	}
	if n, err := deliveryOutstanding(context.Background(), dir, gitHeadRef, baseTip); err != nil || n != 0 {
		t.Fatalf("fixture precondition: commits ahead against %s = %d (err %v), want 0", integration, n, err)
	}
	trackingBefore := strings.TrimSpace(gitIn(t, dir, "rev-parse", "origin/"+DefaultBranch))
	originBefore := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch))

	res, blk, err := AttemptDelivery(context.Background(), cfgWithWorktree(cfg, dir), dir)
	if err != nil {
		t.Fatalf("an empty workspace is a BLOCKER (a fact the next round can act on), not an error: %v", err)
	}
	if blk == nil {
		t.Fatalf("the delivery was allowed with nothing to deliver (pushed=%v detail=%q)", res.Pushed, res.Detail)
	}
	if blk.Kind != BlockerKindEmpty {
		t.Fatalf("blocker kind = %q, want %q — an empty workspace must never be reported through another step's vocabulary (words: %s)",
			blk.Kind, BlockerKindEmpty, blk.Words)
	}
	report := blk.Report()
	// THE FACT ITSELF, and the command that measured it.
	for _, want := range []string{"carries no commit to deliver", deliveryCountFlag} {
		if !strings.Contains(report, want) {
			t.Errorf("the refusal must name %q:\n%s", want, report)
		}
	}
	// THE TWO GHOSTS IT MUST NOT RAISE: the pull step, and a conflict.
	for _, ghost := range []string{"pull step refused", "conflicted path"} {
		if strings.Contains(report, ghost) {
			t.Fatalf("an empty workspace must not be reported with %q (that is the defect this split removes):\n%s", ghost, report)
		}
	}
	t.Logf("the empty refusal: %s", report)

	// AND NO GIT STEP RAN EITHER — the pull never got a chance to move anything.
	if after := strings.TrimSpace(gitIn(t, dir, "rev-parse", "origin/"+DefaultBranch)); after != trackingBefore {
		t.Fatalf("a fetch/pull ran although there was nothing to deliver (origin/%s %s → %s)",
			DefaultBranch, trackingBefore, after)
	}
	if after := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch)); after != originBefore {
		t.Fatalf("origin moved %s → %s", originBefore, after)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build-stamp.txt")); statErr == nil {
		t.Fatal("the declared build RAN on a workspace with nothing to deliver")
	}
}

// TestDeliveryNamesASiblingThatCarriesTheRowAddress is the measured HINT arm:
// "0 commits here" is half a diagnosis, and the operator's next question is
// "then where did the run commit?". When another directory under the SAME
// workspace root carries this row's address ref with commits ahead, the refusal
// names it — as a hint, not as a fact about ownership.
func TestDeliveryNamesASiblingThatCarriesTheRowAddress(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	cfg.GroundBranch = "task/" + testTaskID
	cfg.BaseBranch = DefaultBranch
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)

	// THE SIBLING: another row's ground that happens to carry THIS row's
	// address ref and a commit on it.
	sibling := filepath.Join(cfg.Root, "01a17da2-0000-7000-8000-00000000sib")
	gitIn(t, cfg.Root, "clone", "-q", origin, sibling)
	gitIn(t, sibling, "checkout", "-q", "-b", cfg.GroundBranch)
	commitIn(t, sibling, "theirs.txt", "the run's real commit, in another ground")

	_, blk, err := AttemptDelivery(context.Background(), cfgWithWorktree(cfg, dir), dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk == nil || blk.Kind != BlockerKindEmpty {
		t.Fatalf("want an empty blocker, got %+v", blk)
	}
	report := blk.Report()
	if !strings.Contains(report, sibling) || !strings.Contains(report, cfg.GroundBranch) {
		t.Fatalf("the refusal must name the sibling %q and the address %q it matched:\n%s",
			sibling, cfg.GroundBranch, report)
	}
	// THE SHAPE IS STATED FROM A MEASUREMENT, never implied: this candidate's
	// address ref points at its own HEAD, so it IS a checkout standing on the
	// row's address (deliverySiblingShape measured exactly that).
	if !strings.Contains(report, deliveryShapeRowGround) {
		t.Fatalf("the sibling clause must state the measured shape (%q):\n%s", deliveryShapeRowGround, report)
	}
	if strings.Contains(report, deliveryShapeScratch) {
		t.Fatalf("a ref that points at the candidate's own HEAD is NOT a previous run's scratch clone:\n%s", report)
	}
	// WHERE IS NOT A RECOVERY (row 01a1826e): the hint named the directory and
	// stopped, so the run whose delivery was blocked here had to invent the step
	// that brings the commits home. The clause must carry that step, spelled with
	// the SIBLING DIRECTORY and the FULL ref name — origin does not have the
	// commit (nothing pushed it), and a bare branch name would resolve against
	// whatever that sibling's checkout happens to stand on.
	wantFetch := fmt.Sprintf(deliverySiblingRecoveryFmt, deliveryFetchVerb, sibling, refsHeadsPrefix+cfg.GroundBranch)
	if !strings.Contains(report, wantFetch) {
		t.Fatalf("the hint must state the recovery step %q:\n%s", wantFetch, report)
	}
	t.Logf("the hinted refusal: %s", report)
}

// TestDeliveryStillDeliversWhenTheAnchorMatches is arm ③ — the control. The
// run's commits ARE in the directory the row records, so the gates must stay out
// of the way and the push must land.
func TestDeliveryStillDeliversWhenTheAnchorMatches(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	commitIn(t, dir, "work.txt", "deliverable work")

	// THE SAME DIRECTORY, SPELLED THROUGH A SYMLINK: an identity comparison, not
	// a string comparison — a recorded ground reached by another path is still
	// the same ground, and refusing it would be the gate crying wolf.
	link := filepath.Join(cfg.Root, "recorded-via-link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	res, blk, err := AttemptDelivery(context.Background(), cfgWithWorktree(cfg, link), dir)
	if err != nil {
		t.Fatalf("a matching anchor must deliver, got error: %v", err)
	}
	if blk != nil {
		t.Fatalf("a matching anchor must not block, got %q: %s", blk.Kind, blk.Report())
	}
	if !res.Pushed {
		t.Fatalf("the delivery did not land (detail=%q)", res.Detail)
	}
	if log := gitIn(t, origin, "log", "--oneline", DefaultBranch); !strings.Contains(log, "deliverable work") {
		t.Fatalf("origin is missing the deliverable:\n%s", log)
	}
}

// TestDeliveryDeliversAndReportsAStaleStamp is the OTHER half of the anchor
// rule: the row's record names one directory, the publish directory is another,
// and the COMMITS ARE IN THE PUBLISH DIRECTORY. Refusing there would block a
// legitimate delivery over a stale stamp, so the attempt delivers and says the
// two disagree. (The mirror case — commits in the RECORDED directory, none in
// the publish one — is the refusal arm above.)
func TestDeliveryDeliversAndReportsAStaleStamp(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	commitIn(t, dir, "work.txt", "deliverable work")

	// THE STALE RECORD: another directory, empty of this run's work.
	stale := filepath.Join(cfg.Root, "01a17da2-0000-7000-8000-00000000stale")
	gitIn(t, cfg.Root, "clone", "-q", origin, stale)

	var log strings.Builder
	cfg.Log = &log
	res, blk, err := AttemptDelivery(context.Background(), cfgWithWorktree(cfg, stale), dir)
	if err != nil {
		t.Fatalf("commits in the publish directory must deliver, got error: %v", err)
	}
	if blk != nil {
		t.Fatalf("commits in the publish directory must not block, got %q: %s", blk.Kind, blk.Report())
	}
	if !res.Pushed {
		t.Fatalf("the delivery did not land (detail=%q)", res.Detail)
	}
	if log := gitIn(t, origin, "log", "--oneline", DefaultBranch); !strings.Contains(log, "deliverable work") {
		t.Fatalf("origin is missing the deliverable:\n%s", log)
	}
	// AND THE DISAGREEMENT IS SAID OUT LOUD (Rule 15): neither silent nor fatal.
	if !strings.Contains(log.String(), stale) || !strings.Contains(log.String(), "reporting the disagreement") {
		t.Fatalf("the stale stamp must be reported, naming %q:\n%s", stale, log.String())
	}
}

// TestDeliveryAnchorGateIsSilentWithoutARecordedGround pins the backward-
// compatible arm: no `deliverable.worktree` = no question to ask. A caller that
// never recorded a ground (every legacy row, the retry path before the field was
// wired) must behave exactly as it did before the gate existed.
func TestDeliveryAnchorGateIsSilentWithoutARecordedGround(t *testing.T) {
	ctx := context.Background()
	cfg := Config{} // no Worktree
	if err := deliveryAnchorGate(ctx, cfg, "/any/where", DefaultBranch); err != nil {
		t.Fatalf("an absent recorded ground must ask no question, got: %v", err)
	}
	// AND AN UNRESOLVABLE SIDE IS AN ERROR, never a pass (Rule 15).
	if err := deliveryAnchorGate(ctx, Config{Worktree: " "}, "", DefaultBranch); err != nil {
		t.Fatalf("a blank recorded ground is an absent question, got: %v", err)
	}
	if err := deliveryAnchorGate(ctx, Config{Worktree: "/definitely/not/here"}, "", DefaultBranch); err == nil {
		t.Fatal("a publish directory that resolves to no path must be refused, not passed")
	}
}

// deliveryFrozenBaseClone is the SHAPE the defect was measured on, built by
// hand: a clone whose fetch refspec stops updating `refs/remotes/origin/*`, so
// its `origin/<base>` keeps pointing at the base AS IT WAS at clone time while
// the real base moves on. Measured on this host in exactly this shape:
// `origin/master` read 20c15007 while the base was 26008d87 — eleven commits
// the base had landed hours earlier still counted as "ahead".
func deliveryFrozenBaseClone(t *testing.T, origin string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "frozen-clone")
	gitIn(t, filepath.Dir(dir), "clone", "-q", origin, dir)
	// No branch-level updates reach refs/remotes/origin/* any more: the refs
	// already there are frozen at their clone-time tips.
	gitIn(t, dir, "config", "remote.origin.fetch", "+refs/tags/*:refs/remotes/origin/*")
	return dir
}

// deliveryBringBaseObject brings the base's current tip into an existing clone
// WITHOUT moving any ref that clone already tracks — so the clone can READ a
// revision its own refspec has stopped following. This is the fixture's own
// move; production code fetches nothing (a fetch writes refs).
func deliveryBringBaseObject(t *testing.T, clone, origin string) {
	t.Helper()
	gitIn(t, clone, "fetch", "-q", origin, DefaultBranch+":refs/gopi-test/"+DefaultBranch+"-landed")
}

// commitOnBranch leaves one commit on a NEW branch of repo, cut from
// DefaultBranch, leaving repo checked out on DefaultBranch again.
func commitOnBranch(t *testing.T, repo, branch, name, subject string) {
	t.Helper()
	gitIn(t, repo, "checkout", "-q", "-b", branch, DefaultBranch)
	commitIn(t, repo, name, subject)
	gitIn(t, repo, "checkout", "-q", DefaultBranch)
}

// TestDeliveryDoesNotOfferASiblingWhoseTipTheBaseAlreadyCarries is the arm for
// defect ② — the one that made this gate cry wolf on every run.
//
// THE CANDIDATE CARRIES THE ROW'S ADDRESS AT A COMMIT THE BASE HAS ALREADY
// LANDED, and every question the OLD criterion asked answers "yes, work is in
// flight here": the ref exists, it is not the base branch's name, and a count
// taken against the candidate's OWN `origin/master` is 2 — because that ref
// froze before the base merged the work. The tip carries NOTHING the real base
// lacks, so no work of this row is in flight there, and the refusal must not
// offer it. RED ON THE OLD CODE.
func TestDeliveryDoesNotOfferASiblingWhoseTipTheBaseAlreadyCarries(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	cfg.GroundBranch = "task/" + testTaskID
	cfg.BaseBranch = DefaultBranch
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)

	// THE WORK, on a branch of the base, two commits.
	gitIn(t, origin, "checkout", "-q", "-b", "task/elsewhere", DefaultBranch)
	commitIn(t, origin, "w1.txt", "work one")
	commitIn(t, origin, "w2.txt", "work two")
	gitIn(t, origin, "checkout", "-q", DefaultBranch)

	// THE CANDIDATE IS CLONED WHILE THE BASE IS STILL BEHIND — so its
	// origin/master freezes here, and the work looks "ahead" to it forever.
	cand := filepath.Join(cfg.Root, "01a17da2-0000-7000-8000-00000000base")
	gitIn(t, cfg.Root, "clone", "-q", origin, cand)
	gitIn(t, cand, "checkout", "-q", "-b", cfg.GroundBranch, "origin/task/elsewhere")

	// NOW THE BASE LANDS THAT WORK, and the row's workspace pulls it in (the
	// channel's own Open is the pull).
	gitIn(t, origin, "merge", "-q", "--no-ff", "-m", "land the row's work", "task/elsewhere")
	// The candidate's OWN refspec no longer follows the base, so its view of it
	// is frozen — but it must be able to READ the base's real tip, otherwise the
	// question this arm asks ("is that tip already history?") could not be put
	// to it at all. The fixture brings the object only; the REF stays frozen
	// (and that frozen ref is the trap the old criterion read as "2 ahead").
	deliveryBringBaseObject(t, cand, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open (bring the workspace up to the base that landed): %v", err)
	}

	// THE FIXTURE'S OWN SHAPE, measured (Rule 31), in two halves:
	//   (a) the base really does carry the candidate's tip,
	//   (b) the candidate's OWN spelling of "ahead" still says 2.
	baseTip := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch))
	candTip := strings.TrimSpace(gitIn(t, cand, "rev-parse", refsHeadsPrefix+cfg.GroundBranch))
	if _, aerr := runCommand(context.Background(), cand, gitBinary, gitMergeBaseVerb,
		gitIsAncestorFlag, candTip, baseTip); aerr != nil {
		t.Fatalf("fixture: the candidate's tip %s must already be an ancestor of the base %s: %v",
			candTip, baseTip, aerr)
	}
	if own := strings.TrimSpace(gitIn(t, cand, "rev-list", "--count", candTip,
		deliveryNotFlag, deliveryOriginPrefix+DefaultBranch)); own != "2" {
		t.Fatalf("fixture: the candidate's own origin/%s must still read 2 commits ahead, got %q",
			DefaultBranch, own)
	}

	_, blk, err := AttemptDelivery(context.Background(), cfgWithWorktree(cfg, dir), dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk == nil || blk.Kind != BlockerKindEmpty {
		t.Fatalf("want an empty blocker, got %+v", blk)
	}
	report := blk.Report()
	if strings.Contains(report, cand) {
		t.Fatalf("the refusal offered %s although its tip carries nothing the base lacks "+
			"(the base landed it; nothing of this row is in flight there):\n%s", cand, report)
	}
	t.Logf("the refusal without the false lead: %s", report)
}

// TestDeliveryDistinguishesAnEarlierRunScratchCloneFromARowGround is the SHAPE
// arm: two directories whose address tip is genuinely ahead of the REAL base,
// told apart by the one fact that distinguishes them — does that ref point at
// the directory's OWN HEAD?
//
//	scratch — a clone of a given branch whose checkout has moved since (its ref
//	          is a stamp the previous attempt left, and its own origin/<base> is
//	          frozen, so its own count is 1 while the real base says 3).
//	ground  — a checkout standing ON the address ref (`checkout -b`), ref and
//	          HEAD at the same commit.
func TestDeliveryDistinguishesAnEarlierRunScratchCloneFromARowGround(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	cfg.GroundBranch = "task/" + testTaskID
	cfg.BaseBranch = DefaultBranch
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)

	// THE SCRATCH CLONE, taken now: one local commit on its own branch, the
	// address ref stamped at that commit, and the checkout moved on since.
	scratch := deliveryFrozenBaseClone(t, origin)
	gitIn(t, scratch, "checkout", "-q", "-b", "scratchwork")
	commitIn(t, scratch, "local.txt", "a previous attempt's commit")
	gitIn(t, scratch, "branch", cfg.GroundBranch, "scratchwork")
	gitIn(t, scratch, "checkout", "-q", DefaultBranch)

	// THE BASE MOVES ON BY TWO, and the row's workspace pulls it in.
	commitIn(t, origin, "later-1.txt", "later one")
	commitIn(t, origin, "later-2.txt", "later two")
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open (bring the workspace up to the base that moved): %v", err)
	}
	// Same as the arm above: the scratch clone must be ABLE to read the base's
	// real tip, while its tracked ref stays frozen — that is the shape.
	deliveryBringBaseObject(t, scratch, origin)

	// THE ROW GROUND: a checkout standing ON the address ref, one commit ahead.
	ground := filepath.Join(cfg.Root, "01a17da2-0000-7000-8000-00000000grnd")
	gitIn(t, cfg.Root, "clone", "-q", origin, ground)
	gitIn(t, ground, "checkout", "-q", "-b", cfg.GroundBranch)
	commitIn(t, ground, "work.txt", "the run's real commit, in another row's ground")

	// THE FIXTURE'S OWN SHAPE, measured (Rule 31): the two candidates differ in
	// whether the address ref is their HEAD, and in nothing else that matters.
	scratchTip := strings.TrimSpace(gitIn(t, scratch, "rev-parse", refsHeadsPrefix+cfg.GroundBranch))
	scratchHead := strings.TrimSpace(gitIn(t, scratch, "rev-parse", gitHeadRef))
	groundTip := strings.TrimSpace(gitIn(t, ground, "rev-parse", refsHeadsPrefix+cfg.GroundBranch))
	groundHead := strings.TrimSpace(gitIn(t, ground, "rev-parse", gitHeadRef))
	if scratchTip == scratchHead {
		t.Fatalf("fixture: the scratch clone's address ref must NOT be its HEAD (both %s)", scratchTip)
	}
	if groundTip != groundHead {
		t.Fatalf("fixture: the row ground's address ref must BE its HEAD (%s vs %s)", groundTip, groundHead)
	}

	ctx := context.Background()
	baseTip, integration, berr := deliveryBaseTip(ctx, dir, DefaultBranch, cfg.GroundBranch)
	if berr != nil {
		t.Fatalf("base tip: %v", berr)
	}
	scratchAhead, serr := deliveryOutstanding(ctx, scratch, scratchTip, baseTip)
	if serr != nil {
		t.Fatalf("count the scratch clone against the real base: %v", serr)
	}
	groundAhead, gerr := deliveryOutstanding(ctx, ground, groundTip, baseTip)
	if gerr != nil {
		t.Fatalf("count the row ground against the real base: %v", gerr)
	}
	if scratchAhead == 0 || groundAhead == 0 {
		t.Fatalf("fixture: both candidates must be ahead of %s (scratch %d, ground %d)",
			integration, scratchAhead, groundAhead)
	}
	// THE TWO COUNTS ARE BOTH TAKEN AGAINST THE REAL BASE TIP (the gap between
	// that spelling and the candidate's own frozen one is proved in the arm
	// above, where the same directory reads 2 against its own origin/<base> and
	// 0 against the base): here the point is only that the shape sentence never
	// borrows a number the candidate's stale ref produced.
	t.Logf("scratch clone %d commit(s) ahead, row ground %d, both against %s",
		scratchAhead, groundAhead, integration)

	// THE SENTENCE, for each candidate, from the SAME function that writes it.
	scratchWords := fmt.Sprintf(deliveryEmptySiblingFmt, scratch, cfg.GroundBranch, scratchAhead,
		deliverySiblingShape(scratchHead, scratchTip))
	groundWords := fmt.Sprintf(deliveryEmptySiblingFmt, ground, cfg.GroundBranch, groundAhead,
		deliverySiblingShape(groundHead, groundTip))
	t.Logf("scratch: %s", scratchWords)
	t.Logf("ground:  %s", groundWords)

	if !strings.Contains(scratchWords, deliveryShapeScratch) {
		t.Fatalf("a ref that is NOT the checkout's HEAD must be reported as an earlier run's scratch clone:\n%s",
			scratchWords)
	}
	if strings.Contains(scratchWords, deliveryShapeRowGround) {
		t.Fatalf("the scratch clone must not be reported as a ground standing on the row's address:\n%s",
			scratchWords)
	}
	if !strings.Contains(groundWords, deliveryShapeRowGround) {
		t.Fatalf("a ref that IS the checkout's HEAD must be reported as this row's ground:\n%s", groundWords)
	}
	if strings.Contains(groundWords, deliveryShapeScratch) {
		t.Fatalf("the row ground must not be reported as a previous run's scratch clone:\n%s", groundWords)
	}

	// AND THE GATE'S OWN SENTENCE PROMISES NOTHING IT DID NOT MEASURE: it quotes
	// the count taken against the REAL base tip, and it states a shape.
	_, blk, err := AttemptDelivery(ctx, cfgWithWorktree(cfg, dir), dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk == nil || blk.Kind != BlockerKindEmpty {
		t.Fatalf("want an empty blocker, got %+v", blk)
	}
	report := blk.Report()
	if !strings.Contains(report, ground) {
		t.Fatalf("the refusal must offer the candidate that carries the row's ref ahead of the base:\n%s", report)
	}
	if !strings.Contains(report, deliveryShapeRowGround) {
		t.Fatalf("the offered candidate's shape must be stated as measured:\n%s", report)
	}
	if !strings.Contains(report, strconv.Itoa(groundAhead)) {
		t.Fatalf("the sentence must quote the count measured against %s (%d):\n%s",
			integration, groundAhead, report)
	}
	t.Logf("the hinted refusal: %s", report)
}

// TestDeliverySaysWhenASiblingsCountCouldNotBeMeasured is the ABSTENTION arm.
// A candidate carries this row's address ref but cannot read the commit the
// count is taken against (a clone taken before the base moved, and no fetch is
// this leg's to run). Nothing was measured there, so nothing may be asserted:
// the sentence says which directories, which ref and which tip, and says it was
// NOT measured — never a number, never a shape.
func TestDeliverySaysWhenASiblingsCountCouldNotBeMeasured(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	cfg.GroundBranch = "task/" + testTaskID
	cfg.BaseBranch = DefaultBranch
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)

	// THE CANDIDATE: cloned now, so it holds no object the base gains LATER.
	cand := filepath.Join(cfg.Root, "01a17da2-0000-7000-8000-00000000old")
	gitIn(t, cfg.Root, "clone", "-q", origin, cand)
	gitIn(t, cand, "checkout", "-q", "-b", cfg.GroundBranch)
	// The base moves on, and the row's workspace pulls it in; the candidate does
	// NOT fetch, so its object store stops where it was cloned.
	commitIn(t, origin, "later.txt", "the base moves after the candidate was taken")
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open (bring the workspace up to the base that moved): %v", err)
	}

	ctx := context.Background()
	baseTip, _, berr := deliveryBaseTip(ctx, dir, DefaultBranch, cfg.GroundBranch)
	if berr != nil {
		t.Fatalf("base tip: %v", berr)
	}
	// THE FIXTURE'S SHAPE, measured: the candidate cannot read that tip.
	if deliveryCarriesCommit(ctx, cand, baseTip) {
		t.Fatalf("fixture: the candidate must NOT hold the base's new tip %s", baseTip)
	}

	lead := deliveryEmptySibling(ctx, cfg, dir, DefaultBranch, baseTip)
	if !strings.Contains(lead, deliveryUnmeasuredMark) {
		t.Fatalf("an unmeasurable candidate must be reported as NOT measured:\n%s", lead)
	}
	if strings.Contains(lead, cand) {
		t.Fatalf("an unmeasurable candidate must not be OFFERED as a lead either:\n%s", lead)
	}
	if strings.Contains(lead, deliveryShapeRowGround) || strings.Contains(lead, deliveryShapeScratch) {
		t.Fatalf("no shape may be stated for a candidate nothing was measured in:\n%s", lead)
	}
	t.Logf("the abstention: %s", lead)
}

// TestDeliveryEmptyRefusalNamesTheCommitsTheGroundIsMISSING is the red-proof for
// row 01a1826e: the empty refusal asserted "there is nothing to pull and nothing
// to push" on the strength of ONE reading (`HEAD --not origin/master` = 0), and
// that reading says nothing about what the ground LACKS. Measured on that row's
// own ground, the same refusal said "nothing to pull" while
// `rev-list --count origin/master --not HEAD` answered 4 — and the operator (and
// this agent) spent the first two rounds proving the sentence wrong instead of
// delivering.
//
// The clause is now chosen by a count of its own. This test pins BOTH arms, so a
// future simplification cannot re-derive one fact from the other:
//
//	① BEHIND: origin has landed commits this ground does not have → the refusal
//	  names the count and the command that recovers them, and it does NOT say
//	  "nothing to pull".
//	② LEVEL (the control): the ground is level with its base → the original
//	  spelling stands, so the fix does not turn every empty refusal into a
//	  "behind" report it never measured.
func TestDeliveryEmptyRefusalNamesTheCommitsTheGroundIsMISSING(t *testing.T) {
	ctx := context.Background()

	setup := func(t *testing.T) (Config, string, string) {
		t.Helper()
		origin := newOrigin(t)
		cfg := cloneCfg(t, origin)
		if _, err := Open(ctx, cfg); err != nil {
			t.Fatalf("Open: %v", err)
		}
		return cfg, origin, Dir(cfg.Root, cfg.TaskID)
	}

	t.Run("behind", func(t *testing.T) {
		cfg, origin, dir := setup(t)
		// THE BASE MOVES AFTER THE GROUND WAS CUT — the shape of every real run
		// whose task sat in a queue while master took other merges.
		commitIn(t, origin, "landed-later.txt", "a commit the ground has never seen")
		gitIn(t, dir, "fetch", "-q", "origin")

		// THE PRECONDITION, MEASURED (a test that asserts about a count must
		// first establish the count is what it thinks): 0 ahead, >0 behind.
		baseTip, integration, berr := deliveryBaseTip(ctx, dir, DefaultBranch, cfg.GroundBranch)
		if berr != nil {
			t.Fatalf("base tip: %v", berr)
		}
		if n, err := deliveryOutstanding(ctx, dir, gitHeadRef, baseTip); err != nil || n != 0 {
			t.Fatalf("fixture precondition: commits ahead = %d (err %v), want 0", n, err)
		}
		behind, berr := deliveryOutstanding(ctx, dir, baseTip, gitHeadRef)
		if berr != nil || behind == 0 {
			t.Fatalf("fixture precondition: commits behind = %d (err %v), want >0", behind, berr)
		}

		_, blk, err := AttemptDelivery(ctx, cfgWithWorktree(cfg, dir), dir)
		if err != nil {
			t.Fatalf("an empty workspace is a BLOCKER, not an error: %v", err)
		}
		// THE THIRD KIND, and this arm is its fixture: `ahead == 0` while the base
		// has moved past this ground — HEAD is a STRICT ancestor of the base tip,
		// i.e. the content is already there and the base has landed more since.
		// Reporting that as EMPTY is what kept the delivery loop re-measuring an
		// immutable fact once per round (epic: inherited delivery).
		if blk == nil || blk.Kind != BlockerKindInheritedDelivery {
			t.Fatalf("a ground the base has moved past with nothing of its own to publish has INHERITED "+
				"its delivery: want %q, got %+v", BlockerKindInheritedDelivery, blk)
		}
		report := blk.Report()
		if !strings.Contains(report, deliveryInheritedDeliveryMarker) {
			t.Fatalf("the inherited sentence must state the fact it measured:\n%s", report)
		}

		// ① THE MEASUREMENT IS IN THE SENTENCE, with its count and the fact that
		// the base already contains this ground's HEAD. THE KIND IS INHERITED —
		// this fixture fetches the base forward, so HEAD is a STRICT ancestor of
		// the peer's tip, and row 01a19483 is exactly that: the shape must come
		// from the peer, not from the ref the fetch refreshed. No `pull --rebase`
		// recovery is offered, because a pull cannot make content the base already
		// has into a delivery (the base contains HEAD).
		for _, want := range []string{
			fmt.Sprintf("%d commit(s) this ground does not have", behind),
			integration,
			deliveryInheritedDeliveryMarker,
		} {
			if !strings.Contains(report, want) {
				t.Errorf("the refusal must carry %q:\n%s", want, report)
			}
		}
		if strings.Contains(report, "recovers them") {
			t.Fatalf("an inherited delivery must not offer `pull --rebase` as a recovery:\n%s", report)
		}
		// ② THE FALSE CLAIM IS GONE, and the pull is not re-accused either: the
		// fact is "publishing has nothing to do", never "the pull was refused".
		if strings.Contains(report, deliveryEmptyNothingToMoveText) {
			t.Fatalf("a ground %d commit(s) BEHIND must not be told there is nothing to pull:\n%s", behind, report)
		}
		if strings.Contains(report, "pull step refused") || strings.Contains(report, "conflicted path") {
			t.Fatalf("the two ghosts must stay dead:\n%s", report)
		}
		t.Logf("the behind refusal: %s", report)
	})

	t.Run("level", func(t *testing.T) {
		cfg, _, dir := setup(t)
		baseTip, _, berr := deliveryBaseTip(ctx, dir, DefaultBranch, cfg.GroundBranch)
		if berr != nil {
			t.Fatalf("base tip: %v", berr)
		}
		if behind, err := deliveryOutstanding(ctx, dir, baseTip, gitHeadRef); err != nil || behind != 0 {
			t.Fatalf("fixture precondition: commits behind = %d (err %v), want 0", behind, err)
		}
		_, blk, err := AttemptDelivery(ctx, cfgWithWorktree(cfg, dir), dir)
		if err != nil {
			t.Fatalf("AttemptDelivery: %v", err)
		}
		if blk == nil || blk.Kind != BlockerKindEmpty {
			t.Fatalf("want the empty blocker, got %+v", blk)
		}
		report := blk.Report()
		if !strings.Contains(report, deliveryEmptyNothingToMoveText) {
			t.Fatalf("a ground LEVEL with its base must keep the original spelling:\n%s", report)
		}
		if strings.Contains(report, "this ground does not have") {
			t.Fatalf("a level ground must not be reported as missing commits:\n%s", report)
		}
	})
}
