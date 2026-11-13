package testutil

// THE FALSE-GREEN GATE: `go test -run <a name that matches nothing>` is not a pass.
//
// `go test -run X` prints `testing: warning: no tests to run` and EXITS 0 when X
// matches no test at all, so a mistyped test name and a real pass are the same
// thing to every consumer of the exit status — `PASS`, `ok <pkg> 3.4s`, code 0,
// with the only difference a suffix on stdout that nothing on the merge/verify
// path reads. Measured (bug row 01a12cbe-324f-75a6-860e-f32ba6a7ce39, evidence
// artifacts/false-green-no-tests-to-run.out): a real test `--- PASS … 33.58s`
// vs a typo of it `ok … 3.400s [no tests to run]`, exit 0 in both cases. The
// repo had NOTHING watching the marker string (`grep -rn "no tests to run"
// internal/ scripts/ cmd/` → 0 hits before this change).
//
// scripts/test-budget.sh is the declared entry point on that path (make test /
// make test-budget route through it), so the discrimination lives there: a
// FILTERED run that executed ZERO tests gets its own exit code and a banner
// naming the filter. This file is the gate that keeps it — a verdict nothing
// checks is a verdict that silently disappears the first time someone
// "simplifies" the script's tail.
//
// THREE THINGS ARE ASSERTED, and each covers a hole the others cannot:
//
//	(a) the clause, EXECUTED against the real repo through the real entry point:
//	    a real test name ⇒ 0 with the executed count, an impossible name ⇒ 5 with
//	    the banner. Statement of the verdict, on the real surface (Rule 31);
//	(b) the marker SENTENCE is Go's, derived from the script's own constant and
//	    then looked for in a `go test -json` stream this test produces itself, so
//	    an upstream rewording is a RED test rather than a verdict that quietly
//	    stopped firing (the failure mode a hardcoded string would hide);
//	(c) the same script in a FIXTURE module — the verdict is a property of the
//	    script, not of this repo's package layout.

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/scratch"
)

const (
	// budgetNoMatchExit is the script's dedicated code for "a filtered run
	// executed zero tests" — distinct from 1 (failure) and 3 (budget trip).
	budgetNoMatchExit = 5
	// budgetNoMatchBanner is the headline the script must print for it.
	budgetNoMatchBanner = "NO TEST EXECUTED, THE FILTER MATCHED NOTHING"
	// budgetNoMatchOutcome is the stamp's outcome word, so a reader of the stamp
	// log sees the same verdict without reading the body.
	budgetNoMatchOutcome = "no-test-executed"
	// budgetNoMatchMarkerConstRe derives the marker sentence from the script's
	// single declaration — never re-spelled in this file (Rule 1b/13).
	budgetNoMatchMarkerConstRe = `(?m)^no_tests_marker="([^"]+)"`
	// budgetNoMatchExecutedActionConstRe derives the stream action the script
	// counts from its own declaration, the same way.
	budgetNoMatchExecutedActionConstRe = `(?m)^executed_action="([^"]+)"`
	// budgetNoMatchExecutedLine is the summary line carrying the executed count.
	budgetNoMatchExecutedLine = "executed: "
	// budgetNoMatchExecutedZero / …One are the two readings asserted below.
	budgetNoMatchExecutedZero = budgetNoMatchExecutedLine + "0 test(s) ran"
	budgetNoMatchExecutedOne  = budgetNoMatchExecutedLine + "1 test(s) ran"
	// budgetNoMatchFilterLine is the summary line naming the filter in force.
	budgetNoMatchFilterLine = "filter:   "
	// budgetNoMatchFailureBlock heads the assertion-failure block; a run that
	// executed nothing must NOT be presented through it (the reader would take
	// it for a failing assertion).
	budgetNoMatchFailureBlock = "--- failures (assertion/build"
	// budgetNoMatchTestPkg is the repo package the executed arms run against:
	// tiny, dependency-free, and already the smoke package the merge path uses.
	budgetNoMatchTestPkg = "./internal/smokegate/"
	// budgetNoMatchRealTest is a test that EXISTS in that package.
	budgetNoMatchRealTest = "TestCommandIsNotTheWholeTree"
	// budgetNoMatchImpossibleTest is a filter no test will ever match. It is
	// spelled like a real name on purpose: that is the live typo shape.
	budgetNoMatchImpossibleTest = "TestBudgetNoSuchTestEverExists"
	// budgetNoMatchRawJSONLine is the script's line naming this run's stream.
	budgetNoMatchRawJSONLine = "raw json: "
	// budgetNoMatchStampLine is the script's stamp line prefix.
	budgetNoMatchStampLine = "stamp: "

	// --- (c) the fixture module ---
	// budgetNoMatchFixtureModule is the fixture's module path: a name no real
	// module uses, so a fixture package can never be mistaken for a repo one.
	budgetNoMatchFixtureModule = "example.test/notestsfixture"
	// budgetNoMatchFixturePkgDir is the one package the fixture carries.
	budgetNoMatchFixturePkgDir = "internal/foo"
	// budgetNoMatchFixtureTest is the fixture's real test name.
	budgetNoMatchFixtureTest = "TestFixturePackageRuns"
	// budgetNoMatchGoModPath is the module marker of both the repo and the
	// fixture.
	budgetNoMatchGoModPath = "go.mod"
	// budgetNoMatchScriptsRoot is the repo directory the entry point lives in —
	// the staged closure never leaves it.
	budgetNoMatchScriptsRoot = "scripts"
	// budgetNoMatchMentionRe finds a repo-relative path to another file under
	// scripts/ inside a script's own text — the ONE shape this repo's scripts
	// use to name a sibling: a `source`/`.` line, a variable assignment
	// (`stream_scope_lib="scripts/lib/stream-scope.sh"`), or a quoted
	// `$root/$x`. The fixture's closure is DERIVED from it, so a newly sourced
	// library travels into the fixture without a list anyone must maintain.
	budgetNoMatchMentionRe = `scripts/[A-Za-z0-9_./-]+`
	// budgetNoMatchClosureCap bounds the closure walk: a mention graph that
	// expanded without end would hang the suite instead of failing it.
	budgetNoMatchClosureCap = 256
	// budgetNoMatchMentionTrailingCut trims the punctuation a path can abut in
	// prose (`scripts/lib/x.sh)` / `scripts/lib/x.sh,`).
	budgetNoMatchMentionTrailingCut = `.,;:)"'`
	// budgetNoMatchGoExt is the suffix that decides which staged files the
	// fixture's `./...` pattern will actually try to build.
	budgetNoMatchGoExt = ".go"
)

// TestBudgetFilterMatchingNoTestIsNotAPass is the acceptance gate, run against
// the real repo through the real entry point.
func TestBudgetFilterMatchingNoTestIsNotAPass(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}

	// The red arm and the green arm differ in ONE character-class: a name that
	// exists vs a name that cannot. Everything else (package, flags, script) is
	// identical, which is what makes the pair a measurement.
	t.Run("no-match", func(t *testing.T) {
		out, code := budgetRunThroughEntryPoint(t, root, "-run", "^"+budgetNoMatchImpossibleTest+"$", budgetNoMatchTestPkg)
		if code != budgetNoMatchExit {
			t.Errorf("a filtered run that executed no test exited %d, want %d — Go exits 0 here, so an "+
				"exit code equal to a pass is the false green this gate exists to remove\n%s",
				code, budgetNoMatchExit, out)
		}
		for _, want := range []string{
			budgetNoMatchBanner,
			budgetNoMatchExecutedZero,
			budgetNoMatchFilterLine + "-run ^" + budgetNoMatchImpossibleTest + "$",
			budgetNoMatchOutcome,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("the run's output does not carry %q — a verdict a reader cannot read is not a "+
					"verdict (Rule 15)\n%s", want, out)
			}
		}
		if strings.Contains(out, budgetNoMatchFailureBlock) {
			t.Errorf("the run presented itself as an assertion/build failure (%q) — 'nothing ran' and "+
				"'the assertion failed' are different findings and must not share a block\n%s",
				budgetNoMatchFailureBlock, out)
		}
	})

	t.Run("match", func(t *testing.T) {
		out, code := budgetRunThroughEntryPoint(t, root, "-run", "^"+budgetNoMatchRealTest+"$", budgetNoMatchTestPkg)
		if code != 0 {
			t.Errorf("a filtered run of a test that EXISTS exited %d, want 0 — the emptiness verdict must "+
				"not fire when something ran\n%s", code, out)
		}
		if !strings.Contains(out, budgetNoMatchExecutedOne) {
			t.Errorf("the run's summary does not report %q — the count of tests that actually ran is what "+
				"tells a reader this green is real\n%s", budgetNoMatchExecutedOne, out)
		}
		if strings.Contains(out, budgetNoMatchBanner) {
			t.Errorf("a run that executed a test printed %q\n%s", budgetNoMatchBanner, out)
		}
	})
}

// TestBudgetNoTestsMarkerIsStillWhatGoPrints binds the script's constant to the
// sentence Go itself emits. The script keys the whole verdict on that string, so
// a reworded upstream message would switch the verdict off SILENTLY — here it is
// a failing test instead. The assertion is deliberately two-sided: the marker is
// read from the script (its one definition) and then looked for in a stream this
// test produces with the same filter the script's red arm uses.
func TestBudgetNoTestsMarkerIsStillWhatGoPrints(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	script, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(budgetScriptPath)))
	if err != nil {
		t.Fatalf("read %s: %v", budgetScriptPath, err)
	}
	m := regexp.MustCompile(budgetNoMatchMarkerConstRe).FindStringSubmatch(string(script))
	if m == nil {
		t.Fatalf("%s declares no `no_tests_marker=\"…\"` — the empty-run verdict's trigger must live in "+
			"ONE named constant (Rule 1a/13)", budgetScriptPath)
	}
	marker := m[1]
	am := regexp.MustCompile(budgetNoMatchExecutedActionConstRe).FindStringSubmatch(string(script))
	if am == nil {
		t.Fatalf("%s declares no `executed_action=\"…\"` — the count of executed tests must read ONE "+
			"named stream action (Rule 1a/13)", budgetScriptPath)
	}
	executedAction := am[1]

	// The probe runs from the MODULE ROOT: a package pattern is resolved against
	// the working directory, and this test's directory is a package, not the
	// module (measured while writing this gate: `./internal/smokegate/` run from
	// internal/testutil resolves to internal/testutil/internal/smokegate and the
	// stream carries a build failure instead of the marker).
	probe := exec.Command("go", "test", "-count=1", "-json",
		"-run", "^"+budgetNoMatchImpossibleTest+"$", budgetNoMatchTestPkg)
	probe.Dir = root
	raw, err := probe.Output()
	// Output() returns the bytes even on a non-zero exit, and a non-zero here
	// would itself be interesting — report it rather than swallow it (Rule 15b).
	var exitErr *exec.ExitError
	if err != nil && !budgetAsExitError(err, &exitErr) {
		t.Fatalf("run the streaming probe: %v", err)
	}
	stream := string(raw)
	if exitErr != nil {
		t.Logf("the probe exited %d; its stream is still read below (a build failure would show as a "+
			"missing marker)", exitErr.ExitCode())
	}
	if !strings.Contains(stream, marker) {
		t.Errorf("the script's marker constant %q does not occur in a `go test -json -run "+
			"^%s$ %s` stream — Go's message changed, so the script's empty-run verdict would never "+
			"fire again and a mistyped filter would read as a pass once more (Rule 15: this must be a "+
			"red test, not a silent switch-off)\nstream:\n%s",
			marker, budgetNoMatchImpossibleTest, budgetNoMatchTestPkg, budgetFirstLines(stream, 12))
	}
	// The count the script takes must stay derivable from the stream shape, and
	// for this impossible filter it must read ZERO — a non-zero reading would
	// mean the count is measuring something other than execution.
	if got := streamExecutedTests(stream, executedAction); got != 0 {
		t.Errorf("a stream for the impossible filter %q reports %d executed test(s), want 0 — the "+
			"script's count must read the `run` action and nothing else\n%s",
			budgetNoMatchImpossibleTest, got, budgetFirstLines(stream, 12))
	}
}

// TestBudgetNoTestsVerdictRunsInAFixtureModule executes the SAME script inside a
// throwaway module: the verdict must come from the script, not from this repo's
// packages. The fixture carries one package with one ordinary test, so the two
// arms differ only by the filter — the exact shape a caller meets.
func TestBudgetNoTestsVerdictRunsInAFixtureModule(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	tree := budgetFixtureModule(t, root)

	out, code := budgetRunThroughEntryPoint(t, tree, "-run", "^"+budgetNoMatchImpossibleTest+"$", "./...")
	if code != budgetNoMatchExit {
		t.Errorf("in a fixture module, a filtered run that executed no test exited %d, want %d\n%s",
			code, budgetNoMatchExit, out)
	}
	if !strings.Contains(out, budgetNoMatchBanner) {
		t.Errorf("the fixture run printed no %q\n%s", budgetNoMatchBanner, out)
	}

	out, code = budgetRunThroughEntryPoint(t, tree, "-run", "^"+budgetNoMatchFixtureTest+"$", "./...")
	if code != 0 {
		t.Errorf("in a fixture module, the fixture's real test exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, budgetNoMatchExecutedOne) {
		t.Errorf("the fixture run of the fixture's own test does not report %q\n%s", budgetNoMatchExecutedOne, out)
	}
}

// budgetRunThroughEntryPoint executes the repo's declared entry point (Rule 14:
// the script, never a re-invented `go test`) in dir, with its artifacts isolated
// to this test (Rule 22 + the artifact-isolation the concurrent gate documents —
// a shared scratch root makes a neighbour's stream readable as this run's own),
// and returns its combined output plus exit code.
func budgetRunThroughEntryPoint(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	t.Setenv(scratch.HomeEnvName, t.TempDir())
	var combined strings.Builder
	cmd := exec.Command(budgetShellBin, filepath.Join(dir, filepath.FromSlash(budgetScriptPath)))
	cmd.Args = append(cmd.Args, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), scratch.HomeEnvName+"="+os.Getenv(scratch.HomeEnvName))
	cmd.Stdout, cmd.Stderr = &combined, &combined
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if err != nil {
		if !budgetAsExitError(err, &exitErr) {
			t.Fatalf("run %s in %s: %v\n%s", budgetScriptPath, dir, err, combined.String())
		}
		code = exitErr.ExitCode()
	}
	// The run must name the stream it summarised from; a run whose evidence
	// cannot be located is not evidence (Rule 30's spirit, one level up).
	if !strings.Contains(combined.String(), budgetNoMatchRawJSONLine) {
		t.Fatalf("the run printed no %q line — its artifact contract changed\n%s",
			budgetNoMatchRawJSONLine, combined.String())
	}
	if !strings.Contains(combined.String(), budgetNoMatchStampLine) {
		t.Fatalf("the run printed no %q line — the stamp is how a reader ties a verdict to a run\n%s",
			budgetNoMatchStampLine, combined.String())
	}
	return combined.String(), code
}

// budgetFixtureModule builds a throwaway module carrying the repo's OWN scripts
// tree (the entry point sources scripts/lib/*, so its closure travels with it)
// and one package with one ordinary test. The module path is a name no real
// module uses, so its packages can never be confused with the repo's.
func budgetFixtureModule(t *testing.T, root string) string {
	t.Helper()
	tree := t.TempDir()
	budgetWriteFile(t, filepath.Join(tree, budgetNoMatchGoModPath),
		"module "+budgetNoMatchFixtureModule+"\n\ngo "+budgetGoDirective(t, root)+"\n")
	budgetWriteFile(t, filepath.Join(tree, filepath.FromSlash(budgetNoMatchFixturePkgDir), "foo.go"),
		"// Package foo is the fixture's only package.\npackage foo\n")
	budgetWriteFile(t, filepath.Join(tree, filepath.FromSlash(budgetNoMatchFixturePkgDir), "foo_test.go"),
		"package foo\n\nimport \"testing\"\n\n// "+budgetNoMatchFixtureTest+" exists so the fixture can be run green.\n"+
			"func "+budgetNoMatchFixtureTest+"(t *testing.T) { t.Log(\"the fixture package ran\") }\n")
	budgetStageScriptsTree(t, tree, root)
	return tree
}

// budgetStageScriptsTree stages the entry point's OWN source closure — its
// script file plus every file under scripts/ that those files themselves name —
// and NOT the whole scripts/ tree.
//
// WHY IT IS NARROW (measured on this ground's base 2d6c755, evidence
// remeasure-base.txt): the whole tree carries scripts/lib/ledgersql/ledgersql.go,
// whose `_ "modernc.org/sqlite"` import (added by 55cfeebe) cannot resolve in a
// fixture module that has no `require` block, so `./...` inside the fixture died
// at BUILD with
//
//	scripts/lib/ledgersql/ledgersql.go:35:2: no required module provides
//	package modernc.org/sqlite
//
// and the fixture's green arm (a test that DOES exist) read 1 — a red that says
// nothing about the verdict under test. The entry point needs none of that file.
// Rule 35 decides which side gives way: the fixture must NOT grow a `require`
// (that would make this gate depend on a warm module cache and on a 800MB
// artefact), so the STAGED CODE has to be the closure. The closure is DERIVED
// from the scripts' own text rather than listed by hand, so a newly sourced
// library travels into the fixture by itself and this fixture cannot silently
// drift into under-staging when the entry point gains a dependency.
func budgetStageScriptsTree(t *testing.T, tree, root string) {
	t.Helper()
	mention := regexp.MustCompile(budgetNoMatchMentionRe)
	queued := []string{budgetScriptPath}
	seen := map[string]bool{}
	staged := 0
	for len(queued) > 0 {
		if len(seen) >= budgetNoMatchClosureCap {
			t.Fatalf("the entry point's staged closure exceeded %d files — the mention graph must "+
				"converge, and a walk that does not is a bug in this fixture, not a slow test",
				budgetNoMatchClosureCap)
		}
		rel := queued[0]
		queued = queued[1:]
		if seen[rel] {
			continue
		}
		seen[rel] = true
		srcPath := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(srcPath)
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			// A prose mention of a path that is not a regular file is not a
			// dependency of the closure; the entry point's own absence is caught
			// by the check below, which is fatal.
			continue
		}
		raw, err := os.ReadFile(srcPath)
		if err != nil {
			t.Fatalf("read %s out of the repo's closure: %v", rel, err)
		}
		budgetWriteFileMode(t, filepath.Join(tree, filepath.FromSlash(rel)), string(raw), info.Mode().Perm())
		staged++
		for _, m := range mention.FindAllString(string(raw), -1) {
			queued = append(queued, strings.TrimRight(m, budgetNoMatchMentionTrailingCut))
		}
	}
	if staged == 0 {
		t.Fatalf("staged 0 files from the entry point's closure — a fixture with no scripts must be a " +
			"loud failure, not a vacuous pass")
	}
	if _, serr := os.Stat(filepath.Join(tree, filepath.FromSlash(budgetScriptPath))); serr != nil {
		t.Fatalf("the staged tree carries no %s: %v (the entry point must be the repo's own file)",
			budgetScriptPath, serr)
	}
	// The staged Go files are the ones `./...` will try to build, so their count
	// is what tells a reader whether the fixture is still module-resolvable with
	// no `require` block (Rule 15: an environment-dependent green is named).
	if goFiles := budgetStagedGoFiles(t, tree); goFiles != 0 {
		t.Logf("the staged closure carries %d Go file(s) under %s/; the fixture module declares no "+
			"`require`, so any third-party import among them would fail the build", goFiles,
			budgetNoMatchScriptsRoot)
	}
}

// budgetStagedGoFiles counts the .go files staged under <tree>/scripts — the
// population `go test ./...` resolves against the fixture's bare go.mod.
func budgetStagedGoFiles(t *testing.T, tree string) int {
	t.Helper()
	n := 0
	rootDir := filepath.Join(tree, filepath.FromSlash(budgetNoMatchScriptsRoot))
	err := filepath.WalkDir(rootDir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(d.Name()) == budgetNoMatchGoExt {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the staged %s tree: %v", budgetNoMatchScriptsRoot, err)
	}
	return n
}

// budgetGoDirective reads the repo's own `go` directive so the fixture declares
// the same toolchain the repo does (never a drifted copy).
func budgetGoDirective(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, budgetNoMatchGoModPath))
	if err != nil {
		t.Fatalf("read %s: %v", budgetNoMatchGoModPath, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	t.Fatalf("%s declares no go directive", budgetNoMatchGoModPath)
	return ""
}

func budgetWriteFile(t *testing.T, path, body string) {
	t.Helper()
	budgetWriteFileMode(t, path, body, budgetWriteFileModeDefault)
}

// budgetWriteFileModeDefault is the mode a fixture file that is not executed
// gets; the staged scripts keep their own (a lost +x bit would make the fixture a
// different experiment).
const budgetWriteFileModeDefault = 0o644

// budgetWriteFileMode writes body to path with an explicit permission, so the
// staged entry point stays executable.
func budgetWriteFileMode(t *testing.T, path, body string, perm fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), perm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// streamExecutedTests reads "how many tests did this stream actually RUN" — one
// `run` action per test as it starts, so the number is Go's own report. A
// skipped test never emits one, which is what keeps "ran" apart from "selected".
// streamExecutedTests counts the SAME text fragment the script counts
// (`"Action":"<action>","Package"`) — the point is that the gate reads the
// script's declared action, so a stream-shape change is caught here rather than
// silently zeroing the script's count.
func streamExecutedTests(stream, action string) int {
	fragment := `"Action":"` + action + `","Package"`
	n := 0
	for _, line := range strings.Split(stream, "\n") {
		if strings.Contains(line, fragment) {
			n++
		}
	}
	return n
}

// budgetAsExitError narrows err to *exec.ExitError without importing errors in
// this file's helper set (the same local shape the worker-launcher fixture
// uses).
func budgetAsExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// budgetFirstLines keeps an error message readable when it quotes a whole stream.
func budgetFirstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
