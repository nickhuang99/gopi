// annotate_test.go — the annotator's tests: what it draws, what it refuses, and
// the PIXEL-level verification of both.
//
// THE METHOD (Rule 10.2, 渲染真值): a render claim is settled by comparing PIXELS
// against an independently computed ground truth, never by OCR and never by a
// vision model — neither can measure whether a stroke landed where it was
// declared. Concretely:
//
//   - the GROUND TRUTH is built here in the test, by a rasteriser that shares no
//     code with the annotator (annotateRefRect / annotateRefLine / annotateRefBox
//     below are plain integer loops over a fresh image), so agreement is evidence
//     about the geometry rather than a code path agreeing with itself;
//   - every render assertion is an IoU over the INK MASKS, with the floor named
//     as the exported constant text2img.AnnotateIoUFloor (Rule 1 — never a bare
//     number in the test).
package text2img

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// --- the independent ground truth -------------------------------------------

// annotateRefRect paints annotateStrokeThick nested rectangles grown outward,
// in the same colour convention the annotator uses (any non-zero ink), with
// plain integer loops and no call into the code under test.
func annotateRefRect(r image.Rectangle, thick int) map[image.Point]bool {
	ink := map[image.Point]bool{}
	for i := 0; i < thick; i++ {
		x0, y0 := r.Min.X-i, r.Min.Y-i
		x1, y1 := r.Max.X+i, r.Max.Y+i
		for x := x0; x < x1; x++ {
			ink[image.Pt(x, y0)] = true
			ink[image.Pt(x, y1-1)] = true
		}
		for y := y0; y < y1; y++ {
			ink[image.Pt(x0, y)] = true
			ink[image.Pt(x1-1, y)] = true
		}
	}
	return ink
}

// annotateRefLine is the reference Bresenham with the annotator's stroke rule:
// each raster cell plus the (thick-1) cells below it.
func annotateRefLine(x0, y0, x1, y1, thick int) map[image.Point]bool {
	ink := map[image.Point]bool{}
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
		for t := 0; t < thick; t++ {
			ink[image.Pt(x0, y0+t)] = true
		}
		if x0 == x1 && y0 == y1 {
			return ink
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

// inkMask reads an image's ink as a set of non-black... precisely: the set of
// points whose colour equals want. The mask is the comparison unit, so no test
// depends on PNG byte equality to state a geometry claim.
func inkMask(img *image.RGBA, want color.RGBA) map[image.Point]bool {
	ink := map[image.Point]bool{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.R == want.R && c.G == want.G && c.B == want.B {
				ink[image.Pt(x, y)] = true
			}
		}
	}
	return ink
}

// pngInkMask decodes a written PNG and returns the ink mask for a colour.
func pngInkMask(t *testing.T, path string, want color.RGBA) map[image.Point]bool {
	t.Helper()
	img := decodePNGFile(t, path)
	return inkMask(img, want)
}

// decodePNGFile decodes a PNG through the stdlib — the reader side of the
// pixel-level check, independent of how the file was written.
func decodePNGFile(t *testing.T, path string) *image.RGBA {
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

// iou is the intersection-over-union of two ink masks: 1.0 = identical.
func iou(a, b map[image.Point]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	inter := 0
	for p := range a {
		if b[p] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

// writeBasePNG writes a w×h white base PNG and returns its path.
func writeBasePNG(t *testing.T, dir string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	path := filepath.Join(dir, "base.png")
	fh, err := os.Create(path)
	if err != nil {
		t.Fatalf("create base: %v", err)
	}
	defer fh.Close()
	if err := png.Encode(fh, img); err != nil {
		t.Fatalf("encode base: %v", err)
	}
	return path
}

// --- the export --------------------------------------------------------------

// TestAnnotateDrawTextDrawsAndMeasures is §10 item 1 + 2: the export paints onto
// a caller's EXISTING image (it does not allocate a canvas of its own), it
// INKS (a measured count > 0), and it leaves every pixel outside the text's own
// box untouched.
func TestAnnotateDrawTextDrawsAndMeasures(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 220, 60))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	ink := Ink{R: 200, G: 20, B: 20}
	n := DrawText(img, "Marked", 10, 40, DefaultSize, "", ink)
	if n == 0 {
		t.Fatalf("DrawText inked 0 pixels — the export must measure its own ink")
	}
	// The ink colour is HONOURED (this is the direct regression against the
	// package's grey blend(): a reused blend would paint grey, not red).
	red := inkMask(img, color.RGBA{R: ink.R, G: ink.G, B: ink.B, A: 255})
	if len(red) == 0 {
		t.Fatalf("DrawText painted no pixel in the requested ink colour %+v — a grey-only blend was used", ink)
	}
	// Nothing outside a modest box around the text was touched.
	outside := 0
	for y := 0; y < 60; y++ {
		for x := 0; x < 220; x++ {
			c := img.RGBAAt(x, y)
			if c.R == 255 && c.G == 255 && c.B == 255 {
				continue
			}
			if x < 5 || x > 200 || y < 10 || y > 55 {
				outside++
			}
		}
	}
	if outside != 0 {
		t.Errorf("DrawText touched %d pixels outside the text's box — the export must draw ONLY its text", outside)
	}
}

// TestAnnotateInkNotGrey is §10 item 2 in its sharpest form: a pure-blue ink
// must produce pixels whose blue channel dominates. The package's own blend()
// would produce R==G==B on every partially covered pixel.
func TestAnnotateInkNotGrey(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 120, 40))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	blue := Ink{R: 0, G: 0, B: 255}
	if DrawText(img, "W", 6, 30, DefaultSize, "", blue) == 0 {
		t.Skip("the font chain has no glyph for W on this host")
	}
	greyed := 0
	for y := 0; y < 40; y++ {
		for x := 0; x < 120; x++ {
			c := img.RGBAAt(x, y)
			if c.R == 255 && c.G == 255 && c.B == 255 {
				continue
			}
			if c.R == c.G && c.G == c.B {
				greyed++
			}
		}
	}
	if greyed > 0 {
		t.Errorf("%d inked pixels are grey (R==G==B) under a pure-blue ink — blend()'s grey derivation was reused", greyed)
	}
}

// --- geometry against the independent ground truth ---------------------------

// TestAnnotateRectMatchesGroundTruth is the headline pixel check for a rect: the
// annotator's ink is compared, by IoU over the ink masks, against a rectangle
// rasterised independently here.
func TestAnnotateRectMatchesGroundTruth(t *testing.T) {
	dir := t.TempDir()
	base := writeBasePNG(t, dir, 160, 120)
	out := filepath.Join(dir, "out.png")
	rect := image.Rect(20, 30, 100, 80)
	mark := Mark{Kind: MarkRect, Rect: rect, Anchor: MarkAnchorFixedWord}
	if _, err := AnnotateFile(base, out, []Mark{mark}, AnnotateOptions{}); err != nil {
		t.Fatalf("AnnotateFile: %v", err)
	}
	got := pngInkMask(t, out, MarkInk.rgba())
	want := annotateRefRect(rect, annotateStrokeThick)
	if score := iou(got, want); score < AnnotateIoUFloor {
		t.Errorf("rect ink IoU %.4f < floor %.4f (annotator %d px, ground truth %d px)",
			score, AnnotateIoUFloor, len(got), len(want))
	}
}

// TestAnnotatePointFillsItsBox: a point mark is a FILLED box — every pixel of
// the declared rectangle, and nothing outside it.
func TestAnnotatePointFillsItsBox(t *testing.T) {
	dir := t.TempDir()
	base := writeBasePNG(t, dir, 80, 60)
	out := filepath.Join(dir, "out.png")
	rect := image.Rect(10, 12, 16, 18)
	if _, err := AnnotateFile(base, out, []Mark{{Kind: MarkPoint, Rect: rect, Anchor: MarkAnchorFixedWord}}, AnnotateOptions{}); err != nil {
		t.Fatalf("AnnotateFile: %v", err)
	}
	got := pngInkMask(t, out, MarkInk.rgba())
	want := map[image.Point]bool{}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			want[image.Pt(x, y)] = true
		}
	}
	if score := iou(got, want); score < AnnotateIoUFloor {
		t.Errorf("point ink IoU %.4f < floor %.4f (%d vs %d px)", score, AnnotateIoUFloor, len(got), len(want))
	}
}

// TestAnnotateArrowMatchesGroundTruth: the shaft is the reference Bresenham with
// the annotator's stroke rule. The barbs are asserted SEPARATELY (below), so a
// failure says which half moved.
func TestAnnotateArrowMatchesGroundTruth(t *testing.T) {
	dir := t.TempDir()
	base := writeBasePNG(t, dir, 200, 120)
	out := filepath.Join(dir, "out.png")
	rect := image.Rect(20, 20, 180, 100)
	if _, err := AnnotateFile(base, out, []Mark{{Kind: MarkArrow, Rect: rect, Anchor: MarkAnchorFixedWord}}, AnnotateOptions{}); err != nil {
		t.Fatalf("AnnotateFile: %v", err)
	}
	got := pngInkMask(t, out, MarkInk.rgba())
	shaft := annotateRefLine(rect.Min.X, rect.Min.Y, rect.Max.X-1, rect.Max.Y-1, annotateStrokeThick)
	// Every shaft pixel must be inked (the shaft is exact), and the extra ink is
	// the head — bounded, and asserted to reach the two barbs.
	missing := 0
	for p := range shaft {
		if !got[p] {
			missing++
		}
	}
	if missing > 0 {
		t.Fatalf("%d of %d shaft pixels are not inked — the shaft is not the declared diagonal", missing, len(shaft))
	}
	extra := diffMask(got, shaft)
	if len(extra) == 0 {
		t.Errorf("the arrow inked nothing beyond its shaft — the head was not drawn")
	}
	// The head is a BOUNDED extra: two barbs of annotateHeadPx each. An unbounded
	// extra would mean the ink escaped the declared mark.
	if max := 4 * annotateHeadPx * annotateStrokeThick; len(extra) > max {
		t.Errorf("the arrow inked %d pixels beyond its shaft (> %d) — the ink is not the declared shaft+head", len(extra), max)
	}
}

// diffMask reports the points of a that are not in b.
func diffMask(a, b map[image.Point]bool) map[image.Point]bool {
	out := map[image.Point]bool{}
	for p := range a {
		if !b[p] {
			out[p] = true
		}
	}
	return out
}

// TestAnnotateEveryMarkPixelsAreDeclared: the ink is never outside the declared
// marks' regions plus the label chip — the same "the mutation stayed where it
// said it would" property the demo stills are checked with. A label's chip is
// excluded by asserting on a run with no label.
func TestAnnotateEveryMarkPixelsAreDeclared(t *testing.T) {
	dir := t.TempDir()
	base := writeBasePNG(t, dir, 120, 90)
	out := filepath.Join(dir, "out.png")
	rect := image.Rect(30, 20, 90, 60)
	if _, err := AnnotateFile(base, out, []Mark{{Kind: MarkRect, Rect: rect, Anchor: MarkAnchorFixedWord}}, AnnotateOptions{}); err != nil {
		t.Fatalf("AnnotateFile: %v", err)
	}
	// The grown rect (stroke) is the declared region.
	region := image.Rect(rect.Min.X-annotateStrokeThick, rect.Min.Y-annotateStrokeThick,
		rect.Max.X+annotateStrokeThick, rect.Max.Y+annotateStrokeThick)
	baseImg := decodePNGFile(t, base)
	outImg := decodePNGFile(t, out)
	if baseImg.Bounds() != outImg.Bounds() {
		t.Fatalf("the annotator changed the image shape: %v → %v", baseImg.Bounds(), outImg.Bounds())
	}
	outside := 0
	for y := 0; y < outImg.Bounds().Dy(); y++ {
		for x := 0; x < outImg.Bounds().Dx(); x++ {
			a := baseImg.RGBAAt(x, y)
			b := outImg.RGBAAt(x, y)
			if a == b {
				continue
			}
			if !image.Pt(x, y).In(region) {
				outside++
			}
		}
	}
	if outside != 0 {
		t.Errorf("%d changed pixels lie OUTSIDE the declared mark's region — the annotation is not confined to what it declares", outside)
	}
}

// TestAnnotateLabelChipRefusedWhenItCannotFit: an overrunning label is REFUSED
// (the chosen arm of "refused-or-wrapped"), naming the chip's computed box —
// because a wrapped label would silently change the text the caller asked for,
// while a refusal leaves the caller's command line as the single fix.
func TestAnnotateLabelChipRefusedWhenItCannotFit(t *testing.T) {
	dir := t.TempDir()
	base := writeBasePNG(t, dir, 40, 30)
	out := filepath.Join(dir, "out.png")
	long := "a label far wider than this tiny image can hold"
	mark := Mark{Kind: MarkRect, Rect: image.Rect(2, 12, 10, 20), Label: long, Anchor: MarkAnchorFixedWord}
	_, err := AnnotateFile(base, out, []Mark{mark}, AnnotateOptions{})
	if err == nil {
		t.Fatalf("a label chip that cannot fit was accepted — it must be refused by name")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("does not fit")) {
		t.Errorf("the refusal does not say the chip does not fit: %v", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Errorf("a refused run wrote %s — a refusal writes nothing", out)
	}
}

// --- refusals ----------------------------------------------------------------

// TestAnnotateRefusesOffImageMark: §8.2's deliberate reversal — geometry that
// runs off the image is REFUSED, never clipped (precedent: `gopi tool capture --area`
// refuses a rectangle that does not fit the display).
func TestAnnotateRefusesOffImageMark(t *testing.T) {
	dir := t.TempDir()
	base := writeBasePNG(t, dir, 100, 60)
	out := filepath.Join(dir, "out.png")
	mark := Mark{Kind: MarkRect, Rect: image.Rect(80, 10, 140, 40), Anchor: MarkAnchorFixedWord}
	_, err := AnnotateFile(base, out, []Mark{mark}, AnnotateOptions{})
	if err == nil {
		t.Fatalf("an off-image mark was accepted — it must be refused, never clipped")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("does not fit")) {
		t.Errorf("the refusal does not carry the capture verb's own wording: %v", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Errorf("a refused run wrote %s", out)
	}
	// The CONTROL: the same flag with geometry that fits still works — the
	// refusal is about the rectangle, not about the verb.
	ok := Mark{Kind: MarkRect, Rect: image.Rect(10, 10, 40, 40), Anchor: MarkAnchorFixedWord}
	if _, err := AnnotateFile(base, out, []Mark{ok}, AnnotateOptions{}); err != nil {
		t.Fatalf("the in-image control was refused too: %v", err)
	}
}

// TestAnnotateRefusals is the table of the remaining contract refusals.
func TestAnnotateRefusals(t *testing.T) {
	dir := t.TempDir()
	base := writeBasePNG(t, dir, 100, 60)
	good := Mark{Kind: MarkRect, Rect: image.Rect(10, 10, 40, 40), Anchor: MarkAnchorFixedWord}

	cases := []struct {
		name string
		in   string
		out  string
		mark []Mark
		want string
	}{
		{"out missing", base, "", []Mark{good}, "--out is required"},
		{"out is the input", base, base, []Mark{good}, "is the input itself"},
		{"missing base", filepath.Join(dir, "nope.png"), filepath.Join(dir, "o.png"), []Mark{good}, "cannot read the base image"},
		{"no marks", base, filepath.Join(dir, "o.png"), nil, "nothing to draw"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := AnnotateFile(tc.in, tc.out, tc.mark, AnnotateOptions{})
			if err == nil {
				t.Fatalf("expected a refusal, got none")
			}
			if !bytes.Contains([]byte(err.Error()), []byte(tc.want)) {
				t.Errorf("refusal %q does not carry %q", err.Error(), tc.want)
			}
		})
	}
}

// TestAnnotateRefusesNonPNGBase: a base that is not a PNG is refused NAMING THE
// PATH (an unreadable-input error without the path is a puzzle, not a refusal).
func TestAnnotateRefusesNonPNGBase(t *testing.T) {
	dir := t.TempDir()
	notPNG := filepath.Join(dir, "notes.png")
	if err := os.WriteFile(notPNG, []byte("this is text, not a png"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	out := filepath.Join(dir, "out.png")
	_, err := AnnotateFile(notPNG, out, []Mark{{Kind: MarkRect, Rect: image.Rect(1, 1, 5, 5), Anchor: MarkAnchorFixedWord}}, AnnotateOptions{})
	if err == nil {
		t.Fatalf("a non-PNG base was accepted")
	}
	if !bytes.Contains([]byte(err.Error()), []byte(notPNG)) {
		t.Errorf("the refusal does not name the path it could not decode: %v", err)
	}
}

// TestAnnotateDeterministic: the same arguments produce BYTE-IDENTICAL output
// (the fixed composite order of drawMarks), so a diff of two annotated stills
// means the arguments differed — never that a map iterated differently.
func TestAnnotateDeterministic(t *testing.T) {
	dir := t.TempDir()
	base := writeBasePNG(t, dir, 200, 120)
	marks := []Mark{
		{Kind: MarkRect, Rect: image.Rect(10, 10, 60, 40), Label: "one", Anchor: MarkAnchorFixedWord},
		{Kind: MarkPoint, Rect: image.Rect(150, 90, 156, 96), Anchor: MarkAnchorFixedWord},
		{Kind: MarkArrow, Rect: image.Rect(70, 60, 140, 61), Anchor: MarkAnchorFixedWord},
	}
	var prev []byte
	for i := 0; i < 3; i++ {
		out := filepath.Join(dir, "out.png")
		if _, err := AnnotateFile(base, out, marks, AnnotateOptions{}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		blob, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read run %d: %v", i, err)
		}
		if prev != nil && !bytes.Equal(prev, blob) {
			t.Fatalf("run %d differs from run %d — annotate is not deterministic", i, i-1)
		}
		prev = blob
	}
}

// TestAnnotateLabelIsDrawn: a label with room draws (ink_pixels includes the
// glyphs), and its chip carries the mark ink border.
func TestAnnotateLabelIsDrawn(t *testing.T) {
	dir := t.TempDir()
	base := writeBasePNG(t, dir, 400, 200)
	out := filepath.Join(dir, "out.png")
	mark := Mark{Kind: MarkRect, Rect: image.Rect(100, 100, 200, 150), Label: "status row", Anchor: MarkAnchorFixedWord}
	res, err := AnnotateFile(base, out, []Mark{mark}, AnnotateOptions{})
	if err != nil {
		t.Fatalf("AnnotateFile: %v", err)
	}
	if res.Labels != 1 {
		t.Errorf("Labels = %d, want 1", res.Labels)
	}
	if res.InkPixels <= 0 {
		t.Errorf("InkPixels = %d — a drawn annotation must change pixels", res.InkPixels)
	}
	// The chip's border is mark ink, so the ink mask must be more than the
	// rectangle outline alone.
	got := pngInkMask(t, out, MarkInk.rgba())
	want := annotateRefRect(mark.Rect, annotateStrokeThick)
	if len(got) <= len(want) {
		t.Errorf("the label chip added no mark-ink pixels (%d vs %d) — the chip's border was not drawn", len(got), len(want))
	}
}
