package daemonlifetime_test

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (Rule 22; any t.TempDir() here would
// otherwise land in /tmp — a directory on the / partition behind the
// 2026-09-11 exhaustion). See internal/testutil/scratchroot.go and
// devdoc/tmp-ban-scratch-root.md.
//
// It sits in the EXTERNAL test package on purpose: internal/testutil imports
// internal/daemonlifetime (for the fd-binding leak check), so a pin file inside
// package daemonlifetime would make daemonlifetime's test binary import testutil
// and close an import cycle. The gate reads every _test.go in the directory, so
// the external package's TestMain pins the suite just the same — and since a
// directory holds ONE test binary, this pin also covers the in-package tests in
// validate_hold_fd_internal_test.go.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "daemonlifetime")
}
