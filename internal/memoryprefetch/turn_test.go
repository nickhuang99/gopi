package memoryprefetch

// turn_test.go — the TURN-BOUNDARY core, driven directly. These tests exist
// because the whole point of turn.go is that a turn's input is not a buffer: a
// retrieval key assembled from a notice's verbatim text plus the bytes of a
// pasted file is a different shape from a typed line, and the bounds that keep it
// cheap (the payload clip, the named degradation) are claims that must be
// measured rather than asserted.

import (
	"strings"
	"testing"
)

// turnTriggerText is one turn's own words that clear the gate: >= 3 candidates
// and >= MinInputRunes, so the gate's PRIMARY signal is what admits it.
const turnTriggerText = "memory prefetch turn boundary retrieval"

func TestTurnTextJoinsPartsAndPayloadsAndSkipsEmpties(t *testing.T) {
	in := TurnInput{
		Parts:    []string{"  ", turnTriggerText, ""},
		Payloads: []string{"payload one", ""},
	}
	got, degs := TurnText(in, Config{})
	want := turnTriggerText + turnPartSeparator + "payload one"
	if got != want {
		t.Fatalf("TurnText = %q, want %q", got, want)
	}
	if len(degs) != 0 {
		t.Fatalf("a turn inside the payload bound recorded a degradation: %+v", degs)
	}
}

// The parts of one turn are joined by a NEWLINE, so two parts cannot be glued
// into a word neither of them contains — the extractor splits on whitespace, and
// `alpha` + `beta` must never become the single candidate `alphabeta`.
func TestTurnTextKeepsPartsDistinctAcrossTheJoin(t *testing.T) {
	text, _ := TurnText(TurnInput{Parts: []string{"alpha", "beta"}}, Config{})
	set, err := ExtractCandidates(text, Config{})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, term := range set.TermsText() {
		if term == "alphabeta" {
			t.Fatalf("the join glued two parts into one candidate: %v", set.TermsText())
		}
	}
}

// A payload is bounded and the bound is RECORDED: a whole-block paste is
// unbounded by construction, so a clipped one must say so rather than look like a
// turn that happened to be short.
func TestTurnTextClipsAnOverLongPayloadAndRecordsIt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTurnPayloadRunes = 32
	payload := strings.Repeat("a", 200)
	got, degs := TurnText(TurnInput{Payloads: []string{payload}}, cfg)
	if runeLenOf(got) != 32 {
		t.Fatalf("TurnText kept %d runes of a %d-rune payload bounded at %d", runeLenOf(got), len(payload), cfg.MaxTurnPayloadRunes)
	}
	if len(degs) != 1 || degs[0].Code != DegradationTurnPayloadClipped {
		t.Fatalf("a clipped payload must be recorded as %q, got %+v", DegradationTurnPayloadClipped, degs)
	}
	if !strings.Contains(degs[0].Detail, "MaxTurnPayloadRunes=32") {
		t.Errorf("the degradation must name the bound it applied, got %q", degs[0].Detail)
	}
}

// The clip is on a RUNE boundary: a CJK payload cut mid-character would be a
// corrupt string, not a shorter one.
func TestTurnTextClipsOnARuneBoundary(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTurnPayloadRunes = 5
	got, _ := TurnText(TurnInput{Payloads: []string{"记忆预取轮边界检索"}}, cfg)
	if runeLenOf(got) != 5 {
		t.Fatalf("TurnText = %q (%d runes), want 5 whole runes", got, runeLenOf(got))
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("the clip produced a replacement character: %q", got)
	}
}

// TurnStep is Step with the turn's input assembled: the FIRST turn worth a
// retrieval fetches, the SAME turn again costs nothing (the delta), and a turn
// whose text yields no term is the machine's own named refusal rather than an
// error.
func TestTurnStepFetchesOnceThenRefusesTheSameTurn(t *testing.T) {
	cfg := DefaultConfig()
	s := NewState()
	first, err := TurnStep(s, TurnInput{Parts: []string{turnTriggerText}}, cfg)
	if err != nil {
		t.Fatalf("TurnStep on a usable turn: %v", err)
	}
	if first.Kind != ActionFetch {
		t.Fatalf("the first turn's action = %q (reason %q), want %q", first.Kind, first.Reason, ActionFetch)
	}
	if !strings.Contains(first.Query, "prefetch") {
		t.Errorf("the query does not carry the turn's own words: %q", first.Query)
	}
	if err := s.Admit(first.NewTerms); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	again, err := TurnStep(s, TurnInput{Parts: []string{turnTriggerText}}, cfg)
	if err != nil {
		t.Fatalf("TurnStep on the same turn: %v", err)
	}
	if again.Kind != ActionNone || again.Reason != ReasonNoNewTerms {
		t.Fatalf("a repeated turn = %q/%q, want %q/%q", again.Kind, again.Reason, ActionNone, ReasonNoNewTerms)
	}
}

func TestTurnStepRefusesTextWithNoUsableTerms(t *testing.T) {
	// Constants only: no candidate survives the stop-word list, so the gate's
	// primary signal is zero however long the text is — the honest refusal.
	a, err := TurnStep(NewState(), TurnInput{Parts: []string{strings.Repeat("the and of to ", 20)}}, DefaultConfig())
	if err != nil {
		t.Fatalf("TurnStep on unusable text must refuse, not fail: %v", err)
	}
	if a.Kind != ActionNone || a.Reason != ReasonNoTrigger {
		t.Fatalf("action = %q/%q, want %q/%q", a.Kind, a.Reason, ActionNone, ReasonNoTrigger)
	}
	if a.Decision.Hits != 0 {
		t.Errorf("the decision claims %d candidates on a stop-word-only turn", a.Decision.Hits)
	}
}

// Step does NOT mark anything satisfied: the caller admits only after a retrieval
// succeeded, so a turn whose lookup failed is RETRIED rather than pretending its
// terms are in hand. This is the property that makes a give-up cost a retry — and
// at a turn boundary the retry happens on the VERY NEXT TURN, because the turn
// throttle is 1 (see DefaultTurnThrottleTypes: the throttle guards an event
// stream, and a turn is not one).
func TestTurnStepDoesNotAdmitWithoutASuccessfulRetrieval(t *testing.T) {
	cfg := DefaultConfig()
	s := NewState()
	first, err := TurnStep(s, TurnInput{Parts: []string{turnTriggerText}}, cfg)
	if err != nil {
		t.Fatalf("TurnStep: %v", err)
	}
	if first.Kind != ActionFetch {
		t.Fatalf("first action = %q, want %q", first.Kind, ActionFetch)
	}
	if TurnConfig(cfg).ThrottleTypes != DefaultTurnThrottleTypes {
		t.Fatalf("TurnConfig left ThrottleTypes at %d — the caller's throttle would delay a retry by that many turns", TurnConfig(cfg).ThrottleTypes)
	}
	// No Admit: the very next turn must ask the same question again.
	second, err := TurnStep(s, TurnInput{Parts: []string{turnTriggerText}}, cfg)
	if err != nil {
		t.Fatalf("TurnStep (2): %v", err)
	}
	if second.Kind == ActionNone {
		t.Fatalf("an un-admitted turn's terms were treated as in hand: %q/%q", second.Kind, second.Reason)
	}
	if strings.Join(second.NewTerms, " ") != strings.Join(first.NewTerms, " ") {
		t.Errorf("the retry asks a different question: %q vs %q", second.NewTerms, first.NewTerms)
	}
}

// TestTurnQueryReproducesTurnStepsKeyAndDoesNotAdvanceTheMachine is the
// contract TestOneTypedLineIsRetrievedOnceAtTheTurnBoundary relies on: the key
// TurnQuery reports is EXACTLY the key TurnStep would fetch (so a caller may
// compare it against another machine's key to decide "this question is already
// answered"), and asking does not change the machine's memory — a query that
// spent a throttle step or marked the terms satisfied would make the next real
// turn see its own terms as NEW.
func TestTurnQueryReproducesTurnStepsKeyAndDoesNotAdvanceTheMachine(t *testing.T) {
	in := TurnInput{Parts: []string{turnTriggerText}}
	cfg := Config{}

	asked, askedTerms, err := TurnQuery(NewState(), in, cfg)
	if err != nil {
		t.Fatalf("TurnQuery: %v", err)
	}
	acted, err := TurnStep(NewState(), in, cfg)
	if err != nil {
		t.Fatalf("TurnStep: %v", err)
	}
	if asked == "" || acted.Kind == ActionNone {
		t.Fatalf("the fixture turn did not clear the gate (query=%q kind=%q) — the case would prove nothing", asked, acted.Kind)
	}
	if asked != acted.Query {
		t.Fatalf("TurnQuery reported %q but TurnStep fetches %q — a caller comparing the two keys would share the wrong section", asked, acted.Query)
	}
	if strings.Join(askedTerms, deltaSeparator) != acted.Query {
		t.Fatalf("the delta terms %v do not join to the query %q", askedTerms, acted.Query)
	}

	// THE MACHINE IS UNTOUCHED: the same state, asked twice, answers the same,
	// and a fetch admitted afterwards still leaves the terms in hand.
	s := NewState()
	first, _, err := TurnQuery(s, in, cfg)
	if err != nil {
		t.Fatalf("TurnQuery: %v", err)
	}
	second, _, err := TurnQuery(s, in, cfg)
	if err != nil {
		t.Fatalf("TurnQuery (second): %v", err)
	}
	if first != second {
		t.Fatalf("asking twice changed the answer: %q then %q — TurnQuery advanced the machine", first, second)
	}
	if s.Fetched || s.Steps != 0 || len(s.Satisfied) != 0 {
		t.Fatalf("TurnQuery changed the machine's bookkeeping: fetched=%v steps=%d satisfied=%d",
			s.Fetched, s.Steps, len(s.Satisfied))
	}
	// And the turn that really retrieves still admits its terms, so the NEXT
	// turn with the same words is a delta of nothing rather than a second fetch.
	if err := s.Admit(askedTerms); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if again, _, err := TurnQuery(s, in, cfg); err != nil {
		t.Fatalf("TurnQuery (after admit): %v", err)
	} else if again != "" {
		t.Fatalf("a turn whose terms are all in hand still reports the key %q — the delta was bypassed", again)
	}
}
