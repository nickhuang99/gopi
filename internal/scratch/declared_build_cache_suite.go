// declared_build_cache_suite.go — the SIXTH build-cache row: the suite's own
// GOCACHE sandbox shells, `<build cache root>/gopi-tools-suite-build-cache-<pid>`.
//
// WHY A ROW EXISTS AT ALL. Measured 2026-10-12 on this host (task
// 01a13032-4c50): the shared build-cache root held 686 directories with this
// name, 1.9 GiB, and NOT ONE of the four census arms claimed any of them —
// `gopi tool scratch-gc trees --dry-run --explain` reported tree=build-cache
// holding_n=3 (the three real per-repo keys) and named no sandbox, and
// `scratch-gc sandboxes` walked a different root entirely. A shape that
// accumulates at one directory per run and that no arm recognises is exactly
// what a declared tree is for.
//
// THE WRITER (read, not guessed — internal/tools/testmain_test.go):
// toolsPinBuildCacheSandbox, armed from this suite's TestMain, offers the merger
// BUILD gate a throwaway GOCACHE for the length of a run. Two of its three
// candidates can be refused by the very placement bans the gate applies, and on
// this host both are (the scratch root is tmpfs; $HOME shares the / device), so
// the arm that engages is the WINDOWED one — the directory the gate resolves by
// default — named `gopi-tools-suite-build-cache-<pid>` (toolsBuildCacheSandboxName
// + "-" + os.Getpid()). The pid suffix is deliberate: concurrent suites must not
// share one sandbox. Its parent is the same root the key row above is rooted at,
// which is why this row is built from the same resolver, one per root.
//
// THE NAME IS THE WRITER'S, NOT A SECOND SPELLING (Rules 13/14): the value lives
// once, in internal/worktreeroots (SharedBuildCacheSuiteSandboxPrefix), and the
// writer's own constant in internal/tools is asserted equal to it
// (TestBuildCacheSandboxNameMatchesTheSweepsPrefix) — the same shape
// TestBuildCacheKeyHexLenMatchesWriter already uses for the key length.
//
// AGE IS THE WHOLE PREDICATE, AND THAT IS SAFE HERE FOR A REASON THE KEY ROW'S
// DOES NOT HAVE: the suffix is this run's pid, so the name is reused by a LATER
// run (pid recycling is routine on a 32-core host — the census found pids from
// 4336 to 4187039, i.e. numbers that wrap). A pid that is alive is therefore NOT
// evidence that the directory is that process's work, and the row must not treat
// it as one. Liveness would not add a single keep: a directory whose only
// content is the empty shell carries no cache value for anyone (measured: 646 of
// the 686 are empty; the few that hold shard directories hold zero regular files,
// because every test that could write a cache SKIPS on this host — see the
// devdoc). The window is what decides, and the predicate is the same age rule
// the key row uses on an EMPTY key (buildCacheKeyStale, files == 0 arm) — one
// reading of "stale", not a second one written here.
//
// WHAT THE WINDOW DOES *NOT* DO, STATED SO NOBODY READS THIS ROW AS COVERING IT:
// the shells from the run happening RIGHT NOW are younger than the window and are
// KEPT — correct, they are in use. Expired ones stay behind for the whole window.
// The in-process reaper (toolsReapBuildCacheSandboxOnSignal) is what removes a
// clean or signalled run's shell immediately; this row bounds what nothing
// reaped. Both halves are in the same change for that reason.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// BuildCacheSuiteSandboxTreeNameSuffix names this row in reports. It is a
	// suffix on the build-cache tree name (`build-cache/suite-sandbox`) so the
	// watchdog line and the dry-run plan attribute the entry to the tree it sits
	// in, beside `build-cache/shards` — the other row rooted at the same place.
	BuildCacheSuiteSandboxTreeNameSuffix = "/suite-sandbox"

	// buildCacheSuiteSandboxMinNameLen is the shortest name the predicate will
	// even look at: the writer's prefix plus at least one digit. It keeps the
	// predicate from accepting the bare prefix (which no writer creates).
	buildCacheSuiteSandboxMinNameLen = 1
)

// BuildCacheSuiteSandboxTrees returns the suite-sandbox ROW for every build cache
// tree on this host — one per resolved worktree root, mirroring BuildCacheTrees
// and BuildCacheStrayShardTrees.
//
// A SEPARATE ROW for the same reason the stray-shard row is one: the two faces
// that matter (`gopi tool scratch-gc trees` and its --dry-run) walk
// DeclaredTrees(), so a shape folded into a row whose predicate cannot match it
// would make the plan report NOTHING for a tree the sweep collects — the operator
// could not see the residue before it went.
func BuildCacheSuiteSandboxTrees() []DeclaredTree {
	roots := buildCacheWorktreeRootsResolver()
	seen := make(map[string]bool, len(roots))
	out := make([]DeclaredTree, 0, len(roots))
	for _, r := range roots {
		path := filepath.Clean(r.Path)
		if path == "" || path == "." || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, suiteSandboxTree(path))
	}
	return out
}

// suiteSandboxTree assembles the row for one worktree root: kind EntryKindDir (a
// candidate is a directory removed whole), the writer's name shape as its
// predicate, and — for staleness and size — the CLOSURES the key row already
// uses, so the two rows cannot drift on what "stale" or "how many bytes" means.
func suiteSandboxTree(worktreeRoot string) DeclaredTree {
	name := BuildCacheTreeName + BuildCacheSuiteSandboxTreeNameSuffix
	root := BuildCacheRootFor(worktreeRoot)
	return DeclaredTree{
		Name:       name,
		Class:      TreeClassBuildCache,
		Kind:       EntryKindDir,
		EntryMatch: buildCacheSuiteSandboxName,
		StaleAge:   BuildCacheStaleAge,
		EntryStale: buildCacheKeyStale,
		EntrySize:  buildCacheKeySize,
		Resolve: func() (string, error) {
			if _, err := os.Stat(root); err != nil {
				return "", fmt.Errorf("%w: declared tree %s has no root at %s",
					ErrDeclaredTreeUnconfigured, name, root)
			}
			return root, nil
		},
	}
}

// buildCacheSuiteSandboxName is this row's NAMESPACE PREDICATE: a direct child of
// the build cache root is ours only when it carries the suite sandbox's name
// shape — the writer's prefix and a run's pid after it.
//
// It is a PREFIX PLUS A DIGIT RUN, never a prefix test alone: `<prefix>` and
// `<prefix>-` are names no writer of this shape creates, and a predicate that
// accepted them would be the blunt "anything that starts with our string" the
// declared-tree contract forbids. The pid itself is NOT validated against a live
// process — see the file header: an empty shell has no keepable content, and a
// recycled pid would make liveness a false witness.
func buildCacheSuiteSandboxName(name string) bool {
	prefix := worktreeroots.SharedBuildCacheSuiteSandboxPrefix
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	digits := name[len(prefix):]
	if len(digits) < buildCacheSuiteSandboxMinNameLen {
		return false
	}
	return IsPID(digits)
}

// BuildCacheSuiteSandboxNameIsOurs is the exported face of the SAME predicate
// (Rule 14), for the one caller that must not drift from it: the WRITER's own
// test, which asserts that the sandbox name it mints is a name this sweep
// recognises. A second, hand-written shape check there is exactly the drift
// Rules 13/14 ban.
func BuildCacheSuiteSandboxNameIsOurs(name string) bool { return buildCacheSuiteSandboxName(name) }

// BuildCacheSuiteSandboxStaleAgeFor is the window this row applies, exported so
// the writer's test and the devdoc can cite the number instead of restating it.
func BuildCacheSuiteSandboxStaleAgeFor() time.Duration { return BuildCacheStaleAge }

// BuildCacheSuiteSandboxDirIn is the directory the WRITER would create for a
// given build cache ROOT and pid — the same path the production gate resolves by
// default. It takes the CACHE root (not the worktree root) so the two components
// are joined exactly once here; a caller that passes a worktree root gets a path
// under a directory the writer never uses, which is why the join is not spread
// across callers. It exists so a test can drive the real predicate against the
// real path without re-spelling the writer's name (Rule 14).
func BuildCacheSuiteSandboxDirIn(cacheRoot string, pid int) string {
	return filepath.Join(cacheRoot, fmt.Sprintf("%s%d", worktreeroots.SharedBuildCacheSuiteSandboxPrefix, pid))
}
