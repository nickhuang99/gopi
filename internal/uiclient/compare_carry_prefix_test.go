package uiclient

// compare_carry_prefix_test.go — the row shapes the re-paint rule does NOT cover,
// and the self-contradictory verdict they used to produce.
//
// THE REPORTED CONTRADICTION (task 01a0e313-ac65, real 117x26 pty): a 95-rune ASCII
// answer line was audited as
//
//	damage=paint_loss grid_row=-1 in_grid=false expected_runes=95 prefix=95
//
// — `grid_prefix_runes` equals the line's WHOLE rune count (the entire line is
// verbatim at the start of a grid row) while the verdict says the paint lost it.
// The reader is sent to "the paint lost your text" when the paint carried all of it.
//
// The repaint_tail rule (rowCarriesLine, commit e167bbb9) closed the shape the old
// report was measured on — the row carrying the line DOUBLED (`line+line`), whose
// tail is the line's own head and therefore a re-paint. It does NOT cover the rows
// below, where the row opens with the whole line and then continues with something
// else: a second re-paint (`line+line+line`, whose tail is longer than the line, so
// the "the tail is the line's own head" test cannot fire), or a re-paint that left
// ANOTHER line's text on the row. In every one of those the paint carried ALL of the
// line, and the audit must not name the one damage class that says it carried none.
//
// WHAT IS ASSERTED, and what deliberately is NOT. These cases pin that such a row is
// detected and classified as damage to the ROW rather than to the line's text — they
// do NOT assert CompareMatch. A row still carrying a stale re-paint is a real,
// visible paint defect (see CompareVerdict.DisplayHits) and must stay visible; what
// it must not be called is a LOSS of text that is on the screen.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// asciiReportedLine is the line the report was measured on, verbatim.
const asciiReportedLine = "self-observation: this line must land on the canvas verbatim, and the verb compares both sides."

// TestCompareTranscriptLine_DoubledRowIsNotPaintLoss is the reported shape at the
// containment core: the row carries the whole line, twice. It is the GREEN leg of
// the re-paint rule (rowCarriesLine) and the case the report was filed on.
func TestCompareTranscriptLine_DoubledRowIsNotPaintLoss(t *testing.T) {
	cand := compareCandidate(GeometrySourceLayout, lineedit.TurnMarker+asciiReportedLine+asciiReportedLine)
	v := CompareTranscriptLine(asciiReportedLine, cand, []string{asciiReportedLine})
	if v.Status == CompareMismatch {
		t.Fatalf("status=%q in_grid=%t prefix=%d expected_runes=%d — the whole line is on the row, twice, and the verdict blames the paint for losing it",
			v.Status, v.InGrid, v.GridPrefixRunes, len([]rune(asciiReportedLine)))
	}
	if v.DisplayHits < 2 {
		t.Fatalf("display_hits=%d, want at least 2: the row carries the line more than once and the verdict must say so", v.DisplayHits)
	}
}

// TestCompareTranscriptLine_TripleRowIsNotPaintLoss is the shape the re-paint rule
// MISSES: a re-paint that fired twice leaves `line+line+line`, whose tail (190 runes)
// is longer than the line, so "the tail is the line's own head" cannot be true — and
// the audit reported damage=paint_loss with prefix 95 of 95 runes.
func TestCompareTranscriptLine_TripleRowIsNotPaintLoss(t *testing.T) {
	row := lineedit.TurnMarker + strings.Repeat(asciiReportedLine, 3)
	cand := compareCandidate(GeometrySourceLayout, row)
	v := CompareTranscriptLine(asciiReportedLine, cand, []string{asciiReportedLine})

	if v.GridPrefixRunes < len([]rune(asciiReportedLine)) {
		t.Fatalf("the fixture does not reproduce the contradiction: prefix=%d of %d runes",
			v.GridPrefixRunes, len([]rune(asciiReportedLine)))
	}
	if v.Status == CompareMismatch && v.Damage == DamagePaintLoss {
		t.Fatalf("VERDICT CONTRADICTS ITS OWN EVIDENCE: in_grid=%t prefix=%d expected_runes=%d damage=%s on a row that carries the whole line — the paint did not lose this text, the ROW is damaged",
			v.InGrid, v.GridPrefixRunes, len([]rune(asciiReportedLine)), v.Damage)
	}
	if v.DisplayHits < 3 {
		t.Fatalf("display_hits=%d, want 3: every copy on the row is a real visible defect and the count is what keeps it visible", v.DisplayHits)
	}
}

// TestCompareTranscriptLine_SupersededRowIsNotPaintLoss is the same contradiction
// reached through the ordinary cause rather than the doubling: the band re-painted
// this line and then drew ANOTHER line over the rest of the row, so the row opens
// with the whole line and continues with foreign text. The line's own text is
// entirely on the row; calling that a paint_loss tells a reader the paint lost text
// it is looking at.
func TestCompareTranscriptLine_SupersededRowIsNotPaintLoss(t *testing.T) {
	const otherLine = "and here is a different second line the band drew afterwards"
	cand := compareCandidate(GeometrySourceLayout, lineedit.TurnMarker+asciiReportedLine+otherLine)
	v := CompareTranscriptLine(asciiReportedLine, cand, []string{asciiReportedLine})

	if v.GridPrefixRunes < len([]rune(asciiReportedLine)) {
		t.Fatalf("the fixture does not reproduce the contradiction: prefix=%d of %d runes",
			v.GridPrefixRunes, len([]rune(asciiReportedLine)))
	}
	if v.Status == CompareMismatch && v.Damage == DamagePaintLoss {
		t.Fatalf("VERDICT CONTRADICTS ITS OWN EVIDENCE: in_grid=%t prefix=%d expected_runes=%d damage=%s — the row opens with the whole line",
			v.InGrid, v.GridPrefixRunes, len([]rune(asciiReportedLine)), v.Damage)
	}
	if v.DisplayHits != 1 {
		t.Fatalf("display_hits=%d, want 1: the whole line is on the row exactly once, with other text after it", v.DisplayHits)
	}
}

// TestCompareTranscriptLine_TruncatedRowIsStillPaintLoss is the direction that must
// NOT move, and it is the reason the new rule is about the whole line rather than
// "the row starts with the line": a row that carries only the line's HEAD (the band
// cut it) really did lose the tail. Every other case in this file must be
// distinguishable from this one, and the prefix is the number that distinguishes
// them — 20 runes here against a full 95 there.
func TestCompareTranscriptLine_TruncatedRowIsStillPaintLoss(t *testing.T) {
	row := lineedit.TurnMarker + string([]rune(asciiReportedLine)[:20])
	cand := compareCandidate(GeometrySourceLayout, row)
	v := CompareTranscriptLine(asciiReportedLine, cand, []string{asciiReportedLine})

	if v.Status != CompareMismatch {
		t.Fatalf("status=%q, want %q: this row really is a lost tail", v.Status, CompareMismatch)
	}
	if v.Damage != DamagePaintLoss {
		t.Fatalf("damage=%q, want %q — a row carrying 20 of 95 runes is the real-loss class", v.Damage, DamagePaintLoss)
	}
	if v.DisplayHits != 0 {
		t.Fatalf("display_hits=%d, want 0: the line is not on this row at all", v.DisplayHits)
	}
}

// TestCompareTranscriptLine_WrappedRowOfExactlyGridWidthIsNotPaintLoss is the
// TAXONOMY defect (the report's second finding): a line as wide as the grid ITSELF
// was classified by `cols > grid.Cols` — the width test — and so never even asked the
// wrap instrument whether the grid actually reassembled it. Measured: a 117-cell
// ASCII line the grid wrapped across two rows came back
// `damage=paint_loss ... "the paint lost it, re-wrapped it"` with the whole text
// present on the grid. The class asked a WIDTH question about a fact that is
// observable, and the observable said "arrived".
func TestCompareTranscriptLine_WrappedRowOfExactlyGridWidthIsNotPaintLoss(t *testing.T) {
	const cols = 117
	line := strings.Repeat("abcdefghij", 12)[:cols]
	half := []rune(line)
	cut := len(half) / 2
	cand := compareCandidate(GeometrySourceLayout,
		lineedit.TurnMarker+string(half[:cut]), "  "+string(half[cut:]))

	v := CompareTranscriptLine(line, cand, []string{line})
	if v.Status == CompareMatch {
		// The wrap is now an ARRIVAL on this face too (the join instrument is asked
		// here, not only by the ledger), so the fixture must assert the stronger fact
		// rather than the old "not a paint loss": the rows were reassembled and the
		// prefix runs to the line's whole length — a loss class beside a prefix
		// covering the line was the reported contradiction.
		if !v.Rejoined || v.GridPrefixRunes != len([]rune(line)) {
			t.Fatalf("the grid carried the line wrapped (rejoined=%t prefix=%d of %d runes, %d cells at a %d-cell grid): the wrap must be an arrival whose prefix is measured in the LINE's space",
				v.Rejoined, v.GridPrefixRunes, len([]rune(line)), lineedit.DisplayWidth(line), cols)
		}
		return
	}
	if v.Damage == DamagePaintLoss {
		t.Fatalf("damage=%q for a line the grid carried WRAPPED (prefix=%d of %d runes, %d cells at a %d-cell grid): a width test answered a question the grid can be asked directly",
			v.Damage, v.GridPrefixRunes, len([]rune(line)), lineedit.DisplayWidth(line), cols)
	}
}

// TestCompareTranscriptLine_LostWideAsciiLineIsStillNotAMatch is the anti-widening
// control for the case above: a line as wide as the grid that is NOWHERE on the grid
// must still be reported, and its class must stay the honest "a wrap explains it as
// well as a loss does" rather than the real-loss claim — or the join's new reach
// would have turned a genuine loss into a pass.
func TestCompareTranscriptLine_LostWideAsciiLineIsStillNotAMatch(t *testing.T) {
	const cols = 117
	line := strings.Repeat("abcdefghij", 12)[:cols]
	cand := compareCandidate(GeometrySourceLayout, lineedit.TurnMarker+"nothing of it here")
	v := CompareTranscriptLine(line, cand, []string{line})
	if v.Status != CompareMismatch {
		t.Fatalf("status=%q, want %q", v.Status, CompareMismatch)
	}
	if v.Damage != DamageWrapArtifactOrLoss {
		t.Fatalf("damage=%q, want %q: the line is as wide as the grid, so a wrap explains its absence as well as a loss does",
			v.Damage, DamageWrapArtifactOrLoss)
	}
}
