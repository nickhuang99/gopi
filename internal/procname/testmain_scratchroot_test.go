package procname_test

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root off the / partition (Rule 22): the
// synthetic procfs fixtures (identity_test.go's writeIdentityFixture) build
// directory trees with t.TempDir(), which otherwise lands on the ambient temp
// root — /tmp on this host, the 87%-full boot partition behind the 2026-09-11
// exhaustion.
//
// This file lives in the EXTERNAL test package on purpose. The pin funnels
// through internal/testutil, which reaches internal/procname again
// (testutil → config → procname), so an in-package TestMain is refused with
// "import cycle not allowed in test". Go permits the cycle for an external test
// package, and the two packages share one test binary, so identity_test.go's
// in-package helpers still compile alongside it.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "procname")
}
