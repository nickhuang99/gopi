// tool_dispatch_coverage_test.go — the DEFS ↔ DISPATCHER edge gate.
//
// Rule 3 already had a gate for one of its two edges: TestGOPIRule3_
// MatchesClaudeToolDefs pins GOPI.md's tool list against claudeToolDefs. The
// other edge — every advertised tool actually HAVING a dispatch arm — was
// gated by nothing, and the defect that motivated this file is exactly that
// gap: screen_capture was advertised to the model (a claudeToolDefs entry,
// listed in GOPI.md) while executeToolDefault's `switch tc.Name` had no case
// for it, so every call returned "Unknown tool: screen_capture with args {…}"
// (30 such strings across 9 session logs, live at master 812f9a60). The model
// saw a tool it could not use; nothing in the build noticed.
//
// The check is deliberately STRUCTURAL (a source-level scan of loop.go's
// dispatch switch), not behavioural: a behavioural test can only prove the
// arms it happens to exercise, while this one enumerates every claudeToolDefs
// name and demands a case label for each — and, symmetrically, that no case
// label claims a name the model was never told about.
//
// The two directions are checked against the MERGED-FAMILY ACTION VERBS by
// exclusion: `task` / `job` / `memory` dispatch on their `action` argument with
// nested `case toolnames.ToolTaskCommit:`-style labels that ARE case labels in
// the same switch but are NOT tool names (no model-facing def carries them).
// They are subtracted from the label set before the reverse direction is
// checked, and the subtraction is itself asserted (see the anchors below) so a
// parser that silently stops seeing labels cannot pass by finding nothing.
package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// dispatchSourceFile is the file holding executeToolDefault's dispatch switch.
const dispatchSourceFile = "loop.go"

// dispatchSwitchSelector is the selector expression of the switch whose cases
// are the per-tool dispatch arms (the TOOL-LEVEL switch — nested action
// switches select a different expression and are excluded by construction).
const dispatchSwitchSelector = "tc.Name"

// toolnamesPackageDir is the package directory holding the tool-name
// constants the case labels are written with.
const toolnamesPackageDir = "../toolnames"

// dispatchCoverageReadFloor is the minimum number of tool-level case labels
// the scan must find. It is a read-floor, not an exact expectation: if the
// parser or the switch's shape changes so the scan comes back (near-)empty,
// the gate fails LOUDLY instead of certifying an empty set (a scan that reads
// green-blind is worse than no gate).
const dispatchCoverageReadFloor = 15

// TestGate_ToolDispatchCoversEveryDef is the edge gate: every tool the model is
// advertised (claudeToolDefs[i].Name) must be dispatched, and every dispatched
// tool name must be advertised. It FAILS on pre-fix code — screen_capture is in
// the defs with no arm — and passes once the arm exists.
func TestGate_ToolDispatchCoversEveryDef(t *testing.T) {
	labels := dispatcherCaseLabels(t)
	toolNames := toolnameConstantValues(t)

	dispatched := map[string]bool{}
	for _, expr := range labels {
		switch expr.kind {
		case exprConst:
			// toolnames.ToolX → its wire value.
			v, ok := toolNames[expr.ident]
			if !ok {
				t.Errorf("%s: dispatch case uses toolnames.%s, which is not a known tool-name constant "+
					"(a label must resolve to a wire name — Rule 1b)", dispatchSourceFile, expr.ident)
				continue
			}
			dispatched[v] = true
		case exprBare:
			// A bare string case label in the dispatch switch is a Rule 1b
			// defect regardless of whether it covers a def.
			t.Errorf("%s: dispatch case uses the bare string %q — write toolnames.<Const> (Rule 1b/13)",
				dispatchSourceFile, expr.ident)
		}
	}

	// Direction 1 — defs → dispatcher. This is the edge the epic measured:
	// an advertised tool with no arm is a tool the model cannot use. BOTH wires
	// are checked: a chat-scoped def with no arm is exactly as dead as an
	// ordinary one, and it is the harder case to notice because it is advertised
	// on only one of the two.
	for _, def := range append(append([]deepseek.ClaudeToolDef{}, claudeToolDefs...), chatScopedToolDefs(nil)...) {
		if def.Name == "" {
			continue
		}
		if !dispatched[def.Name] {
			t.Errorf("the tool wire advertises %q but executeToolDefault's `switch %s` has no case for it — "+
				"every call returns %q (the def↔dispatcher edge is a Rule 3 contract)",
				def.Name, dispatchSwitchSelector, "Unknown tool: "+def.Name)
		}
	}

	// The two wires are DISJOINT: a def that appears in both is sent twice on a
	// chat turn (the provider rejects a duplicate tool name), and the difference
	// between the wires is the whole reason chatScopedToolDefs exists.
	ordinary := map[string]bool{}
	for _, def := range claudeToolDefs {
		ordinary[def.Name] = true
	}
	for _, def := range chatScopedToolDefs(nil) {
		if ordinary[def.Name] {
			t.Errorf("%q is in claudeToolDefs AND in chatScopedToolDefs — a chat turn would advertise it twice; "+
				"a chat-scoped def belongs on the chat wire only", def.Name)
		}
	}

	// Direction 2 — dispatcher → defs. A case label the model was never told
	// about is unreachable code (or a def was deleted). Action verbs of the
	// merged families are subtracted first: they are nested action switches,
	// not tool names.
	//
	// BOTH WIRES COUNT AS ADVERTISED. gopi has two tool-def surfaces on purpose
	// (Rule 3): claudeToolDefs is the ORDINARY one, and the chat wire appends its
	// own scoped defs (chatScopedToolDefs) when the surface behind them is
	// installed. A chat-scoped tool has a real dispatch arm and a real def — it is
	// simply not sent to the task path. Reading only claudeToolDefs here would
	// report that arm as dead code, which is the opposite of the truth.
	advertised := map[string]bool{}
	for _, def := range claudeToolDefs {
		advertised[def.Name] = true
	}
	for _, def := range chatScopedToolDefs(nil) {
		advertised[def.Name] = true
	}
	actionVerbs := mergedFamilyActionVerbs(t, toolNames)
	for name := range dispatched {
		if advertised[name] || actionVerbs[name] {
			continue
		}
		t.Errorf("executeToolDefault dispatches %q but no claudeToolDefs entry advertises it — "+
			"the model can never emit it (dead arm, or a def was dropped)", name)
	}

	// Anchors: make a silently-degraded parse FAIL rather than pass vacuously.
	if len(dispatched) < dispatchCoverageReadFloor {
		t.Fatalf("scan found only %d dispatched tool name(s) (read-floor %d) — the dispatch scan is broken, not the code",
			len(dispatched), dispatchCoverageReadFloor)
	}
	for _, want := range []string{toolnames.ToolRead, toolnames.ToolBash, toolnames.ToolTask} {
		if !dispatched[want] {
			t.Fatalf("scan did not find the %q arm — the dispatch scan is broken, not the code", want)
		}
	}
	// The subtraction set must be non-empty: if the action-verb resolution
	// breaks, the reverse direction would report every nested action label as
	// a defect, and silently skipping the assertion would hide it instead.
	if len(actionVerbs) == 0 {
		t.Fatal("no merged-family action verbs resolved — the toolnames scan is broken, not the code")
	}
}

// mergedFamilyActionVerbs resolves the action-verb constants of the merged
// families (task | job | memory) to their wire values. They appear as nested
// case labels in the dispatch switch but are NOT tool names.
func mergedFamilyActionVerbs(t *testing.T, toolNames map[string]string) map[string]bool {
	t.Helper()
	consts := []string{
		"ToolTaskView", "ToolTaskCommit", "ToolTaskReview", "ToolTaskReframe",
		"ToolJobActionStart", "ToolJobActionOutput", "ToolJobActionKill",
		"ToolMemoryActionSearch", "ToolMemoryActionStore",
		"ToolMemoryActionUpdate", "ToolMemoryActionFeedback",
	}
	out := map[string]bool{}
	for _, c := range consts {
		v, ok := toolNames[c]
		if !ok {
			t.Fatalf("toolnames.%s not found — the merged-family action constants moved (update this gate)", c)
		}
		out[v] = true
	}
	return out
}

// caseExprKind distinguishes the shapes a dispatch case label may take.
type caseExprKind int

const (
	exprConst caseExprKind = iota // toolnames.ToolX
	exprBare                      // "literal"
	exprOther                     // anything else (an expression we cannot resolve)
)

// caseExpr is one resolved dispatch case label.
type caseExpr struct {
	kind  caseExprKind
	ident string // constant name (exprConst) or the literal text (exprBare)
}

// dispatcherCaseLabels extracts the case labels of executeToolDefault's
// TOOL-LEVEL switch out of loop.go's AST — the whole reason this gate is
// structural rather than textual: a grep for `case toolnames.Tool` would also
// collect the nested action switches, and a line-based scan would break on
// multi-label cases (`case toolnames.ToolTaskView, toolnames.ToolTaskCommit:`)
// or a case whose arms span lines.
func dispatcherCaseLabels(t *testing.T) []caseExpr {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, dispatchSourceFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dispatchSourceFile, err)
	}
	labels, found := switchCaseLabels(file, dispatchSwitchSelector)
	if !found {
		t.Fatalf("no `switch %s` in %s — executeToolDefault's dispatch switch moved or was renamed "+
			"(this gate is anchored on it; update the anchor, do not delete the gate)", dispatchSwitchSelector, dispatchSourceFile)
	}
	return labels
}

// switchCaseLabels collects the case labels of the FIRST switch whose tag is
// `selector`, and reports whether such a switch exists. It does not descend
// into that switch's bodies, so a nested action switch (`switch action`, or a
// re-switch on tc.Name) contributes nothing — the caller asked for the
// TOOL-LEVEL arms.
func switchCaseLabels(file *ast.File, selector string) ([]caseExpr, bool) {
	var labels []caseExpr
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || sw.Tag == nil || selectorString(sw.Tag) != selector {
			return true
		}
		if found { // a second switch on the same tag: not our dispatch shape
			return true
		}
		found = true
		for _, stmt := range sw.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok || clause.List == nil { // nil list == the `default:` clause
				continue
			}
			for _, e := range clause.List {
				labels = append(labels, resolveCaseExpr(e))
			}
		}
		return false
	})
	return labels, found
}

// resolveCaseExpr maps one case expression to its kind.
func resolveCaseExpr(e ast.Expr) caseExpr {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "toolnames" {
			return caseExpr{kind: exprConst, ident: sel.Sel.Name}
		}
		return caseExpr{kind: exprOther, ident: selectorString(e)}
	}
	if id, ok := e.(*ast.Ident); ok {
		// A bare identifier label (a local/service const, or an unresolved
		// name): not a toolnames constant, so it is reported, not trusted.
		return caseExpr{kind: exprOther, ident: id.Name}
	}
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return caseExpr{kind: exprOther, ident: selectorString(e)}
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return caseExpr{kind: exprOther, ident: lit.Value}
	}
	return caseExpr{kind: exprBare, ident: s}
}

// selectorString renders `a.b` for a selector expression, "" otherwise.
func selectorString(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return pkg.Name + "." + sel.Sel.Name
}

// toolnameConstantValues resolves every constant in internal/toolnames/names.go
// to its string value — the single source of truth the case labels are written
// with (Rule 14: an exported constant package is the reason this resolution is
// possible at all).
func toolnameConstantValues(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(toolnamesPackageDir)
	if err != nil {
		t.Fatalf("read %s: %v", toolnamesPackageDir, err)
	}
	values := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(toolnamesPackageDir, e.Name())
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					if s, err := strconv.Unquote(lit.Value); err == nil {
						values[name.Name] = s
					}
				}
			}
		}
	}
	if len(values) == 0 {
		t.Fatalf("no string constants resolved from %s — the font of tool names moved (update this gate)", toolnamesPackageDir)
	}
	return values
}

// TestGate_ToolDispatchScanIsNotVacuous is the parser self-test: it pins the
// extractor against a synthetic switch shaped like the real one (a nested
// action switch with a bare-string label inside an arm, plus a default clause),
// then asserts the extractor sees exactly the TOOL-LEVEL labels — the nested
// action verb and the default clause must not be collected. A gate whose
// scanner silently over- or under-collects is a gate that certifies the wrong
// thing; this is the test that fails if the scanner drifts.
func TestGate_ToolDispatchScanIsNotVacuous(t *testing.T) {
	const src = `package p

type tcT struct {
	Name string
	Args map[string]interface{}
}

func pkg(expr string) string { return expr }

func executeToolDefault(tc tcT) string {
	switch tc.Name {
	case toolnames.ToolRead:
		return pkg("read")
	case toolnames.ToolTask:
		switch tc.Args["action"] {
		case "commit":
			return pkg("commit")
		}
		return pkg("task")
	case "legacy_tool":
		return pkg("legacy")
	default:
		return pkg("unknown")
	}
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "synthetic.go", src, 0)
	if err != nil {
		t.Fatalf("parse synthetic source: %v", err)
	}
	labels, found := switchCaseLabels(file, dispatchSwitchSelector)
	if !found {
		t.Fatal("switchCaseLabels did not find the synthetic `switch tc.Name`")
	}
	var got []string
	kinds := map[string]caseExprKind{}
	for _, l := range labels {
		got = append(got, l.ident)
		kinds[l.ident] = l.kind
	}
	want := []string{"ToolRead", "ToolTask", "legacy_tool"}
	if len(got) != len(want) {
		t.Fatalf("synthetic scan = %v, want exactly %v (the nested action switch's bare-string label "+
			"and the default clause must NOT be collected)", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("synthetic scan = %v, want exactly %v (order-sensitive: source order)", got, want)
		}
	}
	for _, name := range []string{"ToolRead", "ToolTask"} {
		if kinds[name] != exprConst {
			t.Fatalf("label %q resolved to kind %v, want exprConst (a toolnames.X selector)", name, kinds[name])
		}
	}
	if kinds["legacy_tool"] != exprBare {
		t.Fatalf("label %q resolved to kind %v, want exprBare (a bare string label is a Rule 1b defect "+
			"the scan must SEE, not silently trust)", "legacy_tool", kinds["legacy_tool"])
	}

	// A file with NO switch on the dispatch tag must report found=false — the
	// caller turns that into a loud failure, so a renamed/moved dispatch switch
	// can never be mistaken for "nothing to check".
	other, err := parser.ParseFile(token.NewFileSet(), "other.go", `package p

func f(x int) string {
	switch x {
	case 1:
		return "one"
	}
	return ""
}
`, 0)
	if err != nil {
		t.Fatalf("parse control source: %v", err)
	}
	if _, found := switchCaseLabels(other, dispatchSwitchSelector); found {
		t.Fatal("switchCaseLabels claimed to find `switch tc.Name` in a file that has no such switch")
	}
}
