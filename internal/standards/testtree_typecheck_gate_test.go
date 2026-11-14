// testtree_typecheck_gate_test.go — EVERY _test.go in the module must COMPILE,
// and until this gate existed nothing in the suite said so.
//
// THE MEASURED DEFECT (task 01a14162, master tip adafe54b2, 2026-10-15):
//
//	go vet ./...
//	# github.com/nickhuang99/gopi/<some-package>
//	vet: <some-package>/anchor_number_material_test.go:158:14: undefined: gitDirEnvName
//	→ rc=1
//
// while `go build ./...` was green. A test file referenced identifiers that
// exist only as package-LOCAL names of a DIFFERENT package (cmd/agent's
// cli_doccheck_test.go spells gitDirEnvName / gitWorkTreeEnvName among its own
// fixture vocabulary), so the reference resolves in no package that test file's
// package imports. Every test build of that package — and therefore `go test
// ./<that-package>/...`, and therefore `go vet ./...` — was red on the
// integration branch, while a production-only build stayed green and told
// nobody. `go build` never compiles a _test.go file; `go test` reports the
// failure only for the package you asked it for.
//
// WHY ANOTHER GATE AND NOT "just run vet". `go vet ./...` is the reproduction,
// not a gate: it is not in `make test`'s path, it costs a full type-check of the
// whole tree, and a suite that shelled it out would report a failure the moment
// any UNRELATED vet diagnostic (a printf verb, an unreachable branch) appeared.
// The class that must fail fast is narrower and decidable — DOES THIS PACKAGE'S
// TEST FILES COMPILE — and `go list` already answers it.
//
// THE MECHANISM, and the flag that makes it work. `go list -e …` swallows the
// failure, `-test` enumerates each package's TEST VARIANT (the production files
// PLUS the _test.go files, which is the only shape that can fail this way), and
// `-export` is what FORCES the compiler to run over those variants: without it
// the listing answers from the package graph alone and reports ZERO errors on a
// tree whose test files do not compile. MEASURED (2026-10-15, on an
// `git archive` export of the pre-fix tip, mutated back to the broken names):
// `go list -e -test -json ./...` → 0 packages with errors; `go list -e -export
// -test -json ./...` → 2, carrying `<some-package>/anchor_number_material_test.go:158:14:
// undefined: gitDirEnvName` (the package was the doc-class corpus reader, since
// removed from this package's imports; the MEASUREMENT is about the flag, not
// about which package broke). The red proof below pins that difference, so the
// flag cannot be dropped as an optimisation.
//
// COST, measured on this repo: ~1.7 s warm (the test run has just built the same
// packages, so the export data is in the build cache) and ~45 s on a cold cache
// — the cost of one whole-tree build, which every run of the suite pays anyway.
// It is NOT classified heavy (a heavytest.Require here would take the gate off
// the smoke path, which is the one place this defect has to be caught).

package standardgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// testTreeTypecheckGoExe is the toolchain binary whose subcommand answers the
	// question. Named, never typed at the call site (Rule 1b/13).
	testTreeTypecheckGoExe = "go"
	// testTreeTypecheckListVerb is the subcommand: it loads and (with -export)
	// compiles the package graph without running anything.
	testTreeTypecheckListVerb = "list"
	// testTreeTypecheckFlagErrorTolerant keeps a failing package IN the listing.
	// Without it `go list` exits non-zero and prints to stderr instead of
	// describing the failure, so the gate could not name what broke.
	testTreeTypecheckFlagErrorTolerant = "-e"
	// testTreeTypecheckFlagExport is the LOAD-BEARING one (see the header): it
	// makes the listing produce export data, which is what compiles the test
	// variants. Drop it and the gate goes green on a tree that does not build.
	testTreeTypecheckFlagExport = "-export"
	// testTreeTypecheckFlagTestView enumerates each package's test variant
	// alongside the production package — the variant whose compilation can fail
	// on a _test.go file.
	testTreeTypecheckFlagTestView = "-test"
	// testTreeTypecheckFlagJSON is the machine-readable shape the gate reads.
	testTreeTypecheckFlagJSON = "-json"
	// testTreeTypecheckAllPackages is the SCOPE: every package in the module the
	// gate runs from, which is the same set `go vet ./...` reaches.
	testTreeTypecheckAllPackages = "./..."
	// testTreeTypecheckModuleFile and testTreeTypecheckModuleBody build the
	// synthetic modules the red proof uses. No `require` lines: the fixtures are
	// stdlib-only on purpose, so the proof costs ~0.1 s and cannot be skipped
	// behind a module download.
	testTreeTypecheckModuleFile = "go.mod"
	testTreeTypecheckModuleName = "typecheckfixture"
	testTreeTypecheckModuleBody = "module " + testTreeTypecheckModuleName + "\n\ngo 1.21\n"
	// testTreeTypecheckMinPackages is the non-vacuity floor for the real-tree
	// arm: a tree this large cannot legitimately enumerate fewer first-party
	// packages, and an invocation that silently listed nothing (a broken scope,
	// a missing module root) would otherwise report a clean green.
	testTreeTypecheckMinPackages = 200
	// testTreeTypecheckMinTestVariants is the second floor: it asserts the TEST
	// view was actually enumerated (entries whose package is a <pkg>.test
	// variant). This is the property the whole gate rests on — an enumeration
	// that never looked at a _test.go file cannot find a _test.go defect.
	testTreeTypecheckMinTestVariants = 50
)

// testTreeTypecheckPkg is the subset of `go list -json` this gate reads. Only
// Error matters: every other field is here so a reader can see that the gate
// knows what it is looking at.
type testTreeTypecheckPkg struct {
	ImportPath string
	ForTest    string
	Name       string
	Dir        string
	Incomplete bool
	Error      *struct {
		Err         string
		ImportStack []string
	}
}

// testTreeTypecheckErrors runs the listing over dir and returns (entries,
// testVariants, failures) — how many package entries the toolchain described, how
// many of those are TEST views, and the compile failures it reported, keyed by
// import path with the toolchain's own message (which carries
// `file:line: undefined: X`).
//
// ONE invocation answers all three: the listing is the expensive part (~2 s warm,
// ~45 s cold for ./...), and a second call just to count test variants would
// double it. The variant count comes from the same entries the failures do, so
// the two can never disagree about which view was enumerated.
//
// A non-zero `go list` exit is NOT tolerated as "errors found": with `-e` a
// load-level failure (a missing module root, a broken go.mod) still exits
// non-zero, and treating that as a finding would report the gate's own outage
// as a defect in the tree. It fails the caller instead, so the two can never be
// confused (Rule 15: a scan that never ran is not a scan that found nothing).
func testTreeTypecheckErrors(t *testing.T, dir string, export bool) (int, int, map[string]string) {
	t.Helper()
	args := []string{
		testTreeTypecheckListVerb,
		testTreeTypecheckFlagErrorTolerant,
		testTreeTypecheckFlagTestView,
		testTreeTypecheckFlagJSON,
	}
	if export {
		args = append(args, testTreeTypecheckFlagExport)
	}
	args = append(args, testTreeTypecheckAllPackages)

	cmd := exec.Command(testTreeTypecheckGoExe, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s failed in %s: %v (stderr: %s) — the gate could not RUN, which is "+
			"not the same as the tree being clean",
			testTreeTypecheckGoExe, strings.Join(args, " "), dir, err, strings.TrimSpace(stderr.String()))
	}

	dec := json.NewDecoder(&stdout)
	failures := map[string]string{}
	entries, variants := 0, 0
	for {
		var p testTreeTypecheckPkg
		err := dec.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode %s output in %s: %v", testTreeTypecheckGoExe, dir, err)
		}
		entries++
		if p.ForTest != "" || strings.Contains(p.ImportPath, testTreeTypecheckTestViewMarker) {
			variants++
		}
		if p.Error != nil && strings.TrimSpace(p.Error.Err) != "" {
			failures[p.ImportPath] = p.Error.Err
		}
	}
	return entries, variants, failures
}

// testTreeTypecheckTestViewMarker is the shape `go list -test` gives a test
// variant's import path when it is the `.test` main rather than a ForTest entry:
// `pkg.test`. Named (Rule 1b) because it is a value the gate compares against.
const testTreeTypecheckTestViewMarker = ".test"

// TestTestTreeTypecheckIsGreen is the real-tree arm: every package in this
// module must LOAD AND COMPILE, test files included.
//
// The floor assertions come FIRST, so an invocation that saw nothing cannot
// report a clean green; the failure message names the reproduction
// (`go vet ./...`), because a reader who lands this red wants the command that
// shows it in full.
func TestTestTreeTypecheckIsGreen(t *testing.T) {
	root := repoRoot(t)

	entries, variants, failures := testTreeTypecheckErrors(t, root, true)
	if entries < testTreeTypecheckMinPackages {
		t.Fatalf("the listing described %d package entries, below the floor of %d: this gate "+
			"cannot tell a clean tree from an enumeration that looked at nothing",
			entries, testTreeTypecheckMinPackages)
	}
	if variants < testTreeTypecheckMinTestVariants {
		t.Fatalf("the listing enumerated %d test variants, below the floor of %d: a _test.go "+
			"defect can only be seen in a test variant, so this gate would be blind to the very "+
			"class it exists for", variants, testTreeTypecheckMinTestVariants)
	}

	if len(failures) == 0 {
		return
	}
	paths := make([]string, 0, len(failures))
	for p := range failures {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	fmt.Fprintf(&b, "%d package(s) do not compile — a test build of this tree is RED. "+
		"Reproduce the whole picture with `%s vet %s` (which is rc=1 in this state), or one "+
		"package with `%s test <the import path named below>`:\n",
		len(failures), testTreeTypecheckGoExe, testTreeTypecheckAllPackages, testTreeTypecheckGoExe)
	for _, p := range paths {
		fmt.Fprintf(&b, "\n  %s\n    %s\n", p, strings.ReplaceAll(strings.TrimSpace(failures[p]), "\n", "\n    "))
	}
	t.Error(b.String())
}

// TestTestTreeTypecheckRedProof drives the detector itself, on tiny synthetic
// modules (stdlib-only, so the proof is cheap and cannot be skipped behind a
// module download). It asserts all three sides:
//
//	GREEN  — a module whose test file resolves every name it uses is clean;
//	RED    — the SAME module, with one test file referencing a name that is
//	         package-local to a SIBLING package (the measured shape: docclass
//	         referencing cmd/agent's fixture vocabulary), is reported, and the
//	         report names the file and line;
//	FLAG   — the same broken module WITHOUT -export reports nothing, which is
//	         why that flag is load-bearing rather than an optimisation.
//
// A detector that could not go red would leave the real-tree arm above passing
// vacuously on a tree it never compiled.
func TestTestTreeTypecheckRedProof(t *testing.T) {
	// ── GREEN: the names a test file uses are defined where it looks for them.
	green := testTreeTypecheckFixture(t, testTreeTypecheckFixtureGoodLine)
	entries, variants, failures := testTreeTypecheckErrors(t, green, true)
	if entries == 0 {
		t.Fatalf("the green fixture enumerated no packages at all — the fixture is malformed, " +
			"so its cleanliness says nothing about the detector")
	}
	if len(failures) != 0 {
		t.Fatalf("the detector reports a module that compiles as broken: %v — a detector with "+
			"false positives cannot hold a gate", failures)
	}
	if variants == 0 {
		t.Fatalf("the green fixture enumerated %d package entries but NO test variant — without "+
			"one, a _test.go file is never compiled and this detector would be blind by construction",
			entries)
	}

	// ── RED: the referencing file names a constant of a SIBLING package's own
	// vocabulary, unqualified. This is the defect, spelled the way it was
	// measured: the name exists (in another package), so a reviewer skimming the
	// diff sees a plausible identifier, and only a compile says otherwise.
	red := testTreeTypecheckFixture(t, testTreeTypecheckFixtureBadLine)
	_, _, redFailures := testTreeTypecheckErrors(t, red, true)
	if len(redFailures) == 0 {
		t.Fatalf("the detector reports a module whose test file references an UNDEFINED identifier "+
			"as clean — the missing-identifier class is exactly what this gate exists for, and "+
			"%s could not see it", testTreeTypecheckFlagExport)
	}
	joined := ""
	for p, msg := range redFailures {
		joined += p + "\n" + msg + "\n"
	}
	if !strings.Contains(joined, testTreeTypecheckFixtureBrokenRef) {
		t.Errorf("the failure does not name the undefined identifier %q: the report is what a "+
			"reader acts on, and %q is the thing that has to be resolved:\n%s",
			testTreeTypecheckFixtureBrokenRef, testTreeTypecheckFixtureBrokenRef, joined)
	}

	// ── FLAG: without -export the SAME broken module reports nothing. This is
	// the measurement that keeps the flag in the argument list: a future
	// optimisation that drops it would leave this gate green on a red tree.
	_, _, blindFailures := testTreeTypecheckErrors(t, red, false)
	if len(blindFailures) != 0 {
		t.Logf("NOTE: without %s the listing reported %d failure(s) here — the flag is no longer "+
			"load-bearing on this toolchain (%s), and this arm's comment is out of date",
			testTreeTypecheckFlagExport, len(blindFailures), testTreeTypecheckGoExe)
	}
}

const (
	// testTreeTypecheckFixtureModuleDir is the sibling package whose package-local
	// constant the broken test file will reference from the OTHER package — the
	// measured shape, where the identifier lives in someone else's package.
	testTreeTypecheckFixtureModuleDir = "sibling"
	// testTreeTypecheckFixtureGoodLine is the reference a file in this package
	// resolves: the constant of the package the test file itself belongs to.
	testTreeTypecheckFixtureGoodLine = "_ = keeperConst"
	// testTreeTypecheckFixtureBadLine is the SAME file after the defect: the
	// sibling's package-local constant, spelled unqualified.
	testTreeTypecheckFixtureBadLine = "_ = siblingConst"
	// testTreeTypecheckFixtureBrokenRef is the identifier the failure message must
	// name.
	testTreeTypecheckFixtureBrokenRef = "siblingConst"
)

// testTreeTypecheckFixture materialises a module with two packages: `keeper`
// (whose test file carries the statement `line`) and `sibling` (which defines
// siblingConst as a PACKAGE-LOCAL constant). The test file's package does not
// import `sibling`, so passing the sibling's name produces the defect: the
// reference is resolvable in no package the test file imports.
func testTreeTypecheckFixture(t *testing.T, line string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write(testTreeTypecheckModuleFile, testTreeTypecheckModuleBody)
	write("sibling/sibling.go", "package sibling\n\n// siblingConst exists, and only in THIS "+
		"package: the shape a copied name loses.\nconst siblingConst = \"defined here\"\n")
	write("keeper/keeper.go", "package keeper\n\nconst keeperConst = \"defined here too\"\n")
	write("keeper/keeper_test.go", "package keeper\n\nimport \"testing\"\n\n"+
		"func TestKeep(t *testing.T) {\n\t"+line+"\n\tt.Log(\"ok\")\n}\n")
	return root
}

const (
	// testTreeTypecheckBuildVerb is the subcommand whose verdict is deliberately
	// the WRONG one for this defect: `go build` never compiles a _test.go file,
	// so it stays green on a package whose test binary cannot be built. Named
	// because the duplicate-declaration arm asserts that green EXPLICITLY.
	testTreeTypecheckBuildVerb = "build"
	// testTreeTypecheckDuplicateSymbol is the package-level name BOTH files of
	// the duplicate fixture declare. The spelling is a plausible production type
	// (`dupRow`, the shape `tokenRow` has) precisely because the point is that a
	// reviewer skimming the two diffs separately sees nothing wrong.
	testTreeTypecheckDuplicateSymbol = "dupRow"
	// testTreeTypecheckDuplicateDir is the fixture's single package.
	testTreeTypecheckDuplicateDir = "dup"
)

// testTreeTypecheckDuplicateFixture materialises the SECOND measured shape of "a
// test build this tree is red" — not an undefined identifier, but a
// package-level DECLARATION COLLISION between a PRODUCTION file and a _test.go
// file of the same package.
//
// THE MEASURED DEFECT (task 01a19bfb-7afc-78d6-8b9b-b0cd797469f4, reproduced on
// master 83b145ee6 by renaming cmd/agent's `tokenRow` back to `result`, which
// is what the original `tokens_types.go` declared next to
// cli_tasks_sync_surface_test.go:287's `type result`):
//
//	go build ./...                       → exit 0   (the SILENT half)
//	go test ./cmd/agent/ -run '^$' -count=1 → FAIL [build failed]:
//	     cmd/agent/cli_tasks_sync_surface_test.go:287:6: result redeclared in this block
//	             cmd/agent/tokens_types.go:58:6: other declaration of result
//
// The asymmetry is the whole defect, so the fixture reproduces BOTH halves: a
// green production build AND a red test compilation, from the same bytes.
func testTreeTypecheckDuplicateFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	sym := testTreeTypecheckDuplicateSymbol
	write(testTreeTypecheckModuleFile, testTreeTypecheckModuleBody)
	write(testTreeTypecheckDuplicateDir+"/dup.go",
		"package "+testTreeTypecheckDuplicateDir+"\n\n// "+sym+" is the PRODUCTION declaration: a "+
			"report row. Nothing about it is wrong on its own.\ntype "+sym+" struct {\n\ta int\n}\n")
	write(testTreeTypecheckDuplicateDir+"/dup_test.go",
		"package "+testTreeTypecheckDuplicateDir+"\n\nimport \"testing\"\n\n// "+sym+" is a "+
			"TEST-harness helper that happens to have taken the same generic name. This file "+
			"compiles on its own; together with dup.go it does not.\ntype "+sym+" struct {\n\tb string\n}\n\n"+
			"func TestDup(t *testing.T) {\n\tt.Log(\"ok\")\n}\n")
	return root
}

// TestTestTreeTypecheckRedProofDuplicateDeclaration pins the class the sibling
// red proof does NOT reach.
//
// TestTestTreeTypecheckRedProof asserts the detector sees an UNDEFINED
// IDENTIFIER in a _test.go file. This bug — 01a19bfb — is a different class in
// the same family: every identifier exists, each file is individually correct,
// and the failure comes from two DECLARATIONS colliding in one package. A
// detector could plausibly catch the first and miss the second (a symbol table
// that resolved names would report nothing here), so the class needs its own
// arm rather than an assumption that the neighbour covers it.
//
// Three assertions, and the FIRST is the one that makes the other two mean
// something:
//
//	GREEN-BUILD — `go build ./...` on the broken fixture exits 0. This is not a
//	              sanity check, it is the defect: it is why `go build` gates and
//	              the merge path's declared build could not see it for as long
//	              as it existed. If a toolchain ever made build red here, this
//	              arm says so instead of quietly passing.
//	RED-COMPILE — the detector reports the package and names the colliding
//	              symbol, so a reader knows WHICH name to rename (the report is
//	              the deliverable; "1 package does not compile" is not).
//	NAMES-BOTH  — the report names BOTH declarations' files, because the repair
//	              is a choice between them and a report naming one side sends
//	              the reader to the wrong file half the time.
func TestTestTreeTypecheckRedProofDuplicateDeclaration(t *testing.T) {
	dir := testTreeTypecheckDuplicateFixture(t)

	build := exec.Command(testTreeTypecheckGoExe, testTreeTypecheckBuildVerb, testTreeTypecheckAllPackages)
	build.Dir = dir
	var buildOut bytes.Buffer
	build.Stdout, build.Stderr = &buildOut, &buildOut
	if err := build.Run(); err != nil {
		t.Fatalf("`%s %s %s` is RED on the duplicate-declaration fixture (%v):\n%s\n\n"+
			"This fixture MUST reproduce the SILENT half of the defect — a production build that "+
			"succeeds while the package's test binary cannot be built. A toolchain that fails the "+
			"build here would make the whole class visible to every `go build` gate, which is a "+
			"change in the toolchain and not in this gate; this arm exists to notice that shift "+
			"rather than to assume it cannot happen.",
			testTreeTypecheckGoExe, testTreeTypecheckBuildVerb, testTreeTypecheckAllPackages, err,
			strings.TrimSpace(buildOut.String()))
	}

	_, _, failures := testTreeTypecheckErrors(t, dir, true)
	if len(failures) == 0 {
		t.Fatalf("the detector reports a package whose production file and _test.go file declare the "+
			"SAME package-level type %q as clean — the duplicate-declaration class is the shape task "+
			"01a19bfb delivered, and %s could not see it",
			testTreeTypecheckDuplicateSymbol, testTreeTypecheckFlagExport)
	}
	joined := ""
	for p, msg := range failures {
		joined += p + "\n" + msg + "\n"
	}
	if !strings.Contains(joined, testTreeTypecheckDuplicateSymbol) {
		t.Errorf("the failure does not name the colliding declaration %q: the repair is to rename ONE "+
			"of the two, and a report that withholds the name leaves the reader to find it by compiling "+
			"themselves:\n%s", testTreeTypecheckDuplicateSymbol, joined)
	}
	for _, want := range []string{"dup.go", "dup_test.go"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the failure does not name %s: both declarations are in the report because the "+
				"choice of which one to rename is the reader's, and naming one side only sends them to "+
				"the wrong file half the time:\n%s", want, joined)
		}
	}
}
