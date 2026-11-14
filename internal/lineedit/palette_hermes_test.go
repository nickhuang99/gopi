package lineedit

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// palette_hermes_test.go — the acceptance for the Hermes-derived GOLD palette:
// the answer BODY getting a colour, and the capability gate that decides whether
// a 24-bit table may be drawn at all.
//
// The three properties this file exists to pin, in the order the epic states
// them:
//
//  1. the gold sequence reaches the answer body ONLY when the capability says the
//     terminal advertises truecolour — an 8-colour / TERM=dumb / NO_COLOR run
//     contains none;
//  2. changing the palette never moves a CELL: the same run under
//     MonoPalette / BasicPalette / HermesDark / HermesLight puts the same
//     characters in the same grid cells;
//  3. the status-row invariant survives the new tables: the gold tables carry no
//     theme-dependent colour on the status row either.

// hermesDarkAnswerGold is the packed colour the epic's headline names:
// Hermes DARK_THEME.text, #FFF8DC. Spelled as its three channels here (rather
// than as a sixth copy of the constant) so the assertion is against the WIRE
// spelling the terminal receives.
const (
	hermesGoldAnswerR = 0xFF
	hermesGoldAnswerG = 0xF8
	hermesGoldAnswerB = 0xDC
)

// hermesGoldAnswerSeq is the sequence the answer body must open with under the
// dark gold table: `38;2;255;248;220`, composed through internal/sgr's own
// helper so the test asserts the same spelling the palette emits.
func hermesGoldAnswerSeq() string {
	return sgr.Seq(sgr.TruecolorFgParams(hermesGoldAnswerR, hermesGoldAnswerG, hermesGoldAnswerB))
}

// ---------------------------------------------------------------------------
// 1. The capability gate: the gold table is drawn only where 24 bits exist.
// ---------------------------------------------------------------------------

// TestPaletteTruecolorIsGatedByTheCapability is the gate itself. Each row is a
// real terminal shape, and the assertion is on the TABLE the resolver returns —
// never on a variable it happened to read.
func TestPaletteTruecolorIsGatedByTheCapability(t *testing.T) {
	cases := []struct {
		name        string
		palette     string
		colorTerm   string
		noColor     bool
		wantTable   Palette
		wantName    string
		truecolorOK bool
		// want256OK: the terminal in this row declares 256 colours, so the tier
		// is reported — the row's TERM is "xterm-256color" unless stated.
		want256OK bool
	}{
		{
			name: "hermes on a terminal that advertises 24-bit", palette: PaletteNameHermes,
			colorTerm: ColorTermTruecolor, wantTable: HermesDarkPalette, truecolorOK: true, want256OK: true,
		},
		{
			name: "hermes-light on a terminal that advertises 24-bit", palette: PaletteNameHermesLight,
			colorTerm: ColorTermTruecolor, wantTable: HermesLightPalette, truecolorOK: true, want256OK: true,
		},
		{
			name: "the advertisement is case-insensitive", palette: PaletteNameHermes,
			colorTerm: "TRUECOLOR", wantTable: HermesDarkPalette, truecolorOK: true, want256OK: true,
		},
		{
			// THE FALLBACK ROW, and the one this epic changed: a terminal with
			// no 24-bit advertisement must NOT receive `38;2;…`, whose parameters
			// it would print as text. It is not dropped to the 8-colour floor
			// either — a terminal that declares 256 colours gets the 256 table,
			// the gold family's own approximation (task 01a0dcd4: the binary
			// collapse to BasicPalette was the defect this row records).
			name: "hermes on a terminal that advertises nothing gets the 256 table", palette: PaletteNameHermes,
			wantTable: Palette256, want256OK: true,
		},
		{
			name: "hermes-light without the advertisement gets the 256 table", palette: PaletteNameHermesLight,
			wantTable: Palette256, want256OK: true,
		},
		{
			// A value that is not the truecolour advertisement is not an
			// advertisement: COLORTERM is also used for "24bit" and for
			// terminal names, and neither promises 38;2 support here. The 256
			// tier is a property of TERM, so it applies regardless.
			name: "a non-truecolor COLORTERM value does not enable it", palette: PaletteNameHermes,
			colorTerm: "24bit", wantTable: Palette256, want256OK: true,
		},
		{
			name: "basic needs no capability", palette: PaletteNameBasic,
			wantTable: BasicPalette, want256OK: true,
		},
		{
			// Colour off outranks everything: the gold table is not "a table we
			// would have used", it is not used.
			name: "colour off beats the advertisement", palette: PaletteNameHermes,
			colorTerm: ColorTermTruecolor, noColor: true, wantTable: MonoPalette,
		},
	}
	// A pty is what "the sink is a terminal" means; without one the decision
	// short-circuits at the sink and every row would test the wrong rule.
	master := paletteOpenPty(t)
	defer func() { _ = master.Close() }() //stylegate:ignore closing the pty master is teardown, not a step whose failure matters
	tty := paletteSlaveFile(t, master)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvPalette, tc.palette)
			// An EMPTY value is what ColourWanted's os.Getenv sees for an
			// unadvertised terminal, so the absent case needs no unset dance.
			t.Setenv(EnvColorTerm, tc.colorTerm)
			// The rows below are all "a terminal that declares 256 colours but
			// does not advertise 24-bit" — the shape this epic's tier exists for.
			// The 8-colour floor has its own rows in palette256_test.go.
			t.Setenv(EnvTerm, term256)

			cap := CapabilityForEnv(tc.noColor, tty)
			got := PaletteForCapability(cap)
			if got != tc.wantTable {
				t.Fatalf("PaletteForCapability(%+v) = %+v, want %+v", cap, got, tc.wantTable)
			}
			if cap.Truecolor() != tc.truecolorOK {
				t.Fatalf("capability reports truecolor=%v, want %v", cap.Truecolor(), tc.truecolorOK)
			}
			if cap.Palette256() != tc.want256OK {
				t.Fatalf("capability reports 256 colours=%v, want %v (TERM=%s)", cap.Palette256(), tc.want256OK, term256)
			}
			// The property that matters on the WIRE: a truecolour sequence is
			// present exactly when the advertisement was, and a 256 sequence
			// exactly when the table's own RequiredDepth is the 256 tier (the
			// 8-colour table carries neither).
			if gotTruecolor := strings.Contains(got.Answer.Open(), truecolorMarker()); gotTruecolor != tc.truecolorOK {
				t.Fatalf("answer body carries a truecolour sequence = %v, want %v (open=%q)",
					gotTruecolor, tc.truecolorOK, got.Answer.Open())
			}
			want256Seq := got.RequiredDepth() == Depth256
			if got256 := strings.Contains(got.Answer.Open(), palette256Marker()); got256 != want256Seq {
				t.Fatalf("answer body carries a 256-colour sequence = %v, want %v (open=%q, RequiredDepth=%v)",
					got256, want256Seq, got.Answer.Open(), got.RequiredDepth())
			}
		})
	}
}

// TestNoCapabilityEverSelectsATruecolorTable is the enumeration the gate's
// correctness actually rests on: for EVERY combination of (knob, COLORTERM,
// colour-off) the returned table paints a truecolour colour only where the
// capability reports truecolor. A hand-picked case list cannot show this,
// because the failing combination is the one nobody thought of.
func TestNoCapabilityEverSelectsATruecolorTable(t *testing.T) {
	names := append([]string{""}, PaletteNames()...)
	names = append(names, "gold", "unknown")
	master := paletteOpenPty(t)
	defer func() { _ = master.Close() }() //stylegate:ignore closing the pty master is teardown, not a step whose failure matters
	tty := paletteSlaveFile(t, master)

	for _, name := range names {
		for _, colorTerm := range []string{"", ColorTermTruecolor, "24bit"} {
			for _, noColor := range []bool{false, true} {
				t.Run(name+"/"+colorTerm+"/noColor="+boolWord(noColor), func(t *testing.T) {
					t.Setenv(EnvPalette, name)
					t.Setenv(EnvColorTerm, colorTerm)
					cap := CapabilityForEnv(noColor, tty)
					pal := PaletteForCapability(cap)
					if pal.NeedsTruecolor() && !cap.Truecolor() {
						t.Fatalf("resolved a truecolour table with truecolor=%v (GOPI_PALETTE=%q COLORTERM=%q noColor=%v)",
							cap.Truecolor(), name, colorTerm, noColor)
					}
					if cap.Colour() == ColourOff && hasSGR(pal.Answer.Wrap("x")) {
						t.Fatalf("colour-off capability produced an emitting table (GOPI_PALETTE=%q)", name)
					}
				})
			}
		}
	}
}

// boolWord renders a bool for a subtest name (a name is read in a failure).
func boolWord(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// stripAttrs removes the given attribute openings from a style's sequence and
// returns what is left. It is how "this sequence is ATTRIBUTES ONLY" is asserted
// positively: an empty remainder means every opening in it was an allowed
// attribute — no substring test can pass a colour that way.
func stripAttrs(seq string, allowed []string) string {
	for _, a := range allowed {
		seq = strings.ReplaceAll(seq, a, "")
	}
	return seq
}

// TestPaletteSelectionDefaultsAndReportsUnknownNames pins the knob's two edges:
// an unset/empty name is the UNSET sentinel (nobody chose — the capability
// settles it), and a name the resolver does not know is REFUSED rather than
// obeyed — the run then follows the terminal, which is what the notice says
// (Rule 15: no silent failure).
func TestPaletteSelectionDefaultsAndReportsUnknownNames(t *testing.T) {
	t.Setenv(EnvPalette, "")
	if got := PaletteSelection(); got != PaletteNameUnset {
		t.Fatalf("PaletteSelection() with an empty %s = %q, want the unset sentinel %q", EnvPalette, got, PaletteNameUnset)
	}
	t.Setenv(EnvPalette, "  "+PaletteNameHermes+"  ")
	if got := PaletteSelection(); got != PaletteNameHermes {
		t.Fatalf("PaletteSelection() = %q, want the trimmed %q", got, PaletteNameHermes)
	}
	// The refused name is reported (its own test asserts the message); here the
	// assertion is on the VALUE the run continues with.
	captureStderr(t, func() {
		t.Setenv(EnvPalette, "gold-ish")
		if got := PaletteSelection(); got != PaletteNameUnset {
			t.Fatalf("PaletteSelection() with an unknown name = %q, want the unset sentinel %q", got, PaletteNameUnset)
		}
	})
	for _, name := range PaletteNames() {
		pal, ok := PaletteByName(name)
		if !ok {
			t.Fatalf("PaletteByName(%q) is not known, but it is advertised as a legal value", name)
		}
		if pal == MonoPalette {
			t.Fatalf("PaletteByName(%q) resolved to the zero table — a named palette must emit", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. The answer BODY: the epic's headline, asserted on the emitted bytes.
// ---------------------------------------------------------------------------

// TestHermesAnswerBodyCarriesTheGoldText is the headline change: under the gold
// table the answer body opens with Hermes' own text colour — #FFF8DC, the value
// the epic names — while the same body under MonoPalette is the bare text.
func TestHermesAnswerBodyCarriesTheGoldText(t *testing.T) {
	answer := "the answer body must be gold\n"
	gold := paletteAnswerCapture(t, HermesDarkPalette, answer)
	mono := paletteAnswerCapture(t, MonoPalette, answer)

	want := hermesGoldAnswerSeq()
	if !strings.Contains(gold, want) {
		t.Fatalf("the gold capture does not carry the body colour %q:\n%q", want, gold)
	}
	if got := HermesDarkPalette.Answer.Open(); got != want {
		t.Fatalf("HermesDarkPalette.Answer.Open() = %q, want the Hermes text colour %q", got, want)
	}
	if BasicPalette.Answer.Empty() == false {
		t.Fatalf("BasicPalette.Answer is no longer the unstyled baseline (open=%q) — the fallback table's body must stay as it was", BasicPalette.Answer.Open())
	}
	if hasSGR(mono) {
		t.Fatalf("the monochrome capture carries SGR:\n%q", mono)
	}
	// The light table is the same role on a light background — a different
	// colour, never a different structure.
	if HermesLightPalette.Answer.Empty() {
		t.Fatal("HermesLightPalette.Answer is unstyled — the light table would leave the body plain")
	}
	if HermesLightPalette.Answer.Open() == HermesDarkPalette.Answer.Open() {
		t.Fatal("the light and dark tables paint the body with the same colour — one of them is wrong")
	}
	if got := HermesLightPalette.Answer.Open(); !strings.Contains(got, truecolorMarker()) {
		t.Fatalf("HermesLightPalette.Answer = %q, want a 24-bit colour", got)
	}
}

// TestGoldPalettesCoverEverySurface asserts the two gold tables actually name
// every colour-carrying surface: a field left at the zero style would render
// plain even with the palette on, which no other test would notice (a plain field
// is indistinguishable from a correctly-monochrome one).
func TestGoldPalettesCoverEverySurface(t *testing.T) {
	// The status row is excluded because it is checked separately, field by
	// field, BELOW — not because it is colourless: it carries the table's own
	// gold as a 24-bit foreground (task 01a0e2e6), where the surfaces listed
	// here carry the same colour through their own fields. The body's own
	// baseline is excluded for the same reason.
	coloured := []string{"banner", "prompt", "answer", "answerBold", "code", "diffAdd", "diffDel", "hint", "warning", "error", "toolCall"}
	for _, tc := range []struct {
		name string
		pal  Palette
	}{
		{"HermesDarkPalette", HermesDarkPalette},
		{"HermesLightPalette", HermesLightPalette},
	} {
		byName := map[string]Style{
			"banner": tc.pal.Banner, "prompt": tc.pal.Prompt,
			"answer": tc.pal.Answer, "answerBold": tc.pal.AnswerBold,
			"code": tc.pal.Code, "diffAdd": tc.pal.DiffAdd, "diffDel": tc.pal.DiffDel,
			"hint": tc.pal.Hint, "warning": tc.pal.Warning, "error": tc.pal.Error,
			"toolCall": tc.pal.ToolCall,
		}
		for _, name := range coloured {
			st := byName[name]
			if st.Empty() {
				t.Errorf("%s.%s is empty — the surface would render plain with the palette on", tc.name, name)
			}
			if !strings.Contains(st.Open(), truecolorMarker()) {
				t.Errorf("%s.%s = %q, want a 24-bit colour (the gold family is not expressible in the 16 named ones)", tc.name, name, st.Open())
			}
		}
		// The status row carries Hermes' OWN gold roles as 24-bit foregrounds
		// (task 01a0e2e6): the label takes `label` (#DAA520), the live/tool row
		// takes `primary` (#FFD700), and the thinking state takes `muted`
		// (#CC9B1F) — the attributes are still in front of the colour.
		//
		// The predicate is POSITIVE and exact: strip the allowed ATTRIBUTE
		// openings and what is left must be a truecolour sequence, so a row that
		// fell back to the default foreground (or to a named code) cannot pass a
		// substring test — and a colour cannot hide behind one.
		allowedAttrs := []string{sgrBold, sgrItalic, sgrUnder, sgrReverse}
		wantGold := map[string]int{
			"statusLabel": hermesDarkLabel,
			"statusLive":  hermesDarkPrimary,
			"thinking":    hermesDarkMuted,
			"toolRow":     hermesDarkPrimary,
		}
		if tc.name == "HermesLightPalette" {
			wantGold = map[string]int{
				"statusLabel": hermesLightLabel,
				"statusLive":  hermesLightPrim,
				"thinking":    hermesLightLabel,
				"toolRow":     hermesLightPrim,
			}
		}
		for name, st := range map[string]Style{
			"statusLabel": tc.pal.StatusLabel, "statusLive": tc.pal.StatusLive,
			"thinking": tc.pal.Thinking, "toolRow": tc.pal.ToolRow,
		} {
			if st.Empty() {
				t.Errorf("%s.%s is empty — the status row would carry no emphasis at all", tc.name, name)
			}
			// Attributes first, then the colour: strip the attributes and the
			// remainder must BE the sequence of the Hermes role this row state
			// is supposed to paint.
			want := truecolorFg(wantGold[name]).Open()
			if rest := stripAttrs(st.Open(), allowedAttrs); rest != want {
				t.Errorf("%s.%s = %q carries %q, want exactly the Hermes gold %q (%s) on top of its attributes",
					tc.name, name, st.Open(), rest, want, truecolorToken(wantGold[name]))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 3. Style is additive: it moves no cell and hides no character.
// ---------------------------------------------------------------------------

// TestPaletteChangeNeverMovesACell is the sizing gate for the palette swap: the
// same answer rendered under all four tables must occupy the SAME grid cells with
// the SAME characters. It replays each capture through the cell grid, so a
// renderer that measured an SGR byte as a column would fail here — and so would a
// table whose sequence happened to include a printable parameter.
func TestPaletteChangeNeverMovesACell(t *testing.T) {
	answer := "a wide 答案 line\n```go\nfunc main() {}\n```\n-old\n+new\nend\n"
	tables := []struct {
		name string
		pal  Palette
	}{
		{"MonoPalette", MonoPalette},
		{"BasicPalette", BasicPalette},
		{"Palette256", Palette256},
		{"HermesDarkPalette", HermesDarkPalette},
		{"HermesLightPalette", HermesLightPalette},
	}
	var base string
	var baseName string
	for _, tc := range tables {
		capture := paletteAnswerCapture(t, tc.pal, answer)
		sc := NewScreen(paletteTestCols, paletteTestRows)
		sc.Feed([]byte(capture))
		got := sc.Contents()
		if base == "" {
			base, baseName = got, tc.name
			// The grid must actually hold the answer: a comparison of two empty
			// screens would pass and prove nothing.
			if !strings.Contains(got, "答案") || !strings.Contains(got, "+new") {
				t.Fatalf("%s: the answer did not reach the grid:\n%s", tc.name, got)
			}
			continue
		}
		if got != base {
			t.Fatalf("the grid differs between %s and %s — a style moved a cell:\n %s:\n%s\n %s:\n%s",
				baseName, tc.name, baseName, base, tc.name, got)
		}
	}
}

// TestGoldAnswerEmphasisIsAdditiveAndNotColourAlone is the monochrome rule for
// the new body colour: the gold capture and the basic capture read as the SAME
// characters, so a reader whose terminal has no 24-bit colour loses nothing but
// the gold.
func TestGoldAnswerEmphasisIsAdditiveAndNotColourAlone(t *testing.T) {
	answer := "prose with **emphasis**\ncode below\n```\nfenced\n```\n+added\n-removed\n"
	gold := paletteAnswerCapture(t, HermesDarkPalette, answer)
	basic := paletteAnswerCapture(t, BasicPalette, answer)
	mono := paletteAnswerCapture(t, MonoPalette, answer)

	if !hasSGR(gold) {
		t.Fatal("the gold capture carries no SGR at all — the test would prove nothing")
	}
	if stripSGR(gold) != stripSGR(basic) {
		t.Fatalf("the gold palette changed the answer's TEXT relative to the basic one:\n gold %q\n basic %q", stripSGR(gold), stripSGR(basic))
	}
	if stripSGR(gold) != stripSGR(mono) {
		t.Fatalf("the gold palette conveys something the monochrome run does not:\n gold %q\n mono %q", stripSGR(gold), stripSGR(mono))
	}
	// Every word a reader needs survives with colour off.
	for _, want := range []string{"prose with", "emphasis", "fenced", "+added", "-removed"} {
		if !strings.Contains(stripSGR(gold), want) {
			t.Errorf("the stripped gold capture lost %q:\n%s", want, stripSGR(gold))
		}
	}
	// The intra-line emphasis is WEIGHT, not a second hue on the same role:
	// the gold table's AnswerBold must still carry bold.
	if !strings.Contains(HermesDarkPalette.AnswerBold.Open(), sgrBold) {
		t.Errorf("HermesDarkPalette.AnswerBold = %q, want bold in it", HermesDarkPalette.AnswerBold.Open())
	}
}

// ---------------------------------------------------------------------------
// 4. The status row's own guard, applied to the new tables.
// ---------------------------------------------------------------------------

// TestGoldTablesLeaveTheStatusRowThemeIndependent extends HEAD's salience guard
// (palette_status_salience_test.go) to the tables this change touches: the
// composed row under a GOLD table carries no code whose rendering the terminal's
// theme decides, and it carries the table's own gold (task 01a0e2e6).
func TestGoldTablesLeaveTheStatusRowThemeIndependent(t *testing.T) {
	banned := themeResolvedStatusCodes()
	label := "glm-5.3-flash" + StatusSep + "turn 2" + StatusSep + ToolGlyph + " bash git diff" + StatusSep + "1.2k tokens"
	for _, tc := range []struct {
		name      string
		pal       Palette
		wantGreen string
	}{
		{"HermesDarkPalette", HermesDarkPalette, "truecolor:"},
		{"HermesLightPalette", HermesLightPalette, "truecolor:"},
		{"Palette256", Palette256, "256:"},
	} {
		for _, st := range []Status{
			{Label: label, Live: "ready"},
			{Label: label, Live: "thinking…", Thinking: true},
			{Label: label},
		} {
			row := styledStatusLine(st, 200, tc.pal)
			// Codes read as CODES (a substring test over the row cannot tell
			// italic's "3" from the extended colour's parameters).
			for _, code := range statusRowSGRCodes(row) {
				if name, bad := banned[escCSI+code+"m"]; bad {
					t.Errorf("%s status row carries %s (SGR %s):\n%q", tc.name, name, code, row)
				}
			}
			// The colour is this table's OWN gold, in the spelling its tier pins:
			// a truecolour table may not fall back to a named code, and neither
			// may the 256 tier — that fallback IS the operator's "default color".
			gold := statusRowColourSpellings(row)
			if len(gold) == 0 {
				t.Errorf("%s status row selects no colour at all:\n%q", tc.name, row)
			}
			for _, spelling := range gold {
				if !strings.HasPrefix(spelling, tc.wantGreen) {
					t.Errorf("%s status row selects %s, want a %s colour:\n%q", tc.name, spelling, tc.wantGreen, row)
				}
			}
			if tc.name != "Palette256" {
				if !strings.Contains(row, sgrSeq(truecolorParams(hermesDarkPrimary))) && tc.name == "HermesDarkPalette" {
					t.Errorf("%s status row carries none of Hermes' primary #FFD700:\n%q", tc.name, row)
				}
				if !strings.Contains(row, sgrSeq(truecolorParams(hermesLightPrim))) && tc.name == "HermesLightPalette" {
					t.Errorf("%s status row carries none of Hermes' light primary #8B6914:\n%q", tc.name, row)
				}
			}
			if !strings.Contains(row, escCSI) {
				t.Errorf("%s status row carries no emphasis at all:\n%q", tc.name, row)
			}
			// And the row's TEXT is the plain row's: the palette moved no cell.
			plain := st
			if stripSGR(row) != statusLine(plain, 200) {
				t.Errorf("%s status row text differs from the plain row:\n got %q\nwant %q",
					tc.name, stripSGR(row), statusLine(plain, 200))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 5. The zero table stays zero, whichever table would have been selected.
// ---------------------------------------------------------------------------

// TestColourOffEmitsNothingRegardlessOfTheSelectedPalette is the byte-exact half
// for the new knob: whatever the operator ASKED for, a colour-off capability (a
// pipe, TERM=dumb, NO_COLOR, --no-color) produces the zero table and an
// SGR-free stream. The knob may never be a way around the capability.
func TestColourOffEmitsNothingRegardlessOfTheSelectedPalette(t *testing.T) {
	for _, name := range append([]string{""}, PaletteNames()...) {
		t.Run("palette="+name, func(t *testing.T) {
			t.Setenv(EnvPalette, name)
			t.Setenv(EnvColorTerm, ColorTermTruecolor)
			for _, off := range []struct {
				name string
				pal  Palette
			}{
				{"flag", PaletteForEnvCapability(true, nil)},
				{"no sink", PaletteForEnvCapability(false, nil)},
				{"PaletteFor(ColourOff)", PaletteFor(ColourOff)},
				{"mono", MonoPalette},
			} {
				if hasSGR(off.pal.Answer.Wrap("x") + off.pal.Banner.Wrap("y") + off.pal.Error.Wrap("z")) {
					t.Fatalf("%s: palette %q emitted SGR under a colour-off capability:\n%+v", off.name, name, off.pal)
				}
				// The whole ANSWER PATH, not just one field.
				capture := paletteAnswerCapture(t, off.pal, "answer\n```\ncode\n```\n")
				if hasSGR(capture) {
					t.Fatalf("%s: palette %q emitted SGR through the answer renderer:\n%q", off.name, name, capture)
				}
			}
		})
	}
}
