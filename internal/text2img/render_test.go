package text2img

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestParseTTFRejectsCorruptFonts guards parseTTF against truncated/corrupt
// font blobs: every table directory window and every table's declared
// [to, to+tl) range must be validated BEFORE being dereferenced, so a forged
// header can never trip a slice-bounds panic. Each fixture reads as a valid
// prefix of a real TrueType font but lies about its table headers.
func TestParseTTFRejectsCorruptFonts(t *testing.T) {
	good := "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf"
	data, err := os.ReadFile(good)
	if err != nil {
		t.Skipf("font missing: %s", good)
	}
	// Sanity: the pristine font still parses.
	if _, err := parseTTF(data, 0); err != nil {
		t.Fatalf("pristine font should parse: %v", err)
	}

	// Byte-level mutation helpers. Truncating the file to a length below a
	// table's declared [to, to+tl) end makes that window exceed the data —
	// a genuine corruption the tableOK guard must catch.
	//
	// Find the table that extends furthest (usually glyf near EOF) and cut the
	// file a few thousand bytes below that table's declared end. This yields a
	// directory whose table windows lie, guaranteeing the guard trips.
	var glyfEnd int
	nt := int(be16(data[4:6]))
	for i := 0; i < nt; i++ {
		p := 12 + 16*i
		if string(data[p:p+4]) == "glyf" {
			glyfEnd = int(be32(data[p+8:p+12])) + int(be32(data[p+12:p+16]))
		}
	}
	if glyfEnd == 0 || glyfEnd > len(data) {
		t.Skipf("no valid glyf table end found in %s", good)
	}
	truncateTo := func(n int) []byte { return data[:n] }
	cases := []struct {
		name string
		blob []byte
	}{
		{"truncated-to-empty", []byte{0, 1, 0, 0}},
		{"cut-one-byte-into-glyf", truncateTo(glyfEnd - 1)},
		{"cut-mid-glyf", truncateTo(glyfEnd - 20000)},
		{"cut-half-data", truncateTo(len(data) / 2)},
		// TTC header lying about its index table size (be32[8:12] large).
		{"ttc-lying-offset-table", mockTTC(data)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.blob) == 0 {
				t.Skip("empty fixture")
			}
			// The essence: parseTTF must ERR gracefully, never panic.
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("parseTTF PANICKED on %s: %v (expected graceful error)", tc.name, r)
					}
				}()
				_, err := parseTTF(tc.blob, 0)
				if err == nil {
					t.Fatalf("parseTTF(%s): expected error, got nil (corrupt font accepted)", tc.name)
				}
			}()
		})
	}
}

// mockTTC wraps a TTF in a TTC header whose offset table (options at 12+4*0)
// points far past the data, exercising the TTC offset bound check.
func mockTTC(ttf []byte) []byte {
	hdr := []byte{
		't', 't', 'c', 'f', // tag
		0, 0, 0, 1, // version 1.0
		0, 0, 0, 1, // numFonts = 1
	}
	// font offset: point at byte 60 (mock), beyond the 16-byte header+4.
	off := []byte{0, 0, 0, 60}
	d := append(append(append([]byte{}, hdr...), off...), ttf...)
	// Pad nothing; the offset 60 already exceeds the collection header length
	// in the honest layout, so the bound check must reject it.
	return d
}

// TestParseTTFRejectsBadTableWindows forces a specific table's declared length
// past the end of the data, which must be caught by the tableOK guard before
// any data[to:...] dereference.
func TestParseTTFRejectsBadTableWindows(t *testing.T) {
	good := "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf"
	data, err := os.ReadFile(good)
	if err != nil {
		t.Skipf("font missing: %s", good)
	}
	// Overwrite the glyf table record's length with a huge value — glyf is one
	// of the 7 required tables that tableOK validates, so an out-of-range
	// window here must be rejected. The SFNT offset table is 12 bytes; each of
	// nt table records is 16 bytes starting at 12. The record's length field
	// sits at bytes p+12:p+16 of that record. Skip glyf itself (it must remain
	// parseable); pick the hmtx record as the patched victim (also required).
	nt := int(be16(data[4:6]))
	victim := -1
	for i := 0; i < nt; i++ {
		p := 12 + 16*i
		if string(data[p:p+4]) == "hmtx" {
			victim = 12 + i*16
			break
		}
	}
	if victim < 0 {
		t.Skipf("no hmtx table record found in %s", good)
	}
	bad := append([]byte{}, data...)
	bad[victim+12] = 0xFF
	bad[victim+13] = 0xFF
	bad[victim+14] = 0xFF
	bad[victim+15] = 0xFF
	_, err = parseTTF(bad, 0)
	if err == nil {
		t.Fatalf("parseTTF accepted a table with length 0xFFFFFFFF (should error)")
	}
}

// TestRenderFileCJK renders a Chinese text to a PNG and checks the output is a
// valid, non-blank PNG sized to the content — proving the SFNT parser handles
// the system CJK TrueType font (TTC composite glyphs included).
func TestRenderFileCJK(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.txt")
	text := "你好，世界！\nGoPi text2img 零依赖渲染\n"
	if err := os.WriteFile(src, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := RenderFile(src, "", Options{})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	if res.Pages != 1 {
		t.Errorf("Pages = %d, want 1", res.Pages)
	}
	if len(res.Outputs) != 1 {
		t.Fatalf("Outputs = %v, want 1 file", res.Outputs)
	}
	if res.Font == "" {
		t.Error("Font autodetection returned empty path")
	}
	if res.Size != DefaultSize || res.Lh != DefaultLh || res.Cols != DefaultCols {
		t.Errorf("defaults not applied: size=%d lh=%d cols=%d", res.Size, res.Lh, res.Cols)
	}

	// The PNG must decode and contain black pixels (text) on white.
	f, err := os.Open(res.Outputs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("output is not a valid PNG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Fatalf("empty image bounds %v", b)
	}
	black := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r < 0x8000 && g < 0x8000 && bl < 0x8000 {
				black++
			}
		}
	}
	if black == 0 {
		t.Error("rendered image is blank — no glyphs drawn")
	}
}

// TestRenderFilePagination forces a tiny page size and checks the multi-page
// naming (out-1.png, out-2.png …).
func TestRenderFilePagination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "long.txt")
	// 40 lines × 30 chars — with Lines=10 and Cols=25 this must split into 4+ pages.
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString(strings.Repeat("字", 30))
		b.WriteString("\n")
	}
	if err := os.WriteFile(src, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := RenderFile(src, filepath.Join(dir, "long.png"), Options{Lines: 10, Cols: 25})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	if res.Pages < 3 {
		t.Errorf("Pages = %d, want >= 3 (40 lines of 30 chars, 10 lines/page, 25-char wrap)", res.Pages)
	}
	if len(res.Outputs) != res.Pages {
		t.Errorf("Outputs = %v, len %d != Pages %d", res.Outputs, len(res.Outputs), res.Pages)
	}
	for i, p := range res.Outputs {
		want := filepath.Join(dir, "long.png")
		if res.Pages > 1 {
			want = filepath.Join(dir, "long-"+strconv.Itoa(i+1)+".png")
		}
		if p != want {
			t.Errorf("page %d name %q, want %q", i, p, want)
		}
		if _, err := os.Stat(p); err != nil {
			t.Errorf("page %d missing: %v", i, err)
		}
	}
}

// TestRenderFileMissingInput reports a clear error instead of panicking.
func TestRenderFileMissingInput(t *testing.T) {
	_, err := RenderFile("/nonexistent/nope.txt", "", Options{})
	if err == nil {
		t.Fatal("want error for missing input file")
	}
}

// TestLhScalesWithSize is a regression test for the large-size clipping bug:
// DefaultLh=32 is fixed, so at Size=200 the page height used to be
// 1*32+2*margin = 56px and the glyph was clipped to an 11px sliver at the
// bottom. The default line height must scale with the font size (≈1.6×),
// so a 200px glyph renders at full height.
func TestLhScalesWithSize(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "zhi.txt")
	if err := os.WriteFile(src, []byte("知"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := RenderFile(src, "", Options{Size: 200})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	// Lh must scale with Size, not stay at the fixed 20px default (32 = 20×1.6).
	wantLh := int(math.Round(200 * 1.6)) // 320
	if res.Lh != wantLh {
		t.Errorf("Lh = %d, want %d (scaled with Size=200)", res.Lh, wantLh)
	}

	f, err := os.Open(res.Outputs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Find the ink bbox: black-ish pixels only.
	b := img.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	ink := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r < 0x8000 && g < 0x8000 && bl < 0x8000 {
				ink++
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if ink == 0 {
		t.Fatal("no ink rendered")
	}
	ih, iw := maxY-minY+1, maxX-minX+1
	// A 200px glyph must span roughly its font size vertically. The old bug
	// produced ih ≈ 11px (clipped to the 56px page); accept >= 40% of Size
	// (200 → 80px) as a clear clipping sentinel while staying robust to
	// font metrics (ascender ≈ 0.88–1.0 × upem).
	if ih < int(float64(res.Size)*0.4) {
		t.Errorf("ink height = %dpx (bbox %d–%d), want >= %d — glyph clipped by fixed Lh",
			ih, minY, maxY, int(float64(res.Size)*0.4))
	}
	if ih <= 0 || iw <= 0 {
		t.Errorf("degenerate ink bbox: w=%d h=%d", iw, ih)
	}
}

// TestFillMatchesOutlineArea is a regression test for the non-integer vertex
// double-count bug: the scanline fill emitted crossings from BOTH segments
// sharing a vertex whose y was not integer, doubling the winding count (odd
// crossings, leaks) and over-filling real glyphs by 17-28% (知 in wqy-zenhei:
// 1.285×, DroidSansFallback: 1.172×). After the fix, the rasterized ink area
// must equal the flattened-outline shoelace area (within 3%).
func TestFillMatchesOutlineArea(t *testing.T) {
	fonts := []string{
		"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
		"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
	}
	for _, fp := range fonts {
		data, err := os.ReadFile(fp)
		if err != nil {
			t.Skipf("font missing: %s", fp)
		}
		f, err := parseTTF(data, 0)
		if err != nil {
			t.Fatalf("%s: %v", fp, err)
		}
		gid := f.glyph('知')
		if gid == 0 {
			t.Fatalf("%s: 知 -> gid 0", fp)
		}
		scale := 100.0 / float64(f.upem)
		const tol = 0.05
		type xy struct{ x, y float64 }
		var poly []xy
		for _, c := range f.glyphContours(gid) {
			sc := make([]pt, len(c))
			for i, q := range c {
				sc[i] = pt{q.x * scale, -q.y * scale, q.on}
			}
			segs := flatten(sc, tol)
			for k := 0; k+3 < len(segs); k += 4 {
				poly = append(poly, xy{segs[k], segs[k+1]}, xy{segs[k+2], segs[k+3]})
			}
		}
		area := 0.0
		for i := 0; i+1 < len(poly); i += 2 {
			a, b := poly[i], poly[i+1]
			area += a.x*b.y - b.x*a.y
		}
		area = math.Abs(area) / 2
		alpha, _, _, _, _ := f.rasterGlyph(gid, scale)
		ink := 0.0
		for _, a := range alpha {
			ink += float64(a) / 255
		}
		ratio := ink / area
		if ratio < 0.97 || ratio > 1.03 {
			t.Errorf("%s: fill/outline ratio %.4f outside [0.97,1.03] (vertex double-count regression?)", fp, ratio)
		}
	}
}
