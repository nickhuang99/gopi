package heavytest

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (Rule 22: a temp workspace never lives on
// the / partition). Without it the two t.TempDir() calls in boundary_test.go —
// the generated fixture module (boundaryFixture) and the skip-ledger path in
// TestBoundaryRefusalWritesTheLedger — land on the AMBIENT temp root, i.e.
// /tmp on this host, the 87%-full partition behind the 2026-09-11 exhaustion.
// See internal/testutil/scratchroot.go and devdoc/tmp-ban-scratch-root.md.
//
// ROUTE (a) PIN, NOT (b) RELOCATE — the evidence: BOTH scratch sites are
// created BY THIS package's own tests. boundaryFixture() materialises a
// throwaway module and TestBoundaryRefusalWritesTheLedger writes a ledger
// file; neither is a shared helper delegating to an already-pinned suite, so
// there is nothing to hand off to internal/scratch.MkdirTemp. The point is
// that this suite's OWN run — boundary_test.go runs smoke, unclassified by
// heavytest.Require, i.e. on the merge/verify path — must pin its scratch.
//
// LEAF-PACKAGE DESIGN INTACT: heavy.go documents this package as a leaf with
// ZERO project imports (so in-package tests of scratch/tools/agent can import
// it without a cycle). The testutil import lives in THIS _test.go only, so
// heavytest's production import set stays empty — proven, not argued:
//
//	CGO_ENABLED=0 go list -deps ./internal/heavytest | grep -c 'nick/gopi'  ->  1
//
// (the package itself; testutil and its transitive deps are _test-only and
// never appear in the production import graph).
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "heavytest")
}
