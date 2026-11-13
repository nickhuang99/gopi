package config

// cov_config_effort_test.go — function-level coverage slice for the EXPORTED
// entry point config.ResolveReasoningEffort (branch feat/cov-config-hp).
// Tests only. The unexported engine resolveReasoningEffort already has a
// table in settings_effort_test.go; this file pins the exported wrapper's
// own contract — same answers, reached through the public API — plus the
// default/empty paths and the case-insensitivity edge the wrapper inherits.

import (
	"reflect"
	"strings"
	"testing"
)

// covCfgEffortCases is the wrapper's value matrix: requested effort × the
// settings-declared whitelist → (returned wire value, ok).
var covCfgEffortCases = []struct {
	name     string
	effort   string
	values   []string
	want     string
	wantInfo bool
}{
	// --- default path: no declared list → the DeepSeek baseline ---
	{"nil-list-baseline-max", "max", nil, "max", true},
	{"nil-list-baseline-minimal", "minimal", nil, "minimal", true},
	{"nil-list-unlisted-falls-back", "ultra", nil, defaultReasoningEffort, false},
	// An EMPTY-but-non-nil list behaves exactly like a nil one (the engine
	// branches on len(list)==0): the baseline applies, so a baseline member
	// resolves ok and an unlisted value falls back.
	{"empty-list-baseline", "low", []string{}, "low", true},
	{"empty-list-baseline-again", "medium", []string{}, "medium", true},
	{"empty-list-unlisted-falls-back", "ultra", []string{}, defaultReasoningEffort, false},

	// --- valid: declared lists, verbatim pass-through (casing kept) ---
	{"declared-exact", "high", []string{"minimal", "low", "medium", "high", "max"}, "high", true},
	{"declared-verbatim-casing-MAX", "MAX", []string{"max", "high", "low"}, "MAX", true},
	{"declared-case-insensitive-match", "High", []string{"max", "high", "low"}, "High", true},
	{"declared-request-trimmed", "  high  ", []string{"max", "high", "low"}, "  high  ", true},
	{"declared-list-entry-padded", "high", []string{" max ", " high "}, "high", true},
	{"custom-vendor-value", "xhigh", []string{"xhigh", "high"}, "xhigh", true},

	// --- invalid: not in the list → defaultReasoningEffort + ok=false ---
	{"declared-missing-value", "medium", []string{"max", "high", "low"}, defaultReasoningEffort, false},
	{"declared-empty-request", "", []string{"max", "high", "low"}, defaultReasoningEffort, false},
	{"declared-garbage", "banana", []string{"max", "high"}, defaultReasoningEffort, false},
	{"declared-substring-not-a-member", "hi", []string{"max", "high"}, defaultReasoningEffort, false},

	// --- empty / whitespace request on the default list ---
	{"nil-list-empty-request", "", nil, defaultReasoningEffort, false},
	{"nil-list-whitespace-request", "   ", nil, defaultReasoningEffort, false},

	// --- "off": the harness-only value, always valid, never a wire effort ---
	{"off-with-declared-list", EffortOff, []string{"max", "high"}, EffortOff, true},
	{"off-with-nil-list", EffortOff, nil, EffortOff, true},
	{"off-uppercase", "OFF", []string{"max", "high"}, EffortOff, true},
	{"off-padded", "  off ", []string{"max", "high"}, EffortOff, true},
	{"off-with-empty-list", "off", []string{}, EffortOff, true},
}

// TestCovCfgResolveReasoningEffort_Matrix drives the EXPORTED wrapper over
// the full matrix above.
func TestCovCfgResolveReasoningEffort_Matrix(t *testing.T) {
	for _, tc := range covCfgEffortCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveReasoningEffort(tc.effort, tc.values)
			if got != tc.want || ok != tc.wantInfo {
				t.Errorf("ResolveReasoningEffort(%q, %v) = (%q, %v), want (%q, %v)",
					tc.effort, tc.values, got, ok, tc.want, tc.wantInfo)
			}
		})
	}
}

// TestCovCfgResolveReasoningEffort_ReturnedValueIsOnTheWireWhitelist is the
// contract a caller actually relies on: whenever ok is true and the request
// is not the harness-only "off", the RETURNED value is a member of the
// effective whitelist (the declared list when present, the baseline
// otherwise) — so nothing unlisted can ever reach the provider.
func TestCovCfgResolveReasoningEffort_ReturnedValueIsOnTheWireWhitelist(t *testing.T) {
	for _, tc := range covCfgEffortCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveReasoningEffort(tc.effort, tc.values)
			if !ok {
				if got != defaultReasoningEffort {
					t.Errorf("not-ok resolution returned %q, want %q", got, defaultReasoningEffort)
				}
				return
			}
			if strings.EqualFold(strings.TrimSpace(got), EffortOff) {
				return // harness-only switch — deliberately never wired
			}
			list := tc.values
			if len(list) == 0 {
				list = defaultEffortValues
			}
			found := false
			for _, v := range list {
				if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(got)) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("ok resolution returned %q which is NOT in the whitelist %v", got, list)
			}
		})
	}
}

// TestCovCfgResolveReasoningEffort_DefaultsAreStable pins the two fallback
// constants the wrapper leans on (Rule 1b/13: reference them, never re-type
// the literals): the empty-resolution default and the no-settings baseline
// list. A change here silently moves every user's effort.
func TestCovCfgResolveReasoningEffort_DefaultsAreStable(t *testing.T) {
	if defaultReasoningEffort != "minimal" {
		t.Errorf("defaultReasoningEffort = %q, want minimal", defaultReasoningEffort)
	}
	if EffortOff != "off" {
		t.Errorf("EffortOff = %q, want off", EffortOff)
	}
	if want := []string{"minimal", "low", "medium", "high", "max"}; !reflect.DeepEqual(defaultEffortValues, want) {
		t.Errorf("defaultEffortValues = %v, want %v", defaultEffortValues, want)
	}
	// Every baseline member — and nothing else — resolves ok through the
	// exported wrapper, with the request echoed verbatim.
	for _, v := range defaultEffortValues {
		got, ok := ResolveReasoningEffort(v, nil)
		if !ok || got != v {
			t.Errorf("ResolveReasoningEffort(%q, nil) = (%q, %v), want (%q, true)", v, got, ok, v)
		}
	}
	if got, ok := ResolveReasoningEffort("off", nil); !ok || got != EffortOff {
		t.Errorf("ResolveReasoningEffort(off, nil) = (%q, %v), want (off, true)", got, ok)
	}
	if _, ok := ResolveReasoningEffort("offline", nil); ok {
		t.Error(`"offline" resolved ok — only the exact "off" token is the harness switch`)
	}
}

// TestCovCfgResolveReasoningEffort_FromSettingsFile is the end-to-end path a
// real run takes: settings.json declares reasoning_effort + effort_values,
// and the EXPORTED wrapper resolves the parsed config fields. A value the
// settings themselves declare as legal passes; a foreign one (legal only for
// another vendor) falls back.
func TestCovCfgResolveReasoningEffort_FromSettingsFile(t *testing.T) {
	p := writeSettings(t, `{
		"reasoning_effort":"max",
		"effort_values":["max","high","low"]
	}`)
	cfg, err := FromSettingsFile(p)
	if err != nil {
		t.Fatalf("FromSettingsFile: %v", err)
	}
	if got, ok := ResolveReasoningEffort(cfg.ReasoningEffort, cfg.EffortValues); !ok || got != "max" {
		t.Errorf("settings max vs [max high low] = (%q, %v), want (max, true)", got, ok)
	}
	// The same declared list rejects a baseline value the vendor does not
	// accept — the list IS the schema.
	cfg.ReasoningEffort = "medium"
	got, ok := ResolveReasoningEffort(cfg.ReasoningEffort, cfg.EffortValues)
	if ok || got != defaultReasoningEffort {
		t.Errorf("settings medium vs [max high low] = (%q, %v), want (%q, false)", got, ok, defaultReasoningEffort)
	}
	// And a settings file with NO effort_values falls back to the baseline.
	p2 := writeSettings(t, `{"reasoning_effort":"high"}`)
	cfg2, err := FromSettingsFile(p2)
	if err != nil {
		t.Fatalf("FromSettingsFile: %v", err)
	}
	if cfg2.EffortValues != nil {
		t.Fatalf("EffortValues = %v, want nil (baseline fallback)", cfg2.EffortValues)
	}
	if got, ok := ResolveReasoningEffort(cfg2.ReasoningEffort, cfg2.EffortValues); !ok || got != "high" {
		t.Errorf("settings high vs baseline = (%q, %v), want (high, true)", got, ok)
	}
}
