package gopilog

// torn_tail.go — a session record's LAST line that has no terminator and does
// not parse is a TORN TAIL, and it is reported as its own condition. This file
// exists so that the one place in the whole reader that CAN tell the two apart
// is also the one place that SAYS which one it found.
//
// THE MEASURED DEFECT. On this host three session records were left ending
// mid-record (1,286,144 / 512,000 / 405,504 bytes — all three 4096-byte
// aligned, the final byte a printable character from inside a JSON string; the
// torn fragments are 1,530 / 469 / 135 bytes). Before this file, every strict
// reader answered with the SAME sentence for them:
//
//	JSON parse error: unexpected end of JSON input
//
// That sentence is anonymous in three separate ways, and each one cost a real
// operator step:
//
//  1. no file — with `gopi session log --last` and `--all` a dozen candidate
//     records are read, and the sentence did not say which one was broken;
//  2. no position — "unexpected end of JSON input" is true of a 135-byte
//     fragment and of a 40 MB line alike, so the operator could not find the
//     damage without re-deriving the line offsets by hand;
//  3. no CONDITION — a truncated tail and a corrupt line in the middle of an
//     otherwise good record are different facts (one is "the writer was cut
//     off, write a new record"; the other is "this artifact is damaged, restore
//     it"), and they were indistinguishable.
//
// WHY "TORN" IS THE RIGHT WORD, and what actually produced the three files.
// The epic's hypothesis — a hard kill between write(2) and fsync leaving a
// partial page on disk — is REFUTED by the corpus itself. A page that is lost
// on power loss is zero-filled past i_size, so the damage would end in NULs at
// an ARBITRARY size; the measured files instead end on a printable byte from
// inside a JSON string, and their last structurally written byte lands exactly
// at i_size. What remains is a EINTR/short write at the writer's own seam: the
// writer's buffer is flushed in whole lines (sessionLogSyncThreshold flushes
// wholesale), so the only place a cut can fall inside a record is the kernel's
// write(2) returning a short count — which is what happens when the filesystem
// has no space left (measured: the three files' mtimes 18:16:20 / 18:16:42 /
// 18:16:55 fall inside an 8,504-line "No space left on device" window, journal
// boot -3). The writer's caller discarded that error (see
// internal/agent/session.go), so the FIRST symptom became this anonymous parse
// error in a LATER reader. Fixing the reader's vocabulary is half the repair;
// the other half is the append guard (internal/agent/session.go,
// refuseUnterminatedRecordTail), which stops the next run from turning a
// detectable truncation into permanent MID-FILE corruption by appending behind
// the unreachable fragment.
//
// STRICT REFUSAL STAYS THE DEFAULT. Neither error below is a warning: a reader
// that reported a torn tail and then rendered the file anyway would be silently
// dropping a line (Rule 15 / PROJECT.md's prohibition), and the operator would
// see a conversation with a hole in it. The file is refused; what changes is
// that the refusal now names the condition, the position, and how much of the
// record is still good.

import (
	"fmt"
	"os"
)

// TornTailError reports a session record whose final line was never terminated
// and does not parse: the writer was cut off mid-record. Everything an operator
// needs in order to act is carried here rather than inferred:
//
//   - Path — which record;
//   - Line / Offset — where the torn fragment starts, so the intact prefix can
//     be read with `head -c <Offset>`;
//   - TornBytes — how large the fragment is (the amount the writer failed to
//     complete);
//   - ReadableLines — how many complete lines precede it, i.e. what is still
//     readable;
//   - Unterminated — always true, and stated as a FIELD rather than buried in
//     the sentence, because it is the discriminator against
//     midFileCorruptionError: a caller (or a test) must be able to ask which
//     condition it holds without parsing English.
type TornTailError struct {
	Path          string // the session record
	Line          int64  // 1-based number of the torn (final) line
	Offset        int64  // byte offset where the torn fragment starts
	TornBytes     int    // bytes of the fragment (the part that never became a line)
	ReadableLines int64  // complete lines before it — what is still readable
	Unterminated  bool   // always true; the missing terminator IS the condition
	// Err is the underlying JSON error, kept so a caller can tell a truncated
	// STRING from a truncated OBJECT (they are both torn tails, but the
	// underlying decoder's message is evidence).
	Err error
}

// Error renders the condition, the position, the size and what survives. It is
// printed verbatim by every CLI surface (`Error parsing %s: %v`), so it must
// read as a diagnosis, not as a code.
func (e *TornTailError) Error() string {
	return fmt.Sprintf(
		"session file %s: TORN TAIL — the record's last line (line %d, starting at byte %d) has no trailing newline and its %d bytes do not parse (%v); the %d complete line(s) before it are intact and readable with `head -c %d`. This is a TRUNCATED RECORD (the writer was cut off mid-line), not mid-file corruption: %s",
		e.Path, e.Line, e.Offset, e.TornBytes, e.Err, e.ReadableLines, e.Offset,
		"a damaged line in the MIDDLE would leave later lines after it, and this record has none")
}

// Unwrap exposes the decoder's error (a truncated string, a truncated object),
// which is the finest-grained evidence about WHERE inside the fragment the
// bytes stopped.
func (e *TornTailError) Unwrap() error { return e.Err }

// midFileCorruptionError reports a line that failed to parse and is NOT the
// unterminated final line of its file: the record is damaged, and the lines
// after the damage are still there. It is the other half of the
// discrimination — the same decoder error, a different fact, and therefore a
// different repair — so it gets its own name, its own position, and its own
// sentence rather than sharing the anonymous one.
type midFileCorruptionError struct {
	Path   string
	Line   int64 // 1-based number of the corrupt line
	Offset int64 // byte offset where the corrupt line starts
	Bytes  int   // the corrupt line's length
	// LaterLines is how many COMPLETE lines follow the damage — the fact that
	// makes this "mid-file" and not "torn". It is measured, not assumed.
	LaterLines int64
	Err        error
}

func (e *midFileCorruptionError) Error() string {
	return fmt.Sprintf(
		"session file %s: mid-file corruption — line %d (starting at byte %d, %d bytes) is not valid JSON (%v) and %d complete line(s) FOLLOW it, so the record is damaged rather than truncated; the %d bytes before the damage are intact and readable with `head -c %d`",
		e.Path, e.Line, e.Offset, e.Bytes, e.Err, e.LaterLines, e.Offset, e.Offset)
}

func (e *midFileCorruptionError) Unwrap() error { return e.Err }

// classifyLineReadError turns one unparsable line into the condition it
// actually is. It is the ONLY caller-side place that knows the scanner's
// position and terminator flag, which is why the classification lives here and
// not in the JSON decoder: `json.Unmarshal` cannot see whether the bytes it was
// handed ended with the file's last byte or with a newline in the middle of a
// record, and that difference is the whole diagnosis.
//
// laterLines is the number of complete lines after the failing one; it is only
// consulted for the mid-file case (a torn tail, by definition, has none) and is
// computed by the caller that has already read the rest of the file.
func classifyLineReadError(path string, line int64, offset int64, lineBytes int, unterminated bool, laterLines int64, err error) error {
	if unterminated {
		return &TornTailError{
			Path:          path,
			Line:          line,
			Offset:        offset,
			TornBytes:     lineBytes,
			ReadableLines: line - 1,
			Unterminated:  true,
			Err:           err,
		}
	}
	return &midFileCorruptionError{
		Path:       path,
		Line:       line,
		Offset:     offset,
		Bytes:      lineBytes,
		LaterLines: laterLines,
		Err:        err,
	}
}

// countCompleteLinesAfter counts how many COMPLETE lines follow the line the
// scanner just returned. It is only asked for the mid-file case (a torn tail
// has none by definition), and it is what makes that classification MEASURED
// rather than assumed: the sentence "N complete lines follow it" is the fact an
// operator uses to decide between restoring the artifact and writing a new one.
//
// The scan is destructive by design — the caller is already returning an error,
// so nothing will read this scanner again.
func countCompleteLinesAfter(j *jsonlScanner) int64 {
	var n int64
	for j.Scan() {
		n++
	}
	return n
}

// warnLineUnreadable announces a row a TOLERANT reader skipped, in the SAME
// words the strict reader refuses the file with.
//
// WHY A TOLERANT READER MUST STILL TALK. `LoadAllSummaries` (and the two probes
// in rawlog.go) deliberately survive an unparsable row, because one damaged
// record must not blind a listing to every other session. That decision is
// right; what was wrong is that it made the skip INVISIBLE — the same torn file
// that `gopi session log <id>` refused with a sentence was absorbed by
// `session log --last` without a word, so the operator learned nothing unless
// they happened to open that one session. Silence is the failure mode Rule 15
// names: a dropped row must cost a printed line, never a mystery.
//
// It goes to stderr, which every CLI surface already carries and no machine
// consumer parses for data — the summary TABLE on stdout keeps its shape, so a
// damaged record cannot change what a pipeline reads, only what the operator
// sees.
func warnLineUnreadable(err error) {
	fmt.Fprintf(os.Stderr, "[gopi] session listing skipped an unreadable row: %v\n", err)
}
