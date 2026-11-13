package standardgate

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

// session_view_gate_test.go — Rule 13/14 over the SESSION-RECORD READ PATH: the
// accessors of internal/testutil's Session partition the record's user rows
// exactly once, and no fixture may re-partition them.
//
// WHY THIS IS A GATE AND NOT A PARAGRAPH. The same defect has now been written
// three times, in three different files, by three different runs — and each time
// it made an INTACT product look broken: the assertion read a slice the row
// could never enter, so the failure text pointed at the record instead of at the
// fixture, and the diagnosis cost a whole task session.
//
//	(1) cmd/agent/cli_lineedit_test.go — ranged UserContents and applied
//	    sessionevent.IsHarnessInjected to each element. UserContents had just
//	    become the OPERATOR-ONLY view, so the "injected" half was empty by
//	    construction and the fixture printed `harness turn stamps = 0` against a
//	    record that held two. Measured 2026-10-12.
//	(2) cmd/agent/cli_notify_deliver_test.go + ..._idle_test.go — searched
//	    UserContents for the sentence `gopi notify --session … --message …`
//	    delivers. That sentence is recorded as a row the HARNESS injected
//	    (internal/agent/chat_job_notice_turn.go sessionMessageFact puts
//	    sessionevent.HarnessMessagePrefix on it), so the search was false by
//	    construction on a correct record.
//	(3) cmd/agent/cli_notify_duplicate_test.go — the same read path, reported as
//	    `the session record carries 0 user message(s) with the delivered text`
//	    while the raw JSONL carried exactly one such row (measured this run:
//	    a raw line dump of the record beside the `operator=[]` reading).
//
// The third case is why the class is worth a gate rather than another comment:
// (1) and (2) were already fixed ON THE BRANCH, and (3) was still red — a fix
// per file means the next file is red again.
//
// The two classes below are the ones that are DECIDABLE from the source without
// executing anything. What is deliberately NOT here: "did this fixture choose
// the right view for the row's provenance?" is not statically decidable in
// general (the row's provenance is a fact about the writer), so the notify
// family keeps its own behavioural guard — those fixtures go red the moment the
// read path regresses. This gate covers the shapes that stay GREEN-or-plausibly-
// red without any behavioural signal.

const (
	// sessionViewSourceRel is the ONE file that defines the session fixture's
	// accessors. The gate derives the view inventory from it rather than naming
	// the views itself: a fourth accessor added there is classified by the same
	// rule as the three below, instead of being invisible to this gate.
	sessionViewSourceRel = "internal/testutil/session.go"
	// sessionViewTypeName is the receiver whose methods are accessors.
	sessionViewTypeName = "Session"
	// sessionViewEscape is the reviewed-exception marker (Rule 13/14's inline
	// escape convention): a hit whose loop line — or the line above it — carries
	// it is reported as an escape, not as a violation.
	sessionViewEscape = "session-view-refilter-ok:"
	// stringsPackageName / stringContainsFunc name the search the fixtures use
	// to look for a row's text ("does the record carry this sentence?").
	stringsPackageName = "strings"
	stringContainsFunc = "Contains"
	// notifyMessageFlagIdent is the flag constant whose argument IS the text a
	// caller handed this session through `gopi notify --session … --message …`.
	// A file that passes it has, by definition, a delivered sentence — and that
	// sentence is recorded on a harness row.
	notifyMessageFlagIdent = "notifyMessageFlag"
)

// Read floors. A scan that finds no views, or reads a fragment of the tree, must
// FAIL rather than pass: a green gate over nothing is the failure mode these
// floors exist for (Rule 15 — a scan that found nothing is not a scan that
// passed).
const (
	sessionViewMinScannedFiles = 100
	sessionViewMinViews        = 2
)

// sessionViewRoots are the trees the scan covers: every package that reads a
// session record in a test. testsuite/ is included because it holds the
// PTY-level fixtures of the same family (testsuite/chat_notice_turn_pty_test.go
// reads both views by name).
var sessionViewRoots = []string{"internal", "cmd", "testsuite"}

// harnessPredicateNames are the production predicates that decide which user
// rows are the harness's. A view whose guard calls one of them is a PARTITION; a
// view that does not is the unfiltered union.
var harnessPredicateNames = []string{"IsHarnessUser", "IsHarnessInjected"}

// sessionViewKind classifies an accessor by what its OWN guard does.
type sessionViewKind int

const (
	// viewAll is the unfiltered union: every user row, harness rows included.
	viewAll sessionViewKind = iota
	// viewHarnessOnly keeps harness rows and drops the operator's.
	viewHarnessOnly
	// viewExcludingHarness keeps the operator's turns and drops harness rows.
	viewExcludingHarness
)

// sessionViewHit is one violation, rendered for a human.
type sessionViewHit struct {
	file    string
	line    int
	snippet string
	why     string
}

// sessionViewsInSource derives the accessor inventory from internal/testutil's
// source: a method on Session counts as a view when its body consults a harness
// predicate (directly, or through another view it calls), and its KIND comes
// from the polarity of that consultation.
func sessionViewsInSource(src []byte) (map[string]sessionViewKind, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, sessionViewSourceRel, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	views := map[string]sessionViewKind{}
	bodies := map[string]*ast.BlockStmt{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Body == nil || !receiverIsSessionView(fn.Recv) {
			continue
		}
		bodies[fn.Name.Name] = fn.Body
		if kind, uses := viewKindOf(fn.Body); uses {
			views[fn.Name.Name] = kind
		}
	}
	// A view that DELEGATES to another view (e.g. a convenience alias) inherits
	// its kind, so the inventory cannot be evaded by one indirection.
	for changed := true; changed; {
		changed = false
		for name, body := range bodies {
			if _, known := views[name]; known {
				continue
			}
			for _, called := range viewCalls(body) {
				if kind, ok := views[called]; ok {
					views[name] = kind
					changed = true
					break
				}
			}
		}
	}
	return views, nil
}

func receiverIsSessionView(recv *ast.FieldList) bool {
	for _, fl := range recv.List {
		if _, ok := fl.Type.(*ast.StarExpr); !ok {
			continue
		}
		if ident, ok := fl.Type.(*ast.StarExpr).X.(*ast.Ident); ok && ident.Name == sessionViewTypeName {
			return true
		}
	}
	return false
}

// viewKindOf reports whether body consults a harness predicate, and with which
// polarity: a guard that SKIPS when the predicate is true drops harness rows
// (viewExcludingHarness); one that skips when it is NEGATED keeps only them
// (viewHarnessOnly).
func viewKindOf(body *ast.BlockStmt) (sessionViewKind, bool) {
	kind, found := viewAll, false
	ast.Inspect(body, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok || ifStmt.Cond == nil {
			return true
		}
		if !mentionsHarnessPredicate(ifStmt.Cond) {
			return true
		}
		found = true
		if predicateNegated(ifStmt.Cond) {
			kind = viewHarnessOnly
			return true
		}
		kind = viewExcludingHarness
		return true
	})
	return kind, found
}

func mentionsHarnessPredicate(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && harnessPredicateName(call) != "" {
			found = true
		}
		return true
	})
	return found
}

func predicateNegated(expr ast.Expr) bool {
	negated := false
	ast.Inspect(expr, func(n ast.Node) bool {
		unary, ok := n.(*ast.UnaryExpr)
		if !ok || unary.Op != token.NOT {
			return true
		}
		ast.Inspect(unary.X, func(inner ast.Node) bool {
			if call, ok := inner.(*ast.CallExpr); ok && harnessPredicateName(call) != "" {
				negated = true
			}
			return true
		})
		return true
	})
	return negated
}

// harnessPredicateName returns the called predicate's name when call is one of
// them (ev.IsHarnessUser() / sessionevent.IsHarnessInjected(x)).
func harnessPredicateName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	for _, name := range harnessPredicateNames {
		if sel.Sel.Name == name {
			return name
		}
	}
	return ""
}

// viewCalls returns the method names invoked as `<recv>.<Name>(…)` in body —
// the delegation seam of viewKindOf's closure.
func viewCalls(body *ast.BlockStmt) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			out = append(out, sel.Sel.Name)
		}
		return true
	})
	return out
}

// sessionViewHits scans ONE file's source and returns its violations of both
// classes. views is the inventory derived from the fixture's own source.
//
// CLASS 1 — A FILTERED VIEW IS NOT A RAW RECORD. A loop over a view whose own
// guard already applies the harness predicate, whose body applies that same
// predicate again, partitions nothing: the half it is looking for comes back
// permanently empty. Splitting the union IS legitimate — that is what
// AllUserContents exists for, and cli_lineedit_test.go does exactly that — so
// the class is restricted to the two FILTERED views.
//
// CLASS 2 — THE DELIVERED SENTENCE IS A HARNESS ROW. A file that hands a
// constant to the `--message` flag of `gopi notify` has a text the product
// records through sessionMessageFact, i.e. under HarnessMessagePrefix. Searching
// such a text in the operator-only view is false by construction on a correct
// record — the assertion is about the RECORD, and the record holds the row.
func sessionViewHits(rel string, src []byte, views map[string]sessionViewKind) ([]sessionViewHit, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(src), "\n")
	lineOf := func(pos token.Pos) int { return fset.Position(pos).Line }
	escaped := func(loopLine int) bool {
		for _, off := range []int{0, -1} {
			idx := loopLine - 1 + off
			if idx < 0 || idx >= len(lines) {
				continue
			}
			if strings.Contains(lines[idx], sessionViewEscape) {
				return true
			}
		}
		return false
	}
	delivered := deliveredTextIdents(f)
	var hits []sessionViewHit

	ast.Inspect(f, func(n ast.Node) bool {
		rng, ok := n.(*ast.RangeStmt)
		if !ok || rng.Body == nil {
			return true
		}
		view, ok := rangedViewName(rng.X, views)
		if !ok {
			return true
		}
		kind := views[view]
		line := lineOf(rng.Pos())
		if escaped(line) {
			return true
		}
		snippet := ""
		if line-1 < len(lines) {
			snippet = strings.TrimSpace(lines[line-1])
		}
		if kind != viewAll && bodyMentionsHarnessPredicate(rng.Body) {
			hits = append(hits, sessionViewHit{rel, line, snippet, class1Why(view)})
		}
		if kind == viewExcludingHarness && len(delivered) > 0 && bodySearchesFor(rng.Body, delivered) {
			hits = append(hits, sessionViewHit{rel, line, snippet, class2Why(view)})
		}
		return true
	})
	return hits, nil
}

func class1Why(view string) string {
	return "the loop ranges over " + view + " — a view whose own guard already applies " +
		strings.Join(harnessPredicateNames, "/") + " — and applies that predicate again to each element: " +
		"the half it looks for is empty by construction while the record holds it. " +
		"Split the UNION instead (AllUserContents, internal/testutil/session.go)"
}

func class2Why(view string) string {
	return "the loop ranges over " + view + " (the view that EXCLUDES harness rows) while searching for the " +
		"text this file delivers with `" + notifyMessageFlagIdent + "`: the product records a delivered " +
		"sentence on a HARNESS row (internal/agent/chat_job_notice_turn.go sessionMessageFact), so the " +
		"search is false on a correct record. Read a view that includes harness rows " +
		"(HarnessUserContents / AllUserContents)"
}

// rangedViewName reports the view a range expression iterates, when it is a
// call of one of the fixture's accessors.
func rangedViewName(x ast.Expr, views map[string]sessionViewKind) (string, bool) {
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if _, ok := views[sel.Sel.Name]; !ok {
		return "", false
	}
	return sel.Sel.Name, true
}

func bodyMentionsHarnessPredicate(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && harnessPredicateName(call) != "" {
			found = true
		}
		return true
	})
	return found
}

// bodySearchesFor reports whether body performs a strings.Contains search whose
// haystack/needle mentions one of the delivered-text identifiers.
func bodySearchesFor(body *ast.BlockStmt, idents map[string]bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != stringContainsFunc {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != stringsPackageName {
			return true
		}
		for _, arg := range call.Args {
			if ident, ok := arg.(*ast.Ident); ok && idents[ident.Name] {
				found = true
			}
		}
		return true
	})
	return found
}

// deliveredTextIdents returns the package-level constants of f that are handed
// to the delivery verb, i.e. that appear as an argument beside
// notifyMessageFlagIdent in one call. Package-level declaration (rather than a
// local or a parameter) is what separates "the sentence this file delivers" from
// the surrounding plumbing (`env`, `sessionID`, the *testing.T).
func deliveredTextIdents(f *ast.File) map[string]bool {
	declared := packageLevelNames(f)
	delivered := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		hasFlag := false
		for _, arg := range call.Args {
			if ident, ok := arg.(*ast.Ident); ok && ident.Name == notifyMessageFlagIdent {
				hasFlag = true
			}
		}
		if !hasFlag {
			return true
		}
		for _, arg := range call.Args {
			if ident, ok := arg.(*ast.Ident); ok && declared[ident.Name] {
				delivered[ident.Name] = true
			}
		}
		return true
	})
	return delivered
}

func packageLevelNames(f *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, d := range f.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				out[name.Name] = true
			}
		}
	}
	return out
}

// sessionViewScan runs the gate over the real tree: every test file under
// sessionViewRoots, parsed and checked against the inventory derived from
// internal/testutil/session.go.
func sessionViewScan(t *testing.T) ([]sessionViewHit, int, int) {
	t.Helper()
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sessionViewSourceRel)))
	if err != nil {
		t.Fatalf("read %s: %v — the gate cannot inventory the fixture's views", sessionViewSourceRel, err)
	}
	views, err := sessionViewsInSource(src)
	if err != nil {
		t.Fatalf("parse %s: %v", sessionViewSourceRel, err)
	}
	if len(views) < sessionViewMinViews {
		t.Fatalf("inventory holds %d view(s) (%v) — the derivation is broken, not the tree", len(views), views)
	}
	files := goFilesUnder(t, root, sessionViewRoots, true)
	if len(files) < sessionViewMinScannedFiles {
		t.Fatalf("scan found only %d test file(s) under %v — the scan is broken, not the tree (refusing to pass blind)",
			len(files), sessionViewRoots)
	}
	var hits []sessionViewHit
	for _, rel := range files {
		body, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		fileHits, herr := sessionViewHits(rel, body, views)
		if herr != nil {
			t.Fatalf("parse %s: %v", rel, herr)
		}
		hits = append(hits, fileHits...)
	}
	return hits, len(files), len(views)
}

// TestSessionRecordViewsAreFilteredOnce — the real-tree half. Every test file in
// the three trees is checked; a violation names the file, the line and the
// accessor the loop should have read instead.
func TestSessionRecordViewsAreFilteredOnce(t *testing.T) {
	hits, scanned, views := sessionViewScan(t)
	if len(hits) > 0 {
		var b strings.Builder
		for _, h := range hits {
			b.WriteString("\n" + h.file + ":" + strconv.Itoa(h.line) + ": " + h.snippet + "\n    " + h.why)
		}
		t.Errorf("%d session-record read path(s) re-partition a view the fixture already partitioned (Rule 13/14):%s",
			len(hits), b.String())
	}
	t.Logf("session-record read path: %d test file(s) scanned, %d view(s) classified from %s, %d violation(s)",
		scanned, views, sessionViewSourceRel, len(hits))
}

// TestSessionViewRefilterGateRedProof — the predicate half, with the two
// readings that make the gate load-bearing:
//
//	RED:    both classes fire on a synthetic source that reproduces the two
//	        measured defects, and the fixture's inventory classifies the three
//	        accessors as the distinct kinds the classes key on.
//	NOT-VACUOUS: the legitimate split — the ALL view filtered by the predicate,
//	        which cmd/agent/cli_lineedit_test.go does — is NOT reported, so the
//	        gate cannot be satisfied by banning the read path altogether.
func TestSessionViewRefilterGateRedProof(t *testing.T) {
	views, err := sessionViewsInSource([]byte(sessionViewSourceRelProbe))
	if err != nil {
		t.Fatalf("inventory the probe fixture: %v", err)
	}
	if views["UserContents"] != viewExcludingHarness {
		t.Errorf("UserContents classified as %v, want viewExcludingHarness — the polarity reading is what decides both classes", views["UserContents"])
	}
	if views["HarnessUserContents"] != viewHarnessOnly {
		t.Errorf("HarnessUserContents classified as %v, want viewHarnessOnly", views["HarnessUserContents"])
	}
	if views["AllUserContents"] != viewAll {
		t.Errorf("AllUserContents classified as %v, want viewAll (unfiltered)", views["AllUserContents"])
	}

	redCases := []struct {
		name string
		src  string
		want string
	}{
		{"re-filter a filtered view", probeRefilteredView, "empty by construction"},
		{"delivered text in the operator-only view", probeDeliveredInOperatorView, "EXCLUDES"},
	}
	for _, tc := range redCases {
		hits, herr := sessionViewHits(tc.name+".go", []byte(tc.src), views)
		if herr != nil {
			t.Fatalf("parse probe %s: %v", tc.name, herr)
		}
		if len(hits) != 1 || !strings.Contains(hits[0].why, tc.want) {
			t.Errorf("probe %q: hits = %+v, want exactly 1 naming %q — the gate does not bite on the defect it exists for", tc.name, hits, tc.want)
		}
	}

	cleanCases := []struct {
		name string
		src  string
	}{
		{"split the union with the predicate (cli_lineedit_test.go's legitimate read)", probeUnionSplit},
		{"delivered text in the harness view", probeDeliveredInHarnessView},
		{"reviewed escape", probeEscapedHit},
	}
	for _, tc := range cleanCases {
		hits, herr := sessionViewHits(tc.name+".go", []byte(tc.src), views)
		if herr != nil {
			t.Fatalf("parse probe %s: %v", tc.name, herr)
		}
		if len(hits) != 0 {
			t.Errorf("probe %q: hits = %+v, want none — a gate that also refuses the correct read is not usable", tc.name, hits)
		}
	}
}

// sessionViewSourceRelProbe is a miniature of internal/testutil/session.go: the
// three accessors with the exact guards the real ones carry, so the inventory
// deriver is exercised end to end (not mocked).
const sessionViewSourceRelProbe = `package testutil

type Event struct {
	Type    string
	Content string
}

type Session struct {
	Events []Event
}

// UserContents returns the operator's turns only.
func (s *Session) UserContents() []string {
	var out []string
	for _, ev := range s.Events {
		if ev.Type != "user" || ev.IsHarnessUser() {
			continue
		}
		out = append(out, ev.Content)
	}
	return out
}

// HarnessUserContents returns the harness rows only.
func (s *Session) HarnessUserContents() []string {
	var out []string
	for _, ev := range s.Events {
		if !ev.IsHarnessUser() {
			continue
		}
		out = append(out, ev.Content)
	}
	return out
}

// AllUserContents returns every user row.
func (s *Session) AllUserContents() []string {
	var out []string
	for _, ev := range s.Events {
		if ev.Type != "user" {
			continue
		}
		out = append(out, ev.Content)
	}
	return out
}
`

const probeRefilteredView = `package main

const probeText = "PROBE-TEXT"

func probe(sess *testutil.Session) {
	for _, c := range sess.UserContents() {
		if sessionevent.IsHarnessInjected(c) {
			_ = c
		}
	}
}
`

const probeDeliveredInOperatorView = `package main

const (
	notifyMessageFlag = "--message"
	notifyProbeText   = "PROBE-DELIVERED"
)

func probe(t *testing.T, sess *testutil.Session) {
	_, _, _ = runNotifyDeliverArgv(t, notifyMessageFlag, notifyProbeText)
	for _, c := range sess.UserContents() {
		if strings.Contains(c, notifyProbeText) {
			t.Errorf("carries the delivered text")
		}
	}
}
`

const probeUnionSplit = `package main

func probe(sess *testutil.Session) {
	for _, u := range sess.AllUserContents() {
		if sessionevent.IsHarnessInjected(u) {
			continue
		}
		_ = u
	}
}
`

const probeDeliveredInHarnessView = `package main

const (
	notifyMessageFlag = "--message"
	notifyProbeText   = "PROBE-DELIVERED"
)

func probe(t *testing.T, sess *testutil.Session) {
	_, _, _ = runNotifyDeliverArgv(t, notifyMessageFlag, notifyProbeText)
	for _, c := range sess.HarnessUserContents() {
		if strings.Contains(c, notifyProbeText) {
			t.Errorf("carries the delivered text")
		}
	}
}
`

const probeEscapedHit = `package main

const (
	notifyMessageFlag = "--message"
	notifyProbeText   = "PROBE-DELIVERED"
)

func probe(t *testing.T, sess *testutil.Session) {
	_, _, _ = runNotifyDeliverArgv(t, notifyMessageFlag, notifyProbeText)
	// session-view-refilter-ok: reviewed reason
	for _, c := range sess.UserContents() {
		if strings.Contains(c, notifyProbeText) {
			t.Errorf("carries the delivered text")
		}
	}
}
`
