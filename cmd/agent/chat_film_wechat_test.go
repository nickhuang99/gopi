package main

// chat_film_wechat_test.go — the `--wechat` product's own invariants, plus the
// even-frame contract the frames are written under.
//
// WHAT IS PINNED HERE, and why each one is a regression guard rather than a
// restatement of the code:
//
//   * the SWITCH parses as a bare word (and refuses a value it cannot honour);
//   * the product is STRICTLY stronger than the default — the over-ceiling size
//     is refused only when the destination was named, which is what "the default
//     product is unchanged" means at this seam; the ODD HEIGHT is refused for
//     BOTH, because it is a property of the file rather than of the reader (the
//     parity requirement, checked on both axes and naming the failing one);
//   * the even-frame writer is LOSSLESS on the pixels it keeps and a NO-OP on a
//     frame that is already even (the same slice, not a re-encode);
//   * the default composition is BYTE-IDENTICAL to what internal/gifgen produces
//     for the film's own options — the flag path adds a gate, never a step.
//
// WHAT IS NOT HERE: the end-to-end path (`--wechat --film DIR --out F.gif`) needs
// a display, a terminal and a recording; it is proved by RUNNING it (Rule 31), and
// the receipt is the run's report.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/gifgen"
)

// TestChatParseWechatArg pins the bare switch and its one refusal: a value the
// switch cannot carry is named rather than ignored, and words that merely START
// with it are left for the positional handler.
func TestChatParseWechatArg(t *testing.T) {
	if matched, err := chatParseWechatArg(chatFlagWechat); !matched || err != nil {
		t.Fatalf("the bare switch was not accepted: matched=%v err=%v", matched, err)
	}
	matched, err := chatParseWechatArg(chatFlagWechat + chatFlagValueSep + "1")
	if !matched || err == nil {
		t.Fatalf("a valued spelling was accepted: matched=%v err=%v", matched, err)
	}
	if !strings.Contains(err.Error(), chatFlagWechat) || !strings.Contains(err.Error(), chatFlagOut) {
		t.Errorf("the refusal does not name the switch and the flag it wraps: %v", err)
	}
	for _, other := range []string{"--no-wechat", "--wechatty", "--out"} {
		if matched, err := chatParseWechatArg(other); matched {
			t.Errorf("%q was taken as the switch — only the exact bare word is", other)
		} else if err != nil {
			t.Errorf("%q produced an error it should not have: %v", other, err)
		}
	}
}

// TestChatFilmWechatIsStrictlyStrongerThanTheDefault is the flag's own definition
// as a test: an animation that the DEFAULT product accepts is REFUSED once the
// phone was named, for both added properties — and the refusal carries the numbers
// (Rule 15: never a silent over-limit hand-over).
func TestChatFilmWechatIsStrictlyStrongerThanTheDefault(t *testing.T) {
	ok := func(w, h, b int) *gifgen.Result {
		return &gifgen.Result{
			Out: "film.gif", Width: w, Height: h, Bytes: b,
			LoopCount: gifgen.LoopForever, LoopReadBack: gifgen.LoopForever,
		}
	}
	// Even width and a loop block, but an ODD height: the parity requirement is
	// a property of the FILE, not of the destination, so BOTH products refuse it
	// — and the refusal names the axis that failed.
	oddTall := ok(playableGIFMaxWidth, 521, 1024)
	err := chatFilmRequirePlayable(oddTall, false)
	if err == nil {
		t.Fatal("the default product handed over an odd height — an encoder that subsamples chroma cannot open it, and the phone is not the only reader")
	}
	if !strings.Contains(err.Error(), "ODD") || !strings.Contains(err.Error(), strconv.Itoa(oddTall.Height)) {
		t.Errorf("the height refusal does not say what is wrong and by how much: %v", err)
	}
	if !strings.Contains(err.Error(), filmAxisHeight) || strings.Contains(err.Error(), filmAxisWidth) {
		t.Errorf("the height refusal does not name the failing axis (%q): %v", filmAxisHeight, err)
	}
	if err = chatFilmRequirePlayable(oddTall, true); err == nil {
		t.Fatal("--wechat accepted an odd height — an encoder that subsamples chroma cannot encode it")
	}

	// The same shape for the ceiling: 1 byte over is refused, and the refusal
	// carries the file's size, the limit, and the unit a caller reads.
	over := ok(playableGIFMaxWidth, 342, wechatGIFMaxBytes+1)
	if err := chatFilmRequirePlayable(over, false); err != nil {
		t.Fatalf("the default product refused a size, which it never checked: %v", err)
	}
	err = chatFilmRequirePlayable(over, true)
	if err == nil {
		t.Fatal("--wechat handed over a file over the byte ceiling")
	}
	for _, want := range []string{strconv.Itoa(wechatGIFMaxBytes + 1), strconv.Itoa(wechatGIFMaxBytes)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the ceiling refusal never mentions %s: %v", want, err)
		}
	}

	// Exactly AT the ceiling is inside it: the bound is inclusive, and a test
	// that only ever proved the outside would not say so.
	if err := chatFilmRequirePlayable(ok(playableGIFMaxWidth, 342, wechatGIFMaxBytes), true); err != nil {
		t.Errorf("a file exactly at the ceiling was refused: %v", err)
	}
}

// TestChatFilmWechatFlagNeedsTheAnimation pins the refusal for the destination
// without a product — the one command line that asks for a phone-shaped artifact
// and names no artifact at all.
//
// It asserts on the RENDERED sentence, with the arguments the parser actually
// passes: the format string alone carries `%s` where the flag names go, so
// checking the raw constant would pass for a refusal that never names a flag —
// the shape this test exists to prevent.
func TestChatFilmWechatFlagNeedsTheAnimation(t *testing.T) {
	rendered := fmt.Sprintf(chatFilmWechatNeedsOut,
		chatFlagWechat, chatFlagOut, chatFlagReplay, chatFlagFilm, chatFlagOut, chatFlagWechat)
	for _, want := range []string{chatFlagWechat, chatFlagOut, chatFlagFilm} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the refusal never mentions %s: %s", want, rendered)
		}
	}
	// Every argument the parser supplies is spent: a leftover verb is a refusal
	// rendered with a hole in it. The count is derived from the constant rather
	// than asserted, so a verb added here without a matching argument fails too.
	if n := strings.Count(chatFilmWechatNeedsOut, "%s"); n != 6 {
		t.Errorf("the refusal carries %d verb(s), the parser supplies 6", n)
	}
	if strings.Contains(rendered, "%!") || strings.Contains(rendered, "%s") {
		t.Errorf("the rendered refusal still carries an unfilled verb: %s", rendered)
	}
}

// filmTestPatternPNG writes a w×h PNG whose every pixel is a function of its
// coordinates, so a crop that moved a pixel is visible as a VALUE, not only as a
// count.
func filmTestPatternPNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 7 % 256), G: uint8(y * 11 % 256), B: uint8((x + y) % 256), A: 255})
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

// TestChatFilmEvenFrameIsLossless is the FIX's own proof: an odd frame comes out
// even, and every pixel that survives is the SAME BYTE it was. A resampler would
// fail this on every pixel; a crop cannot fail it at all.
func TestChatFilmEvenFrameIsLossless(t *testing.T) {
	dir := t.TempDir()
	// 9×7 is the smallest odd/odd frame, which is the minimal sample for "both
	// dimensions are corrected at once" (Rule 10's minimal-sample habit).
	path := filmTestPatternPNG(t, dir, "odd.png", 9, 7)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	got, err := chatFilmEvenFrame(raw)
	if err != nil {
		t.Fatalf("even the frame: %v", err)
	}
	if !got.Cropped {
		t.Fatal("a 9x7 frame reported no correction")
	}
	if got.SrcW != 9 || got.SrcH != 7 || got.Width != 8 || got.Height != 6 {
		t.Fatalf("9x7 became %dx%d (source reported %dx%d), want 8x6", got.Width, got.Height, got.SrcW, got.SrcH)
	}
	if got.Width%chatFilmFrameParity != 0 || got.Height%chatFilmFrameParity != 0 {
		t.Fatalf("the written frame is %dx%d, want both dimensions even", got.Width, got.Height)
	}

	src, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode the fixture: %v", err)
	}
	dst, err := png.Decode(bytes.NewReader(got.Out))
	if err != nil {
		t.Fatalf("the written frame does not decode as a PNG: %v", err)
	}
	if dst.Bounds().Dx() != got.Width || dst.Bounds().Dy() != got.Height {
		t.Fatalf("the written PNG is %v but the reading claims %dx%d", dst.Bounds(), got.Width, got.Height)
	}
	for y := 0; y < got.Height; y++ {
		for x := 0; x < got.Width; x++ {
			sr, sg, sb, sa := src.At(x, y).RGBA()
			dr, dg, db, da := dst.At(x, y).RGBA()
			if sr != dr || sg != dg || sb != db || sa != da {
				t.Fatalf("pixel (%d,%d) changed: shot %v written %v — a crop must not touch a surviving pixel",
					x, y, src.At(x, y), dst.At(x, y))
			}
		}
	}
}

// TestChatFilmEvenFrameIsANoOpOnEvenFrames is the OTHER half of the acceptance:
// the frames the reference face already produces are written down UNCHANGED —
// the same bytes, not a re-encode that happens to look the same. It is what makes
// the no-flag product byte-identical to what it was before this fix.
func TestChatFilmEvenFrameIsANoOpOnEvenFrames(t *testing.T) {
	dir := t.TempDir()
	// 496×342 is the product's own real frame at the reference face.
	path := filmTestPatternPNG(t, dir, "even.png", 496, 342)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	got, err := chatFilmEvenFrame(raw)
	if err != nil {
		t.Fatalf("even the frame: %v", err)
	}
	if got.Cropped {
		t.Fatalf("an even frame was reported as corrected: %dx%d", got.SrcW, got.SrcH)
	}
	if !bytes.Equal(got.Out, raw) {
		t.Fatal("an even frame was re-encoded — the bytes written must be the bytes the display produced")
	}
	// The stronger reading, and the one that does not depend on how any Go
	// version's encoder happens to serialize a fixture: the SAME backing array is
	// handed back. A round-trip through image/png could in principle produce equal
	// bytes; it could never produce this identity.
	if len(got.Out) == 0 || len(raw) == 0 || &got.Out[0] != &raw[0] {
		t.Fatal("an even frame was copied rather than passed through — the writer must hand back the display's own bytes, not a reconstruction of them")
	}
}

// TestChatFilmEvenDim pins the arithmetic on its boundaries rather than only at a
// representative value: an already-even number is untouched, and the result is
// never negative.
func TestChatFilmEvenDim(t *testing.T) {
	cases := []struct{ in, want int }{{521, 520}, {342, 342}, {496, 496}, {9, 8}, {2, 2}, {1, 1}, {0, 0}}
	for _, c := range cases {
		if got := evenDim(c.in); got != c.want {
			t.Errorf("evenDim(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestChatFilmDefaultProductIsTheEncodersOwnOutput pins "without --wechat the
// product is unchanged" at the seam that decides it: the film arm's default
// composition is BYTE-IDENTICAL to what internal/gifgen emits for the film's own
// options. If this file's gate ever started rewriting frames — a resize, a second
// pass, a palette change — these two files would stop agreeing.
func TestChatFilmDefaultProductIsTheEncodersOwnOutput(t *testing.T) {
	dir := t.TempDir()
	a := filmTestPatternPNG(t, dir, "a.png", 8, 6)
	b := filmTestPatternPNG(t, dir, "b.png", 8, 6)
	frames := []string{a, b}

	armOut := filepath.Join(dir, "arm.gif")
	if _, err := chatFilmComposeGIF(armOut, frames, false); err != nil {
		t.Fatalf("compose through the arm: %v", err)
	}
	encOut := filepath.Join(dir, "enc.gif")
	seq := []gifgen.Frame{{Path: a}, {Path: b}}
	if _, err := gifgen.Encode(encOut, gifgen.Options{DelayCS: chatFilmGIFDelayCS, Frames: seq}); err != nil {
		t.Fatalf("encode through the encoder: %v", err)
	}
	armBytes, err := os.ReadFile(armOut)
	if err != nil {
		t.Fatalf("read the arm's file: %v", err)
	}
	encBytes, err := os.ReadFile(encOut)
	if err != nil {
		t.Fatalf("read the encoder's file: %v", err)
	}
	if !bytes.Equal(armBytes, encBytes) {
		t.Fatalf("the film arm's default product is %d byte(s), the encoder's own output %d — the arm is no longer a gate over the encoder but a second encoder",
			len(armBytes), len(encBytes))
	}
}

// TestChatFilmWechatProductIsPlayable is the GREEN end-to-end proof of the four
// invariants at the composer's own seam, read off the WRITTEN FILE: both
// dimensions even and inside the width limit, the Netscape block present in the
// bytes, the loop count 0, and the size inside the ceiling.
func TestChatFilmWechatProductIsPlayable(t *testing.T) {
	dir := t.TempDir()
	a := filmTestPNG(t, dir, "a.png", 8, 6, color.RGBA{R: 0x13, G: 0x19, B: 0x26, A: 255})
	b := filmTestPNG(t, dir, "b.png", 8, 6, color.RGBA{R: 0xFF, G: 0xD7, B: 0x00, A: 255})
	out := filepath.Join(dir, "wechat.gif")

	res, err := chatFilmComposeGIF(out, []string{a, b}, true)
	if err != nil {
		t.Fatalf("compose the phone-shaped product: %v", err)
	}
	if res.Width > playableGIFMaxWidth {
		t.Errorf("the written animation is %d px wide, over the %d px limit", res.Width, playableGIFMaxWidth)
	}
	if res.Width%chatFilmFrameParity != 0 || res.Height%chatFilmFrameParity != 0 {
		t.Errorf("the written animation is %dx%d, want both dimensions even", res.Width, res.Height)
	}
	if res.Bytes > wechatGIFMaxBytes {
		t.Errorf("the written animation is %d byte(s), over the %d ceiling", res.Bytes, wechatGIFMaxBytes)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the written GIF: %v", err)
	}
	if !bytes.Contains(raw, []byte("NETSCAPE2.0")) {
		t.Error("the written bytes carry no NETSCAPE2.0 block")
	}
	g, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the written file does not decode as a GIF: %v", err)
	}
	if g.Config.Width%chatFilmFrameParity != 0 || g.Config.Height%chatFilmFrameParity != 0 {
		t.Errorf("the decoder reads %dx%d, want both dimensions even", g.Config.Width, g.Config.Height)
	}
	if g.LoopCount != gifgen.LoopForever {
		t.Errorf("the file's loop count reads back %d, want %d (forever)", g.LoopCount, gifgen.LoopForever)
	}
}
