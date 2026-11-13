package lineedit

// answer_band_turn_end_commit_test.go — the regression guard for the operator's
// PARTIAL REPEAT on the LAST line of a turn (task 01a0e439, the instance-2
// sighting of the defect task 01a0e433-fe9f reported).
//
// THE SHAPE THE OPERATOR SAW (verbatim, `gopi ui_state --target chat`, grid lines
// 000-002 — INSIDE the visible band, not in scrollback):
//
//	▌ 需要你决定：把 `01a0e433-19a7-...` 的实现方向定为 (a) 按键级注入（…
//	▌ 7-...` 的实现方向定为 (a) 按键级注入（`chat_surface action=key`，
//
// one sentence, then the same sentence again cut by the row's width, with the
// transcript holding it ONCE. Instance 1 (row 082) was the same shape on a line
// that did NOT wrap; instance 2 (above) is the same shape on a line that DOES.
// The wrapping case is therefore the decisive one, and the fixture below carries
// BOTH: a non-wrapping and a wrapping last line, each with its terminator and
// without.
//
// WHY THE EXISTING GUARDS CANNOT SEE IT. answer_wrap_scroll_dup_test.go's fixture
// makes every line WIDER than the band, so the wrap carry is the path it
// exercises; answer_band_narrow_end_commit_test.go's fixture makes every line
// narrower, so only the narrow path is. Neither file puts the two shapes in the
// same case, and the defect is reached by the one thing both fixtures do
// differently from a real turn: the turn's last line is the line whose commit
// goes through Finish's carry, and the carry's two halves — the parked earlier
// half and the painted tail — are queued in an order that has to match the order
// the LINE has them.
//
// THE DEFECT (measured on this tree before the fix, one seam: Finish).
// The queue Finish hands to commitPrefix is a string that commitPrefix SPLITS AT
// ITS LINE BREAKS, and a break is the boundary at which a line is decided. With
// the tail appended before the park, the park's own break landed in the MIDDLE of
// the turn's last line:
//
//	park  = "… 09-00 "                     (the busrt the hold withheld)
//	tail  = "09-01 09-02 … 09-19 尾"       (the painted continuation)
//	queue = tail + park                    ← the line's END in front of its BEGINNING
//
// commitPrefix split that at the park's break, so the first line it decided was
// `tail + park` (unterminated, and not a line the stream ever had), `writeRow`
// cut it to its first row — the park, which is what a wrap would have drawn
// anyway — and `writeLive` then painted the tail from column 0: the tail
// overwrote the live row AND committed above it. On the grid the last line is
// therefore present twice: once whole (its head, committed), once as the tail
// prefix, with nothing to repair it.
//
// WHAT IS ASSERTED, AND WHY ON THE GRID. Rule 10.2: the renderer's bytes are
// replayed into the package's own vt100 Screen and the screen's rows are read
// back, so every assertion is about cells a reader would have seen, never about a
// string the renderer produced.
//
//	tiling — the committed reading (scrollback + band, the window that holds what
//	         the band has DELIVERED; see answer_wrap_scroll_dup_test.go's header
//	         for why the band alone is a moving boundary) equals the transcript's
//	         own cells, in order: no loss, no duplication, no reordering.
//	repeat — no run of turnEndRepeatCells cells of that reading occurs twice; this
//	         is the operator's defect stated as a property of the grid.
//	head   — the last line's opening cells occur exactly ONCE in the reading. This
//	         is the sighting itself, one level more specific than `repeat`: the
//	         second copy of the operator's line was a prefix of the first.
//	piece  — every row of the reading is a CONTIGUOUS piece of the transcript, so
//	         a reordering cannot hide behind a row-wise assertion.
//
// THE SWEEP IS CADENCE x LINE SHAPE x TERMINATOR. delta 1..turnEndSweepMax visits
// every burst boundary inside the last line; the line shape decides whether that
// boundary falls in a line the band wraps; and the terminator arm is the control —
// a break in the last delta parks nothing and appends nothing, which is why the
// defect looked cadence-dependent when it is boundary-dependent. Nothing is
// skipped: a skipped cadence is a deleted assertion wearing a pass.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	// turnEndCols is ordinary and deliberately NOT marginal: the band's content
	// width is this minus the turn's gutter, and the wrapping arm's last line is
	// more than twice it while the narrow arm's is well under it. A 117-column
	// terminal measures the same defect — the geometry is not what makes it.
	turnEndCols = 100
	turnEndRows = 30
	// turnEndLines numbered lines precede the last one; every one of them is
	// ~48 cells against a ~98-cell content width, so none of them wraps and the
	// band still scrolls (there are more of them than the band has rows).
	turnEndLines       = 9
	turnEndTokensPerLn = 6
	// turnEndWrapTokens is the LAST line's token count in the wrapping arm: ~205
	// cells, more than two band rows. The narrow arm uses turnEndTokensPerLn.
	turnEndWrapTokens = 20
	// turnEndRepeatCells is the shortest run the guard treats as a repeat: under
	// one token (6 cells), so a repeated token fails it while a two- or
	// three-cell coincidence cannot. turnEndHeadCells is how much of the last
	// line's opening the `head` assertion looks for.
	turnEndRepeatCells = 12
	turnEndHeadCells   = 40
	// turnEndSweepMax is the largest delta the sweep walks; it exceeds the
	// wrapping arm's last line, so the sweep covers both "every burst falls
	// inside the last line" and "the last line arrives whole".
	turnEndSweepMax = 48
	// turnEndMaxMarksPerRow is how many right-edge cut markers a single band row
	// may carry: one row has one right edge, so a second is not a cut.
	turnEndMaxMarksPerRow = 1
)

// turnEndStrippedMarks joins the reading into one string with the band's
// right-edge cut markers removed, returning the joined string, the number of
// markers removed and how many rows carried one. THE MARKER IS NOT AN ANSWER
// CELL: a band row that had to stop inside a token wears the renderer's own
// right-edge glyph at its END (statusEllipsis, read here from the package's one
// definition rather than spelling the rune again — Rule 1) and the displaced
// character leads the row below (master's `0648b117`, "the answer band's
// right-edge cut is visible"). The transcript has no such glyph, so the tiling
// comparison drops them — while every dropped marker is COUNTED, and a row
// carrying more than turnEndMaxMarksPerRow is a failure outright: a row has one
// right edge, so a second marker is not a cut. Stripping can therefore never
// hide a lost cell, and the count is reported in the failure message.
func turnEndStrippedMarks(reading []string) (string, int, int) {
	var b strings.Builder
	marks, rows := 0, 0
	for _, row := range reading {
		n := strings.Count(row, answerCutLabel)
		if n > 0 {
			rows++
		}
		if n > turnEndMaxMarksPerRow {
			return b.String(), marks, -1
		}
		marks += n
		b.WriteString(strings.ReplaceAll(row, answerCutLabel, ""))
	}
	return b.String(), marks, rows
}

// turnEndTranscript is the answer the case streams: unique numbered tokens (an
// (i, j) pair, so no repeated run is the fixture's own doing) followed by a LAST
// line whose shape is the case — narrow (never fills a band row) or wrapping —
// whose terminator is present or not. No markup characters: the renderer strips
// emphasis markers from the cells it paints, and this case is about the commit
// boundary, not about emphasis.
func turnEndTranscript(wrapping, finalBreak bool) string {
	var b strings.Builder
	b.WriteString("开头句：窄行不折行。\n")
	for i := 0; i < turnEndLines; i++ {
		fmt.Fprintf(&b, "L%02d：", i)
		for j := 0; j < turnEndTokensPerLn; j++ {
			fmt.Fprintf(&b, "%02d-%02d ", i, j)
		}
		b.WriteString("\n")
	}
	b.WriteString("L99：")
	tokens := turnEndTokensPerLn
	if wrapping {
		tokens = turnEndWrapTokens
	}
	for j := 0; j < tokens; j++ {
		fmt.Fprintf(&b, "99-%02d ", j)
	}
	if finalBreak {
		b.WriteString("\n")
	} else {
		// The line's own end with NO break after it: the shape a turn-ending
		// stream has when the model stops mid-line, and the shape whose commit
		// goes through Finish's carry.
		b.WriteString("尾")
	}
	return b.String()
}

// turnEndDense is the transcript as the GRID can hold it: spaces and line breaks
// dropped. The renderer CONSUMES the space a wrap breaks at and trims the band's
// rows, so the transcript's air is not the grid's to reproduce; its characters
// are, in order. Nothing else is normalised — a duplicated, dropped or reordered
// character is a failure below.
func turnEndDense(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	return strings.ReplaceAll(s, "\n", "")
}

// turnEndRig streams the case one delta at a time through a real Screen whose
// scrolling region is what the renderer installed, then closes the turn the way
// the chat does (Tick, then Finish) and returns the screen plus the layout.
func turnEndRig(t *testing.T, delta int, wrapping, finalBreak bool) (*frameSink, *region) {
	t.Helper()
	sink := newFrameSink(turnEndCols, turnEndRows)
	reg := newTestRegion(sink, turnEndRows, turnEndCols)
	if !reg.Enable(turnEndRows, turnEndCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want the reserved chat layout installed", turnEndRows, turnEndCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	// The flush window is driven by the test's own clock so the cadence is exact
	// and the sweep is over the arithmetic, not over a wall clock.
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()

	text := turnEndTranscript(wrapping, finalBreak)
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
	return sink, reg
}

// turnEndReading is the band's committed output as the screen can still read it:
// the rows the band pushed off the top of its own scroll region, then the rows the
// band is showing. A committed line LEAVES the band — that is the band's whole
// mechanism — so scrollback + band is what the band has delivered.
func turnEndReading(sink *frameSink, reg *region) []string {
	var out []string
	for _, row := range sink.screen.Scrollback() {
		if text := turnEndDense(strings.TrimRight(row, " ")); text != "" {
			out = append(out, text)
		}
	}
	grid := sink.grid()
	for i := 0; i < reg.BandRows() && i < len(grid); i++ {
		if text := turnEndDense(strings.TrimRight(grid[i], " ")); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// turnEndRepeated returns any run of n cells that occurs twice in s, or "".
func turnEndRepeated(s string, n int) string {
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

// turnEndGrid renders the whole grid for a failure message.
func turnEndGrid(grid []string) string {
	var b strings.Builder
	for i, row := range grid {
		fmt.Fprintf(&b, "%3d|%s\n", i, row)
	}
	return b.String()
}

// turnEndClip shortens a quoted cell run for a failure message.
func turnEndClip(s string) string {
	if len(s) > 260 {
		return s[:260] + "…"
	}
	return s
}

// turnEndDiverge is the first cell at which two readings differ.
func turnEndDiverge(a, b string) int {
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

// turnEndLastLine is the turn's LAST line as the grid holds it — the line whose
// commit goes through Finish's carry, and the line the operator's sighting shows
// twice. finalBreak decides whether it carries the terminator's own trailing text
// (the no-break arm ends mid-line, which is the shape the sighting has).
func turnEndLastLine(wrapping, finalBreak bool) string {
	tokens := turnEndTokensPerLn
	if wrapping {
		tokens = turnEndWrapTokens
	}
	var b strings.Builder
	b.WriteString("L99：")
	for j := 0; j < tokens; j++ {
		fmt.Fprintf(&b, "99-%02d ", j)
	}
	if !finalBreak {
		b.WriteString("尾")
	}
	return turnEndDense(b.String())
}

// TestAnswerBand_TurnEndCommitDrawsTheLastLineOnce is THE case: the turn's last
// line is committed exactly once, whether it wraps or not and whether or not it
// carries its terminator.
//
// RED PROOF, AND WHAT IT FOUND (measured on the rework branch, 2026-09-28).
// Reverting ONLY the production fix — `git revert --no-commit fc3abf00`
// ("fix(lineedit): the turn-end commit assembled an in-flight line from both
// ends"), which restores the pre-fix Finish and its pre-fix emphasis.go while
// every fixture in the tree stays as it is — makes this guard go RED on BOTH
// non-terminator arms:
//
//	dup: a 12-cell run of the committed reading occurs TWICE on the grid
//	  wrapping=false/finalBreak=false, delta=1
//	  wrapping=true/finalBreak=false,  delta=1
//	tiling: … read 466 cells, transcript 463 cells (the redundant
//	  `liveText` append the same commit removed)
//
// Two facts are recorded here rather than left in the archive, because the
// archive's own prose stated the opposite of the second one:
//
//  1. The operator's symptom (`dup`, the last line drawn twice) is the assertion
//     that fires, in BOTH line shapes — so the guard fails on the SHAPE the
//     operator saw, not merely on a length mismatch.
//  2. The red is at delta=1 — the cadence at which the whole last line arrives
//     as ONE burst, so the park is empty and the tail is the whole line. The
//     archive claimed the wrapping arm was red at "all 48" swept cadences; on
//     this tree it is red at ONE. Every cadence is still swept and every cadence
//     is still asserted (nothing is skipped), but the number is measured, not
//     inherited: at the other 47 cadences the pre-fix commit is redundant rather
//     than reordered, which the `dup` assertion does not see.
//
// The run is not merely shorter for it. Both non-terminator arms fail and both
// terminator arms pass — the terminator arm being the control that already made
// the defect look cadence-dependent when it is boundary-dependent.
func TestAnswerBand_TurnEndCommitDrawsTheLastLineOnce(t *testing.T) {
	for _, wrapping := range []bool{false, true} {
		for _, finalBreak := range []bool{false, true} {
			shape := fmt.Sprintf("wrapping=%v/finalBreak=%v", wrapping, finalBreak)
			flat := turnEndDense(turnEndTranscript(wrapping, finalBreak))
			// The last line's opening cells: the copy the operator's sighting
			// showed twice, and the one the `head` assertion counts.
			last := turnEndLastLine(wrapping, finalBreak)
			head := last
			if len(head) > turnEndHeadCells {
				head = head[:turnEndHeadCells]
			}
			t.Run(shape, func(t *testing.T) {
				for delta := 1; delta <= turnEndSweepMax; delta++ {
					sink, reg := turnEndRig(t, delta, wrapping, finalBreak)
					reading := turnEndReading(sink, reg)
					shown := strings.Join(reading, "")
					at := fmt.Sprintf("%s, delta=%d", shape, delta)
					if got := turnEndRepeated(shown, turnEndRepeatCells); got != "" {
						t.Errorf("dup: a %d-cell run of the committed reading occurs TWICE on the grid — the operator's partial repeat on the turn's LAST line:\n  repeat: %q\n  reading: %q\n  (%s, %d cells read, grid:\n%s)",
							turnEndRepeatCells, got, turnEndClip(shown), at, len(shown), turnEndGrid(sink.grid()))
					}
					if n := strings.Count(shown, head); n != 1 {
						t.Errorf("head: the last line's opening cells occur %d times in the committed reading, want 1 — the line was drawn twice (whole, then as its own prefix):\n  head %q at %v\n  reading: %q\n  (%s, grid:\n%s)",
							n, head, turnEndAllIndex(shown, head), turnEndClip(shown), at, turnEndGrid(sink.grid()))
					}
					joined, marks, markedRows := turnEndStrippedMarks(reading)
					if markedRows < 0 {
						t.Fatalf("cut marker: a band row carries more than %d of the renderer's right-edge markers, and a row has one right edge:\n  rows: %q\n  (%s, grid:\n%s)",
							turnEndMaxMarksPerRow, reading, at, turnEndGrid(sink.grid()))
					}
					if strings.Contains(flat, answerCutLabel) {
						t.Fatalf("cut marker: the TRANSCRIPT itself contains the band's right-edge marker %q, so removing it from the reading could hide a dropped cell:\n  (%s)",
							answerCutLabel, at)
					}
					if joined != flat {
						d := turnEndDiverge(joined, flat)
						t.Fatalf("tiling: the committed reading is not the transcript — the answer lost, duplicated or reordered cells (each band row's right-edge cut marker removed BY NAME — %d markers across %d rows — so an added marker is never read as answer text and never hides one):\n  read %d cells, transcript %d cells\n  first divergence at cell %d: read %q / want %q\n  (%s, grid:\n%s)",
							marks, markedRows, len(joined), len(flat), d, turnEndClip(joined[d:]), turnEndClip(flat[d:]), at, turnEndGrid(sink.grid()))
					}
					for i, row := range reading {
						if !strings.Contains(flat, strings.ReplaceAll(row, answerCutLabel, "")) {
							t.Fatalf("piece: row %d of the committed reading is not a contiguous piece of the transcript (a mid-word interleave):\n  row %d: %q\n  (%s, grid:\n%s)",
								i, i, turnEndClip(row), at, turnEndGrid(sink.grid()))
						}
					}
				}
			})
		}
	}
}

// turnEndAllIndex is every offset at which needle occurs in s — the evidence a
// "drawn twice" failure carries.
func turnEndAllIndex(s, needle string) []int {
	var out []int
	for at := strings.Index(s, needle); at >= 0; at = strings.Index(s[at+1:], needle) {
		out = append(out, at)
		if len(out) > 8 {
			break
		}
	}
	return out
}
