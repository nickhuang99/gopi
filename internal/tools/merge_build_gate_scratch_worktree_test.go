package tools

// The regression guard for the worktree-registration leak: a scratch checkout
// created for ONE measurement must leave NOTHING behind in the repository's
// registry, including the LOCKED shape an interrupted `worktree add` produces.
//
// The measured defect this file pins (git 2.43.0, this host, 2026-10-25): the
// test helper checked a revision out into `filepath.Join(t.TempDir(), "wt")`,
// `testing` removed the DIRECTORY, and the git REGISTRATION — which lives in
// `<repo>/.git/worktrees/<id>/`, never in the directory — survived the test.
// `git worktree list` then reported a path that does not exist, and when the
// add had been interrupted the entry carried `locked: initializing`, which
// `git worktree prune` SKIPS forever (measured: exit 0, nothing printed, the
// entry still there) and `git worktree remove --force` REFUSES.
//
// Both arms below fail on the pre-fix code and pass on the fix, which is why
// they are the RED PROOF: the first arm is the ordinary end-of-test state, the
// second is the interrupted one, and the second is the shape that outlives
// every sweep.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scratchWTRepo builds a tiny repository with one commit — the smallest fixture
// a `worktree add --detach` can succeed in.
func scratchWTRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	mgGit(t, repo, "init", "-q", ".")
	mgGit(t, repo, "config", "user.email", "fixture@example.invalid")
	mgGit(t, repo, "config", "user.name", "fixture")
	mgWrite(t, repo, "f.txt", "x\n")
	mgGit(t, repo, "add", "f.txt")
	mgGit(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// scratchWTAdd returns the creator func the helper under test takes: the gate's
// OWN `worktree add --detach` spelling, so the test cannot pass by creating the
// checkout some other way than production does.
func scratchWTAdd(repo, dir string) func() error {
	return func() error {
		_, err := repoSweepGit(repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreeAddVerb,
			mergeBuildGateWorktreeDetachFlag, dir, "HEAD")
		return err
	}
}

// scratchWTRegistered reports the porcelain entry for dir, or "" when the
// registry does not mention it. It is the assertion for both arms, and it reads
// the SAME source the merger, the sweep and finish-cleanup read.
func scratchWTRegistered(t *testing.T, repo, dir string) string {
	t.Helper()
	out, err := repoSweepGit(repo, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		t.Fatalf("worktree list: %v", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == finishCleanupPorcelainWorktreePrefix+dir {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// TestScratchWorktreeReleaseLeavesNoRegistration is the ORDINARY case: the
// measurement finishes, the release runs, and the registry must not know the
// checkout ever existed.
//
// RED on the pre-fix code: the registration outlived `t.TempDir()`'s cleanup,
// so `scratchWTRegistered` still returned the entry after the directory was
// gone — measured on this tree as
// `worktree /dev/shm/suite-tools-run-…/wt` + "prunable gitdir file points to
// non-existent location" surviving a single `go test -run` invocation.
func TestScratchWorktreeReleaseLeavesNoRegistration(t *testing.T) {
	repo := scratchWTRepo(t)
	dir := filepath.Join(t.TempDir(), "wt")

	release, err := mergeBuildGateScratchWorktree(repo, dir, "HEAD", scratchWTAdd(repo, dir))
	if err != nil {
		t.Fatalf("materialise the scratch checkout: %v", err)
	}
	// The PREMISE: the checkout really was registered, so the assertion below is
	// about a removed registration and not about a creation that never happened.
	if scratchWTRegistered(t, repo, dir) == "" {
		t.Fatalf("premise broken: %s was never registered by `worktree add`", dir)
	}

	release()

	if got := scratchWTRegistered(t, repo, dir); got != "" {
		t.Fatalf("the registration survived the release: %q — this is the leak `git worktree list` showed forever", got)
	}
	if _, serr := os.Stat(dir); !os.IsNotExist(serr) {
		t.Fatalf("the scratch directory %s still exists after the release (err=%v)", dir, serr)
	}
}

// TestScratchWorktreeReleaseCollectsTheLockedRegistration is the shape that
// makes residue PERMANENT, and the reason the release is a ladder instead of a
// single git verb.
//
// The lock is written the way git writes it — `locked` = "initializing", the
// exact bytes observed live during a `worktree add` — so this arm measures the
// INTERRUPTED state without needing to kill a process (Rule 42: nothing here
// signals anything it does not own).
//
// RED on the pre-fix code twice over: the registration was never collected at
// all, and the locked one additionally survives `prune` — measured, `prune -v`
// printed nothing and left the entry in place while `remove --force` answered
// "fatal: cannot remove a locked working tree, lock reason: initializing".
func TestScratchWorktreeReleaseCollectsTheLockedRegistration(t *testing.T) {
	repo := scratchWTRepo(t)
	dir := filepath.Join(t.TempDir(), "wt")

	release, err := mergeBuildGateScratchWorktree(repo, dir, "HEAD", scratchWTAdd(repo, dir))
	if err != nil {
		t.Fatalf("materialise the scratch checkout: %v", err)
	}

	// THE INTERRUPTION, reproduced faithfully: git's own lock file inside the
	// registration's admin dir, carrying git's own reason string.
	admin, aerr := worktreeSweepAdminDir(repo, dir)
	if aerr != nil {
		t.Fatalf("locate the registration's admin dir: %v", aerr)
	}
	if werr := os.WriteFile(filepath.Join(admin, worktreeSweepLockFileName),
		[]byte(worktreeSweepInitLockReason+"\n"), 0o644); werr != nil {
		t.Fatalf("write the initializing lock: %v", werr)
	}
	if !worktreeSweepRegistrationLocked(repo, dir) {
		t.Fatalf("premise broken: the lock file did not register as locked")
	}

	release()

	if got := scratchWTRegistered(t, repo, dir); got != "" {
		t.Fatalf("the LOCKED registration survived the release: %q — `git worktree prune` skips this shape forever", got)
	}
	if _, serr := os.Stat(filepath.Join(admin, worktreeSweepLockFileName)); !os.IsNotExist(serr) {
		t.Fatalf("the stale lock file survived the release (err=%v)", serr)
	}
}

// TestScratchWorktreeFailedCreationIsStillReleased pins the failure path: a
// creator that registers and THEN fails (`worktree add` interrupted between the
// registration and the checkout) must not be treated as "nothing to release".
//
// This is the arm a "release only on success" implementation fails, and it is
// the arm that explains the ③ shape in the production file's header: the lock
// exists because the creator died mid-flight, which is exactly a creation that
// reports failure.
func TestScratchWorktreeFailedCreationIsStillReleased(t *testing.T) {
	repo := scratchWTRepo(t)
	dir := filepath.Join(t.TempDir(), "wt")

	// Register, then fail — the created-then-interrupted shape.
	failing := func() error {
		if err := scratchWTAdd(repo, dir)(); err != nil {
			return err
		}
		return os.ErrDeadlineExceeded
	}

	release, err := mergeBuildGateScratchWorktree(repo, dir, "HEAD", failing)
	if err == nil {
		t.Fatalf("the creator's failure must be returned to the caller, not swallowed")
	}
	release() // must be a no-op-safe call: the failure path already reaped

	if got := scratchWTRegistered(t, repo, dir); got != "" {
		t.Fatalf("a FAILED creation left a registration behind: %q — the interrupted-add shape", got)
	}
}
