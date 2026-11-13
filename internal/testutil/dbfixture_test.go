package testutil

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// TestNewTestDBPathIsFreshPinnedAndOffRoot is the WRITER shape's contract test:
// the path is fresh per call, it is inside the pinned scratch root and off the
// `/` filesystem, the ledger env contract points at it, and the legacy spelling
// is masked — the four guarantees the fixture's doc comment makes.
//
// Each arm is a MEASUREMENT (device ids, os.Stat, os.Getenv), never a spelling
// check: Rule 22 is a criterion and a path is only acceptable when its device
// id differs from `/`'s.
func TestNewTestDBPathIsFreshPinnedAndOffRoot(t *testing.T) {
	first := NewTestDBPath(t)
	// The environment is asserted BEFORE the second call: the second call pins
	// its own path (that is the point of the fixture), so reading it afterwards
	// would compare the first path against the second pin.
	envAfterFirst := os.Getenv(config.MemoryDBEnv)
	second := NewTestDBPath(t)

	if first == second {
		t.Fatalf("two calls returned the same path %q — the fixture is not fresh per call, so two tests "+
			"would share rows", first)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("the fixture returned %q, which already exists (stat err = %v) — NewTestDBPath promises a "+
			"FRESH, empty path; a file already there means the caller is about to read someone else's rows",
			first, err)
	}
	if got := filepath.Base(first); got != testDBFileName {
		t.Errorf("the fixture's file name is %q, want %q", got, testDBFileName)
	}
	if envAfterFirst != first {
		t.Errorf("%s = %q after the first call, want the fixture's own path %q — a tool call resolved through "+
			"the environment would otherwise open a different database",
			config.MemoryDBEnv, envAfterFirst, first)
	}
	if legacy := os.Getenv(config.MemoryDBEnvLegacy); legacy != "" {
		t.Errorf("%s = %q, want it masked — the legacy spelling is the same override under an older name, "+
			"so an ambient value smuggles another database back in through the fallback chain",
			config.MemoryDBEnvLegacy, legacy)
	}

	root := SuiteTempRoot()
	if !strings.HasPrefix(first, root+string(filepath.Separator)) {
		t.Errorf("the fixture wrote outside the pinned scratch root: %q is not under %q", first, root)
	}
	dev, err := ScratchDeviceID(filepath.Dir(first))
	if err != nil {
		t.Fatalf("measure the fixture's device id: %v", err)
	}
	rootDev, err := RootDeviceID()
	if err != nil {
		t.Fatalf("measure /'s device id: %v", err)
	}
	if dev == rootDev {
		t.Errorf("the fixture's directory is on the SAME filesystem as / (device 0x%x) — Rule 22 forbids a "+
			"test workspace there, and this fixture must not be the helper that reintroduces it", dev)
	}
}

// TestSampleSeedIsReadableThroughTheReadOnlyHandle is the READER shape's
// contract test: every documented seed row is present under the IDs the contract
// promises, and the handle the fixture hands out cannot write.
func TestSampleSeedIsReadableThroughTheReadOnlyHandle(t *testing.T) {
	db := Sample(t)

	for _, row := range SampleSeed() {
		var gotStatus, gotSummary string
		var gotPriority int
		err := db.QueryRow(
			`SELECT status, summary, priority FROM tasks WHERE task_id = ?`, row.TaskID).
			Scan(&gotStatus, &gotSummary, &gotPriority)
		if err != nil {
			t.Fatalf("the documented seed row %q is not readable: %v — the sample's seed and the contract "+
				"have drifted", row.TaskID, err)
		}
		if gotStatus != row.Status || gotSummary != row.Summary || gotPriority != row.Priority {
			t.Errorf("seed row %q reads back as (%q, %q, %d), want (%q, %q, %d)",
				row.TaskID, gotStatus, gotSummary, gotPriority, row.Status, row.Summary, row.Priority)
		}
	}

	var taskCount, eventCount int
	if err := db.QueryRow(`SELECT count(*) FROM tasks`).Scan(&taskCount); err != nil {
		t.Fatalf("count the sample's tasks: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM task_events`).Scan(&eventCount); err != nil {
		t.Fatalf("count the sample's events: %v", err)
	}
	if taskCount != len(SampleSeed()) {
		t.Errorf("the sample holds %d task row(s), want the %d documented by SampleSeed — a row that is not "+
			"documented is a row a test cannot rely on, and a missing one breaks every assertion on it",
			taskCount, len(SampleSeed()))
	}
	if eventCount != len(SampleSeedEvents()) {
		t.Errorf("the sample holds %d event row(s), want the %d documented by SampleSeedEvents",
			eventCount, len(SampleSeedEvents()))
	}
}

// TestSampleRefusesEveryWriteShape demonstrates the IMMUTABILITY contract with
// three independent attempts, and states which mechanism stops each one:
//
//  1. a write THROUGH THE HANDLE the fixture hands out — refused by the DSN's
//     mode=ro/query_only (the open mode is the primary mechanism);
//  2. a write through a handle opened with the READER's OWN path and a plain
//     DSN — refused by the file mode (0444), which is the second mechanism and
//     the reason SampleDBPath is safe to hand out;
//  3. the mutating statement that a "just seed one more row" caller would reach
//     for (an INSERT) and DDL — refused the same way.
//
// Running as root defeats Unix file modes, so arm 2 SKIPS there with the reason
// named rather than passing silently: a test that cannot observe the mechanism
// must say so (the same discipline as firstOpenDeviceScratch's memory-backed
// skip).
func TestSampleRefusesEveryWriteShape(t *testing.T) {
	db := Sample(t)
	if _, err := db.Exec(`INSERT INTO tasks (task_id, session_id, summary, status) VALUES ('x','s','x','x')`); err == nil {
		t.Error("the fixture's own handle ACCEPTED an INSERT — the reader shape is writable, which is exactly " +
			"the defect it exists to prevent")
	} else if !strings.Contains(strings.ToLower(err.Error()), "readonly") &&
		!strings.Contains(strings.ToLower(err.Error()), "read-only") {
		t.Errorf("the write through the fixture's handle failed with %v — it must fail because the database "+
			"is READ-ONLY, not for some unrelated reason", err)
	}
	if _, err := db.Exec(`CREATE TABLE rogue (id INTEGER)`); err == nil {
		t.Error("the fixture's handle ACCEPTED DDL — a reader must not be able to reshape the sample")
	}

	path := SampleDBPath(t)
	if path == "" {
		t.Fatal("SampleDBPath returned an empty path")
	}
	raw, err := sql.Open(sampleDriverName, path)
	if err != nil {
		t.Fatalf("open the sample with a plain DSN: %v", err)
	}
	defer func() {
		if cerr := raw.Close(); cerr != nil {
			t.Errorf("close the plain-DSN sample handle: %v", cerr)
		}
	}()
	if _, err := raw.Exec(`INSERT INTO tasks (task_id, session_id, summary, status) VALUES ('y','s','y','y')`); err == nil {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat the sample after an accepted write: %v", statErr)
		}
		if info.Mode().Perm()&0o200 != 0 {
			t.Errorf("a plain-DSN handle WROTE the sample %s and its mode is %v — the file is not sealed, "+
				"so SampleDBPath hands out a writable database", path, info.Mode().Perm())
		} else {
			t.Skipf("this process runs as a user that ignores the file mode (root): the sample is sealed " +
				"0444 but the mode does not stop it, so arm 2 cannot be observed here")
		}
	}
}

// TestSampleIsBuiltOnceAndTheSharedCostIsMeasured pins the "built once, reused"
// claim with a MEASUREMENT: the cost of building a sample (what every test would
// pay without the fixture) versus the cost of the cached lookup (what a test
// pays with it).
//
// The comparison is deliberately the fixture's own two code paths, not a
// stopwatch session: buildSampleOnce into a fresh directory is exactly the work
// a per-test sample would do, and sampleDBPath after the first call is exactly
// the work a sharing test does. The numbers are logged (they belong in the
// report); the assertion is only that the cached path is cheaper, which is the
// property the fixture exists for.
func TestSampleIsBuiltOnceAndTheSharedCostIsMeasured(t *testing.T) {
	// Warm: the shared sample exists for this run.
	SampleDBPath(t)

	dir := t.TempDir()
	fresh := filepath.Join(dir, sampleDBFileName)
	start := time.Now()
	if err := buildSampleOnce(dir, fresh); err != nil {
		t.Fatalf("build a fresh sample (the WITHOUT-sharing cost) at %s: %v", fresh, err)
	}
	buildCost := time.Since(start)

	sharedStart := time.Now()
	const sharedCalls = 50
	for i := 0; i < sharedCalls; i++ {
		if got := SampleDBPath(t); got == "" {
			t.Fatal("SampleDBPath returned an empty path")
		}
	}
	sharedPerCall := time.Since(sharedStart) / sharedCalls

	t.Logf("sample setup cost: build-once %v (what EVERY test pays without the fixture; %d tests would pay "+
		"%v) vs shared lookup %v per call (%dx cheaper)",
		buildCost, sharedCalls, buildCost*sharedCalls, sharedPerCall, ratioRounded(buildCost, sharedPerCall))
	if sharedPerCall >= buildCost {
		t.Errorf("the shared lookup costs %v, the build %v — the fixture is not actually SHARING the sample, "+
			"so its whole premise is false", sharedPerCall, buildCost)
	}

	// The sample must also be the SAME file across calls (a per-call rebuild
	// would still look fast when measured like this, so the identity is checked
	// separately).
	if first, second := sampleDBPath(t), sampleDBPath(t); first != second {
		t.Errorf("SampleDBPath returned %q then %q — the sample is not one shared file", first, second)
	}
}

// ratioRounded renders an integer ratio for the log line, clamped so a
// sub-microsecond divisor cannot print a nonsense figure.
func ratioRounded(cost, perCall time.Duration) int64 {
	if perCall <= 0 {
		return -1
	}
	return int64(cost / perCall)
}

// TestDeviceScratchDirIsOffRootAndLeavesNoInTreeDirectory is the leak's
// regression guard AND the real invocation of the migrated helper.
//
// The prior shape created its device directory INSIDE the package source tree
// (`os.MkdirTemp(".", "firstopen-device-")`), where a mid-flight `git add` can
// capture it — measured: five such directories holding 341 untracked test .db
// files in internal/tools, 106 of them once committed. This test asserts the
// property that made that possible is gone: after the helper returns, NO entry
// with the retired prefixes exists in the package directory, and the directory
// it did return is inside the shared scratch home and off the `/` filesystem
// (Rule 22 — measured, never spelled).
//
// It is deliberately cheap (no fsyncs, no heavy fixture): the expensive arm of
// the device test stays behind GOPI_HEAVY_TESTS, while the MECHANISM this ticket
// changed is checked on every run.
func TestDeviceScratchDirIsOffRootAndLeavesNoInTreeDirectory(t *testing.T) {
	retired := []string{"firstopen-device-", "ledgerpin-device-"}
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve the package directory: %v", err)
	}
	before := dbFixtureTreeEntries(t, pkgDir, retired)

	dir := TestDeviceScratchDir(t, "leakguard")

	after := dbFixtureTreeEntries(t, pkgDir, retired)
	if after != before {
		t.Fatalf("the retired in-tree scratch prefixes appeared in %s while the fixture ran (%d -> %d "+
			"entries) — the migration reopened the leak the repo-root junk gate cannot see", pkgDir, before, after)
	}
	home := ScratchRootQueue()
	if !strings.HasPrefix(dir, home+string(filepath.Separator)) {
		t.Errorf("the device scratch directory %q is not under the shared scratch home %q", dir, home)
	}
	dev, err := ScratchDeviceID(dir)
	if err != nil {
		t.Fatalf("measure the device scratch directory's device id: %v", err)
	}
	rootDev, err := RootDeviceID()
	if err != nil {
		t.Fatalf("measure /'s device id: %v", err)
	}
	if dev == rootDev {
		t.Errorf("the device scratch directory is on the SAME filesystem as / (device 0x%x) — Rule 22 forbids "+
			"a test workspace there", dev)
	}
}

// dbFixtureTreeEntries counts entries in dir whose names start with one of the
// retired scratch prefixes.
func dbFixtureTreeEntries(t *testing.T, dir string, prefixes []string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		for _, p := range prefixes {
			if strings.HasPrefix(e.Name(), p) {
				n++
			}
		}
	}
	return n
}
