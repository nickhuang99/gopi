package lineedit

// ambiguous_width_test.go — THE MINIMAL REPRODUCTION of the reported defect, with
// the OLD behaviour pinned so the test is provably red on the implementation that
// shipped it.
//
// THE DEFECT. `runeCells` measured East Asian Width W/F as two cells and
// everything else as one, so a rune of the AMBIGUOUS (A) class — box-drawing
// U+2500–257F in full, `—` U+2014, `→` U+2192, `▌` U+258C, the enclosed numbers —
// was counted as ONE cell in every environment. A terminal drawing them two cells
// wide (the CJK reading) therefore shows more cells than gopi counted: the wrap
// comes late, the layout reports fewer rows than the terminal made, and the cursor
// is put on the ROW ABOVE the one holding the text — exactly the reported symptom
// (paste a Chinese web table; the longer the text, the further off).
//
// WHY THE PINNED-OLD CASE MATTERS (Rule 10.4, red before green). The min repro
// alone cannot show a red state: on master the A runes simply ARE one cell, so a
// test that asked for the new answer would fail for the wrong reason (it would be
// asserting a feature, not a regression). So both readings are asserted from the
// SAME fixtures, and `ambiguousNarrowCells` is the old implementation, written out
// by hand — if the tables are ever changed so that the narrow reading is no longer
// reachable, this file fails and says so.

import (
	"testing"
	"unicode"

	"github.com/nickhuang99/gopi/internal/config"
)

// ambiguousNarrowCells is the OLD runeCells: W/F two cells, everything else one,
// with no notion of an ambiguous class. It exists ONLY as this suite's pinned red
// side — it is the measurement the defect shipped with, and reproducing it here is
// what makes the failure provable rather than narrated.
//
// It is a hand-written mirror on purpose: calling the production predicate with the
// env pinned narrow would prove the KNOB works, not that the old code was wrong.
func ambiguousNarrowCells(r rune) int {
	switch {
	case r == 0, r == '\n', r == '\r':
		return 0
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), unicode.Is(unicode.Cf, r):
		return 0
	case wideOnlyCells(r):
		return 2
	default:
		return 1
	}
}

// wideOnlyCells is the W/F half of the production predicate, reachable without the
// ambiguous half: the local wideRanges table directly.
func wideOnlyCells(r rune) bool {
	for _, rg := range wideRanges {
		if r >= rg.lo && r <= rg.hi {
			return true
		}
	}
	return false
}

// ambiguousPaste is the operator's own shape reduced to its smallest sample: one
// box-drawing row of a pasted table, four cells of Chinese and the symbols that
// made the drift visible. `─`/`│` are the defect's own class (EAW A, and NOT in
// any W/F range — the neighbouring block U+2E80 is above U+2500, which is why it
// was missed).
const ambiguousPaste = "│ 第一行 ─ 51G │"

func TestAmbiguousRunesAreMeasuredByTheEnvironment(t *testing.T) {
	const wantWide = 2
	const wantNarrow = 1

	t.Run("wide environment", func(t *testing.T) {
		t.Setenv(config.AmbiguousWidthEnv, config.AmbiguousWidthWide)
		for _, r := range []rune{'─', '│', '═', '━', '▌', '—', '→', '①'} {
			if got := runeCells(r); got != wantWide {
				t.Errorf("runeCells(%q %#x) = %d cells with %s=wide, want %d",
					r, r, got, config.AmbiguousWidthEnv, wantWide)
			}
		}
		// 19 cells: │(2) sp 第(2) 一(2) 行(2) sp ─(2) sp 5 1 G sp │(2) — the same
		// string is 13 cells in the narrow reading below, and the six cells of
		// difference come from the three ambiguous runes alone.
		if got, want := displayWidth(ambiguousPaste), 19; got != want {
			t.Errorf("displayWidth(%q) = %d with %s=wide, want %d", ambiguousPaste, got, config.AmbiguousWidthEnv, want)
		}
	})

	t.Run("narrow environment is the pinned old behaviour", func(t *testing.T) {
		t.Setenv(config.AmbiguousWidthEnv, config.AmbiguousWidthNarrow)
		for _, r := range []rune{'─', '│', '═', '━', '▌', '—', '→', '①'} {
			if got, old := runeCells(r), ambiguousNarrowCells(r); got != old || got != wantNarrow {
				t.Errorf("runeCells(%q %#x) = %d cells with %s=narrow, want %d (= the old measurement %d)",
					r, r, got, config.AmbiguousWidthEnv, wantNarrow, old)
			}
		}
		if got, old := displayWidth(ambiguousPaste), ambiguousNarrowWidth(ambiguousPaste); got != old {
			t.Errorf("displayWidth(%q) = %d with %s=narrow, but the old implementation measured %d",
				ambiguousPaste, got, config.AmbiguousWidthEnv, old)
		}
	})

	t.Run("the W/F reading is unchanged in both", func(t *testing.T) {
		for _, mode := range []string{config.AmbiguousWidthWide, config.AmbiguousWidthNarrow} {
			t.Setenv(config.AmbiguousWidthEnv, mode)
			if got := runeCells('中'); got != 2 {
				t.Errorf("runeCells('中') = %d with %s=%s, want 2 (W is environment-independent)",
					got, config.AmbiguousWidthEnv, mode)
			}
			if got := runeCells('a'); got != 1 {
				t.Errorf("runeCells('a') = %d with %s=%s, want 1", got, config.AmbiguousWidthEnv, mode)
			}
		}
	})
}

// ambiguousNarrowWidth is the OLD displayWidth: the same walk, the old per-rune
// measure. Kept beside ambiguousNarrowCells so the pinned red side of the repro
// covers the STRING measure as well as the rune measure.
func ambiguousNarrowWidth(s string) int {
	w := 0
	for _, r := range s {
		w += ambiguousNarrowCells(r)
	}
	return w
}

// TestAmbiguousPasteWrapsTheRowsTheTerminalWouldShow is the LAYOUT half of the min
// repro — the half the operator actually sees. With the A runes measured one cell
// short, the rows gopi lays out are fewer than the terminal makes, so the cursor
// is put HIGHER than the text's real end: the reported symptom, and the reason it
// grows with the text (one row per filled row's worth of missed cells).
//
// The assertion is a RELATION between the two readings, not a number: measuring the
// same text with the same width must give the CJK reading more rows, and its cursor
// a lower row, because 40 ambiguous runes are 40 extra cells there.
func TestAmbiguousPasteWrapsTheRowsTheTerminalWouldShow(t *testing.T) {
	const width = 24
	const prompt = "you> "
	// The smallest sample that makes the ROW COUNT disagree: a run of the very
	// symbol the defect is about, long enough that one extra cell per rune is a
	// whole extra row. (The single pasted row above is the operator's text; this
	// is that text's mechanism, isolated.)
	const pasted = "││││││││││││││││││││││││││││││││││││││││"

	measure := func(mode string) (rows, row, col int) {
		t.Helper()
		//stylegate:ignore mode is the ONE value here that is not constant, and it is the input this helper measures BOTH readings with; the test ends after the second call, so t.Cleanup's scope and the per-call scope coincide — nothing flips per call to unwind
		t.Setenv(config.AmbiguousWidthEnv, mode) //stylegate:ignore the only value that is NOT constant (config.AmbiguousWidthEnv) is the mode this helper measures BOTH readings with, and the test ends after the second call — t.Cleanup's scope and the per-call scope coincide
		laid, r, c := layoutText(prompt, pasted, runeLen(pasted), width)
		return len(laid), r, c
	}
	narrowRows, narrowRow, _ := measure(config.AmbiguousWidthNarrow)
	wideRows, wideRow, wideCol := measure(config.AmbiguousWidthWide)

	if len([]rune(pasted)) != 40 {
		t.Fatalf("the fixture carries %d ambiguous runes, want 40", len([]rune(pasted)))
	}
	if wideRows <= narrowRows {
		t.Fatalf("the CJK reading laid out %d rows and the narrow reading %d — 40 ambiguous runes are 40 extra "+
			"cells there, so the wrap counts must differ (the defect was exactly that they did not)",
			wideRows, narrowRows)
	}
	if wideRow <= narrowRow {
		t.Errorf("cursor row: CJK reading %d, narrow reading %d — more cells must put the cursor LOWER, "+
			"which is the whole of the reported symptom", wideRow, narrowRow)
	}
	if wideRow >= wideRows {
		t.Errorf("cursor row %d outside the %d rows the CJK reading laid out", wideRow, wideRows)
	}
	if wideCol >= width {
		t.Errorf("cursor col %d is not a printable cell of a %d-cell row", wideCol, width)
	}
}

// TestAmbiguousGridEqualsTheTerminalsOwnCount is the render-truth half: the
// editor's own cell grid (internal/lineedit.Screen, fed the very bytes the editor
// draws) must agree cell-for-cell with the rune's measured width. This is the
// assertion that caught the reported symptom and it is the one that would fail if
// screenattr.go's second reader of isWideRune drifted from runeCells.
func TestAmbiguousGridEqualsTheTerminalsOwnCount(t *testing.T) {
	const row = "│ ─ │"
	for _, c := range []struct {
		mode string
		want int
	}{
		{config.AmbiguousWidthWide, 8},
		{config.AmbiguousWidthNarrow, 5},
	} {
		t.Setenv(config.AmbiguousWidthEnv, c.mode)
		cols := wideGridCols(t, row)
		if cols != c.want {
			t.Errorf("the cell grid gives %q %d columns with %s=%s, want %d",
				row, cols, config.AmbiguousWidthEnv, c.mode, c.want)
		}
		if got := displayWidth(row); got != cols {
			t.Errorf("displayWidth(%q) = %d but the cell grid drew %d columns with %s=%s — the measurer "+
				"and the screen disagree", row, got, cols, config.AmbiguousWidthEnv, c.mode)
		}
	}
}

// wideGridCols draws text on row 0 of a real cell grid and returns the column the
// grid's cursor ended on — the terminal-side count of the text. It is read from
// the grid, not from the measurer, so a drift between the two readers of
// isWideRune (runeCells above, screenattr.go's isWideRune) shows up here instead
// of cancelling out.
//
// The row is also asserted back: the text must be IN the grid, and the columns a
// wide rune claims must carry the continuation marker rather than a second rune.
func wideGridCols(t *testing.T, text string) int {
	t.Helper()
	sc := NewScreen(80, 2)
	sc.Feed([]byte(text))
	if got := sc.Line(0); got != text {
		t.Fatalf("the grid drew %q for %q", got, text)
	}
	for col := 1; col < sc.CurCol; col++ {
		if sc.Cell(0, col).Kind != CellWideContinuation {
			continue
		}
		if sc.Cell(0, col-1).Kind == CellWideContinuation {
			t.Fatalf("two continuation cells in a row at columns %d,%d of %q — a wide rune claims exactly two",
				col-1, col, text)
		}
	}
	return sc.CurCol
}

// runeLen is len(text) in RUNES — the index space layoutText's cursor argument
// lives in.
func runeLen(s string) int { return len([]rune(s)) }
