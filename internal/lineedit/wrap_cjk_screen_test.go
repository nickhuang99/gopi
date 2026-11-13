package lineedit

// wrap_cjk_screen_test.go — the wrap policy observed on a RENDERED GRID, not on
// the row slice.
//
// WHY A SECOND FILE FOR THE SAME POLICY. wrap_cjk_token_test.go asserts what
// wrapCols RETURNS; this file asserts what a terminal DOES with it. The rows are
// fed, one at a time, into the package's own vt100 model (Screen — the same
// reader `gopi ui_state` and the replay path use), and the grid is read back.
// Two defects live only on that side of the boundary, and neither is visible in
// the row slice:
//
//   - a row WIDER than the terminal is re-wrapped BY THE TERMINAL, at a cell the
//     policy never chose — the halved token comes back even though wrapCols'
//     rows were all in budget;
//   - a row that ends inside a token is only a defect once it is a ROW on a
//     surface the operator copies from, which is what the grid is.
//
// THE ASSERTION is the deliverable's own: on the settled grid, no ASCII token of
// the answer is split across two rows — except a token WIDER than the terminal,
// which has nowhere else to go (the documented fallback).

import (
	"fmt"
	"strings"
	"testing"
)

const (
	// wrapScreenRows is the terminal height the wrapped answer is rendered into:
	// tall enough that a fold never scrolls.
	wrapScreenRows = 40
)

// wrapScreenColsSwept are the widths the answer is rendered at. They are the
// terminals an answer band is actually laid out at, and they are SWEPT rather
// than pinned because the defect is a function of where a token happens to fall:
// at 33 cells the pre-fix grid came out right by luck, while at 12 and 16 it
// halved `regenerable` across two rows. A single width would have been a green
// that measured nothing.
var wrapScreenColsSwept = []int{12, 16, 20, 27, 33, 40, 52}

// wrapScreenAnswer is the operator's own shape: CJK prose with Latin/digit tokens
// written inside it, once spaced and once glued to the surrounding characters.
const wrapScreenAnswer = "磁盘占用 319 KB，这是 regenerable artefact 的大小；" +
	"实测cmd/agent/cli_tasks.go的319 KB与regenerable artefact。" +
	"缓存目录（319 KB）需要重建，这是一个 regenerable artefact。"

// wrapScreenRender lays rows onto a real grid: each row is written the way a
// terminal renderer writes it — the row's bytes, then CR LF — so a row wider than
// the screen is wrapped BY THE SCREEN, exactly as a too-wide row would be.
func wrapScreenRender(rows []string, cols int) *Screen {
	screen := NewScreen(cols, wrapScreenRows)
	for _, row := range rows {
		screen.Feed([]byte(row + "\r\n"))
	}
	return screen
}

// wrapScreenLines is the grid as text: every non-empty row, trimmed.
func wrapScreenLines(screen *Screen) []string {
	var out []string
	for i := 0; i < screen.Rows; i++ {
		if line := strings.TrimRight(screen.Line(i), " "); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// wrapScreenTokens is every ASCII token of the answer — the ones the policy
// exists to keep copyable. A token carrying a wide rune is skipped: CJK text
// legitimately breaks between its characters.
func wrapScreenTokens(answer string) []string {
	var out []string
	for _, field := range strings.Fields(answer) {
		ascii, wide := true, 0
		for _, r := range field {
			if r > 0x7f {
				ascii = false
			}
			if isWideRune(r) {
				wide++
			}
		}
		if ascii && len([]rune(field)) > 1 {
			out = append(out, field)
		}
		_ = wide
	}
	return out
}

// TestWrapScreen_CJKAnswerNeverHalvesAToken is the render-level half of the
// regression: wrap the answer, render it, read the grid, and require that no
// copyable token is split across two rows and that no cell was lost, added or
// reordered on the way to the screen (whitespace excepted — a wrap consumes the
// space it breaks at, and the grid trims its rows' trailing blanks).
func TestWrapScreen_CJKAnswerNeverHalvesAToken(t *testing.T) {
	for _, cols := range wrapScreenColsSwept {
		t.Run("", func(t *testing.T) { wrapScreenSweep(t, cols) })
	}
}

// wrapScreenSweep is one width of the sweep.
func wrapScreenSweep(t *testing.T, cols int) {
	t.Helper()
	rows := wrapCols(wrapScreenAnswer, cols)
	for i, row := range rows {
		if w := displayWidth(row); w > cols {
			t.Fatalf("%d cells: row %d is %d cells, wider than the terminal: %q (rows: %q)",
				cols, i, w, row, rows)
		}
	}
	screen := wrapScreenRender(rows, cols)
	lines := wrapScreenLines(screen)
	if len(lines) < 2 {
		t.Fatalf("%d cells: the answer rendered onto %d row(s); it is wider than one row, so this is not a wrap at all: %q",
			cols, len(lines), lines)
	}

	for _, tok := range wrapScreenTokens(wrapScreenAnswer) {
		runes := []rune(tok)
		if displayWidth(tok) > cols {
			continue // wider than a whole row: the fallback, named above
		}
		for p := 1; p < len(runes); p++ {
			head, tail := string(runes[:p]), string(runes[p:])
			for i := 0; i+1 < len(lines); i++ {
				if strings.HasSuffix(lines[i], head) && strings.HasPrefix(lines[i+1], tail) {
					t.Fatalf("%d cells: token %q is halved on the grid: row %d ends %q, row %d starts %q\n--- grid ---\n%s",
						cols, tok, i, lines[i], i+1, lines[i+1], wrapScreenGrid(lines))
				}
			}
		}
	}

	got := strings.ReplaceAll(strings.Join(lines, ""), " ", "")
	want := strings.ReplaceAll(wrapScreenAnswer, " ", "")
	if got != want {
		t.Fatalf("%d cells: the grid carries %d cells, the answer %d\n--- grid ---\n%s\ngot:  %q\nwant: %q",
			cols, len(got), len(want), wrapScreenGrid(lines), got, want)
	}
}

// wrapScreenGrid renders the reading for a failure message.
func wrapScreenGrid(lines []string) string {
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%3d|%s\n", i, line)
	}
	return b.String()
}
