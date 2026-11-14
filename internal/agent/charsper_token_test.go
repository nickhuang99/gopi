package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// TestEstimateTokensUsesCharsPerToken pins the chars/4 heuristic to the named
// constant (PROJECT.md Rule 1/13): the divisor is declared once and the formula
// is expressed through it, so a re-inlined literal is a compile-visible
// divergence rather than a silently equivalent number.
func TestEstimateTokensUsesCharsPerToken(t *testing.T) {
	if charsPerToken != 4 {
		t.Fatalf("charsPerToken = %d, want 4 — the heuristic documented in EstimateTokens", charsPerToken)
	}
	msg := deepseek.Message{Role: "user", Content: strings.Repeat("a", 400)}
	want := (400+len("user")+2)/charsPerToken + 1
	if got := EstimateTokens(msg); got != want {
		t.Fatalf("EstimateTokens = %d, want %d (charsPerToken=%d)", got, want, charsPerToken)
	}
}

// TestRule13_NoBareCharsPerTokenLiteral is the anti-regression half: the token
// heuristic may not reappear as a division by a bare 4 in this package's
// production code (Rule 13 — a constant that is defined but bypassed is a
// defect).
//
// The check walks the AST, which makes it comment-blind BY CONSTRUCTION: the
// constant's own doc comment quotes the heuristic it replaces, and an
// instrument that flags the explanation teaches the next reader to delete the
// explanation instead of keeping the reference. Comments never reach the AST,
// so only real code is judged.
func TestRule13_NoBareCharsPerTokenLiteral(t *testing.T) {
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
			be, ok := n.(*ast.BinaryExpr)
			if !ok || be.Op != token.QUO {
				return true
			}
			lit, ok := be.Y.(*ast.BasicLit)
			if !ok || lit.Kind != token.INT {
				return true
			}
			if v, cerr := strconv.Atoi(lit.Value); cerr == nil && v == charsPerToken {
				t.Errorf("%s:%d divides by the bare %s literal — reference charsPerToken (Rule 13)",
					f, fset.Position(be.Pos()).Line, lit.Value)
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("scanned 0 production files — the glob is broken, this gate reads green-blind")
	}
}
