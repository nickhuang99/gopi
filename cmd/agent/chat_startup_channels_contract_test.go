package main

// <!-- doc-class: normative -->

// chat_startup_channels_contract_test.go — the CONTRACT the three START-UP notice
// writers satisfy after term_note_row.go became their drawing path:
//
//	chat_session_budget.go  the "publishing GOPI_SESSION_BUDGET=0" line
//	chat_session_slots.go   the "slot N registered in …" self-check line
//	chat_policy_flag.go     the resolved policy posture line
//
// THE PROPERTY THAT MATTERS IS A PAIR, and neither half is safety on its own:
//
//	1. the DRAW carries the run's system voice — with colour on, the line is
//	   painted dim, and dim ONLY (no bold, no underline, no colour index: the
//	   operator's ruling is that the surface's remarks about itself are
//	   LOW-CONTRAST, and an emphasis is not an alarm);
//	2. the TEXT is the historic text — the bytes between/around the sequences
//	   are exactly what `fmt.Fprintf(os.Stderr, "…%s…\n", …)` wrote before this
//	   change, so a colourless run (`NO_COLOR`, `TERM=dumb`, a piped sink) emits
//	   the SAME bytes it always did, and a colour run emits those bytes plus
//	   emphasis and nothing else.
//
// WHY THE ASCII-ONLY ARM OF THE /info CONTRACT DOES NOT TRANSFER HERE. The
// /info block is a PASTE SOURCE: a reader selects a line and runs it, so a
// non-ASCII byte in a cell would be a byte in their shell. These three lines are
// NOT paste sources — they are the surface reporting what it resolved — and
// their prose has carried em dashes since before this change. Asserting
// ASCII-only here would force a reword, i.e. a change to the very bytes this
// test exists to keep. What is asserted instead is the honest pair: no ESCAPE
// byte ever (the SGR lives in the render, never in a cell's text), and no
// invisible character (a zero-width joiner, a control byte) that a reader could
// neither see nor type, because that IS a decoration this change may not add.
//
// WHAT THE RED PROOF MEASURES. The text arm is green both before and after this
// change (it is the compatibility claim). The DRAW arm is the new claim, and it
// is red on the pre-change code in two independent ways — measured, see
// artifacts/redproof/: the mutated drawer that emits Text() instead of Row()
// fails every draw assertion; and the pre-change BINARY, driven through a real
// PTY, paints these three lines at `default`. A claim that only a test can see
// is checked by the test; a claim about the screen is checked on the screen.

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sgr"
)

// chatStartupNoticeSentences are the three writers' sentences built with REAL
// arguments, so the contract is asserted against the shape production emits and
// not against a placeholder. Each is the string the historic Fprintf formatted,
// byte for byte (that is the whole point: the arguments are the ones a real run
// passes).
func chatStartupNoticeSentences() []struct {
	name string
	text string
} {
	return []struct {
		name string
		text string
	}{
		{
			name: "session-budget default",
			text: fmt.Sprintf(chatSessionBudgetDefaultNotice,
				sessionBudgetSurfaceChat, chatSessionBudgetEnv, chatSessionBudgetEnv, chatSessionBudgetUnlimited, chatSessionBudgetEnv),
		},
		{
			name: "slot self-check",
			text: chatSlotNotice + chatSessionSlotSelfCheckLine(
				ChatSessionSlotEntry{Index: 3, Path: "/home/user/.gopi/chat_session_id_3.txt"}, "/home/user/.gopi/sessions/abc"),
		},
		{
			name: "policy posture",
			text: chatPolicyStartupLine(nil, "", agent.ResolveChatPolicyPosture("", os.Getenv, "")),
		},
	}
}

// chatStartupOneRow renders ONE sentence the way every start-up writer does —
// through termNoteRows and termWriteRows — and returns both the drawn bytes and
// the rows, so a case can assert on the two halves the contract names.
func chatStartupOneRow(text string, st termToolStyles) (drawn string, rows []termRow) {
	rows = termNoteRows(text, st)
	var b strings.Builder
	termWriteRows(&b, rows)
	return b.String(), rows
}

// TestStartupChannelsDrawNothingWithoutColour is the COMPATIBILITY half: with
// the monochrome table the three writers emit the historic bytes exactly —
// including the single trailing newline, and with no escape byte anywhere.
//
// It is deliberately asserted against the SAME sentences the writers format, not
// against a literal in this file: a reworded notice fails HERE, beside the
// compatibility claim it would break, rather than silently in a screen capture.
func TestStartupChannelsDrawNothingWithoutColour(t *testing.T) {
	mono := termToolStylesFor(lineedit.ColourOff)
	for _, c := range chatStartupNoticeSentences() {
		t.Run(c.name, func(t *testing.T) {
			drawn, rows := chatStartupOneRow(c.text, mono)
			// c.text is the Fprintf FORMAT with its arguments filled in, and two of
			// the three formats end in "\n" — which is exactly the row boundary the
			// drawer contributes. So the historic bytes are the sentence plus the
			// ONE newline a row ends with, never two: that is the property asserted
			// (a doubled newline would be an invisible change to every stream these
			// lines are piped into).
			want := strings.TrimSuffix(c.text, "\n") + "\n"
			if drawn != want {
				t.Fatalf("colourless bytes are not the historic bytes:\n got %q\nwant %q", drawn, want)
			}
			if strings.ContainsRune(drawn, 0x1b) {
				t.Fatalf("a colourless start-up line carries an escape byte: %q", drawn)
			}
			for i, r := range rows {
				if r.Colour {
					t.Errorf("row %d says it carries emphasis under the monochrome table", i)
				}
				if r.Text() != strings.Split(c.text, "\n")[i] {
					t.Errorf("row %d text is not the sentence's line: got %q", i, r.Text())
				}
			}
		})
	}
}

// TestStartupChannelsDrawDimAndOnlyDim is the NEW half: with colour on, each row
// is painted with the system role's dim sequence, the painted text is unchanged,
// and no other attribute appears — the ban on loud emphasis is asserted, not
// assumed, because "only Dim" is exactly the property a later edit could break
// without anyone noticing on a busy terminal.
func TestStartupChannelsDrawDimAndOnlyDim(t *testing.T) {
	on := termToolStylesFor(lineedit.ColourOn)
	if !on.On {
		t.Fatal("termToolStylesFor(ColourOn) reports colour off — this case would then prove nothing")
	}
	dimSeq := on.Meta.Seq()
	if dimSeq == "" {
		t.Fatal("the system role has no sequence under a colour table: dim would be unimplementable through it")
	}
	for _, c := range chatStartupNoticeSentences() {
		t.Run(c.name, func(t *testing.T) {
			drawn, rows := chatStartupOneRow(c.text, on)
			for i, r := range rows {
				if !r.Colour {
					t.Errorf("row %d does not carry emphasis under a colour table", i)
				}
				if got := r.Render[0].Style; got.Dim != true || got.Bold || got.Underline || got.Fg != sgrDefaultFg {
					t.Errorf("row %d is drawn with %+v, want Dim only (no bold, no underline, no colour index)", i, got)
				}
			}
			if !strings.Contains(drawn, dimSeq) {
				t.Fatalf("the drawn line carries no dim sequence %q: %q", dimSeq, drawn)
			}
			// THE DRAW IS THE TEXT PLUS EMPHASIS — nothing else. Stripping every
			// escape sequence must give back the historic sentence, so a cell that
			// swallowed a decoration character, a wrapper that re-cut the line, or a
			// marker glyph would all show up here.
			if stripped, want := stripEscapeSequences(drawn), strings.TrimSuffix(c.text, "\n")+"\n"; stripped != want {
				t.Fatalf("the drawn text is not the historic text:\n got %q\nwant %q", stripped, want)
			}
			// AND NO OTHER ATTRIBUTE REACHES THE STREAM: the only opening sequence
			// in the row is the system role's own.
			if others := escapeOpenersExcept(drawn, dimSeq); len(others) > 0 {
				t.Fatalf("the start-up row carries emphasis beyond the system voice: %q", others)
			}
		})
	}
}

// TestStartupChannelCopyTextIsNeverDecorated is the invisible-character arm: a
// cell's text (the half a reader or a log parser sees when colour is off) holds
// no ESC and no character a human can neither see nor type.
//
// The accepted non-ASCII set is deliberately narrow — the em dash the prose has
// always carried, and nothing else — so a future "prettier" glyph (a bullet, a
// zero-width space, a non-breaking space) fails here rather than inside somebody's
// terminal.
func TestStartupChannelCopyTextIsNeverDecorated(t *testing.T) {
	for _, c := range chatStartupNoticeSentences() {
		t.Run(c.name, func(t *testing.T) {
			for _, st := range []termToolStyles{termToolStylesFor(lineedit.ColourOff), termToolStylesFor(lineedit.ColourOn)} {
				for i, r := range termNoteRows(c.text, st) {
					for _, cell := range r.Render {
						if strings.ContainsRune(cell.Text, 0x1b) {
							t.Fatalf("row %d cell text carries an escape byte: %q", i, cell.Text)
						}
						for _, ru := range cell.Text {
							if ru == chatStartupEmDash {
								continue
							}
							if ru > 0x7e || (ru < 0x20 && ru != '\t') {
								t.Fatalf("row %d cell text carries %U, which is not printable ASCII and not the prose's em dash: %q",
									i, ru, cell.Text)
							}
						}
						if !utf8.ValidString(cell.Text) {
							t.Fatalf("row %d cell text is not valid UTF-8: %q", i, cell.Text)
						}
					}
					if r.CopyText != r.Text() {
						t.Errorf("row %d CopyText %q is not the drawn text %q — a start-up line has no second form",
							i, r.CopyText, r.Text())
					}
				}
			}
		})
	}
}

// chatStartupEmDash is the ONLY non-ASCII character the three sentences carry
// (the prose's own dash). Named (Rule 1b) so the contract above reads as a rule
// rather than as a magic code point.
const chatStartupEmDash = '\u2014'

// sgrDefaultFg is sgr.Default()'s foreground: "the terminal's own colour", i.e.
// no index chosen. It is read off the package rather than spelled here so the
// assertion follows sgr if the default ever moves.
var sgrDefaultFg = sgr.Default().Fg

// chatSGRReset is the sequence that closes every painted cell (sgr.ResetSeq).
// Named (Rule 1b) because the "only Dim" measurement treats it as the one
// permitted companion of the system role.
var chatSGRReset = sgr.ResetSeq()

// stripEscapeSequences removes every CSI/ESC sequence, leaving the text — the
// measurement behind "the draw is the text plus emphasis".
func stripEscapeSequences(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		// ESC [ … <final byte in @-~>. The writers only ever emit CSI sequences
		// (sgr.Style.Seq / ResetSeq), so this is the whole vocabulary they use.
		j := i + 1
		if j < len(s) && s[j] == '[' {
			j++
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			if j < len(s) {
				j++
			}
		}
		i = j - 1
	}
	return b.String()
}

// escapeOpenersExcept returns the opening sequences in drawn other than want —
// the "only Dim" measurement, expressed on the bytes rather than on the styles so
// a drawer that added a sequence of its own would be caught.
func escapeOpenersExcept(drawn, want string) []string {
	var others []string
	for i := 0; i < len(drawn); i++ {
		if drawn[i] != 0x1b {
			continue
		}
		j := i + 1
		if j < len(drawn) && drawn[j] == '[' {
			j++
			for j < len(drawn) && (drawn[j] < '@' || drawn[j] > '~') {
				j++
			}
			if j < len(drawn) {
				j++
			}
		}
		seq := drawn[i:j]
		if seq != want && seq != chatSGRReset {
			others = append(others, seq)
		}
		i = j - 1
	}
	return others
}
