package tools

// merge_build_gate_scratch_worktree.go — CREATE AND RELEASE ARE ONE FUNCTION.
//
// THE DEFECT, MEASURED (this host, git 2.43.0, 2026-10-25). Two registrations
// created by the TEST TestStylegateRatchetSuiteIsGreenOnThisTree lived in the
// repository's own `.git/worktrees/` and survived the test process:
//
//	worktree /dev/shm/suite-tools-run-<pid>/TestStylegate.../001/wt   ← prunable
//	worktree /dev/shm/suite-tools-run-<pid>/TestStylegate.../002/wt   ← locked
//
// Both are the shape `git worktree add --detach` leaves, and the producer was
// never in doubt: the test's helper checks a revision out into
// `filepath.Join(t.TempDir(), "wt")`. `testing` removes that DIRECTORY at the
// end of the test — and the directory is not where the registration lives.
//
// 5-WHY, and where the chain actually stops.
//
//	① Why did a registration survive the process? Because `git worktree add`
//	   writes `<repo>/.git/worktrees/<id>/` — inside the REPOSITORY, not in the
//	   directory it creates. Deleting the directory never deletes the
//	   registration; only `git worktree prune` does.
//
//	② Why did nobody prune? Because nothing in the creating path did. The gate's
//	   PRODUCTION paths all end in mergeBuildGateReapRegistrations; a caller that
//	   hand-rolls `worktree add` has no such tail at all.
//
//	③ Why is one of them `locked`? Because git writes `.git/worktrees/<id>/locked`
//	   = "initializing" at the START of `worktree add` and removes it when the
//	   checkout completes (MEASURED: observed live during an add). ANY
//	   interruption — a test binary killed by the suite's own deadline, `go test
//	   -timeout` firing, an operator ^C, a SIGKILL — leaves the mark. It is not a
//	   stray artifact; it is git's normal "a creator is running" flag surviving
//	   exactly because the creator did not.
//
//	④ Why is the locked one PERMANENT? MEASURED, three probes in /dev/shm:
//	     dir gone, no lock      → `prune` collects it ("Removing worktrees/…");
//	     dir gone + `locked`    → `remove --force` REFUSES ("fatal: cannot
//	                              remove a locked working tree") and `prune`
//	                              SKIPS it, exit 0, nothing printed — forever;
//	     clear the lock file then prune → "Removing worktrees/…", collected.
//	   So the lock is the difference between residue git collects for free and
//	   residue no sweep on this host can reach without the unlock repair.
//
//	⑤ Why does it poison the worktree registry? `git worktree list --porcelain`
//	   is an EVIDENCE SOURCE — the merger, the sweep's ownership verdict and
//	   finish-cleanup all read it. A registration nobody can remove makes that
//	   source permanently wrong about a path that does not exist.
//
// ROOT CAUSE: creation and release are two separate acts, and only the
// production paths perform the second. A caller that performs only the first
// leaves a registration behind, and an interrupted `add` upgrades that residue
// into the locked shape nothing collects.
//
// THE FIX makes the pair ONE function whose release is the measured ladder, not
// a single git verb:
//
//	clear a stale lock → remove --force → RemoveAll → prune → VERIFY
//
// That ladder is not new machinery (Rule 35): every rung already exists and is
// already the gate's own repair (worktree_sweep.go's worktreeDropStaleLock,
// merge_build_gate.go's mergeBuildGateRemoveWorktree and
// mergeBuildGateReapRegistrations). What was missing is that no single owner ran
// them in the order that WORKS once the directory is gone, and that one creation
// site had no owner at all.
//
// WHAT THIS DOES NOT REACH, stated so it is not mistaken for coverage: a
// SIGKILLed process runs no defer. That residue is the SWEEPER's business
// (worktreeSweepReapRegistration / worktreeDropStaleLock), and this file does
// not pretend to close it — it closes the ordinary case (the caller finishes,
// the release runs, the registration goes) and leaves the kill case to the arm
// that already owns it.

import (
	"fmt"
	"os"
	"strings"
)

// mergeBuildGateScratchWorktree materialises rev as a detached checkout at dir
// and returns the release that removes BOTH the directory and its git
// registration. The caller MUST invoke release (a defer, or the equivalent).
//
// The creation goes through the caller's own creator func so a scratch checkout
// can never be born a second way — production passes the gate's
// `worktree add --detach` spelling and a test passes the same one through the
// same runner. The release is the ladder above, because the cheap verb comes
// first and the expensive repair runs only when something is actually wrong.
func mergeBuildGateScratchWorktree(repo, dir, rev string, createOnly func() error) (release func(), err error) {
	if cerr := createOnly(); cerr != nil {
		// A FAILED creation can still have registered (shape ③: killed inside
		// `add`). Reaping it is the whole point of pairing the two acts, so the
		// failure path releases too — and the ORIGINAL error is the one returned
		// (Rule 15b: the reap's own failures are reported through the notify log
		// and the audit record, never folded into the caller's cause).
		mergeBuildGateReapOneScratch(repo, dir, "the scratch checkout this call failed to create")
		return func() {}, cerr
	}
	return func() {
		mergeBuildGateReapOneScratch(repo, dir, "the scratch checkout this call created; its measurement has finished")
	}, nil
}

// mergeBuildGateReapOneScratch drops ONE scratch checkout's directory and its
// git registration, in the measured order. It is idempotent: every rung
// tolerates "already gone", so a caller may run it on a checkout an earlier rung
// already collected.
//
// Each rung's failure is REPORTED, never swallowed (Rule 15b) — but it does not
// abort the ladder, because the rungs repair different states: `remove --force`
// is the one that works while the directory exists, and `prune` is the one that
// works once it does not. Stopping at the first refusal would be stopping
// exactly where the locked shape begins.
func mergeBuildGateReapOneScratch(repo, dir, evidence string) {
	// ① THE LOCK IS THE HALF THAT MAKES RESIDUE PERMANENT, so it is cleared
	// first. worktreeDropStaleLock is a no-op when no lock is present, and it is
	// the ONE repair for this state (Rule 14: the sweeper's reap calls the same
	// function).
	if lerr := worktreeDropStaleLock(repo, dir); lerr != nil {
		taskNotifyLog("merger: BUILD gate scratch %s has a lock that could not be cleared: %s",
			dir, mergeBuildGateTail(lerr.Error()))
	}
	// ② git's own removal, while the directory still exists — it must run before
	// ③, which is the only reason the order is not cosmetic.
	if rerr := mergeBuildGateRemoveWorktree(repo, dir, evidence); rerr != nil {
		// Expected once the directory is gone; reported so a refusal that is NOT
		// the expected one is visible (Rule 15b).
		taskNotifyLog("merger: BUILD gate scratch %s not removed by git (%s) — dropping the directory and pruning",
			dir, mergeBuildGateTail(rerr.Error()))
	}
	// ③ The directory, whatever git refused to do. It is this call's own
	// checkout, created above, so removing it is inside Rule 42's "the caller
	// created and holds it".
	if rmErr := os.RemoveAll(dir); rmErr != nil {
		taskNotifyLog("merger: BUILD gate scratch directory %s not removed: %v", dir, rmErr)
	}
	// ④ The registration. Once the directory is gone `prune` is the ONLY verb
	// that drops it (measured, ④ above) — and it now collects it, because ①
	// cleared the lock that made it refuse.
	if _, perr := repoSweepGit(repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreePruneVerb); perr != nil {
		taskNotifyLog("merger: BUILD gate prune in %s failed: %s", repo, mergeBuildGateTail(perr.Error()))
	}
	// ⑤ POST-CONDITION, checked rather than assumed: a registration still listed
	// for this directory means the ladder did not do what it claims, and a silent
	// one is the whole defect this file exists for (Rule 15).
	switch still, serr := mergeBuildGateScratchStillRegistered(repo, dir); {
	case serr != nil:
		taskNotifyLog("merger: BUILD gate scratch %s could not be verified clean: %v", dir, serr)
	case still:
		taskNotifyLog("merger: BUILD gate scratch %s is STILL registered after unlock+remove+prune — this is the residue shape", dir)
	}
}

// mergeBuildGateScratchStillRegistered reports whether dir still appears in the
// repository's `worktree list`. An unreadable list is an ERROR, not a "no": a
// check that cannot answer must not read as clean (Rule 15).
func mergeBuildGateScratchStillRegistered(repo, dir string) (bool, error) {
	out, err := repoSweepGit(repo, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return false, fmt.Errorf("list worktrees in %s: %w", repo, err)
	}
	want := finishCleanupPorcelainWorktreePrefix + dir
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true, nil
		}
	}
	return false, nil
}
