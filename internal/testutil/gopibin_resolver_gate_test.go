package testutil

// gopibin_resolver_gate_test.go — the shape gate for the STALE-BINARY family.
//
// WHY THIS EXISTS (measured, not hypothetical). A test that drives a gopi
// binary must ask THIS package where that binary is (GopiBin, run.go). A test
// that resolves it itself is the stale-binary family: on a shared checkout
// (many worktrees side by side, Rule 6a) `filepath.Join("..", "..", "bin",
// "gopi")` is not necessarily THIS worktree's path, so the case silently
// executes ANOTHER TREE'S build, and the E2E's verdict is then about code
// nobody is working on. devdoc/tested-binary-pin.md carries the measurement
// (a "flake" that was two different revisions, one of them reaching the test
// through exactly this spelling).
//
// The gate is a SHAPE check over the module's Go sources, not a review habit:
// the four spellings below are found by parsing, and a new one fails the gate.
// It is deliberately narrow — it demands the ONE resolver only where a binary
// is being resolved, and its permit table says, per surviving site, why that
// site is not one.

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

// The detector's vocabulary (Rule 1: no bare string keywords).
const (
	// gopiResolverKindJoin is `filepath.Join(…, "bin", "gopi")` — the
	// tree-relative spelling whose `<root>` is wandered into by whichever
	// process happens to be running.
	gopiResolverKindJoin = "join-bin-gopi"
	// gopiResolverKindLiteral is a single string literal that already spells a
	// repo-relative path to the binary ("../../bin/gopi", "./bin/gopi").
	gopiResolverKindLiteral = "literal-bin-gopi"
	// gopiResolverKindEnvRead is a direct read of a pinned-binary channel
	// outside this file's own resolution (run.go owns the precedence).
	gopiResolverKindEnvRead = "env-pin-read"
	// gopiResolverKindPathName is exec.Command of the bare name "gopi",
	// resolved through PATH — the same defect one indirection further out.
	gopiResolverKindPathName = "path-resolved-name"

	// gopiBinBase is the binary's file name and gopiBinDir its directory, taken
	// from the ONE definition (run.go's BinFileName / BinDirName) so the gate
	// recognises the sanctioned spelling without re-typing it (Rule 13).
	gopiBinBase = BinFileName
	gopiBinDir  = BinDirName
	// gopiBinRelSuffix is the literal path tail a hand-rolled resolver spells.
	gopiBinRelSuffix = "/" + gopiBinDir + "/" + gopiBinBase

	gopiResolverGateSelfFile = "gopibin_resolver_gate_test.go"
	gopiResolverResolverFile = "run.go"
	gopiResolverScanFloor    = 400 // tracked Go files the scan must have read

	// gopiResolverDeclOnlyWhy is printed with every literal the census does NOT
	// report, so an exclusion is auditable instead of silent (Rule 15: a scan
	// that quietly looked past something is indistinguishable from a clean
	// tree). See gopiResolverDeclarationOnly for the criterion.
	gopiResolverDeclOnlyWhy = "not a resolution — this file declares no import and no " +
		"function, so it is a shared-spellings LEAF (Rules 1/13/14): the literal is the " +
		"DEFINED VALUE of a package-level constant, and no code here can stat, open or " +
		"execute anything"
)

// gopiResolverRootExpr reports whether a Join base names the MODULE ROOT rather
// than a directory a test invented. The distinction is what keeps the gate
// narrow and its permits empty: a base spelled as a RELATIVE LITERAL ("..") or
// produced by a CALL (moduleRoot(), repoRootFromCwd()) is the module root being
// wandered into from wherever the process stands — the stale-binary shape,
// because in a shared checkout that wander lands in another worktree. A bare
// IDENTIFIER base is, in this tree, a fixture directory (every hit of the form
// filepath.Join(repo, "bin", "gopi") is a synthetic repo built under t.TempDir()),
// so the gate does not fire on it and no permit row has to pretend it is a
// resolver.
func gopiResolverRootExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return false
		}
		s, err := strconv.Unquote(v.Value)
		return err == nil && (s == ".." || strings.HasSuffix(s, "/..") || s == "../..")
	case *ast.CallExpr:
		return true
	}
	return false
}

// gopiResolverDeclarationOnly answers whether a parsed file is CAPABLE of
// resolving a binary at all, and the answer is structural rather than a list of
// blessed paths: a file that declares NO import and NO function is a leaf of
// package-level declarations — const/var/type only. Nothing in it can open,
// stat, join, read an env channel or exec: every one of those needs a function
// body and a package that supplies it (`os`, `os/exec`, `path/filepath`,
// `syscall`), and this file has neither.
//
// WHY THE EXCLUSION EXISTS (measured 2026-10-29, task 01a188ad). The detector
// fires on a string literal's VALUE alone, which cannot distinguish a
// resolution from the site where a shared spelling is DEFINED. `internal/
// mansource/paths.go` is exactly that site: `BuiltBinaryRel = "bin/gopi"` is the
// declared build artifact's repo-relative path, handed to the man generator as
// an ARGV ELEMENT (`go run <generator> . <that path> --check`, man_gate.go:185)
// and never resolved against a process's CWD. It is the opposite of the
// stale-binary family: an argv element is meaningless as a relative path, so it
// cannot land in another worktree's build. Flagging it forced a choice between
// two wrong answers — a permit row that would have to call a production spelling
// "not a resolver" (the table's rows all say exactly that, and here it would be
// false), or splitting the literal (Rule 13's banned evasion). The third answer
// is to teach the detector the one structural fact that makes the site safe.
//
// WHY THIS DOES NOT WEAKEN THE GATE: the exclusion needs BOTH halves, and a
// site that can act fails the first. A test that spells `const bin = "./bin/
// gopi"` and then stats/execs it lives in a file with an import and a func, so
// it is reported exactly as before. The census PRINTS every excluded literal
// with this reason (gopiResolverDeclOnlyWhy), so the exclusion can be audited
// from the -v output alone, and
// TestGopiBinaryResolverDeclarationOnlyFileIsNotAResolution pins both arms.
func gopiResolverDeclarationOnly(f *ast.File) bool {
	if len(f.Imports) > 0 {
		return false
	}
	for _, d := range f.Decls {
		if _, ok := d.(*ast.FuncDecl); ok {
			return false
		}
	}
	return true
}

// gopiResolverDeclaredValues returns the expressions that are the VALUE of a
// package-level const or var spec. Those are DEFINITIONS, and they are the only
// literals the declaration-only exclusion may pass over: a literal anywhere else
// in the file (a function body, a composite literal, a struct tag) is code and
// is reported as it always was.
func gopiResolverDeclaredValues(f *ast.File) map[ast.Expr]bool {
	declared := map[ast.Expr]bool{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				declared[v] = true
			}
		}
	}
	return declared
}

// gopiResolverHit is one hand-rolled resolution, located by the parser.
type gopiResolverHit struct {
	File string // module-relative
	Line int
	Kind string
	Text string // the offending snippet, trimmed
}

func (h gopiResolverHit) key() string { return h.File + "|" + h.Kind }

// gopiResolverPermit exempts ONE (file, kind) pair, with the reason it is not a
// resolver at all. Keyed on the pair, never on a line number: a permit that
// survives a file's edit must still be re-read, and a permit whose hit went
// away is itself an error (the table only ratchets down).
type gopiResolverPermit struct {
	File string
	Kind string
	Why  string
}

// gopiResolverPermits is the reviewed allow-list. Every entry is a site where
// the detector's pattern is present but NO gopi-under-test binary is being
// resolved — a production reader of the build contract, a synthetic fixture
// repo, or a path shape under test for its own sake.
//
// Every row below is the SAME kind of site: a token that the detector's
// spelling matches, feeding something that is NOT a resolution. They are keyed
// (file, kind), so a permit exempts the FILE's hits of that kind and nothing
// else.
//
// The taskreport pair is the report-PROSE family: it compares prose and can
// neither build, stat nor execute a binary (devdoc/report-result-delegation.md).
// The two halves are one subject: the production vocabulary the detector
// matches sentences against, and the table asserting that this exact token is
// NOT an artifact path. The token cannot be sourced (internal/testutil owns the
// resolver, and a PRODUCTION package must not import it for its testing deps,
// nor the reverse) and it cannot be assembled from those constants without
// becoming precisely the "spell the path yourself" shape this gate exists to
// refuse — so it stays a literal here, with a reason, inside the census's own
// STALE PERMIT check.
//
// The worker-status row is the ARGV-FIXTURE family: the literal is one element
// of a `[][]string` table handed to workerTaskFromArgv, a pure argv parser that
// reads argv[1] and nothing else. Its judgment is recorded in
// devdoc/gopi-binary-resolver-census.md (§ "The argv fixture vs the resolver").
//
// The deploy-drift row is the REFUSAL-FIXTURE family: the two joins are elements
// of a `[]string` of shapes the case asserts DeployDriftRepoOf REJECTS. Nothing
// is executed there and the paths are the opposite of the under-test binary.
// Its judgment is recorded in devdoc/gopi-binary-resolver-census.md
// (§ "The refusal fixture vs the resolver").
func gopiResolverPermits() []gopiResolverPermit {
	return []gopiResolverPermit{
		{
			File: "internal/taskreport/delegated.go",
			Kind: gopiResolverKindLiteral,
			Why: "delegatedInstruments is a report-PROSE vocabulary: the entry is matched by hasAny " +
				"against a sentence (delegated.go:362), and delegatedProse STRIPS every path token " +
				"before the result test judges that sentence. Nothing here is opened, stat-ed or " +
				"executed — the package imports only fmt/os/path/filepath/strconv/strings/unicode " +
				"and owns no binary resolution at all.",
		},
		{
			File: "internal/taskreport/delegated_test.go",
			Kind: gopiResolverKindLiteral,
			Why: "the token is the SUBJECT of TestDelegatedPathShapeRejectsNonArtifacts — the case " +
				"asserts this argv spelling is NOT a delegated artifact path (no extension in its " +
				"last segment). No gopi binary is resolved by the test; removing the token would " +
				"delete the shape the case exists to pin.",
		},
		{
			File: "cmd/agent/worker_status_task_test.go",
			Kind: gopiResolverKindLiteral,
			Why: "ARGV FIXTURE, not a resolver: the literal is one element of a [][]string table fed " +
				"to workerTaskFromArgv (worker_status_task.go:116), a pure parser that reads argv[1] " +
				"and argv[2:] and returns a struct — the file has no os.Stat, os.Open, exec.*, " +
				"Getenv or testutil.GopiBin (grep over it exits 1), and its imports are encoding/json, " +
				"os, path/filepath, strings, testing, time, procname. It is the PROCEDURAL name of a " +
				"loop worker whose argv carries no row; the case asserts that NONE of these argvs " +
				"yields a task id, so the spelling is the input under test and no binary is touched.",
		},
		{
			File: "internal/tools/deploy_drift_test.go",
			Kind: gopiResolverKindJoin,
			Why: "REFUSAL FIXTURE, not a resolver: both joins are elements of the []string of 'bad' " +
				"paths in TestDeployDriftRepoOf_ResolvesTheArtifactsOwnRepo, and the case asserts " +
				"DeployDriftRepoOf returns ok=false for every one of them. They are the two shapes " +
				"whose PARENT HOLDS NO CHECKOUT — /usr/local (measured on this host: `ls -d " +
				"/usr/local/.git` exits 2) and / — which is the exact arm the function's own comment " +
				"records (deploy_drift.go:238-244). Nothing executes them: the test file's only " +
				"exec.Command is `git` (line 70), it carries no os.Stat/os.Open/Getenv/GopiBin, and " +
				"DeployDriftRepoOf's only filesystem touch is os.Lstat(<parent>/.git), which fails " +
				"for both. Removing the literals would delete the installed-artifact shape the arm " +
				"exists to pin, and routing them through testutil.GopiBin would invert the case — it " +
				"would then assert the under-test binary is REFUSED.",
		},
	}
}

// gopiResolverHits parses ONE Go source and returns every hand-rolled
// resolution it contains. Parsing (not grepping) is the point: a comment or a
// doc string that merely DISCUSSES the pattern is not a hit, and a call whose
// literals are split across lines is.
func gopiResolverHits(file string, src []byte) []gopiResolverHit {
	hits, _ := gopiResolverScan(file, src)
	return hits
}

// gopiResolverScan is gopiResolverHits plus the literals the declaration-only
// exclusion let past. The second list is RETURNED rather than dropped so the
// census can PRINT it: an exclusion the output never mentions is the silent pass
// Rule 15 forbids, and a reader of `-v` must be able to see, per site, that a
// spelling was considered and why it is not a resolution.
func gopiResolverScan(file string, src []byte) (hits, excluded []gopiResolverHit) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return nil, nil // an unparseable file is not this gate's finding
	}
	declOnly := gopiResolverDeclarationOnly(f)
	declared := gopiResolverDeclaredValues(f)
	pos := func(n ast.Node) int { return fset.Position(n.Pos()).Line }
	add := func(n ast.Node, kind, text string) {
		hits = append(hits, gopiResolverHit{File: file, Line: pos(n), Kind: kind, Text: strings.TrimSpace(text)})
	}
	lit := func(e ast.Expr) string {
		b, ok := e.(*ast.BasicLit)
		if !ok || b.Kind != token.STRING {
			return ""
		}
		v, err := strconv.Unquote(b.Value)
		if err != nil {
			return ""
		}
		return v
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			// filepath.Join(…, "bin", "gopi")
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
				x, _ := sel.X.(*ast.Ident)
				if x != nil && x.Name == "filepath" && sel.Sel.Name == "Join" {
					rootish := false
					for _, a := range node.Args {
						if gopiResolverRootExpr(a) {
							rootish = true
							break
						}
					}
					tail := false
					for i := 0; i+1 < len(node.Args); i++ {
						if lit(node.Args[i]) == gopiBinDir && lit(node.Args[i+1]) == gopiBinBase {
							tail = true
							break
						}
					}
					if !tail {
						for _, a := range node.Args {
							if v := lit(a); v == gopiBinDir+"/"+gopiBinBase {
								tail = true
								break
							}
						}
					}
					if tail && rootish {
						add(node, gopiResolverKindJoin, srcLine(src, pos(node)))
					}
				}
				// os.Getenv/os.LookupEnv on a pinned-binary channel.
				if x != nil && x.Name == "os" && (sel.Sel.Name == "Getenv" || sel.Sel.Name == "LookupEnv") &&
					len(node.Args) > 0 {
					if v := lit(node.Args[0]); v == BinaryPinEnv || v == SuiteBinEnv {
						add(node, gopiResolverKindEnvRead, srcLine(src, pos(node)))
					}
				}
			}
			// exec.Command("gopi", …) — PATH resolution.
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
				x, _ := sel.X.(*ast.Ident)
				if x != nil && x.Name == "exec" && sel.Sel.Name == "Command" && len(node.Args) > 0 &&
					lit(node.Args[0]) == gopiBinBase {
					add(node, gopiResolverKindPathName, srcLine(src, pos(node)))
				}
			}
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				break
			}
			v, err := strconv.Unquote(node.Value)
			if err != nil {
				break
			}
			// A RELATIVE literal is the resolution shape; an absolute one
			// ("/usr/local/bin/gopi") is a synthetic or installed path in
			// this tree, and flagging that class would make the permits a
			// list of path literals rather than sites.
			if !strings.HasPrefix(v, "/") &&
				(v == gopiBinDir+"/"+gopiBinBase || strings.HasSuffix(v, gopiBinRelSuffix)) {
				if declOnly && declared[node] {
					// The DEFINITION site of a shared spelling (see
					// gopiResolverDeclarationOnly): a file with no import and no
					// function cannot resolve anything, so this literal is the
					// value other packages reference, not a path a process walks.
					// Reported to the caller, never silently dropped.
					excluded = append(excluded, gopiResolverHit{File: file, Line: pos(node), Kind: gopiResolverKindLiteral, Text: v})
					break
				}
				add(node, gopiResolverKindLiteral, v)
			}
		}
		return true
	})
	sort.Slice(hits, func(i, j int) bool { return hits[i].Line < hits[j].Line })
	return hits, excluded
}

// srcLine returns the raw source line at 1-based n (for the message only).
func srcLine(src []byte, n int) string {
	lines := strings.Split(string(src), "\n")
	if n-1 < 0 || n-1 >= len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[n-1])
}

// gopiResolverScanRoot walks the module's own Go sources (cmd/ + internal/) and
// returns every hit outside the gate's own file and the one resolver.
func gopiResolverScanRoot(t *testing.T, root string) ([]gopiResolverHit, int) {
	t.Helper()
	hits, _, scanned := gopiResolverScanRootWithExcluded(t, root)
	return hits, scanned
}

// gopiResolverScanRootWithExcluded is gopiResolverScanRoot plus the literals the
// declaration-only exclusion passed over, so the census can name them.
func gopiResolverScanRootWithExcluded(t *testing.T, root string) ([]gopiResolverHit, []gopiResolverHit, int) {
	t.Helper()
	var hits, excluded []gopiResolverHit
	scanned := 0
	for _, dir := range []string{"cmd", "internal"} {
		base := filepath.Join(root, dir)
		walkErr := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if filepath.Base(rel) == gopiResolverGateSelfFile ||
				(filepath.Dir(rel) == "internal/testutil" && filepath.Base(rel) == gopiResolverResolverFile) {
				return nil
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			scanned++
			fileHits, fileExcluded := gopiResolverScan(filepath.ToSlash(rel), src)
			hits = append(hits, fileHits...)
			excluded = append(excluded, fileExcluded...)
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", base, walkErr)
		}
	}
	return hits, excluded, scanned
}

// gopiResolverRedProofSrc is a fixture carrying EVERY shape the gate must
// catch. It is written as source text (not as real code in the tree) so the
// proof never becomes a violation itself, and it exercises the shapes in a
// file whose name is not the resolver's, i.e. through the same scan path the
// census test uses.
const gopiResolverRedProofSrc = `package fixture

import (
	"os"
	"os/exec"
	"path/filepath"
)

type fataliser interface{ Fatalf(string, ...any) }

// handRolledRel is the measured 2026-10-03 shape: the relative walk.
func handRolledRel(t fataliser) string {
	bin := filepath.Join("..", "..", "bin", "gopi")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("bin/gopi not built — run the live verification separately")
	}
	return bin
}

// handRolledLiteral spells the path as one string.
func handRolledLiteral() string {
	return "../../bin/gopi"
}

// handRolledEnv reads the pin channel itself instead of asking GopiBin.
func handRolledEnv() string {
	return os.Getenv("GOPI_BIN")
}

// handRolledPath lets PATH decide which binary runs.
func handRolledPath() *exec.Cmd {
	return exec.Command("gopi", "task", "list")
}
`

// gopiResolverRedProofCleanSrc is the NEGATIVE arm: a synthetic repository's
// own bin/gopi (a fixture base), an installed absolute path, and a doc comment
// that merely DISCUSSES the banned spelling. None is a hit — the gate must be
// narrow enough that its permit table can stay empty.
const gopiResolverRedProofCleanSrc = `package fixture

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixture repo's own binary is created by the build contract under test:
// the pre-fix helper was filepath.Join("..", "..", "bin", "gopi").
func fixtureRepoBin(t *testing.T, repo string) {
	_ = filepath.Join(repo, "bin", "gopi")
	_ = "/usr/local/bin/gopi"
	_ = os.Stat("/opt/bin/gopi")
}
`

// TestGopiBinaryResolverGateRedProof is the gate's own red/green proof: the
// fixture is RED (every hand-rolled shape is found, by kind), and the
// synthetic-repo/installed-path/comment shapes are NOT reported. A detector
// that silently stopped matching would fail the first half (`stale-binary
// family regressions` would sail through the census) and one that matched too
// broadly would fail the second (the permits would have to grow rows that lie
// about being resolvers).
func TestGopiBinaryResolverGateRedProof(t *testing.T) {
	got := map[string]int{}
	for _, h := range gopiResolverHits("fixture/handrolled_test.go", []byte(gopiResolverRedProofSrc)) {
		got[h.Kind]++
	}
	for _, want := range []string{
		gopiResolverKindJoin,
		gopiResolverKindLiteral,
		gopiResolverKindEnvRead,
		gopiResolverKindPathName,
	} {
		if got[want] == 0 {
			t.Errorf("the fixture carries a %s resolver and the gate did not report it — a new hand-rolled "+
				"resolver would ship unnoticed (found: %v)", want, got)
		}
	}
	if hit := gopiResolverHits("fixture/clean_test.go", []byte(gopiResolverRedProofCleanSrc)); len(hit) != 0 {
		t.Errorf("the gate reported %d hit(s) on the clean fixture %v — a fixture repo's own bin/gopi, an "+
			"installed absolute path and a doc comment are not resolutions, and flagging them would fill the "+
			"permit table with sites that are not resolvers", len(hit), hit)
	}
}

// gopiResolverDeclOnlyFixtureSrc is the shape the declaration-only exclusion
// exists FOR (measured: internal/mansource/paths.go): a leaf package of shared
// spellings, no import and no function, so the literal is a DEFINED VALUE and
// nothing in the file can stat, open or execute it.
const gopiResolverDeclOnlyFixtureSrc = `package fixture

// builtBinaryRel is the declared build artifact's repo-relative path.
const builtBinaryRel = "bin/gopi"
`

// gopiResolverDeclOnlyResolvingSrc is the SAME spelling in a file that can act:
// one import (`os`) and one function that stats the path. The exclusion requires
// BOTH halves, so this one is a hit exactly as before — the declaration-only
// exclusion did not widen the gate's blind spot by one file.
const gopiResolverDeclOnlyResolvingSrc = `package fixture

import "os"

type fataliser interface{ Fatalf(string, ...any) }

func handRolled(t fataliser) string {
	const bin = "bin/gopi"
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("not built")
	}
	return bin
}
`

// gopiResolverDeclOnlyCollectionSrc is a declaration-only file whose literal is
// NOT the value of a const/var spec but an ELEMENT of a composite literal. That
// is data a function elsewhere could iterate and execute, so the exclusion must
// not cover it: the criterion is the definition site, not the file's shape.
const gopiResolverDeclOnlyCollectionSrc = `package fixture

var handRolledPaths = []string{"bin/gopi"}
`

// TestGopiBinaryResolverDeclarationOnlyFileIsNotAResolution pins the ONE
// exclusion the census makes (§ gopiResolverDeclarationOnly), in both
// directions: a leaf of shared spellings is passed over (and REPORTED as
// passed over, so the output is auditable), while every site that could act is
// still a hit. Without the red arms the first arm would be a licence to exclude
// anything a future author puts in a const block.
func TestGopiBinaryResolverDeclarationOnlyFileIsNotAResolution(t *testing.T) {
	// ARM 1: the leaf's DEFINED VALUE is excluded, and named as excluded.
	hits, excluded := gopiResolverScan("fixture/paths.go", []byte(gopiResolverDeclOnlyFixtureSrc))
	if len(hits) != 0 {
		t.Fatalf("a declaration-only leaf's constant was reported as a resolution (%v) — the "+
			"census cannot go green on internal/mansource/paths.go's shape", hits)
	}
	if len(excluded) != 1 || excluded[0].Line != 4 || excluded[0].Kind != gopiResolverKindLiteral {
		t.Fatalf("the exclusion must NAME the literal it passed over (line 4, kind %s), got %v — "+
			"a silent exclusion is indistinguishable from a clean tree (Rule 15)", gopiResolverKindLiteral, excluded)
	}

	// ARM 2: the same spelling in a file that can act is still a hit. This is
	// the arm that keeps the exclusion from becoming a blind spot.
	hits, excluded = gopiResolverScan("fixture/handrolled_test.go", []byte(gopiResolverDeclOnlyResolvingSrc))
	if len(excluded) != 0 {
		t.Errorf("a file with an import and a function must exclude NOTHING, got %v", excluded)
	}
	if len(hits) != 1 || hits[0].Kind != gopiResolverKindLiteral {
		t.Fatalf("the exclusion let a real resolver through: a file with `os.Stat(%q)` must still be "+
			"one %s hit (got %v)", "bin/gopi", gopiResolverKindLiteral, hits)
	}

	// ARM 3: a declaration-only file whose literal is NOT a spec value — an
	// element of a []string a function elsewhere may execute — is a hit.
	hits, excluded = gopiResolverScan("fixture/table.go", []byte(gopiResolverDeclOnlyCollectionSrc))
	if len(excluded) != 0 {
		t.Errorf("only a const/var spec VALUE is excluded; a composite-literal element must be "+
			"reported, got excluded=%v", excluded)
	}
	if len(hits) != 1 {
		t.Fatalf("a hand-rolled path table in a declaration-only file must still be a %s hit "+
			"(it is data a caller can execute), got %v", gopiResolverKindLiteral, hits)
	}
}

// gopiResolverVerdict is the census's ONE decision, factored out of the test
// body so the ratchet ITSELF can be red-proved: which hits are unpermitted,
// and which permits went stale. A permit is keyed on the (file, kind) PAIR —
// never on a line, never on a file alone and never on a kind alone — so it
// exempts one site, and a permit whose hit has disappeared (cleaned, moved,
// changed shape) is reported rather than left as a silent pass. The table
// therefore only ratchets DOWN, and an empty verdict means exactly what the
// census says it means.
func gopiResolverVerdict(hits []gopiResolverHit, permits []gopiResolverPermit) []string {
	permitted := map[string]gopiResolverPermit{}
	for _, p := range permits {
		permitted[p.File+"|"+p.Kind] = p
	}
	used := map[string]bool{}
	var bad []string
	for _, h := range hits {
		if _, ok := permitted[h.key()]; ok {
			used[h.key()] = true
			continue
		}
		bad = append(bad, h.File+":"+strconv.Itoa(h.Line)+" ["+h.Kind+"] "+h.Text)
	}
	for k, p := range permitted {
		if !used[k] {
			bad = append(bad, "STALE PERMIT "+p.File+" ["+p.Kind+"]: "+p.Why)
		}
	}
	sort.Strings(bad)
	return bad
}

// gopiResolverPermitReasonFloor is the shortest reason this gate accepts on a
// permit row. A permit is an argument, not a shrug: the ratchet is safe only
// while every surviving row states WHY the site is not a resolver, so a row
// carrying "ok" (or nothing) is refused here rather than reviewed later.
const gopiResolverPermitReasonFloor = 60

// TestGopiBinaryResolverPermitsAreNarrowAndRatcheted is the permit table's own
// proof, and it exists because a permit is the one place this gate can be
// quietened — a table that exempted everything, or a row that outlived its
// site, would look exactly like a green census. Three properties:
//
//  1. RATCHET — with no hits at all, every row is reported STALE PERMIT, so a
//     permit cannot survive the site it was written for.
//  2. FILE-NARROW — the same literal in another file is NOT exempt.
//  3. KIND-NARROW — another shape in the SAME file is NOT exempt.
//
// It also floors the reason length, so a row can never be added as a shrug.
func TestGopiBinaryResolverPermitsAreNarrowAndRatcheted(t *testing.T) {
	permits := gopiResolverPermits()
	if len(permits) == 0 {
		t.Skip("no permits in the table — the ratchet arms below would be vacuous")
	}
	for _, p := range permits {
		if len(p.Why) < gopiResolverPermitReasonFloor {
			t.Errorf("permit %s [%s] carries a %d-char reason — a permit is an argument a later reader "+
				"must be able to re-check, not a shrug", p.File, p.Kind, len(p.Why))
		}
		if p.Kind == "" || p.File == "" {
			t.Errorf("permit %+v is not keyed on a (file, kind) pair — an unkeyed row exempts nothing "+
				"and ratchets nothing", p)
		}
	}
	// 1. RATCHET: the site is gone → the permit must be reported.
	for _, p := range permits {
		bad := gopiResolverVerdict(nil, []gopiResolverPermit{p})
		if len(bad) != 1 || !strings.HasPrefix(bad[0], "STALE PERMIT "+p.File) {
			t.Errorf("a permit whose hit disappeared must be reported as STALE PERMIT %s [%s], got %v",
				p.File, p.Kind, bad)
		}
	}
	// 2+3. NARROWNESS: the pair is the key — neither half alone exempts. The
	// arms are read off the REPORTED SET, not off its length, because a real
	// table reports stale rows alongside unpermitted ones and a count would
	// then be satisfied by the wrong entry.
	own := make([]gopiResolverHit, 0, len(permits))
	for _, p := range permits {
		own = append(own, gopiResolverHit{File: p.File, Kind: p.Kind, Text: "./bin/gopi"})
	}
	if got := gopiResolverVerdict(own, permits); len(got) != 0 {
		t.Fatalf("the table does not exempt its own sites, so the arms below would prove nothing: %v", got)
	}
	for _, p := range permits {
		foreignFile := gopiResolverHit{File: "internal/other/elsewhere.go", Kind: p.Kind, Text: "./bin/gopi"}
		if got := gopiResolverVerdict(append(append([]gopiResolverHit{}, own...), foreignFile), permits); !reports(got, foreignFile) {
			t.Errorf("permit %s [%s] let the same shape through in ANOTHER file — the key must be the "+
				"(file, kind) pair (reported: %v)", p.File, p.Kind, got)
		}
		// The sibling kind must be DIFFERENT from the row's own kind, or the arm
		// is vacuous: it would re-add the permit's own key and prove nothing.
		// (Measured 2026-10-29: with the join-bin-gopi row added, a hard-coded
		// join sibling exempted itself and this arm reported `reported: []`.)
		otherKind := gopiResolverOtherKind(p.Kind)
		foreignKind := gopiResolverHit{
			File: p.File,
			Kind: otherKind,
			Text: `filepath.Join("..", "..", "bin", "gopi")`,
		}
		if got := gopiResolverVerdict(append(append([]gopiResolverHit{}, own...), foreignKind), permits); !reports(got, foreignKind) {
			t.Errorf("permit %s [%s] let ANOTHER shape [%s] through in the same file — a permit on one "+
				"shape must not cover a real resolver of another (reported: %v)", p.File, p.Kind, otherKind, got)
		}
	}
}

// gopiResolverOtherKind returns a kind from the detector's own vocabulary that
// is NOT k, so the KIND-NARROW arm above always names a different shape than
// the permit's key. k is always one of the four constants, so the loop returns
// on the first iteration unless k is the last constant.
func gopiResolverOtherKind(k string) string {
	for _, other := range []string{
		gopiResolverKindLiteral,
		gopiResolverKindJoin,
		gopiResolverKindEnvRead,
		gopiResolverKindPathName,
	} {
		if other != k {
			return other
		}
	}
	return ""
}

// reports says whether the verdict names this hit as unpermitted (the verdict's
// own line shape, `file:line [kind] text`).
func reports(bad []string, h gopiResolverHit) bool {
	prefix := h.File + ":" + strconv.Itoa(h.Line) + " [" + h.Kind + "] "
	for _, b := range bad {
		if strings.HasPrefix(b, prefix) {
			return true
		}
	}
	return false
}

// TestGopiBinaryResolverCensus is the CENSUS: it prints every hand-rolled
// resolution in the tree and fails when one is neither permitted nor excluded.
// A run of this test IS the generator of the census table (Rule 38: the figure
// names its generator, and the generator is `go test ./internal/testutil/
// -run TestGopiBinaryResolverCensus -v`).
func TestGopiBinaryResolverCensus(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	hits, excluded, scanned := gopiResolverScanRootWithExcluded(t, root)
	if scanned < gopiResolverScanFloor {
		t.Fatalf("the scan read only %d Go files under cmd/+internal/ (floor %d) — a scan that never ran "+
			"must not pass as a scan that found nothing", scanned, gopiResolverScanFloor)
	}
	for _, h := range excluded {
		t.Logf("%-10s %s:%d [%s] %s — %s", "excluded", h.File, h.Line, h.Kind, h.Text, gopiResolverDeclOnlyWhy)
	}
	permitted := map[string]gopiResolverPermit{}
	for _, p := range gopiResolverPermits() {
		permitted[p.File+"|"+p.Kind] = p
	}
	for _, h := range hits {
		state := "UNPERMITTED"
		if _, ok := permitted[h.key()]; ok {
			state = "permitted"
		}
		t.Logf("%-10s %s:%d [%s] %s", state, h.File, h.Line, h.Kind, h.Text)
	}
	bad := gopiResolverVerdict(hits, gopiResolverPermits())
	t.Logf("census: %d hit(s) over %d Go files, %d permitted", len(hits), scanned, len(permitted))
	if len(bad) > 0 {
		t.Fatalf("hand-rolled gopi-binary resolution — every test that drives a gopi binary must ask "+
			"testutil.GopiBin (internal/testutil/run.go), which resolves the suite build once for the whole "+
			"run; a site that spells the path itself executes whichever tree's build it happens to find "+
			"(Rule 6a shared checkout, devdoc/tested-binary-pin.md):\n  %s", strings.Join(bad, "\n  "))
	}
}
