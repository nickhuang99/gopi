package lineedit

import "testing"

// canvas_grow_test.go — the canvas across a resize that makes the terminal
// WIDER than it started.
//
// WHY THIS CASE EXISTS. The canvas is a CAP, not a frozen width: the layout is
// installed at min(canvas, the terminal's width NOW), and "now" is re-measured
// on every winch (editor.go's EventWinch arm calls canvasWidth() again). A
// caller that resolved its cap ONCE against the terminal it happened to start
// on turns that cap into a constant of the run — start at 63 columns, drag the
// window to 110, and the layout keeps drawing 63 columns while the right half of
// the screen stays permanently empty. The rows followed because they always come
// from the terminal; the width did not, because the freeze was in the VALUE the
// caller handed over, not in the mechanism.
//
// So the two halves of the fix are tested apart, on purpose:
//   - HERE: given a canvas that is the caller's IDEAL (wider than the terminal
//     it starts on), a widening winch must let the layout follow the terminal up
//     to the ideal, and a shrinking winch must still cap at the terminal.
//   - cmd/agent/chat_geometry_test.go: the SURFACE must hand over the ideal, not
//     min(ideal, the columns it measured at startup) — that is the frozen value.
//
// The canvas here is a LITERAL wider than the starting terminal, declared by
// this case, so a green cannot come from reading back whatever a resolver
// happened to answer.

const (
	// canvasGrowStartCols is the terminal the surface starts on: narrow, well
	// below the ideal, so a frozen cap is observable as "stuck at this number".
	canvasGrowStartCols = 63
	// canvasGrowWideCols is the width the operator drags the window to: wider
	// than the start, still below the ideal, so the layout must land exactly on
	// it (a cap stuck at canvasGrowStartCols, or a cap applied at the ideal,
	// both fail this).
	canvasGrowWideCols = 110
	// canvasGrowIdealCols is the canvas the caller declares — the DESIGNED
	// width, not the width the terminal happened to have at startup.
	canvasGrowIdealCols = 117
	// canvasGrowPastIdealCols is wider than the ideal: the cap must bite, so the
	// "it just follows the terminal" failure cannot pass either.
	canvasGrowPastIdealCols = 200
	canvasGrowRows          = 24
)

// canvasGrowEditor installs the reserved layout at startCols/rows with the
// declared canvas and runs the scripted steps through the editor's OWN read loop
// (the same propagation the production winch takes).
func canvasGrowEditor(t *testing.T, startCols, canvas int, steps []scriptStep) *Editor {
	t.Helper()
	clock := &fakeClock{}
	src := &scriptedSource{steps: steps, width: startCols, height: canvasGrowRows, clock: clock}
	ed := NewChatEditor(src, &recordingWriter{}, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	ed.SetCanvasCols(canvas)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows at %d cols) = false — the case needs the layout installed",
			canvasGrowRows, startCols)
	}
	return ed
}

// TestCanvasIdealLetsAWideningWinchThrough is the regression case the defect
// was reported with: start narrow, grow the terminal, and the layout follows —
// bounded by the ideal, not by the width the run started at.
func TestCanvasIdealLetsAWideningWinchThrough(t *testing.T) {
	ed := canvasGrowEditor(t, canvasGrowStartCols, canvasGrowIdealCols,
		[]scriptStep{{winch: true, width: canvasGrowWideCols}})
	// The start is the terminal's own width: min(ideal, 63) = 63.
	if cols, rows, ok := ed.LayoutGeometry(); !ok || cols != canvasGrowStartCols || rows != canvasGrowRows {
		t.Fatalf("before the resize the layout is %dx%d (ok=%v), want %dx%d — the ideal canvas must not "+
			"widen a narrow terminal", cols, rows, ok, canvasGrowStartCols, canvasGrowRows)
	}
	runToCompletion(t, ed)
	cols, rows, ok := ed.LayoutGeometry()
	if !ok {
		t.Fatal("no layout geometry after the widening resize")
	}
	if cols != canvasGrowWideCols {
		t.Fatalf("after the terminal grew from %d to %d columns the layout is %d — a canvas equal to the "+
			"IDEAL must re-clamp on every winch, so the layout follows the terminal up to it",
			canvasGrowStartCols, canvasGrowWideCols, cols)
	}
	if rows != canvasGrowRows {
		t.Fatalf("after the resize the layout has %d rows, want the terminal's %d — rows always come from "+
			"the terminal", rows, canvasGrowRows)
	}
}

// TestCanvasIdealStillCapsPastTheIdeal is the other bound: a terminal dragged
// wider than the designed width keeps the cap. Without this, "it follows the
// terminal" would pass the case above while silently un-capping the chrome.
func TestCanvasIdealStillCapsPastTheIdeal(t *testing.T) {
	ed := canvasGrowEditor(t, canvasGrowStartCols, canvasGrowIdealCols,
		[]scriptStep{{winch: true, width: canvasGrowPastIdealCols}})
	runToCompletion(t, ed)
	cols, _, ok := ed.LayoutGeometry()
	if !ok {
		t.Fatal("no layout geometry after the widening resize")
	}
	if cols != canvasGrowIdealCols {
		t.Fatalf("on a %d-column terminal the layout is %d columns, want the ideal %d — the canvas is still "+
			"a cap", canvasGrowPastIdealCols, cols, canvasGrowIdealCols)
	}
}

// TestCanvasIdealStillCapsANarrowTerminal is the control the epic asked for: an
// ideal canvas on a terminal NARROWER than the ideal must resolve to the
// terminal, on install and after a resize — a row composed for 117 cells on an
// 80-cell screen overflows it, which is why SetCanvasCols may never be dropped
// to "no cap" as a fix for the frozen one.
func TestCanvasIdealStillCapsANarrowTerminal(t *testing.T) {
	const narrow = 80
	ed := canvasGrowEditor(t, canvasGrowStartCols, canvasGrowIdealCols,
		[]scriptStep{{winch: true, width: narrow}})
	if cols, _, ok := ed.LayoutGeometry(); !ok || cols != canvasGrowStartCols {
		t.Fatalf("on install the layout is %d columns (ok=%v), want the terminal's %d", cols, ok, canvasGrowStartCols)
	}
	runToCompletion(t, ed)
	cols, _, ok := ed.LayoutGeometry()
	if !ok {
		t.Fatal("no layout geometry after the resize")
	}
	if cols != narrow {
		t.Fatalf("after the terminal settled at %d columns the layout is %d — the ideal canvas must never "+
			"push rows past the terminal's own width", narrow, cols)
	}
}
