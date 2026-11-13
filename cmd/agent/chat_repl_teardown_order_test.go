package main

// chat_repl_teardown_order_test.go — the CHECK for the teardown order that
// chat_pty_fixture_test.go states ONCE (editor → the terminal, which IS the stop
// → the join → the descriptors).
//
// WHY A SOURCE CHECK AND NOT A RUNTIME ONE. The order cannot be red-proved by a
// green run. With the production stop path in place (internal/lineedit, e6e1791d)
// the read loop ends under EITHER order — closing the pty master also ends it —
// so a green chat case does not distinguish "the join observed the terminal's own
// stop" from "the join observed the descriptor's close, one statement later, on
// scheduling luck". The two orders differ in what the observer MEANS, and that
// difference lives in the ORDER OF THE STATEMENTS a reader sees; the guard that
// catches a regression of it is therefore a check on the source, in the repo's
// own family of source-shape gates (a mirror-literal gate, a worktree-root gate —
// a property no runtime observation can carry).
//
// THE CHECK. For every call to chatJoinReadLoop, the function literal or
// declaration that contains it must ALSO contain a stop of the terminal — a
// `Close` call on a value this package names `term`/`terminal`, directly or as a
// harness field — at an EARLIER position in the source. Both failure directions
// are named, not inferred:
//
//   - the join is placed BEFORE the stop (the pre-fix shape): the join observes
//     the descriptor's close, not the stop path, and the guard fails by name;
//   - the stop is dropped while the join stays: the same failure, because there
//     is then no stop ahead of the join at all.
//
// WHAT IT DOES NOT CHECK (honesty over coverage): it reads NAMES, not types, so a
// stop reached through a differently-named variable is invisible to it (the
// failure message says so, so the remedy is a deliberate edit of this guard
// rather than a mystery). It also says nothing about a fixture that runs no join
// at all — the nine direct-driver pty fixtures have no read loop to join — and
// the package's goroutine sweep remains what catches a leak no ordering would
// have prevented.

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	// chatJoinReadLoopName and chatCloseMethodName are the two identifiers the
	// scan resolves. They are constants because the string IS the matched value
	// (Rule 1b): a control that retyped "chatJoinReadLoop" would silently stop
	// matching the call it is meant to police.
	chatJoinReadLoopName = "chatJoinReadLoop"
	chatCloseMethodName  = "Close"
	// chatGuardScanDir is the package directory the scan reads: the tests' own
	// working directory, which is the directory under test.
	chatGuardScanDir = "."
	// chatTestFileSuffix selects the files scanned — the same set the package's
	// tests are built from.
	chatTestFileSuffix = "_test.go"
)

// chatJoinSiteFloor is the number of chatJoinReadLoop call sites the package is
// known to have. It is the guard's READ-FLOOR: without it, deleting the joins
// (or renaming the helper) would leave the scan with nothing to check and the
// guard would pass vacuously, which is the failure mode a source gate is most
// prone to. Raising it when a fixture is added is the intended cost; LOWERING it
// to make a red run green is the thing it exists to prevent.
const chatJoinSiteFloor = 5

// chatTerminalStopName matches the receiver of a stop call: the terminal value
// itself (`term`, `terminal`), directly or as a harness field (`h.term`,
// `s.term`). A name pattern rather than a type query is deliberate: the property
// being guarded is the statement order a reader sees, and the package's REPL
// fixtures hold the terminal under exactly these names.
var chatTerminalStopName = regexp.MustCompile(`(^|\.)(term|terminal)$`)

// TestChatReplTeardownOrder_EveryJoinStopsTheTerminalFirst is the guard. It is
// green only while every join in the package sits AFTER a terminal stop in its
// own function.
func TestChatReplTeardownOrder_EveryJoinStopsTheTerminalFirst(t *testing.T) {
	entries, err := os.ReadDir(chatGuardScanDir)
	if err != nil {
		t.Fatalf("read the package directory %q: %v", chatGuardScanDir, err)
	}
	fset := token.NewFileSet()
	scanned, joins := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), chatTestFileSuffix) {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		scanned++
		ast.Inspect(f, func(n ast.Node) bool {
			body := chatFuncBody(n)
			if body == nil {
				return true
			}
			stops, sites := chatTeardownPositions(body)
			for _, site := range sites {
				joins++
				if chatStopPrecedes(stops, site) {
					continue
				}
				// The message states the remedy, because the scan reads names: a
				// legitimate rename would otherwise look like a defect with no
				// way forward.
				t.Errorf("%s:%d: %s runs with no terminal stop ahead of it in its own function — the join is what OBSERVES the stop, so a join placed first observes the pty master's close instead (the pre-fix shape), and a join with no stop at all observes a bound that elapsed. The four-step order is stated in chat_pty_fixture_test.go: editor.Close() → term.Close() (THE STOP) → the join → the descriptors. If the stop is now reached under another name, teach chatTerminalStopName the name in the same commit",
					e.Name(), fset.Position(site).Line, chatJoinReadLoopName)
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatalf("the guard scanned no %s file in %q — a source gate that reads nothing passes vacuously", chatTestFileSuffix, chatGuardScanDir)
	}
	if joins < chatJoinSiteFloor {
		t.Fatalf("the guard found %d %s call site(s), below the floor of %d — the REPL fixtures join their read loop, and a scan that no longer sees the joins has stopped policing them (deleting a join instead of ordering it is the failure this floor names)",
			joins, chatJoinReadLoopName, chatJoinSiteFloor)
	}
}

// chatFuncBody is the body of a function declaration or literal, or nil for any
// other node.
func chatFuncBody(n ast.Node) *ast.BlockStmt {
	switch v := n.(type) {
	case *ast.FuncDecl:
		return v.Body
	case *ast.FuncLit:
		return v.Body
	}
	return nil
}

// chatTeardownPositions reports the positions of the terminal stops and of the
// joins performed DIRECTLY in one function body. A nested function literal is
// scanned on its own turn (and is not descended into here): the join and the stop
// it observes are in the same closure, and a stop in some other closure would not
// be an order at all.
func chatTeardownPositions(body *ast.BlockStmt) (stops, joins []token.Pos) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			switch fn := v.Fun.(type) {
			case *ast.Ident:
				if fn.Name == chatJoinReadLoopName {
					joins = append(joins, v.Pos())
				}
			case *ast.SelectorExpr:
				if fn.Sel.Name == chatCloseMethodName && chatTerminalStopName.MatchString(chatExprString(fn.X)) {
					stops = append(stops, v.Pos())
				}
			}
		}
		return true
	})
	return stops, joins
}

// chatStopPrecedes reports whether any recorded stop sits before pos.
func chatStopPrecedes(stops []token.Pos, pos token.Pos) bool {
	for _, s := range stops {
		if s < pos {
			return true
		}
	}
	return false
}

// chatExprString renders an expression back to source so the receiver of a stop
// call can be matched by name (`term`, `h.term`). Rendering rather than a type
// switch keeps the match honest for the selector shapes the fixtures use
// (`s.term`) without a type checker in a test.
func chatExprString(e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		return ""
	}
	return b.String()
}
