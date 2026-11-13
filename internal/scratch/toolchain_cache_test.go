package scratch

// toolchain_cache_test.go — the AGENT-side toolchain cache's measurement, its
// headroom/bound criterion, and its oldest-shard eviction.
//
// THE FIXTURES ARE ALWAYS A CONTROLLED CACHE, NEVER THE OPERATOR'S: every test
// builds its own directory under t.TempDir() (the suite's pinned scratch home)
// and points GOCACHE at it, so no test can read or write ~/.cache/go-build — the
// 121G shared asset this machinery exists to report on and must never touch by
// accident (Rule 41; the trap the merger-gate tests hit once, measured 110M in
// the operator's real cache).
//
// "AGE" IS SET WITH os.Chtimes, NEVER WAITED FOR (Rule 27: no test may generate
// load, and a test that needed a 24h-old shard to exist naturally would need
// either a fake clock or a patience nobody has).

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// toolchainCacheFixture is a controlled cache with a known body, built at the
// TOOLCHAIN'S OWN GRAIN — a cache entry is the `<hex>-a` index file plus its
// `<hex>-d` data file, and the fixture writes both (a fixture that wrote one file
// per entry could not tell the two grains apart, which is the whole subject of
// this file):
//
//	aa/  two entries, aged 48h          → reclaimable (past the 24h window)
//	bb/  one entry,  aged 30h            → reclaimable
//	dd/  one COLD entry (40h) beside a FRESH one (now)
//	                                      → the LIVE-STYLE shard: the shard grain
//	                                        sees a fresh shard and reclaims NOTHING
//	                                        here, the entry grain takes the cold
//	                                        entry and keeps the fresh one
//	cc/  one entry,  written just now    → KEPT (inside the grace window)
//	README, trim.txt, log.txt            → the toolchain's bookkeeping, never a candidate
//	1234567890abcdef/                    → a per-repo KEY directory (not a shard): the
//	                                       16-hex shape the merge gate writes is not a
//	                                       toolchain shard and must be left alone
//
// It returns the directory and the byte totals the test asserts against, both at
// the ENTRY grain.
type toolchainCacheFixture struct {
	dir   string
	old   int64 // bytes an eviction at the entry grain may reclaim (aa+bb+dd's cold entry)
	fresh int64 // bytes it must never touch (cc's entry + dd's fresh entry)
}

const (
	fixtureIndexBytes      = 256
	fixtureOldEntryBytes   = 4096
	fixtureFreshEntryBytes = 8192
	fixtureMixedColdBytes  = 4096
	fixtureMixedFreshBytes = 8192
	fixtureRepoKeyName     = "1234567890abcdef"
	// fixtureLiveStyleShard is the shard whose newest entry is FRESH while it still
	// holds a cold entry — the state of every shard of a live, still-being-written
	// cache, and the state the old shard grain reclaimed nothing from.
	fixtureLiveStyleShard = "dd"
)

// The fixture's entry IDs are 64 lowercase hex characters, the shape the
// toolchain writes (SHA-256 rendered with %x). They are named here so the tests
// assert against the SAME spelling the machinery recognises (Rule 13/14).
const (
	fixtureIDA1         = "1111111111111111111111111111111111111111111111111111111111111111"
	fixtureIDA2         = "2222222222222222222222222222222222222222222222222222222222222222"
	fixtureIDB1         = "3333333333333333333333333333333333333333333333333333333333333333"
	fixtureIDC1         = "4444444444444444444444444444444444444444444444444444444444444444"
	fixtureIDMixedCold  = "5555555555555555555555555555555555555555555555555555555555555555"
	fixtureIDMixedFresh = "6666666666666666666666666666666666666666666666666666666666666666"
)

func newToolchainCacheFixture(t *testing.T) toolchainCacheFixture {
	t.Helper()
	dir := t.TempDir()
	f := toolchainCacheFixture{dir: dir}
	writeShardEntry(t, dir, "aa", fixtureIDA1, fixtureOldEntryBytes, 48*time.Hour)
	writeShardEntry(t, dir, "aa", fixtureIDA2, fixtureOldEntryBytes, 50*time.Hour)
	writeShardEntry(t, dir, "bb", fixtureIDB1, fixtureOldEntryBytes, 30*time.Hour)
	writeShardEntry(t, dir, "cc", fixtureIDC1, fixtureFreshEntryBytes, 0)
	writeShardEntry(t, dir, fixtureLiveStyleShard, fixtureIDMixedCold, fixtureMixedColdBytes, 40*time.Hour)
	writeShardEntry(t, dir, fixtureLiveStyleShard, fixtureIDMixedFresh, fixtureMixedFreshBytes, 0)
	perEntry := int64(fixtureIndexBytes) + fixtureOldEntryBytes
	f.old = 3*perEntry + int64(fixtureIndexBytes) + fixtureMixedColdBytes
	f.fresh = int64(fixtureIndexBytes) + fixtureFreshEntryBytes + int64(fixtureIndexBytes) + fixtureMixedFreshBytes
	// The toolchain's own bookkeeping files, at the ROOT, as FILES.
	for _, name := range []string{"README", "trim.txt", "log.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+" contents\n"), FilePerm); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// A per-repo KEY directory (16 lowercase hex): the shape the MERGE gate writes
	// under its own cache root. It is not a toolchain shard (two hex characters),
	// so nothing in this machinery may treat it as one.
	keyDir := filepath.Join(dir, fixtureRepoKeyName)
	if err := os.MkdirAll(filepath.Join(keyDir, BuildCacheGoCacheLeafName), DirPerm); err != nil {
		t.Fatalf("mkdir key dir: %v", err)
	}
	return f
}

// writeShardEntry writes ONE cache ENTRY into a shard, in the toolchain's own
// shape: the `<id>-a` index file plus the `<id>-d` data file, both aged by age
// (0 = now), so the ENTRY's newest file is what the measuring code must read.
func writeShardEntry(t *testing.T, root, shard, id string, dataBytes int, age time.Duration) {
	t.Helper()
	dir := filepath.Join(root, shard)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		t.Fatalf("mkdir shard %s: %v", shard, err)
	}
	writeCacheEntryFile(t, filepath.Join(dir, id+ToolchainCacheEntryIndexSuffix), fixtureIndexBytes, age)
	writeCacheEntryFile(t, filepath.Join(dir, id+ToolchainCacheEntryDataSuffix), dataBytes, age)
}

// writeCacheEntryFile writes one file of a cache entry — the shared writer of the
// two halves, so the index's size is spelled ONCE (Rule 13/14).
func writeCacheEntryFile(t *testing.T, path string, size int, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), FilePerm); err != nil {
		t.Fatalf("write entry %s: %v", path, err)
	}
	if age > 0 {
		when := time.Now().Add(-age)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatalf("age entry %s: %v", path, err)
		}
	}
}

// TestMeasureToolchainCacheCountsTheBodyAndTheFilesystem is the measurement
// contract: the BODY is the entry shards (not the bookkeeping files, not a repo
// key directory), and the filesystem half is measured too — device, type, total
// and free bytes — because that is the number the criterion exists for.
func TestMeasureToolchainCacheCountsTheBodyAndTheFilesystem(t *testing.T) {
	f := newToolchainCacheFixture(t)
	rep, err := MeasureToolchainCache(f.dir)
	if err != nil {
		t.Fatalf("MeasureToolchainCache: %v", err)
	}
	wantBody := f.old + f.fresh
	if rep.Total != wantBody {
		t.Fatalf("body = %d bytes, want %d (entry shards only)", rep.Total, wantBody)
	}
	if len(rep.Shards) != 4 {
		t.Fatalf("shards = %d, want 4 (aa, bb, cc, dd) — a 16-hex repo key and the bookkeeping files are not shards", len(rep.Shards))
	}
	if rep.OldestShardBytes != f.old-int64(fixtureIndexBytes)-fixtureMixedColdBytes {
		t.Fatalf("the SHARD-grain reading = %d bytes, want %d (aa+bb only: dd's newest entry is fresh, so the old grain loses dd's cold entry entirely)",
			rep.OldestShardBytes, f.old-int64(fixtureIndexBytes)-fixtureMixedColdBytes)
	}
	// THE ENTRY GRAIN, and the point of this whole file: the reclaimable figure is
	// larger than the shard grain's by exactly the cold entries that sit in a shard
	// kept alive by a fresh one (dd).
	if rep.ReclaimableBytes != f.old {
		t.Fatalf("reclaimable = %d bytes at grain %q, want %d (aa+bb+dd's COLD entry)", rep.ReclaimableBytes, rep.Grain, f.old)
	}
	if rep.ReclaimableBytes <= rep.OldestShardBytes {
		t.Fatalf("the entry grain (%d) must exceed the shard grain (%d) on a live-style fixture — otherwise the fixture does not model the defect",
			rep.ReclaimableBytes, rep.OldestShardBytes)
	}
	if rep.Grain != ToolchainCacheEntryGrainName {
		t.Fatalf("the report must name its grain, got %q want %q", rep.Grain, ToolchainCacheEntryGrainName)
	}
	if len(rep.Entries) != 6 {
		t.Fatalf("entries = %d, want 6 (two pairs in aa, one each in bb/cc, two in dd)", len(rep.Entries))
	}
	for _, e := range rep.Entries {
		if e.Bytes != int64(fixtureIndexBytes)+int64(e.DataBytes) {
			t.Fatalf("entry %s/%s: pair bytes %d != index+data (%d + %d)", e.Shard, e.Name, e.Bytes, fixtureIndexBytes, e.DataBytes)
		}
		if len(e.Paths) != 2 {
			t.Fatalf("entry %s/%s: paths = %v, want the index and data halves", e.Shard, e.Name, e.Paths)
		}
	}
	if len(rep.Bookkeeping) != 3 {
		t.Fatalf("bookkeeping = %v, want the three root-level files", rep.Bookkeeping)
	}
	if rep.Device == 0 {
		t.Fatal("the filesystem's device was not measured")
	}
	if rep.FSType == "" {
		t.Fatal("the filesystem's type was not measured")
	}
	if rep.FSFree == 0 || rep.FSTotal == 0 {
		t.Fatalf("the filesystem's capacity was not measured (free=%d total=%d)", rep.FSFree, rep.FSTotal)
	}
	if rep.FSFree > rep.FSTotal {
		t.Fatalf("free (%d) exceeds total (%d) — an implausible measurement must not be reported", rep.FSFree, rep.FSTotal)
	}
}

// TestMeasureToolchainCacheMeasuresTheHoldingFilesystemNotSlash is the
// PLACEMENT-BLIND RED PROOF the epic asks for.
//
// The defect: a criterion that answers "is there room?" by looking at `/` — the
// reading Rule 22's off-`/` test makes — is blind to a cache living on a full
// /home, which is exactly the measured state this file exists for (cache on
// device 66309 with 19G free; `/` on 66307 with 131G free).
//
// The proof is by MEASUREMENT and not by mocking: the fixture lives on the
// filesystem the suite's scratch home is on, and the test asks for a floor ONE
// BYTE above that filesystem's measured free space. Our criterion refuses; a
// criterion reading `/`'s free space would pass, because `/` has more room than
// the floor. When both paths are the same filesystem the proof is vacuous and the
// test says so instead of pretending (Rule 15: a check that could not run must
// not read as one that ran).
func TestMeasureToolchainCacheMeasuresTheHoldingFilesystemNotSlash(t *testing.T) {
	f := newToolchainCacheFixture(t)
	rep, err := MeasureToolchainCache(f.dir)
	if err != nil {
		t.Fatalf("MeasureToolchainCache: %v", err)
	}
	rootFree, _, err := FilesystemCapacity(rootDeviceProbePath)
	if err != nil {
		t.Fatalf("measure %s: %v", rootDeviceProbePath, err)
	}
	rootDev, err := RootDeviceID()
	if err != nil {
		t.Fatalf("RootDeviceID: %v", err)
	}
	if rep.Device == rootDev {
		t.Skipf("the fixture and %s share device 0x%x — the placement-blind red proof would be vacuous on this host",
			rootDeviceProbePath, rootDev)
	}
	if rootFree <= rep.FSFree {
		t.Skipf("the fixture's filesystem (%s free) has no more room than %s (%s free) — the blind reading would refuse too, so this host cannot distinguish them",
			FormatBytes(rep.FSFree), rootDeviceProbePath, FormatBytes(rootFree))
	}
	// The floor is ABOVE the holding filesystem's free space and BELOW `/`'s — the
	// one interval where the two readings disagree.
	floor := rep.FSFree + 1
	v := CheckToolchainCache(rep, 0, floor)
	if !v.LowHeadroom {
		t.Fatalf("a floor of %s above the holding filesystem's %s did not refuse — the criterion is not measuring the filesystem that holds the cache",
			FormatBytes(floor), FormatBytes(rep.FSFree))
	}
	if !strings.Contains(v.Reason, f.dir) || !strings.Contains(v.Reason, FormatBytes(rep.FSFree)) {
		t.Fatalf("the refusal must name the cache path and the measured free space; got: %s", v.Reason)
	}
	// The device must be named in the decimal spelling `stat -c %d` prints, so a
	// reader can CHECK it against their own instrument — and it must be the
	// measured one, not a placeholder.
	if want := "device " + strconv.FormatUint(rep.Device, 10); !strings.Contains(v.Reason, want) {
		t.Fatalf("the refusal must name the MEASURED device (%q, the `stat -c %%d` spelling); got: %s", want, v.Reason)
	}
	// The control, stated as the MEASURED disagreement it is: a criterion that read
	// `/` instead of the holding filesystem would SATISFY this floor, because `/`
	// has more room than it. (An earlier draft of this test asserted this by
	// running CheckToolchainCache on the FIXTURE's report with `/`'s floor — which
	// of course refuses, since the fixture's own free space is what the criterion
	// measures. The assertion below is the honest form of the same claim, and it
	// is what makes the red proof non-vacuous.)
	if rootFree < floor {
		t.Fatalf("`/` reports %s free, below the floor %s — the two readings would agree and this host cannot distinguish them",
			FormatBytes(rootFree), FormatBytes(floor))
	}
}

// TestCheckToolchainCacheOverBoundNamesTheReclaimableBytes pins the bound arm and
// its refusal sentence: an operator reading it must learn the size, the bound and
// how much of the cache is already past the grace window (the actionable number).
func TestCheckToolchainCacheOverBoundNamesTheReclaimableBytes(t *testing.T) {
	f := newToolchainCacheFixture(t)
	rep, err := MeasureToolchainCache(f.dir)
	if err != nil {
		t.Fatalf("MeasureToolchainCache: %v", err)
	}
	bound := rep.Total - 1
	v := CheckToolchainCache(rep, bound, 0)
	if !v.OverBound {
		t.Fatalf("the cache (%d bytes) was not reported over a %d-byte bound", rep.Total, bound)
	}
	if !strings.Contains(v.Reason, f.dir) || !strings.Contains(v.Reason, FormatBytes(uint64(rep.Total))) {
		t.Fatalf("the refusal must name the path and the measured size; got: %s", v.Reason)
	}
	if v.LowHeadroom {
		t.Fatal("a zero headroom floor must disable that arm (the explicit measure-only form)")
	}
	// The control: one byte more room than the body is NOT over bound.
	if ok := CheckToolchainCache(rep, rep.Total+1, 0); ok.OverBound {
		t.Fatal("a bound above the measured body must not report OverBound")
	}
}

// TestEvictToolchainCacheRemovesOldestEntriesOnly is the EVICTION contract AND the
// red proof against the unbounded/naive alternatives:
//
//   - a naive "trim until small" removes the FRESH entries too (cc, and dd's fresh
//     entry) — it must not: a build that ran minutes ago keeps its entries;
//   - the OLD shard grain reclaims NOTHING from dd (its newest entry is fresh), so a
//     live cache gave up zero bytes through it — the GREEN half below is that dd's
//     cold entry DOES come back at the entry grain;
//   - a naive "empty the directory" removes the toolchain's bookkeeping — it must
//     not: README/trim.txt/log.txt are what makes the directory still a cache the
//     toolchain recognises;
//   - the root itself, and every shard DIRECTORY, is never removed, at any target.
//
// The measurements are asserted as BEFORE/AFTER numbers, because the epic's
// acceptance is the verbatim before/after pair.
func TestEvictToolchainCacheRemovesOldestEntriesOnly(t *testing.T) {
	f := newToolchainCacheFixture(t)
	rep, err := MeasureToolchainCache(f.dir)
	if err != nil {
		t.Fatalf("MeasureToolchainCache: %v", err)
	}
	ev, err := EvictToolchainCache(f.dir, 0, ToolchainCacheEvictMinAge, time.Now())
	if err != nil {
		t.Fatalf("EvictToolchainCache: %v", err)
	}
	if ev.Before != rep.Total {
		t.Fatalf("before = %d, want the measured body %d", ev.Before, rep.Total)
	}
	if ev.After != f.fresh {
		t.Fatalf("after = %d, want %d (only the fresh entries survive)", ev.After, f.fresh)
	}
	if ev.FreedBytes != f.old {
		t.Fatalf("freed = %d, want %d (aa+bb+dd's cold entry)", ev.FreedBytes, f.old)
	}
	if ev.RemovedEntries != 4 {
		t.Fatalf("removed entries = %d (%v), want 4 (aa×2, bb, dd's cold one)", ev.RemovedEntries, ev.Removed)
	}
	// THE SHARD GRAIN'S NOTE, as a number: the coarse reading would have emptied
	// the two whole old shards (aa, bb) and stopped, releasing
	// f.old - (dd's cold pair) bytes. That gap is the defect, measured.
	if ev.ShardCandidates != 2 {
		t.Fatalf("shard candidates = %d, want 2 (aa, bb) — the shards the OLD grain would have removed", ev.ShardCandidates)
	}
	for _, gone := range []string{
		filepath.Join("aa", fixtureIDA1+ToolchainCacheEntryDataSuffix),
		filepath.Join("aa", fixtureIDA2+ToolchainCacheEntryIndexSuffix),
		filepath.Join("bb", fixtureIDB1+ToolchainCacheEntryDataSuffix),
		filepath.Join(fixtureLiveStyleShard, fixtureIDMixedCold+ToolchainCacheEntryDataSuffix),
	} {
		if _, err := os.Stat(filepath.Join(f.dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s should be gone (stat err=%v)", gone, err)
		}
	}
	// The RED halves: the grace window, the roots of every shard, and the
	// toolchain's bookkeeping.
	for _, kept := range []string{
		filepath.Join("cc", fixtureIDC1+ToolchainCacheEntryDataSuffix),
		filepath.Join(fixtureLiveStyleShard, fixtureIDMixedFresh+ToolchainCacheEntryIndexSuffix),
		filepath.Join(fixtureLiveStyleShard, fixtureIDMixedFresh+ToolchainCacheEntryDataSuffix),
	} {
		if _, err := os.Stat(filepath.Join(f.dir, kept)); err != nil {
			t.Fatalf("the FRESH entry %s was evicted — the %s grace window was not honoured: %v", kept, ToolchainCacheEvictMinAge, err)
		}
	}
	// Every shard directory survives, INCLUDING the two this pass emptied: the
	// reclaim's unit is the entry, and a directory the toolchain wrote is not this
	// verb's to delete.
	for _, shard := range []string{"aa", "bb", "cc", fixtureLiveStyleShard} {
		if info, err := os.Stat(filepath.Join(f.dir, shard)); err != nil || !info.IsDir() {
			t.Fatalf("shard directory %s must survive every eviction (stat err=%v)", shard, err)
		}
	}
	for _, name := range []string{"README", "trim.txt", "log.txt"} {
		if _, err := os.Stat(filepath.Join(f.dir, name)); err != nil {
			t.Fatalf("the toolchain's own %s was removed — the directory is no longer a cache its writer recognises: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.dir, fixtureRepoKeyName, BuildCacheGoCacheLeafName)); err != nil {
		t.Fatalf("a per-repo key directory was removed — only cache entries are candidates: %v", err)
	}
	if info, err := os.Stat(f.dir); err != nil || !info.IsDir() {
		t.Fatalf("the cache ROOT must survive every eviction (stat err=%v)", err)
	}
	// RE-MEASURED, and the two numbers agree: the after-figure this eviction
	// reported equals what a fresh measurement now reads — the pair an operator
	// checks against `du` (Rule 31: the claim is verified on the surface itself).
	after, err := MeasureToolchainCache(f.dir)
	if err != nil {
		t.Fatalf("MeasureToolchainCache (after): %v", err)
	}
	if after.Total != ev.After {
		t.Fatalf("the eviction reported after=%d but a re-measurement reads %d — the two must agree", ev.After, after.Total)
	}
}

// TestEvictToolchainCacheOldShardGrainWouldReclaimNothing is the RED PROOF the epic
// asks for, stated on the SAME fixture the GREEN proof above runs on: a live-style
// cache — every shard holding at least one fresh entry — is one where the OLD
// grain (whole shards, evictable only when the shard's newest entry is old) has no
// candidate AT ALL.
//
// It is written against the old predicate's own inputs (rep.OldestShardBytes and
// the measured shard newest-mtimes) rather than against a copy of the old code, so
// the proof stays honest after the code moved on: it measures what the old grain
// COULD have claimed about this cache.
func TestEvictToolchainCacheOldShardGrainWouldReclaimNothing(t *testing.T) {
	dir := t.TempDir()
	// A cache whose ONLY shard is live-style: one cold entry (40h) beside a fresh
	// one. Every shard holds something fresh — the state of a cache that is still
	// being written.
	writeShardEntry(t, dir, fixtureLiveStyleShard, fixtureIDMixedCold, fixtureMixedColdBytes, 40*time.Hour)
	writeShardEntry(t, dir, fixtureLiveStyleShard, fixtureIDMixedFresh, fixtureMixedFreshBytes, 0)
	rep, err := MeasureToolchainCache(dir)
	if err != nil {
		t.Fatalf("MeasureToolchainCache: %v", err)
	}
	if rep.Total == 0 {
		t.Fatal("the fixture must hold bytes, otherwise the red proof proves nothing")
	}
	if rep.OldestShardBytes != 0 {
		t.Fatalf("the SHARD grain claims %d reclaimable bytes on a live-style shard, want 0 (its newest entry is fresh, so no shard is a candidate)",
			rep.OldestShardBytes)
	}
	if rep.ReclaimableBytes == 0 {
		t.Fatal("the ENTRY grain must see the cold entry the shard grain cannot")
	}
	// The RED claim, run: the old grain's candidate set is empty, so a bound below
	// the body could not be reached at all by removing shards. The green run on the
	// same cache frees the cold entry.
	ev, err := EvictToolchainCache(dir, 0, ToolchainCacheEvictMinAge, time.Now())
	if err != nil {
		t.Fatalf("EvictToolchainCache: %v", err)
	}
	if len(ev.Removed) == 0 || ev.FreedBytes == 0 {
		t.Fatal("the entry-grain eviction freed nothing on a live-style cache — this is the defect the work exists to fix")
	}
	if ev.After != rep.Total-ev.FreedBytes {
		t.Fatalf("after = %d, want %d (the measured body minus what was freed)", ev.After, rep.Total-ev.FreedBytes)
	}
	if _, err := os.Stat(filepath.Join(dir, fixtureLiveStyleShard, fixtureIDMixedFresh+ToolchainCacheEntryDataSuffix)); err != nil {
		t.Fatalf("the fresh entry was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, fixtureLiveStyleShard, fixtureIDMixedCold+ToolchainCacheEntryDataSuffix)); !os.IsNotExist(err) {
		t.Fatalf("the cold entry survived (stat err=%v)", err)
	}
}

// TestEvictToolchainCacheStopsAtTheTarget is the BOUND half of the eviction: with a
// target above the reclaimable bytes, the un-reclaimable entries are untouched and
// the eviction reports what it skipped — a reclaim that stopped must say why
// (Rule 15b).
func TestEvictToolchainCacheStopsAtTheTarget(t *testing.T) {
	f := newToolchainCacheFixture(t)
	// The target is the fresh bytes alone: the eviction must remove every cold entry
	// to get there, and must not need to touch a fresh one.
	ev, err := EvictToolchainCache(f.dir, f.fresh, ToolchainCacheEvictMinAge, time.Now())
	if err != nil {
		t.Fatalf("EvictToolchainCache: %v", err)
	}
	if ev.After > f.fresh {
		t.Fatalf("after = %d, want at most the target %d", ev.After, f.fresh)
	}
	if ev.FreedBytes != f.old {
		t.Fatalf("freed = %d, want every cold entry's %d bytes", ev.FreedBytes, f.old)
	}
	// NOTHING fresh was skipped: with a target at the fresh bytes, the loop stops
	// when it gets there, so the skip list is empty — the fresh entries were never
	// candidates, not skipped candidates. Both readings must be distinguishable, so
	// the distinction is asserted rather than assumed.
	if len(ev.Skipped) != 0 {
		t.Fatalf("skipped = %v, want none — the target was reached without ever re-reading a fresh entry", ev.Skipped)
	}
}

// TestEvictToolchainCacheSkipsAnEntryThatBecameFresh is the RE-READ half of the
// safety contract (Rule 10.4: one live-style entry is the smallest sample of the
// race): the decision to remove is re-read at the delete, and an entry whose pair
// was written since the measurement is kept even though the walk had selected it.
//
// The race is staged WITHOUT concurrency — the mechanism is exercised by calling the
// re-read directly with a cutoff the entry does not satisfy, which is exactly what a
// build finishing in between produces.
func TestEvictToolchainCacheSkipsAnEntryThatBecameFresh(t *testing.T) {
	dir := t.TempDir()
	writeShardEntry(t, dir, fixtureLiveStyleShard, fixtureIDMixedCold, fixtureMixedColdBytes, 40*time.Hour)
	entry := ToolchainCacheEntry{Shard: fixtureLiveStyleShard, Name: fixtureIDMixedCold + ToolchainCacheEntryDataSuffix}
	// A cutoff OLDER than the entry's own age: the entry is now "newer than the
	// cutoff" — what a build finishing in between produces — and the re-read must
	// refuse it.
	skip, _ := verifyCacheEntryAtDelete(dir, entry, time.Now().Add(-48*time.Hour))
	if skip == "" {
		t.Fatal("an entry whose pair is younger than the cutoff must be kept by the re-read")
	}
	if !strings.Contains(skip, fixtureIDMixedCold) {
		t.Fatalf("the skip reason must name the entry: %q", skip)
	}
	if _, err := os.Stat(filepath.Join(dir, fixtureLiveStyleShard, fixtureIDMixedCold+ToolchainCacheEntryDataSuffix)); err != nil {
		t.Fatalf("the re-read must not have removed anything: %v", err)
	}
	// A cutoff NEWER than its age: the entry is genuinely cold, so it may go, and the
	// re-read returns it as the LIVE entry (whose Paths are the files that exist now).
	skip, live := verifyCacheEntryAtDelete(dir, entry, time.Now())
	if skip != "" {
		t.Fatalf("a cold entry must pass the re-read, got skip=%q", skip)
	}
	if len(live.Paths) != 2 {
		t.Fatalf("the live entry's paths = %v, want the index and data halves", live.Paths)
	}
	// An entry that is GONE (the toolchain's own trim took it) is a skip with its
	// reason, never a crash and never a silent success.
	if err := os.Remove(filepath.Join(dir, fixtureLiveStyleShard, fixtureIDMixedCold+ToolchainCacheEntryIndexSuffix)); err != nil {
		t.Fatalf("remove half the pair: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, fixtureLiveStyleShard, fixtureIDMixedCold+ToolchainCacheEntryDataSuffix)); err != nil {
		t.Fatalf("remove half the pair: %v", err)
	}
	skip, _ = verifyCacheEntryAtDelete(dir, entry, time.Now())
	if skip == "" {
		t.Fatal("an entry that no longer exists must be reported as a skip, not silently counted")
	}
}

// TestResolveToolchainCacheMaxBytesRanking pins the ONE reader's precedence:
// invocation flag > environment knob > default, with a malformed environment
// value REPORTED and the default applied (never a silent zero, Rule 15).
func TestResolveToolchainCacheMaxBytesRanking(t *testing.T) {
	flag := int64(3) * ToolchainCacheGiBBytes
	if got := ResolveToolchainCacheMaxBytes(flag); got != flag {
		t.Fatalf("flag value ignored: got %d, want %d", got, flag)
	}
	t.Setenv(ToolchainCacheMaxEnvName, "7")
	if got := ResolveToolchainCacheMaxBytes(0); got != 7*ToolchainCacheGiBBytes {
		t.Fatalf("environment value ignored: got %d, want %d", got, 7*ToolchainCacheGiBBytes)
	}
	if got := ResolveToolchainCacheMaxBytes(flag); got != flag {
		t.Fatalf("flag must outrank the environment: got %d, want %d", got, flag)
	}
	t.Setenv(ToolchainCacheMaxEnvName, "not-a-number")
	if got := ResolveToolchainCacheMaxBytes(0); got != DefaultToolchainCacheMaxBytes {
		t.Fatalf("a malformed value must fall back to the default: got %d, want %d", got, DefaultToolchainCacheMaxBytes)
	}
}

// goCacheEnvNameInTest is the Go toolchain's own cache-directory variable, named
// ONCE for this test's fixtures. It is NOT a production constant on purpose: the
// variable is the TOOLCHAIN's, and the resolver spells it at its single call site
// (which is also the spelling `go env GOCACHE` answers to). A second exported
// definition would be a second source of truth for a name gopi does not own.
const goCacheEnvNameInTest = "GOCACHE"

// TestResolveToolchainCachePathConsultsTheGoEnvFile pins the channel the
// pre-fix resolver was MISSING: the go env file (<home>/.config/go/env), which is
// where `go env -w` PERSISTS a pin. Measured defect it pins (nick-sager,
// 2026-11-01): the file said GOCACHE=/home/nick/workspace/.gocache while the
// verb reported /home/nick/.cache/go-build — a report about a cache no build
// fills, and (behind `--trim`) an eviction aimed at the wrong tree.
//
// The fixture is a controlled home with a controlled env file — never the
// operator's: the path arrives through scratch.WorkspaceHomeEnvName, the ONE
// override homeBase() honours, and every directory is under t.TempDir().
//
// Three assertions, in the toolchain's own order:
//
//	① go env file  WINS over the user-cache fallback (the repaired bug)
//	② $GOCACHE     WINS over the go env file   (the process env outranks the file)
//	③ --path       WINS over both              (the toolchain's FIRST channel)
//
// ② is the half that keeps ① honest: a resolver that read only the file would
// pass ① and fail ②.
func TestResolveToolchainCachePathConsultsTheGoEnvFile(t *testing.T) {
	home := t.TempDir()
	envFileDir := filepath.Join(home, ".config", "go")
	if err := os.MkdirAll(envFileDir, 0o755); err != nil {
		t.Fatalf("materialise the go env file's directory: %v", err)
	}
	// The DIRECTORY the go env file pins, and the file itself, written the way
	// `go env -w GOCACHE=<dir>` writes it: one bare `KEY=VALUE` line.
	filePinned := filepath.Join(t.TempDir(), "gocache-from-go-env-file")
	if err := os.MkdirAll(filePinned, 0o755); err != nil {
		t.Fatalf("materialise the pinned cache dir %s: %v", filePinned, err)
	}
	envFile := filepath.Join(envFileDir, "env")
	if err := os.WriteFile(envFile, []byte(goCacheEnvNameInTest+"="+filePinned+"\n"), 0o600); err != nil {
		t.Fatalf("write the fixture go env file %s: %v", envFile, err)
	}

	// The channel under test is the one for a host whose shell does NOT export
	// GOCACHE — the measured shape: `go env GOCACHE` answers from the file.
	t.Setenv(WorkspaceHomeEnvName, home)
	t.Setenv(goCacheEnvNameInTest, "")

	// ① the file wins over <user cache dir>/go-build.
	got, err := ResolveToolchainCachePath("")
	if err != nil {
		t.Fatalf("ResolveToolchainCachePath with an unset $GOCACHE and a pinned go env file: %v", err)
	}
	if want := CanonicalPath(filePinned); got != want {
		t.Fatalf("the go env file must outrank the user-cache fallback: got %q, want %q (the file %s pins it)",
			got, want, envFile)
	}

	// ② the process environment wins over the file.
	fromEnv := filepath.Join(t.TempDir(), "gocache-from-the-environment")
	if err := os.MkdirAll(fromEnv, 0o755); err != nil {
		t.Fatalf("materialise the env-pinned cache dir %s: %v", fromEnv, err)
	}
	t.Setenv(goCacheEnvNameInTest, fromEnv)
	got, err = ResolveToolchainCachePath("")
	if err != nil {
		t.Fatalf("ResolveToolchainCachePath with $GOCACHE set: %v", err)
	}
	if want := CanonicalPath(fromEnv); got != want {
		t.Fatalf("$%s must outrank the go env file: got %q, want %q", goCacheEnvNameInTest, got, want)
	}

	// ③ the explicit path wins over both.
	explicit := filepath.Join(t.TempDir(), "gocache-from-the-flag")
	if err := os.MkdirAll(explicit, 0o755); err != nil {
		t.Fatalf("materialise the explicit cache dir %s: %v", explicit, err)
	}
	got, err = ResolveToolchainCachePath(explicit)
	if err != nil {
		t.Fatalf("ResolveToolchainCachePath with an explicit path: %v", err)
	}
	if want := CanonicalPath(explicit); got != want {
		t.Fatalf("the explicit path must outrank both: got %q, want %q", got, want)
	}
}

// TestResolveToolchainCachePathFollowsTheToolchain pins the path resolution's
// three channels: the explicit path, the toolchain's OWN GOCACHE, and the
// documented default. A report about a directory `go` is not using would be a
// confident wrong answer, which is why GOCACHE is honoured rather than guessed.
func TestResolveToolchainCachePathFollowsTheToolchain(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "explicit-cache")
	got, err := ResolveToolchainCachePath(explicit)
	if err != nil {
		t.Fatalf("ResolveToolchainCachePath: %v", err)
	}
	if got != CanonicalPath(explicit) {
		t.Fatalf("explicit path: got %q, want %q", got, CanonicalPath(explicit))
	}
	env := t.TempDir()
	t.Setenv("GOCACHE", env)
	got, err = ResolveToolchainCachePath("")
	if err != nil {
		t.Fatalf("ResolveToolchainCachePath: %v", err)
	}
	if got != CanonicalPath(env) {
		t.Fatalf("GOCACHE: got %q, want %q", got, CanonicalPath(env))
	}
}

// TestMeasureToolchainCacheAbsentDirectoryIsNotAnError pins the read-only face's
// contract: a cache that does not exist yet is reported as an empty cache whose
// FILESYSTEM is still measured — the ordering that makes the criterion answerable
// before anything is created.
func TestMeasureToolchainCacheAbsentDirectoryIsNotAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created-yet", "go-build")
	rep, err := MeasureToolchainCache(missing)
	if err != nil {
		t.Fatalf("an absent cache must be a report, not an error: %v", err)
	}
	if rep.Total != 0 || len(rep.Shards) != 0 {
		t.Fatalf("an absent cache must measure as empty, got body=%d shards=%d", rep.Total, len(rep.Shards))
	}
	if rep.Device == 0 || rep.FSFree == 0 {
		t.Fatal("the filesystem that WOULD hold the cache must still be measured")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("measuring must never create the cache directory")
	}
}

// TestToolchainCacheEntryName pins the ENTRY-shape predicate the whole reclaim
// hangs on: a name is a cache entry only when it is a 64-hex id plus the
// toolchain's own "-a"/"-d". Everything else — the bookkeeping, a shorter id, an
// upper-case id, a foreign file that merely ends in "-d" — is NOT an entry, so no
// reclaim can ever name it.
func TestToolchainCacheEntryName(t *testing.T) {
	entryID := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name string
		want string
		ok   bool
	}{
		{entryID + ToolchainCacheEntryIndexSuffix, entryID, true},
		{entryID + ToolchainCacheEntryDataSuffix, entryID, true},
		// A name that is not the toolchain's shape: kept, never reclaimed.
		{"README", "", false},
		{"trim.txt", "", false},
		{"CACHEDIR.TAG", "", false},
		// 16 hex (the merge gate's per-repo KEY shape) is not a toolchain entry.
		{strings.Repeat("b", 16) + ToolchainCacheEntryDataSuffix, "", false},
		// 63 or 65 hex is a malformed id, not an entry.
		{strings.Repeat("c", 63) + ToolchainCacheEntryIndexSuffix, "", false},
		{strings.Repeat("d", 65) + ToolchainCacheEntryIndexSuffix, "", false},
		// Upper case is not what `go` writes (%x), and this predicate is exact.
		{strings.ToUpper(strings.Repeat("e", 64)) + ToolchainCacheEntryDataSuffix, "", false},
		// A foreign file whose name merely ends in the suffix.
		{"my-cache-d", "", false},
	} {
		got, ok := ToolchainCacheEntryName(tc.name)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("ToolchainCacheEntryName(%q) = (%q, %t), want (%q, %t)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// TestSignedBytesKeepsTheSign is the regression guard for the one place a byte
// count can legitimately be NEGATIVE (the measured change in a filesystem's free
// space): rendering it through the unsigned formatter would wrap a small deficit
// into an absurd magnitude, and a wrong number that looks precise is the failure
// Rule 15 names. The minimal sample is the smallest negative value.
func TestSignedBytesKeepsTheSign(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "+0 bytes"},
		{1, "+1 bytes"},
		{-1, "-1 bytes"},
		{-bytesPerGiB, "-1.0 GiB"},
		{bytesPerGiB, "+1.0 GiB"},
		{-(bytesPerGiB + 1), "-1.0 GiB"},
	} {
		if got := SignedBytes(tc.in); got != tc.want {
			t.Fatalf("SignedBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
