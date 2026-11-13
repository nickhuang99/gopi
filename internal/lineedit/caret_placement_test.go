package lineedit

import (
	"strings"
	"testing"
	"time"
)

// caret_placement_test.go — THE TERMINAL CURSOR MUST END THE FRAME ON THE CARET.
//
// THE DEFECT (measured on a real pty, 2026-10-08, task 01a119d0). The bottom half
// of the chat layout is the editing line ("you> " + the buffer) and the chrome
// around it. Renderer.Redraw places the caret itself — but it is NOT the last
// writer of the frame: the region's band write (region.writeBandLine) ends every
// row by parking the cursor on the band's LIVE row, and it runs AFTER the renderer
// on BOTH roads the layout has:
//
//   - the repaint road (Editor.repaint → drainBandFrames), where the drain happens
//     after Redraw already placed the caret;
//   - the direct-driver road (Editor.SettleBand), where there is no repaint after
//     the drain at all.
//
// So a frame that carried a band row left the hardware block on the band row — one
// row ABOVE the input — and it stayed there until the next keystroke repainted.
// Startup is exactly that frame: the banner and the side panel are committed through
// the same band write, so the FIRST screen the operator sees had the block off the
// input, which reads as "the prompt is not focused yet".
//
// MEASURED, on the real binary on a real pty (100x30, no keystroke sent), by
// replaying the startup byte stream through a region-aware VT model:
//
//	before: cursor row 27 col 0   (the editing line "you>" is row 28)
//	after:  cursor row 28 col 5   (the caret cell, right after the "you> " prompt)
//
// WHAT THESE CASES PIN is the RELATION, never a row number: whatever the layout
// looks like, the cursor that ends the frame is on the row carrying the prompt and
// in the cell right after it. The judge is the RENDERED CELL GRID replayed from the
// editor's own paint stream (Rule 10.2) — a `true` return cannot stand in for a
// placement — and the editing line's row is FOUND in the grid rather than assumed,
// so a case cannot pass by hard-coding the height of the layout it ran on.

// caretRow finds the grid row carrying the editing line's prompt. The prompt is
// matched with its trailing spaces dropped: the grid model's Line() trims them (a
// trailing cell and a never-painted cell are identical), so a prefix test against
// "you> " would match nothing. It fails rather than returning -1 when no row carries
// it: a case that could not find the editing line would otherwise assert a relation
// against nothing.
func caretRow(t *testing.T, screen *Screen, cols, rows int) int {
	t.Helper()
	want := strings.TrimRight(promptForTest, " ")
	for row := 0; row < rows; row++ {
		if strings.HasPrefix(screen.Line(row), want) {
			return row
		}
	}
	t.Fatalf("no row of the rendered grid carries the editing line's prompt %q — the frame under test never drew the input, so a caret could not be measured on it\n--- grid ---\n%s",
		want, screen.Contents())
	return -1
}

// caretHarness replays the editor's paint stream into a grid model, the same way
// the pty fixtures read their screen back.
func caretGrid(out *recordingWriter, cols, rows int) *Screen {
	screen := NewScreen(cols, rows)
	for _, c := range out.snapshot() {
		screen.Feed(c)
	}
	return screen
}

// TestCaret_TheBandWriteIsNotTheLastWordOnTheCursor is the regression on the
// REPAINT road — the road startup takes: the editing line and the chrome are drawn,
// then a band row is committed (the banner, the side panel), and the cursor must end
// the frame on the caret.
//
// Pre-fix the band write's own placement was the last word, so the cursor sat on the
// row the band had drawn — above the input — which is the reported sighting.
func TestCaret_TheBandWriteIsNotTheLastWordOnTheCursor(t *testing.T) {
	ed, out, _ := noloopHarness(t)
	ed.repaint() // the layout's own frame: the editing line, the status row, the chrome
	if !ed.WriteBandRow(noloopRow) {
		t.Fatalf("WriteBandRow(%q) = false with the layout active\n--- paint stream ---\n%s", noloopRow, noloopPaint(out))
	}
	ed.repaint() // the drain that writes the band row happens inside this frame

	screen := caretGrid(out, noloopCols, noloopRows)
	row := caretRow(t, screen, noloopCols, noloopRows)
	if screen.CurRow != row {
		t.Fatalf("after the frame the cursor is on row %d, but the editing line is row %d (%q): the band write's own placement outlived the caret the renderer set — the block the operator saw ABOVE the input and read as an unfocused prompt\n--- grid ---\n%s",
			screen.CurRow, row, screen.Line(row), screen.Contents())
	}
	if want := len(promptForTest); screen.CurCol != want {
		t.Fatalf("the cursor is at column %d of the editing line, want %d (the caret cell, right after the %q prompt): the cursor is on the right ROW but not in the cell the user types into\n--- grid ---\n%s",
			screen.CurCol, want, promptForTest, screen.Contents())
	}
	// The fix MOVES the cursor; it must never cost the frame its content.
	if !strings.Contains(screen.Contents(), noloopRow) {
		t.Fatalf("the band row is gone from the grid: the caret placement replaced the frame it was meant to finish\n--- grid ---\n%s", screen.Contents())
	}
}

// TestCaret_TheSettleRoadAlsoEndsOnTheCaret: the direct-driver road (SettleBand) has
// NO repaint after its drain, so it is the road where a missing placement is
// permanent rather than corrected by the next frame. It is the road cmd/agent's pty
// fixtures drive.
func TestCaret_TheSettleRoadAlsoEndsOnTheCaret(t *testing.T) {
	ed, out, _ := noloopHarness(t)
	ed.repaint()
	if !ed.WriteBandRow(noloopBanner) {
		t.Fatalf("WriteBandRow(%q) = false with the layout active\n--- paint stream ---\n%s", noloopBanner, noloopPaint(out))
	}
	if !ed.SettleBand() {
		t.Fatalf("SettleBand() = false with the layout active: the frame this case measures was never committed\n--- paint stream ---\n%s", noloopPaint(out))
	}

	screen := caretGrid(out, noloopCols, noloopRows)
	row := caretRow(t, screen, noloopCols, noloopRows)
	if screen.CurRow != row || screen.CurCol != len(promptForTest) {
		t.Fatalf("after the settle the cursor is at row %d col %d, want row %d col %d (the caret cell on the editing line): the drain parked it on the band row and nothing after it puts it back\n--- grid ---\n%s",
			screen.CurRow, screen.CurCol, row, len(promptForTest), screen.Contents())
	}
	if !strings.Contains(screen.Contents(), noloopBanner) {
		t.Fatalf("the band row is gone from the grid: the caret placement replaced the frame it was meant to finish\n--- grid ---\n%s", screen.Contents())
	}
}

// TestCaret_TheHistoricRendererKeepsItsOwnBytes: the placement is a LAYOUT
// behaviour. On the historic full-screen renderer (no region installed) there is no
// absolute row to place to — that renderer owns its cursor with RELATIVE moves — so
// PlaceCaret must write nothing. This is the no-regression half: a fix that emitted
// a stray CUP there would move the block on a surface whose bytes were never broken.
func TestCaret_TheHistoricRendererKeepsItsOwnBytes(t *testing.T) {
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: noloopCols, height: noloopRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	ed := NewEditor(src, out, promptForTest, nil)
	ed.SetClock(clock.Now)
	t.Cleanup(func() {
		src.stopSource()
		ed.Close()
	})
	if ed.BandActive() {
		t.Fatal("the case needs the historic renderer, but a reserved layout is active")
	}
	before := len(out.snapshot())
	ed.render.PlaceCaret()
	if after := len(out.snapshot()); after != before {
		t.Fatalf("PlaceCaret wrote %d chunk(s) with no reserved layout installed: the historic path owns its cursor with relative moves, and a stray placement would move the block on a surface whose bytes were never broken\n--- paint stream ---\n%s",
			after-before, noloopPaint(out))
	}
}

// TestCaret_FollowsTheBufferAndNotAFixedOffset: the placement is the CARET's, so it
// must follow the text in the buffer — this is what keeps the cases above from being
// satisfied by parking the block at a constant cell.
func TestCaret_FollowsTheBufferAndNotAFixedOffset(t *testing.T) {
	ed, out, _ := noloopHarness(t)
	typed := "abc"
	ed.val.InsertString(typed)
	ed.repaint()
	ed.repaint()

	screen := caretGrid(out, noloopCols, noloopRows)
	row := caretRow(t, screen, noloopCols, noloopRows)
	if !strings.Contains(screen.Line(row), typed) {
		t.Fatalf("the editing line does not carry %q after typing: %q\n--- grid ---\n%s", typed, screen.Line(row), screen.Contents())
	}
	if screen.CurRow != row {
		t.Fatalf("the cursor left the editing line after typing %q: it is on row %d, and the line is row %d\n--- grid ---\n%s",
			typed, screen.CurRow, row, screen.Contents())
	}
	if want := len(promptForTest) + len(typed); screen.CurCol != want {
		t.Fatalf("the cursor is at column %d after typing %q, want %d — the block must follow the buffer, not sit at a fixed offset\n--- grid ---\n%s",
			screen.CurCol, typed, want, screen.Contents())
	}
}
