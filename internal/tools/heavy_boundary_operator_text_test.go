package tools

// heavy_boundary_operator_text_test.go — the operator text's own guard.
//
// The manual says heavy tests did NOT run BY DESIGN, and that the authority is
// the operator's declaration which the Makefile's opt-in target sets. Both
// halves are claims about OTHER files, so this test reads them:
//
//   - the text must say the heavy set did not run and must not present that as a
//     failure to repair;
//   - it must name the make target;
//   - and that target must actually set the declaration the boundary requires
//     (internal/heavytest.HumanTerminalEnv), plus the opt-in — otherwise the
//     manual would prescribe a route that refuses itself, which is worse than
//     saying nothing.
//
// This is the drift guard for prose: a rename on either side turns the build red
// instead of silently teaching every future verifier a dead route.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/smokegate"
)

// heavyMakeTargetName is the one named opt-in target (the Makefile's, not a
// second spelling invented here).
const heavyMakeTargetName = "test-heavy"

// TestOperatorTextTellsTheVerifierHeavyTestsDidNotRun pins the boundary's
// visibility in the merge/verify briefing.
func TestOperatorTextTellsTheVerifierHeavyTestsDidNotRun(t *testing.T) {
	text := heavyBoundaryOperatorText(t)
	for _, want := range []string{
		"HEAVY TESTS DID NOT RUN, BY DESIGN",
		"by design", // the reason, not just the fact
		"REFUSED",
		heavyMakeTargetName,
		"Rule 27",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the operator text for the merge/verify lane does not carry %q — a verifier who "+
				"cannot see the boundary reads a skipped heavy test as an unrun gate (the text must "+
				"say heavy tests did NOT run by design and what runs them)", want)
		}
	}
	// The text must still keep the smoke path BRANCH-SCOPED: the heavy paragraph
	// is an addition to the existing instruction, never a replacement for it.
	if !strings.Contains(text, smokegate.Command) {
		t.Errorf("the operator text no longer names the smoke command %q", smokegate.Command)
	}
}

// TestTheDocumentedOperatorRouteActuallyEnablesHeavy pins the manual to the
// Makefile: the target the text names must set the declaration the boundary
// demands. A manual that prescribes a self-refusing route is the silent no-op
// this whole change exists to prevent.
func TestTheDocumentedOperatorRouteActuallyEnablesHeavy(t *testing.T) {
	root := heavyRepoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	var body string
	inTarget := false
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "\t") {
			inTarget = line == heavyMakeTargetName+":"
			continue
		}
		if inTarget {
			body += line + "\n"
		}
	}
	if strings.TrimSpace(body) == "" {
		t.Fatalf("the Makefile has no %q target — the operator route the briefings prescribe is gone",
			heavyMakeTargetName)
	}
	for _, want := range []string{heavytest.HumanTerminalEnv + "=1", heavytest.EnvName + "=1"} {
		if !strings.Contains(body, want) {
			t.Errorf("`make %s` does not set %s — the documented operator route would be REFUSED by the "+
				"boundary (it needs both the opt-in and the terminal declaration), so every verifier "+
				"following the manual would hit a dead end", heavyMakeTargetName, want)
		}
	}
}

// heavyBoundaryOperatorText reads the merge/verify lane's briefing SOURCE
// (internal/tools/verify_gate.go, the file that renders it) so the text's claims
// can be read rather than trusted.
func heavyBoundaryOperatorText(t *testing.T) string {
	t.Helper()
	path := filepath.Join(heavyRepoRoot(t), "internal", "tools", "verify_gate.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(src)
}

// heavyRepoRoot resolves the worktree this package lives in.
func heavyRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(wd, "..", ".."))
	if err != nil {
		t.Fatalf("abs repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Makefile")); err != nil {
		t.Fatalf("repo root %s has no Makefile (%v)", root, err)
	}
	return root
}
