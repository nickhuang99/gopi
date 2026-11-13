package lineedit

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// copied_text_test.go — THE PASTE LEG: a command the band shows must be able to
// LEAVE the band and run.
//
// THE DEFECT (reported by the operator 2026-10-02, and reproducible from the
// epic's own words): every row of an answer opens with the turn's two-cell
// gutter, and the gutter of a body row is drawn as its GLYPH — `▌ ` (TurnMarker,
// U+258C). The band's fenced code block is the answer's copying material, so the
// reader selects it off the screen and pastes it into a shell, and the FIRST line
// is not the command: it is `▌ cd /tmp && go build ./...`, which the shell reads
// as a word named `▌`. The fenced ```` ``` ```` rows are worse — they are the
// answer's own fence markers, and behind a `▌ ` they are not markers at all (the
// 2026-09-29 defect recorded in fence_gutter_test.go is the same glyph seen from
// the other end: the classifier had to learn to STEP OVER it, because a copy of
// the band's screen carries it).
//
// THE FIX IS THE GUTTER'S INK, NOT THE GUTTER'S COLUMN. A code/tool-layer row
// keeps its two cells — the block's left edge and the content width are exactly
// what they were, and nothing on the grid reflows — and draws them BLANK
// (answerRow.copied, answerRow.gutterGlyph, writeRow's writeGutter call). What a
// reader copies out of a code block is then the text the block holds.
//
// WHY NOT THE OTHER TWO DIRECTIONS the epic lists:
//
//   - (a) strip on export: there IS no export in this product. The copy is the
//     TERMINAL's own selection off the cell grid, so the grid is the artifact;
//     a stripper would have to sit inside every reader of it (the operator's
//     selection, a dump consumer, a paste into the model), and the dump's row
//     TEXT is the same grid read back — see copiedRowsOf.
//   - (c) draw no gutter at all on a code row: the cells would go with the glyph,
//     and the code block would open two cells LEFT of the prose around it, which
//     reverses this package's oldest grid invariant (the block's column) and
//     reflows the answer. The glyph is what the reader must not copy; the column
//     is what the reader must keep.
//
// THE OTHER HALF, which is the reason the fix is scoped to the code/tool layer
// and not to the band: the turn's SEPARABILITY must not regress. TurnMarker exists
// because two consecutive turns' paragraphs ran together with the reader unable to
// tell where one ended (see its comment in answer.go), and that defect is a PROSE
// shape — the answer's paragraphs are not fenced, so their rows keep the glyph.
// TestCopiedText_TurnSeparabilityIsKept is the anti-regression claim, taken on the
// same run.

// copiedTurnAnswer is the smallest answer that plants the whole shape: prose
// above the block, a fenced block whose first row is a command, and prose after
// the closing marker.
const copiedTurnAnswer = "here is how:\n" +
	FenceMarker + "bash\n" +
	"cd /tmp && go build ./...\n" +
	FenceMarker + "\n" +
	"that is all.\n"

// gridDump is the failure message: every non-blank row with its index, so a
// ragged left edge is visible in the output rather than inferred.
func gridDump(p *gutterProbe) string {
	var b strings.Builder
	for i := 0; i < emphasisProbeRows; i++ {
		l := p.screen.Line(i)
		if strings.TrimSpace(l) == "" {
			continue
		}
		fmt.Fprintf(&b, "row %d: %q\n", i, l)
	}
	return b.String()
}

// copiedRowsOf returns the grid's rows as (row index, text) pairs, blank rows
// dropped — the reader's own view of the band.
func copiedRowsOf(p *gutterProbe) []struct {
	row  int
	text string
} {
	var out []struct {
		row  int
		text string
	}
	for i := 0; i < emphasisProbeRows; i++ {
		if line := p.screen.Line(i); strings.TrimSpace(line) != "" {
			out = append(out, struct {
				row  int
				text string
			}{i, line})
		}
	}
	return out
}

// copiedRowOf returns the index of the single grid row whose text CONTAINS
// needle, and fails the test when there is not exactly one — a claim about "the
// row a reader copies" must be about one row.
func copiedRowOf(t *testing.T, p *gutterProbe, needle string) int {
	t.Helper()
	found := -1
	for _, r := range copiedRowsOf(p) {
		if !strings.Contains(r.text, needle) {
			continue
		}
		if found >= 0 {
			t.Fatalf("%q is on more than one grid row (%d and %d): the claim needs one row\n%s",
				needle, found, r.row, gridDump(p))
		}
		found = r.row
	}
	if found < 0 {
		t.Fatalf("%q is not on the grid at all\n%s", needle, gridDump(p))
	}
	return found
}

// TestCopiedText_CodeBlockRowsArePasteable is THE RED case: the command line of a
// fenced block, read off the rendered grid, is the command and nothing else.
//
// MEASURED DEFECT (master, before the fix): the row reads
// `▌ cd /tmp && go build ./...`, so a paste into a shell dies on the first line
// with the word `▌`. The assertion is BYTE-EXACT against the source line: not
// "does not start with ▌" alone — the row's whole text IS the answer's line, so a
// future decoration cannot hide anywhere else in it either.
func TestCopiedText_CodeBlockRowsArePasteable(t *testing.T) {
	p := renderGutterAnswer(t, true, copiedTurnAnswer)
	t.Logf("band:\n%s", gridDump(p))
	const command = "cd /tmp && go build ./..."
	row := copiedRowOf(t, p, command)
	if got := p.screen.Line(row); got != command {
		t.Fatalf("code row %d = %q, want the command verbatim %q — the text a reader copies out of a code block must BE the command\n%s",
			row, got, command, gridDump(p))
	}
	// The fence marker's own row: the closing marker is the answer's line too, and
	// a reader who copies the enclosing block copies it as well.
	last := -1
	for _, r := range copiedRowsOf(p) {
		if strings.TrimSpace(r.text) == FenceMarker {
			if last >= 0 && r.row > last+1 {
				t.Fatalf("the code block's rows are not contiguous (markers at %d and %d)\n%s", last, r.row, gridDump(p))
			}
			last = r.row
		}
	}
	if last < 0 {
		t.Fatalf("no row of the grid holds the fence marker alone — the block was not painted as a block\n%s", gridDump(p))
	}
	if got := p.screen.Line(last); got != FenceMarker {
		t.Fatalf("closing fence row %d = %q, want %q verbatim\n%s", last, got, FenceMarker, gridDump(p))
	}
	// The OPENING marker carries the language tag, so it is asserted by prefix —
	// and the whole point is that the marker is at the row's FIRST cell, which is
	// what makes it a marker to every markdown reader a paste ever reaches.
	open := copiedRowOf(t, p, FenceMarker+"bash")
	if got := p.screen.Line(open); !strings.HasPrefix(got, FenceMarker) {
		t.Fatalf("opening fence row %d = %q, want it to START with %q — a marker behind a gutter is not a marker\n%s",
			open, got, FenceMarker, gridDump(p))
	}
	// The whole block, as a reader would copy it out (markers included), is the
	// answer's own text — one assertion that no row of it carries extra ink.
	block := []string{}
	for r := open; r <= last; r++ {
		block = append(block, p.screen.Line(r))
	}
	want := []string{FenceMarker + "bash", command, FenceMarker}
	if strings.Join(block, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the copied block is not the answer's block\n got=%q\nwant=%q\n%s",
			block, want, gridDump(p))
	}
}

// TestCopiedText_NoRowOfACodeBlockCarriesTheGutterGlyph states the invariant the
// claim above is an instance of, over a block with several rows and a wrap: NO
// row of a code/tool-layer run opens with a gutter glyph, whatever it holds.
func TestCopiedText_NoRowOfACodeBlockCarriesTheGutterGlyph(t *testing.T) {
	long := "git -C /home/nick/work/GoPi status --short --branch " + strings.Repeat("and-more ", 12) + "tail"
	p := renderGutterAnswer(t, true, FenceMarker+"\n"+long+"\n"+FenceMarker+"\n")
	for _, g := range AnswerGutters() {
		g = strings.TrimRight(g, " ")
		for _, r := range copiedRowsOf(p) {
			if !strings.HasPrefix(r.text, g) {
				continue
			}
			// The band's own chrome (the request row, the header rule) is not a
			// row of the block; the block's rows are the ones between the
			// markers, and those are the ones this claim is about.
			if strings.Contains(r.text, FenceMarker) || strings.Contains(r.text, "and-more") || strings.Contains(r.text, "tail") {
				t.Fatalf("grid row %d = %q opens with the gutter glyph %q and belongs to the code block\n%s",
					r.row, r.text, g, gridDump(p))
			}
		}
	}
}

// TestCopiedText_TurnSeparabilityIsKept is the ANTI-REGRESSION half: the prose of
// two consecutive turns is still distinguishable on the grid.
//
// TurnMarker's own reason is the measured defect it was written for — "Done." of
// turn 1 immediately above "Here is the answer …" of turn 2, with nothing between
// them. The claim here is the criterion, not a snapshot: each turn's PROSE rows
// open with the response layer's glyph, so a reader scanning the band sees which
// turn a paragraph belongs to even where the two blocks touch.
func TestCopiedText_TurnSeparabilityIsKept(t *testing.T) {
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(emphasisProbeRows, emphasisProbeCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", emphasisProbeRows, emphasisProbeCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(BasicPalette)
	r.BeginTurn(TurnInfo{Index: 1, Prompt: "first"})
	r.Delta("Done.\n")
	r.Finish()
	r.EndTurn()
	r.BeginTurn(TurnInfo{Index: 2, Prompt: "second"})
	r.Delta("Here is the answer.\n")
	r.Finish()
	r.EndTurn()
	sc := NewScreen(emphasisProbeCols, emphasisProbeRows)
	sc.Feed([]byte(cap.bytes()))
	p := &gutterProbe{emphasisProbe: &emphasisProbe{screen: sc}}

	// Both prose rows carry the response layer's glyph — the marker column is the
	// turn's and it is still drawn on prose.
	for _, want := range []string{"Done.", "Here is the answer."} {
		row := copiedRowOf(t, p, want)
		if got := sc.Line(row); !strings.HasPrefix(got, TurnMarker) {
			t.Fatalf("prose row %d = %q does not open with the turn marker %q — two turns' prose would run together\n%s",
				row, got, TurnMarker, gridDump(p))
		}
	}
	// And the turns are separated by the closing rule, the other half of the
	// chrome a reader uses.
	rule := false
	for _, rr := range copiedRowsOf(p) {
		if strings.HasPrefix(strings.TrimSpace(rr.text), TurnRuleGlyph) {
			rule = true
		}
	}
	if !rule {
		t.Fatalf("no closing rule between the two turns\n%s", gridDump(p))
	}
}

// TestCopiedText_StreamedFenceLeavesNoStaleGraphics is the STREAMING half, and
// the case the first version of this fix got wrong.
//
// A fenced answer arrives one chunk at a time, so the row that will become the
// OPENING MARKER is painted PROVISIONALLY first — and the provisional paint
// happens while the fence is not yet open, so that row carries the turn's gutter
// glyph. The decided row for the same line is the code layer's, which draws no
// gutter: it is the same TEXT, drawn two cells further left. The erase that
// clears a provisional paint wider than the row committed over it compared the
// two texts' WIDTHS, which is only the right comparison while both start at the
// same column — measured on a live pty 2026-10-02, the marker row came out as
// ```` ```bashsh ````, the tail of the provisional `▌ ```bash` sticking out past
// the decided ```` ```bash ````.
//
// The claim is the cell grid's: EVERY row of the block is the answer's own text,
// byte for byte, however the stream was cut.
func TestCopiedText_StreamedFenceLeavesNoStaleGraphics(t *testing.T) {
	const answer = "run this:\n```bash\ncd /tmp && go build ./...\n```\ndone.\n"
	for _, chunk := range []int{1, 2, 3, 5, 8, 16} {
		rows := chunkProbeTurnGrid(t, chunk, answer)
		joined := strings.Join(rows, "\n")
		// Every row of the block, and the whole block read back, is the answer's
		// text — no leftover cell from a provisional paint.
		for _, want := range []string{"```bash", "cd /tmp && go build ./...", "```"} {
			if rowOf(rows, want) < 0 {
				t.Errorf("chunk=%d: %q is not a whole row of the grid — a provisional paint left cells behind\n%s",
					chunk, want, joined)
			}
		}
		for _, bad := range []string{"```bashsh", "▌ ```", "▌ cd /tmp"} {
			if strings.Contains(joined, bad) {
				t.Errorf("chunk=%d: the grid carries %q — the provisional paint's cells were not cleared\n%s",
					chunk, bad, joined)
			}
		}
	}
}

// chunkProbeTurnGrid is chunkProbeGrid at the TURN shape: the same streamed
// answer, painted inside a marked turn, so every row's gutter decision is live
// (the defect this pins needs the turn chrome — without it there is no gutter to
// paint provisionally and none to drop).
func chunkProbeTurnGrid(t *testing.T, chunk int, text string) []string {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(emphasisProbeRows, emphasisProbeCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", emphasisProbeRows, emphasisProbeCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(BasicPalette)
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.BeginTurn(TurnInfo{Index: 1, Prompt: "run it"})
	for i := 0; i < len(text); i += chunk {
		end := i + chunk
		if end > len(text) {
			end = len(text)
		}
		now = now.Add(emphasisChunkGap)
		r.Delta(text[i:end])
	}
	r.Tick()
	r.Finish()
	r.EndTurn()
	sc := NewScreen(emphasisProbeCols, emphasisProbeRows)
	sc.Feed([]byte(cap.bytes()))
	var rows []string
	for i := 0; i < emphasisProbeRows; i++ {
		if l := strings.TrimRight(sc.Line(i), " "); l != "" {
			rows = append(rows, l)
		}
	}
	return rows
}
