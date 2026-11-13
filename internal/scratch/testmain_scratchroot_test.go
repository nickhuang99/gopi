package scratch_test

import (
	"os"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// This file is an EXTERNAL test package (scratch_test) on purpose: the tests in
// this directory live in package scratch (they exercise unexported helpers such
// as collectReaders-scale internals and the fixture builders), and internal/
// testutil imports nothing from here — but keeping the pin in an external test
// package is the shape every other suite in this repo uses, so the pin has one
// spelling and cannot be broken by a future internal test file's import.
//
// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (the /tmp ban; every t.TempDir() here —
// the sweep fixtures build scratch homes — would otherwise land in /tmp, the
// 87%-full partition behind the 2026-09-11 exhaustion). See
// internal/testutil/scratchroot.go and devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	// The package-lock helper child must return BEFORE the suite pin: it is a bare
	// lock probe with no suite of its own, and entering the pin would os.Exit on a
	// verdict instead of running the helper (same reason internal/runlock's own
	// helper branch comes first).
	if handled, code := runSuitePackageLockHelper(); handled {
		// os.Exit, not `return`: returning from TestMain exits 0, which would report a
		// REFUSAL as success — the silent failure this whole mechanism is about.
		os.Exit(code)
	}
	testutil.RunSuiteMain(m, "scratch")
}
