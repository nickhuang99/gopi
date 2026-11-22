package memoryprefetch

import (
	"fmt"
	"strings"
)

// Adjudicability names HOW a hit may be judged by the reader. It is not a score:
// it says which evidence stands behind the hit, so a model can weight it instead
// of treating every injected line as equally true.
type Adjudicability string

const (
	// AdjudicabilityHot — a long-standing, high-confidence lesson: treat as
	// established.
	AdjudicabilityHot Adjudicability = "established"
	// AdjudicabilityVerified — a warm lesson with a meaningful confidence and
	// evidence behind it: usable, still the reader's call.
	AdjudicabilityVerified Adjudicability = "verified"
	// AdjudicabilityProvisional — a warm but weakly-supported lesson: a hint.
	AdjudicabilityProvisional Adjudicability = "provisional"
	// AdjudicabilityLowConfidence — a cold tier hit: a lead, not a fact.
	AdjudicabilityLowConfidence Adjudicability = "low-confidence"
	// AdjudicabilityStale — any tier, but the record is flagged possibly
	// stale: the reader must re-verify before relying on it.
	AdjudicabilityStale Adjudicability = "possibly-stale"
	// AdjudicabilityUnknown — the tier is not one this package knows. It is a
	// stated unknown, never a default "verified" (Rule 15).
	AdjudicabilityUnknown Adjudicability = "unknown-tier"
	// AdjudicabilityRecordedNote — the hit came from the ROUND-LEVEL NOTE index
	// (notes.go) rather than the distilled memory ledger. It is its own value
	// because the two carry different evidence: a distilled lesson is a claim
	// someone re-verified, while a recorded note is this machine's own
	// one-line record of one round. Naming the difference is what lets a reader
	// weight the line instead of treating every injected line as equally true —
	// and it is NOT a tier: a note has no hot/warm/cold, so it is stated as the
	// kind of thing it is rather than given a tier it does not have.
	AdjudicabilityRecordedNote Adjudicability = "recorded-note"
)

// Tier spellings this package compares against. They are spelled here as
// constants of THIS package because it must not import the tools layer (which
// owns the canonical TierHot/TierWarm/TierCold): the value arrives as a string
// and is matched here, and an unrecognised value gets AdjudicabilityUnknown
// rather than a guess.
const (
	// tierHot matches internal/tools.TierHot.
	tierHot = "hot"
	// tierWarm matches internal/tools.TierWarm.
	tierWarm = "warm"
	// tierCold matches internal/tools.TierCold.
	tierCold = "cold"
)

// confidenceFloor is the confidence at or above which a warm hit is rendered as
// verified rather than provisional.
const confidenceFloor = 0.7

// SourceAnnotation names where a hit came from, for the reader. It is a
// STRUCTURED value on the rendered hit as well as a rendered token, so a caller
// (or a test) can assert provenance without parsing the text.
type SourceAnnotation struct {
	// Origin is the retrieval surface the hit came from — the memory ledger.
	Origin string
	// Record is the ledger's record identity, e.g. "#907".
	Record string
}

// String renders the annotation the way the section carries it.
func (a SourceAnnotation) String() string {
	return fmt.Sprintf("%s:%s", a.Origin, a.Record)
}

// RenderedHit is one hit as the section carries it: the text, plus the two
// provenance pieces as data (Rule 15's omitempty trap in reverse — the source
// and the adjudicability are REQUIRED, so they are always present in both the
// struct and the text).
type RenderedHit struct {
	// Hit is the underlying record.
	Hit Hit
	// Source is the annotation handed to the reader.
	Source SourceAnnotation
	// Adjudicability is how the reader may judge it.
	Adjudicability Adjudicability
	// Confidence is the record's confidence, carried for the meta cell.
	Confidence float64
	// Line is the exact text placed in the section.
	Line string
}

// Segment is the rendered RAG section.
type Segment struct {
	// Text is the section itself — what child 3 may hand to the model.
	Text string
	// Hits are the rendered hits, in chosen order.
	Hits []RenderedHit
	// Runes is the rune count of Text (the measured cost).
	Runes int
	// Budget is the rune budget the displayed hit bodies were trimmed to; the
	// total includes the header and the annotations on top of it.
	Budget int
	// Degradations are the named losses: extraction's, the trim's, and the
	// render's own.
	Degradations []Degradation
}

// provenance origin for every hit this package renders. There is exactly one
// today; it is a constant so the day a second origin appears, it appears once.
const memoryLedgerOrigin = "memory"

// sectionHeaderPrefix opens every rendered section. It is a constant (Rule 1b)
// because the header's existence is now ASSERTED — the injection floor's red
// proof is partly "the refused retrieval wrote no header", which has to be
// stated as a matchable string in one place rather than re-typed in the test and
// in the template.
const sectionHeaderPrefix = "memory-prefetch"

// headerTemplate renders the section header. Kept as one constant so the tests
// and the caller match on the same shape — and built FROM the prefix above, so
// the two cannot drift apart.
const headerTemplate = sectionHeaderPrefix + " (experimental, optional; %d hits; %d runes of %d budget)"

// voteDisciplineLine is the section's OWN statement of the vote that keeps it
// alive, rendered immediately under the header of every NON-EMPTY section.
//
// WHY IT LIVES HERE AND NOT IN THE SYSTEM PROMPT (measured, 2026-11-22). The
// task kernel is 4485 chars against a 4500 budget — 15 chars of headroom — so a
// clause in toolRoutingRules would have to be paid for by compressing an existing
// rule, and the density gate is the prompt's own guard against exactly that
// trade. The section is where the reader meets the hit lines, its bytes are
// bounded by the same Config that bounds the hits, and it is paid for ONLY on the
// turns that actually injected something — which is the shape of the defect it
// addresses: measured over 162 injected pairs in 60 sessions, 14 pairs ever got a
// vote on a record they returned and only 7 got a useful one, because nothing at
// the injection point said a vote was what kept the section on the wire.
//
// It is a claim about the MECHANISM the slim already implements
// (agent.whitelistContextSlim): a pair whose returned records were voted useful
// survives, and every other one is dropped on the next round. The wording states
// the consequence, not a request (Rule 15 — the reader is told what happens, not
// asked to be good).
const voteDisciplineLine = "# vote: these are prefetched hits — memory(action=feedback, id=#N, rating=useful) on a record you used; without a useful vote this pair is cut next round"

// RenderSegment renders the hits into the ONE section the model may be handed.
//
// Every rendered hit carries, on its own line, its SOURCE annotation and its
// ADJUDICABILITY marker (the acceptance criterion), and the section ends with the
// named degradations when there are any — a degraded run says so where the reader
// can see it, not only in a log.
//
// The budget contract is split deliberately: the HIT BODIES are trimmed to
// cfg.MaxSegmentRunes by TrimHits (so TrimResult.Runes is a bound that holds by
// itself and is tested on its own), and the segment total then adds a header, one
// line per hit and the degradation lines. Segment.Runes reports that total, so a
// caller can see it rather than assume it. The prompt-level budget is child 3's
// to enforce; this package's promise is that its own content is bounded and
// measured.
func RenderSegment(hits []Hit, extraDegradations []Degradation, cfg Config) Segment {
	cfg = cfg.withDefaults()
	trim := TrimHits(hits, cfg)
	seg := Segment{
		Budget:       trim.Budget,
		Degradations: append(append([]Degradation{}, extraDegradations...), trim.Degradations...),
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(headerTemplate, len(trim.Kept), trim.Runes, trim.Budget))
	for _, h := range trim.Kept {
		rh := renderHit(h, cfg)
		seg.Hits = append(seg.Hits, rh)
		b.WriteString("\n")
		b.WriteString(rh.Line)
	}
	// The vote discipline rides with the hits, and ONLY with them: a section that
	// kept no hit is never injected (InjectSection refuses it), so a header-only
	// segment must stay a header only — the empty-segment shape is asserted
	// elsewhere and a stray line there would be a claim about nothing.
	if len(trim.Kept) > 0 {
		b.WriteString("\n")
		b.WriteString(voteDisciplineLine)
	}
	for _, d := range seg.Degradations {
		b.WriteString("\n")
		b.WriteString(renderDegradation(d))
	}
	seg.Text = b.String()
	seg.Runes = runeLenOf(seg.Text)
	return seg
}

// renderHit renders one hit: source annotation, adjudicability marker, then the
// body, in that order — a reader can judge provenance before reading content.
//
// THE FOURTH CELL IS THE INDEX'S OWN PROVENANCE SLOT. For a memory hit it is the
// tier (plus the stale flag); for a hit from the note index (notes.go) it is the
// machine, the round, the session, the time and the FULL path of the record file
// — because a note has no tier, and its path is the fact that tells two hosts
// whose home directories are the same string apart. TierCell is that slot
// whenever the leg filled it; the line's shape does not change with the leg.
func renderHit(h Hit, cfg Config) RenderedHit {
	body := clipBody(hitBody(h), cfg.MaxHitRunes)
	src := SourceAnnotation{Origin: h.sourceOrigin(), Record: NoteRecordPrefix(h.ID)}
	adj := h.adjudicability()
	slot := h.TierCell
	if slot == "" {
		staleMark := ""
		if h.Stale {
			staleMark = " stale=yes"
		}
		slot = h.Tier + staleMark
	}
	line := fmt.Sprintf("[%s|%s|%s] %s",
		src.String(), adj, slot, body)
	return RenderedHit{
		Hit:            h,
		Source:         src,
		Adjudicability: adj,
		Confidence:     h.Confidence,
		Line:           line,
	}
}

// sourceOrigin is the index this hit came from: the leg's own origin when it
// named one, the memory ledger otherwise. A hit with no stated origin is a
// memory hit because that was the ONLY leg this package knew before the note
// index arrived — and every caller that builds a hit from the memory ledger
// leaves the field at its zero value, which is why the default lives HERE rather
// than in each of them.
func (h Hit) sourceOrigin() string {
	if o := strings.TrimSpace(h.Source); o != "" {
		return o
	}
	return memoryLedgerOrigin
}

// adjudicability is how a reader may judge THIS hit. A hit carrying its own
// provenance cell instead of a tier (the note leg) is a RECORDED NOTE — a fact
// of a different kind from a distilled lesson, with its own word for it — so it
// is stated rather than passed off as a memory hit of an unknown tier.
func (h Hit) adjudicability() Adjudicability {
	if h.Source != "" && h.Source != memoryLedgerOrigin {
		return AdjudicabilityRecordedNote
	}
	return AdjudicabilityWithConfidence(h.Tier, h.Stale, h.Confidence)
}

// InjectFloor is the CONFIGURED injection floor: the fewest hits a retrieval
// must have returned for InjectSection to append anything at all (Rule 14 —
// the value has one home, and this is its reader rather than a second
// definition). It applies the same defaults-and-clamp every entry point of this
// package applies, so a caller that has to DESCRIBE why it injected nothing (the
// chat executor records the reason) asks the same number that decided it and
// cannot drift from it.
func InjectFloor(cfg Config) int { return cfg.withDefaults().MinInjectHits }

// InjectSection appends the rendered section to an OUTBOUND message, or reports
// that there is nothing to append.
//
// IT IS THE ONE PLACE A PREFETCHED BLOCK ENTERS A MODEL REQUEST, and the reason
// it lives here rather than in the executor is Rule 31: a claim about the
// injected form ("it never touches what the operator is shown", "an empty
// result appends nothing at all, so the message stays byte-identical") has to be
// made against the REAL function, and this package is where that function can be
// driven without a ledger, a goroutine or a terminal. The executor's own
// adoption point calls it and then hands the result to its outbound boundary,
// which carries Display separately — a background retrieval cannot edit the
// operator's own line on screen.
//
// THE INJECTION FLOOR (Config.MinInjectHits, DefaultMinInjectHits=2) is judged
// HERE, on the COUNT OF HITS THE LOOKUP RETURNED — before anything is rendered,
// so a retrieval below the floor produces no text at all: not the hits, and not
// the header that would announce them. That is the difference this floor is
// made of. Rendering first and suppressing afterwards would still have written
// the header line, and a header IS injection: it costs the prompt bytes and
// tells the reader that a retrieval happened, for a retrieval whose single hit
// is the coincidence this floor exists to keep out. So the check is on
// `len(hits)`, the count the caller's own query returned — not on len(seg.Hits),
// which is what SURVIVED the rune trim and would let a 3-hit result the budget
// emptied read as "below the floor". Those are two different refusals and the
// executor names them with two different outcomes (Rule 15).
//
// The second condition is the older one: the section is a `Segment`, its hits
// were already trimmed against the rune budget, and a segment that kept NO hit
// is not a section — it is a header and a degradation list, and injecting that
// would cost the model tokens to read a statement about nothing. So it is
// seg.Hits, never len(seg.Text).
//
// extraDegradations are losses the CALLER knows about and the renderer does not —
// today the second retrieval leg's (a note index that could not be read at all,
// an echo it suppressed). They are passed through to the render rather than
// appended to its output, so the section stays the renderer's to shape and a
// caller cannot inject free text into a block the model reads.
func InjectSection(out string, hits []Hit, cfg Config, extraDegradations ...Degradation) (string, bool) {
	cfg = cfg.withDefaults()
	if len(hits) < cfg.MinInjectHits {
		return out, false
	}
	seg := RenderSegment(hits, extraDegradations, cfg)
	if len(seg.Hits) == 0 {
		return out, false
	}
	return out + sectionSeparator + seg.Text, true
}

// sectionSeparator joins the operator's own message to the injected section: a
// blank line, so the model reads the section as a distinct block and not as a
// continuation of the request.
const sectionSeparator = "\n\n"

// renderDegradation renders one named loss in the section.
func renderDegradation(d Degradation) string {
	return fmt.Sprintf("# degradation: %s — %s", d.Code, d.Detail)
}

// AdjudicabilityOf classifies a hit's evidence. A stale flag outranks the tier
// (a stale "established" is still stale), an unknown tier is stated as unknown,
// and a warm hit's confidence decides between verified and provisional.
func AdjudicabilityOf(tier string, stale bool) Adjudicability {
	if stale {
		return AdjudicabilityStale
	}
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case tierHot:
		return AdjudicabilityHot
	case tierWarm:
		return AdjudicabilityVerified
	case tierCold:
		return AdjudicabilityLowConfidence
	}
	return AdjudicabilityUnknown
}

// AdjudicabilityWithConfidence refines the warm case with the record's
// confidence: below confidenceFloor a warm hit is provisional, not verified. It
// is the function the renderer uses when a confidence is available.
func AdjudicabilityWithConfidence(tier string, stale bool, confidence float64) Adjudicability {
	base := AdjudicabilityOf(tier, stale)
	if base == AdjudicabilityVerified && confidence < confidenceFloor {
		return AdjudicabilityProvisional
	}
	return base
}
