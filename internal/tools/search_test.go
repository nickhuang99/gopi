package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rgAvailable reports whether ripgrep is on PATH (integration tests skip when
// absent so the suite runs on machines without rg installed).
func rgAvailable() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}

// chdirTemp moves the process into a fresh temp dir and restores the old cwd
// on test end — the search tools resolve scope.RootPath "" to the cwd, so this
// mirrors the existing symbol_test.go pattern.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// t.Chdir registers the restore itself and FAILS the test if it cannot
	// move the process in or back out — a silently failed restore would leave
	// every later test in this binary inside a removed t.TempDir().
	t.Chdir(dir)
	return dir
}

func TestCodeSearchStructuredHit(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	dir := chdirTemp(t)
	src := `package demo

// NewThing creates a new thing.
func NewThing() (*Thing, error) {
	return &Thing{}, nil
}

type Thing struct {
	Name string
}
`
	if err := os.WriteFile(filepath.Join(dir, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := CodeSearch(SearchScope{RootPath: "."}, "NewThing")
	if err != nil {
		t.Fatalf("CodeSearch error: %v", err)
	}
	// ctags emits both qualified (demo.NewThing) and bare entries — they must
	// deduplicate to a single structured hit.
	if len(hits) != 1 {
		t.Fatalf("CodeSearch = %d hits, want 1: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.SymbolName != "NewThing" {
		t.Errorf("SymbolName = %q, want %q", h.SymbolName, "NewThing")
	}
	if h.SymbolKind != "func" {
		t.Errorf("SymbolKind = %q, want %q", h.SymbolKind, "func")
	}
	if !strings.HasSuffix(h.FilePath, "demo.go") {
		t.Errorf("FilePath = %q, want suffix demo.go", h.FilePath)
	}
	if h.LineNo != 4 {
		t.Errorf("LineNo = %d, want 4", h.LineNo)
	}
	if !strings.Contains(h.Signature, "func NewThing") {
		t.Errorf("Signature = %q, want definition snapshot", h.Signature)
	}
	// Tags file should now exist.
	if _, err := os.Stat(filepath.Join(dir, ".gopi-tags")); err != nil {
		t.Errorf("expected .gopi-tags to be generated: %v", err)
	}
}

// TestCodeSearchPythonAndShell: the ctags index covers Python and shell
// sources, not just Go — a class in a .py file and a function in a .sh file
// must both resolve as structured definition hits.
func TestCodeSearchPythonAndShell(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	dir := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "car.py"), []byte(`class Vehicle:
    def start(self):
        pass

def drive(v):
    pass
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deploy.sh"), []byte(`#!/bin/bash
deploy() {
  echo "deploying"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := CodeSearch(SearchScope{RootPath: "."}, "Vehicle")
	if err != nil {
		t.Fatalf("CodeSearch(Vehicle) error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("CodeSearch(Vehicle) = %d hits, want 1: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.SymbolKind != "class" {
		t.Errorf("SymbolKind = %q, want %q (python class)", h.SymbolKind, "class")
	}
	if !strings.HasSuffix(h.FilePath, "car.py") || h.LineNo != 1 {
		t.Errorf("hit = %+v, want car.py:1", h)
	}

	hits, _, _, err = CodeSearch(SearchScope{RootPath: "."}, "deploy")
	if err != nil {
		t.Fatalf("CodeSearch(deploy) error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("CodeSearch(deploy) = %d hits, want 1: %+v", len(hits), hits)
	}
	h = hits[0]
	if h.SymbolKind != "func" {
		t.Errorf("SymbolKind = %q, want %q (shell function)", h.SymbolKind, "func")
	}
	if !strings.HasSuffix(h.FilePath, "deploy.sh") || h.LineNo != 2 {
		t.Errorf("hit = %+v, want deploy.sh:2", h)
	}
}

// TestCodeSearchJavaScriptAndTypeScript: the ctags index covers JavaScript and
// TypeScript sources — classes, functions, consts and interfaces in .js/.ts
// must resolve as structured definition hits, including the extensions ctags
// does not map by default (.cjs → JavaScript, .tsx → TypeScript).
func TestCodeSearchJavaScriptAndTypeScript(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	// The test asserts modern JS/TS definition kinds (class/interface/const
	// in ES6+ sources). Exuberant-ctags 5.9 (2011, still shipped by some
	// distros) predates those parsers: its JavaScript parser only knows
	// function/var, so the lookup degrades to ripgrep references. Probe the
	// installed binary for a universal-ctags-era parser name instead of
	// failing the assertion on a capability gap.
	if out, err := exec.Command("ctags", "--list-languages").Output(); err == nil &&
		!strings.Contains(string(out), "TypeScript") {
		t.Skip("installed ctags lacks a TypeScript parser (exuberant-era); JS/TS definition kinds unverifiable")
	}
	dir := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(`const NAME = "gopi";
var counter = 0;
function helper() { return 1; }
class Widget { constructor() {} render() { return NAME; } }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.ts"), []byte(`interface Shape { area(): number; }
const TAU = 6.28;
export function compute(x: number): number { return x * TAU; }
class Circle implements Shape { area(): number { return TAU; } }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comp.tsx"), []byte(`function App() { return <div>hi</div>; }
const styles = { color: "red" };
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.cjs"), []byte(`const legacyName = "l";
function legacyFn() {}
module.exports = { legacyFn };
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		symbol, wantKind, wantFile string
		wantLine                   int
	}{
		{"helper", "func", "app.js", 3},
		{"Widget", "class", "app.js", 4},
		{"NAME", "const", "app.js", 1},
		{"counter", "var", "app.js", 2},
		{"Shape", "interface", "app.ts", 1},
		{"compute", "func", "app.ts", 3},
		{"Circle", "class", "app.ts", 4},
		{"App", "func", "comp.tsx", 1},
		{"legacyFn", "func", "legacy.cjs", 2},
	}
	for _, c := range cases {
		hits, _, _, err := CodeSearch(SearchScope{RootPath: "."}, c.symbol)
		if err != nil {
			t.Fatalf("CodeSearch(%s) error: %v", c.symbol, err)
		}
		var found bool
		for _, h := range hits {
			if h.SymbolKind == c.wantKind && strings.HasSuffix(h.FilePath, c.wantFile) && h.LineNo == c.wantLine {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("CodeSearch(%s) = %+v, want %s %s:%d", c.symbol, hits, c.wantKind, c.wantFile, c.wantLine)
		}
	}
}

// TestCodeSearchAbsoluteRoot exercises scope.RootPath without the chdir trick:
// the ctags index must be generated and read at the given root.
func TestCodeSearchAbsoluteRoot(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nfunc Zeta() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := CodeSearch(SearchScope{RootPath: dir}, "Zeta")
	if err != nil {
		t.Fatalf("CodeSearch error: %v", err)
	}
	if len(hits) != 1 || hits[0].SymbolKind != "func" || hits[0].LineNo != 2 {
		t.Fatalf("CodeSearch = %+v, want one func hit at line 2", hits)
	}
}

// TestCodeSearchFallbackToRipgrep: the symbol exists only in a comment, which
// ctags (definitions only) cannot index — CodeSearch must fall back to
// KeywordSearch so the lookup never comes back empty.
func TestCodeSearchFallbackToRipgrep(t *testing.T) {
	if !ctagsAvailable() || !rgAvailable() {
		t.Skip("ctags and rg required")
	}
	dir := chdirTemp(t)
	src := "package demo\n\n// JustAMarker is a thing.\nfunc Foo() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := CodeSearch(SearchScope{RootPath: ".", IncludeGlob: []string{"**/*.go"}}, "JustAMarker")
	if err != nil {
		t.Fatalf("CodeSearch error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("CodeSearch = %d hits, want 1 fallback hit: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.SymbolKind != "reference" {
		t.Errorf("SymbolKind = %q, want %q (fallback occurrence)", h.SymbolKind, "reference")
	}
	if !strings.HasSuffix(h.FilePath, "demo.go") {
		t.Errorf("FilePath = %q, want suffix demo.go", h.FilePath)
	}
	if h.LineNo != 3 {
		t.Errorf("LineNo = %d, want 3", h.LineNo)
	}
	if !strings.Contains(h.Signature, "JustAMarker") {
		t.Errorf("Signature = %q, want snippet containing the symbol", h.Signature)
	}
}

func TestCodeSearchNotFound(t *testing.T) {
	if !ctagsAvailable() || !rgAvailable() {
		t.Skip("ctags and rg required")
	}
	dir := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := CodeSearch(SearchScope{RootPath: "."}, "NoSuchSymbol")
	if err != nil {
		t.Fatalf("CodeSearch error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("CodeSearch = %d hits, want empty (no error) for missing symbol", len(hits))
	}
}

func TestCodeSearchEmptySymbol(t *testing.T) {
	if _, _, _, err := CodeSearch(SearchScope{}, "   "); err == nil {
		t.Error("expected error for empty symbol")
	}
}

func TestKeywordSearchStructuredHit(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	dir := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n// marker-abc-42 here\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes\nsee marker-abc-42 in docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := KeywordSearch(SearchScope{RootPath: "."}, "marker-abc-42")
	if err != nil {
		t.Fatalf("KeywordSearch error: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("KeywordSearch = %d hits, want 2 (go + md): %+v", len(hits), hits)
	}
	for _, h := range hits {
		if h.LineNo == 0 {
			t.Errorf("hit %+v has zero LineNo", h)
		}
		if !strings.Contains(h.Snippet, "marker-abc-42") {
			t.Errorf("hit %+v snippet missing the match", h)
		}
	}
}

func TestKeywordSearchRegex(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	dir := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("marker-42\nmarker-abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := KeywordSearch(SearchScope{RootPath: "."}, `marker-\d+`)
	if err != nil {
		t.Fatalf("KeywordSearch error: %v", err)
	}
	if len(hits) != 1 || hits[0].LineNo != 1 {
		t.Fatalf("KeywordSearch regex = %+v, want single hit on line 1", hits)
	}
}

func TestKeywordSearchGlobs(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	dir := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n// glow-gob-marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("# b\n# glow-gob-marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Include glob restricts the search to .md files.
	hits, _, _, err := KeywordSearch(SearchScope{RootPath: ".", IncludeGlob: []string{"**/*.md"}}, "glow-gob-marker")
	if err != nil {
		t.Fatalf("KeywordSearch (include) error: %v", err)
	}
	if len(hits) != 1 || !strings.HasSuffix(hits[0].FilePath, "b.md") {
		t.Fatalf("include-glob search = %+v, want only b.md", hits)
	}

	// Exclude glob removes the .md file again.
	hits, _, _, err = KeywordSearch(SearchScope{RootPath: ".", ExcludeGlob: []string{"**/b.md"}}, "glow-gob-marker")
	if err != nil {
		t.Fatalf("KeywordSearch (exclude) error: %v", err)
	}
	if len(hits) != 1 || !strings.HasSuffix(hits[0].FilePath, "a.go") {
		t.Fatalf("exclude-glob search = %+v, want only a.go", hits)
	}
}

func TestKeywordSearchMaxResult(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	dir := chdirTemp(t)
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package p\n// cap-marker\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// R3-8: the cap truncates but reports the dropped count — the result
	// renders "1 more match" instead of silently hiding it.
	hits, dropped, _, err := KeywordSearch(SearchScope{RootPath: ".", MaxResult: 2}, "cap-marker")
	if err != nil {
		t.Fatalf("KeywordSearch error: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("MaxResult=2 but got %d hits", len(hits))
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 (3 matches − cap 2)", dropped)
	}
	rendered := FormatTextHits(hits, dropped)
	if !strings.Contains(rendered, "plus 1 more matches") {
		t.Errorf("overflow notice missing from the rendered result: %q", rendered)
	}
}

// TestCodeSearchOverflowNotice: the R2 A/B A14 scenario — a definition
// lookup whose hits exceed the cap must not silently hide the tail (the
// 21st+ match was the real target under the old cap of 20). 55 definitions
// of one symbol at MaxResult=50 → the overflow notice names the 5 dropped.
func TestCodeSearchOverflowNotice(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	dir := chdirTemp(t)
	for i := 0; i < 55; i++ {
		content := fmt.Sprintf("package p%d\n\nfunc OverflowSym() {}\n", i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.go", i)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	hits, dropped, _, err := CodeSearch(SearchScope{RootPath: ".", MaxResult: 50}, "OverflowSym")
	if err != nil {
		t.Fatalf("CodeSearch error: %v", err)
	}
	if len(hits) != 50 {
		t.Errorf("hits = %d, want 50 (capped)", len(hits))
	}
	if dropped != 5 {
		t.Errorf("dropped = %d, want 5 (55 definitions − cap 50)", dropped)
	}
	rendered := FormatCodeSymbolHits("OverflowSym", hits, dropped)
	if !strings.Contains(rendered, "plus 5 more matches") {
		t.Errorf("overflow notice missing: tail=%q", rendered[len(rendered)-120:])
	}
}

func TestKeywordSearchNoMatch(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	dir := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := KeywordSearch(SearchScope{RootPath: "."}, "definitely-not-present")
	if err != nil {
		t.Fatalf("KeywordSearch error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("KeywordSearch = %d hits, want empty for no-match", len(hits))
	}
}

func TestKeywordSearchEmptyQuery(t *testing.T) {
	if _, _, _, err := KeywordSearch(SearchScope{}, "  "); err == nil {
		t.Error("expected error for empty query")
	}
}

func TestFormatCodeSymbolHits(t *testing.T) {
	hits := []CodeSymbolHit{
		{SymbolName: "NewThing", SymbolKind: "func", FilePath: "demo.go", LineNo: 4, Signature: "func NewThing()"},
		{SymbolName: "OldThing", SymbolKind: "reference", FilePath: "demo.go", LineNo: 0, Signature: "see OldThing"},
	}
	got := FormatCodeSymbolHits("NewThing", hits, 0)
	want := "NewThing  demo.go:4  func  func NewThing()\nOldThing  demo.go  reference  see OldThing"
	if got != want {
		t.Errorf("FormatCodeSymbolHits =\n%s\nwant:\n%s", got, want)
	}

	empty := FormatCodeSymbolHits("Nope", nil, 0)
	if !strings.Contains(empty, "Nope") {
		t.Errorf("empty hits should name the symbol, got %q", empty)
	}
}

func TestFormatTextHits(t *testing.T) {
	hits := []TextHit{
		{FilePath: "a.go", LineNo: 3, Snippet: "// marker here"},
	}
	got := FormatTextHits(hits, 0)
	want := "a.go:3: // marker here"
	if got != want {
		t.Errorf("FormatTextHits = %q, want %q", got, want)
	}
	if FormatTextHits(nil, 0) != "No matches." {
		t.Errorf("empty hits should render 'No matches.'")
	}
	// R3-8: an overflow notice names the dropped count — never silent.
	if overflow := FormatTextHits(hits, 3); !strings.Contains(overflow, "plus 3 more matches") {
		t.Errorf("overflow notice missing the dropped count: %q", overflow)
	}
	symHits := []CodeSymbolHit{{SymbolName: "X", SymbolKind: "func", FilePath: "x.go", LineNo: 1}}
	if overflow := FormatCodeSymbolHits("X", symHits, 5); !strings.Contains(overflow, "plus 5 more matches") {
		t.Errorf("symbol overflow notice missing the dropped count: %q", overflow)
	}
}

func TestSplitRgColumnLine(t *testing.T) {
	file, line, snip, ok := splitRgColumnLine("a.go:12:5:func Foo()")
	if !ok || file != "a.go" || line != 12 || snip != "func Foo()" {
		t.Fatalf("splitRgColumnLine = (%q, %d, %q, %v)", file, line, snip, ok)
	}
	// Content may contain colons; the path is the first field only.
	_, _, snip, ok = splitRgColumnLine("a.go:1:1:x:y:z")
	if !ok || snip != "x:y:z" {
		t.Fatalf("colon content split = (%q, %v)", snip, ok)
	}
	// The PARSER reports an unreadable line as ok=false — the CALLER decides it
	// is a failure, never a miss (see keywordSearchViaRipgrep: a dropped match
	// errs the whole call rather than answering a clean, partial list).
	if _, _, _, ok := splitRgColumnLine("no-colons-here"); ok {
		t.Error("expected miss for line without colons")
	}
	// A column PAST the end of the text is exactly the shape this best-effort
	// parser declared it must survive (search.go's own comment): a colon in the
	// path ("colon:name.cfg") shifts every field, so the field read as "column"
	// can be a number larger than the remaining text. It must answer a sane
	// tail, NEVER panic on line[start:end] (measured: index out of range
	// [3999970] with length 400).
	tail := strings.Repeat("z", 400)
	_, _, snip, ok = splitRgColumnLine("a.go:12:4000001:" + tail)
	if !ok {
		t.Fatal("a column past the text end must still parse (best-effort shape)")
	}
	if !strings.HasPrefix(snip, "…") {
		t.Errorf("snippet centered on a column past the end should mark the loss: %q", snip)
	}
	if body := strings.TrimPrefix(snip, "…"); !strings.HasSuffix(tail, body) || len(body) != 100 {
		t.Errorf("snippet should be the line's last 100 bytes: %q", snip)
	}
	if len(snip) > 106 { // 100 + the ellipsis, rune-boundary slack
		t.Errorf("snippet past the column is unbounded: %d bytes", len(snip))
	}
}

func TestBuildSnippet(t *testing.T) {
	// Short lines pass through whole.
	if got := buildSnippet("short line", 1); got != "short line" {
		t.Errorf("short line snippet = %q", got)
	}
	// Long line is centered on the match column with ellipses.
	long := strings.Repeat("a", 200) + "TARGET" + strings.Repeat("b", 200)
	got := buildSnippet(long, 201)
	if !strings.Contains(got, "TARGET") {
		t.Errorf("snippet lost the match: %q", got)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("long snippet should be ellipsized: %q", got)
	}
	if len(got) > 106 { // 100 + 2 ellipses, rune-boundary slack
		t.Errorf("snippet too long: %d bytes", len(got))
	}
}
