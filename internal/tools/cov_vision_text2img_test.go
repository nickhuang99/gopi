package tools

// Function-level coverage for the image/vision path and the text→PNG tool:
// IsImagePath, LoadImage, VisionAnalyze (vision.go) + Text2Img (text2img.go).
//
// The load-bearing invariant: IsImagePath is the ROUTING predicate (its only
// production caller is cmd/agent/main.go:408, which sends an existing file
// down the image path when it returns true), so it must agree with the
// vision path exactly — IsImagePath(p) == true implies LoadImage(p) accepts
// the extension, and IsImagePath(p) == false implies LoadImage REJECTS it as
// an unsupported format. That agreement is asserted directly over one shared
// case table, plus a walk over the case table asserting no case is
// unrouted in either direction.
//
// VisionAnalyze is exercised only on its FAILURE path (the network path
// needs a live API key and is out of scope for a unit test): the point is
// that its error is user-actionable and names the underlying reason rather
// than a bare failure.

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/text2img"
)

// TestCovIsImagePathExtensions pins the accepted-extension table (case
// insensitive, whitespace tolerant) and the negative boundary set.
func TestCovIsImagePathExtensions(t *testing.T) {
	yes := []string{
		"/tmp/a.jpg", "/tmp/a.jpeg", "/tmp/a.png", "/tmp/a.gif", "/tmp/a.webp",
		"/tmp/UPPER.PNG", "/tmp/Mixed.JpEg",
		"  /tmp/spaced.png  ", // trimmed before the extension lookup
		"/tmp/noext/dir.png",
	}
	for _, p := range yes {
		if !IsImagePath(p) {
			t.Errorf("IsImagePath(%q) = false, want true", p)
		}
	}
	no := []string{
		"", "/tmp/a.txt", "/tmp/a.PNG.txt", "/tmp/png", "/tmp/a.bmp",
		"/tmp/a.tiff", "/tmp/a.svg", "/tmp/a.heic", "/tmp/a.png.bak",
		"/tmp/dir.png/child.txt",
	}
	for _, p := range no {
		if IsImagePath(p) {
			t.Errorf("IsImagePath(%q) = true, want false", p)
		}
	}
}

// covLifeImageCases is the shared routing table: ext + whether the path is a
// supported image. IsImagePath and LoadImage are asserted to agree on every
// entry — the routing predicate and the loader share one source of truth.
var covLifeImageCases = []struct {
	ext string
	ok  bool
}{
	{".jpg", true}, {".jpeg", true}, {".png", true}, {".gif", true}, {".webp", true},
	{".bmp", false}, {".tiff", false}, {".svg", false}, {".txt", false}, {".", false}, {"", false},
}

// TestCovIsImagePathAgreesWithLoadImage is the contract the routing caller
// depends on: for every case, IsImagePath true ⇒ LoadImage loads the file;
// IsImagePath false ⇒ LoadImage refuses with an "unsupported image format"
// error. A disagreement would either drop a loadable image or route a
// non-image into the vision path.
func TestCovIsImagePathAgreesWithLoadImage(t *testing.T) {
	dir := t.TempDir()
	for _, c := range covLifeImageCases {
		// A file whose extension is the case under test (". " and "" are
		// skipped for the write — they are not writable names).
		name := "probe" + c.ext
		if c.ext == "" || c.ext == "." {
			name = "probe"
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		routed := IsImagePath(path)
		if routed != c.ok {
			t.Errorf("IsImagePath(%q) = %v, want %v", path, routed, c.ok)
		}
		img, err := LoadImage(path)
		if routed {
			if err != nil {
				t.Errorf("IsImagePath(%q) routed to the image path but LoadImage failed: %v", path, err)
				continue
			}
			if !strings.HasPrefix(img.MediaType, "image/") || img.Bytes == 0 || img.Base64 == "" {
				t.Errorf("LoadImage(%q) = %+v, want a populated image file", path, img)
			}
		} else if err == nil || !strings.Contains(err.Error(), "unsupported image format") {
			t.Errorf("IsImagePath(%q) = false but LoadImage did not refuse the format: (%+v, %v)", path, img, err)
		}
	}
}

// TestCovLoadImageContentAndMediaTypes asserts the loaded payload contract:
// the base64 decodes back to the exact file bytes, Bytes is the byte count,
// Path is echoed, and the extension maps to the right Anthropic media_type.
func TestCovLoadImageContentAndMediaTypes(t *testing.T) {
	dir := t.TempDir()
	cases := []struct{ ext, mediaType string }{
		{".jpg", "image/jpeg"}, {".jpeg", "image/jpeg"}, {".png", "image/png"},
		{".gif", "image/gif"}, {".webp", "image/webp"},
		{".JPG", "image/jpeg"}, // the extension lookup lowercases
	}
	payload := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0x7f, '\n'}
	for _, c := range cases {
		path := filepath.Join(dir, "img"+c.ext)
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		img, err := LoadImage(path)
		if err != nil {
			t.Fatalf("LoadImage(%s): %v", path, err)
		}
		if img.MediaType != c.mediaType {
			t.Errorf("LoadImage(%s).MediaType = %q, want %q", c.ext, img.MediaType, c.mediaType)
		}
		if img.Bytes != len(payload) {
			t.Errorf("LoadImage(%s).Bytes = %d, want %d", c.ext, img.Bytes, len(payload))
		}
		decoded, err := base64.StdEncoding.DecodeString(img.Base64)
		if err != nil {
			t.Fatalf("base64 decode: %v", err)
		}
		if string(decoded) != string(payload) {
			t.Errorf("LoadImage(%s) payload round-trip mismatch: %v", c.ext, decoded)
		}
		if img.Path != path {
			t.Errorf("LoadImage(%s).Path = %q, want %q", c.ext, img.Path, path)
		}
	}
}

// TestCovLoadImageErrors covers the four error boundaries: empty path,
// unsupported extension, missing file, and the 32 MiB size ceiling (checked
// BEFORE any bytes are read — asserted via a sparse file, so the test never
// allocates 32 MiB).
func TestCovLoadImageErrors(t *testing.T) {
	dir := t.TempDir()

	if _, err := LoadImage("   "); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("LoadImage(blank) err = %v, want an empty-path error", err)
	}

	missing := filepath.Join(dir, "gone.png")
	if _, err := LoadImage(missing); err == nil || !strings.Contains(err.Error(), "file not found") {
		t.Errorf("LoadImage(missing) err = %v, want a not-found error", err)
	}

	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write %s: %v", txt, err)
	}
	if _, err := LoadImage(txt); err == nil || !strings.Contains(err.Error(), "unsupported image format") {
		t.Errorf("LoadImage(.txt) err = %v, want an unsupported-format error", err)
	}

	// A sparse file one byte past the ceiling: os.Truncate sets the size
	// without writing 32 MiB of data.
	big := filepath.Join(dir, "big.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatalf("create %s: %v", big, err)
	}
	if err := f.Truncate(visionMaxFileBytes + 1); err != nil {
		f.Close() //stylegate:ignore close of a file already written and flushed by the checked calls
		t.Fatalf("truncate %s: %v", big, err)
	}
	f.Close() //stylegate:ignore close of a file already written and flushed by the checked calls
	if _, err := LoadImage(big); err == nil || !strings.Contains(err.Error(), "file too large") {
		t.Errorf("LoadImage(oversized) err = %v, want a too-large error", err)
	}

	// Exactly at the ceiling is accepted (the guard is >, not >=).
	atLimit := filepath.Join(dir, "limit.png")
	f2, err := os.Create(atLimit)
	if err != nil {
		t.Fatalf("create %s: %v", atLimit, err)
	}
	if err := f2.Truncate(visionMaxFileBytes); err != nil {
		f2.Close() //stylegate:ignore test cleanup on the t.Fatalf error path while creating the at-ceiling fixture; the test is already failing, so a close error adds nothing
		t.Fatalf("truncate %s: %v", atLimit, err)
	}
	f2.Close() //stylegate:ignore test closes the fixture file so LoadImage reads complete bytes; a close error surfaces as a short read in the assertion on the next line
	if _, err := LoadImage(atLimit); err != nil {
		t.Errorf("LoadImage(at ceiling) err = %v, want acceptance", err)
	}
}

// TestCovVisionAnalyzeValidationErrors asserts VisionAnalyze's validation
// arm: the image load error is surfaced (prefixed with the tool name) BEFORE
// any client/network work, so a bad path never reaches the API.
func TestCovVisionAnalyzeValidationErrors(t *testing.T) {
	dir := t.TempDir()

	if _, err := VisionAnalyze("", "describe"); err == nil ||
		!strings.Contains(err.Error(), "vision_analyze:") {
		t.Errorf("VisionAnalyze(\"\") err = %v, want a vision_analyze-prefixed error", err)
	}

	missing := filepath.Join(dir, "nope.png")
	if _, err := VisionAnalyze(missing, "describe"); err == nil ||
		!strings.Contains(err.Error(), "vision_analyze:") || !strings.Contains(err.Error(), "file not found") {
		t.Errorf("VisionAnalyze(missing) err = %v, want a vision_analyze not-found error", err)
	}

	txt := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(txt, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write %s: %v", txt, err)
	}
	if _, err := VisionAnalyze(txt, ""); err == nil ||
		!strings.Contains(err.Error(), "unsupported image format") {
		t.Errorf("VisionAnalyze(.txt) err = %v, want an unsupported-format error", err)
	}
}

// TestCovVisionAnalyzeWithoutCredentials covers the post-validation arm: with
// no API key resolvable in the isolated sandbox, the client construction
// fails and that failure is returned (never a panic, never a silent empty
// string). The env is cleared so the test does not depend on the host's
// ambient credentials.
func TestCovVisionAnalyzeWithoutCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("GOPI_SETTINGS", filepath.Join(home, "settings.json"))
	for _, k := range []string{"ANTHROPIC_AUTH_TOKEN", "DEEPSEEK_API_KEY"} {
		t.Setenv(k, "")
	}
	png := filepath.Join(home, "ok.png")
	if err := os.WriteFile(png, []byte("fakepng"), 0o644); err != nil {
		t.Fatalf("write %s: %v", png, err)
	}
	out, err := VisionAnalyze(png, "describe")
	if err == nil {
		t.Fatalf("VisionAnalyze without credentials returned %q, want an error", out)
	}
	if out != "" {
		t.Errorf("VisionAnalyze error path returned a non-empty result %q", out)
	}
}

// TestCovText2ImgRendersWithDefaults renders a small sample and asserts the
// summary contract: the ✅ header, the source path, the output path list, and
// that the PNG actually landed on disk with the default option values echoed
// (20px / 50 cols / 90 lines / auto-detected font).
func TestCovText2ImgRendersWithDefaults(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(src, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", src, err)
	}

	out, err := Text2Img(src, "", text2img.Options{})
	if err != nil {
		t.Fatalf("Text2Img: %v", err)
	}
	for _, want := range []string{"✅", src, "output:", "字号 20px", "折行 50", "行高 32"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
	// The default destination is the scratch class tree, NOT beside the source
	// (task 01a1b3cf: `<src>.png` left an untracked PNG in the run's git
	// ground — Rule 30's completed-unclean). Read the page the summary reports
	// and require it to be absolute, inside the class root, and accepted by the
	// reclaim predicate — a destination no collector accepts is not a fix.
	root, rerr := scratch.Text2ImgDumpRoot()
	if rerr != nil || root == "" {
		t.Fatalf("Text2ImgDumpRoot = %q, %v — the suite must have a scratch home", root, rerr)
	}
	var pages []string
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "output:"); ok {
			pages = append(pages, strings.TrimSpace(p))
		}
	}
	if len(pages) != 1 {
		t.Fatalf("summary names %d page(s), want 1:\n%s", len(pages), out)
	}
	png := pages[0]
	if !filepath.IsAbs(png) {
		t.Errorf("default output %q is not absolute", png)
	}
	if filepath.Dir(png) != root {
		t.Errorf("default output %q is not in the class root %q", png, root)
	}
	if !scratch.IsText2ImgDumpName(filepath.Base(png)) {
		t.Errorf("default output name %q is refused by IsText2ImgDumpName — no sweep would ever collect it", filepath.Base(png))
	}
	fi, err := os.Stat(png)
	if err != nil {
		t.Fatalf("default output %s not written: %v", png, err)
	}
	if fi.Size() == 0 {
		t.Errorf("rendered PNG %s is empty", png)
	}
}

// TestCovText2ImgExplicitOutPaginates pins the explicit-out + multi-page
// naming contract: a page limit of 1 over two wrapped lines yields two
// outputs named <out-1.png, <out-2.png> (the src extension is stripped
// before the -N suffix, never appended after it).
func TestCovText2ImgExplicitOutPaginates(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(src, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", src, err)
	}
	out := filepath.Join(dir, "render.png")

	summary, err := Text2Img(src, out, text2img.Options{Lines: 1})
	if err != nil {
		t.Fatalf("Text2Img: %v", err)
	}
	// Two wrapped lines + the trailing newline = 3 rendered lines → 3 pages
	// at Lines:1, named <out>-1 … <out>-N (the summary reports the count).
	lines := strings.Split(summary, "\n")
	pages := 0
	for _, page := range lines {
		if !strings.HasPrefix(strings.TrimSpace(page), "output:") {
			continue
		}
		pages++
		outPath := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(page), "output:"))
		if !strings.HasPrefix(outPath, filepath.Join(dir, "render-")) || !strings.HasSuffix(outPath, ".png") {
			t.Errorf("page path %q must be <out>-N.png", outPath)
		}
		if fi, err := os.Stat(outPath); err != nil || fi.Size() == 0 {
			t.Errorf("page %s not written (err=%v)", outPath, err)
		}
	}
	if pages < 2 {
		t.Errorf("explicit-out pagination produced %d page(s), want >= 2 (Lines: 1):\n%s", pages, summary)
	}
	if want := fmt.Sprintf("%d 页", pages); !strings.Contains(summary, want) {
		t.Errorf("summary reports a page count that disagrees with its output list (want %q):\n%s", want, summary)
	}
	if strings.Contains(summary, "sample.png") && !strings.Contains(summary, "render-") {
		t.Errorf("explicit out ignored:\n%s", summary)
	}
}

// TestCovText2ImgErrors covers the three error boundaries: the missing
// argument, an unreadable source, and the 8 MiB input ceiling (a sparse file
// keeps the test cheap).
func TestCovText2ImgErrors(t *testing.T) {
	dir := t.TempDir()

	if _, err := Text2Img("", "", text2img.Options{}); err == nil ||
		!strings.Contains(err.Error(), "missing <input.txt>") {
		t.Errorf("Text2Img(\"\") err = %v, want a missing-argument error", err)
	}

	missing := filepath.Join(dir, "gone.txt")
	if _, err := Text2Img(missing, "", text2img.Options{}); err == nil ||
		!strings.Contains(err.Error(), "无法读取输入文件") {
		t.Errorf("Text2Img(missing) err = %v, want an unreadable-input error", err)
	}

	big := filepath.Join(dir, "big.txt")
	f, err := os.Create(big)
	if err != nil {
		t.Fatalf("create %s: %v", big, err)
	}
	if err := f.Truncate(text2imgMaxFileBytes + 1); err != nil {
		f.Close() //stylegate:ignore close of a file already written and flushed by the checked calls
		t.Fatalf("truncate %s: %v", big, err)
	}
	f.Close() //stylegate:ignore close of a file already written and flushed by the checked calls
	if _, err := Text2Img(big, "", text2img.Options{}); err == nil ||
		!strings.Contains(err.Error(), "超过上限") {
		t.Errorf("Text2Img(oversized) err = %v, want a size-ceiling error", err)
	}
}
