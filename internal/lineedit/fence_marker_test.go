package lineedit

import (
	"strings"
	"testing"
)

// fence_marker_test.go — the block marker's TOGGLE rule, and what one spurious
// marker used to cost: the rest of the answer's emphasis.
//
// THE DEFECT (measured on master 79807932, task 01a0dcc4). StyleFor toggled
// c.inFence on ANY line whose trimmed text started with a fence marker, so a
// marker that is not a block DELIMITER flipped the classifier for the rest of the
// answer. The real shape is not exotic — it is in this machine's own session
// corpus (session a8553c96): an answer quotes its own screen, and the quoted
// capture's marker is INDENTED inside the block it belongs to:
//
//	```
//	you> head -3 go.mod
//	      ```                 <- the nested capture's own marker, indented
//	```
//
// Every line after that came back LineCode, and AnswerRenderer.rowFor decides the
// drawn text from that verdict — so `**bold**` lost its emphasis AND prose was
// painted in the code style, which is exactly the pair of symptoms the operator
// reported live (2026-09-26: "texts wrapped by double star" not bold, and "this
// has a different color").
//
// TWO CLAIMS, two tests, because they are different layers:
//
//  1. the CLASSIFIER's toggle rule (TestLineComposerMarkerToggleNeedsAWholeLine):
//     a marker delimits a block only when it is a marker of the block's OWN kind,
//     at the block's OWN indentation — so a quoted (deeper) marker stays code and
//     never closes the block around it;
//  2. what the SCREEN gets (TestBoldAfterASpuriousFenceMarkerIsBoldOnTheGrid):
//     the cell grid, not a description of it — bold cells where the span is, and
//     no code-styled cells on the prose rows.

// quotedMarkerIndent is the indentation of a marker quoted INSIDE a block — the
// shape measured in session a8553c96 (a pasted screen capture, indented by six
// cells inside the fence that holds it).
const quotedMarkerIndent = "      "

// fourBacktickMarker is a longer run of the fence character: a marker that is NOT
// the delimiter of a block opened with three of them.
const fourBacktickMarker = FenceMarker + "`"

// TestLineComposerMarkerToggleNeedsAWholeLine pins the classifier's toggle rule
// by SEQUENCE, not line by line: what a marker decides is the style of the lines
// after it, so the assertion has to carry the state forward.
func TestLineComposerMarkerToggleNeedsAWholeLine(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		want  []LineStyle
	}{
		{
			name: "a quoted marker inside a block does not close it",
			lines: []string{
				"here is what I saw:",
				FenceMarker,
				"you> head -3 go.mod",
				quotedMarkerIndent + FenceMarker,
				"module github.com/nickhuang99/gopi",
				FenceMarker,
				"prose after the block",
			},
			want: []LineStyle{LinePlain, LineCode, LineCode, LineCode, LineCode, LineCode, LinePlain},
		},
		{
			name: "a longer run of the fence character does not close a shorter block",
			lines: []string{
				FenceMarker,
				fourBacktickMarker,
				"code line",
				FenceMarker,
				"prose after the block",
			},
			want: []LineStyle{LineCode, LineCode, LineCode, LineCode, LinePlain},
		},
		{
			name: "an indented marker opens a block of its own",
			lines: []string{
				"prose before",
				quotedMarkerIndent + FenceMarker,
				"code line",
				quotedMarkerIndent + FenceMarker,
				"prose after",
			},
			want: []LineStyle{LinePlain, LineCode, LineCode, LineCode, LinePlain},
		},
		{
			name: "an unpaired marker at top level still opens a block",
			lines: []string{
				"prose before",
				FenceMarker,
				"code line",
			},
			want: []LineStyle{LinePlain, LineCode, LineCode},
		},
		{
			name: "the alternative marker is matched as its own kind",
			lines: []string{
				FenceMarkerAlt,
				FenceMarker,
				"code line",
				FenceMarkerAlt,
				"prose after",
			},
			want: []LineStyle{LineCode, LineCode, LineCode, LineCode, LinePlain},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &LineComposer{}
			for i, line := range tc.lines {
				if got := c.StyleFor(line); got != tc.want[i] {
					t.Errorf("line %d: StyleFor(%q) = %v, want %v\nlines so far: %q",
						i, line, got, tc.want[i], tc.lines[:i+1])
				}
			}
		})
	}
}

// TestBoldAfterASpuriousFenceMarkerIsBoldOnTheGrid is the epic's acceptance at the
// CELL level: the shipped AnswerRenderer draws the answer, and the grid the bytes
// produce is asked which cells carry Bold and which carry the code style. A
// callback or a prose description would not distinguish "the row was decided
// LineCode" from "the cells were painted code".
func TestBoldAfterASpuriousFenceMarkerIsBoldOnTheGrid(t *testing.T) {
	for _, tc := range []struct {
		name string
		// deltas are streamed through the real renderer, in order.
		deltas []string
		// bold is the text that must carry Bold, and the row it lives on is found
		// by it — the layout's row is the layout's business.
		bold string
		// prose are rows that must NOT be drawn in the code style.
		prose []string
	}{
		{
			name: "a longer run of the fence character inside a block does not close it",
			deltas: []string{
				FenceMarker + "\n",
				fourBacktickMarker + "\n",
				"code line\n",
				FenceMarker + "\n",
				"the **bold title** line\n",
				"plain prose after it\n",
			},
			bold:  "bold title",
			prose: []string{"plain prose after it"},
		},
		{
			name: "the other fence character inside a block does not close it",
			deltas: []string{
				FenceMarker + "\n",
				"code line\n",
				FenceMarkerAlt + "\n",
				"more code\n",
				FenceMarker + "\n",
				"the **bold conclusion** follows\n",
			},
			bold:  "bold conclusion",
			prose: nil,
		},
		{
			name: "a quoted marker inside a block leaks neither the code style nor the bold",
			deltas: []string{
				"here is what I saw:\n",
				FenceMarker + "\n",
				"you> head -3 go.mod\n",
				quotedMarkerIndent + FenceMarker + "\n",
				"module github.com/nickhuang99/gopi\n",
				FenceMarker + "\n",
				"the **bold conclusion** follows\n",
				"and plain prose after the block\n",
			},
			bold:  "bold conclusion",
			prose: []string{"here is what I saw:", "and plain prose after the block"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := renderEmphasis(t, BasicPalette, false, tc.deltas...)

			// 1. the marker never reaches the screen, and the span's cells are Bold.
			row := probe.rowWith(tc.bold)
			if row < 0 {
				t.Fatalf("no screen row holds %q:\n%s", tc.bold, probe.screen.Contents())
			}
			text, styles := probe.screen.StyledLine(row)
			if strings.Contains(text, BoldMarker) {
				t.Fatalf("row %d still shows the marker: %q", row, text)
			}
			runes := []rune(strings.TrimRight(text, " "))
			start := indexRunes(runes, tc.bold)
			if start < 0 {
				t.Fatalf("row %d = %q does not hold the drawn span %q", row, text, tc.bold)
			}
			if len(runes) != len(styles) {
				t.Fatalf("row %d has %d runes but %d styles", row, len(runes), len(styles))
			}
			boldEnd := start + len(tc.bold) - 1
			var mask []byte
			got := 0
			for i := range runes {
				wantBold := i >= start && i <= boldEnd
				if styles[i].Bold {
					got++
					mask = append(mask, 'B')
				} else {
					mask = append(mask, '.')
				}
				if styles[i].Bold != wantBold {
					t.Errorf("row %d rune %d (%q) Bold = %v, want %v", row, i, runes[i], styles[i].Bold, wantBold)
				}
			}
			// The range is LOGGED as evidence: the report quotes measured cells.
			t.Logf("row %d text=%q | bold cells [%d,%d] of %d | mask %s",
				row, text, start, boldEnd, len(runes), string(mask))
			if got != len(tc.bold) {
				t.Fatalf("row %d marks %d bold cell(s), want %d:\n%s",
					row, got, len(tc.bold), probe.screen.StyledContents())
			}
			// The emphasis is the palette's own bold, not some other sequence.
			want := emphasisStyle(BasicPalette.AnswerBold)
			for i := start; i <= boldEnd; i++ {
				if !styles[i].Equal(want) {
					t.Fatalf("row %d cell %d style = %s, want the palette's AnswerBold %s", row, i, styles[i], want)
				}
			}

			// 2. prose rows are not painted with the code style: a line the composer
			// mis-read as code is visible HERE, as a colour on cells that hold prose.
			proseStyle := emphasisStyle(BasicPalette.Code)
			for _, want := range tc.prose {
				pr := probe.rowWith(want)
				if pr < 0 {
					t.Fatalf("no screen row holds the prose line %q:\n%s", want, probe.screen.Contents())
				}
				_, cells := probe.screen.StyledLine(pr)
				for i, st := range cells {
					if st.Equal(proseStyle) {
						t.Fatalf("prose row %d (%q) cell %d carries the code style %s:\n%s",
							pr, want, i, st, probe.screen.StyledContents())
					}
				}
			}
		})
	}
}
