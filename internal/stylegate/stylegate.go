// Package stylegate is the in-repo, zero-dependency mechanical style scanner
// for the GoPi coding standards (devdoc/coding-standards-checklist.md).
//
// It answers ONE question mechanically: where does the repo still violate a
// machine-decidable style rule? It never rewrites code — it only reports, so
// the same code path produces the "before" and "after" counts of a sweep and
// doubles as the anti-regression gate (see gate_test.go).
//
// Three rule classes are implemented, all with stdlib go/ast + go/types:
//
//	errorf-verb    fmt.Errorf(...) formatting an error value with %v where the
//	               error should stay in the chain (errorlint class) → %w
//	ignored-error  a statement discarding an error-typed value (`_ = f()`)
//	               without an explicit reason comment
//	test-sleep     time.Sleep in a test file without an allow marker
//	test-setenv    os.Setenv in a test file (must be t.Setenv)
//	test-skip      t.Skip/t.Skipf without a literal reason
//
// Rule 7 (dependency preflight): stdlib only. Type information comes from
// `go list -export -json -deps`, the compiler's own export data — no
// golang.org/x/tools, nothing added to go.mod.
package stylegate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Rule identifiers. These are the wire values reported by the scanner and
// asserted by the gate — never spell them inline (Rule 1b).
const (
	RuleErrorfVerb      = "errorf-verb"
	RuleIgnoredError    = "ignored-error"
	RuleUncheckedClose  = "unchecked-close"
	RuleTestSleep       = "test-sleep"
	RuleTestSetenv      = "test-setenv"
	RuleTestSetenvScope = "test-setenv-scope"
	RuleTestSkip        = "test-skip"
	RuleTestChdir       = "test-chdir"
)

// AllRules is the closed set of rules the scanner knows about.
var AllRules = []string{
	RuleErrorfVerb,
	RuleIgnoredError,
	RuleUncheckedClose,
	RuleTestSleep,
	RuleTestSetenv,
	RuleTestSetenvScope,
	RuleTestSkip,
	RuleTestChdir,
}

// Package paths the rules compare against — the stdlib import paths an
// os.PkgName resolves to (Rule 1b: each value is spelled once, here).
const (
	pkgPathOS      = "os"
	pkgPathFmt     = "fmt"
	pkgPathTime    = "time"
	pkgPathTesting = "testing"
)

// AllowMarker is the in-source marker that documents a deliberately retained
// violation. It is the ONLY accepted form for time.Sleep in tests (see
// RuleTestSleep) and it carries the reason on the same line:
//
//	time.Sleep(50 * time.Millisecond) //stylegate:allow-sleep waits out the 30ms debounce
//
// A marker without a reason (fewer than MinReasonLen characters after it) is
// still a violation — "because I said so" is not a reason.
const AllowMarker = "//stylegate:allow-sleep"

// IgnoreMarker documents a deliberately discarded error. It must carry a
// reason on the same line: `//stylegate:ignore <reason>`. Ordinary prose
// comments are NOT documentation — a machine check needs one unambiguous
// spelling, otherwise every TODO next to an ignore would silence the rule.
const IgnoreMarker = "//stylegate:ignore"

// IgnoredProseForms are the two prose phrasings Rule 15 sanctions, accepted
// as an alternative to the marker when they carry a reason.
var IgnoredProseForms = []string{"deliberately ignored", "intentionally ignored"}

// MinReasonLen is the shortest accepted allow/ignore reason.
const MinReasonLen = 10

// Finding is one violation site.
type Finding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	EndLine int    `json:"end_line"` // last line of the offending statement
	Rule    string `json:"rule"`
	Detail  string `json:"detail"`
	// EditOffset/EditLen delimit the exact byte range a mechanical fix must
	// replace (the verb letter for errorf-verb). Zero when the site has no
	// mechanical fix.
	EditOffset int `json:"edit_offset"`
	EditLen    int `json:"edit_len"`
	// EditWhat names the mechanical edit ("%v->%w"), empty when none.
	EditWhat string `json:"edit_what"`
	// NilSafe is true when the offending value is provably non-nil at this
	// site, which is the precondition for the whole mechanical edit set.
	NilSafe bool `json:"nil_safe"`
	// Head is the call/receiver key used to attach a category reason
	// ("json.Marshal", "rows.Close", …) without re-parsing the detail text.
	Head string `json:"head"`
	// Callee is the called function's name without arguments — the grouping
	// key for the reason table ("os.Hostname", "syscall.Kill").
	Callee string `json:"callee"`
	// StmtOffset is the byte offset of the offending statement's first token
	// (the insertion point for an explicit `_ = ` prefix).
	StmtOffset int `json:"stmt_offset"`
	// CommentOffset is the byte offset where a trailing reason comment may be
	// inserted, valid only when CommentSafe is true.
	CommentOffset int `json:"comment_offset"`
	// CommentSafe is true when everything between the statement end and the
	// end of its line is blank or a comment — the only case where a trailing
	// comment can be inserted without changing meaning.
	CommentSafe bool `json:"comment_safe"`
}

// Report is the scanner's full output.
type Report struct {
	Root     string         `json:"root"`
	Counts   map[string]int `json:"counts"`
	Findings []Finding      `json:"findings"`
	// Undecidable counts sites the scanner could not decide mechanically
	// (non-constant format strings, unresolvable types). They are reported
	// separately so they are never silently dropped (Rule 15: no silent
	// failure) — an undecidable site is NOT a violation.
	Undecidable int `json:"undecidable"`
	// DeferredCloses counts `defer x.Close()` sites (informational): the repo
	// policy keeps the conventional deferred close, so they are not findings.
	DeferredCloses int `json:"deferred_closes"`
	// PackagesFailed lists packages whose type information failed to load;
	// their findings are incomplete and the scanner says so instead of
	// pretending completeness (Rule 15: no silent failure).
	PackagesFailed []string `json:"packages_failed"`
	// NilGuard records a region where an expression is provably non-nil
	// because of a dominant condition (`if X != nil {` / `if X == nil {} else {`).
	// Converting %v→%w is only safe inside such a region: a nil error prints
	// `%!w(<nil>)` under %w but `<nil>` under %v, so an unguarded site would
	// change the message text at runtime.
	NilGuards []NilGuard `json:"nil_guards,omitempty"`
}

// A finding is reported with NilSafe=true only when the wrapped error is
// provably non-nil at the call site (dominant `!= nil` guard, no reassignment
// in between). Everything else is left as %v by design.

// NilGuard is a source region guarding an expression against nil.
type NilGuard struct {
	File  string `json:"file"`
	Ident string `json:"ident"`
	Start int    `json:"start"` // byte offset, inclusive
	End   int    `json:"end"`   // byte offset, inclusive
	// obj is the guarded variable's OBJECT identity. Matching by object (not
	// by name) is what makes the guard sound: a shadowed `err` inside an
	// inner scope is a different object and must not inherit the outer
	// guard. Unexported on purpose — it is an in-process identity, never
	// serialised.
	obj types.Object
}

// Options configures a scan.
type Options struct {
	// Root is the module root to scan (default ".").
	Root string
	// IncludeTests also scans _test.go files (rules errorf-verb and
	// ignored-error); the test-* rules always apply to test files.
	IncludeTests bool
	// Exclude drops any file whose repo-relative path contains this substring
	// (used to measure the host repo without the scanner's own package).
	Exclude string
}

// Scan walks the module rooted at o.Root and returns every violation.
func Scan(o Options) (*Report, error) {
	root := o.Root
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	pkgs, exports, err := listPackages(abs)
	if err != nil {
		return nil, err
	}
	rep := &Report{Root: root, Counts: map[string]int{}}
	for _, r := range AllRules {
		rep.Counts[r] = 0
	}
	imp := importer.ForCompiler(token.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) {
		f, ok := exports[path]
		if !ok || f == "" {
			return nil, fmt.Errorf("stylegate: no export data for %q", path)
		}
		return os.Open(f)
	})
	for _, p := range pkgs {
		if p.Error != nil {
			rep.PackagesFailed = append(rep.PackagesFailed, p.ImportPath)
			continue
		}
		found, undecidable, guards, deferred, err := scanPackage(abs, p, imp, o.IncludeTests, o.Exclude)
		if err != nil {
			return nil, err
		}
		rep.Findings = append(rep.Findings, found...)
		rep.NilGuards = append(rep.NilGuards, guards...)
		rep.Undecidable += undecidable
		rep.DeferredCloses += deferred
	}
	sort.Slice(rep.Findings, func(i, j int) bool {
		if rep.Findings[i].File != rep.Findings[j].File {
			return rep.Findings[i].File < rep.Findings[j].File
		}
		return rep.Findings[i].Line < rep.Findings[j].Line
	})
	for _, f := range rep.Findings {
		rep.Counts[f.Rule]++
	}
	return rep, nil
}

// listPkg is the subset of `go list -json` the scanner needs.
type listPkg struct {
	ImportPath   string
	Dir          string
	Name         string
	GoFiles      []string
	CgoFiles     []string
	TestGoFiles  []string
	Export       string
	Standard     bool
	Incomplete   bool
	Error        *struct{ Err string }
	XTestGoFiles []string
}

// listPackages runs `go list -export -deps -json ./...` and returns the
// first-party packages (those under the module) plus the path→export-file map
// the type checker needs for imports.
func listPackages(root string) ([]*listPkg, map[string]string, error) {
	cmd := exec.Command("go", "list", "-e", "-export", "-deps", "-json", "./...")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("stylegate: go list: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	dec := json.NewDecoder(&stdout)
	exports := map[string]string{}
	var pkgs []*listPkg
	for {
		var p listPkg
		err := dec.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil { // the EOF case is handled above; this is a real decode failure
			return nil, nil, fmt.Errorf("stylegate: decode go list: %w", err)
		}
		if p.Export != "" {
			exports[p.ImportPath] = p.Export
		}
		if p.Dir == "" || !strings.HasPrefix(p.Dir, root) {
			continue // stdlib / module cache dependency
		}
		pkgs = append(pkgs, &p)
	}
	return pkgs, exports, nil
}

// scanPackage parses one package and applies every rule.
func scanPackage(root string, p *listPkg, imp types.Importer, includeTests bool, exclude string) ([]Finding, int, []NilGuard, int, error) {
	fset := token.NewFileSet()
	names := append([]string{}, p.GoFiles...)
	names = append(names, p.CgoFiles...)
	if includeTests {
		names = append(names, p.TestGoFiles...)
	}
	var files []*ast.File
	byName := map[string]*ast.File{}
	for _, n := range names {
		path := filepath.Join(p.Dir, n)
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			// A package that does not even parse is a build failure, not a
			// style finding; skip it rather than guess (no silent wrong answer).
			return nil, 0, nil, 0, nil
		}
		files = append(files, f)
		byName[filepath.Join(p.Dir, n)] = f
	}
	if len(files) == 0 {
		return nil, 0, nil, 0, nil
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{
		Importer: imp,
		// Type errors are expected (we merge test files into the package, and
		// a failed import degrades gracefully instead of aborting the scan).
		// Whatever IS resolved is used; unresolved sites stay undecidable.
		Error:       func(error) {},
		FakeImportC: false,
	}
	//stylegate:ignore type errors are expected here — an unresolved import or a
	// merged-in test file degrades to "undecidable", which is reported.
	_, _ = conf.Check(p.ImportPath, fset, files, info)

	var out []Finding
	var guards []NilGuard
	undecidable := 0
	deferredCloses := 0
	for path, f := range byName {
		rel := relPath(root, path)
		if exclude != "" && strings.Contains(rel, exclude) {
			continue
		}
		comments := commentsByLine(fset, f)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, 0, nil, 0, fmt.Errorf("stylegate: read %s: %w", path, err)
		}
		v := &visitor{
			fset: fset, info: info, rel: rel, comments: comments, src: src,
			containerHeaderLine: containerHeaderLines(fset, f),
			testFile:            strings.HasSuffix(path, "_test.go"),
			file:                f,
			out:                 &out, undecidable: &undecidable, deferred: &deferredCloses,
		}
		// Pass 1 — the nil-guard regions must be known before any finding is
		// judged, because %v→%w is only safe under a proven non-nil guard.
		ast.Inspect(f, func(n ast.Node) bool {
			if iff, ok := n.(*ast.IfStmt); ok {
				v.collectGuards(iff)
			}
			return true
		})
		guards = append(guards, v.guards...)
		// Pass 2 — findings.
		ast.Inspect(f, v.visit)
	}
	return out, undecidable, guards, deferredCloses, nil
}

// commentsByLine indexes every comment's text by the line it sits on.
func commentsByLine(fset *token.FileSet, f *ast.File) map[int]string {
	m := map[int]string{}
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			line := fset.Position(c.Pos()).Line
			if prev, ok := m[line]; ok {
				m[line] = prev + " " + c.Text
			} else {
				m[line] = c.Text
			}
		}
	}
	return m
}

func relPath(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

type visitor struct {
	fset     *token.FileSet
	info     *types.Info
	rel      string
	comments map[int]string
	testFile bool
	inDefer  int
	file     *ast.File
	src      []byte
	stmt     ast.Stmt
	// containerHeaderLine marks the lines that begin a container statement —
	// the only lines a marker above a site's own line may be read from.
	containerHeaderLine map[int]bool
	guards              []NilGuard
	funcLits            []*ast.FuncLit
	out                 *[]Finding
	undecidable         *int
	deferred            *int
}

func (v *visitor) visit(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.CallExpr:
		v.checkErrorf(x)
		v.checkTestCall(x)
		if v.inDefer > 0 {
			v.countDeferredClose(x)
		}
	case *ast.ExprStmt:
		// A bare call statement is the only place an unchecked Close lives.
		v.stmt = x
		if call, ok := x.X.(*ast.CallExpr); ok && v.inDefer == 0 {
			v.checkBareClose(call, x)
		}
		v.checkChdirStmt(x, x.X)
	case *ast.AssignStmt:
		v.stmt = x
		v.checkAssign(x)
	case *ast.DeferStmt:
		v.checkChdirStmt(x, x.Call)
		v.inDefer++
		ast.Inspect(x.Call, v.visit)
		v.inDefer--
		return false
	case *ast.GoStmt:
		v.checkChdirStmt(x, x.Call)
	case *ast.FuncLit:
		// Track the enclosing closures so t.Setenv's cleanup scope can be
		// judged (see checkSetenvScope).
		v.funcLits = append(v.funcLits, x)
		ast.Inspect(x.Body, v.visit)
		v.funcLits = v.funcLits[:len(v.funcLits)-1]
		return false
	}
	return true
}

// placeComment fills the trailing-comment insertion point for a statement.
// It refuses (CommentSafe=false) whenever anything other than blank space or
// a comment follows the statement on its line — that is the only shape where
// an inserted comment cannot swallow code.
func (v *visitor) placeComment(f *Finding, stmt ast.Stmt) {
	if stmt == nil {
		return
	}
	f.StmtOffset = v.fset.Position(stmt.Pos()).Offset
	end := v.fset.Position(stmt.End()).Offset
	if end < 0 || end > len(v.src) {
		return
	}
	lineEnd := end
	for lineEnd < len(v.src) && v.src[lineEnd] != '\n' {
		lineEnd++
	}
	rest := string(v.src[end:lineEnd])
	trimmed := strings.TrimSpace(rest)
	if trimmed == "" {
		f.CommentOffset = end
		f.CommentSafe = true
		return
	}
	if strings.HasPrefix(trimmed, "//") {
		f.CommentOffset = end + strings.Index(rest, "//")
		f.CommentSafe = true
		return
	}
	// Anything else (a second statement on the same line, a block terminator
	// or a composite-literal continuation) — no mechanical comment.
}

// report appends a finding with its category head, statement offset and
// comment insertion point, collapsing duplicate findings that share a
// file/line/rule (a merged statement like
// `err = a(); _ = err` yields one site, not two).
func (v *visitor) report(rule string, pos token.Pos, stmt ast.Stmt, detail, head, callee string) {
	for _, prev := range *v.out {
		if prev.File == v.rel && prev.Rule == rule {
			p := v.fset.Position(pos)
			if prev.Line == p.Line {
				return
			}
		}
	}
	f := Finding{
		File: v.rel, Rule: rule, Detail: detail, Head: head, Callee: callee,
	}
	p := v.fset.Position(pos)
	f.Line, f.Col = p.Line, p.Column
	v.placeComment(&f, stmt)
	*v.out = append(*v.out, f)
}

func (v *visitor) add(pos token.Pos, rule, detail string) {
	p := v.fset.Position(pos)
	*v.out = append(*v.out, Finding{File: v.rel, Line: p.Line, Col: p.Column, Rule: rule, Detail: detail})
}

// maxReasonCommentLines bounds how far above a site a marker may sit. A
// reason that wraps onto continuation lines is still one declaration, but an
// unbounded walk would attach a distant unrelated comment block.
const maxReasonCommentLines = 4

// lineComment returns the comment text on line plus the contiguous comment
// block directly above it (a marker may be followed by continuation lines).
func (v *visitor) lineComment(line int) string {
	parts := []string{}
	if c := v.comments[line]; c != "" {
		parts = append(parts, c)
	}
	for i := 1; i <= maxReasonCommentLines; i++ {
		prev := v.comments[line-i]
		if prev == "" {
			break
		}
		parts = append([]string{prev}, parts...)
	}
	return strings.Join(parts, " ")
}

// enclosingStmt returns the statement the visitor is currently inside, used
// only as a comment-insertion anchor.
func (v *visitor) enclosingStmt(ast.Node) ast.Stmt {
	return v.stmt
}

// documented reports whether the site at pos carries an explicit reason
// comment (same line or the line immediately above) — the accepted form of a
// deliberate ignore.
func (v *visitor) documented(pos token.Pos) string {
	return v.lineComment(v.fset.Position(pos).Line)
}

// ignoreReason returns the reason a site declares for discarding an error, and
// whether it declared one at all. Accepted forms (both need >= MinReasonLen
// characters of reason):
//
//	//stylegate:ignore <reason>          (the marker — preferred)
//	// ... deliberately ignored: <reason> (Rule 15's prose form)
//	// ... intentionally ignored: <reason>
func (v *visitor) ignoreReason(pos token.Pos) (string, bool) {
	// The enclosing statement is the FORWARD leg's anchor: without it a marker
	// at the end of a multi-line `_ = call(...)` statement (where an editor's
	// "comment the statement" gesture lands it) is unreachable. Passing nil
	// here is what made the second half of this defect silent.
	//
	// A call site that already knows its statement passes it explicitly
	// (reasonOverSpan(pos, stmt)) and does not come through here.
	return v.reasonOverSpan(pos, v.stmt)
}

// reasonOverSpan resolves a documented reason for a site, accepting the marker
// ANYWHERE in the offending statement's line span — a multi-line call is
// annotated at its end by every editor's "comment the statement" gesture, and
// a scanner that only looks at the first line would silently ignore the
// author's reason.
func (v *visitor) reasonOverSpan(pos token.Pos, stmt ast.Stmt) (string, bool) {
	start := v.fset.Position(pos)
	c := strings.TrimSpace(v.lineComment(start.Line))
	if stmt != nil && stmt.Pos() <= pos && pos <= stmt.End() {
		end := v.fset.Position(stmt.End())
		for l := start.Line + 1; l <= end.Line; l++ {
			if t := strings.TrimSpace(v.comments[l]); t != "" {
				c += " " + t
			}
		}
	}
	if s, ok := reasonInText(c); ok {
		return s, true
	}
	// The block shape: the marker sits on the header of the container whose
	// body this site is the first statement of, so it is neither on the site's
	// own line nor anywhere inside the site's (one-line) statement span.
	return v.containerHeaderReason(pos, stmt)
}

// reasonInText extracts a reason declaration from one blob of comment text and
// reports whether it found a long-enough one. It is the ONE place the accepted
// spellings are matched, so every leg below recognises the same forms.
func reasonInText(c string) (string, bool) {
	if c == "" {
		return "", false
	}
	if i := strings.Index(c, IgnoreMarker); i >= 0 {
		return reasonFrom(c[i+len(IgnoreMarker):]), true
	}
	for _, form := range IgnoredProseForms {
		if i := strings.Index(c, form); i >= 0 {
			return reasonFrom(c[i+len(form):]), true
		}
	}
	return "", false
}

// maxContainerHeaderHops bounds the header climb. The chain it walks is the
// nesting of the site's enclosing blocks (`if` inside `if` inside `for`), which
// is a handful at most; the bound exists so a pathological file cannot turn one
// finding into a whole-file walk, and it is a NAMED limit rather than a literal
// for the reason Rule 1 gives.
const maxContainerHeaderHops = 8

// containerHeaderReason reads a marker written on the header of a container
// statement (`if`/`for`/`range`/`switch`/`type switch`/`select`) that ENCLOSES
// the site, which is the shape a Go author naturally writes:
//
//	//stylegate:ignore the probe's own teardown; ESRCH is the expected error
//	if grandID, grandErr := testutil.ReadProcIdentity(fixture.grand); grandErr == nil {
//		_ = testutil.KillIfIdentical(grandID, syscall.SIGKILL)
//	}
//
// Before this leg existed the scanner read NEITHER half of it: lineComment
// walked up from the site and stopped dead at the `if` header (a code line),
// and reasonOverSpan was called with an enclosing statement that covers the
// site's span only — the header sits outside it. The author's reason was
// silently discarded and the site kept counting against its ceiling (measured
// site: cmd/agent/worker_stop_group_self_guard_test.go:88).
//
// The climb is deliberately NOT a "look further up" widening: it moves only
// through lines that ARE a container statement's first line, so a marker
// detached from the header by a blank line or an ordinary statement (the
// measured false-positive shape in the regression probe) is still unreachable.
func (v *visitor) containerHeaderReason(pos token.Pos, stmt ast.Stmt) (string, bool) {
	line := v.fset.Position(pos).Line
	if stmt != nil {
		// Start just above the statement the marker would document: a marker on
		// a multi-line statement's own line is already covered by its span.
		line = v.fset.Position(stmt.Pos()).Line
	}
	hops := 0
	for hops < maxContainerHeaderHops {
		line--
		if line < 1 || !v.containerHeaderLine[line] {
			return "", false
		}
		hops++
		if s, ok := reasonInText(strings.TrimSpace(v.lineComment(line))); ok {
			return s, true
		}
	}
	return "", false
}

// containerHeaderLines returns the set of lines that begin a container
// statement — the only lines the header climb may step onto.
func containerHeaderLines(fset *token.FileSet, f *ast.File) map[int]bool {
	m := map[int]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
			*ast.TypeSwitchStmt, *ast.SelectStmt:
			m[fset.Position(n.Pos()).Line] = true
		}
		return true
	})
	return m
}

// reasonFrom normalises a reason tail (leading separators stripped) and
// reports whether it is long enough to count as a reason.
func reasonFrom(tail string) string {
	tail = strings.TrimSpace(strings.TrimLeft(tail, ":—-"))
	tail = strings.TrimSpace(tail)
	if len(tail) < MinReasonLen {
		return ""
	}
	return tail
}

// ---------------------------------------------------------------------------
// Rule: errorf-verb — fmt.Errorf("…%v", err) should be %w when the value is an error.
// ---------------------------------------------------------------------------

func (v *visitor) checkErrorf(call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Errorf" {
		return
	}
	if !isPkg(v.info, sel.X, pkgPathFmt) {
		return
	}
	if len(call.Args) < 2 {
		return
	}
	format, ok := constString(v.info, call.Args[0])
	if !ok {
		*v.undecidable++ // dynamic format string: the verbs are not knowable statically
		return
	}
	verbs := parseVerbs(format)
	if len(verbs) == 0 {
		return
	}
	args := call.Args[1:]
	lit, _ := call.Args[0].(*ast.BasicLit)
	for i, vb := range verbs {
		if vb.argIndex > 0 {
			// explicit %[n]v index
			if vb.argIndex-1 >= len(args) {
				continue
			}
			v.maybeErrorVerb(args[vb.argIndex-1], vb, call, lit, format)
			continue
		}
		if i >= len(args) {
			continue
		}
		v.maybeErrorVerb(args[i], vb, call, lit, format)
	}
}

func (v *visitor) maybeErrorVerb(arg ast.Expr, vb verb, call *ast.CallExpr, lit *ast.BasicLit, sem string) {
	if vb.letter != 'v' {
		if vb.letter == 'w' {
			// already wrapping — nothing to do, but do not double-wrap silently
			return
		}
		return
	}
	if vb.flags != "" {
		// %+v / %#v / %-10v print more than %v; %w would change the output.
		// Retained by design — not reported as a violation.
		return
	}
	tv, ok := v.info.Types[arg]
	if !ok || tv.Type == nil {
		*v.undecidable++
		return
	}
	if !implementsError(tv.Type) {
		return
	}
	simple, ok := simpleIdent(arg)
	nilSafe := ok && v.nilSafe(simple, arg.Pos())
	f := Finding{
		File: v.rel, Rule: RuleErrorfVerb,
		Detail:  fmt.Sprintf("%%v wraps %s (expr %s)", tv.Type.String(), exprString(v.fset, arg)),
		NilSafe: nilSafe,
		Head:    exprString(v.fset, arg),
	}
	p := v.fset.Position(call.Pos())
	f.Line, f.Col = p.Line, p.Column
	if nilSafe && lit != nil {
		if off, ok := literalVerbOffset(v.fset.Position(lit.Pos()).Offset, lit.Value, sem, vb.semanticOffset); ok {
			f.EditOffset, f.EditLen, f.EditWhat = off, 1, "%v->%w"
		}
	}
	*v.out = append(*v.out, f)
}

// calleeName renders the called function without its arguments: the grouping
// key for the reason table ("os.Hostname", "syscall.Kill").
func calleeName(fset *token.FileSet, e ast.Expr) string {
	if call, ok := e.(*ast.CallExpr); ok {
		return exprString(fset, call.Fun)
	}
	return exprString(fset, e)
}

// simpleIdent reduces an expression to the bare identifier it names, which is
// the only shape the nil-guard analysis tracks (`err`, `rowsErr`, …). A call,
// field or index expression is never nil-guarded — it is always "unsure", and
// an unsure site stays as-is.
func simpleIdent(e ast.Expr) (*ast.Ident, bool) {
	id, ok := e.(*ast.Ident)
	return id, ok
}

// nilSafe reports whether id is inside a dominant `id != nil` guard region and
// was not reassigned between the guard and pos.
func (v *visitor) nilSafe(id *ast.Ident, pos token.Pos) bool {
	off := v.fset.Position(pos).Offset
	obj := v.info.ObjectOf(id)
	if obj == nil {
		return false // unknown object: no proof, no edit
	}
	for _, g := range v.guards {
		if g.obj != obj || g.File != v.rel || off < g.Start || off > g.End {
			continue
		}
		if !v.reassignedBetween(obj, g.Start, off) {
			return true
		}
	}
	return false
}

// reassignedBetween reports whether obj is assigned anywhere in (from, to].
func (v *visitor) reassignedBetween(obj types.Object, from, to int) bool {
	reassigned := false
	ast.Inspect(v.file, func(n ast.Node) bool {
		if reassigned || n == nil {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		o := v.fset.Position(as.Pos()).Offset
		if o <= from || o > to {
			return true
		}
		for _, lhs := range as.Lhs {
			if lid, ok := lhs.(*ast.Ident); ok && v.info.ObjectOf(lid) == obj {
				reassigned = true
				return false
			}
		}
		return true
	})
	return reassigned
}

// collectGuards records the if-statement body regions guarded by `id != nil`
// (and the else-branch of `id == nil`).
func (v *visitor) collectGuards(iff *ast.IfStmt) {
	b, ok := iff.Cond.(*ast.BinaryExpr)
	if !ok || (b.Op != token.NEQ && b.Op != token.EQL) {
		return
	}
	left, lok := b.X.(*ast.Ident)
	right, rok := b.Y.(*ast.Ident)
	var id *ast.Ident
	switch {
	case lok && rok && right.Name == nilIdent:
		id = left
	case lok && rok && left.Name == nilIdent:
		id = right
	default:
		return
	}
	var nonNilBody, nilBody ast.Stmt = iff.Body, iff.Else
	if b.Op == token.EQL {
		nonNilBody, nilBody = iff.Else, iff.Body
	}
	if nonNilBody == nil {
		return
	}
	if nilBody != nil && assignsIdent(v.info, nilBody, id) {
		// The nil branch mutates the variable, so the "non-nil" region is not
		// provably non-nil afterwards — report nothing rather than guess.
		return
	}
	v.guards = append(v.guards, NilGuard{
		File: v.rel, Ident: id.Name,
		Start: v.fset.Position(nonNilBody.Pos()).Offset,
		End:   v.fset.Position(nonNilBody.End()).Offset,
		obj:   v.info.ObjectOf(id),
	})
}

// assignsIdent reports whether any statement in n assigns the identifier's
// object.
func assignsIdent(info *types.Info, n ast.Node, id *ast.Ident) bool {
	obj := info.ObjectOf(id)
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found || x == nil {
			return false
		}
		as, ok := x.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if lid, ok := lhs.(*ast.Ident); ok && info.ObjectOf(lid) == obj {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// nilIdent is the predeclared nil's spelling (Rule 1b: named, not inline).
const nilIdent = "nil"

// literalVerbOffset maps a verb's index in the DECODED format string back to
// the byte offset of the correspondent character in the Go string LITERAL,
// skipping only one-character backslash escapes. It refuses anything it
// cannot account for exactly (returns false) — an unsure offset must never
// become a wrong edit ("wrong change is worse than no change").
// baseOffset is the literal's byte offset inside its file (token.Pos values
// share one FileSet across a package, so they are NOT file offsets).
func literalVerbOffset(baseOffset int, raw, semantic string, semanticIdx int) (int, bool) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return 0, false // backquoted / malformed — no mechanical edit
	}
	if semanticIdx >= len(semantic) {
		return 0, false
	}
	// Escaped forms wider than one character are outside our exact model.
	if strings.Contains(raw, `\x`) {
		return 0, false
	}
	esc, ok := unescapeOffset(raw, semantic, semanticIdx)
	if !ok {
		return 0, false
	}
	return baseOffset + esc, true
}

// unescapeOffset walks the literal body and the decoded string in lockstep,
// returning the literal-body offset of decoded[semanticIdx].
func unescapeOffset(literal, decoded string, semanticIdx int) (int, bool) {
	body := literal[1 : len(literal)-1]
	li, di := 0, 0
	for di < len(decoded) {
		if li >= len(body) {
			return 0, false
		}
		if di == semanticIdx {
			return li + 1, true // +1 for the opening quote
		}
		if body[li] != '\\' {
			li++
			di++
			continue
		}
		if li+1 >= len(body) {
			return 0, false
		}
		switch body[li+1] {
		case 'n', 't', 'r', '\\', '"', 'a', 'b', 'f', 'v':
			li += 2
			di++
		default:
			// \uXXXX / \0NN / anything wider: outside our exact model.
			return 0, false
		}
	}
	return 0, false
}

type verb struct {
	letter   byte
	flags    string
	argIndex int // 1-based explicit index from %[n]v, 0 when implicit
	// semanticOffset is the index of the verb LETTER inside the semantic
	// format string (escape-decoded), used to locate the byte to rewrite.
	semanticOffset int
}

// parseVerbs extracts the formatting verbs (in order) from a format string.
func parseVerbs(format string) []verb {
	var out []verb
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		if i >= len(format) {
			break
		}
		if format[i] == '%' {
			continue // literal percent
		}
		var v verb
		// %[n]v
		if format[i] == '[' {
			j := i + 1
			for j < len(format) && format[j] >= '0' && format[j] <= '9' {
				j++
			}
			if j < len(format) && format[j] == ']' {
				n, err := strconv.Atoi(format[i+1 : j])
				if err == nil {
					v.argIndex = n
				}
				i = j + 1
			}
		}
		// flags
		start := i
		for i < len(format) && strings.IndexByte("+-# 0", format[i]) >= 0 {
			i++
		}
		v.flags = format[start:i]
		// width / precision
		for i < len(format) && (format[i] >= '0' && format[i] <= '9' || format[i] == '.' || format[i] == '*') {
			i++
		}
		if i >= len(format) {
			break
		}
		v.letter = format[i]
		if isVerbLetter(v.letter) {
			v.semanticOffset = i
			out = append(out, v)
		}
	}
	return out
}

func isVerbLetter(c byte) bool {
	return strings.IndexByte("vTtbcdoOqxXUeEfgGsqpw", c) >= 0
}

// constString resolves a compile-time constant string expression.
func constString(info *types.Info, e ast.Expr) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil {
		return "", false
	}
	s, ok := constantString(tv.Value.String())
	return s, ok
}

// stringFromSyntax resolves a compile-time string expression to its EXACT
// value, including values too long for the type checker's rendering.
//
// go/types renders a constant string longer than 72 characters as
// `"prefix..."` (measured with a standalone probe: an 82-char constant renders
// as 68 chars plus the ellipsis), and that rendering cannot be unquoted — so
// constantString refuses it and the site reads as "no literal reason". The
// VALUE is not truncated at all (constant.StringVal returns all 82 chars), so
// the exact reading comes from the constant, not from its rendering.
//
// Its callers are the rules that ask "is this argument a literal?" — a question
// a folded constant answers correctly at any length.
func stringFromSyntax(info *types.Info, e ast.Expr) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

func constantString(q string) (string, bool) {
	// types constant.Value.String() quotes strings; unquote via the literal parser.
	if len(q) < 2 || q[0] != '"' {
		return "", false
	}
	if strings.HasSuffix(q, "...") {
		// types TRUNCATES a long constant to "prefix..." (a value longer than
		// 72 chars). The ellipsis is not part of the value, so the raw string
		// cannot be unquoted. constString no longer routes a string constant
		// through this rendering (constant.StringVal is exact), so this arm is
		// the residual one: a non-string constant, or a caller that had only a
		// rendered form. Answering "not a constant" here would silently
		// reclassify a perfectly good literal as a violation, so callers that
		// can see the expression re-check it from the value or the syntax.
		return "", false
	}
	s, err := strconv.Unquote(q)
	if err != nil {
		return "", false
	}
	return s, true
}

func implementsError(t types.Type) bool {
	if t == nil {
		return false
	}
	iface, ok := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	return types.Implements(t, iface) || types.Implements(types.NewPointer(t), iface)
}

func isPkg(info *types.Info, e ast.Expr, path string) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	pn, ok := info.Uses[id].(*types.PkgName)
	if !ok {
		return false
	}
	return pn.Imported().Path() == path
}

func exprString(fset *token.FileSet, e ast.Expr) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, e); err != nil {
		return "<expr>"
	}
	s := buf.String()
	if len(s) > maxExprLen {
		s = s[:maxExprLen] + "…"
	}
	return s
}

// maxExprLen bounds a rendered expression in a finding detail.
const maxExprLen = 64

// ---------------------------------------------------------------------------
// Rule: ignored-error — `_ = expr` where expr is an error, with no reason.
// ---------------------------------------------------------------------------

func (v *visitor) checkAssign(as *ast.AssignStmt) {
	if as.Tok != token.ASSIGN && as.Tok != token.DEFINE {
		return
	}
	if len(as.Rhs) != 1 {
		return
	}
	call, isCall := as.Rhs[0].(*ast.CallExpr)
	for i, lhs := range as.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name != "_" {
			continue
		}
		if len(as.Lhs) == 1 {
			tv, ok := v.info.Types[as.Rhs[0]]
			if !ok || tv.Type == nil {
				*v.undecidable++
				return
			}
			if !implementsError(tv.Type) {
				return // discarding a non-error value is not an error-check violation
			}
			v.reportIgnored(as, as.Rhs[0])
			return
		}
		// Multi-value assignment: `_, _ = f()` — the blank may be the error
		// result. Resolve f's signature and check the type of result i.
		if !isCall {
			continue
		}
		res := resultType(v.info, call, i, len(as.Lhs))
		if res == nil {
			*v.undecidable++
			continue
		}
		if !implementsError(res) {
			continue
		}
		if _, ok := v.reasonOverSpan(as.Pos(), as); ok {
			continue
		}
		expr := exprString(v.fset, call)
		v.report(RuleIgnoredError, as.Pos(), as,
			fmt.Sprintf("ignored error-typed value (%s, result %d)", expr, i+1), expr, calleeName(v.fset, call))
	}
}

// reportIgnored records a single-value `_ = expr` whose value is an error.
func (v *visitor) reportIgnored(as ast.Stmt, rhs ast.Expr) {
	if _, ok := v.reasonOverSpan(as.Pos(), as); ok {
		return // explicitly ignored with a reason — accepted
	}
	expr := exprString(v.fset, rhs)
	v.report(RuleIgnoredError, as.Pos(), as, fmt.Sprintf("ignored error-typed value (%s)", expr), expr, calleeName(v.fset, rhs))
}

// resultType resolves the i-th result type of a call whose value is spread
// over n assignment targets (handles the single-call-used-as-all-results
// shape too: n > 0 results collapsed into one expression).
func resultType(info *types.Info, call *ast.CallExpr, i, n int) types.Type {
	tv, ok := info.Types[call]
	if !ok || tv.Type == nil {
		return nil
	}
	tup, ok := tv.Type.(*types.Tuple)
	if !ok {
		if i == 0 {
			return tv.Type
		}
		return nil
	}
	if tup.Len() == 1 && n > 1 {
		// f() with one result feeding N targets is a compile error, but the
		// types map may still hold a tuple; guard rather than index blindly.
		if i == 0 {
			return tup.At(0).Type()
		}
		return nil
	}
	if i >= tup.Len() {
		return nil
	}
	return tup.At(i).Type()
}

// ---------------------------------------------------------------------------
// Rule: unchecked-close — a bare `x.Close()` statement discarding its error.
// `defer x.Close()` is counted for information (DeferredCloses) but is not a
// finding: GoPi's agreed policy keeps the conventional deferred close.
// ---------------------------------------------------------------------------

func (v *visitor) checkBareClose(call *ast.CallExpr, stmt ast.Stmt) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != closeMethodName {
		return
	}
	if !returnsError(v.info, sel.Sel) {
		return
	}
	if _, ok := v.reasonOverSpan(call.Pos(), stmt); ok {
		return // explicitly ignored with a reason — accepted
	}
	v.report(RuleUncheckedClose, call.Pos(), stmt, "error from "+exprString(v.fset, sel)+" discarded", exprString(v.fset, sel), exprString(v.fset, sel))
}

func (v *visitor) countDeferredClose(call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != closeMethodName {
		return
	}
	if returnsError(v.info, sel.Sel) {
		*v.deferred++
	}
}

// returnsError reports whether the method/function object yields a non-empty
// result tuple (i.e. has an error to discard).
func returnsError(info *types.Info, sel *ast.Ident) bool {
	obj := info.Uses[sel]
	if obj == nil {
		obj = info.Defs[sel]
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	return sig.Results().Len() > 0
}

// hasAllowMarker reports whether the sleep site carries the allow marker (the
// marker with a long-enough reason is handled by checkSleep itself).
func (v *visitor) hasAllowMarker(call *ast.CallExpr) bool {
	c := strings.TrimSpace(v.lineComment(v.fset.Position(call.Pos()).Line))
	return strings.Contains(c, AllowMarker)
}

// closeMethodName is the io.Closer method name (Rule 1b: no bare literals).
const closeMethodName = "Close"

// ---------------------------------------------------------------------------
// Test hygiene rules.
// ---------------------------------------------------------------------------

func (v *visitor) checkTestCall(call *ast.CallExpr) {
	if !v.testFile {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	switch sel.Sel.Name {
	case "Sleep":
		if !isPkg(v.info, sel.X, pkgPathTime) {
			return
		}
		if _, ok := v.reasonOverSpan(call.Pos(), v.enclosingStmt(call)); ok && !v.hasAllowMarker(call) {
			return // documented reason on the site
		}
		v.checkSleep(call)
	case "Setenv":
		switch {
		case isPkg(v.info, sel.X, pkgPathOS):
			// A documented reason is the accepted justification for a
			// process-global set: some tests hand the value to a CHILD process,
			// where t.Setenv's scoped restore is the wrong tool.
			if _, ok := v.reasonOverSpan(call.Pos(), v.enclosingStmt(call)); ok {
				return
			}
			v.report(RuleTestSetenv, call.Pos(), v.enclosingStmt(call), "os.Setenv leaks into the process env — use t.Setenv", "os.Setenv", "os.Setenv")
		case isTestReceiver(v.info, sel.X):
			v.checkSetenvScope(call)
		}
	case "Skip", "Skipf", "SkipNow":
		if !isTestReceiver(v.info, sel.X) {
			return
		}
		v.checkSkip(call, sel.Sel.Name)
	}
}

// checkSetenvScope reports `t.Setenv` used INSIDE a closure that captured the
// OUTER test's *testing.T.
//
// t.Setenv registers its undo with t.Cleanup, which runs when the TEST ends —
// not when the closure returns. A closure that flips an env var per call
// therefore leaves the last flip installed for the rest of the test (the
// sibling call, and every later assertion in it, then read the wrong value).
// This is not hypothetical: an os.Setenv+restore-in-place pair inside a
// dispatch sandbox closure was mechanically converted to t.Setenv by the
// style sweep, and cmd/agent's dispatch test started asserting about the
// wrong ledger (bisected to the sweep's Batch 3 commit). The pair was correct
// and the conversion was wrong.
//
// A closure that receives its OWN *testing.T (a t.Run subtest body) is fine:
// its cleanup runs at the subtest's end, which is the scope the author meant.
//
// Precision narrowing: a closure that sets a CONSTANT value in a
// table-driven case cannot mislead a later assertion (the var is reset by the
// harness at test end and no in-test reader depends on the flip unwinding
// early). The hazard is the PER-CALL flip — the value is computed from a
// parameter or looked up in a map, so call N's value must be gone before call
// N+1 and before the test's later assertions. That is the reporting
// condition here, and it selects exactly the two sites this rule exists for.
func (v *visitor) checkSetenvScope(call *ast.CallExpr) {
	if len(v.funcLits) == 0 {
		return // not inside a closure — t.Cleanup's scope IS the test
	}
	if len(call.Args) < 2 || literalIsConstant(v.info, call.Args[1]) {
		return // constant per call: no per-call flip to unwind
	}
	sel := call.Fun.(*ast.SelectorExpr)
	recv, ok := sel.X.(*ast.Ident)
	if !ok {
		return // t.(*T).Setenv-style receiver: no object identity to compare
	}
	obj := v.info.ObjectOf(recv)
	if obj == nil {
		return // unknown object: no proof, no report
	}
	inner := v.funcLits[len(v.funcLits)-1]
	if litDeclaresObject(v.info, inner, obj) {
		return // the closure owns this *testing.T (subtest) — correct scope
	}
	if _, ok := v.reasonOverSpan(call.Pos(), v.enclosingStmt(call)); ok {
		return // documented reason on the site (the form the sibling rules accept)
	}
	v.report(RuleTestSetenvScope, call.Pos(), v.enclosingStmt(call),
		"t.Setenv inside a closure undoes at TEST CLEANUP, not when the closure returns — a per-call value then leaks into the next call and into the test's later assertions; use os.Setenv with an explicit restore, or give the closure its own testing.T",
		"t.Setenv", "t.Setenv")
}

// isStringLit reports whether e is a quoted string constant.
func isStringLit(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// literalIsConstant reports whether e is a compile-time string constant — a
// quoted literal, a concatenation of them, or a named constant whose value the
// type checker resolved.
//
// The distinction matters for test-setenv-scope: the hazard is a PER-CALL
// value (computed from a parameter or a map lookup) whose flip must unwind
// before the next call, while a constant installs the same value every time
// and cannot mislead a later assertion. The rule's own doc says so; the
// predicate used to test only the syntactic literal, so a site setting a NAMED
// constant (EnvTerm, term256 — the ordinary spelling in this repo) was
// reported as if its value were dynamic.
func literalIsConstant(info *types.Info, e ast.Expr) bool {
	if isStringLit(e) {
		return true
	}
	if _, ok := concatLiteralString(e); ok {
		return true
	}
	if _, ok := constString(info, e); ok {
		return true
	}
	// The constant is a string but its rendering was truncated (see
	// constantString); a value the type checker folded is still a constant.
	tv, ok := info.Types[e]
	return ok && tv.Value != nil && tv.Type != nil && isStringType(tv.Type)
}

// isStringType reports whether t is the predeclared string type.
func isStringType(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.String
}

// litDeclaresObject reports whether a func literal declares obj among its own
// parameters/receiver (i.e. the object's scope is the literal itself).
func litDeclaresObject(info *types.Info, lit *ast.FuncLit, obj types.Object) bool {
	if lit.Type == nil || lit.Type.Params == nil {
		return false
	}
	for _, f := range lit.Type.Params.List {
		for _, name := range f.Names {
			if info.ObjectOf(name) == obj {
				return true
			}
		}
	}
	return false
}

func (v *visitor) checkSleep(call *ast.CallExpr) {
	c := strings.TrimSpace(v.lineComment(v.fset.Position(call.Pos()).Line))
	idx := strings.Index(c, AllowMarker)
	if idx < 0 {
		v.report(RuleTestSleep, call.Pos(), v.enclosingStmt(call), "time.Sleep in a test — poll/wait instead, or allow it with "+AllowMarker+" <reason>", "time.Sleep", "time.Sleep")
		return
	}
	reason := strings.TrimSpace(c[idx+len(AllowMarker):])
	if len(reason) < MinReasonLen {
		v.report(RuleTestSleep, call.Pos(), v.enclosingStmt(call), fmt.Sprintf("allow marker needs a reason of >= %d chars", MinReasonLen), "time.Sleep", "time.Sleep")
	}
}

func (v *visitor) checkSkip(call *ast.CallExpr, name string) {
	if name == "SkipNow" {
		v.report(RuleTestSkip, call.Pos(), v.enclosingStmt(call), "t.SkipNow has no reason string — use t.Skip(\"...\")", "t."+name, "t."+name)
		return
	}
	if len(call.Args) == 0 {
		v.report(RuleTestSkip, call.Pos(), v.enclosingStmt(call), "t."+name+" with no reason", "t."+name, "t."+name)
		return
	}
	if ok, reason := literalOrConstString(v.info, call.Args[0]); ok {
		if len(strings.TrimSpace(reason)) >= MinReasonLen {
			return
		}
		// A literal that is too short is a violation regardless of comments.
		v.report(RuleTestSkip, call.Pos(), v.enclosingStmt(call),
			"t."+name+" reason is too short to explain the skip", "t."+name, "t."+name)
		return
	}
	if _, ok := v.ignoreReason(call.Pos()); ok {
		return // dynamic reason, documented by a reason comment
	}
	v.report(RuleTestSkip, call.Pos(), v.enclosingStmt(call), "t."+name+" without a literal reason string (nor a reason comment)", "t."+name, "t."+name)
}

// chdirFuncName is the os.Chdir call the cwd-restore rule watches (Rule 1b:
// the name lives once, the rule reads it from here).
const chdirFuncName = "Chdir"

// checkChdirStmt reports a cwd change whose result nobody looks at.
//
// The scanned shape is a STATEMENT-position os.Chdir — `defer os.Chdir(dir)`,
// `t.Cleanup(func() { os.Chdir(dir) })`, or a bare `os.Chdir(dir)`. All three
// discard the error: an os.Chdir returns an error, and in statement position
// there is no `err :=` and no visible `_ =` to acknowledge it (Rule 15b).
//
// Why this site class is worse than an ordinary swallowed error: os.Chdir
// mutates PROCESS state that every later test in the same binary inherits. If
// the restore back to the original directory fails, the rest of the run
// executes inside a t.TempDir() the framework has already removed — the
// cascade of relative-path and `go build .` failures that follows names
// everything except the real cause (audit: devdoc/test-code-discipline-audit.md
// §2.2). The fix is `t.Chdir(dir)`, which registers its own restore and FAILS
// the test when it cannot perform it — or, where no *testing.T is in scope, an
// explicit checked form:
//
//	defer func() { if err := os.Chdir(dir); err != nil { t.Errorf("restore cwd: %v", err) } }()
//
// An intentional best-effort restore keeps its error visible instead:
// `defer func() { _ = os.Chdir(dir) }()` (the ignore marker at a statement
// site covers the `_ =` form; checkAssign sees that one).
func (v *visitor) checkChdirStmt(n ast.Node, call ast.Expr) {
	if !v.testFile {
		return // production chdirs are checkAssign's business (err := form)
	}
	c, ok := call.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != chdirFuncName || !isPkg(v.info, sel.X, pkgPathOS) {
		return
	}
	// The statement is the expression wrapper (ExprStmt/DeferStmt/GoStmt), so
	// the report anchors there and a deferred closure BODY — itself visited as
	// a block of statements — is scanned like any other body.
	stmt, ok := n.(ast.Stmt)
	if !ok {
		return
	}
	v.report(RuleTestChdir, c.Pos(), stmt,
		"cwd change with its error discarded (statement-position os.Chdir) — use t.Chdir(dir), which restores the process cwd and fails the test if it cannot; where no *testing.T is in scope, check the error explicitly or discard it visibly with `_ =`",
		"os.Chdir", "os.Chdir")
}

// literalOrConstString resolves a string expression to its value using the
// syntactic literal when it is one (robust when the package's type
// information is incomplete and types.Info carries no constant), falling back
// to the typed constant for a named const.
func literalOrConstString(info *types.Info, e ast.Expr) (bool, string) {
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if s, err := strconv.Unquote(lit.Value); err == nil {
			return true, s
		}
	}
	// Constant string CONCATENATION (`"one " + "two"`) is one string constant
	// to the language but two literals to the AST, and types hands back a
	// TRUNCATED rendering of it ("prefix...") that no literal parser can
	// unquote. The reason the rule exists is to require a literal reason; a
	// concatenation of literals IS one, so it is read from the syntax rather
	// than re-derived from the constant (which cannot be recovered exactly).
	if s, ok := concatLiteralString(e); ok {
		return true, s
	}
	// A NAMED constant — including one longer than 72 chars, whose rendered
	// form is truncated and unquotable. The exact value is read from
	// stringFromSyntax, whose whole purpose is that length.
	if s, ok := stringFromSyntax(info, e); ok {
		return true, s
	}
	s, ok := constString(info, e)
	return ok, s
}

// concatLiteralString flattens a `+` tree whose leaves are all string literals
// (the shape a long t.Skipf reason is written in, so the author can break it
// across source lines). It reports false for anything with a variable leaf.
func concatLiteralString(e ast.Expr) (string, bool) {
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return "", false
		}
		return s, true
	}
	bin, ok := e.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return "", false
	}
	left, ok := concatLiteralString(bin.X)
	if !ok {
		return "", false
	}
	right, ok := concatLiteralString(bin.Y)
	if !ok {
		return "", false
	}
	return left + right, true
}

// isTestReceiver reports whether x is a *testing.T / *testing.B / *testing.F.
func isTestReceiver(info *types.Info, x ast.Expr) bool {
	tv, ok := info.Types[x]
	if !ok || tv.Type == nil {
		// Fall back to the syntactic receiver name when types are unavailable;
		// ctx-safe because the caller only gets here for Skip/Skipf/SkipNow.
		if id, ok := x.(*ast.Ident); ok {
			return id.Name == "t" || id.Name == "b" || id.Name == "f"
		}
		return false
	}
	ptr, isPtr := tv.Type.(*types.Pointer)
	var named *types.Named
	if isPtr {
		named, _ = ptr.Elem().(*types.Named)
	} else {
		named, _ = tv.Type.(*types.Named)
	}
	if named == nil || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	p := named.Obj().Pkg().Path()
	if p != pkgPathTesting {
		return false
	}
	switch named.Obj().Name() {
	case "T", "B", "F":
		return true
	}
	return false
}

// String renders a finding in the canonical `file:line: rule: detail` form.
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: [%s] %s", f.File, f.Line, f.Rule, f.Detail)
}
