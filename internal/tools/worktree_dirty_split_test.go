package tools

// Regression tests for the DIRTY-STATE SPLIT (arm 1 of the worktree-refusal
// epic, 2026-09-16) and for the verify child's ground (arm 3).
//
// The defect: guard ② rendered EVERY non-empty `git status --porcelain` body
// as reclaimSkipDirtyReason ("uncommitted work"), so a tree whose only entries
// were `??` — the run's own reports, build output, a scratch checkout — read
// as somebody's uncommitted work, with the file list thrown away. The operator
// could not tell the two apart, and the run whose own residue it was had no
// statement of what to clean.
//
// What must NOT change (asserted below): a tracked change still refuses in the
// old words, nothing is ever removed by either verdict, and an untracked file
// in a DEAD HOLDER's tree is still work the salvage lane must carry (its only
// copy is that directory).
//
// The recipe (pixel-of-truth style minimal sample, Rule 10.4/10.5): classify
// the SMALLEST porcelain body of each shape, then confirm the split end-to-end
// through the real sweep on a real worktree.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReclaimDirtyVerdictClassifiesUntrackedOnlyResidue(t *testing.T) {
	cases := []struct {
		name      string
		porcelain string
		wantDirty bool // refused at all
		wantKind  string
	}{
		{"clean", "", false, ""},
		{"clean sentinel", finishCleanupNoOutput, false, ""},
		{"clean with newline", "\n", false, ""},
		{"untracked only", "?? scratch.txt", true, reclaimUntrackedResidueReason},
		{"untracked only several", "?? a.txt\n?? b/c.txt\n?? dir/", true, reclaimUntrackedResidueReason},
		{"modified tracked", " M a.txt", true, reclaimSkipDirtyReason},
		{"staged tracked", "A  new.go", true, reclaimSkipDirtyReason},
		{"deleted tracked", " D old.go", true, reclaimSkipDirtyReason},
		{"untracked plus tracked", "?? res.txt\n M a.go", true, reclaimSkipDirtyReason},
		{"rename", "R  a.go -> b.go", true, reclaimSkipDirtyReason},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reclaimDirtyVerdict(tc.porcelain)
			if !tc.wantDirty {
				if got != reclaimStatusClean {
					t.Fatalf("a %s tree must classify clean, got %q", tc.name, got)
				}
				return
			}
			if got == reclaimStatusClean {
				t.Fatalf("a dirty tree must be refused, got the clean verdict")
			}
			if !strings.Contains(got, tc.wantKind) {
				t.Fatalf("wrong kind:\n got %q\nwant it to contain %q", got, tc.wantKind)
			}
			// The operator contract every pre-existing arm keys on: the word
			// DIRTY must survive in BOTH wordings.
			if !strings.Contains(got, "DIRTY") {
				t.Errorf("both refusal wordings must keep the operator word DIRTY, got %q", got)
			}
			// Rule 15: the refusal NAMES the files that caused it.
			body := strings.TrimSpace(tc.porcelain)
			if body != "" && !strings.Contains(got, strings.Split(body, "\n")[0]) {
				t.Errorf("the refusal must quote the offending entries back, got %q", got)
			}
		})
	}
	// The two wordings are distinct — collapsing them is the defect.
	if reclaimSkipDirtyReason == reclaimUntrackedResidueReason {
		t.Fatal("the tracked and untracked-only wordings must differ")
	}
}

// TestReclaimDirtyHeldCoversBothShapes pins the salvage lane's predicate: the
// wording split must not drop the untracked half out of the population the
// salvage carries (TestZZRedProof_DirtyDeadHolderWorkIsOrphaned is the
// end-to-end half of this).
func TestReclaimDirtyHeldCoversBothShapes(t *testing.T) {
	if !reclaimDirtyHeld(reclaimSkipDirtyReason) {
		t.Error("the tracked-work refusal must be in the salvage scope")
	}
	if !reclaimDirtyHeld(reclaimUntrackedResidueReason) {
		t.Error("the untracked-only refusal must be in the salvage scope — its files are still only-copy work")
	}
	if reclaimDirtyHeld("worktree in use by live process pid 1") {
		t.Error("a non-dirty refusal must NOT be salvaged behind a dirty label")
	}
}

// TestReclaimUntrackedOnlyWorktreeStillRefused is the end-to-end half: a real
// worktree whose ONLY dirt is untracked must still be refused (nothing is
// removed) while the verdict names the untracked kind. This is the live shape
// the operator hit: a finished run's own report/build residue keeping its
// worktree forever.
func TestReclaimUntrackedOnlyWorktreeStillRefused(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-untracked", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-untracked", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))

	// Untracked residue ONLY — a report the run wrote, nothing tracked.
	if err := os.WriteFile(filepath.Join(wt, "01a0a4ae-verify-report.md"), []byte("report"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview := MergerSweepPreview(repo)
	why, held := preview.Held[branch]
	if !held {
		t.Fatalf("an untracked-only dead-holder worktree must still be refused (nothing is removed), got held=%v", preview.Held)
	}
	if !strings.Contains(why, reclaimUntrackedResidueReason) {
		t.Fatalf("the verdict must name the UNTRACKED kind, got %q", why)
	}
	if !strings.Contains(why, "01a0a4ae-verify-report.md") {
		t.Errorf("the verdict must name the untracked file that holds the tree, got %q", why)
	}
	// The operator line must not promise a release for a tree the arm just
	// refused (DeadHolder names the DETECTED candidates; the promise is the
	// thing that must not appear).
	if line := preview.DescribeProtected(); strings.Contains(line, "reclaimable by the finish-cleanup sweep") {
		t.Fatalf("FALSE PROMISE: an untracked-only tree is refused, yet the operator line promises a release: %q", line)
	}
	// The directory is untouched — refusals never remove.
	if _, err := os.Stat(filepath.Join(wt, "01a0a4ae-verify-report.md")); err != nil {
		t.Fatalf("the refusal must leave the directory alone: %v", err)
	}
}

// initVerifyGroundRepo builds the smallest real repo the ground tests need: one
// commit on master, so `git worktree add --detach <dir> master` has a tip.
func initVerifyGroundRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", IntegrationBranchName},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "tracked.txt"}, {"commit", "-qm", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return repo
}
