package memoryprefetch

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ErrNilState is returned by Step on a nil receiver — a refusal with a name,
// instead of a nil-map write later (Rule 15).
var ErrNilState = errors.New("memoryprefetch: nil state")

// ErrEmptyTerm is returned when a caller hands this package an empty term to
// admit or skip. It is refused rather than stored: an empty key in the satisfied
// set would make the session look like it had retrieved something.
var ErrEmptyTerm = errors.New("memoryprefetch: empty term")

// runeLenOf is the ONE rune count of a string in this package.
func runeLenOf(s string) int { return utf8.RuneCountInString(s) }

// ActionKind is what the caller should do this turn.
type ActionKind string

const (
	// ActionNone — do nothing; still carries the Decision and the reason.
	ActionNone ActionKind = "none"
	// ActionFetch — run the first retrieval for the delta terms.
	ActionFetch ActionKind = "fetch"
	// ActionRefresh — run a DELTA retrieval: only terms not already in hand.
	ActionRefresh ActionKind = "refresh"
)

// ActionReason is the named why of an Action. Every Action carries one, so a
// silent "nothing happened" is not representable.
type ActionReason string

const (
	// ReasonNoTrigger — the gate said no.
	ReasonNoTrigger ActionReason = "gate-not-triggered"
	// ReasonThrottledTypes — within the throttle window and no new terms.
	ReasonThrottledTypes ActionReason = "throttled-typing-steps"
	// ReasonNoNewTerms — every candidate term is already satisfied or excluded.
	ReasonNoNewTerms ActionReason = "no-new-terms"
	// ReasonFirstRetrieval — the first retrieval of this session of typing.
	ReasonFirstRetrieval ActionReason = "first-retrieval"
	// ReasonNewTerms — the throttle window has passed and the delta terms are
	// worth a retrieval.
	ReasonNewTerms ActionReason = "new-terms-appeared"
)

// Action is the state machine's instruction for this typing step.
type Action struct {
	// Kind is what to do.
	Kind ActionKind
	// Reason is why (always set).
	Reason ActionReason
	// Query is the retrieval query for the delta terms: the NEW terms only —
	// never the whole typed buffer, and never a term already in hand.
	Query string
	// NewTerms is the delta, in candidate order.
	NewTerms []string
	// Decision is the gate's output, carried through unchanged.
	Decision Decision
}

// State is the throttle + delta memory of ONE typing session. It holds only what
// it must: which terms have been retrieved, which the operator dismissed, how
// many typing steps have passed, and whether a retrieval has run yet.
//
// It is not safe for concurrent use, and it does not need to be: it is owned by
// the composer's own path.
type State struct {
	// Satisfied holds the terms a retrieval has already covered: a term in hand
	// is never fetched again.
	Satisfied map[string]bool
	// Excluded holds the terms the operator dismissed (memory feedback). A
	// dismissal is permanent for this session — that is the point of having one.
	Excluded map[string]bool
	// Steps counts typing steps since the last retrieval (the throttle).
	Steps int
	// Fetched records whether any retrieval has run yet.
	Fetched bool
}

// NewState returns an empty session state.
func NewState() *State {
	return &State{Satisfied: map[string]bool{}, Excluded: map[string]bool{}}
}

// Step advances the machine one typing step and says what to do.
//
// The order of the checks is the contract:
//
//  1. The gate decides whether this input is worth anything at all; if not, no
//     bookkeeping is spent on it.
//  2. The DELTA is computed: candidate terms minus satisfied minus excluded. An
//     empty delta means there is nothing to retrieve, whatever the gate said.
//  3. The throttle holds a retrieval back until ThrottleTypes steps have passed.
//     The division is deliberate: the DELTA decides WHETHER there is anything to
//     fetch, the THROTTLE decides WHEN. So a second retrieval never runs sooner
//     than the window, however much the operator keeps typing — while a term
//     already in hand is never fetched again at all.
//
// Step does not mark anything satisfied: the caller marks terms with Admit only
// after a retrieval actually succeeded, so a failed fetch costs a retry rather
// than silently pretending the terms are in hand.
func (s *State) Step(in GateInput, set CandidateSet, cfg Config) (Action, error) {
	if s == nil {
		return Action{}, fmt.Errorf("step prefetch: %w", ErrNilState)
	}
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return Action{}, fmt.Errorf("step prefetch: %w", err)
	}
	if s.Satisfied == nil {
		s.Satisfied = map[string]bool{}
	}
	if s.Excluded == nil {
		s.Excluded = map[string]bool{}
	}
	s.Steps++

	decision := EvaluateGate(in, set, cfg)
	if !decision.Trigger {
		return Action{Kind: ActionNone, Reason: ReasonNoTrigger, Decision: decision}, nil
	}
	delta := s.delta(set.TermsText())
	if len(delta) == 0 {
		return Action{Kind: ActionNone, Reason: ReasonNoNewTerms, Decision: decision}, nil
	}
	query := strings.Join(delta, deltaSeparator)
	if !s.Fetched {
		s.Fetched = true
		s.Steps = 0
		return Action{
			Kind:     ActionFetch,
			Reason:   ReasonFirstRetrieval,
			Query:    query,
			NewTerms: delta,
			Decision: decision,
		}, nil
	}
	if s.Steps < cfg.ThrottleTypes {
		return Action{Kind: ActionNone, Reason: ReasonThrottledTypes, Decision: decision}, nil
	}
	s.Steps = 0
	return Action{
		Kind:     ActionRefresh,
		Reason:   ReasonNewTerms,
		Query:    query,
		NewTerms: delta,
		Decision: decision,
	}, nil
}

// deltaSeparator joins delta terms into a query.
const deltaSeparator = " "

// delta returns the candidate terms not yet satisfied and not excluded, in the
// order the candidate set produced them (deterministic, so a query string is
// reproducible).
func (s *State) delta(terms []string) []string {
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		if s.Satisfied[t] || s.Excluded[t] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// Admit marks terms as retrieved — call it AFTER a retrieval succeeded.
// An empty term is refused: admitting "" would put an empty key in the satisfied
// set and hide the fact that the caller passed nothing (Rule 15).
func (s *State) Admit(terms []string) error {
	if s == nil {
		return fmt.Errorf("admit prefetch terms: %w", ErrNilState)
	}
	if s.Satisfied == nil {
		s.Satisfied = map[string]bool{}
	}
	for _, t := range terms {
		if t == "" {
			return fmt.Errorf("admit prefetch terms: %w", ErrEmptyTerm)
		}
		s.Satisfied[t] = true
	}
	return nil
}

// Skip excludes terms permanently for this session (the operator dismissed the
// hit they produced). Excluded terms never reappear in a delta, so a dismissal
// costs one keystroke instead of one retrieval per turn.
func (s *State) Skip(terms []string) error {
	if s == nil {
		return fmt.Errorf("skip prefetch terms: %w", ErrNilState)
	}
	if s.Excluded == nil {
		s.Excluded = map[string]bool{}
	}
	for _, t := range terms {
		if t == "" {
			return fmt.Errorf("skip prefetch terms: %w", ErrEmptyTerm)
		}
		s.Excluded[t] = true
	}
	return nil
}

// Reset forgets the session: satisfied and excluded terms cleared, throttle
// counters back to zero.
func (s *State) Reset() {
	if s == nil {
		return
	}
	s.Satisfied = map[string]bool{}
	s.Excluded = map[string]bool{}
	s.Steps = 0
	s.Fetched = false
}
