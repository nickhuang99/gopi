package lineedit

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// statusColumnEmphasisRow builds a row whose ONE column carries a
// measured-quality span — the shape cmd/agent composes for the cache hit rate
// (the metrics column's "hit 68%" inside a field that also carries other
// read-outs). It is the SMALLEST sample that exercises the channel: one column,
// an emphasis inside it, and text AFTER the span inside the same field (the
// cells the nested-reset defect would strip).
func statusColumnEmphasisRow(suffix string) Status {
	return Status{
		Columns: []StatusColumn{
			{Text: "hit 68%" + suffix, Width: statusColumnEmphasisWidth, EmphasisSpan: "hit 68%", Emphasis: BasicPalette.StatusBad},
		},
		Live:       "ready",
		LiveWidth:  8,
		LayoutCols: BudgetWidth,
	}
}

// statusColumnEmphasisSuffix is the text that follows the measured span INSIDE
// the same column, and it is the sample the nested-reset guard reads: a defect
// there costs exactly the cells between the span and the field's own end.
const statusColumnEmphasisSuffix = " 2k/1k"

// statusColumnEmphasisWidth is the column's reserved width: the field's own text
// at its widest, plus the padding a real column carries (the metrics column
// reserves 27 cells for a field of this shape). It is wide enough that a tail cut
// cannot be mistaken for the nested-reset defect this file exists to catch.
const statusColumnEmphasisWidth = 20

// columnRowGrid replays a composed row through the REAL cell grid and returns
// the grid — the only reader that knows what a terminal would draw, because an
// SGR sequence is not a cell.
func columnRowGrid(t *testing.T, row string) *Screen {
	t.Helper()
	sc := NewScreen(BudgetWidth, 1)
	sc.Feed([]byte(row))
	return sc
}

// TestStyledStatusLine_ColumnSpanCarriesItsOwnStyle: on the ALIGNED path the
// measured-quality span wears its own tier, and it wears it INSIDE the column's
// field — the channel this test exists for, because a field is otherwise styled
// whole by its identity (styledFields) and a score riding inside one has no other
// way to colour itself.
func TestStyledStatusLine_ColumnSpanCarriesItsOwnStyle(t *testing.T) {
	s := statusColumnEmphasisRow("")
	row := styledStatusLine(s, BudgetWidth, BasicPalette)
	plain := stripSGR(row)
	at := strings.Index(plain, "hit 68%")
	if at < 0 {
		t.Fatalf("the score is not on the row: %q", plain)
	}
	sc := columnRowGrid(t, row)
	spanAttr := sc.Attr(0, at)
	if spanAttr.Equal(sgr.Default()) {
		t.Fatalf("the measured span wears no style at all:\n%q", row)
	}
	// The tier must be distinguishable from the FIELD's own style, or this row
	// would pass on a renderer that never resolved a tier (the silent no-op the
	// channel exists to remove). The comparison cell is the field's PADDING
	// (after the span, still inside the column), so it is the field style and
	// nothing else.
	padAttr := sc.Attr(0, at+len("hit 68%"))
	if spanAttr.Equal(padAttr) {
		t.Fatalf("the span's cell carries the field's own style, not the tier:\n%q", row)
	}
	if BasicPalette.StatusBad.Open() == BasicPalette.StatusLabel.Open() {
		t.Fatal("the fixture proves nothing: the bad tier and the field style are the same sequence")
	}
	if stripSGR(row) != statusLine(s, BudgetWidth) {
		t.Fatalf("styling changed the TEXT of the row:\n got %q\nwant %q",
			stripSGR(row), statusLine(s, BudgetWidth))
	}
}

// TestStyledStatusLine_ColumnKeepsItsColourAfterTheSpan is the NESTED-RESET
// guard, and the reason the span is emphasised by RE-STATEMENT rather than by
// splicing a sequence into an already-styled field: the span's own Wrap closes
// with a reset, which also ends the FIELD's colour. Were the field style not
// re-opened after the span, every character of that column following the score
// would fall back to the terminal's default — read here as the tail cells
// carrying the default style.
func TestStyledStatusLine_ColumnKeepsItsColourAfterTheSpan(t *testing.T) {
	tail := statusColumnEmphasisSuffix
	s := statusColumnEmphasisRow(tail)
	row := styledStatusLine(s, BudgetWidth, BasicPalette)
	plain := stripSGR(row)
	if !strings.Contains(plain, tail) {
		t.Fatalf("the column's tail is not on the row at all:\n%q", row)
	}
	sc := columnRowGrid(t, row)
	tailAt := strings.Index(plain, tail)
	spanAt := strings.Index(plain, "hit 68%")
	for i := 1; i < len(tail); i++ {
		got := sc.Attr(0, tailAt+i)
		if got.Equal(sgr.Default()) {
			t.Fatalf("cell %d of the column's tail (%q) is DEFAULT — the span's reset ended the field's colour (nested-reset defect)\nrow: %q",
				i, tail[i:i+1], row)
		}
		// ... and it is the FIELD's style, not the span's: the emphasis must not
		// spread past the cells the producer measured.
		if got.Equal(sc.Attr(0, spanAt)) {
			t.Fatalf("cell %d of the tail wears the span's tier — the emphasis spread past the measured span\nrow: %q",
				i, row)
		}
	}
}

// TestStyledStatusLine_ColumnSpanIsTheOnlyCellThatChanges: the span wears the
// tier and its NEIGHBOURS do not. Without this, a renderer that styled the whole
// column with the tier would pass the tests above while colouring cells the
// producer never measured.
func TestStyledStatusLine_ColumnSpanIsTheOnlyCellThatChanges(t *testing.T) {
	tail := statusColumnEmphasisSuffix
	s := statusColumnEmphasisRow(tail)
	row := styledStatusLine(s, BudgetWidth, BasicPalette)
	plain := stripSGR(row)
	sc := columnRowGrid(t, row)
	spanAt := strings.Index(plain, "hit 68%")
	tier := sc.Attr(0, spanAt)
	for i := 0; i < len("hit 68%"); i++ {
		if got := sc.Attr(0, spanAt+i); !got.Equal(tier) {
			t.Fatalf("score cell %d does not carry the span's style:\nrow: %q", i, row)
		}
	}
	// The cell BEFORE the score belongs to the field: the column's text opens
	// under its own style, and the producer measured no emphasis before the span.
	if before := sc.Attr(0, spanAt); spanAt > 0 && sc.Attr(0, spanAt-1).Equal(before) {
		t.Fatalf("the cell before the score already wears the tier — the emphasis starts before the measured span\nrow: %q", row)
	}
}

// TestStyledStatusLine_SpanNotInTheColumnIsANoOp: the span is FOUND in the text
// the producer composed. A span that is not there is left alone — a guessed
// near-match would emphasise characters nobody measured, and the honest no-op is
// what keeps one composition of the field from disagreeing with itself.
func TestStyledStatusLine_SpanNotInTheColumnIsANoOp(t *testing.T) {
	missing := statusColumnEmphasisRow("")
	missing.Columns[0].EmphasisSpan = "hit 99%"
	got := styledStatusLine(missing, BudgetWidth, BasicPalette)

	cleared := statusColumnEmphasisRow("")
	cleared.Columns[0].EmphasisSpan = ""
	cleared.Columns[0].Emphasis = Style{}
	want := styledStatusLine(cleared, BudgetWidth, BasicPalette)
	if got != want {
		t.Fatalf("a span that is not in the column changed the row:\n got %q\nwant %q", got, want)
	}
}

// TestStyledStatusLine_MonochromeIsByteIdenticalWithAColumnEmphasis: with colour
// off the row is byte-identical to the historic rendering, emphasis or not. The
// comparison is against statusLine (the plain composition), byte for byte — the
// same assertion the pre-existing monochrome test makes for rows WITHOUT an
// emphasis, repeated here because a new channel is exactly what could break it.
func TestStyledStatusLine_MonochromeIsByteIdenticalWithAColumnEmphasis(t *testing.T) {
	for _, cols := range []int{0, 1, 5, 16, 24, 80, BudgetWidth} {
		s := statusColumnEmphasisRow(" 2k/1k")
		plain := statusLine(s, cols)
		if got := styledStatusLine(s, cols, MonoPalette); got != plain {
			t.Fatalf("monochrome row at %d cols = %q, want %q", cols, got, plain)
		}
		// The zero Style is the same promise from the other side: naming a span
		// while resolving NO tier must change nothing at all — the row is the one
		// a caller with no emphasis composes, byte for byte.
		zero := statusColumnEmphasisRow(" 2k/1k")
		zero.Columns[0].Emphasis = Style{}
		none := noEmphasisRow(zero)
		if got, want := styledStatusLine(zero, cols, BasicPalette), styledStatusLine(none, cols, BasicPalette); got != want {
			t.Fatalf("a zero-tier emphasis at %d cols emitted bytes:\n got %q\nwant %q", cols, got, want)
		}
	}
}

// noEmphasisRow is the same row with the span cleared, so a styled row can be
// compared against the composition a caller with no emphasis at all produces.
func noEmphasisRow(s Status) Status {
	for i := range s.Columns {
		s.Columns[i].EmphasisSpan = ""
		s.Columns[i].Emphasis = Style{}
	}
	return s
}

// TestStyledStatusLine_AlignedRowMatchesThePlainCells: the styled aligned path
// re-composes the row (it must — the column boundaries only exist in the
// producer's list) and this asserts what it re-composed is the SAME row the fit
// decided, cell for cell. A second composition that drifted would draw a row
// nobody measured, so the renderer REFUSES rather than draws (statusAlignedRow's
// own check) — and this test is what proves the refusal is not hit on a real row.
func TestStyledStatusLine_AlignedRowMatchesThePlainCells(t *testing.T) {
	states := []Status{
		statusColumnEmphasisRow(""),
		statusColumnEmphasisRow(" 2k/1k"),
		{
			Columns: []StatusColumn{
				{Text: "deepseek-flash", Width: 14},
				{Text: "iter 2", Width: 7},
				{Text: "tools off", Width: 16},
				{Text: "hit 68% \u21912k\u2194582", Width: 27, EmphasisSpan: "hit 68%", Emphasis: BasicPalette.StatusGood},
			},
			Live:       "24s \u00b7 ready",
			LiveWidth:  13,
			LayoutCols: BudgetWidth,
		},
	}
	for _, s := range states {
		plain := statusLine(s, BudgetWidth)
		styled := styledStatusLine(s, BudgetWidth, BasicPalette)
		if stripSGR(styled) != plain {
			t.Fatalf("styled row text differs from the plain row:\n got %q\nwant %q", stripSGR(styled), plain)
		}
		if displayWidth(plain) > BudgetWidth {
			t.Fatalf("the plain row is %d cells, over the budget %d: %q", displayWidth(plain), BudgetWidth, plain)
		}
		// The styled path really is the ALIGNED one for these rows (a refusal
		// would send the assertion above down the unaligned branch, where it would
		// pass while proving nothing about the column channel).
		if _, ok := statusAlignedRow(s, BudgetWidth, plain, BasicPalette); !ok {
			t.Fatalf("the aligned renderer refused a row it must handle: %q", plain)
		}
	}
}

// TestStyledStatusLine_AlignedRowKeepsTheFieldStyles: the aligned path styles
// each column as its identity calls for — a tool-activity column as tool
// activity, every other as the label — exactly as the unaligned path does through
// styledFields. Without this, re-composing the row could quietly change which
// emphasis the row's ordinary fields wear.
func TestStyledStatusLine_AlignedRowKeepsTheFieldStyles(t *testing.T) {
	s := Status{
		Columns: []StatusColumn{
			{Text: "model-x", Width: 14},
			{Text: ToolGlyph + " bash ls", Width: 26},
		},
		Live:       "ready",
		LiveWidth:  8,
		LayoutCols: BudgetWidth,
	}
	row := styledStatusLine(s, BudgetWidth, BasicPalette)
	plain := stripSGR(row)
	sc := columnRowGrid(t, row)
	modelAt := strings.Index(plain, "model-x")
	toolAt := strings.Index(plain, ToolGlyph)
	liveAt := strings.Index(plain, "ready")
	if modelAt < 0 || toolAt < 0 || liveAt < 0 {
		t.Fatalf("the fixture row is not composed as expected: %q", plain)
	}
	if sc.Attr(0, modelAt).Equal(sgr.Default()) {
		t.Errorf("the model column carries no label emphasis:\n%q", row)
	}
	if sc.Attr(0, toolAt).Equal(sc.Attr(0, modelAt)) {
		t.Errorf("the tool-activity column wears the label's emphasis, not its own:\n%q", row)
	}
	if sc.Attr(0, liveAt).Equal(sgr.Default()) {
		t.Errorf("the live hint lost its own style:\n%q", row)
	}
}

// TestStyledStatusLine_UnalignedPathHonoursTheLabelEmphasis: on a terminal too
// narrow for the aligned layout the row falls back to the StatusSep-separated
// label, and the producer's measured-quality span must still wear its tier
// there. Both paths re-state the span through ONE helper (wrapStatusField) — the
// alternative, styling only the aligned path, is exactly the silent non-effect
// this task exists to remove: the emphasis would work on a wide terminal and
// vanish on a narrow one, with nothing reporting the difference.
func TestStyledStatusLine_UnalignedPathHonoursTheLabelEmphasis(t *testing.T) {
	s := Status{
		Label:         "model-x" + StatusSep + "hit 68%",
		LabelEmphasis: "hit 68%",
		Emphasis:      BasicPalette.StatusBad,
		Live:          "ready",
	}
	// A width below the aligned pin, so statusLine takes the unaligned branch.
	cols := 40
	plain := statusLine(s, cols)
	if _, ok := statusAlignedRow(s, cols, plain, BasicPalette); ok {
		t.Fatalf("the fixture is not on the unaligned path: %q", plain)
	}
	styled := styledStatusLine(s, cols, BasicPalette)
	if stripSGR(styled) != plain {
		t.Fatalf("styling changed the unaligned row's TEXT:\n got %q\nwant %q", stripSGR(styled), plain)
	}
	sc := columnRowGrid(t, styled)
	at := strings.Index(plain, "hit 68%")
	if at < 0 {
		t.Fatalf("the score is not on the unaligned row: %q", plain)
	}
	spanAttr := sc.Attr(0, at)
	if spanAttr.Equal(sgr.Default()) {
		t.Fatalf("the unaligned row's span wears no style:\n%q", styled)
	}
	// The field BEFORE the span is the label's; the span's own tier must differ
	// from it, or this row would pass on a renderer that ignored the tier.
	if before := sc.Attr(0, strings.Index(plain, "model-x")); before.Equal(spanAttr) {
		t.Fatalf("the span does not differ from the label field:\n%q", styled)
	}
	// Monochrome stays byte-identical here too.
	if got := styledStatusLine(s, cols, MonoPalette); got != plain {
		t.Fatalf("monochrome unaligned row = %q, want %q", got, plain)
	}
}
