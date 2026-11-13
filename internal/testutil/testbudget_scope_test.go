package testutil

// The STREAM-SCOPE half of the shared-artifact fix (Rules 1a/14/15): the
// predicate that decides whether a `go test -json` stream carries a package the
// run was not asked for.
//
// WHY IT IS TESTED DYNAMICALLY, AND WHY THAT IS THE POINT
// ------------------------------------------------------
// The defect (measured 2026-09-19) was a shared write target: every budget run
// redirected its stream into ONE host-wide file, so a run's summary could be
// rendered from a stream holding a neighbour's events (one file held 2422
// cmd/agent events followed by 1328 internal/tools events). A reporter that
// waits for a real concurrency accident to be exercised is not a guard; the
// predicate therefore lives in scripts/lib/stream-scope.sh and is called here
// with SYNTHETIC streams, so the mingled case is asserted directly.
//
// The tests below execute the library with bash — the same interpreter the
// script uses — rather than re-implementing it in Go: a Go copy would be the
// second definition the fix exists to remove (Rule 14).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// budgetScopeCase is one (stream, scope) → expected-anomalies case.
type budgetScopeCase struct {
	name  string
	scope []string // package patterns the run was asked for
	// streamPkgs are the packages the stream carries.
	streamPkgs []string
	// wantAnomalous are the packages the predicate must report, in sorted order.
	wantAnomalous []string
}

// TestBudgetStreamScopePredicateNamesTheMingledCase is the dynamic half of the
// scope check: it runs the shared library on synthetic streams and pins both
// directions — a stream within scope is silent, and a stream carrying a
// neighbour's package names it.
func TestBudgetStreamScopePredicateNamesTheMingledCase(t *testing.T) {
	cases := []budgetScopeCase{
		{
			name:       "in-scope single package",
			scope:      []string{"./" + strings.TrimPrefix(budgetScopeFixturePkg, budgetScopeFixtureModule+"/") + "/"},
			streamPkgs: []string{budgetScopeFixturePkg},
		},
		{
			// The measured defect in miniature: one run asked for tools, the
			// stream also holds the neighbour's agent events.
			name:          "mingled with a neighbour's package",
			scope:         []string{"./internal/tools/"},
			streamPkgs:    []string{budgetScopeFixturePkg, budgetScopeForeignPkg},
			wantAnomalous: []string{budgetScopeForeignPkg},
		},
		{
			name:       "whole-module scope covers everything in the module",
			scope:      []string{"./..."},
			streamPkgs: []string{budgetScopeFixturePkg, budgetScopeForeignPkg},
		},
		{
			// A `-run` narrowing must not be mistaken for a package pattern.
			name:       "flag values are not package patterns",
			scope:      []string{"-run", "TestSomething", "./internal/tools/"},
			streamPkgs: []string{budgetScopeFixturePkg},
		},
		{
			// Another module's package is outside every scope by definition —
			// in a real run that is a stale binary or a vendor/build-mode
			// change, never evidence about THIS stream. Reporting it would be
			// a warning that cries wolf (a reader learns to ignore it).
			name:       "another module's package is not evidence of mingling",
			scope:      []string{"./internal/tools/"},
			streamPkgs: []string{budgetScopeFixturePkg, budgetScopeOtherModulePkg},
		},
		{
			name:          "an unrelated sibling package is named",
			scope:         []string{"./internal/tools/"},
			streamPkgs:    []string{budgetScopeForeignPkg},
			wantAnomalous: []string{budgetScopeForeignPkg},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, err := repoRootFromCwd()
			if err != nil {
				t.Fatalf("locate repo root: %v", err)
			}
			dir := t.TempDir()
			streamFile := filepath.Join(dir, "fixture-stream.json")
			scopeFile := filepath.Join(dir, "fixture-scope.txt")
			var stream []byte
			for _, pkg := range tc.streamPkgs {
				stream = append(stream, []byte(sprintfJSONLine(pkg, "TestFixture"))...)
			}
			if err := os.WriteFile(streamFile, stream, 0o644); err != nil {
				t.Fatalf("write fixture stream: %v", err)
			}

			libPath := filepath.Join(root, filepath.FromSlash(budgetStreamScopeLib))
			// One bash call performs exactly what the script performs: derive
			// the scope from the patterns, read the stream's packages, compare.
			// The three file paths travel in the ENVIRONMENT (their names come
			// from the constants, so the shell and the Go side cannot drift) and
			// the scope patterns in "$@" — never as fixed positional args,
			// because a pattern may itself be a flag pair (`-run TestX`) and
			// would then shift the file positions (a stray `TestX` file in the
			// test's working directory is how that was caught).
			scopeScript := fmt.Sprintf(
				`. "$1" && gopi_scope_packages "$2" "${@:3}" >"$%s" && `+
					`gopi_stream_packages "$%s" >"$%s" && `+
					`gopi_stream_scope_anomalies "$2" "$%s" "$%s"`,
				budgetScopeFileEnv, budgetStreamFileEnv, budgetStreamPkgsEnv,
				budgetScopeFileEnv, budgetStreamPkgsEnv)
			args := append([]string{"-c", scopeScript, "bash", libPath, budgetScopeFixtureModule}, tc.scope...)
			cmd := exec.Command(budgetShellBin, args...)
			cmd.Env = append(os.Environ(),
				budgetScopeFileEnv+"="+scopeFile,
				budgetStreamFileEnv+"="+streamFile,
				budgetStreamPkgsEnv+"="+filepath.Join(dir, "stream-pkgs.txt"),
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("run the scope predicate: %v\nargs=%q\nenv=%q %q %q\n%s", err, args,
					scopeFile, streamFile, filepath.Join(dir, "stream-pkgs.txt"), out)
			}
			got := normaliseLines(string(out))
			if strings.Join(got, " ") != strings.Join(tc.wantAnomalous, " ") {
				t.Errorf("anomalies = %v, want %v\nstream=%v scope=%v\noutput:\n%s",
					got, tc.wantAnomalous, tc.streamPkgs, tc.scope, out)
			}
		})
	}
}

// TestBudgetScopeCheckVerbReportsMinglingThroughTheScript is the end-to-end half:
// it runs the script's own `--scope-check` verb (the repository's declared exit
// code for "the stream carries out-of-scope packages") over a synthetic stream,
// so the wiring between the shipped verb and the predicate is exercised too —
// a library that is correct but unreachable from the entry point would be a
// guard nobody runs.
func TestBudgetScopeCheckVerbReportsMinglingThroughTheScript(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.json")
	mingled := filepath.Join(dir, "mingled.json")
	// The fixture module path is the module the predicate compares against; the
	// script reads the REAL module from go.mod, so the synthetic fixture is
	// addressed with the repo's own module path instead: the package it names is
	// a directory the repo does not have, which keeps this test independent of
	// the repo's package list while still exercising both directions.
	repoModule := repoModulePath(t, root)
	inScope := repoModule + "/internal/tools"
	foreign := repoModule + "/internal/agent"
	if err := os.WriteFile(clean, []byte(sprintfJSONLine(inScope, "TestFixture")), 0o644); err != nil {
		t.Fatalf("write clean stream: %v", err)
	}
	if err := os.WriteFile(mingled, []byte(sprintfJSONLine(inScope, "TestFixture")+
		sprintfJSONLine(foreign, "TestNeighbour")), 0o644); err != nil {
		t.Fatalf("write mingled stream: %v", err)
	}

	script := filepath.Join(root, filepath.FromSlash(budgetScriptPath))
	run := func(stream string) (int, string) {
		t.Helper()
		// The scratch home is this test's own directory: the verb must not touch
		// the host's shared artifacts (Rule 22/29), and GOPI_TEST_SCRATCH_HOME is
		// the script's documented knob for it.
		cmd := exec.Command(budgetShellBin, script, "--scope-check", stream, "./internal/tools/")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), budgetScratchEnv+"="+dir)
		out, _ := cmd.CombinedOutput() //stylegate:ignore captures the child's output; the assertions below read the text, so a failure surfaces as an empty buffer
		return cmd.ProcessState.ExitCode(), string(out)
	}
	if code, out := run(clean); code != 0 {
		t.Errorf("--scope-check on an in-scope stream exited %d, want 0\n%s", code, out)
	}
	code, out := run(mingled)
	if code != 4 {
		t.Errorf("--scope-check on a mingled stream exited %d, want 4\n%s", code, out)
	}
	if !strings.Contains(out, budgetStreamScopeMarker) {
		t.Errorf("--scope-check on a mingled stream did not print %q:\n%s", budgetStreamScopeMarker, out)
	}
	if !strings.Contains(out, foreign) {
		t.Errorf("--scope-check output does not name the out-of-scope package %s:\n%s", foreign, out)
	}
}

// budgetStreamScopeMarker is the headline the scope check prints — pinned here
// so a rename cannot silently drop the report (the script's own constant is
// asserted from the other side in assertBudgetStreamScopeAnomalyIsChecked).
const budgetStreamScopeMarker = "STREAM OUT OF SCOPE"

// budgetScratchEnv is the script's documented scratch-home knob (the shell side
// of testutil.ScratchHomeEnvName — kept as the literal the LIBRARY reads,
// because this gate asserts the two agree via the lib's own reader).
const budgetScratchEnv = ScratchHomeEnvName

// budgetScriptPath and budgetStreamScopeLib are declared in
// testbudget_gate_test.go (this file is the dynamic half of the same gate).

// sprintfJSONLine renders one `go test -json` event line without importing fmt
// printf into the assertions (the format lives in one const).
func sprintfJSONLine(pkg, test string) string {
	out := budgetStreamJSONLine
	out = strings.Replace(out, "%s", pkg, 1)
	out = strings.Replace(out, "%s", test, 1)
	return out
}

// normaliseLines splits predicate output into non-empty trimmed lines.
func normaliseLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// repoModulePath reads the module path from the repo's go.mod.
func repoModulePath(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	t.Fatal("go.mod declares no module line")
	return ""
}
