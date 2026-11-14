package memoryprefetch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// tmpRootSpelling is the ambient temp-root SPELLING this package's production
// files must not mention — the string TestNoHardcodedScratchOrTempPaths bans,
// named so the literal has one declared home in this file (Rule 1b: the point
// is the name, not the spelling).
//
// It is a DECLARED FIXTURE VALUE by the Rule 13 worktree-root literal gate's
// fixture topology (internal/standards/worktree_root_gate.go): a fixture may
// name the bare host root only as a constant/variable declared in the same
// file. The alternative the gate prefers in production — deriving the path from
// worktreeroots — is unavailable HERE by construction: this package is
// stdlib-only (TestPackageImportsStdlibOnly below), and the point of the test
// using this value is the spelling itself, so an import would break the very
// constraint under test.
const tmpRootSpelling = "/tmp"

// projectImportPrefix and segmenterImportPath spell the repo's module path and
// the ONE project package this package is allowed to import (see
// TestPackageImportsStdlibOnly's revision note). They are named here so the two
// halves of the import constraint — "no third-party module" and "exactly this
// project package" — compare against ONE spelling each rather than repeating the
// strings (Rule 1b/13).
const (
	projectImportPrefix = "github.com/nickhuang99/gopi/"
	segmenterImportPath = "internal/cjkseg"
	segmenterImportFull = projectImportPrefix + segmenterImportPath
)

// repoRoot resolves the repository root from this package's directory
// (internal/memoryprefetch -> ../..).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// TestPackageImportsStdlibOnly pins the zero-dependency constraint on the package
// itself: every import path is a standard-library path (no dot in its first
// element) — which also keeps this package out of the project's import graph.
//
// REVISED, deliberately and with the reason (task 01a115e6): the CJK pass now
// DELEGATES word boundary detection to internal/cjkseg instead of guessing at it
// with a 2-rune sliding window (extract.go's segmentCJKWords). The two properties
// this test protects are therefore split, and BOTH halves are still checked:
//
//  1. NO THIRD-PARTY MODULE may be imported here — that is what "zero dependency"
//     means for this package, and the dot-in-first-element rule below still
//     enforces it exactly. A `github.com/...` import (gse included) is still red.
//  2. The ONE project package it may reach is internal/cjkseg, which is itself a
//     leaf: it imports gse and stdlib, nothing of ours. That is the whole point of
//     the delegation — the segmenter is a package of its own precisely so that the
//     index side and the query side call ONE implementation (Rule 14), and
//     re-implementing it here is the defect this change removed.
//
// A bare allow-list is not the check: the sibling test below
// (TestProjectImportsAreTheSegmenterOnly) pins that the admitted project import is
// exactly cjkseg, so a second one cannot ride in behind this exemption.
func TestPackageImportsStdlibOnly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		scanned++
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", f, perr)
		}
		for _, imp := range file.Imports {
			path, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				t.Fatalf("unquote %s: %v", imp.Path.Value, uerr)
			}
			if strings.HasPrefix(path, projectImportPrefix) {
				continue
			}
			first := path
			if i := strings.Index(path, "/"); i >= 0 {
				first = path[:i]
			}
			if strings.Contains(first, ".") || first == "github.com" {
				t.Errorf("%s imports %q — this package takes no third-party dependency "+
					"(the only project package it may import is %s)", f, path, projectImportPrefix)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned 0 files — the glob is broken")
	}
}

// TestProjectImportsAreTheSegmenterOnly pins the OTHER half of the revision above:
// exactly one project package may be imported, and it is internal/cjkseg. The
// segmenter is shared state whose granularity the memory INDEX is written at, so
// asking it here is the point; a second project import would be new coupling in a
// package whose whole reason to exist is that it is pure logic.
func TestProjectImportsAreTheSegmenterOnly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		scanned++
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", f, perr)
		}
		for _, imp := range file.Imports {
			path, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				t.Fatalf("unquote %s: %v", imp.Path.Value, uerr)
			}
			if !strings.HasPrefix(path, projectImportPrefix) {
				continue
			}
			if path != segmenterImportFull {
				t.Errorf("%s imports %q — the only project package this package may import is %s",
					f, path, segmenterImportFull)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned 0 files — the glob is broken")
	}
}

// TestNoEnvironmentReadsInThePackage pins the Rule 21 stance: this package has no
// env knob at all, so no env-name constant can exist without a reader. A future
// author who adds os.Getenv here fails this test and must route the knob through
// Config (resolved once, by the caller) instead.
func TestNoEnvironmentReadsInThePackage(t *testing.T) {
	const envBanned = "Getenv"
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", f, perr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name == envBanned || sel.Sel.Name == "LookupEnv" {
				t.Errorf("%s calls %s — configuration arrives as an explicit Config (Rule 21)", f, sel.Sel.Name)
			}
			return true
		})
	}
}

// TestNoHardcodedScratchOrTempPaths pins Rule 22's spirit inside this package:
// it creates no temporary path at all, so no /tmp spelling can appear in it.
func TestNoHardcodedScratchOrTempPaths(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, readErr := os.ReadFile(f)
		if readErr != nil {
			t.Fatalf("read %s: %v", f, readErr)
		}
		for _, banned := range []string{tmpRootSpelling, "TempDir", "MkdirTemp"} {
			if strings.Contains(string(data), banned) {
				t.Errorf("%s mentions %q — this package does no IO and creates no temporary path", f, banned)
			}
		}
	}
}

// TestDefaultsAreTheDocumentedValues pins the shipped numbers: a silent retune of
// a budget changes what the feature costs, so it must be visible in a diff.
func TestDefaultsAreTheDocumentedValues(t *testing.T) {
	got := DefaultConfig()
	want := Config{
		MinInputRunes:        24,
		StrongHitCount:       3,
		WeakHitCount:         2,
		WeakInputRunes:       64,
		MaxKeywords:          8,
		MaxCJKCandidateRunes: 8,
		MaxSegmentHits:       5,
		MaxSegmentRunes:      1200,
		MaxHitRunes:          240,
		MinHitRunes:          24,
		// MinInjectHits is the injection floor (config.go): the fewest hits a
		// retrieval must have returned for ANY of it to be injected. It is a
		// shipped number like the rest, and it is listed here for the same
		// reason — a silent retune of it changes what rides into every prompt.
		MinInjectHits: 2,
		ThrottleTypes: 6,
		// MaxTurnPayloadRunes is the TURN-BOUNDARY bound (turn.go): how much of
		// ONE referenced payload may enter a turn's retrieval key. It is listed
		// here with the rest because it is a shipped number like any other —
		// a silent retune of it changes what a pasted turn costs.
		MaxTurnPayloadRunes: 4000,
	}
	if got != want {
		t.Fatalf("DefaultConfig() = %+v, want %+v", got, want)
	}
}
