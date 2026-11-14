package standardgate

// The heavy-test classification is only a classification if it is ENUMERATED
// and kept in step with the code. This gate is what makes it reviewable:
//
//   - testdata/heavy-tests.tsv is the checked-in inventory (one row per heavy
//     call site: test name, criterion, source file);
//   - every `heavytest.Require(t, …)` call site in the repo must appear in it,
//     and every row must have a call site — so the list can neither drift
//     silently nor accumulate stale rows (the same bidirectional discipline the
//     Rule 16-21 baselines use);
//   - the criterion argument must be a heavytest CONSTANT (Rule 1b: no bare
//     literal reasons);
//   - the gate packages (internal/standards, internal/stylegate) must NOT be
//     classified heavy: they have to run on the smoke path, or a merge could
//     land with its own gates unrun (that would be weakening a gate to make
//     this land);
//   - the opt-in is ONE name (heavytest.EnvName) and the smoke/merge path is
//     the default: `make test` must not set it, and `make test-heavy` must.
//
// Why not a `go test -tags` classification: a build tag changes which files are
// COMPILED, so a heavy test in a tagged file is invisible to `go vet ./...` and
// to every static gate until the tag is set — the repo's own gate-integrity
// rule (a gate that cannot see a file reports PASS on it). An in-body
// heavytest.Require keeps every test compiled and type-checked on every run,
// while skipping the expensive part.

import (
	"bufio"
	"go/ast"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// heavyInventoryRel is the reviewed inventory (Rule 14: one definition,
	// shared with internal/heavytest which documents it for users).
	heavyInventoryRel = "testdata/heavy-tests.tsv"
	// heavyRequireFunc is the call that declares a test heavy.
	heavyRequireFunc = "heavytest.Require"
	// heavyPackagesImportsPath is the helper package; its constant prefix marks
	// a criterion argument.
	heavyPackagesImportsPath = "github.com/nickhuang99/gopi/internal/heavytest"
	// heavyEnvConstName is the ONE opt-in name. A bare "GOPI_HEAVY_TESTS"
	// outside its definition is the Rule 13 defect this gate reports.
	heavyEnvConstName = "GOPI_HEAVY_TESTS"
	// heavyEnvDefinitionFile is the only file allowed to spell the literal.
	heavyEnvDefinitionFile = "internal/heavytest/heavy.go"
	// heavyMinInventoryRows is the non-vacuity floor: a classification that
	// finds nothing is not a classification, and a broken glob would otherwise
	// report a clean green.
	heavyMinInventoryRows = 20
	// heavyMakeSmokeTarget is the default (merge/verify) target: it must NOT
	// enable heavy tests.
	heavyMakeSmokeTarget = "test:"
	// heavyMakeHeavyTarget is the named opt-in target.
	heavyMakeHeavyTarget = "test-heavy:"
	// heavyMakeOptInAssignment is how the heavy target turns the switch on.
	heavyMakeOptInAssignment = "GOPI_HEAVY_TESTS=1"
	// heavySmokeOnlyGatePackages keep the gates on the smoke path.
	heavySmokeOnlyGatePackages = "internal/standards|internal/stylegate"
)

// heavyConstantPrefix marks a criterion argument that is a constant rather than
// a bare string literal.
const heavyConstantPrefix = "heavytest.Criterion"

// heavyCallSite is one `heavytest.Require(t, criterion)` occurrence.
type heavyCallSite struct {
	test      string // enclosing test function
	criterion string // the argument source text
	rel       string // repo-relative slash path
	line      int
}

// heavyInventoryRow is one parsed row of the inventory.
type heavyInventoryRow struct {
	test      string
	criterion string
	source    string
}

// TestHeavyTestInventoryMatchesItsCallSites is the whole gate.
func TestHeavyTestInventoryMatchesItsCallSites(t *testing.T) {
	root := repoRoot(t)
	sites := heavyCallSites(t, root)
	rows := heavyInventoryRows(t, root)

	if len(rows) < heavyMinInventoryRows {
		t.Fatalf("%s carries %d row(s) (want at least %d) — the classification is "+
			"empty or the reader is broken, so this gate would pass vacuously",
			heavyInventoryRel, len(rows), heavyMinInventoryRows)
	}
	if len(sites) < heavyMinInventoryRows {
		t.Fatalf("the scan found %d `%s(` call site(s) (want at least %d) — a broken "+
			"scan must fail, never read as 'nothing to report'",
			len(sites), heavyRequireFunc, heavyMinInventoryRows)
	}

	byKey := func(test, rel string) string { return test + "\t" + rel }
	inInventory := make(map[string]heavyInventoryRow, len(rows))
	for _, r := range rows {
		inInventory[byKey(r.test, r.source)] = r
	}
	atSite := make(map[string]heavyCallSite, len(sites))
	for _, s := range sites {
		atSite[byKey(s.test, s.rel)] = s
	}

	// A call site with no row: the classification missed it (the drift that
	// makes an inventory worthless).
	var missing []string
	for k, s := range atSite {
		if _, ok := inInventory[k]; !ok {
			missing = append(missing, s.rel+":"+itoa(s.line)+" "+s.test+" ("+s.criterion+")")
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("heavy call site not in %s: %s — add the row (test, criterion, source) "+
			"so the skip is reviewable; an unlisted heavy test is a silent skip, which this "+
			"repo counts as a defect", heavyInventoryRel, m)
	}
	// A row with no call site: a stale classification (a deleted or renamed
	// test still claiming to be heavy).
	var stale []string
	for k, r := range inInventory {
		if _, ok := atSite[k]; !ok {
			stale = append(stale, r.source+" "+r.test+" ("+r.criterion+")")
		}
	}
	sort.Strings(stale)
	for _, s := range stale {
		t.Errorf("stale row in %s: %s — the test was renamed, moved or deleted; delete the "+
			"row so the inventory keeps ratcheting with the code", heavyInventoryRel, s)
	}

	// Criterion must be a constant, never a bare literal (Rule 1b).
	for _, s := range sites {
		if !strings.HasPrefix(s.criterion, heavyConstantPrefix) {
			t.Errorf("%s:%d passes %q as the heavy criterion — use a %s* constant, so the "+
				"reason has one source and the inventory cannot drift from the code",
				s.rel, s.line, s.criterion, heavyConstantPrefix)
		}
	}

	// The gate packages stay on the smoke path: a heavy gate is an unrun gate.
	heavyRe := regexp.MustCompile(heavySmokeOnlyGatePackages)
	for _, s := range sites {
		if heavyRe.MatchString(filepath.Dir(s.rel)) {
			t.Errorf("%s:%d classifies a test in a GATE package as heavy — %s must keep "+
				"running on the smoke/merge path, or a merge can land with its own gates "+
				"skipped (that would be weakening a gate to make the classification land)",
				s.rel, s.line, filepath.Dir(s.rel))
		}
	}

	heavyAssertOneOptInName(t, root)
	heavyAssertMakeTargetsSplit(t, root)
}

// heavyCallSites parses every *_test.go in the repo and returns each
// heavytest.Require call with its enclosing test name.
func heavyCallSites(t *testing.T, root string) []heavyCallSite {
	t.Helper()
	var out []heavyCallSite
	for _, rel := range goFilesUnder(t, root, []string{"."}, true) {
		if strings.Contains(rel, "/") && strings.HasPrefix(rel, ".") {
			continue // dot-directories are not this repo's test sources
		}
		p := parseRepoFile(t, root, rel)
		for _, decl := range p.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if heavyCallName(call) != heavyRequireFunc {
					return true
				}
				pos := p.fset.Position(call.Pos())
				criterion := ""
				if len(call.Args) >= 2 {
					criterion = heavyExprText(call.Args[1])
				}
				out = append(out, heavyCallSite{test: fn.Name.Name, criterion: criterion, rel: rel, line: pos.Line})
				return true
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].rel != out[j].rel {
			return out[i].rel < out[j].rel
		}
		return out[i].line < out[j].line
	})
	return out
}

// heavyCallName renders a call's callee as `pkg.Func` (or just `Func`), so the
// gate matches the package-qualified form the repo uses without depending on
// how the import is spelled.
func heavyCallName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			return id.Name + "." + f.Sel.Name
		}
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	}
	return ""
}

// heavyExprText renders an argument's source shape: an identifier path
// (heavytest.CriterionDiskFixture) or a literal, enough to check "constant, not
// literal" without a full printer.
func heavyExprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			return id.Name + "." + v.Sel.Name
		}
		return v.Sel.Name
	case *ast.Ident:
		return v.Name
	case *ast.BasicLit:
		return "LITERAL:" + v.Value
	}
	return ""
}

// heavyInventoryRows reads the TSV, ignoring the comment header.
func heavyInventoryRows(t *testing.T, root string) []heavyInventoryRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, heavyInventoryRel))
	if err != nil {
		t.Fatalf("read %s: %v — the heavy-test inventory is gone, so nothing enumerates "+
			"what the smoke run skips (a skip nobody can list is a deleted test wearing a green)",
			heavyInventoryRel, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Errorf("close %s: %v", heavyInventoryRel, cerr)
		}
	}()
	var rows []heavyInventoryRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			t.Errorf("%s: malformed row %q — expected `test<TAB>criterion<TAB>source`", heavyInventoryRel, line)
			continue
		}
		rows = append(rows, heavyInventoryRow{test: parts[0], criterion: parts[1], source: parts[2]})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", heavyInventoryRel, err)
	}
	return rows
}

// heavyAssertOneOptInName pins Rule 13 for the opt-in: the literal may live
// only at its definition, and every other reader must go through the constant.
func heavyAssertOneOptInName(t *testing.T, root string) {
	t.Helper()
	for _, rel := range goFilesUnder(t, root, []string{"internal", "cmd", "testsuite"}, false) {
		if rel == heavyEnvDefinitionFile {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if strings.Contains(string(src), `"`+heavyEnvConstName+`"`) {
			t.Errorf("%s spells the %q literal — reference the constant (heavytest.EnvName, "+
				"defined in %s) so the switch has one name and one reader",
				rel, heavyEnvConstName, heavyEnvDefinitionFile)
		}
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(heavyEnvDefinitionFile))); err != nil {
		t.Errorf("%s is gone — the opt-in's single definition must exist: %v",
			heavyEnvDefinitionFile, err)
	}
}

// heavyAssertMakeTargetsSplit pins the SMOKE-ONLY merge path: the default
// target must not enable heavy tests, and the opt-in must be a named target.
func heavyAssertMakeTargetsSplit(t *testing.T, root string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	sections := map[string]string{}
	var current string
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "\t") {
			current = line
			continue
		}
		if current != "" && strings.HasPrefix(line, "\t") {
			sections[current] += line + "\n"
		}
	}
	smoke, ok := sections[heavyMakeSmokeTarget]
	if !ok {
		t.Fatalf("the Makefile has no %q target — the merge/verify entry point must exist "+
			"and stay smoke-only", heavyMakeSmokeTarget)
	}
	if strings.Contains(smoke, heavyMakeOptInAssignment) {
		t.Errorf("the %q target sets %s — the default (merge/verify) path MUST be smoke-only; "+
			"heavy runs belong to the opt-in target", heavyMakeSmokeTarget, heavyMakeOptInAssignment)
	}
	heavy, ok := sections[heavyMakeHeavyTarget]
	if !ok {
		t.Fatalf("the Makefile has no %q target — a default-off classification needs ONE named "+
			"way back in, or the heavy tests are deleted in all but name", heavyMakeHeavyTarget)
	}
	if !strings.Contains(heavy, heavyMakeOptInAssignment) {
		t.Errorf("the %q target does not set %s — the named opt-in must actually enable the "+
			"heavy set", heavyMakeHeavyTarget, heavyMakeOptInAssignment)
	}
}

// itoa avoids importing strconv for one call in a gate that already has a
// smaller vocabulary than the production files it polices.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
