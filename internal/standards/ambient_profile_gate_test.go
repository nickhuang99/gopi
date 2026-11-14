package standardgate

// ---------------------------------------------------------------------------
// ambient-profile gate — a TEST may not resolve the developer's real ~/.gopi
//
// WHY THIS EXISTS (measured 2026-09-14, task 01a09df5 "worker is gone"): the
// operator's `gopi worker --status` reported 14 phantom "worker pid N" entries.
// NOT one of them was a worker. Every file was written by a `go test
// ./cmd/agent/` process whose fixture called `tools.ControlDir(workerStateDirName)`
// IN-PROCESS — and `ControlDir` resolves `config.GopiDir()`: `$GOPI_HOME/.gopi`
// when set, else the developer's REAL `$HOME/.gopi`.
//
// The fixture sandboxed only its CHILD (`runGopiIn`'s env vector) while the
// parent's own resolution stayed ambient, so the seed landed in the operator's
// real `~/.gopi/worker`, named after the TEST BINARY's pid. The test still
// PASSED, because it only ever asserted on the private home it handed the child.
// Every later `go test ./cmd/agent/` added one more file (14 of them, each a
// different test-binary pid), and `worker --status` rendered them as workers
// that had "gone away".
//
// The mistake is INVISIBLE to its author — the leaking test is green and the
// damage surfaces in ANOTHER process's report (the operator's). A file-level pin
// is the fix; this gate is the ratchet that makes a NEW fixture re-introducing
// the pattern fail the build instead of the operator's next status read.
//
// TWO EXITS, both explicit:
//   - the PACKAGE pins the profile for the whole suite in TestMain
//     (`testutil.PinSuiteGopiProfile`) — then no function in it can resolve the
//     ambient root at all, and every function in that package is covered;
//   - or the FUNCTION itself pins it (any call whose arguments name
//     `config.GopiDirEnv` or the "GOPI_HOME" literal), which is what a test
//     that deliberately wants a NAMED profile does.
//
// A site that is correct for a reason the predicate cannot see carries the
// inline escape `// ambient-profile-ok: <reason>` on the line above the call.

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	// ambientProfileEscape marks a reviewed in-process resolution.
	ambientProfileEscape = "// ambient-profile-ok:"
	// suiteProfilePinFunc is the suite-level pin (internal/testutil) that makes
	// the whole package hermetic — the primary exit.
	suiteProfilePinFunc = "PinSuiteGopiProfile"
	// profileEnvConstName is config.GopiDirEnv, the knob a per-function pin
	// passes. Spelled by IDENTIFIER (not its value): the predicate matches the
	// constant's NAME wherever a call takes it as an argument.
	profileEnvConstName = "GopiDirEnv"
	// profileEnvLiteral is the same knob's wire value — a fixture that pins
	// without importing config still pins.
	profileEnvLiteral = "GOPI_HOME"
	// testMainFunc is the suite entry point whose pin covers a whole package.
	testMainFunc = "TestMain"
)

// ambientProfileResolvers are the calls whose RESULT depends on the ambient
// profile: every one funnels into config.GopiDir(). Named, so a new resolver is
// added here with its reason instead of being missed.
var ambientProfileResolvers = map[string]bool{
	"GopiDir":            true,
	"ControlDir":         true,
	"ControlDirReadOnly": true,
}

// callFuncName returns the called function's name, or "" when the call's target
// is not a plain name.
func callFuncName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

// callNamesProfilePin reports whether a call pins the gopi profile: the
// testutil suite pin itself, or ANY call whose arguments name the env knob
// (config.GopiDirEnv by identifier, or the "GOPI_HOME" literal). Recipient
// independent on purpose — t.Setenv, os.Setenv and a helper funnel all pin.
func callNamesProfilePin(call *ast.CallExpr) bool {
	if callFuncName(call) == suiteProfilePinFunc {
		return true
	}
	for _, arg := range call.Args {
		switch a := arg.(type) {
		case *ast.Ident:
			if a.Name == profileEnvConstName {
				return true
			}
		case *ast.SelectorExpr:
			// config.GopiDirEnv — the constant reached through its package.
			if a.Sel.Name == profileEnvConstName {
				return true
			}
		case *ast.BasicLit:
			if a.Kind != token.STRING {
				continue
			}
			if s, err := strconv.Unquote(a.Value); err == nil && s == profileEnvLiteral {
				return true
			}
		}
	}
	return false
}

// bodyPinsProfile reports whether a function body pins the profile anywhere.
// Deliberately conservative: a pin that runs on only one branch still counts,
// because the predicate cannot judge reachability and a false RED here is what
// makes the gate get switched off.
func bodyPinsProfile(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	pinned := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && callNamesProfilePin(call) {
			pinned = true
			return false
		}
		return true
	})
	return pinned
}

// ambientResolveLines returns the source lines of every ambient-profile
// resolution in a body. Only SELECTOR calls count (`config.GopiDir()`,
// `tools.ControlDir()`): a bare identifier call is a local helper, whose own
// resolution is judged where it is written.
func ambientResolveLines(p *parsed, body *ast.BlockStmt) []int {
	if body == nil {
		return nil
	}
	var lines []int
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !ambientProfileResolvers[sel.Sel.Name] {
			return true
		}
		lines = append(lines, p.fset.Position(call.Pos()).Line)
		return true
	})
	return lines
}

// funcDecls lists a file's top-level function declarations in source order.
func funcDecls(p *parsed) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	for _, d := range p.file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return p.fset.Position(out[i].Pos()).Offset < p.fset.Position(out[j].Pos()).Offset
	})
	return out
}

// suitePinnedFiles returns the repo-relative files of a package directory whose
// TestMain pins the suite profile. Non-empty ⇒ the whole package is hermetic.
func suitePinnedFiles(t *testing.T, root, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	if err != nil {
		return nil
	}
	var pinned []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(dir, e.Name()))
		p := parseRepoFile(t, root, rel)
		for _, f := range funcDecls(p) {
			if f.Name.Name == testMainFunc && bodyPinsProfile(f.Body) {
				pinned = append(pinned, rel)
			}
		}
	}
	return pinned
}

// ambientProfileHits reports one package's un-pinned in-process resolutions.
func ambientProfileHits(t *testing.T, root, dir string, files []string) []string {
	t.Helper()
	if len(suitePinnedFiles(t, root, dir)) > 0 {
		return nil
	}
	var hits []string
	for _, rel := range files {
		p := parseRepoFile(t, root, rel)
		for _, fn := range funcDecls(p) {
			if bodyPinsProfile(fn.Body) {
				continue
			}
			for _, line := range ambientResolveLines(p, fn.Body) {
				if markerOn(linesAbove(p.src, line), ambientProfileEscape) {
					continue
				}
				hits = append(hits, rel+":"+strconv.Itoa(line)+
					"  [func "+fn.Name.Name+" resolves the ambient gopi profile]")
			}
		}
	}
	return hits
}

// TestNoTestResolvesTheAmbientGopiProfile is the gate over the real tree: every
// package that resolves the gopi root from a test must pin the profile — in the
// suite's TestMain, or in the function itself.
func TestNoTestResolvesTheAmbientGopiProfile(t *testing.T) {
	root := repoRoot(t)
	byDir := map[string][]string{}
	for _, rel := range goFilesUnder(t, root, testRoots, true) {
		dir := filepath.Dir(rel)
		byDir[dir] = append(byDir[dir], rel)
	}
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	var pinnedDirs, hits []string
	for _, dir := range dirs {
		if len(suitePinnedFiles(t, root, dir)) > 0 {
			pinnedDirs = append(pinnedDirs, dir)
			continue
		}
		hits = append(hits, ambientProfileHits(t, root, dir, byDir[dir])...)
	}
	t.Logf("ambient-profile gate: %d test package(s) examined, %d pinned for the whole suite",
		len(dirs), len(pinnedDirs))
	for _, d := range pinnedDirs {
		t.Logf("  pinned: %s", d)
	}
	if len(hits) == 0 {
		return
	}
	sort.Strings(hits)
	for _, h := range hits {
		t.Errorf("ambient-profile resolution in a test at %s\n"+
			"  an in-process config.GopiDir()/tools.ControlDir() resolves the DEVELOPER'S real profile\n"+
			"  (a child's env vector cannot relocate the parent). Pin it: testutil.PinSuiteGopiProfile\n"+
			"  in the package TestMain, or t.Setenv(config.GopiDirEnv, <sandbox>) in the function;\n"+
			"  a deliberate real-tree read carries %s <reason> on the line above.",
			h, ambientProfileEscape)
	}
}

// TestAmbientProfileGateRedProof feeds the predicate the EXACT pre-fix shape of
// the fixture that caused the phantom-worker incident (measured 2026-09-14:
// status_action_conflict_e2e_test.go seeded a worker-state file into the
// operator's real profile) and asserts the predicate sees it, plus that both
// exits silence it. Without this the gate could rot to a no-op and stay green.
func TestAmbientProfileGateRedProof(t *testing.T) {
	const preFix = `package main

import "github.com/nickhuang99/gopi/internal/tools"

func TestStatusWithActingActionTouchesNoRequestFile(t *testing.T) {
	home := t.TempDir()
	env := sscPrivateHome(t, home)
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Skipf("cannot resolve: %v", err)
	}
	_ = tools.WriteControlText(tools.ControlStatePath(dir, workerStatePrefix, os.Getpid()), []byte("{}"))
	_, _ = runGopiIn(t, bin, env, "", CliVerbWorker, workerControlStatus)
}
`
	if got := unescapedResolveCount(t, "probe_test.go", preFix); got != 1 {
		t.Fatalf("red proof: the leaking fixture reported %d resolution(s), want 1", got)
	}
	// (a) a per-function pin by CONSTANT name silences it.
	pinConst := strings.Replace(preFix,
		"\thome := t.TempDir()",
		"\thome := t.TempDir()\n\tt.Setenv(config.GopiDirEnv, home)", 1)
	if got := unescapedResolveCount(t, "probe_test.go", pinConst); got != 0 {
		t.Errorf("red proof: %d resolution(s) survive a config.GopiDirEnv pin, want 0", got)
	}
	// (b) the same pin by the env LITERAL silences it.
	pinLiteral := strings.Replace(preFix,
		"\thome := t.TempDir()",
		"\thome := t.TempDir()\n\tt.Setenv(\""+profileEnvLiteral+"\", home)", 1)
	if got := unescapedResolveCount(t, "probe_test.go", pinLiteral); got != 0 {
		t.Errorf("red proof: %d resolution(s) survive a %q literal pin, want 0", got, profileEnvLiteral)
	}
	// (c) the inline escape silences it.
	escaped := strings.Replace(preFix,
		"\tdir, err := tools.ControlDir(",
		"\t"+ambientProfileEscape+" exercised against the real tree on purpose\n\tdir, err := tools.ControlDir(", 1)
	if got := unescapedResolveCount(t, "probe_test.go", escaped); got != 0 {
		t.Errorf("red proof: %d resolution(s) survive the escape marker, want 0", got)
	}
	// (d) an UNRELATED call is NOT mistaken for a resolution.
	nonResolver := strings.Replace(preFix, "tools.ControlDir(", "tools.SomeOtherThing(", 1)
	if got := unescapedResolveCount(t, "probe_test.go", nonResolver); got != 0 {
		t.Errorf("red proof: an unrelated selector call was reported as a resolution (%d hit(s))", got)
	}
}

// unescapedResolveCount parses one probe source and counts the resolutions that
// neither a body-level pin nor the escape marker covers.
func unescapedResolveCount(t *testing.T, name, src string) int {
	t.Helper()
	fset, f := parseSource(t, name, src)
	p := &parsed{rel: name, fset: fset, file: f, src: []byte(src)}
	n := 0
	for _, fn := range funcDecls(p) {
		if bodyPinsProfile(fn.Body) {
			continue
		}
		for _, line := range ambientResolveLines(p, fn.Body) {
			if !markerOn(linesAbove(p.src, line), ambientProfileEscape) {
				n++
			}
		}
	}
	return n
}
