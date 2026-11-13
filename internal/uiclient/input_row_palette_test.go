package uiclient

import (
	"fmt"

	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sgr"
)

// input_row_palette_test.go — the persistent input row's two palette halves.
//
// WHY THESE TESTS EXIST. `gopi ui`'s input row rendered as the terminal's
// default foreground: the row was drawn as one unstyled string, so the
// operator's typing row sat in whatever colour the terminal declares for plain
// text while the rest of the chrome carried the palette. This is the same class
// of defect as C1 at `gopi chat` (reversed in internal/lineedit, task
// 01a0e439), and the same three claims are measured here:
//
//  1. the palette REACHES the live cell grid (attrs per cell, not "we wrote SGR
//     bytes") — TestInputRowPaletteReachesTheCellGrid;
//  2. the two halves are DIFFERENTLY styled, prompt vs buffer, and every colour
//     tier paints them — TestInputRowCarriesThePalettePerTier;
//  3. a monochrome palette emits the PRE-CHANGE BYTES exactly — the goldens
//     below were captured on the pre-change tree — TestInputRowMonoBytesAreUnchanged.

// inputRowShape is one input-row shape: the terminal width, the typed buffer and
// the cursor's rune offset. The six shapes are the ones the pre-change golden was
// captured with (see inputRowMonoGolden's comment).
type inputRowShape struct {
	name   string
	width  int
	input  string
	cursor int
}

// inputRowShapes are the shapes both the golden and the tier tests run: the
// empty row (prompt + caret only), a short typed buffer, a caret in the middle,
// a buffer the window truncates, a CJK buffer (wide cells), and a buffer wider
// than the row.
var inputRowShapes = []inputRowShape{
	{"empty", 40, "", 0},
	{"short", 40, "hello", 5},
	{"mid-caret", 40, "hello world", 5},
	{"truncated", 12, "hello wide world", 16},
	{"cjk", 40, "宽度 table", 8},
	{"wrapped-long", 20, strings.Repeat("x", 40), 40},
}

// inputRowModel builds the smallest model an input row needs.
func inputRowModel(pal lineedit.Palette, sh inputRowShape) model {
	m := model{width: sh.width, height: 24}
	m.pal = pal
	m.input = []rune(sh.input)
	m.cursor = sh.cursor
	return m
}

// inputRowColourTiers are the palettes that MUST paint the row: the zero-value
// MonoPalette is deliberately absent (its claim is byte identity, a different
// test) and every other shipped table must put colour on both halves.
var inputRowColourTiers = []struct {
	name string
	pal  lineedit.Palette
}{
	{"basic", lineedit.BasicPalette},
	{"t256", lineedit.Palette256},
	{"hermes-dark", lineedit.HermesDarkPalette},
	{"hermes-light", lineedit.HermesLightPalette},
}

// TestInputRowCarriesThePalettePerTier is the positive half: every colour tier
// paints BOTH halves of every shape, with the prompt's own field on the prompt
// cells and the buffer's own field after them — and the row's CELL COUNT is
// unchanged from the unstyled layout, i.e. the SGR was applied after the cut and
// not counted as text.
func TestInputRowCarriesThePalettePerTier(t *testing.T) {
	for _, tier := range inputRowColourTiers {
		for _, sh := range inputRowShapes {
			m := inputRowModel(tier.pal, sh)
			got := m.inputLine()
			// The open sequence alone: Wrap("") returns its argument unchanged
			// (Style.Wrap's own empty-text guard), so the field's bytes are read
			// through Open(), the accessor that means "what this style opens with".
			promptSeq, bufferSeq := tier.pal.Prompt.Open(), tier.pal.Buffer.Open()
			if promptSeq == "" || bufferSeq == "" {
				t.Fatalf("%s: the tier paints nothing (prompt %q, buffer %q) — a colour tier with an "+
					"empty style leaves the operator's row on the terminal's default colour",
					tier.name, promptSeq, bufferSeq)
			}
			if !strings.Contains(got, promptSeq+inputPrompt) {
				t.Errorf("%s/%s: the prompt is not drawn under the tier's prompt style %q:\n%q",
					tier.name, sh.name, promptSeq, got)
			}
			// The buffer half: whatever follows the prompt must be wrapped by the
			// buffer's style, and that style must be a DIFFERENT one (two fields
			// that resolve to the same bytes would make the split invisible).
			wantBuffer := tier.pal.Buffer.Wrap(strings.TrimPrefix(stripSGR(got), inputPrompt))
			if !strings.HasSuffix(got, wantBuffer) {
				t.Errorf("%s/%s: the buffer half is not drawn under the tier's buffer style %q:\n%q",
					tier.name, sh.name, tier.pal.Buffer.Wrap(""), got)
			}
			if tier.pal.Prompt.Wrap("x") == tier.pal.Buffer.Wrap("x") {
				t.Errorf("%s: the prompt and the buffer resolve to the same style — the row's two "+
					"halves are indistinguishable", tier.name)
			}
			// The cut still governs the LAYOUT: strip the SGR and the row is the
			// same text, in the same cells, as the unstyled row.
			if plain := stripSGR(got); plain != inputRowModel(lineedit.MonoPalette, sh).inputLine() {
				t.Errorf("%s/%s: styled row carries different TEXT than the unstyled row:\n%q\n%q",
					tier.name, sh.name, plain, inputRowModel(lineedit.MonoPalette, sh).inputLine())
			}
			if w := lineedit.DisplayWidth(stripSGR(got)); m.width >= 1 && w > m.width {
				t.Errorf("%s/%s: the row draws %d cells into a %d-cell window — an SGR byte was "+
					"counted as a cell", tier.name, sh.name, w, m.width)
			}
		}
	}
}

// inputRowMonoGolden is the input row's bytes measured on the PRE-CHANGE tree
// (detached worktree at d6b596a1, probe zz_inputrow_probe_test.go, capture
// /tmp/probe-base.txt in the task's session directory). Each shape was
// byte-identical across all five tiers before the change — the measurement that
// established "the row ignored the palette" — so these strings are the row's
// historic layout, not this change's opinion.
var inputRowMonoGolden = map[string]string{
	"empty":        "you> ▏",
	"short":        "you> hello▏",
	"mid-caret":    "you> hello▏ world",
	"truncated":    "you> hello w",
	"cjk":          "you> 宽度 table▏",
	"wrapped-long": "you> xxxxxxxxxxxxxxx",
}

// TestInputRowMonoBytesAreUnchanged is the regression guard: with colour off the
// row is the pre-change bytes EXACTLY. The claim is by construction (every style
// field is the zero value, so Style.Wrap returns its argument unchanged), and
// this test is what makes a future field that is NOT zero fail loudly instead of
// quietly putting SGR on a monochrome terminal.
func TestInputRowMonoBytesAreUnchanged(t *testing.T) {
	for _, sh := range inputRowShapes {
		want, ok := inputRowMonoGolden[sh.name]
		if !ok {
			t.Fatalf("shape %q has no golden — a shape without a measured pre-change value proves nothing", sh.name)
		}
		for _, pal := range []struct {
			name string
			pal  lineedit.Palette
		}{{"mono", lineedit.MonoPalette}, {"zero-value", lineedit.Palette{}}} {
			got := inputRowModel(pal.pal, sh).inputLine()
			if got != want {
				t.Errorf("%s/%s: input row = %q, want the pre-change bytes %q — the row's bytes are "+
					"not allowed to move when colour is off", pal.name, sh.name, got, want)
			}
		}
	}
}

// TestInputRowPaletteReachesTheCellGrid is the LIVE-SURFACE half: the row is
// rendered by the real model, pushed through the surface's own sink
// (ScreenDumper) and read back as per-cell attrs. A test that only greps
// inputLine()'s string proves SGR was written; this one proves the CELLS carry
// it, which is the only form the operator's terminal can see.
func TestInputRowPaletteReachesTheCellGrid(t *testing.T) {
	const cols, rows = 40, 6
	m := model{width: cols, height: rows}
	m.pal = lineedit.BasicPalette
	m.input = []rune("hi")
	m.cursor = len(m.input)

	sd, err := NewScreenDumper(DumpConfig{Cols: cols, Rows: rows})
	if err != nil {
		t.Fatalf("NewScreenDumper: %v", err)
	}
	if _, err := sd.Write([]byte(m.View())); err != nil {
		t.Fatalf("dumper write: %v", err)
	}
	dump := sd.Snapshot()
	if !dump.AttrsRecorded {
		t.Fatal("AttrsRecorded=false — this build records no attrs, so nothing can be asserted")
	}

	// The row and its prompt column are read from the cell-exact GIRD (one rune
	// per cell, full width, rune index == cell column — ScreenDump.Grid's own
	// contract). The frame is one write whose LF moves down without returning to
	// column 0, so a row can begin at a column that is not 0: the offset is
	// measured, never assumed. (Lines[] is the readable rendering and is NOT
	// column-exact — see the field's own doc.)
	row, promptCol := -1, 0
	for i, grid := range dump.Grid {
		if col := strings.Index(grid, inputPrompt); col >= 0 {
			row, promptCol = i, col
			break
		}
	}
	if row < 0 {
		t.Fatalf("the input row never reached the painted grid:\n%s", strings.Join(dump.Grid, "\n"))
	}
	describe := make([]string, 0, len(dump.AttrRuns[row]))
	for _, run := range dump.AttrRuns[row] {
		describe = append(describe, fmt.Sprintf("%d..%d=%s", run.Col, run.Col+run.Cells-1, run.Style))
	}
	t.Logf("input row %d (prompt at col %d) runs: %v", row, promptCol, describe)
	if len(dump.AttrRuns[row]) < 2 {
		t.Fatalf("the input row carries ONE run (%v) — the typed buffer is not styled at all, so "+
			"the operator's typing row is back on the terminal's default colour", describe)
	}

	// The CELL-LEVEL read on the SAME painted grid the dump was taken from: the
	// row's own cells, asked one by one. Both halves are compared against the
	// table's OWN declared style (read back from the field's sequence), never
	// against a colour typed into this file.
	sc := lineedit.NewScreen(cols, rows)
	sc.Feed([]byte(m.View()))
	wantPrompt := declaredCellStyle(t, lineedit.BasicPalette.Prompt, "BasicPalette.Prompt")
	wantBuffer := declaredCellStyle(t, lineedit.BasicPalette.Buffer, "BasicPalette.Buffer")
	if wantPrompt.Equal(sgrDefaultCell()) || wantBuffer.Equal(sgrDefaultCell()) {
		t.Fatalf("the table declares no colour for one of the halves (%q / %q) — an unstyled half is "+
			"the defect this test exists for", lineedit.BasicPalette.Prompt.Open(), lineedit.BasicPalette.Buffer.Open())
	}
	for col := promptCol; col < promptCol+inputPromptCells; col++ {
		if got := sc.Attr(row, col); !got.Equal(wantPrompt) {
			t.Fatalf("prompt cell %d carries %+v, want the table's Prompt style %+v — the prompt is "+
				"not painted (runs: %v)", col, got, wantPrompt, describe)
		}
	}
	// "hi" is what the operator typed: those cells (and the caret after them)
	// must carry the buffer's style.
	for col := promptCol + inputPromptCells; col < promptCol+inputPromptCells+len("hi"); col++ {
		if got := sc.Attr(row, col); !got.Equal(wantBuffer) {
			t.Fatalf("buffer cell %d carries %+v, want the table's Buffer style %+v — the typed "+
				"buffer is not painted (runs: %v)", col, got, wantBuffer, describe)
		}
	}
	if sc.Attr(row, promptCol).Equal(sc.Attr(row, promptCol+inputPromptCells)) {
		t.Fatalf("the prompt cell and the buffer cell carry the same style — the row reads as one field")
	}
}

// declaredCellStyle reads a palette field's OWN sequence back as the style the
// grid gives a cell: the sequence is fed through lineedit's parser (the ONE SGR
// reader — no second one here) followed by a marker cell, and that cell's style
// is returned. So an assertion can never agree with itself about a colour the
// table does not paint: the expected value comes from the same field the
// renderer wraps text with.
func declaredCellStyle(t *testing.T, st lineedit.Style, what string) sgr.Style {
	t.Helper()
	if st.Open() == "" {
		t.Fatalf("%s declares no style at all — the row would keep the terminal's default colour", what)
	}
	sc := lineedit.NewScreen(1, 1)
	sc.Feed([]byte(st.Open() + "X"))
	return sc.Attr(0, 0)
}

// sgrDefaultCell is the style a cell nobody painted carries.
func sgrDefaultCell() sgr.Style { return sgr.Default() }

// TestInputRowCutLandsOnRuneBoundaries pins the two edges of the split: a cut
// that falls INSIDE the prompt leaves the whole surviving row as prompt, and a
// cut that falls inside the buffer leaves the prompt whole. The wide glyph case
// is the one a byte-index split would corrupt — half a CJK glyph drawn as
// prompt-coloured bytes.
func TestInputRowCutLandsOnRuneBoundaries(t *testing.T) {
	// width 3 cuts inside "you> " — every surviving cell is prompt, and the row
	// carries the prompt's style ONLY (the buffer half emitted no bytes at all).
	m := inputRowModel(lineedit.BasicPalette, inputRowShape{"narrow", 3, "hi", 2})
	got := m.inputLine()
	if plain := stripSGR(got); plain != "you" {
		t.Fatalf("a 3-cell window draws %q, want the prompt's own prefix %q", plain, "you")
	}
	if want := lineedit.BasicPalette.Prompt.Wrap("you"); got != want {
		t.Errorf("a cut inside the prompt drew %q, want exactly the prompt style over the surviving "+
			"cells %q — a buffer style over an empty half is bytes the row does not need", got, want)
	}

	// The rune walk itself: a wide glyph that straddles the boundary stays whole
	// in the tail rather than being cut in half.
	head, tail := splitCells("ab宽cd", 3)
	if head+tail != "ab宽cd" {
		t.Fatalf("splitCells lost bytes: %q + %q", head, tail)
	}
	if head != "ab" || tail != "宽cd" {
		t.Errorf("splitCells(\"ab宽cd\", 3) = %q, %q — want the wide glyph whole in the tail", head, tail)
	}
	// n beyond the string's cells is not an error: everything is head.
	head, tail = splitCells("ab", 99)
	if head != "ab" || tail != "" {
		t.Errorf("splitCells past the end = %q, %q — want the whole string as head", head, tail)
	}
}

// stripSGR removes SGR sequences, leaving the printable text. It exists so a
// styled row can be compared against the layout claim ("the same text in the
// same cells") without restating the palette's bytes.
func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == escByte && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// escByte is the SGR introducer, spelled once (Rule 1).
const escByte = 0x1b
