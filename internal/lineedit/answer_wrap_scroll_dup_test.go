package lineedit

// answer_wrap_scroll_dup_test.go — the regression guard for the operator's PARTIAL
// REPEAT on the painted answer band (task 01a0e2f5; the frozen grid at
// /tmp/zz_dup_grid.txt showed one transcript line reaching the screen as its own
// cells twice, the second copy truncated: `-04 29-05 … 29-17 04 29-05 …`).
//
// WHAT IT ASSERTS, AND WHY ON THE GRID. The transcript holds the line once, so the
// defect was never generation — it was the renderer's paint/wrap/carry bookkeeping,
// and the only instrument that can see it is the painted CELL GRID (Rule 10.2): the
// renderer's bytes are replayed into the package's own vt100 Screen and the screen's
// rows are read back. A string assertion on the renderer's own output sees the line
// once even while the band draws its cells twice.
//
//	tiles — the committed reading (below) equals the transcript's own cells,
//	        character for character, in order. No loss, no duplication and no
//	        reordering land anywhere in the answer.
//	dup   — no run of dupRepeatCells cells of the committed reading occurs twice.
//	        THIS is the operator's defect, stated as a property of the grid.
//	piece — every row of the committed reading is a CONTIGUOUS PIECE of the
//	        transcript, so no mid-word interleave is painted.
//
// THE READING MUST BE scrollback + BAND — this is the instrument gap this file used
// to record and now closes (measured 2026-09-27, task 01a0e355, worktree
// /home/user/workspace/gopi-01a0e355b9fd70c19fe5d61f99197fe0). The band's whole
// mechanism is that a committed line LEAVES it: Region.commitScroll emits one LF
// inside the scroll region (`CSI 1;<bandBottom> r`), which pushes the band's oldest
// row off the TOP of that region. The rig feeds a real Screen whose scrolling region
// is what the renderer set up, and the band's top margin IS the screen's first row
// (asserted by TestAnswerBand_RigReadsTheBandFromTheScreensFirstRow), so the row that
// leaves is SAVED into the screen's scrollback rather than discarded — exactly as a
// real terminal saves a line scrolled off the top.
//
// A band-only reading is therefore a WINDOW on a moving boundary, and its size is a
// function of the CADENCE, not of the renderer's health: measured on this tree, the
// band alone reads 2288 cells at delta=1, 1743 at delta=3, 1639 at delta=13 and 1326
// at 36 of the 41 swept cadences. Read as a whole, `scrollback + band` is 3255 cells —
// the transcript's own count — at EVERY cadence 1..40 and 64, tiling it exactly with
// the band as its tail. THAT is why this guard no longer skips a cadence: there is no
// cadence whose reading the instrument cannot form.
//
// THE HISTORIC SKIP LIST IS EXPLAINED, NOT TOLERATED. This file used to skip cadences
// 1 and 3 ("the band reads EMPTY") and 7/9/11/13/17/21/27/33/39 ("a short leading
// run"), and called it a known instrument gap. Both readings were the band-only
// window: the first was a real renderer loss fixed by commit 469adb94 (0 of 3255 cells
// carried), and the rest were cadences on which LESS of the answer had scrolled out,
// so the band showed MORE, not less (1414-1639 cells against the 1326 of the cadences
// that were read as whole). Neither shape reads on this tree; both are asserted green
// by the sweep below, which skips nothing.
//
// THE FIXTURE CARRIES NO MARKUP CHARACTERS. The renderer STRIPS emphasis markers
// from the cells it paints, so a fixture with backticks asks these assertions to
// compare a grid without markers against a transcript with them: the first marker
// reads as a divergence and every cell after it is reported as damage. Measured on
// this rig (2026-09-27): the entire 780-cell "loss" at delta=2 was the two backticks
// of the opening sentence. The case is about the wrap carry, not about emphasis, so
// the markers are simply absent.
//
// EVERY TOKEN IS UNIQUE — an (i, j) pair. A shared tail would make "this substring
// occurs twice" true of the TRANSCRIPT, and the assertion would be reporting the
// answer instead of the renderer.
//
// HISTORY, because this file's own claim changed. The duplication this guard covers
// was reproduced on master BEFORE commit b7d3f86a ("decide a wrapping line before
// its rows are committed"); that commit decides a wrapping line whole, which removes
// the piecewise carry the duplication needed. On master WITH b7d3f86a the duplication
// no longer reproduces at ANY delta 1..40 (measured, this rig), and the 5-line carry
// change that branch carried is byte-for-byte a no-op on the painted grid (identical
// sha256 of the band rows) — so it was reverted as dead code rather than merged. This
// file stays as the guard: it is the minimal sample for the operator's symptom, green
// on current master, and it fails at the seam if the piecewise carry is ever
// reintroduced.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	// dupCols is the terminal width the case is measured at, and dupRows its
	// height: the well of a real chat window, wide enough for a wrapping line and
	// tall enough that the answer SCROLLS the band.
	dupCols = 100
	dupRows = 30
	// dupLines is how many numbered lines follow the opening paragraph: enough
	// that the band scrolls several times over while the case streams.
	dupLines = 30
	// dupTokensPerLine and dupTokenCells size each line so that it is WIDER than
	// the band row: the wrap is the path under test, so every line must wrap.
	dupTokensPerLine = 18
	dupTokenCells    = 6
	// dupRepeatCells is the shortest run of cells the guard treats as a repeat. It
	// is well under a token (6 cells), so the assertion sees a repeated token while
	// a coincidence of two or three cells cannot fail it.
	dupRepeatCells = 10
	// dupSweepMax is the largest delta the sweep walks and dupControlDelta the
	// coarsest one: the guard runs the WHOLE measured range plus the control (a
	// whole-line-in-one-flush fixture passes over a cadence-dependent defect). The
	// control is one cadence among the sweep now, not a readable-otherwise
	// fallback: every cadence here is asserted.
	dupSweepMax     = 40
	dupControlDelta = 64
	// dupMinFrames is how many per-frame readings the frame half of this guard must
	// have judged before it will call itself a measurement. It is a floor picked
	// well under the measured count (16073 frames at delta=1, 173 at delta=64) so it
	// cannot pass on a rig that stopped driving the renderer, and it is a FLOOR, not
	// an expected value: the exact count is reported, not asserted.
	dupMinFrames = 100
)

// dupTranscript is the answer the case streams: the operator's own sentence (the one
// their dump shows duplicated) followed by numbered lines, with no markup characters
// (see the header).
func dupTranscript() string {
	var b strings.Builder
	b.WriteString("已核实：渲染器整行正确；损坏只在实时折行路径；9c6b8da8 自认的 Not-done-01 佐证。" +
		"未做：我没跑完一个窄宽度的独立复现来在单元测试里也把损坏逼出来，回合预算到了 00。\n")
	for i := 0; i < dupLines; i++ {
		fmt.Fprintf(&b, "第%02d行：", i)
		for j := 0; j < dupTokensPerLine; j++ {
			fmt.Fprintf(&b, "%02d-%02d ", i, j)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// dupDense is the transcript as the GRID can hold it: every space and line break
// dropped. The renderer deliberately CONSUMES the space a wrap breaks at (a wrap
// spends that cell on the break) and the band's rows are trimmed, so the
// transcript's own air is not the grid's to reproduce; its CHARACTERS are, in order.
// Nothing else is normalised — a duplicated, dropped or reordered character is a
// failure of the assertions below.
func dupDense(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")
	return strings.ReplaceAll(s, "\n", "")
}

// dupRig streams the case's transcript one delta at a time and returns the grid
// reader, so a caller can assert on the settled grid and on every frame the terminal
// would have shown.
func dupRig(t *testing.T, delta int) (*frameSink, *region, *AnswerRenderer) {
	t.Helper()
	sink := newFrameSink(dupCols, dupRows)
	reg := newTestRegion(sink, dupRows, dupCols)
	if !reg.Enable(dupRows, dupCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want the reserved chat layout installed", dupRows, dupCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	// The flush window is driven by the test's own clock so the cadence is exact (a
	// millisecond window asserted against a real clock is a flaky test).
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()

	text := dupTranscript()
	for i := 0; i < len(text); i += delta {
		end := i + delta
		if end > len(text) {
			end = len(text)
		}
		now = now.Add(answerFlushWindow * 2)
		r.Delta(text[i:end])
	}
	r.Tick()
	r.Finish()
	return sink, reg, r
}

// dupBandRows reads the answer band's own rows off a grid snapshot, top to bottom.
// Blank rows contribute nothing and are dropped; everything drawn in the band is
// kept, so a row that is NOT transcript text (chrome) surfaces as a tiling failure
// rather than being silently filtered away. The rig never emits such a row — it never
// calls BeginTurn, so no between-turns rule is composed — which is why keeping them
// all is safe here and is the honest reading.
func dupBandRows(rows []string, reg *region) []string {
	out := make([]string, 0, reg.BandRows())
	for i := 0; i < reg.BandRows() && i < len(rows); i++ {
		text := dupDense(cutReadingFold(strings.TrimRight(rows[i], " ")))
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

// dupReading is the BAND'S COMMITTED OUTPUT as the screen can still read it: the rows
// the band has pushed off the top of its own scroll region (the screen's scrollback,
// in the order they left it), followed by the rows the band is showing. A committed
// answer line LEAVES the band — that is the band's whole mechanism — so this, not the
// visible rows alone, is what the band has delivered (see the header).
func dupReading(scroll, rows []string, reg *region) []string {
	out := make([]string, 0, len(scroll)+reg.BandRows())
	for _, row := range scroll {
		if text := dupDense(cutReadingFold(strings.TrimRight(row, " "))); text != "" {
			out = append(out, text)
		}
	}
	return append(out, dupBandRows(rows, reg)...)
}

// dupJoin flattens a reading into its cell run.
func dupJoin(reading []string) string { return strings.Join(reading, "") }

// dupAssertBand applies the guard's invariants to the SETTLED reading of one painted
// band, naming the cadence and handing the whole grid to the failure message.
//
// tiles comes FIRST: it is the strongest statement of the operator's symptom — the
// reading IS the transcript — and it explains a failure of dup or piece far better
// than a quoted repeated substring does.
func dupAssertBand(t *testing.T, at string, reading []string, flat string, grid []string) {
	t.Helper()
	shown := dupJoin(reading)
	if shown != flat {
		d := dupDiverge(shown, flat)
		t.Fatalf("tiles: the band's committed reading is not the transcript — the answer lost, duplicated or reordered cells:\n  read %d cells, transcript %d cells\n  first divergence at cell %d: read %q / want %q\n  (%s; grid:\n%s)",
			len(shown), len(flat), d, dupClip(dupAt(shown, d)), dupClip(dupAt(flat, d)), at, dupGrid(grid))
	}
	dupAssertPiece(t, at, reading, flat, grid)
	dupAssertNoRepeat(t, at, shown, grid)
}

// dupAssertSnapshot applies the invariants that hold at EVERY instant of the stream,
// not only once it has settled: the band's committed reading is always a PREFIX of the
// transcript and never repeats a run of cells. The prefix half is the frame-level form
// of both loss and duplication — the band APPENDS the answer, so a reading that is not
// a prefix has lost, duplicated or reordered something the reader was already shown.
func dupAssertSnapshot(t *testing.T, at string, reading []string, flat string, grid []string) {
	t.Helper()
	shown := dupJoin(reading)
	if !strings.HasPrefix(flat, shown) {
		d := dupDiverge(shown, flat)
		t.Fatalf("prefix: the band's committed reading is not a prefix of the transcript — the answer was lost, duplicated or reordered mid-stream:\n  read %d cells, transcript %d cells\n  first divergence at cell %d: read %q / want %q\n  (%s; grid:\n%s)",
			len(shown), len(flat), d, dupClip(dupAt(shown, d)), dupClip(dupAt(flat, d)), at, dupGrid(grid))
	}
	dupAssertPiece(t, at, reading, flat, grid)
	dupAssertNoRepeat(t, at, shown, grid)
}

// dupAssertPiece is the piece invariant: every row of the reading is a CONTIGUOUS
// piece of the transcript, so no mid-word interleave is painted.
func dupAssertPiece(t *testing.T, at string, reading []string, flat string, grid []string) {
	t.Helper()
	for i, row := range reading {
		if !strings.Contains(flat, row) {
			t.Fatalf("piece: band row %d carries characters that appear in no transcript line, in no line's order — the mid-word interleave:\n  row  %d: %q\n  near : %q\n  (%s; grid:\n%s)",
				i, i, dupClip(row), dupClip(dupAround(flat, row)), at, dupGrid(grid))
		}
	}
}

// dupAssertNoRepeat is the dup invariant — the operator's own defect, stated as a
// property of the grid: no run of dupRepeatCells cells occurs twice.
func dupAssertNoRepeat(t *testing.T, at, shown string, grid []string) {
	t.Helper()
	if got := dupRepeated(shown, dupRepeatCells); got != "" {
		t.Fatalf("dup: a %d-cell run of the band occurs TWICE on the grid — the operator's partial repeat:\n  repeat: %q\n  band  : %q\n  (%s, %d cells read, grid:\n%s)",
			dupRepeatCells, got, dupClip(shown), at, len(shown), dupGrid(grid))
	}
}

// dupDiverge is the first cell at which two readings differ (the shorter length when
// one is a prefix of the other).
func dupDiverge(a, b string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// dupAt quotes a window of a reading around one cell offset.
func dupAt(s string, at int) string {
	if at < 0 {
		at = 0
	}
	if at > len(s) {
		at = len(s)
	}
	return s[max(0, at-30):min(len(s), at+30)]
}

// TestAnswerBand_RigReadsTheBandFromTheScreensFirstRow pins the PREMISE the shared
// reading rests on: the band's top margin is the screen's first row, so the row the
// band's own scroll pushes out is SAVED by the emulator (a line pushed off an
// interior margin is discarded by a real terminal, and a reading built on a discarded
// row would report a loss that never happened). If a layout change ever moves the
// band off row 0, this fails by name instead of turning every assertion below into a
// phantom loss.
func TestAnswerBand_RigReadsTheBandFromTheScreensFirstRow(t *testing.T) {
	_, reg, _ := dupRig(t, dupSweepMax)
	if got := reg.LiveRow() - reg.BandRows() + 1; got != 0 {
		t.Fatalf("the band's top margin is screen row %d, want 0: the band's committed rows would be DISCARDED by the emulator (scrollRows only saves a row that leaves row 0), so scrollback+band would under-count and every assertion in this file would report a phantom loss", got)
	}
}

// TestAnswerBand_WrappingLineIsNeverPaintedTwice is the settled-grid guard, swept
// across every cadence the defect was measured at and its control — no cadence is
// skipped: the reading is cadence-stable (see the header), so there is none the rig
// cannot form.
func TestAnswerBand_WrappingLineIsNeverPaintedTwice(t *testing.T) {
	flat := dupDense(dupTranscript())
	cadences := dupSweepCadences()
	judged := 0
	for _, delta := range cadences {
		t.Run(fmt.Sprintf("delta=%d", delta), func(t *testing.T) {
			sink, reg, _ := dupRig(t, delta)
			dupAssertBand(t, fmt.Sprintf("delta=%d", delta),
				dupReading(sink.screen.Scrollback(), sink.grid(), reg), flat, sink.grid())
			judged++
		})
	}
	if judged != len(cadences) {
		t.Fatalf("asserted on %d of %d cadences: the sweep must judge every one of them", judged, len(cadences))
	}
	t.Logf("cadences asserted (none skipped): %d — %v", judged, cadences)
}

// TestAnswerBand_WrappingLineIsNeverDuplicatedInAnyFrame is the FRAME-level half: a
// settled grid can be right while a frame the human watched was wrong (the band is
// written row by row, and a row is never repainted once committed). Every write's
// resulting screen is read back — scrollback + band — and the reading must be a
// PREFIX of the transcript, which is the frame-level statement of both invariants at
// once: the band only ever APPENDS the answer, so nothing it has shown is ever lost
// and nothing is ever shown twice.
func TestAnswerBand_WrappingLineIsNeverDuplicatedInAnyFrame(t *testing.T) {
	flat := dupDense(dupTranscript())
	judged := 0
	for _, delta := range dupSweepCadences() {
		sink, reg, _ := dupRig(t, delta)
		for _, fr := range sink.frames {
			dupAssertSnapshot(t, fmt.Sprintf("delta=%d at write #%d", delta, fr.chunk),
				dupReading(fr.scroll, fr.rows, reg), flat, fr.rows)
			judged++
		}
	}
	if judged < dupMinFrames {
		t.Fatalf("only %d frame readings were judged, want at least %d: the rig is not driving the renderer over the whole sweep", judged, dupMinFrames)
	}
	// Rule 27: a skip is listed and auditable, never silent — there is no skip here,
	// and the count of what was judged is reported so the claim is checkable.
	t.Logf("frames read and judged across every cadence, with no cadence skipped: %d", judged)
}

// dupSweepCadences is the sweep the guard asserts over: every delta 1..dupSweepMax
// plus the coarsest control, in one list so the settled half can prove it judged all
// of them.
func dupSweepCadences() []int {
	return append(dupSweep(dupSweepMax), dupControlDelta)
}

// dupSweep returns the deltas 1..n, the range the defect was measured over.
func dupSweep(n int) []int {
	out := make([]int, 0, n)
	for d := 1; d <= n; d++ {
		out = append(out, d)
	}
	return out
}

// dupRepeated returns any run of n cells that occurs twice in s, or "".
func dupRepeated(s string, n int) string {
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

// dupAround quotes the transcript around a row's own leading cells.
func dupAround(flat, row string) string {
	if len(row) > 20 {
		row = row[:20]
	}
	at := strings.Index(flat, row)
	if at < 0 {
		return flat[:min(len(flat), 120)]
	}
	return flat[at:min(len(flat), at+120)]
}

// dupClip shortens a quoted cell run for the failure message.
func dupClip(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// dupGrid renders the whole grid for a failure message (blank rows included, so the
// reader can see where the band ends).
func dupGrid(grid []string) string {
	var b strings.Builder
	for i, row := range grid {
		fmt.Fprintf(&b, "%3d|%s\n", i, row)
	}
	return b.String()
}
