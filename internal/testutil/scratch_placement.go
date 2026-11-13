package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/worktreeroots"
)

// Suite TEMP-ROOT placement: where this suite's throwaway trees are allowed to
// live, and what the run says when the host offers no cheap place.
//
// ROOT CAUSE THIS FILE EXISTS FOR (2026-09-26, measured): the suite root is an
// operator-chosen scratch home, and on this host that home is
// /home/nick/workspace/gopi-test-scratch — on the BOOT BLOCK DEVICE
// (measured: stat -c %d = 2052, ext4 /dev/sda4; `/` is 2051). The pin's own
// startup line says so ("the ledger sandbox … is on block-backed filesystem
// type 0xef53 (device-backed): every fresh ledger's first open pays that
// device's journal commits"). A fixture that builds a FRESH SQLite ledger
// therefore pays that device's journal commits, and EVERY writer on the device
// — two live `gopi worker` daemons, a `gopi merge --every 5m`, a sibling
// suite's first opens — shares the ONE journal (ext4 keeps one per
// filesystem).
//
// A tmpfs pays nothing for the same build, because none of its data is ever
// written back. That is not a tuning difference, it is the difference between
// 16-24 ms and 431-2101 ms for the IDENTICAL workload, measured here
// contemporaneously (see tasks/memory-sync-every-freshdb-stall-report.md):
//
//	fresh-DB `gopi memory_store` under the scratch home (device 2052): 468-1695 ms, mean 861 ms (n=20)
//	the same command, tmpfs                                        :  16-24 ms
//	one 4 KiB write + fsync, same device (5 sequential)            : 110 ms/fsync, mean; 5 ms on tmpfs
//
// A suite's throwaway tree owes that durability nothing: the scratch home is
// already swept and rebuilt by design (its GC, the /dev/shm exhaustion it
// guards). So the suite takes the CHEAP placement when the host offers one,
// and REPORTS the cost when it cannot:
//
//   - a caller who pinned GOPI_TEST_TMPDIR (or TMPDIR) keeps their root — the
//     explicit layer beats the ambient default, exactly as Rule 22 requires;
//   - otherwise the scratch home itself is taken when it is a memory
//     filesystem (the historical configuration: a link onto /dev/shm);
//   - otherwise the host's memory base is used when it exists and is a memory
//     filesystem (the standard /dev/shm);
//   - otherwise the suite is a DEVICE-BACKED run, and that is REPORTED — it is
//     a legitimate configuration (Rule 22 puts "off `/`" first: device 2052 is
//     off `/` and therefore legal), it just is not a quiet one.
//
// It never falls back to /tmp (Rule 22: a temp root that cannot be provided
// fails loudly, naming the path, the reason and the channel).
//
// Rule 27 RESPECT: this placement is an I/O-COST decision taken by measuring
// the host's filesystem types. Nothing here generates load.

// The placement names. They are VALUES with meaning (decided among, printed,
// and asserted by the fixtures), so each has exactly one definition here
// (Rule 1b/13) rather than a literal at each site.
const (
	// MinMemoryPlacementFreeBytes / MinSuiteRootFreeBytes are the byte floors
	// the placement and the guard measure against — TRUE ALIASES of the ONE
	// definitions in internal/scratch (Rule 14: the leaf package both the suite
	// and a production caller can import), so the number a suite refuses on and
	// the number a production caller reports cannot drift.
	MinMemoryPlacementFreeBytes = scratch.MinMemoryPlacementFreeBytes
	MinSuiteRootFreeBytes       = scratch.MinSuiteRootFreeBytes

	// placementMemoryRoot marks a root the caller pinned on a memory-backed
	// path (GOPI_TEST_TMPDIR / TMPDIR already point at one).
	placementMemoryRoot = "memory-root"
	// placementMemoryHome marks a root taken from the scratch home because the
	// home itself is memory-backed.
	placementMemoryHome = "memory-home"
	// placementMemoryBase marks a root taken from the host's memory base.
	placementMemoryBase = "memory-base"
	// placementDeviceRoot is the HONEST degradation: no memory-backed location
	// was available, so the suite runs on whatever the caller's root is — a
	// device, whose journal every fresh ledger's first open pays.
	placementDeviceRoot = "device-root"
)

// scratchPlacement is the decision taken by the one function that resolves it
// (scratchPlacementFor), carried for reporting and for a fixture's assertion.
type scratchPlacement struct {
	memory bool
	// dir is the memory-backed directory the run's root hangs under, or "" for
	// the device arm (whose root is the scratch home itself).
	dir   string
	label string
	// freeBytes is the free space MEASURED on the chosen memory-backed
	// directory at decision time (0 for the device arm, whose root the caller
	// owns). It is printed with the decision, because "the cheap root was taken"
	// is only auditable together with how much room it had (Rule 15).
	freeBytes uint64
	// declines names every memory-backed candidate that was REFUSED for lack of
	// room, with its measured bytes. Without it a saturated host looks like a
	// host that simply has no memory filesystem — the two need different fixes.
	declines []string
}

// scratchPlacementDecision records what the running process actually pinned
// (set by PinSuiteTempRoot), so a caller can assert the placement without
// re-deriving it.
var scratchPlacementDecision scratchPlacement

// ScratchPlacementMemoryBacked reports whether THIS run's suite root is
// memory-backed, as decided by the one pin — the answer a fixture needs that
// wants to say something true about where its throwaway files live.
func ScratchPlacementMemoryBacked() bool { return scratchPlacementDecision.memory }

// ScratchPlacementLabel names the decision ("memory-base", "device-root", …).
// Empty before the pin runs.
func ScratchPlacementLabel() string { return scratchPlacementDecision.label }

// ScratchPlacementDir is the memory-backed directory the run's root hangs
// under, or "" on the device arm.
func ScratchPlacementDir() string { return scratchPlacementDecision.dir }

// scratchPlacementFor resolves the placement, in the one place that reads the
// channels (Rule 2). Two channels can name a location, and they are not equal:
//
//	the suite's OWN override (GOPI_TEST_TMPDIR) > the caller's TMPDIR > the
//	scratch home > the host's memory base > the ambient root.
//
// GOPI_TEST_TMPDIR is a caller who named the scratch HOME ITSELF, so the
// location is theirs and only the filesystem is measured (memory → the memory
// arm, a device → the reported device arm).
//
// A caller-exported TMPDIR is NOT that: the suite pin overrides TMPDIR with the
// run's own root before any test runs (children inherit the pinned root), so an
// ambient TMPDIR never decided where this suite's files go. It is honoured for
// the one thing it can honestly mean here — a caller who already pointed TMPDIR
// at a memory filesystem gets that root — and otherwise the placement looks for
// a cheap root of its own.
func scratchPlacementFor(suiteHomeOverride, callerTmpDir string) scratchPlacement {
	if suiteHomeOverride != "" {
		// The caller named the scratch HOME itself: the location is THEIRS and
		// only its filesystem is measured (the memory arm when they pointed at
		// one, the reported device arm otherwise). Its free space is not this
		// function's question — a caller's own root is guarded by the bounded
		// preflight (assertScratchRootHasRoom), which names the cause instead of
		// silently relocating a root the caller chose.
		if mem, _ := ScratchRootFilesystem(suiteHomeOverride); mem {
			// The free-space reading is BEST-EFFORT BY CONTRACT, so there is no error
			// to check here: this arm is chosen on the FILESYSTEM KIND (the question
			// above), and freeBytes is a REPORTED NUMBER for the caller — never a
			// decision input on this path. The caller who names the scratch home owns
			// the location; the room check for it is assertScratchRootHasRoom, which
			// runs on the pinned root and reports its own measurement. A path whose
			// room cannot be measured yields a zero here rather than a silent
			// relocation of a root the caller chose (see scratchPlacement.freeBytes).
			free, _ := scratch.FreeBytes(suiteHomeOverride) //stylegate:ignore best-effort reading reported to the caller: this arm is chosen on the FILESYSTEM KIND, and the room check for a caller's own root is assertScratchRootHasRoom on the pinned root
			return scratchPlacement{memory: true, dir: suiteHomeOverride, label: placementMemoryRoot, freeBytes: free}
		}
		return scratchPlacement{label: placementDeviceRoot}
	}
	var declines []string
	if callerTmpDir != "" {
		if mem, _ := ScratchRootFilesystem(callerTmpDir); mem {
			if p, ok := memoryPlacementCandidate(callerTmpDir, placementMemoryRoot); ok {
				p.declines = declines
				return p
			}
			declines = append(declines, saturationNote(callerTmpDir))
		}
	}
	home := ScratchHomeDir()
	if mem, _ := ScratchRootFilesystem(home); mem {
		if p, ok := memoryPlacementCandidate(home, placementMemoryHome); ok {
			p.declines = declines
			return p
		}
		declines = append(declines, saturationNote(home))
	}
	base := MemoryBase()
	if base != "" {
		if mem, _ := ScratchRootFilesystem(base); mem {
			if p, ok := memoryPlacementCandidate(base, placementMemoryBase); ok {
				p.declines = declines
				return p
			}
			declines = append(declines, saturationNote(base))
		}
	}
	return scratchPlacement{label: placementDeviceRoot, declines: declines}
}

// memoryFreeBytes is the ONE place the placement measures free space. It is a
// variable rather than a direct call so a fixture can drive the saturation
// branch without building a full filesystem — filling a real memory base to
// reproduce the condition would consume the host's RAM and, on a shared tmpfs,
// break every other process on the machine (Rule 27: a test never generates
// load, and a "fixture" that saturates a device is a load generator with a
// different name). Production never replaces it.
var memoryFreeBytes = scratch.FreeBytes

// reclaimSaturatedCandidate is the RECLAIM-BEFORE-DECLINE step: a memory-backed
// candidate that has no room is first given the chance to get its room back, by
// collecting the per-run suite roots THIS system left there.
//
// WHY IT IS PART OF THE PLACEMENT AND NOT A SEPARATE OPERATOR TASK (measured on
// this host, 2026-09-28): /dev/shm held 146 unreclaimed `suite-<pkg>-run-<N>`
// roots — 5.9G of a 7.8G tmpfs — and the placement, having nothing to collect
// them, simply moved every later run onto the block device, where a fresh
// ledger's first open pays that device's journal commits (the cost the
// placement exists to avoid). Declining alone would make saturation permanent:
// nothing ever frees the base, so the cheap root is never used again. The
// reclaim is the SAME sweep the explicit CLI verb runs (one predicate, one
// namespace — scratch.SweepBaseSuiteRuns), bounded by age and by the live-holder
// rule, so a running suite on this host is never its victim.
//
// It runs for the DEFAULT base only: a base the caller CONFIGURED by knob is
// the caller's own private tree, and a machine that sweeps a path the operator
// pointed at deliberately is the kind of surprise this project bans.
func reclaimSaturatedCandidate(dir string) {
	if dir == "" || filepath.Clean(dir) != filepath.Clean(scratch.ResolveBase()) {
		return
	}
	memoryReclaim(dir)
}

// memoryReclaim is the SEAM through which the reclaim step is invoked: the
// production value is sweepSaturatedMemoryBase below, and a fixture replaces it
// so the no-room branch can be driven end to end without filling a real tmpfs
// (Rule 27 — a test that saturated /dev/shm to observe this would be a load
// generator with a different name; the same reason memoryFreeBytes is a var).
var memoryReclaim = sweepSaturatedMemoryBase

// sweepSaturatedMemoryBase is the ONE reclaim-before-decline step: measure what
// the EXISTING sweep faces would collect from a memory base with no room, then
// run their deleting faces.
//
// WHY IT IS A VARIABLE (Rule 31's lesson, and Rule 27's): the production value
// is the real deleter below, and a fixture REPLACES it so the bounded-preflight
// branch can be driven end to end without filling a real tmpfs — a test that
// saturated /dev/shm to observe this would be a load generator with a different
// name.
//
// WHY IT IS NOT ONLY SweepBaseSuiteRuns (measured on this host, 2026-10-12):
// the decline branch used to call that ONE arm, so a saturated base whose
// suite-run family was FRESH (every root inside the 6h window — which is the
// steady state of a host that runs `go test` all day) reclaimed NOTHING, even
// though the same base held 2.4 GiB of residue two sibling arms would have
// collected: `SweepBase`'s own namespace walk (gopi-*/gocache-*/probe-*) and the
// legacy home/base sweep the suite pin already runs on every pin. The measured
// consequence is the one this file's header describes: run-1791787081 printed
// `reclaimed 0 stale suite root(s) (kept 1)` at 935.4 MiB free and sent
// standards/tools/cmd-agent to the block device. A reclaim step that leaves the
// base saturated is a reclaim step that never fires again — so it asks every
// face whose evidence says "this is ours and it is stale", and leaves the
// judgement to them (age + live-holder, unchanged).
func sweepSaturatedMemoryBase(dir string) {
	olderThan := scratch.ResolveStaleAge()
	// Faces 1+2: the base's own namespace walk AND the suite-run fold-in
	// (never reachable automatically: this call site is not a suite's pin, and
	// SetAutoBaseResidue's stand-down is scoped to the PIN — see below).
	baseRemoved, baseErr := scratch.SweepBase(dir, olderThan)
	if baseErr != nil {
		// Rule 15b: a refusal on one face is reported and does not cancel the
		// sibling face (the families are disjoint).
		fmt.Fprintf(os.Stderr, "testutil: could not reclaim the saturated memory base %s: %v\n", dir, baseErr)
	}
	// Face 3: the legacy suite-scratch namespace (`gopi-suite-tmp-*`) that
	// internal/testutil itself owns and already sweeps on every pin.
	legacyRemoved, legacyErr := GCLegacyScratchResidueOlderThan(olderThan)
	if legacyErr != nil {
		fmt.Fprintf(os.Stderr, "testutil: could not reclaim legacy scratch residue from %s: %v\n", dir, legacyErr)
	}
	if total := baseRemoved + legacyRemoved; total > 0 {
		fmt.Fprintf(os.Stderr, "testutil: memory base %s had no room: reclaimed %d stale entr%s "+
			"(base namespace %d, legacy suite scratch %d)\n", dir, total, pluralY(total), baseRemoved, legacyRemoved)
	}
}

// memoryPlacementCandidate is the SATURATION CHECK: it takes a memory-backed
// candidate only when it still has at least
// scratch.MinMemoryPlacementFreeBytes free.
//
// WHY THE CHECK IS PART OF THE DECISION AND NOT A LATER ERROR (measured on this
// host 2026-09-28): the placement preferred the host memory base
// (placementMemoryBase) on filesystem TYPE alone, so a /dev/shm at 98% (160-192
// MiB free of 7.8 GiB, with 146 unreclaimed suite roots) was still chosen. A
// run placed there does not report "no room": its first small directory
// succeeds and a later write fails, so the run reports `repo "/dev/shm/suite-
// cmd-agent-run-<N>/…" is not a git work tree` — a message about a path the
// suite itself created, which reads as a code regression. Declining the
// candidate here is what turns that into a device-backed run that SAYS why.
//
// An unmeasurable candidate is never taken: this function answers "is there
// room", and a number it could not obtain is not a yes (Rule 15).
func memoryPlacementCandidate(dir, label string) (scratchPlacement, bool) {
	free, err := memoryFreeBytes(dir)
	if err != nil {
		return scratchPlacement{}, false
	}
	if free < scratch.MinMemoryPlacementFreeBytes {
		// No room: try to get it back before writing the candidate off, then
		// re-measure. The second reading is what the decision is taken on, so
		// the reported free bytes are always the ones in force at the moment of
		// the choice (Rule 15: never report a stale number).
		reclaimSaturatedCandidate(dir)
		free, err = memoryFreeBytes(dir)
		if err != nil || free < scratch.MinMemoryPlacementFreeBytes {
			return scratchPlacement{}, false
		}
	}
	return scratchPlacement{memory: true, dir: dir, label: label, freeBytes: free}, true
}

// saturationNote words WHY a memory-backed candidate was refused, with the
// measured numbers. It is the half that keeps a saturated host distinguishable
// from a host that has no memory filesystem at all.
func saturationNote(dir string) string {
	free, err := memoryFreeBytes(dir)
	if err != nil {
		return fmt.Sprintf("%s (free space unmeasurable: %v)", dir, err)
	}
	return fmt.Sprintf("%s (%s free, below the %s floor)", dir, scratch.FormatBytes(free), scratch.FormatBytes(scratch.MinMemoryPlacementFreeBytes))
}

// ensureMemoryBase makes the standard memory base usable when no caller pinned
// a root, so a host whose /dev/shm has been unmounted (or whose base was never
// created) still gets the cheap placement rather than a silent device
// fallback. It is called from the one-time suite-env preparation.
//
// It is deliberately scoped to the base's OWN directory: it never touches a
// path the caller named, and it never touches the operator's host
// configuration (Rule 22's out-of-scope list). A failure to create it is
// REPORTED and the run degrades to the device arm — announced, never passed off
// silently as isolated scratch.
func ensureMemoryBase() {
	if SuiteTempRootOverride() != "" || config.TmpDirFromCallerEnv() != "" {
		return
	}
	base := MemoryBase()
	// Only the standard memory base is created by this process (it is OUR
	// scratch namespace's home); a base the caller redirected elsewhere is
	// theirs to provide.
	if base == "" || base != worktreeroots.ShmDir {
		return
	}
	if mem, _ := ScratchRootFilesystem(base); mem {
		return
	}
	if err := os.MkdirAll(base, dirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: memory base %s could not be created (%v) — the suite scratch root "+
			"falls back to the scratch home (%s), where every fresh ledger's first open pays the device's "+
			"journal commits\n", base, err, ScratchHomeDir())
		return
	}
	if mem, description := ScratchRootFilesystem(base); !mem {
		fmt.Fprintf(os.Stderr, "testutil: memory base %s is %s — not memory-backed, so the suite scratch root "+
			"stays on the device and every fresh ledger's first open pays its journal commits (point %s at a "+
			"tmpfs, or accept the cost)\n", base, description, ScratchHomeEnvName)
	}
}

// reportScratchPlacement states the placement ONCE per run, on stderr, for BOTH
// arms. The device arm is the loud one — silence about a device-backed root is
// what let the cost hide — and the memory arm says which channel decided, so a
// run's log carries the answer either way (an unstated decision is an unaudited
// one).
func reportScratchPlacement(suite, root string, p scratchPlacement) {
	_, description := ScratchRootFilesystem(root)
	if p.memory {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: scratch root %s is %s (placement %s, %s free) — fresh ledgers under "+
			"it are built on a memory filesystem, so their first opens pay no block device's journal commits\n",
			suite, root, description, p.label, freeSpaceNote(root))
		if len(p.declines) > 0 {
			fmt.Fprintf(os.Stderr, "testutil: suite %q: memory-backed candidates with too little room were declined: %s\n",
				suite, strings.Join(p.declines, ", "))
		}
		return
	}
	// The device arm leads with WHY — a saturated memory base and a host that
	// has none are different hosts with different fixes, and the measured bytes
	// are the only way to tell them apart (Rule 15).
	reason := "no memory filesystem was available"
	if len(p.declines) > 0 {
		reason = "the memory-backed candidate(s) " + strings.Join(p.declines, ", ") +
			" were declined for lack of room (floor " + scratch.FormatBytes(scratch.MinMemoryPlacementFreeBytes) + ")"
	}
	fmt.Fprintf(os.Stderr, "testutil: suite %q: scratch root %s is %s (placement %s) — a fresh ledger built under it pays "+
		"this device's journal commits, shared with every other writer on it, so a loaded host costs this "+
		"suite more wall clock than its budget assumes. %s. Point %s at a tmpfs with room to remove that cost; "+
		"this is reported, not refused (the root is off /, so Rule 22 holds).\n",
		suite, root, description, p.label, reason, ScratchHomeEnvName)
}

// freeSpaceNote renders the measured free space of the chosen memory-backed
// directory for the placement report (Rule 15: a decision about space is
// reported with the space it was taken on). An unmeasurable directory is
// reported as such, never as a number.
func freeSpaceNote(dir string) string {
	free, err := scratch.FreeBytes(dir)
	if err != nil {
		return "free space unmeasurable: " + err.Error()
	}
	return scratch.FormatBytes(free)
}

// assertScratchRootHasRoom is the BOUNDED PREFLIGHT behind the loud guard: the
// suite's own scratch root must have room BEFORE the run starts, and the
// complaint must name the path, the filesystem and the free bytes.
//
// WHY IT EXISTS (measured on this host, 2026-09-28): with the memory base at
// 98%, the run's first directory creation SUCCEEDED and its first larger write
// failed, so the suite reported `repo "/dev/shm/suite-cmd-agent-run-<N>/gopi-
// repo-ctx-<M>" is not a git work tree (git rev-parse failed: fatal: not a git
// repository)` — for a directory the suite had itself just created. That reads
// as a code regression across 7-8 cmd/agent E2E tests and cost a diagnosis
// session; the real cause was 160 MiB of free space. The guard converts it into
// one named refusal with the numbers (Rule 15).
func assertScratchRootHasRoom(root string) error {
	return scratch.AssertFreeSpace(root, scratch.MinSuiteRootFreeBytes)
}

// scratchRootCreationRefusal is the ONE builder of the message (and the exit
// status) a suite reports when its per-run root could not be created: the
// suite, the directory, the cause, and the MEASURED state of the filesystem
// (path, kind, free bytes, both floors).
//
// It is split out of the pin so the refusal can be asserted verbatim — the
// whole point of the guard is the TEXT (the failure it replaces was a
// misleading one), and a message that can only be produced by exiting the
// process is a message no test can read.
//
// The exit status separates a SPACE refusal from everything else, because the
// two need different fixes and a wrapper may only ever see the status: 98 when
// the root cannot be SHOWN to have room (below the floor, or unmeasurable — a
// root that cannot be measured cannot be proven to have room, so the honest
// verdict is the shortage, not a location bug the operator would hunt for in
// vain), 97 otherwise.
func scratchRootCreationRefusal(suite, home string, cause error) (string, int) {
	report := scratchRootSpaceReport(home)
	code := SuiteScratchUnsafeExitCode
	if err := assertScratchRootHasRoom(home); err != nil {
		code = SuiteScratchNoSpaceExitCode
	}
	return fmt.Sprintf("\nFATAL: suite %q: the scratch root could not be created under %s: %v\n%s\n"+
		"FIX: give that filesystem room (a memory base: `gopi tool scratch-gc base`; elsewhere, free space), or "+
		"point %s at a root with room. A suite that continued here would report the shortage as an "+
		"unrelated-looking error about a path it had just created.\n\n",
		suite, home, cause, report, ScratchHomeEnvName), code
}

// scratchRootSpaceReport is the measured half of a hard refusal about a scratch
// root: the path, the filesystem it is on and the free bytes, plus the two
// floors this tree holds (Rule 15). It is printed with every fatal root
// failure, so the reader gets a cause rather than a symptom — the measured
// shape this exists for was an ENOSPC surfacing as "is not a git work tree"
// about a directory the suite had just created.
func scratchRootSpaceReport(dir string) string {
	free, err := scratch.FreeBytes(dir)
	if err != nil {
		return fmt.Sprintf("scratch root %s: free space could NOT be measured (%v) — the filesystem there "+
			"cannot be judged, which is itself the finding", dir, err)
	}
	_, description := ScratchRootFilesystem(dir)
	return fmt.Sprintf("scratch root %s: %s free on %s (floors: %s to place a run on a memory filesystem, %s "+
		"required inside the chosen root)", dir, scratch.FormatBytes(free), description,
		scratch.FormatBytes(MinMemoryPlacementFreeBytes), scratch.FormatBytes(MinSuiteRootFreeBytes))
}
