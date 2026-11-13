package tools

// merge_build_gate_gocache.go — WHERE the merge-time BUILD gate's Go BUILD
// cache lives, and how its growth is BOUNDED. This is the placement half of the
// 2026-09-15 tmpfs-exhaustion family (the sibling fix in
// merge_build_gate_modcache.go was the MODULE half: the same gate's GOMODCACHE,
// shared and read-only since 455daa77).
//
// THE DEFECT: the gate pointed GOCACHE at
// <scratch>/queue/queue-runtime-merge-build-<pid>/gocache — per-PROCESS scratch
// on the RAM-backed tmpfs under the scratch home. A Go build cache is an
// inherently REUSABLE, cross-task asset; a per-PID directory is the one
// placement that guarantees it is never reused. Every gate call therefore wrote
// its cache from cold and then threw it away:
//
//	per-process cache size      726M-1.14G (a full build's worth, every call)
//	dead copies on this host    54 MB x 100+ (measured 2026-09-15)
//	/dev/shm                    at 100% (5.9G of a 7.8G cap)
//	toolchain compile time      68s cold vs ~2s warm (measured, this host)
//
// So the gate paid the COLD cost on every revision AND consumed RAM-backed
// tmpfs for a cache nothing could read again. Both halves are the same mistake:
// scratch is designed to be COLLECTED (internal/scratch's own contract, and
// Rule 22's criterion), so a reusable cache can never live in it.
//
// THE FIX — one shared, disk-backed, BOUNDED location per repo:
//
//	<worktree root>/gopi-build-cache/<repo-key>/gocache
//
// The default worktree root is <home>/workspace (worktreeroots.DefaultRoot) —
// the same declared-per-repo root a worktree uses — which is the right home for
// two reasons: a build cache is REUSABLE ACROSS TASKS but NOT ACROSS REPOS (a
// cache keyed by one repo's package graph is dead weight for another), so it
// belongs under the per-repo root, on disk. The root is resolved by the SAME
// resolver every other consumer uses (config.ResolveRepoWorktreeRoot →
// worktreeroots.Resolve), so an operator who declares a repo's worktree_root
// moves the cache with it and GOPI_WORKTREE_ROOT (the sandbox hook) redirects
// it too — no second root definition (Rule 14).
//
// THE PLACEMENT IS MEASURED, NEVER ASSUMED (Rule 22: a criterion, not a value):
// scratch.AssertGoCacheOffRootFS measures the device of the cache ROOT against
// the device of `/` and refuses when they are the same filesystem. A disk path
// that resolves onto `/` is the 2026-09-11 exhaustion wearing a new name (that
// partition went 19G → 1.5G free), so the gate ABSTAINS loudly instead of
// building into it — never a silent fallback (Rule 15b).
//
// THE COLLECTED-TREE BAN is the THIRD placement rule and the only one that is
// not a filesystem measurement, because the scratch home's filesystem is no
// longer remarkable: since 524156af it is an ordinary directory on the block
// device (the tmpfs route is retired by operator ruling), so a cache placed
// inside it passes both device checks and is still in the one tree whose age GC
// DELETES things. "Scratch is designed to be COLLECTED" is the whole reason this
// file exists — the defect it removes was a cache in the scratch class — and
// with the memory backing gone that sentence was the only thing standing between
// an override and the same defect coming back through the other door. The gate
// therefore refuses a cache detected in the scratch TREE (measured through
// scratch.IsInScratchTree, the same predicate the suite's own arm asserts, so
// production and test cannot disagree about where the tree is) and ABSTAINS.
//
// THE BOUND is the toolchain's own trim, which needs no code from us to be
// correct (measured in GOROOT/src/cmd/go/internal/cache/cache.go: Trim runs when
// the cache is closed at the end of a `go` invocation — at most once per
// trimInterval = 24h — and deletes entries unused for trimLimit = 5 days, the
// toolchain's own "essentially all reuse happens within 5 days" measurement).
// This matters more here than anywhere: the gate's OLD cache was per-process and
// self-emptying — a bound came for free — while a SHARED cache has no such luck,
// so a shared placement without a bound would be a slow disk leak. What WE add is
// the half the toolchain cannot do: REPORT the size at each gate run (a bound
// nobody can see is a bound nobody can check), and make the bound REACHABLE when
// the toolchain's own schedule cannot run — the gate's build can be killed by its
// timeout, so Trim may never fire. When the measured size exceeds
// mergeBuildGateGoCacheMaxBytes the gate drops the cache body and lets the next
// build repopulate it: bounded disk, warm again within one build, never a
// failure. Both arms are reported (Rule 15b).
//
// WHAT IS DELIBERATELY NOT HERE: GOMODCACHE. The module cache is the shared
// READ-ONLY operator cache (merge_build_gate_modcache.go) and its placement is
// unchanged — the gate never writes there. The non-Go-repo fallback is unchanged
// too (mergeBuildGateScratchModCache): a build that cannot read modules still
// gets the gate's own scratch module cache, because it has no module set to
// resolve. Neither is a pass-through of the operator's BUILD cache
// (~/.cache/go-build): a merge probe must not fill the user's cache.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// mergeBuildGateGoCacheMaxBytes bounds the shared build cache's BODY (the
	// two-digit shard directories — cache entries only, never trim.txt).
	//
	// WHAT THE BOUND IS FOR, and the error the FIRST value made (measured
	// 2026-09-24, this host). The promise is "the cache is a CACHE, not an
	// archive" — it is NOT "the cache is as small as possible". The first value
	// was 512 MiB, chosen *because* it sat BELOW the 726M-1.14G the old
	// per-process cache reached, so that it would be REACHABLE. That reasoning
	// is the defect: the bound was placed ON the workload's own steady-state
	// peak, so it fired on healthy operation and every run became a COLD run.
	// Measured on this repo, 2026-09-24 — the cache directories here hold 449 MB
	// (key 1a6fd2a82928067e) and 419 MB (key 034fa09aeba92685) while the go.mod
	// module path is unchanged, and the operator's own log shows the body
	// straddling the old bound in ordinary use (399 MiB → 407 MiB → 652 MiB →
	// "dropping the cache body"), i.e. 511 MiB measured against that 512 MiB
	// bound: an ordinary `go build ./...` AST package reaches the line, and the
	// guard then drops the cache under the build that is populating it.
	//
	// WHAT THE DROP COSTS (direct A/B on this host, `go build ./...` in the
	// repo, then `go vet ./...`, both through the gate's own cache):
	//
	//	warm  10.8s + 12.2s = ~23s
	//	cold  52.9s + 19.0s = ~72s   (cache dir measured 273 MB after one full build/vet)
	//
	// ~49s per merge pass (~14s of it the build alone), spent re-compiling the
	// standard library because a guard fired on NORMAL work. /home is a 766G
	// partition with 292G free, and the tree's real ACCUMULATION bound is AGE,
	// not size (internal/scratch.BuildCacheStaleAge = 24h, which collects whole
	// keys).
	//
	// WHAT THE BOUND IS SIZED AGAINST — the workload's OBSERVED body, never a
	// round number (the same logs this file's history is built from) — and the
	// size the drop must stay clear of:
	//
	//	one full build+vet        273 MB
	//	idle keys on this host    30-53 MiB (four other per-repo keys)
	//	the operating key         426 MiB body / 439 MiB on disk (measured now)
	//	highest lines in the logs 652/679/671 MiB (679 MiB on the 2 GiB bound)
	//
	// 3 GiB is the value: ~3.5x the operating worktree's steady state and ~11x
	// the cache one full build/vet measures (273 MB). The steady-state figure is
	// 880 MiB — the one this change was asked for, CARRIED rather than reproduced
	// here: on THIS tree, this run, the largest measured body is 426 MiB (so ~7x
	// here), and both readings are of the same kind. The separation-from-the-peak
	// property this comment's history argues for holds at either. It is
	// EXPERIMENTAL and REVERTIBLE in a single edit, and the revert criterion is
	// the honest half: **if a body climbs TOWARDS 3 GiB, the cause is CONCURRENCY,
	// not this number.** A Go build cache's body here is a SUM over the writers
	// sharing the key — the suite's parallel packages, several worktrees of the
	// same repo, a merge pass overlapping a task's own build — all of which write
	// their own entries into ONE directory. Raising the bound again would only
	// move the line the sum crosses, so the fix for that shape is bounding the
	// CONCURRENCY (or giving each writer its own key), never a larger number. A
	// bound that fires on healthy work is a pessimisation wearing a guard's
	// name; a bound that fires only because N writers share one directory is a
	// measurement of N.
	mergeBuildGateGoCacheMaxBytes = 3 << 30

	// mergeBuildGateGoCacheRepoKeyHexLen is the length of the per-repo key
	// directory the cache root is built from: 8 bytes of SHA-256 (16 hex chars)
	// — collision risk negligible for the handful of repos a host sweeps, and
	// short enough to read in a path. The VALUE lives in a leaf both sides
	// import (Rules 13/14): the same constant is the scratch sweep's namespace
	// predicate (scratch.BuildCacheKeyHexLen), so a key length the writer
	// changed and the sweep did not would stop matching silently — the sweep
	// would report the tree as visited and collect nothing. The equality is a
	// reference, and a test pins it to the sweep's predicate.
	mergeBuildGateGoCacheRepoKeyHexLen = scratch.BuildCacheKeyHexLen

	// mergeBuildGateGoCacheHexShardLen is the length of the toolchain's cache
	// shard directory names ("00".."ff" — one two-character level below the
	// cache directory). The bound walk counts entry files inside these shards
	// and leaves the trim.txt bookkeeping file (not a shard) alone: deleting it
	// would only make the toolchain re-trim sooner.
	//
	// The VALUE lives in the leaf BOTH sides import (Rules 13/14):
	// scratch.BuildCacheShardHexLen is the same length the scratch sweep's
	// stray-shard row applies (internal/scratch/declared_build_cache_stray.go),
	// so the shape this gate REPORTS at the cache root and the shape that sweep
	// COLLECTS there cannot drift apart — a length one side changed and the
	// other did not would leave the leak reported forever and collected never.
	// The equality is a reference, and a test pins it to the sweep's predicate.
	mergeBuildGateGoCacheHexShardLen = scratch.BuildCacheShardHexLen

	// The ABSTAIN / bound reasons. Each is a constant because the merger log is
	// read by an operator who must be told WHICH path failed and WHY the gate
	// chose to abstain rather than guess (Rule 15b: an uncreatable cache
	// ABSTAINS, it never falls back to a location the operator did not choose).
	mergeBuildGateGoCacheRootUnresolvedFmt = "the repo's worktree root cannot be resolved (%w)"
	mergeBuildGateGoCacheEmptyRootFmt      = "the resolved build cache root is empty"
	mergeBuildGateGoCacheEnvRelFmt         = "the configured build cache root %q is not an absolute path"
	mergeBuildGateGoCacheUncreatableFmt    = "the shared build cache %s cannot be created (%v)"

	// mergeBuildGateGoCacheInScratchReasonFmt is the COLLECTED-TREE refusal. It
	// is the third placement ban beside off-`/` and off-RAM, and the reason is
	// not a matter of taste: internal/scratch's age GC DELETES what lives in the
	// scratch home, so a cache placed there is a reusable asset someone else's
	// cleanup may drop — and the 2026-09-15 incident's per-process cache was
	// exactly scratch-resident. It is a separate check from the two device
	// measurements because the scratch home is neither on `/` (it is /home here)
	// nor on RAM (it is block-backed since 524156af): its filesystem is
	// unremarkable, and only the TREE it belongs to makes it the wrong home.
	//
	// NOTE the ordering hazard this closes: while the scratch home WAS a tmpfs,
	// an override pointing into it tripped AssertGoCacheOffShm by accident. With
	// the memory backing retired the accident cannot fire, so the rule has to be
	// stated in its own right — a placement rule that only ever worked because
	// of an unrelated filesystem choice is a rule nobody had.
	mergeBuildGateGoCacheInScratchReasonFmt = "the shared build cache %s is inside the scratch tree %s: scratch is " +
		"designed to be COLLECTED (the age GC deletes it), so a reusable cache can never live there — unset %s or " +
		"point it at a directory outside the scratch home"

	// mergeBuildGateGoCacheSizeFmt / mergeBuildGateGoCacheTrimLogFmt /
	// mergeBuildGateGoCacheTrimErrFmt are the lines the bound prints: what was
	// measured, what was dropped, and what could not be dropped (Rule 15b — a
	// cache that was emptied silently is a cache whose warmth an operator
	// cannot explain).
	mergeBuildGateGoCacheSizeFmt    = "merger: BUILD gate shared build cache %s holds %s (bound %s)"
	mergeBuildGateGoCacheTrimLogFmt = "merger: BUILD gate shared build cache %s exceeds its %s bound (held %s) — dropping the cache body so the next build repopulates it warm"
	mergeBuildGateGoCacheTrimErrFmt = "merger: BUILD gate shared build cache %s could not drop entry shard %s: %v"
	mergeBuildGateGoCacheMeasureErr = "merger: BUILD gate shared build cache %s could not be measured (%v) — leaving it as it is"

	// mergeBuildGateGoCacheRootLitterFmt is the line that makes a SECOND,
	// UNMEASURED cache at the cache ROOT visible. First seen by an operator on
	// 2026-09-24: `<worktree root>/gopi-build-cache/` itself held the
	// toolchain's marker files AND 256 two-hex shard directories holding
	// 733 MiB / 6902 files — a whole GOCACHE living one level ABOVE where this
	// file builds them.
	//
	// WHY IT IS INVISIBLE WITHOUT THIS LINE, and why the line is the fix rather
	// than a delete: `mergeBuildGateGoCacheBodySize` walks
	// `<root>/<repo-key>/<cache>/<shard>/<file>`. A shard at the ROOT is not
	// under a repo key, so it is never measured, never trimmed, and never USED
	// (every build points GOCACHE at `<root>/<repo-key>/gocache`, a different
	// directory). It is 733 MiB that grows unboundedly and contributes nothing
	// to warmth — pure residue. The gate never writes at that level, so it does
	// not delete there either (a tree the gate did not create is not the
	// gate's to remove, Rule 15b in the conservative direction): it REPORTS,
	// and the tree's existing age sweep is what may collect it.
	//
	// The predicate is SHARD-SHAPED (mergeBuildGateIsHexShard — the same
	// two-lowercase-hex predicate the bound walks with) rather than "not a repo
	// key": only the toolchain's entry shards have that shape, so the line
	// cannot be provoked by the toolchain's own bookkeeping files (README,
	// trim.txt) or by anything else an operator parks in the tree, and it names
	// the exact shape the bound is blind to.
	mergeBuildGateGoCacheRootLitterFmt = "merger: BUILD gate shared build cache ROOT %s holds %d stray cache shard(s) (%s) — the gate writes its caches one level down, under its own per-repo key, so a shard at this level is a SECOND cache the %s bound cannot measure and no build reads; it is REPORTED, never deleted here"

	// mergeBuildGateGoCacheRootLitterListMax bounds how many stray shard names
	// the line names (a reader needs the shape and a couple of examples, not
	// 256 of them).
	mergeBuildGateGoCacheRootLitterListMax = 4

	// mergeBuildGateGoCacheRootUnreadableFmt is the measurement failure of that
	// check — reported, never swallowed (Rule 15b: an unreadable cache root is
	// an unmeasured one).
	mergeBuildGateGoCacheRootUnreadableFmt = "merger: BUILD gate shared build cache ROOT %s could not be listed (%v) — the stray-shard check did not run"
)

// mergeBuildGateGoCache returns the gate's SHARED, disk-backed, bounded Go
// BUILD cache directory for repo, creating it when needed, and reports whether
// it is usable. ok=false means the gate must ABSTAIN: there is no fallback
// location by design, because a cache the operator did not choose is never
// written and a tmpfs one is the defect this file removes.
func mergeBuildGateGoCache(repo string) (cacheDir string, ok bool) {
	root, err := mergeBuildGateGoCacheRoot(repo)
	if err != nil {
		taskNotifyLog("merger: BUILD gate ABSTAINING — %v", err)
		return "", false
	}
	cacheDir = filepath.Join(root, mergeBuildGateGoCacheRepoKey(repo), mergeBuildGateCacheDirName)
	// THE PLACEMENT BANS ARE MEASURED BEFORE ANYTHING IS CREATED — see
	// mergeBuildGateGoCachePlacement for why the ORDER is the fix rather than a
	// stylistic detail. All three are answerable about a path that does not
	// exist yet, so the gate can refuse a placement without minting it.
	if reason := mergeBuildGateGoCachePlacement(root, cacheDir); reason != nil {
		taskNotifyLog("merger: BUILD gate ABSTAINING — %s", reason)
		return "", false
	}
	if err := os.MkdirAll(cacheDir, scratch.DirPerm); err != nil {
		taskNotifyLog("merger: BUILD gate ABSTAINING — %s",
			fmt.Sprintf(mergeBuildGateGoCacheUncreatableFmt, cacheDir, err))
		return "", false
	}
	// BEFORE the bound: report (never delete) a second cache parked at the ROOT
	// — the level this gate never writes to and its bound therefore cannot see.
	// See mergeBuildGateGoCacheRootLitterFmt for the measurement (733 MiB / 256
	// shards on this host, 2026-09-24) and why reporting is the whole fix here.
	mergeBuildGateReportRootLitter(root)
	mergeBuildGateBoundGoCache(cacheDir)
	return cacheDir, true
}

// mergeBuildGateGoCachePlacement applies the THREE placement bans to the cache
// ROOT and the CACHE DIRECTORY inside it, returning the ABSTAIN reason when the
// pair is not a place a shared build cache may live, or nil when it is.
//
// WHY IT IS ONE FUNCTION AND NOT THREE INLINE CHECKS: the bans used to live
// inline in mergeBuildGateGoCache, after its os.MkdirAll. Moving them is this
// file's fix, and a moved check that is re-spelled is a second check — there is
// ONE wording of each ban (Rules 13/14).
//
// THE BANS, in order (see the file header for the incidents they come from):
//
//	1. off `/`  — measured at the ROOT, so a root that only LOOKS like disk (a
//	   symlink onto `/`, a pin at /tmp) is refused.
//	2. off RAM  — measured at the CACHE DIRECTORY, where the bytes would land.
//	3. outside the collected scratch tree — measured at the CACHE DIRECTORY
//	   (the deepest spelling), so a symlinked scratch home is resolved by the
//	   predicate rather than matched by prefix.
//
// ALL THREE ARE ANSWERABLE BEFORE CREATION, and that is not an accident of this
// code: the two device measurements resolve a path that does not exist yet to
// its nearest existing ancestor (scratch.AssertGoCacheOffRootFS /
// AssertGoCacheOffShm — the same rule IsTempRootOffRootFS already applies to a
// not-yet-created sandbox), and the tree ban is a pure string comparison. A
// directory cannot be a mount point before it exists, so the filesystem that
// would hold the cache IS the filesystem of that ancestor — the substitution is
// the identity for a path that already exists, and it is sound for one that does
// not.
//
// WHY THE ORDER IS THE FIX (measured 2026-10-01, this host): with the bans
// AFTER os.MkdirAll, every ABSTAIN still left
// `<root>/<repo-key>/gocache` on disk. The repo KEY is a digest of the repo's
// ABSOLUTE path, so a suite fixture — whose repo is a fresh temp path every run
// — mints a fresh key every run, and the refused root (/dev/shm, the memory
// filesystem the RAM arm exists to measure) accumulated one such directory per
// run: 171 of them, each holding a single empty `gocache`. The arm that ASSERTS
// the ban was the producer of the residue, and the shape of the defect is the
// order of these two statements.
func mergeBuildGateGoCachePlacement(root, cacheDir string) error {
	// Ban 1 — off `/` (the 2026-09-11 exhaustion): an unbounded cache on the
	// boot partition.
	if err := scratch.AssertGoCacheOffRootFS(root); err != nil {
		return err
	}
	// Ban 2 — off RAM (the 2026-09-15 exhaustion): /dev/shm at 100%, 5.9G of a
	// 7.8G cap, from a per-process cache nobody could reclaim.
	if err := scratch.AssertGoCacheOffShm(cacheDir); err != nil {
		return err
	}
	// Ban 3 — outside the COLLECTED scratch tree (see the file header): the
	// scratch home is on disk since 524156af, so bans 1 and 2 both pass for a
	// cache under it, and it is still the tree the age GC collects.
	if scratch.IsInScratchTree(cacheDir) {
		return fmt.Errorf(mergeBuildGateGoCacheInScratchReasonFmt, cacheDir, scratch.HomeDir(), scratch.GoCacheRootEnvName)
	}
	return nil
}

// mergeBuildGateReportRootLitter reports a shard-shaped entry sitting directly
// in the build cache ROOT — the ONE level this gate never writes to (it writes
// `<root>/<repo-key>/gocache`), and therefore the one level its size bound is
// structurally blind to.
//
// It REPORTS and never deletes, deliberately: the gate did not create that tree
// and a tree it did not create is not its to remove (Rule 15b's conservative
// direction — the delete would be the silent, unattributable action, the log
// line is the attributable one). What may collect it is the tree's existing age
// sweep (internal/scratch's declared build-cache tree walks this same root),
// which is why the line points there rather than inventing a second cleaner.
//
// The predicate is SHARD-SHAPED, not "anything that is not a repo key": only
// the toolchain's entry shards ("00".."ff") match, so the toolchain's own
// bookkeeping files (README, trim.txt) and anything an operator parks in the
// tree are silent, and the line names exactly the shape the bound cannot see.
func mergeBuildGateReportRootLitter(root string) {
	shards, err := mergeBuildGateGoCacheRootLitter(root)
	if err != nil {
		// An unreadable root is an UNMEASURED root: said out loud, never
		// swallowed (Rule 15b).
		taskNotifyLog(mergeBuildGateGoCacheRootUnreadableFmt, root, err)
		return
	}
	if len(shards) == 0 {
		return
	}
	named := shards
	if len(named) > mergeBuildGateGoCacheRootLitterListMax {
		named = named[:mergeBuildGateGoCacheRootLitterListMax]
	}
	taskNotifyLog(mergeBuildGateGoCacheRootLitterFmt, root, len(shards), strings.Join(named, " "),
		mergeBuildGateHumanBytes(mergeBuildGateGoCacheMaxBytes))
}

// mergeBuildGateGoCacheRootLitter lists the shard-shaped directories sitting
// directly in root — the whole detection half of the check above, split out so
// the arm that pins it drives the REAL predicate (its sibling arm cannot: the
// reporter's only observable is a log line). It reports the read error rather
// than returning an empty list for an unreadable root: "nothing is there" and
// "I could not look" are different answers (Rule 15b).
func mergeBuildGateGoCacheRootLitter(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && mergeBuildGateIsHexShard(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// mergeBuildGateGoCacheRoot resolves the directory the per-repo caches live
// under (the repo key and the cache leaf are appended by
// mergeBuildGateGoCache): the ONE override when set (Rule 2 — one reader), else
// <worktree root>/<SharedBuildCacheDirName>.
//
// The error return carries the ABSTAIN reason, never a fallback path.
func mergeBuildGateGoCacheRoot(repo string) (string, error) {
	if raw := strings.TrimSpace(os.Getenv(scratch.GoCacheRootEnvName)); raw != "" {
		if !filepath.IsAbs(raw) {
			return "", fmt.Errorf(mergeBuildGateGoCacheEnvRelFmt, raw)
		}
		return filepath.Clean(raw), nil
	}
	root, err := mergeBuildGateRepoWorktreeRoot(repo)
	if err != nil {
		return "", fmt.Errorf(mergeBuildGateGoCacheRootUnresolvedFmt, err)
	}
	return filepath.Join(root, worktreeroots.SharedBuildCacheDirName), nil
}

// mergeBuildGateRepoWorktreeRoot resolves the worktree root the cache lives
// under, through the ONE resolver the rest of the system uses (Rules 12/14): the
// settings.json repos table, with GOPI_WORKTREE_ROOT winning when a sandbox pins
// it.
func mergeBuildGateRepoWorktreeRoot(repo string) (string, error) {
	root, err := config.ResolveRepoWorktreeRoot(repo)
	if err != nil {
		// The env override IS the sandbox hook and needs no settings file, so a
		// broken settings file must not defeat an explicit pin — and must not
		// silently substitute the default either: worktreeroots.Resolve reports
		// whether its answer was the DEFAULT (unconfigured), and a default here
		// is exactly the silent fallback Rule 15b forbids.
		if envRoot, rerr := worktreeroots.Resolve(""); rerr == nil && envRoot.Source != worktreeroots.SourceDefault {
			return envRoot.Path, nil
		}
		return "", err
	}
	if root.Path == "" {
		return "", errors.New(mergeBuildGateGoCacheEmptyRootFmt)
	}
	return root.Path, nil
}

// mergeBuildGateGoCacheRepoKey is the per-repo component of the cache path: a
// short, filesystem-safe digest of the repo's absolute path.
//
// WHY A DIGEST AND NOT THE REPO'S BASENAME: two repos on a host routinely share
// a leaf ("GoPi" checked out twice, /srv/a/tool and /srv/b/tool), and a shared
// leaf would make two repos silently share one cache — the cross-project cache
// poisoning the module-cache work had to reason about. A digest of the ABSOLUTE
// path is stable by construction: same repo, same key, across processes.
func mergeBuildGateGoCacheRepoKey(repo string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(repo)))
	return hex.EncodeToString(sum[:])[:mergeBuildGateGoCacheRepoKeyHexLen]
}

// mergeBuildGateBoundGoCache enforces the cache bound: it measures the cache
// BODY and, when it exceeds mergeBuildGateGoCacheMaxBytes, drops the body so the
// next build repopulates it (warm again within one build). Both the measurement
// and any trim are REPORTED — a bound nobody can see is a bound nobody can
// check, and a silently emptied cache would make an operator's next build look
// like a warmth regression (Rule 15b). A trim that cannot delete a shard is
// reported per shard and never fatal: the cache is still usable, just still
// large.
func mergeBuildGateBoundGoCache(cacheDir string) {
	size, err := mergeBuildGateGoCacheBodySize(cacheDir)
	if err != nil {
		taskNotifyLog(mergeBuildGateGoCacheMeasureErr, cacheDir, err)
		return
	}
	taskNotifyLog(mergeBuildGateGoCacheSizeFmt, cacheDir,
		mergeBuildGateHumanBytes(size), mergeBuildGateHumanBytes(mergeBuildGateGoCacheMaxBytes))
	if !mergeBuildGateGoCacheOverBound(size) {
		return
	}
	taskNotifyLog(mergeBuildGateGoCacheTrimLogFmt, cacheDir,
		mergeBuildGateHumanBytes(mergeBuildGateGoCacheMaxBytes), mergeBuildGateHumanBytes(size))
	mergeBuildGateTrimGoCache(cacheDir)
}

// mergeBuildGateGoCacheOverBound reports whether a measured cache body exceeds
// the bound. It is a separate predicate so the bound can be exercised at any
// size (Rule 5: a check that can only be reached by filling a real cache is
// a check nobody runs) — the comparison and its boundary are pinned by a test.
func mergeBuildGateGoCacheOverBound(size int64) bool {
	return size > mergeBuildGateGoCacheMaxBytes
}

// mergeBuildGateTrimGoCache drops the cache's ENTRY SHARDS (its body) and leaves
// the toolchain's bookkeeping files (trim.txt, the log) in place, so the next
// build repopulates a cache the toolchain still recognises. A shard that cannot
// be dropped is reported (Rule 15b) and never fatal: the cache is still usable,
// just still large.
func mergeBuildGateTrimGoCache(cacheDir string) {
	for _, shard := range mergeBuildGateGoCacheShards(cacheDir) {
		if rerr := os.RemoveAll(shard); rerr != nil {
			taskNotifyLog(mergeBuildGateGoCacheTrimErrFmt, cacheDir, shard, rerr)
		}
	}
}

// mergeBuildGateGoCacheBodySize sums the size of the cache's ENTRY files — the
// files inside the toolchain's two-character shard directories
// ("<shard>/<hash>-a|d"). The toolchain's own bookkeeping (trim.txt, the log
// file) is not a shard and is deliberately excluded: it is not the cache body,
// and counting it would let the bound trip on an already-empty cache.
//
// A walk error is returned, never skipped: a bound computed from an unreadable
// tree is a bound that silently stopped working (Rule 15b).
func mergeBuildGateGoCacheBodySize(cacheDir string) (int64, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, shard := range entries {
		if !mergeBuildGateIsHexShard(shard.Name()) || !shard.IsDir() {
			continue
		}
		files, derr := os.ReadDir(filepath.Join(cacheDir, shard.Name()))
		if derr != nil {
			return total, derr
		}
		for _, f := range files {
			info, ierr := f.Info()
			if ierr != nil {
				return total, ierr
			}
			total += info.Size()
		}
	}
	return total, nil
}

// mergeBuildGateGoCacheShards lists the cache's entry shard directories, so the
// trim drops the BODY and leaves trim.txt and the log alone — the toolchain then
// re-trims and re-reports normally instead of starting from a directory it does
// not recognise. A read failure yields no shards: the caller's measurement has
// already reported the same failure (both read the same directory), so nothing
// is swallowed here that was not named there.
func mergeBuildGateGoCacheShards(cacheDir string) []string {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if mergeBuildGateIsHexShard(e.Name()) && e.IsDir() {
			out = append(out, filepath.Join(cacheDir, e.Name()))
		}
	}
	return out
}

// mergeBuildGateIsHexShard reports whether name is one of the toolchain's entry
// shards ("00".."ff"): two lowercase hex characters. Named so the size walk and
// the trim agree on what an entry shard is (Rules 13/14 — one predicate, two
// consumers).
func mergeBuildGateIsHexShard(name string) bool {
	if len(name) != mergeBuildGateGoCacheHexShardLen {
		return false
	}
	for _, r := range name {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// mergeBuildGateHumanBytes renders a byte count for the merger log (MiB, or GiB
// once it is worth reading in those units) — the log is the only place an
// operator learns the cache's size, so it must be readable at a glance.
func mergeBuildGateHumanBytes(n int64) string {
	const (
		unitMiB = 1 << 20
		unitGiB = 1 << 30
	)
	switch {
	case n >= unitGiB:
		return fmt.Sprintf("%.2f GiB", float64(n)/unitGiB)
	case n >= unitMiB:
		return fmt.Sprintf("%d MiB", n/unitMiB)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
