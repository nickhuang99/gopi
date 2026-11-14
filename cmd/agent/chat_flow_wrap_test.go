package main

// chat_flow_wrap_test.go — an over-wide AGENT flow row must be wrapped at the
// BAND's own width, by the producer, with the one wrap policy.
//
// The residual this closes (measured with the SGR instrument, 100×30, on top of
// 0fc25400): the agent's flow output reached the band, but a row WIDER than the
// band was handed to the band's own row loop (internal/lineedit
// region.writeBandLine → cutCols at region.Cols()), so the split column was the
// LAYOUT's decision rather than the producer's — and that loop measured bytes
// rather than cells:
//
//	row  9 |aaaa…(72 cells of 'a')|       the layout's cut, taken mid-row
//	row 10 |31mbbbbbbbbbbbbbbbbbbbb|      the sequence's own parameters,
//	                                      printed as literal TEXT
//
// The unit of truth is the RENDERED CELL GRID (Rule 10.2), never the byte log: a
// test asserting "WriteBandRow was called with the whole row" is green on the
// broken build, because the row really was handed over — it was the LAYOUT that
// re-cut it.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sgr"
)

// The probe sequence's channels: three fixed digits, so the parameters string
// it produces is a shape no ordinary text on this screen can contain.
const (
	probeR = 18
	probeG = 52
	probeB = 86
)

// bandTextOf returns the band's screen rows [top,bottom], one string per row.
func bandTextOf(h *streamHarness, top, bottom int) []string {
	rows := make([]string, 0, bottom-top+1)
	for r := top; r <= bottom; r++ {
		rows = append(rows, h.screen.Line(r))
	}
	return rows
}

// TestChatFlow_OverWideDeltaWrapsAtTheBandsOwnWidth is the defect's direct
// guard, asserted on the grid: ONE over-wide agent flow delta arrives as rows
// that (a) never exceed the band's width, (b) leave every row but the last of
// the wrap FILLING the band — the producer's wrap, not the layout's re-cut,
// which comes up short by exactly the escape bytes it counted as cells — and
// (c) leave the editing row and the status row carrying only their own text.
//
// The delta carries the agent's REAL bytes: the dim marker the reasoning stream
// opens with (`\x1b[2m`, internal/agent's thinkingMarkers) and the reset that
// closes it. Those are the bytes the broken build measured as cells.
func TestChatFlow_OverWideDeltaWrapsAtTheBandsOwnWidth(t *testing.T) {
	h := flowHarness(t)
	h.in.PublishStatus()
	h.waitFor(t, "the published model on the status row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.layout().StatusRow), flowBandModel)
	})
	l := h.layout()
	editorTop := h.editorRow(t)

	// One delta, the shape internal/agent streams: marker + a long paragraph +
	// the closing reset. Several times the band's width.
	body := strings.Repeat("r", flowBandCols*2)
	ag, seam := flowSeamAgent(t, h)
	writeFlow(t, ag, seam, sgr.DimSeq()+flowThinkingMarker+body+sgr.ResetSeq())
	h.replay()

	rows := bandTextOf(h, l.BandTop, l.BandBottom)
	// (a) no band row exceeds the band's own width.
	for i, r := range rows {
		if w := lineedit.DisplayWidth(r); w > flowBandCols {
			t.Fatalf("band row %d is %d cells wide, wider than the band's own %d: %q\n--- screen ---\n%s",
				l.BandTop+i, w, flowBandCols, r, h.screen.Contents())
		}
	}
	var carried []int
	for i, r := range rows {
		if strings.Contains(r, "r") {
			carried = append(carried, i)
		}
	}
	// (b) the delta really did wrap across several rows, and every row but the
	// last of the wrap is FULL. "Full" is the measurement that separates the
	// producer's wrap from the layout's re-cut: the re-cut measured `\x1b[2m` as
	// four cells and so ended its first row four cells short of the band.
	if len(carried) < 3 {
		t.Fatalf("the delta occupies %d band rows (want the wrap spread over at least 3):\n--- band ---\n%s",
			len(carried), strings.Join(rows, "\n"))
	}
	for n, i := range carried[:len(carried)-1] {
		if w := lineedit.DisplayWidth(rows[i]); w != flowBandCols {
			t.Fatalf("wrapped segment %d (band row %d) is %d cells wide, want the band's full %d — the row was cut at a column of the layout's choosing, not the producer's:\n--- band ---\n%s",
				n, l.BandTop+i, w, flowBandCols, strings.Join(rows, "\n"))
		}
	}
	// (c) the whole body survives, in order — a wrap must not lose or reorder a
	// cell (the measured tear printed a control sequence's parameters as text
	// instead of the body; that specific shape is pinned by the test below).
	joined := strings.ReplaceAll(strings.Join(rows, ""), " ", "")
	if !strings.Contains(joined, body) {
		t.Fatalf("the delta's text is not intact across the band's rows (wrapping must not lose or reorder a cell):\n--- band ---\n%s",
			strings.Join(rows, "\n"))
	}
	// (d) the chrome keeps its own rows — the half the epic's first defect broke.
	if got := h.screen.Line(editorTop); !strings.Contains(got, strings.TrimRight(chatPrompt, " ")) {
		t.Fatalf("the editing row lost its prompt when an over-wide delta wrapped: %q\n--- screen ---\n%s", got, h.screen.Contents())
	}
	if got := h.screen.Line(l.StatusRow); !strings.Contains(got, flowBandModel) {
		t.Fatalf("the status row lost its own text when an over-wide delta wrapped: %q\n--- screen ---\n%s", got, h.screen.Contents())
	}
}

// TestChatFlow_CutNeverLandsInsideAnEscapeSequence puts the wrap boundary
// EXACTLY on a long control sequence: the pad is `band − len(CSI)` cells, so a
// cutter that measures the introducer's bytes as cells lands between `ESC [` and
// the parameters and prints them as literal text.
//
// The smallest shape that shows it, asserted on the grid: the sequence's
// parameters may appear NOWHERE on screen, and the text either side of it is
// present and adjacent — the sequence cost zero cells, so the pad's last cell
// sits immediately before the tail.
func TestChatFlow_CutNeverLandsInsideAnEscapeSequence(t *testing.T) {
	h := flowHarness(t)
	probe := sgr.TruecolorFgSeq(probeR, probeG, probeB)
	pad := strings.Repeat("a", flowBandCols-len(sgr.CSI))
	tail := "TAILAFTER"
	ag, seam := flowSeamAgent(t, h)
	writeFlow(t, ag, seam, pad+probe+tail)
	h.replay()

	onScreen := strings.ReplaceAll(h.screen.Contents(), "\n", "")
	i := strings.Index(onScreen, tail)
	if i < 0 {
		t.Fatalf("the text after the control sequence (%q) is not on screen at all — it was consumed by the cut:\n--- screen ---\n%s",
			tail, h.screen.Contents())
	}
	// The pad's last cells are immediately before the tail: the sequence between
	// them drew no cell, so nothing fell between them.
	if !strings.HasPrefix(onScreen[:i], pad) {
		t.Fatalf("the %d cells before %q are not the pad's own %d cells — a control sequence between them was counted as cells:\n--- screen ---\n%s",
			i, tail, len(pad), h.screen.Contents())
	}
	params := sgr.TruecolorFgParams(probeR, probeG, probeB)
	if strings.Contains(onScreen, params) {
		t.Fatalf("the control sequence's parameters (%q) were printed as literal text — a row was cut inside a sequence:\n--- screen ---\n%s",
			params, h.screen.Contents())
	}
}
