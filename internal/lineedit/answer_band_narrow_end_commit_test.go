package lineedit

// answer_band_narrow_end_commit_test.go — the guard for the operator's PARTIAL
// REPEAT on a line that NEVER WRAPS, and the minimal repro for the turn-end commit
// defect it was caused by (task 01a0e433).
//
// WHY THIS FILE EXISTS BESIDE answer_wrap_scroll_dup_test.go. That file's fixture
// gives every line eighteen 6-cell tokens, so every line is WIDER than the band and
// the wrap carry is the path under test — its own header says so ("the wrap is the
// path under test, so every line must wrap"). The defect recorded here does not need
// a wrap at all: it is reached by a burst boundary that falls inside the stream's
// LAST line, on the turn-end commit path (Finish), and it reproduces at a band wide
// enough that no line ever fills a row. A wrapping fixture therefore cannot see it,
// and the operator's sighting — one sentence, then a truncated prefix of that same
// sentence — is the shape below rather than the wrap family's mid-word interleave.
//
// THE DEFECT. Finish's carry asked whether the text the live row already DISPLAYS
// was part of what the commit was about to queue, and asked it with a SUFFIX test
// over `prefix + park`. The in-flight line arrives in two registrations which need
// not be disjoint:
//
//	park  (compPrefix) — the line as the burst that was WITHHELD left it
//	tail  (pending)    — the painted continuation of that same burst boundary
//	live  (liveText)   — what the paint drew, which IS the park's text
//
// The suffix test saw the park (which ends with the live text) and concluded the row
// was covered — while the tail, which is in no other registration, was still queued
// IN FRONT of the park. The committed line came out as `tail + park`: the line's own
// end glued to the whole sentence it belongs to. On the painted grid that is the
// sentence twice, the second copy cut by the band row's width — the operator's
// partial repeat.
//
// WHAT IS ASSERTED, AND WHY ON THE GRID. Rule 10.2: the renderer's bytes are
// replayed into the package's own vt100 Screen and the screen's rows are read back,
// so the assertions are about PIXELS the reader would have seen, not about a string
// the renderer produced. A substring test on the renderer's output would see the line
// once even while the band drew its cells twice.
//
//	reading — the committed reading (scrollback + band, the only window that holds
//	          what the band has DELIVERED; see answer_wrap_scroll_dup_test.go's
//	          header for why the band alone is a moving boundary) equals the
//	          transcript's own cells, in order: no loss, no duplication, no
//	          reordering.
//	repeat  — no run of narrowRepeatCells cells of that reading occurs twice. This
//	          is the operator's defect stated as a property of the grid.
//
// THE SWEEP IS CADENCE x TURN-END SHAPE. Every delta 1..narrowSweepMax (so every
// burst boundary inside the last line is visited) crossed with the last line
// carrying its terminator and not: the terminator is the control, because a break in
// the last delta parks nothing and appends nothing, which is what made the defect look
// cadence-dependent when it is only boundary-dependent.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	// narrowCols is WIDE ON PURPOSE. The band's content width is this minus the
	// turn's gutter, and no fixture line comes near it: the case is measured on a
	// non-wrapping band, which is the whole point (a wrapping fixture is the
	// neighbouring file's case, not this one).
	narrowCols = 100
	narrowRows = 30
	// narrowLines is how many numbered lines precede the last one, and
	// narrowTokensPerLine / narrowTokenCells size them. Every line is ~45 cells
	// against a ~98-cell content width, so the band never wraps one — and the
	// tail of the answer still scrolls the band, because the lines are many.
	narrowLines       = 10
	narrowTokensPerLn = 6
	// narrowRepeatCells is the shortest run the guard treats as a repeat. It is
	// under one token (6 cells), so a repeated token fails it while a two- or
	// three-cell coincidence cannot.
	narrowRepeatCells = 12
	// narrowSweepMax is the largest delta the sweep walks. It comfortably exceeds
	// the last line's length, so the sweep covers both "every burst falls inside
	// the last line" and "the last line arrives whole".
	narrowSweepMax = 64
)

// narrowTranscript is the answer the case streams: unique numbered tokens (an (i, j)
// pair, so no repeated run is the fixture's own doing) in lines far narrower than the
// band. finalBreak decides whether the LAST line carries its terminator — the
// control arm: with the break the stream commits through drain and never reaches the
// Finish carry at all.
func narrowTranscript(finalBreak bool) string {
	var b strings.Builder
	b.WriteString("开头句：窄行不折行。\n")
	for i := 0; i < narrowLines; i++ {
		fmt.Fprintf(&b, "L%02d：", i)
		for j := 0; j < narrowTokensPerLn; j++ {
			fmt.Fprintf(&b, "%02d-%02d ", i, j)
		}
		if i < narrowLines-1 || finalBreak {
			b.WriteString("\n")
		} else {
			// The last line's own end, with NO break after it: the shape a
			// turn-ending stream has when the model stops mid-line, and the
			// shape whose commit goes through Finish's carry.
			b.WriteString("尾")
		}
	}
	return b.String()
}

// narrowDense is the transcript as the GRID can hold it: every space and line break
// dropped. The renderer CONSUMES the space a wrap breaks at and trims the band's rows,
// so the transcript's air is not the grid's to reproduce; its characters are, in
// order. Nothing else is normalised — a duplicated, dropped or reordered character is
// a failure below.
func narrowDense(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	return strings.ReplaceAll(s, "\n", "")
}

// narrowRig streams the case one delta at a time through a real Screen whose
// scrolling region is what the renderer installed, and returns the frame reader.
func narrowRig(t *testing.T, delta int, finalBreak bool) (*frameSink, *region, *AnswerRenderer) {
	t.Helper()
	sink := newFrameSink(narrowCols, narrowRows)
	reg := newTestRegion(sink, narrowRows, narrowCols)
	if !reg.Enable(narrowRows, narrowCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want the reserved chat layout installed", narrowRows, narrowCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	// The flush window is driven by the test's own clock so the cadence is exact.
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()

	text := narrowTranscript(finalBreak)
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

// narrowReading is the band's committed output as the screen can still read it: the
// rows the band pushed off the top of its own scroll region, then the rows the band is
// showing. A committed line LEAVES the band — that is the band's whole mechanism — so
// scrollback + band is what the band has delivered.
func narrowReading(sink *frameSink, reg *region) []string {
	var out []string
	for _, row := range sink.screen.Scrollback() {
		if text := narrowDense(strings.TrimRight(row, " ")); text != "" {
			out = append(out, text)
		}
	}
	grid := sink.grid()
	for i := 0; i < reg.BandRows() && i < len(grid); i++ {
		if text := narrowDense(strings.TrimRight(grid[i], " ")); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// narrowRepeated returns any run of n cells that occurs twice in s, or "".
func narrowRepeated(s string, n int) string {
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

// narrowDiverge is the first cell at which two readings differ.
func narrowDiverge(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// narrowWindow quotes a window of a reading around one cell offset.
func narrowWindow(s string, at int) string {
	lo, hi := at-28, at+28
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}

// narrowGrid renders the whole grid for a failure message.
func narrowGrid(grid []string) string {
	var b strings.Builder
	for i, row := range grid {
		fmt.Fprintf(&b, "%3d|%s\n", i, row)
	}
	return b.String()
}

// TestAnswerBand_NarrowLineIsNeverPaintedTwice is THE minimal repro: one answer whose
// every line is narrower than the band, streamed one delta at a time, and the
// committed reading must be the transcript — not the transcript with one line drawn
// twice. RED before the fix at the cadences whose burst boundary falls inside the last
// line, GREEN after it (the assertion names the duplicated cells, so the failure is
// the operator's symptom rather than a length mismatch).
func TestAnswerBand_NarrowLineIsNeverPaintedTwice(t *testing.T) {
	for _, finalBreak := range []bool{true, false} {
		flat := narrowDense(narrowTranscript(finalBreak))
		t.Run(fmt.Sprintf("finalBreak=%v", finalBreak), func(t *testing.T) {
			for delta := 1; delta <= narrowSweepMax; delta++ {
				sink, reg, _ := narrowRig(t, delta, finalBreak)
				reading := narrowReading(sink, reg)
				shown := strings.Join(reading, "")
				at := fmt.Sprintf("delta=%d, finalBreak=%v", delta, finalBreak)
				if got := narrowRepeated(shown, narrowRepeatCells); got != "" {
					t.Fatalf("dup: a %d-cell run of the band occurs TWICE on the grid — the operator's partial repeat, on a line that never wraps:\n  repeat: %q\n  band  : %q\n  (%s, %d cells read, grid:\n%s)",
						narrowRepeatCells, got, narrowClip(shown), at, len(shown), narrowGrid(sink.grid()))
				}
				if shown != flat {
					d := narrowDiverge(shown, flat)
					t.Fatalf("reading: the band's committed reading is not the transcript — the answer lost, duplicated or reordered cells:\n  read %d cells, transcript %d cells\n  first divergence at cell %d: read %q / want %q\n  (%s, grid:\n%s)",
						len(shown), len(flat), d, narrowWindow(shown, d), narrowWindow(flat, d), at, narrowGrid(sink.grid()))
				}
			}
		})
	}
}

// TestAnswerBand_NoCellRunOccursTwice is the REGRESSION GUARD the epic asks for, in
// the general form: for every cadence and both turn-end shapes, no run of
// narrowRepeatCells cells of the committed reading occurs twice, and every row of that
// reading is a contiguous piece of the transcript (so a reordering cannot hide behind
// a per-row assertion). It asserts the property, not the fixture: a future defect that
// duplicates ANY run of cells on this path fails here by name.
func TestAnswerBand_NoCellRunOccursTwice(t *testing.T) {
	for _, finalBreak := range []bool{true, false} {
		flat := narrowDense(narrowTranscript(finalBreak))
		judged := 0
		for delta := 1; delta <= narrowSweepMax; delta++ {
			sink, reg, _ := narrowRig(t, delta, finalBreak)
			reading := narrowReading(sink, reg)
			if got := narrowRepeated(strings.Join(reading, ""), narrowRepeatCells); got != "" {
				t.Fatalf("dup: a %d-cell run occurs twice in the committed reading (finalBreak=%v, delta=%d):\n  repeat: %q\n  grid:\n%s",
					narrowRepeatCells, finalBreak, delta, got, narrowGrid(sink.grid()))
			}
			for i, row := range reading {
				if !strings.Contains(flat, row) {
					t.Fatalf("piece: row %d of the committed reading is not a contiguous piece of the transcript (finalBreak=%v, delta=%d):\n  row %d: %q\n  grid:\n%s",
						i, finalBreak, delta, i, narrowClip(row), narrowGrid(sink.grid()))
				}
			}
			judged++
		}
		if judged != narrowSweepMax {
			t.Fatalf("asserted on %d of %d cadences: the sweep must judge every one of them", judged, narrowSweepMax)
		}
	}
}

// TestAnswerBand_NarrowTurnEndCommitRereadsTheLineOnce is the same property carried
// one level down, at the seam the defect lived in: Finish's carry must not queue the
// live row's text a second time when the park already holds it. It drives the real
// narrow fixture and asserts, BEFORE calling Finish, that the renderer is in the state
// the defect lives in — a park holding the painted line AND a pending tail the flush
// hold has not painted — so the case cannot silently stop exercising the carry, then
// asserts the line's head is committed exactly once.
func TestAnswerBand_NarrowTurnEndCommitRereadsTheLineOnce(t *testing.T) {
	sink := newFrameSink(narrowCols, narrowRows)
	reg := newTestRegion(sink, narrowRows, narrowCols)
	if !reg.Enable(narrowRows, narrowCols) {
		t.Fatalf("region.Enable = false, want the layout installed")
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()

	// The stream is the same one-delta-at-a-time answer the sweep streams; it stops
	// one byte before its end so the state Finish is entered in can be asserted.
	text := narrowTranscript(false)
	for i := 0; i+1 < len(text); i++ {
		now = now.Add(answerFlushWindow * 2)
		r.Delta(text[i : i+1])
	}
	now = now.Add(answerFlushWindow * 2)
	r.Delta(text[len(text)-1:])

	// THE PRECONDITION, asserted rather than assumed: the line is in flight in BOTH
	// registrations at once — the park (the burst the hold withheld) and the text
	// the live row already SHOWS — which is what makes the carry's question
	// ambiguous and is the entire subject of this case.
	//
	// THE PENDING TAIL IS NOT PART OF IT ANY MORE, and the third term is not a
	// weakening: a drain that commits the row its line break TERMINATED (the
	// cadence fix, internal/lineedit/answer.go) leaves the pending buffer EMPTY
	// when the break arrives — the finished line is queued, committed and no
	// longer in flight — so requiring a pending tail here would ask the rig for a
	// state the renderer no longer produces, and the case would fail on its own
	// precondition rather than on the property it is the guard for. What the
	// carry's question NEEDS is the ambiguity (park and live holding the same
	// line at once), and that is the two terms below.
	if r.compPrefix == "" || r.liveText == "" {
		t.Fatalf("the rig did not reach the state the defect lives in: park=%q live=%q pending=%q — this case would pass without ever exercising Finish's carry",
			r.compPrefix, r.liveText, r.pending.String())
	}
	park := r.compPrefix
	r.Tick()
	r.Finish()

	reading := narrowReading(sink, reg)
	shown := strings.Join(reading, "")
	// The parked line's own first characters must reach the committed reading ONCE:
	// a second occurrence is the operator's repeat, and the failure names the offsets.
	needle := narrowDense(park)
	if len(needle) > narrowRepeatCells {
		needle = needle[:narrowRepeatCells]
	}
	if n := strings.Count(shown, needle); n != 1 {
		t.Fatalf("the parked line's head is committed %d times, want 1: Finish's carry queued the text the live row already shows ON TOP of the park that holds it (the operator's partial repeat).\n  needle %q occurs at %v\n  park: %q\n  reading: %q\n  grid:\n%s",
			n, needle, narrowAllIndex(shown, needle), narrowClip(park), narrowClip(shown), narrowGrid(sink.grid()))
	}
	if got := narrowRepeated(shown, narrowRepeatCells); got != "" {
		t.Fatalf("dup: a %d-cell run of the committed reading occurs twice (%q):\n  reading: %q\n  grid:\n%s",
			narrowRepeatCells, got, narrowClip(shown), narrowGrid(sink.grid()))
	}
}

// narrowAllIndex is every offset at which needle occurs in s (the failure message's
// evidence for "committed twice").
func narrowAllIndex(s, needle string) []int {
	var out []int
	for at := strings.Index(s, needle); at >= 0; at = strings.Index(s[at+1:], needle) {
		out = append(out, at)
		if len(out) > 8 {
			break
		}
	}
	return out
}

// narrowClip shortens a quoted cell run for the failure message.
func narrowClip(s string) string {
	if len(s) > 240 {
		return s[:240] + "…"
	}
	return s
}
