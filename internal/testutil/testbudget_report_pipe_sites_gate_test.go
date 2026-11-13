package testutil

// THE REPORT-PIPE GATE, FAILURE-REPORT HALF: the three helpers that render a
// FAILED budget run's evidence may not feed an early-exit consumer either.
//
// WHY IT EXISTS
// -------------
// The green-report half of this defect (list_executed_tests, `… | sort -u |
// head -N`) was fixed and gated in testbudget_report_pipe_gate_test.go. The
// FAILURE REPORT path kept the identical shape in three more places, and a
// reader of a failed run met the noise exactly where it is most expensive — in
// the block whose job is to explain the failure:
//
//	list_failure_tests      done < <(gopi_stream_failures "$raw_json" | head -N)
//	report_package_failures printf '%s\n' "$own" | head -N | sed 's/^/      /'   (×2)
//	fan_out_timeout_marker  grep -a -o -- "$marker…" "$raw_json" | head -3
//
// `head` exits at its cap and closes the pipe; a producer still writing to it
// takes EPIPE/SIGPIPE, and under the script's own `set -uo pipefail` that
// becomes a line in the report — `sort: write failed: … Broken pipe` /
// `grep: write error: Broken pipe` — which reads like a defect of the run under
// report (Rule 15). Each helper now CAPTURES its producer and slices the
// captured text with awk, so no consumer exits early.
//
// WHAT IS ASSERTED, AND WHAT IS DELIBERATELY NOT
// ----------------------------------------------
// Every arm runs the SHIPPED function body (brace-matched out of the script, so
// what runs is the shipped bytes and not a transcription) under the script's own
// `set -uo pipefail`, against a fixture larger than the pipe buffer, and requires
// stderr to be EMPTY. Two of the three arms run the pre-fix SHAPE beside it as a
// POSITIVE CONTROL, so an arm that is quiet only because its fixture is too small
// cannot pass.
//
// Three honest limits, stated because a gate that overstates its coverage is
// worse than one that does not exist (Rule 26 corollary):
//
//  1. The `report_package_failures` control is NOT asserted to be noisy. Its
//     producer is the shell's own `printf` builtin, whose EPIPE bash swallows
//     (measured 2026-10-13: 500 lines into `head -10` gave rc=0 and an EMPTY
//     stderr, where the same shape over external `grep` printed
//     `grep: write error: Broken pipe` and exited 2). The shape is still removed
//     — an early-exit consumer is a latent defect whatever the producer of the
//     day does — but this gate does not claim to have reproduced its noise, so
//     that arm asserts the SILENCE and the SLICE, not a control.
//  2. The `fan_out_timeout_marker` control's observable is its STATUS, not a
//     stderr line: that helper redirects its producer's stderr to /dev/null, so
//     the SIGPIPE line is swallowed by the helper itself — yet `set -uo pipefail`
//     still raises the pipeline's status to grep's 2, which is what a caller
//     sees and what the fix removes. The same shape WITHOUT the redirect prints
//     `grep: write error: Broken pipe` (measured, same fixture).
//  3. The controls run under `trap "" PIPE`: the suite's bash inherits SIGPIPE
//     IGNORED from the Go runtime, and a shell that inherits SIG_IGN hands it to
//     `sort`/`grep`, which then report EPIPE as a plain write(2) error and exit
//     WITHOUT printing (measured: the same runner prints the two noise lines
//     under a default-disposition shell, and nothing — exit 141 — under a
//     Go-spawned one). See testbudget_report_pipe_gate_test.go for the first
//     measurement of that trap.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	// budgetSiteFnListFailures / …PackageFails / …TimeoutMarker are the three
	// helpers this gate guards, by their own identifiers in the script.
	budgetSiteFnListFailures  = "list_failure_tests"
	budgetSiteFnPackageFails  = "report_package_failures"
	budgetSiteFnTimeoutMarker = "fan_out_timeout_marker"
	// budgetSiteLibPath is the shared predicate library: the row separator and
	// the heading/indent constants the arms need come from where they are
	// declared (Rule 13 — one definition, read rather than repeated).
	budgetSiteLibPath = "scripts/lib/stream-failures.sh"
	// The script-side constants the arms read. Their VALUES decide the fixtures
	// and the expectations; their NAMES appear once, here.
	budgetSiteRowsConstName         = "slowest_report_n"
	budgetSiteSepConstName          = "stream_col_sep"
	budgetSiteBodyIndentConstName   = "failure_body_indent"
	budgetSiteBodyCapConstName      = "failure_body_report_n"
	budgetSiteMarkerIndentConstName = "failure_marker_indent"
	// budgetSiteRefusalIndexLeadConst is the constant `list_failure_tests` reads
	// to key its refusal index. It is declared with a `$'…'` escape (a newline),
	// which is why the arm takes the DECLARATION verbatim rather than a value
	// this gate would have to re-escape.
	budgetSiteRefusalIndexLeadConst = "refusal_index_lead"
	budgetSiteBuildCapConstName     = "build_output_report_n"
	budgetSitePkgHeadingConstName   = "package_failures_heading"
	// budgetSiteSignalKindConstName is the SIGNAL kind `report_package_failures`
	// now branches on. The arm's preamble must supply it, or the body raises
	// `unbound variable` under `set -u` — a stderr line the gate reads as the
	// report failing to be silent. It is read from where it is DECLARED (Rule 13),
	// never repeated here.
	budgetSiteSignalKindConstName     = "stream_kind_signaled"
	budgetSiteTimeoutMarkerConstName  = "timeout_panic_marker"
	budgetSiteTimeoutReportNConstName = "timeout_marker_report_n"
	// The noise signature: `sort` says "write failed: … Broken pipe", `grep`
	// says "write error: Broken pipe", so the controls assert both halves.
	budgetSiteNoiseBrokenPipe = "Broken pipe"
	budgetSiteNoiseWriteError = "write error"
	// Fixture sizes: far above the helpers' caps (so the slice bites) and — for
	// every fixture a CONTROL feeds to an external producer — far above the
	// 65536-byte pipe buffer, so the producer is still writing when the consumer
	// exits and the race the defect needs actually fires.
	budgetSiteListRows    = 3000
	budgetSiteBuildLines  = 500
	budgetSiteTimeoutRows = 1000
	// The fixture's own line shapes.
	budgetSiteFailMarker       = "--- FAIL: "
	budgetSiteTimeoutPanicLine = "panic: test timed out after 10m0s"
	budgetSiteCompilerMarker   = "undefined: x"
	// budgetSiteTimeoutMarkerFixture is the marker the FIXTURE uses. The script's
	// own constant carries a JSON-escaped quote, which cannot be re-parsed in a
	// fixture line without escaping the escape; the arms only need one that every
	// row of the fixture carries, so they match on this prefix and still READ the
	// script's constant (asserted non-empty below) rather than copying it.
	budgetSiteTimeoutMarkerFixture = "panic: test timed out after"
)

// budgetSiteArm is one arm of the shell experiment.
type budgetSiteArm struct {
	label    string
	preamble []string
	bodyPath string
	call     string
}

// TestBudgetFailureReportCarriesNoEarlyExitConsumer is the cheap structural
// half, over all three helpers: none of their shipped bodies may pipe into a
// consumer that exits at the cap. Comment lines are skipped — each helper
// DOCUMENTS the shape it avoids, and the documentation names it.
func TestBudgetFailureReportCarriesNoEarlyExitConsumer(t *testing.T) {
	script := budgetSiteReadFile(t, budgetScriptPath)
	re := regexp.MustCompile(budgetReportPipeConsumerPipeline)
	for _, fn := range []string{budgetSiteFnListFailures, budgetSiteFnPackageFails, budgetSiteFnTimeoutMarker} {
		body := budgetSiteFuncBody(t, script, fn)
		for i, line := range strings.Split(body, "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "#") {
				continue
			}
			if re.MatchString(code) {
				t.Errorf("%s: %s() pipes into an early-exit consumer (helper line %d):\n%s\n"+
					"`head` closes the pipe at its cap, the producer gets SIGPIPE, and `set -uo pipefail` "+
					"prints `… Broken pipe` in the middle of a failure report — noise that reads like a "+
					"defect of the run being reported. Capture the producer and slice the captured text "+
					"with awk (the shape %s' body slice uses).",
					budgetScriptPath, fn, i+1, code, budgetSiteFnListFailures)
			}
		}
	}
}

// TestBudgetFailureListArmsAreSilentAndFaithful runs the failure LIST helper as
// shipped beside the pre-fix shape on one fixture whose producer ends in the
// external `sort` — exactly as the real reader does (gopi_stream_failures ends
// `| sort -u`), which is the process whose EPIPE the script's pipefail turns
// into a report line.
func TestBudgetFailureListArmsAreSilentAndFaithful(t *testing.T) {
	script := budgetSiteReadFile(t, budgetScriptPath)
	lib := budgetSiteReadFile(t, budgetSiteLibPath)
	shellOptions := budgetReportPipeShellOptions(t, script)
	body := budgetSiteFuncBody(t, script, budgetSiteFnListFailures)
	capRows := budgetReportPipeIntConst(t, script, budgetSiteRowsConstName)
	sep := budgetReportPipeStringConst(t, lib, budgetSiteSepConstName)

	dir := t.TempDir()
	bodyPath := budgetSiteWriteBody(t, dir, "list-failures", body)
	preamble := []string{
		`raw_json=/nonexistent`,
		budgetSiteVar(budgetSiteRowsConstName, capRows),
		budgetSiteStringVar(budgetSiteSepConstName, sep),
		budgetSiteStringVar(budgetSiteBodyIndentConstName,
			budgetReportPipeStringConst(t, script, budgetSiteBodyIndentConstName)),
		budgetSiteVar(budgetSiteBodyCapConstName,
			budgetReportPipeIntConst(t, script, budgetSiteBodyCapConstName)),
		budgetSiteStringVar(budgetSiteMarkerIndentConstName,
			budgetReportPipeStringConst(t, script, budgetSiteMarkerIndentConstName)),
		`budgetSiteRowsText="$(awk -v pkg="$budgetSitePkg" 'BEGIN { for (i = 0; i < ` +
			strconv.Itoa(budgetSiteListRows) + `; i++) printf "%s\t` + "TestBudgetSite" +
			`%05d\n", pkg, i }')"`,
		`gopi_stream_failures() { printf '%s\n' "$budgetSiteRowsText" | sort -u; }`,
		`gopi_stream_failure_bodies() { printf '%s\n' "    fixture_test.go:9: got 1, want 2"; }`,
		// The refusal predicate and its index lead byte: `list_failure_tests` asks
		// which of the failing rows are stale-binary REFUSALS (it marks those
		// inline). This fixture carries none, so the predicate is stubbed to the
		// empty answer, and the lead byte is taken from the SCRIPT'S OWN
		// declaration (Rule 13) rather than spelled again here — the arm sources
		// the shipped body, so every value that body reads must be in scope.
		`gopi_stream_freshness_tests() { :; }`,
		budgetSiteRawConstDecl(t, script, budgetSiteRefusalIndexLeadConst),
	}
	control := runBudgetSiteArm(t, dir, shellOptions, budgetSiteArm{
		label:    "control-list-failures",
		preamble: preamble,
		bodyPath: bodyPath,
		call:     `gopi_stream_failures "$raw_json" | head -"$` + budgetSiteRowsConstName + `"`,
	})
	// Same two halves as the green-report half (see its control): the
	// locale-independent producer signature first — the observable the defect
	// always leaves — then the two English literals, asserted only because this
	// arm pins the child's locale.
	if !budgetReportPipeMatchProducerNoise(control.stderr) {
		t.Fatalf("the pre-fix shape did NOT reproduce the report noise on a %d-row fixture — the control "+
			"proves nothing, so this gate could not see a regression.\nstderr: %q",
			budgetSiteListRows, control.stderr)
	}
	if !strings.Contains(control.stderr, budgetSiteNoiseBrokenPipe) ||
		!strings.Contains(control.stderr, budgetSiteNoiseWriteError) {
		t.Fatalf("the pre-fix shape reproduced a producer error (%q) but NOT the English text this host's "+
			"locale pin should have produced — the arm's message locale is not the one "+
			"budgetReportPipeChildEnv pins (%q).\nstderr: %q",
			budgetReportPipeNoiseProducerNames, budgetReportPipeChildLocale, control.stderr)
	}

	shipped := runBudgetSiteArm(t, dir, shellOptions, budgetSiteArm{
		label:    "shipped-list-failures",
		preamble: preamble,
		bodyPath: bodyPath,
		call:     budgetSiteFnListFailures,
	})
	if shipped.stderr != "" {
		t.Fatalf("%s wrote to stderr:\n%q\nA failure report must be silent: this is the noise the run "+
			"under report gets blamed for (Rule 15).", budgetSiteFnListFailures, shipped.stderr)
	}
	if shipped.exit != 0 {
		t.Fatalf("%s exited %d, want 0 (stderr %q)", budgetSiteFnListFailures, shipped.exit, shipped.stderr)
	}
	if got, want := strings.Count(shipped.stdout, budgetSiteFailMarker), capRows; got != want {
		t.Errorf("%s printed %d failure marker(s), want the cap %d (%s) — the slice is taken from captured "+
			"text now and must slice the same way.\n%s",
			budgetSiteFnListFailures, got, want, budgetSiteRowsConstName, shipped.stdout)
	}
	// The control's stdout is the ROWS it selected (`package<TAB>test`, one per
	// line) — the shape it consumed, not the markers the helper renders — so the
	// shared fixture is asserted by COUNT: both arms must have selected the same
	// cap's worth of rows, or the silence comparison would be between different
	// inputs.
	if got, want := strings.Count(control.stdout, "\n"), capRows; got != want {
		t.Errorf("the pre-fix shape selected %d row(s), want the cap %d (%s) — the two arms must meet the "+
			"same fixture.\n%s", got, want, budgetSiteRowsConstName, control.stdout)
	}
}

// TestBudgetTimeoutMarkerArmsAreSilentAndFaithful is the TIMEOUT block's arm:
// the marker reader must print the same lines as before, with no pipe noise. Its
// producer is external `grep`, which is why this control DOES reproduce.
func TestBudgetTimeoutMarkerArmsAreSilentAndFaithful(t *testing.T) {
	script := budgetSiteReadFile(t, budgetScriptPath)
	shellOptions := budgetReportPipeShellOptions(t, script)
	body := budgetSiteFuncBody(t, script, budgetSiteFnTimeoutMarker)
	marker := budgetReportPipeStringConst(t, script, budgetSiteTimeoutMarkerConstName)
	// The fixture's rows must carry the marker the script actually looks for:
	// the script's constant is READ (Rule 13) and asserted to still name the
	// panic text, while the arms match on the unambiguous prefix — the script's
	// constant ends in a JSON-escaped quote, which a hand-built JSON fixture line
	// cannot carry without escaping the escape.
	if !strings.Contains(marker, budgetSiteTimeoutMarkerFixture) {
		t.Fatalf("%s=%q no longer names %q — this arm's fixture would stop standing in for the text the "+
			"TIMEOUT block reads", budgetSiteTimeoutMarkerConstName, marker, budgetSiteTimeoutMarkerFixture)
	}
	capLines := budgetReportPipeIntConst(t, script, budgetSiteTimeoutReportNConstName)

	dir := t.TempDir()
	streamPath := filepath.Join(dir, "timeout-stream.json")
	var b strings.Builder
	for i := 0; i < budgetSiteTimeoutRows; i++ {
		b.WriteString(`{"Action":"output","Package":"` + budgetReportPipeFixturePkg + `","Output":"` +
			budgetSiteTimeoutPanicLine + ` (expiry ` + strconv.Itoa(i) + `)\n"}` + "\n")
	}
	if err := os.WriteFile(streamPath, []byte(b.String()), filePerm); err != nil {
		t.Fatalf("write the timeout fixture stream: %v", err)
	}
	bodyPath := budgetSiteWriteBody(t, dir, "timeout-marker", body)
	preamble := []string{
		`raw_json=` + budgetReportPipeQuote(streamPath),
		budgetSiteStringVar(budgetSiteTimeoutMarkerConstName, budgetSiteTimeoutMarkerFixture),
		budgetSiteVar(budgetSiteTimeoutReportNConstName, capLines),
	}
	// The pre-fix SHAPE, with the SAME cap constant so the two arms slice alike.
	legacy := `grep -a -o -- "$` + budgetSiteTimeoutMarkerConstName +
		`[^\"\\]*" "$raw_json" 2>/dev/null | head -"$` + budgetSiteTimeoutReportNConstName + `"`

	control := runBudgetSiteArm(t, dir, shellOptions, budgetSiteArm{
		label:    "control-timeout-marker",
		preamble: preamble,
		bodyPath: bodyPath,
		call:     legacy,
	})
	// The control's observable is its STATUS, not a stderr line: the legacy shape
	// redirects grep's stderr to /dev/null, so the SIGPIPE it takes is SWALLOWED
	// by the helper itself — but `set -uo pipefail` still raises the pipeline's
	// status to grep's 2 (measured 2026-10-13: the same shape WITHOUT the
	// redirect prints `grep: write error: Broken pipe`, and with it prints
	// nothing while still exiting 2). The status is what a caller sees, and it is
	// what the fix removes alongside the shape.
	if control.exit == 0 {
		t.Fatalf("the pre-fix shape exited 0 on a %d-row fixture (stderr %q) — the control expected the "+
			"pipefail status it raises (2). Either the fixture no longer fills the pipe buffer or the "+
			"shape has moved; this gate cannot see a regression without it.",
			budgetSiteTimeoutRows, control.stderr)
	}
	if control.stderr != "" {
		t.Fatalf("the pre-fix shape wrote to stderr (%q) — its redirect should have swallowed the SIGPIPE "+
			"line; the measurement this arm is built on has changed", control.stderr)
	}

	shipped := runBudgetSiteArm(t, dir, shellOptions, budgetSiteArm{
		label:    "shipped-timeout-marker",
		preamble: preamble,
		bodyPath: bodyPath,
		call:     budgetSiteFnTimeoutMarker,
	})
	if shipped.stderr != "" {
		t.Fatalf("%s wrote to stderr:\n%q\nA timeout block must be silent (Rule 15).",
			budgetSiteFnTimeoutMarker, shipped.stderr)
	}
	if shipped.exit != 0 {
		t.Fatalf("%s exited %d, want 0 (stderr %q)", budgetSiteFnTimeoutMarker, shipped.exit, shipped.stderr)
	}
	if shipped.stdout != control.stdout {
		t.Errorf("%s changed what the TIMEOUT block reports.\nbefore:\n%s\nafter:\n%s",
			budgetSiteFnTimeoutMarker, control.stdout, shipped.stdout)
	}
	if got := strings.Count(shipped.stdout, "\n"); got != capLines {
		t.Errorf("%s printed %d marker line(s), want the cap %d (%s)",
			budgetSiteFnTimeoutMarker, got, capLines, budgetSiteTimeoutReportNConstName)
	}
}

// TestBudgetPackageFailureArmSlicesSilently is the third arm: the build-output
// slice must quote the cap's worth of the package's own diagnostics, indented,
// with no pipe noise. Its control is deliberately absent — see the file header
// (limit 1): this producer is the `printf` builtin.
func TestBudgetPackageFailureArmSlicesSilently(t *testing.T) {
	script := budgetSiteReadFile(t, budgetScriptPath)
	lib := budgetSiteReadFile(t, budgetSiteLibPath)
	shellOptions := budgetReportPipeShellOptions(t, script)
	body := budgetSiteFuncBody(t, script, budgetSiteFnPackageFails)
	sep := budgetReportPipeStringConst(t, lib, budgetSiteSepConstName)
	capLines := budgetReportPipeIntConst(t, script, budgetSiteBuildCapConstName)
	indent := budgetReportPipeStringConst(t, script, budgetSiteBodyIndentConstName)
	heading := budgetReportPipeStringConst(t, script, budgetSitePkgHeadingConstName)
	// The signal kind the same body branches on. Supplying it is what makes the
	// arm measure the SHIPPED function: without it `set -u` kills the body with
	// `unbound variable` on stderr, and the gate would report a silent-report
	// breach that is really a fixture that fell out of step with the code.
	signalKind := budgetReportPipeStringConst(t, lib, budgetSiteSignalKindConstName)

	dir := t.TempDir()
	bodyPath := budgetSiteWriteBody(t, dir, "package-failures", body)
	preamble := []string{
		`raw_json=/nonexistent`,
		budgetSiteStringVar(budgetSiteSepConstName, sep),
		budgetSiteStringVar(budgetSiteBodyIndentConstName, indent),
		budgetSiteVar(budgetSiteBuildCapConstName, capLines),
		budgetSiteStringVar(budgetSitePkgHeadingConstName, heading),
		budgetSiteStringVar(budgetSiteSignalKindConstName, signalKind),
		`gopi_stream_package_failures() { printf '%s%sbuild\n' "$budgetSitePkg" "$` +
			budgetSiteSepConstName + `"; }`,
		`gopi_stream_build_output_for() { awk 'BEGIN { for (i = 0; i < ` +
			strconv.Itoa(budgetSiteBuildLines) + `; i++) printf "pkg/b.go:%d:2: ` +
			budgetSiteCompilerMarker + `%d\n", i, i }'; }`,
	}
	shipped := runBudgetSiteArm(t, dir, shellOptions, budgetSiteArm{
		label:    "shipped-package-failures",
		preamble: preamble,
		bodyPath: bodyPath,
		call:     budgetSiteFnPackageFails,
	})
	if shipped.stderr != "" {
		t.Fatalf("%s wrote to stderr:\n%q\nA failure report must be silent (Rule 15).",
			budgetSiteFnPackageFails, shipped.stderr)
	}
	if shipped.exit != 0 {
		t.Fatalf("%s exited %d, want 0 (stderr %q)", budgetSiteFnPackageFails, shipped.exit, shipped.stderr)
	}
	if !strings.Contains(shipped.stdout, heading) {
		t.Errorf("%s printed no %s heading:\n%s", budgetSiteFnPackageFails, budgetSitePkgHeadingConstName, shipped.stdout)
	}
	indented := 0
	for _, line := range strings.Split(shipped.stdout, "\n") {
		if strings.HasPrefix(line, indent) && strings.Contains(line, budgetSiteCompilerMarker) {
			indented++
		}
	}
	if indented != capLines {
		t.Errorf("%s quoted %d diagnostic line(s), want the cap %d (%s) — the slice must still bite on a "+
			"%d-line diagnostic.\n%s", budgetSiteFnPackageFails, indented, capLines,
			budgetSiteBuildCapConstName, budgetSiteBuildLines, shipped.stdout)
	}
}

// budgetSiteReadFile loads a repo file the arms read constants and bodies from.
func budgetSiteReadFile(t *testing.T, rel string) string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

// budgetSiteRawConstDecl returns one of the script's constant DECLARATIONS
// verbatim (`NAME=<rhs>`, whatever the rhs spelling). A declaration whose value is
// a shell escape — `refusal_index_lead=$'\n'` — cannot go through
// budgetReportPipeStringConst, which reads a quoted string; and re-spelling the
// escape here would be a second definition of a value the shipped body reads
// (Rule 13/14). The declaration is READ, and the arm sources it.
func budgetSiteRawConstDecl(t *testing.T, script, name string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^` + name + `=.*$`).FindString(script)
	if m == "" {
		t.Fatalf("%s declares no %s=… — the shipped body this gate sources would read an unbound variable "+
			"and the arm would fail for a reason that is not the defect", budgetScriptPath, name)
	}
	return m
}

// budgetSiteFuncBody is the same brace match the green-report gate uses: the
// shipped bytes from the definition to the first closing brace in column 1.
func budgetSiteFuncBody(t *testing.T, script, fn string) string {
	t.Helper()
	start := strings.Index(script, fn+"() {")
	if start < 0 {
		t.Fatalf("%s: %s() is gone — the helper this gate guards no longer exists (Rule 15)", budgetScriptPath, fn)
	}
	rest := script[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("%s: %s() has no closing brace in column 1 — this gate cannot extract the shipped body",
			budgetScriptPath, fn)
	}
	return rest[:end+len("\n}\n")]
}

// budgetSiteWriteBody materialises an extracted body for the arm to source.
func budgetSiteWriteBody(t *testing.T, dir, label, body string) string {
	t.Helper()
	path := filepath.Join(dir, label+"-body.sh")
	if err := os.WriteFile(path, []byte(body), filePerm); err != nil {
		t.Fatalf("write the %s body: %v", label, err)
	}
	return path
}

// runBudgetSiteArm runs one arm in the script's own shell options, with the
// SIGPIPE disposition the real entry point has (see the file header).
func runBudgetSiteArm(t *testing.T, dir, shellOptions string, o budgetSiteArm) budgetReportPipeArm {
	t.Helper()
	runner := filepath.Join(dir, "arm-"+o.label+".sh")
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString(budgetReportPipeNoInheritedSigPipe + "\n")
	b.WriteString(shellOptions + "\n")
	b.WriteString("budgetSitePkg=" + budgetReportPipeQuote(budgetReportPipeFixturePkg) + "\n")
	for _, line := range o.preamble {
		b.WriteString(line + "\n")
	}
	b.WriteString(". " + budgetReportPipeQuote(o.bodyPath) + "\n")
	b.WriteString(o.call + "\n")
	if err := os.WriteFile(runner, []byte(b.String()), filePerm); err != nil {
		t.Fatalf("write the arm runner %s: %v", runner, err)
	}

	cmd := exec.Command(budgetShellBin, runner)
	cmd.Dir = dir
	// The arm's message locale is an INPUT to this gate, not an ambient
	// accident — see budgetReportPipeChildEnv (the green-report half) for the
	// measurement, and the file header's limit 3 for why an inherited SIGPIPE
	// disposition is restored beside it.
	cmd.Env = budgetReportPipeChildEnv()
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	arm := budgetReportPipeArm{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return arm
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("run %s: %v", runner, err)
	}
	arm.exit = ee.ExitCode()
	return arm
}

// budgetSiteVar / budgetSiteStringVar render the two assignment shapes, so a
// fixture value reaches the arm through the spelling the script itself uses.
func budgetSiteVar(name string, n int) string {
	return name + "=" + strconv.Itoa(n)
}

func budgetSiteStringVar(name, value string) string {
	return name + "=" + budgetReportPipeQuote(value)
}
