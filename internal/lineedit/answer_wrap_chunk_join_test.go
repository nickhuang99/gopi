package lineedit

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// answer_wrap_chunk_join_test.go — the CELL-GRID guard for a long line whose
// carrier splits it, on a line made of ONE REPEATED CHARACTER.
//
// THE DEFECT THIS CASE IS THE MINIMAL REPRO FOR (task 01a1b857, the operator's
// report "121 宽 / 64 字节块只显示 136/200，行首被覆盖"). A delta boundary can
// leave a line's earlier half parked (compPrefix) while its remainder travels in
// the commit buffer. commitLines JOINS the two before deciding the line, because
// the composer is a stateful classifier and deciding a fragment alone asks it
// about a line that does not exist.
//
// The guard that stood in front of that join asked the question BY CONTENT —
// `line != r.compPrefix` / the earlier `!strings.HasPrefix(line, r.compPrefix)` —
// and content cannot answer a question about CONSTRUCTION. On a line made of one
// repeated character (`strings.Repeat("y", 200)`, a `=====` rule, a run of
// box-drawing glyphs, a bar chart), the parked half is a verbatim prefix of the
// remainder's own text, so the guard read "the half is already inside the line"
// and DROPPED it. The half was painted on the live row and never reached the
// band: measured at cols=121 with a 64-byte carrier, 136 of 200 cells landed; and
// the same defect on a CJK line lost the row's head outright. A line of unique
// tokens could not reproduce it, which is exactly why the corpus missed it.
//
// The two registrations are DIFFERENT TEXTS even when their bytes agree: r.prefix
// never contains compPrefix (the one arm that folds it in, routeBody's pipe-row
// arm, clears it), so a half that is still parked is a half this line does not
// carry yet. The join is therefore decided by which registration the text came
// from — the guard is the park's own presence — never by what the characters
// happen to be.
//
// The reading is the package's own cell grid (Rule 10.2), never a string
// assertion on the renderer's bytes: the renderer's output is fed to the vt100
// Screen and the cells are read back.

// chunkJoinRepeatCells is the length of the repeated-character line. It is wider
// than every band width the case sweeps, so every one of them wraps at least
// once — the wrap is what parks the earlier half.
const chunkJoinRepeatCells = 200

// chunkJoinBursts is the carrier's chunk size. 64 is the measured red shape and
// the operator's report; 13 and 200 are controls that carried the whole line even
// before the fix (at those sizes no boundary lands inside the parked half), so a
// red control means the instrument moved, not the renderer.
//
// 1, 2 AND 5 ARE THE SECOND ROOT CAUSE'S GRID (task 01a1b8c2, measured
// 2026-11-08). They were absent from the first fix's grid and that is why it
// reported the defect closed: at cols=100 a 200-cell line is EXACTLY two band
// rows, so a small carrier lands a boundary on the row edge and the parked half
// comes out byte-identical to the remainder the stream sends next. The join guard
// then compared the two CONTENTS (`line != r.compPrefix`), read "same
// registration" and dropped the park — 100 of 200 cells, at bursts 1, 2 and 5 and
// at no other size. The defect lives at ANY width a small carrier can fill to the
// edge; 100 is the width it was reported at.
var chunkJoinBursts = []int{1, 2, 5, 13, 64, 200}

// chunkJoinWidths sweeps the band width: the row a repeated-character line fills
// is decided by this number, so the park/commit seam is swept with it.
var chunkJoinWidths = []int{121, 100, 60, 40}

// chunkJoinStream drives one line through the production layout at one carrier
// size and returns what the band COMMITTED, as dense text.
func chunkJoinStream(t *testing.T, cols int, text string, burst int) string {
	t.Helper()
	sink := newFrameSink(cols, 24)
	reg := newTestRegion(sink, 24, cols)
	if !reg.Enable(24, cols) {
		t.Fatalf("Enable(24,%d) = false", cols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()
	runes := []rune(text)
	for i := 0; i < len(runes); i += burst {
		end := i + burst
		if end > len(runes) {
			end = len(runes)
		}
		now = now.Add(answerFlushWindow * 2)
		r.Delta(string(runes[i:end]))
	}
	r.Tick()
	r.Finish()
	var b strings.Builder
	for _, l := range sink.screen.Scrollback() {
		b.WriteString(dupDense(cutReadingFold(strings.TrimRight(l, " "))))
	}
	b.WriteString(strings.Join(dupBandRows(sink.grid(), reg), ""))
	return b.String()
}

// TestAnswerBand_RepeatedCharLineSurvivesEveryCarrierChunk is the regression
// guard: a line of one repeated character must reach the band WHOLE at every
// carrier size and every width, in order, with nothing duplicated.
func TestAnswerBand_RepeatedCharLineSurvivesEveryCarrierChunk(t *testing.T) {
	// The line is streamed WITH its terminator: a break is what makes the parked
	// half a candidate for the join at all.
	text := strings.Repeat("y", chunkJoinRepeatCells) + "\n"
	want := dupDense(text)
	for _, cols := range chunkJoinWidths {
		for _, burst := range chunkJoinBursts {
			t.Run(fmt.Sprintf("cols=%d/burst=%d", cols, burst), func(t *testing.T) {
				got := chunkJoinStream(t, cols, text, burst)
				if got == want {
					return
				}
				at := dupDiverge(got, want)
				t.Fatalf("the band lost cells from a repeated-character line — the parked earlier "+
					"half was dropped at a carrier boundary:\n  read %d cells, transcript %d cells\n"+
					"  first divergence at cell %d: read %q / want %q\n  (cols=%d, burst=%d)",
					len(got), len(want), at, dupClip(chunkJoinWindow(got, at)), dupClip(chunkJoinWindow(want, at)), cols, burst)
			})
		}
	}
}

// TestAnswerBand_RuleLineSurvivesEveryCarrierChunk is the same property on the
// OTHER family of one-repeated-character lines the corpus streams: a setext
// rule / horizontal rule / box-drawing run. It exists because the first defect
// was reported on `yyyy…` and the guard that hid it is content-shaped; a second
// fixture made of a DIFFERENT repeated character is what keeps a future
// "fix" from being tuned to the letter y.
func TestAnswerBand_RuleLineSurvivesEveryCarrierChunk(t *testing.T) {
	text := strings.Repeat("=", 60) + "\n"
	want := dupDense(text)
	for _, cols := range chunkJoinWidths {
		for _, burst := range chunkJoinBursts {
			t.Run(fmt.Sprintf("cols=%d/burst=%d", cols, burst), func(t *testing.T) {
				got := chunkJoinStream(t, cols, text, burst)
				if got == want {
					return
				}
				at := dupDiverge(got, want)
				t.Fatalf("the band lost or duplicated cells from a rule line:\n  read %d cells, "+
					"transcript %d cells\n  first divergence at cell %d: read %q / want %q\n"+
					"  (cols=%d, burst=%d)",
					len(got), len(want), at, dupClip(chunkJoinWindow(got, at)), dupClip(chunkJoinWindow(want, at)), cols, burst)
			})
		}
	}
}

// chunkJoinWindow is the failure message's view around a divergence: a few
// characters either side, so the report shows the seam rather than the whole line.
func chunkJoinWindow(s string, at int) string {
	lo, hi := at-28, at+28
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}
