package lineedit

// answer_cut_label_glyph_test.go — THE MINIMAL REPRO for the misread this epic
// repairs, and the property pin that keeps it repaired.
//
// THE OPERATOR'S SIGHTING, verbatim in shape: a band row ended `only w…` and the
// row below opened `here the TEMPLATE`; another ended `at its B…` above
// `PE price`. They compared that against raw.log, found no difference at all, and
// filed it as "the renderer altered the model's own words" — because `…` means
// "content was omitted here" in every other surface of this product (fitField,
// the request row) and in every editor they use. The cut had omitted NOTHING: it
// had displaced ONE character onto the row below's first column.
//
// So the assertion is not "a marker is drawn" (answer_cut_marker_test.go's family
// already says that). It is the misreading itself, spelled as a shape:
//
//	A LETTER RUNE IMMEDIATELY FOLLOWED BY U+2026 MUST NOT APPEAR ON THE BAND.
//
// That shape is what a reader parses as "text was dropped after this letter". On
// the pre-change tree this test is RED on exactly those rows (measured, this
// epic's red proof), and the second half of the claim fails with it: the
// character the label displaced must be the FIRST character the next band row
// draws, so a reader who reads on loses nothing.

import (
	"strings"
	"testing"
	"unicode"
)

const (
	// cutGlyphProbeRepeat is how many times the word is repeated: just enough that
	// the word runs past the band (7 cells × 20 = 140 > the band's 117), so a row
	// ends BETWEEN two of its characters — one repeated word, no separators, no
	// second variable.
	// The sample is deliberately MINIMAL — one word, one cut shape, the operator's
	// own `only w…` / `here the TEMPLATE` in the smallest form that shows it
	// (Rule 10's minimal-sample habit) — while the GEOMETRY is the family's
	// existing one (cutBandRows, the real renderer at answerCutProbeCols), so the
	// repro and the rest of the cut family cannot drift onto different rigs.
	cutGlyphProbeRepeat = 20
	// cutGlyphProbeWord is a plain lower-case word with no separator in it, so
	// every boundary it crosses is a word boundary and the cut class is the band's
	// right edge (cutLabelFor asks noteBreakInsideWord).
	cutGlyphProbeWord = "answers"
)

// cutGlyphProbeBody is the smallest answer that forces the shape.
var cutGlyphProbeBody = strings.Repeat(cutGlyphProbeWord, cutGlyphProbeRepeat)

// cutGlyphLetterEllipsis reports the first place the band reading shows a LETTER
// followed by U+2026 — the misleading form, returned so the failure message quotes
// what the reader saw rather than describing it.
func cutGlyphLetterEllipsis(rows []string) (string, bool) {
	for i, row := range rows {
		rs := []rune(row)
		for j := 1; j < len(rs); j++ {
			if rs[j] != '…' {
				continue
			}
			if unicode.IsLetter(rs[j-1]) {
				return strings.Join([]string{row, rows[min(i+1, len(rows)-1)]}, " | "), true
			}
		}
	}
	return "", false
}

// TestAnswerCutLabel_NoLetterIsElidedOnScreen is the epic's repro: on a line the
// band must cut inside a word, no band row may end `letter…`, AND the character
// the cut displaced must open the row below.
//
// RED on the pre-change tree (the label was statusEllipsis): the marked rows read
// `...answers…` and the reader is told something was omitted that was not.
func TestAnswerCutLabel_NoLetterIsElidedOnScreen(t *testing.T) {
	rows := cutGlyphBandRows(t)
	body := cutBody(t, rows, cutGlyphProbeBody)

	// (1) THE MISLEADING FORM IS GONE.
	if seen, found := cutGlyphLetterEllipsis(rows); found {
		t.Fatalf("the band ends a row `letter…` — the reader takes U+2026 for an ELISION, and "+
			"nothing was elided (the character continues on the row below). A reader cannot "+
			"tell this from the renderer having altered the answer's own text:\n  %s", seen)
	}

	// (2) AND THE CUT IS STILL MARKED — this is not "delete the label" dressed as a
	// fix. A row that stopped mid-word with no mark at all is the defect the mark
	// was introduced for.
	marked := 0
	for _, r := range body {
		if r.marked {
			marked++
		}
	}
	if marked == 0 {
		t.Fatalf("no band row wears the cut label %q: the probe proves nothing about the "+
			"glyph\n%s", answerCutLabel, describeCutRows(rows))
	}

	// (3) THE DISPLACED CHARACTER LEADS THE NEXT ROW. Walking the body in order
	// consumes the whole answer; at each marked row the walk pauses on the rune the
	// label displaced, and that rune must be the next row's FIRST cell.
	src := []rune(cutGlyphProbeBody)
	at := 0
	for i, row := range body {
		read := []rune(row.text)
		if row.marked {
			read = read[:len(read)-1] // the label's own cell
		}
		for _, r := range read {
			if at >= len(src) || r != src[at] {
				t.Fatalf("band row %d diverges from the answer at cell %d\n%s",
					i, at, describeCutRows(rows))
			}
			at++
		}
		if !row.marked {
			continue
		}
		if at >= len(src) {
			t.Fatalf("band row %d wears the cut label with nothing left to displace — the "+
				"label claims a continuation the band never draws\n%s", i, describeCutRows(rows))
		}
		if i+1 >= len(body) {
			t.Fatalf("band row %d wears the cut label but no row follows it\n%s",
				i, describeCutRows(rows))
		}
		next := []rune(body[i+1].text)
		if len(next) == 0 || next[0] != src[at] {
			t.Fatalf("band row %d displaced %q onto the row below, but that row opens with %q — "+
				"the character the label took the cell of is not the one the reader reads on "+
				"from\n%s", i, string(src[at]), string(next), describeCutRows(rows))
		}
	}
	if at != len(src) {
		t.Fatalf("the band consumed %d of the answer's %d characters — the cut label may not "+
			"stand for text nobody draws\n%s", at, len(src), describeCutRows(rows))
	}
}

// TestAnswerCutLabel_GlyphIsOneCellAndNotTheElision pins the three properties the
// chosen glyph has to have, so a later "prettier" substitution cannot quietly
// break the band's arithmetic or bring the misreading back:
//
//   - ONE DISPLAY CELL: the band's width arithmetic is cell arithmetic, and a label
//     that renders wide would leave every marked row short (or overflow it);
//   - NOT THE STATUS ROW'S ELISION IDIOM: `…` means "dropped", the band means
//     "displaced", and one spelling for both is the misread;
//   - IT SURVIVES A CJK ENVIRONMENT AT ONE CELL: the mark is neutral-width, so it is
//     NOT subject to the ambiguous-width knob that can spend two cells on U+2026.
func TestAnswerCutLabel_GlyphIsOneCellAndNotTheElision(t *testing.T) {
	if w := displayWidth(answerCutLabel); w != 1 {
		t.Fatalf("the band's cut label %q occupies %d cells; every marked row's arithmetic "+
			"assumes exactly 1 (answer_cut_label_glyph.go)", answerCutLabel, w)
	}
	if answerCutLabel == statusEllipsis {
		t.Fatalf("the band's cut label is the status row's elision idiom %q — a reader takes it "+
			"for omitted text, which is the bug this epic repairs", statusEllipsis)
	}
	if answerCutLabel == NoteContinuation {
		t.Fatalf("the band's cut label is the NOTE row's continuation gutter %q — a different "+
			"path with a different width contract", NoteContinuation)
	}
	// The exported spelling must be the drawn one: an outside caller reads the
	// glyph from here rather than typing it (Rule 14).
	if AnswerCutLabel != answerCutLabel {
		t.Fatalf("AnswerCutLabel %q and answerCutLabel %q disagree — two spellings of one "+
			"glyph is how a fold goes stale", AnswerCutLabel, answerCutLabel)
	}
}

// cutGlyphBandRows renders the probe through the REAL renderer (the region, the
// scroll, the cell arithmetic — cutBandRows' rig, shared rather than copied) and
// returns the band's body rows.
func cutGlyphBandRows(t *testing.T) []string {
	t.Helper()
	return cutBandRows(t, cutGlyphProbeBody)
}
