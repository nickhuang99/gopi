package main

// chat_surface_see_measure_test.go — the MEASUREMENT half of the fix, on the
// production path.
//
// internal/lineedit's suite proves the RENDERING is bounded and internal/agent's
// proves the TOOL cannot return an unbounded answer. Neither of them runs the
// piece this defect actually lived in: `chatSurface.ScreenGrid` in THIS package,
// which is the function `action=see` calls. A test for a boundary that never
// executes the boundary is the Rule 31 failure, so the numbers below are taken
// from the real surface object reading a real artifact on disk, produced by a
// real scrolling screen, and they are compared against the UNBOUNDED rendering of
// the same artifact — i.e. against exactly what this path returned before the
// fix.
//
// The measured incident (2026-11-06) is the shape this fixture models: a 24-row
// screen, a turn history long enough to dominate, and one `see` return of
// 2,783,126 characters of which the grid was 2,948.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// seeMeasureScrollRounds is how many committed lines the fixture pushes off the
// top. Each round records exactly one scrollback row, so the dump's history is
// that many rows — the same arithmetic the incident's `scrollback_begin
// lines=59497` reported.
const seeMeasureScrollRounds = 20000

// seeMeasureRows / seeMeasureCols are the incident's geometry: a real chat screen.
const (
	seeMeasureCols = 100
	seeMeasureRows = 24
)

// TestChatSurfaceSeeReturnsTheGridNotTheHistory is the regression pin AND the
// measurement: the same artifact, read through the production ScreenGrid, must
// come back bounded while the artifact on disk still holds its whole history.
func TestChatSurfaceSeeReturnsTheGridNotTheHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: path, Cols: seeMeasureCols, Rows: seeMeasureRows,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              os.Getpid(),
			Target:           "self:see-measure",
			ColsRows:         [2]int{seeMeasureCols, seeMeasureRows},
			TerminalColsRows: [2]int{seeMeasureCols, seeMeasureRows},
			Started:          time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("arm the dump sink: %v", err)
	}

	// Paint the way the REPL paints: a committed turn line, then the newlines
	// that scroll it off the top into history. Real bytes through the real sink.
	var paint strings.Builder
	for i := 0; i < seeMeasureScrollRounds; i++ {
		fmt.Fprintf(&paint, "\x1b[1;1H──── turn %d · a conversation line of the length a chat turn really has, with prose in it", i+1)
		paint.WriteString(strings.Repeat("\r\n", seeMeasureRows))
	}
	// And the LIVE screen: the grid a reader is asking for.
	paint.WriteString("\x1b[1;1H▌ the answer band of the CURRENT turn\x1b[24;1Hstatus: idle · turns " + fmt.Sprint(seeMeasureScrollRounds))
	if _, err := d.Write([]byte(paint.String())); err != nil {
		t.Fatalf("paint the fixture screen: %v", err)
	}

	snap := d.Snapshot()
	if len(snap.Scrollback) < seeMeasureScrollRounds/2 {
		t.Fatalf("the fixture did not build the history it claims: %d scrollback rows after %d rounds", len(snap.Scrollback), seeMeasureScrollRounds)
	}

	// THE PRODUCTION PATH — the function action=see reaches.
	s := &chatSurface{dumper: d}
	obs := s.ScreenGrid()
	if !obs.Armed || !obs.Found {
		t.Fatalf("the fixture is not armed/found: %+v", obs)
	}

	// The reading this path returned BEFORE the fix: the artifact's whole text.
	before := len(d.Text())
	after := len(obs.Grid)

	t.Logf("SEE-MEASURE scrollback_rows=%d geometry=%dx%d", len(snap.Scrollback), seeMeasureCols, seeMeasureRows)
	t.Logf("SEE-MEASURE before (unbounded artifact rendering) = %d characters", before)
	t.Logf("SEE-MEASURE after  (chatSurface.ScreenGrid)       = %d characters", after)
	t.Logf("SEE-MEASURE reduction = %.1fx", float64(before)/float64(after))

	if after >= before {
		t.Fatalf("ScreenGrid returned %d characters against the artifact's %d — the tool return is not bounded", after, before)
	}
	// The defect, stated as the property that was violated: the answer must not
	// be dominated by history.
	if after*10 >= before {
		t.Fatalf("ScreenGrid returned %d of %d characters (%.1f%%): the scrollback still dominates the answer",
			after, before, 100*float64(after)/float64(before))
	}
	// And it must still be USEFUL: the live grid, verbatim, with the truncation
	// line saying what the artifact holds.
	for _, want := range []string{
		"the answer band of the CURRENT turn",
		"status: idle · turns ",
		lineedit.DumpScrollbackTruncatedLabel,
		"gopi ui_state",
	} {
		if !strings.Contains(obs.Grid, want) {
			t.Errorf("the bounded answer lost %q — a bounded screen must still be the screen\n--- head ---\n%s", want, firstSeeLines(obs.Grid, 12))
		}
	}
	// The ARTIFACT is untouched: ui_state and the replay read it whole.
	if !strings.Contains(d.Text(), "──── turn 1 ·") {
		t.Fatalf("the artifact lost its OLDEST history row — the fix bounded the rendering, not the history, and this file is the check")
	}
	if strings.Contains(obs.Grid, "──── turn 1 ·") {
		t.Fatalf("the tool return still carries the oldest history row — the bound is not in effect")
	}
}

// TestChatSurfaceSeeReturnStaysUnderTheAgentBackstop pins the two bounds against
// each other: an ordinary long session must be handled by the SCROLLBACK bound
// (which keeps its tail and says what it held back), never by the agent package's
// blunt character backstop. The assertion here is the checkable half of that —
// the rendering this package produces, plus the envelope the agent package wraps
// it in, is far below `chatSurfaceSeeCharsMax` — so the blunt bound cannot be the
// thing that fires on a real screen. The agent package's own suite drives its
// dispatcher over this same observation shape.
func TestChatSurfaceSeeReturnStaysUnderTheAgentBackstop(t *testing.T) {
	dir := t.TempDir()
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: filepath.Join(dir, uiclient.ChatDumpFileName),
		Cols: seeMeasureCols, Rows: seeMeasureRows,
		Identity: uiclient.DumpIdentity{
			Surface: uiclient.SurfaceChat, PID: os.Getpid(), Target: "self:see-measure-2",
			ColsRows: [2]int{seeMeasureCols, seeMeasureRows}, TerminalColsRows: [2]int{seeMeasureCols, seeMeasureRows},
			Started: time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("arm the dump sink: %v", err)
	}
	var paint strings.Builder
	for i := 0; i < seeMeasureScrollRounds; i++ {
		fmt.Fprintf(&paint, "\x1b[1;1H──── turn %d · a long line, the kind a real conversation writes, with enough prose to cost bytes", i+1)
		paint.WriteString(strings.Repeat("\r\n", seeMeasureRows))
	}
	// The LIVE screen, the same way the first measurement paints it: the grid a
	// reader is actually asking for.
	paint.WriteString("\x1b[1;1H▌ the answer band of the CURRENT turn\x1b[24;1Hstatus: idle · turns " + fmt.Sprint(seeMeasureScrollRounds))
	if _, err := d.Write([]byte(paint.String())); err != nil {
		t.Fatalf("paint: %v", err)
	}
	obs := (&chatSurface{dumper: d}).ScreenGrid()
	history := len(d.Snapshot().Scrollback)

	// The agent package's envelope over this grid: a facts line, an optional
	// qualification line, the grid, and a trailing newline. The generator is the
	// tool's own ceiling, read from the package that owns it.
	envelope := seeFactsEnvelopeChars + len(obs.Grid) + 1
	t.Logf("SEE-MEASURE scrollback_rows=%d grid=%d chars envelope=%d chars (agent backstop %d)",
		history, len(obs.Grid), envelope, agent.ChatSurfaceSeeCharsMax)
	if envelope*4 >= agent.ChatSurfaceSeeCharsMax {
		t.Fatalf("a %d-row-history answer costs %d characters against the agent backstop's %d: the blunt bound is close enough to fire on a real screen, which would cut an answer without saying what it held back",
			history, envelope, agent.ChatSurfaceSeeCharsMax)
	}
	if !strings.Contains(obs.Grid, "the answer band") && !strings.Contains(obs.Grid, "status: idle · turns ") {
		t.Fatalf("the bounded rendering does not carry the live grid:\n%s", firstSeeLines(obs.Grid, 12))
	}
	if !strings.Contains(obs.Grid, lineedit.DumpScrollbackTruncatedLabel) {
		t.Fatalf("the rendering held history back without saying so")
	}
}

// seeFactsEnvelopeChars bounds the facts line chatSurfaceSee writes above the
// grid: it is one line, and this constant is the generous ceiling a test uses to
// reason about the whole return without re-implementing the format.
const seeFactsEnvelopeChars = 4096

// firstSeeLines bounds a failure message.
func firstSeeLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
