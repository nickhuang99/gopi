package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// tagOfField returns the json tag name of the named field on v, or "" when the
// field does not exist at all. Reflection is deliberate here: a settings WIRE
// name lives in a struct tag, so no identifier reference (and therefore no
// compiler check, Rule 13's usual guard) can tie it to the field — this helper
// is what lets a test assert the tag is gone.
func tagOfField(v any, name string) string {
	t := reflect.TypeOf(v)
	if t == nil {
		return ""
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	f, ok := t.FieldByName(name)
	if !ok {
		return ""
	}
	return strings.Split(f.Tag.Get("json"), ",")[0]
}

// peakGuardRemovedSettingsKeys are the settings.json keys the PEAK-HOUR COST
// GUARD used to declare — `peak_hours` (the window list) and `ignore_peak_hours`
// (its escape hatch). The guard was removed outright
// (devdoc/peak-guard-removal.md); these two strings survive ONLY as the subject
// of the backward-tolerance test below, so an operator's existing file keeps
// loading.
const (
	peakGuardRemovedWindowsKey = "peak_hours"
	peakGuardRemovedIgnoreKey  = "ignore_peak_hours"
)

// peakGuardRemovalDoc is the devdoc recording the removal — cited by the tests
// below so a reader can recover the decision without a session log.
const peakGuardRemovalDoc = "devdoc/peak-guard-removal.md"

// TestRemovedPeakHoursSettingsKeysAreTolerated proves the BACKWARD TOLERANCE the
// removal must keep: a settings.json written while the peak-hour cost guard
// existed still carries `peak_hours` / `ignore_peak_hours`, and that file must
// keep loading with every OTHER knob intact.
//
// It is an assertion about the DECODER, not about the guard: settingsFromFile
// decodes with a plain json.Unmarshal (no DisallowUnknownFields), so a key with
// no field is silently ignored. That property is what makes the removal safe
// for existing deployments — and it is exactly the property a future
// "strict settings" change would break, so it is pinned here rather than
// assumed (Rule 15: the failure mode is a settings file that suddenly refuses
// to load, on a host whose only sin was being configured before the removal).
//
// Both keys are asserted in ONE file alongside a still-live knob (timeout), so
// the test measures tolerance rather than "the file parsed to zero values".
func TestRemovedPeakHoursSettingsKeysAreTolerated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	body := `{
  "peak_hours": "09:00-12:00,14:00-18:00",
  "ignore_peak_hours": true,
  "timeout": 42
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("a settings.json carrying the REMOVED guard keys must keep loading (%s): %v — "+
			"an existing deployment would be unable to start after the removal", peakGuardRemovalDoc, err)
	}
	if cfg == nil {
		t.Fatalf("FromSettingsFile(%s) returned a nil Config with no error", path)
	}
	// The tolerated keys must not have been RESURRECTED as fields either: the
	// point is that they now mean nothing, and the live knob beside them still
	// resolves (so the decode really ran).
	if cfg.TimeoutSec != 42 {
		t.Errorf("TimeoutSec = %d, want 42 — the keys the guard used to declare must be "+
			"ignored WITHOUT disturbing the knobs beside them (%s)", cfg.TimeoutSec, peakGuardRemovalDoc)
	}
}

// TestRemovedPeakHoursSettingsKeysHaveNoField is the structural half of the
// same guarantee: the two removed keys must have NO field on Config, so nothing
// in the tree can resolve a guard setting from a settings file. The string-pair
// map is what encodes "these wire names are gone" without the compiler being
// able to help (the wire name lives in a struct tag, which a rename cannot
// follow).
//
// A field re-added under either json tag fails here, which is the intended
// tripwire: reintroducing the guard must be a deliberate act, never a merge
// accident that revives configuration with no reader (PROJECT.md Rule 21's shape —
// a declared knob whose consumer is gone).
func TestRemovedPeakHoursSettingsKeysHaveNoField(t *testing.T) {
	cfg := &Config{}
	probes := map[string]string{
		peakGuardRemovedWindowsKey: "PeakHours",
		peakGuardRemovedIgnoreKey:  "IgnorePeakHours",
	}
	for wire, field := range probes {
		if tagOfField(cfg, field) == wire {
			t.Errorf("Config.%s carries json tag %q again — the peak-hour cost guard was "+
				"removed (%s); a re-added field would resurrect a settings knob with no reader",
				field, wire, peakGuardRemovalDoc)
		}
	}
}
