package text2img

// Ink tests — the sibling suites assert an image is SIZED to the text, which a
// completely blank page also satisfies (the reported bug: a Latin string on a
// CJK-only face produced a correctly-sized ALL-WHITE PNG and the CLI still
// printed "✅ … 1 页"). Every test here decodes the written PNG and counts DARK
// pixels, so "the renderer ran" can never stand in for "there is ink".

import (
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/scratch"
)

// inkFileExists is a one-line existence probe: the default-output test asserts a
// file is ABSENT, so it cannot use os.Stat's error as the assertion.
func inkFileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// droidCJKOnlyFont is the face that made the bug visible on this host: it has
// full CJK coverage and ZERO Latin glyphs.
const droidCJKOnlyFont = "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf"

// darkPixels decodes a PNG and counts dark (ink) pixels. This is the pixel-level
// truth a size/format check cannot see.
func darkPixels(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	b := img.Bounds()
	dark := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r < 0x8000 && g < 0x8000 && bl < 0x8000 {
				dark++
			}
		}
	}
	return dark
}

// writeText drops a fixture file into a temp dir and returns its path.
func writeText(t *testing.T, name, text string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// inkFloor is the minimum dark-pixel count a text page must carry: a real
// 20px glyph inks 20-40 pixels, so a page of N glyphs sits far above this. The
// floor is deliberately well below the measured value — it exists to catch
// ZERO-ish ink (the blank-page bug), not to pin rasterization detail.
const inkFloor = 150

// TestRenderFileInksASCII is the reported bug's regression test: ASCII text
// through the default (autodetected) chain must leave dark pixels on the page.
// On the pre-fix code the default font was a CJK-only face, every ASCII rune
// mapped to glyph 0 and the written PNG was pure white — this test fails there
// with dark == 0.
func TestRenderFileInksASCII(t *testing.T) {
	src := writeText(t, "ascii.txt", "hello world\nABC 123\n")
	res, err := RenderFile(src, "", Options{})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	if len(res.Outputs) != 1 {
		t.Fatalf("Outputs = %v, want 1 page", res.Outputs)
	}
	dark := darkPixels(t, res.Outputs[0])
	if dark < inkFloor {
		t.Fatalf("ASCII page has %d dark pixels (floor %d) — the renderer produced a blank page; font chain %s",
			dark, inkFloor, res.FontChain())
	}
	// Result.Ink counts every pixel the blit touched (including faint
	// anti-aliased edges); darkPixels counts strongly-dark ones. Ink must
	// therefore be at least the dark count, and it must be non-zero.
	if res.Ink < dark || res.Ink == 0 {
		t.Errorf("Result.Ink = %d, decoded dark pixels = %d — the reported ink count must MEASURE the page (ink >= dark > 0)",
			res.Ink, dark)
	}
	if res.Missing != 0 {
		t.Errorf("Missing = %d runes for ASCII text, want 0 (chain %s)", res.Missing, res.FontChain())
	}
}

// TestRenderFileInksCJK pins CJK ink through the same chain (the case that
// worked before and must keep working).
func TestRenderFileInksCJK(t *testing.T) {
	src := writeText(t, "cjk.txt", "你好，世界\n中文渲染测试\n")
	res, err := RenderFile(src, "", Options{})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	dark := darkPixels(t, res.Outputs[0])
	if dark < inkFloor {
		t.Fatalf("CJK page has %d dark pixels (floor %d) — blank render; font chain %s", dark, inkFloor, res.FontChain())
	}
	if res.Missing != 0 {
		t.Errorf("Missing = %d runes for CJK text, want 0 (chain %s)", res.Missing, res.FontChain())
	}
}

// TestPinnedLatinLessFontInksLatin reproduces the exact failing invocation:
// the CJK-only face is PINNED as the metric font (what the old default did
// implicitly) and the text is Latin. The chain must supply the Latin glyphs
// from a fallback faces rather than write a blank page.
func TestPinnedLatinLessFontInksLatin(t *testing.T) {
	if _, err := os.Stat(droidCJKOnlyFont); err != nil {
		t.Skipf("CJK-only face missing: %s", droidCJKOnlyFont)
	}
	src := writeText(t, "pinned.txt", "hello world\nABC 123\n")
	res, err := RenderFile(src, "", Options{Font: droidCJKOnlyFont})
	if err != nil {
		t.Fatalf("RenderFile (pinned CJK-only font + Latin text): %v", err)
	}
	if res.Font != droidCJKOnlyFont {
		t.Errorf("Font = %q, want the pinned metric font %q (the pen must follow what the user pinned)", res.Font, droidCJKOnlyFont)
	}
	if len(res.Fonts) < 2 {
		t.Fatalf("font chain = %v, want the pinned face PLUS a fallback that can draw Latin", res.Fonts)
	}
	dark := darkPixels(t, res.Outputs[0])
	if dark < inkFloor {
		t.Fatalf("pinned CJK-only font + Latin text rendered %d dark pixels (floor %d) — Latin was silently dropped; font chain %s",
			dark, inkFloor, res.FontChain())
	}
}

// TestRenderFileDefaultOutputNamingAndInk pins the default naming contract (a
// name in the DECLARED text2img scratch class tree, accepted by the reclaim
// predicate) AND that the default-named page carries ink.
//
// THE CONTRACT CHANGED DELIBERATELY (task 01a1b3cf): the old default was
// `<input stem>.png` — BESIDE THE SOURCE — which left an untracked PNG in the
// run's own git ground (Rule 30). A regression here means the defect is back, so
// the assertions are the reclaim's own: absolute, in the class root, and
// collectable by name.
func TestRenderFileDefaultOutputNamingAndInk(t *testing.T) {
	home := t.TempDir()
	t.Setenv(scratch.HomeDirEnvName, home)

	src := writeText(t, "notes.txt", "default naming\n")
	res, err := RenderFile(src, "", Options{})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	if len(res.Outputs) != 1 {
		t.Fatalf("Outputs = %v, want 1 page", res.Outputs)
	}
	want := res.Outputs[0]
	root, rerr := scratch.Text2ImgDumpRoot()
	if rerr != nil || root == "" {
		t.Fatalf("Text2ImgDumpRoot = %q, %v", root, rerr)
	}
	if !filepath.IsAbs(want) || filepath.Dir(want) != root {
		t.Fatalf("default output %s must be an absolute path inside the class root %s", want, root)
	}
	if !scratch.IsText2ImgDumpName(filepath.Base(want)) {
		t.Fatalf("default output name %q is refused by scratch.IsText2ImgDumpName", filepath.Base(want))
	}
	if legacy := strings.TrimSuffix(src, filepath.Ext(src)) + ".png"; inkFileExists(legacy) {
		t.Fatalf("legacy default %s was written beside the source (the defect)", legacy)
	}
	if dark := darkPixels(t, want); dark < inkFloor {
		t.Fatalf("default-named page %s has %d dark pixels (floor %d)", want, dark, inkFloor)
	}
}

// TestRenderFileMultiPageInkPerPage forces pagination with a small --lines and
// asserts EVERY page exists, is named <out>-N.png, and carries ink of its own
// (a multi-page render whose later pages are blank is the same false success).
func TestRenderFileMultiPageInkPerPage(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 6; i++ {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", 8))
	}
	src := writeText(t, "many.txt", b.String())
	out := filepath.Join(t.TempDir(), "many.png")
	res, err := RenderFile(src, out, Options{Lines: 2})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	if res.Pages != 3 {
		t.Fatalf("Pages = %d, want 3 (6 lines, 2 lines/page)", res.Pages)
	}
	total := 0
	for i, p := range res.Outputs {
		want := filepath.Join(filepath.Dir(out), "many-"+strconv.Itoa(i+1)+".png")
		if p != want {
			t.Errorf("page %d name = %q, want %q", i+1, p, want)
		}
		dark := darkPixels(t, p)
		if dark < inkFloor {
			t.Errorf("page %d (%s) has %d dark pixels (floor %d) — a blank page in a multi-page render", i+1, p, dark, inkFloor)
		}
		total += dark
	}
	// Ink counts every touched pixel (faint anti-aliased edges included), dark
	// counts the strongly-dark ones, so the reported total must be >= the dark
	// sum over the pages actually written.
	if res.Ink < total {
		t.Errorf("Result.Ink = %d < sum of per-page dark pixels %d — reported ink cannot be smaller than the ink on the pages", res.Ink, total)
	}
}

// TestRenderFileAllowsWhitespaceOnlyPage — the guard is about TEXT that inks
// nothing, never about ink in general: a page carrying only whitespace (an
// intentional run of blank lines) is legitimate and must still render,
// otherwise the guard would refuse perfectly good documents.
func TestRenderFileAllowsWhitespaceOnlyPage(t *testing.T) {
	src := writeText(t, "blank.txt", "   \n\t\n")
	res, err := RenderFile(src, "", Options{})
	if err != nil {
		t.Fatalf("whitespace-only text must render (got %v)", err)
	}
	if res.Ink != 0 {
		t.Errorf("Ink = %d for whitespace-only text, want 0", res.Ink)
	}
	if dark := darkPixels(t, res.Outputs[0]); dark != 0 {
		t.Errorf("whitespace-only page has %d dark pixels, want 0", dark)
	}
}

// TestRenderFileRefusesZeroInkPage is the loud-failure half: when the text
// resolves to no drawable glyph anywhere in the chain, RenderFile must FAIL
// with the font chain and the measured ink count rather than write a blank PNG
// and report success.
func TestRenderFileRefusesZeroInkPage(t *testing.T) {
	chain, err := loadFontChain("")
	if err != nil {
		t.Fatalf("loadFontChain: %v", err)
	}
	// Find a printable rune the WHOLE chain cannot map — the private-use area
	// is the reliable place for it, and the search keeps the test host-independent
	// (no assumption about which face is installed).
	r := rune(0)
	for c := rune(0xE000); c <= 0xF8FF; c++ {
		if missingGlyphs(string(c), chain) == 1 {
			r = c
			break
		}
	}
	if r == 0 {
		t.Skip("every private-use rune is covered by this host's chain: no zero-ink fixture")
	}
	src := writeText(t, "nogfixture.txt", string(r))
	out := filepath.Join(t.TempDir(), "noink.png")
	_, err = RenderFile(src, out, Options{})
	if err == nil {
		t.Fatalf("RenderFile(%q) succeeded — a page with zero drawable glyphs must FAIL LOUDLY", string(r))
	}
	msg := err.Error()
	for _, want := range []string{"0 像素", "字体链", chain[0].path} {
		if !strings.Contains(msg, want) {
			t.Errorf("error must name %q; got: %v", want, err)
		}
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Errorf("blank page %s was written despite the failure — the guard must refuse BEFORE creating the file", out)
	}
}

// TestFontChainCoversLatinAndCJK asserts the chain itself: the metric font can
// draw Latin (without which the layout face is a lie) and SOME face in the chain
// can draw CJK.
func TestFontChainCoversLatinAndCJK(t *testing.T) {
	chain, err := loadFontChain("")
	if err != nil {
		t.Fatalf("loadFontChain: %v", err)
	}
	if len(chain) < 2 {
		t.Fatalf("chain has %d face(s): %v — Latin and CJK on this host need at least two faces", len(chain), chain.paths())
	}
	if !faceHas(chain[0], coverageLatinProbe) {
		t.Errorf("metric font %s cannot draw Latin %q — the layout face must cover the pen's own text",
			chain[0].path, string(coverageLatinProbe))
	}
	cjk := false
	for _, f := range chain {
		if faceHas(f, coverageCJKProbe) {
			cjk = true
			break
		}
	}
	if !cjk {
		t.Errorf("no face in the chain covers CJK %q: %v", string(coverageCJKProbe), chain.paths())
	}
	for _, f := range chain {
		if f.path == "" {
			t.Error("chain member without a path — diagnostics could not name it")
		}
	}
}
