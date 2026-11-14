package film_test

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/film"
)

// TestPaletteAnchorsMatchThePythonTable PINS the derived anchors against the
// literal table verify.py carried.
//
// WHY THIS TEST EXISTS. The port resolves the colour claim's anchors out of
// internal/lineedit's Palette256 through the xterm cube's arithmetic, instead of
// copying verify.py's four literal sRGB values (`PALETTE_RGB`, L126-L137). That
// is a claim — "the derived anchors ARE the ones the Python compared against" —
// and a claim about a substitution is exactly the kind that rots silently, so it
// is measured here rather than asserted in a comment.
//
// SCOPE, stated so a reader is not misled: verify.py's table has FOUR entries and
// the derived set has FOUR roles, but they are not the same four. The Python's
// second entry, cornsilk `#FFF8DC` `(255,248,220)`, is the 24-BIT tier's answer row
// — this film is drawn by the 256-colour tier (its own metadata's terminal
// declared 256 colours, and that is the tier whose SGR the frames carry), so the
// 256 tier's body colour is the anchor that matters. The three entries both
// tables carry are pinned below by value; the fourth role this port adds (`tool
// activity row`) has no Python twin, because the Python did not anchor it.
func TestPaletteAnchorsMatchThePythonTable(t *testing.T) {
	roles, err := film.PaletteRoles()
	if err != nil {
		t.Fatalf("derive the palette anchors: %v", err)
	}
	// verify.py L126-L137, verbatim: (name fragment, sRGB).
	want := []struct {
		role string
		rgb  [3]int
		note string
	}{
		{"answer body", [3]int{255, 255, 215}, "verify.py's 'colour-230 cubeGoldBody (#FFFFD7)'"},
		{"answer emphasis", [3]int{215, 175, 0}, "verify.py's 'colour-178 cubeDeepGold (#D7AF00)'"},
		{"editing line / buffer", [3]int{255, 215, 0}, "verify.py's 'colour-220 (#FFD700)'"},
	}
	byName := map[string][3]int{}
	for _, r := range roles {
		byName[r.Name] = r.RGB
	}
	for _, w := range want {
		got, ok := byName[w.role]
		if !ok {
			t.Errorf("the derived anchors hold no role named %q — the anchor set changed shape, so the check's names no longer reach verify.py's table", w.role)
			continue
		}
		if got != w.rgb {
			t.Errorf("anchor %q derived as %v, the Python table says %v (%s): the cube arithmetic or the palette moved, and the colour claim is now measured against a colour gopi does not paint",
				w.role, got, w.rgb, w.note)
		}
	}
	// The cube arithmetic itself, at the two indices the derivation is built on:
	// 220 → #FFD700 and 178 → #D7AF00 are the xterm standard's own values, quoted
	// by internal/lineedit's palette comment.
	if got := byName["editing line / buffer"]; got != [3]int{255, 215, 0} {
		t.Errorf("palette index 220 derived as %v, the xterm cube defines #FFD700", got)
	}
	if len(roles) == 0 {
		t.Error("no anchors derived: the colour claim would be vacuous")
	}
	t.Logf("derived anchors: %v", roles)
}
