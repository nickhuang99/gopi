package taskreport

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run when the pin cannot engage (PROJECT.md Rule 22: suite scratch
// must be provably OFF the `/` partition — `/tmp` is a directory on `/` on this
// host, the partition the 2026-09-11 exhaustion filled).
//
// Why here: own_test.go builds its fixtures with t.TempDir() (two call sites),
// and TestEveryTempUsingSuiteIsPinned / TestPinCoverageTable (internal/testutil)
// fail any package that creates scratch directories without routing its
// TestMain through testutil.RunSuiteMain. Without the pin those fixtures land on
// the ambient temp root.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "taskreport")
}
