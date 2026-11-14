package main

import (
	"fmt"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gopiversion "github.com/nickhuang99/gopi/internal/version"
)

// This file is the red proof for the ONE thing `gopi version` must not get
// wrong about a build it cannot name: WHICH fact an absent VCS stamp is
// evidence of.
//
// THE DEFECT IT CLOSES (measured 2026-09-26, Go 1.24.5). The report line for an
// unstamped build read "…carries no VCS stamp (built outside a git checkout)".
// Both halves of that sentence are checked here against real builds, and the
// scenario is not exotic — it is the build shape EVERY gopi agent runs in (the
// task runner hands each run a `git worktree add` checkout):
//
//	build dir                          .git      `go version -m`    what it printed
//	main checkout (real .git dir)       dir      vcs.revision set   the revision
//	LINKED worktree (.git is a FILE)    file     no vcs.* at all    "built outside a git checkout"
//
// The linked-worktree build stood inside a git checkout — `git rev-parse
// --is-inside-work-tree` answers `true` there, and this test asserts it — and
// the binary still said the opposite. That is the false claim: a binary
// asserting a fact about a directory it never looked at, about a build that
// stood in exactly the place it denies.
//
// WHY THE GROUND TRUTH IS BUILT RATHER THAN SYNTHESISED. The claim under test
// is a property of the ARTIFACT (`runtime/debug.ReadBuildInfo` settings inside
// the executable), so the fixture produces REAL binaries and reads REAL
// artifacts:
//
//   - the fixture is `git clone --shared` of this repository — a MAIN checkout,
//     which is what makes stamping possible at all — plus a `git worktree add`
//     of it, the shape under test. A clone is required (a worktree with no
//     checkout that stamps cannot witness the contrast) and `--shared` costs
//     ~1s and touches nothing of the origin repo (Rule 29: no foreign worktree,
//     and this repo's own checkout is only ever READ).
//   - the scratch lives in t.TempDir(), i.e. the suite's pinned scratch root,
//     off the `/` filesystem (Rule 22/32), and is removed with the test.
//   - the two binaries are built by the SAME invocation, differing only in the
//     directory they are built in, and GOFLAGS/GOVCS are dropped from the build
//     environment so the fixture's variable is the DIRECTORY and never a knob
//     somebody exported (Rule 2).

const (
	// versionProofGoBinaryName is the toolchain the fixture drives, named once
	// (Rule 1b) instead of typed at six call sites. gitBinaryName (merge_run.go)
	// already names git for this package.
	versionProofGoBinaryName = "go"
	// versionFixtureCloneFlag makes the fixture's main checkout a shared clone:
	// a real `.git` DIRECTORY (stamping works) with no object copy (fast).
	versionFixtureCloneFlag = "--shared"
	// versionFixtureMainDirName/versionFixtureWorktreeDirName are the two
	// build directories inside the fixture.
	versionFixtureMainDirName     = "main"
	versionFixtureWorktreeDirName = "wt"
	// versionFixtureBranchName is the branch the linked worktree is created on.
	// The fixture owns its clone, so the name needs no uniqueness beyond it.
	versionFixtureBranchName = "version-proof"
	// versionFixtureAgentPkg is the package under test, relative to the fixture
	// checkout's root.
	versionFixtureAgentPkg = "./cmd/agent"
	// versionFixtureBinPrefix names the artifacts inside the fixture root
	// (outside both checkouts, so no build product can dirty the tree under
	// test).
	versionFixtureBinPrefix = "gopi-"
	// versionProofModulePath is the module the fixture must be a clone OF. A
	// checkout of any other module would build a different program, so the guard
	// skips instead of proving something meaningless.
	versionProofModulePath = "module github.com/nickhuang99/gopi"
	// versionProofInsideWorkTree is git's own answer inside a git checkout.
	versionProofInsideWorkTree = "true"
	// versionProofNoStampAbsence is the clause the message must keep, wherever
	// it is worded: the report states that no stamp was embedded, rather than
	// omitting the line (Rule 15).
	versionProofNoStampAbsence = "carries no VCS stamp"
	// versionProofFalseCauseSubstring is the cause the message must NOT assert:
	// it is false for a linked-worktree build (and for a `go install`ed module
	// built on a developer's machine), which is what this file exists to pin.
	versionProofFalseCauseSubstring = "built outside a git checkout"

	// versionProofLdflagsArg / versionProofLdflagX are the flag pair the
	// documented recipe uses (`-ldflags "-X path=value"`), named once so the
	// test and the usage text cannot drift into two spellings of one recipe.
	versionProofLdflagsArg = "-ldflags"
	versionProofLdflagX    = "-X"
	// versionProofDecoyRevision is a well-formed revision that is NOT any
	// revision in the fixture: it exists so "the toolchain stamp won" is an
	// observable fact (the report names the fixture's HEAD) rather than an
	// accident of the decoy being empty.
	versionProofDecoyRevision = "0123456789abcdef0123456789abcdef01234567"

	// overlayProbeSourceDirName / overlayProbeCheckoutDirName are the synthetic
	// source repository and its shadow checkout inside the overlay regression
	// rig (both under that test's own scratch root, Rule 22).
	overlayProbeSourceDirName   = "source"
	overlayProbeCheckoutDirName = "checkout"
	// overlayProbeModulePath is the synthetic module's path: deliberately NOT
	// this repository's, so the proof is a property of the OVERLAY and never of
	// the tree the test happens to run in.
	overlayProbeModulePath = "overlayprobe"
	// overlayProbeAddedFile is the file the synthetic working tree ADDS without
	// committing it — the half of the working-tree contract the pre-fix overlay
	// dropped, and the whole point of the regression rig.
	overlayProbeAddedFile = "lib/helper.go"
	// overlayProbeAddedSymbol is the identifier that added file defines and the
	// TRACKED caller references: a shadow checkout without the file cannot
	// compile, failing with `undefined: <symbol>` — the measured symptom this
	// fix closes.
	overlayProbeAddedSymbol = "overlayProbeHelper"
	// overlayProbeIgnoredFile is a path the synthetic repository IGNORES. The
	// overlay must leave it behind (ignored residue is not source of record),
	// which is what makes `--exclude-standard` load-bearing.
	overlayProbeIgnoredFile = "overlayprobe-ignored.tmp"
	// overlayProbeGoModVersion is the synthetic module's Go directive.
	overlayProbeGoModVersion = "1.21"
)

// versionProofBuildDroppedKnobs are the environment variables dropped from the
// fixture's builds. Each can silently disable stamping in a checkout whose .git
// IS a directory, which would break the main-checkout arm of the contrast and
// make the worktree arm prove nothing about the worktree: `GOFLAGS=
// -buildvcs=false` (the explicit opt-out) and `GOVCS` (which can forbid the VCS
// command outright). The fixture states its own build environment rather than
// inheriting one.
var versionProofBuildDroppedKnobs = []string{"GOFLAGS", "GOVCS"}

// versionFixture is one scratch rig: the main checkout, the linked worktree of
// it, and where the artifacts go.
type versionFixture struct {
	repoRoot string
	root     string
	mainDir  string
	wtDir    string
}

// newVersionFixture builds the rig, or SKIPS with the reason. Skipping is the
// honest outcome when the environment cannot run the proof at all (no
// toolchain, no git, a source tree that is not a gopi checkout — e.g. a
// `git archive` extract, where there is nothing to clone and no .git to stamp
// from); the guard is never a licence to let the fixture pass vacuously.
func newVersionFixture(t *testing.T) versionFixture {
	t.Helper()
	for _, tool := range []string{versionProofGoBinaryName, gitBinaryName} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("the version stamp proof needs %q on PATH: %v", tool, err)
		}
	}
	repoRoot, err := versionProofRepoRoot()
	if err != nil {
		t.Skipf("this source tree is not a gopi git checkout, so the stamp proof has no origin to clone: %v", err)
	}
	f := versionFixture{repoRoot: repoRoot, root: t.TempDir()}
	// The main checkout and the worktree are siblings inside the rig: the
	// ordinary shape, with both directories named inside this fixture's own
	// scratch root (the suite's pinned root, off the `/` filesystem — Rule 22).
	f.mainDir = filepath.Join(f.root, versionFixtureMainDirName)
	f.wtDir = filepath.Join(f.root, versionFixtureWorktreeDirName)
	f.gitRun(t, "", "clone", versionFixtureCloneFlag, "--quiet", repoRoot, f.mainDir)
	// A clone checks out the ORIGIN's default branch, which is not the tree
	// under test — the ordinary case is a task branch whose commit is not on it,
	// and the fixture would then test master's source instead of this one. So
	// the commit under test is checked out explicitly, and the clone's worktree
	// is created from it.
	head := f.gitRun(t, repoRoot, "rev-parse", "HEAD")
	f.gitRun(t, f.mainDir, "checkout", "--quiet", "--detach", head)
	f.gitRun(t, f.mainDir, "worktree", "add", "--quiet", f.wtDir, "-b", versionFixtureBranchName)
	// The commit alone is not the tree under test: uncommitted edits to the
	// tracked sources are materialized into BOTH checkouts, so a work-in-progress
	// edit to the message is exercised (and a red fixture means the CODE, not an
	// uncommitted edit).
	for _, dir := range []string{f.mainDir, f.wtDir} {
		f.overlayWorkingTree(t, dir)
	}
	return f
}

// overlayWorkingTree writes the source repo's WORKING TREE into a fixture
// checkout — the whole contract, not only its committed half: uncommitted edits
// to tracked files, files the working tree DELETED, and every file the working
// tree added that git does not ignore. The suite's own gopi build compiles the
// working tree (gopibin_test.go builds the directory, not the commit), so a
// fixture carrying only the tracked half does not compile the tree under test
// but a MIXTURE of it and HEAD — measured 2026-10-12: a run that added a source
// file saw its shadow build report `undefined: <symbol>` for a symbol that was
// on disk in the tree under test, because the symbol's file was untracked and
// therefore in no checkout, WHILE the tracked caller that referenced it was
// carried over — an uncommitted edit to a tracked file is exactly what the diff
// half copies. The claim this comment used to make ("no non-test source of this
// package depends on one") was false in that run and is false in general: a new
// file is untracked for as long as it takes to commit it.
//
// Files git IGNORES are deliberately left behind: they are not part of the
// working tree's source of record, and a build artifact cached under an ignored
// path must not become an object two checkouts share.
func (f versionFixture) overlayWorkingTree(t *testing.T, dir string) {
	t.Helper()
	for _, name := range f.workingTreeOverlayPaths(t) {
		f.overlayOnePath(t, dir, name)
	}
}

// workingTreeOverlayPaths lists the working-tree paths the overlay carries, in
// the order they are written: the uncommitted edits to tracked files (`git diff
// --name-only -z HEAD` — a deleted file is listed too, and its absence from the
// source tree is the instruction to remove it from the fixture), then every
// untracked file git does not ignore (`git ls-files --others --exclude-standard
// -z` — the pathspec/hidden/repository .gitignore chain decides, which is why
// the fixture asks git rather than walking the filesystem). `--exclude-standard`
// is LOAD-BEARING and not a convenience: `--others` WITHOUT it lists ignored
// residue too, and copying that would put the source tree's build products into
// the fixture. Both lists are NUL-separated because a filename may carry a
// newline, and both are asked of the SOURCE tree.
func (f versionFixture) workingTreeOverlayPaths(t *testing.T) []string {
	t.Helper()
	tracked := f.overlayNames(t, "diff", "--name-only", "-z", "HEAD")
	return append(tracked, f.overlayNames(t, "ls-files", "--others", "--exclude-standard", "-z")...)
}

// overlayNames splits one NUL-separated git list, dropping the empty field the
// trailing NUL leaves. A name git lists is repo-relative by contract; one that
// is not inside the source tree would make the overlay copy from OUTSIDE the
// tree it is materializing, so it is refused by name rather than silently
// skipped (Rule 15b: a skip here would be a missing file with no trace).
func (f versionFixture) overlayNames(t *testing.T, args ...string) []string {
	t.Helper()
	out := f.gitRunRaw(t, f.repoRoot, args...)
	names := make([]string, 0, strings.Count(out, "\x00"))
	for _, name := range strings.Split(out, "\x00") {
		if name == "" {
			continue
		}
		if name == ".." || strings.HasPrefix(name, "../") {
			t.Fatalf("git %s (in %s) listed %q, which is not inside the source tree: the overlay "+
				"materializes that tree, never a path outside it", strings.Join(args, " "), f.repoRoot, name)
		}
		names = append(names, name)
	}
	return names
}

// overlayOnePath carries ONE working-tree path into the fixture checkout: a file
// that exists in the source tree is written there — the MkdirAll is what creates
// the parent directories of an added file, which no checkout ever made — and a
// path the working tree deleted is removed, because a tracked file deleted in
// the source tree must not survive in the fixture either.
func (f versionFixture) overlayOnePath(t *testing.T, dir, name string) {
	t.Helper()
	src := filepath.Join(f.repoRoot, name)
	dst := filepath.Join(dir, name)
	content, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		if rmErr := os.Remove(dst); rmErr != nil && !os.IsNotExist(rmErr) {
			t.Fatalf("fixture overlay: remove %s (deleted in the source tree): %v", dst, rmErr) // Rule 15b
		}
		return
	}
	if err != nil {
		t.Fatalf("fixture overlay: read %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("fixture overlay: create the parent directory of %s: %v", dst, err) // Rule 15b
	}
	if err := os.WriteFile(dst, content, 0o644); err != nil {
		t.Fatalf("fixture overlay: write %s: %v", dst, err)
	}
}

// TestOverlayWorkingTreeCarriesFilesTheWorkingTreeAdded is the minimal
// regression guard for the overlay's contract: it drives the REAL
// overlayWorkingTree against a synthetic repository, so it pins the CONTRACT
// rather than the fixture's current wiring.
//
// WHY IT EXISTS. The overlay used to carry only `git diff --name-only HEAD` —
// the uncommitted half — while the fixture's builds compile a WORKING TREE (a
// new file is untracked for as long as it takes to commit it). A run that added
// a source file therefore handed its shadow checkout a MIXTURE of the tree
// under test and HEAD: the tracked caller came over, the untracked file it
// named was in no checkout, and the fixture's own build died with `undefined:
// <symbol>` for a symbol that was on disk in the tree under test (measured
// 2026-10-12). Red before the fix, green after, and it needs no write access to
// this repository: the rig is two throwaway git repositories under the suite's
// own scratch root.
//
// The synthetic shape is the failing one, and nothing else: a tracked caller
// references a symbol that ONLY the untracked file defines, and the same
// working tree carries one ignored file the overlay must NOT copy. The build at
// the end is the symptom's direct form — `undefined: <symbol>` is what the
// pre-fix overlay produced.
func TestOverlayWorkingTreeCarriesFilesTheWorkingTreeAdded(t *testing.T) {
	for _, tool := range []string{versionProofGoBinaryName, gitBinaryName} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("the overlay proof needs %q on PATH: %v", tool, err)
		}
	}
	f := versionFixture{root: t.TempDir()}
	f.repoRoot = filepath.Join(f.root, overlayProbeSourceDirName)
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(f.repoRoot, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("overlay probe: create the parent directory of %s: %v", path, err) // Rule 15b
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("overlay probe: write %s: %v", path, err) // Rule 15b
		}
	}
	write("go.mod", "module "+overlayProbeModulePath+"\n\ngo "+overlayProbeGoModVersion+"\n")
	write(".gitignore", overlayProbeIgnoredFile+"\n")
	// The tracked caller depends on the added file's symbol: that dependency is
	// what turns a missing file into a build failure rather than a silent pass.
	write("lib/lib.go", "package lib\n\nfunc Used() string { return "+overlayProbeAddedSymbol+"() }\n")
	write("cmd/main.go", "package main\n\nimport \""+overlayProbeModulePath+"/lib\"\n\nfunc main() { _ = lib.Used() }\n")
	f.gitRun(t, f.repoRoot, "init", "--quiet")
	f.gitRun(t, f.repoRoot, "add", "--", "go.mod", ".gitignore", "lib/lib.go", "cmd/main.go")
	f.gitRun(t, f.repoRoot, "commit", "--quiet", "-m", "overlay probe base")
	// Now the working tree's two halves that are NOT in the commit: a file it
	// ADDED (the half the pre-fix overlay dropped) and a file it IGNORES (which
	// the overlay must keep dropping).
	write(overlayProbeAddedFile, "package lib\n\nfunc "+overlayProbeAddedSymbol+"() string { return \"overlay probe\" }\n")
	write(overlayProbeIgnoredFile, "ignored residue that is not source of record\n")

	// The premise, asked of git rather than assumed: the added file is
	// untracked-not-ignored, so the overlay's own listing must carry it.
	if got := f.overlayNames(t, "ls-files", "--others", "--exclude-standard", "-z"); !slicesContains(got, overlayProbeAddedFile) {
		t.Fatalf("the probe's premise does not hold: git %s lists %v, which does not contain %s",
			"ls-files --others --exclude-standard", got, overlayProbeAddedFile)
	}

	checkout := filepath.Join(f.root, overlayProbeCheckoutDirName)
	f.gitRun(t, "", "clone", versionFixtureCloneFlag, "--quiet", f.repoRoot, checkout)
	f.overlayWorkingTree(t, checkout)

	// (1) The added file crossed into the shadow checkout, byte for byte.
	want, err := os.ReadFile(filepath.Join(f.repoRoot, overlayProbeAddedFile))
	if err != nil {
		t.Fatalf("overlay probe: read the source %s: %v", overlayProbeAddedFile, err) // Rule 15b
	}
	got, err := os.ReadFile(filepath.Join(checkout, overlayProbeAddedFile))
	if err != nil {
		t.Fatalf("the overlay did not carry %s into the shadow checkout (%v): the fixture's builds compile "+
			"the WORKING TREE, so a checkout without the files it added compiles a mixture of the tree under "+
			"test and HEAD, and any tracked caller of a symbol they define fails with `undefined: %s`",
			overlayProbeAddedFile, err, overlayProbeAddedSymbol)
	}
	if string(got) != string(want) {
		t.Fatalf("the overlay carried %s into the shadow checkout but changed it:\ngot  %q\nwant %q",
			overlayProbeAddedFile, got, want)
	}
	// (2) The ignored half is still left behind: it is not source of record.
	if _, err := os.Stat(filepath.Join(checkout, overlayProbeIgnoredFile)); !os.IsNotExist(err) {
		t.Fatalf("the overlay carried the IGNORED file %s into the shadow checkout (stat error %v): ignored "+
			"residue is not part of the working tree's source of record", overlayProbeIgnoredFile, err)
	}
	// (3) The symptom's direct form: the shadow checkout compiles. This is the
	// assertion that would have caught the original failure, and the one a build
	// cache cannot satisfy on its own.
	build := exec.Command(versionProofGoBinaryName, "build", "./...")
	build.Dir = checkout
	build.Env = versionProofBuildEnv(t)
	if combined, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... in the shadow checkout: %v\n%s\nthe tracked caller references %s, which only "+
			"%s defines: an overlay that drops the files the working tree ADDED hands the build a mixture of the "+
			"tree under test and HEAD", err, combined, overlayProbeAddedSymbol, overlayProbeAddedFile)
	}
}

// versionProofRepoRoot resolves the gopi checkout this test binary was built
// from, and refuses anything that is not one: the clone must come from the
// module under test, and the root must be the repository the package sits in
// (a parent repository would clone a different project entirely).
func versionProofRepoRoot() (string, error) {
	out, err := exec.Command(gitBinaryName, "rev-parse", "--show-toplevel").Output() // no-wrap: this is a fixture probe, not production error handling
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod")) // no-wrap: same, the caller skips on any read failure
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(string(gomod), versionProofModulePath) {
		return "", os.ErrInvalid
	}
	return root, nil
}

// gitRun runs one git command of the fixture and returns its TRIMMED stdout
// (the value verbs like `rev-parse` are compared against). It fails the test on
// error. dir == "" means "this process's directory" (clone needs no -C).
// Identity and signing are supplied per call because the fixture's clone
// inherits no user config on purpose.
func (f versionFixture) gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(f.gitRunRaw(t, dir, args...))
}

// gitRunRaw is gitRun without the trim, for callers whose output is a
// NUL-separated list (`diff --name-only -z`), where trimming would cut a
// filename.
func (f versionFixture) gitRunRaw(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := f.gitOutput(dir, args...)
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return out
}

// gitOutput runs git and returns its raw stdout, with git's own stderr folded
// into the error (a fixture that fails must say WHY git refused).
func (f versionFixture) gitOutput(dir string, args ...string) (string, error) {
	full := append([]string{"-c", "commit.gpgsign=false",
		"-c", "user.email=version-proof@example.invalid",
		"-c", "user.name=version proof"}, args...)
	cmd := exec.Command(gitBinaryName, full...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// build compiles the package under test in dir, writing the artifact to out
// (outside both checkouts), with the fixture's own build environment.
func (f versionFixture) build(t *testing.T, dir, out string, extra ...string) {
	t.Helper()
	args := append([]string{"build"}, extra...)
	args = append(args, "-o", out, versionFixtureAgentPkg)
	cmd := exec.Command(versionProofGoBinaryName, args...)
	cmd.Dir = dir
	cmd.Env = versionProofBuildEnv(t)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, combined)
	}
}

// versionProofBuildEnv is os.Environ with the stamping knobs dropped.
func versionProofBuildEnv(t *testing.T) []string {
	t.Helper()
	kept := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if slicesContains(versionProofBuildDroppedKnobs, name) {
			continue
		}
		kept = append(kept, kv)
	}
	return kept
}

// slicesContains is a tiny local membership test — the fixture must not pull a
// dependency in for one loop.
func slicesContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// vcsRevisionStamps counts the `vcs.revision` settings inside a real artifact —
// the toolchain's ground truth about the build, one layer below anything gopi
// prints. 0 means "this artifact carries no vcs stamp"; anything else means it
// does, and the number itself is pinned by the caller.
//
// The line shape `go version -m` prints is
// "	build	vcs.revision=<hex>", so the match is per FIELD and anchored on the
// key followed by "=": a substring test would also fire on the `go version -m`
// line that merely names the binary, and a prefix test on the whole line never
// fires at all (the tab-indented fields come first).
func vcsRevisionStamps(t *testing.T, bin string) int {
	t.Helper()
	out, err := exec.Command(versionProofGoBinaryName, "version", "-m", bin).CombinedOutput()
	if err != nil {
		t.Fatalf("go version -m %s: %v\n%s", bin, err, out)
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, gopiversion.BuildInfoKeyRevision+"=") {
				count++
			}
		}
	}
	return count
}

// reportOf runs the fixture binary exactly as an operator would.
func reportOf(t *testing.T, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	// A sandboxed home: `gopi version` short-circuits before config, and the
	// pin makes that a claim this fixture cannot quietly depend on.
	cmd.Env = append(os.Environ(), "GOPI_HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", bin, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// revisionReportLine returns the report line that carries the revision fact —
// located by the producer's OWN key (gopiversion.ReportRevisionKey, Rule 13),
// never by a substring this fixture guessed at. The test is about that line, so
// a report that lost it must fail the fixture rather than read as an absent
// claim.
func revisionReportLine(t *testing.T, report string) string {
	t.Helper()
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, gopiversion.ReportRevisionKey) {
			return strings.TrimSpace(strings.TrimPrefix(line, gopiversion.ReportRevisionKey))
		}
	}
	t.Fatalf("no %q line in the report:\n%s", gopiversion.ReportRevisionKey, report)
	return ""
}

// TestVersionReportDoesNotBlameACheckoutTheBuildStoodIn is THE red proof.
//
// Against the pre-fix constant this test fails on the worktree arm (its line
// read "…(built outside a git checkout)" while the build stood inside one);
// after the fix it passes, and the same assertions still catch a regression in
// EITHER direction — a line that claims a checkout, or a main checkout whose
// build stopped being stamped (the fixture would then be unable to witness the
// contrast, and it says so instead of passing).
func TestVersionReportDoesNotBlameACheckoutTheBuildStoodIn(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	f := newVersionFixture(t)
	mainBin := filepath.Join(f.root, versionFixtureBinPrefix+versionFixtureMainDirName)
	wtBin := filepath.Join(f.root, versionFixtureBinPrefix+versionFixtureWorktreeDirName)
	f.build(t, f.mainDir, mainBin)
	f.build(t, f.wtDir, wtBin)

	// The worktree is a git checkout, by git's own answer — the fact the old
	// message denied. Both checkouts stand on the same commit, so the only
	// variable left between the two builds is the SHAPE of the checkout.
	if got := f.gitRun(t, f.wtDir, "rev-parse", "--is-inside-work-tree"); got != versionProofInsideWorkTree {
		t.Fatalf("fixture worktree %s: git rev-parse --is-inside-work-tree = %q, want %q — the fixture is not "+
			"the shape under test", f.wtDir, got, versionProofInsideWorkTree)
	}
	mainHead := f.gitRun(t, f.mainDir, "rev-parse", "HEAD")
	if wtHead := f.gitRun(t, f.wtDir, "rev-parse", "HEAD"); wtHead != mainHead {
		t.Fatalf("fixture checkouts disagree: main at %s, worktree at %s", mainHead, wtHead)
	}

	// Ground truth, one layer below the report: the main checkout's build IS
	// stamped and the worktree's is NOT. A host or toolchain where either half
	// changed would make this fixture vacuous, so it is asserted, not assumed.
	if n := vcsRevisionStamps(t, mainBin); n != 1 {
		t.Fatalf("the main-checkout build carries %d %s setting(s), want exactly 1 — the fixture cannot witness "+
			"the contrast without a stamped main build", n, gopiversion.BuildInfoKeyRevision)
	}
	if n := vcsRevisionStamps(t, wtBin); n != 0 {
		t.Fatalf("the linked-worktree build carries %d %s setting(s), want 0 — the worktree shape is what this "+
			"proof is about", n, gopiversion.BuildInfoKeyRevision)
	}

	// The stamped arm: the report names the revision, and the post-merge
	// rebuild verifier's reader (the `revision:` KEY's sole reader) recovers it.
	mainReport := reportOf(t, mainBin, CliVerbVersion)
	if rev, ok := gopiversion.RevisionOfDisplay(mainReport); !ok || rev != mainHead {
		t.Errorf("build in the main checkout reports revision %q (ok=%v), want the checked-out %s:\n%s",
			rev, ok, mainHead, mainReport)
	}

	// The worktree arm — THE ASSERTION THE DEFECT FAILED: the report states the
	// absence truthfully, and it does not name a checkout.
	wtReport := reportOf(t, wtBin, CliVerbVersion)
	wtLine := revisionReportLine(t, wtReport)
	if wtLine != versionNoVCSStamp {
		t.Errorf("a binary built in a linked worktree reports\n  %q\nwant the message this package defines\n  %q",
			wtLine, versionNoVCSStamp)
	}
	if strings.Contains(wtReport, versionProofFalseCauseSubstring) {
		t.Errorf("a binary built INSIDE a git checkout (%s, worktree of %s) claims %q:\n%s",
			f.wtDir, f.mainDir, versionProofFalseCauseSubstring, wtReport)
	}
	// The same absence seen by the reader the merge path uses: an unstamped
	// answer must yield NO revision (never the message text, never a guess).
	if rev, ok := gopiversion.RevisionOfDisplay(wtReport); ok {
		t.Errorf("an unstamped report yielded revision %q; the absence must stay an absence (the rebuild "+
			"verifier refuses an unknown artifact by name, it never accepts a fabricated provenance)", rev)
	}
}

// TestVersionReportStampingIsSkippedInAWorktreeEvenWhenBuildvcsIsTrue pins the
// "the toolchain keeps the reason" half of the message: `-buildvcs=true` — the
// spelling that makes cmd/go FAIL LOUDLY when it cannot detect the VCS — does
// not force a stamp for a linked worktree either. It produces the same
// unstamped artifact, silently, which is why the message may state the absence
// but must not name a cause it cannot know.
func TestVersionReportStampingIsSkippedInAWorktreeEvenWhenBuildvcsIsTrue(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	f := newVersionFixture(t)
	bin := filepath.Join(f.root, versionFixtureBinPrefix+"worktree-buildvcs-true")
	f.build(t, f.wtDir, bin, "-buildvcs=true")
	if n := vcsRevisionStamps(t, bin); n != 0 {
		t.Fatalf("a worktree build with -buildvcs=true carries %d %s setting(s), want 0 — if the toolchain "+
			"started stamping worktrees, the message and this proof must be revisited", n,
			gopiversion.BuildInfoKeyRevision)
	}
	if line := revisionReportLine(t, reportOf(t, bin, CliVerbVersion)); line != versionNoVCSStamp {
		t.Errorf("worktree build with -buildvcs=true reports\n  %q\nwant\n  %q", line, versionNoVCSStamp)
	}
}

// TestVersionNoVCSStampMessageNamesNoCheckout is the wording half, checked on
// the CONSTANT rather than through a build: it is what makes the message
// honest for every build shape at once (a tarball extract, a `go install`ed
// module, a worktree, and the `-buildvcs=false` opt-out all reach the same
// line), and it is cheap enough to fail fast when someone re-words it.
func TestVersionNoVCSStampMessageNamesNoCheckout(t *testing.T) {
	if strings.Contains(versionNoVCSStamp, versionProofFalseCauseSubstring) {
		t.Errorf("versionNoVCSStamp claims %q; a linked-worktree build stands inside a git checkout, so that "+
			"cause is false on the ordinary agent path: %q", versionProofFalseCauseSubstring, versionNoVCSStamp)
	}
	for _, part := range []string{versionStampSkippedCause, versionProofNoStampAbsence} {
		if !strings.Contains(versionNoVCSStamp, part) {
			t.Errorf("versionNoVCSStamp %q does not carry %q — the report must state the absence AND the cause "+
				"it can support", versionNoVCSStamp, part)
		}
	}
	// The second cause, and the one the pre-fix wording omitted entirely: the
	// linker drops an `-X` operand that names no variable, silently (exit 0,
	// empty stderr) — so an artifact can reach this line because the RECIPE's
	// own stamp was dropped, not because the toolchain declined to stamp. A
	// reader who did pass -ldflags -X has to be pointed at the operand, and the
	// operand is named from the ONE constant (never retyped here — Rule 13/14,
	// which is also what keeps this assertion from drifting).
	if !strings.Contains(versionNoVCSStamp, versionStampSkippedDroppedOperandCause) {
		t.Errorf("versionNoVCSStamp %q does not carry %q — the dropped-operand cause is exactly the failure the "+
			"documented recipe hits when the operand path goes stale, and a reader following only the "+
			"toolchain causes is pointed at the tree instead of at the flag",
			versionNoVCSStamp, versionStampSkippedDroppedOperandCause)
	}
	if !strings.Contains(versionNoVCSStamp, gopiversion.RevisionLdflagPath) {
		t.Errorf("versionNoVCSStamp %q does not name the operand path %q, so a reader with a stale operand has "+
			"nothing to compare against", versionNoVCSStamp, gopiversion.RevisionLdflagPath)
	}
	// Rule 31's help/behaviour agreement, as a check: `-h` and the report make
	// the same claim about an unstamped build, from the ONE constant that
	// carries it.
	if !strings.Contains(versionUsage, versionStampSkippedCause) {
		t.Errorf("versionUsage does not carry versionStampSkippedCause (%q), so the help text and the report "+
			"would explain the same absence differently:\n%s", versionStampSkippedCause, versionUsage)
	}
	if !strings.Contains(reportOf(t, mustHelperBin(t), CliVerbVersion, HelpFlag), versionStampSkippedCause) {
		t.Errorf("`gopi version %s` does not print the shared cause %q", HelpFlag, versionStampSkippedCause)
	}
}

// TestVersionRevisionRecipeNamesTheBuildOnAnUnstampableGround is the Rule 31
// half of the identity story, and the regression guard for the ONE ground every
// task runs in.
//
// THE PROBLEM IT PINS. A linked worktree is the shape `git worktree add`
// produces, and it is the shape of every gopi task ground (Rule 6a). The
// toolchain writes no `vcs.revision` there (cmd/go recognizes a repository root
// by a `.git` DIRECTORY; a worktree's `.git` is a FILE) — so the binary a run
// builds on its own ground carries NO identity, and a reader checking
// `go version -m | grep vcs` sees an empty answer that is indistinguishable
// from having checked nothing. This test proves the documented recipe closes
// that hole AND that closing it did not smuggle an attestation in:
//
//  1. the recipe (`-X <RevisionLdflagPath>=$(git rev-parse HEAD)`) makes the
//     report name the worktree's own HEAD — the identity Rule 31 asks for;
//  2. the toolchain STILL wrote 0 vcs.* settings, so the value in (1) is
//     DECLARED, not attested — and the report says so on its own line;
//  3. where the toolchain CAN stamp (the main checkout), the stamp WINS: the
//     reported revision is the attested one, and no provenance line appears;
//  4. a worktree build with NO recipe still reports an honest ABSENCE — the
//     fallback never invents a value out of nothing.
//
// (3) and (4) are what make this a proof rather than a demonstration: a
// fallback that always answered would pass (1) and fail (2)/(3), and a fallback
// that answered from the environment would pass (1) on a dirty host and fail
// (4) here.
func TestVersionRevisionRecipeNamesTheBuildOnAnUnstampableGround(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	f := newVersionFixture(t)
	wtHead := f.gitRun(t, f.wtDir, "rev-parse", "HEAD")
	mainHead := f.gitRun(t, f.mainDir, "rev-parse", "HEAD")

	// (1)+(2) the declared arm, built on the ground shape under test.
	declared := filepath.Join(f.root, versionFixtureBinPrefix+"worktree-declared")
	f.build(t, f.wtDir, declared, versionProofLdflagsArg,
		versionProofLdflagX+" "+gopiversion.RevisionLdflagPath+"="+wtHead)

	if n := vcsRevisionStamps(t, declared); n != 0 {
		t.Fatalf("the worktree build carries %d %s setting(s), want 0: the recipe REPLACES the "+
			"toolchain's stamp on a shape it cannot stamp — it must not make one appear (a test "+
			"reading only the report could not tell those two apart)", n, gopiversion.BuildInfoKeyRevision)
	}
	declaredReport := reportOf(t, declared, CliVerbVersion)
	if rev, ok := gopiversion.RevisionOfDisplay(declaredReport); !ok || rev != wtHead {
		t.Errorf("the declared worktree build reports revision %q (ok=%v), want the worktree's own "+
			"HEAD %s:\n%s", rev, ok, wtHead, declaredReport)
	}
	if !strings.Contains(declaredReport, versionRevisionSelfDeclared) {
		t.Errorf("the declared report does not carry %q, so a reader cannot tell a DECLARED revision "+
			"from an ATTESTED one — that is the silent-identity-loss defect this arm exists for:\n%s",
			versionRevisionSelfDeclared, declaredReport)
	}

	// (3) attestation beats declaration: the same flag on a ground the toolchain
	// CAN stamp must not shadow the real stamp.
	shadow := filepath.Join(f.root, versionFixtureBinPrefix+"main-declared")
	f.build(t, f.mainDir, shadow, versionProofLdflagsArg,
		versionProofLdflagX+" "+gopiversion.RevisionLdflagPath+"="+versionProofDecoyRevision)
	if n := vcsRevisionStamps(t, shadow); n != 1 {
		t.Fatalf("the main-checkout build carries %d %s setting(s), want exactly 1 — without a real "+
			"stamp this arm cannot witness precedence", n, gopiversion.BuildInfoKeyRevision)
	}
	shadowReport := reportOf(t, shadow, CliVerbVersion)
	rev, ok := gopiversion.RevisionOfDisplay(shadowReport)
	if !ok || rev != mainHead {
		t.Errorf("a toolchain-stamped build reports revision %q (ok=%v), want the ATTESTED %s — the "+
			"declared value (%s) must never shadow a stamp:\n%s",
			rev, ok, mainHead, versionProofDecoyRevision, shadowReport)
	}
	if strings.Contains(shadowReport, versionRevisionSourceKey) {
		t.Errorf("a toolchain-stamped build printed the %q line; that line belongs to the declared arm "+
			"only, or its presence stops being the signal:\n%s", versionRevisionSourceKey, shadowReport)
	}

	// (4) no recipe, no invention.
	plain := filepath.Join(f.root, versionFixtureBinPrefix+"worktree-undeclared")
	f.build(t, f.wtDir, plain)
	if rev, ok := gopiversion.RevisionOfDisplay(reportOf(t, plain, CliVerbVersion)); ok {
		t.Errorf("an undeclared worktree build yielded revision %q; with no recipe the report must "+
			"state the absence rather than recover a value from anywhere else", rev)
	}
}

// mustHelperBin is the suite's ONE built binary, for the help-flag agreement
// check above (no second build: gopibin_test.go owns the build cache).
func mustHelperBin(t *testing.T) string {
	t.Helper()
	return gopiTestBinPath(t)
}
