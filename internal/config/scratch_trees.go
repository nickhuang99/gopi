package config

import (
	"path/filepath"
	"sort"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// scratch_trees.go — the ONE place the declared-tree resolvers of
// internal/scratch are attached to this package's gopi-data-root resolution.
//
// WHY A SEAM AT ALL (and why this direction): internal/scratch is the leaf
// package that declares WHICH trees this repository's commands create and never
// clean (declared_trees.go), while the gopi data root (<gopi dir> — $GOPI_HOME
// aware) is resolved HERE. scratch cannot import config: config imports scratch
// (config.go, env.go, settings.go, flags.go, tmpdir.go all reference its
// constants), so the reverse import is a cycle. The leaf therefore declares the
// resolvers as function values and this package — the owner of GopiDir — fills
// them in once, at init.
//
// The alternative (re-spelling "<gopi dir>/raw" and "<gopi dir>/sessions" inside
// scratch) would put a second definition of both paths in the tree, which is
// exactly the drift Rules 13/14 ban: the raw directory name is spelled once (the
// scratch constant), the sessions directory name once (SessionsDirName here).
//
// The wiring is CHECKED, not trusted: internal/scratch's
// TestTreeResolversAreWired fails when a declared tree resolves to the
// unwired sentinel, so a build that broke this file cannot silently produce a
// sweep of "" (which would be a sweep of the process's own working directory).
func init() {
	scratch.RegisterDataRootResolvers(
		func() (string, error) {
			root, err := GopiDir()
			if err != nil {
				return "", err
			}
			return filepath.Join(root, scratch.RawLogDirName()), nil
		},
		func() (string, error) {
			root, err := GopiDir()
			if err != nil {
				return "", err
			}
			return filepath.Join(root, SessionsDirName), nil
		},
	)
	// The fourth declared tree (the shared Go build cache) roots at the WORKTREE
	// root, which this package owns the resolution of — <worktree root> is the
	// settings repos table, with worktreeroots' env override on top. The sweep
	// needs the ENUMERATION (a host may declare a root per repo, and each one
	// holds its own cache tree), and it needs it without importing this package
	// (cfg.imports scratch), so it takes it as a function value exactly like the
	// data-root resolvers above.
	scratch.RegisterBuildCacheWorktreeRoots(declaredBuildCacheWorktreeRoots)
	// --- MERGE NOTE (rework run 01a0f645ab907201a9a0d124216a1215) ----------
	// THIS IS A UNION: both sides of the conflict are kept, master's block
	// FIRST (it was already the second half of this init), then the branch's.
	// NOTHING from master was dropped here; the conflict arose only because
	// both sides appended a registration at the same closing seam.
	//   master's block  : the PROFILE ROOT resolver + the SESSIONS TREE child
	//                     (the `scratch-gc profile` arm, landed on master).
	//   branch's block  : the PROFILE's SIBLING TREES registration, into the
	//                     two registries that each own one face of that census.
	// The two are INDEPENDENT — separate registries, no shared symbol, no
	// ordering dependency (each registration is a plain function-value store
	// read later, never at init) — so keeping both preserves both behaviours.
	// -----------------------------------------------------------------------
	// The PROFILE ROOT (this package's own GopiDir) is the tree the arm in
	// scratch/profile_root.go observes and — for its one narrow residue family —
	// may collect. It is wired from HERE for the same reason the two resolvers
	// above are: <gopi dir> is resolved by this package, and scratch cannot
	// import it back (cycle). A profile root the arm resolves itself would be a
	// second spelling of the join, which is the drift Rules 13/14 ban.
	scratch.RegisterProfileRootResolver(GopiDir)
	// THE SESSIONS TREE is declared from HERE, because its NAME is defined here
	// (SessionsDirName) and this package imports scratch — spelling the name in
	// the registry would be a second spelling of it, which is what the
	// hardcoded-literal gate in this very package exists to refuse.
	if err := scratch.RegisterProfileRootChildren(scratch.ProfileRootChild{
		Name:  SessionsDirName,
		Kind:  scratch.ProfileChildExact,
		Class: scratch.ProfileChildState,
		Bound: "the session tree (the ONE official home of a run's evidence): the JSONL history is not bounded here, its `*.raw.log` files are (SessionRawLogStaleAge); never a candidate",
	}); err != nil {
		// Rule 19's exemption is the one init/main/Must* has, and this IS init:
		// a registration refused here would leave the sessions tree undeclared
		// for the whole process, and nothing later could tell that state apart
		// from a tree nobody owns (Rule 15 — the failure must be LOUD, not a
		// silent unregistered row).
		panic("config: the sessions profile-root child was refused: " + err.Error())
	}
	// The PROFILE's sibling trees (the four directories beside the sessions
	// tree: archive/, backups/, notify/, dumps/) hang off the profile root this
	// package owns. TWO registries declare them — internal/sessionlayout (the
	// family census face) and internal/scratch (the declared-tree row that
	// bounds `dumps/`) — and neither may import the other, so both take the
	// profile root as a function value from exactly this place. It is the SAME
	// resolver for both: one profile root, one resolution (Rules 2/14).
	scratch.RegisterSiblingDataRootResolver(GopiDir)
	sessionlayout.RegisterSiblingDataRootResolver(GopiDir)
	// The SCRATCH home's two dump class trees (sessionlayout/scratch_dumps.go):
	// the writers' documented DEFAULT, and on a host that exports neither dump
	// knob the only dump tree there is — so the census must reach it by name.
	// The map is built by the package that owns both the names and the roots
	// (internal/scratch.ScratchDumpClassRoots), which is what keeps a name and
	// its root paired; the trees hang off the SCRATCH home, not off the profile,
	// which is why they are wired here rather than derived from GopiDir.
	sessionlayout.RegisterScratchDumpsRootResolvers(scratch.ScratchDumpClassRoots())
}

// declaredBuildCacheWorktreeRoots enumerates every worktree root this host's
// settings declare — the ambient/default one first, then one per repo whose
// entry declares its own root. The build cache tree hangs off EACH of them
// (internal/scratch: BuildCacheRootFor), so a repo with a declared root has a
// cache tree the ambient resolution alone would never walk.
//
// A repo whose entry declares no root contributes the ambient root again; the
// declared set dedupes by resolved path, so it is not listed twice.
//
// Order is sorted by repo path so the same host produces the same declared set
// on every run — a set whose ORDER moved would make an operator's log diff
// unreadable and the gate's comparisons flaky.
func declaredBuildCacheWorktreeRoots() []worktreeroots.Root {
	out := make([]worktreeroots.Root, 0, 2)
	if root, err := worktreeroots.Resolve(""); err == nil {
		out = append(out, root)
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		// An unreadable settings file costs the PER-REPO roots, never the
		// ambient one already collected: the cache tree that exists is still
		// declared, and a settings that cannot be read is reported by the
		// callers that need it (the merger's own resolution).
		return out
	}
	repos := make([]string, 0, len(cfg.Repos))
	for repo := range cfg.Repos {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	for _, repo := range repos {
		root, rerr := ResolveRepoWorktreeRoot(repo)
		if rerr != nil {
			continue
		}
		out = append(out, root)
	}
	return out
}
