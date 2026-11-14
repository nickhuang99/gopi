package tools

// THE GATE-ROSTER REGRESSION (epic's central acceptance, internal/smoke/gatepackages.go).
//
// The defect: `finishCleanupBranchCheckTest` scoped its command to
// `smoke.TouchedPackages` VERBATIM, so a branch whose diff touches no gate
// package ran NO gate — while the gate packages are the expensive ones that no
// foreground `go test` window reaches, and this repo carries no CI that would
// run them either. Two real reds lived on master across a whole merge wave that
// way.
//
// These arms drive the REAL surface: a real git repository with a real Makefile
// declaring the scoped target and real gate package DIRECTORIES, a real branch,
// and the real diff the selector reads. The assertion is on the rendered command
// that the lane would actually run, so a change that stops unioning the roster
// fails here rather than in review.
//
// A unit test on the pure function alone is NOT enough (Rule 31): it would pass
// while the call site still discarded the answer.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/smoke"
	"github.com/nickhuang99/gopi/internal/smokegate"
	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// fcbGateFixtureMakefile declares the ONE thing the lane will agree to run
	// in an arbitrary tree (Rule 12: a repo's build contract is DECLARED). The
	// recipe is inert — these arms measure the command the lane renders, not its
	// execution, and running the suite from inside the suite is not a thing.
	fcbGateFixtureMakefile = "test-budget:\n\t@echo fixture\n"
	// fcbGateFixtureGoMod is what makes the tree a Go module at all.
	fcbGateFixtureGoMod = "module fcbgatefixture\n\ngo 1.21\n"
	// fcbGateFixturePkgBody is the ONE non-gate package the branch will touch,
	// so the selection is non-empty and the interesting property (the roster is
	// ADDED to a real diff) is what gets measured.
	fcbGateFixturePkgBody = "package p\n"
	// fcbGateFixturePkgDir is that package's directory.
	fcbGateFixturePkgDir = "internal/notagate"
	// fcbGateFixturePkgFile is the file inside it.
	fcbGateFixturePkgFile = "p.go"
	// fcbGateFixtureBaseBranch is the base the selection diffs against.
	fcbGateFixtureBaseBranch = "base"
	// fcbGateFixtureWorkBranch is the branch carrying the non-gate change.
	fcbGateFixtureWorkBranch = "work"
)

// fcbGateFixture builds the repository described above on disk and returns its
// directory and the two refs the selector needs. Every gate package from the
// ROSTER is materialised (not a hardcoded pair), so the fixture follows the
// roster rather than a second spelling of it.
//
// The branch is CUT and the base ref recorded, so `TouchedPackages` performs the
// real `git diff base...branch` read rather than being fed a list.
func fcbGateFixture(t *testing.T) (dir, base, branch string) {
	t.Helper()
	dir = t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		// testutil.GitEnv strips GIT_DIR / GIT_WORK_TREE / the object and index
		// routing names from the inherited environment (Rule 13/15's measured
		// 2026-10-13 data-loss class: cmd.Dir does NOT override GIT_DIR), then
		// appends the fixture's own pins.
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=fcb", "GIT_AUTHOR_EMAIL=fcb@example.com",
			"GIT_COMMITTER_NAME=fcb", "GIT_COMMITTER_EMAIL=fcb@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "--initial-branch", fcbGateFixtureBaseBranch)
	write("Makefile", fcbGateFixtureMakefile)
	write("go.mod", fcbGateFixtureGoMod)
	// The non-gate package the branch will CHANGE exists at the base too (a new
	// file would be an untracked path the git-add below refuses).
	write(fcbGateFixturePkgDir+"/"+fcbGateFixturePkgFile, "package p\n\n// base\n")
	// The gate packages exist as DIRECTORIES (that is the presence question the
	// lane asks); a one-line file each keeps git from refusing the empty tree.
	for _, rel := range smoke.GatePackageDirs {
		write(rel+"/gate.go", "package gate\n")
	}
	git("add", "Makefile", "go.mod", filepath.FromSlash(fcbGateFixturePkgDir)+"/"+fcbGateFixturePkgFile)
	// The gate package files are added one by one (Rule 8: never a blanket add).
	for _, rel := range smoke.GatePackageDirs {
		git("add", filepath.FromSlash(rel)+"/gate.go")
	}
	git("commit", "-q", "-m", "base: module + Makefile + gate packages")
	base = git("rev-parse", "HEAD")
	git("checkout", "-q", "-b", fcbGateFixtureWorkBranch)
	// The ONE change the branch makes: a NON-gate package. This is the exact
	// shape that used to select nothing worth running.
	write(fcbGateFixturePkgDir+"/"+fcbGateFixturePkgFile, fcbGateFixturePkgBody)
	git("add", filepath.FromSlash(fcbGateFixturePkgDir)+"/"+fcbGateFixturePkgFile)
	git("commit", "-q", "-m", "work: a non-gate package only")
	branch = git("rev-parse", "HEAD")
	return dir, base, branch
}

// TestFinishCleanupBranchCheckTestCarriesTheGateRoster is the epic's acceptance
// A: a branch whose diff touches NO gate package still gets a command that
// addresses every gate package in the roster.
func TestFinishCleanupBranchCheckTestCarriesTheGateRoster(t *testing.T) {
	dir, base, branch := fcbGateFixture(t)
	check, note, ok := finishCleanupBranchCheckTest(dir, base, branch)
	if !ok {
		t.Fatalf("the lane abstained on a fixture that declares %s and carries the roster: %q",
			smokegate.EntryPointFile, note)
	}
	for _, pattern := range smoke.GatePackagePatterns() {
		if !strings.Contains(check.Cmd, pattern) {
			t.Errorf("the branch check command does NOT address %q:\n  %s\n"+
				"a branch that touches no gate package must still run the gates — this is the "+
				"defect that let two reds live on master across a merge wave", pattern, check.Cmd)
		}
	}
	// The rendered command is the deliverable's own evidence, so it is printed
	// rather than only asserted: a reader of a `-v` run sees the exact argv the
	// delivery lane hands the declared entry point for a diff that touches NO
	// gate package.
	t.Logf("branch-scoped command rendered for a diff touching only %s:\n  %s", fcbGateFixturePkgDir, check.Cmd)
	// It must ALSO keep the diff's own package: the roster is a UNION, never a
	// replacement (a branch that changed a package would otherwise skip testing
	// what it changed).
	if !strings.Contains(check.Cmd, "./"+fcbGateFixturePkgDir+"/...") {
		t.Errorf("the branch check command dropped the package the diff touched:\n  %s", check.Cmd)
	}
	// And it is still ONE scoped argument (an unquoted list would make each
	// pattern after the first a make TARGET and silently shrink the run).
	if !strings.Contains(check.Cmd, smokegate.ScopedTarget) {
		t.Errorf("the command is not the declared scoped form (%q): %s", smokegate.ScopedTarget, check.Cmd)
	}
}

// TestFinishCleanupBranchCheckTestNarrowsToTheGatePackagesTheTreeHas is the
// ANTI-VACUUM twin, and it measures the property that keeps the roster from
// becoming a refusal: on a tree that does NOT carry a roster package, the
// command names only the members that ARE there — the diff's own packages are
// never dropped in exchange for a gate the tree does not have.
//
// The alternative (abstaining entirely) would turn "the gates are absent here"
// into "nothing is checked here", which is strictly worse than the defect the
// roster closes — and it is what makes the lane usable on fixtures and on other
// projects at all.
func TestFinishCleanupBranchCheckTestNarrowsToTheGatePackagesTheTreeHas(t *testing.T) {
	dir, base, branch := fcbGateFixture(t)
	gone := smoke.GatePackageDirs[0]
	if err := os.RemoveAll(filepath.Join(dir, filepath.FromSlash(gone))); err != nil {
		t.Fatalf("remove %s: %v", gone, err)
	}
	if missing := smoke.GatePackagesMissing(dir); len(missing) != 1 || missing[0] != gone {
		t.Fatalf("GatePackagesMissing(%s) = %v, want exactly [%s] — a caller cannot say which gate "+
			"was unavailable if the roster does not report it", dir, missing, gone)
	}
	check, note, ok := finishCleanupBranchCheckTest(dir, base, branch)
	if !ok {
		t.Fatalf("the lane abstained on a tree that still carries a gate package and a real diff: %q", note)
	}
	if strings.Contains(check.Cmd, smoke.PackagePattern(gone)) {
		t.Errorf("the command addresses %q, which this tree does NOT carry:\n  %s",
			smoke.PackagePattern(gone), check.Cmd)
	}
	for _, rel := range smoke.GatePackageDirs[1:] {
		if !strings.Contains(check.Cmd, smoke.PackagePattern(rel)) {
			t.Errorf("the command dropped %q, a gate package the tree DOES carry:\n  %s",
				smoke.PackagePattern(rel), check.Cmd)
		}
	}
	if !strings.Contains(check.Cmd, "./"+fcbGateFixturePkgDir+"/...") {
		t.Errorf("the command dropped the package the diff touched:\n  %s", check.Cmd)
	}
}

// TestMergeCheckCmdCarriesPackage pins the ONE-measurement predicate: the check
// that keeps internal/stylegate from being measured twice in one delivery (the
// scoped command already carries it, and the style-gate arm is skipped when it
// does).
func TestMergeCheckCmdCarriesPackage(t *testing.T) {
	// The real shape: the diff's own package UNION the roster (what
	// smoke.WithGatePackages answers), rendered by the lane's own composer.
	cmd := mergeCheckSmokeCommand(smoke.WithGatePackages([]string{"./internal/tools/..."}))
	for _, pattern := range smoke.GatePackagePatterns() {
		if !mergeCheckCmdCarriesPackage(cmd, pattern) {
			t.Errorf("mergeCheckCmdCarriesPackage(%q, %q) = false, want true — the roster package "+
				"the scoped command carries must be recognised, or the style-gate arm measures it twice",
				cmd, pattern)
		}
	}
	if mergeCheckCmdCarriesPackage(cmd, "./internal/standardsomething/...") {
		t.Error("a SUBSTRING neighbour must not read as the package itself")
	}
	for _, empty := range []string{"", " "} {
		if mergeCheckCmdCarriesPackage(empty, "./internal/standards/...") {
			t.Errorf("mergeCheckCmdCarriesPackage(%q, …) = true, want false (no command is not a command)", empty)
		}
	}
}
