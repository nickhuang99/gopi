package film

import (
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"os"
	"regexp"
	"strconv"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// The COLOUR claim's own machinery: the palette anchors, the saturation/brightness
// floors, and one pass per frame over the decoded film.
//
// WHY THE ANCHORS ARE DERIVED AND NOT TABLED. verify.py carried a literal table
// of four sRGB values with the names of the roles that paint them
// (`PALETTE_RGB`, L126-L137) because a Python checker could not read a Go table.
// The port can, so the anchors are DERIVED from internal/lineedit's Palette256 —
// the same table the surface draws with — through the xterm cube's own arithmetic
// (index → 6×6×6 level). That is a STRENGTHENING of the criterion, not a change
// to its claim: a literal table can drift from the palette it describes, and this
// one cannot, because it IS the palette. palette_test.go pins the derived values
// against verify.py's four literals, so the substitution is measured rather than
// asserted.

// Colour floors — verify.py's own names and values (L70-L71). A pixel is INK IN
// COLOUR only if it is saturated enough to carry a hue AND bright enough to be
// ink rather than background: this film's background is (19,25,38), blue-tinted
// but far too DARK to pass, which is what makes the claim measure the surface's
// palette rather than the terminal's wallpaper.
const (
	// ColourMinSat / ColourMinValue are verify.py's COLOUR_MIN_SAT /
	// COLOUR_MIN_VALUE. internal/gifgen carries the same pair for its palette
	// summary (its paletteSatMin/paletteValMin), so the assembly log and this
	// gate cannot disagree about what "coloured" means.
	ColourMinSat   = 0.40
	ColourMinValue = 0.50
	// HueBuckets is verify.py's HUE_BUCKETS (L84): the hue circle is cut into
	// this many buckets and the film must carry at least one.
	HueBuckets = 12
	// MinHueBuckets is verify.py's MIN_HUE_BUCKETS (L85): a grey film measures 0,
	// and gopi's chrome is deliberately ONE hue family, so the floor is 1.
	MinHueBuckets = 1
	// MinColouredPixels is verify.py's MIN_COLOURED_PIXELS (L114): generous by
	// orders of magnitude against the measured value, because the claim is
	// "colour exists at all", not "enough colour". A grey film measures 0.
	MinColouredPixels = 1000
	// PaletteRGBTolerance is verify.py's PALETTE_RGB_TOLERANCE (L126): it absorbs
	// the film's GIF quantisation and glyph antialiasing, and is far below the
	// distance between two distinct palette roles.
	PaletteRGBTolerance = 16
	// MinPaletteRoles is verify.py's MIN_PALETTE_ROLES (L110/L137): ONE role is
	// the floor and it is the right floor — which roles appear is a property of
	// the RECORDING (a session with no diff shows no red), while "at least one of
	// gopi's declared roles is on the artifact" is the property the epic is about.
	MinPaletteRoles = 1
)

// xtermCubeLevels are the six brightness levels of the xterm 256-colour cube
// (0,95,135,175,215,255), the palette's own arithmetic: index 16+n maps to
// (n/36, (n/6)%6, n%6) through this table.
var xtermCubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// paletteFirstCubeIndex is the cube's base index (0-15 are the ANSI set, 16-231
// the cube, 232-255 the grey ramp).
const paletteFirstCubeIndex = 16

// paletteCubeLast is the cube's last index.
const paletteCubeLast = 231

// sgrFgIndex extracts the foreground palette index from a Style's own SGR
// open sequence (`\x1b[38;5;230m`). It is the ONE place the palette's structure
// is read, and a failure to parse is reported to the caller rather than
// swallowed: an anchor nobody can resolve must make the claim fail loudly
// (Rule 15), never pass by default.
var sgrFgIndex = regexp.MustCompile(`38;5;(\d+)`)

// PaletteRole is one of gopi's declared colours, as the film would carry it: the
// role's name (the Style field it comes from) and its sRGB value derived from
// the palette index the surface paints with.
type PaletteRole struct {
	Name string
	RGB  [3]int
	// SGR is the sequence the role paints with — kept because it is the EVIDENCE
	// the RGB was derived from: a role that carries no `38;5;n` foreground cannot
	// be resolved through the cube, and a caller comparing two palettes needs the
	// bytes, not just the resolved colour.
	SGR string
}

// paletteRoleStyles is the set of roles the colour claim anchors on, named by the
// Palette256 field that paints each. The names are verify.py's own words for the
// same four roles (its table's comments), kept as the film's report vocabulary.
func paletteRoleStyles() []struct {
	Name  string
	Style lineedit.Style
} {
	return []struct {
		Name  string
		Style lineedit.Style
	}{
		{"answer body", lineedit.Palette256.Answer},
		{"answer emphasis", lineedit.Palette256.AnswerBold},
		{"editing line / buffer", lineedit.Palette256.Buffer},
		{"tool activity row", lineedit.Palette256.ToolCall},
	}
}

// PaletteRoles derives the anchor table from the palette the surface draws with.
func PaletteRoles() ([]PaletteRole, error) {
	var out []PaletteRole
	for _, r := range paletteRoleStyles() {
		rgb, err := cubeRGBFromStyle(r.Style)
		if err != nil {
			return nil, fmt.Errorf("palette anchor %q: %w", r.Name, err)
		}
		out = append(out, PaletteRole{Name: r.Name, RGB: rgb, SGR: r.Style.Open()})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("palette anchors: none derived — the colour claim would be vacuous")
	}
	return out, nil
}

// cubeRGBFromStyle resolves the sRGB value a 256-colour Style paints.
func cubeRGBFromStyle(s lineedit.Style) ([3]int, error) {
	m := sgrFgIndex.FindStringSubmatch(s.Open())
	if m == nil {
		return [3]int{}, fmt.Errorf("its SGR sequence %q carries no 256-colour foreground index", s.Open())
	}
	idx, err := strconv.Atoi(m[1])
	if err != nil {
		return [3]int{}, fmt.Errorf("parse the palette index %q: %w", m[1], err)
	}
	return cubeRGB(idx)
}

// cubeRGB is the xterm cube's own arithmetic (the same table internal/lineedit's
// comments state): 16..231 are the 6×6×6 cube.
func cubeRGB(idx int) ([3]int, error) {
	if idx < paletteFirstCubeIndex || idx > paletteCubeLast {
		return [3]int{}, fmt.Errorf("palette index %d is outside the 6x6x6 cube (%d..%d)", idx, paletteFirstCubeIndex, paletteCubeLast)
	}
	n := idx - paletteFirstCubeIndex
	return [3]int{xtermCubeLevels[n/36], xtermCubeLevels[(n/6)%6], xtermCubeLevels[n%6]}, nil
}

// ColourMeasurement is what ONE pass over the film measured: the saturated,
// bright ink it carries, by hue bucket, and which of gopi's roles are on it.
type ColourMeasurement struct {
	// TotalFrames is how many frames the GIF decodes to — the film, not a sample.
	TotalFrames int
	// Chromatic is how many pixels passed the saturation AND brightness floors.
	Chromatic int
	// Hues is the set of distinct hue buckets that ink occupies.
	Hues map[int]bool
	// Kinds is how many distinct RGB values that ink holds (reported, never
	// decided on — "非灰阶色种类数" does not discriminate, see the package doc).
	Kinds int
	// Roles are the declared palette roles the film carries, by name.
	Roles []string
	// SaturatedPaletteEntries / UsedPaletteEntries are the DECODED GIF's own
	// palette statistics — the numbers that cannot be misread the way a slot
	// count can (internal/gifgen's PaletteSaturated / PaletteUsed).
	SaturatedPaletteEntries int
	UsedPaletteEntries      int
	// PaletteEntries is the decoded palette's size in slots.
	PaletteEntries int
}

// MeasureColour decodes the film and measures every frame's saturated bright ink.
//
// THE MEASUREMENT IS OVER THE FILM, not over a sample of it: the GIF is the
// artifact a reader watches. verify.py did this with numpy because a per-pixel
// Python loop did not finish inside two minutes on 121 frames of 2225×1432.
// Go's decoder is fast enough for one pass over the frame's indexed bytes —
// measured at 6.4 s for the real 139-frame film (the naive per-pixel At() form
// measured 86 s; see the report's timing line) — which is why the substitute needs
// no arithmetic library and go.mod stays untouched (Rule 35).
func MeasureColour(gifPath string) (ColourMeasurement, error) {
	f, err := os.Open(gifPath)
	if err != nil {
		return ColourMeasurement{}, fmt.Errorf("open the film %s: %w", gifPath, err)
	}
	defer f.Close()
	anim, err := gif.DecodeAll(f)
	if err != nil {
		return ColourMeasurement{}, fmt.Errorf("decode the film %s: %w", gifPath, err)
	}
	m := ColourMeasurement{Hues: map[int]bool{}, TotalFrames: len(anim.Image)}
	if len(anim.Image) > 0 {
		m.PaletteEntries = len(anim.Image[0].Palette)
	}
	kinds := map[uint32]bool{}
	// ONE PASS PER FRAME, over the frame's INDEXED bytes: the palette entry's
	// saturation, brightness and hue are resolved ONCE per entry and the pixels are
	// then a table lookup. The naive form — frame.At(x,y).RGBA() per pixel — measured
	// 86 s on the real 139-frame film, because every call crosses an interface and
	// widens 8-bit channels to 16; this form is the same measurement in a fraction.
	usedEntries := map[int]bool{}
	satEntries := 0
	perFrameUsed := make([]bool, 0)
	for _, frame := range anim.Image {
		pal := frame.Palette
		if pal == nil {
			return ColourMeasurement{}, fmt.Errorf("the film %s decodes a frame with no palette: its colours cannot be resolved", gifPath)
		}
		if cap(perFrameUsed) < len(pal) {
			perFrameUsed = make([]bool, len(pal))
		}
		perFrameUsed = perFrameUsed[:len(pal)]
		for i := range perFrameUsed {
			perFrameUsed[i] = false
		}
		type entry struct {
			rgb [3]int
			sat float64
			val float64
			hue int
			col bool
		}
		table := make([]entry, len(pal))
		for i, c := range pal {
			r, g, b, _ := c.RGBA()
			r8, g8, b8 := int(r>>8), int(g>>8), int(b>>8)
			mx, mn := maxMin3(r8, g8, b8)
			v := float64(mx) / 255.0
			sat := 0.0
			if mx > 0 {
				sat = float64(mx-mn) / float64(mx)
			}
			e := entry{rgb: [3]int{r8, g8, b8}, sat: sat, val: v}
			e.col = sat >= ColourMinSat && v >= ColourMinValue
			if e.col {
				e.hue = hueBucket(r8, g8, b8, mx, mn)
			}
			table[i] = e
		}
		b := frame.Bounds()
		stride := frame.Stride
		for y := 0; y < b.Dy(); y++ {
			row := frame.Pix[y*stride : y*stride+b.Dx()]
			for _, idx := range row {
				if int(idx) >= len(table) {
					// An index past the palette is a corrupt stream the decoder
					// accepted: report it rather than reading past the table.
					return ColourMeasurement{}, fmt.Errorf("the film %s has a frame pixel indexing palette entry %d of %d", gifPath, idx, len(table))
				}
				perFrameUsed[idx] = true
				e := table[idx]
				if !e.col {
					continue
				}
				m.Chromatic++
				m.Hues[e.hue] = true
				kinds[uint32(e.rgb[0])<<16|uint32(e.rgb[1])<<8|uint32(e.rgb[2])] = true
			}
		}
		for i, u := range perFrameUsed {
			if !u {
				continue
			}
			usedEntries[i] = true
			if table[i].col {
				satEntries++
			}
		}
	}
	m.Kinds = len(kinds)
	m.UsedPaletteEntries = len(usedEntries)
	m.SaturatedPaletteEntries = satEntries
	roles, err := PaletteRoles()
	if err != nil {
		return ColourMeasurement{}, err
	}
	for _, role := range roles {
		for _, frame := range anim.Image {
			if paletteCarries(frame.Palette, role.RGB, PaletteRGBTolerance) {
				m.Roles = append(m.Roles, role.Name)
				break
			}
		}
	}
	return m, nil
}

// paletteCarries reports whether a frame's palette holds an entry within
// tolerance of want — the anchor test, on the table the GIF actually carries
// rather than on its pixels (a role is "on the film" when the film's own
// quantised table can paint it, which is the property the epic names).
func paletteCarries(pal color.Palette, want [3]int, tol int) bool {
	if pal == nil {
		return false
	}
	for _, c := range pal {
		r, g, b, _ := c.RGBA()
		if absInt(int(r>>8)-want[0]) <= tol && absInt(int(g>>8)-want[1]) <= tol && absInt(int(b>>8)-want[2]) <= tol {
			return true
		}
	}
	return false
}

// hueBucket is verify.py's `film_hues` hue arithmetic (L1472-L1532), in the
// six-sector HSV form: `deg = ((sector/6)*360) mod 360`, cut into HueBuckets.
func hueBucket(r, g, b, mx, mn int) int {
	delta := mx - mn
	if delta == 0 {
		return 0
	}
	fr, fg, fb := float64(r), float64(g), float64(b)
	fdelta := float64(delta)
	var hh float64
	switch {
	case mx == r:
		hh = mod6((fg - fb) / fdelta)
	case mx == g:
		hh = (fb-fr)/fdelta + 2
	default:
		hh = (fr-fg)/fdelta + 4
	}
	deg := (hh / 6.0 * 360.0)
	for deg < 0 {
		deg += 360
	}
	bucket := int(deg*HueBuckets/360.0) % HueBuckets
	if bucket < 0 {
		bucket += HueBuckets
	}
	return bucket
}

// mod6 is Python's `% 6` for a float.
func mod6(v float64) float64 {
	m := v - 6*float64(int(v/6))
	for m < 0 {
		m += 6
	}
	for m >= 6 {
		m -= 6
	}
	return m
}

func maxMin3(r, g, b int) (int, int) {
	mx, mn := r, r
	for _, v := range []int{g, b} {
		if v > mx {
			mx = v
		}
		if v < mn {
			mn = v
		}
	}
	return mx, mn
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// imageIsGrey reports whether an image carries no hue at all — the property the
// colour criterion's counterexample is built to have (and to lose).
func imageIsGrey(img image.Image) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 != g>>8 || g>>8 != bl>>8 {
				return false
			}
		}
	}
	return true
}
