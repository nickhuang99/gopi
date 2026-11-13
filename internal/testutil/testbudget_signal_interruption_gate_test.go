package testutil

// The SIGNAL-INTERRUPTION half of the failures block (Rule 15: a failure must
// name itself, and the name must be TRUE).
//
// WHY IT EXISTS
// -------------
// Bug row 01a164f6, measured 2026-10-23. Two `cmd/agent` budget runs ended with
//
//	failing tests: 0
//	package verdict: FAIL: github.com/nick/gopi/cmd/agent
//	PACKAGE FAILED: github.com/nick/gopi/cmd/agent (a package-level verdict — a
//	  panic or a TestMain exit; no test to name)
//
// and the real cause was neither of those things: the test BINARY had been killed
// by SIGHUP, which `go test` records as a package-level `fail` plus ONE output row
// spelling os/exec's `"signal: " + Signal()` — `signal: hangup`. So the report
// named a false cause for a red nobody had to fix, and the section it named (the
// package-level one) is where a reader goes looking for a panic.
//
// The two shapes the stream really carries, both verbatim:
//
//	package-scoped (minimal repro, `go test -json` on a test that HUPs itself):
//	  {"Action":"output","Package":P,"Output":"signal: hangup\n"}
//	  {"Action":"fail","Package":P,"Elapsed":0.052}
//
//	test-scoped (the real run, run-1792694665-3875354 at 02:51:48):
//	  {"Action":"output","Package":P,"Test":"TestWorkerRoundClaimsWhenNoRequestStands",
//	   "Output":"signal: hangup\n"}
//	  {"Action":"fail","Package":P,"Elapsed":440.462}
//
// WHY IT IS TESTED THIS WAY
// -------------------------
// Same shape as testbudget_package_attribution_gate_test.go: a REAL `go test
// -json` capture (no test bodies, no device work, Rule 27) driven through the
// REAL scripts/test-budget.sh against a stub `go` on PATH — so the assertion is
// about the REPORT a run would print, not about a helper's return value.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// budgetSignalFixturePkg is the package the fixture streams are written
	// against. The MODULE prefix is substituted at use, so the run's own
	// stream-scope check stays silent and the block under test is the signal
	// report alone.
	budgetSignalFixtureModule = "example.test/signaldeath"

	// budgetSignalName is the rendering os/exec produces for SIGHUP —
	// `status.Signal().String()`. It is the token the report must name.
	budgetSignalName = "hangup"

	// budgetSignalPrefix is os/exec's rendering prefix, the constant
	// scripts/lib/stream-failures.sh matches on (stream_signal_prefix).
	budgetSignalPrefix = "signal: "

	// budgetSignalRow is the os/exec line verbatim, as it appears inside the
	// stream's `Output` field (with its JSON newline escape).
	budgetSignalRow = budgetSignalPrefix + budgetSignalName + `\n`

	// budgetSignalMisattribution is the sentence the block used to print for a
	// killed package, and the ONE thing this gate exists to forbid: it is a
	// claim about a panic (or a TestMain exit), and a killed binary is neither.
	budgetSignalMisattribution = "panic or a TestMain exit"

	// The two blocks' headings, used to walk the report by section.
	budgetSignalHeading         = "--- SIGNAL INTERRUPTION"
	budgetSignalPackageHeading  = "  INTERRUPTED: "
	budgetSignalPackageFailures = "--- package-level failures"

	// budgetSignalSectionGap is how many report lines after a heading still
	// belong to that heading's section (the block quotes at most
	// build_output_report_n lines plus its own notes).
	budgetSignalSectionGap = 12
)

// budgetSignalStreamPackageScoped is the MINIMAL repro, captured verbatim from
// `go test -json` over a throwaway module whose single test raises SIGHUP on its
// own process. Nothing else is in it: no panic, no `exit status`, no failing
// assertion — which is exactly why the old classifier had no fact to go on but
// the package-level verdict, and guessed "panic or TestMain".
func budgetSignalStreamPackageScoped() string {
	return strings.Join([]string{
		`{"Action":"start","Package":"` + budgetSignalFixtureModule + `"}`,
		`{"Action":"run","Package":"` + budgetSignalFixtureModule + `","Test":"TestKilledBySignal"}`,
		`{"Action":"output","Package":"` + budgetSignalFixtureModule + `","Test":"TestKilledBySignal","Output":"=== RUN   TestKilledBySignal\n"}`,
		`{"Action":"output","Package":"` + budgetSignalFixtureModule + `","Output":"` + budgetSignalRow + `"}`,
		`{"Action":"output","Package":"` + budgetSignalFixtureModule + `","Output":"FAIL\texample.test/sigdeath\t0.052s\n"}`,
		`{"Action":"fail","Package":"` + budgetSignalFixtureModule + `","Elapsed":0.052}`,
	}, "\n") + "\n"
}

// budgetSignalStreamTestScoped is the REAL run's shape (run-1792694665-3875354):
// the signal row carries a Test field, because the process died while that test
// was live. It is a different row kind after normalisation, and a classifier that
// only read the package-scoped rows would miss the case that actually happened.
//
// It also carries the OTHER tests of that package that PASSED before the kill, so
// the arm can pin that a killed run does not turn its green tests red.
func budgetSignalStreamTestScoped() string {
	return strings.Join([]string{
		`{"Action":"start","Package":"` + budgetSignalFixtureModule + `"}`,
		`{"Action":"run","Package":"` + budgetSignalFixtureModule + `","Test":"TestWorkerStopSignalsIncludeSIGHUP"}`,
		`{"Action":"output","Package":"` + budgetSignalFixtureModule + `","Test":"TestWorkerStopSignalsIncludeSIGHUP","Output":"--- PASS: TestWorkerStopSignalsIncludeSIGHUP (0.00s)\n"}`,
		`{"Action":"pass","Package":"` + budgetSignalFixtureModule + `","Test":"TestWorkerStopSignalsIncludeSIGHUP","Elapsed":0}`,
		`{"Action":"run","Package":"` + budgetSignalFixtureModule + `","Test":"TestWorkerRoundClaimsWhenNoRequestStands"}`,
		`{"Action":"output","Package":"` + budgetSignalFixtureModule + `","Test":"TestWorkerRoundClaimsWhenNoRequestStands","Output":"=== RUN   TestWorkerRoundClaimsWhenNoRequestStands\n"}`,
		`{"Action":"output","Package":"` + budgetSignalFixtureModule + `","Test":"TestWorkerRoundClaimsWhenNoRequestStands","Output":"` + budgetSignalRow + `"}`,
		`{"Action":"output","Package":"` + budgetSignalFixtureModule + `","Output":"FAIL\t` + budgetSignalFixtureModule + `\t440.462s\n"}`,
		`{"Action":"fail","Package":"` + budgetSignalFixtureModule + `","Elapsed":440.462}`,
	}, "\n") + "\n"
}

// budgetSignalStreamPanic is the CONTRAST arm: a real panic. It must KEEP the
// package-level sentence — the fix classifies a signal, it does not weaken the
// report of a genuine panic into "interrupted".
func budgetSignalStreamPanic() string {
	return strings.Join([]string{
		`{"Action":"start","Package":"` + budgetSignalFixtureModule + `"}`,
		`{"Action":"output","Package":"` + budgetSignalFixtureModule + `","Test":"TestPanics","Output":"panic: deliberate [recovered]\n"}`,
		`{"Action":"output","Package":"` + budgetSignalFixtureModule + `","Output":"panic: deliberate\n"}`,
		`{"Action":"output","Package":"` + budgetSignalFixtureModule + `","Output":"FAIL\t` + budgetSignalFixtureModule + `\t0.002s\n"}`,
		`{"Action":"fail","Package":"` + budgetSignalFixtureModule + `","Elapsed":0.002}`,
	}, "\n") + "\n"
}

// budgetSignalReport runs the REAL scripts/test-budget.sh end to end against a
// stub `go` that answers `go test … -json` with stream, and returns the report
// (testbudget_package_attribution_gate_test.go's harness, reused by shape).
func budgetSignalReport(t *testing.T, stream string) string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	module := repoModulePath(t, root)
	stream = strings.ReplaceAll(stream, budgetSignalFixtureModule, module+"-signaldeath")
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

// budgetSignalSection returns the report lines belonging to the section whose
// heading starts with heading, stopping at the next heading. It fails the test
// when no such section rendered: a section that never appeared is not a pass.
func budgetSignalSection(t *testing.T, report, heading string) []string {
	t.Helper()
	lines := strings.Split(report, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, heading) {
			continue
		}
		end := i + 1 + budgetSignalSectionGap
		if end > len(lines) {
			end = len(lines)
		}
		var section []string
		for _, l := range lines[i:end] {
			if l != lines[i] && strings.HasPrefix(l, "--- ") {
				break
			}
			section = append(section, l)
		}
		return section
	}
	t.Fatalf("the report has no %q section — the signal-interruption block did not render:\n%s",
		heading, report)
	return nil
}

// TestBudgetSignalInterruptionIsNotAPanic is THE regression for bug row
// 01a164f6: a stream whose only cause is a killed test binary must be reported as
// an interruption, and the package must NOT be described as "a panic or a
// TestMain exit".
func TestBudgetSignalInterruptionIsNotAPanic(t *testing.T) {
	report := budgetSignalReport(t, budgetSignalStreamPackageScoped())

	section := strings.Join(budgetSignalSection(t, report, budgetSignalHeading), "\n")
	if !strings.Contains(section, budgetSignalName) {
		t.Errorf("the signal section does not name the signal (%q) — a reader cannot tell a hangup "+
			"(a teardown) from a termination (a budget timeout):\n%s", budgetSignalName, section)
	}
	if !strings.Contains(section, budgetSignalPackageHeading) {
		t.Errorf("the signal section does not name the interrupted PACKAGE:\n%s", section)
	}
	// THE EVIDENCE ARM: the claim must carry os/exec's own row under it, not a
	// paraphrase. Measured 2026-10-23 on the replay of the real stream: the
	// section said "killed by hangup" and, because it quoted PACKAGE-scoped
	// output while the real row is TEST-scoped, printed the suite's startup lines
	// under it instead — an assertion with somebody else's evidence.
	if !budgetSignalHasEvidenceRow(section) {
		t.Errorf("the signal section does not quote the verbatim `%s` row from the stream — the "+
			"claim is asserted, not evidenced:\n%s",
			budgetSignalPrefix+budgetSignalName, section)
	}

	// THE MISATTRIBUTION ARM. The sentence is looked for where the old report
	// put it — the package-level block — and there must be no package-level
	// section at all for a run whose only failing package was killed.
	if pkgSection := budgetSignalPackageSectionOrEmpty(report); pkgSection != "" {
		if strings.Contains(pkgSection, budgetSignalMisattribution) {
			t.Errorf("a killed package is still described as %q — the report names a cause that is false "+
				"about a red nobody has to fix (bug row 01a164f6):\n%s",
				budgetSignalMisattribution, pkgSection)
		}
		if strings.Contains(pkgSection, budgetSignalFixtureModule) {
			t.Errorf("the killed package is still listed under the package-level heading, which is where a "+
				"reader goes looking for a panic:\n%s", pkgSection)
		}
	}

	// THE "NOT A REAL FAILURE" ARM: no test is named as failing, and the report
	// says why — the interrupted tests carry no verdict at all.
	if strings.Contains(report, "--- FAIL: ") {
		t.Errorf("the report names a failing TEST for a run in which no test failed (the binary was "+
			"killed):\n%s", report)
	}
	if !strings.Contains(report, "failing tests: 0") {
		t.Errorf("the report does not state `failing tests: 0` for an interrupted run:\n%s", report)
	}
}

// TestBudgetSignalInterruptionTestScopedIsClassified pins the row shape the REAL
// run produced: the `signal:` line carries a Test field. A classifier that only
// read package-scoped rows would let this case fall through to the panic branch —
// i.e. it would fix the minimal repro and leave the measured defect live.
func TestBudgetSignalInterruptionTestScopedIsClassified(t *testing.T) {
	report := budgetSignalReport(t, budgetSignalStreamTestScoped())

	section := strings.Join(budgetSignalSection(t, report, budgetSignalHeading), "\n")
	if !strings.Contains(section, budgetSignalName) {
		t.Errorf("a TEST-scoped signal row was not classified as an interruption (the real run's shape, "+
			"run-1792694665-3875354):\n%s", section)
	}
	// The evidence arm for the TEST-scoped shape specifically: this is the case
	// where a package-scoped quote finds nothing, and printing the section
	// without its row is exactly the defect the replay caught.
	if !budgetSignalHasEvidenceRow(section) {
		t.Errorf("a TEST-scoped signal row is classified but its own `%s` line is not quoted under "+
			"the claim — the section asserts the interruption and shows somebody else's output:\n%s",
			budgetSignalPrefix+budgetSignalName, section)
	}
	if pkgSection := budgetSignalPackageSectionOrEmpty(report); strings.Contains(pkgSection, budgetSignalMisattribution) {
		t.Errorf("a test-scoped signal row still reaches the package-level sentence %q:\n%s",
			budgetSignalMisattribution, pkgSection)
	}
	// The tests that PASSED before the kill stay green: an interrupted run does
	// not retroactively fail what already reported a verdict.
	if strings.Contains(section, "TestWorkerStopSignalsIncludeSIGHUP") {
		t.Errorf("the signal section names a test that PASSED before the kill — no test carries a "+
			"verdict for the interruption:\n%s", section)
	}
}

// TestBudgetPanicKeepsThePackageLevelVerdict is the anti-overreach arm: the fix
// classifies SIGNALS, and a genuine panic must still be reported as one. Without
// this, "never say panic" would be satisfiable by deleting the sentence.
func TestBudgetPanicKeepsThePackageLevelVerdict(t *testing.T) {
	report := budgetSignalReport(t, budgetSignalStreamPanic())
	pkgSection := budgetSignalPackageSectionOrEmpty(report)
	if pkgSection == "" {
		t.Fatalf("a panicking package produced no package-level section at all — the report lost a real "+
			"failure:\n%s", report)
	}
	if !strings.Contains(pkgSection, budgetSignalMisattribution) {
		t.Errorf("a panicking package is no longer described as %q — the signal classification over-reached "+
			"and swallowed the panic arm:\n%s", budgetSignalMisattribution, pkgSection)
	}
	if strings.Contains(report, budgetSignalHeading) {
		t.Errorf("a panicking package was reported as a SIGNAL INTERRUPTION — a panic is not a signal:\n%s", report)
	}
}

// budgetSignalHasEvidenceRow reports whether some line of the section is the
// verbatim os/exec row (`signal: <name>`) — indented quoting allowed, a
// paraphrase not. The wanted text is built from the PREFIX constant's spelling
// plus the signal NAME, never from budgetSignalRow: that constant carries the
// JSON `\n` escape, and the report prints the DECODED line.
func budgetSignalHasEvidenceRow(section string) bool {
	want := budgetSignalPrefix + budgetSignalName
	for _, line := range strings.Split(section, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// budgetSignalPackageSectionOrEmpty returns the package-level section's text, or
// "" when that block did not render.
func budgetSignalPackageSectionOrEmpty(report string) string {
	lines := strings.Split(report, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, budgetSignalPackageFailures) {
			continue
		}
		end := i + 1 + budgetSignalSectionGap
		if end > len(lines) {
			end = len(lines)
		}
		var section []string
		for _, l := range lines[i:end] {
			if l != lines[i] && strings.HasPrefix(l, "--- ") {
				break
			}
			section = append(section, l)
		}
		return strings.Join(section, "\n")
	}
	return ""
}
