package lineedit

// answer_wrap_distinct_rows_test.go — the CELL-GRID guard for the question
// "which ROW does each segment of a wrapped line land on?".
//
// THE QUESTION, and what was measured. A band row is a fixed width; a line wider
// than it is cut into segments by writeRow's loop (answer.go) and each segment
// must reach the grid on a row of its own, in order, with nothing lost — a
// reader scrolls back and reads the line segment by segment. The reported
// symptom (task 01a1b7f2) was that every segment lands on the SAME row, the
// later one covering the earlier, so only the last segment survives.
//
// MEASURED, on the shipped geometry (121 columns), by replaying a real chat
// session's SGR stream through the vt100 Screen: the segments DO land on
// distinct rows, and the reading is complete. From
// e68fddf5-32a9-4e6d-b06f-13a101295883's stream, the block
//
//	30996  ▌ 我当时的理由是“拆分成本是重定向测试”，但忽略了已有 256 KiB 上限，而 `cli_web.go` 已经超过它 374 字节。 现在每加一行
//	30997    都会让这个门更红，而它已经立了不计基线的行。
//
// is one message: its first segment settled on row 30996 and its continuation on
// 30997 — two rows, neither overwritten. Zero settled rows in that stream's
// 31006-row reading exceed the grid width, i.e. no line was ever glued into one
// over-wide row either. The premise of the report is therefore NOT the shipped
// behaviour, and this test pins that: it fails if a future change makes the
// segments share a row, which is the state the report describes.
//
// The measurement is the renderer's own bytes replayed into the package's vt100
// Screen (Rule 10.2 — pixel/cell truth, never a string assertion on the
// renderer's output, which cannot tell a placement from a write). The reading is
// scrollback + band rows, because a committed row LEAVES the screen: the
// layout's mechanism is one scroll of the reserved region, so a rig that read
// only the visible rows would call every committed line "missing".
//
// THE SMALLEST SAMPLE (Rule 10.4): one line, ASCII, no markup, no wide runes —
// nothing but the wrap arithmetic can decide the row count. Each segment gets
// its own letter, so a segment that lands twice, or out of order, is visible in
// the reading rather than merely shorter.
//
// WHAT THIS TEST DOES NOT COVER, and it is a real defect measured at the same
// time (filed separately — do not weaken this test to hide it): a line is LOST
// at some carrier chunk sizes. At 121 columns, one 200-cell line streamed in
// 64-byte chunks settles with 136 cells on the grid and 100-byte chunks with
// 100 — the head of the line never reaches a committed row — while 13-byte and
// whole-line chunks are lossless. The loss depends on the chunk boundary and on
// the width, never on the wrap itself, so it is a separate subject from the
// "which row" question this file answers.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	// wrapRowsCols is the operator's own chat width (the stream header's
	// `grid=121x24`), because the row count a reader sees depends on it.
	wrapRowsCols = 121
	// wrapRowsRows is tall enough that the whole line stays on the grid: the
	// assertion is about WHICH row a segment lands on, and a band that scrolled
	// its own content into scrollback would make the reading harder to read
	// without changing the answer (the test concatenates both anyway).
	wrapRowsRows = 24
	// wrapRowsCells is one segment-and-a-half of the width: the shortest line
	// that still has a SECOND segment to land somewhere.
	wrapRowsCells = wrapRowsCols + wrapRowsCols/2
)

// wrapRowsText is one line of distinct letters, one letter per segment-worth of
// cells, so every row of the reading names the segment it carries.
func wrapRowsText() string {
	var b strings.Builder
	for i := 0; i < wrapRowsCells; i++ {
		b.WriteRune(rune('a' + i/wrapRowsCols))
	}
	return b.String()
}

// wrapRowsReading streams the line at one carrier cadence and returns the
// settled reading (scrollback then band rows), blank rows dropped.
func wrapRowsReading(t *testing.T, burst int) []string {
	t.Helper()
	sink := newFrameSink(wrapRowsCols, wrapRowsRows)
	reg := newTestRegion(sink, wrapRowsRows, wrapRowsCols)
	if !reg.Enable(wrapRowsRows, wrapRowsCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want the reserved chat layout installed",
			wrapRowsRows, wrapRowsCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()
	text := wrapRowsText() + "\n"
	for i := 0; i < len(text); i += burst {
		end := i + burst
		if end > len(text) {
			end = len(text)
		}
		now = now.Add(answerFlushWindow * 2)
		r.Delta(text[i:end])
	}
	r.Tick()
	r.Finish()
	rows := append(append([]string{}, sink.screen.Scrollback()...), sink.grid()...)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if text := strings.TrimSpace(row); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// TestAnswerBand_WrappedSegmentsLandOnDistinctRows pins the shipped behaviour:
// one over-wide line occupies TWO band rows, the first carrying the head and the
// second its continuation, and no row is wider than the band.
//
// It fails in the reported shape (one row, the earlier segment covered) and in
// the glued shape (one row carrying the whole over-wide line).
func TestAnswerBand_WrappedSegmentsLandOnDistinctRows(t *testing.T) {
	text := wrapRowsText()

	// 13 bytes is the cadence the real stream's frames arrive at (measured);
	// whole-line is the other end, where the renderer decides the line at once.
	// (Both are lossless cadences — the burst-dependent character LOSS measured
	// at other chunk sizes is the separate defect named in this file's header,
	// not a wrap-placement property, so it is deliberately outside this guard.)
	for _, burst := range []int{13, len(text) + 1} {
		t.Run(fmt.Sprintf("burst=%d", burst), func(t *testing.T) {
			reading := wrapRowsReading(t, burst)
			if len(reading) != 2 {
				t.Fatalf("burst=%d: the over-wide line (%d cells at %d columns) settled on %d row(s), want 2 — one per segment:\n  %q",
					burst, len(text), wrapRowsCols, len(reading), reading)
			}
			for i, row := range reading {
				if w := len([]rune(row)); w > wrapRowsCols+1 { // +1: the wrap marker cell
					t.Fatalf("burst=%d: band row %d is %d cells wide, wider than the %d-column band (%q)",
						burst, i, w, wrapRowsCols, row)
				}
			}
			// The two rows must carry the LINE, in order, exactly once — which
			// is what "the later segment covered the earlier" destroys. Stated
			// as the reading itself rather than as a guessed break column: the
			// row spends one of its cells on the wrap marker, so where the cut
			// falls is the renderer's arithmetic, and a hand-computed boundary
			// would only re-encode it here.
			var joined strings.Builder
			for _, row := range reading {
				joined.WriteString(strings.TrimSuffix(row, "↵"))
			}
			if got := joined.String(); got != text {
				t.Fatalf("burst=%d: the segments did not land one per row in order — a later segment covered an earlier one:\n  want %q\n  got  %q",
					burst, text, got)
			}
			// And the SECOND row must not be a prefix of the whole line: that is
			// the reported shape (only the tail survives on the one row).
			if reading[1] == strings.TrimSuffix(reading[0], "↵") {
				t.Fatalf("burst=%d: the continuation row repeats the head row — the two segments landed on one row:\n  %q",
					burst, reading)
			}
		})
	}
}
