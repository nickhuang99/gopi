package tools

// worktree_fresh_path_test.go — the RED-BEFORE / GREEN-AFTER proof that a
// worktree path must NOT EXIST before creation (worktree_fresh_path.go).
//
// WHAT THE RED HALF IS. The defect is not "the predicate has a bug" — it is
// "nothing asked the question". So the pre-fix behaviour is the predicate's
// ABSENCE at the call site, and the red proof is the call site with the guard
// removed (the recipe recorded in memory #3861: mutate the guard CLAUSE back to
// the pre-fix code path, never a looser variant of a helper). Three call sites
// are red-proved here, one per creator family:
//
//	TestWorktreeFreshPath_EmptyDirectoryIsRefused                 — the predicate
//	TestTaskGroundCreate_RefusesExistingEmptyDirectory            — internal/tools
//	                                                               (task_run_ground.go)
//	TestBashEntryPoint_RefusesWorktreeAddIntoExistingEmptyDir     — the bash tool
//	                                                               (path_policy.go)
//
// The bash-tool half doubles as the Rule 31 execution: it drives the REAL entry
// point (`BashInDir`, the function the bash tool calls) rather than the
// predicate, so the wiring is proven and not just the policy function.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// worktreeFreshFixture returns a directory under the suite's pinned temp root
// (Rule 22/Rule 32: never a hand-rolled path under /tmp and never the operator's
// profile) plus the path a worktree would be created at.
func worktreeFreshFixture(t *testing.T) (dir, target string) {
	t.Helper()
	dir = t.TempDir()
	return dir, filepath.Join(dir, "gopi-fresh-path-wt")
}

// freshPathFixtureRepo builds a throwaway repo with ONE commit on branch, in the
// isolated environment the package's git fixtures use (GOPI_HOME / the ledger /
// HOME all redirected — covLifeGitEnv), so a caller's real profile can never be
// read or written by these tests.
func freshPathFixtureRepo(t *testing.T, branch string) string {
	t.Helper()
	covLifeGitEnv(t)
	repo := covLifeGitRepo(t)
	covLifeGit(t, repo, covLifeGitBinary, "branch", "-M", branch)
	return repo
}

// TestWorktreeFreshPath_AbsentPathIsNil is the NEGATIVE probe that keeps the
// guard honest: a predicate that refused everything would satisfy every refusal
// test while breaking every legitimate worktree.
func TestWorktreeFreshPath_AbsentPathIsNil(t *testing.T) {
	_, target := worktreeFreshFixture(t)
	if err := worktreeFreshPath(target); err != nil {
		t.Fatalf("an ABSENT path must be fresh: %v", err)
	}
}

// TestWorktreeFreshPath_EmptyDirectoryIsRefused is THE case git lets through:
// `git worktree add` refuses a NON-empty target and silently REUSES an EMPTY
// one, so an empty directory is exactly the occupant this predicate exists for.
func TestWorktreeFreshPath_EmptyDirectoryIsRefused(t *testing.T) {
	_, target := worktreeFreshFixture(t)
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("fixture is not the empty-directory case (git would refuse this one itself): %d entries", len(entries))
	}
	err = worktreeFreshPath(target)
	if err == nil {
		t.Fatal("an existing EMPTY directory was accepted — git reuses it silently, so this is the defect")
	}
	if !errors.Is(err, ErrWorktreePathExhausted) {
		t.Errorf("the refusal must carry the named error: %v", err)
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("the refusal must name the path: %v", err)
	}
	if !strings.Contains(err.Error(), "choose another path") {
		t.Errorf("the refusal must name the remedy: %v", err)
	}
}

// TestWorktreeFreshPath_NonEmptyDirectoryIsRefused pins the case git ALSO
// refuses, so the predicate is the single answer for every occupant rather than
// a patch for one of them.
func TestWorktreeFreshPath_NonEmptyDirectoryIsRefused(t *testing.T) {
	_, target := worktreeFreshFixture(t)
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "occupant.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := worktreeFreshPath(target); err == nil {
		t.Fatal("a non-empty directory was accepted")
	}
}

// TestWorktreeFreshPath_RegularFileIsRefused covers the occupant that is not a
// directory at all.
func TestWorktreeFreshPath_RegularFileIsRefused(t *testing.T) {
	_, target := worktreeFreshFixture(t)
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := worktreeFreshPath(target)
	if err == nil {
		t.Fatal("a regular file was accepted")
	}
	if !strings.Contains(err.Error(), "a regular file") {
		t.Errorf("the refusal must say WHAT is in the way: %v", err)
	}
}

// TestWorktreeFreshPath_DanglingSymlinkIsRefused is why the predicate uses
// os.Lstat and not os.Stat: a symlink whose target does not exist reads as
// ABSENT through Stat, and `git worktree add` would then write THROUGH the link
// — the checkout lands wherever the link points, not where the caller believes.
func TestWorktreeFreshPath_DanglingSymlinkIsRefused(t *testing.T) {
	dir, target := worktreeFreshFixture(t)
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), target); err != nil {
		t.Skipf("symlinks unavailable in this fixture: %v", err)
	}
	// The fixture is genuinely the dangling case: Stat must NOT see it, or the
	// test would be proving the ordinary-directory arm instead.
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("fixture link is not dangling — os.Stat resolved it")
	}
	err := worktreeFreshPath(target)
	if err == nil {
		t.Fatal("a DANGLING symlink was accepted; os.Stat cannot see it and git would write through the link")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("the refusal must report the link (not a resolved directory): %v", err)
	}
}

// TestWorktreeFreshPath_EmptyPathIsRefused: an empty target is not "fresh", it
// is unverifiable — and an unverifiable path is never created (the fail-closed
// rule path_policy.go's judgeTarget already applies).
func TestWorktreeFreshPath_EmptyPathIsRefused(t *testing.T) {
	if err := worktreeFreshPath("   "); err == nil {
		t.Fatal("an empty path was accepted")
	}
}

// TestTaskGroundCreate_RefusesExistingEmptyDirectory drives the FIRST producer
// call site (internal/tools/task_run_ground.go). RED BEFORE THE FIX: with the
// guard clause removed, git creates the worktree inside the pre-existing empty
// directory (rc=0, "Updating files: 100%") and taskGroundCreate returns nil —
// the task's ground silently IS a directory it did not create.
func TestTaskGroundCreate_RefusesExistingEmptyDirectory(t *testing.T) {
	repo := freshPathFixtureRepo(t, "master")
	dir := filepath.Join(t.TempDir(), "gopi-task-ground")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	err := taskGroundCreate(repo, dir, "task/fresh-path-probe")
	if err == nil {
		t.Fatalf("taskGroundCreate reused the pre-existing EMPTY directory %s (git lets this through)", dir)
	}
	if !errors.Is(err, ErrWorktreePathExhausted) {
		t.Fatalf("the refusal must be the fresh-path error, not a git failure: %v", err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("the refusal must name the path: %v", err)
	}
	// The guard REFUSES, it never removes: the pre-existing entry is still there
	// (a human decides what it was — Rule 25's shape, applied to cleanup).
	if _, serr := os.Lstat(dir); serr != nil {
		t.Fatalf("the pre-existing directory was removed by the guard: %v", serr)
	}
	// And nothing was checked out into it.
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(entries) != 0 {
		t.Fatalf("the refused creation still wrote %d entries into %s", len(entries), dir)
	}
}

// TestBashEntryPoint_RefusesWorktreeAddIntoExistingEmptyDir is the bash-tool
// call site AND the Rule 31 execution: BashInDir is the function the bash tool
// invokes, so a green here means the guard reached the real entry point and not
// only the policy helper.
func TestBashEntryPoint_RefusesWorktreeAddIntoExistingEmptyDir(t *testing.T) {
	repo := freshPathFixtureRepo(t, "master")
	// The target must be inside a DECLARED root, or the pre-existing policy
	// (path_policy.go's judgeTarget) would refuse it for the other reason and
	// this test would prove nothing about the fresh-path predicate.
	root, _, _ := guardPolicyFixture(t)
	target := filepath.Join(root, "gopi-collide-probe")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := BashInDir("git -C "+repo+" worktree add "+target+" -b feat/collide-probe", "")
	if err == nil {
		t.Fatalf("the bash tool ran `git worktree add %s` into a pre-existing EMPTY directory (out=%q)", target, out)
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("the refusal must name the path: %v", err)
	}
	if !strings.Contains(err.Error(), worktreeFreshPathExhaustedMarker) {
		t.Errorf("the refusal must carry the named marker %q: %v", worktreeFreshPathExhaustedMarker, err)
	}
	entries, rerr := os.ReadDir(target)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(entries) != 0 {
		t.Fatalf("the refused command still checked out into %s (%d entries)", target, len(entries))
	}
}

// TestBashEntryPoint_AllowsWorktreeAddOnAFreshPath is the green half of the
// same entry point, and the anti-blanket-ban probe: a legitimate creation at a
// path that does not exist must go through, or the guard is the failure mode
// that gets it switched off.
func TestBashEntryPoint_AllowsWorktreeAddOnAFreshPath(t *testing.T) {
	repo := freshPathFixtureRepo(t, "master")
	root, _, _ := guardPolicyFixture(t)
	target := filepath.Join(root, "gopi-fresh-allowed")
	if out, err := BashInDir("git -C "+repo+" worktree add "+target+" -b feat/fresh-allowed", ""); err != nil {
		t.Fatalf("a legitimate worktree on a FRESH path was refused: %v (%s)", err, out)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("the worktree was not created at %s: %v", target, err)
	}
}
