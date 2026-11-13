package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// scratchGCArmConstName is the naming convention this package uses for an ARM
// SELECTOR: a `scratchGC…Flag` constant whose VALUE is a bare word (the arm
// name an operator types, e.g. "base", "trees", "tick") rather than a flag
// spelling ("--dry-run"). It is the shape that can lie about a capability: a
// bare-word constant reading "x11" implies `scratch-gc x11` works, while a
// "--dry-run" constant only names a flag the parser visibly handles.
var scratchGCArmConstName = regexp.MustCompile(`^scratchGC[A-Za-z0-9]*Flag$`)

// scratchGCArmBareWord matches an arm VALUE: letters and digits only, never a
// leading dash. A "--name" value is a FLAG spelling and is out of this test's
// scope (a flag is only meaningful next to the parser that rejects it).
var scratchGCArmBareWord = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// scratchGCArmReaderFloor is the minimum number of arm constants this scan must
// find before it is allowed to pass. A scan that found nothing must be
// distinguishable from a clean one (Rule 15): if the naming convention or the
// file ever moves, this test goes red instead of silently judging an empty set.
const scratchGCArmReaderFloor = 5

// TestScratchGCArmConstantsHaveReaders is the regression guard for the defect
// task 01a18f80 removed: a constant `scratchGCX11Flag = "x11"` sat beside
// `scratchGCBaseFlag`, under a comment claiming "the parser, the usage text and
// the tick arm reference these constants" — the base arm really had all three,
// and the x11 arm had NONE (no `case scratchGCX11Flag`, no usage line, no
// sweeper in internal/scratch), so the comment asserted a capability that could
// not exist and the constant was unreachable.
//
// The rule this pins: every arm selector constant must have at least one REAL
// reader in production code (outside its own definition), anywhere in the
// module. "Real reader" is decided by counting occurrences of the IDENTIFIER —
// not by grepping for its VALUE: a bare `"base"` string literal somewhere would
// be a mirrored spelling (Rule 1b's defect), not a reference to this constant.
func TestScratchGCArmConstantsHaveReaders(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot, err := moduleRoot(pkgDir)
	if err != nil {
		t.Fatalf("locate module root from %s: %v", pkgDir, err)
	}

	index, err := productionReferences(repoRoot)
	if err != nil {
		t.Fatalf("index production references under %s: %v", repoRoot, err)
	}

	var arms []string
	for name := range index.arms {
		arms = append(arms, name)
	}
	sort.Strings(arms)
	fmt.Printf("TestScratchGCArmConstantsHaveReaders: %d arm selector(s) declared in %s: %v\n",
		len(arms), filepath.Base(scratchGCArmDeclFile), arms)
	if len(arms) < scratchGCArmReaderFloor {
		t.Fatalf("scan found only %d arm constant(s) (%v) — the naming convention or %s moved, so this test would judge an empty set and call it a pass",
			len(arms), arms, filepath.Base(scratchGCArmDeclFile))
	}

	for _, arm := range arms {
		refs := index.readers[arm]
		if len(refs) == 0 {
			t.Errorf("arm constant %s is declared at %s and has NO production reader outside its own definition — an arm selector nothing dispatches is an unreachable capability, and the comment beside it will describe that capability as working.\nWire it (parser case + usage line + internal/scratch leg), or delete it and record the retirement in the comment block (the shape this guard was written after).",
				arm, index.arms[arm])
			continue
		}
		fmt.Printf("TestScratchGCArmConstantsHaveReaders: %s → %d production reader(s): %s\n",
			arm, len(refs), strings.Join(refs, ", "))
	}
}

// scratchGCArmIndex is the scan result: where each arm constant is DECLARED and
// which production sites REFERENCE it (the definition itself excluded).
type scratchGCArmIndex struct {
	arms    map[string]string   // arm name → declaration site (file:line)
	readers map[string][]string // arm name → reference sites (file:line)
}

// scratchGCArmDeclFile is the ONE file that declares the arm selectors; it is
// scanned like any other so its non-declaration references still count.
const scratchGCArmDeclFile = "cli_scratch_gc.go"

// productionReferences walks every non-test Go file under the module and builds
// the arm index from IDENTIFIER occurrences. Non-test only: a constant kept
// alive purely by a test is still unwired in production — that is precisely the
// half of the defect a test-only reference would have hidden.
func productionReferences(repoRoot string) (*scratchGCArmIndex, error) {
	index := &scratchGCArmIndex{arms: map[string]string{}, readers: map[string][]string{}}
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(repoRoot, path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			// A file that fails to parse is NOT silently skipped (Rule 15): it
			// may be the one holding the reader.
			return fmt.Errorf("parse %s: %w", rel, perr)
		}

		// Pass 1: the declaration identifiers, and whether each declares a
		// BARE-WORD arm value. `spec.Names` are their own references in the AST,
		// so they are collected here and excluded from pass 2 by position.
		declared := map[*ast.Ident]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, id := range spec.Names {
				declared[id] = true
				if i >= len(spec.Values) || !scratchGCArmConstName.MatchString(id.Name) {
					continue
				}
				lit, ok := spec.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, uerr := strconv.Unquote(lit.Value)
				if uerr != nil || !scratchGCArmBareWord.MatchString(val) {
					continue
				}
				index.arms[id.Name] = fmt.Sprintf("%s:%d", rel, fset.Position(id.Pos()).Line)
			}
			return true
		})

		// Pass 2: every other occurrence is a reference.
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || declared[id] || !scratchGCArmConstName.MatchString(id.Name) {
				return true
			}
			index.readers[id.Name] = append(index.readers[id.Name],
				fmt.Sprintf("%s:%d", rel, fset.Position(id.Pos()).Line))
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return index, nil
}

// moduleRoot walks up from dir to the directory holding go.mod.
func moduleRoot(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}
