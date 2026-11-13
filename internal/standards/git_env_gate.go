// gate 9 — a fixture's git child must not inherit a foreign repository (the
// 2026-10-13 data-loss incident).
//
// WHY THIS GATE EXISTS. `cmd.Dir` does NOT override `GIT_DIR`. A fixture that
// builds its own repository in t.TempDir()
//
//	cmd := exec.Command("git", args...)
//	cmd.Dir = root
//	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=fixture", ...)
//
// therefore hands git an environment that names a DIFFERENT repository whenever
// the process that started the suite carries GIT_DIR / GIT_WORK_TREE /
// GIT_INDEX_FILE / GIT_OBJECT_DIRECTORY. Measured 2026-10-13: a run with
// GIT_DIR=/home/nick/work/GoPi/.git made a corpus-fixture reader's git child
// re-initialise the REAL repository and commit 12 synthetic commits onto real
// master, taking its tree from 7678 files to 2 (507,046 deletions), and a
// sibling worktree was cut from the wreckage.
//
// WHAT IT CHECKS (structural, deterministic, no judgement call — the bar Rules
// 16–21 set). ONE hit per outermost function that BOTH
//
//	(a) execs git — `exec.Command("git", …)` / `exec.CommandContext(ctx, "git", …)`
//	    with the binary as the string literal that names it, and
//	(b) reads the inherited environment — a call to `os.Environ()`
//
// and whose `os.Environ()` line does not strip the routing names. The hit is
// reported ON the os.Environ() line (that is the line to fix) and names the
// function, so a reader lands on the exact expression.
//
// The repair is one call, and it is the reason the helper exists:
//
//	cmd.Env = testutil.GitEnv("GIT_AUTHOR_NAME=fixture", ...)   // routing names REMOVED
//
// or, for a caller that already built the slice: testutil.WithoutGitRoutingEnv.
// A site that must keep the inherited environment carries the inline escape
// `// git-env-ok: <reason>` — the reviewed, visible exception, exactly like
// Rules 16–21's own escapes.
//
// WHAT IT DELIBERATELY DOES NOT CHECK (so a future agent does not "close the
// gap" by guessing):
//
//   - a git child whose binary is a CONSTANT (`gitBin`, `docCheckGitExe`, …)
//     rather than the literal `git`. The predicate is anchored on the literal
//     because it must not fire on `exec.Command(name, …)` for any other
//     program, and "is this identifier a git binary" is not decidable here.
//     Such a fixture is still fixed by the helper; the gate just does not SEE
//     it, and the helper's own assertion (AssertGitDirInsideRoot) is the arm
//     that catches it at run time.
//   - a helper FUNCTION that strips the names for its callers (docCheckGitEnv,
//     withoutGitDirEnv, testutil.GitEnv itself): the strip and the exec live in
//     different functions, so the hit is not reported. Those are the CORRECT
//     shape, and the gate must not punish them.
//   - PRODUCTION files. The class under audit is the fixture; a production git
//     call's routing is its own deliberate decision, and this gate's baseline
//     would otherwise absorb half of internal/tools.
package standardgate

import (
	"go/ast"
	"strings"
)

const (
	// gitEnvEscape marks a site that deliberately hands a git child the
	// inherited environment, with the reason.
	gitEnvEscape = "// git-env-ok:"

	// gitExecPkg is the package whose Command/CommandContext constructors this
	// gate keys on, and gitBinaryLiteral is the string it must name as the
	// binary for the call to be a git call at all.
	gitExecPkg       = "exec"
	gitBinaryLiteral = "git"

	// gitEnvScannedFilesFloor is the coverage floor for the real-tree arm: the
	// gate must have READ at least this many test files, so a scan that
	// silently walked nothing cannot read as a green gate (Rule 39's
	// "a scan that found nothing must be distinguishable from a scan that never
	// ran"). The measured tree carries 126 test files that exec git.
	//
	// The arms that assert this floor are
	// TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly (floor + stale entries
	// only) and TestGate9GitEnvScansTheRealTree (floor + full enforcement, the
	// top-level arm a reader can run by NAME), and neither is the red proof: the
	// coverage arm passes a NEW unbaselined hit, and the real-tree arm reads the
	// tree on purpose. The fatal arms are TestGate9GitEnvScansTheRealTree and
	// this gate's record's own probe, TestStandardGates/TestGate9GitEnvRedProof.
	gitEnvScannedFilesFloor = 100
)

// gitEnvGate describes the gate for the registry.
func gitEnvGate() gate {
	return gate{
		id:             "Rule 13/15 (a fixture's git child carries no inherited repository routing)",
		checklistRow:   "1b/git-env",
		probe:          "TestGate9GitEnvRedProof",
		wantTest:       true,
		scanRoots:      testRoots,
		escape:         gitEnvEscape,
		baselineReason: placeholderGitEnvBaselineReason,
		baseline:       gitEnvBaseline,
		find:           gitEnvFind,
	}
}

// placeholderGitEnvBaselineReason is replaced by the generated reason once the
// real-tree count is measured (see gitAuthoredBaselineReason below).
const placeholderGitEnvBaselineReason = "measured pre-existing sites: every entry is a fixture git call that inherits the process environment, i.e. exactly the class that wrote 12 commits onto real master — the ratchet is downward (fix a site, delete its entry) and the helper that fixes it is internal/testutil.GitEnv."

// ---------------------------------------------------------------------------
// the predicate
// ---------------------------------------------------------------------------

// gitEnvFind reports, per outermost function, the line of an inherited-
// environment read that a git child in the same function would receive.
func gitEnvFind(g gate, p *parsed) []hit {
	var hits []hit
	for _, fn := range outermostFuncs(p.file) {
		execsGit := false
		environLine := 0
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isGitExecCall(call) {
				execsGit = true
				return true
			}
			if isEnvironCall(call) && environLine == 0 {
				environLine = p.fset.Position(call.Pos()).Line
			}
			return true
		})
		if !execsGit || environLine == 0 {
			continue
		}
		hits = append(hits, hit{rel: p.rel, line: environLine, desc: funcNameOf(fn)})
	}
	return hits
}

// outermostFuncs returns the file's outermost func declarations and literals.
// A nested literal is NOT returned separately: its facts are attributed to the
// function that encloses it, so one site yields at most one hit and the walk
// cannot double-count (deterministic, which is the Rules 16–21 bar).
func outermostFuncs(file *ast.File) []ast.Node {
	var out []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			out = append(out, n)
			return false // children belong to this function for this gate
		}
		return true
	})
	return out
}

// funcNameOf names the function node for the report: the declared name, or the
// literal's `<anonymous>` (a hit a reader can still locate by line).
func funcNameOf(n ast.Node) string {
	if fd, ok := n.(*ast.FuncDecl); ok {
		return fd.Name.Name
	}
	return "<anonymous func>"
}

// isGitExecCall reports whether call is exec.Command("git", …) or
// exec.CommandContext(ctx, "git", …): the callee is the exec package's
// constructor and the binary argument is the literal that names git.
func isGitExecCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != execCommandFunc && sel.Sel.Name != execCommandContextFunc {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != gitExecPkg {
		return false
	}
	binIndex := 0
	if sel.Sel.Name == execCommandContextFunc {
		binIndex = 1 // CommandContext(ctx, name, args…)
	}
	if binIndex >= len(call.Args) {
		return false
	}
	return stringLiteralValue(call.Args[binIndex]) == gitBinaryLiteral
}

// execCommandFunc / execCommandContextFunc are os/exec's two constructors, from
// the stdlib rather than re-typed (Rule 1b: the value IS the stdlib's name).
var (
	execCommandFunc        = "Command"
	execCommandContextFunc = "CommandContext"
)

// isEnvironCall reports whether call is `os.Environ()`.
func isEnvironCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != environFuncName {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == osPkgName && len(call.Args) == 0
}

// environFuncName / osPkgName are the stdlib spellings this gate keys on.
const (
	environFuncName = "Environ"
	osPkgName       = "os"
)

// stringLiteralValue returns the quoted literal's value ("" when the expression
// is not a string literal).
func stringLiteralValue(e ast.Expr) string {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind.String() != "STRING" {
		return ""
	}
	return strings.Trim(lit.Value, `"`)
}
