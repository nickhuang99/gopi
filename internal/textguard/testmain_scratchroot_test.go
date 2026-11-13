package textguard

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (the /tmp ban; any t.TempDir() here would
// otherwise land in /tmp — the 86%-full partition behind the 2026-09-11
// exhaustion). See internal/testutil/scratchroot.go.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "textguard")
}
