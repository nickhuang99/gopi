// declared_build_cache.go — the SHARED Go BUILD CACHE tree: the fourth
// declared tree, and the one the merge/build gate itself creates and re-creates.
//
// WHY IT IS ITS OWN FILE: this tree is the only declared tree whose WRITER is
// the merge path rather than the scratch/session path, and the only one whose
// staleness is not its own mtime. Keeping its rule here makes both facts
// reviewable in one place, while the registry (declared_trees.go) stays the one
// shared walk.
//
// THE WRITER (read, not guessed — internal/tools/merge_build_gate_gocache.go):
// the merge-time BUILD gate points GOCACHE at
//
//	<worktree root>/<SharedBuildCacheDirName>/<repo-key>/gocache
//
// where <worktree root> is the ONE resolver the rest of the system uses
// (worktreeroots.Resolve: GOPI_WORKTREE_ROOT > the settings repos table > the
// default <home>/workspace) and <repo-key> is the first 16 hex characters of
// SHA-256 over the repository's ABSOLUTE path.
//
// WHY THE KEY IS THE SWEEP UNIT (measured 2026-09-20, this host): the key is a
// digest of the repo's ABSOLUTE path, so it is stable for ONE checkout only.
// Every worktree of the same repository — `git worktree add` gives each task its
// own absolute path, and the suite plants dozens — mints a NEW key, and a key,
// once minted, is never written again: measured 174 key directories, 80,081
// files, 4.95 GB of `du -sb` tree with NOTHING ever deleted, 16.6 GB at the
// epic's own measurement. The gate's in-run bound
// (mergeBuildGateGoCacheMaxBytes) is PER KEY and only fires when that key's
// build exceeds it, so it cannot bound a tree that grows by ACCUMULATION. The
// tree is the accumulation; the key directories are what accumulate.
//
// WHY STALENESS IS NOT THE KEY'S OWN MTIME (measured 2026-09-20, this host):
// the key directory's mtime is set when the gate creates it and does NOT move
// when the Go toolchain then fills `gocache` below it — 172 keys measured, every
// one of them with `gocache` strictly NEWER than the key that holds it
// (1789864127 vs 1789864139 …). A key backdated by one day therefore looks
// abandoned while a build written into it seconds ago is still inside it. Age
// has to be measured where the writes land: EVERY REGULAR FILE below the key,
// and the key is stale only when the NEWEST of them is older than the window
// (buildCacheKeyNewestFile). That is a STRICTER reading than the mtime one — a
// key holding even one fresh file is never a candidate — and it is the reading
// the hard rule requires, because "nothing is using it right now" is not "it is
// not needed."
package scratch

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// BuildCacheTreeName is the declared tree's identity in reports and on the
	// watchdog's log line — the name the epic, the devdoc and the operator's
	// `du` all use for this tree.
	BuildCacheTreeName = "build-cache"

	// BuildCacheStaleAge is the age past which a per-repo-key cache directory
	// (and the whole build cache it holds) is garbage. The ROOT of the tree —
	// `<worktree root>/<SharedBuildCacheDirName>` — is never a candidate.
	//
	// IT IS 24h, AND THE NUMBER IS MEASURED (2026-09-20, this host; the
	// measurement is what chose it):
	//
	//   - THE GATE'S OWN PER-KEY BOUND CANNOT BOUND THIS TREE. Measured over
	//     183 keys / 84,779 files / 5.21 GB: the LARGEST key holds 38 MB and
	//     the median 32 MB, i.e. every key sits at 7.5% of
	//     mergeBuildGateGoCacheMaxBytes (512 MiB) and NOT ONE reaches it. The
	//     toolchain's own trim keeps each key small, so the bound never fires
	//     and the tree grows by ACCUMULATION alone — which is why age is the
	//     only rule that can bound it, and why the window IS the bound.
	//   - THE WINDOW IS SIZED FROM THE RATE. Measured 181 keys in 3.2 hours
	//     (~1.7 GB/h, one key per worktree per task) — the suite mints most of
	//     them, and a merge-only rate is ~45 keys/day (~1.3 GB/day). At the
	//     observed rate a 72h window holds ~120 GB, which is not a bound at
	//     all; 24h holds tens of GB even at that extreme and ~1.3 GB at the
	//     merge-only rate. A window larger than the time a checkout lives is
	//     disk spent on a cache nobody will read again.
	//   - WHAT THE WINDOW PROTECTS IS ONE REBUILD. A key helps exactly one
	//     thing: rebuilding the SAME checkout. A worktree that has not been
	//     built in a day is a task that is done, and a cold rebuild costs one
	//     build — against three disk-exhaustion incidents (2026-09-11,
	//     2026-09-15, 2026-09-16) caused by trees nothing bounded.
	//
	// The value is deliberately NOT made distinct from GoTmpStaleAge: two
	// windows may legitimately be equal, and choosing a different number to
	// look deliberate would be the opposite of a measured decision.
	BuildCacheStaleAge = 24 * time.Hour

	// buildCacheTreeNameRootSep separates an additional tree's name from the
	// root it belongs to (`build-cache@/root`), so one host's several trees each
	// get a name a log reader can attribute.
	buildCacheTreeNameRootSep = "@"

	// BuildCacheGoCacheLeafName is the LEAF every build cache directory carries:
	// the toolchain's standard GOCACHE directory name, and the leaf BOTH of this
	// tree's consumers join — the merge-time BUILD gate, which builds
	// `<root>/<repo-key>/<this>` as the cache it points GOCACHE at
	// (internal/tools: mergeBuildGateCacheDirName = BuildCacheGoCacheLeafName),
	// and the base sweep's legacy bare-key family, which recognises
	// `<base>/<16-hex>/<this>` as the tree a refused gate placement left behind
	// (base_residue.go). One value, two consumers, no second spelling (Rules
	// 13/14).
	BuildCacheGoCacheLeafName = "gocache"

	// BuildCacheKeyHexLen is the length of the per-repo-key DIRECTORY NAME: the
	// node's first 16 hex characters of SHA-256 over the repo's absolute path.
	//
	// It is DEFINED HERE (the sweep side) and referenced by the writer
	// (internal/tools: mergeBuildGateGoCacheRepoKeyHexLen = BuildCacheKeyHexLen),
	// so the two can never drift: a key length the writer changes and the
	// predicate does not would silently stop matching, and the sweep would
	// report an empty tree while the keys piled up. The equality is machine
	// checked (TestBuildCacheKeyHexLenMatchesWriter).
	BuildCacheKeyHexLen = 16
)

// buildCacheKeyName is the tree's NAMESPACE PREDICATE: a direct child of the
// build cache root is OURS only when it carries the writer's key shape — exactly
// BuildCacheKeyHexLen lowercase hex characters. A declared tree's children are
// judged by age + liveness, so without a name rule ANY aged directory in the
// tree would be collectable, including an operator's own directory parked beside
// the caches (the same reason the GOTMPDIR tree requires `go-build*`).
//
// The writer also creates `<key>/gomodcache` (the gate's non-Go-repo fallback
// module cache) — a SIBLING LEAF inside the key, never a top-level entry, so it
// is neither a candidate (the predicate is one level down) nor lost (it goes
// with its key). It is spelled as a sibling of the key, not as a key: the
// predicate is the guarantee, not a name exclusion list.
func buildCacheKeyName(name string) bool {
	if len(name) != BuildCacheKeyHexLen {
		return false
	}
	for _, r := range name {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// BuildCacheKeyNameIsOurs reports whether name is inside the build-cache tree's
// namespace. It is the exported face of the SAME predicate the sweep applies
// (buildCacheKeyName), for the one caller that must not drift from it: the
// writer's own test, which asserts that the key the gate mints is a name the
// sweep will recognise. A second, hand-written shape check there is exactly the
// drift Rules 13/14 ban.
func BuildCacheKeyNameIsOurs(name string) bool { return buildCacheKeyName(name) }

// buildCacheKeyNewestFile returns the number of regular files below keyDir and
// the NEWEST mtime among them — the measurement a build cache's age must be read
// from (see the file header: the key's own mtime is written once, by the gate
// that created it, and never moves again).
//
// Symlinks are NOT followed and are counted as HOLDERS: a symlinked child is not
// a file whose age says anything about this tree, and following it could age —
// or later delete — bytes that live outside the tree (the same rule dirSize
// applies on the sweep side).
//
// A walk error is returned, never swallowed (Rule 15b): an unreadable cache is
// an unmeasured cache, and an unmeasured entry must be KEPT, not guessed at.
func buildCacheKeyNewestFile(keyDir string) (files int, newest time.Time, held bool, err error) {
	err = filepath.WalkDir(keyDir, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			held = true
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		files++
		if mt := info.ModTime(); mt.After(newest) {
			newest = mt
		}
		return nil
	})
	return files, newest, held, err
}

// buildCacheKeyStale is the tree's STALENESS ORACLE (DeclaredTree.EntryStale):
// a key directory is abandoned only when every regular file below it is older
// than the window. A key holding no file at all falls back to its own mtime —
// an empty key is a key the gate created and never filled, which is exactly what
// its own mtime describes. A key holding a symlink is never stale (nothing
// measurable is being asserted about it).
func buildCacheKeyStale(keyDir string, now time.Time, olderThan time.Duration) (bool, error) {
	files, newest, held, err := buildCacheKeyNewestFile(keyDir)
	if err != nil {
		return false, err
	}
	if held {
		return false, nil
	}
	if files == 0 {
		return ScratchEntryStale(keyDir, now, olderThan)
	}
	return now.Sub(newest) > olderThan, nil
}

// buildCacheKeySize measures a key directory's byte BODY (regular files; a
// symlink's own bytes count for nothing, and its target is never followed).
// It is the number the sweep reports for an entry it removes whole.
func buildCacheKeySize(keyDir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(keyDir, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// buildCacheWorktreeRootsResolver yields the worktree roots whose build cache
// trees this host has: the ambient/default root (GOPI_WORKTREE_ROOT when set,
// else the settings repos table — both inside worktreeroots.Resolve, the ONE
// reader, Rule 2) plus every root the settings table declares PER REPO.
//
// WHY A SEAM: <worktree root>/<cache leaf> is the tree, so a host with a
// declared per-repo root has a second cache tree that the ambient resolution
// cannot see, and the writer would fill a tree the GC never walks — the exact
// invisibility this registry exists to remove. This package is the leaf and
// cannot import internal/config (config imports scratch), so internal/config
// installs the enumeration once from its init (RegisterBuildCacheWorktreeRoots,
// called by config/scratch_trees.go). It stays a FUNCTION VALUE, so a caller
// that never imports config (a unit test) still gets the ambient answer instead
// of an empty declared set.
var buildCacheWorktreeRootsResolver = func() []worktreeroots.Root {
	root, err := worktreeroots.Resolve("")
	if err != nil {
		return nil
	}
	return []worktreeroots.Root{root}
}

// RegisterBuildCacheWorktreeRoots installs the worktree-root enumeration. It is
// called by internal/config's init — the owner of the settings table — and
// never by a caller that has no settings to read.
func RegisterBuildCacheWorktreeRoots(f func() []worktreeroots.Root) {
	if f != nil {
		buildCacheWorktreeRootsResolver = f
	}
}

// BuildCacheRootFor returns the build cache tree's ROOT for one worktree root:
// <root>/<SharedBuildCacheDirName>. The leaf name is
// worktreeroots.SharedBuildCacheDirName — the SAME constant the writer joins
// (Rules 13/14: one definition; this package already imports worktreeroots, so
// no alias and no second spelling is needed).
func BuildCacheRootFor(worktreeRoot string) string {
	return filepath.Join(worktreeRoot, worktreeroots.SharedBuildCacheDirName)
}

// BuildCacheTrees returns the declared rows for every build cache tree on this
// host: ONE row per resolved worktree root (see the resolver seam above). It is
// a function of the resolution rather than a fixed row because the tree's root
// IS the resolution's answer — a fixed row could only ever name one of them.
//
// The row for the AMBIENT root carries the plain tree name (BuildCacheTreeName),
// so the log line and the report read as the epic's tree; an ADDITIONAL declared
// root's row carries `build-cache@<root>`, because two trees sharing one name
// would make the log line unattributable.
//
// When no worktree root can be resolved at all (no HOME), the set is NOT empty:
// it is one row whose resolver reports the tree as unconfigured, so the arm still
// prints its line instead of silently omitting a declared tree (Rule 15 — the
// whole defect this registry closes was a tree nobody noticed).
func BuildCacheTrees() []DeclaredTree {
	roots := buildCacheWorktreeRootsResolver()
	seen := make(map[string]bool, len(roots))
	unique := make([]worktreeroots.Root, 0, len(roots))
	for _, r := range roots {
		path := filepath.Clean(r.Path)
		if path == "" || path == "." || seen[path] {
			continue
		}
		seen[path] = true
		unique = append(unique, worktreeroots.Root{Path: path, Role: r.Role, Source: r.Source})
	}
	out := make([]DeclaredTree, 0, len(unique))
	for i, r := range unique {
		// The FIRST root — the ambient one, which the enumeration contract puts
		// first (see declaredBuildCacheWorktreeRoots) — keeps the plain tree
		// name: it is the tree the epic, the report and the operator's `du` all
		// call `build-cache`, and a stable name is what keeps a watchdog log
		// diffable when a second root appears. Additional roots are attributed
		// to themselves, because two trees sharing one name would make the log
		// line unattributable.
		name := BuildCacheTreeName
		if i > 0 {
			name = BuildCacheTreeName + buildCacheTreeNameRootSep + r.Path
		}
		out = append(out, buildCacheTree(name, r.Path))
	}
	if len(out) == 0 {
		out = append(out, DeclaredTree{
			Name:       BuildCacheTreeName,
			Class:      TreeClassBuildCache,
			Kind:       EntryKindDir,
			EntryMatch: buildCacheKeyName,
			StaleAge:   BuildCacheStaleAge,
			EntryStale: buildCacheKeyStale,
			Resolve: func() (string, error) {
				return "", fmt.Errorf("%w: the %s tree has no worktree root to sit under (no home directory)",
					ErrDeclaredTreeUnconfigured, BuildCacheTreeName)
			},
		})
	}
	return out
}

// buildCacheTree assembles one build-cache row for a resolved worktree root.
func buildCacheTree(name, worktreeRoot string) DeclaredTree {
	return DeclaredTree{
		Name:       name,
		Class:      TreeClassBuildCache,
		Kind:       EntryKindDir,
		EntryMatch: buildCacheKeyName,
		StaleAge:   BuildCacheStaleAge,
		EntryStale: buildCacheKeyStale,
		// The ENTRY SIZE is the key's whole subtree (buildCacheKeySize) — a
		// key is removed whole, so what it reclaims is everything below it,
		// and the sweep's dirSize and this agree on symlinks.
		EntrySize: buildCacheKeySize,
		Resolve: func() (string, error) {
			root := BuildCacheRootFor(worktreeRoot)
			if _, err := os.Stat(root); err != nil {
				return "", fmt.Errorf("%w: declared tree %s has no root at %s",
					ErrDeclaredTreeUnconfigured, name, root)
			}
			return root, nil
		},
	}
}
