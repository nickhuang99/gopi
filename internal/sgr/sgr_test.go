package sgr

import (
	"strconv"
	"strings"
	"testing"
)

// parseParams decodes a parameter list the way the wire carries it — digits
// separated by ParamSeparator — and folds it through the package's interpreter.
//
// It is deliberately built here rather than in the package: the production
// reader is a CSI STATE MACHINE (internal/lineedit/screen.go) that walks a byte
// stream and hands ApplyParam one parameter at a time. What this test pins is the
// interpreter those bytes end up in; the integration test
// (internal/lineedit TestScreenAttrRoundTripThroughFeed) drives the real state
// machine, so the two layers are covered by the tests that own them.
func parseParams(t *testing.T, params string) Style {
	t.Helper()
	style := Default()
	list := make([]int, 0, 8)
	for _, field := range strings.Split(params, string(ParamSeparator)) {
		n, err := strconv.Atoi(field)
		if err != nil {
			t.Fatalf("parameter %q is not a number: %v", field, err)
		}
		list = append(list, n)
	}
	for i := 0; i < len(list); {
		i = ApplyParam(&style, list, i) + 1
	}
	return style
}

// parseSeq decodes an sgr sequence (CSI params m) published by the builders.
func parseSeq(t *testing.T, seq string) Style {
	t.Helper()
	if !strings.HasPrefix(seq, CSI) || !strings.HasSuffix(seq, string(FinalSGR)) {
		t.Fatalf("%q is not an SGR sequence", seq)
	}
	return parseParams(t, strings.TrimSuffix(strings.TrimPrefix(seq, CSI), string(FinalSGR)))
}

// TestFgBgIndexClassification pins the parameter → palette-index mapping: the
// base range, the bright range, and the parameters that name no colour at all.
func TestFgBgIndexClassification(t *testing.T) {
	for _, tc := range []struct {
		param  int
		idx    int
		isFg   bool
		reason string
	}{
		{FgBase + Black, Black, true, "SGR 30 is black"},
		{FgBase + Red, Red, true, "SGR 31 is red"},
		{FgLast, White, true, "SGR 37 is the last base foreground"},
		{BrightFgBase + Black, BrightBlack, true, "the bright range starts at BrightBlack"},
		{BrightFgLast, BrightWhite, true, "SGR 97 is the last bright foreground"},
		{DefaultFg, DefaultColor, false, "SGR 39 names no colour, it restores the default"},
		{BgBase, Black, false, "SGR 40 is a BACKGROUND, not a foreground"},
		{Reset, DefaultColor, false, "SGR 0 is a reset, not a colour"},
		{ExtendedFg, DefaultColor, false, "SGR 38 introduces a colour, it is not one"},
		{99, DefaultColor, false, "a parameter outside every colour range"},
	} {
		got, ok := FgIndex(tc.param)
		if ok != tc.isFg || (ok && got != tc.idx) {
			t.Errorf("FgIndex(%d) = (%d, %v), want (%d, %v): %s", tc.param, got, ok, tc.idx, tc.isFg, tc.reason)
		}
	}
	for _, tc := range []struct {
		param int
		idx   int
		ok    bool
	}{
		{BgBase + Red, Red, true},
		{BgLast, White, true},
		{BrightBgBase + Black, BrightBlack, true},
		{BrightBgLast, BrightWhite, true},
		{DefaultBg, DefaultColor, false},
		{FgBase, DefaultColor, false},
	} {
		got, ok := BgIndex(tc.param)
		if ok != tc.ok || (ok && got != tc.idx) {
			t.Errorf("BgIndex(%d) = (%d, %v), want (%d, %v)", tc.param, got, ok, tc.idx, tc.ok)
		}
	}
	// The two spellings of one colour must collapse onto one index: that is what
	// makes "bright red" and "red+bright" the same colour.
	if idx, _ := FgIndex(BrightFgBase + Red); idx != BrightRed {
		t.Errorf("SGR %d names bright red, got index %d", BrightFgBase+Red, idx)
	}
}

// TestSequenceBuildersPinTheWireFormat pins the exact bytes: these sequences are
// what an emitter writes and what the parser reads, so a changed parameter number
// must fail here rather than in a terminal.
func TestSequenceBuildersPinTheWireFormat(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{ResetSeq(), "\x1b[0m"},
		{BoldSeq(), "\x1b[1m"},
		{DimSeq(), "\x1b[2m"},
		{ItalicSeq(), "\x1b[3m"},
		{UnderlineSeq(), "\x1b[4m"},
		{ReverseSeq(), "\x1b[7m"},
		{FgSeq(Black), "\x1b[30m"},
		{FgSeq(Red), "\x1b[31m"},
		{FgSeq(BrightWhite), "\x1b[97m"},
		{FgSeq(208), "\x1b[38;5;208m"},
		{BgSeq(Red), "\x1b[41m"},
		{BgSeq(BrightBlack), "\x1b[100m"},
		{BgSeq(17), "\x1b[48;5;17m"},
		{TruecolorFgSeq(18, 52, 86), "\x1b[38;2;18;52;86m"},
		{TruecolorBgSeq(1, 2, 3), "\x1b[48;2;1;2;3m"},
		{Seq("1;2"), "\x1b[1;2m"},
		// The bracketed-paste FRAMING (what a terminal sends around a pasted
		// block) is a different sequence from the bracketed-paste MODE
		// (BracketedPasteSeq, `CSI ? 2004 h`, what a program tells the terminal):
		// a reader that confused the two would drop the wrong bytes.
		{BracketedPasteMarkerSeq(PasteMarkerStartParam), "\x1b[200~"},
		{BracketedPasteMarkerSeq(PasteMarkerEndParam), "\x1b[201~"},
		{BracketedPasteSeq(true), "\x1b[?2004h"},
	} {
		if tc.got != tc.want {
			t.Errorf("sequence = %q, want %q", tc.got, tc.want)
		}
	}
	// A channel outside a byte is clamped, not wrapped: the wire carries bytes.
	if got := TruecolorFgSeq(256, -1, 255); got != "\x1b[38;2;255;0;255m" {
		t.Errorf("clamped truecolour = %q, want the channels bounded to a byte", got)
	}
}

// TestStyleRoundTrip is the emit/parse agreement: a style rendered as a sequence
// must read back as the SAME style, for every attribute and every colour space.
// This is the guard against the two sides of the protocol drifting apart.
func TestStyleRoundTrip(t *testing.T) {
	base := Default()
	withAll := base
	withAll.Bold, withAll.Dim, withAll.Italic, withAll.Underline, withAll.Reverse = true, true, true, true, true

	for _, tc := range []struct {
		name  string
		style Style
	}{
		{"default", base},
		{"bold", func() Style { s := base; s.Bold = true; return s }()},
		{"dim", func() Style { s := base; s.Dim = true; return s }()},
		{"italic", func() Style { s := base; s.Italic = true; return s }()},
		{"underline", func() Style { s := base; s.Underline = true; return s }()},
		{"reverse", func() Style { s := base; s.Reverse = true; return s }()},
		{"all emphasis", withAll},
		{"fg black", func() Style { s := base; s.Fg = Black; return s }()},
		{"fg white", func() Style { s := base; s.Fg = White; return s }()},
		{"fg bright red", func() Style { s := base; s.Fg = BrightRed; return s }()},
		{"fg 256", func() Style { s := base; s.Fg = 208; return s }()},
		{"bg 256", func() Style { s := base; s.Bg = 0; return s }()},
		{"fg truecolour", func() Style { s := base; s.FgRGB = RGB(18, 52, 86); return s }()},
		{"bg truecolour", func() Style { s := base; s.BgRGB = RGB(255, 0, 255); return s }()},
		{"emphasis + both colours", func() Style {
			s := withAll
			s.Fg, s.Bg = BrightCyan, 236
			return s
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seq := tc.style.Seq()
			got := parseSeq(t, seq)
			if !got.Equal(tc.style) {
				t.Fatalf("%q parsed back as %s, want %s", seq, got, tc.style)
			}
		})
	}
}

// TestStyleSeqIsAbsolute asserts a style's sequence does not depend on what came
// before it: the sequence resets first, so applying it after an unrelated style
// yields the style itself — a relative specification would inherit the green.
func TestStyleSeqIsAbsolute(t *testing.T) {
	want := Default()
	want.Bold = true
	want.Fg = Blue

	state := Default()
	state.Fg, state.Italic, state.Underline = Green, true, true
	apply := func(params string) {
		list := make([]int, 0, 8)
		for _, f := range strings.Split(params, string(ParamSeparator)) {
			n, _ := strconv.Atoi(f) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
			list = append(list, n)
		}
		for i := 0; i < len(list); {
			i = ApplyParam(&state, list, i) + 1
		}
	}
	apply(strings.TrimSuffix(strings.TrimPrefix(want.Seq(), CSI), string(FinalSGR)))

	if !state.Equal(want) {
		t.Fatalf("state after an absolute spec = %s, want %s", state, want)
	}
	if state.Italic || state.Underline {
		t.Fatal("the absolute spec inherited attributes from the previous state")
	}
}

// TestApplyParamIncrementalSemantics pins the accumulator rules a terminal
// follows: an attribute turns ON and stays on (only a reset clears it), and a
// colour is REPLACED per family.
func TestApplyParamIncrementalSemantics(t *testing.T) {
	// Bold then red: bold survives the colour change.
	got := parseParams(t, "1"+string(ParamSeparator)+"31")
	if !got.Bold || got.Fg != Red {
		t.Fatalf("bold+red = %s, want bold and red", got)
	}
	// Two colours: the last one wins, and the previous family is replaced.
	got = parseParams(t, "31"+string(ParamSeparator)+"34")
	if got.Fg != Blue {
		t.Fatalf("red then blue = %s, want blue (the last colour wins)", got)
	}
	// fg and bg are independent.
	got = parseParams(t, "31"+string(ParamSeparator)+"44")
	if got.Fg != Red || got.Bg != Blue {
		t.Fatalf("fg red + bg blue = %s", got)
	}
	// The default restores one family only, leaving the other alone.
	got = parseParams(t, "31"+string(ParamSeparator)+"44"+string(ParamSeparator)+"39")
	if got.Fg != DefaultColor || got.Bg != Blue {
		t.Fatalf("after SGR 39 = %s, want the background kept", got)
	}
	// The reset clears everything, including the emphasis attributes.
	got = parseParams(t, "1;4;31;44"+string(ParamSeparator)+"0")
	if !got.Equal(Default()) {
		t.Fatalf("after SGR 0 = %s, want default", got)
	}
}

// TestApplyParamExtendedColorConsumption pins how an extended colour eats the
// parameters after it — including the TRUNCATED case, where the payload's digits
// must not be read as further attributes (`38;2;1;2` is not "dim, bold").
func TestApplyParamExtendedColorConsumption(t *testing.T) {
	got := parseParams(t, "38"+string(ParamSeparator)+"5"+string(ParamSeparator)+"208")
	if got.Fg != 208 || got.FgRGB != NoRGB {
		t.Fatalf("38;5;208 = %s, want palette 208", got)
	}
	got = parseParams(t, "38"+string(ParamSeparator)+"2"+string(ParamSeparator)+"18"+
		string(ParamSeparator)+"52"+string(ParamSeparator)+"86")
	if got.FgRGB != RGB(18, 52, 86) || got.Fg != DefaultColor {
		t.Fatalf("38;2;18;52;86 = %s, want truecolour #123456", got)
	}
	got = parseParams(t, "48"+string(ParamSeparator)+"5"+string(ParamSeparator)+"17")
	if got.Bg != 17 {
		t.Fatalf("48;5;17 = %s, want background palette 17", got)
	}
	// A colour after an extended colour replaces it in its own family.
	got = parseParams(t, "38;5;208"+string(ParamSeparator)+"34")
	if got.Fg != Blue || got.FgRGB != NoRGB {
		t.Fatalf("palette then named = %s, want blue and no RGB left", got)
	}
	// Truncated payloads: the colour is NOT applied and the digits are consumed.
	for _, params := range []string{"38;2;1", "38;2;1;2", "38;5", "38"} {
		got = parseParams(t, params)
		if !got.Equal(Default()) {
			t.Errorf("%q = %s, want default (a truncated colour applies nothing and consumes the tail)", params, got)
		}
	}
	// An unknown selector names no colour, and the parameters after it are
	// ordinary parameters again — so only the colour is lost, not the rest of the
	// list. (`38;9;1` is "a colour this grid does not know" + bold.)
	got = parseParams(t, "38;9;1")
	if got.Fg != DefaultColor || got.FgRGB != NoRGB {
		t.Errorf("38;9;1 = %s, want no colour applied (selector 9 names none)", got)
	}
	if !got.Bold {
		t.Errorf("38;9;1 = %s, want bold (the parameter after an unknown selector is an ordinary one)", got)
	}
}

// TestApplyParamIgnoresUnknownParameters asserts an unmodelled parameter is
// skipped rather than guessed at, and does not disturb the modelled ones.
func TestApplyParamIgnoresUnknownParameters(t *testing.T) {
	got := parseParams(t, "1;53;31")
	if !got.Bold || got.Fg != Red {
		t.Fatalf("bold+overline+red = %s, want bold and red", got)
	}
	if got.Dim || got.Italic || got.Underline || got.Reverse {
		t.Fatalf("an ignored parameter set an attribute: %s", got)
	}
}

// TestApplyParamBoundsAndTruncatedList asserts the interpreter is safe at the
// edges: an out-of-range index is a no-op, and a list that ENDS on an extended
// colour changes nothing rather than reading past its end.
func TestApplyParamBoundsAndTruncatedList(t *testing.T) {
	style := Default()
	if got := ApplyParam(&style, nil, 0); got != 0 {
		t.Errorf("ApplyParam on an empty list returned %d, want 0", got)
	}
	if got := ApplyParam(&style, []int{Bold}, 5); got != 5 {
		t.Errorf("ApplyParam past the end returned %d, want 5", got)
	}
	if !style.Equal(Default()) {
		t.Fatalf("an out-of-range call changed the style: %s", style)
	}
	// A list ending exactly on the extended selector consumes all of it.
	last := ApplyParam(&style, []int{ExtendedFg}, 0)
	if last != 0 || !style.Equal(Default()) {
		t.Errorf("a bare 38 consumed %d parameters and left %s, want 0 and default", last, style)
	}
}

// TestRGBPacking pins the packed value the grid stores: the channels in order,
// each bounded to a byte, and extraction returning them back.
func TestRGBPacking(t *testing.T) {
	for _, tc := range []struct {
		r, g, b int
		want    int
	}{
		{0, 0, 0, 0x000000},
		{255, 255, 255, 0xffffff},
		{18, 52, 86, 0x123456},
		{256, -1, 300, 0xff00ff}, // clamped, not wrapped
	} {
		got := RGB(tc.r, tc.g, tc.b)
		if got != tc.want {
			t.Errorf("RGB(%d,%d,%d) = %#06x, want %#06x", tc.r, tc.g, tc.b, got, tc.want)
		}
		if r, g, b := rgbRed(got), rgbGreen(got), rgbBlue(got); RGB(r, g, b) != got {
			t.Errorf("channels of %#06x do not round-trip: %d,%d,%d", got, r, g, b)
		}
	}
	// NoRGB is not a valid colour, so it can never be mistaken for one.
	if NoRGB >= 0 && NoRGB <= 0xffffff {
		t.Fatalf("NoRGB = %d is inside the colour range", NoRGB)
	}
	// DefaultColor is outside the palette, so it can never be a real entry.
	if DefaultColor >= 0 && DefaultColor < Palette256 {
		t.Fatalf("DefaultColor = %d is a real palette entry", DefaultColor)
	}
}

// TestStyleEqualAndFields pins the comparison a test relies on and the readable
// rendering a failure shows.
func TestStyleEqualAndFields(t *testing.T) {
	a := Default()
	b := Default()
	if !a.Equal(b) {
		t.Fatal("two default styles compare unequal")
	}
	b.Dim = true
	if a.Equal(b) {
		t.Fatal("a dim style compares equal to a default one")
	}
	c := Default()
	c.FgRGB = RGB(18, 52, 86)
	if b.Equal(c) {
		t.Fatal("styles with different colours compare equal")
	}
	// The RGB form takes precedence in the rendering, and the modes are named.
	if got, want := c.String(), "fg=rgb:123456"; got != want {
		t.Errorf("truecolour style renders as %q, want %q", got, want)
	}
	both := Default()
	both.Bold, both.Underline = true, true
	both.Fg, both.Bg = Red, Blue
	if got, want := both.String(), "fg=red bg=blue bold underline"; got != want {
		t.Errorf("styled render = %q, want %q", got, want)
	}
	if got := Default().String(); got != defaultStyleName {
		t.Errorf("default style renders as %q, want %q", got, defaultStyleName)
	}
}

// TestColorName pins the palette's names: the sixteen named entries read as
// names and anything else as a number, which is what makes a failure message say
// "red" rather than "1".
func TestColorName(t *testing.T) {
	for _, tc := range []struct {
		idx  int
		want string
	}{
		{DefaultColor, defaultStyleName},
		{Red, "red"},
		{BrightRed, "bright-red"},
		{BrightWhite, "bright-white"},
		{208, "colour-208"},
	} {
		if got := ColorName(tc.idx); got != tc.want {
			t.Errorf("ColorName(%d) = %q, want %q", tc.idx, got, tc.want)
		}
	}
	for _, idx := range []int{Black, Red, Green, Yellow, Blue, Magenta, Cyan, White,
		BrightBlack, BrightRed, BrightGreen, BrightYellow, BrightBlue, BrightMagenta, BrightCyan, BrightWhite} {
		if name := ColorName(idx); strings.HasPrefix(name, colorNumberPrefix) {
			t.Errorf("palette index %d has no name (%q)", idx, name)
		}
	}
}
