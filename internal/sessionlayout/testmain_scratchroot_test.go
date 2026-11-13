package sessionlayout_test

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root OFF the / partition: the tests below
// create their fixtures with t.TempDir(), which would otherwise land in /tmp
// (a directory on /, the 87%-full partition behind the 2026-09-11 exhaustion).
// See internal/testutil/scratchroot.go and devdoc/tmp-ban-scratch-root.md
// (PROJECT.md Rule 22).
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "sessionlayout")
}
