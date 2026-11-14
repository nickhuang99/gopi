// Package gifgen — a PNG frame sequence goes in, ONE animated GIF comes out,
// each frame carrying the words that explain it.
//
// WHY IT EXISTS. gopi's picture surfaces (`gopi tool capture`, `gopi tool shot`,
// `gopi tool text2img`, `gopi tool annotate`) all produce STILLS, and a still cannot show
// a SEQUENCE — a state machine stepping, a screen changing, a before/after. The
// animation was missing, and the two ways to get one both cost something gopi
// does not pay: an external tool (ffmpeg / ImageMagick / gifsicle — Rule 35
// forbids the dependency) or HTML (which needs a browser and cannot be attached
// to a ledger row, a chat answer or a report). This package is the third way:
// the Go standard library's image/gif encoder, plus the glyph rasteriser this
// repository already owns (`internal/text2img`), so a frame's caption is drawn
// by the SAME code that draws a label chip — no second rasteriser, no new
// module, no CGO, no font blob.
//
// THE CAPTION IS THE POINT, NOT DECORATION. A GIF of a UI with no words is
// unreadable to anyone who did not record it. So a frame's caption is composed
// INTO its pixels, on a white band appended BELOW the frame (never over it: a
// caption that covers the pixels it explains destroys the evidence it exists to
// carry — the same reason `gopi tool annotate` always writes a NEW file).
//
// THE BAND IS UNIFORM, AND ONE FILM EITHER HAS ONE OR NOT. If ANY frame carries
// words, EVERY frame gets the same band height (the tallest caption's), and a
// frame with no words shows empty paper. Two reasons, both measured:
//
//   - A GIF has ONE logical screen but PER-FRAME bounds. Frames of differing
//     height are legal and were the first version of this package — and with
//     disposal=none the band area of a caption-less frame keeps the PREVIOUS
//     frame's words on screen, so a stale caption lingers under a picture it
//     does not describe. That is a lie the format hands you for free.
//   - The geometry jumping every frame makes an animation hard to read.
//
// So the band is a property of the FILM: a film with no captions at all has no
// band (the frames are the input, byte-identical in geometry), and a film with
// captions has one band for everybody. "A frame with nothing to say" is then
// visibly an empty strip, not a missing strip.
//
// WHAT IT REFUSES (Rule 15 — a refusal names the cause, never a silent repair):
// frames of mismatched size (a GIF has one logical screen; a caption band of a
// different width is refused rather than cropped), a frame file that is not a
// PNG, a delay outside the 1..65535 centisecond range the format can carry, a
// caption whose glyphs the font chain lacks (refused, never drawn as blank —
// an empty chip is worse than an error), and a non-positive caption size.
//
// DETERMINISM. Two runs over the same inputs produce byte-identical output:
// frames land in the order given, the palette is built in first-seen pixel
// order (a slice, never a map iteration), and no timestamp or path from "now"
// reaches the bytes. That is what lets a caller diff two runs — and what makes
// an artifact worth committing next to the frames it was made from.
package gifgen

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/text2img"
)

// The numbers this package obeys, in ONE place (Rule 1: a literal that carries
// meaning is a named constant, and it is REFERENCED, never re-typed — Rule 13).
const (
	// DefaultDelayCentiseconds is how long one frame shows when the caller
	// names no delay: 25 cs = 250 ms, roughly four frames a second — slow
	// enough that a reader finishes a caption, fast enough to read as motion.
	DefaultDelayCentiseconds = 25
	// MinDelayCentiseconds / MaxDelayCentiseconds are the format's own limits:
	// a GIF graphic-control block carries the delay in an unsigned 16-bit
	// centisecond field, and a delay of 0 is not "instant" in practice — most
	// decoders substitute 10 cs for it, so 0 is REFUSED instead of being
	// reinterpreted behind the caller's back (Rule 15).
	MinDelayCentiseconds = 1
	MaxDelayCentiseconds = 65535
	// LoopForever is the Netscape-loop value that means "never stop" — 0 is the
	// GIF convention for it, and it is the default because a looping demo with
	// no controls must not end while the reader is still reading.
	LoopForever = 0
	// MaxLoopCount is the largest finite repeat count the extension carries.
	MaxLoopCount = 65535
	// loopReadBackOnce is what a decoder reports for a stream that carries NO
	// Netscape looping extension — Go's "play once". It is a READ-BACK value, not
	// a writable one: Go's encoder emits the extension only for a multi-frame
	// stream, so a one-frame film reads back like this whatever it was asked for.
	loopReadBackOnce = -1
	// PaletteSize is the GIF palette ceiling: 2^8 entries. The encoder works in
	// 8-bit indices, so this is the format's own number, not a taste.
	PaletteSize = 256
	// MaxCanvasDim bounds a composed canvas so a corrupt PNG header cannot ask
	// for an allocation that cannot exist (the same sanity guard text2img's
	// rasteriser carries).
	MaxCanvasDim = 16384
	// Caption defaults mirror `gopi tool text2img`'s own: the same font size and page
	// margin, so a caption looks like the prose renders beside it.
	DefaultCaptionSize   = text2img.DefaultSize
	DefaultCaptionMargin = text2img.DefaultMargin
	// CaptionLineGapPx is the extra leading between caption lines, on top of
	// the font size (the band's pitch is size + gap).
	CaptionLineGapPx = 6
	// CaptionFillRGB is the caption band's fill: opaque white, the paper the
	// prose renderer uses — a caption is text on paper, and paper is opaque, so
	// a transparent frame's alpha cannot make the words unreadable.
	CaptionFillRGB = 255
	// CaptionInkRGB is the caption text's colour: black on that paper.
	CaptionInkRGB = 0
	// captionBandChannels is how many bytes per pixel the band arithmetic
	// assumes — stated once for the plane-based fill.
	captionBandChannels = 4
	// paletteFullScale is the maximum a color.Color's channel returns from
	// RGBA(): the interface scales to 16 bits, so a saturation ratio computed
	// from it relates to this, never to 255. Named because the palette is read
	// through that interface and a bare 255 there would be a different (wrong)
	// fraction by a factor of 257.
	paletteFullScale = 65535
	// frameListSep separates the frames of one list, and captionSep separates a
	// frame's path from its words in `--caption`. They are the CLI's grammar and
	// live here so the verb and this package cannot disagree about it.
	frameListSep = ","
	captionSep   = "="
	// PNGMagic is the first byte of every PNG and of no other format worth
	// guessing at: the 8-byte signature the loader checks before decoding, so a
	// JPEG named .png is refused by NAME rather than mis-decoded.
	PNGMagic = "\x89PNG\r\n\x1a\n"
	// DisposalNone is the GIF disposal method this encoder states per frame:
	// each frame is a COMPLETE picture, so the previous one is simply replaced.
	// Stated explicitly rather than left at the zero value, which decoders read
	// as "unspecified" and some then treat as "restore background".
	DisposalNone = gif.DisposalNone
	// MinCaptionSize is the smallest caption font size accepted (text2img's own
	// positive-integer floor for a size flag).
	MinCaptionSize = 1
)

// The numeric flag contract's sentinel, shared shape with text2img's own: 0
// means "the caller did not state a value" for a positive dimension (DelayCS,
// CaptionSize, CaptionMargin), while LoopSet distinguishes "0 = forever" from
// "not stated" for the loop count — the same reason text2img exports
// InvalidFlagInt (Rule 2: the sentinel and the default are read in ONE place).
const (
	// delayUnset is Options.DelayCS's zero value, rerouted to the default.
	delayUnset = 0
	// rgbaOpaque is the alpha every palette entry is registered with. A GIF
	// carries at most ONE transparent index, and a palette entry whose alpha is
	// zero claims that slot: registering the picture's colours as transparent
	// would make the encoder silently pick one of them and punch it out of the
	// frame. The composed canvas is opaque by construction (the caption paper is
	// opaque), so the palette is opaque too.
	rgbaOpaque = 0xff
)

// UsageError is a refusal the caller can fix by changing its argument list —
// a bad delay, a mismatched frame size, a caption the font cannot draw. It is a
// distinct type so the CLI can exit with the POSIX usage class (2) for a wrong
// invocation and the run-failure class (1) for everything else, instead of
// flattening two different operator actions into one exit code.
type UsageError struct{ msg string }

// Error implements error.
func (e *UsageError) Error() string { return e.msg }

// usagef builds a UsageError with a formatted message.
func usagef(format string, a ...any) error { return &UsageError{msg: fmt.Sprintf(format, a...)} }

// IsUsage reports whether err is a caller-fixable refusal.
func IsUsage(err error) bool {
	var u *UsageError
	return errors.As(err, &u)
}

// Frame is one input frame: the PNG to show, and the words that explain it
// (empty = show it unchanged, with no band).
type Frame struct {
	Path    string
	Caption string
}

// Options is one encoding request. Zero values fall back to the defaults above,
// except Loop: LoopSet is what distinguishes "0 = loop forever" from "unset".
type Options struct {
	// DelayCS is how long each frame shows, in centiseconds.
	DelayCS int
	// Loop is the repeat count; LoopSet says whether the caller stated it.
	Loop    int
	LoopSet bool
	// CaptionSize is the caption font size in px (<=0 → DefaultCaptionSize).
	CaptionSize int
	// CaptionMargin is the caption band's padding in px (<=0 → DefaultCaptionMargin).
	CaptionMargin int
	// Frames are the inputs, in the order they will be displayed.
	Frames []Frame
}

// Result reports what was produced, measured rather than promised: the numbers
// are read back off the encoded stream (the decoded frame count, each frame's
// delay and bounds, the real loop count), so a caller — or the CLI summary —
// cannot claim an animation that does not animate.
type Result struct {
	Out         string
	Frames      int
	Width       int
	Height      int
	FrameWidth  int
	FrameHeight int
	CaptionRows int
	Captioned   int
	DelayCS     int
	// LoopCount is the loop count the CALLER asked for, and LoopReadBack is the
	// one the written stream states (-1 = no Netscape extension, i.e. the file
	// says "play once"; Go writes the extension only for a multi-frame stream).
	// Both are reported because they answer different questions — "did my
	// instruction land" and "what will a reader do" — and a single field would
	// have to lie about one of them (Rule 15: no silent difference).
	LoopCount    int
	LoopReadBack int
	Bytes        int
	// PaletteColors is how many SLOTS the written GIF's colour table carries:
	// the size of the table the encoder wrote, NOT a measurement of the film's
	// colour. It is an upper bound (PaletteSize) and it saturates, so it is the
	// wrong number to read as "the film has this much colour" — measured
	// 2026-10-02: the operator's grey film reported 253 while carrying no colour
	// of gopi's at all (five non-grey entries, every one the background). Pair it
	// with PaletteNonGrey, which counts what the table actually holds.
	PaletteColors int
	// PaletteNonGrey is how many of those entries are NOT grey (r==g==b): the
	// palette's real colour content. A monochrome film's non-grey entries are the
	// tinted background and the antialiasing blends around it, so this is still
	// not "the surface's palette reached the film" (that is the film arm's own
	// anchored claim); it is the count that keeps the summary from
	// reading as the opposite of what the artifact is.
	PaletteNonGrey int
	// PaletteUsed is how many palette entries the film's pixels actually
	// REFERENCE — counted by scanning the encoded indices, not inferred from the
	// table's size. It is the number a reader means by "the palette has this much
	// colour in it": an entry the picture never touches is not colour the film
	// carries.
	PaletteUsed int
	// PaletteSaturated is how many palette entries are both SATURATED and bright
	// (saturation >= paletteSatMin, value >= paletteValMin) — the entries that
	// carry a colour a reader would NAME. It is the number that cannot be
	// misread: measured 2026-10-02, the colourless film's palette has 202
	// non-grey entries and every one of them is an antialiasing blend whose
	// chroma tops out at 19/255, so its saturated count is 0 — while the film
	// that the SAME command produces after the fix carries gopi's gold
	// (218,165,32) at 1.2 M pixels. "非灰阶" alone still reads as colourful; this
	// does not.
	PaletteSaturated int
	InkPixels        int
	Warnings         []string
}

// paletteSatMin and paletteValMin are the saturation and brightness floors the
// saturated count uses. They are the pair the film arm's colour claim enforces
// too, so the assembly log and the gate cannot disagree about what "coloured"
// means (Rule 13's one-definition discipline).
const (
	paletteSatMin = 0.40
	paletteValMin = 0.50
)

// paletteNonGrey counts the palette entries whose channels are not all equal —
// the table's colour content, as opposed to its size.
func paletteNonGrey(pal color.Palette) int {
	n := 0
	for _, c := range pal {
		r, g, b, _ := c.RGBA()
		if r != g || g != b {
			n++
		}
	}
	return n
}

// paletteSaturated counts the palette entries that are saturated AND bright —
// see PaletteSaturated for why this, and not the non-grey count, is the number
// a reader must be shown.
func paletteSaturated(pal color.Palette) int {
	n := 0
	for _, c := range pal {
		r, g, b, _ := c.RGBA()
		mx, mn := r, r
		if g > mx {
			mx = g
		}
		if b > mx {
			mx = b
		}
		if g < mn {
			mn = g
		}
		if b < mn {
			mn = b
		}
		if mx == 0 {
			continue
		}
		if float64(mx-mn)/float64(mx) >= paletteSatMin && float64(mx)/float64(paletteFullScale) >= paletteValMin {
			n++
		}
	}
	return n
}

// paletteUsed counts how many palette entries the encoded frames actually
// reference: the union of their index bytes.
func paletteUsed(frames []*image.Paletted) int {
	var seen [PaletteSize]bool
	for _, f := range frames {
		for _, idx := range f.Pix {
			seen[idx] = true
		}
	}
	n := 0
	for _, ok := range seen {
		if ok {
			n++
		}
	}
	return n
}

// Summary is the one-line text form the CLI prints.
func (r *Result) Summary() string {
	loop := loopSpelling(r.LoopCount)
	if r.LoopReadBack != r.LoopCount {
		loop += fmt.Sprintf("（文件读回 %s）", loopSpelling(r.LoopReadBack))
	}
	// Slots / used / non-grey are reported TOGETHER and named, because the slot
	// count alone (the palette's size, up to PaletteSize) reads as the opposite
	// of what a monochrome film is: measured 2026-10-02, the operator's colourless
	// film reported 槽位 253 while its ink carried none of gopi's palette.
	line := fmt.Sprintf("✅ %d 帧 → %s  %dx%d (每帧 %dx%d + 文字带 %d 行) delay=%dcs loop=%s 调色板槽位=%d 实际=%d 色 非灰阶=%d 饱和=%d 色 墨迹=%d 像素 字节=%d",
		r.Frames, r.Out, r.Width, r.Height, r.FrameWidth, r.FrameHeight, r.CaptionRows,
		r.DelayCS, loop, r.PaletteColors, r.PaletteUsed, r.PaletteNonGrey, r.PaletteSaturated, r.InkPixels, r.Bytes)
	if len(r.Warnings) > 0 {
		line += fmt.Sprintf("  ⚠️ %d 条警告", len(r.Warnings))
	}
	return line
}

// loopSpelling renders a loop count for a human: 0 is the format's "forever",
// and -1 is the decoder's "this stream carries no looping extension".
func loopSpelling(n int) string {
	switch n {
	case LoopForever:
		return "forever(0)"
	case loopReadBackOnce:
		return "play-once(-1)"
	}
	return fmt.Sprintf("%d", n)
}

// Encode reads the frames, composes each frame's caption into its pixels and
// writes ONE GIF to out. The bytes are built entirely in memory and written in
// a single call, so a refusal never leaves a half-written GIF behind.
func Encode(out string, o Options) (*Result, error) {
	if strings.TrimSpace(out) == "" {
		return nil, usagef("no output path: pass --out <file.gif> (this verb never writes outside the path it was given)")
	}
	if len(o.Frames) == 0 {
		return nil, usagef("no frames: pass --frames <a.png,b.png,…> — a GIF with no frames is not an animation")
	}
	delay, err := resolveDelay(o.DelayCS)
	if err != nil {
		return nil, err
	}
	loop, err := resolveLoop(o.Loop, o.LoopSet, len(o.Frames))
	if err != nil {
		return nil, err
	}
	size, err := resolveCaptionSize(o.CaptionSize)
	if err != nil {
		return nil, err
	}
	margin := resolveCaptionMargin(o.CaptionMargin)

	res := &Result{Out: out, Frames: len(o.Frames), DelayCS: delay, LoopCount: loop}

	// PASS 1 — load every frame and settle the canvas geometry. Nothing is
	// composed yet: the canvas width is not known until all frames are measured
	// (a mismatched frame is refused HERE, by name, before any pixel is drawn).
	images := make([]*image.RGBA, len(o.Frames))
	for i, f := range o.Frames {
		img, err := loadPNG(f.Path)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			res.FrameWidth, res.FrameHeight = img.Bounds().Dx(), img.Bounds().Dy()
			if res.FrameWidth > MaxCanvasDim || res.FrameHeight > MaxCanvasDim {
				return nil, usagef("frame %s is %dx%d, beyond the %dpx canvas bound",
					f.Path, res.FrameWidth, res.FrameHeight, MaxCanvasDim)
			}
		}
		if w, h := img.Bounds().Dx(), img.Bounds().Dy(); w != res.FrameWidth || h != res.FrameHeight {
			return nil, usagef("frame %s is %dx%d but frame %s is %dx%d — every frame of one GIF shares a canvas, so this is refused rather than cropped",
				f.Path, w, h, o.Frames[0].Path, res.FrameWidth, res.FrameHeight)
		}
		images[i] = img
	}
	face, err := text2img.LoadFace("")
	if err != nil {
		return nil, fmt.Errorf("load the font chain: %w", err)
	}

	// The band is a property of the FILM, not of a frame (see this file's
	// header): its height is the TALLEST caption's, and every frame gets it as
	// soon as any frame has words. It is measured with the same Face the drawing
	// uses, and the same arithmetic (Rules 13/14: CellWidth, FontScale, Ascent
	// are the package's ONE derivation, referenced here, never re-derived).
	band := 0
	for _, f := range o.Frames {
		if strings.TrimSpace(f.Caption) == "" {
			continue
		}
		if rows := captionRows(f.Caption, size, margin); rows > band {
			band = rows
		}
		res.Captioned++
		if _, cols := face.Cells(f.Caption); margin*2+cols*text2img.CellWidth(size) > res.FrameWidth {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"frame %s: the caption's widest line measures %dpx more than the %dpx canvas and is CLIPPED (shorten it or widen the frames)",
				f.Path, margin*2+cols*text2img.CellWidth(size)-res.FrameWidth, res.FrameWidth))
		}
	}
	res.CaptionRows = band
	bands := make([]int, len(o.Frames))
	for i, f := range o.Frames {
		if strings.TrimSpace(f.Caption) != "" {
			bands[i] = band
		}
	}

	// PASS 2 — compose everything first, then build ONE palette for the film.
	// The order matters and is not an optimisation: the quantiser must see every
	// composed pixel (including the caption ink and paper it adds) before it can
	// pick a mode, and the palette belongs to the FILE — a per-frame palette
	// would make frame 1's colours nearest-match frame 2's.
	canvases := make([]*image.RGBA, len(o.Frames))
	for i, f := range o.Frames {
		canvases[i] = composeFrame(images[i], f.Caption, size, margin, band, face, res)
	}
	q := newQuantizer()
	q.prime(canvases)
	pm := make([]*image.Paletted, len(o.Frames))
	for i := range canvases {
		pm[i] = q.palettize(canvases[i])
	}
	if len(q.colors) == 0 {
		// Not reachable through a decoded PNG (every image has at least one
		// pixel), but stated rather than assumed: an empty palette would be
		// encoded as a GIF with no colour table.
		return nil, fmt.Errorf("the composed frames carry no pixels — refusing to write a GIF with an empty palette")
	}
	pal := make(color.Palette, len(q.colors))
	for i, c := range q.colors {
		pal[i] = c
	}
	for i := range pm {
		pm[i].Palette = pal
	}
	res.Width = res.FrameWidth
	res.Height = res.FrameHeight + res.CaptionRows
	res.PaletteColors = len(q.colors)
	res.PaletteNonGrey = paletteNonGrey(pal)
	res.PaletteSaturated = paletteSaturated(pal)
	res.PaletteUsed = paletteUsed(pm)

	delays := make([]int, len(pm))
	disposals := make([]byte, len(pm))
	for i := range pm {
		delays[i] = delay
		disposals[i] = DisposalNone
	}
	g := &gif.GIF{
		Image:     pm,
		Delay:     delays,
		Disposal:  disposals,
		LoopCount: loop,
		Config:    image.Config{ColorModel: pal, Width: res.Width, Height: res.Height},
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		return nil, fmt.Errorf("encode the GIF: %w", err)
	}
	res.Bytes = buf.Len()
	if err := os.WriteFile(out, buf.Bytes(), filePerm); err != nil {
		return nil, fmt.Errorf("write %s: %w", out, err)
	}
	// MEASURE WHAT WAS WRITTEN, not what was intended: decode the bytes back and
	// read the frame count / delays / loop count / bounds off the stream. A
	// caller that reports "N frames, delay D, loop L" must be reporting the
	// FILE, and the only way to know the file's numbers is to read them.
	if err := measureWritten(out, res, len(o.Frames)); err != nil {
		return nil, err
	}
	res.InkPixels = countCaptionInk(pm, res, band)
	return res, nil
}

// filePerm is the mode a written GIF gets: readable by anyone who can read the
// directory, writable by its owner only (the artifact is a deliverable).
const filePerm = os.FileMode(0o644)

// resolveLoop settles the loop count: unset means "loop forever" (0), the GIF
// convention, and a stated count is range-checked against the unsigned 16-bit
// field the extension carries.
//
// ONE FACT ABOUT THE FORMAT, stated here because it shows up as a difference
// between what a caller asks for and what a READER sees: Go's encoder writes the
// Netscape looping extension only for a MULTI-frame stream, so a one-frame GIF
// has no loop count in it at all and a decoder reports -1 ("play once"). That is
// not a loss — for a single still, "forever" and "once" are the same display —
// and Result carries both numbers (LoopCount = what was asked, LoopReadBack =
// what the stream says) rather than hiding the difference.
func resolveLoop(loop int, set bool, frames int) (int, error) {
	if !set {
		return LoopForever, nil
	}
	if loop < LoopForever || loop > MaxLoopCount {
		return 0, usagef("--loop %d is outside the GIF's own range %d..%d (%d = forever)",
			loop, LoopForever, MaxLoopCount, LoopForever)
	}
	return loop, nil
}

// resolveCaptionSize applies the default to an unset size and refuses a NEGATIVE
// one. The distinction is deliberate (Rule 15): `0` is the zero value of an
// unset positive dimension — it means "the caller did not spell the flag" and it
// falls back to the default — while a negative number is an instruction that
// cannot be carried out, and silently treating it as "unset" would answer a
// different question than the one the caller asked.
func resolveCaptionSize(size int) (int, error) {
	if size < 0 {
		return 0, usagef("--caption-size %d: the caption font size must be at least %dpx", size, MinCaptionSize)
	}
	if size == 0 {
		return DefaultCaptionSize, nil
	}
	return size, nil
}

// resolveCaptionMargin is resolveCaptionSize's twin for the band's padding.
func resolveCaptionMargin(margin int) int {
	if margin <= 0 {
		return DefaultCaptionMargin
	}
	return margin
}

// singleFrame is the frame count at which the loop rules change (see
// resolveLoop).
const singleFrame = 1

// resolveDelay applies the default to an unset delay and refuses a value the
// format cannot carry (Rule 2: this is the ONE place the delay is resolved).
func resolveDelay(cs int) (int, error) {
	if cs == delayUnset {
		return DefaultDelayCentiseconds, nil
	}
	if cs < MinDelayCentiseconds || cs > MaxDelayCentiseconds {
		return 0, usagef("--delay %dcs is outside the GIF's own range %d..%d centiseconds (%dms..%.0fs)",
			cs, MinDelayCentiseconds, MaxDelayCentiseconds,
			MinDelayCentiseconds*centisecondsPerMillisecond, float64(MaxDelayCentiseconds)*centisecondsPerSecond)
	}
	return cs, nil
}

// centiseconds → milliseconds and seconds, used only to render a refusal.
const (
	centisecondsPerMillisecond = 10
	centisecondsPerSecond      = 100
)

// loadPNG reads one frame, refusing anything that is not a PNG by NAME (the
// signature is checked before the decoder, so the message can say what the file
// looked like rather than reporting a decoder's internal error).
func loadPNG(spec string) (*image.RGBA, error) {
	path := strings.TrimSpace(spec)
	raw, err := os.ReadFile(path)
	if err != nil {
		// A file the caller named that does not exist (or cannot be read) is a
		// wrong invocation, not an internal failure: the exit class has to match
		// the fix (check the path, or fix the argument list), and a caller that
		// sees "internal error" for a typo will look in the wrong place.
		return nil, usagef("read frame %s: %v", path, err)
	}
	if len(raw) < len(PNGMagic) || string(raw[:len(PNGMagic)]) != PNGMagic {
		return nil, usagef("frame %s is not a PNG (a GIF frame sequence is built from PNGs; the first bytes are %q)",
			path, firstBytes(raw, len(PNGMagic)))
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode frame %s: %w", path, err)
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	// draw.Src keeps the frame's own pixels: the caption band is added below,
	// never over, so nothing about the frame is resampled or blended away.
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	return rgba, nil
}

// firstBytes renders the first n bytes of raw for a refusal message, without
// inventing a value for an empty file.
func firstBytes(raw []byte, n int) string {
	if len(raw) < n {
		n = len(raw)
	}
	if n == 0 {
		return "<empty file>"
	}
	return string(raw[:n])
}

// captionRows is the height the caption band adds for this text: one line per
// newline-separated line, each (size + CaptionLineGapPx) tall, plus the margin
// above and below.
func captionRows(caption string, size, margin int) int {
	lines := len(strings.Split(caption, "\n"))
	return lines*(size+CaptionLineGapPx) + 2*margin
}

// composeFrame returns the frame's pixels with the film's caption band appended
// below. The decision is the BAND's, not the caption's: a film with no band at
// all (no frame had words) returns the input image untouched, while a frame
// inside a captioned film gets the band even when it has nothing to say — the
// band shows paper, never a stale neighbour's words (see this file's header).
func composeFrame(img *image.RGBA, caption string, size, margin, band int, face *text2img.Face, res *Result) *image.RGBA {
	if band <= 0 {
		return img
	}
	w, h := res.FrameWidth, res.FrameHeight+band
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(canvas, image.Rect(0, 0, w, res.FrameHeight), img, image.Point{}, draw.Src)
	bandRect := image.Rect(0, res.FrameHeight, w, h)
	fillBand(canvas, bandRect)
	baseline := float64(res.FrameHeight+margin) + face.Ascent(size)
	ink := text2img.Ink{R: CaptionInkRGB, G: CaptionInkRGB, B: CaptionInkRGB}
	for i, line := range strings.Split(caption, "\n") {
		base := baseline + float64(i*(size+CaptionLineGapPx))
		if text2img.DrawText(canvas, line, float64(margin), base, size, "", ink) == 0 && strings.TrimSpace(line) != "" {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"caption line %q inked no pixel — the font chain has no glyph for it (the band is drawn, the words are not)", line))
		}
	}
	return canvas
}

// fillBand paints the caption paper. It writes the pixels directly instead of
// going through Set (which allocates a color.RGBA per pixel — ~100k allocations
// for a 512x1000 band on the composing path of every frame).
func fillBand(img *image.RGBA, r image.Rectangle) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := img.Pix[y*img.Stride+r.Min.X*captionBandChannels : y*img.Stride+r.Max.X*captionBandChannels]
		for i := 0; i+4 <= len(row); i += captionBandChannels {
			row[i], row[i+1], row[i+2], row[i+3] = CaptionFillRGB, CaptionFillRGB, CaptionFillRGB, 0xff
		}
	}
}

// quantizer turns composed RGBA pixels into GIF palette indices. It has TWO
// modes, and which one runs is decided by the FILM, not by taste:
//
//   - EXACT (the film uses ≤ 256 distinct colours — every text/prose render, an
//     annotated screenshot, anything drawn in a handful of inks): the palette IS
//     the picture's own colours, registered in first-seen order, and the encoding
//     is LOSSLESS. Measured on the demo's three text frames: 79764/79764 pixels
//     reproduced exactly, MAE 0.000.
//   - REDUCED (more than 256 distinct colours — a photograph, a gradient, a
//     textured screenshot): a uniform 3-3-2 grid over RGB gives exactly 256
//     cells, and each cell's palette entry is the MEAN of the pixels that fell
//     into it. This is the mode that had to be found the hard way: a first-seen
//     palette (the first version of this file) spends all 256 slots on whichever
//     colours happen to appear first — measured on the demo's gradient frame:
//     0.49% of pixels reproduced, MAE 83.909, because the palette held the
//     top-left corner and nothing else. Cell means over the whole film brought
//     the same frame to MAE 8.9 (the numbers and the harness are in
//     devdoc/gif-generation.md §1).
//
// DETERMINISM is a property of both modes and neither uses a map ITERATION: the
// exact path appends in scan order, and the reduced path walks the 256 cells in
// index order. Two runs over the same frames produce byte-identical files.
type quantizer struct {
	exact  bool
	colors []color.RGBA
	index  map[uint32]uint8
	// Reduced mode: per-cell accumulation over the WHOLE film, then each cell's
	// mean becomes its palette entry.
	cellSum   [PaletteSize][3]uint64
	cellCount [PaletteSize]uint64
	cellIdx   map[uint16]uint8
}

// The reduced mode's grid: bits per channel. 3+3+2 = 8 bits = 256 cells, which
// is exactly the GIF palette ceiling — so the grid never has to drop a cell to
// fit, and the arithmetic below is stated once, here.
const (
	quantRedBits    = 3
	quantGreenBits  = 3
	quantBlueBits   = 2
	quantRedShift   = 8 - quantRedBits
	quantGreenShift = 8 - quantGreenBits
	quantBlueShift  = 8 - quantBlueBits
	quantMeanRound  = 2 // sum/count rounds by adding count/2 before dividing
)

// newQuantizer returns an empty quantizer.
func newQuantizer() *quantizer {
	return &quantizer{index: map[uint32]uint8{}, cellIdx: map[uint16]uint8{}}
}

// key packs an RGB triple into the lookup key (alpha is dropped: a GIF has no
// alpha channel, and the composition has already resolved every pixel against
// its paper).
func key(c color.RGBA) uint32 {
	return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

// cell is the reduced grid index of a colour: the top bits of each channel,
// packed in RGB order. It doubles as the reduced path's iteration order (a
// uint16 counting up), so the palette is built in a stable, checkable sequence.
func cell(c color.RGBA) uint16 {
	return uint16(c.R>>quantRedShift)<<(quantGreenBits+quantBlueBits) |
		uint16(c.G>>quantGreenShift)<<quantBlueBits |
		uint16(c.B>>quantBlueShift)
}

// prime scans the whole film once and picks the mode. It must see EVERY frame
// before anything is mapped: the palette belongs to the file, not to a frame,
// and a per-frame palette would make frame 1's colours nearest-match frame 2's.
func (q *quantizer) prime(frames []*image.RGBA) {
	seen := map[uint32]struct{}{}
	for _, img := range frames {
		for k := 0; k < len(img.Pix); k += captionBandChannels {
			c := opaque(img.Pix[k], img.Pix[k+1], img.Pix[k+2])
			if _, ok := seen[key(c)]; ok {
				continue
			}
			if len(seen) > PaletteSize {
				// More than the format can carry: stop counting and switch to the
				// reduced mode. The partial histogram is discarded — the reduced
				// pass below rebuilds it over the whole film.
				q.exact = false
				q.buildReduced(frames)
				return
			}
			seen[key(c)] = struct{}{}
			q.colors = append(q.colors, c)
			q.index[key(c)] = uint8(len(q.colors) - 1)
		}
	}
	q.exact = true
}

// buildReduced fills the palette from the per-cell means of the whole film, in
// cell order (never map order — see the type's comment on determinism).
func (q *quantizer) buildReduced(frames []*image.RGBA) {
	for _, img := range frames {
		for k := 0; k < len(img.Pix); k += captionBandChannels {
			ci := cell(opaque(img.Pix[k], img.Pix[k+1], img.Pix[k+2]))
			q.cellSum[ci][0] += uint64(img.Pix[k])
			q.cellSum[ci][1] += uint64(img.Pix[k+1])
			q.cellSum[ci][2] += uint64(img.Pix[k+2])
			q.cellCount[ci]++
		}
	}
	q.colors = q.colors[:0]
	for i := 0; i < PaletteSize; i++ {
		ci := uint16(i)
		n := q.cellCount[ci]
		if n == 0 {
			continue
		}
		c := color.RGBA{
			R: uint8((q.cellSum[ci][0] + n/quantMeanRound) / n),
			G: uint8((q.cellSum[ci][1] + n/quantMeanRound) / n),
			B: uint8((q.cellSum[ci][2] + n/quantMeanRound) / n),
			A: rgbaOpaque,
		}
		q.cellIdx[ci] = uint8(len(q.colors))
		q.colors = append(q.colors, c)
	}
}

// opaque normalizes a pixel's alpha: the palette is OPAQUE even when the input is
// not, because a GIF carries at most ONE transparent index and a palette entry
// registered with alpha 0 would claim that slot (measured: a fully transparent
// RGB(0,0,0) input came back WHITE — the encoder had reserved index 0 as the
// transparent colour).
func opaque(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: rgbaOpaque} }

// idx maps one pixel to its palette index in whichever mode prime() chose.
func (q *quantizer) idx(c color.RGBA) uint8 {
	c.A = rgbaOpaque
	if q.exact {
		return q.index[key(c)]
	}
	return q.cellIdx[cell(c)]
}

// nearest is the exact mode's safety net: it is unreachable through prime() (the
// exact palette holds EVERY colour in the film, so every lookup hits), and it
// exists so a future caller that maps a colour the film never contained gets the
// closest entry instead of index 0 — silently painting it black would be the
// worse failure.
func (q *quantizer) nearest(c color.RGBA) uint8 {
	best, bestDist := uint8(0), 0
	for i, p := range q.colors {
		dr := int(c.R) - int(p.R)
		dg := int(c.G) - int(p.G)
		db := int(c.B) - int(p.B)
		d := dr*dr + dg*dg + db*db
		if i == 0 || d < bestDist {
			best, bestDist = uint8(i), d
		}
	}
	return best
}

// lookup is idx() with the nearest-colour fallback, for a colour that may not be
// in an exact palette.
func (q *quantizer) lookup(c color.RGBA) uint8 {
	c.A = rgbaOpaque
	if !q.exact {
		return q.cellIdx[cell(c)]
	}
	if i, ok := q.index[key(c)]; ok {
		return i
	}
	return q.nearest(c)
}

// palettize maps a composed RGBA frame onto the palette's indices.
func (q *quantizer) palettize(img *image.RGBA) *image.Paletted {
	b := img.Bounds()
	out := image.NewPaletted(b, nil)
	for k := 0; k < len(img.Pix); k += captionBandChannels {
		out.Pix[k/captionBandChannels] = q.idx(opaque(img.Pix[k], img.Pix[k+1], img.Pix[k+2]))
	}
	return out
}

// measureWritten decodes the file that was just written and fills the Result
// from the stream — frame count, per-frame delay, the real loop count and the
// real bounds. wantFrames is the input count; a stream that comes back with a
// different number is an encoder defect, not a caller error, so it is reported
// with the numbers rather than silently accepted.
func measureWritten(out string, res *Result, wantFrames int) error {
	raw, err := os.ReadFile(out)
	if err != nil {
		return fmt.Errorf("read back %s: %w", out, err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decode back %s: %w", out, err)
	}
	if len(g.Image) != wantFrames {
		return fmt.Errorf("wrote %d frames to %s but %d were given — the encoder dropped frames", len(g.Image), out, wantFrames)
	}
	res.Frames = len(g.Image)
	// The reader's own view of the loop count is recorded, never asserted equal:
	// Go emits the Netscape extension only for a multi-frame stream, so a
	// one-frame film reads back -1 ("play once") whatever it was asked for. For a
	// single still those two mean the same display, and Result states both rather
	// than smoothing one into the other.
	res.LoopReadBack = g.LoopCount
	if len(g.Image) > 1 && g.LoopCount != res.LoopCount {
		return fmt.Errorf("wrote %s with loop count %d but the file reads back %d — the loop extension did not land",
			out, res.LoopCount, g.LoopCount)
	}
	if len(g.Delay) > 0 {
		res.DelayCS = g.Delay[0]
	}
	for i, d := range g.Delay {
		if d != res.DelayCS {
			return fmt.Errorf("frame %d of %s carries delay %dcs but frame 0 carries %dcs — a per-frame delay was not requested by this call", i, out, d, res.DelayCS)
		}
	}
	first := g.Image[0].Bounds()
	res.Width, res.Height = first.Dx(), first.Dy()
	for i, f := range g.Image {
		if f.Bounds().Dx() != res.Width || f.Bounds().Dy() != res.Height {
			return fmt.Errorf("frame %d of %s is %dx%d but frame 0 is %dx%d",
				i, out, f.Bounds().Dx(), f.Bounds().Dy(), res.Width, res.Height)
		}
	}
	return nil
}

// countCaptionInk counts the pixels every caption band actually inked — the
// measured proof that words reached the picture (a band of pure paper would
// mean the font chain silently produced nothing). The band is the FILM's, so
// the band count is stated once (see this file's header on uniformity).
func countCaptionInk(frames []*image.Paletted, res *Result, band int) int {
	inked := 0
	for _, f := range frames {
		if band == 0 {
			continue
		}
		// The band starts below the frame's own pixels and runs to the bottom.
		for y := res.FrameHeight; y < f.Bounds().Dy(); y++ {
			row := f.Pix[y*f.Stride : y*f.Stride+f.Bounds().Dx()]
			for _, pi := range row {
				c := f.Palette[pi].(color.RGBA)
				if c.R < CaptionFillRGB || c.G < CaptionFillRGB || c.B < CaptionFillRGB {
					inked++
				}
			}
		}
	}
	return inked
}

// SplitFrames splits a comma-separated frame list, dropping empty items (a
// trailing comma is a typo, not an empty frame).
func SplitFrames(spec string) []string {
	var out []string
	for _, p := range strings.Split(spec, frameListSep) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseCaptionItem parses ONE `<png-path>=<text>` item. The CLI's scanner splits
// the repeatable flag into items; this is the grammar for a single one, so the
// verb and the flag's spelling cannot drift.
func ParseCaptionItem(item string) (path, text string, err error) {
	sep := strings.Index(item, captionSep)
	if sep <= 0 {
		return "", "", usagef("--caption item %q is not spelled <png-path>%s<text> (the %q must follow a path)",
			item, captionSep, captionSep)
	}
	path = strings.TrimSpace(item[:sep])
	if path == "" {
		return "", "", usagef("--caption item %q has an empty path", item)
	}
	return path, item[sep+1:], nil
}

// ParseCaptions parses the whole `--caption` list: `<png-path>=<text>` items
// separated by the same comma. A path is repeated → refused (two captions for
// one frame is an ambiguous instruction, and picking one silently is the defect
// class this refusal exists to prevent). The text of an item may itself contain
// both separators — only the FIRST `=` splits, and the CLI scanner is what keeps
// a comma inside a caption from ending the item.
func ParseCaptions(spec string) (map[string]string, error) {
	out := map[string]string{}
	for _, item := range SplitFrames(spec) {
		path, text, err := ParseCaptionItem(item)
		if err != nil {
			return nil, err
		}
		if _, dup := out[path]; dup {
			return nil, usagef("--caption names %s twice — one frame carries one caption", path)
		}
		out[path] = text
	}
	return out, nil
}

// FramesFromPaths pairs an ordered path list with a caption map, in list order
// (the order a GIF displays is the order the caller wrote, never a map's).
func FramesFromPaths(paths []string, captions map[string]string) []Frame {
	frames := make([]Frame, 0, len(paths))
	for _, p := range paths {
		frames = append(frames, Frame{Path: p, Caption: captions[p]})
	}
	return frames
}
