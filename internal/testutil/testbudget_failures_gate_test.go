package testutil

// The FAILURE-LIST half of the reporting fix (Rules 1a/13/14/15): which failures
// a budget run actually had must be read from the stream's STRUCTURED events
// (`Action=="fail"` + the Test/Package fields), never from the TEXT SHAPE of a
// `--- FAIL:` line.
//
// WHY IT EXISTS
// -------------
// Measured 2026-10-12 (task 01a12e42's narrow run,
// `make test-budget PKGS='./internal/standards ./internal/tools'`): the failures
// block harvested its list with
//
//     grep -a -o -- '--- FAIL: [^"\\]*' "$raw_json"
//
// over the whole stream, so a PASSING test printing its own fixture was reported
// as a failure of the run. Two of the six names in that report — `TestAnchor`
// and `TestPublishedAnchorResolves` — are printed by
// `TestPublishedAnchorGateRefusesAFailingGate` and
// `TestFinishCleanupLocalSyncQuotesTheFailingPackage` (both PASS, both quoting a
// `go test` output inside their own assertion text), and neither name exists as
// a test function of this repository (`go test ./internal/tools/ -list` lists
// neither). The same run printed NOTHING for its package-level failures.
//
// WHY IT IS TESTED THIS WAY
// -------------------------
// The predicate lives in scripts/lib/stream-failures.sh — the same shape as
// scripts/lib/stream-scope.sh — so it can be called with SYNTHETIC streams: the
// "a passing test printed a `--- FAIL:` line" case is asserted here instead of
// waited for on a real tree, and the Go side never re-implements the reader
// (Rule 14: a second definition is the defect, not the test).
//
// The end-to-end arm runs the REAL script against a stub `go` on PATH, the
// pattern testbudget_concurrent_test.go established: cheap, deterministic, no
// test bodies and no device work (Rule 27).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// budgetFailuresLib is the shared predicate (the script's own
	// stream_failures_lib constant, asserted there by the static gate).
	budgetFailuresLib = "scripts/lib/stream-failures.sh"
	// budgetFailuresFixtureModule is the module path the synthetic streams use.
	budgetFailuresFixtureModule = "example.test/failfixture"
	// budgetFailuresFixturePkg is the package that runs tests, and
	// budgetFailuresBuildPkg the one that never compiles.
	budgetFailuresFixturePkg = budgetFailuresFixtureModule + "/internal/tools"
	budgetFailuresBuildPkg   = budgetFailuresFixtureModule + "/internal/broken"
	// budgetFailuresFakeName is the name a PASSING test prints as its own
	// fixture text — the defect's shape. The real run measured this exact
	// string; it must never appear in a failure list.
	budgetFailuresFakeName = "TestAnchor"
	// budgetFailuresRealName is the test that actually fails.
	budgetFailuresRealName = "TestRealFailure"
	// budgetFailuresBodyLine is the failing test's OWN assertion output — the
	// row that `Action=="fail"` does NOT carry (measured: all 17 fail events of
	// run-1791849386-333342 read Output=None), so it is the reason the report
	// has to reach into the test's output rows to make a failure name itself.
	budgetFailuresBodyLine = "    fixture_test.go:9: got 1, want 2"
	// budgetFailuresBodylessName is a test that FAILS with no output rows at all
	// in the stream: the other arm of the same report — the stream carries the
	// verdict and NOT its evidence, which must print as its own sentence and
	// never as an empty list (Rule 15).
	budgetFailuresBodylessName = "TestFailsWithNoOutput"
	// budgetFailuresBodyAbsentMarker is the distinctive half of the sentence the
	// script prints for that arm; the wording lives in the script
	// (`failure_body_absent`) and is pinned here.
	budgetFailuresBodyAbsentMarker = "NO output rows in this stream for this test"
	// budgetFailuresVerdictMarker is the package-level verdict line's prefix:
	// every package the stream recorded as failed, so a reader is not left with
	// a capped test list as their only account of which packages broke.
	budgetFailuresVerdictMarker = "package verdict: FAIL"
	// budgetFailuresCompilerLine is the compiler diagnostic a build-failed
	// package emits — the text the old block could not print (Rule 15).
	budgetFailuresCompilerLine = "broken/b_test.go:6:2: undefined: thisIdentifierDoesNotExist"
	// budgetFailuresPrinterName is the PASSING test that prints the fake line.
	budgetFailuresPrinterName = "TestPrintsAFakeFailLine"
	// budgetFailuresMachineSep is the TAB the library's machine-readable arm
	// uses (`package<TAB>test`).
	budgetFailuresMachineSep = "\t"
	// budgetFailuresMarkerRow is what a FAILURE row starts with — the shape the
	// block prints for a test whose own event failed, and the one a printed marker
	// must never take (that is the claim "the runner reported this test as failed";
	// the unattributed block renders its own, different, shape).
	budgetFailuresMarkerRow = "--- FAIL: "
)

// budgetFailuresStream is the synthetic `go test -json` stream every arm below
// reads: one PASSING test that prints a `--- FAIL: TestAnchor` line (the
// measured defect in miniature), one test that really fails, and one package
// that never compiles.
func budgetFailuresStream() string {
	return strings.Join([]string{
		`{"Action":"start","Package":"` + budgetFailuresFixturePkg + `"}`,
		`{"Action":"run","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresPrinterName + `"}`,
		`{"Action":"output","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresPrinterName + `","Output":"=== RUN   ` + budgetFailuresPrinterName + `\n"}`,
		`{"Action":"output","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresPrinterName + `","Output":"    refusal: go test ./internal/pubgate/ -count=1: --- FAIL: ` + budgetFailuresFakeName + ` (0.00s)\n"}`,
		`{"Action":"output","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresPrinterName + `","Output":"--- PASS: ` + budgetFailuresPrinterName + ` (0.00s)\n"}`,
		`{"Action":"pass","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresPrinterName + `","Elapsed":0}`,
		`{"Action":"run","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresRealName + `"}`,
		`{"Action":"output","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresRealName + `","Output":"` + budgetFailuresBodyLine + `\n"}`,
		`{"Action":"output","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresRealName + `","Output":"--- FAIL: ` + budgetFailuresRealName + ` (0.00s)\n"}`,
		`{"Action":"fail","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresRealName + `","Elapsed":0}`,
		// The SECOND arm: a verdict with NO output rows of its own. `go test -json`
		// really produces this shape — measured on run-1791849386-333342, where
		// every `Action=="fail"` event carries NO Output field at all — so the
		// report must print its own sentence for it rather than an empty line
		// (Rule 15: an absent body is a fact to state, not a blank to skip).
		`{"Action":"run","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresBodylessName + `"}`,
		`{"Action":"fail","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresBodylessName + `","Elapsed":0}`,
		// The package-level verdict that accompanies every failing package: it
		// names no test, so it must NOT read as "a failure with no test to name".
		`{"Action":"fail","Package":"` + budgetFailuresFixturePkg + `","Elapsed":0.1}`,
		`{"Action":"start","Package":"` + budgetFailuresBuildPkg + `"}`,
		`{"Action":"output","Package":"` + budgetFailuresBuildPkg + `","Output":"FAIL\t` + budgetFailuresBuildPkg + ` [build failed]\n"}`,
		`{"ImportPath":"` + budgetFailuresBuildPkg + ` [` + budgetFailuresBuildPkg + `.test]","Action":"build-output","Output":"# ` + budgetFailuresBuildPkg + ` [` + budgetFailuresBuildPkg + `.test]\n"}`,
		`{"ImportPath":"` + budgetFailuresBuildPkg + ` [` + budgetFailuresBuildPkg + `.test]","Action":"build-output","Output":"` + budgetFailuresCompilerLine + `\n"}`,
		`{"ImportPath":"` + budgetFailuresBuildPkg + ` [` + budgetFailuresBuildPkg + `.test]","Action":"build-fail"}`,
		`{"Action":"fail","Package":"` + budgetFailuresBuildPkg + `","Elapsed":0,"FailedBuild":"` + budgetFailuresBuildPkg + ` [` + budgetFailuresBuildPkg + `.test]"}`,
	}, "\n") + "\n"
}

// budgetFailuresRunLib calls one library function with bash — the same
// interpreter the script uses — and returns its output. Any extra words are
// passed as that function's own arguments (the body reader takes a package and a
// test), so the call shape stays "the library, as the script calls it" rather
// than a second reading of the stream implemented in Go (Rule 14).
func budgetFailuresRunLib(t *testing.T, root, streamPath string, call string, extra ...string) (string, error) {
	t.Helper()
	libPath := filepath.Join(root, filepath.FromSlash(budgetFailuresLib))
	argv := append([]string{"-c", `. "$1" && "$2" "$3" "${@:4}"`, "bash", libPath, call, streamPath}, extra...)
	out, err := exec.Command(budgetShellBin, argv...).CombinedOutput()
	return string(out), err
}

// TestBudgetFailurePredicateIgnoresAFakeFailLineInAPassingTest is the core
// regression, at the level where it can be asserted cheaply: the stream holds a
// PASSING test that PRINTS `--- FAIL: TestAnchor`, and the failure list must not
// contain it — while the really failing test is still named.
func TestBudgetFailurePredicateIgnoresAFakeFailLineInAPassingTest(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	streamPath := filepath.Join(t.TempDir(), "fixture-stream.json")
	if err := os.WriteFile(streamPath, []byte(budgetFailuresStream()), 0o644); err != nil {
		t.Fatalf("write fixture stream: %v", err)
	}

	failures, err := budgetFailuresRunLib(t, root, streamPath, "gopi_stream_failures")
	if err != nil {
		t.Fatalf("gopi_stream_failures: %v\n%s", err, failures)
	}
	got := normaliseLines(failures)
	want := []string{
		budgetFailuresFixturePkg + budgetFailuresMachineSep + budgetFailuresBodylessName,
		budgetFailuresFixturePkg + budgetFailuresMachineSep + budgetFailuresRealName,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the failure list is not exactly the tests whose own event failed:\n got %v\nwant %v", got, want)
	}
	if strings.Contains(failures, budgetFailuresFakeName) {
		t.Errorf("the failure list names %s — a line a PASSING test printed as its own fixture text was "+
			"harvested as a failure (the measured defect)", budgetFailuresFakeName)
	}

	count, err := budgetFailuresRunLib(t, root, streamPath, "gopi_stream_failure_count")
	if err != nil {
		t.Fatalf("gopi_stream_failure_count: %v\n%s", err, count)
	}
	if strings.TrimSpace(count) != "2" {
		t.Errorf("gopi_stream_failure_count = %q, want 2 — the count comes from the same events as the "+
			"list, so the two cannot disagree", strings.TrimSpace(count))
	}

	// The BODY half, at the library level: the failing test's own output rows
	// come back for it, and asking for them under the OTHER package's name (a
	// real collision shape: `p1/tools` and `p2/tools` each holding a
	// `TestRealFailure`) returns NOTHING rather than somebody else's text.
	body, err := budgetFailuresRunLib(t, root, streamPath,
		"gopi_stream_failure_bodies", budgetFailuresFixturePkg, budgetFailuresRealName)
	if err != nil {
		t.Fatalf("gopi_stream_failure_bodies: %v\n%s", err, body)
	}
	if !strings.Contains(body, strings.TrimSpace(budgetFailuresBodyLine)) {
		t.Errorf("the failing test's OWN output is not readable from the stream — the reason a test "+
			"failed is the one thing a failures block must carry (Rule 15):\n%s", body)
	}
	foreign, err := budgetFailuresRunLib(t, root, streamPath,
		"gopi_stream_failure_bodies", budgetFailuresFixturePkg+"/other", budgetFailuresRealName)
	if err != nil {
		t.Fatalf("gopi_stream_failure_bodies (other package): %v\n%s", err, foreign)
	}
	if strings.TrimSpace(foreign) != "" {
		t.Errorf("a body was returned for a package the stream does not carry it under — attribution "+
			"by package is what stops one package quoting another's assertion text:\n%s", foreign)
	}
	// And the test that failed with no rows of its own returns an EMPTY body:
	// the script turns that into its own sentence, and it must be able to tell
	// the two apart.
	absent, err := budgetFailuresRunLib(t, root, streamPath,
		"gopi_stream_failure_bodies", budgetFailuresFixturePkg, budgetFailuresBodylessName)
	if err != nil {
		t.Fatalf("gopi_stream_failure_bodies (body-less): %v\n%s", err, absent)
	}
	if strings.TrimSpace(absent) != "" {
		t.Errorf("a test whose stream carries no output rows returned a non-empty body — the two arms "+
			"are indistinguishable then:\n%s", absent)
	}

	// The package-level half: the failing package NAMES ITS TEST, so it must not
	// appear as "a failure with no test"; the un-compilable one must, as `build`.
	pkgFailures, err := budgetFailuresRunLib(t, root, streamPath, "gopi_stream_package_failures")
	if err != nil {
		t.Fatalf("gopi_stream_package_failures: %v\n%s", err, pkgFailures)
	}
	if strings.Contains(pkgFailures, budgetFailuresFixturePkg) {
		t.Errorf("a package WITH a failing test was reported as a package-level failure:\n%s", pkgFailures)
	}
	if want := budgetFailuresBuildPkg + budgetFailuresMachineSep + "build"; !strings.Contains(pkgFailures, want) {
		t.Errorf("gopi_stream_package_failures does not name %q:\n%s", want, pkgFailures)
	}

	build, err := budgetFailuresRunLib(t, root, streamPath, "gopi_stream_build_output")
	if err != nil {
		t.Fatalf("gopi_stream_build_output: %v\n%s", err, build)
	}
	if !strings.Contains(build, budgetFailuresCompilerLine) {
		t.Errorf("the compiler's own diagnostic is not reported — a build failure must NAME itself "+
			"(Rule 15):\n%s", build)
	}
	if strings.Contains(build, `"}`) {
		t.Errorf("the compiler diagnostic kept the JSON fields that follow it:\n%s", build)
	}
}

// TestBudgetFailurePredicateSurvivesABinaryStream pins the `grep -a` half: a
// stream can carry a NUL byte (a test feeding binary data to a tool), and a
// plain grep then calls the whole file binary and prints NOTHING — a silent
// empty failure list, which is the failure mode this report exists to remove.
func TestBudgetFailurePredicateSurvivesABinaryStream(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	streamPath := filepath.Join(t.TempDir(), "binary-stream.json")
	blob := []byte(`{"Action":"output","Package":"` + budgetFailuresFixturePkg + `","Output":"binary \x00 payload\n"}` + "\n" +
		`{"Action":"fail","Package":"` + budgetFailuresFixturePkg + `","Test":"` + budgetFailuresRealName + `","Elapsed":0}` + "\n")
	if err := os.WriteFile(streamPath, blob, 0o644); err != nil {
		t.Fatalf("write binary stream: %v", err)
	}
	out, err := budgetFailuresRunLib(t, root, streamPath, "gopi_stream_failure_count")
	if err != nil {
		t.Fatalf("gopi_stream_failure_count on a binary stream: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "1" {
		t.Errorf("a stream carrying a NUL byte yielded count %q, want 1 — grep must read it as text "+
			"(Rule 15: an empty list is a silent failure)", strings.TrimSpace(out))
	}
}

// TestBudgetScriptReportsOnlyRealFailures runs the REAL script end to end
// against a stub `go` whose stream carries the fixture above, and asserts the
// printed failures block: the real failing test is named, the fake one is not
// anywhere in the block, and the build failure is named WITH its compiler line.
func TestBudgetScriptReportsOnlyRealFailures(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	module := repoModulePath(t, root)
	// The stream's packages are the REPO's, so the run's own scope check stays
	// silent: the stub answers the real `go test` invocation, not a fixture
	// module's.
	stream := strings.ReplaceAll(budgetFailuresStream(), budgetFailuresFixtureModule, module)
	stubDir := t.TempDir()
	scratchHome := t.TempDir()
	stub := "#!/bin/bash\n" +
		"# Stands in for `go`: it answers `go test … -json` with the fixture stream\n" +
		"# (a PASSING test printing a --- FAIL: line, one real failure, one package\n" +
		"# that never compiled) and succeeds for anything else, so the script under\n" +
		"# test runs its real path: stream, summary, failure report.\n" +
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

	script := filepath.Join(root, filepath.FromSlash(budgetScriptPath))
	// The whole module is asked for, so the fixture's build-failed package is
	// WITHIN the requested scope: the run's own out-of-scope check stays silent
	// and the block under test is the failure report alone.
	cmd := exec.Command(budgetShellBin, script, "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		ScratchHomeEnvName+"="+scratchHome,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, _ := cmd.CombinedOutput() //stylegate:ignore the child's exit code is asserted through the report text, which is the signal under test
	report := string(out)
	if !strings.Contains(report, budgetFailuresRealName) {
		t.Fatalf("the report does not name the really failing test %s:\n%s", budgetFailuresRealName, report)
	}
	// The fake name may now appear in the report — but ONLY in the UNATTRIBUTED
	// block, never as a failure row. Before this arm the name had to be absent
	// from the report altogether; that absence was the silent-drop half of the
	// defect (task 01a18362 measured a stream carrying two such rows and a report
	// naming neither, with no account of where they went). What must never appear
	// is the row SHAPE the list renders — `--- FAIL: <name>  (<pkg>)` — because
	// that is the claim "this run's runner reported this test as failed".
	if fakeRow := budgetFailuresMarkerRow + budgetFailuresFakeName + "  ("; strings.Contains(report, fakeRow) {
		t.Errorf("the report lists %s as a failing test — a PASSING test's own fixture text was harvested as a "+
			"failure (want no %q):\n%s", budgetFailuresFakeName, fakeRow, report)
	}
	if !strings.Contains(report, budgetFailuresFakeName) {
		t.Errorf("the report does not classify the printed marker %s at all — a row that reached the stream and "+
			"reaches no category is a row the reader cannot see is missing (Rule 15):\n%s",
			budgetFailuresFakeName, report)
	}
	if !strings.Contains(report, budgetFailuresCompilerLine) {
		t.Errorf("the report does not name the build failure's compiler diagnostic — a package that never "+
			"compiled printed NOTHING before this change (Rule 15):\n%s", report)
	}
	if !strings.Contains(report, strings.ReplaceAll(budgetFailuresBuildPkg, budgetFailuresFixtureModule, module)) {
		t.Errorf("the report does not name the un-compilable package:\n%s", report)
	}

	// THE BODY ARM — the half this change exists for. Before it, the block
	// printed marker lines only, so a failing test arrived with nothing under
	// it: measured 2026-10-13 on run-1791849386-333342, where four `--- FAIL:`
	// rows carried no reason, and the reasons (two of them the runner's own
	// stale pre-built binary) were in the stream all along.
	if !strings.Contains(report, strings.TrimSpace(budgetFailuresBodyLine)) {
		t.Errorf("the report does not quote the failing test's OWN assertion output under its marker — "+
			"a failure that names only itself is the defect this block was fixed for (Rule 15):\n%s", report)
	}
	// It must sit UNDER the marker, not merely somewhere in the report: a body
	// printed anywhere else is a line a reader has to go looking for.
	marker := "--- FAIL: " + budgetFailuresRealName
	markerAt := strings.Index(report, marker)
	bodyAt := strings.Index(report, strings.TrimSpace(budgetFailuresBodyLine))
	if markerAt < 0 || bodyAt < 0 || bodyAt < markerAt {
		t.Errorf("the failing test's output is not printed UNDER its own marker (marker at %d, body at "+
			"%d):\n%s", markerAt, bodyAt, report)
	}
	// THE ABSENT-BODY ARM — a verdict whose stream carries no output rows prints
	// its own sentence, never an empty line. The two arms are asserted together
	// so "the block prints bodies" cannot pass by printing one sentence for
	// everything.
	if !strings.Contains(report, budgetFailuresBodyAbsentMarker) {
		t.Errorf("the report does not state that a failing test's own output is ABSENT from the stream "+
			"— 'no evidence' and 'no failure' must not read alike (Rule 15):\n%s", report)
	}
	// …and the package verdicts, so a capped test list is never a reader's only
	// account of which packages broke.
	if !strings.Contains(report, budgetFailuresVerdictMarker) {
		t.Errorf("the report does not state which PACKAGES the stream recorded as failed (%q) — the "+
			"test list is capped, the package set is not:\n%s", budgetFailuresVerdictMarker, report)
	}
}
