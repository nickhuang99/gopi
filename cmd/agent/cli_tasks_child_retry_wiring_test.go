package main

// cli_tasks_child_retry_wiring_test.go — the WIRING half of the bounded
// abnormal-child retry (epic wrapped-executor-child, 4/5).
//
// WHY A SOURCE GATE INSTEAD OF A LIVE end-to-end. The retry arm hangs off the
// wrapper's `cmd.Wait()` failure return — the ONLY place a real executor child
// can be observed exiting abnormally. Driving that seam end-to-end needs a
// child that really exits non-zero without a model call, and the existing
// hermetic escape (taskChildFailTestEnv) deliberately RETURNS BEFORE cmd.Wait:
// it is the failed-WRITEBACK seam, not the failed-CHILD seam. Faking one there
// would either spawn the TEST binary as the resumed run (the launcher falls
// back to os.Executable()) or rewrite thirteen existing tests' expectations.
//
// So the two halves are pinned separately, each where it can be pinned
// honestly:
//
//   - the DECISION (bounded ladder, cap handover, judged-failure refusal,
//     counter survival across a restart) is exercised in-process against a
//     real ledger — cli_tasks_child_retry_test.go;
//   - the CALL SITE is asserted here, on the source of the production
//     function, both ways: the arm MUST run on the abnormal-child return, and
//     it MUST NOT be reachable from the hermetic escape branch.
//
// The gate reads the file it lives beside (the repo_context_sandbox_guard_test
// precedent: a source-level property is asserted by parsing the source).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

const (
	// The production file and the two identifiers the gate ties together.
	retryWiringFile   = "cli_tasks.go"
	retryWiringFn     = "runTaskRunOnce"
	retryWiringArm    = "taskRunAbnormalChildRetry"
	retryWiringEscape = "taskChildFailTestEnv"
	// The writeback SPELLING the arm must come after. It is the failure seam,
	// not the raw writebackOutcome: since epic writeback-lost every failing arm
	// routes its terminal write through writebackFailure, which is the call that
	// actually writes the row back at this site (writebackOutcome is its callee).
	retryWiringWriteback = "writebackFailure"
	retryWiringReadOnly  = "TaskRowTerminalAt"
)

// TestRetryArmIsWiredToTheAbnormalChildExit: the arm is CALLED from inside
// runTaskRunOnce, in the same `if runErr != nil` block as cmd.Wait — the one
// site where an abnormally-exited child is observed — and the discriminator is
// read BEFORE that block's writeback (a later read would always see the
// wrapper's own terminal write and every exit would look judged).
func TestRetryArmIsWiredToTheAbnormalChildExit(t *testing.T) {
	fn := retryWiringFuncDecl(t, retryWiringFn)

	// Locate the `if runErr != nil { … }` block that guards the waitpid.
	var guard *ast.IfStmt
	ast.Inspect(fn, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || guard != nil {
			return true
		}
		if id, ok := ifs.Cond.(*ast.BinaryExpr); ok {
			if _, ok := id.X.(*ast.Ident); ok && id.X.(*ast.Ident).Name == "runErr" {
				guard = ifs
			}
		}
		return true
	})
	if guard == nil {
		t.Fatalf("%s: no `if runErr != nil` block found — the waitpid failure return moved; this gate must be "+
			"re-pointed at the new site (the retry arm belongs where an abnormal child exit is observed)", retryWiringFn)
	}
	var called, sawWriteback bool
	var callLine, writebackLine int
	ast.Inspect(guard, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// The arm and the writeback are both package-local calls, so the
		// callee is a plain *ast.Ident; the selector arm is kept for the
		// exported/method spellings a future refactor could introduce.
		name := ""
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		}
		switch name {
		case retryWiringArm:
			if callLine == 0 {
				called, callLine = true, retryWiringFset(t).Position(call.Pos()).Line
			}
		case retryWiringWriteback:
			// writebackOutcome is the wrapper's own terminal write; the arm
			// must come AFTER it at the call site.
			if writebackLine == 0 {
				sawWriteback, writebackLine = true, retryWiringFset(t).Position(call.Pos()).Line
			}
		}
		return true
	})
	if !called {
		t.Fatalf("the abnormal-child return does not call %s — the retry ladder is dead code", retryWiringArm)
	}
	if !sawWriteback {
		t.Fatalf("the abnormal-child return no longer writes the row back — the site changed shape; re-point this gate")
	}
	if callLine < writebackLine {
		t.Errorf("%s is called at line %d, before the writeback at %d — the arm must run after the wrapper's own "+
			"terminal write (the pre-writeback status it judges is read by the caller, above both)", retryWiringArm, callLine, writebackLine)
	}
	// The discriminator is read in the same block, BEFORE the arm.
	var statusCallLine int
	ast.Inspect(guard, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		}
		if name == retryWiringReadOnly && statusCallLine == 0 {
			statusCallLine = retryWiringFset(t).Position(call.Pos()).Line
		}
		return true
	})
	if statusCallLine == 0 {
		t.Fatalf("the abnormal-child return never reads the row status (%s) — without it every exit looks judged", retryWiringReadOnly)
	}
	if statusCallLine > callLine {
		t.Errorf("the row status is read at line %d, AFTER the arm at %d — the wrapper's own writeback would have "+
			"made every exit look like a judged failure", statusCallLine, callLine)
	}
}

// TestRetryArmIsNotOnTheHermeticEscapePath: the test escape is the
// failed-WRITEBACK seam. If the arm were reachable from it, every existing
// escape user would spawn a resume run — the launcher falls back to
// os.Executable(), i.e. the TEST BINARY — and thirteen tests would recurse into
// the suite. The exclusion is therefore a property, not a comment.
func TestRetryArmIsNotOnTheHermeticEscapePath(t *testing.T) {
	fn := retryWiringFuncDecl(t, retryWiringFn)
	ast.Inspect(fn, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		// The escape branch: a condition mentioning the escape env constant.
		var namesEscape bool
		ast.Inspect(ifs.Cond, func(c ast.Node) bool {
			if id, ok := c.(*ast.Ident); ok && id.Name == retryWiringEscape {
				namesEscape = true
			}
			return true
		})
		if !namesEscape {
			return true
		}
		ast.Inspect(ifs, func(c ast.Node) bool {
			if call, ok := c.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == retryWiringArm {
					t.Errorf("the hermetic escape branch (line %d) calls %s — that spawns a resume run in every "+
						"existing escape user, and the launcher resolves to the TEST BINARY",
						retryWiringFset(t).Position(call.Pos()).Line, retryWiringArm)
				}
			}
			return true
		})
		return true
	})
}

// retryWiringFuncDecl parses the production file and returns the named
// function declaration.
func retryWiringFuncDecl(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, retryWiringFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", retryWiringFile, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			retryWiringFsetForTest = fset
			return fn
		}
	}
	t.Fatalf("%s not found in %s", name, retryWiringFile)
	return nil
}

// retryWiringFsetForTest carries the FileSet of the parsed production file to
// the position lookups (a FileSet is only meaningful with the file it parsed).
var retryWiringFsetForTest *token.FileSet

// retryWiringFset returns the FileSet used by the last parse, with a floor so a
// lookup before any parse fails loudly instead of panicking.
func retryWiringFset(t *testing.T) *token.FileSet {
	t.Helper()
	if retryWiringFsetForTest == nil {
		t.Fatal("the wiring gate parsed no file — call retryWiringFuncDecl first")
	}
	return retryWiringFsetForTest
}

// TestRetryWiringGateReadsTheRealFile guards the gate itself: a renamed or
// moved production file must fail here rather than silently pass (the
// read-floor pattern of the other source gates in this repo).
func TestRetryWiringGateReadsTheRealFile(t *testing.T) {
	src, err := os.ReadFile(retryWiringFile)
	if err != nil {
		t.Fatalf("read %s: %v — this gate's file must stay readable", retryWiringFile, err)
	}
	if len(src) < 10_000 {
		t.Fatalf("%s is suspiciously small (%d bytes) — the gate is reading the wrong file", retryWiringFile, len(src))
	}
}
