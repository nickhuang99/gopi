package uiclient

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// TestLiveSurfaceEmphasisReachesAttrs is the LIVE-SURFACE half of §2.3 of
// devdoc/handoff-20260924-ui-perception.md: the emphasis tests were all
// text/cell-level, and the handoff (2026-09-24) recorded that "nobody has yet
// confirmed on the real surface that a `**bold**` span shows as a non-default
// attr run".
//
// This closes that gap WITHOUT pixels: render an emphasis-bearing answer
// through the real model with the COLOUR palette (the capability decision a
// live terminal makes), push the resulting bytes through the surface's OWN
// sink (ScreenDumper, the io.Writer beside bubbletea's paint stream), and read
// the screen's per-cell attrs back. If the answer row is `default` for its
// whole width, the emphasis path is NOT reaching the renderer — the exact
// failure mode the handoff names.
func TestLiveSurfaceEmphasisReachesAttrs(t *testing.T) {
	m := newTestModel(t)
	// The LIVE capability: a real terminal resolves ColourOn -> BasicPalette.
	// (MonoPalette would prove nothing — byte-identical by design.)
	m.pal = lineedit.BasicPalette
	m.width = 68
	m.height = 24
	m.entries = []entry{
		{kind: entryUser, text: "what does the cell arithmetic say?"},
		assistantEntry("The answer: the **row shear** comes from counting a marker as a cell."),
	}
	m.assistantIdx = -1

	frame := m.View()

	sd, err := NewScreenDumper(DumpConfig{Cols: 68, Rows: 24})
	if err != nil {
		t.Fatalf("NewScreenDumper: %v", err)
	}
	if _, err := sd.Write([]byte(frame)); err != nil {
		t.Fatalf("dumper write: %v", err)
	}

	dump := sd.Snapshot()
	if !dump.AttrsRecorded {
		t.Fatal("AttrsRecorded=false — this build records no attrs, so nothing can be asserted")
	}
	if len(dump.AttrRuns) != 24 {
		t.Fatalf("AttrRuns rows = %d, want 24", len(dump.AttrRuns))
	}

	answerRow := -1
	for i, line := range dump.Lines {
		if strings.Contains(line, "row shear") {
			answerRow = i
			break
		}
	}
	if answerRow < 0 {
		t.Fatalf("the answer never reached the painted grid:\n%s", strings.Join(dump.Lines, "\n"))
	}

	// A non-default run must exist on the answer row.
	nonDefault := false
	var describe []string
	for _, run := range dump.AttrRuns[answerRow] {
		describe = append(describe, fmt.Sprintf("%d..%d=%s", run.Col, run.Col+run.Cells-1, run.Style))
		if run.Style != "default" {
			nonDefault = true
		}
	}
	if !nonDefault {
		t.Fatalf("answer row %d runs = %v — EVERYTHING is default, so emphasis is NOT reaching the renderer (the handoff's named failure mode)", answerRow, describe)
	}
	t.Logf("answer row %d runs: %v", answerRow, describe)
	t.Logf("answer row %d text: %q", answerRow, dump.Lines[answerRow])
}
