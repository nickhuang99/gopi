// The RETIRED RESIDENT MEMORY-SYNC TICK's guard (2026-10-25).
//
// The tick's own tests (memory_sync_tick_test.go, memory_sync_tick_cwd_test.go)
// were deleted WITH it — they existed to drive a cadence that no longer exists, and
// a test that outlives its mechanism is a green that measures nothing. What must
// stay asserted is the two facts the retirement CREATES, and each has its own arm
// below, in the shape merge_tick_retired_test.go established beside this file:
//
//  1. THE WORKER LOOP NO LONGER SYNCS. The deleted tick's tests asserted the
//     OPPOSITE (that the loop fired it, throttled, once per peer), so the property
//     is INVERTED here rather than dropped: a future edit that puts the driver back
//     into the round must fail a test that names the ruling, not merely differ from
//     a deleted one. The subject is the loop's own SOURCE, because "this file
//     contains no call site" has no other observable shape — and a source read
//     cannot be fooled by a test seam.
//  2. THE RETIRED KNOB SPEAKS. Setting either spelling produces an explicit
//     retirement message naming what to do instead (Rule 15), and the settings face
//     is reported even though the decoder ignores an undeclared key silently.
//
// The RED PROOF is the tree this commit came from: before it, cli_tasks_worker.go
// carried `memorySyncTickSweep(workerRoleLabel)` and memory_sync_tick.go existed,
// so arm 1 failed on the first spelling and arm 3 failed on the file stat.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// memorySyncInvocationNames are the spellings the retired driver could reach the
// worker's round through: its own entry point, the send seam behind it, the process
// clock, and the env knob the call site's comment named.
//
// Every one of these is a code spelling, never a bare word: `memory-sync-tick`
// appears in this file's prose and in the devdoc that records the ruling, and a
// bare-word scan would fail on the documentation that keeps the decision instead of
// on the code the decision removed.
var memorySyncInvocationNames = []string{
	"memorySyncTickSweep",
	"memorySyncTickSend",
	"memorySyncTickLastRun",
	"GOPI_MEMORY_SYNC_TICK_MIN",
}

// TestWorkerLoopDoesNotSync is the ruling, as an assertion: the worker's round
// contains no memory-sync machinery.
func TestWorkerLoopDoesNotSync(t *testing.T) {
	raw, err := os.ReadFile(workerLoopSourcePath)
	if err != nil {
		t.Fatalf("read %s: %v", workerLoopSourcePath, err)
	}
	src := string(raw)
	for _, spelling := range memorySyncInvocationNames {
		if strings.Contains(src, spelling) {
			t.Errorf("%s still mentions %q: the worker must not drive a memory-asset sync — a fired run "+
				"blocked a round for 315-534s (measured, ~/.gopi/logs/worker.log), so `gopi memory sync "+
				"--peer P --push` is a command the operator runs by hand (2026-10-25; "+
				"devdoc/memory-sync-tick-retired.md)", workerLoopSourcePath, spelling)
		}
	}
}

// TestMemorySyncTickPlumbingIsGone asserts the whole cadence chain is retired, not
// merely unplugged: the tick file, both of its test files, its publisher and its
// env name. A knob left half-removed is the failure this checks for — a publisher
// called by nothing, or a cadence read by nothing, is exactly the dead knob the
// retirement is supposed to leave behind.
//
// The RETIRED spellings survive (config.MemorySyncTickRetiredEnvName /
// MemorySyncTickRetiredSettingsKey) and are therefore NOT in the list below: their
// whole purpose is to be nameable by the report asserted in arm 2.
func TestMemorySyncTickPlumbingIsGone(t *testing.T) {
	for _, gone := range []string{"memory_sync_tick.go", "memory_sync_tick_test.go", "memory_sync_tick_cwd_test.go"} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s still exists: the memory-sync tick and its plumbing were retired whole (2026-10-25)", gone)
		}
	}
	// The PUBLISHER must not be called anywhere, and must not exist: calling it
	// would re-create the environment variable the deleted reader consumed.
	for _, src := range []string{workerLoopSourcePath, "tmpdir_inject.go"} {
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		if strings.Contains(string(raw), "PublishMemorySyncTickMin(") {
			t.Errorf("%s still calls config.PublishMemorySyncTickMin — the publisher was retired with the tick it fed", src)
		}
	}
}

// TestRetiredMemorySyncTickKnobsSpeak is the Rule 15 half: an operator who still
// sets either spelling is TOLD it is retired, in a sentence that names the knob, why
// it went, and what to use instead.
//
// WHERE THE SENTENCE LIVES NOW (2026-11-06): the per-knob report FUNCTION was
// replaced by the ONE registry `gopi config show` reads
// (internal/config/retired_settings_keys.go), so this arm reads the row rather
// than a local verdict builder. The obligations it pins are unchanged — both
// faces named, the replacement named, and the knob's own purpose stated — because
// a registry row that lost one of them reports less than the hook it replaced.
func TestRetiredMemorySyncTickKnobsSpeak(t *testing.T) {
	row, ok := config.RetiredSettingsKeyInfo(config.MemorySyncTickRetiredSettingsKey)
	if !ok {
		t.Fatalf("the retired-knob registry carries no row for %q — the settings face of this retirement would be silent in `gopi config show`", config.MemorySyncTickRetiredSettingsKey)
	}
	for _, must := range []string{
		config.MemorySyncTickRetiredEnvName,
		config.MemorySyncTickRetiredSettingsKey,
	} {
		named := row.EnvName == must || row.SettingsKey == must
		if !named {
			t.Errorf("the registry row for this retirement does not carry %q as one of its faces: %+v", must, row)
		}
	}
	// Case-INSENSITIVE on the purpose phrase only: the row's own vocabulary is
	// prose ("the resident MEMORY-SYNC TICK"), and pinning its capitalisation
	// would make a rewrite of the sentence look like a lost obligation.
	body := strings.ToLower(row.Purpose + row.Retired + row.Replacement)
	for _, must := range []string{
		strings.ToLower(config.LedgerCopyForwardReplacement),
		"memory-sync tick",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("the registry row does not name %q — an explanation that omits half of what the startup hook said is a regression:\n%+v", must, row)
		}
	}
	if row.Replacement != config.LedgerCopyForwardReplacement {
		t.Errorf("the registry row's replacement is %q, want the ONE shared constant config.LedgerCopyForwardReplacement — a re-typed sentence is how two surfaces drift", row.Replacement)
	}
	// And the report is silent when NEITHER face is set: the settings probe is the
	// half that makes a settings face possible at all (the decoded Config has no
	// field for the retired key any more, so only the FILE can answer).
	t.Setenv(config.MemorySyncTickRetiredEnvName, "")
	cfg := &config.Config{SettingsPath: filepath.Join(t.TempDir(), "settings.json")}
	if err := os.WriteFile(cfg.SettingsPath, []byte(`{"model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg.SettingsCarriesKey(config.MemorySyncTickRetiredSettingsKey) {
		t.Error("SettingsCarriesKey found a retired key in a file that does not carry it — the probe would report every file")
	}
}

// TestSettingsCarriesKeySeesTheRetiredMemorySyncTickKey is the settings probe's own
// contract, and it is the half that makes the settings face possible at all: the
// decoded Config has no field for the retired key any more, so "the operator still
// sets it" can only be answered from the FILE.
func TestSettingsCarriesKeySeesTheRetiredMemorySyncTickKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"memory_sync_tick":30,"model":"kept"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{SettingsPath: path}
	if !cfg.SettingsCarriesKey(config.MemorySyncTickRetiredSettingsKey) {
		t.Fatalf("the probe did not see %q in a file that carries it — the settings face of the retirement would be silent", config.MemorySyncTickRetiredSettingsKey)
	}
	// The file still LOADS with every other knob intact: the key is tolerated, not
	// rejected, which is what makes "a stale cadence in an old file" harmless.
	loaded, err := config.FromSettingsFile(path)
	if err != nil {
		t.Fatalf("a settings file carrying the retired key must still load: %v", err)
	}
	if loaded == nil || loaded.Model != "kept" {
		t.Fatalf("a settings file carrying the retired key lost its OTHER knobs: %+v", loaded)
	}
}
