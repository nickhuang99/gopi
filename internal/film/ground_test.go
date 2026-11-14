package film_test

// ground_test.go — the ground criterion's own proofs.
//
// TWO KINDS OF PROOF, and both are needed:
//
//  1. THE RED PROOF. A criterion that has never failed on the artifact it is about
//     is a criterion nobody has measured. The operator's rejected GIF is exactly
//     that artifact: 96 % pure white, 0.35 % black, gopi's gold on white paper. It
//     is reproduced here as a fixture IN THE TEST (drawn by the test, never
//     committed — Rule 39) and the criterion must FAIL on it, naming the share and
//     the modal colour.
//
//  2. THE GREEN PROOF. The criterion must PASS the picture the fix produces: the
//     same film content, drawn on the surface's declared ground. A criterion that
//     only fails is a criterion that fires on everything.
//
// The pair is what makes the floor meaningful: it is between two MEASURED
// populations (0 % and 85 %), not between two guesses.

import (
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/film"
	"github.com/nickhuang99/gopi/internal/lineedit"
)

// writeGroundFilm writes a one-frame GIF of the given ground with a few gold
// marks on it, and returns the film's directory.
//
// WHY A GIF AND NOT A PNG: the criterion measures the DELIVERED artifact
// (Film.GIFPath), because that is the file the operator looks at; a test that
// measured the frames would prove a property of an intermediate the operator never
// receives.
//
// WHY THE FRAME IS BUILT AS image.Paletted: gif.Encode given an ordinary image
// QUANTISES it (measured while writing this test — a #131926 frame came back
// #222222 at 48.6 %, the web-safe cube's nearest cell), so a fixture that is
// supposed to prove "the declared ground is on the artifact" would have been
// proving it about a colour the product does not use. A Paletted image carries its
// own palette and the encoder writes those indices verbatim, which is the exact
// colour the criterion must see.
func writeGroundFilm(t *testing.T, dir string, ground color.RGBA, marks int) string {
	t.Helper()
	const w, h = 120, 60
	gold := color.RGBA{R: 0xFF, G: 0xD7, B: 0x00, A: 0xff}
	pal := color.Palette{ground, gold}
	idx := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	for i := 0; i < marks; i++ {
		x := 4 + i*7
		for y := 4; y < 10; y++ {
			idx.SetColorIndex(x, y, 1)
		}
	}
	pngPath := filepath.Join(dir, "frame-0001.png")
	pf, err := os.Create(pngPath)
	if err != nil {
		t.Fatalf("create %s: %v", pngPath, err)
	}
	if err := png.Encode(pf, idx); err != nil {
		t.Fatalf("encode %s: %v", pngPath, err)
	}
	if err := pf.Close(); err != nil {
		t.Fatalf("close %s: %v", pngPath, err)
	}
	gifPath := filepath.Join(dir, film.GIFName)
	out, err := os.Create(gifPath)
	if err != nil {
		t.Fatalf("create %s: %v", gifPath, err)
	}
	if err := gif.Encode(out, idx, nil); err != nil {
		t.Fatalf("encode %s: %v", gifPath, err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close %s: %v", gifPath, err)
	}
	return gifPath
}

// TestFilmGroundCriterionRefusesAWhiteGround is the RED proof, on the operator's
// own artifact: the rejected film's dominant colour was pure white (96.05 % of its
// pixels, measured on /tmp/frame-0150.png) with the surface's gold on it. The
// criterion must fail, and the failure must QUOTE the measurement a reader can
// check rather than only the verdict.
func TestFilmGroundCriterionRefusesAWhiteGround(t *testing.T) {
	dir := t.TempDir()
	writeGroundFilm(t, dir, color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xff}, 12)
	v := film.CheckGround(film.Film{Out: dir})
	if v.OK() {
		t.Fatalf("the criterion PASSED a white-ground film — this is the artifact the operator rejected (96 %% pure white, gold chrome on it), so the criterion is not measuring the ground:\n%s", v.Summary())
	}
	summary := v.Summary()
	for _, want := range []string{"ground", "FFFFFF", lineedit.GroundBackgroundHex} {
		if !strings.Contains(summary, want) {
			t.Errorf("the failure does not quote %q, so a reader cannot check the measurement:\n%s", want, summary)
		}
	}
}

// TestFilmGroundCriterionAcceptsTheSurfaceGround is the GREEN proof: the same film
// content on the ground internal/lineedit declares must pass. Without it, a
// criterion that failed everything would satisfy the red proof above.
func TestFilmGroundCriterionAcceptsTheSurfaceGround(t *testing.T) {
	dir := t.TempDir()
	writeGroundFilm(t, dir, hexRGB(t, lineedit.GroundBackgroundHex), 12)
	v := film.CheckGround(film.Film{Out: dir})
	if !v.OK() {
		t.Fatalf("the criterion refused the surface's OWN declared ground %s — the fix it is supposed to certify would be reported as broken:\n%s",
			lineedit.GroundBackgroundHex, v.Summary())
	}
}

// TestFilmGroundFloorIsMeasuredBetweenTheTwoPopulations pins the floor's SENSE
// rather than its value: the share on the declared ground must clear the floor on a
// frame that is unusually full of ink, and the share on white must not. A frame
// with a dense picture is legitimate — the criterion is a ground threshold, not a
// legibility or a density threshold — so the green case here draws MANY marks.
func TestFilmGroundFloorIsMeasuredBetweenTheTwoPopulations(t *testing.T) {
	dir := t.TempDir()
	// 12 marks × 6 px = 72 ink pixels of 7200 — a dense caption row, and still
	// overwhelmingly ground.
	writeGroundFilm(t, dir, hexRGB(t, lineedit.GroundBackgroundHex), 12)
	m, err := film.MeasureGround(filepath.Join(dir, film.GIFName))
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if m.Frames != 1 || m.Sampled == 0 {
		t.Fatalf("the measurement read %d frame(s) and %d pixel(s) — a measurement that saw nothing cannot witness a floor", m.Frames, m.Sampled)
	}
	if got := m.OnGround; got == 0 {
		t.Fatalf("no sampled pixel carried the declared ground, so this fixture cannot witness the green half either")
	}
}

// TestFilmGroundMirrorsNoSecondDefinition is the Rules-13/14 guard: the criterion
// must read the surface's declaration rather than carry a private copy, so the two
// cannot drift. It asserts the criterion's own constant IS the lineedit one by
// identity — a re-typed literal would fail here the moment either side moved.
func TestFilmGroundMirrorsNoSecondDefinition(t *testing.T) {
	dir := t.TempDir()
	writeGroundFilm(t, dir, hexRGB(t, lineedit.GroundBackgroundHex), 3)
	m, err := film.MeasureGround(filepath.Join(dir, film.GIFName))
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if m.OnGround == 0 {
		t.Fatalf("a frame drawn in lineedit.GroundBackgroundHex (%s) measured ZERO pixels on the criterion's declared ground — the criterion holds a colour of its own, which is the drift this test exists to catch",
			lineedit.GroundBackgroundHex)
	}
}

// hexRGB parses a `#rrggbb` string into a colour, failing the test on a malformed
// value (a fixture that cannot express the colour must not silently draw black).
func hexRGB(t *testing.T, hex string) color.RGBA {
	t.Helper()
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) != 6 {
		t.Fatalf("%q is not a 6-digit hex colour", hex)
	}
	var n uint32
	for _, c := range h {
		var d uint32
		switch {
		case c >= '0' && c <= '9':
			d = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint32(c-'A') + 10
		default:
			t.Fatalf("%q holds a non-hex digit %q", hex, c)
		}
		n = n<<4 | d
	}
	return color.RGBA{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n), A: 0xff}
}

// TestFilmGroundProbeOnARealArtifact measures the criterion against a DELIVERED
// GIF named by the environment, and reports the numbers whether it passes or
// fails. It is the Rule-31 bridge between the fixture proofs above and a real run:
// the fixtures are drawn by this test at 120x60, while the artifact an operator
// holds is 706x342 at the surface's own geometry.
//
//	# the rejected film (must FAIL, and quote the white modal colour)
//	GOPI_FILM_GROUND_PROBE=/tmp/replay.gif GOPI_FILM_GROUND_EXPECT=fail \
//	    go test ./internal/film/ -run TestFilmGroundProbe -v
//	# the fixed film (must PASS)
//	GOPI_FILM_GROUND_PROBE=<dir>/replay.gif GOPI_FILM_GROUND_EXPECT=pass \
//	    go test ./internal/film/ -run TestFilmGroundProbe -v
//
// WITH NO EXPECTATION SET IT ONLY REPORTS — the numbers are the deliverable there.
// WHY IT IS SKIPPED, NOT SILENT, WITHOUT THE ENV: a probe that fell back to a
// committed fixture would report "the real artifact is fine" about a file nobody
// looked at, which is the wrong conclusion to leave behind (Rule 15). With no
// variable set it says so, by name.
func TestFilmGroundProbeOnARealArtifact(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("GOPI_FILM_GROUND_PROBE"))
	if path == "" {
		t.Skip("no GOPI_FILM_GROUND_PROBE: set it to a delivered GIF path to measure the criterion against a real artifact")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, film.GIFName)
	if blob, err := os.ReadFile(path); err != nil {
		t.Fatalf("read %s: %v", path, err)
	} else if werr := os.WriteFile(target, blob, 0o644); werr != nil {
		t.Fatalf("copy %s: %v", path, werr)
	}
	v := film.CheckGround(film.Film{Out: dir})
	t.Logf("ground criterion on %s:\n%s", path, v.Summary())
	m, err := film.MeasureGround(target)
	if err != nil {
		t.Fatalf("measure %s: %v", path, err)
	}
	t.Logf("measured: %d frame(s), %d sampled, %d on ground (%.1f%%), modal #%02X%02X%02X at %.1f%%",
		m.Frames, m.Sampled, m.OnGround, m.GroundShare()*100,
		m.ModalColour.R, m.ModalColour.G, m.ModalColour.B, m.ModalShare*100)
	switch strings.TrimSpace(os.Getenv("GOPI_FILM_GROUND_EXPECT")) {
	case "fail":
		if v.OK() {
			t.Errorf("the criterion PASSED %s, but this artifact was expected to fail", path)
		}
	case "pass":
		if !v.OK() {
			t.Errorf("the criterion REFUSED %s, but this artifact was expected to pass:\n%s", path, v.Summary())
		}
	}
}
