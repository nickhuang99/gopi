package smoke

// The EXECUTED half of the gate — what the epic's acceptance asks for beyond a
// unit test of the mapping:
//
//	(a) a branch touching exactly one package runs ONLY that package, never the
//	    whole module;
//	(b) a heavy test inside the selected package still SKIPS by default (the
//	    smoke run stays cheap without this package knowing about heaviness);
//	(c) the counterfactual is demonstrated, not asserted: the same fixture run
//	    as a whole-module pattern DOES execute the untouched package — that is
//	    the pre-change behaviour the selection replaces.
//
// HOW IT STAYS HONEST AND CHEAP
// -----------------------------
// The runs are real: a throwaway module in t.TempDir(), a real git repository
// with a real `master...feat/topic` diff, and the repo's OWN
// scripts/test-budget.sh executed as the child process (Rule 14 — the gate is
// exercised through the single exported entry point, never through a copy of
// `go test`).
//
// The evidence is read from the entry point's DECLARED artifact, not from its
// human-facing summary: scripts/test-budget.sh streams `go test -json` into a
// per-run file and prints that path, and the stamp line names the per-run copy.
// The assertions below parse that stream — so "which packages ran", "which test
// passed", "which test skipped" are read from the same artifact a report would
// cite. (The alternative — grepping human output — cannot work: the script
// deliberately prints NOTHING about passing tests.)
//
// The fixture module is named `github.com/nick/gopi/smokefixture` and hot-links
// the repo with a `replace` directive, which is what lets it import
// github.com/nick/gopi/internal/heavytest (Go's internal-visibility rule is a
// PATH rule, not a module rule). Nothing is written into the repo's working
// tree and no fixture is checked in, so the repo's own scans (the heavy
// inventory, the `go test`-budget gate) never see it.
//
// The fixture's internal/bar package is the red-proof control: its test FAILS
// by construction and its directory is untouched by the branch, so its presence
// in the stream is a machine-checkable marker for "this package ran" — present
// under a whole-module pattern, absent under the branch-scoped pattern.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// e2eFixtureModule is the fixture's module path. It deliberately lives
	// under github.com/nick/gopi/ so the fixture may import this repo's
	// internal packages (internal visibility is a path-prefix rule).
	e2eFixtureModule = "github.com/nick/gopi/smokefixture"
	// e2eGoModPath locates the module root (of both the repo and the fixture).
	e2eGoModPath = "go.mod"
	// e2eTopicBranch is the branch under test in the fixture repo.
	e2eTopicBranch = "feat/topic"
	// e2eSelectedPackage is the one package the topic branch touches.
	e2eSelectedPackage = "internal/foo"
	// e2eUntouchedPackage is the package the topic branch does NOT touch — the
	// control that must never be executed by the scoped run.
	e2eUntouchedPackage = "internal/bar"
	// e2eSentinel* name the fixture's tests; the assertions read them out of
	// the run's JSON stream.
	e2eSentinelSelected  = "TestSmokeE2ESelectedPackageRuns"
	e2eSentinelHeavySkip = "TestSmokeE2EHeavyTestSkips"
	// e2eSentinelUntouched is the untouched package's failing test name.
	e2eSentinelUntouched = "TestSmokeE2EUntouchedPackageMustNotRun"
	// e2eHeavySkipMarker is the line internal/heavytest prints for a skipped
	// heavy test — the evidence that heaviness self-skips inside a selected
	// package. Read from the package's own exported constant, never re-spelled.
	e2eHeavySkipMarker = heavytest.SkipMarkerPrefix
	// e2eEntryPointDir is where the repo's script tree is staged inside the
	// fixture — a throwaway directory the script resolves its own root from.
	e2eEntryPointDir = "scripts"
	// e2eStampEnv overrides the entry point's stamp file, so this test reads
	// ITS run's stamp rather than sharing the host's (Rule 2: the script's ONE
	// documented override).
	e2eStampEnv = "GOPI_TEST_BUDGET_STAMP"
	// e2eStreamLine is the stamp field naming the run's per-run stream copy —
	// the script's own spelling, quoted here so a rename is a one-line change.
	e2eStreamLine = "stream="
	// e2eRawJSONLine is the summary line naming the stream directory.
	e2eRawJSONLine = "raw json: "
)

// goTestEvent is the subset of `go test -json` this gate reads.
type goTestEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

// rootRun is one executed entry-point run: its summary output, exit code, and
// the parsed JSON stream it produced.
type rootRun struct {
	stdout string
	code   int
	events []goTestEvent
}

// TestMain pins this package's suite scratch root (Rule 22 / the /tmp ban): the
// e2e fixture module is built under a temp root, and on a default host that
// root is the boot device.
func TestMain(m *testing.M) {
	testutil.PinSuiteForTestMain("smoke")
	code := m.Run()
	testutil.FinishSuiteForTestMain()
	os.Exit(code)
}

// TestBranchScopedSmokeRunIsScopedAndSkipsHeavy is the acceptance test.
func TestBranchScopedSmokeRunIsScopedAndSkipsHeavy(t *testing.T) {
	tree := e2eFixtureTree(t)

	sel, err := TouchedPackages(tree, "master", e2eTopicBranch)
	if err != nil {
		t.Fatalf("TouchedPackages: %v", err)
	}
	want := "./" + e2eSelectedPackage + "/..."
	if strings.Join(sel.Packages, " ") != want {
		t.Fatalf("selection = %v, want exactly [%s] — the branch touches only %s", sel.Packages, want, e2eSelectedPackage)
	}
	assertNeverWholeModule(t, sel)

	t.Run("scoped", func(t *testing.T) {
		run := e2eRun(t, tree, sel.Packages)
		t.Logf("scoped run: exit=%d packages-in-stream=%v\n%s", run.code, run.packages(), run.stdout)

		// (a) ONLY the touched package was addressed.
		got := run.packages()
		if len(got) != 1 || !strings.HasSuffix(got[0], "/"+e2eSelectedPackage) {
			t.Fatalf("the scoped run tested %v — want exactly the fixture's %s package. The selection "+
				"must be derived from the branch's .go diff, not widened", got, e2eSelectedPackage)
		}
		if run.actionOf(e2eSentinelUntouched) != "" {
			t.Fatalf("the scoped run executed %s (final action %q) — an UNTOUCHED package ran",
				e2eSentinelUntouched, run.actionOf(e2eSentinelUntouched))
		}
		if act := run.actionOf(e2eSentinelSelected); act != "pass" {
			t.Fatalf("%s: final action %q, want \"pass\" — the selected package must actually run\n%s",
				e2eSentinelSelected, act, run.stdout)
		}
		// (b) the heavy test inside the selected package SKIPPED (and said so).
		if act := run.actionOf(e2eSentinelHeavySkip); act != "skip" {
			t.Fatalf("%s: final action %q, want \"skip\" — selecting a package must not buy back its "+
				"heavy cost\n%s", e2eSentinelHeavySkip, act, run.stdout)
		}
		if !run.outputContains(e2eSentinelHeavySkip, e2eHeavySkipMarker) {
			t.Fatalf("%s skipped without the %q marker — the skip must be reviewable, not silent\n%s",
				e2eSentinelHeavySkip, e2eHeavySkipMarker, run.stdout)
		}
		if run.code != 0 {
			t.Fatalf("the scoped run exited %d — a green run of the touched package is 0\n%s", run.code, run.stdout)
		}
	})

	t.Run("whole-tree-control", func(t *testing.T) {
		// The same fixture under the pattern the gate replaces: the branch
		// never touches internal/bar, yet a whole-module run builds it, runs it
		// and goes red on it. That is the cost the branch-scoped selection
		// exists to remove — shown here as a measurement, not a claim.
		run := e2eRun(t, tree, []string{wholeModulePattern})
		t.Logf("whole-module control: exit=%d packages-in-stream=%v\n%s", run.code, run.packages(), run.stdout)

		if run.actionOf(e2eSentinelUntouched) != "fail" {
			t.Fatalf("the whole-module control did not run %s to a failure — the fixture is not the "+
				"red-proof shape, so this control proves nothing\n%s", e2eSentinelUntouched, run.stdout)
		}
		if run.code == 0 {
			t.Fatalf("the whole-module control exited 0 — the untouched package's failing test must "+
				"redden the run\n%s", run.stdout)
		}
	})
}

// e2eFixtureTree builds the throwaway repository + module the runs above use: a
// `master` base, a `feat/topic` branch that touches ONE package, a heavy test
// inside that package, and a deliberately-failing test in an UNTOUCHED package.
func e2eFixtureTree(t *testing.T) string {
	t.Helper()
	repoRoot := e2eRepoRoot(t)
	tree := t.TempDir()

	gitIn(t, tree, "init", "-q", "-b", "master")
	writeFixtureFile(t, tree, e2eGoModPath, "module "+e2eFixtureModule+"\n\ngo "+e2eRepoGoDirective(t, repoRoot)+
		"\n\nrequire github.com/nick/gopi v0.0.0\n\nreplace github.com/nick/gopi => "+repoRoot+"\n")
	writeFixtureFile(t, tree, filepath.ToSlash(filepath.Join(e2eSelectedPackage, "foo.go")),
		"// Package foo is the package the topic branch touches.\npackage foo\n")
	writeFixtureFile(t, tree, filepath.ToSlash(filepath.Join(e2eSelectedPackage, "foo_test.go")), e2eTestSources)
	writeFixtureFile(t, tree, filepath.ToSlash(filepath.Join(e2eUntouchedPackage, "bar.go")),
		"// Package bar is NOT touched by the topic branch.\npackage bar\n")
	writeFixtureFile(t, tree, filepath.ToSlash(filepath.Join(e2eUntouchedPackage, "bar_test.go")), e2eControlTestSource)
	writeFixtureFile(t, tree, "docs/readme.md", "base\n")

	gitIn(t, tree, "add", ".")
	gitIn(t, tree, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	gitIn(t, tree, "checkout", "-q", "-b", e2eTopicBranch)

	// The branch: one .go edit inside the selected package, one docs edit the
	// selector must ignore. Nothing else changes — that is what makes "exactly
	// one package" a meaningful assertion.
	writeFixtureFile(t, tree, filepath.ToSlash(filepath.Join(e2eSelectedPackage, "foo.go")),
		"// Package foo is the package the topic branch touches.\n// Touched by "+e2eTopicBranch+".\npackage foo\n")
	writeFixtureFile(t, tree, "docs/readme.md", "base\n\ntouched\n")
	gitIn(t, tree, "add", "-A")
	gitIn(t, tree, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "topic")

	// Stage the repo's OWN script tree (the entry point + every lib file it
	// sources) in the fixture's real layout. It is untracked, so it is
	// invisible to the selection (the selector reads the branch's DIFF and
	// TIP, never the working tree). Rule 22: nothing is written outside the
	// throwaway tree.
	stageScriptTree(t, tree, repoRoot)
	return tree
}

// e2eTestSources is the selected package's test file: one ordinary test (the
// scoped run's green marker) and one heavy test that must SKIP.
const e2eTestSources = `package foo

import (
	"testing"

	"github.com/nick/gopi/internal/heavytest"
)

// TestSmokeE2ESelectedPackageRuns is the marker that the selected package ran.
func TestSmokeE2ESelectedPackageRuns(t *testing.T) {
	t.Log("selected package ran")
}

// TestSmokeE2EHeavyTestSkips declares itself heavy: it must skip in a default
// (smoke) run, so selecting this package does NOT buy the heavy cost back.
func TestSmokeE2EHeavyTestSkips(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	t.Fatal("a heavy test must have skipped in the smoke run")
}
`

// e2eControlTestSource is the UNTOUCHED package's test file. It fails by
// construction, so its final action in the stream is a marker for "this package
// ran" — the property a whole-module pattern has and a branch-scoped pattern
// must not.
const e2eControlTestSource = `package bar

import "testing"

// TestSmokeE2EUntouchedPackageMustNotRun fails on purpose: it is the control
// proving that a whole-module run executes packages the branch never touched.
func TestSmokeE2EUntouchedPackageMustNotRun(t *testing.T) {
	t.Fatal("the untouched package ran — this is the whole-module behaviour the branch-scoped gate replaces")
}
`

// e2eRun executes the staged entry point on the given patterns and returns its
// summary output, exit code and parsed JSON stream.
func e2eRun(t *testing.T, tree string, packages []string) rootRun {
	t.Helper()
	stamp := filepath.Join(t.TempDir(), "stamps.log")
	ledger := filepath.Join(t.TempDir(), "heavy-skipped.tsv")
	// The run's artifact tree is ISOLATED to this test, and this is not
	// hygiene — it is correctness. The entry point defaults its artifacts to
	// the host-wide scratch queue ($GOPI_TEST_SCRATCH_HOME/queue/test-budget),
	// where every concurrent run on this shared host writes the SAME
	// `last.json` with `>`. Two overlapping runs therefore interleave into one
	// file (each truncates at open, then appends at its own offset), and a
	// reader like this one would attribute ANOTHER branch's packages to this
	// branch. Measured 2026-09-18: a scoped run of one fixture package came
	// back carrying internal/tools' tests from a neighbour's suite. Pointing
	// the scratch home at this test's own directory is the script's documented
	// knob (scripts/lib/tmp-ban.sh reads it) and keeps the artifacts off `/`
	// (Rule 22) as well.
	t.Setenv(scratch.HomeEnvName, t.TempDir())
	// t.Setenv (Rule 17): the run's own stamp and skip ledger, so this test
	// reads ITS artifacts instead of the host's shared ones. Both are the
	// script's / heavytest's documented knobs.
	t.Setenv(e2eStampEnv, stamp)
	t.Setenv(heavytest.SkipLedgerEnv, ledger)
	// This test asserts the SMOKE DEFAULT, so it pins it: under
	// `GOPI_HEAVY_TESTS=1` (make test-heavy) the fixture's heavy test would
	// legitimately RUN, and an unpinned run would report that as a failure of
	// "a heavy test must skip". Pinning the condition is what makes the claim
	// precise, not weaker — the heavy opt-in itself is heavytest's business.
	t.Setenv(heavytest.EnvName, "")

	var combined strings.Builder
	// No -count flag is passed: the entry point already runs `go test -count=1`
	// (its own declared contract), so the fixture's tests always execute and are
	// never answered from the test cache — a cached PASS would make every
	// assertion below vacuous.
	code, err := ExecuteTo(tree, &combined, &combined, packages)
	if err != nil {
		t.Fatalf("ExecuteTo %v: %v\n%s", packages, err, combined.String())
	}
	run := rootRun{stdout: combined.String(), code: code}
	run.events = goTestEvents(t, e2eStreamPath(t, stamp, run.stdout))
	assertStreamIsOurs(t, run)
	if _, err := os.Stat(ledger); err == nil {
		if raw, rerr := os.ReadFile(ledger); rerr == nil {
			run.stdout += "\n[heavy skip ledger]\n" + string(raw)
		}
	}
	return run
}

// assertStreamIsOurs refuses a stream that carries a package from outside the
// fixture module. It is the guard on the isolation above: if the artifact is
// ever shared again (or the isolation knob is dropped), this fails loudly
// instead of silently reporting another run's packages as this branch's.
func assertStreamIsOurs(t *testing.T, run rootRun) {
	t.Helper()
	for _, pkg := range run.packages() {
		if !strings.HasPrefix(pkg, e2eFixtureModule) {
			t.Fatalf("the run's stream carries %s, which is NOT part of the fixture module (%s) — the "+
				"artifact was shared with another run, so this run's scope cannot be judged from it",
				pkg, e2eFixtureModule)
		}
	}
}

// e2eStreamPath resolves the stream file this run produced, from the entry
// point's own two declared pointers: the `raw json: <path>` line it prints
// (giving the directory) and the stamp line it appends (giving the per-run
// copy's name). Reading the per-run copy rather than the shared `last.json` is
// deliberate: another suite's run on this host overwrites the shared name.
func e2eStreamPath(t *testing.T, stamp, stdout string) string {
	t.Helper()
	dir := ""
	for _, line := range strings.Split(stdout, "\n") {
		if idx := strings.Index(line, e2eRawJSONLine); idx >= 0 {
			dir = filepath.Dir(strings.TrimSpace(line[idx+len(e2eRawJSONLine):]))
			break
		}
	}
	if dir == "" {
		t.Fatalf("the entry point printed no %q line — its artifact contract changed\n%s", e2eRawJSONLine, stdout)
	}
	raw, err := os.ReadFile(stamp)
	if err != nil {
		t.Fatalf("read the run stamp %s: %v", stamp, err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	last := lines[len(lines)-1]
	name := ""
	for _, field := range strings.Fields(last) {
		if strings.HasPrefix(field, e2eStreamLine) {
			name = strings.TrimPrefix(field, e2eStreamLine)
		}
	}
	if name == "" {
		t.Fatalf("the run stamp %q names no %s<file> — cannot locate this run's stream", last, e2eStreamLine)
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("this run's stream %s: %v", path, err)
	}
	return path
}

// goTestEvents parses a `go test -json` stream.
func goTestEvents(t *testing.T, path string) []goTestEvent {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stream %s: %v", path, err)
	}
	var out []goTestEvent
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev goTestEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // a non-event line (a raw build error dumped into the stream)
		}
		out = append(out, ev)
	}
	if len(out) == 0 {
		t.Fatalf("the stream %s carries no events — the gate would pass vacuously", path)
	}
	return out
}

// packages lists the packages the run actually touched, sorted.
func (r rootRun) packages() []string {
	seen := map[string]bool{}
	var out []string
	for _, ev := range r.events {
		if ev.Package == "" || seen[ev.Package] {
			continue
		}
		seen[ev.Package] = true
		out = append(out, ev.Package)
	}
	return out
}

// actionOf returns a test's FINAL action ("" when the test never ran).
//
// The lookup is deliberately the LAST action event for the test: the
// heavy-skip marker arrives as an "output" event and the terminal action is
// "skip", so counting output events would misreport what happened.
func (r rootRun) actionOf(test string) string {
	final := ""
	for _, ev := range r.events {
		if ev.Test != test {
			continue
		}
		switch ev.Action {
		case "pass", "fail", "skip":
			final = ev.Action
		}
	}
	return final
}

// outputContains reports whether any output event for the test carries the want
// substring.
func (r rootRun) outputContains(test, want string) bool {
	for _, ev := range r.events {
		if ev.Test == test && strings.Contains(ev.Output, want) {
			return true
		}
	}
	return false
}

// stageScriptTree mirrors the repo's ENTIRE script tree into the fixture,
// preserving each file's mode and the directory shape, so the staged entry
// point runs in the fixture's own root with everything it can reach present.
//
// WHY THE WHOLE TREE, AND NOT A HAND-LISTED SET OF FILES: this used to stage a
// two-path list (Command + scripts/lib/tmp-ban.sh). scripts/test-budget.sh
// later grew a second source — `. "$root/scripts/lib/heavy-threshold.sh"` —
// and the list did not follow, so the fixture executed a script whose source
// line failed, `set -u` turned the then-unbound heavy_inventory_rel into a
// fatal error, the entry point printed no `raw json: ` line, and this suite
// blamed the product ("its artifact contract changed") for a fixture defect.
// A hand-listed closure is a mirror that goes stale silently; the fix is to
// carry whatever the repo carries, and to CHECK the closure —
// TestStagedScriptTreeCarriesTheSourceClosure is that check.
func stageScriptTree(t *testing.T, tree, repoRoot string) {
	t.Helper()
	staged := 0
	err := filepath.WalkDir(filepath.Join(repoRoot, filepath.FromSlash(e2eEntryPointDir)),
		func(srcPath string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, rerr := filepath.Rel(repoRoot, srcPath)
			if rerr != nil {
				return rerr
			}
			if d.IsDir() {
				return os.MkdirAll(filepath.Join(tree, rel), 0o755)
			}
			if !d.Type().IsRegular() {
				// A symlink would make the fixture's closure unverifiable
				// (the link target may not exist in the throwaway tree).
				return fmt.Errorf("%s is not a regular file", filepath.ToSlash(rel))
			}
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			raw, rerr := os.ReadFile(srcPath)
			if rerr != nil {
				return rerr
			}
			writeFixtureFile(t, tree, filepath.ToSlash(rel), string(raw))
			if cerr := os.Chmod(filepath.Join(tree, rel), info.Mode().Perm()); cerr != nil {
				return cerr
			}
			staged++
			return nil
		})
	if err != nil {
		t.Fatalf("stage the %s tree from the repo: %v (the fixture must carry the entry point's own closure, never an approximation)",
			e2eEntryPointDir, err)
	}
	if staged == 0 {
		t.Fatalf("staged 0 files from the repo's %s tree — the walk read nothing (a fixture with no scripts at all must be a loud failure, not a vacuous pass)",
			e2eEntryPointDir)
	}
	if _, serr := os.Stat(filepath.Join(tree, filepath.FromSlash(Command))); serr != nil {
		t.Fatalf("the staged tree carries no %s: %v (the entry point this suite executes must be the repo's own file)",
			Command, serr)
	}
}

// e2eRepoRoot walks up from the test's working directory to the module root.
func e2eRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, e2eGoModPath)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no %s above the test's working directory", e2eGoModPath)
		}
		dir = parent
	}
}

// e2eRepoGoDirective reads the repo's own `go` directive, so the fixture module
// declares the same toolchain the repo does (never a drifted copy).
func e2eRepoGoDirective(t *testing.T, repoRoot string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, e2eGoModPath))
	if err != nil {
		t.Fatalf("read %s: %v", e2eGoModPath, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	t.Fatalf("%s declares no go directive", e2eGoModPath)
	return ""
}
