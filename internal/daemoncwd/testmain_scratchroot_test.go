package daemoncwd

import (
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// REFUSES to run when it would fall on the / filesystem (the /tmp ban —
// every t.TempDir() here would otherwise land in /tmp, the partition behind
// the 2026-09-11 exhaustion). See internal/testutil/scratchroot.go and
// devdoc/tmp-ban-scratch-root.md.
//
// ONE ENTRY POINT PER SUITE — the reason this file carries the provenance.
// The package briefly held TWO TestMain definitions, and the whole package
// failed to build ("multiple definitions of TestMain"), which `go build ./...`
// cannot see because test files are not part of it. The sequence, measured:
//
//   - 4a836c0f added daemoncwd/testmain_test.go for the relanded holder tests;
//   - 8f0836cb (the revert of that merge) deleted it together with the holder
//     tests — but NOT the t.TempDir() calls in daemoncwd_test.go, so the static
//     pin gates went red on master;
//   - 740f28a6 correctly re-pinned the suite, under this file's name;
//   - 2bc93022 (the RELAND of the reverted merge, a branch that had merged an
//     OLDER master) restored testmain_test.go — and with it a second TestMain.
//
// A Go test binary links every *_test.go of a directory, whether it declares
// `package daemoncwd` or `package daemoncwd_test`, so two TestMain functions
// collide even across the internal/external split. The gate that now prevents a
// repeat is internal/testutil TestAtMostOneTestMainPerSuite.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "daemoncwd")
}
