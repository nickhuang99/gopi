package testsuite

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/testutil"
	"golang.org/x/sys/unix"
)

// chat_cut_pty_test.go — the ANSWER path's right-edge CUT on a REAL terminal.
//
// WHICH PATH THIS GUARDS, and why the choice is stated (task 01a1267b): there are
// TWO cut paths in this product and they now have OPPOSITE contracts.
//
//   - the ANSWER/status path (internal/lineedit/answer.go, writeRow → cutLabelFor)
//     draws a right-edge cut label: a row whose text stops between two narrow word
//     runes "stopped for a reason the reader cannot see", so it SPENDS its last
//     cell on the product's label and hands the displaced character to the row
//     below. A status field is one screen row and DROPS its tail (fitField), so the
//     label there is a true statement.
//   - the NOTE path (writeNote/cutNoteRow) draws NO glyph at all since commit
//     e58009f65 (task 01a12669): its cut is a plain break, the remainder opens the
//     next row, and a glyph would sit inside the command's own cells — a character
//     the reader pastes into a shell that cannot run it.
//
// THIS FILE GUARDS THE FIRST ONE, and the reading is measured rather than
// assumed: the mock answer below is 400 characters of ONE token rendered through
// the ANSWER renderer, and the rows it collects all open with the ANSWER band's
// own lead (`▌ ` first row, two blanks on a continuation) — a note row opens with
// `⚙ ` and continues with `↳ `, so no row this test reads is a note row. The
// label it asserts is therefore `AnswerCutLabel` (the ANSWER BAND's own
// vocabulary since answer_cut_label_glyph.go — a continuation mark, NOT the
// status row's elision idiom `StatusEllipsis`, and not a note-row marker).
//
// THE LEAD ALONE IS NOT SELECTABLE (measured 2026-10-22, task 01a16885): the
// START-UP notice `[gopi] chat: slot 0 registered in …` is drawn at the terminal's
// width and the terminal wraps it, so its own second row opens with two blanks too
// and a lead-only selector collected it into the band — assertion (3) then counted
// the notice's 115 characters as answer text (got 515, want 400). The run is
// therefore ANCHORED at the answer's first characters before any row is appended;
// the lead check remains as the necessary geometry condition. The completeness
// assertion itself is untouched — see devdoc/chat-cut-pty-band-selector.md for the
// measurement and the red proof that a genuinely lost character still fails this
// case.
//
// The unit-level cases (internal/lineedit/answer_cut_marker_test.go) pin the row
// arithmetic against a captured byte stream. This case is the wire level one: a
// `gopi chat` child on a pty of the operator's own geometry, asked for an answer
// that is ONE token with no break opportunity anywhere in it — the shape the
// operator reported (`fix/pushback-rule31-evid` | `ence`, terminal=117x26). The
// answer's rows must then show the cut: the row that ran out of room ends with the
// cut label, and the character the label displaced opens the row below, so a
// narrow terminal can be told from a corrupt row AND no character is lost.
//
// The columns asserted are the ones installed with TIOCSWINSZ and read back
// through the same helper the child uses, so the case cannot pass on a width
// nobody set.

// The case's own bounds, every one named (Rule 1).
const (
	// chatCutSuiteCols / chatCutSuiteRows are the geometry the operator reported:
	// the width where the cut is real (a row of 117 cells) and enough rows for the
	// reserved answer band to have its own space.
	chatCutSuiteCols = 117
	chatCutSuiteRows = 26
	// chatCutSuiteTokenRunes is the answer's length in characters: three full rows
	// at chatCutSuiteCols plus a remainder, so at least two cuts are exercised.
	chatCutSuiteTokenRunes = 400
	// chatCutSuiteTokenPieces are the characters the token is built from. None is a
	// space, a tab, a newline AND NONE IS A SEPARATOR (`/ - _ : .`), so the whole
	// answer is ONE token the source offers no break inside and no wider row can
	// hold: every row boundary is a bare cell cut.
	//
	// THE SEPARATORS WERE REMOVED ON PURPOSE (task 01a1c3b0). They used to spell
	// `fix/pushback-rule31-evid:alpha-beta_gamma.delta`, which is a run of SMALL
	// tokens rather than one wide one — and the band now moves a token that fits a
	// row DOWN WHOLE (band_word_move.go), so those rows ended at a separator with
	// nothing cut and, correctly, wore no label. The label's meaning narrowed to
	// exactly the case this fixture must therefore be: a token WIDER than the band,
	// which has no wider row to move to.
	chatCutSuiteTokenPieces = "fixpushbackrule31evidencealphabetagammadeltaomega"
	// chatCutSuiteBandLead is the two cells every band row starts with: the turn
	// marker column and the space after it.
	chatCutSuiteBandLead = "▌ "
	// chatCutSuiteWrapLead is the same two columns on a row that CONTINUES the row
	// above (the marker column is blank there).
	chatCutSuiteWrapLead = "  "
	// chatCutSuiteTokenTail ends the answer on a line of its own: without it the
	// harness cannot know the stream finished (the rows of a long answer look alike
	// at the first row and the last), and quitting early would measure a partial
	// answer.
	chatCutSuiteTokenTail = "-cut-end-"
)

// chatCutSuiteToken is the answer the mock LLM streams.
func chatCutSuiteToken() string {
	var b strings.Builder
	for b.Len() < chatCutSuiteTokenRunes {
		b.WriteString(chatCutSuiteTokenPieces)
	}
	return b.String()[:chatCutSuiteTokenRunes]
}

// TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal drives the real binary on a pty
// and reads the band out of the captured stream, through the screen the terminal
// itself would be. It asserts the three things the operator's sighting needs:
//
//  1. a band row ENDS with the cut label — the cut is visible;
//  2. the row below opens with the character the label displaced (the character
//     that would have been the marked row's last cell) — so the row below is a
//     continuation, not a fresh word;
//  3. folding the label away, the band's characters ARE the answer, in order, once
//     — the label costs a cell of the ROW, never a character of the ANSWER.
func TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal(t *testing.T) {
	token := chatCutSuiteToken()
	streamed := token + "\n" + chatCutSuiteTokenTail

	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: streamed})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: chatCutSuiteCols, Row: chatCutSuiteRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable: %v", err)
	}
	if cols := lineedit.TerminalCols(int(chat.master.Fd())); cols != chatCutSuiteCols {
		t.Fatalf("the pty reports %d columns, want the %d this case installed", cols, chatCutSuiteCols)
	}

	boot := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	// Typed, not pasted (writeGapped): the question is echoed by the editor one
	// keystroke at a time, which is the path the operator reported — a pasted line
	// takes the spill path instead and never reaches the answer renderer.
	chat.writeGapped(t, "stream one long token")
	chat.write(t, "\r")
	out := boot + chat.waitFor(t, chatCutSuiteTokenTail, ptyPromptTimeout)
	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0; output so far:\n%s", code, out)
	}

	// The screen the terminal would be: the child's own bytes, at the geometry
	// installed above. Reading the grid (rather than the raw stream) is what makes
	// the claim about CELLS — a row's columns are the terminal's, not the writer's.
	sc := lineedit.NewScreen(chatCutSuiteCols, chatCutSuiteRows)
	sc.Feed([]byte(out))

	// The band rows: a 400-character answer is four rows, and the terminal keeps a
	// live screenful plus the history that scrolled off it, so the answer is read
	// from BOTH — the scrollback first (the rows a real terminal would have saved),
	// then the live grid, stopping at the row that carries the answer's own tail.
	//
	// The band rows — selected by the answer's OWN START, not by a row's shape.
	//
	// WHAT A LEAD-ONLY SELECTOR GOT WRONG (measured on this tree, 2026-10-22):
	// `[gopi] chat: slot 0 registered in …` is a START-UP notice drawn at the
	// terminal's own width, and the terminal wraps it the way it wraps anything
	// else, so its rows open with the same geometry a continuation row of the band
	// does — measured, its second row begins with 70 blank cells. A selector that
	// only asked "are the row's first two cells `▌ ` or `  `?" therefore collected
	// the notice into the band, and assertion (3) below counted its 115 characters
	// as answer text: got 515, want 400 — the excess was exactly the notice's
	// length. The rows the answer itself draws are unchanged by this; only the
	// SELECTION was wrong.
	//
	// So the run is ANCHORED at its start: the band opens on the first row of the
	// answer's own column whose body begins with the answer's first characters, and
	// continues through the contiguous band-column rows to the one carrying the
	// answer's tail marker. The lead check remains as the necessary geometry
	// condition — it is what makes a row sit in the answer column at all — but it
	// is no longer SUFFICIENT: a notice row passes the lead and fails the anchor.
	//
	// THE ANCHOR IS NOT A WEAKENED ASSERTION. Every row after the first is appended
	// WITHOUT being compared against the token, so "the band holds the answer
	// whole" is still measured character by character below; a character lost or
	// duplicated inside the answer is caught there, and the anchor decides only
	// WHERE the answer starts — a fact about where the turn begins, not about the
	// answer's bytes.
	const bandLead = 2
	var band []string
	inAnswer := false
	collect := func(raw string) bool {
		// RUNE slicing, not byte slicing: the turn marker is one cell of three
		// bytes, so a byte-prefix comparison against it could never match.
		runes := []rune(raw)
		if len(runes) < bandLead {
			return false
		}
		if lead := string(runes[:bandLead]); lead != chatCutSuiteBandLead && lead != chatCutSuiteWrapLead {
			return false
		}
		body := strings.TrimRight(string(runes[bandLead:]), " ")
		if body == "" {
			return false
		}
		if !inAnswer {
			if !strings.HasPrefix(body, chatCutSuiteTokenPieces) {
				return false // band-column row that is not the answer's first row
			}
			inAnswer = true
		}
		band = append(band, body)
		return strings.HasPrefix(body, chatCutSuiteTokenTail)
	}
	done := false
	for _, raw := range sc.Scrollback() {
		if collect(raw) {
			done = true
			break
		}
	}
	if !done {
		for r := 0; r < chatCutSuiteRows && !done; r++ {
			done = collect(sc.Line(r))
		}
	}
	if len(band) == 0 {
		t.Fatalf("the answer never reached the terminal; captured output:\n%s", out)
	}
	// The rows this case asserts on, quoted verbatim: they are the ANSWER band's
	// rows (the lead check above collected nothing else), and a report can read
	// the case's own evidence without re-deriving it.
	t.Logf("answer-path band rows (%d), as the terminal shows them:", len(band))
	for i, row := range band {
		t.Logf("  band row %d|%s", i, row)
	}

	// THE LABEL IS THE ANSWER BAND'S OWN (AnswerCutLabel), NOT A NOTE-ROW MARKER
	// and NOT the status row's elision idiom — see the file head for the
	// measurement that decided which of the cut contracts this case guards.
	// `NoteCutMarker`/`StatusEllipsis` are the status-row vocabulary (a field whose
	// tail was DROPPED); the band's cut DISPLACES one character onto the row below,
	// and answer_cut_label_glyph.go carries why the two must not share a spelling.
	cutLabel := lineedit.AnswerCutLabel
	marked, below := -1, ""
	for i, row := range band {
		if strings.HasSuffix(row, cutLabel) {
			marked = i
			if i+1 < len(band) {
				below = band[i+1]
			}
			break
		}
	}
	if marked < 0 {
		t.Fatalf("no band row ends with the cut label %q — a row that ran out of room mid-token is "+
			"indistinguishable from a wrapped row, which is the defect; band rows:\n%s",
			cutLabel, strings.Join(band, "\n"))
	}
	if below == "" {
		t.Fatalf("the marked row is the last of the band — the cut label claims a continuation the band "+
			"does not draw; band rows:\n%s", strings.Join(band, "\n"))
	}

	// (2) the character the label displaced opens the row below: the marked row's
	// folded text is a prefix of the answer, so the answer itself names the
	// character the label took.
	folded := strings.TrimSuffix(band[marked], cutLabel)
	if displaced := displacedHead(token, folded); displaced == "" || !strings.HasPrefix(below, displaced) {
		t.Fatalf("the row below the mark does not open with the character the label displaced: "+
			"marked row ends %q, row below begins %q; band rows:\n%s",
			tailRunes(folded, 8), headRunes(below, 8), strings.Join(band, "\n"))
	}

	// (3) the answer survives: every character, in order, exactly once, with the
	// label folded away — the label costs a cell of the ROW, never a character of
	// the ANSWER.
	var got strings.Builder
	for _, row := range band {
		if strings.HasPrefix(row, chatCutSuiteTokenTail) {
			break
		}
		got.WriteString(strings.ReplaceAll(row, cutLabel, ""))
	}
	if got.String() != token {
		t.Fatalf("the band does not hold the answer whole: got %d characters, want the token's %d — "+
			"a marked row must hand its last character to the row below, losing nothing; band rows:\n%s",
			got.Len(), len(token), strings.Join(band, "\n"))
	}
}

// displacedHead is the character the marked row gave up: the answer's first
// character after the row's folded text. A row is a prefix of the token, so the
// answer itself names the character the label displaced.
func displacedHead(token, folded string) string {
	i := strings.Index(token, folded)
	if i < 0 {
		return ""
	}
	rest := token[i+len(folded):]
	if rest == "" {
		return ""
	}
	return rest[:1]
}

// headRunes / tailRunes are the first/last n characters, for a failure message
// that shows the boundary rather than the whole row.
func headRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[len(r)-n:]
	}
	return string(r)
}
