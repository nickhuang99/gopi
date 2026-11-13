package lineedit

import (
	"strings"
	"testing"
	"time"
)

// answer_cut_marker_test.go — the CUT path's vocabulary: a body row whose cell
// run ends INSIDE a word says so.
//
// The defect this closes (operator's bug #6, re-verified on master 2044ae98 at a
// REAL pty 117x26): where a ▌ row WRAPS, the continuation row opens at the
// marker's own column and nothing is lost — that half is fixed and is asserted by
// marker_column_narrow_test.go. Where the SAME line is CUT at the composer's
// right edge with the break landing between two word characters, the row simply
// STOPS: no `…`, no continuation glyph, nothing that tells the reader whether the
// terminal is narrow or the row is corrupt. Measured on the operator's own screen
// (`/tmp/error.txt`, 117 cells), on the tool row and on the answer row alike:
//
//	⚙ call bash cd ~/work/GoPi; echo "=== branch ==="; … echo "=== status ==="; git status -sb | head -20
//	▌ `git rev-list --count master..fix/pushback-rule31-evidence` = **0**，`merge-base --is-ancestor` 返回 YES。它领先 ma
//
// — the row ends mid-identifier and says nothing. (`fix/pushback-rule31-evid` |
// `ence` is the same shape one row further up.)
//
// THE CONTRACT, at two levels:
//
//	ROW level — when a row's cells end between two narrow word runes (an
//	identifier, a path, a shell word: cutBreakInsideWord), the row ENDS WITH
//	answerCutLabel, the band's own cut marker, and the characters it displaced
//	continue on the row below. Never a silent stop.
//
//	STREAM level — the band still shows EXACTLY the cells the answer has: a marked
//	row trades the one cell the marker occupies for the marker, and no cell of the
//	answer is ever absent. A marker-plus-dropped-tail "fix" is exactly what this
//	half catches.
//
// Why both: a row can wear the marker AND have lost its tail, and the cells can
// all be present while a row stops mid-word and says nothing (that is master).
// A marker without the text, and text without a marker, are each half the claim.

// The defect this file was written against is CLOSED: the cut vocabulary is
// implemented (writeRow's label path in answer.go, `…` in the row's last cell and
// the displaced character leading the row below), and the constant below is the
// switch that used to gate the assertions while it was open. It is kept as a
// NAMED SEAM rather than deleted, because the two halves of the claim (a marked
// row, and a band that still holds every cell) are only meaningful together: the
// value is false — the guard is live, nothing is skipped — and flipping it back to
// true reintroduces the skip, which is what makes this constant the one place that
// decision lives (Rule 1: no bare literal, and no silent skip).
//
// History, so the seam is not mistaken for dead weight: the first attempt drew the
// marker INTO the row's source bytes, which moved every byte offset the emphasis
// path depends on (the `row.skip` span walk, the committed-row comparisons, the
// consumed break space) and broke TestEmphasisWrappedLineKeepsTheBoldRunContiguous
// and the A5B wrap guard's family. The landed fix keeps the marker OUT of the
// source (it is a rendering glyph in a cell, never a character of the answer) and
// reconciles the two arithmetics on the SOURCE — see the byte/cell reconciliation
// in devdoc/answer-row-cut-vocabulary.md, which carries the measured evidence both
// ways.
const cutRowMarkerOpenDefect = false

// cutReadingFold removes the cut label from a band reading, for a test that
// compares the band against the answer's own TEXT.
//
// The label is a RENDERING glyph, not a character of the answer: it stands in the
// cell of a character that was displaced onto the row below, so a reading that
// keeps it is one character longer than the source. Every reader that treats the
// band as source text folds it away, exactly as it already folds the emphasis
// MARKERS the paint consumed — the grid is read for the answer, and the label is
// the band talking about its own geometry. A test that asserts on the MARK (the
// cases in this file) reads the grid raw, which is why the fold lives in the
// READERS rather than in the renderer.
//
// See devdoc/answer-row-cut-vocabulary.md, "the byte/cell reconciliation".
func cutReadingFold(s string) string { return strings.ReplaceAll(s, answerCutLabel, "") }

const (
	// answerCutProbeCols is the band width the cases run at: the composer's own
	// width is what decides where a row ends, and 117 is the geometry the
	// operator reported (`terminal=117x26`).
	answerCutProbeCols = 117
	answerCutProbeRows = 26
	// cutProbeBurst is the delta size the answer is streamed in — a real token
	// stream's order of magnitude, and deliberately not a divisor of the band
	// width, so a cut can never be an artefact of a burst boundary.
	cutProbeBurst = 7
	// cutProbePrompt is the turn's prompt: its label is not the subject, but the
	// turn's header must be findable so the body can be told from the chrome.
	cutProbePrompt = "cut probe"
)

// cutProbeLine is the answer's line in the operator's own shape: a long run of
// NARROW word characters (an identifier with separators, like a shell command or
// a branch name), so the break at the band's right edge falls between two of them
// whatever the width is. It is deliberately ASCII: that is what makes the cut
// land mid-word on EVERY row, which is the case master leaves unmarked.
const cutProbeLine = "cut_row_marker_probe_identifier_segment/alpha-beta_gamma.delta:" +
	"epsilon_zeta_eta_theta/iota-kappa_lambda.mu:nu_xi_omicron_pi/rho-sigma_tau." +
	"upsilon_phi_chi_psi/omega-omega2_omega3.omega4:omega5_omega6_omega7"

// cutProbeWideToken is ONE token wider than the whole band: a run of narrow
// letters with no separator at all. It is the only cut the disposition
// `bandCut` cannot repair — there is no wider row to move the token to — so it is
// the case the band's cut label (`answerCutLabel`) is now about. A line of SEPARABLE
// tokens (`cutProbeLine`) no longer earns the label: the token moves down whole
// (band_word_move.go), which is the disposition's own point.
var cutProbeWideToken = strings.Repeat("wideunbreakabletoken", answerCutProbeCols/18+2)

// cutProbeFits is a line that ends well before the band does: the control case.
// A row with room to spare must never wear the marker — the marker means "there
// is more", and a complete row that wears it lies in the other direction.
const cutProbeFits = "this line fits the band with room to spare, so it is whole."

// cutBandRows renders one answer line through the REAL renderer (the region, the
// scroll, the cell arithmetic — the components `gopi chat` drives) and returns
// the band rows it produced, right-trimmed, with the turn's own chrome (header,
// closing rule) excluded: they are not the answer's body.
//
// The shape is emphasis_cut_test.go's (newRegion + NewAnswerRenderer + a Screen
// fed the renderer's own bytes), so the assertion is on the CELL GRID rather than
// on a string the renderer handed back: a row that was painted and then
// overwritten cannot pass for a row that was correct.
func cutBandRows(t *testing.T, line string) []string {
	t.Helper()
	if cutRowMarkerOpenDefect {
		t.Skip("answer-row cut vocabulary is not implemented in this tree — see cutRowMarkerOpenDefect and devdoc/answer-row-cut-vocabulary.md (defect OPEN, test verified RED on master)")
	}
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(answerCutProbeRows, answerCutProbeCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", answerCutProbeRows, answerCutProbeCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	r.SetClock(clock.Now)
	r.SetPalette(BasicPalette)
	r.BeginTurn(TurnInfo{Index: 1, Prompt: cutProbePrompt})
	for _, b := range chunkByRunes(line, cutProbeBurst) {
		r.Delta(b)
	}
	r.Delta(terminalNewline)
	r.Tick()
	r.Finish()
	r.EndTurn()

	sc := NewScreen(answerCutProbeCols, answerCutProbeRows)
	sc.Feed([]byte(cap.bytes()))
	lines := make([]string, 0, answerCutProbeRows)
	for i := 0; i < answerCutProbeRows; i++ {
		lines = append(lines, strings.TrimRight(sc.Line(i), " "))
	}
	header := rowOf(lines, turnHeaderText(1, cutProbePrompt))
	if header < 0 {
		t.Fatalf("no turn header on the grid\n--- band ---\n%s", strings.Join(lines, "\n"))
	}
	body := make([]string, 0, 4)
	for i := skipRequestRows(lines, header+1, cutProbePrompt, answerCutProbeCols); i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == strings.Repeat(TurnRuleGlyph, answerCutProbeCols) {
			break
		}
		body = append(body, lines[i])
	}
	if len(body) == 0 {
		t.Fatalf("the answer produced no band rows\n--- band ---\n%s", strings.Join(lines, "\n"))
	}
	return body
}

// cutBodyRows is the body of the band with the turn's marker column removed: the
// rows the cut claim is about, each with its own reading.
type cutBodyRow struct {
	text   string // the row's cells, gutter stripped
	marked bool   // the row ends with the product's cut marker
}

// cutBody strips the block's marker column from each band row and reports whether
// the row wears the cut marker. The gutter is the SAME width on every body row —
// the marker glyph on the turn's first line, blank cells on a continuation (see
// lineGutter) — so the strip is uniform and cannot hide a ragged column.
func cutBody(t *testing.T, rows []string, line string) []cutBodyRow {
	t.Helper()
	g := displayWidth(TurnMarker)
	out := make([]cutBodyRow, 0, len(rows))
	for i, row := range rows {
		if displayWidth(row) <= g {
			t.Fatalf("row %d is inside the marker column: %q", i, row)
		}
		rs := []rune(row)
		text := string(rs[leadingCells(rs, g):])
		// A row is marked when it ends with the cut marker. The row's own text
		// may END with that glyph legitimately (a line about the marker itself),
		// which no case here streams, so containment is exact.
		out = append(out, cutBodyRow{text: text, marked: strings.HasSuffix(text, answerCutLabel)})
	}
	if len(out) == 0 {
		t.Fatalf("no body rows for %q", line)
	}
	return out
}

// TestAnswerCut_MidWordBreakIsMarked is the label's own case, under the NARROWED
// meaning the whole-word disposition gave it: a row wears the band's cut label
// only when the token it stops inside is WIDER THAN THE WHOLE ROW.
//
// The criterion was "the break landed between two word characters", and that
// class is now repaired rather than labelled: `bandCut` moves such a token down
// whole (band_word_move.go), so the row ends where the reader's own token does and
// owes no mark. What is left is the cut with no wider row to move it to, and the
// label says exactly that.
func TestAnswerCut_MidWordBreakIsMarked(t *testing.T) {
	rows := cutBandRows(t, cutProbeWideToken)
	body := cutBody(t, rows, cutProbeWideToken)
	// The token is longer than the band on purpose: if it were not, there would be
	// no cut to mark, and the test would prove nothing.
	if len(body) < 2 {
		t.Fatalf("the probe token occupies %d band row(s) — it must overflow the band for a cut "+
			"to exist\n%s", len(body), describeCutRows(rows))
	}
	for i, row := range body {
		if i == len(body)-1 {
			break
		}
		if !row.marked {
			t.Errorf("row %d stops inside a token WIDER than the band and wears no marker — the "+
				"reader cannot tell a corrupt row from a narrow terminal\n%s", i, describeCutRows(rows))
			continue
		}
		if w := displayWidth(rows[i]); w < answerCutProbeCols {
			t.Errorf("row %d wears %q but is only %d of %d cells: the marker means the row ran out "+
				"of room, not that the line ended\n  row=%q", i, answerCutLabel, w, answerCutProbeCols, rows[i])
		}
	}
	// THE OTHER HALF OF THE NARROWED MEANING: a line whose tokens the band CAN
	// move whole wears no mark at all, even though its cell edge lands inside a
	// token. Without this arm the change is untested in the direction the operator
	// complained about (rows that do not run).
	sep := cutBandRows(t, cutProbeLine)
	for i, row := range cutBody(t, sep, cutProbeLine) {
		if row.marked {
			t.Errorf("row %d of the separable-token line wears %q although its token was moved down "+
				"whole — the mark now means something else\n%s", i, answerCutLabel, describeCutRows(sep))
		}
	}
	var joined strings.Builder
	for _, row := range cutBody(t, sep, cutProbeLine) {
		joined.WriteString(row.text)
	}
	if got := joined.String(); got != cutProbeLine {
		t.Errorf("the separable-token rows do not reassemble to the line:\n  got  %q\n  want %q",
			got, cutProbeLine)
	}
}

// TestAnswerCut_MarkerIsTheCutLabel pins the marker's IDENTITY: the cut row wears
// the band's own cut label (answerCutLabel), not a glyph invented for this path
// — and not the status row's elision  idiom either. Two vocabularies for one meaning is the defect the operator reported
// beside the missing marker (their screen prints `…` on the rows that were cut by
// budget while the answer rows print nothing).
func TestAnswerCut_MarkerIsTheCutLabel(t *testing.T) {
	rows := cutBandRows(t, cutProbeWideToken)
	body := cutBody(t, rows, cutProbeWideToken)
	marked := 0
	for _, row := range body {
		if row.marked {
			marked++
		}
	}
	if marked == 0 {
		t.Fatalf("no row wears the cut label %q\n%s", answerCutLabel, describeCutRows(rows))
	}
	// AND IT IS NOT THE STATUS ROW'S ELISION IDIOM. `…` means "text was
	// DROPPED here" everywhere else in this product (fitField, the request row,
	// every editor the operator uses); the band's cut DROPS nothing — it displaces
	// one character onto the row below. Spelling the two the same is the misread
	// this epic repairs, so the separation is asserted rather than assumed.
	if answerCutLabel == statusEllipsis {
		t.Fatalf("the band's cut label is %q — the status row's elision idiom; the band's cut "+
			"displaces a character instead of dropping one, and a reader takes that glyph for "+
			"omission (answer_cut_label_glyph.go)", answerCutLabel)
	}
}

// TestAnswerCut_FittingLineIsNeverMarked is the ORDER half, at the band level: a
// line that fits the band is whole, so no row of it wears the marker.
//
// A rule that marks every row is as useless as one that marks none: the marker
// stops meaning "cut here" the moment it appears on rows that were not.
func TestAnswerCut_FittingLineIsNeverMarked(t *testing.T) {
	rows := cutBandRows(t, cutProbeFits)
	for i, row := range cutBody(t, rows, cutProbeFits) {
		if row.marked {
			t.Errorf("row %d wears %q although its line fits the band\n  row=%q", i, answerCutLabel, rows[i])
		}
	}
}

// TestAnswerCut_NoCellOfTheAnswerIsLost is the STREAM-level claim, and the one
// that catches a "fix" which wears the marker while dropping the tail: walking the
// band's cells in order must still consume the WHOLE answer, with exactly one
// character displaced per marked row (the cell the marker occupies).
//
// A marker that stands for text nobody ever draws is not a cut label, it is a
// euphemism.
func TestAnswerCut_NoCellOfTheAnswerIsLost(t *testing.T) {
	rows := cutBandRows(t, cutProbeWideToken)
	body := cutBody(t, rows, cutProbeWideToken)
	src := []rune(cutProbeWideToken)
	at := 0
	displaced := 0
	for i, row := range body {
		read := []rune(row.text)
		if row.marked {
			read = read[:len(read)-1] // the marker occupies one cell
		}
		for _, r := range read {
			if at >= len(src) {
				t.Fatalf("row %d reads past the end of the answer\n%s", i, describeCutRows(rows))
			}
			if r != src[at] {
				t.Fatalf("row %d diverges from the answer at cell %d: band %q vs answer %q\n%s",
					i, at, string(r), string(src[at]), describeCutRows(rows))
			}
			at++
		}
		if row.marked {
			// The marker occupies the cell the row's last character would have
			// taken, and that character MOVES to the row below: the reading is
			// continuous, so the walk does not advance over anything here. That
			// is the difference between a label and a euphemism — nothing the
			// reader cannot see is standing behind the glyph.
			displaced++
		}
	}
	if at != len(src) {
		t.Fatalf("the band consumed %d of the answer's %d cells — %d were dropped\n%s",
			at, len(src), len(src)-at, describeCutRows(rows))
	}
	if displaced == 0 {
		t.Fatalf("no row wears the cut marker, yet the band is whole — the else branch "+
			"of this claim was never exercised\n%s", describeCutRows(rows))
	}
}

// describeCutRows renders the rows with their measured widths, so a failure shows
// the reader exactly what the band did (a bare "want marker" reads as a missing
// assertion, not as the defect).
func describeCutRows(rows []string) string {
	var b strings.Builder
	for i, row := range rows {
		b.WriteString("  row ")
		b.WriteString(itoaSmall(i))
		b.WriteString(" cells=")
		b.WriteString(itoaSmall(displayWidth(row)))
		b.WriteString(" |")
		b.WriteString(row)
		b.WriteString("|\n")
	}
	return b.String()
}

// leadingCells is the rune index at which the cell column n starts: a wide rune
// straddling the column keeps its whole cell.
func leadingCells(rs []rune, n int) int {
	col := 0
	for i, r := range rs {
		if col >= n {
			return i
		}
		col += runeCells(r)
	}
	return len(rs)
}

// itoaSmall is strconv.Itoa for the non-negative row indices this file prints,
// kept local so the failure text needs no second import.
func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
