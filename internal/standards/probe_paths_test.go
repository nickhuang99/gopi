package standardgate

// probe_paths_test.go — the gate registry publishes ONE name per gate, and this
// file is the machine check that the name it publishes is the arm which can
// actually FAIL on the real tree.
//
// THE DEFECT THIS EXISTS FOR (measured 2026-10-28, task 01a18235): the registry
// published each gate's SHORT probe name. `go test ./internal/standards/ -run
// 'TestGate9GitEnvRedProof'` therefore selected the standalone RED PROOF — a
// predicate test over a planted snippet that never reads the tree — and exited 0
// while three unannotated gate-9 hits sat in the tree. The arm that reads the
// tree is the SUBTEST `TestStandardGates/<probe>`, which a `-run` pattern for the
// short name does not reach at all. So the documented enforcement command was
// structurally incapable of failing: a green no tree can turn red, the failure
// shape PROJECT.md Rule 38 names (a named check that does not exist is worse
// than no check at all).
//
// THE TWO ARMS ARE DELIBERATE OPPOSITES, and this file keeps them that way
// instead of collapsing them:
//
//	standalone *RedProof  = PREDICATE proof. Planted snippet + clean control.
//	                        MUST NOT read the tree — a red proof that scanned the
//	                        tree would report tree state as a predicate failure
//	                        and could not tell "the predicate rotted" from
//	                        "somebody added a violation".
//	TestStandardGates/<g> = REAL-TREE enforcement. scan → classifyAll → enforce.
//	                        MUST read the tree — its failure means "fix the site
//	                        or permit it".
//
// WHAT IS CHECKED, and each arm is red-proofed in
// TestGateProbeArmFindsTheDefectItExistsFor:
//
//	(a) every published path is the subtest path of a test that registers it,
//	    and that registration's body calls `scan` — the published name selects an
//	    arm that CAN see the tree;
//	(b) no standalone red proof reaches the tree: none of the registry's probes
//	    calls scan/scanText/classifyAll/enforce/repoRoot in its own body;
//	(c) the tree arm still runs EVERY gate in gates(): every probe is either
//	    registered by name, or the dispatcher registers the gate variable itself
//	    (the dynamic form `t.Run(g.probe, …)`, which covers the whole slice).
//
// Counts are PRINTED and floored — a scan that examined nothing is not a green.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// probePathRe is probePathRef compiled once (Rule 1: the literal lives above).
var probePathRe = regexp.MustCompile(probePathRef)

const (
	// probeTreeReaderEscape marks a tree-reading reference in a red proof as
	// reviewed, with its reason, above the site.
	probeTreeReaderEscape = "probe-tree-ok:"

	// probeEscapeWindow is how far above a reference the reviewed marker is
	// looked for — the same "comment block above the site" discipline
	// classify() applies to inline escapes.
	probeEscapeWindow = 6

	// probeFilesFloor / probeFuncsFloor refuse a scan that read nothing: a
	// package directory with no _test.go file, or an AST pass that examined no
	// test function, would otherwise pass every arm below by finding nothing.
	probeFilesFloor = 20
	probeFuncsFloor = 40

	// probeTestPrefix / probeGoFileSuffix select the population: package test
	// functions, in the package's own directory.
	probeTestPrefix   = "Test"
	probeGoFileSuffix = ".go"

	// probeDynamicName is the pseudo-name a subtest registration carries when
	// its name argument is the gate variable itself (`t.Run(g.probe, …)`)
	// instead of a literal. It is a pseudo-name, never a real test name: the
	// dispatcher that loops over gates() names every subtest this way, and it
	// must not be mistaken for one gate's arm.
	probeDynamicName = "<gate.probe>"

	// probePathRef finds a published subtest path in prose: the tree arm's name,
	// a slash, and the identifier a reader would paste after `-run`.
	probePathRef = `TestStandardGates/([A-Za-z_][A-Za-z0-9_]*)`

	// probePathEscape marks a published path that is DELIBERATELY unresolvable
	// (a page recording the defect), reviewed with its reason above the line.
	probePathEscape = "probe-path-ok:"

	// probePathFloor refuses a survey that read no invocation at all.
	probePathFloor = 8

	// normativeHeader is the mandatory doc-class banner (PROJECT.md Rule 36) that
	// selects the pages whose commands a reader is meant to follow.
	normativeHeader = "<!-- doc-class: normative -->"
)

// probeTreeReaders are the calls that make an arm read the REAL tree — the
// vocabulary a standalone red proof must never reach. It is the enforcement
// chain in one place (Rule 14): scan is the ONE scan entry point (gate.go),
// scanText its text-topology sibling, classifyAll/enforce the accounting, and
// repoRoot the module-root locator every scan starts from.
//
// The file-walkers (goFilesUnder, parseRepoFile, textFilesUnder) are
// deliberately NOT listed: a FIXTURE proof legitimately builds its own tree under
// t.TempDir() and walks it (TestGate7WorktreeRootLiteralInjectionRedProof,
// TestGate8ScratchEnvHomeInjectionRedProof). What makes any arm read the REAL
// tree is repoRoot, and that IS listed — so the shape cannot smuggle itself in
// through a helper.
var probeTreeReaders = []string{
	"scan",
	"scanText",
	"classifyAll",
	"enforce",
	"repoRoot",
}

// probeFunc is one test function's shape: where it is, and the tree-reading
// calls its BODY makes. Func literals are attributed to their enclosing function
// for gate probes (a red proof's closure is the red proof), while a subtest
// registration is attributed to its OWN closure — see surveyProbeArms.
type probeFunc struct {
	file  string
	line  int
	calls map[string]int // reader name → call line
}

// probeSurvey is the whole package's test-function index plus the counts the
// floors need.
type probeSurvey struct {
	funcs    map[string]probeFunc         // test name → shape
	subtests map[string][]probeSubtestArm // enclosing test name → its t.Run registrations
	files    int
	examined int
}

// probeSubtestArm is one `t.Run(<name>, <func literal>)` registration: the
// subtest NAME as written (or probeDynamicName), and the tree readers its
// closure calls.
type probeSubtestArm struct {
	name  string
	calls map[string]int
	line  int
}

// surveyProbeArms parses every _test.go file in the package directory once and
// indexes its test functions. It is the only reader of the tree in this file,
// and it uses the package's own directory (not repoRoot) so the survey cannot be
// confused by testdata fixtures.
func surveyProbeArms(t *testing.T) *probeSurvey {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	s := &probeSurvey{funcs: map[string]probeFunc{}, subtests: map[string][]probeSubtestArm{}}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), probeGoFileSuffix) {
			continue
		}
		path := filepath.Join(".", e.Name())
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v", path, rerr)
		}
		f, perr := parser.ParseFile(fset, path, src, parser.ParseComments)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		s.files++
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, probeTestPrefix) {
				continue
			}
			s.examined++
			pf := probeFunc{file: e.Name(), line: fset.Position(fn.Pos()).Line, calls: map[string]int{}}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if name, lit, isRun := probeRunCall(call); isRun {
					s.subtests[fn.Name.Name] = append(s.subtests[fn.Name.Name], probeSubtestArm{
						name:  name,
						calls: probeCallsIn(lit),
						line:  fset.Position(call.Pos()).Line,
					})
					return true // keep descending: a nested t.Run is that subtest's own arm
				}
				if _, reader := probeReaderIndex()[probeCallName(call)]; reader {
					pf.calls[probeCallName(call)] = fset.Position(call.Pos()).Line
					return false
				}
				return true
			})
			s.funcs[fn.Name.Name] = pf
		}
	}
	return s
}

// probeReaderIndex is the reader vocabulary as a set.
func probeReaderIndex() map[string]struct{} {
	idx := make(map[string]struct{}, len(probeTreeReaders))
	for _, r := range probeTreeReaders {
		idx[r] = struct{}{}
	}
	return idx
}

// probeCallName reports the identifier a call targets: `f(...)` → f,
// `pkg.f(...)` → f, anything computed → "".
func probeCallName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

// probeRunCall reports whether a call is a subtest registration — callee `Run`,
// first argument a string literal or the selector `x.probe` (the registry's own
// field) — and returns the name as written plus the closure that carries the arm.
func probeRunCall(call *ast.CallExpr) (name string, body *ast.FuncLit, ok bool) {
	if probeCallName(call) != "Run" || len(call.Args) != 2 {
		return "", nil, false
	}
	lit, isLit := call.Args[1].(*ast.FuncLit)
	if !isLit {
		return "", nil, false
	}
	switch arg := call.Args[0].(type) {
	case *ast.BasicLit:
		if arg.Kind != token.STRING {
			return "", nil, false
		}
		v, err := strconv.Unquote(arg.Value)
		if err != nil {
			return "", nil, false
		}
		return v, lit, true
	case *ast.SelectorExpr:
		if arg.Sel.Name == "probe" {
			return probeDynamicName, lit, true
		}
	}
	return "", nil, false
}

// probeCallsIn collects the tree readers a function literal's body calls.
func probeCallsIn(lit *ast.FuncLit) map[string]int {
	out := map[string]int{}
	idx := probeReaderIndex()
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name := probeCallName(call); name != "" {
			if _, reader := idx[name]; reader {
				out[name] = 1
			}
		}
		return true
	})
	return out
}

// probeEscaped reports whether a reviewed marker sits in the comment block above
// a line.
func probeEscaped(t *testing.T, file string, line int) bool {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(".", file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	lines := strings.Split(string(src), "\n")
	from := line - probeEscapeWindow
	if from < 1 {
		from = 1
	}
	for i := from - 1; i < line-1 && i < len(lines); i++ {
		if strings.Contains(lines[i], probeTreeReaderEscape) {
			return true
		}
	}
	return false
}

// TestGateProbePathsSelectTheTreeArm is the real-tree structural check: the name
// the registry publishes per gate must be the subtest path, that subtest must be
// registered, its closure must reach `scan`, the standalone red proof must not
// read the tree, and every gate must still be dispatched.
func TestGateProbePathsSelectTheTreeArm(t *testing.T) {
	s := surveyProbeArms(t)
	t.Logf("probe-arm survey: %d test file(s), %d test function(s), %d gate(s)",
		s.files, s.examined, len(gates()))

	if s.files < probeFilesFloor || s.examined < probeFuncsFloor {
		t.Fatalf("survey read %d file(s) / %d test function(s), below the floors %d / %d — "+
			"a scan that examined nothing would pass every arm below by finding nothing",
			s.files, s.examined, probeFilesFloor, probeFuncsFloor)
	}

	// The published root must be a test this package DECLARES, or every path
	// below names a subtest of nothing.
	if _, ok := s.funcs[probeArmRoot]; !ok {
		t.Fatalf("the published arm root %q is not declared in this package — every gate's "+
			"published path would name a subtest of a test that does not exist", probeArmRoot)
	}

	// (a) the dispatcher's registrations, and whether their bodies read the tree.
	dispatched := map[string]bool{}
	reachesScan := false
	for _, arm := range s.subtests[probeArmRoot] {
		dispatched[arm.name] = true
		if _, ok := arm.calls["scan"]; ok {
			reachesScan = true
		}
	}
	t.Logf("tree arm %s registers %d subtest(s), dynamic form present: %v",
		probeArmRoot, len(s.subtests[probeArmRoot]), dispatched[probeDynamicName])
	if !reachesScan {
		t.Errorf("%s registers subtests but none of their bodies calls scan — the arm that "+
			"carries the published name cannot see the tree, which is the defect this test "+
			"exists for (2026-10-28: the short red-proof name exited 0 while three "+
			"unbaselined gate-9 hits sat in the tree)", probeArmRoot)
	}

	seen := map[string]string{}
	judged := 0
	for _, g := range gates() {
		g := g
		published := g.probePath()
		if want := probeArmRoot + "/" + g.probe; published != want {
			t.Errorf("%s: published path %q is not the subtest path %q", g.id, published, want)
		}
		if prev, dup := seen[published]; dup {
			t.Errorf("%s and %s publish the SAME name %q — one would silently shadow the "+
				"other's subtest", prev, g.id, published)
		}
		seen[published] = g.id

		// (c) the tree arm really runs for this gate: by name, or through the
		// dynamic registration that covers the whole slice.
		if !dispatched[g.probe] && !dispatched[probeDynamicName] {
			t.Errorf("%s: the tree arm registers neither %q nor the gate variable — "+
				"`%s%s` would select no subtest and exit 0 on a dirty tree",
				g.id, g.probe, probeRunPrefix, published)
		}

		// (b) the standalone red proof exists, and does NOT read the tree.
		fn, ok := s.funcs[g.probe]
		if !ok {
			t.Errorf("%s: the published path names subtest %q, but no test function of that "+
				"name is declared in this package — the record would publish a probe that does "+
				"not exist (PROJECT.md Rule 38)", g.id, g.probe)
			continue
		}
		judged++
		for reader, line := range fn.calls {
			if probeEscaped(t, fn.file, line) {
				continue
			}
			t.Errorf("%s: the standalone red proof %s (%s:%d) calls %s — a red proof proves the "+
				"PREDICATE over a planted snippet and must not read the tree; the tree-reading "+
				"arm is %s. If this reference is a reviewed exception, say so above it with "+
				"`%s <reason>`", g.id, g.probe, fn.file, line, reader, published,
				probeTreeReaderEscape)
		}
	}
	t.Logf("published paths judged: %d (each resolved to a declared red proof and a "+
		"tree-reading subtest registration)", judged)
}

// TestGateProbeArmFindsTheDefectItExistsFor reproduces, against the real survey
// machinery, the shapes that made the 2026-10-28 command green — so a rotted
// check cannot pass by finding nothing:
//
//  1. a published path that names the standalone red proof instead of the
//     subtest (the original defect — `-run <name>` then selects the arm that
//     never reads the tree);
//  2. a dispatcher whose bodies stop calling scan (the tree arm going blind);
//  3. a gate the dispatcher never registers (a gate silently un-enforced).
func TestGateProbeArmFindsTheDefectItExistsFor(t *testing.T) {
	// 1. the short name is NOT the published name — and the difference must be
	// structural, not a substring accident: the published path must not begin
	// with the red proof's name, or an unanchored -run on one still selects both.
	for _, g := range gates() {
		if g.probePath() == g.probe {
			t.Fatalf("%s: probePath() returns the bare probe %q — running it selects the "+
				"fixture arm, which is the defect", g.id, g.probe)
		}
		if !strings.HasPrefix(g.probePath(), probeArmRoot+"/") {
			t.Fatalf("%s: probePath() = %q does not name a subtest of %s",
				g.id, g.probePath(), probeArmRoot)
		}
		if strings.HasPrefix(g.probePath(), g.probe) {
			t.Fatalf("%s: probePath() = %q begins with the red proof's name, so an -run on "+
				"either still selects both arms", g.id, g.probePath())
		}
	}

	s := surveyProbeArms(t)

	// 2. the dispatcher's bodies read the tree: a rebuilt dispatcher without
	// scan is the blind arm, reproduced.
	sawScan := false
	for _, arm := range s.subtests[probeArmRoot] {
		if _, ok := arm.calls["scan"]; ok {
			sawScan = true
		}
	}
	if !sawScan {
		t.Fatalf("%s registers %d subtest(s) and none calls scan — the arm under the "+
			"published name is blind", probeArmRoot, len(s.subtests[probeArmRoot]))
	}

	// 3. every gate is registered: the count is the check, printed so a run that
	// registered nothing cannot read as green.
	byName := 0
	dynamic := false
	for _, arm := range s.subtests[probeArmRoot] {
		if arm.name == probeDynamicName {
			dynamic = true
			continue
		}
		byName++
	}
	t.Logf("tree arm %s: %d gate(s) registered by name, dynamic gate registration: %v, "+
		"gates() holds %d", probeArmRoot, byName, dynamic, len(gates()))
	if !dynamic && byName < len(gates()) {
		t.Fatalf("the tree arm registers %d gate(s) by name and holds no dynamic "+
			"registration, but gates() holds %d — a gate would be un-enforced",
			byName, len(gates()))
	}
}

// TestGatePublishesOnlyPathsThatResolve closes the hole THIS FILE'S OWN first
// draft opened (measured 2026-10-28, task 01a18299): the arms above check the
// names the REGISTRY publishes (gate.probePath()), and they were green while
// PROJECT.md told a reader to run `-run
// TestStandardGates/TestGate2EnvRestoreShapeRedProof` — a path naming a subtest
// that does not exist. `go test` answered `[no tests to run]` and exited 0: a
// documented check that checks nothing, which is exactly the failure shape
// PROJECT.md Rule 38 exists for, reintroduced by the commit that was fixing it.
//
// WHY arm (a) COULD NOT CATCH IT: TestGate2EnvRestoreShapeRedProof is a
// TOP-LEVEL exhaustive test (it walks the package itself), not a `gates()` entry
// — so no registry row published it, and the survey never looked at prose. The
// published path is what a READER runs, and a reader's command lives in
// PROJECT.md and in the normative devdocs; nothing was checking there.
//
// THE RULE THIS ENFORCES: a `TestStandardGates/<ident>` written anywhere a
// reader takes commands from must SELECT at least one real subtest of the tree
// arm. An ident that equals a gate probe resolves; an ident that is a literal
// PREFIX of one (`TestStandardGates/TestGate2`) resolves, because -run is a
// pattern; an ident matching no gate is a command that exits 0 on any tree.
//
// A document that must quote a non-existent path ON PURPOSE (a record of the
// defect) says so above the line with `probe-path-ok: <reason>`.
func TestGatePublishesOnlyPathsThatResolve(t *testing.T) {
	root := repoRoot(t)

	// The population: the project contract plus every NORMATIVE devdoc (the
	// pages whose commands a reader is meant to follow). Historical pages are
	// excluded by their own header — Rule 36 requires them to keep quoting what
	// was typed that day, including commands that have since rotted.
	files := []string{"PROJECT.md"}
	err := filepath.WalkDir(filepath.Join(root, "devdoc"), func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if strings.HasPrefix(string(src), normativeHeader) {
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return rerr
			}
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk devdoc: %v", err)
	}

	probes := map[string]bool{}
	for _, g := range gates() {
		probes[g.probe] = true
	}

	judged, bad := 0, 0
	for _, rel := range files {
		src, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			for _, m := range probePathRe.FindAllStringSubmatch(line, -1) {
				ident := m[1]
				judged++
				if probePathResolves(ident, probes) {
					continue
				}
				if probePathEscaped(lines, i+1) {
					continue
				}
				bad++
				t.Errorf("%s:%d publishes `-run %s/%s`, which selects NO subtest of %s — "+
					"`go test` answers \"[no tests to run]\" and exits 0 on any tree, so the "+
					"documented check checks nothing (PROJECT.md Rule 38). %q is not a "+
					"gates() probe and is not a prefix of one. Name a registered probe "+
					"(%s), or run a top-level test by its OWN name — never as a subtest of "+
					"%s. A page that must quote this path on purpose says so above the line "+
					"with `%s <reason>`.", rel, i+1, probeArmRoot, ident, probeArmRoot,
					ident, strings.Join(sortedProbeNames(probes), ", "), probeArmRoot,
					probePathEscape)
			}
		}
	}
	t.Logf("published-path survey: %d file(s) judged (%d normative devdoc + PROJECT.md), "+
		"%d `%s/<ident>` invocation(s), %d unresolvable", len(files), len(files)-1,
		judged, probeArmRoot, bad)
	if judged < probePathFloor {
		t.Fatalf("only %d published-path invocation(s) found across %d file(s), below the "+
			"floor %d — a survey that read nothing would pass by finding nothing",
			judged, len(files), probePathFloor)
	}
}

// probePathResolves reports whether an ident written after `TestStandardGates/`
// selects at least one real subtest: an exact probe name, or a literal prefix of
// one (-run is a pattern, so `TestGate2` reaches TestGate2TestEnvRedProof).
func probePathResolves(ident string, probes map[string]bool) bool {
	if ident == "" {
		return false
	}
	for name := range probes {
		if name == ident || strings.HasPrefix(name, ident) {
			return true
		}
	}
	return false
}

// probePathEscaped reports whether the reviewed marker sits in the comment/lines
// above a published path (same block-above discipline as probeEscaped).
func probePathEscaped(lines []string, line int) bool {
	from := line - probeEscapeWindow
	if from < 1 {
		from = 1
	}
	for i := from - 1; i < line-1 && i < len(lines); i++ {
		if strings.Contains(lines[i], probePathEscape) {
			return true
		}
	}
	return false
}

// sortedProbeNames lists the registered probes for the failure message, so a
// reader gets the answer in the same breath as the defect.
func sortedProbeNames(probes map[string]bool) []string {
	out := make([]string, 0, len(probes))
	for n := range probes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// the BARE-NAME half of the same defect (task 01a19730)
// ---------------------------------------------------------------------------

// probeBareRef finds a `-run` value written WITHOUT the `TestStandardGates/`
// prefix: the shape a reader produces when they grep a probe name out of a page
// and paste it. It is deliberately anchored on the flag (`-runPattern`), not on
// a line that also says `go test`, because the two measured sites are
// line-wrapped prose whose `-run` sits alone on its line — a census that
// required `go test` on the same line would have read green by finding nothing.
//
// The capture runs PAST `|`: Go's `|` is the alternation separator, so a value
// like `-run 'TestGate9GitEnvRedProof|TestGate9GitEnvScansTheRealTree'` is ONE
// pattern with TWO alternatives. A capture that stopped at the first identifier
// would judge that pattern as the bare probe alone and demand the very repair
// the pattern already carries — the detector's own `…|…` spelling, measured
// 2026-10-28 on this very line of devdoc/standards-gate.md.
const probeBareRef = `-run[= ]+['"]?([A-Za-z][A-Za-z0-9_|]*)`

// probeBareEscape marks a bare `-run <gate probe>` that is DELIBERATELY broken —
// a page recording that the command exits 0 on a dirty tree — reviewed with its
// reason above the line. It is a separate marker from probePathEscape on
// purpose: the two defects are different (a path naming no subtest vs a path
// naming the PREDICATE arm), and one marker would let a repair of either look
// like a repair of both.
const probeBareEscape = "probe-bare-ok:"

// probeBareFloor refuses a survey that read no invocation at all: the measured
// population of `-run <gate probe>` sites in PROJECT.md + the normative devdocs
// is small (2), so the floor is 1 and any drop to zero is reported as the
// vacuum it is (PROJECT.md Rule 38).
const probeBareFloor = 1

// probeBareRe is probeBareRef compiled once (Rule 1: the literal lives above).
var probeBareRe = regexp.MustCompile(probeBareRef)

// TestGateBareProbeNamesSelectNoTreeArm is the GAP-1 half of the 2026-10-28
// defect, mechanised on the population the sibling arm above already walks.
//
// TestGatePublishesOnlyPathsThatResolve judges `TestStandardGates/<ident>` —
// the SUBTEST spelling. It never looked at the bare spelling, and that is the
// spelling a reader most often copies, because it is what the gate's own
// `probe` field, its baseline block and most pages write. A `-run` on it
// selects the standalone RED PROOF only: a predicate over a planted snippet
// that reads no file, so the command exits 0 on any tree.
//
// The judgement is precise, not a blanket ban on bare names: a gate's own red
// proof IS a legitimate thing to run when you are proving the PREDICATE, so the
// arm fires only when the pattern cannot reach a tree-reading arm at all —
// i.e. when EVERY alternative of the `-run` value is a registered `gates()`
// probe (`-run 'TestGate9GitEnvRedProof'`) and none of them is a top-level
// test. `-run 'TestGate9GitEnvRedProof|TestGate9GitEnvScansTheRealTree'` names
// a real-tree arm in the same alternation, so it passes.
func TestGateBareProbeNamesSelectNoTreeArm(t *testing.T) {
	root := repoRoot(t)
	files := normativeCommandSurfaces(t, root)
	probes := map[string]bool{}
	for _, g := range gates() {
		probes[g.probe] = true
	}

	judged, bare, bad := 0, 0, 0
	for _, rel := range files {
		src, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if strings.Contains(line, probeArmRoot+"/") {
				continue // the prefixed spelling is the sibling arm's population
			}
			for _, m := range probeBareRe.FindAllStringSubmatch(line, -1) {
				judged++
				pattern := m[1]
				alts := strings.Split(pattern, "|")
				onlyProbes := true
				for _, a := range alts {
					if !probes[a] {
						onlyProbes = false
						break
					}
				}
				if !onlyProbes {
					continue
				}
				bare++
				if probePathEscaped(lines, i+1) || probeBareLinesEscaped(lines, i+1) {
					continue
				}
				bad++
				t.Errorf("%s:%d publishes `-run %s`, whose every alternative is a registered "+
					"gates() PROBE — the standalone red proof. It reads no file, so the command "+
					"exits 0 on a tree full of violations (measured 2026-10-28: three "+
					"unannotated gate-9 hits while this command printed `ok`). Name the "+
					"tree-reading arm instead: `%s%s`, or this gate's own top-level "+
					"real-tree arm where it has one. A page that quotes the broken command "+
					"ON PURPOSE says so above the line with `%s <reason>`.",
					rel, i+1, pattern, probeRunPrefix, probeArmRoot+"/"+pattern,
					probeBareEscape)
			}
		}
	}
	t.Logf("bare-probe survey: %d file(s), %d `-run` invocation(s) judged, %d naming only "+
		"gates() probes, %d unescaped", len(files), judged, bare, bad)
	if judged < probeBareFloor {
		t.Fatalf("judged %d `-run` invocation(s) across %d file(s), below the floor %d — a "+
			"survey that read nothing would pass by finding nothing",
			judged, len(files), probeBareFloor)
	}
}

// normativeCommandSurfaces is the population both published-path arms judge: the
// project contract plus every NORMATIVE devdoc — the pages a reader is meant to
// take commands from. Historical pages are excluded by their own header (Rule
// 36: a record keeps quoting what was typed that day), and the exclusion is by
// HEADER rather than by filename so a page that changes class changes
// population with it.
func normativeCommandSurfaces(t *testing.T, root string) []string {
	t.Helper()
	files := []string{"PROJECT.md"}
	err := filepath.WalkDir(filepath.Join(root, "devdoc"), func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if !strings.HasPrefix(string(src), normativeHeader) {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk devdoc: %v", err)
	}
	return files
}

// probeBareLinesEscaped reports whether the reviewed marker for a deliberately
// broken BARE command sits in the comment/lines above it (same block-above
// discipline as the other two escapes).
func probeBareLinesEscaped(lines []string, line int) bool {
	from := line - probeEscapeWindow
	if from < 1 {
		from = 1
	}
	for i := from - 1; i < line-1 && i < len(lines); i++ {
		if strings.Contains(lines[i], probeBareEscape) {
			return true
		}
	}
	return false
}
