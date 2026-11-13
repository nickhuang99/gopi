// gate 1's format-string resolution — Rule 16 must see a NAMED CONSTANT too.
//
// WHY THIS FILE EXISTS. The Rule-16 predicate used to accept only a string
// LITERAL as fmt.Errorf's first argument:
//
//	lit, ok := call.Args[0].(*ast.BasicLit)
//	if !ok || lit.Kind != token.STRING { return true }
//
// A named constant is an *ast.Ident, so `fmt.Errorf(someFormat, err)` returned
// early: never reported, never counted, never listed as undecidable. That is
// PROJECT.md Rule 15's silent failure living INSIDE the gate — and it made this
// gate disagree with internal/stylegate's errorf-verb rule, which resolves the
// constant through go/types (measured 2026-10-22: 8 production hits there, 3
// here, and all 5 of the difference were constant format strings).
//
// WHAT IT DOES NOW. The format expression is resolved to its compile-time text
// through the AST alone — no go/types dependency (zero-dep, Rule 35):
//
//   - a string literal, a parenthesized expression, or a `+` concatenation;
//   - an identifier naming a const IN THE SAME FILE (probe sources live in
//     memory and are in no directory index, so the file is consulted first);
//   - an identifier naming a const in the SAME PACKAGE (every non-test .go file
//     of the file's directory, indexed once per directory);
//   - `pkg.Name` / `alias.Name` where the selector's X is an imported package
//     whose const holds a string literal (the `memorySyncTickCwdWhy…` shape).
//
// WHAT IT DOES NOT DO, AND SAYS SO. A format string that is a variable, a
// function call, a struct field or a chain of consts deeper than
// maxFormatResolveDepth cannot be known statically. Those sites are RECORDED by
// noteUndecidableFormat (surfaced by TestStandardGateDataReport and drained by
// the probe test) instead of being dropped — the visibility stylegate already
// has. The gate stays GREEN on them because a value the analyser cannot see is
// not a violation it may assert.
package standardgate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	// maxFormatResolveDepth bounds a const-reference chain. A cyclic or absurdly
	// deep chain is a code defect, not a reason to hang the gate; hitting the
	// bound makes the site UNDECIDABLE (recorded), never silently clean.
	maxFormatResolveDepth = 4
	// goModPathPrefix / goModPathField locate the module path this package tree
	// belongs to, so an import path can be turned into a directory. The scan is
	// repo-local by construction: an import outside the module is not resolved.
	goModPathPrefix = "module "
	goModFileName   = "go.mod"
)

// formatText resolves x to the compile-time string it holds, or reports that
// the value is not statically knowable.
func formatText(p *parsed, x ast.Expr, depth int) (string, bool) {
	if depth > maxFormatResolveDepth {
		return "", false
	}
	switch v := x.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.ParenExpr:
		return formatText(p, v.X, depth)
	case *ast.BinaryExpr:
		// `"a" + constB` is how a long format string is usually spelled. The
		// depth does NOT advance here: a concatenation is a finite tree by
		// construction (it ends at leaves), so it cannot cycle — only a NAME
		// hop can, and those are counted below. Counting it here would make
		// every long refusal message (a dozen `+` parts is ordinary) resolve as
		// undecidable, which is a false negative dressed as diligence.
		if v.Op != token.ADD {
			return "", false
		}
		left, lok := formatText(p, v.X, depth)
		right, rok := formatText(p, v.Y, depth)
		if !lok || !rok {
			return "", false
		}
		return left + right, true
	case *ast.Ident:
		decl, ok := packageConstDecl(p, v.Name)
		if !ok {
			return "", false
		}
		return formatText(decl.p, decl.expr, depth+1)
	case *ast.SelectorExpr:
		pkg, ok := v.X.(*ast.Ident)
		if !ok {
			// `a.b.c` — a field of a value, never a package qualifier.
			return "", false
		}
		decl, ok := importedConstDecl(p, pkg.Name, v.Sel.Name)
		if !ok {
			return "", false
		}
		return formatText(decl.p, decl.expr, depth+1)
	}
	return "", false
}

// constDecl is one compile-time constant reduced to its value expression and
// the file that declares it (so a nested reference resolves in ITS package).
type constDecl struct {
	expr ast.Expr
	p    *parsed
}

// packageConstDecl finds a package-level (or file-level) const named name for
// the parsed file p. The file itself is searched FIRST: a probe's source is
// in-memory and appears in no directory index, and a same-file const is the
// commonest shape in real code too.
func packageConstDecl(p *parsed, name string) (constDecl, bool) {
	if d, ok := constInFile(p, name); ok {
		return d, true
	}
	dir := pkgDirOf(p.rel)
	if dir == "" {
		return constDecl{}, false
	}
	for _, d := range packageConstIndex(dir) {
		if d.name == name {
			return d.decl, true
		}
	}
	return constDecl{}, false
}

// importedConstDecl resolves `pkg.Name` where pkg is an imported package of p.
func importedConstDecl(p *parsed, pkgName, name string) (constDecl, bool) {
	dir := importDirOf(p, pkgName)
	if dir == "" {
		return constDecl{}, false
	}
	for _, d := range packageConstIndex(dir) {
		if d.name == name {
			return d.decl, true
		}
	}
	return constDecl{}, false
}

// constInFile lists the consts of ONE already-parsed file. A spec with several
// names or several values is skipped: no such spec is one name mapped to one
// value (the same rule constSpecs states for env names).
func constInFile(p *parsed, name string) (constDecl, bool) {
	if p == nil || p.file == nil {
		return constDecl{}, false
	}
	for _, decl := range p.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			if vs.Names[0].Name == name {
				return constDecl{expr: vs.Values[0], p: p}, true
			}
		}
	}
	return constDecl{}, false
}

// namedConst is one entry of a directory's const index.
type namedConst struct {
	name string
	decl constDecl
}

// constIndexCache holds, per directory, that directory's package-level consts.
// Parsing is the expensive part, so it happens once per directory per run.
var constIndexCache sync.Map // map[string][]namedConst

// packageConstIndex parses every non-test Go file in dir once and returns its
// consts. A directory that cannot be read yields an empty index, which makes
// the referencing site UNDECIDABLE (recorded) rather than silently clean.
func packageConstIndex(dir string) []namedConst {
	if cached, ok := constIndexCache.Load(dir); ok {
		out, _ := cached.([]namedConst)
		return out
	}
	var out []namedConst
	entries, err := os.ReadDir(dir) //stylegate:ignore the error IS handled: an unreadable directory yields an empty index, which makes the site undecidable
	if err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, goSourceSuffix) || strings.HasSuffix(n, "_test.go") {
				continue
			}
			names = append(names, n)
		}
		sort.Strings(names) // deterministic: the FIRST declaration of a name wins
		for _, n := range names {
			full := filepath.Join(dir, n)
			src, rerr := os.ReadFile(full)
			if rerr != nil {
				continue
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, full, src, parser.ParseComments)
			if perr != nil {
				continue
			}
			p := &parsed{rel: filepath.ToSlash(full), fset: fset, file: f, src: src}
			for _, decl := range f.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
						continue
					}
					out = append(out, namedConst{name: vs.Names[0].Name,
						decl: constDecl{expr: vs.Values[0], p: p}})
				}
			}
		}
	}
	constIndexCache.Store(dir, out)
	return out
}

// pkgDirOf maps a repo-relative file to the absolute directory holding its
// package ("" when the module root cannot be located, or when the rel path
// names no package directory — a probe's synthetic "probe.go" is not a file on
// disk, and resolving it would query the module root instead of a package).
func pkgDirOf(rel string) string {
	dir := pathDir(rel)
	if dir == "" {
		return ""
	}
	root := repoRootForScan()
	if root == "" {
		return ""
	}
	return filepath.Join(root, filepath.FromSlash(dir))
}

// pathDir is filepath.Dir for a slash-separated repo-relative path: "." when
// the path names no directory, which pkgDirOf treats as "no package here".
func pathDir(rel string) string {
	i := strings.LastIndex(rel, "/")
	if i <= 0 {
		return ""
	}
	return rel[:i]
}

// importDirOf maps p's imported package NAME to the absolute directory holding
// it, repo-local only. An alias is honoured; an import whose path segment is not
// the package name (or one outside this module) resolves to "" and the site is
// recorded as undecidable — the honest answer, not a guess.
func importDirOf(p *parsed, pkgName string) string {
	if p == nil || p.file == nil || pkgName == "" {
		return ""
	}
	root := repoRootForScan()
	mod := modulePath()
	if root == "" || mod == "" {
		return ""
	}
	for _, imp := range p.file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := importBaseName(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name != pkgName {
			continue
		}
		if !strings.HasPrefix(path, mod+"/") {
			return "" // outside this module: not this gate's tree
		}
		dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, mod+"/")))
		if st, serr := os.Stat(dir); serr != nil || !st.IsDir() {
			return ""
		}
		return dir
	}
	return ""
}

// importBaseName is the conventional package name of an import path: its last
// segment. A path whose last segment is a major-version suffix (`v2`) or a
// `-`-suffixed directory needs the import alias, which importDirOf honours.
func importBaseName(path string) string {
	base := path
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return base
}

var (
	modulePathOnce sync.Once
	modulePathVal  string
)

// modulePath reads this repo's module path from go.mod (one read per run).
func modulePath() string {
	modulePathOnce.Do(func() {
		root := repoRootForScan()
		if root == "" {
			return
		}
		b, err := os.ReadFile(filepath.Join(root, goModFileName)) //stylegate:ignore an unreadable go.mod leaves the module path empty, which makes cross-package sites undecidable rather than clean
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, goModPathPrefix) {
				modulePathVal = strings.TrimSpace(strings.TrimPrefix(line, goModPathPrefix))
				return
			}
		}
	})
	return modulePathVal
}

// ---------------------------------------------------------------------------
// the undecidable channel — Rule 15 inside the gate
// ---------------------------------------------------------------------------

// undecidableSite is one fmt.Errorf call whose format string could not be
// resolved statically. It is keyed by its identity so a repeated scan (the gate
// runs once per test) does not multiply the same site.
type undecidableSite struct {
	rel  string
	line int
	expr string
}

var (
	undecidableMu    sync.Mutex
	undecidableSeen  = map[undecidableSite]bool{}
	undecidableDrain []string
	// undecidableCount is the running tally since the last Forget. It exists
	// because the drain list is DESTRUCTIVE: a consumer that wants a number
	// without consuming the sites (the census probe) would otherwise drain them
	// out from under the report test that names them.
	undecidableCount int
)

// noteUndecidableFormat RECORDS a site the predicate cannot decide. This is the
// whole point of the channel: before it existed the predicate returned early and
// the site vanished from every count the gate reported.
func noteUndecidableFormat(p *parsed, line int, x ast.Expr) {
	site := undecidableSite{rel: p.rel, line: line, expr: exprSource(p, x)}
	undecidableMu.Lock()
	defer undecidableMu.Unlock()
	if undecidableSeen[site] {
		return
	}
	undecidableSeen[site] = true
	undecidableCount++
	undecidableDrain = append(undecidableDrain, site.render())
}

// UndecidableFormats returns (and clears) the undecidable sites recorded since
// the last call, sorted, as `rel:line  expr` lines. Consumers are the report
// test (which prints the count the devdoc quotes) and the probe test (which
// asserts a variable format string LANDS here instead of disappearing).
func UndecidableFormats() []string {
	undecidableMu.Lock()
	defer undecidableMu.Unlock()
	out := append([]string(nil), undecidableDrain...)
	undecidableDrain = nil
	sort.Strings(out)
	return out
}

// ForgetUndecidableFormats drops the recorded sites without returning them, so
// a probe starts from a known-empty channel.
func ForgetUndecidableFormats() {
	undecidableMu.Lock()
	defer undecidableMu.Unlock()
	undecidableSeen = map[undecidableSite]bool{}
	undecidableDrain = nil
	undecidableCount = 0
}

// CountUndecidableFormats reports how many distinct unresolvable sites have been
// recorded since the last Forget, WITHOUT draining the list a reader prints.
func CountUndecidableFormats() int {
	undecidableMu.Lock()
	defer undecidableMu.Unlock()
	return undecidableCount
}

// render is the report line for one recorded site.
func (s undecidableSite) render() string {
	return s.rel + ":" + strconv.Itoa(s.line) + "  " + s.expr
}

// exprSource renders an expression verbatim from the file's own bytes, so the
// report shows the operator what it could not decide (a rendering by AST shape
// would be lossy and re-printed differently from the source).
func exprSource(p *parsed, x ast.Expr) string {
	if p == nil || p.fset == nil || x == nil {
		return "?"
	}
	start := p.fset.Position(x.Pos()).Offset
	end := p.fset.Position(x.End()).Offset
	if start < 0 || end > len(p.src) || start >= end {
		return "?"
	}
	return strings.Join(strings.Fields(string(p.src[start:end])), " ")
}
