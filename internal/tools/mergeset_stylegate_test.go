package tools

// Merged-result gate tests for the WHOLE-REPO-RATCHET leg
// (mergeset_stylegate.go): the epic's red-then-green proof.
//
// TWO KINDS OF ARM, deliberately split:
//
//   - the SCOPE arms (fires / abstains / stands down) drive the leg's own
//     decision function over a real repo with a real merge whose diff is the
//     subject — no toolchain needed, so they are cheap and deterministic. They
//     answer "does this landing oblige the ratchet?".
//   - the RED PROOF arm drives the READY entry point (RebuildAfterMergeMergeset)
//     over a real merge and reads the leg's verdict, plus the bug row it filed.
//     It needs a toolchain, and it needs SOMETHING for ./internal/stylegate to
//     be in the fixture's tree — so the fixture carries a MINIMAL stand-in for
//     that package (the sibling docclass landing test's shape: the package the
//     leg names, present and real, with its rule stubbed to the one sentence
//     that matters here — a bare time.Sleep in a _test.go is a hit). What the
//     arm proves is the WIRING and the VERDICT PATH: the command is the leg's
//     own, the packages are the leg's own, and the red travels through the
//     step's own attribution and reporting channels unchanged.
//
// The REAL package is exercised by the executed red proof in the run's report
// (Rule 31): a clone of this repo, a real time.Sleep planted in a real
// _test.go, measured by the same function.
//
// No host load is generated anywhere here (Rule 27): the only processes are the
// fixture's own git and the toolchain the step itself runs.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// sgFixtureModulePath / sgFixtureModuleBody are the fixture's module: one
	// package, no dependency, so the toolchain never reaches the network.
	sgFixtureModulePath = "go.mod"
	sgFixtureModuleBody = "module sgfixture\n\ngo 1.21\n"

	// sgFixturePkgDir / sgFixturePkgFile is the ordinary Go source of the
	// fixture — the package a landing routinely touches (cmd/agent on the real
	// repo).
	sgFixturePkgDir  = "cmd/agent"
	sgFixturePkgFile = "agent.go"
	sgFixturePkgBody = "package agent\n\n// Agent is a fixture stand-in for ordinary Go source.\ntype Agent struct{}\n"

	// sgFixtureRatchetDir is internal/stylegate ITSELF: the package the leg
	// names, present in the fixture's tree so the toolchain has something to
	// run (see the header).
	sgFixtureRatchetDir  = "internal/stylegate"
	sgFixtureRatchetFile = "ratchet_test.go"

	// sgFixtureNeedle is the SLEEP SPELLING the stand-in scans for, built by
	// concatenation so this file does not match its own scan.
	sgFixtureSleepSpell = "time.Sleep("

	// sgFixtureCleanTestFile is the landing's clean half: a test file that
	// brings Go source without a sleep.
	sgFixtureCleanTestFile = "clean_test.go"
	sgFixtureCleanTestBody = "package agent\n\nimport \"testing\"\n\nfunc TestFixtureClean(t *testing.T) {}\n"

	// sgFixtureSleepTestFile is the PLANT: the same package, plus one bare
	// time.Sleep — the site the ratchet's ceiling-0 arm exists to catch.
	sgFixtureSleepTestFile = "sleep_test.go"
	sgFixtureSleepTestBody = "package agent\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\n" +
		"func TestFixtureSleeps(t *testing.T) {\n\t" + sgFixtureSleepSpell + "time.Millisecond)\n}\n"

	// sgFixtureDocFile is the NON-Go landing's only file: a landing that cannot
	// move a Go AST scan at all.
	sgFixtureDocFile = "NOTES.md"
	sgFixtureDocBody = "# fixture notes\n\nno Go source here\n"

	// sgFixtureBranchGo / sgFixtureBranchDocs are the two landing branches.
	sgFixtureBranchGo   = "feat/sg-go"
	sgFixtureBranchDocs = "feat/sg-docs"

	// sgFixtureGitEmail / sgFixtureGitName are the scratch repo's own identity.
	sgFixtureGitEmail = "sg@t"
	sgFixtureGitName  = "sg"
)

// sgFixtureRatchetBody is the stand-in for internal/stylegate's ceiling-0
// invariant: walk the module, fail on a bare sleep in a _test.go. It is
// deliberately the SMALLEST statement of the rule — the leg under test is the
// wiring, and the wiring does not care how many rules the gate has.
var sgFixtureRatchetBody = "package stylegate\n\n" +
	"import (\n\t\"os\"\n\t\"path/filepath\"\n\t\"strings\"\n\t\"testing\"\n)\n\n" +
	"// TestFixtureRatchet stands in for the repo's whole-repo ceiling-0 scan.\n" +
	"func TestFixtureRatchet(t *testing.T) {\n" +
	"\troot, err := os.Getwd()\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n" +
	"\tfor {\n\t\tif _, statErr := os.Stat(filepath.Join(root, \"go.mod\")); statErr == nil {\n\t\t\tbreak\n\t\t}\n" +
	"\t\tparent := filepath.Dir(root)\n\t\tif parent == root {\n\t\t\tt.Fatal(\"no go.mod above \" + root)\n\t\t}\n\t\troot = parent\n\t}\n" +
	"\tneedle := \"time.\" + \"Sleep(\"\n" +
	"\twalkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {\n" +
	"\t\tif walkErr != nil {\n\t\t\treturn walkErr\n\t\t}\n" +
	"\t\tif d.IsDir() {\n\t\t\tif d.Name() == \".git\" {\n\t\t\t\treturn filepath.SkipDir\n\t\t\t}\n\t\t\treturn nil\n\t\t}\n" +
	"\t\tif !strings.HasSuffix(p, \"_test.go\") {\n\t\t\treturn nil\n\t\t}\n" +
	"\t\tbody, readErr := os.ReadFile(p)\n\t\tif readErr != nil {\n\t\t\treturn readErr\n\t\t}\n" +
	"\t\tif !strings.Contains(string(body), needle) {\n\t\t\treturn nil\n\t\t}\n" +
	"\t\trel, relErr := filepath.Rel(root, p)\n\t\tif relErr != nil {\n\t\t\treturn relErr\n\t\t}\n" +
	"\t\tt.Errorf(\"%s: bare sleep in a test (fixture ratchet)\", rel)\n\t\treturn nil\n\t})\n" +
	"\tif walkErr != nil {\n\t\tt.Fatal(walkErr)\n\t}\n}\n"

// sgFixture is one landing: the repository and the merge commit to measure.
type sgFixture struct {
	repo     string
	mergeRev string
}

// sgFixtureLand lays down a base module (with a test-only package and the
// ratchet stand-in), lands ONE branch on top of it, and returns the merge
// commit. changed is the landing's whole diff, which is what every scope
// decision in this leg reads.
func sgFixtureLand(t *testing.T, branch string, files map[string]string) sgFixture {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	mgWrite(t, repo, sgFixtureModulePath, sgFixtureModuleBody)
	mgWrite(t, repo, sgFixturePkgDir+"/"+sgFixturePkgFile, sgFixturePkgBody)
	mgWrite(t, repo, sgFixtureRatchetDir+"/"+sgFixtureRatchetFile, sgFixtureRatchetBody)
	mgGit(t, repo, "init", "-q", "-b", masterBranchName)
	mgGit(t, repo, "config", "user.email", sgFixtureGitEmail)
	mgGit(t, repo, "config", "user.name", sgFixtureGitName)
	mgGit(t, repo, "add", sgFixtureModulePath, sgFixturePkgDir+"/"+sgFixturePkgFile,
		sgFixtureRatchetDir+"/"+sgFixtureRatchetFile)
	mgGit(t, repo, "commit", "-q", "-m", "fixture: a clean module and the ratchet package")

	mgGit(t, repo, "checkout", "-q", "-b", branch)
	names := make([]string, 0, len(files))
	for rel, body := range files {
		mgWrite(t, repo, rel, body)
		names = append(names, rel)
	}
	// One `git add` per path (Rule 8: never a wildcard add), then the commit.
	mgGit(t, repo, append([]string{"add"}, names...)...)
	mgGit(t, repo, "commit", "-q", "-m", "landing: "+branch)

	mgGit(t, repo, "checkout", "-q", masterBranchName)
	mgGit(t, repo, "merge", "-q", "--no-ff", "-m", "merge "+branch, branch)
	mergeRev := mgGit(t, repo, "rev-parse", "HEAD")
	if unmerged := strings.TrimSpace(mgGit(t, repo, "ls-files", "-u")); unmerged != "" {
		t.Fatalf("the fixture's landing must be conflict-free, got unmerged entries:\n%s", unmerged)
	}
	return sgFixture{repo: repo, mergeRev: mergeRev}
}

// sgFindResult returns the ratchet leg's verdict, or nil when it did not run.
func sgFindResult(results []mergesetGateResult) *mergesetGateResult {
	for i := range results {
		if results[i].Gate == mergesetStylegateGateName {
			return &results[i]
		}
	}
	return nil
}

// TestStylegateLegFiresOnAGoTouchingLanding is the epic's SCOPE half, positive
// arm: a landing that brings Go source anywhere obliges the whole-repo ratchet,
// and the run it adds is the module-root suite — the package, the `-count=1`
// (a cached test result is a historical fact, not a measurement of this tree)
// and nothing else.
func TestStylegateLegFiresOnAGoTouchingLanding(t *testing.T) {
	fx := sgFixtureLand(t, sgFixtureBranchGo, map[string]string{
		sgFixturePkgDir + "/" + sgFixtureCleanTestFile: sgFixtureCleanTestBody,
	})
	runs, note := mergesetStylegateRuns(fx.repo, fx.mergeRev, t.TempDir())
	if note != "" {
		t.Fatalf("a landing that brings Go source must oblige the ratchet, got note=%q", note)
	}
	if len(runs) != 1 {
		t.Fatalf("the leg must add exactly ONE run, got %d", len(runs))
	}
	wantRef := "./" + sgFixtureRatchetDir
	if got := strings.Join(runs[0].Packages, " "); got != wantRef {
		t.Fatalf("the leg must ask for %s, got %q", wantRef, got)
	}
	for _, want := range []string{vetGateToolchainBinary, mergesetGateTestVerb,
		mergesetGateCountFlag + " " + mergesetGateCountOne, wantRef} {
		if !strings.Contains(strings.Join(runs[0].Argv, " "), want) {
			t.Fatalf("the leg's command must carry %q, got %q", want, strings.Join(runs[0].Argv, " "))
		}
	}
	if runs[0].Gate != mergesetStylegateGateName {
		t.Fatalf("the run must be identified as the ratchet leg, got gate=%q", runs[0].Gate)
	}
}

// TestStylegateLegAbstainsOnANonGoLanding is the SCOPE half's negative arm: a
// landing that brings no Go source cannot move a Go AST scan, so the leg stands
// down — and SAYS SO, with the diff it read (Rule 15: a skip that cannot be told
// from a broken leg is a hole).
func TestStylegateLegAbstainsOnANonGoLanding(t *testing.T) {
	fx := sgFixtureLand(t, sgFixtureBranchDocs, map[string]string{
		sgFixtureDocFile: sgFixtureDocBody,
	})
	runs, note := mergesetStylegateRuns(fx.repo, fx.mergeRev, t.TempDir())
	// why: correct — the set is EMPTY BY CONSTRUCTION, not by a filter that could exclude a legitimate run: mergesetStylegateRuns returns nil on its first arm when the landing's own diff (the merge commit's first-parent diff) lists no .go path, BEFORE any predicate about packages is consulted. The positive half is carried IN THIS TEST above, by the sibling arm TestStylegateLegFiresOnAGoTouchingLanding, which drives the same function over a landing that DOES bring a .go file and gets exactly one run — so an empty list here is the scope read's own work, not a leg that can never fire.
	if len(runs) != 0 {
		t.Fatalf("a non-Go landing must add no run, got %+v", runs)
	}
	if !strings.Contains(note, "no Go source") {
		t.Fatalf("the abstention must say the landing brought no Go source, got %q", note)
	}
	// The note carries the diff it looked at, so a reader can re-derive the
	// decision instead of trusting it.
	if !strings.Contains(note, fx.mergeRev) {
		t.Fatalf("the abstention must name the revision it read, got %q", note)
	}
}

// TestStylegateLegStandsDownWhenTheGatePackagesLegAlreadyAsksForIt pins the
// no-duplicate rule: when the landing itself touched internal/stylegate, the
// gate-package leg already asks the toolchain for that package, so this leg must
// NOT run the same command a second time.
func TestStylegateLegStandsDownWhenTheGatePackagesLegAlreadyAsksForIt(t *testing.T) {
	fx := sgFixtureLand(t, sgFixtureBranchGo, map[string]string{
		sgFixtureRatchetDir + "/" + sgFixtureCleanTestFile: "package stylegate\n\nimport \"testing\"\n\n" +
			"func TestFixtureExtra(t *testing.T) {}\n",
	})
	runs, note := mergesetStylegateRuns(fx.repo, fx.mergeRev, t.TempDir())
	// why: correct — the stand-down is decided BEFORE any package predicate runs: mergesetStylegateAlreadyAsked delegates to the sibling leg's own mapping, which names ./internal/stylegate exactly when the landing's diff touched that directory, and this test's landing touches nothing else. There is no idempotent step whose exclusion could be wrongly swallowed — the alternative (running the suite twice) is pure duplicated cost, and the positive half is carried IN THIS TEST's sibling arm TestStylegateLegFiresOnAGoTouchingLanding, where the same function over a landing that touches NO gate directory returns exactly one run.
	if len(runs) != 0 {
		t.Fatalf("the leg must not duplicate the gate-package leg's run, got %+v", runs)
	}
	if !strings.Contains(note, mergesetGateStylegateDir) {
		t.Fatalf("the stand-down must name the directory that already obliged the run, got %q", note)
	}
}

// TestStylegateLegMeasuresTheMergedRevisionThroughTheLandingPath is the epic's
// RED PROOF, driven through the REAL entry point: a landing whose merge brings a
// bare sleep in a test file must make the step report the ratchet leg RED, and
// the same landing without the sleep must report it GREEN. The two fixtures
// differ by exactly one file, so the verdict is a fact about the plant.
func TestStylegateLegMeasuresTheMergedRevisionThroughTheLandingPath(t *testing.T) {
	red := sgFixtureLand(t, sgFixtureBranchGo, map[string]string{
		sgFixturePkgDir + "/" + sgFixtureCleanTestFile: sgFixtureCleanTestBody,
		sgFixturePkgDir + "/" + sgFixtureSleepTestFile: sgFixtureSleepTestBody,
	})
	green := sgFixtureLand(t, sgFixtureBranchGo, map[string]string{
		sgFixturePkgDir + "/" + sgFixtureCleanTestFile: sgFixtureCleanTestBody,
	})
	if _, reason := mergesetGateResolveSpec(red.repo); reason != "" {
		t.Skipf("this host cannot measure the merged result: %s", reason)
	}
	mgDeclareBuild(t, red.repo, "true")
	dbPath := mgLedger(t)
	_ = mgNotifySink(t)

	// ① RED: the merged revision carries the sleep, so the leg must read the
	// ratchet's own failure out of the merged tree.
	results := RebuildAfterMergeMergeset(red.repo, red.mergeRev)
	res := sgFindResult(results)
	if res == nil {
		t.Fatalf("a Go-touching landing must oblige the ratchet leg, got %+v", results)
	}
	if !res.Failed() {
		t.Fatalf("the landing brought a bare sleep in a test, so the ratchet leg must be RED, got status=%s reason=%q output=%q",
			res.Status, res.Reason, res.OutputTail)
	}
	if !strings.Contains(res.Summary(red.repo, red.mergeRev), mergesetGateStylegateDir) {
		t.Fatalf("the red must name the package that failed, got %q", res.Summary(red.repo, red.mergeRev))
	}

	// ② THE ROW: the red is a queue item, not a log line — and it names the
	// package and the revision a fixer needs.
	wantSummary := mergesetGateBugSummary(red.repo, *res)
	rows := mdLandingBugRows(t, dbPath)
	row := mdLandingFindRow(rows, mdLedgerSummary(wantSummary))
	if row == nil {
		t.Fatalf("a red ratchet leg must WRITE a bug row whose summary is %q; the ledger holds %+v", wantSummary, rows)
	}
	if !strings.Contains(row.tags, bugTag) {
		t.Fatalf("the filed row must carry the %q tag, got %q", bugTag, row.tags)
	}
	for _, want := range []string{red.repo, red.mergeRev, mergesetStylegateGateName} {
		if !strings.Contains(row.payload, want) {
			t.Fatalf("the filed row's payload must name %q, got:\n%s", want, row.payload)
		}
	}

	// ③ GREEN CONTROL: the same landing WITHOUT the plant. A gate that cannot be
	// told from a stuck alarm is not a measurement — this arm is what makes the
	// red above attributable to the sleep and to nothing else.
	_ = mgNotifySink(t)
	greenResults := RebuildAfterMergeMergeset(green.repo, green.mergeRev)
	greenRes := sgFindResult(greenResults)
	if greenRes == nil {
		t.Fatalf("the clean Go-touching landing must still oblige the ratchet leg, got %+v", greenResults)
	}
	if greenRes.Status != mergeBuildGateStatusPass {
		t.Fatalf("the landing without the plant must be GREEN, got status=%s reason=%q output=%q",
			greenRes.Status, greenRes.Reason, greenRes.OutputTail)
	}
}

// TestStylegateLegIsWiredIntoTheRunSetThoughNoGateDirectoryIsTouched is the
// structural half of the epic, asserted where it can be observed: the fixed
// gate-directory map must NOT name the landing's packages (that is the gap), and
// the run set the step assembles must nevertheless carry the ratchet leg.
func TestStylegateLegIsWiredIntoTheRunSetThoughNoGateDirectoryIsTouched(t *testing.T) {
	fx := sgFixtureLand(t, sgFixtureBranchGo, map[string]string{
		sgFixturePkgDir + "/" + sgFixtureCleanTestFile: sgFixtureCleanTestBody,
	})
	// why: correct — an empty answer here is the premise the epic is ABOUT (a landing that touches no gate directory), read through the sibling's own map rather than re-derived; it is not a filter that could drop a legitimate member, because mergesetGateTouchedGatePackages has no exclusion step: it returns a directory exactly when the diff names a .go file under it. The non-empty direction is pinned by TestStylegateLegStandsDownWhenTheGatePackagesLegAlreadyAsksForIt.
	if refs, reason := mergesetGateTouchedGatePackages(fx.repo, fx.mergeRev); reason != "" || len(refs) != 0 {
		t.Fatalf("the fixture premise is that the landing touches NO gate directory, got %v (reason %q)", refs, reason)
	}
	runs, note := mergesetStylegateRuns(fx.repo, fx.mergeRev, t.TempDir())
	// why: correct — the EMPTY gate-package answer asserted on the line above is the sibling's own reading of a landing that touches no gate directory, and the one run this line requires is the ratchet leg closing exactly that gap; the two facts are read from DIFFERENT functions (mergesetGateTouchedGatePackages vs mergesetStylegateRuns), so neither can be an artefact of the other's filter. The positive control for the zero side is the same file's TestStylegateLegStandsDownWhenTheGatePackagesLegAlreadyAsksForIt, where a landing that DOES touch the directory makes the sibling answer non-empty.
	if note != "" || len(runs) != 1 {
		t.Fatalf("the ratchet leg must be the one that closes this gap, got %d run(s) and note=%q", len(runs), note)
	}
	// Sanity on the diff the decision was read from: the landing really is a Go
	// change in a non-gate package.
	goFiles, reason := mergesetStylegateGoFiles(fx.repo, fx.mergeRev)
	if reason != "" {
		t.Fatalf("read the landing's Go files: %s", reason)
	}
	if len(goFiles) != 1 || goFiles[0] != sgFixturePkgDir+"/"+sgFixtureCleanTestFile {
		t.Fatalf("the landing's Go diff must be exactly the clean test file, got %v", goFiles)
	}
}

// TestStylegateLegAbstainsWhenTheScopeCannotBeRead pins the last abstention arm:
// an unresolvable diff base must NOT be silently read as "no Go source" (which
// would stand the ratchet down on a landing nobody measured).
func TestStylegateLegAbstainsWhenTheScopeCannotBeRead(t *testing.T) {
	fx := sgFixtureLand(t, sgFixtureBranchGo, map[string]string{
		sgFixturePkgDir + "/" + sgFixtureCleanTestFile: sgFixtureCleanTestBody,
	})
	runs, note := mergesetStylegateRuns(fx.repo, "0000000000000000000000000000000000000000", t.TempDir())
	// why: correct — an unreadable diff base returns through the ABSTENTION arm before any run is built (mergesetStylegateGoFiles -> reason), so nothing idempotent can be wrongly excluded: there is no package set to filter at that point. The positive half is carried IN THIS TEST immediately below, where the SAME function over a resolvable scope returns one run — the empty list is the unreadable diff's own consequence.
	if len(runs) != 0 {
		t.Fatalf("an unreadable scope must add no run, got %+v", runs)
	}
	if note == "" || strings.Contains(note, "no Go source") {
		t.Fatalf("an unreadable scope must abstain with its OWN reason, never as 'no Go source', got %q", note)
	}
	// The no-checkout arm, for completeness: the leg never guesses a directory.
	if runs, note := mergesetStylegateRuns(fx.repo, fx.mergeRev, ""); len(runs) != 0 || note == "" {
		t.Fatalf("a missing checkout must abstain, got %d run(s) and note=%q", len(runs), note)
	}
}

// TestStylegateLegScopeReadsTheSameDiffAsItsSiblings guards the Rule 2 property
// this leg shares with the rest of the step: the scope is READ from the merge
// commit's own first-parent diff, never from a caller's claim.
func TestStylegateLegScopeReadsTheSameDiffAsItsSiblings(t *testing.T) {
	fx := sgFixtureLand(t, sgFixtureBranchGo, map[string]string{
		sgFixturePkgDir + "/" + sgFixtureCleanTestFile: sgFixtureCleanTestBody,
		sgFixtureDocFile: sgFixtureDocBody,
	})
	out := mgGit(t, fx.repo, mergesetGateDiffVerb, mergesetGateNameOnlyFlag,
		fx.mergeRev+mergesetGateFirstParent, fx.mergeRev)
	var want []string
	for _, line := range strings.Split(out, "\n") {
		if p := strings.TrimSpace(line); strings.HasSuffix(p, mergesetGateGoSourceSuffix) {
			want = append(want, p)
		}
	}
	got, reason := mergesetStylegateGoFiles(fx.repo, fx.mergeRev)
	if reason != "" {
		t.Fatalf("read the landing's Go files: %s", reason)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("the leg's scope must be the landing's own Go diff: got %v, want %v", got, want)
	}
	if len(got) != 1 {
		t.Fatalf("the non-Go file must not enter the scope, got %v", got)
	}
}

// TestStylegateLegSampleIsBounded keeps the firing line's file list bounded: a
// landing can bring hundreds of Go files and the log line exists to say the leg
// FIRED, not to enumerate the diff.
func TestStylegateLegSampleIsBounded(t *testing.T) {
	files := make([]string, 0, mergesetStylegateSampleMax+2)
	for i := 0; i < mergesetStylegateSampleMax+2; i++ {
		files = append(files, "p"+string(rune('a'+i))+".go")
	}
	if got := mergesetStylegateSample(files); len(got) != mergesetStylegateSampleMax {
		t.Fatalf("the sample must be capped at %d, got %d", mergesetStylegateSampleMax, len(got))
	}
	short := files[:1]
	if got := mergesetStylegateSample(short); len(got) != 1 {
		t.Fatalf("a short list must pass through unchanged, got %d", len(got))
	}
}

// TestStylegateRatchetSuiteIsGreenOnThisTree is the epic's FIRST acceptance
// criterion, measured where the leg will measure it: the repo's own ratchet
// suite runs over THIS tree and passes. It is the honest baseline the leg's
// trigger is calibrated on — with the suite green here, every red the leg later
// reports is a hit a landing brought, not an inherited state.
//
// It rides the same toolchain path as the leg (a scratch worktree of HEAD is not
// needed: the suite scans the tree it is started in, and this package's own
// working directory IS this repo).
func TestStylegateRatchetSuiteIsGreenOnThisTree(t *testing.T) {
	root := toolsRepoRoot(t)
	// The ratchet's own gate file must be the one carrying the ceilings — a leg
	// pointed at a package that no longer gates anything would be a green that
	// means nothing.
	gate, err := os.ReadFile(filepath.Join(root, mergesetGateStylegateDir, "gate_test.go"))
	if err != nil {
		t.Fatalf("read the ratchet's gate file: %v", err)
	}
	if !strings.Contains(string(gate), "func TestStyleGate(") {
		t.Fatalf("the package the leg names must carry the ceiling-0 ratchet (TestStyleGate); got a file without it")
	}
	if _, reason := mergesetGateResolveSpec(root); reason != "" {
		t.Skipf("this host cannot measure the merged result: %s", reason)
	}
	// The MEASUREMENT, through the leg's own argv and the leg's own exec path,
	// over a detached worktree of this repo's HEAD — the same shape the landing
	// path uses. Skipped rather than failed when the host cannot measure, always
	// with the reason stated (Rule 15).
	out, code := mgToolchain(t, root, "HEAD",
		mergesetGateTestVerb, mergesetGateCountFlag+"="+mergesetGateCountOne,
		"."+mergesetGatePackageSep+mergesetGateStylegateDir)
	if code != 0 {
		t.Fatalf("the whole-repo ratchet must be GREEN on this tree (it is the baseline the leg's trigger is calibrated on); got exit %d:\n%s",
			code, out)
	}
}
