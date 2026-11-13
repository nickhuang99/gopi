// reader.go — the record-level decoder for BOTH framings, and the one walk a
// consumer uses so it never branches on the format itself.
package rawlogfmt

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// readerBufBytes is the buffered reader size used to decode records. It bounds
// MEMORY, not what may be read: a payload of any length is streamed through.
const readerBufBytes = 64 << 10

// ErrMalformedRecord is returned when a record's framing does not hold: a framed
// header promises a length the file does not deliver, or a framed header turns up
// inside a legacy file. It is an ERROR and never a silent short read — a truncated
// payload handed on as if whole would corrupt a resume.
var ErrMalformedRecord = errors.New("raw log: malformed record framing")

// Reader decodes a raw log's records in file order, in EITHER framing, so a
// consumer that only cares about records never branches on the format.
//
// The legacy branch reproduces that framing exactly as it was written: a record's
// payload is the bytes between its marker line and the next marker line, minus the
// single trailing newline the legacy logger appended after each write. That one
// rule is right for all four legacy record kinds, which is why one reader serves
// every legacy consumer.
type Reader struct {
	br     *bufio.Reader
	format Format
	off    int64
	line   int
	// pending is a legacy marker line already read but not yet consumed: it is the
	// NEXT record's header, and re-reading it would cost a seek.
	pending      string
	pendingStart int64
	pendingLine  int
	hasPending   bool
	// stashed holds a record NextHeader read but the caller has not decoded yet.
	stashed *Record
	// framedPayloadLeft is the payload length NextHeader announced for a framed
	// record that Skip has not consumed yet (-1 = nothing outstanding).
	framedPayloadLeft int
	// framedHeader is the header NextHeader announced for a framed record whose
	// payload has not been read yet. It is what makes Decode-after-NextHeader
	// return a WHOLE record: without it the decoded record carried its payload but
	// no tag, no offset, no line numbers and no annotation — a shape a test that
	// only asserts payload bytes cannot see, and a reader that then reports
	// "record 0 on line 0" (measured: TestNextHeaderSkipWalksWithoutReadingPayloads
	// passed while Decode returned a tagless record).
	framedHeader *Header
	// skippedComments holds the comment/header (`#`) LINES the decoder stepped
	// over to reach the last record it returned, in file order. They are not
	// records — they carry no direction — but they ARE lines of the file, and a
	// reader that renders lines (LineScanner) must emit them or its line numbers
	// stop matching the file's (which is what `gopi session raw --lines A-B`
	// promises, and what the legacy reader this replaces did).
	skippedComments []string
	// closer is the file OpenReader opened, so a caller that used that door can
	// release it. It is nil for NewReaderFrom (the CALLER owns that io.Reader).
	closer io.Closer
	done   bool
}

// NewReader opens path for record-level decoding and detects its framing.
func NewReader(path string) (*Reader, error) {
	format, err := Detect(path)
	if err != nil {
		return nil, err
	}
	return OpenReader(path, format)
}

// OpenReader opens path for record-level decoding in a KNOWN framing — the door a
// caller uses when it has already asked Detect (one read instead of two).
func OpenReader(path string, format Format) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("raw log: %s: %w", path, err)
	}
	r := NewReaderFrom(f, format)
	r.closer = f
	return r, nil
}

// Close releases the file OpenReader opened. It is a no-op (nil) for a reader built
// by NewReaderFrom, whose io.Reader the caller owns — so a caller may defer Close
// unconditionally without knowing which door its reader came through.
func (r *Reader) Close() error {
	if r.closer == nil {
		return nil
	}
	err := r.closer.Close()
	r.closer = nil
	if err != nil {
		return fmt.Errorf("raw log: close: %w", err)
	}
	return nil
}

// NewReaderFrom wraps an already-open reader with a known framing. The caller owns
// the underlying reader's lifetime.
func NewReaderFrom(r io.Reader, format Format) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, readerBufBytes), format: format, line: 1, framedPayloadLeft: -1}
}

// Format is the framing this reader decodes.
func (r *Reader) Format() Format { return r.format }

// TakeSkippedComments returns (and clears) the comment/header (`#`) and blank
// LINES the decoder stepped over immediately before the record it last returned.
// It is the door a line renderer uses: those lines carry no direction, but they are
// in the file, so a reader that numbers lines must emit them in order.
//
// The contract is ordering, and it is what makes the line numbering exact: the
// comments returned here PRECEDE the record just handed out. Comments after the
// final record are returned once Next has reported io.EOF.
func (r *Reader) TakeSkippedComments() []string {
	out := r.skippedComments
	r.skippedComments = nil
	return out
}

// Next returns the next record, or io.EOF at the end of the file.
func (r *Reader) Next() (Record, error) {
	if r.stashed != nil {
		rec := *r.stashed
		r.stashed = nil
		return rec, nil
	}
	if r.done {
		return Record{}, io.EOF
	}
	if r.format == FormatFramed {
		return r.nextFramed()
	}
	return r.nextLegacy()
}

// Header is a record's framing without its payload: what a caller needs to walk a
// log cheaply. PayloadBytes is the declared payload length, so a caller can decide
// whether to read it at all.
type Header struct {
	// Tag is the record's direction tag.
	Tag string
	// Annotation is the legacy marker's annotation ("" for a framed record and for
	// a plain legacy marker).
	Annotation string
	// Offset is the byte offset of the record's header/marker line.
	Offset int64
	// HeaderLen is the header/marker line's byte length.
	HeaderLen int
	// HeaderTerminated reports whether that line ends with the record terminator
	// (false only for a legacy HTTP-ERROR marker, whose diagnostics are glued onto
	// the same line).
	HeaderTerminated bool
	// PayloadOffset is the byte offset of the payload's first byte.
	PayloadOffset int64
	// PayloadBytes is the declared payload length.
	PayloadBytes int
	// Line / EndLine are the 1-based line numbers the header and the record end on.
	Line int
	// EndLine is the record's last line.
	EndLine int
}

// NextHeader advances to the next record and returns its header WITHOUT keeping
// the payload in memory. It is the door a caller uses to answer "which records are
// here, where does each payload start, how long is it" over a log of any size.
//
// A FRAMED record is genuinely header-only: the length is in the header, so the
// payload is left unread and Skip jumps it. A LEGACY record's extent can only be
// learned by scanning to the next marker line, so for those the payload IS read
// (and stashed for the following Decode) — the asymmetry is intrinsic to the legacy
// framing, and it is why the framed one exists.
func (r *Reader) NextHeader() (Header, error) {
	if r.format == FormatFramed {
		return r.nextFramedHeader()
	}
	rec, err := r.Next()
	if rec.Tag == "" && err != nil {
		return Header{}, err
	}
	r.stashed = &rec
	return Header{
		Tag:              rec.Tag,
		Annotation:       rec.Annotation,
		Offset:           rec.Offset,
		HeaderLen:        rec.HeaderLen,
		HeaderTerminated: rec.HeaderTerminated,
		PayloadOffset:    rec.Offset + int64(rec.HeaderLen),
		PayloadBytes:     len(rec.Payload),
		Line:             rec.Line,
		EndLine:          rec.EndLine,
	}, err
}

// Decode returns the record NextHeader just announced, payload included.
func (r *Reader) Decode() (Record, error) { return r.Next() }

// Skip discards the payload of the record NextHeader just announced, so a walk that
// does not need payload bytes pays for the headers only. It is a no-op for a legacy
// record (already read into the stash).
func (r *Reader) Skip() error {
	if r.stashed != nil {
		r.stashed = nil
		return nil
	}
	if r.format != FormatFramed || r.framedPayloadLeft < 0 {
		return nil
	}
	n := r.framedPayloadLeft + 1 // payload + framing newline
	// The discarded payload still occupies lines: the NEXT record's header line
	// number is only correct if this reader counts them, and a caller that renders a
	// line span relies on it (see LineScanner).
	payload := make([]byte, r.framedPayloadLeft)
	if _, err := io.ReadFull(r.br, payload); err != nil && !errors.Is(err, io.EOF) {
		r.done = true
		return fmt.Errorf("%w: reading %d payload bytes at offset %d: %w", ErrMalformedRecord, len(payload), r.off, err)
	}
	r.line += 1 + bytes.Count(payload, []byte{RecordTerminator})
	r.off += int64(len(payload))
	r.framedPayloadLeft = -1
	r.framedHeader = nil
	if _, err := r.br.Discard(1); err != nil {
		r.done = true
		return fmt.Errorf("%w: discarding %d payload bytes at offset %d: %w", ErrMalformedRecord, n, r.off, err)
	}
	r.off += 1
	return nil
}

// nextFramedHeader reads only the header line of the next framed record.
func (r *Reader) nextFramedHeader() (Header, error) {
	for {
		start, startLine := r.off, r.line
		line, err := r.br.ReadString(RecordTerminator)
		if line != "" {
			r.off += int64(len(line))
			r.line++
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return Header{}, fmt.Errorf("raw log: header: %w", err)
		}
		if line == "" {
			r.done = true
			return Header{}, io.EOF
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" || trimmed[0] == '#' {
			r.skippedComments = append(r.skippedComments, trimmed)
			if errors.Is(err, io.EOF) {
				r.done = true
				return Header{}, io.EOF
			}
			continue
		}
		tag, length, annotation, ok := ParseHeaderLineAnnotated([]byte(line))
		if !ok {
			r.done = true
			return Header{}, fmt.Errorf("%w: no header at offset %d (%q)", ErrMalformedRecord, start, trimString(trimmed))
		}
		r.framedPayloadLeft = length
		h := Header{
			Tag:              ProbeTag(tag, annotation),
			Annotation:       annotation,
			Offset:           start,
			HeaderLen:        len(line),
			HeaderTerminated: true,
			PayloadOffset:    r.off,
			PayloadBytes:     length,
			Line:             startLine,
			EndLine:          r.line,
		}
		r.framedHeader = &h
		return h, nil
	}
}

// nextFramed decodes one length-prefixed record. Comment/header (`#`) lines are
// skipped, so the creation header and a trim marker need no special case here.
func (r *Reader) nextFramed() (Record, error) {
	if r.framedPayloadLeft >= 0 {
		// NextHeader announced this record; consume its payload now. The header it
		// announced is what makes the decoded record whole (see framedHeader) — the
		// payload alone cannot say which direction it was, where it started or which
		// lines it covers.
		start := r.off
		length := r.framedPayloadLeft
		r.framedPayloadLeft = -1
		rec := Record{Offset: start, Terminated: true}
		if h := r.framedHeader; h != nil {
			rec.Tag, rec.Annotation, rec.HeaderLen = h.Tag, h.Annotation, h.HeaderLen
			rec.HeaderTerminated = h.HeaderTerminated
			rec.Offset, rec.Line, rec.EndLine = h.Offset, h.Line, h.EndLine
			r.framedHeader = nil
		}
		payload := make([]byte, length)
		n, rerr := io.ReadFull(r.br, payload)
		rec.Payload = payload[:n]
		r.off += int64(n)
		if rerr != nil {
			r.done = true
			return rec, fmt.Errorf("%w: record at offset %d promised %d payload bytes, %d present (%w)",
				ErrMalformedRecord, start, length, n, rerr)
		}
		b, berr := r.br.ReadByte()
		if berr == nil && b == RecordTerminator {
			r.off++
			// THE LINE COUNTER MUST COUNT THE PAYLOAD'S LINES: without this, the next
			// record's Line was reported as if this payload were empty (measured: every
			// framed record after the first claimed line 2). A line span view renumbers
			// the whole file from these numbers.
			r.line += 1 + bytes.Count(rec.Payload, []byte{RecordTerminator})
			rec.EndLine = r.line - 1
			return rec, nil
		}
		r.done = true
		rec.Terminated = false
		if berr != nil && !errors.Is(berr, io.EOF) {
			return rec, fmt.Errorf("raw log: framing newline after offset %d: %w", start, berr)
		}
		return rec, fmt.Errorf("%w: record at offset %d is not closed by %q", ErrMalformedRecord, start, string(RecordTerminator))
	}
	for {
		start, startLine := r.off, r.line
		line, err := r.br.ReadString(RecordTerminator)
		if line != "" {
			r.off += int64(len(line))
			r.line++
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return Record{}, fmt.Errorf("raw log: header: %w", err)
		}
		if line == "" {
			r.done = true
			return Record{}, io.EOF
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" || trimmed[0] == '#' {
			r.skippedComments = append(r.skippedComments, trimmed)
			if errors.Is(err, io.EOF) {
				r.done = true
				return Record{}, io.EOF
			}
			continue
		}
		tag, length, annotation, ok := ParseHeaderLineAnnotated([]byte(line))
		if !ok {
			r.done = true
			return Record{}, fmt.Errorf("%w: no header at offset %d (%q)", ErrMalformedRecord, start, trimString(trimmed))
		}
		payload := make([]byte, length)
		n, rerr := io.ReadFull(r.br, payload)
		rec := Record{
			Tag:              ProbeTag(tag, annotation),
			Annotation:       annotation,
			Payload:          payload[:n],
			Offset:           start,
			HeaderLen:        len(line),
			HeaderTerminated: true,
			Line:             startLine,
			EndLine:          r.line,
		}
		r.off += int64(n)
		if rerr != nil {
			r.done = true
			return rec, fmt.Errorf("%w: record at offset %d promised %d payload bytes, %d present (%w)",
				ErrMalformedRecord, start, length, n, rerr)
		}
		b, berr := r.br.ReadByte()
		if berr == nil && b == RecordTerminator {
			rec.Terminated = true
			r.off++
			return rec, nil
		}
		// The byte after the payload is not the framing newline (or the file ended
		// there). The framing cannot be trusted past this point.
		r.done = true
		if berr != nil && !errors.Is(berr, io.EOF) {
			return rec, fmt.Errorf("raw log: framing newline after offset %d: %w", start, berr)
		}
		return rec, fmt.Errorf("%w: record at offset %d is not closed by %q", ErrMalformedRecord, start, string(RecordTerminator))
	}
}

// nextLegacy decodes one marker-framed record: the marker line, then every
// following line up to (but excluding) the next marker line, with exactly one
// trailing newline removed.
func (r *Reader) nextLegacy() (Record, error) {
	header := ""
	start, startLine := int64(0), 0
	if r.hasPending {
		header, start, startLine = r.pending, r.pendingStart, r.pendingLine
		r.hasPending = false
	} else {
		for {
			lineStart, lineNo := r.off, r.line
			line, err := r.readLine()
			if line != "" {
				r.off += int64(len(line))
				r.line++
			}
			if err != nil && !errors.Is(err, io.EOF) {
				return Record{}, err
			}
			if line == "" && errors.Is(err, io.EOF) {
				r.done = true
				return Record{}, io.EOF
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if _, _, ok := ParseHeaderLine([]byte(line)); ok {
				r.done = true
				return Record{}, fmt.Errorf("%w: framed header in a legacy log at line %d", ErrMalformedRecord, lineNo)
			}
			if trimmed != "" && IsLegacyMarkerLine(trimmed) {
				header, start, startLine = trimmed, lineStart, lineNo
				break
			}
			// Not a marker: a comment/header line or a blank one. It is still a LINE
			// of the file (see skippedComments).
			r.skippedComments = append(r.skippedComments, trimmed)
			if errors.Is(err, io.EOF) {
				r.done = true
				return Record{}, io.EOF
			}
		}
	}
	base := markerFor(header)
	annotation := LegacyAnnotation(header, base)
	tag, _ := LegacyTag(header)
	headerLen := len(header) + 1
	if base == MarkerHTTPError {
		// The marker line has no newline of its own and the diagnostics' first
		// bytes are glued onto it (HTTPErrorRemainder): only "HTTP-ERROR" belongs
		// to the framing, the rest is payload.
		headerLen = len(MarkerHTTPError)
	}
	rec := Record{
		Tag:        ProbeTag(tag, annotation),
		Annotation: annotation,
		Offset:     start,
		HeaderLen:  headerLen,
		// A legacy marker line ends with a newline for every direction except
		// HTTP-ERROR, whose marker is the bare word and whose diagnostics' first
		// bytes sit on the same line (they are payload — see HTTPErrorRemainder).
		HeaderTerminated: base != MarkerHTTPError,
		Line:             startLine,
	}
	var sb strings.Builder
	// HTTP-ERROR glues the diagnostics' first bytes onto the marker line itself
	// (see HTTPErrorRemainder): they ARE payload and must not be dropped.
	if rest := HTTPErrorRemainder(header); rest != "" {
		sb.WriteString(rest)
		sb.WriteString(string(RecordTerminator))
	}
	for {
		lineStart, lineNo := r.off, r.line
		line, err := r.readLine()
		if line != "" {
			r.off += int64(len(line))
			r.line++
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return rec, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed != "" && IsLegacyMarkerLine(trimmed) {
			r.pending, r.pendingStart, r.pendingLine, r.hasPending = trimmed, lineStart, lineNo, true
			rec.Payload, rec.Terminated = legacyPayload(&sb)
			rec.EndLine = lineNo
			return rec, nil
		}
		if line == "" && errors.Is(err, io.EOF) {
			rec.Payload, rec.Terminated = legacyPayload(&sb)
			rec.EndLine = lineNo
			r.done = true
			return rec, nil
		}
		sb.WriteString(line)
		if errors.Is(err, io.EOF) {
			rec.Payload, rec.Terminated = legacyPayload(&sb)
			rec.EndLine = lineNo
			r.done = true
			return rec, nil
		}
	}
}

// legacyPayload finishes a legacy record's payload: the decoder accumulates the
// record's lines WITH their terminators and then trims exactly one, because that
// terminator belongs to the framing (it closed the record), not to the payload.
//
// IT REPORTS THAT TRIM IN Terminated, and the report is load-bearing: FlattenRecord
// rebuilds a record's PHYSICAL text from Terminated, so a decoder that trimmed
// silently made a legacy record indistinguishable from a truncated framed tail —
// the line renderer then could not tell that the record's own closing newline had
// been removed and had to be put back. MEASURED on a real 1.79 GB legacy session
// log: `session raw --rounds` reported the LAST round's END as 7 where the file's
// own last line is 6, and every blank-separated SSE event lost its separator line,
// so 3381 rounds were displayed against a renumbered line span.
func legacyPayload(sb *strings.Builder) ([]byte, bool) {
	text := sb.String()
	if !strings.HasSuffix(text, string(RecordTerminator)) {
		// The file ended inside the payload (no terminator to trim): a truncated
		// tail, and saying so is what keeps the renderer honest about it.
		return []byte(text), false
	}
	return []byte(strings.TrimSuffix(text, string(RecordTerminator))), true
}

// readLine returns the next line WITH its newline. It does not advance the
// counters — the caller does, so a line it decides to push back is not
// double-counted.
func (r *Reader) readLine() (string, error) {
	line, err := r.br.ReadString(RecordTerminator)
	if err != nil && !errors.Is(err, io.EOF) {
		return line, fmt.Errorf("raw log: read: %w", err)
	}
	return line, err
}

// markerFor returns the base marker a legacy header line belongs to ("" when it is
// none).
func markerFor(line string) string {
	for _, m := range []string{MarkerRequest, MarkerResponse, MarkerChunk} {
		if markerMatches(line, m) {
			return m
		}
	}
	if isHTTPErrorMarker(line) {
		return MarkerHTTPError
	}
	return ""
}

// quotedFragmentBytes bounds a quoted fragment in an error message, so a corrupt log
// cannot put a megabyte of payload into one sentence.
const quotedFragmentBytes = 48

func trimString(s string) string {
	if len(s) <= quotedFragmentBytes {
		return s
	}
	return s[:quotedFragmentBytes] + "..."
}
