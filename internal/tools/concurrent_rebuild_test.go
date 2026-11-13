package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This file pins the CONCURRENT-REBUILD contract of the code_search index, the
// other half of the torn-index fix (torn_index_test.go pins the artifact
// probes themselves):
//
//	(a) the publish is ATOMIC — a reader in the same worktree never observes a
//	    partial index, however a peer's rebuild is interleaved with its reads;
//	(b) a PEER's freshly published COMPLETE index is accepted on its own bytes
//	    — the per-root size witness may not condemn an artifact that passes the
//	    tail and byte-order probes.
//
// Both were measured defects, not theory: with ctags writing `-f` IN PLACE, two
// gopi processes (two concurrent rebuilders, one worktree) made code_search
// refuse with `tags index incomplete (rebuilt 0s ago …)` on a perfectly healthy
// tree, which surfaced as an unrelated assertion failure in whatever test
// happened to call code_search (cmd/agent's TestCodeSearchBackendNoSymbol).

// The handshake between the test and its ctags stand-in is two marker FILES
// (paths passed through the environment, so the shell script needs no arguments
// beyond the ones ctags gets): the shim creates midwriteMarkerEnv once a partial
// write is on disk, and holds the write open until the test creates
// releaseMarkerEnv. That makes "read the index exactly while a rebuilder is in
// flight" deterministic instead of a race window.
const (
	midwriteMarkerEnv = "GOPI_TEST_CTAGS_MIDWRITE"
	releaseMarkerEnv  = "GOPI_TEST_CTAGS_RELEASE"
)

const (
	// rebuildHandshakePoll bounds each wait for a marker file; the wait itself
	// is bounded by rebuildHandshakeTimeout.
	rebuildHandshakePoll = 10 * time.Millisecond
	// rebuildHandshakeTimeout bounds every wait in this file — the shim's own
	// hold is shorter (testConcurrentRebuildCtagsShim), so a marker that never
	// arrives fails the test instead of hanging it.
	rebuildHandshakeTimeout = 20 * time.Second
)

// TestTagsIndexCompleteAcceptsPeerPublishedIndex is the size-witness contract:
// this process has already served one FINISHED index for the root (size A), a
// PEER then publishes a different, COMPLETE index (size B, the shape runCtags
// produces: temp file + rename, so the publication is a whole new file). The
// probe must judge the artifact in front of it — B's tail is a whole line and
// B's last entries are byte-ordered — instead of refusing it against the stale
// number A, which is what turned a healthy worktree red.
//
// Red on the pre-fix code: the size mismatch returned "unfinished" outright.
func TestTagsIndexCompleteAcceptsPeerPublishedIndex(t *testing.T) {
	dir := t.TempDir()
	tagsPath := filepath.Join(dir, tagsIndexFileName)
	resetTagsIndexWatch()
	resetStalenessCache()

	first := completeIndex()
	writeTornFixture(t, tagsPath, first)
	if !tagsIndexComplete(tagsPath) {
		t.Fatal("the fixture's first index was judged unfinished — the fixture is broken, not the probe")
	}

	second := []byte("!_TAG_FILE_FORMAT\t2\t/extended format/\n" +
		"!_TAG_FILE_SORTED\t1\t/0=unsorted, 1=sorted, 2=foldcase/\n" +
		"Foo\ta.go\t/^func Foo() {$/;\"\tf\tline:3\tlanguage:Go\n" +
		"Zoo\tb.go\t/^func Zoo() {$/;\"\tf\tline:7\tlanguage:Go\n")
	if len(second) == len(first) {
		t.Fatal("fixture error: the peer's index must differ in size, that is the whole scenario")
	}
	publishIndexAtomically(t, tagsPath, second)

	if !tagsIndexComplete(tagsPath) {
		t.Error("a peer's freshly published COMPLETE index was judged unfinished — the size witness condemned an artifact whose tail is a whole line and whose entries are byte-ordered (this is the concurrent-rebuild flake)")
	}
	// And the witness self-corrects: the artifact that was just accepted is the
	// new reference size, so a later replacement is detected again.
	if got, ok := completeTagsIndexSize(dir); !ok || got != int64(len(second)) {
		t.Errorf("completeTagsIndexSize = (%d, %v), want (%d, true) — the accepted artifact must become the new witness",
			got, ok, len(second))
	}
}

// TestCodeSearchServesACompleteIndexWhileAPeerRebuildsTheSameRoot is the epic's
// regression, carrying a concurrent rebuilder in the SAME worktree (Rule 5):
//
//  1. the root has a real ctags index, which this process has already served;
//  2. a second rebuilder starts a ctags run over that same root and HOLDS its
//     write open mid-flight (the shim, on a marker handshake);
//  3. the reader calls code_search while that write is in flight.
//
// Post-fix both assertions hold: the artifact published at the root is still the
// WHOLE previous index (the writer is filling a temp file, tagsIndexTempPattern,
// and only rename(2) can make it visible) and code_search answers from it.
//
// Red on the pre-fix code: runCtags pointed ctags' `-f` at the root's index
// itself, so step 3 read a header-valid partial, its completeness probe failed,
// and code_search either rebuilt the whole tree again (a second shim instance) or
// refused with `tags index incomplete` — a failure with nothing to do with the
// caller's query.
func TestCodeSearchServesACompleteIndexWhileAPeerRebuildsTheSameRoot(t *testing.T) {
	requireCtags(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	if err := os.WriteFile(src, []byte("package a\n\nfunc CsConcurrentRebuild() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tagsPath := filepath.Join(dir, tagsIndexFileName)
	resetTagsIndexWatch()
	resetStalenessCache()

	// The initial index comes from the REAL ctags (the shim is installed only
	// afterwards), so the bytes the reader serves are production-shaped.
	if err := runCtags(context.Background(), dir, tagsPath); err != nil {
		t.Fatalf("building the initial index: %v", err)
	}
	served, err := os.ReadFile(tagsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(served), "CsConcurrentRebuild") {
		t.Fatalf("the initial index does not hold the queried symbol:\n%s", served)
	}

	scope := SearchScope{RootPath: dir, IncludeGlob: CodeSearchGlobs, MaxResult: 50}
	if hits, _, _, err := CodeSearch(scope, "CsConcurrentRebuild"); err != nil || len(hits) == 0 {
		t.Fatalf("priming read over the freshly built index: hits=%d err=%v", len(hits), err)
	}

	markers := t.TempDir()
	mid, release := filepath.Join(markers, "midwrite"), filepath.Join(markers, "release")
	testConcurrentRebuildCtagsShim(t, mid, release)

	writerDone := make(chan error, 1)
	go func() { writerDone <- runCtags(context.Background(), dir, tagsPath) }()
	waitForMarker(t, mid, "the concurrent rebuilder never started writing")

	// (a) ATOMICITY — the published artifact is untouched while the writer is in
	//     flight. Pre-fix this read is a partial index (header plus the first
	//     entries the stand-in wrote).
	onDisk, err := os.ReadFile(tagsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(served) {
		t.Errorf("the published index changed while a concurrent rebuilder was still writing (%d bytes on disk, %d bytes served before) — the write is not atomic",
			len(onDisk), len(served))
	}

	// (b) The reader answers. Pre-fix this returns `tags index incomplete`
	//     after rebuilding the whole tree a second time.
	hits, _, _, err := CodeSearch(scope, "CsConcurrentRebuild")
	if err != nil {
		t.Errorf("CodeSearch while a peer rebuilt the same root: %v", err)
	}
	if len(hits) == 0 {
		t.Error("no hits — a concurrent rebuild made code_search lose a symbol that exists")
	} else if hits[0].SymbolKind == "reference" {
		t.Errorf("SymbolKind = %q (ripgrep fallback) — the tool degraded instead of serving the complete index", hits[0].SymbolKind)
	}

	releaseMarker(t, release)
	select {
	case err := <-writerDone:
		if err != nil {
			t.Errorf("the concurrent rebuilder failed: %v", err)
		}
	case <-time.After(rebuildHandshakeTimeout):
		t.Error("the concurrent rebuilder did not finish after the release marker")
	}
	// The rebuild DID publish a fresh artifact in the end — the atomic publish
	// must not have swallowed the writer's own output.
	afterRebuild, err := os.ReadFile(tagsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(afterRebuild), "CsConcurrentRebuild") {
		t.Errorf("the published index lost the symbol after the rebuild finished:\n%s", afterRebuild)
	}
}

// publishIndexAtomically places data at tagsPath the way runCtags does: a unique
// temp file in the SAME directory (so the rename is within one filesystem),
// then rename(2). The temp name is derived from the production index name, so
// the fixture cannot drift from the artifact it stands in for.
func publishIndexAtomically(t *testing.T, tagsPath string, data []byte) {
	t.Helper()
	tmp, err := os.CreateTemp(filepath.Dir(tagsPath), tagsIndexTempPattern)
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpPath, tagsPath); err != nil {
		t.Fatal(err)
	}
}

// testConcurrentRebuildCtagsShim installs a ctags stand-in that answers the
// capability probes and, on a generation run, writes a header-valid PREFIX to
// its `-f` target, announces the mid-write on mid, and holds the write open
// until release appears (bounded, so the test can never hang on it).
func testConcurrentRebuildCtagsShim(t *testing.T, mid, release string) {
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
printf '!_TAG_FILE_FORMAT\t2\t/extended format/\n!_TAG_FILE_SORTED\t1\t/0=unsorted/\n' > "$target"
printf 'Aaa000\ta.go\t/^func Aaa000() {}$/;"\tf\tline:1\tlanguage:Go\n' >> "$target"
: > "$GOPI_TEST_CTAGS_MIDWRITE"
i=0
while [ ! -f "$GOPI_TEST_CTAGS_RELEASE" ] && [ $i -lt 100 ]; do
  sleep 0.1
  i=$((i+1))
done
printf 'CsConcurrentRebuild\ta.go\t/^func CsConcurrentRebuild() {}$/;"\tf\tline:3\tlanguage:Go\n' >> "$target"
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "ctags"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(midwriteMarkerEnv, mid)
	t.Setenv(releaseMarkerEnv, release)
}

// waitForMarker blocks until the marker file exists, failing the test when it
// never does (a bounded wait — never an unbounded poll).
func waitForMarker(t *testing.T, path, what string) {
	t.Helper()
	deadline := time.Now().Add(rebuildHandshakeTimeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(rebuildHandshakePoll) //stylegate:allow-sleep polls for the rebuild handshake file the case waits on; the file is not written yet
	}
	t.Fatalf("%s (waited %s for %s)", what, rebuildHandshakeTimeout, path)
}

// releaseMarker lets the ctags stand-in finish its write.
func releaseMarker(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}
