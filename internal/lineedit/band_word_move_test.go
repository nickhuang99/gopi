package lineedit

import (
	"strings"
	"testing"
	"time"
)

// band_word_move_test.go — the answer band's whole-word disposition, at the two
// levels that matter: the row cutter itself, and the BAND the operator reads
// (region, scroll, marker column, cut label — the components `gopi chat` drives).
//
// The disposition (band_word_move.go): a token the cell edge would halve moves
// down WHOLE, so a row ends where the reader's own token does. The band's cut
// label therefore narrows to the one cut nothing can repair — a token wider than
// the whole row.
//
// Why both levels: the cutter can be right while the band is wrong (the label, the
// marker column, the wrap's own row arithmetic), and the band can look right while
// the cells disagree with the source. The cases below assert the two separately.

const (
	// bandWordCols is the width the deliverable names: narrow enough that the
	// operator's reported line wraps twice, wide enough that no token of it is
	// over-wide — i.e. exactly the window where the disposition has work to do.
	bandWordCols = 32
	// bandWordRows is a band tall enough for the probe lines plus the turn's own
	// chrome (header, request rows, closing rule).
	bandWordRows = 26
	// bandWordBurst is the carrier's delta size. Deliberately not a divisor of
	// bandWordCols, so no break can be an artefact of a burst boundary.
	bandWordBurst = 5
	// bandWordPrompt labels the turn; the body is read below its header.
	bandWordPrompt = "wrap"
)

// bandWordLine is the operator's line (the deliverable's own repro): two tokens the
// cell edge would halve — `endpoint` and `s//tyle` — inside ordinary prose.
const bandWordLine = "run /compact then check the endpoint of s//tyle now"

// bandWordRowsAt streams one line through the real renderer at `cols` and returns
// the band's BODY rows with the turn's marker column stripped, right-trimmed.
func bandWordRowsAt(t *testing.T, line string, cols int) []string {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(bandWordRows, cols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", bandWordRows, cols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	r.SetClock(clock.Now)
	r.SetPalette(BasicPalette)
	r.BeginTurn(TurnInfo{Index: 1, Prompt: bandWordPrompt})
	for _, b := range chunkByRunes(line, bandWordBurst) {
		r.Delta(b)
	}
	r.Delta(terminalNewline)
	r.Tick()
	r.Finish()
	r.EndTurn()

	sc := NewScreen(cols, bandWordRows)
	sc.Feed([]byte(cap.bytes()))
	lines := make([]string, 0, bandWordRows)
	for i := 0; i < bandWordRows; i++ {
		lines = append(lines, strings.TrimRight(sc.Line(i), " "))
	}
	header := rowOf(lines, turnHeaderText(1, bandWordPrompt))
	if header < 0 {
		t.Fatalf("no turn header on the grid\n--- band ---\n%s", strings.Join(lines, "\n"))
	}
	body := make([]string, 0, 4)
	for i := skipRequestRows(lines, header+1, bandWordPrompt, cols); i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == strings.Repeat(TurnRuleGlyph, cols) {
			break
		}
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		body = append(body, lines[i])
	}
	if len(body) == 0 {
		t.Fatalf("the answer produced no band rows\n--- band ---\n%s", strings.Join(lines, "\n"))
	}
	return body
}

// bandWordTexts strips the turn's marker column from each row — it is the band's
// chrome, not the answer's characters, and it is the same width on every body row
// (the glyph on the turn's first line, blanks on a continuation; lineGutter).
func bandWordTexts(t *testing.T, rows []string) []string {
	t.Helper()
	g := displayWidth(TurnMarker)
	out := make([]string, 0, len(rows))
	for i, row := range rows {
		rs := []rune(row)
		if displayWidth(row) < g {
			t.Fatalf("row %d is inside the marker column: %q", i, row)
		}
		out = append(out, string(rs[leadingCells(rs, g):]))
	}
	return out
}

// TestAnswerBand_WholeWordMovesDown is the deliverable's own case: at 32 cells the
// operator's line wraps TWICE, and neither wrap may halve a token — so no row wears
// the cut label, and the rows still reassemble to the line character for character.
//
// RED before the disposition: every row ended mid-token and wore ↵, because writeRow
// cut by cells alone.
func TestAnswerBand_WholeWordMovesDown(t *testing.T) {
	rows := bandWordRowsAt(t, bandWordLine, bandWordCols)
	texts := bandWordTexts(t, rows)
	if len(texts) < 2 {
		t.Fatalf("the probe line occupies %d band row(s) at %d cells — it must wrap for the "+
			"disposition to be exercised\n%s", len(texts), bandWordCols, describeCutRows(rows))
	}
	for i, text := range texts {
		if strings.Contains(text, answerCutLabel) {
			t.Errorf("row %d wears the band's cut label %q although every token of the line fits a "+
				"whole row — the token must move down whole, not be halved\n%s",
				i, answerCutLabel, describeCutRows(rows))
		}
	}
	// THE ROWS REASSEMBLE TO THE LINE. Each wrap CONSUMES the one space the
	// continuation would otherwise draw inside the block's column
	// (trimWrapSpace), so the reconstruction re-inserts it where the line had it
	// — and the walk below proves there is exactly one such space per row
	// boundary, rather than joining with a space that was never there.
	at := 0
	for i, text := range texts {
		if !strings.HasPrefix(bandWordLine[at:], text) {
			if at >= len(bandWordLine) || bandWordLine[at] != ' ' ||
				!strings.HasPrefix(bandWordLine[at+1:], text) {
				t.Fatalf("row %d (%q) does not continue the line at byte %d (%q)\n%s",
					i, text, at, bandWordLine[at:], describeCutRows(rows))
			}
			at++ // the consumed wrap space
		}
		at += len(text)
	}
	if at != len(bandWordLine) {
		t.Errorf("the rows consumed %d of the line's %d bytes — %d were dropped or duplicated\n%s",
			at, len(bandWordLine), len(bandWordLine)-at, describeCutRows(rows))
	}
	// THE TOKENS THE READER COPIES ARE WHOLE: `endpoint` and `s//tyle` each lie
	// inside ONE row rather than straddling two — which is the whole claim, since
	// a halves-of-a-token pair does not run.
	for _, token := range []string{"endpoint", "s//tyle"} {
		whole := false
		for _, text := range texts {
			if strings.Contains(text, token) {
				whole = true
			}
		}
		if !whole {
			t.Errorf("token %q is split across rows — the band halved it\n%s",
				token, describeCutRows(rows))
		}
	}
}

// TestAnswerBand_OverWideTokenKeepsTheLabel is the OTHER half of the narrowed
// meaning: a token wider than the whole row has no wider row to move to, so its cut
// stays — and it still says so.
func TestAnswerBand_OverWideTokenKeepsTheLabel(t *testing.T) {
	line := strings.Repeat("wideunbreakabletoken", bandWordCols/18+2)
	rows := bandWordRowsAt(t, line, bandWordCols)
	texts := bandWordTexts(t, rows)
	marked := 0
	for i, text := range texts {
		if i < len(texts)-1 && strings.HasSuffix(text, answerCutLabel) {
			marked++
		}
	}
	if marked == 0 {
		t.Fatalf("no row wears the cut label %q although the token is wider than the band\n%s",
			answerCutLabel, describeCutRows(rows))
	}
	// Nothing lost behind the glyph: the reading minus its labels is the line.
	var joined strings.Builder
	for _, text := range texts {
		joined.WriteString(cutReadingFold(text))
	}
	if got := joined.String(); got != line {
		t.Errorf("the band consumed %d of the token's %d cells — the label stands for text nobody "+
			"draws:\n  got  %q\n  want %q", len(got), len(line), got, line)
	}
}

// TestBandCut_KeepsTheCounterexamples is the cutter's own regression arm, for the
// three shapes the disposition must NOT break. Each is the reason the rules around
// the move exist:
//
//   - CJK: a wide rune is never split, and a wide↔narrow junction is already a
//     boundary, so no move (and no label) applies;
//   - a path separator: `wrapSoftBreakPunct` is where a row may legitimately end,
//     so `fix/pushback` never moves as one token;
//   - SGR: the move may not end a row inside an escape sequence — the parameters
//     would print as text (wrapSafeRowEnd).
//
// The invariant asserted on all three: head + tail is the input, byte for byte, and
// no width is exceeded.
func TestBandCut_KeepsTheCounterexamples(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
	}{
		{"cjk-wide-run", "说明文字很长需要折行显示出来", 8},
		{"cjk-then-latin", "说明文字 regenerable artefact 后面还有", 12},
		{"path-separator", "src/internal/lineedit/band_word_move.go", 11},
		{"sgr-run", "\x1b[1mkey\x1b[0m endpoint of the answer band", 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			head, tail, halved := bandCut(c.text, c.width)
			if head+tail != c.text {
				t.Fatalf("head+tail is not the input:\n  head %q\n  tail %q\n  want %q", head, tail, c.text)
			}
			if w := displayWidth(head); w > c.width {
				t.Errorf("head %q measures %d cells, over the row's %d", head, w, c.width)
			}
			if halved && !bandCutHalvesToken(c.text, len(head)) {
				t.Errorf("the row is reported halved at %d, but that offset is not inside a token", len(head))
			}
			// A row may never end inside an escape sequence, whatever else is
			// true: a torn sequence prints its parameters as text.
			if !wrapSafeRowEnd(c.text, 0, len(head)) {
				t.Errorf("the row ends inside a control sequence at %d: %q", len(head), head)
			}
		})
	}
}

// TestBandCut_MovesAFittingTokenDown pins the minimal statement the band's version
// of the rule makes, and the one that keeps the two paints (streaming and replay)
// agreeing: the move is decidable from the text in hand — it reads no byte past the
// cut, so an unfinished line and the finished line it becomes reach the SAME break.
func TestBandCut_MovesAFittingTokenDown(t *testing.T) {
	head, tail, halved := bandCut("the endpoint", 6)
	if head != "the" || tail != " endpoint" {
		t.Fatalf("bandCut(%q, 6) = (%q, %q) — the token must move down whole", "the endpoint", head, tail)
	}
	if halved {
		t.Errorf("a token that moved down whole is not a halved token")
	}
	// The same text, TRUNCATED at the cut and at one byte past it: the decision is
	// the same at both, because nothing past the cut is read. This is what makes
	// the streaming paint and the commit replay land on one break.
	for _, inFlight := range []string{"the endpo", "the endpoint"} {
		h, _, _ := bandCut(inFlight, 6)
		if h != head {
			t.Errorf("bandCut(%q, 6) = %q, want the same break as the whole line (%q)", inFlight, h, head)
		}
	}
	// A token WIDER than the row keeps the cell cut: there is no wider row.
	h, tf, halved := bandCut("abcdefgh", 4)
	if h != "abcd" || tf != "efgh" || !halved {
		t.Fatalf("bandCut(%q, 4) = (%q, %q, halved=%v) — an over-wide token keeps the cell cut and "+
			"the label", "abcdefgh", h, tf, halved)
	}
	// A boundary the source offers is not a halved token: whitespace and the soft
	// break punctuation are places a row may legitimately end.
	if _, _, halved := bandCut("src/internal", 5); halved {
		t.Errorf("a row ending after a path separator is reported halved")
	}
}

// ── the mixed CJK/Latin line: a soft-break token is still a token ───────────
//
// The band's cut predicate has to see the run the WRAP policy moves — which
// includes the soft-break punctuation — because the band's row end is the cell
// edge's own, not a boundary the source offered. Measured defect: at 28 cells the
// operator's mixed line ended at `s//` and the old predicate (word runes on BOTH
// sides) said "not halved", so `s//tyle` was drawn as two rows that do not run.
//
// The expected break, worked out by hand off the grid (2 cells per CJK rune):
//
//	这个_ (5) endpoint_- (14) 的处理在 (22, a CJK run) space (23) s//tyle …
//	28 cells lands INSIDE `s//tyle` at column 26 (`t`), so the token moves down
//	and the row ends after the CJK rune at byte 28.

// bandWordMixedLine is the deliverable's own repro: CJK prose with one Latin word
// and one path-ish token, at a width where the cell edge falls inside the token.
const (
	bandWordMixedLine = "这个 endpoint 的处理在 s//tyle 里面已经改好了"
	bandWordMixedCols = 28
	// bandWordMixedShift is the byte offset the row must END at: after the last
	// drawn rune of `这个 endpoint 的处理在` (2 cells each for the CJK runes).
	bandWordMixedShift = 28
)

// TestBandWordShift_MixedSoftBreakToken pins the requirement at the byte level.
//
// TWO cuts matter here and they are different numbers:
//
//   - cutCols(text, 28) takes the CELL edge to byte 34 (`...s//ty`) — that is the
//     cut the band would make on its own, and it halves `s//tyle`;
//   - bandWordShift moves it back to 29 (the token's own first byte, `s`), and
//     bandCut then carries the space down with the token, so the row ends at 28.
//
// bandWordShift(text, 0, 28) therefore returns 28 UNCHANGED: byte 28 ends after
// the wide rune `在`, which is not a halved token — the halving cut is 34, and it
// is the one the disposition has to repair. (The deliverable's brief said this
// call should read 20; measured, 20 is not a position in this string that any
// rule produces — see the row-end facts asserted below.)
func TestBandWordShift_MixedSoftBreakToken(t *testing.T) {
	cellHead, cellTail := cutCols(bandWordMixedLine, bandWordMixedCols)
	// The CELL edge really does land inside `s//tyle`: the fixture is worthless if
	// it stops straddling the token.
	if !strings.HasPrefix(bandWordMixedLine[len(cellHead):], "le ") {
		t.Fatalf("the cell cut at %d does not land inside `s//tyle` (head %q, tail %q) — the "+
			"fixture no longer stands where it was written", len(cellHead), cellHead, cellTail)
	}
	// A cut that lands after a WIDE rune halves nothing, so it is left alone.
	if got := bandWordShift(bandWordMixedLine, 0, bandWordMixedShift); got != bandWordMixedShift {
		t.Errorf("bandWordShift(%q, 0, %d) = %d, want it unchanged — that offset ends after a wide "+
			"rune, not inside a token", bandWordMixedLine, bandWordMixedShift, got)
	}
	// The cell edge's cut is inside the token, and it moves back to the token's
	// own first byte.
	if !bandCutHalvesToken(bandWordMixedLine, len(cellHead)) {
		t.Fatalf("the cut at %d is reported as NOT halving a token, but it lands inside `s//tyle`",
			len(cellHead))
	}
	if got := bandWordShift(bandWordMixedLine, 0, len(cellHead)); got != bandWordMixedShift+1 {
		t.Errorf("bandWordShift(%q, 0, %d) = %d, want %d (the token's own first byte `s`)",
			bandWordMixedLine, len(cellHead), got, bandWordMixedShift+1)
	}
	if got := bandCutRunStart(bandWordMixedLine, 0, len(cellHead), bandTokenRune); got != bandWordMixedShift+1 {
		t.Errorf("bandCutRunStart(%q, 0, %d, bandTokenRune) = %d, want %d",
			bandWordMixedLine, len(cellHead), got, bandWordMixedShift+1)
	}
	// THE PREFIX RULE (memory #30145/#30136): the decision is the same on the line
	// truncated AT the cut and one rune further, because nothing past the cut is
	// read. This is what keeps the streaming paint and the commit replay on one
	// break.
	// (Each truncation must be LONGER than the cut: past the end of the text there
	// is no row to end, and bandWordShift reports that unchanged.)
	for _, inFlight := range []string{
		bandWordMixedLine[:len(cellHead)+1],
		bandWordMixedLine[:len(cellHead)+3],
	} {
		if got := bandWordShift(inFlight, 0, len(cellHead)); got != bandWordMixedShift+1 {
			t.Errorf("bandWordShift(truncated %q, 0, %d) = %d, want the whole line's %d",
				inFlight, len(cellHead), got, bandWordMixedShift+1)
		}
	}
	// The row cutter itself takes the moved break, carries the space down with the
	// token, and owes no cut label.
	h, tf, halved := bandCut(bandWordMixedLine, bandWordMixedCols)
	if h != bandWordMixedLine[:bandWordMixedShift] || h+tf != bandWordMixedLine {
		t.Fatalf("bandCut(%q, %d) = (%q, %q) — want the head to end after the CJK run at byte %d",
			bandWordMixedLine, bandWordMixedCols, h, tf, bandWordMixedShift)
	}
	if halved {
		t.Errorf("bandCut reports a halved row although the token moved down whole")
	}
	if !strings.HasPrefix(tf, " s//tyle") {
		t.Errorf("the tail is %q, want it to open with the moved token ` s//tyle`", tf)
	}
}

// TestAnswerBand_MixedSoftBreakToken is the same requirement on the surface the
// operator reads: the band's own rows, through the real renderer (region, scroll,
// marker column, cut label).
func TestAnswerBand_MixedSoftBreakToken(t *testing.T) {
	rows := bandWordRowsAt(t, bandWordMixedLine, bandWordMixedCols)
	texts := bandWordTexts(t, rows)
	whole := false
	for i, text := range texts {
		if strings.Contains(text, answerCutLabel) {
			t.Errorf("row %d wears the cut label %q although every token fits a whole row\n%s",
				i, answerCutLabel, describeCutRows(rows))
		}
		if strings.Contains(text, "s//tyle") {
			whole = true
		}
	}
	if !whole {
		t.Fatalf("`s//tyle` is not inside one band row — the band halved it\n%s", describeCutRows(rows))
	}
	// The rows still reassemble to the line, with the consumed wrap space
	// re-inserted where the line had it (the walk TestAnswerBand_WholeWordMovesDown
	// uses).
	at := 0
	for i, text := range texts {
		if !strings.HasPrefix(bandWordMixedLine[at:], text) {
			if at >= len(bandWordMixedLine) || bandWordMixedLine[at] != ' ' ||
				!strings.HasPrefix(bandWordMixedLine[at+1:], text) {
				t.Fatalf("row %d (%q) does not continue the line at byte %d\n%s",
					i, text, at, describeCutRows(rows))
			}
			at++
		}
		at += len(text)
	}
	if at != len(bandWordMixedLine) {
		t.Errorf("the rows consumed %d of the line's %d bytes\n%s",
			at, len(bandWordMixedLine), describeCutRows(rows))
	}
}

// TestBandCut_TokenPredicate pins the predicate's edges with explicit cuts: what
// counts as one token (the wrap policy's word rune plus the ONE separator this
// disposition was measured on, the path slash, because "may end here" is not "must
// end here"), and what does not (the rest of the wrap set, a wide CJK rune,
// whitespace) — so the move can never weld two words it was not given.
func TestBandCut_TokenPredicate(t *testing.T) {
	cases := []struct {
		s    string
		cut  int
		want bool
	}{
		{"s//tyle", 2, true},   // inside `s//`: the operator's own token
		{"s//tyle", 3, true},   // after the second `/`, still inside it
		{"s//tyle", 5, true},   // inside `tyle`
		{"a/file.go", 3, true}, // a path keeps its slash
		{"abcdefgh", 4, true},  // a plain word
		{"a-b", 2, false},      // a hyphen is NOT a token interior here (measured: the
		//                        wide set reddens the frame-monotonicity guard)
		{"k=v", 2, false},  // same for `=`
		{"x_1", 2, false},  // same for `_`
		{"a)b", 2, false},  // a closer ends the token
		{"a]b", 2, false},  // same
		{"a b", 2, false},  // whitespace is a boundary
		{"说明文字", 3, false}, // a wide↔wide junction is CJK's own boundary
		{"a说明", 1, false},  // a wide↔narrow junction is a boundary too
		{"a:aa", 3, true},  // `:` is not a token rune, so the halved run begins after it
	}
	for _, c := range cases {
		if got := bandCutHalvesToken(c.s, c.cut); got != c.want {
			t.Errorf("bandCutHalvesToken(%q, %d) = %v, want %v", c.s, c.cut, got, c.want)
		}
	}
	// The label's predicate is the NARROW one, and the two must stay separable: a
	// row ending after the slash is a break the reader can see, so it owes no ↵.
	if bandCutWordHalved("s//tyle", 3) {
		t.Errorf("bandCutWordHalved(%q, 3) = true — a break after `/` is visible and must not wear the label", "s//tyle")
	}
	if !bandCutWordHalved("abcdefgh", 4) {
		t.Errorf("bandCutWordHalved(%q, 4) = false — a break between two letters is invisible and wears the label", "abcdefgh")
	}
}

// TestBandTokenSetIsTheMeasuredSlashOnly pins the narrowed set as a DECISION, with
// its measurement: widening it to the wrap policy's full punctuation reddens the
// frame-monotonicity guard, so a later "cleanup" that derives the set from
// wrapSoftBreakPunct must fail here first.
func TestBandTokenSetIsTheMeasuredSlashOnly(t *testing.T) {
	if bandTokenSoftPunct != "/" {
		t.Fatalf("bandTokenSoftPunct = %q, want %q — the widening was measured: `-` (and the "+
			"whole wrapSoftBreakPunct set) makes TestAnswerBand_WrappedLineLosesNothingInAnyFrame red, "+
			"because the band's own transcript welds number ranges into one token and a frame the "+
			"reader saw SHRINKS", bandTokenSoftPunct, "/")
	}
	for _, r := range wrapSoftBreakPunct {
		switch r {
		case '/':
			if !bandTokenRune(r) {
				t.Errorf("the slash must belong to the token, or `s//tyle` is halved again")
			}
		case ')', ']':
			// Closers: a token never continues through one.
		default:
			if bandTokenRune(r) {
				t.Errorf("%q is in wrapSoftBreakPunct and must NOT be a token interior here", r)
			}
		}
	}
}
