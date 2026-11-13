package tools

// worktree_sweep_dead_registration_test.go — the regression guard for the
// SIGKILLed `worktree add` leak.
//
// THE BUG (measured on this host, 2026-09-14): a merge-build gate checkout is
// created with `git worktree add --detach`. While that command runs, git writes
// `<repo>/.git/worktrees/<id>/locked` containing "initializing". A SIGKILL
// between the registration and the completion leaves BOTH the lock and (once a
// defer's os.RemoveAll runs) a registration whose directory is gone. Nothing
// collects that shape:
//
//   - `git worktree prune` SKIPS a locked entry (probe: the locked dead
//     registration survived `prune -v` untouched, twice),
//   - `git worktree remove --force` REFUSES it ("cannot remove a locked working
//     tree, lock reason: initializing — use 'remove -f -f' to override or
//     unlock first"),
//   - `git worktree unlock <path>` FAILS once the directory is gone
//     ("'…' is not a working tree"),
//   - and the sweeper's own judge classified it `keep` twice over — once for
//     Locked ("an explicit do-not-touch") and once for Prunable ("the repo
//     prune collects it"), neither of which is true for this shape.
//
// Every test below drives the REAL runner against a REAL git repository (the
// registration, the lock and the directory are the fixture's own), so the RED
// proof is the leak's own mechanism rather than a mock's.
//
// RED-before/GREEN-after: on the pre-fix tree these fail with the registration
// still present and `reaped=0`; the fix must make the admin entry disappear, the
// directory contain no leaked registration, and the reaped count match.

import (
	"context"
	"github.com/nick/gopi/internal/scratch"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/worktreeroots"
)

// sweepDeadFixture builds the leak exactly as the gate does: a linked worktree
// whose registration carries git's "initializing" lock and whose directory has
// been removed (the SIGKILL-then-RemoveAll shape). It returns the repo, the
// dangling worktree path and the branch name ("" for a detached checkout).
func sweepDeadFixture(t *testing.T, branch string) (repo, wt, regID string) {
	t.Helper()
	repo = sweepInitRepo(t)
	wt = filepath.Join(sweepFixtureRoot(t), "wt-dead")
	args := []string{"worktree", "add", wt}
	if branch == "" {
		args = append(args, "--detach")
	} else {
		args = append(args, "-b", branch)
	}
	sweepGit(t, repo, args...)
	regID = sweepRegistrationID(t, repo, wt)
	lockPath := filepath.Join(repo, worktreeSweepAdminPathName, regID, worktreeSweepLockFileName)
	if err := os.WriteFile(lockPath, []byte(worktreeSweepInitLockReason), 0o644); err != nil {
		t.Fatalf("write the initializing lock: %v", err)
	}
	// AGE the lock past the in-flight floor: an abandoned lock is by definition
	// OLD (the creator died a while ago), while a lock seconds old belongs to a
	// `worktree add` that is still running — see worktreeSweepInitLockFloor and
	// TestSweepKeepsALockYoungerThanTheInFlightFloor for that half.
	sweepAgeFile(t, lockPath, worktreeSweepInitLockFloor+time.Hour)
	if err := os.RemoveAll(wt); err != nil {
		t.Fatalf("remove the worktree directory: %v", err)
	}
	return repo, wt, regID
}

// sweepAgeFile backdates a file's mtime by d (the fixture's way to say "this
// happened d ago" without waiting).
func sweepAgeFile(t *testing.T, path string, d time.Duration) {
	t.Helper()
	when := time.Now().Add(-d)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("age %s: %v", path, err)
	}
}

// sweepRegistrationID resolves git's admin id for a worktree path by matching
// the recorded backlink — the same identity rule the repair uses.
func sweepRegistrationID(t *testing.T, repo, wt string) string {
	t.Helper()
	adminRoot := filepath.Join(repo, worktreeSweepAdminPathName)
	entries, err := os.ReadDir(adminRoot)
	if err != nil {
		t.Fatalf("read %s: %v", adminRoot, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(adminRoot, entry.Name(), worktreeSweepGitdirFileName))
		if rerr != nil {
			continue
		}
		if strings.TrimSpace(string(raw)) == filepath.Join(wt, worktreeSweepGitdirName) {
			return entry.Name()
		}
	}
	t.Fatalf("no registration under %s names %s", adminRoot, wt)
	return ""
}

// sweepDeadRun executes one real sweep over the fixture repo.
func sweepDeadRun(t *testing.T, repo string, dryRun bool) worktreeSweepResult {
	t.Helper()
	opts := sweepOptionsNow(dryRun)
	opts.MinAge = time.Hour
	return worktreeSweepRunWith(context.Background(), opts,
		worktreeSweepDeps{Repos: []string{repo}, LedgerRead: true}, nil)
}

// TestWorktreeDropStaleLockRepairsTheGateShape is the fix for the gate's OWN
// residue: merge_build_gate's defer must not leave a "locked: initializing"
// registration behind when it dies mid-add. The repair is the shared primitive,
// so the test drives it directly on the exact shape a SIGKILLed
// `git worktree add` leaves — registration present, lock present, directory
// present (the gate's state) — and on the dir-gone variant.
func TestWorktreeDropStaleLockRepairsTheGateShape(t *testing.T) {
	repo := sweepInitRepo(t)
	root := sweepFixtureRoot(t)
	wt := filepath.Join(root, "wt-gate-shape")
	sweepGit(t, repo, "worktree", "add", wt, "--detach")
	regID := sweepRegistrationID(t, repo, wt)
	lock := filepath.Join(repo, worktreeSweepAdminPathName, regID, worktreeSweepLockFileName)
	if err := os.WriteFile(lock, []byte(worktreeSweepInitLockReason), 0o644); err != nil {
		t.Fatal(err)
	}
	// git's own refusal, asserted: without the repair the gate cannot remove
	// the checkout it made.
	if out, err := sweepGitErr(t, repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreeRemoveVerb, mergeBuildGateWorktreeForceFlag, wt); err == nil {
		t.Fatalf("git removed a locked worktree without the repair (%q) — the fixture no longer reproduces the incident", out)
	}
	if err := worktreeDropStaleLock(repo, wt); err != nil {
		t.Fatalf("worktreeDropStaleLock: %v", err)
	}
	if _, serr := os.Stat(lock); !os.IsNotExist(serr) {
		t.Fatalf("the lock survived the repair")
	}
	// The repair makes git's own removal succeed again — the gate's second step.
	sweepGit(t, repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreeRemoveVerb, mergeBuildGateWorktreeForceFlag, wt)
	if _, serr := os.Stat(filepath.Join(repo, worktreeSweepAdminPathName, regID)); !os.IsNotExist(serr) {
		t.Fatalf("the registration survived git's post-repair removal")
	}

	// The dir-GONE variant: no `unlock` target to point at, so the fix's second
	// step (the lock file inside git's admin area) is the one that must hold.
	repo2, wt2, id2 := sweepDeadFixture(t, "")
	if err := worktreeDropStaleLock(repo2, wt2); err != nil {
		t.Fatalf("worktreeDropStaleLock on a gone directory: %v", err)
	}
	sweepGit(t, repo2, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreePruneVerb)
	if _, serr := os.Stat(filepath.Join(repo2, worktreeSweepAdminPathName, id2)); !os.IsNotExist(serr) {
		t.Fatalf("the unlocked dead registration %s survived a prune", id2)
	}
}

// sweepGitErr runs one git command and returns its output and error (the
// refusal assertions need the error, where sweepGit fails the test).
func sweepGitErr(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestSweepReapsLockedDeadRegistration is THE leak regression: a locked
// registration whose directory is gone must be dropped, not kept forever.
func TestSweepReapsLockedDeadRegistration(t *testing.T) {
	repo, wt, regID := sweepDeadFixture(t, "")
	regs, err := worktreeSweepRegistrations(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 2 {
		t.Fatalf("fixture has %d registrations, want 2 (main + the dead one)", len(regs))
	}
	if !regs[1].Locked {
		t.Fatalf("the fixture's registration is not locked — the leak shape was not built: %+v", regs[1])
	}
	// The precondition that makes this the LEAK: git's own prune cannot see it.
	sweepGit(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreePruneVerb)
	if _, serr := os.Stat(filepath.Join(repo, worktreeSweepAdminPathName, regID)); serr != nil {
		t.Fatalf("the fixture is not the leak: prune alone already removed the locked registration (%v)", serr)
	}

	res := sweepDeadRun(t, repo, false)
	if res.Reaped != 1 {
		t.Fatalf("reaped=%d (%+v), want 1 — the locked dead registration leaked", res.Reaped, res.Findings)
	}
	if _, serr := os.Stat(filepath.Join(repo, worktreeSweepAdminPathName, regID)); !os.IsNotExist(serr) {
		t.Fatalf("the registration %s still exists after the sweep", regID)
	}
	regs, err = worktreeSweepRegistrations(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 1 || !regs[0].Main {
		t.Fatalf("registrations left: %+v, want only the main checkout", regs)
	}
	// The dead worktree path is NOT recreated by the repair (the directory was
	// already gone; only the bookkeeping was collected).
	if _, serr := os.Stat(wt); !os.IsNotExist(serr) {
		t.Fatalf("the repair created the dead directory %s", wt)
	}
}

// TestSweepReapsDeadRegistrationBranchWithTheWorktree answers the epic's second
// question in code: the registration AND its branch go together, through git's
// own `branch -d` — so an unmerged branch is refused and reported instead.
func TestSweepReapsDeadRegistrationBranchWithTheWorktree(t *testing.T) {
	// A branch whose tip never moved past master: `branch -d` accepts it.
	repo, _, regID := sweepDeadFixture(t, "feat/dead-clean")
	res := sweepDeadRun(t, repo, false)
	if res.Reaped != 1 || res.BranchesReaped != 1 {
		t.Fatalf("reaped=%d branches=%d (%+v), want 1/1", res.Reaped, res.BranchesReaped, res.Findings)
	}
	if out := sweepGit(t, repo, worktreeSweepBranchVerb, "--list", "feat/dead-clean"); strings.TrimSpace(out) != "" {
		t.Fatalf("branch feat/dead-clean survived: %q", out)
	}
	if _, serr := os.Stat(filepath.Join(repo, worktreeSweepAdminPathName, regID)); !os.IsNotExist(serr) {
		t.Fatalf("registration %s survived the reap", regID)
	}

	// The UNMERGED case: work the branch alone holds is never force-deleted —
	// the registration goes, the ref stays, and the finding says so.
	repo2 := sweepInitRepo(t)
	root2 := sweepFixtureRoot(t)
	live := filepath.Join(root2, "wt-live")
	sweepGit(t, repo2, finishCleanupWorktreeVerb, "add", live, "-b", "feat/dead-unmerged")
	sweepGit(t, live, "commit", "--allow-empty", "-q", "-m", "unmerged work")
	sweepGit(t, repo2, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, "--force", live)
	dead2 := filepath.Join(root2, "wt-dead2")
	sweepGit(t, repo2, finishCleanupWorktreeVerb, "add", dead2, "feat/dead-unmerged")
	id2 := sweepRegistrationID(t, repo2, dead2)
	lock2 := filepath.Join(repo2, worktreeSweepAdminPathName, id2, worktreeSweepLockFileName)
	if err := os.WriteFile(lock2, []byte(worktreeSweepInitLockReason), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepAgeFile(t, lock2, worktreeSweepInitLockFloor+time.Hour)
	if err := os.RemoveAll(dead2); err != nil {
		t.Fatal(err)
	}
	res2 := sweepDeadRun(t, repo2, false)
	if res2.Reaped != 1 || res2.BranchesReaped != 0 {
		t.Fatalf("reaped=%d branches=%d (%+v), want the registration gone and the unmerged ref kept", res2.Reaped, res2.BranchesReaped, res2.Findings)
	}
	if out := sweepGit(t, repo2, worktreeSweepBranchVerb, "--list", "feat/dead-unmerged"); strings.TrimSpace(out) == "" {
		t.Fatal("the unmerged branch was removed — unmerged work must survive")
	}
	found := false
	for _, f := range res2.Findings {
		if strings.Contains(f.Reason, "feat/dead-unmerged") && strings.Contains(f.Reason, "kept") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no finding named the kept unmerged branch (Rule 15): %+v", res2.Findings)
	}
}

// TestSweepReapIsStillRefusedWhileThePathIsHeld: a registration can be dir-less
// from OUR view and in use in another mount namespace — the holder veto stays.
func TestSweepReapIsStillRefusedWhileThePathIsHeld(t *testing.T) {
	root := sweepFixtureRoot(t)
	roots := []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(true)
	reg := sweepFixtureReg(root, "wt-dead-held", "feat/held")
	reg.Locked = true
	ho := worktreeSweepHostObservations{DirGone: true}
	holders := scratch.HolderScan{Paths: []string{filepath.Join(reg.Path, "keep")}}
	if f := worktreeSweepJudge(reg, nil, holders, roots, opts, ho); f.Disposition != string(worktreeKeep) {
		t.Fatalf("a held dir-gone registration was %s (%s)", f.Disposition, f.Reason)
	}
}

// TestSweepDeadRegistrationOwnedByLiveTaskKeepsItsBranch: the registration is
// dead (nothing to protect there), but a live row's branch is not touched.
func TestSweepDeadRegistrationOwnedByLiveTaskKeepsItsBranch(t *testing.T) {
	root := sweepFixtureRoot(t)
	roots := []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(true)
	reg := sweepFixtureReg(root, "wt-dead-owned", "feat/owned")
	ho := worktreeSweepHostObservations{DirGone: true}
	rows := []tasksRowForSweep{{TaskID: "T-live", Status: taskStatusInProgress, Branch: "feat/owned"}}
	f := worktreeSweepJudge(reg, rows, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeKeep) || !strings.Contains(f.Reason, "T-live") {
		t.Fatalf("a dead registration of a LIVE task's branch was %s (%s)", f.Disposition, f.Reason)
	}
}

// TestSweepDryRunReapsNothing is the posture guard: the plan may name a reap,
// the run may not perform one.
func TestSweepDryRunReapsNothing(t *testing.T) {
	repo, _, regID := sweepDeadFixture(t, "")
	calls := withGitCallRecorder(t)
	res := sweepDeadRun(t, repo, true)
	if res.WouldReap != 1 || res.Reaped != 0 {
		t.Fatalf("dry run: would_reap=%d reaped=%d (%+v), want 1/0", res.WouldReap, res.Reaped, res.Findings)
	}
	if n := len(calls.filter(finishCleanupWorktreeVerb, worktreeSweepUnlockVerb)); n != 0 {
		t.Fatalf("a dry run invoked %d unlocks", n)
	}
	if n := len(calls.filter(finishCleanupWorktreeVerb, finishCleanupWorktreePruneVerb)); n != 0 {
		t.Fatalf("a dry run invoked %d prunes", n)
	}
	if _, serr := os.Stat(filepath.Join(repo, worktreeSweepAdminPathName, regID)); serr != nil {
		t.Fatalf("the dry run removed the registration: %v", serr)
	}
}

// TestSweepKeepsALockYoungerThanTheInFlightFloor is the OTHER half of the
// dir-gone arm: the same shape with a FRESH lock must be KEPT. A live
// `git worktree add` writes exactly this (dir not yet populated, lock present),
// so reaping it would let the following repo prune pull the registration out
// from under the creator — the race the age floor exists to close.
func TestSweepKeepsALockYoungerThanTheInFlightFloor(t *testing.T) {
	repo := sweepInitRepo(t)
	root := sweepFixtureRoot(t)
	reg := sweepFixtureRegistration(t, repo, root, "wt-inflight", "")
	lockPath := filepath.Join(repo, worktreeSweepAdminPathName, reg, worktreeSweepLockFileName)
	if err := os.WriteFile(lockPath, []byte(worktreeSweepInitLockReason), 0o644); err != nil {
		t.Fatal(err)
	}
	res := sweepDeadRun(t, repo, false)
	if res.Reaped != 0 {
		t.Fatalf("reaped=%d (%+v), want 0 — a lock this fresh may belong to a creator that is still running", res.Reaped, res.Findings)
	}
	if _, serr := os.Stat(filepath.Join(repo, worktreeSweepAdminPathName, reg)); serr != nil {
		t.Fatalf("the in-flight registration was dropped: %v", serr)
	}
	found := false
	for _, f := range res.Findings {
		if f.Disposition == string(worktreeKeep) && strings.Contains(f.Reason, "may still be running") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no finding named the in-flight lock (Rule 15): %+v", res.Findings)
	}
}

// sweepFixtureRegistration creates a linked worktree (detached when branch is
// "") with a GONE directory and returns git's admin id for it — the shared
// fixture for the lock-age probes.
func sweepFixtureRegistration(t *testing.T, repo, root, name, branch string) string {
	t.Helper()
	wt := filepath.Join(root, name)
	args := []string{"worktree", "add", wt}
	if branch == "" {
		args = append(args, "--detach")
	} else {
		args = append(args, "-b", branch)
	}
	sweepGit(t, repo, args...)
	id := sweepRegistrationID(t, repo, wt)
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	return id
}
