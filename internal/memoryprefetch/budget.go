package memoryprefetch

import (
	"fmt"
	"strings"
)

// DropReason is the named why of a dropped hit (Rule 15: a hit that does not
// reach the section says why, rather than vanishing).
type DropReason string

const (
	// DropReasonHitCountCap — the section already carries MaxSegmentHits hits.
	DropReasonHitCountCap DropReason = "hit-count-cap"
	// DropReasonBudgetExhausted — the rune budget cannot fit this hit.
	DropReasonBudgetExhausted DropReason = "budget-exhausted"
	// DropReasonHitTooShort — the hit's own body is shorter than
	// MinHitRunes; a sliver is not worth the model's attention.
	DropReasonHitTooShort DropReason = "hit-too-short"
)

// Hit is ONE retrieved memory hit, as data. It is deliberately a local struct
// rather than internal/tools.MemoryHit: this package must not import the tools
// layer (standalone by contract), and the renderer needs only these fields —
// child 3 maps a MemoryHit onto this struct, one field at a time, at the seam.
type Hit struct {
	// ID is the hit's identity IN THE INDEX IT CAME FROM: a distilled_qa record
	// number for a memory hit (#N in the memory ledger), a chat_notes row id for
	// a note hit. The rendered line always names the index beside it (Source
	// below), so #N is an address rather than an ambiguity (Rule 15a: the
	// identity is kept, never stripped and never reinterpreted).
	ID int
	// Topic, Question, Answer come from the record.
	Topic    string
	Question string
	Answer   string
	// Note is the recorded text of a NOTE-index hit (the second retrieval leg,
	// notes.go). It is the SAME field position in the hit body's resolution
	// order — see hitBody — so both legs render through one body function and
	// one rune budget instead of two.
	//
	// An empty Note is the ordinary state of a memory hit; the two legs are told
	// apart by Source, never by which text field happens to be set.
	Note string
	// Tier is the record's hot/warm/cold tier, rendered as the adjudicability
	// signal's input. A note hit leaves it EMPTY and carries its provenance in
	// TierCell instead: the tier ladder is a property of distilled memory, and a
	// note has no tier — inventing one would be a fabricated judgement.
	Tier string
	// TierCell is where a hit's own provenance cell goes when the tier slot is
	// not the reader's answer (the note leg: host / round / session / time /
	// full path). It REPLACES Tier in the rendered line when non-empty, so the
	// line keeps its four-cell shape and a reader still learns what stands
	// behind the hit before reading it. The note leg also carries the machine
	// and the FULL record path here, which is what tells two hosts whose home
	// directories are the same string apart.
	TierCell string
	// Stale is the record's "possibly stale" signal, rendered per hit.
	Stale bool
	// Confidence is the record's confidence in [0,1]; it refines a warm hit
	// between "verified" and "provisional".
	Confidence float64
	// Source NAMES THE INDEX the hit came from — memoryprefetch's memory-leg
	// origin by default, the note index for a note hit. It is REQUIRED, not
	// optional: the annotation makes "which store answered this line" a fact on
	// the line itself, and it is the second half of the epic's requirement that
	// both legs' hits be visible with the index they came from named (Rule 15a
	// in reverse — the field is always present, so nothing has to infer it).
	Source string
}

// Dropped is one hit that did not make it, with the reason.
type Dropped struct {
	Hit    Hit
	Reason DropReason
}

// TrimResult is the budget's output.
type TrimResult struct {
	// Kept are the hits that fit, in input order.
	Kept []Hit
	// Dropped are the hits that did not, each with its reason.
	Dropped []Dropped
	// Runes is the measured rune count of the kept hits' BODIES (the part the
	// budget governs) — never a re-derived estimate.
	Runes int
	// Budget is the budget this trim was measured against, carried so a caller
	// can report the comparison without re-reading its own Config.
	Budget int
	// Degradations are the named losses of the trim itself.
	Degradations []Degradation
}

// bodyRunes is the rune count a hit costs, after the per-hit clip. It is the ONE
// accounting function: TrimHits and the rendered section both call it, so the
// measured total and the enforced total cannot drift apart.
func bodyRunes(h Hit, cfg Config) int {
	return runeLenOf(clipBody(hitBody(h), cfg.MaxHitRunes))
}

// hitBody resolves a hit's body text: Answer, else Question, else Topic, flattened
// to one line. Resolution order in ONE place, so the trim's cost and the rendered
// text can never be computed from different fields.
func hitBody(h Hit) string {
	// Note first, then the memory fields: the two legs never set both, and the
	// order states which one wins if a caller ever sets them together (the note
	// leg, whose text field is the whole reason the hit exists). ONE resolution
	// order for both legs, so the trim's cost and the rendered text can never be
	// computed from different fields.
	for _, field := range []string{h.Note, h.Answer, h.Question, h.Topic} {
		if flat := strings.Join(strings.Fields(field), " "); flat != "" {
			return flat
		}
	}
	return ""
}

// TrimHits selects the prefix of hits that fits the rune budget.
//
// THE GUARANTEE: TrimResult.Runes never exceeds cfg.MaxSegmentRunes, for any
// input and any configuration — including a budget too small to hold even one
// hit, which yields an empty section and a named degradation rather than an
// over-budget section. The guarantee is discharged in one place: a hit is
// appended only if the MEASURED total after appending still fits, so there is no
// arithmetic that can drift from the measurement.
//
// It is pure, and it never silently reorders: hits are considered in input order
// (a caller that wants ranking applies its own ordering first).
func TrimHits(hits []Hit, cfg Config) TrimResult {
	cfg = cfg.withDefaults()
	res := TrimResult{Budget: cfg.MaxSegmentRunes}
	if cfg.MaxSegmentRunes < cfg.MinHitRunes {
		res.Degradations = append(res.Degradations, Degradation{
			Code: DegradationBudgetTooSmall,
			Detail: fmt.Sprintf(
				"MaxSegmentRunes=%d is smaller than MinHitRunes=%d: no hit can be rendered",
				cfg.MaxSegmentRunes, cfg.MinHitRunes),
		})
		res.Dropped = droppedAll(hits, DropReasonBudgetExhausted)
		return res
	}
	for _, h := range hits {
		if len(res.Kept) >= cfg.MaxSegmentHits {
			res.Dropped = append(res.Dropped, Dropped{Hit: h, Reason: DropReasonHitCountCap})
			continue
		}
		cost := bodyRunes(h, cfg)
		if cost < cfg.MinHitRunes {
			res.Dropped = append(res.Dropped, Dropped{Hit: h, Reason: DropReasonHitTooShort})
			continue
		}
		if res.Runes+cost > cfg.MaxSegmentRunes {
			res.Dropped = append(res.Dropped, Dropped{Hit: h, Reason: DropReasonBudgetExhausted})
			continue
		}
		res.Kept = append(res.Kept, h)
		res.Runes += cost
	}
	if len(res.Kept) == 0 && len(hits) > 0 {
		res.Degradations = append(res.Degradations, Degradation{
			Code: DegradationAllHitsDropped,
			Detail: fmt.Sprintf(
				"all %d hit(s) were dropped inside a %d-rune budget; %s",
				len(hits), cfg.MaxSegmentRunes, dropSummary(res.Dropped)),
		})
	}
	return res
}

// droppedAll maps every hit to one drop reason.
func droppedAll(hits []Hit, reason DropReason) []Dropped {
	out := make([]Dropped, 0, len(hits))
	for _, h := range hits {
		out = append(out, Dropped{Hit: h, Reason: reason})
	}
	return out
}

// dropSummary counts the drops by reason, deterministically ordered by first
// appearance. It exists so the degradation detail says WHAT was lost, not just
// that something was.
func dropSummary(dropped []Dropped) string {
	counts := map[DropReason]int{}
	var order []DropReason
	for _, d := range dropped {
		if counts[d.Reason] == 0 {
			order = append(order, d.Reason)
		}
		counts[d.Reason]++
	}
	parts := make([]string, 0, len(order))
	for _, r := range order {
		parts = append(parts, fmt.Sprintf("%s=%d", r, counts[r]))
	}
	return strings.Join(parts, " ")
}

// clipRunes truncates s to at most max runes, on a rune boundary (never inside a
// multi-byte character — a clipped CJK glyph would be a corrupt string, not a
// shorter one).
func clipRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if runeLenOf(s) <= max {
		return s
	}
	runes := make([]rune, 0, max)
	for _, r := range s {
		if len(runes) == max {
			break
		}
		runes = append(runes, r)
	}
	return string(runes)
}

// clipMark is appended to a body that was cut short. Without it a clipped body
// ends mid-word and reads as complete — the reader cannot tell a two-line answer
// from a truncated one (Rule 15: a loss is stated, never absorbed).
const clipMark = "…"

// clipBody truncates a hit body to at most max runes INCLUDING the mark, so the
// mark can never push a body over the budget it was trimmed against.
func clipBody(s string, max int) string {
	if runeLenOf(s) <= max {
		return s
	}
	limit := max - runeLenOf(clipMark)
	if limit < 0 {
		limit = 0
	}
	return clipRunes(s, limit) + clipMark
}

// cellText flattens one hit field into a single line: newlines and runs of
// whitespace collapse to one space, so a rendered hit can never break the
// section's line structure.
func cellText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
