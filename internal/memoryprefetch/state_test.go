package memoryprefetch

import (
	"errors"
	"strings"
	"testing"
)

// step drives one typing step through the machine with a fresh extraction.
func step(t *testing.T, s *State, input string, cfg Config) Action {
	t.Helper()
	set := mustExtract(t, input, cfg)
	a, err := s.Step(GateInput{Text: input}, set, cfg)
	if err != nil {
		t.Fatalf("step(%q): %v", input, err)
	}
	return a
}

// triggerInput is a many-hit buffer: it fires the gate on the primary signal
// regardless of the throttle. It is deliberately SHORT so that the tests below
// have room to add new terms inside the candidate cap.
const triggerInput = "alpha bravo charlie delta"

// stateCfg is the configuration these tests drive the machine with: the shipped
// defaults, except that the candidate cap is raised so a test can add a NEW term
// to an already-retrieved buffer (the cap is exercised in extract_test.go).
func stateCfg() Config {
	cfg := DefaultConfig()
	cfg.MaxKeywords = 32
	return cfg
}

// TestState_FirstRetrievalFires pins the first action: a triggering input yields
// ActionFetch with the delta terms as the query.
func TestState_FirstRetrievalFires(t *testing.T) {
	cfg := stateCfg()
	s := NewState()
	a := step(t, s, triggerInput, cfg)
	if a.Kind != ActionFetch {
		t.Fatalf("kind = %q (reason %q), want %q", a.Kind, a.Reason, ActionFetch)
	}
	if a.Reason != ReasonFirstRetrieval {
		t.Fatalf("reason = %q, want %q", a.Reason, ReasonFirstRetrieval)
	}
	if a.Query == "" || len(a.NewTerms) == 0 {
		t.Fatalf("a fetch must carry a delta query, got %+v", a)
	}
	for _, term := range a.NewTerms {
		if !strings.Contains(a.Query, term) {
			t.Fatalf("query %q does not carry delta term %q", a.Query, term)
		}
	}
}

// TestState_DeltaNeverRefetchesSatisfiedTerms is the DELTA acceptance case: once
// terms are admitted, the query carries only the new ones.
func TestState_DeltaNeverRefetchesSatisfiedTerms(t *testing.T) {
	cfg := stateCfg()
	s := NewState()
	first := step(t, s, triggerInput, cfg)
	if err := s.Admit(first.NewTerms); err != nil {
		t.Fatalf("admit: %v", err)
	}
	s.Steps = cfg.ThrottleTypes // window passed: the throttle is not what refuses now
	second := step(t, s, triggerInput+" india juliet", cfg)
	if second.Kind == ActionNone {
		t.Fatalf("new terms must produce a delta action, got reason %q", second.Reason)
	}
	for _, term := range second.NewTerms {
		if contains(first.NewTerms, term) {
			t.Fatalf("delta re-fetched satisfied term %q: %v", term, second.NewTerms)
		}
	}
	for _, term := range []string{"india", "juliet"} {
		if !contains(second.NewTerms, term) {
			t.Fatalf("delta %v is missing the new term %q", second.NewTerms, term)
		}
	}
}

// TestState_NoNewTermsIsNotASecondFetch pins that a repeated buffer costs
// nothing: nothing new to learn means no retrieval.
func TestState_NoNewTermsIsNotASecondFetch(t *testing.T) {
	cfg := stateCfg()
	s := NewState()
	first := step(t, s, triggerInput, cfg)
	if err := s.Admit(first.NewTerms); err != nil {
		t.Fatalf("admit: %v", err)
	}
	s.Steps = cfg.ThrottleTypes
	again := step(t, s, triggerInput, cfg)
	if again.Kind != ActionNone {
		t.Fatalf("kind = %q, want %q — every term was already in hand", again.Kind, ActionNone)
	}
	if again.Reason != ReasonNoNewTerms {
		t.Fatalf("reason = %q, want %q", again.Reason, ReasonNoNewTerms)
	}
}

// TestState_ThrottleHoldsTheSecondRetrieval pins the throttle: inside the window
// a second retrieval is held back, and the action says which throttle did it.
func TestState_ThrottleHoldsTheSecondRetrieval(t *testing.T) {
	cfg := stateCfg()
	s := NewState()
	a := step(t, s, triggerInput, cfg)
	if a.Kind != ActionFetch {
		t.Fatalf("first step did not fetch: %+v", a)
	}
	if err := s.Admit(a.NewTerms); err != nil {
		t.Fatalf("admit: %v", err)
	}
	held := step(t, s, triggerInput+" india", cfg)
	if held.Kind != ActionNone {
		t.Fatalf("kind = %q, want %q inside the throttle window", held.Kind, ActionNone)
	}
	if held.Reason != ReasonThrottledTypes {
		t.Fatalf("reason = %q, want %q", held.Reason, ReasonThrottledTypes)
	}
}

// TestState_ThrottleReleasesAfterTheWindow pins the other half: the window is a
// delay, not a stop — with ThrottleTypes steps elapsed, the delta fires.
func TestState_ThrottleReleasesAfterTheWindow(t *testing.T) {
	cfg := stateCfg()
	s := NewState()
	a := step(t, s, triggerInput, cfg)
	if err := s.Admit(a.NewTerms); err != nil {
		t.Fatalf("admit: %v", err)
	}
	var last Action
	for i := 0; i < cfg.ThrottleTypes; i++ {
		last = step(t, s, triggerInput+" india", cfg)
	}
	if last.Kind == ActionNone {
		t.Fatalf("after %d steps the throttle still refused: reason %q", cfg.ThrottleTypes, last.Reason)
	}
	if last.Reason != ReasonNewTerms {
		t.Fatalf("reason = %q, want %q", last.Reason, ReasonNewTerms)
	}
}

// TestState_OnlyTheFirstRetrievalIsAFetch pins the shape difference: later
// retrievals are ActionRefresh, never ActionFetch.
func TestState_OnlyTheFirstRetrievalIsAFetch(t *testing.T) {
	cfg := stateCfg()
	s := NewState()
	first := step(t, s, triggerInput, cfg)
	if first.Kind != ActionFetch {
		t.Fatalf("kind = %q, want %q", first.Kind, ActionFetch)
	}
	if err := s.Admit(first.NewTerms); err != nil {
		t.Fatalf("admit: %v", err)
	}
	s.Steps = cfg.ThrottleTypes
	second := step(t, s, triggerInput+" india", cfg)
	if second.Kind != ActionRefresh {
		t.Fatalf("kind = %q, want %q", second.Kind, ActionRefresh)
	}
}

// TestState_GateRefusalIsReportedAndCostsNothing pins that a non-triggering input
// is passed through with its own reason and leaves the throttle untouched.
func TestState_GateRefusalIsReportedAndCostsNothing(t *testing.T) {
	cfg := stateCfg()
	s := NewState()
	a := step(t, s, "hi", cfg)
	if a.Kind != ActionNone || a.Reason != ReasonNoTrigger {
		t.Fatalf("action = %+v, want none/%s", a, ReasonNoTrigger)
	}
	if a.Decision.Reason != GateReasonInputTooShort {
		t.Fatalf("decision reason = %q, want %q", a.Decision.Reason, GateReasonInputTooShort)
	}
	if s.Fetched {
		t.Fatal("a refused input must not mark the session as fetched")
	}
}

// TestState_SkipExcludesPermanently pins the operator dismissal: an excluded term
// never returns in a delta, however often it is retyped.
func TestState_SkipExcludesPermanently(t *testing.T) {
	cfg := stateCfg()
	s := NewState()
	first := step(t, s, triggerInput, cfg)
	if err := s.Skip([]string{"alpha"}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	if err := s.Admit(first.NewTerms); err != nil {
		t.Fatalf("admit: %v", err)
	}
	s.Steps = cfg.ThrottleTypes
	next := step(t, s, triggerInput+" india", cfg)
	if contains(next.NewTerms, "alpha") {
		t.Fatalf("delta %v carries an excluded term", next.NewTerms)
	}
	if !contains(next.NewTerms, "india") {
		t.Fatalf("delta %v lost the genuine new term", next.NewTerms)
	}
}

// TestState_ResetForgetsTheSession pins Reset: a new typing session starts with
// no history, so the first retrieval is a fetch again.
func TestState_ResetForgetsTheSession(t *testing.T) {
	cfg := stateCfg()
	s := NewState()
	a := step(t, s, triggerInput, cfg)
	if err := s.Admit(a.NewTerms); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if err := s.Skip([]string{"alpha"}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	s.Reset()
	if s.Fetched || s.Steps != 0 || len(s.Satisfied) != 0 || len(s.Excluded) != 0 {
		t.Fatalf("reset left state behind: %+v", s)
	}
	again := step(t, s, triggerInput, cfg)
	if again.Kind != ActionFetch {
		t.Fatalf("after reset kind = %q, want %q", again.Kind, ActionFetch)
	}
}

// TestState_RefusesNilReceiverAndEmptyTerms pins the named refusals (Rule 15):
// no silent nil-map write, no empty key in the satisfied set.
func TestState_RefusesNilReceiverAndEmptyTerms(t *testing.T) {
	var nilState *State
	if _, err := nilState.Step(GateInput{Text: triggerInput}, CandidateSet{}, Config{}); !errors.Is(err, ErrNilState) {
		t.Fatalf("nil Step error = %v, want ErrNilState", err)
	}
	if err := nilState.Admit([]string{"x"}); !errors.Is(err, ErrNilState) {
		t.Fatalf("nil Admit error = %v, want ErrNilState", err)
	}
	if err := nilState.Skip([]string{"x"}); !errors.Is(err, ErrNilState) {
		t.Fatalf("nil Skip error = %v, want ErrNilState", err)
	}
	s := NewState()
	if err := s.Admit([]string{"ok", ""}); !errors.Is(err, ErrEmptyTerm) {
		t.Fatalf("empty Admit error = %v, want ErrEmptyTerm", err)
	}
	if err := s.Skip([]string{""}); !errors.Is(err, ErrEmptyTerm) {
		t.Fatalf("empty Skip error = %v, want ErrEmptyTerm", err)
	}
	nilState.Reset() // must not panic
}

// TestState_InvalidConfigIsRefusedByStep pins that the machine validates its
// configuration instead of computing with it.
func TestState_InvalidConfigIsRefusedByStep(t *testing.T) {
	s := NewState()
	bad := Config{MinInputRunes: 100, WeakInputRunes: 10}
	_, err := s.Step(GateInput{Text: triggerInput}, CandidateSet{}, bad)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Step error = %v, want ErrInvalidConfig", err)
	}
}
