package agent

import (
	"encoding/json"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// TestNewAgent_ReasoningEffortOff disables thinking entirely
// (P0-1: enum off → thinking disabled, no wire effort — dsh resolveThinking).
func TestNewAgent_ReasoningEffortOff(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_REASONING_EFFORT", "off")

	ag, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if ag.Thinking == nil || ag.Thinking.Type != "disabled" {
		t.Errorf("GOPI_REASONING_EFFORT=off: expected Thinking.Type=disabled, got %+v", ag.Thinking)
	}
	if ag.ReasoningEffort != "" {
		t.Errorf("GOPI_REASONING_EFFORT=off: expected ReasoningEffort=\"\" (never wired), got %q", ag.ReasoningEffort)
	}
}

// TestNewAgent_ReasoningEffortHigh wires the effort enum with thinking on.
func TestNewAgent_ReasoningEffortHigh(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_REASONING_EFFORT", "high")

	ag, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if ag.Thinking == nil || ag.Thinking.Type != "enabled" {
		t.Errorf("GOPI_REASONING_EFFORT=high: expected Thinking.Type=enabled, got %+v", ag.Thinking)
	}
	if ag.ReasoningEffort != "high" {
		t.Errorf("GOPI_REASONING_EFFORT=high: expected ReasoningEffort=high, got %q", ag.ReasoningEffort)
	}
}

// TestNewAgent_ReasoningEffortInvalid falls back to the default (minimal)
// WITHOUT auto-enabling thinking — an invalid value does not imply intent
// (PA ruling 2026-08-27: only an explicit VALID non-off effort enables it).
func TestNewAgent_ReasoningEffortInvalid(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_REASONING_EFFORT", "turbo")

	ag, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if ag.ReasoningEffort != "minimal" {
		t.Errorf("invalid effort: expected fallback to minimal, got %q", ag.ReasoningEffort)
	}
	if ag.Thinking == nil || ag.Thinking.Type != "disabled" {
		t.Errorf("invalid effort: expected Thinking.Type=disabled (invalid value does not auto-enable), got %+v", ag.Thinking)
	}
}

// TestNewAgent_EffortOffBeatsThinkingEnabled verifies effort=off wins the
// conflict (warning logged, thinking disabled).
func TestNewAgent_EffortOffBeatsThinkingEnabled(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_REASONING_EFFORT", "off")
	t.Setenv("GOPI_THINKING", "enabled")

	ag, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if ag.Thinking == nil || ag.Thinking.Type != "disabled" {
		t.Errorf("effort=off + thinking=enabled: expected disabled, got %+v", ag.Thinking)
	}
	if ag.ReasoningEffort != "" {
		t.Errorf("expected empty wire effort, got %q", ag.ReasoningEffort)
	}
}

// TestNewAgent_DefaultMinimalWired verifies the default: the internal effort
// is minimal (dead-config fix), thinking is disabled, and NO reasoning_effort
// reaches the wire — effort is only wired while thinking is enabled (PA
// ruling 2026-08-27).
func TestNewAgent_DefaultMinimalWired(t *testing.T) {
	newTestAgentEnv(t)

	ag, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if ag.ReasoningEffort != "minimal" {
		t.Errorf("default: expected ReasoningEffort=minimal, got %q", ag.ReasoningEffort)
	}
	// Wire check through the real request builder.
	req := deepseek.BuildClaudeRequest(
		"test-model", nil, ag.SystemPrompt, "", claudeToolDefs,
		deepseek.DefaultClaudeMaxTokens, ag.Thinking, ag.ReasoningEffort)
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := m["reasoning_effort"]; ok {
		t.Errorf("wire must NOT contain reasoning_effort (thinking disabled), got %v: %s", got, string(b))
	}
	if _, ok := m["thinking"]; !ok {
		t.Errorf("wire missing thinking block: %s", string(b))
	}
}

// TestNewAgent_EffortValuesListValidates covers the settings-driven effort
// whitelist (config-ization: the list IS the provider's schema, data not
// code). GOPI_EFFORT_VALUES is what config.ApplyEnv injects from the
// settings.json "effort_values" array:
//   - effort listed → passed through verbatim (glm list: high stays high)
//   - effort NOT listed → falls back to minimal WITHOUT auto-enabling
//     thinking (same invalid-value semantics as before — the PA ruling
//     only lets an explicit VALID effort enable thinking)
//   - no list (nil) → the DeepSeek baseline (minimal..max) applies
func TestNewAgent_EffortValuesListValidates(t *testing.T) {
	newTestAgentEnv(t)

	// glm-style list: max/high/low. high is listed → verbatim; medium is
	// not → minimal fallback, thinking NOT auto-enabled.
	t.Run("listed-high-passthrough", func(t *testing.T) {
		t.Setenv("GOPI_EFFORT_VALUES", "max,high,low")
		t.Setenv("GOPI_REASONING_EFFORT", "high")
		ag, err := NewAgent("glm-5.3-flash")
		if err != nil {
			t.Fatalf("NewAgent: %v", err)
		}
		if ag.ReasoningEffort != "high" {
			t.Errorf("listed effort: expected verbatim high, got %q", ag.ReasoningEffort)
		}
		if ag.Thinking == nil || ag.Thinking.Type != "enabled" {
			t.Errorf("explicit valid listed effort must auto-enable thinking, got %+v", ag.Thinking)
		}
	})

	t.Run("unlisted-medium-fallback-no-autothink", func(t *testing.T) {
		t.Setenv("GOPI_EFFORT_VALUES", "max,high,low")
		t.Setenv("GOPI_REASONING_EFFORT", "medium")
		ag, err := NewAgent("glm-5.3-flash")
		if err != nil {
			t.Fatalf("NewAgent: %v", err)
		}
		if ag.ReasoningEffort != "minimal" {
			t.Errorf("unlisted effort: expected minimal fallback, got %q", ag.ReasoningEffort)
		}
		if ag.Thinking == nil || ag.Thinking.Type != "disabled" {
			t.Errorf("unlisted effort must NOT auto-enable thinking, got %+v", ag.Thinking)
		}
	})

	// nil list → DeepSeek baseline: medium is a legal baseline value.
	t.Run("nil-list-baseline-medium", func(t *testing.T) {
		t.Setenv("GOPI_REASONING_EFFORT", "medium")
		ag, err := NewAgent("test-model")
		if err != nil {
			t.Fatalf("NewAgent: %v", err)
		}
		if ag.ReasoningEffort != "medium" {
			t.Errorf("nil list: expected baseline medium to pass, got %q", ag.ReasoningEffort)
		}
	})

	// A listed value beyond the baseline ("max" in a glm list is baseline,
	// so use a purely synthetic one) still passes verbatim — what you send
	// is what goes.
	t.Run("synthetic-listed-value-passthrough", func(t *testing.T) {
		t.Setenv("GOPI_EFFORT_VALUES", "ultra,low")
		t.Setenv("GOPI_REASONING_EFFORT", "ultra")
		ag, err := NewAgent("glm-5.3-flash")
		if err != nil {
			t.Fatalf("NewAgent: %v", err)
		}
		if ag.ReasoningEffort != "ultra" {
			t.Errorf("custom listed value: expected verbatim ultra, got %q", ag.ReasoningEffort)
		}
	})
}
