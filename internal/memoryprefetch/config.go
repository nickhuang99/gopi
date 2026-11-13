// Package memoryprefetch is the PURE-LOGIC core of the optional, experimental
// "prefetch memory while the operator types" feature (epic 01a0e481, child 2/3).
//
// WHAT IT IS. While a person types into the chat line editor, the machine may
// already be able to fetch what the person is about to ask for. This package
// holds the decisions that feature needs, and nothing else:
//
//	extract.go  — typed input -> candidate retrieval terms: Latin words, plus CJK
//	              words from the project's ONE segmenter (internal/cjkseg). The
//	              2-rune sliding window survives only as the segmenter's
//	              UNAVAILABLE path, and using it is recorded as a named
//	              degradation — never a silent drop.
//	gate.go     — is this input worth a retrieval at all? Primary signal: how
//	              many candidate terms it yields. Secondary: how long it is.
//	state.go    — throttle + DELTA retrieval: never fetch what is already in
//	              hand, never fetch twice for the same terms
//	budget.go   — trim the retrieved hits into a rune budget, never over it
//	render.go   — render the one RAG section the model may be handed, with a
//	              source annotation and an adjudicability marker per hit
//
// IT IS TOKEN-FREE IN THE ONE SENSE THAT MATTERS HERE. The section render.go
// produces is bounded by runes (Config.MaxSegmentRunes), so the cost of handing
// it to a model is known BEFORE it is handed over — the reason this feature is
// worth trying at all. This package does NOT decide whether to inject anything:
// child 3 owns the switch, and the switch's default is off.
//

// WHY THERE IS NO ENV KNOB (PROJECT.md Rule 21). An env-name constant must have a
// real reader; a documented hook nothing consumes makes the help text lie. This
// package therefore takes an explicit Config and reads no environment at all —
// child 3 resolves whatever channels the operator chose and hands the result in.
package memoryprefetch

import (
	"errors"
	"fmt"
)

// ErrInvalidConfig is the base of every configuration refusal. Callers match it
// with errors.Is, so a caller can distinguish "you configured me wrong" from
// "the input was unusable" (Rule 16: every wrap below keeps the chain).
var ErrInvalidConfig = errors.New("memoryprefetch: invalid config")

// Defaults. Every number this package compares against is one of these (Rule 1):
// named here, referenced everywhere, and never re-typed at a comparison site.
const (
	// DefaultMinInputRunes is the rune floor below which NOTHING fires. Short
	// typing must cost nothing — the primary purpose of the gate.
	DefaultMinInputRunes = 24

	// DefaultMinKeywordRunes is the shortest token the extractor accepts as a
	// candidate (extract.go's minCandidateRunes): shorter fragments are noise in
	// a bag-of-words index. It is exported so a caller's own arithmetic (the
	// typing path, which reports how many keywords a line yielded) is computed
	// from the SAME floor the extractor enforces rather than from a copy of it.
	DefaultMinKeywordRunes = minCandidateRunes

	// DefaultStrongHitCount is the PRIMARY trigger: this many distinct
	// candidate terms in the typed input is a retrieval worth making,
	// regardless of length.
	DefaultStrongHitCount = 3

	// DefaultWeakHitCount is the term count of the secondary (length-gated)
	// path: a long input with this many terms is still worth retrieving.
	DefaultWeakHitCount = 2

	// DefaultWeakInputRunes is the length that opens the secondary path — a
	// paste, a pasted stack trace, a long question.
	DefaultWeakInputRunes = 64

	// DefaultMaxKeywords caps the candidate set. The cap is not silent: a
	// truncation records DegradationKeywordCapReached.
	DefaultMaxKeywords = 8

	// DefaultMaxCJKCandidateRunes caps a whole CJK run promoted to a candidate:
	// longer than this and only the bigrams survive, so one long run cannot
	// become a term that matches nothing.
	DefaultMaxCJKCandidateRunes = 8

	// DefaultMaxSegmentHits caps how many hits the rendered section may carry.
	DefaultMaxSegmentHits = 5

	// DefaultMinInjectHits is the INJECTION FLOOR: the fewest hits a retrieval
	// must have returned for ANY of it to be injected. Below the floor the
	// section is not rendered AT ALL — not the hits, and not the header that
	// would announce them — so a one-hit retrieval costs the prompt zero bytes.
	//
	// WHY A FLOOR AT ALL (the ruling, and the measurement behind it). A single
	// hit is the shape a bag-of-words retrieval produces by coincidence: the
	// query shares one token with one record, and the section then claims a
	// relation the ledger never asserted. The cost is not the bytes — it is the
	// reader: a block headed "1 hits" that rides into the model's own request
	// spends attention on an association that is not there, and it spends it on
	// EVERY turn the line is sealed (measured in the operator's own corpus: of
	// 407 recorded lookups, 77 returned exactly one hit and every one of them
	// was adopted).
	//
	// WHY IT IS A FRAMEWORK-LEVEL POLICY AND NOT A KNOB. The floor is a
	// statement about what a retrieval is WORTH — one hit is not an answer — so
	// a knob for it would be a knob for "inject noise, please". No channel
	// carries it: not the CLI flag, not GOPI_MEMORY_PREFETCH, not settings.json.
	// Config.Validate REFUSES a configuration that would LOWER it, so the one
	// direction a caller of this package can move it (upward) can only make the
	// injection stricter, and the policy "never inject a single hit" is
	// unconditional (Rule 15: a limit that cannot be reached is stated).
	DefaultMinInjectHits = 2

	// DefaultMaxSegmentRunes is the rune budget of the whole rendered section —
	// the token-free bound.
	DefaultMaxSegmentRunes = 1200

	// DefaultMaxHitRunes / DefaultMinHitRunes bound ONE hit's body: a hit is
	// clipped to the former, and a hit that cannot be given at least the latter
	// is dropped (with a reason) rather than delivered in a useless sliver.
	DefaultMaxHitRunes = 240
	DefaultMinHitRunes = 24

	// DefaultThrottleTypes is how many typing steps must pass before a second
	// retrieval fires. The throttle never suppresses the FIRST retrieval, and it
	// only ever delays one: the delta already guarantees a retrieval carries
	// only terms not already in hand.
	DefaultThrottleTypes = 6

	// DefaultMaxTurnPayloadRunes is the rune bound ONE referenced payload may
	// contribute to a turn's retrieval key (turn.go). A whole-block paste or a
	// delivered file is unbounded by construction, so the key must be bounded
	// here: the retrieval key is a bag of words, not a transcript, and a
	// megabyte of pasted log is not a better key than its first screen. The cut
	// is RECORDED as a degradation, so a clipped turn says so rather than
	// looking like a short one.
	DefaultMaxTurnPayloadRunes = 4000
)

// Config is the whole tuning surface. The zero value is legal: every field left
// at zero takes its Default* value (Config.withDefaults), so a caller may set
// only what it cares about and cannot accidentally configure a zero budget.
type Config struct {
	// Gate.
	MinInputRunes  int
	StrongHitCount int
	WeakHitCount   int
	WeakInputRunes int

	// Extraction.
	MaxKeywords          int
	MaxCJKCandidateRunes int

	// Budget / rendering.
	MaxSegmentHits  int
	MaxSegmentRunes int
	MaxHitRunes     int
	MinHitRunes     int
	// MinInjectHits is the injection floor (DefaultMinInjectHits). The zero
	// value takes the default, and Validate REFUSES any value below it, so this
	// field can only ever RAISE the floor — the shipped policy (a retrieval of
	// fewer than 2 hits is injected as nothing) holds for every legal Config,
	// including one a caller built field by field.
	MinInjectHits int

	// Throttle (state machine).
	ThrottleTypes int

	// Turn boundary (turn.go).
	MaxTurnPayloadRunes int
}

// DefaultConfig is the ONE place the shipped defaults are assembled, so a caller
// that wants to start from them and change one field writes
// `cfg := DefaultConfig(); cfg.MaxSegmentRunes = n`.
func DefaultConfig() Config {
	return Config{
		MinInputRunes:        DefaultMinInputRunes,
		StrongHitCount:       DefaultStrongHitCount,
		WeakHitCount:         DefaultWeakHitCount,
		WeakInputRunes:       DefaultWeakInputRunes,
		MaxKeywords:          DefaultMaxKeywords,
		MaxCJKCandidateRunes: DefaultMaxCJKCandidateRunes,
		MaxSegmentHits:       DefaultMaxSegmentHits,
		MaxSegmentRunes:      DefaultMaxSegmentRunes,
		MaxHitRunes:          DefaultMaxHitRunes,
		MinHitRunes:          DefaultMinHitRunes,
		MinInjectHits:        DefaultMinInjectHits,
		ThrottleTypes:        DefaultThrottleTypes,
		MaxTurnPayloadRunes:  DefaultMaxTurnPayloadRunes,
	}
}

// withDefaults fills every zero field from DefaultConfig. It is applied at every
// public entry point, so no exported function can be steered into a zero budget
// by a hand-built Config.
func (c Config) withDefaults() Config {
	def := DefaultConfig()
	if c.MinInputRunes <= 0 {
		c.MinInputRunes = def.MinInputRunes
	}
	if c.StrongHitCount <= 0 {
		c.StrongHitCount = def.StrongHitCount
	}
	if c.WeakHitCount <= 0 {
		c.WeakHitCount = def.WeakHitCount
	}
	if c.WeakInputRunes <= 0 {
		c.WeakInputRunes = def.WeakInputRunes
	}
	if c.MaxKeywords <= 0 {
		c.MaxKeywords = def.MaxKeywords
	}
	if c.MaxCJKCandidateRunes <= 0 {
		c.MaxCJKCandidateRunes = def.MaxCJKCandidateRunes
	}
	if c.MaxSegmentHits <= 0 {
		c.MaxSegmentHits = def.MaxSegmentHits
	}
	if c.MaxSegmentRunes <= 0 {
		c.MaxSegmentRunes = def.MaxSegmentRunes
	}
	if c.MaxHitRunes <= 0 {
		c.MaxHitRunes = def.MaxHitRunes
	}
	if c.MinHitRunes <= 0 {
		c.MinHitRunes = def.MinHitRunes
	}
	// THE FLOOR IS FILLED FROM THE DEFAULT AND THEN CLAMPED UP, in that order,
	// because the two answer different inputs and only the second is a policy:
	// a ZERO field means the caller did not mention it and takes the default like
	// every other unset budget; a NON-ZERO field below the floor is a caller
	// asking to weaken it, and gets the floor instead of its number. One
	// assignment is enough (the default IS the floor), and Config.Validate names
	// the second case out loud for a caller that wants to hear the refusal — this
	// clamp is the reason a caller that skips Validate cannot weaken the policy
	// anyway. The posture matches the rest of this function toward a zero budget.
	if c.MinInjectHits <= 0 {
		c.MinInjectHits = def.MinInjectHits
	}
	if c.MinInjectHits < def.MinInjectHits {
		c.MinInjectHits = def.MinInjectHits
	}
	if c.ThrottleTypes <= 0 {
		c.ThrottleTypes = def.ThrottleTypes
	}
	if c.MaxTurnPayloadRunes <= 0 {
		c.MaxTurnPayloadRunes = def.MaxTurnPayloadRunes
	}
	return c
}

// Validate refuses a configuration this package cannot honour, naming the field
// and the relation it violated (Rule 15: a refusal is stated, never absorbed).
// A caller that never calls it is still safe — every entry point fills defaults
// and validates — it exists so a caller can fail at startup instead of per turn.
func (c Config) Validate() error {
	// THE ONE REFUSAL THAT IS A POLICY, not an internal inconsistency, and it
	// reads the RAW field — before withDefaults, which would have clamped it to
	// the floor and made this check unreachable. The field may RAISE the
	// injection floor; a caller that asks to LOWER it is told so by name
	// (Rule 15) instead of being quietly corrected.
	if c.MinInjectHits != 0 && c.MinInjectHits < DefaultMinInjectHits {
		return fmt.Errorf("%w: MinInjectHits=%d below the framework floor %d (the floor cannot be lowered)",
			ErrInvalidConfig, c.MinInjectHits, DefaultMinInjectHits)
	}
	cfg := c.withDefaults()
	switch {
	case cfg.WeakInputRunes < cfg.MinInputRunes:
		return fmt.Errorf("%w: WeakInputRunes=%d below MinInputRunes=%d",
			ErrInvalidConfig, cfg.WeakInputRunes, cfg.MinInputRunes)
	case cfg.StrongHitCount < cfg.WeakHitCount:
		return fmt.Errorf("%w: StrongHitCount=%d below WeakHitCount=%d",
			ErrInvalidConfig, cfg.StrongHitCount, cfg.WeakHitCount)
	case cfg.MinHitRunes > cfg.MaxHitRunes:
		return fmt.Errorf("%w: MinHitRunes=%d above MaxHitRunes=%d",
			ErrInvalidConfig, cfg.MinHitRunes, cfg.MaxHitRunes)
	case cfg.MaxCJKCandidateRunes < cjkBigramRunes:
		return fmt.Errorf("%w: MaxCJKCandidateRunes=%d below the bigram window %d",
			ErrInvalidConfig, cfg.MaxCJKCandidateRunes, cjkBigramRunes)
	}
	return nil
}
