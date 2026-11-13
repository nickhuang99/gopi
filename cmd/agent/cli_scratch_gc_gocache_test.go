package main

// cli_scratch_gc_gocache_test.go — the gocache arm driven through the REAL binary.
//
// The subject is a CONTROLLED cache directory (`--path`), never the operator's
// ~/.cache/go-build: the machinery lives in internal/scratch and its unit tests
// cover the arithmetic, but what only this file can show is that the FLAG survives
// main()'s argument handling, that the arm reports the numbers it promises, that
// it deletes NOTHING without `--trim`, and that a `--trim` on a disabled sweep is
// withheld AND SAID SO (Rule 15: an operator who asked for a reclaim must learn it
// did not happen).
//
// Ages are set with os.Chtimes (Rule 27: no test generates load, and none waits).

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// gocacheArmEntryBytes is the size of one fixture cache entry's DATA file. It is
// deliberately ABOVE the invocation's bound (`--max-gib 1`), because a trim is only
// meaningful against a cache that is OVER its bound: the arm's target is the bound,
// so a cache already inside it is correctly left alone — and a test that asserted
// removals on an under-bound fixture would be asserting a bug.
//
// The bytes are allocated SPARSELY (truncate, not write): the measurement reads
// file SIZE, which is exactly what a real cache entry costs, while the fixture
// allocates no device blocks — the point is to exercise the arithmetic, not to
// generate I/O (Rule 27).
const gocacheArmEntryBytes = 600 << 20

// gocacheArmIndexBytes is the size of one entry's INDEX file (`<hex>-a`). It is
// small on purpose — an index record is ~270 bytes in the real toolchain — so a
// fixture that forgot the pair would be visibly different from one that has it.
const gocacheArmIndexBytes = 512

// gocacheArmEntryID renders a fixture entry id in the toolchain's own shape: 64
// lowercase hex characters. The id is derived from a single digit so the call
// sites read as "entry 1", "entry 2", … and no literal is retyped (Rules 1/13).
func gocacheArmEntryID(t *testing.T, digit string) string {
	t.Helper()
	if len(digit) != 1 {
		t.Fatalf("gocacheArmEntryID: %q is not a single character", digit)
	}
	return strings.Repeat(digit, scratch.ToolchainCacheEntryIDHexLen)
}

// gocacheArmFixture builds a controlled toolchain cache at the toolchain's own
// grain: entries are `<hex>-a` / `<hex>-d` PAIRS, and one shard is LIVE-STYLE (a
// cold entry beside a fresh one) — the state every shard of a still-being-written
// cache is in, and the state the old shard grain reclaimed nothing from.
//
// It returns the directory and the byte totals the assertions use, both at the
// entry grain.
func gocacheArmFixture(t *testing.T) (dir string, coldBytes, freshBytes int64) {
	t.Helper()
	dir = t.TempDir()
	// ab/ — two COLD entries (48h/50h), so the whole shard is reclaimable.
	writeGocacheArmEntry(t, filepath.Join(dir, "ab"), gocacheArmEntryID(t, "1"), gocacheArmEntryBytes, 48*time.Hour)
	writeGocacheArmEntry(t, filepath.Join(dir, "ab"), gocacheArmEntryID(t, "2"), gocacheArmEntryBytes, 50*time.Hour)
	// cd/ — LIVE-STYLE: one cold entry (40h) beside one fresh entry (now). The
	// SHARD grain sees a fresh shard and reclaims nothing here; the ENTRY grain
	// takes the cold pair and keeps the fresh one.
	writeGocacheArmEntry(t, filepath.Join(dir, "cd"), gocacheArmEntryID(t, "3"), gocacheArmEntryBytes, 40*time.Hour)
	writeGocacheArmEntry(t, filepath.Join(dir, "cd"), gocacheArmEntryID(t, "4"), gocacheArmEntryBytes, 0)
	for _, book := range []string{"README", "trim.txt", "log.txt", "CACHEDIR.TAG"} {
		if err := os.WriteFile(filepath.Join(dir, book), []byte(book+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", book, err)
		}
	}
	cold := int64(3 * (gocacheArmIndexBytes + gocacheArmEntryBytes))
	fresh := int64(gocacheArmIndexBytes + gocacheArmEntryBytes)
	return dir, cold, fresh
}

// gocacheArmTrimPair parses the arm's `gocache-trim` line into its three byte
// numbers, failing the test if the line is absent or malformed.
func gocacheArmTrimPair(t *testing.T, out string) (before, after, freed int64) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "gocache-trim ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			for _, want := range []struct {
				key string
				dst *int64
			}{{"before=", &before}, {"after=", &after}, {"freed=", &freed}} {
				if !strings.HasPrefix(field, want.key) {
					continue
				}
				n, err := strconv.ParseInt(strings.TrimPrefix(field, want.key), 10, 64)
				if err != nil {
					t.Fatalf("the %s field of %q is not a number: %v", want.key, line, err)
				}
				*want.dst = n
			}
		}
		return before, after, freed
	}
	t.Fatalf("no gocache-trim line in the output:\n%s", out)
	return 0, 0, 0
}

func writeGocacheArmEntry(t *testing.T, shardDir, id string, dataBytes int, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(shardDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", shardDir, err)
	}
	for _, half := range []struct {
		suffix string
		size   int
	}{
		{scratch.ToolchainCacheEntryIndexSuffix, gocacheArmIndexBytes},
		{scratch.ToolchainCacheEntryDataSuffix, dataBytes},
	} {
		path := filepath.Join(shardDir, id+half.suffix)
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create %s: %v", path, err)
		}
		if err := f.Truncate(int64(half.size)); err != nil {
			f.Close() //stylegate:ignore the Truncate failure below is the reported one; this close is the fixture file's best-effort release on a path that aborts the test
			t.Fatalf("size %s: %v", path, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close %s: %v", path, err)
		}
		if age > 0 {
			when := time.Now().Add(-age)
			if err := os.Chtimes(path, when, when); err != nil {
				t.Fatalf("age %s: %v", path, err)
			}
		}
	}
}

// TestScratchGCGocache_ReportsWithoutDeleting is the Rule-41 half at the CLI
// boundary: the bare arm measures and JUDGES (the verdict reaches stderr) and
// leaves every byte on disk — including the shards that are already past the
// grace window and COULD be reclaimed. The toolchain cache is the operator's
// shared asset; a report must not be a reclaim.
func TestScratchGCGocache_ReportsWithoutDeleting(t *testing.T) {
	dir, coldBytes, freshBytes := gocacheArmFixture(t)
	out := runScratchGCVerb(t, t.TempDir(), scratchGCGocacheFlag, scratchGCGocachePathFlag, dir,
		scratchGCGocacheMaxGiBFlag, "1")
	if !strings.Contains(out, "role="+scratchGCGocacheRole) {
		t.Fatalf("the arm did not report on its own role line:\n%s", out)
	}
	if !strings.Contains(out, "path="+dir) {
		t.Fatalf("the line must name the cache it measured:\n%s", out)
	}
	// The bound is supplied by the FLAG here so the assertion is about this
	// invocation, not about the host's configuration: 1 GiB is below the fixture's
	// 3 MiB body only in spirit — the value that matters is that the flag parsed.
	if !strings.Contains(out, "bound="+scratch.FormatBytes(uint64(scratch.ToolchainCacheGiBBytes))) {
		t.Fatalf("--max-gib did not reach the measurement:\n%s", out)
	}
	if !strings.Contains(out, "free=") || !strings.Contains(out, "device=") {
		t.Fatalf("the line must carry the HOLDING filesystem's free space and device (the measurement / cannot answer):\n%s", out)
	}
	if !strings.Contains(out, "trim=false") {
		t.Fatalf("a bare invocation must report trim=false:\n%s", out)
	}
	// Every byte the fixture wrote is still there: the cold entries, the fresh one,
	// and the bookkeeping.
	for _, rel := range []string{
		filepath.Join("ab", gocacheArmEntryID(t, "1")+scratch.ToolchainCacheEntryDataSuffix),
		filepath.Join("ab", gocacheArmEntryID(t, "2")+scratch.ToolchainCacheEntryIndexSuffix),
		filepath.Join("cd", gocacheArmEntryID(t, "3")+scratch.ToolchainCacheEntryDataSuffix),
		filepath.Join("cd", gocacheArmEntryID(t, "4")+scratch.ToolchainCacheEntryDataSuffix),
		"README", "trim.txt", "log.txt", "CACHEDIR.TAG",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("the report removed %s — reporting must never delete (Rule 41): %v", rel, err)
		}
	}
	// The measured body the line must carry is the fixture's whole body: this is
	// the number a reader compares against `du`, so it is asserted, not logged.
	if !strings.Contains(out, "body="+scratch.FormatBytes(uint64(coldBytes+freshBytes))) {
		t.Fatalf("the line's body figure must be the measured entry total (%s):\n%s",
			scratch.FormatBytes(uint64(coldBytes+freshBytes)), out)
	}
	// THE GRAIN, in the arm's own output: the unit `reclaimable=` is counted in.
	if !strings.Contains(out, "grain="+scratch.ToolchainCacheEntryGrainName) {
		t.Fatalf("the line must name the grain its reclaimable figure is expressed at:\n%s", out)
	}
	// The reclaimable figure is the ENTRY grain's — larger than the shard grain's,
	// because the live-style shard cd holds a cold entry the shard reading cannot
	// see. This is the number the trim will actually free.
	if !strings.Contains(out, "reclaimable="+scratch.FormatBytes(uint64(coldBytes))) {
		t.Fatalf("the line's reclaimable figure must be the cold ENTRIES (%s at the %s grain):\n%s",
			scratch.FormatBytes(uint64(coldBytes)), scratch.ToolchainCacheEntryGrainName, out)
	}
	// The OLD grain's reading is still printed beside it, and it is SMALLER — the
	// two numbers on one line are the whole diagnosis of this work.
	if !strings.Contains(out, "shard_reclaimable=") {
		t.Fatalf("the line must keep the shard-grain reading beside the entry-grain one:\n%s", out)
	}
	if !strings.Contains(out, "shard_reclaimable="+scratch.FormatBytes(uint64(2*(gocacheArmIndexBytes+gocacheArmEntryBytes)))) {
		t.Fatalf("the shard-grain reading must count only the wholly-cold shard ab (%s):\n%s",
			scratch.FormatBytes(uint64(2*(gocacheArmIndexBytes+gocacheArmEntryBytes))), out)
	}
}

// TestScratchGCGocache_TrimRemovesColdEntriesAndReportsThePair drives the arm's
// ONLY deleting face and asserts the before/after numbers VERBATIM (the epic's
// acceptance evidence), the entries that must survive, and that the toolchain's
// bookkeeping is still there — the difference between "a bounded cache" and "a
// directory the toolchain no longer recognises".
func TestScratchGCGocache_TrimRemovesColdEntriesAndReportsThePair(t *testing.T) {
	dir, coldBytes, freshBytes := gocacheArmFixture(t)
	out := runScratchGCVerb(t, t.TempDir(), scratchGCGocacheFlag, scratchGCGocachePathFlag, dir,
		scratchGCGocacheTrimFlag, scratchGCGocacheMaxGiBFlag, "1")
	if !strings.Contains(out, "trim=true") {
		t.Fatalf("--trim was not honoured:\n%s", out)
	}
	if !strings.Contains(out, "gocache-trim role=") || !strings.Contains(out, "path="+dir) {
		t.Fatalf("the before/after line is missing or unlabelled:\n%s", out)
	}
	if !strings.Contains(out, "before=") || !strings.Contains(out, "after=") || !strings.Contains(out, "freed=") {
		t.Fatalf("the before/after numbers must be reported verbatim:\n%s", out)
	}
	// THE COLD ENTRIES ARE GONE, at the toolchain's own grain: both of ab's pairs and
	// the COLD pair of the live-style shard cd. The last one is what the old shard
	// grain could never reach — cd's newest entry is fresh.
	for _, gone := range []string{
		filepath.Join("ab", gocacheArmEntryID(t, "1")+scratch.ToolchainCacheEntryDataSuffix),
		filepath.Join("ab", gocacheArmEntryID(t, "2")+scratch.ToolchainCacheEntryIndexSuffix),
		filepath.Join("cd", gocacheArmEntryID(t, "3")+scratch.ToolchainCacheEntryDataSuffix),
	} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s survived a trim of the cold entries (stat err=%v)", gone, err)
		}
	}
	// EVERY SHARD DIRECTORY SURVIVES, including the emptied one: the reclaim's unit
	// is the entry, and a directory the toolchain wrote is not this verb's to delete.
	for _, shard := range []string{"ab", "cd"} {
		if info, err := os.Stat(filepath.Join(dir, shard)); err != nil || !info.IsDir() {
			t.Fatalf("shard directory %s must survive a trim (stat err=%v)", shard, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "cd", gocacheArmEntryID(t, "4")+scratch.ToolchainCacheEntryDataSuffix)); err != nil {
		t.Fatalf("the FRESH entry of the live-style shard cd was evicted — the grace window was not honoured: %v", err)
	}
	for _, book := range []string{"README", "trim.txt", "log.txt", "CACHEDIR.TAG"} {
		if _, err := os.Stat(filepath.Join(dir, book)); err != nil {
			t.Fatalf("the toolchain's %s was removed — the cache is no longer one its writer recognises: %v", book, err)
		}
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("the cache root must survive (stat err=%v)", err)
	}
	// The before/after pair is asserted as NUMBERS, not as a substring: the pair
	// is the epic's acceptance evidence, and "before > after" with the exact values
	// is the claim. freed is every COLD entry's bytes — including the cold pair of
	// the live-style shard cd, which the old shard grain could not have reached;
	// after is the surviving fresh entry's bytes.
	before, after, freed := gocacheArmTrimPair(t, out)
	if before != coldBytes+freshBytes {
		t.Fatalf("before = %d, want the measured body %d", before, coldBytes+freshBytes)
	}
	if after != freshBytes {
		t.Fatalf("after = %d, want the fresh entry's %d bytes", after, freshBytes)
	}
	if freed != coldBytes {
		t.Fatalf("freed = %d, want the cold entries' %d bytes", freed, coldBytes)
	}
	// THE ACCEPTANCE NUMBER, read off the CLI's OWN line: `body_after=` is the
	// RE-MEASURED body, and it must agree with the pair's after — a before/after
	// derived by subtraction would be a claim about arithmetic, not an observation.
	if !strings.Contains(out, "body_after="+scratch.FormatBytes(uint64(after))) {
		t.Fatalf("body_after must be the re-measured body (%d):\n%s", after, out)
	}
}

// TestScratchGCGocache_SaysWhenItCannotReclaim is the epic's acceptance (b), and
// the reason this work exists: on a cache that is OVER its bound but has NOTHING
// past the grace window (every entry fresh — a live, still-being-written cache),
// a reclaim frees ZERO bytes at either grain. The pre-fix arm printed
// `reclaimable=0B` and left the operator to read it as "your cache is fine"; the
// arm must now say, in its own output, which grain it works at and that it cannot
// reclaim.
func TestScratchGCGocache_SaysWhenItCannotReclaim(t *testing.T) {
	// The SMALLEST sample (Rule 10.4): one live-style shard whose entries are all
	// fresh — nothing can be reclaimed — with just enough body to sit OVER the
	// invocation's 1 GiB bound (2 × 600 MiB entries), so the arm's "nothing can be
	// reclaimed" sentence is what an over-bound operator actually sees.
	dir := t.TempDir()
	writeGocacheArmEntry(t, filepath.Join(dir, "cd"), gocacheArmEntryID(t, "7"), gocacheArmEntryBytes, 0)
	writeGocacheArmEntry(t, filepath.Join(dir, "ef"), gocacheArmEntryID(t, "8"), gocacheArmEntryBytes, time.Minute)
	for _, book := range []string{"README", "trim.txt"} {
		if err := os.WriteFile(filepath.Join(dir, book), []byte(book+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", book, err)
		}
	}
	// A bound BELOW the fixture's body, so the cache IS over its bound.
	out := runScratchGCVerb(t, t.TempDir(), scratchGCGocacheFlag, scratchGCGocachePathFlag, dir,
		scratchGCGocacheMaxGiBFlag, "1")
	if !strings.Contains(out, "reclaimable=0 bytes") {
		t.Fatalf("the fixture must report a zero reclaimable figure:\n%s", out)
	}
	if !strings.Contains(out, "grain="+scratch.ToolchainCacheEntryGrainName) {
		t.Fatalf("the arm must name its grain even when it reclaims nothing:\n%s", out)
	}
	if !strings.Contains(out, "NOTHING CAN BE RECLAIMED") {
		t.Fatalf("a zero reclaimable figure on an over-bound cache must be EXPLAINED in the arm's own output:\n%s", out)
	}
	if !strings.Contains(out, scratch.ToolchainCacheEntryGrainName) {
		t.Fatalf("the explanation must name the grain:\n%s", out)
	}
	if !strings.Contains(out, "grace window") {
		t.Fatalf("the explanation must name the reason (the grace window):\n%s", out)
	}
	// And the `--trim` face, on the same live-style cache: it must SAY it could not
	// reclaim rather than print a bare zero and exit.
	trimOut := runScratchGCVerb(t, t.TempDir(), scratchGCGocacheFlag, scratchGCGocachePathFlag, dir,
		scratchGCGocacheTrimFlag, scratchGCGocacheMaxGiBFlag, "1")
	if !strings.Contains(trimOut, "NOTHING CAN BE RECLAIMED") {
		t.Fatalf("a --trim that freed nothing on an over-bound cache must say so:\n%s", trimOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "cd", gocacheArmEntryID(t, "7")+scratch.ToolchainCacheEntryDataSuffix)); err != nil {
		t.Fatalf("a trim that could not reclaim must still have removed nothing: %v", err)
	}
}

// TestScratchGCGocache_TrimReportsTheFilesystemsOwnDelta is the regression guard
// for the one reading a reader could otherwise get wrong: a reclaim on a
// filesystem that was BELOW its headroom floor prints the report's "is NOT touched
// here" sentence (correct for a report) and then reclaims. Without the measured
// delta the two lines read as a contradiction; with it, the operator sees the
// MEASURED change in free space — and the field is signed, because that change can
// be no gain at all when another writer fills the gap.
func TestScratchGCGocache_TrimReportsTheFilesystemsOwnDelta(t *testing.T) {
	dir, coldBytes, _ := gocacheArmFixture(t)
	// A floor no real filesystem can satisfy, so the verdict is guaranteed to be
	// the low-headroom one regardless of where the suite's scratch root lives.
	out := runScratchGCVerb(t, t.TempDir(), scratchGCGocacheFlag, scratchGCGocachePathFlag, dir,
		scratchGCGocacheTrimFlag, scratchGCGocacheMaxGiBFlag, "1",
		scratchGCGocacheHeadroomGiBFlag, "1000000")
	if !strings.Contains(out, "the reclaim freed ") || !strings.Contains(out, "of filesystem space on ") {
		t.Fatalf("a reclaim on a below-floor filesystem must report the MEASURED change in free space:\n%s", out)
	}
	if !strings.Contains(out, "floor ") {
		t.Fatalf("the delta sentence must name the floor it was judged against:\n%s", out)
	}
	// The removed bytes are the cold entries' — the same figure the trim pair
	// reports RAW (that line's numbers are unformatted so they can be copied
	// verbatim) — so the two sentences cannot disagree about what was reclaimed.
	if !strings.Contains(out, "freed="+strconv.FormatInt(coldBytes, 10)) {
		t.Fatalf("the trim pair must report the cold entries' bytes (%d):\n%s", coldBytes, out)
	}
}

// TestScratchGCGocache_NamesTheOperatorDecision is the acceptance this row exists
// for: an arm reporting an overshoot must say, in its own output, that the reclaim
// is the OPERATOR's decision, name the exact command, and point at where the reason
// for the report-only design is written down — so a reader (and the next reader of
// `gocache --trim`) does not have to re-derive why an over-bound cache is still
// over its bound.
//
// The negative half is asserted on the same fixture with a bound ABOVE its body: a
// cache inside its bound is not a decision, and a sentence urging a reclaim there
// would be noise the operator learns to skip.
func TestScratchGCGocache_NamesTheOperatorDecision(t *testing.T) {
	dir, coldBytes, freshBytes := gocacheArmFixture(t)
	body := uint64(coldBytes + freshBytes)
	out := runScratchGCVerb(t, t.TempDir(), scratchGCGocacheFlag, scratchGCGocachePathFlag, dir,
		scratchGCGocacheMaxGiBFlag, "1")
	// A report is READ-ONLY, and this test's subject must not be the trim: the
	// decision sentence appears on the report face.
	if !strings.Contains(out, "trim=false") {
		t.Fatalf("the decision sentence belongs to the REPORT face:\n%s", out)
	}
	if !strings.Contains(out, "OPERATOR's decision") {
		t.Fatalf("an over-bound report must name whose decision the reclaim is:\n%s", out)
	}
	// The EXACT command, spelled from the arm's own constants (Rule 1b), so a doc
	// and the arm cannot drift: trim flag, arm selector, verb.
	if !strings.Contains(out, scratchGCGocacheTrimFlag) {
		t.Fatalf("the sentence must carry the exact reclaim command:\n%s", out)
	}
	if !strings.Contains(out, scratchGCGocacheDecisionReasonSource) {
		t.Fatalf("the sentence must name where the reason lives in the source:\n%s", out)
	}
	if !strings.Contains(out, scratchGCGocacheDecisionReasonDevdoc) {
		t.Fatalf("the sentence must name the decision brief:\n%s", out)
	}
	// And the reason itself, not only its address: the design sentence (Rule 41 —
	// the finder proposes, the operator decides) must be legible in the output.
	if !strings.Contains(out, "shared cross-project asset") || !strings.Contains(out, "never automatic") {
		t.Fatalf("the sentence must state WHY the arm never reclaims on its own:\n%s", out)
	}
	// Reading the report must have changed nothing.
	for _, rel := range []string{
		filepath.Join("ab", gocacheArmEntryID(t, "1")+scratch.ToolchainCacheEntryDataSuffix),
		filepath.Join("cd", gocacheArmEntryID(t, "3")+scratch.ToolchainCacheEntryDataSuffix),
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("the decision sentence's report removed %s (Rule 41): %v", rel, err)
		}
	}

	// NEGATIVE HALF: a cache INSIDE its bound carries no decision sentence. The bound
	// is supplied in MiB-below-the-body terms by the fixture itself — the arm's flag
	// is in GiB, so the bound is set from the measured body in whole GiB plus slack.
	underGiB := int64((body >> 30) + 2) // strictly above the fixture body, in GiB
	underOut := runScratchGCVerb(t, t.TempDir(), scratchGCGocacheFlag, scratchGCGocachePathFlag, dir,
		scratchGCGocacheMaxGiBFlag, strconv.FormatInt(underGiB, 10))
	if strings.Contains(underOut, "OPERATOR's decision") {
		t.Fatalf("a cache within its bound is not a decision — no sentence expected:\n%s", underOut)
	}
}

// TestScratchGCGocache_TrimFaceDoesNotUrgeItself pins the other half of the face
// split: when the operator HAS run `--trim`, the arm must not tell them to run it.
// The sentence is about a decision not yet taken.
func TestScratchGCGocache_TrimFaceDoesNotUrgeItself(t *testing.T) {
	dir, _, _ := gocacheArmFixture(t)
	out := runScratchGCVerb(t, t.TempDir(), scratchGCGocacheFlag, scratchGCGocachePathFlag, dir,
		scratchGCGocacheTrimFlag, scratchGCGocacheMaxGiBFlag, "1")
	if !strings.Contains(out, "trim=true") {
		t.Fatalf("--trim was not honoured:\n%s", out)
	}
	if strings.Contains(out, "OPERATOR's decision") {
		t.Fatalf("the trim face must not urge the flag that was just run:\n%s", out)
	}
}

// TestScratchGCGocache_TrimWithheldWhenSweepsDisabled is the disabled-host half:
// even an explicit `--trim` must not delete when the operator has turned the
// sweeps off — and the refusal must be a sentence, not a silence (Rule 15).
func TestScratchGCGocache_TrimWithheldWhenSweepsDisabled(t *testing.T) {
	dir, _, _ := gocacheArmFixture(t)
	out := runScratchGCVerb(t, t.TempDir(), "env:"+scratch.GCIntervalEnvName+"=0",
		scratchGCGocacheFlag, scratchGCGocachePathFlag, dir, scratchGCGocacheTrimFlag, scratchGCGocacheMaxGiBFlag, "1")
	if !strings.Contains(out, "withheld") {
		t.Fatalf("a --trim on a disabled host must say it was withheld:\n%s", out)
	}
	if !strings.Contains(out, "trim=false") {
		t.Fatalf("the disabled arm must report trim=false:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "ab", gocacheArmEntryID(t, "1")+scratch.ToolchainCacheEntryDataSuffix)); err != nil {
		t.Fatalf("a disabled sweep deleted an entry on the strength of a flag: %v", err)
	}
}

// TestScratchGCGocache_BadGiBValueIsRefused pins the loud-failure half of the two
// new flags: a mistyped threshold must not silently become the default.
func TestScratchGCGocache_BadGiBValueIsRefused(t *testing.T) {
	dir, _, _ := gocacheArmFixture(t)
	out := runScratchGCVerbExpectFailure(t, t.TempDir(), scratchGCGocacheFlag, scratchGCGocachePathFlag, dir,
		scratchGCGocacheMaxGiBFlag, "lots")
	if !strings.Contains(out, "requires a positive integer number of GiB") {
		t.Fatalf("a bad GiB value was not refused with a reason:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "ab")); err != nil {
		t.Fatalf("a refused invocation must not touch the cache: %v", err)
	}
}
