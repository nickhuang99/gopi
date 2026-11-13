package tools

// TWO-ARM REGRESSION for the GROUND-GONE verdict of the delivery-proof arm
// (task_delivery_proof.go), filed on four measured rows: 01a17de7 / 01a17e58 /
// 01a17e03-ef4a / 01a17e03-cb63.
//
// THE DEFECT (measured 2026-10-27/28, this host). All four rows read
// `delivery-proof-unverified` with the sentence "the tip this run produced
// cannot be named: its ground is gone and no removal-audit record kept its sha"
// while their commits were ANCESTORS OF origin/master:
//
//	$ git merge-base --is-ancestor 6afc5ae542de33c81f30458e3fd9733f6ae0085d origin/master
//	$ echo $?
//	0        ← and likewise 8f12b871e / 7c8079dc7 / 5f4c675fe
//
// The arm could JUDGE BY SHA only after it could NAME a tip, and it could name
// one only from a directory that the close-out chain had already reclaimed. The
// row's own landed record — the clone channel's statement naming the very commit
// it pushed — was read exclusively from inside the tip-known path, so it was
// unreachable in exactly the case it was designed for.
//
// THE FIX (three parts, all in task_delivery_proof.go):
//  1. deliveryProofTipOf gains a third witness: the row's own landed record
//     (deliveryProofLandedTip), so a tip is knowable after the ground is gone.
//  2. When the ground is MISSING, the integration branch is asked as a further
//     DESTINATION (deliveryProofIntegrationDestination): the channel publishes
//     HEAD:refs/heads/<integration> while the row records `task/<id>`, so
//     origin/<declared> resolves nowhere and the commit is on master.
//  3. A tip NAMED by the landed record may not then be judged PRESENT by it
//     (tipFromLandedRecord), because one fact answering two questions is how a
//     judgement becomes unconditionally green.
//
// THE TWO ARMS, and neither is optional:
//
//	(everything gone + the commit IS on the integration branch)  → DELIVERED
//	(everything gone + the commit is NOT on the branch)          → STILL REFUSED
//
// The second arm is the reason this fix cannot be "delete the check": with it
// broken, a row whose push never landed would close as delivered, which is row
// 01a163df's exact shape (a success terminal on work that exists nowhere the
// merger can see).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// dpggChannel builds the post-2026-10-28 clone-channel shape and returns the
// recorded repo, the ground's directory (ALREADY DELETED, so a caller cannot
// accidentally measure a live ground), and the run's own tip.
//
// `landIt` decides the two arms: true pushes the run's commit to the
// integration branch (the channel's own refspec) before the ground goes; false
// leaves it exist only in the ground, which is then reclaimed.
func dpggChannel(t *testing.T, landIt bool) (repo, goneGround, tip string) {
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
	rebuildTestGit(t, seed, "config", "user.email", "dpgg@test")
	rebuildTestGit(t, seed, "config", "user.name", "dpgg")
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", "f.txt")
	rebuildTestGit(t, seed, "commit", "-q", "-m", "base")
	rebuildTestGit(t, seed, "push", "-q", bare, masterBranchName)

	ground := filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", bare, ground)
	rebuildTestGit(t, ground, "config", "user.email", "dpgg@test")
	rebuildTestGit(t, ground, "config", "user.name", "dpgg")
	if err := os.WriteFile(filepath.Join(ground, "work.txt"), []byte("the run's own work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", "work.txt")
	rebuildTestGit(t, ground, "commit", "-q", "-m", "the work this run produced")
	tip = rebuildTestGit(t, ground, "rev-parse", "HEAD")
	if info, err := os.Stat(filepath.Join(ground, dpGroundGit)); err != nil || !info.IsDir() {
		t.Fatalf("fixture must be a CLONE ground (.git a directory): %v", err)
	}
	if landIt {
		// THE CHANNEL'S OWN REFSPEC (devworkspace.publishGround): the integration
		// branch, not the row's address.
		rebuildTestGit(t, ground, "push", "-q", deliveryProofOriginRemote, "HEAD:refs/heads/"+masterBranchName)
	}
	// The recorded repo is a SECOND clone of the same delivery point — the
	// directory the row names as `repo`, which persists after the ground is
	// reclaimed and can answer for origin/master.
	repo = filepath.Join(root, "recorded")
	rebuildTestGit(t, root, "clone", "-q", bare, repo)
	// AND NOW THE GROUND GOES — the precondition of the whole repair.
	if err := os.RemoveAll(ground); err != nil {
		t.Fatalf("the fixture must be able to reclaim the ground: %v", err)
	}
	return repo, ground, tip
}

// dpggRecord writes the clone channel's OWN landed record for this row — the
// artefact the arm now reads when the ground is gone. It goes through the one
// writer (TaskDeliveryRecordLanded), never a hand-typed grammar.
func dpggRecord(t *testing.T, db, id, branch, ground, tip string) {
	t.Helper()
	TaskDeliveryRecordLanded(id, branch, ground, tip, "pushed master to origin after 1 attempt(s)")
	if n := dpEventCount(t, db, id, DeliveryLandedEventName); n == 0 {
		t.Fatalf("fixture: the landed record did not land on %s's trail", id)
	}
}

// TestDeliveryProof_GroundGoneStillJudgesBySHA is ARM ONE: the four rows'
// exact shape, resolved the way the fix resolves it.
//
// RED WHEN THE FIX IS REVERTED: deliveryProofTipOf answers "" (no ground, no
// removal-audit sha) and the row comes back `unknown`, so the write is not
// refused but the ROW'S TRAIL carries `delivery-proof-unverified` — which is
// what this test asserts against, because `unknown` is recorded and allowed.
// The assertion is therefore on the VERDICT the arm reached, not on a refusal
// appearing or not.
func TestDeliveryProof_GroundGoneStillJudgesBySHA(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, goneGround, tip := dpggChannel(t, true)

	id := "dpgg-delivered"
	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "feat: the work was pushed and the ground was reclaimed", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, branch, goneGround)
	dpggRecord(t, db, id, branch, goneGround, tip)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.Tip != tip {
		t.Fatalf("the tip must be named by the row's own landed record after the ground is gone: got %q (%s), want %q",
			out.Tip, out.Note, tip)
	}
	if out.State != deliveryProofDelivered {
		t.Fatalf("a commit on the integration branch must be DELIVERED even with the ground gone; "+
			"got state=%q note=%s\n(this is the four measured rows' exact false verdict)", out.State, out.Note)
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

// TestDeliveryProof_GroundGoneStillRefusesUnlandedTip is ARM TWO, and it is the
// one that keeps the repair from being "delete the check": same reclaimed
// ground, same landed record claiming a push — but the delivery point does NOT
// hold the commit.
//
// A channel record is a statement about what was SENT. A destination that does
// not contain the commit answers nothing about it, so the verdict must stay
// RED. With the one-fact-two-answers shortcut in place (the landed record
// serving as both the tip's witness and the proof of arrival) this row closes
// `completed` — row 01a163df's shape, and the reason this arm exists.
func TestDeliveryProof_GroundGoneStillRefusesUnlandedTip(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, goneGround, tip := dpggChannel(t, false)

	id := "dpgg-undelivered"
	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "feat: the push never landed", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, branch, goneGround)
	// The channel SAYS it pushed this commit. It did not reach the destination.
	dpggRecord(t, db, id, branch, goneGround, tip)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.State != deliveryProofUndelivered {
		t.Fatalf("a commit the delivery point does not hold must NOT be judged delivered just because the "+
			"channel recorded pushing it; got state=%q note=%s", out.State, out.Note)
	}
	werr := func() error {
		_, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done")
		return err
	}()
	if werr == nil {
		t.Fatalf("row %s reached %s while its tip %s is in NEITHER origin/%s NOR the patches of it — "+
			"the ground-gone repair made the judgement unconditionally green",
			id, taskStatusCompleted, branchDeleteShortHash(tip), masterBranchName)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusInProgress {
		t.Errorf("row status after the refused write = %q, want %q (the write must not land)", got, taskStatusInProgress)
	}
	if n := dpEventCount(t, db, id, deliveryProofOwedEvent); n == 0 {
		t.Errorf("the refusal must leave a %s trail entry on the still-open row", deliveryProofOwedEvent)
	}
}

// TestDeliveryProof_GroundGoneWithoutAnyWitnessStaysUnknown is the CONTROL: with
// no ground, no removal-audit sha and no landed record, nothing can name the tip
// and the arm must SAY so (Unknown, never a fabricated delivered/undelivered).
// It pins the "cannot be named" sentence the four rows carried to the ONE state
// that still earns it.
func TestDeliveryProof_GroundGoneWithoutAnyWitnessStaysUnknown(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, goneGround, _ := dpggChannel(t, true)

	id := "dpgg-unnameable"
	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "feat: nothing kept the tip", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, branch, goneGround)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.State != deliveryProofUnknown {
		t.Fatalf("with no witness at all the arm must answer unknown, got state=%q note=%s", out.State, out.Note)
	}
	if !containsAll(out.Note, "cannot be named", "landed record") {
		t.Errorf("the unnameable note must say what was looked for, got: %s", out.Note)
	}
}

// containsAll is the tiny helper this file needs (Rule 35: no new dependency
// for an in-string membership test).
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
