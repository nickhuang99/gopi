package tools

// worktree_sweep_ground_checklist_test.go — the SWEEP's route through the
// deterministic checklist (reclaim_ground_checklist.go).
//
// WHY THIS FILE EXISTS (measured, Rule 31, on the REAL verb): driving
// `gopi task sweep-worktrees --apply` over a two-registration fixture — one on a
// normal-lineage branch, one on an ORPHAN-root branch sharing no ancestor with
// master — printed the orphan's refusal TWICE and stamped it with the JUDGE's
// own rule (`unclaimed-reclaim`), so the plan read as "the judge changed its
// mind", not "a POST-JUDGE arm refused this". Both halves are regressions this
// file pins:
//
//	① the refusal appears EXACTLY ONCE in the findings;
//	② the finding's Rule names the arm that actually refused it
//	  (worktreeRuleGroundChecklist), which is also what keeps the ladder's own
//	  arm-citation gate (assertEveryJudgeArmIsCited) honest.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sweepGroundChecklistRepo builds a repo with TWO linked registrations: one on a
// normal-lineage branch, one on an orphan root. Returns the repo and the two
// worktree paths.
func sweepGroundChecklistRepo(t *testing.T) (repo, normalWT, orphanWT string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := sweepFixtureRoot(t)
	repo = sweepInitRepo(t)
	normalWT = filepath.Join(root, "wt-normal")
	orphanWT = filepath.Join(root, "wt-orphan")

	sweepGit(t, repo, "checkout", "-q", "-b", "feat/normal-lineage")
	if err := os.WriteFile(filepath.Join(repo, "w.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, repo, "add", "w.txt")
	sweepGit(t, repo, "commit", "-q", "-m", "work")
	sweepGit(t, repo, "checkout", "-q", masterBranchName)
	sweepGit(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeAddVerb, normalWT, "feat/normal-lineage")

	sweepGit(t, repo, "checkout", "-q", "--orphan", reclaimOrphanBranch)
	if err := os.WriteFile(filepath.Join(repo, "o.txt"), []byte("orphan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, repo, "add", "o.txt")
	sweepGit(t, repo, "commit", "-q", "-m", "an orphan root")
	sweepGit(t, repo, "checkout", "-q", masterBranchName)
	sweepGit(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeAddVerb, orphanWT, reclaimOrphanBranch)

	// The precondition IS the case (Rule 10: verify the smallest sample).
	if _, err := exec.Command("git", "-C", repo, reclaimMergeBaseVerb, masterBranchName, reclaimOrphanBranch).Output(); err == nil {
		t.Fatalf("fixture: %s shares an ancestor with %s — the case is not set up", reclaimOrphanBranch, masterBranchName)
	}
	auditAgeRegistration(t, normalWT)
	auditAgeRegistration(t, orphanWT)
	return repo, normalWT, orphanWT
}

// TestSweepRefusesAnUnrelatedHistoryBranchFromTheRealVerb is ① — the sweep's
// reclaim arm must not free the ground of an unrelated-history branch, and it
// must say so ONCE, naming the checklist as the arm that refused.
func TestSweepRefusesAnUnrelatedHistoryBranchFromTheRealVerb(t *testing.T) {
	repo, normalWT, orphanWT := sweepGroundChecklistRepo(t)
	opts := sweepOptionsNow(false)
	opts.MinAge = time.Hour

	res := worktreeSweepRunWith(context.Background(), opts,
		worktreeSweepDeps{Repos: []string{repo}, LedgerRead: true}, nil)

	// ① the NORMAL-lineage registration is still reclaimed — a rung that
	// refused everything would be a stopped release pass, not a guard.
	if _, err := os.Stat(normalWT); !os.IsNotExist(err) {
		t.Fatalf("the normal-lineage registration must still be reclaimed (%v): %+v", err, res.Findings)
	}
	// ② the ORPHAN's ground survives.
	if _, err := os.Stat(orphanWT); err != nil {
		t.Fatalf("the unrelated-history branch's ground must survive: %v", err)
	}
	// ③ EXACTLY ONE finding names the orphan — printed twice was the measured
	// defect (the switch's own append plus the trailing one).
	named := 0
	var last worktreeSweepFinding
	for _, f := range res.Findings {
		if f.Path != orphanWT {
			continue
		}
		named++
		last = f
	}
	if named != 1 {
		t.Fatalf("the orphan registration is named %d times in the plan, want exactly 1 (the real verb printed it twice): %+v", named, res.Findings)
	}
	// ④ the refusal carries the checklist's own sentence AND its own rule — the
	// rule is what tells the plan which arm refused (the judge said reclaim).
	if !strings.Contains(last.Reason, reclaimLineageArmPhrase) {
		t.Fatalf("the refusal must NAME the failed check (%q), got %q", reclaimLineageArmPhrase, last.Reason)
	}
	if last.Rule != string(worktreeRuleGroundChecklist) {
		t.Fatalf("the finding's rule = %q, want %q — a post-judge refusal must not be stamped with the judge's arm",
			last.Rule, worktreeRuleGroundChecklist)
	}
	// ⑤ the branch is untouched: no arm deletes a ref it refused to free.
	if _, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", reclaimOrphanBranch).Output(); err != nil {
		t.Fatalf("the branch must survive the refusal: %v", err)
	}
}

// TestSweepGroundChecklistStampsItsOwnRule cites the post-judge arm in the
// ladder's own arm-citation gate's terms (assertEveryJudgeArmIsCited counts the
// arm constants and requires the extra ones to be documented as post-judge).
// Kept beside the behaviour test so the two cannot drift.
func TestSweepGroundChecklistStampsItsOwnRule(t *testing.T) {
	if string(worktreeRuleGroundChecklist) != "ground-checklist" {
		t.Fatalf("the arm's spelling changed to %q — the plan's rule column and the rung's name are pinned in ONE place",
			worktreeRuleGroundChecklist)
	}
	// The rung itself answers "" for a registration with nothing to judge
	// (no repo/branch/worktree) — it must never INVENT a refusal there.
	if why := reclaimGroundChecklistVerdict("", "", ""); why != "" {
		t.Fatalf("an empty triple must not be refused, got %q", why)
	}
}
