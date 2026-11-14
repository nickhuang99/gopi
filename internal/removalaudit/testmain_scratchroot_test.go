package removalaudit

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (Rule 22; any t.TempDir() here would
// otherwise land in /tmp — a directory on the / partition behind the
// 2026-09-11 exhaustion). See internal/testutil/scratchroot.go and
// devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "removalaudit")
}
