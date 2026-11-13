package testsuite

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"golang.org/x/sys/unix"
)

// chat_wrap_pty_test.go — the answer stream, end to end on REAL terminals.
//
// The property under test is the operator's: a long answer must reach the
// terminal as the MODEL'S BYTES, on a wide terminal and a narrow one alike, so
// the same logical line copies out the same way before and after a resize. A
// terminal folds a row the application never ended at zero bytes of stream, so
// the screen still shows the fold while the bytes carry none of it.
//
// Both halves are measured on the wire: the bytes the child actually wrote to
// the pty (found verbatim, or not at all), and the cell grid those bytes paint
// (where the fold is visible). The geometry is installed with TIOCSWINSZ and the
// columns the child could read are asserted against the same ioctl, so the case
// cannot pass on a width nobody set.

// The case's own bounds, every one named (Rule 1).
const (
	// chatWireSuiteNarrowCols / chatWireSuiteWideCols are the two geometries the
	// SAME answer is streamed into. They are far apart on purpose: an
	// application that bounded its rows would break the answer at two different
	// places, which is exactly the "the copy moved with the window" defect this
	// case measures as bytes.
	chatWireSuiteNarrowCols = 40
	chatWireSuiteWideCols   = 101
	// chatWireSuiteRows is the grid height the fold is rendered on: tall enough
	// for the chrome plus every folded row of the answer.
	chatWireSuiteRows = 24
	// chatWireSuiteTail ends the answer on a line of its own. Without it the
	// harness cannot tell a finished answer from a partial one.
	chatWireSuiteTail = "-end-"
	// chatWireSuitePathCopies is how many times the fixture path is repeated:
	// two copies make the line longer than the WIDE terminal too, so both runs
	// fold.
	chatWireSuitePathCopies = 2
)

// chatWireSuitePath is the answer's payload: a real, long, space-free file path,
// repeated so the line also exceeds the WIDE terminal. It is the shape that was
// reported (a long path broken in the middle by a CR LF).
const chatWireSuitePath = "/home/nick/workspace/01a1bca5-4a71-71bf-9d4b-bcecdec15313/internal/tools/git_publish_target_guard.go"

// chatWireSuiteAnswer is one logical line: no space, no newline, longer than the
// wide terminal.
func chatWireSuiteAnswer() string {
	return strings.TrimSuffix(strings.Repeat(chatWireSuitePath+" ", chatWireSuitePathCopies), " ")
}

// TestChatPTYAnswer_CarriesNoAppWrapBytes is the acceptance case: on two real
// `gopi chat` sessions of different widths, the long logical line arrives
// VERBATIM — so the copy a user takes out of the terminal has no return and no
// mid-line break in it — and the two sessions deliver the same bytes.
//
// The verbatim search IS the byte comparison: a stream that inserted CR LF every
// N cells cannot contain the line, and one that did not contains it whole. A
// case that compared only two hashes could pass while both were broken the same
// way.
func TestChatPTYAnswer_CarriesNoAppWrapBytes(t *testing.T) {
	answer := chatWireSuiteAnswer()
	if len(answer) <= chatWireSuiteWideCols {
		t.Fatalf("the fixture is wrong: %d bytes does not exceed the wide width (%d), so the wide run would fold nothing",
			len(answer), chatWireSuiteWideCols)
	}

	narrow := runChatAnswerOnPty(t, chatWireSuiteNarrowCols, answer)
	wide := runChatAnswerOnPty(t, chatWireSuiteWideCols, answer)

	// (1) The line is there, whole and unbroken, at BOTH widths.
	for _, run := range []ptyAnswerRun{narrow, wide} {
		if !strings.Contains(run.output, answer) {
			t.Fatalf("at %d columns the child did not deliver the answer as one unbroken line.\n"+
				"A stream that breaks a row writes CR LF into the answer, so the line cannot be found.\n--- captured (%d bytes) ---\n%s",
				run.cols, len(run.output), chatWireSuiteExcerpt(run.output, answer))
		}
		// (2) No carriage return anywhere in the answer's own region.
		start := strings.Index(run.output, answer)
		if i := strings.IndexByte(run.output[start:start+len(answer)], '\r'); i >= 0 {
			t.Fatalf("at %d columns the answer region carries a carriage return at offset %d", run.cols, i)
		}
	}

	// (3) The two widths delivered the SAME bytes for the ANSWER: the exact
	//     bytes the model sent, at both geometries. This is the resize half of
	//     the acceptance — before the fix the answer was cut every `cols` cells,
	//     so the same answer arrived as different bytes at 40 and 101 columns.
	if narrow.span != wide.span {
		t.Fatalf("the answer's bytes differ between %d and %d columns:\n narrow %q\n   wide %q",
			narrow.cols, wide.cols, narrow.span, wide.span)
	}
	if narrow.span != answer {
		t.Fatalf("the answer arrived as %q, want the model's own bytes %q", shortSpan(narrow.span), shortSpan(answer))
	}
	if i := strings.IndexByte(narrow.span, '\r'); i >= 0 {
		t.Fatalf("the answer carries a carriage return at byte %d of %d", i, len(narrow.span))
	}
	if i := strings.IndexByte(narrow.span, '\n'); i >= 0 {
		t.Fatalf("the answer carries a line feed at byte %d of %d — the model sent none", i, len(narrow.span))
	}
	if !narrow.lineEndsAfterAnswer {
		t.Fatalf("the answer's line does not END after it: the byte after the answer is not the terminal's CR LF")
	}
	t.Logf("the answer's %d bytes are identical at %d and %d columns, with no CR and no interior LF",
		len(narrow.span), narrow.cols, wide.cols)

	// (4) Non-degradation: the terminal still FOLDS the row — the answer's first
	//     copy occupies consecutive grid rows of exactly the terminal's width.
	for _, run := range []ptyAnswerRun{narrow, wide} {
		rows, ok := chatWireSuiteFoldRows(run.output, answer, run.cols)
		if !ok {
			t.Fatalf("at %d columns the answer does not appear folded across consecutive rows — the row was not displayed:\n%s",
				run.cols, chatWireSuiteExcerpt(run.output, answer))
		}
		wantRows := (len(answer) + run.cols - 1) / run.cols
		if rows != wantRows {
			t.Fatalf("at %d columns the answer occupies %d grid rows, want %d (ceil(%d/%d))",
				run.cols, rows, wantRows, len(answer), run.cols)
		}
		t.Logf("at %d columns the answer is folded across %d grid rows, each the next %d bytes",
			run.cols, rows, run.cols)
	}
}

// shortSpan bounds a stream for a failure message.
func shortSpan(s string) string {
	const limit = 400
	if len(s) <= limit {
		return s
	}
	return s[:limit] + " …"
}

// ptyAnswerRun is one `gopi chat` session's result: everything the child wrote
// to the pty, and the width the pty was set to for that run.
type ptyAnswerRun struct {
	output string
	// span is the bytes from the answer's first byte to the tail marker that
	// ends it: the region the renderer owns, with the width-shaped chrome
	// excluded.
	span string
	// lineEndsAfterAnswer records that the byte right after the answer's own
	// bytes is the terminal's CR LF for the line feed the model ended the line
	// with — i.e. the line ended where the ANSWER ended it, not mid-way.
	lineEndsAfterAnswer bool
	cols                int
}

// runChatAnswerOnPty asks a real `gopi chat` child (on a pty of the given width)
// for a scripted answer and returns everything the terminal received. It is the
// same harness every other chat pty case uses — the child is the real binary and
// stdout IS a terminal, so the renderer under test is the one a user gets.
func runChatAnswerOnPty(t *testing.T, cols int, answer string) ptyAnswerRun {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: answer + "\n" + chatWireSuiteTail})
	defer llm.Close()

	// --no-editor: no reserved answer band, so the historic buffered renderer
	// owns the answer — the path this run changes.
	chat := startPTYChat(t, []string{"chat", "--no-editor"}, testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: uint16(cols), Row: chatWireSuiteRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable: %v", err)
	}
	if got := lineedit.TerminalCols(int(chat.master.Fd())); got != cols {
		t.Fatalf("the pty reports %d columns, want the %d this case installed", got, cols)
	}

	out := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	chat.write(t, "stream a long answer\r")
	out += chat.waitFor(t, chatWireSuiteTail, ptyPromptTimeout)

	chat.write(t, "/quit\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0; output so far:\n%s", code, out)
	}
	run := answerSpan(t, out, answer)
	return ptyAnswerRun{output: out, span: run.span, lineEndsAfterAnswer: run.lineEnds, cols: cols}
}

// answerSpanResult is what answerSpan reads: the answer's own bytes as they
// reached the terminal, and whether the terminal ended the line right after them.
type answerSpanResult struct {
	span     string
	lineEnds bool
}

// answerSpan cuts the ANSWER out of a session's stream: exactly the model's
// bytes, located by the answer's own leading bytes. Anything after them (a REPL
// notice, the tail marker) is chrome and is not part of this reading.
func answerSpan(t *testing.T, output, answer string) answerSpanResult {
	t.Helper()
	start := strings.Index(output, answer[:24])
	if start < 0 {
		t.Fatalf("the answer never reached the terminal; captured %d bytes:%s",
			len(output), chatWireSuiteExcerpt(output, answer))
	}
	end := start + len(answer)
	if end > len(output) {
		t.Fatalf("the stream ended inside the answer (%d of %d bytes)", len(output)-start, len(answer))
	}
	rest := output[end:]
	return answerSpanResult{span: output[start:end], lineEnds: strings.HasPrefix(rest, "\r\n")}
}

// chatWireSuiteFoldRows replays the child's stream through the cell grid and
// reports how many CONSECUTIVE rows the ANSWER occupies, each one exactly the
// next cols bytes of it — that is what a fold is, and nothing else produces it.
// ok=false when no row starts the answer or the rows are not consecutive.
//
// THIS IS THE NON-DEGRADATION HALF. The bytes carry no break (the assertions
// above), and the reader must still see the row folded — the fold is the
// TERMINAL's work, so a fix that also stopped the display fails here.
//
// A row's trailing blanks are not content (the grid space-fills every cell), so
// both sides are trimmed before they are compared: the answer's own separator
// blank at a row edge is a cell the fold consumed, not a byte that went missing
// (the byte-level assertion above is what proves nothing was lost).
func chatWireSuiteFoldRows(output, answer string, cols int) (int, bool) {
	screen := lineedit.NewScreen(cols, chatWireSuiteRows+len(answer)/cols+chatWireSuiteRows)
	screen.Feed([]byte(output))
	lines := strings.Split(screen.Contents(), "\n")
	row := func(i int) string { return strings.TrimRight(lines[i], " ") }
	start := -1
	for i := range lines {
		if row(i) == strings.TrimRight(answer[:cols], " ") {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, false
	}
	rows := 0
	for want := 0; want*cols < len(answer); want++ {
		idx := start + want
		if idx >= len(lines) {
			return rows, false
		}
		end := (want + 1) * cols
		if end > len(answer) {
			end = len(answer)
		}
		if row(idx) != strings.TrimRight(answer[want*cols:end], " ") {
			return rows, false
		}
		rows++
	}
	return rows, true
}

// chatWireSuiteExcerpt bounds a captured stream for a failure message, keeping
// the region around the answer: the whole pty stream of a chat session is mostly
// chrome and would bury the defect.
func chatWireSuiteExcerpt(output, answer string) string {
	const contextBytes = 240
	idx := strings.Index(output, answer[:24])
	if idx < 0 {
		if len(output) <= contextBytes {
			return output
		}
		return output[:contextBytes] + fmt.Sprintf(" … (%d bytes captured in total)", len(output))
	}
	from := idx - contextBytes
	if from < 0 {
		from = 0
	}
	to := idx + len(answer) + contextBytes
	if to > len(output) {
		to = len(output)
	}
	return fmt.Sprintf("… [%d] %q …", from, output[from:to])
}
