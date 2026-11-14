package standardgate

// The RED PROOFS and the POSITIVE CONTROLS for the census-evidence gate
// (PROJECT.md Rule 38). Every test in this file is named TestCensusGate*, which
// is not cosmetic: the rule's own Check line is
// `go test ./internal/standards/ -run TestCensusGate`, and before this file
// existed that command matched NOTHING, printed "no tests to run" and exited 0
// — a permanently green citation. The names are the repair.
//
// FIVE THINGS ARE PROVEN HERE, on trees this file builds and owns (Rule 32: no
// ambient path; TestMain pins the scratch root off `/`), plus the integration
// run:
//
//	① RED, the PIN half — a census document with a project-wide figure and a
//	   named generator, pinned to a token that does NOT resolve as a commit, is
//	   refused; the same document pinned to the fixture's own HEAD is green. The
//	   token is what decides, not the presence of 40 hex characters.
//	② RED, the GENERATOR half — a census document pinning a real commit but
//	   naming no generator is refused; naming one turns it green.
//	③ the ESCAPE — absent, the document is judged; with a stub reason the stub
//	   does not exempt it (and the failure QUOTES the stub); only a stated reason
//	   records it instead of failing it.
//	④ the SCOPE rule — the gate keys on a title that carries BOTH a census word
//	   and a project-scope word, and a narrative figure (a line window, a pid) is
//	   never read as a project-wide quantity.
//	⑤ the VACUUM — a sweep that judges nothing, or a fragment of the corpus,
//	   or an escape count above its ratchet, is REFUSED rather than passed.
//
// Then the integration run: the REAL corpus is swept through the same reader
// Rule 36's gate uses, the accounting line is printed, the positive controls are
// named as passing, and the floors are enforced in BOTH directions.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devdoc"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// censusPositiveControls are the documents the epic names: each carries a
// project-wide census AND its own re-derivation recipe.
var censusPositiveControls = []string{
	"devdoc/gopi-statistics-20260920.md",
	"devdoc/gopi-statistics-20260927.md",
	"devdoc/gopi-verb-census.md",
}

// censusFixtureDocTemplate is the document every case starts from: a
// title-declared PROJECT census, carrying project-wide figures, a named
// generator and a pin the caller substitutes.
const censusFixtureDocTemplate = `# Project statistics — fixture census

| Dimension | Value |
|---|---:|
| Tracked files | 5,258 |
| Packages | 65 |

**Generator:** ` + "`scripts/gopi-stats.sh`" + ` — re-run to reproduce every number above.
**Base commit measured:** ` + "`%s`" + `
`

// censusFixtureNoGenerator is the same document with the generator line removed
// — case ② 's weakened shape. The pin placeholder survives, so the caller still
// substitutes the fixture's own HEAD.
const censusFixtureNoGenerator = `# Project statistics — fixture census

| Dimension | Value |
|---|---:|
| Tracked files | 5,258 |
| Packages | 65 |

**Base commit measured:** ` + "`%s`" + `
`

// censusNotACommit is the unresolvable pin case ① uses: the sha256 shape that
// LOOKS like a commit (and is exactly what a snapshot records as its artifact
// hash), so the case proves the gate runs the reader's own check rather than
// pattern-matching hex.
const censusNotACommit = "0f9194af7e97b9c6e03ab73d0df10ca8ba03fea93d73cdc570aa168bc6c2a65f"

// censusFixturePath is the judged document inside every fixture tree, spelled
// once because every arm asserts on it BY NAME.
const censusFixturePath = censusCorpusRoot + "/census-fixture.md"

// censusFixtureGit runs a git command in the fixture and returns its trimmed
// output. The child gets testutil.GitEnv, i.e. the inherited repository ROUTING
// is stripped (gate 9): a fixture git child that inherits GIT_DIR from the
// process that started the suite writes into the REAL repository — the
// 2026-10-13 data-loss incident this package already carries a gate for.
func censusFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(censusGitBinary, append([]string{censusGitDirFlag, dir}, args...)...)
	cmd.Env = testutil.GitEnv("GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// censusFixtureTree builds a repository-shaped fixture whose census document is
// exactly as strong as the caller spells it, and returns its root plus the
// commit it pinned. `doc` is a template: every `%s` in it is replaced by the
// fixture's OWN HEAD, so a case can pin the commit it was measured at without
// typing a hash (and without the trap of pinning a HEAD read from a DIFFERENT
// tree — a token that resolves in the repository it was read from does not
// resolve in the fixture's).
func censusFixtureTree(t *testing.T, doc string) (root, pinned string) {
	t.Helper()
	root = t.TempDir()
	censusFixtureGit(t, root, "init", "-q", "-b", "master", ".")
	const fixtureDocPath = censusCorpusRoot + "/census-fixture.md"
	censusFixtureWrite(t, filepath.Join(root, filepath.FromSlash(fixtureDocPath)), "# placeholder\n")
	censusFixtureGit(t, root, "add", fixtureDocPath)
	censusFixtureGit(t, root, "commit", "-q", "-m", "fixture: the commit the census was measured at")
	pinned = censusFixtureGit(t, root, "rev-parse", "HEAD")
	censusFixtureWrite(t, filepath.Join(root, filepath.FromSlash(fixtureDocPath)), strings.ReplaceAll(doc, "%s", pinned))
	censusFixtureGit(t, root, "add", fixtureDocPath)
	censusFixtureGit(t, root, "commit", "-q", "-m", "fixture: the census document under test")
	return root, pinned
}

// censusFixtureWrite writes one fixture file, creating its directory.
func censusFixtureWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// censusFixtureSweep sweeps a fixture and fails the test if the sweep itself
// broke. Every arm goes through this, so no arm can pass on an empty finding
// list produced by an error nobody looked at.
func censusFixtureSweep(t *testing.T, root string) []CensusFinding {
	t.Helper()
	findings, err := CensusEvidenceFindings(root)
	if err != nil {
		t.Fatalf("sweep %s: %v", root, err)
	}
	return findings
}

// TestCensusGatePinsTheCommitThatResolves is the RED proof for the pin half.
func TestCensusGatePinsTheCommitThatResolves(t *testing.T) {
	weak, _ := censusFixtureTree(t, strings.Replace(censusFixtureDocTemplate, "%s", censusNotACommit, 1))
	findings := censusFixtureSweep(t, weak)
	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 judged document, got %d (%+v)", len(findings), findings)
	}
	if findings[0].Generator == "" {
		t.Fatalf("the fixture's generator line was not read: %+v", findings[0])
	}
	if findings[0].Pin != "" {
		t.Fatalf("a token that does not resolve was accepted as a pin: %q", findings[0].Pin)
	}
	verdict := CensusEvidenceVerdict(findings, 0, 0)
	if len(verdict) != 1 {
		t.Fatalf("expected exactly 1 failure for the unresolvable pin, got %d:\n%s", len(verdict), strings.Join(verdict, "\n"))
	}
	if !strings.Contains(verdict[0], censusFixturePath+":") {
		t.Fatalf("the failure does not name %s:\n%s", censusFixturePath, verdict[0])
	}
	if !strings.Contains(verdict[0], "pin MISSING") {
		t.Fatalf("the failure does not name the MISSING pin half:\n%s", verdict[0])
	}
	t.Logf("RED (pin): %s", strings.TrimSpace(verdict[0]))

	// The same document, pinned to the fixture's own HEAD, is green — so the
	// arm above failed for the RIGHT reason, not because everything fails.
	strong, head := censusFixtureTree(t, censusFixtureDocTemplate)
	findings = censusFixtureSweep(t, strong)
	if v := CensusEvidenceVerdict(findings, 0, 0); len(v) != 0 {
		t.Fatalf("a resolvable pin did not turn the gate green: %v", v)
	}
	if findings[0].Pin != head {
		t.Errorf("expected the pin %s to resolve, got %q", head, findings[0].Pin)
	}
	t.Logf("GREEN (pin): %s", strings.TrimSpace(CensusEvidenceReport(findings)))
}

// TestCensusGateNamesTheGenerator is the RED proof for the generator half.
func TestCensusGateNamesTheGenerator(t *testing.T) {
	weak, head := censusFixtureTree(t, censusFixtureNoGenerator)
	findings := censusFixtureSweep(t, weak)
	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 judged document, got %d (%+v)", len(findings), findings)
	}
	if findings[0].Generator != "" {
		t.Fatalf("a document naming no generator reported one: %q", findings[0].Generator)
	}
	if findings[0].Pin != head {
		t.Fatalf("the fixture's own HEAD did not resolve as a pin: got %q want %q", findings[0].Pin, head)
	}
	verdict := CensusEvidenceVerdict(findings, 0, 0)
	if len(verdict) != 1 {
		t.Fatalf("expected exactly 1 failure for the missing generator, got %d:\n%s", len(verdict), strings.Join(verdict, "\n"))
	}
	if !strings.Contains(verdict[0], censusFixturePath+":") {
		t.Fatalf("the failure does not name %s:\n%s", censusFixturePath, verdict[0])
	}
	if !strings.Contains(verdict[0], "generator MISSING") {
		t.Fatalf("the failure does not name the MISSING generator half:\n%s", verdict[0])
	}
	t.Logf("RED (generator): %s", strings.TrimSpace(verdict[0]))

	// Naming a generator turns it green, holding the pin fixed.
	repaired, head2 := censusFixtureTree(t, censusFixtureDocTemplate)
	findings = censusFixtureSweep(t, repaired)
	if v := CensusEvidenceVerdict(findings, 0, 0); len(v) != 0 {
		t.Fatalf("naming the generator did not turn the gate green: %v", v)
	}
	if findings[0].Pin != head2 {
		t.Errorf("expected the pin %s to resolve, got %q", head2, findings[0].Pin)
	}
	t.Logf("GREEN (generator): %s", strings.TrimSpace(CensusEvidenceReport(findings)))
}

// TestCensusGateEscapeIsStatedNotSilent pins the escape: without it the doc is
// judged, a stub reason does not exempt it, and a stated reason records it —
// and the escape count it produces is subject to the ratchet.
func TestCensusGateEscapeIsStatedNotSilent(t *testing.T) {
	const bare = `# Project inventory — the whole tree, counted

| Dimension | Value |
|---|---:|
| Tracked files | 5,258 |
`
	for _, tc := range []struct {
		name    string
		marker  string
		gated   bool
		ratchet int // escapes this shape produces
	}{
		{name: "no escape", marker: "", gated: true},
		{name: "stub reason", marker: "\n<!-- " + censusEscapeMarker + " hist -->\n", gated: true},
		{name: "stated reason", marker: "\n<!-- " + censusEscapeMarker + " a past run's own record of a tree that no longer exists -->\n", gated: false, ratchet: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := censusFixtureTree(t, bare+tc.marker)
			findings := censusFixtureSweep(t, root)
			if len(findings) != 1 {
				t.Fatalf("expected 1 judged document, got %d", len(findings))
			}
			f := findings[0]
			if f.CensusGated() != tc.gated {
				t.Fatalf("gated = %v, want %v (escape reason %q, stub %q)", f.CensusGated(), tc.gated, f.EscapeReason, f.EscapeStub)
			}
			if tc.gated {
				// The document stays judged AND fails: the escape was not bought.
				if len(CensusEvidenceViolations(findings)) != 1 {
					t.Fatalf("a document without a stated escape was not judged as failing: %+v", f)
				}
				if f.EscapeStub != "" {
					stubVerdict := CensusEvidenceVerdict(findings, 0, 0)
					if len(stubVerdict) != 1 || !strings.Contains(stubVerdict[0], strconv.Quote(f.EscapeStub)) {
						t.Fatalf("the failure does not QUOTE the stub reason %q the author wrote — a reader cannot see what was rejected: %v", f.EscapeStub, stubVerdict)
					}
				}
				t.Logf("%s: %s", tc.name, strings.TrimSpace(CensusEvidenceReport(findings)))
				return
			}
			if len(f.EscapeReason) < censusMinEscapeReasonLen {
				t.Fatalf("escape reason %q is below the floor %d — a stub must stay judged", f.EscapeReason, censusMinEscapeReasonLen)
			}
			if f.CensusPasses() {
				t.Fatalf("fixture unexpectedly passes without evidence: %+v", f)
			}
			// The escape RECORDS the document, and the ratchet is what prices it.
			if v := CensusEvidenceVerdict(findings, 0, tc.ratchet); len(v) != 0 {
				t.Fatalf("a stated escape at its ceiling was refused: %v", v)
			}
			if v := CensusEvidenceVerdict(findings, 0, 0); len(v) != 1 || !strings.Contains(v[0], "inline escape") {
				t.Fatalf("an escape above its ceiling (%d) was not refused by the ratchet: %v", tc.ratchet, v)
			}
			t.Logf("%s: %s", tc.name, strings.TrimSpace(CensusEvidenceReport(findings)))
		})
	}
}

// TestCensusGateDenialIsNotEvidence is the regression guard for a defect this
// gate shipped for one run (measured 2026-10-22 on the real-corpus red proof):
// the generator cue was read as evidence WITHOUT looking at what the sentence
// said, so a deficient document whose last line was "No generator and no commit
// is named here" PASSED the generator half — the refusal sentence printed
// `generator generator`, the gate quoting its own absence as proof. A citation
// is a citation; a denial is not.
func TestCensusGateDenialIsNotEvidence(t *testing.T) {
	const denied = `# Project statistics — fixture census

| Dimension | Value |
|---|---:|
| Tracked files | 5,258 |

No generator is named here, on purpose — nothing re-derives these figures.
`
	root, _ := censusFixtureTree(t, denied)
	findings := censusFixtureSweep(t, root)
	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 judged document, got %d (%+v)", len(findings), findings)
	}
	if findings[0].Generator != "" {
		t.Fatalf("a DENIAL was read as generator evidence: %q (the sentence says there is none)", findings[0].Generator)
	}
	// The denying document names no pin line either, so it fails BOTH halves —
	// and the half under test here is the generator one.
	if findings[0].CensusPasses() {
		t.Fatalf("the denying document passed the gate: %+v", findings[0])
	}
	verdict := CensusEvidenceVerdict(findings, 0, 0)
	if len(verdict) != 1 || !strings.Contains(verdict[0], "generator MISSING") {
		t.Fatalf("the denying document was not refused for the missing generator: %v", verdict)
	}
	t.Logf("RED (denial): %s", strings.TrimSpace(verdict[0]))

	// The citation shapes still count, including one whose line merely CONTAINS
	// a negation in an unrelated clause: the word "no" alone is not the test.
	const cited = `# Project statistics — fixture census

| Dimension | Value |
|---|---:|
| Tracked files | 5,258 |

There is no substitute for re-running the generator, so re-run it.
**Base commit measured:** ` + "`%s`" + `
`
	citedRoot, _ := censusFixtureTree(t, cited)
	citedFindings := censusFixtureSweep(t, citedRoot)
	if len(citedFindings) != 1 || citedFindings[0].Generator == "" {
		t.Fatalf("a genuine generator citation was dropped because its line carries an unrelated \"no\": %+v", citedFindings)
	}
	if v := CensusEvidenceVerdict(citedFindings, 0, 0); len(v) != 0 {
		t.Fatalf("the cited document did not pass: %v", v)
	}
}

// TestCensusGateScopeRuleIsTheTitle pins the scope itself: the census word
// alone is not the claim (a command group's own inventory is a local figure),
// the narrative figures of one run are never project-wide quantities, and the
// corpus root/extension come from the ONE package that owns them.
func TestCensusGateScopeRuleIsTheTitle(t *testing.T) {
	for _, want := range []string{
		"# GoPi project statistics — second snapshot (2026-09-27)",
		"# G7 census — every top-level `gopi` verb: dispatched, advertised",
	} {
		if !CensusTitleDeclaresProjectCensus(want) {
			t.Errorf("a project census title was not recognised: %q", want)
		}
	}
	for _, local := range []string{
		"# `gopi task` help-surface inventory (G1 of the help-surface hardening)",
		"# Inventory — G6 help surface (tools-from-the-shell, maintenance, top-level)",
		"# `gopi memory` help-surface inventory (G3 of the help-surface hardening)",
		"# Task Report: the removal-ground gate read a GUARDED arm as unguarded",
		"# The merger's ghost ref: a deleted branch named forever by the discovery snapshot",
	} {
		if CensusTitleDeclaresProjectCensus(local) {
			t.Errorf("a LOCAL title was read as a project census declaration: %q", local)
		}
	}

	// The narrative shape the boundary keeps OUT of the gate: one run's own
	// numbers (a line window, a pid, a log offset) are evidence of that run, and
	// no generator could reproduce them.
	const narrative = "# Task Report: the removal gate's window\n\nThe window was 41 lines (`:1675` vs `:1716`) and the pid was 1503983.\nThe merger log line was 27630 and 13 removal arms were counted.\n"
	if got := CensusFigure(narrative); got != "" {
		t.Errorf("a narrative figure was read as a project-wide census quantity: %q", got)
	}
	// The code-span exclusion, on its own: a task id and a file path are not
	// quantities even when a dimension word sits beside them.
	const codeSpanOnly = "# Project census — fixture\n\nSee `01a0d687` for the 1525157-line record in `internal/tools/tasks.go`.\n"
	if got := CensusFigure(codeSpanOnly); got != "" {
		t.Errorf("a code span was read as a census figure: %q", got)
	}
	// The year band: a snapshot's DATE is not a quantity about the tree.
	const yearOnly = "# Project statistics — fixture\n\nMeasured 2026 for the packages.\n"
	if got := CensusFigure(yearOnly); got != "" {
		t.Errorf("a four-digit year was read as a census figure: %q", got)
	}
	// And the positive direction, so the rule is not merely restrictive.
	const census = "# Project statistics — fixture\n\n| Dimension | Value |\n|---|---:|\n| Tracked files | 5,258 |\n"
	if got := CensusFigure(census); got != "5,258" {
		t.Errorf("a project-wide table figure was not read: got %q", got)
	}

	// The corpus vocabulary is the devdoc package's, never a second spelling.
	if censusCorpusRoot != devdoc.Root {
		t.Errorf("the gate's corpus root drifted from devdoc.Root: %q vs %q", censusCorpusRoot, devdoc.Root)
	}
	if censusMarkdownExt != devdoc.MarkdownExt {
		t.Errorf("the gate's markdown extension drifted from devdoc.MarkdownExt: %q vs %q", censusMarkdownExt, devdoc.MarkdownExt)
	}
}

// TestCensusGateRefusesAVacuum is the RED proof for the defect this file was
// written to repair, one level down: a sweep that measured nothing must FAIL,
// never pass silently. Same shape as the missing `-run TestCensusGate` match —
// a green that measured nothing is indistinguishable from a clean corpus.
func TestCensusGateRefusesAVacuum(t *testing.T) {
	// Judge nothing at all.
	if v := CensusEvidenceVerdict(nil, 0, 0); len(v) != 1 || !strings.Contains(v[0], "judged 0") {
		t.Fatalf("an EMPTY sweep produced %v, want the vacuity refusal saying what it measured", v)
	}
	// A fragment of the corpus: judged, passing, but below the real floor.
	root, _ := censusFixtureTree(t, censusFixtureDocTemplate)
	findings := censusFixtureSweep(t, root)
	v := CensusEvidenceVerdict(findings, censusMinJudgedDocs, censusMaxEscapedDocs)
	if len(v) != 1 || !strings.Contains(v[0], "non-vacuity floor") {
		t.Fatalf("a 1-document sweep was not refused by the floor %d: %v", censusMinJudgedDocs, v)
	}
	// The floors are enforced in BOTH directions: the accounting line states
	// every one of them, so a reader of a green run knows what the green is
	// worth (a summary that printed only "ok" would be the defect again).
	walked, walkErr := censusWalkCount(root)
	if walkErr != nil {
		t.Fatalf("count the fixture corpus: %v", walkErr)
	}
	summary := CensusEvidenceSummary(walked, findings)
	for _, want := range []string{"walked ", "judged ", "passing ", "escaped ", "red ", "floors:"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the accounting line does not carry %q:\n%s", want, summary)
		}
	}
	t.Log(summary)
}

// TestCensusGateJudgesTheRealCorpus is the integration run: the corpus is swept
// through the same reader Rule 36's gate uses, the accounting line is printed,
// the epic's positive controls are named as passing, and the floors are held in
// both directions.
func TestCensusGateJudgesTheRealCorpus(t *testing.T) {
	root := repoRoot(t)
	md, err := censusWalkCount(root)
	if err != nil {
		t.Fatalf("count the corpus under %s/: %v", censusCorpusRoot, err)
	}
	if md < censusMarkdownFloor {
		t.Fatalf("corpus walk found %d markdown documents (floor %d) — a sweep over an empty corpus is vacuously green", md, censusMarkdownFloor)
	}
	findings, err := CensusEvidenceFindings(root)
	if err != nil {
		t.Fatalf("sweep %s/: %v", censusCorpusRoot, err)
	}
	summary := CensusEvidenceSummary(md, findings)
	t.Log(summary)
	t.Logf("census gate verdict:\n%s", CensusEvidenceReport(findings))

	// The floors, in both directions — and stated as one line so a green run
	// proves it MEASURED something.
	if len(findings) < censusMinJudgedDocs {
		t.Fatalf("judged %d census document(s) (floor %d) — the walk or the title rule broke:\n%s", len(findings), censusMinJudgedDocs, summary)
	}
	// THE EVIDENCE FLOOR COUNTS BOTH HONEST ANSWERS. A judged document either
	// shows the evidence (a generator AND a resolving pin) or STATES why it
	// cannot — the escape is a decision with a written reason, which is exactly
	// what this floor exists to see. What it still refuses is a corpus where a
	// document silently stops showing either, and the escape COUNT is held by
	// censusMaxEscapedDocs so an escape cannot become the cheap path.
	passing := 0
	byPath := make(map[string]CensusFinding, len(findings))
	for _, f := range findings {
		byPath[f.Path] = f
		if f.CensusGated() && f.CensusPasses() {
			passing++
			continue
		}
		if !f.CensusGated() && strings.TrimSpace(f.EscapeReason) != "" {
			passing++
		}
	}
	if passing < censusMinPassingDocs {
		t.Fatalf("only %d document(s) carry BOTH a generator and a resolving pin (floor %d) — the corpus stopped showing the evidence:\n%s", passing, censusMinPassingDocs, summary)
	}
	for _, want := range censusPositiveControls {
		f, ok := byPath[want]
		if !ok {
			t.Errorf("positive control %s was not judged at all — it is a title-declared project census carrying project-wide figures:\n%s", want, summary)
			continue
		}
		// A positive control satisfies this arm EITHER by carrying both halves of
		// the evidence OR by stating, in its own escape line, why it cannot. The
		// public export is the second case: it was published with no git history,
		// so no commit pin can resolve there — an escape with a written reason is
		// the honest answer, and the escape COUNT is separately ratcheted.
		if !f.CensusPasses() && strings.TrimSpace(f.EscapeReason) == "" {
			t.Errorf("positive control %s FAILS the gate: figure %q, generator %q, pin %q, and no %s reason",
				want, f.Figure, f.Generator, f.Pin, censusEscapeMarker)
		}
	}

	// The escape ratchet, and every judged-document failure, through the SAME
	// verdict function the fixture arms drive.
	verdict := CensusEvidenceVerdict(findings, censusMinJudgedDocs, censusMaxEscapedDocs)
	for _, failure := range verdict {
		t.Error(failure)
	}
	if len(verdict) != 0 {
		t.Errorf("full verdict:\n%s", CensusEvidenceReport(findings))
	}
}
