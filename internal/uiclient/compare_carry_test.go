package uiclient

// compare_carry_test.go — the row that carries a line AND continues past it.
//
// THE MEASURED DEFECT this pins (task 01a0e305-f0b4-7a47-a07a-2d0cf05a2126,
// 2026-09-27, real 117x26 pty): the band re-drew the answer line while the tail of
// the previous paint of that SAME line was still on the row, so the dump's row
// held the line followed by the line's own head again. The containment core matched
// rows BYTE-EXACT, the row was therefore never equal to the line, and
// `gopi ui_state --target chat --all` reported a faithful CJK paint as
// damage=paint_loss (exit 3) — the diagnosis inverted.
//
// Both directions are pinned here: the carried row is ARRIVAL, and a row that
// carries the line plus anything that is NOT the line's own head stays a mismatch,
// so the rule cannot blanket-excuse a genuinely lost line.

import (
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// cjkAnswerLine is the line the defect was measured on, verbatim.
const cjkAnswerLine = "自观测闭环：这一行必须原样落在画布上，verb 自己会把两面对照。"

// TestCompareTranscriptLine_RowCarryingLinePlusItsOwnHeadIsArrival is the GREEN
// direction: the measured row is an arrival, not a loss.
func TestCompareTranscriptLine_RowCarryingLinePlusItsOwnHeadIsArrival(t *testing.T) {
	head := []rune(cjkAnswerLine)
	// The measured tail: the line's own head re-drawn, 29 runes of it.
	const tailRunes = 29
	row := lineedit.TurnMarker + cjkAnswerLine + string(head[:tailRunes])
	cand := compareCandidate(GeometrySourceLayout, "turn 1 banner", row)

	v := CompareTranscriptLine(cjkAnswerLine, cand, []string{cjkAnswerLine})
	if v.Status != CompareMatch {
		t.Fatalf("a row carrying the whole line was not an arrival: status=%q in_grid=%t reason=%q",
			v.Status, v.InGrid, v.Reason)
	}
	if !v.InGrid || v.GridRow != 1 {
		t.Fatalf("in_grid=%t grid_row=%d, want true and 1", v.InGrid, v.GridRow)
	}
	if v.RowTailRunes != tailRunes {
		t.Fatalf("row_tail_runes=%d, want %d — the evidence for the verdict is the tail, and a reader must be able to re-do the arithmetic",
			v.RowTailRunes, tailRunes)
	}

	// The row as the grid owner renders it: ScreenDump.Lines trims the trailing
	// blanks of a padded row, so a row whose text is followed by padding carries no
	// tail at all. Both shapes are the same arrival, and the tail number tells them
	// apart — which is exactly why it is reported.
	exact := CompareTranscriptLine(cjkAnswerLine, compareCandidate(GeometrySourceLayout, lineedit.TurnMarker+cjkAnswerLine), []string{cjkAnswerLine})
	if exact.Status != CompareMatch || exact.RowTailRunes != 0 {
		t.Fatalf("the ordinary row-exact hit must still be an arrival with no tail: status=%q row_tail_runes=%d", exact.Status, exact.RowTailRunes)
	}
}

// TestCompareTranscriptLine_TailMustBeTheLinesOwnHead is the RED (no-blanket-excuse)
// direction, and the reason the rule is not "the row starts with the line": a row
// that continues with ANY OTHER text is somebody else's row, and the line's
// containment answer must NOT be reported as an arrival on that row.
//
// WHAT MOVED, and why: these rows are not arrivals, but calling them paint_loss is
// what the reported CONTRADICTION was (task 01a0e313: `in_grid=false` beside a prefix
// equal to the line's whole rune count). They are damage_to_row — the line's text IS
// on the row, and the ROW is what is damaged — and the class is asserted here so the
// "not an arrival" direction keeps its teeth. The genuine-loss control (the truncated
// row) stays paint_loss and is asserted below. `RowTailRunes` is NOT the field that
// measures this class: its contract (-1 = no row carries the line) is unchanged, and
// the tail of a damage_to_row row is derived by tailPastFirstCopy.
func TestCompareTranscriptLine_TailMustBeTheLinesOwnHead(t *testing.T) {
	cases := []struct {
		name string
		row  string
	}{
		{
			// A row carrying this line and then a DIFFERENT line (a wrap of the next
			// transcript line, or a stale unrelated paint). Accepting this as an
			// ARRIVAL would stop reporting the row defect; calling it paint_loss
			// would report a loss of text that is on the screen.
			name: "a different second line on the row",
			row:  lineedit.TurnMarker + cjkAnswerLine + "另一行完全不同的内容",
		},
		{
			// The line's head re-drawn and then FOREIGN text appended past the line's
			// own length: the row carries a third thing the line does not have.
			name: "the head re-drawn plus foreign text",
			row:  lineedit.TurnMarker + cjkAnswerLine + cjkAnswerLine + "某",
		},
		{
			// The head matches its first rune then diverges: a re-paint draws the
			// line from its start, so a divergence is foreign text.
			name: "a tail that diverges from the line after one rune",
			row:  lineedit.TurnMarker + cjkAnswerLine + string([]rune(cjkAnswerLine)[:1]) + "某",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := CompareTranscriptLine(cjkAnswerLine, compareCandidate(GeometrySourceLayout, tc.row), []string{cjkAnswerLine})
			if v.InGrid {
				t.Fatalf("in_grid=true — the row is not the line, and it must not be reported as an arrival")
			}
			if v.Status == CompareMatch {
				t.Fatalf("status=%q — a row carrying foreign text is not a match", v.Status)
			}
			if v.RowTailRunes != -1 {
				t.Fatalf("row_tail_runes=%d, want -1 (the field means 'no row CARRIES this line', and none does)", v.RowTailRunes)
			}
			if v.Damage != DamageToRow {
				t.Fatalf("damage=%q, want %q — the whole line is at the row's start, so the damage is the ROW's (damage=%q would report a loss of text that is on the screen)",
					v.Damage, DamageToRow, DamagePaintLoss)
			}
			if v.GridPrefixRunes < len([]rune(cjkAnswerLine)) {
				t.Fatalf("the prefix diagnostic (%d) does not cover the line (%d)", v.GridPrefixRunes, len([]rune(cjkAnswerLine)))
			}
		})
	}

	// THE REAL-LOSS CONTROL: the row is a genuine PREFIX of the line (the band
	// truncated it), so the tail really is missing and the real-loss class is what
	// must come back.
	t.Run("a truncated line, no tail", func(t *testing.T) {
		v := CompareTranscriptLine(cjkAnswerLine, compareCandidate(GeometrySourceLayout, lineedit.TurnMarker+string([]rune(cjkAnswerLine)[:20])), []string{cjkAnswerLine})
		if v.Status != CompareMismatch {
			t.Fatalf("status=%q in_grid=%t, want %q", v.Status, v.InGrid, CompareMismatch)
		}
		if v.Damage != DamagePaintLoss {
			t.Fatalf("damage=%q, want %q: this row really is a lost tail", v.Damage, DamagePaintLoss)
		}
		if v.RowTailRunes != -1 {
			t.Fatalf("row_tail_runes=%d, want -1", v.RowTailRunes)
		}
	})
}

// TestCompareTranscriptLine_ASCIIExactRowIsStillAnArrival keeps the ASCII control
// green at the containment layer: an ASCII line painted on one row exactly as the
// transcript has it is matched, with no tail claim.
func TestCompareTranscriptLine_ASCIIExactRowIsStillAnArrival(t *testing.T) {
	const ascii = "self-observation loop: this line must land on the canvas verbatim, and the verb checks both sides."
	cand := compareCandidate(GeometrySourceLayout, "banner", lineedit.TurnMarker+ascii)
	v := CompareTranscriptLine(ascii, cand, []string{ascii})
	if v.Status != CompareMatch || !v.InGrid || v.RowTailRunes != 0 {
		t.Fatalf("the ASCII control regressed: status=%q in_grid=%t row_tail_runes=%d", v.Status, v.InGrid, v.RowTailRunes)
	}
}
