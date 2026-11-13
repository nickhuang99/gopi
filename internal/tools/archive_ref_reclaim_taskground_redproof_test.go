package tools

// THE RED PROOF, kept as a shipping test on purpose (Rule 5: every bug fix
// carries a minimal repro).
//
// It is written against the arm's PRE-EXISTING surface ONLY — `ArchiveReclaimDecidedRefs`
// plus the fixture helpers — so the very same file COMPILES on the code before
// the fix and on the code after it. That is what makes the transition a
// measurement rather than an assertion: on the pre-fix arm this test FAILS
// (the ref is still there, refused by its name), on the post-fix arm it passes.
// The table in archive_ref_reclaim_taskground_test.go covers the four facts
// exhaustively but cannot serve as the red proof, because it names the seam and
// the refusal kinds that only exist after the fix.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// TestArchiveReclaimReclaimsAnUnownedTaskGround is the minimal repro of the
// measured defect: a `task/<id>` branch whose archive tag names its own tip,
// whose ledger holds NO row for it and which no worktree holds — the shape a
// finished, merged-then-archived run leaves behind — must be RECLAIMED.
//
// Before the fix the arm refused it with "branch … is the integration branch or
// a task-run ground — never a reclaim candidate", a sentence the same run's own
// measurements contradicted (no row in the ledger named the branch, and
// `git worktree list` held no tree on it).
func TestArchiveReclaimReclaimsAnUnownedTaskGround(t *testing.T) {
	ctx := context.Background()
	repo := rsTestRepo(t)
	// A fresh, pinned ledger with no rows at all: the branch below is owned by
	// nobody, so the ONLY thing that can keep it is a fact about its name
	// (Rule 32 — the scaffold owns the path; the operator's profile is never
	// touched).
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	if db, derr := openTasksDB(ctx, memoryDBPath()); derr != nil {
		t.Fatalf("fixture ledger: %v", derr) // the ONE schema initializer
	} else {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tasks`).Scan(&n); err != nil {
			t.Fatalf("fixture ledger: %v", err)
		}
		db.Close() //stylegate:ignore t.Fatalf above ends the test on a query failure; on the surviving path a read-only ledger close cannot change the assertion that follows
		if n != 0 {
			t.Fatalf("fixture: the branch must be unowned, ledger holds %d row(s)", n)
		}
	}

	const branch = TaskGroundBranchPrefix + "01a0f44c2d0f7a82bde8f76445daa301"
	rsMakeBranch(t, repo, branch)
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)
	tip := reclaimHeadTip(t, repo, branch)
	if tip == "" {
		t.Fatal("fixture: the ground branch must exist")
	}

	res := ArchiveReclaimDecidedRefs(repo)
	if res.Err != "" {
		t.Fatalf("reclaim scan failed: %s", res.Err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("want one row for %s, got %+v", branch, res.Rows)
	}
	if res.Rows[0].Verdict != ArchiveReclaimDeleted || res.Deleted != 1 {
		t.Fatalf("an unowned, unheld task ground must be reclaimed, got %+v (%s)",
			res.Rows[0], res.Rows[0].Reason)
	}
	if after := reclaimHeadTip(t, repo, branch); after != "" {
		t.Fatalf("%s%s still resolves to %s — the residue survived the reclaim", refsHeadsPrefix, branch, after)
	}
	if got := reclaimTagExists(t, repo, branch); got != tip {
		t.Fatalf("the archive tag must survive the reclaim: %q, want %q", got, tip)
	}
	if !DecidedRefReclaimed(repo, branch) {
		t.Fatal("DecidedRefReclaimed must agree with the state the arm produced")
	}
	// IDEMPOTENT: nothing left to reclaim.
	if again := ArchiveReclaimDecidedRefs(repo); again.Err != "" || again.Deleted != 0 || len(again.Rows) != 0 {
		t.Fatalf("second pass must be a no-op, got %+v", again)
	}
	if !strings.Contains(rsGit(t, repo, true, "for-each-ref", "--format=%(refname)", archiveTagRefPrefix), archiveBranchPrefix+branch) {
		t.Fatal("the tag must remain enumerable in the archive namespace")
	}
}
