// cli_tools_maintenance_help_surface_doc_leg_purity_test.go — the two G6 gates
// that grade the INVENTORY DOCUMENT are BINARY-FREE, and that is a property
// this file measures rather than assumes.
//
// WHY IT EXISTS (measured 2026-10-12, task 01a1300b583b7bb08b5ef82223f3c788).
// test-budget run-1791803014-371240 reported 13 cmd/agent failures; eleven of
// them carried the binary-freshness refusal (a binary built from another
// worktree's tree), and the two below carried a different message entirely:
//
//	cli_tools_maintenance_help_surface_basic_test.go:426: `gopi task archive
//	  --unreachable [--repo PATH]` is advertised by the help (archive) but the
//	  inventory does not list it — re-run scripts/g6-…-help-surface.sh …
//	cli_tools_maintenance_help_surface_basic_test.go:945: "gopi task archive
//	  --unreachable [--repo PATH]" (archive) is advertised and NEITHER tested NOR
//	  declared blocked …
//
// Those two lines were attributed to the freshness guard with the other eleven
// ("all 13 are the same artifact-from-another-worktree defect"). They are NOT:
// the resolution to (a) vs (b) — the flags really shipped with a stale table,
// or the help leg silently read another tree's artifact — turns on whether the
// two gates resolve a binary AT ALL. Re-running them in a `git archive` of the
// incident tip reproduces both messages byte for byte while the tip's committed
// inventory genuinely lacks all four census rows, so the answer is (a): a real
// red of that run, and the 13-are-all-guard attribution under-counts.
//
// THE PROPERTY, stated so a later edit cannot quietly remove it: the closure of
// the two gates must contain no path to a binary. A test that reads a checked-in
// Markdown table plus the usage CONSTANTS the help printer itself reads cannot
// be misled by a foreign worktree's build — and the day somebody "improves" one
// of these gates by driving the real binary through a fallback, the same
// misattribution becomes possible again and nothing else in the suite would
// notice.
//
// WHAT IT DOES NOT DO: it never runs a probe, never builds anything and never
// touches the host's load (Rule 27). It parses the package's own sources — the
// same tree the gates read — and reasons about the call graph.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// g6DocLegRoots are the two gates whose subject is the INVENTORY DOCUMENT (the
// table versus the in-process derivation) rather than the live `-h` pages. The
// distinction is the whole point of this gate: the page-grading siblings
// (`…PagesComeFromTheirOwnConstant`, the `g6Cases` table) DO drive the binary
// through `gopiTestBinPath`, which is correct for them and forbidden here.
var g6DocLegRoots = []string{
	"TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation",
	"TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason",
}

// g6BinaryResolutionNames are the call names that put a BINARY between the gate
// and its subject. `exec.Command` is included because a hand-rolled invocation
// is the same defect with the resolution spelled out inline (Rule 13: the
// precedence lives in ONE helper, and these gates must not go around it).
var g6BinaryResolutionNames = []string{
	"gopiTestBinPath",
	"resolveGopiTestBin",
	"CoverBin",
	"resolveGopiCoverBin",
	"GopiBin",
	"GopiBinOptional",
	"exec.Command",
}

// g6EnvReadNames are the ambient channels a binary path arrives through; a
// gate with no binary has no reason to read one.
var g6EnvReadNames = []string{
	"os.Getenv",
	"os.LookupEnv",
}

// TestG6InventoryGatesResolveNoBinary walks the call graph from each root and
// fails when any reachable function names a binary-resolution or env call.
func TestG6InventoryGatesResolveNoBinary(t *testing.T) {
	paths, err := g6TestGoFiles()
	if err != nil {
		t.Fatalf("glob the package's test files: %v", err)
	}
	if len(paths) < 100 {
		t.Fatalf("only %d *_test.go files visible in %s — the scan is reading blind, so a green run would prove nothing",
			len(paths), ".")
	}
	bodies := map[string][]ast.Node{}
	fset := token.NewFileSet()
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			bodies[fd.Name.Name] = []ast.Node{fd.Body}
		}
	}

	for _, root := range g6DocLegRoots {
		if _, ok := bodies[root]; !ok {
			t.Errorf("%s is not declared in this package — this gate is scanning for a test that no longer exists, "+
				"so its verdict about that gate would be vacuous (if the test was renamed, move this root with it)", root)
			continue
		}
		seen := map[string]bool{root: true}
		queue := []string{root}
		for len(queue) > 0 {
			name := queue[0]
			queue = queue[1:]
			nodes := bodies[name]
			if nodes == nil {
				continue // a call to something outside this package (or a builtin)
			}
			offenders := g6BinaryRefsIn(nodes[0])
			for _, offender := range offenders {
				t.Errorf("%s reaches %s (via %s) — the inventory gates must grade the checked-in table and the "+
					"usage CONSTANTS only: a binary in this closure is exactly the seam through which "+
					"test-budget run-1791803014-371240's two failures could be attributed to a foreign "+
					"worktree's artifact instead of to the stale table they actually reported", root, offender, name)
			}
			for _, callee := range g6LocalCallees(nodes[0], bodies) {
				if !seen[callee] {
					seen[callee] = true
					queue = append(queue, callee)
				}
			}
		}
		if len(seen) < 2 {
			t.Errorf("%s reaches %d function(s) — a closure that size means the walk is broken, not that the gate is pure",
				root, len(seen))
		}
	}
}

// g6BinaryRefsIn returns the binary-resolution / env call names spelled anywhere
// in one body, sorted (a stable failure message across Go versions).
func g6BinaryRefsIn(body ast.Node) []string {
	want := append(append([]string{}, g6BinaryResolutionNames...), g6EnvReadNames...)
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := g6CallName(call.Fun)
		for _, w := range want {
			if name == w {
				out = append(out, name)
			}
		}
		return true
	})
	sort.Strings(out)
	return out
}

// g6CallName renders a call target the way the ban list spells it: `f` for a
// bare identifier, `pkg.F` for a selector, "" for a call through a value (which
// this walk cannot resolve by name — the shape is reported by the closure
// floor instead of guessed at).
func g6CallName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if pkg, ok := f.X.(*ast.Ident); ok {
			return pkg.Name + "." + f.Sel.Name
		}
	}
	return ""
}

// g6LocalCallees returns the package-local functions one body calls — the edges
// of the call graph this gate walks. A call to an identifier that is not a
// declared function in the package (a closure variable, a parameter) is skipped:
// an indirect call is a real hole in the walk, and g6BinaryRefsIn still catches
// a banned name spelled in the body itself.
func g6LocalCallees(body ast.Node, bodies map[string][]ast.Node) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name := g6CallName(call.Fun); name != "" && !strings.Contains(name, ".") {
			if _, declared := bodies[name]; declared {
				out = append(out, name)
			}
		}
		return true
	})
	return out
}

// g6TestGoFiles lists the package's *_test.go files. It is a named helper rather
// than an inline call so the scan's blind-run floor has exactly one reading.
func g6TestGoFiles() ([]string, error) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no *_test.go files in %q", ".")
	}
	return out, nil
}
