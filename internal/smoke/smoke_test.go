package smoke

// Tests for the branch→touched-package primitive. Three layers, deliberately:
//
//  1. PURE unit tests of the mapping (PackagesIn/PackagePattern/Args): every
//     judgement the selector makes is exercised without a repository, so a
//     regression is localised to a rule rather than to "the run failed".
//  2. A REAL-GIT test over a throwaway repository (internal/foo, internal/bar,
//     a docs file, a directory the branch deletes outright, a nested module):
//     the diff range, the .go filtering and the tip query are all exercised
//     end to end against git itself, not against a fake.
//  3. RED-PROOF guards in this file that fail if the selection ever widens to
//     the whole module — the property the epic exists for.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// wholeModulePattern is the pattern the selection must NEVER produce: `./...`
// is the whole module, i.e. the full-suite run this package replaces. It is
// spelled here (and in the fixture's assertion) as the anti-widening oracle.
const wholeModulePattern = "./..."

// fixtureRepo builds a throwaway git repository with a `master` base and a
// `feat/topic` branch that:
//   - edits internal/foo/foo.go                    → must be selected
//   - edits internal/bar/bar.go AND deletes it     → must be dropped (the tip
//     has no Go source in internal/bar any more)
//   - edits docs/readme.md (not .go)               → must be ignored
//   - edits devdoc/mod/mod.go inside a NESTED module → must be ignored
//   - edits root.go at the module root             → must be selected as "./"
//
// It is a real repository on purpose: the primitive's two git queries are the
// part most likely to be wrong, and a fake would not exercise them.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "master")
	files := map[string]string{
		"root.go":                 "package gopi\n",
		"internal/foo/foo.go":     "package foo\n",
		"internal/bar/bar.go":     "package bar\n",
		"docs/readme.md":          "hello\n",
		"devdoc/mod/go.mod":       "module example.com/nested\n\ngo 1.24.0\n",
		"devdoc/mod/mod.go":       "package nested\n",
		"internal/untouched/u.go": "package untouched\n",
	}
	for rel, content := range files {
		writeFixtureFile(t, dir, rel, content)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	gitIn(t, dir, "checkout", "-q", "-b", "feat/topic")

	writeFixtureFile(t, dir, "root.go", "package gopi\n\n// touched\n")
	writeFixtureFile(t, dir, "internal/foo/foo.go", "package foo\n\n// touched\n")
	writeFixtureFile(t, dir, "docs/readme.md", "hello again\n")
	writeFixtureFile(t, dir, "devdoc/mod/mod.go", "package nested\n\n// touched\n")
	if err := os.Remove(filepath.Join(dir, "internal/bar/bar.go")); err != nil {
		t.Fatalf("remove internal/bar/bar.go: %v", err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "topic")
	return dir
}

func writeFixtureFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=smoke-fixture", "GIT_AUTHOR_EMAIL=smoke@example.invalid",
		"GIT_COMMITTER_NAME=smoke-fixture", "GIT_COMMITTER_EMAIL=smoke@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestTouchedPackagesIsScopedToTheDiff is the epic's central assertion: a
// branch touching internal/foo yields EXACTLY ./internal/foo/... and never the
// whole module.
func TestTouchedPackagesIsScopedToTheDiff(t *testing.T) {
	dir := fixtureRepo(t)
	sel, err := TouchedPackages(dir, "master", "feat/topic")
	if err != nil {
		t.Fatalf("TouchedPackages: %v", err)
	}
	want := []string{rootPackagePattern, "./internal/foo/..."}
	if got := strings.Join(sel.Packages, " "); got != strings.Join(want, " ") {
		t.Fatalf("selection = %q, want %q\n(the diff touched: %v)", got, strings.Join(want, " "), sel.ChangedGoFiles)
	}
	assertNeverWholeModule(t, sel)
	for _, banned := range []string{"./internal/bar/...", "./internal/untouched/...", "./internal/emptied/...", "./devdoc/mod/..."} {
		for _, got := range sel.Packages {
			if got == banned {
				t.Errorf("selection contains %s — it was not touched (or is not a package at the tip, "+
					"or belongs to a nested module)", banned)
			}
		}
	}
}

// TestTouchedPackagesNeverWidensToTheWholeModule is the standalone red-proof
// oracle: whatever the diff, the result is never the full module pattern. It is
// separate from the scoping test so a report can point at ONE assertion that
// fails if the selector is ever "simplified" back to `go test ./...`.
func TestTouchedPackagesNeverWidensToTheWholeModule(t *testing.T) {
	dir := fixtureRepo(t)
	for _, branch := range []string{"master", "feat/topic"} {
		sel, err := TouchedPackages(dir, "master", branch)
		if err != nil {
			t.Fatalf("TouchedPackages(%s): %v", branch, err)
		}
		assertNeverWholeModule(t, sel)
	}
}

func assertNeverWholeModule(t *testing.T, sel Selection) {
	t.Helper()
	for _, p := range sel.Packages {
		if p == wholeModulePattern {
			t.Fatalf("selection = %v contains %s — the WHOLE module. The primitive exists to avoid "+
				"exactly that run: the selection must be derived from the diff (.go files only), "+
				"never widened as a fallback", sel.Packages, wholeModulePattern)
		}
	}
}

// TestChangedGoFilesFiltersAndHandlesDeletion pins the diff half: .go only,
// deleted paths included (the tip decides their fate), docs ignored.
func TestChangedGoFilesFiltersAndHandlesDeletion(t *testing.T) {
	dir := fixtureRepo(t)
	files, err := ChangedGoFiles(dir, "master", "feat/topic")
	if err != nil {
		t.Fatalf("ChangedGoFiles: %v", err)
	}
	joined := strings.Join(files, "\n")
	for _, want := range []string{"internal/foo/foo.go", "internal/bar/bar.go", "devdoc/mod/mod.go", "root.go"} {
		if !strings.Contains(joined, want) {
			t.Errorf("changed Go files %v missing %s (a DELETED file must still be listed — the "+
				"tip query decides whether its directory is still a package)", files, want)
		}
	}
	if strings.Contains(joined, "docs/readme.md") {
		t.Errorf("changed files %v include a non-Go path — only .go files are package evidence", files)
	}
}

// TestTipTreeKnowsPackagesAndNestedModules pins the tip half without git: the
// tree answers "is this still a package" and "is this a separate module".
func TestTipTreeKnowsPackagesAndNestedModules(t *testing.T) {
	tree := Tree{
		goDirs:        map[string]bool{"": true, "internal/foo": true, "internal/keepme": true, "devdoc/mod": true},
		nestedModules: map[string]bool{"devdoc/mod": true},
	}
	got := PackagesIn([]string{
		"internal/foo/foo_test.go",   // package, selected
		"internal/foo/foo.go",        // same package: deduped
		"internal/gone/gone.go",      // deleted with its whole directory: no Go source at the tip
		"devdoc/mod/mod.go",          // nested module: outside the root module's patterns
		"internal/keepme/old.go",     // deleted, but the package survives
		"docs/readme.md",             // not Go
		"internal/foo/notgo_yet.txt", // not Go
	}, tree)
	want := "./internal/foo/... ./internal/keepme/..."
	if strings.Join(got, " ") != want {
		t.Fatalf("PackagesIn = %v, want %s", got, want)
	}
}

// TestPackagePatternRootIsNeverRecursive pins the composition rule that makes
// the root safe: a change to the root package addresses `./` alone.
func TestPackagePatternRootIsNeverRecursive(t *testing.T) {
	if got := PackagePattern(""); got != rootPackagePattern || got == wholeModulePattern {
		t.Fatalf("PackagePattern(\"\") = %q, want %q (never %q)", got, rootPackagePattern, wholeModulePattern)
	}
	if got := PackagePattern("internal/foo"); got != "./internal/foo/..." {
		t.Fatalf("PackagePattern(internal/foo) = %q", got)
	}
}

// TestEmptySelectionIsReportedNotSilentlyGreen pins Rule 15 at the API edge: a
// docs-only branch must produce an EMPTY selection plus ErrNoPackages, never a
// silent "0 packages, exit 0" that reads as a passing smoke run.
func TestEmptySelectionIsReportedNotSilentlyGreen(t *testing.T) {
	dir := fixtureRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "docs/only", "master")
	writeFixtureFile(t, dir, "docs/readme.md", "docs only\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "docs")

	sel, err := TouchedPackages(dir, "master", "docs/only")
	if err != nil {
		t.Fatalf("TouchedPackages: %v", err)
	}
	if !sel.Empty() {
		t.Fatalf("docs-only branch selected %v — nothing there is testable", sel.Packages)
	}
	if _, _, err := Run(dir, "master", "docs/only"); err != ErrNoPackages {
		t.Fatalf("Run on an empty selection returned err=%v, want ErrNoPackages (an empty selection is a "+
			"decision the caller must make, never a silent green)", err)
	}
	if _, err := Execute(dir, nil); err != ErrNoPackages {
		t.Fatalf("Execute with no packages returned err=%v, want ErrNoPackages", err)
	}
}

// TestArgsPutsTheSelectionFirst pins the command shape a consumer (and a
// report) relies on: selected patterns first, caller flags after.
func TestArgsPutsTheSelectionFirst(t *testing.T) {
	got := Args([]string{"./internal/foo/..."}, "-run", "TestX")
	want := []string{"./internal/foo/...", "-run", "TestX"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Args = %v, want %v", got, want)
	}
}

// TestSelectionCommandIsTheDeclaredEntryPoint pins Rule 14: the smoke command
// has ONE exported name and it is the repo's budget-declaring script — not a
// bare `go test`, which re-inherits Go's 10m cap with no timeout report.
func TestSelectionCommandIsTheDeclaredEntryPoint(t *testing.T) {
	if Command != "scripts/test-budget.sh" {
		t.Fatalf("Command = %q — every consumer must reference the single budget-declaring entry point", Command)
	}
	if !strings.HasSuffix(Command, ".sh") || strings.Contains(Command, "go test") {
		t.Fatalf("Command = %q is not the declared script entry point", Command)
	}
}
