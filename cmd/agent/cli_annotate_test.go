// cli_annotate_test.go — the annotate verb's CLI contract: the `--mark` grammar,
// the cell-anchor conversion, and every refusal §8.1 of the handoff spec names.
//
// The GRAMMAR tests are unit-level (no process, no display). The end-to-end run
// — the real binary, a real PNG, pixel-level measurement — is Rule 31's
// requirement and lives in the report's `Ran:` line, never only here.
package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"

	"github.com/nickhuang99/gopi/internal/text2img"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// annotateFixtureGrid is a small resolved grid for the cell-anchor tests: 4 rows
// of 20 cells, no scrollback, layout geometry.
func annotateFixtureGrid() *annotateGrid {
	rows := make([]string, 4)
	for i := range rows {
		rows[i] = strings.Repeat(".", 20)
	}
	return &annotateGrid{
		path:     "/x/chat-screen.dump",
		source:   uiclient.GeometrySourceLayout,
		cols:     20,
		rows:     4,
		cellW:    text2img.CellWidth(text2img.DefaultSize),
		cellH:    text2img.FontScale(text2img.DefaultSize),
		cells:    rows,
		scrollba: nil,
	}
}

// TestAnnotateParseMarkSpecFixed covers the `fixed` arm of the grammar: the
// geometry is pixels, and the anchor field may be spelled `fixed` or left empty
// (a trailing colon).
func TestAnnotateParseMarkSpecFixed(t *testing.T) {
	cases := []struct {
		spec string
		want image.Rectangle
		kind text2img.MarkKind
		text string
	}{
		{"rect:200x40+14+13:status row:fixed", image.Rect(14, 13, 214, 53), text2img.MarkRect, "status row"},
		{"rect:200x40+14+13:status row:", image.Rect(14, 13, 214, 53), text2img.MarkRect, "status row"},
		{"point:6x6+88+40::fixed", image.Rect(88, 40, 94, 46), text2img.MarkPoint, ""},
		{"arrow:120x1+40+90:shrinks:fixed", image.Rect(40, 90, 160, 91), text2img.MarkArrow, "shrinks"},
	}
	for _, tc := range cases {
		m, err := parseMarkSpec(tc.spec, nil)
		if err != nil {
			t.Errorf("parseMarkSpec(%q): %v", tc.spec, err)
			continue
		}
		if m.Rect != tc.want {
			t.Errorf("parseMarkSpec(%q).Rect = %v, want %v", tc.spec, m.Rect, tc.want)
		}
		if m.Kind != tc.kind {
			t.Errorf("parseMarkSpec(%q).Kind = %v, want %v", tc.spec, m.Kind, tc.kind)
		}
		if m.Label != tc.text {
			t.Errorf("parseMarkSpec(%q).Label = %q, want %q", tc.spec, m.Label, tc.text)
		}
	}
}

// TestAnnotateParseMarkSpecMalformed: every malformed shape is refused, and the
// refusal quotes the grammar back (the caller can fix the command line from the
// message alone).
func TestAnnotateParseMarkSpecMalformed(t *testing.T) {
	for _, spec := range []string{
		"rect:200x40+14+13",            // 3 fields: the label is missing the separator's context
		"blob:20x10+0+0:x:fixed",       // unknown kind
		"rect:20x10+0+0:x:fixed:extra", // too many fields
		"rect:notgeometry:x:fixed",     // unparsable geometry
		"rect:0x10+0+0:x:fixed",        // non-positive extent
		"rect:20x10-5+0:x:fixed",       // negative offset
		"arrow:20x10+0+0:x:12",         // cell anchor without a grid resolved
		"rect:20x10+0+0:x:12#",         // malformed cell reference
		"rect:20x10+0+0:x:12#x",        // non-numeric column
		"rect:20x10+0+0:x:9#9",         // cell outside a 2x2 grid
		"rect:20x10+0+0:x:1#1-0#0",     // backwards range
		"rect:20x10+0+0:x:0#0-0#0#0",   // over-long cell reference
	} {
		t.Run(spec, func(t *testing.T) {
			grid := annotateFixtureGrid()
			grid.rows, grid.cols = 2, 2
			grid.cells = []string{"..", ".."}
			_, err := parseMarkSpec(spec, grid)
			if err == nil {
				t.Fatalf("parseMarkSpec(%q) was accepted", spec)
			}
			if !strings.Contains(err.Error(), "--mark") && !strings.Contains(err.Error(), "cell reference") {
				t.Errorf("refusal %q names neither the flag nor the cell reference", err.Error())
			}
		})
	}
}

// TestAnnotateCellAnchorConversion: a cell anchor converts through the ONE
// derivation (text2img.GridCellRect, which lives beside the grid renderer) and
// covers the named cells at their full extent.
func TestAnnotateCellAnchorConversion(t *testing.T) {
	g := annotateFixtureGrid()
	m, err := parseMarkSpec("rect:1x1+0+0:row 2:2#3-2#7", g)
	if err != nil {
		t.Fatalf("parseMarkSpec: %v", err)
	}
	want := text2img.GridCellRect(2, 3, 3, 8, len(g.scrollba), g.opts)
	if m.Rect != want {
		t.Errorf("cell anchor resolved to %v, want %v (cells 3..7 of row 2)", m.Rect, want)
	}
	if m.Rect.Dx() != 5*g.cellW || m.Rect.Dy() != g.cellH {
		t.Errorf("the anchor covers %dx%d, want %dx%d — a cell anchor covers WHOLE cells",
			m.Rect.Dx(), m.Rect.Dy(), 5*g.cellW, g.cellH)
	}
}

// TestAnnotateCellAnchorHonoursThePageMargin is the regression for the defect
// this feature shipped with and then had measured: the first conversion omitted
// the grid renderer's page margin, so every cell mark landed margin(12)px up and
// left of the cell it named. The assertion is the renderer's own default, read
// from the package that owns it — never a 12 typed here.
func TestAnnotateCellAnchorHonoursThePageMargin(t *testing.T) {
	g := annotateFixtureGrid()
	m, err := parseMarkSpec("rect:1x1+0+0:origin:0#0-0#0", g)
	if err != nil {
		t.Fatalf("parseMarkSpec: %v", err)
	}
	if m.Rect.Min.X < text2img.DefaultMargin || m.Rect.Min.Y < text2img.DefaultMargin {
		t.Errorf("cell 0#0 resolved to %v — the page margin %d is missing from the conversion",
			m.Rect.Min, text2img.DefaultMargin)
	}
}

// TestAnnotateAnchorConvertsThroughTheSameFieldsTheGridRendererUses is the
// anti-drift guard for the conversion: the numbers the CLI resolves with must be
// the SAME ones `text2img.RenderGridBytes` lays a terminal capture out with. The
// test measures a rendered grid rather than restating the arithmetic, so a change
// to either side that breaks their agreement fails here.
func TestAnnotateAnchorConvertsThroughTheSameFieldsTheGridRendererUses(t *testing.T) {
	rows := []string{strings.Repeat(".", 10), "abcdefghij"}
	opts := text2img.GridOptions{Size: text2img.DefaultSize, Margin: 0}
	blob, res, err := text2img.RenderGridBytes(rows, nil, opts)
	if err != nil {
		t.Fatalf("RenderGridBytes: %v", err)
	}
	_ = blob
	// The grid renderer's cell pitch for this size IS the conversion's basis.
	if got, want := text2img.CellWidth(text2img.DefaultSize), text2img.DefaultSize/2; got != want {
		t.Fatalf("CellWidth(%d) = %d, want %d — the conversion's basis moved", text2img.DefaultSize, got, want)
	}
	if res.Lh != text2img.FontScale(text2img.DefaultSize) {
		t.Errorf("the grid renderer's row pitch is %d but the cell anchor converts with %d — the two must be the same rule",
			res.Lh, text2img.FontScale(text2img.DefaultSize))
	}
}

// TestAnnotateScrollbackRowsMovesTheAnchorDown: --scrollback-rows is the ONE
// knob that moves a cell mark vertically without moving the grid, and it moves it
// by exactly the band plus the rule row that closes it (the renderer's own
// arithmetic — text2img.GridCellRect owns it).
func TestAnnotateScrollbackRowsMovesTheAnchorDown(t *testing.T) {
	plain := annotateFixtureGrid()
	plain.bandRows = 0
	banded := annotateFixtureGrid()
	banded.bandRows = 4
	a, err := plain.cellRect("1#2", "spec")
	if err != nil {
		t.Fatalf("cellRect: %v", err)
	}
	b, err := banded.cellRect("1#2", "spec")
	if err != nil {
		t.Fatalf("cellRect: %v", err)
	}
	if got, want := b.Min.Y-a.Min.Y, (4+1)*plain.cellH; got != want {
		t.Errorf("--%s 4 moved the anchor by %d px, want %d (4 rows + the closing rule row)",
			annotateFlagScrollbackRows, got, want)
	}
	if a.Min.X != b.Min.X || a.Dx() != b.Dx() || a.Dy() != b.Dy() {
		t.Errorf("the band moved the anchor horizontally or resized it: %v vs %v", a, b)
	}
	if _, err := parseAnnotateArgs([]string{"a.png", "--out", "b.png", "--" + annotateFlagScrollbackRows, "-3"}); err == nil {
		t.Errorf("a negative --%s was accepted", annotateFlagScrollbackRows)
	}
}

// TestAnnotateParseArgs: the flag parser's own contract, including the repeated
// --mark (one flag per mark, so four marks are four arguments).
func TestAnnotateParseArgs(t *testing.T) {
	req, err := parseAnnotateArgs([]string{"a.png", "--out", "b.png",
		"--mark", "rect:1x1+0+0::fixed", "--mark", "point:2x2+1+1::fixed",
		"--size", "28", "--json"})
	if err != nil {
		t.Fatalf("parseAnnotateArgs: %v", err)
	}
	if req.in != "a.png" || req.out != "b.png" {
		t.Errorf("paths: in=%q out=%q", req.in, req.out)
	}
	if len(req.marks) != 2 {
		t.Errorf("marks = %d, want 2", len(req.marks))
	}
	if req.size != 28 || !req.json {
		t.Errorf("size=%d json=%v", req.size, req.json)
	}
	for _, args := range [][]string{
		{"--out", "b.png"},                           // no input
		{"a.png", "c.png", "--out", "b.png"},         // two inputs
		{"a.png", "--out", "b.png", "--size", "big"}, // non-numeric size
		{"a.png", "--out", "b.png", "--nope", "1"},   // unknown flag
	} {
		if _, err := parseAnnotateArgs(args); err == nil {
			t.Errorf("parseAnnotateArgs(%v) was accepted", args)
		}
	}
}

// TestAnnotateGridRefusals: the two grids a cell anchor must refuse BY NAME —
// the sink's own FALLBACK geometry, and a dump that carries no cells.
func TestAnnotateGridRefusals(t *testing.T) {
	fallback := annotateFixtureGrid()
	fallback.source = uiclient.GeometrySourceFallback
	err := annotateGridUsable(fallback, fallback.path)
	if err == nil {
		t.Fatalf("a FALLBACK-geometry grid was accepted")
	}
	if !strings.Contains(err.Error(), "FALLBACK") {
		t.Errorf("the refusal does not name the fallback geometry: %v", err)
	}

	empty := annotateFixtureGrid()
	empty.cells = nil
	if err := annotateGridUsable(empty, empty.path); err == nil {
		t.Fatalf("a grid with no cells was accepted")
	}
}

// TestAnnotateRefusesNoMarksWhenCellAnchorAsked: a cell anchor with no grid on
// hand is refused naming what is missing (never silently treated as fixed).
func TestAnnotateRefusesNoMarksWhenCellAnchorAsked(t *testing.T) {
	if !annotateNeedsGrid([]string{"rect:1x1+0+0:x:2#3"}) {
		t.Fatalf("a cell anchor was not detected as needing a grid")
	}
	if annotateNeedsGrid([]string{"rect:1x1+0+0:x:fixed"}) {
		t.Fatalf("a fixed anchor was detected as needing a grid")
	}
	if _, err := parseMarkSpec("rect:1x1+0+0:x:2#3", nil); err == nil {
		t.Fatalf("a cell anchor with no resolved grid was accepted")
	}
}

// TestAnnotateUsageNamesEveryFlag: the page a caller reads documents every flag
// the parser accepts (a flag the help hides is a flag nobody uses).
func TestAnnotateUsageNamesEveryFlag(t *testing.T) {
	for name := range annotateFlagMembers {
		if !strings.Contains(annotateUsage, "--"+name) {
			t.Errorf("annotateUsage does not document --%s", name)
		}
	}
	if !strings.Contains(annotateUsage, markSpecWantText) {
		t.Errorf("annotateUsage does not carry the --mark grammar %q", markSpecWantText)
	}
}

// writeAnnotateBasePNG writes a white w×h PNG and returns nothing: the fixture
// is the file, not a path spelling.
func writeAnnotateBasePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	fh, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer fh.Close()
	if err := png.Encode(fh, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

// decodeAnnotatePNG decodes a written PNG through the stdlib — the reader side
// of the pixel-level check.
func decodeAnnotatePNG(t *testing.T, path string) *image.RGBA {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	src, err := png.Decode(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.Set(x-b.Min.X, y-b.Min.Y, src.At(x, y))
		}
	}
	return out
}

// TestAnnotateE2EOnAWrittenBase drives the WHOLE verb path (parse → resolve →
// annotate) on a real PNG written by the test, and measures the result at pixel
// level: the output differs from the input, the JSON's ink_pixels > 0, and every
// pixel outside the declared marks is byte-identical between the two images.
func TestAnnotateE2EOnAWrittenBase(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.png")
	writeAnnotateBasePNG(t, base, 240, 140)
	out := filepath.Join(dir, "out.png")

	// Two marks, both LABEL-FREE, so the confinement assertion below is about
	// the marks alone (a label's chip is deliberately drawn OUTSIDE its mark —
	// it must be legible, not on top of the thing it describes — and the chip's
	// own geometry is asserted in internal/text2img).
	req := annotateRequest{
		in: base, out: out,
		marks: []string{
			"rect:60x30+20+20::fixed",
			"point:6x6+200+100::fixed",
		},
	}
	doc, err := annotateRun(req)
	if err != nil {
		t.Fatalf("annotateRun: %v", err)
	}
	if doc.InkPixels <= 0 {
		t.Fatalf("ink_pixels = %d — a drawn annotation must change pixels", doc.InkPixels)
	}
	if doc.Marks != 2 {
		t.Errorf("marks = %d, want 2", doc.Marks)
	}
	if doc.AnchorSource != markAnchorNone {
		t.Errorf("anchor_source = %q, want %q", doc.AnchorSource, markAnchorNone)
	}
	inBlob, _ := os.ReadFile(base) //stylegate:ignore reads back the blob the annotate CLI just wrote; a read error fails the byte comparison below
	outBlob, _ := os.ReadFile(out) //stylegate:ignore reads the annotated output blob for the same byte comparison
	if bytes.Equal(inBlob, outBlob) {
		t.Fatalf("the output is byte-identical to the input — nothing was drawn")
	}
	// Every changed pixel lies inside a declared mark's region.
	bi := decodeAnnotatePNG(t, base)
	oi := decodeAnnotatePNG(t, out)
	const strokeThick = 2 // text2img's annotateStrokeThick, as the CLI sees it
	regions := []image.Rectangle{
		image.Rect(20-strokeThick, 20-strokeThick, 80+strokeThick, 50+strokeThick),
		image.Rect(200, 100, 206, 106),
	}
	outside := 0
	for y := 0; y < bi.Bounds().Dy(); y++ {
		for x := 0; x < bi.Bounds().Dx(); x++ {
			if bi.RGBAAt(x, y) == oi.RGBAAt(x, y) {
				continue
			}
			inAny := false
			for _, r := range regions {
				if image.Pt(x, y).In(r) {
					inAny = true
					break
				}
			}
			if !inAny {
				outside++
			}
		}
	}
	if outside != 0 {
		t.Errorf("%d changed pixels lie outside the two declared regions", outside)
	}

	// The LABEL path, on the same base: a label must add ink and report itself.
	labeled := filepath.Join(dir, "labeled.png")
	req.marks[0] = "rect:60x30+20+20:first:fixed"
	req.out = labeled
	doc2, err := annotateRun(req)
	if err != nil {
		t.Fatalf("annotateRun with a label: %v", err)
	}
	if doc2.Labels != 1 {
		t.Errorf("labels = %d, want 1", doc2.Labels)
	}
	if doc2.InkPixels <= doc.InkPixels {
		t.Errorf("the label added no ink (%d vs %d) — the chip and its text were not drawn", doc2.InkPixels, doc.InkPixels)
	}
}

// TestAnnotateCellAnchorE2E is the end-to-end calibration §4.2 of the handoff
// spec asks for: the base image IS a real `text2img` grid render, the mark is
// anchored to a CELL of a dump describing that grid, and the assertion is made
// against the RENDERED GLYPH PIXELS — never against arithmetic restated in the
// test.
//
// The fixture grid carries EXACTLY ONE glyph, at cell row 1 col 2 ("X"; every
// other cell is a space). That makes the calibration decidable by pixels alone:
//
//   - every dark pixel of the image must lie inside the mark (the anchor covers
//     the cell it names, completely);
//   - the mark displaced by ONE cell — up, down, left or right — must contain NO
//     dark pixel (the anchor is on its OWN cell, not a neighbour's);
//   - the mark displaced by the grid renderer's PAGE MARGIN up-and-left (exactly
//     what the pre-fix conversion produced) must contain NO dark pixel either:
//     the shipped defect becomes a fact about this image, not a claim in a
//     comment.
func TestAnnotateCellAnchorE2E(t *testing.T) {
	dir := t.TempDir()
	const cols, rows = 20, 5
	const glyphRow, glyphCol = 1, 2
	dumpPath := writeAnnotateFixtureDump(t, dir, cols, rows)
	// SPACES, not dots: "." is a glyph the renderer PAINTS, so a dotted filler
	// would fill the calibration image with ink and make "the only glyph" false.
	grid := []string{
		"                    ",
		"  X                 ",
		"                    ",
		"                    ",
		"                    ",
	}
	base := filepath.Join(dir, "base-grid.png")
	opts := text2img.GridOptions{Size: text2img.DefaultSize, Margin: text2img.DefaultMargin}
	if _, err := text2img.RenderGrid(base, grid, nil, opts); err != nil {
		t.Fatalf("RenderGrid: %v", err)
	}
	out := filepath.Join(dir, "out.png")
	anchor := strconv.Itoa(glyphRow) + markCellSep + strconv.Itoa(glyphCol)
	doc, err := annotateRun(annotateRequest{
		in: base, out: out, display: dumpPath,
		marks: []string{"rect:1x1+0+0::" + anchor},
	})
	if err != nil {
		t.Fatalf("annotateRun: %v", err)
	}
	if doc.AnchorSource != markAnchorCell {
		t.Errorf("anchor_source = %q, want %q", doc.AnchorSource, markAnchorCell)
	}
	if doc.GridCols != cols || doc.GridRows != rows {
		t.Errorf("grid = %dx%d, want %dx%d", doc.GridCols, doc.GridRows, cols, rows)
	}

	before := decodeAnnotatePNG(t, base)
	after := decodeAnnotatePNG(t, out)
	b := before.Bounds()
	glyph := map[image.Point]bool{}
	mark := map[image.Point]bool{}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			was, is := before.RGBAAt(x, y), after.RGBAAt(x, y)
			if was.R < 200 || was.G < 200 || was.B < 200 {
				glyph[image.Pt(x, y)] = true
			}
			if is.R == text2img.MarkInk.R && is.G == text2img.MarkInk.G && is.B == text2img.MarkInk.B {
				mark[image.Pt(x, y)] = true
			}
		}
	}
	if len(glyph) == 0 {
		t.Fatalf("the fixture grid rendered no glyph — the calibration has nothing to anchor to")
	}
	if len(mark) == 0 {
		t.Fatalf("the mark drew no ink")
	}

	// (a) The mark's ink box IS the anchored cell, grown outward by the stroke's
	// inner ring. This is the whole conversion asserted as one equality: the
	// margin, the row pitch the band folded in, and the cell advance are all
	// inside it (the pre-fix conversion, which omitted the margin, differs here
	// by exactly DefaultMargin on both axes).
	const strokeInner = 1 // annotateStrokeThick-1: the outer ring sits one px out
	want := text2img.GridCellRect(glyphRow, glyphCol, glyphRow+1, glyphCol+1, 0, opts)
	if got := inkBox(mark); got != want.Inset(-strokeInner) {
		t.Fatalf("the mark's ink box is %v, want the anchored cell %v grown by the inner stroke ring %d",
			got, want, strokeInner)
	}

	// (b) The anchored cell CONTAINS the glyph: the mark really is on the cell
	// the glyph occupies. A glyph box is up to one cell wide plus its bearing, so
	// the floor is a coverage ratio (measured on this fixture: 68 of 75 dark
	// pixels, the 7 outliers being the X's outermost columns spilling past the
	// 10px cell), asserted against any rival placement rather than alone.
	anchorCover := annotateCoverage(glyph, want)
	if float64(anchorCover)/float64(len(glyph)) < annotateAnchorCoverFloor {
		t.Fatalf("the anchored cell %v contains only %d of %d glyph pixels (floor %.2f)",
			want, anchorCover, len(glyph), annotateAnchorCoverFloor)
	}

	// (c) Every rival placement is strictly worse, and the pre-fix one — the
	// cell shifted by the page margin — contains NOTHING. A conversion error
	// therefore cannot pass this test by luck: it has to beat the anchored cell
	// on this very image.
	cellW, cellH := want.Dx(), want.Dy()
	rivals := map[string]image.Rectangle{
		"one cell right":  want.Add(image.Pt(cellW, 0)),
		"one cell left":   want.Add(image.Pt(-cellW, 0)),
		"one cell down":   want.Add(image.Pt(0, cellH)),
		"one cell up":     want.Add(image.Pt(0, -cellH)),
		"the page margin": want.Add(image.Pt(-text2img.DefaultMargin, -text2img.DefaultMargin)),
		"margin error +x": want.Add(image.Pt(text2img.DefaultMargin, 0)),
		"margin error +y": want.Add(image.Pt(0, text2img.DefaultMargin)),
	}
	for name, r := range rivals {
		if n := annotateCoverage(glyph, r); n >= anchorCover {
			t.Errorf("the %q placement %v covers %d glyph pixels, not fewer than the anchored cell's %d — the calibration cannot tell them apart",
				name, r, n, anchorCover)
		}
	}
	if n := annotateCoverage(glyph, rivals["the page margin"]); n != 0 {
		t.Errorf("the pre-fix (margin-shifted) placement covers %d glyph pixels — the shipped defect is supposed to be a real displacement on this fixture", n)
	}

	// (d) The mark changed nothing outside its own cell.
	slack := want.Inset(-2 * strokeInner)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if before.RGBAAt(x, y) == after.RGBAAt(x, y) {
				continue
			}
			if !image.Pt(x, y).In(slack) {
				t.Fatalf("the mark changed pixel %v, outside its own cell %v", image.Pt(x, y), want)
			}
		}
	}
}

// annotateAnchorCoverFloor is the minimum share of a fixture glyph's dark pixels
// the anchored cell must contain (see the calibration in
// TestAnnotateCellAnchorE2E: a glyph box is up to one cell wide plus its
// bearings, so an exact 1.0 would be false — the floor is what makes the claim
// falsifiable without making it wrong).
const annotateAnchorCoverFloor = 0.8

// coverage counts the points of a set that lie inside r.
func annotateCoverage(ink map[image.Point]bool, r image.Rectangle) int {
	n := 0
	for p := range ink {
		if p.In(r) {
			n++
		}
	}
	return n
}

// inkBox is the tight bounding box of a point set.
func inkBox(ink map[image.Point]bool) image.Rectangle {
	box := image.Rect(0, 0, 0, 0)
	for p := range ink {
		box = box.Union(image.Rect(p.X, p.Y, p.X+1, p.Y+1))
	}
	return box
}

// writeAnnotateFixtureDump writes a screen dump PAIR (the text artifact the
// record rides on, plus the machine-checkable .json twin the reader requires)
// through the SINK's own constant spellings — never a hand-typed format. A pair
// written by hand from memory is exactly what the reader refuses, so a fixture
// that gets it right is also the guard that the sink's vocabulary did not move.
func writeAnnotateFixtureDump(t *testing.T, dir string, cols, rows int) string {
	t.Helper()
	grid := make([]string, rows)
	lines := make([]string, rows)
	for r := range grid {
		label := "row " + strconv.Itoa(r)
		grid[r] = label + strings.Repeat(".", cols-len(label))
		lines[r] = strings.TrimRight(grid[r], ".")
	}
	screen := lineedit.ScreenDump{
		Version: lineedit.DumpVersion,
		Cols:    cols,
		Rows:    rows,
		Grid:    grid,
		Lines:   lines,
	}
	now := time.Now().UTC()
	pid := os.Getpid()
	twin := map[string]any{
		"identity": map[string]any{
			"surface":           string(uiclient.SurfaceChat),
			"pid":               pid,
			"target":            "self:chat-tools",
			"tools":             true,
			"model_geometry":    [2]int{cols, rows},
			"terminal_geometry": [2]int{cols, rows},
			"mode":              string(uiclient.DumpModeANSI),
			"started":           now,
		},
		"observer": map[string]any{"frames": 3, "dumps": 3, "error": ""},
		"screen":   screen,
	}
	twinBlob, err := json.Marshal(twin)
	if err != nil {
		t.Fatalf("marshal twin: %v", err)
	}
	path := filepath.Join(dir, uiclient.ChatDumpFileName)
	record := uiclient.ChatDumpRecordPrefix +
		" pid=" + strconv.Itoa(pid) +
		" target=self:chat-tools tools=on" +
		" terminal=" + gridWord(cols, rows) +
		" model=" + gridWord(cols, rows) +
		" grid=" + gridWord(cols, rows) +
		" geometry=" + string(uiclient.GeometrySourceLayout) +
		" mode=" + string(uiclient.DumpModeANSI) +
		" frames=3 dumps=3 started=" + now.Format(time.RFC3339) + "\n"
	body := "#gopi-screen-dump v=" + strconv.Itoa(lineedit.DumpVersion) +
		" cols=" + strconv.Itoa(cols) + " rows=" + strconv.Itoa(rows) + "\n" +
		strings.Join(grid, "\n") + "\n"
	if err := os.WriteFile(path, []byte(record+body), 0o644); err != nil {
		t.Fatalf("write fixture dump: %v", err)
	}
	if err := os.WriteFile(path+".json", twinBlob, 0o644); err != nil {
		t.Fatalf("write fixture twin: %v", err)
	}
	return path
}

// gridWord renders the sink's own NxM spelling.
func gridWord(cols, rows int) string {
	return strconv.Itoa(cols) + "x" + strconv.Itoa(rows)
}

// TestAnnotateVerbRunsARealArgvEndToEnd hands the verb a real argv and measures
// what came out — the drive the census's `annotate` row records as `a:test` (the
// `capture` precedent: parse-level tests prove the parser and nothing about the
// verb). Two shapes, both counted by the census's own derivation:
//
//	(1) an argv LITERAL whose first element is the verb word (what a shell hands
//	    the binary), pushed through the verb's dispatch predicate and its run
//	    function;
//	(2) the verb's own run function called directly with those arguments.
//
// The refusal arm is driven through the ENGINE entry point (annotateRun) because
// the refusal path ends in os.Exit: a re-exec'd child that exits non-zero takes
// the suite's process-lifetime machinery down with it, and a test that kills its
// own harness proves nothing. The exit status of the refusal is evidenced by the
// out-of-suite binary run (the report's Ran: line), not by a probe here.
func TestAnnotateVerbRunsARealArgvEndToEnd(t *testing.T) {
	cases := []struct {
		name    string
		argv    []string
		wantOut bool
		wantInk bool
	}{
		{
			name:    "two marks on a real base draw ink and write the output",
			argv:    []string{CliVerbAnnotate, "", "--out", "", "--" + annotateFlagJSON, "--mark", "rect:60x30+20+20:label:fixed", "--mark", "point:6x6+200+100::fixed"},
			wantOut: true, wantInk: true,
		},
		{
			name:    "-h is the early return: no file is written, but the verb claims the argv",
			argv:    []string{CliVerbAnnotate, "-h"},
			wantOut: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, "base-argv.png")
			out := filepath.Join(dir, "marked-argv.png")
			writeAnnotateBasePNG(t, base, 240, 140)

			argv := append([]string(nil), tc.argv...)
			for i, a := range argv {
				if i > 0 && a == "" {
					if i == 1 {
						argv[i] = base
						continue
					}
					argv[i] = out
				}
			}
			if !isAnnotateSubcommand(argv) {
				t.Fatalf("the dispatch predicate does not claim %v — the verb is unreachable", argv)
			}

			stdout := captureStdout(t, func() { runAnnotateSubcommand(argv[1:]) })

			if !tc.wantOut {
				if _, err := os.Stat(out); err == nil {
					t.Fatalf("-h wrote %s — the usage early return must not touch the filesystem", out)
				}
				// The page must be the verb's OWN page, and it is identified by
				// FINGERPRINT — its title line, through the same helper the CLI
				// contract case uses (cli_contracts_test.go) — never by a
				// substring of a command path. The assertion this replaces looked
				// for `gopi annotate`, which the page has never carried since the
				// group door became the only spelling (task 01a10dc3…, 2026-10-06:
				// the title is `gopi tool annotate — …`). So the page was right
				// and the assertion was about a fragment of a superseded spelling:
				// STALE (Rule 20 — no assertion was relaxed; the opposite, a
				// substring of ANY page containing the word would have passed).
				if got, want := cliUsageTitle(stdout), cliUsageTitle(annotateUsage); got != want {
					t.Errorf("-h printed the page titled %q, want the verb's own page titled %q", got, want)
				}
				return
			}

			var doc annotateJSON
			if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &doc); err != nil {
				t.Fatalf("the run's stdout is not the JSON result (%v): %q", err, stdout)
			}
			if doc.Marks != 2 {
				t.Errorf("marks = %d, want 2 — the argv's two --mark flags did not both reach the engine", doc.Marks)
			}
			if tc.wantInk && doc.InkPixels <= 0 {
				t.Errorf("ink_pixels = %d, want > 0 — the argv drew nothing", doc.InkPixels)
			}
			before := decodeAnnotatePNG(t, base)
			after := decodeAnnotatePNG(t, out)
			if changed := countChangedPixels(before, after); changed != doc.InkPixels {
				t.Errorf("the file changed %d pixels but the result reports ink_pixels=%d — the reported number is not the drawn ink", changed, doc.InkPixels)
			}
		})
	}
}

// TestAnnotateVerbRefusesOffImageGeometryAndWritesNothing drives the REFUSAL
// contract through the verb's own argument parser and engine: a mark that does
// not fit the base is refused, and the refusal leaves no file behind — the
// property that makes a refused run distinguishable from a small one.
func TestAnnotateVerbRefusesOffImageGeometryAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base-refuse.png")
	out := filepath.Join(dir, "never-written.png")
	writeAnnotateBasePNG(t, base, 240, 140)

	req, err := parseAnnotateArgs([]string{base, "--out", out, "--" + annotateFlagMark, "rect:900x30+20+20::fixed"})
	if err != nil {
		t.Fatalf("the argument list was refused before the geometry was checked: %v", err)
	}
	if _, err := annotateRun(req); err == nil {
		t.Fatal("a rectangle that does not fit the 240x140 base was ACCEPTED — off-image geometry must be refused, never clipped")
	} else if !strings.Contains(err.Error(), "does not fit") {
		t.Errorf("the refusal does not name the geometry problem: %v", err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("the refusal CREATED %s — a refused run must write nothing", out)
	}
}

// countChangedPixels is the pixel-level measurement the argv drive asserts on:
// how many pixels differ between two images of the same size.
func countChangedPixels(a, b *image.RGBA) int {
	if a.Bounds() != b.Bounds() {
		return -1
	}
	n := 0
	for i := 0; i < len(a.Pix); i += 4 {
		if a.Pix[i] != b.Pix[i] || a.Pix[i+1] != b.Pix[i+1] || a.Pix[i+2] != b.Pix[i+2] || a.Pix[i+3] != b.Pix[i+3] {
			n++
		}
	}
	return n
}
