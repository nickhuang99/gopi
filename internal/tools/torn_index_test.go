package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The torn-index fixtures below reproduce ONE measured shape: ctags writes its
// -f target IN PLACE — open O_WRONLY|O_APPEND, ftruncate to 0, then append
// entries in name order as it walks the tree — so a reader that opens the file
// while that write is in flight (or after it was killed) sees the header block
// plus the entries written so far.
//
// Measured 2026-10-08 on this repo (universal-ctags 5.9.0): the rewrite takes
// ~10.2s inside a 12.5s run, the inode never changes, the size goes 17.7MB → 0
// and climbs back, and a sample taken mid-rewrite was 2.2MB with the intact
// header — i.e. a perfectly valid, perfectly byte-ordered PREFIX of the final
// index. The old pre-flight (header + NUL, first 256 bytes) accepted that file,
// and the binary search then missed symbols sorting after the writer's position,
// which is how code_search silently degraded to the ripgrep fallback.

// TestTagsIndexProbeOnRealCtagsBytes pins the probe contract on bytes a real
// ctags produced: a FINISHED index is accepted; the same output cut mid-write is
// rejected. Two tear shapes are covered, because they are caught by different
// halves of the probe:
//
//   - mid-ENTRY (the writer stopped inside a line): rejected outright by the
//     "ctags appends whole lines" check — no history needed;
//   - on a LINE BOUNDARY (the writer stopped exactly at a newline): the bytes
//     are then indistinguishable from a small complete index by shape alone, so
//     it is caught by the SIZE WITNESS — the size at which this process last saw
//     this root's index finished — which is exactly the situation a reader is in
//     when the file it already served is being rewritten underneath it.
func TestTagsIndexProbeOnRealCtagsBytes(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed — this test needs a real ctags index")
	}
	full := buildRealTagsIndex(t)

	dir := t.TempDir()
	tagsPath := filepath.Join(dir, tagsIndexFileName)
	writeTornFixture(t, tagsPath, full)
	if !tagsIndexValid(tagsPath) {
		t.Fatal("the header probe rejected a COMPLETE index")
	}
	if !tagsIndexComplete(tagsPath) {
		t.Fatal("a complete ctags index was judged unfinished")
	}

	// Shape 1 — truncation inside a line: the final byte is not a newline.
	cut := len(full) - 5
	if strings.HasSuffix(string(full[:cut]), "\n") {
		t.Fatalf("cut offset %d ends exactly on a newline — pick another", cut)
	}
	writeTornFixture(t, tagsPath, full[:cut])
	if !tagsIndexValid(tagsPath) {
		t.Fatal("the header probe rejected a torn prefix — it must still pass, that is the defect")
	}
	if tagsIndexComplete(tagsPath) {
		t.Error("a mid-entry torn prefix was judged FINISHED")
	}

	// Shape 2 — truncation on a line boundary, in a root whose finished size is
	// already known (the production sequence: a reader served this index, then a
	// new ctags run truncated it in place). The cut must remove at least one
	// whole line, so the truncated file is a SHORTER file of the same shape.
	//
	// The witness needs no plumbing: the probe above ACCEPTED the complete index
	// and recorded its size and node identity itself. That identity is what the
	// tear is caught by — Shape 1 and Shape 2 here both rewrite the very node
	// the reader served (os.SameFile), which is the half of the check that
	// survives a write stopping exactly on a line boundary.
	lb := strings.LastIndex(string(full[:len(full)-1]), "\n") + 1
	if lb <= 0 || lb >= len(full) {
		t.Fatalf("no interior line boundary in a %d-byte index", len(full))
	}
	writeTornFixture(t, tagsPath, full[:lb])
	if !strings.HasSuffix(string(full[:lb]), "\n") {
		t.Fatal("fixture error: the boundary cut must end with a newline")
	}
	if tagsIndexComplete(tagsPath) {
		t.Error("a line-boundary tear of a REPLACED index was judged FINISHED (size witness did not fire)")
	}

	// And the witness is not a permanent refusal: an index that IS the recorded
	// size is accepted again.
	writeTornFixture(t, tagsPath, full)
	if !tagsIndexComplete(tagsPath) {
		t.Error("the restored complete index was judged unfinished")
	}
}

// TestQueryDefinitionHealsMidEntryTornIndex is the epic's regression through the
// real CodeSearch path: with a torn index on disk (a byte-ordered prefix that
// stops inside a line), a symbol that EXISTS in the tree must not come back as a
// ripgrep-fallback "reference" — the tool must rebuild once and answer with a
// real definition.
func TestQueryDefinitionHealsMidEntryTornIndex(t *testing.T) {
	requireCtags(t)
	dir, tagsPath := tornRoot(t, "CsTornMidEntry", 300)
	resetStalenessCache()
	resetTagsIndexWatch()
	tagsSelfHealCount.Store(0)

	scope := SearchScope{RootPath: dir, IncludeGlob: CodeSearchGlobs, MaxResult: 50}
	hits, _, _, err := CodeSearch(scope, "CsTornMidEntry")
	if err != nil {
		t.Fatalf("CodeSearch over a torn index: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits — a torn index produced neither a rebuild nor a hit")
	}
	if hits[0].SymbolKind == "reference" {
		t.Errorf("SymbolKind = %q (ripgrep fallback) — the torn index degraded silently instead of self-healing; hits: %+v",
			hits[0].SymbolKind, hits)
	}
	if n := tagsSelfHealCount.Load(); n != 1 {
		t.Errorf("self-heal count = %d, want 1", n)
	}
	if !tagsIndexComplete(tagsPath) {
		t.Error("the index was not repaired on disk (still unfinished after the rebuild)")
	}
}

// TestQueryDefinitionDoesNotReadAMissOffATornIndex is the other half of "never
// degrade silently": a clean miss is a miss only on a FINISHED index. ctags
// appends entries in name order while it walks, so a symbol sorting after the
// writer's position is genuinely absent from the bytes on disk right now —
// reading that as "undefined" is exactly what makes code_search fall back to
// ripgrep and answer reference-only for a symbol that exists.
func TestQueryDefinitionDoesNotReadAMissOffATornIndex(t *testing.T) {
	requireCtags(t)
	// The index holds only "Aaa###" entries; the queried symbol sorts AFTER all
	// of them, so the lookup clean-misses at EOF — the dangerous shape.
	dir, _ := tornRoot(t, "ZzTornLateSymbol", 300)
	resetStalenessCache()
	resetTagsIndexWatch()
	tagsSelfHealCount.Store(0)

	scope := SearchScope{RootPath: dir, IncludeGlob: CodeSearchGlobs, MaxResult: 50}
	hits, _, _, err := CodeSearch(scope, "ZzTornLateSymbol")
	if err != nil {
		t.Fatalf("CodeSearch: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits — a clean miss over a torn index was served instead of re-checked")
	}
	if hits[0].SymbolKind == "reference" {
		t.Errorf("SymbolKind = %q (ripgrep fallback), want a ctags definition hit", hits[0].SymbolKind)
	}
}

// TestEnsureTagsRefusesRepeatedRebuildOfUnfinishedIndex pins the bound the fix
// must not lose: a rebuild costs a full-tree ctags run, so an index that is STILL
// unfinished right after a rebuild means a concurrent writer, and gopi must
// refuse loudly (Rule 15) instead of re-indexing the tree on every call (Rule 27
// — never manufacture load to satisfy a signal). A COMPLETE index lifts the
// refusal: it is about the artifact, not a permanent wedge.
//
// The throttle is armed by the first rebuild of an unfinished index — the
// expensive step — so the repair happens ONCE and every later call refuses
// loudly until a finished index appears.
func TestEnsureTagsRefusesRepeatedRebuildOfUnfinishedIndex(t *testing.T) {
	dir := t.TempDir()
	writeTornFixture(t, filepath.Join(dir, "a.go"), []byte("package a\n\nfunc Foo() {}\n"))
	// A header-valid index with no trailing newline (an unfinished write), newer
	// than the source so it is SERVED rather than regenerated for staleness.
	torn := strings.TrimSuffix(string(completeIndex()), "\n")
	writeTornFixture(t, filepath.Join(dir, tagsIndexFileName), []byte(torn))
	now := time.Now()
	if err := os.Chtimes(filepath.Join(dir, tagsIndexFileName), now, now); err != nil {
		t.Fatal(err)
	}
	resetStalenessCache()
	resetTagsIndexWatch()
	logPath := testTornCtagsShim(t)

	if _, err := ensureTags(context.Background(), dir); err != nil {
		t.Fatalf("the first repair should be allowed to run: %v", err)
	}
	if n := countAttempts(t, logPath); n != 1 {
		t.Fatalf("ctags invocations = %d, want 1 after the first repair", n)
	}

	_, err := ensureTags(context.Background(), dir)
	if err == nil {
		t.Fatal("ensureTags = nil error, want a loud refusal instead of a second rebuild")
	}
	if !strings.Contains(err.Error(), errTagsIndexIncomplete.Error()) {
		t.Errorf("error = %v, want it to name %q", err, errTagsIndexIncomplete)
	}
	if n := countAttempts(t, logPath); n != 1 {
		t.Errorf("ctags invocations = %d, want still 1 (refuse instead of rebuilding every call)", n)
	}

	// A finished index lifts the throttle: it is about the artifact, not a
	// permanent wedge.
	writeTornFixture(t, filepath.Join(dir, tagsIndexFileName), completeIndex())
	now = time.Now()
	if err := os.Chtimes(filepath.Join(dir, tagsIndexFileName), now, now); err != nil {
		t.Fatal(err)
	}
	resetStalenessCache()
	if _, err := ensureTags(context.Background(), dir); err != nil {
		t.Errorf("ensureTags after a complete index: %v (the throttle must lift)", err)
	}
}

// tornRoot builds a search root whose index is a byte-ordered prefix that stops
// INSIDE a line (no trailing newline) — the shape a ctags write in flight leaves
// — plus a Go file defining symbol. The prefix holds filler entries only, so the
// queried symbol cannot be found in it.
func tornRoot(t *testing.T, symbol string, filler int) (dir, tagsPath string) {
	t.Helper()
	dir = t.TempDir()
	writeTornFixture(t, filepath.Join(dir, "a.go"), []byte("package a\n\nfunc "+symbol+"() {}\n"))
	var sb strings.Builder
	sb.WriteString("!_TAG_FILE_FORMAT\t2\t/extended format/\n!_TAG_FILE_SORTED\t1\t/0=unsorted, 1=sorted, 2=foldcase/\n")
	for i := 0; i < filler; i++ {
		name := fmt.Sprintf("Aaa%03d", i)
		sb.WriteString(name + "\ta.go\t/^func " + name + "() {}$/;\"\tf\tline:1\tlanguage:Go\n")
	}
	sb.WriteString("Aaa999\tpartial") // the writer stops mid-entry: no newline
	tagsPath = filepath.Join(dir, tagsIndexFileName)
	writeTornFixture(t, tagsPath, []byte(sb.String()))
	// Keep the index NEWER than the source: the artifact must be served (and
	// therefore judged), not replaced for staleness before anything looks at it.
	now := time.Now()
	if err := os.Chtimes(tagsPath, now, now); err != nil {
		t.Fatal(err)
	}
	return dir, tagsPath
}

// buildRealTagsIndex generates a real ctags index over this repository once and
// returns its bytes (the fixture every real-byte assertion is cut from).
func buildRealTagsIndex(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "real.tags")
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("module root not found from %s: %v", root, err)
	}
	if err := runCtags(context.Background(), root, path); err != nil {
		t.Skipf("ctags failed over %s (%v) — the fixture premise cannot be built", root, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatal("ctags output does not end with a newline — the completion signal needs re-measuring")
	}
	return data
}

func writeTornFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// completeIndex is a minimal FINISHED index: a valid header block, one entry,
// and the trailing newline ctags' whole-line appending leaves behind.
func completeIndex() []byte {
	return []byte("!_TAG_FILE_FORMAT\t2\t/extended format/\n" +
		"!_TAG_FILE_SORTED\t1\t/0=unsorted, 1=sorted, 2=foldcase/\n" +
		"Foo\ta.go\t/^func Foo() {$/;\"\tf\tline:3\tlanguage:Go\n")
}

// testTornCtagsShim installs a ctags stand-in that answers the capability probes
// and, on a generation run, writes a header-valid index cut mid-entry — one that
// stays UNFINISHED however often it is run.
func testTornCtagsShim(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	shim := `#!/bin/sh
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
printf '!_TAG_FILE_FORMAT\t2\t/extended format/\n!_TAG_FILE_SORTED\t1\t/0=unsorted/\nZzz\ta.go\t/^func Zzz' > "$target"
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "ctags"), []byte(shim), 0o755); err != nil {
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
