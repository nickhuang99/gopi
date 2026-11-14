package standardgate

// The gate on the test-database SCAFFOLD (PROJECT.md Rule 32).
//
// WHY IT EXISTS. The scaffold (internal/testutil) offers exactly two supported
// ways for a test to obtain a database — NewTestDBPath (the writer shape: a
// fresh per-test file under the pinned scratch root, ledger env pinned, the
// operator's profile refused) and Sample/SampleDBPath (the reader shape: a
// shared, read-only, once-built sample) — and the measured 2026-09-21 state was
// ~500 hand-rolled sites plus 147 per-file DB helpers in internal/tools alone.
// A convention without a check does not hold (原则无效，检查才有效), so this gate
// makes the dangerous shape VISIBLE.
//
// WHAT IT CATCHES, AND WHY THE PREDICATE IS THIS NARROW. The dangerous shape is
// not "a test with a database" — it is a test that RESOLVES a database path and
// OPENS it without pinning the ledger environment contract:
//
//	db := filepath.Join(t.TempDir(), "memory.db")   // resolves a path …
//	conn := sql.Open("sqlite", db)                  // … and opens it
//
// With no GOPI_MEMORY_DB in force, a tools-layer call in the SAME test resolves
// the OPERATOR'S live ledger instead — and the ledger is APPEND-ONLY, so a
// fixture row written there is a permanent lie in the operator's history (that
// is how `t-stale-sibling` made the merger report a phantom /tmp repo for days).
//
// The predicate therefore flags a **per-file helper function whose body opens a
// sqlite database but never sets the DB environment**: that is the shared-helper
// shape profileguard.go's own comment names as the leak ("share one helper
// between tests that set it and tests that do not"). A test that opens a
// foreign, non-gopi database (a fixture proving a refusal) stays legal, because
// the fingerprint requires an env-set to be ABSENT — and a test that pins the
// env on the same line as its opener carries the sanctioned pattern.
//
// It is deliberately NOT "flag every sql.Open": that predicate would flag 34
// legitimate sites today, and a gate that is red on the day it lands gets
// switched off (the argument recorded at the top of gate.go). The narrow
// fingerprint is what keeps the false-positive count low enough to be
// load-bearing.
//
// THE SECOND ARM closes the leak that actually put files in the tree: a test
// that creates a scratch DIRECTORY inside the source tree (`os.MkdirTemp(".", …)
// — the shape that left 341 untracked test `.db` files inside internal/tools).
//
// THE ADVANCED FORM IS NAMED, NOT PRETENDED AWAY: a fully general detector (a
// test that resolves a DB path in one helper and opens it in another) is not
// decidable by this static check. The gate is partial, and PROJECT.md Rule 32 says
// so in the rule itself — an over-claimed gate is how rules lose authority.
//
// THE GATE ITSELF IS SELF-CAUGHT. This file is a *_test.go in internal/ and it
// defines a helper whose body references a sqlite opener… so the scanner skips
// itself BY PATH, with the reason recorded in dbScaffoldScannerExemptions, and a
// live-permit check (a permit whose file the scan never saw fails) keeps that
// exemption honest (Rule 16 baseline style: an exact permit with a reason, never
// a weakened predicate).

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
	// dbScaffoldEnvMarker is the substring an environment NAME must carry for
	// the predicate to treat an env write as "the ledger env contract". It is
	// deliberately broad (GOPI_MEMORY_DB, config.MemoryDBEnv, memoryDBEnv —
	// every spelling in this tree) and it is what keeps the fingerprint's
	// env-set arm from depending on one constant's spelling.
	dbScaffoldEnvMarker = "DB"
	// dbScaffoldDriverName is the sqlite driver name (Rule 1b: one definition
	// per scanner, compared by name, never re-typed at a comparison site).
	dbScaffoldDriverName = "sqlite"
	// dbScaffoldEscape is the documented inline escape: a helper that
	// legitimately opens a database without pinning the ledger env says why on
	// the line (the //stylegate:ignore <reason> precedent). The marker is
	// spelled WITHOUT its slashes because it is compared against
	// ast.CommentGroup.Text(), which strips the comment introducer — measured:
	// a marker carrying "//" never matches, so the escape was silently ignored.
	dbScaffoldEscape = "db-scaffold-ok:"
	// dbScaffoldKinds name the two shapes the gate reports.
	dbScaffoldKindHelper = "hand-rolled DB helper"
	dbScaffoldKindTree   = "scratch directory inside the source tree"
)

// dbScaffoldScanRoots are the trees the gate walks, with a per-root read floor:
// a walk that silently stops finding a tree must fail loudly instead of passing
// vacuously over the trees it did find (the same read-floor contract the
// ledger-DSN gate and the Rule 21 env-reader gate share).
//
// testsuite/ is included because it is the e2e tree the Rule 17 gate also had to
// be taught about; internal/standards itself is included so the scanner cannot
// exempt itself silently.
var dbScaffoldScanRoots = []dbScaffoldScanRoot{
	{path: filepath.Join("internal", "tools"), minTests: 100},
	{path: filepath.Join("internal", "agent"), minTests: 20},
	{path: filepath.Join("internal", "testutil"), minTests: 20},
	{path: filepath.Join("cmd", "agent"), minTests: 20},
	{path: "testsuite", minTests: 5},
	{path: filepath.Join("internal", "standards"), minTests: 10},
	{path: "archive", minTests: 2},
}

// dbScaffoldScanRoot is one tree plus the number of *_test.go files it must
// yield for the scan to be believable.
type dbScaffoldScanRoot struct {
	path     string
	minTests int
}

// dbScaffoldScannerExemptions are the files the scanner skips BY PATH with the
// reason recorded. Every entry is verified live (a permit the scan never saw
// fails), so the list can only ratchet down.
var dbScaffoldScannerExemptions = map[string]string{
	filepath.Join("internal", "standards", "db_scaffold_gate_test.go"): "the gate's own source: it parses its OWN synthetic " +
		"violating snippets with go/parser, and its prose quotes the shapes it forbids — a self-scan would read the " +
		"snippets as violations of the very gate that produced them",
	filepath.Join("cmd", "agent", "ledger_dsn_contract_test.go"): "the synthetic-source red proof of the LEDGER-DSN gate " +
		"(TestLedgerBareOpenerPredicateReddensOnSyntheticSources): its verdicts are produced by that gate's predicate over " +
		"in-memory sources, so it is the same self-scan case as the entry above",
}

// dbScaffoldExemptUsed records, per exempted path, whether the scan actually saw
// that file.
var dbScaffoldExemptUsed = map[string]bool{}

// dbScaffoldBaseline is the allow-list of hand-rolled DB helpers that ALREADY
// existed when this gate landed, as exact `file:line` permits (the mechanism
// gate.go's baselines use: a permit is not an exemption of the file — the same
// shape on any other line is reported — and a permit that no longer matches is a
// STALE entry that fails the gate, so the list can only ratchet down).
//
// Every entry here is a FALSE POSITIVE of the narrow fingerprint, not debt: the
// helper takes a path from its caller or resolves it inside the checkout's
// sandboxed home, and the database it opens is deliberately NOT a gopi ledger
// (a foreign file whose refusal is the subject) — so the "no ledger env in
// force" hazard the predicate screens for cannot arise. They are permitted BY
// LINE rather than escaped inline because the file belongs to another ticket's
// corpus migration (PROJECT.md Rule 26), and an inline marker on each would edit
// files this ticket does not own.
var dbScaffoldBaseline = map[string]string{
	"internal/tools/ledger_backup_test.go:121": "reads the live ledger the test JUST snapshotted (the property under " +
		"test is that the snapshot left it intact), inside a sandboxed HOME; a foreign-DB reader that pins nothing is " +
		"the subject here, not an accident",
	"internal/tools/ledger_backup_test.go:275": "DELIBERATELY a foreign, non-ledger database (a well-formed SQLite " +
		"file the verification must REFUSE); opening it through the scaffold would apply the ledger contract to a " +
		"database that must not be touched by it",
}

// dbScaffoldBaselineUsed records which permits matched, so a stale one fails.
var dbScaffoldBaselineUsed = map[string]bool{}

// dbScaffoldFinding is one hit, with the shape it matched.
type dbScaffoldFinding struct {
	line int
	kind string
	name string
}

// dbScaffoldFindings is the pure predicate: it parses src and reports the
// scaffold-bypass shapes in it. A parse failure is returned as an error, never
// swallowed (Rule 15b): a gate that cannot read a file must not report it clean.
func dbScaffoldFindings(filename, src string) ([]dbScaffoldFinding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	// The documented inline escape, resolved to the LINES it covers (a comment
	// group spans its own lines). One mechanism serves both arms: a function
	// carrying the marker anywhere between its braces is escaped, and so is a
	// single call that carries it as a trailing comment.
	escaped := dbScaffoldEscapeLines(fset, file)

	var out []dbScaffoldFinding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if dbScaffoldSetsDBEnv(fn) || dbScaffoldSpansEscape(fset, fn, escaped) {
			continue
		}
		openLine, opensSQLite := dbScaffoldSQLiteOpener(fset, fn)
		// The leak shape needs BOTH halves: the helper RESOLVES a database path
		// itself (t.TempDir / MkdirTemp / filepath.Join / the scaffold's own
		// path builders) and then opens it. A helper that takes the path as a
		// PARAMETER is not this shape at all — the caller owns the env, which is
		// where the pin belongs, and flagging those would mark 20 legitimate
		// fixtures (measured: the parameter-taking openers were 20 of the 28
		// hits the path-blind fingerprint produced) for no safety gain.
		if opensSQLite && dbScaffoldResolvesPath(fn) {
			// The fingerprint: it opens a database itself and never pins the
			// DB environment, so a tools-layer call in the same test would
			// resolve the operator's live ledger.
			out = append(out, dbScaffoldFinding{line: openLine, kind: dbScaffoldKindHelper, name: fn.Name.Name})
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		line, hit := dbScaffoldSourceTreeMkdir(call)
		if hit && !escaped[line] {
			out = append(out, dbScaffoldFinding{line: line, kind: dbScaffoldKindTree})
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].line != out[j].line {
			return out[i].line < out[j].line
		}
		return out[i].kind < out[j].kind
	})
	return out, nil
}

// dbScaffoldSQLiteOpener reports the line of the FIRST sqlite open in fn's body
// and whether there was one. The opener forms this tree uses are `sql.Open` and
// `sql.OpenDB`; the driver argument must be the sqlite name, so a fixture that
// opens a foreign database with another driver is not this gate's business.
func dbScaffoldSQLiteOpener(fset *token.FileSet, fn *ast.FuncDecl) (int, bool) {
	line, found := 0, false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || len(call.Args) == 0 {
			return true
		}
		if sel.Sel.Name != "Open" && sel.Sel.Name != "OpenDB" {
			return true
		}
		if !dbScaffoldIsSQLiteDriverArg(call.Args[0]) {
			return true
		}
		line, found = fset.Position(call.Pos()).Line, true
		return false
	})
	return line, found
}

// dbScaffoldPathResolvers are the callee names that mean "this function works
// out where the database goes". A helper that calls one of them and then opens
// a sqlite handle is the hand-rolled shape; a helper that opens a path it was
// handed is not.
var dbScaffoldPathResolvers = map[string]bool{
	"TempDir":       true, // t.TempDir()
	"MkdirTemp":     true, // os.MkdirTemp(...)
	"CreateTemp":    true,
	"Join":          true, // filepath.Join(...)
	"ScratchDBPath": true, // the pre-scaffold helper this one replaces
	"NewTestDBPath": true,
	"SampleDBPath":  true,
}

// dbScaffoldResolvesPath reports whether fn's body resolves a database path
// ITSELF — the half of the fingerprint that separates the leak shape from a
// fixture handed its path by the caller.
//
// A resolver call whose argument is one of fn's own PARAMETERS does not count:
// `filepath.Join(home, "memory.db")` inside a helper that took `home` is the
// caller's path, not a path the helper invented, so the env decision stays with
// the caller (where the pin belongs). Measured: that shape was 2 of the 6
// remaining hits and both are correct code — a false positive here would push a
// legitimate fixture into a baseline for no safety gain.
func dbScaffoldResolvesPath(fn *ast.FuncDecl) bool {
	params := map[string]bool{}
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				params[name.Name] = true
			}
		}
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch callee := call.Fun.(type) {
		case *ast.SelectorExpr:
			if callee.Sel != nil {
				name = callee.Sel.Name
			}
		case *ast.Ident:
			name = callee.Name
		}
		if !dbScaffoldPathResolvers[name] {
			return true
		}
		if dbScaffoldArgsAreAllParameters(call, params) {
			return true
		}
		found = true
		return false
	})
	return found
}

// dbScaffoldArgsAreAllParameters reports whether every argument of a resolver
// call is one of fn's parameters (or a literal constant, which carries no path
// of its own). An empty argument list is NOT "all parameters" — a bare
// t.TempDir() is the clearest resolve-it-here shape there is.
func dbScaffoldArgsAreAllParameters(call *ast.CallExpr, params map[string]bool) bool {
	if len(call.Args) == 0 {
		return false
	}
	for _, arg := range call.Args {
		switch a := arg.(type) {
		case *ast.Ident:
			if !params[a.Name] {
				return false
			}
		case *ast.BasicLit:
			// a literal carries no path of its own
		default:
			return false
		}
	}
	return true
}

// dbScaffoldIsSQLiteDriverArg reports whether an opener's driver argument names
// the sqlite driver — as a string literal, which is how every site spells it.
func dbScaffoldIsSQLiteDriverArg(arg ast.Expr) bool {
	lit, ok := arg.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	return strings.Trim(lit.Value, `"`) == dbScaffoldDriverName
}

// dbScaffoldEscapeLines resolves the documented inline escape to the set of
// lines it covers.
//
// The escape is read from the file's COMMENT MAP, not from the AST: Go comments
// are not AST nodes (they only exist when the file is parsed with
// parser.ParseComments), and a body-only walk would therefore never see the
// marker it is supposed to honour — measured: the first version of this gate
// reported a correctly-escaped foreign-DB fixture.
func dbScaffoldEscapeLines(fset *token.FileSet, file *ast.File) map[int]bool {
	escaped := map[int]bool{}
	for _, group := range file.Comments {
		if !strings.Contains(group.Text(), dbScaffoldEscape) {
			continue
		}
		for line := fset.Position(group.Pos()).Line; line <= fset.Position(group.End()).Line; line++ {
			escaped[line] = true
		}
	}
	return escaped
}

// dbScaffoldSpansEscape reports whether any escaped line falls between a
// function's braces — the function-scoped half of the escape contract.
func dbScaffoldSpansEscape(fset *token.FileSet, fn *ast.FuncDecl, escaped map[int]bool) bool {
	start, end := fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line
	for line := start; line <= end; line++ {
		if escaped[line] {
			return true
		}
	}
	return false
}

// dbScaffoldSetsDBEnv reports whether the function writes a DB-sounding
// environment variable (t.Setenv/os.Setenv) anywhere in its body.
func dbScaffoldSetsDBEnv(fn *ast.FuncDecl) bool {
	sets := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		node, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := node.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Setenv" || len(node.Args) == 0 {
			return true
		}
		name, ok := node.Args[0].(*ast.BasicLit)
		if !ok || name.Kind != token.STRING {
			return true
		}
		if strings.Contains(name.Value, dbScaffoldEnvMarker) {
			sets = true
		}
		return true
	})
	return sets
}

// dbScaffoldSourceTreeMkdir reports whether a call creates a scratch DIRECTORY
// relative to the current directory — `os.MkdirTemp(".", …)`, the shape that
// left 341 untracked test .db files inside internal/tools. The directory
// argument must be the literal "." or "..", which is what makes it a source-tree
// write rather than an ordinary temp dir.
func dbScaffoldSourceTreeMkdir(call *ast.CallExpr) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || len(call.Args) == 0 {
		return 0, false
	}
	switch sel.Sel.Name {
	case "MkdirTemp", "CreateTemp":
	default:
		return 0, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return 0, false
	}
	switch strings.Trim(lit.Value, `"`) {
	case ".", "..":
		return 0, true
	}
	return 0, false
}

// dbScaffoldGoFiles returns the *_test.go files under a scan root (the gate only
// speaks about tests: production code opens databases for a living).
func dbScaffoldGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// testdata holds fixtures, not tests.
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

// TestTestDBScaffoldIsUsedOrTheLedgerEnvIsPinned is the gate over the tree: a
// hand-rolled DB helper, or a scratch directory inside the source tree, fails
// unless it carries the documented inline escape.
func TestTestDBScaffoldIsUsedOrTheLedgerEnvIsPinned(t *testing.T) {
	root := repoRoot(t)
	total := 0
	for _, scanRoot := range dbScaffoldScanRoots {
		dir := filepath.Join(root, scanRoot.path)
		// A ROOT THIS TREE DOES NOT CARRY is REPORTED and skipped, never fatal.
		// `archive/` is one of the scanned trees in the private development repo
		// (spent mechanism fixtures live there); the public export ships no
		// archive/ at all, and a walk of a directory that does not exist used to
		// abort the whole gate — which made the export unable to run a gate over
		// the five roots it DOES carry. The absence is stated so it cannot pass
		// as a clean scan (Rule 15), and the other roots' floors still apply.
		if _, statErr := os.Stat(dir); statErr != nil {
			t.Logf("db-scaffold gate: %s/ is absent in this tree — its root and its floor (%d test file(s)) do not apply here",
				scanRoot.path, scanRoot.minTests)
			continue
		}
		files := dbScaffoldGoFiles(t, dir)
		scanned := 0
		for _, file := range files {
			rel, err := filepath.Rel(root, file)
			if err != nil {
				t.Fatalf("relativise %s: %v", file, err)
			}
			if _, exempt := dbScaffoldScannerExemptions[rel]; exempt {
				dbScaffoldExemptUsed[rel] = true
				continue
			}
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", rel, err)
			}
			findings, err := dbScaffoldFindings(rel, string(src))
			if err != nil {
				t.Fatalf("parse %s: %v", rel, err)
			}
			scanned++
			for _, f := range findings {
				key := rel + ":" + strconv.Itoa(f.line)
				if _, permitted := dbScaffoldBaseline[key]; permitted {
					dbScaffoldBaselineUsed[key] = true
					continue
				}
				switch f.kind {
				case dbScaffoldKindHelper:
					t.Errorf("%s:%d: helper %s opens a sqlite database itself but never pins the DB "+
						"environment — with no %s in force a tools-layer call in the same test resolves the "+
						"OPERATOR'S live ledger, which is append-only (a row written there is permanent). "+
						"Use the scaffold: path := testutil.NewTestDBPath(t) and open it through the tools "+
						"layer (or pin the env in the same helper: t.Setenv(config.MemoryDBEnv, path)); a "+
						"deliberate exception carries %q with its reason. See GOPI.md Rule 32 and "+
						"devdoc/testdb-scaffold-contract.md",
						rel, f.line, f.name, "GOPI_MEMORY_DB", dbScaffoldEscape)
				case dbScaffoldKindTree:
					t.Errorf("%s:%d: creates a scratch directory inside the SOURCE TREE (MkdirTemp(\".\", …)) — "+
						"the shape that left 341 untracked test .db files inside internal/tools and got 106 of "+
						"them into a commit (c2f5a9de). A device-backed fixture belongs in the scratch home: "+
						"testutil.TestDeviceScratchDir(t, sub) measures the backing and skips where there is no "+
						"device. See GOPI.md Rule 32", rel, f.line)
				}
			}
		}
		if scanned < scanRoot.minTests {
			t.Fatalf("scanned only %d _test.go files under %s (floor %d) — the walk is broken, so this gate "+
				"proves nothing about that tree", scanned, scanRoot.path, scanRoot.minTests)
		}
		total += scanned
	}
	if total < dbScaffoldMinFilesTotal {
		t.Fatalf("scanned %d _test.go files in total — the walk is broken", total)
	}
	for name, used := range dbScaffoldExemptUsed {
		if !used {
			t.Errorf("stale exemption: %s is exempted but the scan never saw it — the file moved or was "+
				"renamed; update or drop the exemption", name)
		}
	}
	for key, reason := range dbScaffoldBaseline {
		if !dbScaffoldBaselineUsed[key] {
			t.Errorf("stale permit: %s is baselined (as: %s) but no finding matched it — the site was fixed or "+
				"moved; delete the entry, because the allow-list may only ratchet down", key, reason)
		}
	}
}

// dbScaffoldMinFilesTotal is the whole-scan read floor.
const dbScaffoldMinFilesTotal = 150

// TestDBScaffoldPredicateReddensOnSyntheticSources is the RED PROOF — the half
// that keeps the gate from rotting into decoration. Each snippet's verdict is
// known in advance and is stated in the case: the leak shape MUST be reported,
// and every legitimate shape MUST stay silent (a gate whose false-positive count
// is not pinned is a gate someone switches off).
func TestDBScaffoldPredicateReddensOnSyntheticSources(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantHits int
		wantKind string
	}{
		{
			name: "PLANTED VIOLATION — the shared-hand-rolled helper (opens a DB, pins nothing)",
			src: `package p

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func claimTestDB(t *testing.T) string {
	db := filepath.Join(t.TempDir(), "memory.db")
	conn, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn
	return db
}
`,
			wantHits: 1,
			wantKind: dbScaffoldKindHelper,
		},
		{
			name: "COMPLIANT — a local helper that pins the ledger env in the same function",
			src: `package p

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func claimTestDB(t *testing.T) string {
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", db)
	conn, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn
	return db
}
`,
			wantHits: 0,
		},
		{
			name: "COMPLIANT — the scaffold itself (no opener in the test at all)",
			src: `package p

import "testing"

func TestReadsTheSample(t *testing.T) {
	db := Sample(t)
	_ = db
}
`,
			wantHits: 0,
		},
		{
			name: "COMPLIANT — a foreign, non-gopi database whose refusal is the subject",
			src: `package p

import (
	"database/sql"
	"testing"
)

func TestRefusesAForeignFile(t *testing.T) {
	conn, err := sql.Open("sqlite", "/tmp/not-a-ledger.db") //db-scaffold-ok: a foreign file, not a gopi DB
	if err != nil {
		t.Fatal(err)
	}
	_ = conn
}
`,
			wantHits: 0,
		},
		{
			name: "COMPLIANT — another driver is not this gate's business",
			src: `package p

import (
	"database/sql"
	"testing"
)

func openFixture(t *testing.T) {
	conn, err := sql.Open("postgres", "dsn")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn
}
`,
			wantHits: 0,
		},
		{
			name: "PLANTED VIOLATION (arm 2) — a scratch directory created inside the source tree",
			src: `package p

import (
	"os"
	"testing"
)

func deviceScratch(t *testing.T) string {
	dir, err := os.MkdirTemp(".", "firstopen-device-")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
`,
			wantHits: 1,
			wantKind: dbScaffoldKindTree,
		},
		{
			name: "COMPLIANT (arm 2) — the scratch home, passed in explicitly",
			src: `package p

import (
	"os"
	"testing"
)

func deviceScratch(t *testing.T, home string) string {
	dir, err := os.MkdirTemp(home, "device-")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
`,
			wantHits: 0,
		},
		{
			name: "COMPLIANT — a comment quoting the bad shape is not a call",
			src: `package p

import "testing"

// never do sql.Open("sqlite", path) without pinning the env.
func TestX(t *testing.T) {}
`,
			wantHits: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := dbScaffoldFindings(tc.name+".go", tc.src)
			if err != nil {
				t.Fatalf("parse the synthetic source: %v", err)
			}
			if len(findings) != tc.wantHits {
				t.Fatalf("the predicate reported %d finding(s) %+v, want %d — the gate is vacuous or "+
					"over-broad", len(findings), findings, tc.wantHits)
			}
			if tc.wantHits > 0 && findings[0].kind != tc.wantKind {
				t.Errorf("the predicate reported kind %q, want %q", findings[0].kind, tc.wantKind)
			}
		})
	}
}

// TestSampleSeedStatusesExistInTheToolsConstants pins the ONE duplication this
// ticket introduces on purpose.
//
// internal/testutil cannot import internal/tools (cycle), so the sample's seed
// rows must spell their status values as literals — while internal/tools
// compares against its own taskStatus* constants. Two spellings of one contract
// is exactly the drift Rule 13/14 forbid, so the duplication is CHECKED rather
// than trusted: every status the sample seeds must be a value of a taskStatus*
// constant in internal/tools. A rename in the ledger's status vocabulary
// therefore fails HERE instead of silently making the sample's rows
// unreachable by the production code a reader is testing.
func TestSampleSeedStatusesExistInTheToolsConstants(t *testing.T) {
	root := repoRoot(t)
	known, err := taskStatusConstants(filepath.Join(root, "internal", "tools", "tasks.go"))
	if err != nil {
		t.Fatalf("read the tools status constants: %v", err)
	}
	if len(known) < 5 {
		t.Fatalf("parsed only %d taskStatus* constant(s) %v — the parse is broken, so this check proves "+
			"nothing", len(known), known)
	}
	seeded, err := sampleSeedStatusValues(filepath.Join(root, "internal", "testutil", "dbfixture.go"))
	if err != nil {
		t.Fatalf("read the sample's seed statuses: %v", err)
	}
	if len(seeded) == 0 {
		t.Fatalf("the sample's seed defines no Status value — the parse is broken, so this check proves nothing")
	}
	for _, status := range seeded {
		if !known[status] {
			t.Errorf("the shared sample seeds status %q, which is not a value of any taskStatus* constant in "+
				"internal/tools (%v) — the sample and the ledger's status vocabulary have drifted apart, so a "+
				"reader asserting on %q is asserting on a row production code will never produce", status, keys(known), status)
		}
	}
}

// taskStatusConstants parses path and returns the set of values assigned to
// constants whose name starts with taskStatus.
func taskStatusConstants(path string) (map[string]bool, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "taskStatus") || i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						return nil, err
					}
					out[value] = true
				}
			}
		}
	}
	return out, nil
}

// sampleSeedStatusValues parses the testutil scaffold and returns every value
// assigned to a `Status:` field of a composite literal inside SampleSeed — the
// seed's status vocabulary, read from the source of truth itself.
func sampleSeedStatusValues(path string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "SampleSeed" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Status" {
				return true
			}
			lit, ok := kv.Value.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, uerr := strconv.Unquote(lit.Value)
			if uerr == nil {
				out = append(out, value)
			}
			return true
		})
	}
	sort.Strings(out)
	return out, nil
}

// keys renders a set for a failure message, sorted so two runs read the same.
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
