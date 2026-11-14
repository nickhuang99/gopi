package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// This file owns the ONE build of the package under test that the
// binary-driving tests in this suite execute. Four independent copies of
// "build the gopi binary once" used to live in cli_version_test.go,
// home_settings_test.go, settings_nilpanic_test.go and cli_contracts_test.go,
// each writing the same package-level cache: whichever ran first won, so the
// binary the other three executed was decided by test order rather than by
// their intent, and the losers silently skipped the fresh build they expected
// (bug: duplicated gopi-binary build cache). One helper, one cache, one write.

// Build-destination policy constants (Rule 1a/1b: a policy spelled as an
// inline literal drifts).
const (
	// The pin name and the artifact's name come from the package that OWNS the
	// resolution (Rule 14/13: one definition, referenced — never re-typed).
	// gopiTestBinEnv overrides the build: point it at a prebuilt binary and
	// the whole suite uses that one instead (kept from the four original
	// copies, which all honoured this name).
	gopiTestBinEnv = testutil.SuiteBinEnv
	// gopiBuildDirPrefix names the per-run build directory created inside the
	// suite's pinned scratch root. Per-run + MkdirTemp-generated, never a
	// fixed /tmp path: /tmp is the boot device on this host, a predictable
	// name in a writable dir is a plant-a-symlink hazard, and nothing age-GCs
	// a fixed path.
	gopiBuildDirPrefix = "gopi-suite-bin-"
	// gopiCoverBuildDirPrefix names the per-run directory holding the
	// coverage-instrumented build (`go build -cover`). It is a SECOND artifact
	// on purpose: the plain binary and the instrumented one are different
	// programs (one writes coverage counters, the other is what production
	// ships), and substituting one for the other would make a test's claim
	// depend on a build flag it never asked for.
	gopiCoverBuildDirPrefix = "gopi-suite-coverbin-"
	// gopiBinFileName is the binary's name inside that directory.
	gopiBinFileName = testutil.BinFileName
)

// gopiBinOnce/gopiBinErr cache the ONE build per test binary: every case in
// every file of this package reuses the same freshly built binary.
//
//nolint:gochecknoglobals // process-level build cache
var (
	gopiBinOnce string
	gopiBinErr  error
)

// gopiTestBinPath returns the path of the gopi binary to drive: $GOPI_TEST_BIN
// when set, else a freshly built one. The build happens ONCE per test binary
// (process-level cache above) into the suite's pinned scratch root — the
// suite's TestMain removes that root, so the binary never outlives the run.
// A test whose build cannot be produced is SKIPPED, never failed: a plain
// `go test ./...` in a toolchain-less sandbox stays green.
func gopiTestBinPath(t *testing.T) string {
	t.Helper()
	if gopiBinOnce == "" && gopiBinErr == nil {
		gopiBinOnce, gopiBinErr = resolveGopiTestBin()
	}
	if gopiBinErr != nil {
		t.Skipf("could not build gopi binary: %v", gopiBinErr)
	}
	return gopiBinOnce
}

// resolveGopiTestBin produces the binary path ONCE: the operator's pinned
// $GOPI_TEST_BIN when set, else a fresh `go build` of this package (the test
// binary's own package, so the tests never execute a stale prebuilt).
func resolveGopiTestBin() (string, error) {
	if bin := os.Getenv(gopiTestBinEnv); bin != "" {
		return bin, nil
	}
	dir, err := os.MkdirTemp(testutil.SuiteTempRoot(), gopiBuildDirPrefix)
	if err != nil {
		return "", fmt.Errorf("build dir in scratch root %s: %w", testutil.SuiteTempRoot(), err)
	}
	out := filepath.Join(dir, gopiBinFileName)
	// Stamp the binary with the revision it is built from: the toolchain writes
	// `vcs.revision` only for a checkout whose `.git` is a DIRECTORY, so a linked
	// worktree — this package's own task ground (Rule 6a) — yields an artifact
	// carrying no identity at all, leaving the freshness guard to compare mtimes,
	// which cannot tell a byte-identical rewrite from a real edit. The operand's
	// spelling is testutil's (it reads internal/buildidentity's constants, Rule
	// 14) and an unresolvable revision is REPORTED there, never faked with an
	// operand-free flag (Rule 15).
	srcDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve this package's directory to stamp the tested binary: %w", err)
	}
	args := []string{"build", "-o", out}
	args = append(args, testutil.LdflagsFlag(srcDir)...)
	args = append(args, ".")
	if o, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build -o %s .: %w\n%s", out, err, o)
	}
	return out, nil
}

// gopiCoverBinOnce/gopiCoverBinErr cache the ONE coverage-instrumented build
// per test binary, exactly like the plain-binary cache above (same rationale:
// one producer, one cache entry).
//
//nolint:gochecknoglobals // process-level build cache
var (
	gopiCoverBinOnce string
	gopiCoverBinErr  error
)

// CoverBin returns a gopi binary built with `go build -cover`, so a test that
// drives the CLI out of process (the only way to exercise the exit paths)
// contributes its child's statement counters to `go test -coverprofile`.
//
// It is the SAME package build as gopiTestBinPath, only instrumented — the
// tests that use it are testing production behaviour, not the build flag.
// $GOPI_TEST_BIN_COVER overrides it, mirroring $GOPI_TEST_BIN.
func CoverBin(t *testing.T) string {
	t.Helper()
	if gopiCoverBinOnce == "" && gopiCoverBinErr == nil {
		gopiCoverBinOnce, gopiCoverBinErr = resolveGopiCoverBin()
	}
	if gopiCoverBinErr != nil {
		t.Skipf("could not build the coverage-instrumented gopi binary: %v", gopiCoverBinErr)
	}
	return gopiCoverBinOnce
}

// gopiTestCoverBinEnv overrides the instrumented build.
const gopiTestCoverBinEnv = "GOPI_TEST_BIN_COVER"

// resolveGopiCoverBin produces the instrumented binary ONCE.
func resolveGopiCoverBin() (string, error) {
	if bin := os.Getenv(gopiTestCoverBinEnv); bin != "" {
		return bin, nil
	}
	dir, err := os.MkdirTemp(testutil.SuiteTempRoot(), gopiCoverBuildDirPrefix)
	if err != nil {
		return "", fmt.Errorf("cover build dir in scratch root %s: %w", testutil.SuiteTempRoot(), err)
	}
	out := filepath.Join(dir, gopiBinFileName)
	if o, err := exec.Command("go", "build", "-cover", "-o", out, ".").CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build -cover -o %s .: %w\n%s", out, err, o)
	}
	return out, nil
}

// gopiBinCacheOwner is the ONE file allowed to hold the build cache. The
// four-way duplication this file replaced was invisible to the compiler
// (the copies all compiled), so the guard is exhaustive rather than
// conventional: every other test file in this package must not mention the
// cache at all.
const gopiBinCacheOwner = "gopibin_test.go"

// TestGopiBinCacheHasSingleOwner is the anti-regression guard for the
// duplicated-build-cache bug: it parses every *_test.go in this package and
// fails if any file other than gopiBinCacheOwner declares OR writes
// gopiBinOnce/gopiBinErr, and if the owner itself holds more than one
// declaration or more than one write. Repeated owners = order-dependent
// binary selection, exactly the bug (an exhaustive predicate beats a
// convention somebody re-inlines next month).
func TestGopiBinCacheHasSingleOwner(t *testing.T) {
	cached := []string{"gopiBinOnce", "gopiBinErr", "gopiCoverBinOnce", "gopiCoverBinErr"}
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package dir: %v", err)
	}
	found := 0
	for _, path := range paths {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		decls, writes := gopiBinCacheTouches(file, cached)
		if decls+writes == 0 {
			continue
		}
		found++
		if path != gopiBinCacheOwner {
			t.Errorf("%s declares/writes the gopi binary cache (%d decl, %d write) — the cache is owned by %s alone",
				path, decls, writes, gopiBinCacheOwner)
			continue
		}
		if decls != len(cached) {
			t.Errorf("%s: want exactly %d package-level declarations (%v), got %d",
				path, len(cached), cached, decls)
		}
		if writes != 2 {
			t.Errorf("%s: want exactly 2 cache writes (plain + cover build, each assigned once), got %d", path, writes)
		}
	}
	if found == 0 {
		t.Errorf("no file carries the gopi binary cache — the guard is reading blind (expected %s)", gopiBinCacheOwner)
	}
}

// gopiBinCacheTouches counts package-level var declarations and assignment
// writes of any of names in one parsed file.
func gopiBinCacheTouches(file *ast.File, names []string) (decls, writes int) {
	isCached := func(id *ast.Ident) bool {
		for _, n := range names {
			if id != nil && id.Name == n {
				return true
			}
		}
		return false
	}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, id := range vs.Names {
				if isCached(id) {
					decls++
				}
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.Tok != token.ASSIGN {
			return true
		}
		for _, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && isCached(id) {
				writes++ // one per STATEMENT, not per name: the cache pair is assigned together
				break
			}
		}
		return true
	})
	return decls, writes
}
