package terminalgeom

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root through internal/testutil so any
// t.TempDir()/os.MkdirTemp in this package lands OFF the boot device instead of
// the ambient temp root (/tmp on this host). See devdoc/tmp-ban-scratch-root.md
// and Rule 22.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "terminalgeom")
}
