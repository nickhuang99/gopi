package smokegate

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (the /tmp ban, Rule 22). Without it the
// t.TempDir() fixtures in smokegate_test.go land on the ambient temp root —
// /tmp on this host, a directory on the / partition behind the 2026-09-11
// exhaustion. internal/testutil's pin gate (TestEveryTempUsingSuiteIsPinned,
// and TestPinCoverageTable's per-package PIN/MISSING table) fails any
// temp-using package whose TestMain does not route through it.
//
// Backfill, not a regression fix: the gate landed after this package, so the
// two gates above were RED at pristine master tip for exactly this reason. The
// fix is the ONE entry point every other suite already shares (capturesurvey,
// cgroupscope, checkpoint, heavytest …) — no hand-rolled TestMain, so the pin
// also installs the notify-env sandbox (suiteenv.go): a suite with its own
// TestMain would read the operator's live settings.json.
//
// See internal/testutil/scratchroot.go and devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "smokegate")
}
