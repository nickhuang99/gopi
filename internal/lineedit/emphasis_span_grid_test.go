package lineedit

import (
	"strings"
	"testing"
	"time"
)

// emphasis_span_grid_test.go — the INTRA-LINE SPAN's cells on the screen, at the
// operator's own width.
//
// The defect this file guards (measured 2026-09-29 on a live 117-column chat):
// the bold run a `**…**` span produces does NOT cover the span's characters — its
// start is early, its end late, and the boundary lands in the middle of a
// multi-byte character. The shape that makes it reachable is a STREAM: the answer
// arrives in deltas, so a delta boundary can fall INSIDE a marker pair (`**b` |
// `**`), and the row that is finally committed must still carry the span's
// emphasis on exactly the span's cells.
//
// WHY THE ASSERTION IS PER-CELL. A string comparison on the row cannot see this:
// the row's characters are right, and it is their ATTRIBUTES that are shifted. So
// every case below reads the terminal grid (Screen.StyledLine) and compares, rune
// by rune, the drawn text AND each rune's Bold flag against the spans the format
// layer decided (LineSpans) — the grid is the truth the operator reported from.
//
// The width is the operator's 117 columns, never a convenient one.

const (
	// spanGridCols is the operator's terminal width (the live capture that
	// produced this report read `cols=117`).
	spanGridCols = 117
	// spanGridRows holds the whole fixture plus the band's trailing blanks, so a
	// row that is asserted here has never scrolled off.
	spanGridRows = 40
	// spanGridTurnPrompt is the turn header's prompt. The turn's chrome (a header
	// row, a rule, and the two-cell gutter) is part of the geometry under test:
	// the gutter is what makes a wrapped row re-open the block's column.
	spanGridTurnPrompt = "span grid probe"
)

// spanGridCells is the answer as a flat cell sequence: the runes and, per rune,
// whether the terminal drew it Bold. Both sides of the comparison are this shape,
// so the two are compared rune by rune rather than as strings.
type spanGridCells struct {
	runes []rune
	bold  []bool
}

func (c *spanGridCells) add(r rune, bold bool) {
	c.runes = append(c.runes, r)
	c.bold = append(c.bold, bold)
}

func (c spanGridCells) text() string { return string(c.runes) }

// spanGridExpect is the expected side, built from the source text through the
// LIVE tokenizer — never from a hand-written literal: a test that spelled the bold
// run itself could agree with a renderer that shifted it.
func spanGridExpect(src string) spanGridCells {
	var w spanGridCells
	for _, line := range strings.Split(strings.TrimSuffix(src, "\n"), "\n") {
		for _, sp := range LineSpans(line) {
			for _, r := range sp.Text {
				w.add(r, sp.Emphasis == EmphasisBold)
			}
		}
	}
	return w
}

// isSpanGridRule reports whether a row is the turn's chrome — its header row
// (dashes with the turn's label inside them) or its closing rule. Neither carries
// body cells, so the cell walk skips both.
func isSpanGridRule(text string) bool { return strings.HasPrefix(text, "─") }

// spanGridDrawn is the answer as the SCREEN holds it. The turn's chrome (the
// header, the rule, the two-cell gutter the band draws at a body row's left edge)
// is folded away, and so is the CUT LABEL: `…` is a RENDERING glyph standing in
// the cell of a character that is drawn on the row below, so folding it away is
// what makes the drawn cells comparable with the source's own characters
// (cutReadingFold — the same fold the cut-marker family uses).
func spanGridDrawn(sc *Screen, rows int, marked bool) spanGridCells {
	var g spanGridCells
	for row := 0; row < rows; row++ {
		text, styles := sc.StyledLine(row)
		if text == "" || isSpanGridRule(text) || strings.HasPrefix(text, "▸ ") {
			continue
		}
		runes := []rune(text)
		body := 0
		if marked {
			// A marked turn's body row opens with the block's two-cell gutter: the
			// glyph on the line's first row, blank cells on every row the line
			// WRAPS onto. Both are chrome, not body cells.
			body = displayWidth(TurnMarker)
		}
		for i := body; i < len(runes); i++ {
			r := runes[i]
			if r == []rune(answerCutLabel)[0] {
				continue
			}
			g.add(r, styles[i].Bold)
		}
	}
	return g
}

// spanGridScreen drives the SHIPPED renderer over src at the operator's width in
// deltas of `chunk` bytes and returns the resulting terminal grid. The deltas are
// separated by more than answerFlushWindow, so the cadence flush paints an
// unfinished line exactly as a real stream does — which is what puts a marker pair
// across a delta boundary.
func spanGridScreen(t *testing.T, src string, marked bool, chunk int) *Screen {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(spanGridRows, spanGridCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", spanGridRows, spanGridCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(BasicPalette)
	if marked {
		r.BeginTurn(TurnInfo{Index: 1, Prompt: spanGridTurnPrompt})
	} else {
		r.Begin()
	}
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	if !strings.HasSuffix(src, "\n") {
		src += "\n"
	}
	for i := 0; i < len(src); i += chunk {
		end := i + chunk
		if end > len(src) {
			end = len(src)
		}
		now = now.Add(emphasisChunkGap)
		r.Delta(src[i:end])
	}
	r.Tick()
	r.Finish()
	sc := NewScreen(spanGridCols, spanGridRows)
	sc.Feed([]byte(cap.bytes()))
	return sc
}

// spanGridCompare asserts the grid against the format layer's decision, rune by
// rune and flag by flag, and reports the two failures apart: a row whose
// CHARACTERS are right and whose ATTRIBUTES are shifted is the defect this file
// exists for, and one "mismatch" message cannot say which of the two moved.
//
// The walk tolerates exactly ONE difference between the source and the drawn
// cells: a wrap may CONSUME the space it breaks at (trimWrapSpace — the block's
// gutter is drawn where that space would have been), so a space the source has and
// the grid does not is skipped. Anything else — a missing character, a literal
// `**`, a character drawn twice — is a failure, and it is a TEXT failure; a cell
// that is Bold where the span is not (or the reverse) is a BOLD failure.
func spanGridCompare(t *testing.T, name, src string, marked bool, chunk int) {
	t.Helper()
	want := spanGridExpect(src)
	got := spanGridDrawn(spanGridScreen(t, src, marked, chunk), spanGridRows, marked)
	wantRunes, gotRunes := []rune(strings.TrimRight(want.text(), " ")), []rune(strings.TrimRight(got.text(), " "))
	var cells []spanGridCells
	cells = append(cells, spanGridCells{runes: wantRunes, bold: want.bold})
	p := 0
	for i, r := range gotRunes {
		for p < len(wantRunes) && wantRunes[p] == ' ' && wantRunes[p] != r {
			p++
		}
		if p >= len(wantRunes) || wantRunes[p] != r {
			t.Fatalf("%s: marked=%v chunk=%d: the grid's cell %d is %q, which the source does not have at that point —\n  grid  %q\n  want  %q\n  want[%d:]=%q",
				name, marked, chunk, i, string(r), string(gotRunes), string(wantRunes), p, string(wantRunes[p:]))
		}
		if got.bold[i] != want.bold[p] {
			t.Fatalf("%s: marked=%v chunk=%d: %q at grid cell %d is Bold=%v, want %v — the span's cells are shifted\n  grid %q\n  want %q",
				name, marked, chunk, string(r), i, got.bold[i], want.bold[p], string(gotRunes), string(wantRunes))
		}
		p++
	}
	for p < len(wantRunes) {
		if wantRunes[p] != ' ' {
			t.Fatalf("%s: marked=%v chunk=%d: the grid DRAWED %q, which stops %d cell(s) before the source ends —\n  grid %q\n  want %q",
				name, marked, chunk, string(gotRunes), len(wantRunes)-p, string(gotRunes), string(wantRunes))
		}
		p++
	}
	_ = cells
}

// spanGridChunks are the delta sizes the cases are swept over. They cross the
// marker pair (`**` is two bytes) at every parity, and they include sizes larger
// than a whole short line, so a case cannot pass merely because the whole line
// arrived at once.
var spanGridChunks = []int{1, 2, 3, 5, 7, 11, 17, 32, 64}

// The two fillers differ in BYTES PER CELL (ASCII is 1 byte per cell, the CJK
// filler 3), so a ledger that confuses bytes with cells drifts by a different
// amount in each.
const (
	spanGridASCIIFiller = "the quick brown fox jumps over the lazy dog and keeps going "
	spanGridCJKFiller   = "这一段中文答案用来检查换行之后的列是否对齐，继续填充"
)

// TestEmphasisSpanCellsMatchTheSourceAtTheOperatorWidth is the epic's case: at 117
// columns, every `**…**` span's cells carry Bold and no other cell does, asserted
// per cell against the tokenizer's own spans.
//
// The fixtures are the shapes the operator reported, plus the streaming split that
// makes the shift reachable: pure ASCII emphasis, a CJK emphasis run, a CJK line
// carrying inline code, long lines that WRAP in both byte-per-cell regimes, and
// answers whose emphasis pair is cut by a delta boundary.
func TestEmphasisSpanCellsMatchTheSourceAtTheOperatorWidth(t *testing.T) {
	cases := []struct{ name, src string }{
		{"ascii bold", "plain ascii **bold** tail"},
		{"cjk bold", "**粗体中文**收尾"},
		{"cjk bold with inline code", "- **粗体中文**（`code`）尾"},
		{"ascii wrap", strings.Repeat(spanGridASCIIFiller, 3) + "**bold runs across the wrap boundary here**" + spanGridASCIIFiller},
		{"cjk wrap", strings.Repeat(spanGridCJKFiller, 4) + "**跨过折行边界的粗体段**" + strings.Repeat(spanGridCJKFiller, 2)},
		{"cjk wrap with inline code", strings.Repeat(spanGridCJKFiller, 3) + "**粗体**（`code`）" + strings.Repeat(spanGridCJKFiller, 3)},
		{"marker split across deltas", "a\n**b**"},
		{"marker split, cjk", "证据到手了。**这是第一段粗体。**\n**\"截图\"这个词，被从中间切开，两个颜色。**"},
	}
	for _, tc := range cases {
		for _, marked := range []bool{false, true} {
			for _, chunk := range spanGridChunks {
				t.Run(tc.name+"/chunk="+itoa(chunk)+"/"+spanGridShapeName(marked), func(t *testing.T) {
					spanGridCompare(t, tc.name, tc.src, marked, chunk)
				})
			}
		}
	}
}

// spanGridShapeName names the two turn shapes (only one draws the gutter).
func spanGridShapeName(marked bool) string {
	if marked {
		return "marked=gutter"
	}
	return "marked=plain"
}
