package replayplay

// geometry_terminal_test.go — A WORKER RUN'S REPLAY WRAPS AT THE TERMINAL THE
// OPERATOR IS ON, and only a run with no terminal at all falls back.
//
// THE SECOND HALF OF THE DEFECT (measured 2026-10-28, task 01a18269). A chat
// surface's dump records the grid it painted, so its replay is cell-exact on a
// geometry the recording owns. A WORKER run records no screen at all: its
// session log carries long prose, paths and command output, and the loader
// wrapped every one of them at a hard FallbackCols (80) no matter what terminal
// the operator was reading on. Measured in a real pty before the fix:
//
//	$ script -qec "stty cols 160 rows 40; gopi session replay 93043087… --turns"
//	session 93043087…  geometry=80x24 (fallback)   ← the operator was on 160
//
// The fix gives that session the terminal's own measurement
// (terminalgeom.Standard — the tree's ONE probe), and it ANNOUNCES which of the
// three channels it used, so a wrapped replay never passes for recorded
// geometry (Rule 15).
//
// WHY THE MEASUREMENT IS INJECTED HERE: under `go test` this process's standard
// descriptors are pipes, so a test that asserted on the PRODUCTION probe would
// be asserting on whatever the test runner happens to have. The loader takes
// the probe as a parameter (loadSessionAtMeasured) precisely so this arm drives
// the SAME decision with a terminal the test owns — and the production entry
// point passes terminalgeom.Standard unchanged.

import (
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// TestTimelineOnlySessionWrapsAtTheMeasuredTerminal is the arm the operator's
// 160-column run failed: with a window measured, a session that has no dump is
// laid out for THAT window and says "terminal", not "fallback".
func TestTimelineOnlySessionWrapsAtTheMeasuredTerminal(t *testing.T) {
	dir := t.TempDir()
	writeTimeline(t, dir, 1)
	const cols, rows = 160, 40
	mat, err := loadSessionAtMeasured(dir, "", sessionevent.MeasuredTerminalGeometry(cols, rows))
	if err != nil {
		t.Fatalf("loadSessionAtMeasured: %v", err)
	}
	if mat.Cols != cols || mat.Rows != rows {
		t.Fatalf("geometry is %dx%d, want the measured %dx%d — a worker run's log must wrap at the terminal the operator is on",
			mat.Cols, mat.Rows, cols, rows)
	}
	if mat.GeometrySource != GeometryFromTerminal {
		t.Fatalf("geometry source is %q, want %q: the header must say the width was MEASURED, not recorded",
			mat.GeometrySource, GeometryFromTerminal)
	}
}

// TestUnmeasurableTerminalStillFallsBack is the other side of the same
// decision, and it is what keeps this fix from inventing a width: with nothing
// measured (a pipe, a redirect, CI) the documented default stands and the
// header says "fallback".
func TestUnmeasurableTerminalStillFallsBack(t *testing.T) {
	dir := t.TempDir()
	writeTimeline(t, dir, 1)
	mat, err := loadSessionAtMeasured(dir, "",
		sessionevent.UnknownTerminalGeometry(sessionevent.GeometryReasonNoTerminal))
	if err != nil {
		t.Fatalf("loadSessionAtMeasured: %v", err)
	}
	if mat.Cols != FallbackCols || mat.Rows != FallbackRows {
		t.Fatalf("geometry is %dx%d, want the documented fallback %dx%d", mat.Cols, mat.Rows, FallbackCols, FallbackRows)
	}
	if mat.GeometrySource != GeometryFromFallback {
		t.Fatalf("geometry source is %q, want %q", mat.GeometrySource, GeometryFromFallback)
	}
}

// TestDumpGeometryBeatsTheTerminal is the constraint the fix must not break: a
// chat surface recorded the grid it painted, and the playback stays cell-exact
// on THAT — a wider window the operator opened later is not the recording.
func TestDumpGeometryBeatsTheTerminal(t *testing.T) {
	dir := writeDump(t, 42, 7, []string{"row a", "row b"}, nil, nil)
	mat, err := loadSessionAtMeasured(dir, "", sessionevent.MeasuredTerminalGeometry(240, 60))
	if err != nil {
		t.Fatalf("loadSessionAtMeasured: %v", err)
	}
	if mat.GeometrySource != GeometryFromDump {
		t.Fatalf("geometry source is %q, want %q — the dump states the screen that WAS", mat.GeometrySource, GeometryFromDump)
	}
	if mat.Cols == 240 {
		t.Fatal("the measured terminal's width replaced the dump's recorded grid — a cell-exact replay must not re-wrap")
	}
}

// TestProductionProbeIsTerminalgeomStandard keeps the seam honest: the public
// loaders must pass the tree's ONE measurement, not a second copy of the ioctl.
// The check is textual and it is exact — a hand-rolled probe here is the defect
// Rule 14 names.
func TestProductionProbeIsTerminalgeomStandard(t *testing.T) {
	src, err := os.ReadFile("material.go")
	if err != nil {
		t.Fatalf("read material.go: %v", err)
	}
	if !strings.Contains(string(src), "terminalgeom.Standard()") {
		t.Fatal("material.go does not probe through terminalgeom.Standard() — a second copy of TIOCGWINSZ would drift from the tree's one answer")
	}
}
