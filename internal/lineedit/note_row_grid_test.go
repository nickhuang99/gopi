package lineedit

// note_row_grid_test.go — the row-ORDER contract between the band's two queues.
//
// THE DEFECT THIS PINS (measured 2026-10-22 on a real 100×30 PTY, task
// 01a16298): drainBandFrames drains the band FRAMES first and the caller rows
// (notes) after, and those are two separate queues whose drain order is not the
// order the producers queued them in. `/info` published its block as notes; the
// REPL's next pass queued its inter-turn blank through WriteLines (a frame) a
// moment later; the blank was committed ABOVE the block. The block's text was
// identical to master's and its emphasis was right — only its START ROW moved
// (11 → 12), with every following grid row shifted by one.
//
// WHY THE EXISTING TESTS COULD NOT SEE IT: every one of them asserts the band's
// CONTENT (the rows' text, in order). A row that moved down one grid row keeps
// its text and its relative order, so content assertions read green while the
// screen is wrong. These tests assert the GRID — the replayed cell grid, row by
// row — which is the only observation that contains a row NUMBER.
//
// The seam under test is the production one: Editor.NoteRunsCopied publishes a
// block the way cmd/agent's `/info` does, and the follow-up frame is the REPL
// loop's own WriteLines("\n"). Both are called from THIS goroutine, which is a
// FOREIGN caller exactly as cmd/agent's REPL goroutine is (bandwait.go's file
// comment), so the wait they owe runs here for the same reason it runs there.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// noteOrderRows / noteOrderCols is the geometry these tests open at. It is TALLER
// than the shared harness on purpose: the block is bottom-anchored in the band,
// so a short terminal scrolls the block's own rows out of the grid and leaves
// nothing to compare. The real capture that found the defect used 30 rows.
const (
	noteOrderRows = 24
	noteOrderCols = 48
)

// noteOrderHarness is newStatusHarness at this file's own geometry — the same
// reserved layout, the same live read loop, only taller.
func newNoteOrderHarness(t *testing.T) *statusHarness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: noteOrderCols, height: noteOrderRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	ed := NewChatEditor(src, out, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", noteOrderRows)
	}
	if !ed.NotifyEnabled() {
		t.Fatal("the harness's source supplied no wake seam — a publish could never reach the read loop")
	}
	h := &statusHarness{ed: ed, out: out, src: src, screen: NewScreen(noteOrderCols, noteOrderRows), answer: ed.Answer(), done: make(chan struct{})}
	go func() {
		defer close(h.done)
		_, _ = ed.Run() //stylegate:ignore the harness's teardown: a real failure surfaces as a bounded waitContents timeout that dumps the screen
	}()
	t.Cleanup(func() {
		src.stopSource()
		select {
		case <-h.done:
		case <-time.After(statusDrawWait):
		}
		ed.Close()
	})
	return h
}

// noteOrderFiller is how many rows are committed BEFORE the block, so the block
// lands partway down the band rather than at its first row: a shift is only
// observable against a known starting row.
const noteOrderFiller = 2

// noteOrderBlock is the block the two paths commit — plain text lines, shaped
// like /info's own (a title, then facts), short enough for the harness's band.
var noteOrderBlock = []string{
	"session info:",
	"session  0123",
	"model  test-model",
}

// noteOrderGrid commits the filler plus the block through ONE of the two paths
// and returns the replayed grid, one row per line — the observation that
// carries row numbers.
func noteOrderGrid(t *testing.T, viaNote bool) string {
	t.Helper()
	h := newNoteOrderHarness(t)
	h.drainWrites()
	for i := 0; i < noteOrderFiller; i++ {
		if !h.ed.WriteLines(fmt.Sprintf("prior %d", i)) {
			t.Fatalf("filler WriteLines = false with the layout installed")
		}
	}
	if viaNote {
		runs := make([]NoteRun, 0, len(noteOrderBlock))
		for _, line := range noteOrderBlock {
			runs = append(runs, NoteRun{Text: line + "\n"})
		}
		if !h.ed.NoteRunsCopied(runs) {
			t.Fatalf("NoteRunsCopied = false with the layout installed")
		}
	} else if !h.ed.WriteLines(strings.Join(noteOrderBlock, "\n")) {
		t.Fatalf("block WriteLines = false with the layout installed")
	}
	// THE REPL'S OWN NEXT PASS, in BOTH branches: the loop's top writes one blank
	// line through the FRAME queue before it reads the next command. This is the
	// row that used to overtake the block, and doing it on one branch only would
	// make the two grids differ for a reason that is not the defect.
	if !h.ed.WriteLines("\n") {
		t.Fatalf("the loop's blank WriteLines = false with the layout installed")
	}
	h.waitContents(t, noteOrderBlock[len(noteOrderBlock)-1])
	h.replay()
	var b strings.Builder
	for row := 0; row < noteOrderRows; row++ {
		fmt.Fprintf(&b, "%2d|%s\n", row, h.screen.Line(row))
	}
	return b.String()
}

// blockStartRow is the grid row the block's title sits on — the number the
// screen actually shows.
func blockStartRow(grid string) int {
	for _, line := range strings.Split(grid, "\n") {
		bar := strings.IndexByte(line, '|')
		if bar < 0 {
			continue
		}
		if strings.HasPrefix(line[bar+1:], noteOrderBlock[0]) {
			row := 0
			if _, err := fmt.Sscanf(line[:bar], "%d", &row); err != nil {
				return -1
			}
			return row
		}
	}
	return -1
}

// TestNoteBlockStartsOnTheSameGridRowAsWriteLines is the regression: the same
// block reaches the SAME grid row whether it goes through the panel seam
// (WriteLines, master's path) or through the caller-row seam (NoteRunsCopied,
// the /info path), even when the REPL queues its inter-turn blank immediately
// afterwards.
//
// RED on the pre-fix shape: the note path's block opened one row lower (the
// blank frame overtook it), so the two grids differ at the block and every row
// below it.
func TestNoteBlockStartsOnTheSameGridRowAsWriteLines(t *testing.T) {
	viaLines := noteOrderGrid(t, false)
	viaNote := noteOrderGrid(t, true)

	linesRow := blockStartRow(viaLines)
	noteRow := blockStartRow(viaNote)
	if linesRow < 0 || noteRow < 0 {
		t.Fatalf("the block is not on the grid at all (WriteLines row %d, note row %d)\n--- WriteLines ---\n%s\n--- note ---\n%s",
			linesRow, noteRow, viaLines, viaNote)
	}
	if noteRow != linesRow {
		t.Fatalf("the block starts on grid row %d through the note path and %d through WriteLines — the caller rows must be committed BEFORE the frame queued after them (a row that moves down a grid row keeps its text, which is why content assertions never saw this)\n--- WriteLines ---\n%s\n--- note ---\n%s",
			noteRow, linesRow, viaLines, viaNote)
	}
	// …and the WHOLE grid agrees, not only the block's first row: a shift of any
	// later row is the same defect one line further down.
	if viaNote != viaLines {
		t.Fatalf("the two paths draw different grids\n--- WriteLines ---\n%s\n--- note ---\n%s", viaLines, viaNote)
	}
}
