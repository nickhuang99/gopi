package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// memorySyncTickRemovalDoc is the devdoc recording the retirement — cited by the
// tests below so a reader can recover the decision without a session log.
const memorySyncTickRemovalDoc = "devdoc/memory-sync-tick-retired.md"

// The three tests in this file are the MEMORY-SYNC tick's half of the pair
// merge_tick_removed_test.go already keeps, written in the same shape on purpose:
// one tolerance arm (an old settings file must keep loading), one structural
// tripwire (no field may carry the retired json tag again) and one Rule 21 arm
// (the retired env spelling exists to be NAMED, never read). A second pattern was
// not invented — the mechanism retired here is the same KIND of thing, and a
// future reader should find one shape, not two.

// TestRetiredMemorySyncTickSettingsKeyIsTolerated is the BACKWARD-TOLERANCE half: a
// settings.json written while the resident memory-sync tick existed still carries
// "memory_sync_tick", and such a file must keep loading with every OTHER knob
// intact.
//
// The key is named from its own constant rather than typed here (Rule 1b): the
// retirement report in cmd/agent must name the SAME spelling, and a test that
// spelled its own copy would keep passing after a rename that broke the report.
func TestRetiredMemorySyncTickSettingsKeyIsTolerated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	body := `{"` + MemorySyncTickRetiredSettingsKey + `":30,"timeout":42}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("a settings.json carrying the RETIRED memory-sync tick key must keep loading (%s): %v — "+
			"an existing deployment would be unable to start after the retirement", memorySyncTickRemovalDoc, err)
	}
	if cfg == nil {
		t.Fatalf("FromSettingsFile(%s) returned a nil Config with no error", path)
	}
	if cfg.TimeoutSec != 42 {
		t.Errorf("TimeoutSec = %d, want 42 — the retired key must be ignored WITHOUT disturbing the knobs "+
			"beside it (%s)", cfg.TimeoutSec, memorySyncTickRemovalDoc)
	}
	// The tolerance is what the config package owes. The SILENCE it leaves behind is
	// closed on the worker start path by the retirement report
	// (cmd/agent/memory_sync_tick_retired.go), which is the only place that can read
	// the FILE rather than the decoded struct — this is the probe it uses, pinned
	// here so the two halves cannot drift apart.
	if !cfg.SettingsCarriesKey(MemorySyncTickRetiredSettingsKey) {
		t.Errorf("SettingsCarriesKey(%q) = false for a file that carries it — the operator would be told "+
			"nothing at all (%s)", MemorySyncTickRetiredSettingsKey, memorySyncTickRemovalDoc)
	}
}

// TestRetiredMemorySyncTickSettingsKeyHasNoField is the structural tripwire: no
// field on Config may carry the retired json tag again.
//
// The field NAME is gone, so the check cannot name a field — it scans EVERY field's
// tag, which is the only shape that catches a re-added knob under the same wire name
// (a struct tag is a string the compiler cannot follow, Rule 13's known blind spot).
// Reintroducing the cadence must be a deliberate act, never a merge accident that
// revives configuration with no reader.
func TestRetiredMemorySyncTickSettingsKeyHasNoField(t *testing.T) {
	cfgType := reflect.TypeOf(Config{})
	for i := 0; i < cfgType.NumField(); i++ {
		tag := strings.Split(cfgType.Field(i).Tag.Get("json"), ",")[0]
		if tag == MemorySyncTickRetiredSettingsKey {
			t.Errorf("Config.%s carries json tag %q again — the resident memory-sync tick and its cadence "+
				"were retired (%s); a re-added field would resurrect a settings knob whose consumer is gone",
				cfgType.Field(i).Name, tag, memorySyncTickRemovalDoc)
		}
	}
}

// TestRetiredMemorySyncTickEnvNameHasNoReader is Rule 21's requirement stated where
// the constant lives: the retired env spelling must exist to be NAMED, never to be
// read.
//
// The temptation here is specific and easy to miss — the next reader may "restore"
// the env lookup so some diagnostic can print the value, and that single line is
// what turns a retired knob back into a live one that nothing honours.
func TestRetiredMemorySyncTickEnvNameHasNoReader(t *testing.T) {
	if MemorySyncTickRetiredEnvName != "GOPI_MEMORY_SYNC_TICK_MIN" {
		t.Fatalf("the retired env spelling changed to %q — the report would name a variable nobody ever set",
			MemorySyncTickRetiredEnvName)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the config package directory: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		raw, rerr := os.ReadFile(e.Name())
		if rerr != nil {
			t.Fatalf("read %s: %v", e.Name(), rerr)
		}
		for _, reader := range []string{
			"os.Getenv(" + MemorySyncTickRetiredEnvName + ")",
			"t.Setenv(" + MemorySyncTickRetiredEnvName,
		} {
			if strings.Contains(string(raw), reader) {
				t.Errorf("%s reads the RETIRED knob (%s) — it exists to be NAMED by the retirement report, "+
					"never consumed; a reader would make it a live knob nothing honours (%s)",
					e.Name(), reader, memorySyncTickRemovalDoc)
			}
		}
	}
}
