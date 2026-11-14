package version

// The Go linker does NOT check that an `-X importpath.name=value` operand names
// a variable that exists: an operand whose path names nothing is dropped in
// SILENCE — the build exits 0, stderr stays empty, and the annotated variable
// keeps its zero value. Measured 2026-10-13 (Go 1.24.0): the documented recipe
//
//	CGO_ENABLED=0 go build -ldflags "-X <RevisionLdflagPath>=$(git rev-parse HEAD)" -o bin/gopi ./cmd/agent/
//
// with the operand's module half spelled `github.com/youngminz/gopi` instead of
// this go.mod's `github.com/nickhuang99/gopi` produced exit 0, empty stderr, and a
// binary whose `version` printed "revision: unavailable" — i.e. the build
// recipe degraded to stamping NOTHING, with no diagnostic at any surface.
//
// That makes RevisionLdflagPath (the ONE spelling of the operand — Rule
// 1b/13/14) a constant whose staleness is invisible everywhere else in the
// tree: a module rename does not break the build, does not break a test, and
// does not change a single output byte except the one that silently stops
// carrying the revision. This file is the missing check: the constant is held
// against the three facts it must agree with, each read from ITS OWN source of
// truth rather than trusted:
//
//	the module path  go.mod's `module` line — a two-line file anybody may edit
//	the package dir  this package's own directory, relative to the repo root
//	the variable     the `var Revision` declaration in this package's sources
//
// A module rename, a package move and a variable rename each make the operand
// point at nothing (silently, at link time) and each is caught here.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// goModFileName is the file the module path is read from — the source of
	// truth for the operand's first half, and the file whose EDITS this test
	// exists to catch (Rule 1b: named once, not typed at every call site).
	goModFileName = "go.mod"
	// goModModuleDirective is the keyword carrying the module path; the path is
	// the rest of that line.
	goModModuleDirective = "module "
	// goSourceSuffix selects the files scanned for the variable declaration.
	goSourceSuffix = ".go"
	// revisionVarName is the VARIABLE the operand must end in — the name of
	// the package-level `var` the linker writes when the operand is right, and
	// the symbol `gopi version` reads back (self.go).
	revisionVarName = "Revision"
	// testFileSuffix marks the files the declaration scan skips: a declaration
	// in a _test.go file is not one the linker can write.
	testFileSuffix = "_test.go"
	// revisionLdflagSeparator is the character between the operand's package
	// path and its variable name (`importpath.name`), and ldflagParentSep is
	// the one inside the package path itself.
	revisionLdflagSeparator = "."
)

// modulePathFromGoMod returns the module path go.mod declares at root, and
// fails the test — rather than skipping or returning "" — when the file is
// missing, unreadable, or carries no `module` directive: a check that cannot
// find its source of truth must be RED, never silently green (Rule 15; an
// empty module path would make the agreement assertion below fail anyway, but
// the reason would then be a mystery instead of a named defect).
func modulePathFromGoMod(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, goModFileName))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(root, goModFileName), err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, goModModuleDirective); ok {
			path := strings.TrimSpace(rest)
			if path == "" {
				t.Fatalf("%s declares an empty module path: %q", goModFileName, line)
			}
			return path
		}
	}
	t.Fatalf("%s declares no %q directive — the operand's first half has no source of truth", goModFileName, goModModuleDirective)
	return ""
}

// packageDirFromRoot walks up from the test's own working directory (the Go
// tool runs a package's tests with CWD = the package directory) to the nearest
// directory holding go.mod, and returns both that repo root and this package's
// directory RELATIVE to it, in slash form because it is glued into an import
// path. Failing loudly on both misses is the read floor: a walk that found no
// go.mod, or a package outside its module, must not pass for "agreement".
func packageDirFromRoot(t *testing.T) (root, relDir string) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for walk := dir; ; {
		if fi, statErr := os.Stat(filepath.Join(walk, goModFileName)); statErr == nil && !fi.IsDir() {
			rel, relErr := filepath.Rel(walk, dir)
			if relErr != nil {
				t.Fatalf("filepath.Rel(%s, %s): %v", walk, dir, relErr)
			}
			return walk, filepath.ToSlash(rel)
		}
		parent := filepath.Dir(walk)
		if parent == walk {
			t.Fatalf("no %s in any directory above %s — this package cannot be checked against its module", goModFileName, dir)
		}
		walk = parent
	}
}

// declaredVariableNames returns the package-level variable names declared by
// the non-test Go files of dir. It parses the sources (Rules: a scan that reads
// TEXT misses what it does not anticipate; go/parser sees every spelling of a
// declaration) and fails when it parsed nothing at all, so a broken directory
// read cannot pass vacuously.
func declaredVariableNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	names := map[string]bool{}
	fset := token.NewFileSet()
	parsed := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, goSourceSuffix) || strings.HasSuffix(name, testFileSuffix) {
			continue
		}
		f, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		parsed++
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, id := range value.Names {
					names[id.Name] = true
				}
			}
		}
	}
	if parsed == 0 {
		t.Fatalf("no non-test %s file parsed under %s — the declaration scan read nothing", goSourceSuffix, dir)
	}
	return names
}

// TestRevisionLdflagPathAgreesWithItsSourceOfTruth is the regression guard for
// the silent-drop path: it rebuilds the operand from the module path go.mod
// declares, this package's own directory, and the declared variable name, and
// requires RevisionLdflagPath to be exactly that.
//
// Mutation that makes it red (the measurement this file exists for): change
// RevisionLdflagPath's module half to `github.com/youngminz/gopi` — the
// assertion fails and NAMES the operand, the module path it must start with,
// and the rebuild-from-truth value. Without this test that same mutation
// ships: the tree builds, every other test passes, and the documented recipe
// silently stops stamping the revision (linker: exit 0, empty stderr).
func TestRevisionLdflagPathAgreesWithItsSourceOfTruth(t *testing.T) {
	root, relDir := packageDirFromRoot(t)
	modulePath := modulePathFromGoMod(t, root)
	// Read floor: the operand must be checked against a real package location
	// and a real declaration, never against an empty string that makes any
	// comparison trivially satisfiable.
	if relDir == "" || relDir == "." {
		t.Fatalf("this package's directory resolved to %q relative to %s; the operand's package half cannot be derived", relDir, root)
	}
	// The declaration is looked for in THIS package's directory (the operand's
	// path half names it), not at the repo root: `var Revision` at root would be
	// a variable the linker never writes through this operand. What is asserted
	// is that the variable IS DECLARED, not what it holds — the value is the
	// linker's to write, and `Revision = ""` is the correct zero state of a tree
	// nobody has stamped.
	pkgDir := filepath.Join(root, relDir)
	if !declaredVariableNames(t, pkgDir)[revisionVarName] {
		t.Fatalf("no package-level `var %s` is declared in the non-test sources under %s", revisionVarName, pkgDir)
	}

	want := modulePath + "/" + relDir + revisionLdflagSeparator + revisionVarName
	if RevisionLdflagPath != want {
		t.Errorf("RevisionLdflagPath = %q\n  want %q\n"+
			"built from go.mod's module path (%q), this package's directory (%q) and the declared "+
			"variable (%q). The Go linker does not check an -X operand: a stale path makes the "+
			"documented build stamp NOTHING at exit 0 with empty stderr (Rule 15). Fix the constant "+
			"(or the module path) — do not weaken this test.",
			RevisionLdflagPath, want, modulePath, relDir, revisionVarName)
	}
}

// TestRevisionLdflagPathNamesAVariableInThisPackage is the symbol half, kept
// separate so its failure says WHICH half broke: the operand's path is only
// correct if the name after its last dot is a variable the linker can actually
// write in the package the path points at. The linker answers a wrong name with
// the same silence it answers a wrong path with, so the name is checked here
// against the declaration in this package's own sources.
func TestRevisionLdflagPathNamesAVariableInThisPackage(t *testing.T) {
	_, relDir := packageDirFromRoot(t)
	at := strings.LastIndex(RevisionLdflagPath, revisionLdflagSeparator)
	if at <= 0 || at == len(RevisionLdflagPath)-1 {
		t.Fatalf("RevisionLdflagPath = %q does not have the `importpath.name` shape the -X flag requires", RevisionLdflagPath)
	}
	if pkgDir, varName := RevisionLdflagPath[:at], RevisionLdflagPath[at+1:]; !strings.HasSuffix(pkgDir, "/"+relDir) || varName != revisionVarName {
		t.Errorf("RevisionLdflagPath = %q: it names %q in package %q, but this package is %q and declares %q",
			RevisionLdflagPath, varName, pkgDir, relDir, revisionVarName)
	}
}
