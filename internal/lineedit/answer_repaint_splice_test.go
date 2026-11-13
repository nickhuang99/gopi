package lineedit

// answer_repaint_splice_test.go — THE MINIMAL REPRO for the operator's dump line
// 114: a sentence laid down on top of different content, its tail spliced over the
// old text and its inline markup left unbalanced.
//
// THE MECHANISM (measured 2026-09-27). The answer band's layout moves in TWO
// directions, and only one of them carried its erase:
//
//   - the band SHRINKS (the editing line grows): region.layout() owns a carry —
//     one LF at the band's live row promotes every committed row one row up, so
//     the rows it gives away are left blank (see the `lost` branch; measured
//     2026-09-26, session 01a0dd28).
//   - the band EXPANDS (the editing line shrinks: the message was just
//     submitted, so the input is empty again): the band's live row MOVES DOWN
//     onto a row the editing line was using, and NOTHING cleared it. That row
//     still held the editing line's last paint — its own `you> ` row, drawn with
//     `escEraseLine` before every row (render.go), so it can hold text across the
//     row's WHOLE width.
//
// The answer's next burst then writes onto that row. An answer write APPENDS at
// the row's live column, so it REPLACES the head of whatever is there and stops
// where its own characters stop: the old run's tail survives, and the two texts
// read as one line. Because the old run was the editing line's own text — inline
// markup and all — the reader sees a sentence laid down twice with unbalanced
// markup, which is exactly what the operator reported. `\033[K` is NOT the remedy
// here (this package bans it for display code): the erase is the whole row
// (escEraseLine), so the cleared span covers every width it could be replacing.
//
// The three claims this file pins, each RED on master:
//
//	T1 the frame sequence settles with the answer's own text on the row, and the
//	   replaced text is gone (the reproduction).
//	T2 the invariant: for EVERY replaced width — the old row was as wide as its
//	   own text, from one cell to the whole row — the replacement leaves nothing
//	   of it behind.
//	T3 frame-level: NO intermediate frame holds a row carrying both the replaced
//	   text and the replacing text. A final-state-only test passes while the splice
//	   is still visible to a human watching the screen; this one walks the paint
//	   stream and checks every frame the terminal would have shown.

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// spliceUserMsg is the text the editing line was showing when the band expanded
// over its rows. It is long enough to fill a row of the test's narrow terminal
// and carries the tail marker the assertions look for.
const spliceUserMsg = "提交前的编辑行文本：这条消息很长，提交之后它那一行归了 band TAILMARKER"

// spliceAnswer is the answer whose first burst lands on the row the editing line
// vacated. Its characters are disjoint from the editing line's, so a row holding
// both is a splice and nothing else.
const spliceAnswer = "我在 —— 自动 gc 已被禁用，仓库会持续膨胀。"

// spliceShortAnswer is a replacement SHORTER than the row it lands on: the shape
// where a write-and-stop leaves the replaced row's tail alive.
const spliceShortAnswer = "短答"

const (
	spliceCols = 100
	spliceRows = 24
	// spliceTailMarker identifies the editing line's text wherever it survives.
	spliceTailMarker = "TAILMARKER"
	// spliceAnswerMarker identifies a character of the answer, so a frame can be
	// asked whether it carries BOTH texts.
	spliceAnswerMarker = "仓库会持续膨胀"
)

// spliceFrame is the screen's own rows at one instant of the paint stream.
type spliceFrame struct {
	chunk int
	rows  []string
	// scroll is the screen's scrollback at this instant. A committed answer line
	// LEAVES the screen (the band's whole mechanism is one scroll of the reserved
	// region), so `rows` alone is a window on the band, not the band's output: a
	// frame-level reading of what the band has committed is scroll + rows.
	scroll []string
}

// frameSink replays the renderer's byte stream into a screen and records the
// screen's rows after EVERY write — one write is one emission (the region
// composes a placement with the bytes it positions for exactly this reason), so
// the recorded sequence is the frame sequence the terminal would have displayed.
type frameSink struct {
	screen *Screen
	frames []spliceFrame
	chunk  int
}

func newFrameSink(cols, rows int) *frameSink {
	return &frameSink{screen: NewScreen(cols, rows)}
}

func (f *frameSink) Write(p []byte) (int, error) {
	f.chunk++
	f.screen.Feed(p)
	rows := make([]string, f.screen.Rows)
	for i := range rows {
		rows[i] = strings.TrimRight(f.screen.Line(i), " ")
	}
	f.frames = append(f.frames, spliceFrame{chunk: f.chunk, rows: rows, scroll: f.screen.Scrollback()})
	return len(p), nil
}

func (f *frameSink) grid() []string {
	rows := make([]string, f.screen.Rows)
	for i := range rows {
		rows[i] = strings.TrimRight(f.screen.Line(i), " ")
	}
	return rows
}

// spliceRig drives the production layout through the operator's own sequence: the
// editing line takes TWO rows (the submitted message wrapped), then gives one
// back (the input is empty), and the answer streams onto the row that opens up.
//
// It returns the rig, so a caller can assert on the settled grid AND on every
// frame in between.
func spliceRig(t *testing.T, editorText string) (*frameSink, *region, *AnswerRenderer) {
	t.Helper()
	sink := newFrameSink(spliceCols, spliceRows)
	reg := newTestRegion(sink, spliceRows, spliceCols)
	if !reg.Enable(spliceRows, spliceCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want the layout installed", spliceRows, spliceCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()

	// The editing line occupies two rows and has painted its own text on BOTH of
	// them — the editor's own redraw, reproduced byte for byte (an erase before
	// each row, then the text).
	if !reg.SetEditorRows(2) {
		t.Fatalf("SetEditorRows(2) reported no layout change")
	}
	top := reg.EditorTop()
	for i := 0; i < 2; i++ {
		sink.Write([]byte("\x1b[" + strconv.Itoa(top+1+i) + ";1H"))
		sink.Write([]byte(escEraseLine))
		sink.Write([]byte("you> " + editorText))
	}
	return sink, reg, r
}

// TestRepaint_BandExpansionDoesNotSpliceTheOldRow is THE reproduction: the
// operator's paragraph rendered on the row the editing line vacated must not
// leave the editing line's text spliced into it.
func TestRepaint_BandExpansionDoesNotSpliceTheOldRow(t *testing.T) {
	sink, reg, r := spliceRig(t, spliceUserMsg)

	// The layout transition: the submitted message is gone, so the editing line
	// needs one row again and the band takes the other one.
	if !reg.SetEditorRows(1) {
		t.Fatalf("SetEditorRows(1) reported no layout change: the band never expanded, so this case is not exercising the defect")
	}
	live := reg.LiveRow()

	r.Delta(spliceAnswer)
	r.Tick()
	r.Finish()

	grid := sink.grid()
	if !strings.Contains(strings.Join(grid, "\n"), spliceAnswerMarker) {
		t.Fatalf("the answer never reached the screen, so this case exercises nothing: %q", grid)
	}
	if where := spliceRowIndex(grid, spliceAnswerMarker); where >= 0 {
		t.Fatalf("the row the band expanded into still carries the editing line's text — the answer was written OVER it and the old tail survived (the operator's dump line 114):\n  row %d: %q\n  whole grid:\n%s",
			where, grid[where], spliceGrid(grid))
	}
	_ = live
}

// TestRepaint_BandExpansionClearsTheWholeReplacedWidth is the INVARIANT the writer
// was missing: the erase must cover the width of the text being replaced. The
// editing line's row can hold text across its WHOLE width (it is erased with a
// whole-row erase before it is drawn), so the invariant is checked for every
// replaced width from a single cell to the whole row.
func TestRepaint_BandExpansionClearsTheWholeReplacedWidth(t *testing.T) {
	// The editing line's text is a run of one character plus the marker, so the
	// replaced width IS the length: the row's own width is what varies.
	for _, width := range []int{1, 5, 20, 50, 70, 84} {
		t.Run("width="+strconv.Itoa(width), func(t *testing.T) {
			body := strings.Repeat("Z", width) + spliceTailMarker
			sink, reg, r := spliceRig(t, body)
			if !reg.SetEditorRows(1) {
				t.Fatalf("SetEditorRows(1) reported no layout change")
			}
			// A replacement SHORTER than the text it lands on is the case that
			// leaves a tail: a two-cell answer over a row of `width` cells. It is
			// the pad-width rule in its purest form — the writer must clear what
			// is there, not merely write over it.
			r.Delta(spliceShortAnswer)
			r.Tick()
			r.Finish()
			grid := sink.grid()
			if where := spliceRowIndex(grid, spliceShortAnswer); where >= 0 {
				t.Fatalf("pad-width invariant violated at replaced width %d+cells: a row still holds the replaced text after a replacement:\n  row %d: %q\n  whole grid:\n%s",
					width+len([]rune(spliceShortAnswer)), where, grid[where], spliceGrid(grid))
			}
		})
	}
}

// TestRepaint_BandExpansionNoIntermediateFrameSplices is the FRAME-LEVEL claim:
// the corrupted intermediate state is impossible, not merely absent at the end.
//
// It walks every write the renderer emits — the paint stream a terminal would
// have shown, frame by frame — and fails on the FIRST frame holding a row that
// carries the replaced text AND the replacing text together. That frame is the
// splice as a human watching the screen sees it, and a final-text-only assertion
// cannot see it.
func TestRepaint_BandExpansionNoIntermediateFrameSplices(t *testing.T) {
	sink, reg, r := spliceRig(t, spliceUserMsg)
	if !reg.SetEditorRows(1) {
		t.Fatalf("SetEditorRows(1) reported no layout change")
	}
	r.Delta(spliceAnswer)
	r.Tick()
	r.Finish()

	for _, f := range sink.frames {
		if where := spliceRowIndex(f.rows, spliceAnswerMarker); where >= 0 {
			t.Fatalf("frame %d (of %d) is spliced: row %d carries the replaced text AND the replacing text at once — the corruption was VISIBLE on screen, even though a later frame may have repaired it:\n  row %d: %q\n  that row across the whole frame sequence:\n%s",
				f.chunk, len(sink.frames), where, where, f.rows[where], spliceRowHistory(sink.frames, where))
		}
	}
}

// TestRepaint_BandExpansionKeepsOneTurnMarkerPerRow is the guard for the shared
// SYMPTOM the operator reported separately (the duplicate `▌ ` on a markdown
// table row, answer.go's writeGutter comment): whatever the expansion clears, no
// row may end up carrying the turn's marker twice. It is asserted here so a fix
// that cleared the row by re-drawing its gutter fails loudly.
func TestRepaint_BandExpansionKeepsOneTurnMarkerPerRow(t *testing.T) {
	sink, reg, r := spliceRig(t, spliceUserMsg)
	if !reg.SetEditorRows(1) {
		t.Fatalf("SetEditorRows(1) reported no layout change")
	}
	r.Delta(spliceAnswer)
	r.Tick()
	r.Finish()
	for i, row := range sink.grid() {
		if n := strings.Count(row, "\u258c"); n > 1 {
			t.Fatalf("row %d carries the turn marker %d times, want at most 1 — a marker was carried into the row's body and drawn again:\n  row: %q",
				i, n, row)
		}
	}
}

// spliceRowIndex returns the index of a row carrying BOTH the replaced and the
// replacing text — the splice — or -1 when the grid is clean. It is the
// row-independent reading of the defect: the row the band expands into is not a
// fixed index (the band scrolls), so an assertion pinned to one row number would
// measure the wrong row on the next layout change.
func spliceRowIndex(rows []string, replacing string) int {
	for i, row := range rows {
		if strings.Contains(row, spliceTailMarker) && strings.Contains(row, replacing) {
			return i
		}
	}
	return -1
}

// spliceGrid renders a grid for a failure message: the rows that are not blank,
// numbered, so a reader can see which row moved where.
func spliceGrid(rows []string) string {
	var b strings.Builder
	for i, row := range rows {
		if row == "" {
			continue
		}
		b.WriteString(strconv.Itoa(i))
		b.WriteString("|")
		b.WriteString(row)
		b.WriteString("\n")
	}
	return b.String()
}

// spliceRowHistory renders one screen row across every frame, so a failure shows
// the sequence that produced the splice rather than one instant of it.
func spliceRowHistory(frames []spliceFrame, row int) string {
	var b strings.Builder
	for _, f := range frames {
		b.WriteString("chunk ")
		b.WriteString(strconv.Itoa(f.chunk))
		b.WriteString(": ")
		b.WriteString(strconv.Quote(f.rows[row]))
		b.WriteString("\n")
	}
	return b.String()
}
