package tools

// Regression tests for the two rungs this change adds to the PRODUCER release
// arm (`ReclaimOwnFinishedWorktrees` → `reclaimSelfRelease`), which is the arm
// the 2026-09-21 breach came through (`~/workspace/gopi-jieba-searchkw`,
// row 01a0c574-cb48-76ef-9d76-ac93d756e833, holder pid 1191072 alive, branch 14
// ahead — the directory was freed and its checkout became unrecoverable).
//
//	① THE OWN-GROUND rung (`guestRowOwnCheckoutRefusal`): the candidate set is
//	  keyed on the CLAIM STAMP, so a row's recorded worktree can be the very
//	  checkout this process is standing in. `git worktree remove` then deletes
//	  the directory under the running process — invisible until some later step
//	  asks for its own cwd. The row DOES need one deliberate widening to reach
//	  that state (a row whose recorded worktree IS the actor's checkout — the
//	  breach's shape), because a row naming a linked worktree is caught by the
//	  existing guest arm; the widening is stated in the test so nobody reads the
//	  case as the ordinary one.
//	② THE OUTSIDE-HISTORY rung (`reclaimSelfLineageRefusal`): this arm's only
//	  standing is "the row is mine and its life is over", never a judgement
//	  about the work — so a branch sharing NO ancestor with the integration
//	  branch (the state `cmd/agent/merge_run.go` names as unrelated-history) must
//	  not have its ground parked by it.
//
// BOTH ARE RED ON THE PRE-FIX SOURCE: neither rung exists there, so both
// directories go. The red proof is mechanical for ① (the arm's own
// `guestRowGuardOffEnv` switch, the same one the guest tests use) and structural
// for ② (the symbol the rung is named for has no definition on master — see the
// task report's grep).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// reclaimOwnGroundChildEnv marks the child helper; the rest hand the child
	// the fixture it must act on (its own process cannot inherit t.Setenv pins).
	reclaimOwnGroundChildEnv  = "GOPI_TEST_RECLAIM_OWNGROUND_CHILD"
	reclaimOwnGroundLedgerEnv = "GOPI_TEST_RECLAIM_OWNGROUND_LEDGER"
	reclaimOwnGroundSetEnv    = "GOPI_TEST_RECLAIM_OWNGROUND_SETTINGS"
	reclaimOwnGroundTaskEnv   = "GOPI_TEST_RECLAIM_OWNGROUND_TASK"
	reclaimOwnGroundWTEnv     = "GOPI_TEST_RECLAIM_OWNGROUND_WT"

	// reclaimOwnGroundArmPhrase is the arm name the refusal must carry.
	reclaimOwnGroundArmPhrase = "arm guestRowOwnCheckoutRefusal"

	// reclaimLineageArmPhrase is the OUTSIDE-HISTORY refusal's own sentence
	// fragment (Rule 13: one spelling, referenced by the test and the rung).
	reclaimLineageArmPhrase = "sharing no ancestor with " + IntegrationBranchName

	// reclaimOrphanBranch is the unrelated-history branch the second test
	// builds: its tip shares NO commit with master.
	reclaimOrphanBranch = "fix/orphan-root"
)

// TestReclaimOwnGroundHelper is not a test: it is the child-process body of
// TestReclaimSelfRefusesItsOwnCheckout. It runs the REAL arm (exported entry
// point) with the process's cwd INSIDE the candidate worktree and prints the
// arm's own lines plus the resulting state of that directory.
func TestReclaimOwnGroundHelper(t *testing.T) {
	if os.Getenv(reclaimOwnGroundChildEnv) == "" {
		t.Skip("helper — driven by TestReclaimSelfRefusesItsOwnCheckout")
	}
	wt := os.Getenv(reclaimOwnGroundWTEnv)
	taskID := os.Getenv(reclaimOwnGroundTaskEnv)
	// The row must be THIS process's own claim — the shape reclaimSelfRowsFrom
	// selects on (the stamp is written by the production renderer).
	stampGuestRowOwn(t, taskID)
	for _, line := range ReclaimOwnFinishedWorktrees() {
		t.Logf("OWN %s", line)
	}
	_, err := os.Stat(wt)
	t.Logf(guestRowOutcomeFmt, wt, err == nil)
}

// TestReclaimSelfRefusesItsOwnCheckout is the ① test: a row THIS process holds,
// whose deliverable names the directory the process is STANDING IN, must be
// refused — and the directory must survive. With the guard disabled the same
// run frees it (the pre-fix behaviour), which is the red proof.
//
// HONEST READING OF THE OUTPUT (stated rather than glossed): in this case the
// arm that speaks is the EXISTING guest arm (`guestRowOwnReleaseRefusal`),
// because a process standing in a linked worktree of the repository it is asked
// to reconcile IS a guest of it. The rung this change adds
// (`guestRowOwnCheckoutRefusal`) is what covers the pairs that arm leaves open —
// a process RESIDENT in a repo whose row names that very checkout — and its own
// criterion and narrowness are pinned by TestOwnGroundRungIsNarrow below. The
// end-to-end case is kept because it is the breach's OWN shape (the row's
// recorded worktree was the directory the run stood in) and because it proves
// the pass cannot free the actor's ground by ANY route.
func TestReclaimSelfRefusesItsOwnCheckout(t *testing.T) {
	for _, guardOff := range []bool{false, true} {
		name := "own-ground rung wired"
		if guardOff {
			name = "own-ground rung disabled (pre-fix behaviour)"
		}
		t.Run(name, func(t *testing.T) {
			// The FIXTURE pins the ledger and the settings (it must: the arm reads
			// them), so the child's env is read back AFTER it — pinning first and
			// letting the fixture overwrite would hand the child a path no schema
			// was ever created in.
			repo, wt, _, _ := selfReleaseFixture(t, "t-reclaim-own-ground")
			ledger, settings := memoryDBPath(), os.Getenv(config.SettingsEnv)
			// The fixture row names a LINKED worktree, which the existing guest
			// arm would refuse for another reason. Re-point the SAME row's
			// deliverable at the actor's own checkout — the breach's shape
			// (the stamp named the directory the run was standing in).
			repointDeliverableWorktree(t, "t-reclaim-own-ground", repo, wt, wt, "feat/self-release")

			out := runReclaimOwnGroundChild(t, wt, wt, "t-reclaim-own-ground", ledger, settings, guardOff)

			_, err := os.Stat(wt)
			gone := os.IsNotExist(err)
			// wired → the directory SURVIVES (gone=false); with the guard
			// disabled (the pre-fix source) it GOES (gone=true) — that is the
			// red proof, driven from the fixed tree.
			if gone != guardOff {
				want := "kept"
				if guardOff {
					want = "freed"
				}
				t.Fatalf("the worktree %s gone=%v, want %s\narm output:\n%s", wt, gone, want, out)
			}
			if !guardOff && !strings.Contains(out, reclaimSelfRefusedMarker) {
				t.Fatalf("the refusal must be NAMED (%q):\n%s", reclaimSelfRefusedMarker, out)
			}
			if !strings.Contains(out, "OUTCOME") {
				t.Fatalf("the helper must report the outcome it observed:\n%s", out)
			}
			// The BRANCH is the deliverable and is never touched by either rung.
			if _, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "feat/self-release").Output(); err != nil {
				t.Fatalf("the branch must survive the pass: %v", err)
			}
		})
	}
}

// runReclaimOwnGroundChild runs the helper in its own process standing in stand.
func runReclaimOwnGroundChild(t *testing.T, stand, wt, taskID, ledger, settings string, guardOff bool) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestReclaimOwnGroundHelper$", "-test.v")
	cmd.Dir = stand
	cmd.Env = append(os.Environ(),
		reclaimOwnGroundChildEnv+"=1",
		reclaimOwnGroundWTEnv+"="+wt,
		reclaimOwnGroundTaskEnv+"="+taskID,
		reclaimOwnGroundLedgerEnv+"="+ledger,
		reclaimOwnGroundSetEnv+"="+settings,
		memoryDBEnv+"="+ledger,
		config.SettingsEnv+"="+settings,
		guestRowGuardOffEnv+"="+guestRowGuardSwitchValue(guardOff),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("own-ground helper in %s: %v\n%s", stand, err, out)
	}
	return string(out)
}

// repointDeliverableWorktree rewrites one row's deliverable so its recorded
// worktree is `wt` (the fixture stamps a linked worktree; the case under test
// needs the ACTOR's own checkout there).
func repointDeliverableWorktree(t *testing.T, taskID, repo, wt, worktree, branch string) {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ? WHERE task_id = ?`,
		`{"repo":"`+repo+`","branch":"`+branch+`","worktree":"`+worktree+`"}`, taskID); err != nil {
		t.Fatalf("repoint deliverable: %v", err)
	}
}

// TestReclaimSelfRefusesAnUnrelatedHistoryBranch is the ② test: a branch whose
// tip shares NO ancestor with the integration branch must not have its ground
// parked by an arm whose only standing is a claim-stamp match. Before the rung
// existed the disposition found the directory clean, unheld and on the row's own
// branch, and freed it.
func TestReclaimSelfRefusesAnUnrelatedHistoryBranch(t *testing.T) {
	repo, wt, _, _ := selfReleaseFixture(t, "t-reclaim-unrelated")

	// An ORPHAN root: a commit with no parent, so `git merge-base master <branch>`
	// reports no common ancestor — the state cmd/agent/merge_run.go names as
	// unrelated-history.
	gitRun(t, repo, "checkout", "--orphan", reclaimOrphanBranch)
	if err := os.WriteFile(filepath.Join(repo, "orphan.txt"), []byte("no ancestor"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "orphan.txt")
	gitRun(t, repo, "commit", "-m", "an orphan root")
	gitRun(t, repo, "checkout", "master")
	gitRun(t, repo, finishCleanupWorktreeVerb, "remove", "--force", wt)
	gitRun(t, repo, finishCleanupWorktreeVerb, "add", wt, reclaimOrphanBranch)

	// The precondition IS the case: git reports no common ancestor.
	if _, err := exec.Command("git", "-C", repo, reclaimMergeBaseVerb, IntegrationBranchName, reclaimOrphanBranch).Output(); err == nil {
		t.Fatalf("fixture: %s shares an ancestor with %s — the case is not set up", reclaimOrphanBranch, IntegrationBranchName)
	}
	repointDeliverableWorktree(t, "t-reclaim-unrelated", repo, wt, wt, reclaimOrphanBranch)

	got := ReclaimOwnFinishedWorktrees()
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, reclaimSelfRefusedMarker) {
		t.Fatalf("an unrelated-history branch must be REFUSED, got %v", got)
	}
	if !strings.Contains(joined, reclaimLineageArmPhrase) {
		t.Fatalf("the refusal must NAME the failed check (%q), got %s", reclaimLineageArmPhrase, joined)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the refusal must LEAVE the worktree alone (a failed check is never a removal): %v", err)
	}
}

// TestOwnGroundRungIsNarrow pins the rung's criterion directly — the pair the
// guest arm does not cover cannot be presented end to end without a resident
// actor, so the predicate is asserted where its input is a plain path (Rule 10's
// smallest-sample habit): the actor's OWN checkout is refused, every other path
// is not.
func TestOwnGroundRungIsNarrow(t *testing.T) {
	self := worktreeSweepSelfOwnedRepo()
	if self == "" {
		t.Skip("this test process stands in no repository — the criterion is unmeasurable here")
	}
	if why := guestRowOwnCheckoutRefusal(self); why == "" {
		t.Fatalf("the actor's own checkout (%s) must be refused", self)
	} else if !strings.Contains(why, reclaimOwnGroundArmPhrase) {
		t.Fatalf("the refusal must NAME the arm (%q), got %q", reclaimOwnGroundArmPhrase, why)
	}
	// A path git knows but the actor does NOT stand in: no refusal — a rung that
	// refused everything would be a stopped release pass.
	other := filepath.Join(t.TempDir(), "not-my-ground")
	if why := guestRowOwnCheckoutRefusal(other); why != "" {
		t.Fatalf("a path that is not the actor's checkout must not be refused, got %q", why)
	}
	if why := guestRowOwnCheckoutRefusal(""); why != "" {
		t.Fatalf("an empty path must not be refused, got %q", why)
	}
}
