// cli_tools_maintenance_help_surface_cases_test.go — the per-FORM cases for the
// G6 help-surface group. Split from cli_tools_maintenance_help_surface_basic_test.go
// so the derivation/gate machinery and the (long) case table stay readable
// apart; the completeness gate in that file is what keeps this table honest.
//
// EVERY EXPECTATION HERE IS AN OBSERVATION, NOT AN AUTHORED HOPE. Each case was
// run by hand against the real binary before it was written down, and the
// recorded wantAny is what the binary actually printed on this host. Two
// consequences follow, deliberately:
//
//   - A form whose HAPPY path needs a credential, a device, a peer or a
//     Chromium render is asserted up to its FIRST LOCAL REFUSAL (credential not
//     set, no X11 display, not reachable, file not found). That refusal is a
//     real observation of the advertised argv travelling through the real
//     dispatcher and the real guard — and it is exactly the difference between a
//     tested surface and a mocked one. Forms with NO local state at all are not
//     here: they are in g6BlockedForms with their blocker.
//   - wantAny is not slack. It names the states this host can legitimately
//     produce for that form (an X11 session present or absent; a sandboxed home
//     with no settings.json). A form whose local state space is wider than that
//     does not belong in a basic test and is declared blocked instead.
package main

import (
	"fmt"
	"github.com/nick/gopi/internal/heavytest"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/envctx"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/shot"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// g6Fixture* are the fixtures the cases share.
const (
	g6FixturePNGName = "tiny.png"
	g6FixtureTxtName = "in.txt"
)

// The text2img sizing fixture, shared by the two cases whose claim is "this
// numeric flag was HONOURED" (the positional-output form and its --out
// spelling). A number written into a case would be a bare literal (Rule 1); as
// a named pair its value and the summary sentence it is read back through stay
// one fact, so the flag and the assertion cannot drift apart.
const (
	g6Text2imgSize28      = "28"
	g6Text2imgSizeSummary = "字号 " + g6Text2imgSize28 + "px"
)

// g6Text2imgOutName is the output file BOTH spellings of the output argument
// are asserted against — `out.png` positional and `--out out.png`. One name,
// three readers (the two argv builders and the wantSurvives check), so the
// spelling that the page documents as an ALIAS cannot be read back through a
// different path than the one it wrote.
const g6Text2imgOutName = "out.png"

// g6FixtureAbsentToken is the placeholder operand the RESUME cases hand to a
// selector that must resolve against REAL state: no ledger row carries that task
// id, no row names that branch and no work tree lives at that path — so the
// refusal is the first local truth of those forms, and the assertion is on the
// refusal's own word rather than on an exit code alone.
//
// It is reused as the declared-trees fixture's entry name: the token also
// appears in no tree the operator owns, so a fixture run that somehow resolved a
// real tree would name a path this suite never planted.
const g6FixtureAbsentToken = "g6-no-such-row"

// g6FixtureAbsentPID is the operand the --pid selector case hands its selector:
// a pid NO host can have allocated, so "nothing is running" is a property of the
// fixture and not of the machine the suite happens to run on. It sits above the
// kernel's own ceiling (Documentation/sysctl/kernel.txt, /proc/sys/kernel/pid_max),
// which is the only bound that makes the claim true everywhere — a pid picked to
// be merely "unused now" is a race with the next process fork. The sibling
// fixture wcsDeadPID (1<<22, in worker_control_selector_test.go) happens to EQUAL
// this host's pid_max, so it is a live pid on a machine with the knob raised; this
// const is the one the help-surface case uses, and it cannot be reached by any
// allocation.
const g6FixtureAbsentPID = 1 << 31

// g6ProfileLedgerRel is the ledger's path RELATIVE to a profile home — the file
// `gopi config setup` / `gopi config setup` initialises, and the one this suite reads
// back to prove the provisioning arm actually provisioned (the verb's own
// printed account of what it did is not the measurement; Rule 31).
const g6ProfileLedgerRel = ".gopi/memory.db"

// g6NoX11Marker is the refusal every X11-backed form prints when no display is
// available. It is named because three case families assert on the same words
// (act, voice_input --self, screen_capture), so a reworded guard must redden all
// of them at once rather than leaving two cases asserting a sentence that no
// longer exists.
const g6NoX11Marker = "no X11 display"

// g6NoDisplayValue is the value a case writes for DISPLAY to ask for the
// no-display state: EMPTY, which the X11 layer reads as "unset" and answers in
// 0.01s with g6NoX11Marker.
//
// WHY A CASE MUST SAY SO EXPLICITLY (measured on this host, 2026-10-01): the
// ambient DISPLAY here is a FORWARDED one (`localhost:15.0`, an ssh -X leg). A
// probe that INHERITED it did not refuse — the forwarder accepted the TCP
// connect and then answered nothing, so the X11 client blocked until its own
// 20s io bound and the probe was killed at its 15s contract budget having
// printed nothing: the exact signature this suite reserves for "the argv fell
// through to the agent loop". The suite's base environment now masks DISPLAY
// (cliContractMasksAmbientDisplay); these cases pin it themselves as well, so
// their refusal is a property of the CASE and not of whatever envar the host
// happened to export.
const g6NoDisplayValue = ""

// g6NoX11Env is that pin, in the shape a case's own env function returns.
func g6NoX11Env(string) []string {
	return []string{cliContractDisplayEnvName + "=" + g6NoDisplayValue}
}

// --- the worktree-stub fixture (the scratch-gc stubs arm) --------------------

const (
	// g6StubsRootRel is the sandbox directory the stub arm is pointed at, and
	// g6StubsRoot is that directory as an absolute path. It is a WORKTREE ROOT
	// for the arm's purposes: a place a dispatched row's ground lives.
	g6StubsRootRel = "stub-workspace"
	// g6StubFixtureName is the leaked ground itself. The name must satisfy
	// scratch's own family predicate (WorktreeStubNamePrefix + 8..64 hex-ish
	// characters), which is why the tail is an opaque id rather than a word:
	// the arm matches by SHAPE first, and a fixture outside the family would be
	// skipped rather than swept — i.e. would test nothing.
	g6StubFixtureName = scratch.WorktreeStubNamePrefix + g6StubFixtureTail
	// g6StubFixtureRel is it relative to the sandbox home, for the disk checks.
	g6StubFixtureRel = g6StubsRootRel + "/" + g6StubFixtureName
	// g6StubSuiteRunName is the suite RUN directory the dangling pointer names.
	// Its shape is scratch's own (SuiteRunMarker + a runner + a generated tail);
	// the comment there is explicit that the shape is what makes a dead target
	// PROVENANCE'd as this repository's own test entry point, so a fixture using
	// another shape would be kept as "not from suite" and the case would fail —
	// which is the assertion working, not a nuisance.
	g6StubSuiteRunName = scratch.SuiteRunMarker + "g6-stub-fixture-run-2046292404"
	// g6StubsDryRunReportToken is the arm's own account of NOT applying: the
	// field plus the value, both from the arm's constants, so the case cannot
	// assert a sentence the arm no longer prints.
	g6StubsDryRunReportToken = scratchGCStubsDryRunField + "true"
	// g6StubsAppliedReportToken is the same field with the confirming run's value.
	g6StubsAppliedReportToken = scratchGCStubsDryRunField + "false"
	// g6StubsWouldRemoveToken is the per-candidate verdict line the report-only
	// arm prints for a collectable entry: the arm's OWN verdict constant (not a
	// copy of the word — a reworded plan renames it here too), the path it
	// applies to.
	g6StubsWouldRemoveToken = scratchGCStubsVerdictWouldRemove + " {{home}}/" + g6StubFixtureRel
)

// g6StubFixtureTail is an inert opaque id. Named rather than typed into
// g6StubFixtureName so the family rule (8..64 characters) is visible where a
// reader would otherwise have to count.
const g6StubFixtureTail = "01a0e85bd6977df790e4a048b251610c"

func g6StubsRoot(home string) string { return filepath.Join(home, g6StubsRootRel) }

// --- the worktree-root census fixture (the scratch-gc roots arm) --------------
//
// The SAME leaked ground serves both arms (the roots arm's removable class is
// the stubs arm's class), so these are derived from that fixture's constants
// rather than respelled — a second copy would let the two arms' fixtures drift
// apart while both cases stayed green.
var (
	// g6RootsStubChildToken is the per-child census line for the leaked ground:
	// the class word the census prints for it, the path, and its signature field.
	// The class word comes from the census's OWN label function, so a reworded
	// class renames this token too.
	g6RootsStubChildToken = scratchGCStubsChildClassToken(scratch.RootChildStub) + " path={{home}}/" + g6StubFixtureRel
	// g6RootsCountToken is the count line's arm and root fields — the claim that
	// the census ran over THIS root, not the operator's own. It is TWO tokens,
	// because the shared format's own fields (removed/stale/disabled) and this
	// arm's root field sit between the home and the counts; asserting one
	// hyphenated sentence would pin text this arm does not own.
	g6RootsCountToken = "role=" + scratchGCRootsRole + " home={{home}}/" + g6StubsRootRel
	// g6RootsCountsToken is the count line's four class counts — the census's
	// summary, and the field a reader greps for the residue family.
	g6RootsCountsToken = "children="
	// g6RootsExplainChildToken is the per-child line shape --explain adds for the
	// classes the quiet default only counts (worktree/foreign). It asserts the
	// CLASS WORD and the line's own field, not a name: which foreign children
	// exist depends on the sandbox home the case runs in.
	g6RootsExplainChildToken = "class=" + scratch.RootChildClassLabel(scratch.RootChildForeign) + " path="
	// g6RootsDryRunReportToken is the census's own account of NOT applying. It is
	// the shared suffix constant, so a reworded field reaches the arm and this
	// case at once.
	g6RootsDryRunReportToken = scratchGCDryRunSuffix
)

// g6StubsChildClassToken renders the per-child census line's leading field. The
// format string is the arm's OWN constant (scratchGCRootsChildLogFmt), reduced to
// the part before the path, so the case asserts against the printer rather than
// against a sentence copied out of it.
func scratchGCStubsChildClassToken(c scratch.RootChildClass) string {
	fmtPart := strings.SplitN(scratchGCRootsChildLogFmt, " path=", 2)[0]
	return fmt.Sprintf(fmtPart, scratch.RootChildClassLabel(c), "", false, "")
}

// NOTE (the stubs arm and the operator's real ground): the cases pin the root
// with the ADVERTISED flag (`--root <home>`, the sandbox), never through the
// ambient default. The default root here is the operator's own worktree root —
// where their live worktrees are — so a case that let the arm fall back to it
// would be sweeping the host (Rule 27's spirit, and Rule 29). The fixture lives
// inside home, so `--root` being honoured is itself part of what the case
// measures: ignore the flag and the sweep finds nothing and the disk assertions
// fail.
// g6SeedStubFixture plants ONE leaked ground, in the shape scratch's own sweep
// witness is built on (see internal/scratch's suite-run fixture, one level up):
// a `gopi-<id>` directory whose `.git` is a FILE holding a `gitdir: ` pointer at
// a path under a `suite-` run that no longer exists, plus the flat remains a
// fixture leaves beside it.
func g6SeedStubFixture(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, g6StubFixtureRel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir stub fixture %s: %v", dir, err)
	}
	target := filepath.Join(home, "memory-base", g6StubSuiteRunName, "TestG6Stubs", "001", "main",
		scratch.WorktreeStubGitPointerName, "worktrees", g6StubFixtureName)
	g6WriteFile(t, filepath.Join(dir, scratch.WorktreeStubGitPointerName), scratch.WorktreeStubGitDirKey+target+"\n")
	g6WriteFile(t, filepath.Join(dir, "README.md"), "left by the fixture\n")
}

// --- the toolchain-cache fixture (the scratch-gc gocache arm) ----------------
//
// The arm measures a Go toolchain cache: two-hex shard directories holding
// `<64-hex id>-a` / `-d` entry pairs, plus the toolchain's own root-level
// bookkeeping leaves. The fixture below is that shape, and it is SPARSE by
// construction — the entry files are given their length with `Truncate`, so the
// APPARENT size (what the arm reads through `stat`) is the entry's byte count
// while the disk cost is a couple of blocks per file. A fixture that really wrote
// 600 MiB per entry would push 2.4 GiB through the suite's disk for a number the
// arm reads from metadata (Rule 27: the fixture must not manufacture load).
//
// The AGES are the arm's own two states and they are what makes each case a
// FALSIFIER rather than a shape check: the three cold entries are past
// scratch.ToolchainCacheEvictMinAge and reclaimable, the live one is inside the
// window and must survive BOTH the report and the reclaim.
const (
	// g6GocacheFixtureCacheRel is the fixture cache's path RELATIVE to the sandbox
	// home — the path the report's `path=` field must carry, and the only reason
	// `--path` being honoured is observable at all (a form that ignored the flag
	// would measure some other directory and name it).
	g6GocacheFixtureCacheRel = "gocache-fixture"

	// g6GocacheColdShard / g6GocacheLiveShard are the fixture's two shards. The
	// first holds only cold entries, the second holds one cold entry beside the
	// live one — the mix the arm's eviction is built for (a shard with a live
	// entry still yields the cold entries beside it).
	g6GocacheColdShard = "ab"
	g6GocacheLiveShard = "cd"

	// g6GocacheColdEntryDigit / g6GocacheColdSecondDigit / g6GocacheShardColdDigit
	// / g6GocacheLiveEntryDigit are the hex characters the entry ids are rendered
	// from: one repeated character each, so the toolchain's 64-hex shape is kept
	// while a failure message still reads a distinct entry.
	g6GocacheColdEntryDigit  = "1"
	g6GocacheColdSecondDigit = "2"
	g6GocacheShardColdDigit  = "3"
	g6GocacheLiveEntryDigit  = "4"

	// g6GocacheColdAge / g6GocacheLiveAge are the entries' ages. The cold ones are
	// past scratch.ToolchainCacheEvictMinAge by a MULTIPLE, never a margin: a clock
	// skew or a slow suite must not be able to make them fresh. The live one is
	// written NOW, the freshest path through the window.
	g6GocacheColdAge = 50 * time.Hour
	g6GocacheLiveAge = 0 * time.Second

	// g6GocacheEntryBytes / g6GocacheIndexBytes are one entry's two halves: the
	// index is a handful of bytes (it is size-independent in the real toolchain),
	// the data carries the size the bound is compared against.
	g6GocacheEntryBytes = int64(600) << 20
	g6GocacheIndexBytes = int64(512)

	// g6GocacheBookkeepingName is the toolchain's own bookkeeping leaf. It is a
	// FILE at the cache's root, so the report's `preserved=` list names it and the
	// reclaim can be asserted not to have taken it (eviction never touches
	// root-level files — they are unreachable by a shard sweep).
	g6GocacheBookkeepingName = "README"

	// g6GocacheFixtureFiles / g6GocacheFixtureDirs are the counts the report must
	// carry: four entries × two halves plus the bookkeeping leaf, over two shard
	// directories.
	g6GocacheFixtureFiles = 9
	g6GocacheFixtureDirs  = 2

	// g6GocacheTrimBoundGiB is the bound the `--trim` case pins for THIS
	// invocation, through the arm's own flag. It is below the fixture's body, which
	// is what makes the reclaim bite deterministically instead of depending on a
	// host setting: a case that relied on the default bound would remove nothing —
	// and would pass a trim that did nothing at all.
	//
	// The flag is DELIBERATELY not advertised as a form by the page (the page
	// documents `--path` and `--trim`); like the base arm's GOTMPDIR pin, it is the
	// knob that makes the advertised form locally decidable, not a form of its own.
	g6GocacheTrimBoundGiB = 1

	// g6GocacheBoundGiB is the bound the `--max-gib` form carries; g6GocacheEnvBoundGiB
	// is the DIFFERENT, lower bound the same case exports in the environment. The two
	// are unequal ON PURPOSE: the printed `bound=` is then the only way to tell which
	// channel the resolver honoured, so a form that accepted the flag and ignored it
	// prints the environment's number and FAILS here, instead of passing on whatever
	// the host happens to be configured with.
	g6GocacheBoundGiB    = 9
	g6GocacheEnvBoundGiB = 3

	// g6GocacheHeadroomGiB is the free-space floor the `--headroom-gib` form carries:
	// larger than any real filesystem, so the criterion's headroom arm is what the
	// probe observes on EVERY host. The reading asserted is the floor named inside the
	// arm's own verdict sentence, which is the flag's operand rendered by the arm's
	// own formatter — a form that ignored the flag would name the DEFAULT floor there.
	g6GocacheHeadroomGiB = 1000000

	// g6GocacheToolchainEnvName is the Go toolchain's own cache-directory variable
	// — the one scratch.ResolveToolchainCachePath reads when no --path is given.
	// The tree has no exported home for it (internal/scratch spells it at the
	// resolver's single call site, internal/tools' const is unexported), so it is
	// named ONCE here for the probe's child environment rather than spread through
	// three cases as a bare string (Rule 1b).
	g6GocacheToolchainEnvName = "GOCACHE"

	// g6GocacheIgnoredCacheRel is the ABSENT directory the `--path` case points the
	// toolchain's own variable at. It is the falsifier's other half: `--path` must
	// win over it, so a `path=` naming the fixture proves the argument that was
	// resolved came from the flag the page advertises.
	g6GocacheIgnoredCacheRel = "cache-absent-toolchain-default"
)

// g6GocacheTrimBoundBytes is the pinned bound in the arm's own unit, so the
// assertion on `bound=` is derived from the flag operand rather than from a
// remembered rendering.
var g6GocacheTrimBoundBytes = int64(g6GocacheTrimBoundGiB) * scratch.ToolchainCacheGiBBytes

// The fixture's derived numbers and the tokens built from them. Every assertion
// below reads its byte count from HERE, so the fixture's arithmetic and the case's
// expectation cannot drift apart (Rule 1b).
var (
	// g6GocacheColdBytes is the reclaimable half: the three entries past the grace
	// window, at the arm's entry grain (index + data per entry).
	g6GocacheColdBytes = 3 * (g6GocacheIndexBytes + g6GocacheEntryBytes)
	// g6GocacheLiveBytes is the entry inside the window — what the cache holds
	// AFTER a reclaim down to the pinned bound.
	g6GocacheLiveBytes = g6GocacheIndexBytes + g6GocacheEntryBytes
	// g6GocacheBodyBytes is the whole fixture body: the number the bound is
	// compared against and the one `body=` must print.
	g6GocacheBodyBytes = g6GocacheColdBytes + g6GocacheLiveBytes

	// g6GocacheBodyToken / g6GocacheReclaimableToken / g6GocacheAfterBoundToken are
	// the three measured byte readings the report makes, each rendered through the
	// arm's own formatter.
	g6GocacheBodyToken        = "body=" + scratch.FormatBytes(uint64(g6GocacheBodyBytes))
	g6GocacheReclaimableToken = "reclaimable=" + scratch.FormatBytes(uint64(g6GocacheColdBytes))
	g6GocacheAfterBoundToken  = "body_after=" + scratch.FormatBytes(uint64(g6GocacheLiveBytes))

	// g6GocacheTrimFalseToken / g6GocacheTrimTrueToken are the arm's own account of
	// whether it deleted anything. A REPORT must say false (Rule 41: the finder
	// proposes), the reclaim must say true.
	g6GocacheTrimFalseToken = "trim=false"
	g6GocacheTrimTrueToken  = "trim=true"

	// g6GocachePreservedToken is the bookkeeping leaf in the report's own list — the
	// arm naming what it did not touch.
	g6GocachePreservedToken = "preserved=[" + g6GocacheBookkeepingName + "]"

	// g6GocacheBoundToken / g6GocacheHeadroomFloorToken are the two GiB flags'
	// operands as the ARM renders them: the bound a `--max-gib` invocation judged
	// against, and the floor its low-headroom sentence names. Both go through the
	// same formatter the arm uses, so a case cannot assert a rendering the arm does
	// not print (Rules 1b/13) — and both are DISTINCT from what the default or the
	// environment would produce, which is what makes them falsifiers.
	g6GocacheBoundToken         = "bound=" + scratch.FormatBytes(g6GocacheBoundBytes())
	g6GocacheHeadroomFloorToken = "the " + scratch.FormatBytes(g6GocacheHeadroomBytes()) + " headroom floor"

	// g6GocacheColdEntries / g6GocacheShardColdEntries are the TWO GRAIN readings
	// over ONE cache, and their disagreement is the arm's whole reason for printing
	// both: three entries sit past the grace window (the ENTRY grain's answer, the
	// one the reclaim honours), but only two of them live in a shard that is WHOLLY
	// cold (the SHARD grain's, kept for comparison) — the third is in cd/ beside a
	// FRESH entry, and the coarse grain cannot reach an entry inside a shard whose
	// newest entry is young. So a reclaim that freed the SHARD grain's answer would
	// leave cd/'s cold entry standing, and these two counts are what makes the case
	// a FALSIFIER of that reclaim rather than a shape check of the report.
	//
	// They are derived from the fixture's own entry arithmetic (Rule 1b): the count
	// and the bytes it is rendered from are the same set of files, so the assertion
	// on `reclaimable_entries=` can never drift from `reclaimable=`.
	g6GocacheColdEntries      = 3
	g6GocacheShardColdEntries = 2

	g6GocacheEntryTotalBytes = g6GocacheIndexBytes + g6GocacheEntryBytes

	// g6GocacheReclaimableCountToken / g6GocacheShardReclaimableToken are those two
	// readings as the ARM prints them: the entry grain first, with the old shard
	// grain beside it on the same line, so one cache shows both answers at once —
	// the divergence is the assertion. `shard_reclaimable=` is rendered through the
	// arm's own formatter because the arm renders it, so a case cannot assert a
	// spelling the arm does not print (Rules 1b/13).
	g6GocacheReclaimableCountToken = "reclaimable_entries=" + strconv.Itoa(g6GocacheColdEntries)
	g6GocacheShardReclaimableToken = "shard_reclaimable=" + scratch.FormatBytes(uint64(g6GocacheShardColdEntries)*uint64(g6GocacheEntryTotalBytes))

	// g6GocacheTrimBeforeToken / g6GocacheTrimAfterToken / g6GocacheTrimFreedToken
	// are the `--trim` line's before/after/freed triple — the only place the arm
	// states its reclaim as RAW BYTES, and therefore the strongest evidence the
	// reclaim ran at the ENTRY grain: `freed=` is all THREE cold entries, a total a
	// shard-grain reclaim could not produce (it would free the two in the wholly-cold
	// shard and leave the third). They are asserted as byte integers, not as the
	// rendered `body_after=` figure, so a reader can diff them against `du` verbatim.
	g6GocacheTrimBeforeToken = "before=" + strconv.FormatInt(g6GocacheBodyBytes, 10)
	g6GocacheTrimAfterToken  = "after=" + strconv.FormatInt(g6GocacheLiveBytes, 10)
	g6GocacheTrimFreedToken  = "freed=" + strconv.FormatInt(g6GocacheColdBytes, 10)

	// g6GocacheColdEntryFiles / g6GocacheLiveEntryFiles are the entries' BOTH
	// halves as paths RELATIVE to the sandbox home, so the disk assertions name
	// every file the fixture planted and not only half of each entry.
	//
	// BOTH HALVES IS THE POINT (kept from the sibling branch's version of this
	// fixture): an eviction that took an entry's index file and left its data file
	// would still be counted as ONE removal by the arm's own bookkeeping — the
	// `entries_removed=` reading counts entries, not paths — so a disk assertion
	// naming only the index half could not see it. Naming both makes a HALF
	// eviction fail here, which is the one shape the arm's own account cannot
	// distinguish from a clean one.
	g6GocacheColdEntryFiles = []string{
		g6GocacheEntryRel(g6GocacheColdShard, g6GocacheColdEntryDigit),
		g6GocacheEntryDataRel(g6GocacheColdShard, g6GocacheColdEntryDigit),
		g6GocacheEntryRel(g6GocacheColdShard, g6GocacheColdSecondDigit),
		g6GocacheEntryDataRel(g6GocacheColdShard, g6GocacheColdSecondDigit),
		g6GocacheEntryRel(g6GocacheLiveShard, g6GocacheShardColdDigit),
		g6GocacheEntryDataRel(g6GocacheLiveShard, g6GocacheShardColdDigit),
	}
	g6GocacheLiveEntryFiles = []string{
		g6GocacheEntryRel(g6GocacheLiveShard, g6GocacheLiveEntryDigit),
		g6GocacheEntryDataRel(g6GocacheLiveShard, g6GocacheLiveEntryDigit),
	}

	// g6GocacheShardDirRels are the two shard DIRECTORIES, asserted to SURVIVE the
	// reclaim — including the one it emptied.
	//
	// This is a grain assertion and not a tidiness check: the reclaim's unit is the
	// ENTRY, so a shard is never the thing removed, and a reclaim that worked at the
	// coarse grain would plausibly take the whole emptiable shard directory with it.
	// Asserting both directories stand (one now empty) is how the case sees the
	// difference on disk, where the arm's own count could not.
	g6GocacheShardDirRels = []string{
		filepath.Join(g6GocacheFixtureCacheRel, g6GocacheColdShard),
		filepath.Join(g6GocacheFixtureCacheRel, g6GocacheLiveShard),
	}
)

// g6GocacheTrimSurvivorRels is what must be on disk AFTER the reclaim: the entry
// the grace window protects and the two shard directories it sat in.
func g6GocacheTrimSurvivorRels() []string {
	out := append([]string{}, g6GocacheLiveEntryFiles...)
	return append(out, g6GocacheShardDirRels...)
}

// g6GocacheFixture is the fixture cache's absolute path inside a sandbox home.
func g6GocacheFixture(home string) string { return filepath.Join(home, g6GocacheFixtureCacheRel) }

// g6GocacheEntryID renders one fixture entry id in the toolchain's own shape:
// scratch.ToolchainCacheEntryIDHexLen repeats of one character.
func g6GocacheEntryID(digit string) string {
	return strings.Repeat(digit, scratch.ToolchainCacheEntryIDHexLen)
}

// g6GocacheEntryRel is one entry's INDEX half as a home-relative path — the
// shortest file that proves the entry is present, since the eviction removes both
// halves or neither (an eviction that took only one half would leave an entry the
// shard re-measurement reports as partial, and this assertion would catch it).
func g6GocacheEntryRel(shard, digit string) string {
	return filepath.Join(g6GocacheFixtureCacheRel, shard, g6GocacheEntryID(digit)+scratch.ToolchainCacheEntryIndexSuffix)
}

// g6GocacheEntryDataRel is ONE entry's DATA half, the companion of
// g6GocacheEntryRel above and the other file the eviction must take with it. See
// g6GocacheColdEntryFiles for why the disk assertions name both halves.
func g6GocacheEntryDataRel(shard, digit string) string {
	return filepath.Join(g6GocacheFixtureCacheRel, shard, g6GocacheEntryID(digit)+scratch.ToolchainCacheEntryDataSuffix)
}

// g6SeedGocacheFixture plants the cache the arm measures: two shards, three cold
// entries and one live one, plus the toolchain's bookkeeping leaf.
func g6SeedGocacheFixture(t *testing.T, home string) {
	t.Helper()
	for _, e := range []struct {
		shard string
		digit string
		age   time.Duration
	}{
		{g6GocacheColdShard, g6GocacheColdEntryDigit, g6GocacheColdAge},
		{g6GocacheColdShard, g6GocacheColdSecondDigit, g6GocacheColdAge},
		{g6GocacheLiveShard, g6GocacheShardColdDigit, g6GocacheColdAge},
		{g6GocacheLiveShard, g6GocacheLiveEntryDigit, g6GocacheLiveAge},
	} {
		g6SeedGocacheEntry(t, g6GocacheFixture(home), e.shard, e.digit, e.age)
	}
	g6WriteFile(t, filepath.Join(g6GocacheFixture(home), g6GocacheBookkeepingName), "fixture cache bookkeeping\n")
}

// g6SeedGocacheEntry writes one entry's two halves and backdates them. The age is
// applied to BOTH halves: an entry's age is its NEWEST file (that is the rule the
// arm measures with), so leaving one half at `now` would make every entry live and
// the fixture would prove nothing.
func g6SeedGocacheEntry(t *testing.T, root, shard, digit string, age time.Duration) {
	t.Helper()
	dir := filepath.Join(root, shard)
	if err := os.MkdirAll(dir, scratch.DirPerm); err != nil {
		t.Fatalf("seed gocache shard %s: %v", dir, err)
	}
	id := g6GocacheEntryID(digit)
	for _, half := range []struct {
		suffix string
		size   int64
	}{
		{scratch.ToolchainCacheEntryIndexSuffix, g6GocacheIndexBytes},
		{scratch.ToolchainCacheEntryDataSuffix, g6GocacheEntryBytes},
	} {
		path := filepath.Join(dir, id+half.suffix)
		if err := g6WriteSizedFile(path, half.size); err != nil {
			t.Fatalf("seed gocache entry %s: %v", path, err)
		}
		if age > 0 {
			when := time.Now().Add(-age)
			if err := os.Chtimes(path, when, when); err != nil {
				t.Fatalf("age gocache entry %s: %v", path, err)
			}
		}
	}
}

// g6WriteSizedFile creates a file of `size` bytes whose data blocks are never
// materialised: Truncate extends the length without writing a byte, so the file
// the arm measures is the size the fixture claims while the fixture costs the
// filesystem almost nothing. This is a FIXTURE choice, not a shortcut around the
// measurement — the arm reads sizes from metadata (a real cache's entries are
// sparse in the same way between toolchain writes).
func g6WriteSizedFile(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := f.Truncate(size); err != nil {
		f.Close() //stylegate:ignore the Truncate error is returned below; this close releases the fixture file on the aborting path
		return err
	}
	return f.Close()
}

// g6GocacheEnv pins the cache the arm measures through the toolchain's OWN
// variable, which is the FIRST of the resolution channels the bare form
// advertises (`--path` > `$GOCACHE` > GOCACHE in the go env file > `<user cache
// dir>/go-build`). The fixture lives inside the sandbox home, so the bare
// form is locally decidable: without the pin it would measure the OPERATOR's own
// 542 GiB cache and every byte count would be theirs.
func g6GocacheEnv(home string) []string {
	return []string{g6GocacheToolchainEnvName + "=" + g6GocacheFixture(home)}
}

// g6GocachePathEnv is the `--path` case's environment: the toolchain's own
// variable points at a directory that does NOT exist, so the two resolutions are
// distinguishable. If `--path` were ignored, the report would name the absent
// directory (and carry a zero body) — which is exactly what the case forbids.
func g6GocachePathEnv(home string) []string {
	return []string{g6GocacheToolchainEnvName + "=" + filepath.Join(home, g6GocacheIgnoredCacheRel)}
}

// g6GocacheBoundBytes / g6GocacheHeadroomBytes render the two GiB flags' operands
// at the byte scale the arm prints them at, through the arm's own GiB unit — so a
// case cannot assert a number the arm would not print (Rules 1b/13/14).
func g6GocacheBoundBytes() uint64 {
	return uint64(g6GocacheBoundGiB) * uint64(scratch.ToolchainCacheGiBBytes)
}

func g6GocacheHeadroomBytes() uint64 {
	return uint64(g6GocacheHeadroomGiB) * uint64(scratch.ToolchainCacheGiBBytes)
}

// g6GocacheEnvBoundEnv is the `--max-gib` case's environment: the bound knob set
// to a value BELOW the form's own. It exists only to be OUTRANKED — the printed
// `bound=` is then a statement about which channel the resolver read, and the case
// fails if it read this one (the flag accepted and ignored).
func g6GocacheEnvBoundEnv(home string) []string {
	return []string{scratch.ToolchainCacheMaxEnvName + "=" + strconv.Itoa(g6GocacheEnvBoundGiB)}
}

// --- the archive fixture (the --reclaim / --verify-commit modes) -------------

const (
	// g6FixtureRepoName is the fixture repo's leaf under the sandbox home; the
	// three archive helpers below (setup, argv, path) all read it.
	g6FixtureRepoName = "fixture-repo"
	// g6ArchiveReclaimBranch is the branch the fixture leaves DECIDED (it carries
	// an archive tag) and therefore reclaimable. It is the name the arm's own
	// report must echo.
	g6ArchiveReclaimBranch = "feat/g6-reclaimed"
	// g6ArchiveVerifyCommitFlag is the falsifier mode's flag. The spelling is the
	// production parser's own member set (cli_archive.go's archiveFlagMembers) —
	// taken from there rather than re-typed, so the case cannot outlive a rename.
	g6ArchiveVerifyCommitFlag = "verify-commit"
	// g6ArchiveExampleRev is the page's OWN example revision, typed into the case
	// because the advertised form carries it verbatim.
	g6ArchiveExampleRev = "e1701ac8"
	// g6ArchiveUnresolvedMarker is the word the falsifier answers with when git
	// cannot resolve a revision. It is the difference between "not on master" and
	// "I do not know" — the whole reason the mode exists — so the case asserts on
	// the ACKNOWLEDGEMENT rather than on the exit code alone.
	g6ArchiveUnresolvedMarker = "UNRESOLVED"

	// --- the archive --unreachable / --rescue-tip constants ----------------------
	//
	// Both modes were added by 4387c8a2b, which advertised them on the page without
	// regenerating devdoc/gopi-tools-maintenance-help-surface-inventory.md — the
	// drift the inventory gate now catches. Their cases live below with the rest of
	// the archive family.
	g6ArchiveUnreachableFlag = archiveUnreachableFlag
	g6ArchiveRescueTipFlag   = archiveRescueTipFlag
	// g6ArchiveReasonFlag has no production constant to alias — `reason` is
	// spelled inline in the parser's member set and its usage text — so the full
	// spelling is named here and gated the way every advertised spelling is: the
	// derivation reads the page, so a rename makes this table's key stale and
	// TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason names
	// it as a stale case.
	g6ArchiveReasonFlag = "--reason"
	// g6ArchiveUnreachableCensusMarker is the word the census mode heads its
	// report with (tools' archiveUnreachableCensusHeader spelling). Both
	// --unreachable cases assert on it: the mode's claim is that it REPORTED on
	// the repo it was pointed at, and the header is where the repo is named.
	g6ArchiveUnreachableCensusMarker = "unreachable census:"
	// g6ArchiveRescueMarker is the word the rescue mode's success line opens with,
	// and g6ArchiveNothingToRescueMarker the one its refusal opens with. They are
	// TWO states of ONE precedence rule, which is why both cases below exist: a
	// tip no durable ref holds is RESCUED (exit 0), and a tip a durable ref
	// already holds is REFUSED (also exit 0 — the run reached the verb and its
	// answer; only an UNRESOLVABLE revision exits non-zero). Asserting one
	// without the other would let a rescue that always refuses look tested.
	g6ArchiveRescueMarker          = "rescued "
	g6ArchiveNothingToRescueMarker = "rescue refused:"
	// g6ArchiveRescueReason is the reason code the two ALWAYS-RESCUED cases
	// declare. It is tools.ArchiveReasonHandMistake, referenced rather than
	// re-typed (Rule 13/14) — and it is not arbitrary: a `task/<id>` tip is FORCED
	// to tools.ArchiveReasonUnowned by the production decision path, so a fixture
	// on a `task/`-named branch would read a different reason back than it wrote.
	g6ArchiveRescueReason = tools.ArchiveReasonHandMistake
	// g6ArchiveRescueTipBranch is the throwaway branch the orphan fixture commits
	// on before deleting it — that deletion is what makes the tip UNREACHABLE,
	// which is the precondition the rescue form exists for.
	g6ArchiveRescueTipBranch = "tmp/g6-orphan"
	// g6ArchiveRescueTipSubject is what the orphan commit is ABOUT, so a rescue
	// that landed on the wrong commit's durable ref is visible in the fixture
	// rather than only in the selector's account of itself.
	g6ArchiveRescueTipSubject = "orphan fixture"
	// g6ArchiveRescueExampleRev is the page's OWN example revision, typed verbatim
	// (exactly as g6ArchiveExampleRev is for the falsifier): the page advertises
	// `--rescue-tip 00fb325abef8`, so the form is exercised with the revision it
	// prints. It resolves to nothing in the fixture repo, which is the point — the
	// refusal NAMES the revision, so the assertion is that it acknowledged not
	// knowing rather than guessing at a tip.
	g6ArchiveRescueExampleRev = "00fb325abef8"
	// g6ArchiveRescueExampleDetail is the --detail text that same example carries,
	// verbatim from the page — the form's claim includes it (--detail is on the
	// advertised form), so it is typed rather than dropped.
	g6ArchiveRescueExampleDetail = "orphaned tip from task/01a10e23"
	// g6ArchiveRescueTipRev is the revision the synopsis case hands --rescue-tip:
	// the commit the fixture orphaned one reflog entry ago. It is its own constant
	// because it is BOTH the argv operand and the thing the case is about (a
	// revision no ref names), so the spelling lives in one place.
	g6ArchiveRescueTipRev = "HEAD@{1}"
	// g6ArchiveRescueUnresolvedMarker is the phrase the rescue prints when git
	// cannot resolve the revision it was handed. It is the third state of the
	// mode's precedence rule (rescued / already held / unresolvable), for the same
	// reason g6ArchiveUnresolvedMarker exists for the falsifier: "I do not know"
	// must be distinguishable from "no".
	g6ArchiveRescueUnresolvedMarker = "does not resolve to a commit"
	// g6ArchiveNotARepoName is the directory the --repo case makes the probe's cwd
	// — deliberately NOT a repo, so `--repo` is the only thing that can reach the
	// fixture repo and the case's marker can only appear if it did.
	g6ArchiveNotARepoName = "not-a-repo-cwd"
	// g6ArchiveCensusNamesTheRepo is the --repo case's marker: the census header
	// followed by the repo it reports. The `{{home}}` token is the harness's own
	// expansion (see g6AssertFixtureState's sibling: the runner replaces it with
	// the case's sandbox home), so the assertion names the path --repo was HANDED
	// without hard-coding a directory the harness chose.
	// g6ArchiveCensusListsTheOrphanTip is the bare case's marker: the census's OWN
	// tip line must carry the fixture's orphan commit subject. The header alone
	// would be too weak — wantAny is OR-semantics, so a marker that a census of an
	// EMPTY repo also prints would leave the case green on a census that found
	// nothing (the vacuous-case trap memory #9510's pitfall 2 names). The subject
	// can only appear if this census actually listed this repo's unreachable tip,
	// so the fixture's seed and the assertion are the same fact.
	g6ArchiveCensusListsTheOrphanTip = g6ArchiveRescueTipSubject
	// g6ArchiveHomePlaceholder is the harness's own home token, expanded to the
	// case's sandbox home at RUN time (see the runner's replacement). Spelling it
	// once means the two markers below cannot drift from the token the runner
	// actually understands.
	g6ArchiveHomePlaceholder = "{{home}}"
	// g6ArchiveCensusNamesTheRepo is the --repo case's marker: the census header
	// followed by the repo path the flag was HANDED. Unlike the bare case, this one
	// stands alone rather than as an OR-fallback, because its claim IS the operand:
	// the probe's cwd is a non-repo, so a census that had ignored --repo (or fallen
	// back to the unresolved cwd) could not print this path.
	g6ArchiveCensusNamesTheRepo = g6ArchiveUnreachableCensusMarker + " " +
		g6ArchiveHomePlaceholder + "/" + g6FixtureRepoName
)

// g6FixtureRepoPath is the fixture repo the archive cases point --repo at (the
// git fixture builds it in g6ArchiveSetup; these modes only need the repo).
func g6FixtureRepoPath(home string) string { return filepath.Join(home, g6FixtureRepoName) }

const (
	// g6MergeStopWaitInterval is the duration the `--stop` case hands --wait:
	// the case never reaches a wait (there is no live loop to watch), so the
	// value only has to be the spelling the parser ADMITS.
	g6MergeStopWaitInterval = "1s"
	// g6MergeStopNoTargetMarker is the stable half of the sender's refusal —
	// merge_stop.go's mergeStopNoTargetErr renders it when no live loop
	// publishes a state record. It is named here so a rewrite of that line is
	// red in this case rather than silently unasserted.
	g6MergeStopNoTargetMarker = "no live merge loop to stop"
)

// g6ArchiveReclaimSetup builds the repo and leaves ONE decided branch in it:
// `feat/g6-reclaimed` plus the archive tag that records the decision, which is
// the exact pair `--reclaim` exists to clean up.
func g6ArchiveReclaimSetup(t *testing.T, home string) {
	t.Helper()
	repo := g6FixtureRepo(t, home)
	run := func(args ...string) {
		t.Helper()
		if out, err := g6Git(t, repo, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("branch", g6ArchiveReclaimBranch)
	run("tag", toolsArchivePrefix+g6ArchiveReclaimBranch, g6ArchiveReclaimBranch)
}

// g6ArchiveOrphanTipSetup builds the fixture repo and leaves ONE commit that NO
// ref names: it is committed on a throwaway branch, and DELETING that branch is
// what makes the tip unreachable — the precondition BOTH rescue cases below need
// (they commit their own orphans, so neither depends on the other's residue).
//
// It returns the dead tip's sha, which is the selector the cases hand the flag.
func g6ArchiveOrphanTipSetup(t *testing.T, home string) string {
	t.Helper()
	repo := g6FixtureRepo(t, home)
	run := func(args ...string) string {
		t.Helper()
		out, err := g6Git(t, repo, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// The fixture repo's OWN current branch is captured rather than assumed: git
	// init's default branch name is a host configuration (master here, main
	// elsewhere), so the helper reads it back instead of guessing.
	base := run("rev-parse", "--abbrev-ref", "HEAD")
	run("checkout", "-q", "-b", g6ArchiveRescueTipBranch)
	if err := os.WriteFile(filepath.Join(repo, "orphan.txt"), []byte("orphan\n"), 0o600); err != nil {
		t.Fatalf("write the orphan fixture's file: %v", err)
	}
	run("add", "orphan.txt")
	run("commit", "-qm", g6ArchiveRescueTipSubject)
	tip := run("rev-parse", "HEAD")
	run("checkout", "-q", base)
	run("branch", "-D", g6ArchiveRescueTipBranch)
	return tip
}

// g6ArchiveCensusSetup builds the fixture repo WITH an unreachable tip in it
// (the orphan helper) and makes it the probe's working directory: the bare
// `--unreachable` example resolves the cwd, and a census of a repo holding
// nothing unreachable would print a header and no tip — which the case's marker
// (the tip's own subject) would catch as a failure, deliberately.
func g6ArchiveCensusSetup(t *testing.T, home string) {
	t.Helper()
	g6ArchiveOrphanTipSetup(t, home)
	t.Chdir(g6FixtureRepoPath(home))
}

// g6ArchiveArgvWith builds an archive argv from a full flag SPELLING (the
// leading `--` included — the constants above alias the parser's own), in the
// order the page's own examples use.
func g6ArchiveArgvWith(flag, value string, extra ...string) []string {
	argv := []string{CliVerbArchive, flag, value}
	return append(argv, extra...)
}

// --- the --audio-source fixture (the file-source seam) ------------------------

const (
	// g6VoiceAudioSourceFixtureName is the recording the seam reads, in the
	// CAPTURE CONTRACT's own geometry (16 kHz mono s16le) — the only geometry the
	// file source accepts, so the fixture states it rather than discovering it.
	g6VoiceAudioSourceFixtureName = "audio-source-fixture.wav"
	// g6VoiceAudioSourceSeconds is the bound the case runs with. It is ALSO the
	// fixture's own duration, which is what makes the assertion below read as a
	// measurement rather than as a constant: the seam reports exactly the audio
	// it read, so the number in the output is the fixture's length.
	g6VoiceAudioSourceSeconds = "2"
	// g6VoiceAudioSourceCaptured is that measurement, spelled the way the verb
	// prints it: 2s of 16 kHz mono s16le = 64000 bytes. It is asserted BECAUSE it
	// ties the output to the file's content — a run that printed the flag line
	// while reading nothing would not produce it.
	g6VoiceAudioSourceCaptured = "captured 64000 bytes"
	// g6VoiceAudioSourceToneHz is the fixture's tone: a real waveform, not
	// silence, so a byte count coming out of a decoder that dropped the body
	// cannot match the file's length by accident.
	g6VoiceAudioSourceToneHz = 440
	// g6VoiceAudioSourceToneAmplitude keeps the tone well inside int16 so the
	// fixture is ordinary audio — a clipped square would still be 16-bit samples,
	// but the case's subject is the seam, not a saturated waveform.
	g6VoiceAudioSourceToneAmplitude = 8000
)

// g6SeedVoiceAudioSourceFixture writes a WAV in the capture contract: 16 kHz,
// mono, 16-bit signed little-endian, a 440 Hz tone for the case's duration — a
// hand-built RIFF header plus samples, because the fixture must be a FILE the
// source can validate, not a Go value the test could pass it directly.
func g6SeedVoiceAudioSourceFixture(t *testing.T, home string) {
	t.Helper()
	seconds, err := strconv.Atoi(g6VoiceAudioSourceSeconds)
	if err != nil {
		t.Fatalf("the case's own --seconds value %q is not a number: %v", g6VoiceAudioSourceSeconds, err)
	}
	samples := voiceCaptureSampleRate * seconds
	pcm := make([]byte, 0, samples*voiceCaptureBytesPerSample)
	for i := 0; i < samples; i++ {
		v := int16(g6VoiceAudioSourceToneAmplitude * math.Sin(2*math.Pi*float64(g6VoiceAudioSourceToneHz)*float64(i)/float64(voiceCaptureSampleRate)))
		pcm = append(pcm, byte(v), byte(uint16(v)>>8))
	}
	path := filepath.Join(home, g6VoiceAudioSourceFixtureName)
	if err := os.WriteFile(path, voiceBuildWAV(pcm, voiceWAVFormatPCM, voiceCaptureBitsPerSample), 0o600); err != nil {
		t.Fatalf("write the audio-source fixture %s: %v", path, err)
	}
	// The engine's deployment with the rest of the family, so the run's first
	// refusal is the transcriber rather than a missing local runtime.
	g6SeedVoiceEngineDeployment(t, home)
}

// --- the capture-device fixture (the voice_input --self family) ---------------

const (
	// g6VoiceBinDirName is the sandbox directory the stub capture tools live in.
	g6VoiceBinDirName = "voice-bin"
	// g6VoiceFixtureSeconds and g6VoiceFixtureDisplay are the page's OWN example
	// values (`--seconds 8`, `--display :57`), kept as named constants (Rule 1)
	// so the cases read as the help's lines rather than as numbers.
	g6VoiceFixtureSeconds = "8"
	g6VoiceFixtureDisplay = ":57"
	// g6VoiceShortSeconds is the bare-capture family's own advertised value: the
	// `gopi tool voice_input --seconds 5` row on the page. It is a separate name
	// from g6VoiceFixtureSeconds because the two cases state different things —
	// the --self cases replay the page's `--seconds 8` example, this one replays
	// the bare page's `--seconds 5` example — and a shared constant would let one
	// row's value drift under the other row's assertion.
	g6VoiceShortSeconds = "5"
	// g6VoiceArecordCard and g6VoiceCardName are the ONE card the stubbed
	// enumerator lists, in the exact line shape `arecord -l` prints (the parser
	// under test reads that shape, so a fixture using another one would be
	// testing the fixture).
	g6VoiceArecordCard = "card 1: G6Fixture [g6 fixture card], device 0: G6 Analog [G6 Analog]"
	g6VoiceCardName    = "G6Fixture"
	// g6VoiceArecordListFlag is the enumerator's listing flag, and
	// g6VoiceOpenRefusal is the recorder's refusal. Both are the MEASURED strings
	// of the hotplug shape (a device that was listed and is gone by the time it
	// is opened): voiceErrMissingMarkers matches the refusal, which is what turns
	// it into the "re-list the devices" advice the cases assert on.
	g6VoiceArecordListFlag = "-l"
	g6VoiceOpenRefusal     = "audio open error: No such file or directory"
	// g6VoiceStubShell is the interpreter the stubs are written for, and
	// g6VoiceStubMode makes them executable.
	g6VoiceStubShell = "/bin/sh"
	g6VoiceStubMode  = 0o700
	// g6VoiceDeviceRefusedMarker and g6VoiceRelistMarker are the two sentences
	// the policy's refusal carries: which device was refused, and what to do. The
	// device id is composed from the OWNING constants (the backend name, the
	// separator, the by-name form) so the assertion cannot drift from the id the
	// policy builds.
	g6VoiceDeviceRefusedMarker = voiceDeviceBackendALSA + voiceDeviceIDSeparator + "plughw:CARD=" + g6VoiceCardName + ",DEV=0"
	g6VoiceRelistMarker        = toolnames.ToolVoiceInput + " --" + voiceFlagListDevices

	// g6VoiceOggFixtureName / g6VoicePCMFixtureName are the two file fixtures
	// the page's own refusal examples are executed with, and g6VoiceOggFixtureBody
	// / g6VoicePCMFixtureBody are their bytes. The bodies are NOT audio and not
	// even container-shaped — deliberately: the local arm's refusal is decided by
	// the SUFFIX and by the absence of a container magic (voiceFormatRefusalHint),
	// so bytes that snuffed as a real container would assert a different branch.
	// The names carry the extension the decode-set assertion is about, and the
	// argv builds its path from them so the argv and the fixture cannot drift.
	g6VoiceOggFixtureName = "clip.ogg"
	g6VoicePCMFixtureName = "clip.pcm"
	g6VoiceOggFixtureBody = "not-ogg-bytes\n"
	g6VoicePCMFixtureBody = "not-pcm-bytes\n"
)

// g6VoiceCaptureEnv points the probe's PATH at the stub directory FIRST, so the
// device policy's own discovery (`pactl`, `arecord -l`, `parecord`) resolves to
// the stubs and never to the host's sound system. The case's own env is applied
// after the base environment, so this wins.
//
// IT ALSO PINS THE ENGINE, AND THAT PIN IS THE WHOLE FIX OF 2026-10-09 (task
// 01a11e05). Every voice_input case in this table asserts a state of the LOCAL
// engine — the device policy's refusal, or the decoder's decode-set hint — and
// that state is only reachable when the LOCAL engine is the one running. Left
// unresolved, the engine is resolved on the host's own channels, and on this
// host (and on any host that never set one) the default IS the cloud engine
// (config.AudioEngineDefault): the run then stops at the FIRST CLOUD STATE
// instead — `doubao engine needs an API key` when no key file exists under
// $HOME, or, worse, an upload of the fixture's bytes to the vendor's API when
// one does. Neither is this table's subject, and the second is not something a
// test may do at all. So the case names its engine rather than inheriting one:
// the pin makes the asserted state a property of the CASE on every host, which
// is the same argument the PATH pin above makes for the sound system.
//
// HOME IS PINNED WITH IT, for the same reason one level down: the engine
// selection and the local deployment are resolved from the profile
// (GOPI_HOME, already the sandbox) AND from $HOME (the legacy rung
// `$HOME/work/...`, and the doubao key file `$HOME/.doubao/apikey.txt`). With
// the real HOME in place a case silently READ the operator's own voice
// installation and key (measured: the device refusal the --self family asserts
// is reachable on this host only because `$HOME/work/funasr-models` happens to
// hold a complete deployment). Pinned, the family is hermetic — see
// g6SeedVoiceEngineDeployment for the deployment the engine-bearing cases then
// provide themselves.
func g6VoiceCaptureEnv(home string) []string {
	return []string{
		"PATH=" + filepath.Join(home, g6VoiceBinDirName) + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + home,
		config.AudioEngineEnv + "=" + config.AudioEngineLocal,
	}
}

// g6SeedVoiceCaptureStubs writes them: an enumerator that lists one card, a
// recorder that cannot open it, and a sound server that is not there.
func g6SeedVoiceCaptureStubs(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, g6VoiceBinDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir voice stub dir %s: %v", dir, err)
	}
	stubs := map[string]string{
		voiceArecordBinary: "#!" + g6VoiceStubShell + "\n" +
			"if [ \"$1\" = \"" + g6VoiceArecordListFlag + "\" ]; then\n" +
			"  echo '" + g6VoiceArecordCard + "'\n" +
			"  exit 0\n" +
			"fi\n" +
			"echo '" + voiceArecordBinary + ": " + g6VoiceOpenRefusal + "' 1>&2\n" +
			"exit 1\n",
		// The pulse side is simply absent: the policy's own fall-through to ALSA
		// is then a measured fact of the run rather than an assumption.
		voicePactlBinary: "#!" + g6VoiceStubShell + "\n" +
			"echo '" + voicePactlBinary + ": " + g6VoiceOpenRefusal + "' 1>&2\n" +
			"exit 1\n",
	}
	for name, body := range stubs {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), g6VoiceStubMode); err != nil {
			t.Fatalf("seed capture stub %s: %v", path, err)
		}
	}
}

// g6VoiceRuntimeStub is the LOCAL engine's runtime the engine-bearing cases
// plant: a program that fails without printing, which is the input
// voiceFormatRefusal's decode-set hint is written for (the hint fires from the
// file's own bytes, not from the engine's words — see the measured comparison
// in the cases' notes). It is a stub rather than the real 6MB llama-funasr-cli
// because the cases assert the REFUSAL path, and a real runtime would have to
// be found on the host first (Rule 35: the voice deployment is a foreign
// artifact gopi neither builds nor downloads).
const g6VoiceRuntimeStub = "#!/bin/sh\nexit 1\n"

// g6SeedVoiceEngineDeployment plants a LOCAL voice deployment in the sandbox
// profile — the runtime on rung 1 and both weight files beside it.
//
// WHY A CASE MUST PLANT ONE. The engine-bearing forms (`gopi tool voice_input`,
// its `--seconds` spelling, and the two file forms) read the deployment through
// voiceResolveDeployment, whose rung 1 is `<GOPI_HOME>/.gopi/voice` and whose
// rung 2 is `<HOME>/work/...`. With the engine pinned to local and nothing
// planted, the FIRST state of those forms is not the one they assert: it is
// "runtime not found" plus the 15-line install guidance (measured 2026-10-09).
// Planting the deployment makes the device policy the first thing reached,
// which is what the device family claims.
//
// THE WEIGHT FILES ARE EMPTY ON PURPOSE: the walk tests PRESENCE
// (voiceModelsComplete → voiceFilePresent), and the stub runtime never opens
// them. The case would be asserting the engine's own behaviour if it did, which
// is not what this table is for.
func g6SeedVoiceEngineDeployment(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, config.ProfileDirName, voiceProfileDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir voice deployment %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, voiceProfileRuntimeName), []byte(g6VoiceRuntimeStub), g6VoiceStubMode); err != nil {
		t.Fatalf("seed voice runtime in %s: %v", dir, err)
	}
	for _, name := range []string{voiceEncoderName, voiceLLMName} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, g6VoiceStubMode); err != nil {
			t.Fatalf("seed voice weight %s: %v", name, err)
		}
	}
}

// g6SeedVoiceLocalStubs is the setup the two BARE-CAPTURE cases share: the
// stub capture tools (so the policy never reaches the host's sound system) AND
// the local deployment (so the pinned engine is the one that runs).
func g6SeedVoiceLocalStubs(t *testing.T, home string) {
	t.Helper()
	g6SeedVoiceCaptureStubs(t, home)
	g6SeedVoiceEngineDeployment(t, home)
}

// g6SeedVoiceEngineFile is the setup the two FILE cases share: the fixture
// bytes at a name outside the decode set, plus the same local deployment. One
// function, so the two cases cannot disagree about what "the engine was there"
// means.
func g6SeedVoiceEngineFile(name, body string) func(*testing.T, string) {
	return func(t *testing.T, home string) {
		t.Helper()
		g6WriteFile(t, filepath.Join(home, name), body)
		g6SeedVoiceEngineDeployment(t, home)
	}
}

// The declared-trees fixture's names (g6DeclaredTreesEnv /
// g6SeedDeclaredTreesFixture). Every spelling is taken from the OWNING package's
// constant where one exists, so the fixture plants exactly the shapes the sweep
// recognises.
const (
	// g6GoTmpDirName is the sandbox's GOTMPDIR leaf. The tree is named by the
	// ENVIRONMENT (GOTMPDIR points at it), so its leaf is the harness's choice.
	g6GoTmpDirName = "go-tmp"
	// g6GoTmpFixtureName is the aged `go-build*` child planted in it. Its prefix
	// is scratch's own namespace predicate for that tree — the name rule the
	// sweep applies before age is considered, so a fixture using another prefix
	// would be skipped rather than swept.
	g6GoTmpFixtureName = scratch.GoBuildDirEntryPrefix + g6FixtureAbsentToken

	// scratchGCTreesRoleToken is the arm label the trees output carries. The
	// harness timeout diagnostic reads the SAME token to tell "the probe reached
	// its verb" from "the argv fell through" — one definition, both readers.
	scratchGCTreesRoleToken = g6VerbLineToken + scratchGCTreesRole
	// scratchGCDryRunPlanToken is the flag echo a report-only run prints.
	scratchGCDryRunPlanToken = scratchGCDryRunFlag + "=true"
)

// g6EnvProbeRel is where the `gopi config env` READ-arm case points --path:
// inside the sandbox home, so the case's promise ("it prints, it writes
// nothing") is checked against a path that belongs to the harness rather than
// to the operator's real profile.
var g6EnvProbeRel = filepath.Join(g6GoTmpDirName, "env-probe.md")

// g6GoTmpFixtureRel is the aged entry's path RELATIVE to the sandbox home, and
// g6GoTmpFixturePathToken is it as a case asserts on it — the runner expands
// {{home}} to the case's own sandbox, so the claim is a real path without this
// table hard-coding a directory the harness chose.
var (
	g6GoTmpFixtureRel       = filepath.Join(g6GoTmpDirName, g6GoTmpFixtureName)
	g6GoTmpFixturePathToken = "{{home}}/" + g6GoTmpFixtureRel
)

// g6GoTmpFixturePath is that path inside one case's sandbox home.
func g6GoTmpFixturePath(home string) string {
	return filepath.Join(home, g6GoTmpFixtureRel)
}

// The SELECTOR case's SIBLING tree — the declared tree a `--tree <go-tmp>` pass
// must leave alone, and the reason that case can claim CONFINEMENT rather than
// only "something was swept".
//
// Every spelling comes from the owning package, so the fixture plants exactly
// the shape the sweep's namespace predicate recognises: the directory is the
// profile's own raw-log leaf (`internal/scratch`'s rawLogDirName re-homed as
// `RawLogDirName()`, under `config.ProfileDirName`), and the file name is the
// writer's own prefix + suffix pair. A fixture using another shape would be
// SKIPPED by the sweep rather than spared by the selector — which would make the
// case green for the wrong reason (Rule 41's lesson: prove the mechanism, not a
// spelling).
var (
	g6RawDirRel     = filepath.Join(config.ProfileDirName, scratch.RawLogDirName())
	g6RawFixtureRel = filepath.Join(g6RawDirRel, g6RawFixtureName)
)

// g6RawFixtureName is the sibling tree's aged log. Its stem is a fixed date, so
// the name is a constant a reader can find in a listing; its AGE, not its name,
// is what makes it collectable (the same rule the sweep applies).
const g6RawFixtureName = scratch.RawLogNamePrefix + "20260101-000000" + scratch.RawLogFileSuffix

// g6SeedDeclaredTreesSelectorFixture is g6SeedDeclaredTreesFixture PLUS the
// sibling tree's aged entry, and it exists because the selector case's claim is
// a RELATION between two trees: "the one I named moved, the one I did not is
// untouched". With only the Go work tree seeded, a selector that silently
// widened would find nothing to take in the sibling and the case would pass.
//
// BOTH entries are backdated past their OWN tree's window (GoTmpStaleAge for the
// work tree, RawLogStaleAge for the raw log), so each is collectable by the age
// rule alone with no knob exported — if the pass reached the sibling, it would
// take it, and the case would fail on the surviving path rather than on a
// hypothesis.
func g6SeedDeclaredTreesSelectorFixture(t *testing.T, home string) {
	t.Helper()
	g6SeedDeclaredTreesFixture(t, home)

	path := filepath.Join(home, g6RawFixtureRel)
	if err := os.MkdirAll(filepath.Dir(path), scratch.DirPerm); err != nil {
		t.Fatalf("seed sibling declared tree %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("raw http traffic\n"), scratch.FilePerm); err != nil {
		t.Fatalf("seed sibling declared tree entry %s: %v", path, err)
	}
	stamp := time.Now().Add(-(scratch.RawLogStaleAge + g6ExplainBackdate))
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("backdate sibling declared tree entry %s: %v", path, err)
	}
}

// g6ExplainBackdate seeds the `--explain` fixture PAST the default stale window
// (scratch's default is 6h), so the verdict line under test is produced by the
// AGE rule with no knob set in the environment. It is named here because it is
// the provenance of that case AND its anti-regression guard: moving the default
// window under 7h turns the case red instead of leaving it green on a keep.
const g6ExplainBackdate = 7 * time.Hour

// g6Case is one advertised form's basic test.
type g6Case struct {
	// verb is the help page the form belongs to — the usage page must NOT be
	// the probe's answer, which is what proves the argv reached the verb.
	verb string
	// argv builds the form's arguments from the sandbox home.
	argv func(home string) []string
	// wantExit is the exit code local reality must produce (-1 = either).
	wantExit int
	// wantAny lists substrings, at least one of which must appear. Empty means
	// "assert only that the verb was reached". A `{{home}}` token is expanded
	// to the case's sandbox home, so a form whose SUCCESS output is a path can
	// state that path without hard-coding a directory the harness chose.
	wantAny []string
	// wantAll lists substrings ALL of which must appear. wantAny is OR-semantics
	// as its name says — at least one token, and the runner stops at the first
	// match — so a CONJUNCTION ("the flag was honoured AND the measurement is the
	// fixture's own") cannot be stated there at all: every sibling token is a
	// fallback that can carry the case on its own, and the case reports green with
	// its decisive reading absent. Measured on the `scratch-gc gocache --path DIR`
	// case: with the three decisive tokens in wantAny, deleting `--path` (and its
	// operand) from the argv left the case GREEN, because `trim=false` matched
	// alone — an implementation that ignores the flag passed. A case whose claim
	// IS a flag being honoured states that claim here, where a missing conjunct
	// fails the case and is NAMED in the failure. Same `{{home}}` expansion.
	wantAll []string
	// wantGone / wantSurvives name fixture paths RELATIVE to the sandbox home
	// whose presence the filesystem must show after the probe: the case's own
	// promise, read off the disk rather than off the verb's account of it.
	wantGone     []string
	wantSurvives []string
	// wantContains maps such a relative path to a substring its CONTENT must
	// carry — the difference between "a file exists" and "the file says what the
	// claim needs" (the shot arms' launch evidence: the argv record must carry
	// the verb's own headless flags).
	wantContains map[string]string
	// setup prepares the case's fixtures in the sandbox home (optional).
	setup func(t *testing.T, home string)
	// env adds environment entries on top of the sandbox contract (optional).
	env func(home string) []string
	// stdin is written to the probe's stdin (optional) — the `< input.txt`
	// invocation SHAPE, which has no argv of its own.
	stdin string
	// wantHelp marks a form whose CLAIM IS the usage page (`-h`), so "the
	// answer must not be the usage page" would invert the assertion.
	wantHelp bool
	// wantPNG names the file a render form must actually produce when it exits
	// 0. Such a form's local state space is genuinely TWO-VALUED (with a live
	// X11 session Chromium starts and the PNG lands; without one the launch is
	// refused), so asserting only "it failed" would break on a working host —
	// and asserting only the path would accept an empty file. When the exit code
	// is 0 the BYTES are checked: a real PNG signature.
	wantPNG func(home string) string
	// budget bounds the probe's wall clock for THIS case. Zero means
	// cliContractTimeout — the shared bound, and the right one for a form whose
	// local work is a usage print or a refusal. A case whose work scales with
	// the FIXTURE it was given (the declared-trees arm walks every tree) states
	// its own budget AND seeds that fixture, so the arm's duration is a
	// property of the case rather than of the host it runs on.
	budget time.Duration
	// note records WHY the expected state is the first reachable one.
	note string
}

// g6Cases is keyed by the DERIVED form string — the exact line the help prints.
// The completeness gate in the sibling file fails when a key is not an
// advertised form, or an advertised form is neither here nor in g6BlockedForms.
var g6Cases = map[string]g6Case{
	// --- vision_analyze: the image forms, asserted to the credential refusal --

	`gopi tool vision_analyze <image_path> [--prompt "text"]`: {
		verb: toolnames.ToolVisionAnalyze, wantExit: 1,
		note: "a real PNG is given; the model then needs credentials — that refusal is the first local state",
		argv: func(h string) []string {
			return []string{toolnames.ToolVisionAnalyze, filepath.Join(h, g6FixturePNGName)}
		},
		setup:   func(t *testing.T, h string) { g6WriteFixturePNG(t, filepath.Join(h, g6FixturePNGName)) },
		wantAny: []string{"not set"},
	},
	"gopi tool vision_analyze screenshot.png": {
		verb: toolnames.ToolVisionAnalyze, wantExit: 1,
		note:    "the page's own placeholder name resolves to a missing file — the file guard answers first",
		argv:    func(string) []string { return []string{toolnames.ToolVisionAnalyze, "screenshot.png"} },
		wantAny: []string{"file not found", "not set"},
	},
	`gopi tool vision_analyze chart.png --prompt "describe the chart data"`: {
		verb: toolnames.ToolVisionAnalyze, wantExit: 1,
		note: "as above with the page's own instruction; --prompt must be admitted",
		argv: func(string) []string {
			return []string{toolnames.ToolVisionAnalyze, "chart.png", "--prompt", "describe the chart data"}
		},
		wantAny: []string{"file not found", "not set"},
	},
	"gopi tool vision_analyze photo.png --prompt \"识别图片中的所有中文文字\"": {
		verb: toolnames.ToolVisionAnalyze, wantExit: 1,
		note: "the non-ASCII --prompt must survive argv unmodified; the file guard is what answers",
		argv: func(string) []string {
			return []string{toolnames.ToolVisionAnalyze, "photo.png", "--prompt", "识别图片中的所有中文文字"}
		},
		wantAny: []string{"file not found", "not set"},
	},

	// --- text2img: the render is PURE GO and lands a real file -----------------

	"gopi tool text2img <input.txt> [output.png] [flags]": {
		verb: toolnames.ToolText2img, wantExit: 0,
		note:    "zero-dependency TrueType renderer: the happy path is fully local and must produce a page",
		argv:    func(h string) []string { return []string{toolnames.ToolText2img, filepath.Join(h, g6FixtureTxtName)} },
		setup:   func(t *testing.T, h string) { g6WriteFile(t, filepath.Join(h, g6FixtureTxtName), "hello\nsecond\n") },
		wantAny: []string{"output:"},
	},
	"gopi tool text2img notes.txt": {
		verb: toolnames.ToolText2img, wantExit: 0,
		note: "the page's own short example, executed literally in a sandbox holding notes.txt",
		argv: func(string) []string { return []string{toolnames.ToolText2img, "notes.txt"} },
		setup: func(t *testing.T, h string) {
			g6WriteFile(t, filepath.Join(h, "notes.txt"), "notes for the renderer\n")
			t.Chdir(h) // the form's argv is RELATIVE, so the probe runs in the sandbox
		},
		wantAny: []string{"output:"},
	},
	"gopi tool text2img report.txt out.png --size 28 --cols 60": {
		verb: toolnames.ToolText2img, wantExit: 0,
		note: "the page's full example: the output positional and both sizing flags must all be honoured",
		argv: func(string) []string {
			return []string{
				toolnames.ToolText2img, "report.txt", g6Text2imgOutName,
				"--" + text2imgFlagSize, g6Text2imgSize28,
				"--" + text2imgFlagCols, "60",
			}
		},
		setup: func(t *testing.T, h string) {
			g6WriteFile(t, filepath.Join(h, "report.txt"), "a report\nwith two lines\n")
			t.Chdir(h)
		},
		// The size is asserted FROM THE OUTPUT LINE, not assumed: the page
		// promises the flags are applied, so the claim is checked. The file
		// itself is asserted off the DISK as well, at the name the form's own
		// positional gave — the exit status alone would accept a run that
		// printed the summary and wrote nowhere.
		wantAny:      []string{g6Text2imgSizeSummary},
		wantSurvives: []string{g6Text2imgOutName},
	},
	// The FLAG spelling of the output path, beside the positional one above.
	// The two forms differ in ONE argv slot — `out.png` becomes `--out out.png`
	// — so this case is what separates "the positional is honoured" from "the
	// documented flag spelling is an alias of it". Without it the page
	// advertised `--out PATH` (and the derivation listed it) while no case ever
	// passed that word: the exact "advertised but never typed" defect this
	// group exists to remove.
	"gopi tool text2img report.txt --out out.png --size 28": {
		verb: toolnames.ToolText2img, wantExit: 0,
		note: "the page's --out spelling of the same output: the flag must be an alias of the positional, not a second argument",
		argv: func(string) []string {
			return []string{
				toolnames.ToolText2img, "report.txt",
				"--" + text2imgFlagOut, g6Text2imgOutName,
				"--" + text2imgFlagSize, g6Text2imgSize28,
			}
		},
		setup: func(t *testing.T, h string) {
			g6WriteFile(t, filepath.Join(h, "report.txt"), "a report\nwith two lines\n")
			t.Chdir(h)
		},
		// Both halves of the promise are read back independently: the size the
		// binary says it applied, and the file the flag named, ON THE DISK — a
		// run that accepted `--out` and ignored it exits 0 with no file here,
		// which is exactly the defect an exit-status-only assertion would miss.
		wantAny:      []string{g6Text2imgSizeSummary},
		wantSurvives: []string{g6Text2imgOutName},
	},
	"gopi tool text2img paper.txt --lines 60 --lh 36": {
		verb: toolnames.ToolText2img, wantExit: 0,
		note: "the page's pagination example: --lines and --lh must both be honoured",
		argv: func(string) []string {
			return []string{toolnames.ToolText2img, "paper.txt", "--lines", "60", "--lh", "36"}
		},
		setup: func(t *testing.T, h string) {
			g6WriteFile(t, filepath.Join(h, "paper.txt"), "paper body\n")
			t.Chdir(h)
		},
		wantAny: []string{"行高 36"},
	},

	// --- screen_capture: the X11 device is the whole surface ------------------

	"gopi tool screen_capture --out before.png": {
		verb: toolnames.ToolScreenCapture, wantExit: -1,
		note: "the capture needs a live X11 display: the CASE pins DISPLAY empty (g6NoX11Env), so `no X11 display` is the refusal — the PIN is what makes it this case's state instead of the operator's forwarded display, which answers nothing and used to hang the probe to its bound (see g6NoX11Env)",
		argv: func(h string) []string {
			return []string{toolnames.ToolScreenCapture, "--out", filepath.Join(h, "before.png")}
		},
		wantPNG: func(h string) string { return filepath.Join(h, "before.png") },
		env:     g6NoX11Env,
		wantAny: []string{"{{home}}/before.png", g6NoX11Marker, `"path"`},
	},
	"gopi tool screen_capture --out after.png": {
		verb: toolnames.ToolScreenCapture, wantExit: -1,
		note: "the page's second half of the capture→compare recipe; same pinned no-display state as the first",
		argv: func(h string) []string {
			return []string{toolnames.ToolScreenCapture, "--out", filepath.Join(h, "after.png")}
		},
		wantPNG: func(h string) string { return filepath.Join(h, "after.png") },
		env:     g6NoX11Env,
		wantAny: []string{"{{home}}/after.png", g6NoX11Marker, `"path"`},
	},
	"gopi tool screen_capture [flags]": {
		verb: toolnames.ToolScreenCapture, wantExit: -1,
		note: "the bare synopsis: --out alone must be accepted by the parser, and the pinned no-display refusal is what follows",
		argv: func(h string) []string {
			return []string{toolnames.ToolScreenCapture, "--out", filepath.Join(h, "flags.png")}
		},
		wantPNG: func(h string) string { return filepath.Join(h, "flags.png") },
		env:     g6NoX11Env,
		wantAny: []string{"{{home}}/flags.png", g6NoX11Marker, `"path"`},
	},

	// --- act: the dry-run face is the locally testable half -------------------

	"gopi tool act info": {
		verb: CliVerbAct, wantExit: -1,
		note:    "read-only probe: with no display the X11 guard refuses, with one it prints the JSON state",
		argv:    func(string) []string { return []string{CliVerbAct, "info"} },
		env:     g6NoX11Env,
		wantAny: []string{g6NoX11Marker, `"action":"info"`},
	},
	"gopi tool act move X Y": {
		verb: CliVerbAct, wantExit: 1,
		note: "the synopsis placeholders are NOT numbers: the coordinate validator is what answers",
		argv: func(string) []string { return []string{CliVerbAct, "move", "X", "Y"} },
		// Observed: `gopi tool act: 'move' wants integer X Y, got "X" "Y"`.
		env:     g6NoX11Env,
		wantAny: []string{"wants integer X Y", g6NoX11Marker},
	},
	"gopi tool act click [--button N]": {
		verb: CliVerbAct, wantExit: -1,
		note:    "the optional flag spelled literally must be accepted by the parser; then the display guard answers",
		argv:    func(string) []string { return []string{CliVerbAct, "click", "[--button", "N]"} },
		env:     g6NoX11Env,
		wantAny: []string{"takes at most one button argument", g6NoX11Marker},
	},
	"gopi tool act type [--text STR]": {
		verb: CliVerbAct, wantExit: -1,
		note:    "the optional flag spelled literally must be accepted by the parser; then the display guard answers",
		argv:    func(string) []string { return []string{CliVerbAct, "type", "[--text", "STR]"} },
		env:     g6NoX11Env,
		wantAny: []string{"needs --text STR", g6NoX11Marker},
	},
	"gopi tool act key [--key NAME]": {
		verb: CliVerbAct, wantExit: -1,
		note:    "the optional flag spelled literally must be accepted by the parser; then the display guard answers",
		argv:    func(string) []string { return []string{CliVerbAct, "key", "[--key", "NAME]"} },
		env:     g6NoX11Env,
		wantAny: []string{"needs --key NAME", g6NoX11Marker},
	},
	"gopi tool act move 400 300": {
		verb: CliVerbAct, wantExit: -1,
		note:    "the page's own example coordinates are VALID, so the guard (or the injection) is the state",
		argv:    func(string) []string { return []string{CliVerbAct, "move", "400", "300", "--dry-run"} },
		env:     g6NoX11Env,
		wantAny: []string{g6NoX11Marker, `"action":"move"`},
	},
	"gopi tool act click 1": {
		verb: CliVerbAct, wantExit: -1,
		note:    "the page's positional button example; --dry-run keeps the injection out of the real desktop",
		argv:    func(string) []string { return []string{CliVerbAct, "click", "1", "--dry-run"} },
		env:     g6NoX11Env,
		wantAny: []string{g6NoX11Marker, `"action":"click"`},
	},
	`gopi tool act type --text 'say hello\n'`: {
		verb: CliVerbAct, wantExit: -1,
		note:    "the page's own example text (with its literal backslash-n) must survive argv; --dry-run keeps it off the desktop",
		argv:    func(string) []string { return []string{CliVerbAct, "type", "--text", `say hello\n`, "--dry-run"} },
		env:     g6NoX11Env,
		wantAny: []string{g6NoX11Marker, `"action":"type"`},
	},
	`gopi tool act type --text 'say hello' --enter`: {
		verb: CliVerbAct, wantExit: -1,
		note: "the page's spelled-out alternative: --enter is its own flag and must be admitted",
		argv: func(string) []string {
			return []string{CliVerbAct, "type", "--text", "say hello", "--enter", "--dry-run"}
		},
		env:     g6NoX11Env,
		wantAny: []string{g6NoX11Marker, `"action":"type"`},
	},
	// --- act --self: the addressing arm ---------------------------------------
	//
	// Added with the 2026-10-01 inventory repair. The act page advertises --self
	// three times (the --text example, the --text/--enter example and the --key
	// example) and the voice_input page points at it too, and none of the three
	// was in either map.
	//
	// The happy path needs a window that hosts THIS process, so the claim is the
	// addressing refusal — which is a real answer of the self-window resolver, not
	// a stand-in: with the display pinned empty the X11 guard refuses first
	// (g6NoX11Env), and WITH a display the resolver reports that no window carries
	// this process's pid (measured on this host against a live display, where the
	// full ancestry walk and the enumerated _NET_WM_PID set are printed). Both are
	// the binary's own first local truth for the form; the case pins the first, so
	// its outcome does not depend on whether the host has an X server at all.
	// The remaining gap (driving the happy path needs a terminal emulator owning
	// this probe) is recorded in devdoc/g6-help-surface-forwarded-display.md.

	`gopi tool act type --text 'ls -l\n' --self`: {
		verb: CliVerbAct, wantExit: 1,
		note:    "the page's own --self example: with the display pinned empty the X11 guard answers before any injection could happen",
		argv:    func(string) []string { return []string{CliVerbAct, "type", "--text", `ls -l\n`, "--" + actFlagSelf} },
		env:     g6NoX11Env,
		wantAny: []string{g6NoX11Marker},
	},
	`gopi tool act type --text 'x' --self --dry-run`: {
		verb: CliVerbAct, wantExit: 1,
		note: "the page's short --self example with the vetting flag: --self is admitted and the display guard still answers, so --dry-run is not what refuses",
		argv: func(string) []string {
			return []string{CliVerbAct, "type", "--text", "x", "--" + actFlagSelf, "--dry-run"}
		},
		env:     g6NoX11Env,
		wantAny: []string{g6NoX11Marker},
	},
	"gopi tool act key --key return --self": {
		verb: CliVerbAct, wantExit: 1,
		note:    "the page's own key example WITH the self address: the named key is accepted by the validator, and the display guard answers before any injection into the hosting window",
		argv:    func(string) []string { return []string{CliVerbAct, "key", "--key", "return", "--" + actFlagSelf} },
		env:     g6NoX11Env,
		wantAny: []string{g6NoX11Marker},
	},

	"gopi tool act key --key return": {
		verb: CliVerbAct, wantExit: -1,
		note:    "the page's own key example: the named key must be accepted by the validator, then the display guard is the state",
		argv:    func(string) []string { return []string{CliVerbAct, "key", "--key", "return", "--dry-run"} },
		env:     g6NoX11Env,
		wantAny: []string{g6NoX11Marker, `"action":"key"`},
	},

	// --- shot: Chromium is the surface; the guard is the local state ----------
	//
	// These two render forms inject the case's OWN browser (g6ShotEnv) and point
	// --out inside the sandbox. The launch, the DevTools handshake and the CDP
	// discovery are all real; what is NOT real is the host's Chromium, whose cold
	// start (5.6s idle, 28.4s at load 14.0 — measured) was this arm's whole wall
	// clock and made a routing-shaped timeout out of a slow browser. The stub's
	// cost is the case's (2.0-2.2s, flat across load), and the argv record it
	// writes is read back — so the claim here is "the verb EXECUTED the browser it
	// resolved", which is the launch half of the form, proven rather than assumed.
	// The pixel half stays heavy and off the merge path (internal/shot's
	// TestCaptureRealChromeUnreachableURL, heavytest.CriterionDeviceExec).

	"gopi tool shot --url URL [--out PATH] [flags]": {
		verb: CliVerbShot, wantExit: 1,
		// Exit 1 is no longer "two-valued": the stub admits "URL" as no browser
		// can (nothing listens on the DevTools port it advertises), so the CDP
		// discovery refusal is this arm's deterministic first local truth. A
		// successful render of a real page is the heavy test's job.
		note: "the case-owned browser is launched, answers the DevTools handshake, and nothing listens there — so CDP discovery refuses by name (measured 2.0-2.2s, host browser not involved)",
		// --out is passed INSIDE the sandbox: the help's own flag, so the render
		// never leaves a PNG in the checkout (Rule 30).
		argv: func(h string) []string {
			return []string{CliVerbShot, "--url", "URL", "--out", filepath.Join(h, g6ShotPNGName)}
		},
		env:     g6ShotEnv,
		setup:   g6SeedShotStubChrome,
		wantAny: []string{"cdp discovery", "did not report a DevTools port", "not reachable"},
		// The launch evidence is the stub's own argv record: it can exist only if
		// the verb EXECUTED the browser the env pinned, and it must carry the
		// headless flag set the verb builds (not a random exec).
		wantSurvives: []string{g6ShotStubArgvFile},
		wantContains: map[string]string{g6ShotStubArgvFile: g6ShotHeadlessFlag},
	},
	"gopi tool shot --self [--addr HOST:PORT] [--out PATH] [flags]": {
		verb: CliVerbShot, wantExit: 1,
		note: "the --self form with an unreachable address: the reachability guard answers before the render",
		argv: func(h string) []string {
			return []string{CliVerbShot, "--self", "--addr", "127.0.0.1:1", "--out", filepath.Join(h, g6ShotPNGName)}
		},
		wantAny: []string{"is not reachable", "shot:"},
	},
	"gopi tool shot --self": {
		verb: CliVerbShot, wantExit: 1,
		note: "same owned-browser fixture as the --url arm: the self page is served locally, so the only non-deterministic input was the host browser, and it is injected away here too",
		argv: func(h string) []string {
			return []string{CliVerbShot, "--self", "--out", filepath.Join(h, g6ShotPNGName)}
		},
		env:     g6ShotEnv,
		setup:   g6SeedShotStubChrome,
		wantAny: []string{"cdp discovery", "did not report a DevTools port", "not reachable"},
		// The launch half, on the disk: the stub recorded the argv it was handed.
		wantSurvives: []string{g6ShotStubArgvFile},
		wantContains: map[string]string{g6ShotStubArgvFile: g6ShotHeadlessFlag},
	},

	// --- code-search: a pure-Go ctags scan, fully local ----------------------
	//
	// Every form below runs in the sandbox home, which is NOT a repository:
	// since the wrong-ground refusal (searchExitWrongGround), that is a REFUSAL
	// with its own code, not a miss. The note on each row says which of the two
	// local truths this host produces, and the miss half is driven against a
	// real checkout in cli_search_root_test.go (the table cannot: its fixtures
	// are one sandbox home per form).

	"gopi tool code-search <symbol> [--root DIR]": {
		verb: CliVerbCodeSearch, wantExit: searchExitWrongGround,
		note:    "the sandbox home is no repository, so the search cannot run and the refusal is the honest result — NOT \"no symbol found\", which would claim a tree that was never read",
		argv:    func(string) []string { return []string{CliVerbCodeSearch, "<symbol>", "--root", "."} },
		setup:   func(t *testing.T, h string) { t.Chdir(h) },
		wantAny: []string{searchGroundLabel, "not a readable directory"},
	},
	"gopi tool code-search NewAgent": {
		verb: CliVerbCodeSearch, wantExit: searchExitWrongGround,
		note:    "run from the sandbox home, which is not a Go checkout — the wrong-ground refusal, with the absolute root named",
		argv:    func(string) []string { return []string{CliVerbCodeSearch, "NewAgent"} },
		setup:   func(t *testing.T, h string) { t.Chdir(h) },
		wantAny: []string{searchGroundLabel},
	},
	"gopi tool code-search codeSearchScope --root internal/agent": {
		verb: CliVerbCodeSearch, wantExit: searchExitWrongGround,
		note: "a relative --root that does not exist in the sandbox: the directory guard answers, and since the refusal landed it carries the right-ground code rather than the miss's",
		argv: func(string) []string {
			return []string{CliVerbCodeSearch, "codeSearchScope", "--root", "internal/agent"}
		},
		setup:   func(t *testing.T, h string) { t.Chdir(h) },
		wantAny: []string{"not a readable directory", searchGroundLabel},
	},

	// --- archive: a real git fixture, and the page's own branch names ---------

	"gopi task archive <branch> --reason <code> [--detail TEXT] [--override-completed]": {
		verb: CliVerbArchive, wantExit: 1,
		note:    "`<code>` is a placeholder, not a reason: the reason validator MUST refuse it (an unknown code is not an archive)",
		argv:    func(string) []string { return []string{CliVerbArchive, "feat/old-idea", "--reason", "<code>"} },
		wantAny: []string{"reason", "not"},
	},
	"gopi task archive --check": {
		verb: CliVerbArchive, wantExit: 1,
		note: "with no --repo the command resolves the CWD, and the probe's CWD is a worktree whose ledger is not the sandbox — the guard is the local truth",
		argv: func(h string) []string {
			return []string{CliVerbArchive, "--check", "--repo", filepath.Join(h, "not-a-repo")}
		},
		setup:   func(t *testing.T, h string) { t.Chdir(h) },
		wantAny: []string{"not a git repository", "no such file or directory", "exit status 128"},
	},

	"gopi task archive --check [--repo PATH]": {
		verb: CliVerbArchive, wantExit: 1,
		note:    "the sandbox home is not a git repo, so the repo guard answers; the fixture repo case below is the happy half",
		argv:    func(h string) []string { return []string{CliVerbArchive, "--check", "--repo", h} },
		setup:   func(t *testing.T, h string) { t.Chdir(h) },
		wantAny: []string{"git rev-parse", "not a git repository"},
	},
	"gopi task archive feat/old-idea --reason superseded --detail \"landed as feat/new-idea\"": {
		verb: CliVerbArchive, wantExit: 0,
		note: "the page's own example, executed literally against a real fixture repo with that branch",
		argv: func(h string) []string {
			return g6ArchiveArgv(h, "feat/old-idea", "superseded", "--detail", "landed as feat/new-idea")
		},
		setup:   g6ArchiveSetup,
		wantAny: []string{"archived feat/old-idea"},
	},
	"gopi task archive fix/broken --reason compile-failure": {
		verb: CliVerbArchive, wantExit: 0,
		note:    "the page's own example, executed literally against the fixture repo",
		argv:    func(h string) []string { return g6ArchiveArgv(h, "fix/broken", "compile-failure") },
		setup:   g6ArchiveSetup,
		wantAny: []string{"archived fix/broken"},
	},
	"gopi task archive feat/forgotten --reason hand-mistake --override-completed": {
		verb: CliVerbArchive, wantExit: 0,
		note: "the page's own override example: the branch has no completed row, so the override is the point",
		argv: func(h string) []string {
			return g6ArchiveArgv(h, "feat/forgotten", "hand-mistake", "--override-completed")
		},
		setup:   g6ArchiveSetup,
		wantAny: []string{"archived feat/forgotten"},
	},
	// --- archive --reclaim / --verify-commit (added with the 2026-10-01 repair)
	//
	// The page advertises both modes, and three of their forms were in neither
	// map. Neither mode's happy path is a fixture problem: BOTH are repo-local,
	// read/compute work, so both are asserted on REAL state.
	//
	//  * --reclaim deletes the refs of branches whose archive tag says the
	//    decision was made. The fixture builds exactly that pair (branch + its
	//    archive tag), so the arm's own line names the tag it deleted — and the
	//    BRANCH is then gone from the repo, which is the claim read off git
	//    rather than off the report.
	//  * --verify-commit is the FALSIFIER: it measures whether a revision is an
	//    ancestor of the integration branch. A revision the fixture never created
	//    is the case that must be answered with UNRESOLVED — the falsifier
	//    refusing to manufacture an answer (Rule 15) — and the page's own
	//    `e1701ac8` example is typed literally, because a form advertised with a
	//    literal revision is only as real as the revision it names.

	"gopi task archive --reclaim [--repo PATH]": {
		verb: CliVerbArchive, wantExit: 0,
		note: "the synopsis form: --repo is admitted, the fixture's archive tag is reclaimed, and the branch it covered is gone from the repo afterwards",
		argv: func(h string) []string {
			return []string{CliVerbArchive, "--reclaim", "--repo", g6FixtureRepoPath(h)}
		},
		setup:   g6ArchiveReclaimSetup,
		wantAny: []string{toolsArchivePrefix + g6ArchiveReclaimBranch},
	},
	"gopi task archive --verify-commit <rev> [--repo PATH]": {
		verb: CliVerbArchive, wantExit: 1,
		note: "the synopsis form: `<rev>` is not a revision, so the falsifier answers UNRESOLVED by name instead of guessing — the refusal is the claim, and it is measured against the fixture repo, not the operator's",
		argv: func(h string) []string {
			return []string{CliVerbArchive, "--" + g6ArchiveVerifyCommitFlag, g6FixtureAbsentToken, "--repo", g6FixtureRepoPath(h)}
		},
		setup:   g6ArchiveReclaimSetup,
		wantAny: []string{g6ArchiveUnresolvedMarker},
	},
	"gopi task archive --verify-commit e1701ac8": {
		verb: CliVerbArchive, wantExit: 1,
		note: "the page's OWN example revision, typed verbatim: it resolves to nothing in the fixture repo, so the answer is UNRESOLVED — the form is exercised exactly as advertised, and the assertion is that it acknowledged not knowing",
		argv: func(h string) []string {
			return []string{CliVerbArchive, "--" + g6ArchiveVerifyCommitFlag, g6ArchiveExampleRev, "--repo", g6FixtureRepoPath(h)}
		},
		setup:   g6ArchiveReclaimSetup,
		wantAny: []string{g6ArchiveUnresolvedMarker, g6ArchiveExampleRev},
	},

	// --- archive --unreachable / --rescue-tip (the census lane) --------------
	//
	// The two forms of each lane: the SYN-OPSIS line (advertised with the
	// operand placeholders `<rev>` / `<code>`) and the page's own EXAMPLE. Both
	// are driven with the operands they are advertised with, and each asserts the
	// first local truth that form reaches — the census's own count and the tip's
	// subject from the measured fixture, the rescue lane's reason-vocabulary
	// refusal and its unresolvable-tip refusal.

	"gopi task archive --unreachable [--repo PATH]": {
		verb: CliVerbArchive, wantExit: 0,
		note: "the synopsis form: --repo must be admitted AND be the repo the census reports. The probe's cwd is a NON-repo, so a census that had quietly fallen back to the resolved-to-missing cwd would refuse instead of printing the fixture repo's path — the marker carries the --repo operand, which is what makes this the reachability claim rather than a bare exit-0",
		argv: func(h string) []string {
			return []string{CliVerbArchive, g6ArchiveUnreachableFlag, "--repo", g6FixtureRepoPath(h)}
		},
		setup: func(t *testing.T, h string) {
			g6FixtureRepo(t, h)
			dir := filepath.Join(h, g6ArchiveNotARepoName)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir the non-repo cwd: %v", err)
			}
			t.Chdir(dir)
		},
		wantAny: []string{g6ArchiveCensusNamesTheRepo, g6ArchiveUnreachableCensusMarker},
	},

	"gopi task archive --unreachable": {
		verb: CliVerbArchive, wantExit: 0,
		note:    "the page's own bare example, executed literally: no --repo, so the mode resolves the CWD — and the fixture repo IS the cwd here, the state the page's examples assume (exactly what g6ArchiveArgv's comment says about --repo on the archive path)",
		argv:    func(string) []string { return []string{CliVerbArchive, g6ArchiveUnreachableFlag} },
		setup:   g6ArchiveCensusSetup,
		wantAny: []string{g6ArchiveCensusListsTheOrphanTip},
	},

	"gopi task archive --rescue-tip <rev> --reason <code> [--repo PATH]": {
		verb: CliVerbArchive, wantExit: 0,
		note: "the synopsis form, with <rev>/<code> instantiated by the fixture: HEAD@{1} is the tip the fixture orphaned, a REVISION git resolves but only the reflog holds — so this case is also the FALSIFIER for a rescue that answered out of `git log --all` (which does not print that commit) or out of a ref walk (which finds no ref naming it)",
		argv: func(h string) []string {
			return g6ArchiveArgvWith(g6ArchiveRescueTipFlag, g6ArchiveRescueTipRev,
				g6ArchiveReasonFlag, g6ArchiveRescueReason, "--repo", g6FixtureRepoPath(h))
		},
		setup:   func(t *testing.T, h string) { g6ArchiveOrphanTipSetup(t, h) },
		wantAny: []string{g6ArchiveRescueMarker},
	},

	"gopi task archive --rescue-tip 00fb325abef8 --reason hand-mistake --detail \"orphaned tip from task/01a10e23\"": {
		verb: CliVerbArchive, wantExit: 1,
		note: "the page's OWN example, typed verbatim (the revision AND the reason code it prints): 00fb325abef8 resolves to nothing in the fixture repo, so the rescue acknowledges not knowing and NAMES the revision it could not resolve rather than minting a ref for a guess — exit 1 is the third state its precedence rule has, and asserting only the exit-0 states above would leave it untyped",
		argv: func(h string) []string {
			return g6ArchiveArgvWith(g6ArchiveRescueTipFlag, g6ArchiveRescueExampleRev,
				g6ArchiveReasonFlag, g6ArchiveRescueReason,
				"--detail", g6ArchiveRescueExampleDetail,
				"--repo", g6FixtureRepoPath(h))
		},
		setup:   func(t *testing.T, h string) { g6ArchiveOrphanTipSetup(t, h) },
		wantAny: []string{g6ArchiveRescueExampleRev, g6ArchiveRescueUnresolvedMarker},
	},

	// --- archive --unreachable / --rescue-tip (advertised by 4387c8a2b) -------
	//
	// The page prints four forms for the two modes, and all four are repo-local
	// read/compute work — NO credential, device, peer or render is involved — so
	// none of them belongs in g6BlockedForms and each is asserted on REAL state.
	// The two modes' states are complementary and each is exercised on the side
	// that makes its claim non-vacuous:
	//
	//  * the CENSUS is asserted THROUGH --repo while the probe's cwd is NOT a repo
	//    (the value actually reached the repo it named), and a second time through
	//    the resolved cwd spelling the page's own bare example uses;
	//  * the RESCUE is asserted on BOTH answers it can give — a tip no durable ref
	//    holds is RESCUED, a tip a durable ref already holds is REFUSED — because
	//    a case that typed only the refusal would stay green if the mode refused
	//    everything. The refusal case reaches that state the honest way: the
	//    fixture repo's own HEAD is held by its branch, so no mock is needed.

	// --- merge: NO CASES, and that is the change -------------------------------
	//
	// The six rows that lived here typed every advertised form of `gopi merge`
	// (−-status, --already-landed, --max-conflicted-files, -h, --every, --stop)
	// because the G6 derivation reads that page. The verb no longer dispatches
	// (epic merge-removal, item 2): it left G6_VERBS, so there is no page to
	// derive and no form to type — and a case here would assert that a REFUSAL
	// exits 0, which is the opposite of the contract. The flag ADMISSION paths
	// those rows reached are still covered where they always were, by the merge
	// suite's own tests (merge_stop_test.go, merge_every_test.go) against the
	// functions rather than through argv.

	// --- worker: the READ-ONLY faces; the run/drain faces are blocked ---------
	"gopi worker --status": {
		verb: CliVerbWorker, wantExit: 0,
		note:    "a read-only process-table probe: no worker is running in the sandbox, and that is the observation",
		argv:    func(string) []string { return []string{CliVerbWorker, "--status"} },
		wantAny: []string{"no workers running"},
	},
	"gopi worker --status --json": {
		verb: CliVerbWorker, wantExit: 0,
		note:    "the MACHINE form of the same read-only probe: the sandbox runs no worker, so the document is the empty enumeration — and the case asserts the DOCUMENT's own keys (program / workers / residue_count) rather than the human line, which is what makes it a different surface from the --status row above. wantAll, not wantAny: a document missing 'workers' entirely would satisfy a disjunction by matching 'program' alone, and an ABSENT key is a different lie than an empty list (Rule 15a)",
		argv:    func(string) []string { return []string{CliVerbWorker, workerControlStatus, workerStatusJSONFlag} },
		wantAll: []string{"\"program\"", "\"workers\"", "\"residue_count\""},
	},
	"gopi worker list": {
		verb: CliVerbWorker, wantExit: 0,
		note:    "the ALIAS, typed: `list` is the POSITIONAL spelling of the same read-only probe --status runs. It is typed here rather than only asserted in-process because the word's whole risk is ROUTING — a positional that misses the control route is scanned as an EXPLICIT TASK ID, so the probe would claim a row named \"list\" instead of answering the question. The expected state is the same empty enumeration, and the exit status is the same 0.",
		argv:    func(string) []string { return []string{CliVerbWorker, workerControlList} },
		wantAny: []string{workerStatusNoWorkers},
	},
	"gopi worker --stop": {
		verb: CliVerbWorker, wantExit: 0,
		note:    "no daemon holds the sandbox lock, so the no-op refusal is the local truth — and it must NOT signal anything. The line names what was MEASURED (no live daemon state) and the second half states the process-table fact; the pre-fix wording equated 'no state file' with 'no worker' (bug 01a131c8-0500)",
		argv:    func(string) []string { return []string{CliVerbWorker, "--stop"} },
		wantAny: []string{"no DETACHED worker daemon is running"},
	},
	// The SELECTOR family (`--stop --branch B` / `--stop --pid N`): the target
	// is resolved and refused BY NAME before any process is asked to leave, so
	// the FIRST LOCAL TRUTH of each form is the resolution's own refusal — and
	// what makes it safe to type here is that a refusal signals NOTHING. The
	// happy path is host work (it stops a real worker), so it is not in this
	// table; the resolution's ledger/process-table half is driven in-process by
	// worker_control_selector_test.go.
	//
	// Each case is asserted with wantAll — EVERY fragment — and on the WORD that
	// names the target it resolved, because "the selector was honoured" and "the
	// selector was discarded and some other arm answered" print different
	// sentences. master kept the `--branch` case as a wantAny pair; both of its
	// arms are fragments of the ONE selector refusal line
	// (workerControlSelectorNoOwnerFmt), so the pair is redundant rather than
	// wrong — what it never asserted is the TARGET's identity, which wantAll
	// here does (a sentence naming a different branch would satisfy master's
	// form and fails this one).
	"gopi worker --stop --branch B": {
		verb: CliVerbWorker, wantExit: 1,
		note: "a branch no row records is refused BY NAME, naming the branch and the ledger it measured — the resolution reads the LEDGER, never the repository, so the refusal is the local truth and no worker is addressed. wantAll (not master's wantAny) also pins the TOKEN, so a sentence naming a different branch fails here",
		argv: func(string) []string {
			return []string{CliVerbWorker, workerControlStop, workerControlBranchFlag, g6FixtureAbsentToken}
		},
		wantAll: []string{"no ledger row names", g6FixtureAbsentToken},
	},
	"gopi worker --stop --pid N": {
		verb: CliVerbWorker, wantExit: 1,
		note: "an unallocated pid is refused by name with the pid it read, and NOTHING is signalled — the second half of the sentence says so. g6FixtureAbsentPID sits above every host's pid_max, so no process can hold it (a fixture pid that happened to be live would make the case's verdict a property of the host, which is what pairing this assertion with the 1<<22 fixture risked)",
		argv: func(string) []string {
			return []string{CliVerbWorker, workerControlStop, workerControlPIDFlag, strconv.Itoa(g6FixtureAbsentPID)}
		},
		wantAll: []string{strconv.Itoa(g6FixtureAbsentPID), "is not running"},
	},

	// The RESUME family is the one worker face whose ground resolution is
	// READ-ONLY: the selector is looked up and refused BY NAME before any work
	// tree, branch or ledger row is touched. The happy path is host work (it
	// spawns a child and then runs the close-out chain), so the three ground
	// selectors the page prints are asserted at that refusal — the shape the
	// resume e2e test (cli_worker_resume_e2e_test.go) drives further.
	"gopi worker --resume <task-id>": {
		verb: CliVerbWorker, wantExit: 1,
		note: "the ROW is the default selector: an id no ledger holds is named back before the ground is resolved",
		argv: func(string) []string {
			return []string{CliVerbWorker, workerFlagResume, g6FixtureAbsentToken}
		},
		wantAny: []string{"no ledger row to resume"},
	},
	"gopi worker --resume --branch B": {
		verb: CliVerbWorker, wantExit: 1,
		note: "a branch no row records is refused by name (the selector reads the ledger, never the repository)",
		argv: func(string) []string {
			return []string{CliVerbWorker, workerFlagResume, tools.TaskResumeGroundBranchFlag, g6FixtureAbsentToken}
		},
		wantAny: []string{"no ground to resume", "no ledger row names"},
	},
	"gopi worker --resume --worktree P": {
		verb: CliVerbWorker, wantExit: 1,
		note: "a path that is not a work tree is refused before the resume child is spawned — the ground's own guard answers",
		argv: func(home string) []string {
			return []string{CliVerbWorker, workerFlagResume, tools.TaskResumeGroundWorktreeFlag, filepath.Join(home, g6FixtureAbsentToken)}
		},
		wantAny: []string{"is not a work tree"},
	},

	// --- config --------------------------------------------------------------

	"gopi config show [--settings PATH]": {
		verb: CliVerbConfig, wantExit: 0,
		note: "the settings path is a sandbox file, so the print reflects the sandbox and nothing of the operator",
		argv: func(h string) []string {
			return []string{CliVerbConfig, "show", "--settings", filepath.Join(h, "empty.json")}
		},
		setup:   func(t *testing.T, h string) { g6WriteFile(t, filepath.Join(h, "empty.json"), `{"repos":{}}`) },
		wantAny: []string{"gopi configuration in force"},
	},
	"gopi config env [--write] [--path P]": {
		verb: CliVerbConfig, wantExit: 0,
		note: "the READ arm of the sub-verb, driven with --path pointed INTO the sandbox: it must print the description and write NOTHING, so the operator's own description (and the sandbox's) is never touched by a test. The writing arm is typed in-process by TestConfigEnvWriteIsFoundByTheReader; driving --write here would materialise a file this case would then have to clean up.",
		argv: func(h string) []string {
			return []string{CliVerbConfig, configEnvVerb, configEnvPathFlag, filepath.Join(h, g6EnvProbeRel)}
		},
		wantGone: []string{g6EnvProbeRel},
		wantAny:  []string{envctx.FileTitle},
	},
	"gopi config setup [flags]": {
		verb: CliVerbConfig, wantExit: 0,
		note: "the RE-HOMED provisioning arm (epic verb-restructure, 2026-10-05): the SAME command as top-level `gopi config setup`, driven against the case's own sandbox home — it provisions THAT profile (root, sessions, settings.json, memory.db) and nothing of the operator's. Idempotence is the reason a re-run is asserted too: the second run must report the same four paths as present.",
		argv: func(string) []string { return []string{CliVerbConfig, "setup"} },
		setup: func(t *testing.T, h string) {
			// The FIRST run builds the profile; the assertion below reads the
			// same four paths back off the disk, so setup's account of what it
			// did is checked against the filesystem (Rule 31).
			out, code := g6ProbeEnvBounded(t, h, gopiTestBinPath(t), nil, "", cliContractTimeout, CliVerbConfig, "setup")
			if code != 0 {
				t.Fatalf("`gopi config setup` exited %d:\n%s", code, g6Head(out))
			}
			for _, rel := range []string{g6ProfileLedgerRel} {
				if _, err := os.Stat(filepath.Join(h, rel)); err != nil {
					t.Fatalf("`gopi config setup` claimed to provision %s but it is not on disk: %v\n%s", rel, err, g6Head(out))
				}
			}
		},
		wantSurvives: []string{g6ProfileLedgerRel},
		wantAny:      []string{"gopi profile:"},
	},
	"gopi config completion <shell>": {
		verb: CliVerbConfig, wantExit: 0,
		note:    "the RE-HOMED shell-surface arm: the generator is pure Go and writes to stdout, so the claim is the emitted script — driven with the bash arm (the shell the fixture runs under). The page advertises `<shell>` and NOT a `bash|zsh|fish` shorthand: the shorthand was one token no dispatcher accepts, caught by the cross-group gate in cli_help_shorthand_gate_test.go; the three shells ARE separately typed below, under `gopi config completion bash|zsh|fish`'s successor rows.",
		argv:    func(string) []string { return []string{CliVerbConfig, "completion", "bash"} },
		wantAny: []string{"bash completion for gopi"},
	},
	"gopi config -h": {
		verb: CliVerbConfig, wantExit: 0,
		wantHelp: true, note: "the help verb itself is the surface here",
		argv:    func(string) []string { return []string{CliVerbConfig, "-h"} },
		wantAny: []string{"gopi config"},
	},

	// --- completion: generated scripts, fully local ---------------------------

	"gopi config completion bash": {
		verb: CliVerbCompletion, wantExit: 0,
		note:    "the generator is pure Go: the script must be produced on stdout",
		argv:    func(string) []string { return []string{CliVerbCompletion, "bash"} },
		wantAny: []string{"bash completion for gopi"},
	},
	"gopi config completion bash > ~/.local/share/bash-completion/completions/gopi": {
		verb: CliVerbCompletion, wantExit: 0,
		note:    "the same verb with the page's own redirect; the redirect is the SHELL's, so the assertion is the same generator output",
		argv:    func(string) []string { return []string{CliVerbCompletion, "bash"} },
		wantAny: []string{"bash completion for gopi"},
	},
	"gopi config completion zsh": {
		verb: CliVerbCompletion, wantExit: 0,
		note:    "the zsh generator's own header is the claim",
		argv:    func(string) []string { return []string{CliVerbCompletion, "zsh"} },
		wantAny: []string{"#compdef gopi"},
	},
	"gopi config completion fish": {
		verb: CliVerbCompletion, wantExit: 0,
		note:    "the fish generator's own header is the claim",
		argv:    func(string) []string { return []string{CliVerbCompletion, "fish"} },
		wantAny: []string{"fish completion for gopi"},
	},
	"gopi config completion fish > ~/.config/fish/completions/gopi.fish": {
		verb: CliVerbCompletion, wantExit: 0,
		note:    "the fish generator behind the page's redirect, as above",
		argv:    func(string) []string { return []string{CliVerbCompletion, "fish"} },
		wantAny: []string{"fish completion for gopi"},
	},

	// --- scratch-gc: the sweep, with the home pinned INTO the sandbox ---------

	"gopi tool scratch-gc": {
		verb: CliVerbScratchGC, wantExit: 0,
		note:    "the sweep is bounded to $GOPI_TEST_SCRATCH_HOME, which the probe points inside its own sandbox — nothing of the operator is touched",
		argv:    func(string) []string { return []string{CliVerbScratchGC} },
		env:     g6ScratchEnv,
		wantAny: []string{"role=cli", "removed="},
	},
	"gopi tool scratch-gc --dry-run": {
		verb: CliVerbScratchGC, wantExit: 0,
		note:    "the dry-run must report and delete nothing; dry-run=true is the promise, asserted on the output",
		argv:    func(string) []string { return []string{CliVerbScratchGC, "--dry-run"} },
		env:     g6ScratchEnv,
		wantAny: []string{"dry-run=true"},
	},
	"gopi tool scratch-gc --explain": {
		verb: CliVerbScratchGC, wantExit: 0,
		// THE IMPLICATION, ASSERTED ON THE DISK. The page used to demonstrate
		// `--explain --dry-run` as "the VETTING face" while `--explain` ALONE fell
		// through into the live sweep — so a reader who typed the flag the page's
		// own Flags section documents deleted the very entries the verdicts were
		// describing (measured on task 01a1957a: the operator's
		// `scratch-gc --explain` removed a go-build root it had just printed
		// `verdict=collectable` for). `--explain` now IMPLIES `--dry-run`, and
		// the proof is not the word "dry-run" in the output — it is the seeded,
		// backdated root STILL BEING THERE afterwards.
		note: "the verdict face alone must delete nothing: dry-run=true on the count line AND the seeded aged root still on disk (wantSurvives reads the disk, not the verb's account)",
		argv: func(string) []string {
			return []string{CliVerbScratchGC, scratchGCExplainFlag}
		},
		env:          g6ScratchEnv,
		setup:        func(t *testing.T, h string) { g6SeedAgedScratchRoot(t, h) },
		wantAll:      []string{"dry-run=true", scratchGCVerdictLabel, "verdict=" + scratchGCCollectableVerdict},
		wantSurvives: []string{g6SeedAgedScratchRootRel()},
	},
	"gopi tool scratch-gc --explain --dry-run": {
		verb: CliVerbScratchGC, wantExit: 0,
		// The VETTING face: the plan AND a verdict line per candidate. The
		// fixture below is backdated PAST scratch's default stale window, so the
		// collectable verdict is produced by the AGE rule itself — no stale-window
		// knob is exported here, which is what makes this case a guard on that
		// default rather than a test of the knob.
		note: "with --explain the run prints the plan AND its verdict half; the seeded root is past the default window, so its own verdict line must say collectable and give a reason",
		argv: func(string) []string {
			return []string{CliVerbScratchGC, scratchGCExplainFlag, scratchGCDryRunFlag}
		},
		env:     g6ScratchEnv,
		setup:   func(t *testing.T, h string) { g6SeedAgedScratchRoot(t, h) },
		wantAny: []string{"dry-run=true", scratchGCVerdictLabel, "verdict=" + scratchGCCollectableVerdict},
	},
	"gopi tool scratch-gc base": {
		verb: CliVerbScratchGC, wantExit: -1,
		note: "the memory base defaults to /dev/shm, which is SHARED — the probe therefore pins $GOPI_SCRATCH_BASE (and the whole-mode dry-run) so it cannot remove a peer's residue",
		argv: func(h string) []string { return []string{CliVerbScratchGC, "base", "--dry-run"} },
		env: func(h string) []string {
			return append(g6ScratchEnv(h), scratch.BaseEnvName+"="+filepath.Join(h, "base"))
		},
		wantAny: []string{"role=cli-base", "dry-run=true"},
	},
	"gopi tool scratch-gc base --dry-run": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "report-only over a sandboxed base: dry-run=true and the base path must both appear",
		argv: func(string) []string { return []string{CliVerbScratchGC, "base", "--dry-run"} },
		env: func(h string) []string {
			return append(g6ScratchEnv(h), scratch.BaseEnvName+"="+filepath.Join(h, "base"))
		},
		wantAny: []string{"dry-run=true"},
	},
	"gopi tool scratch-gc tick": {
		verb: CliVerbScratchGC, wantExit: -1, budget: g6DeclaredTreesBudget,
		// THE RESIDENT TICK, ON DEMAND — the one-shot face the WORKER's claim
		// loop used to fire every round (operator ruling, 2026-10-26: a worker
		// dispatches, it does not run maintenance passes). It is typed here for
		// the reason this whole group exists: a face that replaced a resident
		// carrier must not be "advertised but never typed".
		//
		// Every arm that could touch shared state is PINNED into the sandbox —
		// the scratch home, HOME and GOTMPDIR for the declared-trees arm, and
		// GOPI_SANDBOX_DIR for the sandbox arm — so this is a real tick over a
		// private tree, never the operator's. wantExit is -1 (either) because
		// the arms report per-tree verdicts: an unconfigured tree is REPORTED,
		// not an alarm (the arm's own fail-open contract).
		note: "runs the resident tick once over a sandboxed scratch home; the tick's own count line must be rendered, and the call must not reach the operator's trees",
		argv: func(string) []string { return []string{CliVerbScratchGC, scratchGCTickFlag} },
		env: func(h string) []string {
			return append(g6DeclaredTreesEnv(h),
				config.SandboxEnvName+"="+filepath.Join(h, "sandboxes"))
		},
		wantAny: []string{scratchGCRoleLabel + " role="},
	},
	"gopi tool scratch-gc trees": {
		verb: CliVerbScratchGC, wantExit: 0, budget: g6DeclaredTreesBudget,
		// The full (non-dry-run) form, over a tree the PROBE owns: GOTMPDIR is the
		// sandbox's own directory and holds one aged `go-build*` child, so this case
		// sweeps real work, reports it, and its duration is a property of that
		// fixture instead of the operator's trees (see g6DeclaredTreesEnv). The
		// other three declared trees are left UNCONFIGURED inside the sandbox (their
		// roots do not exist) — the strict proof that no operator tree is read, and
		// the reason the arm's per-tree /proc cost stays at one tree.
		// wantExit is 0 for the same reason: every tree either resolves or reports
		// itself unconfigured, and neither is an alarm.
		note:    "the declared-tree arm sweeps the sandbox's OWN Go work tree (GOTMPDIR + HOME pinned into the case's home, one aged go-build* child); the entry must be removed and the line must report it",
		argv:    func(string) []string { return []string{CliVerbScratchGC, scratchGCTreesFlag} },
		env:     g6DeclaredTreesEnv,
		setup:   g6SeedDeclaredTreesFixture,
		wantAny: []string{scratchGCTreesRoleToken, g6GoTmpFixturePathToken, "removed=1"},
		// The STATE half: the aged entry is the one this case's line reports as
		// removed, and the GOTMPDIR tree's ROOT survives (deleting it broke the next
		// build — the sweep's own invariant).
		wantGone:     []string{g6GoTmpFixtureRel},
		wantSurvives: []string{g6GoTmpDirName},
	},
	"gopi tool scratch-gc trees --dry-run": {
		verb: CliVerbScratchGC, wantExit: 0, budget: g6DeclaredTreesBudget,
		// As above, report-only: the same owned fixture, and the promise that
		// dry-run deletes NOTHING is asserted on the fixture itself (the aged entry
		// is still there afterwards), not merely on the flag being echoed.
		note: "report-only over the owned fixture: dry-run=true plus the per-tree would-remove line for the aged entry; nothing may be deleted",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCTreesFlag, scratchGCDryRunFlag}
		},
		env:     g6DeclaredTreesEnv,
		setup:   g6SeedDeclaredTreesFixture,
		wantAny: []string{scratchGCTreesRoleToken, scratchGCDryRunPlanToken, g6GoTmpFixturePathToken},
		// dry-run deletes NOTHING: the aged entry it just reported as a candidate is
		// still on disk, which is the promise read off the fixture.
		wantSurvives: []string{g6GoTmpFixtureRel, g6GoTmpDirName},
	},
	`gopi tool scratch-gc trees --tree NAME`: {
		verb: CliVerbScratchGC, wantExit: 0, budget: g6DeclaredTreesBudget,
		// THE SELECTOR'S OWN FORM. Its claim is CONFINEMENT, so the fixture gives
		// it a sibling tree to spare: an aged `gopi-raw-*.log` in the `raw`
		// declared tree, which a `--tree `+TreeNameGoTmp+` pass must leave
		// byte-for-byte alone while the Go work tree's aged entry goes.
		//
		// WHY wantAll AND NOT wantAny. The case's claim is a CONJUNCTION — the
		// report line names the tree that was ASKED for (tree=, with its own
		// window and its bytes) AND the named tree's entry moved AND the sibling's
		// did not. Under wantAny (OR-semantics) each of those is a fallback that
		// can carry the case alone, so a selector that silently widened would
		// still pass on the sibling's surviving path. wantAll names every
		// conjunct, and a mutation drops one of them by name.
		note: "the declared-tree selector: a pass named " + scratch.TreeNameGoTmp + " sweeps THAT tree's aged entry, reports tree=" + scratch.TreeNameGoTmp + " with its own window and bytes, and leaves the sibling `" + scratch.TreeNameRaw + "` tree's aged log untouched",
		argv: func(string) []string {
			return []string{CliVerbScratchGC, scratchGCTreesFlag, scratch.DeclaredTreeFlagSpelling, scratch.TreeNameGoTmp}
		},
		env:   g6DeclaredTreesEnv,
		setup: g6SeedDeclaredTreesSelectorFixture,
		wantAll: []string{
			// (c) the report line: the tree asked for, its own window, its bytes,
			// and the post-pass reading of the root.
			"tree=" + scratch.TreeNameGoTmp,
			"stale=" + scratch.GoTmpStaleAge.String(),
			"bytes=",
			"present=true",
			"entries=",
			// (a) the named tree is the one that was swept.
			"removed=1",
		},
		wantAny: []string{scratchGCTreesRoleToken},
		// (a) read off the DISK, not off the verb's account: the named tree's
		// aged entry is gone while the sibling tree's aged log is still there.
		wantGone: []string{g6GoTmpFixtureRel},
		// (b) every declared ROOT survives, the selector's own subject included —
		// and the sibling's entry, which confines the pass.
		wantSurvives: []string{g6GoTmpDirName, g6RawFixtureRel, g6RawDirRel},
	},
	// --- scratch-gc families (the sessions tree's family census) --------------
	//
	// The arm the declared-tree inventory could not produce: `trees` reports the
	// BOUND (one line per declared tree), `families` reports the SCOPE (one line
	// per SHAPE actually present, with the family that claims it and the decision
	// taken about it). The fixture is the operator's PROFILE ROOT pinned into the
	// sandbox (GOPI_HOME), seeded with one session container holding the artifact
	// families the session writers produce — so the case reads a census of a tree
	// it OWNS, and the operator's sessions tree is never walked.
	"gopi tool scratch-gc families": {
		verb: CliVerbScratchGC, wantExit: 0,
		// The arm's cost is the fixture's: seconds, not the operator's 250k-entry
		// tree. Its own budget keeps a slow host from reading as a routing bug.
		budget: g6DeclaredTreesBudget,
		note:   "the family census over a seeded sandbox sessions tree: the per-shape line naming a family, and the summary line counting the shapes, are the arm's whole claim",
		argv:   func(string) []string { return []string{CliVerbScratchGC, scratchGCFamiliesFlag} },
		env: func(h string) []string {
			return append(g6ScratchEnv(h), "GOPI_HOME="+h, "HOME="+h)
		},
		setup:   g6SeedSessionsFamilyFixture,
		wantAny: []string{g6FamiliesRoleToken, g6FamiliesRecordToken, g6FamiliesSummaryToken},
	},
	`gopi tool scratch-gc families --list`: {
		verb: CliVerbScratchGC, wantExit: 0,
		budget: g6DeclaredTreesBudget,
		note:   "the enumeration of the trees this arm censuses: every name a reader may pass to --tree, with the root it resolves to. It is what makes the selector self-describing rather than guessable",
		argv: func(string) []string {
			return []string{CliVerbScratchGC, scratchGCFamiliesFlag, scratchGCFamiliesListFlag}
		},
		env: func(h string) []string {
			return append(g6ScratchEnv(h), "GOPI_HOME="+h, "HOME="+h)
		},
		setup:        g6SeedSiblingDumpFixture,
		wantAny:      []string{g6FamiliesSessionsTree + " root=", g6FamiliesSiblingDumpTree + " root="},
		wantSurvives: []string{filepath.Join(config.ProfileDirName, g6FamiliesSiblingDumpTree)},
	},
	`gopi tool scratch-gc families --tree NAME`: {
		verb: CliVerbScratchGC, wantExit: 0,
		budget: g6DeclaredTreesBudget,
		note:   "the TREE SELECTOR over the fixture's own sibling dump tree: the line must name that TREE and that tree's BOUND family — the two fields that separate 'censused the tree I asked for' from 'censused the sessions tree again'",
		argv: func(string) []string {
			return []string{CliVerbScratchGC, scratchGCFamiliesFlag, scratchGCFamiliesTreeFlag, g6FamiliesSiblingDumpTree}
		},
		env: func(h string) []string {
			return append(g6ScratchEnv(h), "GOPI_HOME="+h, "HOME="+h)
		},
		setup:        g6SeedSiblingDumpFixture,
		wantAny:      []string{g6FamiliesSiblingDumpToken, g6FamiliesBoundToken, g6FamiliesRoleToken},
		wantSurvives: []string{filepath.Join(config.ProfileDirName, g6FamiliesSiblingDumpTree, g6FamiliesFixtureSiblingDump)},
	},
	"gopi tool scratch-gc sandboxes": {
		verb: CliVerbScratchGC, wantExit: -1,
		// PRE-EXISTING GAP, closed here because this task regenerated the same
		// inventory: the sandbox arm is advertised by the live page and was
		// neither typed nor declared blocked, so the group's completeness gate
		// was RED on master (the sandbox epic landed without its case rows).
		// The probe pins the sandbox root INSIDE its own sandbox home, so the
		// sweep cannot touch the operator's sandboxes; it is a real sweep of an
		// empty root.
		note: "the sandbox arm reads the configured sandbox root (pinned into the probe's own home); dry-run keeps it report-only",
		argv: func(h string) []string { return []string{CliVerbScratchGC, "sandboxes", "--dry-run"} },
		env: func(h string) []string {
			return append(g6ScratchEnv(h), config.SandboxEnvName+"="+filepath.Join(h, "sandboxes"))
		},
		wantAny: []string{"dry-run=true", "role=cli-sandboxes"},
	},
	"gopi tool scratch-gc sandboxes --dry-run": {
		verb: CliVerbScratchGC, wantExit: -1,
		note: "as above: the per-sandbox lines with their reason field are the claim",
		argv: func(h string) []string { return []string{CliVerbScratchGC, "sandboxes", "--dry-run"} },
		env: func(h string) []string {
			return append(g6ScratchEnv(h), config.SandboxEnvName+"="+filepath.Join(h, "sandboxes"))
		},
		wantAny: []string{"dry-run=true", "role=cli-sandboxes"},
	},
	// --- scratch-gc stubs (added with the 2026-10-01 inventory repair) ---------
	//
	// The page advertises the WORKTREE-STUB arm and three of its forms were in
	// neither map. The fixture is the shape scratch's own witness is built on
	// (internal/scratch's suite-run fixture, one level up): a directory whose
	// name carries WorktreeStubNamePrefix and whose `.git` is a FILE holding a
	// `gitdir: ` pointer at a path under a `suite-` run that no longer exists —
	// i.e. exactly a linked worktree a killed suite left behind. Its disposition
	// is then a REAL verdict from the sweep's own rule (`collectable`: the
	// pointer dangles), not a canned string.
	"gopi tool scratch-gc stubs --dry-run": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "report-only: the verdict line naming the leaked ground, its reason, and the plan echo (dry-run=true) are the arm's whole claim",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCStubsFlag, scratchGCDryRunFlag, scratchGCStubsRootFlag, g6StubsRoot(h)}
		},
		setup:        g6SeedStubFixture,
		wantAny:      []string{g6StubsWouldRemoveToken, g6StubsDryRunReportToken},
		wantSurvives: []string{g6StubFixtureRel},
	},
	"gopi tool scratch-gc stubs --yes": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "the confirming form: dry-run=false is the arm's own account, and the case's promise is read off the DISK — the leaked ground must be gone (a sweep that reports removed=1 and leaves the directory is exactly what this catches)",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCStubsFlag, scratchGCStubsYesFlag, scratchGCStubsRootFlag, g6StubsRoot(h)}
		},
		setup:        g6SeedStubFixture,
		wantAny:      []string{g6StubsAppliedReportToken},
		wantGone:     []string{g6StubFixtureRel},
		wantSurvives: []string{g6StubsRootRel},
	},
	"gopi tool scratch-gc stubs": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "the bare arm: with no --yes it REPORTS what it would take and takes nothing, so the leaked ground must still be there — the confirmation flag is what separates the two arms",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCStubsFlag, scratchGCStubsRootFlag, g6StubsRoot(h)}
		},
		setup:        g6SeedStubFixture,
		wantAny:      []string{g6StubsDryRunReportToken, g6StubsWouldRemoveToken},
		wantSurvives: []string{g6StubFixtureRel},
	},

	// --- scratch-gc roots (added with the worktree-root census arm) -----------
	//
	// Three forms, one fixture (the SAME leaked ground the stubs cases plant — the
	// roots arm's removable class IS the stubs arm's class, so reusing the fixture
	// is what makes that claim testable rather than asserted). The census is the
	// arm's whole product, so the cases read it off stdout: the per-child line for
	// the stub, the four-class count line, and — the arm's own promise — the
	// leaked ground still on disk after a bare run.
	"gopi tool scratch-gc roots": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "the census: the stub child is named with its class and reason, the count line carries the four class counts, and a report-only run leaves the ground on disk",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCRootsFlag, scratchGCStubsRootFlag, g6StubsRoot(h)}
		},
		setup:        g6SeedStubFixture,
		wantAny:      []string{g6RootsStubChildToken, g6RootsCountToken, g6RootsCountsToken, g6RootsDryRunReportToken},
		wantSurvives: []string{g6StubFixtureRel},
	},
	"gopi tool scratch-gc roots --explain": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "the census with every child: the fixture root also holds the sandbox's own directories, so --explain is what makes them appear as foreign/worktree lines instead of only being counted",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCRootsFlag, scratchGCExplainFlag, scratchGCStubsRootFlag, g6StubsRoot(h)}
		},
		setup:   g6SeedStubFixture,
		wantAny: []string{g6RootsExplainChildToken, g6RootsCountToken, g6RootsCountsToken},
	},
	"gopi tool scratch-gc roots --yes": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "the confirming form: the census and the sweep must agree, and the promise is read off the DISK — the leaked ground must be gone while the root itself survives",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCRootsFlag, scratchGCStubsYesFlag, scratchGCStubsRootFlag, g6StubsRoot(h)}
		},
		setup:        g6SeedStubFixture,
		wantAny:      []string{g6StubsAppliedReportToken, g6RootsCountToken, g6RootsCountsToken},
		wantGone:     []string{g6StubFixtureRel},
		wantSurvives: []string{g6StubsRootRel},
	},
	"gopi tool scratch-gc roots --root DIR": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "the flag spelling itself is a form the page advertises (a space-separated value, not the `=` form), and it is what points the census at the fixture root instead of the operator's own",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCRootsFlag, scratchGCStubsRootFlag, g6StubsRoot(h)}
		},
		setup:        g6SeedStubFixture,
		wantAny:      []string{g6RootsCountToken, g6RootsCountsToken},
		wantSurvives: []string{g6StubFixtureRel},
	},

	// --- scratch-gc profile (added with the profile-root arm) -----------------
	//
	// Three forms, one fixture: a sandbox profile root at <home>/.gopi holding
	// one declared child, one undeclared child and one aged residue child with a
	// payload inside it. The sandbox HOME is what puts the root there
	// (config resolves <GOPI_HOME>/.gopi), so the arm cannot reach the
	// operator's own profile.
	"gopi tool scratch-gc profile": {
		verb: CliVerbScratchGC, wantExit: 0,
		note:         "the profile-root REPORT (the bare form deletes nothing — the arm follows the stub arm's convention): the count line and the actionable class lines are its claim, and the residue child is still on disk afterwards",
		argv:         func(string) []string { return []string{CliVerbScratchGC, scratchGCProfileFlag} },
		setup:        g6SeedProfileRootFixture,
		env:          g6ProfileRootEnv,
		wantAny:      []string{"role=" + scratchGCProfileRole, "children=3", "undeclared=1"},
		wantSurvives: []string{filepath.Join(config.ProfileDirName, g6ProfileResidueName), filepath.Join(config.ProfileDirName, g6ProfileDeclaredName)},
	},
	"gopi tool scratch-gc profile --dry-run": {
		verb: CliVerbScratchGC, wantExit: 0,
		note:         "the same, spelled out: dry-run=true on the count line, and the per-child lines name the residue child removable",
		argv:         func(string) []string { return []string{CliVerbScratchGC, scratchGCProfileFlag, scratchGCDryRunFlag} },
		setup:        g6SeedProfileRootFixture,
		env:          g6ProfileRootEnv,
		wantAny:      []string{"dry-run=true", "children=3", "undeclared=1", "class=residue name=" + g6ProfileResidueName},
		wantSurvives: []string{filepath.Join(config.ProfileDirName, g6ProfileResidueName)},
	},
	"gopi tool scratch-gc profile --explain": {
		verb: CliVerbScratchGC, wantExit: 0,
		note:         "the vetting form: EVERY child gets a verdict line, including the declared one the default report leaves out — and it still deletes nothing",
		argv:         func(string) []string { return []string{CliVerbScratchGC, scratchGCProfileFlag, scratchGCExplainFlag} },
		setup:        g6SeedProfileRootFixture,
		env:          g6ProfileRootEnv,
		wantAny:      []string{"class=state name=" + g6ProfileDeclaredName, "class=undeclared name=" + g6ProfileUndeclaredName},
		wantSurvives: []string{filepath.Join(config.ProfileDirName, g6ProfileResidueName)},
	},
	"gopi tool scratch-gc profile --yes": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "the confirming form: it REMOVES the aged residue child, and the disk is the promise — the declared and undeclared children must survive it",
		argv: func(string) []string {
			return []string{CliVerbScratchGC, scratchGCProfileFlag, scratchGCStubsYesFlag}
		},
		setup:        g6SeedProfileRootFixture,
		env:          g6ProfileRootEnv,
		wantAny:      []string{"role=" + scratchGCProfileRole, "removed=1"},
		wantGone:     []string{filepath.Join(config.ProfileDirName, g6ProfileResidueName)},
		wantSurvives: []string{filepath.Join(config.ProfileDirName, g6ProfileDeclaredName), filepath.Join(config.ProfileDirName, g6ProfileUndeclaredName)},
	},

	// --- scratch-gc gocache (the toolchain cache arm) --------------------------
	//
	// Three forms, ONE fixture: a controlled toolchain cache (two shards, three
	// cold entries and one live one) planted inside the sandbox. The arms differ
	// in exactly what the page advertises:
	//
	//	bare     measure it and judge it; it DELETES NOTHING (Rule 41 — the
	//	         operator's shared cross-project cache is proposed against, not
	//	         reclaimed by a report)
	//	--path   measure THAT directory instead of the resolved one
	//	--trim   ALSO reclaim, down to the bound the invocation carries
	//
	// Each case is a FALSIFIER, not a shape check: the bare form must print
	// trim=false and leave all four entries on disk; the flag form must name the
	// directory it was handed while the toolchain's own variable points at a
	// directory that does not exist; the reclaim must take exactly the three COLD
	// entries and leave the live one — which is the grace window, read off the disk.
	"gopi tool scratch-gc gocache": {
		verb: CliVerbScratchGC, wantExit: 0,
		note:  "the MEASUREMENT (no flag): the line names the fixture cache and the holding filesystem, and its body/reclaimable readings are the fixture's own numbers",
		argv:  func(string) []string { return []string{CliVerbScratchGC, scratchGCGocacheFlag} },
		env:   g6GocacheEnv,
		setup: g6SeedGocacheFixture,
		// The secondary shape fields: the report MUST carry them, and they say
		// nothing about WHICH directory was measured or whether it was touched —
		// so an OR list is the right container (assert at least one is present).
		wantAny: []string{
			"role=" + scratchGCGocacheRole,
			"device=", "fs=", "free=", "total=",
			"grain=" + scratch.ToolchainCacheEntryGrainName,
		},
		// The DECISIVE reading, conjoined: this is the fixture's own measured size
		// (so the arm measured THIS cache, reached through the toolchain's own
		// variable with no flag), the fixture's own entry and directory counts (so
		// it enumerated it rather than describing it), and trim=false — the report
		// proposed and reclaimed nothing. Any ONE of these missing means the case
		// is not testing what its note claims, and the failure names the missing
		// one; in an OR list each would be a fallback for the others (the defect
		// this field exists to remove).
		wantAll: []string{
			"path=" + "{{home}}/" + g6GocacheFixtureCacheRel,
			g6GocacheBodyToken,
			"files=" + strconv.Itoa(g6GocacheFixtureFiles),
			"dirs=" + strconv.Itoa(g6GocacheFixtureDirs),
			g6GocacheReclaimableToken,
			// The TWO GRAINS on ONE line, and they must DISAGREE: the entry grain
			// counts three reclaimable entries, the shard grain's byte reading is the
			// two in the wholly-cold shard. Both are conjoined rather than offered as
			// alternatives — an OR pair could be satisfied by either grain's answer
			// alone, which is precisely the reading this case exists to separate (a
			// report that printed one grain twice would fail here and nowhere else).
			g6GocacheReclaimableCountToken,
			g6GocacheShardReclaimableToken,
			g6GocacheTrimFalseToken,
			g6GocachePreservedToken,
		},
		// The report's own promise: every byte is still there. The cold entries
		// are precisely the ones a report MUST NOT take, so asserting them is what
		// separates this case from the --trim one.
		wantSurvives: append(append([]string{}, g6GocacheColdEntryFiles...), g6GocacheLiveEntryFiles...),
	},
	"gopi tool scratch-gc gocache --path DIR": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "the flag points the measurement at the fixture while the toolchain's own cache variable names a directory that does not exist — a report naming the fixture is the flag being honoured",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCGocacheFlag, scratchGCGocachePathFlag, g6GocacheFixture(h)}
		},
		env:   g6GocachePathEnv,
		setup: g6SeedGocacheFixture,
		// No OR list: this case has no secondary reading worth a fallback, and the
		// arm does not echo the flag's own spelling back (measured: the line carries
		// the RESOLVED directory, `path=…`, never `--path=…`) — a token invented
		// here would be a claim about output nobody observed. Every assertion this
		// case makes is the conjunction below.
		// THE conjunction this case exists for. With GOCACHE naming a directory
		// that does NOT exist, the resolved-cache arm and the flag arm are
		// observably different, and these tokens are the difference: the measured
		// directory is the fixture (not the unresolved toolchain variable), the
		// body and the file/dir counts are the FIXTURE's own (a measurement of the
		// absent default would report nothing at all), and trim stays false. All of
		// them together are the flag being honoured; any one alone is not, which is
		// what OR-semantics could not express.
		wantAll: []string{
			"path=" + "{{home}}/" + g6GocacheFixtureCacheRel,
			g6GocacheBodyToken,
			"files=" + strconv.Itoa(g6GocacheFixtureFiles),
			"dirs=" + strconv.Itoa(g6GocacheFixtureDirs),
			g6GocacheTrimFalseToken,
		},
		wantSurvives: append(append([]string{}, g6GocacheColdEntryFiles...), g6GocacheLiveEntryFiles...),
	},
	"gopi tool scratch-gc gocache --trim": {
		verb: CliVerbScratchGC, wantExit: 0,
		// The reclaim is driven down to a bound THIS invocation carries, so its
		// outcome is a property of the case rather than of the host's settings: the
		// pinned bound is below the fixture body, which makes the eviction bite. The
		// bound flag is not itself an advertised form — it is the knob that makes
		// the advertised form decidable, the same role the base arm's TMPDIR pin
		// plays in `scratch-gc base` above.
		note: "the explicit reclaim: trim=true, the before/after pair on a line of its own, and the disk is the promise — exactly the COLD entries go and the live one stays",
		argv: func(string) []string {
			return []string{CliVerbScratchGC, scratchGCGocacheFlag, scratchGCGocacheTrimFlag,
				scratchGCGocacheMaxGiBFlag, strconv.Itoa(g6GocacheTrimBoundGiB)}
		},
		env:   g6GocacheEnv,
		setup: g6SeedGocacheFixture,
		// Secondary shape fields (OR): which role/subject the line describes. They
		// hold whether or not anything was reclaimed, so alone they prove nothing
		// about the reclaim.
		wantAny: []string{
			"role=" + scratchGCGocacheRole,
			"grain=" + scratch.ToolchainCacheEntryGrainName,
			"gocache-trim role=" + scratchGCGocacheRole,
		},
		// The reclaim's own account, conjoined: it acted (trim=true), it removed
		// exactly the cold entries, the after-body is the LIVE bytes (not "before",
		// not zero, not the whole body), and the bound it was handed is the one this
		// invocation pinned. In an OR list any surviving sibling carries the case
		// while the reading that matters is absent — e.g. a report naming the bound
		// and never saying what it did. The DISK sets below are the other half of
		// the falsifier (the verb's account vs. the filesystem), and this is the
		// account itself.
		wantAll: []string{
			g6GocacheTrimTrueToken,
			// The COUNT is the entry count, not the number of paths asserted on disk
			// (the disk sets below name both halves of each entry, so their length is
			// twice this): the arm counts ENTRIES, and deriving this token from a
			// file-path list would silently start asserting 6.
			"entries_removed=" + strconv.Itoa(g6GocacheColdEntries),
			g6GocacheAfterBoundToken,
			"bound=" + scratch.FormatBytes(uint64(g6GocacheTrimBoundBytes)),
			g6GocachePreservedToken,
			// The reclaim stated as RAW BYTES: the fixture's whole body before, the
			// live entry's bytes after, and the difference. `freed=` is the decisive
			// one and it is the ENTRY grain's total — all three cold entries,
			// including cd/'s, which the shard grain cannot reach. A reclaim that
			// worked at the coarse grain would print the two-entry total here and
			// fail this assertion while leaving the filesystem almost right; that is
			// the failure this triple is here to name.
			g6GocacheTrimBeforeToken,
			g6GocacheTrimAfterToken,
			g6GocacheTrimFreedToken,
		},
		wantGone:     g6GocacheColdEntryFiles,
		wantSurvives: g6GocacheTrimSurvivorRels(),
	},
	// --- the two GiB forms (this epic's own half) ------------------------------
	//
	// Both take an operand the invocation itself supplies, and both are asserted
	// against a reading that a DIFFERENT channel would have produced:
	//
	//	--max-gib GiB      the bound knob is exported BELOW the flag's own value,
	//	                   so `bound=` distinguishes "the flag won" from "the
	//	                   environment did" — not a shape check but a falsifier
	//	--headroom-gib GiB the floor is unsatifiable by any filesystem, so the
	//	                   low-headroom verdict is reached on every host, and the
	//	                   sentence naming the floor is where the flag's operand
	//	                   is read back
	//
	// Both are REPORT forms — the arm deletes nothing without --trim — so the disk
	// half of each case is that every entry is still standing.
	"gopi tool scratch-gc gocache --max-gib GiB": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "the bound flag must OUTRANK the environment knob: the probe exports " + scratch.ToolchainCacheMaxEnvName + "=" + strconv.Itoa(g6GocacheEnvBoundGiB) + " while the form says " + strconv.Itoa(g6GocacheBoundGiB) + ", so the printed bound separates the two channels — a form that accepted --max-gib and ignored it prints the environment's number and fails; it is a report, so the disk half is that nothing was removed",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCGocacheFlag, scratchGCGocachePathFlag, g6GocacheFixture(h),
				scratchGCGocacheMaxGiBFlag, strconv.Itoa(g6GocacheBoundGiB)}
		},
		env:   g6GocacheEnvBoundEnv,
		setup: g6SeedGocacheFixture,
		// The conjunction is the case's whole claim: the fixture was measured
		// through the flag's own directory, the bound is the FLAG's rendering, and
		// the arm reports it proposed and reclaimed nothing.
		wantAll: []string{
			"path=" + "{{home}}/" + g6GocacheFixtureCacheRel,
			g6GocacheBodyToken,
			g6GocacheBoundToken,
			g6GocacheTrimFalseToken,
		},
		wantSurvives: append(append([]string{}, g6GocacheColdEntryFiles...), g6GocacheLiveEntryFiles...),
	},
	"gopi tool scratch-gc gocache --headroom-gib GiB": {
		verb: CliVerbScratchGC, wantExit: 0,
		note: "the free-space FLOOR form: the floor is high enough that no filesystem satisfies it, so the arm's headroom arm always speaks on this host, and the sentence it prints names the floor the INVOCATION carried — the flag's operand read back from the arm's own verdict text; the sentence's own claim (the cache is NOT touched here) is then read off the disk",
		argv: func(h string) []string {
			return []string{CliVerbScratchGC, scratchGCGocacheFlag, scratchGCGocachePathFlag, g6GocacheFixture(h),
				scratchGCGocacheHeadroomGiBFlag, strconv.Itoa(g6GocacheHeadroomGiB)}
		},
		env:   g6GocacheEnv,
		setup: g6SeedGocacheFixture,
		wantAll: []string{
			"path=" + "{{home}}/" + g6GocacheFixtureCacheRel,
			g6GocacheHeadroomFloorToken,
			g6GocacheTrimFalseToken,
		},
		wantSurvives: append(append([]string{}, g6GocacheColdEntryFiles...), g6GocacheLiveEntryFiles...),
	},

	"gopi tool scratch-gc deps": {
		verb: CliVerbScratchGC, wantExit: -1,
		// The Rule 35 discovery arm: it READS the host (crontab, unit files,
		// /proc) and never writes. The assertion is the shape of the report —
		// the summary line and the two notes that name both halves of the rule
		// — because the FINDINGS are this machine's, not a fixture's. A crontab
		// that cannot be read is the arm's own non-zero exit, so wantExit -1
		// (any status) is the honest pin: the report is the claim.
		note:    "the Rule 35 host-discovery arm reports what is scheduled and resident; it is read-only, so the probe asserts the report's shape",
		argv:    func(string) []string { return []string{CliVerbScratchGC, "deps"} },
		wantAny: []string{"deps summary scheduled=", "deps note=\"host half only"},
	},
	"gopi tool scratch-gc -h": {
		verb: CliVerbScratchGC, wantExit: 0,
		wantHelp: true, note: "the page promises `this help (zero side-effects)` — the sweep must not run",
		argv:    func(string) []string { return []string{CliVerbScratchGC, "-h"} },
		wantAny: []string{scratchGCUsageFingerprint},
	},

	// --- version -------------------------------------------------------------

	"gopi version": {
		verb: CliVerbVersion, wantExit: 0,
		note:    "prints the build version; `dev` in a test build",
		argv:    func(string) []string { return []string{CliVerbVersion} },
		wantAny: []string{"dev", "."},
	},

	// --- the top-level forms that are locally decidable ----------------------

	"gopi [options] \"<prompt>\"": {
		verb: g6TopLevelVerb, wantExit: 1,
		note:    "with no credentials the run stops at the client constructor — that refusal is the local state, and it proves the prompt reached the run entry",
		argv:    func(string) []string { return []string{"say hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi [options] input.txt": {
		verb: g6TopLevelVerb, wantExit: 1,
		note:    "a real payload file is read, then the credential refusal — the payload path is what is asserted",
		argv:    func(h string) []string { return []string{filepath.Join(h, g6FixtureTxtName)} },
		setup:   func(t *testing.T, h string) { g6WriteFile(t, filepath.Join(h, g6FixtureTxtName), "hello\n") },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi [options] < input.txt": {
		verb: g6TopLevelVerb, wantExit: -1,
		note:    "an EMPTY prompt with a real payload on stdin: the stdin text becomes the task, and the run then stops at the credential refusal",
		argv:    func(string) []string { return []string{"--prompt", ""} },
		stdin:   "hello from stdin\n",
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi [options] --prompt TEXT a.png": {
		verb: g6TopLevelVerb, wantExit: 1,
		note:    "--prompt plus an image positional: the image branch announces itself, then the credential refusal",
		argv:    func(h string) []string { return []string{"--prompt", "describe", filepath.Join(h, g6FixturePNGName)} },
		setup:   func(t *testing.T, h string) { g6WriteFixturePNG(t, filepath.Join(h, g6FixturePNGName)) },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi -h | --help": {
		verb: g6TopLevelVerb, wantExit: -1,
		wantHelp: true, note: "the real -h; the page's own first line is the claim",
		argv:    func(string) []string { return []string{"-h"} },
		wantAny: []string{"GoPi — a tool-using AI coding agent"},
	},
	"gopi --help-env": {
		verb: g6TopLevelVerb, wantExit: 0,
		note:    "the env table is rendered from the config catalog — a pure print",
		argv:    func(string) []string { return []string{"--help-env"} },
		wantAny: []string{"GoPi environment variables"},
	},
	"gopi <command> -h": {
		verb: g6TopLevelVerb, wantExit: 0,
		note:    "the shape, executed with a real command word: `gopi config completion -h`",
		argv:    func(string) []string { return []string{CliVerbCompletion, "-h"} },
		wantAny: []string{"gopi config completion"},
	},
	"gopi @PATH": {
		verb: g6TopLevelVerb, wantExit: 1,
		note:    "the @ payload path: a MISSING file is refused BY NAME (the read happens before any credential), which is the local state",
		argv:    func(h string) []string { return []string{"@" + filepath.Join(h, "absent-payload.txt")} },
		wantAny: []string{"Error reading @ payload file", "no such file"},
	},
	// The two rows below drive the page's OWN spelling, verbatim: the exemplar is
	// path-qualified (`./…`), so the case runs it in a directory where exactly
	// that spelling resolves (t.Chdir to the sandbox). The page's own claim is
	// what makes the token a FILE rather than a command word — a bare `image.png`
	// reading is what the reserved word `task.txt` collided with, and the
	// path-shape contract (cmd/agent/inputpath.go) refuses such a token by NAME
	// rather than sending it to the model as prompt text.
	"gopi ./image.png": {
		verb: g6TopLevelVerb, wantExit: 1,
		note: "an image positional switches the run to the vision model; the credential refusal is the local state. " +
			"The argv is the page's own PATH-QUALIFIED spelling, run in the sandbox — the bare `image.png` reading is what a " +
			"reserved verb word (e.g. `task.txt`) collided with, so the page qualifies its exemplars and the case drives it verbatim",
		argv:    func(string) []string { return []string{"./" + g6FixturePNGName} },
		setup:   func(t *testing.T, h string) { g6WriteFixturePNG(t, filepath.Join(h, g6FixturePNGName)); t.Chdir(h) },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi ./notes.txt": {
		verb: g6TopLevelVerb, wantExit: 1,
		note: "a non-image file positional contributes its text to the task, so the run needs a model: the credential refusal " +
			"is the first local state. The file is named by PATH, which is what keeps the exemplar from being read as a verb",
		argv: func(string) []string { return []string{"./notes.txt"} },
		setup: func(t *testing.T, h string) {
			g6WriteFile(t, filepath.Join(h, "notes.txt"), "notes for the run\n")
			t.Chdir(h)
		},
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi ./PROJECT.md": {
		verb: g6TopLevelVerb, wantExit: 1,
		note: "a project-rules file as the positional is read as a payload; the credential refusal follows. " +
			"The file is PROJECT.md, the TRACKED project contract the Inputs row names (the rules file split, commit 484583a, " +
			"moved the page's exemplar off the machine-local GOPI.md)",
		argv:    func(h string) []string { return []string{filepath.Join(h, "PROJECT.md")} },
		setup:   func(t *testing.T, h string) { g6WriteFile(t, filepath.Join(h, "PROJECT.md"), "# rules\n") },
		wantAny: []string{"not set", "Error creating agent"},
	},

	// --- the top-level option table, admitted through its own parser ---------

	"gopi --settings": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the flag must parse, and the run then stops at the credential refusal",
		argv: func(h string) []string {
			return []string{"--settings", filepath.Join(h, "empty.json"), "--prompt", "hi"}
		},
		setup:   func(t *testing.T, h string) { g6WriteFile(t, filepath.Join(h, "empty.json"), `{"repos":{}}`) },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --home": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission only: --home redirects the data root, so the case points it at the sandbox",
		argv:    func(h string) []string { return []string{"--home", filepath.Join(h, "home"), "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --prompt": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: --prompt carries the task text",
		argv:    func(string) []string { return []string{"--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --model": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: a model name is accepted and the run stops at the credential refusal",
		argv:    func(string) []string { return []string{"--model", "deepseek-chat", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --api-key": {
		verb: g6TopLevelFlagsVerb, wantExit: 1,
		note: "admission: an explicitly BLANK --api-key reaches the credential guard, which is the local state — " +
			"a non-blank bogus key would send a real request (measured 2.7s to the live endpoint) and no test may depend on an external service",
		argv:    func(string) []string { return []string{"--api-key", "", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --base-url": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the base URL points at a closed local port, so no request can leave the host",
		argv:    func(string) []string { return []string{"--base-url", "http://127.0.0.1:1", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent", "connection refused"},
	},
	"gopi --vision-model": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the vision model name is a plain string",
		argv:    func(string) []string { return []string{"--vision-model", "deepseek-flash", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --vision-base-url": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission, pointed at a closed local port",
		argv:    func(string) []string { return []string{"--vision-base-url", "http://127.0.0.1:1", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --reasoning-effort": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the level is checked against the settings list, then the credential refusal follows",
		argv:    func(string) []string { return []string{"--reasoning-effort", "low", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent", "reasoning"},
	},
	"gopi --thinking": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the ON|OFF spelling",
		argv:    func(string) []string { return []string{"--thinking", "OFF", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --hide-thinking": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the ON|OFF spelling",
		argv:    func(string) []string { return []string{"--hide-thinking", "ON", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --keep-thinking": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the ON|OFF spelling",
		argv:    func(string) []string { return []string{"--keep-thinking", "OFF", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --max-tokens": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: a bounded integer",
		argv:    func(string) []string { return []string{"--max-tokens", "16", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --context-window": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: a bounded integer",
		argv:    func(string) []string { return []string{"--context-window", "4096", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --timeout": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: 0 means unlimited, so a small bound keeps the probe inside cliContractTimeout",
		argv:    func(string) []string { return []string{"--timeout", "5", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --compact": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the ON|OFF spelling",
		argv:    func(string) []string { return []string{"--compact", "OFF", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --tool-timeout": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: a bounded integer",
		argv:    func(string) []string { return []string{"--tool-timeout", "5", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --bash-timeout": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: a bounded integer",
		argv:    func(string) []string { return []string{"--bash-timeout", "5", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --bash-idle": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: a bounded integer",
		argv:    func(string) []string { return []string{"--bash-idle", "5", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --raw-log": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the ON|OFF spelling",
		argv:    func(string) []string { return []string{"--raw-log", "OFF", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --db": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the DB path points INSIDE the sandbox (never the operator's ledger, Rule 32)",
		argv:    func(h string) []string { return []string{"--db", filepath.Join(h, "g6-probe.db"), "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --tmpdir": {
		verb: g6TopLevelFlagsVerb, wantExit: -1, note: "admission: the temp root points INSIDE the sandbox (Rule 22 — this file names no temp root of its own)",
		argv:    func(h string) []string { return []string{"--tmpdir", filepath.Join(h, "tmp"), "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --chat-ideal-geometry": {
		verb: g6TopLevelFlagsVerb, wantExit: -1,
		note:    "admission: the pre-verb spelling of the chat surface's ideal geometry — the run reaches the same credential refusal, which is what proves the flag was HOISTED into the knob's env representation rather than refused as unknown",
		argv:    func(string) []string { return []string{"--chat-ideal-geometry", "100x30", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --chat-surface-turns": {
		verb: g6TopLevelFlagsVerb, wantExit: -1,
		note: "admission: the pre-verb spelling carries a VALUE, and the run still reaches the credential refusal — the " +
			"knob's own resolver is the only thing that reads it (config.ChatSurfaceTurns), so a flag that reached no " +
			"channel would be invisible here instead of merely unconfigured. The value is INSIDE the accepted range on " +
			"purpose: an unusable one is reported and falls through by design, which is a different claim",
		argv:    func(string) []string { return []string{"--chat-surface-turns", "30", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},
	"gopi --memory-prefetch": {
		verb: g6TopLevelFlagsVerb, wantExit: -1,
		note: "admission: the flag is written BARE (no value), exactly as the help row's own example does, and the run " +
			"still reaches the credential refusal — which is the whole claim the row makes: `gopi --memory-prefetch chat` " +
			"arms the knob and leaves `chat` in argv (prefetch_boundEnv's conditional peek). A flag whose bare form ate the " +
			"next token would instead consume `--prompt` and never reach the guard",
		argv:    func(string) []string { return []string{"--memory-prefetch", "--prompt", "hi"} },
		wantAny: []string{"not set", "Error creating agent"},
	},

	// --- voice_input: the device + refusal faces are locally reachable --------

	"gopi tool voice_input --list-devices": {
		verb: toolnames.ToolVoiceInput, wantExit: 0,
		note:    "the device table prints even with no pulse/alsa answering — the unavailable line is part of the observation, not a failure",
		argv:    func(string) []string { return []string{toolnames.ToolVoiceInput, "--list-devices"} },
		wantAny: []string{"BACKEND", "DESCRIPTION"},
	},
	"gopi tool voice_input --client --list-devices": {
		verb: toolnames.ToolVoiceInput, wantExit: 0,
		note:    "the client face of the same probe: the table is the claim",
		argv:    func(string) []string { return []string{toolnames.ToolVoiceInput, "--client", "--list-devices"} },
		wantAny: []string{"BACKEND", "DESCRIPTION"},
	},
	// --- voice_input: the --audio-source seam (task 01a11f13) -------------------
	//
	// The form's FIRST REACHABLE STATE on a hermetic host is asserted, and the
	// reachable state is deliberately NOT "transcribe a recording": the sandbox
	// has no transcription endpoint, so the run stops there. What the case
	// proves is the thing the seam is FOR — the recording's bytes were read as
	// a capture (counted in the line below), and the run reached that point
	// WITHOUT opening a capture device: the fixture's PATH holds no recorder
	// stub at all, so a run that still enumerated would have stopped at the
	// device policy's own refusal first. The engine is pinned with the family
	// (g6VoiceCaptureEnv) so the state is the case's on every host.
	"gopi tool voice_input --client --audio-source FILE [--seconds N]": {
		verb: toolnames.ToolVoiceInput, wantExit: 1,
		note: "the --audio-source seam: a WAV of a recording is read as the capture (the byte count is asserted) with NO device opened, and the run's first local refusal is the missing transcriber — the seam's own line proves the source replaced the microphone",
		argv: func(h string) []string {
			return []string{toolnames.ToolVoiceInput, "--client",
				"--" + voiceFlagAudioSource, filepath.Join(h, g6VoiceAudioSourceFixtureName),
				"--seconds", g6VoiceAudioSourceSeconds}
		},
		env:     g6VoiceCaptureEnv,
		setup:   g6SeedVoiceAudioSourceFixture,
		wantAny: []string{"(no capture device is opened)", g6VoiceAudioSourceCaptured},
	},

	"gopi tool voice_input clip.ogg": {
		verb: toolnames.ToolVoiceInput, wantExit: 1,
		note: "the page's own refusal example, executed literally with a non-ogg-bytes file: the engine's decode set names wav/mp3/flac and refuses ogg. WHICH engine answers depends on the ENV — the cloud engine is the default, so on a host that chose nothing this file would reach doubao and be refused upstream there (or, with a key present, be UPLOADED), while the local runtime's decode set is reached only where a channel selects `local`. The case therefore names its engine (g6VoiceCaptureEnv pins local) and pins HOME with it, and what it pins is the FIRST REACHABLE STATE of that local run: the form was typed, it reached the verb, and it was REFUSED rather than silently accepted",
		argv: func(h string) []string {
			return []string{toolnames.ToolVoiceInput, filepath.Join(h, g6VoiceOggFixtureName)}
		},
		env:     g6VoiceCaptureEnv,
		setup:   g6SeedVoiceEngineFile(g6VoiceOggFixtureName, g6VoiceOggFixtureBody),
		wantAny: []string{"not in the engine's decode set", "doubao engine:"},
	},
	"gopi tool voice_input clip.pcm": {
		verb: toolnames.ToolVoiceInput, wantExit: 1,
		note: "the page's other refusal example: a bare PCM FILE cannot be sniffed, and the hint names --server --stdio as the way that works. Same engine pin as clip.ogg above (g6VoiceCaptureEnv, same HOME pin) — the local runtime's decode-set lines are asserted, and the cloud engine's own refusal stays accepted because a run that reached doubao really did refuse these bytes",
		argv: func(h string) []string {
			return []string{toolnames.ToolVoiceInput, filepath.Join(h, g6VoicePCMFixtureName)}
		},
		env:     g6VoiceCaptureEnv,
		setup:   g6SeedVoiceEngineFile(g6VoicePCMFixtureName, g6VoicePCMFixtureBody),
		wantAny: []string{"cannot read raw PCM", "wraps a bare 16 kHz mono f32 PCM stream", "doubao engine:"},
	},

	// --- voice_input: the --self device family (added with the 2026-10-01
	// inventory repair) ---------------------------------------------------------
	//
	// These three forms are advertised by BOTH the voice_input page and the act
	// page (the act page's addressing section shows the `activate a new display`
	// example and hands the reader `gopi tool voice_input --client --self`), and until
	// this task NEITHER page's row was in this table nor in g6BlockedForms — the
	// completeness gate's own "advertised but never typed" defect.
	//
	// Their happy path really does need a microphone (the transcript is what gets
	// typed into the window), so the claim is asserted to the FIRST REFUSAL the
	// form's own device policy produces. The device half is the case's: the
	// sandbox PATH gets a capture-tool stub set (g6SeedVoiceCaptureStubs) whose
	// enumerator lists ONE card and whose recorder then fails to open it — the
	// measured hotplug shape — so the refusal names the device by ID and says to
	// re-list. That is a statement about the ARGV and about the policy, both of
	// which are the binary's, with no microphone and no sound server involved.
	//
	// The --self half (typing the transcript into this process's window) needs a
	// transcript, so it is deliberately NOT reached here: it is the same X11 path
	// `gopi tool act type --self` exercises, which IS typed (with the display pinned
	// empty, see g6NoX11Env) two hundred lines up, and its own case covers the
	// addressing refusal (`--self --dry-run`).
	//
	// This family carries the engine pin with the rest of the family
	// (g6VoiceCaptureEnv → local) WITHOUT needing it, and the measurement is
	// what says so: these three cases PASSED on this host before the pin
	// existed, with the cloud engine as the host default and no key file. The
	// reason is the one the announcement rule already names — `--client` is
	// outside needsEngine (it transcribes nothing, so the engine is its
	// server's), and the device policy is reached without a credential check.
	// They are pinned anyway because the pin is a property of the shared env
	// helper, and splitting the helper into a pinned and an unpinned variant
	// would put the host-dependence back into whichever half lagged.
	// The remaining gap is recorded in devdoc/g6-help-surface-forwarded-display.md
	// (the inventory document is GENERATED and carries no prose of its own).

	"gopi tool voice_input --client --self": {
		verb: toolnames.ToolVoiceInput, wantExit: 1,
		note: "the device policy answers first: the stubbed enumerator lists one card, the stubbed recorder cannot open it, so the refusal names that device by ID",
		argv: func(string) []string {
			return []string{toolnames.ToolVoiceInput, "--" + voiceFlagClient, "--" + voiceFlagSelf}
		},
		env:     g6VoiceCaptureEnv,
		setup:   g6SeedVoiceCaptureStubs,
		wantAny: []string{g6VoiceDeviceRefusedMarker, g6VoiceRelistMarker},
	},
	"gopi tool voice_input --client --self --seconds 8": {
		verb: toolnames.ToolVoiceInput, wantExit: 1,
		note: "as above with the page's own duration: --seconds must be admitted and reach the same policy refusal (the length is spent recording, never on the device check)",
		argv: func(string) []string {
			return []string{toolnames.ToolVoiceInput, "--" + voiceFlagClient, "--" + voiceFlagSelf, "--" + voiceFlagSeconds, g6VoiceFixtureSeconds}
		},
		env:     g6VoiceCaptureEnv,
		setup:   g6SeedVoiceCaptureStubs,
		wantAny: []string{g6VoiceDeviceRefusedMarker, g6VoiceRelistMarker},
	},
	"gopi tool voice_input --client --self --seconds 8 --display :57": {
		verb: toolnames.ToolVoiceInput, wantExit: 1,
		note: "the page's own other-display example: --display must be admitted, and the device refusal still comes first because there is no transcript to type anywhere",
		argv: func(string) []string {
			return []string{
				toolnames.ToolVoiceInput, "--" + voiceFlagClient, "--" + voiceFlagSelf,
				"--" + voiceFlagSeconds, g6VoiceFixtureSeconds,
				"--" + voiceFlagDisplay, g6VoiceFixtureDisplay,
			}
		},
		env:     g6VoiceCaptureEnv,
		setup:   g6SeedVoiceCaptureStubs,
		wantAny: []string{g6VoiceDeviceRefusedMarker, g6VoiceRelistMarker},
	},

	// --- voice_input: the bare CAPTURE family (the page's own default shape)
	// ---------------------------------------------------------------------------
	//
	// `gopi tool voice_input` is the verb's DEFAULT shape — no file, no --server,
	// no --client, no --list-devices — and the page advertises it twice: in the
	// synopsis ("speak, read the transcript") and again as the `--seconds 5`
	// "a shorter window" example. Both rows were advertised but listed in
	// NEITHER map until this repair (the same "advertised but never typed"
	// defect the --self family above was added for), so the completeness gate
	// reddened on each.
	//
	// The happy path needs a microphone, exactly as the --self family's does, so
	// the claim is the FIRST LOCAL REFUSAL the capture shape's own device policy
	// produces: with the same stubbed PATH the --self cases use, the bare shape
	// resolves the system default, finds it unreachable (the stubbed recorder
	// refuses), falls through to the ONE card the stubbed enumerator lists, and
	// is refused naming that device by ID with the "re-list" advice. Measured by
	// running both argv against the real binary before writing them down.
	//
	// --seconds is asserted to be ADMITTED rather than to change the outcome:
	// the window is spent recording, so it is fixed before the device check runs.
	// The `--seconds 5` case therefore carries the page's own example value
	// (g6VoiceShortSeconds), not this host's default length.

	"gopi tool voice_input": {
		verb: toolnames.ToolVoiceInput, wantExit: 1,
		note:    "the verb's DEFAULT shape must reach the capture path's device policy (not the usage page): the stubbed enumerator's card is refused by ID and the advice names --list-devices. The engine is PINNED to local (g6VoiceCaptureEnv) and the local deployment is PLANTED (g6SeedVoiceLocalStubs) — without both, this form's first state on a host whose default engine is the cloud one, or whose profile holds no deployment, is a different refusal entirely, and the device policy is never reached (measured 2026-10-09, task 01a11e05)",
		argv:    func(string) []string { return []string{toolnames.ToolVoiceInput} },
		env:     g6VoiceCaptureEnv,
		setup:   g6SeedVoiceLocalStubs,
		wantAny: []string{g6VoiceDeviceRefusedMarker, g6VoiceRelistMarker},
	},
	"gopi tool voice_input --seconds 5": {
		verb: toolnames.ToolVoiceInput, wantExit: 1,
		note: "the page's \"a shorter window\" example: --seconds must be ADMITTED on the bare capture shape and reach the same policy refusal (the length is spent recording, never on the device check). Same engine + deployment pin as the bare form above: with a cloud default and no key, the form is refused at the credential check and the device policy — the thing --seconds is being admitted INTO — is never reached",
		argv: func(string) []string {
			return []string{toolnames.ToolVoiceInput, "--" + voiceFlagSeconds, g6VoiceShortSeconds}
		},
		env:     g6VoiceCaptureEnv,
		setup:   g6SeedVoiceLocalStubs,
		wantAny: []string{g6VoiceDeviceRefusedMarker, g6VoiceRelistMarker},
	},

	// --- worker: the help verb ----------------------------------------------

	"gopi worker -h|--help": {
		verb: CliVerbWorker, wantExit: 0,
		wantHelp: true, note: "the page promises `this help (claims nothing)`; -h must print it and touch no ledger",
		argv:    func(string) []string { return []string{CliVerbWorker, "-h"} },
		wantAny: []string{"GoPi worker"},
	},
}

// g6ArchiveArgv is the page's own example, verbatim: no --repo, because the
// page's archive examples carry none AND --repo does not reach this path (FOUND
// WHILE WRITING THIS: the page documents `--repo PATH  Repo to inspect/act on.
// Default: the repo of the cwd`, and the archive path ignores it — see
// devdoc/gopi-tools-maintenance-help-surface-inventory.md, "findings"). The
// cases therefore chdir into the fixture repo, which is what the page's own
// examples assume.
func g6ArchiveArgv(_ string, branch, reason string, extra ...string) []string {
	argv := []string{CliVerbArchive, branch, "--reason", reason}
	return append(argv, extra...)
}

// g6ArchiveSetup builds the fixture repo in the sandbox and makes it the probe's
// working directory (the page's examples are repo-relative by construction).
func g6ArchiveSetup(t *testing.T, home string) {
	t.Helper()
	g6FixtureRepo(t, home)
	t.Chdir(g6FixtureRepoPath(home))
}

// g6ScratchEnv points the sweep's scratch home INSIDE the probe's sandbox, so
// the "sweep now" forms can be executed for real without touching the operator's
// scratch tree or a peer's (Rule 27's spirit: never make a shared resource the
// subject).
// --- the profile-root fixture (the scratch-gc profile arm) -------------------

// The three child names the profile-root cases plant. They are constants
// because the argv builders and the disk assertions must name the SAME children
// (Rule 1): the declared name is a real registry name, the undeclared one is a
// word no family of ours matches, and the residue one is the measured shape of
// the family this arm exists for (a lowercase-hex token, as os.MkdirTemp emits).
const (
	g6ProfileDeclaredName   = "settings.json"
	g6ProfileUndeclaredName = "g6-undeclared"
	g6ProfileResidueName    = "evid-deadbeef"
	g6ProfileResidueAge     = 30 * time.Hour
)

// g6ProfileRootEnv pins the profile root inside the case's own sandbox: HOME and
// GOPI_HOME are the same directory, so config resolves <GOPI_HOME>/.gopi and the
// arm reports the fixture rather than the operator's /home/user/.gopi. GOPI_SETTINGS is
// pointed at the fixture's own settings file for the same reason.
func g6ProfileRootEnv(home string) []string {
	return []string{
		"GOPI_HOME=" + home,
		"GOPI_SETTINGS=" + filepath.Join(home, config.ProfileDirName, g6ProfileDeclaredName),
	}
}

// g6SeedProfileRootFixture plants the three children. The residue child carries
// a PAYLOAD that is backdated with it, so "removed=1" is a statement about bytes
// that really existed rather than about an empty directory.
func g6SeedProfileRootFixture(t *testing.T, home string) {
	t.Helper()
	root := filepath.Join(home, config.ProfileDirName)
	if err := os.MkdirAll(root, scratch.DirPerm); err != nil {
		t.Fatalf("seed profile root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, g6ProfileDeclaredName), []byte("{}"), scratch.FilePerm); err != nil {
		t.Fatalf("seed declared child: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, g6ProfileUndeclaredName), scratch.DirPerm); err != nil {
		t.Fatalf("seed undeclared child: %v", err)
	}
	residue := filepath.Join(root, g6ProfileResidueName)
	if err := os.Mkdir(residue, scratch.DirPerm); err != nil {
		t.Fatalf("seed residue child: %v", err)
	}
	payload := filepath.Join(residue, "bin-gopi-before")
	if err := os.WriteFile(payload, []byte("evidence"), scratch.FilePerm); err != nil {
		t.Fatalf("seed residue payload: %v", err)
	}
	stamp := time.Now().Add(-g6ProfileResidueAge)
	for _, p := range []string{residue, payload} {
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatalf("backdate %s: %v", p, err)
		}
	}
}

func g6ScratchEnv(home string) []string {
	return []string{scratch.HomeEnvName + "=" + filepath.Join(home, "scratch")}
}

// g6DeclaredTreesBudget is the wall clock the declared-trees arm is given over
// the fixture it OWNS (see g6DeclaredTreesEnv). The arm's duration is then a
// property of that fixture rather than of the host: measured inside this suite,
// the full form takes 2.3s and the report-only form 1.0s on a host at load ~15
// with ~400 processes. This bound is deliberately ABOVE both and still a bound —
// the arm tripping it is the finding the timeout message names, and the
// headroom is there because the per-tree /proc pass scales with the HOST's
// process table, which is not the case's to control.
const g6DeclaredTreesBudget = 10 * time.Second

// g6DeclaredTreesEnv sandboxes EVERY declared tree the `trees` arm reads, and is
// the whole reason those two forms are locally decidable. The arm's own note used
// to say "the trees are the host's own" and asserted nothing but the verb's
// presence, which made its DURATION a property of the operator's trees (a build
// cache of 32MB keys, a ~75k-entry sessions tree) under a 15s ceiling shared by
// every form in this table — a slow arm then surfaced as "TIMEOUT — argv fell
// through to the agent loop", i.e. a routing bug that did not exist.
//
// Two env layers, because the trees resolve from two different knobs and only the
// fixture makes them agree with the sandbox:
//
//   - HOME is the sandbox. Both scratch's home base and worktreeroots'
//     DefaultRoot fall back to $HOME, so pinning it (rather than overriding the
//     worktree root with GOPI_WORKTREE_ROOT) makes the build-cache tree resolve
//     through the OPERATOR's own rule — just not to the operator's tree. HOME is
//     appended last on purpose: os/exec passes a duplicated variable through and
//     the child keeps the LAST spelling, which is the fixture (measured, not
//     assumed).
//   - GOPI_HOME redirects the gopi data root, so raw (GOPI_HOME/.gopi/raw) and
//     session-raw (GOPI_HOME/.gopi/sessions) are resolved by internal/config's own
//     resolvers — never a second spelling here.
//
// GOTMPDIR pins the Go work tree, and it is the ONE tree the fixture SEEDS: the
// other three are left unconfigured inside the sandbox (their roots do not exist),
// which is both the strictest proof that no operator tree is read and the reason
// the arm's per-configured-tree /proc pass stays at one.
func g6DeclaredTreesEnv(home string) []string {
	return append(g6ScratchEnv(home),
		"GOPI_HOME="+home,
		"HOME="+home,
		scratch.GoTmpDirEnvName+"="+filepath.Join(home, g6GoTmpDirName))
}

// g6SeedDeclaredTreesFixture plants ONE aged entry in the one declared tree the
// fixture makes CONFIGURED (the Go work tree), so the full form has real work to
// do and the output carries the aged path the case asserts on.
//
// ONE tree, deliberately. The arm's own cost is not the trees' CONTENTS: each
// configured tree pays one full /proc pass (scratch.LiveHolders reads every
// process's cwd, exe, maps and fds), so a fixture that pins all four trees makes
// every run of this case pay four of them — measured at ~8s on a loaded host with
// ~400 processes. The minimal sample is therefore one configured tree; the other
// three stay UNCONFIGURED inside the sandbox (their roots do not exist), which is
// what keeps the case off the operator's trees AND off the process table.
//
// The entry mirrors the writer's own shape rather than inventing one: the
// GOTMPDIR child carries scratch.GoBuildDirEntryPrefix (the namespace predicate
// the sweep applies), and it is backdated past scratch.GoTmpStaleAge, so the
// CASE's own promise (removed by the full form, kept by --dry-run) is decided by
// the age rule with no knob exported.
func g6SeedDeclaredTreesFixture(t *testing.T, home string) {
	t.Helper()
	path := g6GoTmpFixturePath(home)
	if err := os.MkdirAll(filepath.Dir(path), scratch.DirPerm); err != nil {
		t.Fatalf("seed declared-tree fixture %s: %v", path, err)
	}
	if err := os.MkdirAll(path, scratch.DirPerm); err != nil {
		t.Fatalf("seed declared-tree fixture %s: %v", path, err)
	}
	stamp := time.Now().Add(-(scratch.GoTmpStaleAge + g6ExplainBackdate))
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("backdate declared-tree fixture %s: %v", path, err)
	}
}

// --- the sessions-tree family census fixture --------------------------------
//
// The census reads the SESSIONS TREE, which resolves through the profile root
// (GOPI_HOME) exactly as the declared trees do — so the fixture pins GOPI_HOME
// into the sandbox and seeds ONE session container holding the artifact families
// the session writers produce. The case then reads a census of a tree it owns,
// which is the whole reason the arm is locally decidable rather than a walk of
// the operator's 250k-entry tree.
const (
	// g6FamiliesRoleToken is the arm label every census line carries.
	g6FamiliesRoleToken = "role=" + scratchGCFamiliesRole
	// g6FamiliesRecordToken is the family the fixture's session record lands on —
	// the assertion that a SHAPE was classified, not merely counted.
	g6FamiliesRecordToken = "family=" + sessionlayout.FamilySessionRecord
	// g6FamiliesSummaryToken is the summary line's marker.
	g6FamiliesSummaryToken = "shapes="
	// g6FamiliesFixtureSession is the container the fixture seeds.
	g6FamiliesFixtureSession = "01a0f4f9-baf5-7f13-a707-d05a68ea12e4"
	// g6FamiliesFixtureRawLog is a per-run raw HTTP log's name.
	g6FamiliesFixtureRawLog = g6FamiliesFixtureSession + ".raw.log"
	// g6FamiliesFixtureCrashDump is a per-run crash dump's name.
	g6FamiliesFixtureCrashDump = g6FamiliesFixtureSession + ".dump.json"
	// g6FamiliesSiblingDumpTree is the SIBLING tree the selector cases census.
	// It is the registry's own name (Rule 13: never a second literal).
	g6FamiliesSiblingDumpTree = sessionlayout.SiblingTreeDumps
	// g6FamiliesSiblingDumpToken is the `tree=` field a sibling census line
	// carries — the assertion that the SELECTOR reached the tree it names,
	// rather than silently censusing the sessions tree.
	g6FamiliesSiblingDumpToken = "tree=" + g6FamiliesSiblingDumpTree
	// g6FamiliesBoundToken is the decision field the sibling dump family
	// carries: the ONE `bound:` decision of the whole sibling registry.
	g6FamiliesBoundToken = "decision=" + string(sessionlayout.DecisionBound)
	// g6FamiliesFixtureSiblingDump is a request dump's own timestamp name — the
	// shape the bound family claims.
	g6FamiliesFixtureSiblingDump = "20261001-104445.dump.json"
	// g6FamiliesSessionsTree is the SESSIONS TREE's selector name (the arm's own
	// default subject; spelled here from the constant the arm selects on).
	g6FamiliesSessionsTree = scratchGCFamiliesSessionsTree
)

// g6SeedSessionsFamilyFixture plants one session container with the artifact
// families the session writers produce, so the census has a classified subject
// and the case's claim ("a shape is named with its family") is read off real
// entries.
func g6SeedSessionsFamilyFixture(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, g6SessionDirRel, g6FamiliesFixtureSession)
	if err := os.MkdirAll(dir, scratch.DirPerm); err != nil {
		t.Fatalf("seed family-census fixture %s: %v", dir, err)
	}
	files := []string{
		sessionlayout.RecordName,
		g6FamiliesFixtureRawLog,
		g6FamiliesFixtureCrashDump,
		sessionlayout.MetadataFileName,
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), scratch.FilePerm); err != nil {
			t.Fatalf("seed family-census fixture %s: %v", filepath.Join(dir, name), err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, sessionlayout.AttachmentsDirName), scratch.DirPerm); err != nil {
		t.Fatalf("seed family-census attachments: %v", err)
	}
}

// g6SeedSiblingDumpFixture plants the operator PROFILE's sibling dump tree inside
// the sandbox home, with ONE entry of the bound family: the census selector cases
// then read a census of a tree they OWN, and the operator's own `dumps/` is never
// walked. The tree's name is the registry's (a sibling registry name, resolved by
// the same seam the arm uses), so the fixture cannot seed a directory the arm
// would not look at.
func g6SeedSiblingDumpFixture(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, config.ProfileDirName, g6FamiliesSiblingDumpTree)
	if err := os.MkdirAll(dir, scratch.DirPerm); err != nil {
		t.Fatalf("seed sibling dump fixture %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, g6FamiliesFixtureSiblingDump), []byte("{}\n"), scratch.FilePerm); err != nil {
		t.Fatalf("seed sibling dump fixture %s: %v", filepath.Join(dir, g6FamiliesFixtureSiblingDump), err)
	}
}

// g6SessionDirRel is the sessions directory under the profile root, spelled from
// the package that owns the name (never a second literal — Rule 13).
var g6SessionDirRel = filepath.Join(config.ProfileDirName, config.SessionsDirName)

// --- the shot family's fixture: the BROWSER is injected, never the host's -----
//
// WHY THIS FIXTURE EXISTS (measured 2026-09-27). The `gopi tool shot --url URL` arm
// was the one entry in this table that launched the HOST's browser: ~5.6s on a
// quiet host and 28.4s at load 14.0, i.e. its wall clock was the host's Chromium
// cold start and nothing this case owns — against a ceiling (cliContractTimeout,
// 15s) shared with every usage print in the table. So the arm went red as
// "TIMEOUT — the argv never printed its verb's output", a ROUTING verdict, while
// the argv HAD reached its verb and the browser was merely slow (the same run's
// `--self` sibling, same shape, host-launch-bound too, did not trip it that
// time). The bound and the fixture were the defect; the verb was not.
//
// The remedy follows the declared-trees precedent (g6DeclaredTreesEnv): the arm
// OWNS its fixture. GOPI_SHOT_CHROME — the verb's own documented override, which
// internal/shot reads FIRST, before the playwright cache and PATH — is pointed at
// a stub this case writes. The launch, the DevTools handshake and the CDP
// discovery are therefore still exercised for real, over the help's own argv, but
// against a browser whose cost is the case's: measured 2.0-2.2s, and flat across
// load (the stub's fixed cost is launchChromeUserUnit's transient-unit attempt,
// systemdRunStartTimeout, which times out on this host because no user manager
// answers, plus the CDP discovery refusal — both properties of the fixture).
//
// WHAT IT DELIBERATELY DOES NOT COVER: PIXELS. A stub cannot render, so these
// arms assert the LAUNCH and the first refusal, and the real-browser render — a
// PNG of a real page, and the chrome-error: page of an unreachable URL — stays
// where it belongs: internal/shot's TestCaptureRealChromeUnreachableURL, which
// declares heavytest.CriterionDeviceExec and is therefore off the merge/verify
// path. A real browser on the smoke path is exactly what made this arm
// host-load-dependent; that half is not traded back.
const (
	// g6ShotStubShell is the interpreter the injected browser is written for.
	g6ShotStubShell = "/bin/sh"
	// g6ShotStubMode is the stub's file mode: it must be EXECUTABLE, because
	// internal/shot rejects a non-executable override at discovery time.
	g6ShotStubMode = 0o700
	// g6ShotPNGName is the --out leaf the shot arms point INSIDE the sandbox
	// (Rule 30: the help's own flag, so no probe can leave a PNG in the checkout).
	g6ShotPNGName = "shot.png"
	// g6ShotStubChromeName is the injected browser's file name in the case's home.
	g6ShotStubChromeName = "g6-shot-chrome-stub"
	// g6ShotStubArgvFile is where the stub records the argv the verb handed it,
	// BESIDE ITSELF (so the record lands in the case's sandbox home). The arms
	// assert on this file: it can exist only if the verb really EXECUTED the
	// browser it resolved, which is the launch half of the form's claim.
	g6ShotStubArgvFile = "g6-shot-stub-argv.txt"
	// g6ShotStubDevToolsToken is the stub's stderr handshake, in the exact shape
	// internal/shot's wsURLPattern requires (the trailing UUID segment is
	// mandatory). Its port listens to nothing, so the run proceeds into CDP
	// discovery and fails there with a typed refusal — the same shape the real
	// browser produced for the test URL, at a fraction of its cost. The SHAPE is
	// not re-stated as a second regexp anywhere: the arms' own green runs are
	// what pin it, and a stub that broke it would fail the arm.
	g6ShotStubDevToolsToken = "DevTools listening on ws://127.0.0.1:1/devtools/browser/00000000-0000-0000-0000-000000000000"
	// g6ShotHeadlessFlag is the first flag of the headless argv internal/shot
	// builds; the arms assert it appears in the argv the stub recorded, so the
	// launch evidence is the VERB's own invocation rather than any exec at all.
	// One definition, shared by both arms (Rule 14).
	g6ShotHeadlessFlag = "--headless=new"
)

// g6ShotStubChromePath is the injected browser's path inside one case's home.
func g6ShotStubChromePath(home string) string {
	return filepath.Join(home, g6ShotStubChromeName)
}

// g6ShotStubChromeBody is the stand-in browser: it records the argv it was
// handed (the launch evidence), prints the DevTools line gopi's handshake reads,
// and exits. Nothing listens on the advertised port, so the run continues into
// CDP discovery and fails there — the refusal is the arm's observed exit state.
func g6ShotStubChromeBody() string {
	return "#!" + g6ShotStubShell + "\n" +
		"printf '%s\\n' \"$@\" > \"$(dirname \"$0\")/" + g6ShotStubArgvFile + "\"\n" +
		"echo '" + g6ShotStubDevToolsToken + "' 1>&2\n" +
		"exit 0\n"
}

// g6SeedShotStubChrome writes that stub into the case's home.
func g6SeedShotStubChrome(t *testing.T, home string) {
	t.Helper()
	path := g6ShotStubChromePath(home)
	if err := os.WriteFile(path, []byte(g6ShotStubChromeBody()), g6ShotStubMode); err != nil {
		t.Fatalf("seed the injected browser %s: %v", path, err)
	}
}

// g6ShotEnv injects that browser through the verb's own override, so the arm
// never resolves the host's Chromium (playwright cache or PATH) at all.
func g6ShotEnv(home string) []string {
	return []string{shot.ChromeEnvName + "=" + g6ShotStubChromePath(home)}
}

// g6SeedAgedScratchRoot seeds ONE suite run root in the case's sandbox scratch
// home, backdated by g6ExplainBackdate. The name comes from scratch's own marker
// constant (Rule 14), so it stays a root the sweep recognizes as its own.
// g6SeedAgedScratchRootRel names the sandbox-relative path g6SeedAgedScratchRoot
// creates, so a case can assert on the DISK that the entry survived (Rule 31: the
// verb's account of what it did is a claim; the filesystem is the reading).
func g6SeedAgedScratchRootRel() string {
	return filepath.Join("scratch", scratch.SuiteRunMarker+g6FixtureAbsentToken)
}

func g6SeedAgedScratchRoot(t *testing.T, home string) {
	t.Helper()
	path := filepath.Join(home, g6SeedAgedScratchRootRel())
	if err := os.MkdirAll(path, scratch.DirPerm); err != nil {
		t.Fatalf("seed scratch root %s: %v", path, err)
	}
	stamp := time.Now().Add(-g6ExplainBackdate)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("backdate %s: %v", path, err)
	}
}

// g6WriteFile is a one-line fixture writer.
func g6WriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

// TestToolsMaintenanceHelpSurfaceFormsAreTyped drives every case: the REAL
// binary, the help page's OWN argv, an assert on the observed state. The
// subtest name is the advertised form, so a failure names the surface an
// operator would have typed.
func TestToolsMaintenanceHelpSurfaceFormsAreTyped(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := gopiTestBinPath(t)
	for form, c := range g6Cases {
		form, c := form, c
		if c.argv == nil {
			continue // a placeholder entry, kept out of the table by the gate
		}
		t.Run(form, func(t *testing.T) {
			home := t.TempDir()
			if c.setup != nil {
				c.setup(t, home)
			}
			args := c.argv(home)
			if args == nil {
				// The stdin invocation shape: no positional at all.
				args = []string{"--prompt", ""}
			}
			out, code := g6ProbeEnvBounded(t, home, bin, c.env, c.stdin, g6CaseBudget(c), args...)
			if !c.wantHelp {
				g6AssertReachedVerb(t, c.verb, out)
			}
			if c.wantExit >= 0 && code != c.wantExit {
				t.Errorf("`gopi %s`: %s", strings.Join(args, " "), g6FailureText(
					fmt.Sprintf(g6ExpectationExitFmt, code, c.wantExit), c.note, out))
			}
			if c.wantPNG != nil && code == 0 {
				g6AssertPNG(t, c.wantPNG(home))
			}
			g6AssertFixtureState(t, home, c, args)
			g6AssertTokens(t, args, out, home, c.wantAny, false, c.note)
			g6AssertTokens(t, args, out, home, c.wantAll, true, c.note)
		})
	}
}

// TestG6VoiceCasesPinTheirEngine is the regression guard for the 2026-10-09
// defect (task 01a11e05): four voice_input cases in this table assert states of
// the LOCAL engine, and they used to inherit the engine from the HOST — so on a
// host whose default engine is the cloud one (config.AudioEngineDefault) and
// which holds no key file, all four stopped at `doubao engine needs an API key`
// and never reached the state they claim.
//
// The guard drives the REAL binary twice over ONE fixture set, and the two arms
// differ in exactly the thing the fix added:
//
//	arm 1  no engine pin  → the cloud engine runs, the deployment planted in the
//	                        sandbox is never opened, and the refusal is the
//	                        credential one — which is the RED state this case
//	                        family used to report as a failure of the FIXTURE.
//	arm 2  g6VoiceCaptureEnv (the pin) → the same fixtures reach the device
//	                        policy the cases assert.
//
// It is a guard rather than a note because the pin is load-bearing and nothing
// else would notice its removal: the four cases would go on passing on a host
// whose operator happens to set GOPI_VOICE_ENGINE=local, and the table would
// silently stop testing anything on a cloud-default host.
func TestG6VoiceCasesPinTheirEngine(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	g6SeedVoiceLocalStubs(t, home)

	// Arm 1 — HOME and the stub PATH only: nobody named an engine.
	unpinned := func(h string) []string {
		return []string{
			"HOME=" + h,
			"PATH=" + filepath.Join(h, g6VoiceBinDirName) + string(os.PathListSeparator) + os.Getenv("PATH"),
		}
	}
	out, code := g6ProbeEnvBounded(t, home, bin, unpinned, "", cliContractTimeout, toolnames.ToolVoiceInput)
	if code != 1 || !strings.Contains(out, voiceDoubaoKeyHint) {
		t.Errorf("with no engine named, the default engine must answer first (exit 1, %q) — got exit %d:\n%s",
			voiceDoubaoKeyHint, code, g6Head(out))
	}

	// Arm 2 — the case family's own env: the same fixtures now reach the device
	// policy. Both halves are asserted, because "the marker appeared" without
	// the first arm would also pass on a host that pinned the engine globally.
	out, code = g6ProbeEnvBounded(t, home, bin, g6VoiceCaptureEnv, "", cliContractTimeout, toolnames.ToolVoiceInput)
	if code != 1 || !strings.Contains(out, g6VoiceDeviceRefusedMarker) {
		t.Errorf("with %s=%s the planted deployment must carry the run to the device policy (exit 1, %q) — got exit %d:\n%s",
			config.AudioEngineEnv, config.AudioEngineLocal, g6VoiceDeviceRefusedMarker, code, g6Head(out))
	}
}

// g6CaseBudget resolves a case's wall clock: the case's own budget, or the shared
// bound when it declares none.
//
// g6ArmBudgetOverrideEnv overrides ONLY a case that declared its own budget (the
// arms, never the shared bound). It exists so the timeout diagnostic is
// red-provable WITHOUT manufacturing load on the host (Rule 27 forbids that):
// set it under the arm's measured wall clock and the arm trips a bound that is
// genuinely its own, so the message can be read in place. The unset default is
// the case's constant, and this function is the only reader.
func g6CaseBudget(c g6Case) time.Duration {
	if c.budget <= 0 {
		return cliContractTimeout
	}
	if v := strings.TrimSpace(os.Getenv(g6ArmBudgetOverrideEnv)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return c.budget
}

// g6ArmBudgetOverrideEnv is the diagnostic hook above: an arm's own budget, for
// one run, from the environment.
const g6ArmBudgetOverrideEnv = "GOPI_G6_ARM_BUDGET"

// TestG6TimeoutDiagnosisNamesTheShape pins the SPLIT the diagnostic exists for,
// against the two output shapes measured on the real binary: a probe that
// reached its verb prints the arm's own line (`role=…`), and a probe whose argv
// fell through to the agent loop prints no verb output at all (it stops at the
// client constructor). The names are asserted, because the wording IS the
// product here: a slow arm filed as a routing bug is exactly the misdiagnosis
// this split removes.
func TestG6TimeoutDiagnosisNamesTheShape(t *testing.T) {
	const bound = 15 * time.Second
	cases := []struct {
		name string
		out  string
		want []string
		not  []string
	}{
		{
			name: "reached its verb",
			out: "scratch-gc " + scratchGCTreesRoleToken +
				" home= removed=0 stale=24h0m0s disabled=false tree=go-tmp bytes=0 dry-run=true\n",
			want: []string{"SLOW arm", bound.String(), g6GoalSlowArm},
			not:  []string{"AGENT-LOOP FALLTHROUGH"},
		},
		{
			name: "fell through to the agent loop (verbatim capture: stdout empty, stderr carries the refusal)",
			out:  "Error creating agent: failed to create DeepSeek Claude client: deepseek: ANTHROPIC_AUTH_TOKEN not set\n",
			want: []string{"NO VERB OUTPUT", g6GoalNone, "FELL THROUGH to the agent loop"},
			not:  []string{"SLOW arm"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := g6TimeoutDiagnosis(tc.out, bound, bound)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("diagnosis does not name %q:\n%s", want, got)
				}
			}
			for _, not := range tc.not {
				if strings.Contains(got, not) {
					t.Errorf("diagnosis claims %q, which this shape is not:\n%s", not, got)
				}
			}
		})
	}
}

// g6AssertTokens asserts a case's token claims against the verb's output:
// all=false is the OR direction (at least one), all=true the conjunction (every
// one, and the failure NAMES the missing conjunct).
func g6AssertTokens(t testing.TB, args []string, out, home string, wants []string, all bool, note string) {
	t.Helper()
	if len(wants) == 0 {
		return
	}
	// g6TokenPresent does the `{{home}}` expansion in ONE place for both
	// directions, so an OR case and an AND case can never disagree about which
	// token they are asserting.
	present := func(want string) bool {
		return strings.Contains(out, strings.ReplaceAll(want, "{{home}}", home))
	}
	if all {
		// The conjunction: EVERY token, and the failure NAMES the missing ones.
		// Naming them is the half that makes the red proof readable — "not every
		// one of [...] appeared" would leave the reader to guess WHICH conjunct
		// the mutation dropped, which is exactly the reading a falsifier exists to
		// hand over.
		missing := make([]string, 0, len(wants))
		for _, want := range wants {
			if !present(want) {
				missing = append(missing, want)
			}
		}
		if len(missing) > 0 {
			t.Errorf("`gopi %s`: %s", strings.Join(args, " "), g6FailureText(
				fmt.Sprintf(g6ExpectationAllFmt, len(missing), len(wants), missing), note, out))
		}
		return
	}
	// wantAny keeps its OR-semantics unchanged: at least one.
	for _, want := range wants {
		if present(want) {
			return
		}
	}
	t.Errorf("`gopi %s`: %s", strings.Join(args, " "), g6FailureText(
		fmt.Sprintf(g6ExpectationAnyFmt, wants), note, out))
}

// The three parts of a token-assertion failure, each LABELLED (bug 01a13207).
// They used to be printed as one run-on line, and a reader (the author of that
// bug row, who spent a whole turn chasing a string the binary never printed)
// took the case's wantAny — and the author's note — for the command's output.
// See cli_tools_maintenance_help_surface_failure_text_test.go for the measured
// incident and the guard that pins this shape.
//
// The labels are the whole point: the expectation (the case's wantAny/wantAll)
// and the note are the CASE AUTHOR's words, while the output is the only part
// of the message the invoked binary produced. A failure that does not say which
// is which sends its reader to look for a string that was never printed.
const (
	g6ExpectationLabel   = "EXPECTATION ("
	g6ExpectationAnyFmt  = g6ExpectationLabel + "this case's wantAny, OR-semantics — at least one of these tokens must appear): none of %q appeared"
	g6ExpectationAllFmt  = g6ExpectationLabel + "this case's wantAll, AND-semantics — every one required): %d of %d token(s) never appeared, missing %q"
	g6ExpectationExitFmt = g6ExpectationLabel + "this case's wantExit): exited %d, expected %d"
	g6AuthorNoteLabel    = "AUTHOR'S NOTE (a description of the CASE, NOT this command's stdout): "
	g6NoAuthorNote       = "AUTHOR'S NOTE: (this case declares none)"
	g6ActualOutputLabel  = "ACTUAL OUTPUT of the command driven above: "
)

// g6FailureText renders the three parts in one fixed order — expectation, the
// author's note, then the command's own output — with every part labelled, so
// the reader of a failure can tell them apart without knowing this file.
func g6FailureText(expected, note, out string) string {
	var b strings.Builder
	b.WriteString(expected)
	b.WriteString("\n")
	if note == "" {
		b.WriteString(g6NoAuthorNote)
	} else {
		b.WriteString(g6AuthorNoteLabel)
		b.WriteString(note)
	}
	b.WriteString("\n")
	b.WriteString(g6ActualOutputLabel)
	b.WriteString(g6Head(out))
	return b.String()
}

// g6AssertFixtureState reads the case's own promise off the DISK: a declared
// path that the verb said it removed must be gone, and one it must leave alone
// must still be there. A verb's account of what it did is a claim (Rule 31), and
// for a sweep the claim is exactly the thing worth checking — "removed=1" on a
// file that is still there is the shape this catches.
func g6AssertFixtureState(t *testing.T, home string, c g6Case, args []string) {
	t.Helper()
	for _, rel := range c.wantGone {
		path := filepath.Join(home, rel)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("`gopi %s`: %s should have been swept, but it is still there (err=%v)", strings.Join(args, " "), path, err)
		}
	}
	for _, rel := range c.wantSurvives {
		path := filepath.Join(home, rel)
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("`gopi %s`: %s must survive the sweep, but it is gone: %v", strings.Join(args, " "), path, err)
		}
	}
	for rel, want := range c.wantContains {
		path := filepath.Join(home, rel)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("`gopi %s`: %s carries the fixture's own record, but it is unreadable: %v", strings.Join(args, " "), path, err)
			continue
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("`gopi %s`: %s does not carry %q — the record exists but says something else:\n%s",
				strings.Join(args, " "), path, want, g6Head(string(b)))
		}
	}
}

// g6ProbeEnv is g6Probe with extra environment entries (a case's own knobs).
func g6ProbeEnv(t *testing.T, home, bin string, env func(string) []string, stdin string, args ...string) (string, int) {
	t.Helper()
	return g6ProbeEnvBounded(t, home, bin, env, stdin, 0, args...)
}

// g6ProbeEnvBounded is g6ProbeEnv with the case's own wall-clock budget (0 =
// cliContractTimeout). The budget is the case's, so a slow-but-correct arm
// fails against its OWN number with a message that says so.
func g6ProbeEnvBounded(t *testing.T, home, bin string, env func(string) []string, stdin string, budget time.Duration, args ...string) (string, int) {
	t.Helper()
	cmd := g6Command(bin, args...)
	cmd.Env = cliContractEnv(home)
	if env != nil {
		cmd.Env = append(cmd.Env, env(home)...)
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return g6RunCommand(t, cmd, args, budget)
}

// g6Git builds a git command for the fixture repo.
func g6Git(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd
}

// g6AssertPNG checks the bytes a render claim produced: a real PNG signature. A
// render form that exits 0 has CLAIMED the file is there, so the claim is read
// off the file rather than off the exit code (Rule 31 — the claimed effect must
// be visible afterwards).
func g6AssertPNG(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("the form exited 0 but %s is not readable: %v — a render claiming success must leave the file", path, err)
		return
	}
	if len(b) < 8 || string(b[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Errorf("%s is not a PNG (%d bytes, first 8: %q)", path, len(b), b[:min(8, len(b))])
	}
}

// g6FixtureRepo builds a real one-commit git repo in the sandbox with the three
// branch names the archive page's own examples use, so those examples are
// executed LITERALLY rather than being rewritten into something easier to pass.
func g6FixtureRepo(t *testing.T, home string) string {
	t.Helper()
	repo := filepath.Join(home, g6FixtureRepoName)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir fixture repo: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := g6Git(t, repo, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", ".")
	run("config", "user.email", "g6@test.invalid")
	run("config", "user.name", "g6 fixture")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	run("add", "f.txt")
	run("commit", "-qm", "fixture")
	for _, b := range []string{"feat/old-idea", "fix/broken", "feat/forgotten"} {
		run("branch", b)
	}
	return repo
}
