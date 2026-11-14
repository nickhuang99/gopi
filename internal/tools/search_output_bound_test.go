package tools

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

// search_output_bound_test.go — the regression guards for the defect this file's
// production change fixes: internal/tools/search.go's ripgrep leg used
// cmd.CombinedOutput(), so rg's ENTIRE stdout was read into one buffer before a
// single line was parsed. searchOutputLineCeiling bounded ONE line; nothing
// bounded the sum. Measured at the cut point (6157455f9ee4…, both binaries, one
// 40 MiB fixture of ten 4 MB rows, one query — the numbers are in this task's
// report): peak RSS 204,800 KB before, 12,288 KB after.
//
// THREE ARMS, and each answers a question the others cannot:
//
//  1. BEHAVIOUR — the production entry (KeywordSearch) still serves a fixture
//     whose matched output is tens of megabytes, with the right hit count, the
//     right line numbers and the per-line ceiling intact. A streaming rewrite
//     that broke large rows would fail here.
//  2. THE CEILING — a search whose backend output passes the bound returns the
//     NAMED refusal with no hits, never a silently truncated set (Rule 15).
//  3. THE SHAPE — the ripgrep leg must not collect the child's stdout into one
//     buffer again. This is the anti-regression guard for the exact defect, and
//     it is an AST reading rather than a grep, so the production comments may
//     keep naming the old call without tripping it.
//
// WHY ARM 2 SUPPLIES ITS OWN CEILING instead of driving searchOutputTotalCeiling
// (256 MiB): tripping the production bound through KeywordSearch would mean
// materialising a quarter of a gigabyte of matched output on every suite run
// (Rule 27: a test may not manufacture that kind of cost). The function under
// test is the same one the production entry calls — the bound is a parameter
// there and only there — and arm 3 pins that the production entry passes the
// constant, so the two halves together cover the whole path.

// searchBoundFixtureRows / …Files size arm 1's fixture: files × rows lines of
// rowBytes each, which is the matched output rg must emit for `query`.
//
// DELIVERY-ROUND NOTE (2/3). These arms arrived with the streaming change's own
// commit; the integration branch then landed the SAME fix independently
// (ceb1436e, searchOutputByteCeiling/SearchCap) and this file was re-pointed at
// that implementation during the rebase rather than kept as a second copy of it.
// What changed and why is stated arm by arm below:
//   - arm 2 now asserts the INTEGRATION branch's contract (partial hits + a
//     SearchCap that names the cut) instead of a hard refusal carrying its own
//     error type. Both are Rule 15 answers; only one of them is in the tree now,
//     and a second error type for the same fact would be a second definition
//     (Rule 14).
//   - arm 3 judges the one streaming leg that exists
//     (keywordSearchViaRipgrepCapped) and the one named constant the production
//     entry passes (searchOutputByteCeiling).
//   - arm 4 is this branch's own increment and does not exist upstream at all:
//     the retained hit's FIELDS must not alias the backend's row.
const (
	searchBoundFixtureFiles = 6
	searchBoundFixtureRows  = 1
	searchBoundRowBytes     = 4 << 20
	// searchBoundSampledRows is how many hits arm 1 asserts on: one per file,
	// because each file is ONE enormous row.
	searchBoundSampledRows = searchBoundFixtureFiles
	// searchBoundTestCeiling is arm 2's fixture-sized ceiling — one MiB, so a
	// 12 MiB output stream trips it after a handful of rows.
	searchBoundTestCeiling = 1 << 20
	// searchBoundLargestRealOutputMeasured is the largest genuine ripgrep output
	// observed on this host (a session-file query, 38,874,644 bytes; the same
	// reading is quoted in search.go above searchOutputByteCeiling). The production
	// ceiling must stay ABOVE it, or queries that answer correctly today would begin
	// being truncated — so it is the floor the arm below holds the constant to.
	searchBoundLargestRealOutputMeasured = 38_874_644
	// searchBoundCeilingFixtureFiles sizes arm 2's fixture so its output is
	// 12× the ceiling above.
	searchBoundCeilingFixtureFiles = 12
)

// writeSearchBoundFixture writes `files` files, each holding `rows` lines made of
// the query marker followed by rowBytes-1 padding bytes. The padding keeps every
// line under searchOutputLineCeiling, so what these arms measure is the TOTAL,
// never the per-line cap (that one has its own guard, session_search_test.go).
func writeSearchBoundFixture(t *testing.T, files, rows int, query string) string {
	t.Helper()
	dir := t.TempDir()
	padding := strings.Repeat("x", searchBoundRowBytes-1)
	for i := 0; i < files; i++ {
		var b strings.Builder
		for r := 0; r < rows; r++ {
			b.WriteString(query)
			b.WriteString(": ")
			b.WriteString(padding)
			b.WriteString("\n")
		}
		name := filepath.Join(dir, fmt.Sprintf("part-%02d.jsonl", i))
		if err := os.WriteFile(name, []byte(b.String()), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	return dir
}

// TestKeywordSearchStreamsAHugeOutput is arm 1: the production entry point over a
// fixture whose rg output is tens of megabytes. It asserts the ANSWER (hit count,
// line numbers, snippets trimmed by buildSnippet, nothing dropped) — a streaming
// rewrite that lost rows past some internal buffer would fail it — and it is the
// fixture whose real-binary peak RSS the task's A/B measures.
func TestKeywordSearchStreamsAHugeOutput(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	const query = "STREAM_MARKER"
	dir := writeSearchBoundFixture(t, searchBoundFixtureFiles, searchBoundFixtureRows, query)

	hits, dropped, searchCap, err := KeywordSearch(SearchScope{RootPath: dir}, query)
	if err != nil {
		t.Fatalf("KeywordSearch over a %d MiB-of-output fixture returned an error: %v",
			searchBoundFixtureFiles*searchBoundRowBytes>>20, err)
	}
	if len(hits) != searchBoundSampledRows {
		t.Fatalf("hits = %d, want %d (one per fixture file): stream lost rows", len(hits), searchBoundSampledRows)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 (no result cap was set)", dropped)
	}
	// The ordinary case must be untouched: this fixture's output is well under the
	// production ceiling, so nothing was cut and the caller's rendering is exactly
	// what it was before the bound existed.
	if searchCap.Stopped {
		t.Errorf("SearchCap.Stopped = true on a %d MiB output under the %d-byte ceiling — a complete answer is being reported as partial",
			searchBoundFixtureFiles*searchBoundRowBytes>>20, searchOutputByteCeiling)
	}
	if got := FormatTextHitsWithCap(hits, dropped, searchCap); got != FormatTextHits(hits, dropped) {
		t.Errorf("an uncut result is rendered differently from FormatTextHits — ordinary output must stay byte-identical:\n%s", got)
	}
	for _, h := range hits {
		if h.LineNo != 1 {
			t.Errorf("hit %s reported line %d, want 1: the parser read the wrong field", h.FilePath, h.LineNo)
		}
		if !strings.Contains(h.Snippet, query) {
			t.Errorf("hit %s snippet does not carry the match: %.80q", h.FilePath, h.Snippet)
		}
		// buildSnippet trims to ~100 bytes around the match; a snippet that kept
		// the whole 4 MB row would mean the trimming path was bypassed.
		if len(h.Snippet) > searchBoundRowBytes/1000 {
			t.Errorf("hit %s snippet is %d bytes — the row was not trimmed", h.FilePath, len(h.Snippet))
		}
	}
}

// TestKeywordSearchTruncatesAtTheCeilingAndSaysSo is arm 2: the bound fires, and
// the caller is TOLD — the answer is the hits that came before the cut plus a
// SearchCap that names it, never a silently complete-looking result and never a
// rendering that claims the query ran to the end of the data (Rule 15).
//
// The contract asserted here is the integration branch's (ceb1436e,
// runSearchBackend): a truncated read returns PARTIAL hits with Stopped set and
// the measured numbers, which is a different Rule 15 answer from this task's first
// attempt (a named refusal returning no hits). Two shapes for one fact would be two
// definitions of it, so the rebase kept the one that is in the tree, and this arm
// moved with it — the VALUE of the arm is unchanged: a search whose reading was cut
// must be impossible to mistake for one that finished.
func TestKeywordSearchTruncatesAtTheCeilingAndSaysSo(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	const query = "CEILING_MARKER"
	dir := writeSearchBoundFixture(t, searchBoundCeilingFixtureFiles, searchBoundFixtureRows, query)

	hits, dropped, searchCap, err := keywordSearchViaRipgrepCapped(
		context.Background(), SearchScope{RootPath: dir}, query, searchBoundTestCeiling)
	if err != nil {
		t.Fatalf("a %d MiB output stream over a %d-byte ceiling returned an error instead of a capped answer: %v",
			searchBoundCeilingFixtureFiles*searchBoundRowBytes>>20, searchBoundTestCeiling, err)
	}
	if !searchCap.Stopped {
		t.Fatalf("the reading was cut but SearchCap.Stopped is false — a partial answer is being reported as a complete one (Rule 15)")
	}
	if searchCap.MaxBytes != searchBoundTestCeiling {
		t.Errorf("SearchCap.MaxBytes = %d, want the ceiling that applied, %d", searchCap.MaxBytes, searchBoundTestCeiling)
	}
	if searchCap.Bytes <= searchBoundTestCeiling {
		t.Errorf("SearchCap.Bytes = %d does not exceed the ceiling %d — the number reported is not the reading that fired",
			searchCap.Bytes, searchBoundTestCeiling)
	}
	if searchCap.Kept != len(hits) {
		t.Errorf("SearchCap.Kept = %d but %d hit(s) were returned — the notice would misstate what the caller holds",
			searchCap.Kept, len(hits))
	}
	// PARTIAL means partial: the fixture holds one match per file and the ceiling
	// cuts before the last of them, so a result carrying every file would prove the
	// ceiling never fired at all.
	if len(hits) >= searchBoundCeilingFixtureFiles {
		t.Errorf("%d hit(s) kept of %d matches — the ceiling did not cut the stream", len(hits), searchBoundCeilingFixtureFiles)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 (no result cap was set: this is the READING bound, not the result cap)", dropped)
	}
	// And the rendering must carry the notice: the hits look ordinary, so the only
	// thing that stops a caller treating them as the whole answer is the sentence.
	rendered := FormatTextHitsWithCap(hits, dropped, searchCap)
	if !strings.Contains(rendered, "search truncated") || !strings.Contains(rendered, "narrow the query") {
		t.Errorf("the truncated result is rendered without its notice — the caller cannot tell it is partial:\n%s", rendered)
	}
}

// TestKeywordSearchCapsWhileCounting is arm 2b: the second half of the memory
// bound, and the one a streaming read alone does not give.
//
// The streaming loop used to append EVERY parsed match and slice at the end, so
// the wrapper still held the whole match set — bounded by nothing but the output
// ceiling — for the sake of a LENGTH it only needed to count. Measured on the
// short-line sweep in this task's report: ~37 MB of retained hits on a
// 200,000-match fixture. The loop now retains at most MaxResult and counts the
// rest, and this arm pins the observable contract that guard must preserve: the
// hit list is the cap, and `dropped` is the exact remainder — the number the
// renderer turns into "… plus N more matches" (R3-8).
func TestKeywordSearchCapsWhileCounting(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	const (
		query   = "CAP_MARKER"
		files   = 5
		rows    = 40 // 200 matches
		maxHits = 7
	)
	dir := t.TempDir()
	for i := 0; i < files; i++ {
		var b strings.Builder
		for r := 0; r < rows; r++ {
			fmt.Fprintf(&b, "%s line %d of file %d\n", query, r, i)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), []byte(b.String()), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	hits, dropped, searchCap, err := KeywordSearch(SearchScope{RootPath: dir, MaxResult: maxHits}, query)
	if err != nil {
		t.Fatalf("KeywordSearch: %v", err)
	}
	if searchCap.Stopped {
		t.Errorf("SearchCap.Stopped = true on a %d-byte output under the production ceiling — the result cap must not be reported as a reading cut",
			files*rows*32)
	}
	if len(hits) != maxHits {
		t.Errorf("hits = %d, want the cap %d", len(hits), maxHits)
	}
	if dropped != files*rows-maxHits {
		t.Errorf("dropped = %d, want %d — the count must survive the retention cap (the renderer's \"N more matches\" is this number)",
			dropped, files*rows-maxHits)
	}
	if rendered := FormatTextHits(hits, dropped); !strings.Contains(rendered, fmt.Sprintf("plus %d more matches", dropped)) {
		t.Errorf("rendered result does not carry the overflow notice:\n%s", rendered)
	}

	// THE SHAPE HALF, and it is the half that makes this arm a memory guard
	// rather than a behaviour guard. Appending every hit and slicing at the end
	// produces the SAME hits and the SAME dropped count — measured: the red-proof
	// arm that removed the retention guard left this test GREEN, because the
	// contract had not changed, only the peak. So the retention has to be pinned
	// where it lives: the append inside the scan loop must be guarded by
	// maxResult, i.e. the hit list may never grow past what the caller can receive.
	//
	// WHERE it lives is upstream's decision (ceb1436e): the per-line parse and the
	// retention moved into readBackendStdout, which takes maxResult as its own
	// parameter. This arm therefore reads THAT function — pinning the name of a
	// function the integration branch owns would turn a legitimate refactor into a
	// red, which is the failure mode this arm must not have.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "search.go", nil, 0)
	if err != nil {
		t.Fatalf("parse search.go: %v", err)
	}
	leg := findFuncDecl(f, "readBackendStdout")
	if leg == nil {
		t.Fatal("readBackendStdout is gone — the scan loop the retention guard lives in was renamed or removed")
	}
	if !appendsHitsWithinMaxResultGuard(leg) && !appendsHitsBehindAGuardClause(leg) {
		t.Error("the scan loop appends to hits with NO result-cap guard above it (neither an enclosing if nor a preceding `continue`) — every match is retained and the wrapper's peak grows with the hit count again (this arm's own red proof: that mutation left the behavioural assertions green)")
	}
}

// TestRipgrepLegDoesNotSlurpTheBackend is arm 3: the SHAPE guard. The ripgrep leg
// must obtain the child's output through StdoutPipe and parse it as it arrives;
// collecting it with CombinedOutput()/Output() is the defect, and it is invisible
// to any assertion about the RESULT (the old code returned the same hits) — which
// is precisely why it needs its own reading.
//
// It reads the AST, so the production comments that explain what the old call did
// do not trip it, and a bare-literal ceiling cannot hide behind a string.
func TestRipgrepLegDoesNotSlurpTheBackend(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "search.go", nil, 0)
	if err != nil {
		t.Fatalf("parse search.go: %v", err)
	}

	// The ONE function this arm judges: the streaming leg. Upstream split the read
	// out into runSearchBackend, so the pipe is opened there — the arm therefore
	// checks the whole call path the production entry reaches, not one function.
	fn := findFuncDecl(f, "keywordSearchViaRipgrepCapped")
	if fn == nil {
		t.Fatal("keywordSearchViaRipgrepCapped is gone — the streaming leg this file guards was renamed or removed")
	}
	backend := findFuncDecl(f, "runSearchBackend")
	if backend == nil {
		t.Fatal("runSearchBackend is gone — the function that owns the backend read was renamed or removed")
	}

	var sawStdoutPipe bool
	for _, judged := range []*ast.FuncDecl{fn, backend} {
		ast.Inspect(judged, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "CombinedOutput", "Output":
				t.Errorf("search.go:%d calls %s on the rg child — that reads the ENTIRE backend output into one buffer, which is the defect this guard exists for (use StdoutPipe + scanner.Scan)",
					fset.Position(sel.Pos()).Line, sel.Sel.Name)
			case "StdoutPipe":
				sawStdoutPipe = true
			}
			return true
		})
	}
	if !sawStdoutPipe {
		t.Error("the rg leg never calls StdoutPipe — its output is not being streamed")
	}

	// The production entry must hand the BOUNDED leg the named constant (Rule 1/13:
	// the limit exists to be referenced, and a caller-visible knob is not the point
	// of it). Asserting the identifier, not a value, keeps the reading about the
	// wiring rather than about today's number.
	entry := findFuncDecl(f, "keywordSearchViaRipgrep")
	if entry == nil {
		t.Fatal("keywordSearchViaRipgrep is gone — the production entry this arm guards was removed")
	}
	if !callsWithIdentArg(entry, "keywordSearchViaRipgrepCapped", "searchOutputByteCeiling") {
		t.Error("keywordSearchViaRipgrep does not pass searchOutputByteCeiling to the capped leg — the production path would be unbounded")
	}

	// The ceiling must leave every query that WORKS TODAY untouched, and that is a
	// measured property rather than an aesthetic one: the largest real ripgrep
	// output seen on this host is a session-file query (38.9 MB, quoted in
	// search.go's own comment above searchOutputByteCeiling). A ceiling below it
	// would turn ordinary queries into truncated ones, which is the regression this
	// arm forbids.
	if searchOutputByteCeiling < searchBoundLargestRealOutputMeasured {
		t.Errorf("searchOutputByteCeiling = %d is below the largest real output measured on this host (%d): ordinary queries would start being cut",
			searchOutputByteCeiling, searchBoundLargestRealOutputMeasured)
	}
}

// findFuncDecl returns the named top-level function declaration's body, or nil.
func findFuncDecl(f *ast.File, name string) *ast.FuncDecl {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name != nil && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

// callsWithIdentArg reports whether body contains a call to callee whose
// arguments include the bare identifier argName.
func callsWithIdentArg(body *ast.FuncDecl, callee, argName string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != callee {
			return true
		}
		for _, a := range call.Args {
			if arg, ok := a.(*ast.Ident); ok && arg.Name == argName {
				found = true
			}
		}
		return true
	})
	return found
}

// appendsHitsWithinMaxResultGuard reports whether every `hits = append(hits, …)`
// inside body sits under an `if` whose text mentions scope.MaxResult. It walks
// the AST rather than the bytes so the production comments (which name both the
// identifier and the defect) cannot satisfy or trip it.
//
// A false NEGATIVE is possible in principle — a guard spelled through some other
// identifier — and that is the right trade: a guard the reading cannot see is a
// guard the next reader will not see either, and MaxResult is the field the
// SearchScope contract already uses for exactly this.
func appendsHitsWithinMaxResultGuard(body *ast.FuncDecl) bool {
	found := 0
	guarded := 0
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt:
			// Report the appends found inside this guard by walking its body.
			inner := 0
			ast.Inspect(node.Body, func(m ast.Node) bool {
				if isHitsAppend(m) {
					inner++
				}
				return true
			})
			if inner == 0 {
				return true
			}
			found += inner
			if mentionsSelector(node.Cond, "scope", "MaxResult") {
				guarded += inner
			}
			return false // do not descend again from the outer walk
		}
		return true
	})
	// found counts every hits-append that sits under SOME if (guarded or not);
	// an append under no if at all is counted by the second pass, so a guard that
	// was deleted outright cannot pass by making `found` zero.
	unguarded := 0
	ast.Inspect(body, func(n ast.Node) bool {
		if isHitsAppend(n) && !insideAnyIf(body, n) {
			unguarded++
		}
		return true
	})
	return found > 0 && guarded == found && unguarded == 0
}

// appendsHitsBehindAGuardClause is the OTHER spelling of the same retention
// guard, and it is the one the integration branch uses (ceb1436e, and this is
// exactly why the arm had to be re-pointed):
//
//	matched++
//	if maxResult > 0 && len(hits) >= maxResult {
//		continue                      // guard clause, not an enclosing if
//	}
//	hits = append(hits, TextHit{…})
//
// The cap is enforced by the early continue ABOVE the append, so a scan of
// "appends lexically inside an if" finds nothing and would report a bound that is
// in fact present. This reads the same fact in that shape: at least one
// maxResult-testing if whose body ends in `continue`, positioned before every
// hits-append.
//
// It is deliberately loose about DISTANCE (any preceding guard clause counts):
// pinning adjacency to the byte would make a comment or a renamed variable red,
// which is the failure mode this whole file exists to avoid. What it does catch is
// the real regression — delete the guard clause and the append is unreachable by
// either spelling of the check.
func appendsHitsBehindAGuardClause(body *ast.FuncDecl) bool {
	type guard struct{ to token.Pos }
	var guards []guard
	ast.Inspect(body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if !mentionsBareIdent(ifs.Cond, "maxResult") {
			return true
		}
		if !blockEndsWithContinue(ifs.Body) {
			return true
		}
		guards = append(guards, guard{to: ifs.End()})
		return true
	})
	if len(guards) == 0 {
		return false
	}
	appends := 0
	guarded := 0
	ast.Inspect(body, func(n ast.Node) bool {
		if !isHitsAppend(n) {
			return true
		}
		appends++
		for _, g := range guards {
			if n.Pos() > g.to {
				guarded++
				break
			}
		}
		return true
	})
	return appends > 0 && guarded == appends
}

// blockEndsWithContinue reports whether the block's LAST statement is `continue`.
func blockEndsWithContinue(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) == 0 {
		return false
	}
	br, ok := b.List[len(b.List)-1].(*ast.BranchStmt)
	return ok && br.Tok == token.CONTINUE
}

// mentionsBareIdent reports whether expr contains the identifier name anywhere
// (as opposed to mentionsSelector, which wants x.sel).
func mentionsBareIdent(expr ast.Expr, name string) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return true
	})
	return found
}

// isHitsAppend matches the specific statement `hits = append(hits, …)`.
func isHitsAppend(n ast.Node) bool {
	as, ok := n.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return false
	}
	lhs, ok := as.Lhs[0].(*ast.Ident)
	if !ok || lhs.Name != "hits" {
		return false
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "append" {
		return false
	}
	if len(call.Args) == 0 {
		return false
	}
	first, ok := call.Args[0].(*ast.Ident)
	return ok && first.Name == "hits"
}

// mentionsSelector reports whether expr contains the selector x.sel.
func mentionsSelector(expr ast.Expr, x, sel string) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		s, ok := n.(*ast.SelectorExpr)
		if !ok || s.Sel.Name != sel {
			return true
		}
		if id, ok := s.X.(*ast.Ident); ok && id.Name == x {
			found = true
		}
		return true
	})
	return found
}

// insideAnyIf reports whether target is lexically inside some *ast.IfStmt body
// within root. It is a position test: an append is inside an if when its
// position falls within that if's body span.
func insideAnyIf(root ast.Node, target ast.Node) bool {
	tp := target.Pos()
	inside := false
	ast.Inspect(root, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if tp >= ifs.Body.Lbrace && tp <= ifs.Body.Rbrace {
			inside = true
		}
		return true
	})
	return inside
}

// TestRetainedHitFieldsDoNotPinTheWholeRow is arm 4 — the ALLOCATION guard, and it
// exists because the byte-bound arms above were all GREEN while the memory still
// grew with the hit count.
//
// splitRgColumnLine splits ONE rg stdout line, so every string it returns that is
// a slice of that line is a SUBSTRING sharing the line's backing array. The row
// for a session search is megabytes (the largest on this host is 3,987,146
// bytes), so a retained TextHit whose FilePath or Snippet points INTO the row
// pins the whole row for as long as the hit lives — the scan can stream perfectly
// and the process still grows linearly in the output, which is the defect this
// file is about, one indirection further out.
//
// MEASURED (both fields cloned vs snippet only), 20 matched rows of 4 MB, MaxResult
// 50, one query, peak RSS of the gopi process alone: 98,584 KB before the FilePath
// clone, 41,984 KB after — and 17,904 KB / 18,092 KB on ONE row, i.e. the curve
// went from linear in the output to flat. The numbers are in this task's report.
//
// The reading is unsafe.StringData identity, not a size: a clone allocates, so its
// data pointer cannot equal the row's. It is the only way to observe aliasing, and
// the wrong direction is silent — an aliasing field costs nothing at the assertion
// and everything at the peak.
func TestRetainedHitFieldsDoNotPinTheWholeRow(t *testing.T) {
	// A row long enough that "pins the parent" is a real cost, with a match
	// column inside it: the shape a session-log search produces.
	row := "/home/user/.gopi/sessions/abcdef/abcdef.raw.log:12:1:" + strings.Repeat("z", 4096)
	file, lineNo, snippet, ok := splitRgColumnLine(row)
	if !ok {
		t.Fatalf("splitRgColumnLine refused a well-formed line: %q", row[:40])
	}
	if lineNo != 12 {
		t.Fatalf("lineNo = %d, want 12", lineNo)
	}

	rowData := unsafe.StringData(row)
	if unsafe.StringData(file) == rowData {
		t.Errorf("FilePath aliases the whole rg output row (%d bytes) — every retained hit pins its row in memory, so peak RSS keeps growing with the matched output even though the parse is streamed; clone it (strings.Clone) where the hit is built", len(row))
	}
	if unsafe.StringData(snippet) == rowData {
		t.Errorf("Snippet aliases the whole rg output row (%d bytes) — same defect through the other field", len(row))
	}
	if file != "/home/user/.gopi/sessions/abcdef/abcdef.raw.log" {
		t.Errorf("FilePath = %q, want the path field intact (a clone must not change the value)", file)
	}
}
