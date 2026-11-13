package testutil

// The PACKAGE-ATTRIBUTION half of the failures block (Rules 1a/14/15): the
// compiler diagnostics quoted under a package-level failure must be THAT
// package's own, and a package-level verdict must quote its own evidence.
//
// WHY IT EXISTS
// -------------
// The first fix (task 01a12e58, scripts/lib/stream-failures.sh) read the
// failure LIST from the stream's events and named the package-level failures the
// text harvest could not see at all. It resolved the quoted DIAGNOSTICS by a
// substring of the package's last path element:
//
//     gopi_stream_build_output "$raw_json" | grep -F -- "$(basename "$pkg")"
//
// Two packages that share a basename (`p1/tools`, `p2/tools`) both match that
// filter, so each one quotes the OTHER's compiler line as its own evidence
// (measured: devdoc/task report, task 01a12e7a; fixture
// `real-twobroken-same-basename.json` is a verbatim `go test -json ./...`).
// The same block resolved a `package`-kind verdict (a TestMain exit, a panic
// that took the package down) with the WHOLE stream's build output — the
// package's own evidence (`exit status 3`) was printed nowhere.
//
// WHY IT IS TESTED THIS WAY
// -------------------------
// Same shape as testbudget_failures_gate_test.go: the streams are REAL `go test
// -json` captures of small throwaway modules (no test bodies, no device work,
// Rule 27), and the arm runs the REAL script end to end against a stub `go` on
// PATH — cheap and deterministic.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// budgetAttributionTwobrokenStream is `go test -json ./...` over a module with
	// two packages that share the basename `tools` and both fail to compile. The
	// MODULE prefix is substituted at use, so the run's scope check stays silent.
	budgetAttributionTwobrokenStream = `{"ImportPath":"example.test/attribution/p1/tools [example.test/attribution/p1/tools.test]","Action":"build-output","Output":"# example.test/attribution/p1/tools [example.test/attribution/p1/tools.test]\n"}
{"ImportPath":"example.test/attribution/p1/tools [example.test/attribution/p1/tools.test]","Action":"build-output","Output":"p1/tools/a.go:3:23: undefined: missingInP1\n"}
{"ImportPath":"example.test/attribution/p1/tools [example.test/attribution/p1/tools.test]","Action":"build-fail"}
{"Action":"start","Package":"example.test/attribution/p1/tools"}
{"Action":"output","Package":"example.test/attribution/p1/tools","Output":"FAIL\texample.test/attribution/p1/tools [build failed]\n"}
{"Action":"fail","Package":"example.test/attribution/p1/tools","Elapsed":0,"FailedBuild":"example.test/attribution/p1/tools [example.test/attribution/p1/tools.test]"}
{"ImportPath":"example.test/attribution/p2/tools [example.test/attribution/p2/tools.test]","Action":"build-output","Output":"# example.test/attribution/p2/tools [example.test/attribution/p2/tools.test]\n"}
{"ImportPath":"example.test/attribution/p2/tools [example.test/attribution/p2/tools.test]","Action":"build-output","Output":"p2/tools/a.go:3:23: undefined: missingInP2\n"}
{"ImportPath":"example.test/attribution/p2/tools [example.test/attribution/p2/tools.test]","Action":"build-fail"}
{"Action":"start","Package":"example.test/attribution/p2/tools"}
{"Action":"output","Package":"example.test/attribution/p2/tools","Output":"FAIL\texample.test/attribution/p2/tools [build failed]\n"}
{"Action":"fail","Package":"example.test/attribution/p2/tools","Elapsed":0,"FailedBuild":"example.test/attribution/p2/tools [example.test/attribution/p2/tools.test]"}
`
	// budgetAttributionTestMainStream is `go test -json ./c/... ./d/...`: c/thing
	// has a TestMain that exits 3 (a package-level verdict with NO test to name,
	// and no compiler diagnostic at all), d/thing is green.
	budgetAttributionTestMainStream = `{"Action":"start","Package":"example.test/attribution/c/thing"}
{"Action":"output","Package":"example.test/attribution/c/thing","Output":"exit status 3\n"}
{"Action":"output","Package":"example.test/attribution/c/thing","Output":"FAIL\texample.test/attribution/c/thing\t0.002s\n"}
{"Action":"fail","Package":"example.test/attribution/c/thing","Elapsed":0.002}
{"Action":"start","Package":"example.test/attribution/d/thing"}
{"Action":"run","Package":"example.test/attribution/d/thing","Test":"TestOK"}
{"Action":"output","Package":"example.test/attribution/d/thing","Test":"TestOK","Output":"=== RUN   TestOK\n"}
{"Action":"output","Package":"example.test/attribution/d/thing","Test":"TestOK","Output":"--- PASS: TestOK (0.00s)\n"}
{"Action":"pass","Package":"example.test/attribution/d/thing","Test":"TestOK","Elapsed":0}
{"Action":"output","Package":"example.test/attribution/d/thing","Output":"PASS\n"}
{"Action":"output","Package":"example.test/attribution/d/thing","Output":"ok  \texample.test/attribution/d/thing\t0.001s\n"}
{"Action":"pass","Package":"example.test/attribution/d/thing","Elapsed":0.002}
`
	// The module prefix the fixture streams are written against, and the strings
	// that tell one same-basename package's diagnostics from the other's.
	budgetAttributionFixtureModule = "example.test/attribution"
	budgetAttributionP1Line        = "missingInP1"
	budgetAttributionP2Line        = "missingInP2"
	// The package-level evidence a TestMain exit carries and a build failure
	// does not — the line the block must quote for the `package` kind.
	budgetAttributionExitStatus = "exit status 3"
	// The two headings the block prints, used to walk the report by section.
	budgetAttributionBuildHeading   = "BUILD FAILED: "
	budgetAttributionPackageHeading = "PACKAGE FAILED: "
	// budgetAttributionQuoteIndent is the indent of a quoted diagnostic line.
	budgetAttributionQuoteIndent = "      "
	// budgetAttributionSectionGap is how many report lines after a heading still
	// belong to that heading's section (the block quotes at most
	// build_output_report_n lines, plus the heading itself).
	budgetAttributionSectionGap = 12
)

// budgetAttributionReport runs the REAL scripts/test-budget.sh end to end against
// a stub `go` that answers `go test … -json` with stream, and returns the report.
// The pattern is testbudget_failures_gate_test.go's: the script's own path runs
// (stream, summary, failure report), no test bodies and no device work.
func budgetAttributionReport(t *testing.T, stream string) string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	module := repoModulePath(t, root)
	// The stream's packages are the REPO's, so the run's own scope check stays
	// silent and the block under test is the failure report alone.
	stream = strings.ReplaceAll(stream, budgetAttributionFixtureModule, module)
	stubDir := t.TempDir()
	scratchHome := t.TempDir()
	stub := "#!/bin/bash\n" +
		"# Stands in for `go`: `go test … -json` is answered with the fixture\n" +
		"# stream, anything else succeeds (the script's own build probe).\n" +
		"for a in \"$@\"; do\n" +
		"    if [ \"$a\" = \"build\" ]; then\n" +
		"        out=\"\"\n" +
		"        while [ $# -gt 0 ]; do case \"$1\" in -o) out=\"$2\";; esac; shift; done\n" +
		"        [ -n \"$out\" ] && : >\"$out\"\n" +
		"        exit 0\n" +
		"    fi\n" +
		"done\n" +
		"cat <<'FIXTURE_STREAM'\n" + stream + "FIXTURE_STREAM\n" +
		"exit 1\n"
	stubPath := filepath.Join(stubDir, "go")
	if err := os.WriteFile(stubPath, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub go: %v", err)
	}
	cmd := exec.Command(budgetShellBin, filepath.Join(root, filepath.FromSlash(budgetScriptPath)), "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		ScratchHomeEnvName+"="+scratchHome,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, _ := cmd.CombinedOutput() //stylegate:ignore the child's exit code is asserted through the report text, the signal under test
	return string(out)
}

// budgetAttributionSection returns the report lines that belong to the failure
// section whose heading names pkg — the heading line itself plus the indented
// diagnostic lines under it, stopping at the next heading or at the block's own
// closing note. It fails the test when no such heading exists: a section that
// was never rendered is not a pass.
func budgetAttributionSection(t *testing.T, report, heading, pkg string) []string {
	t.Helper()
	lines := strings.Split(report, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "  "+heading) {
			continue
		}
		if !strings.Contains(line, pkg+" ") {
			continue
		}
		end := i + 1 + budgetAttributionSectionGap
		if end > len(lines) {
			end = len(lines)
		}
		var section []string
		for _, l := range lines[i:end] {
			if l != lines[i] && !strings.HasPrefix(l, budgetAttributionQuoteIndent) {
				break
			}
			section = append(section, l)
		}
		return section
	}
	t.Fatalf("the report has no %q section naming %s — the package-level half of the failures "+
		"block did not render:\n%s", heading, pkg, report)
	return nil
}

// TestBudgetBuildDiagnosticsStayWithTheirOwnPackage is the core regression: two
// packages that share a basename both fail to compile, and each one's quoted
// diagnostic must be ITS OWN (a substring filter on `basename $pkg` gives each
// the other's line).
func TestBudgetBuildDiagnosticsStayWithTheirOwnPackage(t *testing.T) {
	report := budgetAttributionReport(t, budgetAttributionTwobrokenStream)
	p1Section := strings.Join(budgetAttributionSection(t, report, budgetAttributionBuildHeading, "/p1/tools"), "\n")
	p2Section := strings.Join(budgetAttributionSection(t, report, budgetAttributionBuildHeading, "/p2/tools"), "\n")

	if !strings.Contains(p1Section, budgetAttributionP1Line) {
		t.Errorf("the p1/tools section does not quote its own compiler diagnostic (%s):\n%s",
			budgetAttributionP1Line, p1Section)
	}
	if strings.Contains(p1Section, budgetAttributionP2Line) {
		t.Errorf("the p1/tools section quotes p2/tools's compiler diagnostic (%s) as its own — the "+
			"diagnostics were selected by the package's basename, which two packages can share:\n%s",
			budgetAttributionP2Line, p1Section)
	}
	if !strings.Contains(p2Section, budgetAttributionP2Line) {
		t.Errorf("the p2/tools section does not quote its own compiler diagnostic (%s):\n%s",
			budgetAttributionP2Line, p2Section)
	}
	if strings.Contains(p2Section, budgetAttributionP1Line) {
		t.Errorf("the p2/tools section quotes p1/tools's compiler diagnostic (%s) as its own:\n%s",
			budgetAttributionP1Line, p2Section)
	}
}

// TestBudgetPackageLevelVerdictQuotesItsOwnEvidence pins the other half: a
// package-level verdict with no compiler diagnostic (a TestMain exiting non-zero)
// must quote its OWN output — the whole stream's build output is somebody else's
// evidence, and for this package there is none at all, so the block printed a
// heading with nothing behind it (Rule 15).
func TestBudgetPackageLevelVerdictQuotesItsOwnEvidence(t *testing.T) {
	report := budgetAttributionReport(t, budgetAttributionTestMainStream)
	section := strings.Join(budgetAttributionSection(t, report, budgetAttributionPackageHeading, "/c/thing"), "\n")
	if !strings.Contains(section, budgetAttributionExitStatus) {
		t.Errorf("the c/thing section does not quote its own evidence (%q) — a package-level verdict "+
			"must NAME what happened:\n%s", budgetAttributionExitStatus, section)
	}
}
