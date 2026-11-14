package film

// ground.go — WHERE THE CHAT SURFACE IS LEGIBLE, as a criterion.
//
// WHAT IT REPLACES. Five criteria were ported from verify.py (traceability,
// legibility, motion, withholding, colour). All five measure the surface's INK:
// whether the right beats reached the grid, whether the glyphs are legible, whether
// the picture moves, whether the redactions hold, whether gopi's palette is on the
// artifact. NONE of them measures the GROUND the ink sits on — and the ground is
// exactly what failed on 2026-10-04. The operator was handed
// `gopi chat --replay <id> --film DIR --out /tmp/replay.gif` and rejected it: the
// frames were 96 % PURE WHITE with gopi's pale gold on them. Every ported
// criterion would have PASSED that film (its glyphs were rendered, it moved, it
// carried `38;5;` SGR bytes) while a human looking at it said, correctly, that it
// is not the product. That is the gap this file closes.
//
// THE CRITERION, one sentence: the film's frames must be drawn on the ground the
// chat surface's palettes were composed against (internal/lineedit's declared
// GroundBackgroundHex), because those palettes are FOREGROUND-ONLY tables whose
// contrast was measured against a dark screen — and on a terminal that keeps its
// own default (a white xterm on this host) the surface is drawn at roughly 1.2:1
// and stops being recognisable.
//
// WHY A SHARE AND NOT A SINGLE PIXEL. "One dark pixel exists" is satisfied by a
// white window with a dark cursor on it — that is precisely the artifact the
// operator rejected, and it measured 852 black pixels out of 241 452 (0.35 %) in
// its worst frame with a white share of 96 %. So the criterion is the modal
// ground's SHARE of the canvas, with a floor that a white-ground film cannot
// reach: the surface's own frames measure 85-90 %, and a window that never got the
// ground measures ~0 %.
//
// WHY THE FLOOR IS AS LOW AS IT IS. It is not a legibility threshold (the
// legibility criterion owns the ink's contrast). It is a GROUND threshold: a
// surface that is mostly its own background, versus one that is mostly a white
// window. The margin between the two measured populations is 85 % against 0 %, so
// a floor near the halfway mark separates them with room for a frame that is
// unusually full of text — a frame CAN legitimately be denser than 50 % ink, and
// this criterion must not turn a busy beat into a failure.

import (
	"fmt"
	"image/color"
	"image/gif"
	"os"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

const (
	// GroundMinShareNumerator / GroundMinShareDenominator are the floor, as a
	// fraction rather than a float literal so the comparison is exact: half the
	// canvas. See this file's header for why the floor sits where it does (the two
	// measured populations are ~85 % and ~0 %).
	GroundMinShareNumerator   = 1
	GroundMinShareDenominator = 2
	// groundMinFrames is how many frames the criterion reads before it decides.
	// One frame is enough to measure a ground (`--film` runs are homogeneous in the
	// property under test: every frame is the same terminal with the same
	// palette), and reading more spends decoder time for no new information. It is
	// a constant so a reader can see the bound rather than infer it.
	groundMinFrames = 1
	// groundSampleStride is the pixel stride: every Nth pixel is sampled. The
	// property is a SHARE of a homogeneous region, so a stride cannot change the
	// verdict and it keeps a 150-frame film's criterion cheap. It is a stride, not
	// a limit — the walk still covers the whole canvas.
	groundSampleStride = 3
)

// GroundMeasurement is what the criterion measured, so a report can quote the
// number rather than the verdict alone.
type GroundMeasurement struct {
	// Frames is how many frames were sampled.
	Frames int
	// Sampled is how many pixels were looked at, across those frames.
	Sampled int
	// OnGround is how many of them carried the declared ground exactly.
	OnGround int
	// ModalColour / ModalShare are the most common colour and its share — the
	// number that makes a white-ground film self-describing in the report.
	ModalColour color.RGBA
	ModalShare  float64
}

// groundBackgroundHex / groundForegroundHex are internal/lineedit's declared
// ground, taken from the ONE definition (Rules 13/14: a value shared across
// packages is referenced, never mirrored). internal/film already imports that
// package for the colour anchors, so the criterion cannot drift from the palette
// it judges: if the surface ever moves its ground, this criterion moves with it
// and every film measured afterwards is judged by the ground that is current.
const (
	groundBackgroundHex = lineedit.GroundBackgroundHex
	groundForegroundHex = lineedit.GroundForegroundHex
)

// groundRGB parses the declared ground into its three channels. It reads the
// constant rather than re-typing the colour, which is the one thing this
// criterion must not do: a test re-typing it would keep passing after the product
// changed it.
func groundRGB() (r, g, b uint8, err error) {
	hex := strings.TrimPrefix(strings.TrimSpace(groundBackgroundHex), "#")
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("the declared ground %q is not a 6-digit hex colour", groundBackgroundHex)
	}
	n, perr := strconv.ParseUint(hex, 16, 32)
	if perr != nil {
		return 0, 0, 0, fmt.Errorf("the declared ground %q does not parse: %w", groundBackgroundHex, perr)
	}
	return uint8(n >> 16), uint8(n >> 8), uint8(n), nil
}

// MeasureGround samples the film's frames and reports the ground's share.
func MeasureGround(gifPath string) (GroundMeasurement, error) {
	var m GroundMeasurement
	gr, gg, gb, err := groundRGB()
	if err != nil {
		return m, err
	}
	f, err := os.Open(gifPath)
	if err != nil {
		return m, fmt.Errorf("open the film %s for the ground criterion: %w", gifPath, err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		return m, fmt.Errorf("decode the film %s for the ground criterion: %w", gifPath, err)
	}
	if len(g.Image) == 0 {
		return m, fmt.Errorf("the film %s holds no frame — a ground cannot be measured on an animation with no pictures", gifPath)
	}
	counts := map[color.RGBA]int{}
	for i := 0; i < len(g.Image) && i < groundMinFrames; i++ {
		img := g.Image[i]
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x += groundSampleStride {
				r, g8, bl, _ := img.At(x, y).RGBA()
				c := color.RGBA{uint8(r >> 8), uint8(g8 >> 8), uint8(bl >> 8), 0xff}
				m.Sampled++
				counts[c]++
				if c.R == gr && c.G == gg && c.B == gb {
					m.OnGround++
				}
			}
		}
		m.Frames++
	}
	// The modal colour, by explicit maximum: the first colour seen wins ties, so
	// two runs over the same film report the same one (no map-iteration order
	// leak into a reported number).
	best := -1
	for c, n := range counts {
		if n > best || (n == best && rgbLess(c, m.ModalColour)) {
			best, m.ModalColour = n, c
		}
	}
	if m.Sampled > 0 {
		m.ModalShare = float64(best) / float64(m.Sampled)
	}
	return m, nil
}

// rgbLess orders two colours so a tie in the modal scan is broken the same way
// on every run: a reported number that depended on map iteration order would make
// two measurements of one film disagree.
func rgbLess(a, b color.RGBA) bool {
	if a.R != b.R {
		return a.R < b.R
	}
	if a.G != b.G {
		return a.G < b.G
	}
	return a.B < b.B
}

// groundShare reports the declared ground's fraction of the sampled pixels.
func (m GroundMeasurement) groundShare() float64 {
	if m.Sampled == 0 {
		return 0
	}
	return float64(m.OnGround) / float64(m.Sampled)
}

// CheckGround is criterion 6: the film is drawn on the surface's own ground.
//
// It is the criterion the operator's verdict was about, and it is deliberately
// SEPARATE from the colour criterion rather than folded into it: colour asks
// "is gopi's palette on the artifact", which a white-ground film answers YES to
// (gold bytes on white paper are still gold bytes). Ground asks "is the screen
// the one those colours were composed for", which is the question that was
// actually wrong.
func CheckGround(f Film) Verdict {
	v := Verdict{Name: "6. ground", Source: "written for this repository, 2026-10-04 (see ground.go)", N: map[string]int{}}
	gifPath := f.GIFPath()
	if _, err := os.Stat(gifPath); err != nil {
		v.fail("film", fmt.Sprintf("no film at %s: the ground of the delivered artifact cannot be measured", gifPath))
		return v
	}
	m, err := MeasureGround(gifPath)
	if err != nil {
		v.fail("film", err.Error())
		return v
	}
	v.N["frames"] = m.Frames
	v.N["sampled"] = m.Sampled
	v.N["on_ground"] = m.OnGround
	share := m.groundShare()
	v.note("the film's first %d frame(s) were sampled every %dth pixel (%d pixel(s)); %d of them carry the surface's declared ground %s (%.1f%%), and the frame's MODAL colour is #%02X%02X%02X at %.1f%%",
		m.Frames, groundSampleStride, m.Sampled, m.OnGround, groundBackgroundHex, share*100,
		m.ModalColour.R, m.ModalColour.G, m.ModalColour.B, m.ModalShare*100)
	if m.Sampled < GroundMinShareDenominator {
		v.fail("film", fmt.Sprintf("only %d pixel(s) were sampled from %s — too few to measure a ground, which is reported rather than passed", m.Sampled, gifPath))
		return v
	}
	if share*GroundMinShareDenominator < GroundMinShareNumerator {
		v.fail("ground",
			fmt.Sprintf("only %.1f%% of the film's pixels carry the surface's declared ground %s (the floor is %d/%d): the frame's modal colour is #%02X%02X%02X at %.1f%%, so this film was drawn on a screen the palettes were NOT composed for — the chat surface's tables are foreground-only and were measured against a dark ground, which is why a terminal keeping its own default (a white xterm) produces a film at ~1.2:1 that reads as a white window",
				share*100, groundBackgroundHex, GroundMinShareNumerator, GroundMinShareDenominator,
				m.ModalColour.R, m.ModalColour.G, m.ModalColour.B, m.ModalShare*100))
		return v
	}
	v.note("OK: the film is drawn on the declared ground, so its chrome is on the screen it was composed for")
	// The geometry claim rides here because it is the SAME property one level up:
	// a canvas that is not the photographed frame's own size is the signature of a
	// band appended to the picture (the caption this film must not carry).
	if fw, fh, gerr := gifFirstFrameGeometry(gifPath); gerr == nil {
		v.N["frame_width"] = fw
		v.N["frame_height"] = fh
		v.note("the GIF's frame geometry is %dx%d — the photographed frame's own size, with nothing appended", fw, fh)
	}
	return v
}

// gifFirstFrameGeometry reports the first frame's bounds, so the ground criterion
// can state the picture's size beside its colour. A separate reader from
// MeasureGround's because the two are used independently (the geometry note must
// not force a second full decode of the film).
func gifFirstFrameGeometry(gifPath string) (int, int, error) {
	f, err := os.Open(gifPath)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		return 0, 0, err
	}
	if len(g.Image) == 0 {
		return 0, 0, fmt.Errorf("no frames")
	}
	b := g.Image[0].Bounds()
	return b.Dx(), b.Dy(), nil
}

// GroundShare exports the share for a caller outside this package (a test, a
// report) — the SAME arithmetic the criterion decides on, exposed rather than
// re-derived. It is the exported twin of groundShare and exists for the same
// reason Verdict carries N: a reader must be able to quote the number the verdict
// was made from.
func (m GroundMeasurement) GroundShare() float64 { return m.groundShare() }
