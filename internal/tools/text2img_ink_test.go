package tools

// Ink assertions for the shared text2img tool path. The pre-existing sibling
// test (cov_vision_text2img_test.go) checks that the PNG EXISTS and that the
// summary carries the ✅ header — both of which a completely blank page
// satisfies (the reported bug: a Latin string on a CJK-only face produced a
// correctly-sized ALL-WHITE PNG and the summary still said "✅ … 1 页"). These
// tests decode the PNG and count DARK pixels, and they pin the summary line
// that carries the measured ink count, so a caller can never read "✅" over a
// blank page.

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/text2img"
)

// toolInkFloor is the minimum dark-pixel count a 20px text page must carry.
const toolInkFloor = 150

// darkPixels decodes a PNG and counts dark (ink) pixels.
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

// TestText2ImgASCIIDefaultWritesInk is the shared-path regression: the ASCII
// default render must ink the page and REPORT the measured ink.
func TestText2ImgASCIIDefaultWritesInk(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "ascii.txt")
	if err := os.WriteFile(src, []byte("hello world\nABC 123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	summary, res, err := Text2ImgDetail(src, "", text2img.Options{})
	if err != nil {
		t.Fatalf("Text2ImgDetail: %v", err)
	}
	// The default destination is the scratch class tree, NOT a file beside src
	// (task 01a1b3cf): read the path the renderer actually reports and check it
	// is one the reclaim predicate accepts, instead of assuming a location that
	// the old default produced.
	if len(res.Outputs) == 0 {
		t.Fatalf("default ASCII render reported no output path; summary:\n%s", summary)
	}
	out := res.Outputs[0]
	if !filepath.IsAbs(out) || !scratch.IsText2ImgDumpName(filepath.Base(out)) {
		t.Fatalf("default ASCII render landed at %q — want an absolute path whose name the reclaim predicate accepts", out)
	}
	dark := darkPixels(t, out)
	if dark < toolInkFloor {
		t.Fatalf("default ASCII render wrote %s with %d dark pixels (floor %d) — blank page reported as success; summary:\n%s",
			out, dark, toolInkFloor, summary)
	}
	if res.Ink < dark {
		t.Errorf("Result.Ink = %d < decoded dark pixels %d", res.Ink, dark)
	}
	// The summary must expose the measurement, not just a ✅.
	if !strings.Contains(summary, "墨迹 ") {
		t.Errorf("summary does not report the measured ink count:\n%s", summary)
	}
	if !strings.Contains(summary, res.FontChain()) {
		t.Errorf("summary does not name the font chain %q:\n%s", res.FontChain(), summary)
	}
	if !strings.Contains(summary, "✅") {
		t.Errorf("summary lost its ✅ success header:\n%s", summary)
	}
}

// TestText2ImgCJKWritesInk pins CJK through the shared path (both scripts must
// ink, since the font chain must cover Latin AND CJK).
func TestText2ImgCJKWritesInk(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "cjk.txt")
	if err := os.WriteFile(src, []byte("你好，世界\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, res, err := Text2ImgDetail(src, "", text2img.Options{})
	if err != nil {
		t.Fatalf("Text2ImgDetail: %v", err)
	}
	if len(res.Outputs) == 0 {
		t.Fatalf("CJK default render reported no output path")
	}
	out := res.Outputs[0]
	if !filepath.IsAbs(out) || !scratch.IsText2ImgDumpName(filepath.Base(out)) {
		t.Fatalf("CJK default render landed at %q — want an absolute path whose name the reclaim predicate accepts", out)
	}
	if dark := darkPixels(t, out); dark < toolInkFloor {
		t.Fatalf("CJK render wrote %s with %d dark pixels (floor %d)", out, dark, toolInkFloor)
	}
}

// TestText2ImgFailsLoudlyOnZeroInk is the failure half through the shared path:
// text the font chain cannot draw must fail with a diagnostic naming the font
// chain and the measured ink count, and must NOT leave a blank PNG behind.
func TestText2ImgFailsLoudlyOnZeroInk(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "noink.txt")
	// Private-use runes: no general-purpose face maps them.
	if err := os.WriteFile(src, []byte("\ue123\ue124\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "noink.png")
	summary, _, err := Text2ImgDetail(src, out, text2img.Options{})
	if err == nil {
		t.Fatalf("zero-ink text succeeded with summary %q — it must FAIL LOUDLY", summary)
	}
	msg := err.Error()
	for _, want := range []string{"0 像素", "字体链"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error must name %q; got: %v", want, err)
		}
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Errorf("blank page %s was written even though the render failed", out)
	}
}
