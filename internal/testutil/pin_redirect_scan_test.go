package testutil

// Completion of the pin gate's marker set: the SHAPE scan.
//
// TestEveryTempUsingSuiteIsPinned (and its sibling TestPinCoverageTable) decide
// "pinned" from a static scan of a package's test sources. Until this file that
// scan was a substring search for the three in-tree entry points
// (pinEntryPointMarkers: testutil.RunSuiteMain / PinSuiteForTestMain /
// PinSuiteTempRoot) — but task 01a13352 then rewrote the gate's FAIL text by
// package SHAPE, telling a package in a NESTED STANDALONE MODULE (one carrying
// its own go.mod, for which every one of those imports is refused by Go) to
// redirect the pin env name in its OWN TestMain as remedy (2). The predicate
// could not see that remedy, so text and predicate disagreed: a module repairing
// itself exactly as instructed stayed reported NO-PIN for ever. That was
// recorded as the `## Not Done` gap of
// devdoc/scratch-pin-gate-nested-remedy.md, and this file closes it.
//
// It does so WITHOUT relaxing the predicate: nothing previously reported is
// exempted, and the marker set is only COMPLETED by a second evidence shape,
// read from the syntax tree rather than from the text:
//
//	pinned := any(pinEntryPointMarkers appears in the test sources)   // unchanged
//	       OR a TestMain whose BODY assigns the pin env name          // added
//	          to something that is not provably the ambient root
//
// The evidence is the ASSIGNMENT, so prose is not evidence: a comment naming the
// variable in any spelling adds nothing (the red proof below pins that), and the
// in-tree marker scan still reports a package that only MENTIONS the helper in a
// comment — the incident Rule 22's own history records.
//
// Rule 22's criterion is untouched: off-/ is a DEVICE-ID fact, and a static scan
// cannot measure device numbers. It therefore refuses only the value spellings
// that are the ambient root BY DEFINITION — the root's own path literal,
// os.TempDir(), whatever os.Getenv of the variable returns, and a path built
// under any of those. Every other directory is accepted, and its device id is
// the runtime fact the FAIL text states the module must satisfy.

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

// The names this predicate is built from, one named constant each (Rules 1/13):
// they are what decides whether a suite counts as pinned, so a bare literal
// would be a second source of truth for the gate's verdict.
const (
	// pinSuiteEntryPoint is the suite entry point whose BODY is read: the
	// package-level `func TestMain(m *testing.M)`, the one place a suite can arm
	// the environment before any test runs.
	pinSuiteEntryPoint = "TestMain"
	// pinEnvSetterSelector is the method name that ASSIGNS an environment
	// variable. The CALL is the evidence; a mention of it is not.
	pinEnvSetterSelector = "Setenv"
	// pinEnvSetterReceiverName is the receiver that assigns the PROCESS env —
	// the only one a TestMain can use (it has no *testing.T).
	pinEnvSetterReceiverName = "os"
	// pinTestEnvSetterReceiverName is the *testing.T/A/B form, accepted for the
	// same reason (a suite's helper may be handed a TB), so the predicate reads
	// the SHAPE of the assignment rather than one package's habit.
	pinTestEnvSetterReceiverName = "t"
	// pinEnvReadSelector is os.Getenv: a redirect to what it returns is a
	// redirect to the ambient root, whatever the variable holds at run time.
	pinEnvReadSelector = "Getenv"
	// pinAmbientRootSelector is os.TempDir(), by definition the ambient root.
	pinAmbientRootSelector = "TempDir"
	// pinAmbientJoinerSelector is filepath.Join: a path built UNDER the ambient
	// root sits on the same device, so it is refused exactly like the root.
	pinAmbientJoinerSelector = "Join"
	// pinEnvNameArg / pinEnvValueArg are the argument positions of the setter
	// call: the variable name and the directory it is pointed at.
	pinEnvNameArg  = 0
	pinEnvValueArg = 1
)

// bannedPinRootPath is the one path this scan refuses as a redirect TARGET: the
// banned temp root itself. It is composed from the constant that names that
// directory (Rules 1/13b) rather than typed as a literal, and the refusal is a
// /-partition fact, not a spelling preference — a path under it is refused too.
func bannedPinRootPath() string {
	return "/" + temptRootDirName
}

// TestPinRedirectMarkerAcceptsARunTimeRedirectAndRefusesProse is the green/red
// proof pair for the completed marker set, driven through the SAME predicate the
// two gate tests call (packagePinned): the accepted shape is a real assignment
// in a TestMain BODY, and the refused shape is the very same variable named in
// prose. A marker that can be satisfied by a comment would be a relaxed
// predicate wearing a new name, which is what this test exists to stop.
func TestPinRedirectMarkerAcceptsARunTimeRedirectAndRefusesProse(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: acceptedPinRedirect,
			src: "package probe\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
				"func TestMain(m *testing.M) {\n" +
				"\tos.Setenv(" + quoted(scratchTempRootVar) + ", probeScratchDir)\n" +
				"\tos.Exit(m.Run())\n}\n",
			want: true,
		},
		{
			name: acceptedPinRedirectSubdir,
			src: "package probe\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n)\n\n" +
				"func TestMain(m *testing.M) {\n" +
				"\tos.Setenv(" + quoted(scratchTempRootVar) + ", filepath.Join(scratchDir, \"t\"))\n" +
				"\tos.Exit(m.Run())\n}\n",
			want: true,
		},
		{
			name: refusedPinProseOnly,
			src: "package probe\n\nimport \"testing\"\n\n" +
				"// A pinned suite would os.Setenv(" + quoted(scratchTempRootVar) + ", dir) here.\n" +
				"// See the gate's FAIL text: redirect TMPDIR in this module's OWN TestMain.\n" +
				"func TestMain(m *testing.M) { os.Exit(m.Run()) }\n",
			want: false,
		},
		{
			name: refusedPinNoTestMain,
			src: "package probe\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
				"func TestArmTheEnv(m *testing.M) {\n" +
				"\tos.Setenv(" + quoted(scratchTempRootVar) + ", probeScratchDir)\n}\n",
			want: false,
		},
		{
			name: refusedPinAmbientLiteral,
			src: "package probe\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
				"func TestMain(m *testing.M) {\n" +
				"\tos.Setenv(" + quoted(scratchTempRootVar) + ", " + quoted(bannedPinRootPath()) + ")\n" +
				"\tos.Exit(m.Run())\n}\n",
			want: false,
		},
		{
			name: refusedPinAmbientLiteralSubdir,
			src: "package probe\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
				"func TestMain(m *testing.M) {\n" +
				"\tos.Setenv(" + quoted(scratchTempRootVar) + ", " + quoted(bannedPinRootPath()+"/probe") + ")\n" +
				"\tos.Exit(m.Run())\n}\n",
			want: false,
		},
		{
			name: refusedPinAmbientTempDir,
			src: "package probe\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n)\n\n" +
				"func TestMain(m *testing.M) {\n" +
				"\tos.Setenv(" + quoted(scratchTempRootVar) + ", filepath.Join(os.TempDir(), \"probe\"))\n" +
				"\tos.Exit(m.Run())\n}\n",
			want: false,
		},
		{
			name: refusedPinAmbientEnvRead,
			src: "package probe\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
				"func TestMain(m *testing.M) {\n" +
				"\tos.Setenv(" + quoted(scratchTempRootVar) + ", os.Getenv(" + quoted(scratchTempRootVar) + "))\n" +
				"\tos.Exit(m.Run())\n}\n",
			want: false,
		},
		{
			name: refusedPinWrongVariable,
			src: "package probe\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
				"func TestMain(m *testing.M) {\n" +
				"\tos.Setenv(\"SOME_OTHER_VAR\", probeScratchDir)\n" +
				"\tos.Exit(m.Run())\n}\n",
			want: false,
		},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeProbeSource(t, dir, c.src)
		if got := packagePinned(t, filepath.Dir(dir), filepath.Base(dir)); got != c.want {
			t.Errorf("packagePinned(%s) = %v, want %v\nsource:\n%s", c.name, got, c.want, c.src)
		}
	}
}

// The fixture names and the two verdict labels, named once (Rules 1/13): the
// table above and the message regression read the same values.
const (
	acceptedPinRedirect            = "accepted: redirect in TestMain"
	acceptedPinRedirectSubdir      = "accepted: redirect to a subdirectory of the target"
	refusedPinProseOnly            = "refused: prose only"
	refusedPinNoTestMain           = "refused: assignment outside TestMain"
	refusedPinAmbientLiteral       = "refused: the ambient root literal"
	refusedPinAmbientLiteralSubdir = "refused: a path under the ambient root"
	refusedPinAmbientTempDir       = "refused: os.TempDir()"
	refusedPinAmbientEnvRead       = "refused: what the variable already holds"
	refusedPinWrongVariable        = "refused: a different variable"
	probeSourceFileName            = "probe_test.go"
)

// TestPinRedirectMarkerIsLoadBearing is the red proof for the COMPLETION itself:
// with the shape scan removed, a package that repairs itself exactly as the
// nested remedy instructs is reported NO-PIN again — at the same predicate the
// gates call. The removal is simulated by handing packagePinned a directory whose
// TestMain is the prose-only shape, i.e. the state the predicate was in before
// this file, and asserting the two verdicts still differ.
func TestPinRedirectMarkerIsLoadBearing(t *testing.T) {
	dir := t.TempDir()
	writeProbeSource(t, dir, "package probe\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n"+
		"func TestMain(m *testing.M) {\n"+
		"\tos.Setenv("+quoted(scratchTempRootVar)+", probeScratchDir)\n"+
		"\tos.Exit(m.Run())\n}\n")
	if !packagePinned(t, filepath.Dir(dir), filepath.Base(dir)) {
		t.Fatalf("a TestMain that assigns %s was NOT accepted — the completion is not wired into packagePinned",
			scratchTempRootVar)
	}
	// The marker set alone (the PRE-completion predicate) must still reject it:
	// that is what makes the scan the load-bearing half rather than a decoration.
	src := readTestSources(t, dir)
	if containsAny(src, pinEntryPointMarkers) {
		t.Fatalf("the fixture carries an in-tree marker, so this proof cannot separate the two halves:\n%s", src)
	}
	if !fileDeclaresPinRedirect(parseProbeFile(t, dir)) {
		t.Errorf("the AST half did not see the assignment that packagePinned accepted — the predicate is "+
			"accepting for some other reason, which is exactly the false green this test guards:\n%s", src)
	}
}

// TestPinRedirectScanReadsTheRealTree is the anti-vacuity anchor: the scan must
// run over the actual repository without erroring, and the packages that pin the
// in-tree way must still be recognised (the completion may only ADD evidence).
func TestPinRedirectScanReadsTheRealTree(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	pinned := 0
	for _, rel := range testPackageDirs(t, root) {
		if packagePinned(t, root, rel) {
			pinned++
		}
	}
	if pinned < gateMinTempPackages {
		t.Fatalf("the completed predicate recognises only %d pinned packages (want at least %d) — a scan that "+
			"sees nothing is not a scan (vacuous pass guard)", pinned, gateMinTempPackages)
	}
	if !packagePinned(t, root, testutilPackageDir) {
		t.Fatalf("%s is not recognised as pinned — the in-tree marker half of the predicate regressed",
			testutilPackageDir)
	}
}

// packagePinned reports whether a test package's suite pins its scratch root.
// It is the ONE predicate both gate tests call (TestEveryTempUsingSuiteIsPinned
// and TestPinCoverageTable), so their verdicts cannot disagree: the in-tree
// marker scan first, then the shape scan that sees the remedy a nested
// standalone module actually has (see this file's header).
func packagePinned(t *testing.T, root, rel string) bool {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(rel))
	if containsAny(readTestSources(t, dir), pinEntryPointMarkers) {
		return true
	}
	return packageDeclaresPinRedirect(t, dir)
}

// packageDeclaresPinRedirect reports whether any test source of dir declares a
// TestMain whose BODY redirects the pin env name off the ambient root.
func packageDeclaresPinRedirect(t *testing.T, dir string) bool {
	t.Helper()
	fset := token.NewFileSet()
	for _, name := range testSourceFileNames(t, dir) {
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			// A test file that does not parse cannot run, so its TestMain arms
			// nothing — but the fallback must be VISIBLE, never silent (Rule 15):
			// the package is then judged by the marker scan alone.
			t.Logf("pin shape scan: %s does not parse (%v) — judged by the marker scan alone", name, err)
			continue
		}
		if fileDeclaresPinRedirect(file) {
			return true
		}
	}
	return false
}

// fileDeclaresPinRedirect reads one parsed file for the accepted shape: a
// package-level `func TestMain(m *testing.M)` whose body ASSIGNS the pin env
// name. Comments are not statements and are never visited (that is the whole
// point of reading the tree instead of the text).
func fileDeclaresPinRedirect(file *ast.File) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != pinSuiteEntryPoint || fn.Type.Params.NumFields() != 1 {
			continue
		}
		if pinRedirectInBody(fn.Body) {
			return true
		}
	}
	return false
}

// pinRedirectInBody reports whether a statement list assigns the pin env name to
// something that is not provably the ambient root. It descends into nested
// blocks so a suite that arms the pin inside an `if` or a closure is still seen.
func pinRedirectInBody(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !pinEnvSetterCall(call) {
			return true
		}
		if !pinRedirectValueIsAmbient(call.Args[pinEnvValueArg]) {
			found = true
			return false
		}
		return true
	})
	return found
}

// pinEnvSetterCall reports whether a call is `<os|t>.Setenv(<the pin env name>, …)`
// — the assignment SHAPE, independent of the spelling a package chose for the
// variable (a literal, or an identifier naming a constant that holds it).
func pinEnvSetterCall(call *ast.CallExpr) bool {
	if len(call.Args) <= pinEnvValueArg {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != pinEnvSetterSelector {
		return false
	}
	recv, ok := sel.X.(*ast.Ident)
	if !ok || (recv.Name != pinEnvSetterReceiverName && recv.Name != pinTestEnvSetterReceiverName) {
		return false
	}
	name := call.Args[pinEnvNameArg]
	if lit, ok := name.(*ast.BasicLit); ok {
		return unquoteLiteral(lit) == scratchTempRootVar
	}
	// A package's own constant for the variable's name — accepted because the
	// name must still SPELL it, never because any identifier will do.
	if id, ok := name.(*ast.Ident); ok {
		return strings.Contains(id.Name, scratchTempRootVar)
	}
	return false
}

// pinRedirectValueIsAmbient reports whether an assigned value is the ambient temp
// root BY DEFINITION — the only thing a STATIC scan can refuse honestly, since
// device numbers are a runtime fact (Rule 22's criterion). The refused shapes:
// the banned root's own path literal (or a path under it), os.TempDir(), and
// what os.Getenv of the variable itself returns.
func pinRedirectValueIsAmbient(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind.String() != "STRING" {
			return false
		}
		value := unquoteLiteral(e)
		return value == bannedPinRootPath() || strings.HasPrefix(value, bannedPinRootPath()+"/")
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok {
			return false
		}
		switch sel.Sel.Name {
		case pinAmbientRootSelector:
			return recv.Name == pinEnvSetterReceiverName
		case pinEnvReadSelector:
			lit, isLit := e.Args[pinEnvNameArg].(*ast.BasicLit)
			return isLit && unquoteLiteral(lit) == scratchTempRootVar
		case pinAmbientJoinerSelector:
			// filepath.Join(<ambient root>, …) is a path UNDER it: same device,
			// same partition, same defect.
			for _, arg := range e.Args {
				if pinRedirectValueIsAmbient(arg) {
					return true
				}
			}
		}
	}
	return false
}

// unquoteLiteral returns a string literal's value in both spellings Go accepts.
// A literal that does not unquote is returned as written, which cannot equal the
// banned root, so the comparison stays conservative.
func unquoteLiteral(lit *ast.BasicLit) string {
	if v, err := strconv.Unquote(lit.Value); err == nil {
		return v
	}
	return lit.Value
}

// quoted renders a Go source string literal, so the fixtures above are built
// from the same constants the predicate reads instead of a second spelling.
func quoted(s string) string {
	return strconv.Quote(s)
}

// writeProbeSource materialises one fixture source file in a directory that
// holds only that file — the shape packagePinned reads.
func writeProbeSource(t *testing.T, dir, src string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, probeSourceFileName), []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", probeSourceFileName, err)
	}
}

// parseProbeFile parses the fixture written by writeProbeSource.
func parseProbeFile(t *testing.T, dir string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, probeSourceFileName), nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", probeSourceFileName, err)
	}
	return file
}
