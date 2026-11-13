package main

// chat_geometry_test.go — the SURFACE half of the ideal geometry: the
// degradation sentence and the canvas width.
//
// WHY THESE ARE PURE FUNCTIONS UNDER TEST. The behaviour that matters is a
// DECISION — "is this terminal smaller than the declared ideal, and if so what
// exactly did it lose?" — and a decision that can only be observed by driving a
// real terminal is a decision nobody can regression-test. chatGeometryNotice and
// chatGeometryCanvas take the measurements as arguments, so the wording, the
// thresholds and the min() are pinned here; the install path that CALLS them is
// exercised end to end by the pty suite (chat_geometry_pty_test.go).

import (
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

func TestChatGeometryNoticeSilentWhenTheTerminalHostsTheIdeal(t *testing.T) {
	ideal := config.ChatIdealState{Cols: 117, Rows: 26, Source: config.ChatIdealGeometrySourceDefault}
	for _, tc := range []struct{ cols, rows int }{
		{117, 26}, {120, 26}, {117, 30}, {200, 60},
	} {
		if got := chatGeometryNotice(ideal, tc.cols, tc.rows); got != "" {
			t.Errorf("chatGeometryNotice(%dx%d) = %q, want silence — the terminal hosts the ideal", tc.cols, tc.rows, got)
		}
	}
}

func TestChatGeometryNoticeSpeaksWhenTheTerminalIsSmaller(t *testing.T) {
	ideal := config.ChatIdealState{Cols: 117, Rows: 26, Source: config.ChatIdealGeometryEnv}
	got := chatGeometryNotice(ideal, 80, 24)
	if got == "" {
		t.Fatal("a terminal smaller than the ideal produced NO notice — that is a silent downgrade (Rule 15)")
	}
	// The sentence must carry BOTH sizes and the SOURCE: an operator who set the
	// ideal themselves has a different remedy from one who inherited the default.
	for _, want := range []string{"80x24", "117x26", config.ChatIdealGeometryEnv} {
		if !strings.Contains(got, want) {
			t.Errorf("the notice does not name %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("the notice must be a whole line (it is written to the diagnostic stream), got %q", got)
	}
}

// TestChatGeometryNoticeNamesThePartThatIsLost: the notice is not a generic
// "too small" — it says WHAT the terminal cannot host, and it never claims a
// loss the measurements do not show.
func TestChatGeometryNoticeNamesThePartThatIsLost(t *testing.T) {
	ideal := config.ChatIdealState{Cols: 117, Rows: 26, Source: config.ChatIdealGeometrySourceDefault}

	// Narrower than the banner's floor: the BOX is what is lost.
	narrow := chatGeometryNotice(ideal, chatBannerMinCols()-1, 26)
	if !strings.Contains(narrow, "box") {
		t.Errorf("a terminal below the banner floor must name the box:\n%s", narrow)
	}
	// Wide enough for the box, too narrow for the pinned status row.
	between := chatGeometryNotice(ideal, chatBannerMinCols(), 26)
	if strings.Contains(between, "box") {
		t.Errorf("the box FITS at %d cols — the notice may not claim it is lost:\n%s", chatBannerMinCols(), between)
	}
	if !strings.Contains(between, "info bar") {
		t.Errorf("a terminal wide enough for the box but short of the pinned row must name the info bar:\n%s", between)
	}
	// Wide enough for every piece of chrome, only ROWS short.
	short := chatGeometryNotice(ideal, ideal.Cols, ideal.Rows-1)
	if strings.Contains(short, "box") || strings.Contains(short, "info bar") {
		t.Errorf("at %d cols only the ROW count is short — the notice must say so:\n%s", ideal.Cols, short)
	}
	if !strings.Contains(short, "answer band") {
		t.Errorf("the row-count loss must name the answer band:\n%s", short)
	}
}

// TestChatGeometryNoticeSilentOnUnknownGeometry: "not a terminal" (0) is NOT a
// small terminal. Claiming a degradation nobody measured is its own defect.
func TestChatGeometryNoticeSilentOnUnknownGeometry(t *testing.T) {
	ideal := config.ChatIdealState{Cols: 117, Rows: 26, Source: config.ChatIdealGeometrySourceDefault}
	for _, tc := range []struct{ cols, rows int }{{0, 0}, {80, 0}, {0, 24}} {
		if got := chatGeometryNotice(ideal, tc.cols, tc.rows); got != "" {
			t.Errorf("chatGeometryNotice(%dx%d) = %q, want silence — nothing was measured", tc.cols, tc.rows, got)
		}
	}
}

// TestChatGeometryCanvasIsTheIdealNotTheStartupWidth pins the width rule THE
// VALUE carries: a cap is the DESIGNED width, and the terminal it was measured on
// at launch is not part of it. Clamping against the terminal is canvasWidth()'s
// job and it happens on every winch (editor.go), so folding the startup measurement
// in here would freeze the layout at whichever width the window happened to have
// when the run started — the defect this row reports.
func TestChatGeometryCanvasIsTheIdealNotTheStartupWidth(t *testing.T) {
	ideal := config.ChatIdealState{Cols: 117, Rows: 26, Source: config.ChatIdealGeometrySourceDefault}
	for _, tc := range []struct{ term, want int }{
		{200, 117}, // wider terminal: the ideal, and the remainder is left blank
		{117, 117}, // exact
		{63, 117},  // STARTED NARROW: still the ideal — canvasWidth() clamps to 63 NOW
		{110, 117}, // started narrow, since widened: the value must not have followed
		{80, 117},
		{0, 0},  // unknown: no canvas at all (the terminal's own width, as before)
		{-4, 0}, // nonsense measurement: the same "no canvas" answer, never a negative cap
	} {
		if got := chatGeometryCanvas(ideal, tc.term); got != tc.want {
			t.Errorf("chatGeometryCanvas(ideal 117, startup terminal %d) = %d, want %d — the canvas is a "+
				"CAP, and a cap that shrinks to the startup width can never grow again", tc.term, got, tc.want)
		}
	}
}

// TestChatGeometryCanvasIsIdentityAtTheDefaultGeometry is the REGRESSION GUARD
// the epic's own design note asked for: at 117x26 the min() is an IDENTITY, so
// every existing pty test (which allocates exactly that geometry) must see the
// canvas it always saw. A change that made this false would move bytes in tests
// that have nothing to do with this knob.
func TestChatGeometryCanvasIsIdentityAtTheDefaultGeometry(t *testing.T) {
	def := config.ChatIdealGeometry()
	for cols := def.Cols; cols <= def.Cols+40; cols += 8 {
		if got := chatGeometryCanvas(def, cols); got != def.Cols {
			t.Fatalf("at the default ideal %dx%d a %d-col terminal got canvas %d — the min() is no longer "+
				"the identity it was designed to be", def.Cols, def.Rows, cols, got)
		}
	}
	if got := chatGeometryCanvas(def, def.Cols); got != def.Cols {
		t.Fatalf("canvas at the exact ideal = %d, want %d", got, def.Cols)
	}
}

// TestChatGeometryIdealAccessorsReadTheSharedResolver guards the "one idea of
// the designed size" claim at the SURFACE: the values the beat protocol and the
// film default to are the SAME ones a live chat resolves, in the same run, with
// the same channel winning.
func TestChatGeometryIdealAccessorsReadTheSharedResolver(t *testing.T) {
	t.Setenv(config.ChatIdealGeometryEnv, "105x33")
	if got := chatBeatIdealCols(); got != 105 {
		t.Errorf("chatBeatIdealCols() = %d, want the resolved 105", got)
	}
	if got := chatBeatIdealRows(); got != 33 {
		t.Errorf("chatBeatIdealRows() = %d, want the resolved 33", got)
	}
	if got := config.ChatIdealCols(); got != 105 {
		t.Errorf("config.ChatIdealCols() = %d, want 105 — the surface and the resolver disagree", got)
	}
}

// TestChatGeometryFlagIsInTheVerbsOwnFlagList mirrors the dump-flag contract: a
// flag the parser READS must be in chatREPLFlags() and in the one-line usage, or
// the usage page describes a command line the verb refuses.
func TestChatGeometryFlagIsInTheVerbsOwnFlagList(t *testing.T) {
	found := false
	for _, f := range chatREPLFlags() {
		if f == chatGeometryFlag {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("%s is read by the verb's parser but is NOT in chatREPLFlags() — the usage page and every "+
			"unknown-flag refusal would omit it", chatGeometryFlag)
	}
	if !strings.Contains(chatUsageLine, chatGeometryFlag) {
		t.Errorf("%s is missing from chatUsageLine", chatGeometryFlag)
	}
	// Both spellings the page advertises must actually parse — and the value must
	// land in the knob's ONE env representation.
	for _, argv := range [][]string{
		{chatGeometryFlag, "100x30"},
		{chatGeometryFlag + "=100x30"},
	} {
		t.Setenv(config.ChatIdealGeometryEnv, "")
		v, consumed, err := chatParseGeometryArg(argv[0], chatNextArg(argv, 0), true)
		if !consumed || err != nil {
			t.Fatalf("chatParseGeometryArg(%q) = (%q,%v,%v), want it consumed without error", argv[0], v, consumed, err)
		}
		if v != "100x30" {
			t.Fatalf("chatParseGeometryArg(%q) = %q, want 100x30", argv[0], v)
		}
		config.PublishChatIdealGeometryFlag(v)
		if got := os.Getenv(config.ChatIdealGeometryEnv); got != "100x30" {
			t.Fatalf("%q published %q, want the knob's env representation to hold 100x30", argv[0], got)
		}
	}
}

// TestChatGeometryFlagRefusesAMissingValue: a bare flag must be a USAGE error
// naming the shape, never a silent swallow that leaves the operator believing
// they sized the screen (Rule 15).
func TestChatGeometryFlagRefusesAMissingValue(t *testing.T) {
	if _, consumed, err := chatParseGeometryArg(chatGeometryFlag, "", false); !consumed || err == nil {
		t.Fatalf("a bare %s = (consumed=%v err=%v), want consumed and refused", chatGeometryFlag, consumed, err)
	}
	if _, consumed, err := chatParseGeometryArg(chatGeometryFlag+"=", "", true); !consumed || err == nil {
		t.Fatalf("an empty inline value = (consumed=%v err=%v), want consumed and refused", consumed, err)
	}
	if _, consumed, err := chatParseGeometryArg(chatGeometryFlag, "--tools", true); !consumed || err == nil {
		t.Fatalf("a value that is another flag = (consumed=%v err=%v), want consumed and refused", consumed, err)
	}
	// A NON-flag word is not this flag at all.
	if _, consumed, err := chatParseGeometryArg("--tools", "", true); consumed {
		t.Errorf("--tools was consumed as the geometry flag (err=%v)", err)
	}
}
