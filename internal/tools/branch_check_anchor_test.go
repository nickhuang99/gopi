package tools

// branch_check_anchor_test.go — THE REGRESSION GUARD for the defect task
// 01a13132 measured: a COMMANDS.md generated block that disagreed with its own
// source was committed on a branch and LANDED on the integration branch, because
// nothing on the branch's own way out asked the published-anchor edge.
//
// THE MEASURED INCIDENT, so the guard has a reason a later reader can check:
// `fix(session): restore ONE definition of round` (3592f4498, 2026-10-13 00:09)
// moved cmd/agent/cli_log.go's routing predicate 602 -> 611 and committed a
// COMMANDS.md block still naming `cmd/agent/cli_log.go:607` — the document was
// regenerated against a PRE-FINAL state of that very commit. The landing that
// carried it (merge c52dad2d2, 00:57) is RED at its own revision: three
// TestCommandsDoc failures in internal/standards, measured in a checkout of that
// tree (the block disagrees with the source, the anchor resolves to a blank
// line, and the gate's own red proof finds the stale block it needs).
//
// Why the existing machinery did not stop it is measured too, and it is NOT
// "nobody regenerated" (3592f4498's own diff touched COMMANDS.md): the landing's
// union of paths DID contain cmd/agent/cli_log.go and the edge DID answer
// internal/standards — but the step that asks that question
// (internal/tools/mergeset_gendoc.go) is REPORT-ONLY by contract (Rule 12: a
// gate never touches res.OK), and the arm that REFUSED at the delivery point
// (internal/tools/task_finish_gendoc.go, "a non-zero exit REFUSES the delivery")
// was deleted 73 minutes earlier by babef3935 when the client's trial merge was
// retired.
//
// So the guard pins the two properties the fix must have: the branch's own
// pre-push leg ASKS the edge (a runnable check appears when the branch's change
// touches a published anchor source, naming the package the EDGE answered with),
// and it does NOT ask it blindly (a change touching no anchor source yields NO
// check, and an un-measurable change is an abstention with its own sentence,
// never a silent pass — Rule 15).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliindex"
	"github.com/nickhuang99/gopi/internal/gendoc"
	"github.com/nickhuang99/gopi/internal/smokegate"
)

// The fixture's own names.
const (
	bcaGoModBody = "module bcafixture\n\ngo 1.21\n"
	bcaDocName   = "COMMANDS.md"

	// bcaAnchorSrc is the ANCHOR SOURCE: the file the document publishes a
	// `<file>.go:<line>` reference into, and the one thing the branch moves.
	//
	// It lives in a package of its own WITH a passing test, on purpose — that is
	// the incident's shape. The branch changing this file makes the SMOKE leg
	// run internal/bcaanchor's own tests, which pass; the document's gate lives
	// in ANOTHER package, which the branch does not touch, so the smoke
	// selection never reaches it. The anchor arm is therefore the only way a
	// dropped regeneration is caught here, and the end-to-end arm below asserts
	// exactly that.
	bcaAnchorSrc     = bcaAnchorPkgDir + "/thing.go"
	bcaAnchorPkgDir  = "internal/bcaanchor"
	bcaAnchorTest    = "thing_test.go"
	bcaAnchorDeclare = "func isThingSubcommand"

	// bcaGatePkgDir is deliberately NOT cmd/agent and NOT internal/standards:
	// the edge derives the gate package by READING which package names the
	// document, so a passing answer cannot come from a hard-coded list.
	bcaGatePkgDir = "internal/bcagate"
	bcaGateSrc    = "bcagate.go"
	bcaGateTest   = "gate_test.go"
	// bcaGateTestName is the test the gate package runs — spelled once and
	// matched on, so the end-to-end arm proves WHICH check refused rather than
	// merely that something did.
	bcaGateTestName = "TestCommandsDocInStep"

	// bcaGeneratorPath is the program the fixture document NAMES as its own
	// regenerator. The edge requires that path to EXIST and never runs it, so the
	// fixture ships a stub — and the document's instruction is where the
	// doc <-> generator pairing comes from, never a list in the test.
	bcaGeneratorPath = "tools/bca.go"

	// bcaOtherPkgDir is touched by a THIRD branch and holds no anchor source.
	bcaOtherPkgDir = "internal/bcaother"
	bcaOtherSrc    = "bcaother.go"
)

// bcaCmdPkgDir is a package under the OTHER base directory (cmd/). It belongs to
// no arm's change — it exists so the fixture is a COMPLETE tree, both bases
// present, which is the precondition of the empty-gate-package ERROR class: that
// class claims "no package CAN read this document", and a tree missing a base
// would make the claim an unmeasured one (Dep.SkippedBases vetoes it, measured
// 2026-10-14 on this very fixture).
const (
	bcaCmdPkgDir = "cmd/bcatool"
	bcaCmdSrc    = "main.go"
)

// bcaDocBody is a document that publishes the anchor shape the edge reads: a
// generated region (the marker) AND a `<file>.go:<line>` span — the two legs
// Candidates/DepsSurvey and AnchorPattern require.
func bcaDocBody() string {
	return "# fixture doc\n\n" + cliindex.TableBegin + "\n" +
		"| verb | note |\n|---|---|\n| `gopi thing` | routed by `" + bcaAnchorSrc + ":3` |\n" +
		cliindex.TableEnd + "\n" +
		"Regenerate with `go run " + bcaGeneratorPath + " . " + bcaDocName + "`.\n"
}

// bcaAnchorSrcBody is the anchor source. Line 3 is the declaration the
// document's anchor points at, and every line inserted ABOVE it is the move the
// branch makes — the incident's own shape (3592f4498 inserted above cli_log.go's
// routing predicate and left the document naming the old line).
func bcaAnchorSrcBody(extraLines int) string {
	return "package bcaanchor\n\n" + strings.Repeat("// filler\n", extraLines) +
		bcaAnchorDeclare + "(args []string) bool {\n\treturn len(args) > 0\n}\n"
}

// bcaAnchorTestBody is the anchor source's OWN test — green before and after the
// move, which is what makes it the smoke leg's honest answer and leaves the
// document the only thing out of step.
func bcaAnchorTestBody() string {
	return "package bcaanchor\n\nimport \"testing\"\n\nfunc TestThing(t *testing.T) {}\n"
}

// bcaGateSrcBody carries the document's NAME in a quoted span, which is the
// single thing GatePackages reads to decide that this package must run when the
// document's anchors move. It is a comment on purpose: the edge's question is
// "who blocks on this document", and a package that merely lives near it does
// not.
func bcaGateSrcBody() string {
	return "package bcagate\n\n// This package blocks on \"" + bcaDocName + "\" — " +
		"a moved anchor must fail here.\nfunc Gate() bool { return true }\n"
}

// bcaGeneratorBody is the program the fixture document names as its own
// regenerator. The edge reads the document's `go run <path>.go` instruction and
// requires that path to exist (it never runs it), so the fixture ships one.
func bcaGeneratorBody() string {
	return "package main\n\nfunc main() {}\n"
}

// bcaGateTestBody is the document's OWN gate test, materialised into the fixture:
// it resolves the anchor the document publishes and asserts that the line it
// names still carries the declaration. Quoted into the fixture, so the assertion
// lives where the document does.
func bcaGateTestBody() string {
	return "package bcagate\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"regexp\"\n\t\"strings\"\n\t\"testing\"\n)\n\n// anchorSpan is the document's own \x60<file>.go:<line>\x60 reference.\nvar anchorSpan = regexp.MustCompile(\x60([A-Za-z0-9_./-]+\\.go):([0-9]+)\x60)\n\n// TestCommandsDocInStep is the document's gate: the declaration the document\n// publishes an anchor to must be the line it says. It is green while the two\n// agree, and red the moment the source moves without the document — which is\n// exactly the regeneration the change owed.\nfunc TestCommandsDocInStep(t *testing.T) {\n\troot := \".\"\n\tfor i := 0; i < 6; i++ {\n\t\tif _, err := os.Stat(filepath.Join(root, \"COMMANDS.md\")); err == nil {\n\t\t\tbreak\n\t\t}\n\t\troot = filepath.Join(root, \"..\")\n\t}\n\traw, err := os.ReadFile(filepath.Join(root, \"COMMANDS.md\"))\n\tif err != nil {\n\t\tt.Fatalf(\"read the document: %v\", err)\n\t}\n\tm := anchorSpan.FindStringSubmatch(string(raw))\n\tif m == nil {\n\t\tt.Fatalf(\"the document publishes no anchor, so there is nothing to check\")\n\t}\n\tsrc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m[1])))\n\tif err != nil {\n\t\tt.Fatalf(\"read %s: %v\", m[1], err)\n\t}\n\tlines := strings.Split(string(src), \"\\n\")\n\tn := 0\n\tfor _, r := range m[2] {\n\t\tn = n*10 + int(r-'0')\n\t}\n\tif n < 1 || n > len(lines) {\n\t\tt.Fatalf(\"the document anchors %s at line %d, which is out of range\", m[1], n)\n\t}\n\tif !strings.HasPrefix(strings.TrimSpace(lines[n-1]), \"func isThingSubcommand\") {\n\t\tt.Errorf(\"the document says %s:%d is the declaration, but that line reads %q — the document was NOT regenerated\",\n\t\t\tm[1], n, strings.TrimSpace(lines[n-1]))\n\t}\n}\n"
}

func bcaWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// bcaWriteRepo materialises the fixture repo and returns it with the refs the
// arm is asked about: the integration branch, a branch that MOVES a published
// anchor source, and a branch that touches Go files but no anchor source.
func bcaWriteRepo(t *testing.T) (repo, masterRef, branchRef, quietRef string) {
	t.Helper()
	repo = t.TempDir()
	bcaWrite(t, filepath.Join(repo, mergeBuildGateGoModFile), bcaGoModBody)
	bcaWrite(t, filepath.Join(repo, bcaDocName), bcaDocBody())
	bcaWrite(t, filepath.Join(repo, bcaGeneratorPath), bcaGeneratorBody())
	bcaWrite(t, filepath.Join(repo, bcaAnchorSrc), bcaAnchorSrcBody(0))
	bcaWrite(t, filepath.Join(repo, bcaAnchorPkgDir, bcaAnchorTest), bcaAnchorTestBody())
	bcaWrite(t, filepath.Join(repo, bcaGatePkgDir, bcaGateSrc), bcaGateSrcBody())
	bcaWrite(t, filepath.Join(repo, bcaGatePkgDir, bcaGateTest), bcaGateTestBody())
	bcaWrite(t, filepath.Join(repo, bcaOtherPkgDir, bcaOtherSrc), "package bcaother\n\nfunc F() int { return 1 }\n")
	bcaWrite(t, filepath.Join(repo, bcaCmdPkgDir, bcaCmdSrc), "package main\n\nfunc main() {}\n")
	preMergeGit(t, repo, "init", "-q", "-b", masterBranchName)
	preMergeGit(t, repo, "config", "user.email", "t@t")
	preMergeGit(t, repo, "config", "user.name", "t")
	preMergeGit(t, repo, "add", "-A")
	preMergeGit(t, repo, "commit", "-q", "-m", "base")
	masterRef = masterBranchName

	// The branch that MOVES a published anchor source above its declaration —
	// the incident's own shape (a line inserted above the predicate).
	preMergeGit(t, repo, "checkout", "-q", "-b", "feature")
	bcaWrite(t, filepath.Join(repo, bcaAnchorSrc), bcaAnchorSrcBody(2))
	preMergeGit(t, repo, "add", "-A")
	preMergeGit(t, repo, "commit", "-q", "-m", "branch: move the anchor source, drop the regeneration")
	branchRef = "feature"

	// A branch whose change touches Go files but NO published anchor source.
	preMergeGit(t, repo, "checkout", "-q", masterBranchName)
	preMergeGit(t, repo, "checkout", "-q", "-b", "quiet")
	bcaWrite(t, filepath.Join(repo, bcaOtherPkgDir, bcaOtherSrc), "package bcaother\n\nfunc F() int { return 2 }\n")
	preMergeGit(t, repo, "add", "-A")
	preMergeGit(t, repo, "commit", "-q", "-m", "branch: touch no anchor source")
	quietRef = "quiet"
	return repo, masterRef, branchRef, quietRef
}

// TestBranchCheckAnchorArmAsksTheEdge is the guard's POSITIVE arm: a branch that
// moves a published anchor source makes the check appear, and the check names
// the package the EDGE answered with — not cmd/agent, the package the SMOKE
// selection maps the same file onto.
func TestBranchCheckAnchorArmAsksTheEdge(t *testing.T) {
	repo, base, branch, _ := bcaWriteRepo(t)

	check, findings, why, ok := finishCleanupBranchAnchorCheck(repo, base, branch)
	if !ok {
		t.Fatalf("a branch that moves a published anchor source must yield a check; got abstention %q (findings %v)", why, findings)
	}
	if len(findings) != 0 {
		t.Fatalf("a followable edge must carry no findings, got %v", findings)
	}
	if check.Name != gendocUnionGateName {
		t.Errorf("the check must report under %q, got %q", gendocUnionGateName, check.Name)
	}
	if !strings.Contains(check.Cmd, "./"+bcaGatePkgDir) {
		t.Errorf("the check must run the package the EDGE named (%q), got %q", bcaGatePkgDir, check.Cmd)
	}
	if strings.Contains(check.Cmd, bcaAnchorPkgDir) {
		t.Errorf("the check must not simply re-run the changed package (%q) — that is the smoke leg's job, got %q",
			bcaAnchorPkgDir, check.Cmd)
	}
	if !strings.Contains(check.Cmd, smokegate.ScopedTarget) {
		t.Errorf("the check must be the declared scoped entry point (%q), got %q", smokegate.ScopedTarget, check.Cmd)
	}
	t.Logf("the check the branch's own leg would run: %s", check.Cmd)
}

// TestBranchCheckAnchorArmStaysSilentWhenNoAnchorMoved is the guard's NEGATIVE
// arm, and it is the one that keeps the fix from becoming noise: a branch whose
// change touches no published anchor source must produce NO check and say so,
// rather than running a document gate nobody's change obliged.
func TestBranchCheckAnchorArmStaysSilentWhenNoAnchorMoved(t *testing.T) {
	repo, base, _, quiet := bcaWriteRepo(t)

	check, findings, why, ok := finishCleanupBranchAnchorCheck(repo, base, quiet)
	if ok {
		t.Fatalf("a change that touches no published anchor source must yield no check, got %q", check.Cmd)
	}
	if len(findings) != 0 {
		t.Fatalf("nothing is broken about this change, got findings %v", findings)
	}
	if why == "" {
		t.Fatal("a silent arm must still say WHY it is silent (Rule 15) — an empty sentence reads as a pass")
	}
	t.Logf("the abstention sentence an operator reads: %s", why)
}

// TestBranchCheckAnchorArmReportsAnUnmeasurableChange is the Rule 15 arm: a base
// that does not resolve is an ABSTENTION with its own sentence, never an empty
// path list that would read as "nothing to do".
func TestBranchCheckAnchorArmReportsAnUnmeasurableChange(t *testing.T) {
	repo, _, branch, _ := bcaWriteRepo(t)

	_, _, why, ok := finishCleanupBranchAnchorCheck(repo, "no-such-ref", branch)
	if ok {
		t.Fatal("an unresolvable base must not produce a check")
	}
	if !strings.Contains(why, gendocUnionGateName) {
		t.Errorf("the abstention must name the arm that abstained, got %q", why)
	}
	t.Logf("the abstention sentence an operator reads: %s", why)
}

// TestBranchCheckAnchorArmRefusesABrokenEdge pins the one arm in this family
// that is a REFUSAL rather than an abstention: a document that states a
// generated region and publishes no anchor is an instruction a reader would
// follow into nothing, and the lane must not pass it.
func TestBranchCheckAnchorArmRefusesABrokenEdge(t *testing.T) {
	repo, base, branch, _ := bcaWriteRepo(t)
	bcaWrite(t, filepath.Join(repo, bcaDocName),
		"# fixture doc\n\n"+cliindex.TableBegin+"\n| v |\n|---|\n"+cliindex.TableEnd+"\n\n"+
			"Regenerate with `go run "+bcaGeneratorPath+" . "+bcaDocName+"`.\n")
	preMergeGit(t, repo, "checkout", "-q", branch)
	preMergeGit(t, repo, "add", "-A")
	preMergeGit(t, repo, "commit", "-q", "-m", "branch: a document that publishes no anchor")

	check, findings, why, ok := finishCleanupBranchAnchorCheck(repo, base, branch)
	if ok {
		t.Fatalf("a document with no anchor must not silently pass, got check %q", check.Cmd)
	}
	if len(findings) == 0 {
		t.Fatal("a document that carries a generated region and publishes no anchor is a FINDING, not an abstention")
	}
	if why != mergeCheckAnchorBrokenMark {
		t.Errorf("the finding must be introduced by %q, got %q", mergeCheckAnchorBrokenMark, why)
	}
	t.Logf("the finding the lane refuses on: %s", strings.Join(findings, "; "))
}

// TestBranchCheckAnchorArmRefusesADroppedRegeneration is the guard's END-TO-END
// arm, on the surface the branch actually leaves by
// (finishCleanupBranchCheckVerify, the client's own pre-push test run).
//
// The fixture is the incident: the branch moves a published anchor source
// without regenerating its document. The moved file's OWN package test passes,
// so the sibling SMOKE leg is green there, and the document's gate — which the
// branch does not touch — is the only red thing in the lane. A refusal is
// therefore reachable through the anchor arm alone; the assertions below also
// state that positively, so a later refactor that let the smoke leg cover the
// gate would fail this test loudly instead of passing for the wrong reason.
func TestBranchCheckAnchorArmRefusesADroppedRegeneration(t *testing.T) {
	f := newLocalSyncFixture(t)
	preMergeSeedIsolation(t)
	pmcPointCaches(t)

	// THE SCAFFOLD BELONGS TO THE INTEGRATION BRANCH — the document, the
	// program it names as its regenerator, the package that blocks on it, and
	// the anchor source all live on master, so the branch's own change is ONE
	// file. That is the incident's shape, and it is what makes this test about
	// the anchor arm: the moved file's own package test is green, and the
	// document's gate sits in a package the branch never touched, which the
	// branch-scoped smoke selection therefore never reaches.
	pmsStageEntryPoint(t, pmsRepoRoot(t), f.repoDir)
	pmcStageCLIPackage(t, f.repoDir)
	pmcWriteFile(t, filepath.Join(f.repoDir, mergeBuildGateGoModFile), pmcGoModBody)
	pmcWriteFile(t, filepath.Join(f.repoDir, bcaDocName), bcaDocBody())
	pmcWriteFile(t, filepath.Join(f.repoDir, bcaGeneratorPath), bcaGeneratorBody())
	pmcWriteFile(t, filepath.Join(f.repoDir, bcaGatePkgDir, bcaGateSrc), bcaGateSrcBody())
	pmcWriteFile(t, filepath.Join(f.repoDir, bcaGatePkgDir, bcaGateTest), bcaGateTestBody())
	pmcWriteFile(t, filepath.Join(f.repoDir, bcaAnchorSrc), bcaAnchorSrcBody(0))
	pmcWriteFile(t, filepath.Join(f.repoDir, bcaAnchorPkgDir, bcaAnchorTest), bcaAnchorTestBody())
	preMergeGit(t, f.repoDir, "add", "-A")
	preMergeGit(t, f.repoDir, "commit", "-q", "-m", "master: the document, its gate, and the anchor source")

	// The delivery-point sync (Rule 30a) brings the scaffold onto the branch —
	// so the branch's diff against the integration branch is the ONE move
	// below, and the gate package is not part of it.
	preMergeGit(t, f.wtDir, "merge", "-q", "--no-edit", masterBranchName)
	preMergeGit(t, f.wtDir, "add", "-A")

	// The incident's own move: a line inserted above the declaration, with the
	// document left as it was — the regeneration the branch owed and dropped.
	pmcWriteFile(t, filepath.Join(f.wtDir, bcaAnchorSrc), bcaAnchorSrcBody(2))
	preMergeGit(t, f.wtDir, "add", "-A")
	preMergeGit(t, f.wtDir, "commit", "-q", "-m", "branch: move the anchor source, drop the regeneration")

	res := finishCleanupBranchCheckVerify(context.Background(), f.repoDir, f.wtDir, f.featBranch)

	if res.Status != finishCleanupPreMergeStatusRefused {
		t.Fatalf("a branch that moved a published anchor without regenerating must REFUSE, got status=%q detail=%q",
			res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, bcaGateTestName) {
		t.Errorf("the refusal must quote the ANCHOR arm's failing test %q, got:\n%s", bcaGateTestName, res.Detail)
	}
	// WHICH check refused: the sibling smoke leg runs in the same lane, and
	// "something refused" is not the claim — the claim is that the check the
	// DOCUMENT obliged is what caught it.
	if !strings.Contains(res.Detail, "("+gendocUnionGateName+", rev ") {
		t.Errorf("the refusal must name the check that refused as %q, got:\n%s", gendocUnionGateName, res.Detail)
	}
	if !strings.Contains(res.Detail, "./"+bcaGatePkgDir) {
		t.Errorf("the refusal must show the gate package %q in the command it ran, got:\n%s", bcaGatePkgDir, res.Detail)
	}
	t.Logf("the refusal an operator reads: %s", res.Detail)
}

// --- the EMPTY-GATE-PACKAGE ERROR CLASS on the committing surface -------------

// bcaTestdataGeneratorPath is a generator under the directory the Go tool does
// not test — the shape that leaves a document with NO gate package any package
// could ever be. It is deliberately NOT bcaGeneratorPath (tools/bca.go): a
// generator under a base directory but outside testdata/ leaves the empty set a
// NOTE about the corpus, which is the class this arm does not refuse on.
const bcaTestdataGeneratorPath = "internal/bcagate/testdata/bca-census.go"

// bcaDocBodyUngated is bcaDocBody with the generator moved under testdata/ — the
// document itself is otherwise unchanged, so the ONLY difference between this
// fixture and its control is where the program the document names lives.
func bcaDocBodyUngated() string {
	return "# fixture doc\n\n" + cliindex.TableBegin + "\n" +
		"| verb | note |\n|---|---|\n| `gopi thing` | routed by `" + bcaAnchorSrc + ":3` |\n" +
		cliindex.TableEnd + "\n" +
		"Regenerate with `go run " + bcaTestdataGeneratorPath + " . " + bcaDocName + "`.\n"
}

// bcaUngatedRepo stages the fixture tree for the class: the document names a
// generator under testdata/, and the package that used to read it is GONE — so
// nothing in the module names the document at all.
func bcaUngatedRepo(t *testing.T) (repo, masterRef, branchRef string) {
	t.Helper()
	repo, masterRef, branchRef, _ = bcaWriteRepo(t)
	bcaWrite(t, filepath.Join(repo, bcaDocName), bcaDocBodyUngated())
	if err := os.RemoveAll(filepath.Join(repo, filepath.FromSlash(bcaGatePkgDir))); err != nil {
		t.Fatalf("drop the gate package: %v", err)
	}
	// AFTER the removal, and that order is the point: the generator must be IN
	// the tree (a document naming an absent program is the sibling defect, a
	// different class with a different remedy) while the package that used to
	// read the document is gone.
	bcaWrite(t, filepath.Join(repo, bcaTestdataGeneratorPath), bcaGeneratorBody())
	preMergeGit(t, repo, "checkout", "-q", branchRef)
	preMergeGit(t, repo, "add", "-A")
	preMergeGit(t, repo, "commit", "-q", "-m", "branch: a document only a generator under testdata/ names")
	return repo, masterRef, branchRef
}

// TestBranchCheckAnchorArmRefusesTheEmptyGatePackageClass is the COMMITTING
// SURFACE's half of the class: the branch's own pre-push check must REFUSE when
// the document its change moves an anchor under derives no gate package at all,
// and the refusal must name the document and the class rather than a re-parse of
// some test's output (there is no test to run — that is the point).
func TestBranchCheckAnchorArmRefusesTheEmptyGatePackageClass(t *testing.T) {
	repo, base, branch := bcaUngatedRepo(t)

	check, findings, why, ok := finishCleanupBranchAnchorCheck(repo, base, branch)
	if ok {
		t.Fatalf("a document no package can gate must not yield a check to run, got %q", check.Cmd)
	}
	if len(findings) == 0 {
		t.Fatalf("the class must be REPORTED as the findings the lane refuses on, got why=%q", why)
	}
	if !strings.Contains(why, mergeCheckAnchorErrorClassMark) {
		t.Errorf("the refusal must be introduced by its OWN mark %q (a different remedy from a broken "+
			"non-ERROR edge), got %q", mergeCheckAnchorErrorClassMark, why)
	}
	if why == mergeCheckAnchorBrokenMark {
		t.Errorf("the class was reported as the sibling broken-edge mark — the two classes are not the same fact")
	}
	joined := strings.Join(findings, "\n")
	for _, must := range []string{bcaDocName, bcaTestdataGeneratorPath} {
		if !strings.Contains(joined, must) {
			t.Errorf("the refusal does not name %q:\n%s", must, joined)
		}
	}
	if !strings.Contains(joined, gendoc.EmptyGateDerivedMark) {
		t.Errorf("the refusal does not quote the edge's own sentence (%q):\n%s", gendoc.EmptyGateDerivedMark, joined)
	}
	t.Logf("the refusal an operator reads: %s — %s", why, joined)
}

// TestBranchCheckAnchorArmDoesNotRefuseWhenAGatePackageExists is the CONTROL, and
// it is what keeps the class from being noise: the SAME document with ONE package
// that names it derives a gate package, so the arm builds the check it always
// built and refuses nothing.
func TestBranchCheckAnchorArmDoesNotRefuseWhenAGatePackageExists(t *testing.T) {
	repo, base, branch, _ := bcaWriteRepo(t)

	check, findings, why, ok := finishCleanupBranchAnchorCheck(repo, base, branch)
	if !ok {
		t.Fatalf("a document a package reads must yield a check; got abstention %q (findings %v)", why, findings)
	}
	if len(findings) != 0 {
		t.Fatalf("no edge defect exists on this tree, got %v", findings)
	}
	if strings.Contains(why, mergeCheckAnchorErrorClassMark) {
		t.Errorf("the control was refused on the ERROR class: %q", why)
	}
	if !strings.Contains(check.Cmd, "./"+bcaGatePkgDir) {
		t.Errorf("the check must run the package the EDGE named (%q), got %q", bcaGatePkgDir, check.Cmd)
	}
}

// TestBranchCheckAnchorArmRetiresTheClassOnTheCommittingSurface is the FIX's
// acceptance: GOPI_GENDOC_ERROR_CLASS_SKIP must reach the COMMITTING arm's
// fallback, not only the class arm above it.
//
// WHY THE FALLBACK IS THE CLASS'S SECOND FACE (measured 2026-10-14, task
// 01a13ae1): the fallback is reachable only once the class arm has stood down —
// a tree with a live ERROR class refuses there first. So the pre-fix shape was:
// set the skip, the class arm yields, and the arm below it returns
// mergeCheckAnchorErrorClassMark anyway. The caller still refused, and the
// documented escape had zero strength on the surface it was written for.
//
// The observation is taken on the REAL fixture tree (bcaUngatedRepo: root doc +
// generated region + generator under testdata/ + no gate package) through the
// REAL committing surface, and the assertion is the operator-visible contract: a
// refusal is classified by its MARK, so a skip that still yields the mark is not
// a retirement.
func TestBranchCheckAnchorArmRetiresTheClassOnTheCommittingSurface(t *testing.T) {
	repo, base, branch := bcaUngatedRepo(t)

	// ARM 1 — LIVE: the class is a refusal, and it carries the class mark. This
	// is the CONTROL for the retirement below: it proves the fixture really is
	// the shape the class fires on, so arm 2 cannot pass vacuously on a tree that
	// simply has no defect.
	_, findings, whyLive, okLive := finishCleanupBranchAnchorCheck(repo, base, branch)
	if okLive {
		t.Fatalf("the ungated fixture must not yield a check to run, got ok=true (why=%q)", whyLive)
	}
	if !strings.Contains(whyLive, mergeCheckAnchorErrorClassMark) {
		t.Fatalf("the LIVE class must refuse with its own mark %q, got %q", mergeCheckAnchorErrorClassMark, whyLive)
	}
	if len(findings) == 0 {
		t.Fatalf("the LIVE class must be reported as findings, got none (why=%q)", whyLive)
	}

	// ARM 2 — RETIRED: the operator has retired the class, so the committing
	// surface must not refuse on it.
	t.Setenv(gendocErrorClassSkipEnv, "1")
	_, findingsSkipped, whySkipped, okSkipped := finishCleanupBranchAnchorCheck(repo, base, branch)
	if strings.Contains(whySkipped, mergeCheckAnchorErrorClassMark) {
		t.Errorf("with %s set, the committing arm must NOT refuse on the retired class — a mark-carrying "+
			"sentence is read as a refusal by its caller (Rule 15). got why=%q findings=%v",
			gendocErrorClassSkipEnv, whySkipped, findingsSkipped)
	}
	if okSkipped {
		// A check to RUN is impossible here: this document derives no gate
		// package, so there is nothing to run. Reporting ok=true would be the
		// opposite defect (a pass manufactured out of a retired class).
		t.Errorf("a document that derives no gate package cannot yield a runnable check; got ok=true why=%q", whySkipped)
	}
	if len(findingsSkipped) != 0 {
		t.Errorf("the retired class's findings must not be handed to the caller as refusals, got %v", findingsSkipped)
	}
	if !strings.Contains(whySkipped, mergeCheckAnchorErrorClassRetiredMark) {
		t.Errorf("the retirement must be REPORTED, never silent: the sentence must carry %q, got %q",
			mergeCheckAnchorErrorClassRetiredMark, whySkipped)
	}
	if !strings.Contains(whySkipped, gendocErrorClassSkipEnv) {
		t.Errorf("the sentence must name the escape that produced it (%q), got %q", gendocErrorClassSkipEnv, whySkipped)
	}
	t.Logf("LIVE: %s\nRETIRED: %s", whyLive, whySkipped)
}

// TestBranchCheckAnchorArmSkipDoesNotWeakenTheLiveClass is the NO-WEAKENING
// control for every tree that does NOT retire the class: the same fixture, the
// same surface, WITHOUT the escape, must refuse exactly as it refused before the
// retirement existed. It is deliberately the same arm as the test above's ARM 1,
// stated separately so a change that made the class conditional on something
// else (a settings key, a tree property) turns this red rather than passing
// because the skip happened to cover it.
func TestBranchCheckAnchorArmSkipDoesNotWeakenTheLiveClass(t *testing.T) {
	repo, base, branch := bcaUngatedRepo(t)

	// An explicit empty value: "the escape is documented and NOT taken" is a
	// different state from "the escape was never exported", and the refusal must
	// hold in both (config.EnvBool reads the enabling spellings only).
	t.Setenv(gendocErrorClassSkipEnv, "")
	_, findings, why, ok := finishCleanupBranchAnchorCheck(repo, base, branch)
	if ok {
		t.Fatalf("a tree that does not retire the class must still refuse; got ok=true why=%q", why)
	}
	if !strings.Contains(why, mergeCheckAnchorErrorClassMark) {
		t.Errorf("the live refusal must still carry %q for a tree that does not retire the class, got %q",
			mergeCheckAnchorErrorClassMark, why)
	}
	if strings.Contains(why, mergeCheckAnchorErrorClassRetiredMark) {
		t.Errorf("a tree that does NOT retire the class must never read as retired, got %q", why)
	}
	if len(findings) == 0 {
		t.Errorf("the live class must still hand its findings to the caller, got none (why=%q)", why)
	}
	if !strings.Contains(strings.Join(findings, "\n"), gendoc.EmptyGateDerivedMark) {
		t.Errorf("the live refusal must still quote the edge's own sentence (%q):\n%s",
			gendoc.EmptyGateDerivedMark, strings.Join(findings, "\n"))
	}
}
