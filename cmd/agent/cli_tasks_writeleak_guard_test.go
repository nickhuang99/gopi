// cli_tasks_writeleak_guard_test.go — the "mocked tool call must not write
// into the repo" guard.
//
// WHY: TestTaskRunNormalExitSingleRowE2E used to feed the write tool a
// RELATIVE path ("out.txt") through a mocked LLM tool call. The tool executes
// in a process whose cwd is whatever the runner started in — for `go test`
// that is the PACKAGE DIRECTORY — so `go test ./cmd/agent/` left an untracked
// cmd/agent/out.txt (contents "done") in every checkout that ran the package.
// It was never gitignored, so it polluted `git status` and could be swept into
// a commit by any `git add` of the package directory.
//
// The fix has two halves, and this file owns the second one:
//  1. the e2e feeds an ABSOLUTE path inside its own t.TempDir() and asserts
//     the artifact is really there (so the mocked write is still exercised)
//     and that nothing landed in the package directory;
//  2. the static gate below makes the whole CLASS unrepeatable — any mocked
//     tool-call argument map in this package that names the write tool's path
//     argument with a relative string literal fails the build.
//
// Rule 13 (a constant exists to be referenced): the argument names and the
// artifact name are declared once here, never re-typed at a call site.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/toolnames"
)

const (
	// writeToolOutName is the artifact the waitpid e2e's mocked write tool call
	// produces. Also the name the runtime leak assertion looks for in the
	// package directory (it must NOT be there).
	writeToolOutName = "out.txt"
	// writeToolPathArg and writeToolContentArg are the write tool's wire
	// argument names — the keys the model emits in a tool_use input and the
	// ones TestTaskRunNormalExitSingleRowE2E puts in its mocked call.
	writeToolPathArg    = "path"
	writeToolContentArg = "content"
)

// writeToolPathLiteralRe matches a mocked tool-call argument map that names the
// write tool's path argument with a STRING LITERAL value, capturing the
// literal. A map whose value is an identifier (a variable holding an absolute
// path) deliberately does not match — the gate judges what is written, not how
// the caller spelled it.
//
// `[^}]` (not `.`) spans newlines on purpose: a multi-line Args map is the
// common shape in this package and must be judged the same as a one-liner.
var writeToolPathLiteralRe = regexp.MustCompile(
	`map\[string\]interface\{\}\{[^}]*"` + writeToolPathArg + `":\s*"([^"]*)"`)

// relativeWriteToolPathLiterals returns every RELATIVE write-tool path literal
// found in one Go source file. Split out from the scan so the predicate itself
// can be red-proofed on synthetic sources — a gate that cannot be shown to
// flag the defect it exists for is a gate that reads green blind.
func relativeWriteToolPathLiterals(src []byte) []string {
	var out []string
	for _, m := range writeToolPathLiteralRe.FindAllSubmatch(src, -1) {
		lit := string(m[1])
		if lit == "" || filepath.IsAbs(lit) {
			continue
		}
		out = append(out, lit)
	}
	return out
}

// packageTestDir is the directory of THIS test file — the cmd/agent package
// directory, which is also the cwd `go test ./cmd/agent/` gives the test
// binary. Resolved from the source location rather than os.Getwd so it stays
// truthful no matter what another test in this binary did to the process cwd.
func packageTestDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not resolve this test file's path")
	}
	return filepath.Dir(file)
}

// TestWriteToolPathsInPackageTestsAreAbsolute is the static half of the guard:
// no test in this package may hand the write tool a relative path literal,
// because the executing process's cwd is the package directory.
func TestWriteToolPathsInPackageTestsAreAbsolute(t *testing.T) {
	dir := packageTestDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir %s: %v", dir, err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		for _, lit := range relativeWriteToolPathLiterals(src) {
			t.Errorf("%s: mocked %s tool call uses the RELATIVE path %q — it resolves against the test binary's cwd (%s) and drops an untracked artifact into the repo. Use an absolute path inside t.TempDir().",
				name, toolnames.ToolWrite, lit, dir)
		}
	}
	if scanned == 0 {
		t.Fatalf("scanned 0 test files in %s — the guard read green blind", dir)
	}
}

// TestRelativeWriteToolPathPredicateRedProof red-proofs the predicate above:
// it MUST flag the exact argument map that produced the leaked
// cmd/agent/out.txt, and must leave the corrected absolute-path forms alone.
// Without this, a regex that matches nothing would make the static guard
// vacuously green.
//
// The probe's source is ASSEMBLED at run time (fmt.Sprintf over %q) rather
// than typed as a literal: a literal would itself be a relative write-tool
// path in this very file, so the exhaustive scan above would flag its own
// red-proof. The assembled bytes fed to the predicate are byte-identical to
// the historical leak line — which is what the first assertion checks.
func TestRelativeWriteToolPathPredicateRedProof(t *testing.T) {
	leak := []byte("\tArgs: map[string]interface{}{" +
		fmt.Sprintf("%q: %q, %q: %q", writeToolPathArg, writeToolOutName, writeToolContentArg, "done") + "},")
	if !strings.Contains(string(leak), `"path": "out.txt"`) {
		t.Fatalf("probe source is not the historical leak spelling: %s", leak)
	}
	if got := relativeWriteToolPathLiterals(leak); len(got) != 1 || got[0] != writeToolOutName {
		t.Fatalf("predicate on the historical leak = %q, want [%q] — the guard cannot see the defect it exists for",
			got, writeToolOutName)
	}

	ok := []struct {
		name string
		src  string
	}{
		{"absolute one-liner", fmt.Sprintf(`Args: map[string]interface{}{%q: %q, %q: %q}`,
			writeToolPathArg, "/scratch/out.txt", writeToolContentArg, "done")},
		{"absolute multi-line", fmt.Sprintf("Args: map[string]interface{}{\n\t\t%q: %q,\n\t\t%q: %q,\n\t}",
			writeToolPathArg, "/scratch/out.txt", writeToolContentArg, "done")},
		{"identifier, not a literal", fmt.Sprintf(`Args: map[string]interface{}{%q: outPath, %q: %q}`,
			writeToolPathArg, writeToolContentArg, "done")},
		{"unrelated map key", `Args: map[string]interface{}{"command": "pwd"}`},
	}
	for _, c := range ok {
		if got := relativeWriteToolPathLiterals([]byte(c.src)); len(got) != 0 {
			t.Errorf("%s: predicate flagged %q, want nothing", c.name, got)
		}
	}
}
