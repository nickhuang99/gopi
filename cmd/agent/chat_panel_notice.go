package main

// chat_panel_notice.go — where the REPL's START-UP notices go: the solution to
// the operator's report that the chat's output area was polluted by tips.
//
// THE DEFECT, MEASURED (2026-10-03, dump of a real 100x30 pty). `gopi chat` opens
// with its startup screen (chat_banner.go: the boxed mark plus the side panel of
// getting-started tips and session facts) and then commits `[gopi] …` prose lines
// into the ANSWER BAND — the area the operator reads as the conversation:
//
//	[gopi] chat: this terminal does not report Shift+Enter (no reply to the
//	        keyboard-protocol query) — press Alt+Enter …
//	[gopi] chat_surface tool: this run's own screen is readable → …
//	[gopi] chat_surface tool: this run's own surface identity is published …
//
// Each wraps to two or three band rows before the operator has asked anything.
// The report is exact: those are tips for the user, the startup screen is where
// they belong, and the output area is for the conversation.
//
// WHY THEY WERE IN THE BAND AT ALL, so the fix does not repeat the mistake. Each
// notice used to be written straight to stderr. With the reserved layout
// installed, a flowed stderr write lands on the row the editor last drew and the
// next repaint erases it — the measurement `/new` produced (lineedit_wire.go's
// WriteNotice carries it). Routing them through WriteNotice fixed INVISIBILITY
// and chose the wrong destination for CONTENT: the only carrier that survived a
// repaint was the band, so advice, status and plumbing all went there alike.
//
// WHAT REPLACES IT. The startup screen is that second carrier, and it is the one
// the operator pointed at. So the start-up notices are not deleted and not muted
// (Rule 15: several report an ABSENCE the operator may have to act on — the dump
// is off, the identity could not be published — and a surface that decided not to
// say so would be the silent failure the rule forbids). Every notice is HELD
// instead of printed, and it reaches the screen by the route that fits it:
//
//   - its FACT joins the side panel as the "surface" row: whether this run's own
//     screen can be read back, whether its identity reached its children, and
//     which key inserts a newline on THIS terminal (the ONE place the keyboard
//     negotiation's verdict is stated, and it is stated for the terminal that
//     needs it);
//   - its DETAIL — the path, the switch, the variable, i.e. what makes the fact
//     actionable — is drawn in the box's own NOTICES section, inside the same
//     border (chatBannerScreen). Those lines are short on purpose: the section
//     shares the screen with the box, and prose here would push the box up.
//
// The ordering constraint holds either way: the box is composed and committed in
// ONE WriteLines, and the panel's facts are arguments to that SAME composition —
// so nothing can land ON the panel after it was printed (the defect commit
// 6544dad5 fixed, in its own words: "the tips land on the panel instead of in the
// transcript").
//
// THE FALLBACK IS THE HISTORIC PATH, and it is not optional. On a cooked or
// redirected run, or a terminal too narrow to draw the box, there is no screen to
// move a notice into: each held notice then takes the same WriteNotice call it
// always did, and the piped output is what it was. This is the contract the rest
// of the chat chrome keeps (colour off is byte-identical), and it is why every
// notice's TEXT is left untouched here — the panel strips the `[gopi] ` prefix
// for its own copy, and the stream keeps the whole line.

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// chatPanelNoticePrefix is the marker every REPL notice carries ON THE STREAM.
// It is the string the panel's copy strips, named here rather than typed at the
// strip site so the two cannot drift.
const chatPanelNoticePrefix = "[gopi] "

// chatPanelNotice is one start-up notice, held instead of printed.
type chatPanelNotice struct {
	// stream is the exact line the historic path writes (marker included), so the
	// fallback is byte-identical to what this surface printed before.
	stream string
	// panel is the SHORT copy the startup screen draws under its notices header.
	// It carries the one actionable detail the panel's own surface fact does not
	// (the dump path, the off switch, the variable), and it is short on purpose:
	// the section shares the screen with the box.
	panel string
}

// chatPanelNotices is the run's held start-up notices plus what the panel's own
// surface fact says. It is assembled where the facts are known (the REPL's setup)
// and consumed twice — by the startup screen's spec, and by the fallback that
// prints whatever the screen could not state — so it is a NAMED struct rather
// than a handful of booleans at the call site: a notice added to one path and
// forgotten in the other is exactly the "notice nobody can find" defect this file
// removes.
type chatPanelNotices struct {
	lines   []chatPanelNotice
	surface chatBannerSurface
}

// add holds one notice. An empty stream line is ignored (a caller with nothing to
// say must not add a blank row to the section).
func (n *chatPanelNotices) add(stream, panel string) {
	if strings.TrimSpace(stream) == "" {
		return
	}
	n.lines = append(n.lines, chatPanelNotice{stream: stream, panel: panel})
}

// bannerNotices renders the section's rows, in the order the REPL states them.
func (n chatPanelNotices) bannerNotices() []bannerNotice {
	out := make([]bannerNotice, 0, len(n.lines))
	for _, l := range n.lines {
		if l.panel != "" {
			out = append(out, bannerNotice{text: l.panel})
		}
	}
	return out
}

// chatPanelNoticesFallback writes every held notice whose section row the box did
// not draw, through the SAME method the historic path used (WriteNotice) and in
// the original order, so a notice can be MOVED but never LOST (Rule 15).
//
// `stated` is how many section rows chatBannerNoticeFit drew. A notice with no
// panel copy has no row at all (its FACT is on the side panel instead) and is
// never printed here: repeating a sentence the screen already states in the band
// is the duplication this design removes.
func chatPanelNoticesFallback(in *chatInput, n chatPanelNotices, stated int) {
	if in == nil {
		return
	}
	drawn := 0
	for _, l := range n.lines {
		if l.panel == "" {
			continue
		}
		if drawn < stated {
			drawn++
			continue
		}
		in.WriteNotice(l.stream)
	}
}

// chatPanelNoticesStream is the cooked path's answer: EVERY held notice printed,
// in order, because there is no screen to hold any of them. It is the one place
// the two routes part (the fallback above only prints what the box could not
// state).
func chatPanelNoticesStream(in *chatInput, n chatPanelNotices) {
	if in == nil {
		return
	}
	for _, l := range n.lines {
		in.WriteNotice(l.stream)
	}
}

// chatBannerSurface is what the startup screen states about THIS run's own
// observability — the facts the start-up notices used to carry into the answer
// band. Each field is a named string (Rule 1b), so the panel's value is composed
// from a vocabulary rather than from prose written at the call site.
type chatBannerSurface struct {
	// arm is whether this run's own screen can be read back
	// (chatBannerSurfaceScreen*).
	arm string
	// identity is whether the run's surface identity reached its children
	// (chatBannerSurfaceIdentity*).
	identity string
	// newline is the key that inserts a newline on THIS terminal, read from the
	// keyboard negotiation's verdict.
	newline string
}

// The surface fact's vocabulary. The words are SHORT on purpose: the value shares
// a panel row with its label and the panel is width-bounded, so a sentence here
// would be cropped (see chatBannerPanelMaxCols).
const (
	chatBannerSurfaceScreenArmed   = "screen readable"
	chatBannerSurfaceScreenUnarmed = "screen off"
	chatBannerSurfaceIdentityOn    = "identity published"
	chatBannerSurfaceIdentityOff   = "identity unpublished"
	chatBannerSurfaceNewlineShift  = "newline Shift+Enter"
	chatBannerSurfaceNewlineAlt    = "newline Alt+Enter"
	chatBannerSurfaceSep           = " · "
)

// chatBannerNoticeIdentityOff names the consequence, so the fact is actionable:
// a child's `gopi ui_state --target chat` then falls back to the general rule,
// which refuses on an ambiguous profile. It is the ONE notices-section copy that
// survives the 2026-10-11 ruling, because it backs an ANOMALY (a publication that
// failed) rather than the ordinary arm state.
const (
	chatBannerNoticeIdentityOff = "surface identity not published — a child's ui_state --target chat falls back to the general rule"
)

// chatSurfaceIdentityUnpublishedNotice is the STREAM spelling of the publication
// failure — the notice a run prints when the box could not be drawn. It carries
// the channel's name and the fact that the child's reader degrades, which is what
// makes it actionable rather than a decoration. The underlying cause is printed
// by the publisher itself (chat_surface_identity.go), so this line does not repeat
// a Go error at the operator.
const chatSurfaceIdentityUnpublishedNotice = chatPanelNoticePrefix + "chat_surface tool: this run's surface identity could not be published (%s) — a child's `gopi ui_state --target chat` falls back to the general rule, which refuses when several chat surfaces are live\n"

// value renders the fact for the panel, or "" when this run has nothing to state
// (a cooked run with no screen and no negotiation): bannerFact's own contract
// omits an empty value rather than printing a blank row.
func (s chatBannerSurface) value() string {
	parts := make([]string, 0, 3)
	for _, p := range []string{s.arm, s.identity, s.newline} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, chatBannerSurfaceSep)
}

// chatPanelSurfaceArmed maps the dump's state to the surface fact's vocabulary.
func chatPanelSurfaceArmed(armed bool) string {
	if armed {
		return chatBannerSurfaceScreenArmed
	}
	return chatBannerSurfaceScreenUnarmed
}

// chatPanelSurfaceIdentity maps the publication's outcome to the fact's
// vocabulary: a publication that FAILED is stated as such, never omitted — the
// child's own fallback is the consequence a reader has to be able to explain.
func chatPanelSurfaceIdentity(published bool) string {
	if published {
		return chatBannerSurfaceIdentityOn
	}
	return chatBannerSurfaceIdentityOff
}

// chatPanelSurfaceNewline maps the keyboard negotiation's verdict to the fact's
// vocabulary — and it states NOTHING for a run that never negotiated
// (KeyboardProtocolUnavailable: no terminal, no probe), because advice about a
// negotiation that never happened is a claim this screen cannot support
// (chat_keyboard_protocol.go's own rule).
func chatPanelSurfaceNewline(state lineedit.KeyboardProtocol) string {
	switch state {
	case lineedit.KeyboardProtocolSupported:
		return chatBannerSurfaceNewlineShift
	case lineedit.KeyboardProtocolUnsupported:
		return chatBannerSurfaceNewlineAlt
	default:
		return ""
	}
}

// chatStartupNotices gathers THIS run's start-up notices: the lines the REPL used
// to commit into the answer band, HELD instead, with the fact each one states
// recorded for the side panel (chat_panel_notice.go carries the report and the
// design).
//
// It is the ONE place the two routes read the same facts: the panel's surface row
// is built from the same values the section's short copies are, so the screen and
// the fallback can never describe this run differently.
//
// `identity` is the value chatPublishSurfaceIdentity published ("" when it
// failed); `keyboard` is the negotiation's verdict for THIS terminal.
func chatStartupNotices(toolsOn bool, dumper *uiclient.ScreenDumper, identity string, keyboard lineedit.KeyboardProtocol) chatPanelNotices {
	n := chatPanelNotices{}
	// (1) The keyboard negotiation's verdict is a FACT the panel states (the same
	// measurement chat_banner.go's newline tip is written from), and its historic
	// sentence is the notice that carried it — held here so the paths with no
	// screen still print it.
	n.surface.newline = chatPanelSurfaceNewline(keyboard)
	if advice := keyboardAdviceNotice(keyboard); advice != "" {
		n.add(advice, "")
	}
	// (2) The surface IDENTITY — published, or this run's own report that it could
	// not be. It is held for EVERY run, tool-less included, because the historic
	// print site was never gated on tools: installChatSurface publishes and prints
	// on either path (chat_surface_surface.go), and holding this line behind the
	// tools gate DROPS a sentence the operator used to receive — the quiet
	// deletion this whole file exists to prevent (Rule 15). The ORDER is the
	// historic one too: the identity was announced BEFORE the screen pair, and a
	// moved notice keeps its place in the queue it moved from.
	if identity != "" {
		n.add(chatSurfaceIdentityStreamLine(identity), "")
	} else if toolsOn {
		// The publication FAILED. chatPublishSurfaceIdentity already reported the
		// underlying cause on stderr; the notice here is the RUN's own fact, and it
		// is ACTIONABLE — it names the variable that did not get set and what a
		// child's reader then does instead — so the panel's section carries a short
		// copy of it.
		n.add(fmt.Sprintf(chatSurfaceIdentityUnpublishedNotice, config.ChatSurfaceIdentityEnv), chatBannerNoticeIdentityOff) //stylegate:ignore the panel's copy is the notice's own format, one implementation
	}
	if !toolsOn {
		// A tool-less run has no chat_surface capability at all: nothing to
		// announce about its own screen — the fact is published to the REQUEST
		// instead (SetChatSurfaceFact, chat_surface_fact.go), where the model that
		// owns the capability reads it.
		return n
	}
	// (3) The run's own SCREEN: the arm fact for the PANEL. It is a FACT about
	// this run (like the session id and the model), not a notice — the startup
	// screen legitimately states it, and the detail it needs (the artifact path)
	// has moved to the request-side note, which is why no section line is added
	// here.
	armed := chatSurfaceArmed(dumper)
	n.surface.arm = chatPanelSurfaceArmed(armed)
	n.surface.identity = chatPanelSurfaceIdentity(identity != "")
	return n
}

// keyboardAdviceNotice is the newline ADVICE the keyboard negotiation produces,
// as a held notice: the exact sentence the historic path printed, or "" for a
// verdict that needs no advice (a terminal that answered, or one never asked).
func keyboardAdviceNotice(state lineedit.KeyboardProtocol) string {
	if state == lineedit.KeyboardProtocolUnsupported {
		return chatKeyboardUnsupportedNotice
	}
	return ""
}

// chatSurfaceIdentityStreamLine renders the identity-publication notice's stream
// spelling, so the environment name and the value are interpolated in ONE place.
func chatSurfaceIdentityStreamLine(value string) string {
	return fmt.Sprintf(chatSurfaceIdentityNotice, config.ChatSurfaceIdentityEnv, value)
}
