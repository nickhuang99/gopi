package tools

// Regression for THE DELIVERY-PROOF criterion (task_delivery_proof.go).
//
// THE BUG (row 01a163df-a105-71b8-a297-680c5d35d093, measured 2026-10-22): the
// row reached `completed` while all three of its commits existed only in its
// clone ground and never reached the delivery point —
//
//	$ git -C <ground> rev-list --left-right --count origin/master...HEAD
//	0	3
//
// — so every surface that renders a terminal row rendered this one exactly like
// a delivered one.
//
// These tests drive the REAL write path (TaskUpdate / TaskUpdateWithResult —
// the one local mutation core, taskUpdateStampedFrom) against REAL scratch git
// repos, so they go RED the moment the arm is neutralized: without it the write
// returns a nil error and the row lands terminal, and every assertion on the
// returned error is then an assertion on "".

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	dpSeedClock = 1792679579.0 // the incident row's ended_at, as a REAL column value
	dpGroundGit = ".git"
)

// dpWriteDelivery stamps a row's deliverable blob directly (the submit engine
// does this on the real path; a fixture needs the same column contents).
func dpWriteDelivery(t *testing.T, dbPath, id, repo, branch, ground string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	blob := fmt.Sprintf(`{"repo":%q,"branch":%q,"worktree":%q}`, repo, branch, ground)
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, id); err != nil {
		t.Fatalf("stamp deliverable on %s: %v", id, err)
	}
}

// dpSeedRow inserts one open row the delivery check will judge.
func dpSeedRow(t *testing.T, dbPath, id, summary, status string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, payload, deliverable, tags, source, created_at, updated_at)
		 VALUES (?, ?, ?, '', '', '', 'auto', ?, ?)`,
		id, summary, status, dpSeedClock, dpSeedClock); err != nil {
		t.Fatalf("seed row %s: %v", id, err)
	}
}

// dpRowStatus reads the row's status back — the assertion the ROOT symptom
// needs (a refused write must leave the row OPEN).
func dpRowStatus(t *testing.T, dbPath, id string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	row, err := taskByID(ctx, db, id)
	if err != nil || row == nil {
		t.Fatalf("read row %s: %v", id, err)
	}
	return row.Status
}

// dpEventCount counts trail entries of one event for a row.
func dpEventCount(t *testing.T, dbPath, id, event string) int {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event LIKE ?`, id, event+"%").Scan(&n); err != nil {
		t.Fatalf("count events for %s: %v", id, err)
	}
	return n
}

// dpCloneChannel builds the incident's OWN shape: a BARE delivery point, a
// CLONE ground of it (`.git` a DIRECTORY — the channel discriminator), and a
// commit on the ground that exists NOWHERE else. Returns (ground, bare,
// masterTip, groundTip) where masterTip is what the delivery point holds and
// groundTip is the undelivered commit.
func dpCloneChannel(t *testing.T) (ground, bare, masterTip, groundTip string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "server")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, bare, "init", "-q", "--bare", "-b", masterBranchName)

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "init", "-q", "-b", masterBranchName)
	rebuildTestGit(t, seed, "config", "user.email", "dp@test")
	rebuildTestGit(t, seed, "config", "user.name", "dp")
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", "f.txt")
	rebuildTestGit(t, seed, "commit", "-q", "-m", "base")
	rebuildTestGit(t, seed, "push", "-q", bare, masterBranchName)

	ground = filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", bare, ground)
	rebuildTestGit(t, ground, "config", "user.email", "dp@test")
	rebuildTestGit(t, ground, "config", "user.name", "dp")
	masterTip = rebuildTestGit(t, ground, "rev-parse", "HEAD")
	// The undelivered commit: real work, committed, never pushed.
	if err := os.WriteFile(filepath.Join(ground, "work.txt"), []byte("delivered nowhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", "work.txt")
	rebuildTestGit(t, ground, "commit", "-q", "-m", "work nobody merged")
	groundTip = rebuildTestGit(t, ground, "rev-parse", "HEAD")
	if info, err := os.Stat(filepath.Join(ground, dpGroundGit)); err != nil || !info.IsDir() {
		t.Fatalf("fixture must be a CLONE ground (.git a directory): %v", err)
	}
	return ground, bare, masterTip, groundTip
}

// TestDeliveryProof_RefusesUndeliveredCloneGround is THE regression: the
// incident's exact shape must NOT reach a success terminal, on the writeback
// door (TaskUpdateWithResult) as well as the plain one.
//
// RED WHEN THE ARM IS REVERTED: every call below returns nil and the row lands
// `completed`, so the "want a refusal" assertions fail on "" and on the status.
func TestDeliveryProof_RefusesUndeliveredCloneGround(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, masterTip, groundTip := dpCloneChannel(t)
	repo := filepath.Join(filepath.Dir(ground), "seed")

	for _, status := range []string{taskStatusCompleted, taskStatusVerified} {
		t.Run(status, func(t *testing.T) {
			id := "dp-undelivered-" + status
			from := taskStatusInProgress
			if status == taskStatusVerified {
				from = taskStatusFailed
			}
			dpSeedRow(t, db, id, "feat: make the group signal safe", from)
			dpWriteDelivery(t, db, id, repo, masterBranchName, ground)

			var err error
			if status == taskStatusCompleted {
				_, err = TaskUpdateWithResult(id, status, "work done")
			} else {
				_, err = TaskUpdateReason(id, status, "evidence: dp probe", "")
			}
			if err == nil {
				t.Fatalf("row %s reached %s while its ground HEAD %s is 3 commits ahead of the delivery point %s — "+
					"the delivery-proof arm is not refusing (reverted?); this is row 01a163df's exact shape",
					id, status, branchDeleteShortHash(groundTip), branchDeleteShortHash(masterTip))
			}
			if !strings.Contains(err.Error(), deliveryProofMarker) {
				t.Errorf("refusal must name the undelivered deliverable, got: %v", err)
			}
			if got := dpRowStatus(t, db, id); got != from {
				t.Errorf("row status after the refused write = %q, want %q (the write must not land)", got, from)
			}
			if n := dpEventCount(t, db, id, deliveryProofOwedEvent); n == 0 {
				t.Errorf("the refusal must leave a %s trail entry on the still-open row", deliveryProofOwedEvent)
			}
		})
	}
}

// TestDeliveryProof_AllowsLandedBranch proves the arm does not refuse a
// LEGITIMATE close: the very same fixture, after the ground's work is pushed to
// the delivery point.
func TestDeliveryProof_AllowsLandedBranch(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, _, groundTip := dpCloneChannel(t)
	repo := filepath.Join(filepath.Dir(ground), "seed")
	// Push through the remote NAME (the channel's own addressing), so the
	// ground's tracking ref — the declared destination this arm measures — moves
	// the way the delivery loop moves it.
	rebuildTestGit(t, ground, "push", "-q", deliveryProofOriginRemote, masterBranchName)

	id := "dp-delivered"
	dpSeedRow(t, db, id, "feat: landed work", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err != nil {
		t.Fatalf("a landed deliverable (%s at the delivery point) must NOT be refused, got: %v",
			branchDeleteShortHash(groundTip), err)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusCompleted {
		t.Errorf("row status = %q, want %q", got, taskStatusCompleted)
	}
}

// TestDeliveryProof_AllowsPatchEquivalent is the FALSE-REFUSAL trap memory #3342
// names: this repo lands work by cherry-pick/rebase, so the ground stays AHEAD
// by commit identity while the delivery point holds the identical patch. A
// guard with only the ancestry arm refuses exactly those closes.
func TestDeliveryProof_AllowsPatchEquivalent(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, bare, masterTip, groundTip := dpCloneChannel(t)
	repo := filepath.Join(filepath.Dir(ground), "seed")
	// Re-create the ground's patch on the delivery point as a DIFFERENT commit
	// (the merger's adoption shape), so neither is an ancestor of the other.
	cherry := filepath.Join(filepath.Dir(ground), "cherry")
	rebuildTestGit(t, filepath.Dir(ground), "clone", "-q", bare, cherry)
	rebuildTestGit(t, cherry, "config", "user.email", "dp@test")
	rebuildTestGit(t, cherry, "config", "user.name", "dp")
	if err := os.WriteFile(filepath.Join(cherry, "work.txt"), []byte("delivered nowhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, cherry, "add", "work.txt")
	rebuildTestGit(t, cherry, "commit", "-q", "-m", "adoption: merge the same patch")
	rebuildTestGit(t, cherry, "push", "-q", bare, masterBranchName)
	if _, err := branchDeleteGit(context.Background(), ground, "merge-base", "--is-ancestor", groundTip, "refs/remotes/origin/"+masterBranchName); err == nil {
		t.Fatalf("fixture is wrong: the ground tip must NOT be an ancestor of the delivery point (%s vs %s)",
			branchDeleteShortHash(groundTip), branchDeleteShortHash(masterTip))
	}
	rebuildTestGit(t, ground, "fetch", "-q", deliveryProofOriginRemote)

	id := "dp-patch-equivalent"
	dpSeedRow(t, db, id, "feat: adopted patch", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err != nil {
		t.Fatalf("a PATCH-EQUIVALENT deliverable must NOT be refused (cherry-pick/rebase landing), got: %v", err)
	}
}

// TestDeliveryProof_UnknownIsRecordedNeverRefused pins the three-valued
// contract: a row whose ground is gone and whose destination resolves to
// nothing is UNKNOWN — recorded loudly on the trail, and the write PROCEEDS.
// Refusing what cannot be seen would wedge the ledger (memory #3342).
func TestDeliveryProof_UnknownIsRecordedNeverRefused(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "init", "-q", "-b", masterBranchName)
	rebuildTestGit(t, repo, "config", "user.email", "dp@test")
	rebuildTestGit(t, repo, "config", "user.name", "dp")

	id := "dp-unknown"
	dpSeedRow(t, db, id, "feat: no ground left", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, "task/nowhere", filepath.Join(t.TempDir(), "gone"))

	out, err := TaskDeliveryProof(id)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.State != deliveryProofUnknown {
		t.Fatalf("state = %q, want %q (nothing resolvable must never be reported as undelivered)", out.State, deliveryProofUnknown)
	}
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err != nil {
		t.Fatalf("an UNKNOWN delivery must be recorded, never refused, got: %v", err)
	}
	if n := dpEventCount(t, db, id, DeliveryProofUnverifiedEvent); n == 0 {
		t.Errorf("an unjudged close must leave a %s trail entry (Rule 15: never silent)", DeliveryProofUnverifiedEvent)
	}
}

// TestDeliveryProof_NoBranchIsNotJudged is the control case: a row that
// declares no branch makes no delivery claim, so the arm must not touch it.
func TestDeliveryProof_NoBranchIsNotJudged(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	id := "dp-no-branch"
	dpSeedRow(t, db, id, "chore: no deliverable", taskStatusInProgress)
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "done"); err != nil {
		t.Fatalf("a row with no deliverable branch must not be judged, got: %v", err)
	}
	out, err := TaskDeliveryProof(id)
	if err != nil || out.State != deliveryProofUnknown {
		t.Fatalf("outcome = %+v, %v (want unknown with no refusal)", out, err)
	}
	if strings.HasPrefix(out.Note, deliveryProofMarker) {
		t.Fatalf("a row with no branch must not wear the defect sentence: %s", out.Note)
	}
}

// TestDeliveryProof_RefusalLeavesRowByteIdentical pins Rule 30/15's shape: the
// refused write touches NOTHING on the row (status, ended_at both unchanged).
func TestDeliveryProof_RefusalLeavesRowByteIdentical(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, _, _ := dpCloneChannel(t)
	repo := filepath.Join(filepath.Dir(ground), "seed")
	id := "dp-untouched"
	dpSeedRow(t, db, id, "feat: undelivered", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)

	before := dpRowSnapshot(t, db, id)
	if _, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done"); err == nil {
		t.Fatalf("expected a refusal")
	}
	after := dpRowSnapshot(t, db, id)
	if before != after {
		t.Errorf("the refused write changed the row:\nbefore %s\nafter  %s", before, after)
	}
}

// dpRowSnapshot renders the lifecycle columns a refused write must not touch.
func dpRowSnapshot(t *testing.T, dbPath, id string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var status, reason string
	var ended sql.NullFloat64
	if err := db.QueryRowContext(ctx,
		`SELECT status, COALESCE(reason,''), ended_at FROM tasks WHERE task_id = ?`, id).
		Scan(&status, &reason, &ended); err != nil {
		t.Fatalf("snapshot %s: %v", id, err)
	}
	return fmt.Sprintf("status=%s ended=%v reason=%q", status, ended, reason)
}
