package tools

// TWO-ARM REGRESSION for THE TIP THAT SURVIVES ITS DIRECTORY — the fix that puts
// the run's tip into the GROUND-DELETION record (task_ground_removal_record.go)
// so the delivery proof can judge BY SHA after the ground is gone.
//
// THE DEFECT (measured 2026-10-29 on this host). The close-out of a clone-channel
// run deletes its ground with `devworkspace.Reclaim` → `os.RemoveAll`, and that
// removal wrote NO record at all. The delivery proof's witnesses were:
//
//	1. the ground's HEAD            → the directory is deleted by this very step
//	2. a branch-delete record's sha → no such record exists for a clone ground
//	                                   (that arm deletes the REF, and only for
//	                                   rows that reached the branch step)
//	3. the row's landed record      → written on the DELIVERED path only
//
// so a commit that WAS an ancestor of origin/master came back
// `delivery-proof-unverified` for the sole reason that a DIRECTORY no longer
// existed. Measured on the sink: 17704 `kind=worktree-removal` records, ZERO
// carrying a sha, against 495 of 5259 `kind=branch-delete` ones.
//
// THE TWO ARMS, and neither is optional:
//
//	(the record names the tip + the commit IS on the integration branch)  → DELIVERED
//	(the record names the tip + the commit is NOT on the branch)          → STILL REFUSED
//
// THE RED HALF IS THE TIP ITSELF. With the reader reverted
// (deliveryProofGroundRemovalTip removed from deliveryProofTipOf) the arm has NO
// witness at all in this fixture — there is deliberately no landed record — so
// `out.Tip` is "" and the state is `unknown`: these tests fail on their FIRST
// assertion, which is the defect's own sentence ("the tip this run produced
// cannot be named"). That is why the first assertion is on `out.Tip` and not on
// the state.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// dpgpChannel builds a clone-channel shape whose ground has been DELETED the way
// the close-out deletes it — record first, directory second — and returns the
// recorded repo, the ground's (now absent) path, and the run's own tip.
//
// `landIt` decides the arms: true pushes the run's commit to the integration
// branch (the channel's own refspec) before the ground goes; false leaves it
// existing only in the ground, which is then reclaimed.
//
// It pins its OWN removal-audit sink (Rule 34's shape: a test gets its
// environment from the scaffold) because this file WRITES records — the
// operator's live sink is read-only from a test's point of view.
func dpgpChannel(t *testing.T, landIt bool, taskID string) (repo, goneGround, tip string) {
	t.Helper()
	t.Setenv(removalaudit.EnvSink, filepath.Join(t.TempDir(), removalaudit.FileName))

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
	rebuildTestGit(t, seed, "config", "user.email", "dpgp@test")
	rebuildTestGit(t, seed, "config", "user.name", "dpgp")
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", "f.txt")
	rebuildTestGit(t, seed, "commit", "-q", "-m", "base")
	rebuildTestGit(t, seed, "push", "-q", bare, masterBranchName)

	ground := filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", bare, ground)
	rebuildTestGit(t, ground, "config", "user.email", "dpgp@test")
	rebuildTestGit(t, ground, "config", "user.name", "dpgp")
	if err := os.WriteFile(filepath.Join(ground, "work.txt"), []byte("the run's own work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", "work.txt")
	rebuildTestGit(t, ground, "commit", "-q", "-m", "the work this run produced")
	tip = rebuildTestGit(t, ground, "rev-parse", "HEAD")
	if landIt {
		rebuildTestGit(t, ground, "push", "-q", deliveryProofOriginRemote, "HEAD:refs/heads/"+masterBranchName)
	}
	// The recorded repo is a SECOND clone of the same delivery point: the
	// directory the row names as `repo`, which outlives the ground and can answer
	// for origin/master.
	repo = filepath.Join(root, "recorded")
	rebuildTestGit(t, root, "clone", "-q", bare, repo)
	// THE CLOSE-OUT'S OWN ORDER, THROUGH THE PRODUCTION CALL: record (with the
	// tip, read while the directory exists), then delete.
	RecordGroundRemovalTip(removalaudit.ArmCloneChannelReclaim, repo, ground,
		TaskGroundBranchName(taskID), taskID, "push accepted; the ground is reclaimed")
	if err := os.RemoveAll(ground); err != nil {
		t.Fatalf("the fixture must be able to reclaim the ground: %v", err)
	}
	if _, err := os.Stat(ground); !os.IsNotExist(err) {
		t.Fatalf("the fixture's ground must be GONE (stat err = %v)", err)
	}
	return repo, ground, tip
}

// TestDeliveryProof_GroundDeletionRecordNamesTheTip_Delivered is ARM ONE: the
// record of the ground's deletion is the ONLY witness left, and the commit is on
// the integration branch.
func TestDeliveryProof_GroundDeletionRecordNamesTheTip_Delivered(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	id := "dpgp-delivered"
	repo, goneGround, tip := dpgpChannel(t, true, id)

	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "feat: the work landed and the ground was reclaimed", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, branch, goneGround)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.Tip != tip {
		t.Fatalf("the ground-deletion record must name the run's tip after the directory is gone: got %q "+
			"(%s), want %q\n(this is the pre-fix reading: no witness at all)", out.Tip, out.Note, tip)
	}
	if out.State != deliveryProofDelivered {
		t.Fatalf("a commit on the integration branch must be DELIVERED even with the ground gone; "+
			"got state=%q note=%s", out.State, out.Note)
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

// TestDeliveryProof_GroundDeletionRecordNamesTheTip_StillRefusesUnlanded is ARM
// TWO, and it is what keeps this repair from being "judge everything delivered":
// the record names a tip that the delivery point does NOT hold.
//
// A record of a removal is evidence about what the directory's HEAD pointed at.
// It says NOTHING about whether that commit arrived, so the containment arm below
// still decides — and here it must decide against the row.
func TestDeliveryProof_GroundDeletionRecordNamesTheTip_StillRefusesUnlanded(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	id := "dpgp-undelivered"
	repo, goneGround, tip := dpgpChannel(t, false, id)

	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "feat: the push never landed and the ground is gone", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, branch, goneGround)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.Tip != tip {
		t.Fatalf("the ground-deletion record must name the tip even when the commit did not land: got %q, want %q",
			out.Tip, tip)
	}
	if out.State != deliveryProofUndelivered {
		t.Fatalf("a commit the delivery point does not hold must be UNDELIVERED even though a removal record "+
			"names it; got state=%q note=%s", out.State, out.Note)
	}
	werr := func() error {
		_, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done")
		return err
	}()
	if werr == nil {
		t.Fatalf("row %s reached %s while its tip %s is in NEITHER origin/%s NOR the patches of it — "+
			"the repair made the judgement unconditionally green",
			id, taskStatusCompleted, branchDeleteShortHash(tip), masterBranchName)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusInProgress {
		t.Errorf("row status after the refused write = %q, want %q (the write must not land)", got, taskStatusInProgress)
	}
}

// TestDeliveryProof_GroundDeletionRecordIsReadByBranch pins WHERE the tip is
// read from: the record the close-out writes names the branch the ground stood
// on, and the reader asks it by the row's declared address FIRST and by its
// derived `task/<id>` second. A record keyed on some other name is not evidence
// about this row, so it must leave the tip unknown.
func TestDeliveryProof_GroundDeletionRecordIsReadByBranch(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	sink := filepath.Join(t.TempDir(), removalaudit.FileName)
	t.Setenv(removalaudit.EnvSink, sink)

	id := "dpgp-foreign"
	branch := TaskGroundBranchName(id)
	dpSeedRow(t, db, id, "feat: nobody recorded this row's ground", taskStatusInProgress)
	dpWriteDelivery(t, db, id, t.TempDir(), branch, filepath.Join(t.TempDir(), "gone"))

	// A record for ANOTHER branch, carrying a real-shaped sha.
	removalaudit.WorktreeRemoveAt(removalaudit.ArmCloneChannelReclaim, "/repo", "/elsewhere",
		"task/somebody-elses-row", "somebody-else", strings.Repeat("a", 40), "push accepted")

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.Tip != "" {
		t.Fatalf("a record keyed on another branch must not name this row's tip: got %q", out.Tip)
	}
	if out.State != deliveryProofUnknown {
		t.Fatalf("with no witness at all the arm must answer unknown, got state=%q note=%s", out.State, out.Note)
	}
	if !strings.Contains(out.Note, "cannot be named") {
		t.Errorf("the unnameable note must say what was looked for, got: %s", out.Note)
	}
}
