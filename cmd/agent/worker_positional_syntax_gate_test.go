// worker_positional_syntax_gate_test.go — the task-id placeholder has ONE
// home, and a re-typing of it fails the build.
//
// WHY: `gopi worker <task-id>` renders its placeholder in a dozen operator-
// facing strings — the grammar synopsis, the three contradiction refusals, the
// unknown-word remedy, the accepted-vocabulary line, the sessions path in the
// output note. Each of those was a DOUBLE-QUOTED FORMAT STRING, which is
// exactly the shape that rots: the row became positional (epic
// worker-positional-taskid), the shell spelling moved again to
// `gopi worker [<task-id>]`, and every copy that meant the same shape had to be
// edited in lockstep. That is GopiPi.md Rule 13's defect — a value with many
// literal copies and no single home.
//
// WHAT IS GATED: every production `.go` file in this package is parsed, and any
// DOUBLE-QUOTED string literal whose VALUE contains the placeholder fails,
// except the one declaration site. Raw (backtick) literals are deliberately
// out of scope: a raw literal in this package is a multi-line usage PAGE whose
// text is already pinned verbatim by the help/width gates, not a format string
// that composes a message at a call site.
//
// THE PRESENCE HALF: a gate that only forbade literals would also pass if the
// constant were deleted (nothing left to reference) — so the same scan asserts
// the declaration exists in the holder file. Rule 25's shape: a read-floor plus
// the half that stops the correction from being satisfied by removal.

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// workerPositionalSyntaxHolder is the ONE production file allowed to carry the
// placeholder as a double-quoted literal — the file that DECLARES the constant
// (workerPositionalSyntax, cli_worker_daemon.go). Every other file references
// the constant; a copy is the drift this gate exists to stop.
const workerPositionalSyntaxHolder = "cli_worker_daemon.go"

// TestWorkerPositionalPlaceholderHasOneHome walks every production `.go` file in
// this package and fails on a double-quoted literal carrying the placeholder
// outside the holder, then asserts the holder still declares it.
func TestWorkerPositionalPlaceholderHasOneHome(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	scanned, holderDeclares := 0, false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// Raw literals are usage pages, not format strings — see the header.
			if strings.HasPrefix(lit.Value, "`") {
				return true
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if !strings.Contains(val, workerPositionalSyntax) {
				return true
			}
			if name == workerPositionalSyntaxHolder {
				return true
			}
			t.Errorf("%s:%d carries the task-id placeholder %q as a literal — reference workerPositionalSyntax (declared in %s): %s",
				name, fset.Position(lit.Pos()).Line, workerPositionalSyntax, workerPositionalSyntaxHolder, lit.Value)
			return true
		})
	}
	// Read-floor: a broken glob must not make this gate pass vacuously.
	if scanned == 0 {
		t.Fatalf("no production .go file scanned — the gate is reading blind")
	}
	// Presence half: the literal may live in the holder ONLY because the
	// constant is declared there; without it there is nothing to reference.
	src, err := os.ReadFile(workerPositionalSyntaxHolder)
	if err != nil {
		t.Fatalf("read the holder %s: %v", workerPositionalSyntaxHolder, err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(line, "const workerPositionalSyntax =") {
			holderDeclares = true
		}
	}
	if !holderDeclares {
		t.Fatalf("%s no longer declares workerPositionalSyntax — the placeholder has no home (a gate satisfied by deletion is not a gate)",
			workerPositionalSyntaxHolder)
	}
}
