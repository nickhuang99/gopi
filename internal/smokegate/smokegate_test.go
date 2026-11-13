package smokegate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScopedCommandStaysOneSingleQuotedArgument pins the verified shell/make
// fact this constant exists to carry: `PKGS=<list>` must be ONE single-quoted
// assignment. Measured in this repo's Makefile —
//
//	make test-budget PKGS=./internal/tools/ ./internal/agent/   # BROKEN
//	→ scripts/test-budget.sh ./internal/tools/
//	  make: Nothing to be done for 'internal/agent/'.
//
// because the second path is parsed as a make TARGET, not part of the variable.
// An unquoted variant therefore silently runs a SUBSET while reporting green —
// the exact silent-failure shape (PROJECT.md Rule 15) a merge-path instruction
// must never have.
func TestScopedCommandStaysOneSingleQuotedArgument(t *testing.T) {
	if !strings.Contains(ScopedCommand, "PKGS='") {
		t.Errorf("ScopedCommand %q no longer opens a single-quoted PKGS assignment — an unquoted package "+
			"list makes make treat all but the first as targets, so the run silently covers a subset", ScopedCommand)
	}
	if strings.Count(ScopedCommand, "'") != 2 {
		t.Errorf("ScopedCommand %q must carry exactly two quotes (one PKGS='…' assignment)", ScopedCommand)
	}
	if !strings.Contains(ScopedCommand, Command) {
		t.Errorf("ScopedCommand %q must be the SAME command as Command %q, only narrowed", ScopedCommand, Command)
	}
}

// TestCommandIsNotTheWholeTree is the invariant that makes this package worth
// existing: the smoke command is the Makefile target, never the bare whole-tree
// invocation that bypasses the declared per-package budget and the heavy-skip
// ledger.
func TestCommandIsNotTheWholeTree(t *testing.T) {
	if strings.Contains(Command, WholeTreeNeedle) {
		t.Errorf("Command %q carries the whole-tree invocation (%q) — the merge/verify path must route "+
			"through the declared budget + skip ledger", Command, WholeTreeNeedle)
	}
	if WholeTreeNeedle == Command {
		t.Error("WholeTreeNeedle and Command are identical — one of them is wrong")
	}
	if !strings.Contains(Doc, "BRANCH-SCOPED") || !strings.Contains(Doc, "never the whole tree") {
		t.Errorf("Doc %q must state the rule both halves (branch-scoped, never the whole tree)", Doc)
	}
}

// TestScopedForPackagesBuildsTheDeclaredScopedForm pins the seam a CODE caller
// uses (as opposed to the briefing TEXT above): the target and the assignment
// come from this package, and the caller's list travels as ONE argument.
func TestScopedForPackagesBuildsTheDeclaredScopedForm(t *testing.T) {
	quote := func(s string) string { return "'" + s + "'" }
	got := ScopedForPackages([]string{"./internal/a/...", "./internal/b/..."}, quote)
	want := ScopedTarget + " " + ScopedPackagesAssignment + "'./internal/a/... ./internal/b/...'"
	if got != want {
		t.Fatalf("ScopedForPackages = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, makeVerb+" ") {
		t.Errorf("the scoped form must be a make invocation, got %q", got)
	}
	if strings.Count(got, "'") != 2 {
		t.Errorf("the package list must be ONE single-quoted argument (make reads an unquoted 2nd path as a "+
			"TARGET and silently runs a subset), got %q", got)
	}
	if ScopedCommand != ScopedForPackages([]string{"<the Go packages your diff touched>"}, quote) {
		t.Errorf("ScopedCommand %q and ScopedForPackages disagree about the command's head — they must be the "+
			"SAME spelling (Rule 13)", ScopedCommand)
	}
}

// TestDeclaresScopedTargetReadsTheTreeNotAGuess pins the probe's both arms: the
// repo DECLARES the target (so the answer is a fact about the tree), and a tree
// without it says so instead of leaving a caller to invent a command.
func TestDeclaresScopedTargetReadsTheTreeNotAGuess(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	declared, err := DeclaresScopedTarget(repoRoot)
	if err != nil {
		t.Fatalf("DeclaresScopedTarget(%s): %v", repoRoot, err)
	}
	if !declared {
		t.Fatalf("this repo's %s must declare %q — %q names it", EntryPointFile, ScopedTarget, ScopedCommand)
	}
	empty := t.TempDir()
	missing, err := DeclaresScopedTarget(empty)
	if err == nil && missing {
		t.Fatalf("a directory with no %s must not report the target as declared", EntryPointFile)
	}
	if err == nil {
		t.Fatalf("a directory with no %s must report the read failure, not an invented answer", EntryPointFile)
	}
	// A Makefile that MENTIONS the target without defining it is not a
	// declaration (the scan looks for a rule, not a word).
	dir := t.TempDir()
	body := "# " + scopedTargetName + ": mentioned here, defined nowhere\nother:\n\ttrue\n"
	if werr := os.WriteFile(filepath.Join(dir, EntryPointFile), []byte(body), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if got, rerr := DeclaresScopedTarget(dir); rerr != nil || got {
		t.Fatalf("a comment mentioning the target must not count as a declaration (got %v, err %v)", got, rerr)
	}
}
