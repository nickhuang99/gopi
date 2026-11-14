package testsuite

// chat_startup_cursor_pty_test.go — WHERE THE TERMINAL CURSOR IS WHEN `gopi chat`
// HAS FINISHED ITS FIRST FRAME, measured on a REAL pty with the REAL binary.
//
// THE REPORTED DEFECT (task 01a119d0, 2026-10-08). On startup the cursor did not
// blink at the input's edit position: the prompt was painted, the box was painted,
// and the block sat one row ABOVE the editing line — where it stayed, at a fixed
// but wrong row, until the next keystroke repainted. A user reads that as "the
// prompt is not focused yet".
//
// WHY THIS CASE NEEDS A PTY. The defect is a property of the FRAME: which writer
// had the last word on the cursor after every part of the startup screen (the
// box, the side panel, the chrome) had been committed. No in-process assertion can
// see it — the renderer's own belief (`cursorRows`) was correct the whole time;
// what was wrong was that the band's write ran AFTER the renderer
// (region.writeBandLine parks the cursor on the band's live row). So the judge
// here is the RENDERED CELL GRID replayed from the bytes the surface actually
// wrote (Rule 10.2), never a return value and never a byte pattern.
//
// The four assertions, and why each is the point:
//
//  1. THE CURSOR IS ON THE EDITING LINE. The row carrying the `you>` prompt is
//     FOUND in the grid, never assumed, and the cursor that ends the frame must be
//     on exactly that row. Pre-fix this failed by one row (measured: cursor row 27,
//     editing line row 28 at 100x30).
//  2. THE CURSOR IS IN THE CARET CELL. Being on the right row is not enough: the
//     block must be in the cell the user's next keystroke appends at — the column
//     right after the prompt — not back at column 0.
//  3. THE FRAME WAS NOT TRADED AWAY FOR IT. The box is still in the grid: a
//     placement that replaced the startup screen it was meant to finish would
//     satisfy 1 and 2 while destroying the surface.
//  4. NO KEYSTROKE WAS SENT. Every byte asserted on arrives between the child's
//     prompt and the assertions; the harness types nothing until after them, which
//     is exactly the operator's situation (the defect needed a keystroke to clear).

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// The case's own bounds, every one named (Rule 1).
const (
	// startupCursorCols / startupCursorRows are the geometry installed with
	// TIOCSWINSZ — the shape internal/lineedit's own capture documents and the one
	// the defect was measured at (100x30). The layout must be wide and tall enough
	// for the box plus the chrome below it, or the surface (rightly) declines to
	// draw a box it cannot fit and the case would be measuring a different screen.
	startupCursorCols = 100
	startupCursorRows = 30
	// startupCursorWindow is how long the case listens to the pty AFTER the prompt
	// appears. The prompt is painted as part of the layout, and the startup screen
	// is committed AFTER it (chat_banner_pty_test.go's own measurement), so the
	// frame under test is not complete at the prompt — reading there would assert
	// on a half-painted screen.
	startupCursorWindow = 2 * time.Second
	// startupCursorBoxBottom is the startup box's closing corner, spelled as the
	// grid shows it (chat_banner_pty_test.go's own glyph, repeated here because a
	// `package main` constant is not visible to this suite — the same reason
	// chatPromptMark is spelled).
	startupCursorBoxBottom = "╯"
)

// TestChatPTY_StartupLeavesTheCursorOnTheEditingCaret is the acceptance test for
// the reported defect: after the first frame, and with nothing typed, the block is
// on the input's caret.
func TestChatPTY_StartupLeavesTheCursorOnTheEditingCaret(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	// The mock is never asked anything (no turn is submitted); it is here because
	// MockEnv and a started `chat` expect a resolvable model endpoint, and a run
	// that cannot resolve one dies before it paints.
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "startup-cursor-case is not asked anything"})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: startupCursorCols, Row: startupCursorRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}
	// THE GEOMETRY IS PROVEN, NOT ASSUMED. A pty that never received the size
	// reports 0 columns and the surface then draws no box at all, which would make
	// this case pass or fail for a reason that has nothing to do with the cursor.
	if cols := lineedit.TerminalCols(int(chat.master.Fd())); cols != startupCursorCols {
		t.Fatalf("the pty reports %d columns, want the %d this case installed", cols, startupCursorCols)
	}

	boot := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	// The rest of the startup frame — the box and the side panel, committed after
	// the prompt. NOTHING IS TYPED: collect() is a pure reader, so every byte
	// judged below arrived with the operator's own situation reproduced (an idle
	// prompt and no keystroke to refresh the cursor).
	stream := boot + chat.collect(t, startupCursorWindow)

	// The screen the terminal would be: the child's own bytes, at the geometry
	// installed above. Reading the GRID (rather than the raw stream) is what makes
	// the claim about CELLS — and about the cursor a real VT would hold.
	screen := lineedit.NewScreen(startupCursorCols, startupCursorRows)
	screen.Feed([]byte(stream))

	// ---- 1 & 2. the cursor ends the frame on the editing line's caret cell ----
	// The row is FOUND, never assumed: a case that hard-coded the layout's height
	// would stop measuring the relation the moment the layout changed.
	line := startupCursorEditingRow(t, screen)
	if screen.CurRow != line {
		t.Fatalf("after the startup frame the cursor is on row %d, but the editing line is row %d (%q): the block is off the input and the prompt reads as unfocused — this is the reported sighting, and it stood until a keystroke repainted it\n--- grid ---\n%s",
			screen.CurRow, line, screen.Line(line), screen.Contents())
	}
	// The caret cell is the prompt's own width (the prompt INCLUDES its trailing
	// blank: the renderer trims the row's trailing cells and positions the cursor
	// with a CSI, so the blank is the cell the next character lands in).
	if want := len(chatPromptMark) + 1; screen.CurCol != want {
		t.Fatalf("the cursor is at column %d of the editing line, want %d (the cell right after the %q prompt): the row is right, the cell the user types into is not\n--- grid ---\n%s",
			screen.CurCol, want, chatPromptMark, screen.Contents())
	}

	// ---- 3. the frame was not traded away for the placement -----------------
	// The startup screen's own closing corner, read from the grid or the
	// scrollback a real terminal would have saved: a placement that cost the frame
	// its content would satisfy the two assertions above while destroying the
	// surface the operator was looking at.
	if !startupCursorBoxIsOnTheScreen(screen) {
		t.Fatalf("the startup box is gone from the screen: the caret placement replaced the frame it was meant to finish\n--- grid ---\n%s",
			screen.Contents())
	}

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — the session ends at the deliberate /quit", code)
	}
}

// startupCursorEditingRow returns the grid row carrying the editing line's prompt.
// The prompt is matched with its trailing spaces dropped: the grid model's Line()
// trims them (a trailing cell and a never-painted cell are identical in the grid),
// so a prefix test against the raw mark would match nothing. It fails rather than
// returning -1 — a case that could not find the editing line would otherwise
// assert a relation against whatever row happened to be there.
func startupCursorEditingRow(t *testing.T, screen *lineedit.Screen) int {
	t.Helper()
	want := strings.TrimRight(chatPromptMark, " ")
	for row := 0; row < screen.Rows; row++ {
		if strings.HasPrefix(screen.Line(row), want) {
			return row
		}
	}
	t.Fatalf("no row of the rendered grid carries the editing line's prompt %q — the frame under test never drew the input, so the cursor could not be measured on it\n--- grid ---\n%s",
		want, screen.Contents())
	return -1
}

// startupCursorBoxIsOnTheScreen reports whether the startup box's closing corner is
// anywhere the terminal would still show it — the live grid or the scrollback. The
// box is tall enough that a short pty may have pushed part of it into the saved
// history, and "the user can still read it" is the fact being asserted, not which
// buffer it is in.
func startupCursorBoxIsOnTheScreen(screen *lineedit.Screen) bool {
	for row := 0; row < screen.Rows; row++ {
		if strings.Contains(screen.Line(row), startupCursorBoxBottom) {
			return true
		}
	}
	for _, line := range screen.Scrollback() {
		if strings.Contains(line, startupCursorBoxBottom) {
			return true
		}
	}
	return false
}
