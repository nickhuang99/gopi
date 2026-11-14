package lineedit

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// palette256_test.go — the acceptance for the MIDDLE tier: a terminal that
// declares the 256-colour palette (TERM contains the 256color marker) but does
// NOT advertise 24-bit colour gets the gold family as `38;5;n`, not the stock
// 8-colour ANSI set.
//
// The defect this file pins is a MEASUREMENT, not a taste (task 01a0dcd4, the
// operator's own report): on this host TERM=xterm-256color and COLORTERM is
// unset, so the binary layer had exactly two answers — the 16 named colours or
// 24-bit gold — and the terminal that declares 256 colours received
// SGR 32/33/36/90: the same bytes `ls --color` emits. The interface was therefore
// indistinguishable from an unthemed console, and the operator read that as "no
// styling was done". `internal/sgr` had supported `38;5;n` (ColorModePalette,
// FgParams) the whole time; nothing in this package used it.
//
// What is asserted here, in the order the epic states it:
//
//  1. the TIER RESOLUTION: a 256-colour TERM yields Palette256 by DEFAULT, while
//     COLORTERM=truecolor still yields the 24-bit table and an 8-colour terminal
//     still yields BasicPalette;
//  2. the WIRE: the default table on this host emits `38;5;n`, composed through
//     internal/sgr, and its bytes are NOT BasicPalette's;
//  3. NO SILENT DOWNGRADE: a table the terminal cannot draw is substituted with
//     ONE stderr line naming the cause and the enabling environment variable;
//  4. the INVARIANTS survive the new table: no cell moves, the status row keeps
//     its own gold (38;5;220 on this tier — palette256StatusFg), colour-off
//     stays byte-exact.

// term256 is the TERM value this epic was reported from: the marker-bearing name
// a modern terminal sets. It is COMPOSED from the marker constant (Rule 13/14) so
// the test and the resolver cannot disagree about what a 256-colour TERM looks
// like — a hand-spelled "xterm-256color" here would keep passing if the marker
// itself changed.
const term256 = "xterm-" + Term256Color

// ---------------------------------------------------------------------------
// 0. The xterm cube, reconstructed independently.
// ---------------------------------------------------------------------------

// xtermCubeValue is the xterm 256-colour cube's channel value for step s (0..5),
// reconstructed from the STANDARD rather than from the palette constants: steps
// 1..5 are 40*s+55 (95, 135, 175, 215, 255) and step 0 is 0. It exists so the
// test can assert what a cube index RESOLVES TO — the palette names indices, and
// an index is only the promised colour if the cube says so.
//
// This is deliberately a second, independent derivation of the same fact: a test
// that read the palette's own constants back would agree with any index,
// including a wrong one.
func xtermCubeValue(step int) int {
	if step == 0 {
		return 0
	}
	return cubeChannelBase + cubeChannelStep*step
}

// xtermCubeRGB decodes a cube index (16..231) into its three channels.
func xtermCubeRGB(idx int) (int, int, int) {
	n := idx - cubeFirstIndex
	b := n % cubeSteps
	g := n / cubeSteps % cubeSteps
	r := n / (cubeSteps * cubeSteps)
	return xtermCubeValue(r), xtermCubeValue(g), xtermCubeValue(b)
}

// The cube's own arithmetic, named (Rule 1): the 6×6×6 cube starts at entry 16
// and its non-zero channel steps are 40*n+55.
const (
	cubeFirstIndex  = 16
	cubeSteps       = 6
	cubeChannelBase = 55
	cubeChannelStep = 40
)

// TestPalette256CubeIndicesAreTheGoldFamily is the colour TRUTH of the new table:
// every index it names must decode, through the xterm cube arithmetic, to a warm
// colour of the right family. This is what separates "a 256 approximation of the
// gold roles" (the epic's requirement) from "some other 256 numbers".
func TestPalette256CubeIndicesAreTheGoldFamily(t *testing.T) {
	cases := []struct {
		role    string
		idx     int
		wantHex string
		// warm requires r > b: the gold/amber family is a warm hue, and a
		// "gold" index that decodes to a blue would be a transposed digit.
		warm bool
	}{
		{"banner accent", cubeGoldAccent, "#FFD700", true},
		{"prompt accent", cubePaleGold, "#D7D787", true},
		{"answer body", cubeGoldBody, "#FFFFD7", true},
		{"answer emphasis", cubeDeepGold, "#D7AF00", true},
		{"muted chrome", cubeMutedGold, "#D7AF87", true},
		{"diff added", cubeGreen, "#87D787", false},
		{"diff removed", cubeRed, "#FF8787", false},
		{"warning", cubeAmber, "#FFAF5F", true},
	}
	for _, tc := range cases {
		r, g, b := xtermCubeRGB(tc.idx)
		got := fmt.Sprintf("#%02X%02X%02X", r, g, b)
		if got != tc.wantHex {
			t.Errorf("%s: index %d decodes to %s, want %s", tc.role, tc.idx, got, tc.wantHex)
		}
		if tc.warm && r <= b {
			t.Errorf("%s: index %d decodes to %s, which is not a warm (gold-family) hue", tc.role, tc.idx, got)
		}
	}
	// The banner's accent IS Hermes' own gold: the 256 table's headline role
	// survives the quantisation exactly, which is why the cube family was chosen
	// for this tier.
	if r, g, b := xtermCubeRGB(cubeGoldAccent); r != 0xFF || g != 0xD7 || b != 0x00 {
		t.Errorf("the banner accent is %02X%02X%02X, want #FFD700 (Hermes DARK_THEME.primary)", r, g, b)
	}
}

// palette256Seq is the sequence a 256-style opens with, composed through
// internal/sgr — the package that owns the wire spelling of `38;5;n` (and the
// only place that knows entries 0..15 have a shorter spelling this table avoids).
func palette256Seq(idx int) string { return sgr.Seq(sgr.FgParams(idx)) }

// ---------------------------------------------------------------------------
// 1. Tier resolution.
// ---------------------------------------------------------------------------

// TestPalette256TierResolution is the gate: for each real terminal shape, which
// table does the ONE resolver return, and does the capability report the tier it
// drew? The rows are the three tiers plus the two shape traps (a truecolor
// terminal is a 256-colour terminal too; a TERM that merely CONTAINS the digits
// is not one).
func TestPalette256TierResolution(t *testing.T) {
	cases := []struct {
		name       string
		term       string
		colorTerm  string
		palette    string
		wantTable  Palette
		wantDepth  TermDepth
		want256    bool
		wantTrueco bool
	}{
		{
			// THE EPIC'S OWN ROW: this host, nothing requested.
			name: "the default on a 256-colour terminal is the 256 table", term: term256,
			wantTable: Palette256, wantDepth: Depth256, want256: true,
		},
		{
			name: "the default on a truecolour terminal is still the 24-bit gold table", term: term256,
			colorTerm: ColorTermTruecolor, wantTable: HermesDarkPalette, wantDepth: DepthTruecolor,
			want256: true, wantTrueco: true,
		},
		{
			name: "the default on an 8-colour terminal is the basic floor", term: "xterm",
			wantTable: BasicPalette, wantDepth: DepthBasic,
		},
		{
			// A truecolor terminal without the marker still is a 256-colour
			// terminal: 38;5;n is understood wherever 38;2 is.
			name: "a truecolor advertisement alone satisfies the 256 tier", term: "xterm",
			colorTerm: ColorTermTruecolor, wantTable: HermesDarkPalette, wantDepth: DepthTruecolor, want256: true, wantTrueco: true,
		},
		{
			// The marker is the TI/terminfo convention: it appears in the middle
			// of the name (screen-256color, tmux-256color), and case is not the
			// terminal's business to get right.
			name: "the marker is matched case-insensitively", term: "SCREEN-256COLOR",
			wantTable: Palette256, wantDepth: Depth256, want256: true,
		},
		{
			// A TERM carrying the DIGITS without the marker is not a 256-colour
			// declaration: `xterm-256` is not a TERM anyone sets, and treating it
			// as one would advertise 38;5;n to a terminal that never said it
			// understands it.
			name: "the digits without the marker are not a declaration", term: "xterm-256",
			wantTable: BasicPalette, wantDepth: DepthBasic,
		},
		{
			name: "an explicitly requested 256 table is drawn where it is declared", term: term256,
			palette: PaletteNamePalette256, wantTable: Palette256, wantDepth: Depth256, want256: true,
		},
		{
			name: "an explicitly requested 256 table on a 24-bit terminal is drawn as-is", term: term256,
			colorTerm: ColorTermTruecolor, palette: PaletteNamePalette256,
			wantTable: Palette256, wantDepth: DepthTruecolor, want256: true, wantTrueco: true,
		},
		{
			// DOWNGRADE, tier preserved: 256 asked for on the floor.
			name: "an explicitly requested 256 table on an 8-colour terminal falls back to basic", term: "xterm",
			palette: PaletteNamePalette256, wantTable: BasicPalette, wantDepth: DepthBasic,
		},
		{
			// DOWNGRADE, tier preserved the other way: 24-bit asked for where
			// only 256 exists gets the 256 table — NOT the floor. This is the row
			// the old binary collapse got wrong.
			name: "a 24-bit table requested on a 256-colour terminal degrades to 256, not to basic",
			term: term256, palette: PaletteNameHermes,
			wantTable: Palette256, wantDepth: Depth256, want256: true,
		},
		{
			name: "a light 24-bit table degrades the same way", term: term256, palette: PaletteNameHermesLight,
			wantTable: Palette256, wantDepth: Depth256, want256: true,
		},
		{
			name: "the basic floor is reachable verbatim on a 256-colour terminal", term: term256,
			palette: PaletteNameBasic, wantTable: BasicPalette, wantDepth: Depth256, want256: true,
		},
	}

	// The notices each row emits are asserted in their own test; here they are
	// swallowed so a passing run stays quiet on stderr.
	captureStderr(t, func() {
		master := paletteOpenPty(t)
		defer func() { _ = master.Close() }() //stylegate:ignore closing the pty master is teardown, not a step whose failure matters
		tty := paletteSlaveFile(t, master)

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Setenv(EnvTerm, tc.term)
				t.Setenv(EnvColorTerm, tc.colorTerm)
				t.Setenv(EnvPalette, tc.palette)

				c := CapabilityForEnv(false, tty)
				got := PaletteForCapability(c)
				if got != tc.wantTable {
					t.Fatalf("PaletteForCapability = %+v, want %+v (TERM=%q COLORTERM=%q GOPI_PALETTE=%q)",
						got, tc.wantTable, tc.term, tc.colorTerm, tc.palette)
				}
				if c.Depth() != tc.wantDepth {
					t.Fatalf("capability depth = %v, want %v (TERM=%q COLORTERM=%q)", c.Depth(), tc.wantDepth, tc.term, tc.colorTerm)
				}
				if c.Palette256() != tc.want256 {
					t.Fatalf("Palette256() = %v, want %v", c.Palette256(), tc.want256)
				}
				if c.Truecolor() != tc.wantTrueco {
					t.Fatalf("Truecolor() = %v, want %v", c.Truecolor(), tc.wantTrueco)
				}
				// The table's own declaration must agree with the capability
				// that drew it — a table drawn at a depth below its own
				// requirement is exactly the "prints its parameters as text"
				// defect.
				if !c.AtLeast(got.RequiredDepth()) {
					t.Fatalf("the resolved table needs %v but the capability reaches %v", got.RequiredDepth(), c.Depth())
				}
			})
		}
	})
}

// TestPalette256RequiredDepthIsDerivedFromTheTable pins the derivation the gate
// rests on: each table's depth comes from the sequences it carries, not from a
// declared field, so a table that grows a deeper colour is gated automatically.
func TestPalette256RequiredDepthIsDerivedFromTheTable(t *testing.T) {
	for _, tc := range []struct {
		name string
		pal  Palette
		want TermDepth
	}{
		{"MonoPalette", MonoPalette, DepthBasic},
		{"BasicPalette", BasicPalette, DepthBasic},
		{"Palette256", Palette256, Depth256},
		{"HermesDarkPalette", HermesDarkPalette, DepthTruecolor},
		{"HermesLightPalette", HermesLightPalette, DepthTruecolor},
	} {
		if got := tc.pal.RequiredDepth(); got != tc.want {
			t.Errorf("%s.RequiredDepth() = %v, want %v", tc.name, got, tc.want)
		}
	}
	// The 256 table must NOT be mistaken for a 24-bit one (the two markers are
	// both `38;`-prefixed, so this is the derivation's sharp edge).
	if Palette256.NeedsTruecolor() {
		t.Error("Palette256 reports a 24-bit requirement — the derivation confused 38;5 with 38;2")
	}
	if !strings.Contains(Palette256.Answer.Open(), palette256Marker()) {
		t.Errorf("Palette256.Answer = %q, want a %s spelling", Palette256.Answer.Open(), palette256Marker())
	}
	if !strings.Contains(HermesDarkPalette.Answer.Open(), truecolorMarker()) {
		t.Errorf("HermesDarkPalette.Answer = %q, want a %s spelling", HermesDarkPalette.Answer.Open(), truecolorMarker())
	}
}

// ---------------------------------------------------------------------------
// 2. The wire: this host's default emits 38;5;n, and it is not the basic table.
// ---------------------------------------------------------------------------

// TestDefaultOnA256TerminalIsNotTheBasicTable is the epic's acceptance, measured
// on the BYTES the default run emits on a terminal that declares 256 colours:
// the answer body carries `38;5;n`, the stream does NOT carry the basic table's
// own sequences, and the two byte streams differ.
func TestDefaultOnA256TerminalIsNotTheBasicTable(t *testing.T) {
	captureStderr(t, func() {
		master := paletteOpenPty(t)
		defer func() { _ = master.Close() }() //stylegate:ignore closing the pty master is teardown, not a step whose failure matters
		tty := paletteSlaveFile(t, master)

		// THE HOST'S OWN SHAPE: TERM declares 256 colours, COLORTERM is unset,
		// and the operator requested nothing.
		t.Setenv(EnvTerm, term256)
		t.Setenv(EnvColorTerm, "")
		t.Setenv(EnvPalette, "")

		c := CapabilityForEnv(false, tty)
		if c.Colour() != ColourOn {
			t.Fatalf("colour is off with a real pty sink and no NO_COLOR — the test would prove nothing")
		}
		def := PaletteForCapability(c)
		if def != Palette256 {
			t.Fatalf("the default table on TERM=%s is %+v, want Palette256", term256, def)
		}

		answer := "the default palette on this host\n```go\ncode line\n```\n-old\n+new\n"
		got := paletteAnswerCapture(t, def, answer)
		basic := paletteAnswerCapture(t, BasicPalette, answer)

		// (a) the wire spelling: 38;5;n reaches the body.
		wantBody := palette256Seq(cubeGoldBody)
		if !strings.Contains(got, wantBody) {
			t.Fatalf("the default capture does not carry %q — the 256 tier never reached the wire:\n%q", wantBody, got)
		}
		if !strings.Contains(got, palette256Marker()) {
			t.Fatalf("the default capture carries no 256-colour foreground:\n%q", got)
		}
		// (b) it is NOT the basic table's output, byte for byte.
		if got == basic {
			t.Fatalf("the default capture is byte-identical to BasicPalette's:\n%q", got)
		}
		// (c) and specifically: the 8-colour ANSI codes the basic table uses for
		// these roles are ABSENT. This is the operator's actual reading — "no
		// difference from the default console" — stated as a falsifiable claim.
		for name, code := range map[string]string{
			"sgrCyan (code/banner)":  sgrCyan,
			"sgrGreen (prompt/diff)": sgrGreen,
			"sgrRed (errors)":        sgrRed,
			"sgrGray (hint)":         sgrGray,
			"sgrYellow (warning)":    sgrYellow,
		} {
			if strings.Contains(got, code) {
				t.Errorf("the default capture still carries %s (%q) — the 256 table did not displace the stock ANSI set:\n%q",
					name, code, got)
			}
		}
		// (d) a 24-bit sequence must NOT appear: this terminal advertised no
		// COLORTERM, and 38;2 at it would be printed as text.
		if strings.Contains(got, truecolorMarker()) {
			t.Fatalf("the default capture carries a 24-bit sequence on a terminal that advertises none:\n%q", got)
		}
		// (e) the emphasis is real: the stream is not merely "different bytes".
		if !hasSGR(got) {
			t.Fatal("the default capture carries no SGR at all")
		}
		// (f) colour-off is still byte-exact under the same environment.
		off := PaletteForCapability(CapabilityForEnv(true, tty))
		if mono := paletteAnswerCapture(t, off, answer); hasSGR(mono) {
			t.Fatalf("a colour-off run under TERM=%s emitted SGR:\n%q", term256, mono)
		}
	})
}

// TestPalette256CoversEverySurface asserts the table names every colour-carrying
// surface with a 256 entry: a field left at the zero style would render plain
// even with the palette on, which no other test would notice.
func TestPalette256CoversEverySurface(t *testing.T) {
	for name, st := range map[string]Style{
		"banner": Palette256.Banner, "prompt": Palette256.Prompt,
		"answer": Palette256.Answer, "answerBold": Palette256.AnswerBold,
		"code": Palette256.Code, "diffAdd": Palette256.DiffAdd, "diffDel": Palette256.DiffDel,
		"hint": Palette256.Hint, "warning": Palette256.Warning, "error": Palette256.Error,
		"toolCall": Palette256.ToolCall,
	} {
		if st.Empty() {
			t.Errorf("Palette256.%s is empty — the surface would render plain with the palette on", name)
		}
		if !strings.Contains(st.Open(), palette256Marker()) {
			t.Errorf("Palette256.%s = %q, want a %s spelling", name, st.Open(), palette256Marker())
		}
	}
	// The status row carries this table's own gold — the cube's #FFD700, the
	// colour Hermes calls primary — with its attributes, so the row is
	// distinguished from the body by colour AND weight (task 01a0e2e6).
	allowedAttrs := []string{sgrBold, sgrItalic, sgrUnder, sgrReverse}
	for name, st := range map[string]Style{
		"statusLabel": Palette256.StatusLabel, "statusLive": Palette256.StatusLive,
		"thinking": Palette256.Thinking, "toolRow": Palette256.ToolRow,
	} {
		if st.Empty() {
			t.Errorf("Palette256.%s is empty — the status row would carry no emphasis at all", name)
		}
		if rest := stripAttrs(st.Open(), allowedAttrs); rest != palette256Seq(cubeGoldAccent) {
			t.Errorf("Palette256.%s = %q carries %q, want exactly the cube gold %q on top of its attributes — the row is coloured, never left at the terminal's default",
				name, st.Open(), rest, palette256Seq(cubeGoldAccent))
		}
	}
	// The banner and the prompt must be DISTINGUISHABLE colours: the epic asks for
	// a distinct accent on each, and two roles sharing one sequence would make
	// the chrome read as one surface.
	if Palette256.Banner.Open() == Palette256.Prompt.Open() {
		t.Error("the 256 table's banner and prompt share one sequence — the two chrome surfaces are indistinguishable")
	}
	// ...and the emphasis inside the body must be weight PLUS a different entry,
	// not a second hue that the body does not carry.
	if !strings.Contains(Palette256.AnswerBold.Open(), sgrBold) {
		t.Errorf("Palette256.AnswerBold = %q, want bold plus the emphasis colour", Palette256.AnswerBold.Open())
	}
}

// TestPaletteByNameKnowsThe256Table pins the knob's value domain: the name (and
// its aliases) resolve to the table, and every advertised name is reachable —
// PaletteNames is what the usage text and the unknown-name error print, so a
// name listed there but not resolvable would be a documented lie.
func TestPaletteByNameKnowsThe256Table(t *testing.T) {
	for _, name := range append([]string{PaletteNamePalette256}, palette256Aliases...) {
		pal, ok := PaletteByName(name)
		if !ok {
			t.Fatalf("PaletteByName(%q) = not recognised, but it names the 256 table", name)
		}
		if pal != Palette256 {
			t.Fatalf("PaletteByName(%q) resolved to %+v, want Palette256", name, pal)
		}
	}
	for _, name := range PaletteNames() {
		if _, ok := PaletteByName(name); !ok {
			t.Errorf("PaletteNames advertises %q, which PaletteByName does not recognise", name)
		}
	}
	// The default sentinel is deliberately NOT a table name: it stands for "the
	// terminal decides", and resolving it in PaletteByName would give the
	// sentinel a fixed answer — the binary default this epic removes. It is the
	// EMPTY string, so it is also not a word an operator could set the knob to
	// and be refused.
	if PaletteNameUnset != "" {
		t.Errorf("PaletteNameUnset = %q: the sentinel must be the empty selection, so the knob cannot be set to it", PaletteNameUnset)
	}
	if _, ok := PaletteByName(PaletteNameUnset); ok {
		t.Errorf("PaletteByName(%q) resolves: the default sentinel must be settled by the CAPABILITY, not by a name lookup", PaletteNameUnset)
	}
	// The word "auto" is what a reader would GUESS the default is called; it is
	// not a legal value, and the run says so rather than pretending (the notice
	// is asserted in TestPaletteSelectionRefusesAnUnknownName).
	if _, ok := PaletteByName("auto"); ok {
		t.Error(`PaletteByName("auto") resolves: a name nobody documents would become an accidental second spelling of the default`)
	}
}

// ---------------------------------------------------------------------------
// 3. No silent downgrade.
// ---------------------------------------------------------------------------

// TestDowngradeNoticeNamesTheCauseAndTheEnablingVariable pins the ONE stderr
// line: it must name the table asked for, why it cannot be drawn, the observed
// environment, the table actually drawn, and the environment variable that would
// enable the request. Rule 15's discipline, applied to capability rather than to
// a typo.
func TestDowngradeNoticeNamesTheCauseAndTheEnablingVariable(t *testing.T) {
	captureStderr(t, func() {
		master := paletteOpenPty(t)
		defer func() { _ = master.Close() }() //stylegate:ignore closing the pty master is teardown, not a step whose failure matters
		tty := paletteSlaveFile(t, master)

		t.Setenv(EnvTerm, term256)
		t.Setenv(EnvColorTerm, "")
		t.Setenv(EnvPalette, PaletteNameHermes)

		var drew Palette
		notice := captureStderr(t, func() {
			drew = PaletteForCapability(CapabilityForEnv(false, tty))
		})
		if drew != Palette256 {
			t.Fatalf("the downgrade drew %+v, want Palette256 (the deepest table this terminal can draw)", drew)
		}
		if notice == "" {
			t.Fatal("a capability downgrade produced NO stderr line — the silent collapse this epic removes is back")
		}
		for what, want := range map[string]string{
			"the table asked for":           PaletteNameHermes,
			"the enabling variable":         EnvColorTerm,
			"the value that enables":        ColorTermTruecolor,
			"the variable's observed value": EnvColorTerm + "=\"\"",
			"the table drawn":               PaletteNamePalette256,
		} {
			if !strings.Contains(notice, want) {
				t.Errorf("the downgrade notice does not name %s (%q):\n%q", what, want, notice)
			}
		}
		// EXACTLY ONE line: a notice per field or per frame would bury the run's
		// own output. A notice that wrapped would be two lines and is a failure,
		// not a formatting preference.
		if n := strings.Count(strings.TrimRight(notice, "\n"), "\n"); n != 0 {
			t.Errorf("the downgrade notice spans %d lines, want exactly 1:\n%q", n+1, notice)
		}

		// The two shapes that must stay SILENT: an unset knob (nobody asked for
		// a table they are not getting) and a request the terminal CAN draw.
		for _, tc := range []struct {
			name    string
			term    string
			palette string
		}{
			{"unset knob follows the terminal", term256, ""},
			{"a drawable request", term256, PaletteNamePalette256},
			{"the floor on the floor", "xterm", PaletteNameBasic},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Setenv(EnvTerm, tc.term)
				t.Setenv(EnvPalette, tc.palette)
				quiet := captureStderr(t, func() {
					_ = PaletteForCapability(CapabilityForEnv(false, tty))
				})
				if quiet != "" {
					t.Fatalf("a run that got the table it asked for printed a notice:\n%q", quiet)
				}
			})
		}
	})
}

// TestPaletteSelectionRefusesAnUnknownName pins the knob's other edge: an empty
// knob (and the word a reader would GUESS names the default) follows the
// terminal, a real value is taken, and anything else is REPORTED — and the report
// does not promise a table it cannot know, because the default depends on the
// terminal's depth.
func TestPaletteSelectionRefusesAnUnknownName(t *testing.T) {
	for _, tc := range []struct {
		name string
		knob string
		want string
	}{
		{"unset follows the terminal", "", PaletteNameUnset},
		{"whitespace is unset", "   ", PaletteNameUnset},
		{"the obvious guess is not a value", "auto", PaletteNameUnset},
		{"a real value is taken", "  " + PaletteNameHermes + "  ", PaletteNameHermes},
		{"the 256 table is a value", PaletteNamePalette256, PaletteNamePalette256},
		{"an alias is a value", palette256Aliases[0], palette256Aliases[0]},
		{"a typo follows the terminal", "gold-ish", PaletteNameUnset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvPalette, tc.knob)
			var got string
			notice := captureStderr(t, func() { got = PaletteSelection() })
			if got != tc.want {
				t.Fatalf("PaletteSelection() with %s=%q = %q, want %q", EnvPalette, tc.knob, got, tc.want)
			}
			refused := strings.TrimSpace(tc.knob) != "" && tc.want == PaletteNameUnset
			if refused {
				if notice == "" {
					t.Fatalf("%s=%q was refused with no stderr line — a silent refusal is the same defect as a silent downgrade", EnvPalette, tc.knob)
				}
				for what, want := range map[string]string{
					"the offending value":    strings.TrimSpace(tc.knob),
					"the variable":           EnvPalette,
					"a legal value":          PaletteNamePalette256,
					"the empty-knob default": "following the terminal",
				} {
					if !strings.Contains(notice, want) {
						t.Errorf("the refusal notice does not name %s (%q):\n%q", what, want, notice)
					}
				}
				// One cause, one line: the resolver must not ALSO warn about a
				// downgrade it is not performing (the terminal decides here).
				if n := strings.Count(strings.TrimRight(notice, "\n"), "\n"); n != 0 {
					t.Errorf("the refusal notice spans %d lines, want exactly 1:\n%q", n+1, notice)
				}
				return
			}
			if notice != "" {
				t.Fatalf("%s=%q printed a notice but is a legal input:\n%q", EnvPalette, tc.knob, notice)
			}
		})
	}
}

// TestNoDepthCollapsesToSilenceOrToTheWrongTier is the enumeration the gate rests
// on: over EVERY (TERM, COLORTERM, name, colour-off) combination, the resolved
// table is never empty while colour is on, never needs a depth the capability
// lacks, and never silently drops a request to the FLOOR when a deeper tier was
// available. A hand-picked case list cannot show this — the failing combination
// is the one nobody thought of.
func TestNoDepthCollapsesToSilenceOrToTheWrongTier(t *testing.T) {
	terms := []string{"", "xterm", term256, "tmux-256color", "screen", "linux", TermDumb, "xterm-256color-extra"}
	colorTerms := []string{"", ColorTermTruecolor, "24bit"}
	names := append([]string{""}, PaletteNames()...)
	names = append(names, PaletteNameUnset, "unknown")
	master := paletteOpenPty(t)
	defer func() { _ = master.Close() }() //stylegate:ignore closing the pty master is teardown, not a step whose failure matters
	tty := paletteSlaveFile(t, master)

	for _, term := range terms {
		for _, colorTerm := range colorTerms {
			for _, name := range names {
				t.Run(term+"/"+colorTerm+"/"+name, func(t *testing.T) {
					t.Setenv(EnvTerm, term)
					t.Setenv(EnvColorTerm, colorTerm)
					t.Setenv(EnvPalette, name)
					_, known := PaletteByName(name)
					quiet := captureStderr(t, func() {
						c := CapabilityForEnv(false, tty)
						pal := PaletteForCapability(c)
						if c.Colour() == ColourOff {
							// TERM=dumb: the zero table is the CORRECT answer and
							// the empty table is not a collapse — the rest of the
							// assertions below are about a colour-on run.
							if pal != MonoPalette {
								t.Fatalf("a colour-off capability returned %+v (TERM=%q COLORTERM=%q name=%q)", pal, term, colorTerm, name)
							}
							if hasSGR(pal.Answer.Wrap("x")) {
								t.Fatalf("a colour-off capability emitted SGR (TERM=%q COLORTERM=%q)", term, colorTerm)
							}
							return
						}
						// (a) never empty while colour is on.
						if pal == MonoPalette {
							t.Fatalf("an empty table under a colour-on capability (TERM=%q COLORTERM=%q name=%q)", term, colorTerm, name)
						}
						// (b) never a table the terminal cannot draw.
						if !c.AtLeast(pal.RequiredDepth()) {
							t.Fatalf("drew a table needing %v at depth %v (TERM=%q COLORTERM=%q name=%q)",
								pal.RequiredDepth(), c.Depth(), term, colorTerm, name)
						}
						// (c) never a silent drop to the floor where a deeper
						// tier exists: the collapse this epic removes. A 256-
						// capable terminal may only be served BasicPalette if
						// that is what was ASKED for.
						if c.AtLeast(Depth256) && pal == BasicPalette && name != PaletteNameBasic {
							t.Fatalf("a 256-capable terminal was served the 8-colour floor (TERM=%q COLORTERM=%q name=%q)",
								term, colorTerm, name)
						}
						// (d) the request was not silently obeyed where it could
						// not be drawn, and not silently refused where it could:
						// the drawn table is the named one whenever it fits.
						if name != PaletteNameUnset {
							if want, ok := PaletteByName(name); ok && c.canDraw(want) && pal != want {
								t.Fatalf("a drawable request %q was not honoured: drew %+v (TERM=%q COLORTERM=%q)",
									name, pal, term, colorTerm)
							}
						}
					})
					// The notice rule, stated for each of the three shapes:
					//   * a name the resolver does not know: the REFUSAL line,
					//     emitted whatever the colour decision is — a typo in the
					//     knob is worth reading even on a terminal that gets no
					//     colour (the preference is recorded, not silently
					//     dropped).
					//   * colour OFF (TERM=dumb) with a known name: the palette
					//     preference is moot — a higher decision already said no,
					//     and every redirected or dumb run would print this line
					//     onto a captured transcript. Silence is correct.
					//   * a named table this terminal cannot draw: the DOWNGRADE
					//     line. A drawable request and an unset knob stay quiet.
					colourOff := strings.EqualFold(term, TermDumb)
					refusal := !known && name != ""
					downgrade := !colourOff && known && name != "" && !fitsDepth(colorTerm, term, name)
					wantNotice := refusal || downgrade
					gotNotice := quiet != ""
					if gotNotice != wantNotice {
						t.Fatalf("stderr notice = %v, want %v (TERM=%q COLORTERM=%q name=%q): %q",
							gotNotice, wantNotice, term, colorTerm, name, quiet)
					}
					if gotNotice {
						isRefusal := strings.Contains(quiet, "not a known palette")
						if isRefusal != refusal {
							t.Fatalf("the notice shape does not match the cause (refusal=%v, message=%q) for name=%q", refusal, quiet, name)
						}
						// One cause, one line — never both messages for one input.
						if n := strings.Count(strings.TrimRight(quiet, "\n"), "\n"); n != 0 {
							t.Fatalf("the notice spans %d lines, want exactly 1:\n%q", n+1, quiet)
						}
						if downgrade && !strings.Contains(quiet, EnvColorTerm) {
							t.Fatalf("the downgrade notice does not name the enabling variable %s:\n%q", EnvColorTerm, quiet)
						}
					}
				})
			}
		}
	}

	// The colour-off arm, over the same grid: the zero table, always, whatever
	// depth the environment declares.
	for _, term := range terms {
		for _, colorTerm := range colorTerms {
			t.Setenv(EnvTerm, term)
			t.Setenv(EnvColorTerm, colorTerm)
			t.Setenv(EnvPalette, PaletteNamePalette256)
			c := CapabilityForEnv(true, tty)
			pal := PaletteForCapability(c)
			if pal != MonoPalette {
				t.Fatalf("colour off returned %+v (TERM=%q COLORTERM=%q)", pal, term, colorTerm)
			}
			if c.Palette256() || c.Truecolor() {
				t.Fatalf("a colour-off capability reports a depth (TERM=%q COLORTERM=%q)", term, colorTerm)
			}
			if hasSGR(pal.Answer.Wrap("x")) {
				t.Fatalf("the colour-off table emitted SGR (TERM=%q COLORTERM=%q)", term, colorTerm)
			}
		}
	}
}

// fitsDepth reports whether a directly-named palette can be drawn in this
// environment — the predicate the notice assertion above is stated against. It
// asks the package's own resolver rather than re-deriving the tiers, so a change
// to the ladder cannot leave the test asserting a stale notion of "drawable".
func fitsDepth(colorTerm, term, name string) bool {
	pal, ok := PaletteByName(name)
	if !ok {
		return false
	}
	c := Capability{colour: ColourOn, palette: name}
	// The DECLARATION bits are set the way CapabilityForEnv sets them (in the
	// order and with the spellings it uses), because this predicate is asked
	// about environments, not about hand-built structs: a test that set them
	// differently would answer a question the program never asks.
	if strings.EqualFold(colorTerm, ColorTermTruecolor) {
		c.declaredTruecolor = true
	}
	if strings.Contains(strings.ToLower(term), Term256Color) {
		c.declaredPalette256 = true
	}
	c.declaredDepth = declaredTermDepth(c)
	return c.canDraw(pal)
}

// ---------------------------------------------------------------------------
// 4. The invariants, applied to the new table.
// ---------------------------------------------------------------------------

// TestPalette256LeavesTheStatusRowThemeIndependent extends the salience guard to
// the table this change adds: the composed row under Palette256 carries no code
// whose rendering the terminal's theme decides, and it is still emphasised —
// with this tier's own gold, the cube's #FFD700 (task 01a0e2e6).
func TestPalette256LeavesTheStatusRowThemeIndependent(t *testing.T) {
	banned := themeResolvedStatusCodes()
	label := "glm-5.3-flash" + StatusSep + "turn 2" + StatusSep + ToolGlyph + " bash git diff" + StatusSep + "1.2k tokens"
	for _, st := range []Status{
		{Label: label, Live: "ready"},
		{Label: label, Live: "thinking…", Thinking: true},
		{Label: label},
	} {
		row := styledStatusLine(st, 200, Palette256)
		// The ban is applied to the row's CODES (see statusRowSGRCodes): a
		// substring test over the row would read SGR 3 (italic) as part of the
		// extended colour's parameters and could not tell a code from a digit.
		for _, code := range statusRowSGRCodes(row) {
			if name, bad := banned[escCSI+code+"m"]; bad {
				t.Errorf("the 256 status row carries %s (SGR %s):\n%q", name, code, row)
			}
		}
		// The colour must be the 256 tier's own: `38;5;n` — numbered, so the
		// terminal cannot remap it — and specifically the cube's gold.
		want := palette256Seq(cubeGoldAccent)
		if !strings.Contains(row, want) {
			t.Errorf("the 256 status row does not carry the cube gold %q (#FFD700):\n%q", want, row)
		}
		for _, spelling := range statusRowColourSpellings(row) {
			if !strings.HasPrefix(spelling, "256:") {
				t.Errorf("the 256 status row selects %s, want a 256-colour selection — a named colour is the theme's decision:\n%q", spelling, row)
			}
		}
		if !strings.Contains(row, escCSI) {
			t.Errorf("the 256 status row carries no emphasis at all:\n%q", row)
		}
		if stripSGR(row) != statusLine(st, 200) {
			t.Errorf("the 256 status row's text differs from the plain row:\n got %q\nwant %q", stripSGR(row), statusLine(st, 200))
		}
	}
}

// TestColourOffEmitsNothingUnderThe256Tier is the byte-exact half for the new
// tier: a colour-off capability produces the zero table whatever depth the
// environment declares — the knob and the terminal's name are not a way around
// the capability.
func TestColourOffEmitsNothingUnderThe256Tier(t *testing.T) {
	for _, term := range []string{term256, "xterm", TermDumb} {
		t.Run("TERM="+term, func(t *testing.T) {
			t.Setenv(EnvTerm, term)
			t.Setenv(EnvColorTerm, "")
			t.Setenv(EnvPalette, PaletteNamePalette256)
			captureStderr(t, func() {
				// A nil sink is not a terminal, so the decision is off there too.
				for _, pal := range []Palette{
					PaletteForEnvCapability(true, nil),
					PaletteForEnvCapability(false, nil),
					PaletteForCapability(Capability{colour: ColourOff, declaredPalette256: true, declaredDepth: Depth256}),
					PaletteFor(ColourOff),
				} {
					if hasSGR(pal.Banner.Wrap("b") + pal.Answer.Wrap("a") + pal.Error.Wrap("e")) {
						t.Fatalf("TERM=%s: a colour-off capability emitted SGR from %+v", term, pal)
					}
					// The whole ANSWER PATH, not just one field.
					if got := paletteAnswerCapture(t, pal, "answer\n```\ncode\n```\n+add\n-del\n"); hasSGR(got) {
						t.Fatalf("TERM=%s: the answer renderer emitted SGR:\n%q", term, got)
					}
				}
			})
		})
	}
}

// C is a one-word alias for building a capability by hand in tests — it keeps the
// table above readable (Rule 1: nothing here spells a literal that a named
// constant already names).
func c(mode ColourMode) Capability { return Capability{colour: mode} }

// TestPalette256EmphasisIsNotColourAlone is the monochrome rule for the new tier:
// the 256 capture and the basic capture read as the SAME characters, so a reader
// on a terminal with no colour loses nothing but the colour.
func TestPalette256EmphasisIsNotColourAlone(t *testing.T) {
	answer := "prose with **emphasis**\ncode below\n```\nfenced\n```\n+added\n-removed\n"
	gold := paletteAnswerCapture(t, Palette256, answer)
	basic := paletteAnswerCapture(t, BasicPalette, answer)
	mono := paletteAnswerCapture(t, MonoPalette, answer)

	if !hasSGR(gold) {
		t.Fatal("the 256 capture carries no SGR at all — the test would prove nothing")
	}
	if stripSGR(gold) != stripSGR(basic) {
		t.Fatalf("the 256 table changed the answer's TEXT relative to the basic one:\n 256 %q\n basic %q", stripSGR(gold), stripSGR(basic))
	}
	if stripSGR(gold) != stripSGR(mono) {
		t.Fatalf("the 256 table conveys something the monochrome run does not:\n 256 %q\n mono %q", stripSGR(gold), stripSGR(mono))
	}
	for _, want := range []string{"prose with", "emphasis", "fenced", "+added", "-removed"} {
		if !strings.Contains(stripSGR(gold), want) {
			t.Errorf("the stripped 256 capture lost %q:\n%s", want, stripSGR(gold))
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what it
// wrote. The downgrade notice is a stderr line, so this is how the assertion sees
// it; WITHOUT a redirection the test would read the run's own noise.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, captureReadBuf)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	fn()
	// Close the write end so the reader sees EOF, then restore the real stderr.
	_ = w.Close() //stylegate:ignore closes the fixture write end
	os.Stderr = old
	got := <-done
	_ = r.Close() //stylegate:ignore closes the fixture read end
	return got
}

// captureReadBuf is the read chunk size the capture above uses (Rule 1: a bare
// number in a loop is a knob nobody can find).
const captureReadBuf = 4096
