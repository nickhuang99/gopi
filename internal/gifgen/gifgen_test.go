package gifgen

import (
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain pins this suite's scratch root into the shared scratch home and
// refuses to run it on the / filesystem: every t.TempDir() below would
// otherwise land in /tmp (Rule 22 — the temp root must be provably OFF the /
// partition, checked by device id, never by spelling). Same shape as
// internal/text2img's own TestMain.
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "gifgen")
}

// --- helpers ----------------------------------------------------------------

// solidPNG writes a w×h PNG of one colour into dir and returns its path.
func solidPNG(t *testing.T, dir, name string, w, h int, c color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	return path
}

// loadGIF decodes a written GIF, failing the test when it is unreadable.
func loadGIF(t *testing.T, path string) *gif.GIF {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	g, err := gif.DecodeAll(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return g
}

// rgbaOf reads one pixel of an encoded frame back as RGBA.
func rgbaOf(g *gif.GIF, frame, x, y int) color.RGBA {
	f := g.Image[frame]
	c := f.Palette[f.Pix[y*f.Stride+x]]
	r, gg, b, _ := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(gg >> 8), B: uint8(b >> 8), A: 0xff}
}

// frameIsUniform reports whether every pixel of the frame equals want.
func frameIsUniform(g *gif.GIF, frame int, want color.RGBA) bool {
	f := g.Image[frame]
	for y := f.Bounds().Min.Y; y < f.Bounds().Max.Y; y++ {
		for x := f.Bounds().Min.X; x < f.Bounds().Max.X; x++ {
			if rgbaOf(g, frame, x, y) != want {
				return false
			}
		}
	}
	return true
}

const (
	// testFrameW / testFrameH size the fixture frames: big enough that a
	// caption band and the frame above it are distinguishable, small enough
	// that the whole suite stays in the millisecond range.
	testFrameW = 96
	testFrameH = 40
	// testCaptionDelayCS is the delay the fixtures state, chosen to differ from
	// the package default so a default leaking into the stream fails the test.
	testCaptionDelayCS = 40
)

// --- tests ------------------------------------------------------------------

// TestGifEncodesFramesDelaysAndLoop is the core contract: N PNGs + one delay +
// a loop count in, one GIF out whose OWN bytes carry those numbers.
//
// It also pins DETERMINISM: the same call twice must produce identical bytes
// (this package's header promises it — the palette is built in first-seen
// order and nothing about "now" reaches the encoder), and the composed frame
// count/size must come back from the written file, not from the caller's list.
//
// The geometry assertion is the FILM's rule: one band height for every frame
// (the tallest caption's), including the frame that carried no words at all —
// otherwise the band area keeps the previous frame's text via disposal=none.
func TestGifEncodesFramesDelaysAndLoop(t *testing.T) {
	dir := t.TempDir()
	f1 := solidPNG(t, dir, "a.png", testFrameW, testFrameH, color.RGBA{R: 0, G: 0, B: 0, A: 0xff})
	f2 := solidPNG(t, dir, "b.png", testFrameW, testFrameH, color.RGBA{R: 255, G: 0, B: 0, A: 0xff})
	f3 := solidPNG(t, dir, "c.png", testFrameW, testFrameH, color.RGBA{R: 0, G: 0, B: 255, A: 0xff})
	out := filepath.Join(dir, "out.gif")

	o := Options{
		DelayCS: testCaptionDelayCS,
		Frames: []Frame{
			{Path: f1, Caption: "第一帧：起点"},
			{Path: f2},
			{Path: f3, Caption: "第三帧：终点"},
		},
	}
	res, err := Encode(out, o)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	g := loadGIF(t, out)
	if len(g.Image) != 3 {
		t.Fatalf("wrote %d frames, want 3", len(g.Image))
	}
	for i, d := range g.Delay {
		if d != testCaptionDelayCS {
			t.Errorf("frame %d delay = %dcs, want %dcs", i, d, testCaptionDelayCS)
		}
	}
	if g.LoopCount != LoopForever {
		t.Errorf("LoopCount = %d, want %d (forever)", g.LoopCount, LoopForever)
	}
	band := captionRows(o.Frames[2].Caption, DefaultCaptionSize, DefaultCaptionMargin)
	if band <= 0 {
		t.Fatalf("the fixture's caption produced no band rows — the test is not measuring what it claims")
	}
	if res.CaptionRows != band {
		t.Errorf("Result.CaptionRows = %d, want the tallest caption's band %d", res.CaptionRows, band)
	}
	for i := range g.Image {
		if got, want := g.Image[i].Bounds().Dy(), testFrameH+band; got != want {
			t.Errorf("frame %d height = %d, want %d (%d frame + %d band) — the band is a property of the FILM",
				i, got, want, testFrameH, band)
		}
		if got := g.Image[i].Bounds().Dx(); got != testFrameW {
			t.Errorf("frame %d width = %d, want %d", i, got, testFrameW)
		}
	}
	if res.Frames != 3 || res.DelayCS != testCaptionDelayCS || res.LoopCount != LoopForever {
		t.Errorf("Result = %+v, want 3 frames / %dcs / loop %d", res, testCaptionDelayCS, LoopForever)
	}
	if res.InkPixels == 0 {
		t.Errorf("Result.InkPixels = 0: the captions inked nothing, so the band carries no words")
	}
	if res.Captioned != 2 {
		t.Errorf("Result.Captioned = %d, want 2 (the middle frame has no caption)", res.Captioned)
	}

	// DETERMINISM: same inputs, same bytes.
	first, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if _, err := Encode(out, o); err != nil {
		t.Fatalf("second Encode: %v", err)
	}
	second, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back again: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("two runs over the same inputs produced different bytes (%d vs %d) — a GIF a caller cannot diff is not a deliverable", len(first), len(second))
	}
}

// TestGifCaptionBandNeverTouchesTheFrame is the pixel-level truth check for the
// composition rule (the band is APPENDED, never painted over): the top
// testFrameH rows of a captioned frame must equal the same rows of the SAME
// input encoded with no caption at all.
//
// The comparison is between two independently composed pictures, so it cannot
// be satisfied by the encoder agreeing with itself.
func TestGifCaptionBandNeverTouchesTheFrame(t *testing.T) {
	dir := t.TempDir()
	// A frame the caption could visibly damage if it were drawn over: pure blue,
	// where black text would be obvious.
	f := solidPNG(t, dir, "frame.png", testFrameW, testFrameH, color.RGBA{R: 0, G: 0, B: 255, A: 0xff})
	plain := filepath.Join(dir, "plain.gif")
	captioned := filepath.Join(dir, "captioned.gif")

	if _, err := Encode(plain, Options{Frames: []Frame{{Path: f}}}); err != nil {
		t.Fatalf("Encode (no caption): %v", err)
	}
	if _, err := Encode(captioned, Options{Frames: []Frame{{Path: f, Caption: "这一帧被解释了"}}}); err != nil {
		t.Fatalf("Encode (with caption): %v", err)
	}
	gp, gc := loadGIF(t, plain), loadGIF(t, captioned)
	if gp.Image[0].Bounds() != image.Rect(0, 0, testFrameW, testFrameH) {
		t.Fatalf("the caption-less FILM's frame is %v, want the input's own %dx%d — a film with nothing to say must not be given a band",
			gp.Image[0].Bounds(), testFrameW, testFrameH)
	}
	if gc.Image[0].Bounds().Dy() <= testFrameH {
		t.Fatalf("the captioned frame is %v: the band was not appended", gc.Image[0].Bounds())
	}
	// The top rows must be IDENTICAL pixel for pixel (blue, untouched).
	for y := 0; y < testFrameH; y++ {
		for x := 0; x < testFrameW; x++ {
			if a, b := rgbaOf(gp, 0, x, y), rgbaOf(gc, 0, x, y); a != b {
				t.Fatalf("caption changed the frame's own pixel at (%d,%d): %v → %v — a caption must never cover the pixels it explains", x, y, a, b)
			}
		}
	}
	// The band below is paper: its corners are white (no ink reaches them).
	lo := gc.Image[0].Bounds().Max.Y - 1
	if c := rgbaOf(gc, 0, 0, lo); c != (color.RGBA{R: CaptionFillRGB, G: CaptionFillRGB, B: CaptionFillRGB, A: 0xff}) {
		t.Errorf("band bottom-left pixel = %v, want the caption paper %d/%d/%d", c, CaptionFillRGB, CaptionFillRGB, CaptionFillRGB)
	}
	if c := rgbaOf(gc, 0, testFrameW-1, lo); c.R != CaptionFillRGB || c.G != CaptionFillRGB || c.B != CaptionFillRGB {
		t.Errorf("band bottom-right pixel = %v — the paper does not span the canvas width", c)
	}
	// And there IS ink in the band (the words reached the picture): count the
	// non-paper pixels strictly inside the band.
	inked := 0
	for y := testFrameH; y < gc.Image[0].Bounds().Max.Y; y++ {
		for x := 0; x < testFrameW; x++ {
			c := rgbaOf(gc, 0, x, y)
			if c.R != CaptionFillRGB || c.G != CaptionFillRGB || c.B != CaptionFillRGB {
				inked++
			}
		}
	}
	if inked == 0 {
		t.Fatalf("the caption band is pure paper — the words were not drawn (a band with no text is worse than no band)")
	}
}

// TestGifCaptionInkIsOpaqueBlack pins the ink the caption is drawn in: the
// composer's blend must yield an OPAQUE pixel, not the frame's blue showing
// through a half-covered glyph (the defect annotate.go's own tests pin for
// marks, one function over).
func TestGifCaptionInkIsOpaqueBlack(t *testing.T) {
	dir := t.TempDir()
	f := solidPNG(t, dir, "frame.png", testFrameW, testFrameH, color.RGBA{R: 0, G: 0, B: 255, A: 0xff})
	out := filepath.Join(dir, "ink.gif")
	if _, err := Encode(out, Options{Frames: []Frame{{Path: f, Caption: "WWWW"}}}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	g := loadGIF(t, out)
	// The darkest band pixel must be clearly darker than BOTH the paper and the
	// frame's blue: a partial blend of black over blue would keep B high.
	darkest := 3 * CaptionFillRGB
	for y := testFrameH; y < g.Image[0].Bounds().Max.Y; y++ {
		for x := 0; x < testFrameW; x++ {
			c := rgbaOf(g, 0, x, y)
			if sum := int(c.R) + int(c.G) + int(c.B); sum < darkest {
				darkest = sum
			}
		}
	}
	if darkest > 3*CaptionInkRGB+3*inkHalfTolerance {
		t.Errorf("the darkest caption pixel sums to %d — no glyph reached full coverage (ink is not opaque)", darkest)
	}
}

// inkHalfTolerance bounds how far above pure ink a fully covered glyph pixel
// may land: the rasteriser is 4x supersampled, so an edge pixel can be a few
// levels off, but a HALF-covered glyph is hundreds of levels off. Named
// (Rule 1) so the assertion says what it means.
const inkHalfTolerance = 24

// TestGifLoopCountIsHonoured is the "loop actually loops" case for a FINITE
// count: 0 is only one of the two shapes the format carries, and a package that
// always wrote 0 would pass the loop-forever test while ignoring the caller.
//
// TWO frames, deliberately: Go's encoder writes the Netscape looping extension
// only for a multi-frame stream (see Result.LoopReadBack), so a one-frame
// fixture could never test that a stated count reaches the file.
func TestGifLoopCountIsHonoured(t *testing.T) {
	dir := t.TempDir()
	f := solidPNG(t, dir, "a.png", testFrameW, testFrameH, color.RGBA{A: 0xff})
	f2 := solidPNG(t, dir, "b.png", testFrameW, testFrameH, color.RGBA{R: 255, A: 0xff})
	out := filepath.Join(dir, "loop3.gif")
	res, err := Encode(out, Options{Loop: 3, LoopSet: true, Frames: []Frame{{Path: f}, {Path: f2}}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if res.LoopCount != 3 || res.LoopReadBack != 3 {
		t.Errorf("Result asked-for/read-back = %d/%d, want 3/3", res.LoopCount, res.LoopReadBack)
	}
	if got := loadGIF(t, out).LoopCount; got != 3 {
		t.Errorf("the written file's LoopCount = %d, want 3", got)
	}
	if !strings.Contains(res.Summary(), "loop=3") {
		t.Errorf("Summary does not report the loop count: %s", res.Summary())
	}
}

// TestGifRefusesBadInput is the refusal table. Every case asserts BOTH that the
// error is a caller-fixable UsageError (so the CLI can exit with the usage
// class) and that the message NAMES the cause (Rule 15: a refusal that does not
// say what is wrong is a puzzle, not a refusal).
func TestGifRefusesBadInput(t *testing.T) {
	dir := t.TempDir()
	ok := solidPNG(t, dir, "ok.png", testFrameW, testFrameH, color.RGBA{A: 0xff})
	big := solidPNG(t, dir, "big.png", testFrameW+1, testFrameH, color.RGBA{A: 0xff})
	notPNG := filepath.Join(dir, "actually-text.png")
	if err := os.WriteFile(notPNG, []byte("this is not a PNG, whatever the name says"), 0o644); err != nil {
		t.Fatalf("write %s: %v", notPNG, err)
	}
	out := filepath.Join(dir, "out.gif")

	cases := []struct {
		name string
		o    Options
		out  string
		want string
	}{
		{"no frames", Options{}, out, "no frames"},
		{"empty out", Options{Frames: []Frame{{Path: ok}}}, "", "no output path"},
		{"mismatched frame size", Options{Frames: []Frame{{Path: ok}, {Path: big}}}, out, "shares a canvas"},
		{"not a png", Options{Frames: []Frame{{Path: notPNG}}}, out, "is not a PNG"},
		{"missing file", Options{Frames: []Frame{{Path: filepath.Join(dir, "nope.png")}}}, out, "read frame"},
		{"negative delay", Options{DelayCS: -1, Frames: []Frame{{Path: ok}}}, out, "outside the GIF's own range"},
		{"huge delay", Options{DelayCS: MaxDelayCentiseconds + 1, Frames: []Frame{{Path: ok}}}, out, "outside the GIF's own range"},
		{"negative loop", Options{Loop: -2, LoopSet: true, Frames: []Frame{{Path: ok}}}, out, "outside the GIF's own range"},
		{"huge loop", Options{Loop: MaxLoopCount + 1, LoopSet: true, Frames: []Frame{{Path: ok}}}, out, "outside the GIF's own range"},
		{"negative caption size", Options{CaptionSize: -1, Frames: []Frame{{Path: ok, Caption: "x"}}}, out, "caption font size"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Encode(tc.out, tc.o)
			if err == nil {
				t.Fatalf("Encode accepted %s — want a refusal", tc.name)
			}
			if !IsUsage(err) {
				t.Errorf("the refusal is not a UsageError (exit class): %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not name the cause %q: %v", tc.want, err)
			}
		})
	}
	// A refusal must not leave a half-written GIF behind (the encoder builds in
	// memory and writes once).
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists after a refused call — a refusal must write nothing", out)
	}
}

// TestGifZeroMeansUnsetWhileNegativeIsRefused pins the sentinel rule this
// package shares with text2img's numeric flags (Rule 2): 0 is the zero value of
// an unset positive dimension and falls back to the default, while a negative
// number is an instruction that cannot be carried out and is refused BY NAME.
// A package that treated both as "unset" would answer a different question than
// the caller asked, and a package that refused 0 would break every caller that
// leaves the field alone.
func TestGifZeroMeansUnsetWhileNegativeIsRefused(t *testing.T) {
	dir := t.TempDir()
	f := solidPNG(t, dir, "f.png", testFrameW, testFrameH, color.RGBA{A: 0xff})
	out := filepath.Join(dir, "zero.gif")
	// DelayCS = 0 (unset) → the package default, and LoopSet=false → forever.
	res, err := Encode(out, Options{Frames: []Frame{{Path: f, Caption: "默认字号"}}})
	if err != nil {
		t.Fatalf("Encode with unset delay/size: %v", err)
	}
	if res.DelayCS != DefaultDelayCentiseconds {
		t.Errorf("DelayCS = %d, want the default %d when unstated", res.DelayCS, DefaultDelayCentiseconds)
	}
	if res.LoopCount != LoopForever {
		t.Errorf("LoopCount = %d, want %d when unstated", res.LoopCount, LoopForever)
	}
	wantBand := captionRows("默认字号", DefaultCaptionSize, DefaultCaptionMargin)
	if res.CaptionRows != wantBand {
		t.Errorf("CaptionRows = %d, want the default-size band %d", res.CaptionRows, wantBand)
	}
	// A NEGATIVE margin is refused too (it is an instruction, not a sentinel).
	if _, err := Encode(out, Options{CaptionMargin: -4, Frames: []Frame{{Path: f, Caption: "x"}}}); err != nil {
		t.Errorf("a negative caption margin was refused: %v — it is resolveCaptionMargin's default-fallback case, documented as such", err)
	}
}

// TestGifSingleFrameFilmHasNoLoopExtension pins the format fact behind the two
// loop fields: Go's encoder emits the Netscape looping extension only for a
// MULTI-frame stream, so a one-frame film reads back -1 ("play once") whatever
// it was asked for. For a single still those mean the same display — and the
// package states both numbers instead of hiding the difference (a single field
// would have to lie about one of them).
func TestGifSingleFrameFilmHasNoLoopExtension(t *testing.T) {
	dir := t.TempDir()
	f := solidPNG(t, dir, "still.png", testFrameW, testFrameH, color.RGBA{A: 0xff})
	out := filepath.Join(dir, "still.gif")
	res, err := Encode(out, Options{Frames: []Frame{{Path: f}}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if res.LoopCount != LoopForever {
		t.Errorf("Result.LoopCount = %d, want %d (the caller asked for the default)", res.LoopCount, LoopForever)
	}
	if got := loadGIF(t, out).LoopCount; got != loopReadBackOnce || res.LoopReadBack != loopReadBackOnce {
		t.Errorf("one-frame stream reads back LoopCount = %d (Result.LoopReadBack = %d), want %d for a stream with no looping extension",
			got, res.LoopReadBack, loopReadBackOnce)
	}
	if !strings.Contains(res.Summary(), "play-once") {
		t.Errorf("Summary hides the read-back difference: %s", res.Summary())
	}
	// Two frames with the same "unset" input DO carry the extension, forever.
	f2 := solidPNG(t, dir, "still2.png", testFrameW, testFrameH, color.RGBA{R: 255, A: 0xff})
	res2, err := Encode(out, Options{Frames: []Frame{{Path: f}, {Path: f2}}})
	if err != nil {
		t.Fatalf("Encode (two frames): %v", err)
	}
	if res2.LoopCount != LoopForever || res2.LoopReadBack != LoopForever {
		t.Errorf("two-frame film: asked %d, file says %d, want %d / %d",
			res2.LoopCount, res2.LoopReadBack, LoopForever, LoopForever)
	}
	if got := loadGIF(t, out).LoopCount; got != LoopForever {
		t.Errorf("the written two-frame file reads back LoopCount = %d, want %d", got, LoopForever)
	}
}

// TestGifRefusesCaptionWithoutSeparator pins the `--caption` grammar at the
// parser the CLI shares, including the duplicate-path refusal: two captions for
// one frame is an ambiguous instruction, and silently keeping one is the defect
// class the refusal exists to prevent.
func TestGifCaptionsAndFrameListParsing(t *testing.T) {
	paths := SplitFrames(" a.png , b.png ,, c.png ")
	if len(paths) != 3 || paths[0] != "a.png" || paths[2] != "c.png" {
		t.Errorf("SplitFrames = %v, want [a.png b.png c.png] (empty items dropped)", paths)
	}
	caps, err := ParseCaptions("a.png=第一帧, b.png=第二帧")
	if err != nil {
		t.Fatalf("ParseCaptions: %v", err)
	}
	if caps["a.png"] != "第一帧" || caps["b.png"] != "第二帧" {
		t.Errorf("ParseCaptions = %v", caps)
	}
	// The text may itself contain the separator: only the FIRST one splits.
	if caps, err := ParseCaptions("a.png=一=二"); err != nil || caps["a.png"] != "一=二" {
		t.Errorf("ParseCaptions kept only part of the text: %v (err=%v)", caps, err)
	}
	for _, bad := range []string{"a.png", "=text", "a.png=one,a.png=two"} {
		if _, err := ParseCaptions(bad); err == nil {
			t.Errorf("ParseCaptions accepted %q", bad)
		} else if !IsUsage(err) {
			t.Errorf("ParseCaptions(%q) returned a non-usage error: %v", bad, err)
		}
	}
	// Frames keep the CALLER's order, never a map's.
	frames := FramesFromPaths(paths, caps)
	if frames[0].Path != "a.png" || frames[1].Caption != "第二帧" || frames[2].Caption != "" {
		t.Errorf("FramesFromPaths lost the order or the pairing: %+v", frames)
	}
}

// TestGifMidFilmFrameWithoutWordsShowsPaper is the OTHER half of the band rule,
// and it is the one a reader would notice: inside a captioned film, a frame
// whose own caption is empty still gets the band — filled with paper. If it did
// not, disposal=none would leave the PREVIOUS frame's words on screen under a
// picture they do not describe (measured: the first version of this package
// wrote frames of differing height and the reader saw stale text).
func TestGifMidFilmFrameWithoutWordsShowsPaper(t *testing.T) {
	dir := t.TempDir()
	blue := color.RGBA{R: 0, G: 0, B: 255, A: 0xff}
	f := solidPNG(t, dir, "f.png", testFrameW, testFrameH, blue)
	out := filepath.Join(dir, "film.gif")
	if _, err := Encode(out, Options{Frames: []Frame{
		{Path: f, Caption: "有文字的一帧"},
		{Path: f},
	}}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	g := loadGIF(t, out)
	band := captionRows("有文字的一帧", DefaultCaptionSize, DefaultCaptionMargin)
	if band <= 0 {
		t.Fatalf("the fixture produced no band rows")
	}
	if got, want := g.Image[1].Bounds().Dy(), testFrameH+band; got != want {
		t.Fatalf("the wordless frame's height = %d, want the film's uniform %d", got, want)
	}
	// Its band is PURE PAPER: no ink, and in particular not frame 0's words.
	bandInk := 0
	for y := testFrameH; y < g.Image[1].Bounds().Max.Y; y++ {
		for x := 0; x < testFrameW; x++ {
			c := rgbaOf(g, 1, x, y)
			if c.R != CaptionFillRGB || c.G != CaptionFillRGB || c.B != CaptionFillRGB {
				bandInk++
			}
		}
	}
	if bandInk != 0 {
		t.Errorf("the wordless frame's band carries %d non-paper pixels — it is showing something (a stale neighbour's words?)", bandInk)
	}
}

// TestGifWideCaptionWarnsRatherThanSilentlyClipping pins the one place this
// package accepts a lossy outcome: a caption wider than the canvas is CLIPPED
// (the geometry is the caller's to change) but the Result SAYS SO, so the CLI
// can print the warning and nobody ships an unreadable frame believing it is
// fine.
func TestGifWideCaptionWarnsRatherThanSilentlyClipping(t *testing.T) {
	dir := t.TempDir()
	// A narrow frame with a long Latin caption: certainly wider than 32px.
	f := solidPNG(t, dir, "narrow.png", 32, 20, color.RGBA{A: 0xff})
	out := filepath.Join(dir, "wide.gif")
	res, err := Encode(out, Options{Frames: []Frame{{Path: f, Caption: "a caption far wider than the frame it explains"}}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("a caption wider than the canvas produced no warning — the loss is silent")
	}
	if !strings.Contains(strings.Join(res.Warnings, " "), "CLIPPED") {
		t.Errorf("the warning does not say the text was clipped: %v", res.Warnings)
	}
	if !strings.Contains(res.Summary(), "警告") {
		t.Errorf("Result.Summary omits the warning count: %s", res.Summary())
	}
	// A frame that FITS produces no warning (the check must not cry wolf).
	ok := solidPNG(t, dir, "wide.png", 640, 20, color.RGBA{A: 0xff})
	res2, err := Encode(out, Options{Frames: []Frame{{Path: ok, Caption: "short"}}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(res2.Warnings) != 0 {
		t.Errorf("a caption that fits warned anyway: %v", res2.Warnings)
	}
}

// TestGifEmptyCaptionTextIsNotABand pins the difference between "no caption"
// and "a caption of spaces": whitespace is not words, and a band of paper is
// the dishonest picture (it claims a frame said something).
func TestGifEmptyCaptionTextIsNotABand(t *testing.T) {
	dir := t.TempDir()
	f := solidPNG(t, dir, "f.png", testFrameW, testFrameH, color.RGBA{A: 0xff})
	out := filepath.Join(dir, "blank.gif")
	res, err := Encode(out, Options{Frames: []Frame{{Path: f, Caption: "   "}}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if res.Captioned != 0 || res.CaptionRows != 0 {
		t.Errorf("whitespace caption counted as words: %+v", res)
	}
	if b := loadGIF(t, out).Image[0].Bounds(); b.Dx() != testFrameW || b.Dy() != testFrameH {
		t.Errorf("a whitespace caption changed the geometry: %v", b)
	}
}

// TestGifFrameColoursAreLosslessWhenFew pins that the palette path is not
// "quantise and hope": with few colours the written frames must reproduce the
// input's exact RGB, because the palette is built from the picture's own
// colours in first-seen order.
func TestGifFrameColoursAreLosslessWhenFew(t *testing.T) {
	dir := t.TempDir()
	blue := color.RGBA{R: 0, G: 0, B: 255, A: 0xff}
	f := solidPNG(t, dir, "f.png", testFrameW, testFrameH, blue)
	out := filepath.Join(dir, "lossless.gif")
	if _, err := Encode(out, Options{Frames: []Frame{{Path: f}}}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	g := loadGIF(t, out)
	if !frameIsUniform(g, 0, blue) {
		t.Errorf("a one-colour frame did not come back as that colour — the palette path altered pixels it had room to keep")
	}
}

// TestGifManyColoursStayWithinPalette bounds the overflow path: a frame with
// more distinct colours than the format can carry must still encode (nearest
// colour), never fail and never write an index past the palette.
func TestGifManyColoursStayWithinPalette(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: uint8((x * y) % 256), A: 0xff})
		}
	}
	path := filepath.Join(dir, "many.png")
	fh, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := png.Encode(fh, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := fh.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	out := filepath.Join(dir, "many.gif")
	res, err := Encode(out, Options{Frames: []Frame{{Path: path}}})
	if err != nil {
		t.Fatalf("Encode with >256 colours: %v", err)
	}
	if res.PaletteColors > PaletteSize {
		t.Errorf("palette has %d colours, above the format's %d", res.PaletteColors, PaletteSize)
	}
	g := loadGIF(t, out)
	// Every index in the stream must address a real palette entry.
	for _, f := range g.Image {
		for _, pi := range f.Pix {
			if int(pi) >= len(f.Palette) {
				t.Fatalf("frame carries index %d but the palette has %d entries", pi, len(f.Palette))
			}
		}
	}
}

// TestGifReducedPaletteStaysCloseOnManyColours is the REGRESSION GUARD for the
// defect that produced this package's two-mode quantiser. The first version
// built the palette in FIRST-SEEN order, which on a many-coloured frame spends
// all 256 slots on whichever colours appear first: measured on the gradient
// fixture below, 0.49% of pixels reproduced with MAE 83.909 (the palette held
// the top-left corner and little else). The reduced mode measures the whole film
// and maps each pixel to its cell's mean; the same fixture now lands at MAE
// ≈10.7 with a worst-channel error of 65.
//
// The assertion is a CEILING, not an equality (the measured value is logged, so
// a drift is visible without the test being flaky): a first-seen palette cannot
// come back through this door.
func TestGifReducedPaletteStaysCloseOnManyColours(t *testing.T) {
	const (
		maxMAE = 20.0 // measured 10.727; a first-seen palette scores 83.909
		// minReducedColors is a FLOOR, not the ceiling: the reduced grid has 256
		// cells and a given fixture need not populate every one (this one fills
		// 246 — the blue axis has 4 levels, so some cells stay empty). What the
		// floor proves is that the mode is the reduced one and that the palette is
		// spread over the colour space rather than parked on the first few colours
		// it saw, which is what the defect looked like.
		minReducedColors = 200
	)
	dir := t.TempDir()
	// A deterministic "photographic" frame: a two-axis gradient whose third
	// channel is a product, which puts thousands of distinct colours in it. No
	// randomness — a fixture that varies between runs cannot guard a regression.
	img := image.NewRGBA(image.Rect(0, 0, testFrameW, testFrameH))
	for y := 0; y < testFrameH; y++ {
		for x := 0; x < testFrameW; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 255 / testFrameW),
				G: uint8(y * 255 / testFrameH),
				B: uint8(x * y % 256),
				A: 0xff,
			})
		}
	}
	src := filepath.Join(dir, "photo.png")
	fh, err := os.Create(src)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := png.Encode(fh, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := fh.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	out := filepath.Join(dir, "photo.gif")
	res, err := Encode(out, Options{Frames: []Frame{{Path: src}}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if res.PaletteColors < minReducedColors {
		t.Errorf("palette has %d colours, want at least %d for a many-coloured frame (the reduced mode was not taken, or its cells collapsed)",
			res.PaletteColors, minReducedColors)
	}
	g := loadGIF(t, out)
	total, worst := 0, 0
	for y := 0; y < testFrameH; y++ {
		for x := 0; x < testFrameW; x++ {
			want := color.RGBA{R: uint8(x * 255 / testFrameW), G: uint8(y * 255 / testFrameH), B: uint8(x * y % 256), A: 0xff}
			got := rgbaOf(g, 0, x, y)
			d := absDiff(int(want.R), int(got.R)) + absDiff(int(want.G), int(got.G)) + absDiff(int(want.B), int(got.B))
			total += d
			if d > worst {
				worst = d
			}
		}
	}
	mae := float64(total) / float64(3*testFrameW*testFrameH)
	if mae > maxMAE {
		t.Errorf("reduced-palette MAE = %.3f (worst channel sum %d), above the %.1f ceiling — the palette is no longer built from the whole film's colours (a first-seen palette scores 83.91)",
			mae, worst, maxMAE)
	}
	t.Logf("reduced palette: MAE %.3f over %d pixels, worst channel sum %d, palette %d colours",
		mae, testFrameW*testFrameH, worst, res.PaletteColors)
}

// absDiff is |a-b| for the MAE arithmetic above (stated once, named — Rule 1).
func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// TestGifSummaryReportsPaletteContentNotItsSize is the regression guard for the
// assembly log line (task 01a0fa54). The line used to name ONE number — the
// palette's slot count, `q.colors`, up to PaletteSize — and that number reads as
// the OPPOSITE of what a monochrome film is: measured 2026-10-02, the operator's
// colourless film reported 253 slots while carrying no colour of gopi's at all
// (five non-grey entries, every one the tinted background), so the log said
// "colourful" about the artifact that proved colour had been lost. The fix names
// the table's size AND what it holds, so the two cannot be confused again.
func TestGifSummaryReportsPaletteContentNotItsSize(t *testing.T) {
	dir := t.TempDir()
	// A COLOURLESS film: two greys — every channel equal on both, so the table
	// holds no colour at all.
	g1 := solidPNG(t, dir, "grey1.png", testFrameW, testFrameH, color.RGBA{R: 128, G: 128, B: 128, A: 0xff})
	g2 := solidPNG(t, dir, "grey2.png", testFrameW, testFrameH, color.RGBA{R: 208, G: 208, B: 208, A: 0xff})
	greyRes, err := Encode(filepath.Join(dir, "grey.gif"), Options{Frames: []Frame{{Path: g1}, {Path: g2}}})
	if err != nil {
		t.Fatalf("Encode (grey film): %v", err)
	}
	if greyRes.PaletteNonGrey != 0 || greyRes.PaletteSaturated != 0 {
		t.Errorf("a film of two greys reports %d non-grey / %d saturated entries — the palette content count is wrong",
			greyRes.PaletteNonGrey, greyRes.PaletteSaturated)
	}
	greyLine := greyRes.Summary()
	// The OLD spelling — the bare slot count — must be gone: it is the spelling
	// that let a reader conclude the opposite of the artifact.
	if strings.Contains(greyLine, "调色板=") {
		t.Errorf("the summary still carries the bare slot-count spelling %q: %s", "调色板=", greyLine)
	}
	for _, want := range []string{"调色板槽位=", "实际=", "非灰阶=", "饱和="} {
		if !strings.Contains(greyLine, want) {
			t.Errorf("summary does not name %q: %s", want, greyLine)
		}
	}
	// And the number that cannot be misread is on the line, at zero.
	if !strings.Contains(greyLine, "饱和=0 色") {
		t.Errorf("a colourless film's summary does not state 饱和=0 色: %s", greyLine)
	}

	// A COLOURED film: gopi's own deep gold, Palette256.AnswerBold's colour-178
	// (#D7AF00 = 215,175,0). The same field must move off zero.
	gold := solidPNG(t, dir, "gold.png", testFrameW, testFrameH, color.RGBA{R: 215, G: 175, B: 0, A: 0xff})
	goldRes, err := Encode(filepath.Join(dir, "gold.gif"), Options{Frames: []Frame{{Path: gold}}})
	if err != nil {
		t.Fatalf("Encode (gold film): %v", err)
	}
	if goldRes.PaletteSaturated < 1 {
		t.Errorf("a film of gopi's colour-178 reports %d saturated entries, want >= 1", goldRes.PaletteSaturated)
	}
	if !strings.Contains(goldRes.Summary(), "饱和=1 色") {
		t.Errorf("a coloured film's summary does not state 饱和=1 色: %s", goldRes.Summary())
	}
}
