package main

// retired_verb_helper_reach_test.go — the WIDENED half of the retired-verb
// test-driver gate (task 01a10ef3…). It closes the coverage limit the sibling
// file states in its own report: a *_test.go that reaches the REAL binary
// through a LOCAL HELPER.
//
// WHY A SECOND FILE. `retired_verb_test_driver_gate_test.go` recognises the two
// stdlib program starters (`exec.Command`, `exec.CommandContext`) at the CALL
// SITE. A test that types its argv into a package-local runner —
//
//	fx.sb.g4RunSplit(t, CliVerbCheckpoint, "list")   // the word never appears
//	                                                 // beside an exec call
//
// is invisible to that scan: the word reaches argv[0] two frames away. That was
// MEASURED (the predecessor run found those sites by RUNNING the suite, never by
// reading the gate), and it is the gap this file closes.
//
// HOW IT SEES THROUGH A HELPER — one reachability step, not a dataflow engine.
// The question a helper call site raises is: "does the word I am handing this
// helper land in argv[0] of a program start?" That is decidable, cheaply, for
// one shape and NOT for its generalisation:
//
//	VERBATIM   the helper's body hands the caller's argv STRAIGHT to the exec
//	           call (exec.Command(bin, args…) with `args` a parameter). The
//	           caller's first word IS argv[0]. → the call site is gated.
//	TRANSFORMED the body hands it to something else first
//	           (exec.Command(bin, familyAddressArgs(args)…)). The caller's word
//	           may have been REWRITTEN, so which word reaches argv[0] is not
//	           decidable from the call site. → NOT gated, deliberately.
//
// THE TRANSFORMED EXCLUSION IS NOT A LOOPHOLE — IT IS THE RETIREMENT'S OWN
// REPAIR. `familyAddressArgs` (cli_family_address_test.go) is the ONE door
// mapper that turns a retired flat spelling into the family address that
// dispatches it. A gate that flagged its call sites would refuse the very
// mechanism the retirement ships to fix a stale driver, i.e. it would be a
// false-positive machine — and a gate whose false positives are noise gets
// pinned away, not obeyed. So the transform is recognised BY PREDICATE (the
// argv is handed to a call, not to exec), never by naming `familyAddressArgs`
// (that name-blindness is the rot recorded in this repo's own memory: a
// hardcoded recognition set rots the day a helper is added).
//
// WHAT IT STILL CANNOT SEE, stated so no reader implies full coverage: a word
// that reaches argv[0] through TWO OR MORE helpers, through a slice built in a
// loop, through a closure assigned to a variable, or through a helper whose
// argv position depends on a runtime branch. Those are reported as limits in
// the task report, not silently covered. The predicate is syntactic and always
// has a boundary (Rule 26's corollary: a fix that overstates its coverage is
// worse than one that never landed).
//
// THE VOCABULARY IS STILL READ, NEVER COPIED (Rules 13/14): both halves consume
// the same `map[string]string` the sibling file projects out of
// verbAliasWords() + undispatchedRefusedWordSpellings().

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	// retiredVerbGateHelperVerbatimMin is the widened half's READ-FLOOR (Rule
	// 15): a helper table that came back empty would make every helper call
	// site invisible while the gate still printed a clean line — a scan that
	// never ran wearing a pass. Measured 2026-10-06 on this tree's tip: several
	// verbatim argv forwarders (wdRun, mdRun, …), so a floor of 3 is a floor
	// under the measurement rather than a number fitted to it.
	retiredVerbGateHelperVerbatimMin = 3

	// retiredVerbGateKnownSecondForwarder is the SECOND verbatim forwarder this
	// test pins by NAME, beside `wdRun`. It used to be `mdRun`, a helper that
	// lived in `cmd/agent/merger_test_rig_test.go`; 5a9af79 ("test(merge):
	// 退役 merge 路线的 95 个测试文件") deleted that file with the whole retired
	// merge suite, so the pinned name pointed at nothing and the test failed
	// with "mdRun is not recognised as a helper reaching a program start".
	//
	// THE NAME IS A CONSTANT (Rule 1/13) because the failure mode is a name
	// ROTTING when the file that declared it is deleted: a future retirement
	// sweep that removes the helper this constant names will fail here and be
	// TOLD which declaration to re-point at, instead of silently shrinking the
	// table. `replayRunBinary` is the pinned pair's live member: it is a Go
	// test helper in `session_replay_abolition_test.go` whose body is
	// `exec.Command(bin, args...)` — the verbatim argv forwarding shape the
	// widened half exists to recognise, and the one whose call sites really do
	// hand a refused word to argv[0].
	retiredVerbGateKnownSecondForwarder = "replayRunBinary"

	// retiredVerbGateHelperMinGoFiles floors the widened WALK as well: the
	// helper table is built from every Go source, so a walk that found almost
	// nothing must fail rather than report an empty table as clean.
	retiredVerbGateHelperMinGoFiles = 1500
)

// retiredVerbGateHelperKind is how a local helper's parameters reach a program
// start. The distinction IS the predicate: only Verbvbatim call sites are
// gated.
type retiredVerbGateHelperKind int

const (
	// retiredVerbGateHelperVerbatim: the caller's argv is forwarded unchanged.
	retiredVerbGateHelperVerbatim retiredVerbGateHelperKind = iota + 1
	// retiredVerbGateHelperTransformed: the caller's argv is handed to another
	// call first, so the word that reaches argv[0] is not decidable here.
	retiredVerbGateHelperTransformed
)

// retiredVerbGateHelper is one in-package function or method recognised as
// reaching a program start, with the position the caller's argv[0] lands in.
type retiredVerbGateHelper struct {
	name      string
	kind      retiredVerbGateHelperKind
	file      string
	line      int
	argvIndex int // the call argument index that becomes argv[0]

	// recv is the RECEIVER PARAMETER's identifier for a method declaration
	// (`func (p *g5Probe) run(…)` → "p"), and empty for a package-level
	// function. It is what makes a table entry addressable: two declarations
	// that share a NAME are two different helpers, and the call site's own
	// receiver expression says which one it means. Measured root cause of the
	// defect this field closes: the table was keyed on the bare name, so
	// `internal/devworkspace`'s package-level `run` overwrote this package's
	// `(p *g5Probe) run` and every `p.run(…)` call site was then read as
	// driving argv[0] at ITS argv index — a false positive that accused three
	// correct call sites.
	recv string
}

// retiredVerbGateHelperReachScan builds the helper table from every Go source
// under root, then returns the flagged CALL SITES in the *_test.go files —
// plus the number of VERBATIM helpers found, which is the read-floor's input.
//
// It is deliberately a separate walk from the sibling file's (which reads only
// *_test.go): the helper's BODY is what decides whether its call sites drive the
// binary verbatim, and a helper may live in a non-test file.
func retiredVerbGateHelperReachScan(t *testing.T, root string, words map[string]string) ([]retiredVerbDriveGateFinding, int) {
	t.Helper()
	paths := []string{}
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == retiredVerbDriveGateSkipDir || d.Name() == retiredVerbDriveGateVendorDir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, retiredVerbDriveGateGoSuffix) {
			paths = append(paths, path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s for helpers: %v", root, walkErr)
	}
	sort.Strings(paths)

	// KEYED BY NAME, VALUED BY BUCKET: one name can be several declarations
	// (`run` is a package-level function in internal/devworkspace AND a method on
	// this package's probes), and collapsing them into one slot is how a name
	// table starts lying about which helper a call site means.
	helpers := map[string][]retiredVerbGateHelper{}
	type parsed struct {
		rel     string
		file    *ast.File
		fset    *token.FileSet
		src     string
		escaped map[int]bool
		slices  map[string][]ast.Expr
		consts  map[string]string
	}
	files := make([]parsed, 0, len(paths))
	// stringConsts is the PACKAGE-WIDE table of `NAME = "value"` constants, built
	// as the walk goes; see the second pass below for why the helper table waits
	// for it.
	stringConsts := map[string]string{}
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			// A file that does not parse is an ERROR, never a skip: a gate that
			// silently ignores what it cannot read has a hole shaped like
			// whatever broke the parser.
			t.Fatalf("parse %s: %v", path, err)
		}
		rel := path
		if r, err := filepath.Rel(root, path); err == nil {
			rel = r
		}
		text := string(src)
		consts := retiredVerbGateStringLiterals(f)
		for name, value := range consts {
			stringConsts[name] = value
		}
		files = append(files, parsed{
			rel: rel, file: f, fset: fset, src: text,
			escaped: retiredVerbGateHelperEscapeLines(text),
			slices:  retiredVerbDriveGateSliceLiterals(f),
			consts:  consts,
		})
	}
	// The HELPER TABLE is built in a SECOND pass: a helper's program argument may
	// name a package constant declared in ANOTHER file of this package
	// (`exec.Command(systemdRunPath, …)`), so the constant table has to be
	// complete before any body is classified.
	for _, p := range files {
		for _, decl := range p.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			h, ok := retiredVerbGateClassifyHelper(fn, p.fset, p.rel, stringConsts)
			if !ok {
				continue
			}
			helpers[h.name] = append(helpers[h.name], h)
		}
	}

	// The floor counts DECLARATIONS, and a name shared by two declarations is
	// two helpers — the read-floor's job is "did the walk see forwarders at all",
	// which a per-name count would understate.
	verbatim := 0
	for _, bucket := range helpers {
		for _, h := range bucket {
			if h.kind == retiredVerbGateHelperVerbatim {
				verbatim++
			}
		}
	}

	out := []retiredVerbDriveGateFinding{}
	for _, p := range files {
		if !strings.HasSuffix(p.rel, retiredVerbDriveGateTestSuffix) {
			continue // the scope of the contract is the test face
		}
		out = append(out, retiredVerbGateHelperCallFindings(p.rel, p.file, p.fset, p.src, p.escaped, p.slices, helpers, words)...)
	}
	return out, verbatim
}

// retiredVerbGateHelperEscapeLines indexes the sibling gate's line-scoped escape
// marker, read through the SAME constant that file declares (Rule 13: the
// marker has one definition). A reviewer marks the call's line or the line
// above it; the helper arm honours the same escape, so a deliberate probe is
// suppressible in one place.
func retiredVerbGateHelperEscapeLines(src string) map[int]bool {
	escaped := map[int]bool{}
	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, retiredVerbDriveGateEscape) {
			continue
		}
		escaped[i+1] = true
		escaped[i+2] = true
	}
	return escaped
}

// retiredVerbGateClassifyHelper decides whether ONE function or method reaches a
// program start, and HOW its caller's argv gets there. It returns ok=false for
// everything it cannot decide — the conservative direction, because a helper
// misclassified as VERBATIM is a false positive and a gate that cries wolf is
// worth less than no gate.
//
// Three conditions must hold together:
//
//  1. the program being started is NOT NAMED IN THE SOURCE — neither as a string
//     literal nor as a constant whose value the source carries (`cwdProbeGitBinary`,
//     `systemdRunPath`, `systemctlPath`, …). This is what keeps a `git`/`sleep`/`bash`
//     helper out of the table: a call site handing it `log` means git's log, and
//     gating that would be a false positive (measured: the gate flagged
//     cwdProbeGitOutput, whose body is `exec.Command(cwdProbeGitBinary, args…)`
//     with cwdProbeGitBinary = "git"). Every gopi driver in this repo passes the
//     built binary as a VARIABLE it never gives a spelling (`bin`, `s.bin`,
//     `testutil.GopiBin(t)`, a function parameter).
//  2. one of the helper's PARAMETERS is what the caller's argv is forwarded as
//     — a helper that builds its own argv from a local constant exposes no call
//     site to gate.
//  3. that parameter reaches the exec call either DIRECTLY (verbatim, gated) or
//     through another call (transformed, not gated).
func retiredVerbGateClassifyHelper(fn *ast.FuncDecl, fset *token.FileSet, rel string, consts map[string]string) (retiredVerbGateHelper, bool) {
	if fn.Body == nil || fn.Name == nil || fn.Type.Params == nil {
		return retiredVerbGateHelper{}, false
	}
	// The FLAT index of each parameter: `func wdRun(t *testing.T, bin string,
	// env []string, args ...string)` numbers t=0, bin=1, env=2, args=3, which
	// is exactly the position the caller's argument lands in — including for a
	// variadic, which occupies ONE position.
	flat := map[string]int{}
	next := 0
	for _, f := range fn.Type.Params.List {
		for _, n := range f.Names {
			flat[n.Name] = next
			next++
		}
	}
	if next == 0 {
		return retiredVerbGateHelper{}, false // no parameters: nothing to forward
	}

	found := false
	var kind retiredVerbGateHelperKind
	argvIndex := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		execName := retiredVerbDriveGateExecCallName(call)
		if execName == "" {
			return true
		}
		idx := retiredVerbDriveGateExecProgramIndex[execName]
		if len(call.Args) <= idx {
			return true
		}
		if retiredVerbGateProgramIsSpelled(call.Args[idx], consts, programNames) {
			// condition 1: the source NAMES the program — a `git`, a `sqlite3`,
			// a `systemd-run` — so this is not a driver of THIS binary.
			return true
		}
		rest := call.Args[idx+1:]
		if len(rest) == 0 {
			return true
		}
		k, param, ok := retiredVerbGateArgvForwarding(rest[0], flat)
		if !ok {
			return true
		}
		// A VERBATIM sighting outranks a transformed one: if any program start
		// in the body forwards a parameter unchanged, a caller's refused word
		// really does land in argv[0].
		if found && kind == retiredVerbGateHelperVerbatim {
			return true
		}
		found, kind, argvIndex = true, k, flat[param]
		return true
	})
	if !found {
		return retiredVerbGateHelper{}, false
	}
	pos := fset.Position(fn.Pos())
	recv := ""
	if fn.Recv != nil && len(fn.Recv.List) > 0 && len(fn.Recv.List[0].Names) > 0 {
		recv = fn.Recv.List[0].Names[0].Name
	}
	return retiredVerbGateHelper{name: fn.Name.Name, kind: kind, file: rel, line: pos.Line, argvIndex: argvIndex, recv: recv}, true
}

// retiredVerbGateProgramIsSpelled reports whether the exec call's PROGRAM
// argument — the thing that becomes argv[0] — is NAMED in the source: a string
// literal, a package-level constant whose value the tree carries, or a selector
// onto one (`toolnames.ToolCodeSearch`). Any of those says the program is a
// TOOL, not this binary, and the gate must not read the call's first ARGUMENT as
// a gopi verb (Rule 15: a gate that cries wolf on `git log` is worth less than
// no gate — measured on cwdProbeGitOutput, task 01a139ec).
//
// A NAME that resolves to nothing (`bin`, `s.bin`, `testutil.GopiBin(t)`, a
// function's own parameter `exe`) is NOT this case, so the helper stays in the
// table — which is the direction that keeps the gate useful, since every driver
// of the real binary in this repo passes a variable exactly like that.
//
// It is deliberately here rather than in the sibling file: the sibling's
// resolver knows this PACKAGE's gopi-verb constants (CliVerbLog, …), while this
// one knows every string constant in the tree — a wider table would be a wider
// hole on the other side, where a retired word spelled through a variable name
// must still be caught.
func retiredVerbGateProgramIsSpelled(e ast.Expr, consts map[string]string, programNames map[string]bool) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.Ident:
		if _, named := consts[v.Name]; named {
			return true
		}
		return programNames[v.Name]
	case *ast.SelectorExpr:
		if pkg, ok := v.X.(*ast.Ident); ok {
			if _, named := consts[pkg.Name+"."+v.Sel.Name]; named {
				return true
			}
			return programNames[pkg.Name+"."+v.Sel.Name]
		}
	}
	return false
}

// programNames is the DECLARED answer to "is this variable a program name?" —
// every value the tree itself declares as one, keyed by the identifier that
// carries it. It exists because a source cannot always be read for it (a
// cross-package export such as testutil.BinDirName is a file read away), and a
// guess is exactly what this predicate must not make.
//
// Rule 1: the NAMES are declared here rather than spelled at the predicate.
//
// Rule 35 (zero new dependencies): this table adds no artifact, no file and no
// runtime requirement — it is a list of identifiers inside a test.
var programNames = func() map[string]bool {
	out := map[string]bool{}
	for _, n := range []string{
		// The binary's OWN NAME — the word in a path to gopi itself, as this
		// package already declares it (`testutil.BinFileName` is the SSOT and the
		// gate reads the same key). A helper whose program argument carries this
		// name is the OPPOSITE of a false positive: it would be a real driver the
		// helper arm should keep scanning, so it is listed for the record rather
		// than hidden, and the direct arm still sees its call sites.
		"testutil.BinFileName",
		"testutil.BinDirName",
	} {
		out[n] = true
	}
	return out
}()

// retiredVerbGateStringLiterals indexes every package-level string constant a
// file declares, keyed BOTH by its bare identifier and by `pkg.Name`, so a
// selector onto an imported constant is readable too. It is the tree's own
// answer to "what does this name hold", which is what condition 1 needs: a
// program that is NAMED is a tool, not this binary.
//
// Only `NAME = "literal"` rows are indexed, in both the `const` and the `var`
// form (this repo declares `cwdProbeGitBinary` in one and `systemctlPath` in the
// other). A constant built at runtime from a path join is NOT resolved here, and
// that limit is honest rather than silent: such a helper still enters the table
// and its call sites are still gated, which is the conservative direction for a
// gate whose false positives are the thing that gets it switched off.
func retiredVerbGateStringLiterals(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				out[name.Name] = value
			}
		}
	}
	return out
}

// retiredVerbGateArgvForwarding answers the one question the table needs about
// an exec call's argv argument: is this a PARAMETER of the enclosing function,
// and is it handed over unchanged?
//
// The three spellings of the same argv are the reason this is AST work:
//
//	exec.Command(bin, args…)                    → Ident, a parameter → VERBATIM
//	exec.Command(bin, familyAddressArgs(args)…)  → CallExpr carrying a param → TRANSFORMED
//	exec.Command(bin, []string{first, args…}…)   → spliced with something else → TRANSFORMED
//
// The default is ok=false: an expression that carries no parameter of this
// function says nothing about what a CALLER passed, and inventing a verdict for
// it is how a predicate acquires false positives.
func retiredVerbGateArgvForwarding(e ast.Expr, flat map[string]int) (retiredVerbGateHelperKind, string, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		if _, isParam := flat[v.Name]; isParam {
			return retiredVerbGateHelperVerbatim, v.Name, true
		}
	case *ast.CallExpr:
		for _, a := range v.Args {
			if id, isIdent := a.(*ast.Ident); isIdent {
				if _, isParam := flat[id.Name]; isParam {
					return retiredVerbGateHelperTransformed, id.Name, true
				}
			}
		}
	case *ast.CompositeLit:
		for _, el := range v.Elts {
			if id, isIdent := el.(*ast.Ident); isIdent {
				if _, isParam := flat[id.Name]; isParam {
					return retiredVerbGateHelperTransformed, id.Name, true
				}
			}
		}
	}
	return 0, "", false
}

// retiredVerbGateHelperCallFindings returns the flagged call sites in ONE test
// source: a call to a VERBATIM helper whose argument at the helper's argv[0]
// position resolves to a refused word.
//
// The resolution reuses the sibling file's `retiredVerbDriveGateLiteralWord`,
// so the identifier form (`CliVerbCheckpoint`) and the literal form
// ("checkpoint") are caught by the same rule in both halves of the gate — a
// second resolver here would be a second answer to one question (Rule 13).
func retiredVerbGateHelperCallFindings(
	rel string,
	file *ast.File,
	fset *token.FileSet,
	src string,
	escaped map[int]bool,
	slices map[string][]ast.Expr,
	helpers map[string][]retiredVerbGateHelper,
	words map[string]string,
) []retiredVerbDriveGateFinding {
	out := []retiredVerbDriveGateFinding{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		h, isHelperCall := retiredVerbGateHelperForCall(call, helpers)
		if !isHelperCall || h.kind != retiredVerbGateHelperVerbatim {
			return true
		}
		pos := fset.Position(call.Pos())
		if escaped[pos.Line] {
			return true
		}
		if len(call.Args) <= h.argvIndex {
			return true
		}
		word, ok := retiredVerbGateArgvZeroWord(call.Args[h.argvIndex], slices)
		if !ok {
			return true
		}
		address, refused := words[word]
		if !refused {
			return true
		}
		out = append(out, retiredVerbDriveGateFinding{
			file:    rel,
			line:    pos.Line,
			word:    word,
			address: address,
			via:     h.name,
			snippet: retiredVerbDriveGateSnippet(src, pos.Line),
		})
		return true
	})
	return out
}

// retiredVerbGateHelperForCall resolves the callee to the helper the CALL SITE
// means — not merely the first declaration that happens to share its name.
//
// The name alone is not an identity. `run` is declared as a package-level
// function taking the program as a value in internal/devworkspace, and as a
// METHOD on this package's probes whose body hands the argv through the door
// mapper; the `cli_*_test.go` files each declare their own `run(t, repo, args…)`
// that splits the argv first. Keyed on the bare name, the last declaration
// parsed wins, and every call site is then read as if it were calling THAT one:
// a false positive that accused three correct `p.run(t, "", CliVerbTool,
// CliVerbProxy, …)` call sites of driving `gopi proxy` verbatim.
//
// So the resolution is a three-step SEARCH, most specific first, and it returns
// the first bucket that can address the call shape:
//
//  1. a bare identifier call `run(…)` addresses a PACKAGE-LEVEL function of that
//     name — a method is not callable that way;
//  2. a selector call `x.run(…)` names its own RECEIVER IDENTIFIER, so a
//     declaration whose receiver parameter is that identifier is the helper
//     meant; this is the arm that makes `p.run(…)` resolve to the method on
//     `*g5Probe` rather than to some other package's function;
//  3. a CHAINED selector (`fx.sb.run(…)`) carries no identifier to match, so any
//     method of that name answers — the pre-fix behaviour, kept as the last
//     resort because a name match is still a hit, just never preferred over an
//     exact receiver match.
//
// When a bucket holds several candidates, the FIRST IN PARSE ORDER answers: the
// walk sorts paths, so this is deterministic (a gate that answers differently on
// consecutive runs would be unusable).
func retiredVerbGateHelperForCall(call *ast.CallExpr, helpers map[string][]retiredVerbGateHelper) (retiredVerbGateHelper, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		for _, h := range helpers[fun.Name] {
			if h.recv == "" {
				return h, true
			}
		}
	case *ast.SelectorExpr:
		bucket := helpers[fun.Sel.Name]
		for _, h := range bucket {
			if h.recv == "" {
				continue
			}
			if id, ok := fun.X.(*ast.Ident); ok && id.Name == h.recv {
				return h, true
			}
		}
		for _, h := range bucket {
			if h.recv != "" {
				return h, true
			}
		}
	}
	return retiredVerbGateHelper{}, false
}

// retiredVerbGateArgvZeroWord resolves the caller's argument at the argv[0]
// position to the word it carries — the same three shapes the sibling file's
// direct predicate reads (a literal, a package constant, a slice bound to a name
// earlier in the file), because a call site spelling its argv as
// `wdRun(t, bin, env, args…)` is the form the measured sites use.
func retiredVerbGateArgvZeroWord(e ast.Expr, slices map[string][]ast.Expr) (string, bool) {
	if lit, ok := e.(*ast.CompositeLit); ok {
		if len(lit.Elts) == 0 {
			return "", false
		}
		return retiredVerbDriveGateLiteralWord(lit.Elts[0])
	}
	if id, ok := e.(*ast.Ident); ok {
		if elts, ok := slices[id.Name]; ok {
			if len(elts) == 0 {
				return "", false
			}
			return retiredVerbDriveGateLiteralWord(elts[0])
		}
	}
	return retiredVerbDriveGateLiteralWord(e)
}

// --- the widened half's own read-floor and proofs -----------------------------

// TestRetiredVerbGateHelperTableIsNotEmpty is the widened half's READ-FLOOR
// (Rule 15). Without it, a helper detector that silently recognised nothing
// would leave every helper call site unscanned while the gate printed a clean
// line — "the scan found no helpers" reading as "there are no helpers", which is
// exactly the silent-pass failure shape.
//
// It asserts the FLOOR and two named facts: the two verbatim forwarders this
// widening was built for are IN the table, and the mapper helper is in it as
// TRANSFORMED — so the exclusion is a measured decision about a real helper
// rather than an argument about one.
func TestRetiredVerbGateHelperTableIsNotEmpty(t *testing.T) {
	root := retiredVerbDriveGateGoModRoot(t)
	_, verbatim := retiredVerbGateHelperReachScan(t, root, map[string]string{})

	paths := 0
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == retiredVerbDriveGateSkipDir || d.Name() == retiredVerbDriveGateVendorDir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, retiredVerbDriveGateGoSuffix) {
			paths++
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", root, walkErr)
	}
	if paths < retiredVerbGateHelperMinGoFiles {
		t.Fatalf("the widened walk found only %d %s file(s) (floor %d) — the helper table would be "+
			"built from a fragment, and every call site it missed would read as clean",
			paths, retiredVerbDriveGateGoSuffix, retiredVerbGateHelperMinGoFiles)
	}
	if verbatim < retiredVerbGateHelperVerbatimMin {
		t.Fatalf("the helper table holds %d VERBATIM forwarder(s) (floor %d) — a helper-reachability "+
			"scan that recognises no helper is blind, and a blind scan must not pass",
			verbatim, retiredVerbGateHelperVerbatimMin)
	}
	t.Logf("widened half: %d verbatim argv forwarder(s) recognised across %d %s file(s)",
		verbatim, paths, retiredVerbDriveGateGoSuffix)
}

// TestRetiredVerbGateNamesTheKnownHelpers pins the two facts the read-floor
// above counts, so a refactor that renames a forwarder cannot quietly shrink the
// table to a number that still clears the floor. It asserts the DECLARATIONS
// this repo really ships.
func TestRetiredVerbGateNamesTheKnownHelpers(t *testing.T) {
	root := retiredVerbDriveGateGoModRoot(t)
	helpers := map[string][]retiredVerbGateHelper{}
	for _, probe := range []string{"wdRun", retiredVerbGateKnownSecondForwarder} {
		h, ok := retiredVerbGateClassifyHelperInTree(t, root, probe)
		if !ok {
			t.Errorf("%s is not recognised as a helper reaching a program start — the widened predicate "+
				"lost the forwarder its own read-floor counts", probe)
			continue
		}
		if h.kind != retiredVerbGateHelperVerbatim {
			t.Errorf("%s classified as %v, want VERBATIM: its body hands the caller's argv straight to "+
				"exec.Command, so a refused word at one of its call sites really does reach argv[0]",
				probe, h.kind)
		}
		helpers[probe] = append(helpers[probe], h)
	}
	if len(helpers) != 2 {
		t.Fatalf("only %d of the 2 named forwarders were found — the table is not reading this tree",
			len(helpers))
	}
}

// retiredVerbGateClassifyHelperInTree finds ONE named declaration in the tree and
// classifies it — the instrument behind the test above, reading the real file
// rather than a fixture.
func retiredVerbGateClassifyHelperInTree(t *testing.T, root, name string) (retiredVerbGateHelper, bool) {
	t.Helper()
	found := false
	result := retiredVerbGateHelper{}
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() {
			if d.Name() == retiredVerbDriveGateSkipDir || d.Name() == retiredVerbDriveGateVendorDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, retiredVerbDriveGateGoSuffix) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return nil
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil || fn.Name.Name != name {
				continue
			}
			if h, ok := retiredVerbGateClassifyHelper(fn, fset, path, retiredVerbGateStringLiterals(f)); ok {
				result, found = h, true
				return nil
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s for %s: %v", root, name, walkErr)
	}
	return result, found
}

// TestRetiredVerbGateHelperArmIsRedOnTheRealTree is the RED PROOF for the
// widened arm, measured on the LIVE tree rather than on a fixture — and it is
// stated as the DIFFERENCE between the two arms, because that difference IS the
// blind spot this task exists to close.
//
// The shape: one word that today's tests hand to a VERBATIM helper as argv[0] is
// retired inside this sub-run alone, and BOTH scans run over the same real
// corpus. The claim is two-sided —
//
//   - the widened arm MUST name the call sites (the predicate reads the tree), and
//   - the DIRECT arm, which is the whole of the original gate, MUST NOT see them
//     (the word never appears beside an exec call).
//
// A proof of only the first half would leave "did the widening add anything?"
// unanswered; the second half is what makes this a measurement of coverage
// rather than of activity. `chat` is the word because this repo's tests drive it
// through `testutil.RunGopi` by the literal (`RunGopi(t, []string{"chat", …})`),
// which is exactly the frame the narrow scan cannot see through.
//
// IT ASSERTS THE WORD, not merely a non-empty result: an earlier draft of this
// test passed because it counted findings produced by OTHER real violations that
// happened to be present, so it was green without measuring the retired word at
// all. Naming the word in the assertion is what makes that class of accidental
// pass impossible.
func TestRetiredVerbGateHelperArmIsRedOnTheRealTree(t *testing.T) {
	root := retiredVerbDriveGateGoModRoot(t)
	words, _ := retiredVerbDriveGateVocabulary(t)

	// A word today's tests hand to a VERBATIM helper — retired only inside this
	// sub-run, never in the product vocabulary.
	const drivenOnlyThroughAHelper = "chat"
	words[drivenOnlyThroughAHelper] = "chat [flags]"

	helperFindings, verbatim := retiredVerbGateHelperReachScan(t, root, words)
	if verbatim < retiredVerbGateHelperVerbatimMin {
		t.Fatalf("the helper table holds %d verbatim forwarder(s) (floor %d) — the red proof read no "+
			"helper, so it would prove nothing", verbatim, retiredVerbGateHelperVerbatimMin)
	}
	through := []retiredVerbDriveGateFinding{}
	for _, f := range helperFindings {
		if f.via != "" && f.word == drivenOnlyThroughAHelper {
			through = append(through, f)
		}
	}
	if len(through) == 0 {
		t.Fatalf("retiring %q turned NO helper-reached call site red — the widened arm is not reading "+
			"the real tree, so the green gate proves nothing about the blind spot it closes",
			drivenOnlyThroughAHelper)
	}

	// The OTHER side of the same measurement: the direct arm — the original gate
	// in full — over the same corpus and the same vocabulary.
	directFindings, _ := retiredVerbDriveGateScan(t, root, words)
	direct := 0
	for _, f := range directFindings {
		if f.word == drivenOnlyThroughAHelper {
			direct++
		}
	}
	if direct != 0 {
		t.Fatalf("the DIRECT arm already sees %d site(s) driving %q as argv[0] — the word is not "+
			"helper-only in this tree any more, so this proof no longer demonstrates the widening; "+
			"pick a word reached only through a helper (first: %s)",
			direct, drivenOnlyThroughAHelper, directFindings[0])
	}
	t.Logf("red proof (widened arm): retiring %q alone is INVISIBLE to the direct arm (0 findings) and "+
		"turns %d helper-reached call site(s) red across %d verbatim forwarder(s) (e.g. %s)",
		drivenOnlyThroughAHelper, len(through), verbatim, through[0])
}

// TestRetiredVerbGateHelperArmFixtureProofs proves the widened predicate's four
// boundaries on the smallest samples (Rule 10's minimal-sample habit): a verbatim
// helper IS flagged, a TRANSFORMED helper is NOT (the mapper exclusion), an
// unreached helper is NOT, and the escape marker suppresses one line. Each case
// is one synthetic source pair, so the assertion is falsifiable without waiting
// for the next retiree.
func TestRetiredVerbGateHelperArmFixtureProofs(t *testing.T) {
	const declinedWord = "retired-probe-word"
	words := map[string]string{declinedWord: "gopi session probe"}

	cases := []struct {
		name       string
		helper     string // the non-test source declaring the helper
		call       string // the test source calling it
		wantLine   int    // 0 = no finding expected
		wantVia    string
		wantMarker bool // the escape marker is present in the call source
	}{
		{
			name: "VERBATIM helper: the caller's word lands in argv[0] and is flagged",
			helper: `package main

import "os/exec"

func runIt(bin string, args ...string) (int, string) {
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	_ = err
	return 0, string(out)
}
`,
			call: `package main

import "testing"

func TestDrives(t *testing.T) {
	runIt(bin, "` + declinedWord + `", "list")
}
`,
			wantLine: 6,
			wantVia:  "runIt",
		},
		{
			name: "TRANSFORMED helper: the argv is rewritten first, so the call site is NOT flagged",
			helper: `package main

import "os/exec"

func transform(args []string) []string { return args }

func runIt(bin string, args ...string) (int, string) {
	cmd := exec.Command(bin, transform(args)...)
	out, err := cmd.CombinedOutput()
	_ = err
	return 0, string(out)
}
`,
			call: `package main

import "testing"

func TestDrives(t *testing.T) {
	runIt(bin, "` + declinedWord + `", "list")
}
`,
			wantLine: 0,
		},
		{
			name: "a literal-named program (a NON-gopi helper) is not a driver",
			helper: `package main

import "os/exec"

func runGit(args ...string) (int, string) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	_ = err
	return 0, string(out)
}
`,
			call: `package main

import "testing"

func TestDrives(t *testing.T) {
	runGit("` + declinedWord + `", "list")
}
`,
			wantLine: 0,
		},
		{
			name: "the escape marker suppresses the helper call site",
			helper: `package main

import "os/exec"

func runIt(bin string, args ...string) (int, string) {
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	_ = err
	return 0, string(out)
}
`,
			call: `package main

import "testing"

func TestDrives(t *testing.T) {
	// retired-drive-ok: this test MUST feed the refusal its own word
	runIt(bin, "` + declinedWord + `", "list")
	runIt(bin, "` + declinedWord + `", "stats")
}
`,
			wantLine:   8,
			wantVia:    "runIt",
			wantMarker: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "helpers.go"), []byte(tc.helper), 0o600); err != nil {
				t.Fatalf("write helper fixture: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "use_test.go"), []byte(tc.call), 0o600); err != nil {
				t.Fatalf("write call fixture: %v", err)
			}
			findings, _ := retiredVerbGateHelperReachScan(t, dir, words)
			if tc.wantLine == 0 {
				if len(findings) != 0 {
					t.Fatalf("the widened arm flagged %d call site(s) it must stay quiet about — this is "+
						"the false-positive shape that would refuse the retirement's own repair:\n%v",
						len(findings), findings)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("the widened arm reported %d finding(s), want exactly 1 (red on the verbatim "+
					"helper, quiet about the rest):\n%v", len(findings), findings)
			}
			got := findings[0]
			if got.line != tc.wantLine || got.via != tc.wantVia {
				t.Fatalf("finding = %s (via %q line %d), want via %q on line %d",
					got, got.via, got.line, tc.wantVia, tc.wantLine)
			}
			if got.address == "" {
				t.Errorf("the finding carries no address — the refusal text must send the reader to a live door (Rule 15)")
			}
			if !strings.Contains(got.String(), tc.wantVia) {
				t.Errorf("the message does not name the helper %q, so the reader cannot tell where to fix it: %s",
					tc.wantVia, got)
			}
			_ = tc.wantMarker
		})
	}
}

// TestRetiredVerbGateHelperResolutionIsNotNameOnly is the REGRESSION FIXTURE for
// the false positive that made this gate red on master: the table was keyed on
// the bare NAME, so a declaration in another package (or on another receiver)
// overwrote the helper the call site actually meant, and three correct
// `p.run(t, "", CliVerbTool, CliVerbProxy, …)` call sites were accused of
// driving the retired word verbatim.
//
// TWO cases, and they pull in OPPOSITE directions — together they say the
// resolution is *specific*, not merely *quieter*:
//
//   - the FALSE-POSITIVE shape: the same-named declaration that wins by parse
//     order is VERBATIM (a package-level `run` from another package), while the
//     receiver the call site names is a TRANSFORMED method. The call site is
//     correct and must NOT be flagged. Before the fix this case reported 1.
//   - the FALSE-NEGATIVE shape: the call site is a package-level call to a
//     VERBATIM helper, and a same-named TRANSFORMED method wins by parse order.
//     The call site really does put the word in argv[0] and MUST be flagged.
//     Before the fix this case reported 0 — the name collision hid a real
//     driver, which is the more dangerous of the two failure directions.
//
// Both fixtures answer to the same predicate (declaration identity), so a
// "fix" that merely suppressed same-named findings would pass one and fail the
// other.
func TestRetiredVerbGateHelperResolutionIsNotNameOnly(t *testing.T) {
	const declinedWord = "retired-probe-word"
	words := map[string]string{declinedWord: "gopi session probe"}

	cases := []struct {
		name         string
		files        map[string]string
		wantFindings int
		wantVia      string
	}{
		{
			name: "same-named VERBATIM function elsewhere does NOT flag a correct call site",
			files: map[string]string{
				// Parsed LAST (path order), and VERBATIM: this is the declaration
				// that used to overwrite every other `run` in the table.
				"z_devworkspace.go": `package main

import (
	"context"
	"os/exec"
)

func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	_ = dir
	return string(out), err
}
`,
				// The declaration the CALL SITE means: a method on *g5Probe whose
				// body hands the argv to the door mapper first → TRANSFORMED.
				"a_probe.go": `package main

import (
	"os/exec"
	"testing"
)

type g5Probe struct{ bin string }

type g5Result struct{}

func transform(args []string) []string { return args }

func (p *g5Probe) run(t *testing.T, stdin string, args ...string) g5Result {
	cmd := exec.Command(p.bin, transform(args)...)
	_ = cmd
	return g5Result{}
}
`,
				"b_use_test.go": `package main

import "testing"

func TestDrives(t *testing.T) {
	var p *g5Probe
	p.run(t, "", "list", "` + declinedWord + `")
}
`,
			},
			wantFindings: 0,
		},
		{
			name: "a same-named TRANSFORMED method elsewhere does NOT hide a verbatim helper",
			files: map[string]string{
				"a_helper.go": `package main

import "os/exec"

func run(bin string, args ...string) (string, error) {
	out, err := exec.Command(bin, args...).CombinedOutput()
	return string(out), err
}
`,
				// Parsed LAST and TRANSFORMED: keyed on the bare name this entry
				// replaced the verbatim helper above and the real driver vanished.
				"z_probe.go": `package main

import "os/exec"

type probe struct{}

func transform(args []string) []string { return args }

func (x *probe) run(bin string, args ...string) string {
	out, err := exec.Command(bin, transform(args)...).CombinedOutput()
	_ = err
	return string(out)
}
`,
				"b_use_test.go": `package main

import "testing"

func TestDrives(t *testing.T) {
	run(bin, "` + declinedWord + `", "list")
}
`,
			},
			wantFindings: 1,
			wantVia:      "run",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
					t.Fatalf("write fixture %s: %v", name, err)
				}
			}
			findings, _ := retiredVerbGateHelperReachScan(t, dir, words)
			if len(findings) != tc.wantFindings {
				t.Fatalf("the widened arm reported %d finding(s), want %d — the resolution is "+
					"by-name again (a declaration in another package or on another receiver decided "+
					"this call site):\n%v", len(findings), tc.wantFindings, findings)
			}
			if tc.wantFindings > 0 && findings[0].via != tc.wantVia {
				t.Fatalf("finding names helper %q, want %q", findings[0].via, tc.wantVia)
			}
		})
	}
}

// TestRetiredVerbGateHelperArmSeesThroughTheMapper is the WIDENED half's
// honest-coverage assertion: the two sites the predecessor run named as the
// blind spot REALLY ARE reached through a helper, and the predicate's verdict on
// them is a DECISION (transformed → legal) rather than an oversight (invisible).
//
// It is stated that way because the alternative reading of this whole task is
// wrong: those sites are not stale drivers waiting to be caught — they pass
// through `familyAddressArgs`, the door mapper the retirement ships. A gate that
// flagged them would be refusing the repair. What the widening must show is that
// it SAW them and explained itself.
func TestRetiredVerbGateHelperArmSeesThroughTheMapper(t *testing.T) {
	root := retiredVerbDriveGateGoModRoot(t)
	h, ok := retiredVerbGateClassifyHelperInTree(t, root, "g4RunSplit")
	if !ok {
		t.Fatal("g4RunSplit is not recognised at all as reaching a program start — the widened predicate " +
			"is blind to the very helper the predecessor run named as the blind spot")
	}
	if h.kind != retiredVerbGateHelperTransformed {
		t.Fatalf("g4RunSplit classified as %v, want TRANSFORMED: its body wraps the caller's argv in the "+
			"door mapper before exec, which is why its call sites are legal and must not be flagged", h.kind)
	}
	t.Logf("g4RunSplit: recognised at %s:%d as a TRANSFORMED forwarder — its call sites reach the binary "+
		"through the retirement's own door mapper, so they are legal by decision, not invisible by oversight",
		h.file, h.line)
}
