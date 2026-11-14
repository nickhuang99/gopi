package testutil

// The COMPILE-ONLY exemption from the cross-run package lock (suitepackage.go).
//
// ── THE DEFECT THIS CLOSES (measured 2026-11-08, row 01a1b996) ──────────────
//
// The delivery loop's test-compile leg is `go test -run '^$' -count=1 ./...`
// (internal/testcompile, the ONE spelling of it). `-run '^$'` matches no TEST,
// so every test binary is built and no test body runs — but **TestMain still
// runs**, and every suite in this repository takes the cross-run package lock
// there (PinSuiteForTestMain → AcquireSuitePackageLock). So the leg that
// touches NOTHING at runtime — no ledger, no listener, no child process, no
// scratch queue — was refused with exit 99 the moment any sibling run of the
// same package was in flight:
//
//	# full suite of one package running concurrently
//	go test -count=1 ./internal/scratch                  → FULL_EXIT=0 (116s)
//	# the same instant
//	go test -run '^$' ./internal/scratch                 → COMPILE_ONLY_EXIT=1
//	FATAL: suite "scratch": another run of this package is in flight.
//	  holder:   pid 361175 ... verdict live
//
// That is a FALSE POSITIVE of the mechanism, not a true collision: the whole
// reason for the lock is that two runs share the scratch home, the machine's
// TCP surface and the operator profile's ledger — and a run with no test body
// cannot reach any of the three. A refusal whose premise cannot hold is the
// class of noise that teaches an operator to ignore the refusal.
//
// ── WHY IT IS DETECTED HERE AND NOT AT THE CALLER ──────────────────────────
//
// The caller is every suite's TestMain, through one entry point; a per-suite
// opt-out would be N copies of one decision and the N+1th suite would forget it
// (Rule 14 — one definition). The detection is the PROCESS'S OWN ARGV, which is
// the only place the toolchain's instruction is visible to the binary it built.
//
// ── WHY THE SPELLING COMES FROM internal/testcompile ───────────────────────
//
// `^$` is not a literal this file may retype (Rule 1/13/14): it is
// testcompile.RunNothingFilter, the ONE definition, and `testcompile` is a
// LEAF (stdlib only — no gopi import), so `testutil → testcompile` cannot
// create the cycle that `testutil → internal/runlock` would (see
// suitepackage.go's header for that measured cycle).

import (
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/testcompile"
)

const (
	// testRunFlagName is how the `go test -run` instruction reaches the test
	// BINARY it started: the toolchain prefixes test-binary flags with `test.`
	// (`-test.run`, `-test.count`, …). It is the ONE spelling of that flag in
	// this file, in both its `-test.run VALUE` and `-test.run=VALUE` forms.
	testRunFlagName = "-test.run"
)

// runNothingRequested reports whether THIS test binary was asked to run no test
// at all, i.e. whether the process is the compile-only leg of a test build.
//
// The read is os.Args verbatim: the toolchain hands the filter to the binary it
// built, so the binary's own argv is the instruction — nothing else needs to be
// consulted, and nothing else could disagree with it.
func runNothingRequested() bool {
	return runNothingRequestedIn(os.Args)
}

// runNothingRequestedIn is the PURE half, so the detection can be asserted
// without re-executing a test binary with a fabricated argv.
func runNothingRequestedIn(args []string) bool {
	withValue := testRunFlagName + "="
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == testRunFlagName:
			if i+1 < len(args) && args[i+1] == testcompile.RunNothingFilter {
				return true
			}
		case strings.HasPrefix(arg, withValue):
			if strings.TrimPrefix(arg, withValue) == testcompile.RunNothingFilter {
				return true
			}
		}
	}
	return false
}
