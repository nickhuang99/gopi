package tools

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

// TestWriteSuggestions_RespectNamedLimits pins the directory-suggestion hint to
// the named constants (PROJECT.md Rule 1): at most maxPathSuggestions hints, each
// matched by the suggestionPrefixLen-character prefix. The fixture has FIVE
// prefix-matching directories and one non-matching one, so both the cap and
// the prefix match are observable from the error text.
func TestWriteSuggestions_RespectNamedLimits(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}
	base := t.TempDir()
	matching := []string{"target-1", "target-2", "target-3", "target-4", "target-5"}
	for _, d := range append(append([]string{}, matching...), "unrelated") {
		if err := os.Mkdir(filepath.Join(base, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(base, 0o555); err != nil { // MkdirAll below now fails
		t.Fatal(err)
	}
	//stylegate:ignore sets a fixture file's mode; the mode read back is the assertion
	defer func() { _ = os.Chmod(base, 0o755) }()

	// "target-9" does not exist → MkdirAll fails → the hint path runs.
	err := Write(filepath.Join(base, "target-9", "x.txt"), "content")
	if err == nil {
		t.Fatal("expected an error creating a directory under a read-only parent")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Similar directories") {
		t.Fatalf("expected a similar-directory hint, got: %v", msg)
	}
	listed := 0
	for _, d := range matching {
		if strings.Contains(msg, d) {
			listed++
		}
	}
	if listed == 0 {
		t.Fatalf("expected prefix-matched suggestions, got: %v", msg)
	}
	if listed > maxPathSuggestions {
		t.Fatalf("%d suggestions listed, want at most maxPathSuggestions=%d: %v",
			listed, maxPathSuggestions, msg)
	}
	if strings.Contains(msg, "unrelated") {
		t.Fatalf("a non-prefix directory must not be suggested (suggestionPrefixLen=%d): %v",
			suggestionPrefixLen, msg)
	}
	if suggestionPrefixLen >= len("target-9") {
		t.Fatalf("fixture assumes a prefix shorter than the name, got suggestionPrefixLen=%d",
			suggestionPrefixLen)
	}
}

// TestRule13_NoBareBufferLiteral is the anti-regression half of the Rule-13
// backfill: the bash pipe read buffer size may not reappear as a bare size
// argument in this package's production code (the constant exists to be
// referenced, not bypassed).
//
// The check walks the AST — comment-blind by construction, so the constant's own
// doc comment may quote the literal it replaces without tripping the gate.
func TestRule13_NoBareBufferLiteral(t *testing.T) {
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
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			// Match `make([]byte, <int literal>)`.
			id, isID := call.Fun.(*ast.Ident)
			if !isID || id.Name != "make" {
				return true
			}
			at, ok := call.Args[0].(*ast.ArrayType)
			if !ok {
				return true
			}
			if elt, isID := at.Elt.(*ast.Ident); !isID || elt.Name != "byte" {
				return true
			}
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.INT {
				return true
			}
			if v, cerr := strconv.Atoi(lit.Value); cerr == nil && v == pipeReadBufferSize {
				t.Errorf("%s:%d makes a %s-byte buffer with a bare literal — reference pipeReadBufferSize (Rule 13)",
					f, fset.Position(call.Pos()).Line, lit.Value)
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("scanned 0 production files — the glob is broken, this gate reads green-blind")
	}
}

// TestPipeReadBufferSize documents the constant's value: 4KB is the intended
// line-oriented read size, so a silent retune of the value (which would change
// memory per concurrent command) is visible here.
func TestPipeReadBufferSize(t *testing.T) {
	if pipeReadBufferSize != 4096 {
		t.Fatalf("pipeReadBufferSize = %d, want 4096", pipeReadBufferSize)
	}
}
