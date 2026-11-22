// toolchain_cache.go — the AGENT-side Go toolchain cache (~/.cache/go-build):
// how big it is, whether the filesystem holding it has headroom, and how it is
// brought back under a bound WITHOUT deleting the operator's cache.
//
// WHY THIS FILE EXISTS (measured on nick-HP-Laptop, 2026-10-12):
//
//	/dev/nvme0n1p7  (66309, /home)   458G, 416G used, 19G free  — 96% FULL
//	/dev/nvme0n1p5  (66307, /)       183G,  43G used, 131G free — 25%
//	~/.cache/go-build                121G, 368605 files, device 66309
//
// The full partition is /home, NOT `/` — and that is the whole reason this file
// is not already covered by the existing machinery:
//
//   - scratch.AssertGoCacheOffRootFS / AssertGoCacheOffShm (rootgocache.go)
//     answer "is this cache on the SAME filesystem as `/`?" and "is it on RAM?".
//     Both are the RIGHT question for the shared MERGE-gate cache (whose defect
//     was a cache on the boot partition, then a cache in tmpfs) and both answer
//     NO here: /home is neither `/` nor a memory filesystem. Rule 22's criterion
//     is a criterion about `/`, and the partition that actually filled is a
//     different one.
//   - merge_build_gate_gocache.go bounds the MERGE gate's own per-repo cache
//     (3 GiB, per repo key). It never touches ~/.cache/go-build, by an explicit
//     decision ("a merge probe must not fill the user's cache").
//
// So the agent-side toolchain cache had NO bound and NO headroom criterion: the
// one number that would have made the exhaustion visible (free bytes on the
// filesystem that HOLDS it) was measured by nothing.
//
// WHAT THIS FILE ADDS, and what it deliberately does NOT:
//
//   - A BOUND with OLDEST-ENTRY-SHARD EVICTION. The toolchain's own Trim (at
//     most once per 24h, deleting entries unused for 5 days) never runs when a
//     build is killed by its timeout — the same mechanism the merge gate's bound
//     exists for — so a bound the operator can SEE and a reclaim the operator can
//     RUN is the missing half. Eviction removes whole two-hex entry SHARDS,
//     oldest first, and never touches the toolchain's own bookkeeping (README,
//     trim.txt, log.txt, CACHEDIR.TAG): the directory stays a cache the toolchain
//     recognises.
//   - A HEADROOM CRITERION measured with statfs on the filesystem that would
//     hold the cache (device id, filesystem type, free bytes) — so the answer is
//     about /home when /home is what is full, and the refusal NAMES the path, the
//     device and the measured free bytes (Rule 15b: loud, never a silent
//     fallback).
//
// IT NEVER DELETES ANYTHING BY ITSELF, and this is a design decision rather than
// a missing feature (Rule 41): the toolchain cache is the operator's shared,
// cross-project asset, and the finder of a defect proposes the reclaim — the
// operator decides when it happens. This file therefore ships MEASUREMENT +
// VERDICT + an explicitly-invoked EVICTION; the CLI face
// (`gopi tool scratch-gc gocache`) reports by default and evicts only on an
// explicit flag.
package scratch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// ToolchainCacheMaxEnvName is the ONE knob that sets the bound (GiB), and it
	// is read in exactly ONE place (ResolveToolchainCacheMaxBytes — Rule 2). It
	// is a gopi knob about a directory the Go TOOLCHAIN owns, not GOCACHE: GOCACHE
	// is read by `go` itself and pinning it here would move the operator's cache,
	// which is the opposite of this file's job.
	ToolchainCacheMaxEnvName = "GOPI_BUILD_CACHE_MAX_GIB"

	// DefaultToolchainCacheMaxBytes is the bound applied when the knob is unset.
	//
	// WHY 25 GiB, and not a number chosen for tidiness: the bound's job is to be
	// REACHABLE only when something has gone wrong, because a bound that fires on
	// healthy work makes every build cold (the merge gate's own first bound, 512
	// MiB, was measured to fire on ordinary builds and was raised (2 GiB, then
	// 3 GiB) for
	// exactly this reason — see merge_build_gate_gocache.go). The MEASURED subject
	// here is the operator's ~/.cache/go-build at 121 GiB — ~60x the merge gate's
	// per-repo bound and demonstrably unbounded (it grows with every package
	// graph the host ever compiled, across every project). 25 GiB is ~1/5 of that
	// measurement and ~12x the per-repo bound: far above the working set of a warm
	// build on one repo, far below "the disk is full", and therefore a bound that
	// is INFORMATIVE when it fires.
	DefaultToolchainCacheMaxBytes int64 = 25 << 30

	// DefaultToolchainCacheHeadroomBytes is the free-space FLOOR of the headroom
	// criterion: how much room the filesystem that holds the cache must still
	// have. Below it the criterion refuses, naming the path and the measured
	// bytes.
	//
	// MEASURED basis: the hosting partition was found at 19 GiB free of 458 GiB
	// (96% used) with ~121 GiB of that held by this one cache. 5 GiB is the
	// smallest room a `go build ./...` of this repository plus its toolchain
	// writes was measured to need with slack (one full build/vet measures ~273 MB
	// of new cache body, and a `go` invocation may also materialise modules and
	// work directories) — a floor, not a quota: a filesystem with more room than
	// this is untouched by the criterion.
	DefaultToolchainCacheHeadroomBytes uint64 = 5 << 30

	// ToolchainCacheEvictMinAge is the grace window eviction respects: a shard
	// whose NEWEST entry is younger than this is never a candidate, because a
	// running build (or one that was killed an hour ago) may still be using it.
	//
	// The toolchain's own trim uses trimLimit = 5 days of NON-USE — a much longer
	// window than this, and the reason a manual reclaim is needed at all: an
	// operator at 96% cannot wait five days. 24h is the shortest window that keeps
	// "is a build using this right now?" answerable from the filesystem itself
	// (entry mtime), rather than from a liveness scan of processes this package
	// cannot enumerate for a foreign toolchain.
	ToolchainCacheEvictMinAge = 24 * time.Hour

	// toolchainCacheGocacheLeaf is the leaf the toolchain's own default cache
	// carries under the user cache directory ("~/.cache/go-build"). It is the
	// TOOLCHAIN's spelling (written by `go`, not chosen here) and it is named so
	// the CLI's default resolution does not spell it inline (Rule 1b).
	toolchainCacheGocacheLeaf = "go-build"

	// toolchainCacheBookkeepingShapes names the files the toolchain writes
	// BESIDE its entry shards. They are never candidates for anything this file
	// does: eviction removes shard DIRECTORIES only, and the shape table exists so
	// the report can say how many of these it saw preserved (Rule 15: an operator
	// must be able to see that the cache is still recognised by its own writer).
	toolchainCacheBookkeepingShapes = "README trim.txt log.txt CACHEDIR.TAG"

	// ToolchainCacheEntryGrainName is the WORD this machinery answers "at what
	// grain can bytes be reclaimed here?" with. It is a NAMED CONSTANT because it
	// reaches the operator through three faces that must never disagree (Rule
	// 1b/14): the bound's refusal sentence, the gocache arm's `grain=` field, and
	// the tests that assert both.
	ToolchainCacheEntryGrainName = "cache-entry"

	// ToolchainCacheShardGrainName is the word for the grain this file used
	// BEFORE it learned to work at the toolchain's own entry grain: a whole two-hex
	// shard directory. It is kept as a name (not deleted) because the arm's report
	// and its tests must be able to say WHICH grain a reading is about — the
	// defect this file fixes was a report that left the grain unnamed, so the
	// reclaimable=0 it printed read as "your cache is fine".
	ToolchainCacheShardGrainName = "shard"

	// ToolchainCacheEntryIndexSuffix / ToolchainCacheEntryDataSuffix are the two
	// file names the Go toolchain gives ONE cache entry (cmd/go/internal/cache's
	// entryName): the INDEX file (`v1 <action id> <output id> ...`) and the DATA
	// file (the output the action produced). They are the toolchain's own spelling
	// — written by `go`, not chosen here — and the pair is what
	// ToolchainCacheEntryName recognises (Rule 1b: never retyped inline). Exported
	// because the tests of BOTH packages build fixtures in the toolchain's shape and
	// must not respell it (Rules 13/14).
	//
	// ToolchainCacheEntryIndexSuffix: the index half.
	ToolchainCacheEntryIndexSuffix = "-a"

	// ToolchainCacheEntryDataSuffix: the data half — the one that CARRIES THE
	// BYTES. The index file is one `recordSize`-shaped record (~270 bytes), and an
	// entry's output size may legitimately be 0. Both halves are removed together,
	// because an index whose data file is gone is a cache MISS the toolchain pays
	// for on every reuse, and a data file whose index is gone is unreachable bytes —
	// a leak, not a hole.
	ToolchainCacheEntryDataSuffix = "-d"

	// ToolchainCacheEntryIDHexLen is the number of hex characters in a cache
	// entry's id. MEASURED on the real cache rather than assumed (2026-10-12, the
	// operator's 121 GiB ~/.cache/go-build: every entry file in a shard is a
	// 64-character lowercase-hex id plus its "-a"/"-d" suffix), and it is the
	// toolchain's own HashSize (32 bytes = SHA-256) rendered as hex — cmd/go/
	// internal/cache/hash.go's `HashSize = 32` printed with %x.
	//
	// It is a LENGTH and not a "starts with" test on purpose: a shard may hold a
	// foreign file whose name ends in "-d", and grouping it under a bogus entry id
	// would put a stranger's bytes into an entry this sweep then deletes.
	ToolchainCacheEntryIDHexLen = 64

	// bytesToMiBShift once rendered a byte count in MiB for the report line. The
	// report is now in FormatBytes (the unit an operator compares with `du`), so the
	// shift has no reader and is DELETED rather than left as a value that looks like
	// it still decides something (Rule 13: a constant exists to be referenced).

	// ToolchainCacheGiBBytes is the GiB unit the bound knob is expressed in. It is
	// exported because TWO callers convert with it and neither may spell the number
	// itself (Rules 13/14): this package's reader of the environment knob, and the
	// CLI flag that outranks it (cmd/agent's `--max-gib` / `--headroom-gib`).
	ToolchainCacheGiBBytes int64 = 1 << 30
)

// ToolchainCacheShard is one two-hex entry shard as measured: its whole size and
// the newest mtime found inside it (the eviction ordering key — a shard is "old"
// when its NEWEST entry is old, which is the only reading under which no live
// build's freshly written entry can be removed).
type ToolchainCacheShard struct {
	Path       string
	Name       string
	Bytes      int64
	NewestMod  time.Time
	EntryCount int
}

// ToolchainCacheEntry is ONE cache entry as the TOOLCHAIN defines it: the
// `<hex>-a` index file and the `<hex>-d` data file that sit together inside a
// shard directory (cmd/go/internal/cache's entryName / trimSubdir). It is the
// grain at which the toolchain itself reclaims, and therefore the grain at which
// a live cache — every shard of which holds at least one fresh entry — can be
// brought back under a bound at all.
//
// SIZE is the WHOLE pair's size, not the data file's: both files are removed
// together, so counting one of them would report a reclaim the eviction does not
// actually make (the over-estimate this file's own history is a record of).
type ToolchainCacheEntry struct {
	Shard string
	// Name is the entry's identity inside its shard: the `<hex>-d` data file name
	// when the entry has one (the toolchain's own entryName spelling), else the
	// `<hex>-a` index name for an entry that has no data file yet.
	Name string
	// Path is what an eviction removes (os.RemoveAll): the DATA file, or the
	// INDEX file for an entry that has no data file.
	Path string
	// Paths is every file of the pair that exists, removed in this order.
	Paths []string
	// DataBytes is the part of Bytes the toolchain actually pays for the output
	// (0 for an entry with no data file, or one whose output size is legitimately
	// zero). Reported so a reader can tell a cache full of DATA from one full of
	// tiny index records.
	DataBytes int64
	Bytes     int64
	Mod       time.Time
}

// ToolchainCacheReport is the MEASUREMENT of a toolchain cache, in the two halves
// the epic asks for and no more:
//
//   - WHAT it holds (body bytes across the entry shards, the shard list, the
//     whole-tree byte count, how many files) — the number a bound is compared
//     against;
//   - WHICH FILESYSTEM HOLDS IT (device id, filesystem type by statfs, total and
//     free bytes) — the headroom criterion's input, measured on the partition
//     that actually carries the cache rather than on `/`.
//
// The filesystem half is measured on the nearest EXISTING ancestor, so the
// question is answerable before the cache exists (the same ordering rule
// AssertGoCacheOffRootFS documents) and so a cache path that does not exist yet
// is still described by the filesystem that would hold it.
type ToolchainCacheReport struct {
	Path  string
	Leaf  string
	Total int64 // bytes in the entry shards (the "body" the bound compares)

	// Whole-tree measurement (informational: what the directory costs on disk).
	AllBytes int64
	Files    int
	Dirs     int

	Shards []ToolchainCacheShard

	// The toolchain's own bookkeeping files present at the root, as measured.
	Bookkeeping []string

	// The filesystem that would hold the cache.
	//
	// Device is the STAT(2) device number of the filesystem holding the cache —
	// the number `stat -c %d`, `df` and a `/proc` device list print, so an
	// operator can CHECK the report against their own instrument. It is
	// deliberately NOT the statfs Fsid that DeviceID returns: Fsid is the right
	// identity for comparing two paths against each other (which is all
	// AssertGoCacheOffRootFS needs), but it matches nothing a reader can look up,
	// and a report whose one checkable-looking field cannot be checked is worse
	// than one that omits it (Rule 15b). Both are measured; this one is PRINTED.
	Device       uint64
	FSType       string
	FSTotal      uint64
	FSFree       uint64
	FSMeasuredAt string // the nearest existing ancestor the filesystem was measured on

	// OldestShardBytes is the bytes held in shards whose NEWEST entry is older
	// than ToolchainCacheEvictMinAge — i.e. what an eviction COULD reclaim right
	// now without touching anything a recent build wrote. It is the measured
	// answer to "how much comes back tonight".
	//
	// IT IS NOT THE RECLAIMABLE FIGURE ANY MORE, and keeping it is a deliberate
	// record rather than an oversight: it is the SHARD-grain reading, and on a live
	// cache it is 0 at every bound (every shard holds a fresh entry), which is the
	// defect this file now answers at the ENTRY grain. The number an operator acts
	// on is ReclaimableBytes; this one stays printable so the two grains can be
	// compared on the same cache — a reader who sees shard_grain=0 MiB beside
	// entry_grain=N MiB has the whole diagnosis in one line.
	OldestShardBytes int64

	// Entries is every cache ENTRY of the cache, at the toolchain's own grain (the
	// `<hex>-a` / `<hex>-d` pair inside a shard) — the grain at which this
	// machinery measures and reclaims.
	Entries []ToolchainCacheEntry

	// ReclaimableBytes is what an eviction could actually free HERE AND NOW: the
	// bytes of entries older than ToolchainCacheEvictMinAge, at the ENTRY grain.
	// It is the number the bound's refusal quotes and the arm prints as
	// `reclaimable=`, and it is the one that is nonzero on a live cache whose
	// shards all hold something fresh.
	ReclaimableBytes int64

	// ReclaimableEntries is ReclaimableBytes' entry count — the other half of
	// "how much comes back tonight", and the figure that makes a stale-cache
	// reading checkable by hand (`ls` the shard and count).
	ReclaimableEntries int

	// Grain is the NAME of the grain this report's ReclaimableBytes is expressed
	// at, so a zero can never be read as "your cache is fine" (Rule 15): it is
	// ToolchainCacheEntryGrainName, and the arm prints it as `grain=`.
	Grain string
}

// ToolchainCacheVerdict is the headroom/bound CRITERION, as a reportable
// decision: whether the cache is over its bound, whether the filesystem holding
// it is below the headroom floor, and the sentence that says so — naming the
// path, the device, the measured free space and the bound (Rule 15b: a refusal a
// reader can act on, never a silent one).
type ToolchainCacheVerdict struct {
	OverBound    bool
	LowHeadroom  bool
	BoundBytes   int64
	HeadroomWant uint64

	// Reason is "" when the verdict is a clean OK, and otherwise the sentence an
	// operator reads. Exactly one sentence per verdict KIND (a single string, so
	// two refusal faces cannot describe one state differently).
	Reason string
}

// Ok reports whether the criterion found nothing to say — the state in which the
// CLI's report line carries no reason at all.
func (v ToolchainCacheVerdict) Ok() bool { return !v.OverBound && !v.LowHeadroom }

// GrainName is the grain a report's reclaimable figure is expressed at, named
// even for a report built by hand (a zero-value ToolchainCacheReport carries the
// empty string, and an empty grain name in a printed line is exactly the
// ambiguity this constant exists to remove). It is a METHOD so no caller has to
// decide what an absent grain means (Rule 14: one answer, one place).
func (rep ToolchainCacheReport) GrainName() string {
	if rep.Grain != "" {
		return rep.Grain
	}
	return ToolchainCacheEntryGrainName
}

// ResolveToolchainCacheMaxBytes is the ONE reader of the bound knob (Rule 2):
// THIS INVOCATION's explicit value when one was given (flagBytes > 0), else the
// environment value in GiB when it is usable, else
// DefaultToolchainCacheMaxBytes.
//
// The two channels are ranked HERE and nowhere else, so the flag, the
// environment and the default can never disagree about which one wins — the
// shape PublishScratchGCStaleMin uses for the sweep's window.
//
// A malformed or non-positive value is REPORTED and the default applies — never a
// silent zero (a zero bound would mean "evict everything", the opposite of a
// configured bound) and never an error (a mistyped threshold must not disable the
// mechanism, Rule 15).
func ResolveToolchainCacheMaxBytes(flagBytes int64) int64 {
	if flagBytes > 0 {
		return flagBytes
	}
	raw := strings.TrimSpace(os.Getenv(ToolchainCacheMaxEnvName))
	if raw == "" {
		return DefaultToolchainCacheMaxBytes
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "build-cache: %s=%q is not a positive number of GiB — using the default %s\n",
			ToolchainCacheMaxEnvName, raw, FormatBytes(uint64(DefaultToolchainCacheMaxBytes)))
		return DefaultToolchainCacheMaxBytes
	}
	return n * ToolchainCacheGiBBytes
}

// toolchainCacheGoEnvName is the Go toolchain's OWN cache-directory variable,
// named ONCE for this file's three readings of it (Rules 1/13). It is the
// TOOLCHAIN's spelling, not gopi's: pinning it differently here would make the
// resolver disagree with `go env GOCACHE`, which is the whole subject.
//
// It is UNEXPORTED on purpose. This package defines a constant for the value
// only because it is READ here more than once; a consumer that needs the name in
// its own child environment (cmd/agent's help-surface probe, internal/tools'
// merge gate) owns its own spelling, the way #9669 recorded — the value is the
// toolchain's, and a second exported definition would be a second source of
// truth for a name gopi does not own.
const toolchainCacheGoEnvName = "GOCACHE"

// ResolveToolchainCachePath is the ONE resolver for the cache DIRECTORY a report
// is about. Its precedence is the TOOLCHAIN's OWN (`go help environment` /
// `cmd/go/internal/cfg`), and it is stated here in full because getting it wrong
// is silent:
//
//  1. the explicit path (the CLI's --path)                       wins outright
//  2. $GOCACHE                                                    the process env
//  3. GOCACHE in the go env file (<home>/.config/go/env)          the file `go env -w` writes
//  4. <user cache dir>/go-build                                  the toolchain's built-in default
//
// WHY CHANNEL 3 EXISTS, and the measured defect it repairs (the host,
// 2026-11-01): the Go toolchain persists `go env -w KEY=VALUE` in the go env
// FILE, not in any shell profile, so on a host that pinned its cache with
// `go env -w GOCACHE=/home/user/workspace/.gocache` the variable is EMPTY in the
// process environment and the toolchain nevertheless uses that directory (548G).
// The pre-fix resolver read channels 1, 2 and 4 only and reported
// /home/user/.cache/go-build (273M) — a cache `go` IS NOT USING. Reported by a
// READ-ONLY diagnostic that is a confident wrong answer, and fatal behind
// `--trim`, which would have evicted from a directory no build ever fills. So the
// file channel is not a convenience: a report about a cache the toolchain is not
// using is exactly the defect the whole configuration discipline exists to
// prevent (Rule 2 — one resolver, the toolchain's precedence, not a second one).
//
// The file's PATH and its key=value parsing are NOT spelled again here: they come
// from goEnvFilePath/goToolEnvFileValue, the package's ONE reader (Rules 13/14).
// A file that cannot be READ is an error rather than a fall-through, because
// "could not measure" must never render as "not declared" (Rule 15); a MISSING
// file is the ordinary case of an operator who never ran `go env -w`.
func ResolveToolchainCachePath(explicit string) (string, error) {
	if p := strings.TrimSpace(explicit); p != "" {
		return CanonicalPath(p), nil
	}
	if p := strings.TrimSpace(os.Getenv(toolchainCacheGoEnvName)); p != "" {
		return CanonicalPath(p), nil
	}
	fromGoEnv, err := goToolEnvFileValue(toolchainCacheGoEnvName)
	if err != nil {
		return "", fmt.Errorf("the toolchain cache directory cannot be resolved: %w", err)
	}
	if p := strings.TrimSpace(fromGoEnv); p != "" {
		return CanonicalPath(p), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("the toolchain cache directory cannot be resolved: %s is set neither in the environment nor in %s, and the user cache dir does not answer (%w)",
			toolchainCacheGoEnvName, goEnvFilePath(), err)
	}
	return CanonicalPath(filepath.Join(base, toolchainCacheGocacheLeaf)), nil
}

// ToolchainCacheGrainNote renders the ONE sentence a report needs when its two
// grains disagree — i.e. when the coarse (shard) reading counts bytes the
// entry-grain reclaim does not free, because those entries are themselves fresh
// inside a shard whose NEWEST entry is old.
//
// It is the "never a silent no-op" half of this file's contract at the REPORT
// face: without it, an operator comparing `reclaimable=` (entry grain, correct)
// against the shard-grain figure sees two numbers for one question and no way to
// tell which one the eviction will honour. It returns "" when the two agree —
// the ordinary case, and one that must not be narrated.
func ToolchainCacheGrainNote(rep ToolchainCacheReport) string {
	over := rep.OldestShardBytes - rep.ReclaimableBytes
	if over <= 0 {
		return ""
	}
	return fmt.Sprintf(toolchainCacheGrainNoteFmt, rep.Path,
		ToolchainCacheShardGrainName, FormatBytes(uint64(over)),
		ToolchainCacheEntryGrainName, FormatBytes(uint64(rep.ReclaimableBytes)), ToolchainCacheEntryGrainName)
}

// MeasureToolchainCache measures dir and returns the report above. It NEVER
// writes: no directory is created and nothing is removed (the read-only face of

// this file).
//
// A cache that does not exist is NOT an error: it is reported as an empty cache
// whose filesystem is still measured (the question "where WOULD it live" is
// answerable — that is what makes the criterion usable before the cache exists).
// A directory that exists and cannot be walked IS an error: "nothing is there" and
// "I could not look" must never read alike (Rule 15).
func MeasureToolchainCache(dir string) (ToolchainCacheReport, error) {
	rep := ToolchainCacheReport{Path: dir, Leaf: filepath.Base(dir)}
	probe := existingAncestor(dir)
	rep.FSMeasuredAt = probe
	dev, err := PathDeviceNumber(probe)
	if err != nil {
		return rep, fmt.Errorf("the filesystem holding %s cannot be measured — refusing to guess: %w", dir, err)
	}
	rep.Device = dev
	if name, ferr := FilesystemTypeName(probe); ferr == nil {
		rep.FSType = name
	} else {
		// A filesystem whose type cannot be read is REPORTED as unknown, never as
		// a plausible-looking default (Rule 15).
		rep.FSType = "unknown (" + ferr.Error() + ")"
	}
	if total, free, ferr := FilesystemCapacity(probe); ferr == nil {
		rep.FSTotal, rep.FSFree = total, free
	} else {
		return rep, ferr
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return rep, nil // absent: the expected non-event, filesystem still reported
		}
		return rep, fmt.Errorf("the toolchain cache %s cannot be listed: %w", dir, err)
	}
	cutoff := time.Now().Add(-ToolchainCacheEvictMinAge)
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		if e.IsDir() && buildCacheShardName(name) {
			shard, serr := measureToolchainCacheShard(path, name)
			if serr != nil {
				return rep, serr
			}
			entriesInShard, eerr := measureToolchainCacheEntries(path, name)
			if eerr != nil {
				return rep, eerr
			}
			rep.Shards = append(rep.Shards, shard)
			rep.Total += shard.Bytes
			if shard.NewestMod.Before(cutoff) {
				rep.OldestShardBytes += shard.Bytes
			}
			// The ENTRY grain and the SHARD grain are measured on the SAME walk of
			// the same directory, so the two figures a report prints can never be
			// about two different moments (the trap a second, later walk would set).
			rep.Entries = append(rep.Entries, entriesInShard...)
			for i := range entriesInShard {
				if entriesInShard[i].Mod.Before(cutoff) {
					rep.ReclaimableBytes += entriesInShard[i].Bytes
					rep.ReclaimableEntries++
				}
			}
			continue
		}
		if !e.IsDir() && strings.Contains(toolchainCacheBookkeepingShapes, name) {
			rep.Bookkeeping = append(rep.Bookkeeping, name)
		}
	}
	rep.Grain = ToolchainCacheEntryGrainName
	// The whole-tree totals are a SECOND walk on purpose: they must include
	// everything the first walk deliberately skipped (the toolchain's bookkeeping,
	// any nested directory the toolchain grows inside a shard), and a report that
	// called those bytes "the cache" would understate what the directory costs.
	all, files, dirs, werr := measureToolchainCacheTree(dir)
	if werr != nil {
		return rep, werr
	}
	rep.AllBytes, rep.Files, rep.Dirs = all, files, dirs
	sort.Slice(rep.Shards, func(i, j int) bool { return rep.Shards[i].Name < rep.Shards[j].Name })
	return rep, nil
}

// measureToolchainCacheShard sums ONE shard's bytes and finds the newest mtime
// inside it — the eviction ordering key. A shard that cannot be read is an error,
// never a zero (Rule 15).
func measureToolchainCacheShard(path, name string) (ToolchainCacheShard, error) {
	shard := ToolchainCacheShard{Path: path, Name: name}
	entries, err := os.ReadDir(path)
	if err != nil {
		return shard, fmt.Errorf("cache shard %s cannot be listed: %w", path, err)
	}
	for _, e := range entries {
		info, ierr := e.Info()
		if ierr != nil {
			return shard, fmt.Errorf("cache shard %s entry %s cannot be measured: %w", path, e.Name(), ierr)
		}
		shard.Bytes += info.Size()
		shard.EntryCount++
		if info.ModTime().After(shard.NewestMod) {
			shard.NewestMod = info.ModTime()
		}
	}
	return shard, nil
}

// ToolchainCacheEntryName splits a shard entry name into the identity of the
// cache ENTRY it belongs to, reporting whether it is one at all.
//
// The shape is the toolchain's own: a 32-hex (SHA-256) id followed by the
// "-a" (index) or "-d" (data) suffix — exactly what cmd/go/internal/cache's
// fmt.Sprintf("%x%s", …) writes and what its trimSubdir's suffix test reclaims.
// Everything else a shard may hold (a nested directory, a stray file, an editor
// backup) is NOT an entry and is therefore never a candidate — the same
// conservative direction the rest of this package takes (an unknown is kept).
//
// It answers with the DATA id when it sees either half of the pair, so the two
// files of one entry group under one identity.
func ToolchainCacheEntryName(name string) (id string, ok bool) {
	for _, suffix := range []string{ToolchainCacheEntryIndexSuffix, ToolchainCacheEntryDataSuffix} {
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		id = strings.TrimSuffix(name, suffix)
		if !isHexID(id, ToolchainCacheEntryIDHexLen) {
			return "", false
		}
		return id, true
	}
	return "", false
}

// isHexID reports whether s is exactly n lowercase hex characters — the shape
// predicate shared by the toolchain's entry ids, spelled once so the entry and
// the shard readings cannot drift (Rule 14).
func isHexID(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// measureToolchainCacheEntries lists ONE shard at the toolchain's ENTRY grain:
// every cache entry it holds, with the whole pair's bytes and the newer mtime of
// the two files.
//
// WHY THE NEWER MTIME: the toolchain stamps an entry's files when it USES them
// (cache.markUsed, at most once an hour), and a pair can be written through two
// separate paths, so the pair's "last use" is the LATEST of its two halves.
// Taking the index file's mtime alone would call an entry cold while a build was
// reading its data file — the direction that deletes something in use.
//
// A subdirectory inside a shard is skipped, never descended: the toolchain writes
// an executable cache entry (`<hex>-d` as a DIRECTORY) in some configurations,
// and this reclaim's promise is about FILES it can measure to the byte. The
// skip is REPORTED through the returned pair, so a caller can see that a shard's
// bytes exceed the sum of its entries' (Rule 15: an unmeasured thing must not
// read as an empty one).
//
// A shard that cannot be read is an error, never an empty result.
func measureToolchainCacheEntries(shardPath, shardName string) ([]ToolchainCacheEntry, error) {
	dirEntries, err := os.ReadDir(shardPath)
	if err != nil {
		return nil, fmt.Errorf("cache shard %s cannot be listed: %w", shardPath, err)
	}
	byID := map[string]*ToolchainCacheEntry{}
	order := make([]string, 0, len(dirEntries))
	for _, de := range dirEntries {
		if de.IsDir() {
			continue // an executable-style entry directory: kept, never measured as a file
		}
		id, ok := ToolchainCacheEntryName(de.Name())
		if !ok {
			continue // not a cache entry (the toolchain's own bookkeeping, a stray file)
		}
		info, ierr := de.Info()
		if ierr != nil {
			return nil, fmt.Errorf("cache entry %s/%s cannot be measured: %w", shardPath, de.Name(), ierr)
		}
		entry := byID[id]
		if entry == nil {
			entry = &ToolchainCacheEntry{Shard: shardName, Name: de.Name(), Path: filepath.Join(shardPath, de.Name())}
			byID[id] = entry
			order = append(order, id)
		}
		entry.Paths = append(entry.Paths, filepath.Join(shardPath, de.Name()))
		entry.Bytes += info.Size()
		if info.ModTime().After(entry.Mod) {
			entry.Mod = info.ModTime()
		}
		if strings.HasSuffix(de.Name(), ToolchainCacheEntryDataSuffix) {
			// The data half is the entry's identity AND the bytes the toolchain
			// pays for the output; both facts belong to the pair, not to the file.
			entry.Name = de.Name()
			entry.Path = filepath.Join(shardPath, de.Name())
			entry.DataBytes += info.Size()
		}
	}
	out := make([]ToolchainCacheEntry, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

func measureToolchainCacheTree(root string) (int64, int, int, error) {
	var total int64
	var files, dirs int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root {
				dirs++
			}
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		total += info.Size()
		files++
		return nil
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("the toolchain cache %s cannot be walked: %w", root, err)
	}
	return total, files, dirs, nil
}

// CheckToolchainCache applies the CRITERION to a report: the bound compares the
// measured body, the headroom compares the measured FREE space of the filesystem
// that holds the cache. bound <= 0 and headroom == 0 each disable their own arm
// (the explicit "measure, do not judge" form a caller may want), and the reason
// names the measured facts in both cases.
func CheckToolchainCache(rep ToolchainCacheReport, bound int64, headroom uint64) ToolchainCacheVerdict {
	v := ToolchainCacheVerdict{BoundBytes: bound, HeadroomWant: headroom}
	if bound > 0 && rep.Total > bound {
		v.OverBound = true
		v.Reason = fmt.Sprintf(toolchainCacheOverBoundFmt, rep.Path,
			FormatBytes(uint64(rep.Total)), FormatBytes(uint64(bound)),
			FormatBytes(uint64(rep.ReclaimableBytes)), rep.GrainName(),
			rep.ReclaimableEntries, ToolchainCacheEvictMinAge)
	}
	if headroom > 0 && rep.FSFree < headroom {
		v.LowHeadroom = true
		reason := fmt.Sprintf(toolchainCacheLowHeadroomFmt, rep.Path, rep.FSType, rep.Device,
			FormatBytes(rep.FSFree), FormatBytes(rep.FSTotal), FormatBytes(headroom))
		if v.Reason == "" {
			v.Reason = reason
		} else {
			v.Reason = v.Reason + " " + reason
		}
	}
	return v
}

// The two refusal sentences, each naming the measured facts an operator needs:
// WHICH path, WHICH filesystem (type + device), HOW MUCH free space it really has,
// and WHAT to run. They are constants because the CLI line, the devdoc and the
// tests quote the same words (Rule 1b/13).
//
// The FIRST TWO placeholders (%s %s) are the grain's NAME and the bytes — the
// grain is quoted in the sentence itself, because a reclaimable figure without its
// grain is the reading that produced this file's defect: `0 MiB … can be reclaimed`
// at the shard grain reads as "your cache is fine" on a live cache holding 90 GiB
// of cold entries.
const (
	toolchainCacheOverBoundFmt   = "the toolchain build cache %s holds %s, over its %s bound (%s of that is reclaimable now at the %s grain, and %d of its entries are past the %s grace window)"
	toolchainCacheLowHeadroomFmt = "the filesystem holding the toolchain build cache (%s, %s, device %d) has %s free of %s — below the %s headroom floor; the cache is a shared asset and is NOT touched here: reclaim it with `gopi tool scratch-gc gocache --trim` once you have read the report"
	toolchainCacheGrainNoteFmt   = "the toolchain build cache %s: the %s grain counts %s that a reclaim at the %s grain does NOT free (those entries are fresh inside a shard whose newest entry is old); the figure the eviction honours is %s at the %s grain"
)

// ToolchainCacheEviction is what one eviction call did, at the ENTRY grain — the
// before/after numbers the epic requires be reported verbatim.
//
// THE GRAIN OF EVERY FIGURE HERE (Rule 1b/13: a figure without its grain is the
// defect this work exists for): Before/After/FreedBytes are summed over the
// cache's ENTRIES, exactly as ToolchainCacheReport.Total is — so `after` equals
// the next measurement's `body=` and the pair can be checked against each other.
// ShardCandidates/ShardKept are the SHARD-grain readings, kept because they are
// what the previous grain could say and their being 0 beside a nonzero Freed
// is the measurement that proves the fix.
type ToolchainCacheEviction struct {
	// Before / After are the cache BODY measured at the entry grain, before and
	// after this call; FreedBytes is what the removals actually took away.
	Before     int64
	After      int64
	FreedBytes int64

	// BeforeEntries / RemovedEntries are the pair's other half at the entry grain.
	BeforeEntries  int
	RemovedEntries int

	// Removed names the entries removed, one per entry, as `<shard>/<entry>`.
	Removed []string

	// ShardCandidates is how many shards the OLD shard grain would have found —
	// the number that is 0 on a live cache, which is why this fix exists. ShardKept
	// counts the shards the grace window keeps whole. Both are readings, never
	// decisions: nothing here removes a directory.
	ShardCandidates int
	ShardKept       int

	// Skipped names what the grace window kept (the measured reason a bound may
	// not be reached tonight). A SHARD that had candidates is not named — the
	// reason would drown the reading, since a live cache keeps every shard — so the
	// two conditions are kept apart: `Skipped` is entries, `ShardKept` is shards.
	Skipped []string
	// Errs carries per-entry failures that did not stop the rest (Rule 15b: one
	// unremovable entry is reported, never fatal).
	Errs []string
}

// EvictToolchainCache brings the cache BODY down to target bytes by removing whole
// cache ENTRIES — the toolchain's own unit of reclamation, the `<hex>-a` /
// `<hex>-d` pair inside a shard — OLDEST FIRST (by the entry's newest mtime), and
// returns the measured before/after.
//
// It is the ONLY deleting function in this file and it is never called by anything
// that ticks: the caller is the operator's explicit verb.
//
// FOUR INVARIANTS, each of which is a way this could destroy something the
// operator needs:
//
//  1. ONLY cache-ENTRY FILES are removed — the `<hex>-a` / `<hex>-d` pair, the
//     toolchain's own entryName shape. The toolchain's bookkeeping (README,
//     trim.txt, log.txt, CACHEDIR.TAG) lives at the cache ROOT and is unreachable
//     by construction; anything inside a shard that is not an entry (a nested
//     directory — the executable-entry shape — a stray file) is never a candidate.
//     Removing the bookkeeping would leave a directory the toolchain no longer
//     recognises as its cache.
//  2. THE GRACE WINDOW IS RESPECTED at the ENTRY grain: an entry whose newest file
//     is younger than minAge is never a candidate, so a build that ran minutes ago
//     keeps its entries. This is what makes the entry grain SAFE where a
//     whole-shard removal would not be: the shard holding a fresh entry survives
//     while the cold entries beside it come back.
//  3. THE ROOT — AND EVERY SHARD DIRECTORY — IS NEVER REMOVED, at any age or size.
//     Emptying the directory would make every following build pay the cold cost and
//     would rewrite a directory the operator may have configured; removing shard
//     directories would also destroy the `trim.txt`/shard scaffolding the toolchain
//     writes into. A shard this pass empties is left as an empty directory.
//  4. THE DECISION IS RE-READ AT THE DELETE. Entries are re-measured immediately
//     before removal and an entry that is GONE (already removed by the toolchain's
//     own concurrent trim) or that has become FRESH (a build just wrote its pair)
//     is skipped: a measurement and a delete are two moments, and only the second
//     one may authorise removing a file.
//
// It stops as soon as the measured body is at or below target, and it reports what
// it could not reach (the grace window, a per-entry failure) rather than looping
// (Rule 15b).
func EvictToolchainCache(dir string, target int64, minAge time.Duration, now time.Time) (ToolchainCacheEviction, error) {
	var out ToolchainCacheEviction
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out, fmt.Errorf("the toolchain cache %s cannot be listed: %w", dir, err)
	}
	cutoff := now.Add(-minAge)
	var candidates []ToolchainCacheEntry
	for _, e := range entries {
		if !e.IsDir() || !buildCacheShardName(e.Name()) {
			continue
		}
		shardPath := filepath.Join(dir, e.Name())
		shard, serr := measureToolchainCacheShard(shardPath, e.Name())
		if serr != nil {
			return out, serr
		}
		out.Before += shard.Bytes
		// The SHARD-grain reading this call would have made before the fix: a
		// shard is a candidate only when even its newest entry is old.
		if shard.NewestMod.Before(cutoff) {
			out.ShardCandidates++
		} else {
			out.ShardKept++
		}
		shardEntries, eerr := measureToolchainCacheEntries(shardPath, e.Name())
		if eerr != nil {
			return out, eerr
		}
		out.BeforeEntries += len(shardEntries)
		for _, entry := range shardEntries {
			if entry.Mod.Before(cutoff) {
				candidates = append(candidates, entry)
			}
		}
	}
	out.After = out.Before
	// OLDEST FIRST, by the ENTRY's newest file — the ordering is what makes the
	// reclaim bite in the case the epic names: the cold entries of a live cache
	// sort first, and the fresh ones that share their shard are never candidates at
	// all.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Mod.Equal(candidates[j].Mod) {
			return entrySortKey(candidates[i]) < entrySortKey(candidates[j])
		}
		return candidates[i].Mod.Before(candidates[j].Mod)
	})
	for _, entry := range candidates {
		if out.After <= target {
			break
		}
		skipped, live := verifyCacheEntryAtDelete(dir, entry, cutoff)
		if skipped != "" {
			out.Skipped = append(out.Skipped, skipped)
			continue
		}
		entry = live
		freed := int64(0)
		removedAny := false
		for _, path := range entry.Paths {
			info, serr := os.Stat(path)
			if serr != nil {
				if os.IsNotExist(serr) {
					continue // a concurrent trim won this race: nothing to remove
				}
				out.Errs = append(out.Errs, fmt.Sprintf("entry %s: stat %s: %v", entry.Name, path, serr))
				continue
			}
			if rerr := os.Remove(path); rerr != nil {
				out.Errs = append(out.Errs, fmt.Sprintf("entry %s: remove %s: %v", entry.Name, path, rerr))
				continue
			}
			freed += info.Size()
			removedAny = true
		}
		if !removedAny {
			continue
		}
		out.Removed = append(out.Removed, entry.Shard+"/"+entry.Name)
		out.RemovedEntries++
		out.FreedBytes += freed
		out.After -= freed
	}
	return out, nil
}

// entrySortKey is the deterministic tie-breaker for two entries that share an
// mtime: a total order means two runs over the same cache remove the same set
// (Rule 10.4: the smallest sample must be reproducible).
func entrySortKey(e ToolchainCacheEntry) string { return e.Shard + "/" + e.Name }

// verifyCacheEntryAtDelete re-reads ONE entry's shard immediately before the
// removal and answers whether it may still be removed. It returns a non-empty
// reason when the entry must be SKIPPED, and otherwise the entry as re-measured
// (whose Paths are the files that exist NOW — the toolchain's own trim may have
// taken one half of the pair in between).
//
// Both skip directions are the conservative one: an entry that cannot be
// re-measured is not removed (an unmeasurable is kept, the rule this whole package
// follows), and an entry that has become fresh is likewise kept. A shard that
// cannot be listed is reported as a skip with its reason rather than silently
// turning into "nothing to do".
func verifyCacheEntryAtDelete(dir string, entry ToolchainCacheEntry, cutoff time.Time) (skip string, live ToolchainCacheEntry) {
	shardPath := filepath.Join(dir, entry.Shard)
	current, err := measureToolchainCacheEntries(shardPath, entry.Shard)
	if err != nil {
		return fmt.Sprintf("%s/%s: kept — its shard could not be re-read before the removal (%v)", entry.Shard, entry.Name, err), entry
	}
	id, ok := ToolchainCacheEntryName(entry.Name)
	if !ok {
		return fmt.Sprintf("%s/%s: kept — the entry name is not a cache-entry name", entry.Shard, entry.Name), entry
	}
	for _, cur := range current {
		curID, curOK := ToolchainCacheEntryName(cur.Name)
		if !curOK || curID != id {
			continue
		}
		if !cur.Mod.Before(cutoff) {
			return fmt.Sprintf("%s/%s: kept — it became fresh (newest file %s) since the measurement", entry.Shard, entry.Name, cur.Mod.UTC().Format(time.RFC3339)), entry
		}
		return "", cur
	}
	return fmt.Sprintf("%s/%s: kept — it is gone (a concurrent reclaim or the toolchain's own trim removed it)", entry.Shard, entry.Name), entry
}

// FilesystemTypeName classifies a path by its MEASURED statfs type and returns a
// NAME, so a report can say which filesystem carries a cache without inventing
// one. It is the generalisation of MemoryFilesystem (rootgocache.go), which
// answers the narrower tmpfs/ramfs question: here the answer must distinguish the
// block-backed families too, because "which partition is full?" is a question
// about device ids and types, and /home being ext4 while `/` is ext4 is exactly
// the case a spelling cannot see.
//
// The known magics are NAMED constants (Rule 1: no bare literals for values that
// carry meaning), and an unknown magic is reported by its number rather than
// guessed (Rule 15).
func FilesystemTypeName(path string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "", fmt.Errorf("statfs(%s): %w", path, err)
	}
	magic := uint64(st.Type)
	switch magic {
	case TmpfsMagic:
		return "tmpfs", nil
	case RamfsMagic:
		return "ramfs", nil
	case ext4SuperMagic:
		return "ext4", nil
	case xfsSuperMagic:
		return "xfs", nil
	case btrfsSuperMagic:
		return "btrfs", nil
	case overlayfsSuperMagic:
		return "overlayfs", nil
	}
	return fmt.Sprintf("filesystem-type-0x%x", magic), nil
}

// The filesystem magic numbers this report can NAME. Only the ones a Linux host
// with a block-backed /home can plausibly carry are listed, and an unlisted magic
// is printed as its own number (never as a plausible default).
const (
	ext4SuperMagic      = 0xef53
	xfsSuperMagic       = 0x58465342
	btrfsSuperMagic     = 0x9123683e
	overlayfsSuperMagic = 0x794c7630
)

// FilesystemCapacity reports the total and FREE bytes of the filesystem holding
// path, as the kernel measures them for an unprivileged writer (Bavail excludes
// the root-reserved blocks — the same reading FreeBytes takes, and the one that
// answers "how much room do I actually have").
//
// An unreadable path or an implausible geometry is an error, never a guess
// (Rule 15). It is a separate function from FreeBytes because the criterion needs
// BOTH numbers in one measured breath: a floor compared against bytes from one
// call and a total from another could describe two different mounts.
func FilesystemCapacity(path string) (uint64, uint64, error) {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		target = resolved
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(target, &st); err != nil {
		return 0, 0, fmt.Errorf("statfs(%s): %w", target, err)
	}
	if st.Bsize <= 0 {
		return 0, 0, fmt.Errorf("statfs(%s): implausible block size %d", target, st.Bsize)
	}
	return uint64(st.Blocks) * uint64(st.Bsize), uint64(st.Bavail) * uint64(st.Bsize), nil
}

// PathDeviceNumber reports the STAT(2) device number of the filesystem holding
// path — the value `stat -c %d` prints (e.g. 66309 for /home on this host, 66307
// for `/`), and therefore the one a reader can verify with their own tool.
//
// It is NOT DeviceID: that returns the statfs Fsid, which identifies a filesystem
// just as well for a two-path comparison but is not a number any external tool
// shows. A per-report number must be checkable (Rule 15b), so a report uses this;
// a placement comparison keeps using DeviceID, whose semantics the existing
// off-/ and off-shm assertions are already pinned to.
func PathDeviceNumber(path string) (uint64, error) {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		target = resolved
	}
	var st syscall.Stat_t
	if err := syscall.Stat(target, &st); err != nil {
		return 0, fmt.Errorf("stat(%s): %w", target, err)
	}
	return uint64(st.Dev), nil
}

// ErrToolchainCacheRefused is the sentinel a caller can match with errors.Is when
// the criterion refused — kept because a caller that must ABSTAIN needs to tell a
// refusal from an I/O failure without string matching.
var ErrToolchainCacheRefused = errors.New("toolchain build cache refused by the headroom/bound criterion")
