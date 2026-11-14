package testsuite

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (the /tmp ban). The three-mode E2E smoke
// suite creates GOPI_HOME sandboxes and work directories in bulk (26
// t.TempDir() calls), so without the pin every one of them lands in /tmp — the
// 87%-full partition behind the 2026-09-11 exhaustion. See
// internal/testutil/scratchroot.go and devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "testsuite")
}
