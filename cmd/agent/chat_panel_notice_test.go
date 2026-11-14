package main

// chat_panel_notice_test.go — the in-package contract for the fix the operator
// reported on 2026-10-03: the chat's START-UP notices do not go into the answer
// band. chat_panel_notice.go carries the report and the design; this file pins
// the four properties a render check alone cannot state.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// panelNoticeTestSpec is a spec wide enough for the box, so a case that is not
// about geometry never fails for a geometry reason.
// panelNoticeTestCols is a width the box is definitely drawn at — wide enough for
// the panel's own maximum (chatBannerPanelMaxCols) plus the mark's column and the
// borders, so a case that is not about geometry never fails for a geometry reason.
const panelNoticeTestCols = 120

func panelNoticeTestSpec(notices ...bannerNotice) chatBannerSpec {
	spec := bannerTestSpec(panelNoticeTestCols, lineedit.PaletteFor(lineedit.ColourOff))
	spec.notices = notices
	return spec
}

// TestChatPanelNoticeSectionIsInsideTheBox is the FIRST property: the notices are
// drawn INSIDE the box's own border, below the panel — the same object on the
// screen, not a second block glued under it. A section drawn outside would still
// be "on the startup screen", and would still be the failure this fix removes
// (a line the operator reads as transcript).
func TestChatPanelNoticeSectionIsInsideTheBox(t *testing.T) {
	spec := panelNoticeTestSpec(bannerNotice{text: "screen dump → /tmp/x.dump"})
	rows, remaining := chatBannerScreen(spec)
	if len(rows) == 0 {
		t.Fatal("the screen was declined at the fixture's width")
	}
	if len(remaining) != 0 {
		t.Fatalf("the section reported %d notices unstated, want 0 (the screen had room)", len(remaining))
	}
	if !strings.Contains(strings.Join(rows, "\n"), chatBannerNoticesHeader) {
		t.Fatalf("the notices section is not on the screen:\n%s", strings.Join(rows, "\n"))
	}
	// The section's rows are the box's rows: the first row of the screen is the
	// top border and the last is the bottom one, and the section lies between.
	first, last := rows[0], rows[len(rows)-1]
	if !strings.Contains(first, chatBannerVersionLabel(spec.version)) || !strings.Contains(first, chatBannerTopLeft) {
		t.Errorf("row 0 is not the box's title bar: %q", first)
	}
	if !strings.Contains(last, chatBannerBottomLeft) {
		t.Errorf("the last row is not the box's bottom border: %q", last)
	}
	// EVERY row is exactly the terminal's width, section rows included: a short
	// row there would leave the right border open.
	for i, row := range rows {
		if got := len([]rune(row)); got != spec.cols {
			t.Errorf("row %d is %d cells, want the terminal's %d: %q", i, got, spec.cols, row)
		}
	}
}

// TestChatPanelNoticeSectionIsNeverHalfDrawn is the SECOND property: when the
// screen cannot hold a notice, the section stops at a NOTICE boundary and says
// so — it never ends mid-sentence, and it reports exactly what it did not state
// so the caller can print it (Rule 15).
func TestChatPanelNoticeSectionIsNeverHalfDrawn(t *testing.T) {
	long := strings.Repeat("word ", 60)
	spec := panelNoticeTestSpec(bannerNotice{text: long}, bannerNotice{text: "second notice"})

	for budget := 1; budget <= len(chatBannerRows(spec))+40; budget++ {
		spec.maxRows = budget
		rows, remaining := chatBannerScreen(spec)
		if len(rows) == 0 {
			continue // the box itself was declined at this budget: nothing drawn
		}
		joined := strings.Join(rows, "\n")
		if !strings.Contains(joined, chatBannerNoticesHeader) {
			// The box fits but the section did not: every notice must be reported
			// as unstated, or a notice would vanish without a trace.
			if len(remaining) != 2 {
				t.Fatalf("budget=%d: no section on screen but %d notices reported unstated, want 2", budget, len(remaining))
			}
			continue
		}
		// The last row of the box is its border, whatever the section did.
		if last := rows[len(rows)-1]; !strings.Contains(last, chatBannerBottomLeft) {
			t.Fatalf("budget=%d: the box's bottom border is missing — the section ran past the screen:\n%s", budget, joined)
		}
		// A row that carries a fragment of the long notice's text must be the
		// FULL line the wrapper produced, i.e. it must end at a word boundary:
		// the wrap helper's own contract, restated here because a crop would
		// break it.
		for _, row := range rows {
			if !strings.Contains(row, "word") {
				continue
			}
			if strings.HasSuffix(strings.TrimRight(row, " │"), "wor") {
				t.Fatalf("budget=%d: a notice row was CROPPED mid-word: %q", budget, row)
			}
		}
	}
}

// TestChatPanelNoticesFallbackPrintsWhatTheScreenDidNot is the THIRD property:
// a notice the box did not draw is printed through the historic stream, in order,
// and a notice the box DID draw is not printed again — moved, never lost, and
// never duplicated.
func TestChatPanelNoticesFallbackPrintsWhatTheScreenDidNot(t *testing.T) {
	var got []string
	// The fallback writes through chatInput.WriteNotice, whose last resort is
	// errOut (no editor is installed here) — which is exactly the stream the
	// historic path used.
	in := &chatInput{errOut: &panelNoticeCapture{lines: &got}}

	n := chatPanelNotices{}
	// The identity-unpublished notice is the ONE surviving section copy after the
	// 2026-10-11 ruling (it backs an ANOMALY, not the ordinary arm state); the
	// second line carries its fact on the panel and therefore has no copy.
	n.add(fmt.Sprintf(chatSurfaceIdentityUnpublishedNotice, config.ChatSurfaceIdentityEnv), chatBannerNoticeIdentityOff)
	n.add(chatSurfaceIdentityStreamLine("pid:session"), "")

	// stated=0 (nothing drawn): the notice WITH a panel copy is printed, and the
	// one WITHOUT a panel copy is not — its fact is on the panel, and repeating
	// the sentence in the band is the duplication this design removes.
	chatPanelNoticesFallback(in, n, 0)
	if len(got) != 1 || !strings.Contains(got[0], "surface identity") {
		t.Fatalf("stated=0: stream carried %q, want the unpublished-identity notice only", got)
	}

	// stated=1 (the section drew it): nothing is printed at all.
	got = nil
	chatPanelNoticesFallback(in, n, 1)
	if len(got) != 0 {
		t.Fatalf("stated=1: stream carried %q, want nothing (the screen stated it)", got)
	}

	// THE COOKED PATH prints EVERY held notice: there is no screen to hold any.
	got = nil
	chatPanelNoticesStream(in, n)
	if len(got) != 2 {
		t.Fatalf("the stream path carried %d notices, want all 2: %q", len(got), got)
	}
	for i, want := range []string{"identity could not be published", "surface identity is published"} {
		if !strings.Contains(got[i], want) {
			t.Errorf("notice %d is %q, want it to carry %q (the historic order)", i, got[i], want)
		}
	}
}

// TestChatPanelNoticesStateTheFactsTheBandUsedToCarry is the FOURTH property, and
// it is the one that keeps the fix from being a silent deletion: every FACT the
// start-up notices carried is stated SOMEWHERE the operator can read it — on the
// panel's surface row, or in the section, or (when neither is drawn) on the
// stream.
func TestChatPanelNoticesStateTheFactsTheBandUsedToCarry(t *testing.T) {
	spec := panelNoticeTestSpec()
	spec.surface = chatBannerSurface{
		arm:      chatPanelSurfaceArmed(true),
		identity: chatPanelSurfaceIdentity(true),
		newline:  chatPanelSurfaceNewline(lineedit.KeyboardProtocolUnsupported),
	}
	panel := panelNoticeFactText(spec)
	// The panel states all three: which key inserts a newline on THIS terminal,
	// that the screen can be read back, and that the identity was published.
	for _, want := range []string{
		chatBannerSurfaceNewlineAlt,
		chatBannerSurfaceScreenArmed,
		chatBannerSurfaceIdentityOn,
	} {
		if !strings.Contains(panel, want) {
			t.Errorf("the panel's facts do not state %q; they are:\n%s", want, panel)
		}
	}

	// An UNARMED run states the absence in words (never a blank value), and a
	// FAILED publication says so rather than omitting the row.
	spec.surface = chatBannerSurface{
		arm:      chatPanelSurfaceArmed(false),
		identity: chatPanelSurfaceIdentity(false),
	}
	panel = panelNoticeFactText(spec)
	for _, want := range []string{chatBannerSurfaceScreenUnarmed, chatBannerSurfaceIdentityOff} {
		if !strings.Contains(panel, want) {
			t.Errorf("the degraded run's facts do not state %q; they are:\n%s", want, panel)
		}
	}

	// A COOKED run (nothing probed) states no newline advice: advice about a
	// negotiation that never ran is a claim this screen cannot support.
	cooked := chatBannerSurface{newline: chatPanelSurfaceNewline(lineedit.KeyboardProtocolUnavailable)}
	if v := cooked.value(); strings.Contains(v, "Enter") {
		t.Errorf("a run that never negotiated states %q, want no newline advice", v)
	}
}

// TestChatPanelSurfaceNewlineIsTheKeyboardVerdict ties the panel's fact to the
// negotiation's own vocabulary — the two enums cannot drift into disagreeing about
// which terminal gets which key.
func TestChatPanelSurfaceNewlineIsTheKeyboardVerdict(t *testing.T) {
	if got := chatPanelSurfaceNewline(lineedit.KeyboardProtocolSupported); got != chatBannerSurfaceNewlineShift {
		t.Errorf("an answering terminal states %q, want %q", got, chatBannerSurfaceNewlineShift)
	}
	if got := chatPanelSurfaceNewline(lineedit.KeyboardProtocolUnsupported); got != chatBannerSurfaceNewlineAlt {
		t.Errorf("a silent terminal states %q, want %q", got, chatBannerSurfaceNewlineAlt)
	}
	if got := chatPanelSurfaceNewline(lineedit.KeyboardProtocolUnavailable); got != "" {
		t.Errorf("an unprobed terminal states %q, want the empty value (nothing to say)", got)
	}
}

// TestChatStartupNoticesNeverStateTheArmState is the 2026-10-11 contract, and it
// is the NEGATIVE half of the operator's ruling: the arm state is NOT a startup
// notice any more. It is a fact on the REQUEST (internal/agent's
// chat_surface_fact.go, published by installChatSurface), so no run — armed,
// unarmed, tooled or tool-less — may hold a notice carrying it, in any spelling
// (the stream sentence, or the section's `screen dump …` short copy).
//
// The check is written as a SUBSTRING sweep over everything the gathering
// function returns, so a future notice that re-introduces the fact by a new
// wording still fails here.
func TestChatStartupNoticesNeverStateTheArmState(t *testing.T) {
	forbidden := []string{"own screen", "screen dump", "screen readable", "screen off"}
	cases := []struct {
		tag      string
		tools    bool
		dumper   *uiclient.ScreenDumper
		identity string
		keyboard lineedit.KeyboardProtocol
		// wantArm is the panel fact this case must carry: the arm fact exists only
		// where the capability does (a tool-less run has no chat_surface tool to
		// describe, which is also why the request-side note is not published there).
		wantArm string
	}{
		{"tooled, identity published", true, nil, "pid:session", lineedit.KeyboardProtocolUnsupported, chatBannerSurfaceScreenUnarmed},
		{"tooled, identity unpublished", true, nil, "", lineedit.KeyboardProtocolUnsupported, chatBannerSurfaceScreenUnarmed},
		{"tool-less", false, nil, "pid:session", lineedit.KeyboardProtocolUnavailable, ""},
	}
	for _, tc := range cases {
		n := chatStartupNotices(tc.tools, tc.dumper, tc.identity, tc.keyboard)
		var held []string
		for _, l := range n.lines {
			held = append(held, l.stream, l.panel)
		}
		for _, text := range held {
			for _, bad := range forbidden {
				if strings.Contains(text, bad) {
					t.Errorf("%s: a startup notice states the arm fact (%q) in %q — the fact belongs on the request, not the screen",
						tc.tag, bad, text)
				}
			}
		}
		// AND THE FACT IS STILL STATED SOMEWHERE — that is the whole reason the
		// panel row exists (Rule 15: moved, never lost).
		if n.surface.arm != tc.wantArm {
			t.Errorf("%s: the panel's surface fact is %q, want %q", tc.tag, n.surface.arm, tc.wantArm)
		}
	}
}

// TestChatStartupNoticesHoldEveryLineTheReplWouldPrint is the seam the REPL leans
// on: the gathering function holds a notice for EVERY line the historic path still
// prints, so the two routes cannot describe the same run differently — after the
// 2026-10-11 ruling that is the keyboard advice and the identity line, and nothing
// about the arm state (the fact moved to the request).
func TestChatStartupNoticesHoldEveryLineTheReplWouldPrint(t *testing.T) {
	withTools := chatStartupNotices(true, nil, "pid:session", lineedit.KeyboardProtocolUnsupported)
	if got := len(withTools.lines); got != 2 {
		t.Fatalf("a tooled run holds %d notices, want 2 (keyboard advice, identity): %+v", got, withTools.lines)
	}
	if withTools.surface.arm != chatBannerSurfaceScreenUnarmed {
		t.Errorf("a nil dumper states %q, want the unarmed fact", withTools.surface.arm)
	}
	// Every held notice carries the stream's own marker: the stream spelling is
	// what the fallback prints, and the marker is what makes it recognisable.
	for i, l := range withTools.lines {
		if !strings.HasPrefix(l.stream, chatPanelNoticePrefix) {
			t.Errorf("notice %d does not carry the stream's marker: %q", i, l.stream)
		}
	}

	without := chatStartupNotices(false, nil, "", lineedit.KeyboardProtocolUnavailable)
	if len(without.lines) != 0 {
		t.Errorf("a tool-less, unprobed run holds %d notices, want 0: %+v", len(without.lines), without.lines)
	}

	// A TOOL-LESS RUN STILL ANNOUNCES ITS IDENTITY. The historic print site was
	// never gated on tools (installChatSurface publishes and reports on either
	// path), so holding this line behind the tools gate would DROP a sentence the
	// operator used to receive — measured against bin/gopi-base on a cooked run,
	// where the base prints it and the gated form did not (Rule 15: moved, never
	// lost).
	toolLess := chatStartupNotices(false, nil, "pid:session", lineedit.KeyboardProtocolUnavailable)
	if len(toolLess.lines) != 1 || !strings.Contains(toolLess.lines[0].stream, "surface identity") {
		t.Errorf("a tool-less run with a published identity holds %+v, want exactly the identity line", toolLess.lines)
	}

	// AND THE ORDER IS THE HISTORIC ONE. The stream is what an operator reads
	// top-down, so a queue that moved must keep its place: the keyboard advice
	// first, then the identity — and the arm-state notice that used to be third is
	// GONE from the queue (its fact is on the request), which is why the expected
	// length is 2 rather than 3.
	wantOrder := []string{"Shift+Enter", "surface identity"}
	if len(withTools.lines) != len(wantOrder) {
		t.Fatalf("held %d notices, want %d: %+v", len(withTools.lines), len(wantOrder), withTools.lines)
	}
	for i, want := range wantOrder {
		if !strings.Contains(withTools.lines[i].stream, want) {
			t.Errorf("notice %d is %.60q…, want it to carry %q (the historic order)", i, withTools.lines[i].stream, want)
		}
	}
}

// panelNoticeFactText renders the panel's facts the way the box does — label and
// value in the two columns — so the case asserts on what a reader SEES rather
// than on the fact struct.
func panelNoticeFactText(spec chatBannerSpec) string {
	var out []string
	for _, f := range chatBannerFacts(spec, chatBannerPanelMaxCols) {
		out = append(out, f.label+chatBannerSpace+f.value)
	}
	return strings.Join(out, "\n")
}

// panelNoticeCapture is the notice sink the fallback writes into: an
// errOut-shaped recorder, so the case reads the STREAM rather than inferring it.
type panelNoticeCapture struct{ lines *[]string }

func (c *panelNoticeCapture) Write(p []byte) (int, error) {
	*c.lines = append(*c.lines, string(p))
	return len(p), nil
}
