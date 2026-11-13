package lineedit

// input_cursor_row_test.go — the CURSOR'S ROW is asserted against the rows the
// editing line is DRAWN on, over real input: a line that wraps, a line with an
// explicit newline, a line of wide (CJK) runes, and a line long enough that the
// window scrolls. The claim is the operator's own: "the cursor ends up one row
// above the input".
//
// WHY THE GRID AND NOT THE RENDERER'S OWN NUMBERS (Rule 10.2). What the operator
// sees is where the terminal put the cursor after the bytes were written, so the
// judge is the grid model's own CurRow/CurCol after replaying every painted byte
// — never `Renderer.cursorRows`, which is the value the renderer BELIEVES it
// wrote. A disagreement between the two is exactly the class of defect this case
// is about.

import (
	"strings"
	"testing"
	"time"
)

// cursorRowHarness is this file's own harness rather than the package's injected
// one (injectHarness), and the reason is a CEILING: that harness installs the
// package's 6-key test period, and this case needs one key per rune of a
// 120-rune line. ResetKeys cannot raise a period (deliberately — a bound the
// bounded party can lift is not a bound), so the harness is built here with a
// ceiling that fits the longest path.
type cursorRowHarness struct {
	ed     *Editor
	out    *recordingWriter
	screen *Screen
	src    *scriptedSource
	inj    *Injector
	exited chan struct{}
	fed    int
}

// cursorRowRunes is the ceiling this case declares: one key per rune of the
// longest path, with room to spare.
const cursorRowRunes = 256

func newCursorRowHarness(t *testing.T) *cursorRowHarness {
	t.Helper()
	src := &scriptedSource{width: cursorRowCols, height: cursorRowRows, holdAtEnd: true}
	inj := NewInjector(src, cursorRowRunes)
	out := &recordingWriter{}
	ed := NewChatEditor(inj, out, promptForTest, nil, nil, (&stubCompleter{}).complete)
	t.Cleanup(inj.Close)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", cursorRowRows)
	}
	h := &cursorRowHarness{
		ed: ed, out: out, screen: NewScreen(cursorRowCols, cursorRowRows),
		src: src, inj: inj, exited: make(chan struct{}),
	}
	go func() {
		defer close(h.exited)
		_, _ = ed.Run() //stylegate:ignore the harness closes the editor; Run's Result is another case's subject
	}()
	t.Cleanup(func() {
		src.stopSource()
		select {
		case <-h.exited:
		case <-time.After(cursorRowWait):
			t.Fatalf("Run did not return after the source was stopped\n--- screen ---\n%s", h.rows())
		}
		ed.Close()
	})
	return h
}

const (
	// cursorRowCols / cursorRowRows are the terminal this case runs the editor on.
	cursorRowCols = 40
	cursorRowRows = 12
	// cursorRowWait bounds every wait in this file (the screen, never the wait, is
	// the assertion).
	cursorRowWait = 3 * time.Second
	// cursorRowPoll is those waits' polling interval.
	cursorRowPoll = 2 * time.Millisecond
)

// replay feeds every recorded write the grid has not seen yet, in order.
func (h *cursorRowHarness) replay() {
	chunks := h.out.snapshot()
	for _, w := range chunks[h.fed:] {
		h.screen.Feed(w)
	}
	h.fed = len(chunks)
}

// rows is the painted grid as text.
func (h *cursorRowHarness) rows() string {
	h.replay()
	return h.screen.Contents()
}

// waitFor polls the painted grid until cond holds, dumping it on failure.
func (h *cursorRowHarness) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(cursorRowWait)
	for time.Now().Before(deadline) {
		h.replay()
		if cond() {
			return
		}
		time.Sleep(cursorRowPoll) //stylegate:allow-sleep bounded poll (cursorRowWait) for the read loop to paint the effect of an injected key — the assertion is the grid, not the wait
	}
	t.Fatalf("timed out waiting for %s\n--- screen ---\n%s", what, h.screen.Contents())
}

// inputCursorPaths are the four input shapes the operator reported: a line that
// nearly fills its row, an explicit newline, wide runes, and a line long enough
// to scroll the window.
var inputCursorPaths = []struct {
	name string
	text string
}{
	{"near-full-row", strings.Repeat("a", inputCursorNearFull)},
	{"explicit-newline", "first\nsecond"},
	{"cjk-wide", strings.Repeat("\u77e5", inputCursorCJKRunes)},
	{"long-scrolling", strings.Repeat("b", inputCursorLongRunes)},
}

const (
	// inputCursorNearFull fills the editing row and spills one cell past it:
	// cursorRowCols (40) minus the prompt ("you> " = 5 cells) is 35, so 35 runes
	// end exactly at the row's last cell — the geometry where a pending-wrap
	// misfiling would put the cursor one row off — and 36 is the first rune that
	// must wrap. The +1 is the point: a line that only exactly fills its row never
	// reaches the wrapped shape this path exists to test (measured — the first
	// version of this case asserted "it wrapped" on 35 runes and did not).
	inputCursorNearFull = cursorRowCols - len(promptForTest) + 1
	// inputCursorCJKRunes is 20 wide runes = 40 cells, well past the prompt's 5
	// cells, so the wide runes themselves force the wrap.
	inputCursorCJKRunes = 20
	// inputCursorLongRunes outruns the editing line's whole row budget several
	// times over, so the window scrolls and a marker row appears.
	inputCursorLongRunes = 120
)

// typeInput injects the line and waits until the editor's VALUE carries it, then
// until the PAINTED value catches up.
//
// IT IS CHUNKED, and the chunk size is the GATE'S, not this case's convenience:
// InjectBytesBounded bounds ONE read unit at injectReadBytes (64), so a 120-rune
// line must go in as two units. Sending it as one call is refused — measured, the
// first version of this case did exactly that and the seam's own boundary caught
// it, which is the entry point behaving as designed rather than a case bug.
// InjectBytesBounded is used rather than a call per rune because the seam's
// pacing contract (injectKeyInterval) is a floor between two consecutive units:
// a per-rune loop is tens of seconds of wall clock, and the shape/keysource cases
// already exercise the per-key door.
func typeInput(t *testing.T, h *cursorRowHarness, path, text string) {
	t.Helper()
	for i := 0; i < len(text); i += injectReadBytes {
		end := i + injectReadBytes
		if end > len(text) {
			end = len(text)
		}
		if res := h.inj.InjectBytesBounded(text[i:end], cursorRowRunes); !res.Accepted {
			t.Fatalf("%s: injecting bytes [%d:%d] refused: %s", path, i, end, res.Why)
		}
	}
	h.waitFor(t, "the editor to take every injected rune", func() bool {
		return len([]rune(h.ed.Value().Text())) >= len([]rune(text))
	})
	// And the PAINT must have caught up: one drain return does not mean the
	// renderer has drawn the whole line, and every assertion below reads the grid.
	h.waitFor(t, "the editing line to be painted", func() bool {
		return h.ed.render.DrawnRows() >= 1
	})
}

// assertCursorRowIsOnTheInput is the invariant: the painted cursor sits on a row
// the editing line DRAWS — never above them, never on the band or the status row.
func assertCursorRowIsOnTheInput(t *testing.T, h *cursorRowHarness, path string) {
	t.Helper()
	h.replay()
	top := h.ed.region.EditorTop()
	drawn := h.ed.render.DrawnRows()
	if drawn <= 0 {
		t.Fatalf("%s: the editing line drew no rows", path)
	}
	if row := h.screen.CurRow; row < top || row >= top+drawn {
		t.Errorf("%s: the painted cursor is on grid row %d, outside the editing line's drawn rows [%d,%d) — the cursor is not on the input\n--- screen ---\n%s",
			path, row, top, top+drawn, h.screen.Contents())
	}
}

// TestInputCursorStaysOnTheEditingLine drives real injected input down the four
// reported paths and reads the cursor's row off the painted grid.
//
// EACH PATH ASSERTS ITS OWN PREMISE FIRST: the editing line must have REACHED the
// shape the path is about (it wrapped, it carries a newline, it scrolled).
// Without that, a path whose injection silently did nothing would pass by drawing
// a one-row line and putting the cursor on it — a green case that measured
// nothing (Rule 31).
func TestInputCursorStaysOnTheEditingLine(t *testing.T) {
	for _, path := range inputCursorPaths {
		t.Run(path.name, func(t *testing.T) {
			h := newCursorRowHarness(t)
			typeInput(t, h, path.name, path.text)
			h.replay()

			if got, want := h.ed.Value().Text(), path.text; got != want {
				t.Fatalf("%s: the editor holds %q, want %q — the injection did not deliver the path's own text, so nothing below would be measuring this path",
					path.name, got, want)
			}
			// THE PREMISE: more than one row of editing line means the line really
			// wrapped; without it the case is asserting the trivial one-row case.
			if drawn := h.ed.render.DrawnRows(); drawn < 2 {
				t.Fatalf("%s: the editing line drew %d row(s) for %d runes — the path never reached the wrapped shape it exists to test",
					path.name, drawn, len([]rune(path.text)))
			}
			assertCursorRowIsOnTheInput(t, h, path.name)
		})
	}
}

// TestInputCursorRowMatchesTheTextRow is the DIRECT comparison the operator's
// report asks for: the input's text occupies rows, and the cursor must be on one
// of them — a cursor sitting on the row ABOVE the text at the end of a wrapped
// line is the reported symptom, and it is what this case fails on.
func TestInputCursorRowMatchesTheTextRow(t *testing.T) {
	for _, path := range inputCursorPaths {
		t.Run(path.name, func(t *testing.T) {
			h := newCursorRowHarness(t)
			typeInput(t, h, path.name, path.text)
			assertCursorRowIsOnTheInput(t, h, path.name)

			h.replay()
			top := h.ed.region.EditorTop()
			drawn := h.ed.render.DrawnRows()
			row := h.screen.CurRow
			cursorLine := strings.TrimRight(h.screen.Line(row), " ")

			// The LAST text row of the editing line, read off the GRID (never off
			// the renderer): the row range the input occupies is [top, top+drawn),
			// and the text's own end is the last row in it that carries text.
			lastTextRow := -1
			for r := top; r < top+drawn; r++ {
				if strings.TrimRight(h.screen.Line(r), " ") != "" {
					lastTextRow = r
				}
			}
			if lastTextRow < 0 {
				t.Fatalf("%s: no editing row carries text\n--- screen ---\n%s", path.name, h.screen.Contents())
			}
			// The cursor's OWN row must carry some of the input: a blank cursor row
			// under a text row is the "cursor one row above the input" reading.
			textBefore := false
			for r := top; r < row; r++ {
				if strings.TrimRight(h.screen.Line(r), " ") != "" {
					textBefore = true
				}
			}
			if cursorLine == "" && textBefore {
				t.Errorf("%s: the cursor sits on blank grid row %d while the editing line's text is on rows above it — the cursor is off the input\nrows [%d,%d):\n%s",
					path.name, row, top, top+drawn, h.screen.Contents())
			}
			if row > lastTextRow {
				t.Errorf("%s: the cursor is on row %d, BELOW the editing line's last text row %d\n--- screen ---\n%s",
					path.name, row, lastTextRow, h.screen.Contents())
			}
		})
	}
}
