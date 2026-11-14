package reposource

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (the /tmp ban, Rule 22). Without it the
// t.TempDir() fixtures in reposource_test.go (TestFromCwd and the
// no-repository case) land on the ambient temp root — /tmp on this host, a
// directory on the 87%-full / partition behind the 2026-09-11 exhaustion.
// internal/testutil's pin gate (TestEveryTempUsingSuiteIsPinned, and the
// coverage table beside it in pin_coverage_test.go) fails any temp-using
// package whose TestMain does not route through it, naming this package.
//
// The pin is also the notify-env sandbox installer (suiteenv.go), so a suite
// with a hand-rolled TestMain would read the operator's live settings.json.
// It is additive and test-only: it changes no production code and it does not
// touch the gate (no permit row, no baseline entry, no skip).
// See internal/testutil/scratchroot.go and devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "reposource")
}
