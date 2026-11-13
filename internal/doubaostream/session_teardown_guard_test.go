package doubaostream

// session_teardown_guard_test.go — the guard that makes "the teardown FORGETS to
// join" a RED build, and the honest reason it exists.
//
// THE PROPERTY. Session.Close and Session.Finish both end by waiting for the
// reader goroutine (awaitReader). The wait is not decoration: Close's contract to
// its caller is that the socket is gone and the reader has stopped using it, and
// Finish's is that the transcript it returns is the one the reader had finished
// folding. A teardown that returns without the wait has not broken a type, a
// signature or any assertion an ordinary case can make — the caller still gets
// its *Result, the transcript is usually already complete, and on an idle machine
// the reader almost always got there first.
//
// WHY THIS IS A SOURCE GUARD AND NOT A BEHAVIOURAL CASE (the measured part, so a
// later reader does not read this as laziness). The case WAS written first and it
// does not work: a fake server that observes the close runs INSIDE the test
// binary, so it stops reading at the same instant Close stops waiting — the
// process does not exit while another goroutine is blocked in a read, and by the
// time the test asks, both orders have converged. Measured: session_test.go's
// TestSessionCloseWaitsForTheServicesOwnReadOfTheClose is green with the await
// DELETED (mutation run, 2026-10-09). What the behavioural cases in that file DO
// prove is what the landing design actually delivered — the frames the service
// read, the tail carried in front of the end-of-stream flag, and the abort path
// that sends no flag. The absence of the join is a fact about the source.
//
// THE SHAPE IS THE ONE THIS REPO ALREADY USES for a property no runtime
// observation can reach: parse the package's own source with go/ast and assert on
// the compiler's idea of a function body (precedents: internal/agent's
// console_provenance_pin_guard_test.go, cmd/agent's merge_prompt_gate_test.go).
// It is anchored on NAMES rather than on text, so a reformat or a comment cannot
// retire it, and a rename fails loudly instead of passing by absence (Rule 15).
//
// HONEST LIMITATION. The guard reads ONE file (session.go) and one method per
// assertion. A teardown moved to a helper — Close calling something that itself
// awaits — would trip it; that is deliberate (the await must be where the
// contract is stated) and the failure message says which shape it wants. It
// cannot see a session type introduced in another file, and it makes no claim
// about the wait's DURATION, which is sessionCloseWait's business.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// sessionSourceFile is the file the teardown contract lives in.
const sessionSourceFile = "session.go"

// sessionAwaitMethod is the ONE method that performs the join. Every awaited
// teardown calls it, so a second spelling of the wait cannot appear unnoticed.
const sessionAwaitMethod = "awaitReader"

// sessionTeardownAwaits are the teardowns that must call it: the abort path and
// the end-of-utterance path. Both are terminal, and both hand the caller a
// statement about a socket and a result that is only true once the reader has
// returned.
var sessionTeardownAwaits = []string{"Close", "Finish"}

// sessionAwaitSignals are the two facts every join must be built from: the
// channel the reader closes when it returns, and the bound that keeps a reader
// which ignored the close from holding the caller forever (Rule 15: the wait is
// bounded).
var sessionAwaitSignals = []string{"readerDone", "sessionCloseWait"}

// TestSessionTeardownAwaitsTheReader is the guard.
func TestSessionTeardownAwaitsTheReader(t *testing.T) {
	path := sessionSourcePath(t)
	src, err := os.ReadFile(path)
	if err != nil {
		// A guard that cannot read what it guards must FAIL, never pass quietly.
		t.Fatalf("read %s: %v", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, name := range sessionTeardownAwaits {
		body := methodBody(t, file, name)
		if !bodyCallsMethod(body, sessionAwaitMethod) {
			t.Errorf("%s does not call %s — a teardown that returns without the join hands its caller a statement about a socket and a result that are not yet true (see this file's header for why no behavioural case can catch this)",
				name, sessionAwaitMethod)
		}
	}

	// THE WAIT ITSELF, so the join cannot be satisfied by an empty method: it must
	// be a select on the reader's channel and on the named bound.
	awaitBody := methodBody(t, file, sessionAwaitMethod)
	for _, want := range sessionAwaitSignals {
		if !bodyMentionsIdent(awaitBody, want) {
			t.Errorf("%s's body does not mention %s — the join must wait on the reader's own channel and be bounded by the named constant, not by an invented duration",
				sessionAwaitMethod, want)
		}
	}
}

// sessionSourcePath resolves this package's session.go through the compiler's own
// record of where this file came from, so the guard reads the tree it was built
// from rather than a hard-coded path (Rule 22: no path is spelled twice).
func sessionSourcePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller could not name this test file")
	}
	return filepath.Join(filepath.Dir(thisFile), sessionSourceFile)
}

// methodBody returns the body of the *Session method named name, or fails.
func methodBody(t *testing.T, file *ast.File, name string) *ast.BlockStmt {
	t.Helper()
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != name {
			continue
		}
		if len(fn.Recv.List) != 1 || !isSessionPointer(fn.Recv.List[0].Type) {
			continue
		}
		if fn.Body == nil {
			t.Fatalf("%s has no body to read", name)
		}
		return fn.Body
	}
	t.Fatalf("session.go declares no method (*Session).%s — a teardown this guard names no longer exists under that name, which is a finding, not a pass", name)
	return nil
}

// isSessionPointer reports whether a receiver type is *Session.
func isSessionPointer(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "Session"
}

// bodyCallsMethod reports whether the body contains a call to the named method —
// spelled as a method so a local function with the same name cannot satisfy it.
func bodyCallsMethod(body *ast.BlockStmt, method string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == method {
			found = true
		}
		return true
	})
	return found
}

// bodyMentionsIdent reports whether the body names the given identifier anywhere
// (a channel read, a constant in a select arm) — the check is on the compiler's
// own identifier nodes, so a comment mentioning the name does not satisfy it.
func bodyMentionsIdent(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok && ident.Name == name {
			found = true
		}
		return true
	})
	return found
}
