package lineedit

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// fence_gutter_test.go — a fence marker that arrives WEARING the answer band's
// own row prefix.
//
// THE DEFECT (reported by the operator 2026-09-29, reproduced from his minimal
// artifact /tmp/hippo.txt; the same function, the NEXT face of the 2026-09-26
// defect recorded in fence_marker_test.go). asFenceMarker reads a marker from the
// line's FIRST non-blank character and counts only fenceIndentChars (" \t") as
// blank. The band's gutter is `▌ ` (TurnMarker, U+258C) — NOT one of them — so a
// marker behind it parsed as no marker at all:
//
//	▌ ```go    →  indent=""  rest="▌ ```go"  →  rest[0]='▌' ≠ '`'  →  not a marker
//
// The stream therefore never entered the block the marker announced and never
// left it, and every line after it was classified by whatever state the stream
// was already in — the code style painted from a fixed point onward and running
// across the answer's paragraph boundaries to its end. That is the operator's
// sighting verbatim: "从上一行的' [0]）'延续到下一行全部变色".
//
// THE SHAPE IS NOT EXOTIC, and it is why the classifier is the place this had to
// be fixed rather than a call site: the answer's OWN TEXT carries the gutter
// whenever it QUOTES a screen — the operator pasted the band's rows into his
// message, the read tool returned them (they hold the gutter), and the model
// fenced them in its answer. AnswerRenderer hands exactly those lines to
// StyleFor (measured below, TestComposerSeesTheBandGutterOnTheProductionPath).
//
// THE FIX, and why it is the classifier's: StyleFor steps over ONE leading band
// gutter before it decides anything (trimAnswerGutter, compose.go), the same
// tolerance the line's own indentation already gets. At the call sites the same
// rule would be written once per caller (Rule 1b/14), and a caller that forgot it
// would classify an answer's quoted block as prose — the defect returning through
// a new door.
//
// TWO CLAIMS again, at the two layers, because they are different questions:
//
//  1. the CLASSIFIER, by SEQUENCE
//     (TestLineComposerMarkerBehindTheBandGutterToggles): a guttered marker opens
//     and the matching guttered marker closes;
//  2. the SCREEN (TestGutterPrefixedFenceIsCodeOnTheGrid): the cell grid — the
//     marker's own cells and the block's cells carry the code style, and the
//     prose row after the CLOSING marker carries NONE. The second half is the
//     assertion that fails on the unfixed tree: a block that never closes paints
//     the rest of the answer as code.

// bandGutter is the band's two-cell prefix as the fixture spells it, taken from
// its one definition (Rule 1b) so the fixture cannot drift from the renderer.
const bandGutter = TurnMarker

// gutterProbe is one rendered guttered answer: the shipped renderer's own
// bytes, the vt100 grid they produce, and the composer that decided each line —
// the last one so the test can state WHICH SHAPE reached the classifier instead
// of inferring it from the call graph.
type gutterProbe struct {
	*emphasisProbe
	recorder *recordingComposer
}

// renderGutterAnswer drives the SHIPPED AnswerRenderer over the deltas and feeds
// what it wrote into the cell grid, with a recording composer installed.
// marked=true is the turn shape whose body rows carry the band's gutter — the
// shape the defect was reported on.
func renderGutterAnswer(t *testing.T, marked bool, deltas ...string) *gutterProbe {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(emphasisProbeRows, emphasisProbeCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", emphasisProbeRows, emphasisProbeCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(BasicPalette)
	rec := &recordingComposer{}
	r.SetComposer(rec)
	if marked {
		r.BeginTurn(TurnInfo{Index: 1, Prompt: "show me the quoted screen"})
	} else {
		r.Begin()
	}
	for _, d := range deltas {
		r.Delta(d)
	}
	r.Tick()
	r.Finish()
	if marked {
		r.EndTurn()
	}
	sc := NewScreen(emphasisProbeCols, emphasisProbeRows)
	sc.Feed([]byte(cap.bytes()))
	return &gutterProbe{emphasisProbe: &emphasisProbe{cap: cap, screen: sc}, recorder: rec}
}

// recordingComposer wraps the SHIPPED classifier and keeps every line it was
// asked about, in order — the measurement that settles "does the composer see
// the gutter, or is it stripped before it arrives?".
type recordingComposer struct {
	inner LineComposer
	seen  []string
}

func (c *recordingComposer) StyleFor(line string) LineStyle {
	c.seen = append(c.seen, line)
	return c.inner.StyleFor(line)
}

func (c *recordingComposer) Open() LineStyle { return c.inner.Open() }

// rowHasCode reports whether any cell of the row carries the palette's code
// style — the same style the renderer paints a LineCode line with (rowFor's own
// mapping), so this is the band's verdict read back off the grid.
func (p *gutterProbe) rowHasCode(styles []sgr.Style) bool {
	code := emphasisStyle(BasicPalette.Code)
	for _, st := range styles {
		if st.Equal(code) {
			return true
		}
	}
	return false
}

// screenHasCodeCells reports whether any cell anywhere on the grid carries the
// code style.
func (p *gutterProbe) screenHasCodeCells() bool {
	for row := 0; row < emphasisProbeRows; row++ {
		_, styles := p.screen.StyledLine(row)
		if p.rowHasCode(styles) {
			return true
		}
	}
	return false
}

// TestLineComposerMarkerBehindTheBandGutterToggles pins the classifier's rule by
// SEQUENCE: what a marker decides is the style of the lines after it.
func TestLineComposerMarkerBehindTheBandGutterToggles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		want  []LineStyle
	}{
		{
			name: "a guttered marker opens and the matching guttered marker closes",
			lines: []string{
				"prose before the block",
				bandGutter + FenceMarker + "go",
				bandGutter + "code line",
				bandGutter + FenceMarker,
				"prose after the block",
			},
			want: []LineStyle{LinePlain, LineCode, LineCode, LineCode, LinePlain},
		},
		{
			name: "the opening marker may be gutterless while its content is guttered",
			lines: []string{
				"prose before the block",
				FenceMarker + "go",
				bandGutter + "code line",
				bandGutter + FenceMarker,
				"prose after the block",
			},
			want: []LineStyle{LinePlain, LineCode, LineCode, LineCode, LinePlain},
		},
		{
			name: "the gutter is a row prefix, not indentation: a guttered marker still pairs with its own shape",
			lines: []string{
				bandGutter + FenceMarker,
				bandGutter + quotedMarkerIndent + FenceMarker,
				bandGutter + "code line",
				bandGutter + FenceMarker,
				"prose after the block",
			},
			// The inner marker is deeper than the block's own delimiter, so it is
			// content — the 2026-09-26 rule, unchanged by the gutter being
			// stepped over first (it composes with it rather than replacing it).
			want: []LineStyle{LineCode, LineCode, LineCode, LineCode, LinePlain},
		},
		{
			name: "the thinking gutter is read as a row prefix too",
			lines: []string{
				ThinkingGutter + FenceMarker,
				"code line",
				ThinkingGutter + FenceMarker,
				"prose after the block",
			},
			want: []LineStyle{LineCode, LineCode, LineCode, LinePlain},
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

// TestComposerSeesTheBandGutterOnTheProductionPath settles the layer question by
// MEASUREMENT rather than by reasoning about the call graph: the SHIPPED renderer
// is driven with an answer whose text carries the band's gutter (the operator's
// artifact shape) and a recording composer reports, verbatim, what StyleFor was
// handed. The guttered lines arrive WITH their gutter, so a classifier that
// refuses to look behind the product's own glyph cannot see the marker.
func TestComposerSeesTheBandGutterOnTheProductionPath(t *testing.T) {
	deltas := []string{
		FenceMarker + "go\n",
		bandGutter + "// a quoted screen row\n",
		bandGutter + FenceMarker + "\n",
	}
	probe := renderGutterAnswer(t, true, deltas...)
	rec := probe.recorder
	if rec == nil {
		t.Fatal("the renderer handed the composer no line at all")
	}
	var guttered *string
	for _, line := range rec.seen {
		if strings.HasPrefix(line, bandGutter) {
			guttered = &line
			break
		}
	}
	if guttered == nil {
		t.Fatalf("no guttered line reached StyleFor; composer saw %q", rec.seen)
	}
	if !probe.screenHasCodeCells() {
		t.Fatalf("the quoted block's cells carry no code style — the marker behind the gutter was not read:\n%s",
			probe.screen.StyledContents())
	}
	t.Logf("StyleFor received %d line(s), including %q (gutter intact)", len(rec.seen), *guttered)
}

// TestGutterPrefixedFenceIsCodeOnTheGrid is the epic's acceptance at the CELL
// level: the block the guttered markers delimit is drawn as CODE, and — the half
// that fails before the fix — the prose AFTER the block's closing marker is NOT.
func TestGutterPrefixedFenceIsCodeOnTheGrid(t *testing.T) {
	probe := renderGutterAnswer(t, true,
		"prose before the block\n",
		bandGutter+FenceMarker+"go\n",
		bandGutter+"code line\n",
		bandGutter+FenceMarker+"\n",
		"prose after the block\n",
	)

	text, styles := probe.screen.StyledLine(probe.rowWith("code line"))
	if len(text) == 0 {
		t.Fatal("the guttered block's content never reached the grid")
	}
	if !probe.rowHasCode(styles) {
		t.Fatalf("the guttered block's content row carries no code style:\n%s", probe.screen.StyledContents())
	}

	// The marker rows are code too — that is the composer's own contract for the
	// fence line, and it is what the guttered closing marker must obey to end the
	// block at the right point.
	_, marker := probe.screen.StyledLine(probe.rowWith(FenceMarker))
	if !probe.rowHasCode(marker) {
		t.Fatalf("the guttered fence marker's row carries no code style:\n%s", probe.screen.StyledContents())
	}

	// THE ANTI-LEAK ASSERTION: a block whose closing marker was not recognised
	// keeps painting — every row below it is code. This is the operator's
	// "高亮跨越段落边界一直到答案末尾", stated as cells.
	proseRow := probe.rowWith("prose after the block")
	if proseRow < 0 {
		t.Fatalf("the trailing prose never reached the grid:\n%s", probe.screen.Contents())
	}
	_, after := probe.screen.StyledLine(proseRow)
	if probe.rowHasCode(after) {
		t.Fatalf("row %d holds prose but carries the code style — the guttered closing marker did not close the block:\n%s",
			proseRow, probe.screen.StyledContents())
	}
	t.Logf("grid: content row %d is code, prose row %d is not", probe.rowWith("code line"), proseRow)
}
