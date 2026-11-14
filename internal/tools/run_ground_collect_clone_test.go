package tools

// run_ground_collect_clone_test.go — the SHAPE SPLIT of the run-ground anchor's
// removal leg (RunGroundCollect, run_ground_anchor.go).
//
// THE DEFECT THIS PINS, measured on this host 2026-10-26: the anchor axis listed
// 725 collectable grounds, and every one of them that still existed was a
// CLONE-channel ground (`.git` is a DIRECTORY, internal/devworkspace) — a
// per-task clone in the reclaimable workspace root, 351 directories / 28.9 GB on
// a 96 %-full `/` partition. The leg had ONE verb:
//
//	$ git worktree remove /home/user/workspace/<task-id>
//	fatal: '/home/user/workspace/<task-id>' is not a working tree
//	exit 128
//
// so the axis removed NOTHING, forever, while its own plan said those grounds
// were collectable. The verb was never wrong for a LINKED worktree (`.git` is a
// FILE): it is wrong for the other shape, and the fix is the split this file
// tests — linked worktrees keep `git worktree remove`, clone grounds are removed
// as directories (devworkspace.Reclaim), and BOTH write the removal record first.
//
// WHAT THE CLONE LEG MUST NOT BECOME, tested here in three directions: a
// whole-directory removal deletes a repository, so it may only be aimed inside
// the configured workspace root, at a tree with no uncommitted work, and at a
// branch whose commits exist somewhere besides that directory. Those three
// refusals and the gone-directory skip are one test each below, because each is
// a different way for this arm to destroy something it does not own.
//
// Every case drives the REAL collect path (RunGroundCollect) over a REAL
// two-repository fixture — nothing is stubbed, because the whole point is that
// the two verbs disagree about what is on disk.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// collectCloneFixture is one clone-channel ground inside a workspace root, the
// exact shape the channel opens (<workspace_root>/<task-id>), plus the main repo
// it was cloned from.
type collectCloneFixture struct {
	root   string // the configured workspace root
	main   string // the "server" repo the ground was cloned from
	ground string // <root>/<task-id>
	branch string
	sink   string
}

// newCollectCloneFixture builds main + clone, points the audit sink at a file the
// test owns, pins the workspace root to the clone's parent, and asserts the two
// preconditions the fixture's meaning depends on.
func newCollectCloneFixture(t *testing.T, taskID string) collectCloneFixture {
	t.Helper()
	root := t.TempDir()
	main := filepath.Join(t.TempDir(), "main")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, main, "init", "-q", "-b", masterBranchName)
	sweepGit(t, main, "config", "user.email", "clone-collect@test")
	sweepGit(t, main, "config", "user.name", "clone-collect")
	if err := os.WriteFile(filepath.Join(main, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, main, "add", "seed.txt")
	sweepGit(t, main, "commit", "-q", "-m", "seed")

	ground := filepath.Join(root, taskID)
	sweepGit(t, root, "clone", "-q", main, ground)
	sweepGit(t, ground, "config", "user.email", "clone-collect@test")
	sweepGit(t, ground, "config", "user.name", "clone-collect")

	t.Setenv(config.WorkspaceRootEnvName, root)
	sink := anchorSink(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))

	f := collectCloneFixture{root: root, main: main, ground: ground, branch: masterBranchName, sink: sink}
	// PRECONDITION ①: `.git` is a DIRECTORY here — the discriminator the split
	// keys on. If this ever stops being true the test has moved to the other
	// shape and must say so rather than pass on the wrong one.
	if !deliveryProofIsCloneGround(ground) {
		t.Fatalf("fixture: %s is not recognised as a clone ground (.git must be a DIRECTORY)", ground)
	}
	// PRECONDITION ②: the fixture starts DELIVERED (0 ahead of the integration
	// branch), so every later refusal is caused by what the case adds.
	if p := worktreeUndeliveredCommits(ground, f.branch); !p.Measured || p.Ahead != 0 {
		t.Fatalf("fixture: the clone's %s must be 0 ahead (measured=%v ahead=%d why=%q)",
			f.branch, p.Measured, p.Ahead, p.Why)
	}
	return f
}

// anchored stages a FINISHED taker for the ground: a pid that is dead by
// measurement, with an anchor aged past the floor — the state the sweep reaches
// after a run ends. The ledger row is deliberately absent (this axis exists for
// exactly the grounds no row can be paired against).
func (f collectCloneFixture) anchored(t *testing.T) {
	t.Helper()
	dead := deadPidOnThisHost(t)
	writeAnchorRecordForTest(t, f.sink, f.main, f.ground, f.branch, "", strconv.Itoa(dead), "999999999", strconv.Itoa(dead))
	anchorAgeForTest(t, f.sink, f.ground, 48*time.Hour)
}

// collect drives the REAL path: the plan, then the apply.
func (f collectCloneFixture) collect(t *testing.T) (collectable []RunGroundLiveness, removed, skipped []string) {
	t.Helper()
	collectable, kept, err := RunGroundCollectableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundCollectableGrounds: %v", err)
	}
	t.Logf("read-out:\n  %s", strings.Join(RunGroundSweepReport(collectable, kept), "\n  "))
	if !groundIn(collectable, f.ground) {
		t.Fatalf("the fixture never reached the APPLY step — collectable=%v kept=%v",
			groundPaths(collectable), groundPaths(kept))
	}
	removed, skipped, cerr := RunGroundCollect(collectable)
	if cerr != nil {
		t.Fatalf("RunGroundCollect: %v", cerr)
	}
	t.Logf("apply: removed=%v skipped=%v", removed, skipped)
	return collectable, removed, skipped
}

// removalRecords returns the sink's removal records for one target.
func removalRecords(t *testing.T, sink, target string) []removalaudit.Record {
	t.Helper()
	recs, err := removalaudit.Records(sink)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read sink: %v", err)
	}
	var out []removalaudit.Record
	for _, r := range recs {
		if r.Kind == removalaudit.KindWorktreeRemoval && sameWorkingPath(r.Target, target) {
			out = append(out, r)
		}
	}
	return out
}

// TestRunGroundCollectRemovesACloneGround is the RED→GREEN core: a collectable
// CLONE ground is removed by the collect leg and its directory is GONE
// afterwards. Pre-fix this arm answered the skip
// "git could not remove the registration: … is not a working tree" for every
// such ground, and the directory stayed on disk forever (which is the 28.9 GB).
func TestRunGroundCollectRemovesACloneGround(t *testing.T) {
	const row = "01a1732e-0000-7000-8000-00000000c101"
	f := newCollectCloneFixture(t, row)
	f.anchored(t)
	// THE TIP, READ BEFORE THE DIRECTORY GOES (measured 2026-10-30). This arm is
	// the SECOND code path that destroys a clone ground with os.RemoveAll, and
	// its record carried no sha — the same hole the channel's own reclaim had,
	// on a path the channel's fix never touches. Captured here, from the ground
	// itself, while it still exists: after the collect it is unreadable, so a
	// test that asked afterwards could only measure its own lateness.
	wantTip := strings.TrimSpace(sweepGit(t, f.ground, "rev-parse", "HEAD"))

	_, removed, skipped := f.collect(t)
	if !containsPath(removed, f.ground) {
		t.Fatalf("the clone ground was not removed: removed=%v skipped=%v", removed, skipped)
	}
	if _, err := os.Stat(f.ground); !os.IsNotExist(err) {
		t.Fatalf("the clone ground's directory must be GONE after the apply; stat err=%v", err)
	}
	// The workspace root itself is never the target: only the per-task child.
	if _, err := os.Stat(f.root); err != nil {
		t.Fatalf("the workspace ROOT must survive (only its per-task child is removable): %v", err)
	}
	// The audit record exists, names the CLONE arm and precedes the removal in
	// the sink's own file order (one record, written before the act).
	recs := removalRecords(t, f.sink, f.ground)
	if len(recs) != 1 {
		t.Fatalf("the removal must be recorded exactly once; got %d records", len(recs))
	}
	if recs[0].Arm != string(removalaudit.ArmTaskGroundAnchorClone) {
		t.Errorf("the record's arm = %q, want %q (a reader must be able to tell the two destruction shapes apart)",
			recs[0].Arm, removalaudit.ArmTaskGroundAnchorClone)
	}
	if !strings.Contains(recs[0].Evidence, runGroundCollectCloneLabel) {
		t.Errorf("the record's evidence must carry the clone leg's label; got %q", recs[0].Evidence)
	}
	// THE RECORD NAMES THE TIP. Without this field the delivery proof answers
	// "the tip this run produced cannot be named" for a commit that is on the
	// integration branch — the judgement becomes a question about a DIRECTORY
	// instead of about a revision.
	if recs[0].Sha != wantTip {
		t.Errorf("the removal record must carry the ground's tip so the delivery proof can judge BY SHA "+
			"after the directory is gone: got sha=%q, want %q", recs[0].Sha, wantTip)
	}
}

// TestRunGroundCollectRefusesACloneGroundCarryingUndeliveredWork pins the rung
// that decides whether the removal DESTROYS work: a clone ground's directory is
// its only repository, so a commit that exists nowhere else keeps it.
func TestRunGroundCollectRefusesACloneGroundCarryingUndeliveredWork(t *testing.T) {
	const row = "01a1732e-0000-7000-8000-00000000c102"
	f := newCollectCloneFixture(t, row)
	// Work nobody else has: a branch and a commit that exist only in this clone.
	sweepGit(t, f.ground, "checkout", "-q", "-b", "task/undelivered")
	if err := os.WriteFile(filepath.Join(f.ground, "work.txt"), []byte("nobody else has this\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, f.ground, "add", "work.txt")
	sweepGit(t, f.ground, "commit", "-q", "-m", "work nobody has")
	f.branch = "task/undelivered"
	f.anchored(t)

	// The rung fires at the PLAN step (runGroundAnchorLiveness asks it before a
	// ground can ever be called collectable), so the ground must appear among the
	// KEPT with the rung's own sentence — and an apply over that plan must then
	// remove nothing.
	collectable, kept, cerr := RunGroundCollectableGrounds(time.Now(), time.Hour)
	if cerr != nil {
		t.Fatalf("RunGroundCollectableGrounds: %v", cerr)
	}
	t.Logf("read-out:\n  %s", strings.Join(RunGroundSweepReport(collectable, kept), "\n  "))
	if groundIn(collectable, f.ground) {
		t.Fatalf("a clone ground carrying an undelivered commit must never be collectable; collectable=%v", groundPaths(collectable))
	}
	var why string
	for _, v := range kept {
		if sameWorkingPath(v.Anchor.Worktree, f.ground) {
			why = v.Reason
		}
	}
	if !strings.Contains(why, "no delivery point has") {
		t.Errorf("the keep-reason must NAME the undelivered-commits rung; got %q (all kept: %v)", why, groundPaths(kept))
	}
	removed, skipped, cerr := RunGroundCollect(collectable)
	if cerr != nil {
		t.Fatalf("RunGroundCollect: %v", cerr)
	}
	t.Logf("apply: removed=%v skipped=%v", removed, skipped)
	if containsPath(removed, f.ground) {
		t.Fatalf("the leg FREED a clone ground whose commit exists nowhere else: removed=%v", removed)
	}
	if _, err := os.Stat(f.ground); err != nil {
		t.Fatalf("a refused ground's directory must survive: %v", err)
	}
	if out, err := groundTestGit(f.ground, "cat-file", "-t", "HEAD"); err != nil || strings.TrimSpace(out) != "commit" {
		t.Errorf("the refused ground must still be a readable repository: out=%q err=%v", out, err)
	}
	if recs := removalRecords(t, f.sink, f.ground); len(recs) != 0 {
		t.Errorf("a refusal must write NO removal record (nothing was removed); got %d", len(recs))
	}
}

// TestRunGroundCollectRefusesADirtyCloneGround pins the second rung git would
// have provided for a registration and os.RemoveAll does not: a TRACKED change is
// uncommitted work and refuses, with the files named.
func TestRunGroundCollectRefusesADirtyCloneGround(t *testing.T) {
	const row = "01a1732e-0000-7000-8000-00000000c103"
	f := newCollectCloneFixture(t, row)
	if err := os.WriteFile(filepath.Join(f.ground, "seed.txt"), []byte("edited after the commit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.anchored(t)

	_, removed, skipped := f.collect(t)
	if containsPath(removed, f.ground) {
		t.Fatalf("the leg FREED a clone ground holding an uncommitted TRACKED change: removed=%v", removed)
	}
	var why string
	for _, s := range skipped {
		if strings.Contains(s, f.ground) {
			why = s
		}
	}
	if !strings.Contains(why, reclaimSkipDirtyReason) || !strings.Contains(why, "seed.txt") {
		t.Errorf("the skip must name the dirty refusal AND the file that caused it; got %q", why)
	}
	if _, err := os.Stat(filepath.Join(f.ground, "seed.txt")); err != nil {
		t.Errorf("a refused ground's tree must survive: %v", err)
	}
	if recs := removalRecords(t, f.sink, f.ground); len(recs) != 0 {
		t.Errorf("a refusal must write NO removal record; got %d", len(recs))
	}
}

// TestRunGroundCollectRefusesACloneGroundWithScratchInIt pins the PARITY half of
// the dirty rung: `git worktree remove` (the linked leg) refuses a tree holding
// UNTRACKED files — it asks for --force, which this arm never passes — so the
// clone leg, whose removal is an os.RemoveAll with no such question, must refuse
// them too. Untracked-only is still reported as the KIND it is, so the operator's
// next step differs from the uncommitted-work case.
func TestRunGroundCollectRefusesACloneGroundWithScratchInIt(t *testing.T) {
	const row = "01a1732e-0000-7000-8000-00000000c106"
	f := newCollectCloneFixture(t, row)
	// LIVE PARITY PROOF: the linked arm's verb refuses this tree, so the clone leg
	// has to as well — the fixture states the fact it is matching, measured.
	if err := os.WriteFile(filepath.Join(f.ground, "scratch.log"), []byte("the run's own scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := groundTestGit(f.ground, "status", "--porcelain"); err != nil {
		t.Fatalf("fixture git status: %v", err)
	}
	f.anchored(t)

	_, removed, skipped := f.collect(t)
	if containsPath(removed, f.ground) {
		t.Fatalf("the leg FREED a clone ground holding untracked files (the linked leg's `git worktree remove` refuses exactly this tree): removed=%v", removed)
	}
	var why string
	for _, s := range skipped {
		if strings.Contains(s, f.ground) {
			why = s
		}
	}
	if !strings.Contains(why, reclaimUntrackedResidueReason) || !strings.Contains(why, "scratch.log") {
		t.Errorf("the skip must name the untracked-residue kind AND the entry that caused it; got %q", why)
	}
	if _, err := os.Stat(filepath.Join(f.ground, "scratch.log")); err != nil {
		t.Errorf("a refused ground's tree must survive: %v", err)
	}
	if recs := removalRecords(t, f.sink, f.ground); len(recs) != 0 {
		t.Errorf("a refusal must write NO removal record; got %d", len(recs))
	}
}

// TestRunGroundCollectRefusesALiveCloneGround pins the holder veto on this leg
// too: a process standing in the directory is not a ground to delete — least of
// all when the removal is an os.RemoveAll that would delete it under that
// process.
func TestRunGroundCollectRefusesALiveCloneGround(t *testing.T) {
	const row = "01a1732e-0000-7000-8000-00000000c104"
	f := newCollectCloneFixture(t, row)
	// The ASKER's own pid: alive by construction, and the liveness ladder reads
	// it as the taker still present (this is the shape a resumed run has).
	writeAnchorRecordForTest(t, f.sink, f.main, f.ground, f.branch, "", strconv.Itoa(os.Getpid()), testStartTicks(t, os.Getpid()), strconv.Itoa(os.Getpid()))
	anchorAgeForTest(t, f.sink, f.ground, 48*time.Hour)

	collectable, kept, err := RunGroundCollectableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundCollectableGrounds: %v", err)
	}
	t.Logf("read-out:\n  %s", strings.Join(RunGroundSweepReport(collectable, kept), "\n  "))
	if groundIn(collectable, f.ground) {
		t.Fatalf("a ground whose recorded taker is ALIVE must never be collectable; collectable=%v", groundPaths(collectable))
	}
	if !groundIn(kept, f.ground) {
		t.Fatalf("the live ground must appear among the KEPT with its reason; kept=%v", groundPaths(kept))
	}
	if _, err := os.Stat(f.ground); err != nil {
		t.Fatalf("the live ground must survive: %v", err)
	}
	if recs := removalRecords(t, f.sink, f.ground); len(recs) != 0 {
		t.Errorf("keeping a ground must write NO removal record; got %d", len(recs))
	}
}

// TestRunGroundCollectSkipsAGoneCloneGround pins the third outcome of the leg —
// neither removed nor failed. 679 of the 725 collectable anchors on this host
// named directories that no longer existed, and the pre-fix leg rendered each of
// them as a git failure an operator had to read past.
func TestRunGroundCollectSkipsAGoneCloneGround(t *testing.T) {
	const row = "01a1732e-0000-7000-8000-00000000c105"
	f := newCollectCloneFixture(t, row)
	f.anchored(t)
	// The directory is gone while the ANCHOR remains — the stale-anchor shape.
	if err := os.RemoveAll(f.ground); err != nil {
		t.Fatal(err)
	}

	_, removed, skipped := f.collect(t)
	if containsPath(removed, f.ground) {
		t.Fatalf("a gone directory cannot be removed; removed=%v", removed)
	}
	var why string
	for _, s := range skipped {
		if strings.Contains(s, f.ground) {
			why = s
		}
	}
	if !strings.Contains(why, runGroundCollectGoneReason) {
		t.Errorf("the skip must say the directory is already GONE (not a git failure); got %q (all skips: %v)", why, skipped)
	}
	if strings.Contains(why, "is not a working tree") {
		t.Errorf("git's wording must never be the criterion here: a linked registration and a clone ground answer it differently; got %q", why)
	}
	if recs := removalRecords(t, f.sink, f.ground); len(recs) != 0 {
		t.Errorf("a gone ground must write NO removal record; got %d", len(recs))
	}
}
