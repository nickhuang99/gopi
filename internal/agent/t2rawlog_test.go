package agent

import (
	"os"
	"testing"
)

// mustReadFile reads a whole file, failing the test on error. Used by the
// T2 evidence tests to assert framework injections appear as real events in
// the session raw log (acceptance evidence beyond in-memory state).
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
