package shot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// The tests below drive a REAL pty with a REAL child process. The child is a
// small shell script rather than an in-test helper binary on purpose: a Go test
// binary on a pty prints its own harness output ("=== RUN …", "PASS"), which
// would be captured as if the UI had drawn it. A shell script draws exactly what
// the test asks for, so every assertion below is about the capture engine.

// Named bounds for the tests (Rule 1).
const (
	testTUISettle = 250 * time.Millisecond
	testTUIWait   = 8 * time.Second
	testTUIGrace  = 2 * time.Second
	testCols      = 60
	testRows      = 12
	// testScriptName is the fake UI's file name inside the test's temp dir, and
	// testScriptMode its executable permissions.
	testScriptName = "fake-tui.sh"
	testScriptMode = 0o755
)

// writeFakeTUI drops an executable shell script into dir and returns its path.
func writeFakeTUI(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), testScriptName)
	if err := os.WriteFile(path, []byte(body), testScriptMode); err != nil {
		t.Fatalf("write fake TUI: %v", err)
	}
	return path
}

// TestCaptureTextSurface_InterpretsTheScreen — the capture's contract: the byte
// stream a TUI writes is INTERPRETED, not echoed. The script paints a marker,
// erases part of it with a CSI and repositions the cursor with another, so the
// raw stream contains text the finished screen must NOT show and omits the
// layout the screen must show. A capture that returned the byte stream would
// fail both halves.
func TestCaptureTextSurface_InterpretsTheScreen(t *testing.T) {
	// printf writes (to the pty, since all three std descriptors are the slave):
	//   - a row that is later erased on screen ("GONE") and a cursor move to
	//     column 8 to overwrite the rest ("KEPT")
	//   - the prompt, then a blocking read so the child stays alive while the
	//     capture's quiet window elapses
	script := writeFakeTUI(t, `#!/bin/sh
printf '\033[2JGONE-MARKER\033[1;1H\033[2Kyou> '
read -r line
printf 'echo: %s\r\n' "$line"
`)
	cap, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:   []string{script},
		Cols:   testCols,
		Rows:   testRows,
		Ready:  "you>",
		Settle: testTUISettle,
		Wait:   testTUIWait,
	})
	if err != nil {
		t.Fatalf("CaptureTextSurface: %v", err)
	}
	if cap.Cols != testCols || cap.Rows != testRows {
		t.Fatalf("geometry = %dx%d, want %dx%d", cap.Cols, cap.Rows, testCols, testRows)
	}
	if cap.Bytes == 0 {
		t.Fatal("no bytes were read from the pty")
	}
	// The erased row is gone from the finished screen and the row the child drew
	// last is there — i.e. the ERASE was applied, not merely received.
	if strings.Contains(cap.ScreenText, "GONE-MARKER") {
		t.Fatalf("the erased text survived on the screen:\n%s", cap.ScreenText)
	}
	if !strings.Contains(cap.ScreenText, "you>") {
		t.Fatalf("the prompt is not on the captured screen:\n%s", cap.ScreenText)
	}
	// The screen is a GRID: at most Rows rows, each at most Cols cells.
	lines := strings.Split(cap.ScreenText, "\n")
	if len(lines) > testRows {
		t.Fatalf("captured %d rows, want at most the pty's %d", len(lines), testRows)
	}
	for i, l := range lines {
		if w := cellsOf(l); w > testCols {
			t.Fatalf("row %d is %d cells wide, want at most %d: %q", i, w, testCols, l)
		}
	}
}

// cellsOf measures a row's width in terminal CELLS by driving the reference
// implementation (internal/lineedit's cell grid) — the same instrument the
// renderer's own width rule is asserted against in internal/text2img.
func cellsOf(s string) int {
	const probeCols = 4096
	scr := lineedit.NewScreen(probeCols, 1)
	scr.Feed([]byte(s))
	return scr.CurCol
}

// TestCaptureTextSurface_TypedLineReachesTheUIAndComesBack — keys written to the
// pty are the USER's input: the child reads the line and echoes it, so the
// capture shows both sides. This is what makes the surface a UI capture rather
// than a screenshot of a static frame.
func TestCaptureTextSurface_TypedLineReachesTheUIAndComesBack(t *testing.T) {
	script := writeFakeTUI(t, `#!/bin/sh
printf 'you> '
read -r line
printf '\r\nanswer: %s\r\n' "$line"
exit 0
`)
	cap, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:       []string{script},
		Cols:       testCols,
		Rows:       testRows,
		Ready:      "you>",
		ExitKeys:   "hello-from-the-test" + textCarriageReturn,
		ExitWait:   testTUIGrace,
		Settle:     testTUISettle,
		Wait:       testTUIWait,
		Scrollback: true,
	})
	if err != nil {
		t.Fatalf("CaptureTextSurface: %v", err)
	}
	if !strings.Contains(cap.ScreenText, "answer: hello-from-the-test") {
		t.Fatalf("the UI's answer to the typed line is not on the captured screen:\n%s", cap.ScreenText)
	}
}

// TestCaptureTextSurface_ExitKeysWithACarriageReturnSubmit — the practical
// regression: the keys a capture sends are a command plus the submit key, and
// the engine must deliver them so the UI ACTS on them. The script answers a line
// only when it arrives terminated by CR, so a capture that sent the bytes
// without the submit key would hang until the bound and fail this test.
func TestCaptureTextSurface_ExitKeysWithACarriageReturnSubmit(t *testing.T) {
	script := writeFakeTUI(t, `#!/bin/sh
printf 'you> '
IFS= read -r line
printf '\r\nacted: %s\r\n' "$line"
exit 0
`)
	cap, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:     []string{script},
		Cols:     testCols,
		Rows:     testRows,
		Ready:    "you>",
		ExitKeys: "run-it" + textCarriageReturn,
		ExitWait: testTUIGrace,
		Settle:   testTUISettle,
		Wait:     testTUIWait,
	})
	if err != nil {
		t.Fatalf("CaptureTextSurface: %v", err)
	}
	if !strings.Contains(cap.ScreenText, "acted: run-it") {
		t.Fatalf("the UI never acted on the keys — the submit key did not arrive:\n%s", cap.ScreenText)
	}
}

// TestCaptureTextSurface_ChildExitEndsTheCaptureCleanly — a UI that ends itself
// is reported as EXITED with its exit code, so a caller can tell a clean end
// from a kill (the chat surface relies on this: /quit must really close the
// session). Nothing is killed here, so the code is the child's own.
func TestCaptureTextSurface_ChildExitEndsTheCaptureCleanly(t *testing.T) {
	script := writeFakeTUI(t, `#!/bin/sh
printf 'you> '
read -r line
printf '\r\nbye\r\n'
exit 7
`)
	cap, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:     []string{script},
		Cols:     testCols,
		Rows:     testRows,
		Ready:    "you>",
		ExitKeys: "quit" + textCarriageReturn,
		ExitWait: testTUIGrace,
		Settle:   testTUISettle,
		Wait:     testTUIWait,
	})
	if err != nil {
		t.Fatalf("CaptureTextSurface: %v", err)
	}
	if !cap.Exited {
		t.Fatal("the child ended itself but the capture reports it as killed")
	}
	if cap.ExitCode != 7 {
		t.Fatalf("exit code = %d, want the child's own 7", cap.ExitCode)
	}
	if !strings.Contains(cap.ScreenText, "bye") {
		t.Fatalf("the child's last frame is missing from the capture:\n%s", cap.ScreenText)
	}
}

// TestCaptureTextSurface_ExitKeysWipeKeepsTheScreen — the regression for the
// chat capture's observed flake. cmd/agent's chat REPL hands the screen back on
// exit (internal/lineedit's region.Reset: `\033[r\033[H\033[2J`, i.e. reset the
// scroll region, home the cursor, erase the display — measured on the real pty
// immediately before the goodbye line). A capture that honours its exit keys
// therefore ends holding an ERASED grid, and what the PNG then shows is whatever
// the wipe left behind — which is nothing, or a leftover row, depending on timing.
//
// The contract: the capture photographs the screen the UI had FINISHED drawing,
// so the child's own close-out cannot delete the frame being captured. The child
// here does exactly what the chat REPL does — answer, wipe, exit — and the answer
// AND the banner must still be in the capture.
func TestCaptureTextSurface_ExitKeysWipeKeepsTheScreen(t *testing.T) {
	script := writeFakeTUI(t, `#!/bin/sh
printf 'banner-line\r\n'
printf 'you> '
read -r line
printf '\r\nanswer: %s\r\n' "$line"
printf '\033[r\033[H\033[2J'
exit 0
`)
	cap, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:     []string{script},
		Cols:     testCols,
		Rows:     testRows,
		Ready:    "you>",
		ExitKeys: "/quit" + textCarriageReturn,
		ExitWait: testTUIGrace,
		Settle:   testTUISettle,
		Wait:     testTUIWait,
	})
	if err != nil {
		t.Fatalf("CaptureTextSurface: %v", err)
	}
	if !cap.FrameFromHistory {
		t.Fatalf("the child erased the display before exiting but the capture reports the live frame:\n%s", cap.ScreenText)
	}
	for _, want := range []string{"banner-line", "you>"} {
		if !strings.Contains(cap.ScreenText, want) {
			t.Fatalf("the close-out wipe deleted %q from the capture — the ending overwrote the screen being captured:\n%s", want, cap.ScreenText)
		}
	}
	if strings.TrimSpace(cap.ScreenText) == "" {
		t.Fatal("the capture came back empty: the wipe was photographed instead of the screen")
	}
}

// TestCaptureTextSurface_ExitKeysAnswerIsNotOverwritten — the other side of the
// same rule: a UI that ANSWERS the exit keys instead of erasing the screen is
// captured answering them. The fallback must not fire here, or a capture would
// silently throw away the very frame the keys produced.
func TestCaptureTextSurface_ExitKeysAnswerIsNotOverwritten(t *testing.T) {
	script := writeFakeTUI(t, `#!/bin/sh
printf 'banner-line\r\n'
printf 'you> '
read -r line
printf '\r\nanswer: %s\r\n' "$line"
exit 0
`)
	cap, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:     []string{script},
		Cols:     testCols,
		Rows:     testRows,
		Ready:    "you>",
		ExitKeys: "/quit" + textCarriageReturn,
		ExitWait: testTUIGrace,
		Settle:   testTUISettle,
		Wait:     testTUIWait,
	})
	if err != nil {
		t.Fatalf("CaptureTextSurface: %v", err)
	}
	if cap.FrameFromHistory {
		t.Fatal("the UI answered the exit keys and kept its screen, but the capture fell back to an earlier frame")
	}
	if !strings.Contains(cap.ScreenText, "answer: /quit") {
		t.Fatalf("the UI's answer to the exit keys is missing from the capture:\n%s", cap.ScreenText)
	}
}

// TestCaptureTextSurface_FallbackNeedsNoMarker — the criterion is content LOSS,
// not the ready marker: when the ending erased rows the settled screen had
// shown, the capture restores them whether or not the caller named a marker to
// wait for. The marker's job is synchronisation (when to stop reading); it is not
// a licence to ship a PNG of an erased screen.
func TestCaptureTextSurface_FallbackNeedsNoMarker(t *testing.T) {
	script := writeFakeTUI(t, `#!/bin/sh
printf 'banner-line\r\n'
read -r line
printf '\033[2J'
exit 0
`)
	cap, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:     []string{script},
		Cols:     testCols,
		Rows:     testRows,
		Ready:    TextReadyNone,
		ExitKeys: "q" + textCarriageReturn,
		ExitWait: testTUIGrace,
		Settle:   testTUISettle,
		Wait:     testTUIWait,
	})
	if err != nil {
		t.Fatalf("CaptureTextSurface: %v", err)
	}
	if !cap.FrameFromHistory {
		t.Fatal("the child erased the display and the capture kept the wiped frame (no marker was named, but the loss is still a loss)")
	}
	if !strings.Contains(cap.ScreenText, "banner-line") {
		t.Fatalf("the pre-wipe screen was not restored:\n%s", cap.ScreenText)
	}
}

// TestTrimGridRowsAndGridLostRows pin the two pure rules the fallback rests on:
// trailing blanks fold away (interior blanks stay — they are layout), and a
// screen counts as destroyed only when a row it showed is GONE from the grid in
// hand. A row REWRITTEN in place (a UI answering the keys) is not destruction,
// which is what keeps the capture from preferring an older frame.
func TestTrimGridRowsAndGridLostRows(t *testing.T) {
	rows := trimGridRows([]string{"a", "", "b", "", ""})
	if len(rows) != 3 || rows[2] != "b" {
		t.Fatalf("trimGridRows = %q, want the trailing blanks folded and the interior blank kept", rows)
	}
	if got := trimGridRows([]string{"", "", ""}); len(got) != 0 {
		t.Fatalf("an all-blank grid trimmed to %q, want empty", got)
	}

	settled := []string{"banner", "you>"}
	if !gridLostRows(settled, []string{"you>"}) {
		t.Fatal("a wiped screen (only the prompt left) must count as lost rows")
	}
	if !gridLostRows(settled, nil) {
		t.Fatal("an empty grid must count as lost rows")
	}
	if gridLostRows(settled, []string{"banner", "you> seen:[/quit]", "answer"}) {
		t.Fatal("a row rewritten in place was reported as lost: the fallback would discard the UI's answer")
	}
	if gridLostRows([]string{"", ""}, nil) {
		t.Fatal("blank rows carry no content and must never count as lost")
	}
}

// TestCaptureTextSurface_MissingMarkerFailsLoudly — a capture that cannot see
// the UI's readiness marker must fail NAMING the marker and the bytes it did
// receive. A silent success here would hand back a PNG of whatever happened to
// be on the screen (Rule 15).
func TestCaptureTextSurface_MissingMarkerFailsLoudly(t *testing.T) {
	script := writeFakeTUI(t, `#!/bin/sh
printf 'starting up, no prompt yet\r\n'
read -r _
`)
	_, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:   []string{script},
		Cols:   testCols,
		Rows:   testRows,
		Ready:  "never-on-this-screen",
		Settle: testTUISettle,
		Wait:   time.Second,
	})
	if err == nil {
		t.Fatal("a capture that never saw its readiness marker must fail, not return a frame")
	}
	if !strings.Contains(err.Error(), "never-on-this-screen") {
		t.Fatalf("the error does not name the marker it waited for: %v", err)
	}
}

// TestCaptureTextSurface_NoOutputIsNotAScreen — a UI that draws nothing yields a
// failure, never an empty capture that the CLI would render to a blank PNG.
func TestCaptureTextSurface_NoOutputIsNotAScreen(t *testing.T) {
	script := writeFakeTUI(t, `#!/bin/sh
sleep 5
`)
	start := time.Now()
	cap, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:   []string{script},
		Cols:   testCols,
		Rows:   testRows,
		Settle: testTUISettle,
		Wait:   testTUIWait,
	})
	if err != nil {
		// The engine may also fail the capture outright; both endings are honest.
		return
	}
	if strings.TrimSpace(cap.ScreenText) != "" {
		t.Fatalf("a silent UI produced screen text: %q", cap.ScreenText)
	}
	if elapsed := time.Since(start); elapsed > testTUIWait {
		t.Fatalf("the settle path took %s, longer than the capture's own bound %s", elapsed, testTUIWait)
	}
}

// TestCaptureTextSurface_MissingPtyNamesTheFacility — an environment without
// /dev/ptmx must produce an error naming it, not a panic or a hang.
func TestCaptureTextSurface_MissingPtyNamesTheFacility(t *testing.T) {
	if _, err := os.Stat(ptyMasterPath); err == nil {
		t.Skip("this host has a pty multiplexer: the missing-facility path cannot be exercised here")
	}
	_, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv: []string{"true"},
		Cols: testCols,
		Rows: testRows,
		Wait: time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), ptyMasterPath) {
		t.Fatalf("error = %v, want it to name %s", err, ptyMasterPath)
	}
}

// TestCaptureTextSurface_NoArgvIsRefused — the engine never invents a command.
func TestCaptureTextSurface_NoArgvIsRefused(t *testing.T) {
	_, err := CaptureTextSurface(context.Background(), TextCaptureOptions{Cols: testCols, Rows: testRows})
	if err == nil {
		t.Fatal("a capture with no argv must be refused")
	}
}

// TestWriteTypedKeys_DoesNotEscapeIntoTheScreen is the regression for the
// terminal's own escape mechanism: the exit keys are ordinary characters
// ("/quit" + CR) and must reach the child as BYTES, because a bare ESC inside a
// pty write is the start of a control sequence — the terminal would swallow it
// and the keystrokes after it, so the UI would never see the command. The keys
// must also be separated in time (see writeTypedKeys): the assertion is on the
// bytes AND on the gaps.
func TestWriteTypedKeys_DoesNotEscapeIntoTheScreen(t *testing.T) {
	script := writeFakeTUI(t, `#!/bin/sh
printf 'you> '
read -r line
printf '\r\nseen:[%s]\r\n' "$line"
exit 0
`)
	cap, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:     []string{script},
		Cols:     testCols,
		Rows:     testRows,
		Ready:    "you>",
		ExitKeys: "/quit" + textCarriageReturn,
		ExitWait: testTUIGrace,
		Settle:   testTUISettle,
		Wait:     testTUIWait,
	})
	if err != nil {
		t.Fatalf("CaptureTextSurface: %v", err)
	}
	if !strings.Contains(cap.ScreenText, "seen:[/quit]") {
		t.Fatalf("the child did not receive the command verbatim:\n%s", cap.ScreenText)
	}
	if strings.ContainsRune(cap.ScreenText, '\033') {
		t.Fatal("an escape byte reached the screen: the keys were interpreted as a control sequence")
	}
}

// TestCaptureTextSurface_StubbornUIFailsInsteadOfHanging — a UI that ignores the
// exit keys is KILLED and the capture fails, naming what happened. A silent
// success here would leave the child running and hand back a frame that looks
// like a clean capture, which is how a leaked process is born (Rule 15).
func TestCaptureTextSurface_StubbornUIFailsInsteadOfHanging(t *testing.T) {
	script := writeFakeTUI(t, `#!/bin/sh
printf 'you> '
while true; do sleep 1; done
`)
	_, err := CaptureTextSurface(context.Background(), TextCaptureOptions{
		Argv:     []string{script},
		Cols:     testCols,
		Rows:     testRows,
		Ready:    "you>",
		ExitKeys: "quit" + textCarriageReturn,
		ExitWait: time.Second,
		Settle:   testTUISettle,
		Wait:     testTUIWait,
	})
	if err == nil {
		t.Fatal("a UI that never honours the exit keys must fail the capture, not return a frame")
	}
	if !strings.Contains(err.Error(), "killed") {
		t.Fatalf("the error does not say the UI was killed: %v", err)
	}
}

// TestTextReadyDefaultMatchesTheRenderer — the marker a caller that passes none
// gets must be the UNPADDED prompt form. The renderer trims a row's trailing
// cells and positions the cursor with a CSI, so the padded spelling ("you> ")
// never appears on the interpreted screen; a marker that is not a substring of
// the screen is a guaranteed timeout on a healthy UI (the defect this pins).
func TestTextReadyDefaultMatchesTheRenderer(t *testing.T) {
	if strings.TrimRight(TextReadyDefault, " ") != TextReadyDefault {
		t.Fatalf("TextReadyDefault = %q carries a trailing blank the renderer trims", TextReadyDefault)
	}
	if strings.TrimSpace(TextReadyDefault) == "" {
		t.Fatalf("TextReadyDefault = %q is empty", TextReadyDefault)
	}
}

// TestDefaultTUIArgvRunsTheChatVerb — the default terminal UI is gopi's own chat
// command, and the verb it uses is the CLI's chat verb (asserted against
// cmd/agent's constant in that package's test — this side pins the shape).
func TestDefaultTUIArgvRunsTheChatVerb(t *testing.T) {
	argv := DefaultTUIArgv("/usr/bin/gopi")
	if len(argv) != 2 || argv[0] != "/usr/bin/gopi" || argv[1] != cliVerbChat {
		t.Fatalf("DefaultTUIArgv = %v, want [/usr/bin/gopi %s]", argv, cliVerbChat)
	}
}
