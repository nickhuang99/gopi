package main

import (
	"strings"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// The PRODUCER half of the status row's measured-quality emphasis: WHICH spans of
// the metrics column are measurements (the prompt cache's two hit rates — this
// session's cumulative one and this turn's), which TIER each falls in, and the
// style the row draws them with.
//
// WHY THE TIER BOUNDARIES LIVE HERE AND NOT IN internal/lineedit. Whether "4% hit"
// is a number to worry about is a judgement about THIS read-out's economics — the
// tokens the prompt cache served against the tokens it had to write, a pair
// priced ~30x apart on this transport — and it belongs to the code that MEASURED
// the number, not to the renderer. The renderer's half is the hue: it has a table
// per capability (24-bit / 256 / the 8-colour floor / monochrome) and answers
// "which green is `good` here?" without knowing what was measured
// (internal/lineedit's StatusGood / StatusFair / StatusBad; memory #5707: the
// decision comes from the producer, the renderer only looks it up).
//
// WHY THIS FILE COMPOSES NO NUMBER OF ITS OWN. Both readings' texts are composed
// by the row's own composers (statusCacheHitRate / statusCacheTurnHitRate) and
// spelled as spans by the two helpers beside them, which use the SAME glyph and
// suffix constants the row drew them with. A second composition here would be a
// second answer to "what does the hit rate look like", and the emphasis must name
// the cells the ROW drew — so this file COMPOSES NOTHING and only reads the forms
// back.
//
// Rule 1: the thresholds are named constants and nothing here types a bare number
// or an SGR sequence — the only sequences it can reach are the palette's own
// fields, resolved through the table the run decided ONCE.

// The hit rate's tier boundaries, in PERCENT of read/(read+write) — the floors are
// whole percents while the cumulative reading carries a fraction
// (statusHitRateDecimals) and the turn's does not (statusHitRatePctWhole), so the
// comparison is against the printed number rather than a rounded one. They are the
// producer's OWN judgement and are stated as such, not derived from anything:
//
//   - At or above statusHitRateGoodFloorPct the cache is doing its job — the steady
//     state of a long conversation, where every re-sent prefix is served from the
//     cache and only the new turn is written.
//   - At or above statusHitRateFairFloorPct it works, but a visible share of the
//     prompt is written every round — the state a session sits in while its prefix
//     is still growing.
//   - Below that the cache is contributing little: this is the state that spends
//     money on every round, and the row carries the read-out precisely because it
//     must be distinguishable at a glance from the healthy one.
//
// Two ends worth naming, with a middle so the reading is not a boolean — the same
// shape the palette trio was added for (internal/lineedit's StatusGood note). They
// are NOT a claim about the transport's billing thresholds: no price, no contract
// and no API document is consulted here, and nothing in this file depends on one.
//
// BOTH READINGS WEAR THE SAME TIERS, deliberately: the question the hue answers is
// "is this rate healthy?", and that question does not change with the window the
// rate is taken over. The two readings are told apart by their MARKERS (Σ / Δ,
// statusHitRateCumulativeGlyph / statusHitRateDeltaGlyph), never by their colour —
// which is also what keeps the distinction legible on a monochrome terminal
// (internal/lineedit's TestPaletteCarriesNoInformationAlone).
const (
	statusHitRateGoodFloorPct = 80
	statusHitRateFairFloorPct = 50
)

// statusHitRatePctSuffix and statusHitRateReading — the glyph that makes the number
// a PERCENTAGE, and the reader for the form it is written in — live with the
// composer in chat_stream.go: the format is ONE convention with two halves (Rule
// 14), and this file READS it back rather than keeping a second opinion about it.
// BOTH readings come back through that one reader (statusHitRatePctWhole also
// appends the same suffix), so the numbers judged here are the numbers drawn.

// statusHitRateTierStyle resolves a READING's tier and the hue that tier has in
// THIS palette. It is the ONE place the two floors are compared, so the two
// readings cannot grade the same number differently.
//
// ok=false is a reading this field cannot parse, or an empty one: the caller's
// answer is to wear no tier rather than to guess one, because guessing would put
// the wrong hue on a measurement nobody can parse (Rule 15).
func statusHitRateTierStyle(reading string, pal lineedit.Palette) (lineedit.Style, bool) {
	if reading == "" {
		return lineedit.Style{}, false
	}
	pct, ok := statusHitRateReading(reading)
	if !ok {
		return lineedit.Style{}, false
	}
	switch {
	case pct >= float64(statusHitRateGoodFloorPct):
		return pal.StatusGood, true
	case pct >= float64(statusHitRateFairFloorPct):
		return pal.StatusFair, true
	default:
		return pal.StatusBad, true
	}
}

// statusHitRateEmphasis resolves the CUMULATIVE reading's span and its tier: the
// exact text of the session's rate read-out ("Σ74.5%"), wearing the style this
// palette gives that tier.
//
// An unmeasured cache yields ("", zero Style) — the no-emphasis state, which renders
// the column exactly as it did before this channel existed. That is the honest no-op
// rather than a silent failure: the sub-cell composes the empty string too
// (statusCacheHitRate's own unmeasured arm), so there is no measurement to
// emphasise (Rule 15b).
func statusHitRateEmphasis(ops statusOps, pal lineedit.Palette) (span string, style lineedit.Style) {
	span = statusHitRateCumulativeSpan(ops)
	style, ok := statusHitRateTierStyle(statusCacheHitRate(ops), pal)
	if !ok {
		return "", lineedit.Style{}
	}
	return span, style
}

// statusHitRateTurnEmphasis is the SAME resolver for the TURN reading: the span is
// Δ plus this turn's whole percent ("Δ10%"), and the tier comes from the same floors
// — a session at 95% whose current turn was mostly written is a state the reader can
// act on, and one hue for both readings would hide it.
//
// Its empty states are the turn's own (no baseline yet, no traffic this turn —
// statusCacheTurnHitRate carries them), and each lands on the no-emphasis state for
// the same reason: there is no measurement, so there is nothing to colour.
func statusHitRateTurnEmphasis(ops statusOps, pal lineedit.Palette) (span string, style lineedit.Style) {
	span = statusHitRateDeltaSpan(ops)
	style, ok := statusHitRateTierStyle(statusCacheTurnHitRate(ops), pal)
	if !ok {
		return "", lineedit.Style{}
	}
	return span, style
}

// statusHitRateSpans is the span LIST the row's metrics column wears: the cumulative
// reading first, then the turn's, each with its own tier — they are independent
// measurements and can legitimately wear different hues.
//
// EACH SPAN IS CHECKED AGAINST THE COLUMN'S OWN TEXT before it is handed over
// (strings.Contains), and that check is the point rather than a formality: the
// composer drops readings WHOLE when the cells do not fit
// (statusCacheHitRateReadings' ladder), so a span the column does not carry must
// wear nothing — emphasising a span that is not in the text is the silent
// non-effect this channel exists to remove, and guessing at a near-match would
// decorate cells nobody measured (Rule 15).
func statusHitRateSpans(ops statusOps, pal lineedit.Palette, columnText string) []lineedit.StatusEmphasisSpan {
	cumulativeSpan, cumulativeStyle := statusHitRateEmphasis(ops, pal)
	turnSpan, turnStyle := statusHitRateTurnEmphasis(ops, pal)
	var spans []lineedit.StatusEmphasisSpan
	for _, one := range [2]lineedit.StatusEmphasisSpan{
		{Text: cumulativeSpan, Style: cumulativeStyle},
		{Text: turnSpan, Style: turnStyle},
	} {
		if one.Text == "" || one.Style.Empty() || !strings.Contains(columnText, one.Text) {
			continue
		}
		spans = append(spans, one)
	}
	return spans
}

// statusHitRateSpan is the ONE-span spelling of the same decision, for the caller
// that has a single span slot: lineedit.Status carries one LabelEmphasis, which is
// the UNALIGNED label path. It answers with the cumulative reading when the label
// carries it — the reading the row has always emphasised — and with nothing
// otherwise.
//
// The ALIGNED path does NOT come through here: it hands the renderer the whole list
// (statusHitRateSpans), so two readings can wear two tiers.
func statusHitRateSpan(ops statusOps, pal lineedit.Palette, columnText string) (string, lineedit.Style) {
	spanText, style := statusHitRateEmphasis(ops, pal)
	if spanText == "" || !strings.Contains(columnText, spanText) {
		return "", lineedit.Style{}
	}
	return spanText, style
}
