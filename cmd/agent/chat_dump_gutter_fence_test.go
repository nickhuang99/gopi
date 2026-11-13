package main

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// chat_dump_gutter_fence_test.go — the PRODUCTION face of the guttered-fence
// defect (2026-09-29), measured on the artifact a real chat run leaves behind.
//
// fence_gutter_test.go (internal/lineedit) pins the classifier and the palette
// grid; this file pins the CHAT SURFACE: the same answer text, driven through the
// real chat turn renderer with the dump sink armed, and the verdict read back off
// the artifact's per-cell ATTRIBUTE runs. The two are not the same claim — the
// shared seat between the composer and a cell of the artifact is where the
// operator's sighting lived ("从上一行的' [0]）'延续到下一行全部变色"), and the
// sibling of this file (TestChatDump_QuotedFenceMarkerKeepsTheProseProse) has
// guarded the neighbouring face of the same defect since 2026-09-26 while the
// guttered shape stayed open.
//
// THE SHAPE: the answer's own text carries the band's `▌ ` prefix, because the
// answer QUOTES a screen of the product (the read tool returned a dump; the model
// put its rows in a fence). Every guttered fence marker — the closing one
// included — was therefore invisible to asFenceMarker, the block never closed,
// and every row below it was painted in the code style to the end of the answer.
//
// WHAT IS ASSERTED, per cell: the run of the block's content row carries the
// palette's CODE colour, and the run of the prose row BELOW the closing marker
// carries anything but. "Code colour" is read from the palette's own definition
// of the code style rather than typed (Rule 1b/13) — see codeStyleWord.

// codeStyleWord is the dump's spelling of the palette's code style, derived from
// the palette itself: a dump run records an sgr.Style's fields, so the code style
// is identified by the colour name its SGR sequence names. A palette whose Code
// field stops naming a foreground colour FAILS here by name (Rule 15b) instead of
// matching on an empty string that every run also contains.
func codeStyleWord(t *testing.T) string {
	t.Helper()
	seq := lineedit.BasicPalette.Code.Open()
	// sgrCyan is CSI + "36m": the palette's code style names palette colour 6.
	const cyanParam = "36"
	if !strings.Contains(seq, cyanParam) {
		t.Fatalf("BasicPalette.Code opens %q, which does not name the cyan SGR parameter %q: "+
			"the code-style assertion has no colour to match on", seq, cyanParam)
	}
	return "cyan"
}

// rowCarriesCode reports, for a dumped row, the columns whose attribute run names
// the code colour — the artifact's own answer to "is this cell code?".
func (p *chatDumpProbe) rowCarriesCode(t *testing.T, word string, row int) (int, string) {
	t.Helper()
	if row < 0 || row >= len(p.screen.AttrRuns) {
		return 0, ""
	}
	cells := 0
	var styles []string
	for _, run := range p.screen.AttrRuns[row] {
		if !strings.Contains(run.Style, "fg="+word) {
			continue
		}
		cells += run.Cells
		styles = append(styles, run.Style)
	}
	return cells, strings.Join(styles, " | ")
}

// TestChatDump_GutterPrefixedFenceClosesOnTheArtifact is the regression: on the
// real artifact, the quoted block's cells are code and the answer's closing prose
// is NOT — the assertion that fails on the unfixed tree, where the guttered
// closing marker never closed the block and the prose came out code-coloured.
//
// Two cases, because the operator's artifact and his paste differ in one detail
// and both are occurrences of the same shape:
//
//	allGuttered — the artifact verbatim (attachments/tmp/hippo.txt): EVERY line of
//	              the block carries the gutter, opening marker included. The block
//	              is entered through the guttered opening marker and left through
//	              the guttered closing one.
//	mixed       — the shape his answer actually had: a plain opening marker with
//	              guttered content, closed by a GUTTERED closing marker. This is
//	              the half that explains the sighting — the block OPENED, so the
//	              code style was live, and the guttered closing marker could not
//	              end it, so the span ran into the prose.
func TestChatDump_GutterPrefixedFenceClosesOnTheArtifact(t *testing.T) {
	const (
		fence = lineedit.FenceMarker
		// gutter is the band's own row prefix, taken from its one definition.
		gutter = lineedit.TurnMarker
		// quoted is a row of a screen the answer quotes: the same shape the
		// operator pasted out of his run ("152|▌ holdPending(tail)").
		quoted = "152|" + gutter + "holdPending(tail)"
		// prose is the answer's closing sentence, after the block closes. It is
		// only ever reached if the guttered closing marker did its job.
		prose = "so the tail loses its introducer"
	)
	for _, tc := range []struct {
		name   string
		deltas []string
	}{
		{
			name: "every block line guttered, the operator's artifact verbatim",
			deltas: []string{
				"here is what the screen showed:\n",
				gutter + fence + "go\n",
				quoted + "\n",
				gutter + fence + "\n",
				prose + "\n",
			},
		},
		{
			name: "plain opening marker, guttered content and closing marker",
			deltas: []string{
				"here is what the screen showed:\n",
				fence + "go\n",
				quoted + "\n",
				gutter + fence + "\n",
				prose + "\n",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			word := codeStyleWord(t)
			p := newChatDumpProbe(t, tc.deltas...)

			contentRow := p.textRowOf(quoted)
			if contentRow < 0 {
				t.Fatalf("the quoted row never reached the artifact:\n--- dump ---\n%s", p.grid())
			}
			if cells, styles := p.rowCarriesCode(t, word, contentRow); cells == 0 {
				t.Fatalf("the quoted block's row %d carries no cell in the code style %q — the marker behind the gutter was not read\n--- dump ---\n%s",
					contentRow, word, p.grid())
			} else {
				t.Logf("row %d: %d cell(s) in the code style (%s)", contentRow, cells, styles)
			}

			proseRow := p.textRowOf(prose)
			if proseRow < 0 {
				t.Fatalf("the closing prose never reached the artifact:\n--- dump ---\n%s", p.grid())
			}
			if cells, styles := p.rowCarriesCode(t, word, proseRow); cells != 0 {
				t.Fatalf("row %d holds the closing prose but carries %d cell(s) in the code style (%s): the guttered closing marker did not close the block — the code span ran past the block's end into the answer's prose\n--- dump ---\n%s",
					proseRow, cells, styles, p.grid())
			}
			t.Logf("artifact: quoted row %d is code, closing prose row %d is not", contentRow, proseRow)
		})
	}
}
