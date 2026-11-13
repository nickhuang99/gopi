package tools

// search_fallback_test.go — the regression guard for the two search tools' ZERO-
// BINARY leg (search_fallback.go).
//
// WHY THESE TESTS MUST CONTROL PATH: the host that carries the defect (nick-sager)
// had BOTH packages installed by 2026-10-12 08:51, so a test that merely calls
// CodeSearch on the ambient PATH measures the ctags/ripgrep leg on this machine
// and would pass on a host where the bug is fully alive. Every test below either
// empties PATH (t.Setenv, restored per test — Rule 17) or puts ONLY a shim on it,
// so the leg under test is the in-process scanner and nothing else can supply the
// answer.
//
// WHAT THE RED PROOF IS, and why it is a BINARY comparison rather than a test
// run (Rule 10.4 / the epic's acceptance). This file calls keywordSearchViaRipgrep,
// a symbol that does NOT exist on the pre-fix tree, so reverting search.go alone
// makes this package fail to BUILD rather than fail an assertion — a build failure
// proves nothing about behaviour. The observable pre-fix state is therefore the
// BINARY: `bin-master` (unmodified tree) vs `bin-fixed`, both driven with the same
// controlled PATH (the epic's own acceptance shape). Measured on nick-sager,
// artifacts under this task's session directory (artifacts/fallback-probe/):
//
//	bin-master, PATH=<empty>:      code_search: ctags: exec: "ctags": executable file not found in $PATH:   exit=1
//	bin-master, PATH=<ctags only>: keyword_search: rg: exec: "rg": executable file not found in $PATH:      exit=1   ← defects ②③
//	bin-fixed,  PATH=<empty>:      a real definition hit                                                      exit=0
//	bin-fixed,  PATH=<ctags only>: the fixture's reference hits                                               exit=0
//
// So these tests are not vacuous: each one asserts the leg that produced that
// one-line failure, and each asserts the backend really is unreachable first.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// searchFallbackRepoRoot returns the repository root of this test's checkout:
// the package directory's grandparent, verified by the go.mod that names it. It
// is where a REAL repository symbol is looked up — the epic's acceptance asks for
// a hit on a symbol that exists, not on a fixture this test just wrote.
func searchFallbackRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(cwd))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s (cwd %s): the real-symbol lookup needs a checkout root", root, cwd)
	}
	return root
}

// searchFallbackScope is the agent loop's code_search scope, spelled here so the
// test measures the production filter (a wider or narrower fixture scope would
// prove nothing about the tool the model actually calls).
func searchFallbackScope(root string) SearchScope {
	return SearchScope{
		RootPath:    root,
		IncludeGlob: CodeSearchGlobs,
		ExcludeGlob: []string{".git/**", "vendor/**"},
		MaxResult:   50,
	}
}

// searchFallbackNoBackend empties PATH for this test and returns the empty
// directory, so the probe is a MEASUREMENT of the scanner leg rather than of
// whatever this host happens to have installed.
func searchFallbackNoBackend(t *testing.T) string {
	t.Helper()
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if searchBackendAvailable(searchBackendCtags) || searchBackendAvailable(searchBackendRipgrep) {
		t.Fatalf("PATH=%s still reaches a search backend — the test would not exercise the fallback", empty)
	}
	return empty
}

// TestSearchFallbackFindsRealDefinitionWithoutAnyBackend is acceptance 4a: with NO
// search backend on PATH at all, code_search still returns a DEFINITION hit for a
// symbol that really is in this repository.
func TestSearchFallbackFindsRealDefinitionWithoutAnyBackend(t *testing.T) {
	searchFallbackNoBackend(t)
	root := searchFallbackRepoRoot(t)

	const symbol = "scanSymbolHits" // defined in internal/tools/search_fallback.go
	start := time.Now()
	// The probe carries its own budget for the same reason the keyword probe
	// below does: this scope is the WHOLE checkout, so the production entry point
	// (CodeSearch → FileToolTimeout) would judge the scanner by the host's load.
	// Measured with a tightened knob: `GOPI_TOOL_TIMEOUT_SEC=1` → `code_search:
	// timed out after 1s` (artifacts/red-proof-1s-x5.txt). See
	// searchFallbackProbeBudget.
	hits, dropped, _, err := codeSearchWithTimeout(searchFallbackScope(root), symbol, searchFallbackProbeTimeout(t))
	if err != nil {
		t.Fatalf("CodeSearch with no backend on PATH returned an error (%v) — the scanner leg did not answer", err)
	}
	if len(hits) == 0 {
		t.Fatalf("no hit for %s with no backend on PATH: the fallback leg found nothing in %s", symbol, root)
	}
	defs := 0
	for _, h := range hits {
		if h.SymbolKind != codeSearchReferenceKind {
			defs++
		}
		if !strings.HasSuffix(h.FilePath, ".go") {
			t.Errorf("hit outside the scope's include globs: %s", h.FilePath)
		}
		if h.SymbolName != symbol {
			t.Errorf("hit names %q, want %q", h.SymbolName, symbol)
		}
	}
	if defs == 0 {
		t.Errorf("only reference hits for %s — a definition of it is in this tree: %+v", symbol, hits)
	}
	t.Logf("scanner leg: %d hits (%d definitions, %d dropped) in %s", len(hits), defs, dropped, time.Since(start))
}

// TestSearchFallbackKeywordFindsRealTextWithoutRG is acceptance 4b: keyword_search
// over the same tree answers with no ripgrep installed.
func TestSearchFallbackKeywordFindsRealTextWithoutRG(t *testing.T) {
	searchFallbackNoBackend(t)
	root := searchFallbackRepoRoot(t)
	scope := searchFallbackScope(root)
	scope.IncludeGlob = nil // keyword_search has no include glob (see keywordSearchScope)

	// The production entry point (KeywordSearch) would bound this probe with the
	// TOOL's clock (FileToolTimeout, 30s / GOPI_TOOL_TIMEOUT_SEC). That clock is a
	// promise about a user-facing call, not a fact about this fixture: the scan
	// below walks the WHOLE checkout (7781 files / 163MB), so its wall time is a
	// function of HOST LOAD, and the assertion "the scanner returns real hits"
	// turned into "this host was idle enough" — measured red on nick-sager
	// (`keyword_search: timed out after 30s`) and reproduced here with a tightened
	// knob (`GOPI_TOOL_TIMEOUT_SEC=1` → `keyword_search: timed out after 1s`,
	// artifacts/red-proof-1s-x5.txt). The probe therefore carries its OWN budget
	// (searchFallbackProbeBudget) and the assertions below are untouched.
	hits, _, _, err := keywordSearchWithTimeout(scope, "scanTextHits", searchFallbackProbeTimeout(t))
	if err != nil {
		t.Fatalf("KeywordSearch with no backend on PATH returned an error (%v) — the scanner leg did not answer", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hit for scanTextHits with no backend on PATH")
	}
	found := false
	for _, h := range hits {
		if strings.HasSuffix(h.FilePath, filepath.Join("internal", "tools", "search_fallback.go")) {
			found = true
		}
		if h.LineNo == 0 {
			t.Errorf("hit without a line number: %+v", h)
		}
	}
	if !found {
		t.Errorf("the scanner leg missed the file that defines scanTextHits: %+v", hits)
	}
}

// TestSearchFallbackMissIsAnEmptyAnswerNotAnError: the scanner must keep the
// tools' miss contract — zero hits, nil error — so the CLI still prints
// "no symbol found" / "No matches." with its own exit code (exit 1), never a
// backend failure. "I looked and it is not there" and "I could not look" are
// different answers (Rule 15).
func TestSearchFallbackMissIsAnEmptyAnswerNotAnError(t *testing.T) {
	searchFallbackNoBackend(t)
	root := searchFallbackRepoRoot(t)
	symbol := "SearchFallbackSymbolThatExistsNowhere" + time.Now().Format("150405")

	// Both legs of the miss contract walk the same whole-tree scope, so both take
	// the probe's own budget (see searchFallbackProbeBudget) — otherwise the
	// production clock decides whether "nothing matched" is reported as an empty
	// answer or, under load, as a timeout error that looks like a violation of it.
	hits, dropped, _, err := codeSearchWithTimeout(searchFallbackScope(root), symbol, searchFallbackProbeTimeout(t))
	if err != nil {
		t.Fatalf("a miss must not be an error, got %v", err)
	}
	if len(hits) != 0 || dropped != 0 {
		t.Fatalf("a miss returned %d hits (%d dropped)", len(hits), dropped)
	}

	scope := searchFallbackScope(root)
	scope.IncludeGlob = nil
	textHits, _, _, err := keywordSearchWithTimeout(scope, "KeywordSearchFallbackNoSuchText"+time.Now().Format("0405"), searchFallbackProbeTimeout(t))
	if err != nil {
		t.Fatalf("a text miss must not be an error, got %v", err)
	}
	if len(textHits) != 0 {
		t.Fatalf("a text miss returned %d hits", len(textHits))
	}
}

// TestCodeSearchCleanMissDoesNotNeedRipgrep is defect ②'s regression test: ctags
// IS installed (a stand-in answering the capability probes and writing a valid
// index that does not contain the symbol) while ripgrep is NOT — and code_search
// must still answer with the symbol's occurrences instead of failing under the
// other tool's name.
//
// The shim is a stand-in and not the real ctags so the miss is CONTROLLED: the
// index it writes is derived from nothing, which is exactly the "stale index /
// generated code" miss the hop exists for.
func TestCodeSearchCleanMissDoesNotNeedRipgrep(t *testing.T) {
	shimDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(shimDir, "ctags"), []byte(searchFallbackCtagsShim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir) // ctags shim ONLY: no rg anywhere on PATH

	if !searchBackendAvailable(searchBackendCtags) {
		t.Fatal("the ctags shim is not reachable on PATH")
	}
	if searchBackendAvailable(searchBackendRipgrep) {
		t.Fatalf("rg is still reachable with PATH=%s — this test would not exercise the no-rg hop", shimDir)
	}

	root := t.TempDir()
	const symbol = "GadgetTarget"
	// A REFERENCE and no definition: the clean-miss hop's own input.
	src := "package fixture\n\n// " + symbol + " is used here, defined elsewhere.\nfunc caller() { _" + " = " + symbol + " }\n"
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := CodeSearch(searchFallbackScope(root), symbol)
	if err != nil {
		t.Fatalf("code_search clean miss with ctags-only PATH: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits: the clean-miss hop did not fall through to the scanner")
	}
	for _, h := range hits {
		if h.SymbolKind != codeSearchReferenceKind {
			t.Errorf("hit kind %q, want %q (the fixture defines nothing)", h.SymbolKind, codeSearchReferenceKind)
		}
		if !strings.HasSuffix(h.FilePath, "a.go") {
			t.Errorf("hit in %s, want the fixture a.go", h.FilePath)
		}
	}
	// Both the comment mentioning the symbol (line 3) and the use of it (line 4)
	// are occurrences — and neither is a definition, which is what this hop is.
	linesHit := map[int]bool{}
	for _, h := range hits {
		linesHit[h.LineNo] = true
	}
	if !linesHit[3] || !linesHit[4] {
		t.Errorf("occurrences reported on lines %v, want 3 and 4", linesHit)
	}
}

// TestSearchFallbackErrorsCarryTheCallingSurface is defect ③'s regression test.
// When the scanner leg itself fails (here: a source file this process cannot
// read) the error must be the CALLER's, and it must not mention the other tool —
// the filed symptom was `code_search: keyword_search: rg: ...`, a caller told
// about a tool it never called.
func TestSearchFallbackErrorsCarryTheCallingSurface(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}
	searchFallbackNoBackend(t)
	root := t.TempDir()
	locked := filepath.Join(root, "locked.go")
	if err := os.WriteFile(locked, []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	//stylegate:ignore the fixture file's mode IS the assertion (an unreadable source file)
	defer func() { _ = os.Chmod(locked, 0o644) }()

	_, _, _, symErr := CodeSearch(searchFallbackScope(root), "AnythingAtAll")
	if symErr == nil {
		t.Fatal("an unreadable source file must be reported, not skipped silently")
	}
	if !strings.HasPrefix(symErr.Error(), "code_search: ") {
		t.Errorf("code_search error text = %q, want the calling surface's prefix", symErr)
	}
	if strings.Contains(symErr.Error(), toolOtherSearchName) {
		t.Errorf("code_search error names the other tool: %q", symErr)
	}

	scope := searchFallbackScope(root)
	scope.IncludeGlob = nil
	_, _, _, kwErr := KeywordSearch(scope, "package")
	if kwErr == nil {
		t.Fatal("an unreadable source file must be reported by keyword_search too")
	}
	if !strings.HasPrefix(kwErr.Error(), "keyword_search: ") {
		t.Errorf("keyword_search error text = %q, want the calling surface's prefix", kwErr)
	}
}

// toolOtherSearchName is the string a wrongly-attributed error carries — the
// other tool's name, as it was filed (defect ③).
const toolOtherSearchName = "keyword_search:"

// The probe budget for the whole-tree scanner tests below. WHY A TEST-OWNED
// CONSTANT instead of the tool's own clock (the filed defect):
//
//   - The scope of these probes is the WHOLE CHECKOUT (7782 tracked files /
//     135.7 MB of tracked blobs — measured, `git ls-files`), so their wall time is this host's filesystem and load, not a property of the
//     code under test. Measured on this 4-core host: 5.63s with a cold page
//     cache, 0.76–1.04s warm, at load 1.9–4.4 — while the discovering host
//     measured 22.5s for the same probe. The production default (30s) therefore
//     sits ~7s above the worst measurement, i.e. the test's verdict was decided
//     by the host, not by the scanner (reproduced here: GOPI_TOOL_TIMEOUT_SEC=1
//     → `keyword_search: timed out after 1s`, artifacts/red-proof-1s-x5.txt).
//   - It is deliberately NOT read from GOPI_TOOL_TIMEOUT_SEC: the knob exists to
//     bound the USER-FACING call, and a probe that inherits it lets an operator
//     (or a sibling suite) tighten the tool's contract and turn this assertion
//     red from outside the file. The production default stays untouched.
//   - Sized from the two measurements that exist rather than from taste: 5.63s
//     cold / 0.76–1.04s warm on this 4-core host, 22.5s on the discovering host
//     (which is why 20s would have been too small and 30s had ~7s of margin).
//     searchFallbackProbeBudget is 45s — 2× the worst measurement, and one and a
//     half times the production clock, so the host is measured rather than
//     assumed idle while a hung scanner is still reported.
const searchFallbackProbeBudget = 45 * time.Second

// searchFallbackProbeDeadlineReserve keeps the probe's own failure report inside
// the runner's -timeout: a probe that cannot fit must fail as a measured slow
// host, not as a blanket "test binary timed out" that hides which leg hung.
const searchFallbackProbeDeadlineReserve = 5 * time.Second

// searchFallbackProbeTimeout returns the budget for one whole-tree scanner probe,
// observed from the ACTUAL test deadline instead of assuming 20s of headroom.
// `go test` kills the test binary on its own -timeout (default 10m); a run with
// less than the budget plus the reserve left cannot finish this probe honestly,
// and says so (t.Skip names the numbers) instead of being killed mid-scan.
func searchFallbackProbeTimeout(t *testing.T) time.Duration {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		return searchFallbackProbeBudget
	}
	remaining := time.Until(deadline)
	if remaining < searchFallbackProbeBudget+searchFallbackProbeDeadlineReserve {
		t.Skipf("only %s until the runner's deadline: a whole-tree scanner probe needs %s plus a %s reserve; run it without a shortened -timeout",
			remaining.Round(time.Second), searchFallbackProbeBudget, searchFallbackProbeDeadlineReserve)
	}
	return searchFallbackProbeBudget
}

// TestSearchFallbackProbeBudgetExceedsTheProductionClock is the anti-regression
// pin for the fix above: the probe must NOT be bounded by the tool's user-facing
// clock, or the filed symptom returns the moment that clock moves. It also pins
// the other direction — a probe budget large enough to hide a genuinely hung
// scanner is not a fix either, so the value is asserted to be a bounded multiple
// of the production default rather than "big".
func TestSearchFallbackProbeBudgetExceedsTheProductionClock(t *testing.T) {
	if searchFallbackProbeBudget <= DefaultFileToolTimeout {
		t.Fatalf("probe budget %s does not exceed the production tool clock %s — a whole-tree scan would be judged by the user-facing deadline again",
			searchFallbackProbeBudget, DefaultFileToolTimeout)
	}
	if searchFallbackProbeBudget > searchFallbackProbeBudgetCeiling {
		t.Fatalf("probe budget %s exceeds the ceiling %s — a hung scanner would be waited out instead of reported",
			searchFallbackProbeBudget, searchFallbackProbeBudgetCeiling)
	}
	if searchFallbackProbeDeadlineReserve <= 0 {
		t.Fatalf("deadline reserve %s must be positive: the probe's own failure report has to fit before the runner kills the binary",
			searchFallbackProbeDeadlineReserve)
	}
}

// searchFallbackProbeBudgetCeiling bounds the probe budget from above (Rule 10.4:
// the smallest sample that still proves the point). Twice the production clock is
// the widest defensible reading of "this host is slow": beyond it the test is no
// longer measuring a slow host but tolerating a hung one.
const searchFallbackProbeBudgetCeiling = 2 * DefaultFileToolTimeout

// searchFallbackCtagsShim is a stand-in ctags: it answers the capability probes
// like a real universal-ctags and, on a generation run, writes a minimal but
// VALID and COMPLETE index for a symbol the fixture does not contain, so the
// lookup is a clean miss.
const searchFallbackCtagsShim = `#!/bin/sh
target=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-f" ]; then target="$a"; fi
  prev="$a"
done
case "$1" in
  --list-languages) printf 'Go\nPython\nSh\nC++\nC\nRust\nJavaScript\nTypeScript\n'; exit 0;;
  --version) exit 0;;
  --extras=+q|--extra=+q|--map-JavaScript=+.cjs) exit 0;;
esac
[ -n "$target" ] || exit 0
printf '!_TAG_FILE_FORMAT\t2\t/extended format/\n!_TAG_FILE_SORTED\t1\t/0=unsorted, 1=sorted/\nZzzIndexedSymbol\ta.go\t/^func ZzzIndexedSymbol() {$/;"\tf\n' > "$target"
`

// TestSearchFallbackGlobSemantics pins the glob translation the scanner uses
// against ripgrep's own reading of the same scope, on a fixture where the two
// could disagree silently: "**/*.go" must match a root-level file, "vendor/**"
// must prune a directory, and ".git" must never be entered.
func TestSearchFallbackGlobSemantics(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const sym = "GlobProbeSymbol"
	write("root.go", "package p\n\nfunc "+sym+"() {}\n")
	write("deep/nested/leaf.go", "package p\n\nfunc "+sym+"() {}\n")
	write("deep/nested/leaf.py", "def "+sym+"():\n    pass\n")
	write("vendor/dep/dep.go", "package dep\n\nfunc "+sym+"() {}\n")
	write(".git/objects/pack.go", "package g\n\nfunc "+sym+"() {}\n")

	// The scope alone must be the filter under test: the ctags leg is taken out
	// of the way with an index that is valid, complete and MISSES the symbol
	// (searchFallbackCtagsShim writes exactly that), so every hit below came
	// through the scanner's own glob handling.
	shimDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(shimDir, "ctags"), []byte(searchFallbackCtagsShim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir)

	scope := SearchScope{
		RootPath:    root,
		IncludeGlob: []string{"**/*.go"},
		ExcludeGlob: []string{".git/**", "vendor/**"},
		MaxResult:   50,
	}
	hits, _, _, err := CodeSearch(scope, sym)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, h := range hits {
		got[filepath.ToSlash(h.FilePath)]++
	}
	seen := func(name string) bool {
		for p := range got {
			if strings.HasSuffix(p, "/"+name) || p == name {
				return true
			}
		}
		return false
	}
	if !seen("root.go") {
		t.Errorf("a root-level .go file must match **/*.go (ripgrep matches it): %v", got)
	}
	if !seen("leaf.go") {
		t.Errorf("a nested .go file must match **/*.go: %v", got)
	}
	if seen("leaf.py") {
		t.Errorf(".py file matched **/*.go: %v", got)
	}
	if seen("dep.go") {
		t.Errorf("vendor/** files are excluded: %v", got)
	}
	if seen("pack.go") {
		t.Errorf(".git is never searched: %v", got)
	}
}

// TestSearchFallbackRespectsTheResultCap: the scope's cap is the memory bound the
// epic asks for, and the overflow count must stay truthful ("N more matches" is
// rendered from it — a silent truncation is the R3-8 defect).
func TestSearchFallbackRespectsTheResultCap(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	const sym = "CapProbeSymbol"
	for i := 0; i < 12; i++ {
		b.WriteString("// " + sym + " occurrence\n")
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package p\n"+b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	scope := SearchScope{RootPath: root, IncludeGlob: []string{"**/*.go"}, MaxResult: 5}

	hits, dropped, _, err := CodeSearch(scope, sym)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 5 {
		t.Fatalf("got %d hits with MaxResult=5", len(hits))
	}
	if dropped != 7 {
		t.Fatalf("dropped=%d, want 7 (12 occurrences)", dropped)
	}

	scope.IncludeGlob = nil
	textHits, textDropped, _, err := KeywordSearch(scope, sym)
	if err != nil {
		t.Fatal(err)
	}
	if len(textHits) != 5 || textDropped != 7 {
		t.Fatalf("keyword cap: %d hits, %d dropped — want 5 and 7", len(textHits), textDropped)
	}
}

// TestSearchFallbackDefinitionClassification pins definitionKind and the
// whole-identifier rule on the cases that made occurrences indistinguishable
// from definitions: a commented-out declaration, a decorated declaration, and a
// longer identifier that merely CONTAINS the symbol.
func TestSearchFallbackDefinitionClassification(t *testing.T) {
	cases := []struct {
		line     string
		wantKind string
		wantName string
	}{
		{"func Foo() {}", "function", "Foo"},
		{"pub fn foo() {}", "function", "foo"},
		{"export class Foo {}", "type", "Foo"},
		{"pub struct Foo;", "type", "Foo"},
		{"#[derive(Debug)] struct Foo;", "type", "Foo"},
		{"type Foo struct {", "type", "Foo"},
		{"const Foo = 1", "variable", "Foo"},
		{"let Foo = 1;", "variable", "Foo"},
		{"func (c *Client) Foo() {}", "function", "Foo"},
		{"// func Foo() {}", "", ""},
		{"/* func Foo() {}", "", ""},
		{"# func Foo() {}", "", ""},
		{" * func Foo() {}", "", ""},
		{"Foo := bar()", "", ""},
		{"call(Foo)", "", ""},
		{"func caller() { _ = Foo }", "function", "caller"},
	}
	for _, tc := range cases {
		kind, name := definitionSignature(tc.line)
		if kind != tc.wantKind || name != tc.wantName {
			t.Errorf("definitionSignature(%q) = (%q, %q), want (%q, %q)", tc.line, kind, name, tc.wantKind, tc.wantName)
		}
	}

	if _, ok := wholeSymbolIndex("func NewClientPool() {}", "NewClient"); ok {
		t.Error("NewClient matched inside NewClientPool — a longer identifier is not the symbol")
	}
	if at, ok := wholeSymbolIndex("x := NewClient(a)", "NewClient"); !ok || at != 5 {
		t.Errorf("wholeSymbolIndex = (%d, %v), want (5, true)", at, ok)
	}
	// A rune boundary, not a byte boundary: the symbol butted against a
	// multi-byte rune is still whole.
	if _, ok := wholeSymbolIndex("「NewClient」", "NewClient"); !ok {
		t.Error("a symbol between non-ASCII runes was not recognised as whole")
	}
}

// TestSearchFallbackAgreesWithRipgrepOnTheSameTree is the parity probe: on this
// fixture the two legs must report the same MATCH LINES for the same query. It
// needs ripgrep, so it skips (naming the binary) where the package is absent —
// the same auditable skip the package already uses for the backend tests.
func TestSearchFallbackAgreesWithRipgrepOnTheSameTree(t *testing.T) {
	requireRipgrep(t)
	root := t.TempDir()
	body := "package p\n\n// ParityNeedle first\nvar x = 1\n// ParityNeedle second\n// no match here\n"
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	scope := SearchScope{RootPath: root, IncludeGlob: []string{"**/*.go"}, MaxResult: 50}

	rgHits, _, _, err := keywordSearchViaRipgrep(context.Background(), scope, "ParityNeedle")
	if err != nil {
		t.Fatalf("ripgrep leg: %v", err)
	}
	scanHits, _, err := scanTextHits(root, scope, "ParityNeedle")
	if err != nil {
		t.Fatalf("scanner leg: %v", err)
	}
	lines := func(hits []TextHit) map[int]bool {
		m := map[int]bool{}
		for _, h := range hits {
			m[h.LineNo] = true
		}
		return m
	}
	rg, sc := lines(rgHits), lines(scanHits)
	for ln := range rg {
		if !sc[ln] {
			t.Errorf("scanner missed line %d that ripgrep found", ln)
		}
	}
	for ln := range sc {
		if !rg[ln] {
			t.Errorf("scanner reported line %d that ripgrep did not", ln)
		}
	}
}
