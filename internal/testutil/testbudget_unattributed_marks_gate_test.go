package testutil

// testbudget_unattributed_marks_gate_test.go — THE CLASSIFICATION HALF of the
// failure attribution: a `--- FAIL:` line the RUNNER never attributed to a test
// must not be reported as a failing test, and must not vanish either.
//
// THE DEFECT THIS PINS (measured twice: task 01a12e58, and again on every run
// whose log quotes a `go test` result). A `go test -json` stream carries two
// kinds of `--- FAIL:` text:
//
//  1. the RUNNER's own row — the last line of a test that really failed, always
//     accompanied by `Action=="run"` for that name and an `Action=="fail"` event
//     carrying that Test field;
//  2. a TEST'S OWN TEXT — a passing test asserting on a failure it captured
//     prints the marker into its log. `TestBranchCheckRunQuotesTheFailingPackage`
//     prints `--- FAIL: TestPublishedAnchorResolves`; that name exists only as a
//     fixture string, and the runner started no such test in this run.
//
// The first fix (the event-based failure list) removed kind 2 from the list — and
// with it removed the reader's only account of the row, so a run whose stream
// carries two quoted markers and one real failure reported "failing tests: 1"
// with nothing saying where the other two went (measured on
// run-1793204886-2346679, the run this epic names). A row that reaches the stream
// and reaches no category is a row the reader cannot see is missing (Rule 15).
//
// WHAT IS ASSERTED. The failures list names ONLY the runner's verdicts; the
// quoted row is printed in its OWN block, naming the test that printed it, and
// that block states it is not a failure. So a reader can follow a name they saw
// in the raw stream to its origin in one line, and `grep -c '--- FAIL:'` over the
// report has an explanation for its number.
//
// WHY IT IS TESTED THIS WAY. The readers that decide all of this are the shared
// shell predicates (scripts/lib/stream-failures.sh, Rule 14) — this file never
// re-reads the JSON. The end-to-end arm runs the REAL script
// (scripts/test-budget.sh) against a stub `go`, exactly as
// testbudget_refusal_inline_gate_test.go and testbudget_failures_gate_test.go do:
// cheap, deterministic, no test bodies, no load (Rule 27).
//
// THE RED PROOF. The shipped script is run beside a copy whose unattributed-block
// constants are emptied — the PRE-FIX rendering by construction (the row is
// dropped, which is exactly the behaviour being corrected) — and the arm asserts
// the classification is ABSENT there and present in the shipped script.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// budgetUnattributedFixturePkgTail is the package half of the fixture's
	// stream; the module prefix is the REPO's (read at run time), so the run's
	// own out-of-scope check stays silent.
	budgetUnattributedFixturePkgTail = "/cmd/agent"
	// budgetUnattributedPrintedName is the name a PASSING test prints inside its
	// own log — the row that must be classified as quoted output, never as a
	// failing test. It is deliberately a name no Go file defines, which is the
	// property that made the original defect obvious.
	budgetUnattributedPrintedName = "NotARealTest"
	// budgetUnattributedPrinterName is the PASSING test whose log carries it.
	budgetUnattributedPrinterName = "TestQuotesAFailingTestResult"
	// budgetUnattributedRealName is the test that really fails — the control: the
	// classification must not swallow an ordinary red.
	budgetUnattributedRealName = "TestFailsForItsOwnReason"
	// budgetUnattributedBodyLine is the real failure's own assertion output.
	budgetUnattributedBodyLine = "    fixture_test.go:11: got 1, want 2"
	// budgetUnattributedMarkerLine is what every failing test is rendered with —
	// the shape `go test` prints, kept so a reader still recognises the row.
	budgetUnattributedMarkerLine = "--- FAIL: "
	// budgetUnattributedHeadingConst / …RowFmtConst / …OriginFmtConst are the
	// script's own constants: the gate READS them (Rule 13) rather than copying
	// the text, so a reword cannot leave the gate asserting a string no longer
	// printed.
	budgetUnattributedHeadingConst = "unattributed_heading"
	budgetUnattributedRowFmtConst  = "unattributed_row_fmt"
	budgetUnattributedOriginConst  = "unattributed_origin_fmt"
	// budgetUnattributedLibFn is the shared predicate the report reads; the gate
	// asserts the library still defines it (Rule 14: one definition).
	budgetUnattributedLibFn = "gopi_stream_unattributed_fail_marks"
)

// budgetUnattributedStream is the fixture stream: ONE passing test that PRINTS a
// `--- FAIL: NotARealTest` line, and ONE test that really fails — the two rows a
// text harvest cannot tell apart.
func budgetUnattributedStream(module string) string {
	pkg := module + budgetUnattributedFixturePkgTail
	row := func(action, test, output string) string {
		if output == "" {
			return `{"Action":"` + action + `","Package":"` + pkg + `","Test":"` + test + `"}`
		}
		return `{"Action":"` + action + `","Package":"` + pkg + `","Test":"` + test + `","Output":"` + output + `\n"}`
	}
	return strings.Join([]string{
		`{"Action":"start","Package":"` + pkg + `"}`,
		// The quoted marker: printed by a test the runner DID start, claiming a
		// name the runner did NOT start.
		row("run", budgetUnattributedPrinterName, ""),
		row("output", budgetUnattributedPrinterName, "    gates_test.go:88: the arm quotes its own fixture:"),
		row("output", budgetUnattributedPrinterName, "              "+budgetUnattributedMarkerLine+budgetUnattributedPrintedName+" (0.00s)"),
		row("output", budgetUnattributedPrinterName, "--- PASS: "+budgetUnattributedPrinterName+" (0.00s)"),
		`{"Action":"pass","Package":"` + pkg + `","Test":"` + budgetUnattributedPrinterName + `","Elapsed":0}`,
		// The real verdict.
		row("run", budgetUnattributedRealName, ""),
		row("output", budgetUnattributedRealName, budgetUnattributedBodyLine),
		row("output", budgetUnattributedRealName, budgetUnattributedMarkerLine+budgetUnattributedRealName+" (0.01s)"),
		`{"Action":"fail","Package":"` + pkg + `","Test":"` + budgetUnattributedRealName + `","Elapsed":0.01}`,
		`{"Action":"fail","Package":"` + pkg + `","Elapsed":0.02}`,
	}, "\n") + "\n"
}

// budgetUnattributedRunLib calls one library function with bash — the same
// interpreter the script uses — and returns its output, so the arm measures the
// SHARED predicate rather than a second reading of the stream implemented in Go
// (Rule 14).
func budgetUnattributedRunLib(t *testing.T, root, streamPath, call string) string {
	t.Helper()
	libPath := filepath.Join(root, filepath.FromSlash(budgetFailuresLib))
	out, err := exec.Command(budgetShellBin, "-c", `. "$1" && "$2" "$3"`, "bash", libPath, call, streamPath).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", call, err, out)
	}
	return string(out)
}

// budgetUnattributedReport runs the SHIPPED script against a stub `go` that
// answers with the fixture stream and a failing status, and returns everything it
// printed.
func budgetUnattributedReport(t *testing.T, root, script, stream string) string {
	t.Helper()
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	stubDir := t.TempDir()
	scratchHome := t.TempDir()
	streamPath := filepath.Join(t.TempDir(), "unattributed-stream.json")
	if err := os.WriteFile(streamPath, []byte(stream), 0o644); err != nil {
		t.Fatalf("write the fixture stream: %v", err)
	}
	stub := "#!/bin/bash\n" +
		"# Stands in for `go`: a build succeeds (no artifact is needed — the failure\n" +
		"# readers classify the STREAM, not the binary), and `go test` answers with the\n" +
		"# fixture stream and the failing status a real red run returns.\n" +
		"for a in \"$@\"; do\n" +
		"    if [ \"$a\" = \"build\" ]; then\n" +
		"        out=\"\"\n" +
		"        while [ $# -gt 0 ]; do case \"$1\" in -o) out=\"$2\";; esac; shift; done\n" +
		"        [ -n \"$out\" ] && : >\"$out\"\n" +
		"        exit 0\n" +
		"    fi\n" +
		"done\n" +
		"cat <<'UNATTRIBUTED_STREAM'\n" + stream + "UNATTRIBUTED_STREAM\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(stubDir, testbudgetRunConditionGoBin), []byte(stub), 0o755); err != nil {
		t.Fatalf("write the stub go: %v", err)
	}
	cmd := exec.Command(budgetShellBin, script, "./cmd/agent/")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		ScratchHomeEnvName+"="+scratchHome,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, _ := cmd.CombinedOutput() //stylegate:ignore the exit code is asserted through the report text, which is the signal under test
	return string(out)
}

// budgetUnattributedPreFix is the PRE-FIX rendering, by construction: the
// constants the classification introduced are emptied in a copy of the SHIPPED
// script, so what the red arm runs is the shipped bytes minus the fix — never a
// paraphrase. Emptying the heading and both format strings leaves the block
// printing blank lines while the ROW itself is still produced by the library, so
// the arm additionally removes the block's CALL (its only invocation) to measure
// the behaviour that actually shipped: the row dropped from the report.
func budgetUnattributedPreFix(t *testing.T, root, script string) string {
	t.Helper()
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read %s: %v", script, err)
	}
	text := string(raw)
	for _, name := range []string{
		budgetUnattributedHeadingConst,
		budgetUnattributedRowFmtConst,
		budgetUnattributedOriginConst,
	} {
		re := regexp.MustCompile(`(?m)^` + name + `=.*$`)
		if !re.MatchString(text) {
			t.Fatalf("%s declares no %s=… — the red proof cannot revert what is not there", budgetScriptPath, name)
		}
		text = re.ReplaceAllString(text, "")
	}
	callRe := regexp.MustCompile(`(?m)^[[:space:]]*report_unattributed_fail_marks$`)
	if !callRe.MatchString(text) {
		t.Fatalf("%s no longer calls report_unattributed_fail_marks — the classification this proof reverts is "+
			"not wired in", budgetScriptPath)
	}
	text = callRe.ReplaceAllString(text, "    :")
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("make %s: %v", scriptsDir, err)
	}
	// The script resolves its shared libraries relative to its OWN directory
	// (`$root/scripts/lib/…`), so the copy needs the same neighbour or the
	// missing-library refusal fires before any rendering runs.
	if err := os.Symlink(filepath.Join(root, "scripts", "lib"), filepath.Join(scriptsDir, "lib")); err != nil {
		t.Fatalf("link the shared libraries beside the pre-fix copy: %v", err)
	}
	preFix := filepath.Join(scriptsDir, filepath.Base(script))
	if err := os.WriteFile(preFix, []byte(text), 0o755); err != nil {
		t.Fatalf("write the pre-fix copy: %v", err)
	}
	return preFix
}

// TestTestBudgetDoesNotReportATestPrintedFailMarker is the epic's acceptance arm:
// a test that PRINTS `--- FAIL: NotARealTest` must not produce a failure row,
// the real red must still be listed, and the printed row must be classified as
// unattributed output naming the test that printed it.
func TestTestBudgetDoesNotReportATestPrintedFailMarker(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	script := filepath.Join(root, filepath.FromSlash(budgetScriptPath))
	shipped := budgetSiteReadFile(t, budgetScriptPath)
	lib := budgetSiteReadFile(t, budgetFailuresLib)
	if !strings.Contains(lib, budgetUnattributedLibFn+"()") {
		t.Fatalf("%s no longer defines %s() — the report would have no predicate to classify a printed marker "+
			"with (Rule 14)", budgetFailuresLib, budgetUnattributedLibFn)
	}
	heading := budgetReportPipeStringConst(t, shipped, budgetUnattributedHeadingConst)
	if strings.TrimSpace(heading) == "" {
		t.Fatalf("%s: %s is empty — the block that explains the printed rows would print nothing",
			budgetScriptPath, budgetUnattributedHeadingConst)
	}
	module := repoModulePath(t, root)
	pkg := module + budgetUnattributedFixturePkgTail
	stream := budgetUnattributedStream(module)

	// (0) THE LIBRARY, measured directly: the printed name is not in the failure
	// list, and IS in the unattributed list, attributed to the test that printed
	// it. This half is what makes the report's claim accountable to the stream.
	streamPath := filepath.Join(t.TempDir(), "lib-stream.json")
	if err := os.WriteFile(streamPath, []byte(stream), 0o644); err != nil {
		t.Fatalf("write the fixture stream: %v", err)
	}
	failures := budgetUnattributedRunLib(t, root, streamPath, "gopi_stream_failures")
	if strings.Contains(failures, budgetUnattributedPrintedName) {
		t.Errorf("the failure list names %s — a line a PASSING test printed as its own fixture text was "+
			"harvested as a failure (the measured defect):\n%s", budgetUnattributedPrintedName, failures)
	}
	if !strings.Contains(failures, budgetUnattributedRealName) {
		t.Fatalf("the failure list does not carry the genuinely failing test %s — the fixture is not "+
			"measuring the distinction:\n%s", budgetUnattributedRealName, failures)
	}
	marks := budgetUnattributedRunLib(t, root, streamPath, budgetUnattributedLibFn)
	wantMark := pkg + budgetFailuresMachineSep + budgetUnattributedPrintedName + budgetFailuresMachineSep +
		budgetUnattributedPrinterName
	if !strings.Contains(marks, wantMark) {
		t.Errorf("the unattributed-mark list does not carry %q — the printed row reached no category at all, "+
			"which is the silent-drop half of the defect:\n%s", wantMark, marks)
	}

	// (1) THE REPORT, from the shipped script run end to end.
	report := budgetUnattributedReport(t, root, script, stream)
	plainFake := budgetUnattributedMarkerLine + budgetUnattributedPrintedName + "  (" + pkg + ")"
	if strings.Contains(report, plainFake) {
		t.Errorf("the report lists the printed marker as a failing test (%q):\n%s", plainFake, report)
	}
	plainReal := budgetUnattributedMarkerLine + budgetUnattributedRealName + "  (" + pkg + ")"
	if !strings.Contains(report, plainReal) {
		t.Errorf("the report does not list the genuinely failing test (%q):\n%s", plainReal, report)
	}
	if !strings.Contains(report, heading) {
		t.Errorf("the report does not carry the unattributed block's heading %q — the printed row is dropped "+
			"silently:\n%s", heading, report)
	}
	if !strings.Contains(report, budgetUnattributedPrintedName+"  ("+pkg+")") {
		t.Errorf("the unattributed block does not name the printed marker %q with its package — a reader who "+
			"saw that line in the stream has no account of it:\n%s",
			budgetUnattributedPrintedName+"  ("+pkg+")", report)
	}
	origin := budgetReportPipeStringConst(t, shipped, budgetUnattributedOriginConst)
	originWithName := strings.Replace(origin, `\n`, "", 1)
	originWithName = strings.Replace(originWithName, "%s", "", 1)
	originWithName = strings.Replace(originWithName, "%s", budgetUnattributedPrinterName, 1)
	if !strings.Contains(report, originWithName) {
		t.Errorf("the report does not attribute the printed marker to the test whose log carried it (%q) — the "+
			"origin is the answer to \"did this really fail?\":\n%s", originWithName, report)
	}

	// (2) THE RED PROOF: the pre-fix rendering of the same script must drop the
	// row and name nothing, so the arm measures the classification and not the
	// fixture.
	preFix := budgetUnattributedPreFix(t, root, script)
	reverted := budgetUnattributedReport(t, root, preFix, stream)
	if strings.Contains(reverted, heading) {
		t.Errorf("the PRE-FIX rendering already prints the unattributed block — the red arm is not measuring the "+
			"classification:\n%s", reverted)
	}
	if !strings.Contains(reverted, plainReal) {
		t.Fatalf("the PRE-FIX rendering does not even render the real failure's marker line — the red arm is not "+
			"measuring the rendering difference:\n%s", reverted)
	}
}
