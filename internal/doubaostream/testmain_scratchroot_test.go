package doubaostream

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run when it would fall on the / filesystem (Rule 22 — every
// t.TempDir() here would otherwise land in /tmp).
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "doubaostream")
}
