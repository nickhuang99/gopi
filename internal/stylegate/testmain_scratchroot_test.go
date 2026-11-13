package stylegate

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this package's scratch root through internal/testutil: the
// scanner and its tests create temp dirs, and unpinned scratch lands on the
// ambient temp root (/tmp — the 87%-full partition behind the 2026-09-11
// exhaustion). See devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "stylegate")
}
