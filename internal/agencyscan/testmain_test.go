package agencyscan

import (
	"os"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// agencyscanSuiteName labels this suite's scratch root.
const agencyscanSuiteName = "agencyscan"

// TestMain pins the suite's scratch root (t-1788361069, 2026-09-11: the
// boot-device fsync ban). Every test here builds throwaway ledger DBs via
// t.TempDir(); without the pin those hundreds of files land on the ambient
// temp root and fsync against the boot device.
func TestMain(m *testing.M) {
	testutil.PinSuiteForTestMain(agencyscanSuiteName)
	code := m.Run()
	testutil.FinishSuiteForTestMain()
	os.Exit(code)
}
