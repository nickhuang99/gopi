package gopilog

// Regression guards for the reader's line-length decision (see jsonlline.go).
//
// THE DEFECT: the same binary-prompt session whose renderers printed `data`
// (binaryprompt_render_test.go) also held its whole conversation on ONE
// 87,618,404-byte line — the model echoing the captured ELF bytes back. The old
// reader capped a line at 10 MiB, so every verb on that session died with
// bufio's bare "bufio.Scanner: token too long" and the session became
// uninspectable exactly when an operator needed to look at it.
//
// The fix is deliberate (jsonlline.go states it): STREAM the line (no content
// cap — a session line's length is the model's output, which this process
// cannot bound), keep a 1 GiB MEMORY-safety ceiling, and report an over-ceiling
// line as a named error carrying file + line + byte offset + ceiling.
//
// No large fixture is committed (the epic's constraint): the over-ceiling case
// is exercised by injecting a small ceiling, and the "a line beyond the OLD
// 10 MiB cap is read" case is generated at test time.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sessionevent"
)

// beyondFormerCapBytes is a line length comfortably past the OLD
// scannerMaxBufSize (10 MiB) — the size the real session exceeded.
const beyondFormerCapBytes = 12 << 20 // 12 MiB

// TestParseFileReadsLineBeyondFormerScannerCap is the acceptance case: a
// session line LARGER than the old 10 MiB cap is now read successfully instead
// of failing the whole file. The line is generated (not committed) and written
// as a real JSONL user row, so the test drives the identical path the CLI does.
func TestParseFileReadsLineBeyondFormerScannerCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	payload := strings.Repeat("知", beyondFormerCapBytes/3) // 3 bytes per rune
	writeJSONLine(t, path, userEvent(payload))
	writeJSONLine(t, path, assistantEvent("done"))

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile on a >10 MiB session line: %v (the old cap made this 'token too long')", err)
	}
	if got := len(s.Task); got < beyondFormerCapBytes {
		t.Fatalf("read back %d task bytes, want >= %d — the line was truncated", got, beyondFormerCapBytes)
	}
	// A huge line is still a line: the NEXT line must be read too.
	if len(s.Iterations) == 0 {
		t.Fatalf("events after the huge line were lost: %+v", s)
	}
	if !strings.Contains(FormatSession(s), "done") {
		t.Errorf("rendered session is missing the post-huge-line content")
	}
}

// TestParseFileOverCeilingErrorNamesFileLineOffsetAndByteCount is the
// no-silent-failure proof: when a line DOES exceed the (injected, small)
// ceiling, the error is a named jsonlLineTooLongError carrying the file, the
// 1-based line number, the byte offset where that line starts and the ceiling
// that was exceeded — never bufio's context-free "token too long".
func TestParseFileOverCeilingErrorNamesFileLineOffsetAndByteCount(t *testing.T) {
	const ceiling = 1 << 10 // 1 KiB, injected so no huge fixture is needed
	path := filepath.Join(t.TempDir(), "session.jsonl")

	ev := RawEvent{Type: sessionevent.EventTypeSessionStart}
	ev.SessionID = "s"
	writeJSONLine(t, path, ev)
	firstLen := jsonLineLen(t, path, 1)

	writeJSONLine(t, path, userEvent(strings.Repeat("x", ceiling*4)))
	writeJSONLine(t, path, assistantEvent("never reached"))

	_, err := parseFileCeiling(path, ceiling)
	if err == nil {
		t.Fatalf("a line over the ceiling must fail, not be skipped")
	}
	var tooLong *jsonlLineTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("error is not the named jsonlLineTooLongError: %T %v", err, err)
	}
	if tooLong.Path != path {
		t.Errorf("error path = %q, want %q", tooLong.Path, path)
	}
	if tooLong.Line != 2 {
		t.Errorf("error line = %d, want 2 (the oversized line)", tooLong.Line)
	}
	if want := int64(firstLen + 1); tooLong.Offset != want {
		t.Errorf("error offset = %d, want %d (start of line 2)", tooLong.Offset, want)
	}
	if tooLong.CapBytes != ceiling {
		t.Errorf("error ceiling = %d, want %d", tooLong.CapBytes, ceiling)
	}
	// The byte count the acceptance asks for: the error must carry how large
	// the offending line actually was (>= the ceiling that tripped, and equal
	// to what was accumulated when the reader stopped).
	if tooLong.ObservedBytes <= ceiling {
		t.Errorf("error observed bytes = %d, want > ceiling %d (the actual line size)", tooLong.ObservedBytes, ceiling)
	}
	// The operator-facing text must name the file, the line, the offset, the
	// ACTUAL byte count and the ceiling — and must NOT leak bufio's anonymous
	// wording.
	msg := err.Error()
	for _, want := range []string{
		path,
		"line 2",
		fmt.Sprintf("byte %d", tooLong.Offset),
		fmt.Sprintf("is %d bytes", tooLong.ObservedBytes),
		fmt.Sprintf("%d-byte", ceiling),
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q is missing %q", msg, want)
		}
	}
	if strings.Contains(msg, "token too long") {
		t.Errorf("error message still leaks bufio's bare wording: %q", msg)
	}
}

// TestJSONLScannerCountsLinesAndOffsets pins the position accounting directly
// (it is what makes the over-ceiling error actionable).
func TestJSONLScannerCountsLinesAndOffsets(t *testing.T) {
	body := "alpha\nbeta\n\ngamma"
	sc := newJSONLScanner("mem", strings.NewReader(body), jsonlLineSafetyCeiling)
	var got []string
	for sc.Scan() {
		got = append(got, string(sc.Bytes()))
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if want := []string{"alpha", "beta", "", "gamma"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("lines = %q, want %q", got, want)
	}
	// A file whose last line has no newline must yield that line (a truncated
	// tail is data, not an error).
	sc = newJSONLScanner("mem", strings.NewReader("only-line"), jsonlLineSafetyCeiling)
	if !sc.Scan() || string(sc.Bytes()) != "only-line" || sc.Scan() {
		t.Errorf("final unterminated line mishandled")
	}
	// CRLF files must not leave the CR on the line (JSON would still parse, but
	// the byte would reach renderers).
	sc = newJSONLScanner("mem", strings.NewReader("crlf\r\nnext"), jsonlLineSafetyCeiling)
	if !sc.Scan() || string(sc.Bytes()) != "crlf" {
		t.Errorf("CRLF line = %q, want %q", sc.Bytes(), "crlf")
	}
}

func jsonLineLen(t *testing.T, path string, n int) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) < n {
		t.Fatalf("file has %d lines, want >= %d", len(lines), n)
	}
	return len(lines[n-1])
}
