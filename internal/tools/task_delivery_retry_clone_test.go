package tools

// A CLONE-GROUND ROW DELIVERS THROUGH THE CLONE CHANNEL — never through the
// close-out chain's integration-branch exemption.
//
// THE DEFECT (bug 01a174c3 / L2, measured live 2026-10-26). A clone ground is a
// clone of its own, and it records `deliverable.branch = master` because the
// channel's own delivery is `push origin HEAD:master`. `task retry-delivery`
// routed every row through `TaskFinishCleanupSessionID`, whose
// `finishCleanupExemptBranchesResolved` SKIPS master/main as the integration
// branch — correct for a chain whose job is to recycle a MERGED FEATURE branch,
// and a permanent block for a row whose whole deliverable is that push:
//
//	$ ./bin/gopi task retry-delivery --task 01a16deb-73df-7a96-851d-97297f619246
//	01a16deb-… blocked branch=master peer=peerbox:/home/user/work/gopi — delivery owed: branch master is exempt
//
// while the ground's HEAD sat on NO peer ref at all. 99 of 103 failed rows on
// this host were that shape.
//
// WHAT THIS FILE PINS, and nothing else:
//
//	① the ROUTE — a clone-ground row's attempt really is the channel's
//	   (devworkspace.AttemptDelivery), proven by the ground's own HEAD reaching
//	   the peer bare repo, asked of the BARE REPO and never of a report;
//	② the REASON — when the channel refuses, the row's reason is the channel's
//	   own words and is NOT the close-out chain's exemption sentence, so an
//	   operator can tell the two failures apart without knowing either file;
//	③ the RED PROOF — the assertion is asked twice in one test: the shipped
//	   router satisfies it, and the pre-fix route (the one-line switch in
//	   `deliveryRetryRouteIsCloneChannel`) does not. A test that only proved ①
//	   would pass just as happily on the branch that caused the bug.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// delCloneSettingsFile is the private settings document this file pins: it
	// declares the fixture repo's build command (Rule 12 — the channel refuses
	// to push what it never built) and its workspace root, so no ambient
	// setting can decide the test.
	delCloneSettingsFile = "settings.json"
	// delCloneBuildScript is the declared build: it must run (the channel builds
	// before it pushes) AND it must succeed.
	delCloneBuildScript = "bz.txt"
	// delCloneBuildCommand is what the repos table declares. It reads a file the
	// fixture commits, so a build that ran leaves its mark on the tree it built.
	delCloneBuildCommand = "cat bz.txt >/dev/null"
	// delCloneBuildTimeoutSec bounds the fixture build.
	delCloneBuildTimeoutSec = 60
	// delCloneFilePerm is the mode for every file this fixture writes.
	delCloneFilePerm = 0o644
	// delCloneExemptSkipMarker is the close-out chain's integration-branch skip
	// wording — the sentence the pre-fix route answered with, and the one a
	// clone-ground row must never be handed again.
	delCloneExemptSkipMarker = "is exempt"
)

// delCloneSettings writes the private settings document and points this process
// at it, returning the workspace ROOT the document declares.
func delCloneSettings(t *testing.T, devRepo, buildRepo, root string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		config.DevRepoSettingsKey:       devRepo,
		config.WorkspaceRootSettingsKey: root,
		config.ReposSettingsKey: map[string]any{
			buildRepo: map[string]any{
				"build":             delCloneBuildCommand,
				"build_timeout_sec": delCloneBuildTimeoutSec,
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	path := filepath.Join(t.TempDir(), delCloneSettingsFile)
	if err := os.WriteFile(path, body, delCloneFilePerm); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsPathEnv, path)
}

// delCloneUpstream builds the fixture's source repository: a repo carrying the
// declared build's input file, so a build that ran cannot be a no-op, standing
// on the integration branch. The clone ground is cloned FROM IT, which is what
// makes the ground's `origin` the path the settings declare.
func delCloneUpstream(t *testing.T) string {
	t.Helper()
	up := covLifeGitRepo(t)
	covLifeWriteFile(t, filepath.Join(up, delCloneBuildScript), "the declared build's input\n")
	covLifeGit(t, up, covLifeGitBinary, "add", delCloneBuildScript)
	covLifeGit(t, up, covLifeGitBinary, "-c", "user.email=clone@example.invalid",
		"-c", "user.name=clone", "commit", "-q", "-m", "fixture: the declared build input")
	return up
}

// delCloneGround clones upstream into root/<task-id> — the exact directory the
// channel derives from the row id (devworkspace.Dir) — and puts ONE commit on it
// that no peer has ever seen: the row's undelivered work.
//
// The ground's ORIGIN is re-pointed at the peer through the fixture's ssh alias,
// which is how production is shaped: the channel pushes to `origin`, and that is
// the same host:path the retry asks about. Without it the push would reach
// upstream directly and the test would be measuring a different repository than
// the one it then reads.
func delCloneGround(t *testing.T, root, taskID, upstream string) (ground, rowTip string) {
	t.Helper()
	ground = filepath.Join(root, taskID)
	covLifeGit(t, root, covLifeGitBinary, "clone", "-q", upstream, ground)
	covLifeGit(t, ground, covLifeGitBinary, "remote", "set-url", deliveryProofOriginRemote,
		delRetryShimAlias+":~/work/GoPi")
	covLifeWriteFile(t, filepath.Join(ground, "deliverable.txt"), "the row's work\n")
	covLifeGit(t, ground, covLifeGitBinary, "add", "deliverable.txt")
	covLifeGit(t, ground, covLifeGitBinary, "-c", "user.email=clone@example.invalid",
		"-c", "user.name=clone", "commit", "-q", "-m", "the row's deliverable")
	return ground, covLifeGit(t, ground, covLifeGitBinary, "rev-parse", "HEAD")
}

// TestDeliveryRetryCloneGroundRowDeliversThroughTheChannel is ①+②+③ in one
// test, because the epic's claim is about ONE decision (which delivery a
// clone-ground row gets) and splitting it would let a router that answers right
// once and wrong the next time go unnoticed.
func TestDeliveryRetryCloneGroundRowDeliversThroughTheChannel(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)

	root := t.TempDir()
	upstream := delCloneUpstream(t)
	delCloneSettings(t, upstream, upstream, root)

	taskID := "t-retry-clone-channel"
	ground, rowTip := delCloneGround(t, root, taskID, upstream)

	// The peer holds its OWN master at a different tip — the state of every row
	// the defect mis-classified.
	shim.seedPeerMaster(t, upstream)
	peerTipBefore := shim.holds(t, masterBranchName)
	if peerTipBefore == "" {
		t.Fatal("fixture: the peer must advertise the integration branch for this arm to mean anything")
	}
	if peerTipBefore == rowTip {
		t.Fatalf("fixture: the row's tip must differ from the peer's, both are %s", rowTip)
	}

	covLifeSeedTaskRow(t, taskID,
		taskDeliverableRebuild(taskDeliverable{Repo: upstream, Branch: masterBranchName, Worktree: ground}))

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry on a clone-ground row: %v", err)
	}

	// ② THE REASON THIS TEST EXISTS: whatever else happens, the row must NOT be
	// handed the close-out chain's integration-branch exemption again.
	if strings.Contains(rep.Detail, delCloneExemptSkipMarker) {
		t.Fatalf("a clone-ground row was answered with the close-out chain's exemption (%q) — "+
			"the retry is still routing it through a chain that skips master by design "+
			"(status=%q rowTip=%q peerTip=%q)",
			rep.Detail, rep.Status, rep.RowTip, rep.PeerTip)
	}
	// Nor may the close-out chain have run at all: it is not this row's delivery.
	// (The retry no longer holds a chain report to inspect — the finish-cleanup
	// mechanism is retired and this path cannot reach one; 58b5618 removed the
	// DeliveryRetryReport.Chain field with its only writer, so this assertion is
	// now structural rather than a field read. The detail check above is what
	// would catch a regression in wording.)

	// ① THE ROUTE: the peer now advertises this row's OWN tip, asked of the BARE
	// REPO. This is the fact that only the channel's `push origin HEAD:<branch>`
	// can produce.
	if rep.Status != deliveryRetryStatusDelivered {
		t.Fatalf("status=%q, want %q (detail=%q, clone=%+v)",
			rep.Status, deliveryRetryStatusDelivered, rep.Detail, rep.Clone)
	}
	if got := shim.holds(t, masterBranchName); got != rowTip {
		t.Fatalf("the peer's %s is %q after the retry, want the row's own tip %q — "+
			"the delivery was reported but the push never landed", masterBranchName, got, rowTip)
	}
	if rep.PeerTip != rowTip {
		t.Errorf("the report's PeerTip = %q, want the row's own tip %q (the verdict must be the PEER's)", rep.PeerTip, rowTip)
	}
	// THE CHANNEL REALLY RAN ITS STEPS, not just a push: AttemptDelivery refuses
	// before touching git when no build is declared (Rule 12), so a delivery
	// here proves the declaration was resolved and the build ran.
	if rep.Clone == nil {
		t.Fatal("the report carries no clone-channel result for a clone-ground row")
	}
	if rep.Clone.Attempts < 1 {
		t.Errorf("the channel ran %d attempt(s) — a delivery costs at least one", rep.Clone.Attempts)
	}
	if !rep.Clone.Pushed {
		t.Errorf("the channel's own result does not report a push: %+v", *rep.Clone)
	}
	// The ground is KEPT: this path is a repair, not a reclaim (the sweep owns
	// that), and the row's commits must not disappear behind a report.
	if _, err := os.Stat(filepath.Join(ground, "deliverable.txt")); err != nil {
		t.Errorf("the ground's deliverable is gone after the retry: %v", err)
	}
}

// TestDeliveryRetryCloneGroundRefusalIsTheChannelsNotTheExemptions is the
// REFUSAL arm: a clone ground that CANNOT be delivered (no declaration for its
// build) must be reported blocked in the channel's own words, with the debt
// visible on the row's trail — and never as the close-out chain's exemption.
//
// It is a separate test from the delivered arm because the two shapes differ in
// the claim being made: ① asks "did the delivery happen", this one asks "when it
// cannot, does the operator learn WHY, and is it the right why".
func TestDeliveryRetryCloneGroundRefusalIsTheChannelsNotTheExemptions(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)

	root := t.TempDir()
	upstream := delCloneUpstream(t)
	// NO repos entry for this repo: the channel must refuse by name rather than
	// discover a build (Rule 12).
	delCloneSettings(t, upstream, filepath.Join(t.TempDir(), "an-unrelated-repo"), root)

	taskID := "t-retry-clone-refusal"
	ground, rowTip := delCloneGround(t, root, taskID, upstream)
	shim.seedPeerMaster(t, upstream)

	covLifeSeedTaskRow(t, taskID,
		taskDeliverableRebuild(taskDeliverable{Repo: upstream, Branch: masterBranchName, Worktree: ground}))

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry on a clone-ground row: %v", err)
	}
	if !rep.Owed() {
		t.Fatalf("a clone ground with no declared build must still OWE its delivery: status=%q detail=%q",
			rep.Status, rep.Detail)
	}
	if strings.Contains(rep.Detail, delCloneExemptSkipMarker) {
		t.Fatalf("the refusal is the close-out chain's exemption (%q), not the channel's refusal", rep.Detail)
	}
	if !strings.Contains(rep.Detail, cloneChannelFailurePrefix) {
		t.Errorf("the refusal must carry the channel's own prefix %q: %q", cloneChannelFailurePrefix, rep.Detail)
	}
	// EXACTLY ONCE (defect measured 2026-10-30): `blk.Report()` and the
	// channel's own errors already open with the prefix, and this face used to
	// paste it again — `delivery owed: clone channel: clone channel: …`. The
	// assertion above cannot see that (Contains is satisfied by a stutter), so
	// the count is the one that pins the fix.
	if n := strings.Count(rep.Blocked, cloneChannelFailurePrefix); n != 1 {
		t.Errorf("the channel prefix must occur exactly once in the owed reason, count=%d: %q", n, rep.Blocked)
	}
	// THE REASON AS A READER SEES IT — logged, so a green run still carries the
	// composed sentence (Rule 5: the evidence is the observation, not a claim).
	t.Logf("the row's owed reason: %q", rep.Blocked)
	if !strings.Contains(rep.Detail, "no build command is declared") {
		t.Errorf("the refusal must be the channel's declared-build sentence: %q", rep.Detail)
	}
	// And nothing was pushed: the refusal is a refusal, not a hopeful attempt.
	if got := shim.holds(t, masterBranchName); got == rowTip {
		t.Fatalf("the peer holds this row's tip %q after a REFUSED delivery — the gate did not withhold it", rowTip)
	}
	// The debt is on the row's trail where the resident pass reads it.
	if events := delRetryTrailEvents(t, taskID); !hasString(events, deliveryRetryBlockedEvent) {
		t.Errorf("the row's trail must carry %q when a clone-channel delivery is blocked, got %v",
			deliveryRetryBlockedEvent, events)
	}
}

// TestDeliveryRetryCloneGroundRouteIsTheChannel pins the PREDICATE the router
// uses, so the router cannot rot into "everything is a clone ground": a clone
// ground (`.git` is a DIRECTORY) routes to the channel; a linked worktree
// (`.git` is a FILE), a path that does not exist, and an empty path must not.
//
// The RED proof for the routing itself is a real mutation, measured in the
// task's report: the production router in `deliveryRetryOne` was changed back to
// the pre-fix shape (every row to the close-out chain), the two arms above went
// red on the exemption sentence, and the file was restored and re-verified by
// content identity before the suite was run green. A test cannot prove that by
// itself, so it is measured rather than narrated.
func TestDeliveryRetryCloneGroundRouteIsTheChannel(t *testing.T) {
	ground := t.TempDir()
	covLifeGit(t, ground, covLifeGitBinary, "init", "-q")
	linked := t.TempDir()
	covLifeWriteFile(t, filepath.Join(linked, "README.md"), "a linked worktree's .git is a FILE\n")
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: /nowhere\n"), delCloneFilePerm); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	if !deliveryProofIsCloneGround(ground) {
		t.Fatal("a clone ground (.git is a DIRECTORY) must be recognised — the router keys on this")
	}
	if deliveryProofIsCloneGround(linked) {
		t.Error("a linked worktree (.git is a FILE) must not be recognised as a clone ground")
	}
	if deliveryProofIsCloneGround("") {
		t.Error("an empty worktree path must not be recognised as a clone ground")
	}
	if deliveryProofIsCloneGround(filepath.Join(t.TempDir(), "does-not-exist")) {
		t.Error("a path that is not there must not be recognised as a clone ground")
	}
}

// TestDeliveryRetryCloneGroundDoesNotCallAnEmptyPushADelivery is the SECOND
// defect, found by RUNNING the surface rather than by reasoning about it
// (2026-10-26, row 01a16deb-73df-7a96-851d-97297f619246).
//
// The first version of this routing reported `delivered` on a row whose peer
// ref never moved:
//
//	$ ./bin/gopi task retry-delivery --task 01a16deb-…
//	01a16deb-… delivered branch=master … the peer now advertises it at b7a1ff8…
//	$ git ls-remote --heads peerbox:/home/user/work/gopi
//	b7a1ff8…	refs/heads/master          ← the SAME tip as before the run
//
// The mechanism: the channel's FIRST step is `pull --rebase origin <branch>`,
// which fast-forwarded the ground onto the peer (that row's patch had already
// been re-created upstream by another commit), so the push that followed was an
// EMPTY no-op that git still exits 0 on — and the post-attempt tip comparison
// read the ground AFTER the pull, i.e. a value against itself.
//
// The fix is the pair of facts this test pins: the peer's ref must MOVE, and
// what it holds must contain this row's work as it stood BEFORE the attempt.
func TestDeliveryRetryCloneGroundDoesNotCallAnEmptyPushADelivery(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)

	root := t.TempDir()
	upstream := delCloneUpstream(t)
	delCloneSettings(t, upstream, upstream, root)

	taskID := "t-retry-clone-noop"
	ground, _ := delCloneGround(t, root, taskID, upstream)

	// THE TRAP: the peer is seeded from the GROUND'S OWN TIP, so the ground is
	// already contained in what the peer holds. The channel's pull has nothing
	// to fast-forward to, its push is an empty no-op, and git exits 0.
	shim.seedPeerMaster(t, ground)
	groundTip := covLifeGit(t, ground, covLifeGitBinary, "rev-parse", "HEAD")
	if got := shim.holds(t, masterBranchName); got != groundTip {
		t.Fatalf("fixture: the peer must already hold the ground's tip %q, it holds %q", groundTip, got)
	}

	covLifeSeedTaskRow(t, taskID,
		taskDeliverableRebuild(taskDeliverable{Repo: upstream, Branch: masterBranchName, Worktree: ground}))

	rep, err := DeliveryRetryTaskID(taskID)
	if err != nil {
		t.Fatalf("delivery retry on a clone-ground row: %v", err)
	}
	// THE INVARIANT: nothing moved, so nothing may be reported as delivered.
	if rep.Status == deliveryRetryStatusDelivered {
		t.Fatalf("a run whose peer ref never moved was reported %q (detail=%q, clone=%+v) — "+
			"an empty push is not a delivery", rep.Status, rep.Detail, rep.Clone)
	}
	if got := shim.holds(t, masterBranchName); got != groundTip {
		t.Fatalf("the peer's tip moved to %q; the fixture requires it not to", got)
	}
	// The work IS on the peer, so the honest disposition is the adoption one —
	// never a fresh `delivered`, and never a plain block either (the row's work
	// is not owed a second push).
	if rep.Status != deliveryRetryStatusAlreadyMerged && rep.Status != deliveryRetryStatusAlreadyDelivered {
		t.Errorf("status=%q, want %q or %q (detail=%q)",
			rep.Status, deliveryRetryStatusAlreadyMerged, deliveryRetryStatusAlreadyDelivered, rep.Detail)
	}
}
