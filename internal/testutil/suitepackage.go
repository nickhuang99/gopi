package testutil

// Cross-run exclusion for a whole suite of one package, installed in the ONE
// entry point every suite already shares (PinSuiteForTestMain) and released by the
// ONE exit path (FinishSuiteForTestMain).
//
// WHERE THE MECHANISM LIVES, AND WHY NOT HERE. The lock itself is
// internal/scratch's (suite_package_lock.go): that package already owns the scratch
// home, the advisory lock the sweep takes, the .holder record every scratch entry
// writes, and the /proc identity ladder that judges a recorded owner. This file is
// the SUITE half only — the exit code, the sentence an operator acts on, and the
// lifetime (take before m.Run, release after the root is gone). It could not be
// built on internal/runlock instead: runlock's own tests route through
// testutil.RunSuiteMain, so a testutil → runlock edge is an import cycle in the
// test binary (measured: "import cycle not allowed in test").
//
// WHY THERE IS EXCLUSION AT ALL (measured). Every suite already gets a PER-RUN
// scratch root (PinSuiteTempRoot → os.MkdirTemp — unique by construction), so it is
// tempting to think two concurrent runs are isolated. They are not:
//
//   - the scratch HOME (<workspace-home>/workspace/gopi-test-scratch) is ONE
//     directory per host — every run's GC scan and removal pass walks it, and its
//     queue subtree is deliberately not per-run;
//   - the operator profile's settings document and append-only ledger are reachable
//     through anything a suite does not pin;
//   - the machine's TCP surface and process table, which the web/voice arms exercise
//     with real listeners and real child processes.
//
// With a sibling run of the same package in flight the suite went quiet (the
// progress watcher printed NO PROGRESS); with no sibling, three consecutive
// whole-package runs printed it zero times.
//
// NOBODY MAY MASK THIS: raising runNoProgressStall hides a stall instead of removing
// it, and `-p 1` only makes the collision rarer while removing the parallelism the
// suite is sized for — and neither says anything (Rule 15). The refusal below NAMES
// the holder instead.

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testcompile"
)

const (
	// SuiteBusyExitCode is the process exit code a suite returns when a SIBLING RUN
	// of the same package is in flight. It is distinct from
	// SuiteScratchUnsafeExitCode (a scratch-root refusal) and
	// SuiteScratchNoSpaceExitCode (a capacity refusal) because the three need three
	// different fixes, and a wrapper that reads only the status must be able to tell
	// them apart — next free code after 98.
	SuiteBusyExitCode = 99

	// SuitePackageLockPrefix is the name prefix of a package's lock directory in the
	// shared scratch home. It is an ALIAS of internal/scratch's definition (Rule 14):
	// that leaf package's sweep must recognise the family it never collects, and a
	// second literal here would drift from the one it reads.
	SuitePackageLockPrefix = scratch.SuitePackageLockPrefix

	// SuitePackageLockOffEnv is the documented operator escape. The BINDING is
	// internal/scratch's (it is the package that decides), and this name is
	// re-exported so the refusal's FIX line cannot keep naming a knob that was
	// renamed (Rule 1b/13: one spelling of the knob).
	SuitePackageLockOffEnv = scratch.SuitePackageLockOffEnvName

	// testBinarySuffix is what the go tool appends to a package's import path to
	// name the test binary built from it ("…/cmd/stylegate" →
	// "…/cmd/stylegate.test" in the binary's own build info). It is the ONE
	// spelling of that suffix in this file.
	testBinarySuffix = ".test"
)

// heldSuitePackageLock is this process's take, released by FinishSuiteForTestMain.
var heldSuitePackageLock *scratch.SuitePackageLock

// suitePackageLockMu guards it (the release path must not race a take).
var suitePackageLockMu sync.Mutex

// AcquireSuitePackageLock takes this package's cross-run lock, or REFUSES the run
// with a sentence naming the sibling.
//
// Outcomes:
//
//	acquired      this run owns the package for its whole life (released by
//	              FinishSuiteForTestMain, or broken by the next run's ladder if this
//	              one was killed)
//	nested-suite  the holder is an ANCESTOR — a gate spawning `go test ./cmd/agent` as
//	              a child — so no second lock is taken and the run proceeds
//	disabled      GOPI_SUITE_PACKAGE_LOCK=0: the run proceeds and SAYS SO
//	refused       a sibling is in flight; the message names its pid, its start
//	              instant and the tree it works in, and the process exits with
//	              SuiteBusyExitCode
//
// A refusal is a fatal exit rather than a returned error on purpose: every suite
// calls this from TestMain BEFORE m.Run, and a caller that could ignore the error
// would run straight into the interleaving this exists to stop. The ONE error return
// is an infrastructure failure (the scratch home could not be written) — a different
// fact, which must never be reported as "somebody else is running" (Rule 15).
func AcquireSuitePackageLock(suite string) (*scratch.SuitePackageLock, error) {
	// COMPILE-ONLY RUN: the toolchain asked for no test, so this process runs no
	// test body — the premise of the whole exclusion (the shared scratch home, the
	// machine's TCP surface, the operator profile's ledger) cannot hold, and taking
	// the lock would refuse the delivery loop's test-compile leg whenever a sibling
	// suite of the same package was in flight. Announced, never silent (Rule 15): a
	// reader of the log must be able to see that THIS run took no lock, and why.
	if runNothingRequested() {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: %s=%s — this run executes NO test (a test-COMPILE leg), so the cross-run package lock is NOT taken: it guards runtime state that a compile-only run never touches\n",
			suite, testRunFlagName, testcompile.RunNothingFilter)
		return nil, nil
	}
	workDir, err := os.Getwd()
	if err != nil {
		// The working directory is DIAGNOSTIC (it is what a refusal shows the
		// sibling's operator), so an unreadable cwd leaves the field absent rather
		// than failing the take — and it is reported, never swallowed (Rule 15b).
		fmt.Fprintf(os.Stderr, "testutil: suite %q: cannot read the working directory for the lock record: %v\n", suite, err)
		workDir = ""
	}
	// The identity the take is about to be keyed on: reported BEFORE the take, so a
	// refusal is never read without the reader being able to see what was keyed.
	announceSuitePackageLockIdentity(suite)
	lock, err := scratch.AcquireSuitePackageLock(ScratchHomeDir(), suitePackageLockKey(suite), workDir)
	var busy *scratch.SuitePackageBusyError
	if errors.As(err, &busy) {
		fmt.Fprint(os.Stderr, suitePackageBusyMessage(suite, busy))
		os.Exit(SuiteBusyExitCode)
	}
	if err != nil {
		return nil, err
	}
	switch lock.Outcome {
	case scratch.SuitePackageLockOff:
		// Announced, not silent: the operator asked for this, and the next reader of
		// the log must not conclude the lock was in force (Rule 15).
		fmt.Fprintf(os.Stderr, "testutil: suite %q: the package lock is DISABLED by %s=0 — two concurrent runs of this package may interleave\n",
			suite, SuitePackageLockOffEnv)
	case scratch.SuitePackageLockNested:
		fmt.Fprintf(os.Stderr, "testutil: suite %q: %s — continuing WITHOUT taking a second lock\n",
			suite, lock.Outcome)
	}
	if lock.Held {
		suitePackageLockMu.Lock()
		heldSuitePackageLock = lock
		suitePackageLockMu.Unlock()
	}
	return lock, nil
}

// ReleaseSuitePackageLock frees the package for the next run. It is idempotent and
// safe when nothing was taken (the nested path), so FinishSuiteForTestMain calls it
// unconditionally.
func ReleaseSuitePackageLock() error {
	suitePackageLockMu.Lock()
	lock := heldSuitePackageLock
	heldSuitePackageLock = nil
	suitePackageLockMu.Unlock()
	if lock == nil {
		return nil
	}
	if err := lock.Release(); err != nil {
		return fmt.Errorf("testutil: %w", err)
	}
	return nil
}

// suitePackageBusyMessage is the refusal. ONE function so every field is named in
// one place: the suite, the verdict word, the address, the holder's own evidence,
// and the things an operator can actually DO (Rule 15 — a refusal that does not say
// what to do next is half a refusal).
func suitePackageBusyMessage(suite string, busy *scratch.SuitePackageBusyError) string {
	ownership := suitePackageHolderOwnership(busy.HolderPid)
	return fmt.Sprintf(
		"\nFATAL: suite %q: %s.\n"+
			"  lock:     %s\n"+
			"  holder:   %s\n"+
			"%s"+
			"Two whole-suite runs of ONE package share resources that live OUTSIDE either run's clone — the scratch home %s, "+
			"the machine's TCP surface, and the operator profile's ledger — so they must not run at the same time. This run "+
			"refuses instead of interleaving with the sibling: a silent wait would be reported as a stall (NO PROGRESS) about "+
			"the wrong thing, which is the defect this exclusion exists to end.\n"+
			"FIX: wait for the holder above to finish; or, BEFORE any signal, check that the holder is a process you own — "+
			"the ownership line above is that check, and a holder in another session (or an unmeasured one) is not yours to "+
			"signal, whatever the record says; or, if you have decided the overlap is acceptable, re-run with %s=0 (that path "+
			"is announced in this suite's output, never silent).\n\n",
		suite, busy.Outcome, busy.Dir, busy.Evidence, ownership.render(), ScratchHomeDir(), SuitePackageLockOffEnv)
}
