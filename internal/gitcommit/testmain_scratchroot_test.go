package gitcommit

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root through internal/testutil so the
// t.TempDir() call in gitcommit_test.go lands OFF the boot device instead of
// the ambient temp root (/tmp on this host). See devdoc/tmp-ban-scratch-root.md
// and Rule 22 — and note the pin is not optional book-keeping: without it
// internal/testutil's TestPinCoverageTable and TestEveryTempUsingSuiteIsPinned
// both go red for this package (measured 2026-10-31, task
// 01a18281-267b-7bea-8e6e-98a1880694fd: `internal/gitcommit ... NO-PIN`).
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "gitcommit")
}
