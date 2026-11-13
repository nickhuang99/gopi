package devworkspace

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (the /tmp ban, Rule 22). Every fixture in
// devworkspace_test.go, ground_test.go and trace_test.go builds a real git
// repository under t.TempDir(), so without this pin all of that scratch data
// lands on the ambient temp root — /tmp on this host, the 87%-full / partition
// behind the 2026-09-11 exhaustion. internal/testutil's pin gate
// (TestEveryTempUsingSuiteIsPinned, pin_coverage_test.go:110) and the coverage
// table beside it (pin_coverage_test.go:112) fail any temp-using package whose
// TestMain does not route through it, so the pin is the fix — never a permit
// row, a skip or a baseline edit.
//
// The pin is also the notify/ledger sandbox installer (internal/testutil's
// suiteenv.go), so a hand-rolled TestMain here would read the operator's live
// settings.json. See internal/testutil/scratchroot.go and
// devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "devworkspace")
}
