package main

// chat_answer_ellipsis_attribution_test.go — THE RED EVIDENCE THAT THE OLD
// WORDING WAS A MISATTRIBUTION.
//
// The scenario, spelled out: the MODEL'S TEXT CONTAINS NO U+2026 AT ALL, and the
// operator's rendered copy still carried a cut label — because the answer band's
// right edge cut a row between two word runes and the renderer DREW its label
// there (internal/lineedit answer.go cutLabelFor → writeCutLabel, recorded by
// answer_cut_labels.go). The drawn cells therefore carry a character the wire
// copy does not, at a location the model never wrote.
//
// THE LABEL'S OWN GLYPH MOVED (task 01a1a325: `…` → `↵`, lineedit.AnswerCutLabel).
// The attribution these cases assert is UNCHANGED — the record still says the
// renderer drew a cell the answer's text does not contain, and the report still
// credits it to the display layer instead of the model. What the glyph change also
// buys is that the two copies no longer collide on one spelling: a `…` in the
// drawn band is the MODEL's text (or the status row's, above the band), while `↵`
// can only be the layout's. This file therefore keeps asserting the RECORD's
// meaning, which is the thing a glyph change must not be allowed to weaken.
//
// On the pre-change code this turn produced EITHER nothing at all (the watch only
// ever saw the answer's own text, and that text is clean) OR — in the measured
// session, where both shapes were present — "the body was NOT altered; the prompt
// forbids it", i.e. a verdict on the MODEL for a cell the RENDERER drew. Both are
// wrong; the first is silent and the second accuses.
//
// After the change the report must say, in the words a reader can act on:
//
//   - that the character is in the RENDERED body,
//   - that the model's output does NOT contain it (display-layer difference),
//   - and, when the two copies disagree in a way this seam cannot resolve per
//     character, that it CANNOT attribute.
//
// The last arm is the one that keeps the fix honest: "I cannot tell" is a result,
// "the model violated the prompt" was an assumption (Rule 15).

import (
	"strings"
	"testing"
)

// cutEllipsisProbeBody is a single word-character token with no whitespace and no
// separator, so the band's right edge must cut it mid-token: the one shape that
// earns a cut label without the answer's text containing the glyph.
var cutEllipsisProbeBody = strings.Repeat(cutEllipsisProbeWord, 30)

// cutEllipsisProbeWord is the token the probe body repeats: word characters only,
// so the band must break INSIDE it (the cut-label case) and the word itself
// contains no U+2026.
const cutEllipsisProbeWord = "identifier"

// drawCutEllipsisTurn streams a clean body through the REPL's own turn renderer
// (the reserved/band path, which is the path that draws cut labels) and returns
// the renderer, ready for the report seam.
func drawCutEllipsisTurn(t *testing.T, body string) (*chatTurnRenderer, *streamHarness) {
	t.Helper()
	h := newStreamHarness(t)
	// Wait for the layout to exist: the band is installed by the editor's read
	// loop, so asking for it before the harness is live returns nil.
	h.waitFor(t, "the answer band", func() bool { return h.in.Answer() != nil })
	r := newChatTurnRenderer(h.in)
	if !r.Reserved() {
		t.Fatal("the turn renderer did not take the reserved/band path — the cut label " +
			"is drawn by that path only, so this probe would prove nothing")
	}
	r.BeginTurn(4, "the operator's line")
	// Stream in small fragments: a real delta cadence, so the cut happens inside
	// the stream exactly as it does in a run.
	for i := 0; i < len(body); i += 7 {
		end := i + 7
		if end > len(body) {
			end = len(body)
		}
		r.Write(body[i:end])
	}
	r.Finish()
	// The band is painted by the editor's READ LOOP (the renderer routes its rows
	// to the owner of the region), so the drawn cells appear on a later repaint:
	// the harness waits for them the way every band assertion in this package
	// does. Without the wait the ledger reads a renderer that has painted nothing.
	h.waitFor(t, "the answer body on the band", func() bool {
		h.replay()
		return strings.Count(h.screen.Contents(), cutEllipsisProbeWord) > 1
	})
	return r, h
}

// TestChatRenderedEllipsis_ModelWroteNoneIsADisplayLayerFact_RED is the epic's
// own scenario: a body with no U+2026 in it produces `…` cells on the screen, and
// the report must name the DISPLAY LAYER rather than the model.
func TestChatRenderedEllipsis_ModelWroteNoneIsADisplayLayerFact_RED(t *testing.T) {
	if strings.Contains(cutEllipsisProbeBody, string(chatAnswerEllipsisRune)) {
		t.Fatal("the probe body must not itself carry the character under test")
	}
	r, _ := drawCutEllipsisTurn(t, cutEllipsisProbeBody)

	hits := r.Ellipses()
	if len(hits) != 0 {
		t.Fatalf("the answer's own text carries no U+2026, so the WATCH must record none; "+
			"it recorded %d: %+v", len(hits), hits)
	}
	drawn := r.CutLabels()
	if drawn == 0 {
		t.Fatalf("the band drew no cut label — the probe is too short for the %d-cell band, "+
			"so it proves nothing (the body must overflow the band)", completionCols)
	}

	var report strings.Builder
	if n := reportChatAnswerEllipses(&report, hits, drawn, 4); n == 0 {
		t.Fatalf("a turn whose RENDERED copy carries %d U+2026 cell(s) that the model never "+
			"wrote produced NO report at all — silence is how the operator's own "+
			"investigation started, and the guard's whole job is to prevent it", drawn)
	}
	line := report.String()
	for _, want := range []string{
		chatAnswerEllipsisReportPrefix,
		"display layer",
		"does NOT contain",
		chatAnswerEllipsisSourceRendererWord,
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("the report does not carry %q — a reader cannot tell the display copy "+
				"from the model's own text:\n%s", want, line)
		}
	}
	// THE OLD WORDING MUST NOT COME BACK. "the prompt forbids it" is a verdict on
	// the model; this turn's model written nothing of the sort.
	if strings.Contains(line, chatAnswerEllipsisBannedVerdict) {
		t.Fatalf("the report still carries the old model-violation wording %q:\n%s",
			chatAnswerEllipsisBannedVerdict, line)
	}
}

// TestChatRenderedEllipsis_HitBesideCutLabelsIsUnattributable_PER_CHARACTER is the
// second arm: the answer's text ALSO carries a `…` of its own in the same turn the
// renderer drew labels. This seam cannot say WHICH drawn `…` is the model's — the
// grid rows and the source offsets are different coordinate systems, and the wire
// copy is the transport's record, not this seam's — so the report must say so
// instead of assuming the model's copy is the one on screen.
func TestChatRenderedEllipsis_HitBesideCutLabelsIsUnattributable_PER_CHARACTER(t *testing.T) {
	// A body whose `…` CUTS A PASTED UNIT, so the turn is one the report speaks
	// about at all: since 2026-11-09 a prose-only turn is silent (its whole record
	// is the hits themselves — see reportChatAnswerEllipses's silence criterion),
	// and an attribution assertion needs a line to read it off.
	const body = "the model wrote task/01a16033-dd15-… and nothing else"
	r, _ := drawCutEllipsisTurn(t, cutEllipsisProbeBody)
	// Feed a second, SHORT turn through the same renderer: it carries a genuine
	// model U+2026 and draws no label (it fits the band) — the "model's own text"
	// arm, proven on the same seam.
	r.BeginTurn(5, "second line")
	r.Write(body)
	r.Finish()

	hits := r.Ellipses()
	if len(hits) != 1 {
		t.Fatalf("the second turn's body carries exactly one U+2026; the watch recorded %d: %+v",
			len(hits), hits)
	}

	// (a) A turn with a hit and NO drawn label: the model's own text, stated.
	var clean strings.Builder
	if n := reportChatAnswerEllipses(&clean, hits, 0, 5); n != 1 {
		t.Fatalf("one hit with no cut label wrote %d line(s), want 1:\n%s", n, clean.String())
	}
	if !strings.Contains(clean.String(), chatAnswerEllipsisSourceAnswerWord) {
		t.Fatalf("a hit with no cut label must be reported as the model's own text:\n%s",
			clean.String())
	}

	// (b) The SAME hit beside drawn labels: per-character attribution is not
	// available, and the report must say so AND name the display-layer fact — in
	// ONE aggregated line, because the display-layer note is part of the turn's
	// summary rather than a line of its own (the per-turn budget is what this epic
	// is about; the count still carries the record).
	var mixed strings.Builder
	if n := reportChatAnswerEllipses(&mixed, hits, 3, 5); n < 1 {
		t.Fatalf("a turn with a hit AND 3 drawn labels wrote %d line(s); it must write a "+
			"summary naming the display-layer fact:\n%s", n, mixed.String())
	}
	got := mixed.String()
	for _, want := range []string{
		chatAnswerEllipsisSourceUnattributableWord,
		"display layer",
		"does NOT contain",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the mixed-copy report does not carry %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, chatAnswerEllipsisBannedVerdict) {
		t.Fatalf("the mixed-copy report still accuses the model with %q:\n%s",
			chatAnswerEllipsisBannedVerdict, got)
	}
	// And the per-hit line must NOT claim "model output" while the turn is
	// unattributable — that single word is the misattribution.
	if strings.Contains(got, chatAnswerEllipsisSourceAnswerWord) {
		t.Fatalf("a per-hit line still claims the model's copy while the turn cannot be "+
			"attributed:\n%s", got)
	}
}
