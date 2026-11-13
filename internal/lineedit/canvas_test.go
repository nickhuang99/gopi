package lineedit

// canvas_test.go — the CANVAS: a caller-declared drawing width that bounds the
// reserved layout (Editor.SetCanvasCols), and its behaviour across a resize.
//
// WHY THIS IS A LINEEDIT TEST AND NOT A CHAT ONE. The canvas is lineedit's: every
// width the layout installs at — the first install, every winch, and the answer
// renderer's SetCols — comes from the ONE canvasWidth(), and the resize path is
// reachable only through the editor's own read loop. Driving it here (a scripted
// source delivers a winch step carrying a new width) asserts the MECHANISM; the
// chat side asserts what it DECLARES (cmd/agent/chat_geometry_pty_test.go).

import (
	"testing"
	"time"
)

// The terminal the scripted source reports, and the canvas declared below it —
// wider than the canvas, so the cap is observable.
const (
	canvasTermCols  = 200
	canvasTermRows  = 30
	canvasIdealCols = 117
)

// canvasEditor builds the reserved layout at canvasTermCols/canvasTermRows with a
// caller-declared canvas. It returns the editor once the layout is installed.
func canvasEditor(t *testing.T, canvas int, steps []scriptStep) *Editor {
	t.Helper()
	clock := &fakeClock{}
	src := &scriptedSource{steps: steps, width: canvasTermCols, height: canvasTermRows, clock: clock}
	ed := NewChatEditor(src, &recordingWriter{}, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	ed.SetCanvasCols(canvas)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows at %d cols) = false — the case needs the layout installed",
			canvasTermRows, canvasTermCols)
	}
	return ed
}

// runToCompletion drives the editor's read loop until the script is exhausted —
// the same shape the resize tests use, so the winch reaches the layout through
// the editor's OWN propagation (a test that called Resize itself would prove only
// that the setter works). The deadline is not the assertion: it only turns a hang
// into a failure.
func runToCompletion(t *testing.T, ed *Editor) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := ed.Run(); err != nil {
			t.Errorf("editor run: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(a5Wait):
		cols, rows, ok := ed.LayoutGeometry()
		t.Fatalf("timed out waiting for the editor to finish (layout cols=%d rows=%d ok=%v)", cols, rows, ok)
	}
}

// TestCanvasCapsTheLayoutWidth: with a canvas declared, the layout installs at
// the canvas, not at the terminal's width.
func TestCanvasCapsTheLayoutWidth(t *testing.T) {
	ed := canvasEditor(t, canvasIdealCols, nil)
	cols, rows, ok := ed.LayoutGeometry()
	if !ok {
		t.Fatal("no layout geometry after EnableRegion")
	}
	if cols != canvasIdealCols {
		t.Fatalf("the layout installed at %d columns, want the canvas %d (the terminal reports %d)",
			cols, canvasIdealCols, canvasTermCols)
	}
	// ROWS ARE THE TERMINAL'S: the status row is anchored to the screen's LAST
	// row, so the canvas is a WIDTH and nothing else.
	if rows != canvasTermRows {
		t.Fatalf("the layout installed at %d rows, want the terminal's %d — the canvas must not claim rows",
			rows, canvasTermRows)
	}
}

// TestCanvasLargerThanTheTerminalIsTheTerminal: a canvas wider than the terminal
// cannot be drawn — the terminal would wrap every row — so the terminal wins.
func TestCanvasLargerThanTheTerminalIsTheTerminal(t *testing.T) {
	ed := canvasEditor(t, canvasTermCols*2, nil)
	cols, _, ok := ed.LayoutGeometry()
	if !ok {
		t.Fatal("no layout geometry after EnableRegion")
	}
	if cols != canvasTermCols {
		t.Fatalf("the layout installed at %d columns, want the terminal's %d — a canvas may never exceed the terminal",
			cols, canvasTermCols)
	}
}

// TestCanvasZeroMeansTheTerminal is the HISTORIC behaviour, pinned: the zero
// value must leave every existing caller byte-identical. A default that changed
// the layout width would move pixels in tests that never asked for a canvas.
// A negative value is the same as zero — "no canvas", never a refusal mid-run.
func TestCanvasZeroMeansTheTerminal(t *testing.T) {
	for _, canvas := range []int{0, -5} {
		ed := canvasEditor(t, canvas, nil)
		cols, _, ok := ed.LayoutGeometry()
		if !ok {
			t.Fatalf("no layout geometry with canvas %d", canvas)
		}
		if cols != canvasTermCols {
			t.Fatalf("canvas %d produced a %d-column layout, want the terminal's %d", canvas, cols, canvasTermCols)
		}
	}
}

// TestCanvasSurvivesAShrinkingResize: the canvas is a PROPERTY of the editor,
// not of the install. A winch that takes the terminal BELOW the canvas must
// re-install at the terminal's own width — a canvas wider than the terminal wraps
// every row, which is the one thing it may never do.
func TestCanvasSurvivesAShrinkingResize(t *testing.T) {
	const shrunk = 90
	ed := canvasEditor(t, canvasIdealCols, []scriptStep{{winch: true, width: shrunk}})
	runToCompletion(t, ed)
	cols, _, ok := ed.LayoutGeometry()
	if !ok {
		t.Fatal("no layout geometry after the resize")
	}
	if cols != shrunk {
		t.Fatalf("after the terminal shrank to %d columns the layout is %d — the canvas must never widen the terminal",
			shrunk, cols)
	}
}

// TestCanvasStillCapsAGrowingResize is the other direction: growing the terminal
// past the canvas must re-apply the cap (the winch path re-reads the SAME
// canvasWidth(), so a canvas that survived the shrink must still be in force).
func TestCanvasStillCapsAGrowingResize(t *testing.T) {
	const grown = 180
	ed := canvasEditor(t, canvasIdealCols, []scriptStep{{winch: true, width: grown}})
	runToCompletion(t, ed)
	cols, _, ok := ed.LayoutGeometry()
	if !ok {
		t.Fatal("no layout geometry after the resize")
	}
	if cols != canvasIdealCols {
		t.Fatalf("after the terminal grew to %d columns the layout is %d, want the canvas %d",
			grown, cols, canvasIdealCols)
	}
}
