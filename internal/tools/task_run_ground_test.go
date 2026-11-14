package tools

// task_run_ground_test.go — the WORKER-CREATED CHILD GROUND (epic
// worker-creates-child-ground, 2026-09-22).
//
// WHAT IS BEING PINNED, and why each assertion is the smallest sample that can
// separate the two shapes:
//
//	① a repo-aware row whose ground does not exist yet gets ONE, CREATED
//	   before anything runs — a linked worktree of the row's repo, on the row's
//	   branch, under that repo's declared worktree_root. Before the fix the
//	   resolution had nothing to return and the spawn fell back to the main
//	   checkout (the epic's defect), so this test is RED on the pre-fix code.
//	② an unresolvable ground is a LOUD REFUSAL — never a fallback to the main
//	   checkout. The refusal has to name the row, the repo and the reason.
//	③ the ground is STAMPED back onto the row (deliverable.worktree /
//	   deliverable.branch populated for the run that is happening), through the
//	   existing single blob writer.
//	④ a re-resolution of the SAME row REUSES the ground rather than minting a
//	   sibling — the deterministic naming requirement, and the property that
//	   makes a resumed run land where its predecessor worked.
//	⑤ the main-checkout ASSERTION (the spawn seam's item 4) fires for a row
//	   whose directory is the repo's main checkout, and stays quiet for one
//	   that is not.
//
// The fixture is a REAL repo (git init + commit) with the worktree root pinned
// through the SANDBOX ENVIRONMENT override, which is the repo-declared root's
// own precedence layer (worktreeroots.Resolve: env > settings > default) — no
// new knob, no settings file needed.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// groundTestRepo builds a REAL git repo with one commit and returns its path.
func groundTestRepo(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", repo, err)
	}
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"checkout", "-b", "master"},
	} {
		if out, err := groundTestGit(repo, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := groundTestGit(repo, "add", "-A"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := groundTestGit(repo, "commit", "-m", "base"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	return repo
}

// groundTestGit runs one git command in dir, failing the test on error.
func groundTestGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// groundTestGitT is groundTestGit with the test-fatal contract (a fixture reads
// better as a statement than as five error checks).
//
// MOVED HERE by task 01a18799 (Rule 43): it used to live in
// task_branch_refs_test.go, the file the retired `gopi task branch-refs` owned,
// while its readers are ground-fixture tests that have nothing to do with that
// verb (task_ground_shape_clause_test.go). It is the SHARED helper, so it moved
// in the same commit that deleted the verb — a symbol deleted out from under a
// live reader would have turned the package red.
func groundTestGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := groundTestGit(dir, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return out
}

// groundFixture pins the ledger and the worktree root for one test and submits
// a repo-aware row with no ground, returning the row id, the repo and the root
// the ground must land under.
func groundFixture(t *testing.T, taskID, repoName string) (id, repo, root string) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	root = t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	repo = groundTestRepo(t, repoName)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "ground fixture " + taskID, Payload: "do the work", Repo: repo, TaskID: taskID,
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	return taskID, repo, root
}

// TestTaskRunGroundCreatesTheWorktreeBeforeTheRun is ACCEPTANCE ①: a row with
// deliverable.repo and no existing worktree resolves to a REAL linked worktree
// of that repo — created by the resolution itself, on the row's branch.
//
// The three readings are deliberately independent: the directory EXISTS, git
// REGISTERS it as a linked worktree of the repo (not merely a directory that
// looks like one), and it is NOT the main checkout (the defect's shape).
func TestTaskRunGroundCreatesTheWorktreeBeforeTheRun(t *testing.T) {
	id, repo, root := groundFixture(t, "01a11500-0000-7000-8000-000000000001", "ground-repo")

	got, err := TaskRunGroundFor(id)
	if err != nil {
		t.Fatalf("resolving a repo-aware row's ground must CREATE it, got error: %v", err)
	}
	if !got.Created {
		t.Fatalf("the ground did not exist, so the resolution must report it CREATED; got %+v", got)
	}
	if got.Dir == "" {
		t.Fatalf("the resolution returned no directory — the child would fall back to the main checkout, which is exactly the epic's defect: %+v", got)
	}
	if !strings.HasPrefix(got.Dir, root+string(filepath.Separator)) {
		t.Errorf("the ground must live under the repo's worktree root %s; got %s", root, got.Dir)
	}
	if fi, err := os.Stat(got.Dir); err != nil || !fi.IsDir() {
		t.Fatalf("the ground %s must exist as a directory: %v", got.Dir, err)
	}
	// A LINKED worktree, proven by git — its own .git is a FILE pointing at the
	// shared object store (a main checkout's .git is a directory).
	dotGit, err := os.Stat(filepath.Join(got.Dir, ".git"))
	if err != nil {
		t.Fatalf("stat %s/.git: %v", got.Dir, err)
	}
	if dotGit.IsDir() {
		t.Fatalf("%s looks like a MAIN checkout (.git is a directory) — the ground must be a linked worktree", got.Dir)
	}
	if !groundRegistered(t, repo, got.Dir) {
		t.Fatalf("git must register %s as a worktree of %s; listing:\n%s", got.Dir, repo, groundWorktreeList(t, repo))
	}
	if SameDir(got.Dir, repo) {
		t.Fatalf("the ground resolved to the repo's MAIN checkout %s — the epic's defect verbatim", repo)
	}
	// The branch: named, non-exempt (a ground on master would leave the row
	// unattributable at completion), and the one the worktree carries.
	if got.Branch == "" {
		t.Fatalf("the ground must name the branch it was checked out on; got %+v", got)
	}
	if taskBranchExempt(got.Branch) {
		t.Fatalf("the ground's branch %q is EXEMPT from attribution — a ground on master/main/archive-* is not a deliverable", got.Branch)
	}
	if branch := groundHeadBranch(t, got.Dir); branch != got.Branch {
		t.Fatalf("the ground %s carries branch %q, not the resolved %q", got.Dir, branch, got.Branch)
	}
}

// TestTaskRunGroundRefusesWhenTheGroundCannotBeMade is ACCEPTANCE ②: a
// repo-aware row whose repository cannot answer is REFUSED, loudly, with a text
// naming the row and the repo — and the resolution returns NO directory, so the
// caller cannot silently fall back to the main checkout.
func TestTaskRunGroundRefusesWhenTheGroundCannotBeMade(t *testing.T) {
	id, repo, _ := groundFixture(t, "01a11500-0000-7000-8000-000000000002", "refuse-repo")
	// The repo was real at submit time (submit verifies it) and stops being
	// resolvable afterwards: its git directory is gone. That is the shape the
	// ground cannot be created from, and the row still names the repo.
	if err := os.RemoveAll(filepath.Join(repo, ".git")); err != nil {
		t.Fatalf("remove .git: %v", err)
	}

	got, err := TaskRunGroundFor(id)
	if err == nil {
		t.Fatalf("an unresolvable repo must REFUSE the run; got dir=%q created=%v", got.Dir, got.Created)
	}
	if got.Dir != "" {
		t.Fatalf("a refusal must carry NO directory (a directory here is the silent fallback the epic removes); got %q", got.Dir)
	}
	refusal := TaskRunGroundRefusal(id, err)
	for _, want := range []string{id, repo, "refusing to start"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal must name %q so an operator can act on it; got:\n%s", want, refusal)
		}
	}
}

// TestTaskRunGroundStampsTheRowDuringTheRun is ACCEPTANCE ③: the resolved
// ground is written onto the row — deliverable.worktree AND deliverable.branch
// — through the existing single blob writer, so an in-flight row names where it
// works instead of waiting for the completion-time backfill.
func TestTaskRunGroundStampsTheRowDuringTheRun(t *testing.T) {
	id, repo, _ := groundFixture(t, "01a11500-0000-7000-8000-000000000003", "stamp-repo")

	got, err := TaskRunGroundFor(id)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	worktree, branch := TaskDeliverableWorktreeFor(id)
	if worktree == "" {
		t.Fatalf("the row must name the ground it is executing in; deliverable.worktree is empty (resolved %s)", got.Dir)
	}
	if !SameDir(worktree, got.Dir) {
		t.Fatalf("deliverable.worktree must be the ground the child starts in (%s); got %s", got.Dir, worktree)
	}
	if branch == "" {
		t.Fatalf("deliverable.branch must be populated for the run that is happening, not only by the completion backfill")
	}
	if branch != got.Branch {
		t.Fatalf("deliverable.branch must be the ground's branch %q; got %q", got.Branch, branch)
	}
	if repoFor := TaskDeliverableRepoFor(id); !SameDir(repoFor, repo) {
		t.Fatalf("deliverable.repo must stay the submit-time repo %s; got %s", repo, repoFor)
	}
}

// TestTaskRunGroundReusesAnExistingGround is ACCEPTANCE ④'s reuse half: the
// SAME row resolved twice lands on the SAME directory and branch — a second
// attempt (or a resumed run) returns to the ground its predecessor made rather
// than minting a sibling worktree.
func TestTaskRunGroundReusesAnExistingGround(t *testing.T) {
	id, _, _ := groundFixture(t, "01a11500-0000-7000-8000-000000000004", "reuse-repo")

	first, err := TaskRunGroundFor(id)
	if err != nil {
		t.Fatalf("first resolution: %v", err)
	}
	if !first.Created {
		t.Fatalf("the first resolution must create the ground; got %+v", first)
	}
	second, err := TaskRunGroundFor(id)
	if err != nil {
		t.Fatalf("second resolution: %v", err)
	}
	if second.Created {
		t.Fatalf("the second resolution must REUSE the ground, not create another; got %+v", second)
	}
	if second.Dir != first.Dir {
		t.Fatalf("the ground must be DETERMINISTIC for a row (%s), got a second directory %s — a sibling worktree would be a leak", first.Dir, second.Dir)
	}
	if second.Branch != first.Branch {
		t.Fatalf("the ground's branch must be stable (%s), got %s", first.Branch, second.Branch)
	}
}

// TestTaskRunGroundChildInMainCheckoutAsserts is ACCEPTANCE ④'s ASSERTION half
// (the spawn seam's item 4): a repo-aware resolution reports a refusal when the
// directory about to be used IS the main checkout or lies inside it, and stays
// quiet for a directory that is the row's own ground.
//
// This is the guard that turns "this directory is legal" into "this directory
// is wrong FOR THIS ROW" — the whole reason the defect was invisible.
func TestTaskRunGroundChildInMainCheckoutAsserts(t *testing.T) {
	id, repo, _ := groundFixture(t, "01a11500-0000-7000-8000-000000000005", "assert-repo")
	got, err := TaskRunGroundFor(id)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.MainCheckout == "" {
		t.Fatalf("the resolution must report the repo's main checkout so the seam can assert on it; got %+v", got)
	}
	if !SameDir(got.MainCheckout, repo) {
		t.Fatalf("the main checkout must be the repo itself for a non-bare repository (%s); got %s", repo, got.MainCheckout)
	}
	// The defect shape: the main checkout itself.
	if refusal, bad := got.ChildInMainCheckout(repo); !bad {
		t.Fatalf("spawning in the repo's main checkout %s must be refused for a repo-aware row; got no refusal", repo)
	} else if !strings.Contains(refusal, "MAIN checkout") {
		t.Errorf("the refusal must say what it refused (the MAIN checkout), got %q", refusal)
	}
	// A directory INSIDE the main checkout is the same defect (the child would
	// still be standing in the shared tree).
	inside := filepath.Join(repo, "cmd", "agent")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, bad := got.ChildInMainCheckout(inside); !bad {
		t.Fatalf("a directory inside the main checkout (%s) is the same defect and must be refused", inside)
	}
	// The row's own ground is fine, and so is a repo-less row (nothing to
	// assert — that is the documented non-repo shape).
	if refusal, bad := got.ChildInMainCheckout(got.Dir); bad {
		t.Fatalf("the row's own ground %s must NOT be refused: %s", got.Dir, refusal)
	}
	var none TaskGroundResolution
	if _, bad := none.ChildInMainCheckout(repo); bad {
		t.Fatalf("a repo-less resolution has nothing to assert and must stay quiet")
	}
}

// groundRegistered reports whether git registers dir as a worktree of repo.
func groundRegistered(t *testing.T, repo, dir string) bool {
	t.Helper()
	want, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range groundListEntries(t, repo) {
		if got, aerr := filepath.Abs(e); aerr == nil && got == want {
			return true
		}
	}
	return false
}

// groundListEntries returns the worktree paths git registers for repo.
func groundListEntries(t *testing.T, repo string) []string {
	t.Helper()
	out := groundWorktreeList(t, repo)
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok {
			paths = append(paths, p)
		}
	}
	return paths
}

// groundWorktreeList renders `git worktree list` for repo (the raw evidence).
func groundWorktreeList(t *testing.T, repo string) string {
	t.Helper()
	out, err := groundTestGit(repo, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatalf("git worktree list in %s: %v\n%s", repo, err, out)
	}
	return out
}

// groundHeadBranch returns the branch checked out at dir.
func groundHeadBranch(t *testing.T, dir string) string {
	t.Helper()
	out, err := groundTestGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD in %s: %v\n%s", dir, err, out)
	}
	return strings.TrimSpace(out)
}
