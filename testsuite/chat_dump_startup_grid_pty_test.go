package testsuite

// chat_dump_startup_grid_pty_test.go — the chat REPL's OWN screen dump must
// describe the screen the terminal got, measured on a REAL pty with the REAL
// binary and nothing typed.
//
// THE REPORTED DEFECT (task 01a119e6, 2026-10-08; the second sighting of a seam
// first filed in task 01a0fd34's family). A live `gopi chat --dump-screen` run
// reported an ALL-BLANK grid and `cursor row=0 col=0` in its artifact while the
// very same process's pty byte stream carried a fully painted screen (the banner
// box, the `you>` editing line, the status row). Measured at 100x30 with no
// keystroke, on the pre-fix binary:
//
//	#gopi-chat-dump … grid=100x30 geometry=layout mode=ansi frames=5 dumps=2
//	cursor row=0 col=0        ← the artifact
//	MINI-MODEL cursor after replay: row=28 col=5   ← the same run's byte stream
//
// Both wrong claims are consumed by agents (`gopi ui_state --target chat`), so
// this case is about the OBSERVER, not the paint path: internal/lineedit's own
// caret placement is correct on the same binary (testsuite's
// chat_startup_cursor_pty_test.go asserts it), which is why the judge below is
// the artifact against a REPLAY of the child's own bytes at the same geometry —
// two independent readings of one instant.
//
// THE MECHANISM, and why the case has to be a pty case at all. The sink is
// throttled (internal/uiclient dumpMinInterval): a window opened by one paint is
// closed by a timer, so a burst's tail still reaches the file. The window that
// broke this run was opened by a FLUSH rather than by a write — the layout's
// install calls SetGeometry, whose forced install restamps the interval in the
// MIDDLE of the startup burst — and the timer was armed only on a render write
// that found the interval elapsed. Every write after that flush was therefore
// `due=false` and the window was closed by nobody. Measured on the sink's own
// trace of the pre-fix binary: `frames=72 dumps=2`, the last install at
// `frames=5` (before the paint), while the in-memory grid held all 22 painted rows
// from `frames=6` onward. A unit test cannot see it: the grid was never wrong, the
// FILE was — which is a property of one throttle window at one instant of one run.
//
// THE ASSERTIONS. The artifact is read back through the SHIPPED reader
// (uiclient.ReadDump, the same one `gopi ui_state --target chat` uses):
//
//  1. the grid is not all-blank (BlankUnpainted), and it carries the editing
//     line's prompt — the row a reader of the artifact needs most;
//  2. the artifact's `cursor row/col` equals the position the SAME run's byte
//     stream leaves, as the region-aware VT replay computes it — the equality is
//     the whole point, because either number alone can be produced by a lucky
//     freeze.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/uiclient"
)

// The case's own bounds, every one named (Rule 1).
const (
	// dumpStartupCols / dumpStartupRows are the pty geometry, installed with
	// TIOCSWINSZ before the child is asked for anything. They are the geometry
	// of the measured sighting (100x30) and wide enough that the reserved
	// layout installs (below RowsMinUsable the surface draws no layout at all
	// and this case would be measuring a different screen).
	dumpStartupCols = 100
	dumpStartupRows = 30
	// dumpStartupSettle is how long the case listens AFTER the layout's first
	// full paint before it reads the artifact. It is comfortably longer than the
	// sink's own staleness bound (dumpMinInterval) plus the closing margin, which
	// is exactly what the fix restores: a reader of a live artifact must not have
	// to wait for a keystroke.
	dumpStartupSettle = 900 * time.Millisecond
	// ptyCtrlC is Ctrl+C, the key the REPL reads as "exit cleanly" at the prompt.
	// It is the case's own teardown input, never a keystroke the assertions rely
	// on (every byte judged above was read before this is sent).
	ptyCtrlC = "\x03"
)

// TestChatDumpPTY_StartupGridAndCursorMatchTheByteStream is the acceptance test:
// a live `gopi chat --dump-screen`, nothing typed, and the artifact must agree
// with the pty stream it observed.
func TestChatDumpPTY_StartupGridAndCursorMatchTheByteStream(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	// The mock is never asked anything (no turn is submitted); it is here because
	// the run must be able to resolve a model endpoint, or it dies before it paints.
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "the dump case is not asked anything"})
	defer llm.Close()

	// The artifact goes to a path of THIS case's own, so the reading cannot be a
	// leftover file from another run (the reason the flag exists).
	dumpPath := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)
	chat := startPTYChat(t, []string{"chat", "--dump-screen=" + dumpPath}, testutil.MockEnv(t, home, llm.URL()), work)
	// The geometry is installed BEFORE the surface reads it: a pty that never
	// received a size reports 0 columns, and a surface with no usable geometry
	// draws no reserved layout at all — which would make this case pass or fail
	// for a reason that has nothing to do with the dump.
	chat.resize(t, dumpStartupCols, dumpStartupRows)
	// The startup screen is committed after the prompt (chat_pty_test.go's own
	// measurement), so the window under test opens at the prompt and runs past it.
	stream := chat.waitFor(t, chatPromptMark, ptyPromptTimeout) + chat.collect(t, dumpStartupSettle)

	// ---- the artifact, read through the SHIPPED reader --------------------
	cand, err := uiclient.ReadDump(dumpPath)
	if err != nil {
		t.Fatalf("read the chat dump %s: %v\n--- stderr/output ---\n%s", dumpPath, err, stream)
	}
	grid := strings.Join(cand.Dump.Lines, "\n")
	frames := cand.Record.Fields[dumpRecordFramesField]
	dumps := cand.Record.Fields[dumpRecordDumpsField]

	// ---- 1. the grid is the screen the terminal got -----------------------
	if cand.BlankUnpainted() {
		t.Fatalf("the artifact's grid is ALL-BLANK (frames=%s dumps=%s geometry=%s) while the pty stream painted the screen: the dump described a screen that never existed.\n--- artifact grid ---\n%s\n--- pty stream (tail) ---\n%s",
			frames, dumps, cand.Record.Fields[dumpRecordGeometryField], grid, tailOf(stream, dumpStreamTailBytes))
	}
	if !strings.Contains(grid, chatPromptMark) {
		t.Fatalf("the artifact's grid does not carry the editing line's %q prompt (frames=%s dumps=%s): a reader asking the artifact what is on screen cannot see the input row.\n--- artifact grid ---\n%s",
			chatPromptMark, frames, dumps, grid)
	}

	// ---- 2. the cursor header equals the byte stream's own position -------
	// The replay is the INDEPENDENT judge: the same bytes, fed to the same
	// region-aware interpreter the pty path uses, at the geometry the pty was
	// given. The sink and this replay share an interpreter by design (the artifact
	// must mean the same thing as the screen), so a disagreement can only come
	// from the sink having been fed a DIFFERENT PREFIX of the stream — which is
	// exactly the defect this case pins.
	replay := lineedit.NewScreen(dumpStartupCols, dumpStartupRows)
	replay.Feed([]byte(stream))
	if cand.Dump.Cursor.Row != replay.CurRow || cand.Dump.Cursor.Col != replay.CurCol {
		t.Fatalf("the artifact says cursor row=%d col=%d while the same run's byte stream leaves the cursor at row=%d col=%d (frames=%s dumps=%s): the header an agent reads is the position of a frame the terminal had already left behind.\n--- artifact grid ---\n%s",
			cand.Dump.Cursor.Row, cand.Dump.Cursor.Col, replay.CurRow, replay.CurCol, frames, dumps, grid)
	}
	// The cursor is on the editing line's own row, found rather than assumed
	// (a hard-coded layout height stops measuring the relation the moment the
	// layout changes) — the claim an agent actually acts on.
	if row := editingRowIn(replay); row >= 0 && cand.Dump.Cursor.Row != row {
		t.Fatalf("the artifact's cursor is on row %d, but the editing line is row %d in the replayed screen (%q)",
			cand.Dump.Cursor.Row, row, replay.Line(row))
	}

	// The run is ended through its OWN input (Ctrl+C at the prompt is "exit
	// cleanly", chatInput.ReadLine's contract), so the case does not leave a gopi
	// child parked on a pty: the suite's close-out sweep counts live gopi
	// processes, and a leaked one is a failure of the CASE, not of the product.
	chat.write(t, ptyCtrlC)
	chat.close(t)
}

// editingRowIn finds the row carrying the editing line's prompt, or -1. It is the
// pty suite's own spelling of the same lookup chat_startup_cursor_pty_test.go
// performs (a `package main` constant is not visible here).
func editingRowIn(s *lineedit.Screen) int {
	for r := 0; r < s.Rows; r++ {
		if strings.Contains(s.Line(r), chatPromptMark) {
			return r
		}
	}
	return -1
}

// tailOf returns the last n bytes of a stream, spelled readably, for a failure
// message that shows what the run actually painted.
func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// dumpStreamTailBytes bounds the stream excerpt a failure message carries: enough
// to see the frame, small enough that a 100x30 startup burst stays readable.
const dumpStreamTailBytes = 1200

// The record fields this case quotes. They are the reader's own vocabulary
// (uiclient's DumpRecord.Fields), spelled once here so a failure message cannot
// name a field the record does not carry.
const (
	dumpRecordFramesField   = "frames"
	dumpRecordDumpsField    = "dumps"
	dumpRecordGeometryField = "geometry"
)
