package lineedit

// palette_answer_hex_test.go — the regression guard for the derivation the
// `gopi chat -h` page depends on.
//
// WHY IT EXISTS (task 01a169bb, the palette knob made discoverable). The help
// page names the gold answer body's colour, and it must DERIVE that spelling
// from the table (Rules 1/13) rather than re-type it: a restated colour is a
// second definition that no test can keep in step. The derivation is only worth
// having if the two halves really are one colour, so this test states the
// invariant the page relies on — the hex spelling and the WIRE sequence
// HermesDarkPalette.Answer emits name the same 24-bit value. Change the table's
// packed constant and this test follows it; hard-code a colour somewhere in the
// chain and it goes red, naming both sides.

import (
	"strings"
	"testing"

	"strconv"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// TestPaletteAnswerHexIsTheTablesOwnColour pins both directions for both gold
// tables: the documented spelling parses back to the channels the palette's own
// Answer style emits.
func TestPaletteAnswerHexIsTheTablesOwnColour(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hex   string
		style Style
	}{
		{"dark", HermesDarkAnswerHex(), HermesDarkPalette.Answer},
		{"light", HermesLightAnswerHex(), HermesLightPalette.Answer},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, g, b, ok := parsePaletteHex(t, tc.hex)
			if !ok {
				return
			}
			// The one value the derivation must carry: what the table paints.
			want := sgr.TruecolorFgParams(r, g, b)
			open := tc.style.Open()
			if !strings.Contains(open, want) {
				t.Fatalf("the %s table's Answer style opens with %q, which does not select 38;2 from the "+
					"colour its own hex spelling %q names (%q) — the help page and the rendered body "+
					"would advertise two different colours", tc.name, open, tc.hex, want)
			}
		})
	}
}

// TestHermesDarkAnswerHexSpellingIsTheOperatorForm keeps the SPELLING an
// operator reads stable: `#` plus six upper-case digits. It is the shape the
// `gopi chat -h` paragraph prints, so a lower-case or short form would be a
// silent edit of the published page.
func TestHermesDarkAnswerHexSpellingIsTheOperatorForm(t *testing.T) {
	got := HermesDarkAnswerHex()
	if len(got) != len("#RRGGBB") || got[0] != '#' {
		t.Fatalf("HermesDarkAnswerHex() = %q, want a #RRGGBB spelling", got)
	}
	if strings.ToUpper(got) != got {
		t.Fatalf("HermesDarkAnswerHex() = %q, want upper-case digits (the form the help page prints)", got)
	}
}

// parsePaletteHex turns "#RRGGBB" into its three channels. It reports ok=false
// (after failing the test) on anything else, so a malformed spelling is a
// failure of THIS test rather than a silently skipped comparison.
func parsePaletteHex(t *testing.T, hex string) (r, g, b int, ok bool) {
	t.Helper()
	if len(hex) != len("#RRGGBB") || hex[0] != '#' {
		t.Fatalf("%q is not a #RRGGBB spelling", hex)
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		t.Fatalf("%q carries non-hex digits after the '#': %v", hex, err)
		return 0, 0, 0, false
	}
	rgb := int(v)
	return rgb >> rgbRedShift & 0xFF, rgb >> rgbGreenShift & 0xFF, rgb & 0xFF, true
}
