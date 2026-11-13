// Package smoke maps a branch's diff to the Go packages it touched and runs the
// repo's declared smoke command on EXACTLY those packages.
//
// WHY THIS EXISTS
// ---------------
// The merge/verify path had two bad options and no good one. It could run
// `go test ./...` — the whole tree, i.e. every fixture, every package, the
// expensive double-work the heavy-test epic complains about — or it could run
// nothing behavioural at all (take-over runs only build + vet). What was
// missing is the middle: the branch touched internal/foo and internal/bar, so
// the honest smoke run is `./internal/foo/... ./internal/bar/...`.
//
// That mapping is the whole point of this package, and it is deliberately
// narrow:
//
//   - the selection is a PURE function of the diff plus the branch tip. It
//     never falls back to `./...` (the failure mode this exists to remove) and
//     it never guesses: an empty selection is returned as an empty selection;
//   - a deleted file is handled by asking the TREE, not the diff — the diff
//     names a path that no longer exists, so the tip decides whether the
//     directory is still a package (a deletion that empties a directory drops
//     it; a deletion inside a surviving package keeps it, because that package
//     still has to compile and its tests still have to pass);
//   - a nested module under the repo (this repo carries several under devdoc/)
//     is NOT part of the root module's package pattern — `./devdoc/x/...` would
//     be outside the main module, so it is dropped;
//   - heavy tests inside a selected package still skip by themselves
//     (internal/heavytest), so the smoke run stays cheap without this package
//     knowing anything about heaviness.
//
// The command itself is NOT invented here: `Command` is the one exported name
// for the repo's single-source, budget-declared test entry point (Rule 14).
// Every consumer — a merge lane, a resolve lane, an operator one-liner —
// references it instead of re-inlining `go test`, so a change to the budget or
// its reporting lands in one place.
package smoke

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// Command is the repo-relative entry point that runs the suite with the
	// DECLARED per-package budget. It is exported so no consumer re-inlines a
	// bare `go test ./...` (Rule 1b/14): the budget, the timeout report and the
	// per-run JSON stream all come from that one script.
	//
	// SEAM WITH THE SIBLING SPLIT (2/4, `feat/wire-smoke-gate-merge-path`): that
	// branch adds internal/smokegate with its own exported Command ("make test",
	// the Makefile target that routes through this same script) plus a
	// ScopedCommand slot for BRIEFING TEXT, and it deliberately leaves the
	// branch→packages mapping to this package. Neither branch can import the
	// other while only one is on master, so the two constants coexist until both
	// land; the unification is then a one-line change: a consumer that already
	// has smokegate should pass smokegate.Command down (Execute/ExecuteTo take
	// the entry point through this constant today) and this constant should
	// become the alias, leaving exactly ONE definition (Rule 13).
	Command = "scripts/test-budget.sh"

	// NoPackagesError is returned by Execute when the selection is empty:
	// running nothing is a DECISION a caller must make explicitly, never a
	// silent green (Rule 15).
	NoPackagesError = "smoke: no touched packages to test"

	// diffRangeSeparator is git's "symmetric difference" range operator:
	// `git diff --name-only base...branch` lists what the BRANCH changed since
	// the merge base — not what master changed meanwhile.
	diffRangeSeparator = "..."

	// goSourceSuffix identifies a Go source file (test files included: they are
	// part of their package).
	goSourceSuffix = ".go"

	// goModName is the module marker; its directory is a module root.
	goModName = "go.mod"

	// gitDiffNameOnly lists changed paths without contents.
	gitDiffNameOnly = "--name-only"

	// gitLsTreeRecursive lists every path of a tree, recursively.
	gitLsTreeRecursive = "-r"

	// gitContextDirFlag makes one git invocation resolve a directory without
	// requiring the caller's working directory to be the repo (no global
	// chdir, which would race other goroutines in the same process).
	gitContextDirFlag = "-C"

	// submoduleMode marks a submodule entry in `git ls-tree`; its tree is NOT
	// part of this module, so its paths are never recursed into.
	submoduleMode = "160000"

	// gitCommandTimeout bounds one git invocation. A bounded subprocess is not
	// optional: a hung git on the merge path would hold the tick (Rule 1a —
	// named, not inlined).
	gitCommandTimeout = 60 * time.Second

	// optionalLocksEnv tells git not to take optional index locks for
	// read-only queries, so a selection never contends with a concurrent
	// commit in the same repo.
	optionalLocksEnv = "GIT_OPTIONAL_LOCKS=0"

	// patternPrefix / patternSuffix / rootPackagePattern compose a go-test
	// package pattern. The root package is addressed as `./` ALONE: appending
	// the recursive ellipsis would address the whole module, which is exactly
	// the whole-tree run this package exists to replace (Rule 1b: named, not
	// spelled at each call site).
	patternPrefix      = "./"
	patternSuffix      = "/..."
	rootPackagePattern = "./"
)

// ErrNoPackages reports an empty selection. Callers decide the policy: a
// merge lane may treat a docs-only branch as "nothing to smoke", while a
// verification lane may require at least one package.
var ErrNoPackages = errors.New(NoPackagesError)

// Tree is the branch-tip view the selector needs, parsed from one `git
// ls-tree` call: which directories still hold Go sources (so a deleted file is
// judged by the tree, not by the diff), and which directories are their own
// module (so a nested module is never offered to the root module's test run).
type Tree struct {
	goDirs        map[string]bool
	nestedModules map[string]bool
}

// Selection is the outcome of mapping a branch's diff to packages.
type Selection struct {
	// Packages are go-test patterns (`./internal/foo/...`), sorted and deduped.
	// Empty means the diff changed nothing testable.
	Packages []string
	// ChangedGoFiles are the .go paths the diff reported, sorted. Kept so a
	// report can show WHY a package was selected.
	ChangedGoFiles []string
}

// Empty reports whether the selection names no package.
func (s Selection) Empty() bool { return len(s.Packages) == 0 }

// ChangedGoFiles lists the Go files the branch changed since the merge base
// with base, using the same `base...branch` range the merge machinery uses.
// Deleted paths are included — the tree, not the diff, decides whether their
// directory is still a package.
func ChangedGoFiles(repoDir, base, branch string) ([]string, error) {
	rangeSpec := base + diffRangeSeparator + branch
	out, err := git(repoDir, "diff", gitDiffNameOnly, rangeSpec)
	if err != nil {
		return nil, fmt.Errorf("smoke: diff %s: %w", rangeSpec, err)
	}
	files := make([]string, 0, 32)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, goSourceSuffix) {
			continue
		}
		files = append(files, line)
	}
	sort.Strings(files)
	return files, nil
}

// TipTree reads the branch tip: the directories holding Go sources, and the
// directories that are their own module. One git call keeps the selector cheap
// enough for a merge tick.
func TipTree(repoDir, branch string) (Tree, error) {
	out, err := git(repoDir, "ls-tree", gitLsTreeRecursive, "--name-only", branch)
	if err != nil {
		// `go.mod`-less git trees (a bare fixture with only blobs) are still
		// readable; any real failure is reported, never softened into an empty
		// tree (Rule 15).
		return Tree{}, fmt.Errorf("smoke: ls-tree %s: %w", branch, err)
	}
	tree := Tree{goDirs: map[string]bool{}, nestedModules: map[string]bool{}}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		dir := path.Dir(line)
		if dir == "." {
			dir = ""
		}
		if strings.HasSuffix(line, goSourceSuffix) {
			tree.goDirs[dir] = true
		}
		if path.Base(line) == goModName && dir != "" {
			tree.nestedModules[dir] = true
		}
	}
	return tree, nil
}

// PackagesIn maps changed Go files onto testable package patterns, given the
// branch tip. It is pure — every judgement is visible here and unit-testable
// without a repository:
//
//   - a directory is selected only if the TIP still holds a Go source in it
//     (a file deleted from a package that survives keeps that package; a
//     directory emptied by the branch disappears);
//   - a directory inside a nested module is dropped (it belongs to another
//     module, so the root module's pattern cannot address it);
//   - the repository root's own package becomes `./...`? No: `./...` is the
//     WHOLE module — the root package is addressed as `./` alone, because
//     anything wider would re-introduce the full-suite run this package exists
//     to replace.
func PackagesIn(files []string, tree Tree) []string {
	seen := map[string]bool{}
	var out []string
	for _, file := range files {
		if !strings.HasSuffix(file, goSourceSuffix) {
			continue
		}
		dir := path.Dir(file)
		if dir == "." {
			dir = ""
		}
		if !tree.goDirs[dir] {
			continue // the tip has no Go source there: not a package any more
		}
		if underNestedModule(dir, tree.nestedModules) {
			continue
		}
		pattern := PackagePattern(dir)
		if seen[pattern] {
			continue
		}
		seen[pattern] = true
		out = append(out, pattern)
	}
	sort.Strings(out)
	return out
}

// PackagePattern renders the go-test pattern for a module-relative directory
// ("" is the module root). The root uses `./` alone and never `./...` — the
// ellipsis addresses the WHOLE module and would defeat the selection.
func PackagePattern(dir string) string {
	if dir == "" {
		return rootPackagePattern
	}
	return patternPrefix + dir + patternSuffix
}

// TouchedPackages is the primitive the epic asked for: repo + base + branch
// in, the set of package patterns the branch touched out. It performs exactly
// two git calls and never falls back to the whole module.
func TouchedPackages(repoDir, base, branch string) (Selection, error) {
	files, err := ChangedGoFiles(repoDir, base, branch)
	if err != nil {
		return Selection{}, err
	}
	tree, err := TipTree(repoDir, branch)
	if err != nil {
		return Selection{}, err
	}
	return Selection{Packages: PackagesIn(files, tree), ChangedGoFiles: files}, nil
}

// Args is the argument list Execute runs: the selected packages first, then any
// extra go-test flags the caller needs (a `-run` narrowing, `-count`). It is
// exported so a report can print the exact command a selection produced.
func Args(packages []string, extraArgs ...string) []string {
	args := make([]string, 0, len(packages)+len(extraArgs))
	args = append(args, packages...)
	args = append(args, extraArgs...)
	return args
}

// Execute runs the repo's declared smoke command on the given packages, with
// the child's stdout/stderr streamed to the caller's own (so a human or a log
// sees the run as it happens). It returns the child's exit code; a non-zero
// code is NOT an error — the script's codes are its report (0 green, 1
// assertion/build failure, 2 usage, 3 budget trip, 5 a FILTERED run that
// executed no test at all — see devdoc/test-budget-no-tests-to-run.md), and an
// error is reserved for "could not start the run at all".
func Execute(repoDir string, packages []string, extraArgs ...string) (int, error) {
	return ExecuteTo(repoDir, os.Stdout, os.Stderr, packages, extraArgs...)
}

// ExecuteTo is Execute with the child's streams named by the caller: a report
// harness captures them instead of inheriting the terminal. Splitting this out
// is what lets a test assert on the run's own output (the heavy-skip line, the
// selected package list) rather than only on the ledger it leaves behind.
func ExecuteTo(repoDir string, stdout, stderr io.Writer, packages []string, extraArgs ...string) (int, error) {
	if len(packages) == 0 {
		return 0, ErrNoPackages
	}
	script := filepath.Join(repoDir, filepath.FromSlash(Command))
	if _, err := os.Stat(script); err != nil {
		return 0, fmt.Errorf("smoke: %s: %w (the declared smoke entry point is missing)", Command, err)
	}
	cmd := exec.Command(script, Args(packages, extraArgs...)...)
	cmd.Dir = repoDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), optionalLocksEnv)
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, fmt.Errorf("smoke: run %s: %w", Command, err)
}

// Run is the whole gate in one call: select, then execute. It returns the
// selection alongside the exit code so a caller can report both what was
// chosen and how it went. An empty selection returns ErrNoPackages with the
// (empty) selection attached — the caller decides whether that is acceptable.
func Run(repoDir, base, branch string, extraArgs ...string) (Selection, int, error) {
	sel, err := TouchedPackages(repoDir, base, branch)
	if err != nil {
		return Selection{}, 0, err
	}
	if sel.Empty() {
		return sel, 0, ErrNoPackages
	}
	code, err := Execute(repoDir, sel.Packages, extraArgs...)
	return sel, code, err
}

// underNestedModule reports whether dir belongs to a module other than the root
// one — the directory itself being a module root, or anything beneath it.
func underNestedModule(dir string, modules map[string]bool) bool {
	for mod := range modules {
		if dir == mod || strings.HasPrefix(dir, mod+"/") {
			return true
		}
	}
	return false
}

// git runs one read-only git query in repoDir, with a bounded wall clock.
func git(repoDir string, args ...string) (string, error) {
	full := append([]string{gitContextDirFlag, repoDir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), optionalLocksEnv)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(gitCommandTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return stdout.String(), errors.New(msg)
		}
		return stdout.String(), nil
	case <-timer.C:
		if cmd.Process != nil {
			// Best-effort kill; the timeout is reported either way.
			_ = cmd.Process.Kill() //stylegate:ignore the smoke run is being abandoned; a failed kill only means it already exited
		}
		return "", fmt.Errorf("git %s timed out after %s", strings.Join(args, " "), gitCommandTimeout)
	}
}
