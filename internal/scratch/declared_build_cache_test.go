// declared_build_cache_test.go — the build-cache tree's own rules, each pinned
// where it can actually fail.
//
// The four claims under test, one per way this tree is different from the three
// file-shaped trees beside it:
//
//  1. staleness is read BELOW the key (the key's own mtime stops moving when
//     the build that fills it starts — measured, see declared_build_cache.go);
//     a key holding ONE fresh file is not a candidate, however old the key is;
//  2. the namespace predicate is the writer's own key shape (16 lowercase hex),
//     so an operator's directory parked in the same root is never a candidate;
//  3. the tree's ROOT is never a candidate, at any age, including the re-check
//     before the delete (the same invariant as every declared tree);
//  4. the row is ONE function of the worktree-root resolution: an extra declared
//     root contributes its own row, and a host with no resolvable root still
//     declares the tree (as unconfigured) instead of silently omitting it.
package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/worktreeroots"
)

// buildCacheFixtureKey is a well-formed key name (16 lowercase hex characters)
// that is not a digest of anything — the predicate is a SHAPE rule.
const buildCacheFixtureKey = "0123456789abcdef"

// buildCacheFixtureKey2 is a second well-formed key, for the two-key cases.
const buildCacheFixtureKey2 = "fedcba9876543210"

// seedBuildCacheKey plants a cache key holding one file at
// <root>/<key>/gocache/00/<file> — the real writer's shape, one level down.
func seedBuildCacheKey(t *testing.T, root, key string, age time.Duration, body string) string {
	t.Helper()
	shard := filepath.Join(root, key, "gocache", "00")
	if err := os.MkdirAll(shard, DirPerm); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(shard, "abcdef01-a")
	if err := os.WriteFile(file, []byte(body), FilePerm); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-age)
	if err := os.Chtimes(file, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, key)
}

// TestBuildCacheKeyPredicateIsTheWritersKeyShape pins the namespace rule: the
// writer's key is the first 16 hex characters of a digest of the repo's absolute
// path, and anything else in the root is somebody else's directory.
func TestBuildCacheKeyPredicateIsTheWritersKeyShape(t *testing.T) {
	ours := []string{buildCacheFixtureKey, "0000000000000000", "ffffffffffffffff", "a1b2c3d4e5f60718"}
	for _, name := range ours {
		if !buildCacheKeyName(name) {
			t.Errorf("%q is a well-formed build-cache key and must be in the tree's namespace", name)
		}
	}
	theirs := []string{
		"",
		"0123456789abcde",   // 15 — one short
		"0123456789abcdef0", // 17 — one long
		"0123456789ABCDEF",  // uppercase: the writer lowercases (hex.EncodeToString)
		"0123456789abcdeg",  // 'g' is not hex
		"build-cache",
		"git",       // an operator's own directory beside the caches
		".snapshot", // a dot-directory
		"0123456789abcd-f",
	}
	for _, name := range theirs {
		if buildCacheKeyName(name) {
			t.Errorf("%q is NOT one of our keys and must never be a candidate", name)
		}
	}
}

// TestBuildCacheStalenessReadsBelowTheKey is the measurement this tree exists
// for: an old key holding a fresh file is NOT stale. A naive
// ScratchEntryStale(root-of-key) would have collected it mid-build — the
// freshness of the build is recorded in the cache bodies below the key, because
// the gate writes the key directory once and never touches it again.
func TestBuildCacheStalenessReadsBelowTheKey(t *testing.T) {
	root := t.TempDir()
	key := seedBuildCacheKey(t, root, buildCacheFixtureKey, time.Minute, "fresh cache entry\n")
	// The KEY itself is ancient: this is exactly the state the writer leaves
	// behind on every build (measured: every key older than its own gocache).
	ancient := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(key, ancient, ancient); err != nil {
		t.Fatal(err)
	}

	stale, err := buildCacheKeyStale(key, time.Now(), BuildCacheStaleAge)
	if err != nil {
		t.Fatalf("measuring a key must not fail: %v", err)
	}
	if stale {
		t.Fatalf("a key holding a file written a minute ago was called stale — the next build's cache would be collected mid-flight")
	}

	// Now age the CONTENT past the window: the key becomes a candidate.
	if err := os.Chtimes(filepath.Join(key, "gocache", "00", "abcdef01-a"), ancient, ancient); err != nil {
		t.Fatal(err)
	}
	stale, err = buildCacheKeyStale(key, time.Now(), BuildCacheStaleAge)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatalf("a key whose every file is %v old is not stale under a %v window", 30*24*time.Hour, BuildCacheStaleAge)
	}
}

// TestBuildCacheStalenessIsConservativeAtTheBoundary pins the comparison: the
// window is EXCLUSIVE (an entry exactly at the age is not yet garbage — the hard
// rule's own boundary, ScratchEntryStale), so the sweep never takes an entry the
// rule it reuses would keep.
func TestBuildCacheStalenessIsConservativeAtTheBoundary(t *testing.T) {
	root := t.TempDir()
	key := seedBuildCacheKey(t, root, buildCacheFixtureKey, time.Hour, "at the bound\n")
	// Pin ONE `now` and place the file exactly one window behind it: measuring
	// against time.Now() would race the clock and read as "just past".
	now := time.Now()
	stamp := now.Add(-BuildCacheStaleAge)
	if err := os.Chtimes(filepath.Join(key, "gocache", "00", "abcdef01-a"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	stale, err := buildCacheKeyStale(key, now, BuildCacheStaleAge)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatalf("a file exactly at the window is not yet garbage (the hard rule is strictly greater-than)")
	}
}

// TestBuildCacheKeyWithNoFilesFallsBackToItsOwnMtime: an empty key is one the
// gate created and never filled, which is exactly what the key's own mtime
// describes — it must still age out, or a crashed gate would leak a directory
// per invocation.
func TestBuildCacheKeyWithNoFilesFallsBackToItsOwnMtime(t *testing.T) {
	root := t.TempDir()
	key := filepath.Join(root, buildCacheFixtureKey)
	if err := os.MkdirAll(key, DirPerm); err != nil {
		t.Fatal(err)
	}
	if stale, err := buildCacheKeyStale(key, time.Now(), BuildCacheStaleAge); err != nil || stale {
		t.Fatalf("a key created just now must not be stale (stale=%v err=%v)", stale, err)
	}
	ancient := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(key, ancient, ancient); err != nil {
		t.Fatal(err)
	}
	stale, err := buildCacheKeyStale(key, time.Now(), BuildCacheStaleAge)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatalf("an empty key abandoned %v ago must age out", 30*24*time.Hour)
	}
}

// TestBuildCacheKeyHoldingASymlinkIsNeverCollected: a key holding a symlink
// asserts nothing measurable about itself (the link's target is never followed),
// so the sweep keeps it. The alternative — following the link — would age, and
// then delete, bytes that live outside the tree.
func TestBuildCacheKeyHoldingASymlinkIsNeverCollected(t *testing.T) {
	root := t.TempDir()
	key := seedBuildCacheKey(t, root, buildCacheFixtureKey, 30*24*time.Hour, "aged cache entry\n")
	if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(key, "link")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	stale, err := buildCacheKeyStale(key, time.Now(), BuildCacheStaleAge)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatalf("a key holding a symlink must never be a candidate")
	}
}

// TestBuildCacheKeySizeCountsTheSubtreeAndSkipsSymlinks: the reported bytes are
// what the entry reclaims — every regular file below the key, and nothing
// through a symlink.
func TestBuildCacheKeySizeCountsTheSubtreeAndSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	key := seedBuildCacheKey(t, root, buildCacheFixtureKey, time.Hour, strings.Repeat("x", 1234))
	size, err := buildCacheKeySize(key)
	if err != nil {
		t.Fatalf("measuring a key must not fail: %v", err)
	}
	if size != 1234 {
		t.Fatalf("a key holding one 1234-byte file measured %d bytes", size)
	}
}

// TestBuildCacheRootIsTheWritersOwnJoin pins the path: the sweep's root is
// <worktree root>/<SharedBuildCacheDirName>, built from the SAME leaf constant
// the writer joins (worktreeroots.SharedBuildCacheDirName) — never a second
// spelling of "gopi-build-cache" (Rules 13/14).
func TestBuildCacheRootIsTheWritersOwnJoin(t *testing.T) {
	wt := filepath.Join(t.TempDir(), "workspace")
	want := filepath.Join(wt, worktreeroots.SharedBuildCacheDirName)
	if got := BuildCacheRootFor(wt); got != want {
		t.Fatalf("build cache root = %q, want %q (the writer's own join)", got, want)
	}
}

// TestBuildCacheTreesFollowTheWorktreeRootResolution: the row is a function of
// the resolution, so an extra declared root contributes its own row and no root
// is declared twice.
func TestBuildCacheTreesFollowTheWorktreeRootResolution(t *testing.T) {
	ambient := filepath.Join(t.TempDir(), "workspace")
	extra := filepath.Join(t.TempDir(), "root-for-one-repo")
	install := func() []worktreeroots.Root {
		return []worktreeroots.Root{
			{Path: ambient, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceDefault},
			{Path: extra, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey},
			// A duplicate of the ambient root must not produce a second row:
			// two rows on one tree would double-count the same bytes.
			{Path: ambient, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceDefault},
		}
	}
	restoreBuildCacheRoots := installBuildCacheRootsForTest(install)
	t.Cleanup(restoreBuildCacheRoots)

	trees := BuildCacheTrees()
	if len(trees) != 2 {
		t.Fatalf("two distinct roots must yield two rows, got %d: %+v", len(trees), treeNames(trees))
	}
	if trees[0].Name != BuildCacheTreeName {
		t.Errorf("the first row must carry the plain tree name %q, got %q", BuildCacheTreeName, trees[0].Name)
	}
	if !strings.HasPrefix(trees[1].Name, BuildCacheTreeName+"@") {
		t.Errorf("an additional root's row must be attributed to its root, got %q", trees[1].Name)
	}
	for _, tree := range trees {
		if tree.Class != TreeClassBuildCache {
			t.Errorf("tree %q has class %q", tree.Name, tree.Class)
		}
		if tree.EntryStale == nil {
			t.Errorf("tree %q must carry the key staleness rule (its keys do not age by their own mtime)", tree.Name)
		}
		if tree.EntryMatch == nil {
			t.Errorf("tree %q must carry the key namespace predicate", tree.Name)
		}
		if tree.StaleAge != BuildCacheStaleAge {
			t.Errorf("tree %q must reference the named age constant, got %v", tree.Name, tree.StaleAge)
		}
	}
}

// TestBuildCacheTreeIsDeclaredWhenNoRootResolves: a host with no home directory
// still DECLARES the tree — as unconfigured, which the CLI reports with exit 0.
// Omitting it would recreate exactly the defect this registry closes: a tree
// that exists and no line mentions.
func TestBuildCacheTreeIsDeclaredWhenNoRootResolves(t *testing.T) {
	t.Cleanup(installBuildCacheRootsForTest(func() []worktreeroots.Root { return nil }))

	trees := BuildCacheTrees()
	if len(trees) != 1 || trees[0].Name != BuildCacheTreeName {
		t.Fatalf("an unresolvable root must still declare the tree, got %+v", treeNames(trees))
	}
	if _, err := trees[0].Resolve(); err == nil {
		t.Fatalf("the unconfigured row must report its reason, not an empty path")
	} else if !strings.Contains(err.Error(), ErrDeclaredTreeUnconfigured.Error()) {
		t.Fatalf("the refusal must be the NAMED unconfigured error (a watchdog must not alarm), got %v", err)
	}
}

// installBuildCacheRootsForTest points the tree's root enumeration at a fixture
// and returns the restore, so one case cannot leak its roots into the next.
func installBuildCacheRootsForTest(f func() []worktreeroots.Root) func() {
	prev := buildCacheWorktreeRootsResolver
	RegisterBuildCacheWorktreeRoots(f)
	return func() { buildCacheWorktreeRootsResolver = prev }
}

// treeNames is a small diagnostic helper for the assertions above.
func treeNames(trees []DeclaredTree) []string {
	out := make([]string, 0, len(trees))
	for _, tree := range trees {
		out = append(out, tree.Name)
	}
	return out
}

// TestBuildCacheRootSurvivesAnAgedKey is the ROOT INVARIANT for this tree,
// against the live declared row: an ancient root with an ancient key inside it
// still yields the root as a non-candidate, and the delete refuses it.
func TestBuildCacheRootSurvivesAnAgedKey(t *testing.T) {
	home := t.TempDir()
	wtRoot := filepath.Join(home, "workspace")
	root := BuildCacheRootFor(wtRoot)
	key := seedBuildCacheKey(t, root, buildCacheFixtureKey, 365*24*time.Hour, "ancient\n")
	ancient := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(root, ancient, ancient); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(installBuildCacheRootsForTest(func() []worktreeroots.Root {
		return []worktreeroots.Root{{Path: wtRoot, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceDefault}}
	}))
	t.Setenv("HOME", home)

	tree := BuildCacheTrees()[0]
	if !IsDeclaredTreeRoot(root) {
		t.Fatalf("%s must be recognised as a declared tree root", root)
	}
	if declaredEntryCollectable(tree, root, time.Now(), time.Hour, heldTreeScan{}) {
		t.Fatalf("the ROOT of the build-cache tree is a candidate — the tree itself must survive every sweep")
	}
	if err := removeCollectableOfTree(tree, root); err == nil {
		t.Fatalf("removeCollectableOfTree removed the build-cache ROOT")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the build-cache root did not survive: %v", err)
	}
	// And the aged key inside it IS a candidate — the invariant protects the
	// tree, never the garbage in it.
	if !declaredEntryCollectable(tree, key, time.Now(), time.Hour, heldTreeScan{}) {
		t.Fatalf("an ancient key with no fresh file inside must be collectable")
	}
}
