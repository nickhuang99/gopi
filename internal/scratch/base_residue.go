// Base residue — the scratch family that lives DIRECTLY in the memory base.
//
// ROOT CAUSE (measured on this host, 2026-09-15, devdoc/scratch-base-residue.md):
// agent sessions create ad-hoc scratch directories straight in the tmpfs memory
// base (/dev/shm) — `gopi-verify-<taskid>`, `gocache-<taskid>`,
// `gopi-acc-<taskid>`, `ledger-snap*.db`, `*-final.log` — instead of under the
// owned scratch home. Every existing sweep is namespace-limited BY
// CONSTRUCTION and therefore matches NONE of them:
//
//   - the home sweep (gc.go) walks only HomeDir() and collects
//     suite-/legacy/queue names;
//   - the legacy residue sweep (internal/testutil) walks the memory base but
//     matches only the `gopi-` + `suite-tmp-` infix.
//
// So this family had no namespace, no collector and no lifetime: 2.6 GB in 702
// entries filled the 7.8 GB tmpfs to 100% and broke builds (sqlite "disk is
// full", ENOSPC in /dev/shm — devdoc/scratch-base-residue.md). The host
// has since been reclaimed by hand (re-measured the same day: 1.2G/7.8G, 9
// entries) — the DEFECT IS THE MECHANISM, not the byte count.
//
// WHAT THIS FILE IS, and the three rules it inherits unchanged from gc.go:
//
//  1. NAMESPACE-LIMITED. Only a name this system's own commands produce is a
//     candidate (below). A neighbouring program's directory, a user's file, a
//     database, a log — none of them match, so none of them can ever be
//     deleted by this sweep. The name layer and the KIND layer are one
//     predicate now, and the seam between them is pinned by its own test —
//     devdoc/scratch-base-residue-file-shape.md is the ruling that a name in
//     the namespace with no collector is a defect rather than a resting state.
//  2. AGE. Only entries older than the threshold the caller passes are
//     candidates — the caller supplies scratch.ResolveStaleAge(), i.e. the SAME
//     window the home sweep uses (one policy, never two to keep in sync).
//  3. HOLDER. Only entries held by NO live process are candidates: ONE batched
//     /proc cwd+fd+exe+maps pass (LiveHolders) plus the .holder registration.
//     A directory held by a live process is NEVER removed — the hard rule.
//
// And the boundaries that make the base a different place from the home:
//
//   - The base is shared and WORLD-WRITABLE (mode 1777 tmpfs). The namespace
//     predicate is therefore the ONLY thing standing between "sweep" and
//     "delete another user's data", which is why it is narrow, shape-checked
//     and table-tested rather than a lone prefix test.
//   - The sweep walks the DIRECT CHILDREN of the base it is given and never
//     recurses and never resolves a symlink: a foreign tree that happens to sit
//     in the base is a single entry that is skipped.
//   - The base is never created (a read-only dry run must not be able to bring
//     a tree into existence) — unlike the scratch home, which is created and
//     owned. A missing base is reported as EMPTY, not created.
//   - A base on the same filesystem as `/` is refused (Rule 22: measured device
//     ids, never spelling): a base on the boot partition is not this sweep's
//     target (the /tmp ban — scratch/tmproot.go). The base was a tmpfs "by
//     definition" while the scratch home was memory-backed; since the
//     2026-09-18 ruling it is a RETIRED location whose LEGACY residue this
//     sweep exists to collect, so the check is the device one and never a
//     requirement that the base still be RAM.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// ResolveBaseEnvName is the ONE name of the base knob ResolveBase reads
	// (Rule 2: read in exactly one place). It is deliberately NOT the host's
	// GOPI_SCRATCH_MEMORY_BASE (tmproot.go's MemoryBaseEnvName): that variable
	// is overridden in fixtures by the tests (testutil.MemoryBase), and a
	// production sweep must never inherit a fixture's base. The memory base is
	// HOST configuration; the reader below is where its env wins, so a test
	// that must exercise the real resolution points this at its fixture.
	ResolveBaseEnvName = "GOPI_SCRATCH_BASE"

	// DefaultBasePath is the base when nothing overrides it: the standard Linux
	// shared-memory filesystem — the location RECOGNISED as the legacy
	// memory-base residue namespace, not one this system creates in any more
	// (operator ruling 2026-09-18: the scratch home is a real directory on the
	// block device). The value is the same one the suite layer aliases
	// (internal/testutil.MemoryBaseDefault) and the retired setup script used to
	// mount; the sweep reads it to COLLECT what older revisions left there.
	DefaultBasePath = worktreeroots.ShmDir

	// BaseResiduePrefixGopi is the repo's OWN directory-name prefix: the
	// historical suite roots (internal/testutil's "gopi-suite-tmp-", matched by
	// the legacy sweep) and every ad-hoc per-task directory an agent session
	// measured in the base ("gopi-verify-*", "gopi-acc-*", "gopi-rule11-*",
	// "gopi-voice-*", "gopi-privhint-*", "gopi-sg-probe*"). One prefix, one
	// namespace — NOT a new invented one.
	BaseResiduePrefixGopi = "gopi-"

	// BaseResiduePrefixGocache names a Go build cache an agent session pointed
	// at the memory base BY HAND (GOCACHE=<base>/gocache-<taskid> — the env
	// decision belongs to the operator, the collectability decision belongs to
	// this sweep). No repo literal emits it, so it is justified by the measured
	// residue: 89 such directories, the largest single family in the 702.
	BaseResiduePrefixGocache = "gocache-"

	// BaseResiduePrefixProbe names the short-lived probe directory a session
	// creates to answer "is <base> writable / on which device" — ALWAYS with a
	// generated suffix ("probe-<rand>"/"probe-<pid>"), because it is created by
	// os.MkdirTemp. A named probe directory ("probe-cfg", "probe-root") is
	// deliberately NOT a candidate.
	BaseResiduePrefixProbe = "probe-"

	// baseResidueHexTokenLen is the length of a TASK-ID-shaped token (see
	// isLowerHex): the repo's task ids are 32-character lowercase hex
	// ("01a0e5aa3dc9734f86169b7a82d93117"), and a run root a fixture names after
	// one is a name this system wrote rather than a word a person chose. It is
	// bounded below so a short hex word ("abcd") cannot pass as an id.
	baseResidueHexTokenLen = 8

	// BaseResidueGeneratedTokenMinLen is the shortest RANDOM-KEY token the rule
	// accepts ("gocache-k7fq"): a key has to be long enough to carry the digit
	// mix isBase36ish demands. Three-letter tokens ("cfg", "abc") are a word's
	// length, not a key's.
	BaseResidueGeneratedTokenMinLen = 4

	// baseResidueUUIDLen is the length of a canonical UUID's 8-4-4-4-12 spelling
	// (hyphens included): the one generated shape an operator's own `mktemp -d`
	// leaves under the base.
	baseResidueUUIDLen = 36

	// SuiteRunToken is the second TOKEN of a suite's per-run root name
	// ("suite-cmd-agent-run-<rand>"): internal/testutil's pin creates its root
	// with os.MkdirTemp(<placement dir>, "suite-<suite>-run-"), so the name is
	// the marker plus the suite's own name plus this token. It is the ONE
	// definition of that spelling (Rule 14): the home sweep matches the same
	// shape through SuiteRunMarker, and the marker test pins the two together.
	//
	// WHY THE SHAPE IS NOT "A SUITE NAME PICKED BY A HUMAN": a suite name is a
	// WORD ("cmd-agent", "internal-tools"), indistinguishable by itself from a
	// directory an operator named "suite-notes". What makes the name this
	// system's is the GENERATED tail — the same rule the gocache/probe families
	// already obey, and the reason this arm reuses baseResidueSuffixGenerated
	// rather than a second, weaker one (see IsBaseResidueName).
	SuiteRunToken = "run"

	// suiteRunRandomModulus / suiteRunRandomMax / suiteRunRandomMin bound the
	// randomness of that tail: os.MkdirTemp appends its own generated string to
	// the pattern ("suite-agent-run-3113361481" — 8-10 digits on this host), so
	// a name carrying FEWER than suiteRunRandomMin digits after the token is a
	// name a human typed ("suite-notes-run-1") while one carrying a hex task id
	// (the legacy "gopi-tools-suite-tmp-" spelling) or a UUID is a name a
	// command wrote.
	suiteRunRandomMin = 2
	// suiteRunRandomModulus / suiteRunRandomMax describe the numeric range
	// os.MkdirTemp's suffix was measured in: a base-10 integer below
	// suiteRunRandomMax. The modulus arm is what puts the digits in the
	// DISTINCT positions the generator guarantees (Go's rand.Uint32()%modulus
	// leaves the high digits random but the low ones well mixed), so a counter
	// a human spelled cannot pass.
	suiteRunRandomModulus = 10
	suiteRunRandomMax     = 1 << 32
)

// baseResiduePrefixes lists the families this sweep may ever consider. It is a
// package-level slice (not a map) so the order is stable for the tests that
// walk it, and it is unexported because the exported surface is the PREDICATE
// (IsBaseResidueName / IsBaseResiduePath), never a second list to keep in sync.
var baseResiduePrefixes = []string{
	BaseResiduePrefixGopi,
	BaseResiduePrefixGocache,
	BaseResiduePrefixProbe,
}

// BaseResidueLegacyGoCacheShapes names the shapes the LEGACY bare-key build-cache
// family may contain and still be ours.
//
// WHY THE FAMILY NEEDED ITS OWN ARM (measured 2026-10-01, this host): a run of
// the merge-time BUILD gate's own PLACEMENT TEST left
// `<base>/<16-hex>/gocache` behind — 171 such trees, each holding a single
// empty `gocache`, because the test that ASSERTS the "never a cache on RAM" ban
// pins the cache root AT the memory base, and the gate created the cache
// directory BEFORE it measured the placement. The `gocache` LEAF is the toolchain's
// standard leaf name (it sorts before any alphanumeric suffix, so it cannot
// collide with the `gocache-<token>` PREGENERATED family above — measured: the
// two shapes share no name).
//
// THIS CONSTANT IS THE SHAPE TABLE, and it is what keeps the arm from becoming
// the blunt "delete anything that looks hex-ish" it must never be: a child of
// the key directory that is neither a toolchain entry SHARD nor one of the
// toolchain's own BOOKKEEPING files proves the directory is somebody else's
// tree, and the entry is KEPT.
var BaseResidueLegacyGoCacheShapes = baseResidueLegacyGoCacheShapes

// baseResidueLegacyGoCacheShapes is the ONE definition of that table (Rule 14 —
// the name half is exported through the alias above because a test and a report
// quote the shapes).
//
// The four leaf names are the Go toolchain's OWN (GOCACHE's README, its trim
// bookkeeping, its log and the CACHEDIR.TAG marker every cache directory
// carries) — they are values WRITTEN BY `go`, not chosen here, which is the
// GOTMPDIR case: the toolchain owns the spelling and this sweep only recognises
// it.
var baseResidueLegacyGoCacheShapes = []string{
	"README",
	"trim.txt",
	"log.txt",
	"CACHEDIR.TAG",
}

// baseResidueLegacyGoCacheSuffix is the LEGACY bare-key family's suffix: the
// child's name with the toolchain's leaf appended (`<16-hex>/gocache`). The
// value is the WRITER's own leaf constant (Rule 14: one definition — the gate
// joins it to build its cache path, this sweep joins it to recognise one), and
// it is a NAMED value here because the whole family is identified by it.
const baseResidueLegacyGoCacheSuffix = BuildCacheGoCacheLeafName

// isLegacyBareKeyGoCacheName reports whether name is the KEY DIRECTORY of the
// legacy bare-shaped build-cache family: exactly the writer's key shape
// (buildCacheKeyName — the SAME predicate the declared build-cache tree uses,
// Rule 13: one shape, two consumers), with NO suffix after it.
//
// The no-suffix half is what separates it from the pregenerated family above:
// `gocache-<token>` carries the family prefix and `IsBaseResidueName` matches it
// there; `<16-hex>-<token>` carries a dash and this predicate refuses it.
func isLegacyBareKeyGoCacheName(name string) bool {
	return !strings.Contains(name, "-") && buildCacheKeyName(name)
}

// isLegacyBareKeyGoCacheDir reports whether path is a COLLECTABLE instance of
// that family: a real directory (never a symlink — the hard kind rule), whose
// only child is the toolchain's `gocache` leaf, and whose entire content is
// cache-shaped.
//
// WHY THE CONTENT IS MEASURED AND NOT ASSUMED: the family was minted empty (the
// refusing arm aborts before any build writes into it), so "holds a single
// `gocache`" is the whole measured shape. An entry that holds anything else —
// another child, or a `gocache` containing something that is neither a shard
// nor toolchain bookkeeping — is a tree this sweep did not create, and is KEPT
// (Rule 15: unknown is never a delete).
func isLegacyBareKeyGoCacheDir(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return false
	}
	children, err := os.ReadDir(path)
	if err != nil || len(children) != 1 {
		return false // unmeasurable, or not the single-leaf shape: keep it
	}
	if children[0].Name() != baseResidueLegacyGoCacheSuffix || !children[0].IsDir() {
		return false
	}
	body, err := os.ReadDir(filepath.Join(path, baseResidueLegacyGoCacheSuffix))
	if err != nil {
		return false
	}
	for _, e := range body {
		if e.IsDir() && buildCacheShardName(e.Name()) {
			continue
		}
		if e.IsDir() {
			return false // a directory that is not an entry shard: not a cache we wrote
		}
		if !slices.Contains(baseResidueLegacyGoCacheShapes, e.Name()) {
			return false // a file the toolchain does not write there: not ours
		}
	}
	return true
}

// legacyBareKeyGoCacheCollectable is the DELETE predicate of that family. It
// applies the SAME age + liveness rule every other arm of this sweep applies
// (staleUnheldDirectory — one rule, N consumers), and it lives here rather than
// inside the verdict function because a `gocache`-shaped NAME must not be
// collectable by the generic namespace walk (see baseResidueLegacyGoCachePlan).
func legacyBareKeyGoCacheCollectable(path string, now time.Time, olderThan time.Duration, holders HolderScan, self string) bool {
	if !isLegacyBareKeyGoCacheDir(path) {
		return false
	}
	return staleUnheldDirectory(path, now, olderThan, holders, self)
}

// sweepDefaultBaseLegacyGoCaches is the DELETING face of the family, called only
// from SweepBase — the operator's explicit one-shot (`gopi tool scratch-gc base`),
// never the resident tick.
//
// A refusal is reported and the walk continues (Rule 15b: one unremovable entry
// must not stop the rest, and a failure is never swallowed).
func sweepLegacyBareKeyGoCaches(base string, olderThan time.Duration) int {
	if base == "" {
		base = ResolveBase()
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "scratch: legacy bare-key cache sweep could not read %s: %v\n", base, err)
		return 0
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	var holders HolderScan
	holdersLoaded := false
	removed := 0
	for i, entry := range entries {
		if i >= ScanLimit {
			fmt.Fprintf(os.Stderr, "scratch: legacy bare-key cache sweep of %s stopped early at %d entries\n", base, ScanLimit)
			break
		}
		name := entry.Name()
		if !isLegacyBareKeyGoCacheName(name) {
			continue
		}
		path := filepath.Join(base, name)
		if !holdersLoaded {
			holders = ScanHolders()
			holdersLoaded = true
		}
		if !legacyBareKeyGoCacheCollectable(path, now, olderThan, holders, self) {
			continue
		}
		if err := removeCollectable(path); err != nil {
			fmt.Fprintf(os.Stderr, "scratch: legacy bare-key cache sweep could not remove %s: %v\n", path, err)
			continue
		}
		removed++
	}
	return removed
}

// BaseResidueLegacyGoCachePlan is the PLAN face of the same family — the
// --dry-run twin of sweepDefaultBaseLegacyGoCaches, built from the SAME
// predicate so the promise and the behaviour cannot disagree.
func BaseResidueLegacyGoCachePlan(base string, olderThan time.Duration) ([]ScratchPlanEntry, error) {
	if base == "" {
		base = ResolveBase()
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read memory base %s: %w", base, err)
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	var holders HolderScan
	holdersLoaded := false
	var plan []ScratchPlanEntry
	for i, entry := range entries {
		if i >= ScanLimit {
			return plan, fmt.Errorf("memory base %s has more than %d entries — legacy plan stopped early", base, ScanLimit)
		}
		if !isLegacyBareKeyGoCacheName(entry.Name()) {
			continue
		}
		path := filepath.Join(base, entry.Name())
		if !holdersLoaded {
			holders = ScanHolders()
			holdersLoaded = true
		}
		if !legacyBareKeyGoCacheCollectable(path, now, olderThan, holders, self) {
			continue
		}
		fi, lerr := os.Lstat(path)
		if lerr != nil {
			continue // vanished between the predicate and the measurement
		}
		plan = append(plan, ScratchPlanEntry{Path: path, Age: now.Sub(fi.ModTime())})
	}
	return plan, nil
}

// BaseResidueLegacyGoCacheVerdicts is the --explain face of the family: every
// BARE-KEY entry in the base with the sentence that decided it, so an operator
// asking "why is this residue still here?" is answered for the shapes the
// generic namespace walk never reaches.
func BaseResidueLegacyGoCacheVerdicts(base string, olderThan time.Duration) ([]EntryVerdict, error) {
	if base == "" {
		base = ResolveBase()
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read memory base %s: %w", base, err)
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	var holders HolderScan
	holdersLoaded := false
	var out []EntryVerdict
	for i, entry := range entries {
		if i >= ScanLimit {
			return out, fmt.Errorf("memory base %s has more than %d entries — legacy explain stopped early", base, ScanLimit)
		}
		if !isLegacyBareKeyGoCacheName(entry.Name()) {
			continue
		}
		path := filepath.Join(base, entry.Name())
		v := EntryVerdict{Path: path}
		fi, lerr := os.Lstat(path)
		if lerr != nil {
			v.Reason = fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, lerr)
			out = append(out, v)
			continue
		}
		v.Age = now.Sub(fi.ModTime())
		if !isLegacyBareKeyGoCacheDir(path) {
			// In our KEY namespace, but not the shape this system left: a
			// directory a person or another program made is never a candidate,
			// and saying so is the point of this face.
			v.Reason = BaseResidueKeepNotLegacyBareKeyCacheShape
			out = append(out, v)
			continue
		}
		if !holdersLoaded {
			holders = ScanHolders()
			holdersLoaded = true
		}
		if !staleUnheldDirectory(path, now, olderThan, holders, self) {
			v.Reason = baseResidueKeepReason(path, now, olderThan, holders, self)
			out = append(out, v)
			continue
		}
		v.Collectable = true
		v.Reason = EntryCollectablePrefix + OwnerReasonUnregistered
		out = append(out, v)
	}
	return out, nil
}

// autoBaseResidueEnabled is the TEST seam that keeps the SUITE HOST SAFE: the
// unit tests must exercise the AUTOMATIC base resolution (ResolveBase) without
// letting an auto-resolved base sweep the developer's REAL /dev/shm (the tests
// run on the very host whose residue this sweep collects — Rule 6: a test never
// touches the developer's real state). The zero value is true, so nothing in
// production can ever turn the automatic arm off; internal/testutil's TestMain
// pin calls SetAutoBaseResidue(false) for the duration of a suite, and the ONE
// test that deliberately arms it re-enables it for its own body (and restores
// it). It is a process-global because that is exactly the scope of the thing it
// guards — the suite's own process.
var autoBaseResidueEnabled = true

// SetAutoBaseResidue turns the automatic (unconfigured) base arm on or off and
// returns the previous value, so a caller restores exactly what it found.
func SetAutoBaseResidue(enabled bool) bool {
	prev := autoBaseResidueEnabled
	autoBaseResidueEnabled = enabled
	return prev
}

// ResolveBase is the ONE place the memory-base knob is read (Rule 2): the
// environment value when set, else DefaultBasePath, both canonicalised so that
// a base reached through a symlink is compared and reported in one spelling
// (the kernel reports resolved paths — see CanonicalPath).
//
// An empty resolution (nothing set and no /dev/shm) returns DefaultBasePath
// unchanged: the caller's read-only faces then report an empty plan, and only
// the sweep's own measurement decides whether that path is sweepable.
func ResolveBase() string {
	if base := strings.TrimSpace(os.Getenv(BaseEnvName)); base != "" {
		return CanonicalPath(base)
	}
	return CanonicalPath(DefaultBasePath)
}

// IsBaseResidueName reports whether a directory NAME directly inside the
// resolved memory base belongs to this system's own ad-hoc namespace — the
// whole safety argument of the base sweep, which is why it is exported and
// table-tested directly.
//
// THE RULE IS SHAPE, NOT A BARE PREFIX (a prefix test alone would collect
// another program's data the moment a user names a directory "gopi-..."):
//
//		name = <family> "-" <token> [ "-" <token> ]*
//
//	  - the FIRST dash-separated token must be EQUAL to a family, so
//	    "gopi-notes" matches the family but "gofile-x" does not;
//	  - a bare family with no suffix ("gocache-", "probe") is refused: this
//	    system never creates one, so a match would be a guess;
//	  - every remaining token must be non-empty and made of letters/digits,
//	    with at most one dash inside a token (what appendPathToken produces when
//	    de-duplicating "gopi-acc-<pid>-1");
//	  - at least one remaining token must look GENERATED rather than chosen: it
//	    carries a digit OR an underscore (os.MkdirTemp's alphabet — "gopi-verify-
//	    Xk3", "gopi-sg-probe_2"), OR it is at least BaseResidueGeneratedTokenMinLen
//	    characters AND contains a non-hex letter (the base36/random case:
//	    "gocache-k7fq", and a 32-/36-character uuid), OR it is exactly
//	    baseResidueHexTokenLen LOWERCASE HEX characters (a task id: "01a0a392",
//	    which os.MkdirTemp alone cannot produce — its alphabet includes
//	    uppercase, so the shape stays unambiguous).
//
// The refused shapes are the point: "gocache-key-2026" (both tokens are chosen
// words — no generated token anywhere), "probe-cfg", "gofi-gopi-notes" and
// "gocache-root" are all names a HUMAN picked, and a name this sweep did not
// create is not a name this sweep may delete. The accepted set is exactly the
// measured residue: "gopi-verify-01a0a387", "gocache-01a0a392",
// "gopi-acc-4211", "gopi-privhint-4211", "gopi-sg-probe".
//
// It deliberately does NOT consult age, liveness or the filesystem: those are
// the caller's half of the predicate (BaseResidueCollectable), and keeping them
// apart is what lets a test pin the namespace rule on names alone.
func IsBaseResidueName(name string) bool {
	// The legacy suite roots are THIS package's other base sweep's candidates
	// (internal/testutil.LegacyScratchBases collects them under its own
	// namespace rule). Matching them here would give one directory two
	// collectors — so they are refused EXPLICITLY rather than by accident of
	// the shape rule.
	if strings.Contains(name, LegacyScratchMarker) {
		return false
	}
	// The scratch home itself can sit IN the base (the unprivileged setup
	// symlinks <home>/workspace/gopi-test-scratch onto the memory filesystem,
	// and a test scratch home is often created there directly). It is owned by
	// the home sweep — and a base sweep that collected it would delete the live
	// tree. The comparison is by NAME, so it holds for a base that is the
	// memory filesystem itself as well as for a fixture base that contains it.
	if name == RootDirName {
		return false
	}
	// The SUITE-RUN family — the one arm that is not unconditional: the per-run
	// roots the placement creates DIRECTLY in the default memory base are the
	// family with no collector (this file's own header), and reaping them on
	// every automatic pass would make every `gopi task worker` tick delete the
	// roots of the suites running on the same host (the layout the placement
	// chooses is the SHARED /dev/shm, and their owners' registrations are not
	// always observable). They are collected by the EXPLICIT sweep instead —
	// `gopi tool scratch-gc base`, which is what cleanupDefaultBaseResidue is and
	// what the epic asks for by name. A base the caller CONFIGURED
	// (ResolveBaseEnvName) keeps the arm in both modes: that base is the
	// caller's own private tree.
	if !autoBaseResidueEnabled && nameIsDefaultBaseSuiteRun(name) {
		return false
	}
	if suiteRunTokens(name) != nil {
		return true
	}
	for _, prefix := range baseResiduePrefixes {
		token, matched := strings.CutPrefix(name, prefix)
		if !matched || token == "" {
			continue // a bare family: never a shape this system produces
		}
		return baseResidueSuffixGenerated(token)
	}
	return false
}

// suiteRunTokens splits a name into the tokens of a suite's per-run root
// ("suite-cmd-agent-run-3113361481" → ["cmd-agent" "3113361481"]) and returns
// nil when the name is not that shape.
//
// WHY THIS FAMILY NEEDED ITS OWN ARM (measured on this host 2026-09-28): the
// suite placement prefers a MEMORY filesystem
// (internal/testutil/scratch_placement.go → placementMemoryBase), and its pin
// creates its per-run root DIRECTLY in that base — so /dev/shm accumulated one
// "suite-<pkg>-run-<rand>" root per `go test` invocation with NO collector:
// 146 roots / 5.9G in a 7.8G tmpfs, of which 43 were already older than the
// stale window. The sweep that owns the base (this file) matched only the
// gopi-/gocache-/probe- prefixes, the legacy arm in internal/testutil matched
// only "gopi-suite-tmp-", and the home sweep never sees the base at all. The
// family was therefore in nobody's namespace — the same defect shape as the
// 2026-09-15 incident this file exists for, one naming scheme later.
//
// THE SHAPE RULE (narrow on purpose — the base is 1777 and shared):
//
//		suite-<suite-name>-run-<tail>
//
//	  - the SUITE NAME is one or more dash-joined tokens of the name alphabet;
//	  - "run" must be its own token, so a root name is never matched by INFIX
//	    (the home sweep's SuiteRunMarker test would accept "my-suite-runner-x";
//	    a base sweep that is one typo away from another program's directory does
//	    not get that latitude);
//	  - the TAIL must be GENERATED, by the SAME folding predicate the other
//	    families use: os.MkdirTemp's random suffix (digits, measured 8-10 of
//	    them: "suite-cmd-agent-run-3113361481"), a task id / UUID ("suite-x-run-
//	    01a0a392", "suite-internal-tools-run-<uuid>"), or a token carrying an
//	    underscore or a base36 key.
//
// REFUSED, and each one is a case a person can name: "suite-notes" (no "run"
// token), "suite-notes-run-1" (one digit is a counter a human wrote, not
// os.MkdirTemp's suffix), "suite-notes-run-latest" (a word), "my-suite-runner-2"
// ("run" is not a token), "suite--run-1234" (an empty suite name),
// "suite-notes-run-<uuid>.tar" (a dot is not in the alphabet). Every one of
// them is a name this system never created, and a name this sweep never created
// is not a name this sweep may delete.
func suiteRunTokens(name string) []string {
	rest, matched := strings.CutPrefix(name, SuiteRunMarker)
	if !matched || rest == "" {
		return nil
	}
	tokens := strings.Split(rest, "-")
	// The FIRST "run" token separates the suite name from the tail: everything
	// before it names the suite, everything after it is the generated part —
	// which may itself carry dashes, because a fixture may name its root with a
	// task id or an operator's own `mktemp -d -t` spelling
	// ("suite-x-run-550e8400-e29b-41d4-a716-446655440000").
	cut := -1
	for i, token := range tokens {
		if token == SuiteRunToken {
			cut = i
			break
		}
	}
	if cut <= 0 || cut == len(tokens)-1 {
		// Either no run token at all ("suite-notes"), or nothing after it
		// ("suite-notes-run"): neither is a root this system created.
		return nil
	}
	for _, token := range tokens[:cut] {
		if token == "" || !isBaseResidueTokenAlphabet(token) {
			return nil // "suite--run-1234": an empty suite name is not ours
		}
	}
	for _, token := range tokens[cut+1:] {
		if token == "" || !isBaseResidueTokenAlphabet(token) {
			return nil // "suite-notes-run-1234.tar": a dot is not in the alphabet
		}
		if suiteRunTailGenerated(token) {
			return tokens
		}
	}
	return nil
}

// suiteRunTailGenerated reports whether ONE tail token of a suite per-run root
// name is a shape a GENERATOR produced rather than a word a person chose.
//
// It is deliberately STRICTER than the family-wide baseResidueSuffixGenerated,
// which accepts any token carrying a digit: here that catch-all would accept
// "suite-notes-run-v2" (and every other human spelling that happens to hold a
// digit), and the base is a shared 1777 directory. The accepted shapes are the
// ones this system's writers actually emit:
//
//   - a DECIMAL run of at least suiteRunRandomMin digits — os.MkdirTemp's own
//     suffix ("3113361481"), and its de-dup retry;
//   - a LOWERCASE HEX run of at least baseResidueHexTokenLen — a task id, and
//     one group of a UUID (a fixture naming its root after a task);
//   - a token carrying an UNDERSCORE — os.MkdirTemp's alphabet, never a word a
//     human types;
//   - a base36-ish key (the family-wide rule for the random-key shape).
func suiteRunTailGenerated(token string) bool {
	switch {
	case isDecimalToken(token):
		return len(token) >= suiteRunRandomMin
	case isLowerHex(token) && len(token) >= baseResidueHexTokenLen:
		return true
	case strings.ContainsRune(token, '_'):
		return true
	case len(token) >= BaseResidueGeneratedTokenMinLen && isBase36ish(token):
		return true
	}
	return false
}

// isDecimalToken reports whether a token is a plain base-10 integer no larger
// than suiteRunRandomMax — the numeric range os.MkdirTemp's suffix was measured
// in here. A number that does not fit is not this generator's output.
func isDecimalToken(token string) bool {
	if token == "" {
		return false
	}
	for i := 0; i < len(token); i++ {
		if token[i] < '0' || token[i] > '9' {
			return false
		}
	}
	if len(token) > len("4294967295") {
		return false // longer than any uint32 spelling
	}
	n, err := strconv.ParseUint(token, 10, 64)
	if err != nil {
		return false
	}
	return n < suiteRunRandomMax
}

// baseResidueSuffixGenerated reports whether the part after the family prefix
// carries a token that a command in this repository WROTE rather than a name a
// human chose (see IsBaseResidueName's rule). Only filepath-safe names reach it
// (the caller iterates the base's directory entries).
//
// The accepted token shapes, and what each one stands for:
//
//   - the TOKEN ALPHABET: anything outside [A-Za-z0-9_] cannot be produced by a
//     filesystem name — "gopi-verify-01a0a387.tar" carries a DOT and is refused
//     here (and again, as a non-directory, by IsBaseResiduePath);
//   - a DIGIT: every writer in this repository appends one — a task id
//     ("gopi-verify-01a0a387"), a pid ("gopi-acc-4211", "gopi-privhint-4211"),
//     a de-dup counter ("gopi-acc-4211-1"), os.MkdirTemp's alphabet
//     ("gopi-sg-probe_2");
//   - an UNDERSCORE: os.MkdirTemp's alphabet, never a word a human types;
//   - a UUID: the canonical 8-4-4-4-12 hex shape, which an operator's
//     `mktemp -d` under the base produces;
//   - a long NON-HEX alphanumeric run: base36 / a random key
//     ("gocache-9f3a2c1b7e4d6a8f9c0b1d2e3f4a5b6c").
//
// WHAT IS DELIBERATELY NOT ENOUGH: a plain word ("root", "cache", "cfg",
// "notes"), a short mixed-case token ("k7fq"), and — the case this rule was
// corrected for — THIS PROCESS'S OWN PID. The pid arm looked like a precise
// match ("gopi-acc-<mypid>" is genuinely this system's residue) and it was the
// widest hole in the predicate: it matched EVERY name ending in the number
// 4211, so "gocache-root", "gopi-notes", "probe-cfg" and "gocache-key-2026"
// all read as generated whenever the sweep happened to run from pid 4211. A
// predicate whose answer depends on the CALLER'S PID is not a namespace rule —
// it was removed, and the names it admitted are pinned as REFUSED below.
func baseResidueSuffixGenerated(suffix string) bool {
	generated := false
	for _, token := range strings.Split(suffix, "-") {
		if token == "" || !isBaseResidueTokenAlphabet(token) {
			return false // "gopi--x", a dot, a plus: nothing this system writes
		}
		// EVERY token must be a shape this system writes, and AT LEAST ONE must
		// be machine-generated (a token carrying a digit, an underscore, a uuid
		// shape or a base36 key). The FOLD is the point: judged per token,
		// "gopi-privhint-4211" reads as generated because of "4211" while
		// "gopi-sg-probe" — two plain words that this system really did create —
		// would be refused. Folded, both are accepted, and the human names stay
		// refused only when EVERY token is a word.
		if !generated {
			switch {
			case strings.ContainsAny(token, "0123456789"),
				strings.ContainsRune(token, '_'),
				IsUUIDName(token),
				len(token) >= BaseResidueGeneratedTokenMinLen && isBase36ish(token):
				generated = true
			}
		}
	}
	return generated
}

// isBase36ish reports whether a token looks like a RANDOM base36 key (a build
// cache fingerprint: "k7fq", "9f3a2c1b7e4d6a8f9c0b1d2e3f4a5b6c", "eJ0u9K")
// rather than a word or a year: it must be at least one lowercase letter AND at
// least a quarter of its characters must be DIGITS. A lowercase word ("root",
// "notes", "cache") has none, and a word-with-a-counter ("key2026") mixes the
// digits into one cluster rather than throughout the token.
func isBase36ish(token string) bool {
	lower := 0
	digits := 0
	trailingRun := 0
	runIsDigits := false
	for _, r := range token {
		if r >= '0' && r <= '9' {
			if runIsDigits {
				trailingRun++
			} else {
				trailingRun = 1
				runIsDigits = true
			}
			digits++
			continue
		}
		runIsDigits = false
		if r >= 'a' && r <= 'z' {
			lower++
		}
	}
	if lower == 0 || len(token) < BaseResidueGeneratedTokenMinLen {
		return false
	}
	if digits*4 < len(token) {
		return false // too few digits to be a key
	}
	// A key interleaves: the token must not END in a digit run (that is what a
	// human's name-with-a-counter looks like — "key2026").
	return trailingRun == 0
}

// isBaseResidueTokenAlphabet reports whether a token consists only of the
// characters a name this system writes can carry: ASCII letters, digits and the
// underscore (os.MkdirTemp's alphabet plus this repository's own spellings).
// It is what refuses "gopi-verify-01a0a387.tar" by NAME, before the entry's kind
// is even inspected.
func isBaseResidueTokenAlphabet(token string) bool {
	for _, r := range token {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return true
}

// IsUUIDName reports whether a name is a canonical UUID (8-4-4-4-12 lowercase
// hex, optionally brace-wrapped). It is exported because it is a general shape
// question a caller asks about a NAME (not a path), and because the base sweep
// must recognise the spelling an operator's `mktemp -d -t` leaves behind — the
// one generated shape that carries no digit in its TOKENS but is unmistakably
// machine-written.
func IsUUIDName(name string) bool {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(name, "{"), "}")
	if len(trimmed) != baseResidueUUIDLen {
		return false
	}
	dashPositions := []int{8, 13, 18, 23}
	nextDash := 0
	for i := 0; i < len(trimmed); i++ {
		if nextDash < len(dashPositions) && i == dashPositions[nextDash] {
			if trimmed[i] != '-' {
				return false
			}
			nextDash++
			continue
		}
		if !isLowerHexChar(trimmed[i]) {
			return false
		}
	}
	return true
}

// isLowerHexChar / isLowerHex report whether a byte (a string) is a run of
// 0-9a-f — the task-id shape, used by the UUID test above.
func isLowerHexChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isLowerHexChar(s[i]) {
			return false
		}
	}
	return s != ""
}

// isBaseResidueContainer is the KIND gate of the base sweep: an entry this
// sweep may consider is a DIRECTORY or a REGULAR FILE, judged from an Lstat
// (which is also why a symlink is judged as a symlink and never followed).
//
// WHY FILES ARE IN, AND WHY THAT IS A FIX RATHER THAN A WIDENING (measured
// 2026-10-01): an agent session that builds the binary with `-o /dev/shm/gopi-…`
// leaves the LINKER'S OUTPUT — a 28 MB regular file — as a direct child of the
// base, under a name this system's own generator wrote ("gopi-red2",
// "gopi-after-01a0e8c7", "gopi-verify", …). The NAME layer said yes
// (IsBaseResidueName is true for every one of them) while the KIND layer said no,
// so the entry was in the namespace with NO collector: `gopi tool scratch-gc base`
// reported removed=0 and `--explain` printed not one line about it, and the
// residue was permanent (four such files, ~113 MB, invisible to every sweep —
// and to the suite-*/home sweeps too, which never walk the base's direct
// children).
//
// A FILE HAS NO IDENTITY BEYOND ITS NAME, so the namespace rule (which is what
// decides "did this system write this name?") is the whole of the safety
// argument here — and on a 1777 shared base that rule is shape-checked and
// table-tested precisely because it has to carry that weight alone. The DIRECTORIES
// this sweep protected itself against were never the risk: a symlink is refused
// below and stays refused, because following one means deleting whatever it
// points at (a target that may be nowhere near the base).
//
// A socket, a fifo, a block/character device is NEVER a candidate: this sweep
// deletes residue, and deleting a device node a neighbour's daemon is using is
// the one mistake that cannot be undone.
func isBaseResidueContainer(fi os.FileInfo) bool {
	if fi.Mode()&os.ModeSymlink != 0 {
		return false // a link is someone's pointer; its target is not ours to remove
	}
	return fi.IsDir() || fi.Mode().IsRegular()
}

// IsBaseResiduePath reports whether path is one of this system's own ad-hoc
// entries directly inside the base: the NAME rule above, PLUS the entry's own
// KIND (Lstat, so a symlink is judged as a symlink). It is checked BEFORE the
// age and holder questions, so a foreign or wrong-kind entry is never even
// measured.
//
// An unreadable entry (Lstat error) is not a candidate: an entry this sweep
// cannot inspect is never one it deletes (Rule 15).
func IsBaseResiduePath(path, name string) bool {
	if !IsBaseResidueName(name) {
		return false
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return isBaseResidueContainer(fi)
}

// The KIND refusals, as reportable sentences. Constants for the same reason as
// every other reason string here (Rule 1b/13): the CLI's --explain line and its
// tests quote them, and a reason typed twice drifts.
const (
	// BaseResidueKeepSymlink is the refusal of a link — the ONE kind the
	// 2026-10-01 relaxation must never admit.
	BaseResidueKeepSymlink = "a symlink is never a candidate — its target is not this sweep's to remove"
	// BaseResidueKeepNotLegacyBareKeyCacheShape is the refusal of a bare-key
	// entry whose CONTENT is not this system's: the name is in our key shape but
	// the tree inside it is somebody else's, and a name alone is never a delete
	// authorisation.
	BaseResidueKeepNotLegacyBareKeyCacheShape = "the name is our key shape but the tree inside is not a build cache we wrote — kept"
	// BaseResidueKeepNotFileOrDir is the refusal of every other kind (socket,
	// fifo, device): not something this system's commands leave behind.
	BaseResidueKeepNotFileOrDir = "neither a directory nor a regular file — never this system's residue"
)

// BaseResidueVerdictFor is the ONE collectability predicate of the base sweep,
// in its REPORTING form: a directory OR regular file in this system's own
// namespace, older than olderThan, and held by no live process — plus the
// sentence that decided it. BaseResidueCollectable (below), BaseResiduePlan (the
// dry-run face), SweepBase (the deleting face) and the CLI's --explain face all
// call THIS function, so a report can never give a reason the sweep disagrees
// with, and a plan can never promise something the sweep would refuse — or vice
// versa.
//
// holders is the ONE batched /proc pass the caller collected (LiveHolders):
// asking the holder question per entry would cost a full process-table walk per
// candidate, which is the defect that once hung the queue sweep (gc.go).
//
// self is the sweeping process's own pid string (see unheld): an entry WE hold
// is still collectable, because a previous run of our own pid cannot be alive —
// otherwise the sweep could never collect its own leftovers.
//
// A name in this system's namespace that the sweep will NOT collect is exactly
// the case this function exists to give a REASON for (Rule 15: a silent skip and
// a working sweep look identical from a removal count alone — measured: four
// 28 MB compiler outputs sat in /dev/shm while the verb reported removed=0 and
// --explain printed nothing).
func BaseResidueVerdictFor(path, name string, now time.Time, olderThan time.Duration, holders HolderScan, self string) EntryVerdict {
	v := EntryVerdict{Path: path}
	if !IsBaseResidueName(name) {
		v.Reason = EntryKeepNotOurs
		return v
	}
	fi, err := os.Lstat(path)
	if err != nil {
		// Vanished, or unreadable: a fact about an entry we could not measure is
		// never a delete authorisation (Rule 15).
		v.Reason = fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, err)
		return v
	}
	if !isBaseResidueContainer(fi) {
		if fi.Mode()&os.ModeSymlink != 0 {
			v.Reason = BaseResidueKeepSymlink
			return v
		}
		v.Reason = BaseResidueKeepNotFileOrDir
		return v
	}
	v.Age = now.Sub(fi.ModTime())
	if !staleUnheldDirectory(path, now, olderThan, holders, self) {
		// The age/holder half is shared with every other arm
		// (staleUnheldDirectory), so this function names the arm that decided it
		// without ever re-deciding it.
		v.Reason = baseResidueKeepReason(path, now, olderThan, holders, self)
		return v
	}
	v.Collectable = true
	v.Reason = EntryCollectablePrefix + OwnerReasonUnregistered
	return v
}

// baseResidueKeepReason names WHICH half of the shared age/holder predicate kept
// an in-namespace entry, in the same order staleUnheldDirectory applies them —
// so the sentence a report prints is the reason the sweep acted on, never a
// second opinion about it.
func baseResidueKeepReason(path string, now time.Time, olderThan time.Duration, holders HolderScan, self string) string {
	stale, err := ScratchEntryStale(path, now, olderThan)
	if err != nil {
		return fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, err)
	}
	if !stale {
		return fmt.Sprintf("%s (%s)", EntryKeepFresh, olderThan)
	}
	if !unheldBatch(path, holders) {
		// CHECKED FIRST, and not in the other order: the .holder registration
		// says a pid was recorded, which is a WRITER's claim, while the scan
		// truncation says the kernel was not fully read. Reporting the
		// registration as "a live process holds it" would present the weaker
		// evidence as the stronger one (Rule 15).
		if holders.Truncated {
			return HolderScanWithheldReason()
		}
		if _, holder := HolderAlive(path); holder != "" {
			return fmt.Sprintf("%s (the %s registration names live pid %s)", EntryKeepLiveHolder, HolderFileName, holder)
		}
		return EntryKeepLiveHolder
	}
	return EntryKeepLiveHolder
}

// BaseResidueCollectable is BaseResidueVerdictFor's boolean face, kept because
// it is the predicate a caller asks a yes/no question of. It carries no second
// rule: the answer is the verdict's own Collectable field.
func BaseResidueCollectable(path, name string, now time.Time, olderThan time.Duration, holders HolderScan, self string) bool {
	return BaseResidueVerdictFor(path, name, now, olderThan, holders, self).Collectable
}

// BaseResidueVerdicts is the base sweep's --explain face: EVERY entry of the base
// whose NAME is in this system's namespace, with the sentence that decided
// whether it may be collected. It deletes nothing and takes no lock.
//
// WHY THE NAMESPACE IS THE FILTER (and not "every entry in the base"): the
// question an operator brings to `--explain` is "why is THIS residue still
// here?", and the base is a shared 1777 directory full of neighbours' files
// (measured: 305 entries on this host, most of them not ours). Printing a
// verdict per neighbour would bury the answer in noise about entries that were
// never candidates by construction — while an entry in OUR namespace that the
// sweep refuses is a defect report, and that is precisely the line that used to
// be missing.
//
// It shares BaseResidueVerdictFor with the plan and the sweep, so the three
// faces cannot disagree.
func BaseResidueVerdicts(base string, olderThan time.Duration) ([]EntryVerdict, error) {
	if base == "" {
		base = ResolveBase()
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // a base that is not there is the expected non-event
		}
		return nil, fmt.Errorf("read memory base %s: %w", base, err)
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	var holders HolderScan
	holdersLoaded := false
	out := make([]EntryVerdict, 0, len(entries))
	for i, entry := range entries {
		if i >= ScanLimit {
			return out, fmt.Errorf("memory base %s has more than %d entries — report stopped early", base, ScanLimit)
		}
		name := entry.Name()
		if !IsBaseResidueName(name) {
			continue // not ours: never a candidate, so never a verdict to explain
		}
		if !holdersLoaded {
			holders = ScanHolders()
			holdersLoaded = true
		}
		out = append(out, BaseResidueVerdictFor(filepath.Join(base, name), name, now, olderThan, holders, self))
	}
	return out, nil
}

// ownSelfHolder reports whether the ONLY live reason to keep path is this very
// process's pid. It exists because the batched /proc pass cannot distinguish
// "this sweep's own shell is cd'd into the candidate" (a session that swept its
// own working directory — collectable) from "another live session holds it"
// (kept), while DirHolder answers exactly that. Rule 15: a holder check that
// cannot be completed is NOT a delete authorisation, so the expensive
// re-measurement only ever runs to LOOSEN a veto, never to grant one.
func ownSelfHolder(path, self string) bool {
	if self == "" {
		return false
	}
	holder, err := DirHolder(path)
	if err != nil {
		return false // could not measure: the batched answer stands
	}
	return holder == self
}

// BaseResiduePlan reports the entries SweepBase would collect, deleting
// nothing. It shares BaseResidueCollectable with the sweep, so a --dry-run
// against a base is a promise about what the real sweep does.
//
// A missing base is an EMPTY plan, not an error and not a directory: the
// dry-run face must never bring a tree into existence (SweepBase's contract),
// and "there is no memory base on this host" is the expected non-event.
func BaseResiduePlan(base string, olderThan time.Duration) ([]ScratchPlanEntry, error) {
	if base == "" {
		base = ResolveBase()
	}
	if base == DefaultBasePath && !autoBaseResidueEnabled {
		// The dry-run twin of SweepBase's stand-down (see autoBaseResidueEnabled):
		// the plan face must never promise entries the sweep would refuse.
		return nil, nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read memory base %s: %w", base, err)
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	// ONE /proc pass, collected ONCE and shared by every candidate — and only
	// if there is a candidate at all (a base full of foreign entries pays
	// nothing).
	holders := ScanHolders()
	plan := make([]ScratchPlanEntry, 0, len(entries))
	if filepath.Clean(base) == filepath.Clean(DefaultBasePath) {
		// The plan face of the explicit suite-run arm (see SweepBase): the
		// automatic walk above refuses that family by construction, so a
		// `--dry-run` that stopped there would report nothing while the real
		// sweep collected the roots. The plan is built by the SAME predicate the
		// sweep uses (BaseSuiteRunPlan), so the promise and the behaviour cannot
		// disagree.
		suitePlan, serr := BaseSuiteRunPlan(base, olderThan)
		if serr != nil {
			return plan, serr
		}
		plan = append(plan, suitePlan...)
	}
	// The plan face of the legacy BARE-KEY build-cache family (see
	// sweepLegacyBareKeyGoCaches): its names are not in the generic namespace
	// above (no family prefix, no dash), so a plan that stopped there would
	// report nothing while the real sweep collected the trees.
	//
	// OUTSIDE the default-base branch, deliberately, because the SWEEP is:
	// SweepBase runs this arm for whatever base it was handed, so scoping the
	// plan to the default base would make `--dry-run` and the real sweep
	// disagree about every non-default base — measured 2026-10-01, a fixture
	// base of 2 instances printed `removed=0` and no plan while the sweep
	// removed one of them. A dry-run that under-reports is the "a zero it could
	// not explain" defect this arm exists to close.
	legacyPlan, lerr := BaseResidueLegacyGoCachePlan(base, olderThan)
	if lerr != nil {
		return plan, lerr
	}
	plan = append(plan, legacyPlan...)
	for i, entry := range entries {
		if i >= ScanLimit {
			return plan, fmt.Errorf("memory base %s has more than %d entries — plan stopped early", base, ScanLimit)
		}
		path := filepath.Join(base, entry.Name())
		if !BaseResidueCollectable(path, entry.Name(), now, olderThan, holders, self) {
			continue
		}
		fi, lerr := os.Lstat(path)
		if lerr != nil {
			continue // vanished between the predicate and the measurement
		}
		plan = append(plan, ScratchPlanEntry{Path: path, Age: now.Sub(fi.ModTime())})
	}
	return plan, nil
}

// SweepBase removes this system's own ad-hoc scratch directories from the
// memory base it is given and returns how many it removed.
//
// SAFETY, in the order it is enforced (every veto is a KEEP):
//
//  0. an empty base argument means the RESOLVED base (ResolveBase — ONE
//     reader), and a base that cannot be READ at all (missing) is the expected
//     non-event: 0, no error, and nothing created;
//  1. the base must not share the filesystem of `/` (measured device ids,
//     Rule 22) — a "memory base" on the boot partition is not this sweep's
//     target, and a measurement that fails is never a pass. Measured only for
//     a base that exists, so a host without a memory base is not an alarm;
//  2. only a direct child whose NAME is in this system's own namespace is
//     looked at (IsBaseResidueName) — foreign names are not candidates at all,
//     and an in-namespace entry the sweep refuses IS reported (--explain), so
//     the two facts never look alike;
//  3. only a DIRECTORY or a REGULAR FILE, judged by Lstat (a symlink is never
//     followed and never a candidate — see isBaseResidueContainer);
//  4. only an entry older than olderThan;
//  5. only an entry no live process holds (one batched /proc pass + the
//     .holder registration) — the hard rule;
//  6. the removal is the SAME removal the home sweep uses (removeCollectable),
//     so the permission-blocked-subtree repair and its "unmeasurable ⇒ keep"
//     contract apply unchanged.
//
// Refusals are REPORTED, never silent (Rule 15): a base that exists but cannot
// be measured, or that shares the filesystem of `/`, returns an error the
// caller must surface.
//
// It is a no-op when the base does not exist, and a no-op when base resolves to
// the scratch HOME: the home is a tree this system OWNS and the home sweep
// already collects (Collectable), so a base arm would either double-collect or
// — worse — RemoveAll an entry the home sweep deliberately protects.
func SweepBase(base string, olderThan time.Duration) (int, error) {
	if base == "" {
		// An EMPTY base argument means "the resolved base" (ResolveBase), the
		// same resolution the CLI and the resident tick use — ONE reader. The
		// argument is kept so a fixture (and the auto-arm seam) can sweep a
		// directory of its own; what no caller may do is pass a base it did not
		// resolve AND did not mean.
		base = ResolveBase()
	}
	if base == DefaultBasePath && !autoBaseResidueEnabled {
		// The AUTOMATIC arm (the default base nobody configured explicitly) is
		// stood down inside a suite — see autoBaseResidueEnabled. A caller that
		// configured ResolveBaseEnvName gets exactly what it asked for.
		return 0, nil
	}
	if samePathAsScratchHome(base) {
		return 0, nil // owned by the home sweep (gc.go): one collector per tree
	}
	explicitSuiteRunRemoved := 0
	if filepath.Clean(base) == filepath.Clean(DefaultBasePath) {
		// The DEFAULT memory base carries a family the automatic arm REFUSES to
		// touch (see IsBaseResidueName's stand-down): the per-run suite roots
		// the placement creates there. An explicit sweep — `gopi tool scratch-gc
		// base`, which is exactly this call — is HOW they are reclaimed, so the
		// two halves are ordered here rather than left to the caller. This is
		// the ONE call site of the suite-run arm's deleting face; it runs BEFORE
		// the namespace walk below (the families are disjoint), and its count is
		// part of this sweep's total.
		//
		// WHY THIS IS AN EXPLICIT ARM AND NOT A RESIDENT ONE (the 2026-10-01
		// correction): the family lives in the SHARED base and a RESIDENT pass
		// has no way to tell another host-user's live suite from an abandoned
		// one, so SweepBase — which the scratch tick called — is reachable
		// from a tick process. That made the tick delete the roots of the
		// suites running on this host (16 aged ones per hour here), under a
		// comment claiming the opposite, and the deletion is what hid the leak
		// behind it. The tick now calls BaseSuiteRunReport instead (nothing
		// removed); SweepBase keeps the fold-in for callers that may delete: the
		// operator's one-shot verb and the placement's reclaim-before-decline.
		explicitSuiteRunRemoved = sweepDefaultBaseSuiteRuns(olderThan)
	}
	// The LEGACY BARE-KEY build-cache family — the trees a REFUSED gate
	// placement used to leave as `<base>/<16-hex>/gocache` (see
	// sweepLegacyBareKeyGoCaches). This arm is NOT restricted to the default
	// base: the family is a plain function of whatever base a caller swept, and
	// a fixture needs to exercise the arm without touching the host's real base
	// (Rule 6). Its names are not in the generic namespace below (no family
	// prefix, no dash), so without this call the family would be swept by
	// nothing — which is exactly the defect it closes.
	explicitSuiteRunRemoved += sweepLegacyBareKeyGoCaches(base, olderThan)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // nothing to do is not an error (the base is not created here)
		}
		return 0, fmt.Errorf("read memory base %s: %w", base, err)
	}
	// The DEVICE measurement happens only for a base that EXISTS: a missing
	// base is the expected non-event on a host without a memory filesystem, and
	// reporting a statfs failure for a directory nobody created would turn that
	// non-event into an alarm the watchdog cannot act on. A base that exists but
	// cannot be measured is still a refusal (never a guess).
	sharesRoot, err := SharesRootFS(base)
	if err != nil {
		return 0, fmt.Errorf("scratch: cannot measure the filesystem of memory base %s — refusing to sweep it: %w", base, err)
	}
	if sharesRoot {
		return 0, fmt.Errorf("scratch: memory base %s is on the same filesystem as %s — refusing to sweep it", base, rootDeviceProbePath)
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	var holders HolderScan
	holdersLoaded := false
	removed := explicitSuiteRunRemoved
	for i, entry := range entries {
		if i >= ScanLimit {
			return removed, fmt.Errorf("memory base %s has more than %d entries — sweep stopped early", base, ScanLimit)
		}
		name := entry.Name()
		path := filepath.Join(base, name)
		if !IsBaseResiduePath(path, name) {
			continue // foreign name, or a kind this sweep never touches: not measured
		}
		if !holdersLoaded {
			holders = ScanHolders()
			holdersLoaded = true
		}
		if !BaseResidueCollectable(path, name, now, olderThan, holders, self) {
			continue
		}
		if err := removeCollectable(path); err != nil {
			// Report and keep sweeping (Rule 15b): one unremovable entry must
			// not stop the rest, and the failure is never swallowed.
			fmt.Fprintf(os.Stderr, "scratch: base sweep could not remove %s: %v\n", path, err)
			continue
		}
		removed++
	}
	return removed, nil
}

// samePathAsScratchHome reports whether base is the scratch home this process
// resolves to (compared both literally and resolved, so a base spelled through a
// symlink is still recognised as the home).
func samePathAsScratchHome(base string) bool {
	home := HomeDir()
	if filepath.Clean(base) == filepath.Clean(home) {
		return true
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return false
	}
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return false
	}
	return resolvedBase == resolvedHome
}
