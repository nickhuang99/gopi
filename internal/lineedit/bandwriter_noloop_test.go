package lineedit

import (
	"strings"
	"testing"
	"time"
)

// bandwriter_noloop_test.go — the layout's DIRECT drivers: a caller that installs
// the layout (EnableRegion) and writes band rows with NO read loop ever running.
//
// THE DEFECT (measured 2026-09-28 in cmd/agent, task 01a0e965). b3687371 routed
// WriteBandRow through the read loop's queue (`queueBand` → `waitBandWrite`, bounded
// by `bandWaitBudget` = 4000 × 1ms = 4s), and 363aa34e gave WriteLines the arm for
// the state with no drainer in sight (`loopGoid == 0` → return at once) — but NOT
// WriteBandRow, its sibling on the same seam. So a caller that never runs a loop
// spent the whole bound and then read the false as "no reserved layout is active" —
// a wrong message twice over: no loop was stalled (none existed) and the layout WAS
// active (EnableRegion returned true). Measured: 10 cmd/agent cases, ~4s each
// (`TestUIStateAudit_*`, `TestChatDump_GridFollowsTheLayoutGeometryOnARealPty`).
//
// THE CONTRACT THESE CASES PIN has two halves, and the second is why the fix is a
// SEAM rather than a branch inside WriteBandRow:
//
//  1. A direct driver gets the row ON THE GRID (Editor.SettleBand), because it reads
//     the grid back on its own goroutine and has no next wake at all — for it "late"
//     is "never". The assertions below are on the RENDERED CELL GRID replayed from
//     the editor's own paint stream (Rule 10.2), never on a return value alone: a
//     `true` that left the grid empty is the pre-fix failure read from the other
//     side.
//
//  2. WriteBandRow itself must NOT spend the bound when no loop is running — that is
//     the half that made the call take 4s. It cannot DRAW there instead, because
//     "the loop has not been scheduled yet" and "no loop will ever come" are the
//     same editor state at the moment of the write; the measured cost of drawing in
//     that window is a row the loop's first repaint paints over (cmd/agent's
//     TestChatStream_RepaintsStayLinearInTheAnswer lost a line: 39 of 40 committed).
//     So it queues and returns the historic true, and the caller that KNOWS it owns
//     the screen settles.

const (
	// noloopCols / noloopRows: a band wide enough that the row arrives on ONE row,
	// so a wrap cannot explain a missing cell.
	noloopCols = 60
	noloopRows = 12
	// noloopRow is the row the direct driver writes; noloopBanner is a second row,
	// so the band's own row loop is exercised rather than a single lucky write.
	noloopRow    = "direct-driver-row-marker"
	noloopBanner = "second row on the same screen"
	// noloopSteps bounds the observation windows — a synchronization wait, never a
	// measured duration. It is far below bandWaitBudget (4s), so a call that waited
	// cannot be inside it.
	noloopSteps = 200
	noloopStep  = 5 * time.Millisecond
)

// noloopHarness builds a chat editor with the layout installed and NO read loop: the
// exact shape the pty fixtures drive (real editor, EnableRegion, then a write). A
// wake seam IS present (the scripted source supplies one), which is the state that
// mattered — with a seam installed the pre-fix code took the waiting road and spent
// the whole bound.
func noloopHarness(t *testing.T) (*Editor, *recordingWriter, *Screen) {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: noloopCols, height: noloopRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	ed := NewChatEditor(src, out, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%dx%d) = false, want true (the layout's floor is %d rows)",
			noloopCols, noloopRows, RowsMinUsable())
	}
	t.Cleanup(func() {
		src.stopSource()
		ed.Close()
	})
	return ed, out, NewScreen(noloopCols, noloopRows)
}

// noloopGrid replays the editor's own paint stream through the screen model: the
// rendered cell grid is the unit of truth, and the paint stream is what the terminal
// received.
func noloopGrid(out *recordingWriter, screen *Screen) string {
	for _, c := range out.snapshot() {
		screen.Feed(c)
	}
	return screen.Contents()
}

// TestBandWriter_NoReadLoopWriteDoesNotSpendTheBound is HALF ONE of the regression:
// with no read loop the call must return at once. This is what made the pty fixtures
// red — 4s per call and then a false their caller read as a missing layout.
//
// The window is a COARSE floor, not a measured duration (Rule 27): the pre-fix call
// spends a whole 4s, and the assertion only has to be smaller than that. Its real
// subject is the bound not being spent at all.
func TestBandWriter_NoReadLoopWriteDoesNotSpendTheBound(t *testing.T) {
	ed, out, _ := noloopHarness(t)

	if ed.onLoopGoroutine() {
		t.Fatal("the harness is running a read loop — this case is about the shape that is not")
	}
	if !ed.BandActive() {
		t.Fatal("BandActive() = false after EnableRegion: the layout the case needs was never installed")
	}

	start := time.Now()
	ok := ed.WriteBandRow(noloopRow)
	waited := time.Since(start)
	if waited > noloopStep*time.Duration(noloopSteps) {
		t.Fatalf("WriteBandRow spent %s with no read loop to drain the queue: the caller waited on a drainer that does not exist (pre-fix this was the whole bandWaitBudget = %s, ending in a false the caller reads as 'no reserved layout is active')\n--- paint stream ---\n%s",
			waited, bandWaitBudget, noloopPaint(out))
	}
	// The return keeps its meaning: the layout owns the screen, so a caller keeps
	// its historic road (the row is drawn by the settle below, or by the loop).
	if !ok {
		t.Fatalf("WriteBandRow = false with the layout active: the caller then prints the historic flow bytes over a screen the layout DOES own\n--- paint stream ---\n%s", noloopPaint(out))
	}
	// And it QUEUED the frame rather than dropping it: the row the settle is about
	// to write has to be there.
	if !ed.bandPending() {
		t.Fatalf("no band frame is pending after the call: the row was neither drawn nor queued, so it is gone\n--- paint stream ---\n%s", noloopPaint(out))
	}
}

// TestBandWriter_TheDirectDriverSettlesTheRowsOntoTheGrid is HALF TWO: the direct
// driver's settle puts the QUEUED rows on the rendered grid, in order, without a read
// loop. This is the half the pty fixtures actually assert — they read the grid back
// on their own goroutine, so a queued-but-undrawn row is an empty grid for them.
func TestBandWriter_TheDirectDriverSettlesTheRowsOntoTheGrid(t *testing.T) {
	ed, out, screen := noloopHarness(t)

	for _, row := range []string{noloopRow, noloopBanner} {
		if !ed.WriteBandRow(row) {
			t.Fatalf("WriteBandRow(%q) = false with the layout active and no read loop\n--- paint stream ---\n%s", row, noloopPaint(out))
		}
	}
	if !ed.SettleBand() {
		t.Fatalf("SettleBand() = false with the layout active: the caller would announce a screen no layout owns\n--- paint stream ---\n%s", noloopPaint(out))
	}
	grid := noloopGrid(out, screen)
	for _, want := range []string{noloopRow, noloopBanner} {
		if !strings.Contains(grid, want) {
			t.Fatalf("the grid does not carry %q after the settle: the rows the band queued were never written, which is the empty grid the pty fixtures audit\n--- grid ---\n%s",
				want, grid)
		}
	}
	// The level is DOWN after the settle: nothing is left standing for a read loop
	// this editor does not have — a leftover frame would be drawn against whatever
	// screen came next.
	if ed.bandPending() {
		t.Fatal("a band frame is still PENDING after SettleBand returned: the row was left as a level for a loop that does not exist")
	}
}

// TestBandWriter_TheDirectDriverKeepsTheHistoricFalseWhenTheLayoutIsGone: the return
// value keeps its meaning on the settle road too. With the layout taken down the call
// must answer false — the caller then writes the historic flow bytes — never a true
// that announces a screen no layout owns.
func TestBandWriter_TheDirectDriverKeepsTheHistoricFalseWhenTheLayoutIsGone(t *testing.T) {
	ed, out, _ := noloopHarness(t)

	ed.DisableRegion()
	if ed.BandActive() {
		t.Fatal("DisableRegion left the layout active — the case cannot measure the taken-down shape")
	}
	if ok := ed.WriteBandRow(noloopRow); ok {
		t.Fatalf("WriteBandRow = true after the layout was taken down: the caller would announce a screen no layout owns\n--- paint stream ---\n%s", noloopPaint(out))
	}
	if ed.SettleBand() {
		t.Fatal("SettleBand = true after the layout was taken down — the layout does not own the screen")
	}
	if strings.Contains(noloopGrid(out, NewScreen(noloopCols, noloopRows)), noloopRow) {
		t.Fatal("the row was drawn on a layout that was taken down")
	}
}

// noloopPaint renders the recorded paint stream as text for a failure message.
func noloopPaint(out *recordingWriter) string {
	var b strings.Builder
	for i, w := range out.snapshot() {
		b.WriteString(itoa(i))
		b.WriteString(": ")
		b.WriteString(strings.ReplaceAll(string(w), "\x1b", "<ESC>"))
		b.WriteString("\n")
	}
	return b.String()
}
