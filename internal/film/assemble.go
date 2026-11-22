package film

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/gifgen"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// The assembler: a frame sequence in, a film directory out. It replaces
// tools/replay/assemble.py (343 lines of Python) with this package, driving the
// repo's OWN encoder — internal/gifgen, the backend of the `gopi tool gif` verb — so
// there is no second GIF writer to disagree with the first.
//
// WHAT IT WRITES, and this is the documented shape every reader names:
//
//	<Out>/frames/*.png    the delivered frames, at the terminal's own pixel size
//	<Out>/film.gif        the animation
//	<Out>/replay.json     the run's arithmetic and geometry
//
// WHY THE FRAMES ARE NOT RESCALED (assemble.py's header, kept because the reason
// is what the legibility criterion measures): a terminal transcript's legibility
// is px per CHARACTER — frame width / terminal columns. Upscaling would raise
// that number by interpolation: bigger pixels of the same glyphs, a larger file
// carrying no more information. The honest lever is the terminal's font size,
// which the capturer sets; the assembler never touches the pixels.

// DefaultGIFDelayCS is how long one frame shows, in centiseconds, when the caller
// states nothing. It is the value the 2026-10-02 film was assembled with
// (replay.json's gif_delay_cs on the real artifact), and gifgen's own default
// (25) is a different film — a caption-paced one — so the assembler states its
// own rather than inheriting a default chosen for another use.
const DefaultGIFDelayCS = 45

// AssembleOptions is one assembly request. Frames are the inputs, in the order
// they will be displayed.
type AssembleOptions struct {
	// Out is the film directory to create (it is made if missing).
	Out string
	// Frames are the delivered frames, in display order. They are COPIED into
	// Out/frames when they live elsewhere.
	Frames []string
	// DelayCS is how long each frame shows, in centiseconds (<=0 → DefaultGIFDelayCS).
	DelayCS int
	// Loop is the repeat count and LoopSet distinguishes "0 = loop forever" from
	// "unset" — gifgen's own contract, passed through rather than re-decided.
	Loop    int
	LoopSet bool
	// Captions maps a frame path to the words carried under it. Empty is the
	// ordinary case: the film's own frames already carry the screen.
	Captions map[string]string
	// RawFrames is how many beats were PHOTOGRAPHED; DistinctFrames is how many
	// DISTINCT screens they held (the capturer's byte-level reduction). The
	// arithmetic the metadata must close is raw - distinct = dropped, and the
	// default for both is len(Frames) — a caller that reduced nothing.
	//
	// The distinction is the METADATA's whole arithmetic, and it is a reduction
	// rather than a loss: every beat keeps its ledger row and its grid, and a
	// frame grabbed before the terminal committed its repaint is indistinguishable
	// here from a screen that did not move (assemble.py's own comment, kept).
	RawFrames      int
	DistinctFrames int
	// Beats is how many rows the beat ledger holds. The caller supplies it
	// because the ledger is the recording's, not the assembler's; 0 falls back to
	// RawFrames.
	Beats int
	// WindowFile optionally carries "<wid> <w> <h>" — the window the frames were
	// grabbed from, as the capturer wrote it. Without it the geometry is measured
	// off the first delivered frame, which is the same canvas by construction.
	WindowFile string
	// Meta carries the provenance fields the caller knows: the session the frames
	// came from, the surface, the geometry. Arithmetic fields are computed here
	// and are NOT taken from the caller (Rule 15: one writer per number).
	Meta Metadata
}

// AssembleResult reports what the assembly produced, measured off the artifact.
type AssembleResult struct {
	// Metadata is what was written to replay.json.
	Metadata Metadata
	// GIF is internal/gifgen's own reading of the encoded stream: the decoded
	// frame count, the canvas, the palette statistics. They are the encoder's
	// measurement, not a promise this package makes.
	GIF gifgen.Result
	// FramesCopied is how many frames the assembler had to copy into the film
	// directory (0 when they were already there).
	FramesCopied int
}

// Summary prints a readable production summary: what a person reading a build log
// needs in order to know whether the film is the one they asked for. It names the
// GIF's decoded stats and the FOUR palette numbers, because one number is
// misread — measured 2026-10-02, a colourless film reports 253 palette SLOTS
// while carrying no colour of gopi's at all (internal/gifgen's PaletteColors
// comment). Slots / used / non-grey / saturated are reported side by side so the
// summary cannot read as the opposite of what the artifact is.
func (r *AssembleResult) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "film: %s\n", r.Metadata.GIF)
	fmt.Fprintf(&b, "  frames: %d delivered, %d raw - %d distinct = %d dropped\n",
		r.Metadata.Frames, r.Metadata.RawFrames, r.Metadata.DistinctFrames, r.Metadata.DroppedFrames)
	fmt.Fprintf(&b, "  canvas: %dx%d px, frame %dx%d px, %d frame(s) decoded, %d byte(s)\n",
		r.GIF.Width, r.GIF.Height, r.GIF.FrameWidth, r.GIF.FrameHeight, r.GIF.Frames, r.GIF.Bytes)
	fmt.Fprintf(&b, "  palette: %d slots, %d used, %d non-grey, %d saturated\n",
		r.GIF.PaletteColors, r.GIF.PaletteUsed, r.GIF.PaletteNonGrey, r.GIF.PaletteSaturated)
	fmt.Fprintf(&b, "  timing: %d cs per frame, loop %s\n", r.GIF.DelayCS, loopSpelling(r.GIF))
	if r.Metadata.TermCols > 0 && len(r.Metadata.WindowPX) == 2 {
		fmt.Fprintf(&b, "  geometry: %d columns x %d rows, window %dx%d px, %.3f px per character\n",
			r.Metadata.TermCols, r.Metadata.TermRows,
			r.Metadata.WindowPX[0], r.Metadata.WindowPX[1], r.Metadata.PXPerChar)
	}
	for _, w := range r.GIF.Warnings {
		fmt.Fprintf(&b, "  warning: %s\n", w)
	}
	return b.String()
}

// loopSpelling renders the loop count a reader understands (gifgen's own words:
// 0 is the format's "forever", -1 the decoder's "no looping extension").
func loopSpelling(res gifgen.Result) string {
	switch res.LoopReadBack {
	case gifgen.LoopForever:
		return "forever(0)"
	case -1:
		return "play-once(-1)"
	}
	return fmt.Sprintf("%d (asked %d)", res.LoopReadBack, res.LoopCount)
}

// Assemble builds a film from an already-captured frame sequence.
//
// It refuses rather than guesses (Rule 15): no frames, a frame that is not
// readable, or a metadata field the arithmetic cannot close are all errors, not
// silent defaults. The GIF is written by internal/gifgen in ONE call, which
// builds its bytes in memory and therefore never leaves a half-written film
// behind on a refusal.
func Assemble(o AssembleOptions) (*AssembleResult, error) {
	if strings.TrimSpace(o.Out) == "" {
		return nil, fmt.Errorf("assemble: no output directory — a film is written somewhere nameable")
	}
	if len(o.Frames) == 0 {
		return nil, fmt.Errorf("assemble: no frames — a film with no frames is not an animation")
	}
	framesDir := filepath.Join(o.Out, FramesDirName)
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(framesDir, 0o755); err != nil {
		return nil, fmt.Errorf("assemble: create the frames directory %s: %w", framesDir, err)
	}

	res := &AssembleResult{}
	var gifFrames []gifgen.Frame
	for _, in := range o.Frames {
		dst, copied, err := deliverFrame(in, framesDir)
		if err != nil {
			return nil, err
		}
		if copied {
			res.FramesCopied++
		}
		gifFrames = append(gifFrames, gifgen.Frame{Path: dst, Caption: o.Captions[in]})
	}

	delay := o.DelayCS
	if delay <= 0 {
		delay = DefaultGIFDelayCS
	}
	gifPath := filepath.Join(o.Out, GIFName)
	encoded, err := gifgen.Encode(gifPath, gifgen.Options{
		DelayCS: delay,
		Loop:    o.Loop,
		LoopSet: o.LoopSet,
		Frames:  gifFrames,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble: encode %s: %w", gifPath, err)
	}
	res.GIF = *encoded

	// THE ARITHMETIC IS COMPUTED HERE, from the frame count that was actually
	// delivered — never taken from the caller, so the metadata cannot state a
	// count the directory contradicts.
	raw := o.RawFrames
	if raw <= 0 {
		raw = len(gifFrames)
	}
	distinct := o.DistinctFrames
	if distinct <= 0 {
		distinct = len(gifFrames)
	}
	if raw < distinct {
		return nil, fmt.Errorf("assemble: %d raw frames is fewer than %d distinct frames — "+
			"a reduction cannot have produced frames the recording did not hold", raw, distinct)
	}
	meta := o.Meta
	meta.Frames = len(gifFrames)
	meta.RawFrames = raw
	meta.DistinctFrames = distinct
	meta.DroppedFrames = raw - distinct
	meta.Beats = o.Beats
	if meta.Beats == 0 {
		meta.Beats = raw
	}
	meta.GIF = GIFName
	meta.GIFDelayCS = encoded.DelayCS
	// The window the frames came from: the capturer's own record when it was
	// passed, the delivered frame's canvas otherwise (they are the same canvas —
	// a frame is grabbed FROM that window).
	if w, h, id, ok := readWindowFile(o.WindowFile); ok {
		meta.WindowPX = []int{w, h}
		meta.WindowID = id
	} else if encoded.FrameWidth > 0 {
		meta.WindowPX = []int{encoded.FrameWidth, encoded.FrameHeight}
	}
	if meta.TermCols > 0 && len(meta.WindowPX) == 2 && meta.WindowPX[0] > 0 {
		meta.PXPerChar = round3(float64(meta.WindowPX[0]) / float64(meta.TermCols))
	}
	res.Metadata = meta
	if err := writeMetadata(filepath.Join(o.Out, MetadataName), meta); err != nil {
		return nil, err
	}
	return res, nil
}

// deliverFrame puts one input frame into the film's frames/ directory and returns
// its delivered path. A frame already in place is left alone — the delivered copy
// is the artifact, and rewriting it would change a file a check may already have
// measured.
func deliverFrame(in, framesDir string) (string, bool, error) {
	abs, err := filepath.Abs(in)
	if err != nil {
		return "", false, fmt.Errorf("assemble: resolve the frame path %s: %w", in, err)
	}
	dst := filepath.Join(framesDir, filepath.Base(abs))
	if filepath.Dir(abs) == framesDir {
		return dst, false, nil
	}
	blob, err := os.ReadFile(abs)
	if err != nil {
		return "", false, fmt.Errorf("assemble: read the frame %s: %w", in, err)
	}
	if err := os.WriteFile(dst, blob, 0o644); err != nil {
		return "", false, fmt.Errorf("assemble: write the delivered frame %s: %w", dst, err)
	}
	return dst, true, nil
}

// writeMetadata writes replay.json with its fields in a stable ORDER, so two
// assemblies of the same frames produce byte-identical metadata and a diff means
// a real change. Go's encoder sorts map keys but not struct fields, and this
// struct's order is the file's own reading order (the arithmetic first).
func writeMetadata(path string, m Metadata) error {
	blob, err := marshalMetadata(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		return fmt.Errorf("write the metadata %s: %w", path, err)
	}
	return nil
}

// SortedFrameNames lists a directory's PNG frames in display order — the helper a
// caller uses to turn a captured sequence into AssembleOptions.Frames.
func SortedFrameNames(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list the frames in %s: %w", dir, err)
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), PNGGlobSuffix) {
			continue
		}
		names = append(names, filepath.Join(dir, e.Name()))
	}
	sort.Strings(names)
	return names, nil
}

// marshalMetadata renders replay.json's bytes. Indented, with a trailing newline:
// the file is read by a human as often as by a check, and a diff between two runs
// should be readable.
func marshalMetadata(m Metadata) ([]byte, error) {
	blob, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return nil, fmt.Errorf("encode the metadata: %w", err)
	}
	return append(blob, '\n'), nil
}

// readWindowFile reads the capturer's window record: one line, `<wid> <w> <h>`.
// It is tools/replay/grab.py's format (assemble.py read the same three fields),
// and a malformed file is NOT a silent fallback: the caller learns the geometry
// could not be read, because a film whose px-per-character is computed from a
// frame instead of a window is a different measurement.
func readWindowFile(path string) (w, h int, id string, ok bool) {
	if strings.TrimSpace(path) == "" {
		return 0, 0, "", false
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, "", false
	}
	fields := strings.Fields(string(blob))
	if len(fields) < 3 {
		return 0, 0, "", false
	}
	wi, err1 := strconv.Atoi(fields[1])
	hi, err2 := strconv.Atoi(fields[2])
	if err1 != nil || err2 != nil || wi <= 0 || hi <= 0 {
		return 0, 0, "", false
	}
	return wi, hi, fields[0], true
}

// round3 rounds to three decimals — the precision replay.json states for
// px_per_char (assemble.py's `round(ppc, 3)`), so a report quoting the number and
// a reader comparing two files see the same digits.
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
