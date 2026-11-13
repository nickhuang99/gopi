package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ctagsRefusalShim is a stand-in `ctags` that answers the capability probes
// like a real universal-ctags and, on a generation run (-f <target>), either
// writes a minimal valid index or reproduces ctags' own refusal:
//
//	ctags: "<path>" doesn't look like a tag file; I refuse to overwrite it.
//
// GOPI_TEST_CTAGS_ALWAYS_REFUSE=1 makes it refuse even when the target is
// absent, so the bounded-retry contract can be pinned. Every generation run
// appends one line to the file named by GOPI_TEST_CTAGS_LOG, so a test can
// assert HOW MANY times ctags was invoked (the epic's fix must cost exactly
// one extra attempt, never an unbounded loop).
const ctagsRefusalShim = `#!/bin/sh
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
echo "attempt" >> "$GOPI_TEST_CTAGS_LOG"
if [ -e "$target" ] || [ "$GOPI_TEST_CTAGS_ALWAYS_REFUSE" = "1" ]; then
  echo "ctags: \"$target\" doesn't look like a tag file; I refuse to overwrite it." >&2
  exit 1
fi
printf '!_TAG_FILE_FORMAT\t2\t/extended format/\n!_TAG_FILE_SORTED\t1\t/0=unsorted/\nFoo\ta.go\t/^func Foo() {$/;\"\tf\n' > "$target"
`

// testCtagsShim installs the shim as `ctags` and returns a path to the
// invocation log (one "attempt" line per generation run).
func testCtagsShim(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ctags"), []byte(ctagsRefusalShim), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "ctags.log")
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOPI_TEST_CTAGS_LOG", logPath)
	return logPath
}

func countAttempts(t *testing.T, logPath string) int {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "attempt")
}

// shimRoot creates a search root whose index is older than its source file,
// so ensureTags takes the staleness path into ctags. The planted index keeps
// a VALID header: it passes the pre-flight probe, which is exactly the case
// the pre-flight cannot predict — ctags refuses anyway, and the fix must
// self-heal there too (the epic's literal symptom: the refusal, not the NUL).
func shimRoot(t *testing.T, indexContent string) (string, string) {
	t.Helper()
	root := t.TempDir()
	tagsPath := filepath.Join(root, tagsIndexFileName)
	if err := os.WriteFile(tagsPath, []byte(indexContent), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate the index so the freshly written source below is newer = stale.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(tagsPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resetStalenessCache()
	tagsSelfHealCount.Store(0)
	return root, tagsPath
}

// TestEnsureTagsHealsCtagsRefusal covers the class the pre-atomic writer had to
// heal: ctags refusing a `-f` target it does not recognize as a tag file. The
// atomic publish removes the input to that refusal — ctags is handed a path
// that does NOT exist (runCtags removes CreateTemp's placeholder before the
// run, so ctags CREATEs its target exactly as it does for a missing index) —
// and this test pins that with the shim's own existence arm as the instrument:
// a shim that refuses whenever its target exists can only succeed if it was
// handed a fresh path. The regeneration must then cost exactly ONE attempt,
// with no self-heal and no retry, even though the root holds a planted,
// header-valid index (the artifact the old writer handed to ctags).
//
// The refusal machinery itself is still exercised by
// TestEnsureTagsCtagsRefusalRetryIsBounded (a ctags build that refuses an
// absent target must fail once, loudly, never loop).
func TestEnsureTagsHealsCtagsRefusal(t *testing.T) {
	logPath := testCtagsShim(t)
	validHeader := "!_TAG_FILE_FORMAT\t2\t/extended format/\n!_TAG_FILE_SORTED\t1\t/0=unsorted/\n"
	root, tagsPath := shimRoot(t, validHeader)

	got, err := ensureTags(context.Background(), root)
	if err != nil {
		t.Fatalf("ensureTags with a planted index at the root: %v", err)
	}
	if got != tagsPath {
		t.Errorf("ensureTags path = %q, want %q", got, tagsPath)
	}
	if !tagsIndexValid(tagsPath) {
		t.Error("index not regenerated")
	}
	if n := countAttempts(t, logPath); n != 1 {
		t.Errorf("ctags invocations = %d, want 1 — the writer must never hand ctags an existing target, so its refusal arm cannot fire", n)
	}
	if n := tagsSelfHealCount.Load(); n != 0 {
		t.Errorf("self-heal count = %d, want 0 (nothing to heal: the target is always fresh)", n)
	}
	data, err := os.ReadFile(tagsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Foo") {
		t.Errorf("the planted index was not replaced by the freshly generated one:\n%s", data)
	}
}

// TestEnsureTagsCtagsRefusalRetryIsBounded is the anti-infinite-loop guard: a
// ctags that refuses no matter what must fail ONCE with the underlying error
// surfaced (Rule 15 — no silent failure, no retry storm).
func TestEnsureTagsCtagsRefusalRetryIsBounded(t *testing.T) {
	logPath := testCtagsShim(t)
	t.Setenv("GOPI_TEST_CTAGS_ALWAYS_REFUSE", "1")
	validHeader := "!_TAG_FILE_FORMAT\t2\t/extended format/\n!_TAG_FILE_SORTED\t1\t/0=unsorted/\n"
	root, _ := shimRoot(t, validHeader)

	_, err := ensureTags(context.Background(), root)
	if err == nil {
		t.Fatal("ensureTags = nil error, want the ctags refusal surfaced when retrying cannot help")
	}
	if !strings.Contains(err.Error(), "doesn't look like a tag file") {
		t.Errorf("error = %v, want the ctags refusal visible to the caller", err)
	}
	if n := countAttempts(t, logPath); n != 2 {
		t.Errorf("ctags invocations = %d, want exactly 2 (one retry, no loop)", n)
	}
}

// TestQueryDefinitionSelfHealsUnsearchableIndex pins the second, independent
// failure path: an index that KEEPS a valid header yet cannot be searched
// (torn body / oversized line). The header probe passes, so only the query
// path can recover — it must rebuild once and return real hits.
func TestQueryDefinitionSelfHealsUnsearchableIndex(t *testing.T) {
	requireCtags(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc CsTornBody() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Valid header, intact probe window, then an oversized body line. The
	// corruption deliberately starts BEYOND tagHeaderProbeBytes so the
	// pre-flight accepts the file (exactly the shape the pre-flight cannot see)
	// and only the line scanner rejects it with "tags line exceeds ...".
	head := "!_TAG_FILE_FORMAT\t2\t/extended format/\n!_TAG_FILE_SORTED\t1\t/0=unsorted/\n"
	pad := strings.Repeat("#", tagHeaderProbeBytes) // inside the probe window, no NUL
	torn := []byte(head + pad + "\n" + strings.Repeat("x", tagMaxLineBytes*2) + "\n")
	if err := os.WriteFile(filepath.Join(dir, tagsIndexFileName), torn, 0o644); err != nil {
		t.Fatal(err)
	}
	resetStalenessCache()
	tagsSelfHealCount.Store(0)

	scope := SearchScope{RootPath: dir, IncludeGlob: CodeSearchGlobs, MaxResult: 50}
	hits, _, _, err := CodeSearch(scope, "CsTornBody")
	if err != nil {
		t.Fatalf("CodeSearch over a header-valid but unsearchable index: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits — the unsearchable index was not rebuilt")
	}
	if hits[0].SymbolKind == "reference" {
		t.Errorf("SymbolKind = %q (ripgrep fallback), want a ctags definition hit", hits[0].SymbolKind)
	}
}
