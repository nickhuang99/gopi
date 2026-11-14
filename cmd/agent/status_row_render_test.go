package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// TestStatusRow_RenderProbe is the SCRATCH instrument (Rule 31 evidence: the real
// rows, not a description of them). It composes the row the REPL composes — through
// the source's own status() accessor, with the terminal width arriving through the
// viewport hook — and prints the ACTUAL CHARACTERS at the operator's geometry and at
// every rung of the degradation ladder. It asserts nothing but the width contract
// (nothing may run past its terminal), so it can be run before the change for the
// "before" rows and after it for the "after" rows.
//
// Run with: go test ./cmd/agent/ -run TestStatusRow_RenderProbe -v
func TestStatusRow_RenderProbe(t *testing.T) {
	const operatorCols = 117
	s := statusDegradeState(t)
	for _, cols := range append([]int{operatorCols}, statusDegradationWidths...) {
		s.setAvailCols(cols)
		row := s.status().Line(cols)
		w := lineedit.DisplayWidth(row)
		t.Logf("cols=%3d width=%3d %s", cols, w, row)
		if w > cols {
			t.Errorf("cols=%d: row is %d cells wide — it runs past its terminal\nrow: %q", cols, w, row)
		}
	}
	// The ladders the epic names, plus the narrowest rung, with the state a live
	// tool and a measured cache produce (the degrade state already carries both).
	for _, cols := range []int{100, 80, 60, 40, 24} {
		s.setAvailCols(cols)
		row := s.status().Line(cols)
		t.Logf("ladder cols=%3d width=%3d %s", cols, lineedit.DisplayWidth(row), row)
	}
	// THE OPERATOR'S OWN ROW: the numbers he read off the screen (13.1k/200.0k, 6%,
	// cache 12k/4.2k). This is the case the briefing is about, printed verbatim, so
	// the before/after comparison is made on his row rather than on a fixture's.
	ops := statusDegradeStateOperations()
	s.setOpsFn(func() statusOps { return ops })
	s.setAvailCols(operatorCols)
	op := s.status().Line(operatorCols)
	t.Logf("OPERATOR 117 width=%3d %s", lineedit.DisplayWidth(op), op)
	s.setAvailCols(statusDegradeWidth24)
	t.Logf("OPERATOR  24 width=%3d %s", lineedit.DisplayWidth(s.status().Line(statusDegradeWidth24)), s.status().Line(statusDegradeWidth24))
	// The cells each candidate label would cost, measured by the same function the
	// geometry's arithmetic uses — never hand-counted.
	for _, lab := range []string{"ctx", "context", "window", "ctx window", "cache", "C"} {
		t.Logf("label %-10q = %d cells", lab, lineedit.DisplayWidth(lab))
	}
	if got := fmt.Sprintf("%d", lineedit.DisplayWidth("ctx/")); got == "" {
		t.Fatal("unreachable")
	}
	_ = strings.TrimSpace
	_ = time.Second
}

// statusDegradeStateOperations is the operator's live row as status OPS: the
// context pair and percentage he quoted (13.1k of 200.0k, 6%) and the cache pair
// the row carried beside it.
func statusDegradeStateOperations() statusOps {
	return statusOps{
		ctxUsed:      13100,
		ctxMax:       200000,
		ctxMeasured:  true,
		cacheRead:    12000,
		cacheWrite:   4200,
		elapsed:      4 * time.Minute,
		elapsedKnown: true,
	}
}
