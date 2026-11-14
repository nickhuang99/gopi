package doubaoasr

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run when it would fall on the / filesystem (the /tmp ban —
// every t.TempDir() here would otherwise land in /tmp, the partition behind
// the 2026-09-11 exhaustion). See internal/testutil/scratchroot.go and
// devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "doubaoasr")
}
