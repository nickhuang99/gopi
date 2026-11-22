package main

// cli_scratch_gc_gocache.go — the `gopi tool scratch-gc gocache` arm: the
// AGENT-side Go toolchain cache (~/.cache/go-build) MEASURED, JUDGED and — only
// when asked — brought back under its bound.
//
// WHY THIS ARM EXISTS (measured on nick-HP-Laptop, 2026-10-12):
//
//	/dev/nvme0n1p7 (/home, device 66309)  458G, 416G used, 19G free — 96% FULL
//	/dev/nvme0n1p5 (/,     device 66307)  183G,  43G used, 131G free
//	~/.cache/go-build                     121G, 368605 files, device 66309
//
// The partition that filled is /home, NOT `/`. Rule 22's off-`/` criterion is
// answerable for `/` and answered NO (the cache is not on the boot partition and
// not on RAM), while the filesystem actually carrying 121G of cache had no
// headroom question asked about it at all. That is the hole this arm closes: the
// criterion is measured on the filesystem that HOLDS the cache.
//
// WHAT IT IS NOT: it is not a second policy and not a new schedule. The
// measurement, the verdict and the eviction live in internal/scratch
// (toolchain_cache.go), which is the leaf that already owns the cache-shape
// predicates (BuildCacheShardHexLen, buildCacheShardName, the toolchain's own
// bookkeeping leaf names); this file only drives them. It adds no daemon, no cron
// and no timer (Rule 35) — it rides the verb the host watchdog already executes.
//
// IT DELETES NOTHING UNLESS ASKED, and that is Rule 41 rather than caution: the
// toolchain cache is the operator's SHARED, cross-project asset (every repo on the
// host compiles into it), the finder of an exhaustion proposes the reclaim, and
// the operator decides when it happens. So the bare form is a REPORT; `--trim` is
// the explicit reclaim; `--dry-run` is the report spelled out. And when the
// operator has disabled the sweeps (the cadence knob), even `--trim` is withheld
// and says so — a disabled sweep must not delete anything on the strength of a
// flag.

import (
	"fmt"
	"os"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// The arm's spellings, each named ONCE (Rule 1b): the parser, the usage text and
// the tests reference these constants instead of re-typing a literal that could
// drift.
const (
	// scratchGCGocacheFlag is the arm selector: `gopi tool scratch-gc gocache`.
	scratchGCGocacheFlag = "gocache"

	// scratchGCGocacheRole labels the arm's logfmt line, so an operator reading a
	// watchdog log tells the arms apart by the `role=` field alone (the same
	// discipline as cli-trees / cli-sandboxes).
	scratchGCGocacheRole = "cli-gocache"

	// scratchGCGocacheTrimFlag is the EXPLICIT reclaim. Without it the arm is
	// read-only, which is the whole of Rule 41's "the finder proposes" here: the
	// operator's shared cache is never brought under the bound by a report.
	scratchGCGocacheTrimFlag = "--trim"

	// scratchGCGocachePathFlag measures a cache OTHER than the resolved default
	// (a second toolchain's cache, a fixture — and the flag that makes this arm
	// live-testable against a controlled directory instead of the operator's own).
	scratchGCGocachePathFlag = "--path"

	// scratchGCGocacheMaxGiBFlag is THIS INVOCATION's bound, in GiB, and it
	// outranks the environment knob (the ranking lives in the ONE reader,
	// scratch.ResolveToolchainCacheMaxBytes). It exists because a bound that can
	// only be exercised by filling a real cache to 25 GiB is a bound nobody ever
	// tests, and because an operator reclaiming a one-off cache wants a one-off
	// target (Rule 5).
	scratchGCGocacheMaxGiBFlag = "--max-gib"

	// scratchGCGocacheHeadroomGiBFlag is this invocation's FREE-SPACE FLOOR, in
	// GiB — the criterion's other arm, overridable for the same reason and by the
	// same measurement (the filesystem that holds the cache).
	scratchGCGocacheHeadroomGiBFlag = "--headroom-gib"
)

// The arm's OPERATOR-DECISION line: the one sentence that turns an over-bound
// reading into a decision the reader can take or refuse, and that points at where
// the reason for the standing design is written down.
//
// WHY IT EXISTS (measured on nick-HP-Laptop, 2026-10-17): the cache sat at 3.1x
// its own 25 GiB bound (78.3 GiB body, 49.9 GiB reclaimable at the entry grain,
// 184489 entries past the grace window) ACROSS a /home exhaustion that killed a
// `cmd/agent` suite with ENOSPC. Nothing was broken — the bound existed, the grain
// was right, the reclaim command was printed — yet a reader of the arm's output
// could still take the overshoot for a defect the arm was failing to fix, because
// the arm reported a number and never said whose move it was. The report-only
// behaviour is a DESIGN DECISION (Rule 41: the finder of a defect proposes the
// reclaim, the operator decides when it happens), so it must be legible from the
// report itself and not only from the source header.
//
// It is a sentence rather than a field because it is an explanation, and it goes to
// stderr so the logfmt line stays machine-parseable. It names the DECISION, the
// EXACT command, and the two places the reason lives (Rule 35: a standing design
// decision that a future reader can neither identify nor safely revisit is worse
// than the problem it solved) — so the next reader does not have to re-derive it.
const scratchGCGocacheDecisionFmt = "gopi %s %s: the cache is OVER its bound and bringing it back is the OPERATOR's decision — this arm never reclaims on its own, by design: the toolchain cache is a shared cross-project asset (every repo on this host compiles into it) and the eviction is deliberately never automatic. Run `gopi %s %s %s` when you choose to reclaim it, or lower %s deliberately; the reason the arm stays report-only is recorded in %s and %s"

// The two places the decision's REASON is recorded, each named ONCE (Rule 1b): the
// sentence above is read by an operator who is deciding whether to delete 49.9 GiB
// of shared cache, so "where do I read why?" must be answerable without a grep.
const (
	// scratchGCGocacheDecisionReasonSource is the leaf whose own header states the
	// decision ("IT NEVER DELETES ANYTHING BY ITSELF … a design decision rather than
	// a missing feature"), so the reason survives even if the devdoc is not read.
	scratchGCGocacheDecisionReasonSource = "internal/scratch/toolchain_cache.go"

	// scratchGCGocacheDecisionReasonDevdoc is the decision BRIEF: the measured
	// overshoot, why the bound did not act, the two arms and the operator's exact
	// commands — the document this sentence owes its reader (Rule 7).
	scratchGCGocacheDecisionReasonDevdoc = "devdoc/toolchain-cache-bound-decision.md"
)

// scratchGCGocacheLogFmt is the arm's line — the ONE line a reader diffs between
// runs, on the same logfmt shape the other arms print.
//
// The fields are the epic's required numbers and no more: WHERE (path, the
// filesystem's device — the DECIMAL number `stat -c %d` prints, so a reader can
// check it — and its type), HOW MUCH ROOM THE HOLDING FILESYSTEM HAS (free,
// total — the measurement Rule 22 could not answer for /home), WHAT THE CACHE
// HOLDS (body = the cache ENTRIES, the number the bound compares; all = the whole
// directory; files/dirs), WHAT THE BOUND IS, WHAT THE GRAIN IS (`grain=` — the
// unit `reclaimable=` is expressed at, so a 0 can never be read as "your cache is
// fine"), WHAT COULD COME BACK TONIGHT (reclaimable = the bytes past the grace
// window at that grain, and reclaimable_entries = how many entries), the OLD
// shard-grain reading beside it (shard_reclaimable / shard_candidates, named so the
// two grains can be compared on one cache — a reader seeing shard_reclaimable=0B
// next to reclaimable=90GiB has the whole diagnosis), and — when the trim ran —
// body_after plus the before/after pair on its own line below.
const scratchGCGocacheLogFmt = "scratch-gc role=%s path=%s device=%d fs=%s free=%s total=%s body=%s all=%s files=%d dirs=%d bound=%s grain=%s reclaimable=%s reclaimable_entries=%d shard_reclaimable=%s shards=%d shard_candidates=%d trim=%t body_after=%s entries_removed=%d"

// scratchGCGocacheHeadroomDeltaFmt is the sentence for the case the headroom
// verdict alone misleads about, and the one the epic's paragraph on /home exists
// for: the filesystem was BELOW its headroom floor and the reclaim ran anyway, so
// the operator must see the MEASURED change in filesystem free space rather than
// the pre-trim prediction. Every placeholder is an observed number: the free space
// before and after, the delta, and the floor.
//
// WHY IT IS PRINTED: the `--trim` face is the operator's own explicit decision, and
// it is deliberately NOT gated on the headroom verdict (the cache is one consumer
// of a shared filesystem, and a reclaim that the toolchain can survive is a
// strictly good thing there). The consequence is that a reader sees "below the
// floor … is NOT touched here" (the REPORT's sentence, correct for a report) and a
// reclaim in the same run; this line states what actually changed.
const scratchGCGocacheHeadroomDeltaFmt = "gopi %s %s: the reclaim freed %s of filesystem space on %s (free %s → %s, floor %s) — the change in the FILESYSTEM's free space, measured, not a prediction; a cache reclaim can free less than the bytes it removes when other writers on the same filesystem fill the space in between"

// scratchGCGocacheGrainNotePrefix is the prefix of the ONE line that explains
// which grain the eviction honours when a report's two grains disagree. It is a
// line of its own (not a field) because it is an explanation and not a reading,
// and it is named here so the tests can assert its presence in the arm's own
// output (the epic's requirement (b): the arm must say which grain it works at,
// in its own words).
const scratchGCGocacheGrainNotePrefix = "gocache-grain "

// scratchGCGocacheCannotReclaimFmt is the arm's sentence for the state that
// produced this work: a cache that is OVER its bound and has nothing past the
// grace window, so a reclaim at the named grain frees ZERO bytes. The pre-fix arm
// printed `reclaimable=0B` and left the operator to read it as "your cache is
// fine"; this sentence says the opposite in the arm's own output (Rule 15: never a
// silent no-op, and never a zero without its meaning). Every placeholder is a
// measured fact: the measured body, the bound, the grain and the grace window.
const scratchGCGocacheCannotReclaimFmt = "gopi %s %s: NOTHING CAN BE RECLAIMED at the %s grain — the cache holds %s, over its %s bound, but no entry is older than the %s grace window, so a reclaim would free 0 bytes; the space is held by WORKING-SET entries (a live still-being-written cache), and neither grain frees them: wait for entries to age past the window, or lower %s deliberately"

// runScratchGCGocache measures the toolchain cache, applies the headroom/bound
// criterion, reports both, and (only when asked AND the sweep is enabled) evicts
// oldest entry shards down to the bound.
//
// Exit status follows the verb's contract: 0 for a measurement that ran —
// INCLUDING a cache that is over its bound or a filesystem that is nearly full,
// because those are FINDINGS a watchdog must not turn into an alarm on the sweeps
// it runs every tick; 1 only when the measurement itself could not be taken (an
// unreadable path, an unmeasurable filesystem), which is the state a reader must
// never mistake for "clean" (Rule 15).
func runScratchGCGocache(path string, trim, sweepsEnabled bool, maxGiB, headroomGiB int64) {
	cachePath, err := scratch.ResolveToolchainCachePath(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbScratchGC, err)
		os.Exit(scratchGCExitError)
	}
	rep, err := scratch.MeasureToolchainCache(cachePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbScratchGC, scratchGCGocacheFlag, err)
		os.Exit(scratchGCExitError)
	}
	var flagBytes int64
	if maxGiB > 0 {
		flagBytes = maxGiB * scratch.ToolchainCacheGiBBytes
	}
	bound := scratch.ResolveToolchainCacheMaxBytes(flagBytes)
	headroom := scratch.DefaultToolchainCacheHeadroomBytes
	if headroomGiB > 0 {
		headroom = uint64(headroomGiB) * uint64(scratch.ToolchainCacheGiBBytes)
	}
	verdict := scratch.CheckToolchainCache(rep, bound, headroom)

	// The verdict is PRINTED, not inferred from the numbers: an operator who has
	// to compare free bytes against a floor in their head is being asked to do the
	// check's job. It goes to stderr so the logfmt line stays machine-parseable.
	if !verdict.Ok() {
		fmt.Fprintf(os.Stderr, "gopi %s %s: %s\n", CliVerbScratchGC, scratchGCGocacheFlag, verdict.Reason)
	}
	// The two-grain explanation, printed whenever the report's SHARD-grain reading
	// counts bytes the entry-grain reclaim does not free. Without it an operator
	// comparing the two fields sees two numbers and no statement of which one the
	// eviction honours (Rule 15: never a silent discrepancy).
	if note := scratch.ToolchainCacheGrainNote(rep); note != "" {
		fmt.Fprintf(os.Stderr, "%s%s\n", scratchGCGocacheGrainNotePrefix, note)
	}
	// CAN THIS ARM RECLAIM AT ALL? The decision is taken from the report itself
	// (reclaimable bytes at the entry grain), never from "did the trim run", so the
	// SENTENCE is printed by a plain report too: the state it describes — a cache
	// over its bound with nothing past the grace window — is the one an operator
	// must not be left to read out of a bare `reclaimable=0`.
	if rep.Total > bound && rep.ReclaimableBytes == 0 {
		fmt.Fprintf(os.Stderr, scratchGCGocacheCannotReclaimFmt+"\n",
			CliVerbScratchGC, scratchGCGocacheFlag, rep.GrainName(),
			scratch.FormatBytes(uint64(rep.Total)), scratch.FormatBytes(uint64(bound)),
			scratch.ToolchainCacheEvictMinAge, scratch.ToolchainCacheMaxEnvName)
	}

	freed := scratch.ToolchainCacheEviction{}
	trimmed := false
	// WHOSE MOVE IS IT? A report that measures an overshoot must say that the
	// reclaim is the OPERATOR's to run — otherwise an over-bound reading is
	// indistinguishable from an arm that failed to act (measured: 78.3 GiB against a
	// 25 GiB bound, across a /home exhaustion). Printed on the REPORT face only: a
	// `--trim` invocation IS the operator taking that decision, and telling them to
	// run the flag they just ran would be noise.
	if !trim && rep.Total > bound {
		fmt.Fprintf(os.Stderr, scratchGCGocacheDecisionFmt+"\n",
			CliVerbScratchGC, scratchGCGocacheFlag,
			CliVerbScratchGC, scratchGCGocacheFlag, scratchGCGocacheTrimFlag,
			scratch.ToolchainCacheMaxEnvName,
			scratchGCGocacheDecisionReasonSource, scratchGCGocacheDecisionReasonDevdoc)
	}
	if trim {
		if !sweepsEnabled {
			// A disabled sweep must not delete on the strength of a flag — said
			// out loud rather than silently skipped (Rule 15: an operator who
			// asked for a reclaim must learn it did not happen).
			fmt.Fprintf(os.Stderr,
				"gopi %s %s: %s withheld — the sweeps are disabled on this host (the cadence knob is 0); nothing was removed\n",
				CliVerbScratchGC, scratchGCGocacheFlag, scratchGCGocacheTrimFlag)
		} else {
			// The eviction is bounded by the SAME criterion the report just
			// printed: the target is the operator's configured bound, never
			// "as small as possible" (a trim that emptied the cache would make
			// every following build cold — the merge gate's own first bound was
			// raised for exactly this defect).
			ev, eerr := scratch.EvictToolchainCache(cachePath, bound, scratch.ToolchainCacheEvictMinAge, time.Now())
			if eerr != nil {
				fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbScratchGC, scratchGCGocacheFlag, eerr)
				os.Exit(scratchGCExitError)
			}
			freed, trimmed = ev, true
			// Per-entry failures and the grace-window skips are REPORTED (Rule
			// 15b): a reclaim that reached less than it promised must say so.
			for _, e := range ev.Errs {
				fmt.Fprintf(os.Stderr, "gopi %s %s: could not remove %s\n", CliVerbScratchGC, scratchGCGocacheFlag, e)
			}
			if len(ev.Skipped) > 0 {
				fmt.Fprintf(os.Stderr,
					"gopi %s %s: %d entry(ies) kept — their newest file is inside the %s grace window\n",
					CliVerbScratchGC, scratchGCGocacheFlag, len(ev.Skipped), scratch.ToolchainCacheEvictMinAge)
			}
			if len(ev.Removed) == 0 && len(ev.Errs) == 0 {
				// A trim that removed nothing must SAY WHY (Rule 15b). Two
				// reasons exist and they are different findings, so they are two
				// sentences: the cache is already within its bound (nothing to do),
				// or it is over the bound with every entry still fresh — the LIVE
				// cache this arm exists for, where the space is working-set and no
				// grain frees it.
				if rep.Total <= bound {
					fmt.Fprintf(os.Stderr,
						"gopi %s %s: nothing removed — the cache body (%s) is already within the %s bound; the headroom verdict judges the whole filesystem, not this cache alone\n",
						CliVerbScratchGC, scratchGCGocacheFlag, scratch.FormatBytes(uint64(rep.Total)), scratch.FormatBytes(uint64(bound)))
				} else {
					fmt.Fprintf(os.Stderr, scratchGCGocacheCannotReclaimFmt+"\n",
						CliVerbScratchGC, scratchGCGocacheFlag, rep.GrainName(),
						scratch.FormatBytes(uint64(rep.Total)), scratch.FormatBytes(uint64(bound)),
						scratch.ToolchainCacheEvictMinAge, scratch.ToolchainCacheMaxEnvName)
				}
			}
		}
	}

	bodyAfter := rep
	freeAfter := rep.FSFree
	if trimmed {
		// RE-MEASURED, never computed: bytes on disk are the kernel's answer, and a
		// before/after pair derived by subtraction is a claim about arithmetic
		// rather than an observation of the tree (the epic asks for the numbers
		// verbatim). The FILESYSTEM's free space is re-measured the same way, on the
		// same probe the report used — the number the delta sentence below is about.
		if after, aerr := scratch.MeasureToolchainCache(cachePath); aerr == nil {
			bodyAfter = after
			freeAfter = after.FSFree
		} else {
			fmt.Fprintf(os.Stderr, "gopi %s %s: the cache could not be re-measured after the trim: %v\n",
				CliVerbScratchGC, scratchGCGocacheFlag, aerr)
		}
	}
	// THE FILESYSTEM'S OWN DELTA, printed when a reclaim ran on a filesystem that
	// was below its headroom floor — the state in which the report's "is NOT touched
	// here" sentence and a completed reclaim would otherwise read as a contradiction.
	// Printed for a reclaim that moved the needle at all, so a reader never has to
	// infer free space from removed bytes (other writers share the filesystem).
	if trimmed && !verdict.Ok() && freed.FreedBytes > 0 {
		fmt.Fprintf(os.Stderr, scratchGCGocacheHeadroomDeltaFmt+"\n",
			CliVerbScratchGC, scratchGCGocacheFlag,
			scratch.SignedBytes(int64(freeAfter)-int64(rep.FSFree)), rep.Path,
			scratch.FormatBytes(rep.FSFree), scratch.FormatBytes(freeAfter),
			scratch.FormatBytes(headroom))
	}

	fmt.Printf(scratchGCGocacheLogFmt+"\n",
		scratchGCGocacheRole,
		rep.Path,
		rep.Device,
		rep.FSType,
		scratch.FormatBytes(rep.FSFree),
		scratch.FormatBytes(rep.FSTotal),
		scratch.FormatBytes(uint64(rep.Total)),
		scratch.FormatBytes(uint64(rep.AllBytes)),
		rep.Files,
		rep.Dirs,
		scratch.FormatBytes(uint64(bound)),
		rep.GrainName(),
		scratch.FormatBytes(uint64(rep.ReclaimableBytes)),
		rep.ReclaimableEntries,
		scratch.FormatBytes(uint64(rep.OldestShardBytes)),
		len(rep.Shards),
		freed.ShardCandidates,
		trimmed,
		scratch.FormatBytes(uint64(bodyAfter.Total)),
		len(freed.Removed),
	)
	if trimmed {
		// The before/after pair on its OWN line, so a reader (and the acceptance
		// check) can copy the two numbers verbatim without parsing the long line.
		// Its grain is named on the line itself: entry-grain before/after, so the
		// pair can be compared against a following run's `body=` and against `du`.
		fmt.Printf("gocache-trim role=%s path=%s grain=%s before=%d after=%d freed=%d entries=%d entries_before=%d shards=%d\n",
			scratchGCGocacheRole, rep.Path, rep.GrainName(), freed.Before, freed.After,
			freed.FreedBytes, freed.RemovedEntries, freed.BeforeEntries, len(rep.Shards))
	}
	// The toolchain's own bookkeeping is named when present, so a reader can see
	// the cache is still one its writer recognises (eviction never touches those
	// files — they are root-level FILES, unreachable by a shard-only sweep).
	if len(rep.Bookkeeping) > 0 {
		fmt.Printf("gocache-bookkeeping path=%s preserved=%v\n", rep.Path, rep.Bookkeeping)
	}
}
