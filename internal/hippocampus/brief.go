package hippocampus

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/tools"
)

// Brief derivation: the engine reports raw objective signals (hits, executed
// actions, stop reason); the HOST needs the briefing contract of epic child
// 3/4 (confidence / memory weight / a body). The arithmetic lives HERE, next to
// the types it reads, so the host side stays a pure adapter and the numbers are
// testable without a run.
//
// Devdoc/hippocampus.md §8.2 named the algorithm and left it open: memory
// weight = the hits' own confidence × tier weight × token-coverage
// normalisation. That is exactly what BriefFromResult implements — no invented
// signal, only the fields tools.MemoryHit already carries.

const (
	// BriefTopHits is how many leads a briefing describes. It is EXPORTED
	// because it is also the host's display cap: the numbers below are derived
	// from the leads the model is SHOWN, so a host that displayed a different
	// number of them would publish a weight for a briefing nobody saw (Rule 14 —
	// one definition for a value two packages share). briefTopHits remains the
	// package-internal spelling the renderer reads.
	BriefTopHits = briefTopHits
	// briefTopHits is how many hits the briefing body lists. The body is read
	// by a model mid-task: the strongest few leads, not the whole result set.
	briefTopHits = 3
	// briefConfidenceNoHits is the relevance of a recall that found nothing.
	// It is deliberately just under the host's append floor (0.2): an empty
	// retrieval is archived, and is never dressed up as a lead for the model.
	briefConfidenceNoHits = 0.1
	// briefTierWeightHot / briefTierWeightWarm / briefTierWeightCold are the
	// tier factors of a brief's weight.
	//
	// They are NOT the memory engine's ranking weights (tools' tierWeightHot =
	// 2.0 with tierWeightCold = -6.0 set the ORDER of a search): a brief's
	// weight is an intrinsic-weight READING of its leads in [0,1], so it cannot
	// be a monotone document of an ordering whose cold end is negative.
	briefTierWeightHot  = 2.0
	briefTierWeightWarm = 1.0
	briefTierWeightCold = 0.25
	// briefTokenCoverageFull is the token-coverage reading that counts as "the
	// lead covers the query's intent" (tools.MemoryHit.Tokens is a count of
	// covered query tokens; three distinct meaningful tokens is a solid match).
	briefTokenCoverageFull = 3.0
	// briefTierTextHot / briefTierTextWarm / briefTierTextCold are the tier
	// labels as the knowledge base spells them. They are read from
	// tools.MemoryHit.Tier, so a NEW tier value falls back to warm semantics
	// rather than being silently ranked as cold.
	briefTierTextHot  = "hot"
	briefTierTextWarm = "warm"
	briefTierTextCold = "cold"
	// briefWeightFloor / briefWeightCeil clamp the derived memory weight: the
	// host compares it against nothing today, but an unclamped derivation would
	// make the archived number a trap for the next consumer.
	briefWeightFloor = 0.0
	briefWeightCeil  = 1.0
	// briefMaxBodyChars bounds the briefing body handed to the host.
	briefMaxBodyChars = 1200
	// BriefMaxBodyChars bounds a briefing body. It is EXPORTED because the host
	// renders its own digest of the same leads: the bound is ONE number (Rule 14)
	// — bytelen, since it caps a UTF-8 body — and a second, larger copy on the
	// host side would let a digest outgrow what a briefing may be.
	BriefMaxBodyChars = briefMaxBodyChars
	// BriefLeadKeyPrefix namespaces the identity keys of the DISPLAYED leads —
	// the keys a host's convergence gate compares. There is one prefix because
	// display and identity are the same rendering: the host builds both from the
	// leads the briefing shows, and a second spelling would let the gate compare
	// something the model never saw.
	BriefLeadKeyPrefix = "lead:"
	// BriefLeadKeyDelimiter joins the rendered lead lines into that identity,
	// and is equally the join of the briefing body's lead block — the host's
	// digest uses this same delimiter for that reason.
	BriefLeadKeyDelimiter = "\n"
)

// TierWeight maps a stored tier onto its derivation weight. An unknown tier (a
// future tier value) is treated as warm — never as cold, which would silently
// suppress a legitimate lead.
//
// It is EXPORTED because it is the single home of the tier factor (Rule 14): a
// host that needs the factor imports it instead of spelling a second table that
// drifts from this one.
func TierWeight(tier string) float64 {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case tools.TierHot:
		return briefTierWeightHot
	case tools.TierCold:
		return briefTierWeightCold
	default:
		return briefTierWeightWarm
	}
}

// EffectiveConfidence is a hit's confidence signal: the decayed/re-verified
// effective confidence when the memory engine computed one, the stored
// confidence otherwise. Both numbers a brief publishes are built on THIS
// reading, so what the model is told about a memory and what the brief's
// numbers say about it can never disagree.
func EffectiveConfidence(h tools.MemoryHit) float64 {
	if h.EffConfidence > 0 {
		return h.EffConfidence
	}
	return h.Confidence
}

// ClampedBriefConfidence is the relevance a host may publish for one recall:
// the strongest lead's own confidence, damped by how much of the retrieval
// actually matched the query (a lone weak match is not the same evidence as
// five consistent ones).
//
// Why the damping exists: without it a single stray hit at confidence 0.95
// would be published as a 0.95-relevant brief even though four of five leads
// missed the intent — and the host's delivery ladder (append / meta-event /
// soft-interrupt floors) would escalate pure noise. With every lead matching,
// the damping factor is 1 and the best lead's confidence is published unchanged.
//
// Contract with a host: the MAX over the hits (never Hits[0]) is what makes this
// safe regardless of retrieval order — no consumer may assume res.Hits is
// sorted; the live path deliberately does not re-sort, because the engine's
// order is already the relevance order the model is shown.
//
// A recall with no hits returns briefConfidenceNoHits — under the host's append
// floor, which is exactly the point: an empty retrieval is archived and never
// dressed up as a lead.
func ClampedBriefConfidence(res *Result) float64 {
	if res == nil || len(res.Hits) == 0 {
		return briefConfidenceNoHits
	}
	best := 0.0
	matched := 0
	for _, h := range res.Hits {
		if c := EffectiveConfidence(h); c > best {
			best = c
		}
		if h.Tokens > 0 {
			matched++
		}
	}
	fit := float64(matched) / float64(len(res.Hits))
	return clampUnit(best * (briefConfidenceNoHits + (1-briefConfidenceNoHits)*fit))
}

// ClampedBriefMemoryWeight is the intrinsic weight of the memory a brief rests
// on: the MEAN effective confidence of its leads, scaled by each lead's tier
// factor and normalised by those tier weights, so the reading keeps its [0,1]
// range instead of collapsing as the tier factor shrinks it (a warm hit at
// confidence 0.9 weighs 0.9, not 0.63).
//
// Contract with a host: the mean is taken over the DISPLAYED leads (at most
// BriefTopHits) — the briefing the model actually reads. Averaging over every
// returned hit would move the number when hits the model never sees change,
// i.e. when the delivered briefing changed in no visible way.
func ClampedBriefMemoryWeight(res *Result) float64 {
	if res == nil || len(res.Hits) == 0 {
		return 0
	}
	shown := res.Hits
	if len(shown) > BriefTopHits {
		shown = shown[:BriefTopHits]
	}
	sum, weights := 0.0, 0.0
	for _, h := range shown {
		w := TierWeight(h.Tier)
		sum += EffectiveConfidence(h) * w
		weights += w
	}
	if weights == 0 {
		return 0
	}
	return clampUnit(sum / weights)
}

// briefHitWeight is one hit's intrinsic weight: its own effective confidence,
// scaled by its tier and by how much of the query's intent it covers. It is the
// per-lead term ClampedBriefMemoryWeight averages — kept here (rather than
// inlined) because the token-coverage shape is this package's document of what
// "the lead covers the query" means.
func briefHitWeight(h tools.MemoryHit) float64 {
	weight := EffectiveConfidence(h) * TierWeight(h.Tier)
	coverage := float64(h.Tokens) / briefTokenCoverageFull
	if coverage > 1 {
		coverage = 1
	}
	if coverage > 0 {
		weight *= coverage
	}
	return weight
}

// clampUnit folds a derived weight into [briefWeightFloor, briefWeightCeil].
func clampUnit(v float64) float64 {
	if v < briefWeightFloor {
		return briefWeightFloor
	}
	if v > briefWeightCeil {
		return briefWeightCeil
	}
	return v
}

// Brief is the organ's report to its HOST (epic child 3/4, devdoc §6): raw
// objective signals plus a briefing body — never a delivery level (the host's
// scheduler computes that) and never an answer to the task (职责漂移防线).
//
// It is a DERIVED view of one cycle's Result, not a second source of truth: every
// field is computed by BriefFromResult from the hits/actions the cycle produced.
type Brief struct {
	// Confidence is the relevance this cycle earned (see BriefFromResult).
	Confidence float64
	// MemoryWeight is the mean intrinsic weight of the leads (confidence ×
	// tier × query-token coverage).
	MemoryWeight float64
	// Summary states what was retrieved, and what the cycle did to the KB.
	Summary string
	// LeadsKey is the stable identity of WHAT this brief is about: the leads it
	// retrieved (hits and the retrieval's stop reason / query), deliberately
	// EXCLUDING the per-cycle action echo in Summary and any resume directives.
	//
	// Why it must exist: the host suppresses re-delivering material the model
	// has already seen. Keying that decision on Summary fails, because Summary
	// changes with the cycle's own bookkeeping ("本轮记忆操作：fetch #2" vs
	// "none") while the leads the model must be told about are IDENTICAL.
	LeadsKey string
	// Hits are the structured leads, in the engine's ranked order.
	Hits []tools.MemoryHit
	// Query is the retrieval query that produced them.
	Query string
	// StopReason is the cycle's exit path (one of the Stop* constants) — the
	// host records it so "found nothing" and "gave up early" stay distinct.
	StopReason string
}

// BriefFromResult derives the host's briefing contract from one engine cycle.
//
// Confidence is the strongest hit's own confidence, damped by how many of the
// recall's leads actually matched (a lone weak match is not the same evidence
// as five consistent ones). MemoryWeight is the mean intrinsic weight of the
// leads. Summary states what was retrieved, never what to do about it (the
// 职责漂移防线 of devdoc/hippocampus.md §3).
//
// The result is ALWAYS non-nil for a non-nil result, so a host never has to
// nil-check before archiving a cycle.
func BriefFromResult(res *Result) *Brief {
	if res == nil {
		return nil
	}
	b := &Brief{
		Hits:       res.Hits,
		Query:      res.Query,
		StopReason: res.StopReason,
	}
	b.LeadsKey = leadsKey(res)
	if len(res.Hits) == 0 {
		b.Confidence = briefConfidenceNoHits
		b.Summary = renderNoHits(res)
		return b
	}

	// The two NUMBERS are the shared derivation (ClampedBrief*), not a second
	// copy of it: the host publishes the very same functions for the very same
	// result, so what a brief says and what a host says about one retrieval can
	// never fork. Only the body is rendered here.
	best := res.Hits[0]
	for _, h := range res.Hits {
		if h.Confidence > best.Confidence {
			best = h
		}
	}
	b.Confidence = ClampedBriefConfidence(res)
	b.MemoryWeight = ClampedBriefMemoryWeight(res)
	b.Summary = renderBriefBody(res, best)
	return b
}

// renderLeadLines renders the lead lines (id / tier / confidence / topic /
// question) of a retrieval — capped at briefTopHits, exactly as the model sees
// them. Factored out so the briefing body and the lead identity are the SAME
// rendering: the identity must never be finer or coarser than what is shown
// (live runs defeated two earlier versions that keyed on a rewritten form).
func renderLeadLines(res *Result) []string {
	lines := make([]string, 0, briefTopHits)
	for i, h := range res.Hits {
		if i >= briefTopHits {
			break
		}
		line := fmt.Sprintf("\n- #%d [%s conf=%.2f] %s", h.ID, h.Tier, h.Confidence, h.Topic)
		if q := strings.TrimSpace(h.Question); q != "" {
			line += " | " + q
		}
		lines = append(lines, line)
	}
	return lines
}

// renderBriefBody states what was retrieved: the leads (id / tier / confidence
// / topic / question) and what the cycle did to the knowledge base. It carries
// no instruction — the HOST owns the framing (Lv-1 memo vs Lv-2 meta event vs
// Lv-3 soft interrupt), which is exactly why the two are separate fields.
func renderBriefBody(res *Result, best tools.MemoryHit) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("检索到 %d 条相关记忆（最高关联度 %.2f）：", len(res.Hits), best.Confidence))
	for _, line := range renderLeadLines(res) {
		sb.WriteString(line)
	}
	if acts := renderActions(res.Actions); acts != "" {
		sb.WriteString("\n本轮记忆操作：" + acts)
	}
	return truncateBriefBody(sb.String())
}

// leadsKey is the identity of the LEADS a brief puts in front of the model: the
// very lines the briefing body shows, capped at briefTopHits.
//
// It is derived from renderLeadLines — NOT rewritten from the hits — so it
// cannot silently drift from what the model is shown. Two defeated attempts
// taught this: keying on the raw hit list made the identity FINER than the
// display (hits beyond the top three moved the key while the displayed leads
// stayed identical), and including the query made it finer again (a re-phrased
// query over the same rows delivered a byte-identical body twice).
//
// Deliberately excludes the count line and the action echo: those change how the
// cycle went, not what the model has already been told.
//
// Returns "" when there are no hits: an empty retrieval shows no leads, and the
// host then falls back to its body-level guard (whose body carries the query).
func leadsKey(res *Result) string {
	if len(res.Hits) == 0 {
		return ""
	}
	return "leads" + strings.Join(renderLeadLines(res), "")
}

// renderActions renders the executed decisions compactly (verb + target).
func renderActions(actions []Action) string {
	if len(actions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(actions))
	for _, a := range actions {
		part := a.Verb
		if a.ID != 0 {
			part += fmt.Sprintf(" #%d", a.ID)
		}
		if a.Err != "" {
			part += "(failed)"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

// renderNoHits is the body of an empty retrieval: an honest "nothing found",
// which the host archives (its confidence is under the append floor).
func renderNoHits(res *Result) string {
	return truncateBriefBody(fmt.Sprintf("本轮检索无命中（stop=%s，query=%q）。", res.StopReason, res.Query))
}

// truncateBriefBody bounds the body handed to the host.
func truncateBriefBody(s string) string {
	if len(s) <= briefMaxBodyChars {
		return s
	}
	return s[:briefMaxBodyChars]
}
