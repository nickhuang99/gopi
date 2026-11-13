package memoryprefetch

// GateInput is what the gate is allowed to look at. RuneLen is the caller's own
// measurement of the typed buffer — the composer owns the reading of that buffer
// (child 3 reads the line editor's published snapshot), and this package does not
// re-derive it. Passing 0 means "count the text", so a caller that only has the
// string is not forced to duplicate the counting.
type GateInput struct {
	Text    string
	RuneLen int
}

// Runes applies the "0 means count it" rule once, here, so no two call sites can
// disagree about it.
func (in GateInput) Runes() int {
	if in.RuneLen > 0 {
		return in.RuneLen
	}
	return runeLenOf(in.Text)
}

// GateReason is the named verdict of the gate. A reason is always produced —
// "did not fire" is never the absence of an answer (Rule 15).
type GateReason string

const (
	// GateReasonTriggered — the input is worth a retrieval.
	GateReasonTriggered GateReason = "triggered"
	// GateReasonInputTooShort — below MinInputRunes: short typing costs nothing.
	GateReasonInputTooShort GateReason = "input-too-short"
	// GateReasonTooFewHits — long enough, but not enough distinct candidate
	// terms to be worth a query.
	GateReasonTooFewHits GateReason = "too-few-hits"
)

// Decision is the gate's output. It carries the numbers it decided on, so the
// caller can log WHY without re-deriving anything.
type Decision struct {
	// Trigger is the verdict.
	Trigger bool
	// Reason is the named verdict, always set.
	Reason GateReason
	// Hits is the primary signal: distinct candidate terms.
	Hits int
	// InputRunes is the secondary signal: the typed length.
	InputRunes int
	// StrongHitCount / WeakHitCount / WeakInputRunes / MinInputRunes are the
	// thresholds this decision was made against — carried so a log line or the
	// rendered section shows the actual comparison, not a re-typed copy of it.
	MinInputRunes  int
	StrongHitCount int
	WeakHitCount   int
	WeakInputRunes int
}

// EvaluateGate decides whether the typed input is worth a retrieval.
//
// The order is the design, not an accident:
//
//  1. A SHORT input never fires, however many candidate terms it yields. While a
//     person is typing the first few characters the machine must do nothing —
//     this is the rule that keeps the feature free.
//  2. Otherwise the PRIMARY signal is the candidate count: StrongHitCount
//     distinct terms is enough on its own.
//  3. Otherwise the SECONDARY signal is length: a long input (a paste, a long
//     question) with WeakHitCount terms is also worth a query — long text with
//     little vocabulary still has a topic.
//
// It is pure and total: no error, no IO, no state.
func EvaluateGate(in GateInput, set CandidateSet, cfg Config) Decision {
	cfg = cfg.withDefaults()
	runes := in.Runes()
	hits := set.HitCount()
	d := Decision{
		Hits:           hits,
		InputRunes:     runes,
		MinInputRunes:  cfg.MinInputRunes,
		StrongHitCount: cfg.StrongHitCount,
		WeakHitCount:   cfg.WeakHitCount,
		WeakInputRunes: cfg.WeakInputRunes,
	}
	if runes < cfg.MinInputRunes {
		d.Reason = GateReasonInputTooShort
		return d
	}
	if hits >= cfg.StrongHitCount {
		d.Trigger = true
		d.Reason = GateReasonTriggered
		return d
	}
	if runes >= cfg.WeakInputRunes && hits >= cfg.WeakHitCount {
		d.Trigger = true
		d.Reason = GateReasonTriggered
		return d
	}
	d.Reason = GateReasonTooFewHits
	return d
}
