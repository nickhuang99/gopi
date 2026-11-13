package lineedit

import (
	"strings"
	"testing"
	"time"
)

// emphasis_chunked_test.go — the defect the operator reported: `gopi chat` draws
// markdown's intra-line emphasis markers LITERALLY on the screen.
//
// The emphasis layer's own unit cases pass, because each of them hands the
// renderer a COMPLETE line. A model does not: it streams 6-byte deltas, so a
// line's `**bold**` pair is split across deltas and the line's BREAK arrives in a
// delta of its own. The renderer then paints the unfinished line on the live row
// PROVISIONALLY — with its markers intact, because removing a marker is part of
// the decision it has not taken yet (rowForPartial) — and the decision, when the
// break arrives, must reconcile that provisional paint.
//
// What is asserted here is the SCREEN (the rendered cell grid), never a callback:
// after a chunked stream the row shows the decided text exactly once, with no
// marker left behind. Both failure shapes measured on the master tip are pinned:
//
//	chunk=1   "the bold conclusion followsthe bold conclusion follows"  (duplicated)
//	chunk=2   "the **bold conclusion** follows"                         (markers kept)
//	chunk=32+ "the bold conclusion follows"                             (correct)
//
// The number is the DELTA SIZE, and that is the whole point: the same text must
// paint the same screen at every chunk size, because the operator's terminal does
// not choose how the model tokenizes.

// chunkProbeGrid drives the SHIPPED renderer over `text` cut into `chunk`-byte
// deltas, with the cadence clock advanced between them the way a real stream
// advances it, and returns the rows the terminal would show.
func chunkProbeGrid(t *testing.T, pal Palette, chunk int, text string) []string {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(emphasisProbeRows, emphasisProbeCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", emphasisProbeRows, emphasisProbeCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(pal)
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()
	for i := 0; i < len(text); i += chunk {
		end := i + chunk
		if end > len(text) {
			end = len(text)
		}
		// The gap between two deltas of one stream: longer than
		// answerFlushWindow, which is what makes the cadence flush paint the
		// unfinished line rather than waiting for its break.
		now = now.Add(emphasisChunkGap)
		r.Delta(text[i:end])
	}
	r.Tick()
	r.Finish()
	sc := NewScreen(emphasisProbeCols, emphasisProbeRows)
	sc.Feed([]byte(cap.bytes()))
	var rows []string
	for i := 0; i < emphasisProbeRows; i++ {
		if l := strings.TrimRight(sc.Line(i), " "); l != "" {
			rows = append(rows, l)
		}
	}
	return rows
}

const (
	// emphasisChunkEpoch is the fixed clock origin of the chunked probe (a real
	// clock in a cadence assertion is a flake).
	emphasisChunkEpoch = 1700000000
	// emphasisChunkGap is the pause between two deltas of one stream. It is
	// LONGER than answerFlushWindow so the cadence flush actually runs — the
	// production timing a fast local endpoint produces, and the timing this
	// defect needs.
	emphasisChunkGap = 50 * time.Millisecond
)

// TestEmphasisChunkedStreamDrawsOneDecidedRow is the regression: the SAME line
// must paint the SAME row at EVERY delta size, once, without its markers.
func TestEmphasisChunkedStreamDrawsOneDecidedRow(t *testing.T) {
	const (
		source = "the **bold conclusion** follows\n"
		drawn  = "the bold conclusion follows"
	)
	for _, chunk := range []int{1, 2, 3, 4, 6, 9, 16, 32, len(source)} {
		t.Run("chunk="+itoa(chunk), func(t *testing.T) {
			rows := chunkProbeGrid(t, BasicPalette, chunk, source)
			got := strings.Join(rows, " / ")
			if len(rows) != 1 {
				t.Fatalf("the line painted %d rows, want 1 (the answer is one line at every delta size): %q", len(rows), got)
			}
			if rows[0] != drawn {
				t.Fatalf("row = %q, want %q — the same text must paint the same row whatever the delta size is (a marker left on the row, or the line drawn twice, is the defect): the raw stream was cut into %d-byte deltas",
					rows[0], drawn, chunk)
			}
		})
	}
}

// TestEmphasisChunkedStreamMarksExactlyTheBoldCells is the emphasis half of the
// same claim: with the markers gone, the cells the format marked must be the ones
// carrying Bold — asserted on the per-cell attribute grid, so a fix that removed
// the markers by drawing the line PLAIN fails here.
func TestEmphasisChunkedStreamMarksExactlyTheBoldCells(t *testing.T) {
	const (
		source = "the **bold conclusion** follows\n"
		bold   = "bold conclusion"
	)
	for _, chunk := range []int{1, 2, 3, 6, 32} {
		t.Run("chunk="+itoa(chunk), func(t *testing.T) {
			p := chunkProbeScreen(t, BasicPalette, chunk, source)
			row := rowHolding(p, "the bold conclusion follows")
			if row < 0 {
				t.Fatalf("no row holds the drawn answer:\n%s", p.Contents())
			}
			text, styles := p.StyledLine(row)
			col := strings.Index(text, bold)
			if col < 0 {
				t.Fatalf("row %d does not hold the marked run %q: %q", row, bold, text)
			}
			for i := 0; i < len(bold); i++ {
				st := styles[col+i]
				if !st.Bold {
					t.Fatalf("cell %d of the marked run is not bold (row %d = %q, style %v): the emphasis was decided but not painted",
						i, row, strings.TrimRight(text, " "), st)
				}
			}
			// The cells outside the marked run must NOT be bold: a fix that
			// emphasised the whole row would pass the check above.
			if st := styles[col-1]; st.Bold {
				t.Fatalf("the cell before the marked run (%d) is bold, want the row's plain style: %q", col-1, text)
			}
			if end := col + len(bold); end < len(styles) && styles[end].Bold && strings.TrimSpace(text[end:]) != "" {
				t.Fatalf("the cell after the marked run (%d) is bold, want the row's plain style: %q", end, text)
			}
		})
	}
}

// chunkProbeScreen is chunkProbeGrid's screen (the grid, not its rows), so an
// attribute assertion reads the same artifact the row assertion does.
func chunkProbeScreen(t *testing.T, pal Palette, chunk int, text string) *Screen {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(emphasisProbeRows, emphasisProbeCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", emphasisProbeRows, emphasisProbeCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(pal)
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()
	for i := 0; i < len(text); i += chunk {
		end := i + chunk
		if end > len(text) {
			end = len(text)
		}
		now = now.Add(emphasisChunkGap)
		r.Delta(text[i:end])
	}
	r.Tick()
	r.Finish()
	sc := NewScreen(emphasisProbeCols, emphasisProbeRows)
	sc.Feed([]byte(cap.bytes()))
	return sc
}

// rowHolding and itoa already exist in this package's tests
// (answer_live_row_shrink_test.go, history_test.go): the chunk probe reuses them
// rather than growing a second spelling of the same helper.
