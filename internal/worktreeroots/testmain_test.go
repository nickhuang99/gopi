package worktreeroots_test

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem — the standing /tmp ban (Rule 22): the
// policy tests below create fixtures, and an unpinned suite would land them on
// the boot device (the 2026-09-11 exhaustion). See
// internal/testutil/scratchroot.go and devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "worktreeroots")
}
