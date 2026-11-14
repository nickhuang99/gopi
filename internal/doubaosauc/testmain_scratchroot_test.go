package doubaosauc

// testmain_scratchroot_test.go pins this suite's scratch root into the shared
// scratch home and REFUSES to run when it would fall on the / filesystem
// (Rule 22 — every t.TempDir() here would otherwise land in /tmp). It is the
// same one-line contract every suite in this repo carries, through the same
// helper.

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "doubaosauc")
}
