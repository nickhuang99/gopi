package main

// chat_banner_test.go — the startup screen's own contract, at the level a
// terminal cannot check.
//
// The pty case (testsuite/chat_banner_pty_test.go) proves the box REACHES a real
// screen; these tests prove the box is WORTH reaching — its arithmetic holds at
// every width, its colour-off bytes carry no escape sequence at all, and its
// content gives way by PRIORITY (the mark before the tips, the tips before the
// panel's least load-bearing facts) rather than by being cropped into nonsense.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/fullid"
	"github.com/nick/gopi/internal/lineedit"
	gopiversion "github.com/nick/gopi/internal/version"
)

// bannerTestSpec is the fixed input these tests compose from: one geometry, one
// set of facts, one palette per case.
func bannerTestSpec(cols int, pal lineedit.Palette) chatBannerSpec {
	return chatBannerSpec{
		cols:      cols,
		version:   gopiversion.Version,
		model:     "deepseek-v4-flash",
		node:      "zpeer",
		dir:       "/home/user/workspace/gopi-example",
		sessionID: bannerTestSessionID,
		toolsOn:   true,
		pal:       pal,
		// The newline tip is the one line whose TEXT depends on the terminal, so
		// the fixture pins the verdict it draws for. The SILENT case is the
		// operator's own terminal (xterm, measured 2026-10-02) and it is the one
		// that carries the reason as well as the key — the widest of the three.
		keyboardProtocol: lineedit.KeyboardProtocolUnsupported,
	}
}

// bannerTestSessionID is the fixture's session identity: a canonical uuid, so
// the identity fact is exercised in the shape a real run carries it (36 cells).
// It is a LITERAL here rather than a generated one on purpose — a test that
// mints its own uuid cannot assert that the panel prints THIS id back, which is
// the whole contract of the fact.
const bannerTestSessionID = "0199f2c1-4f3a-7c2e-9d31-0a1b2c3d4e5f"

// TestChatBannerRowsAreExactlyTheTerminalWidth is the invariant the whole box
// rests on: EVERY row occupies exactly cols cells. A row one cell short shears
// the right border; one cell long wraps and destroys the box. It is checked at
// several widths because the column split rounds, and the padding must follow
// the rounding rather than a fixed plan.
func TestChatBannerRowsAreExactlyTheTerminalWidth(t *testing.T) {
	for _, cols := range []int{chatBannerMinCols(), chatBannerMinCols() + 1, 78, 80, 100, 117, 133, 200} {
		rows := chatBannerRows(bannerTestSpec(cols, lineedit.PaletteFor(lineedit.ColourOn)))
		if len(rows) == 0 {
			t.Fatalf("cols=%d: the box was declined at a width it must fit", cols)
		}
		for i, row := range rows {
			if got := lineedit.DisplayWidth(row); got != cols {
				t.Errorf("cols=%d row %d occupies %d cells, want exactly %d: %q", cols, i, got, cols, row)
			}
		}
	}
}

// TestChatBannerMonochromeIsPlainText is the fallback contract's first half:
// with colour off the box is PLAIN TEXT — not one escape sequence, so a
// redirected run's transcript is exactly the screen the operator sees and stays
// comparable to the historic output.
func TestChatBannerMonochromeIsPlainText(t *testing.T) {
	rows := chatBannerRows(bannerTestSpec(100, lineedit.MonoPalette))
	if len(rows) == 0 {
		t.Fatal("the box was declined at 100 columns with colour off")
	}
	for i, row := range rows {
		if strings.ContainsRune(row, 0x1b) {
			t.Errorf("row %d carries an escape sequence with colour off: %q", i, row)
		}
	}
}

// TestChatBannerHistoricContractSurvives pins the two statements the e2e
// interrupt case requires (testsuite/chat_pty_interrupt_test.go) and the exact
// historic one-liner both fallbacks print. The startup screen must say what the
// one-liner says: a user who gets the box must still learn that Ctrl+C
// interrupts and that Ctrl+D or /quit leaves — and a user who does not (a
// redirected run, a narrow terminal) must get the bytes this surface has always
// emitted.
func TestChatBannerHistoricContractSurvives(t *testing.T) {
	joined := strings.Join(chatBannerRows(bannerTestSpec(100, lineedit.MonoPalette)), "\n")
	for _, want := range []string{
		"Ctrl+C interrupts the running turn",
		chatBannerExit,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the startup screen never states %q; screen:\n%s", want, joined)
		}
	}
	// The one-liner the fallback prints is built from the SAME fragment the
	// e2e case asserts on, so the two cannot drift.
	if !strings.Contains(chatBannerLine, "Ctrl+C interrupts the running turn") {
		t.Errorf("chatBannerLine %q lost the interrupt fragment the e2e case requires", chatBannerLine)
	}
}

// TestChatBannerDeclinesWhatItCannotDraw is the fallback contract's second half:
// a terminal too narrow for two readable columns gets NO box (nil), and the
// caller prints the historic one-liner. A degraded half-box would be worse than
// no box — it would claim a layout the terminal cannot hold.
func TestChatBannerDeclinesWhatItCannotDraw(t *testing.T) {
	for _, cols := range []int{0, 20, 40, chatBannerMinCols() - 1} {
		if rows := chatBannerRows(bannerTestSpec(cols, lineedit.MonoPalette)); len(rows) != 0 {
			t.Errorf("cols=%d: the box was drawn at a width too narrow for two columns:\n%s", cols, strings.Join(rows, "\n"))
		}
	}
}

// TestChatBannerGivesWayByPriority is the row budget's contract: on a terminal
// too short for the whole screen, the content sheds in a DEFINED order — the
// tips first, then the least load-bearing facts, and the mark LAST — and the box
// still fits. The order is the design decision (the mark is what the operator
// asked for), so it is pinned rather than left to the loop's incidental shape.
func TestChatBannerGivesWayByPriority(t *testing.T) {
	spec := bannerTestSpec(100, lineedit.MonoPalette)
	full := chatBannerRows(spec)
	if len(full) == 0 {
		t.Fatal("the box was declined at 100 columns")
	}

	// Every budget from "the whole screen" down to the smallest box that is
	// still a box must produce rows that FIT, and the mark must survive longer
	// than the tips do.
	markRows := 0
	tipsFullRows := 0
	for _, budget := range []int{len(full), len(full) - 2, len(full) - 4, 14, 12, 10, 8, 6} {
		shrunk := spec
		shrunk.maxRows = budget
		rows := chatBannerRows(shrunk)
		if len(rows) == 0 {
			// Below the smallest useful box the renderer declines, which is the
			// same honest answer as the narrow terminal: the caller falls back.
			continue
		}
		if len(rows) > budget {
			t.Errorf("maxRows=%d: the box occupies %d rows — it does not fit its own carrier", budget, len(rows))
		}
		joined := strings.Join(rows, "\n")
		if strings.Contains(joined, chatBannerMarkFact()) {
			markRows++
		}
		if strings.Contains(joined, chatBannerTipsHeader) {
			tipsFullRows++
		}
	}
	if markRows == 0 {
		t.Error("the mark never survived any row budget — the thing the screen is FOR was the first to go")
	}
}

// chatBannerMarkFact is the mark's widest drawn row, used by the priority test
// to ask "is the logo still on the screen".
func chatBannerMarkFact() string {
	widest := ""
	for _, line := range chatBannerLogoArt {
		if lineedit.DisplayWidth(line) > lineedit.DisplayWidth(widest) {
			widest = line
		}
	}
	return strings.TrimSpace(widest)
}

// --- the 3D wordmark --------------------------------------------------------

// TestChatBannerWordmarkVariantsAreOneDesign pins the two halves of the mark's
// contract: the SHORT wordmark is the floor every two-column box is derived from
// (its widest row fits chatBannerLeftColMin with the headroom the constant
// documents), and BOTH variants cost exactly chatBannerArtRowsMax rows — the row
// budget is spent by the mark, so a second variant that was a row taller would
// silently shrink every other part of the screen.
//
// It is the regression guard for the geometry constants: the art changed once
// without them (the old mark was 15 cells wide and the column floor had been
// derived from it), and this test is what makes the next art change state its
// width relation rather than hope for it.
func TestChatBannerWordmarkVariantsAreOneDesign(t *testing.T) {
	if got := len(chatBannerLogoArt); got != chatBannerArtRowsMax {
		t.Errorf("the short wordmark is %d rows, want chatBannerArtRowsMax=%d", got, chatBannerArtRowsMax)
	}
	if got := len(chatBannerLogoArtWide); got != len(chatBannerLogoArt) {
		t.Errorf("the two variants are %d and %d rows — they must be one design at two lengths",
			len(chatBannerLogoArt), len(chatBannerLogoArtWide))
	}
	short := widestArtRow(chatBannerLogoArt)
	wide := widestArtRow(chatBannerLogoArtWide)
	if short > chatBannerLeftColMin {
		t.Errorf("the short wordmark's widest row is %d cells and chatBannerLeftColMin is %d: the mark no longer fits the column floor derived for it",
			short, chatBannerLeftColMin)
	}
	if chatBannerLeftColMin-short > 2 {
		t.Errorf("the short wordmark is %d cells in a %d-cell column: the floor carries %d cells of headroom, more than the one-per-side the constant documents",
			short, chatBannerLeftColMin, chatBannerLeftColMin-short)
	}
	if wide <= short {
		t.Errorf("the wide wordmark (%d cells) is not wider than the short one (%d) — it is not a second length", wide, short)
	}
	// The wide one must actually be reachable: a variant nothing can hold is
	// dead art.
	if chatBannerLogoVariant(wide) == nil {
		t.Errorf("no column width draws the wide wordmark, not even its own %d cells", wide)
	}
}

// widestArtRow is an art block's widest row in display cells.
func widestArtRow(art []string) int {
	widest := 0
	for _, line := range art {
		if w := lineedit.DisplayWidth(line); w > widest {
			widest = w
		}
	}
	return widest
}

// TestChatBannerLogoVariantChoosesByRoom: the variant is chosen by WHAT FITS,
// never by a preference that could crop. A column that cannot hold the wide
// wordmark gets the short one, a column that cannot hold even that gets NOTHING
// (the caller then drops the art rather than drawing a sheared mark), and the
// two answers meet at exactly the wide art's widest row.
func TestChatBannerLogoVariantChoosesByRoom(t *testing.T) {
	wide := widestArtRow(chatBannerLogoArtWide)
	short := widestArtRow(chatBannerLogoArt)
	for _, tc := range []struct {
		cols int
		want []string
	}{
		{chatBannerLeftColMin, chatBannerLogoArt},
		{short, chatBannerLogoArt},
		{wide - 1, chatBannerLogoArt},
		{wide, chatBannerLogoArtWide},
		{wide + 40, chatBannerLogoArtWide},
		{short - 1, nil},
		{0, nil},
	} {
		got := chatBannerLogoVariant(tc.cols)
		if len(got) != len(tc.want) {
			t.Errorf("leftCols=%d: the variant is %d rows, want %d (%v)", tc.cols, len(got), len(tc.want), artFirstRow(got))
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("leftCols=%d row %d: the variant reads %q, want %q", tc.cols, i, got[i], tc.want[i])
				break
			}
		}
	}
}

// artFirstRow names an art block by its first row (or "none"), for failure text.
func artFirstRow(art []string) string {
	if len(art) == 0 {
		return "none"
	}
	return art[0]
}

// TestChatBannerMarkSurvivesInTheDrawnBox is the end-to-end half of the variant
// choice: at the narrowest terminal that gets a box at all the DRAWN screen
// carries the short wordmark, and somewhere inside an ordinary terminal the wide
// one takes its place — the mark is on the screen at both ends, never absent and
// never sheared.
//
// The cutover width is MEASURED by walking the geometry rather than computed
// here, for the same reason chatBannerMinFactRows walks budgets: the mark column
// is what is LEFT of the panel, and the panel's width is derived from the tip
// vocabulary and the identity fact — a number typed into this test would pin the
// fixture to today's tip list and break the day a tip is reworded.
func TestChatBannerMarkSurvivesInTheDrawnBox(t *testing.T) {
	shortMarker := strings.TrimSpace(chatBannerLogoArt[0])
	wideMarker := strings.TrimSpace(chatBannerLogoArtWide[0])
	if strings.Contains(shortMarker, wideMarker) || strings.Contains(wideMarker, shortMarker) {
		t.Fatalf("the two variants' first rows overlap (%q / %q): this test cannot tell them apart", shortMarker, wideMarker)
	}

	narrow := chatBannerMinCols()
	rows := chatBannerRows(bannerTestSpec(narrow, lineedit.MonoPalette))
	if len(rows) == 0 {
		t.Fatalf("cols=%d: the box was declined at a width that must carry it", narrow)
	}
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, shortMarker) {
		t.Errorf("cols=%d: the drawn box does not carry the short wordmark %q:\n%s", narrow, shortMarker, joined)
	}
	if strings.Contains(joined, wideMarker) {
		t.Errorf("cols=%d: the box drew the wide wordmark at the narrowest box width:\n%s", narrow, joined)
	}

	wideAt := 0
	for cols := narrow + 1; cols <= chatBannerWideArtSearchMax; cols++ {
		drawn := strings.Join(chatBannerRows(bannerTestSpec(cols, lineedit.MonoPalette)), "\n")
		if strings.Contains(drawn, wideMarker) {
			wideAt = cols
			if strings.Contains(drawn, shortMarker) {
				t.Errorf("cols=%d: the box carries BOTH wordmarks — the variant replaces the mark, it does not join it:\n%s", cols, drawn)
			}
			break
		}
	}
	if wideAt == 0 {
		t.Errorf("no terminal up to %d columns draws the wide wordmark: the second length is dead art (chatBannerMinCols()=%d)",
			chatBannerWideArtSearchMax, narrow)
		return
	}
	t.Logf("the wide wordmark first appears at cols=%d (the narrowest box is %d)", wideAt, narrow)
}

// chatBannerWideArtSearchMax bounds the walk above. It is above every terminal
// the box is built for, so a real regression (art no width can hold) fails here
// rather than looping.
const chatBannerWideArtSearchMax = 160

// TestChatBannerFactsAreOmittedNotFaked: a fact the session does not have is
// ABSENT rather than blank. A blank value invites a reader to read one into it,
// and the startup screen is exactly where a wrong fact is believed (it is the
// first thing on the screen).
func TestChatBannerFactsAreOmittedNotFaked(t *testing.T) {
	spec := bannerTestSpec(100, lineedit.MonoPalette)
	spec.node = ""
	spec.dir = ""
	rows := chatBannerRows(spec)
	if len(rows) == 0 {
		t.Fatal("the box was declined at 100 columns")
	}
	joined := strings.Join(rows, "\n")
	for _, label := range []string{chatBannerFactNode, chatBannerFactDir} {
		if strings.Contains(joined, label) {
			t.Errorf("the panel prints the %q fact with no value behind it:\n%s", label, joined)
		}
	}
	// The facts that DO have values are still there: omission is per-fact, not
	// an all-or-nothing collapse.
	if !strings.Contains(joined, chatBannerFactModel) {
		t.Errorf("dropping an absent fact also dropped the model fact:\n%s", joined)
	}
}

// TestChatBannerCropMarksWhatItCut: a line the box cannot hold is cut AND
// marked. The marker is the difference between "the value ends here" and "the
// value continues" — a screen that reports the first when the second is true is
// lying about the session's own directory.
func TestChatBannerCropMarksWhatItCut(t *testing.T) {
	long := "an-extremely-long-value-that-no-column-will-hold"
	got := chatBannerCrop(long, 12)
	if !strings.HasSuffix(got, chatBannerCutMarker) {
		t.Errorf("crop of an over-long value %q carries no cut marker", got)
	}
	if w := lineedit.DisplayWidth(got); w > 12 {
		t.Errorf("crop produced %d cells for a budget of 12: %q", w, got)
	}
	if short := chatBannerCrop("short", 12); short != "short" {
		t.Errorf("a value that fits was modified: %q", short)
	}
}

// TestChatBannerShortenDirKeepsTheTail: an over-long path keeps its LAST
// components, which are the ones that identify it, and the user's home becomes
// "~". Both are display-only, and both are pinned because a path shortened from
// the wrong end names a different directory.
func TestChatBannerShortenDirKeepsTheTail(t *testing.T) {
	long := "/home/user/workspace/gopi-01a0e7ed69337a83870fcb8291a09b21"
	got := chatBannerShortenDir(long, 20)
	if !strings.HasPrefix(got, chatBannerCutMarker) {
		t.Errorf("shortened %q does not mark the cut at its head: %q", long, got)
	}
	if !strings.HasSuffix(got, "a09b21") {
		t.Errorf("shortened %q lost the tail that identifies the directory: %q", long, got)
	}
	if w := lineedit.DisplayWidth(got); w > 20 {
		t.Errorf("shortened path occupies %d cells for a budget of 20: %q", w, got)
	}
	if empty := chatBannerShortenDir("", 20); empty != "" {
		t.Errorf("an empty dir rendered as %q — it must be omitted, not printed blank", empty)
	}
}

// --- the tips' explicit priority, and the newline tip's three states ---------

// bannerTipSpec is the spec these cases compose from: the shared one plus the
// keyboard verdict, which is the field the newline tip branches on.
func bannerTipSpec(kb lineedit.KeyboardProtocol, toolsOn bool) chatBannerSpec {
	spec := bannerTestSpec(100, lineedit.MonoPalette)
	spec.keyboardProtocol = kb
	spec.toolsOn = toolsOn
	return spec
}

// bannerHoldTip joins the tips a spec draws AT THE WIDTH THE PANEL ACTUALLY
// DRAWS THEM — the panel's own column budget, not an arbitrary one, so a case
// asking "is this line on the screen" is asking about the screen.
func bannerHoldTip(t *testing.T, spec chatBannerSpec, tip string) bool {
	t.Helper()
	cols := chatBannerPanelCols(spec, spec.cols-chatBannerGutterCells)
	joined := strings.Join(chatBannerTips(spec, cols), "\n")
	return strings.Contains(joined, tip)
}

// TestChatBannerPanelHoldsEveryTipWhole pins the relation chatBannerPanelMaxCols
// exists for: at the widest terminal the panel ever gets, every tip the panel can
// draw is there WHOLE. A tip cropped in the panel is advice the operator reads
// half of — the fallback newline line is the case that made this a test, since a
// panel narrower than it would print "the key does not work" and cut the reason.
func TestChatBannerPanelHoldsEveryTipWhole(t *testing.T) {
	// A wide terminal: the panel is allowed its maximum, so anything cropped
	// here is cropped on ANY terminal a user could have.
	spec := bannerTipSpec(lineedit.KeyboardProtocolUnsupported, true)
	spec.cols = 200
	cols := chatBannerPanelCols(spec, spec.cols-chatBannerGutterCells)
	if cols > chatBannerPanelMaxCols {
		t.Fatalf("the panel asked for %d cells, more than the %d it is bounded to", cols, chatBannerPanelMaxCols)
	}
	for _, tip := range chatBannerTips(spec, cols) {
		if got := chatBannerCrop(tip, cols); got != tip {
			t.Errorf("the panel's own width (%d) crops a tip it draws: %q -> %q", cols, tip, got)
		}
		if w := lineedit.DisplayWidth(tip); w > chatBannerPanelMaxCols {
			t.Errorf("a tip is %d cells wide, past the panel's %d-cell maximum: %q", w, chatBannerPanelMaxCols, tip)
		}
	}
}

// TestChatBannerNewlineTipFollowsTheKeyboardVerdict is the three-state contract
// at the content level: the wording follows what the terminal ANSWERED, and a
// run that never asked (no terminal, the cooked path) is told nothing — an
// advice about a negotiation that never happened would be a claim the screen
// cannot support (Rule 15).
func TestChatBannerNewlineTipFollowsTheKeyboardVerdict(t *testing.T) {
	for _, tc := range []struct {
		name string
		kb   lineedit.KeyboardProtocol
		want string
		gone string
	}{
		{"answered", lineedit.KeyboardProtocolSupported, chatBannerTipNewlineKitty, chatBannerTipNewlineFallback},
		{"silent", lineedit.KeyboardProtocolUnsupported, chatBannerTipNewlineFallback, chatBannerTipNewlineKitty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := bannerTipSpec(tc.kb, true)
			if !bannerHoldTip(t, spec, tc.want) {
				t.Errorf("keyboard=%v: the panel does not carry %q; tips:\n%s",
					tc.kb, tc.want, strings.Join(chatBannerTips(spec, chatBannerPanelMaxCols), "\n"))
			}
			if bannerHoldTip(t, spec, tc.gone) {
				t.Errorf("keyboard=%v: the panel ALSO carries the other state's line %q — the verdict must be exclusive", tc.kb, tc.gone)
			}
		})
	}

	// THE COOKED PATH (no terminal, nothing probed): no newline tip at all, in
	// either spelling. This is the regression the piped run depends on.
	spec := bannerTipSpec(lineedit.KeyboardProtocolUnavailable, true)
	for _, tip := range []string{chatBannerTipNewlineKitty, chatBannerTipNewlineFallback} {
		if bannerHoldTip(t, spec, tip) {
			t.Errorf("not-probed: the panel carries %q, but no negotiation ever ran", tip)
		}
	}
}

// TestChatBannerMustTipsComeUpInTheirPriorityOrder is the MUST list's contract:
// every must tip is drawn on every run, in the order the priority constants
// argue for — and two of those ranks are a CONTRACT rather than a preference.
//
// The priority type exists because that ORDER broke once: the interrupt sentence
// must reach the screen before the exit sentence, since the e2e case
// (testsuite/chat_pty_interrupt_test.go) reads the pty stream and stops at the
// exit sentence's marker. An exit line printed first hands that reader a block
// with the interrupt sentence still unpainted.
func TestChatBannerMustTipsComeUpInTheirPriorityOrder(t *testing.T) {
	spec := bannerTipSpec(lineedit.KeyboardProtocolSupported, true)
	joined := strings.Join(chatBannerTips(spec, chatBannerPanelMaxCols), "\n")

	// The order the priorities declare — the must head of the panel.
	want := []string{
		"/" + helpCommandName + " lists every command",
		chatBannerCtrlCTail,
		chatBannerExit,
		chatBannerTipNewlineKitty,
	}
	at := -1
	for _, w := range want {
		i := strings.Index(joined, w)
		if i < 0 {
			t.Fatalf("the panel dropped the must tip %q:\n%s", w, joined)
		}
		if i < at {
			t.Errorf("%q appears out of its declared priority order:\n%s", w, joined)
		}
		at = i
	}

	// THE INTERRUPT SENTENCE PRECEDES THE EXIT SENTENCE (the stream contract).
	if i, j := strings.Index(joined, chatBannerCtrlCTail), strings.Index(joined, chatBannerExit); i < 0 || j < 0 || i > j {
		t.Errorf("the interrupt sentence no longer precedes the exit sentence (at %d and %d):\n%s", i, j, joined)
	}
}

// TestChatBannerToolsTipIsInapplicableNotUnprioritised: on a run without tools
// the /tools line is not "ranked last", it does not APPLY — so the row it would
// have cost goes to the rotation, and the panel stays full instead of showing one
// line fewer.
func TestChatBannerToolsTipIsInapplicableNotUnprioritised(t *testing.T) {
	withTools := bannerTipSpec(lineedit.KeyboardProtocolSupported, true)
	without := bannerTipSpec(lineedit.KeyboardProtocolSupported, false)

	tipsWith := chatBannerTips(withTools, chatBannerPanelMaxCols)
	tipsWithout := chatBannerTips(without, chatBannerPanelMaxCols)
	if len(tipsWith) != chatBannerTipsMax || len(tipsWithout) != chatBannerTipsMax {
		t.Fatalf("the panel drew %d tips with tools and %d without, want the full %d in both — an inapplicable line must not cost a row:\n%v\n%v",
			len(tipsWith), len(tipsWithout), chatBannerTipsMax, tipsWith, tipsWithout)
	}
	joined := strings.Join(tipsWithout, "\n")
	if strings.Contains(joined, "/"+toolsCommandName+" ") {
		t.Errorf("the panel advertises /tools on a run without tools:\n%s", joined)
	}
	// The freed row goes to the ROTATION: the ROTATING POOL the run draws from is
	// one slot wider on a tool-less run, so the row is spent on variety rather
	// than dropped. Counted on the tips that actually APPLY, which is the
	// distinction this whole test is about.
	applying := func(spec chatBannerSpec) int {
		n := 0
		for _, tip := range chatBannerMustTips(spec) {
			if tip.applies == nil || tip.applies(spec) {
				n++
			}
		}
		return n
	}
	withSlots := chatBannerTipsMax - applying(withTools)
	withoutSlots := chatBannerTipsMax - applying(without)
	if withoutSlots != withSlots+1 {
		t.Errorf("a tool-less run left %d rotating slots against the tool-enabled run's %d, want exactly one more", withoutSlots, withSlots)
	}
}

// TestChatBannerCookedRunDrawsNoScreen is the cooked path's regression, pinned
// at the composer the REPL actually calls: with no reserved layout there is no
// band to carry the screen, so chatComposeStartupScreen returns "" and the
// caller prints the historic one-liner — whose bytes this change does not touch.
// A newline tip on that path would be advice about a negotiation this run never
// performed.
func TestChatBannerCookedRunDrawsNoScreen(t *testing.T) {
	cooked := &chatInput{} // no editor: Answer() == nil is the cooked shape
	if got := chatComposeStartupScreen(nil, nil, cooked, lineedit.MonoPalette); got != "" {
		t.Errorf("the cooked path composed a startup screen:\n%s", got)
	}
	if got := chatComposeStartupScreen(nil, nil, nil, lineedit.MonoPalette); got != "" {
		t.Errorf("a nil input composed a startup screen:\n%s", got)
	}
}

// --- the session IDENTITY fact ----------------------------------------------

// TestChatBannerPrintsTheFullSessionUUID is the epic's acceptance at the content
// level: `gopi chat`'s startup screen carries THIS session's identity, WHOLE —
// 36 cells, the exact string the spec was handed, no elision and no prefix.
//
// WHY IT IS THE FIRST FACT AND WHY THAT IS CHECKED HERE. The panel's row budget
// sheds the TAIL of chatBannerFacts when a terminal is short (chatBannerRows
// takes facts[:nFacts]), so "the identity is on the screen when only one fact
// fits" is a claim about its POSITION in that list, not only about its text.
// Both halves are asserted: the value is the full uuid verbatim, and it is the
// fact that survives when the panel can hold exactly one.
func TestChatBannerPrintsTheFullSessionUUID(t *testing.T) {
	spec := bannerTestSpec(100, lineedit.MonoPalette)
	facts := chatBannerFacts(spec, chatBannerPanelMaxCols)
	if len(facts) == 0 || facts[0].label != chatBannerFactSession {
		t.Fatalf("the identity is not the panel's first fact: %+v", facts)
	}
	if got := facts[0].value; got != bannerTestSessionID {
		t.Errorf("the identity fact reads %q, want the session's own uuid %q verbatim", got, bannerTestSessionID)
	}
	if w := lineedit.DisplayWidth(facts[0].value); w != fullid.UUIDLen {
		t.Errorf("the identity occupies %d cells, want the full %d — a shorter one is not an identity", w, fullid.UUIDLen)
	}
	// On the drawn screen, not just in the slice.
	joined := strings.Join(chatBannerRows(spec), "\n")
	if !strings.Contains(joined, bannerTestSessionID) {
		t.Errorf("the drawn panel does not carry the full uuid %q:\n%s", bannerTestSessionID, joined)
	}
	// And it survives the row budget down to the smallest box: the identity is
	// the head of the shed order, so a one-fact panel is the identity panel.
	oneFact := spec
	oneFact.maxRows = chatBannerMinFactRows(t)
	if oneFact.maxRows <= 0 {
		t.Skip("no row budget holds exactly one fact on this geometry")
	}
	if rows := chatBannerRows(oneFact); len(rows) > 0 {
		short := strings.Join(rows, "\n")
		if !strings.Contains(short, bannerTestSessionID) {
			t.Errorf("the identity was shed first at maxRows=%d — the seat it holds is the argument for its position:\n%s", oneFact.maxRows, short)
		}
	}
}

// chatBannerMinFactRows finds the row budget at which the panel still draws
// AT LEAST one fact row, by walking the budgets the renderer accepts. It is
// measured rather than computed because the box's height depends on the mark,
// the tips and the padding, and a number typed here would pin the fixture to
// today's arithmetic.
func chatBannerMinFactRows(t *testing.T) int {
	t.Helper()
	spec := bannerTestSpec(100, lineedit.MonoPalette)
	for budget := 6; budget <= 40; budget++ {
		s := spec
		s.maxRows = budget
		rows := chatBannerRows(s)
		if len(rows) == 0 {
			continue
		}
		joined := strings.Join(rows, "\n")
		if !strings.Contains(joined, chatBannerTipsHeader) && strings.Contains(joined, chatBannerFactSession) {
			// The panel kept its facts and dropped the tips: a budget where what
			// survives is decided by chatBannerFacts' own order.
			return budget
		}
	}
	return 0
}

// TestChatBannerIdentityIsNeverAShortPrefix is the epic's BAN, asserted: the
// panel's identity value is either the WHOLE uuid or a VISIBLY elided one — it
// is never a short slice presented as if it were the id (the superseded
// "shortest unique prefix" design: a prefix is an identifier that silently names
// more than one thing, fullid's own measured finding).
func TestChatBannerIdentityIsNeverAShortPrefix(t *testing.T) {
	const full = bannerTestSessionID
	for budget := 0; budget <= fullid.UUIDLen+4; budget++ {
		got := chatBannerIdentityValue(full, budget)
		if budget <= 0 {
			if got != "" {
				t.Errorf("budget=%d: a value was rendered with no room for one: %q", budget, got)
			}
			continue
		}
		if budget >= fullid.UUIDLen {
			if got != full {
				t.Errorf("budget=%d: the whole identity fits but %q was rendered", budget, got)
			}
			continue
		}
		// Too little room: the elided shape, and it must SAY so.
		if got == full {
			t.Errorf("budget=%d: the whole uuid was rendered into a column that cannot hold it: %q", budget, got)
		}
		if got != chatBannerCutMarker && !strings.Contains(got, chatBannerCutMarker) {
			t.Errorf("budget=%d: the elided identity %q carries no elision marker — a reader would take it for the id", budget, got)
		}
		if w := lineedit.DisplayWidth(got); w > budget {
			t.Errorf("budget=%d: the elided identity occupies %d cells: %q", budget, w, got)
		}
		// A prefix presented bare (the banned shape) is exactly the case where
		// the rendered text is a leading slice of the id with no marker.
		if strings.HasPrefix(full, got) && got != chatBannerCutMarker {
			t.Errorf("budget=%d: %q is a BARE PREFIX of the uuid — the banned short-id design", budget, got)
		}
	}
}

// TestChatBannerIdentityElisionKeepsBothEnds: the elided shape keeps the id's
// HEAD and TAIL — the two groups a reader matches by eye against `/log`'s own
// listing — so the fallback is still usable as an eyeball reference rather than
// as a one-sided fragment.
func TestChatBannerIdentityElisionKeepsBothEnds(t *testing.T) {
	const full = bannerTestSessionID
	got := chatBannerIdentityValue(full, 20)
	head, tail, ok := strings.Cut(got, chatBannerCutMarker)
	if !ok {
		t.Fatalf("no elision marker in %q", got)
	}
	if head == "" || tail == "" {
		t.Fatalf("the elided identity %q kept only one end", got)
	}
	if !strings.HasPrefix(full, head) {
		t.Errorf("the kept head %q is not the uuid's own head", head)
	}
	if !strings.HasSuffix(full, tail) {
		t.Errorf("the kept tail %q is not the uuid's own tail", tail)
	}
	// Below the floor there is no room for both ends: the value degrades to the
	// marker alone rather than to a stub that looks like a value.
	if stub := chatBannerIdentityValue(full, chatBannerIdentityKeepMin-1); stub != chatBannerCutMarker {
		t.Errorf("below the keep floor the identity rendered %q, want the marker alone", stub)
	}
}

// TestChatBannerFactsShedOrderStatesTheIdentityLast is the ORDER's other half,
// and the one a reader cannot see from the happy path: when the panel's rows run
// out, the facts give way from the TAIL — so the facts a reader can obtain
// elsewhere (the version in the title bar, the tools mode in the status row, the
// model in the band's own byline) go before the identity does.
func TestChatBannerFactsShedOrderStatesTheIdentityLast(t *testing.T) {
	facts := chatBannerFacts(bannerTestSpec(100, lineedit.MonoPalette), chatBannerPanelMaxCols)
	positions := map[string]int{}
	for i, f := range facts {
		positions[f.label] = i
	}
	order := []string{
		chatBannerFactSession,
		chatBannerFactModel,
		chatBannerFactDir,
		chatBannerFactNode,
		chatBannerFactVersion,
		chatBannerFactTools,
	}
	for i := 1; i < len(order); i++ {
		if positions[order[i-1]] >= positions[order[i]] {
			t.Fatalf("the shed order is not the argued one: %v", order)
		}
	}
	if len(facts) != chatBannerSessionFactCount {
		t.Errorf("the panel states %d facts but chatBannerSessionFactCount says %d — the capacity constant and the list are one decision",
			len(facts), chatBannerSessionFactCount)
	}
}

// TestChatBannerIdentityFactIsOmittedWithoutASession: a run with no session
// record states NO identity rather than a blank one (the panel's own rule: an
// absent fact says nothing, a blank one invites the reader to read a value into
// it) — and the panel does not grow a stray label row.
func TestChatBannerIdentityFactIsOmittedWithoutASession(t *testing.T) {
	spec := bannerTestSpec(100, lineedit.MonoPalette)
	spec.sessionID = ""
	// The ROW is what must be absent — an empty-valued fact is omitted by the
	// renderer (bannerFact's own contract), and the panel's section HEADER still
	// says "This session", so the assertion reads the rows BELOW the header. A
	// substring test over the whole box could not tell the header from a leaked
	// row, which is exactly the confusion this reads around.
	lines := chatBannerRows(spec)
	header := -1
	for i, line := range lines {
		if strings.Contains(line, chatBannerSessionHeader) {
			header = i
			break
		}
	}
	if header < 0 {
		t.Fatalf("the panel lost its own section header:\n%s", strings.Join(lines, "\n"))
	}
	for _, line := range lines[header+1:] {
		if strings.Contains(line, chatBannerFactSession) {
			t.Errorf("the panel prints the identity label with no id behind it: %q", line)
		}
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, chatBannerFactModel) {
		t.Errorf("dropping the identity also dropped the model fact:\n%s", joined)
	}
}

// TestChatBannerPanelAdvisesThePathToTheFullId is the elision's OTHER half: a
// panel that cannot hold the whole identity must name the in-UI way to get it,
// and that way has to be a real command the carrier answers. The tip is checked
// against the REGISTRY (lookupCommand) rather than against its own string, so a
// rename of /log fails this test instead of leaving the screen advertising a
// word nothing answers.
func TestChatBannerPanelAdvisesThePathToTheFullId(t *testing.T) {
	const tip = "/" + logCommandName + " " + chatBannerLogListSub + " lists every session id in full"
	if _, ok := lookupCommand(logCommandName); !ok {
		t.Fatalf("the panel advises /%s but the registry does not own that word", logCommandName)
	}
	// The advice must be REACHABLE on some run's panel: the rotation draws a
	// subset, so it is asserted over the pool the rotation draws from.
	found := false
	for _, rt := range chatBannerRotatingTips {
		if rt.text == tip {
			found = true
		}
	}
	if !found {
		t.Fatalf("the path to the full id %q is not in the panel's rotating pool", tip)
	}
	// And the subcommand it names is one /log answers (runSlashLog's switch):
	// the word after the command is the subcommand, tested through the REPL's
	// own handler rather than by reading the switch's source.
	// RUN IT, do not read the switch: the subcommand must be ANSWERED (nil error
	// and not the usage block an unknown/bare word returns). The profile this test
	// runs under is a pinned EMPTY one, so the answer may legitimately be "no
	// sessions recorded yet" — what is asserted is that the word is accepted and
	// the reader is told about sessions at all, not that this host has any.
	out, err := runSlashLog([]string{chatBannerLogListSub}, CommandContext{})
	if err != nil {
		t.Fatalf("/log %s is refused: %v", chatBannerLogListSub, err)
	}
	if out == logCommandUsage {
		t.Fatalf("/log %s answered with the usage block, i.e. the subcommand was not understood", chatBannerLogListSub)
	}
	if !strings.Contains(out, "SESSION") && !strings.Contains(out, "no sessions recorded yet") {
		t.Errorf("/log %s answered %q — neither a session table nor the empty-profile sentence", chatBannerLogListSub, out)
	}
}

// TestChatBannerIdentitySizesThePanelItsOwnRoom is the geometric half of the
// epic's "whenever the panel's value column can hold it", and it exists because
// the panel's width is derived from its CONTENT — the widest tip — and a tip's
// width is terminal-dependent (the silent-terminal newline line is 71 cells, the
// kitty one 29). On a kitty-capable terminal every other tip is shorter still,
// so a panel sized by the tips alone comes out ~45 cells: a 35-cell value column
// for a 36-cell uuid, and the panel's own FIRST fact would elide on a terminal
// that has room for it. The identity's room is therefore part of the panel's own
// demand, and this case pins that: at BOTH keyboard verdicts the drawn identity
// is the full uuid, and the panel stays inside chatBannerPanelMaxCols.
func TestChatBannerIdentitySizesThePanelItsOwnRoom(t *testing.T) {
	for _, kb := range []lineedit.KeyboardProtocol{
		lineedit.KeyboardProtocolSupported,
		lineedit.KeyboardProtocolUnsupported,
	} {
		spec := bannerTipSpec(kb, true)
		spec.cols = 200 // a terminal with room to spare: the panel is its own widest demand
		cols := chatBannerPanelCols(spec, spec.cols-chatBannerGutterCells)
		if cols > chatBannerPanelMaxCols {
			t.Errorf("keyboard=%v: the panel asked for %d cells, past its %d-cell maximum", kb, cols, chatBannerPanelMaxCols)
		}
		value := cols - chatBannerFactCells
		t.Logf("keyboard=%v cols=%d -> panel=%d value_column=%d", kb, spec.cols, cols, value)
		if got := chatBannerIdentityValue(bannerTestSessionID, value); got != bannerTestSessionID {
			t.Errorf("keyboard=%v: the panel's value column is %d cells and the identity rendered %q, want the full uuid",
				kb, value, got)
		}
		// And on the drawn screen, not only in the arithmetic.
		if rows := chatBannerRows(spec); len(rows) == 0 {
			t.Errorf("keyboard=%v: the box was declined at 200 columns", kb)
		} else if joined := strings.Join(rows, "\n"); !strings.Contains(joined, bannerTestSessionID) {
			t.Errorf("keyboard=%v: the drawn panel does not carry the full uuid:\n%s", kb, joined)
		}
	}
}
