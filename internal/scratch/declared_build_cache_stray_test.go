// declared_build_cache_stray_test.go — the ROOT-LEVEL STRAY SHARD row: what it
// must collect, what it must never touch, and the writer-side shape it must
// agree with.
//
// THE DEFECT THESE ARMS PIN (measured on this host 2026-09-24): a whole GOCACHE
// sat at the build cache ROOT — 256 two-hex shard directories, 6902 files,
// 733 MiB — one level ABOVE `<root>/<repo-key>/gocache`. The gate's own bound
// walks `<root>/<key>/<cache>/<shard>/<file>` and is structurally blind to it,
// and the KEY row's predicate is 16 hex characters, so it could never match
// either. It was never measured, never trimmed and read by no build.
package scratch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// strayFixtureKeyHex is a 16-lowercase-hex directory name — the shape the
	// gate mints as a per-repo cache key (mergeBuildGateGoCacheRepoKey).
	strayFixtureKeyHex = "0123456789abcdef"
	// strayFixtureAge is how far back a fixture's mtimes are pushed so its
	// entries are unambiguously older than the window.
	strayFixtureAge = 48 * time.Hour
)

// strayFixtureRoot builds a real build cache ROOT under a fresh worktree root
// and returns (worktreeRoot, cacheRoot): the root holds a per-repo key (the
// writer's own shape) plus whatever the caller seeds beside it.
func strayFixtureRoot(t *testing.T) (string, string) {
	t.Helper()
	wt := t.TempDir()
	restore := installBuildCacheRootsForTest(func() []worktreeroots.Root {
		return []worktreeroots.Root{{Path: wt, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceDefault}}
	})
	t.Cleanup(restore)
	root := BuildCacheRootFor(wt)
	if err := os.MkdirAll(filepath.Join(root, strayFixtureKeyHex, cacheDirNameForTest), DirPerm); err != nil {
		t.Fatalf("mkdir fixture key: %v", err)
	}
	return wt, root
}

// cacheDirNameForTest is the cache leaf the gate writes below a key. The value
// lives in internal/tools (mergeBuildGateCacheDirName) and is spelled here as a
// literal ON PURPOSE: this package is the leaf and must not import internal/tools
// (tools imports scratch). The pair is held together by the writer's own test,
// which asserts the leaf it writes is the one this fixture's key holds.
const cacheDirNameForTest = "gocache"

// seedStrayShard creates one shard-shaped directory holding one entry file, aged
// by age. It returns the directory's path.
func seedStrayShard(t *testing.T, root, name string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		t.Fatalf("mkdir shard %s: %v", name, err)
	}
	entry := filepath.Join(dir, "0123456789abcdef-a")
	if err := os.WriteFile(entry, make([]byte, 4096), FilePerm); err != nil {
		t.Fatalf("write entry in %s: %v", name, err)
	}
	when := time.Now().Add(-age)
	for _, p := range []string{entry, dir} {
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatalf("age %s: %v", p, err)
		}
	}
	return dir
}

// strayTreeFor returns the installed stray-shard row (there is exactly one when
// one worktree root resolves).
func strayTreeFor(t *testing.T) DeclaredTree {
	t.Helper()
	trees := BuildCacheStrayShardTrees()
	if len(trees) != 1 {
		t.Fatalf("expected one stray-shard row for one resolved root, got %d", len(trees))
	}
	return trees[0]
}

// TestBuildCacheShardPredicateIsTheWritersShardShape pins the shape both sides
// are built from: the row's predicate is the toolchain's two-lowercase-hex shard
// name, and the length it is spelled with is the exported constant (so the gate
// that REPORTS the shape and this sweep that COLLECTS it cannot drift).
func TestBuildCacheShardPredicateIsTheWritersShardShape(t *testing.T) {
	if BuildCacheShardHexLen != len("ab") {
		t.Fatalf("BuildCacheShardHexLen = %d, want 2 (the toolchain's shard level)", BuildCacheShardHexLen)
	}
	for _, name := range []string{"00", "0f", "ab", "ff"} {
		if !buildCacheShardName(name) || !BuildCacheShardNameIsOurs(name) {
			t.Errorf("%q must be in the stray-shard row's namespace", name)
		}
	}
	for _, name := range []string{"ABC", "AB", "a", "abc", "0g", "a-f", strayFixtureKeyHex, "gocache", "log.txt", ""} {
		if buildCacheShardName(name) || BuildCacheShardNameIsOurs(name) {
			t.Errorf("%q must NOT be in the stray-shard row's namespace (a repo key is 16 hex, a shard is 2)", name)
		}
	}
	// DISJOINTNESS: the two rows of the same tree can never claim one directory,
	// so no path is ever collected twice and no repo key is ever taken by the
	// shard row (or the reverse).
	for _, name := range []string{"00", "ab", strayFixtureKeyHex, "notakey"} {
		if buildCacheShardName(name) && buildCacheKeyName(name) {
			t.Errorf("%q is in BOTH rows' namespaces: the key row and the shard row must be disjoint", name)
		}
	}
}

// TestBuildCacheStrayShardSweepTakesTheLeakAndNothingElse is the row's whole
// contract on a fixture: the aged stray shard GOES, and every neighbour stays —
// a fresh shard (still a live cache), the writer's own repo key, the toolchain's
// bookkeeping FILES, and an operator's non-shard directory.
func TestBuildCacheStrayShardSweepTakesTheLeakAndNothingElse(t *testing.T) {
	wt, root := strayFixtureRoot(t)
	tree := strayTreeFor(t)

	aged := seedStrayShard(t, root, "ab", strayFixtureAge)
	fresh := seedStrayShard(t, root, "0f", 0)
	dirShapedFile := filepath.Join(root, "cd")
	if err := os.WriteFile(dirShapedFile, []byte("not a shard: a shard is a DIRECTORY"), FilePerm); err != nil {
		t.Fatalf("write flat two-hex file: %v", err)
	}
	key := filepath.Join(root, strayFixtureKeyHex)
	operatorDir := filepath.Join(root, "my-notes")
	if err := os.MkdirAll(operatorDir, DirPerm); err != nil {
		t.Fatalf("mkdir operator dir: %v", err)
	}
	keep := []string{root, fresh, dirShapedFile, key, operatorDir}

	removed, freed, err := tree.Sweep(BuildCacheStaleAge)
	if err != nil {
		t.Fatalf("stray-shard sweep failed: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed=%d, want exactly the one aged stray shard", removed)
	}
	if freed <= 0 {
		t.Fatalf("freed=%d bytes, want the shard's own bytes", freed)
	}
	if _, err := os.Stat(aged); !os.IsNotExist(err) {
		t.Fatalf("the aged stray shard %s survived the sweep (%v)", aged, err)
	}
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("the sweep removed %s, which is not a stray shard: %v", p, err)
		}
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the worktree root %s did not survive: %v", wt, err)
	}
}

// TestBuildCacheStrayShardsPlanIsTheSweepsOwnSet: the plan face reports exactly
// the set the collect face takes — same predicate, same window. A row whose plan
// said one thing and whose sweep did another would be the one disagreement an
// operator cannot act on.
func TestBuildCacheStrayShardsPlanIsTheSweepsOwnSet(t *testing.T) {
	_, root := strayFixtureRoot(t)
	tree := strayTreeFor(t)
	aged := seedStrayShard(t, root, "1c", strayFixtureAge)
	seedStrayShard(t, root, "2d", 0)

	plan, err := DeclaredTreePlan(tree)
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if len(plan) != 1 || plan[0].Path != aged {
		t.Fatalf("plan = %+v, want exactly the aged stray shard %s", plan, aged)
	}
	if plan[0].Size <= 0 {
		t.Errorf("the planned entry carries no measured size (%+v): the report must measure what the sweep frees", plan[0])
	}
	want := TreePlanTotal(plan)

	removed, freed, err := tree.Sweep(BuildCacheStaleAge)
	if err != nil {
		t.Fatalf("sweep failed: %v", err)
	}
	if removed != len(plan) {
		t.Fatalf("the sweep removed %d entries while its plan named %d — the two faces disagree", removed, len(plan))
	}
	if freed != want {
		t.Fatalf("the sweep reports %d bytes freed while its plan measured %d", freed, want)
	}
}

// TestBuildCacheStrayShardSweepNeverTakesTheRoot: the declared-tree invariant,
// driven through this row: the cache ROOT itself is never a candidate at any age
// — even when it is the only thing aged in its tree.
func TestBuildCacheStrayShardSweepNeverTakesTheRoot(t *testing.T) {
	_, root := strayFixtureRoot(t)
	tree := strayTreeFor(t)
	old := time.Now().Add(-strayFixtureAge * 4)
	if err := os.Chtimes(root, old, old); err != nil {
		t.Fatalf("age the cache root: %v", err)
	}
	removed, freed, err := tree.Sweep(BuildCacheStaleAge)
	if err != nil {
		t.Fatalf("sweep failed: %v", err)
	}
	if removed != 0 || freed != 0 {
		t.Fatalf("removed=%d freed=%d, want 0/0: the cache ROOT is never a candidate, however old it is", removed, freed)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the cache ROOT was removed: %v", err)
	}
	if err := removeCollectableOfTree(tree, root); err == nil {
		t.Fatalf("removeCollectableOfTree accepted the cache ROOT — the last-moment check must refuse it")
	}
}

// TestBuildCacheStrayShardSweepIsACleanNoopWithoutARoot: a host whose gate never
// ran has no cache root at all, and a sweep of it is a no-op rather than an
// error (the key row already reports the tree as unconfigured, once).
func TestBuildCacheStrayShardSweepIsACleanNoopWithoutARoot(t *testing.T) {
	wt := t.TempDir()
	t.Cleanup(installBuildCacheRootsForTest(func() []worktreeroots.Root {
		return []worktreeroots.Root{{Path: wt, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceDefault}}
	}))
	tree := strayTreeFor(t)
	removed, freed, err := tree.Sweep(BuildCacheStaleAge)
	if err != nil {
		t.Fatalf("a missing cache root must be a no-op, got %v", err)
	}
	if removed != 0 || freed != 0 {
		t.Fatalf("removed=%d freed=%d, want 0/0 for a root that does not exist", removed, freed)
	}
	if plan, err := DeclaredTreePlan(tree); err != nil || len(plan) != 0 {
		t.Fatalf("the plan of a missing root must be empty and error-free, got %+v / %v", plan, err)
	}
}

// TestBuildCacheStrayShardRowIsDeclaredAndAttributed: the row reaches the ONE
// declared set (so `gopi tool scratch-gc trees` walks it) and keeps the tree's name,
// class and window.
func TestBuildCacheStrayShardRowIsDeclaredAndAttributed(t *testing.T) {
	wt := t.TempDir()
	t.Cleanup(installBuildCacheRootsForTest(func() []worktreeroots.Root {
		return []worktreeroots.Root{{Path: wt, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceDefault}}
	}))
	declared, ok := treeByName(BuildCacheTreeName + BuildCacheStrayShardTreeNameSuffix)
	if !ok {
		t.Fatalf("the stray-shard row %q is not in DeclaredTrees() — the sweep would never walk it",
			BuildCacheTreeName+BuildCacheStrayShardTreeNameSuffix)
	}
	if declared.Class != TreeClassBuildCache {
		t.Errorf("the row's class is %q, want %q", declared.Class, TreeClassBuildCache)
	}
	if declared.StaleAge != BuildCacheStaleAge {
		t.Errorf("the row must reference the named age constant, got %v", declared.StaleAge)
	}
	if declared.EntryMatch == nil || declared.List == nil || declared.Sweep == nil {
		t.Fatalf("the row must carry its predicate and both faces: %+v", declared)
	}
	if declared.EntryMatch(strayFixtureKeyHex) {
		t.Errorf("the row's predicate accepted a repo key: %q is 16 hex, not a 2-hex shard", strayFixtureKeyHex)
	}
	if _, err := declared.Resolve(); err != nil {
		t.Errorf("the row's resolver must name the tree's root, got %v", err)
	}
}
