package standardgate

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root off the / partition (Rule 22): the
// gate proofs build fixture trees with t.TempDir(), which otherwise lands on
// the ambient temp root (/tmp on this host, the 87%-full boot partition behind
// the 2026-09-11 exhaustion).
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "standards")
}
