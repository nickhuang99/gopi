package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Named, AUDITABLE skips for the two EXTERNAL binaries the search verbs shell
// out to: ctags (code_search's symbol index) and rg (keyword_search).
//
// THE CONVENTION IS THE REPOSITORY'S OWN, not a new one: this package already
// writes `t.Skipf("ctags not installed: %v", err)` (cli_code_search_corrupt_test.go)
// and `t.Skipf("ctags not available in this environment: %v", err)`
// (cli_code_search_test.go). What those did not cover is the trio that reaches
// the SAME binary through a path nobody guarded — measured 2026-10-11 on host
// nick-sager, `make test-budget PKGS='./internal/tools ./cmd/agent'`:
//
//	TestCodeSearchBackendNoSymbol      exec: "ctags": executable file not found in $PATH
//	TestKeywordSearchRunPrintsHitsAndNoMatches  exec: "rg": executable file not found
//	TestSearchWrongGroundExitCodeOnTheRealBinary (3 subtests, same two causes)
//
// A skip here is not a weakened gate: it is VISIBLE in the stream JSON as
// `"Action":"skip"` carrying this reason, so the suite still declares what it did
// not run — it just stops reporting a missing host PACKAGE as a REGRESSION. The
// guarded tests keep every assertion they had; what is dropped is the claim that
// a host without the binary is this repository's regression.
//
// The sibling of this file in internal/tools carries the fuller note; the guard
// below is deliberately the SAME shape in both packages (Rules 13/14): one
// mechanism, two spellings of the same contract.

// requireCtags skips the calling test, naming the binary, when ctags is missing.
func requireCtags(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ctags"); err != nil {
		t.Skip("ctags not installed")
	}
}

// requireRipgrep is requireCtags' sibling for ripgrep. Separate because the two
// go missing independently — this host has neither, another has one of them — and
// a test that needs only one must not be parked by the other's absence.
func requireRipgrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
}

// toolPathProbeEnvName selects which helper the probe child exercises (the child
// is this test binary, re-executed with the variable set).
const (
	toolPathProbeEnvName  = "GOPI_TEST_TOOLPATH_PROBE"
	toolPathProbeCtags    = "ctags"
	toolPathProbeRipgrep  = "rg"
	toolPathProbeTestName = "TestToolPathProbeChild"
	// toolPathSkipMarker is the runner's own skip line for the child — the
	// observable verdict, and the reason a child is needed at all: `testing` gives
	// a parent no way to ask whether a subtest skipped.
	toolPathSkipMarker = "--- SKIP: " + toolPathProbeTestName
)

// TestToolPathProbeChild calls the requested helper and FAILS if it returns
// instead of skipping — the one property the parent cannot observe in-process.
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

// TestToolPathSkipIsConditionalOnPath guards both helpers against both wrong
// directions: a helper that never skips (the host-dependent red stays) and a
// helper that always skips (the test is deleted while claiming to pass). PATH is
// pinned per arm, so each verdict is a measurement of the helper rather than of
// whatever the host happens to have installed.
func TestToolPathSkipIsConditionalOnPath(t *testing.T) {
	fixture := t.TempDir()
	for _, name := range []string{toolPathProbeCtags, toolPathProbeRipgrep} {
		if err := os.WriteFile(filepath.Join(fixture, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	empty := t.TempDir()

	for _, probe := range []string{toolPathProbeCtags, toolPathProbeRipgrep} {
		out := toolPathRunProbe(t, probe, empty)
		if !strings.Contains(out, toolPathSkipMarker) {
			t.Errorf("with %s absent the probe child did not skip — %s would run on and fail with "+
				"`exec: %q: executable file not found in $PATH`:\n%s", probe, probe, probe, out)
		}
	}
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

// toolPathRunProbe runs the probe child once with PATH set to dir alone, and
// returns its combined output. A child that could not start is reported, never
// silently read as "did not skip" (Rule 15b).
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
