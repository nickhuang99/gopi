package config

import (
	"strings"
	"testing"
)

// TestFlagOverridesRefusesSingleDashOption is the REGRESSION test for the
// `gopi -z` defect (2026-09-21, Rule 5: every fix ships a minimal repro).
//
// The defect, as the operator reported it: ledger rows carried
// `run_cmd: gopi -z` and `summary: [auto] -z`. `-z` was NOT a typo in the
// ledger — it was a positional argument, so the parser put it in Positionals,
// `readInput` turned it into the task prompt, and the harness-registered row
// recorded its own verbatim argv. The run did what it was told; the surprise is
// that the parser accepted an OPTION-SHAPED token as prompt text while every
// other gopi surface refuses an unknown `--flag` loudly (Rule 15).
func TestFlagOverridesRefusesSingleDashOption(t *testing.T) {
	_, err := FlagOverrides([]string{"-z"})
	if err == nil {
		t.Fatalf("FlagOverrides(-z) = nil error, want a refusal: an option-shaped token must NEVER become the prompt")
	}
	if !strings.Contains(err.Error(), "-z") {
		t.Errorf("refusal %q does not name the offending token %q", err.Error(), "-z")
	}
	// The message must point at the escape hatch, or an operator who really
	// means the literal text has nowhere to go.
	if !strings.Contains(err.Error(), "--prompt") {
		t.Errorf("refusal %q does not name `--prompt` as the way to pass the text literally", err.Error())
	}
}

// TestFlagOverridesSingleDashRefusalIsPerToken proves the refusal is not a
// blanket dash ban: any single-dash spelling is refused, and the long-flag
// spelling of the SAME letter is still a normal (unknown-flag) error — the two
// classes stay distinguishable to a reader.
func TestFlagOverridesSingleDashRefusalIsPerToken(t *testing.T) {
	for _, tok := range []string{"-z", "-x", "-verbose", "-1"} {
		if _, err := FlagOverrides([]string{tok}); err == nil {
			t.Errorf("FlagOverrides(%q) = nil error, want a refusal", tok)
		}
	}
	if _, err := FlagOverrides([]string{"--z"}); err == nil {
		t.Errorf("FlagOverrides(--z) = nil error, want the unknown-long-flag refusal (unchanged path)")
	}
}

// TestFlagOverridesSingleDashStaysUsableEverywhereElse is the other half of the
// regression: the two DELIBERATE ways to pass such text must keep working, and
// ordinary prompts must be untouched.
func TestFlagOverridesSingleDashStaysUsableEverywhereElse(t *testing.T) {
	// ① --prompt says it literally (flag value, never reclassified).
	cfg, err := FlagOverrides([]string{"--prompt", "-z"})
	if err != nil {
		t.Fatalf("--prompt -z must be accepted: %v", err)
	}
	if cfg.Prompt != "-z" {
		t.Errorf("Prompt = %q, want %q", cfg.Prompt, "-z")
	}
	// ② a prompt that merely CONTAINS a dash-word is still plain text.
	cfg, err = FlagOverrides([]string{"explain what -z does"})
	if err != nil {
		t.Fatalf("a positional prompt mentioning a dash-word must be accepted: %v", err)
	}
	if len(cfg.Positionals) != 1 || cfg.Positionals[0] != "explain what -z does" {
		t.Errorf("Positionals = %q, want the prompt text", cfg.Positionals)
	}
	// ③ the documented flag-value-aware positional collection is unchanged.
	cfg, err = FlagOverrides([]string{"--prompt", "-z", "chart.png"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Positionals) != 1 || cfg.Positionals[0] != "chart.png" {
		t.Errorf("Positionals = %q, want [chart.png]", cfg.Positionals)
	}
}
