package replayplay

// content_wrap_test.go — THE REPLAY BREAKS A ROW WHERE THE LIVE SCREEN DOES.
//
// THE DEFECT THIS FILE PINS (measured 2026-11-09, task 01a1c04f, session
// 61f5992a turn 8 — the operator quote is his own `--turn 8 --full` output).
//
// `internal/replayplay` used to own a SECOND wrap policy: material.go's
// wrapCells cut a row at the cell edge with no word boundary at all, and
// wrap.go's wrapTake broke at whitespace or at its own private separator set
// ("/-._=:,"). The live chat screen breaks through internal/lineedit's
// WrapToCols (whitespace, `/-_?&=,;)]`, or between two wide runes). One
// recording, two rules, so a token was whole on screen and chopped in the
// replay:
//
//	  的含义：实测gopi session replay … → header calls=286，正文^⚙ 行数= 286，一
//	  eader calls=8 正文0 个⚙；sessi…
//
// `header` lost its `h`; `session` became `sessi`. Both fragments are the
// pre-fix rows of the operator's own file (/tmp/t8full.txt lines 738 and 739).
//
// WHAT IS PINNED, and why it is a LINE-FOR-LINE comparison rather than a
// "looks better" assertion: the requirement is that the two surfaces share ONE
// policy (Rule 14). A property test of "no text is lost" passes for ANY wrapper,
// including the hard-chopping one this replaces — it is exactly the arm that was
// green while the replay was chopping words. So the expectation here is COMPUTED
// FROM internal/lineedit.WrapToCols, the live screen's own function: same input,
// same width, same rows, element by element. A change to either side's policy
// makes this red, which is the only thing that keeps two surfaces from drifting
// apart again.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// The long tokens a chat row actually carries, quoted from the operator's own
// session: a workspace path, a `--flag=value`, a `task=…/commit=…` chain, and a
// CJK-and-ASCII mix whose breaks are decided BETWEEN wide runes (CJK text has no
// spaces to break at). Each is longer than the canvas in at least one arm below,
// so each one forces the break decision under test.
const (
	wrapFixturePathToken   = "/home/nick/workspace/01a17e74-2f9b-4c3e-9d1f-393e636d3fce/internal/replayplay/material.go"
	wrapFixtureFlagToken   = "--turn=8 --full --cols=101 --from=auto --session=61f5992a-c126-4a55-935e-68fc80bebe19"
	wrapFixtureKeyValToken = "task=01a1c04f-3558-7c71-ad74-3ee0eaf650f7 commit=15182088 branch=master host=nick-sager"
	wrapFixtureCJKToken    = "的含义：实测gopi session replay 01a19075-80dd --turn 1 → header calls=286，正文^⚙ 行数= 286，一个不丢。"
)

// contentWrapFixtureText is one content block holding every fixture token, the
// way a tool result carries them: newline-separated paragraphs, long tokens
// inside them.
var contentWrapFixtureText = strings.Join([]string{
	wrapFixturePathToken,
	wrapFixtureFlagToken,
	wrapFixtureKeyValToken,
	wrapFixtureCJKToken,
}, "\n")

// liveScreenRows is what the LIVE surface lays the same text out as: one
// internal/lineedit.WrapToCols call per paragraph (a paragraph is a source line,
// which is how the screen's own answer rows arrive), with the block's inset
// taken OUT of the width and put back on every row — the shape contentLines
// promises.
//
// It is written from the CALLER's side on purpose: it may not call anything
// replayplay owns, or the comparison would be the implementation checked against
// itself.
func liveScreenRows(text, inset string, cols int) []string {
	room := cols - lineedit.DisplayWidth(inset)
	if room < 1 {
		room = cols
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		for _, row := range lineedit.WrapToCols(para, room) {
			out = append(out, inset+row)
		}
	}
	return out
}

// TestContentWrapEqualsTheLiveScreenRowForRow is THE regression arm: the replay's
// content rows are the live screen's rows, one for one, at every width a canvas
// actually takes.
//
// RED on the pre-fix tree, and the failure names the defect in the operator's own
// words — no round-trip property could have caught it, because the hard-chopping
// wrapper loses no byte either.
func TestContentWrapEqualsTheLiveScreenRowForRow(t *testing.T) {
	for _, cols := range []int{40, 60, 80, 100, 101, 117, 200} {
		got := contentLines(contentWrapFixtureText, cols)
		want := liveScreenRows(contentWrapFixtureText, roundIndent, cols)
		if len(got) != len(want) {
			t.Errorf("cols=%d: %d row(s) from the replay, %d from the live screen\n replay:\n%s\n live:\n%s",
				cols, len(got), len(want), strings.Join(got, "\n"), strings.Join(want, "\n"))
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("cols=%d row %d:\n replay: %q\n live:   %q", cols, i, got[i], want[i])
			}
			if w := cellsOf(got[i]); w > cols {
				t.Errorf("cols=%d row %d is %d cells wide — fitCells would cut its text: %q",
					cols, i, w, got[i])
			}
		}
	}
}

// TestContentWrapKeepsALongTokenWhole is the arm the operator measures with his
// own eyes rather than by counting rows: a path, a `--flag=value` and a
// `key=value` chain each come out WHOLE on one row at every width the replay
// really uses (101 is the dump's own geometry; 117 and 200 are the operator's).
//
// The pre-fix rows the operator filed are reproduced from his own file
// (/tmp/t8full.txt): `eader calls=8` and `sessi…`. They are matched ROW-WISE and
// only over the fixture tokens, never over the whole block — the fixture text
// itself contains `header` and `sessi…` as content, and a substring search would
// convict the fixture rather than the wrapper.
func TestContentWrapKeepsALongTokenWhole(t *testing.T) {
	const (
		choppedHeader = "eader calls="
		choppedPath   = "it -C /home/nick"
	)
	for _, cols := range []int{101, 117, 200} {
		rows := contentLines(contentWrapFixtureText, cols)
		var body strings.Builder
		for i, r := range rows {
			if i > 0 {
				body.WriteString("\n")
			}
			body.WriteString(r)
			// A row that OPENS with the tail of a token is the defect: the live
			// screen never starts a row with `eader` because it breaks before the
			// space that precedes `header`.
			if strings.HasPrefix(r, roundIndent+choppedHeader) ||
				strings.HasPrefix(r, roundIndent+choppedPath) {
				t.Errorf("cols=%d: a row starts inside a token, the way the pre-fix replay did: %q", cols, r)
			}
		}
		whole := body.String()
		for _, token := range []string{wrapFixturePathToken, wrapFixtureFlagToken, wrapFixtureKeyValToken} {
			if !strings.Contains(whole, token) {
				t.Errorf("cols=%d: %q is not whole on any row:\n%s", cols, token, whole)
			}
		}
	}
}

// TestContentWrapBreaksWhereThePolicySays pins the FOUR boundary kinds in one
// table, so a future "simplification" of the shared policy has to argue with the
// evidence rather than with one arm: after whitespace, after a soft-break
// separator, between two wide runes — and, for the one case that has no
// alternative, inside a token longer than a whole row.
//
// The expectation is COMPUTED from the live screen's own function and the arm
// then asserts the KIND of each boundary, because the kind is what a reader
// feels: a row ending on a space delivered a whole word, a row ending on a
// separator delivered a whole path element, a row ending on a wide rune ended
// between two CJK characters, and a row ending on none of those had no choice.
func TestContentWrapBreaksWhereThePolicySays(t *testing.T) {
	cases := []struct {
		name string
		text string
		cols int
		kind func(prevRow rune) bool // the rune the row ended after
		// noBoundary marks the arm whose source offers NO boundary at all, where
		// the cell cut is the only answer and the kind check cannot apply.
		noBoundary bool
	}{
		{
			name: "after whitespace",
			text: "the file is at the path below",
			cols: 14,
			kind: func(r rune) bool { return r == ' ' },
		},
		{
			name: "after a soft-break separator",
			text: "--turn=8 --full --cols=101",
			cols: 16,
			kind: func(r rune) bool { return strings.ContainsRune("-=,;)]/", r) },
		},
		{
			name: "between two wide runes",
			text: "正文行数统计",
			cols: 6,
			kind: func(r rune) bool { return cellsOf(string(r)) == 2 },
		},
		{
			// No boundary exists in the source, so the cell cut is the only answer
			// left — and the rows must still reassemble to the token.
			name:       "inside a token longer than the row",
			text:       strings.Repeat("x", 9),
			cols:       6,
			noBoundary: true,
			kind:       func(r rune) bool { return false },
		},
	}
	for _, c := range cases {
		rows := contentLines(c.text, c.cols)
		want := liveScreenRows(c.text, roundIndent, c.cols)
		if len(rows) != len(want) {
			t.Fatalf("%s: %d row(s), the live screen makes %d: %#v vs %#v",
				c.name, len(rows), len(want), rows, want)
		}
		var stripped []string
		for i := range want {
			if rows[i] != want[i] {
				t.Errorf("%s row %d: %q, the live screen makes %q", c.name, i, rows[i], want[i])
			}
			stripped = append(stripped, strings.TrimPrefix(rows[i], roundIndent))
		}
		if got := strings.Join(stripped, ""); got != c.text {
			t.Errorf("%s: the rows do not reassemble into the source:\n got: %q\nwant: %q", c.name, got, c.text)
		}
		if c.noBoundary {
			continue
		}
		// Every row except the last ends at the declared KIND of boundary.
		for i := 0; i < len(stripped)-1; i++ {
			runes := []rune(stripped[i])
			last := runes[len(runes)-1]
			if !c.kind(last) {
				t.Errorf("%s: row %d ends after %q (%U), which is not this arm's boundary kind: %q",
					c.name, i, last, last, stripped[i])
			}
		}
	}
}

// TestWrapPrefixedSharesTheLiveScreenPolicy is the CARRYING-row arm: a row that
// wears a mark at column 0 (a tool call's `⚙ `, its result's `↳ `) is wrapped by
// the same policy, with the mark taken out of the width. The continuation rows
// are stripped of the mark and joined, which is exactly the text the live screen
// would have laid out in `cols - mark` cells.
func TestWrapPrefixedSharesTheLiveScreenPolicy(t *testing.T) {
	room := func(cols int) int { return cols - cellsOf(roundResultLabel) }
	for _, cols := range []int{60, 101, 200} {
		rows := wrapPrefixed(wrapFixtureFlagToken, "", roundResultLabel, cols)
		var stripped []string
		for i, r := range rows {
			if i == 0 {
				stripped = append(stripped, r)
				continue
			}
			if !strings.HasPrefix(r, roundResultLabel) {
				t.Fatalf("cols=%d: continuation row %d lost its mark: %q", cols, i, r)
			}
			stripped = append(stripped, strings.TrimPrefix(r, roundResultLabel))
		}
		want := lineedit.WrapToCols(wrapFixtureFlagToken, room(cols))
		if len(stripped) != len(want) {
			t.Fatalf("cols=%d: %d row(s), the live screen makes %d\n replay: %#v\n live:   %#v",
				cols, len(stripped), len(want), stripped, want)
		}
		for i := range want {
			if stripped[i] != want[i] {
				t.Errorf("cols=%d row %d:\n replay: %q\n live:   %q", cols, i, stripped[i], want[i])
			}
		}
	}
}

// TestContentWrapDegenerateCanvasKeepsText is the Rule-15 arm for the inset: a
// canvas narrower than the block's own mark used to print two inset cells and
// let fitCells trim them off EVERY row, eating a character per row. The mark is
// dropped instead, and the text survives.
func TestContentWrapDegenerateCanvasKeepsText(t *testing.T) {
	rows := contentLines("abcdefgh", 2)
	if got := strings.Join(rows, ""); got != "abcdefgh" {
		t.Fatalf("a 2-cell canvas produced %q, want the text intact", got)
	}
	if got := contentLines("abcdefgh", 0); got != nil {
		t.Fatalf("cols=0 answered %#v, want nil (a canvas of no width lays out nothing)", got)
	}
}

// TestFitCellsMeasuresControlSequencesAsZeroCells is the arm the unified wrap
// needed and did not have. fitCells bounds the ROW a wrap produces, so it must
// measure the same way the wrap does — and the wrap measures through
// internal/lineedit, where an SGR sequence costs no cell. It used to charge
// those bytes as cells instead, so a scrollback row the wrap had just laid out as
// fitting was CUT by the truncator: measured in the operator's own replay,
// `Tips for getting started` (a dump scrollback row that carries its colour
// inline) came out as the bare `│` on the row before it.
//
// A sequence is CARRIED, never dropped and never charged: dropping it would
// repaint the row in the fallback palette.
func TestFitCellsMeasuresControlSequencesAsZeroCells(t *testing.T) {
	const (
		colourOn  = "\x1b[38;2;218;165;32m"
		colourOff = "\x1b[0m"
		tips      = "Tips for getting started"
	)
	row := colourOn + tips + colourOff
	got := fitCells(row, 80)
	if !strings.Contains(got, tips) {
		t.Fatalf("the truncator cut text a colour sequence was charged for: %q", got)
	}
	if !strings.HasPrefix(got, colourOn) || !strings.Contains(got, colourOff) {
		t.Fatalf("the row lost the sequence that colours it: %q", got)
	}
	// It still PADS to the canvas the way it always did, measured on the cells
	// that draw something.
	if w := cellsOf(got); w != 80 {
		t.Fatalf("the padded row is %d cells, want 80: %q", w, got)
	}
	// And a genuinely over-wide row is still truncated, at a whole rune.
	if got := fitCells(strings.Repeat("x", 20), 5); got != "xxxxx" {
		t.Fatalf("a 20-cell row truncated to 5 gave %q", got)
	}
}
