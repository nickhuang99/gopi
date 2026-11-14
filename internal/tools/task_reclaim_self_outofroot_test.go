// Out-of-root disposal — the regression tests for the walk measured 2026-09-27:
//
//	own registered worktree kept: /tmp/gopi-ctl-01a0ddea — outside every declared worktree root — not swept
//	own registered worktree kept: /tmp/gopi-prefix-fix — outside every declared worktree root — not swept
//
// The refusal was CORRECT (`/tmp` shares the root device, which Rule 22 forbids) and
// that was the defect: it was also the ONLY door, so the registration stayed forever
// and `scripts/safe-merge.sh` gate [c] kept withholding its branch. Both tests below
// drive reclaimSelfOwnOutOfRootDisposal directly, on a fixture registration and the
// host's REAL declared roots — the arm is only reachable for a registration that the
// real roots refuse, so the fixture is placed on a path and the test asserts which
// of the two host conditions it needs before asserting anything.
package tools

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// outOfRootFixture builds a repo with one linked worktree at dir, and returns the
// repo, the registration and the branch. `git worktree add` writes the whole chain
// the arm measures (age, status, removal), so nothing about the fixture is faked.
func outOfRootFixture(t *testing.T, dir, branch string) (repo string, reg worktreeRegistration) {
	t.Helper()
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")
	// The branch is created and the repo leaves it BEFORE the registration: git
	// refuses to check the same branch out twice, and a repo left standing on it
	// would make `worktree add` fail rather than register.
	gitRun(t, repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "deliverable.txt"), []byte("the delivery"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "deliverable.txt")
	gitRun(t, repo, "commit", "-m", "the deliverable")
	gitRun(t, repo, "checkout", "master")
	gitRun(t, repo, finishCleanupWorktreeVerb, "add", dir, branch)
	return repo, worktreeRegistration{Path: dir, Branch: branch}
}

// outOfRootSkipIfDeclared skips the test with the MEASURED reason when dir is
// inside a declared root — the arm declines such a registration by design, so on
// such a host the fixture cannot exercise it and the test must say so rather than
// pass blind (Rule 15: an unmeasurable condition is never reported as a result).
func outOfRootSkipIfDeclared(t *testing.T, repo, dir string) []worktreeroots.Root {
	t.Helper()
	roots := worktreeSweepRoots(repo)
	if r, ok := worktreeroots.PermitsAny(roots, dir); ok {
		t.Skipf("%s is inside the declared root %s (%s), so the out-of-root arm declines it here — "+
			"this fixture needs a path outside every declared root", dir, r.Path, r.Role)
	}
	return roots
}

// TestOutOfRootDisposalRefusesRootFSRegistration is the POSITIVE CONTROL for the
// refusal that must survive the disposal arm: a registration outside every declared
// root that sits on the `/` device is REPORTED, never removed.
//
// IT CREATES NOTHING THERE, deliberately. The arm refuses at rung ① — the device
// measurement — before it looks at age, status or holders, so the registration is a
// PATH to measure and never a directory to make: staging the ban by writing a
// worktree onto `/` would commit the very act Rule 22 forbids, in a test whose
// subject is that it is forbidden. `masterDeviceFixturePath` therefore returns only
// the measured path, and the assertion is that the arm reported the ban and did not
// touch it.
func TestOutOfRootDisposalRefusesRootFSRegistration(t *testing.T) {
	path := masterDeviceFixturePath(t)
	// The registration is deliberately synthetic (no directory exists at it): the
	// refusal under test reads the DEVICE of the nearest existing ancestor and
	// returns before any other rung, so a real checkout would test nothing more.
	reg := worktreeRegistration{Path: path, Branch: "detached"}
	repo := t.TempDir()

	line, disposed, handled := reclaimSelfOwnOutOfRootDisposal(reg, repo, worktreeSweepRoots(repo), scratch.ScanHolders())
	if disposed {
		t.Fatalf("the arm DISPOSED of a `/`-device registration — Rule 22's ban must never be repaired by deleting what is in it: %s", line)
	}
	if !handled {
		t.Fatalf("the arm declined a registration outside every declared root instead of refusing it by name — "+
			"the `outside every declared worktree root — not swept` walk would be permanent again: %s", line)
	}
	if !strings.Contains(line, reclaimSelfOwnRootFSReason) {
		t.Errorf("the refusal does not name the root-device ban as its reason:\n%s", line)
	}
}

// masterDeviceFixturePath returns a path on the `/` device that is inside NO
// declared root of a throwaway repo — the condition the measured pair
// (the ambient temp root's gopi-ctl-01a0ddea, gopi-prefix-fix) sat in.
//
// The candidates are MEASURED, never assumed: the first that the same predicate the
// arm uses (scratch.AssertPathOffRootFS) reports as ON the root device wins, and a
// host where none does skips with the reason. Nothing is created at the path.
//
// The spellings come from their ONE definition (worktreeroots, Rule 14): these are
// HOST roots, and a second literal here would be a second answer to "which
// directory is the ambient temp root".
func masterDeviceFixturePath(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	roots := worktreeSweepRoots(repo)
	for _, candidate := range []string{worktreeroots.AmbientTempRoot} {
		probe := filepath.Join(candidate, "gopi-outofroot-fixture-not-created")
		if err := scratch.AssertPathOffRootFS(probe); err == nil {
			continue // this candidate is OFF `/` here; the next one may not be
		}
		if _, declared := worktreeroots.PermitsAny(roots, probe); declared {
			continue // inside a declared root: the arm declines such a path by design
		}
		return probe
	}
	t.Skipf("%s is not on the / device here, or is inside a declared root — the root-device refusal cannot be staged on this host",
		worktreeroots.AmbientTempRoot)
	return ""
}

// TestOutOfRootDisposalFreesAnOffRootRegistration is the DISPOSAL: outside every
// declared root, OFF the `/` device, clean, unheld and past the quarantine floor —
// the arm removes the CHECKOUT and leaves the BRANCH, which is the deliverable.
//
// RED before this commit, by construction: the pre-change tree had no such arm, so
// the registration reached reclaimSelfOwnWhy's rung ⑤ and came back with
// "outside every declared worktree root — not swept" for ever.
func TestOutOfRootDisposalFreesAnOffRootRegistration(t *testing.T) {
	// A directory on a device that is NOT `/` and is not a declared root: the
	// allocation is measured, never spelled — the host decides, and a host without
	// one skips.
	base := offRootFixtureBase(t)
	dir := filepath.Join(base, "gopi-outofroot-"+strconv.Itoa(os.Getpid()))
	//stylegate:ignore best-effort temp cleanup; the OS/harness sweeps leftovers too
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	branch := "feat/outofroot-disposal"
	repo, reg := outOfRootFixture(t, dir, branch)
	roots := outOfRootSkipIfDeclared(t, repo, dir)
	tip := gitOut(t, repo, "rev-parse", branch)

	// Age the registration past the quarantine floor. worktreeSweepCreatedAt takes
	// the FIRST HIT of a fixed order — the registration's own `gitdir` backlink
	// inside the main repo's worktrees area, then `HEAD`, then the git dir itself —
	// and all three are written once by `git worktree add`, so aging the first is
	// what ages the registration as production measures it.
	backlink, err := os.ReadFile(filepath.Join(dir, worktreeSweepGitdirName))
	if err != nil {
		t.Fatalf("read the registration backlink: %v", err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(backlink)), finishCleanupGitDirFilePrefix))
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	old := time.Now().Add(-reclaimSelfOwnQuarantineAge - time.Hour)
	if err := os.Chtimes(filepath.Join(gitdir, worktreeSweepGitdirFileName), old, old); err != nil {
		t.Fatalf("age the registration stamp: %v", err)
	}
	if err := os.Chtimes(filepath.Join(gitdir, worktreeSweepHeadName), old, old); err != nil {
		t.Fatalf("age the registration HEAD: %v", err)
	}
	if created, err := worktreeSweepCreatedAt(dir); err != nil || time.Since(created) < reclaimSelfOwnQuarantineAge {
		t.Fatalf("the fixture is not past the quarantine floor after aging (created=%v err=%v)", created, err)
	}

	line, disposed, handled := reclaimSelfOwnOutOfRootDisposal(reg, repo, roots, scratch.ScanHolders())
	if !handled {
		t.Fatalf("the arm declined a registration outside every declared root — the walk would be permanent again: %s", line)
	}
	if !disposed {
		t.Fatalf("the arm did not dispose of an off-root, clean, unheld, old out-of-root registration: %s", line)
	}
	if !strings.Contains(line, reclaimSelfOwnQuarantinedMarker) || !strings.Contains(line, branch) {
		t.Errorf("the disposal line does not name the release and the branch it left alone:\n%s", line)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("the registration's directory is still on disk after a reported disposal: %s", dir)
	}
	if got := gitOut(t, repo, "rev-parse", branch); got != tip {
		t.Errorf("the branch moved or vanished during a disposal: %s → %s (the branch is the deliverable and is never touched)", tip, got)
	}
	if out := gitOut(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb); strings.Contains(out, dir) {
		t.Errorf("git still lists the registration after a reported disposal:\n%s", out)
	}
	// KEEP THE BRANCH REACHABLE FOR THE ASSERTION ABOVE, then let the fixture go.
	_ = reg
}

// offRootFixtureBase returns a directory on a filesystem that is NOT `/` — the
// device question is MEASURED (scratch.AssertPathOffRootFS), never spelled, so this
// is a host capability probe and not a path list. It skips when the host has none,
// which is honest: the disposal arm's off-root branch is not stageable there.
func offRootFixtureBase(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{worktreeroots.ShmDir} {
		if err := os.MkdirAll(candidate, 0o700); err != nil {
			continue
		}
		if err := scratch.AssertPathOffRootFS(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("no off-/ directory is available to stage an out-of-root registration (the / device is the only writable area here)")
	return ""
}
