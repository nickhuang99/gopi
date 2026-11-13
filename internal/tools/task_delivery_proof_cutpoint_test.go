package tools

// Regression for THE PRODUCED-NOTHING GATE in TaskDeliveryProof
// (task_delivery_proof.go, `tipIsTheCutPoint`).
//
// THE DEFECT THIS FILE PINS. Every containment arm of the delivery proof asks
// "does the destination carry what the run produced?". A ground whose HEAD is
// the very commit its cut-time snapshot names produced NOTHING, and the
// ancestry arm (`merge-base --is-ancestor`) then answers "contained" for a
// trivial reason: the tip is an ancestor of ITSELF. The gate must run BEFORE
// any arm, or a run that produced nothing is reported `delivered`.
//
// WHY THIS IS NOT A HYPOTHETICAL. Rule 37 gives a `gopi chat` session a ground
// of its own and nothing commits into it, so a chat's ground sits exactly on its
// cut point for the whole session. Without the gate, every chat row's delivery
// proof comes back `delivered` — and `delivered` is what releases the ground.
//
// THE RED PROOF IS THE ARM ITSELF, not a stub. The second check below calls
// `deliveryProofTipContained` directly on the SAME fixture and asserts it
// answers CONTAINED — the measurement that the arm would have delivered the row
// if the gate were not standing in front of it. A gate asserted only by "the
// row is unknown" would pass for a fixture where no arm ever fires, and would
// prove nothing about the false positive this file exists for.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

const (
	dpgEmail = "dpg@client"
	dpgUser  = "dpg"
	// dpgBaseFile is the file the fixture's base commit carries — the cut
	// point's own content, which is what a stray arm "finds" at the destination.
	dpgBaseFile = "base.txt"
	dpgRowID    = "dpg-empty-run"
	// dpgRunFile is what the RUN commits — its own work, distinct from the base.
	dpgRunFile = "run.txt"
	// dpgAbsentBranch names no branch of the fixture: the no-reflog case.
	dpgAbsentBranch = "task/never-cut"
)

// dpgEmptyRunGround builds the three directories of the clone channel with a
// ground that commits NOTHING:
//
//	server  the delivery point (bare)
//	seed    the row's recorded repo — carries the LIVE integration branch
//	ground  a CLONE (`.git` a DIRECTORY) sitting exactly on its cut point
func dpgEmptyRunGround(t *testing.T) (ground, seed string) {
	t.Helper()
	root := t.TempDir()
	server := filepath.Join(root, "server")
	rebuildTestGit(t, root, "init", "-q", "--bare", "-b", masterBranchName, server)

	seed = filepath.Join(root, "seed")
	rebuildTestGit(t, root, "clone", "-q", server, seed)
	rebuildTestGit(t, seed, "config", "user.email", dpgEmail)
	rebuildTestGit(t, seed, "config", "user.name", dpgUser)
	if err := os.WriteFile(filepath.Join(seed, dpgBaseFile), []byte("the cut point's own content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", dpgBaseFile)
	rebuildTestGit(t, seed, "commit", "-q", "-m", "base")
	rebuildTestGit(t, seed, "push", "-q", deliveryProofOriginRemote, masterBranchName)

	ground = filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", server, ground)
	rebuildTestGit(t, ground, "config", "user.email", dpgEmail)
	rebuildTestGit(t, ground, "config", "user.name", dpgUser)
	// NO COMMIT of its own: HEAD is the cut point, exactly as a chat session's
	// ground is for as long as the session lasts.
	if info, err := os.Stat(filepath.Join(ground, dpGroundGit)); err != nil || !info.IsDir() {
		t.Fatalf("the fixture ground must be a CLONE (.git a directory): %v", err)
	}
	return ground, seed
}

// TestDeliveryProof_AGroundOnItsCutPointProducedNothing is the positive half:
// the row is `unknown`, never `delivered` and never `undelivered`.
func TestDeliveryProof_AGroundOnItsCutPointProducedNothing(t *testing.T) {
	ground, seed := dpgEmptyRunGround(t)

	db := testutil.NewTestDBPath(t)
	dpSeedRow(t, db, dpgRowID, "chore: no commit was made", taskStatusInProgress)
	dpWriteDelivery(t, db, dpgRowID, seed, masterBranchName, ground)

	out, err := TaskDeliveryProof(dpgRowID)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	// NEITHER VERDICT FITS, and the honest answer is the third one. `delivered`
	// would be a lie (the run's tip is the point it was cut from — nothing was
	// produced) and `undelivered` would refuse a delivery that never had
	// anything to deliver, which costs the row its close-out. `unknown` says
	// exactly that and still records the fact on the trail (Rule 15).
	if out.State != deliveryProofUnknown {
		t.Fatalf("state = %q, want %q — a ground with no commit of its own PRODUCED NOTHING: an empty "+
			"subject set is not \"everything landed\" (delivered is a false positive) and there is no "+
			"deliverable to refuse (undelivered is a false refusal) (note: %s)",
			out.State, deliveryProofUnknown, out.Note)
	}
	if !strings.Contains(out.Note, "produced NO commit of its own") {
		t.Fatalf("note = %q, want it to say the run produced nothing, in the gate's own words", out.Note)
	}
}

// TestDeliveryProof_TheGateGuardsAnArmThatWouldHaveSaidDelivered is the RED
// PROOF, and it is a measurement rather than a stub: the ancestry arm, asked
// the same question about the same fixture, answers CONTAINED. That is the
// false positive the gate is standing in front of — asserted, not asserted-about.
func TestDeliveryProof_TheGateGuardsAnArmThatWouldHaveSaidDelivered(t *testing.T) {
	ground, seed := dpgEmptyRunGround(t)
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()

	tip := deliveryProofGitRead(ctx, ground, "rev-parse", "HEAD")
	if tip == "" {
		t.Fatal("the fixture's tip could not be read")
	}
	// `config` — the arm's own answer for a destination ref, in the directory
	// that carries both objects.
	contained, why := deliveryProofTipContained(ctx, seed,
		refsRemotesPrefix+deliveryProofOriginRemote+"/"+masterBranchName, tip)
	if !contained {
		t.Fatalf("the ancestry arm answered NOT-contained (%q) for a tip that IS the destination's own "+
			"commit — the fixture no longer reproduces the shape this gate is for, so the test above "+
			"would be green for the wrong reason", why)
	}
	// And the gate is what separates the two: asked the cut-point question, the
	// same tip is named as the point it was cut from.
	if !tipIsTheCutPoint(ctx, ground, masterBranchName, tip) {
		t.Fatalf("tipIsTheCutPoint(%s) = false for a ground sitting on its own cut point — the gate "+
			"cannot fire, and the arm above would deliver the row", tip)
	}
}

// TestTipIsTheCutPoint_SurvivesTheRunsOwnDeliveryPush is the REGRESSION for the
// false negative measured on row 01a1810a (2026-10-28): a run that DID deliver
// was reported `produced NO commit of its own`, so the proof hid a landing
// instead of proving it.
//
// THE MECHANISM, and it is the clone channel's own delivery leg: the channel
// publishes with `git push origin HEAD:<integration branch>`
// (devworkspace.publishGround's ONE refspec), and a successful push ADVANCES the
// clone's `refs/remotes/origin/master` to the pushed tip — the very ref the gate
// used to read as "the cut point". Measured in a scratch clone:
//
//	push#1      HEAD=a091808  origin/master=a091808
//	before #2   HEAD=dc3809a  origin/master=a091808
//	AFTER  #2   HEAD=dc3809a  origin/master=dc3809a   ← the push moved the witness
//
// So the assertion below is TWO-SIDED on the same fixture: with the run's commit
// pushed, the OLD instrument (that ref) would answer "this is the cut point",
// and the one in use must answer NO. A test that only asserted the new answer
// would not show what the fix is fixing.
func TestTipIsTheCutPoint_SurvivesTheRunsOwnDeliveryPush(t *testing.T) {
	ground, _ := dpgEmptyRunGround(t)
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()

	// The run produces a commit OF ITS OWN and delivers it through the channel's
	// one refspec — the exact sequence that moved the old witness.
	if err := os.WriteFile(filepath.Join(ground, dpgRunFile), []byte("the run's own work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", dpgRunFile)
	rebuildTestGit(t, ground, "commit", "-q", "-m", "the run's own commit")
	rebuildTestGit(t, ground, "push", "-q", deliveryProofOriginRemote, "HEAD:"+masterBranchName)

	tip := deliveryProofGitRead(ctx, ground, "rev-parse", "HEAD")
	if tip == "" {
		t.Fatal("the fixture's tip could not be read")
	}
	// THE OLD INSTRUMENT, still read here on purpose: after the run's own push
	// this ref IS the tip, which is exactly the false negative the gate's old
	// spelling produced.
	if moved := deliveryProofGitRead(ctx, ground,
		"rev-parse", refsRemotesPrefix+deliveryProofOriginRemote+"/"+masterBranchName); moved != tip {
		t.Fatalf("the fixture no longer reproduces the shape: after the run's delivery push, %s/master is %q "+
			"but the tip is %q — the push did not advance the clone's tracking ref, so this test would be "+
			"green for the wrong reason", deliveryProofOriginRemote, moved, tip)
	}
	if tipIsTheCutPoint(ctx, ground, masterBranchName, tip) {
		t.Fatalf("tipIsTheCutPoint says the tip IS the cut point for a run that committed AND pushed its own " +
			"work — this is the false negative of row 01a1810a: a delivered run reported as " +
			"\"produced NO commit of its own\"")
	}
}

// TestTipIsTheCutPoint_AnUnreadableCutPointIsNotAVerdict is Rule 15's arm: a
// ground whose branch reflog cannot be read must answer "the question was not
// answered", never "the run produced nothing" — the second is a verdict the
// reader acts on, and it would refuse a delivery that was never measured.
func TestTipIsTheCutPoint_AnUnreadableCutPointIsNotAVerdict(t *testing.T) {
	ground, _ := dpgEmptyRunGround(t)
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	tip := deliveryProofGitRead(ctx, ground, "rev-parse", "HEAD")
	if tip == "" {
		t.Fatal("the fixture's tip could not be read")
	}
	// A branch that does not exist here has no reflog — `GroundCutPoint` returns
	// unmeasured rather than a zero sha, and the gate must fall through.
	if tipIsTheCutPoint(ctx, ground, dpgAbsentBranch, tip) {
		t.Fatalf("a branch with no reflog answered \"the run produced nothing\" — an unread cut point is "+
			"not evidence (branch %q)", dpgAbsentBranch)
	}
	// An empty branch (a row that records none) is the same non-answer.
	if tipIsTheCutPoint(ctx, ground, "", tip) {
		t.Fatal("an empty branch name answered a verdict — no subject is never a pass")
	}
}
