package replayplay

// reportrow_test.go — THE TWO CUTS THIS VIEW PERFORMS MUST NOT LOOK ALIKE.
//
// The view can shorten a line in exactly two ways, and they mean opposite
// things, so they must be distinguishable BY GLYPH:
//
//	content cut      the recording is longer than the spelling admits —
//	                 boundRunes appends `…(+N runes)` (roundToolInputMaxRune).
//	report row cut   THIS PACKAGE speaks and the CANVAS is too narrow — the
//	                 row is shortened and closed with reportRowCutLabel.
//
// THE DEFECT PINNED HERE (measured 2026-11-01, session 61f5992a,
// `session replay --turn 8 --cols 40`): the turn body's response row printed
//
//	(3 row(s) of this turn's context are not
//
// — fitCells ended it at the canvas edge and dropped `; its tool rows are)`
// with NO mark at all, so the one row whose job is to admit a cut was itself
// cut silently. The operator's report ("the replay prints `…` where nothing was
// cut") is the other half of the same confusion: a mark standing for two
// opposite facts.
//
// The tests below are the epic's two assertions, kept separate on purpose: a
// change that fixes one and collapses the other cannot pass both.

import (
	"fmt"
	"strings"
	"testing"
)

// elisionRune is U+2026, spelled as a rune so this file can assert its ABSENCE
// from a report row without typing it into a string literal that a reader would
// have to eyeball (the glyph is invisible in most editors' widths).
const elisionRune = '\u2026'

// contentCutNote is the note boundRunes appends to a cut CONTENT line. It is
// built here rather than spelled, so the assertion is about the note's SHAPE
// (the count it states) and not about a copy of its bytes.
const contentCutNote = "(+"

// reportRowsNarrowFixture is a turn whose first exchange floods the ceiling (so
// the body must report dropped context) and whose single round, laid out on its
// own, must report its own overflow.
func reportRowsNarrowFixture() Round {
	turn := Round{Index: 1, Kind: RoundTurn, RoundTotal: 2, Prompt: "ask"}
	flood := Round{Index: 1, Kind: RoundModel, Prompt: "ask"}
	for i := 0; i < roundRowsMax; i++ {
		flood.HarnessPrompts = append(flood.HarnessPrompts, fmt.Sprintf("tick %d", i))
	}
	flood.Calls = []ToolCallView{{Name: "bash", InputRaw: `{"command":"echo flooded"}`, Result: "flooded result"}}
	turn.Segments = []Round{flood}
	return turn
}

// TestReportRowsNeverWearTheElisionGlyph is assertion (1): a report row says
// what this package did to the view, so it may never carry the mark that means
// "content was cut" — and, when the canvas is too narrow to hold it, it wears
// reportRowCutLabel so the shortening is VISIBLE.
func TestReportRowsNeverWearTheElisionGlyph(t *testing.T) {
	turn := reportRowsNarrowFixture()

	// A canvas narrower than the sentence: the row MUST be shortened, and the
	// shortening must be stated.
	const narrow = 40
	body := TurnBodyRows(turn, narrow)

	var reportRows []string
	for _, r := range body {
		if r.Role == RoleError {
			reportRows = append(reportRows, r.Cells)
		}
	}
	if len(reportRows) == 0 {
		t.Fatalf("the fixture's narrow body carries no cut report: it does not reach the ceiling, so this test proves nothing:\n%s", joinRowCells(body))
	}
	for _, cells := range reportRows {
		if strings.ContainsRune(cells, elisionRune) {
			t.Fatalf("a report row wears U+2026: %q — that glyph means CONTENT was cut and a report row is the view speaking, not the recording", cells)
		}
		if !strings.Contains(cells, reportRowCutLabel) {
			t.Fatalf("a report row was shortened by the %d-column canvas WITHOUT saying so: %q — the row that admits a cut may not itself be cut silently", narrow, cells)
		}
		if got := cellsOf(cells); got != narrow {
			t.Fatalf("a report row measures %d cell(s) on a %d-column canvas: %q", got, narrow, cells)
		}
	}

	// A canvas wide enough to hold them: the sentence is DRAWN WHOLE, states the
	// measured count, and wears no label at all (a label on a row that was not
	// shortened would be a false claim about a cut that never happened).
	wide := TurnBodyRows(turn, FallbackCols)
	stated := 0
	for _, r := range wide {
		if r.Role != RoleError {
			continue
		}
		if strings.Contains(r.Cells, reportRowCutLabel) {
			t.Fatalf("a report row on an %d-column canvas claims it was shortened: %q", FallbackCols, r.Cells)
		}
		if strings.ContainsRune(r.Cells, elisionRune) {
			t.Fatalf("a report row wears U+2026 on a wide canvas: %q", r.Cells)
		}
		var n int
		if _, err := fmt.Sscanf(r.Cells, turnBodySegmentDroppedFmt, &n); err == nil {
			stated++
		}
	}
	if stated == 0 {
		t.Fatalf("the wide body carries no whole turn-dropped report — the sentence must be readable where the canvas admits it:\n%s", joinRowCells(wide))
	}

	// The round's OWN overflow report (roundTruncatedRowFmt) is the second report
	// row, and it takes the same path: shortened, labelled, never elided.
	r := turn.Segments[0]
	roundBody := roundRows(r, Dims{Cols: narrow})
	if len(roundBody) < 2 {
		t.Fatalf("the fixture round laid out %d row(s): it does not reach the round ceiling", len(roundBody))
	}
	tail := roundBody[len(roundBody)-1]
	if tail.Role != RoleError {
		t.Fatalf("the round's last row is role %v, want the overflow report — this test needs one", tail.Role)
	}
	if strings.ContainsRune(tail.Cells, elisionRune) {
		t.Fatalf("the round's overflow row wears U+2026: %q", tail.Cells)
	}
	if !strings.Contains(tail.Cells, reportRowCutLabel) {
		t.Fatalf("the round's overflow row was shortened silently on a %d-column canvas: %q", narrow, tail.Cells)
	}
}

// TestContentCutStillSaysHowManyRunesItLeftOut is assertion (2): the OTHER cut —
// the recording's own input, bounded at roundToolInputMaxRune — keeps its note.
// The epic's fix must not take the content note away with the report glyph: the
// two cuts are different facts and both must stay legible.
func TestContentCutStillSaysHowManyRunesItLeftOut(t *testing.T) {
	long := strings.Repeat("x", roundToolInputMaxRune+25)
	round := Round{Index: 1, Kind: RoundModel}
	round.Calls = []ToolCallView{{Name: "bash", InputRaw: fmt.Sprintf("%q", long), Result: "ok"}}

	rows := roundRows(round, Dims{Cols: FallbackCols})
	var cut []string
	for _, r := range rows {
		if r.Role == RoleToolCall && strings.Contains(r.Cells, contentCutNote) {
			cut = append(cut, r.Cells)
		}
	}
	if len(cut) == 0 {
		t.Fatalf("a %d-rune tool input laid out on an %d-column canvas carries no `…(+N runes)` note: the content cut became silent:\n%s",
			len(long), FallbackCols, joinRowCells(rows))
	}
	// The note names a MEASURED count, not a bare marker.
	if !strings.Contains(cut[0], " runes)") {
		t.Fatalf("the content-cut note %q does not name runes: the two cuts must stay distinguishable", cut[0])
	}

	// And the DEBUG spelling keeps the note away — it cuts nothing, so it must
	// not claim a cut (the same honesty the report rows get above).
	full := roundRows(round, Dims{Cols: FallbackCols, FullToolInput: true})
	for _, r := range full {
		if strings.Contains(r.Cells, contentCutNote) {
			t.Fatalf("the debug lay-out reports a content cut it did not perform: %q", r.Cells)
		}
	}
}
