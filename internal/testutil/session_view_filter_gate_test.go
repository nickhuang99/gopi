package testutil

// A FIXTURE MUST NOT FILTER AN ALREADY-FILTERED SESSION VIEW — as a CHECK.
//
// WHY THIS FILE EXISTS (measured twice, on the same shape)
// -------------------------------------------------------
// Session.UserContents() applies sessionevent.IsHarnessInjected itself and
// returns the OPERATOR's turns. A fixture that then re-tests every element it
// returned with the same predicate partitions nothing: the injected half comes
// back permanently EMPTY while the operator half reads correctly. The assertion
// that reads the injected half is then red on a record that is exactly right,
// and the red names the product ("the run wrote no turn stamp at all") when the
// defect is in the read path. Measured 2026-10-12 TWICE:
//
//  1. cmd/agent/cli_lineedit_test.go's A4 census looped over
//     sess.UserContents() and re-tested each element with
//     sessionevent.IsHarnessInjected. The record held the two turn stamps the
//     run really wrote (internal/agent/clock.go's turnStampLine, one per
//     assembled request) and the fixture reported `harness turn stamps = 0,
//     model requests = 2` — the exact string in the bug report that opened this
//     family.
//  2. The SAME shape in three notify fixtures (cli_notify_deliver_test.go,
//     its idle sibling and cli_notify_duplicate_test.go), red for two days
//     across two trees before anyone read the read path, because the assertion
//     text — "the session record carries no user turn with the delivered
//     message — the model was told, the record does not show it" — reads like a
//     write-side defect.
//
// A principle is not effective; a check is. This file is the check for exactly
// that call shape: a RANGE over a Session user-row view whose body re-tests the
// element with a harness predicate.
//
// WHAT IT READS (the AST, never the text)
// ---------------------------------------
// go/parser only (stdlib, Rule 35): no type information, so no build of the
// scanned packages is needed and the gate cannot be defeated by a build tag.
// Two arms, because the shape arrives in two spellings:
//
//	arm 1  `for _, u := range sess.UserContents() { … IsHarnessInjected(u) … }`
//	       a range directly over the view CALL;
//	arm 2  `users := sess.UserContents(); … for _, u := range users { … }`
//	       the view bound to a variable FIRST, then ranged — one hop, which is
//	       the obfuscation a gate that only matched arm 1 would wave through.
//
// THE NAMES ARE DERIVED, NOT SPELLED HERE (Rule 14): the view set comes from
// internal/testutil/session.go's own methods on *Session that return []string
// and read user rows, and the predicate set from the functions NAMED IsHarness*
// in their defining files. A rename therefore follows automatically; there is no
// second spelling to drift.
//
// THE READ FLOOR, and it is the point of it: a scan that stops seeing the tree
// reports ZERO violations, which is indistinguishable from a green repo — so the
// walk asserts it found at least sessionViewMinCallSites view call sites, a
// number MEASURED at the commit this gate landed (37 across internal/, cmd/ and
// testsuite/). A traversal that finds nothing FAILS rather than reading green
// (Rule 15: a scan that never ran must not look like a pass).
//
// THE HONEST LIMIT. This gate reads a RANGE over a view inside ONE function
// body. It does not follow a view's elements through a struct field, a return
// value of a helper in another file, or a map. A fixture that stores
// UserContents() in a field and filters it in a test helper is not caught —
// which is a text/AST gate's ceiling, and it is stated here rather than
// glossed. The arm it does cover is the one that shipped, twice.

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
	// sessionViewSessionFile is the production file whose methods DEFINE the
	// user-row views — the single source the view set is read from (Rule 14).
	sessionViewSessionFile = "internal/testutil/session.go"

	// sessionViewMarkerFiles are the files whose IsHarness* functions DEFINE the
	// harness predicates. Two homes, one per side: the reader-side marker the
	// session format shares (sessionevent) and the fixture's own event reader.
	sessionViewMarkerFiles = "internal/sessionevent/harness.go,internal/testutil/session.go"

	// sessionViewScanRoots are the trees a fixture can live in. testsuite/ is
	// included because its chat fixtures are where this shape reaches a full
	// six-minute acceptance run before anyone notices.
	sessionViewScanRoots = "internal,cmd,testsuite"

	// sessionViewMinCallSites is the read floor (see the file comment): the
	// number of view call sites MEASURED in the test trees at the commit this
	// gate landed — 24 with the fix in place, 25 with the pre-fix shape restored
	// (the pre-fix range named a view that IS in the derived set, the fix names
	// AllUserContents, which is deliberately not). The floor sits below the
	// measured value so an ordinary fixture deletion does not fail the gate,
	// while a walk that sees no tree does.
	sessionViewMinCallSites = 20

	// sessionViewViewMethodPrefix is the receiver whose methods the view set is
	// read from: names collected are the []string readers on THIS type, so an
	// unrelated []string method elsewhere in session.go is not swept in.
	sessionViewViewMethodPrefix = "Session"

	// sessionViewMarkerPrefix is the prefix that makes a function a harness
	// predicate ("IsHarnessInjected", "IsHarnessUser"). Spelled once.
	sessionViewMarkerPrefix = "IsHarness"

	// sessionViewTestSuffix is what makes a file a fixture. Spelled once.
	sessionViewTestSuffix = "_test.go"

	// sessionViewEscape documents a range over a view that deliberately re-tests
	// the element, and carries its reason on the same line:
	// `//session-view-ok: <reason>`. Like every inline escape in this repo, a
	// marker without a reason is still a violation.
	sessionViewEscape = "//session-view-ok:"

	// sessionViewEscapeMinReason is the shortest accepted escape reason — a
	// marker with "ok" after it is not documentation.
	sessionViewEscapeMinReason = 10
)

// sessionViewViolation is one range-over-a-filtered-view site.
type sessionViewViolation struct {
	File   string
	Line   int
	View   string
	Marker string // the predicate that re-tests the element
}

func (v sessionViewViolation) String() string {
	return v.File + ":" + strconv.Itoa(v.Line) + " ranges over " + v.View + "() and re-tests each element with " + v.Marker
}

// TestSessionViewIsNeverFilteredASecondTime is the gate. It fails on any range
// over a Session user-row view whose body re-tests the element with a harness
// predicate, unless that line carries the escape marker and a reason.
func TestSessionViewIsNeverFilteredASecondTime(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate the module root: %v", err)
	}
	markers, err := sessionViewMarkerNames(root)
	if err != nil {
		t.Fatalf("derive the predicate set: %v", err)
	}
	views, err := sessionViewNames(root, markers)
	if err != nil {
		t.Fatalf("derive the view set: %v", err)
	}
	if len(views) == 0 {
		t.Fatalf("derived no filtered views from %s — the derivation is broken, not the repo", sessionViewSessionFile)
	}
	if len(markers) == 0 {
		t.Fatalf("derived no %s* predicate from %s — the derivation is broken, not the repo",
			sessionViewMarkerPrefix, sessionViewMarkerFiles)
	}
	t.Logf("views derived from %s: %v", sessionViewSessionFile, sortedKeys(views))
	t.Logf("harness predicates derived: %v", sortedKeys(markers))

	files := sessionViewTestFiles(t, root)
	sites := 0
	var found []sessionViewViolation
	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		// A cheap pre-filter: only a file that MENTIONS a view name can hold a
		// site. It never decides the verdict — the AST does — it only keeps the
		// parse off the 1600 test files that cannot match.
		if !mentionsAny(string(src), views) {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, rel, src, parser.ParseComments)
		if perr != nil {
			t.Fatalf("parse %s: %v", rel, perr)
		}
		lines := strings.Split(string(src), "\n")
		fileSites, fileFound := sessionViewScan(fset, f, lines, views, markers)
		sites += fileSites
		for i := range fileFound {
			fileFound[i].File = rel
			found = append(found, fileFound[i])
		}
	}

	// THE READ FLOOR, checked BEFORE the verdict: a walk that saw no tree must
	// not read as a clean repo (Rule 15).
	if sites < sessionViewMinCallSites {
		t.Fatalf("the scan saw %d view call sites across %s, floor is %d — the walk did not reach the tree it audits, so its verdict means nothing",
			sites, sessionViewScanRoots, sessionViewMinCallSites)
	}
	for _, v := range found {
		t.Errorf("%s — %s() has ALREADY applied the harness predicate and returns the operator's turns, so re-testing each element with %s partitions nothing: the injected half is permanently empty and an assertion on it is red on a record that is exactly right. Read HarnessUserContents()/AllUserContents() for that half (or the per-EVENT predicate via AllUserContents), never this view filtered again. (Measured twice: 2026-10-12, the A4 turn-stamp census and the three notify fixtures.)",
			v, v.View, v.Marker)
	}
	t.Logf("scanned %d test files, %d view call sites, %d re-filter site(s)", len(files), sites, len(found))
}

// sessionViewScan returns the number of view CALL SITES and the violations in
// one parsed file. Both arms live here so the red proof drives the same code
// the real tree does.
func sessionViewScan(fset *token.FileSet, f *ast.File, lines []string, views, markers map[string]bool) (int, []sessionViewViolation) {
	sites := 0
	// ARM 2's binding table: variable name → the view it was assigned from.
	bound := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if v, ok := sessionViewCallName(st.Rhs, views); ok {
				sites++
				for _, lhs := range st.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						bound[id.Name] = v
					}
				}
			}
		case *ast.ValueSpec:
			if v, ok := sessionViewCallName(st.Values, views); ok {
				sites++
				for _, name := range st.Names {
					bound[name.Name] = v
				}
			}
		}
		return true
	})

	var found []sessionViewViolation
	sawRange := map[token.Pos]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		rng, ok := n.(*ast.RangeStmt)
		if !ok || sawRange[rng.Pos()] {
			return true
		}
		sawRange[rng.Pos()] = true
		view, direct := sessionViewCallName([]ast.Expr{rng.X}, views)
		if direct {
			sites++
		} else if id, ok := rng.X.(*ast.Ident); ok {
			view = bound[id.Name]
		}
		if view == "" {
			return true
		}
		marker := sessionMarkerCall(rng.Body, markers)
		if marker == "" {
			return true
		}
		line := fset.Position(rng.Pos()).Line
		if hasEscape(lines, line) {
			return true
		}
		found = append(found, sessionViewViolation{Line: line, View: view, Marker: marker})
		return true
	})
	return sites, found
}

// sessionViewCallName returns the view name when expr list is a call of one of
// the view methods (the FIRST expression only — a multi-value assignment is not
// one of these calls).
func sessionViewCallName(exprs []ast.Expr, views map[string]bool) (string, bool) {
	if len(exprs) == 0 {
		return "", false
	}
	call, ok := exprs[0].(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !views[sel.Sel.Name] {
		return "", false
	}
	return sel.Sel.Name, true
}

// sessionMarkerCall returns the harness predicate called anywhere inside body,
// or "" when the body re-tests nothing.
func sessionMarkerCall(body *ast.BlockStmt, markers map[string]bool) string {
	if body == nil {
		return ""
	}
	name := ""
	ast.Inspect(body, func(n ast.Node) bool {
		if name != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if markers[fun.Sel.Name] {
				name = fun.Sel.Name
			}
		case *ast.Ident:
			if markers[fun.Name] {
				name = fun.Name
			}
		}
		return true
	})
	return name
}

// hasEscape reports whether the line carries a documented escape: the marker
// plus a reason long enough to be one. The marker may sit anywhere on the line
// (before the range, or in a trailing comment after it), because the line IS
// the escape's scope — this is a one-line exemption, not a block one.
func hasEscape(lines []string, line int) bool {
	if line < 1 || line > len(lines) {
		return false
	}
	i := strings.Index(lines[line-1], sessionViewEscape)
	if i < 0 {
		return false
	}
	rest := lines[line-1][i+len(sessionViewEscape):]
	// The reason runs to the end of the line; a trailing `*/` or another
	// comment marker is not part of it, so trim the punctuation a reason may
	// be wrapped in.
	if j := strings.IndexAny(rest, "*/"); j >= 0 {
		rest = rest[:j]
	}
	return len(strings.TrimSpace(rest)) >= sessionViewEscapeMinReason
}

// sessionViewNames reads the FILTERED view set out of the methods that define
// it in internal/testutil/session.go: a []string reader on *Session whose body
// ALREADY CALLS a harness predicate. That is the property the rule is about —
// "a view that has already applied the predicate" — and deriving it that way
// (rather than by name) keeps the set honest in both directions:
//
//   - UserContents (operator turns) and HarnessUserContents (injections) both
//     call the predicate, so re-filtering either one partitions nothing;
//   - AllUserContents calls NO predicate — it returns every user row — so
//     splitting IT with the predicate is the FIX, not the defect, and it must
//     not be reported. A name list would have had to spell this exception.
func sessionViewNames(root string, markers map[string]bool) (map[string]bool, error) {
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sessionViewSessionFile)))
	if err != nil {
		return nil, err
	}
	f, err := parser.ParseFile(token.NewFileSet(), sessionViewSessionFile, src, 0)
	if err != nil {
		return nil, err
	}
	views := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
			return true
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			return true
		}
		if id, ok := star.X.(*ast.Ident); !ok || id.Name != sessionViewViewMethodPrefix {
			return true
		}
		if !returnsStringSlice(fn.Type) || fn.Body == nil || sessionMarkerCall(fn.Body, markers) == "" {
			return true
		}
		views[fn.Name.Name] = true
		return true
	})
	return views, nil
}

// sessionViewMarkerNames reads the harness predicate names out of the files
// that DEFINE them — no spelling of either name lives in this gate. A predicate
// is a function NAMED sessionViewMarkerPrefix* in either home, PACKAGE-LEVEL or
// a METHOD (internal/testutil/session.go's Event.IsHarnessUser is a method, and
// a method-only derivation missed it — the measured reason the teeth test
// caught).
func sessionViewMarkerNames(root string) (map[string]bool, error) {
	markers := map[string]bool{}
	for _, rel := range strings.Split(sessionViewMarkerFiles, ",") {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(token.NewFileSet(), rel, src, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, sessionViewMarkerPrefix) {
				return true
			}
			markers[fn.Name.Name] = true
			return true
		})
	}
	return markers, nil
}

// returnsStringSlice reports whether the function's result is exactly []string.
func returnsStringSlice(ft *ast.FuncType) bool {
	if ft.Results == nil || len(ft.Results.List) != 1 {
		return false
	}
	arr, ok := ft.Results.List[0].Type.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	id, ok := arr.Elt.(*ast.Ident)
	return ok && id.Name == "string"
}

// sessionViewTestFiles lists every test file under the scanned roots.
func sessionViewTestFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, r := range strings.Split(sessionViewScanRoots, ",") {
		base := filepath.Join(root, filepath.FromSlash(r))
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, sessionViewTestSuffix) {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", r, err)
		}
	}
	sort.Strings(out)
	return out
}

// mentionsAny is the pre-filter (never the verdict): does the text name a view?
func mentionsAny(src string, views map[string]bool) bool {
	for v := range views {
		if strings.Contains(src, v) {
			return true
		}
	}
	return false
}

// sortedKeys renders a name set deterministically for the log line.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestSessionViewGateHasTeeth drives the SAME scan the real tree does, over two
// synthetic sources: the shape that shipped (both arms) must be reported, and a
// legitimate consumer of the two-halves view must not be. Without the second
// half a gate that reported EVERYTHING would look green here.
func TestSessionViewGateHasTeeth(t *testing.T) {
	markers, err := sessionViewMarkerNames(repoRootOrFatal(t))
	if err != nil {
		t.Fatalf("derive the predicate set: %v", err)
	}
	views, err := sessionViewNames(repoRootOrFatal(t), markers)
	if err != nil {
		t.Fatalf("derive the view set: %v", err)
	}

	for _, tc := range []struct {
		name    string
		src     string
		want    int
		comment string
	}{
		{
			name: "arm 1 — range over the view, re-tested with the predicate",
			src: `package cmd

import "testing"

func TestShipped(t *testing.T) {
	var operator, injected []string
	for _, u := range sess.UserContents() {
		if sessionevent.IsHarnessInjected(u) {
			injected = append(injected, u)
			continue
		}
		operator = append(operator, u)
	}
	_ = operator
	_ = injected
}
`,
			want:    1,
			comment: "the A4 census shape, verbatim",
		},
		{
			name: "arm 2 — the view bound to a variable, then ranged",
			src: `package cmd

import "testing"

func TestBound(t *testing.T) {
	users := sess.UserContents()
	var injected []string
	for _, u := range users {
		if event.IsHarnessUser() {
			injected = append(injected, u)
		}
	}
	_ = injected
}
`,
			want:    1,
			comment: "one hop — the spelling a call-only gate would wave through",
		},
		{
			name: "legitimate — the unfiltered view is what gets partitioned",
			src: `package cmd

import "testing"

func TestLegit(t *testing.T) {
	var operator, injected []string
	for _, u := range sess.AllUserContents() {
		if sessionevent.IsHarnessInjected(u) {
			injected = append(injected, u)
			continue
		}
		operator = append(operator, u)
	}
	_ = operator
	_ = injected
}
`,
			want:    0,
			comment: "AllUserContents is the UNFILTERED view — splitting it is the fix",
		},
		{
			name: "legitimate — a range that re-tests nothing",
			src: `package cmd

import "testing"

func TestPlain(t *testing.T) {
	for _, u := range sess.UserContents() {
		if u == "hello there" {
			return
		}
	}
}
`,
			want:    0,
			comment: "no harness predicate in the body",
		},
		{
			name: "documented escape",
			src: `package cmd

import "testing"

func TestEscaped(t *testing.T) {
	for _, u := range sess.HarnessUserContents() { //session-view-ok: this loop counts only, the predicate is asserted per row
		if sessionevent.IsHarnessInjected(u) {
			return
		}
	}
}
`,
			want:    0,
			comment: "the marker plus a reason is the only accepted exemption",
		},
		{
			name: "escape without a reason is still a violation",
			src: `package cmd

import "testing"

func TestEscapeWithNoReason(t *testing.T) {
	for _, u := range sess.HarnessUserContents() { //session-view-ok: ok
		if sessionevent.IsHarnessInjected(u) {
			return
		}
	}
}
`,
			want:    1,
			comment: "\"ok\" after the marker is not documentation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, tc.name+".go", tc.src, parser.ParseComments)
			if perr != nil {
				t.Fatalf("parse fixture: %v", perr)
			}
			_, found := sessionViewScan(fset, f, strings.Split(tc.src, "\n"), views, markers)
			if len(found) != tc.want {
				t.Fatalf("%s: %d violation(s), want %d — %v\n%s", tc.comment, len(found), tc.want, found, tc.src)
			}
		})
	}
}

// repoRootOrFatal is the module root, for the teeth test's own derivations.
func repoRootOrFatal(t *testing.T) string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate the module root: %v", err)
	}
	return root
}
