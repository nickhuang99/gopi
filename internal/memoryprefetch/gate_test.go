package memoryprefetch

import (
	"strings"
	"testing"
)

// mustExtract fails the test when extraction refuses.
func mustExtract(t *testing.T, input string, cfg Config) CandidateSet {
	t.Helper()
	set, err := ExtractCandidates(input, cfg)
	if err != nil {
		t.Fatalf("extract(%q): %v", input, err)
	}
	return set
}

// pad brings an input to exactly n runes using a rune that is neither a letter
// nor a digit, so padding never adds a candidate term.
func pad(input string, n int) string {
	have := len([]rune(input))
	if have >= n {
		return input
	}
	return input + strings.Repeat(".", n-have)
}

// TestGate_ShortInputNeverTriggers is the acceptance case "short input does not
// trigger": three candidate terms inside a short buffer still cost nothing,
// because the rune floor is checked FIRST.
func TestGate_ShortInputNeverTriggers(t *testing.T) {
	cfg := DefaultConfig()
	const shortInput = "alpha bravo charlie"
	if runes := len([]rune(shortInput)); runes >= cfg.MinInputRunes {
		t.Fatalf("fixture no longer short: %d runes >= MinInputRunes=%d", runes, cfg.MinInputRunes)
	}
	set := mustExtract(t, shortInput, cfg)
	if set.HitCount() < cfg.StrongHitCount {
		t.Fatalf("fixture must carry at least %d hits to prove the floor wins, got %d",
			cfg.StrongHitCount, set.HitCount())
	}
	d := EvaluateGate(GateInput{Text: shortInput}, set, cfg)
	if d.Trigger {
		t.Fatalf("short input triggered: %+v", d)
	}
	if d.Reason != GateReasonInputTooShort {
		t.Fatalf("reason = %q, want %q", d.Reason, GateReasonInputTooShort)
	}
}

// TestGate_LongManyHitInputTriggers is the acceptance case "long + many-hit
// input does trigger".
func TestGate_LongManyHitInputTriggers(t *testing.T) {
	cfg := DefaultConfig()
	input := strings.Repeat("alpha bravo charlie delta echo foxtrot ", 3)
	set := mustExtract(t, input, cfg)
	d := EvaluateGate(GateInput{Text: input}, set, cfg)
	if !d.Trigger {
		t.Fatalf("long many-hit input did not trigger: %+v", d)
	}
	if d.Reason != GateReasonTriggered {
		t.Fatalf("reason = %q, want %q", d.Reason, GateReasonTriggered)
	}
	if d.Hits < cfg.StrongHitCount {
		t.Fatalf("hits = %d, want >= StrongHitCount=%d", d.Hits, cfg.StrongHitCount)
	}
}

// TestGate_StrongHitBoundary pins the PRIMARY threshold exactly: N-1 hits do not
// fire on that signal, N hits do.
func TestGate_StrongHitBoundary(t *testing.T) {
	cfg := DefaultConfig()
	base := "firstword secondword"
	base = pad(base, cfg.MinInputRunes) // long enough to clear the floor
	if len([]rune(base)) >= cfg.WeakInputRunes {
		t.Fatalf("fixture reached the weak-length path: %d runes >= %d", len([]rune(base)), cfg.WeakInputRunes)
	}

	below := mustExtract(t, base, cfg)
	if below.HitCount() != cfg.StrongHitCount-1 {
		t.Fatalf("fixture has %d hits (%v), want exactly StrongHitCount-1=%d",
			below.HitCount(), below.TermsText(), cfg.StrongHitCount-1)
	}
	if d := EvaluateGate(GateInput{Text: base}, below, cfg); d.Trigger {
		t.Fatalf("N-1 hits fired the primary signal: %+v", d)
	}

	at := mustExtract(t, base+" thirdword", cfg)
	if at.HitCount() != cfg.StrongHitCount {
		t.Fatalf("fixture has %d hits (%v), want exactly StrongHitCount=%d",
			at.HitCount(), at.TermsText(), cfg.StrongHitCount)
	}
	if d := EvaluateGate(GateInput{Text: base + " thirdword"}, at, cfg); !d.Trigger {
		t.Fatalf("N hits did not fire the primary signal: %+v", d)
	}
}

// TestGate_WeakLengthBoundary pins the SECONDARY (length) threshold exactly:
// WeakInputRunes-1 runes do not fire, WeakInputRunes do — with the same hits.
func TestGate_WeakLengthBoundary(t *testing.T) {
	cfg := DefaultConfig()
	base := "firstword secondword"
	if got := len([]rune(base)); got >= cfg.MinInputRunes {
		t.Fatalf("fixture is not short enough to need padding: %d runes", got)
	}

	oneBelow := pad(base, cfg.WeakInputRunes-1)
	set := mustExtract(t, oneBelow, cfg)
	if set.HitCount() != cfg.WeakHitCount {
		t.Fatalf("fixture has %d hits (%v), want WeakHitCount=%d", set.HitCount(), set.TermsText(), cfg.WeakHitCount)
	}
	if d := EvaluateGate(GateInput{Text: oneBelow}, set, cfg); d.Trigger {
		t.Fatalf("%d runes (< WeakInputRunes=%d) fired the secondary path: %+v",
			len([]rune(oneBelow)), cfg.WeakInputRunes, d)
	}

	atThreshold := pad(base, cfg.WeakInputRunes)
	setAt := mustExtract(t, atThreshold, cfg)
	if d := EvaluateGate(GateInput{Text: atThreshold}, setAt, cfg); !d.Trigger {
		t.Fatalf("%d runes (== WeakInputRunes=%d) did not fire the secondary path: %+v",
			len([]rune(atThreshold)), cfg.WeakInputRunes, d)
	}
}

// TestGate_TooFewHitsIsNamed pins that a long-but-thin input is refused with a
// named reason, not an absence of answer.
func TestGate_TooFewHitsIsNamed(t *testing.T) {
	cfg := DefaultConfig()
	input := pad("firstword", cfg.WeakInputRunes)
	set := mustExtract(t, input, cfg)
	d := EvaluateGate(GateInput{Text: input}, set, cfg)
	if d.Trigger {
		t.Fatalf("a one-term long input must not fire: %+v", d)
	}
	if d.Reason != GateReasonTooFewHits {
		t.Fatalf("reason = %q, want %q", d.Reason, GateReasonTooFewHits)
	}
}

// TestGate_ConfiguredThresholdsAreHonoured pins that the thresholds are really
// configurable — a caller may lower them, and the boundary moves with them.
func TestGate_ConfiguredThresholdsAreHonoured(t *testing.T) {
	cfg := Config{MinInputRunes: 5, StrongHitCount: 2, WeakHitCount: 2, WeakInputRunes: 5}
	const input = "alpha bravo"
	set := mustExtract(t, input, cfg)
	d := EvaluateGate(GateInput{Text: input}, set, cfg)
	if !d.Trigger {
		t.Fatalf("configured thresholds (%+v) did not fire on %q: %+v", cfg, input, d)
	}
	if d.StrongHitCount != 2 || d.MinInputRunes != 5 {
		t.Fatalf("decision must carry the thresholds it used, got %+v", d)
	}
}

// TestGate_CarriesItsNumbers pins the logging contract: a decision always states
// the numbers it decided on.
func TestGate_CarriesItsNumbers(t *testing.T) {
	cfg := DefaultConfig()
	input := pad("firstword secondword", cfg.WeakInputRunes)
	set := mustExtract(t, input, cfg)
	d := EvaluateGate(GateInput{Text: input}, set, cfg)
	if d.Hits != set.HitCount() {
		t.Fatalf("decision hits = %d, want %d", d.Hits, set.HitCount())
	}
	if d.InputRunes != len([]rune(input)) {
		t.Fatalf("decision runes = %d, want %d", d.InputRunes, len([]rune(input)))
	}
	if d.WeakInputRunes != cfg.WeakInputRunes || d.WeakHitCount != cfg.WeakHitCount {
		t.Fatalf("decision must carry the weak thresholds, got %+v", d)
	}
}

// TestGate_RuneLenOverrideIsUsed pins GateInput.RuneLen: a caller that measured
// the buffer itself is not re-counted, and a zero means "count it".
func TestGate_RuneLenOverrideIsUsed(t *testing.T) {
	cfg := DefaultConfig()
	set := mustExtract(t, "alpha bravo charlie delta echo", cfg)
	// A caller reporting a huge buffer with the gate's own default text.
	d := EvaluateGate(GateInput{Text: "", RuneLen: cfg.WeakInputRunes}, set, cfg)
	if !d.Trigger {
		t.Fatalf("RuneLen override was ignored: %+v", d)
	}
	if d.InputRunes != cfg.WeakInputRunes {
		t.Fatalf("InputRunes = %d, want the override %d", d.InputRunes, cfg.WeakInputRunes)
	}
}
