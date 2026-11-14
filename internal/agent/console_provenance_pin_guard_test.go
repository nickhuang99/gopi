package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// console_provenance_pin_guard_test.go — the family-A guard for THIS package's
// console-provenance fixtures (the precedent is internal/tools'
// task_display_env_guard_test.go, which polices the same defect class for
// `task show`'s rendered provenance lines).
//
// THE DEFECT THIS PREVENTS. A fixture that asserts a provenance reading is
// asserting a value DERIVED from the process's environment
// (tasksource.Derive → config.AgentSpawnSignatureNow). Unpinned, it measures the
// shell that launched `go test` instead of the code under test — and the ambient
// direction is INVISIBLE to a bare `go test`, so the reviewer who would notice is
// exactly the one whose environment hides it. MEASURED 2026-10-04 on master
// 05c7d78ca: TestChatProvenanceCaptureIsIdempotent is green in a clean
// environment and red under GOPI_HUMAN_TERMINAL=1 ("the second capture
// overwrote the first: \"human\", want \"agent\"") — the heavy path exports that
// variable for the WHOLE suite (`make test-heavy`), so the red arrives on the
// heaviest run, in a fixture that has nothing to do with heaviness.
//
// WHY A GUARD AND NOT ONLY THE PIN. The pin landed in that one fixture; nothing
// catches the NEXT capture fixture written without it — and because
// AgentRun() consults HumanTerminal FIRST, an inherited claim silently converts
// every one of them from an agent reading to a human one.
//
// WHAT IT CHECKS:
//
//  1. COVERAGE — every test function in this package whose body takes a console
//     provenance reading (calls CaptureConsoleProvenance) must also PIN the
//     human-terminal claim, by name (config.HumanTerminalEnv) or by its value.
//     A fixture asserting the OTHER half, chatSessionSource()'s live derivation,
//     reads the environment the same way and is caught by the same rule.
//  2. LISTING INTEGRITY — every fixture named in consoleProvenanceGuardedFixtures
//     must still exist AND still take a reading, so a rename or a quietly
//     dropped assertion cannot retire the guard (Rule 15: a check that can pass
//     by absence is not a check).
//
// WHY THE GUARD READS THE TEST SOURCE. Asserting on a RENDERED reading needs the
// ambient environment, which is the very variable being controlled. The parse is
// go/ast over this package's own *_test.go files, so a body's boundary is the
// compiler's idea of it, not a brace count.
//
// HONEST LIMITATION (Rule 15 — a check that overstates its coverage is worse than
// one that never landed). Three shapes escape a source scan:
//   - a reading reached through a HELPER the scan cannot resolve (the guarded
//     fixture itself stays clean while its helper reads the environment);
//   - a fixture that pins the claim through some OTHER spelling the two accepted
//     ones do not name (testutil.PinBareTerminalProcess pins it too — the
//     failure message says so, and the honest fix is an explicit pin rather than
//     a wider matcher);
//   - a capture call written as a method value or via reflection.
//
// The first is the real hole, and it is narrow: the fixture must both delegate
// its reading to a helper AND be named in the listing below to matter.
//
// The mirror direction is LOUD, never silent: the selector name below is a
// mirror of config's exported identifier (a source scan cannot reflect on an
// identifier's name), so renaming that constant makes this guard report a
// correctly-pinned fixture as a violation rather than letting an unpinned one
// pass.

// consoleProvenanceGuardFile is this guard's own file name — skipped by the
// scan, so the guard's red-proof snippets are never read as the contracts it
// polices.
const consoleProvenanceGuardFile = "console_provenance_pin_guard_test.go"

// consoleProvenanceCaptureCall is the function a fixture calls to TAKE the
// reading (chatregister.go).
const consoleProvenanceCaptureCall = "CaptureConsoleProvenance"

// consoleProvenancePinSelector is the exported identifier of
// config.HumanTerminalEnv — the var a fixture pins. The VALUE is never spelled
// here: the scan compares a string literal against config.HumanTerminalEnv
// itself (Rule 13/14), so a change to that value cannot leave this guard
// matching a variable that no longer exists.
const consoleProvenancePinSelector = "HumanTerminalEnv"

// consoleProvenanceGuardedFixtures names the capture fixtures that must keep both
// halves of their contract: the reading, and the pin. Dropping a name here is the
// same act as dropping the fixture's coverage, so it belongs in a commit with the
// reason written down.
var consoleProvenanceGuardedFixtures = []string{
	"TestCaptureConsoleProvenanceSurvivesTheAnchorOverwrite",
	"TestChatProvenanceIsHumanWithoutAnAgentChain",
	"TestChatProvenanceCaptureIsIdempotent",
}

// TestConsoleProvenanceFixturesPinTheHumanClaim is the family-A guard itself.
func TestConsoleProvenanceFixturesPinTheHumanClaim(t *testing.T) {
	decls, scanned := consoleProvenancePackageFixtures(t)

	reads := 0
	seen := map[string]bool{}
	for _, fn := range decls {
		if !consoleProvenanceAssertsReading(fn) {
			continue
		}
		reads++
		seen[fn.Name.Name] = true
		if consoleProvenancePinsTheClaim(fn) {
			continue
		}
		t.Errorf("%s takes a console provenance reading without pinning the human-terminal claim.\n"+
			"Add `t.Setenv(config.HumanTerminalEnv, \"\")` (or the enabling spelling, if the fixture "+
			"asserts the human reading) before the first CaptureConsoleProvenance call: the reading is "+
			"DERIVED from the process's environment, so an inherited %s — which `make test-heavy` exports "+
			"for the whole suite — flips it to `human` and this fixture measures the shell that launched "+
			"`go test` instead of the ordering it exists to pin. MEASURED 2026-10-04 on master 05c7d78ca: "+
			"green in a clean environment, red under %s=1. (A pin through %s also sets this variable; "+
			"state it explicitly here so the fixture says WHICH signature it asserts.)\n"+
			"Family A: internal/tools/task_display_env_guard_test.go, devdoc/internal-tools-whole-package-triage-20260925.md",
			fn.Name.Name, config.HumanTerminalEnv, config.HumanTerminalEnv, consoleProvenancePinHelperCall)
	}

	for _, name := range consoleProvenanceGuardedFixtures {
		if !seen[name] {
			t.Errorf("guarded fixture %q takes no console provenance reading in this package — it was "+
				"renamed, deleted, or its assertion was dropped, and a guard covering nothing reports "+
				"green while the contract it was written for is unasserted. Update "+
				"consoleProvenanceGuardedFixtures in the same commit, or restore the reading.", name)
		}
	}

	if reads == 0 {
		t.Fatalf("the scan found %d console provenance readings across %d test functions — a zero means "+
			"the scan is looking at the wrong files (vacuous pass guard)", reads, scanned)
	}
	if scanned < consoleProvenanceMinScannedFixtures {
		t.Fatalf("the scan parsed only %d test functions of this package's suite (want at least %d) — the "+
			"file walk is not reaching the package's test sources (vacuous pass guard)",
			scanned, consoleProvenanceMinScannedFixtures)
	}

	t.Run("the_predicate_discriminates", func(t *testing.T) {
		// A green guard and a no-op guard look identical, so the decision these
		// errors ride on is exercised here against synthetic fixtures (Rule 30's
		// minimal sample: the smallest input that separates the shapes).
		unpinned := consoleProvenanceDecl(t, `func TestUnpinned(t *testing.T) {
	resetConsoleProvenanceForTest()
	t.Setenv(config.AgentSessionEnv, "probe-session")
	CaptureConsoleProvenance()
	if got := chatSessionSource(); got != tasksource.Agent {
		t.Errorf("got %q", got)
	}
}`)
		pinned := consoleProvenanceDecl(t, `func TestPinned(t *testing.T) {
	t.Setenv(config.HumanTerminalEnv, "")
	CaptureConsoleProvenance()
}`)
		pinnedByValue := consoleProvenanceDecl(t, `func TestPinnedByValue(t *testing.T) {
	t.Setenv("GOPI_HUMAN_TERMINAL", "1")
	CaptureConsoleProvenance()
}`)
		unrelated := consoleProvenanceDecl(t, `func TestUnrelated(t *testing.T) {
	t.Setenv(config.AgentSessionEnv, "")
	if got := tasksource.DeriveNow(""); got != tasksource.Human {
		t.Errorf("got %q", got)
	}
}`)
		// A fixture that MERELY NAMES the capture in text is not a reading: the
		// guard must key on the call, not on the word, or it would demand a pin of
		// every fixture that documents this one.
		mentionsInText := consoleProvenanceDecl(t, `func TestMentions(t *testing.T) {
	// CaptureConsoleProvenance is called by cmd/agent at startup
	_ = "CaptureConsoleProvenance()"
}`)

		for _, c := range []struct {
			name       string
			fn         *ast.FuncDecl
			wantRead   bool
			wantPinned bool
		}{
			{"unpinned", unpinned, true, false},
			{"pinned-by-name", pinned, true, true},
			{"pinned-by-value", pinnedByValue, true, true},
			{"no-reading", unrelated, false, false},
			{"reading-named-only-in-text", mentionsInText, false, false},
		} {
			if got := consoleProvenanceAssertsReading(c.fn); got != c.wantRead {
				t.Errorf("%s: consoleProvenanceAssertsReading = %t, want %t", c.name, got, c.wantRead)
			}
			if got := consoleProvenancePinsTheClaim(c.fn); got != c.wantPinned {
				t.Errorf("%s: consoleProvenancePinsTheClaim = %t, want %t", c.name, got, c.wantPinned)
			}
		}
	})
}

// consoleProvenancePinHelperCall names the shared pin helper a fixture MAY use
// instead of an explicit t.Setenv — named here only so the failure message can
// tell the reader that such a pin exists and why this guard still wants the
// fixture to state the claim itself.
const consoleProvenancePinHelperCall = "testutil.PinBareTerminalProcess"

// consoleProvenanceMinScannedFixtures is a floor, not an expectation: the package
// carries 128 test files, so a scan that parses a handful is not looking at the
// suite. It is deliberately far below the real count — a floor that had to be
// maintained would be a second thing to keep true.
const consoleProvenanceMinScannedFixtures = 50

// consoleProvenancePackageFixtures parses every test file beside this guard and
// returns their top-level test functions, plus how many were parsed.
func consoleProvenancePackageFixtures(t *testing.T) ([]*ast.FuncDecl, int) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(consoleProvenanceGuardDir(t), "*_test.go"))
	if err != nil {
		t.Fatalf("glob the package's test files: %v", err)
	}
	var decls []*ast.FuncDecl
	scanned := 0
	for _, path := range paths {
		if filepath.Base(path) == consoleProvenanceGuardFile {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			// Never swallowed (Rule 15b): an unparsed file is a file the guard did
			// not cover, and a silent skip would report a clean scan.
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			scanned++
			decls = append(decls, fn)
		}
	}
	return decls, scanned
}

// consoleProvenanceGuardDir resolves this file's own directory, so the scan works
// whatever working directory the suite runs in.
func consoleProvenanceGuardDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller: cannot locate the guard's own directory")
	}
	return filepath.Dir(thisFile)
}

// consoleProvenanceDecl parses ONE synthetic fixture, so the red proof exercises
// the real predicate on the smallest possible input.
func consoleProvenanceDecl(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "snippet_test.go", "package agent\n\n"+src, 0)
	if err != nil {
		t.Fatalf("parse the red-proof snippet: %v\n%s", err, src)
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn
		}
	}
	t.Fatalf("the red-proof snippet declares no function:\n%s", src)
	return nil
}

// consoleProvenanceAssertsReading reports whether a fixture takes a console
// provenance reading. Comments are not parsed (ParseFile without ParseComments),
// and a mention inside a string is a BasicLit, not an Ident — so only a real call
// counts.
func consoleProvenanceAssertsReading(fn *ast.FuncDecl) bool {
	return consoleProvenanceAnyNode(fn, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		return ok && id.Name == consoleProvenanceCaptureCall
	})
}

// consoleProvenancePinsTheClaim reports whether a fixture pins the human-terminal
// claim — by naming the constant, or by setting the variable to its value
// (compared against config.HumanTerminalEnv, never a copy of the spelling).
func consoleProvenancePinsTheClaim(fn *ast.FuncDecl) bool {
	return consoleProvenanceAnyNode(fn, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			return v.Sel != nil && v.Sel.Name == consoleProvenancePinSelector
		case *ast.Ident:
			return v.Name == consoleProvenancePinSelector
		case *ast.BasicLit:
			return v.Kind == token.STRING &&
				strings.Trim(v.Value, `"`) == config.HumanTerminalEnv
		}
		return false
	})
}

// consoleProvenanceAnyNode reports whether any node in the body satisfies pred.
func consoleProvenanceAnyNode(fn *ast.FuncDecl, pred func(ast.Node) bool) bool {
	if fn == nil || fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil || found {
			return false
		}
		if pred(n) {
			found = true
			return false
		}
		return true
	})
	return found
}
