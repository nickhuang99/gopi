package text2img

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// gridCellW is the cell width the grid tests render at, and gridLh its line
// height: both are EXPLICIT so an expected pixel size is arithmetic, not a
// guess about the renderer's defaults.
const (
	gridCellW = 10
	gridLh    = 24
	gridSize  = 20
	gridMarg  = 12
)

// decodePNGSize decodes w×h from a PNG byte slice — the pixel-level instrument
// for "the image is the grid's size" (an OCR or a vision score cannot see a
// layout regression).
func decodePNGSize(t *testing.T, data []byte) (int, int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

// TestRenderGridBytesSizeIsTheGrid — the image's size is DECIDED by the grid,
// not by the longest line the renderer happens to measure: every cell is one
// gridCellW wide and every row one gridLh tall, so N columns of M rows render to
// exactly N*cell + 2*margin by M*lh + 2*margin. A wrap (which would move text to
// a second line) or a variable advance (which would shift columns between rows)
// breaks this identity — which is the whole point of capturing a terminal.
func TestRenderGridBytesSizeIsTheGrid(t *testing.T) {
	rows := []string{
		"you> hello there",
		strings.Repeat("x", gridCellWidthCols),
		"", // a blank row still occupies its line
	}
	opts := GridOptions{Size: gridSize, CellW: gridCellW, Lh: gridLh, Margin: gridMarg}
	data, res, err := RenderGridBytes(rows, nil, opts)
	if err != nil {
		t.Fatalf("RenderGridBytes: %v", err)
	}
	wantW := gridCellWidthCols*gridCellW + 2*gridMarg
	wantH := len(rows)*gridLh + 2*gridMarg
	gotW, gotH := decodePNGSize(t, data)
	if gotW != wantW || gotH != wantH {
		t.Fatalf("image = %dx%d, want %dx%d (cols=%d rows=%d cell=%d lh=%d)",
			gotW, gotH, wantW, wantH, gridCellWidthCols, len(rows), gridCellW, gridLh)
	}
	if res.Cols != gridCellWidthCols || res.Lines != len(rows) {
		t.Fatalf("summary = cols %d lines %d, want %d/%d", res.Cols, res.Lines, gridCellWidthCols, len(rows))
	}
}

// gridCellWidthCols is the widest row the size test uses.
const gridCellWidthCols = 40

// TestRenderGridBytesNeverWrapsALongRow — the terminal's rows ARE the layout, so
// a row wider than the grid is CLIPPED, never wrapped onto a second line. The
// assertion is the row count: a wrap would make the image taller by one line
// per overflowing row, which a size check catches without reading a pixel.
func TestRenderGridBytesNeverWrapsALongRow(t *testing.T) {
	rows := []string{
		strings.Repeat("A", 10),
		strings.Repeat("B", 500), // far wider than the grid's own widest row
	}
	// The grid's width comes from the widest row (the 500-cell one); the rows
	// after it must not add lines. Compare against the same grid rendered with
	// the long row LAST — a wrap would make the two differ.
	wide := []string{strings.Repeat("B", 500), strings.Repeat("A", 10)}
	opts := GridOptions{Size: gridSize, CellW: gridCellW, Lh: gridLh, Margin: gridMarg}
	_, resA, err := RenderGridBytes(rows, nil, opts)
	if err != nil {
		t.Fatalf("RenderGridBytes: %v", err)
	}
	_, resB, err := RenderGridBytes(wide, nil, opts)
	if err != nil {
		t.Fatalf("RenderGridBytes: %v", err)
	}
	if resA.Lines != len(rows) || resB.Lines != len(wide) {
		t.Fatalf("rows lost: %d/%d from %d/%d input rows — a long row must be clipped, not wrapped",
			resA.Lines, resB.Lines, len(rows), len(wide))
	}
}

// TestRenderGridScrollbackAddsHeaderAndRule — the scrollback band rides ABOVE
// the screen and is closed by a rule row of exactly the screen's width, so the
// image grows by (scrollback + 1) lines and never grows WIDER than the screen
// (a rule drawn to a fixed width would widen the picture — the defect this pins).
func TestRenderGridScrollbackAddsHeaderAndRule(t *testing.T) {
	rows := []string{"screen row one", "screen row two"}
	scroll := []string{"old row A", strings.Repeat("Z", 200)} // longer than the screen
	opts := GridOptions{Size: gridSize, CellW: gridCellW, Lh: gridLh, Margin: gridMarg}

	_, plain, err := RenderGridBytes(rows, nil, opts)
	if err != nil {
		t.Fatalf("RenderGridBytes(no scrollback): %v", err)
	}
	data, withScroll, err := RenderGridBytes(rows, scroll, opts)
	if err != nil {
		t.Fatalf("RenderGridBytes(scrollback): %v", err)
	}

	wantLines := len(rows) + len(scroll) + 1 // + the rule row
	if withScroll.Lines != wantLines {
		t.Fatalf("scrollback render has %d lines, want %d (screen %d + scrollback %d + rule)",
			withScroll.Lines, wantLines, len(rows), len(scroll))
	}
	// The screen's own width decides the image width: the over-long scrollback
	// row is clipped, so the band cannot widen the capture.
	if withScroll.Cols != plain.Cols {
		t.Fatalf("scrollback widened the image: cols %d, want the screen's %d", withScroll.Cols, plain.Cols)
	}
	gotW, gotH := decodePNGSize(t, data)
	wantW := plain.Cols*gridCellW + 2*gridMarg
	wantH := wantLines*gridLh + 2*gridMarg
	if gotW != wantW || gotH != wantH {
		t.Fatalf("scrollback image = %dx%d, want %dx%d", gotW, gotH, wantW, wantH)
	}
}

// TestRenderGridFileWritesTheSameBytes — the file form and the in-memory form
// are one render (a capture must not depend on which entry point it used).
func TestRenderGridFileWritesTheSameBytes(t *testing.T) {
	rows := []string{"alpha", "beta"}
	opts := GridOptions{Size: gridSize, CellW: gridCellW, Lh: gridLh, Margin: gridMarg}
	mem, _, err := RenderGridBytes(rows, nil, opts)
	if err != nil {
		t.Fatalf("RenderGridBytes: %v", err)
	}
	out := filepath.Join(t.TempDir(), "grid.png")
	res, err := RenderGrid(out, rows, nil, opts)
	if err != nil {
		t.Fatalf("RenderGrid: %v", err)
	}
	onDisk, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read rendered file: %v", err)
	}
	if !bytes.Equal(mem, onDisk) {
		t.Fatalf("file render (%d bytes) differs from the in-memory render (%d bytes)", len(onDisk), len(mem))
	}
	if len(res.Outputs) != 1 || res.Outputs[0] != out {
		t.Fatalf("summary outputs = %v, want [%s]", res.Outputs, out)
	}
}

// TestGridRuneCellsAgreesWithLineedit is the anti-drift guard for the ONE rule
// this package mirrors instead of importing: a terminal cell's width. The grid
// renderer must lay out a row exactly as internal/lineedit's cell grid does, or
// a capture's columns would shift away from the screen it claims to show. The
// sampled runes cover zero-width (combining), narrow (ASCII), wide (CJK,
// fullwidth) and the emoji/astral blocks.
func TestGridRuneCellsAgreesWithLineedit(t *testing.T) {
	samples := []struct {
		name string
		s    string
	}{
		{"ascii", "you> hello"},
		{"cjk", "你好世界"},
		{"mixed", "gopi 中文 x1"},
		{"fullwidth", "ＡＢＣ"},
		{"combining", "a\u0301e\u0301"},  // e-acute as base + combining mark
		{"zero-width", "a\u200Bb\u200D"}, // ZWSP / ZWJ
		{"emoji", "🚀🎉"},
		{"box drawing", "┌───┐│└─┘"},
		{"hangul", "한국어"},
	}
	for _, tc := range samples {
		// internal/lineedit's grid is the reference implementation: feed the
		// runes and read the advance the grid's own cursor made.
		want := lineeditCells(t, tc.s)
		got := runeCount(tc.s)
		if got != want {
			t.Errorf("%s (%q): grid width %d, internal/lineedit says %d", tc.name, tc.s, got, want)
		}
	}
}

// lineeditCells asks internal/lineedit's cell grid how many columns s advances
// the cursor — the terminal's own answer, obtained by driving the real Screen.
func lineeditCells(t *testing.T, s string) int {
	t.Helper()
	const cols, rows = 200, 2
	scr := lineedit.NewScreen(cols, rows)
	scr.Feed([]byte(s))
	// The cursor's column IS the advance (no newline is fed).
	return scr.CurCol
}

// TestRenderGridRejectsAnEmptyGrid — a grid with no cells would produce a
// 1x1-ish image that looks like a capture; it is refused instead.
func TestRenderGridRejectsAnEmptyGrid(t *testing.T) {
	_, _, err := RenderGridBytes(nil, nil, GridOptions{Size: gridSize, CellW: gridCellW, Lh: gridLh, Margin: gridMarg})
	if err == nil {
		t.Fatal("an empty grid must not render to an image that looks like a capture")
	}
}

// TestGridImageIsWhiteBackgroundBlackInk — the ink is what a vision model has
// to read: the top-left margin pixel is background, and a row of text puts dark
// pixels somewhere in that row's band (a renderer that painted nothing, or
// painted white-on-white, would fail here where a size check cannot).
func TestGridImageIsWhiteBackgroundBlackInk(t *testing.T) {
	rows := []string{strings.Repeat("M", 10)}
	data, _, err := RenderGridBytes(rows, nil, GridOptions{Size: gridSize, CellW: gridCellW, Lh: gridLh, Margin: gridMarg})
	if err != nil {
		t.Fatalf("RenderGridBytes: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r, g, b, _ := img.At(0, 0).RGBA(); r != 0xffff || g != 0xffff || b != 0xffff {
		t.Fatalf("corner pixel = %v, want white background", img.At(0, 0))
	}
	dark := 0
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if r, g, b, _ := img.At(x, y).RGBA(); r < 0x8000 && g < 0x8000 && b < 0x8000 {
				dark++
			}
		}
	}
	if dark == 0 {
		t.Fatal("no dark pixels: the grid rendered no ink")
	}
	// Sanity bound: 10 glyphs cannot fill the whole page.
	if dark > bounds.Dx()*bounds.Dy()/2 {
		t.Fatalf("%d dark pixels of %d — the render filled the page", dark, bounds.Dx()*bounds.Dy())
	}
}

// TestGridFontIsMonospace — the grid's default face must actually BE monospace,
// or "one global advance per cell" paints a proportional font spread across
// cells (which the size test cannot see, but a reader sees at once).
func TestGridFontIsMonospace(t *testing.T) {
	fpath := gridFont("")
	if fpath == "" {
		t.Skip("no monospace font on this host: the grid path falls back to the system chain")
	}
	data, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read %s: %v", fpath, err)
	}
	f, err := parseTTF(data, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", fpath, err)
	}
	// A proportionally spaced face advances 'i' and 'W' differently; a monospace
	// face advances them identically.
	adv := func(r rune) float64 { return f.advance(f.glyph(r)) }
	if adv('i') != adv('W') || adv('i') != adv('m') {
		t.Fatalf("%s advances i=%.0f W=%.0f m=%.0f — not monospace", fpath, adv('i'), adv('W'), adv('m'))
	}
}

// imageSizeOf is a tiny helper the tests above share when only the size matters.
func imageSizeOf(t *testing.T, img image.Image) (int, int) {
	t.Helper()
	b := img.Bounds()
	return b.Dx(), b.Dy()
}
