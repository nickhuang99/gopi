package uiclient

// geometry_test.go — the regression guard for the terminal-geometry defect.
//
// WHAT WENT WRONG (measured on a pty, 2026-09-21): `gopi ui` laid out for
// 80x24 in EVERY terminal. Bubble Tea decides whether to query the terminal
// size by testing its OUTPUT for being a terminal, and the surface wraps that
// output in ScreenWriter (an io.Writer, not a tty) — so no WindowSizeMsg was
// ever emitted and the model kept its hardcoded default. On a 100-column pty
// the model's own notice line broke at 76 visible columns (width 80) while the
// kernel wrapped the banner lines at 100.
//
// THE GUARD, and why it is shaped this way: Run() needs a real tty to receive a
// WindowSizeMsg, so a test cannot observe the defect end-to-end — but it does
// not have to. The contract that failed is "the caller's geometry reaches the
// model", and that is `applyGeometry`, asserted directly here. The end-to-end
// half of the proof is in the report (a pty capture at 120 columns, red before
// / green after).

import "testing"

func TestApplyGeometryUsesTheCallersTerminalSize(t *testing.T) {
	m, err := newModel(&Client{BaseURL: DefaultServerBase}, DefaultServerBase, false)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	// The model's own default, for the record: a caller that knows nothing
	// keeps this, which is the pre-fix behaviour.
	if m.width == 0 || m.height == 0 {
		t.Fatalf("newModel produced a zero-sized model: %dx%d", m.width, m.height)
	}
	defaultW, defaultH := m.width, m.height

	applyGeometry(&m, Config{Width: 120, Height: 45})
	if m.width != 120 || m.height != 45 {
		t.Errorf("applyGeometry(120x45) left the model at %dx%d — the caller's terminal size did not reach the model", m.width, m.height)
	}

	// A zero field means "not known": the model must keep its default rather
	// than adopt a zero-width layout (the failure mode a naive assignment has).
	m2, err := newModel(&Client{BaseURL: DefaultServerBase}, DefaultServerBase, false)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	applyGeometry(&m2, Config{})
	if m2.width != defaultW || m2.height != defaultH {
		t.Errorf("applyGeometry(zero) changed the model to %dx%d, want the %dx%d default kept",
			m2.width, m2.height, defaultW, defaultH)
	}
}

// TestGeometryReachesTheView: the geometry is not merely stored, it is what the
// screen is laid out from — a wide terminal must produce a wider header line.
func TestGeometryReachesTheView(t *testing.T) {
	m, err := newModel(&Client{BaseURL: DefaultServerBase}, DefaultServerBase, false)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	narrow := m
	narrow.width = 40
	wide := m
	wide.width = 200

	headerNarrow := narrow.View()
	headerWide := wide.View()
	if headerWide == headerNarrow {
		t.Fatal("the view is identical at width 40 and width 200 — the geometry does not reach the layout")
	}
	// The header is truncated to the width, so the wide terminal must show at
	// least as much of it.
	if len([]rune(firstLine(headerWide))) <= len([]rune(firstLine(headerNarrow))) {
		t.Errorf("the header did not grow with the terminal: %q vs %q", firstLine(headerNarrow), firstLine(headerWide))
	}
}

// firstLine returns the first line of a rendered screen.
func firstLine(view string) string {
	if i := indexByte(view, '\n'); i >= 0 {
		return view[:i]
	}
	return view
}

// indexByte is strings.IndexByte, kept local so this file needs no import for
// one call.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
