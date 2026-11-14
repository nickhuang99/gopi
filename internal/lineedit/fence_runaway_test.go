package lineedit

import (
	"strings"
	"testing"
)

// fence_runaway_test.go — the SECOND shape of the fence defect: a fence that
// OPENS legitimately and is never CLOSED.
//
// THE DEFECT (measured on master, task 01a0ddaa). 67066b52 fixed the marker's
// TOGGLE rule (an unpaired marker no longer hijacks the block). This is the same
// family's other shape: a fence that opens — and so is correctly LineCode itself —
// but is never closed. The classifier's `open` then survives the end of the text
// it was derived from, and every line of the NEXT assistant message is classified
// LineCode. AnswerRenderer.rowFor gates the span path on `ls != LinePlain`, so
// that message's `**bold**` is drawn as its literal markers, and its table rows
// are painted in the code style.
//
// It is not exotic. A turn with tools carries SEVERAL assistant messages, and the
// reported shape is exactly this: one message ends inside a fenced snippet (the
// session's stored copy of such a message is truncated mid-block — measured over
// this machine's own corpus: 26 prose lines carrying emphasis markers were called
// code at session scope, 0 at answer scope, 0 of them preceded by an answer that
// itself ended open) and the message after it is a TABLE whose head cell carries
// a bold span.
//
// TWO CLAIMS, at two layers, as 67066b52's guards are:
//
//  1. the COMPOSER's rule (TestLineComposerEndStreamClosesTheBlock): the block
//     state is dropped at the stream's end and nowhere earlier — a fence that IS
//     closed is unaffected, and a marker quoted inside a real CLOSED block is
//     still literal text (the invariant the `ls != LinePlain` gate exists for);
//  2. what the SCREEN gets (TestBoldAfterAnUnclosedFence...): the shipped
//     renderer draws two messages of one turn, and the cell grid is asked which
//     cells carry Bold and which carry the code style.

// unclosedFenceMessage is the shape the corpus records: an assistant message that
// quotes a snippet and ends INSIDE the fence it opened. Its own lines are correctly
// code.
const unclosedFenceMessage = "先说结论：**renderer 是字节流，不是布局树**。\n" +
	"关键的一处代码在这里：\n" +
	FenceMarker + "go\n" +
	"opts = append(opts, tea.WithInput(cfg.In), tea.WithOutput(cfg.Out))"

// nextMessageInTheSameTurn is the message AFTER it — an ordinary answer with a
// table row whose cell carries a bold span, and prose that must be prose.
const nextMessageInTheSameTurn = "| 对比项 | gopi ui | 浏览器引擎 |\n" +
	"|---|---|---|\n" +
	"| 布局状态 | `View()` 返回**一个字符串** | DOM tree |\n" +
	"\n" +
	"and the **bold conclusion** follows."

// TestLineComposerEndStreamClosesTheBlock pins the classifier's boundary rule by
// SEQUENCE: what EndStream decides is the style of the lines after it, so the
// assertion carries the state across the boundary and asserts BOTH sides of it.
//
// The rule's justification is the format's own: a fence delimiter belongs to the
// message that carries it. A message that opens a block and never closes it has
// left an unfinished quote — the next message is a new answer, and its prose is
// prose. The narrower alternative (letting the block stay open until a later
// message closes it) is rejected for the same reason: the closing delimiter would
// then be a marker in ANOTHER message's text, which is not the format.
//
// The measured basis (session corpus, 2026-09-26 — 2,598 sessions, 79,924 turns,
// 81,157 stored assistant answers, 35,846 lines that markdown's own marker parity
// says are prose and that carry an emphasis marker): classifying a whole session
// with ONE composer calls 26 of those lines code; a composer bounded at each
// answer calls 0, and every one of the 26 inherited its block from earlier text
// rather than opening it itself (0 opened it inside the same answer). A bounded
// classifier therefore removes the leaks without touching a single line that is
// genuinely inside a block.
func TestLineComposerEndStreamClosesTheBlock(t *testing.T) {
	unclosed := strings.Split(unclosedFenceMessage, "\n")
	next := strings.Split(nextMessageInTheSameTurn, "\n")

	c := &LineComposer{}
	for _, l := range unclosed {
		c.StyleFor(l)
	}
	if !c.inFence() {
		t.Fatalf("the fixture's message must END inside a block, or the test asserts nothing")
	}
	// The message's own tail is code, correctly.
	if got := c.StyleFor("still inside the quote"); got != LineCode {
		t.Fatalf("a line of the message that opened the block = %v, want LineCode", got)
	}
	// The boundary: the next message is a new answer.
	c.EndStream()
	if c.inFence() {
		t.Fatalf("EndStream left the classifier inside a block")
	}

	// The next message is classified as prose, all the way through — including the
	// table rows, which the operator's screen showed painted as code.
	for i, l := range next {
		want := LinePlain
		if got := c.StyleFor(l); got != want {
			t.Errorf("next message line %d = %q: StyleFor = %v, want %v "+
				"(the previous message's unclosed fence ran away into this one)", i, l, got, want)
		}
	}
}

// TestLineComposerEndStreamIsNotABlockClose asserts the other half of the rule: the
// reset is a boundary, never a delimiter. Text inside a block that IS closed keeps
// its classification, and a marker quoted inside such a block stays literal — the
// correct case 67066b52 shipped, re-asserted here because the runaway fix is the
// change most likely to break it.
func TestLineComposerEndStreamIsNotABlockClose(t *testing.T) {
	c := &LineComposer{}
	lines := []string{
		"here is what I saw:",
		FenceMarker,
		"you> head -3 go.mod",
		quotedMarkerIndent + FenceMarker, // quoted INSIDE the block: literal text
		"module github.com/nickhuang99/gopi",
		FenceMarker, // the block's own delimiter: closes it
		BoldMarker + "prose after the closed block" + BoldMarker,
	}
	want := []LineStyle{LinePlain, LineCode, LineCode, LineCode, LineCode, LineCode, LinePlain}
	for i, l := range lines {
		if got := c.StyleFor(l); got != want[i] {
			t.Fatalf("line %d = %q: StyleFor = %v, want %v", i, l, got, want[i])
		}
	}
	// The boundary AFTER a closed block changes nothing: there is no block to drop.
	c.EndStream()
	if got := c.StyleFor("plain prose after the boundary"); got != LinePlain {
		t.Fatalf("prose after a closed block and a boundary = %v, want LinePlain", got)
	}
}

// TestBoldAfterAnUnclosedFenceIsBoldOnTheGrid is the epic's acceptance at the CELL
// level, on the shipped renderer: ONE turn streams the unclosed-fence message, the
// caller announces the message boundary (a turn with tools has several messages),
// and the next message is drawn. The grid the bytes produce is asked which cells
// carry Bold and which carry the code style — a callback or a prose description
// could not distinguish "the row was decided LineCode" from "the cells were painted
// code", and the operator's report is a colour on the screen.
func TestBoldAfterAnUnclosedFenceIsBoldOnTheGrid(t *testing.T) {
	for _, tc := range []struct {
		name string
		// boundary is whether the caller announces the message end. Without it the
		// renderer cannot know the message ended: its input is one stream of deltas.
		boundary bool
		wantBold bool
	}{
		{
			name:     "the message boundary is announced: the next message is prose",
			boundary: true,
			wantBold: true,
		},
		{
			name:     "RED PROOF — no boundary: the unclosed fence runs away",
			boundary: false,
			wantBold: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cap := newPaletteCapture()
			reg := newRegion(cap.out)
			if !reg.Enable(emphasisProbeRows, emphasisProbeCols) {
				t.Fatalf("region.Enable(%d, %d) = false, want true", emphasisProbeRows, emphasisProbeCols)
			}
			r := NewAnswerRenderer(reg, nil)
			r.SetCols(reg.Cols())
			r.SetPalette(BasicPalette)
			r.BeginTurn(TurnInfo{Index: 1, Prompt: "who draws these lines?"})
			r.Delta(unclosedFenceMessage + "\n")
			r.Finish()
			if tc.boundary {
				r.AnswerBoundary()
			}
			r.Delta(nextMessageInTheSameTurn)
			r.Tick()
			r.Finish()
			r.EndTurn()

			sc := NewScreen(emphasisProbeCols, emphasisProbeRows)
			sc.Feed([]byte(cap.bytes()))
			probe := &emphasisProbe{cap: cap, screen: sc}

			// The verdict the CLASSIFIER reached for the next message's first line.
			// It is the losing verdict the report quotes.
			c := &LineComposer{}
			for _, l := range strings.Split(unclosedFenceMessage, "\n") {
				c.StyleFor(l)
			}
			if tc.boundary {
				c.EndStream()
			}
			row, ok := c.StyleFor(strings.Split(nextMessageInTheSameTurn, "\n")[0]), true
			t.Logf("verdict for the next message's table row: StyleFor = %v (boundary announced: %v)", row, ok)

			const span = "一个字符串"
			// The span's LENGTH is counted in runes: len(span) is its byte length,
			// and the grid is indexed by cell.
			spanRunes := len([]rune(span))
			wantRow := probe.rowWith(span)
			if wantRow < 0 {
				t.Fatalf("no screen row holds %q:\n%s", span, probe.screen.Contents())
			}
			text, styles := probe.screen.StyledLine(wantRow)
			runes := []rune(strings.TrimRight(text, " "))
			start := indexRunes(runes, span)
			if start < 0 {
				t.Fatalf("row %d = %q does not hold %q", wantRow, text, span)
			}
			var boldAt []int
			for i := start; i < start+spanRunes && i < len(styles); i++ {
				if styles[i].Bold {
					boldAt = append(boldAt, i)
				}
			}
			if got := len(boldAt) == spanRunes; got != tc.wantBold {
				t.Fatalf("row %d = %q: %d bold cell(s) in %q = %v, want %v",
					wantRow, text, len(boldAt), span, got, tc.wantBold)
			}
			t.Logf("row %d text=%q | bold cells %v of %q", wantRow, text, boldAt, span)

			if !tc.wantBold {
				// The RED proof's other half: the markers themselves reached the
				// screen, because the span path was skipped for a LineCode row.
				if !strings.Contains(text, BoldMarker) {
					t.Fatalf("the runaway did not draw its markers literally on row %d = %q", wantRow, text)
				}
			}

			// Prose is not painted in the code style once the boundary is announced.
			proseStyle := emphasisStyle(BasicPalette.Code)
			proseRow := probe.rowWith("and the")
			if proseRow >= 0 {
				_, pstyles := probe.screen.StyledLine(proseRow)
				coded := 0
				for _, st := range pstyles {
					if st.Equal(proseStyle) {
						coded++
					}
				}
				if tc.wantBold && coded > 0 {
					t.Errorf("row %d (%q) carries %d code-styled cell(s) after the boundary",
						proseRow, "and the", coded)
				}
			}
		})
	}
}
