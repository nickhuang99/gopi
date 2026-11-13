package standardgate

// The heavy-test inventory (testdata/heavy-tests.tsv) is a SNAPSHOT: it was
// derived once, from one measured run's durations (2026-09-16), and the
// inventory gate keeps it truthful about the CALL SITES it lists. Nothing in
// that pair can notice a test that becomes long LATER — and an unclassified
// long test is exactly the test that silently eats the smoke budget: it is not
// in the inventory (so no gate asks about it) and not in .heavy-skipped.tsv (so
// no reviewer sees it either).
//
// This file gates the answer, in two halves:
//
//  1. WIRING (cheap, static): the smoke/merge entry point must REPORT the
//     unclassified slow tests of the run it just paid for; the report must be a
//     real script that names each test AND prints the re-derivation command; the
//     floor and the inventory path have ONE definition, shared by every script
//     and agreeing with the Go constant heavytest.InventoryRel (a renamed
//     inventory would otherwise make the report read zero classified rows and
//     name every heavy test as unclassified — a report that always cries wolf
//     is the same defect as no report).
//  2. BEHAVIOUR (deterministic, no host dependence): the report is RUN against
//     a synthetic stream it must fail to name, and against one it must name.
//     Nothing here measures the real suite: this gate is on the smoke path by
//     definition (internal/standards may never be classified heavy), so it
//     cannot afford to be slow, and a report that is only grepped is a report
//     nobody has ever seen run.

import (
	"go/ast"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	// heavyThresholdLibRel is the ONE definition of the floor + inventory path.
	heavyThresholdLibRel = "scripts/lib/heavy-threshold.sh"
	// heavyBudgetScriptRel is the smoke/merge entry point; it must report drift.
	heavyBudgetScriptRel = "scripts/test-budget.sh"
	// heavyDeriveScriptRel re-derives the inventory from a real run's stream.
	heavyDeriveScriptRel = "scripts/heavy-derive.sh"
	// heavyDriftReportScriptRel names the unclassified slow tests of a stream.
	heavyDriftReportScriptRel = "scripts/heavy-drift-report.sh"
	// heavyReportFuncName is the reporting half inside the budget script.
	heavyReportFuncName = "report_unclassified_slow"
	// heavyFloorShellVar is the floor's shell name, defined once in the lib.
	heavyFloorShellVar = "heavy_slow_threshold_s"
	// heavyInventoryShellVar is the inventory path's shell name.
	heavyInventoryShellVar = "heavy_inventory_rel"
	// heavyGoConstFile is the Go-side single source of the inventory path.
	heavyGoConstFile = "internal/heavytest/heavy.go"
	// heavyGoConstName is that constant.
	heavyGoConstName = "InventoryRel"
	// heavyRederivationDocRel carries the recipe and the flaky disposition.
	heavyRederivationDocRel = "devdoc/heavy-tests-rederivation.md"
	// heavyMeasuredFloor is the floor the 2026-09-16 baseline was derived with
	// (45 tests >=5s of 3553). The lib may not drift from it silently: the
	// inventory's arithmetic is stated in terms of this number.
	heavyMeasuredFloor = "5"
	// heavyUnclassifiedMarker prefixes one named unclassified slow test.
	heavyUnclassifiedMarker = "UNCLASSIFIED"
	// heavyProbeSlowSeconds is the synthetic slow test's duration: above the
	// floor by a wide margin, so the probe cannot be defeated by rounding.
	heavyProbeSlowSeconds = 42.0
	// heavyProbeFastSeconds keeps the probe's classified control test below the
	// floor, so a hit can only come from the test the report should name.
	heavyProbeFastSeconds = 3.0
)

// TestHeavyThresholdReportIsWiredToTheSmokeRun gates the static wiring.
func TestHeavyThresholdReportIsWiredToTheSmokeRun(t *testing.T) {
	root := repoRoot(t)

	lib := readRepoFile(t, root, heavyThresholdLibRel)
	bash := readRepoFile(t, root, heavyBudgetScriptRel)
	derive := readRepoFile(t, root, heavyDeriveScriptRel)
	report := readRepoFile(t, root, heavyDriftReportScriptRel)

	// 1. The floor is defined ONCE, in the shared lib, at the measured value.
	floorRe := regexp.MustCompile(`(?m)^` + heavyFloorShellVar + `=([0-9]+)\s*$`)
	m := floorRe.FindStringSubmatch(lib)
	if m == nil {
		t.Fatalf("%s does not define `%s=<n>` — the classification floor must have one "+
			"definition, or the report and the inventory drift apart", heavyThresholdLibRel, heavyFloorShellVar)
	}
	if m[1] != heavyMeasuredFloor {
		t.Errorf("%s sets %s=%s, but the inventory was derived with a %ss floor (45 tests "+
			">=%ss of 3553 in the 2026-09-16 baseline). If the floor really changed, say so in "+
			"%s and re-derive the inventory from it — never move the number alone",
			heavyThresholdLibRel, heavyFloorShellVar, m[1], heavyMeasuredFloor, heavyMeasuredFloor,
			heavyRederivationDocRel)
	}
	if n := strings.Count(lib, heavyFloorShellVar+"="); n != 1 {
		t.Errorf("%s assigns %s %d times — one definition, sourced by every consumer (Rule 14)",
			heavyThresholdLibRel, heavyFloorShellVar, n)
	}

	// 2. Every script sources that one definition (never re-declares the value).
	for _, s := range []struct{ rel, src string }{
		{heavyBudgetScriptRel, bash},
		{heavyDeriveScriptRel, derive},
		{heavyDriftReportScriptRel, report},
	} {
		if !strings.Contains(s.src, "lib/heavy-threshold.sh") {
			t.Errorf("%s does not source %s — it must take the floor and the inventory path from "+
				"the one definition (a copy here is drift waiting to happen)",
				s.rel, heavyThresholdLibRel)
		}
		if regexp.MustCompile(`(?m)^\s*` + heavyFloorShellVar + `=`).MatchString(s.src) {
			t.Errorf("%s re-declares %s — the shared lib is its only definition (Rule 14)",
				s.rel, heavyFloorShellVar)
		}
	}

	// 3. The smoke/merge entry point REPORTS unclassified slow tests — and the
	//    call is wired in, not merely defined (a definition nobody calls is a
	//    knob documenting behaviour nobody gets).
	if !strings.Contains(bash, heavyReportFuncName+"()") {
		t.Fatalf("%s does not define %s() — an unclassified long test must be NAMED by the run "+
			"that paid for it", heavyBudgetScriptRel, heavyReportFuncName)
	}
	// The report must be CALLED, not merely defined or mentioned. Counting
	// occurrences of the NAME is not enough: a comment or the definition's own
	// line satisfies a count, so a run whose call site was deleted would still
	// read as wired (measured 2026-09-18: removing the call left the count at 2 —
	// comment + definition — and this gate stayed green). The call is a statement
	// on a line of its own.
	calledRe := regexp.MustCompile(`(?m)^\s*` + heavyReportFuncName + `\s*$`)
	if !calledRe.MatchString(bash) {
		t.Errorf("%s never CALLS %s on a line of its own — a report nobody runs leaves the "+
			"inventory silent, which is the defect this closes (a definition, or a mention in a "+
			"comment, is not a call)", heavyBudgetScriptRel, heavyReportFuncName)
	}
	if !strings.Contains(bash, heavyDriftReportScriptRel) {
		t.Errorf("%s does not invoke %s — the report's one definition lives there",
			heavyBudgetScriptRel, heavyDriftReportScriptRel)
	}

	// 4. The report names the test and the way to re-derive, and it tallies the
	//    slow set (a report that cannot say "5 slow, 0 unclassified" cannot be
	//    told apart from one that read nothing).
	if !strings.Contains(report, heavyUnclassifiedMarker) {
		t.Errorf("%s never prints %s — a report must NAME the test it is flagging, or the "+
			"reader has to re-derive the comparison by hand", heavyDriftReportScriptRel, heavyUnclassifiedMarker)
	}
	if !strings.Contains(report, "heavy_derive_cmd") {
		t.Errorf("%s does not print the re-derivation command ($heavy_derive_cmd) — naming a "+
			"long test without saying how to re-derive the list leaves the fix to invention",
			heavyDriftReportScriptRel)
	}

	// 5. The derive script is a real re-derivation, not a stub: it reads a
	//    stream and refuses a narrowed one (that guard is what keeps a partial
	//    run from shrinking the classification to whatever it touched).
	if !strings.Contains(derive, "$stream") {
		t.Errorf("%s does not read a stream — re-derivation must come from a measured run, "+
			"never from taste", heavyDeriveScriptRel)
	}
	if !strings.Contains(derive, heavyInventoryShellVar) {
		t.Errorf("%s does not read the inventory (%s) — it cannot exclude what is already "+
			"classified, so its output would be unactionable", heavyDeriveScriptRel, heavyInventoryShellVar)
	}
	if !strings.Contains(derive, "heavy_stream_tests_floor") {
		t.Errorf("%s does not apply the non-vacuity floor — a stream of a handful of events "+
			"would derive an inventory that shrinks the classification", heavyDeriveScriptRel)
	}
	// A test COUNT cannot tell a whole suite from a narrowed one: measured
	// 2026-09-18, the standards-only stream carried 244 top-level tests, above any
	// affordable floor. Coverage can — the stream must report a result for every
	// package carrying test files, so the guard must be coverage-based.
	if !strings.Contains(derive, "heavy_golist_format") {
		t.Errorf("%s does not enumerate the packages carrying test files (heavy_golist_format) — "+
			"a count floor alone lets a narrowed run re-derive the classification from whatever "+
			"it happened to touch", heavyDeriveScriptRel)
	}
	if !strings.Contains(derive, "reports no result for") {
		t.Errorf("%s does not refuse a stream that is missing test packages — the coverage guard "+
			"is what distinguishes a suite from a slice of one", heavyDeriveScriptRel)
	}

	// 6. The Go side and the shell side agree on the inventory path.
	goPath := heavyInventoryConstValue(t, root)
	shellPathRe := regexp.MustCompile(`(?m)^` + heavyInventoryShellVar + `="([^"]+)"`)
	sm := shellPathRe.FindStringSubmatch(lib)
	if sm == nil {
		t.Fatalf("%s does not define %s=\"…\" — the report needs the inventory path from the "+
			"shared definition", heavyThresholdLibRel, heavyInventoryShellVar)
	}
	if sm[1] != goPath {
		t.Errorf("inventory path drift: %s says %q while %s's %s says %q — one of the two sides "+
			"reads a file that is not the inventory", heavyThresholdLibRel, sm[1],
			heavyGoConstFile, heavyGoConstName, goPath)
	}

	// 7. The gate packages the scripts exclude must be the same set the Go gate
	//    protects: a script that forgot one would report its tests as
	//    unclassified — and they can never be classified, so the report would
	//    name a test nobody may act on (noise that teaches a reader to ignore it).
	for _, pkg := range []string{"internal/standards", "internal/stylegate"} {
		if !strings.Contains(lib, pkg) {
			t.Errorf("%s does not exclude the gate package %s — tests there may NEVER be "+
				"classified heavy (internal/standards' inventory gate fails on it), so the "+
				"report must not name them as unclassified", heavyThresholdLibRel, pkg)
		}
	}

	// 8. The recipe and the flaky disposition live in the doc this gate names.
	doc := readRepoFile(t, root, heavyRederivationDocRel)
	if !strings.Contains(doc, "flaky") || !strings.Contains(doc, "CriterionHostCondition") {
		t.Errorf("%s does not disposition the flaky class against a criterion — the class is "+
			"either named as covered (CriterionHostCondition) or filed as its own task; an "+
			"unstated class is the drift this task exists to end", heavyRederivationDocRel)
	}
}

// TestHeavyDriftReportNamesAnUnclassifiedSlowTest runs the report against a
// synthetic stream, so the naming behaviour is checked rather than grepped.
//
// It is deliberately tiny: the gate package must stay on the smoke path, so the
// probe is a 40-line JSON stream, not a suite run.
func TestHeavyDriftReportNamesAnUnclassifiedSlowTest(t *testing.T) {
	root := repoRoot(t)
	stream := filepath.Join(t.TempDir(), "probe.json")

	// Three top-level tests: one slow and unclassified (must be named), one slow
	// but classified (must NOT be named), one fast (below the floor).
	const (
		classifiedPkg = "github.com/nick/gopi/internal/tools"
		slowPkg       = "github.com/nick/gopi/internal/agent"
		sentinelTest  = "TestProbeUnclassifiedSlow"
		classifiedT   = "TestProbeClassifiedSlow"
		fastT         = "TestProbeFast"
	)
	classifiedName := ""
	for _, line := range strings.Split(readRepoFile(t, root, "testdata/heavy-tests.tsv"), "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) >= 3 && strings.HasPrefix(cols[2], "internal/tools/") {
			classifiedName = cols[0]
			break
		}
	}
	if classifiedName == "" {
		t.Skip("no internal/tools row in the inventory to use as the classified control")
	}

	writeProbeStream(t, stream, []probeTest{
		{pkg: slowPkg, test: sentinelTest, sec: heavyProbeSlowSeconds},
		{pkg: classifiedPkg, test: classifiedName, sec: heavyProbeSlowSeconds},
		{pkg: slowPkg, test: fastT, sec: heavyProbeFastSeconds},
	})

	out := runHeavyDriftReport(t, root, stream)
	if !strings.Contains(out, sentinelTest) {
		t.Errorf("the report did not name the unclassified %0.0fs test %s:\n%s\n"+
			"An unclassified long test is what silently eats the smoke budget — the report "+
			"exists to name it", heavyProbeSlowSeconds, sentinelTest, out)
	}
	if !strings.Contains(out, heavyUnclassifiedMarker) {
		t.Errorf("the report did not mark the hit with %q:\n%s", heavyUnclassifiedMarker, out)
	}
	if strings.Contains(out, classifiedName) {
		t.Errorf("the report named %s, which IS in the inventory — a report that flags "+
			"classified tests is noise a reader learns to ignore:\n%s", classifiedName, out)
	}
	if strings.Contains(out, fastT) {
		t.Errorf("the report named %s (%0.0fs, below the floor) — the threshold is not applied:\n%s",
			fastT, heavyProbeFastSeconds, out)
	}
	if out := runHeavyDriftReport(t, root, stream); !strings.Contains(out, "at or above it") {
		t.Errorf("the report printed no slow-set tally:\n%s", out)
	}
}

// TestHeavyDriftReportAgreesWithTheInventoryAlone is the negative control: with
// only classified slow tests, the report must name NOBODY (otherwise its hits
// mean nothing).
func TestHeavyDriftReportAgreesWithTheInventoryAlone(t *testing.T) {
	root := repoRoot(t)
	var rows []probeTest
	for _, line := range strings.Split(readRepoFile(t, root, "testdata/heavy-tests.tsv"), "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 3 || !strings.HasSuffix(cols[2], "_test.go") {
			continue
		}
		rows = append(rows, probeTest{
			pkg:  "github.com/nick/gopi/" + path.Dir(cols[2]),
			test: cols[0],
			sec:  heavyProbeSlowSeconds,
		})
	}
	if len(rows) == 0 {
		t.Fatal("the inventory has no rows to build the control from")
	}
	stream := filepath.Join(t.TempDir(), "control.json")
	writeProbeStream(t, stream, rows)
	out := runHeavyDriftReport(t, root, stream)
	if strings.Contains(out, heavyUnclassifiedMarker) {
		t.Errorf("the report flagged tests that ARE in the inventory:\n%s", out)
	}
}

// probeTest is one synthetic test event in a probe stream.
type probeTest struct {
	pkg  string
	test string
	sec  float64
}

// writeProbeStream writes a minimal `go test -json` stream: package-level
// start/run events plus one run+pass pair per test, which is the shape the
// report parses.
func writeProbeStream(t *testing.T, path string, tests []probeTest) {
	t.Helper()
	var b strings.Builder
	pkgSeen := map[string]bool{}
	for _, pt := range tests {
		if !pkgSeen[pt.pkg] {
			pkgSeen[pt.pkg] = true
			b.WriteString(`{"Action":"start","Package":"` + pt.pkg + `"}` + "\n")
			b.WriteString(`{"Action":"run","Package":"` + pt.pkg + `","Test":"` + pt.test + `"}` + "\n")
		}
		b.WriteString(`{"Action":"run","Package":"` + pt.pkg + `","Test":"` + pt.test + `","Elapsed":0}` + "\n")
		b.WriteString(`{"Action":"pass","Package":"` + pt.pkg + `","Test":"` + pt.test + `","Elapsed":` +
			strconv.FormatFloat(pt.sec, 'f', 2, 64) + `}` + "\n")
	}
	for pkg := range pkgSeen {
		b.WriteString(`{"Action":"pass","Package":"` + pkg + `","Elapsed":1.0}` + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write probe stream: %v", err)
	}
}

// runHeavyDriftReport runs the report over a stream and returns its output. The
// report is a script in the repo under test: it must run from the checked-out
// worktree, not from a copy.
func runHeavyDriftReport(t *testing.T, root, stream string) string {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(root, heavyDriftReportScriptRel), stream)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s: %v\n%s", heavyDriftReportScriptRel, err, out)
	}
	return string(out)
}

// heavyInventoryConstValue reads the Go-side inventory path constant, so the
// shell definition can be checked against the single source the inventory gate
// itself reads.
func heavyInventoryConstValue(t *testing.T, root string) string {
	t.Helper()
	p := parseRepoFile(t, root, heavyGoConstFile)
	value := ""
	for _, decl := range p.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != heavyGoConstName || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if unq, err := strconv.Unquote(lit.Value); err == nil {
					value = unq
				}
			}
		}
	}
	if value == "" {
		t.Fatalf("%s has no %s string constant — the inventory's Go-side single source is gone, "+
			"so nothing can check the report against it", heavyGoConstFile, heavyGoConstName)
	}
	return value
}

// readRepoFile reads a repo-relative file or fails the gate loudly (a missing
// script must fail, never read as "nothing to report").
func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v — the heavy-classification report's wiring is gone; an "+
			"unclassified long test would then eat the smoke budget unnamed", rel, err)
	}
	return string(b)
}
