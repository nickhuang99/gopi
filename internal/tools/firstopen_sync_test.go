package tools

// firstopen_sync_test.go — the reproduction and acceptance tests for the
// first-open durability decision (see firstOpenSyncMode in memory.go and
// devdoc/first-open-durability.md).
//
// Three claims, four tests, because they fail for different reasons:
//
//  1. the DECISION is made by the MEASURED filesystem, never a path spelling
//     (Rule 22) — a decision table;
//  2. the COST the decision removes is the sync itself — a same-file A/B on a
//     real block device, where the ONLY difference is the pragma, decided by a
//     paired t-statistic over ABBA-ordered pairs rather than one sample per arm
//     (see devdoc/firstopen-ab-contention.md);
//  3. the non-production mode does NOT leak onto the handles the caller gets
//     afterwards — production durability is untouched;
//  4. (the instrument's own regression guard, claim 2's) the statistic above
//     does not flip under a noisy neighbour, and it still FAILS a genuine
//     misattribution — a decision table over synthetic samples, no device.
//
// The statistic itself lives in firstopen_sync_decision.go as a pure function
// over a sample set, so claim 4 can be tested without touching a device and
// without generating load (Rule 27: no test in this repo may saturate a shared
// resource to make a number appear).

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// The in-test A/B's constants. Every one is named (Rule 1a) because the
// assertion's meaning is encoded in the values: firstOpenABPairs fixes the
// statistic's N, and firstOpenABMinSignal fixes what "the host can see the sync
// at all" means.
const (
	// firstOpenABPairs is how many pairs the A/B builds — each pair is FOUR fresh
	// ledgers, so a run is firstOpenABPairs*firstOpenABBuildsPerPair first opens.
	//
	// 32 is the calibrated value (devdoc/firstopen-ab-contention.md,
	// ".evid4/calibration"): against this host's MEASURED per-build noise
	// (σ ≈ 0.12 log units, from 1000+ first opens) it reports the wrong direction
	// in 0.17% of runs, passes with ~62% probability at the real ~10% effect and
	// skips the rest. Fewer pairs lose power (half the runs skip); more cost smoke
	// time without moving a risk that is already a probability rather than a coin
	// flip.
	firstOpenABPairs = 32
	// firstOpenABBuildsPerPair is the quadruple: both arms appear TWICE per pair,
	// which is what lets the order effects cancel (firstOpenABPairOrderFor).
	firstOpenABBuildsPerPair = 4
	// firstOpenABMinSignalMs is the smallest NORMAL-arm MEDIAN cost (in
	// milliseconds, the sampler's unit) at which the A/B may assert anything.
	//
	// It is deliberately far below the single-sample instrument's 25ms: that
	// floor existed because the old assertion compared TWO MAGNITUDES across a
	// gap, so it needed the gap to be visibly large. The paired majority compares
	// each arm against the OTHER ARM OF ITS OWN PAIR, so it needs no absolute
	// signal to be meaningful — it needs only that the sync can be visible at
	// all, i.e. that a first open is not over in microseconds. Below this the
	// pairs are measurement quantisation and ties, and the run is skipped with
	// its distribution rather than asserted.
	firstOpenABMinSignalMs = 2.0
	// firstOpenABNoiseMultiplier is the "a noisy neighbour is present" line: a
	// sample above this multiple of its own arm's median is not a measurement of
	// the pragma. It does not decide anything — the count is reported, so a run
	// whose verdict sits next to heavy noise is visible as such.
	firstOpenABNoiseMultiplier = 3.0
)

// TestFirstOpenSyncModeDecisionTable pins claim 1: the mode is chosen from the
// measured backing filesystem of the ledger's own directory, and every path that
// is not PROVEN memory-backed keeps production's durable mode.
//
// THE CONTRACT IS MEASURED, NOT ENCODED IN A FIXTURE'S LOCATION (2026-09-18).
// This table used to call t.TempDir() "the suite's pinned scratch root: a memory
// filesystem" and expect the relaxed mode there. That was a claim about the HOST
// dressed as a claim about the decision, and 524156af retired its premise: the
// scratch home is now an ordinary directory on the block device, so the
// framework temp dir is block-backed and the arm asserted the opposite of what
// the code correctly does (red at 8b0011a3; at 524156af the same file SKIPPED —
// two tips, two outcomes, one wrong premise).
//
// So the memory arm is DERIVED: a directory that MEASURES as a memory filesystem
// is used when the host has one, and when it does not the arm is asserted
// SYMBOLICALLY — i.e. against what the decision is a function OF. The symmetry
// is the point, and it is why the symbolic arm is not a weaker test: the
// in-repo fixture cannot create a tmpfs, so on a block-backed host every
// reachable path is block-backed, and asserting only the reached path would
// leave the memory→relaxed half of the contract untested on exactly the hosts
// that run the suite. The mapping itself is a total function of one boolean
// (firstOpenLedgerSyncMode), so pinning it symbolically plus pinning the
// measurement on a real path covers the contract.
func TestFirstOpenSyncModeDecisionTable(t *testing.T) {
	deviceDir := firstOpenDeviceScratch(t)
	memoryDir, ramDescription := firstOpenMemoryScratch()

	cases := []struct {
		name string
		path string
		want string
		why  string
	}{
		{
			name: "device-filesystem-ledger",
			path: filepath.Join(deviceDir, "memory.db"),
			want: ledgerSyncMode,
			why:  "a ledger on a block device keeps paying for durability — losing it is real data loss",
		},
		{
			name: "unmeasurable-directory",
			path: filepath.Join(deviceDir, "no-such-dir", "memory.db"),
			want: ledgerSyncMode,
			why:  "the burden of proof is on the cheap path: an unmeasurable directory is NOT proven memory-backed",
		},
	}
	if memoryDir != "" {
		cases = append(cases, struct {
			name string
			path string
			want string
			why  string
		}{
			name: "memory-filesystem-ledger",
			path: filepath.Join(memoryDir, "memory.db"),
			want: ledgerSyncModeWorkingFS,
			why:  "the schema is built into a fresh file on a memory filesystem, where a sync protects nothing",
		})
		t.Logf("the memory arm is measured on %s (%s)", memoryDir, ramDescription)
	} else {
		t.Logf("no memory filesystem on this host (%s) — the memory arm is asserted against the decision "+
			"function itself, and every path this host can reach is asserted to keep the durable mode", ramDescription)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstOpenSyncMode(tc.path); got != tc.want {
				t.Errorf("firstOpenSyncMode(%q) = %q, want %q — %s", tc.path, got, tc.want, tc.why)
			}
		})
	}

	// The same decision, read from its own function rather than through a path —
	// the half above cannot reach when no memory filesystem exists here. It
	// tests what firstOpenSyncMode is: a mapping from the MEASURED backing to a
	// mode.
	for _, tc := range []struct {
		name    string
		memory  bool
		want    string
		journey string
	}{
		{
			name:    "memory-filesystem-ledger",
			memory:  true,
			want:    ledgerSyncModeWorkingFS,
			journey: "a memory filesystem is measured: the sync protects nothing, so the build runs relaxed",
		},
		{
			name:    "block-device-ledger",
			memory:  false,
			want:    ledgerSyncMode,
			journey: "anything not proven memory-backed keeps production's durable mode",
		},
	} {
		if got := firstOpenLedgerSyncMode(tc.memory); got != tc.want {
			t.Errorf("firstOpenLedgerSyncMode(memory=%v) = %q, want %q — %s", tc.memory, got, tc.want, tc.journey)
		}
	}

	if ledgerSyncModeWorkingFS == ledgerSyncMode {
		t.Fatalf("the working-filesystem mode and production's mode are the same value %q — the decision "+
			"would be a no-op and this whole test vacuous", ledgerSyncMode)
	}
}

// firstOpenMemoryScratch returns a directory the KERNEL reports as a memory
// filesystem, or "" (with a description of every candidate and why it was
// rejected) on a host that has none. Candidates are the standard Linux memory
// mounts plus the framework temp root — a suite on a host that still pins its
// scratch home to RAM lands there — and each is classified by statfs, never by
// its name (Rule 22: measured, never a spelling).
//
// No fixture is created: the candidate is used as the ledger's DIRECTORY (the
// measurement is taken on it), so the caller may point a path inside it.
func firstOpenMemoryScratch() (string, string) {
	var rejected []string
	for _, dir := range []string{worktreeroots.ShmDir, firstOpenRunShmDir, scratch.HomeDir()} {
		mem, description, err := scratch.MemoryFilesystem(dir)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s (%v)", dir, err))
			continue
		}
		if mem {
			return dir, description
		}
		rejected = append(rejected, fmt.Sprintf("%s (%s)", dir, description))
	}
	return "", "measured candidates: " + strings.Join(rejected, "; ")
}

// firstOpenRunShmDir is the second standard shared-memory mount on a Linux host.
// It is NOT a copy of worktreeroots.ShmDir: /run/shm is a separate mount point
// (on some hosts a symlink to /dev/shm, here possibly its own tmpfs), and it is
// named rather than inlined so the candidate list doubles as documentation of
// what the measurement probes (Rule 1b).
const firstOpenRunShmDir = "/run/shm"

// TestFirstOpenKeepsProductionDurabilityOffTheWorkingPath pins claim 3:
// journal_mode is a persistent header property but synchronous is PER
// CONNECTION, so relaxing the mode for the first-open build must not reach any
// handle a caller opens afterwards. Reading `PRAGMA synchronous` back from a
// fresh handle is the direct measurement.
func TestFirstOpenKeepsProductionDurabilityOffTheWorkingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "durability.db")
	if err := initKnowledgeBase(context.Background(), path); err != nil {
		t.Fatalf("first open: %v", err)
	}
	if got := firstOpenSyncMode(path); got != ledgerSyncModeWorkingFS {
		t.Skipf("this scratch root is not memory-backed (mode %q) — the leak this test guards only exists on "+ //stylegate:ignore the skip reason is the measured mode, which is dynamic by construction
			"the working path", got)
	}
	db, err := openDB(path, false)
	if err != nil {
		t.Fatalf("open after the first build: %v", err)
	}
	defer db.Close()
	var mode int
	if err := db.QueryRow("PRAGMA synchronous").Scan(&mode); err != nil {
		t.Fatalf("read the later handle's synchronous: %v", err)
	}
	if mode != firstOpenSyncFull {
		t.Errorf("a handle opened AFTER the first build reads synchronous=%d, want %d (SQLite's default FULL): "+
			"the first-open mode leaked past its own connection", mode, firstOpenSyncFull)
	}
}

// TestFirstOpenBlockDeviceCostIsTheSync pins claim 2 with a NOISE-TOLERANT
// instrument: firstOpenABPairs fresh ledgers per arm, built in INTERLEAVED
// pairs on the same block-device directory, with the synchronous= pragma as the
// only difference inside a pair. The verdict is the paired majority
// (firstOpenABDecide, a pure function in firstopen_sync_decision.go), never one
// sample of each arm.
//
// WHY this shape (devdoc/firstopen-ab-contention.md): one sample per arm asserts
// an ordering that a single contended sample can invert — a shared block
// device's fsync costs sub-10 ms when idle and 0.7-1.3 s under pressure, so the
// noise is larger than the effect. Interleaving removes the drift a
// measure-arm-A-then-arm-B layout accumulates, and the majority removes the
// single-outlier flip. The distribution is logged, so "the test passed" is
// always accompanied by the numbers it passed on.
//
// It asserts a DIRECTION with a majority, never an absolute wall time, so a busy
// or idle host cannot make it red for a reason unrelated to the mechanism; when
// the host is too quiet to see a sync at all (median below
// firstOpenABMinSignalMs) or the majority is a tie, it skips and reports the
// distribution.
func TestFirstOpenBlockDeviceCostIsTheSync(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	dir := firstOpenDeviceScratch(t)

	pairDeltas := make([]float64, 0, firstOpenABPairs)
	durableMs := make([]float64, 0, firstOpenABPairs*2)
	relaxedMs := make([]float64, 0, firstOpenABPairs*2)
	for i := 0; i < firstOpenABPairs; i++ {
		// ABBA: durable, relaxed, relaxed, durable. The second half mirrors the
		// first, so a device that gets slower during the pair (a neighbour
		// arriving, a journal queue draining) contributes EQUALLY to both arms and
		// cancels in the difference instead of landing on the second arm.
		for n, arm := range firstOpenABPairOrderFor(i) {
			wallMs := firstOpenMeasureBuild(t, filepath.Join(dir,
				fmt.Sprintf("%s%d-%s%d%s", firstOpenABDBPrefix, i, arm, n, firstOpenABDBExt)),
				firstOpenABSyncMode(arm))
			if arm == firstOpenABArmDurable {
				durableMs = append(durableMs, wallMs)
				continue
			}
			relaxedMs = append(relaxedMs, wallMs)
		}
		pairDeltas = append(pairDeltas, firstOpenABPairDelta(durableMs[len(durableMs)-2:], relaxedMs[len(relaxedMs)-2:]))
	}

	decision := firstOpenABDecide(pairDeltas)
	durableMedian := firstOpenABMedian(durableMs)
	relaxedMedian := firstOpenABMedian(relaxedMs)
	t.Logf("same-file A/B on %s over %d order-balanced pairs (%d fresh ledgers): synchronous(%s) median "+
		"%.3fms, samples %v; synchronous(%s) median %.3fms, samples %v; paired difference "+
		"(relaxed-durable) mean %+.3fms, median %+.3fms; pairs by winner relaxed %d / durable %d of %d "+
		"ordered; signed ranks W- %.1f, W+ %.1f, required %.1f (false-lead probability under %v); "+
		"verdict %s (noisy samples >%.0fx median: %d/%d)",
		dir, decision.Pairs, len(durableMs)+len(relaxedMs), ledgerSyncMode, durableMedian,
		firstOpenABMs(durableMs), ledgerSyncModeWorkingFS, relaxedMedian, firstOpenABMs(relaxedMs),
		decision.MeanDeltaMs, decision.MedianDeltaMs, decision.RelaxedCheaperPairs,
		decision.DurableCheaperPairs, decision.UsablePairs, decision.RelaxedRankSum,
		decision.DurableRankSum, decision.CriticalRankSum, firstOpenABMaxFalseVerdictRate,
		decision.Verdict, firstOpenABNoiseMultiplier, firstOpenABNoisyCount(durableMs, durableMedian)+
			firstOpenABNoisyCount(relaxedMs, relaxedMedian), len(durableMs)+len(relaxedMs))

	if durableMedian < firstOpenABMinSignalMs {
		t.Skipf("no measurable sync signal on this host right now (%s arm median %.3fms < %v) — every "+ //stylegate:ignore the skip reason is the measured arm median, which is dynamic by construction
			"pair would be a coin flip; measured distribution above",
			ledgerSyncMode, durableMedian, firstOpenABMinSignalMs)
	}
	if decision.Verdict == firstOpenABVerdictAmbiguous {
		t.Skipf("the paired evidence does not separate the arms over %d order-balanced pairs (mean "+ //stylegate:ignore the skip reason is the measured rank statistic, which is dynamic by construction
			"%+.3fms, median %+.3fms, W- %.1f, W+ %.1f, needed %.1f): the host's noise is as large as "+
			"the effect, so this run cannot say anything about the mechanism — measured distribution above",
			decision.Pairs, decision.MeanDeltaMs, decision.MedianDeltaMs, decision.RelaxedRankSum,
			decision.DurableRankSum, decision.CriticalRankSum)
	}
	if err := firstOpenABMisattribution(decision); err != nil {
		t.Error(err)
	}
}

// The two pair orders, named (Rule 1b). Each is palindromic in arm labels and
// balanced 2-2: the palindrome makes a monotone drift during the pair cancel
// exactly, and the balance makes a SHORT burst fall as often on one arm as the
// other once the two orders below alternate.
const (
	// firstOpenABOrderDurableFirst is ABBA: durable, relaxed, relaxed, durable.
	firstOpenABOrderDurableFirst = "ABBA"
	// firstOpenABOrderRelaxedFirst is its mirror, RDDR: the same palindrome with
	// the arms exchanged.
	firstOpenABOrderRelaxedFirst = "RDDR"
)

// firstOpenABPairOrderFor returns the build order of pair i: mirrored ABBA, with
// the mirror flipped on odd pairs. The flip is the second half of the order
// balance — a palindrome alone cancels a DRIFT (a cost that changes steadily
// across the four builds), but a short burst that covers the two middle builds
// still lands entirely on whichever arm sits there, so the mirrored order is used
// on half the pairs to make that a fair coin instead of a bias.
func firstOpenABPairOrderFor(pair int) [firstOpenABBuildsPerPair]string {
	return firstOpenABPairOrderForOrder(firstOpenABPairOrderName(pair))
}

// firstOpenABPairOrderName names pair i's order, alternating the two.
func firstOpenABPairOrderName(pair int) string {
	if pair%2 == 0 {
		return firstOpenABOrderDurableFirst
	}
	return firstOpenABOrderRelaxedFirst
}

// firstOpenABPairOrderForOrder is the one place an order NAME becomes a build
// sequence, so the sampler and the tests cannot disagree about what the orders
// are.
func firstOpenABPairOrderForOrder(name string) [firstOpenABBuildsPerPair]string {
	if name == firstOpenABOrderRelaxedFirst {
		return [firstOpenABBuildsPerPair]string{
			firstOpenABArmRelaxed, firstOpenABArmDurable, firstOpenABArmDurable, firstOpenABArmRelaxed,
		}
	}
	return [firstOpenABBuildsPerPair]string{
		firstOpenABArmDurable, firstOpenABArmRelaxed, firstOpenABArmRelaxed, firstOpenABArmDurable,
	}
}

// firstOpenABPairDelta is a pair's relaxed-minus-durable difference in
// milliseconds: the MEAN of the two relaxed builds minus the MEAN of the two
// durable builds. NEGATIVE means the relaxed arm was cheaper, the expected
// direction.
//
// Averaging (rather than a single difference) is the second half of the drift
// cancellation: the two positions of each arm are averaged BEFORE they are
// compared, so a drift that moves both positions of an arm together — the same
// direction for the whole run — cancels within the arm instead of being read as
// an effect.
func firstOpenABPairDelta(durablePair, relaxedPair []float64) float64 {
	if len(durablePair) == 0 || len(relaxedPair) == 0 {
		return 0
	}
	return firstOpenABMean(relaxedPair) - firstOpenABMean(durablePair)
}

// firstOpenABMean is the arithmetic mean of a sample set (0 for none), so the
// pair construction above and the statistic cannot disagree about what a pair's
// value is.
func firstOpenABMean(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range samples {
		sum += x
	}
	return sum / float64(len(samples))
}

// The two arms of the A/B, named (Rule 1b): the labels are used as log text, as
// database file name stems, and as the ordering key in firstOpenABArmOrder, so a
// re-typed literal at any of those sites would silently decouple them.
const (
	firstOpenABArmDurable = "durable"
	firstOpenABArmRelaxed = "relaxed"
)

// firstOpenABMicrosPerMilli converts the sampler's microsecond durations to the
// milliseconds the statistic and the log line speak in (Rule 1a).
const firstOpenABMicrosPerMilli = 1000.0

// firstOpenABDBPrefix and firstOpenABDBExt name the per-arm database file inside
// the device-scratch directory (Rule 1b): the arm label is carried by the file
// name, so a failure leaves behind the arm that produced it.
const (
	firstOpenABDBPrefix = "firstopen-ab-"
	firstOpenABDBExt    = ".db"
)

// firstOpenABSyncMode maps an arm name to the SQLite synchronous= mode it
// builds under — the A/B's ONLY variable. It is a function, not a second copy
// of the mapping, so the instrument cannot drift from the decision it measures.
func firstOpenABSyncMode(arm string) string {
	if arm == firstOpenABArmDurable {
		return ledgerSyncMode
	}
	return ledgerSyncModeWorkingFS
}

// firstOpenABNoisyCount counts samples above firstOpenABNoiseMultiplier times
// their own arm's median — the "a neighbour was in this measurement" count. It
// is REPORTED, never asserted on: the verdict is the paired majority, and a
// noise count is context for the reader, not a second gate that could itself be
// flaky.
func firstOpenABNoisyCount(samples []float64, median float64) int {
	if median <= 0 {
		return 0
	}
	n := 0
	for _, s := range samples {
		if s > firstOpenABNoiseMultiplier*median {
			n++
		}
	}
	return n
}

// firstOpenABMs renders a sample set for the log line, rounded to three
// decimals so the distribution is readable without a wall of digits.
func firstOpenABMs(samples []float64) []string {
	out := make([]string, 0, len(samples))
	for _, s := range samples {
		out = append(out, strconv.FormatFloat(s, 'f', 3, 64))
	}
	return out
}

// firstOpenMeasureBuild runs the same first-open schema build with an explicit
// synchronous= mode and returns its wall time in MILLISECONDS.
//
// Milliseconds (not a time.Duration) because every consumer is the statistic or
// its log line, both of which speak in one unit — returning a Duration and
// converting at two call sites is how a 1000x conversion lands in a log line
// and nobody notices.
func firstOpenMeasureBuild(t *testing.T, path, syncMode string) float64 {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(path + suffix) //stylegate:ignore the arm is replacing its own fresh DB; a failed remove surfaces in the open below
	}
	ctx := context.Background()
	db, err := openDB(path, false)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	start := time.Now()
	if err := enableWALWithSync(ctx, db, syncMode); err != nil {
		t.Fatalf("enableWALWithSync(%s): %v", syncMode, err)
	}
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensureSchema: %v", err)
	}
	if err := ensureFeedbackSchema(ctx, db); err != nil {
		t.Fatalf("ensureFeedbackSchema: %v", err)
	}
	if err := ensureTasksSchema(ctx, db); err != nil {
		t.Fatalf("ensureTasksSchema: %v", err)
	}
	wall := time.Since(start)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return float64(wall.Microseconds()) / firstOpenABMicrosPerMilli
}

// firstOpenDeviceScratch returns a directory on a NON-memory filesystem, so the
// device arm of both tests measures a real device. The directory comes from
// testutil.TestDeviceScratchDir — the shared, GC'd scratch tree OUTSIDE every
// checkout.
//
// WHY IT NO LONGER CREATES ITS OWN DIRECTORY HERE (the leak, 2026-09-21): this
// helper used to be `os.MkdirTemp(".", "firstopen-device-")`, i.e. a directory
// inside the package source tree, on the reasoning that the checkout is the one
// block device a test may assume exists. Five such directories holding 341 test
// `.db` files accumulated untracked inside internal/tools, and a `git add` run
// while the test was mid-flight captured 106 of them into a commit (c2f5a9de).
// The repo-root junk gate reads `git ls-files` and checks repo-ROOT paths only,
// so a NESTED tree was outside its field of view. The lesson is in the helper's
// removal: a fixture that needs a path with a DEVICE property must be TOLD that
// path, never infer it from where its own source file happens to live.
//
// The device property is still MEASURED, not assumed: when the scratch home is
// memory-backed there is no device arm to run, and the fixture skips with that
// reason.
func firstOpenDeviceScratch(t *testing.T) string {
	t.Helper()
	return testutil.TestDeviceScratchDir(t, "firstopen")
}

// Ensure the sql import stays used if the query above is ever refactored away.
var _ = sql.ErrNoRows

// TestTasksSchemaBuildIsEquivalentAcrossPaths pins the SAFETY of the batched
// tasks-schema build: a fresh ledger built on a device (batched: one commit for
// the whole DDL set) must end up with EXACTLY the schema a fresh ledger on a
// memory filesystem gets (autocommit: the original statement sequence) — same
// objects, same SQL, same working FTS triggers. Without this, the batch could
// publish a different schema on exactly the path the ordinary suite never
// exercises.
func TestTasksSchemaBuildIsEquivalentAcrossPaths(t *testing.T) {
	autocommit := firstOpenTasksSchemaObjects(t, filepath.Join(t.TempDir(), "memory.db"))
	batched := firstOpenTasksSchemaObjects(t, filepath.Join(firstOpenDeviceScratch(t), "device.db"))

	if len(autocommit) == 0 || len(batched) == 0 {
		t.Fatalf("schema object lists are empty (autocommit %d, batched %d) — the comparison would be "+
			"vacuous", len(autocommit), len(batched))
	}
	for name, sql := range autocommit {
		if batched[name] != sql {
			t.Errorf("%s: the batched build produced %q, the autocommit build %q — the two paths must build "+
				"the same schema", name, batched[name], sql)
		}
	}
	for name := range batched {
		if _, ok := autocommit[name]; !ok {
			t.Errorf("%s exists only in the batched build — the two paths must build the same schema", name)
		}
	}
}

// firstOpenTasksSchemaObjects builds a fresh tasks schema at path and returns its
// sqlite_master objects (type+name → SQL), after proving the FTS keep-in-sync
// triggers are LIVE: a row inserted below must appear in tasks_fts.
func firstOpenTasksSchemaObjects(t *testing.T, path string) map[string]string {
	t.Helper()
	ctx := context.Background()
	db, err := openDB(path, false)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	if err := ensureTasksSchema(ctx, db); err != nil {
		t.Fatalf("ensureTasksSchema(%s): %v", path, err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, session_id, summary, status, tags, source, priority, created_at, updated_at)
		 VALUES ('t-equiv','s','equivalence probe','pending','','auto',5,1,1)`); err != nil {
		t.Fatalf("insert into %s: %v", path, err)
	}
	var indexed int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM tasks_fts WHERE tasks_fts MATCH 'equivalence'`).Scan(&indexed); err != nil {
		t.Fatalf("query tasks_fts on %s: %v", path, err)
	}
	if indexed != 1 {
		t.Fatalf("the FTS trigger did not index the inserted row on %s (matched %d) — a schema built without "+
			"live triggers is not equivalent", path, indexed)
	}
	rows, err := db.QueryContext(ctx, `SELECT type, name, COALESCE(sql,'') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		t.Fatalf("read sqlite_master on %s: %v", path, err)
	}
	defer rows.Close()
	objects := map[string]string{}
	for rows.Next() {
		var typ, name, sql string
		if err := rows.Scan(&typ, &name, &sql); err != nil {
			t.Fatalf("scan sqlite_master on %s: %v", path, err)
		}
		objects[typ+" "+name] = sql
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sqlite_master on %s: %v", path, err)
	}
	return objects
}
