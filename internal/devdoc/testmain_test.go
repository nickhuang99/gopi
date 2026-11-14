package devdoc

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// refuses to run on the `/` filesystem — the standing /tmp ban (Rule 22). This
// package's tests write real files (a manifest, a document with a banner) through
// t.TempDir(), and an unpinned suite lands them on the ambient temp root, which
// on a default host is `/tmp` on the boot device (the 2026-09-11 exhaustion).
//
// It is not decoration: internal/standards's scripts gate reads this package as
// an in-tree package that creates scratch directories, and requires the pin.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "devdoc")
}
