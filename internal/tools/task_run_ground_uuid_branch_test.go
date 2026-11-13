package tools

// task_run_ground_uuid_branch_test.go — THE BRANCH NAME IS THE TASK UUID
// (epic: "why cannot we use task UUID as branch name?", 2026-10-07).
//
// THE OPERATOR'S RULE, verbatim: "in most cases except resume, the name of
// branch can use task uuid, so does the worktree, unless there exists any
// conflict, or special case of resume, gopi should take first choice to use
// this principle, however, if conflict, i.e. name exists, then fallback to
// create new uuid."
//
// WHAT THESE TESTS SEPARATE (each is the smallest sample that can tell the two
// shapes apart):
//
//	① a RECORDED descriptive branch (fix/…, feat/…) is NOT inherited when a
//	   ground is minted — inheriting it is how a run's ref drifted away from
//	   its row, so no ledger lookup could read the row off the branch. On the
//	   pre-change code the recorded name came back verbatim, so this test is
//	   RED there.
//	② a name that is TAKEN is never ATTACHED to: the resolution refuses loudly
//	   (the pre-existing ground-path refusal) instead of putting the run on
//	   somebody else's deliverable ref.
//	③ a name held by a DIFFERENT ROW in the ledger falls back to a FRESH
//	   uuid-named branch, and the other row's ref is left untouched — the
//	   "fallback to create new uuid" half of the rule.
//
// The RESUME exception is structural, not textual: a resumed run returns at
// TaskRunGroundFor step 1 (the recorded ground wins, branch and all) and never
// reaches the minting path these tests drive.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/worktreeroots"
)

// TestTaskGroundBranchForKeepsTheUuidAndDropsADescriptiveName is ①: the minting
// path's branch is DERIVED from the row, whatever descriptive name the row
// happens to record — while the row's own derived name (a re-claim) and an
// exempt integration branch keep their existing treatment.
func TestTaskGroundBranchForKeepsTheUuidAndDropsADescriptiveName(t *testing.T) {
	const id = "01a11554-b3b1-731e-8425-2ce4986786f9"
	derived := TaskGroundBranchPrefix + taskGroundIDStem(id)
	if derived != "task/01a11554b3b1731e84252ce4986786f9" {
		t.Fatalf("the derived branch must BE the row's uuid under %q; got %q", TaskGroundBranchPrefix, derived)
	}
	// repo "" = the ownership probe is not asked (no repo to scan) — these cases
	// are about WHICH name the row's own ref gets, not about collisions.
	cases := []struct {
		name     string
		recorded string
	}{
		{"no recorded branch", ""},
		{"a descriptive fix/ name", "fix/some-branch-name"},
		{"a descriptive feat/ name", "feat/some-branch-name"},
		{"the rename the old Rule 6a blessed", "task/crelease"},
		{"an exempt integration branch", "master"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := taskGroundBranchFor(id, tc.recorded, "")
			if got != derived {
				t.Fatalf("a minted ground's branch must be the row's uuid (%s); recorded %q produced %q — a descriptive or foreign recorded name must never be inherited", derived, tc.recorded, got)
			}
		})
	}
	// The row's OWN derived name is not "a recorded branch" to be overridden —
	// a second claim of the same row resolves the same ref.
	if got := taskGroundBranchFor(id, derived, ""); got != derived {
		t.Fatalf("the row's own derived branch must be kept verbatim; got %q", got)
	}
}

// TestTaskRunGroundNeverAttachesToATakenBranchName is ②: when the row's derived
// ref already exists and NO ledger row owns it (an orphan ref — a failed
// attempt, an archived name, a ref somebody else minted), the resolution mints
// a FRESH uuid-named branch rather than checking the run out on that ref.
// Attaching would put the run on a base nobody can vouch for, and
// finish-cleanup's push-back would then fast-forward that ref with this run's
// commits.
func TestTaskRunGroundNeverAttachesToATakenBranchName(t *testing.T) {
	id, repo, _ := groundFixture(t, "7f6a8320-589b-4aeb-a1d6-ca7152914172", "uuid-branch-orphan-repo")
	derived := TaskGroundBranchPrefix + taskGroundIDStem(id)
	if out, err := groundTestGit(repo, "branch", derived); err != nil {
		t.Fatalf("pre-creating the orphan ref %s: %v\n%s", derived, err, out)
	}
	orphanTip, err := groundTestGit(repo, "rev-parse", "refs/heads/"+derived)
	if err != nil {
		t.Fatalf("rev-parse the orphan ref: %v", err)
	}

	got, err := TaskRunGroundFor(id)
	if err != nil {
		t.Fatalf("a taken name is the FALLBACK case, not a refusal: %v", err)
	}
	if got.Branch == derived {
		t.Fatalf("the run was checked out on the pre-existing ref %s — a name that exists and that no row owns must fall back to a fresh uuid", derived)
	}
	if !strings.HasPrefix(got.Branch, TaskGroundBranchPrefix) ||
		strings.TrimPrefix(got.Branch, TaskGroundBranchPrefix) == taskGroundIDStem(id) {
		t.Fatalf("the fallback must be a FRESH name in the ground namespace; got %q", got.Branch)
	}
	if branch := groundHeadBranch(t, got.Dir); branch != got.Branch {
		t.Fatalf("the ground %s carries branch %q, not the resolved fallback %q", got.Dir, branch, got.Branch)
	}
	// The pre-existing ref is evidence: not deleted, not moved, not checked out.
	if tip, err := groundTestGit(repo, "rev-parse", "refs/heads/"+derived); err != nil || tip != orphanTip {
		t.Fatalf("the pre-existing ref %s must be left exactly where it was (was %q, now %q, err %v)", derived, orphanTip, tip, err)
	}
	if groundHeadBranch(t, repo) == derived {
		t.Fatalf("the repo's main checkout must not have been moved onto the ref %s", derived)
	}
}

// TestTaskRunGroundFallsBackToAFreshUuidWhenAnotherRowHoldsTheName is ③ — the
// operator's fallback clause: the name exists AND is another row's, so the run
// mints a fresh uuid-named branch, works on the deterministic ground path, and
// leaves the other row's ref exactly where it was.
func TestTaskRunGroundFallsBackToAFreshUuidWhenAnotherRowHoldsTheName(t *testing.T) {
	const other = "fe733387-875d-4862-8276-f8c74d5dc888"
	const mine = "7253151e-3397-46af-9321-eaac4b3bd875"
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	root := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	repo := groundTestRepo(t, "uuid-branch-clash-repo")
	for _, id := range []string{other, mine} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: "uuid branch clash " + id, Payload: "do the work", Repo: repo, TaskID: id,
		}); err != nil {
			t.Fatalf("TaskSubmit %s: %v", id, err)
		}
	}
	// The other row's deliverable claims the ref this row's uuid would mint.
	clash := TaskGroundBranchPrefix + taskGroundIDStem(mine)
	if out, err := groundTestGit(repo, "branch", clash); err != nil {
		t.Fatalf("pre-creating %s: %v\n%s", clash, err, out)
	}
	uuidBranchStampDeliverable(t, other, `{"repo":"`+repo+`","branch":"`+clash+`"}`)

	got, err := TaskRunGroundFor(mine)
	if err != nil {
		t.Fatalf("a name held by another row must FALL BACK to a fresh uuid, not refuse: %v", err)
	}
	if got.Branch == clash {
		t.Fatalf("the run was put on %s — another row's deliverable ref; the collision must mint a new name", clash)
	}
	if !strings.HasPrefix(got.Branch, TaskGroundBranchPrefix) {
		t.Errorf("the fallback branch must live in the ground namespace %q; got %q", TaskGroundBranchPrefix, got.Branch)
	}
	if strings.TrimPrefix(got.Branch, TaskGroundBranchPrefix) == taskGroundIDStem(mine) {
		t.Errorf("the fallback must be a FRESH name, not the collided one; got %q", got.Branch)
	}
	if branch := groundHeadBranch(t, got.Dir); branch != got.Branch {
		t.Fatalf("the ground %s carries branch %q, not the resolved fallback %q", got.Dir, branch, got.Branch)
	}
	// The other row's ref is evidence, never a write target: unchanged, and
	// still owned by the row that named it.
	if out, err := groundTestGit(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+clash); err != nil {
		t.Fatalf("the other row's ref %s was destroyed by the fallback: %v\n%s", clash, err, out)
	}
	if owner, err := taskBranchRowOwner(mine, repo, clash); err != nil || owner != other {
		t.Fatalf("the collided ref must still be owned by %s (owner=%q, err=%v)", other, owner, err)
	}
	// The fresh name is registered in git, on a real ground directory.
	if _, err := groundTestGit(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+got.Branch); err != nil {
		t.Fatalf("the fallback branch %s must exist in %s: %v", got.Branch, repo, err)
	}
	if fi, err := os.Stat(filepath.Join(got.Dir, ".git")); err != nil || fi.IsDir() {
		t.Fatalf("the ground %s must be a LINKED worktree: %v", got.Dir, err)
	}
}

// uuidBranchStampDeliverable writes a deliverable blob onto a fixture row
// through the ONE ledger the test pinned (Rule 32 — the test owns the pin).
func uuidBranchStampDeliverable(t *testing.T, taskID, blob string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore test-scoped handle closed when the fixture row is stamped
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, taskID); err != nil {
		t.Fatalf("stamp deliverable on %s: %v", taskID, err)
	}
}
