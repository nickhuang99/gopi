package memoryprefetch

import (
	"fmt"
	"strings"
)

// turn.go — the TURN-BOUNDARY half of this package's pure core: what the WHOLE
// input of one turn is worth retrieving for.
//
// WHY THIS IS NOT A SECOND MECHANISM. state.go's machine was written FOR this
// caller and its own header says so: "Step decides whether a TURN is worth a
// retrieval and what its delta terms are (a turn boundary, driven by a composer
// that has the whole buffer), while Session decides whether the OPERATOR has
// paused". Until this file existed, no caller drove Step — the typing path
// (Session, sampled from the line editor's published snapshot) was the only one
// wired, and the four source classes that have NO line editor to sample got
// nothing at all: a `gopi notify --session … --message …` delivery, a whole-block
// paste, a `/queue`d prompt, and a line the model typed for itself through
// `chat_surface type`. On a notice turn there is nothing being typed, so
// Editor.Value().Text() is empty and the typing machine has no input; on a paste
// turn the editor's line holds a REFERENCE (`[paste N: /abs/path]`) while the
// words that matter are in the file. That absence is what this file closes: it
// supplies the missing INPUT (TurnInput) and the named bounds that make
// assembling it cheap. The decisions themselves stay where they already were —
// ExtractCandidates, EvaluateGate, Step, TrimHits, RenderSegment, InjectSection.
//
// WHAT IT DOES NOT DO. It reads no file, opens no ledger, spawns nothing and
// looks at no clock: a turn's referenced payloads arrive here as STRINGS, already
// read by the caller (cmd/agent owns the I/O and the bound on the read). That is
// the same split session.go keeps, and it is what lets this whole path be tested
// without a terminal, a provider or a fixture.

// TurnInput is ONE turn's own words — the parts of the input that carry what the
// turn is ABOUT, and the payloads it refers to.
//
// Parts is deliberately NOT the sealed message: a message the boundary framed
// carries a constant preamble ("[system] An external process sent this session a
// message: ", "[gopi message boundary] What you received is a REFERENCE …") whose
// words would enter the candidate set on EVERY turn of that shape. A constant
// term is worse than a missing one — it would satisfy the gate by itself and
// pollute the delta machine with a keyword that never changes. So the caller
// passes the turn's own content: the operator's line, the notice's verbatim text,
// the command a job ran.
type TurnInput struct {
	// Parts are the turn's own words, in the order a reader would read them.
	Parts []string
	// Payloads are the payloads the turn refers to, already read by the caller
	// and bounded by it at MaxTurnPayloadRunes runes each (TurnText clips again,
	// so the bound holds however the caller read).
	Payloads []string
}

// TurnText assembles the retrieval text of ONE turn.
//
// It is the ONE place the turn's parts are joined, so the text the gate judges,
// the text the extractor reads and the text the record can name are the same
// string. A payload longer than MaxTurnPayloadRunes is CLIPPED and the clip is
// recorded as a named degradation (Rule 15: a loss is stated, never absorbed) —
// a whole-block paste is unbounded by construction, and the retrieval key is not
// a transcript.
func TurnText(in TurnInput, cfg Config) (string, []Degradation) {
	cfg = cfg.withDefaults()
	var b strings.Builder
	var degradations []Degradation
	for _, p := range in.Parts {
		if s := strings.TrimSpace(p); s != "" {
			b.WriteString(s)
			b.WriteString(turnPartSeparator)
		}
	}
	clipped := 0
	for _, p := range in.Payloads {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		if runeLenOf(s) > cfg.MaxTurnPayloadRunes {
			// clipRunes cuts on a rune boundary, so a clipped CJK payload is a
			// shorter string and never a corrupt one (see its own note).
			s = clipRunes(s, cfg.MaxTurnPayloadRunes)
			clipped++
		}
		b.WriteString(s)
		b.WriteString(turnPartSeparator)
	}
	if clipped > 0 {
		degradations = append(degradations, Degradation{
			Code: DegradationTurnPayloadClipped,
			Detail: fmt.Sprintf(
				"%d referenced payload(s) entered the retrieval key clipped to MaxTurnPayloadRunes=%d",
				clipped, cfg.MaxTurnPayloadRunes),
		})
	}
	return strings.TrimSpace(b.String()), degradations
}

// DefaultTurnThrottleTypes is the throttle a TURN-BOUNDARY machine runs with: no
// throttle at all, because the event it counts is not a keystroke.
//
// WHY THE THROTTLE IS 1 HERE AND 6 THERE (and this is a decision, not a default):
// config.DefaultThrottleTypes exists to keep a fast TYPIST from spending a
// retrieval per keystroke — the typing machine is driven by an event stream
// nobody's own code controls. A turn boundary is the opposite: the REPL calls it
// once per sealed turn, which is a rate the OPERATOR sets, and the delta already
// guarantees a retrieval carries only terms not already in hand. A throttle on
// top of that would mean a turn whose lookup FAILED (a give-up, a locked ledger)
// could not be retried for several more turns — a real loss for no protection.
// The delta is the whole gate at a turn boundary.
const DefaultTurnThrottleTypes = 1

// TurnConfig is the configuration a TURN-BOUNDARY machine runs with: the
// caller's own Config, with the throttle replaced by DefaultTurnThrottleTypes.
//
// It exists so the reason above lives in ONE place and is applied by the pure
// package rather than by a caller re-deriving it (Rule 13): a caller that built
// its own Config with ThrottleTypes=1 inline would be a second, undocumented
// statement of the same rule.
func TurnConfig(cfg Config) Config {
	cfg = cfg.withDefaults()
	cfg.ThrottleTypes = DefaultTurnThrottleTypes
	return cfg
}

// TurnStep advances the turn-boundary machine for ONE sealed turn and says what
// to do with it: fetch (the first retrieval for these terms), refresh (a delta
// retrieval), or nothing, with the named reason either way.
//
// It is Step with the turn's INPUT assembled and the turn's THROTTLE applied, and
// nothing else: the gate and the delta are the ones state.go already owns, so the
// turn path and any future caller of Step cannot disagree about WHEN a retrieval
// is worth making.
//
// A turn whose text yields no usable term is NOT an error: it is the machine's
// own ActionNone with a named reason (gate-not-triggered / no-new-terms), which
// the caller records or ignores. An error here means the config or the extractor
// refused the input, which is a defect in the caller rather than a property of the
// turn (Rule 15).
func TurnStep(s *State, in TurnInput, cfg Config) (Action, error) {
	cfg = TurnConfig(cfg)
	text, _ := TurnText(in, cfg)
	set, err := ExtractCandidates(text, cfg)
	if err != nil {
		return Action{}, fmt.Errorf("turn prefetch: %w", err)
	}
	return s.Step(GateInput{Text: text, RuneLen: runeLenOf(text)}, set, cfg)
}

// TurnQuery returns the retrieval key this turn WOULD fetch, and the delta terms
// it would admit — the state machine's own answer about this turn, computed
// WITHOUT advancing it (Step's bookkeeping is untouched: no throttle step is
// spent and nothing is marked satisfied).
//
// WHY IT EXISTS. A line the operator TYPED reaches two callers: the typing
// sampler, which fetches a key built from the published buffer, and the turn
// boundary, which fetches a key built from the sealed input. When they are the
// same key the second lookup answers a question already answered (task
// 01a11601), and the caller needs to know that BEFORE it spends the retrieval.
// It has to ask THIS package, not reconstruct the key beside it: the key is the
// delta the machine computes (candidate terms minus satisfied minus excluded),
// and a caller joining terms itself would be a second, drifting copy of
// state.go's own join and its own delta rule (Rule 13/14).
//
// The caller that uses this for the sharing check still calls TurnStep for the
// turn that must really retrieve, so the decision about WHEN a retrieval is worth
// making stays in exactly one place (state.go) — this function answers "what
// would it be?", never "should it happen?".
func TurnQuery(s *State, in TurnInput, cfg Config) (string, []string, error) {
	cfg = TurnConfig(cfg)
	text, _ := TurnText(in, cfg)
	set, err := ExtractCandidates(text, cfg)
	if err != nil {
		return "", nil, fmt.Errorf("turn query: %w", err)
	}
	if !EvaluateGate(GateInput{Text: text, RuneLen: runeLenOf(text)}, set, cfg).Trigger {
		return "", nil, nil
	}
	delta := s.delta(set.TermsText())
	return strings.Join(delta, deltaSeparator), delta, nil
}

// turnPartSeparator joins a turn's parts. A newline rather than a space keeps the
// extractor's word splitting honest across a part boundary (two parts that end
// and begin mid-identifier must not be glued into a word neither of them contains).
const turnPartSeparator = "\n"
