package tools

// own_worktree_self_declaration_test.go — the `Self` in the producer release's
// name says WHOSE SCAN it is, never WHOSE ROW the report is about.
//
// THE MEASURED DEFECT (2026-10-27, row 01a17dcd's close-out on this host):
//
//	task 01a17dcd-c59c-7344-a99a-83133d4416c7: own-worktree-release: own registered worktree kept: /home/nick/workspace/cptest-verify — worktree is DIRTY (uncommitted work) — left for a human
//
// The directory was a chat's hand-made checkout; the seven files in it belonged
// to a DIFFERENT task; `01a17dcd`'s own commit was complete and correct. The
// function that printed it is named `ReclaimSelfOwnRegisteredWorktrees` and has
// NO task id parameter — so nothing in its signature contradicted reading
// `Self` as "this row's", while its BODY enriches EVERY registration of EVERY
// repo the ledger names. The repair (own_worktree_ownership.go) makes the
// SUBJECT per-registration and neutral for what no row owns; what this file
// pins is the half a repair can silently lose again: THE DECLARATION.
//
// Two properties, both machine-read from the source (no comment is trusted as
// documentation — it is read as DATA):
//
//	① SIGNATURE — the pass takes no task id. A parameter would re-open the
//	  reading the defect came from ("give it the row and it will scope itself"),
//	  and the body would still walk every repo. Scope is decided per
//	  registration by GroundOwnership, which reads the LEDGER, not an argument.
//	② DOC FIRST LINE — the declaration `Self = this PROCESS, not this ROW.` is
//	  the function's FIRST comment line. Anywhere lower and a reader who greps
//	  the name meets `Self` before meeting the disclaimer.
//
// The red proof is mechanical: delete the first line, or add a parameter, and
// this test fails with the exact text it expected. Reverting the whole repair
// (own_worktree_ownership.go's reclaimSelfOwnLine neutral branch) turns the
// sibling test file red instead — the two halves fail on their own subjects.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ownSelfDeclaredSubject is the ONE spelling of the declaration (Rule 1b: a
// string a test asserts on and a doc carries must come from one place, and the
// test IS that place for a comment).
const ownSelfDeclaredSubject = "Self = this PROCESS, not this ROW."

// ownSelfDeclarationSource is the file the declaration lives in, resolved from
// THIS file's own location (runtime.Caller) rather than from the process's
// working directory — a `go test` from any directory must judge the same file.
const ownSelfDeclarationSource = "task_reclaim_self.go"

// ownSelfDeclarationFuncs are the entry points whose names carry `Self` and
// whose scope a reader must not misread: the pass, and its bounded wrapper
// (both are exported, both are called by a producer's close-out).
var ownSelfDeclarationFuncs = []string{
	"ReclaimSelfOwnRegisteredWorktrees",
	"ReclaimSelfOwnRegisteredWorktreesWithin",
}

// ownSelfDeclarationFile parses the declaration's source and returns the file's
// AST — the ONE reader both properties share, so a parse failure is reported
// once, by name, rather than as two mysterious misses.
func ownSelfDeclarationFile(t *testing.T) *ast.File {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller: cannot locate this test's own directory")
	}
	path := filepath.Join(filepath.Dir(thisFile), ownSelfDeclarationSource)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}

// ownSelfDeclarationDecl finds ONE function declaration by name, failing loudly
// when it is absent: a renamed function must not silently pass this guard (the
// guard's whole subject is a name that misleads a reader).
func ownSelfDeclarationDecl(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != name {
			continue
		}
		return fn
	}
	t.Fatalf("%s: the declaration %q is not in the source this guard reads — a renamed pass must be re-declared, "+
		"not left to a reader's guess about what `Self` scopes", ownSelfDeclarationSource, name)
	return nil
}

// TestReclaimSelfFunctionsDeclareTheirSubject is ① + ② over every function whose
// name carries `Self`.
func TestReclaimSelfFunctionsDeclareTheirSubject(t *testing.T) {
	file := ownSelfDeclarationFile(t)
	for _, name := range ownSelfDeclarationFuncs {
		fn := ownSelfDeclarationDecl(t, file, name)

		// ① THE SIGNATURE: no argument at all.
		//
		// A pass that took a task id would appear row-scoped while still
		// enumerating every repo the ledger names (the defect's exact shape: the
		// name promised a scope the body never had). The scope is answered per
		// registration by GroundOwnership, so an argument is not merely unused —
		// it would be a lie in the signature.
		if params := fn.Type.Params; params != nil && len(params.List) > 0 {
			var kinds []string
			for _, field := range params.List {
				kinds = append(kinds, ownSelfFieldSpelling(field))
			}
			t.Fatalf("%s takes (%s): `Self` names THIS PROCESS's scan, not a row — an argument would let a "+
				"reader believe the report is row-scoped while the body walks every repo the ledger names "+
				"(the 2026-10-27 defect). Ownership is decided per registration by GroundOwnership.",
				name, strings.Join(kinds, ", "))
		}

		// ② THE DOC FIRST LINE — read from the AST, so the assertion is on the
		// comment the compiler attaches to the declaration, not on any comment
		// that happens to mention it.
		if fn.Doc == nil {
			t.Fatalf("%s carries NO doc comment — the declaration %q must be its first line", name, ownSelfDeclaredSubject)
		}
		var firstLine string
		for _, line := range strings.Split(fn.Doc.Text(), "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				firstLine = trimmed
				break
			}
		}
		if firstLine != ownSelfDeclaredSubject {
			t.Fatalf("%s's doc must OPEN with %q so a reader who greps the name meets the disclaimer before "+
				"the word `Self` misleads them; got first line %q", name, ownSelfDeclaredSubject, firstLine)
		}
	}
}

// ownSelfFieldSpelling renders one parameter in the report sentence: the names
// when they have any, else the type — enough for an operator to see WHAT
// argument appeared without opening the file.
func ownSelfFieldSpelling(field *ast.Field) string {
	var parts []string
	for _, n := range field.Names {
		parts = append(parts, n.Name)
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	return "unnamed argument"
}
