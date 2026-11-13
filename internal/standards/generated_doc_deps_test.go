package standardgate

// generated_doc_deps_test.go — the LOWEST-LEVEL guard for the edge in
// generated_doc_deps.go, and the red proof that makes it a guard rather than a
// description.
//
// The predicate under test is `generatedDocImpact`: given the discovered edges
// and the files a change touched, it answers TWO things — which packages must
// run for the drift to be caught, and which edges are broken (a document whose
// generator is missing from the tree, a document with no gate package, a
// published anchor pointing at a file that does not exist). Both answers are
// findings-shaped so the red proofs below can drive the SAME function the
// real-tree assertion calls.

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The document paths and the one generator this file's dependency scan reads.
// They lived beside the COMMANDS.md document gate until that gate left the
// package in the 2026-10-16 doc-pipeline removal; they are spelled HERE, beside their only
// surviving consumer, rather than re-created as a second document-gate file
// (Rules 1b/13: one definition, referenced where the value is needed).
const (
	// commandsDocName is the reference document's path, relative to the module
	// root.
	commandsDocName = "COMMANDS.md"
	// commandsDocGeneratorRel is the ONE generator COMMANDS.md instructs a reader
	// to run — the same instruction the page carries in its own prose.
	commandsDocGeneratorRel = "internal/standards/testdata/commands-census.go"
	// commandsDocAnchorSourceProbe is the published anchor source this epic was
	// filed over: a commit inserted a line in this file above a predicate the
	// generated block publishes a `file.go:NNN` anchor for, and nothing on the
	// committing branch ran the check that blocks on the drift.
	commandsDocAnchorSourceProbe = "cmd/agent/cli_tasks.go"
	// standardsGatePackageRel is this package's own directory — the package a
	// change to a published anchor source must reach.
	standardsGatePackageRel = "internal/standards"
)

func TestGeneratedDocDepsCoverTheReferenceDocuments(t *testing.T) {
	root := repoRoot(t)
	deps, err := generatedDocDeps(root)
	if err != nil {
		t.Fatalf("discover the generated-document edges: %v", err)
	}
	if len(deps) < generatedDocMinDeps {
		t.Fatalf("the scan found %d generated document(s) (floor %d) — either the walk did not read the "+
			"tree or the documents lost their generator instruction; refusing to pass blind",
			len(deps), generatedDocMinDeps)
	}
	byDoc := map[string]generatedDocDep{}
	for _, d := range deps {
		byDoc[d.Doc] = d
		t.Logf("%s → generator %s, %d anchor source(s), gate package(s) %v",
			d.Doc, d.Generator, len(d.AnchorSources), d.GatePackages)
	}
	// The reference document this gate was written for: its edge must name the
	// generator the document itself instructs a reader to run, the source file
	// whose line numbers it publishes, and the package that blocks on it.
	dep, ok := byDoc[commandsDocName]
	if !ok {
		t.Fatalf("%s carries a generated block but no edge was discovered for it — the scan reads the "+
			"document's own `go run` instruction, so its absence means the instruction moved", commandsDocName)
	}
	if dep.Generator != commandsDocGeneratorRel {
		t.Errorf("%s names generator %q, want %q — the edge must be READ from the document's own "+
			"regeneration instruction, not typed here", commandsDocName, dep.Generator, commandsDocGeneratorRel)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(dep.Generator))); err != nil {
		t.Errorf("%s names generator %q, which is not in the tree: %v", commandsDocName, dep.Generator, err)
	}
	for _, anchorSource := range dep.AnchorSources {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(anchorSource))); err != nil {
			t.Errorf("%s publishes an anchor into %q, which is not in the tree: %v",
				commandsDocName, anchorSource, err)
		}
	}
	if !generatedDocHas(dep.AnchorSources, commandsDocAnchorSourceProbe) {
		t.Errorf("%s's published anchors do not include %q — the file whose drift started this epic is "+
			"exactly the edge this scan must keep visible", commandsDocName, commandsDocAnchorSourceProbe)
	}
	if !generatedDocHas(dep.GatePackages, standardsGatePackageRel) {
		t.Errorf("the package that blocks on %s is not reported as its gate: got %v, want it to contain %q",
			commandsDocName, dep.GatePackages, standardsGatePackageRel)
	}
}

// TestGeneratedDocAnchorChangeRequiresItsGate is the HARDENING, stated at the
// lowest level: a change that touches a source file whose line numbers a
// generated document publishes must reach the package that blocks on that
// document. The assertion is over the REAL tree and the REAL edge — this is the
// defect (cli_tasks.go moved under a published anchor) driven through the one
// predicate that maps a change to the packages it obliges.
func TestGeneratedDocAnchorChangeRequiresItsGate(t *testing.T) {
	root := repoRoot(t)
	deps, err := generatedDocDeps(root)
	if err != nil {
		t.Fatalf("discover the generated-document edges: %v", err)
	}
	if findings := generatedDocFindings(root, deps); len(findings) > 0 {
		t.Errorf("the discovered edges are broken — a change against them cannot be mapped:\n  %s",
			strings.Join(findings, "\n  "))
	}
	// The change that started the epic: one line inserted into the file whose
	// line numbers COMMANDS.md publishes.
	required, _ := generatedDocImpact(root, deps, []string{commandsDocAnchorSourceProbe})
	if !generatedDocHas(required, standardsGatePackageRel) {
		t.Errorf("a change to %q requires %v — %q is missing, so a line moved under a published anchor "+
			"would reach master with nothing on the committing branch running the check that blocks on it",
			commandsDocAnchorSourceProbe, required, standardsGatePackageRel)
	}
	// The control: a change to a file no document publishes anchors into obliges
	// nothing. Without it, a predicate that required every package for every
	// change would pass the assertion above.
	if required, _ := generatedDocImpact(root, deps, []string{"devdoc/rules-reference.md"}); len(required) != 0 {
		t.Errorf("a change to an unrelated document required %v, want nothing", required)
	}
}

// TestGeneratedDocImpactRedProof drives the predicate with poisoned edges and
// demands a finding for each way the edge can be broken. Every finding is a
// guard that would otherwise be a silent pass: a change that moves published
// anchors and reaches no gate package, a document naming a generator the tree
// does not have, and a document with no discovered anchor source at all.
func TestGeneratedDocImpactRedProof(t *testing.T) {
	root := repoRoot(t)
	probe := []string{commandsDocAnchorSourceProbe}

	// (a) a change that moves published anchors and no package would notice.
	ungated := []generatedDocDep{{Doc: commandsDocName, Generator: commandsDocGeneratorRel,
		AnchorSources: []string{commandsDocAnchorSourceProbe}}}
	required, findings := generatedDocImpact(root, ungated, probe)
	if len(generatedDocEdgeFindings(findings)) == 0 {
		t.Errorf("red-proof (a) failed: an edge with no gate package yielded no EDGE finding, so the change "+
			"would be mapped to nothing and pass silently (required=%v)", required)
	}
	// The control: the same edge WITH its gate package yields the requirement
	// and no finding — a predicate that always fired would prove nothing.
	gated := []generatedDocDep{{Doc: commandsDocName, Generator: commandsDocGeneratorRel,
		AnchorSources: []string{commandsDocAnchorSourceProbe}, GatePackages: []string{standardsGatePackageRel}}}
	required, findings = generatedDocImpact(root, gated, probe)
	// The control asserts what an EDGE answer owes: no finding about the edge
	// itself. It reads the edge-only half of the answer, because the same call
	// also raises the tree-level DECLARATION invariant over the REAL document
	// (see generatedDocEdgeFindings) — a defect in the CONDITION the edge points
	// at, and a red proof of the EDGE predicate must not be satisfiable by a
	// document that happens to be stale.
	edgeFindings := generatedDocEdgeFindings(findings)
	if len(edgeFindings) != 0 {
		t.Errorf("red-proof (a) control failed: a well-formed edge yielded %v", edgeFindings)
	}
	if !generatedDocHas(required, standardsGatePackageRel) {
		t.Errorf("red-proof (a) control failed: the well-formed edge required %v", required)
	}

	// (b) a document naming a generator the tree does not have: the edge is
	// unusable — whoever follows it runs nothing.
	missing := []generatedDocDep{{Doc: commandsDocName, Generator: commandsDocStaleGeneratorProbe,
		AnchorSources: []string{commandsDocAnchorSourceProbe}, GatePackages: []string{standardsGatePackageRel}}}
	// The arms (b)/(c)/(d) read the EDGE half for the same reason the control
	// above does: with the tree-level DECLARATION ERROR live (a stale shipped
	// document) a raw `len(findings) > 0` is satisfied by a class these arms do
	// not test — a vacuous pass on exactly the case the split exists for.
	if _, findings := generatedDocImpact(root, missing, probe); len(generatedDocEdgeFindings(findings)) == 0 {
		t.Errorf("red-proof (b) failed: a generator that is not in the tree yielded no EDGE finding, so a stale " +
			"edge would be followed into a command that does not exist")
	}

	// (c) a document with no discovered anchor source: nothing can map a change
	// to it, which is the scan-not-run shape (a floor, not a pass).
	anchorless := []generatedDocDep{{Doc: commandsDocName, Generator: commandsDocGeneratorRel,
		GatePackages: []string{standardsGatePackageRel}}}
	if _, findings := generatedDocImpact(root, anchorless, probe); len(generatedDocEdgeFindings(findings)) == 0 {
		t.Errorf("red-proof (c) failed: an edge with no anchor source yielded no finding")
	}

	// (d) the read floor itself: a discovery that returned nothing must be a
	// finding, never an empty green answer.
	if _, findings := generatedDocImpact(root, nil, probe); len(generatedDocEdgeFindings(findings)) == 0 {
		t.Errorf("red-proof (d) failed: an empty discovery yielded no finding — a scan that found nothing " +
			"must be distinguishable from a scan that never ran")
	}
}

// TestGeneratedDocCandidatesAreTheReferenceDocuments pins the discovery's scope
// against the tree: the documents it reads are the ones that carry a generated
// region or a generator instruction, and the walk finds at least the floor. A
// scope that silently emptied would make every assertion above vacuous.
func TestGeneratedDocCandidatesAreTheReferenceDocuments(t *testing.T) {
	root := repoRoot(t)
	docs, err := generatedDocCandidates(root)
	if err != nil {
		t.Fatalf("list the candidate documents: %v", err)
	}
	if len(docs) < generatedDocMinNodes {
		t.Fatalf("the walk found %d markdown document(s) (floor %d)", len(docs), generatedDocMinNodes)
	}
	sorted := append([]string{}, docs...)
	sort.Strings(sorted)
	if !generatedDocHas(sorted, commandsDocName) {
		t.Errorf("the walk did not reach %s: %v", commandsDocName, sorted)
	}
}

// TestGeneratedDocMandateNamesTheGeneratorAndTheGate is the CONSUMER-side
// assertion: the sentence a committing path would carry is built from the
// discovered edge (the generator path and the gate package both come out of the
// tree), and a change that obliges nothing produces NO sentence — an empty
// mandate is a value, so a caller cannot render a caution nobody needs.
func TestGeneratedDocMandateNamesTheGeneratorAndTheGate(t *testing.T) {
	root := repoRoot(t)
	got, err := GeneratedDocMandate(root, []string{commandsDocAnchorSourceProbe})
	if err != nil {
		t.Fatalf("render the mandate for %q: %v", commandsDocAnchorSourceProbe, err)
	}
	for _, must := range []string{commandsDocName, commandsDocGeneratorRel, standardsGatePackageRel} {
		if !strings.Contains(got, must) {
			t.Errorf("the mandate for %q does not name %q:\n%s", commandsDocAnchorSourceProbe, must, got)
		}
	}
	if empty, err := GeneratedDocMandate(root, []string{"devdoc/rules-reference.md"}); err != nil {
		t.Errorf("render the mandate for an unrelated change: %v", err)
	} else if empty != "" {
		t.Errorf("an unrelated change produced a mandate (%q), want none", empty)
	}
	// The exported answer and the mandate agree about the same change — the two
	// consumers of the edge cannot disagree, because both read it.
	required, findings, err := GeneratedDocGates(root, []string{commandsDocAnchorSourceProbe})
	if err != nil {
		t.Fatalf("the exported gate answer: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("the real tree's edges are broken: %v", findings)
	}
	if !generatedDocHas(required, standardsGatePackageRel) {
		t.Errorf("the exported answer for %q is %v, want it to contain %q",
			commandsDocAnchorSourceProbe, required, standardsGatePackageRel)
	}
}

// --- the predicate under test -------------------------------------------------

// generatedDocFindings reports every broken edge in deps: a document whose
// generator is not in the tree, an edge naming no anchor source, and an edge no
// package would run for. An empty slice means every edge is usable.
func generatedDocFindings(root string, deps []generatedDocDep) []string {
	_, findings := generatedDocImpact(root, deps, nil)
	return generatedDocEdgeFindings(findings)
}

// generatedDocEdgeFindings splits an Impact answer the way the delivery path
// does (gendoc's own SeparateErrors, re-exported by export_test.go, so the
// severity marker is read from its ONE definition rather than re-typed here,
// Rule 1b/13/14) and returns the half that is a statement about the EDGE — its
// gate packages, its generator, its anchor sources.
//
// The other half is the tree-level DECLARATION invariant over the REAL document:
// an anchor published inside a generated region that resolves to a comment or
// blank line because the block was not regenerated. That is a defect in the
// CONDITION the edge points at, owned by that check and asserted by its own arms
// (internal/gendoc's TestRegionAnchorDeclarationInvariant* — the fixture arms
// prove it fires, the real-block arm proves the shipped document is in step).
//
// WHY THE SPLIT IS THE FIX (measured 2026-10-14, base 48ad72424). The red proofs
// below drive Impact with POISONED dep literals to isolate the predicate, while
// the declaration invariant reads the document it is given OUT of the real tree
// — so a stale COMMANDS.md on that base (its verb index published
// `cmd/agent/cli_doccheck.go:246`, the comment line above the declaration at
// :297) red the EDGE's own red proof over a fact the proof does not test, and
// "regenerate the document" was the only way to green it. An edge proof must be
// satisfiable by the edge predicate alone; the document has its own gates.
func generatedDocEdgeFindings(findings []string) []string {
	edge, _ := generatedDocSeparateErrors(findings)
	return edge
}

// The probes this file's red proofs poison an edge with. They are deliberately
// not trees that exist: the point of each is that the predicate must REPORT an
// unusable edge rather than follow it.
const (
	// commandsDocStaleGeneratorProbe is a generator path no tree holds.
	commandsDocStaleGeneratorProbe = "internal/standards/testdata/no-such-census.go"
)

// --- a tree of a DIFFERENT SHAPE -----------------------------------------------

// The fixture tree below is the shape that broke the edge (task 01a12bb0): the
// real tree minus its cmd/ half. Walking a base the tree does not have made
// filepath.WalkDir report lstat ENOENT, the walk's error was wrapped by
// generatedDocGatePackages and returned, and the whole edge came back a hard
// error — so a touch of a published anchor in such a tree produced an ABSTENTION
// carrying raw lstat text instead of a measured answer.
//
// These tests are the red proof of the fix: the red one drives the REAL exported
// surface (GeneratedDocGates) on a real temp tree, and the two beside it pin the
// line the fix must NOT cross — a base that is PRESENT but unusable keeps failing
// loudly, and the skip is STATED rather than silent.

const (
	// generatedDocFixtureGoMod is the module file every fixture tree needs: the
	// scan reads a tree, and a tree without a module is not the shape under test.
	generatedDocFixtureGoMod = "module fixture\n\ngo 1.21\n"
	// generatedDocFixtureAnchorSource is the source file the fixture document
	// publishes an anchor into — the "changed" path the test touches.
	generatedDocFixtureAnchorSource = "internal/standards/fixture_anchor.go"
	// generatedDocFixtureGateDir is where the fixture's gate package lives: a
	// `.go` file under internal/ naming the document is what makes a package the
	// document's gate, and internal/ is the base the fixture DOES have.
	generatedDocFixtureGateDir = "internal/standards"
	// generatedDocFixtureDoc is the fixture's document of record. It carries a
	// generated region, the `go run` instruction that names its generator, and
	// one published anchor into generatedDocFixtureAnchorSource.
	generatedDocFixtureDoc = "COMMANDS.md"
	// generatedDocFixtureDocBody is that document. Written out rather than
	// derived: a fixture tree must be able to carry ANY shape, and the point of
	// this one is the two lines the scan reads (the generated marker and the
	// generator instruction) plus the anchor.
	generatedDocFixtureDocBody = "# Fixture commands\n\n" +
		"<!-- BEGIN GENERATED -->\n" +
		"Regenerate with `go run " + generatedDocFixtureGeneratorRel + " . " + generatedDocFixtureDoc + "`.\n" +
		"See `" + generatedDocFixtureAnchorSource + ":12`.\n" +
		"<!-- END GENERATED -->\n"
	// generatedDocFixtureGeneratorRel is the fixture's generator program, which
	// must EXIST in the tree or the edge is a finding (a deliberately different
	// path from the real tree's, so nothing here depends on this repo).
	generatedDocFixtureGeneratorRel = "internal/standards/testdata/fixture-census.go"
)

// generatedDocFixtureTree materialises the differently-shaped tree: go.mod, a
// document carrying a generated region and a generator instruction, a gate
// package under internal/, and NO cmd/ directory at all.
func generatedDocFixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":               generatedDocFixtureGoMod,
		generatedDocFixtureDoc: generatedDocFixtureDocBody,
		generatedDocFixtureGateDir + "/gate_test.go": "package fixture\n\nconst fixtureDoc = " + strconv.Quote(generatedDocFixtureDoc) + "\n",
		generatedDocFixtureGeneratorRel:              "package main\n\nfunc main() {}\n",
		generatedDocFixtureAnchorSource:              "package fixture\n",
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("fixture %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("fixture %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, generatedDocBaseDirs[0])); err != nil {
		t.Fatalf("the fixture must HAVE the %s base for this test to mean anything: %v",
			generatedDocBaseDirs[0], err)
	}
	if _, err := os.Stat(filepath.Join(root, generatedDocFixtureAbsentBase(t))); !os.IsNotExist(err) {
		t.Fatalf("the fixture must NOT have a %s base — that absent half is the whole shape under test "+
			"(stat err=%v)", generatedDocFixtureAbsentBase(t), err)
	}
	return root
}

// generatedDocFixtureAbsentBase names the base the fixture omits. It is READ OUT
// of generatedDocBaseDirs rather than typed, so the test cannot drift from the
// base list it is about (Rule 1/13).
func generatedDocFixtureAbsentBase(t *testing.T) string {
	t.Helper()
	for _, base := range generatedDocBaseDirs {
		if base != generatedDocFixturePresentBase {
			return base
		}
	}
	t.Fatalf("generatedDocBaseDirs holds no base beside %q — the absent-base fixture cannot be built",
		generatedDocFixturePresentBase)
	return ""
}

// generatedDocFixturePresentBase is the base the fixture DOES have.
const generatedDocFixturePresentBase = "internal"

// TestGeneratedDocGatesAnswersOnATreeWithoutTheBaseDir is the RED PROOF of the
// fix, driven through the exported surface a consumer calls: on a tree shaped
// like the real one minus its other half, GeneratedDocGates must ANSWER — no
// error — and the touch of a published anchor must still be MEASURED (the
// fixture's gate package comes back as required), which is exactly what the
// pre-fix code could not do: it returned the raw `lstat … no such file or
// directory` text and the caller abstained.
func TestGeneratedDocGatesAnswersOnATreeWithoutTheBaseDir(t *testing.T) {
	root := generatedDocFixtureTree(t)

	required, findings, err := GeneratedDocGates(root, []string{generatedDocFixtureAnchorSource})
	if err != nil {
		t.Fatalf("GeneratedDocGates on a tree with no %s/ dir: %v — a tree of a different shape is not a "+
			"broken edge, it is a tree the scan must ANSWER about", generatedDocFixtureAbsentBase(t), err)
	}
	if !generatedDocHas(required, generatedDocFixtureGateDir) {
		t.Errorf("the anchor touch of %q required %v, want it to contain %q — the touch must be MEASURED on "+
			"this tree, not abstained", generatedDocFixtureAnchorSource, required, generatedDocFixtureGateDir)
	}
	if len(findings) != 0 {
		t.Errorf("the fixture's edge is complete (a generator in the tree, an anchor, a gate package) yet the "+
			"scan reported findings: %v", findings)
	}

	// The same tree, a change that touches nothing published: no requirement and
	// still no error — the control that keeps the assertion above from being
	// satisfied by a predicate that requires everything for every change.
	if required, _, err := GeneratedDocGates(root, []string{"devdoc/unrelated.md"}); err != nil {
		t.Errorf("an unrelated change on the same tree: %v", err)
	} else if len(required) != 0 {
		t.Errorf("an unrelated change required %v, want nothing", required)
	}
}

// TestGeneratedDocGatesStatesTheBaseItCouldNotWalk is the OTHER half of the fix:
// the skip is VISIBLE. Skipping must not become silence — a scan that could not
// look under a base says so in the finding it produced, so a reader of "no test
// package reads this document" on a differently-shaped tree learns WHY the
// answer is short (Rule 15).
func TestGeneratedDocGatesStatesTheBaseItCouldNotWalk(t *testing.T) {
	root := generatedDocFixtureTree(t)
	// Remove the gate package so the "no package reads this document" finding is
	// produced — and then must carry the stated skip.
	if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(generatedDocFixtureGateDir))); err != nil {
		t.Fatalf("drop the fixture's gate package: %v", err)
	}

	deps, err := generatedDocDeps(root)
	if err != nil {
		t.Fatalf("discover the fixture's edges: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("the fixture should hold exactly one edge, got %d (%v)", len(deps), deps)
	}
	if got := deps[0].SkippedBases; len(got) != 1 || got[0] != generatedDocFixtureAbsentBase(t) {
		t.Errorf("the discovered edge recorded skipped bases %v, want exactly [%s] — a base the tree does not "+
			"have is skipped, and the skip is RECORDED rather than swallowed",
			got, generatedDocFixtureAbsentBase(t))
	}

	_, findings, err := GeneratedDocGates(root, nil)
	if err != nil {
		t.Fatalf("GeneratedDocGates: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("a document no package reads yielded no finding, so the shortening of the answer was silent")
	}
	joined := strings.Join(findings, "\n")
	if !strings.Contains(joined, generatedDocFixtureAbsentBase(t)) {
		t.Errorf("the findings do not name the base the scan could not walk (%s):\n%s",
			generatedDocFixtureAbsentBase(t), joined)
	}
}

// TestGeneratedDocGatePackagesRefusesAnUnusableBase is the line the fix must NOT
// cross: "the tree does not have it" and "I could not read it" are different
// facts (Rule 15). A base that is PRESENT but is not a directory — a file
// standing where the base belongs — stays a hard error, because a scan that
// answered "no package reads this document" for a tree it could not read would be
// the silent-empty answer this whole file exists to refuse.
func TestGeneratedDocGatePackagesRefusesAnUnusableBase(t *testing.T) {
	root := generatedDocFixtureTree(t)
	unusable := filepath.Join(root, generatedDocFixtureAbsentBase(t))
	if err := os.WriteFile(unusable, []byte("not a base dir"), 0o644); err != nil {
		t.Fatalf("place a file where the base belongs: %v", err)
	}
	_, _, err := generatedDocGatePackages(root, generatedDocFixtureDoc)
	if err == nil {
		t.Fatalf("a base that exists as a FILE yielded no error — a base that is there but unusable must fail " +
			"loudly, or the scan reports on a tree it never read")
	}
	if !strings.Contains(err.Error(), generatedDocFixtureAbsentBase(t)) {
		t.Errorf("the error does not name the base it refused (%s): %v", generatedDocFixtureAbsentBase(t), err)
	}

	// And the whole edge must refuse too: no half-answer from GeneratedDocGates.
	if _, _, err := GeneratedDocGates(root, []string{generatedDocFixtureAnchorSource}); err == nil {
		t.Errorf("GeneratedDocGates answered on a tree whose %s base is unreadable",
			generatedDocFixtureAbsentBase(t))
	}
}

// TestGeneratedDocBaseAvailableDistinguishesAbsentFromUnreadable drives the ONE
// predicate the fix turns on, at its own level. An absent base is a skip; a
// present-but-unreadable one is an error. A permission-proof arm runs only when
// the process is genuinely subject to the mode bits (root ignores them) — and it
// SKIPS BY NAME, never silently.
func TestGeneratedDocBaseAvailableDistinguishesAbsentFromUnreadable(t *testing.T) {
	root := t.TempDir()
	absent := filepath.Join(root, generatedDocFixtureAbsentBase(t))

	present, err := generatedDocBaseAvailable(absent, generatedDocFixtureAbsentBase(t))
	if err != nil || present {
		t.Errorf("an absent base reported present=%v err=%v, want present=false err=nil (the skip must not be "+
			"an error)", present, err)
	}

	// The unreadable arm needs a path os.Stat cannot RESOLVE: mode 000 on the
	// base itself still stats fine, so the permission is removed from the parent
	// the base lives under (EACCES on lookup, not ENOENT).
	parent := filepath.Join(root, "unreachable")
	locked := filepath.Join(parent, "base")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) }) //stylegate:ignore cleanup restores the fixture directory's mode after the unwritable-parent case
	if _, statErr := os.Stat(locked); statErr == nil {
		t.Skip("this process is not subject to the directory mode bits (running as root) — the unreadable-base " +
			"arm is skipped BY NAME here; the file-where-a-base-belongs arm above still covers the distinction")
	}
	if _, err := generatedDocBaseAvailable(locked, "base"); err == nil {
		t.Errorf("an unreadable base yielded no error — \"I could not look\" must not read as \"there is " +
			"nothing there\"")
	}
}

// generatedDocHas reports whether values holds want.
func generatedDocHas(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
