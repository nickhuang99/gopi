package testutil

// The test-side entry points for the two halves of the daemon-leak fix.
//
//  1. THE BINDING (internal/daemonlifetime): the test binary holds one end of a
//     pipe, every daemon it starts holds the other, and the KERNEL closes the
//     test binary's end whatever kills it — SIGKILL included. That is the half
//     that prevents a FUTURE leak.
//  2. THE SWEEP (orphansweep.go): a daemon orphaned by an EARLIER run (started
//     before the binding existed, or by a run where arming failed) is still
//     running and still holding its tmpfs sandbox. Nothing can bind a process
//     that is already detached, so the backstop finds it by ownership and stops
//     it. That is the half that cleans up the PAST.
//
// Both are wired by one call, so a suite cannot end up with half the protection.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/daemonlifetime"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// ArmAndSweepForSuite is what a suite's TestMain calls (after
// PinSuiteForTestMain, before m.Run):
//
//   - it arms the daemon lifetime binding for THIS test process, so every daemon
//     this run starts dies with it even under SIGKILL;
//   - it then sweeps the suite's own run roots for orphans left by earlier runs.
//
// Order matters: the sweep runs FIRST in effect (arm then sweep is harmless,
// but the sweep must complete before the suite starts its own daemons, and the
// arm must be in place before the first daemon is started — this single call
// guarantees both).
//
// Neither half is silent about failure (Rule 15): an unarmed binding and a
// failed sweep both go to stderr, because "the protection is not actually in
// force" is exactly what a later reader must not have to guess.
func ArmAndSweepForSuite(suite string) {
	if err := ArmDaemonLifetime(); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: %s: daemon lifetime binding NOT armed: %v\n", suite, err)
	}
	SweepOrphansAtSuiteStart(suite)
	// The ROOT half of the backstop does NOT live here: it is armed by the pin
	// itself (PinSuiteForTestMain → ReapDeadSuiteRunRootsAtSuiteStart), because
	// this call is not made by every suite — internal/tools' TestMain pins and
	// finishes without it, and that suite is one of the two that mint most of the
	// /dev/shm roots (measured 2026-10-22: 11 of 19). One call site for "reap my
	// own family's dead roots", at the pin every suite shares, is what makes the
	// arm reach all of them.
}

// ReapDeadSuiteRunRootsAtSuiteStart collects the run roots THIS SUITE left in the
// trees its own pin lands in, whose recorded owner the kernel confirms dead. It
// deletes nothing else: the scope members are `<parent>/suite-<suite>-run-`
// prefixes (SuiteOrphanScopeRoots), and every declined root is REPORTED with its
// reason so an operator can tell "still in use" from "the sweep does not work"
// (Rule 15).
func ReapDeadSuiteRunRootsAtSuiteStart(suite string) (removed int) {
	for _, member := range SuiteOrphanScopeRoots(suite) {
		parent, pattern := filepath.Dir(member), filepath.Base(member)
		n, kept, err := scratch.ReapDeadSuiteRunRootsUnder(parent, pattern)
		if err != nil {
			fmt.Fprintf(os.Stderr, "testutil: %s: suite-run root sweep of %s failed: %v\n", suite, member, err)
			continue
		}
		for _, k := range kept {
			fmt.Fprintf(os.Stderr, "testutil: %s: kept suite run root %s\n", suite, k)
		}
		if n > 0 {
			fmt.Fprintf(os.Stderr, "testutil: %s: reaped %d dead suite run root(s) under %s\n", suite, n, parent)
			removed += n
		}
	}
	return removed
}

// ArmDaemonLifetime binds every daemon this test process — or any launcher child
// it starts — to this process's lifetime, using the production mechanism.
//
// An error means the binding is NOT in force: the caller reports it, never
// absorbs it. A suite that believes it is protected while it is not would rebuild
// the original leak with nobody watching.
func ArmDaemonLifetime() error {
	return daemonlifetime.ArmThisProcess()
}

// DaemonLifetimeArmed reports whether the binding is in force — the guard a case
// that DEPENDS on it uses, so it fails rather than passing for the wrong reason.
func DaemonLifetimeArmed() bool { return daemonlifetime.ArmedForThisProcess() }
