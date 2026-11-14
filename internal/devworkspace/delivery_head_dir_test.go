package devworkspace

// delivery_head_dir_test.go — WHERE DOES THIS DIRECTORY'S HEAD LIVE?
//
// THE DEFECT THIS FILE PINS (measured 2026-10-29 on nick-HP): every git step of
// the delivery leg runs `git -C <dir> …`, and GIT WALKS UP. A `dir` that is not
// a work tree of its own makes every count about the ENCLOSING repository, while
// the refusal names `dir` — so a leg that never looked at its own ground reported
// facts about another directory, and the run was sent to chase a `pull --rebase`
// that had nothing to do with it. The measurement, taken in this very checkout:
//
//	$ mkdir -p .probe-anchor/nested            # no .git of its own
//	$ git -C .probe-anchor/nested rev-parse --show-toplevel
//	/home/user/workspace/01a17dfb-…            # the ENCLOSING repo
//	$ git -C .probe-anchor/nested rev-parse HEAD
//	64301adf…                                  # the ENCLOSING repo's HEAD
//	$ git -C .probe-anchor/nested status --porcelain; echo $?
//	0                                          # and it answers "clean"
//
// So `isGitWorkTree` (which stats `<dir>/.git`) and every count built on it were
// asking a question about the wrong directory. The gate this file tests asks
// git's own question instead and REFUSES, naming both paths.
//
// THE THREE ARMS (the epic's acceptance):
//
//	① A is where the leg is told to publish, HEAD lives in B → refuse, naming
//	  both, and DO NOT report a pull failure.
//	② a workspace with nothing ahead of its base → its OWN message ("carries no
//	  commit to deliver"), never the pull's vocabulary — and the head gate must
//	  not steal that case (a real checkout passes the gate).
//	③ a healthy run (commits in its own deliverable.worktree) delivers as
//	  before — the gate that refuses everything satisfies ① and ② while breaking
//	  the channel.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// headElsewhereFixture builds the measured shape by hand: a clone of the origin
// standing at <root>/<task-id>, and a plain SUBDIRECTORY of it (no .git) that a
// caller could mistake for a ground of its own. `git -C <subdir>` answers with
// the clone's HEAD, which is exactly the walk-up this gate exists to catch.
func headElsewhereFixture(t *testing.T) (origin string, cfg Config, subdir string) {
	t.Helper()
	origin = newOrigin(t)
	cfg = cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	// ONE COMMIT IN THE CLONE, so "the enclosing repo has work" is a measured
	// fact rather than an empty repository answering the same way by accident.
	commitIn(t, dir, "work.txt", "the run's real work, in the real ground")
	subdir = filepath.Join(dir, "not-a-ground")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return origin, cfg, subdir
}

// TestDeliveryRefusesWhenTheHeadLivesInAnotherDirectory is arm ①. The leg is told
// to publish from a directory that is not a work tree of its own; git measures its
// HEAD in the enclosing clone. The attempt must STOP, name both paths, and say
// nothing about pulling.
func TestDeliveryRefusesWhenTheHeadLivesInAnotherDirectory(t *testing.T) {
	ctx := context.Background()
	origin, cfg, subdir := headElsewhereFixture(t)
	dir := Dir(cfg.Root, cfg.TaskID)
	// THE MEASUREMENT THE GATE RESTS ON, taken here so a failure shows whether
	// the fixture or the gate is wrong.
	measured, err := deliveryHeadDir(ctx, subdir)
	if err != nil {
		t.Fatalf("fixture: git cannot be asked about %s: %v", subdir, err)
	}
	if measured != deliveryDirIdentity(dir) {
		t.Fatalf("fixture: the HEAD of %s was measured in %q, want the enclosing clone %q — "+
			"git did not walk up here, and this test then proves nothing", subdir, measured, dir)
	}

	// THE PRECONDITIONS THAT MAKE "NOTHING RAN" PROVABLE.
	originBefore := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch))
	trackingBefore := strings.TrimSpace(gitIn(t, dir, "rev-parse", "origin/"+DefaultBranch))

	// NO RECORDED GROUND ON PURPOSE: this gate is about the PUBLISH DIRECTORY,
	// so it has an opinion even when no row records anything. (The anchor gate
	// abstains in that case — a different question, a different gate.)
	res, blk, err := AttemptDelivery(ctx, cfg, subdir)
	if blk != nil {
		t.Fatalf("a wrong publish directory is an ERROR, never a blocker about this work: %q: %s",
			blk.Kind, blk.Report())
	}
	if err == nil {
		t.Fatalf("the delivery was ALLOWED from %s although its HEAD lives in %s (pushed=%v)", subdir, dir, res.Pushed)
	}
	// BOTH PATHS, VERBATIM — an operator copies the second one into a command.
	for _, want := range []string{subdir, dir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got: %v", want, err)
		}
	}
	if !strings.Contains(err.Error(), "refusing to publish") {
		t.Errorf("the refusal must SAY what it refuses to do, got: %v", err)
	}
	// AND THE TEMPTING WRONG ANSWER IS ABSENT: this is not a pull, not a
	// conflict, not a build.
	for _, ghost := range []string{"pull step refused", "conflicted path", "attempt 1/"} {
		if strings.Contains(err.Error(), ghost) {
			t.Fatalf("the head-directory refusal must not be worded as %q — that is the misattribution "+
				"this gate removes: %v", ghost, err)
		}
	}
	t.Logf("the refusal: %v", err)

	// NOTHING RAN: no pull (which would move the tracking ref), no build, no push.
	if after := strings.TrimSpace(gitIn(t, dir, "rev-parse", "origin/"+DefaultBranch)); after != trackingBefore {
		t.Fatalf("origin/%s in the clone moved %s → %s: a fetch/pull RAN although the head gate refused",
			DefaultBranch, trackingBefore, after)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build-stamp.txt")); statErr == nil {
		t.Fatal("the declared build RAN although the head gate refused")
	}
	if after := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch)); after != originBefore {
		t.Fatalf("origin moved %s → %s although the head gate refused", originBefore, after)
	}
}

// TestDeliveryEmptyStaysDistinctFromARefusedPull is arm ②, re-asserted through
// the NEW gate: a real checkout with nothing ahead of its base must still answer
// with its own message, which is the fact the head gate must not steal (it passes
// a real checkout) and must not be confused with (a refused pull never appears).
//
// The two sentences are asserted to be DISJOINT here, which is the property the
// epic asks for — "no commits to deliver" and "the pull step refused" must not be
// able to describe one situation twice.
func TestDeliveryEmptyStaysDistinctFromARefusedPull(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)

	// MOTION IN THE BASE, so a pull WOULD have something to do: without it "the
	// pull never ran" could be explained by the pull having nothing to fetch.
	commitIn(t, origin, "theirs.txt", "the peer moves ahead")

	var log strings.Builder
	cfg.Log = &log
	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("an empty workspace is a BLOCKER (a fact the next round acts on), not an error: %v", err)
	}
	if blk == nil {
		t.Fatalf("the delivery was allowed with nothing to deliver (pushed=%v detail=%q)", res.Pushed, res.Detail)
	}
	// THE KIND, and the base MOVED in this fixture — so the shape is measured
	// against the peer and comes back INHERITED (row 01a19483: the shape used to
	// be read from the stale remote-tracking ref, which is why this arm saw
	// `empty`). The subject of THIS arm is unchanged and is asserted below: a
	// workspace with nothing to deliver must never be described through the pull
	// step's vocabulary. Both no-delivery kinds qualify; a pull/conflict kind
	// would not.
	if blk.Kind != BlockerKindEmpty && blk.Kind != BlockerKindInheritedDelivery {
		t.Fatalf("blocker kind = %q, want %q or %q — a workspace that is merely EMPTY must never be "+
			"reported through another step's vocabulary:\n%s", blk.Kind, BlockerKindEmpty,
			BlockerKindInheritedDelivery, blk.Report())
	}
	// THE HEAD GATE RAN AND AGREED — said out loud, so "asked and answered yes"
	// is distinguishable from "never asked" (Rule 15).
	if !strings.Contains(log.String(), "is where its HEAD lives") {
		t.Fatalf("the head-directory gate did not report its measurement on a healthy ground:\n%s", log.String())
	}
	report := blk.Report()
	if !strings.Contains(report, "carries no commit to deliver") {
		t.Fatalf("the empty refusal must say there is nothing to deliver:\n%s", report)
	}
	for _, pullWords := range []string{"pull step refused", "conflicted path", "CONFLICT"} {
		if strings.Contains(report, pullWords) {
			t.Fatalf("an empty workspace must not be reported with %q — that is the collapse this split "+
				"removes:\n%s", pullWords, report)
		}
	}
}

// TestDeliveryHeadGateReportsWhenItAgrees is arm ③ seen from the gate's own side:
// a real checkout passes, and the pass is NARRATED (a silent green cannot be told
// apart from a gate that never ran).
func TestDeliveryHeadGateReportsWhenItAgrees(t *testing.T) {
	ctx := context.Background()
	_, cfg, _ := headElsewhereFixture(t)
	dir := Dir(cfg.Root, cfg.TaskID)
	var log strings.Builder
	cfg.Log = &log
	if err := deliveryHeadDirGate(ctx, cfg, dir); err != nil {
		t.Fatalf("a real checkout must pass the head-directory gate: %v", err)
	}
	if !strings.Contains(log.String(), deliveryHeadMatchesFmt[:20]) {
		t.Fatalf("the gate must state the measurement it took:\n%s", log.String())
	}
	// AND THE GATE IS NOT DECEIVED BY A SPELLING: the same directory reached
	// through a symlink is the same directory, so a row recorded through a link
	// is not refused for a typographical reason.
	link := filepath.Join(cfg.Root, "spelled-differently")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := deliveryHeadDirGate(ctx, Config{}, link); err != nil {
		t.Fatalf("a symlinked spelling of the publish directory must pass (identity, not string equality): %v", err)
	}
	// AN UNMEASURABLE DIRECTORY IS AN ERROR, never a pass (Rule 15).
	if err := deliveryHeadDirGate(ctx, Config{}, filepath.Join(cfg.Root, "no-such-directory")); err == nil {
		t.Fatal("a directory git cannot measure must be refused, not published from")
	}
}
