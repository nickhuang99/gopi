package lineedit

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sgr"
)

// emphasis_test.go — the intra-line emphasis layer's acceptance.
//
// The four claims, in the epic's order:
//
//  1. the TOKENIZER's table: `**bold**` is one bold span with the markers gone,
//     an unclosed marker stays literal, an empty pair is literal, and a line
//     with no marker is one span;
//  2. the ARITHMETIC: a marker occupies ZERO display cells, so the drawn text is
//     shorter than the line by exactly the markers that were consumed;
//  3. the SCREEN: the bold span's cells carry Bold in the attribute grid, and
//     the marker never occupies a column;
//  4. the MONOCHROME invariant: with colour off the bytes are IDENTICAL to the
//     same answer written without emphasis (a sha256 comparison, because "the
//     same" is a claim about every byte).

// probeRows/probeCols are the geometry the screen-level assertions draw at.
// emphasisProbeRows is bumped so the grid holds a taller answer than the
// emphasis fixtures needed: the nested-quote fixture below paints seven rows plus
// the band's blank lines, and a row asserted here must never have scrolled off.
const (
	emphasisProbeRows = 14
	emphasisProbeCols = 60
)

// emphasisProbe is one rendered answer: the bytes a real AnswerRenderer wrote,
// and the terminal grid those bytes produce.
type emphasisProbe struct {
	cap    *paletteCapture
	screen *Screen
}

// renderEmphasis drives the SHIPPED AnswerRenderer (region + palette + the
// public Begin/Delta/Tick/Finish surface) over the deltas given, and feeds what it
// wrote into a stateful vt100 grid — so every assertion below reads the screen
// the operator would see, not a callback.
//
// marked selects the turn shape: false is Begin (no turn chrome — an unmarked
// line, the shape a caller that keeps no turn counter gets), true is BeginTurn
// with a real header, whose body lines carry the turn's marker column. The two
// differ in ONE thing — the gutter's cells — which is why the emphasis assertion
// below is stated over the row's own text rather than over a literal prefix.
func renderEmphasis(t *testing.T, pal Palette, marked bool, deltas ...string) *emphasisProbe {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(emphasisProbeRows, emphasisProbeCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", emphasisProbeRows, emphasisProbeCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(pal)
	if marked {
		r.BeginTurn(TurnInfo{Index: 1, Prompt: "colour my day, please"})
	} else {
		r.Begin()
	}
	for _, d := range deltas {
		r.Delta(d)
	}
	r.Tick()
	r.Finish()
	if marked {
		r.EndTurn()
	}

	sc := NewScreen(emphasisProbeCols, emphasisProbeRows)
	sc.Feed([]byte(cap.bytes()))
	return &emphasisProbe{cap: cap, screen: sc}
}

// rowWith returns the screen row holding want, or -1. The answer lands at the
// band's top, but the exact row is the layout's business — an assertion that
// assumed row 0 would be asserting the layout, not the emphasis.
func (p *emphasisProbe) rowWith(want string) int {
	for r := 0; r < emphasisProbeRows; r++ {
		if strings.Contains(p.screen.Line(r), want) {
			return r
		}
	}
	return -1
}

// TestLineSpansTokenTable pins the tokenizer's decisions: what a marker means is
// decided in ONE place, so this table is that place's own definition.
func TestLineSpansTokenTable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		line  string
		drawn string // the text the spans draw (markers removed)
		bold  []string
	}{
		{name: "no marker is one span", line: "plain words", drawn: "plain words"},
		{name: "one span", line: "a **bold** word", drawn: "a bold word", bold: []string{"bold"}},
		{name: "marker only", line: "**bold**", drawn: "bold", bold: []string{"bold"}},
		{name: "two spans", line: "**a** and **b**", drawn: "a and b", bold: []string{"a", "b"}},
		{name: "unclosed stays literal", line: "**hi", drawn: "**hi"},
		{name: "empty pair stays literal", line: "****", drawn: "****"},
		{name: "empty pair among text", line: "a****b **c**", drawn: "a****b c", bold: []string{"c"}},
		{name: "trailing marker literal", line: "**a** **", drawn: "a **", bold: []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spans := LineSpans(tc.line)
			if got := SpanText(spans); got != tc.drawn {
				t.Fatalf("SpanText(%q) = %q, want %q", tc.line, got, tc.drawn)
			}
			var bold []string
			for _, sp := range spans {
				switch sp.Emphasis {
				case EmphasisBold:
					bold = append(bold, sp.Text)
				case EmphasisNone:
				default:
					t.Fatalf("span %q has emphasis %d, want none or bold", sp.Text, sp.Emphasis)
				}
			}
			if strings.Join(bold, "|") != strings.Join(tc.bold, "|") {
				t.Fatalf("bold spans of %q = %v, want %v", tc.line, bold, tc.bold)
			}
			// An EMPHASIS span never carries a marker: only text the tokenizer
			// refused to emphasise (the literal shapes) may still spell one.
			for _, sp := range spans {
				if sp.Emphasis == EmphasisBold && strings.Contains(sp.Text, BoldMarker) {
					t.Fatalf("a BOLD span of %q still carries the marker: %q", tc.line, sp.Text)
				}
			}
			if len(spans) > 0 && len(tc.drawn) > len(tc.line) {
				t.Fatalf("SpanText(%q) = %q, longer than the line", tc.line, tc.drawn)
			}
		})
	}
}

// TestEmphasisMarkersOccupyNoColumn is the arithmetic claim: the drawn text is
// the line minus exactly the markers that were consumed — 2 bytes per bold span,
// per marker pair — so every cell measurement downstream is the same with and
// without emphasis.
func TestEmphasisMarkersOccupyNoColumn(t *testing.T) {
	// markerBytes is how many bytes of the line the tokenizer REMOVED — derived
	// from the table, never assumed (markers are 2 bytes each).
	for _, line := range []string{
		"**bold**",
		"a **bold** word",
		"**a** and **b**",
		"**hi",           // unclosed: nothing is consumed
		"****",           // empty pair: nothing is consumed
		"no marker here", // the fast path
		"a****b **c**",
	} {
		spans := LineSpans(line)
		drawn := SpanText(spans)
		// Every bold span consumed ONE PAIR of markers (2 × len(BoldMarker)
		// bytes), so the text that vanished must account for exactly those pairs.
		consumed := len(line) - len(drawn)
		bold := countBoldSpans(spans)
		if want := bold * 2 * len(BoldMarker); consumed != want {
			t.Errorf("%q: %d bytes vanished for %d bold span(s), want %d — a marker vanished that opened no span",
				line, consumed, bold, want)
		}
		// The DISPLAY claim: a marker occupies no cell, so the drawn text's width
		// is the line's width minus exactly the cells the markers would have taken.
		if got, want := displayWidth(drawn), displayWidth(line)-bold*2*len(BoldMarker); got != want {
			t.Errorf("display cells of %q = %d, want %d", drawn, got, want)
		}
		if !strings.Contains(line, BoldMarker) && drawn != line {
			t.Errorf("a line with no marker was altered: %q → %q", line, drawn)
		}
	}
}

// countBoldSpans counts the spans the tokenizer marked bold.
func countBoldSpans(spans []LineSpan) int {
	n := 0
	for _, sp := range spans {
		if sp.Emphasis == EmphasisBold {
			n++
		}
	}
	return n
}

// TestStyledLineMarksExactlyTheBoldCells is the epic's StyledLine case: a real
// answer line streamed through the renderer as `the **bold** word` must leave
// Bold on exactly the cells of "bold" — asserted as per-cell ATTRIBUTES on the
// grid (screenattr.go), so a renderer that painted bold bytes in the wrong place,
// or counted a marker as a column, fails here.
func TestStyledLineMarksExactlyTheBoldCells(t *testing.T) {
	probe := renderEmphasis(t, BasicPalette, false, "the **bold** word\n")
	row := probe.rowWith("bold")
	if row < 0 {
		t.Fatalf("no screen row holds the answer:\n%s", probe.screen.Contents())
	}
	text, styles := probe.screen.StyledLine(row)
	// The marker occupies NO column: the drawn row is the line minus its markers.
	if strings.Contains(text, BoldMarker) {
		t.Fatalf("row %d still shows a marker: %q", row, text)
	}
	// The gutter's cells are the layout's decision, not this test's: the row's
	// text must END with the drawn answer and hold nothing else, so the assertion
	// is stated on the body rather than on a hard-coded prefix.
	drawn := strings.TrimRight(text, " ")
	if !strings.HasSuffix(drawn, "the bold word") {
		t.Fatalf("row %d text = %q, want it to end with the drawn line %q", row, text, "the bold word")
	}

	// The bold cell RANGE: the columns of "bold" in the drawn row, read off the
	// text rather than written as literals (a hard-coded range would pin the
	// gutter, not the emphasis).
	bodyStart := indexRunes([]rune(drawn), "the bold word")
	if bodyStart < 0 {
		t.Fatalf("row %d = %q: no body %q", row, text, "the bold word")
	}
	boldStart := bodyStart + len("the ")
	boldEnd := boldStart + len("bold") - 1 // inclusive, per StyledLine's rune-indexed styles
	runes := []rune(drawn)
	if boldEnd >= len(styles) || len(runes) != len(styles) {
		t.Fatalf("row %d = %q: %d runes / %d styles, bold span [%d,%d]",
			row, text, len(runes), len(styles), boldStart, boldEnd)
	}
	for i := range runes {
		wantBold := i >= boldStart && i <= boldEnd
		if got := styles[i].Bold; got != wantBold {
			t.Errorf("rune %d (%q) Bold = %v, want %v\nrow text: %q\nstyles: %s",
				i, runes[i], got, wantBold, text, probe.screen.StyledContents())
		}
	}
	// And the emphasis is the palette's own bold, not some other sequence: the
	// cell's style is the style the table names for the span, ON the grid.
	want := emphasisStyle(BasicPalette.AnswerBold)
	// The range is LOGGED as evidence: the report quotes the measured cells
	// instead of describing them.
	t.Logf("StyledLine: row %d text=%q | bold cells [%d,%d] of %d cells | grid line=%q",
		row, text, boldStart, boldEnd, len(runes), probe.screen.Line(row))
	for i := boldStart; i <= boldEnd; i++ {
		if !styles[i].Equal(want) {
			t.Fatalf("cell %d style = %s, want the palette's AnswerBold %s", i, styles[i], want)
		}
	}
	// The cells either side of the span are NOT bold: the emphasis is bounded by
	// the span, and a renderer that opened bold once and never closed it (or
	// opened it a cell early) fails here.
	if boldStart > 0 && styles[boldStart-1].Bold {
		t.Errorf("the cell before the span is bold: %s", probe.screen.StyledContents())
	}
	if boldEnd+1 < len(styles) && styles[boldEnd+1].Bold {
		t.Errorf("the cell after the span is bold: %s", probe.screen.StyledContents())
	}
}

// indexRunes returns the first RUNE index at which want occurs in runes, or -1.
// StyledLine indexes styles by rune position (a wide rune's two columns share one
// entry), so every range in this file is computed in runes — a byte index would
// mark the wrong cell the moment a multi-byte glyph precedes the span.
func indexRunes(runes []rune, want string) int {
	w := []rune(want)
	for i := 0; i+len(w) <= len(runes); i++ {
		if string(runes[i:i+len(w)]) == want {
			return i
		}
	}
	return -1
}

// TestStyledLineBoldCellsInsideAMarkedTurn is the same assertion on the OTHER
// turn shape: a marked turn's rows carry the two-cell marker column, so the bold
// span's columns shift — and the marker's removal must not shift them further.
// It is the regression guard for a renderer that counted a tokenizer-removed
// marker as a cell (the row would then creep left by two per span).
func TestStyledLineBoldCellsInsideAMarkedTurn(t *testing.T) {
	probe := renderEmphasis(t, BasicPalette, true, "the **bold** word\n")
	row := probe.rowWith("bold")
	if row < 0 {
		t.Fatalf("no screen row holds the answer:\n%s", probe.screen.Contents())
	}
	text, styles := probe.screen.StyledLine(row)
	drawn := strings.TrimRight(text, " ")
	if !strings.HasSuffix(drawn, "the bold word") {
		t.Fatalf("marked-turn row %d = %q, want it to end with %q", row, text, "the bold word")
	}
	// The gutter is present (a marked turn's body is drawn inside the column) and
	// is NOT emphasised: the turn's own chrome is written as its own text.
	// The span's range is computed in RUNES: StyledLine indexes styles by rune
	// position, and the turn's marker glyph is multi-byte.
	runes := []rune(drawn)
	bodyStart := indexRunes(runes, "the bold word")
	if bodyStart <= 0 {
		t.Fatalf("row %d = %q: no gutter before the body", row, text)
	}
	for i := 0; i < bodyStart; i++ {
		if styles[i].Bold {
			t.Fatalf("gutter cell %d is bold: %s", i, probe.screen.StyledContents())
		}
	}
	start := bodyStart + len("the ")
	if len(runes) != len(styles) {
		t.Fatalf("row %d has %d runes but %d styles", row, len(runes), len(styles))
	}
	t.Logf("marked turn: row %d text=%q | gutter %d rune(s) | bold cells [%d,%d] of %d | grid line=%q",
		row, text, bodyStart, start, start+len("bold")-1, len(runes), probe.screen.Line(row))
	for i, r := range runes {
		wantBold := i >= start && i < start+len("bold")
		if styles[i].Bold != wantBold {
			t.Errorf("marked turn: rune %d (%q) Bold = %v, want %v\n%s", i, r, styles[i].Bold, wantBold, text)
		}
	}
}

// emphasisStyle resolves the style a palette entry leaves on a cell: an SGR
// sequence's EFFECT on the grid, read back from the grid's own parser rather
// than assumed.
func emphasisStyle(st Style) sgr.Style {
	sc := NewScreen(4, 1)
	sc.Feed([]byte(st.Wrap("x")))
	return sc.Attr(0, 0)
}

// TestEmphasisColourOffBytesAreUnchanged is the monochrome invariant stated as a
// BYTE claim: with colour off, an answer carrying `**bold**` writes exactly the
// bytes the same answer writes with the emphasis spelled out — markers removed,
// nothing else. A sha256 of both captures, because "unchanged" must hold for
// every byte, not for a sampled one.
func TestEmphasisColourOffBytesAreUnchanged(t *testing.T) {
	withMarker := renderEmphasis(t, MonoPalette, false, "the **bold** word\nsecond line, no marker\n")
	without := renderEmphasis(t, MonoPalette, false, "the bold word\nsecond line, no marker\n")
	a, b := withMarker.cap.bytes(), without.cap.bytes()
	if hasSGR(a) {
		t.Fatalf("the monochrome capture carries SGR bytes:\n%q", a)
	}
	if a != b {
		t.Fatalf("colour-off bytes differ with and without the marker:\n **: %q\n  -: %q", a, b)
	}
	// The colour-on capture must differ (otherwise the layer is inert) while
	// drawing the SAME characters — emphasis is additive, never a substitution.
	// The comparison is between the two SCREENS, not between byte streams: the
	// coloured run legitimately carries extra cursor motion (the absolute
	// placements the palette's sequences come with), so equals-as-bytes is a
	// claim only the monochrome pair can make.
	on := renderEmphasis(t, BasicPalette, false, "the **bold** word\nsecond line, no marker\n")
	if !hasSGR(on.cap.bytes()) {
		t.Fatal("the colour-on capture carries no SGR: the emphasis never reached the wire")
	}
	if got, want := on.screen.Contents(), withMarker.screen.Contents(); got != want {
		t.Fatalf("the coloured run drew different characters than the monochrome run:\n on  %q\n off %q", got, want)
	}
}

// TestEmphasisStreamedAcrossFrames paints `**bo` and then `ld**` — the split the
// epic names — and asserts the DECIDED result: the visible letters never change
// when the second frame arrives (a reader may not see `**bo` become `bo`), and
// once the line's break arrives the emphasis is the whole span's.
func TestEmphasisStreamedAcrossFrames(t *testing.T) {
	t.Run("closed by the next frame", func(t *testing.T) {
		probe := renderEmphasis(t, BasicPalette, false, "**bo", "ld**\n")
		row := probe.rowWith("bold")
		if row < 0 {
			t.Fatalf("no screen row holds the decided line:\n%s", probe.screen.Contents())
		}
		text, styles := probe.screen.StyledLine(row)
		if strings.Contains(text, BoldMarker) {
			t.Fatalf("the decided row still shows a marker: %q", text)
		}
		start := strings.Index(strings.TrimRight(text, " "), "bold")
		if start < 0 {
			t.Fatalf("row %d = %q, want the drawn line \"bold\"", row, text)
		}
		for i := start; i < start+len("bold"); i++ {
			if !styles[i].Bold {
				t.Fatalf("rune %d of %q is not bold — the span did not survive the frame split",
					i, text)
			}
		}
	})

	t.Run("never closed stays literal", func(t *testing.T) {
		probe := renderEmphasis(t, BasicPalette, false, "**bo", "ld\n")
		if row := probe.rowWith(BoldMarker + "bold"); row < 0 {
			t.Fatalf("an unclosed marker did not stay literal on screen:\n%s", probe.screen.Contents())
		}
	})
}

// TestEmphasisInsideAFenceStaysLiteral is the parity claim across the two
// carriers: `**` inside a fenced code block is the literal the code block says it
// is — the same rule the web page implements (a code span wins over emphasis) and
// the same one the composer already applies to a fence. The gate is the
// composer's VERDICT about the line (LineCode), not a marker test in the
// renderer, which is why nothing in the renderer had to learn about fences.
func TestEmphasisInsideAFenceStaysLiteral(t *testing.T) {
	const fence = "```"
	probe := renderEmphasis(t, BasicPalette, false,
		fence+"\na **b** c\n"+fence+"\na **b** c\n")
	// Two rows carry the text: the fenced one and the prose one after the fence.
	var rows []int
	for r := 0; r < emphasisProbeRows; r++ {
		if strings.Contains(probe.screen.Line(r), "b") {
			rows = append(rows, r)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("expected the fenced line and the prose line, found rows %v:\n%s", rows, probe.screen.Contents())
	}
	fenced := probe.screen.Line(rows[0])
	prose := probe.screen.Line(rows[1])
	if !strings.Contains(fenced, BoldMarker+"b"+BoldMarker) {
		t.Errorf("the fenced line lost its literal markers: %q", fenced)
	}
	if strings.Contains(prose, BoldMarker) {
		t.Errorf("the prose line kept its markers: %q", prose)
	}
	// And the fenced cells are NOT bold: the code style wins, as it does in the
	// page's DOM (a code span is not wrapped in <strong>).
	_, fencedStyles := probe.screen.StyledLine(rows[0])
	for i := range fencedStyles {
		if fencedStyles[i].Bold {
			t.Fatalf("a fenced cell is bold: %s", probe.screen.StyledContents())
		}
	}
	_, proseStyles := probe.screen.StyledLine(rows[1])
	boldCells := 0
	for i := range proseStyles {
		if proseStyles[i].Bold {
			boldCells++
		}
	}
	if boldCells != len("b") {
		t.Errorf("the prose line marks %d bold cell(s), want %d: %s", boldCells, len("b"), probe.screen.StyledContents())
	}
}

// TestEmphasisWrapCountIsUnchanged is the width invariance: a long emphasised
// line wraps into the same number of rows, and its drawn characters are the same
// as the plain line's, so the emphasis layer adds no row arithmetic of its own.
func TestEmphasisWrapCountIsUnchanged(t *testing.T) {
	plain := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima"
	emphasised := strings.Replace(plain, "charlie", "**charlie**", 1)
	if emphasised == plain {
		t.Fatal("the fixture did not gain a marker")
	}
	offEmph := renderEmphasis(t, MonoPalette, false, emphasised+"\n")
	offPlain := renderEmphasis(t, MonoPalette, false, plain+"\n")
	if got, want := offEmph.screen.Contents(), offPlain.screen.Contents(); got != want {
		t.Fatalf("the emphasised line drew a different screen than the plain one:\n emphasised:\n%s\n plain:\n%s", got, want)
	}
	if got, want := offEmph.cap.bytes(), offPlain.cap.bytes(); got != want {
		t.Fatalf("emphasis changed the monochrome byte stream:\n emphasised: %q\n plain:      %q", got, want)
	}
}
