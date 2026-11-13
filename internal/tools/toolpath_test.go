package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Named, AUDITABLE skips for the two EXTERNAL binaries the search tools shell
// out to: ctags (code_search's symbol index) and rg (keyword_search). Both are
// host packages, not repo deliverables, so a host that lacks one cannot be made
// green by any change in this repository — the choice is a SKIP THAT NAMES ITS
// REASON or a red that drowns every real regression.
//
// THE CONVENTION IS NOT NEW AND THIS FILE DOES NOT INVENT ONE: 25+ sites in this
// package and cmd/agent already write `t.Skip("ctags not installed")` /
// `t.Skip("rg not installed")` (search_test.go, symbol_test.go, torn_index_test.go,
// corrupt_index_test.go, search_withtimeout_test.go, cli_code_search_test.go,
// cli_code_search_corrupt_test.go). What those sites DID NOT COVER is the family
// of tests that reach the same external binary through a path they forgot to
// guard — measured 2026-10-11 on host nick-sager, where `make test-budget
// PKGS='./internal/tools ./cmd/agent'` reported 8 such tests, every one of them
// red with `exec: "ctags": executable file not found in $PATH`:
//
//	internal/tools: TestCodeSearchSurvivesEOFProbe,
//	                TestQueryDefinitionHealsMidEntryTornIndex,
//	                TestQueryDefinitionDoesNotReadAMissOffATornIndex,
//	                TestQueryDefinitionSelfHealsUnsearchableIndex,
//	                TestCodeSearchWithTimeout_ShortTimeoutNormal,
//	                TestSearchWithTimeout_NoMatchReturnsEmptyNotError
//	cmd/agent:      TestCodeSearchBackendNoSymbol,
//	                TestKeywordSearchRunPrintsHitsAndNoMatches,
//	                TestSearchWrongGroundExitCodeOnTheRealBinary (3 subtests)
//
// NONE OF THOSE IS A WEAKENED GATE. A skipped test is VISIBLE in the stream JSON
// as `"Action":"skip"` with this exact reason, so the suite still declares what it
// did not run; what it stops doing is claiming a REGRESSION where the host is
// simply missing a package. The distinction this preserves is the one the
// acceptance turns on: after the fix the only residual reds are "this host has no
// <binary>", each naming its binary.
//
// WHAT IS DELIBERATELY NOT DONE HERE: nothing is stubbed, mocked or shimmed to
// make a guarded test "still run". The tests that DO exercise the tool path
// without a host binary keep their own shims (testCtagsShim / testTornCtagsShim
// install a stand-in `ctags` and are NOT skipped) — that coverage is untouched.

// requireCtags skips the calling test, NAMING the missing binary, when ctags is
// not on PATH. It is the one spelling of that skip for this package.
func requireCtags(t *testing.T) {
	t.Helper()
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
}

// requireRipgrep is requireCtags' sibling for ripgrep, the keyword_search
// engine. It is separate because the two binaries go missing INDEPENDENTLY
// (measured on this host: `which ctags` and `which rg` are both empty, while
// other hosts have one and not the other), and a test that needs only one of
// them must not be parked by the other's absence.
func requireRipgrep(t *testing.T) {
	t.Helper()
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
}

// toolPathProbeEnvName selects which helper the PROBE CHILD exercises. The child
// is this same test binary re-executed with the variable set, and its stdout is
// the evidence (see TestToolSkipIsConditionalOnPath).
const (
	toolPathProbeEnvName  = "GOPI_TEST_TOOLPATH_PROBE"
	toolPathProbeCtags    = "ctags"
	toolPathProbeRipgrep  = "rg"
	toolPathProbeTestName = "TestToolPathProbeChild"
	// toolPathSkipMarker is the line the `go test` runner prints for a skipped
	// test — the child's observable verdict, and the reason this guard needs a
	// child at all: `testing` exposes no way for a parent to ask "did that
	// subtest skip?", so the answer is read off the runner's own output.
	toolPathSkipMarker = "--- SKIP: " + toolPathProbeTestName
)

// TestToolPathProbeChild is the probe the guard below drives: it calls the
// requested helper and, if the helper RETURNS instead of skipping, FAILS. That
// is the one property a parent cannot observe in-process and the child exists to
// make observable — a helper that forgot to skip would otherwise leave the test
// to run on and die with `exec: "ctags": executable file not found`.
func TestToolPathProbeChild(t *testing.T) {
	switch os.Getenv(toolPathProbeEnvName) {
	case toolPathProbeCtags:
		requireCtags(t)
	case toolPathProbeRipgrep:
		requireRipgrep(t)
	default:
		t.Skip("not the probe child")
	}
	t.Errorf("%s returned instead of skipping — the skip is not wired to the PATH decision",
		os.Getenv(toolPathProbeEnvName))
}

// TestToolSkipIsConditionalOnPath is the guard on requireCtags/requireRipgrep:
// the skip must be a statement about THIS HOST'S PATH, and it must go BOTH ways.
//
// Why a CHILD PROCESS: the two failure modes are "never skips" (the
// host-dependent red stays) and "always skips" (the test is deleted while
// claiming to pass), and neither is visible from inside the test binary —
// `testing` gives a parent no way to ask whether a subtest skipped. Re-executing
// this binary with a controlled PATH and reading the runner's own `--- SKIP`
// line is the measurement: a skip the runner reports is a skip, by definition.
//
// The reachable arm builds a fixture directory holding executables named `ctags`
// and `rg`, so the guard measures the mechanism rather than a string.
func TestToolSkipIsConditionalOnPath(t *testing.T) {
	fixture := t.TempDir()
	for _, name := range []string{toolPathProbeCtags, toolPathProbeRipgrep} {
		if err := os.WriteFile(filepath.Join(fixture, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	empty := t.TempDir()

	// Arm 1 — the binary is absent: the helper MUST skip. `empty` is a directory
	// with nothing in it, so this is a measurement of the helper and not of the
	// ambient PATH of whatever host runs the suite.
	for _, probe := range []string{toolPathProbeCtags, toolPathProbeRipgrep} {
		out := toolPathRunProbe(t, probe, empty)
		if !strings.Contains(out, toolPathSkipMarker) {
			t.Errorf("with %s absent the probe child did not skip — %s would run on and fail with "+
				"`exec: %q: executable file not found in $PATH`:\n%s", probe, probe, probe, out)
		}
	}

	// Arm 2 — the binary IS reachable: the helper must NOT skip (the child then
	// fails on its own guard line, which is the property being asserted: it got
	// PAST the helper). A helper that skipped unconditionally would park every
	// test on every host and this arm is what catches it.
	for _, probe := range []string{toolPathProbeCtags, toolPathProbeRipgrep} {
		out := toolPathRunProbe(t, probe, fixture)
		if strings.Contains(out, toolPathSkipMarker) {
			t.Errorf("with a fixture %q on PATH the probe child SKIPPED — the guard would drop this "+
				"test's coverage on every host:\n%s", probe, out)
		}
		if !strings.Contains(out, "returned instead of skipping") {
			t.Errorf("with a fixture %q on PATH the child neither skipped nor reached its guard line — "+
				"the probe did not run:\n%s", probe, out)
		}
	}
}

// toolPathRunProbe runs the probe child once with PATH set to dir (plus nothing
// else: the arm must not inherit whatever the host happens to have installed) and
// returns its combined output. A child that could not start is reported, never
// silently treated as "did not skip" (Rule 15b).
func toolPathRunProbe(t *testing.T, probe, dir string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+toolPathProbeTestName+"$", "-test.v")
	cmd.Env = append(os.Environ(),
		toolPathProbeEnvName+"="+probe,
		"PATH="+dir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		t.Fatalf("probe child (%s, PATH=%s) could not run: %v", probe, dir, err)
	}
	return string(out)
}
