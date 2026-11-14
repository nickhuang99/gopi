package main

// chat_geometry_pty_test.go — the ideal geometry installed on a REAL pty, and
// the degradation sentence it writes when the terminal is smaller than the ideal.
//
// WHY A PTY AND NOT A UNIT TEST. chat_geometry_test.go pins the DECISION (the
// sentence's wording, the min()). What it cannot pin is whether the decision is
// WIRED: whether the canvas a `gopi chat` installs really reaches the layout, and
// whether the notice really lands on the diagnostic stream before the layout takes
// the screen. Both are properties of the install path (lineedit_wire.go's
// newChatInputWithPalette), so this case drives the same calls that path makes —
// lineedit.Open on a pty, chatApplyIdealGeometry, EnableRegion — and reads the
// layout's OWN geometry back (Editor.LayoutGeometry), which is the geometry the
// screen is drawn at.

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/lineedit"
)

// The pty this case runs on. geomPtyWideCols is deliberately WIDER than the ideal
// (117) — that is the whole point: the canvas must cap it — and geomPtyNarrowCols
// is below the banner's floor, so the notice has something to name.
const (
	geomPtyWideCols   = 200
	geomPtyWideRows   = 40
	geomPtyNarrowCols = 80
	geomPtyNarrowRows = 24
)

// geomTestIdeal is the ideal this case declares, written as a literal so the case
// does not pass by reading back whatever the resolver happened to answer. It is
// distinct from the built-in 117x26 for the same reason: a case that declared the
// default could not tell "the canvas was applied" from "nothing happened".
const geomTestIdeal = "117x26"

// openGeomEditor opens a real editor over a pty of the given size, with the
// ideal-geometry knob declared, and returns the editor, the terminal and the
// notice the wiring wrote.
func openGeomEditor(t *testing.T, cols, rows int) (*lineedit.Editor, *lineedit.Terminal, string) {
	t.Helper()
	t.Setenv(config.ChatIdealGeometryEnv, geomTestIdeal)
	pair := chatOpenPtyPair(t, cols, rows)
	if got := lineedit.TerminalCols(int(pair.Slave.Fd())); got != cols {
		t.Fatalf("the pty reports %d columns, want %d — the kernel did not take the size", got, cols)
	}
	term, err := lineedit.Open(pair.Slave, os.Stdout)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	t.Cleanup(func() {
		if err := term.Close(); err != nil {
			t.Errorf("close the terminal: %v", err)
		}
	})
	ed := lineedit.NewChatEditor(term, os.Stdout, chatPrompt, nil, os.Stderr, NewSlashCompleter())
	// The editor's OWN teardown (the completion worker) — registered AFTER the
	// terminal's, so it runs BEFORE it (t.Cleanup is LIFO) and the worker is gone
	// before the descriptor it may be parked on. A harness that skipped this left
	// a parked goroutine behind for the suite's sweep to report.
	t.Cleanup(ed.Close)
	var notice bytes.Buffer
	// THE PRODUCTION CALL, with the production arguments: the wiring passes the
	// diagnostic stream, and this case passes a buffer so the same bytes are
	// readable.
	chatApplyIdealGeometry(ed, term, &notice)
	return ed, term, notice.String()
}

// TestChatIdealGeometryCapsAWideTerminalAtTheIdeal is the canvas half: on a
// 200-column pty a chat run draws the reserved layout at the IDEAL width, and the
// extra columns are simply not the layout's.
func TestChatIdealGeometryCapsAWideTerminalAtTheIdeal(t *testing.T) {
	ed, _, notice := openGeomEditor(t, geomPtyWideCols, geomPtyWideRows)
	if notice != "" {
		t.Fatalf("a %d-column terminal hosts the %s ideal (%d) — it must NOT be told it degraded:\n%s",
			geomPtyWideCols, geomTestIdeal, config.ChatIdealCols(), notice)
	}
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false — the case needs the reserved layout installed",
			geomPtyWideCols, geomPtyWideRows)
	}
	cols, rows, ok := ed.LayoutGeometry()
	if !ok {
		t.Fatal("no layout geometry after EnableRegion — nothing was drawn at a width this case can read")
	}
	if cols != config.ChatIdealCols() {
		t.Fatalf("the layout is %d columns wide on a %d-column terminal, want the ideal %d — the canvas was not applied",
			cols, geomPtyWideCols, config.ChatIdealCols())
	}
	// ROWS ARE THE TERMINAL'S, never the ideal's: the status row is anchored to
	// the screen's LAST row, so a canvas claiming fewer rows than the terminal has
	// would park the status line mid-screen.
	if rows != geomPtyWideRows {
		t.Fatalf("the layout is %d rows tall, want the terminal's %d — rows must never be capped by the ideal (%d)",
			rows, geomPtyWideRows, config.ChatIdealRows())
	}
}

// TestChatIdealGeometryKeepsANarrowTerminalAndSaysSo is the degradation half:
// below the ideal the canvas is the TERMINAL's width (never wider — the terminal
// would wrap every row), and the operator is TOLD what the terminal cannot host.
func TestChatIdealGeometryKeepsANarrowTerminalAndSaysSo(t *testing.T) {
	ed, _, notice := openGeomEditor(t, geomPtyNarrowCols, geomPtyNarrowRows)
	if notice == "" {
		t.Fatal("an 80x24 terminal against a 117x26 ideal produced NO notice — a silent downgrade (Rule 15)")
	}
	for _, want := range []string{
		"80x24",
		geomTestIdeal,
		config.ChatIdealGeometryEnv,
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("the degradation notice does not name %q:\n%s", want, notice)
		}
	}
	if !ed.EnableRegion() {
		// 80x24 is above the layout's own floor, so the reserved layout must still
		// install — the degradation is about the CHROME, not about the layout.
		t.Fatal("EnableRegion(80x24) = false — the reserved layout's floor is lower than this")
	}
	cols, _, ok := ed.LayoutGeometry()
	if !ok {
		t.Fatal("no layout geometry after EnableRegion")
	}
	if cols != geomPtyNarrowCols {
		t.Fatalf("the layout is %d columns wide on an %d-column terminal, want the terminal's own width — a canvas wider than the terminal wraps every row",
			cols, geomPtyNarrowCols)
	}
}

// (The RESIZE half lives in internal/lineedit/canvas_test.go, where a scripted
// source can deliver a winch step and the layout's width is read back through
// the editor's own read loop — the pty path's resize is the same canvasWidth()
// call, so asserting it twice would only assert the fixture.)
