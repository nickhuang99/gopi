package agent

// test_entry_point_description_gate_test.go — the two tool descriptions that
// LAUNCH a test suite must name this repo's DECLARED test entry point.
//
// WHY THIS GATE EXISTS (Rule 5: every fix ships its regression guard).
// Measured 2026-10-01 (devdoc/overlapping-test-runs-diagnosis.md, task
// 01a0f3f72a0173e4a44548a3a8accee2): one run issued two overlapping `go test`
// sets (~2 min of true concurrency, `./internal/standards/` in both package
// lists; one of the two killed by the idle-output net having printed nothing)
// and BOTH were BARE `go test`. `internal/smokegate` already names the declared
// entry point — the Makefile's smoke target, which routes through
// scripts/test-budget.sh (per-package budget, heavy/smoke skip ledger, per-run
// stream + scope check) — but nothing carried it into the tool descriptions
// that launch a suite, and the description is a tool's ONLY persuasion
// mechanism (PROJECT.md Rule 11). The fix put the entry point in the `bash` and
// `job` descriptions; this test is what keeps a later editor from dropping it.
//
// WHAT IT READS, AND WHY THE AST RATHER THAN A GREP. A grep for the command
// would pass on any file that merely MENTIONS it (a comment, a doc, an
// unrelated description) — the shape this gate must not accept. So it reads the
// real `claudeToolDefs` composite literal and evaluates each description's
// expression with the smokegate constants' VALUES substituted in, exactly as
// the registration site does. The literals are then allowed to change freely —
// what is pinned is the RENDERED description, which is the thing the model
// reads (Rule 13: the command is referenced from the package that defines it,
// never retyped in the description; Rule 14: one spelling of the note, shared
// by both tools).
//
// The gate has NO permit list, so the drift it describes fails on the commit
// that introduces it.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/smokegate"
	"github.com/nick/gopi/internal/toolnames"
)

const (
	// testEntryPointToolDefFile is the SSOT of the tool definitions (GOPI.md
	// Rule 3: claudeToolDefs), read as source rather than through the compiled
	// value so the LITERAL SHAPE is what is judged.
	testEntryPointToolDefFile = "loop.go"
	// testEntryPointToolDefsVar is the slice the gate evaluates.
	testEntryPointToolDefsVar = "claudeToolDefs"
	// testEntryPointNoteIdent is the identifier every launch-a-suite description
	// must reference: one constant, so both descriptions cannot drift apart.
	testEntryPointNoteIdent = "bashJobTestEntryPointNote"
	// testEntryPointMinTools is the non-vacuity floor: the two tools that launch
	// a test suite (bash, job) and no fewer — a slice that stopped parsing would
	// otherwise report a clean green.
	testEntryPointMinTools = 2
)

// testEntryPointSmokegateEnv seeds the evaluator with the constants' REAL values
// (Rule 14: the values live in smokegate; this test never spells them).
func testEntryPointSmokegateEnv() map[string]string {
	return map[string]string{
		"smokegate.Command":         smokegate.Command,
		"smokegate.ScopedCommand":   smokegate.ScopedCommand,
		"smokegate.WholeTreeNeedle": smokegate.WholeTreeNeedle,
		"smokegate.Doc":             smokegate.Doc,
	}
}

// testEntryPointEval renders one expression the way the Go compiler would, for
// the subset of shapes the descriptions use: string literals, identifiers from
// env, `+` concatenation, and parentheses. Any other shape returns ok=false so a
// rewrite this test cannot read is reported instead of silently passing.
func testEntryPointEval(expr ast.Expr, env map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return v, true
	case *ast.Ident:
		v, ok := env[e.Name]
		return v, ok
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		v, ok := env[pkg.Name+"."+e.Sel.Name]
		return v, ok
	case *ast.ParenExpr:
		return testEntryPointEval(e.X, env)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, ok := testEntryPointEval(e.X, env)
		if !ok {
			return "", false
		}
		r, ok := testEntryPointEval(e.Y, env)
		if !ok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}

// testEntryPointIdentRefs counts references to ident anywhere in expr — the
// "this description carries the shared note" half of the gate.
func testEntryPointIdentRefs(expr ast.Expr, ident string) int {
	n := 0
	ast.Inspect(expr, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok && id.Name == ident {
			n++
		}
		return true
	})
	return n
}

// testEntryPointToolDefsLit returns the composite literal assigned to the
// claudeToolDefs variable, or nil.
func testEntryPointToolDefsLit(f *ast.File) *ast.CompositeLit {
	var found *ast.CompositeLit
	ast.Inspect(f, func(n ast.Node) bool {
		decl, ok := n.(*ast.GenDecl)
		if !ok || decl.Tok != token.VAR {
			return true
		}
		for _, spec := range decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != testEntryPointToolDefsVar || i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.CompositeLit); ok {
					found = lit
				}
			}
		}
		return true
	})
	return found
}

// testEntryPointKeyedExpr returns the value expression of the `key: value` pair
// inside one struct literal, or nil.
func testEntryPointKeyedExpr(lit *ast.CompositeLit, key string) ast.Expr {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if id, ok := kv.Key.(*ast.Ident); ok && id.Name == key {
			return kv.Value
		}
	}
	return nil
}

// testEntryPointDefName reads a tool def's `Name:` value as a rendered string
// (e.g. toolnames.ToolBash → "bash").
func testEntryPointDefName(lit *ast.CompositeLit) (string, bool) {
	expr := testEntryPointKeyedExpr(lit, "Name")
	if expr == nil {
		return "", false
	}
	env := map[string]string{
		"toolnames.ToolBash": toolnames.ToolBash,
		"toolnames.ToolJob":  toolnames.ToolJob,
	}
	return testEntryPointEval(expr, env)
}

// testEntryPointNoteValue evaluates the shared note constant's declaration in
// the parsed file.
func testEntryPointNoteValue(f *ast.File) (string, bool) {
	var (
		val string
		ok  bool
	)
	ast.Inspect(f, func(n ast.Node) bool {
		decl, isDecl := n.(*ast.GenDecl)
		if !isDecl || decl.Tok != token.VAR {
			return true
		}
		for _, spec := range decl.Specs {
			vs, isVal := spec.(*ast.ValueSpec)
			if !isVal {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != testEntryPointNoteIdent || i >= len(vs.Values) {
					continue
				}
				val, ok = testEntryPointEval(vs.Values[i], testEntryPointSmokegateEnv())
			}
		}
		return true
	})
	return val, ok
}

// testEntryPointLaunchSuiteDescriptions parses loop.go and returns the rendered
// description of every tool def named in `wanted`, keyed by tool name.
func testEntryPointLaunchSuiteDescriptions(t *testing.T, wanted []string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, testEntryPointToolDefFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", testEntryPointToolDefFile, err)
	}

	note, ok := testEntryPointNoteValue(f)
	if !ok {
		t.Fatalf("%s: no var declaration for %q (or its expression is not a readable string concatenation) "+
			"— the shared note is the ONE spelling both descriptions carry", testEntryPointToolDefFile, testEntryPointNoteIdent)
	}

	env := testEntryPointSmokegateEnv()
	env[testEntryPointNoteIdent] = note

	out := map[string]string{}
	// Descend from the claudeToolDefs declaration ONLY: another composite
	// literal in this file may carry a `Name: "job"` (the system-prompt section
	// registry does), and a gate that wandered into it would judge a section
	// header as if it were a tool description.
	slice := testEntryPointToolDefsLit(f)
	if slice == nil {
		t.Fatalf("%s: no %s declaration found — the gate would judge nothing", testEntryPointToolDefFile, testEntryPointToolDefsVar)
	}
	ast.Inspect(slice, func(n ast.Node) bool {
		lit, isLit := n.(*ast.CompositeLit)
		if !isLit {
			return true
		}
		name, named := testEntryPointDefName(lit)
		if !named {
			return true
		}
		for _, w := range wanted {
			if name != w {
				continue
			}
			expr := testEntryPointKeyedExpr(lit, "Description")
			if expr == nil {
				t.Errorf("tool %q has no Description literal", name)
				continue
			}
			if refs := testEntryPointIdentRefs(expr, testEntryPointNoteIdent); refs != 1 {
				t.Errorf("tool %q's Description references %s %d times, want exactly 1 — the entry-point note "+
					"must ride the shared constant, not a hand-copied sentence", name, testEntryPointNoteIdent, refs)
			}
			rendered, ok := testEntryPointEval(expr, env)
			if !ok {
				t.Errorf("tool %q's Description is not a readable string concatenation — this gate cannot judge it, "+
					"so a dropped entry point would pass", name)
				continue
			}
			out[name] = rendered
		}
		return true
	})
	return out
}

// TestLaunchSuiteToolsNameTheDeclaredTestEntryPoint is the gate: the `bash` and
// `job` descriptions must both name the declared entry point (smokegate.Command
// and its scoped form), and must both do it through the ONE shared constant.
func TestLaunchSuiteToolsNameTheDeclaredTestEntryPoint(t *testing.T) {
	wanted := []string{toolnames.ToolBash, toolnames.ToolJob}
	got := testEntryPointLaunchSuiteDescriptions(t, wanted)
	if len(got) < testEntryPointMinTools {
		t.Fatalf("read %d of the %d launch-a-suite tool definitions from %s — a slice this gate cannot see "+
			"would report a clean green while judging nothing", len(got), testEntryPointMinTools, testEntryPointToolDefFile)
	}

	for _, name := range wanted {
		desc, ok := got[name]
		if !ok {
			t.Errorf("tool %q was not found in %s", name, testEntryPointToolDefFile)
			continue
		}
		for _, must := range []string{smokegate.Command, smokegate.ScopedCommand} {
			if !strings.Contains(desc, must) {
				t.Errorf("the %q tool description does not name the declared test entry point %q.\n"+
					"  A bare `go test` from this tool bypasses the per-package budget, the heavy/smoke skip "+
					"ledger and the per-run stream + scope check, and it BUFFERS PER PACKAGE — the observed "+
					"incident's second half (killed by the idle-output net having printed nothing).\n"+
					"  Fix: keep %s in %s's Description (%s).",
					name, must, testEntryPointNoteIdent, testEntryPointToolDefFile, smokegate.Doc)
			}
		}
	}
}

// TestEntryPointDescriptionGateRedProof proves the gate's predicate reports the
// shape it exists for and stays silent on the shape it must not accept: a
// description that names the entry point only in PROSE (a hand-typed sentence)
// or not at all is exactly what a later edit would produce.
func TestEntryPointDescriptionGateRedProof(t *testing.T) {
	env := testEntryPointSmokegateEnv()

	// The real note renders both commands — the GREEN half, evaluated from the
	// same expression the registration site uses.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, testEntryPointToolDefFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", testEntryPointToolDefFile, err)
	}
	note, ok := testEntryPointNoteValue(f)
	if !ok {
		t.Fatalf("%s: %s is not readable as a string expression", testEntryPointToolDefFile, testEntryPointNoteIdent)
	}
	for _, must := range []string{smokegate.Command, smokegate.ScopedCommand} {
		if !strings.Contains(note, must) {
			t.Fatalf("RED PROOF FAILED: the shared note does not render %q — the gate above would be vacuous", must)
		}
	}

	// RED: the shape the incident's tools had — a description with no entry
	// point at all. A grep-based gate would also pass on the second probe below;
	// this one must not.
	redFile := "package p\n\n" +
		"var probe = []T{{\n" +
		"\t{Name: \"bash\", Description: \"Execute a shell command via bash -c for build, test, run.\"},\n" +
		"}}\n"
	if strings.Contains(redFile, smokegate.Command) {
		t.Fatalf("RED PROOF is mis-built: the probe already carries %q", smokegate.Command)
	}

	// RED (prose): the entry point mentioned in a sentence but NOT through the
	// shared constant — the edit a future author would most plausibly make.
	prose := "\"run the repo's declared test entry point (the make smoke target), not a bare go test\""
	v, ok := testEntryPointEval(mustParseExpr(t, prose), env)
	if !ok {
		t.Fatalf("RED PROOF: the prose probe did not evaluate")
	}
	if strings.Contains(v, smokegate.Command) {
		t.Fatalf("RED PROOF is mis-built: the prose probe renders %q", smokegate.Command)
	}

	// RED (bare literal): a description that spells the scoped command as its own
	// string rather than referencing the constant — readable, but the ONE
	// spelling check (exactly one reference to the shared ident) is what rejects
	// the copy that would then drift.
	bare := "package p\n\n" +
		"var probe = []T{{\n" +
		"\t{Name: \"bash\", Description: \"use make test-budget PKGS='<pkg>' not go test\"},\n" +
		"}}\n"
	bfset := token.NewFileSet()
	bf, err := parser.ParseFile(bfset, "probe.go", bare, 0)
	if err != nil {
		t.Fatalf("parse probe: %v", err)
	}
	refs := 0
	ast.Inspect(bf, func(n ast.Node) bool {
		if lit, isLit := n.(*ast.CompositeLit); isLit {
			if expr := testEntryPointKeyedExpr(lit, "Description"); expr != nil {
				refs += testEntryPointIdentRefs(expr, testEntryPointNoteIdent)
			}
		}
		return true
	})
	if refs != 0 {
		t.Fatalf("RED PROOF: a hand-typed description reported %d references to %s, want 0", refs, testEntryPointNoteIdent)
	}
}

// mustParseExpr parses one Go expression from src, for the red proof.
func mustParseExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("parse expr %q: %v", src, err)
	}
	return expr
}
