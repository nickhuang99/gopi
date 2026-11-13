package gopilog

// jsonlline.go — the ONE JSONL line reader for session files (Rule 14: one
// implementation for reader.go, rawlog.go and stats.go, which all scan the
// same files).
//
// THE DEFECT IT CLOSES (binary-briefing defect, 2026-09-15): a session whose
// accepted prompt was a binary file read as prompt TEXT (measured: the run's
// ./gopi ELF executable, `gopi gopi help`) does not stay small — the model
// echoes the captured bytes back, and the resulting session.jsonl held ONE
// 87,618,404-byte line (measured: ~/.gopi/sessions/a3d34ed1-…/session.jsonl,
// line 2, 28× the next largest line; its content alone is 21,690,123 bytes
// carrying 3,372,545 NULs). The old reader capped a line at 10 MiB
// (scannerMaxBufSize, since removed), so EVERY verb on that session — `gopi
// log <id>`, `--todo`, `--last`, `gopi session stats --cost` — died with bufio's bare
// "bufio.Scanner: token too long": the session became uninspectable, which is
// precisely when an operator needs to inspect it.
//
// DECISION (deliberate, in the fix rather than in a comment elsewhere):
//  1. STREAM the line instead of capping it. A session line's length is set by
//     the model's own output, which nothing in this process can bound — a cap
//     is therefore always a latent "this legitimate session cannot be
//     inspected" bug, only with a different number. The reader accumulates the
//     line itself (bufio.Reader.ReadSlice + append), so an 87 MB line is read
//     exactly like a 200-byte one.
//  2. KEEP a safety ceiling, because "unbounded" is not a design: a corrupt or
//     hostile file must not drive this process out of memory. The ceiling is
//     jsonlLineSafetyCeiling (1 GiB, ~12× the largest session line ever
//     measured here), documented as a MEMORY-EXHAUSTION guard, not a content
//     policy.
//  3. NEVER fail silently or anonymously: exceeding the ceiling, or any read
//     error, is reported as jsonlLineTooLongError / a wrapped error carrying
//     the file, the 1-based line number, the byte offset where that line
//     starts and the ceiling itself. bufio's bare "token too long" (no file,
//     no line, no number, indistinguishable from a truncated read) is never
//     surfaced (Rule 15: an error must say what happened — the operator's next
//     action is to look at that offset in that file).
//
// SCOPE OF THE DECISION: session JSONL files (reader.go, stats.go and the two
// session-file probes in rawlog.go) stream with jsonlLineSafetyCeiling. The
// RAW HTTP log family keeps its own already-documented cap
// (rawScannerMaxBufSize, rawlog.go — a different file format, whose request
// bodies are legitimately large), but it is streamed by this same reader, so
// it now fails with the same named, positioned error rather than bufio's bare
// text.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// jsonlLineSafetyCeiling is the largest single JSONL line this reader will
// hold. It is a MEMORY guard (see the decision above), not a content policy:
// the largest session line measured on this host is 87,618,404 bytes, so this
// is ~12× headroom, and a line beyond it means the file is not what a session
// JSONL is.
const jsonlLineSafetyCeiling = 1 << 30 // 1 GiB

// jsonlScanner reads a session JSONL line by line with an UNBOUNDED line
// length (up to the safety ceiling) and an exact position, so an over-ceiling
// line is reported with its byte offset instead of bufio's bare
// "token too long".
//
// ceiling is a parameter rather than a direct read of the constant so a test
// can exercise the over-ceiling path on a small file (a real >1 GiB fixture is
// not writable in a test); production passes jsonlLineSafetyCeiling.
type jsonlScanner struct {
	br      *bufio.Reader
	path    string
	ceiling int
	line    []byte // accumulated current line (no trailing newline)
	lines   int64  // lines returned so far
	off     int64  // bytes consumed from the stream (start offset of the NEXT line)
	// lineStart is the byte offset where the CURRENT line begins (jsonl:
	// `off` at the top of Scan). It is what an error must report — the
	// position of the line that could not be read, not of the one after it.
	lineStart int64
	// unterminated marks the CURRENT line as one the stream ended inside: the
	// file's last line had no trailing '\n'. Scan can only ever set this for
	// the FINAL line (a '\n' ends every other line), so it is exactly the
	// condition the torn-tail classification keys on (see reader.go's
	// classifyLineReadError): a line that is both unterminated and
	// unparsable is a TRUNCATED TAIL, while the same parse failure on a
	// terminated line is mid-file corruption. Those two have different causes
	// and different repairs, and reporting them with one anonymous sentence was
	// the defect this flag exists to close.
	unterminated bool
	err          error
}

func newJSONLScanner(path string, r io.Reader, ceiling int) *jsonlScanner {
	return &jsonlScanner{
		br:      bufio.NewReaderSize(r, scannerInitBufSize),
		path:    path,
		ceiling: ceiling,
		// The line accumulator grows on demand — pre-allocating the reader's
		// whole window per file was the second half of the same
		// read-amplification cost (one 1 MiB allocation for every session file
		// a listing touches, including the vast majority it only reads the
		// session_start line of).
		line: make([]byte, 0, scannerInitBufSize),
	}
}

// Scan advances to the next line. False means either EOF or a terminal error
// (Err() then reports it).
func (j *jsonlScanner) Scan() bool {
	if j.err != nil {
		return false
	}
	j.line = j.line[:0]
	startOff := j.off
	j.lineStart = startOff
	j.unterminated = false
	for {
		chunk, err := j.br.ReadSlice('\n')
		complete := err == nil
		if complete {
			chunk = chunk[:len(chunk)-1] // drop the newline the reader kept
		}
		j.off += int64(len(chunk)) + int64(boolToInt(complete))
		j.line = append(j.line, chunk...)
		// The ceiling is enforced on the ACCUMULATED line, not on the reader's
		// chunk: bufio's buffer size must not decide which files are readable.
		if len(j.line) > j.ceiling {
			j.err = &jsonlLineTooLongError{
				Path:     j.path,
				Line:     j.lines + 1,
				Offset:   startOff,
				CapBytes: j.ceiling,
				// The bytes accumulated when the ceiling tripped: the line is
				// at least this long (the reader stops reading it here), which
				// is the order of magnitude the operator needs.
				ObservedBytes: len(j.line),
			}
			return false
		}
		switch {
		case complete:
			// A CRLF file would otherwise leave a '\r' on every line: a JSONL
			// line never ends in CR, so strip it here rather than at every call
			// site.
			if n := len(j.line); n > 0 && j.line[n-1] == '\r' {
				j.line = j.line[:n-1]
			}
			j.lines++
			return true
		case errors.Is(err, bufio.ErrBufferFull):
			continue // the line continues past this chunk
		case errors.Is(err, io.EOF):
			if len(j.line) == 0 {
				return false // clean EOF
			}
			// THE STREAM ENDED INSIDE THIS LINE: no terminator. It is still
			// returned as a line — a complete final line without a trailing
			// newline is legal — but the flag travels with it, so a caller that
			// cannot PARSE this line knows which of the two conditions it is
			// looking at.
			j.unterminated = true
			j.lines++
			return true // final line without a trailing newline
		default:
			j.err = fmt.Errorf("read %s: %w", j.path, err)
			return false
		}
	}
}

// boolToInt keeps the offset arithmetic above readable (Rule 1: no bare magic
// in an expression; a true is exactly the one newline byte the reader consumed).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Bytes is the current line, without its trailing newline. The slice is reused
// by the next Scan — callers that keep it must copy (json.Unmarshal reads it
// immediately, which is what every call site does).
func (j *jsonlScanner) Bytes() []byte { return j.line }

// LineStart is the byte offset where the CURRENT line begins — the position an
// error about that line must report.
func (j *jsonlScanner) LineStart() int64 { return j.lineStart }

// Unterminated reports whether the CURRENT line was the stream's last and had
// no trailing '\n'. It is the discriminator between a TORN TAIL (the writer was
// cut off mid-record) and mid-file corruption (a damaged record with intact
// lines after it), and it is only ever true for the final line.
func (j *jsonlScanner) Unterminated() bool { return j.unterminated }

// Err reports the terminal error: nil at a clean EOF, jsonlLineTooLongError
// when the safety ceiling was hit, else the read error wrapped with the path.
func (j *jsonlScanner) Err() error {
	if j.err != nil {
		return j.err
	}
	return nil
}

// jsonlLineTooLongError is the named, actionable form of an over-ceiling line:
// it carries everything the operator needs to act (which file, which line,
// where it starts, how large the ceiling is). The type is unexported — its
// consumer is the CLI's `Error: %v` print — and exists so a test can assert
// the SHAPE (file + byte count + ceiling) rather than match bufio's text.
type jsonlLineTooLongError struct {
	Path     string // the session file
	Line     int64  // 1-based number of the offending line
	Offset   int64  // byte offset where the offending line starts
	CapBytes int    // the ceiling that was exceeded
	// ObservedBytes is how many bytes of that line had accumulated when the
	// ceiling tripped — the ACTUAL byte count of the offending line (a lower
	// bound: the reader stops there), so the operator sees the magnitude
	// instead of only the limit.
	ObservedBytes int
}

func (e *jsonlLineTooLongError) Error() string {
	return fmt.Sprintf(
		"session file %s: line %d starting at byte %d is %d bytes, over the %d-byte per-line safety ceiling — the file is not readable as JSONL (raise jsonlLineSafetyCeiling in internal/gopilog/jsonlline.go if this is a legitimate session)",
		e.Path, e.Line, e.Offset, e.ObservedBytes, e.CapBytes)
}
