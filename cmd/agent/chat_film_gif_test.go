package main

// chat_film_gif_test.go — the animation arm's own units: the `--out` flag's two
// spellings and its refusals, the caption a frame carries, the 2-frame → 2-frame
// composition (Rule 10's minimal sample: the smallest input that is an
// ANIMATION, not a still), and B3's refusal of a dump word on a film run.
//
// WHAT IS NOT HERE. The end-to-end path (`gopi chat --replay <session> --film
// DIR --out FILE.gif`) needs a display, a terminal and a recording; it is proved
// by RUNNING it (Rule 31), and the receipt is the run's report. What is pinned
// here is everything that must not depend on a machine having a display.

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/gifgen"
	"github.com/nickhuang99/gopi/internal/lineedit"
)

// filmTestPNG writes a w×h PNG of one colour and returns its path — the same
// shape internal/gifgen's own suite uses, kept local because a shared helper
// across packages would be a new exported surface for a test fixture.
func filmTestPNG(t *testing.T, dir, name string, w, h int, c color.RGBA) string {
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

// TestChatParseOutArg pins both spellings and every refusal: the value may be in
// the next slot or after `=`, and a GLF with no file must be refused with the
// spelling the caller can correct.
func TestChatParseOutArg(t *testing.T) {
	cases := []struct {
		name     string
		arg      string
		next     string
		hasNext  bool
		wantPath string
		wantHit  bool
		wantFail bool
	}{
		{name: "space form", arg: chatFlagOut, next: "/tmp/replay.gif", hasNext: true, wantPath: "/tmp/replay.gif", wantHit: true},
		{name: "equals form", arg: chatFlagOut + "=/tmp/replay.gif", wantPath: "/tmp/replay.gif", wantHit: true},
		{name: "absent", arg: chatFlagFilm, next: "x", hasNext: true},
		{name: "no value at all", arg: chatFlagOut, hasNext: false, wantHit: true, wantFail: true},
		{name: "no value before another flag", arg: chatFlagOut, next: chatFlagFilm, hasNext: true, wantHit: true, wantFail: true},
		{name: "empty equals value", arg: chatFlagOut + "=", wantHit: true, wantFail: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, hit, err := chatParseOutArg(c.arg, c.next, c.hasNext)
			if hit != c.wantHit {
				t.Fatalf("consumed = %v, want %v", hit, c.wantHit)
			}
			if c.wantFail {
				if err == nil {
					t.Fatal("a malformed --out was accepted")
				}
				if !strings.Contains(err.Error(), chatFlagOut) {
					t.Fatalf("the refusal does not name the flag: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if path != c.wantPath {
				t.Fatalf("path = %q, want %q", path, c.wantPath)
			}
		})
	}
}

// TestChatFilmComposeGIFMinimalSample is the smallest sample that is an
// ANIMATION rather than a still: TWO frames in, TWO frames out, read back off the
// written stream (Rule 10's minimal-sample discipline, and the same read-back
// measureWritten performs: the claim is about the FILE, never about the intent).
func TestChatFilmComposeGIFMinimalSample(t *testing.T) {
	dir := t.TempDir()
	a := filmTestPNG(t, dir, "a.png", 24, 12, color.RGBA{R: 200, G: 30, B: 30, A: 255})
	b := filmTestPNG(t, dir, "b.png", 24, 12, color.RGBA{R: 30, G: 200, B: 30, A: 255})
	out := filepath.Join(dir, "film.gif")

	res, err := chatFilmComposeGIF(out, []string{a, b}, false)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if res.Frames != 2 {
		t.Fatalf("the encoder reports %d frame(s), want the 2 that went in", res.Frames)
	}
	if res.DelayCS != chatFilmGIFDelayCS {
		t.Fatalf("delay = %dcs, want the film's own %dcs (the pace is one definition, in internal/gifgen)", res.DelayCS, chatFilmGIFDelayCS)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the written GIF: %v", err)
	}
	g, err := gif.DecodeAll(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("the written file does not decode as a GIF: %v", err)
	}
	if len(g.Image) != 2 {
		t.Fatalf("the FILE holds %d frame(s), want 2 — a one-frame GIF is not a replay", len(g.Image))
	}
	for i, d := range g.Delay {
		if d != chatFilmGIFDelayCS {
			t.Errorf("frame %d shows for %dcs, want %dcs", i, d, chatFilmGIFDelayCS)
		}
	}
	// The two frames must actually DIFFER: an "animation" of two identical
	// pictures would satisfy a frame count and be invisible, which is the failure
	// mode a count alone cannot see.
	if string(g.Image[0].Pix) == string(g.Image[1].Pix) {
		t.Error("the two frames are pixel-identical — a frame count of 2 is not motion")
	}
}

// TestChatFilmGIFPixelsAreThePhotographs is the CAPTION-BAND regression, and it
// is a PIXEL claim rather than a comment (Rule 10.2): the animation's canvas is
// the frame's own geometry, and every frame's pixels are the photographed PNG's
// pixels — nothing appended, nothing overlaid, nothing re-drawn.
//
// WHY IT IS THE TEST THAT MATTERS. The defect this replaces was not a crash: the
// band was ADDITIVE, so the film "worked" while the GIF grew a white strip of
// BLACK text2img text under every dark frame, and the operator's verdict was that
// the result did not look like the product. A count of frames cannot see that, and
// neither can a size check — so this asserts the exact geometry AND pixel identity
// against the inputs, which is the property the acceptance names ("the frame's
// pixels come from the real surface").
func TestChatFilmGIFPixelsAreThePhotographs(t *testing.T) {
	dir := t.TempDir()
	// The two inputs are deliberately NOT the film's dark ground: the claim is
	// "the encoder adds nothing", and a test whose inputs are already the expected
	// ground could pass while a band merely happened to match it.
	dark := color.RGBA{R: 0x13, G: 0x19, B: 0x26, A: 255}
	gold := color.RGBA{R: 0xFF, G: 0xD7, B: 0x00, A: 255}
	a := filmTestPNG(t, dir, "a.png", 40, 20, dark)
	b := filmTestPNG(t, dir, "b.png", 40, 20, dark)
	// One gold row, so the frame is not uniform and a re-draw cannot coincide.
	imgB := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			c := dark
			if y == 3 {
				c = gold
			}
			imgB.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(b)
	if err != nil {
		t.Fatalf("create %s: %v", b, err)
	}
	if err := png.Encode(f, imgB); err != nil {
		t.Fatalf("encode %s: %v", b, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", b, err)
	}

	out := filepath.Join(dir, "film.gif")
	res, err := chatFilmComposeGIF(out, []string{a, b}, false)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	// GEOMETRY: the canvas is the frame, with no band below it.
	if res.Width != 40 || res.Height != 20 {
		t.Fatalf("the GIF canvas is %dx%d, want the photographed 40x20 — a canvas that differs from the frames is the caption band's signature (this film must have none)", res.Width, res.Height)
	}
	if res.CaptionRows != 0 {
		t.Fatalf("the encoder reports %d caption row(s), want 0 — the film draws no caption into its pixels", res.CaptionRows)
	}
	if res.Captioned != 0 {
		t.Fatalf("%d frame(s) were captioned, want 0", res.Captioned)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read %s: %v", out, err)
	}
	g, err := gif.DecodeAll(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	// PIXELS: two colours in, the same two colours out, at the same coordinates.
	// An exact-palette encode of a two-colour frame is lossless, so identity is
	// the right assertion rather than a tolerance.
	for i, src := range []string{a, b} {
		want := mustDecodePNG(t, src)
		got := g.Image[i]
		if got.Bounds() != want.Bounds() {
			t.Fatalf("frame %d is %v, want the photograph's own %v", i, got.Bounds(), want.Bounds())
		}
		for y := got.Bounds().Min.Y; y < got.Bounds().Max.Y; y++ {
			for x := got.Bounds().Min.X; x < got.Bounds().Max.X; x++ {
				gr, gg, gb, _ := got.At(x, y).RGBA()
				wr, wg, wb, _ := want.At(x, y).RGBA()
				if gr != wr || gg != wg || gb != wb {
					t.Fatalf("frame %d pixel (%d,%d) is %04x%04x%04x but the photographed PNG has %04x%04x%04x — the encoder changed a pixel of the surface it was handed",
						i, x, y, gr, gg, gb, wr, wg, wb)
				}
			}
		}
	}
	// The dark ground share is the CRITERION the acceptance names, asserted here
	// on the film's own output: a white picture would measure the opposite.
	share := darkShare(t, g.Image[0])
	if share < 0.5 {
		t.Fatalf("only %.1f%% of frame 0 is the photographed dark ground — this frame is not a picture of the surface", share*100)
	}
}

// mustDecodePNG decodes a PNG or fails the test: a fixture that cannot be read is
// a broken test, never a skipped assertion.
// groundRGB parses internal/lineedit's declared ground into its three channels —
// READ from the constant rather than re-typed, so the test cannot pass against a
// ground the product no longer draws (Rules 13/14: the value has ONE definition
// and this is a reader of it, not a second copy).
func groundRGB(t *testing.T) (r, g, b uint8) {
	t.Helper()
	hex := strings.TrimPrefix(lineedit.GroundBackgroundHex, "#")
	if len(hex) != 6 {
		t.Fatalf("the declared ground %q is not a 6-digit hex colour", lineedit.GroundBackgroundHex)
	}
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		t.Fatalf("the declared ground %q does not parse: %v", lineedit.GroundBackgroundHex, err)
	}
	return uint8(n >> 16), uint8(n >> 8), uint8(n)
}

func mustDecodePNG(t *testing.T, path string) image.Image {
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
	return img
}

// darkShare is the fraction of an image's pixels that are the film's declared
// ground (internal/lineedit's GroundBackgroundHex): the ONE measurement the
// acceptance's "dark UI, not white" is made of, in one place so the test and any
// runtime evidence use the same arithmetic rather than two approximations.
func darkShare(t *testing.T, img image.Image) float64 {
	t.Helper()
	gr, gg, gb := groundRGB(t)
	b := img.Bounds()
	total := 0
	hit := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			total++
			if uint8(r>>8) == gr && uint8(g>>8) == gg && uint8(bl>>8) == gb {
				hit++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(hit) / float64(total)
}

// TestChatFilmDumpRefusalIsB3sFix pins B3 in both directions: a dump word on a
// FILM command line is REFUSED by name (with the environment named as the route
// that works), and the environment itself is NOT covered by that refusal —
// because the filmed producer inherits it and honours it.
func TestChatFilmDumpRefusalIsB3sFix(t *testing.T) {
	// Neither word given: nothing to refuse. This is the DEFAULT and the env
	// path, and refusing them would break the one route that works.
	if err := chatFilmDumpRefusal(false, chatDumpOptions{requested: true, stated: false}); err != nil {
		t.Fatalf("the default (no dump word on this command line) was refused: %v", err)
	}
	// The ARM flag: refused, and the refusal names the flag AND the env.
	err := chatFilmDumpRefusal(true, chatDumpOptions{requested: true, stated: true})
	if err == nil {
		t.Fatal("--dump-screen was silently accepted on a film run — this is B3")
	}
	for _, want := range []string{chatDumpFlag, chatDumpEnv} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
	// --dump-screen=PATH is the same word with a value: still refused.
	if err := chatFilmDumpRefusal(true, chatDumpOptions{path: "/tmp/x.dump", requested: true, stated: true}); err == nil {
		t.Error("--dump-screen=PATH was accepted on a film run")
	}
	// The OFF switch: refused too, and for the reason that makes it different —
	// the decline cannot reach the process that would stop dumping.
	offErr := chatFilmDumpRefusal(true, chatDumpOptions{disabled: true, stated: true})
	if offErr == nil {
		t.Fatal("--no-dump-screen was silently accepted on a film run")
	}
	if !strings.Contains(offErr.Error(), chatNoDumpFlag) || !strings.Contains(offErr.Error(), chatDumpEnv) {
		t.Errorf("the off switch's refusal does not name the switch and the working route: %v", offErr)
	}
	if offErr.Error() == err.Error() {
		t.Error("the two refusals are the same sentence — a decline and a request are opposite intents and need opposite explanations")
	}
}

// TestChatFlagOutIsInTheVerbsOwnVocabulary: the flag the parser READS must be in
// the list the usage page and every unknown-flag refusal print, in both the long
// usage page and the one-line synopsis — otherwise the page describes a command
// line the verb refuses (the drift this list exists to catch, measured while
// adding --film).
func TestChatFlagOutIsInTheVerbsOwnVocabulary(t *testing.T) {
	found := false
	for _, f := range chatREPLFlags() {
		if f == chatFlagOut {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("%s is read by the verb's parser but is NOT in chatREPLFlags()", chatFlagOut)
	}
	valued := false
	for _, f := range chatResumeValuedFlags() {
		if f == chatFlagOut {
			valued = true
			break
		}
	}
	if !valued {
		t.Errorf("%s takes a value in its next slot but is not in chatResumeValuedFlags() — the vocabulary walk would hand it a bare slot", chatFlagOut)
	}
	for _, page := range []string{chatUsage, chatUsageLine} {
		if !strings.Contains(page, chatFlagOut) {
			t.Errorf("%s is missing from a page the caller reads:\n%s", chatFlagOut, page)
		}
	}
}

// TestChatFilmUsageStatesThePaceItWritesAt: the help states the animation's pace,
// and the number it prints is the SAME one the encoder is called with — never a
// hand-written copy that can drift from chatFilmGIFDelayCS.
func TestChatFilmUsageStatesThePaceItWritesAt(t *testing.T) {
	// The film's block renders on the SCOPED page (`gopi chat --replay -h`, the
	// family's own page — chat_replay_help_scope.go), and the main page carries the
	// index that names every one of its words. Both halves are asserted because
	// either one alone would let the block drop off the surface a reader has.
	if !strings.Contains(chatReplayHelp, chatFilmUsage) {
		t.Fatal("chatFilmUsage is not joined into chatReplayHelp — the film's own arm would be invisible to `gopi chat --replay -h`")
	}
	if !strings.Contains(chatUsage, chatFlagFilm) || !strings.Contains(chatUsage, chatReplayHelpPointsHere) {
		t.Errorf("the session page neither names %s nor points at %s — the film becomes unreachable from `gopi chat -h`",
			chatFlagFilm, chatReplayHelpPointsHere)
	}
	want := strconv.Itoa(chatFilmGIFDelayCS)
	for _, line := range strings.Split(chatFilmUsage, "\n") {
		if strings.Contains(line, "cs apart") && !strings.Contains(line, want) {
			t.Errorf("the usage line states a pace that is not the encoder's own %s: %q", want, line)
		}
	}
	if !strings.Contains(chatFilmUsage, chatFlagOut) {
		t.Errorf("chatFilmUsage does not document %s", chatFlagOut)
	}
}

// --- the DELIVERY contract: an animation a phone will actually play ----------
//
// Measured 2026-10-04: the film's default canvas (the surface's ideal 117x26 at
// xterm's 6x13 face = 706 px, 1186 px with a wider recording) is REFUSED by
// WeChat's conversation view, which accepts an animation only up to 500 px wide
// and wants an even width plus a Netscape loop block. The refusals themselves are
// the contract, so they get a red proof of their own and a green proof that the
// film's real product passes all three.

// TestChatFilmPlayableNarrowedOnlyAboveTheLimit pins the CAP: a canvas inside the
// limit is untouched, a wider one is narrowed to filmPlayableMaxCols with the
// size change SAID OUT LOUD (Rule 15), and the pixel width of the narrowed canvas
// is derived (filmFrameWidthPx) rather than asserted against a second literal.
func TestChatFilmPlayableNarrowedOnlyAboveTheLimit(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "")
	// THE FACE IS AN ARGUMENT NOW (01a10603): this unit states the canvas it
	// narrows to on the REFERENCE face, which is the face the pre-face contract
	// implied. The boundary the FACE itself moves is proved in
	// chat_film_face_test.go, on faces this host actually measured.
	max := filmReferenceMaxCols()
	// The narrowing must actually be a narrowing for the fixtures below to mean
	// anything, and the derived width must clear the limit it exists for.
	if max <= 0 || max >= chatBeatIdealCols() {
		t.Fatalf("filmReferenceMaxCols() = %d, want a positive count below the surface's ideal %d columns", max, chatBeatIdealCols())
	}
	if w := filmFrameWidthPx(max); w > playableGIFMaxWidth || w%playableGIFParity != 0 {
		t.Fatalf("the largest playable canvas is %d px, want an even width at or below %d", w, playableGIFMaxWidth)
	}

	var quiet bytes.Buffer
	if cols, rows := chatFilmPlayableGeometry(filmReferenceFace(), max, 26, &quiet); cols != max || rows != 26 {
		t.Fatalf("a canvas already inside the limit was changed to %dx%d, want %dx26", cols, rows, max)
	}
	if quiet.Len() != 0 {
		t.Errorf("a canvas inside the limit was reported as narrowed: %s", quiet.String())
	}

	var loud bytes.Buffer
	cols, rows := chatFilmPlayableGeometry(filmReferenceFace(), chatBeatIdealCols(), chatBeatIdealRows(), &loud)
	if cols != max || rows != chatBeatIdealRows() {
		t.Fatalf("the ideal canvas was shot at %dx%d, want the capped %dx%d", cols, rows, max, chatBeatIdealRows())
	}
	notice := loud.String()
	for _, want := range []string{
		strconv.Itoa(chatBeatIdealCols()),
		strconv.Itoa(filmFrameWidthPx(chatBeatIdealCols())),
		strconv.Itoa(playableGIFMaxWidth),
		strconv.Itoa(max),
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("the narrowing notice never mentions %s — the size change is not attributable:\n%s", want, notice)
		}
	}
}

// TestChatFilmRequirePlayableRefusesOverWide is the RED proof: an encoder result
// above the limit is refused BY NAME with the numbers, and the refusal carries
// the flag that fixes it. A silent resample here would deliver a picture nobody
// measured.
func TestChatFilmRequirePlayableRefusesOverWide(t *testing.T) {
	wide := &gifgen.Result{Out: "film.gif", Width: playableGIFMaxWidth + 1, Height: 342, LoopCount: gifgen.LoopForever, LoopReadBack: gifgen.LoopForever}
	err := chatFilmRequirePlayable(wide, false)
	if err == nil {
		t.Fatalf("a %d px wide animation was accepted, want a refusal at the %d px limit", wide.Width, playableGIFMaxWidth)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(playableGIFMaxWidth)) || !strings.Contains(err.Error(), chatFlagCols) {
		t.Errorf("the refusal does not name the limit and the flag that fixes it: %v", err)
	}

	odd := &gifgen.Result{Out: "film.gif", Width: playableGIFMaxWidth - 1, Height: 342, LoopCount: gifgen.LoopForever, LoopReadBack: gifgen.LoopForever}
	if err := chatFilmRequirePlayable(odd, false); err == nil {
		t.Fatalf("a %d px wide (odd) animation was accepted", odd.Width)
	} else if !strings.Contains(err.Error(), "ODD") {
		t.Errorf("the parity refusal does not say what is wrong: %v", err)
	}

	noLoop := &gifgen.Result{Out: "film.gif", Width: playableGIFMaxWidth - 2, Height: 342, LoopCount: gifgen.LoopForever, LoopReadBack: -1}
	if err := chatFilmRequirePlayable(noLoop, false); err == nil {
		t.Fatal("an animation whose FILE carries no loop block was accepted — it plays once and stops")
	} else if !strings.Contains(err.Error(), "Netscape") {
		t.Errorf("the loop refusal does not name the missing block: %v", err)
	}
}

// TestChatFilmComposeGIFIsPlayable is the GREEN proof of the same three
// properties, read off the WRITTEN FILE rather than off the request — the arm's
// real product, held to the delivery contract end to end.
func TestChatFilmComposeGIFIsPlayable(t *testing.T) {
	dir := t.TempDir()
	a := filmTestPNG(t, dir, "a.png", 8, 6, color.RGBA{R: 0x13, G: 0x19, B: 0x26, A: 255})
	b := filmTestPNG(t, dir, "b.png", 8, 6, color.RGBA{R: 0xFF, G: 0xD7, B: 0x00, A: 255})
	out := filepath.Join(dir, "film.gif")

	res, err := chatFilmComposeGIF(out, []string{a, b}, false)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if res.Width > playableGIFMaxWidth || res.Width%playableGIFParity != 0 {
		t.Fatalf("the written animation is %d px wide, want an even width at or below %d", res.Width, playableGIFMaxWidth)
	}
	// The FILE, not the Result: decode the bytes and read the loop count the
	// stream itself states (Go writes the Netscape block only for a multi-frame
	// stream, so a multi-frame film is the sample that can carry one at all).
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the written GIF: %v", err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the written file does not decode as a GIF: %v", err)
	}
	if len(g.Image) < 2 {
		t.Fatalf("the file holds %d frame(s), want the 2 that went in", len(g.Image))
	}
	if g.LoopCount != gifgen.LoopForever {
		t.Errorf("the file's loop count reads back %d, want %d (forever) — a phone shows such a file once and stops", g.LoopCount, gifgen.LoopForever)
	}
	// The Netscape block is what CARRIES that count, so its bytes are read
	// directly: "the decoder reported 0" and "the bytes hold the extension" are
	// different facts, and only the second is what WeChat reads.
	if !bytes.Contains(raw, []byte("NETSCAPE2.0")) {
		t.Error("the written bytes carry no NETSCAPE2.0 application block — the loop count has nothing to ride in")
	}
}

// --- the DIMENSION parity: BOTH axes, and the failing one is NAMED -----------
//
// The requirement is ONE (playableGIFParity) and it is checked on both axes of
// the DELIVERED FILE. An odd width was refused from the start; the height leg
// landed later, and this pair is its guard: the value handed in is the only
// variable, so a gate that silently dropped the height check — or one that
// reported the wrong axis — fails here.

// TestChatFilmRequirePlayableRefusesOddHeight is the RED proof of the height
// half. The frame is 500x521 — even width, odd height, the measured size of the
// film's own ideal canvas on a face whose border is odd — and it must be refused
// BY NAME, naming the HEIGHT, so a reader is not sent to look at the width.
//
// The operand is deliberately the DEFAULT product (wechat=false): an odd height
// is a property of the file, not a destination's preference, so a film that no
// encoder subsampling chroma can open must not be handed over just because the
// phone was not named.
func TestChatFilmRequirePlayableRefusesOddHeight(t *testing.T) {
	oddHeight := &gifgen.Result{
		Out: "film.gif", Width: playableGIFMaxWidth,
		Height:    521, // the measured height of the film's ideal canvas on an odd-border face
		LoopCount: gifgen.LoopForever, LoopReadBack: gifgen.LoopForever,
	}
	err := chatFilmRequirePlayable(oddHeight, false)
	if err == nil {
		t.Fatalf("a %dx%d animation was accepted, want a refusal on the odd height", oddHeight.Width, oddHeight.Height)
	}
	msg := err.Error()
	if !strings.Contains(msg, filmAxisHeight) {
		t.Errorf("the refusal does not name the failing axis (%q): %v", filmAxisHeight, err)
	}
	if strings.Contains(msg, filmAxisWidth) {
		t.Errorf("the height refusal names the width — the reader is sent to the wrong axis: %v", err)
	}
	if !strings.Contains(msg, strconv.Itoa(oddHeight.Height)) {
		t.Errorf("the refusal does not state the offending value %d: %v", oddHeight.Height, err)
	}
	if !strings.Contains(msg, strconv.Itoa(playableGIFParity)) {
		t.Errorf("the refusal does not state the alignment the value must have: %v", err)
	}
	// The named product refuses it too — the leg is not flag-dependent.
	if err := chatFilmRequirePlayable(oddHeight, true); err == nil {
		t.Error("--wechat accepted an odd height")
	}

	// The pair is SYMMETRIC: an odd width is still refused, and names the width.
	oddWidth := &gifgen.Result{
		Out: "film.gif", Width: playableGIFMaxWidth - 1, Height: 342,
		LoopCount: gifgen.LoopForever, LoopReadBack: gifgen.LoopForever,
	}
	err = chatFilmRequirePlayable(oddWidth, false)
	if err == nil {
		t.Fatalf("a %dx%d animation was accepted", oddWidth.Width, oddWidth.Height)
	}
	if !strings.Contains(err.Error(), filmAxisWidth) {
		t.Errorf("the width refusal does not name the failing axis (%q): %v", filmAxisWidth, err)
	}
	if strings.Contains(err.Error(), filmAxisHeight) {
		t.Errorf("the width refusal names the height: %v", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(oddWidth.Width)) {
		t.Errorf("the width refusal does not state the offending value %d: %v", oddWidth.Width, err)
	}

	// And an animation aligned on BOTH axes with a loop block is ACCEPTED — a
	// gate that refuses everything would satisfy each assertion above.
	good := &gifgen.Result{
		Out: "film.gif", Width: playableGIFMaxWidth - playableGIFParity, Height: 342,
		LoopCount: gifgen.LoopForever, LoopReadBack: gifgen.LoopForever,
	}
	if err := chatFilmRequirePlayable(good, false); err != nil {
		t.Errorf("an animation aligned on both axes was refused: %v", err)
	}
}

// TestChatFilmParityIsOneDefinition pins Rules 13/14 at the seam this change
// touches: the alignment the frame WRITER targets and the alignment the delivery
// GATE checks are the same number, not two literals that happen to agree today.
// A second `= 2` anywhere in this package would let the crop and the gate drift,
// which is exactly the defect class this task exists to close.
func TestChatFilmParityIsOneDefinition(t *testing.T) {
	if chatFilmFrameParity != playableGIFParity {
		t.Fatalf("the frame writer targets %d and the delivery gate %d — the picture written and the picture accepted have drifted apart",
			chatFilmFrameParity, playableGIFParity)
	}
	// The crop is what makes the gate's height leg a no-op on this arm's own
	// product, so the two must agree on the VALUE, not merely both be even.
	if playableGIFParity <= 0 {
		t.Fatalf("playableGIFParity = %d, want a positive alignment", playableGIFParity)
	}
}
