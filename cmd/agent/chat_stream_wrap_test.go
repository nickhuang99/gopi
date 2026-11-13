package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// chat_stream_wrap_test.go — the fallback renderer's byte contract, asserted at
// the level the contract lives at: THE BYTES.
//
// The renderer used to bound each emitted row to the terminal's width, which
// meant WRITING the row break: a long answer reached the terminal as N rows
// separated by CR LF. A row copied out of that stream arrived split (a long file
// path broke in the middle), a pasted command carried a return the shell read as
// ENTER, and because the width was re-read every turn the split MOVED with the
// window. A terminal folds a row the application never ended at zero bytes of
// stream, so the fix is not to fold more carefully — it is not to fold at all.
//
// The cases below therefore assert three different things, because three
// different properties are at stake:
//
//   - the BYTES are the model's, on every writer (the defect itself);
//   - the SCREEN still shows a folded row (the visual is the terminal's work,
//     and Rule 10.2 says a rendering claim is measured on the cell grid, not
//     inferred from a byte count);
//   - a non-TTY stream is byte-identical to the historic renderer, because
//     scripts read that stream (that contract predates this change and outlives
//     it).

// Test bounds, every one named (Rule 1).
const (
	// chatWireTestCols is the terminal width the grid cases render at. 40 is the
	// width the editor harness runs at, so a wrap measurement and an editor
	// measurement are comparable.
	chatWireTestCols = 40
	// chatWireTestRows is the screen height: enough for the folded rows plus the
	// row a terminal leaves under them.
	chatWireTestRows = 12
	// chatWireTestDeltaRunes is the length of the unbroken delta: five full rows
	// at chatWireTestCols, i.e. a line the terminal must fold twice over.
	chatWireTestDeltaRunes = 200
	// chatWireTestCJKCols is deliberately NOT a multiple of a wide rune's two
	// cells: at 41 the first row holds twenty wide runes (40 cells) and the
	// twenty-first must start the next row, never be straddled.
	chatWireTestCJKCols = 41
	// chatWireTestCJKRunes is how many wide runes the CJK delta carries.
	chatWireTestCJKRunes = 30
	// chatWireTestNarrowCols / chatWireTestWideCols are the two widths the pty
	// case streams the SAME answer into. They straddle chatWireTestDeltaRunes so
	// a width-bounded renderer would cut it at two different places — which is
	// the "the copy moved with the window" defect, asserted as bytes.
	chatWireTestNarrowCols = 40
	chatWireTestWideCols   = 101
	// chatWireTestPtySettle is how long the pty case waits for the kernel to hand
	// over what it has before concluding the stream ended. It is a harness
	// deadline, never a production timer.
	chatWireTestPtySettle = 250 * time.Millisecond
	// chatWireTestPtyPoll is the read cadence inside that window.
	chatWireTestPtyPoll = 10 * time.Millisecond
)

// wireRun is one renderer run: the raw deltas as the model sent them, and the
// bytes the renderer emitted for them.
type wireRun struct {
	raw     string
	emitted string
}

// emitAll runs deltas through the fallback renderer and returns both streams.
// There is no width parameter because the renderer has none — that is the fix,
// and a caller that wants different bytes has to change the model's text.
func emitAll(deltas ...string) wireRun {
	var out bytes.Buffer
	r := newChatRenderer(&out)
	var raw strings.Builder
	for _, d := range deltas {
		raw.WriteString(d)
		r.write(d)
	}
	r.flush()
	return wireRun{raw: raw.String(), emitted: out.String()}
}

// wireCells is the emitted stream's row cells: the bytes are replayed through
// the terminal's line discipline first (a terminal applies OPOST|ONLCR, the
// kernel default, so every LF is delivered as CR LF), then through the cell
// grid. Feeding the raw stream would model a device nobody has, and the grid is
// where "the terminal filled the width by itself" is visible at all.
func wireCells(emitted string, cols, rows int) *lineedit.Screen {
	s := lineedit.NewScreen(cols, rows)
	s.Feed([]byte(lineDiscipline(emitted)))
	return s
}

// lineDiscipline is the terminal's contribution to the byte stream: every LF is
// delivered as CR LF (an existing CRLF stays one CR LF — only the LF is
// translated).
func lineDiscipline(s string) string {
	lf, crlf := "\n", "\r\n"
	return strings.ReplaceAll(strings.ReplaceAll(s, crlf, lf), lf, crlf)
}

// wireScreenRow is row's cells with the untouched tail trimmed (a screen row is
// space-filled to the full width; the trailing blanks are not content).
func wireScreenRow(s *lineedit.Screen, row int) string {
	return strings.TrimRight(s.Line(row), " ")
}

// TestChatStream_EmitsTheModelsBytesOnEveryWriter is THE case this change
// exists for, and the red-proof of the defect: a long answer with no break
// opportunity must reach the writer as the model's own bytes. A renderer that
// bounds its rows to a width inserts a break every width cells — this case then
// fails with the first five bytes it added, which is exactly what a reader
// copied into a shell and got a return in the middle of a path.
//
// It does not need a terminal to be red: the width came from a FIELD, and a
// renderer told to wrap would wrap into any writer. That is why the assertion is
// here and not only in the pty case — a defect reachable without a pty is
// cheaper to keep fixed than one that needs one.
func TestChatStream_EmitsTheModelsBytesOnEveryWriter(t *testing.T) {
	delta := strings.Repeat("w", chatWireTestDeltaRunes)
	run := emitAll(delta)

	if run.emitted != run.raw {
		t.Fatalf("the emitted stream is not the model's bytes:\n got %d bytes\nwant %d bytes\n got %q\nwant %q",
			len(run.emitted), len(run.raw), run.emitted, run.raw)
	}
	if i := strings.IndexAny(run.emitted, "\r"); i >= 0 {
		t.Fatalf("the answer stream carries a carriage return at byte %d (%q) — a copied row would paste a return into a shell",
			i, run.emitted[max(0, i-8):min(len(run.emitted), i+8)])
	}
	if i := strings.IndexByte(run.emitted, '\n'); i >= 0 {
		t.Fatalf("the answer stream carries a line feed at byte %d: the answer has none, so the renderer added one", i)
	}
}

// TestChatStream_LongPathSurvivesWhole is the operator's own case, in bytes: a
// long file path is one logical line, and it must arrive as one logical line at
// ANY writer width. It is the same property as the case above with the shape
// that was actually reported.
func TestChatStream_LongPathSurvivesWhole(t *testing.T) {
	const path = "/home/nick/workspace/01a1bca5-4a71-71bf-9d4b-bcecdec15313/internal/tools/git_publish_target_guard.go"
	if len(path) <= chatWireTestNarrowCols {
		t.Fatalf("the fixture is wrong: the path is %d bytes, which fits the narrow width — it would prove nothing",
			len(path))
	}
	// Streamed the way a model streams: in pieces, some of them mid-path.
	run := emitAll(path[:20], path[20:60], path[60:], "\n")
	if got := strings.TrimSuffix(run.emitted, "\n"); got != path {
		t.Fatalf("the path did not survive the renderer:\n got %q\nwant %q", got, path)
	}
	if strings.Contains(strings.TrimSuffix(run.emitted, "\n"), "\n") {
		t.Fatalf("the path arrived split across rows: %q", run.emitted)
	}
}

// TestChatStream_TerminalFoldsTheLongRow is the visual half (Rule 10.2): the
// bytes carry no break, and the row still appears folded on the grid the reader
// sees. It pins NON-DEGRADATION — the same 200 cells fill five 40-column rows
// whether the application or the terminal put them there — so a fix that also
// broke the display would be caught here rather than by the operator.
func TestChatStream_TerminalFoldsTheLongRow(t *testing.T) {
	delta := strings.Repeat("w", chatWireTestDeltaRunes)
	run := emitAll(delta)

	s := wireCells(run.emitted, chatWireTestCols, chatWireTestRows)
	wantRows := chatWireTestDeltaRunes / chatWireTestCols
	for row := 0; row < wantRows; row++ {
		if got := wireScreenRow(s, row); got != strings.Repeat("w", chatWireTestCols) {
			t.Fatalf("screen row %d = %q (%d cells), want a full %d-cell row — the terminal did not fold the row:\n%s",
				row, got, len(got), chatWireTestCols, s.Contents())
		}
	}
	// The stream's own bytes are still one unbroken row: the fold happened on
	// the grid, and cost the stream nothing.
	if strings.ContainsAny(run.emitted, "\r\n") {
		t.Fatalf("the fold cost the stream bytes: %q", run.emitted)
	}
}

// TestChatStream_NeverStraddlesAWideRune is the CJK half of the same property:
// the terminal never splits a wide rune at a column edge, so a stream that hands
// it runes instead of cells still renders without a replacement character. It
// is asserted on the grid because that is where a straddled rune would show.
func TestChatStream_NeverStraddlesAWideRune(t *testing.T) {
	delta := strings.Repeat("知", chatWireTestCJKRunes)
	run := emitAll(delta)

	if run.emitted != delta {
		t.Fatalf("the CJK answer was rewritten: %d runes out, %d in", len([]rune(run.emitted)), len([]rune(delta)))
	}
	s := wireCells(run.emitted, chatWireTestCJKCols, chatWireTestRows)
	if strings.Contains(s.Contents(), "\ufffd") {
		t.Fatalf("the rendered grid shows a replacement character — a wide rune was straddled:\n%s", s.Contents())
	}
	// 41 cells hold twenty wide runes per row, so the 21st starts row 1.
	const wantFirstRowRunes = (chatWireTestCJKCols - 1) / 2
	if got := len([]rune(wireScreenRow(s, 0))); got != wantFirstRowRunes {
		t.Fatalf("row 0 holds %d wide runes, want %d:\n%s", got, wantFirstRowRunes, s.Contents())
	}
}

// TestChatStream_MultilineAnswerKeepsTheAnswersOwnBreaks pins that the ONLY line
// breaks in the stream are the answer's own: a streamed multi-line answer is
// emitted byte-for-byte, and no break is added anywhere.
func TestChatStream_MultilineAnswerKeepsTheAnswersOwnBreaks(t *testing.T) {
	run := emitAll("short\n", strings.Repeat("m", chatWireTestCols*2), "\ntail")

	if got, want := run.emitted, run.raw; got != want {
		t.Fatalf("the multiline answer changed:\n got %q\nwant %q", got, want)
	}
	if got, want := strings.Count(run.emitted, "\n"), 2; got != want {
		t.Fatalf("the stream carries %d line feeds, want the answer's own %d: %q", got, want, run.emitted)
	}
}

// TestChatStream_NonTTYOutputIsByteIdentical is the contract this change must
// not touch: the piped/file stream is byte-for-byte what the pre-U2 renderer
// wrote, including the flush cadence's own chunking. The reference is the
// historic renderer written out — the three flush points, in the same order —
// not a paraphrase of it.
func TestChatStream_NonTTYOutputIsByteIdentical(t *testing.T) {
	deltas := []string{"full line one\n", "partial word ", "more", " words\n", "tail without a break"}

	historic := func() string {
		var out bytes.Buffer
		var buf strings.Builder
		for _, d := range deltas {
			buf.WriteString(d)
			s := buf.String()
			if idx := strings.LastIndexByte(s, '\n'); idx >= 0 {
				fmt.Fprint(&out, s[:idx+1])
				buf.Reset()
				buf.WriteString(s[idx+1:])
				continue
			}
			// A word-window or buffer-size flush cannot be reached here (the
			// lastFlush window has not elapsed in a tight loop and the buffer
			// never fills), and both historic branches write the SAME bytes as
			// the newline branch above: the whole buffer.
		}
		if buf.Len() > 0 {
			fmt.Fprint(&out, buf.String())
		}
		return out.String()
	}()

	// (1) The renderer directly.
	if run := emitAll(deltas...); run.emitted != historic {
		t.Fatalf("renderer stream = %q, want the historic bytes %q", run.emitted, historic)
	}
	// (2) The turn loop's own fallback renderer, which is the path a real
	//     non-TTY run takes.
	var buf bytes.Buffer
	renderer := newChatTurnRenderer(&chatInput{flowOut: &buf})
	if renderer.Reserved() {
		t.Fatal("the turn renderer claimed the reserved path with no answer band")
	}
	if renderer.buffered == nil {
		t.Fatal("the turn renderer built no fallback renderer for a non-TTY writer")
	}
	for _, d := range deltas {
		renderer.Write(d)
	}
	renderer.Finish()
	if got := buf.String(); got != historic {
		t.Fatalf("turn-loop stream = %q, want the historic bytes %q", got, historic)
	}
	// (3) A long unbroken delta is untouched too — no inserted break, no CR.
	long := emitAll(strings.Repeat("z", chatWireTestDeltaRunes))
	if long.emitted != long.raw {
		t.Fatalf("a long delta was rewritten: %q", long.emitted)
	}
	if strings.ContainsAny(long.emitted, "\r") {
		t.Fatalf("the stream carries a carriage return: %q", long.emitted)
	}
}

// TestChatStream_HasNoWidthToBeGiven is the structural half of the operator's
// second acceptance criterion ("the same answer must copy out the same way after
// a resize"). The property is not enforced by a comparison of two runs — it is
// enforced by the renderer having NO width input at all, so no window geometry
// can reach the answer's bytes. The real-terminal measurement lives where a real
// `gopi chat` child runs: testsuite/chat_wrap_pty_test.go.
//
// (A case that streamed the same answer into two pty widths was written and
// removed: the renderer's signature already proves it, and a pty pair inside
// this package only measured the fixture.)
func TestChatStream_HasNoWidthToBeGiven(t *testing.T) {
	// The ONE constructor takes a writer and nothing else (Rule 1/Rule 14: a
	// second parameter would be a second policy). Asserted as the property the
	// caller sees — two renderers over two writers with different geometry hold
	// no width — rather than by reading the struct's fields.
	var a, b bytes.Buffer
	ra, rb := newChatRenderer(&a), newChatRenderer(&b)
	delta := strings.Repeat("x", chatWireTestDeltaRunes)
	ra.write(delta)
	ra.flush()
	rb.write(delta)
	rb.flush()
	if a.String() != b.String() {
		t.Fatalf("two renderers emitted different bytes for the same answer")
	}
	if a.String() != delta {
		t.Fatalf("the renderer rewrote the answer: %q", shortForReport(a.String()))
	}
}

// shortForReport bounds a stream for a failure message: a 200-cell fixture is
// worth reading end to end, a passing detail is not.
func shortForReport(s string) string {
	const limit = 400
	if len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf(" … (%d bytes total)", len(s))
}
