package tools

// THE GROUND-GONE FALSE POSITIVE, AND ITS SECOND ARM — the regression for the
// criterion this file's sibling implements (task_delivery_proof_produced.go).
//
// THE DEFECT (row 01a18253-f7c4-7cf7-8cb3-868647a25401, measured on the live
// ledger 2026-10-28). `tipIsTheCutPoint` can answer "did this run produce any
// commit?" only from the GROUND's object store, because both objects it
// compares (the tip and the branch reflog's oldest position) must be readable in
// one place. Once the ground is reclaimed it returns false — correctly, since an
// unread cut point must never manufacture a "produced nothing" verdict — and the
// containment arms then answer "contained" for the cut-point commit, which is an
// ancestor of the integration branch BY CONSTRUCTION (it was cut from it). The
// row is judged `delivered`, which RELEASES the ground and lets the writeback
// land: a run that never committed anything is indistinguishable, on every
// surface, from one that delivered.
//
// THE MEASURED INSTANCE: `01a17dc1-4fe2-7c63-9b1d-f1add338eda8` — ground cut at
// 6c752f6, recorded tip 6c752f685 (the same commit), run 2m24s long, channel's
// landed record "delivered … at 6c752f685" (a push of an unmoved HEAD succeeds
// trivially), and the arm answered `delivered`.
//
// THE TWO ARMS, and neither may be dropped:
//
//	ARM ONE (this file's TestDeliveryProof_GroundGoneCutPointIsNotADelivery)
//	  ground deleted, tip IS the cut point, the channel recorded pushing it,
//	  the row's execution clock is LATER than the commit's own stamps
//	  → must NOT be `delivered`.
//
//	ARM TWO (TestDeliveryProof_GroundGoneProducedTipIsStillDelivered)
//	  ground deleted, tip is a commit THIS RUN made (its stamps fall inside the
//	  row's execution window) and it is at the delivery point
//	  → must still be `delivered` (d370a6e's positive arm, which must not regress
//	  into `unknown`).
//
// ARM ONE IS THE RED PROOF'S SUBJECT: with the produced-tip witness disabled the
// same fixture comes back `delivered`, and the test fails naming it.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// dpcgCutEpoch is when the fixture's base commit (the cut point) was made, and
// dpcgRunStart is when the ROW's execution began. The gap is deliberately far
// wider than deliveryProofClockSkewSeconds so the fixture measures the criterion
// rather than the slack constant (Rule 1: no bare number at a call site).
const (
	dpcgCutEpoch = 1793000000.0
	dpcgRunStart = 1793003600.0 // one hour after the cut, well past the skew
)

// dpcgClockGit runs one git command in dir with PINNED commit stamps, so a
// fixture can place a commit before or after the row's execution clock — the
// two sides of the criterion. It goes through testutil.GitEnv for the same
// reason every other fixture here does: a child git must not inherit the
// caller's repository routing.
func dpcgClockGit(t *testing.T, dir string, at float64, args ...string) string {
	t.Helper()
	stamp := strconv.FormatInt(int64(at), 10) + " +0000"
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp,
		"GIT_AUTHOR_NAME=dpcg", "GIT_AUTHOR_EMAIL=dpcg@test",
		"GIT_COMMITTER_NAME=dpcg", "GIT_COMMITTER_EMAIL=dpcg@test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// dpcgChannel builds the post-2026-10-28 clone-channel shape and returns the
// recorded repo, the NOW-DELETED ground, and the tip the row's landed record
// will name.
//
// `commitWork` decides the two arms: true makes the run's own commit (stamped
// INSIDE the run's window) and pushes it to the delivery point — a real
// delivery. false leaves the ground ON ITS CUT POINT (no commit at all) and
// records the cut-point sha as the tip — the false positive.
func dpcgChannel(t *testing.T, commitWork bool) (repo, goneGround, tip string) {
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
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", "f.txt")
	// THE CUT POINT: stamped an hour BEFORE the row's execution starts.
	dpcgClockGit(t, seed, dpcgCutEpoch, "commit", "-q", "-m", "base — the commit every ground is cut from")
	rebuildTestGit(t, seed, "push", "-q", bare, masterBranchName)
	cut := rebuildTestGit(t, seed, "rev-parse", "HEAD")

	ground := filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", bare, ground)
	rebuildTestGit(t, ground, "config", "user.email", "dpcg@test")
	rebuildTestGit(t, ground, "config", "user.name", "dpcg")
	if info, err := os.Stat(filepath.Join(ground, dpGroundGit)); err != nil || !info.IsDir() {
		t.Fatalf("fixture must be a CLONE ground (.git a directory): %v", err)
	}
	tip = cut
	if commitWork {
		if err := os.WriteFile(filepath.Join(ground, "work.txt"), []byte("the run's own work\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		rebuildTestGit(t, ground, "add", "work.txt")
		// STAMPED INSIDE THE RUN'S WINDOW: this is the run's own commit.
		dpcgClockGit(t, ground, dpcgRunStart+60, "commit", "-q", "-m", "the work this run produced")
		tip = rebuildTestGit(t, ground, "rev-parse", "HEAD")
		if tip == cut {
			t.Fatalf("fixture: the run's commit must move the tip off its cut point")
		}
		// THE CHANNEL'S OWN REFSPEC (devworkspace.publishGround).
		rebuildTestGit(t, ground, "push", "-q", deliveryProofOriginRemote, "HEAD:refs/heads/"+masterBranchName)
	}
	repo = filepath.Join(root, "recorded")
	rebuildTestGit(t, root, "clone", "-q", bare, repo)
	// AND NOW THE GROUND GOES — the precondition of the whole class.
	if err := os.RemoveAll(ground); err != nil {
		t.Fatalf("the fixture must be able to reclaim the ground: %v", err)
	}
	return repo, ground, tip
}

// dpcgSeedRow inserts the row with an EXPLICIT execution clock, because the
// criterion compares the tip's commit stamps against exactly that clock and the
// shared dpSeedRow stamps a fixed historical value.
func dpcgSeedRow(t *testing.T, db, id, summary string) {
	t.Helper()
	ctx := context.Background()
	handle, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer handle.Close()
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, payload, deliverable, tags, source,
		                    created_at, updated_at, claimed_at, in_progress_at)
		 VALUES (?, ?, ?, '', '', '', 'auto', ?, ?, ?, ?)`,
		id, summary, taskStatusInProgress, dpcgRunStart, dpcgRunStart, dpcgRunStart, dpcgRunStart); err != nil {
		t.Fatalf("seed row %s: %v", id, err)
	}
}

// TestDeliveryProof_GroundGoneCutPointIsNotADelivery is ARM ONE — the false
// positive this repair exists for.
//
// THE FIXTURE IS THE MEASURED ROW'S SHAPE, exactly: the ground never committed
// (HEAD is its cut point), the channel pushed that unmoved HEAD successfully and
// wrote its landed record naming it, the ground was then reclaimed, and the row
// closed. Every containment arm answers "contained" because the cut-point commit
// is on the integration branch — so the ONLY thing that can refuse it is the
// produced-tip witness.
func TestDeliveryProof_GroundGoneCutPointIsNotADelivery(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, goneGround, tip := dpcgChannel(t, false)

	id := "dpcg-empty-run"
	branch := TaskGroundBranchName(id)
	dpcgSeedRow(t, db, id, "a run that never made a commit of its own")
	dpWriteDelivery(t, db, id, repo, branch, goneGround)
	dpggRecord(t, db, id, branch, goneGround, tip)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.Tip != tip {
		t.Fatalf("the tip must still be NAMED by the row's landed record: got %q, want %q (note: %s)",
			out.Tip, tip, out.Note)
	}
	if out.State == deliveryProofDelivered {
		t.Fatalf("a ground that never committed sits ON its cut point (%s), and that commit is an ancestor of the "+
			"integration branch BY CONSTRUCTION — judging it delivered releases the ground for a run that produced "+
			"nothing. got state=%q note=%s", branchDeleteShortHash(tip), out.State, out.Note)
	}
	if out.State != deliveryProofUnknown {
		t.Errorf("the honest answer for a run that produced no commit is %q (recorded, not refused); got %q note=%s",
			deliveryProofUnknown, out.State, out.Note)
	}
	if !containsAll(out.Note, "existed BEFORE this run began", branchDeleteShortHash(tip)) {
		t.Errorf("the note must state the measurement it refused on (the tip predates the run's clock), got: %s", out.Note)
	}
}

// TestDeliveryProof_GroundGoneProducedTipIsStillDelivered is ARM TWO — the
// mirror that keeps the repaired criterion from being "always unknown".
//
// Same shape (clone ground, ground reclaimed, landed record naming the tip), but
// this run DID make its commit and the channel pushed it. Its stamps fall inside
// the row's execution window, so the disproof does not fire and the row must
// still close as `delivered` — d370a6e's positive arm.
func TestDeliveryProof_GroundGoneProducedTipIsStillDelivered(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, goneGround, tip := dpcgChannel(t, true)

	id := "dpcg-produced"
	branch := TaskGroundBranchName(id)
	dpcgSeedRow(t, db, id, "a run whose commit really was pushed")
	dpWriteDelivery(t, db, id, repo, branch, goneGround)
	dpggRecord(t, db, id, branch, goneGround, tip)

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.State != deliveryProofDelivered {
		t.Fatalf("a commit THIS RUN made (its stamps are inside the run's window) and pushed to the integration "+
			"branch must be DELIVERED even with the ground gone; got state=%q note=%s", out.State, out.Note)
	}
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err != nil {
		t.Fatalf("the write must not be refused for a delivered row: %v", err)
	}
	if n := dpEventCount(t, db, id, DeliveryProofUnverifiedEvent); n != 0 {
		t.Errorf("a judged-delivered row must carry no %s record, got %d", DeliveryProofUnverifiedEvent, n)
	}
}

// TestDeliveryProof_ProducedWitnessIsThreeValued pins the ABSENCE branch: a row
// whose ledger carries no execution clock at all must NOT be called "produced
// nothing" — that would turn every legacy row into `unknown` and undo the 42
// rows the previous repair recovered.
func TestDeliveryProof_ProducedWitnessIsThreeValued(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo, goneGround, tip := dpcgChannel(t, false)

	id := "dpcg-no-clock"
	branch := TaskGroundBranchName(id)
	// dpSeedRow stamps created_at=dpSeedClock and leaves claimed_at /
	// in_progress_at empty; then the clock is CLEARED entirely so the witness
	// has nothing to compare against.
	dpSeedRow(t, db, id, "a legacy row with no execution clock", taskStatusInProgress)
	clearRowClock(t, db, id)
	dpWriteDelivery(t, db, id, repo, branch, goneGround)
	dpggRecord(t, db, id, branch, goneGround, tip)

	verdict, evidence := deliveryProofProduced(context.Background(), id, repo, tip)
	if verdict != deliveryProofProducedUnreadable {
		t.Fatalf("a row with NO clock cannot answer the question: got verdict=%q evidence=%s",
			verdict, evidence)
	}
	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("TaskDeliveryProof: %v", err)
	}
	if out.State != deliveryProofDelivered {
		t.Fatalf("with no clock the witness must fall through and change NOTHING — the row keeps the verdict the "+
			"other arms reach; got state=%q note=%s", out.State, out.Note)
	}
}

// clearRowClock empties every stamp deliveryProofRowStart may read, so the
// fixture tests the ABSENT case rather than a value it happened to seed.
func clearRowClock(t *testing.T, db, id string) {
	t.Helper()
	ctx := context.Background()
	handle, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer handle.Close()
	if _, err := handle.ExecContext(ctx,
		`UPDATE tasks SET created_at = 0, claimed_at = NULL, in_progress_at = NULL WHERE task_id = ?`, id); err != nil {
		t.Fatalf("clear clock on %s: %v", id, err)
	}
}
