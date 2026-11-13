package tools

// THE RED PROOF for the delivery-proof criterion (task_delivery_proof.go).
//
// This file is written against the PRE-FIX API ONLY — no symbol introduced by
// the fix appears in it — so it can be copied into a detached checkout of the
// commit before the fix and RUN there, where it must FAIL and print the
// symptom:
//
//	a `completed` row whose deliverable commits are absent from the
//	destination the row declared (the clone channel's origin/<branch>)
//
// After the fix the same file PASSES. That A/B is the whole point: a test that
// can only compile against the new code proves nothing about the defect it
// claims to have closed.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

const (
	zzrpClock    = 1792679579.0
	zzrpGitDir   = ".git"
	zzrpEmail    = "zzrp@test"
	zzrpUserName = "zzrp"
)

func zzrpGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return rebuildTestGit(t, dir, args...)
}

func zzrpSeed(t *testing.T, dbPath, id, status, repo, branch, ground string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, payload, deliverable, tags, source, created_at, updated_at)
		 VALUES (?, ?, ?, '', ?, '', 'auto', ?, ?)`,
		id, "feat: the work nobody delivered", status,
		fmt.Sprintf(`{"repo":%q,"branch":%q,"worktree":%q}`, repo, branch, ground),
		zzrpClock, zzrpClock); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func zzrpStatus(t *testing.T, dbPath, id string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	row, err := taskByID(ctx, db, id)
	if err != nil || row == nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return row.Status
}

// zzrpCloneChannel builds the incident's shape: a bare delivery point and a
// CLONE ground (its `.git` a DIRECTORY) carrying one commit that exists
// nowhere else.
func zzrpCloneChannel(t *testing.T) (ground, repo, masterTip, groundTip string) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "server")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	zzrpGit(t, bare, "init", "-q", "--bare", "-b", masterBranchName)
	repo = filepath.Join(root, "seed")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	zzrpGit(t, repo, "init", "-q", "-b", masterBranchName)
	zzrpGit(t, repo, "config", "user.email", zzrpEmail)
	zzrpGit(t, repo, "config", "user.name", zzrpUserName)
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zzrpGit(t, repo, "add", "f.txt")
	zzrpGit(t, repo, "commit", "-q", "-m", "base")
	zzrpGit(t, repo, "push", "-q", bare, masterBranchName)

	ground = filepath.Join(root, "ground")
	zzrpGit(t, root, "clone", "-q", bare, ground)
	zzrpGit(t, ground, "config", "user.email", zzrpEmail)
	zzrpGit(t, ground, "config", "user.name", zzrpUserName)
	masterTip = zzrpGit(t, ground, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(ground, "work.txt"), []byte("delivered nowhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zzrpGit(t, ground, "add", "work.txt")
	zzrpGit(t, ground, "commit", "-q", "-m", "work nobody delivered")
	groundTip = zzrpGit(t, ground, "rev-parse", "HEAD")
	if info, err := os.Stat(filepath.Join(ground, zzrpGitDir)); err != nil || !info.IsDir() {
		t.Fatalf("fixture must be a CLONE ground (.git a directory): %v", err)
	}
	return ground, repo, masterTip, groundTip
}

// TestRedProofDeliveryProof_CompletedRowWithAbsentCommits is THE red proof. It
// FAILS against the pre-fix tree (the write returns nil and the row lands
// `completed` while the ground's own HEAD is ahead of the declared destination)
// and PASSES once the delivery-proof arm refuses that write.
func TestRedProofDeliveryProof_CompletedRowWithAbsentCommits(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, repo, masterTip, groundTip := zzrpCloneChannel(t)
	ctx := context.Background()
	ahead, err := branchDeleteGit(ctx, ground, "rev-list", "--left-right", "--count",
		"origin/"+masterBranchName+"...HEAD")
	if err != nil {
		t.Fatalf("measure the fixture: %v", err)
	}
	id := "zzrp-undelivered"
	zzrpSeed(t, db, id, taskStatusInProgress, repo, masterBranchName, ground)

	out, wbErr := TaskUpdateWithResult(id, taskStatusCompleted, "work done")
	status := zzrpStatus(t, db, id)
	// THE SYMPTOM SENTENCE — printed on both sides so a pre-fix run reads as
	// the defect, not as a mystery.
	symptom := fmt.Sprintf(
		"SYMPTOM: row %s reached %q while its deliverable commits are absent from the declared destination — "+
			"ground HEAD %s vs origin/%s %s, `rev-list --left-right --count origin/%s...HEAD` = %s; writeback returned err=%v out=%q",
		id, status, strings.TrimSpace(groundTip), masterBranchName, strings.TrimSpace(masterTip),
		masterBranchName, strings.TrimSpace(ahead), wbErr, out)
	if status == taskStatusCompleted {
		t.Fatalf("%s\n→ the delivery-proof criterion is ABSENT: a success terminal was written on an undelivered deliverable "+
			"(row 01a163df-a105-71b8-a297-680c5d35d093 is the measured incident)", symptom)
	}
	if wbErr == nil {
		t.Fatalf("the undelivered writeback must be REFUSED, got nil error\n%s", symptom)
	}
	// A live incident row: this is only a fixture for the write, but its
	// directive matters — real deliverable column contents, real git, real
	// ledger. Nothing else about the row is asserted here.
	if status != taskStatusInProgress {
		t.Errorf("a refused write must leave the row OPEN, got %q", status)
	}
}
