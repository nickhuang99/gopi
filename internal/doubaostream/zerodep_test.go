package doubaostream

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// ── the zero-dependency gate ────────────────────────────────────────────
//
// WHY THIS TEST EXISTS: the whole reason this package is hand-written (an HTTP
// upgrade, masked frames, ping/pong, gzip) is that gopi's binary must not grow
// a dependency — the official demo for this channel imports
// github.com/gorilla/websocket and github.com/satori/go.uuid. A prose promise
// ("pure stdlib") rots the first time somebody reaches for a websocket library,
// so the constraint is CHECKED.
//
// The predicate is deterministic and needs no external tool: every import of
// every PRODUCTION file of this package must be a standard-library path. A
// stdlib import path has no dot in its first segment; a third-party one always
// has (github.com/…, golang.org/x/…, modernc.org/…). The scan reads the files
// directly (no `go list`, no build cache, no network) and has a READ FLOOR, so
// a broken glob cannot pass vacuously.
func TestZeroDependencyStdlibOnlyImports(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, imp := range parseImports(t, name, string(src)) {
			if strings.Contains(firstSegment(imp), ".") {
				t.Errorf("%s imports %q — this package must stay STANDARD LIBRARY ONLY "+
					"(the zero-dependency constraint is the reason it exists; see the package doc)",
					name, imp)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("read floor: no production .go files found in this package — the scan proved nothing")
	}
}

// firstSegment returns the first path element of an import path.
func firstSegment(imp string) string {
	if i := strings.IndexByte(imp, '/'); i >= 0 {
		return imp[:i]
	}
	return imp
}

// TestParseImportsSeesEveryImportForm is the guard for the hole a line-scanning
// implementation has: a single-line `import _ "x"` and a parenthesised block
// must both be seen, or the gate above can be defeated by formatting.
func TestParseImportsSeesEveryImportForm(t *testing.T) {
	imports := parseImports(t, "x.go", "package x\n\nimport _ \"modernc.org/sqlite\"\n")
	if len(imports) != 1 || imports[0] != "modernc.org/sqlite" {
		t.Fatalf("single-line import not seen: %v", imports)
	}
	if !strings.Contains(firstSegment(imports[0]), ".") {
		t.Errorf("firstSegment(%q) = %q, want a dotted segment", imports[0], firstSegment(imports[0]))
	}
	imports = parseImports(t, "y.go", "package y\n\nimport (\n\t\"fmt\"\n\t_ \"github.com/gorilla/websocket\"\n)\n")
	if len(imports) != 2 {
		t.Fatalf("block import not fully seen: %v", imports)
	}
	imports = parseImports(t, "z.go", "package z\n\nimport \"golang.org/x/net/websocket\" // the alternative dependency\n")
	if len(imports) != 1 || imports[0] != "golang.org/x/net/websocket" {
		t.Fatalf("trailing-comment import not seen: %v", imports)
	}
}

// parseImports reads one Go source and returns every import path it names, by
// parsing rather than by scanning lines.
func parseImports(t *testing.T, name, src string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	out := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		out = append(out, strings.Trim(imp.Path.Value, `"`))
	}
	return out
}
