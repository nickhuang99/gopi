package lineedit

// answer_cut_labels_test.go — the RED/GREEN pair for the renderer's cut-label
// ledger (answer_cut_labels.go).
//
// THE PROPERTY, stated as cells: when the band's right edge cuts a row, the
// renderer DRAWS the product's cut label in the row's last cell — a character
// the answer's own text does not contain — and it RECORDS that it did. The
// second half is the epic's defect: without the record, a reader comparing the
// screen against the model's wire text sees two copies of "the same" body that
// disagree, and cannot tell which layer produced the difference.
//
// The ledger is asserted against the GRID (a Screen fed the renderer's own
// bytes), so a record cannot agree with itself while the paint does something
// else: the number of labels the screen shows and the number the ledger reports
// must be the same number, for the same rows.

import (
	"strings"
	"testing"
	"time"
)

// cutLabelProbeRows/Cols are the band geometry the probe runs at. Wide enough to
// hold the turn header, narrow enough that a word-character line is cut.
const (
	cutLabelProbeCols = 40
	cutLabelProbeRows = 26
	// cutLabelProbeBurst is the delta size the body is streamed in: a real token
	// cadence, so the cut happens mid-stream exactly as it does in a run.
	cutLabelProbeBurst = 7
	// cutLabelProbePrompt is the operator's line for the turn (the header needs
	// one; the label ledger never reads it).
	cutLabelProbePrompt = "probe"
)

// cutLabelRuneForTest is the cut label's rune, taken from the production constant
// (Rule 1/13: the ONE definition is answerCutLabel, and this alias exists so the
// test reads as "the glyph under test" rather than as a literal typed into three
// places — it moved off statusEllipsis when the band got its own glyph,
// answer_cut_label_glyph.go). A var, not a const: a rune decoded from a string is
// not a constant expression.
var cutLabelRuneForTest = []rune(answerCutLabel)[0]

// cutLabelProbeBody is a line of WORD characters with no whitespace and no
// separator in it — the one shape that forces the band's right edge to cut
// between two narrow word runes, which is the case the cut label exists for. It
// is built from a single repeated rune so the arithmetic is readable.
var cutLabelProbeBody = strings.Repeat("identifier", 30)

// drawCutLabelProbe streams the probe body through a real renderer and returns
// the band's body rows (gutter stripped) plus the renderer, so a caller can read
// the ledger the same run produced.
func drawCutLabelProbe(t *testing.T) ([]string, *AnswerRenderer) {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(cutLabelProbeRows, cutLabelProbeCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", cutLabelProbeRows, cutLabelProbeCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	r.SetClock(clock.Now)
	r.SetPalette(BasicPalette)
	r.BeginTurn(TurnInfo{Index: 1, Prompt: cutLabelProbePrompt})
	for _, b := range chunkByRunes(cutLabelProbeBody, cutLabelProbeBurst) {
		r.Delta(b)
	}
	r.Delta(terminalNewline)
	r.Tick()
	r.Finish()
	r.EndTurn()

	sc := NewScreen(cutLabelProbeCols, cutLabelProbeRows)
	sc.Feed([]byte(cap.bytes()))
	lines := make([]string, 0, cutLabelProbeRows)
	for i := 0; i < cutLabelProbeRows; i++ {
		lines = append(lines, strings.TrimRight(sc.Line(i), " "))
	}
	header := rowOf(lines, turnHeaderText(1, cutLabelProbePrompt))
	if header < 0 {
		t.Fatalf("no turn header on the grid\n--- band ---\n%s", strings.Join(lines, "\n"))
	}
	body := make([]string, 0, 4)
	for i := skipRequestRows(lines, header+1, cutLabelProbePrompt, cutLabelProbeCols); i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == strings.Repeat(TurnRuleGlyph, cutLabelProbeCols) {
			break
		}
		body = append(body, lines[i])
	}
	if len(body) == 0 {
		t.Fatalf("the answer produced no band rows\n--- band ---\n%s", strings.Join(lines, "\n"))
	}
	return body, r
}

// TestAnswerCutLabel_IsRecordedWhereItIsDrawn is the RED half: a body carrying
// neither U+2026 nor the band's own cut glyph, drawn through a band narrow enough
// to cut it, has the product's cut label on the grid — AND the renderer reports it
// as its own.
//
// RED on the pre-change tree: the glyph is drawn and NOTHING records it, so the
// only thing a reader can compare is the screen against the wire, and the screen
// is the one that disagrees.
func TestAnswerCutLabel_IsRecordedWhereItIsDrawn(t *testing.T) {
	body, r := drawCutLabelProbe(t)

	// The body's OWN text carries no ellipsis: every `…` on this grid is the
	// renderer's. That is the epic's scenario in one line.
	if strings.Contains(cutLabelProbeBody, string(cutLabelRuneForTest)) {
		t.Fatalf("the probe body must not itself contain the character under test")
	}

	drawn := 0
	for _, row := range body {
		drawn += strings.Count(row, string(cutLabelRuneForTest))
	}
	if drawn == 0 {
		t.Fatalf("the band cut nothing: %d row(s), none wearing the cut label — the probe "+
			"proves nothing\n%s", len(body), describeCutRows(body))
	}

	rows, total := r.CutLabels()
	if total != drawn {
		t.Fatalf("the renderer drew %d cut label(s) on the grid but records %d — a record that "+
			"disagrees with the paint misattributes the character\n--- band ---\n%s",
			drawn, total, describeCutRows(body))
	}
	if len(rows) != total {
		t.Fatalf("the ledger kept %d row(s) but counted %d; the cap is %d and nothing here "+
			"reaches it, so the kept list must be complete", len(rows), total, cutLabelLedgerCap)
	}
	for i, rec := range rows {
		if rec.Turn != 1 {
			t.Fatalf("record %d names turn %d, want 1 — a label that cannot be traced to its "+
				"turn is not reported", i, rec.Turn)
		}
		if rec.Row < 0 {
			t.Fatalf("record %d names row %d; the label was drawn on a live row and must say "+
				"which one", i, rec.Row)
		}
	}
}

// TestAnswerCutLabel_IsQuietWhenNothingIsCut is the GREEN half: a body that fits
// the band is drawn exactly as the answer wrote it, and the ledger says so — a
// record that fires on an uncut body would turn every answer into "the renderer
// altered this", which is the misattribution this work exists to remove.
func TestAnswerCutLabel_IsQuietWhenNothingIsCut(t *testing.T) {
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(cutLabelProbeRows, cutLabelProbeCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", cutLabelProbeRows, cutLabelProbeCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetClock((&fakeClock{t: time.Unix(a5UnixTime, 0)}).Now)
	r.SetPalette(BasicPalette)
	r.BeginTurn(TurnInfo{Index: 2, Prompt: cutLabelProbePrompt})
	const short = "short answer"
	r.Delta(short)
	r.Delta(terminalNewline)
	r.Tick()
	r.Finish()
	r.EndTurn()

	rows, total := r.CutLabels()
	if total != 0 || len(rows) != 0 {
		t.Fatalf("a body that fits the band recorded %d cut label(s) (%d kept): %+v",
			total, len(rows), rows)
	}
}

// TestAnswerCutLabel_LedgerIsBoundedAndCounts is the Rule 15 arm: the kept list
// is capped, and the cap is never a silent truncation — the total is reported
// beside it.
func TestAnswerCutLabel_LedgerIsBoundedAndCounts(t *testing.T) {
	var l cutLabelLedger
	over := cutLabelLedgerCap + 5
	for i := 0; i < over; i++ {
		l.add(3, i)
	}
	rows, total := l.snapshot()
	if total != over {
		t.Fatalf("the ledger counted %d labels, want %d — the cap must bound the LIST, never "+
			"the count", total, over)
	}
	if len(rows) != cutLabelLedgerCap {
		t.Fatalf("the ledger kept %d rows, want %d", len(rows), cutLabelLedgerCap)
	}
	if rows[len(rows)-1].Row != cutLabelLedgerCap-1 {
		t.Fatalf("the kept rows are not the FIRST %d in draw order: last kept row is %d",
			cutLabelLedgerCap, rows[len(rows)-1].Row)
	}
}
