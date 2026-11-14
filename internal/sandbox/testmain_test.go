package sandbox

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the `/` filesystem (Rule 22). It is not optional: this
// suite drives the REAL initializer, which links the SHARED sample database
// (testutil.SampleDBPath) — and that helper refuses an unpinned suite by design,
// because an unpinned fixture would create its database in the ambient temp root
// (`/tmp` on a default host, the boot partition).
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "sandbox")
}
