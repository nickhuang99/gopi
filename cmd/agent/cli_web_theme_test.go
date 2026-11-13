// cli_web_theme_test.go — the PALETTE and the NARROW-SCREEN gate for the chat
// page (cmd/agent/cli_web.go's `const webChatPage`).
//
// Why this file exists, and why it asserts STRUCTURE rather than looks:
//
// The page used to carry 44 distinct #RRGGBB literals in its <style> block
// alongside the eight variables in :root. A theme is impossible on top of that
// — a light palette can only override what is a VAR, so every literal left
// behind is a dark patch on a light page. The three properties this file pins
// are the ones a later hand-edit silently breaks:
//
//  1. NO LITERAL OUTSIDE THE TWO PALETTE BLOCKS. The literals belong in the
//     :root line and in the [data-theme="light"] override, and nowhere else —
//     that is exactly the "a literal→var() refactor is complete" claim, and it
//     is the check that turns red when one replacement is missed.
//  2. EVERY DEFINED VARIABLE IS USED, AND EVERY USED VARIABLE IS DEFINED. A
//     palette entry nobody reads is dead weight that reads like a feature; a
//     var() nobody defines renders as nothing at all (CSS drops the
//     declaration), so a typo there is invisible in every screenshot.
//  3. THE LIGHT PALETTE IS COMPLETE AND READABLE. Every variable :root defines
//     must be redefined by the light block (else it stays dark), and --fg on
//     --bg must clear WCAG AA's 4.5:1 in BOTH palettes — computed here from the
//     shipped hex values, not asserted as a remembered number.
//
// The responsive half is checked as a SHAPE, because a screenshot cannot show
// what was permanently hidden: no @media block may use display:none (a hidden
// control is unreachable, not responsive), the tool-card body must scroll
// (overflow-x:auto) rather than clip (hidden is silent content loss), and the
// composer must be allowed to wrap so the buttons cannot eat #input.
package main

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// pageStyleBlock returns the page's <style> block, boundaries included.
func pageStyleBlock(t *testing.T) string {
	t.Helper()
	const open, close = "<style>", "</style>"
	i := strings.Index(webChatPage, open)
	if i < 0 {
		t.Fatalf("the embedded page has no <style> block")
	}
	j := strings.Index(webChatPage[i:], close)
	if j < 0 {
		t.Fatalf("the page's <style> block is never closed")
	}
	return webChatPage[i : i+j+len(close)]
}

// pageThemeBlocks returns the two palette blocks a literal is ALLOWED to live
// in: the :root line and the [data-theme="light"] override. Everything else in
// <style> must be var() references.
func pageThemeBlocks(t *testing.T, css string) (root, light string) {
	t.Helper()
	i := strings.Index(css, ":root{")
	if i < 0 {
		t.Fatalf("the page carries no :root{...} palette line")
	}
	j := strings.Index(css[i:], "}")
	if j < 0 {
		t.Fatalf("the :root palette line is never closed")
	}
	root = css[i : i+j]
	k := strings.Index(css, `[data-theme="light"]{`)
	if k < 0 {
		t.Fatalf("the page carries no [data-theme=\"light\"] palette")
	}
	l := strings.Index(css[k:], "}")
	if l < 0 {
		t.Fatalf("the light palette is never closed")
	}
	return root, css[k : k+l]
}

// hexLiteral matches a #RRGGBB / #RGB colour. The page's own comments and
// selectors carry no '#', so this cannot fire on prose.
var hexLiteral = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)

// cssVarDef matches a `--name:` DEFINITION.
var cssVarDef = regexp.MustCompile(`--([a-z0-9-]+):`)

// cssVarUse matches a `var(--name` REFERENCE.
var cssVarUse = regexp.MustCompile(`var\(--([a-z0-9-]+)`)

// TestWebPagePaletteLivesInVariables is the refactor's own guard: the <style>
// block carries no colour literal outside the two palette blocks. It is the
// test that goes RED when one `#rrggbb` → `var(--x)` replacement is missed,
// which is the only failure mode this step has.
func TestWebPagePaletteLivesInVariables(t *testing.T) {
	css := pageStyleBlock(t)
	root, light := pageThemeBlocks(t, css)
	// Blank OUT the two palette blocks by deleting them from the scanned text —
	// deleting rather than replacing, so an offset shift cannot leave a fragment
	// of a palette behind to be counted (or, worse, hide one).
	rest := strings.Replace(css, root, "", 1)
	rest = strings.Replace(rest, light, "", 1)

	if hits := hexLiteral.FindAllString(rest, -1); len(hits) != 0 {
		t.Fatalf("the page's <style> block still carries %d colour literal(s) outside "+
			":root and [data-theme=\"light\"]: %s\n"+
			"Every literal must become a var() — a literal left behind is a dark patch "+
			"that no theme can override.", len(hits), strings.Join(dedupe(hits), ", "))
	}
	// A VACUUM GUARD: a scan that blanked too much would pass this test while
	// proving nothing, so the palette blocks must actually carry literals.
	if n := len(hexLiteral.FindAllString(root, -1)); n < 8 {
		t.Fatalf("the :root palette defines only %d colours — the scan read the wrong block", n)
	}
	if n := len(hexLiteral.FindAllString(light, -1)); n < 8 {
		t.Fatalf("the light palette defines only %d colours — the scan read the wrong block", n)
	}
}

// TestWebPageThemeVariablesAreUsedAndDefined pins both directions of the
// palette map: no dead variable, no dangling reference.
//
// A dead variable is a lie about capability ("we have a --warn") that nothing
// renders; a dangling one is INVISIBLE — CSS drops a declaration whose var() has
// no value, so the property silently does not apply and no screenshot shows it.
func TestWebPageThemeVariablesAreUsedAndDefined(t *testing.T) {
	css := pageStyleBlock(t)
	root, light := pageThemeBlocks(t, css)

	defined := map[string]bool{}
	for _, m := range cssVarDef.FindAllStringSubmatch(root+"}", -1) {
		defined[m[1]] = true
	}
	if len(defined) < 8 {
		t.Fatalf("the :root palette defines %d variables — the scan read the wrong block", len(defined))
	}
	used := map[string]bool{}
	for _, m := range cssVarUse.FindAllStringSubmatch(css, -1) {
		used[m[1]] = true
	}
	if len(used) == 0 {
		t.Fatal("the page's <style> block references no CSS variable at all")
	}

	var dead, dangling []string
	for name := range defined {
		if !used[name] {
			dead = append(dead, "--"+name)
		}
	}
	for name := range used {
		if !defined[name] {
			dangling = append(dangling, "--"+name)
		}
	}
	if len(dead) != 0 {
		t.Errorf("%d defined colour variable(s) are referenced by nothing: %s\n"+
			"A palette entry nobody reads is dead weight that reads like a feature.",
			len(dead), strings.Join(sorted(dead), ", "))
	}
	if len(dangling) != 0 {
		t.Errorf("%d var() reference(s) have no definition: %s\n"+
			"CSS drops such a declaration silently — the property never applies.",
			len(dangling), strings.Join(sorted(dangling), ", "))
	}

	// THE LIGHT OVERRIDE MUST COVER THE DARK ONE. A variable the light block
	// forgets keeps its dark value, which is the "one dark patch on a light
	// page" defect in its most common shape.
	lightDefined := map[string]bool{}
	for _, m := range cssVarDef.FindAllStringSubmatch(light+"}", -1) {
		lightDefined[m[1]] = true
	}
	var uncovered []string
	for name := range defined {
		if !lightDefined[name] {
			uncovered = append(uncovered, "--"+name)
		}
	}
	if len(uncovered) != 0 {
		t.Errorf("the light palette does not redefine %d variable(s): %s\n"+
			"Each one keeps its dark value on a light surface.",
			len(uncovered), strings.Join(sorted(uncovered), ", "))
	}
}

// TestWebPageLightPaletteIsReadable computes WCAG 2.1 contrast for --fg on --bg
// in BOTH palettes. The threshold is the AA body-text rule (4.5:1) and it is
// computed from the shipped hex values, so a palette edit that dims the text is
// caught here rather than by an eye.
func TestWebPageLightPaletteIsReadable(t *testing.T) {
	css := pageStyleBlock(t)
	root, light := pageThemeBlocks(t, css)
	for _, tc := range []struct {
		name, block string
	}{
		{"dark", root},
		{"light", light},
	} {
		fg, okFg := cssVarValue(tc.block, "fg")
		bg, okBg := cssVarValue(tc.block, "bg")
		if !okFg || !okBg {
			t.Fatalf("the %s palette does not define both --fg and --bg (fg=%q bg=%q)", tc.name, fg, bg)
		}
		ratio := contrastRatio(t, fg, bg)
		t.Logf("%s palette: --fg %s on --bg %s = %.2f:1", tc.name, fg, bg, ratio)
		if ratio < wcagAA {
			t.Errorf("the %s palette's --fg on --bg is %.2f:1, under WCAG AA's %.1f:1 — "+
				"body text at that ratio is unreadable for a large share of readers",
				tc.name, ratio, wcagAA)
		}
	}
}

// TestWebPageResponsiveShape pins the responsive half as a SHAPE: two
// breakpoints exist, neither hides a control, the tool card scrolls instead of
// clipping, and the composer may wrap.
func TestWebPageResponsiveShape(t *testing.T) {
	css := pageStyleBlock(t)

	for _, bp := range []string{"@media(max-width:600px)", "@media(max-width:900px)"} {
		i := strings.Index(css, bp)
		if i < 0 {
			t.Fatalf("the page carries no %s block — the narrow-screen rules are missing", bp)
		}
		depth, end := 0, -1
		for k := i; k < len(css); k++ {
			switch css[k] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = k + 1
				}
			}
			if end > 0 {
				break
			}
		}
		if end < 0 {
			t.Fatalf("%s is never closed", bp)
		}
		block := css[i:end]
		// THE ANTI-SHORTCUT: a control hidden by a media query is not
		// responsive, it is unreachable — and on a phone it is unreachable in the
		// one place the user is standing.
		for _, sel := range []string{"#mic", "#clip", "#shot", "#send", "#input", "#theme", "#status"} {
			if strings.Contains(block, sel+"{display:none") || strings.Contains(block, sel+" {display:none") {
				t.Errorf("%s hides %s with display:none — a hidden control is not a responsive layout, "+
					"it is a feature the phone user cannot reach", bp, sel)
			}
		}
	}

	// The tool card body is monospace code: it either scrolls inside its card or
	// it silently loses the tail. `hidden` is the silent option and it is banned.
	if !strings.Contains(css, ".toolcard .tbody{overflow-x:auto}") {
		t.Error("no rule gives .toolcard .tbody overflow-x:auto — a wide tool result then " +
			"pushes the whole page sideways on a narrow screen")
	}
	if strings.Contains(css, ".toolcard .tbody{overflow-x:hidden") {
		t.Error(".toolcard .tbody clips its overflow — hidden content is content silently gone")
	}
	// The composer is a non-wrapping flex row by default, and that is exactly how
	// the four glyph buttons plus Send ate #input at 380 px.
	if !strings.Contains(css, "#form{flex-wrap:wrap") {
		t.Error("no narrow-screen rule lets #form wrap — at phone width the buttons then " +
			"take their intrinsic width out of #input's share")
	}
}

// wcagAA is WCAG 2.1's minimum contrast for body text (level AA, 1.4.3).
const wcagAA = 4.5

// cssVarValue reads one --name: #rrggbb pair out of a palette block.
func cssVarValue(block, name string) (string, bool) {
	re := regexp.MustCompile(`--` + regexp.QuoteMeta(name) + `:(#[0-9a-fA-F]{3,8})`)
	m := re.FindStringSubmatch(block)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// contrastRatio computes WCAG 2.1's relative-luminance contrast between two
// hex colours — (L1+0.05)/(L2+0.05) with L1 the lighter of the two.
func contrastRatio(t *testing.T, a, b string) float64 {
	t.Helper()
	la, lb := relativeLuminance(t, a), relativeLuminance(t, b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// relativeLuminance is WCAG 2.1's definition over linearised sRGB channels.
func relativeLuminance(t *testing.T, hex string) float64 {
	t.Helper()
	r, g, b := hexChannels(t, hex)
	return 0.2126*linearise(r) + 0.7152*linearise(g) + 0.0722*linearise(b)
}

// linearise is the sRGB → linear-light transfer WCAG 2.1 specifies.
func linearise(c float64) float64 {
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// hexChannels parses #rgb or #rrggbb into three [0,1] channel values.
func hexChannels(t *testing.T, hex string) (float64, float64, float64) {
	t.Helper()
	h := strings.TrimPrefix(hex, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		t.Fatalf("%q is not a 3- or 6-digit hex colour", hex)
	}
	vals := make([]float64, 3)
	for i := 0; i < 3; i++ {
		n, err := strconv.ParseUint(h[i*2:i*2+2], 16, 8)
		if err != nil {
			t.Fatalf("%q carries a non-hex channel: %v", hex, err)
		}
		vals[i] = float64(n) / 255
	}
	return vals[0], vals[1], vals[2]
}

// dedupe drops repeated strings, keeping first-seen order.
func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// sorted returns a stable copy, so a failure message reads the same twice.
func sorted(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
