// annotate.go — the annotator engine: draw marks (rectangle, point, arrow,
// label chip) onto an EXISTING PNG, and measure what that changed.
//
// WHY IT LIVES HERE. An annotation's only element that is not integer pixel
// writes is its LABEL, and a label is a glyph rasteriser. This package already
// owns one (font chain + 4x supersampled SFNT rasteriser + the glyph blit that
// takes its target as a parameter), so the annotator is a new file in the
// package that owns the painter — never a second rasteriser and never a new
// dependency (Rule 35: zero new module, zero new dependency, no committed font
// blob; the label's font is the system font this package already autodetects).
//
// WHY A NEW BLEND. The package's own blend() derives grey from glyph coverage
// (`return color.RGBA{v, v, v, 255}`, render.go) because it composites black
// ink over the WHITE PAPER it allocated itself. That is correct for a page this
// package made and wrong for a photograph: an annotator must respect the ink's
// colour. So the glyph blit used here calls annotateBlend, which keeps the ink
// RGB and scales only its COVERAGE.
//
// THE CONTRACT (devdoc/capability-annotate.md):
//   - geometry is in OUTPUT-IMAGE PIXELS, always, after the caller has resolved
//     any cell anchor (the cell→pixel conversion belongs to the caller that
//     owns the dump — see cmd/agent/cli_annotate.go);
//   - a mark whose geometry is not fully inside the output image is REFUSED,
//     never clipped (the same rule `gopi tool capture --area` already applies);
//   - a label chip that would not fit the image is REFUSED, naming its box;
//   - N marks requested and 0 pixels changed is a REFUSAL, not a success — the
//     anti-false-success field is a measurement, like Result.Ink.
package text2img

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// The annotator's named numbers (Rule 1: no bare literals).
const (
	// annotateStrokeThick is the stroke width in px, applied by two rules and
	// no third one: a rectangle draws annotateStrokeThick nested outlines, a
	// line draws annotateStrokeThick consecutive raster rows per column.
	annotateStrokeThick = 2
	// annotateHeadPx is the arrowhead's barb length in px.
	annotateHeadPx = 8
	// annotateHeadAngleDeg is the half-angle between the shaft and each barb.
	annotateHeadAngleDeg = 25
	// annotateLabelPadX / PadY pad the label text inside its chip, in px.
	annotateLabelPadX = 6
	annotateLabelPadY = 4
	// annotateLabelGap is the px gap between a mark's box and its chip.
	annotateLabelGap = 6
	// annotateChipInkMax is full 8-bit coverage — the alpha at which a glyph is
	// painted in the ink colour itself.
	annotateChipInkMax = 255

	// The mark ink is one colour, contrasting against both a dark terminal and
	// white paper. Its channels are named (Rule 1).
	markInkR = 220
	markInkG = 30
	markInkB = 30
	// markInkA is opaque: an annotation that let the base show through would be
	// ambiguous about which pixels it claims.
	markInkA = 255
	// annotateLabelFillRGB is the chip's fill — opaque white, so the label is
	// legible over any base.
	annotateLabelFillRGB = 255

	// The `--mark` grammar's own words. They are values compared against, so
	// they are constants, never typed twice (Rules 1, 13).
	markKindRectWord  = "rect"
	markKindArrowWord = "arrow"
	markKindPointWord = "point"
	// MarkAnchorFixedWord is the anchor value meaning "the geometry IS the
	// output rectangle" — no cell grid involved.
	MarkAnchorFixedWord = "fixed"

	// annotateDegToRad converts the barb angle to radians.
	annotateDegToRad = math.Pi / 180
)

// AnnotateIoUFloor is the pixel-agreement floor an annotator's ink must reach
// against an INDEPENDENTLY computed ground truth for the same geometry
// (devdoc/capability-annotate.md §verification). It is one definition, shared
// by that document and the test that asserts it (Rules 13/14) — a floor quoted
// in prose and measured by a different number would be two claims, not one.
const AnnotateIoUFloor = 0.95

// MarkInk is the ink every mark is drawn in.
var MarkInk = Ink{R: markInkR, G: markInkG, B: markInkB}

// LabelInk is the ink a label's text is drawn in.
var LabelInk = Ink{}

// Ink is an opaque ink for compositing onto an EXISTING image. It exists
// because this package's blend() derives grey from glyph coverage — correct on
// the white paper the prose path allocates, wrong as a compositing operator
// over a photograph. An Ink carries the colour a caller asked for.
type Ink struct{ R, G, B uint8 }

// rgba widens the ink to the color.RGBA the package's glyph blit takes.
func (k Ink) rgba() color.RGBA {
	return color.RGBA{R: k.R, G: k.G, B: k.B, A: markInkA}
}

// MarkKind is the shape of one mark. Named constants, never bare strings.
type MarkKind int

const (
	// MarkRect is a rectangle outline.
	MarkRect MarkKind = iota
	// MarkArrow is a shaft with a two-barb head, running along its box's
	// diagonal: from the box's top-left corner to its bottom-right corner. A
	// nearly-horizontal arrow is therefore spelled with a 1px-tall box
	// (`120x1+40+90`), which the shared WxH+X+Y grammar admits because the
	// height is positive.
	MarkArrow
	// MarkPoint is a filled box — the smallest mark that says "here".
	MarkPoint
)

// String renders the kind with the word the `--mark` grammar uses, so an error
// message can be pasted back as an argument.
func (k MarkKind) String() string {
	switch k {
	case MarkRect:
		return markKindRectWord
	case MarkArrow:
		return markKindArrowWord
	case MarkPoint:
		return markKindPointWord
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// ParseMarkKind maps the grammar's word to a kind, refusing anything else by
// name (Rule 15: an unknown kind is never silently a rectangle).
func ParseMarkKind(s string) (MarkKind, error) {
	switch s {
	case markKindRectWord:
		return MarkRect, nil
	case markKindArrowWord:
		return MarkArrow, nil
	case markKindPointWord:
		return MarkPoint, nil
	}
	return MarkRect, fmt.Errorf("unknown mark kind %q (want %s|%s|%s)",
		s, markKindRectWord, markKindArrowWord, markKindPointWord)
}

// Mark is one annotation, with its geometry already resolved to pixels of the
// OUTPUT image.
type Mark struct {
	Kind   MarkKind
	Rect   image.Rectangle
	Label  string
	Anchor string
}

// AnnotateOptions are the label-rendering knobs.
type AnnotateOptions struct {
	// Size is the label font size in px (<=0 → DefaultSize).
	Size int
	// Lh is the label line spacing in px (<=0 → FontScale(Size)).
	Lh int
	// Font pins the metric face ("" → this package's autodetected chain).
	Font string
}

// MarkCount is the number of marks the spec asked for — reported back so a
// caller can assert "N requested, 0 changed".
func (o AnnotateOptions) labelSize() int { return norm(o.Size, DefaultSize) }

// AnnotateResult is the annotator's account of a run. InkPixels is MEASURED
// (pixels whose value differs from the input), never claimed — it is the
// anti-false-success field, the same shape as Result.Ink.
type AnnotateResult struct {
	Path      string
	Input     string
	Width     int
	Height    int
	Marks     int
	Labels    int
	InkPixels int
	// Boxes are the resolved pixel rectangles, in drawing order — the caller
	// that resolved a cell anchor reports what it resolved to.
	Boxes []image.Rectangle
}

// AnnotateFile decodes inPath, draws marks onto it and writes the result to
// outPath. The input is never modified; nothing is written when the run is
// refused.
func AnnotateFile(inPath, outPath string, marks []Mark, o AnnotateOptions) (*AnnotateResult, error) {
	if strings.TrimSpace(outPath) == "" {
		return nil, fmt.Errorf("--out is required: annotate writes a NEW png and never edits the input in place")
	}
	if samePath(inPath, outPath) {
		return nil, fmt.Errorf("--out %s is the input itself: annotating in place would destroy the evidence the mark points at", outPath)
	}
	blob, err := os.ReadFile(inPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read the base image %s: %w", inPath, err)
	}
	img, err := decodeBase(inPath, blob)
	if err != nil {
		return nil, err
	}
	res := &AnnotateResult{
		Path: outPath, Input: inPath,
		Width: img.Bounds().Dx(), Height: img.Bounds().Dy(),
		Marks: len(marks),
	}
	before := make([]byte, len(img.Pix))
	copy(before, img.Pix)

	if err := drawMarks(img, marks, o, res); err != nil {
		return nil, err
	}
	res.InkPixels = changedPixels(before, img.Pix)
	if res.InkPixels == 0 {
		// The package's own rule, inherited: a render that changed nothing is a
		// failed render, and a file written for it would be a false success.
		return nil, fmt.Errorf("annotate: %d mark(s) requested and 0 pixels changed — refusing to write %s",
			len(marks), outPath)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("PNG 编码失败: %w", err)
	}
	if err := os.WriteFile(outPath, buf.Bytes(), annotateFilePerm); err != nil {
		return nil, fmt.Errorf("cannot write %s: %w", outPath, err)
	}
	return res, nil
}

// annotateFilePerm is the mode of the PNG annotate writes (a user deliverable,
// readable by the group the operator belongs to — the same default os.Create
// applies).
const annotateFilePerm = 0o644

// decodeBase decodes the base PNG into an *image.RGBA (the canvas the package's
// glyph blit and the mark writers both take). A decode failure names the PATH:
// "not a PNG" without the file it is not a PNG of is a useless refusal.
func decodeBase(inPath string, blob []byte) (*image.RGBA, error) {
	src, err := png.Decode(bytes.NewReader(blob))
	if err != nil {
		return nil, fmt.Errorf("cannot decode the base image %s as PNG: %w", inPath, err)
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("the base image %s is %dx%d — an empty canvas cannot carry a mark", inPath, b.Dx(), b.Dy())
	}
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)
	return img, nil
}

// samePath reports whether two paths name the same file. It compares the
// cleaned spellings AND, when both exist, the files themselves (so a symlink or
// a relative spelling cannot slip past the in-place refusal).
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

// changedPixels counts the bytes that differ between two RGBA buffers — the
// measurement behind InkPixels.
func changedPixels(before, after []byte) int {
	n := 0
	for i := 0; i+3 < len(before) && i+3 < len(after); i += 4 {
		if before[i] != after[i] || before[i+1] != after[i+1] || before[i+2] != after[i+2] || before[i+3] != after[i+3] {
			n++
		}
	}
	return n
}

// drawMarks composites every mark onto img in a FIXED order — rect, point,
// arrow, then every label on top — so the same arguments produce the same
// pixels (determinism is a tested property, never "the order a map iterated").
func drawMarks(img *image.RGBA, marks []Mark, o AnnotateOptions, res *AnnotateResult) error {
	if len(marks) == 0 {
		return fmt.Errorf("annotate: no --mark given — nothing to draw")
	}
	for _, m := range marks {
		if err := withinImage(img, m.Rect, m); err != nil {
			return err
		}
	}
	for _, kind := range []MarkKind{MarkRect, MarkPoint, MarkArrow} {
		for _, m := range marks {
			if m.Kind != kind {
				continue
			}
			drawMark(img, m)
		}
	}
	for _, m := range marks {
		if strings.TrimSpace(m.Label) == "" {
			continue
		}
		if err := drawLabel(img, m, o); err != nil {
			return err
		}
		res.Labels++
	}
	for _, m := range marks {
		res.Boxes = append(res.Boxes, m.Rect)
	}
	return nil
}

// withinImage refuses a mark whose declared geometry is not fully inside the
// image. Clipping is deliberately NOT offered: the precedent is
// `gopi tool capture --area`, whose off-screen rectangle is refused by name
// ("does not fit"), and an annotation is evidence — a silently clipped box
// changes what it claims to point at.
func withinImage(img *image.RGBA, r image.Rectangle, m Mark) error {
	b := img.Bounds()
	if r.Dx() <= 0 || r.Dy() <= 0 {
		return fmt.Errorf("mark %s %s: geometry is empty (%dx%d)", m.Kind, rectSpelling(r), r.Dx(), r.Dy())
	}
	if r.Min.X < b.Min.X || r.Min.Y < b.Min.Y || r.Max.X > b.Max.X || r.Max.Y > b.Max.Y {
		return fmt.Errorf("mark %s %s does not fit the %dx%d image: the rectangle must be fully inside it, never clipped",
			m.Kind, rectSpelling(r), b.Dx(), b.Dy())
	}
	return nil
}

// rectSpelling renders a rectangle in the same WxH+X+Y spelling the geometry
// grammar accepts, so an error message can be pasted back as an argument.
func rectSpelling(r image.Rectangle) string {
	return fmt.Sprintf("%dx%d+%d+%d", r.Dx(), r.Dy(), r.Min.X, r.Min.Y)
}

// drawMark paints one rect/point/arrow with the mark ink.
func drawMark(img *image.RGBA, m Mark) {
	c := MarkInk.rgba()
	switch m.Kind {
	case MarkPoint:
		fillBox(img, m.Rect, c)
	case MarkArrow:
		x0, y0 := m.Rect.Min.X, m.Rect.Min.Y
		x1, y1 := m.Rect.Max.X-1, m.Rect.Max.Y-1
		drawLine(img, x0, y0, x1, y1, c)
		drawArrowHead(img, x0, y0, x1, y1, c)
	default:
		drawRectOutline(img, m.Rect, c)
	}
}

// fillBox paints every pixel of r in c (the point mark).
func fillBox(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

// drawRectOutline paints annotateStrokeThick nested outlines, the i-th grown
// outward by i px on every side. Growing OUTWARD (not inward) is what keeps a
// small box visible: an inward stroke on a 2px-tall box would leave nothing.
func drawRectOutline(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	for i := 0; i < annotateStrokeThick; i++ {
		rr := image.Rect(r.Min.X-i, r.Min.Y-i, r.Max.X+i, r.Max.Y+i).Intersect(img.Bounds())
		if rr.Empty() {
			continue
		}
		for x := rr.Min.X; x < rr.Max.X; x++ {
			img.SetRGBA(x, rr.Min.Y, c)
			img.SetRGBA(x, rr.Max.Y-1, c)
		}
		for y := rr.Min.Y; y < rr.Max.Y; y++ {
			img.SetRGBA(rr.Min.X, y, c)
			img.SetRGBA(rr.Max.X-1, y, c)
		}
	}
}

// drawLine paints the integer Bresenham raster of the segment, plus, for each
// raster cell, the annotateStrokeThick-1 cells BELOW it — the package's stroke
// rule for a line, so one stroke width looks even on every edge.
func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	b := img.Bounds()
	dx := absInt(x1 - x0)
	dy := -absInt(y1 - y0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		for t := 0; t < annotateStrokeThick; t++ {
			x, y := x0, y0+t
			if x >= b.Min.X && x < b.Max.X && y >= b.Min.Y && y < b.Max.Y {
				img.SetRGBA(x, y, c)
			}
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// drawArrowHead paints the two barbs from the tip back at ±annotateHeadAngleDeg.
func drawArrowHead(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	ang := math.Atan2(float64(y1-y0), float64(x1-x0))
	for _, sign := range []float64{-1, 1} {
		a := ang + math.Pi + sign*annotateHeadAngleDeg*annotateDegToRad
		bx := x1 + int(math.Round(math.Cos(a)*float64(annotateHeadPx)))
		by := y1 + int(math.Round(math.Sin(a)*float64(annotateHeadPx)))
		drawLine(img, x1, y1, bx, by, c)
	}
}

// absInt is the small integer absolute value (math.Abs is for float64).
func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// drawLabel measures the label, places its chip (above the mark, or below when
// there is no room above), refuses a chip that fits nowhere, paints the chip and
// draws the text on it through the package's own glyph blit.
func drawLabel(img *image.RGBA, m Mark, o AnnotateOptions) error {
	size := o.labelSize()
	face, err := LoadFace(o.Font)
	if err != nil {
		return fmt.Errorf("cannot load a font for the label %q: %w", m.Label, err)
	}
	lineHeight := o.Lh
	if lineHeight <= 0 {
		lineHeight = FontScale(size)
	}
	cols, rows := face.Cells(m.Label)
	chip := image.Rect(0, 0,
		cols*CellWidth(size)+2*annotateLabelPadX,
		rows*lineHeight+2*annotateLabelPadY)
	chip = chip.Add(image.Pt(m.Rect.Min.X, m.Rect.Min.Y-annotateLabelGap-chip.Dy()))
	if chip.Min.Y < img.Bounds().Min.Y {
		// No room above: put it below the mark instead of refusing a label that
		// fits the image perfectly well there.
		chip = chip.Add(image.Pt(0, m.Rect.Dy()+annotateLabelGap+chip.Dy()+annotateLabelGap))
	}
	if chip.Min.X < img.Bounds().Min.X || chip.Min.Y < img.Bounds().Min.Y ||
		chip.Max.X > img.Bounds().Max.X || chip.Max.Y > img.Bounds().Max.Y {
		return fmt.Errorf("label %q for mark %s %s: the chip %s does not fit the %dx%d image — shorten the label or move the mark",
			m.Label, m.Kind, rectSpelling(m.Rect), rectSpelling(chip), img.Bounds().Dx(), img.Bounds().Dy())
	}
	fillBox(img, chip, color.RGBA{R: annotateLabelFillRGB, G: annotateLabelFillRGB, B: annotateLabelFillRGB, A: markInkA})
	drawRectOutline(img, chip, MarkInk.rgba())
	baseline := float64(chip.Min.Y+annotateLabelPadY) + face.ascentPx(size)
	if DrawText(img, m.Label, float64(chip.Min.X+annotateLabelPadX), baseline, size, o.Font, LabelInk) == 0 {
		return fmt.Errorf("label %q for mark %s: the font chain has no glyph for the label's runes — refusing to write a chip with no text in it",
			m.Label, m.Kind)
	}
	return nil
}

// Face is a loaded font chain — the measurement handle a caller needs to size a
// label's chip BEFORE drawing it. DrawText loads its own chain internally, so
// the annotator measures with one Face and draws with the same chain's
// autodetection (o.Font pins both when it is set).
type Face struct{ chain fontChain }

// LoadFace loads the package's font chain (o.Font's face when pinned, the
// autodetected chain otherwise) without rendering anything.
func LoadFace(font string) (*Face, error) {
	chain, err := loadFontChain(font)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, fmt.Errorf("no usable font in the chain")
	}
	return &Face{chain: chain}, nil
}

// String names the faces, in order — the diagnostic the refusal messages carry.
func (f *Face) String() string { return f.chain.String() }

// Ascent is the chain's ascender at sizePx — the pen's baseline offset measured
// down from the TOP of the line box. A caller that composites text onto a
// canvas it owns must place the baseline itself, and it must place it with the
// SAME derivation the annotator uses for its label chips (this one): guessing an
// offset is how a caption ends up a few pixels high in one renderer and low in
// another. It is exported for exactly the reason Cells/CellWidth/FontScale are —
// a second package draws with this chain (internal/gifgen's caption band), and
// the alternative to exporting is a mirrored constant in it (Rules 13, 14).
func (f *Face) Ascent(sizePx int) float64 { return f.ascentPx(sizePx) }

// Cells measures text in the package's own cell units: the widest line's rune
// count (the terminal cell rule, runeCount) and the number of lines.
func (f *Face) Cells(text string) (cols, rows int) {
	lines := strings.Split(text, "\n")
	rows = len(lines)
	for _, l := range lines {
		if n := runeCount(l); n > cols {
			cols = n
		}
	}
	return cols, rows
}

// ascentPx is the chain's ascender at sizePx — the pen's baseline offset.
func (f *Face) ascentPx(sizePx int) float64 {
	primary := f.chain[0]
	return primary.ascender * (float64(sizePx) / float64(primary.upem))
}

// CellWidth is the package's monospace cell width for a font size: the same
// derivation the grid path applies (size/defaultCellWidthDivisor, floored at
// 1px) — the ONE place the divisor lives, referenced, never re-typed.
func CellWidth(sizePx int) int {
	w := norm(sizePx, DefaultSize) / defaultCellWidthDivisor
	if w <= 0 {
		w = 1
	}
	return w
}

// FontScale is this package's line-height rule for a font size:
// int(size*lineHeightRatio + 0.5) — the same arithmetic the prose and grid
// paths apply, from the same constant (Rules 1, 13, 14).
func FontScale(sizePx int) int {
	return int(float64(norm(sizePx, DefaultSize))*lineHeightRatio + 0.5)
}

// DrawText draws text onto an EXISTING image at penX/baseline, in ink, and
// returns the number of pixels it inked. It reuses the package's font chain,
// rasteriser and glyph blit; it does NOT create a canvas (contrast
// RenderFile/RenderGrid, which allocate their own image and so cannot be handed
// one). A label containing a newline is laid out on successive lines spaced by
// FontScale(sizePx).
func DrawText(img *image.RGBA, text string, penX, baseline float64, sizePx int, font string, ink Ink) int {
	if img == nil || sizePx <= 0 {
		return 0
	}
	chain, err := loadFontChain(font)
	if err != nil || len(chain) == 0 {
		return 0
	}
	primary := chain[0]
	scale := float64(sizePx) / float64(primary.upem)
	lineHeight := float64(FontScale(sizePx))
	c := ink.rgba()
	inked := 0
	for li, line := range strings.Split(text, "\n") {
		base := baseline + float64(li)*lineHeight
		pen := penX
		for _, r := range line {
			gid := primary.glyph(r)
			inked += annotateBlit(img, chain, sizePx, r, pen, base, c)
			pen += primary.advance(gid) * scale
		}
	}
	return inked
}

// annotateBlit paints rune r through the FIRST chain face that can, compositing
// over whatever the pixel already holds — the annotator's blend rule, which
// honours the ink's RGB (see this file's header for why the package's own
// blend() must not be used here).
func annotateBlit(img *image.RGBA, chain fontChain, sizePx int, r rune, penX, baseline float64, ink color.RGBA) int {
	for _, f := range chain {
		gid := f.glyph(r)
		if gid == 0 {
			continue
		}
		alpha, w, h, ox, oy := f.rasterGlyph(gid, float64(sizePx)/float64(f.upem))
		if w == 0 {
			continue
		}
		return annotateRasterInto(img, alpha, w, h, ox, oy, penX, baseline, ink)
	}
	return 0
}

// annotateRasterInto blits one rasterised glyph box onto img at
// (penX+ox, baseline+oy) and returns how many pixels it inked. Off-image pixels
// are clipped and not counted.
func annotateRasterInto(img *image.RGBA, alpha []byte, w, h, ox, oy int, penX, baseline float64, ink color.RGBA) int {
	if w == 0 || h == 0 {
		return 0
	}
	dx, dy := int(penX)+ox, int(baseline)+oy
	b := img.Bounds()
	inked := 0
	for y := 0; y < h; y++ {
		py := dy + y
		if py < b.Min.Y || py >= b.Max.Y {
			continue
		}
		for x := 0; x < w; x++ {
			px := dx + x
			if px < b.Min.X || px >= b.Max.X {
				continue
			}
			a := alpha[y*w+x]
			if a == 0 {
				continue
			}
			img.SetRGBA(px, py, annotateBlend(img.RGBAAt(px, py), ink, a))
			inked++
		}
	}
	return inked
}

// annotateBlend composites ink over an existing pixel by coverage: full
// coverage is the ink itself, partial coverage mixes ink and base per channel.
// Unlike the package's grey blend, the ink's RGB survives.
func annotateBlend(base, ink color.RGBA, a byte) color.RGBA {
	if a == annotateChipInkMax {
		return color.RGBA{R: ink.R, G: ink.G, B: ink.B, A: markInkA}
	}
	mix := func(dst, src uint8) uint8 {
		return uint8((int(dst)*int(annotateChipInkMax-a) + int(src)*int(a)) / annotateChipInkMax)
	}
	return color.RGBA{
		R: mix(base.R, ink.R),
		G: mix(base.G, ink.G),
		B: mix(base.B, ink.B),
		A: markInkA,
	}
}

// GridCellRect is the image rectangle a CELL SPAN occupies in a grid render —
// the ONE cell→pixel derivation, living beside gridImage so the two cannot
// drift. row/col and rowEnd/colEnd are inclusive-exclusive cell coordinates
// (the renderer's own convention), scrollbackRows is the number of history rows
// the render prepended (0 = none, in which case there is no closing rule row
// either), and o is the SAME GridOptions the render was made with.
//
// The four terms are the renderer's own layout (gridImage): the page margin, the
// row pitch, the cell advance, and the scrollback band plus its one closing rule
// row. They are read from GridOptions by the same normalisation gridImage
// applies, so a caller that passes the options it rendered with gets the
// rectangle the renderer actually used — never an approximation of it.
func GridCellRect(row, col, rowEnd, colEnd, scrollbackRows int, o GridOptions) image.Rectangle {
	size := norm(o.Size, DefaultSize)
	margin := norm(o.Margin, DefaultMargin)
	cellW := o.CellW
	if cellW <= 0 {
		cellW = size / defaultCellWidthDivisor
	}
	if cellW <= 0 {
		cellW = 1
	}
	cellH := o.Lh
	if cellH <= 0 {
		cellH = int(float64(size)*lineHeightRatio + 0.5)
	}
	band := 0
	if scrollbackRows > 0 {
		// The band is the history rows PLUS the rule row that closes it.
		band = scrollbackRows + 1
	}
	return image.Rect(
		margin+col*cellW,
		margin+(band+row)*cellH,
		margin+colEnd*cellW,
		margin+(band+rowEnd)*cellH,
	)
}
