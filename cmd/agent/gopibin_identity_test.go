package main

// gopibin_identity_test.go — the guard's own tests, driven against a REAL
// repository and a REAL artifact.
//
// THE DEFECT THESE PIN (measured 2026-10-13, run 1791902369-3257849, task
// 01a13508): a red-proof RESTORE rewrote a production source with byte-identical
// content, which moved only its mtime forward, and the mtime verdict refused a
// run whose binary already contained the code. The four arms below are the four
// facts that make the replacement trustworthy:
//
//	1. an identical-bytes rewrite (mtime forward) PASSES, and the criterion that
//	   decided is `content` — the reason the defect is gone;
//	2. a production edit whose CONTENT differs FAILS and NAMES the file;
//	3. an artifact whose revision this repository does not contain FAILS naming
//	   `build identity` (it cannot be from here, whatever the files say);
//	4. an artifact that carries NO revision is decided by `mtime` and SAYS so —
//	   the last resort may not pass itself off as a content proof.
//
// The fixture is deliberately the smallest thing that can answer the questions:
// a git repository holding two tracked production sources, plus a REAL binary
// whose embedded `-ldflags` setting carries the identity operand. The binary is
// built from a throwaway hello-world module — the guard reads the OPERAND out of
// the artifact's build settings, and the linker records `-ldflags` whatever the
// operand names (a wrong path is dropped in silence, which is itself the reason
// internal/version/module_path_agreement_test.go exists).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/buildidentity"
)

const (
	// fixtureSourceDir is the production tree the fixture tracks a file in; the
	// fixture's second tree (sourceTreeInternalDir) is exercised below.
	fixtureProductionFile = "probe_source.go"
	fixtureProductionText = "package probe\n\n// probeSource is the production source under test.\nvar probeSource = 1\n"
	fixtureInternalFile   = "second_source.go"
	fixtureGoModName      = "go.mod"
	fixtureGoModBody      = "module fixture\n\ngo 1.21\n"
	fixtureMainFileName   = "main.go"
	fixtureMainFileBody   = "package main\n\nfunc main() {}\n"
	fixtureGitEmail       = "fixture@example.invalid"
	fixtureGitName        = "freshness fixture"
	fixtureUnrelatedRev   = "0000000000000000000000000000000000000000"
)

// freshnessFixtureRepo lays out a git repository holding the two production
// sources the guard compares, committed, and returns its root.
func freshnessFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{
		filepath.Join(sourceTreeCmdDir, "agent", fixtureProductionFile):    fixtureProductionText,
		filepath.Join(sourceTreeInternalDir, "agent", fixtureInternalFile): fixtureProductionText,
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	fixtureGit(t, root, "init", "-q")
	fixtureGit(t, root, "add", "--", sourceTreeCmdDir, sourceTreeInternalDir)
	fixtureGit(t, root, "-c", "user.email="+fixtureGitEmail, "-c", "user.name="+fixtureGitName,
		"commit", "-q", "-m", "fixture: production sources")
	return root
}

// fixtureGit runs one git command in the fixture repository and fails the test
// when it cannot complete.
func fixtureGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitExecutable, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), root, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fixtureRevision returns the fixture repository's HEAD revision.
func fixtureRevision(t *testing.T, root string) string {
	t.Helper()
	return fixtureGit(t, root, gitRevParseVerb, gitHeadRevision)
}

// fixtureBinary builds a minimal real binary whose embedded build settings carry
// the identity operand for rev (or none at all when rev is empty), and returns
// its path.
//
// A hello-world module is enough ON PURPOSE: what the guard reads is the
// `-ldflags` setting the build was given, and the linker records it whether or
// not the operand names a variable that exists. The build runs under the Go
// toolchain with the caller's GOFLAGS dropped, so the fixture's own flag is the
// only variable (Rule 2).
func fixtureBinary(t *testing.T, rev string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fixtureGoModName), []byte(fixtureGoModBody), 0o644); err != nil {
		t.Fatalf("write %s: %v", fixtureGoModName, err)
	}
	if err := os.WriteFile(filepath.Join(dir, fixtureMainFileName), []byte(fixtureMainFileBody), 0o644); err != nil {
		t.Fatalf("write %s: %v", fixtureMainFileName, err)
	}
	out := filepath.Join(dir, "fixturebin")
	args := []string{"build", "-o", out}
	if rev != "" {
		// The operator and the path are SPACE-separated. Measured 2026-10-31: the
		// joined spelling (`-X<path>=<rev>`) is refused by the toolchain itself —
		// `flag provided but not defined: -Xgithub.com/…` — so every arm of this
		// fixture SKIPPED for months with "needs a working Go toolchain", which read
		// as a toolchain-less host rather than as a broken fixture. The spaced form
		// is the one internal/testutil.LdflagsFlag and every build recipe emit; a
		// gate compares the two spellings
		// (TestFixtureLdflagSpellingMatchesTheBuildRecipe).
		args = append(args, "-ldflags", revisionLdflagKey+" "+buildidentity.RevisionLdflagPath+revisionOperandSplit+rev)
	}
	args = append(args, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(cleanFixtureEnv(), "GOFLAGS=")
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("the fixture needs a working Go toolchain: go build %v: %v\n%s", args, err, combined)
	}
	return out
}

// cleanFixtureEnv is the fixture build's environment: the caller's op-level
// environment with the toolchain knobs that would decide the artifact for it
// removed (a GOFLAGS-carried -ldflags is NOT recorded in the embedded settings —
// measured — so leaving it set would make the fixture's arm unreadable).
func cleanFixtureEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOFLAGS=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// TestFreshnessArrowPassesOnIdenticalBytesRewrite is THE regression test for the
// measured defect: the SAME content, rewritten with a newer mtime, must not be
// refused — and the verdict must say `content` decided it.
func TestFreshnessArrowPassesOnIdenticalBytesRewrite(t *testing.T) {
	root := freshnessFixtureRepo(t)
	rev := fixtureRevision(t, root)
	bin := fixtureBinary(t, rev)

	// The red-proof restore: byte-identical content, mtime moved forward past
	// every source in the tree.
	source := filepath.Join(root, sourceTreeCmdDir, "agent", fixtureProductionFile)
	if err := os.WriteFile(source, []byte(fixtureProductionText), 0o644); err != nil {
		t.Fatalf("rewrite %s: %v", source, err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(source, future, future); err != nil {
		t.Fatalf("chtimes %s: %v", source, err)
	}
	probe, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}
	if string(probe) != fixtureProductionText {
		t.Fatalf("the fixture did not rewrite identical bytes — the arm proves nothing")
	}

	verdict, err := decideBinFreshness(root, bin)
	if err != nil {
		t.Fatalf("decideBinFreshness: %v", err)
	}
	if !verdict.Fresh {
		t.Fatalf("a byte-identical rewrite (mtime forward) was refused — the mtime defect is back:\n%s", verdict.Refusal)
	}
	if verdict.Criterion != criterionContent {
		t.Errorf("deciding criterion = %q; want %q (an identical-bytes rewrite is a CONTENT question)", verdict.Criterion, criterionContent)
	}
}

// TestFreshnessFailsOnAProductionEditAndNamesTheFile is the other direction: a
// production source whose CONTENT differs from the revision the binary carries
// must be refused, the refusal must name that file, and it must name the
// criterion that decided.
func TestFreshnessFailsOnAProductionEditAndNamesTheFile(t *testing.T) {
	root := freshnessFixtureRepo(t)
	rev := fixtureRevision(t, root)
	bin := fixtureBinary(t, rev)

	// The ordinary shape: the source is edited, the binary is not rebuilt. HEAD
	// is unchanged, so only a CONTENT comparison can see it.
	edited := filepath.Join(root, sourceTreeInternalDir, "agent", fixtureInternalFile)
	if err := os.WriteFile(edited, []byte("package probe\n\nvar probeSource = 2\n"), 0o644); err != nil {
		t.Fatalf("edit %s: %v", edited, err)
	}

	verdict, err := decideBinFreshness(root, bin)
	if err != nil {
		t.Fatalf("decideBinFreshness: %v", err)
	}
	if verdict.Fresh {
		t.Fatalf("an edited production source was treated as present in the binary — the guard would verify code that is not in it")
	}
	if verdict.Criterion != criterionContent {
		t.Errorf("deciding criterion = %q; want %q", verdict.Criterion, criterionContent)
	}
	wantName := filepath.Join(sourceTreeInternalDir, "agent", fixtureInternalFile)
	if !strings.Contains(verdict.Refusal, wantName) {
		t.Errorf("the refusal does not name the changed file %s:\n%s", wantName, verdict.Refusal)
	}
	if !strings.Contains(verdict.Refusal, criterionContent) {
		t.Errorf("the refusal does not name the deciding criterion %q:\n%s", criterionContent, verdict.Refusal)
	}
	if !strings.Contains(verdict.Refusal, "go build") {
		t.Errorf("the refusal does not say how to fix it:\n%s", verdict.Refusal)
	}
}

// TestFreshnessRefusesAnArtifactFromAnotherRepository pins the build-identity
// criterion: a revision this repository does not contain cannot be compared by
// content at all, so the verdict is `build identity` — and it never claims the
// content matched.
func TestFreshnessRefusesAnArtifactFromAnotherRepository(t *testing.T) {
	root := freshnessFixtureRepo(t)
	bin := fixtureBinary(t, fixtureUnrelatedRev)

	verdict, err := decideBinFreshness(root, bin)
	if err != nil {
		t.Fatalf("decideBinFreshness: %v", err)
	}
	if verdict.Fresh {
		t.Fatalf("an artifact whose revision this repository does not contain was accepted:\n%+v", verdict)
	}
	if verdict.Criterion != criterionBuildIdentity {
		t.Errorf("deciding criterion = %q; want %q", verdict.Criterion, criterionBuildIdentity)
	}
	if !strings.Contains(verdict.Refusal, criterionBuildIdentity) {
		t.Errorf("the refusal does not name the deciding criterion %q:\n%s", criterionBuildIdentity, verdict.Refusal)
	}
	if !strings.Contains(verdict.Refusal, shortRev(fixtureUnrelatedRev)) {
		t.Errorf("the refusal does not name the revision that decided:\n%s", verdict.Refusal)
	}
}

// TestFreshnessFallsBackToMtimeOnlyWithoutAnIdentity pins the last resort, both
// ways, and that it ANNOUNCES itself: an artifact with no identity operand is
// judged by mtime, a newer artifact passes, an older one is refused, and the
// refusal says the verdict was mtime — so nobody reads it as a content proof.
func TestFreshnessFallsBackToMtimeOnlyWithoutAnIdentity(t *testing.T) {
	root := freshnessFixtureRepo(t)
	bin := fixtureBinary(t, "")

	if _, ok := binaryBuildRevision(bin); ok {
		t.Fatalf("the fixture binary was supposed to carry NO identity operand")
	}

	verdict, err := decideBinFreshness(root, bin)
	if err != nil {
		t.Fatalf("decideBinFreshness: %v", err)
	}
	if !verdict.Fresh {
		t.Fatalf("a binary newer than every production source was refused:\n%s", verdict.Refusal)
	}
	if verdict.Criterion != criterionMtime {
		t.Errorf("deciding criterion = %q; want %q (an artifact with no identity has no content anchor to compare against)",
			verdict.Criterion, criterionMtime)
	}

	// Age the binary past every source and ask again.
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(bin, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", bin, err)
	}
	verdict, err = decideBinFreshness(root, bin)
	if err != nil {
		t.Fatalf("decideBinFreshness (aged): %v", err)
	}
	if verdict.Fresh {
		t.Fatalf("an artifact older than every production source was accepted without an identity")
	}
	if verdict.Criterion != criterionMtime {
		t.Errorf("deciding criterion = %q; want %q", verdict.Criterion, criterionMtime)
	}
	if !strings.Contains(verdict.Refusal, criterionMtime) {
		t.Errorf("the refusal does not name the deciding criterion %q:\n%s", criterionMtime, verdict.Refusal)
	}
	if !strings.Contains(verdict.Refusal, fixtureProductionFile) {
		t.Errorf("the refusal does not name the newest production source:\n%s", verdict.Refusal)
	}
}

// TestFreshnessReportsAMissingSourceTreeAsAbsence pins Rule 15's other half: a
// question that cannot be asked is REPORTED, never answered with a pass — a
// repository with no production sources gives no way to prove anything about the
// artifact, and the guard must refuse rather than shrug.
func TestFreshnessReportsAMissingSourceTreeAsAbsence(t *testing.T) {
	root := t.TempDir()
	fixtureGit(t, root, "init", "-q")
	bin := fixtureBinary(t, "")

	verdict, err := decideBinFreshness(root, bin)
	if err != nil {
		t.Fatalf("decideBinFreshness: %v", err)
	}
	if verdict.Fresh {
		t.Fatalf("an empty repository was treated as evidence that the artifact is fresh")
	}
	if !strings.Contains(verdict.Refusal, "no "+productionSourceSuffix+" source") {
		t.Errorf("the refusal does not state the absence it found:\n%s", verdict.Refusal)
	}
	if verdict.Criterion != criterionMtime {
		t.Errorf("deciding criterion = %q; want %q", verdict.Criterion, criterionMtime)
	}
}

// TestBinaryBuildRevisionReadsTheArtifactNotTheProcess pins the reader itself:
// the identity comes out of the FILE, so it must agree with the operand the
// fixture's build was given — and be ABSENT for a build that was given none.
func TestBinaryBuildRevisionReadsTheArtifactNotTheProcess(t *testing.T) {
	rev := fixtureRevision(t, freshnessFixtureRepo(t))
	stamped := fixtureBinary(t, rev)
	got, ok := binaryBuildRevision(stamped)
	if !ok {
		t.Fatalf("no identity read out of %s, whose build carried %s", stamped, buildidentity.RevisionLdflagPath)
	}
	if got != rev {
		t.Errorf("identity read = %q; want %q", got, rev)
	}
	if _, ok := binaryBuildRevision(fixtureBinary(t, "")); ok {
		t.Errorf("an artifact built with no operand was reported as carrying an identity")
	}
	if _, ok := binaryBuildRevision(filepath.Join(t.TempDir(), "absent")); ok {
		t.Errorf("a missing file was reported as carrying an identity")
	}
}

// TestFreshnessCriteriaAreDistinct guards the vocabulary the failure texts are
// assembled from: the three criterion names must be three different words, so a
// message can never be ambiguous about which one decided.
func TestFreshnessCriteriaAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range []string{criterionBuildIdentity, criterionContent, criterionMtime} {
		if c == "" {
			t.Fatalf("a criterion name is empty")
		}
		if seen[c] {
			t.Fatalf("criterion %q is used twice — the verdict could not say which one decided", c)
		}
		seen[c] = true
	}
}
