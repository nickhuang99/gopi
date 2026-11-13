package tools

// run_ground_adopt_test.go — the BACKFILL leg of the run-ground anchor axis
// (run_ground_adopt.go), pinned in both directions.
//
// THE GAP THIS CLOSES. Every reclaim rung on the anchor axis keys on a RECORD:
// RunGroundAnchors reads kind=worktree-ground out of the removal-audit sink, and
// a directory with no such line is invisible to the whole axis — not kept, not
// collectable, not even counted. The producer that writes those lines only began
// writing them at commit 3feaf4a (task 01a17b5b), and it is FORWARD-ONLY, so
// every clone ground the older producer opened is permanently unnameable: on
// this host that population measured 14 uuid-shaped clone grounds / 2150 MB on a
// 96 %-full `/` partition (evidence/01-before.txt of this run).
//
// WHAT THE BACKFILL MAY NOT BECOME. The anchor's design principle is that
// ownership is RECORDED BY THE OWNER AT ACQUISITION and never reconstructed from
// /proc absence — so a backfill anchor may not invent a taker. It records
// `unrecorded` in all three identity fields and carries its own MEASUREMENT in
// the evidence field instead: no live process stands in the ground (the shared
// cwd/fd/exe guard every other removal arm consults), the tree is clean, and the
// branch holds no commit that no delivery point has. The liveness ladder then
// answers from that measurement through a NAMED rung rather than through the
// launch ladder, which has no identity to ask about.
//
// Every case below drives the REAL criterion (RunGroundAdoptableGrounds) and the
// REAL writer (RunGroundAdopt) over a REAL two-repository fixture, and then asks
// the REAL reader (RunGroundCollectableGrounds) what the axis now thinks.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/removalaudit"
	"github.com/nick/gopi/internal/testutil"
)

// adoptGroundName is a canonical task-id-shaped directory name — the shape the
// backfill claims (an auto-minted clone ground), which is why the fixture is
// named like a row and not like a hand-made clone.
const adoptGroundName = "01a17baf-d099-7f00-9681-e5d4e5873d9f"

// newAdoptFixture builds one clone ground with NO anchor record, inside the
// configured workspace root, on top of a main repo it is fully pushed to.
func newAdoptFixture(t *testing.T) collectCloneFixture {
	t.Helper()
	f := newCollectCloneFixture(t, adoptGroundName)
	// The ground must be OLDER than the floor the case asks with. The clone was
	// created seconds ago, so the fixture ages the directory itself — the same
	// input the criterion reads (os.Stat's mtime).
	ageGround(t, f.ground)
	return f
}

// ageGround pushes the directory's mtime past any test's floor. A DIRTY case
// must call it AFTER it writes its loose file: writing at the ground's root
// moves the mtime, and an unaged ground would then be refused by the AGE rung
// instead of the rung under test (measured while writing these cases — the
// assertion failed with "younger than the floor" and told us the rung order).
func ageGround(t *testing.T, dir string) {
	t.Helper()
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("age the ground: %v", err)
	}
}

// TestRunGroundAdoptTakesTheUnanchoredDeliveredGround is the POSITIVE case and
// the whole point of the task: a ground nobody ever anchored, with no live
// process in it and no undelivered commit, must reach the axis.
//
// RED BEFORE THE FIX: before run_ground_adopt.go existed the directory was
// invisible to the axis by construction — RunGroundAnchors never returned it, so
// neither list named it (checked below by the first assertion, which is how this
// test states the gap rather than assuming it).
func TestRunGroundAdoptTakesTheUnanchoredDeliveredGround(t *testing.T) {
	f := newAdoptFixture(t)
	testutil.NewTestDBPath(t)

	// THE GAP, measured rather than asserted from prose: with no anchor, the
	// axis cannot see the ground at all.
	before, beforeKept, err := RunGroundCollectableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundCollectableGrounds (before): %v", err)
	}
	if groundIn(before, f.ground) || groundIn(beforeKept, f.ground) {
		t.Fatalf("precondition: an UNANCHORED ground must be invisible to the axis, but it appeared (collectable=%v kept=%v)",
			groundPaths(before), groundPaths(beforeKept))
	}

	adoptable, refused, err := RunGroundAdoptableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundAdoptableGrounds: %v", err)
	}
	t.Logf("adoptable=%v refused=%v", adoptPaths(adoptable), adoptPaths(refused))
	if !adoptIn(adoptable, f.ground) {
		t.Fatalf("the delivered, clean, unowned clone ground was not adoptable (refused=%v)", adoptPaths(refused))
	}

	adopted, skipped, err := RunGroundAdoptGrounds(adoptable)
	if err != nil {
		t.Fatalf("RunGroundAdopt: %v", err)
	}
	if !containsPath(adopted, f.ground) {
		t.Fatalf("RunGroundAdopt did not adopt %s (adopted=%v skipped=%v)", f.ground, adopted, skipped)
	}

	// The record the writer left: an anchor for THIS directory whose identity
	// fields are all `unrecorded` — a fabricated pid would be the exact
	// reconstruction the design forbids.
	anchors := anchorsIn(t, f.sink)
	var got *RunGroundAnchor
	for i := range anchors {
		if sameWorkingPath(anchors[i].Worktree, f.ground) {
			got = &anchors[i]
		}
	}
	if got == nil {
		t.Fatalf("no anchor record for %s after adoption (anchors=%v)", f.ground, anchorPaths(anchors))
	}
	if got.PID != runGroundAnchorUnrecorded || got.StartTicks != runGroundAnchorUnrecorded || got.Session != runGroundAnchorUnrecorded {
		t.Fatalf("the backfill FABRICATED a taker: pid=%q ticks=%q session=%q — every identity field must be %q",
			got.PID, got.StartTicks, got.Session, runGroundAnchorUnrecorded)
	}
	if !got.Backfilled {
		t.Fatalf("the record does not say it was a BACKFILL (arm %q)", got.Arm)
	}

	// And now the axis can name it: the ground is COLLECTABLE, through the
	// named backfill rung, with a reason an operator can read.
	collectable, kept, err := RunGroundCollectableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundCollectableGrounds (after): %v", err)
	}
	if !groundIn(collectable, f.ground) {
		t.Fatalf("the adopted ground is not collectable — kept=%v", groundPaths(kept))
	}
	reason := reasonForGround(collectable, f.ground)
	if !strings.Contains(reason, runGroundAdoptTakerReason) {
		t.Fatalf("the collectable reason does not carry the named backfill rung: %q", reason)
	}
}

// TestRunGroundAdoptRefusesALiveGround pins the veto that keeps the backfill from
// becoming a licence: a real process standing in the directory keeps it, no
// matter how old or how delivered it is. The holder is REAL (a `sleep` whose cwd
// is the ground), never a forged pid.
func TestRunGroundAdoptRefusesALiveGround(t *testing.T) {
	f := newAdoptFixture(t)
	testutil.NewTestDBPath(t)
	stop := groundInUseLiveChild(t, f.ground)
	defer stop()

	adoptable, refused, err := RunGroundAdoptableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundAdoptableGrounds: %v", err)
	}
	if adoptIn(adoptable, f.ground) {
		t.Fatalf("a ground holding a LIVE process was adoptable (refused=%v)", adoptPaths(refused))
	}
	if !adoptIn(refused, f.ground) {
		t.Fatalf("the live ground was not even reported as refused (adoptable=%v)", adoptPaths(adoptable))
	}
	if why := adoptReasonFor(refused, f.ground); !strings.Contains(why, runGroundAdoptOccupiedReason) {
		t.Fatalf("the refusal does not name the occupancy rung: %q", why)
	}
}

// TestRunGroundAdoptRefusesUndeliveredWork is the clone leg's third refusal,
// reached through the backfill's own criterion: a ground whose branch holds
// commits no delivery point has is the ONLY home of that work, so it may not be
// adopted into "nobody owns this".
func TestRunGroundAdoptRefusesUndeliveredWork(t *testing.T) {
	f := newAdoptFixture(t)
	testutil.NewTestDBPath(t)
	if err := os.WriteFile(filepath.Join(f.ground, "local-only.txt"), []byte("unpushed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, f.ground, "add", "local-only.txt")
	sweepGit(t, f.ground, "commit", "-q", "-m", "local-only work")
	ageGround(t, f.ground)

	adoptable, refused, err := RunGroundAdoptableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundAdoptableGrounds: %v", err)
	}
	if adoptIn(adoptable, f.ground) {
		t.Fatalf("a ground holding UNDELIVERED commits was adoptable (refused=%v)", adoptPaths(refused))
	}
	if why := adoptReasonFor(refused, f.ground); !strings.Contains(why, "no delivery point has") {
		t.Fatalf("the refusal does not quote the delivery rung: %q", why)
	}
	// THE RUNG MUST BE NAMED BY WHAT IT IS. This ground's tree is CLEAN (the
	// fixture commits everything it writes), so a refusal that opens with the
	// cleanliness prefix is a wrong attribution — the operator's repair is "push
	// the branch or merge it", never "clean the tree", and the old caller stamped
	// that prefix onto all three clone-leg rungs. Measured cost of the defect on
	// this host 2026-10-27: 5 of 7 prefixed refusals in one real dry run were this
	// rung over an empty `git status --porcelain`.
	why := adoptReasonFor(refused, f.ground)
	if strings.Contains(why, runGroundAdoptDirtyPrefix) {
		t.Fatalf("a CLEAN ground refused by the DELIVERY rung was reported as dirty — the repair it names is not the one that applies: %q", why)
	}
	if !strings.Contains(why, runGroundAdoptRungDeliveryPrefix) {
		t.Fatalf("the refusal does not carry the DELIVERY rung's own name: %q", why)
	}
	// And the premise, measured rather than assumed: the fixture really is clean,
	// so the assertion above is about the SENTENCE and not about a dirty tree.
	if out, gerr := groundTestGit(f.ground, "status", "--porcelain"); gerr != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("precondition: the fixture's tree must be clean for this case to be about the delivery rung; got %q (err=%v)", out, gerr)
	}
}

// TestRunGroundAdoptRefusesADirtyGround — an uncommitted change is a human's
// work in progress, and the backfill must not be the door that lets a sweeper
// free it. The classifier's own sentence is quoted, so the operator knows which
// kind of dirt it is.
//
// THIS IS THE OTHER HALF OF THE SPLIT: the fix that stopped stamping a dirt
// prefix onto the DELIVERY rung must not make a genuinely dirty tree unnameable.
// A red-proved version of this case (the rung labelled "delivery" for dirt) would
// send the operator to push a branch whose tree is uncommitted work — the same
// wrong instruction, mirrored.
func TestRunGroundAdoptRefusesADirtyGround(t *testing.T) {
	f := newAdoptFixture(t)
	testutil.NewTestDBPath(t)
	if err := os.WriteFile(filepath.Join(f.ground, "loose.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ageGround(t, f.ground)

	adoptable, refused, err := RunGroundAdoptableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundAdoptableGrounds: %v", err)
	}
	if adoptIn(adoptable, f.ground) {
		t.Fatalf("a DIRTY ground was adoptable (refused=%v)", adoptPaths(refused))
	}
	why := adoptReasonFor(refused, f.ground)
	if !strings.Contains(why, runGroundAdoptDirtyPrefix) {
		t.Fatalf("the refusal does not name the cleanliness rung: %q", why)
	}
	// The classifier's SPLIT must survive: an untracked-only tree is called what
	// it is, and the offending entry is quoted back.
	if !strings.Contains(why, reclaimUntrackedResidueMarker) || !strings.Contains(why, "loose.txt") {
		t.Fatalf("the refusal must name the KIND of dirt and quote the entry: %q", why)
	}
	// And it must NOT be labelled as a delivery refusal — that is the rung this
	// test's sibling pins, and the two prefixes must never coincide.
	if strings.Contains(why, runGroundAdoptRungDeliveryPrefix) {
		t.Fatalf("a genuinely DIRTY ground was reported as a DELIVERY refusal: %q", why)
	}
}

// TestRunGroundAdoptRefusesATrackedDirtyGround pins the OTHER cleanliness
// sentence: `reclaimDirtyVerdict` splits untracked-only residue from a tracked
// change, and the tracked half is the one where the operator's next step is a
// human decision. Same rung, same prefix, different body — and the split is what
// the prefix must not flatten.
func TestRunGroundAdoptRefusesATrackedDirtyGround(t *testing.T) {
	f := newAdoptFixture(t)
	testutil.NewTestDBPath(t)
	// A TRACKED file modified in place: not residue, a person's work.
	if err := os.WriteFile(filepath.Join(f.ground, "seed.txt"), []byte("edited after the commit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ageGround(t, f.ground)

	adoptable, refused, err := RunGroundAdoptableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundAdoptableGrounds: %v", err)
	}
	if adoptIn(adoptable, f.ground) {
		t.Fatalf("a ground holding an uncommitted TRACKED change was adoptable (refused=%v)", adoptPaths(refused))
	}
	why := adoptReasonFor(refused, f.ground)
	if !strings.Contains(why, runGroundAdoptDirtyPrefix) || !strings.Contains(why, reclaimSkipDirtyReason) {
		t.Fatalf("the tracked-dirt refusal must name the cleanliness rung AND the tracked-change sentence: %q", why)
	}
	if !strings.Contains(why, "seed.txt") {
		t.Fatalf("the refusal must quote the file that caused it: %q", why)
	}
	if strings.Contains(why, runGroundAdoptRungDeliveryPrefix) {
		t.Fatalf("a TRACKED dirty ground was reported as a DELIVERY refusal: %q", why)
	}
}

// TestRunGroundAdoptRungLabelsAreDistinct is the guard on the function itself: a
// prefix table that collapsed two rungs into one word would restore the defect
// this change removes, and it would do so silently (Rule 15). The rung value, not
// the sentence text, is what decides — so every rung is fed here directly.
func TestRunGroundAdoptRungLabelsAreDistinct(t *testing.T) {
	body := "some rung sentence"
	// The PREFIX each rung renders, which is the part that must distinguish the
	// questions. The body is passed through verbatim (asserted below), so it
	// cannot be what tells the operator which rung fired.
	labelOf := func(rung runGroundCollectCloneRung) string {
		got := runGroundAdoptRungReason(rung, body)
		if !strings.HasSuffix(got, body) {
			t.Fatalf("rung %q: the rung's own sentence must be carried VERBATIM, got %q", rung, got)
		}
		return strings.TrimSuffix(strings.TrimSuffix(got, " — "+body), body)
	}
	// Three questions, three labels. The two cleanliness answers SHARE one label on
	// purpose (both are about the tree, and the body splits tracked work from
	// untracked residue), so they are checked as a group below rather than here.
	for _, pair := range [][2]runGroundCollectCloneRung{
		{runGroundCollectCloneRungUnrooted, runGroundCollectCloneRungDirty},
		{runGroundCollectCloneRungUnrooted, runGroundCollectCloneRungUndelivered},
		{runGroundCollectCloneRungDirty, runGroundCollectCloneRungUndelivered},
	} {
		a, b := labelOf(pair[0]), labelOf(pair[1])
		if a == b {
			t.Fatalf("rungs %q and %q render the SAME label %q — two questions, one sentence restored", pair[0], pair[1], a)
		}
	}
	// Both cleanliness answers name the tree, and only they do. This is the
	// regression the change exists for: the DELIVERY rung used to carry it.
	if labelOf(runGroundCollectCloneRungDirty) != runGroundAdoptDirtyPrefix {
		t.Fatalf("the DIRTY rung no longer carries the cleanliness prefix: %q", labelOf(runGroundCollectCloneRungDirty))
	}
	if labelOf(runGroundCollectCloneRungCleanlinessUnmeasured) != runGroundAdoptDirtyPrefix {
		t.Fatalf("the UNMEASURED-cleanliness rung no longer carries the cleanliness prefix: %q", labelOf(runGroundCollectCloneRungCleanlinessUnmeasured))
	}
	if labelOf(runGroundCollectCloneRungUndelivered) == runGroundAdoptDirtyPrefix {
		t.Fatalf("the DELIVERY rung still carries the cleanliness prefix %q", runGroundAdoptDirtyPrefix)
	}
	if strings.Contains(runGroundAdoptRungReason(runGroundCollectCloneRungUndelivered, body), runGroundAdoptDirtyPrefix) {
		t.Fatalf("a delivery refusal's rendered sentence contains the cleanliness prefix")
	}
}

// TestRunGroundAdoptRefusesANameThisAxisDidNotOpen — the SCOPE rung. The
// backfill's population is the grounds the CLONE CHANNEL opened, and it names
// them by the row id it cloned for (a canonical task id). A directory that is a
// clone ground but carries somebody else's name — a hand-made checkout, a
// fixture, a mirror — is refused BY NAME: claiming it would be the
// unowned-directory grab the origin rung exists to stop, and the message says
// so rather than silently skipping it.
//
// THE WS-ROOT rung is inherited, not re-tested here: it is reached through
// runGroundCollectCloneVerdict, the clone leg's own function, and the scan
// enumerates only entries INSIDE the configured root (so an outside ground is
// not even a candidate). Its own test is
// run_ground_collect_clone_test.go's, which drives the same function.
func TestRunGroundAdoptRefusesANameThisAxisDidNotOpen(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, main, "init", "-q", "-b", masterBranchName)
	sweepGit(t, main, "config", "user.email", "adopt@test")
	sweepGit(t, main, "config", "user.name", "adopt")
	if err := os.WriteFile(filepath.Join(main, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, main, "add", "seed.txt")
	sweepGit(t, main, "commit", "-q", "-m", "seed")

	root := t.TempDir()
	dir := filepath.Join(root, "llvm-project") // a hand-made clone, not a row's ground
	sweepGit(t, root, "clone", "-q", main, dir)
	t.Setenv("GOPI_WORKSPACE_ROOT", root)
	t.Setenv(removalaudit.EnvSink, filepath.Join(t.TempDir(), removalaudit.FileName))
	testutil.NewTestDBPath(t)
	ageGround(t, dir)

	adoptable, refused, err := RunGroundAdoptableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundAdoptableGrounds: %v", err)
	}
	if adoptIn(adoptable, dir) {
		t.Fatalf("a directory whose name is not a task id was adoptable — the axis claimed somebody else's clone")
	}
	if why := adoptReasonFor(refused, dir); !strings.Contains(why, "not a canonical task id") {
		t.Fatalf("the scope refusal does not say why: %q (refused=%v)", why, adoptPaths(refused))
	}
}

// TestRunGroundAdoptIsIdempotent — a second pass adopts nothing: the ground now
// HAS an anchor, and re-adopting would overwrite the record that names the real
// taker of a ground a live run later takes (TaskGroundAnchorRecord's own
// one-line-per-ground contract).
func TestRunGroundAdoptIsIdempotent(t *testing.T) {
	f := newAdoptFixture(t)
	testutil.NewTestDBPath(t)
	writeAnchorRecordForTest(t, f.sink, f.main, f.ground, f.branch, "", "424242", "999999999", "424242")
	anchorAgeForTest(t, f.sink, f.ground, 48*time.Hour)

	adoptable, _, err := RunGroundAdoptableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundAdoptableGrounds: %v", err)
	}
	if adoptIn(adoptable, f.ground) {
		t.Fatalf("a ground that ALREADY has an anchor was adoptable — the backfill overwrote a recorded taker")
	}
}

// --- helpers ---------------------------------------------------------------

func adoptPaths(list []RunGroundAdopt) []string {
	var out []string
	for _, a := range list {
		out = append(out, a.Dir+" ("+a.Reason+")")
	}
	return out
}

func adoptIn(list []RunGroundAdopt, dir string) bool {
	for _, a := range list {
		if sameWorkingPath(a.Dir, dir) {
			return true
		}
	}
	return false
}

func adoptReasonFor(list []RunGroundAdopt, dir string) string {
	for _, a := range list {
		if sameWorkingPath(a.Dir, dir) {
			return a.Reason
		}
	}
	return ""
}

func anchorPaths(list []RunGroundAnchor) []string {
	var out []string
	for _, a := range list {
		out = append(out, a.Worktree+" arm="+a.Arm+" pid="+a.PID)
	}
	return out
}

func reasonForGround(list []RunGroundLiveness, dir string) string {
	for _, v := range list {
		if sameWorkingPath(v.Anchor.Worktree, dir) {
			return v.Reason
		}
	}
	return ""
}

// TestRunGroundBackfillArmIsTheRecordTheReaderRecognises closes the loop the
// naming depends on: the ARM the writer passes is the arm the reader turns into
// Backfilled. Two spellings of that fact would let the writer record a backfill
// the ladder then reads as an anonymous unmeasurable anchor — the state that
// keeps a ground forever.
func TestRunGroundBackfillArmIsTheRecordTheReaderRecognises(t *testing.T) {
	sink := anchorSink(t)
	dir := filepath.Join(t.TempDir(), adoptGroundName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	taskGroundAnchorBackfill(dir, "", "/some/repo", "fixture evidence")
	got := anchorsIn(t, sink)
	if len(got) != 1 {
		t.Fatalf("the backfill wrote %d records, want 1: %v", len(got), anchorPaths(got))
	}
	if got[0].Arm != string(removalaudit.ArmTaskGroundAnchorBackfill) || !got[0].Backfilled {
		t.Fatalf("the record's arm %q does not read back as a backfill (Backfilled=%v)", got[0].Arm, got[0].Backfilled)
	}
	if got[0].PID != runGroundAnchorUnrecorded || got[0].StartTicks != runGroundAnchorUnrecorded || got[0].Session != runGroundAnchorUnrecorded {
		t.Fatalf("backfill fabricated an identity: pid=%q ticks=%q session=%q",
			got[0].PID, got[0].StartTicks, got[0].Session)
	}
}
