package tools

// THE DOCS-ONLY ABSTENTION (bug row 01a1ad80, found by 01a1ad54's measurement).
//
// The gate-roster regression beside this file pins the UNION: a branch whose
// diff touches a non-gate package still addresses every gate package. It does
// NOT pin the shape that actually broke — a branch whose diff names NO Go
// package at all. That shape took an EARLIER exit than the union: the lane
// computed `smoke.TouchedPackages`, found it empty, and returned
// finishCleanupBranchCheckNoPackagesMark before `smoke.WithGatePackagesIn` was
// ever called, so on a markdown-only delivery every gate in the roster went
// unrun and the delivery read green.
//
// MEASURED CONSEQUENCE (this is not hypothetical): the gap-doc contract gate
// landed in `07b85fd2`, and the three docs-only commits after it
// (`115340f6` / `8a867570` / `521e580b`, 0 Go files each) each took that path —
// so the gate they existed to satisfy ran nowhere, and the documents landed red
// on master for a whole wave.
//
// The fix is ORDER, and these two arms are the order's red/green:
//
//   - a docs-only branch in a tree that CARRIES the roster must produce a
//     runnable check naming `internal/standards` and `internal/stylegate`
//     (the union is taken BEFORE the emptiness question);
//   - a docs-only branch in a tree that carries NONE of the roster must still
//     abstain, and must say the words rather than pass silently (Rule 15: the
//     remaining abstention names its reason).
//
// Both arms drive the REAL surface on a REAL repository: a real module, a real
// declared Makefile target, real gate package directories, a real branch and the
// real `git diff base...branch` the selector reads. A unit test on
// `smoke.WithGatePackagesIn` alone cannot see the defect — it is a CALL-SITE
// ordering bug, so the call site is what is measured here (Rule 31).

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
	// fcbDocsFixtureNoteDir is the ONLY thing the docs-only branch changes: a
	// directory that is not a Go package, so the diff's selection is empty and
	// the branch is exactly the shape that used to abstain.
	fcbDocsFixtureNoteDir = "devdoc"
	// fcbDocsFixtureNoteFile is the markdown file inside it.
	fcbDocsFixtureNoteFile = "note.md"
	// fcbDocsFixtureNoteBase / fcbDocsFixtureNoteChanged are its two revisions:
	// the base content and the one line the branch adds. A MODIFIED file (not an
	// added one) keeps the diff a plain `M`, which is what a docs landing is.
	fcbDocsFixtureNoteBase    = "# note\n"
	fcbDocsFixtureNoteChanged = "# note\n\nthe branch's own change\n"
	// fcbDocsFixtureBaseBranch / fcbDocsFixtureWorkBranch are the two refs the
	// selector diffs between.
	fcbDocsFixtureBaseBranch = "base"
	fcbDocsFixtureWorkBranch = "work"
	// fcbDocsFixtureAgentFile makes the tree carry the declared entry point's
	// PREREQUISITE (finishCleanupCLIPackageDir), so these arms measure the
	// SELECTION and not the preflight abstention: a fixture without it would set
	// check.preflightWhy and the measurement would be about something else.
	fcbDocsFixtureAgentFile = "agent.go"
)

// fcbDocsOnlyFixture builds the docs-only repository described above and returns
// its directory plus the two refs. `withRoster` decides whether the tree carries
// the gate package directories — the presence question `WithGatePackagesIn` asks.
//
// The refs are CUT, never fabricated: `base` is a commit and `work` is its
// child, so `TouchedPackages` performs the real git reads.
func fcbDocsOnlyFixture(t *testing.T, withRoster bool) (dir, base, branch string) {
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
		// testutil.GitEnv strips GIT_DIR / GIT_WORK_TREE and the object/index
		// routing names from the inherited environment (cmd.Dir does NOT override
		// GIT_DIR), then appends the fixture's own pins.
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=feb", "GIT_AUTHOR_EMAIL=feb@example.com",
			"GIT_COMMITTER_NAME=feb", "GIT_COMMITTER_EMAIL=feb@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "--initial-branch", fcbDocsFixtureBaseBranch)
	// The declared entry point + the module it lives in (Rule 12: a repo's build
	// contract is DECLARED; the recipe is inert because these arms measure the
	// command the lane renders, not its execution).
	write(smokegate.EntryPointFile, fcbGateFixtureMakefile)
	write("go.mod", fcbGateFixtureGoMod)
	// The entry point's prerequisite package.
	write(finishCleanupCLIPackageDir+"/"+fcbDocsFixtureAgentFile, "package agent\n")
	// The file the branch will change — present at the base as a tracked path.
	write(fcbDocsFixtureNoteDir+"/"+fcbDocsFixtureNoteFile, fcbDocsFixtureNoteBase)

	basePaths := []string{
		smokegate.EntryPointFile,
		"go.mod",
		filepath.FromSlash(finishCleanupCLIPackageDir) + "/" + fcbDocsFixtureAgentFile,
		filepath.FromSlash(fcbDocsFixtureNoteDir) + "/" + fcbDocsFixtureNoteFile,
	}
	// The roster packages exist as DIRECTORIES (that is the question the lane
	// asks). The list is read from the roster itself, never spelled again here.
	if withRoster {
		for _, rel := range smoke.GatePackageDirs {
			p := filepath.FromSlash(rel) + "/gate.go"
			write(p, "package gate\n")
			basePaths = append(basePaths, p)
		}
	}
	// Rule 8: the paths are added one by one, never with a blanket add.
	for _, p := range basePaths {
		git("add", p)
	}
	git("commit", "-q", "-m", "base: module + declared target + docs file")
	base = git("rev-parse", "HEAD")
	git("checkout", "-q", "-b", fcbDocsFixtureWorkBranch)
	// The branch's ONLY change: markdown. No Go file, so the diff names no
	// package — the exact shape the lane used to abstain on.
	write(fcbDocsFixtureNoteDir+"/"+fcbDocsFixtureNoteFile, fcbDocsFixtureNoteChanged)
	git("add", filepath.FromSlash(fcbDocsFixtureNoteDir)+"/"+fcbDocsFixtureNoteFile)
	git("commit", "-q", "-m", "docs: a markdown-only change")
	branch = git("rev-parse", "HEAD")
	return dir, base, branch
}

// TestFinishCleanupBranchCheckTestRunsTheGatesOnADocsOnlyBranch is THE
// regression for this bug: the diff selects nothing, the tree carries the
// roster, and the lane must still build a runnable check that addresses every
// gate package.
//
// Before the fix this arm fails at the first assertion with
// ok=false and note == finishCleanupBranchCheckNoPackagesMark — the lane
// returned before the union, which is the defect stated as an observation.
func TestFinishCleanupBranchCheckTestRunsTheGatesOnADocsOnlyBranch(t *testing.T) {
	dir, base, branch := fcbDocsOnlyFixture(t, true)

	// THE PREMISE, measured rather than assumed: this branch's diff really does
	// name no Go package. Without this the arm could pass on a branch that
	// touched Go, and would then be a test of the OTHER regression.
	sel, err := smoke.TouchedPackages(dir, base, branch)
	if err != nil {
		t.Fatalf("TouchedPackages(%s, %s, %s): %v", dir, base, branch, err)
	}
	if len(sel.Packages) != 0 {
		t.Fatalf("the fixture's diff selects %v — it must select NOTHING for this arm to measure "+
			"the docs-only shape at all", sel.Packages)
	}

	check, note, ok := finishCleanupBranchCheckTest(dir, base, branch)
	if !ok {
		t.Fatalf("the lane ABSTAINED on a docs-only branch in a tree that carries the whole gate "+
			"roster: %q\n"+
			"that is the defect: the union of smoke.WithGatePackagesIn is taken AFTER the empty-"+
			"selection question, so a markdown-only landing runs no gate at all", note)
	}
	if note != "" {
		t.Errorf("a RUNNABLE check must carry no abstention sentence, got %q", note)
	}
	// BOTH gate packages, by name, in the command the lane would actually run.
	// They are read from the roster so a third member added there is covered
	// here for free (Rule 13: never a second spelling of the membership).
	for _, rel := range smoke.GatePackageDirs {
		if !strings.Contains(check.Cmd, smoke.PackagePattern(rel)) {
			t.Errorf("the docs-only branch check does NOT address %q:\n  %s\n"+
				"a markdown-only delivery must still run the gates — the gap-doc gate landed in "+
				"07b85fd2 and the three docs-only commits after it each skipped it this way",
				smoke.PackagePattern(rel), check.Cmd)
		}
	}
	// The two the epic names explicitly, asserted by their spelled constants: a
	// roster that was emptied would pass the loop above vacuously, and this arm
	// exists because a vacuous gate read green for a whole wave.
	for _, want := range []string{smoke.GatePackageStandards, smoke.GatePackageStylegate} {
		if !strings.Contains(check.Cmd, smoke.PackagePattern(want)) {
			t.Errorf("the docs-only branch check omits %q specifically:\n  %s", want, check.Cmd)
		}
	}
	// It is still the DECLARED scoped form (Rule 27: never the whole-tree run),
	// and the check is named and bounded like every other arm.
	if !strings.Contains(check.Cmd, smokegate.ScopedTarget) {
		t.Errorf("the command is not the declared scoped form (%q): %s", smokegate.ScopedTarget, check.Cmd)
	}
	// The prerequisite abstention is NOT armed: this fixture carries
	// finishCleanupCLIPackageDir, so nothing may claim the tree is unmeasurable —
	// which is also the second half of the fix (the preflight asks the RUNNABLE
	// list, not the diff's raw selection).
	if check.preflightWhy != "" {
		t.Errorf("the check was armed with a preflight abstention (%q) on a tree that carries %q",
			check.preflightWhy, finishCleanupCLIPackageDir)
	}
	// Printed, not only asserted: this string IS the deliverable's evidence, and a
	// reader of a -v run sees the exact command a docs-only landing now runs.
	t.Logf("branch-scoped command rendered for a markdown-only diff:\n  %s", check.Cmd)
}

// TestFinishCleanupBranchCheckTestAbstainsWhenTheTreeCarriesNoRoster is the
// OTHER side of the same order, and it is what keeps the fix from becoming a
// refusal: a docs-only branch in a tree that carries NONE of the roster's
// packages has nothing to run, and the lane must SAY that (Rule 15) instead of
// rendering a command that would fail for a reason nobody named.
//
// If the emptiness question were simply DELETED rather than moved, this arm
// fails: the lane would return a check whose command names no package this tree
// has, and `ok=true` would hide it.
func TestFinishCleanupBranchCheckTestAbstainsWhenTheTreeCarriesNoRoster(t *testing.T) {
	dir, base, branch := fcbDocsOnlyFixture(t, false)

	// The premise, again measured: the tree really carries none of the roster.
	if missing := smoke.GatePackagesMissing(dir); len(missing) != len(smoke.GatePackageDirs) {
		t.Fatalf("GatePackagesMissing(%s) = %v, want all of %v — this arm's tree must carry no "+
			"roster package", dir, missing, smoke.GatePackageDirs)
	}
	sel, err := smoke.TouchedPackages(dir, base, branch)
	if err != nil {
		t.Fatalf("TouchedPackages(%s, %s, %s): %v", dir, base, branch, err)
	}
	if len(sel.Packages) != 0 {
		t.Fatalf("the fixture's diff selects %v — it must select NOTHING", sel.Packages)
	}

	check, note, ok := finishCleanupBranchCheckTest(dir, base, branch)
	if ok {
		t.Fatalf("the lane claimed a runnable check on a tree that carries no roster package and "+
			"whose diff selects no package: cmd=%q\n"+
			"there is nothing to run here, and a command naming absent packages is not a check",
			check.Cmd)
	}
	if note != finishCleanupBranchCheckNoPackagesMark {
		t.Fatalf("the abstention sentence is %q, want %q — the reason is the one thing an operator "+
			"reads, and a generic sentence cannot be told apart from a silent pass",
			note, finishCleanupBranchCheckNoPackagesMark)
	}
	// Nothing was built: an abstained check must not carry a half-rendered
	// command, or a caller that logged `check.Cmd` would print a command nobody
	// ran.
	if check.Cmd != "" {
		t.Errorf("an abstained check carries a command (%q) — it must not", check.Cmd)
	}
}
