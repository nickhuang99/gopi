package tools

// task_delivery_proof_live_witness_test.go — THE DELIVERY POINT ITSELF is the
// only witness a CLONE ground's close-out can trust, and the two refs the arm
// reads today are LOCAL CACHES of it.
//
// THE FALSE REFUSAL THIS PINS (measured 2026-10-26, from the two-direction
// observation recorded in devdoc/delivery-peer-repo-path-and-identity.md):
// `deliveryProofDestinations` measures `refs/remotes/origin/<branch>` in the
// ground and in the row's recorded repo. BOTH are remote-tracking refs — a
// cache that moves only when THAT particular directory happens to fetch or
// push. So a deliverable that IS at the delivery point is reported
// "undelivered" whenever neither cache has been refreshed since:
//
//	$ git -C <ground> ls-remote <delivery point> refs/heads/master
//	<tip>	refs/heads/master            ← the work IS there
//	$ git -C <ground> rev-parse refs/remotes/origin/master
//	<an older commit>                    ← the cache says otherwise
//
// Row 01a16f7c is the measured instance of the shape (its `completed` was
// written 78 minutes late, after the work had been merged into master by a
// DIFFERENT path — `f8e31a2d4` — which this ground's cache never saw; the
// `delivery-proof-owed` events at 04:36:54 / 04:37:46 quote a stale tip
// against a stale cache).
//
// A refusal here is not harmless: at close-out it leaves a FINISHED row OPEN
// and skips its reclaim, and the ledger then reads like a run still in flight.
// The arm therefore gains ONE live witness — the delivery point's own branch,
// asked with `git ls-remote` through the ground's `origin` URL — and keeps the
// two caches as fallbacks so an offline close-out behaves exactly as before.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// dpStaleCaches builds the fixture above: a BARE delivery point that HOLDS the
// deliverable, a CLONE ground whose remote-tracking ref was deliberately left
// at the pre-delivery tip (the shape a delivery that did not go through THIS
// ground's push leaves behind), and a recorded repo whose cache is stale too.
func dpStaleCaches(t *testing.T) (ground, repo, deliveryPoint, tip string) {
	t.Helper()
	root := t.TempDir()
	deliveryPoint = filepath.Join(root, "server")
	rebuildTestGit(t, root, "init", "-q", "--bare", "-b", masterBranchName, deliveryPoint)

	seed := filepath.Join(root, "seed")
	rebuildTestGit(t, root, "clone", "-q", deliveryPoint, seed)
	rebuildTestGit(t, seed, "config", "user.email", "dp@test")
	rebuildTestGit(t, seed, "config", "user.name", "dp")
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", "f.txt")
	rebuildTestGit(t, seed, "commit", "-q", "-m", "base")
	rebuildTestGit(t, seed, "push", "-q", "origin", masterBranchName)
	cut := rebuildTestGit(t, seed, "rev-parse", "HEAD")

	ground = filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", deliveryPoint, ground)
	rebuildTestGit(t, ground, "config", "user.email", "dp@test")
	rebuildTestGit(t, ground, "config", "user.name", "dp")
	if err := os.WriteFile(filepath.Join(ground, "work.txt"), []byte("delivered elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", "work.txt")
	rebuildTestGit(t, ground, "commit", "-q", "-m", "work delivered by another path")
	tip = rebuildTestGit(t, ground, "rev-parse", "HEAD")

	// THE DELIVERY: it reaches the point, but through a route this ground's
	// cache does not follow — a second clone pushes it (the merger's adoption
	// shape / another node's push). Nothing here is invented: `git push` from a
	// DIFFERENT directory is exactly how a merge lands while a run's ground
	// keeps the cut point.
	other := filepath.Join(root, "other")
	rebuildTestGit(t, root, "clone", "-q", deliveryPoint, other)
	rebuildTestGit(t, other, "config", "user.email", "dp@test")
	rebuildTestGit(t, other, "config", "user.name", "dp")
	rebuildTestGit(t, other, "fetch", "-q", ground, "refs/heads/"+masterBranchName)
	rebuildTestGit(t, other, "push", "-q", "origin", "FETCH_HEAD:refs/heads/"+masterBranchName)

	// AND THE TWO CACHES STAY WHERE THEY WERE.
	rebuildTestGit(t, ground, "update-ref", "refs/remotes/origin/"+masterBranchName, cut)
	if got := rebuildTestGit(t, ground, "rev-parse", "refs/remotes/origin/"+masterBranchName); got == tip {
		t.Fatalf("fixture is wrong: the ground's cache followed the delivery (%s)", got)
	}

	repo = seed // the row's recorded repo — its cache is the same cut point
	if got := rebuildTestGit(t, repo, "rev-parse", "refs/remotes/origin/"+masterBranchName); got == tip {
		t.Fatalf("fixture is wrong: the recorded repo's cache followed the delivery (%s)", got)
	}
	return ground, repo, deliveryPoint, tip
}

// TestDeliveryProof_CloneGroundTrustsTheDeliveryPointNotItsCache is the
// regression. RED WITHOUT THE LIVE WITNESS: both cache destinations miss, the
// arm answers `undelivered`, and the close-out refuses a deliverable that is
// sitting at its declared destination.
func TestDeliveryProof_CloneGroundTrustsTheDeliveryPointNotItsCache(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, repo, deliveryPoint, tip := dpStaleCaches(t)

	// THE CONTROL: the work really is at the delivery point, measured by git
	// itself and not by this arm.
	live := rebuildTestGit(t, ground, "ls-remote", deliveryPoint, refsHeadsPrefix+masterBranchName)
	if want := tip + "\t" + refsHeadsPrefix + masterBranchName; !linePrefixed(live, want) {
		t.Fatalf("fixture is wrong: the delivery point does not hold the tip\nls-remote:\n%s\nwant: %s", live, want)
	}

	id := "dp-live-witness"
	dpSeedRow(t, db, id, "fix: delivered by another path", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.State != deliveryProofDelivered {
		t.Fatalf("the arm answered %q while git says %s IS at the delivery point %s — "+
			"a clone ground's remote-tracking ref is a LOCAL CACHE and must not be the only witness "+
			"(note: %s)", out.State, branchDeleteShortHash(tip), deliveryPoint, out.Note)
	}
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err != nil {
		t.Fatalf("a deliverable that IS at its delivery point must not be refused at close-out (the row "+
			"would stay OPEN and be skipped by the reclaim), got: %v", err)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusCompleted {
		t.Errorf("row status = %q, want %q", got, taskStatusCompleted)
	}
}

// TestDeliveryProof_LiveWitnessDoesNotRescueAnUndeliveredGround is the mirror
// arm and the reason the witness is ADDITIVE: an arm that only ever answers
// "delivered" proves nothing. Here the ground holds a commit the delivery point
// has never seen — the incident's own shape — and the answer must still be
// `undelivered`.
func TestDeliveryProof_LiveWitnessDoesNotRescueAnUndeliveredGround(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, masterTip, groundTip := dpCloneChannel(t)
	repo := filepath.Join(filepath.Dir(ground), "seed")

	id := "dp-live-witness-negative"
	dpSeedRow(t, db, id, "feat: work only in the ground", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.State != deliveryProofUndelivered {
		t.Fatalf("state = %q, want %q: %s is in NO destination, cache or delivery point (ground tip %s vs point %s)",
			out.State, deliveryProofUndelivered, branchDeleteShortHash(groundTip),
			branchDeleteShortHash(groundTip), branchDeleteShortHash(masterTip))
	}
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err == nil {
		t.Fatal("an undelivered clone ground must still be refused")
	}
}

// TestDeliveryProof_LiveWitnessIsSilentWhenThePointIsUnreachable pins the
// offline contract: an unreachable delivery point is an ABSENCE of evidence,
// never a verdict of its own — the caches still decide, exactly as before.
func TestDeliveryProof_LiveWitnessIsSilentWhenThePointIsUnreachable(t *testing.T) {
	ctx := context.Background()
	ground, _, _, _ := dpCloneChannel(t)
	if got, ok := deliveryProofLiveDestination(ctx, ground, masterBranchName); !ok || got.Ref == "" {
		t.Fatal("the reachable fixture point must resolve — otherwise the offline arm proves nothing")
	}
	if got, ok := deliveryProofLiveDestination(ctx, ground, "task/no-such-branch"); ok || got.Ref != "" {
		t.Fatalf("a branch the point does not carry must answer no destination, got %+v ok=%v", got, ok)
	}
	gone := filepath.Join(t.TempDir(), "no-remote-here")
	if got, ok := deliveryProofLiveDestination(ctx, gone, masterBranchName); ok || got.Ref != "" {
		t.Fatalf("a directory with no origin must answer no destination, got %+v ok=%v", got, ok)
	}
}

// linePrefixed reports whether out carries the exact line want (ls-remote pads
// with a tab, and a trailing newline is not part of the answer).
func linePrefixed(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if line == want {
			return true
		}
	}
	return false
}
