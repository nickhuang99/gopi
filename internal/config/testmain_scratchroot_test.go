package config_test

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// This file is an EXTERNAL test package (config_test) on purpose: the
// internal test files are in the same package as the code, and testutil
// imports this package (testutil/session.go), so an internal test file would
// close an import cycle. An external test package has no such constraint.
//
// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run on the / filesystem (the /tmp ban; every t.TempDir() here
// would otherwise land in /tmp — the 87%-full partition behind the
// 2026-09-11 exhaustion). See internal/testutil/scratchroot.go and
// devdoc/tmp-ban-scratch-root.md.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "config")
}
