package tools

// TWO-ARM REGRESSION for the delivery-proof arm on a row whose ground is STILL
// STANDING (measured 2026-10-31, rows 01a18799-3304-7c30-b50f-8feb76908c8b and
// 01a18ea5-8925-7c3e-b99e-130eae4c0f91).
//
// THE DEFECT. The integration branch was asked as a destination ONLY when
// `devworkspace.GroundModelOf(ground) == GroundModelMissing`. A DISPATCHED row
// records `task/<id>` as its declared branch while the clone channel publishes
// `HEAD:refs/heads/master`, so `refs/remotes/origin/task/<id>` resolves nowhere.
// The ground is alive at the end hook, so the integration destination was never
// added, the destination list came back EMPTY, and the arm answered
// `delivery-proof-unverified` for both rows — which `deliveryProofRefusal` LETS
// THROUGH. The row's own trail, verbatim (task_events, 01a18ea5-…):
//
//	delivery-proof-unverified | delivery proof for 01a18ea5-… (branch
//	  task/01a18ea589257c3eb99e130eae4c0f91) NOT judged: no
//	  refs/remotes/origin/task/01a18ea589257c3eb99e130eae4c0f91 resolves in the
//	  ground or the recorded repo, so there is no declared destination to
//	  measure against
//	completed                 | — elapsed=3209.4s iter=172 tokens=19214676
//
// Both rows had died inside the LLM call (`Error: LLM call failed … read:
// connection timed out`) and their commits sat only in their grounds.
//
// THE FIX. The integration branch is asked whether or not the ground survives.
//
// THE TWO ARMS, and neither is optional (the same pair the ground-gone repair
// carries, because the same guard protects the same claim):
//
//	(live ground + the commit IS on the integration branch) → DELIVERED
//	(live ground + the commit is NOT there)                 → REFUSED
//
// The second arm is what keeps this repair from being "delete the check": it is
// row 01a163df's shape, a success terminal on work that exists nowhere the
// merger can see.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// dplgLiveGround builds the DISPATCHED-row shape: a CLONE ground that is STILL
// STANDING, a row whose declared branch is its ADDRESS (`task/<id>` — the name
// no `origin` ref ever carries), and a channel that may or may not have pushed
// the run's commit to the integration branch.
//
// It returns the repo the row records, the LIVE ground directory, and the tip.
func dplgLiveGround(t *testing.T, landIt bool) (repo, liveGround, tip string) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "server")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, bare, "init", "-q", "--bare", "-b", masterBranchName)

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "init", "-q", "-b", masterBranchName)
	rebuildTestGit(t, seed, "config", "user.email", "dplg@test")
	rebuildTestGit(t, seed, "config", "user.name", "dplg")
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", "f.txt")
	rebuildTestGit(t, seed, "commit", "-q", "-m", "base")
	rebuildTestGit(t, seed, "push", "-q", bare, masterBranchName)

	liveGround = filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", bare, liveGround)
	rebuildTestGit(t, liveGround, "config", "user.email", "dplg@test")
	rebuildTestGit(t, liveGround, "config", "user.name", "dplg")
	if err := os.WriteFile(filepath.Join(liveGround, "work.txt"), []byte("the run's own work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, liveGround, "add", "work.txt")
	rebuildTestGit(t, liveGround, "commit", "-q", "-m", "the work this run produced")
	tip = rebuildTestGit(t, liveGround, "rev-parse", "HEAD")
	// The precondition of this whole file: the ground must SURVIVE, and it must
	// be a CLONE (`.git` a directory) — the shape the retired arm's condition
	// excluded.
	if info, err := os.Stat(filepath.Join(liveGround, dpGroundGit)); err != nil || !info.IsDir() {
		t.Fatalf("fixture must be a LIVE CLONE ground (.git a directory): %v", err)
	}
	if landIt {
		// THE CHANNEL'S OWN REFSPEC (devworkspace.publishGround): the integration
		// branch, never the row's address.
		rebuildTestGit(t, liveGround, "push", "-q", deliveryProofOriginRemote, "HEAD:refs/heads/"+masterBranchName)
	}
	// The recorded repo is a SECOND clone of the same delivery point — the
	// directory the row names as `repo`, and the one the arm is asked over.
	repo = filepath.Join(root, "recorded")
	rebuildTestGit(t, root, "clone", "-q", bare, repo)
	return repo, liveGround, tip
}

// TestDeliveryProof_LiveGroundJudgesTheIntegrationBranch is ARM ONE: a run
// whose work DID reach the integration branch is judged delivered even though
// its declared branch is its address and its ground still stands.
//
// RED WHEN THE FIX IS REVERTED: `dests` is empty (origin/task/<id> resolves
// nowhere, and the integration destination is gated on the ground being gone),
// the verdict is `deliveryProofUnknown`, and the row's trail carries
// `delivery-proof-unverified` — the measured sentence of the two rows.
func TestDeliveryProof_LiveGroundJudgesTheIntegrationBranch(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, liveGround, tip := dplgLiveGround(t, true)

	id := "dplg-delivered"
	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "feat: the work reached the integration branch, the ground still stands", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, branch, liveGround)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.Tip != tip {
		t.Fatalf("the tip must be the ground's HEAD: got %q, want %q (note=%s)", out.Tip, tip, out.Note)
	}
	if out.State != deliveryProofDelivered {
		t.Fatalf("a commit the integration branch HOLDS must be judged delivered while the ground still stands; "+
			"got state=%q note=%s\n(this is rows 01a18799 / 01a18ea5's exact false verdict)", out.State, out.Note)
	}
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err != nil {
		t.Fatalf("the write must not be refused for a delivered row: %v", err)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusCompleted {
		t.Errorf("row status = %q, want %q", got, taskStatusCompleted)
	}
	if n := dpEventCount(t, db, id, DeliveryProofUnverifiedEvent); n != 0 {
		t.Errorf("a judged-delivered row must carry no %s record, got %d", DeliveryProofUnverifiedEvent, n)
	}
}

// TestDeliveryProof_LiveGroundRefusesUnlandedTip is ARM TWO, and it is the one
// that keeps the repair from being "add a destination that always says yes":
// same live ground, same declared address, but the delivery point does NOT hold
// the commit.
//
// The row must stay OPEN. With the integration destination silently treated as
// "anything counts", or with the empty-destination case folded into a pass, a
// run whose push never landed closes `completed` — row 01a163df's shape.
func TestDeliveryProof_LiveGroundRefusesUnlandedTip(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, liveGround, tip := dplgLiveGround(t, false)

	id := "dplg-undelivered"
	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "feat: the push never landed, the ground still stands", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, branch, liveGround)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.State == deliveryProofDelivered {
		t.Fatalf("a tip the integration branch does NOT hold must not be judged delivered (note=%s)", out.Note)
	}
	werr := func() error {
		_, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done")
		return err
	}()
	if werr == nil && out.State == deliveryProofUndelivered {
		t.Fatalf("row %s reached %s while its tip %s is in NEITHER origin/%s NOR the patches of it — "+
			"the live-ground repair made the judgement unconditionally green",
			id, taskStatusCompleted, branchDeleteShortHash(tip), masterBranchName)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusInProgress {
		t.Errorf("row status after the refused write = %q, want %q (the write must not land)",
			got, taskStatusInProgress)
	}
}
