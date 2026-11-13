package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These tests pin the second half of the atomic-publish change: the publish
// itself must not be visible as a TREE CHANGE to the staleness snapshot.
//
// The measurement they come from (2026-10-12): with the temp+rename publish,
// `TestStalenessWalk_IncrementalSkipsWalk` went red with one extra walk —
// creating a temp beside the index and renaming it on bumps the ROOT
// DIRECTORY's mtime, and that mtime was half of the snapshot's validity test,
// so every regeneration invalidated the snapshot it had just been computed
// against and forced a full-tree walk on the next call.
//
// The fix records the root's OWN non-directory names (index artifacts excluded)
// instead of its mtime, and these tests hold both directions of that: a publish
// must NOT invalidate the snapshot, and a real source-file change still MUST.

// stalenessTestWrite writes one file and dates it, so the staleness walk has a
// deterministic newest-source mtime to compare the index against.
func stalenessTestWrite(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte("package a\nfunc Foo() {}\n"), tagsIndexFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// stalenessTestExpireVerdict ages the cached verdict for dir past its TTL, so
// the next ensureTags re-derives the staleness verdict (and so consults the
// walk snapshot). Mirrors the helper the existing staleness tests use.
func stalenessTestExpireVerdict(dir string) {
	stalenessCacheMu.Lock()
	defer stalenessCacheMu.Unlock()
	e := stalenessCache[dir]
	e.walkedAt = time.Now().Add(-stalenessCacheTTL - time.Second)
	stalenessCache[dir] = e
}

// stalenessTestPrimeIndex produces a clean starting point: a root with one
// source file, a published index, and a walk snapshot primed by exactly one
// real walk (walks counted from zero).
func stalenessTestPrimeIndex(t *testing.T) (dir string, walks int64) {
	t.Helper()
	ctx := context.Background()
	dir = t.TempDir()
	old := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	pkgDir := filepath.Join(dir, "pkg")
	if err := os.Mkdir(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stalenessTestWrite(t, filepath.Join(pkgDir, "a.go"), old)
	if err := os.Chtimes(pkgDir, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	// Initial index (no index yet → regenerate), then prime the snapshot.
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("initial ensureTags: %v", err)
	}
	if err := os.Chtimes(filepath.Join(dir, tagsIndexFileName), old, old); err != nil {
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
	return dir, 1
}

// TestIndexPublishDoesNotInvalidateTheWalkSnapshot: a regeneration goes through
// the ATOMIC PUBLISH (a temp file created in the root and renamed onto the
// index), which is a change to the root DIRECTORY — and it must not be read as
// a change to the TREE. RED before the fix: the publish bumped the root mtime,
// the next staleness derivation walked the whole tree again.
//
// The same test holds the OPPOSITE direction, because suppressing by name is
// only correct if the names still cover what the walk reads: a source file
// added at the root must still invalidate the snapshot immediately (no TTL
// wait), and so must an in-place edit that moves the newest mtime.
func TestIndexPublishDoesNotInvalidateTheWalkSnapshot(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	ctx := context.Background()
	dir, walks := stalenessTestPrimeIndex(t)

	// Make the index stale so the next ensureTags regenerates it THROUGH THE
	// PUBLISH. That regeneration legitimately re-walks (a source moved), so the
	// snapshot it stores is taken by that walk — and the publish lands after it.
	newer := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	stalenessTestWrite(t, filepath.Join(dir, "pkg", "a.go"), newer)
	if err := os.Chtimes(filepath.Join(dir, "pkg"), newer, newer); err != nil {
		t.Fatal(err)
	}
	stalenessTestExpireVerdict(dir)
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (regenerate through the publish): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != walks+1 {
		t.Fatalf("the regeneration itself must walk once, got %d walks (want %d)", got, walks+1)
	}
	walks = stalenessWalkCount.Load()

	// The tree is now UNCHANGED, and the index we just published must not
	// invalidate the snapshot the regeneration computed. RED pre-fix: this is
	// the extra walk (`the publish is not a tree change`).
	stalenessTestExpireVerdict(dir)
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (unchanged tree after a publish): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != walks {
		t.Fatalf("the index publish must not invalidate the walk snapshot, got %d walks (want %d) — the publish is being read as a tree change", got, walks)
	}

	// An in-flight temp (what a CONCURRENT rebuilder leaves in the root while it
	// writes) is the same artifact class as the index and must be ignored too.
	tmpPath := filepath.Join(dir, tagsIndexTempPrefix+"probe")
	if err := os.WriteFile(tmpPath, []byte("!_TAG_FILE_FORMAT\n"), tagsIndexFileMode); err != nil {
		t.Fatal(err)
	}
	stalenessTestExpireVerdict(dir)
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (in-flight temp present): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != walks {
		t.Fatalf("a peer's in-flight temp must not invalidate the walk snapshot, got %d walks (want %d)", got, walks)
	}
	if err := os.Remove(tmpPath); err != nil {
		t.Fatal(err)
	}

	// The other direction — suppression must not swallow a REAL change: a new
	// source file at the root is not a name the snapshot recorded.
	stalenessTestWrite(t, filepath.Join(dir, "b.go"), newer)
	stalenessTestExpireVerdict(dir)
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (new source file at the root): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != walks+1 {
		t.Fatalf("a new source file at the root must still invalidate the snapshot, got %d walks (want %d)", got, walks+1)
	}
	walks = stalenessWalkCount.Load()

	// …and so must a REMOVAL, which is the same name-set comparison read the
	// other way (a file the snapshot recorded that is no longer there).
	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil {
		t.Fatal(err)
	}
	stalenessTestExpireVerdict(dir)
	if _, err := ensureTags(ctx, dir); err != nil {
		t.Fatalf("ensureTags (source file removed): %v", err)
	}
	if got := stalenessWalkCount.Load(); got != walks+1 {
		t.Fatalf("removing a source file at the root must invalidate the snapshot, got %d walks (want %d)", got, walks+1)
	}
}
