// The EXTERNAL test package on purpose: internal/testutil reaches
// internal/rawlogfmt (testutil → scratch → rawlogfmt), so a TestMain in the
// in-package `rawlogfmt` test files would be an import cycle ("import cycle not
// allowed in test"). A TestMain declared in `rawlogfmt_test` links into the
// SAME test binary, so it pins the same scratch root, and the dependency runs
// one way only (rawlogfmt_test → testutil → rawlogfmt).
package rawlogfmt_test

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (the /tmp ban, Rule 22). Without it the
// t.TempDir() fixtures in rawlogfmt_test.go (the reader, the truncation and the
// shape-table arms) land on the ambient temp root — /tmp on this host, a
// directory on the / partition behind the 2026-09-11 exhaustion.
// internal/testutil's pin gate (TestEveryTempUsingSuiteIsPinned, and the
// coverage table beside it) fails any temp-using package whose TestMain does not
// route through it.
//
// The pin is also the notify-env sandbox installer (testutil/suiteenv.go), so a
// suite with a hand-rolled TestMain would read the operator's live
// settings.json. See internal/testutil/scratchroot.go and
// devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "rawlogfmt")
}
