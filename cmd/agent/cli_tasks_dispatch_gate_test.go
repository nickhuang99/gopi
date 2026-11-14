package main

// cli_tasks_dispatch_gate_test.go — the Rule 1b/13 guard for the `gopi task`
// family DISPATCH surface, and the AST utilities the family's other sync
// guards share.
//
// BACKGROUND (why the scanner is an AST one): the dispatch switch used to
// compare bare verb literals (`case "search", "show", "status", "update",
// "archive":`), and TestTaskCompletionNamesMatchDispatch discovered the
// dispatched verbs by REGEX-scanning for QUOTED case labels. Making the labels
// reference internal/taskverbs made that regex find nothing, so the test would
// have reported every verb as missing while the code was in fact correct. A
// regex over source text cannot see through a constant, so the surface is
// resolved properly here: parse the file, walk the AST, and resolve every
// label/spelling through internal/taskverbs (the family's single source of
// truth).
//
// WHAT IS ENFORCED
//
//  1. Decision sites carry no bare family-verb literal. A "decision site" is a
//     comparison against the verb variable (`verb == "search"`) or a case
//     label of a switch ON the verb variable (`switch verb { case "status":`).
//     The scan is deliberately scoped to the verb/sub/target discriminants: a
//     FLAG name may legitimately spell a family word (`--status` is a flag of
//     several verbs), so a whole-file literal scan would be noise, while a
//     literal compared against the verb variable is exactly the defect.
//  2. Every dispatch case label resolves to a taskverbs constant — an
//     unresolved identifier or a bare literal is a finding.
//  3. The dispatch surface is EXHAUSTIVE and EXACT: the label set equals
//     taskverbs.DispatchVerbs (see TestTaskCompletionNamesMatchDispatch in
//     cli_completion_test.go for the cross-surface half).
//
// A literal that is NOT the family verb but happens to be spelled the same
// (git's `push` subcommand) carries the reviewed inline escape
// `// task-verb-lit-ok: <reason>` on its line, mirroring the
// internal/standards gates (an escape is a reasoned exception, not a
// suppression).

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskverbs"
)

const (
	// taskverbsSourcePath is the family's single source of truth (leaf
	// package): its const block is what a dispatch label resolves against.
	taskverbsSourcePath = "../../internal/taskverbs/verbs.go"

	// verbLitEscape is the reviewed inline escape: a literal that equals a
	// family verb spelling but is NOT the family verb (a git subcommand, a
	// flag name compared on its own) marks its line with this comment.
	verbLitEscape = "// task-verb-lit-ok:"

	// verbDecisionDiscriminants are the variables whose comparison against a
	// string literal IS a family decision (the dispatch verb, the /task slash
	// sub-verb, and the run target — `target == "next"`).
	verbDecisionDiscriminants = "verb,sub,target"

	// minScannedDecisions guards against a vacuous scan: the family dispatcher
	// alone has ~25 case labels plus the comparison sites, and the tree walk
	// must see the family files. A scan that inspected fewer means the walk or
	// the AST lookup broke (a green gate that read nothing is the failure mode
	// this floor exists to prevent).
	minScannedDecisions = 30
	minScannedFiles     = 8
)

// taskverbConsts parses internal/taskverbs/verbs.go and returns its exported
// const name → verb spelling map. Reading the values from the definition site
// (instead of a hand-maintained copy in this test) is what keeps the guard
// honest: there is exactly one place a family spelling is written (Rule 13).
func taskverbConsts(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, taskverbsSourcePath, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", taskverbsSourcePath, err)
	}
	out := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("%s: unquote %s: %v", taskverbsSourcePath, lit.Value, err)
			}
			for _, name := range vs.Names {
				out[name.Name] = val
			}
		}
	}
	if len(out) < 20 {
		t.Fatalf("resolved only %d constants from %s — the family vocabulary is not being read", len(out), taskverbsSourcePath)
	}
	return out
}

// resolvedVerb resolves one case-label / comparison expression to a family verb
// spelling. A BasicLit is a bare literal (finding). A taskverbs selector
// resolves through consts. Anything else is unresolved (finding).
func resolvedVerb(expr ast.Expr, consts map[string]string) (verb string, ok bool) {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		pkg, isIdent := e.X.(*ast.Ident)
		if !isIdent || pkg.Name != "taskverbs" {
			return "", false
		}
		v, found := consts[e.Sel.Name]
		return v, found
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			if s, err := strconv.Unquote(e.Value); err == nil {
				return s, false
			}
		}
		return "", false
	default:
		return "", false
	}
}

// verbDecisionFindings scans one source file for the two decision-site defects:
// a case label of a switch ON a family discriminant that is not a taskverbs
// constant, and a string literal compared (== / !=) against a family
// discriminant. It returns one human-readable finding per site and the number
// of decision sites inspected.
// onlyFuncs, when non-empty, restricts the scan to those top-level functions.
// It exists because commands.go hosts SEVERAL slash-command families in one
// file (`sub` discriminates /task, /log and /memory) — scanning the whole file
// would flag `/log search` as if it were the task verb `search`. The family
// scope is the function, not the file.
func verbDecisionFindings(t *testing.T, path string, src []byte, consts map[string]string, onlyFuncs ...string) (findings []string, inspected int) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	roots := []ast.Node{f}
	if len(onlyFuncs) > 0 {
		roots = nil
		want := map[string]bool{}
		for _, n := range onlyFuncs {
			want[n] = true
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && want[fn.Name.Name] {
				roots = append(roots, fn)
			}
		}
		if len(roots) != len(onlyFuncs) {
			t.Fatalf("%s: found %d of %d named functions (%v) — the scope is wrong, so a green run would prove nothing",
				path, len(roots), len(onlyFuncs), onlyFuncs)
		}
	}
	lines := strings.Split(string(src), "\n")
	escaped := func(pos token.Pos) bool {
		ln := fset.Position(pos).Line
		if ln <= 0 || ln > len(lines) {
			return false
		}
		return strings.Contains(lines[ln-1], verbLitEscape)
	}
	isDiscriminant := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		if !ok {
			return false
		}
		for _, d := range strings.Split(verbDecisionDiscriminants, ",") {
			if id.Name == d {
				return true
			}
		}
		return false
	}
	visit := func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SwitchStmt:
			if node.Tag == nil || !isDiscriminant(node.Tag) {
				return true
			}
			for _, stmt := range node.Body.List {
				cc, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expr := range cc.List {
					verb, resolved := resolvedVerb(expr, consts)
					if resolved {
						inspected++
						continue
					}
					// A label that is not a family spelling at all (e.g. the
					// `-h|--help|help` arm) is another vocabulary's business.
					familyName, isFamily := familyVerbValue(consts, verb)
					if !isFamily {
						continue
					}
					_ = familyName
					inspected++
					if escaped(expr.Pos()) {
						continue
					}
					findings = append(findings, describeVerbSite(t, path, fset, expr.Pos(),
						"%s", verb, "case label"))
				}
			}
		case *ast.BinaryExpr:
			if node.Op != token.EQL && node.Op != token.NEQ {
				return true
			}
			lit, isLit := node.Y.(*ast.BasicLit)
			if !isLit {
				lit, isLit = node.X.(*ast.BasicLit)
			}
			if !isLit || lit.Kind != token.STRING {
				return true
			}
			if !isDiscriminant(node.X) && !isDiscriminant(node.Y) {
				return true
			}
			inspected++
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if _, known := familyVerbValue(consts, val); !known {
				return true // a non-family spelling is not this gate's business
			}
			if escaped(lit.Pos()) {
				return true
			}
			findings = append(findings, describeVerbSite(t, path, fset, lit.Pos(),
				"%s", val, "comparison against the family discriminant"))
		}
		return true
	}
	for _, root := range roots {
		ast.Inspect(root, visit)
	}
	return findings, inspected
}

// familyVerbValue reports whether a spelling IS a family verb (any constant in
// the leaf package), and returns its constant name.
func familyVerbValue(consts map[string]string, val string) (string, bool) {
	for name, v := range consts {
		if v == val {
			return name, true
		}
	}
	return "", false
}

// describeVerbSite renders a finding with the file:line and the offending text.
func describeVerbSite(t *testing.T, path string, fset *token.FileSet, pos token.Pos, format, val, what string) string {
	t.Helper()
	where := fset.Position(pos)
	return fmt.Sprintf("%s:%d: %s uses the bare literal %q — reference the internal/taskverbs constant (Rule 1b/13)",
		path, where.Line, what, fmt.Sprintf(format, val))
}

// taskFamilyScanScope is the family's production scan scope: every
// cli_tasks*.go file whole, plus the ONE slash-command function in
// commands.go that mirrors the family engine surface (the file also hosts
// /log and /memory, whose `sub` switch is a different vocabulary).
type taskFamilyScanScope struct {
	file  string
	funcs []string // empty = the whole file
}

func taskFamilyProductionScope(t *testing.T) []taskFamilyScanScope {
	t.Helper()
	local, err := filepath.Glob("cli_tasks*.go")
	if err != nil {
		t.Fatalf("glob cli_tasks*.go: %v", err)
	}
	scopes := []taskFamilyScanScope{}
	for _, p := range local {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		scopes = append(scopes, taskFamilyScanScope{file: p})
	}
	scopes = append(scopes, taskFamilyScanScope{file: "commands.go", funcs: []string{"runSlashTask"}})
	if len(scopes) < minScannedFiles {
		t.Fatalf("the family scan scope is %d files (%v) — the glob broke, so this gate would pass vacuously",
			len(scopes), scopes)
	}
	return scopes
}

// TestNoBareTaskVerbLiteralAtDecisionSite is the gate: no family-verb literal
// survives at a dispatch/decision site in the family's production sources.
func TestNoBareTaskVerbLiteralAtDecisionSite(t *testing.T) {
	consts := taskverbConsts(t)
	total := 0
	for _, scope := range taskFamilyProductionScope(t) {
		src, err := os.ReadFile(scope.file)
		if err != nil {
			t.Fatalf("read %s: %v", scope.file, err)
		}
		findings, inspected := verbDecisionFindings(t, scope.file, src, consts, scope.funcs...)
		total += inspected
		for _, f := range findings {
			t.Error(f)
		}
	}
	if total < minScannedDecisions {
		t.Fatalf("the scan inspected only %d decision sites (floor %d) — the AST lookup is broken, so a green run proves nothing",
			total, minScannedDecisions)
	}
}

// TestVerbDecisionGateRedProof proves the gate is not vacuous: it runs the
// predicate over the PRE-FIX source shapes that were red on master (a quoted
// case-label line and a `verb == "search"` comparison) and requires both to be
// flagged. A gate you have never seen fail is a gate you cannot trust.
func TestVerbDecisionGateRedProof(t *testing.T) {
	consts := taskverbConsts(t)
	for _, probe := range []struct {
		name string
		src  string
		want int
	}{
		{
			name: "master's quoted dispatch labels",
			src: `package main

func runTasksSubcommand(args []string) {
	verb := args[0]
	switch verb {
	case "search", "show", "status", "update", "archive":
		_ = verb
	}
}
`,
			want: 5,
		},
		{
			name: "master's comparison on the run target",
			src: `package main

func runTaskRunOnce(target string) {
	if target == "" || target == "next" {
		return
	}
}
`,
			want: 1,
		},
		{
			// A FLAG name is not a decision on the verb: no finding. This is
			// the negative probe that keeps the gate's scope honest.
			name: "flag-name literal (not a decision site)",
			src: `package main

func parseTasksArgs(verb string) string {
	return verb + ":" + "status"
}
`,
			want: 0,
		},
		{
			// The reviewed escape: a same-spelled literal that is NOT the
			// family verb (git's push subcommand) is allowed with a reason.
			name: "escaped non-family literal",
			src: `package main

func pushBranch(verb string) []string {
	return []string{"push"} // task-verb-lit-ok: git subcommand, not the family verb
}
`,
			want: 0,
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			findings, _ := verbDecisionFindings(t, "probe.go", []byte(probe.src), consts)
			if len(findings) != probe.want {
				t.Errorf("probe %q produced %d findings, want %d:\n%s",
					probe.name, len(findings), probe.want, strings.Join(findings, "\n"))
			}
		})
	}
}

// TestDispatchLabelsResolveToTaskverbsConstants is the exactness half: every
// case label of the dispatcher's verb switch resolves to a taskverbs constant,
// AND the resolved set equals taskverbs.DispatchVerbs — so a NEW family verb
// (added to the leaf package) with no dispatch case fails here, which is the
// guard that a constant-only conversion must not lose.
func TestDispatchLabelsResolveToTaskverbsConstants(t *testing.T) {
	consts := taskverbConsts(t)
	src, err := os.ReadFile("cli_tasks.go")
	if err != nil {
		t.Fatalf("read cli_tasks.go: %v", err)
	}
	labels := dispatchVerbLabels(t, src, consts)
	if len(labels) < 20 {
		t.Fatalf("resolved only %d dispatch labels — the AST lookup is broken", len(labels))
	}
	seen := map[string]bool{}
	for _, v := range labels {
		seen[v] = true
	}
	for _, want := range taskverbs.DispatchVerbs {
		if !seen[want] {
			t.Errorf("taskverbs.DispatchVerbs declares %q but runTasksSubcommand has no case for it — add the dispatch case (or drop the verb from the vocabulary)", want)
		}
	}
	known := map[string]bool{}
	for _, v := range taskverbs.DispatchVerbs {
		known[v] = true
	}
	for _, got := range labels {
		if !known[got] {
			t.Errorf("runTasksSubcommand dispatches %q, which taskverbs.DispatchVerbs does not declare — add it to the leaf package so help/completion/tab can follow", got)
		}
	}
}

// dispatchVerbLabels returns the resolved verb spellings of every
// `switch verb { … }` case label inside runTasksSubcommand (the AST replacement
// for the old regex-over-quoted-labels scan).
func dispatchVerbLabels(t *testing.T, src []byte, consts map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "cli_tasks.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse cli_tasks.go: %v", err)
	}
	var out []string
	found := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "runTasksSubcommand" {
			continue
		}
		found = true
		ast.Inspect(fn, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			id, ok := sw.Tag.(*ast.Ident)
			if !ok || id.Name != "verb" {
				return true
			}
			for _, stmt := range sw.Body.List {
				cc, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expr := range cc.List {
					verb, resolved := resolvedVerb(expr, consts)
					if !resolved {
						t.Errorf("%s: dispatch case label %q is not a taskverbs constant", fset.Position(expr.Pos()), exprText(src, expr))
						continue
					}
					out = append(out, verb)
				}
			}
			return true
		})
	}
	if !found {
		t.Fatal("runTasksSubcommand not found in cli_tasks.go")
	}
	return out
}

// exprText renders an expression's source text for diagnostics.
func exprText(src []byte, expr ast.Expr) string {
	start := expr.Pos() - 1
	end := expr.End() - 1
	if int(start) < 0 || int(end) > len(src) || start > end {
		return "<expr>"
	}
	return string(src[start:end])
}
