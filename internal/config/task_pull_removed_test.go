package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// taskPullRemovalDoc is the evidence record for this retirement — cited by the
// tests below so a reader can recover the decision without a session log.
//
// (It is the bug row's own evidence file rather than a devdoc page: this
// retirement is a CORRECTION to batch 5's deletion, and the batch's own commit
// message plus this file are the record. A devdoc would restate them.)
const taskPullRemovalDoc = "row 01a1ad9d / batch 5 commit d7d836e5"

// The three tests here are the WORKER-PULL switch's half of the pair
// merge_tick_removed_test.go and memory_sync_tick_removed_test.go already keep,
// written in the same shape on purpose: one tolerance arm (an old settings file
// must keep loading), one structural tripwire (no field may carry the retired
// json tag again) and one spelling arm (the retired env name exists to be NAMED,
// never read). A second pattern was not invented.

// TestRetiredTaskPullSettingsKeyIsTolerated is the BACKWARD-TOLERANCE half: a
// settings.json written while the worker-pull switch existed still carries
// "task_pull", and such a file must keep loading with every OTHER knob intact.
func TestRetiredTaskPullSettingsKeyIsTolerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	body := `{"` + TaskPullRetiredSettingsKey + `":true,"timeout":42}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("a settings.json carrying the RETIRED %q key must keep loading (%s): %v — an existing "+
			"deployment would be unable to start after the retirement", TaskPullRetiredSettingsKey, taskPullRemovalDoc, err)
	}
	if cfg == nil {
		t.Fatalf("FromSettingsFile(%s) returned a nil Config with no error", path)
	}
	if cfg.TimeoutSec != 42 {
		t.Errorf("TimeoutSec = %d, want 42 — the retired key must be ignored WITHOUT disturbing the knobs "+
			"beside it (%s)", cfg.TimeoutSec, taskPullRemovalDoc)
	}
	// The tolerance is what this package owes. The SILENCE it leaves behind is
	// closed on the worker start path by the retirement report
	// (cmd/agent/task_pull_retired.go), whose settings face uses exactly this
	// probe — pinned here so the two halves cannot drift apart.
	if !cfg.SettingsCarriesKey(TaskPullRetiredSettingsKey) {
		t.Errorf("SettingsCarriesKey(%q) = false for a file that carries it — the operator would be told "+
			"nothing at all (%s)", TaskPullRetiredSettingsKey, taskPullRemovalDoc)
	}
}

// TestRetiredTaskPullSettingsKeyHasNoField is the structural tripwire: no field on
// Config may carry the retired json tag again. Re-adding it would resurrect a
// switch whose consumer is gone — and, worse, would make the retirement report
// claim a LIVE knob is dead.
func TestRetiredTaskPullSettingsKeyHasNoField(t *testing.T) {
	cfgType := reflect.TypeOf(Config{})
	for i := 0; i < cfgType.NumField(); i++ {
		tag := strings.Split(cfgType.Field(i).Tag.Get("json"), ",")[0]
		if tag == TaskPullRetiredSettingsKey {
			t.Errorf("Config.%s carries json tag %q again — the worker-pull switch was retired (%s); a "+
				"re-added field would resurrect a settings knob whose only consumer was deleted",
				cfgType.Field(i).Name, tag, taskPullRemovalDoc)
		}
	}
}

// TestRetiredTaskPullAndMemorySyncEveryEnvNamesAreNamedNotRead is Rule 21's
// requirement stated where the constants live. Both spellings must exist to be
// NAMED by the retirement report and must never be read here.
//
// The temptation is specific: a future diagnostic "just prints the value it saw",
// and that one line turns a retired knob back into a live one that nothing
// honours.
func TestRetiredTaskPullAndMemorySyncEveryEnvNamesAreNamedNotRead(t *testing.T) {
	if TaskPullRetiredEnvName != "GOPI_TASK_PULL" {
		t.Fatalf("the retired env spelling changed to %q — the report would name a variable nobody ever set",
			TaskPullRetiredEnvName)
	}
	if MemorySyncEveryRetiredEnvName != "GOPI_MEMORY_SYNC_EVERY_SEC" {
		t.Fatalf("the retired env spelling changed to %q — the report would name a variable nobody ever set",
			MemorySyncEveryRetiredEnvName)
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
		for _, name := range []string{TaskPullRetiredEnvName, MemorySyncEveryRetiredEnvName} {
			for _, reader := range []string{"os.Getenv(" + name + ")", "t.Setenv(" + name + ","} {
				if strings.Contains(string(raw), reader) {
					t.Errorf("%s reads the RETIRED knob (%s) — it exists to be NAMED by the retirement report, "+
						"never consumed; a reader would make it a live knob nothing honours (%s)",
						e.Name(), reader, taskPullRemovalDoc)
				}
			}
		}
	}
}

// TestRetiredMemorySyncEveryHasNoSettingsKey pins the one asymmetry the briefing
// required: the periodic memory-sync cadence was ENV-ONLY, so no settings key is
// invented for it at retirement time.
func TestRetiredMemorySyncEveryHasNoSettingsKey(t *testing.T) {
	raw, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}
	if strings.Contains(string(raw), "MemorySyncEveryRetiredSettingsKey") {
		t.Errorf("config.go defines a settings-key constant for the periodic memory-sync cadence — that knob "+
			"was ENV-ONLY for its whole life; inventing a key names a spelling no version of this program "+
			"ever wrote (%s)", taskPullRemovalDoc)
	}
	cfgType := reflect.TypeOf(Config{})
	for i := 0; i < cfgType.NumField(); i++ {
		tag := strings.Split(cfgType.Field(i).Tag.Get("json"), ",")[0]
		if tag == "memory_sync_every" {
			t.Errorf("Config.%s carries json tag %q — the cadence was never a settings key", cfgType.Field(i).Name, tag)
		}
	}
}
