package tools

// Merged-result gate tests for the CMD/AGENT-RATCHET leg
// (mergeset_cmdagent.go): the epic's red-then-green proof.
//
// TWO KINDS OF ARM, deliberately split (the sibling stylegate test's shape):
//
//   - the SCOPE arms drive the leg's own decision function over a real repo with
//     a real merge whose diff is the subject — no toolchain needed, so they are
//     cheap and deterministic. They answer "does this landing oblige the
//     roster?" and "is the roster's -run expression DERIVED from its names?".
//   - the RED PROOF arm drives the READY entry point (RebuildAfterMergeMergeset)
//     over real landings and reads the leg's verdict, plus the bug row it filed.
//     The fixture carries a MINIMAL cmd/agent package whose Test* names are the
//     roster's: one reads a published document (`man/<doc>`) the way the real man
//     gates read theirs, so a landing that changes that document turns the roster
//     member red — which IS the defect this leg exists to catch. What the arm
//     proves is the WIRING and the VERDICT PATH: the command is the leg's own,
//     the -run expression is the roster's own, and the red travels through the
//     step's own attribution and reporting channels unchanged.
//
// No host load is generated anywhere here (Rule 27): the only processes are the
// fixture's own git and the toolchain the step itself runs.

import (
	"path/filepath"
	"strings"
	"testing"
)

const (
	// caFixtureManDir / caFixtureManDoc is the published document the fixture's
	// roster member reads — the stand-in for man/gopi-session.md.
	caFixtureManDir = "man"
	caFixtureManDoc = "fixture-page.md"

	// caFixtureMarker is the sentence the document must NOT carry in the green
	// landing; when a landing adds it, the fixture's gate goes red exactly as the
	// real man gate does when a page names a flag no `-h` page prints.
	caFixtureMarker = "FIXTURE-DRIFT-MARKER"

	// caFixtureGreenDoc / caFixtureRedDoc are the two documents the two landings
	// publish. The RED one differs by the marker ALONE, so nothing else in the
	// proof can be the difference.
	caFixtureGreenDoc = "# fixture page\n\nthe page a clean landing publishes\n"
	caFixtureRedDoc   = caFixtureGreenDoc + "\n" + caFixtureMarker + " a flag no page prints\n"

	// caFixtureGateTestFile is the fixture's roster member: its NAME is one of
	// the roster's (TestManSourcesMatchTheirHelpPage), and it reads a document
	// outside its own package — the criterion this whole leg exists for.
	caFixtureGateTestFile = "man_gate_test.go"

	// caFixtureScriptFile / caFixtureScriptBody is a landing that touches NOTHING
	// the roster reads: no Go source, no man page, no document. The landing carries
	// NO SUFFIX on purpose — it is fabricated inside the fixture's own temp repo and
	// never exists here, so spelling it with one the citation gate's pattern reads
	// (`*.sh`, `*.md`, `*.go`, `*.tsv`, `*.json`) would put a path into this
	// repository's source that names nothing, and the gate would have to freeze it.
	caFixtureScriptFile = "scripts/notes"
	caFixtureScriptBody = "#!/bin/sh\necho notes\n"

	// caFixtureBranchDoc / caFixtureBranchGo / caFixtureBranchUnrelated are the
	// three landing branches.
	caFixtureBranchDoc       = "feat/ca-doc"
	caFixtureBranchGo        = "feat/ca-go"
	caFixtureBranchUnrelated = "feat/ca-unrelated"
)

// caFixtureGateBody is the fixture's roster member. It is a repository-reading
// gate with NO dependency on the toolchain's packages: it opens a document at
// the MODULE ROOT — the fixture package is cmd/agent, so its own working
// directory is one level down, which is the same relative shape the real man
// gates use — and that is what makes a DOCUMENT-ONLY landing oblige it: the very
// case the merge path used to measure as green.
var caFixtureGateBody = "package agent\n\n" +
	"import (\n\t\"os\"\n\t\"strings\"\n\t\"testing\"\n)\n\n" +
	"// TestManSourcesMatchTheirHelpPage stands in for the repo's published-document\n" +
	"// gate: a document a DIFFERENT landing owns decides this package's verdict.\n" +
	"func TestManSourcesMatchTheirHelpPage(t *testing.T) {\n" +
	"\tbody, err := os.ReadFile(\"../../" + caFixtureManDir + "/" + caFixtureManDoc + "\")\n" +
	"\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n" +
	"\tif strings.Contains(string(body), \"" + caFixtureMarker + "\") {\n" +
	"\t\tt.Errorf(\"the published page carries %q, which no help page prints\", \"" + caFixtureMarker + "\")\n" +
	"\t}\n}\n"

// caFixtureTree writes the fixture's base module: one ordinary package and one
// cmd/agent package carrying the roster member above.
func caFixtureTree(t *testing.T) string {
	t.Helper()
	repo := caFixtureRepo(t)
	mgWrite(t, repo, sgFixtureModulePath, sgFixtureModuleBody)
	mgWrite(t, repo, sgFixturePkgDir+"/"+sgFixturePkgFile, sgFixturePkgBody)
	mgWrite(t, repo, mergesetCmdAgentDir+"/"+caFixtureGateTestFile, caFixtureGateBody)
	mgWrite(t, repo, caFixtureManDir+"/"+caFixtureManDoc, caFixtureGreenDoc)
	mgGit(t, repo, "init", "-q", "-b", masterBranchName)
	mgGit(t, repo, "config", "user.email", sgFixtureGitEmail)
	mgGit(t, repo, "config", "user.name", sgFixtureGitName)
	mgGit(t, repo, "add", sgFixtureModulePath, sgFixturePkgDir+"/"+sgFixturePkgFile,
		mergesetCmdAgentDir+"/"+caFixtureGateTestFile, caFixtureManDir+"/"+caFixtureManDoc)
	mgGit(t, repo, "commit", "-q", "-m", "fixture: a module, its document and the roster member")
	return repo
}

// caFixtureRepo makes the fixture's (not-yet-a-repository) directory.
func caFixtureRepo(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "repo")
}

// caFixtureLandBranch writes files on a fresh branch off master and merges it
// with --no-ff, so the merge commit's first-parent diff IS the landing.
func caFixtureLandBranch(t *testing.T, repo, branch string, files map[string]string) string {
	t.Helper()
	mgGit(t, repo, "checkout", "-q", "-b", branch)
	for rel, body := range files {
		mgWrite(t, repo, rel, body)
	}
	mgGit(t, repo, "add", "-A")
	mgGit(t, repo, "commit", "-q", "-m", "landing: "+branch)
	mgGit(t, repo, "checkout", "-q", masterBranchName)
	mgGit(t, repo, "merge", "-q", "--no-ff", "-m", "merge "+branch, branch)
	if unmerged := strings.TrimSpace(mgGit(t, repo, "ls-files", "-u")); unmerged != "" {
		t.Fatalf("the fixture's landing must be conflict-free, got unmerged entries:\n%s", unmerged)
	}
	return mgGit(t, repo, "rev-parse", "HEAD")
}

// TestCmdAgentLegFiresOnAGoTouchingLanding is the ordinary case: any landing
// that brings Go source can move the whole-module scans, so the roster runs.
func TestCmdAgentLegFiresOnAGoTouchingLanding(t *testing.T) {
	repo := caFixtureTree(t)
	rev := caFixtureLandBranch(t, repo, caFixtureBranchGo, map[string]string{
		sgFixturePkgDir + "/" + sgFixtureCleanTestFile: sgFixtureCleanTestBody,
	})
	runs, note := mergesetCmdAgentRuns(repo, rev, t.TempDir())
	if note != "" {
		t.Fatalf("the leg abstained on a Go-touching landing: %s", note)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want exactly 1 (one named run over ./%s)", len(runs), mergesetCmdAgentDir)
	}
	got := runs[0]
	if got.Gate != mergesetCmdAgentGateName {
		t.Errorf("gate = %q, want %q", got.Gate, mergesetCmdAgentGateName)
	}
	wantRef := "." + mergesetGatePackageSep + mergesetCmdAgentDir
	for _, want := range []string{vetGateToolchainBinary, mergesetGateTestVerb, wantRef,
		mergesetCmdAgentRunFlag, mergesetCmdAgentRatchetPattern()} {
		if !strings.Contains(strings.Join(got.Argv, " "), want) {
			t.Errorf("argv %v does not carry %q — the run must be the roster's own -run expression over its own package", got.Argv, want)
		}
	}
}

// TestCmdAgentLegFiresOnADocumentOnlyLanding is THE DEFECT'S OWN SHAPE: the
// landing brings no Go source at all — it publishes a document — and the roster
// must still run, because a published-document projection in another package
// reads it. The gate-package leg cannot see this landing (no gate directory was
// touched) and the stylegate leg stands down (no Go), which is precisely how the
// red used to reach master unmeasured.
func TestCmdAgentLegFiresOnADocumentOnlyLanding(t *testing.T) {
	repo := caFixtureTree(t)
	rev := caFixtureLandBranch(t, repo, caFixtureBranchDoc, map[string]string{
		caFixtureManDir + "/" + caFixtureManDoc: caFixtureRedDoc,
	})
	runs, note := mergesetCmdAgentRuns(repo, rev, t.TempDir())
	if note != "" {
		t.Fatalf("the leg abstained on a document-only landing: %s", note)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want exactly 1", len(runs))
	}
}

// TestCmdAgentLegAbstainsWhenTheLandingTouchesNothingItReads is the other half
// of the scope: a landing outside Go source, the man corpus and the document
// corpus cannot move any member's answer, so nothing runs — and the abstention
// NAMES the diff it looked at (Rule 15: a skip that cannot be told from a broken
// leg is a hole, not a saving).
func TestCmdAgentLegAbstainsWhenTheLandingTouchesNothingItReads(t *testing.T) {
	repo := caFixtureTree(t)
	rev := caFixtureLandBranch(t, repo, caFixtureBranchUnrelated, map[string]string{
		caFixtureScriptFile: caFixtureScriptBody,
	})
	runs, note := mergesetCmdAgentRuns(repo, rev, t.TempDir())
	// why: correct — the set is the RUN SET this landing's scope decision produced, and the guard that must not be too broad is mergesetCmdAgentScopeObliges itself: it admits Go source anywhere plus the man/ and devdoc/ trees, and the fixture's landing is the suffix-free caFixtureScriptFile, which matches none of them. An idempotent member (the vet leg, a build leg) belongs to a DIFFERENT list: mergesetCmdAgentRuns appends runs only from the roster, so an over-broad predicate here would show as ONE MORE run over ./cmd/agent, which the sibling arm TestCmdAgentLegFiresOnAGoTouchingLanding pins at exactly 1.
	if len(runs) != 0 {
		t.Fatalf("runs = %d, want none — the landing touched nothing this roster reads", len(runs))
	}
	if !strings.Contains(note, rev) {
		t.Errorf("note = %q, want it to name the revision pair the scope was read from (%s)", note, rev)
	}
	if !strings.Contains(note, mergesetGateFirstParent) {
		t.Errorf("note = %q, want it to name the diff it looked at (%q)", note, mergesetGateFirstParent)
	}
}

// TestCmdAgentRosterDerivesOneRunExpression pins the ONE-source rule (Rule
// 2/14): the -run expression is built FROM the roster, so a name added there is
// measured and a name that is not there cannot be run by accident.
func TestCmdAgentRosterDerivesOneRunExpression(t *testing.T) {
	pattern := mergesetCmdAgentRatchetPattern()
	if !strings.HasPrefix(pattern, "^(") || !strings.HasSuffix(pattern, ")$") {
		t.Fatalf("pattern %q is not an anchored alternation — an unanchored one would match a longer test name", pattern)
	}
	seen := map[string]bool{}
	for _, name := range mergesetCmdAgentRatchetTests {
		if name == "" {
			t.Fatal("the roster carries an empty name")
		}
		if seen[name] {
			t.Errorf("the roster names %q twice — one test, one entry", name)
		}
		seen[name] = true
		if !strings.Contains(pattern, name) {
			t.Errorf("pattern %q does not carry the roster member %q — the expression and the list have drifted apart", pattern, name)
		}
	}
	if got, want := strings.Count(pattern, "|"), len(mergesetCmdAgentRatchetTests)-1; got != want {
		t.Errorf("pattern has %d alternations, want %d (one per member, joined)", got, want)
	}
}

// TestCmdAgentLegRedThenGreen drives the REAL entry point over two landings that
// differ by ONE sentence in a published document: the red one carries the drift
// marker, the green one does not. Each arm gets its OWN fixture, because this
// step measures the merge's first parent for attribution — a second landing on
// top of the first would sit on a base that is already red, and the honest
// verdict there is the abstention the sibling arm proves below.
func TestCmdAgentLegRedThenGreen(t *testing.T) {
	// ① RED: the landing publishes the drifting sentence, on a clean base.
	redRepo := caFixtureTree(t)
	if _, reason := mergesetGateResolveSpec(redRepo); reason != "" {
		t.Skipf("this host cannot measure the merged result: %s", reason)
	}
	redRev := caFixtureLandBranch(t, redRepo, caFixtureBranchDoc, map[string]string{
		caFixtureManDir + "/" + caFixtureManDoc: caFixtureRedDoc,
	})
	red := caFindResult(RebuildAfterMergeMergeset(redRepo, redRev))
	if red == nil {
		t.Fatalf("the merged-result step returned no %q verdict on a landing whose document is red", mergesetCmdAgentGateName)
	}
	if !red.Failed() {
		t.Fatalf("status = %s (%s), want FAIL — the roster member reads the document this landing broke\nengine: %s\n%s",
			red.Status, red.Reason, red.Cmd, red.OutputTail)
	}
	if !red.BaseWasGreen {
		t.Errorf("the red was not attributed to the landing — a base comparison that did not run cannot tell this landing's defect from one that was already there")
	}
	// ② GREEN: the same document change, without the drifting sentence, on its
	// own clean base.
	greenRepo := caFixtureTree(t)
	greenRev := caFixtureLandBranch(t, greenRepo, caFixtureBranchDoc, map[string]string{
		caFixtureManDir + "/" + caFixtureManDoc: caFixtureGreenDoc + "\nan ordinary sentence a landing may publish\n",
	})
	green := caFindResult(RebuildAfterMergeMergeset(greenRepo, greenRev))
	if green == nil {
		t.Fatalf("the merged-result step returned no %q verdict on the clean landing", mergesetCmdAgentGateName)
	}
	if green.Failed() {
		t.Fatalf("status = %s on the clean landing, want PASS — a leg that fires on a document it reads CLEANLY reports an alarm nobody can clear\nengine: %s\n%s",
			green.Status, green.Cmd, green.OutputTail)
	}
}

// TestCmdAgentLegAbsorbsARedBase is the other half of the attribution contract,
// and it is this leg's OWN safety property: a landing whose base is ALREADY red
// must not be reported as having introduced the defect. The real reason matters
// here — the two man gates are red on the integration tip at this rev (see
// mergeset_cmdagent.go's header), so without this arm every landing that touches
// Go source would file a bug row naming a defect it did not cause.
func TestCmdAgentLegAbsorbsARedBase(t *testing.T) {
	repo := caFixtureTree(t)
	if _, reason := mergesetGateResolveSpec(repo); reason != "" {
		t.Skipf("this host cannot measure the merged result: %s", reason)
	}
	// The red lands FIRST, so it becomes the base of the landing measured below.
	caFixtureLandBranch(t, repo, caFixtureBranchDoc, map[string]string{
		caFixtureManDir + "/" + caFixtureManDoc: caFixtureRedDoc,
	})
	second := caFixtureLandBranch(t, repo, caFixtureBranchGo, map[string]string{
		sgFixturePkgDir + "/" + sgFixtureCleanTestFile: sgFixtureCleanTestBody,
	})
	got := caFindResult(RebuildAfterMergeMergeset(repo, second))
	if got == nil {
		t.Fatalf("the merged-result step returned no %q verdict", mergesetCmdAgentGateName)
	}
	if got.Failed() {
		t.Fatalf("status = %s, want ABSTAIN — the red pre-existed this landing's base, and reporting it would fire on every landing forever\n%s",
			got.Status, got.Reason)
	}
	if !strings.Contains(got.Reason, "PRE-EXISTED") {
		t.Errorf("abstention reason = %q, want it to state that the red pre-existed (Rule 15: an abstention says WHICH measurement it made)", got.Reason)
	}
}

// caFindResult returns the verdict this leg spoke, or nil when it did not run.
func caFindResult(results []mergesetGateResult) *mergesetGateResult {
	for i := range results {
		if results[i].Gate == mergesetCmdAgentGateName {
			return &results[i]
		}
	}
	return nil
}
