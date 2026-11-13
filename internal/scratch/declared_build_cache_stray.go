// declared_build_cache_stray.go — the SECOND row the shared build cache tree
// needs: the ROOT-LEVEL STRAY SHARDS.
//
// WHY A SEPARATE ROW (and not a predicate change on the key row): the tree has
// two disjoint entry shapes at its root, and they are not interchangeable —
//
//   - a REPO KEY is a 16-lowercase-hex directory the gate creates once per
//     repository checkout, and it is stale when the NEWEST FILE BELOW it is
//     older than the window (it never moves its own mtime, see
//     declared_build_cache.go);
//   - a STRAY SHARD is a 2-lowercase-hex directory — the Go toolchain's own
//     entry-shard shape — that a GOCACHE parked AT the root produced. Measured
//     on this host 2026-09-24: 256 such directories, 6902 files, 733 MiB,
//     sitting one level ABOVE `<root>/<repo-key>/gocache`, read by no build and
//     measured by no bound.
//
// The KEY row cannot see it (its predicate is 16 hex characters, so a shard is
// never a candidate and the shard's bytes are never reclaimed), and the key row
// must not be widened to reach it: "any two-hex child of the cache root" is a
// different question from "a per-repo cache nobody will read again", answered by
// a different staleness rule (a shard's own mtime IS its content's mtime — the
// toolchain writes entries directly inside it, unlike a key whose mtime freezes
// at creation), and the two shapes are disjoint by construction: a 16-hex key is
// never two characters, a two-character shard is never 16.
//
// WHY THE GATE REPORTS IT AND THIS SWEEP COLLECTS IT: the gate
// (internal/tools/merge_build_gate_gocache.go) writes only
// `<root>/<repo-key>/gocache`, so a cache at the root is not its to delete — it
// REPORTS the leak (Rule 15b) and names this sweep as the collector. Collection
// belongs where every other declared tree's collection lives: a row in
// DeclaredTrees, walked by the ONE sweep, with the root never a candidate.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// BuildCacheShardHexLen is the length of a Go build cache SHARD directory
	// name — the toolchain's own two-character level ("00".."ff") below a
	// GOCACHE root. It is DEFINED HERE (the leaf both sides import) and
	// referenced by the writer (internal/tools:
	// mergeBuildGateGoCacheHexShardLen = BuildCacheShardHexLen), so a shard
	// shape the gate reports and the sweep collects can never drift apart: a
	// length one side changed and the other did not would leave the leak
	// reported forever and collected never. The equality is machine checked
	// (TestBuildCacheShardHexLenMatchesWriter).
	BuildCacheShardHexLen = 2

	// BuildCacheStrayShardTreeNameSuffix names the tree's SECOND row. It is a
	// suffix rather than a name of its own so the row stays attributable to the
	// tree it belongs to in a log line (`build-cache/shards`).
	BuildCacheStrayShardTreeNameSuffix = "/shards"
)

// buildCacheShardName is the stray-shard row's NAMESPACE PREDICATE: a direct
// child of the cache root is a stray shard only when it carries the toolchain's
// shard shape — exactly BuildCacheShardHexLen lowercase hex characters.
//
// It is deliberately SHARD-SHAPED rather than "anything that is not a repo
// key": only the toolchain's entry shards have that shape, so the toolchain's
// own bookkeeping files (README, trim.txt, log.txt) and anything an operator
// parks in the tree are never candidates, and the row names exactly the shape
// the gate's size bound is blind to.
func buildCacheShardName(name string) bool {
	if len(name) != BuildCacheShardHexLen {
		return false
	}
	for _, r := range name {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// BuildCacheShardNameIsOurs reports whether name is inside the stray-shard row's
// namespace. It is the exported face of the SAME predicate the sweep applies
// (buildCacheShardName), for the one caller that must not drift from it: the
// writer's own test, which asserts that the shape the gate REPORTS is a shape
// this sweep will recognise — a second, hand-written shape check there is
// exactly the drift Rules 13/14 ban.
func BuildCacheShardNameIsOurs(name string) bool { return buildCacheShardName(name) }

// buildCacheRootStrayShards lists the root-level stray shard directories of one
// build cache root.
//
// It is the sweep-side counterpart of the gate's own report (internal/tools'
// mergeBuildGateGoCacheRootLitter) and reads the SAME shape through this
// package's exported predicate; the two readers are on purpose separate (the
// gate must report without importing a sweep), and their agreement is pinned by
// the writer's test driving both against one fixture.
//
// A DIRECTORY only: a file named "ab" is not a toolchain shard (shards are the
// directories holding entry files). An unreadable root reports its error rather
// than an empty list — "nothing is there" and "I could not look" are different
// answers (Rule 15b).
func buildCacheRootStrayShards(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && buildCacheShardName(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// strayShardCandidates is the ONE listing behind this row's BOTH faces: the
// stray shards of root that are AGED under olderThan, each measured (size by the
// sweep's own dirSize, age by its own mtime). The plan face returns it; the
// collect face removes exactly it. Two walks here would be two answers.
//
// The root is never a candidate (the walk starts one level down) and a declared
// root is refused again per path — the same invariant, restated at the listing
// so a caller that deletes from this list cannot take a root by accident.
//
// A root that does not exist yields an EMPTY listing and no error: a host whose
// gate never ran has no such tree, and the key row already reports the tree as
// unconfigured once (ErrDeclaredTreeUnconfigured). A root that exists and cannot
// be READ is an error, never an empty list (Rule 15b).
func strayShardCandidates(root string, now time.Time, olderThan time.Duration) ([]TreeEntry, error) {
	names, err := buildCacheRootStrayShards(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]TreeEntry, 0, len(names))
	for _, name := range names {
		path := filepath.Join(root, name)
		if IsDeclaredTreeRoot(path) {
			continue
		}
		stale, serr := ScratchEntryStale(path, now, olderThan)
		if serr != nil || !stale {
			continue // unmeasurable or fresh: KEEP, never guess (Rule 15b)
		}
		e := TreeEntry{Path: path, IsDir: true}
		if size, derr := dirSize(path); derr == nil {
			e.Size = size // best effort: the reclaim is reported, never enforced
		}
		if fi, ferr := os.Lstat(path); ferr == nil {
			e.Age = now.Sub(fi.ModTime())
		}
		out = append(out, e)
	}
	return out, nil
}

// sweepBuildCacheStrayShards is the row's COLLECT face: it removes exactly the
// candidates strayShardCandidates listed, re-checking the root invariant and the
// liveness verdict at each delete through removeCollectableOfTree (the .holder
// registration + the live-holder check + the root refusal) — the same delete path
// every other declared tree uses, so "never remove a tree a live process holds"
// stays one rule with one implementation.
func sweepBuildCacheStrayShards(rowName, root string, age time.Duration) (int, int64, error) {
	now := time.Now()
	entries, err := strayShardCandidates(root, now, age)
	if err != nil {
		return 0, 0, err
	}
	tree := DeclaredTree{Name: rowName, Class: TreeClassBuildCache}
	removed := 0
	var freed int64
	for _, e := range entries {
		if rerr := removeCollectableOfTree(tree, e.Path); rerr != nil {
			fmt.Fprintf(os.Stderr, "scratch: GC could not remove stray build-cache shard %s (%s): %v\n",
				e.Path, tree.Name, rerr)
			continue
		}
		removed++
		freed += e.Size
	}
	return removed, freed, nil
}

// BuildCacheStrayShardTrees returns the stray-shard ROW for every build cache
// tree on this host — one per resolved worktree root, mirroring BuildCacheTrees.
//
// It is a SEPARATE ROW rather than a second entry shape on the key row so the
// sweep's two faces stay in step: `gopi tool scratch-gc trees` and its `--dry-run`
// walk DeclaredTrees(), so folding this shape into the key row (whose predicate
// can never match it) would leave the plan reporting NOTHING for a tree the
// sweep collects — an operator could not see the leak before it went.
func BuildCacheStrayShardTrees() []DeclaredTree {
	roots := buildCacheWorktreeRootsResolver()
	seen := make(map[string]bool, len(roots))
	out := make([]DeclaredTree, 0, len(roots))
	for _, r := range roots {
		path := filepath.Clean(r.Path)
		if path == "" || path == "." || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, strayShardTree(path))
	}
	return out
}

// strayShardTree assembles the stray-shard row for one worktree root: kind
// EntryKindDir (a candidate is a directory removed whole), the toolchain's
// shard shape as its predicate, and the two closures that make its plan and its
// sweep the SAME listing.
func strayShardTree(worktreeRoot string) DeclaredTree {
	name := BuildCacheTreeName + BuildCacheStrayShardTreeNameSuffix
	root := BuildCacheRootFor(worktreeRoot)
	return DeclaredTree{
		Name:       name,
		Class:      TreeClassBuildCache,
		Kind:       EntryKindDir,
		StaleAge:   BuildCacheStaleAge,
		EntryMatch: buildCacheShardName,
		List: func(age time.Duration) ([]TreeEntry, error) {
			return strayShardCandidates(root, time.Now(), age)
		},
		Sweep: func(age time.Duration) (int, int64, error) {
			return sweepBuildCacheStrayShards(name, root, age)
		},
		Resolve: func() (string, error) {
			return root, nil
		},
	}
}
