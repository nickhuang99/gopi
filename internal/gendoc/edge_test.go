package gendoc

// edge_test.go — the LOWEST-LEVEL guard for the edge in edge.go, and the red
// proof that makes it a guard rather than a description. It moved WITH the edge
// out of internal/standards, whose test files import internal/tools (see
// internal/standards/export_test.go for the cycle that forced the move).
//
// The predicate under test is `generatedDocImpact`: given the discovered edges
// and the files a change touched, it answers TWO things — which packages must
// run for the drift to be caught, and which edges are broken (a document whose
// generator is missing from the tree, a document with no gate package, a
// published anchor pointing at a file that does not exist). Both answers are
// findings-shaped so the red proofs below can drive the SAME function the
// real-tree assertion calls.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory to the module root (the
// local copy of the helper internal/standards uses — this package may not
// import that one, see the probes below).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s — cannot locate the module root", dir)
		}
		dir = parent
	}
}

// The probes this package's assertions are pinned on. They are the SAME values
// internal/standards' COMMANDS.md gate uses (its own copy, deliberately: this
// package must stay importable by the delivery path, and that package's test
// files import internal/tools — a value shared through an import would be a
// cycle. The tree is the source of truth both read, so a drift in either is a
// red test, not a divergence between two lists).
const (
	commandsDocName              = "COMMANDS.md"
	commandsDocGeneratorRel      = "internal/standards/testdata/commands-census.go"
	commandsDocAnchorSourceProbe = "cmd/agent/cli_tasks.go"
	standardsGatePackageRel      = "internal/standards"
)

func TestGeneratedDocDepsCoverTheReferenceDocuments(t *testing.T) {
	root := repoRoot(t)
	deps, err := Deps(root)
	if err != nil {
		t.Fatalf("discover the generated-document edges: %v", err)
	}
	if len(deps) < MinEdges {
		t.Fatalf("the scan found %d generated document(s) (floor %d) — either the walk did not read the "+
			"tree or the documents lost their generator instruction; refusing to pass blind",
			len(deps), MinEdges)
	}
	byDoc := map[string]Dep{}
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
	if !has(dep.AnchorSources, commandsDocAnchorSourceProbe) {
		t.Errorf("%s's published anchors do not include %q — the file whose drift started this epic is "+
			"exactly the edge this scan must keep visible", commandsDocName, commandsDocAnchorSourceProbe)
	}
	if !has(dep.GatePackages, standardsGatePackageRel) {
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
	deps, err := Deps(root)
	if err != nil {
		t.Fatalf("discover the generated-document edges: %v", err)
	}
	// The guard reads the EDGE half of the answer, the same half a consumer
	// reads: the one call also raises the tree-level DECLARATION invariant over
	// the REAL COMMANDS.md, which is a defect in the CONDITION the edge points
	// at and is owned by its own arm
	// (TestRegionAnchorDeclarationInvariantIsSilentOnTheRealBlock, which reads
	// the RAW answer because the ERROR is what that arm asserts). Measured
	// 2026-10-14 on base 18ff7fe: that document was stale there, so this guard
	// red on the ERROR and reported "the discovered edges are broken" about
	// edges that were intact. An edge proof must be satisfiable by the edge
	// predicate alone.
	if edge := edgeOf(findings(root, deps)); len(edge) > 0 {
		t.Errorf("the discovered edges are broken — a change against them cannot be mapped:\n  %s",
			strings.Join(edge, "\n  "))
	}
	// The change that started the epic: one line inserted into the file whose
	// line numbers COMMANDS.md publishes.
	required, _ := Impact(root, deps, []string{commandsDocAnchorSourceProbe})
	if !has(required, standardsGatePackageRel) {
		t.Errorf("a change to %q requires %v — %q is missing, so a line moved under a published anchor "+
			"would reach master with nothing on the committing branch running the check that blocks on it",
			commandsDocAnchorSourceProbe, required, standardsGatePackageRel)
	}
	// The control: a change to a file no document publishes anchors into obliges
	// nothing. Without it, a predicate that required every package for every
	// change would pass the assertion above.
	if required, _ := Impact(root, deps, []string{"devdoc/rules-reference.md"}); len(required) != 0 {
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
	ungated := []Dep{{Doc: commandsDocName, Generator: commandsDocGeneratorRel,
		AnchorSources: []string{commandsDocAnchorSourceProbe}}}
	required, findings := Impact(root, ungated, probe)
	if len(edgeOf(findings)) == 0 {
		t.Errorf("red-proof (a) failed: an edge with no gate package yielded no EDGE finding, so the change "+
			"would be mapped to nothing and pass silently (required=%v)", required)
	}
	// The control: the same edge WITH its gate package yields the requirement
	// and no EDGE finding — a predicate that always fired would prove nothing.
	// It reads the edge half of the answer only: the same call ALSO raises the
	// tree-level DECLARATION invariant over the REAL COMMANDS.md, whose own arms
	// own that class (TestRegionAnchorDeclarationInvariant*). Measured
	// 2026-10-14 on base 48ad72424: that document was stale, so the invariant
	// fired here and red the edge's red proof over a fact this proof does not
	// test — an edge proof must be satisfiable by the edge predicate alone.
	gated := []Dep{{Doc: commandsDocName, Generator: commandsDocGeneratorRel,
		AnchorSources: []string{commandsDocAnchorSourceProbe}, GatePackages: []string{standardsGatePackageRel}}}
	required, findings = Impact(root, gated, probe)
	// SeparateErrors is the production split the delivery path reads (Findings =
	// edge defects, Errors = the tree-level declaration invariant); the control
	// reads its FIRST half, never a second spelling of the marker.
	if edge, _ := SeparateErrors(findings); len(edge) != 0 {
		t.Errorf("red-proof (a) control failed: a well-formed edge yielded %v", edge)
	}
	if !has(required, standardsGatePackageRel) {
		t.Errorf("red-proof (a) control failed: the well-formed edge required %v", required)
	}

	// (b) a document naming a generator the tree does not have: the edge is
	// unusable — whoever follows it runs nothing.
	missing := []Dep{{Doc: commandsDocName, Generator: commandsDocStaleGeneratorProbe,
		AnchorSources: []string{commandsDocAnchorSourceProbe}, GatePackages: []string{standardsGatePackageRel}}}
	if _, findings := Impact(root, missing, probe); len(edgeOf(findings)) == 0 {
		t.Errorf("red-proof (b) failed: a generator that is not in the tree yielded no EDGE finding, so a "+
			"stale edge would be followed into a command that does not exist (answer=%v)", findings)
	}

	// (c) a document with no discovered anchor source: nothing can map a change
	// to it, which is the scan-not-run shape (a floor, not a pass).
	anchorless := []Dep{{Doc: commandsDocName, Generator: commandsDocGeneratorRel,
		GatePackages: []string{standardsGatePackageRel}}}
	if _, findings := Impact(root, anchorless, probe); len(edgeOf(findings)) == 0 {
		t.Errorf("red-proof (c) failed: an edge with no anchor source yielded no EDGE finding (answer=%v)", findings)
	}

	// (d) the read floor itself: a discovery that returned nothing on a tree
	// that HAS a corpus must be a finding, never an empty green answer.
	if _, findings := Impact(root, nil, probe); len(edgeOf(findings)) == 0 {
		t.Errorf("red-proof (d) failed: an empty discovery yielded no EDGE finding — a scan that found "+
			"nothing must be distinguishable from a scan that never ran (answer=%v)", findings)
	}
}

// edgeOf is the edge half of an Impact answer (see the control in red-proof (a)):
// the arms above must be red on the EDGE class, so that the tree-level
// declaration ERROR — which is present whenever the shipped document is stale —
// can never satisfy them in its place.
func edgeOf(findings []string) []string {
	edge, _ := SeparateErrors(findings)
	return edge
}

// TestGeneratedDocImpactSparesATreeWithNoCorpus is the regression guard for the
// measured defect of 2026-10-12 (brother of red-proof (d), and the reason it is
// phrased "on a tree that HAS a corpus").
//
// The read floor used to fire on ANY empty discovery, including a tree that has
// no generated-document corpus at all — which is every fixture repository the
// suite builds and every consumer tree of another shape. `Impact` then reported
// a broken edge, the delivery path refused (`gendocGateBrokenEdgeMarker`, the
// client-side half since removed with the trial merge), and the close-out
// stopped BEFORE the worktree release: 24 tests in internal/tools failed, every
// one of them a ground that was never reclaimed. The MERGER's half of the edge
// (`mergeset_gendoc.go`) has the same read-floor contract and is what this
// predicate protects now.
//
// The two directions are asserted together, because either one alone is passable
// by a wrong predicate: the SAME empty discovery is a finding on a tree that
// carries the corpus (the edge was lost) and is silent on a tree that does not
// (there was never an edge to lose).
func TestGeneratedDocImpactSparesATreeWithNoCorpus(t *testing.T) {
	probe := []string{commandsDocAnchorSourceProbe}

	// ① A tree with no corpus: a candidate markdown file that carries neither
	// the generated marker nor the verb-index marker, no go.mod, no cmd/, no
	// internal/. Nothing was lost here, so there is no broken edge to report.
	bare := t.TempDir()
	if err := os.WriteFile(filepath.Join(bare, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("write the corpus-less tree: %v", err)
	}
	present, err := CorpusPresent(bare)
	if err != nil {
		t.Fatalf("CorpusPresent on a corpus-less tree: %v", err)
	}
	if present {
		t.Fatalf("a tree with no generated region was reported as carrying the corpus")
	}
	if _, findings := Impact(bare, nil, probe); len(findings) != 0 {
		t.Errorf("a tree with NO generated-document corpus must not report a broken edge — nothing was "+
			"lost there, and reporting one refuses the close-out of a branch that never touched a "+
			"document (the measured 24-test failure): %v", findings)
	}

	// ② A tree that carries the corpus: the same empty discovery must STILL be a
	// finding — a document with the generated region and the regeneration
	// instruction whose edge the scan nevertheless lost.
	withCorpus := t.TempDir()
	if err := os.WriteFile(filepath.Join(withCorpus, "REFERENCE.md"),
		[]byte("# d\n\n<!-- BEGIN GENERATED -->\n\n| `internal/x/a.go:1` |\n\n<!-- END GENERATED -->\n\n"+
			"Regenerate with `go run internal/standards/testdata/x-census.go . REFERENCE.md`.\n"), 0o644); err != nil {
		t.Fatalf("write the corpus tree: %v", err)
	}
	present, err = CorpusPresent(withCorpus)
	if err != nil {
		t.Fatalf("CorpusPresent on a tree that carries the corpus: %v", err)
	}
	if !present {
		t.Fatalf("a document carrying the generated marker was NOT reported as a corpus — the predicate " +
			"would excuse the lost-edge case it exists for")
	}
	if _, findings := Impact(withCorpus, nil, probe); len(findings) == 0 {
		t.Errorf("on a tree that CARRIES the corpus an empty discovery must stay a finding (red-proof (d))")
	}

	// ③ An UNMEASURABLE corpus keeps the finding: a root that cannot be walked is
	// not a tree with no corpus (Rule 15 — "I could not look" is not "there is
	// nothing there").
	if _, findings := Impact(filepath.Join(bare, "no-such-dir"), nil, probe); len(findings) == 0 {
		t.Errorf("an unreadable root must keep the finding, never read as a corpus-less tree")
	}
	// ④ The clean tree — the shape every fixture repository and every consumer
	// tree has — is the one the delivery path actually sees: an empty answer,
	// no finding, so the close-out proceeds to the release.
	clean := t.TempDir()
	if err := os.WriteFile(filepath.Join(clean, "go.mod"), []byte("module fixture\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write the clean tree: %v", err)
	}
	if _, findings := Impact(clean, nil, probe); len(findings) != 0 {
		t.Errorf("a clean tree with no corpus must answer with no finding at all: %v", findings)
	}
}

// TestGeneratedDocCandidatesAreTheReferenceDocuments pins the discovery's scope
// against the tree: the documents it reads are the ones that carry a generated
// region or a generator instruction, and the walk finds at least the floor. A
// scope that silently emptied would make every assertion above vacuous.
func TestGeneratedDocCandidatesAreTheReferenceDocuments(t *testing.T) {
	root := repoRoot(t)
	docs, err := Candidates(root)
	if err != nil {
		t.Fatalf("list the candidate documents: %v", err)
	}
	if len(docs) < MinNodes {
		t.Fatalf("the walk found %d markdown document(s) (floor %d)", len(docs), MinNodes)
	}
	sorted := append([]string{}, docs...)
	sort.Strings(sorted)
	if !has(sorted, commandsDocName) {
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
	if !has(required, standardsGatePackageRel) {
		t.Errorf("the exported answer for %q is %v, want it to contain %q",
			commandsDocAnchorSourceProbe, required, standardsGatePackageRel)
	}
}

// --- the predicate under test -------------------------------------------------

// findings is the RAW answer of Impact over deps — both classes, EDGE and the
// tree-level DECLARATION ERROR. Two callers read it, deliberately differently:
// the edge assertion (TestGeneratedDocAnchorChangeRequiresItsGate) reads the
// EDGE half through edgeOf, because the ERROR is a fact about the document, not
// about the edge; the real-block arm
// (TestRegionAnchorDeclarationInvariantIsSilentOnTheRealBlock) reads RAW,
// because the ERROR is exactly what it asserts. Neither reads the other's half.
func findings(root string, deps []Dep) []string {
	_, findings := Impact(root, deps, nil)
	return findings
}

// The probes this file's red proofs poison an edge with. They are deliberately
// not trees that exist: the point of each is that the predicate must REPORT an
// unusable edge rather than follow it.
// commandsDocStaleGeneratorProbe is a generator path no tree holds — the red
// proof poisons an edge with it, and the predicate must REPORT it rather than
// follow it into a command that runs nothing.
const commandsDocStaleGeneratorProbe = "internal/standards/testdata/no-such-census.go"

// has reports whether values holds want.
func has(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// --- participation: the predicate that keeps a tree of ANOTHER SHAPE from
// being read as a broken edge (row 01a12de0) ---------------------------------
//
// The defect this answers, measured on pristine master: the delivery path ran
// `GeneratedDocGates` on an ARBITRARY deliverable.repo — the close-out's own
// throwaway fixtures among them — and read the discovery's empty result as
// "the published-anchor edge is BROKEN", which REFUSED the delivery and kept
// every worktree it was about. The two failures have opposite remedies and only
// the tree can tell them apart: ABSENT participation (no candidate document, or
// candidates with no generated region) is an ABSTENTION; a document that STATES
// a generated region and yields no followable edge is a FINDING, and it stays
// one.

// participationFixture is the smallest thing that measures the classification:
// a directory with a chosen set of markdown files in it.
// declarationGatePath and declarationGateBody materialise the fixture's OWN
// gate package: a package whose test reads the document the arms below write.
//
// Without it those arms would no longer measure the DECLARATION invariant alone
// (measured 2026-10-14): their fixtures name a generator under testdata/, so once
// the empty-gate-package ERROR class existed, the fixture carried a second,
// unrelated ERROR and the arms' CONTROL — "an anchor ON its declaration raises
// nothing" — red on a defect it does not test.
const declarationGatePath = "internal/declgate/gate_test.go"

func declarationGateBody() string {
	return "package declgate\n\n// This package blocks on \"DRIFTED.md\".\n"
}

func participationFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

// TestGeneratedDocParticipationDistinguishesAbsentFromPresent drives the ONE
// predicate at its own level: a tree with no candidate document, a tree whose
// candidates carry no generated region, and a tree that publishes one.
func TestGeneratedDocParticipationDistinguishesAbsentFromPresent(t *testing.T) {
	// (a) NO candidate document at all: the walk read the tree and found no
	// place a document of record could live. Absent, and absent for its own
	// stated reason.
	part, reading, reason := ClassifyParticipation(0, 0, 0)
	if part != ParticipationAbsent {
		t.Fatalf("a tree with no candidate document classified as %q, want %q", part, ParticipationAbsent)
	}
	if reading != NoCandidateCode {
		t.Errorf("the reading is %q, want %q — an abstention states WHICH measurement came back empty",
			reading, NoCandidateCode)
	}
	if reason == "" {
		t.Errorf("the abstention carries no reason — a skip without a reason is the silent pass this " +
			"predicate exists to prevent")
	}

	// (b) candidates, none of them a generated document.
	part, reading, reason = ClassifyParticipation(3, 0, 0)
	if part != ParticipationAbsent {
		t.Fatalf("a tree whose candidates carry no generated region classified as %q, want %q",
			part, ParticipationAbsent)
	}
	if reading != NoRegionCode {
		t.Errorf("the reading is %q, want %q", reading, NoRegionCode)
	}
	if !strings.Contains(reason, "3") {
		t.Errorf("the abstention must state the count it measured, got %q", reason)
	}

	// (c) at least one generated document: PRESENT, and no abstention text.
	part, reading, reason = ClassifyParticipation(5, 1, 1)
	if part != ParticipationPresent {
		t.Fatalf("a tree holding a generated document classified as %q, want %q", part, ParticipationPresent)
	}
	if reading != "" || reason != "" {
		t.Errorf("a participating tree must carry no abstention (reading=%q reason=%q)", reading, reason)
	}

	// (d) THE TOOTH, at the predicate's own level: candidates that CARRY a
	// generated region while the discovery derived no edge from them. The tree
	// takes part — a document STATED it owns a generated region — so the edge's
	// read floor (a finding, a refusal on the delivery path) must still speak.
	// Counting only the doc-side edges would let a lost `go run` instruction
	// turn a real defect into a quiet abstention.
	part, reading, reason = ClassifyParticipation(2, 1, 0)
	if part != ParticipationPresent {
		t.Fatalf("a tree whose document states a generated region but yielded no edge classified as %q, "+
			"want %q — that tree does not have a different SHAPE, it has a broken edge", part, ParticipationPresent)
	}
	if reading != "" || reason != "" {
		t.Errorf("a participating tree must carry no abstention (reading=%q reason=%q)", reading, reason)
	}
}

// TestGeneratedDocAnswerOnATreeThatDoesNotParticipate is the same question
// through the surface a CONSUMER calls: on a tree the edge does not apply to,
// there must be NO finding (a finding is a refusal on the delivery path — the
// defect), the counts must be stated, and the tree must not be reported as
// taking part.
func TestGeneratedDocAnswerOnATreeThatDoesNotParticipate(t *testing.T) {
	root := participationFixture(t, map[string]string{
		"README.md": "# a tree of another shape\n\nno generated region, no regeneration command\n",
	})
	ans, err := GeneratedDocAnswer(root, []string{"src/main.go"})
	if err != nil {
		t.Fatalf("GeneratedDocAnswer on a tree with no document of record: %v — a differently-shaped "+
			"tree is a tree the edge must ANSWER about, not fail on", err)
	}
	if len(ans.Findings) != 0 {
		t.Fatalf("a tree with no document of record produced findings (%v) — on the delivery path those "+
			"were read as a BROKEN edge and refused every delivery in a repository that never had one",
			ans.Findings)
	}
	if ans.TakesPart() {
		t.Errorf("a tree with no generated document reported that it takes part in the edge")
	}
	if ans.Participation != ParticipationAbsent || ans.Reading == "" || ans.Reason == "" {
		t.Errorf("the abstention is not fully stated: participation=%q reading=%q reason=%q",
			ans.Participation, ans.Reading, ans.Reason)
	}
	if ans.Candidates != 1 || ans.Docs != 0 {
		t.Errorf("the counts behind the abstention are %d candidate(s) / %d edge(s), want 1 / 0 — the "+
			"reader must be able to see that the scan DID read the tree", ans.Candidates, ans.Docs)
	}

	// The control: the SAME call on a tree that DOES publish a generated
	// document reports participation — a predicate that answered "absent" for
	// everything would satisfy the assertions above and mean nothing.
	// (The real tree is the one such tree this package can read without standing
	// up another fixture: COMMANDS.md states its own regeneration command.)
	root = repoRoot(t)
	ans, err = GeneratedDocAnswer(root, []string{commandsDocAnchorSourceProbe})
	if err != nil {
		t.Fatalf("GeneratedDocAnswer on the real tree: %v", err)
	}
	if !ans.TakesPart() {
		t.Fatalf("the real tree publishes %s and was classified %q — the edge this package exists for "+
			"would be invisible", commandsDocName, ans.Participation)
	}
}

// TestGeneratedDocAnswerKeepsABrokenEdgeAFinding is THE GUARD the fix must not
// eat: a tree that STATES a generated region and yields no followable edge is a
// finding, on the same call whose absent-tree answer carries none. Without this
// arm, "no findings on a tree of another shape" could have been bought by
// deleting the finding entirely — and that finding is the defect the whole edge
// was built for.
func TestGeneratedDocAnswerKeepsABrokenEdgeAFinding(t *testing.T) {
	// A document that states a generated region and a regeneration command whose
	// generator the tree does not hold: exactly the shape a reader would follow
	// into a command that runs nothing.
	root := participationFixture(t, map[string]string{
		"PUBLISHED.md": "# published\n\n<!-- BEGIN GENERATED -->\n\n| `pkg/a.go:3` |\n\n" +
			"<!-- END GENERATED -->\n\nRegenerate with `go run internal/standards/testdata/absent-census.go .`.\n",
		"pkg/x.go": "package pkg\n",
	})
	ans, err := GeneratedDocAnswer(root, []string{"pkg/a.go"})
	if err != nil {
		t.Fatalf("GeneratedDocAnswer: %v", err)
	}
	if !ans.TakesPart() {
		t.Fatalf("a tree publishing %s was classified %q — a document that STATES a generated region "+
			"makes the tree participate, whatever shape the rest of it has",
			"PUBLISHED.md", ans.Participation)
	}
	if len(ans.Findings) == 0 {
		t.Fatalf("a document naming a generator the tree does not hold produced NO finding — the edge " +
			"is unusable and a reader following it runs nothing (this is the arm the absent-tree fix " +
			"must not have deleted)")
	}
	joined := strings.Join(ans.Findings, "\n")
	if !strings.Contains(joined, "PUBLISHED.md") {
		t.Errorf("the finding does not name the document whose edge is broken:\n%s", joined)
	}
}

// --- the DECLARATION invariant -------------------------------------------------

// TestRegionAnchorDeclarationInvariantRedProof drives the invariant with the
// EXACT stale shape a real drift produced, on a fixture: a generated region that
// publishes `pkg/b.go:6`, where line 6 is the comment line ABOVE the declaration
// the anchor was written for (line 7).
//
// The point of the arm is that the anchor RESOLVES — line 6 exists — so a
// reference check that only asks "does the anchor land on a line, and not on a
// blank one" passes it. That was measured on the real tree on 2026-10-13:
// `cmd/agent/cli_ui.go:125` (the comment above `func isUISubcommand` at 126) and
// `cmd/agent/cli_doccheck.go:234` (the comment above `func isDocCheckSubcommand`
// at 246) were both stale and both passed the resolution check; only the
// block-vs-source comparison caught them, in a package the committing branch
// need not run.
func TestRegionAnchorDeclarationInvariantRedProof(t *testing.T) {
	// The drift: the anchor names the comment line above the declaration.
	stale := participationFixture(t, map[string]string{
		"DRIFTED.md": "# drifted\n\n<!-- BEGIN GENERATED -->\n\n" +
			"| `pkg/b.go:6` |\n\n<!-- END GENERATED -->\n\n" +
			"Regenerate with `go run internal/standards/testdata/x-census.go . DRIFTED.md`.\n",
		"pkg/b.go":          "package pkg\n\n// 1\n// 2\n// 3\n// the comment above the declaration\nfunc b() {}\n",
		declarationGatePath: declarationGateBody(),
	})
	ans, err := GeneratedDocAnswer(stale, []string{"pkg/b.go"})
	if err != nil {
		t.Fatalf("GeneratedDocAnswer on the drifted fixture: %v", err)
	}
	if !ans.TakesPart() {
		t.Fatalf("a tree publishing DRIFTED.md was classified %q", ans.Participation)
	}
	if len(ans.Errors) == 0 {
		t.Fatalf("the region publishes an anchor on a COMMENT line and no ERROR finding was raised — "+
			"the anchor resolves, so nothing else in the tree would name it (findings=%v)", ans.Findings)
	}
	joinedErrors := strings.Join(ans.Errors, "\n")
	for _, must := range []string{"DRIFTED.md", "pkg/b.go:6", "comment line"} {
		if !strings.Contains(joinedErrors, must) {
			t.Errorf("the ERROR finding does not name %q:\n%s", must, joinedErrors)
		}
	}
	// The ERROR is its OWN channel: it does not ride Findings, so the refusal
	// arms that existed before it keep the behaviour they had (see
	// SeparateErrors for the measured reason).
	if len(ans.Errors) == 0 {
		t.Errorf("the ERROR class did not carry the finding (Errors empty, Findings=%v)", ans.Findings)
	}
	if strings.Contains(strings.Join(ans.Findings, "\n"), ErrorFindingPrefix) {
		t.Errorf("the ERROR rode Findings as well — every existing refusal arm that scans Findings changes "+
			"behaviour as a side effect of this class:\n%s", strings.Join(ans.Findings, "\n"))
	}

	// The control: the SAME fixture with the anchor moved onto the declaration is
	// silent — an invariant that fired on every anchor would prove nothing.
	coherent := participationFixture(t, map[string]string{
		"DRIFTED.md": "# drifted\n\n<!-- BEGIN GENERATED -->\n\n" +
			"| `pkg/b.go:7` |\n\n<!-- END GENERATED -->\n\n" +
			"Regenerate with `go run internal/standards/testdata/x-census.go . DRIFTED.md`.\n",
		"pkg/b.go":          "package pkg\n\n// 1\n// 2\n// 3\n// the comment above the declaration\nfunc b() {}\n",
		declarationGatePath: declarationGateBody(),
	})
	ans, err = GeneratedDocAnswer(coherent, []string{"pkg/b.go"})
	if err != nil {
		t.Fatalf("GeneratedDocAnswer on the coherent fixture: %v", err)
	}
	if len(ans.Errors) != 0 {
		t.Errorf("an anchor ON its declaration raised %v", ans.Errors)
	}

	// The blank-line arm: the drift shape where the declaration moved DOWN far
	// enough that the anchor lands in white space. (The block's own resolution
	// check looks for exactly this, so the invariant must name it too.)
	blank := participationFixture(t, map[string]string{
		"DRIFTED.md": "# drifted\n\n<!-- BEGIN GENERATED -->\n\n" +
			"| `pkg/b.go:5` |\n\n<!-- END GENERATED -->\n\n" +
			"Regenerate with `go run internal/standards/testdata/x-census.go . DRIFTED.md`.\n",
		"pkg/b.go":          "package pkg\n\n// 1\n// 2\n\n// the comment above the declaration\nfunc b() {}\n",
		declarationGatePath: declarationGateBody(),
	})
	ans, err = GeneratedDocAnswer(blank, []string{"pkg/b.go"})
	if err != nil {
		t.Fatalf("GeneratedDocAnswer on the blank-line fixture: %v", err)
	}
	if !strings.Contains(strings.Join(ans.Errors, "\n"), "blank line") {
		t.Errorf("an anchor landing on a BLANK line raised no blank-line ERROR: %v", ans.Errors)
	}
}

// TestRegionAnchorSymbolShapeIsExtractedAndStillInvariant is the regression
// guard for the vacuity this arm was blind to (measured on master 02e138f,
// 2026-10-25). f571564a0 re-keyed COMMANDS.md's generated block on SYMBOL
// IDENTITY — a cell now reads `cmd/agent/cli_notify.go:isNotifySubcommand`
// instead of `cli_notify.go:611` — while AnchorPattern still demanded `:(\d+)`.
// RegionAnchors therefore returned ZERO anchors over a block that publishes 86,
// and the real-tree arm below caught it as "the region published no anchor at
// all". That guard works, but it only fires on the real block: nothing proved
// the invariant still has teeth ON the symbol shape. This arm plants the three
// symbol-shape outcomes and requires each to land on its own side:
//
//   - a symbol that IS declared in the cited file → silent (the control);
//   - a symbol that is NOT declared there → ERROR (drifted name, e.g. a rename
//     the generator was not re-run for);
//   - a symbol whose cited FILE does not exist → ERROR, never silently skipped
//     (an unmeasurable anchor reading as a satisfied one is the failure mode
//     Rule 15 names).
func TestRegionAnchorSymbolShapeIsExtractedAndStillInvariant(t *testing.T) {
	// (a) the control: the block addresses a real declaration by NAME.
	coherent := participationFixture(t, map[string]string{
		"SYM.md": "# sym\n\n<!-- BEGIN GENERATED -->\n\n" +
			"| `gopi thing` | routed by `pkg/b.go:b` |\n\n<!-- END GENERATED -->\n\n" +
			"Regenerate with `go run internal/standards/testdata/x-census.go . SYM.md`.\n",
		"pkg/b.go":                       "package pkg\n\n// a comment\nfunc b() {}\n",
		"internal/declgate/gate_test.go": strings.Replace(declarationGateBody(), "DRIFTED.md", "SYM.md", 1),
	})
	coherentDeps, err := Deps(coherent)
	if err != nil {
		t.Fatalf("Deps on the coherent symbol fixture: %v", err)
	}
	if len(coherentDeps) != 1 {
		t.Fatalf("the symbol fixture discovered %d edge(s), want 1", len(coherentDeps))
	}
	// THE EXTRACTION arm: a symbol-shaped span must be READ. Without this, the
	// three outcome arms below would pass by reading nothing (the exact shape
	// of the 02e138f defect).
	if got := coherentDeps[0].RegionAnchors; len(got) != 1 || got[0] != "pkg/b.go:b" {
		t.Fatalf("the block publishes `pkg/b.go:b` and the reader extracted %v — an extraction that "+
			"reads no symbol-shaped anchor makes every arm below vacuous", got)
	}
	if got := strings.Join(findings(coherent, coherentDeps), "\n"); strings.Contains(got, ErrorFindingPrefix) {
		t.Errorf("an anchor naming a real declaration raised an ERROR:\n%s", got)
	}

	// (b) the drift: the block names a declaration the file does not carry.
	renamed := participationFixture(t, map[string]string{
		"SYM.md": "# sym\n\n<!-- BEGIN GENERATED -->\n\n" +
			"| `gopi thing` | routed by `pkg/b.go:bOld` |\n\n<!-- END GENERATED -->\n\n" +
			"Regenerate with `go run internal/standards/testdata/x-census.go . SYM.md`.\n",
		"pkg/b.go":                       "package pkg\n\n// a comment\nfunc b() {}\n",
		"internal/declgate/gate_test.go": strings.Replace(declarationGateBody(), "DRIFTED.md", "SYM.md", 1),
	})
	renamedDeps, err := Deps(renamed)
	if err != nil {
		t.Fatalf("Deps on the renamed-symbol fixture: %v", err)
	}
	renamedFindings := strings.Join(findings(renamed, renamedDeps), "\n")
	if !strings.Contains(renamedFindings, ErrorFindingPrefix) {
		t.Fatalf("a symbol anchor naming no declaration raised no ERROR — the invariant reads symbol "+
			"anchors as nothing to check:\n%s", renamedFindings)
	}
	for _, must := range []string{"SYM.md", "pkg/b.go:bOld", "no declaration of that name"} {
		if !strings.Contains(renamedFindings, must) {
			t.Errorf("the symbol-drift ERROR does not name %q:\n%s", must, renamedFindings)
		}
	}

	// (c) the unmeasurable anchor: the cited file is not in the tree. This must
	// be a FINDING, not a skip: an anchor nobody could look up is not a
	// satisfied anchor.
	missing := participationFixture(t, map[string]string{
		"SYM.md": "# sym\n\n<!-- BEGIN GENERATED -->\n\n" +
			"| `gopi thing` | routed by `pkg/gone.go:b` |\n\n<!-- END GENERATED -->\n\n" +
			"Regenerate with `go run internal/standards/testdata/x-census.go . SYM.md`.\n",
		"pkg/b.go":                       "package pkg\n\n// a comment\nfunc b() {}\n",
		"internal/declgate/gate_test.go": strings.Replace(declarationGateBody(), "DRIFTED.md", "SYM.md", 1),
	})
	missingDeps, err := Deps(missing)
	if err != nil {
		t.Fatalf("Deps on the missing-file fixture: %v", err)
	}
	if got := strings.Join(findings(missing, missingDeps), "\n"); !strings.Contains(got, ErrorFindingPrefix) {
		t.Fatalf("an anchor citing a file the tree does not carry raised no ERROR:\n%s", got)
	} else if !strings.Contains(got, "could not be measured") {
		t.Errorf("the unmeasurable-anchor finding does not say it could not be measured:\n%s", got)
	}
}

// regionAnchorFloorViolation is the COUNT FLOOR the invariant's silence is
// measured against: a document that carries a generated region and publishes no
// anchor at all FAILS, and this returns the sentence that says so. Without it,
// "the invariant raised no finding" has two indistinguishable meanings — every
// anchor resolved (the good case) and the reader extracted nothing (the 02e138f
// defect, where the whole symbol-keyed block read as zero anchors). Rule
// 15/36/38/42: a scan that read nothing must be distinguishable from a clean
// one.
//
// It returns a SENTENCE rather than calling t.Fatalf so that the real tree and
// the planted fixture drive the identical predicate: a guard proved only by the
// real tree is untested on the shape that breaks it, and a guard proved only by
// a comment is not a guard. Empty return = the floor is satisfied.
func regionAnchorFloorViolation(doc string, anchors []string) string {
	if len(anchors) == 0 {
		return doc + "'s generated region published no anchor at all — the invariant would read the " +
			"whole block as nothing to check"
	}
	return ""
}

// TestRegionAnchorCountFloorRefusesADocumentThatPublishesNothing plants the
// vacuity directly: a candidate document with a generated region and NOT ONE
// anchor inside it. The floor must fire. This is the arm that stops a future
// extraction regression (the 02e138f shape) from being absorbed by relaxing the
// invariant into "no anchor = nothing to check".
func TestRegionAnchorCountFloorRefusesADocumentThatPublishesNothing(t *testing.T) {
	root := participationFixture(t, map[string]string{
		"EMPTY.md": "# empty\n\n<!-- BEGIN GENERATED -->\n\n| no anchors here |\n\n<!-- END GENERATED -->\n\n" +
			"Regenerate with `go run internal/standards/testdata/x-census.go . EMPTY.md`.\n",
		"internal/declgate/gate_test.go": strings.Replace(declarationGateBody(), "DRIFTED.md", "EMPTY.md", 1),
	})
	deps, err := Deps(root)
	if err != nil {
		t.Fatalf("Deps on the anchorless fixture: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("the anchorless fixture discovered %d edge(s), want 1", len(deps))
	}
	// The fixture really IS the shape being guarded: a region, and zero anchors.
	if got := deps[0].RegionAnchors; len(got) != 0 {
		t.Fatalf("the fixture published %v — it is not the anchorless shape this arm exists for", got)
	}
	got := regionAnchorFloorViolation("EMPTY.md", deps[0].RegionAnchors)
	if got == "" {
		t.Fatalf("the floor accepted a document that publishes no anchor — a reader that extracts " +
			"nothing would read as a clean tree")
	}
	for _, must := range []string{"EMPTY.md", "published no anchor at all"} {
		if !strings.Contains(got, must) {
			t.Errorf("the floor's sentence does not name %q: %s", must, got)
		}
	}
	// The CONTROL: the same predicate on a document that DOES publish an anchor
	// stays silent, so the arm above is not satisfied by a floor that always
	// fires.
	if got := regionAnchorFloorViolation("EMPTY.md", []string{"pkg/b.go:b"}); got != "" {
		t.Errorf("the floor fired on a document that publishes an anchor: %s", got)
	}
}

// TestRegionAnchorDeclarationInvariantIsSilentOnTheRealBlock is the real-tree
// arm, and it is the one that separates this invariant from a fiction: on the
// tree as it stands (the regenerated block), every anchor published inside
// COMMANDS.md's generated region resolves to a declaration, so the invariant
// raises nothing — while the fixture arms above prove it is not silent in
// principle.
//
// Without this arm the invariant could be satisfied by reporting nothing at all,
// which is the "a check that found nothing must be distinguishable from a check
// that never ran" failure mode (Rule 15). The count floor below is what makes
// "nothing was raised" a statement about anchors that WERE read.
func TestRegionAnchorDeclarationInvariantIsSilentOnTheRealBlock(t *testing.T) {
	root := repoRoot(t)
	deps, err := Deps(root)
	if err != nil {
		t.Fatalf("discover the edges: %v", err)
	}
	var regionAnchors int
	var floorViolation string
	for _, dep := range deps {
		if dep.Doc != commandsDocName {
			continue
		}
		regionAnchors = len(dep.RegionAnchors)
		floorViolation = regionAnchorFloorViolation(commandsDocName, dep.RegionAnchors)
		for _, ref := range dep.RegionAnchors {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ref[:strings.LastIndex(ref, ":")]))); err != nil {
				t.Errorf("the region publishes %s, whose file is not in the tree: %v", ref, err)
			}
		}
	}
	if regionAnchors == 0 {
		t.Fatalf("%s carries no discovered edge — the real-tree arm measured nothing", commandsDocName)
	}
	// The SAME floor the planted fixture drives above; a violation here means
	// the reader extracted nothing from a block that publishes anchors.
	if floorViolation != "" {
		t.Fatalf("%s", floorViolation)
	}
	if got := strings.Join(findings(root, deps), "\n"); strings.Contains(got, ErrorFindingPrefix) {
		t.Errorf("the real tree's generated region publishes an anchor that does not resolve to a "+
			"declaration — regenerate the block with its own writer, never hand-fix the anchor:\n%s", got)
	}
}

// --- the EMPTY-GATE-PACKAGE ERROR CLASS ---------------------------------------

// The fixture's own names. The document is a ROOT document (the candidate scope
// includes the module root), its generator lives under a BASE DIRECTORY but
// inside testdata/ — the shape measured on the real tree, where COMMANDS.md's
// generator is internal/standards/testdata/commands-census.go — and the anchor
// source it publishes a line number for is a production file.
const (
	emptyGateDocName      = "REFERENCE.md"
	emptyGateGeneratorRel = "internal/standards/testdata/ref-census.go"
	emptyGateGeneratorDir = "internal/standards/testdata"
	emptyGateAnchorSource = "internal/refgate/a.go"
	emptyGateLine         = 3
	emptyGateGateDir      = "internal/refgate"
	emptyGateGateTestName = "gate_test.go"
	// emptyGateForeignBaseDir is a package under the OTHER base directory, so the
	// fixture is a complete tree: the class asserted below is a claim about a walk
	// that entered both bases.
	emptyGateForeignBaseDir = "cmd/refcmd"
	emptyGateDocBodyFormat  = "# reference\n\n<!-- BEGIN GENERATED -->\n\n" +
		"| `gopi thing` | routed by `" + emptyGateAnchorSource + ":%d` |\n\n<!-- END GENERATED -->\n\n" +
		"Regenerate with `go run " + emptyGateGeneratorRel + " . " + emptyGateDocName + "`.\n"
)

// emptyGateFixtureTree materialises the tree the class is measured on:
//
//   - the ROOT document, carrying a generated region, an anchor into a
//     production source, and its own regeneration instruction;
//   - the generator program it names, under a base directory but inside
//     testdata/ — where the Go tool does not test it. The program does NOT carry
//     the document's name (a regenerator takes the document as an argument), so
//     NOTHING in the module names it: the set is empty by construction, which is
//     the measured shape of the real tree.
//
// withGate adds the arm the control turns on: a package under a base directory
// whose test names the document — a gate package the edge CAN name.
func emptyGateFixtureTree(t *testing.T, withGate bool) string {
	t.Helper()
	files := map[string]string{
		emptyGateDocName:                         fmt.Sprintf(emptyGateDocBodyFormat, emptyGateLine),
		emptyGateAnchorSource:                    "package refgate\n\nfunc A() string { return \"A\" }\n",
		emptyGateGeneratorDir + "/ref-census.go": "package main\n\nfunc main() {}\n",
		// Both BASES exist, as they do on the real tree: the class is a claim
		// about a COMPLETE walk ("none can exist"), and a tree missing a base
		// would make it an unmeasured claim instead (see Dep.SkippedBases).
		emptyGateForeignBaseDir + "/main.go": "package main\n\nfunc main() {}\n",
	}
	if withGate {
		files[emptyGateGateDir+"/"+emptyGateGateTestName] = "package refgate\n\n// This package blocks on \"" +
			emptyGateDocName + "\".\nfunc TestDoc(t *testing.T) {}\n"
	}
	return participationFixture(t, files)
}

// TestGeneratedDocEmptyGatePackageIsAnErrorClass is the RED PROOF of the class
// this change adds, and it drives the REAL surface a committing path reads
// (GeneratedDocAnswer) rather than an internal predicate.
//
// (a) THE DEFECT. A root document that carries a generated region, states its
// own regeneration instruction, and names a generator that lives under
// testdata/ derives NO gate package — and none CAN be derived: GatePackages walks
// the module's own layout, the only test that names the document is the
// generator's own, and that directory is a program, so `go test` builds nothing
// for it. Before this change the edge reported a plain FINDING and every
// committing path that refuses on a broken edge kept merging: a line moved under
// one of the document's anchors reached the integration branch with nobody able
// to catch it. Now it is the ERROR class those paths act on.
func TestGeneratedDocEmptyGatePackageIsAnErrorClass(t *testing.T) {
	root := emptyGateFixtureTree(t, false)

	ans, err := GeneratedDocAnswer(root, []string{emptyGateAnchorSource})
	if err != nil {
		t.Fatalf("GeneratedDocAnswer on the fixture: %v", err)
	}
	if !ans.TakesPart() {
		t.Fatalf("a tree publishing %s was classified %q — a document that STATES a generated region "+
			"makes the tree participate", emptyGateDocName, ans.Participation)
	}
	if ans.Docs != 1 {
		t.Fatalf("the fixture should hold exactly one edge, got %d — the measurement below is about it", ans.Docs)
	}
	// THE MEASUREMENT the class is derived from: no gate package, and the
	// derivation impossible.
	deps, err := Deps(root)
	if err != nil {
		t.Fatalf("discover the fixture's edges: %v", err)
	}
	if len(deps[0].GatePackages) != 0 {
		t.Fatalf("the fixture's document has gate package(s) %v — the class is measured on an EMPTY set",
			deps[0].GatePackages)
	}
	if !deps[0].GateUnderivable {
		t.Fatalf("the fixture's generator %s is the only reader of %s and is a program the Go tool does not "+
			"test, so the gate set must be reported UNDIVABLE (GatePackages=%v)",
			emptyGateGeneratorRel, emptyGateDocName, deps[0].GatePackages)
	}

	if len(ans.Errors) == 0 {
		t.Fatalf("a document no package can gate produced NO ERROR — the hole is exactly what a committing "+
			"path must refuse on (findings=%v)", ans.Findings)
	}
	joined := strings.Join(ans.Errors, "\n")
	for _, must := range []string{emptyGateDocName, emptyGateGeneratorRel, ErrorFindingPrefix, EmptyGateDerivedMark} {
		if !strings.Contains(joined, must) {
			t.Errorf("the ERROR does not name %q:\n%s", must, joined)
		}
	}
	// THE CLASS IS ITS OWN CHANNEL: the sentence rides Errors, never Findings, so
	// every refusal arm that existed before this class keeps the behaviour it had
	// (see SeparateErrors for the measured reason).
	if strings.Contains(strings.Join(ans.Findings, "\n"), EmptyGateDerivedMark) {
		t.Errorf("the ERROR-class sentence rode Findings as well — an existing arm that scans Findings "+
			"changes behaviour as a side effect of this class:\n%s", strings.Join(ans.Findings, "\n"))
	}
	if got := EmptyGateDerivedErrors(ans.Errors); len(got) != 1 {
		t.Errorf("the sub-class selector returned %d of the fixture's %d ERROR(s) — the committing paths "+
			"select on it, and a selector that misses its own class is a refusal that never fires", len(got), len(ans.Errors))
	}
	// The change obliges nothing runnable: the document's anchors ARE touched,
	// and the packages the edge would have named do not exist. A consumer reading
	// Required alone would run an empty check and pass.
	if len(ans.Required) != 0 {
		t.Errorf("the fixture's change required %v, want nothing — the gate packages the edge could name are "+
			"the thing this class reports the absence of", ans.Required)
	}

	// (b) THE CONTROL, and it is what keeps the class from being noise: the SAME
	// document, on a tree where a package that is NOT a program reads it, is
	// neither an ERROR nor a Finding — the class fires on the DERIVATION being
	// impossible, never on a gate nobody has written yet.
	gated := emptyGateFixtureTree(t, true)
	ans, err = GeneratedDocAnswer(gated, []string{emptyGateAnchorSource})
	if err != nil {
		t.Fatalf("GeneratedDocAnswer on the gated fixture: %v", err)
	}
	if len(ans.Errors) != 0 {
		t.Errorf("a document a real gate package reads raised %v", ans.Errors)
	}
	if len(ans.Findings) != 0 {
		t.Errorf("a document a real gate package reads raised findings %v", ans.Findings)
	}
	if !has(ans.Required, emptyGateGateDir) {
		t.Errorf("the gated fixture's change required %v, want it to contain %q — the control must prove the "+
			"edge still names a derived gate", ans.Required, emptyGateGateDir)
	}

	// (c) THE OPERATOR'S SKIP retires the class and NOTHING ELSE, and it says so:
	// the channel is empty and the Answer records that it was emptied, so a
	// consumer cannot read a retired class as a clean edge.
	ans, err = GeneratedDocAnswerOpts(root, []string{emptyGateAnchorSource}, true)
	if err != nil {
		t.Fatalf("GeneratedDocAnswerOpts: %v", err)
	}
	if len(ans.Errors) != 0 {
		t.Errorf("the skip did not retire the ERROR class: %v", ans.Errors)
	}
	if !ans.Skip {
		t.Errorf("the skip emptied the channel without recording the decision — an emptied channel a reader " +
			"cannot tell from a clean edge (Rule 15)")
	}
	if !ans.TakesPart() {
		t.Errorf("the skip changed the participation classification to %q", ans.Participation)
	}

	// (d) THE COMPLETENESS VETO, measured on the shape that broke a real test
	// (2026-10-14): the SAME document on a tree that has no cmd/ half walks only
	// one base, so "no package names it" was measured over a walk that could not
	// enter the other. The set is empty and the finding stays the NOTE it always
	// was — an incomplete walk must not be turned into a refusal (Rule 15: "I
	// could not look" and "there is nothing there" have opposite remedies).
	partial := participationFixture(t, map[string]string{
		emptyGateDocName:                         fmt.Sprintf(emptyGateDocBodyFormat, emptyGateLine),
		emptyGateAnchorSource:                    "package refgate\n\nfunc A() string { return \"A\" }\n",
		emptyGateGeneratorDir + "/ref-census.go": "package main\n\nfunc main() {}\n",
	})
	deps, err = Deps(partial)
	if err != nil {
		t.Fatalf("discover the partial tree's edges: %v", err)
	}
	if len(deps[0].SkippedBases) == 0 {
		t.Fatalf("the partial fixture was expected to miss a base dir, so the arm measures nothing")
	}
	if deps[0].GateUnderivable {
		t.Errorf("a tree whose walk could not enter every base claimed the gate set CANNOT be derived — " +
			"that claim needs a complete walk")
	}
	ans, err = GeneratedDocAnswer(partial, []string{emptyGateAnchorSource})
	if err != nil {
		t.Fatalf("GeneratedDocAnswer on the partial tree: %v", err)
	}
	if got := EmptyGateDerivedErrors(ans.Errors); len(got) != 0 {
		t.Errorf("an incomplete walk raised the ERROR class %v", got)
	}
}

// --- the PROSE population: the anchors a document carries OUTSIDE its
// generated regions (task 01a195c9) -------------------------------------------
//
// THE HOLE THIS CLOSES, measured 2026-11-02 on this tree. The declaration
// invariant read RegionAnchors and nothing else, so every anchor typed into a
// document's HAND-WRITTEN prose was never content-checked: COMMANDS.md carried
// 107 such spans beside its 84 rendered ones, and 20 of them — every one a
// line-number address — pointed at a comment or blank line while
// `go test ./internal/gendoc/ ./internal/standards/ -run 'Generated|Anchor'`
// stayed green. The two spans the row was opened for
// (`cmd/agent/cli_scratch_gc.go:555`, `:594`) were of exactly that shape.
//
// The fix widens the POPULATION and leaves the PREDICATE alone. The arms below
// are the ones that make that a fact rather than a claim:
//
//   - a planted prose anchor on a comment line turns the gate RED, and the same
//     anchor moved onto its declaration turns it green again (the control);
//   - a blank-line arm, because a prose anchor stales DOWN as often as up;
//   - a NON-VACUITY floor: a document that publishes prose anchors must have
//     been JUDGED, so a scan that read the wrong population cannot pass as a
//     clean one;
//   - the real tree, reporting HOW MANY prose anchors it judged.

// proseAnchorDocName is the fixture document. It carries a generated region AND
// prose beside it, which is the shape of every real document of record.
const proseAnchorDocName = "PROSE.md"

// proseAnchorFixture is the smallest tree that has both populations: a region
// with one rendered anchor (on a declaration, so the region arm is silent) and
// a line of prose whose anchor byteOffset is the anchor under test.
func proseAnchorFixture(t *testing.T, proseAnchor string) string {
	t.Helper()
	return participationFixture(t, map[string]string{
		proseAnchorDocName: "# prose\n\n" +
			proseRegionLine + "\n\n" +
			"<!-- END GENERATED -->\n\n" +
			"The routing lives in `" + proseAnchor + "`.\n\n" +
			"Regenerate with `go run internal/standards/testdata/x-census.go . " + proseAnchorDocName + "`.\n",
		"pkg/b.go":                       "package pkg\n\n// 1\n// 2\n// the comment above the declaration\nfunc b() {}\n\nfunc c() {}\n",
		"internal/declgate/gate_test.go": strings.Replace(declarationGateBody(), "DRIFTED.md", proseAnchorDocName, 1),
	})
}

// proseRegionLine is the fixture's single rendered anchor: line 8 of pkg/b.go is
// `func c() {}`, the SECOND declaration (line 6 is `func b() {}`), so the region
// arm raises nothing and the arms below measure the PROSE population alone.
//
// The fixture file, by line: 1 `package pkg`, 3–5 the comment stack, 6 the first
// declaration, 7 the blank line between them, 8 the second declaration. The
// arms below address all three outcomes on this ONE file: 8 = declaration,
// 5 = comment, 7 = blank.
const proseRegionLine = "<!-- BEGIN GENERATED -->\n\n| `pkg/b.go:8` |"

// proseFindingsOver returns the ERROR-class findings of one fixture tree.
func proseFindingsOver(t *testing.T, root string) (errs []string, prose []string) {
	t.Helper()
	deps, err := Deps(root)
	if err != nil {
		t.Fatalf("discover the edges of the prose fixture: %v", err)
	}
	for _, dep := range deps {
		if dep.Doc == proseAnchorDocName {
			body, readErr := os.ReadFile(filepath.Join(root, proseAnchorDocName))
			if readErr != nil {
				t.Fatalf("read the fixture document: %v", readErr)
			}
			prose = ProseAnchors(string(body), Marker, VerbIndexMarker)
		}
	}
	for _, f := range findings(root, deps) {
		if strings.HasPrefix(f, ErrorFindingPrefix) {
			errs = append(errs, f)
		}
	}
	return errs, prose
}

// TestProseAnchorDeclarationInvariantRedProof is the RED PROOF for the widened
// population: a hand-written anchor that points at the COMMENT above its
// declaration must be reported, and the same anchor on the declaration must be
// silent.
//
// Both halves drive GeneratedDocAnswer's own findings channel through Deps +
// Impact — the surfaces a committing path reads — not a private predicate, so
// the arm cannot pass while the guard a delivery runs stays blind.
func TestProseAnchorDeclarationInvariantRedProof(t *testing.T) {
	// THE EXTRACTION ARM FIRST: without it every arm below could pass by reading
	// nothing, which is the 02e138f shape this file already lost a round to.
	root := proseAnchorFixture(t, "pkg/b.go:8")
	errs, prose := proseFindingsOver(t, root)
	if len(prose) != 1 || prose[0] != "pkg/b.go:8" {
		t.Fatalf("the reader extracted %v prose anchor(s) from a document carrying exactly one — "+
			"every arm below is vacuous without this", prose)
	}
	// The CONTROL: prose pointing at a DECLARATION is silent.
	if len(errs) != 0 {
		t.Fatalf("a prose anchor ON its declaration raised %v", errs)
	}

	// (a) the drift the row was opened for: the anchor names the comment line
	// above the declaration.
	errs, _ = proseFindingsOver(t, proseAnchorFixture(t, "pkg/b.go:5"))
	if len(errs) == 0 {
		t.Fatalf("a hand-written anchor on a COMMENT line raised no ERROR — the prose population is " +
			"still outside the invariant, which is the defect this arm exists for")
	}
	joined := strings.Join(errs, "\n")
	for _, must := range []string{proseAnchorDocName, "pkg/b.go:5", "comment line", "hand-written prose"} {
		if !strings.Contains(joined, must) {
			t.Errorf("the prose ERROR does not name %q:\n%s", must, joined)
		}
	}
	// The remedy sentence is POPULATION-specific: a reader told to regenerate a
	// block that never generated this line would do the wrong thing.
	if strings.Contains(joined, regionAnchorPopulationFormat) {
		t.Errorf("the prose finding is reported under the REGION's population sentence — a reader "+
			"would regenerate a block that does not carry the anchor:\n%s", joined)
	}

	// (b) the blank-line arm: a prose anchor stales DOWN as well as up.
	errs, _ = proseFindingsOver(t, proseAnchorFixture(t, "pkg/b.go:7"))
	if !strings.Contains(strings.Join(errs, "\n"), "blank line") {
		t.Errorf("a hand-written anchor on a BLANK line raised %v — the predicate must be the SAME one "+
			"the region arm uses", errs)
	}

	// (c) the ABSENT-DECLARATION arm: a symbol identity the cited file does not
	// declare is a finding of its own class, never a skip.
	errs, _ = proseFindingsOver(t, proseAnchorFixture(t, "pkg/b.go:bOld"))
	if !strings.Contains(strings.Join(errs, "\n"), "no declaration of that name") {
		t.Errorf("a prose anchor naming a symbol the file does not declare raised %v", errs)
	}

	// (d) the UNMEASURABLE arm: a SYMBOL anchor whose cited file the tree does
	// not carry is a finding, not a pass (Rule 15).
	//
	// The shape matters and is deliberate: a symbol address is answered by
	// PARSING the file, so an absent file means the question could not be put —
	// which is a finding this invariant must name. A LINE address into an absent
	// file (or past the end of one) stays what it has always been for the region
	// population too: the dangling-reference class owns it, and re-deciding it
	// here would attach this invariant's name to a defect it did not measure.
	errs, _ = proseFindingsOver(t, proseAnchorFixture(t, "pkg/gone.go:b"))
	if !strings.Contains(strings.Join(errs, "\n"), "could not be measured") {
		t.Errorf("a prose anchor citing a file the tree does not carry raised %v", errs)
	}
}

// proseAnchorFloorViolation is the NON-VACUITY floor of the prose population: a
// document that publishes at least one prose anchor must have been JUDGED, and
// this returns the sentence that says so when it was not.
//
// It answers the one failure the red proof above cannot: a reader that extracts
// ZERO prose anchors raises zero findings, and "no finding" then has two
// indistinguishable meanings — every anchor resolved (the good case) and
// nothing was read (the 02e138f defect). Same shape, and the same reason for
// being a pure predicate, as regionAnchorFloorViolation: the real tree and the
// planted fixture drive the IDENTICAL code.
//
// judged is the number of prose anchors the walk put through the predicate;
// published is the number the document carries. Empty return = the floor holds.
func proseAnchorFloorViolation(doc string, published, judged int) string {
	if published == 0 {
		return ""
	}
	if judged == 0 {
		return doc + " publishes " + fmt.Sprint(published) + " hand-written anchor(s) and the invariant " +
			"judged NONE of them — the prose population is outside the walk again"
	}
	return ""
}

// proseAnchorJudged is how many of a document's prose anchors the invariant
// actually put through the predicate: the population it walks, counted from the
// same reader the invariant uses.
func proseAnchorJudged(root string, dep Dep) int {
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dep.Doc)))
	if err != nil {
		return 0
	}
	return len(ProseAnchors(string(body), Marker, VerbIndexMarker))
}

// TestProseAnchorCountFloorRefusesAScanThatJudgedNothing plants the vacuity
// directly and gives it its control: a document with a published count and zero
// judged must fail, a document with nothing published must stay silent, and a
// document whose anchors were judged must pass.
func TestProseAnchorCountFloorRefusesAScanThatJudgedNothing(t *testing.T) {
	if got := proseAnchorFloorViolation("PROSE.md", 3, 0); got == "" {
		t.Fatalf("a document publishing 3 hand-written anchors and judging 0 passed the floor — " +
			"a scan over the wrong population would read as a clean one")
	} else if !strings.Contains(got, "judged NONE") {
		t.Errorf("the floor sentence does not say nothing was judged: %s", got)
	}
	// The control: a document that publishes no prose anchor owes nothing.
	if got := proseAnchorFloorViolation("QUIET.md", 0, 0); got != "" {
		t.Errorf("the floor fired on a document that publishes no prose anchor: %s", got)
	}
	// And the positive: judged == published is the satisfied shape.
	if got := proseAnchorFloorViolation("PROSE.md", 3, 3); got != "" {
		t.Errorf("the floor fired on a fully judged population: %s", got)
	}

	// The floor driven by a REAL tree, not only by numbers: a document whose
	// prose carries an anchor must come back judged, and the same tree with the
	// prose stripped must come back published=0 (so the floor is satisfied for
	// the right reason, not by reading zero of zero).
	root := proseAnchorFixture(t, "pkg/b.go:8")
	errs, prose := proseFindingsOver(t, root)
	if floor := proseAnchorFloorViolation(proseAnchorDocName, len(prose), len(prose)); floor != "" || len(prose) == 0 {
		t.Fatalf("the fixture publishes %d prose anchor(s) and the floor said %q (findings=%v)", len(prose), floor, errs)
	}
	stripped := participationFixture(t, map[string]string{
		proseAnchorDocName: "# prose\n\n" + proseRegionLine + "\n\n<!-- END GENERATED -->\n\n" +
			"Regenerate with `go run internal/standards/testdata/x-census.go . " + proseAnchorDocName + "`.\n",
		"pkg/b.go":                       "package pkg\n\n// 1\n// 2\n// the comment above the declaration\nfunc b() {}\n\nfunc c() {}\n",
		"internal/declgate/gate_test.go": strings.Replace(declarationGateBody(), "DRIFTED.md", proseAnchorDocName, 1),
	})
	_, noProse := proseFindingsOver(t, stripped)
	if len(noProse) != 0 {
		t.Errorf("the prose-free fixture still yielded %v — the complement is not complementing", noProse)
	}
}

// TestProseAnchorPopulationIsJudgedOnTheRealTree is the real-tree arm: the tree
// as it stands must be CLEAN over its prose population, and the run must report
// how many anchors that verdict is about (proseAnchorDocFloor).
//
// Without the count, "no finding" is a statement about nothing — the same
// failure mode regionAnchorFloorViolation exists for, one population over.
func TestProseAnchorPopulationIsJudgedOnTheRealTree(t *testing.T) {
	root := repoRoot(t)
	deps, err := Deps(root)
	if err != nil {
		t.Fatalf("discover the edges: %v", err)
	}
	docsWithProse := 0
	judged := 0
	for _, dep := range deps {
		n := proseAnchorJudged(root, dep)
		if n == 0 {
			continue
		}
		docsWithProse++
		judged += n
		if floor := proseAnchorFloorViolation(dep.Doc, n, n); floor != "" {
			t.Fatalf("%s", floor)
		}
	}
	if docsWithProse < proseAnchorDocFloor {
		t.Fatalf("the walk judged prose anchors in %d document(s), below the floor of %d — a scan over the "+
			"wrong population cannot pass as a clean corpus", docsWithProse, proseAnchorDocFloor)
	}
	t.Logf("prose anchors judged: %d across %d document(s) (floor: %d document(s))",
		judged, docsWithProse, proseAnchorDocFloor)
	var proseErrs []string
	for _, f := range findings(root, deps) {
		if strings.HasPrefix(f, ErrorFindingPrefix) && strings.Contains(f, proseAnchorPopulationFormat) {
			proseErrs = append(proseErrs, f)
		}
	}
	if len(proseErrs) != 0 {
		t.Errorf("the real tree's hand-written anchors do not resolve to declarations — re-point them by "+
			"hand, no generator re-derives this half:\n%s", strings.Join(proseErrs, "\n"))
	}
}

// TestAnchorPatternReadsSpansNotFormatPlaceholders is the regression guard for
// the shape that made this edge RED on a clean tree (measured 2026-11-06, base
// 925a078, still red at 512c294).
//
// THE DEFECT, MEASURED. COMMANDS.md:385 said
//
//	that implemented the `file.go:NNN` criterion for the two document gates.
//
// and described a mechanism that no longer exists — that is, the sentence was
// PROSE ABOUT AN ANCHOR FORMAT, not a claim about a file. AnchorPattern cannot
// tell the two apart: `file.go` matches its path segment and `NNN` matches its
// address segment, so the placeholder was published as a real anchor source and
// the edge went looking for `<tree root>/file.go`. It has never existed —
// `git log --all --diff-filter=A -- file.go` is EMPTY — so the edge reported a
// document that "publishes an anchor into a file which is not in the tree".
//
// WHICH SIDE WAS WRONG. The document: THIS page's own convention is that an
// anchor format is written with ANGLE BRACKETS (`file.go:<symbol>`,
// `file.go:<line>`) and a resolved anchor is written bare (`cli_tasks.go:528`).
// The other three mentions in COMMANDS.md (:15, :381, :389) already obey it;
// :385 was the one that did not. So the repair is IN THE DOCUMENT, and the gate
// and this pattern stay exactly as they are — nothing was exempted or relaxed.
//
// WHY A TEST AT ALL, given one apostrophe of prose was the whole fix. Because
// the property is not "COMMANDS.md is clean today" — that is the real-tree arm's
// job — it is "the reader this package publishes treats a bracketed placeholder
// as prose". A future author writing `file.go:<NNN>` in a document is the
// CORRECT spelling and must stay cheap; one who writes it bare gets this edge
// red again, and this arm is where the reason is written down.
func TestAnchorPatternReadsSpansNotFormatPlaceholders(t *testing.T) {
	// The two resolved shapes the real block emits — both must be ANCHORS.
	resolved := []string{
		"| `gopi thing` | routed by `cmd/agent/cli_tasks.go:528` |",
		"| `gopi thing` | routed by `cmd/agent/cli_notify.go:isNotifySubcommand` |",
		"a claim (`internal/gendoc/edge.go:AnchorPattern`) worth citing",
	}
	for _, line := range resolved {
		if got := AnchorPattern.FindAllStringSubmatch(line, -1); len(got) != 1 {
			t.Errorf("a RESOLVED anchor was not read as one (got %d): %s", len(got), line)
		}
	}
	// The placeholder shapes — a FORMAT, not a claim — must read as NO anchor.
	// The angle brackets are what marks them, and that is this page's own
	// convention rather than a rule invented here.
	placeholders := []string{
		"that implemented the `file.go:<NNN>` criterion for the two document gates.",
		"- **Every claim carries a `file.go:<symbol>` anchor** that must resolve",
		"`file.go:<line>` anchors are the legacy form of the same claim",
	}
	for _, line := range placeholders {
		if got := AnchorPattern.FindAllStringSubmatch(line, -1); len(got) != 0 {
			t.Errorf("a format PLACEHOLDER was published as an anchor source (%v) — the document writes its "+
				"placeholders with angle brackets, and one typed bare sends this edge to stat a file that "+
				"never existed: %s", got, line)
		}
	}
	// The third shape, kept separate because it is the one that was BARE and is
	// now bracketed: the line that was red. It is asserted here so the reading
	// does not depend on re-spelling the sentence later.
	bare := "that implemented the `file.go:NNN` criterion for the two document gates."
	got := AnchorPattern.FindAllStringSubmatch(bare, -1)
	if len(got) != 1 || got[0][1] != "file.go" {
		t.Fatalf("the BARE placeholder no longer resolves to %q — this arm's premise moved, and the "+
			"document's repair should be re-checked against the real tree", "file.go")
	}
}

// --- the SEPARATOR-ONLY target: the third outcome of the numeric arm
// (task 01a1b0c7) --------------------------------------------------------------
//
// THE HOLE THIS CLOSES, and it is the predicate's own shape rather than a
// population's. anchorFindings judges a numeric anchor's target line and, before
// this arm, recognised exactly TWO consequences: blank and comment. Every other
// line — including `}`, `{`, `},`, a line made of nothing but structural
// punctuation — fell through the switch and was reported as NOTHING, so an
// anchor that drifted onto the closing brace of the block it was written for
// read as a satisfied one. Nothing else in the tree names that shape: the anchor
// RESOLVES (the line exists), the file exists, and the region/prose arms both
// ask the same predicate. So the drift was invisible from every direction.
//
// The arms below are what make the repair a fact rather than a claim, and they
// are deliberately driven at BOTH ends because the wall this arm must not cross
// is measured, not assumed:
//
//   - the RED end: a target of `}` raises the finding, under its OWN verdict —
//     the sentence must not be the blank one or the comment one, because the
//     repair differs (re-point at the declaration AND check the address never
//     meant the brace);
//   - the GREEN end, which is the false-positive wall: `} else {`, `} else if
//     err != nil {`, `}, s.field`, a code-block continuation and a `//` line that
//     merely LOOKS like punctuation must all stay silent. `} else {` is the
//     single most common legitimate line of that family in real Go, and a
//     predicate spelled "does it start with a bracket" reddens it.
//
// THE WALL, MEASURED ON THE REAL TREE (this run, base 05f8f88; the reading is
// reproducible with a scan of the documents this package discovers):
//
//	COMMANDS.md  48 distinct numeric anchors: blank=0 comment=0 separator-only=0 other=48
//	CONFIG.md   217 distinct numeric anchors: blank=3 comment=67 separator-only=2 other=145
//
// The two CONFIG.md hits (`cmd/agent/main.go:737`, `internal/tools/memory_sync_delta.go:277`)
// point at a lone `}`, i.e. they are REAL separators — and CONFIG.md is outside
// this package's population entirely (it carries no generated marker, so Deps
// never returns it). Inside the population, this arm reddens NOTHING today; it
// is a guard against the next drift, not a defect report about this one. That is
// why the real-tree arms below must stay GREEN, and why they are the acceptance
// criterion rather than something to be tuned away.
//
// separatorAnchorDocName is a SECOND fixture document, separate from
// proseAnchorDocName, because the two arms measure different predicates and one
// fixture would let a change to one silently move the other's premise.
const separatorAnchorDocName = "SEPARATOR.md"

// separatorAnchorRegionLine renders ONE anchor inside the generated region, so
// the region population (judgesTestOwnership, the first population) is the one
// under test — the same channel a real drift in COMMANDS.md's block would take.
// The %s is the anchor VERBATIM (`pkg/b.go:5`), never a bare line number: a
// format that pasted a prefix onto the ref produced `pkg/b.go:pkg/b.go:5`, which
// AnchorPattern does not match, and the extraction arm below is what caught it.
const separatorAnchorRegionLine = "<!-- BEGIN GENERATED -->\n\n| `%s` |"

// separatorAnchorFixture is a tree whose generated region publishes `pkg/b.go:N`
// for the ONE line number the arm under test picked. The cited file's layout is
// separatorAnchorSource, quoted verbatim below with its line numbers — read that
// map, never guess: the measured trap from task 01a195c9 was a fixture whose
// "declaration" line was actually a blank line.
func separatorAnchorFixture(t *testing.T, target string) string {
	t.Helper()
	return participationFixture(t, map[string]string{
		separatorAnchorDocName: "# separators\n\n" + fmt.Sprintf(separatorAnchorRegionLine, target) + "\n\n" +
			"<!-- END GENERATED -->\n\n" +
			"Regenerate with `go run internal/standards/testdata/x-census.go . " + separatorAnchorDocName + "`.\n",
		"pkg/b.go":                       separatorAnchorSource,
		"internal/declgate/gate_test.go": strings.Replace(declarationGateBody(), "DRIFTED.md", separatorAnchorDocName, 1),
	})
}

// separatorAnchorSource is the cited file. Its line numbers, verbatim:
//
//	1  package pkg
//	2  (blank)
//	3  // b is the first declaration
//	4  func b() {
//	5  }
//	6  (blank)
//	7  func c() {
//	8  	if b() == 0 {
//	9  		return
//	10 	} else {
//	11 	}
//	12 }
//	13 (blank)
//	14 var d = []int{
//	15 	1,
//	16 },
//	17 (blank)
//	18 // }
//
// The three targets the arms below address are therefore:
//
//	5  = `}`                    -> SEPARATOR-ONLY, must RED
//	10 = `	} else {`             -> the WALL, must stay GREEN
//	16 = `},`                   -> SEPARATOR-ONLY, must RED
//	18 = `// }`                 -> a COMMENT line, which the comment arm owns
//
// Every one of them is a line a real anchor can drift onto, and the family is
// what a "must be a declaration" predicate would redden wholesale.
const separatorAnchorSource = "package pkg\n" +
	"\n" +
	"// b is the first declaration\n" +
	"func b() {\n" +
	"}\n" +
	"\n" +
	"func c() {\n" +
	"\tif b() == 0 {\n" +
	"\t\treturn\n" +
	"\t} else {\n" +
	"\t}\n" +
	"}\n" +
	"\n" +
	"var d = []int{\n" +
	"\t1,\n" +
	"},\n" +
	"\n" +
	"// }\n"

// separatorFindingsOver returns the ERROR-class findings of one fixture tree.
func separatorFindingsOver(t *testing.T, root string) []string {
	t.Helper()
	deps, err := Deps(root)
	if err != nil {
		t.Fatalf("discover the edges of the separator fixture: %v", err)
	}
	var errs []string
	for _, f := range findings(root, deps) {
		if strings.HasPrefix(f, ErrorFindingPrefix) {
			errs = append(errs, f)
		}
	}
	return errs
}

// TestSeparatorOnlyAnchorTargetIsReportedAndTheWallStaysSilent is the red proof
// for the third arm, driven at both ends on the SAME fixture file.
func TestSeparatorOnlyAnchorTargetIsReportedAndTheWallStaysSilent(t *testing.T) {
	// THE EXTRACTION ARM FIRST: without it every arm below could pass by reading
	// nothing (the 02e138f shape this file already lost a round to).
	root := separatorAnchorFixture(t, "pkg/b.go:4")
	errs := separatorFindingsOver(t, root)
	if len(errs) != 0 {
		t.Fatalf("an anchor ON its declaration raised %v — the control arm is not the control", errs)
	}
	deps, err := Deps(root)
	if err != nil {
		t.Fatalf("discover the edges: %v", err)
	}
	var extracted int
	for _, dep := range deps {
		if dep.Doc == separatorAnchorDocName {
			extracted = len(dep.RegionAnchors)
		}
	}
	if extracted != 1 {
		t.Fatalf("the reader extracted %d region anchor(s) from a region publishing exactly one — "+
			"every arm below is vacuous without this", extracted)
	}

	// (a) THE RED END — a lone closing brace. The finding must be present, carry
	// this anchor, and carry the NEW verdict: a reader told "a comment line"
	// would go looking for prose that is not there.
	errs = separatorFindingsOver(t, separatorAnchorFixture(t, "pkg/b.go:5"))
	joined := strings.Join(errs, "\n")
	if len(errs) == 0 {
		t.Fatalf("an anchor on a line of PURE STRUCTURAL PUNCTUATION (`}`) raised nothing — the " +
			"predicate falls through to green, which is the defect this arm exists for")
	}
	for _, must := range []string{separatorAnchorDocName, "pkg/b.go:5", AnchorSeparatorOnlyVerdict} {
		if !strings.Contains(joined, must) {
			t.Errorf("the separator finding does not name %q:\n%s", must, joined)
		}
	}
	// The verdict must be its OWN sentence, not one of the two that existed: a
	// duplicated string would make this arm pass while telling the reader
	// nothing new.
	for _, other := range []string{AnchorBlankLineVerdict, AnchorCommentLineVerdict} {
		if strings.Contains(joined, other) {
			t.Errorf("the separator finding carries the verdict %q — the two existing arms must judge "+
				"their own outcomes and this one its own:\n%s", other, joined)
		}
	}

	// (b) THE WALL — `},` at line 16 (the closing brace of a composite literal
	// followed by the element separator) is separator-only too, so the arm must
	// fire on it for the same reason. It is here rather than in (a) so the two
	// punctuation SHAPES are shown to be one class.
	if got := strings.Join(separatorFindingsOver(t, separatorAnchorFixture(t, "pkg/b.go:16")), "\n"); !strings.Contains(got, AnchorSeparatorOnlyVerdict) {
		t.Errorf("an anchor on `},` raised %q, want the separator verdict", got)
	}

	// (c) THE GREEN END — the measured false-positive wall. Every line below is
	// a line conventional Go spelling puts at a bracket, carries meaning, and is
	// a target real anchors legitimately land on. If any of them reddens, the
	// predicate is "starts with a bracket" rather than "is only punctuation",
	// and it would have reddened the real tree.
	for _, wall := range []struct {
		name   string
		target string
		why    string
	}{
		{"pkg/b.go:10", "pkg/b.go:10", "`} else {` — the most common legitimate line of this family"},
		{"pkg/b.go:4", "pkg/b.go:4", "`func b() {` — the declaration a correct anchor names"},
	} {
		got := separatorFindingsOver(t, separatorAnchorFixture(t, wall.target))
		if len(got) != 0 {
			t.Errorf("the wall line %s (%s) raised %v — a predicate that reddens it would redden the "+
				"real tree, which is the false-positive wall this arm is measured against", wall.target, wall.why, got)
		}
	}

	// (c2) THE ARM ORDER — `// }` (line 18) is punctuation inside a COMMENT. The
	// comment arm owns it and must keep owning it: this run adds an arm, it does
	// not re-classify one. So the line stays RED, under the COMMENT verdict and
	// never the separator one — the assertion is the verdict, not the silence,
	// because a silent `// }` would mean the existing comment arm had been
	// weakened to make room for the new one.
	if got := strings.Join(separatorFindingsOver(t, separatorAnchorFixture(t, "pkg/b.go:18")), "\n"); !strings.Contains(got, AnchorCommentLineVerdict) {
		t.Errorf("a `// }` target raised %q — the COMMENT arm owns it and must still report it", got)
	} else if strings.Contains(got, AnchorSeparatorOnlyVerdict) {
		t.Errorf("a `// }` target was re-classified as structural punctuation — the new arm must not "+
			"take a line the comment arm already judges:\n%s", got)
	}

	// (d) The PREDICATE itself, at the boundary, so the class is stated once
	// independently of any tree: the two punctuation shapes are separator-only,
	// every line carrying a byte outside the class is not.
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"}", true},
		{"{", true},
		{"},", true},
		{"})", true},
		{"} else {", false},
		{"} else if err != nil {", false},
		{"// }", false},
		{"", false},
		{"return", false},
		{"1,", false},
	} {
		if got := isSeparatorOnlyLine(strings.TrimSpace(tc.line)); got != tc.want {
			t.Errorf("isSeparatorOnlyLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
