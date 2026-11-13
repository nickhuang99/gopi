package agent

import (
	"os"

	"github.com/nick/gopi/internal/config"
)

// The three environment names the thinking resolution reads. They are declared
// ONCE here and referenced by both the resolver (NewAgentResuming's switch) and
// the coupling predicate below, so "what decides thinking" has one spelling per
// name instead of one per call site (Rules 1/13).
const (
	// ThinkingEnvName carries the explicit thinking switch: enabled/disabled.
	ThinkingEnvName = "GOPI_THINKING"
	// ReasoningEffortEnvName carries the depth knob, which ALSO implies thinking
	// when it is explicit, valid and not "off" (PA ruling 2026-08-27).
	ReasoningEffortEnvName = "GOPI_REASONING_EFFORT"
	// EffortValuesEnvName carries the legal effort list, joined by
	// config.ApplyEnv from the settings key of the same name.
	EffortValuesEnvName = "GOPI_EFFORT_VALUES"
)

// EffortImpliesThinking states the ONE cross-field coupling of the thinking
// knob, in the exact form the resolver's switch tests: an EXPLICIT, VALID,
// non-"off" reasoning effort with no explicit thinking auto-enables thinking.
// NewAgentResuming's switch calls it, and so does the /config report — which
// must therefore NOT claim the compiled-in default (disabled) runs when this
// returns true.
//
// Both arguments are facts the caller already measured, never values derived
// here: effortExplicit is "the environment carried a VALID effort", not "an
// effort resolved to something" — an invalid value falls back to the baseline
// WITHOUT implying intent (the ruling's "invalid value does not imply intent").
func EffortImpliesThinking(effortExplicit, effortOff bool) bool {
	return effortExplicit && !effortOff
}

// ThinkingImpliedByEffort answers EffortImpliesThinking from the ENVIRONMENT, for
// a caller that holds no resolution of its own — the /config report, which
// describes a process rather than running the loop inside it.
//
// It applies the resolver's own validation (config.ResolveReasoningEffort against
// the declared effort list), so an effort the model's schema rejects does NOT
// count as explicit here either: that value falls back to the baseline and, by
// the same ruling, does not imply wanting thinking. getenv is threaded so a
// caller (and a test) reads the very environment it is reporting on, instead of a
// second hand-rolled copy of os.Getenv (Rule 2).
func ThinkingImpliedByEffort(getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	if getenv(ThinkingEnvName) != "" {
		return false // an explicit thinking setting owns the switch
	}
	effort := getenv(ReasoningEffortEnvName)
	if effort == "" {
		return false
	}
	values := splitEffortValuesEnv(getenv(EffortValuesEnvName))
	if err := config.ValidateReasoningEffortSettings(values); err != nil {
		values = nil // the resolver ignores an invalid list and uses the baseline
	}
	resolved, valid := config.ResolveReasoningEffort(effort, values)
	return valid && EffortImpliesThinking(true, resolved == config.EffortOff)
}
