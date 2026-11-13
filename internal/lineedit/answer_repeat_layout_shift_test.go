package lineedit

// answer_repeat_layout_shift_test.go — the operator's PARTIAL REPEAT, reproduced
// on the one path the neighbouring guards do not cover: a LAYOUT SHIFT that
// happens while an answer line is still in flight.
//
// THE SHAPE THE OPERATOR PHOTOGRAPHED (2026-11-10, session
// 61f5992a-c126-4a55-935e-68fc80bebe19, chat-screen.dump.json scrollback 1817/1818):
//
//	row 1: ▌ 它当时是我在气头上拿「self-record 行与行打架」当替罪羊提交的：真正的矛盾是我自己前后说了两套话它当
//	row 2:   时是我在气头上拿「self-record 行与行打架」当替罪羊提交的：真正的矛盾是我自己前后说了两套话**（先说
//
// Row 1 ends at a band row's width and row 2 begins where row 1's text would
// have continued — the answer's line drawn twice, the second copy starting a few
// characters in, opening with the continuation indent. In the screen stream that
// pair is produced by ONE frame: `\x1b[20;1H▌ <head>\x1b[20;77H<tail>`, then a
// second one on the row BELOW it.
//
// WHAT IT IS NOT. It is not the wrap carry (answer_wrap_scroll_dup_test.go: the
// fixture there makes every line wider than the band so the WRAP is the path
// under test), not Finish's carry (answer_band_narrow_end_commit_test.go: its
// fixture never fills a row), and not a second model message
// (chat-duplicate-assistant-record.md). It needs neither a wrap-in-the-middle
// nor a turn end: it needs the EDITING LINE to give a row back — the layout
// transition every submitted message causes — while the answer is mid-line.
//
// THE MECHANISM (read off the code, then measured below). writeLive re-anchors
// before it paints (`reanchorLiveRow`), because the layout can have moved since
// the last burst. When the live row MOVES, it clears its own column/text state
// and RE-PAINTS the carried head (`r.liveText`) at the new live row — and the
// cells that same head already occupies on the row it was painted on are left
// exactly where they are. The band then scrolls that old row into the committed
// reading as a row of its own, so the reader sees the line's beginning twice:
// the left-behind copy first, whole, then the re-painted one.
//
// WHAT IS ASSERTED, AND ON WHAT. Rule 10.2: the renderer's own byte stream is
// replayed into the package's vt100 Screen and the SCREEN's rows are read back,
// so the assertions are about cells a reader would have seen.
//
//	reading — the band's committed reading (scrollback + band, the only window
//	          that holds what the band DELIVERED; a committed line leaves the
//	          band) equals the transcript's own cells, in order.
//	repeat  — no run of repeatRunCells cells of that reading occurs twice. This
//	          is the operator's defect stated as a property of the grid, and it
//	          is the same `repeat:` assertion shape the two neighbouring guards
//	          use (answer_band_narrow_end_commit_test.go, answer_wrap_scroll_dup_test.go).
//
// THE SWEEP IS SHIFT POINT x CADENCE x WIDTH. The layout shift is applied after
// every possible number of deltas, so every position inside the streaming line
// is visited, and each is crossed with several delta sizes so the shift lands
// both on a burst boundary and inside one.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	// repeatCols is the operator's own width (101 cells) — the band's content
	// width is this minus the turn's gutter, and the fixture line is wider than
	// it, which is what makes the in-flight paint fill a row before its break
	// arrives.
	repeatCols = 101
	repeatRows = 22
	// repeatEditorRowsBefore / After are the editing line's row counts on either
	// side of the shift: a submitted two-row message, then the empty input that
	// gives a row back to the band. That is the transition the chat surface
	// performs on every turn (region.SetEditorRows).
	repeatEditorRowsBefore = 2
	repeatEditorRowsAfter  = 1
	// repeatRunCells is the shortest run the guard treats as a repeat; it is well
	// under the fixture's shortest unique phrase, so a repeated word cannot make
	// it fire while a repeated fragment does.
	repeatRunCells = 12
	// repeatSweepMax is the largest delta the cadence sweep walks.
	repeatSweepMax = 16
)

// repeatTranscript is the answer under test: ONE line, wider than the band, with
// no space in it (CJK prose), so the row it fills is cut by cells rather than by
// a word boundary and its continuation opens at the block's column.
func repeatTranscript() string {
	return "它当时是我在气头上拿「self-record 行与行打架」当替罪羊提交的：真正的矛盾是我自己前后说了两套话" +
		"（先说导出被截断、后说无截断，且说的是两个不同对象而我没说清）。把它做成「写入机制要检测冲突」" +
		"是在给你的工具加复杂度，去掩盖我表述不清的问题。\n"
}

// repeatDense is the transcript as the GRID can hold it: every space and line
// break dropped, and the emphasis markers dropped (the band draws the decided
// text). A repeated, dropped or reordered character is a failure below.
func repeatDense(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\n", "")
	return strings.ReplaceAll(s, BoldMarker, "")
}

// repeatReading is the band's committed output as the screen can still read it —
// the rows the band pushed off the top of its own scroll region, then the rows it
// is showing — with the turn's gutter and the wrap continuation indent removed
// from each row (they are columns, not text: see writeGutter), and the band's cut
// label dropped (it is the renderer's own mark, not the answer's characters).
func repeatReading(sink *frameSink, reg *region) []string {
	var out []string
	collect := func(row string) {
		if text := strings.TrimRight(row, " "); text != "" {
			out = append(out, text)
		}
	}
	for _, row := range sink.screen.Scrollback() {
		collect(row)
	}
	grid := sink.grid()
	for i := 0; i < reg.BandRows() && i < len(grid); i++ {
		collect(grid[i])
	}
	return out
}

// repeatFlatten turns the reading into the one string the two assertions are
// made on, dropping the chrome the renderer adds to a row (the turn gutter, the
// continuation indent, the cut label) and keeping every character of the answer.
func repeatFlatten(reading []string) string {
	var b strings.Builder
	for _, row := range reading {
		r := strings.TrimPrefix(row, TurnMarker+" ")
		r = strings.TrimPrefix(r, "  ")
		r = strings.TrimSuffix(r, " "+answerCutLabel)
		b.WriteString(repeatDense(r))
	}
	return b.String()
}

// repeatRepeated returns any run of n cells of s that occurs twice, or "".
func repeatRepeated(s string, n int) string {
	seen := make(map[string]struct{}, len(s))
	for i := 0; i+n <= len(s); i++ {
		sub := s[i : i+n]
		if _, ok := seen[sub]; ok {
			return sub
		}
		seen[sub] = struct{}{}
	}
	return ""
}

// repeatRig streams the transcript in `delta`-byte bursts and performs the layout
// shift after `shiftAfter` deltas (a negative value never shifts — the control
// arm, which is the same stream with no layout transition at all).
func repeatRig(t *testing.T, delta, shiftAfter int) (*frameSink, *region, []string) {
	t.Helper()
	sink := newFrameSink(repeatCols, repeatRows)
	reg := newTestRegion(sink, repeatRows, repeatCols)
	if !reg.Enable(repeatRows, repeatCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want the reserved chat layout installed", repeatRows, repeatCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()
	// The editing line owns two rows while the answer starts — the submitted
	// message still on screen, which is the state the answer streams into.
	reg.SetEditorRows(repeatEditorRowsBefore)

	text := repeatTranscript()
	sent := 0
	for i := 0; i < len(text); i += delta {
		if sent == shiftAfter {
			// The input is empty now: the editing line takes one row back and the
			// band gains one — the row the answer's line is being painted across.
			reg.SetEditorRows(repeatEditorRowsAfter)
		}
		end := i + delta
		if end > len(text) {
			end = len(text)
		}
		now = now.Add(answerFlushWindow * 2)
		r.Delta(text[i:end])
		sent++
	}
	now = now.Add(answerFlushWindow * 2)
	r.Tick()
	r.Finish()
	return sink, reg, repeatReading(sink, reg)
}

// TestAnswerRepeatOnLayoutShift is THE minimal repro of the operator's partial
// repeat: one line wider than the band, streamed while the editing line gives a
// row back, and the committed reading must contain the line ONCE.
func TestAnswerRepeatOnLayoutShift(t *testing.T) {
	want := repeatDense(repeatTranscript())
	for _, delta := range []int{1, 2, 3, 5, 8, repeatSweepMax} {
		for shiftAfter := 0; shiftAfter <= len(repeatTranscript())/delta; shiftAfter++ {
			sink, reg, reading := repeatRig(t, delta, shiftAfter)
			shown := repeatFlatten(reading)
			at := fmt.Sprintf("delta=%d, shift after %d delta(s)", delta, shiftAfter)
			if got := repeatRepeated(shown, repeatRunCells); got != "" {
				t.Fatalf("repeat: a %d-cell run of the band occurs TWICE on the grid — the operator's partial repeat, with the second copy starting a few characters into the first:\n  repeat: %q\n  band  : %q\n  (%s, %d cells read, grid:\n%s)",
					repeatRunCells, got, shown, at, len(shown), repeatGrid(sink.grid()))
			}
			if shown != want {
				t.Fatalf("reading: the band's committed reading is not the transcript — the answer lost, repeated or reordered cells:\n  read %d cells, transcript %d cells\n  read %q\n  want %q\n  (%s, grid:\n%s)",
					len(shown), len(want), shown, want, at, repeatGrid(sink.grid()))
			}
			_ = reg
		}
	}
}

// TestAnswerLayoutShiftNoCellRunOccursTwice is the property-shaped guard: at
// every shift point and cadence, no run of repeatRunCells cells of the committed
// reading occurs twice, and each row of that reading is a contiguous piece of
// the transcript — so a repeat that lands on a row boundary cannot hide behind a
// per-row assertion.
func TestAnswerLayoutShiftNoCellRunOccursTwice(t *testing.T) {
	flat := repeatDense(repeatTranscript())
	judged := 0
	for _, delta := range []int{1, 2, 4, 7, 11} {
		for shiftAfter := 0; shiftAfter <= len(repeatTranscript())/delta; shiftAfter++ {
			sink, _, reading := repeatRig(t, delta, shiftAfter)
			joined := repeatFlatten(reading)
			if got := repeatRepeated(joined, repeatRunCells); got != "" {
				t.Fatalf("repeat: a %d-cell run occurs twice in the committed reading (delta=%d, shift after %d):\n  repeat: %q\n  grid:\n%s",
					repeatRunCells, delta, shiftAfter, got, repeatGrid(sink.grid()))
			}
			for i, row := range reading {
				piece := repeatDense(strings.TrimPrefix(strings.TrimPrefix(row, TurnMarker+" "), "  "))
				if piece == "" {
					continue
				}
				if !strings.Contains(flat, piece) {
					t.Fatalf("piece: row %d of the committed reading is not a contiguous piece of the transcript (delta=%d, shift after %d):\n  row %d: %q\n  grid:\n%s",
						i, delta, shiftAfter, i, piece, repeatGrid(sink.grid()))
				}
			}
			judged++
		}
	}
	if judged < 20 {
		t.Fatalf("the sweep judged only %d layout/cadence combinations, want the full sweep — a guard that looked at nothing must not pass", judged)
	}
}

// repeatGrid renders the whole grid for a failure message.
func repeatGrid(grid []string) string {
	var b strings.Builder
	for i, row := range grid {
		fmt.Fprintf(&b, "%3d|%s\n", i, row)
	}
	return b.String()
}
