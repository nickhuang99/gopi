package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ctagsAvailable reports whether universal-ctags is on PATH (integration tests
// skip when absent so the suite runs on machines without ctags installed).
func ctagsAvailable() bool {
	_, err := exec.LookPath("ctags")
	return err == nil
}

func TestFormatTagEntry(t *testing.T) {
	line := "NewClient\tinternal/deepseek/client.go\t/^func NewClient() (*Client, error) {$/;\"\tf\tline:12\tpackage:deepseek\ttyperef:typename:(*Client, error)"
	h := formatTagEntry(line)
	if h.SymbolName != "NewClient" {
		t.Errorf("SymbolName = %q, want NewClient", h.SymbolName)
	}
	if h.SymbolKind != "func" {
		t.Errorf("SymbolKind = %q, want func", h.SymbolKind)
	}
	if h.FilePath != "internal/deepseek/client.go" {
		t.Errorf("FilePath = %q", h.FilePath)
	}
	if h.LineNo != 12 {
		t.Errorf("LineNo = %d, want 12", h.LineNo)
	}
	if h.Signature != "func NewClient() (*Client, error) {" {
		t.Errorf("Signature = %q, want raw definition line", h.Signature)
	}
}

// TestFormatTagEntryNoLineNo: indices generated before --fields=+n lack the
// line:N field — LineNo must degrade to 0 instead of erroring.
func TestFormatTagEntryNoLineNo(t *testing.T) {
	line := "Old\told.go\t/^func Old() {$/;\"\tf\tpackage:old"
	h := formatTagEntry(line)
	if h.LineNo != 0 {
		t.Errorf("LineNo = %d, want 0", h.LineNo)
	}
	if h.SymbolKind != "func" || h.Signature != "func Old() {" {
		t.Errorf("hit = %+v", h)
	}
}

// TestFormatTagEntryShortLine: header lines and malformed entries produce a
// partial hit instead of a panic.
func TestFormatTagEntryShortLine(t *testing.T) {
	h := formatTagEntry("!_TAG_FILE_FORMAT\t2")
	if h.SymbolName != "!_TAG_FILE_FORMAT" || h.SymbolKind != "symbol" {
		t.Errorf("short entry = %+v", h)
	}
}

func TestSignatureFromPattern(t *testing.T) {
	cases := map[string]string{
		`/^func NewClient() (*Client, error) {$/;"`: "func NewClient() (*Client, error) {",
		`/^type Thing struct {$/;"`:                 "type Thing struct {",
		`/^	Name string$/;"`: "	Name string",
		`/^func Esc() \/\\ {$/;"`: "func Esc() /\\ {",
		`garbage-pattern-field`:   "garbage-pattern-field",
	}
	for in, want := range cases {
		if got := signatureFromPattern(in); got != want {
			t.Errorf("signatureFromPattern(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTagsBinarySearch: the R3-6 lookup — a sorted tags file with >500
// symbols must resolve any position (first/middle/last entry), miss cleanly
// for absent names, and surface duplicate definitions for the dedup. The
// synthetic file mirrors real ctags output: "!_TAG_..." headers first,
// byte-order-sorted entries (case-sensitive, capital letters before
// lowercase), plus a qualified alias (pkg.Sym) pairing a bare entry.
func TestTagsBinarySearch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gopi-tags")
	var sb strings.Builder
	sb.WriteString("!_TAG_FILE_FORMAT\t2\t/extended/\n")
	sb.WriteString("!_TAG_FILE_SORTED\t1\t/0=unsorted, 1=sorted/\n")
	// Sorted byte order (case-sensitive): capital 'W' (0x57) sorts before
	// lowercase 's' (0x73), so the qualified pair comes first.
	sb.WriteString("Widget\tw.go\t/^func Widget() {}$/;\"\tf\tline:1\tlanguage:Go\n")
	sb.WriteString("mypkg.Widget\tw.go\t/^func Widget() {}$/;\"\tf\tline:1\tlanguage:Go\n")
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&sb, "sym%04d\tfile%d.go\t/^func sym%04d() {}$/;\"\tf\tline:%d\tlanguage:Go\n", i, i, i, i+1)
	}
	// Two definitions of the same name (dedup case).
	sb.WriteString("sym0600\ta.go\t/^func sym0600() {}$/;\"\tf\tline:1\tlanguage:Go\n")
	sb.WriteString("sym0600\tb.go\t/^func sym0600() {}$/;\"\tf\tline:2\tlanguage:Go\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	size := info.Size()

	// First, middle and last entries resolve to their own clusters.
	for _, sym := range []string{"sym0000", "sym0300", "sym0599", "sym0600"} {
		start, err := tagsLowerBound(f, size, sym)
		if err != nil {
			t.Fatalf("tagsLowerBound(%s): %v", sym, err)
		}
		if start >= size {
			t.Fatalf("tagsLowerBound(%s) = %d, expected a hit", sym, start)
		}
		line, err := readTagLineAt(f, start, size)
		if err != nil {
			t.Fatalf("readTagLineAt(%s): %v", sym, err)
		}
		if tagLineKey(line) != sym {
			t.Errorf("lowerBound(%s) landed on %q", sym, tagLineKey(line))
		}
	}

	// The duplicate cluster of sym0600 yields both definitions (dedup input).
	start, err := tagsLowerBound(f, size, "sym0600")
	if err != nil {
		t.Fatal(err)
	}
	hits, err := scanTagCluster(f, start, "sym0600")
	if err != nil {
		t.Fatalf("scanTagCluster: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("sym0600 cluster = %d hits, want 2 (dup definitions)", len(hits))
	}

	// The qualified alias is directly addressable (old behavior: qualified
	// queries never matched ctags and fell back to rg).
	start, err = tagsLowerBound(f, size, "mypkg.Widget")
	if err != nil {
		t.Fatal(err)
	}
	hits, err = scanTagCluster(f, start, "mypkg.Widget")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].SymbolName != "mypkg.Widget" || hits[0].LineNo != 1 {
		t.Fatalf("qualified lookup = %+v, want one Widget hit", hits)
	}

	// Absent names miss cleanly: after the last entry → size; between
	// entries → the insertion point (first key strictly greater), where the
	// caller's cluster scan breaks immediately.
	start, err = tagsLowerBound(f, size, "sym9999")
	if err != nil {
		t.Fatal(err)
	}
	if start < size {
		t.Errorf("lowerBound(sym9999) = %d, want size (past the end)", start)
	}
	start, err = tagsLowerBound(f, size, "sym0300a")
	if err != nil {
		t.Fatal(err)
	}
	if line, err := readTagLineAt(f, start, size); err != nil || tagLineKey(line) != "sym0301" {
		t.Errorf("lowerBound(sym0300a) should land on sym0301, got %q (err %v)", tagLineKey(line), err)
	}
	// A query below every key lands on the very first line (offset 0).
	start, err = tagsLowerBound(f, size, "!")
	if err != nil {
		t.Fatal(err)
	}
	if start != 0 {
		t.Errorf("lowerBound(!) = %d, want 0 (first header line)", start)
	}
	if line, err := readTagLineAt(f, start, size); err != nil || tagLineKey(line) != "!_TAG_FILE_FORMAT" {
		t.Errorf("lowerBound(!) should land on the first header, got %q (err %v)", tagLineKey(line), err)
	}
}

// TestTagsBinarySearchQualifiedPair: the R2 A/B note — --extras=+q qualified
// aliases always pair a bare entry at the same file:line, so the bare-name
// cluster is complete and the old qualified-variant stripping can be dropped
// without losing definitions. Assert the pair resolves identically through
// the cluster scan (dedup collapses them to one hit).
func TestTagsBinarySearchQualifiedPair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gopi-tags")
	content := "!_TAG_FILE_FORMAT\t2\t/extended/\n" +
		"Widget\tw.go\t/^func Widget() {}$/;\"\tf\tline:1\tlanguage:Go\n" +
		"mypkg.Widget\tw.go\t/^func Widget() {}$/;\"\tf\tline:1\tlanguage:Go\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	start, err := tagsLowerBound(f, info.Size(), "Widget")
	if err != nil {
		t.Fatal(err)
	}
	hits, err := scanTagCluster(f, start, "Widget")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].SymbolKind != "func" {
		t.Fatalf("bare cluster = %+v, want the single Widget definition", hits)
	}
}

// extension, not just .go — newer .py and .sh files count, other files don't.
func TestNewestSourceFile(t *testing.T) {
	dir := t.TempDir()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	pyTime := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	shTime := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)

	writeAt := func(name string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	writeAt("a.go", old)
	writeAt("b.py", pyTime)
	writeAt("c.sh", shTime)
	// Newest of all, but not an indexed extension — must be ignored.
	writeAt("notes.txt", time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC))

	got, err := newestSourceFile(context.Background(), dir)
	if err != nil {
		t.Fatalf("newestSourceFile: %v", err)
	}
	if !got.Equal(shTime) {
		t.Errorf("newestSourceFile = %v, want %v (newest .sh)", got, shTime)
	}

	empty := t.TempDir()
	if _, err := newestSourceFile(context.Background(), empty); err == nil {
		t.Error("expected error for dir without indexed sources")
	}
}

// TestEnsureTagsRegeneratesForNewerPython: a newer .py file must trigger
// index regeneration exactly like a newer .go file, and an unchanged tree
// must not.
func TestEnsureTagsRegeneratesForNewerPython(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	tagsPath := filepath.Join(dir, ".gopi-tags")

	writeAt := func(name string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("package a\nfunc Foo() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	writeAt("a.go", old)
	writeAt("b.py", old)

	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("initial ensureTags: %v", err)
	}
	if err := os.Chtimes(tagsPath, old, old); err != nil {
		t.Fatal(err)
	}

	// No indexed file newer than the tags file → no regeneration.
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (unchanged): %v", err)
	}
	if info, err := os.Stat(tagsPath); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("tags regenerated without a newer source file: mtime=%v err=%v", info.ModTime(), err)
	}

	// A newer .py file must trigger regeneration. R6: clear the staleness
	// cache first — otherwise the fresh verdict from the step above is still
	// inside the TTL and legitimately suppresses the walk.
	resetStalenessCache()
	writeAt("b.py", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (newer .py): %v", err)
	}
	if info, err := os.Stat(tagsPath); err != nil || !info.ModTime().After(old) {
		t.Fatalf("tags not regenerated after newer .py: mtime=%v err=%v", info.ModTime(), err)
	}
}

// TestEnsureTagsRegeneratesForNewerTS: a newer .ts file must trigger index
// regeneration exactly like a newer .py or .go file, and the regenerated index
// must cover JavaScript/TypeScript sources (verified via a .cjs/.tsx symbol —
// the extensions ctags does not map by default).
func TestEnsureTagsRegeneratesForNewerTS(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	tagsPath := filepath.Join(dir, ".gopi-tags")

	writeAt := func(name, content string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	writeAt("app.js", "const NAME = 1;\nfunction helper() {}\n", old)
	// .ts content: a bare `interface` emits NO tag on exuberant-ctags (its
	// JavaScript parser only knows f/c/m/p/v kinds — interfaces are TS syntax
	// it cannot name), and even `const NAME` alone emits nothing (kind v
	// requires `var`). A function tag is recognized by BOTH ctags variants,
	// so the .ts assertion is ctags-variant-stable.
	writeAt("app.ts", "function tsShape() { return null; }\n", old)

	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("initial ensureTags: %v", err)
	}
	if err := os.Chtimes(tagsPath, old, old); err != nil {
		t.Fatal(err)
	}

	// A newer .tsx file (mapped via --map-TypeScript) must trigger regeneration.
	// R6: clear the staleness cache first (see the Python test for why).
	resetStalenessCache()
	writeAt("comp.tsx", "function App() { return null; }\n", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (newer .tsx): %v", err)
	}
	if info, err := os.Stat(tagsPath); err != nil || !info.ModTime().After(old) {
		t.Fatalf("tags not regenerated after newer .tsx: mtime=%v err=%v", info.ModTime(), err)
	}

	// The regenerated index must contain the .ts/.tsx/.js symbols.
	tags, err := os.ReadFile(tagsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\tapp.ts\t", "\tcomp.tsx\t", "\tapp.js\t"} {
		if !strings.Contains(string(tags), want) {
			t.Errorf("regenerated tags missing %q entry", want)
		}
	}
}

// TestCodeSearchGlobsMatchExtensions: the ripgrep fallback globs must be
// exactly the indexed extensions in **/* form, and cover the required
// languages (Go, Python, shell).
func TestCodeSearchGlobsMatchExtensions(t *testing.T) {
	if len(CodeSearchGlobs) != len(indexedSourceExtensions) {
		t.Fatalf("CodeSearchGlobs = %d entries, want %d extensions", len(CodeSearchGlobs), len(indexedSourceExtensions))
	}
	for i, ext := range indexedSourceExtensions {
		if want := "**/*" + ext; CodeSearchGlobs[i] != want {
			t.Errorf("CodeSearchGlobs[%d] = %q, want %q", i, CodeSearchGlobs[i], want)
		}
	}
	for _, required := range []string{".go", ".py", ".sh", ".bash",
		".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx"} {
		if !hasIndexedExt("file" + required) {
			t.Errorf("required extension %s missing from the indexed set", required)
		}
	}
}

// TestCodeSearchCoversCHeaders: .h was deliberately excluded before the
// C/C++ review fix — a reviewer asking for a struct defined in a header got
// nothing from ctags and reverted to grep. Regression: a symbol defined only
// in a .h file must come back with a definition hit (requires ctags).
func TestCodeSearchCoversCHeaders(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	dir := t.TempDir()
	src := "#ifndef T_H\n#define T_H\nstruct CsNode { int v; };\n#endif\n"
	if err := os.WriteFile(filepath.Join(dir, "t.h"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, _, _, err := CodeSearch(SearchScope{RootPath: dir, IncludeGlob: CodeSearchGlobs, MaxResult: 50}, "CsNode")
	if err != nil {
		t.Fatalf("CodeSearch(CsNode): %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits for a symbol defined in a .h header — .h dropped from the index again?")
	}
	if !strings.HasSuffix(hits[0].FilePath, ".h") {
		t.Errorf("hit path = %q, want a .h file", hits[0].FilePath)
	}
	if hits[0].SymbolKind != "struct" {
		t.Errorf("SymbolKind = %q, want struct", hits[0].SymbolKind)
	}
}

func TestKindName(t *testing.T) {
	// No language field → Go mapping (backward compat for older indices).
	cases := map[string]string{
		"f": "func", "s": "struct", "i": "interface", "m": "method",
		"t": "type", "v": "var", "c": "const", "p": "package", "": "symbol",
		"x": "x", // unknown passes through
	}
	for k, want := range cases {
		if got := kindName(k, ""); got != want {
			t.Errorf("kindName(%q, \"\") = %q, want %q", k, got, want)
		}
	}
}

// TestKindNameByLanguage: kind letters collide across languages (Python/C++
// "c" is a class, Go "c" is a const), so the language field must disambiguate.
func TestKindNameByLanguage(t *testing.T) {
	cases := []struct{ kind, language, want string }{
		{"c", "Python", "class"}, {"f", "Python", "func"},
		{"m", "Python", "method"}, {"v", "Python", "var"},
		{"I", "Python", "namespace"},
		{"f", "Sh", "func"}, {"a", "Sh", "alias"}, {"s", "Sh", "script"},
		{"c", "C++", "class"}, {"s", "C++", "struct"}, {"m", "C++", "member"},
		{"n", "C++", "namespace"}, {"t", "C++", "type"},
		{"s", "Rust", "struct"}, {"f", "Rust", "func"}, {"P", "Rust", "method"},
		{"c", "Rust", "impl"}, {"m", "Rust", "field"},
		{"c", "JavaScript", "class"}, {"f", "JavaScript", "func"},
		{"m", "JavaScript", "method"}, {"v", "JavaScript", "var"},
		{"C", "JavaScript", "const"}, {"p", "JavaScript", "property"},
		{"c", "TypeScript", "class"}, {"f", "TypeScript", "func"},
		{"m", "TypeScript", "method"}, {"i", "TypeScript", "interface"},
		{"n", "TypeScript", "namespace"}, {"C", "TypeScript", "const"},
		// Unknown kind within a known language still passes through.
		{"q", "Python", "q"},
	}
	for _, c := range cases {
		if got := kindName(c.kind, c.language); got != c.want {
			t.Errorf("kindName(%q, %q) = %q, want %q", c.kind, c.language, got, c.want)
		}
	}
}

// TestFormatTagEntryLanguage: entries from multi-language indices carry a
// language:X extra which must select the per-language kind name.
func TestFormatTagEntryLanguage(t *testing.T) {
	line := "Vehicle\tcar.py\t/^class Vehicle:$/;\"\tc\tline:1\tlanguage:Python"
	h := formatTagEntry(line)
	if h.SymbolName != "Vehicle" || h.SymbolKind != "class" {
		t.Errorf("python entry = %+v, want SymbolKind class", h)
	}
	shLine := "deploy\tdeploy.sh\t/^deploy() {$/;\"\tf\tline:2\tlanguage:Sh"
	if h := formatTagEntry(shLine); h.SymbolKind != "func" {
		t.Errorf("sh entry = %+v, want SymbolKind func", h)
	}
	jsLine := "helper\tutil.js\t/^function helper() {}$/;\"\tf\tline:3\tlanguage:JavaScript"
	if h := formatTagEntry(jsLine); h.SymbolKind != "func" {
		t.Errorf("js entry = %+v, want SymbolKind func", h)
	}
	jsConstLine := "NAME\tapp.js\t/^const NAME = \"gopi\";$/;\"\tC\tline:1\tlanguage:JavaScript"
	if h := formatTagEntry(jsConstLine); h.SymbolKind != "const" {
		t.Errorf("js const entry = %+v, want SymbolKind const", h)
	}
	tsLine := "Shape\tapp.ts\t/^interface Shape { area(): number; }$/;\"\ti\tline:1\tlanguage:TypeScript"
	if h := formatTagEntry(tsLine); h.SymbolKind != "interface" {
		t.Errorf("ts entry = %+v, want SymbolKind interface", h)
	}
}

// TestStalenessCache_TagsMissing: a missing tags file never walks the tree —
// it is regenerated unconditionally (behavior unchanged from pre-R6); the
// regenerated index's fresh verdict is then cached, so a same-round follow-up
// code_search skips the walk too.
func TestStalenessCache_TagsMissing(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	tagsPath := filepath.Join(dir, ".gopi-tags")

	writeAt := func(name string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("package a\nfunc Foo() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	writeAt("a.go", old)
	resetStalenessCache()
	stalenessWalkCount.Store(0)

	// Tags missing: regenerate unconditionally, no tree walk.
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (missing tags): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 0 {
		t.Fatalf("expected no walk for missing tags, got %d", got)
	}
	if _, err := os.Stat(tagsPath); err != nil {
		t.Fatalf("tags file not created: %v", err)
	}

	// Same-round follow-up: the fresh verdict is cached → still no walk.
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (follow-up): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 0 {
		t.Fatalf("expected cached follow-up to skip the walk, got %d walks", got)
	}
}

// TestStalenessCache_FreshVerdictReused: after one real walk, a second call
// inside the TTL with an unchanged tags mtime is served from the cache — the
// walk counter stays put and the index is not regenerated.
func TestStalenessCache_FreshVerdictReused(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	tagsPath := filepath.Join(dir, ".gopi-tags")

	writeAt := func(name string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("package a\nfunc Foo() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	writeAt("a.go", old)

	// Prime the cache with one real walk: first call generates the tags
	// (no walk), then reset and call again with tags present → 1 walk.
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("initial ensureTags: %v", err)
	}
	resetStalenessCache()
	stalenessWalkCount.Store(0)
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("priming ensureTags: %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 1 {
		t.Fatalf("expected 1 walk to prime the cache, got %d", got)
	}
	tagsMtime, err := os.Stat(tagsPath)
	if err != nil {
		t.Fatal(err)
	}
	mtime := tagsMtime.ModTime()

	// Cache hit: no walk, no regeneration.
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("cached ensureTags: %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 1 {
		t.Fatalf("expected the cached call to skip the walk, got %d walks", got)
	}
	if info, err := os.Stat(tagsPath); err != nil || !info.ModTime().Equal(mtime) {
		t.Fatalf("tags regenerated on a cache hit: mtime=%v err=%v", info.ModTime(), err)
	}
}

// TestStalenessCache_StaleAfterTTL: when the cached verdict has expired (walk
// time moved back past the TTL), the next call walks again and the
// regenerated index picks up the newer source file.
func TestStalenessCache_StaleAfterTTL(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	tagsPath := filepath.Join(dir, ".gopi-tags")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	writeAt := func(name string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("package a\nfunc Foo() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	writeAt("a.go", old)
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("initial ensureTags: %v", err)
	}
	if err := os.Chtimes(tagsPath, old, old); err != nil {
		t.Fatal(err)
	}
	resetStalenessCache()
	stalenessWalkCount.Store(0)

	// Newer source file, then expire the cached verdict deterministically.
	writeAt("a.go", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	stalenessCacheMu.Lock()
	e := stalenessCache[dir]
	e.walkedAt = time.Now().Add(-stalenessCacheTTL - time.Second)
	stalenessCache[dir] = e
	stalenessCacheMu.Unlock()

	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (expired verdict): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 1 {
		t.Fatalf("expected the expired verdict to re-walk (1), got %d", got)
	}
	if info, err := os.Stat(tagsPath); err != nil || !info.ModTime().After(old) {
		t.Fatalf("tags not regenerated after TTL expiry: mtime=%v err=%v", info.ModTime(), err)
	}
}

// TestStalenessWalk_IncrementalSkipsWalk: R3-7 — when the verdict TTL
// expires but the tree is unchanged, the re-derivation stats only the
// top-level directories instead of walking the whole tree; a structural
// change (new subdirectory) re-walks immediately; an in-place file edit does
// not bump directory mtimes and is (deliberately) deferred to the snapshot
// TTL.
func TestStalenessWalk_IncrementalSkipsWalk(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	tagsPath := filepath.Join(dir, ".gopi-tags")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	writeAt := func(name string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("package a\nfunc Foo() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	pkgDir := filepath.Join(dir, "pkg")
	if err := os.Mkdir(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAt("pkg/a.go", old)
	if err := os.Chtimes(pkgDir, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}

	// Initial index (tags missing → regenerate, no walk), then prime the
	// walk snapshot with one real walk.
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("initial ensureTags: %v", err)
	}
	if err := os.Chtimes(tagsPath, old, old); err != nil {
		t.Fatal(err)
	}
	resetStalenessCache()
	stalenessWalkCount.Store(0)
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("priming ensureTags: %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 1 {
		t.Fatalf("expected 1 walk to prime the snapshot, got %d", got)
	}

	// expireVerdict ages the cached verdict past its TTL so the next
	// ensureTags re-derives (the re-derivation re-caches a fresh verdict, so
	// every step below needs its own expiry).
	expireVerdict := func() {
		stalenessCacheMu.Lock()
		defer stalenessCacheMu.Unlock()
		e := stalenessCache[dir]
		e.walkedAt = time.Now().Add(-stalenessCacheTTL - time.Second)
		stalenessCache[dir] = e
	}

	// Verdict TTL expired, tree unchanged → incremental re-derivation, no
	// walk, no regeneration.
	expireVerdict()
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (expired verdict, unchanged tree): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 1 {
		t.Fatalf("unchanged tree must not re-walk on verdict expiry, got %d walks", got)
	}
	if info, err := os.Stat(tagsPath); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("tags regenerated without a tree change: mtime=%v err=%v", info.ModTime(), err)
	}

	// Structural change: a new subdirectory with a newer file bumps the
	// directory chain → immediate full walk + regeneration.
	newDir := filepath.Join(dir, "newpkg")
	if err := os.Mkdir(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAt("newpkg/b.go", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	expireVerdict()
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (new subdir): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 2 {
		t.Fatalf("new subdirectory must re-walk, got %d walks", got)
	}
	if info, err := os.Stat(tagsPath); err != nil || !info.ModTime().After(old) {
		t.Fatalf("tags not regenerated after a structural change: mtime=%v err=%v", info.ModTime(), err)
	}

	// In-place edit (no directory mtime bump) → deferred to the snapshot TTL;
	// the incremental check passes and no walk runs (documented tradeoff).
	writeAt("pkg/a.go", time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC))
	expireVerdict()
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (in-place edit): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 2 {
		t.Fatalf("in-place edit must not trigger a walk before the snapshot TTL, got %d walks", got)
	}
}

// TestStalenessWalk_SnapshotTTLExpires: R3-7 — the walk snapshot itself
// expires after stalenessWalkCacheTTL, so in-place edits are recognized with
// bounded delay (a full walk then picks up the newer file).
func TestStalenessWalk_SnapshotTTLExpires(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	tagsPath := filepath.Join(dir, ".gopi-tags")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	writeAt := func(name string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("package a\nfunc Foo() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	writeAt("a.go", old)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("initial ensureTags: %v", err)
	}
	if err := os.Chtimes(tagsPath, old, old); err != nil {
		t.Fatal(err)
	}
	resetStalenessCache()
	stalenessWalkCount.Store(0)
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("priming ensureTags: %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 1 {
		t.Fatalf("expected 1 walk to prime, got %d", got)
	}

	// In-place edit, then expire BOTH the verdict and the walk snapshot: the
	// re-derivation must run a real walk and regenerate the index.
	writeAt("a.go", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	stalenessCacheMu.Lock()
	e := stalenessCache[dir]
	e.walkedAt = time.Now().Add(-stalenessCacheTTL - time.Second)
	stalenessCache[dir] = e
	stalenessCacheMu.Unlock()
	walkCacheMu.Lock()
	s := walkCache[dir]
	s.takenAt = time.Now().Add(-stalenessWalkCacheTTL - time.Second)
	walkCache[dir] = s
	walkCacheMu.Unlock()

	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (expired snapshot): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 2 {
		t.Fatalf("expired snapshot must re-walk, got %d walks", got)
	}
	if info, err := os.Stat(tagsPath); err != nil || !info.ModTime().After(old) {
		t.Fatalf("tags not regenerated after the snapshot TTL: mtime=%v err=%v", info.ModTime(), err)
	}
}

// TestStalenessCache_TagsMtimeInvalidates: touching the tags file inside the
// TTL invalidates the cache via the mtime guard — the next call walks again
// (proving the cache is not a pure time window).
func TestStalenessCache_TagsMtimeInvalidates(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	tagsPath := filepath.Join(dir, ".gopi-tags")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	writeAt := func(name string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("package a\nfunc Foo() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	writeAt("a.go", old)
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("initial ensureTags: %v", err)
	}
	resetStalenessCache()
	stalenessWalkCount.Store(0)

	// Touch the tags file — mtime changes, cached verdict must be rejected
	// even though we are well inside the TTL.
	newer := time.Now()
	if err := os.Chtimes(tagsPath, newer, newer); err != nil {
		t.Fatal(err)
	}

	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (touched tags): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != 1 {
		t.Fatalf("expected the mtime change to force a re-walk (1), got %d", got)
	}
}

// --- EOF-probe boundary tests -------------------------------------------
//
// P0 (2026-09-21, task 01a0c37d): code_search panicked with
// "slice bounds out of range [:-3952]" in readTagLineAt and killed the whole
// agent process. The loop's EOF clamp `readLen = size - pos` had no lower
// guard: once pos advanced PAST size the expression went negative and
// buf[:readLen] panicked. pos overshoots because ReadAt reports io.EOF only on
// a SHORT read — a binary-search probe landing on an exactly-sized,
// unterminated tail read returns n == readLen with err == nil, so the loop's
// EOF branch never fires and pos += tagLineReadChunkBytes steps over size.
// The crash index was 0x41b000 = 4198400 bytes with its final newline at
// 4198399 (a 144-byte last chunk, hence [:-3952]).

// eofProbeOnce caches the mechanism fixture/report across the boundary tests
// (one generation per affected root, and the file's hexdump is FIXED pre-fix
// evidence for the report — regenerating it would rewrite the artifact the
// panic is quoted from).
var (
	eofProbeFixtureOnce = sync.OnceValue(func() string { return eofProbeFixture() })
	eofProbeHexOnce     = sync.OnceValue(func() string { return eofProbeHex() })
)

// eofProbeFixture returns a tags-shaped index whose final tagLineReadChunkBytes
// (4096) start exactly at a line head and carry NO newline: the production
// index's shape at the crash. Size is 8192 so the last chunk is a whole chunk —
// the probe that panics reads it as `f.ReadAt(buf, 4096)` (n == 4096, err ==
// nil) and then steps pos to 8192.
func eofProbeFixture() string {
	line := "a" + strings.Repeat("x", 62)   // 63 bytes
	head := strings.Repeat(line+"\n", 64)   // 64*64 = 4096, last newline at 4095
	tail := "q" + strings.Repeat("y", 4095) // 4096 bytes, unterminated
	return head + tail
}

// eofProbeHex returns the fixture's final 48 bytes, hexdumped — the byte-level
// observation the report quotes (device truth, not a rendering): the last
// newline sits at offset 4095 and the file ends mid-line.
func eofProbeHex() string {
	body := eofProbeFixture()
	buf := []byte(body[len(body)-48:])
	var sb strings.Builder
	for i := 0; i < len(buf); i += 16 {
		fmt.Fprintf(&sb, "%04x  % x\n", len(body)-48+i, buf[i:i+16])
	}
	return sb.String()
}

// writeEOFTagsFile writes content to a fresh .gopi-tags and returns the path.
func writeEOFTagsFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".gopi-tags")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// openEOFTags opens path and returns the file plus its size.
func openEOFTags(t *testing.T, path string) (*os.File, int64) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	//stylegate:ignore close of a file already written and flushed by the checked calls
	t.Cleanup(func() { f.Close() })
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return f, info.Size()
}

// TestReadTagLineAtAtEOF: off == size — the position a binary search returns
// when the target sorts after every entry (tagsLowerBound's documented
// "== size" contract). Contract: empty line, no panic, no error.
func TestReadTagLineAtAtEOF(t *testing.T) {
	path := writeEOFTagsFile(t, eofProbeFixture())
	f, size := openEOFTags(t, path)

	line, err := readTagLineAt(f, size, size)
	if err != nil {
		t.Fatalf("readTagLineAt(off=size=%d): unexpected error %v", size, err)
	}
	if line != "" {
		t.Fatalf("readTagLineAt(off=size=%d) = %q, want \"\" (off==size is EOF, not a line)", size, line)
	}
}

// TestReadTagLineAtPastEOF: off > size — the state the crash reached when pos
// stepped one chunk past a whole-chunk, unterminated, exactly-sized tail read.
// Contract: an out-of-range offset is reported as an ERROR, never a panic.
func TestReadTagLineAtPastEOF(t *testing.T) {
	path := writeEOFTagsFile(t, eofProbeFixture())
	f, size := openEOFTags(t, path)

	line, err := readTagLineAt(f, size+1, size)
	if err == nil {
		t.Fatalf("readTagLineAt(off=size+1=%d) = %q with nil error, want a bounds error", size+1, line)
	}
	if !strings.Contains(err.Error(), "tags") {
		t.Errorf("readTagLineAt(off>size) error %q does not name the caller context", err)
	}

	// The probe's FIRST chunk — the shape that actually killed production:
	// off = size - tagLineReadChunkBytes with the tail unterminated.
	line, err = readTagLineAt(f, size-tagLineReadChunkBytes, size)
	if err != nil {
		t.Fatalf("readTagLineAt(off=size-chunk): %v", err)
	}
	wantTail := "q" + strings.Repeat("y", 4095) // the unterminated final line
	if line != wantTail {
		t.Fatalf("readTagLineAt(off=size-chunk) = %.8q… (len %d), want the unterminated tail line (len %d)",
			line, len(line), len(wantTail))
	}
}

// TestTagsLowerBoundPastEveryEntryUnterminated: the caller-side shape — a
// symbol sorting after every entry on an index whose last line has no trailing
// newline. The returned offset must be safe to hand readTagLineAt.
func TestTagsLowerBoundPastEveryEntryUnterminated(t *testing.T) {
	const unterminated = "!_TAG_FILE_FORMAT\t2\t/extended/\n" +
		"Alpha\ta.go\t/^func Alpha() {}$/;\"\tf\tline:1\tlanguage:Go\n" +
		"Zeta\tz.go\t/^func Zeta() {}$/;\"\tf\tline:9\tlanguage:Go"
	// Control: the same content WITH its trailing newline (ordinary index).
	const terminated = unterminated + "\n"

	for _, tc := range []struct {
		name    string
		content string
	}{
		{"unterminated", unterminated},
		{"terminated", terminated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEOFTagsFile(t, tc.content)
			f, size := openEOFTags(t, path)

			start, err := tagsLowerBound(f, size, "zzz-after-everything")
			if err != nil {
				t.Fatalf("tagsLowerBound(after-everything): %v", err)
			}
			if start != size {
				t.Fatalf("tagsLowerBound(after-everything) = %d, want size (%d)", start, size)
			}
			// The contract: this offset must be readable, not a panic.
			line, err := readTagLineAt(f, start, size)
			if err != nil {
				t.Fatalf("readTagLineAt(off=tagsLowerBound=%d): %v", start, err)
			}
			if line != "" {
				t.Errorf("readTagLineAt(off==size) = %q, want \"\"", line)
			}
			// And the present symbol still resolves.
			start, err = tagsLowerBound(f, size, "Alpha")
			if err != nil {
				t.Fatalf("tagsLowerBound(Alpha): %v", err)
			}
			line, err = readTagLineAt(f, start, size)
			if err != nil {
				t.Fatalf("readTagLineAt(Alpha at %d): %v", start, err)
			}
			if tagLineKey(line) != "Alpha" {
				t.Errorf("lowerBound(Alpha) landed on %q", tagLineKey(line))
			}
		})
	}
}

// TestReadTagLineAtSingleLineNoNewline: a file consisting of exactly one line
// and no newline at all — the degenerate case where off == 0 and EOF coincide
// with the whole file.
func TestReadTagLineAtSingleLineNoNewline(t *testing.T) {
	const oneLine = "Only\tonly.go\t/^func Only() {}$/;\"\tf\tline:1\tlanguage:Go"
	path := writeEOFTagsFile(t, oneLine)
	f, size := openEOFTags(t, path)

	line, err := readTagLineAt(f, 0, size)
	if err != nil {
		t.Fatalf("readTagLineAt(single line, off=0): %v", err)
	}
	if line != oneLine {
		t.Fatalf("readTagLineAt(single line) = %q, want %q", line, oneLine)
	}
	// A probe at the single line's head must resolve it, and the lower bound
	// for a name after it must be size and remain readable.
	start, err := tagsLowerBound(f, size, "Only")
	if err != nil {
		t.Fatalf("tagsLowerBound(Only): %v", err)
	}
	if start != 0 {
		t.Errorf("tagsLowerBound(Only) = %d, want 0", start)
	}
	start, err = tagsLowerBound(f, size, "zzz")
	if err != nil {
		t.Fatalf("tagsLowerBound(zzz): %v", err)
	}
	if start != size {
		t.Errorf("tagsLowerBound(zzz) = %d, want %d", start, size)
	}
	if _, err := readTagLineAt(f, start, size); err != nil {
		t.Errorf("readTagLineAt(off=%d == size) = %v", start, err)
	}
	// Every probe point in [0, size] must be safe (the binary search's domain).
	for pos := int64(0); pos <= size+1; pos++ {
		if _, err := readTagLineAt(f, pos, size); err != nil && pos <= size {
			t.Fatalf("readTagLineAt(off=%d, size=%d): %v", pos, size, err)
		}
	}
}

// TestSearchTagIndexSurvivesEOFProbe drives the PRODUCTION entry
// (searchTagIndex — the function on the crash stack, reached from
// queryDefinition/searchTagIndex) over the exact index shape that killed the
// agent: a whole-chunk-length, unterminated final line, queried by a symbol
// that sorts after every entry. Pre-fix this panics; post-fix it is a clean
// miss and the defined symbol still resolves.
func TestSearchTagIndexSurvivesEOFProbe(t *testing.T) {
	path := writeEOFTagsFile(t, eofProbeFixture())

	// A symbol sorting after every entry: the binary search walks to EOF.
	hits, err := searchTagIndex(path, "zzz-not-present")
	if err != nil {
		t.Fatalf("searchTagIndex(zzz-not-present): %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("searchTagIndex(zzz-not-present) = %d hits, want a clean miss", len(hits))
	}

	// The unterminated tail line is still a real entry and must resolve.
	hits, err = searchTagIndex(path, "q"+strings.Repeat("y", 4095))
	if err != nil {
		t.Fatalf("searchTagIndex(unterminated tail): %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("searchTagIndex(unterminated tail) = %d hits, want 1", len(hits))
	}
}

// TestQueryDefinitionSurvivesEOFProbe is the end-to-end leg: queryDefinition —
// the function codeSearchImpl calls (search.go:105) — over a generated index
// whose final line is unterminated. Pre-fix the EOF probe panicked here too.
//
// The unterminated tail is also, by design, an UNFINISHED index (ctags appends
// whole lines), so the FIRST lookup self-heals by regenerating from the tree —
// which is why the "real definition" leg asserts against a symbol that exists in
// the sources below rather than against the planted fixture bytes.
func TestQueryDefinitionSurvivesEOFProbe(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("universal-ctags not on PATH")
	}
	resetStalenessCache()
	resetTagsIndexWatch()

	line := "a" + strings.Repeat("x", 62) // 63 bytes
	var sb strings.Builder
	sb.WriteString("!_TAG_FILE_FORMAT\t2\t/extended/\n")
	sb.WriteString("!_TAG_FILE_SORTED\t1\t/0=unsorted, 1=sorted/\n")
	sb.WriteString(strings.Repeat(line+"\n", 64))   // newline at 4095 of this run
	sb.WriteString("q" + strings.Repeat("y", 4095)) // unterminated final line

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gopi-tags"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// A real symbol for the regenerate leg below.
	if err := os.WriteFile(filepath.Join(root, "eofprobe.go"), []byte("package eofprobe\n\nfunc PkgThing() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	// A symbol sorting after every entry must be a clean miss, not a crash.
	// Then a REAL symbol must come back as a definition, not as a fallback
	// reference: the unterminated tail is an unfinished index, so the lookup
	// rebuilds it rather than reading a miss off a partial body.
	for _, tc := range []struct {
		sym  string
		want int
	}{
		{"zzz-not-present", 0},
		{"PkgThing", 1},
	} {
		hits, err := queryDefinition(ctx, root, tc.sym)
		if err != nil {
			t.Fatalf("queryDefinition(%s): %v", tc.sym, err)
		}
		if len(hits) != tc.want {
			t.Errorf("queryDefinition(%s) = %d hits, want %d", tc.sym, len(hits), tc.want)
		}
	}
}

// TestCodeSearchSurvivesEOFProbe is the outermost door (the surface the agent
// actually calls): CodeSearch -> codeSearchImpl -> queryDefinition ->
// searchTagIndex -> tagsLowerBound -> readTagLineAt. Over a root whose index
// ends in the unterminated, exactly-chunk-sized tail, a symbol sorting after
// every entry must be an ordinary miss. Pre-fix this panicked and took the
// calling process with it.
func TestCodeSearchSurvivesEOFProbe(t *testing.T) {
	requireCtags(t)
	resetStalenessCache()
	resetTagsIndexWatch()
	root := t.TempDir()

	// Source first, index second — the index must be the newer artifact so
	// ensureTags treats it as fresh and does not regenerate it away.
	src := filepath.Join(root, "pkg.go")
	if err := os.WriteFile(src, []byte("package pkg\n\nfunc PkgThing() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}

	line := "a" + strings.Repeat("x", 62)
	var sb strings.Builder
	sb.WriteString("!_TAG_FILE_FORMAT\t2\t/extended/\n")
	sb.WriteString("!_TAG_FILE_SORTED\t1\t/0=unsorted, 1=sorted/\n")
	sb.WriteString(strings.Repeat(line+"\n", 64))
	sb.WriteString("q" + strings.Repeat("y", 4095)) // unterminated final line
	if err := os.WriteFile(filepath.Join(root, ".gopi-tags"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	scope := SearchScope{RootPath: root, IncludeGlob: CodeSearchGlobs, MaxResult: 50}
	hits, _, _, err := CodeSearch(scope, "zzz-after-every-entry")
	if err != nil {
		t.Fatalf("CodeSearch(zzz-after-every-entry): %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("CodeSearch(zzz-after-every-entry) = %d hits, want 0", len(hits))
	}
}
