package config

import (
	"os"
	"reflect"
	"testing"
)

// TestResolveReasoningEffort covers the settings-driven effort whitelist:
// the list IS the provider's schema (data, not code) — nil falls back to the
// DeepSeek baseline, listed values pass through verbatim (user casing kept),
// and anything else falls back to minimal with ok=false. "off" is always a
// valid harness-only resolution (thinking disabled, never wired).
func TestResolveReasoningEffort(t *testing.T) {
	cases := []struct {
		name     string
		effort   string
		values   []string
		want     string
		wantInfo bool
	}{
		{"nil-list-deepseek-baseline-high", "high", nil, "high", true},
		{"nil-list-medium", "medium", nil, "medium", true},
		{"nil-list-invalid-fallback", "ultra", nil, "minimal", false},
		{"glm-list-max-passthrough", "max", []string{"max", "high", "low"}, "max", true},
		{"glm-list-high-passthrough", "high", []string{"max", "high", "low"}, "high", true},
		{"glm-list-medium-not-listed", "medium", []string{"max", "high", "low"}, "minimal", false},
		{"casing-verbatim-MAX", "MAX", []string{"max", "high", "low"}, "MAX", true},
		{"case-insensitive-match", "High", []string{"max", "high", "low"}, "High", true},
		{"trimmed-entry-match", " high ", []string{"max", "high", "low"}, " high ", true},
		{"off-always-valid", "off", []string{"max", "high", "low"}, "off", true},
		{"off-valid-even-nil-list", "off", nil, "off", true},
		{"empty-list-falls-to-baseline", "minimal", []string{}, "minimal", true},
		{"empty-effort-not-listed", "", []string{"max", "high", "low"}, "minimal", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveReasoningEffort(tc.effort, tc.values)
			if got != tc.want || ok != tc.wantInfo {
				t.Errorf("resolveReasoningEffort(%q, %v) = (%q, %v), want (%q, %v)",
					tc.effort, tc.values, got, ok, tc.want, tc.wantInfo)
			}
		})
	}
}

// TestDefaultEffortValues pins the DeepSeek baseline list — the no-settings
// fallback that keeps today's behaviour byte-identical for existing users.
func TestDefaultEffortValues(t *testing.T) {
	want := []string{"minimal", "low", "medium", "high", "max"}
	if !reflect.DeepEqual(defaultEffortValues, want) {
		t.Errorf("defaultEffortValues = %v, want %v", defaultEffortValues, want)
	}
}

// TestValidateReasoningEffortSettings rejects obviously broken lists at
// settings-load time: empty entries and "off" as a member (off is a
// harness-only switch, never a vendor wire value).
func TestValidateReasoningEffortSettings(t *testing.T) {
	if err := ValidateReasoningEffortSettings([]string{"max", "high", "low"}); err != nil {
		t.Errorf("valid list rejected: %v", err)
	}
	if err := ValidateReasoningEffortSettings(nil); err != nil {
		t.Errorf("nil list must be allowed: %v", err)
	}
	if err := ValidateReasoningEffortSettings([]string{"max", "", "low"}); err == nil {
		t.Error("empty entry accepted")
	}
	if err := ValidateReasoningEffortSettings([]string{"max", "off"}); err == nil {
		t.Error(`"off" accepted as a wire value — it is harness-only`)
	}
}

// TestFromSettingsFileEffortValues verifies the settings.json channel:
// "effort_values" parses into cfg.EffortValues and its absence leaves nil
// (→ baseline fallback at resolution time).
func TestFromSettingsFileEffortValues(t *testing.T) {
	p := writeSettings(t, `{
		"model":"glm-5.3-flash",
		"reasoning_effort":"high",
		"effort_values":["max","high","low"]
	}`)
	cfg, err := FromSettingsFile(p)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !reflect.DeepEqual(cfg.EffortValues, []string{"max", "high", "low"}) {
		t.Errorf("EffortValues = %v, want [max high low]", cfg.EffortValues)
	}
	if v, ok := resolveReasoningEffort(cfg.ReasoningEffort, cfg.EffortValues); !ok || v != "high" {
		t.Errorf("settings high vs [max high low] resolved to (%q,%v), want (high,true)", v, ok)
	}

	p2 := writeSettings(t, `{"model":"deepseek-v4-pro"}`)
	cfg2, err := FromSettingsFile(p2)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if cfg2.EffortValues != nil {
		t.Errorf("absent effort_values = %v, want nil", cfg2.EffortValues)
	}
}

// TestApplyEnvEffortValues verifies the settings→env channel: ApplyEnv joins
// cfg.EffortValues into GOPI_EFFORT_VALUES (comma-separated) for the
// single-source env readers; a nil list sets nothing.
func TestApplyEnvEffortValues(t *testing.T) {
	cfg := &Config{EffortValues: []string{"max", "high", "low"}}
	ApplyEnv(cfg, true)
	if got := os.Getenv("GOPI_EFFORT_VALUES"); got != "max,high,low" {
		t.Errorf("GOPI_EFFORT_VALUES = %q, want max,high,low", got)
	}
	t.Setenv("GOPI_EFFORT_VALUES", "")
	cfg2 := &Config{}
	ApplyEnv(cfg2, true)
	if got := os.Getenv("GOPI_EFFORT_VALUES"); got != "" {
		t.Errorf("nil list must not set GOPI_EFFORT_VALUES, got %q", got)
	}
}

// TestDefaultEffortValuesAccessorIsTheResolverBaseline pins the ONE exported
// accessor a second package reads this whitelist through (Rules 13/14): the
// `/config` report must name the same list the resolver actually falls back to,
// and it must not be able to perturb it.
//
// THE DEFECT THIS GUARDS (bug row 01a1d207-1b98): the report printed
// `effort_values (unset)` in both columns because nothing exported this list, so
// the honest fix was one accessor here rather than a literal retyped in
// cmd/agent — a retyped copy goes green while the report drifts from behaviour.
func TestDefaultEffortValuesAccessorIsTheResolverBaseline(t *testing.T) {
	got := DefaultEffortValues()
	if !reflect.DeepEqual(got, defaultEffortValues) {
		t.Fatalf("DefaultEffortValues() = %v, want the resolver's own list %v", got, defaultEffortValues)
	}
	// Every member is really legal: the resolver accepts it against its own
	// fallback (case-insensitively, verbatim out), so the accessor cannot hand a
	// caller a list the resolver would reject.
	for _, v := range got {
		if resolved, ok := resolveReasoningEffort(v, nil); !ok || resolved != v {
			t.Errorf("baseline member %q does not resolve against the fallback: (%q, %v)", v, resolved, ok)
		}
	}
	// It is a COPY: a caller mutating its answer cannot change what the resolver
	// uses.
	got[0] = "caller-mutated"
	if defaultEffortValues[0] == "caller-mutated" {
		t.Fatalf("DefaultEffortValues() exposed the package's own slice — a caller's write reached the resolver's list (%v)", defaultEffortValues)
	}
	if _, ok := resolveReasoningEffort("caller-mutated", nil); ok {
		t.Error("a value injected through the accessor's slice became legal")
	}
}
