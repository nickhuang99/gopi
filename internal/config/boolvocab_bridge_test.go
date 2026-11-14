package config

// boolvocab_bridge_test.go — the bridge between this package and the ONE
// boolean-vocabulary definition (internal/boolvocab).
//
// WHY A BRIDGE TEST AND NOT A MIRROR. config's env channel (EnvBool) and the
// vocabulary now live in two packages: config cannot host the definition
// because internal/heavytest must not import it (config → internal/scratch, and
// a scratch in-package test imports heavytest). The definition therefore lives
// in the leaf, and this test is the check Rule 14 requires of every split — an
// unreachable-but-pinned pair, never a silent one.

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/boolvocab"
)

// TestEnvBoolOnIsATruthySpelling pins the exported canonical ON value to the
// vocabulary: EnvBoolOn is the spelling ApplyEnv writes for an enabled switch,
// so a vocabulary edit that dropped it would make every enabled child read as
// DISABLED — silently, since "1" is also the commonest hand-written value.
func TestEnvBoolOnIsATruthySpelling(t *testing.T) {
	if !boolvocab.IsTrue(EnvBoolOn) {
		t.Errorf("EnvBoolOn = %q is not a truthy spelling of %s — ApplyEnv writes it for every ENABLED switch, "+
			"so every child would read its switch as off (Rule 14: the pair is pinned, not mirrored)",
			EnvBoolOn, boolvocab.TrueValues)
	}
}

// TestEnvBoolReadsTheVocabulary is the behavioural half of the bridge: the
// process-env channel accepts exactly the vocabulary's spellings — enabled
// spellings ON, the canonical force-written "off" (the 2026-08-29 regression)
// and every unknown value OFF.
func TestEnvBoolReadsTheVocabulary(t *testing.T) {
	const probeEnv = "GOPI_TEST_BOOLVOCAB_BRIDGE"
	for _, in := range []string{"1", "true", "on", "yes", "enabled", "TRUE", "  Yes  ", "On"} {
		t.Setenv(probeEnv, in)
		if !EnvBool(probeEnv) {
			t.Errorf("EnvBool(%q=%q) = false, want true (the vocabulary's truthy set)", probeEnv, in)
		}
	}
	// "off" is what ApplyEnv force-writes for a DISABLED switch; reading it as
	// ON is the regression this vocabulary exists to prevent.
	for _, in := range []string{"0", "false", "off", "no", "disabled", "OFF", " off ", "", "nope", "2"} {
		t.Setenv(probeEnv, in)
		if EnvBool(probeEnv) {
			t.Errorf("EnvBool(%q=%q) = true — truthy-only parsing: the canonical %q must never enable a switch",
				probeEnv, in, in)
		}
	}
}

// TestIsTrueValueAndIsFalseValue mirrors the raw-string channel onto the same
// vocabulary, so the settings/JSON readers and the env reader cannot diverge.
func TestIsTrueValueAndIsFalseValue(t *testing.T) {
	for _, in := range []string{"1", "true", "on", "yes", "enabled", " On "} {
		if !IsTrueValue(in) || IsFalseValue(in) {
			t.Errorf("IsTrueValue(%q)/IsFalseValue(%q) = %v/%v, want true/false", in, in, IsTrueValue(in), IsFalseValue(in))
		}
	}
	for _, in := range []string{"0", "false", "off", "no", "disabled", " Off "} {
		if !IsFalseValue(in) || IsTrueValue(in) {
			t.Errorf("IsFalseValue(%q)/IsTrueValue(%q) = %v/%v, want true/false", in, in, IsFalseValue(in), IsTrueValue(in))
		}
	}
	for _, in := range []string{"", " ", "maybe", "2"} {
		if IsTrueValue(in) || IsFalseValue(in) {
			t.Errorf("%q is neither an ON nor an OFF spelling — both predicates must be false (a default-ON switch stays on)", in)
		}
	}
}
