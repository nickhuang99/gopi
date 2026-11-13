package tools

// Suite scratch-root isolation — the full-suite hang fix (t-1788361069,
// round 2: the connectionOpener hang that survived the WAL-first DDL fix).
//
// ROOT CAUSE (goroutine dump, 2026-09-10): the ONLY blocked goroutine in a
// hung suite is inside fsync(2), reached from a *WAL close-time checkpoint*:
//
//	harvestSeedRow (memory_sync_harvest_test.go:46, `defer db.Close()`)
//	  → database/sql.(*DB).Close  → modernc sqlite conn.Close
//	  → sqlite3_close_v2 → *WalClose → sqlite3WalCheckpoint → _full_fsync → Xfsync
//
// while every `database/sql.(*DB).connectionOpener` is merely idle in
// `select` — the openers are VICTIMS: the connection they want cannot be
// created because the process-wide modernc/sqlite mutex is held by that
// one fsync (or the pool is exhausted by a close that never returns).
//
// WHY ONE fsync HANGS THE WHOLE SUITE: the internal/tools suite creates
// hundreds of fresh temp DBs (434 t.TempDir() sites, 106 fresh-DB open
// sites), each of which is opened, initialized, written and CLOSED. WAL
// makes the DDL storm cheap, but SQLite still fsyncs the database file at
// the WAL close-time checkpoint. t.TempDir() lives under TMPDIR — the
// system default is /tmp, i.e. the BOOT DEVICE (/dev/sda3 here, 94% full,
// measured fsync latency 49-225ms, with multi-second stalls under the
// concurrent-agent I/O this host runs). ONE stalled fsync blocks minutes
// and every later test dies behind it — the suite's timeout picks whichever
// test happened to be running (today TestHarvestConflictSameStampBothTerminal,
// earlier TestImportReRunIsIdempotent / TestTasksMigrationFTSParityClassic /
// TestMemoryFeedbackSuggestCold — the moving target IS the symptom).
//
// The fix is test-side ISOLATION, not production semantics: the suite's
// throwaway scratch data belongs on a memory filesystem, where the close
// checkpoint sync is a no-op. TestMain pins TMPDIR through
// internal/testutil.PinSuiteTempRoot (the shared implementation of this pin
// — it used to live only here, which is why the internal/agent suite kept
// flaking on the same boot-device fsync; see
// devdoc/write-task-area-schema-timeout.md), so every t.TempDir() (and every
// scratch DB inside it) stops syncing to the boot device. Production
// behaviour is untouched.
//
// ROUND 3 (contention robustness, feat/freshdb-contention-guard): the hang
// guard used to decide on an ABSOLUTE wall clock (25ms/DB). That budget
// cannot tell the guard's own target (a block-device fsync at WAL close)
// from host CPU starvation: with the scratch root already on tmpfs the
// measured 40-66ms/DB under 4-core oversubscription (load 14-19) reds an
// otherwise green suite, and the message then asserted the tmpfs root
// "syncs at WAL close" — a claim its own statfs-contradicting evidence
// disproved. The verdict is now I/O-ATTRIBUTABLE: the same open→INSERT→
// Close cycle is timed on the scratch root AND on an fsync-free in-memory
// control DB in the same process, interleaved; only the RATIO decides.
// Contention inflates both loops together (ratio stays ~1-2x), while a
// scratch root that syncs at WAL close diverges by two orders of magnitude
// (measured /tmp vs in-memory: ~140x). The statfs classification of the
// root is reported in the failure text so the message can never again blame
// a block device when the root is a memory filesystem.

import (
	"context"
	"fmt"
	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/testutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	freshDBOpenCloseTries = 12
	// freshDBDBFileName names the fresh ledger DB the candidate loop creates
	// inside its own t.TempDir().
	freshDBDBFileName = "memory.db"

	// freshDBControlDSNPrefix / freshDBControlDSNSuffix build the DSN of the
	// guard's CONTROL loop: an in-memory SQLite database with a shared cache,
	// opened through the same openTasksDB path as the candidate. SQLite never
	// syncs it (there is no file backing), so it measures the same in-process
	// work the candidate does WITHOUT the filesystem term — the noise
	// reference the ratio is taken against. The unique per-iteration name
	// (pid + index) is required because the fresh-DB schema init keyed on a
	// DSN would otherwise be de-duplicated, and the trailing '&' lets
	// openDB append its own "_txlock=immediate" query parameter.
	freshDBControlDSNPrefix = "file:gopi-testsuite-control-"
	freshDBControlDSNSuffix = "?mode=memory&cache=shared&"

	// freshDBAttributionRatioMax is the largest MEDIAN per-iteration ratio
	// (candidate / fsync-free control) that still counts as "same I/O class".
	// It is what makes the verdict contention-proof: both loops run in this
	// process, interleaved, so CPU starvation inflates them together and the
	// ratio stays near 1 (measured 1.2-1.6x on tmpfs, under load 8-19).
	// A scratch root that syncs at WAL close diverges by two orders of
	// magnitude instead (measured 141-154x on this host's /tmp), so the
	// threshold sits an order of magnitude above the noise floor and an
	// order of magnitude below the divergence it must catch.
	//
	// It is the candidate/control ratio on ONE host measurement window, so on a
	// block-backed scratch root (the intended state since the 2026-09-18
	// ruling) it is a DETECTOR, not a budget: it reds when the candidate loop
	// stalls orders of magnitude beyond the identical in-process work.
	freshDBAttributionRatioMax = 12.0
	// freshDBStallCeiling is the per-iteration hard ceiling: no single
	// open→INSERT→Close of ONE fresh DB may take longer than this, whatever
	// the host. It is the guard's HANG detector — the original purpose, since
	// one stalled fsync blocks minutes — and NOT a filesystem-quality budget.
	//
	// The value is measured against the state the 2026-09-18 ruling created:
	// the scratch home is a real directory on the block device, where a single
	// fresh-DB cycle was measured at 2.73s under this host's ambient load
	// against a 31ms fsync-free in-memory control (tools suite, 2026-09-18).
	// The ceiling therefore sits an order of magnitude above that measured
	// worst case — a genuine hang is minutes, not seconds, and a tighter bound
	// would make a loaded host, not the code, decide the verdict.
	freshDBStallCeiling = 30 * time.Second
	// raceBudgetMultiplier widens a timing budget under -race, where the
	// instrumentation — not the filesystem — dominates the measurement.
	raceBudgetMultiplier = 20
)

// suiteBudget scales a timing budget by the race-detector multiplier when
// this binary is instrumented.
func suiteBudget(d time.Duration) time.Duration {
	if raceEnabled {
		return d * raceBudgetMultiplier
	}
	return d
}

// TestSuiteScratchRootIsPinned — assert TestMain actually moved the suite's
// scratch data off the system temp root (mutation guard for the isolation
// half of the fix: dropping pinSuiteTempRoot from TestMain makes this red).
func TestSuiteScratchRootIsPinned(t *testing.T) {
	if !testutil.SuiteTempRootPinned() {
		t.Skipf("no fast scratch root available (%s absent and no %s override) — scratch stays on the ambient temp root",
			testutil.ShmDir, testutil.SuiteTempRootEnvName)
	}
	if got := os.Getenv(testutil.TempDirEnvName); got != testutil.SuiteTempRoot() {
		t.Fatalf("%s = %q, want the pinned scratch root %q", testutil.TempDirEnvName, got, testutil.SuiteTempRoot())
	}
	dir := t.TempDir()
	if !strings.HasPrefix(dir, testutil.SuiteTempRoot()) {
		t.Fatalf("t.TempDir() = %q, want a path under the pinned scratch root %q", dir, testutil.SuiteTempRoot())
	}
}

// freshDBOpenWriteClose runs ONE fresh-ledger-DB open→INSERT→Close cycle at
// the given path (or DSN — the control loop passes an in-memory DSN) and
// returns how long the cycle took. The cycle is byte-for-byte the step the
// suite hang blocked in: close is where SQLite checkpoints the WAL.
func freshDBOpenWriteClose(ctx context.Context, path string) (time.Duration, error) {
	start := time.Now()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		return 0, fmt.Errorf("openTasksDB: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, payload, status, tags, source, priority, created_at, updated_at)
		 VALUES ('t','s','p','pending','','auto',5,1,1)`); err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return 0, fmt.Errorf("insert: %w", err)
	}
	if err := db.Close(); err != nil {
		return 0, fmt.Errorf("close (WAL checkpoint): %w", err)
	}
	return time.Since(start), nil
}

// freshDBControlOpenWriteClose runs the same cycle against the fsync-free
// in-memory control DB of iteration i. A keeper connection holds the
// shared-cache in-memory database alive across the cycle's internal
// open/close, so the cycle performs the identical schema init + insert +
// WAL-close work the candidate does — with no filesystem to sync.
func freshDBControlOpenWriteClose(ctx context.Context, i int) (time.Duration, error) {
	dsn := fmt.Sprintf("%s%d-%d%s", freshDBControlDSNPrefix, os.Getpid(), i, freshDBControlDSNSuffix)
	// The keeper goes through the production opener (which serializes one
	// connection per DB and validates the DSN) so the control never needs a
	// raw driver name of its own.
	keeper, err := openDB(dsn, false)
	if err != nil {
		return 0, fmt.Errorf("control keeper open: %w", err)
	}
	defer keeper.Close()
	if err := keeper.Ping(); err != nil {
		return 0, fmt.Errorf("control keeper ping: %w", err)
	}
	return freshDBOpenWriteClose(ctx, dsn)
}

// freshDBMedian returns the median of the samples (a copy — the caller's
// slice is never reordered).
func freshDBMedian(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func freshDBMax(ds []time.Duration) time.Duration {
	var max time.Duration
	for _, d := range ds {
		if d > max {
			max = d
		}
	}
	return max
}

// TestFreshDBOpenCloseIsFast — the hang guard. It reproduces the exact
// blocked step from the suite hang: a fresh ledger DB opened, written and
// CLOSED (the close is where SQLite checkpoints the WAL and fsyncs). On a
// syncing scratch root that close stalls for seconds-to-minutes and the
// whole suite dies behind it; on the pinned tmpfs root the close costs
// nothing.
//
// The verdict is a RATIO, not a wall clock: every iteration times the
// candidate cycle (scratch root) and an fsync-free in-memory control cycle
// (same work, no file), interleaved in this process. CPU contention slows
// both, so the guard no longer reds a healthy suite on a loaded host; a
// scratch root that syncs at WAL close slows only the candidate and
// diverges by orders of magnitude. statfs classifies the root so the
// failure text can only blame a block device when the root really is one.
//
// Reproduce the pre-fix behaviour without editing code:
//
//	GOPI_TEST_TMPDIR=/tmp go test ./internal/tools/ -run TestFreshDBOpenCloseIsFast -count=1
//
// → red (the candidate loop diverges from the in-memory control), while the
// same test without the override is green.
// Root cause of the tail this file documents (2026-09-13, measured): the
// 60-285s per-test times are NOT sqlite work. `TaskShow`'s session-lookup
// helpers rescan the operator's ENTIRE sessions tree — ~56k entries / 16 GB on
// this host — once per call (taskFindSessionJSONL readdirs every subdirectory
// and reads the first line of every *.jsonl; taskSessionLogPath,
// taskRawLogPathFromSession and taskModelFromSession each call it, and
// taskBriefingFromSession calls it again). A/B at equal load, one probe
// process: 8.9s per helper with the ambient GOPI_HOME vs 30us with an empty
// GOPI_HOME (300,000x), while taskEventLatest / TaskReportShowBlock /
// taskWorkerOutputLogLine stay in the microseconds on both branches — so the
// cost is the tree scan, not the ledger, not the test.
func TestFreshDBOpenCloseIsFast(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	ctx := context.Background()
	root := testutil.SuiteTempRoot()
	memBacked, fsDescription := testutil.ScratchRootFilesystem(root)
	ceiling := suiteBudget(freshDBStallCeiling)

	cand := make([]time.Duration, 0, freshDBOpenCloseTries)
	ctrl := make([]time.Duration, 0, freshDBOpenCloseTries)
	for i := 0; i < freshDBOpenCloseTries; i++ {
		candDur, err := freshDBOpenWriteClose(ctx, filepath.Join(t.TempDir(), freshDBDBFileName))
		if err != nil {
			t.Fatalf("iter %d: candidate DB on the scratch root %s: %v", i, root, err)
		}
		ctrlDur, err := freshDBControlOpenWriteClose(ctx, i)
		if err != nil {
			// The control loop is the measuring instrument: without it the
			// ratio cannot be taken, so this is a failure, not a skip — and
			// never a silent pass (Rule 15).
			t.Fatalf("iter %d: fsync-free in-memory control DB: %v", i, err)
		}
		cand = append(cand, candDur)
		ctrl = append(ctrl, ctrlDur)
		if candDur > ceiling {
			t.Fatalf("one fresh DB on the scratch root %q (%s) took %v — over the %v per-DB stall ceiling — after %d clean iterations; the fsync-free in-memory control DB took %v. %s",
				root, fsDescription, candDur, ceiling, i, ctrlDur,
				freshDBStallAttribution(memBacked, root, fsDescription))
		}
	}

	candMed, ctrlMed := freshDBMedian(cand), freshDBMedian(ctrl)
	ratio := float64(candMed) / float64(ctrlMed)
	t.Logf("scratch root %q: %s; fresh DB open+INSERT+close on the root: median %v, max %v (%d DBs); same cycle on the fsync-free in-memory control DB: median %v, max %v → median ratio %.2fx (limit %.1fx)",
		root, fsDescription, candMed, freshDBMax(cand), len(cand), ctrlMed, freshDBMax(ctrl), ratio, freshDBAttributionRatioMax)

	if ratio <= freshDBAttributionRatioMax {
		return
	}
	if memBacked {
		// Statfs rules out the guard's own target: a memory filesystem
		// cannot send an fsync to a block device. Say so, or the next
		// agent chases an fsync that does not exist (Rule 15).
		t.Fatalf("the scratch root %q is %s, so a block-device fsync at WAL close is ruled out by statfs — yet the fresh-DB cycle on it measured %v (median) vs %v (median) for the identical fsync-free in-memory control cycle in this same process: %.1fx (limit %.1fx). One of the two loops stalled inside the process (host CPU/IO oversubscription, or a lock wait in the modernc/sqlite path) — re-run on an idle host before treating the scratch root as defective. Raw candidate samples: %v.",
			root, fsDescription, candMed, ctrlMed, ratio, freshDBAttributionRatioMax, cand)
	}
	t.Fatalf("the scratch root %q is %s: %d fresh DBs took %v (median %v each) while the identical fsync-free in-memory control cycle took %v (median %v each) — a median ratio of %.1fx against a %.1fx limit. The scratch root pays a block-device sync at WAL close; ONE stalled fsync hangs the whole suite behind the process-wide sqlite lock (the connectionOpener hang, t-1788361069). That is the operator's accepted cost since the 2026-09-18 ruling (the memory-backed scratch root is RETIRED — %s is no longer a creation target), so the remedy is to attribute the cost, not to move the root: re-run on an idle host and check the host's I/O load. %s = %q. Raw candidate samples: %v.",
		root, fsDescription, len(cand), sumDurations(cand), candMed, sumDurations(ctrl), ctrlMed, ratio, freshDBAttributionRatioMax,
		testutil.ShmDir, testutil.TempDirEnvName, os.Getenv(testutil.TempDirEnvName), cand)
}

// freshDBStallAttribution words the per-DB stall ceiling failure truthfully:
// a memory-backed root is never blamed on an fsync, a block-backed one is.
// The block-backed arm states the measured cost and the ruling that accepted
// it — it must NOT prescribe the retired memory base (2026-09-18; the
// assertion in TestScratchRootFilesystemClassification pins that).
func freshDBStallAttribution(memBacked bool, root, fsDescription string) string {
	if memBacked {
		return fmt.Sprintf("The root is %s, so this is NOT a block-device fsync — the process itself was stalled (host CPU/IO oversubscription, or a lock wait). Re-run on an idle host.", fsDescription)
	}
	return fmt.Sprintf("The root is %s, so the WAL-close checkpoint this guard exists to catch (the connectionOpener hang, t-1788361069) writes to that device: that cost is the operator's accepted trade (2026-09-18 ruling retired the memory-backed scratch home). Attribute it before blaming the root: re-run on an idle host, and check the host's I/O load.", fsDescription)
}

// sumDurations totals a sample slice for the diagnostic message.
func sumDurations(ds []time.Duration) time.Duration {
	var tot time.Duration
	for _, d := range ds {
		tot += d
	}
	return tot
}

// TestSuiteScratchRootBackingIsKnown — the mechanism behind the guard,
// asserted deterministically instead of by wall clock.
//
// IT USED TO REQUIRE MEMORY BACKING ("the pinned scratch root must sit on a
// memory filesystem"). The operator's 2026-09-18 ruling ("just for a few ms
// performance? no, no no! don't use tmpfs. change settings to workspace. now")
// retired that requirement: the scratch home is a real directory on the block
// device, so a block-backed root is the INTENDED state and the old assertion
// would be red on a correctly configured host. What survives — and what the
// guard was really for — is that the backing is MEASURED and NAMED rather than
// assumed, and that the root is off the `/` device (AssertSuiteScratchRootSafe
// owns the hard half of that; this test reports the classification a reader
// needs to attribute a slow run).
//
// When such a root IS slow, the cost is real and diagnosed by
// TestFreshDBOpenCloseIsFast's per-DB stall ceiling — which, since the ruling,
// can no longer prescribe a tmpfs and reports the measured device instead.
func TestSuiteScratchRootBackingIsKnown(t *testing.T) {
	if !testutil.SuiteTempRootPinned() {
		t.Skipf("no scratch root pinned — scratch stays on the ambient temp root")
	}
	root := testutil.SuiteTempRoot()
	memBacked, description := testutil.ScratchRootFilesystem(root)
	if description == "" {
		t.Fatalf("scratch root %s has no filesystem classification — the backing of a scratch root is "+
			"never \"unknown\": the classification is what lets a slow run be attributed (Rule 15)", root)
	}
	t.Logf("scratch root %s: %s (memory-backed=%t); %s is the scrubbed legacy memory base, no longer a "+
		"creation target (operator ruling 2026-09-18)", root, description, memBacked, testutil.ShmDir)
	if testutil.SuiteTempRootOverride() != "" {
		t.Skipf("%s pins the scratch root explicitly — its backing is the operator's own choice", testutil.SuiteTempRootEnvName)
	}
	if err := testutil.AssertSuiteScratchRootSafe("tools"); err != nil {
		t.Fatalf("the pinned scratch root is not a usable one: %v", err)
	}
}

// TestScratchRootFilesystemClassification — minimal repro guard for the
// false-red that motivated the I/O attribution (feat/freshdb-contention-guard):
// the guard's failure text used to assert "the scratch root syncs at WAL
// close" while printing a /dev/shm root. The classifier must answer from
// statfs, and an unreadable root must be reported as unknown rather than
// silently defaulting to either class (Rule 15).
func TestScratchRootFilesystemClassification(t *testing.T) {
	if fi, err := os.Stat(testutil.ShmDir); err == nil && fi.IsDir() {
		memBacked, description := testutil.ScratchRootFilesystem(testutil.ShmDir)
		if !memBacked {
			t.Fatalf("%s classified as %s, want a memory filesystem", testutil.ShmDir, description)
		}
	}
	// The failure text must never contradict the measurement: a memory-backed
	// root may not be blamed on a block-device fsync, and a block-backed root
	// must not be told to go back to the retired memory base (2026-09-18
	// ruling). Both halves are asserted here, on whatever root this host
	// actually pinned.
	if testutil.SuiteTempRootPinned() && testutil.SuiteTempRootOverride() == "" {
		memBacked, description := testutil.ScratchRootFilesystem(testutil.SuiteTempRoot())
		if memBacked && strings.Contains(freshDBStallAttribution(true, testutil.SuiteTempRoot(), description), "WAL-close checkpoint fsync") {
			t.Fatalf("stall attribution for a memory-backed root still asserts the block-device fsync: %s",
				freshDBStallAttribution(true, testutil.SuiteTempRoot(), description))
		}
		if !memBacked {
			attribution := freshDBStallAttribution(false, testutil.SuiteTempRoot(), description)
			if strings.Contains(attribution, testutil.ShmDir) {
				t.Fatalf("the block-backed attribution still prescribes the retired memory base %s: %s",
					testutil.ShmDir, attribution)
			}
			if !strings.Contains(attribution, "device") {
				t.Fatalf("the block-backed attribution names no device cost: %s", attribution)
			}
		}
	}
	// Unknown root: an unreadable path is neither class — say so, never guess.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if memBacked, description := testutil.ScratchRootFilesystem(missing); memBacked || !strings.Contains(description, "statfs") {
		t.Fatalf("testutil.ScratchRootFilesystem(%q) = (%v, %q), want (false, a statfs failure description)", missing, memBacked, description)
	}
}

// TestFreshDBAttributionRatioIsIOAttributable — the verdict is a same-process
// RATIO, not a wall clock: with both loops timed in this process the scratch
// root's own ratio must sit far under the attribution limit. This is the
// regression guard for the contention false red (the old 25ms/DB absolute
// budget reded a healthy tmpfs-backed suite at load 14-19).
func TestFreshDBAttributionRatioIsIOAttributable(t *testing.T) {
	if !testutil.SuiteTempRootPinned() {
		t.Skipf("no scratch root pinned — the ratio needs a scratch root to compare against")
	}
	if _, description := testutil.ScratchRootFilesystem(testutil.SuiteTempRoot()); !strings.Contains(description, "memory filesystem") {
		t.Skipf("scratch root %s is %s — the ratio limit only holds on a memory filesystem", testutil.SuiteTempRoot(), description)
	}
	ctx := context.Background()
	cand, ctrl := make([]time.Duration, 0, freshDBOpenCloseTries), make([]time.Duration, 0, freshDBOpenCloseTries)
	for i := 0; i < freshDBOpenCloseTries; i++ {
		candDur, err := freshDBOpenWriteClose(ctx, filepath.Join(t.TempDir(), freshDBDBFileName))
		if err != nil {
			t.Fatalf("iter %d: candidate: %v", i, err)
		}
		ctrlDur, err := freshDBControlOpenWriteClose(ctx, i)
		if err != nil {
			t.Fatalf("iter %d: control: %v", i, err)
		}
		cand = append(cand, candDur)
		ctrl = append(ctrl, ctrlDur)
	}
	ratio := float64(freshDBMedian(cand)) / float64(freshDBMedian(ctrl))
	if ratio > freshDBAttributionRatioMax {
		t.Fatalf("median ratio %.2fx on the memory-backed scratch root %s exceeds the %.1fx attribution limit — the control loop is not measuring the same in-process work as the candidate (candidate median %v over %d DBs, control median %v)",
			ratio, testutil.SuiteTempRoot(), freshDBAttributionRatioMax, freshDBMedian(cand), len(cand), freshDBMedian(ctrl))
	}
}
