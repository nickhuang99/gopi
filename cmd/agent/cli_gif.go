// cli_gif.go — `gopi tool gif --frames <a.png,b.png,…> --out <file.gif>`: a PNG frame
// sequence becomes ONE animated GIF, each frame carrying the words that explain
// it.
//
// RULE 3'S BOUNDARY. This is not a new top-level TOOL: the toolset stays at the
// fixed 17, and gif is a CLI verb reached through `bash` — the same shape
// `gopi tool text2img`, `gopi tool capture` and `gopi tool annotate` already have. The engine
// lives in internal/gifgen, which draws its captions with internal/text2img (the
// repository's ONE glyph rasteriser), so no second rasteriser and no new
// dependency is added: stdlib image/gif + the code we already own.
//
// WHY THIS VERB IS NOT `gopi tool annotate --animate`. annotate's contract is that it
// writes a NEW STILL and never touches its input; an animation has a third
// dimension annotate's geometry (pixels of one image) cannot express — the frame
// ORDER and each frame's DURATION. Two verbs, two grammars, one rasteriser
// underneath.
//
// WHY A GIF AT ALL. The alternatives cost what gopi does not pay: an external
// tool (ffmpeg / ImageMagick / gifsicle — Rule 35 forbids the dependency) or an
// HTML page (needs a browser, cannot be attached to a ledger row, a chat answer
// or a report). A GIF is one file, plays in a terminal, an editor, a browser and
// a chat, and diffs byte-for-byte between two runs. The measured trade-off,
// including what GIF is WORSE at, is devdoc/gif-generation.md (§"文字带" and
// the quantizer pitfalls); the legibility measurements are in
// devdoc/human-ui-gif-legibility.md.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/nickhuang99/gopi/internal/gifgen"
)

// gifUsage is the -h text for the gif subcommand.
const gifUsage = `gopi tool gif — turn a PNG frame sequence into ONE animated GIF, with words on it

Reads frames in the order you name them, draws each frame's caption on a white
band APPENDED BELOW it (never over its pixels — a caption that covers what it
explains destroys the evidence it exists to carry) and writes one looping GIF.

Zero dependencies: Go's standard library image/gif plus this repository's own
rasteriser (the one ` + "`gopi tool text2img`" + ` and ` + "`gopi tool annotate`" + ` use). No ffmpeg,
no ImageMagick, no gifsicle, no new module — so the verb runs wherever the
binary runs.

Usage:
  gopi tool gif --frames <a.png,b.png,…> --out <file.gif> [options]

Required:
  --frames LIST      Comma-separated PNG paths, IN THE ORDER THEY PLAY. Every
                     frame must be the same size (a GIF has one logical screen;
                     a mismatched frame is refused, never cropped).
  --out PATH         The GIF to write.

Options:
  --caption PATH=TEXT   Repeatable: the words drawn under the frame at PATH.
                        Appears once per frame; the text may contain '=' (only
                        the FIRST one splits) and any commas. Naming the same
                        path twice is refused.
  --caption-size N      Caption font size in px (default 20, text2img's own).
  --caption-margin N    Caption band padding in px (default 12).
  --delay N             How long each frame shows, in CENTISECONDS (default 25 =
                        250 ms). The GIF format carries this as a 16-bit field,
                        so 1..65535; 0 is refused rather than reinterpreted.
  --loop N              How many times the GIF repeats (0 = forever, the
                        default). A one-frame GIF carries no looping extension
                        at all — Go's encoder writes it only for multi-frame
                        streams — so its read-back count is -1; the summary
                        reports both numbers rather than hiding the difference.
  --json                Print the machine-readable result instead of one line.

WIDTH: the canvas is your frames' own, and this verb never resizes or refuses
one. But an animation wider than a phone conversation view accepts may not play
there, so a product over that ceiling is REPORTED — the warning names the width,
the ceiling and the file, on stderr (and in the --json "warnings" array). The
file is still written: composing for another reader is a legitimate use.

The band is a property of the FILM: as soon as any frame has words, EVERY frame
gets the same band height (the tallest caption's). A frame with nothing to say
shows empty paper — not a shorter picture, which would let the previous frame's
words stay on screen under it (disposal=none).

Examples:
  gopi tool gif --frames a.png,b.png,c.png --out out.gif
  gopi tool gif --frames a.png,b.png --out out.gif --delay 40 --loop 3 \
    --caption "a.png=第 1 步：起点" --caption "b.png=第 2 步：终点"
`

// The flag names, as constants (Rule 1b: one spelling, referenced).
const (
	gifFlagOut           = "out"
	gifFlagFrames        = "frames"
	gifFlagCaption       = "caption"
	gifFlagCaptionSize   = "caption-size"
	gifFlagCaptionMargin = "caption-margin"
	gifFlagDelay         = "delay"
	gifFlagLoop          = "loop"
	gifFlagJSON          = "json"
)

// gifFlagMembers lists every flag the subcommand accepts.
var gifFlagMembers = map[string]bool{
	gifFlagOut:           true,
	gifFlagFrames:        true,
	gifFlagCaption:       true,
	gifFlagCaptionSize:   true,
	gifFlagCaptionMargin: true,
	gifFlagDelay:         true,
	gifFlagLoop:          true,
	gifFlagJSON:          true,
}

// gifRequest is one parsed invocation.
type gifRequest struct {
	out      string
	frames   []string
	captions []string
	size     int
	margin   int
	delay    int
	loop     int
	loopSet  bool
	json     bool
}

// isGifSubcommand reports whether argv begins with the gif subcommand and runs
// it.
func isGifSubcommand(args []string) bool {
	if len(args) > 0 && args[0] == CliVerbGif {
		runGifSubcommand(args[1:])
		return true
	}
	return false
}

// parseGifArgs parses the flags without touching the process (unit-testable,
// per the parseAnnotateArgs / parseCaptureArgs pattern).
func parseGifArgs(args []string) (gifRequest, error) {
	var req gifRequest
	fs := newMemoryFlagSet()
	fs.repeat(gifFlagCaption)
	positional, err := fs.parse(args)
	if err != nil {
		return req, err
	}
	for _, name := range fs.order {
		if !gifFlagMembers[name] {
			return req, fmt.Errorf("unknown flag --%s", name)
		}
	}
	// No positional arguments: this verb's grammar is flags only, so a stray
	// token is refused BY NAME rather than silently taken as a frame (which
	// would produce a GIF from the wrong file with no complaint).
	if len(positional) > 0 {
		return req, fmt.Errorf("unexpected argument %q: frames come from --%s, one comma-separated list (see `gopi %s -h`)",
			positional[0], gifFlagFrames, CliVerbGif)
	}
	req.out = strings.TrimSpace(fs.get(gifFlagOut))
	req.frames = gifgen.SplitFrames(fs.get(gifFlagFrames))
	req.captions = fs.list(gifFlagCaption)
	req.json = fs.has(gifFlagJSON)
	if err := parseIntFlag(fs.get(gifFlagCaptionSize), gifFlagCaptionSize, &req.size); err != nil {
		return req, err
	}
	if err := parseIntFlag(fs.get(gifFlagCaptionMargin), gifFlagCaptionMargin, &req.margin); err != nil {
		return req, err
	}
	if err := parseIntFlag(fs.get(gifFlagDelay), gifFlagDelay, &req.delay); err != nil {
		return req, err
	}
	if err := parseIntFlag(fs.get(gifFlagLoop), gifFlagLoop, &req.loop); err != nil {
		return req, err
	}
	// Absent vs stated-zero: --loop 0 is a real instruction ("forever"), while
	// no --loop at all means the default. Only the flag's PRESENCE can tell the
	// two apart, which is what has() reads.
	req.loopSet = fs.has(gifFlagLoop)
	return req, nil
}

// runGifSubcommand executes the verb. Exit codes: 2 = the invocation cannot be
// carried out as written (the POSIX usage class), 1 = the run itself failed.
func runGifSubcommand(args []string) {
	if len(args) > 0 && (args[0] == HelpFlag || args[0] == HelpFlagLong) {
		fmt.Print(gifUsage)
		return
	}
	req, err := parseGifArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbGif, err)
		os.Exit(gifUsageExitCode)
	}
	doc, err := gifRun(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbGif, err)
		if gifgen.IsUsage(err) {
			os.Exit(gifUsageExitCode)
		}
		os.Exit(gifRunExitCode)
	}
	if req.json {
		blob, encErr := json.MarshalIndent(doc, "", "  ")
		if encErr != nil {
			fmt.Fprintf(os.Stderr, "gopi %s: cannot render the result as JSON: %v\n", CliVerbGif, encErr)
			os.Exit(gifRunExitCode)
		}
		fmt.Println(string(blob))
		return
	}
	fmt.Println(doc.summary())
	for _, w := range doc.Warnings {
		fmt.Fprintf(os.Stderr, "gopi %s: ⚠️ %s\n", CliVerbGif, w)
	}
}

// The exit codes this verb uses (Rule 1): the POSIX usage class for a
// command line that cannot be carried out, 1 for a run that failed.
const (
	gifUsageExitCode = 2
	gifRunExitCode   = 1
)

// gifOverPlayableWidthWarnFmt is what an over-wide product is TOLD (Rules 1/13:
// one spelling, referenced by the check that prints it). Args: out path, width,
// height, the playable ceiling.
//
// WHY A WARNING AND NOT A REFUSAL. playableGIFMaxWidth is a ceiling of the
// READER (a phone WeChat conversation view), and this verb is a general
// COMPOSER: `gopi tool gif` writes the animation the caller's frames describe,
// and the frames decide the canvas. A caller composing for a different reader
// (a report, an editor, a browser) is a caller this check must not stop, so the
// product is always written — over-wide input still succeeds — and the reader
// constraint is stated instead of being enforced behind the caller's back
// (Rule 15: said, not silently smoothed).
//
// It is not a second opinion about the number: the ceiling is the SAME constant
// the film arm refuses on (chat_film_gif.go), referenced here, never re-typed.
// The film arm refuses because it SHOT the frames and can shoot them narrower;
// this verb cannot — the caller's PNGs are already the picture.
//
// The reading behind the ceiling, and the one-line way to diagnose a GIF that
// will not play on a phone (its WIDTH, read with `od -j6 -N4 file.gif`), are at
// playableGIFMaxWidth and devdoc/film-gif-wechat-playable.md.
const gifOverPlayableWidthWarnFmt = "%s is %d px wide (%d px tall) — OVER the %d px ceiling a phone WeChat conversation view accepts, so this GIF may not play there even though it was written successfully " +
	"(the width is your frames' own; nothing here resizes or refuses it — check it with `od -j6 -N4 %s`)"

// gifJSON is the machine-readable result. loop_read_back rides beside
// loop_count because they answer different questions (see the usage text).
type gifJSON struct {
	Tool             string   `json:"tool"`
	Out              string   `json:"out"`
	Frames           int      `json:"frames"`
	Width            int      `json:"width"`
	Height           int      `json:"height"`
	FrameWidth       int      `json:"frame_width"`
	FrameHeight      int      `json:"frame_height"`
	CaptionRows      int      `json:"caption_rows"`
	Captioned        int      `json:"captioned"`
	DelayCS          int      `json:"delay_cs"`
	LoopCount        int      `json:"loop_count"`
	LoopReadBack     int      `json:"loop_read_back"`
	PaletteColors    int      `json:"palette_colors"`
	PaletteNonGrey   int      `json:"palette_non_grey"`
	PaletteSaturated int      `json:"palette_saturated"`
	PaletteUsed      int      `json:"palette_used"`
	InkPixels        int      `json:"ink_pixels"`
	Bytes            int      `json:"bytes"`
	Warnings         []string `json:"warnings,omitempty"`
	DependsOnTools   string   `json:"depends_on_tools,omitempty"`
}

// summary is the one-line text form.
func (d gifJSON) summary() string {
	return (&gifgen.Result{
		Out: d.Out, Frames: d.Frames, Width: d.Width, Height: d.Height,
		FrameWidth: d.FrameWidth, FrameHeight: d.FrameHeight, CaptionRows: d.CaptionRows,
		Captioned: d.Captioned, DelayCS: d.DelayCS, LoopCount: d.LoopCount, LoopReadBack: d.LoopReadBack,
		PaletteColors: d.PaletteColors, PaletteNonGrey: d.PaletteNonGrey, PaletteSaturated: d.PaletteSaturated, PaletteUsed: d.PaletteUsed,
		InkPixels: d.InkPixels, Bytes: d.Bytes,
		Warnings: d.Warnings,
	}).Summary()
}

// gifRun parses the caption list, encodes, and returns the reportable document.
func gifRun(req gifRequest) (gifJSON, error) {
	captions := map[string]string{}
	for _, item := range req.captions {
		path, text, err := gifgen.ParseCaptionItem(item)
		if err != nil {
			return gifJSON{}, err
		}
		if _, dup := captions[path]; dup {
			return gifJSON{}, fmt.Errorf("--%s names %s twice — one frame carries one caption", gifFlagCaption, path)
		}
		captions[path] = text
	}
	for path := range captions {
		if !slices.Contains(req.frames, path) {
			return gifJSON{}, fmt.Errorf("--%s names %s but --%s does not play it — a caption for a frame that is not in the film is a typo, not an instruction",
				gifFlagCaption, path, gifFlagFrames)
		}
	}
	res, err := gifgen.Encode(req.out, gifgen.Options{
		DelayCS:       req.delay,
		Loop:          req.loop,
		LoopSet:       req.loopSet,
		CaptionSize:   req.size,
		CaptionMargin: req.margin,
		Frames:        gifgen.FramesFromPaths(req.frames, captions),
	})
	if err != nil {
		return gifJSON{}, err
	}
	// The playable-width WARNING (see gifOverPlayableWidthWarnFmt): this verb is a
	// general composer — it writes the animation the caller's frames describe —
	// so an over-wide product is REPORTED, never refused and never resized. The
	// check rides here, on the engine's own result, because the width that
	// matters is the one the FILE carries (the encoder's logical screen), not
	// the first frame's PNG header.
	if res.Width > playableGIFMaxWidth {
		res.Warnings = append(res.Warnings, fmt.Sprintf(gifOverPlayableWidthWarnFmt,
			res.Out, res.Width, res.Height, playableGIFMaxWidth, res.Out))
	}
	return gifJSON{
		Tool: CliVerbGif, Out: res.Out, Frames: res.Frames, Width: res.Width, Height: res.Height,
		FrameWidth: res.FrameWidth, FrameHeight: res.FrameHeight, CaptionRows: res.CaptionRows,
		Captioned: res.Captioned, DelayCS: res.DelayCS, LoopCount: res.LoopCount, LoopReadBack: res.LoopReadBack,
		PaletteColors: res.PaletteColors, PaletteNonGrey: res.PaletteNonGrey, PaletteSaturated: res.PaletteSaturated, PaletteUsed: res.PaletteUsed,
		InkPixels: res.InkPixels, Bytes: res.Bytes,
		Warnings: res.Warnings,
	}, nil
}
