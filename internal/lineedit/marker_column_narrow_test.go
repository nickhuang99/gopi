package lineedit

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// marker_column_narrow_test.go — the marker column's CELL-WIDTH discipline at a
// NARROW width, with mixed ASCII + CJK content and a fenced block in the same
// turn.
//
// The operator's dump (/tmp/error.txt, 117 cells wide) shows three symptoms in
// one committed turn: rows of the turn that open OUTSIDE the marker column
// while their neighbours carry `▌ `, a ragged left edge on the rows a CJK line
// wraps onto, and a fenced block whose content carries the `▌ ` prefix. All
// three are the same column arithmetic, so they are asserted HERE, on the cell
// grid, at a geometry narrow enough that the marker column and the wrap both
// bite.
//
// THE CONTRACT (stated once, asserted by every case below): inside a committed
// turn, every non-blank body row OPENS AT EXACTLY displayWidth(TurnMarker)
// cells — the marker GLYPH on a line of the turn, and that same number of cells
// of BLANK on a row the line wrapped onto. Not "at least": a row that opens at
// cell 0 has silently left the turn, and a row that opens one cell further in
// breaks the block's left edge into a staircase (`▌ x` / `   y` / `  z`), which
// is what a reader sees as a ragged column.
//
// WHY THE TEXT IS ASSERTED TOO. A row can be at the right column and have lost
// characters, so each case also reconstructs the body from the grid (the marker
// column stripped) and compares it with the text the turn was given. Columns
// without text is a screenshot that looks right.

const (
	// The geometry is the turn harness's own (turnHarnessRows × turnHarnessCols,
	// turn_layout_test.go): a NARROW band — 40 cells is wide enough for the
	// marker column and a cell of content (lineGutter needs more than
	// displayWidth(TurnMarker)+minGutterContentCells) and narrow enough that a
	// CJK line wraps three or four times. The width is taken from that ONE
	// definition rather than spelled again here (Rule 13).
	narrowMarkerRows = turnHarnessRows
	narrowMarkerCols = turnHarnessCols

	// markerCJKWrapCols is the width at which the CJK paragraph below wraps with
	// a SPACE at the row's tail — the shape that made the continuation row open
	// one cell too far in (see TestMarkerColumn_WrappedCJKRowKeepsTheExactColumn).
	markerCJKWrapCols = 56
)

// markerCJKParagraph is the content every case streams: one line of CJK with
// ASCII words and an inline code span inside it. It is longer than every width
// used here on purpose — the wrap is what the case is about.
const markerCJKParagraph = "这一段中文答案用来检查换行之后的列是否对齐，中间夹着 ASCII token 和 code 片段，然后再来一段中文以确保换行发生多次，最后以句号结束。"

// terminalNewline is the break that ends a line of the answer.
const terminalNewline = "\n"

// markerCJKCodeLine is the CJK line INSIDE the fenced block: it wraps too, so
// the block's own continuation row is on the grid.
const markerCJKCodeLine = "这是一行很长的中文代码内容，用来在窄列里强制换行，看看续行有没有标记。"

// markerProbe is ONE turn driven through the real renderer, plus the band rows
// it produced (0-based, bottom band row excluded) and the body's row range.
type markerProbe struct {
	h     *a5Harness
	lines []string
	// first..last bound the turn's body inside lines (the rows between the turn
	// header and the closing rule).
	first, last int
}

// renderMarkerTurn streams one turn's deltas — each as its own burst, which is
// what a real token stream does — and reads the grid back.
func renderMarkerTurn(t *testing.T, cols, rows int, prompt string, deltas ...string) markerProbe {
	t.Helper()
	h := newA5HarnessSized(t, nil, nil, rows, cols)
	h.answer.SetCols(cols)
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: prompt})
	for _, d := range deltas {
		h.answer.Delta(d)
	}
	h.answer.Finish()
	h.answer.EndTurn()
	h.replay()

	lines := bandLines(h, h.rowsTo(h.region.Plan().BandTop, h.region.Plan().BandBottom))
	p := markerProbe{h: h, lines: lines, first: -1, last: -1}
	header := rowOf(lines, turnHeaderText(1, prompt))
	if header < 0 {
		t.Fatalf("no turn header on the grid\n--- band ---\n%s", strings.Join(lines, "\n"))
	}
	for r := skipRequestRows(lines, header+1, prompt, cols); r < len(lines); r++ {
		if strings.TrimSpace(lines[r]) == strings.Repeat(TurnRuleGlyph, cols) {
			p.last = r - 1
			break
		}
		if p.first < 0 {
			p.first = r
		}
	}
	if p.last < p.first {
		t.Fatalf("the turn's closing rule was not found on the grid\n--- band ---\n%s", strings.Join(lines, "\n"))
	}
	return p
}

// runsInTurn are the body rows after the turn's opening row: the rows the line
// wrapped onto, and the fenced block's rows. They are the rows the column claim
// is about, so a case that has none has not been exercised.
func (p markerProbe) runsInTurn() []int {
	var out []int
	for r := p.first + 1; r <= p.last; r++ {
		if strings.TrimSpace(p.lines[r]) != "" {
			out = append(out, r)
		}
	}
	return out
}

// dump is the failure message: every band row with its cell width, the cell its
// text starts at, and the text — the three numbers the claim is made of.
func (p markerProbe) dump() string {
	var b strings.Builder
	for i, l := range p.lines {
		mark := "  "
		if i >= p.first && i <= p.last {
			mark = "->"
		}
		fmt.Fprintf(&b, "%s %2d [cells=%3d textstarts=%d] %q\n",
			mark, i, displayWidth(l), displayWidth(l)-displayWidth(strings.TrimLeft(l, " ")), l)
	}
	return b.String()
}

// fencedRows is the half-open range [first, last] of grid rows that lie inside a
// fenced block, read off the block's own delimiters; {0, -1} when the turn has no
// block (the range is then empty and every row is claimed).
func (p markerProbe) fencedRows() [2]int {
	from, to := 0, -1
	for r := p.first; r <= p.last; r++ {
		if !strings.HasPrefix(strings.TrimSpace(p.lines[r]), FenceMarker) {
			continue
		}
		if to < p.first {
			from = r
		}
		to = r
	}
	if to < p.first {
		return [2]int{0, -1}
	}
	return [2]int{from, to}
}

// blockColumn is the cell a body row must open at: the marker column's own
// cell width, never a second number (Rule 14 — displayWidth of the marker).
func blockColumn() int { return displayWidth(TurnMarker) }

// bodyText reconstructs the turn's body from the grid: the marker column is
// stripped from each row and the rows are concatenated, exactly as a reader
// reading the block would reassemble them.
func (p markerProbe) bodyText() string {
	var b strings.Builder
	block := p.fencedRows()
	for r := p.first; r <= p.last; r++ {
		l := p.lines[r]
		if strings.TrimSpace(l) == "" {
			continue
		}
		// A row of the fenced block carries NO marker column at all (see
		// answerRow.gutterGlyph), so it is read back whole: stripping the block's
		// column off it would eat the block's first two characters and the
		// round-trip would be the test's own arithmetic rather than the screen's.
		if r >= block[0] && r <= block[1] {
			b.WriteString(l)
			continue
		}
		switch {
		case strings.HasPrefix(l, TurnMarker):
			b.WriteString(strings.TrimPrefix(l, TurnMarker))
		case strings.HasPrefix(l, ThinkingGutter):
			b.WriteString(strings.TrimPrefix(l, ThinkingGutter))
		default:
			_, rest := cutCols(l, blockColumn())
			b.WriteString(rest)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// assertBodyRowsOpenAtTheBlockColumn is the claim every case below makes, over
// the rows that CONTINUE the turn's opening row.
func assertBodyRowsOpenAtTheBlockColumn(t *testing.T, p markerProbe, what string, want int) {
	t.Helper()
	rows := p.runsInTurn()
	if len(rows) == 0 {
		t.Fatalf("%s: the turn rendered no continuation row — the content must WRAP for this assertion to bite\n%s",
			what, p.dump())
	}
	block := p.fencedRows()
	for _, r := range rows {
		l := p.lines[r]
		// The rows of a FENCED BLOCK belong to the code/tool layer, which opens at
		// column 0 so a reader can copy them out whole (answerRow.gutterGlyph):
		// this claim is about the PROSE body's column, and the block is the one
		// shape exempt from it — by design, not by accident, and asserted instead
		// by TestCopiedText_CodeBlockRowsArePasteable.
		if r >= block[0] && r <= block[1] {
			continue
		}
		// A row that carries the turn's GLYPH is a line of the turn (or a code
		// block's own line): its column is the glyph's, and that is the correct
		// shape. It is the CONTINUATION row — the row a line wrapped onto, which
		// must open with the same number of BLANK cells — that this asserts.
		if strings.HasPrefix(l, TurnMarker) || strings.HasPrefix(l, ThinkingGutter) {
			continue
		}
		if got := displayWidth(l) - displayWidth(strings.TrimLeft(l, " ")); got != want {
			t.Fatalf("%s: body row %d opens at cell %d, want the block's column %d — the left edge is ragged\n%s",
				what, r, got, want, p.dump())
		}
	}
}

// assertBodyTextRoundTrips checks the columns did not cost the answer its
// characters: the body read back off the grid is the text the turn was given
// (whitespace at a wrap boundary is not preserved by the renderer, so the
// comparison is on the non-space characters).
func assertBodyTextRoundTrips(t *testing.T, p markerProbe, what, want string) {
	t.Helper()
	got := p.bodyText()
	if stripSpaces(got) != stripSpaces(want) {
		t.Fatalf("%s: the answer did not round-trip through the grid\n got=%q\nwant=%q\n%s",
			what, got, want, p.dump())
	}
}

// stripSpaces drops spaces: a wrap consumes the space it broke at, so the
// round-trip comparison is about characters, not about the break.
func stripSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' {
			return -1
		}
		return r
	}, cutReadingFold(s))
}

// TestMarkerColumn_WrappedCJKRowsKeepTheExactColumn is THE RED case: one CJK
// paragraph with an ASCII word inside it, wrapped at 56 cells. The row the line
// wraps onto must open at exactly the marker column's two cells.
//
// MEASURED DEFECT (master, before the fix): the wrap landed after "ASCII", and
// the row it wrapped onto opened at cell 3 — `   token 和 code 片段…` — because
// a wrap that breaks AT a space draws that space inside the marker column
// instead of consuming it. On the operator's own dump the same arithmetic shows
// up as rows opening at cell 0 (`tatus ==="; git status …`, `="; python3 -c "`)
// beside rows that carry the glyph.
func TestMarkerColumn_WrappedCJKRowsKeepTheExactColumn(t *testing.T) {
	p := renderMarkerTurn(t, markerCJKWrapCols, narrowMarkerRows, "窄列中文",
		markerCJKParagraph+"\n",
	)
	t.Logf("band:\n%s", p.dump())
	assertBodyRowsOpenAtTheBlockColumn(t, p, "wrapped CJK paragraph", blockColumn())
	assertBodyTextRoundTrips(t, p, "wrapped CJK paragraph", markerCJKParagraph)
}

// TestMarkerColumn_FencedBlockRowsKeepTheColumn is the fenced-code half: a fenced
// block inside a turn is part of the turn, so its rows keep the block's column,
// and the rows its content WRAPS onto open at the column with NO glyph — never
// the marker repeated on code, and never at cell 0.
func TestMarkerColumn_FencedBlockRowsKeepTheColumn(t *testing.T) {
	p := renderMarkerTurn(t, markerCJKWrapCols, narrowMarkerRows, "窄列代码",
		"解释如下：\n",
		FenceMarker+"\n",
		markerCJKCodeLine+"\n",
		"echo ok\n",
		FenceMarker+"\n",
	)
	t.Logf("band:\n%s", p.dump())
	// The PROSE row still opens at the block's column; the block's own rows open
	// at column 0, because they are the reader's copying material (see
	// answerRow.gutterGlyph): they are excluded from the column claim and are
	// asserted, byte-exact, to BE the answer's lines.
	assertBodyRowsOpenAtTheBlockColumn(t, p, "fenced block", blockColumn())
	// The block's rows are on the grid verbatim, in order: the first may WRAP
	// (the CJK line is wider than the band), so its two rows are re-joined the
	// way a reader copying the block reassembles them.
	blk := p.fencedRows()
	joined := strings.Join(p.lines[blk[0]:blk[1]+1], "")
	if wantBlock := FenceMarker + markerCJKCodeLine + "echo ok" + FenceMarker; joined != wantBlock {
		t.Errorf("the copied block = %q, want the answer's block verbatim %q — a code row that carries ink of its own cannot be pasted\n%s",
			joined, wantBlock, p.dump())
	}
	// The body read back off the grid is the turn's own text, fences included:
	// the block's rows keep the column, and the block is part of the turn.
	want := "解释如下：" + FenceMarker + markerCJKCodeLine + "echo ok" + FenceMarker
	assertBodyTextRoundTrips(t, p, "fenced block", want)
}

// TestMarkerColumn_OperatorGeometryRowsKeepTheColumn runs the same claim at the
// OPERATOR's own geometry (117 cells — the width of /tmp/error.txt's rows) and
// with the shape its ragged rows have: a line whose row ends on a space, so the
// row that follows opens one cell inside the column (`   properly.` / `   吗？`).
func TestMarkerColumn_OperatorGeometryRowsKeepTheColumn(t *testing.T) {
	const cols = 117
	// The operator's own sentence shape (/tmp/error.txt row 113-115): a CJK line
	// long enough to fill 117 cells, whose row ends at a space so the row that
	// follows opens one cell inside the column. The dump itself shows it as
	// `▌ …要我跑一次 git prune / gc` then `   吗？` — three cells in, against the
	// block's two. Reproduced verbatim here.
	const operatorLine = "另外提醒一件无关但有风险的事：fetch 时 git 警告 There are too many " +
		"unreachable loose objects 且 .git/gc.log 存在 —— 自动 gc 已被禁用，仓库会持续膨胀。" +
		"要我跑一次 git prune / gc 吗？"
	p := renderMarkerTurn(t, cols, narrowMarkerRows, "操作员宽度", chunkByRunes(operatorLine+terminalNewline, 7)...)
	t.Logf("band:\n%s", p.dump())
	assertBodyRowsOpenAtTheBlockColumn(t, p, "operator geometry", blockColumn())
	// A word-per-line run makes the same arithmetic fire on pure ASCII: the row
	// that wraps must open at the column whatever the script.
	p = renderMarkerTurn(t, cols, narrowMarkerRows, "操作员宽度",
		chunkByRunes(strings.Repeat("word ", 60)+"tail."+terminalNewline, 7)...)
	t.Logf("band:\n%s", p.dump())
	assertBodyRowsOpenAtTheBlockColumn(t, p, "operator geometry, ASCII", blockColumn())
}

// TestMarkerColumn_WrapOntoASpaceIsNotDrawnInsideTheColumn is the mechanism
// pinned on its own, at two widths and three burst sizes: a text whose row ends
// ON a space must leave the continuation row at the block's column. The burst
// sweep matters because a wrap can be decided by the cut (one burst) or by a
// committed row's carry (several) — and the carry path must obey the same
// column.
func TestMarkerColumn_WrapOntoASpaceIsNotDrawnInsideTheColumn(t *testing.T) {
	// 56 cells: the body is 54 cells wide, and this text's tail word lands
	// exactly on the boundary so the break is at a space.
	const text = markerCJKParagraph
	for _, cols := range []int{40, markerCJKWrapCols, 60, 117} {
		for _, burst := range []int{7, 64, 1 << 20} {
			p := renderMarkerTurn(t, cols, narrowMarkerRows, "空格断行", chunkByRunes(text+"\n", burst)...)
			assertBodyRowsOpenAtTheBlockColumn(t, p, fmt.Sprintf("cols=%d burst=%d", cols, burst), blockColumn())
		}
	}
}

// chunkByRunes splits s into bursts of at most n runes — the shape of a real
// token stream, and the axis a wrap can be decided on (a burst boundary and a
// row boundary are independent).
func chunkByRunes(s string, n int) []string {
	var out []string
	for i := 0; i < len(s); {
		j, c := i, 0
		for j < len(s) && c < n {
			_, size := utf8.DecodeRuneInString(s[j:])
			j += size
			c++
		}
		out = append(out, s[i:j])
		i = j
	}
	return out
}
